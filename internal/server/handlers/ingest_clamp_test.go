package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clampTestNow is the fixed server receive time used by the clamp tests.
var clampTestNow = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

// newClampTestHandlers returns Handlers whose receive clock is fixed at *clock.
func newClampTestHandlers(t *testing.T, clock *time.Time) (*Handlers, *testutil.MockTelemetryRepo) {
	t.Helper()
	h, telRepo, devRepo := newHeartbeatTestHandlers(t)
	devRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "node-1"}
	h.ingestClock = func() time.Time { return *clock }
	h.clampLog = newClampLogLimiter()
	return h, telRepo
}

func postTelemetry(t *testing.T, h *Handlers, deviceID, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/v1/devices/{id}/telemetry", h.Telemetry)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/"+deviceID+"/telemetry", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func snapshotBody(ts time.Time) string {
	return fmt.Sprintf(`{"timestamp":%q,"data":{}}`, ts.Format(time.RFC3339Nano))
}

func heartbeatBody(ts time.Time) string {
	return fmt.Sprintf(`{"timestamp":%q,"data":{"uptime":100,"cpu_percent":5,"mem_percent":10,"load_avg_1m":0.1,"disk_root_percent":20}}`,
		ts.Format(time.RFC3339Nano))
}

// ---------------------------------------------------------------------------
// Pure clamp rule
// ---------------------------------------------------------------------------

func TestClampFutureTimestamp_AC028_Boundaries(t *testing.T) {
	R := clampTestNow
	require.Equal(t, 5*time.Minute, db.FutureSkewAllowance, "FR-030: allowance is 5 minutes")

	cases := []struct {
		name        string
		ts          time.Time
		want        time.Time
		wantClamped bool
	}{
		{"[AC-028] R+4m is kept as sent", R.Add(4 * time.Minute), R.Add(4 * time.Minute), false},
		{"[AC-028] R+5m exactly is kept (inclusive boundary)", R.Add(5 * time.Minute), R.Add(5 * time.Minute), false},
		{"[AC-028] R+5m+1ns is clamped to R", R.Add(5*time.Minute + time.Nanosecond), R, true},
		{"[AC-028] R+6m is clamped to R", R.Add(6 * time.Minute), R, true},
		{"[AC-025] R+24h is clamped to R", R.Add(24 * time.Hour), R, true},
		{"[AC-027] R-2h is kept as sent", R.Add(-2 * time.Hour), R.Add(-2 * time.Hour), false},
		{"[AC-027] R itself is kept", R, R, false},
		{"zero timestamp defaults to R without being reported as a clamp", time.Time{}, R, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, clamped := clampFutureTimestamp(c.ts, R)
			assert.True(t, got.Equal(c.want), "stored timestamp: got %s want %s", got, c.want)
			assert.Equal(t, c.wantClamped, clamped, "clamped flag")
		})
	}
}

// ---------------------------------------------------------------------------
// Debounced clamp logging
// ---------------------------------------------------------------------------

func TestClampLogLimiter_AC031_DebouncePerDeviceKind(t *testing.T) {
	l := newClampLogLimiter()
	t0 := clampTestNow

	// 100 clamps of one device and kind within the hour: exactly one emit.
	emits := 0
	for i := 0; i < 100; i++ {
		emit, n := l.observe("dev-1", clampKindSnapshot, t0.Add(time.Duration(i)*30*time.Second))
		if emit {
			emits++
			assert.Equal(t, 1, n, "first entry reports one clamp")
		}
	}
	assert.Equal(t, 1, emits, "at most one entry per device and kind per hour")

	// Another kind, and another device, are independent.
	emit, n := l.observe("dev-1", clampKindHeartbeat, t0.Add(10*time.Minute))
	assert.True(t, emit, "heartbeat kind is limited independently of snapshots")
	assert.Equal(t, 1, n)
	emit, _ = l.observe("dev-2", clampKindSnapshot, t0.Add(10*time.Minute))
	assert.True(t, emit, "another device is limited independently")

	// Still inside the window of the first emit (t0): suppressed.
	emit, _ = l.observe("dev-1", clampKindSnapshot, t0.Add(time.Hour-time.Second))
	assert.False(t, emit, "just inside the window must stay suppressed")

	// At +1h the next entry reports the clamps since the previous entry:
	// 99 suppressed + the one just inside the window = 100, plus this one.
	emit, n = l.observe("dev-1", clampKindSnapshot, t0.Add(time.Hour))
	assert.True(t, emit, "an entry is due once the window has elapsed")
	assert.Equal(t, 101, n, "must report the clamps since the previous entry, including this one")

	// And the window restarts.
	emit, n = l.observe("dev-1", clampKindSnapshot, t0.Add(time.Hour+time.Minute))
	assert.False(t, emit)
	_ = n
}

