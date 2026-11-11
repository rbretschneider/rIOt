package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// EXPECT-007 AU: the update events' structured fields, the attempt ID and its
// persistence, and the bounded retry (ADD section 8). The server is a fake
// http.RoundTripper, so there is no socket; the clock and the sleeper are fakes;
// every Agent has a fake runner, a fake maintenance client and its own state
// directory. Nothing here starts a process, performs an update or sleeps in
// real time.

// ---------------------------------------------------------------------------
// Fake server
// ---------------------------------------------------------------------------

// ex7Answer is what the fake server does with one event POST.
type ex7Answer struct {
	status int           // 0 means 200
	err    error         // a transport error
	block  bool          // hold the request until the wire is released or its context ends
	body   string        // response body
	after  func()        // runs after the request was recorded, before it is answered
	retry  time.Duration // Retry-After seconds, if > 0
}

// ex7Event is one recorded event POST.
type ex7Event struct {
	evt models.AgentEvent
	raw map[string]any
	// body is the exact request body, to compare retries byte for byte.
	body []byte
	at   time.Time
	n    int // 1-based count of this event type
}

// ex7Wire is the fake riot-server behind the agent's HTTPClient.
type ex7Wire struct {
	clk *fakeClock

	mu       sync.Mutex
	check    UpdateCheckResponse
	events   []ex7Event
	counts   map[models.EventType]int
	script   func(t models.EventType, n int) ex7Answer
	onEvent  func(e ex7Event) // runs inside the round trip, before the answer
	release  chan struct{}
	inFlight atomic.Int32
	maxIn    atomic.Int32
}

func newEX7Wire(t *testing.T, clk *fakeClock) *ex7Wire {
	t.Helper()
	w := &ex7Wire{clk: clk, counts: map[models.EventType]int{}, release: make(chan struct{})}
	t.Cleanup(w.free)
	return w
}

// free releases every blocked request. Safe to call twice.
func (w *ex7Wire) free() {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-w.release:
	default:
		close(w.release)
	}
}

func (w *ex7Wire) RoundTrip(r *http.Request) (*http.Response, error) {
	respond := func(status int, body string, hdr http.Header) *http.Response {
		if hdr == nil {
			hdr = http.Header{}
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: hdr, Request: r}
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/update/check") {
		raw, _ := json.Marshal(w.check)
		return respond(200, string(raw), nil), nil
	}
	if !strings.HasSuffix(r.URL.Path, "/events") {
		return respond(404, "not found", nil), nil
	}
	body, _ := io.ReadAll(r.Body)
	var evt models.AgentEvent
	_ = json.Unmarshal(body, &evt)
	raw := map[string]any{}
	_ = json.Unmarshal(body, &raw)

	w.mu.Lock()
	w.counts[evt.Type]++
	rec := ex7Event{evt: evt, raw: raw, body: body, at: w.clk.Now(), n: w.counts[evt.Type]}
	w.events = append(w.events, rec)
	script, hook, release := w.script, w.onEvent, w.release
	w.mu.Unlock()

	cur := w.inFlight.Add(1)
	for {
		m := w.maxIn.Load()
		if cur <= m || w.maxIn.CompareAndSwap(m, cur) {
			break
		}
	}
	defer w.inFlight.Add(-1)

	if hook != nil {
		hook(rec)
	}
	ans := ex7Answer{}
	if script != nil {
		ans = script(evt.Type, rec.n)
	}
	if ans.after != nil {
		ans.after()
	}
	if ans.block {
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	if ans.err != nil {
		return nil, ans.err
	}
	status := ans.status
	if status == 0 {
		status = 200
	}
	hdr := http.Header{}
	if ans.retry > 0 {
		hdr.Set("Retry-After", fmt.Sprintf("%d", int(ans.retry.Seconds())))
	}
	return respond(status, ans.body, hdr), nil
}

// sent returns the recorded events of the given types (all when none).
func (w *ex7Wire) sent(types ...models.EventType) []ex7Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []ex7Event
	for _, e := range w.events {
		if len(types) == 0 || slices.Contains(types, e.evt.Type) {
			out = append(out, e)
		}
	}
	return out
}

func (w *ex7Wire) count(t models.EventType) int { return len(w.sent(t)) }

// ---------------------------------------------------------------------------
// Fake agent
// ---------------------------------------------------------------------------

// ex7Runner is the fake command runner: it records every call and starts
// nothing. unit is what the unit-state query answers.
type ex7Runner struct {
	mu    sync.Mutex
	calls [][]string
	unit  string
}

func (r *ex7Runner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string{name}, args...))
	if name == "/usr/bin/systemctl" {
		return []byte(r.unit + "\n"), nil
	}
	return nil, nil
}

func (r *ex7Runner) setUnit(state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unit = state
}

type ex7Agent struct {
	*Agent
	wire   *ex7Wire
	clk    *fakeClock
	runner *ex7Runner
	dir    string

	pmu    sync.Mutex
	pauses []time.Duration
}

func (a *ex7Agent) pausesTaken() []time.Duration {
	a.pmu.Lock()
	defer a.pmu.Unlock()
	return append([]time.Duration(nil), a.pauses...)
}

