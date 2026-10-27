package expect_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/commands"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/expect"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The EXPECT-001 test harness (ADD section 8, "H"): a real Tracker, a real
// commands.Service (so the observer hook is exercised) and a real
// events.Generator over the testutil mocks, a fake clock, a recording
// dispatcher and broadcaster, and `harness.restart()`.
//
// The generator is the real one (WP-2). containerDied, containerOOM,
// deviceOffline and register are thin drivers: they call the generator the way
// the handlers and the offline detector do, then read what it stored and
// dispatched. oracleSpy sits between the generator and the tracker only to
// remember the tracker's error and answer, which the generator swallows.

// ---------------------------------------------------------------------------
// Clock
// ---------------------------------------------------------------------------

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// baseTime is 10:00:00 UTC on the story date; tests name times relative to it.
var baseTime = time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)

// ---------------------------------------------------------------------------
// Fakes for the tracker's collaborators
// ---------------------------------------------------------------------------

type pushRec struct {
	Event    models.Event
	Rule     *models.AlertRule
	Hostname string
}

// fakeEscalator is the tracker's expect.Escalator: the real generator does the
// work (rule lookup, event, notification); this wrapper only records each push
// and lets a test hold PublishEscalation open (block must honour ctx).
type fakeEscalator struct {
	h *harness

	mu       sync.Mutex
	pushes   []pushRec
	prepared int
	block    func(ctx context.Context) error
}

func (f *fakeEscalator) PrepareEscalation(ctx context.Context, req models.EscalationRequest) (models.PreparedEscalation, error) {
	f.mu.Lock()
	f.prepared++
	f.mu.Unlock()
	f.h.gen.InvalidateRulesCache() // tests edit the rule repository directly
	return f.h.gen.PrepareEscalation(ctx, req)
}

func (f *fakeEscalator) PublishEscalation(ctx context.Context, evt models.Event, rule *models.AlertRule, host string) error {
	if f.block != nil {
		if err := f.block(ctx); err != nil {
			return err
		}
	}
	f.mu.Lock()
	f.pushes = append(f.pushes, pushRec{Event: evt, Rule: rule, Hostname: host})
	f.mu.Unlock()
	return f.h.gen.PublishEscalation(ctx, evt, rule, host)
}

func (f *fakeEscalator) Pushes() []pushRec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]pushRec(nil), f.pushes...)
}

// oracleSpy is the generator's events.ExpectedActivity: it forwards to the
// current tracker and remembers the last error and answer of each call, which
// the generator itself swallows (it alerts when the tracker cannot decide).
type oracleSpy struct {
	mu       sync.Mutex
	tr       *expect.Tracker
	coverErr error
	offErr   error
	offKind  models.OfflineKind
	offOK    bool
	note     *models.OnlineNote
}

func (s *oracleSpy) set(tr *expect.Tracker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tr = tr
}

func (s *oracleSpy) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.coverErr, s.offErr, s.offKind, s.offOK, s.note = nil, nil, "", false, nil
}

func (s *oracleSpy) current() *expect.Tracker {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tr
}

func (s *oracleSpy) CoverContainer(deviceID string, keys []string, at time.Time) (models.Coverage, bool, error) {
	cov, ok, err := s.current().CoverContainer(deviceID, keys, at)
	s.mu.Lock()
	s.coverErr = err
	s.mu.Unlock()
	return cov, ok, err
}

func (s *oracleSpy) CoverOffline(ctx context.Context, deviceID string, at time.Time) (models.OfflineCoverage, bool, error) {
	cov, ok, err := s.current().CoverOffline(ctx, deviceID, at)
	s.mu.Lock()
	s.offErr, s.offOK = err, ok
	if ok {
		s.offKind = cov.Kind
	}
	s.mu.Unlock()
	return cov, ok, err
}

func (s *oracleSpy) NoteOnline(ctx context.Context, deviceID string, at time.Time) *models.OnlineNote {
	n := s.current().NoteOnline(ctx, deviceID, at)
	s.mu.Lock()
	s.note = n
	s.mu.Unlock()
	return n
}

type fakeBroadcaster struct {
	mu         sync.Mutex
	events     []models.Event
	activities []activityRec
}

