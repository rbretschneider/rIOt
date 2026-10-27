// Package expect holds the expected-activity Tracker (EXPECT-001): it derives
// every window from the commands table, answers "is this container death or
// offline transition expected, and by which command?" from an in-memory cache,
// ends windows through one routine that always runs the backstop (finishWindow),
// raises the single escalation when something is still down, and computes the
// per-device activity the dashboard shows.
//
// Lock discipline (ADD section 12 note 2): the Tracker never calls the
// Escalator, the publisher, the Broadcaster or a repository while holding its
// mutex; CommandChanged never blocks; reconcile builds off-lock and merges under
// the lock.
package expect

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
)

// Readiness errors. Every Cover* call returns one of them when the tracker
// cannot answer, and the event generator then alerts exactly as it does without
// windows (NFR-009, AD-002).
var (
	ErrNotReady    = errors.New("expected-activity tracker has not loaded")
	ErrStale       = errors.New("expected-activity tracker reconcile is stale")
	ErrLoopStalled = errors.New("expected-activity tracker loop is not live")
)

const (
	// dbTimeout is the context of every database call (AD-009).
	dbTimeout = 5 * time.Second
	// tickBudget: no new database call starts after this much tick time; the
	// rest carries over to the next tick (C-3).
	tickBudget = 3 * time.Second
	// evictAfter: a window whose finish keeps failing this long after its close
	// time leaves the cache (not the database) and is retried after the next reload.
	evictAfter = 5 * time.Minute
	// closedRecentTTL is how long the id of a window this process closed is kept,
	// so a late hook or a lagging reconcile cannot reopen it (NFR-004: released
	// within 5 minutes).
	closedRecentTTL = 5 * time.Minute
	// overflowEventEvery is the most often one device gets an overflow warning event.
	overflowEventEvery = time.Hour
	// endedPOTTL is how long a registration keeps the powered-off record it ended
	// for the online event that follows it.
	endedPOTTL = 5 * time.Minute
	// reconcileEvery is the reconcile period.
	reconcileEvery = 30 * time.Second
)

// Options configures a Tracker. Repo and Devices are required for Load.
type Options struct {
	Repo      db.ExpectedActivityRepository
	Devices   DeviceLister
	Policies  PolicyLister
	Telemetry TelemetryReader
	Commands  CommandReader
	Escalator Escalator
	// Broadcaster receives the stored escalation events and the activity changes.
	Broadcaster Broadcaster
	// NewestAgentVersion returns the newest agent version the server knows ("" if none).
	NewestAgentVersion func() string
	// Now is the clock; nil means time.Now. No code of this package reads the
	// wall clock directly.
	Now func() time.Time
	// GraceEnd is the end of the restart grace (AD-008). Before it the tracker
	// makes no online-state decision. The zero value means no grace.
	GraceEnd time.Time
	Logger   *slog.Logger
	// DisplayZone is the zone the event messages write times in; nil means UTC
	// (TIME-12H-001 AD-004). The process zone is never used.
	DisplayZone *time.Location

	// Publisher tuning (defaults: queue 64, 2 workers, 30 s per push).
	QueueSize   int
	Workers     int
	PushTimeout time.Duration
	// InlinePublish runs every push on the caller's goroutine instead of the
	// workers. It exists for deterministic tests only.
	InlinePublish bool
}

type openEscalation struct {
	commandID, action, actor, reason, bound string
	at, startedAt                           time.Time
	// fromVersion and toVersion are the versions of an agent self-update record
	// ("" for a command), carried so the escalated label names the update
	// (EXPECT-002 AD-009).
	fromVersion, toVersion string
	// origin is the origin of the escalated command (EXPECT-008 AD-010).
	origin string
}

type devState struct {
	id           string
	known        bool // state came from the device list or a hook
	hostname     string
	agentVersion string

	online        bool
	onlineSince   time.Time
	offlineSince  time.Time
	returnedAt    time.Time
	lastHeartbeat time.Time

	windows    map[string]*window // at most ExpectPerDeviceWindowCap
	poweredOff *models.PoweredOffState
	// endedPO is the powered-off record a registration just ended, kept so the
	// online event that follows it can say "powered off <duration>" (the
	// handler calls NoteRegistration before NoteOnline). It is consumed by
	// NoteOnline and dropped by housekeep after endedPOTTL.
	endedPO    *models.PoweredOffState
	endedPOAt  time.Time
	escalation *openEscalation

	overflow        int
	overflowEventAt time.Time
	lastActivity    *models.DeviceActivity // the last activity successfully queued for broadcast
}

func (d *devState) view() deviceView {
	return deviceView{online: d.online, onlineSince: d.onlineSince, returnedAt: d.returnedAt}
}

// devSnap is a copy of the device state taken under the lock.
type devSnap struct {
	id, hostname, agentVersion string
	online                     bool
	onlineSince, offlineSince  time.Time
	returnedAt                 time.Time
}

func (d *devState) snap() devSnap {
	return devSnap{id: d.id, hostname: d.hostname, agentVersion: d.agentVersion, online: d.online,
		onlineSince: d.onlineSince, offlineSince: d.offlineSince, returnedAt: d.returnedAt}
}