// newEX7Agent builds an Agent with the default retry policy over a fake wire.
// Two agents built over the same dir model a restart: they share the state
// files and nothing else.
func newEX7Agent(t *testing.T, version, dir string) *ex7Agent {
	t.Helper()
	clk := newFakeClock()
	wire := newEX7Wire(t, clk)
	wire.check = UpdateCheckResponse{
		LatestVersion: "2.87.0", UpdateAvail: true,
		Assets:       map[string]string{platformSuffix(testGoarm()): "https://example.invalid/bin"},
		ChecksumURL:  "https://example.invalid/checksums.txt",
		SignatureURL: "https://example.invalid/checksums.txt.sig",
	}
	cfg := &Config{}
	cfg.Agent.DeviceID = testDeviceID
	runner := &ex7Runner{unit: "active"}
	ag := &ex7Agent{wire: wire, clk: clk, runner: runner, dir: dir}
	a := &Agent{
		config:  cfg,
		version: version,
		client:  &HTTPClient{baseURL: "http://server.test", apiKey: "test-key", client: &http.Client{Transport: wire}},
		runner:  runner.run,
		clk:     clk,
		locks:   newTargetLocks(),
		maintenanceClient: &maintenanceClient{
			present: func() bool { return false },
			wait:    func(ctx context.Context) error { return ctx.Err() },
		},
		updateRetry: defaultUpdateEventRetry(),
	}
	a.pause = func(ctx context.Context, d time.Duration) bool {
		ag.pmu.Lock()
		ag.pauses = append(ag.pauses, d)
		ag.pmu.Unlock()
		clk.Advance(d)
		return ctx.Err() == nil
	}
	a.cmdCtx, a.cmdCancel = context.WithCancel(context.Background())
	a.inflight = newInflightStore(filepath.Join(dir, "inflight-commands.json"), newFaultFS())
	ag.Agent = a
	t.Cleanup(func() {
		a.cmdCancel()
		wire.free()
		a.cmdWG.Wait()
	})
	return ag
}

// hostLockHeld reports whether the host lock is taken (a second taker is refused).
func (a *ex7Agent) hostLockHeld() bool {
	_, _, ok := a.locks.TryLockAll([]string{models.TargetKeyHost}, "ex7-probe", models.ActionAgentUpdate)
	if ok {
		a.locks.Unlock([]string{models.TargetKeyHost}, "ex7-probe")
	}
	return !ok
}

// ---------------------------------------------------------------------------
// Log recorder
// ---------------------------------------------------------------------------

type ex7Logs struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (h *ex7Logs) Enabled(context.Context, slog.Level) bool { return true }
func (h *ex7Logs) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recs = append(h.recs, r.Clone())
	return nil
}
func (h *ex7Logs) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *ex7Logs) WithGroup(string) slog.Handler      { return h }

func captureEX7Logs(t *testing.T) *ex7Logs {
	t.Helper()
	h := &ex7Logs{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

// atLeast returns the records at or above lvl whose message satisfies keep.
func (h *ex7Logs) atLeast(lvl slog.Level, keep func(msg string) bool) []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []slog.Record
	for _, r := range h.recs {
		if r.Level >= lvl && (keep == nil || keep(r.Message)) {
			out = append(out, r)
		}
	}
	return out
}

func recordAttrs(r slog.Record) map[string]any {
	out := map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value.Any()
		return true
	})
	return out
}

const eventNotDelivered = "agent update event not delivered"

func isNotDelivered(msg string) bool { return msg == eventNotDelivered }

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

var attemptIDShape = regexp.MustCompile(`^[0-9a-f]{32}$`)

const (
	ex7Started   = models.EventAgentUpdateStarted
	ex7Completed = models.EventAgentUpdateCompleted
	ex7Failed    = models.EventAgentUpdateFailed
	ex7Digest    = "abababababababababababababababababababababababababababababababab"
	ex7CmdID     = "0b5e6a1c-2d3f-4a5b-8c7d-9e0f1a2b3c4d"
)

// runSelfUpdate runs one self-update attempt with a fake apply and restart.
func runSelfUpdate(ag *ex7Agent, apply error) (applied int) {
	ag.runUpdateCheck(context.Background(), func(context.Context, string, string, string, string) error {
		applied++
		return apply
	}, func() {})
	return applied
}

// ---------------------------------------------------------------------------
// AC-011 .. AC-014: structured fields, attempt ID, message text
// ---------------------------------------------------------------------------