type activityRec struct {
	DeviceID string
	Activity *models.DeviceActivity
}

func (b *fakeBroadcaster) BroadcastEvent(e *models.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, *e)
}

func (b *fakeBroadcaster) BroadcastActivity(deviceID string, a *models.DeviceActivity) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.activities = append(b.activities, activityRec{deviceID, a})
}

func (b *fakeBroadcaster) Activities() []activityRec {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]activityRec(nil), b.activities...)
}

// lockedBuffer is a log sink that is safe for concurrent writers.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// ---------------------------------------------------------------------------
// The harness
// ---------------------------------------------------------------------------

type harness struct {
	t     *testing.T
	clock *fakeClock

	cmds    *testutil.MockCommandRepo
	events  *testutil.MockEventRepo
	devices *testutil.MockDeviceRepo
	repo    *testutil.MockExpectedActivityRepo
	tele    *testutil.MockTelemetryRepo
	pol     *testutil.MockAutoUpdateRepo
	rules   *testutil.MockAlertRuleRepo
	agents  *testutil.FakeAgents
	esc     *fakeEscalator
	gen     *events.Generator
	disp    *testutil.MockDispatcher
	spy     *oracleSpy
	bc      *fakeBroadcaster
	logs    *lockedBuffer

	newest string
	tr     *expect.Tracker
	svc    *commands.Service

	lastReconcile time.Time
	// noResolver leaves the tracker without a self-update resolver (C-4).
	noResolver bool
	// noReinstallResolver leaves the tracker without a ReinstallResolver (EXPECT-008
	// stuck-record case).
	noReinstallResolver bool

	// alerts are the occurrences that went the "today" path: stored as a non-expected
	// event and, when the rule notifies, dispatched by the generator.
	alerts []alertRec
}

type alertRec struct {
	Type      models.EventType
	Device    string
	Container string
	Message   string
	Notified  bool
}

type hOpt func(*hConfig)

type hConfig struct {
	noLoad              bool
	graceEnd            time.Time
	noRules             bool
	queueSize           int
	workers             int
	inline              bool
	notConnect          bool
	deviceSetup         func(h *harness)
	noResolver          bool
	noReinstallResolver bool
}

func withoutLoad() hOpt             { return func(c *hConfig) { c.noLoad = true } }
func withoutRules() hOpt            { return func(c *hConfig) { c.noRules = true } }
func withGraceEnd(t time.Time) hOpt { return func(c *hConfig) { c.graceEnd = t } }
func withAgentsOffline() hOpt       { return func(c *hConfig) { c.notConnect = true } }
func withAsyncPublisher(q, w int) hOpt {
	return func(c *hConfig) { c.inline = false; c.queueSize = q; c.workers = w }
}
func withDevices(f func(h *harness)) hOpt { return func(c *hConfig) { c.deviceSetup = f } }

// withoutSelfUpdateResolver leaves the tracker without a SelfUpdateResolver
// (EXPECT-002 C-4: the stuck-record case).
func withoutSelfUpdateResolver() hOpt { return func(c *hConfig) { c.noResolver = true } }

// withoutReinstallResolver leaves the tracker without a ReinstallResolver
// (EXPECT-008: the stuck-record case).
func withoutReinstallResolver() hOpt { return func(c *hConfig) { c.noReinstallResolver = true } }

var allDevices = []struct{ id, host string }{{"hawaii", "hawaii"}, {"vermont", "vermont"}, {"picups", "PiCUPS"}}