func TestClampLogLimiter_AC031_NilReceiverAlwaysEmits(t *testing.T) {
	var l *clampLogLimiter
	emit, n := l.observe("dev-1", clampKindSnapshot, clampTestNow)
	assert.True(t, emit)
	assert.Equal(t, 1, n)
}

func TestClampLogLimiter_NFR009_SweepBounded(t *testing.T) {
	l := newClampLogLimiter()
	t0 := clampTestNow

	// 200 devices clamp once, then go quiet.
	for i := 0; i < 200; i++ {
		l.observe(fmt.Sprintf("dev-%d", i), clampKindSnapshot, t0)
	}
	assert.Len(t, l.entries, 200, "one entry per (device, kind)")

	// Any observation after the window triggers the sweep and drops the quiet entries.
	l.observe("dev-live", clampKindSnapshot, t0.Add(clampLogWindow+time.Minute))
	assert.Len(t, l.entries, 1, "entries with nothing pending and an elapsed window must be swept; remaining: %d", len(l.entries))
	assert.Contains(t, l.entries, clampKey{deviceID: "dev-live", kind: clampKindSnapshot})

	// A pending suppressed count survives the first sweep but not a 24h idle period.
	l2 := newClampLogLimiter()
	l2.observe("dev-a", clampKindSnapshot, t0)
	l2.observe("dev-a", clampKindSnapshot, t0.Add(time.Minute)) // suppressed = 1
	l2.observe("dev-b", clampKindSnapshot, t0.Add(clampLogWindow+time.Minute))
	assert.Contains(t, l2.entries, clampKey{deviceID: "dev-a", kind: clampKindSnapshot},
		"an entry with a pending count is kept for the report")
	l2.observe("dev-b", clampKindSnapshot, t0.Add(clampEntryMaxAge+2*clampLogWindow))
	assert.NotContains(t, l2.entries, clampKey{deviceID: "dev-a", kind: clampKindSnapshot},
		"an entry idle for over %s must be dropped", clampEntryMaxAge)
}

// ---------------------------------------------------------------------------
// Handler: telemetry
// ---------------------------------------------------------------------------

func TestTelemetry_AC025_FutureSnapshotClampedAndAccepted(t *testing.T) {
	clock := clampTestNow
	h, telRepo := newClampTestHandlers(t, &clock)

	rec := postTelemetry(t, h, "dev-1", snapshotBody(clock.Add(24*time.Hour)))

	require.Equal(t, http.StatusOK, rec.Code, "clamping must never reject; body: %s", rec.Body.String())
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String(), "the response must be the normal success response")
	stored := telRepo.Snapshots["dev-1"]
	require.Len(t, stored, 1)
	assert.True(t, stored[0].Timestamp.Equal(clock), "stored timestamp must equal receive time R: got %s", stored[0].Timestamp)
}