func TestEX7_AC011_SelfUpdateEventsCarryAttemptAndVersions(t *testing.T) {
	t.Run("started and completed", func(t *testing.T) {
		ag := newEX7Agent(t, "2.86.0", t.TempDir())
		require.Equal(t, 1, runSelfUpdate(ag, nil))

		started := ag.wire.sent(ex7Started)
		completed := ag.wire.sent(ex7Completed)
		require.Len(t, started, 1, "[AC-011] one started event")
		require.Len(t, completed, 1, "[AC-011] one completed event")
		s, c := started[0].evt, completed[0].evt
		assert.Regexp(t, attemptIDShape, s.AttemptID, "[AC-011] the start carries a 128-bit hex attempt ID")
		assert.Equal(t, s.AttemptID, c.AttemptID, "[AC-011] the completion carries the same attempt ID")
		assert.Equal(t, "2.86.0", s.FromVersion, "[AC-011] from version")
		assert.Equal(t, "2.87.0", s.ToVersion, "[AC-011] to version")
		assert.Equal(t, "2.86.0", c.FromVersion, "[AC-011] the completion carries the attempt's from version")
		assert.Equal(t, "2.87.0", c.ToVersion, "[AC-011] the completion carries the attempt's to version")
		assert.Empty(t, s.CommandID, "[AC-011] a self-update never carries a command reference")
		assert.Empty(t, c.CommandID, "[AC-011] a self-update never carries a command reference")
	})
	t.Run("failed", func(t *testing.T) {
		ag := newEX7Agent(t, "2.86.0", t.TempDir())
		runSelfUpdate(ag, errors.New("download: connection reset"))

		started, failed := ag.wire.sent(ex7Started), ag.wire.sent(ex7Failed)
		require.Len(t, started, 1)
		require.Len(t, failed, 1, "[AC-011] one failed event")
		assert.Equal(t, started[0].evt.AttemptID, failed[0].evt.AttemptID, "[AC-011] the failure carries the attempt's ID")
		assert.Equal(t, "2.86.0", failed[0].evt.FromVersion)
		assert.Equal(t, "2.87.0", failed[0].evt.ToVersion)
	})
	t.Run("agent_update_available is unchanged", func(t *testing.T) {
		ag := newEX7Agent(t, "2.86.0", t.TempDir())
		runSelfUpdate(ag, nil)
		avail := ag.wire.sent(models.EventAgentUpdateAvail)
		require.Len(t, avail, 1)
		assert.ElementsMatch(t, []string{"type", "severity", "message"}, keysOf(avail[0].raw),
			"[FR-037] the available event carries no new field")
	})
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestEX7_AC012_AttemptIDUniqueAndShaped(t *testing.T) {
	t.Run("two self-update attempts", func(t *testing.T) {
		ag := newEX7Agent(t, "2.86.0", t.TempDir())
		runSelfUpdate(ag, nil)
		runSelfUpdate(ag, nil)
		started := ag.wire.sent(ex7Started)
		require.Len(t, started, 2)
		assert.NotEqual(t, started[0].evt.AttemptID, started[1].evt.AttemptID, "[AC-012] two attempts get different IDs")
		for _, s := range started {
			assert.GreaterOrEqual(t, len(s.evt.AttemptID), 16, "[AC-012] at least 16 bytes")
			assert.LessOrEqual(t, len(s.evt.AttemptID), 64, "[AC-012] at most 64 bytes")
			assert.True(t, models.ValidRefToken(s.evt.AttemptID), "[AC-012] letters, digits and hyphens only")
		}
	})
	t.Run("no collisions in 1000 attempts", func(t *testing.T) {
		seen := map[string]bool{}
		for i := 0; i < 1000; i++ {
			id := newUpdateAttempt("2.86.0", "2.87.0").ID
			require.Regexp(t, attemptIDShape, id)
			require.False(t, seen[id], "[AC-012] attempt IDs must not repeat")
			seen[id] = true
		}
	})
}

func TestEX7_AC012_RestartedInstanceReusesHandoffAttempt(t *testing.T) {
	dir := t.TempDir()
	first := newEX7Agent(t, "2.86.0", dir)
	att := newUpdateAttempt("2.86.0", "2.87.0")
	ctx := withUpdateAttempt(context.Background(), att)

	// The first process persists the hand-off before the apply launch.
	p := &updatePipeline{}
	first.bindHandoff(ctx, p)
	at := first.clk.Now()
	require.NoError(t, p.beforeApply(ex7Digest, at), "[AC-012] the hand-off is persisted")
	var onDisk updateHandoff
	found, err := first.signedHandoffFile().Load(&onDisk)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, att.ID, onDisk.AttemptID, "[AC-012] the attempt ID is in the hand-off file")
	assert.Equal(t, "2.86.0", onDisk.FromVersion)
	assert.Equal(t, "2.87.0", onDisk.ToVersion)
	assert.Empty(t, onDisk.CommandID)

	// A second instance (the restarted process, now running 2.87.0) finishes it.
	second := newEX7Agent(t, "2.87.0", dir)
	second.updateStatusReader = func() (maintenance.UpdateStatus, error) {
		return maintenance.UpdateStatus{State: "installed", SHA256: ex7Digest, UpdatedAt: at.Add(time.Second)}, nil
	}
	second.updateDigestReader = func() (string, error) { return ex7Digest, nil }
	second.recoverAutomaticSignedHandoff()
	second.cmdWG.Wait()

	completed := second.wire.sent(ex7Completed)
	require.Len(t, completed, 1, "[AC-012] the restarted process sends the completion")
	assert.Equal(t, att.ID, completed[0].evt.AttemptID, "[AC-012] the completion carries the first attempt's ID")
	assert.Equal(t, "2.86.0", completed[0].evt.FromVersion, "[AC-012] from is the attempt's, not the running version")
	assert.Equal(t, "2.87.0", completed[0].evt.ToVersion)
	assert.Empty(t, completed[0].evt.CommandID)
}

