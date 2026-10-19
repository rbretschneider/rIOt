package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// telemetryBroadcast is one recorded BroadcastTelemetry call.
type telemetryBroadcast struct {
	DeviceID    string
	Data        *models.FullTelemetryData
	CollectedAt time.Time
	ReceivedAt  time.Time
}

// recordingHub is a dashboardHub that records telemetry and heartbeat
// broadcasts.
type recordingHub struct {
	mu         sync.Mutex
	telemetry  []telemetryBroadcast
	heartbeats []time.Time
}

func (r *recordingHub) BroadcastTelemetry(deviceID string, data *models.FullTelemetryData, collectedAt, receivedAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.telemetry = append(r.telemetry, telemetryBroadcast{deviceID, data, collectedAt, receivedAt})
}

func (r *recordingHub) BroadcastHeartbeat(_ string, _ *models.HeartbeatData, receivedAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.heartbeats = append(r.heartbeats, receivedAt)
}

func (r *recordingHub) BroadcastDeviceUpdate(*models.Device)        {}
func (r *recordingHub) BroadcastEvent(*models.Event)                {}
func (r *recordingHub) BroadcastDeviceRemoved(string)               {}
func (r *recordingHub) BroadcastDockerUpdate(string, interface{})   {}
func (r *recordingHub) BroadcastCommandUpdate(models.CommandUpdate) {}
func (r *recordingHub) HandleWS(http.ResponseWriter, *http.Request) {}

func (r *recordingHub) broadcasts() []telemetryBroadcast {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]telemetryBroadcast(nil), r.telemetry...)
}

type telEnv struct {
	t     *testing.T
	h     *Handlers
	tel   *testutil.MockTelemetryRepo
	hub   *recordingHub
	clock *testClock
	logs  *bytes.Buffer
}

func newTelEnv(t *testing.T) *telEnv {
	t.Helper()
	e := &telEnv{t: t, tel: testutil.NewMockTelemetryRepo(), hub: &recordingHub{}, clock: newTestClock(), logs: &bytes.Buffer{}}
	e.tel.Now = e.clock.Now
	devs := testutil.NewMockDeviceRepo()
	devs.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "host"}
	realHub := websocket.NewHub()
	go realHub.Run()
	gen := events.NewGenerator(testutil.NewMockEventRepo(), realHub, testutil.NewMockAlertRuleRepo(), testutil.NewMockDispatcher(), testutil.NewMockCommandRepo())
	e.h = &Handlers{
		telemetry: e.tel, devices: devs, hub: e.hub, eventGen: gen, now: e.clock.Now,
		async: func(fn func()) { fn() }, clampLog: newClampLogLimiter(),
	}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return e
}

// post sends a telemetry snapshot; ts "" omits the timestamp.
func (e *telEnv) post(ts string) *httptest.ResponseRecorder {
	e.t.Helper()
	body := `{"data":{"system":{"hostname":"host"}}}`
	if ts != "" {
		body = fmt.Sprintf(`{"timestamp":%q,"data":{"system":{"hostname":"host"}}}`, ts)
	}
	r := chi.NewRouter()
	r.Post("/api/v1/devices/{id}/telemetry", e.h.Telemetry)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/dev-1/telemetry", strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(e.t, http.StatusOK, rec.Code, rec.Body.String())
	return rec
}

func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func TestCL_AC079_TelemetryBroadcastHasTimestamps(t *testing.T) {
	e := newTelEnv(t)
	collected := e.clock.Now().Add(-3 * time.Second)

	e.post(rfc(collected))

	b := e.hub.broadcasts()
	require.Len(t, b, 1)
	assert.True(t, b[0].CollectedAt.Equal(collected), "[AC-079] collected_at is the snapshot timestamp")
	assert.True(t, b[0].ReceivedAt.Equal(e.clock.Now()), "[AC-079] received_at is server time")
	assert.Equal(t, "dev-1", b[0].DeviceID)
}