func TestTelemetry_AC025_ClampedResponseIdenticalToUnclamped(t *testing.T) {
	clock := clampTestNow
	h, _ := newClampTestHandlers(t, &clock)

	clamped := postTelemetry(t, h, "dev-1", snapshotBody(clock.Add(24*time.Hour)))
	plain := postTelemetry(t, h, "dev-1", snapshotBody(clock.Add(-time.Minute)))

	assert.Equal(t, plain.Code, clamped.Code)
	assert.Equal(t, plain.Body.String(), clamped.Body.String(), "FR-034: same success status and body")
}

func TestTelemetry_AC026_FutureSnapshotNeverLatest(t *testing.T) {
	clock := clampTestNow
	h, telRepo := newClampTestHandlers(t, &clock)

	// A snapshot dated one year ahead, then a correctly dated snapshot S later.
	require.Equal(t, http.StatusOK, postTelemetry(t, h, "dev-1", snapshotBody(clock.AddDate(1, 0, 0))).Code)
	clock = clock.Add(30 * time.Second)
	sTime := clock
	require.Equal(t, http.StatusOK, postTelemetry(t, h, "dev-1", snapshotBody(sTime)).Code)

	stored := telRepo.Snapshots["dev-1"]
	require.Len(t, stored, 2)
	// "Latest" is the row with the greatest stored timestamp (ORDER BY timestamp DESC).
	latest := stored[0]
	for _, s := range stored[1:] {
		if s.Timestamp.After(latest.Timestamp) {
			latest = s
		}
	}
	assert.True(t, latest.Timestamp.Equal(sTime), "S must be the latest; got %s", latest.Timestamp)
	assert.True(t, stored[0].Timestamp.Before(sTime), "the year-ahead snapshot must have been stored at its receive time, not a year ahead")
}

func TestTelemetry_AC027_PastSnapshotStoredAsSent(t *testing.T) {
	clock := clampTestNow
	h, telRepo := newClampTestHandlers(t, &clock)

	t0 := clock
	require.Equal(t, http.StatusOK, postTelemetry(t, h, "dev-1", snapshotBody(t0)).Code)
	// The agent's offline buffer replays an older snapshot on reconnect.
	clock = clock.Add(time.Minute)
	replay := t0.Add(-2 * time.Hour)
	require.Equal(t, http.StatusOK, postTelemetry(t, h, "dev-1", snapshotBody(replay)).Code)

	stored := telRepo.Snapshots["dev-1"]
	require.Len(t, stored, 2)
	assert.True(t, stored[1].Timestamp.Equal(replay), "replay must be stored exactly as sent: got %s", stored[1].Timestamp)
	latest := stored[0]
	for _, s := range stored[1:] {
		if s.Timestamp.After(latest.Timestamp) {
			latest = s
		}
	}
	assert.True(t, latest.Timestamp.Equal(t0), "the T0 snapshot must still be the latest")
}

func TestTelemetry_AC028_WithinAllowanceKept(t *testing.T) {
	clock := clampTestNow
	h, telRepo := newClampTestHandlers(t, &clock)

	require.Equal(t, http.StatusOK, postTelemetry(t, h, "dev-1", snapshotBody(clock.Add(4*time.Minute))).Code)
	require.Equal(t, http.StatusOK, postTelemetry(t, h, "dev-1", snapshotBody(clock.Add(6*time.Minute))).Code)

	stored := telRepo.Snapshots["dev-1"]
	require.Len(t, stored, 2)
	assert.True(t, stored[0].Timestamp.Equal(clock.Add(4*time.Minute)), "R+4m must be kept: got %s", stored[0].Timestamp)
	assert.True(t, stored[1].Timestamp.Equal(clock), "R+6m must be clamped to R: got %s", stored[1].Timestamp)
}

func TestTelemetry_AC025_MissingTimestampDefaultsToReceiveTime(t *testing.T) {
	clock := clampTestNow
	h, telRepo := newClampTestHandlers(t, &clock)

	require.Equal(t, http.StatusOK, postTelemetry(t, h, "dev-1", `{"data":{}}`).Code)

	stored := telRepo.Snapshots["dev-1"]
	require.Len(t, stored, 1)
	assert.True(t, stored[0].Timestamp.Equal(clock), "FR-033: a missing timestamp defaults to the receive time")
}