func newHarness(t *testing.T, opts ...hOpt) *harness {
	t.Helper()
	cfg := hConfig{inline: true}
	for _, o := range opts {
		o(&cfg)
	}
	h := &harness{
		t: t, clock: &fakeClock{t: baseTime},
		cmds: testutil.NewMockCommandRepo(), events: testutil.NewMockEventRepo(), devices: testutil.NewMockDeviceRepo(),
		tele: testutil.NewMockTelemetryRepo(), pol: testutil.NewMockAutoUpdateRepo(), rules: testutil.NewMockAlertRuleRepo(),
		bc: &fakeBroadcaster{}, logs: &lockedBuffer{}, newest: "2.78.0",
	}
	h.repo = testutil.NewMockExpectedActivityRepo(h.cmds, h.events, h.devices)
	h.cmds.LinkEvents(h.events)   // the rate-cap event of a self-update start lands here
	h.cmds.LinkDevices(h.devices) // a reinstall notice reads the device status
	h.noReinstallResolver = cfg.noReinstallResolver
	h.noResolver = cfg.noResolver
	h.esc = &fakeEscalator{h: h}
	h.disp = testutil.NewMockDispatcher()
	h.spy = &oracleSpy{}
	hub := websocket.NewHub()
	go hub.Run() // drains the generator's broadcasts; no client is connected
	h.gen = events.NewGenerator(h.events, hub, h.rules, h.disp, h.cmds)
	h.gen.SetClock(h.clock.Now)
	h.gen.SetExpectedActivity(h.spy)
	var ids []string
	for _, d := range allDevices {
		hb := baseTime
		h.devices.Devices[d.id] = &models.Device{ID: d.id, Hostname: d.host, Status: models.DeviceStatusOnline, LastHeartbeat: &hb, AgentVersion: "2.77.0"}
		ids = append(ids, d.id)
	}
	if !cfg.notConnect {
		h.agents = testutil.NewFakeAgents(ids...)
	} else {
		h.agents = testutil.NewFakeAgents()
	}
	if !cfg.noRules {
		// Like the rules the UI creates: the operator and threshold make an event of
		// value 1 match. Cooldowns are 0: the cooldown path is not what these tests
		// pin (generator_expect_test.go covers it with an injected clock).
		_ = h.rules.Create(context.Background(), &models.AlertRule{Name: "container died", Enabled: true, Metric: "container_died",
			Operator: ">", Threshold: 0, Severity: "critical", Notify: true})
		_ = h.rules.Create(context.Background(), &models.AlertRule{Name: "container oom", Enabled: true, Metric: "container_oom",
			Operator: ">", Threshold: 0, Severity: "critical", Notify: true})
		_ = h.rules.Create(context.Background(), &models.AlertRule{Name: "device offline", Enabled: true, Metric: "device_offline",
			Operator: ">", Threshold: 0, Severity: "critical", Notify: true})
	}
	if cfg.deviceSetup != nil {
		cfg.deviceSetup(h)
	}
	h.build(cfg)
	if !cfg.noLoad {
		require.NoError(t, h.tr.Load(context.Background()), "initial Load")
		h.lastReconcile = h.clock.Now()
	}
	return h
}