func TestCL_AC083_OlderSnapshotHistoryOnly(t *testing.T) {
	e := newTelEnv(t)
	t2 := e.clock.Now().Add(-10 * time.Second)
	t1 := t2.Add(-time.Minute)

	e.post(rfc(t2))
	e.post(rfc(t1)) // the agent flushes an older buffered snapshot afterwards

	assert.Len(t, e.hub.broadcasts(), 1, "[AC-083] a stale snapshot must not be broadcast")
	assert.Len(t, e.tel.Snapshots["dev-1"], 2, "[AC-083] ... but it is stored in history")
	latest, err := e.tel.GetLatestSnapshot(nil, "dev-1") //nolint:staticcheck // the mock ignores the context
	require.NoError(t, err)
	assert.True(t, latest.Timestamp.Equal(t2), "[AC-083] the current state must stay T2, got %s", latest.Timestamp)
}

func TestTelemetry_EqualTimestampIsStale(t *testing.T) {
	e := newTelEnv(t)
	ts := rfc(e.clock.Now().Add(-time.Second))

	e.post(ts)
	e.post(ts)

	assert.Len(t, e.hub.broadcasts(), 1, "[AC-081 server half] a snapshot at the same instant is not newer")
	assert.Len(t, e.tel.Snapshots["dev-1"], 2)
}

func TestCL_AC084_MissingTimestampUsesReceivedAt(t *testing.T) {
	e := newTelEnv(t)

	e.post("")

	require.Len(t, e.tel.Snapshots["dev-1"], 1)
	assert.True(t, e.tel.Snapshots["dev-1"][0].Timestamp.Equal(e.clock.Now()), "[AC-084] collected_at must equal received_at")
	b := e.hub.broadcasts()
	require.Len(t, b, 1)
	assert.True(t, b[0].CollectedAt.Equal(b[0].ReceivedAt))
}

func TestCL_SEC003_FutureTimestampClampedToReceivedAt(t *testing.T) {
	e := newTelEnv(t)

	e.post(rfc(e.clock.Now().Add(365 * 24 * time.Hour)))

	require.Len(t, e.tel.Snapshots["dev-1"], 1)
	assert.True(t, e.tel.Snapshots["dev-1"][0].Timestamp.Equal(e.clock.Now()), "[SEC-003] the stored timestamp is received_at")
	assert.Contains(t, e.logs.String(), "future-dated timestamp clamped")
	assert.Contains(t, e.logs.String(), "device_id=dev-1")
}

func TestCL_SEC003_ClampToleranceBoundary(t *testing.T) {
	e := newTelEnv(t)
	e.post(rfc(e.clock.Now().Add(models.TelemetryFutureTolerance))) // exactly at the tolerance is kept
	assert.True(t, e.tel.Snapshots["dev-1"][0].Timestamp.Equal(e.clock.Now().Add(models.TelemetryFutureTolerance)))
	assert.NotContains(t, e.logs.String(), "future-dated timestamp clamped")

	e.clock.Advance(time.Hour)
	e.post(rfc(e.clock.Now().Add(models.TelemetryFutureTolerance + time.Second)))
	assert.True(t, e.tel.Snapshots["dev-1"][1].Timestamp.Equal(e.clock.Now()), "past the tolerance it is clamped")
}

func TestCL_SEC003_FutureSnapshotDoesNotSuppressLaterTelemetry(t *testing.T) {
	e := newTelEnv(t)

	e.post(rfc(e.clock.Now().Add(365 * 24 * time.Hour))) // one hostile / skewed snapshot
	e.clock.Advance(15 * time.Second)
	e.post(rfc(e.clock.Now().Add(-time.Second))) // the next genuine one

	b := e.hub.broadcasts()
	assert.Len(t, b, 2, "[SEC-003] after a +1-year snapshot the next real snapshot must still be broadcast")
	assert.Equal(t, 2, len(e.tel.Snapshots["dev-1"]))
}