// ---------------------------------------------------------------------------
// Handler: heartbeat
// ---------------------------------------------------------------------------

func TestHeartbeat_AC029_FutureHeartbeatClamped(t *testing.T) {
	clock := clampTestNow
	h, telRepo := newClampTestHandlers(t, &clock)

	rec := postHeartbeat(t, h, "dev-1", heartbeatBody(clock.Add(24*time.Hour)))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
	stored := telRepo.Heartbeats["dev-1"]
	require.Len(t, stored, 1)
	assert.True(t, stored[0].Timestamp.Equal(clock), "stored heartbeat timestamp must equal receive time: got %s", stored[0].Timestamp)
}

func TestHeartbeat_AC030_PastHeartbeatStoredAsSent(t *testing.T) {
	clock := clampTestNow
	h, telRepo := newClampTestHandlers(t, &clock)
	past := clock.Add(-10 * time.Minute)

	rec := postHeartbeat(t, h, "dev-1", heartbeatBody(past))

	require.Equal(t, http.StatusOK, rec.Code)
	stored := telRepo.Heartbeats["dev-1"]
	require.Len(t, stored, 1)
	assert.True(t, stored[0].Timestamp.Equal(past), "a past heartbeat must be stored unchanged: got %s", stored[0].Timestamp)
}

func TestHeartbeat_AC028_WithinAllowanceKept(t *testing.T) {
	clock := clampTestNow
	h, telRepo := newClampTestHandlers(t, &clock)

	require.Equal(t, http.StatusOK, postHeartbeat(t, h, "dev-1", heartbeatBody(clock.Add(4*time.Minute))).Code)
	require.Equal(t, http.StatusOK, postHeartbeat(t, h, "dev-1", heartbeatBody(clock.Add(6*time.Minute))).Code)

	stored := telRepo.Heartbeats["dev-1"]
	require.Len(t, stored, 2)
	assert.True(t, stored[0].Timestamp.Equal(clock.Add(4*time.Minute)))
	assert.True(t, stored[1].Timestamp.Equal(clock))
}

func TestHeartbeat_AC031_ClampLogDebounced(t *testing.T) {
	buf := captureLogs(t)
	clock := clampTestNow
	h, telRepo := newClampTestHandlers(t, &clock)
	future := clock.Add(24 * time.Hour)

	for i := 0; i < 100; i++ {
		require.Equal(t, http.StatusOK, postHeartbeat(t, h, "dev-1", heartbeatBody(future)).Code,
			"a clamp must never reject (iteration %d)", i)
	}
	require.Len(t, telRepo.Heartbeats["dev-1"], 100, "every clamped heartbeat must still be stored")

	clampLines := func() []logLine {
		var out []logLine
		for _, l := range parseLogLines(t, buf) {
			if l["msg"] == "future-dated timestamp clamped" {
				out = append(out, l)
			}
		}
		return out
	}

	lines := clampLines()
	require.Len(t, lines, 1, "100 future heartbeats within the hour must produce one clamp entry")
	e := lines[0]
	assert.Equal(t, "WARN", e["level"])
	assert.Equal(t, "dev-1", e["device_id"])
	assert.Equal(t, clampKindHeartbeat, e["kind"])
	assert.Equal(t, "24h0m0s", e["skew"])
	assert.EqualValues(t, 86400, e["skew_seconds"])
	assert.EqualValues(t, 1, e["clamps_since_last"])

	// After the window the next clamp is logged and reports the 99 suppressed + itself.
	clock = clock.Add(clampLogWindow)
	require.Equal(t, http.StatusOK, postHeartbeat(t, h, "dev-1", heartbeatBody(clock.Add(24*time.Hour))).Code)
	lines = clampLines()
	require.Len(t, lines, 2, "the next entry is due after the window")
	assert.EqualValues(t, 100, lines[1]["clamps_since_last"], "must report the clamps since the previous entry")
}