func (h *harness) build(cfg hConfig) {
	h.tr = expect.New(expect.Options{
		Repo: h.repo, Devices: h.devices, Policies: h.pol, Telemetry: h.tele, Commands: h.cmds,
		Escalator: h.esc, Broadcaster: h.bc, Now: h.clock.Now, GraceEnd: cfg.graceEnd,
		NewestAgentVersion: func() string { return h.newest },
		Logger:             slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		QueueSize:          cfg.queueSize, Workers: cfg.workers, InlinePublish: cfg.inline,
	})
	h.spy.set(h.tr)
	h.svc = commands.New(commands.Options{
		Commands: h.cmds, Policies: h.pol, Telemetry: h.tele, Agents: h.agents, Observer: h.tr, Now: h.clock.Now,
		SelfUpdates: h.cmds,
		Reinstalls:  h.cmds,
		Logger:      slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if !h.noResolver {
		h.tr.SetSelfUpdateResolver(h.svc) // before Load, like server.Start
	}
	if !h.noReinstallResolver {
		h.tr.SetReinstallResolver(h.svc) // before Load, like server.Start
	}
}

// restart simulates a server restart: a new Tracker (and Service) over the same
// durable state, with the given grace end, loaded from the database.
func (h *harness) restart(graceEnd time.Time) {
	h.t.Helper()
	h.build(hConfig{inline: true, graceEnd: graceEnd})
	require.NoError(h.t, h.tr.Load(context.Background()), "Load after restart")
	h.lastReconcile = h.clock.Now()
}

func (h *harness) ctx() context.Context { return context.Background() }

// at sets the clock to hh:mm:ss on the story date and runs the loop.
func (h *harness) at(hms string) {
	h.t.Helper()
	var hh, mm, ss int
	_, err := fmt.Sscanf(hms, "%d:%d:%d", &hh, &mm, &ss)
	require.NoError(h.t, err, "time %q", hms)
	h.clock.Set(time.Date(2026, 10, 26, hh, mm, ss, 0, time.UTC))
	h.settle()
}

// advance moves the clock and runs the loop once (and a reconcile when 30 s
// have passed, like the reconcile goroutine would).
func (h *harness) advance(d time.Duration) {
	h.t.Helper()
	h.clock.Advance(d)
	h.settle()
}

func (h *harness) settle() {
	h.t.Helper()
	if h.clock.Now().Sub(h.lastReconcile) >= 30*time.Second {
		require.NoError(h.t, h.tr.Reconcile(h.ctx()), "reconcile")
		h.lastReconcile = h.clock.Now()
	}
	h.tr.Tick(h.ctx())
}

// tick runs one loop pass without moving the clock.
func (h *harness) tick() { h.tr.Tick(h.ctx()) }

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

type dispatchOpt func(*commands.DispatchRequest)

func byActor(name string) dispatchOpt {
	return func(r *commands.DispatchRequest) { r.ActorName = name }
}
func withKeys(keys ...string) dispatchOpt {
	return func(r *commands.DispatchRequest) { r.TargetKeys = keys }
}
func withOrigin(origin string, policyID int) dispatchOpt {
	return func(r *commands.DispatchRequest) {
		r.Origin = origin
		if policyID > 0 {
			r.PolicyID = &policyID
		}
	}
}
func withBulk(n int) dispatchOpt {
	return func(r *commands.DispatchRequest) {
		ids := make([]interface{}, n)
		for i := range ids {
			ids[i] = fmt.Sprintf("cid-%03d", i)
		}
		r.Params = map[string]interface{}{"container_ids": ids}
	}
}

// dispatch creates a command through the real service. With the agent
// connected it is claimed at once (dispatched); otherwise it stays queued.
func (h *harness) dispatch(device, action string, opts ...dispatchOpt) models.Command {
	h.t.Helper()
	req := commands.DispatchRequest{DeviceID: device, Action: action, ActorKind: "operator", ActorName: "ryan"}
	for _, o := range opts {
		o(&req)
	}
	res, err := h.svc.Dispatch(h.ctx(), req)
	require.NoError(h.t, err, "dispatch %s", action)
	require.NotNil(h.t, res.Command, "dispatch %s must create a command (conflict: %+v)", action, res.Conflict)
	return *res.Command
}

func (h *harness) report(device string, r models.CommandReport) {
	h.t.Helper()
	_, err := h.svc.HandleAgentReport(h.ctx(), device, r)
	require.NoError(h.t, err)
}

func (h *harness) step(c models.Command, step string) {
	h.t.Helper()
	h.report(c.DeviceID, models.CommandReport{Kind: models.ReportKindProgress, CommandID: c.ID, State: "running", Step: step})
}

func (h *harness) succeed(c models.Command) {
	h.t.Helper()
	h.report(c.DeviceID, models.CommandReport{Kind: models.ReportKindResult, CommandID: c.ID, Result: &models.CommandResult{
		CommandID: c.ID, Status: models.ResultStatusSuccess, Outcome: models.OutcomeSucceeded, Message: "ok"}})
}

func (h *harness) fail(c models.Command) {
	h.t.Helper()
	h.report(c.DeviceID, models.CommandReport{Kind: models.ReportKindResult, CommandID: c.ID, Result: &models.CommandResult{
		CommandID: c.ID, Status: models.ResultStatusError, Outcome: models.OutcomeFailed, Message: "boom"}})
}

// sweep runs the command sweeper (moves overdue commands to timed_out).
func (h *harness) sweep() { h.svc.Sweep(h.ctx()) }

func (h *harness) command(id string) models.Command {
	h.t.Helper()
	c, ok := h.cmds.Get(id)
	require.True(h.t, ok, "command %s must exist", id)
	return c
}

// seed stores a command row directly (a durable state the tracker must pick up
// at Load or reconcile, with no hook call).
func (h *harness) seed(c models.Command) models.Command {
	h.t.Helper()
	if c.Params == nil {
		c.Params = map[string]interface{}{}
	}
	if c.Origin == "" {
		c.Origin = models.OriginUser
	}
	if c.ActorName == "" {
		c.ActorName = "ryan"
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = h.clock.Now()
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = c.CreatedAt
	}
	if c.ExecDeadlineS == 0 {
		c.ExecDeadlineS = models.ExecDeadlineSeconds(c.Action, 1)
	}
	if c.ExpiresAt.IsZero() {
		c.ExpiresAt = c.CreatedAt.Add(time.Hour)
	}
	cp := c
	h.cmds.Seed(&cp)
	return c
}

func tp(t time.Time) *time.Time { return &t }

// ---------------------------------------------------------------------------
// The generator drivers
// ---------------------------------------------------------------------------

func shortID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			b.WriteRune(r)
		}
		if b.Len() == 12 {
			break
		}
	}
	return b.String()
}