func TestEX7_AC013_OldHandoffYieldsNoReference(t *testing.T) {
	// A hand-off as an agent before EXPECT-007 wrote it: no attempt, no versions.
	type oldHandoff struct {
		SHA256      string    `json:"sha256"`
		RequestedAt time.Time `json:"requested_at"`
	}
	requested := newFakeClock().Now()

	check := func(t *testing.T, ev ex7Event) {
		t.Helper()
		assert.Empty(t, ev.evt.AttemptID, "[AC-013] no attempt reference")
		assert.Empty(t, ev.evt.CommandID, "[AC-013] no command reference")
		assert.Empty(t, ev.evt.FromVersion, "[AC-013] no structured from version")
		assert.Empty(t, ev.evt.ToVersion, "[AC-013] no structured to version: the running version is never invented")
		assert.ElementsMatch(t, []string{"type", "severity", "message"}, keysOf(ev.raw), "[AC-013] the payload is exactly an old agent's")
	}

	t.Run("recoverAutomaticSignedHandoff", func(t *testing.T) {
		dir := t.TempDir()
		ag := newEX7Agent(t, "2.87.0", dir)
		require.NoError(t, ag.signedHandoffFile().Save(oldHandoff{SHA256: ex7Digest, RequestedAt: requested}))
		ag.updateStatusReader = func() (maintenance.UpdateStatus, error) {
			return maintenance.UpdateStatus{State: "installed", SHA256: ex7Digest, UpdatedAt: requested.Add(time.Second)}, nil
		}
		ag.updateDigestReader = func() (string, error) { return ex7Digest, nil }

		ag.recoverAutomaticSignedHandoff()
		ag.cmdWG.Wait()

		completed := ag.wire.sent(ex7Completed)
		require.Len(t, completed, 1)
		check(t, completed[0])
	})
	t.Run("watchSignedHandoff of the old process", func(t *testing.T) {
		ag := newEX7Agent(t, "2.86.0", t.TempDir())
		ag.runner.setUnit("inactive") // the helper stopped without a matching status
		ag.updateStatusReader = func() (maintenance.UpdateStatus, error) { return maintenance.UpdateStatus{}, errors.New("none") }

		ag.watchSignedHandoff(updateHandoff{SHA256: ex7Digest, RequestedAt: requested})
		ag.cmdWG.Wait()

		failed := ag.wire.sent(ex7Failed)
		require.Len(t, failed, 1)
		check(t, failed[0])
	})
}

func TestEX7_AC014_MessagesByteForByte(t *testing.T) {
	const arrow = "→"
	t.Run("self-update path", func(t *testing.T) {
		ag := newEX7Agent(t, "2.86.0", t.TempDir())
		runSelfUpdate(ag, nil)
		runSelfUpdate(ag, errors.New("boom"))
		got := messagesOf(ag.wire.sent(ex7Started, ex7Completed, ex7Failed))
		assert.Equal(t, []string{
			"Agent update started: 2.86.0 " + arrow + " 2.87.0",
			"Agent updated to 2.87.0",
			"Agent update started: 2.86.0 " + arrow + " 2.87.0",
			"Agent update failed: boom",
		}, got, "[AC-014] the self-update path's texts are byte-for-byte today's")
	})
	t.Run("command path", func(t *testing.T) {
		for name, applyErr := range map[string]error{"completed": nil, "failed": errors.New("boom")} {
			t.Run(name, func(t *testing.T) {
				ag := newEX7Agent(t, "2.86.0", t.TempDir())
				ag.updateApply = func(context.Context, string, string, string, string) error { return applyErr }
				ag.restartHook = func() {}
				ag.handleTriggerUpdate(context.WithValue(context.Background(), updateCommandIDKey{}, ex7CmdID))
				ag.cmdWG.Wait()
				want := "Agent updated to 2.87.0"
				if applyErr != nil {
					want = "Agent update failed: boom"
				}
				assert.ElementsMatch(t, []string{"Agent update started: 2.86.0 " + arrow + " 2.87.0", want},
					messagesOf(ag.wire.sent(ex7Started, ex7Completed, ex7Failed)),
					"[AC-014] the command path's texts are byte-for-byte today's")
			})
		}
	})
	t.Run("signed completion and failure texts", func(t *testing.T) {
		requested := newFakeClock().Now()
		cases := []struct {
			name   string
			status maintenance.UpdateStatus
			digest string
			unit   string
			want   string
		}{
			{"helper error", maintenance.UpdateStatus{State: "error", Message: "kaput"}, ex7Digest, "active", "signed agent update error: kaput"},
			{"helper deferred", maintenance.UpdateStatus{State: "deferred", Message: "later"}, ex7Digest, "active", "signed agent update deferred: later"},
			{"installed and verified", maintenance.UpdateStatus{State: "installed"}, ex7Digest, "active", "signed installation completion verified against running executable"},
			{"installed, digest differs", maintenance.UpdateStatus{State: "installed"}, "cdcd", "active", "installed digest does not match the running executable; completion remains unconfirmed"},
			{"helper stopped", maintenance.UpdateStatus{}, ex7Digest, "inactive", "signed update helper stopped without verified completion"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				ag := newEX7Agent(t, "2.87.0", t.TempDir())
				ag.runner.setUnit(tc.unit)
				st := tc.status
				if st.State != "" {
					st.SHA256, st.UpdatedAt = ex7Digest, requested.Add(time.Second)
				}
				ag.updateStatusReader = func() (maintenance.UpdateStatus, error) {
					if st.State == "" {
						return maintenance.UpdateStatus{}, errors.New("none")
					}
					return st, nil
				}
				ag.updateDigestReader = func() (string, error) { return tc.digest, nil }
				ag.watchSignedHandoff(updateHandoff{SHA256: ex7Digest, RequestedAt: requested, AttemptID: "9f1c0123456789abcdef0123456789ab", FromVersion: "2.86.0", ToVersion: "2.87.0"})
				ag.cmdWG.Wait()
				got := messagesOf(ag.wire.sent(ex7Completed, ex7Failed))
				require.Len(t, got, 1)
				assert.Equal(t, tc.want, got[0], "[AC-014] the signed path's text is byte-for-byte today's")
			})
		}
	})
}

func messagesOf(events []ex7Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.evt.Message
	}
	return out
}

