package expect

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// The publisher (AD-010, C-3, C-5) takes everything that can block off the
// tracker's loop: the hub broadcast, the notification dispatch and the activity
// broadcast. It is a bounded queue and a fixed pool of workers, so a stuck
// channel can delay at most its own notification, never window evaluation or
// another escalation (SEC-003). It also enforces the per-device ceiling on
// escalation pushes (SEC-005).
//
// Bounds (AD-012): the queue holds defaultQueueSize jobs, a full queue drops
// the push and keeps the event (R-3); defaultWorkers goroutines; every push has
// a PublishTimeout context; per device at most ExpectPushCeilingCount
// timestamps, one held count and one rule are kept, plus one summary per hour.
const (
	defaultQueueSize = 64
	defaultWorkers   = 2
	// PublishTimeout is the deadline of one push.
	PublishTimeout = 30 * time.Second
	// warnEvery is the per-kind throttle of the tracker's own Warn lines.
	warnEvery = time.Minute
)

type jobKind int

const (
	jobPush jobKind = iota
	jobActivity
	jobSummary
)

type job struct {
	kind     jobKind
	evt      models.Event
	rule     *models.AlertRule
	hostname string
	deviceID string
	activity *models.DeviceActivity // nil clears the device's activity
}

// pushState is the per-device ceiling state.
type pushState struct {
	times       []time.Time // the last pushes inside the rolling window (<= ExpectPushCeilingCount)
	held        int
	heldSince   time.Time
	heldRule    *models.AlertRule
	heldHost    string
	lastSummary time.Time
	queued      bool // a summary job is already queued
}

type publisher struct {
	log     *slog.Logger
	now     func() time.Time
	esc     Escalator
	bc      Broadcaster
	events  summaryStore
	timeout time.Duration
	queue   chan job
	workers int
	inline  bool
	warnFn  func(kind, msg string, args ...any)
	startMu sync.Mutex
	started bool
	mu      sync.Mutex
	push    map[string]*pushState
	// zone is the display zone of the held-summary time; nil means UTC.
	zone *time.Location
}

// summaryStore writes the held-summary event.
type summaryStore interface {
	CreateWarningEvent(ctx context.Context, e *models.Event) error
}

func newPublisher(o Options, now func() time.Time, log *slog.Logger, warn func(kind, msg string, args ...any)) *publisher {
	size := o.QueueSize
	if size <= 0 {
		size = defaultQueueSize
	}
	workers := o.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}
	timeout := o.PushTimeout
	if timeout <= 0 {
		timeout = PublishTimeout
	}
	return &publisher{
		log: log, now: now, esc: o.Escalator, bc: o.Broadcaster, events: o.Repo,
		timeout: timeout, queue: make(chan job, size), workers: workers, inline: o.InlinePublish,
		warnFn: warn, push: map[string]*pushState{}, zone: o.DisplayZone,
	}
}

// start launches the workers once. In inline mode (tests) there are none.
func (p *publisher) start(ctx context.Context) {
	p.startMu.Lock()
	defer p.startMu.Unlock()
	if p.started || p.inline {
		return
	}
	p.started = true
	for i := 0; i < p.workers; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case j := <-p.queue:
					p.run(ctx, j)
				}
			}
		}()
	}
}

// enqueue hands a job to the workers without blocking. It reports false when
// the queue is full (the job is dropped, with a throttled Warn).
func (p *publisher) enqueue(j job) bool {
	if p.inline {
		p.run(context.Background(), j)
		return true
	}
	select {
	case p.queue <- j:
		return true
	default:
		p.warnFn("publisher_full", "expected-activity publish queue full; push dropped (the event is kept)",
			"device_id", j.deviceID, "queue", cap(p.queue))
		return false
	}
}