// Tracker is the expected-activity tracker.
type Tracker struct {
	opt      Options
	log      *slog.Logger
	now      func() time.Time
	graceEnd time.Time
	pub      *publisher
	wake     chan struct{}
	tickMu   sync.Mutex

	mu            sync.Mutex
	devs          map[string]*devState
	closedRecent  map[string]time.Time
	policyTargets map[int]string
	loaded        bool
	lastReconcile time.Time
	lastTick      time.Time
	warned        map[string]time.Time

	// selfResolver ends agent self-update records at their bound (EXPECT-002). It
	// is set once, before Load, and only read afterwards.
	selfResolver SelfUpdateResolver
	// reinstallResolver ends installer reinstall records at their bound (EXPECT-008).
	// Set once, before Load, and only read afterwards.
	reinstallResolver ReinstallResolver
	// observedAfterGrace is set by the first ObserveDevices call at or after
	// graceEnd: the offline detector's pass that makes a device that died during
	// downtime known to be offline (the detector gate of AD-006, INFO-4).
	observedAfterGrace bool
}

// New builds a Tracker. It starts no goroutine (Run does). The returned value is
// never nil; never assign a nil *Tracker to an interface.
func New(o Options) *Tracker {
	t := &Tracker{
		opt: o, graceEnd: o.GraceEnd,
		wake:          make(chan struct{}, 1),
		devs:          map[string]*devState{},
		closedRecent:  map[string]time.Time{},
		policyTargets: map[int]string{},
		warned:        map[string]time.Time{},
	}
	t.now = func() time.Time { return time.Now().UTC() }
	if o.Now != nil {
		t.now = func() time.Time { return o.Now().UTC() }
	}
	t.log = o.Logger
	if t.log == nil {
		t.log = slog.Default()
	}
	t.pub = newPublisher(o, t.now, t.log, t.warnThrottled)
	return t
}

// warnThrottled writes a Warn at most once a minute per kind (AD-012). The kinds
// are a fixed set, so the map is bounded.
func (t *Tracker) warnThrottled(kind, msg string, args ...any) {
	now := t.now()
	t.mu.Lock()
	last, seen := t.warned[kind]
	if seen && now.Sub(last) < warnEvery {
		t.mu.Unlock()
		return
	}
	t.warned[kind] = now
	t.mu.Unlock()
	t.log.Warn(msg, args...)
}

// Ready reports whether Cover* can answer: nil, or the reason it cannot.
func (t *Tracker) Ready() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.readyLocked(t.now())
}

// readyLocked is the three-part readiness of AD-002: loaded, reconciled within
// ExpectStaleAfter, and a loop tick completed within ExpectLoopStaleAfter.
func (t *Tracker) readyLocked(now time.Time) error {
	switch {
	case !t.loaded:
		return ErrNotReady
	case now.Sub(t.lastReconcile) > models.ExpectStaleAfter:
		return ErrStale
	case now.Sub(t.lastTick) > models.ExpectLoopStaleAfter:
		return ErrLoopStalled
	}
	return nil
}

// WindowCount is the number of windows currently cached (all devices).
func (t *Tracker) WindowCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, d := range t.devs {
		n += len(d.windows)
	}
	return n
}

func (t *Tracker) graceOver(now time.Time) bool { return !now.Before(t.graceEnd) }

// devLocked returns the device's state, creating it online-since-now when the
// tracker has never seen the device.
func (t *Tracker) devLocked(id string) *devState {
	d := t.devs[id]
	if d == nil {
		d = &devState{id: id, windows: map[string]*window{}, online: true, onlineSince: t.now()}
		t.devs[id] = d
	}
	return d
}

func (t *Tracker) policyTargetLocked(c *models.Command) string {
	if c.PolicyID == nil {
		return ""
	}
	return t.policyTargets[*c.PolicyID]
}

func (t *Tracker) actorPhraseLocked(w *window) string {
	return models.ActorPhrase(w.cmd, t.policyTargetLocked(&w.cmd))
}

func (t *Tracker) actorPhrase(w *window) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.actorPhraseLocked(w)
}

// trimCommand copies the fields the tracker needs and nothing else (no params,
// steps, output): the cache holds at most ExpectPerDeviceWindowCap of them per device.
func trimCommand(c models.Command) models.Command {
	out := models.Command{
		ID: c.ID, DeviceID: c.DeviceID, Action: c.Action, Status: c.Status, Origin: c.Origin,
		ActorName: models.SafeName(c.ActorName), PolicyID: copyIntPtr(c.PolicyID),
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, ExpiresAt: c.ExpiresAt, ExecDeadlineS: c.ExecDeadlineS,
		DeadlineAt: copyTimePtr(c.DeadlineAt), DispatchedAt: copyTimePtr(c.DispatchedAt),
		StartedAt: copyTimePtr(c.StartedAt), VerifyingAt: copyTimePtr(c.VerifyingAt), FinishedAt: copyTimePtr(c.FinishedAt),
		Step: c.Step, Progress: copyIntPtr(c.Progress),
	}
	out.TargetKeys = append([]string(nil), c.TargetKeys...)
	return out
}

func copyIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func copyTimePtr(p *time.Time) *time.Time {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func startOf(c *models.Command) time.Time {
	if c.DispatchedAt != nil {
		return *c.DispatchedAt
	}
	return c.CreatedAt
}

// ---------------------------------------------------------------------------
// Command hook (AD-004)
// ---------------------------------------------------------------------------

// CommandChanged is the commands.CommandObserver hook. It copies the row under
// the lock, does no I/O and never blocks: it signals the loop to tick.
func (t *Tracker) CommandChanged(c models.Command) {
	if c.WindowClosedAt != nil {
		return
	}
	policy, ok := models.ExpectPolicyFor(c)
	if !ok {
		return
	}
	// The versions of a self-update record are read from its params before the
	// row is trimmed (EXPECT-002 implementation note 6).
	selfFrom, selfTo := selfUpdateVersionsOf(c)
	snap := trimCommand(c)
	snap.Normalize()
	now := t.now()

	t.mu.Lock()
	if _, closed := t.closedRecent[c.ID]; closed {
		// A late result after timed_out must not reopen a processed window.
		t.mu.Unlock()
		return
	}
	dev := t.devLocked(c.DeviceID)
	if w := dev.windows[c.ID]; w != nil {
		w.cmd = snap
	} else {
		w = &window{cmd: snap, policy: policy, addedAt: now, selfFrom: selfFrom, selfTo: selfTo}
		if policy.Class == models.WindowAgent {
			w.fromVersion = dev.agentVersion
		}
		t.admitLocked(dev, w, now)
	}
	t.mu.Unlock()
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

// priorityLocked orders windows for the per-device cap: 1 active or still
// covering, 2 due, 3 queued (AD-009).
func (t *Tracker) priorityLocked(w *window, now time.Time) int {
	c := &w.cmd
	switch {
	case c.DispatchedAt == nil && !c.State().IsTerminal():
		return 3
	case c.State().IsTerminal() && c.DispatchedAt != nil && !now.Before(finishedAt(c).Add(w.policy.PostTerminal)):
		return 2
	case c.State().IsTerminal() && c.DispatchedAt == nil:
		return 2
	}
	return 1
}

// admitLocked adds a new window, keeping the per-device bound. At the cap the
// lowest-priority cached window leaves the cache (it stays pending in the
// database, so its backstop is deferred, never skipped); a new window that
// ranks below every cached one is not cached.
func (t *Tracker) admitLocked(dev *devState, w *window, now time.Time) {
	if len(dev.windows) < models.ExpectPerDeviceWindowCap {
		dev.windows[w.cmd.ID] = w
		return
	}
	var victim *window
	vp := 0
	for _, x := range dev.windows {
		p := t.priorityLocked(x, now)
		if p > vp || (p == vp && victim != nil && x.cmd.CreatedAt.Before(victim.cmd.CreatedAt)) {
			victim, vp = x, p
		}
	}
	if victim == nil || vp <= t.priorityLocked(w, now) {
		dev.overflow++
		return
	}
	delete(dev.windows, victim.cmd.ID)
	dev.overflow++
	dev.windows[w.cmd.ID] = w
}

// ---------------------------------------------------------------------------
// Coverage (AD-005, AD-007)
// ---------------------------------------------------------------------------

func (t *Tracker) coverageLocked(w *window) models.Coverage {
	return models.Coverage{
		CommandID: w.cmd.ID, ShortID: models.ShortCommandID(w.cmd.ID), Action: w.cmd.Action,
		ActionLabel: windowActionLabel(w), ActorPhrase: t.actorPhraseLocked(w),
		Target: models.CommandTargetLabel(w.cmd), Class: w.policy.Class,
	}
}

// CoverContainer says whether a container death (or OOM) at time at is covered
// by an open window of the device, and by which command (FR-007: a window that
// holds the event's exact target beats a host-class window; ties go to the
// latest dispatch). keys are the event's target keys. It reads memory only. An
// error means the tracker cannot answer and the caller must alert as it would
// without windows.
func (t *Tracker) CoverContainer(deviceID string, keys []string, at time.Time) (models.Coverage, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if err := t.readyLocked(now); err != nil {
		return models.Coverage{}, false, err
	}
	dev := t.devs[deviceID]
	if dev == nil {
		return models.Coverage{}, false, nil
	}
	dv := dev.view()
	var best *window
	var bestD time.Time
	bestTargeted := false
	for _, w := range dev.windows {
		if w.policy.Containers == models.CoverNoContainers {
			continue
		}
		tl := evaluate(w, dv, now, t.graceEnd)
		if !tl.coversAt(tl.containerEnd, at) {
			continue
		}
		targeted := false
		if w.policy.Containers == models.CoverTargetContainers {
			if !intersects(w.cmd.TargetKeys, keys) {
				continue
			}
			targeted = true
		}
		if best == nil || (targeted && !bestTargeted) || (targeted == bestTargeted && tl.d.After(bestD)) {
			best, bestD, bestTargeted = w, tl.d, targeted
		}
	}
	if best == nil {
		return models.Coverage{}, false, nil
	}
	return t.coverageLocked(best), true, nil
}

const poweredOffCallTimeout = dbTimeout

// CoverOffline says whether an offline transition at offlineAt is covered. It
// also records the transition in the tracker's device state.
//
// A succeeded shutdown whose return phase contains offlineAt enters the
// powered-off state (AD-007). A pre-existing powered-off row is never a
// coverage reason: a live-to-offline transition proves the device was alive
// after it powered off, so the row is deleted and the transition alerts
// (C-1 a). Every storage failure answers "not covered".
func (t *Tracker) CoverOffline(ctx context.Context, deviceID string, offlineAt time.Time) (models.OfflineCoverage, bool, error) {
	t.mu.Lock()
	now := t.now()
	if err := t.readyLocked(now); err != nil {
		t.mu.Unlock()
		return models.OfflineCoverage{}, false, err
	}
	dev := t.devLocked(deviceID)
	dev.known = true
	dev.online = false
	dev.offlineSince = offlineAt
	dv := dev.view()

	var best *window
	var bestTL timeline
	for _, w := range dev.windows {
		if !w.policy.CoversOffline {
			continue
		}
		tl := evaluate(w, dv, now, t.graceEnd)
		if !tl.coversAt(tl.offlineEnd, offlineAt) {
			continue
		}
		if best == nil || tl.d.After(bestTL.d) {
			best, bestTL = w, tl
		}
	}
	stale := dev.poweredOff
	if best == nil {
		dev.poweredOff = nil
		t.mu.Unlock()
		if stale != nil {
			t.deletePoweredOff(ctx, deviceID)
		}
		return models.OfflineCoverage{}, false, nil
	}
	w := *best
	actor := t.actorPhraseLocked(best)
	cov := models.OfflineCoverage{
		Kind: models.OfflineKindWindow, CommandID: w.cmd.ID, ShortID: models.ShortCommandID(w.cmd.ID),
		Action: w.cmd.Action, ActionLabel: windowActionLabel(&w), ActorPhrase: actor, BackBy: bestTL.backBy,
		Zone: t.opt.DisplayZone,
	}
	entry := w.policy.Class == models.WindowShutdown && w.cmd.State() == models.CommandSucceeded
	if !entry {
		dev.poweredOff = nil
	}
	t.mu.Unlock()

	if !entry {
		if stale != nil {
			t.deletePoweredOff(ctx, deviceID)
		}
		return cov, true, nil
	}

	state := models.PoweredOffState{
		DeviceID: deviceID, CommandID: w.cmd.ID, ActionLabel: cov.ActionLabel, ActorPhrase: actor,
		ShutdownAt: finishedAt(&w.cmd), OfflineAt: offlineAt, CreatedAt: offlineAt,
	}
	cctx, cancel := context.WithTimeout(ctx, poweredOffCallTimeout)
	stored, err := t.opt.Repo.UpsertPoweredOff(cctx, &state)
	cancel()
	if err != nil || !stored {
		// Unverifiable, or this very command already holds the row: the transition
		// alerts normally and any row is dropped (C-1 a, c).
		if err != nil {
			t.warnThrottled("upsert_powered_off", "powered-off state could not be stored; the offline alerts", "device_id", deviceID, "error", err.Error())
		}
		t.mu.Lock()
		dev.poweredOff = nil
		if cw := dev.windows[w.cmd.ID]; cw != nil {
			cw.noPoweredOff = true
		}
		t.mu.Unlock()
		t.deletePoweredOff(ctx, deviceID)
		return models.OfflineCoverage{}, false, nil
	}
	t.mu.Lock()
	dev.poweredOff = &state
	t.mu.Unlock()
	t.finishWindow(ctx, deviceID, w.cmd.ID, models.EndPoweredOff, false)
	cov.Kind = models.OfflineKindPoweredOff
	cov.ShutdownAt = state.ShutdownAt
	cov.BackBy = nil
	return cov, true, nil
}

// deletePoweredOff removes the durable powered-off row of a device. A failure is
// logged; the row can never cover anything, and the next purge removes it.
func (t *Tracker) deletePoweredOff(ctx context.Context, deviceID string) *models.PoweredOffState {
	cctx, cancel := context.WithTimeout(ctx, poweredOffCallTimeout)
	defer cancel()
	row, err := t.opt.Repo.DeletePoweredOff(cctx, deviceID)
	if err != nil {
		t.warnThrottled("delete_powered_off", "powered-off row could not be deleted", "device_id", deviceID, "error", err.Error())
		return nil
	}
	return row
}

func (t *Tracker) clearEscalations(ctx context.Context, deviceID string, at time.Time) {
	cctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	if _, err := t.opt.Repo.ClearDeviceEscalations(cctx, deviceID, at); err != nil {
		t.warnThrottled("clear_escalations", "device escalations could not be cleared", "device_id", deviceID, "error", err.Error())
	}
}

// NoteOnline is called when a device registers while it was not live (FR-044,
// FR-030). It always clears the powered-off row and the open escalations
// durably, whether or not the tracker is ready (C-1 b), and returns the note
// that names the action when a window covered the device or it was powered off.
func (t *Tracker) NoteOnline(ctx context.Context, deviceID string, at time.Time) *models.OnlineNote {
	t.mu.Lock()
	dev := t.devLocked(deviceID)
	dev.known = true
	offlineSince := dev.offlineSince
	dev.online = true
	dev.onlineSince = at
	dev.returnedAt = at
	cachedPO := dev.poweredOff
	if cachedPO == nil {
		cachedPO = dev.endedPO
	}
	dev.poweredOff, dev.endedPO = nil, nil
	dev.escalation = nil

	var best *window
	var bestD time.Time
	now := t.now()
	dv := dev.view()
	for _, w := range dev.windows {
		if !w.policy.CoversOffline || w.deferred {
			continue
		}
		tl := evaluate(w, dv, now, t.graceEnd)
		if !tl.opened || tl.phase == models.PhaseSettling && tl.offlineEnd.IsZero() {
			continue
		}
		// A self-update record names itself only when the offline that just ended
		// began inside its coverage (EXPECT-002 AD-008): the transition has already
		// run, so offlineEnd is the registration (V) or the failure (F).
		if (w.policy.SelfUpdate || w.policy.Reinstall) && !tl.coversAt(tl.offlineEnd, offlineSince) {
			continue
		}
		if best == nil || tl.d.After(bestD) {
			best, bestD = w, tl.d
		}
	}
	var note *models.OnlineNote
	if best != nil {
		wcopy := *best
		d := time.Duration(0)
		if !offlineSince.IsZero() && at.After(offlineSince) {
			d = at.Sub(offlineSince)
		}
		note = &models.OnlineNote{
			CommandID: wcopy.cmd.ID, ShortID: models.ShortCommandID(wcopy.cmd.ID),
			ActionLabel: windowActionLabel(&wcopy), ActorPhrase: t.actorPhraseLocked(&wcopy), Offline: d,
		}
	}
	t.mu.Unlock()

	row := t.deletePoweredOff(ctx, deviceID)
	t.clearEscalations(ctx, deviceID, at)
	po := cachedPO
	if po == nil {
		po = row
	}
	if po != nil {
		d := at.Sub(po.OfflineAt)
		if d < 0 {
			d = 0
		}
		return &models.OnlineNote{
			CommandID: po.CommandID, ShortID: models.ShortCommandID(po.CommandID),
			ActionLabel: po.ActionLabel, ActorPhrase: po.ActorPhrase, Offline: d, PoweredOff: true,
		}
	}
	return note
}

// NoteRegistration is called on every re-registration of an existing device,
// including when the device was already online: a registration is evidence of
// life, so a powered-off row is deleted (C-1 b). It records the observed return
// that the reboot and agent windows need to close early.
func (t *Tracker) NoteRegistration(deviceID string, at time.Time) {
	t.mu.Lock()
	dev := t.devLocked(deviceID)
	cached := dev.poweredOff
	needDelete := cached != nil || !dev.known || !t.loaded
	if !dev.online {
		dev.onlineSince = at
	}
	dev.online = true
	dev.returnedAt = at
	dev.poweredOff = nil
	if cached != nil {
		dev.endedPO, dev.endedPOAt = cached, at
	}
	t.mu.Unlock()
	if needDelete {
		row := t.deletePoweredOff(context.Background(), deviceID)
		if row != nil && cached == nil {
			t.mu.Lock()
			dev.endedPO, dev.endedPOAt = row, at
			t.mu.Unlock()
		}
	}
}

// durableAction is a database write decided under the lock and run after it.
type durableAction struct {
	deviceID      string
	deletePowered bool
	clearEsc      bool
	at            time.Time
}

// observeLocked folds the device list into the tracker's device state. initial
// is true at Load: live devices start online-since-at, offline devices are
// offline since their last heartbeat plus the detection threshold. It returns
// the durable clears to run after the lock is released (AD-006 path 4).
func (t *Tracker) observeLocked(devices []models.Device, at time.Time, initial bool) []durableAction {
	var acts []durableAction
	for i := range devices {
		d := &devices[i]
		dev := t.devLocked(d.ID)
		dev.known = true
		dev.hostname = d.Hostname
		dev.agentVersion = d.AgentVersion
		if d.LastHeartbeat != nil {
			dev.lastHeartbeat = *d.LastHeartbeat
		}
		live := models.DeviceIsLive(d.Status)
		switch {
		case initial:
			dev.online = live
			if live {
				dev.onlineSince, dev.returnedAt = at, at
			} else {
				dev.offlineSince = offlineStart(dev, at)
			}
		case live && !dev.online:
			dev.online = true
			dev.onlineSince, dev.returnedAt = at, at
			dev.poweredOff, dev.escalation = nil, nil
			acts = append(acts, durableAction{deviceID: d.ID, deletePowered: true, clearEsc: true, at: at})
		case !live && dev.online:
			dev.online = false
			dev.offlineSince = offlineStart(dev, at)
		}
		if dev.poweredOff != nil && (live || (!dev.lastHeartbeat.IsZero() && dev.lastHeartbeat.After(dev.poweredOff.OfflineAt))) {
			dev.poweredOff = nil
			acts = append(acts, durableAction{deviceID: d.ID, deletePowered: true, at: at})
		}
	}
	return acts
}

// offlineStart is the time a device that is offline went offline: its last
// heartbeat plus the detection threshold, or at when it never heartbeated.
func offlineStart(dev *devState, at time.Time) time.Time {
	if dev.lastHeartbeat.IsZero() {
		return at
	}
	return dev.lastHeartbeat.Add(models.OfflineHeartbeatThreshold)
}

func (t *Tracker) runActions(ctx context.Context, acts []durableAction) {
	for _, a := range acts {
		if a.deletePowered {
			t.deletePoweredOff(ctx, a.deviceID)
		}
		if a.clearEsc {
			t.clearEscalations(ctx, a.deviceID, a.at)
		}
	}
}

// ObserveDevices is called by the offline worker after each pass with the
// devices' current statuses (AD-006 path 4). A device that is live while the
// tracker had it offline is a return without a registration (a heartbeat); it
// runs the same transition and durable clears as NoteOnline, without an event.
func (t *Tracker) ObserveDevices(devices []models.Device, at time.Time) {
	t.mu.Lock()
	acts := t.observeLocked(devices, at, false)
	if !at.Before(t.graceEnd) {
		t.observedAfterGrace = true
	}
	t.mu.Unlock()
	t.runActions(context.Background(), acts)
}

// ---------------------------------------------------------------------------
// Load, reconcile (AD-009)
// ---------------------------------------------------------------------------

// Load reads the tracker's state from the database. Until it has succeeded once
// every Cover* call returns ErrNotReady.
func (t *Tracker) Load(ctx context.Context) error { return t.reconcile(ctx, true) }

// Reconcile re-reads the database and merges it into the cache. It runs every
// 30 s on its own goroutine (Run); tests call it directly.
func (t *Tracker) Reconcile(ctx context.Context) error { return t.reconcile(ctx, false) }

type reconcileData struct {
	poweredOff []models.PoweredOffState
	open       []models.Command
	backlog    []models.Command
	lapse      []models.Command
	escs       []models.Escalation
	devices    []models.Device
	policies   []models.AutoUpdatePolicy
	havePol    bool
	escCmds    map[string]models.Command
}

func (t *Tracker) readAll(ctx context.Context, start time.Time) (reconcileData, error) {
	var rd reconcileData
	repo := t.opt.Repo
	step := func(f func(ctx context.Context) error) error {
		cctx, cancel := context.WithTimeout(ctx, dbTimeout)
		defer cancel()
		return f(cctx)
	}
	var err error
	if err = step(func(c context.Context) error { _, e := repo.PurgeInvalidPoweredOff(c); return e }); err != nil {
		return rd, err
	}
	if err = step(func(c context.Context) (e error) { rd.poweredOff, e = repo.ListPoweredOff(c); return }); err != nil {
		return rd, err
	}
	if err = step(func(c context.Context) (e error) {
		rd.open, e = repo.ListOpenWindows(c, start, models.ExpectOpenWindowsLimit)
		return
	}); err != nil {
		return rd, err
	}
	if err = step(func(c context.Context) (e error) {
		rd.backlog, e = repo.ListDueBacklog(c, start, models.ExpectDueBacklogLimit)
		return
	}); err != nil {
		return rd, err
	}
	if err = step(func(c context.Context) (e error) {
		rd.lapse, e = repo.ListLapseCandidates(c, start.Add(-models.ExpectDeferredRecheckBound), models.ExpectLapseLimit)
		return
	}); err != nil {
		return rd, err
	}
	if err = step(func(c context.Context) (e error) {
		rd.escs, e = repo.ListOpenDeviceEscalations(c, start.Add(-models.ExpectEscalationDisplayTTL))
		return
	}); err != nil {
		return rd, err
	}
	if t.opt.Devices != nil {
		if err = step(func(c context.Context) (e error) { rd.devices, e = t.opt.Devices.List(c); return }); err != nil {
			return rd, err
		}
	}
	if t.opt.Policies != nil {
		if e := step(func(c context.Context) (e error) { rd.policies, e = t.opt.Policies.ListAll(c); return }); e != nil {
			t.warnThrottled("list_policies", "expected-activity could not read the auto-update policies; actor phrases use ids", "error", e.Error())
		} else {
			rd.havePol = true
		}
	}
	return rd, nil
}

func (t *Tracker) reconcile(ctx context.Context, initial bool) error {
	start := t.now()
	rd, err := t.readAll(ctx, start)
	if err != nil {
		t.warnThrottled("reconcile", "expected-activity reconcile failed", "error", err.Error())
		return err
	}

	// Escalation labels need the command of every open device escalation.
	loaded := map[string]models.Command{}
	for _, list := range [][]models.Command{rd.open, rd.backlog, rd.lapse} {
		for _, c := range list {
			loaded[c.ID] = c
		}
	}
	rd.escCmds = map[string]models.Command{}
	if t.opt.Commands != nil {
		for _, e := range rd.escs {
			if _, ok := loaded[e.CommandID]; ok {
				continue
			}
			if _, ok := rd.escCmds[e.CommandID]; ok {
				continue
			}
			cctx, cancel := context.WithTimeout(ctx, dbTimeout)
			c, gerr := t.opt.Commands.GetByID(cctx, e.CommandID)
			cancel()
			if gerr == nil && c != nil {
				rd.escCmds[e.CommandID] = *c
			}
		}
	}

	var deleted []deletedWindow
	var overflowEvents []overflowEvent
	t.mu.Lock()
	acts := t.mergeLocked(rd, loaded, start, initial, &deleted, &overflowEvents)
	t.loaded = true
	t.lastReconcile = start
	if initial || t.lastTick.IsZero() {
		t.lastTick = start
	}
	t.mu.Unlock()

	t.runActions(ctx, acts)
	for _, d := range deleted {
		t.finishWindow(ctx, d.devID, d.winID, models.EndDeleted, false)
	}
	for _, o := range overflowEvents {
		t.writeWarning(ctx, o.deviceID, o.message)
	}
	return nil
}

type deletedWindow struct{ devID, winID string }

type overflowEvent struct{ deviceID, message string }

func (t *Tracker) writeWarning(ctx context.Context, deviceID, message string) {
	cctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	e := models.Event{DeviceID: deviceID, Type: models.EventExpectBackstopSkipped, Severity: models.SeverityWarning,
		Message: message, CreatedAt: t.now()}
	if err := t.opt.Repo.CreateWarningEvent(cctx, &e); err != nil {
		t.warnThrottled("warning_event", "expected-activity warning event could not be stored", "device_id", deviceID, "error", err.Error())
		return
	}
	if t.opt.Broadcaster != nil {
		t.opt.Broadcaster.BroadcastEvent(&e)
	}
}

// mergeLocked folds the loaded rows into the cache (AD-009 reconcile steps 5 and
// the overflow, deleted-row rules). The caller holds t.mu.
func (t *Tracker) mergeLocked(rd reconcileData, loaded map[string]models.Command, start time.Time, initial bool,
	deleted *[]deletedWindow, overflowEvents *[]overflowEvent) []durableAction {

	if rd.havePol {
		m := make(map[int]string, len(rd.policies))
		for _, p := range rd.policies {
			m[p.ID] = p.Target
		}
		t.policyTargets = m
	}

	var acts []durableAction
	seen := make(map[string]bool, len(rd.devices))
	if rd.devices != nil || t.opt.Devices != nil {
		acts = t.observeLocked(rd.devices, start, initial)
		for _, d := range rd.devices {
			seen[d.ID] = true
		}
	}

	// Candidate windows per device, deduplicated across the three lists.
	byDev := map[string][]models.Command{}
	added := map[string]bool{}
	for _, list := range [][]models.Command{rd.open, rd.backlog, rd.lapse} {
		for _, c := range list {
			if added[c.ID] {
				continue
			}
			if _, closed := t.closedRecent[c.ID]; closed {
				continue
			}
			if _, ok := models.ExpectPolicyFor(c); !ok {
				continue
			}
			added[c.ID] = true
			byDev[c.DeviceID] = append(byDev[c.DeviceID], c)
		}
	}
	openTruncated := len(rd.open) >= models.ExpectOpenWindowsLimit
	backlogTruncated := len(rd.backlog) >= models.ExpectDueBacklogLimit

	for devID, cmds := range byDev {
		dev := t.devLocked(devID)
		prio := func(c models.Command) int {
			w := &window{cmd: c}
			w.policy, _ = models.ExpectPolicyFor(c)
			return t.priorityLocked(w, start)
		}
		sort.SliceStable(cmds, func(i, j int) bool {
			pi, pj := prio(cmds[i]), prio(cmds[j])
			if pi != pj {
				return pi < pj
			}
			if pi == 2 {
				return finishedAt(&cmds[i]).Before(finishedAt(&cmds[j])) // due: oldest first
			}
			return cmds[i].CreatedAt.After(cmds[j].CreatedAt) // newest first
		})
		keep := map[string]bool{}
		for i, c := range cmds {
			if i >= models.ExpectPerDeviceWindowCap {
				dev.overflow++
				continue
			}
			keep[c.ID] = true
			selfFrom, selfTo := selfUpdateVersionsOf(c)
			snap := trimCommand(c)
			snap.Normalize()
			if w := dev.windows[c.ID]; w != nil {
				if !snap.UpdatedAt.Before(w.cmd.UpdatedAt) {
					w.cmd = snap
				}
				continue
			}
			pol, _ := models.ExpectPolicyFor(c)
			w := &window{cmd: snap, policy: pol, selfFrom: selfFrom, selfTo: selfTo}
			if pol.Class == models.WindowAgent {
				w.fromVersion = dev.agentVersion
			}
			dev.windows[c.ID] = w
		}
		if dev.overflow > 0 && (dev.overflowEventAt.IsZero() || start.Sub(dev.overflowEventAt) >= overflowEventEvery) {
			dev.overflowEventAt = start
			*overflowEvents = append(*overflowEvents, overflowEvent{deviceID: devID,
				message: overflowMessage(dev.overflow, models.SafeName(dev.hostname))})
			t.log.Warn("expected-activity windows exceed the tracking limit", "device_id", devID, "windows", dev.overflow)
		}
		dev.overflow = 0
	}

	// Cached windows the database no longer returns.
	for devID, dev := range t.devs {
		for id, w := range dev.windows {
			if added[id] {
				continue
			}
			if !w.addedAt.IsZero() && !w.addedAt.Before(start) {
				continue // created by the hook while this reconcile was reading
			}
			if _, ok := loaded[id]; ok {
				// Loaded but not kept: beyond the per-device cap. It stays pending
				// in the database, so its backstop is deferred, not skipped.
				delete(dev.windows, id)
				continue
			}
			if openTruncated || backlogTruncated {
				continue // the lists were cut: absence proves nothing
			}
			if _, closed := t.closedRecent[id]; closed {
				delete(dev.windows, id)
				continue
			}
			*deleted = append(*deleted, deletedWindow{devID, id})
		}
	}

	// Powered-off rows: the database holds only valid ones.
	po := make(map[string]models.PoweredOffState, len(rd.poweredOff))
	for _, p := range rd.poweredOff {
		po[p.DeviceID] = p
	}
	for id, dev := range t.devs {
		if p, ok := po[id]; ok {
			c := p
			dev.poweredOff = &c
		} else {
			dev.poweredOff = nil
		}
	}

	// Open device escalations of devices that are not live (labels).
	for _, dev := range t.devs {
		dev.escalation = nil
	}
	for _, e := range rd.escs {
		dev := t.devs[e.DeviceID]
		if dev == nil || dev.online {
			continue
		}
		c, ok := loaded[e.CommandID]
		if !ok {
			c, ok = rd.escCmds[e.CommandID]
		}
		oe := &openEscalation{commandID: e.CommandID, at: e.CreatedAt, reason: models.EscalationReasonNotBack, origin: c.Origin}
		if ok {
			pol, _ := models.ExpectPolicyFor(c)
			w := &window{cmd: trimCommand(c), policy: pol}
			oe.action, oe.actor, oe.startedAt = c.Action, t.actorPhraseLocked(w), startOf(&c)
			oe.bound = models.FormatDurationShort(pol.PostTerminal)
			switch s := c.State(); {
			case pol.SelfUpdate:
				// A self-update record escalates as not back, or as failed; its
				// not-confirmed outcome (timed_out) is also "not back" (AD-009).
				oe.bound = models.FormatDurationShort(models.ExpectSelfUpdateBound)
				oe.fromVersion, oe.toVersion = selfUpdateVersionsOf(c)
				if s == models.CommandFailed {
					oe.reason = models.EscalationReasonFailed
				}
			case pol.Reinstall:
				// A reinstall record escalates as not back; its not-confirmed outcome
				// (timed_out) is also "not back" (EXPECT-008 AD-010).
				oe.bound = models.FormatDurationShort(models.ExpectReinstallBound)
			case s == models.CommandFailed:
				oe.reason = models.EscalationReasonFailed
			case s == models.CommandTimedOut:
				oe.reason = models.EscalationReasonTimedOut
			case s.IsActive():
				oe.reason = models.EscalationReasonNoResult
			}
		}
		dev.escalation = oe
	}

	// Devices that left the database leave the cache.
	if rd.devices != nil || t.opt.Devices != nil {
		for id, dev := range t.devs {
			if seen[id] {
				continue
			}
			keepDev := false
			for _, w := range dev.windows {
				if !w.addedAt.IsZero() && !w.addedAt.Before(start) {
					keepDev = true
				}
			}
			if !keepDev {
				delete(t.devs, id)
			}
		}
	}
	return acts
}

// StartPublisher launches the publisher workers (Run does this too). Tests
// that drive Tick themselves start only the workers.
func (t *Tracker) StartPublisher(ctx context.Context) { t.pub.start(ctx) }

// Run starts the publisher workers, the reconcile goroutine and the one-second
// evaluation loop. It returns when ctx is cancelled.
func (t *Tracker) Run(ctx context.Context) {
	t.pub.start(ctx)
	go func() {
		ticker := time.NewTicker(reconcileEvery)
		defer ticker.Stop()
		for {
			if err := t.Reconcile(ctx); err != nil && ctx.Err() == nil {
				t.log.Debug("expected-activity reconcile retry pending", "error", err.Error())
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.Tick(ctx)
		case <-t.wake:
			t.Tick(ctx)
		}
	}
}

// hostnameOf is the SafeName of a device's hostname ("device" when unknown).
func hostnameOf(d devSnap) string {
	if h := models.SafeName(strings.TrimSpace(d.hostname)); h != "" {
		return h
	}
	return "device"
}

// Stats is the size of the tracker's memory, for the bounded-growth tests and
// diagnostics (NFR-004): it is bounded by the open windows plus one entry per
// device, and the ids of recently closed windows for closedRecentTTL.
type Stats struct {
	Devices      int
	Windows      int
	ClosedRecent int
}

// Stats returns the current memory footprint.
func (t *Tracker) Stats() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Stats{Devices: len(t.devs), ClosedRecent: len(t.closedRecent)}
	for _, d := range t.devs {
		s.Windows += len(d.windows)
	}
	return s
}