func TestEX7_AC027_AgentEventJSONKeys(t *testing.T) {
	base := []string{"type", "severity", "message"}
	self := append(append([]string(nil), base...), "from_version", "to_version", "attempt_id")
	cmd := append(append([]string(nil), base...), "from_version", "to_version", "command_id")

	t.Run("self-update path", func(t *testing.T) {
		ag := newEX7Agent(t, "2.86.0", t.TempDir())
		runSelfUpdate(ag, nil)
		runSelfUpdate(ag, errors.New("boom"))
		for _, e := range ag.wire.sent(ex7Started, ex7Completed, ex7Failed) {
			assert.ElementsMatch(t, self, keysOf(e.raw), "[AC-027] %s on the self-update path", e.evt.Type)
		}
		for _, e := range ag.wire.sent(models.EventAgentUpdateAvail) {
			assert.ElementsMatch(t, base, keysOf(e.raw), "[AC-027] available keeps the old shape")
		}
	})
	t.Run("command path", func(t *testing.T) {
		for _, applyErr := range []error{nil, errors.New("boom")} {
			ag := newEX7Agent(t, "2.86.0", t.TempDir())
			ag.updateApply = func(context.Context, string, string, string, string) error { return applyErr }
			ag.restartHook = func() {}
			ag.handleTriggerUpdate(context.WithValue(context.Background(), updateCommandIDKey{}, ex7CmdID))
			ag.cmdWG.Wait()
			for _, e := range ag.wire.sent(ex7Started, ex7Completed, ex7Failed) {
				assert.ElementsMatch(t, cmd, keysOf(e.raw), "[AC-027] %s on the command path", e.evt.Type)
			}
		}
	})
}