func (p *publisher) run(parent context.Context, j job) {
	ctx, cancel := context.WithTimeout(parent, p.timeout)
	defer cancel()
	switch j.kind {
	case jobActivity:
		if p.bc != nil {
			p.bc.BroadcastActivity(j.deviceID, j.activity)
		}
	case jobPush:
		evt := j.evt
		if p.bc != nil {
			p.bc.BroadcastEvent(&evt)
		}
		if j.rule != nil && j.rule.Notify && p.allow(j.deviceID, j.rule, j.hostname) && p.esc != nil {
			if err := p.esc.PublishEscalation(ctx, evt, j.rule, j.hostname); err != nil {
				p.warnFn("publish_failed", "expected-activity escalation push failed",
					"device_id", j.deviceID, "error", err.Error())
			}
		}
	case jobSummary:
		p.sendSummary(ctx, j.deviceID)
	}
}

// allow charges one push against the device's ceiling. A push past the ceiling
// is held: the event is already stored and broadcast, only the notification is
// withheld, and the first held rule is remembered for the summary.
func (p *publisher) allow(deviceID string, rule *models.AlertRule, host string) bool {
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.stateLocked(deviceID)
	pruneTimes(s, now)
	if len(s.times) < models.ExpectPushCeilingCount {
		s.times = append(s.times, now)
		return true
	}
	if s.held == 0 {
		s.heldSince, s.heldRule, s.heldHost = now, rule, host
	}
	s.held++
	return false
}

func (p *publisher) stateLocked(deviceID string) *pushState {
	s := p.push[deviceID]
	if s == nil {
		s = &pushState{}
		p.push[deviceID] = s
	}
	return s
}

func pruneTimes(s *pushState, now time.Time) {
	kept := s.times[:0]
	for _, t := range s.times {
		if now.Sub(t) < models.ExpectPushCeilingWindow {
			kept = append(kept, t)
		}
	}
	s.times = kept
}

// flushHeld queues a summary push for every device whose held notifications
// can now be reported: capacity is free again and the last summary is more
// than the ceiling window old (at most one summary per device per hour).
func (p *publisher) flushHeld() {
	now := p.now()
	var due []string
	p.mu.Lock()
	for id, s := range p.push {
		pruneTimes(s, now)
		if s.held == 0 && len(s.times) == 0 && !s.queued && now.Sub(s.lastSummary) >= models.ExpectPushCeilingWindow {
			delete(p.push, id) // idle device: keep the map bounded by active devices
			continue
		}
		if s.held > 0 && !s.queued && len(s.times) < models.ExpectPushCeilingCount &&
			(s.lastSummary.IsZero() || now.Sub(s.lastSummary) >= models.ExpectPushCeilingWindow) {
			s.queued = true
			due = append(due, id)
		}
	}
	p.mu.Unlock()
	for _, id := range due {
		if !p.enqueue(job{kind: jobSummary, deviceID: id}) {
			p.mu.Lock()
			if s := p.push[id]; s != nil {
				s.queued = false
			}
			p.mu.Unlock()
		}
	}
}

// sendSummary stores and pushes the "N escalation notifications were held"
// event through the first held escalation's rule.
func (p *publisher) sendSummary(ctx context.Context, deviceID string) {
	now := p.now()
	p.mu.Lock()
	s := p.push[deviceID]
	if s == nil || s.held == 0 {
		if s != nil {
			s.queued = false
		}
		p.mu.Unlock()
		return
	}
	held, since, rule, host := s.held, s.heldSince, s.heldRule, s.heldHost
	s.held, s.heldRule, s.heldHost, s.heldSince = 0, nil, "", time.Time{}
	s.lastSummary, s.queued = now, false
	p.mu.Unlock()

	evt := models.Event{
		DeviceID: deviceID, Type: models.EventExpectNotificationsHeld, Severity: models.SeverityWarning,
		Message: heldSummaryMessage(held, models.SafeName(host), since, p.zone), CreatedAt: now,
	}
	if p.events != nil {
		if err := p.events.CreateWarningEvent(ctx, &evt); err != nil {
			p.warnFn("summary_failed", "expected-activity held summary could not be stored", "device_id", deviceID, "error", err.Error())
			return
		}
	}
	if p.bc != nil {
		e := evt
		p.bc.BroadcastEvent(&e)
	}
	if rule != nil && rule.Notify && p.esc != nil {
		if err := p.esc.PublishEscalation(ctx, evt, rule, host); err != nil {
			p.warnFn("publish_failed", "expected-activity held summary push failed", "device_id", deviceID, "error", err.Error())
		}
	}
}