func (h *harness) hostname(device string) string { return h.devices.Devices[device].Hostname }

// recordAlerts runs a generator call and files every container_died,
// container_oom or device_offline event it stored that is not an expected-
// activity info event as an alert, notified when the dispatcher got it. These
// are the occurrences that went "the today path".
func (h *harness) recordAlerts(call func()) {
	h.t.Helper()
	var lastID int64
	for _, e := range h.allEvents() {
		if e.ID > lastID {
			lastID = e.ID
		}
	}
	dispatched := len(h.disp.Alerts)
	call()
	fresh := h.allEvents()
	sort.Slice(fresh, func(i, j int) bool { return fresh[i].ID < fresh[j].ID })
	for _, e := range fresh {
		if e.ID <= lastID || e.CommandAttr != "" {
			continue
		}
		switch e.Type {
		case models.EventContainerDied, models.EventContainerOOM, models.EventDeviceOffline:
		default:
			continue
		}
		notified := false
		for _, a := range h.disp.Alerts[dispatched:] {
			if a.Event != nil && a.Event.ID == e.ID {
				notified = true
			}
		}
		h.alerts = append(h.alerts, alertRec{Type: e.Type, Device: e.DeviceID, Container: e.ContainerName, Message: e.Message, Notified: notified})
	}
}

type deathResult struct {
	Suppressed bool
	Err        error
}

// dockerEvent drives the generator with a container event the way
// ReceiveDockerEvent does: the target keys are filled in by the handler.
func (h *harness) dockerEvent(action, device, name string, keys []string) deathResult {
	h.t.Helper()
	if keys == nil {
		keys = []string{models.TargetKeyContainer(name)}
	}
	h.spy.reset()
	h.gen.InvalidateRulesCache()
	before := len(h.eventsOf(models.EventContainerDied, models.CommandAttrExpected))
	h.recordAlerts(func() {
		h.gen.CheckDockerEvent(h.ctx(), device, h.hostname(device), &models.DockerEvent{
			ContainerName: name, ContainerID: shortID(name), Action: action, TargetKeys: keys,
		})
	})
	h.spy.mu.Lock()
	defer h.spy.mu.Unlock()
	return deathResult{Suppressed: len(h.eventsOf(models.EventContainerDied, models.CommandAttrExpected)) > before, Err: h.spy.coverErr}
}

// containerDied is CheckDockerEvent "die": exclusion first (FR-015, X-9), then
// the tracker's answer.
func (h *harness) containerDied(device, name string, keys ...string) deathResult {
	h.t.Helper()
	return h.dockerEvent("die", device, name, keys)
}

// containerOOM is CheckDockerEvent "oom": never suppressed (FR-011).
func (h *harness) containerOOM(device, name string, keys ...string) {
	h.t.Helper()
	h.dockerEvent("oom", device, name, keys)
}

// containerStarted stores a container_started event (the agent saw it start).
func (h *harness) containerStarted(device, name string, at time.Time) {
	h.t.Helper()
	e := models.Event{DeviceID: device, Type: models.EventContainerStart, Severity: models.SeverityInfo,
		Message: "Container " + name + " started", ContainerName: name, CreatedAt: at}
	require.NoError(h.t, h.events.Create(h.ctx(), &e))
}