func TestCL_SEC003_BroadcastCollectedAtIsClampedValue(t *testing.T) {
	e := newTelEnv(t)

	e.post(rfc(e.clock.Now().Add(24 * time.Hour)))

	b := e.hub.broadcasts()
	require.Len(t, b, 1)
	assert.True(t, b[0].CollectedAt.Equal(b[0].ReceivedAt), "[SEC-003] the broadcast collected_at must be the clamped value")
	assert.False(t, b[0].CollectedAt.After(b[0].ReceivedAt.Add(models.TelemetryFutureTolerance)),
		"collected_at is never more than the tolerance ahead of received_at")
}

// After merging REG-HARDEN-001 the clamp Warn is its debounced noteClamp (one
// entry per device and kind per clampLogWindow), which is stricter than the
// story's original 10-per-minute throttle and still satisfies SEC-001.
func TestTelemetry_ClampLogIsThrottledPerDevice(t *testing.T) {
	e := newTelEnv(t)
	for i := 0; i < 30; i++ {
		e.clock.Advance(time.Second)
		e.post(rfc(e.clock.Now().Add(time.Hour)))
	}
	assert.Equal(t, 1, strings.Count(e.logs.String(), "future-dated timestamp clamped"),
		"[SEC-001/003] 30 clamps from one device within the window produce one Warn")
}

func TestTelemetry_StaleSkipsCurrentStateWork(t *testing.T) {
	e := newTelEnv(t)
	t2 := e.clock.Now().Add(-time.Minute)
	e.post(rfc(t2))
	e.h.devices.(*testutil.MockDeviceRepo).Devices["dev-1"].Status = models.DeviceStatusOnline

	// An older snapshot reporting UPS on battery must not flip the device status.
	body := fmt.Sprintf(`{"timestamp":%q,"data":{"ups":{"on_battery":true}}}`, rfc(t2.Add(-time.Hour)))
	r := chi.NewRouter()
	r.Post("/api/v1/devices/{id}/telemetry", e.h.Telemetry)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/devices/dev-1/telemetry", strings.NewReader(body)))
	require.Equal(t, http.StatusOK, rec.Code)

	dev, err := e.h.devices.GetByID(nil, "dev-1") //nolint:staticcheck // the mock ignores the context
	require.NoError(t, err)
	assert.Equal(t, models.DeviceStatusOnline, dev.Status, "a stale snapshot must not run UPS handling")
	assert.Len(t, e.hub.broadcasts(), 1)
}

func TestHeartbeat_BroadcastCarriesServerReceivedAt(t *testing.T) {
	e := newTelEnv(t)
	r := chi.NewRouter()
	r.Post("/api/v1/devices/{id}/heartbeat", e.h.Heartbeat)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/devices/dev-1/heartbeat",
		bytes.NewBufferString(`{"timestamp":"2001-01-01T00:00:00Z","data":{"uptime":5}}`)))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	e.hub.mu.Lock()
	defer e.hub.mu.Unlock()
	require.Len(t, e.hub.heartbeats, 1)
	assert.True(t, e.hub.heartbeats[0].Equal(e.clock.Now()), "the heartbeat envelope carries server time, not the agent's")
}

func TestTelemetryEnvelopeJSONShape(t *testing.T) {
	// The WS envelope puts the timestamps at the top level, next to type and
	// device_id, never inside data (WP-WEB contract).
	collected := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(websocket.WSMessage{Type: "telemetry", DeviceID: "d", Data: map[string]int{"x": 1},
		CollectedAt: &collected, ReceivedAt: &collected})
	require.NoError(t, err)
	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &top))
	assert.Contains(t, top, "collected_at")
	assert.Contains(t, top, "received_at")
	var data map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(top["data"], &data))
	assert.NotContains(t, data, "collected_at")
}