func TestEX7_FR012_CommandPathCarriesCommandRefOnly(t *testing.T) {
	for name, applyErr := range map[string]error{"completed": nil, "failed": errors.New("download failed")} {
		t.Run(name, func(t *testing.T) {
			ag := newEX7Agent(t, "2.86.0", t.TempDir())
			ag.updateApply = func(context.Context, string, string, string, string) error { return applyErr }
			ag.restartHook = func() {}
			status, _ := ag.handleTriggerUpdate(context.WithValue(context.Background(), updateCommandIDKey{}, ex7CmdID))
			ag.cmdWG.Wait()
			if applyErr == nil {
				assert.Equal(t, "success", status)
			}
			events := ag.wire.sent(ex7Started, ex7Completed, ex7Failed)
			require.Len(t, events, 2, "[FR-012] started plus completed or failed")
			for _, e := range events {
				assert.Equal(t, ex7CmdID, e.evt.CommandID, "[FR-012] %s carries the command reference", e.evt.Type)
				assert.Empty(t, e.evt.AttemptID, "[FR-012] and never an attempt reference")
				assert.Equal(t, "2.86.0", e.evt.FromVersion)
				assert.Equal(t, "2.87.0", e.evt.ToVersion)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AC-030 .. AC-033: bounded retry
// ---------------------------------------------------------------------------

var errRefused = errors.New("dial tcp: connect: connection refused")

func TestEX7_AC030_StartRetriedApplyWaitsLockHeld(t *testing.T) {
	ag := newEX7Agent(t, "2.86.0", t.TempDir())
	ag.wire.script = func(typ models.EventType, n int) ex7Answer {
		if typ == ex7Started && n <= 2 {
			return ex7Answer{err: errRefused}
		}
		return ex7Answer{}
	}
	var lockedAtAttempt []bool
	ag.wire.onEvent = func(e ex7Event) {
		if e.evt.Type == ex7Started {
			lockedAtAttempt = append(lockedAtAttempt, ag.hostLockHeld())
		}
	}
	var startsAtApply int
	var lockAtApply bool
	var firstAt time.Time
	ag.runUpdateCheck(context.Background(), func(context.Context, string, string, string, string) error {
		startsAtApply = ag.wire.count(ex7Started)
		lockAtApply = ag.hostLockHeld()
		return errUpdateScheduled // the signed hand-off: the lock stays held
	}, func() {})

	starts := ag.wire.sent(ex7Started)
	require.Len(t, starts, 3, "[AC-030] exactly three attempts")
	firstAt = starts[0].at
	assert.Equal(t, 5*time.Second, starts[1].at.Sub(firstAt), "[AC-030] the second attempt follows the schedule")
	assert.Equal(t, 15*time.Second, starts[2].at.Sub(firstAt), "[AC-030] the third attempt is 15 s after the first")
	assert.Equal(t, starts[0].body, starts[1].body, "[AC-030] a resend carries identical content")
	assert.Equal(t, starts[0].body, starts[2].body, "[AC-030] a resend carries identical content")
	assert.Equal(t, 3, startsAtApply, "[AC-030] the apply step ran only after the third attempt was answered 200")
	assert.True(t, lockAtApply, "[AC-030] the host lock is held when the update is applied")
	assert.Equal(t, []bool{true, true, true}, lockedAtAttempt, "[AC-030] the host lock is held throughout the retries")
	assert.True(t, ag.hostLockHeld(), "[AC-030] a signed hand-off keeps the host lock")
}

func TestEX7_AC031_BudgetBoundedOneWarn(t *testing.T) {
	logs := captureEX7Logs(t)
	ag := newEX7Agent(t, "2.86.0", t.TempDir())
	ag.wire.script = func(typ models.EventType, _ int) ex7Answer {
		if typ == ex7Started {
			return ex7Answer{err: context.DeadlineExceeded}
		}
		return ex7Answer{}
	}
	var appliedAt time.Time
	var first time.Time
	ag.runUpdateCheck(context.Background(), func(context.Context, string, string, string, string) error {
		appliedAt = ag.clk.Now()
		return errUpdateScheduled
	}, func() {})

	starts := ag.wire.sent(ex7Started)
	require.LessOrEqual(t, len(starts), 5, "[AC-031] at most 5 attempts")
	require.Len(t, starts, 5, "[AC-031] a start that always times out uses its five attempts")
	first = starts[0].at
	for i, s := range starts {
		assert.LessOrEqual(t, s.at.Sub(first), models.UpdateEventRetryBudget, "[AC-031] attempt %d must not start more than 90 s after the first", i+1)
	}
	assert.LessOrEqual(t, appliedAt.Sub(first), models.UpdateEventRetryBudget+models.UpdateEventAttemptTimeout,
		"[AC-031] the apply step runs no later than 90 s plus one request timeout after the first attempt")
	warns := logs.atLeast(slog.LevelWarn, isNotDelivered)
	require.Len(t, warns, 1, "[AC-031] exactly one Warn for the abandoned event")
	assert.Equal(t, map[string]any{"type": "agent_update_started", "attempts": int64(5)}, normalizeAttrs(recordAttrs(warns[0])),
		"[AC-031] the Warn carries exactly the event type and the attempt count")
	assert.Zero(t, ag.wire.count(ex7Completed)+ag.wire.count(ex7Failed), "[AC-031] the update itself proceeded (apply returned scheduled)")
}

func TestEX7_AC032_NoRetryOn400(t *testing.T) {
	ag := newEX7Agent(t, "2.86.0", t.TempDir())
	ag.wire.script = func(typ models.EventType, _ int) ex7Answer {
		if typ == ex7Started {
			return ex7Answer{status: 400, body: "bad request"}
		}
		return ex7Answer{}
	}
	applied := 0
	logs := captureEX7Logs(t)
	ag.runUpdateCheck(context.Background(), func(context.Context, string, string, string, string) error {
		applied++
		assert.Equal(t, 1, ag.wire.count(ex7Started), "[AC-032] exactly one attempt before the apply")
		return errUpdateScheduled
	}, func() {})

	assert.Equal(t, 1, ag.wire.count(ex7Started), "[AC-032] a 400 is final: exactly one attempt")
	assert.Equal(t, 1, applied, "[AC-032] the update proceeds")
	assert.Empty(t, ag.pausesTaken(), "[AC-032] the update proceeds at once: nothing waited")
	assert.Empty(t, logs.atLeast(slog.LevelWarn, isNotDelivered), "[AC-032] a rejection is not a lost event: no Warn")
}

func TestEX7_AC033_CommandPathNeverWaits(t *testing.T) {
	ag := newEX7Agent(t, "2.86.0", t.TempDir())
	ag.wire.script = func(models.EventType, int) ex7Answer { return ex7Answer{block: true} }
	var applied atomic.Bool
	ag.updateApply = func(context.Context, string, string, string, string) error {
		applied.Store(true)
		return nil
	}
	restarted := make(chan struct{}, 1)
	ag.restartHook = func() { restarted <- struct{}{} }

	done := make(chan struct{})
	var status string
	go func() {
		status, _ = ag.handleTriggerUpdate(context.WithValue(context.Background(), updateCommandIDKey{}, ex7CmdID))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("[AC-033] handleTriggerUpdate waited for a blocked update event")
	}
	assert.True(t, applied.Load(), "[AC-033] the apply step ran without waiting for the event")
	assert.Equal(t, "success", status, "[AC-033] the command's result is not delayed")
	require.Eventually(t, func() bool { return ag.wire.inFlight.Load() >= 1 }, 2*time.Second, time.Millisecond,
		"the blocked start is still in flight")
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("[AC-033] the restart hook was not reached")
	}
}

func TestEX7_AC033_BusySlotAttemptOffCallerGoroutine(t *testing.T) {
	ag := newEX7Agent(t, "2.86.0", t.TempDir())
	ag.wire.script = func(models.EventType, int) ex7Answer { return ex7Answer{block: true, status: 500} }
	idx := updateEventIndex(ex7Started)
	ag.updateEventSlots[idx].Store(true) // the retrying sender of this type is busy

	evt := buildUpdateEvent(ex7Started, models.SeverityInfo, "Agent update started: 2.86.0 → 2.87.0", ex7CmdID, updateAttempt{From: "2.86.0", To: "2.87.0"})
	returned := make(chan struct{})
	go func() {
		ag.sendUpdateEvent(context.Background(), evt, deliverAsync)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("[C-4 b] the busy-slot single attempt ran on the caller's goroutine")
	}
	require.Eventually(t, func() bool { return ag.wire.inFlight.Load() == 1 }, 2*time.Second, time.Millisecond,
		"[C-4 b] the single attempt is in flight on its own goroutine after the caller returned")
	assert.True(t, ag.updateEventSingles[idx].Load(), "[C-4 b] the single-shot slot is held while it runs")

	// A second event of the same type finds both slots busy and is dropped.
	ag.sendUpdateEvent(context.Background(), evt, deliverAsync)
	assert.Equal(t, 1, ag.wire.count(ex7Started), "[C-4 b] a third sender is dropped, not queued")

	ag.wire.free()
	ag.cmdWG.Wait()
	assert.Equal(t, 1, ag.wire.count(ex7Started), "[C-4 b] a busy-slot event gets exactly one attempt and no retries")
	assert.False(t, ag.updateEventSingles[idx].Load(), "[C-4 b] the single-shot slot is released")
	assert.True(t, ag.updateEventSlots[idx].Load(), "[C-4 b] the retrying slot belongs to its own holder, untouched")
}

// ---------------------------------------------------------------------------
// Supporting: classification, StatusError, bounds
// ---------------------------------------------------------------------------

func TestEX7_FR031_RetryClassification(t *testing.T) {
	alive := context.Background()
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	timeoutCtx, cancelT := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancelT()
	<-timeoutCtx.Done()

	cases := []struct {
		name   string
		parent context.Context
		err    error
		want   eventVerdict
	}{
		{"transport error", alive, errRefused, evRetry},
		{"per-attempt timeout while the agent is alive", alive, context.DeadlineExceeded, evRetry},
		{"HTTP 500", alive, &StatusError{StatusCode: 500}, evRetry},
		{"HTTP 503", alive, &StatusError{StatusCode: 503}, evRetry},
		{"HTTP 429", alive, &StatusError{StatusCode: 429}, evRetry},
		{"HTTP 400", alive, &StatusError{StatusCode: 400}, evFinal},
		{"HTTP 401", alive, &StatusError{StatusCode: 401}, evFinal},
		{"HTTP 404", alive, &StatusError{StatusCode: 404}, evFinal},
		{"HTTP 409", alive, &StatusError{StatusCode: 409}, evFinal},
		{"2xx", alive, nil, evAccepted},
		{"no server client", alive, errNoServerClient, evFinal},
		{"the agent's own context cancelled", dead, context.Canceled, evCanceled},
		{"the agent's own context cancelled, whatever the attempt returned", dead, errRefused, evCanceled},
		{"the agent's own context past its deadline", timeoutCtx, context.DeadlineExceeded, evCanceled},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, classifyEventSendErr(tc.parent, tc.err), "[FR-031] %s", tc.name)
	}

	// The same table end to end through the HTTP client: attempts made per status.
	for status, wantAttempts := range map[int]int{500: 5, 503: 5, 429: 5, 400: 1, 401: 1, 404: 1, 409: 1, 200: 1, 204: 1} {
		t.Run(fmt.Sprintf("status %d", status), func(t *testing.T) {
			ag := newEX7Agent(t, "2.86.0", t.TempDir())
			ag.wire.script = func(models.EventType, int) ex7Answer { return ex7Answer{status: status} }
			evt := buildUpdateEvent(ex7Started, models.SeverityInfo, "m", "", updateAttempt{ID: "9f1c0123456789abcdef0123456789ab", From: "2.86.0", To: "2.87.0"})
			ag.sendUpdateEvent(context.Background(), evt, deliverAwait)
			assert.Equal(t, wantAttempts, ag.wire.count(ex7Started), "[FR-031] attempts for HTTP %d", status)
		})
	}
}

func TestEX7_FR031_ReportEventReturnsStatusError(t *testing.T) {
	clk := newFakeClock()
	wire := newEX7Wire(t, clk)
	c := &HTTPClient{baseURL: "http://server.test", apiKey: "k", client: &http.Client{Transport: wire}}
	evt := &models.AgentEvent{Type: ex7Started, Severity: models.SeverityInfo, Message: "m"}

	wire.script = func(models.EventType, int) ex7Answer {
		return ex7Answer{status: 503, body: strings.Repeat("x", 64<<10), retry: 7 * time.Second}
	}
	err := c.ReportEvent(context.Background(), testDeviceID, evt)
	var se *StatusError
	require.ErrorAs(t, err, &se, "[FR-031] a status >= 400 is a *StatusError")
	assert.Equal(t, 503, se.StatusCode)
	assert.Equal(t, 7*time.Second, se.RetryAfter)
	assert.LessOrEqual(t, len(se.Body), maxErrorBodyBytes, "[FR-031] the error body read is bounded")

	for _, status := range []int{200, 204} {
		wire.script = func(models.EventType, int) ex7Answer { return ex7Answer{status: status} }
		assert.NoError(t, c.ReportEvent(context.Background(), testDeviceID, evt), "[FR-031] %d is success", status)
	}
}

func TestEX7_NFR003_AtMostOneRetrierPerType(t *testing.T) {
	ag := newEX7Agent(t, "2.86.0", t.TempDir())
	ag.wire.script = func(models.EventType, int) ex7Answer { return ex7Answer{block: true} }
	types := []models.EventType{ex7Started, ex7Completed, ex7Failed}
	for _, typ := range types {
		for i := 0; i < 3; i++ {
			evt := buildUpdateEvent(typ, models.SeverityInfo, "m", ex7CmdID, updateAttempt{From: "2.86.0", To: "2.87.0"})
			ag.sendUpdateEvent(context.Background(), evt, deliverAsync)
		}
	}
	require.Eventually(t, func() bool { return ag.wire.inFlight.Load() == 6 }, 2*time.Second, time.Millisecond,
		"[NFR-003] three event types with a retrier and a single-shot sender each: six in flight")
	assert.EqualValues(t, 6, ag.wire.maxIn.Load(), "[NFR-003] never more than six sender goroutines in flight")
	for _, typ := range types {
		assert.Equal(t, 2, ag.wire.count(typ), "[NFR-003] %s: the third concurrent event is dropped", typ)
	}
	ag.wire.free()
	ag.cmdWG.Wait()
	for i := 0; i < updateEventTypes; i++ {
		assert.False(t, ag.updateEventSlots[i].Load(), "[NFR-003] every retry slot is released")
		assert.False(t, ag.updateEventSingles[i].Load(), "[NFR-003] every single-shot slot is released")
	}
}

func TestEX7_NFR003_NilPolicyIsSingleAttempt(t *testing.T) {
	ag := newEX7Agent(t, "2.86.0", t.TempDir())
	ag.updateRetry = nil
	ag.wire.script = func(models.EventType, int) ex7Answer { return ex7Answer{status: 503} }
	logs := captureEX7Logs(t)
	evt := buildUpdateEvent(ex7Started, models.SeverityInfo, "m", "", updateAttempt{ID: "9f1c0123456789abcdef0123456789ab", From: "2.86.0", To: "2.87.0"})

	ag.sendUpdateEvent(context.Background(), evt, deliverAwait)
	ag.sendUpdateEvent(context.Background(), evt, deliverFirstSync)

	assert.Equal(t, 2, ag.wire.count(ex7Started), "[NFR-003] a nil policy is one attempt per call, as before EXPECT-007")
	assert.Empty(t, ag.pausesTaken(), "[NFR-003] a nil policy never waits")
	assert.Empty(t, logs.atLeast(slog.LevelWarn, isNotDelivered), "[NFR-003] and writes no Warn")
}

func TestEX7_NFR003_AgentContextEndStopsRetryNoWarn(t *testing.T) {
	failEveryAttempt := func(ag *ex7Agent, cancelOn func()) {
		ag.wire.script = func(models.EventType, int) ex7Answer {
			return ex7Answer{err: errRefused, after: cancelOn}
		}
	}
	t.Run("background sender", func(t *testing.T) {
		logs := captureEX7Logs(t)
		ag := newEX7Agent(t, "2.86.0", t.TempDir())
		failEveryAttempt(ag, ag.cmdCancel) // the agent's context ends during the first attempt
		evt := buildUpdateEvent(ex7Started, models.SeverityInfo, "m", ex7CmdID, updateAttempt{From: "2.86.0", To: "2.87.0"})

		ag.sendUpdateEvent(context.Background(), evt, deliverAsync)
		ag.cmdWG.Wait() // drains: the sender ended instead of retrying to the end of the budget

		assert.Equal(t, 1, ag.wire.count(ex7Started), "[C-4 a] no retry once the agent's context ended")
		assert.False(t, ag.updateEventSlots[updateEventIndex(ex7Started)].Load(), "[C-4 a] the slot is released at once")
		assert.Empty(t, logs.atLeast(slog.LevelWarn, isNotDelivered), "[C-4 a] a stopping agent is not a delivery failure: no Warn")
		assert.Empty(t, ag.pausesTaken(), "[C-4 a] it did not sleep towards the next attempt")
	})
	t.Run("caller's sender", func(t *testing.T) {
		logs := captureEX7Logs(t)
		ag := newEX7Agent(t, "2.86.0", t.TempDir())
		ctx, cancel := context.WithCancel(context.Background())
		failEveryAttempt(ag, cancel)
		evt := buildUpdateEvent(ex7Started, models.SeverityInfo, "m", "", updateAttempt{ID: "9f1c0123456789abcdef0123456789ab", From: "2.86.0", To: "2.87.0"})

		ag.sendUpdateEvent(ctx, evt, deliverAwait)

		assert.Equal(t, 1, ag.wire.count(ex7Started), "[C-4 a] the awaiting sender stops at once too")
		assert.False(t, ag.updateEventSlots[updateEventIndex(ex7Started)].Load(), "[C-4 a] and releases its slot")
		assert.Empty(t, logs.atLeast(slog.LevelWarn, isNotDelivered), "[C-4 a] with no Warn")
	})
}

func TestEX7_AC035_AgentLogsAndHandoffBounded(t *testing.T) {
	logs := captureEX7Logs(t)
	dir := t.TempDir()
	ag := newEX7Agent(t, "2.86.0", dir)

	// A full attempt: the start is retried once, the hand-off is persisted.
	ag.wire.script = func(typ models.EventType, n int) ex7Answer {
		if typ == ex7Started && n == 1 {
			return ex7Answer{status: 503}
		}
		return ex7Answer{}
	}
	var persisted string
	ag.runUpdateCheck(context.Background(), func(ctx context.Context, _, _, _, _ string) error {
		p := &updatePipeline{}
		ag.bindHandoff(ctx, p)
		require.NoError(t, p.beforeApply(ex7Digest, ag.clk.Now()))
		persisted = filepath.Join(dir, "agent-update-handoff.json")
		return errUpdateScheduled
	}, func() {})

	for _, r := range logs.atLeast(slog.LevelWarn, nil) {
		t.Errorf("[AC-035] the agent wrote a %s line: %q", r.Level, r.Message)
	}
	for _, r := range logs.atLeast(slog.LevelInfo, nil) {
		assert.Contains(t, []string{"update available", "signed agent update accepted; privileged verifier owns installation and restart"},
			r.Message, "[AC-035] no new line above Debug: %q", r.Message)
	}

	info, err := os.Stat(persisted)
	require.NoError(t, err, "[AC-035] the hand-off file exists")
	assert.LessOrEqual(t, info.Size(), int64(512), "[AC-035] the hand-off file is small and bounded")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	handoffFiles := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "agent-update-handoff") {
			handoffFiles++
		}
	}
	assert.Equal(t, 1, handoffFiles, "[AC-035] one hand-off file, no leftovers")
	var h updateHandoff
	raw, _ := os.ReadFile(persisted)
	require.NoError(t, json.Unmarshal(raw, &h))
	assert.LessOrEqual(t, len(h.AttemptID), 64, "[AC-035] the attempt ID is at most 64 bytes")
	assert.LessOrEqual(t, len(h.FromVersion), 32)
	assert.LessOrEqual(t, len(h.ToVersion), 32)
	assert.Equal(t, 2, ag.wire.count(ex7Started), "[AC-035] one retry only")
}

// normalizeAttrs turns typed string values (event types) into plain strings.
func normalizeAttrs(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if s, ok := v.(fmt.Stringer); ok {
			out[k] = s.String()
		} else if et, ok := v.(models.EventType); ok {
			out[k] = string(et)
		} else {
			out[k] = v
		}
	}
	return out
}