// snapshot stores a telemetry snapshot listing the given containers as running.
func (h *harness) snapshot(device string, at time.Time, running ...string) {
	h.t.Helper()
	var cs []models.ContainerInfo
	for _, n := range running {
		cs = append(cs, models.ContainerInfo{Name: n, State: "running"})
	}
	snap := models.TelemetrySnapshot{DeviceID: device, Timestamp: at, Data: models.FullTelemetryData{Docker: &models.DockerInfo{Containers: cs}}}
	require.NoError(h.t, h.tele.StoreSnapshot(h.ctx(), &snap))
}

type offlineResult struct {
	Covered bool
	Kind    models.OfflineKind
	Err     error
}

// deviceOffline is the offline detector's pass for one device at offlineAt
// (default: now): the DB status flips to offline, the generator's
// DeviceOfflineAt asks the tracker, then the worker reports the new statuses
// (ObserveDevices).
func (h *harness) deviceOffline(device string, at ...time.Time) offlineResult {
	h.t.Helper()
	offlineAt := h.clock.Now()
	if len(at) > 0 {
		offlineAt = at[0]
	}
	d := h.devices.Devices[device]
	hb := offlineAt.Add(-models.OfflineHeartbeatThreshold)
	d.LastHeartbeat = &hb
	require.NoError(h.t, h.devices.SetStatus(h.ctx(), device, models.DeviceStatusOffline))
	h.spy.reset()
	h.gen.InvalidateRulesCache()
	h.recordAlerts(func() { h.gen.DeviceOfflineAt(h.ctx(), device, d.Hostname, &hb) })
	h.spy.mu.Lock()
	res := offlineResult{Covered: h.spy.offOK && h.spy.offErr == nil, Kind: h.spy.offKind, Err: h.spy.offErr}
	h.spy.mu.Unlock()
	h.observe()
	return res
}

// register is a device (re)registering: the DB status turns online, the
// handler tells the tracker (NoteRegistration, before DeviceOnline), and the
// generator's DeviceOnline stores the online event when the device was not live
// before. It returns the note the tracker gave the generator.
func (h *harness) register(device string) *models.OnlineNote {
	h.t.Helper()
	d := h.devices.Devices[device]
	wasLive := models.DeviceIsLive(d.Status)
	now := h.clock.Now()
	d.LastHeartbeat = tp(now)
	require.NoError(h.t, h.devices.SetStatus(h.ctx(), device, models.DeviceStatusOnline))
	h.tr.NoteRegistration(device, now)
	if wasLive {
		return nil
	}
	h.spy.reset()
	h.gen.DeviceOnline(h.ctx(), device, d.Hostname)
	h.spy.mu.Lock()
	defer h.spy.mu.Unlock()
	return h.spy.note
}

// heartbeat is a silent return: the DB status turns online and nothing tells
// the tracker (UpdateHeartbeatTime).
func (h *harness) heartbeat(device string) {
	h.t.Helper()
	h.devices.Devices[device].LastHeartbeat = tp(h.clock.Now())
	require.NoError(h.t, h.devices.SetStatus(h.ctx(), device, models.DeviceStatusOnline))
}

// observe is the offline worker reporting the device list to the tracker.
func (h *harness) observe() {
	h.t.Helper()
	list, err := h.devices.List(h.ctx())
	require.NoError(h.t, err)
	h.tr.ObserveDevices(list, h.clock.Now())
}

// ---------------------------------------------------------------------------
// Assertions and queries
// ---------------------------------------------------------------------------

func (h *harness) allEvents() []models.Event {
	evts, _ := h.events.ListAll(h.ctx(), 100000, 0)
	return append([]models.Event(nil), evts...)
}

func (h *harness) eventsOf(typ models.EventType, attr string) []models.Event {
	var out []models.Event
	for _, e := range h.allEvents() {
		if e.Type == typ && e.CommandAttr == attr {
			out = append(out, e)
		}
	}
	return out
}

func (h *harness) escalations() []models.Event {
	var out []models.Event
	for _, e := range h.allEvents() {
		if e.CommandAttr == models.CommandAttrEscalation {
			out = append(out, e)
		}
	}
	return out
}

func (h *harness) escalationsOf(typ models.EventType) []models.Event {
	var out []models.Event
	for _, e := range h.escalations() {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// notifications are the pushes the test double saw (the today path plus the
// escalation pushes).
func (h *harness) notifications() []string {
	var out []string
	for _, a := range h.alerts {
		if a.Notified {
			out = append(out, a.Message)
		}
	}
	for _, p := range h.esc.Pushes() {
		out = append(out, p.Event.Message)
	}
	sort.Strings(out)
	return out
}

func (h *harness) alertsOf(typ models.EventType) []alertRec {
	var out []alertRec
	for _, a := range h.alerts {
		if a.Type == typ {
			out = append(out, a)
		}
	}
	return out
}

func (h *harness) warningEvents(typ models.EventType) []models.Event {
	var out []models.Event
	for _, e := range h.allEvents() {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func (h *harness) activity(device string) *models.DeviceActivity {
	h.t.Helper()
	for _, a := range h.tr.Activities().Activities {
		if a.DeviceID == device {
			c := a
			return &c
		}
	}
	return nil
}

func (h *harness) local(t time.Time) string { return t.Local().Format("15:04") }

func requireNoAlerts(t *testing.T, h *harness, msg string) {
	t.Helper()
	assert.Empty(t, h.alerts, msg)
	assert.Empty(t, h.notifications(), msg)
}

// ---------------------------------------------------------------------------
// EXPECT-002 helpers. They mirror the order the handlers use: an agent update
// event goes through commands.Service.NoteAgentUpdateEvent first and is then
// stored with its link (ReceiveAgentEvent); a registration refreshes the
// heartbeat, tells the tracker (NoteRegistration), then the service
// (NoteSelfUpdateRegistration), then the generator (DeviceOnline); the offline
// detector pass flips stale devices and reports the statuses to the tracker.
// ---------------------------------------------------------------------------

const (
	suFrom = "2.78.0"
	suTo   = "2.80.0"
)

func startMessage(from, to string) string {
	return fmt.Sprintf("Agent update started: %s \u2192 %s", from, to)
}

// agentEvent is ReceiveAgentEvent for an agent_update_* event of the device:
// recognition and attribution first, then the event is stored as the agent sent
// it plus the link. It returns the link.
func (h *harness) agentEvent(device string, typ models.EventType, sev models.EventSeverity, msg string) commands.AgentUpdateEventLink {
	h.t.Helper()
	now := h.clock.Now()
	link := h.svc.NoteAgentUpdateEvent(h.ctx(), commands.AgentUpdateEventInput{
		DeviceID: device, Hostname: h.hostname(device), Type: typ, Message: msg, At: now,
	})
	e := models.Event{DeviceID: device, Type: typ, Severity: sev, Message: msg, CreatedAt: now,
		CommandID: link.CommandID, CommandAttr: link.CommandAttr}
	require.NoError(h.t, h.events.Create(h.ctx(), &e))
	return link
}

// selfStart is the agent's "Agent update started: 2.78.0 -> 2.80.0".
func (h *harness) selfStart(device string) commands.AgentUpdateEventLink {
	h.t.Helper()
	return h.selfStartVersions(device, suFrom, suTo)
}

func (h *harness) selfStartVersions(device, from, to string) commands.AgentUpdateEventLink {
	h.t.Helper()
	return h.agentEvent(device, models.EventAgentUpdateStarted, models.SeverityInfo, startMessage(from, to))
}

func (h *harness) agentCompleted(device, msg string) commands.AgentUpdateEventLink {
	h.t.Helper()
	return h.agentEvent(device, models.EventAgentUpdateCompleted, models.SeverityInfo, msg)
}

func (h *harness) agentFailed(device, msg string) commands.AgentUpdateEventLink {
	h.t.Helper()
	return h.agentEvent(device, models.EventAgentUpdateFailed, models.SeverityWarning, msg)
}

// registerOn is RegisterDevice for an existing device reporting agent version:
// the heartbeat is refreshed (X-20), then NoteRegistration, then
// NoteSelfUpdateRegistration, then DeviceOnline when the device was not live.
func (h *harness) registerOn(device, version string) *models.OnlineNote {
	h.t.Helper()
	d := h.devices.Devices[device]
	wasLive := models.DeviceIsLive(d.Status)
	now := h.clock.Now()
	d.LastHeartbeat = tp(now)
	d.AgentVersion = version
	require.NoError(h.t, h.devices.SetStatus(h.ctx(), device, models.DeviceStatusOnline))
	h.tr.NoteRegistration(device, now)
	h.svc.NoteSelfUpdateRegistration(h.ctx(), device, d.Hostname, version, now)
	h.svc.NoteReinstallRegistration(h.ctx(), device, now)
	if wasLive {
		return nil
	}
	h.spy.reset()
	h.gen.DeviceOnline(h.ctx(), device, d.Hostname)
	h.spy.mu.Lock()
	defer h.spy.mu.Unlock()
	return h.spy.note
}

// heartbeatOn is a heartbeat that reports an agent version.
func (h *harness) heartbeatOn(device, version string) {
	h.t.Helper()
	h.devices.Devices[device].AgentVersion = version
	h.heartbeat(device)
}

// detectorPass is one pass of the offline detector at the current time: every
// live device whose heartbeat is older than the threshold goes offline (and the
// generator is asked about the transition), then the tracker is told the
// statuses.
func (h *harness) detectorPass() {
	h.t.Helper()
	now := h.clock.Now()
	list, err := h.devices.List(h.ctx())
	require.NoError(h.t, err)
	threshold := now.Add(-models.OfflineHeartbeatThreshold)
	for i := range list {
		d := &list[i]
		if !models.DeviceIsLive(d.Status) || d.LastHeartbeat == nil || !d.LastHeartbeat.Before(threshold) {
			continue
		}
		require.NoError(h.t, h.devices.SetStatus(h.ctx(), d.ID, models.DeviceStatusOffline))
		d.Status = models.DeviceStatusOffline
		h.spy.reset()
		h.gen.InvalidateRulesCache()
		hb := *d.LastHeartbeat
		h.recordAlerts(func() { h.gen.DeviceOfflineAt(h.ctx(), d.ID, d.Hostname, &hb) })
	}
	h.tr.ObserveDevices(list, now)
}

// reinstallNotice is the handler side of an authenticated agent_reinstall_started
// notice for the device (EXPECT-008 AD-002): the service records it at the
// current time and the cap event, if any, is already stored by the repository.
func (h *harness) reinstallNotice(device string) models.ReinstallDisposition {
	h.t.Helper()
	res := h.svc.NoteReinstallNotice(h.ctx(), commands.ReinstallNoticeInput{
		DeviceID: device, Hostname: h.hostname(device), At: h.clock.Now(),
	})
	return res.Disposition
}

// reinstallRecords returns the device's installer rows, oldest first.
func (h *harness) reinstallRecords(device string) []models.Command {
	var out []models.Command
	for _, c := range h.cmds.All() {
		if c.DeviceID == device && c.Origin == models.OriginInstaller {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// reinstallRecord is the device's only installer row.
func (h *harness) reinstallRecord(device string) models.Command {
	h.t.Helper()
	rows := h.reinstallRecords(device)
	require.Len(h.t, rows, 1, "exactly one reinstall record for %s", device)
	return rows[0]
}

// selfRecords returns the device's self-update rows, oldest first.
func (h *harness) selfRecords(device string) []models.Command {
	var out []models.Command
	for _, c := range h.cmds.All() {
		if c.DeviceID == device && c.Origin == models.OriginAgentSelfUpdate {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// selfRecord is the device's only self-update row.
func (h *harness) selfRecord(device string) models.Command {
	h.t.Helper()
	rows := h.selfRecords(device)
	require.Len(h.t, rows, 1, "exactly one self-update record for %s", device)
	return rows[0]
}

// eventsLinked returns the stored events of one type linked to a command.
func (h *harness) eventsLinked(typ models.EventType, commandID string) []models.Event {
	var out []models.Event
	for _, e := range h.allEvents() {
		if e.Type == typ && e.CommandID == commandID {
			out = append(out, e)
		}
	}
	return out
}

// logLevelLines counts the log lines at a level ("WARN", "INFO", "ERROR").
func (h *harness) logLevelLines(level string) int {
	return strings.Count(h.logs.String(), "level="+level)
}
