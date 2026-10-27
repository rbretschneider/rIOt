package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-002 handler tests (ADD section 8, "HT"): ReceiveAgentEvent recognises
// and attributes the agent_update_* events of the authenticated device,
// RegisterDevice refreshes the heartbeat and moves an open record, and the
// agent report endpoints read a self-update record as an unknown command. The
// clock is injected into the Handlers; nothing sleeps.

const (
	ex2Hawaii  = "11111111-1111-4111-8111-111111111111"
	ex2Vermont = "22222222-2222-4222-8222-222222222222"
	ex2Start   = "Agent update started: 2.78.0 → 2.80.0"
)

var ex2T0 = time.Date(2026, 10, 26, 17, 51, 0, 0, time.UTC)

type ex2Clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *ex2Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *ex2Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// ex2Hub is a dashboardHub that records the events and command updates it is
// asked to broadcast.
type ex2Hub struct {
	mu       sync.Mutex
	events   []models.Event
	commands []models.CommandUpdate
}

func (h *ex2Hub) BroadcastEvent(e *models.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, *e)
}

func (h *ex2Hub) BroadcastCommandUpdate(u models.CommandUpdate) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.commands = append(h.commands, u)
}

func (h *ex2Hub) BroadcastDeviceUpdate(*models.Device)                                       {}
func (h *ex2Hub) BroadcastHeartbeat(string, *models.HeartbeatData, time.Time)                {}
func (h *ex2Hub) BroadcastTelemetry(string, *models.FullTelemetryData, time.Time, time.Time) {}
func (h *ex2Hub) BroadcastDeviceRemoved(string)                                              {}
func (h *ex2Hub) BroadcastDockerUpdate(string, interface{})                                  {}
func (h *ex2Hub) HandleWS(http.ResponseWriter, *http.Request)                                {}

func (h *ex2Hub) eventsOf(typ models.EventType) []models.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []models.Event
	for _, e := range h.events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

type ex2Fx struct {
	t      *testing.T
	h      *Handlers
	dev    *testutil.MockDeviceRepo
	evts   *testutil.MockEventRepo
	cmds   *testutil.MockCommandRepo
	hub    *ex2Hub
	disp   *testutil.MockDispatcher
	clock  *ex2Clock
	gen    *events.Generator
	router chi.Router
}

func newEX2Fixture(t *testing.T) *ex2Fx {
	t.Helper()
	f := &ex2Fx{
		t: t, dev: testutil.NewMockDeviceRepo(), evts: testutil.NewMockEventRepo(), cmds: testutil.NewMockCommandRepo(),
		hub: &ex2Hub{}, disp: testutil.NewMockDispatcher(), clock: &ex2Clock{t: ex2T0},
	}
	f.cmds.LinkEvents(f.evts)
	for id, host := range map[string]string{ex2Hawaii: "hawaii", ex2Vermont: "vermont"} {
		hb := ex2T0
		f.dev.Devices[id] = &models.Device{ID: id, ShortID: id[:8], Hostname: host, Arch: "amd64", AgentVersion: "2.78.0",
			Status: models.DeviceStatusOnline, LastHeartbeat: &hb}
	}
	f.dev.APIKeys["key-hawaii"] = ex2Hawaii
	f.dev.APIKeys["key-vermont"] = ex2Vermont
	rules := testutil.NewMockAlertRuleRepo()
	_ = rules.Create(context.Background(), &models.AlertRule{Name: "device offline", Enabled: true, Metric: "device_offline",
		Operator: ">", Threshold: 0, Severity: "critical", Notify: true})
	realHub := websocket.NewHub()
	go realHub.Run()
	f.gen = events.NewGenerator(f.evts, realHub, rules, f.disp, f.cmds)
	f.gen.SetClock(f.clock.Now)
	f.h = &Handlers{devices: f.dev, events: f.evts, commandRepo: f.cmds, hub: f.hub, eventGen: f.gen,
		adminRepo: testutil.NewMockAdminRepo(""), now: f.clock.Now}
	f.router = chi.NewRouter()
	f.router.Post("/api/v1/devices/{id}/events", f.h.ReceiveAgentEvent)
	f.router.Post("/api/v1/devices/{id}/commands/{commandId}/report", f.h.AgentCommandReport)
	f.router.Post("/api/v1/devices/{id}/commands/{commandId}/cancel", f.h.CancelCommand)
	f.router.Get("/api/v1/devices/{id}/commands/{commandId}/events", f.h.GetCommandEvents)
	return f
}

// do runs a request through the router, optionally as an authenticated device
// (the context value DeviceAuth sets).
func (f *ex2Fx) do(method, path, body, authAs string) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if authAs != "" {
		req = req.WithContext(context.WithValue(req.Context(), middleware.DeviceIDKey, authAs))
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func agentEventBody(typ, severity, message string, extra map[string]string) string {
	m := map[string]string{"type": typ, "severity": severity, "message": message}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func (f *ex2Fx) event(typ, severity, message, authAs string) *httptest.ResponseRecorder {
	f.t.Helper()
	return f.do("POST", "/api/v1/devices/"+ex2Hawaii+"/events", agentEventBody(typ, severity, message, nil), authAs)
}

func (f *ex2Fx) records(device string) []models.Command {
	var out []models.Command
	for _, c := range f.cmds.All() {
		if c.DeviceID == device && c.Origin == models.OriginAgentSelfUpdate {
			out = append(out, c)
		}
	}
	return out
}

func (f *ex2Fx) eventsOfType(typ models.EventType) []models.Event {
	var out []models.Event
	all, err := f.evts.ListAll(context.Background(), 100000, 0)
	require.NoError(f.t, err)
	for _, e := range all {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func (f *ex2Fx) register(key, deviceID, version string) *httptest.ResponseRecorder {
	f.t.Helper()
	body, _ := json.Marshal(models.DeviceRegistration{Hostname: f.dev.Devices[deviceID].Hostname, Arch: "amd64", AgentVersion: version, DeviceID: deviceID})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/register", strings.NewReader(string(body)))
	req.Header.Set("X-rIOt-Key", key)
	req.RemoteAddr = regTestClientAddr
	rec := httptest.NewRecorder()
	f.h.RegisterDevice(rec, req)
	return rec
}

func TestEX2_Handler_ValidStartCreatesRecordAndLinks(t *testing.T) {
	f := newEX2Fixture(t)
	f.clock.Set(ex2T0.Add(3 * time.Second))
	// The agent may send any command_id: the server never reads it for these types.
	other := "0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c"
	body := agentEventBody("agent_update_started", "info", ex2Start, map[string]string{"command_id": other})
	rec := f.do("POST", "/api/v1/devices/"+ex2Hawaii+"/events", body, ex2Hawaii)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String(), "the response is unchanged")

	recs := f.records(ex2Hawaii)
	require.Len(t, recs, 1, "one record is created")
	assert.Equal(t, "running", recs[0].Status)
	assert.True(t, recs[0].CreatedAt.Equal(ex2T0.Add(3*time.Second)), "[AD-014] the record starts at the injected clock's time")
	started := f.eventsOfType(models.EventAgentUpdateStarted)
	require.Len(t, started, 1)
	assert.Equal(t, recs[0].ID, started[0].CommandID, "[FR-005] the event is linked to the record")
	assert.NotEqual(t, other, started[0].CommandID, "[X-2] the agent's command_id is ignored")
	assert.Equal(t, models.CommandAttrInferred, started[0].CommandAttr)
	assert.Equal(t, ex2Start, started[0].Message, "stored as sent")
	assert.True(t, started[0].CreatedAt.Equal(ex2T0.Add(3*time.Second)), "the event uses the injected clock")
	require.Len(t, f.hub.eventsOf(models.EventAgentUpdateStarted), 1, "and is broadcast")
	require.Len(t, f.hub.commands, 1, "the record's creation reaches the dashboards")
	assert.Equal(t, "agent_self_update", f.hub.commands[0].Origin)
}

func TestEX2_AC016_MalformedStartStoredAsToday(t *testing.T) {
	f := newEX2Fixture(t)
	msgs := []string{
		"Agent update started: 2.78.0 -> 2.80.0",
		"Agent update started: dev → 2.80.0",
		"Agent update started: 2.80.0 → 2.80.0",
		"Agent update started: 2.78.0 → 2.80.0\u0000x",
		"agent update started: 2.78.0 → 2.80.0",
		"Agent update started: 2.78.0 → " + strings.Repeat("9", 300),
		strings.Repeat("B", 10<<10),
	}
	for _, m := range msgs {
		rec := f.event("agent_update_started", "info", m, ex2Hawaii)
		assert.Equal(t, http.StatusOK, rec.Code, "[AC-016] the response code is unchanged")
	}
	assert.Empty(t, f.records(ex2Hawaii), "[AC-016] no record is created")
	stored := f.eventsOfType(models.EventAgentUpdateStarted)
	require.Len(t, stored, len(msgs), "every event is stored")
	for i, e := range stored {
		assert.Empty(t, e.CommandID, "[AC-016] message %d is stored without a link", i)
		assert.Empty(t, e.CommandAttr)
		assert.Equal(t, models.SeverityInfo, e.Severity)
	}
	assert.Empty(t, f.hub.commands, "no command_update broadcast")
}

func TestEX2_AC018_MismatchedAuthenticatedDeviceIgnored(t *testing.T) {
	f := newEX2Fixture(t)
	// A request that names hawaii but is authenticated as vermont (DeviceAuth makes
	// this impossible, the handler checks it again), and one that did not pass
	// DeviceAuth at all.
	for name, authAs := range map[string]string{"authenticated as another device": ex2Vermont, "not authenticated": ""} {
		rec := f.event("agent_update_started", "info", ex2Start, authAs)
		assert.Equal(t, http.StatusOK, rec.Code, name)
	}
	assert.Empty(t, f.records(ex2Hawaii), "[INFO-1] no record for the named device")
	assert.Empty(t, f.records(ex2Vermont), "and none for the authenticated one")
	stored := f.eventsOfType(models.EventAgentUpdateStarted)
	require.Len(t, stored, 2, "the events are stored as today")
	for _, e := range stored {
		assert.Empty(t, e.CommandID, "without a link")
	}

	// The same device authenticated: a record for that device only.
	rec := f.event("agent_update_started", "info", ex2Start, ex2Hawaii)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, f.records(ex2Hawaii), 1)
	assert.Empty(t, f.records(ex2Vermont), "[NFR-010] a start from one device never creates a record for another")
}

func TestEX2_AC020_EventStoredWhenRecordStoreFails(t *testing.T) {
	f := newEX2Fixture(t)
	f.cmds.FailOn("CreateSelfUpdate", errors.New("the record store is down"))
	rec := f.event("agent_update_started", "info", ex2Start, ex2Hawaii)
	assert.Equal(t, http.StatusOK, rec.Code, "[AC-020] the agent still gets its 200")
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
	stored := f.eventsOfType(models.EventAgentUpdateStarted)
	require.Len(t, stored, 1, "the event is stored as today")
	assert.Empty(t, stored[0].CommandID, "without a link")
	assert.Empty(t, f.records(ex2Hawaii))

	f.cmds.FailOn("CreateSelfUpdate", nil)
	f.cmds.FailOn("LatestSelfUpdateSince", errors.New("the record store is down"))
	rec = f.event("agent_update_completed", "info", "Agent updated to 2.80.0", ex2Hawaii)
	assert.Equal(t, http.StatusOK, rec.Code, "a failing attribution read never fails the request")
	assert.Len(t, f.eventsOfType(models.EventAgentUpdateCompleted), 1)
}

func TestEX2_AC027_UpdateAvailableUnchanged(t *testing.T) {
	f := newEX2Fixture(t)
	rec := f.event("agent_update_available", "info", "Agent update available: 2.78.0 → 2.80.0", ex2Hawaii)
	require.Equal(t, http.StatusOK, rec.Code)
	rec = f.event("agent_update_available", "info", ex2Start, ex2Hawaii) // even a start-looking text
	require.Equal(t, http.StatusOK, rec.Code)
	stored := f.eventsOfType(models.EventAgentUpdateAvail)
	require.Len(t, stored, 2, "[AC-027] stored and broadcast as today")
	for _, e := range stored {
		assert.Empty(t, e.CommandID, "no link")
		assert.Equal(t, models.SeverityInfo, e.Severity)
	}
	assert.Len(t, f.hub.eventsOf(models.EventAgentUpdateAvail), 2)
	assert.Empty(t, f.records(ex2Hawaii), "no record is created")
	assert.Equal(t, 0, f.cmds.CallCount("CreateSelfUpdate"), "the repository is never asked")
	assert.Empty(t, f.hub.commands, "and no label appears")
}

func TestEX2_X19_AgentUpdateSeverityClamped(t *testing.T) {
	big := strings.Repeat("x", 1024)
	for _, typ := range []models.EventType{models.EventAgentUpdateAvail, models.EventAgentUpdateStarted, models.EventAgentUpdateCompleted, models.EventAgentUpdateFailed} {
		want := models.SeverityInfo
		if typ == models.EventAgentUpdateFailed {
			want = models.SeverityWarning
		}
		for _, bad := range []string{"", "panic", "WARNING", big} {
			f := newEX2Fixture(t)
			rec := f.event(string(typ), bad, "whatever", ex2Hawaii)
			require.Equal(t, http.StatusOK, rec.Code, "[X-19] %s with severity %.8q", typ, bad)
			stored := f.eventsOfType(typ)
			require.Len(t, stored, 1)
			assert.Equal(t, want, stored[0].Severity, "[X-19] %s: %.8q is clamped to %s", typ, bad, want)
		}
		for _, keep := range []models.EventSeverity{models.SeverityInfo, models.SeverityWarning, models.SeverityCrit} {
			f := newEX2Fixture(t)
			f.event(string(typ), string(keep), "whatever", ex2Hawaii)
			stored := f.eventsOfType(typ)
			require.Len(t, stored, 1)
			assert.Equal(t, keep, stored[0].Severity, "[X-19] a known severity is stored as sent (%s)", keep)
		}
	}
}

func TestEX2_Handler_CompletedAndFailedMoveTheRecord(t *testing.T) {
	f := newEX2Fixture(t)
	f.event("agent_update_started", "info", ex2Start, ex2Hawaii)
	rec0 := f.records(ex2Hawaii)[0]
	f.clock.Set(ex2T0.Add(90 * time.Second))
	f.event("agent_update_failed", "warning", "Agent update failed: download: connection reset", ex2Hawaii)
	got := f.records(ex2Hawaii)[0]
	assert.Equal(t, "failed", got.Status)
	failed := f.eventsOfType(models.EventAgentUpdateFailed)
	require.Len(t, failed, 1)
	assert.Equal(t, rec0.ID, failed[0].CommandID, "the failed event is linked to the record")
	assert.Equal(t, models.SeverityWarning, failed[0].Severity)
}

func TestEX2_AC005_StaleHeartbeatAfterFastReturnNoAlert(t *testing.T) {
	// A restart that registers at last heartbeat + 40 s, a detector pass at + 46 s,
	// and the first heartbeat at + 50 s: with a registration that refreshes the
	// heartbeat (X-20), the detector sees a fresh one and makes no offline transition.
	f := newEX2Fixture(t)
	f.event("agent_update_started", "info", ex2Start, ex2Hawaii)
	lastHeartbeat := ex2T0

	f.clock.Set(lastHeartbeat.Add(40 * time.Second))
	rec := f.register("key-hawaii", ex2Hawaii, "2.80.0")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	d := f.dev.Devices[ex2Hawaii]
	require.NotNil(t, d.LastHeartbeat)
	assert.True(t, d.LastHeartbeat.Equal(lastHeartbeat.Add(40*time.Second)),
		"[X-20] the registration refreshed last_heartbeat to the registration time, got %s", d.LastHeartbeat)

	// The detector's predicate (offline.go check): live and heartbeat older than the threshold.
	detectorPass := func(now time.Time) bool {
		threshold := now.Add(-models.OfflineHeartbeatThreshold)
		return models.DeviceIsLive(d.Status) && d.LastHeartbeat != nil && d.LastHeartbeat.Before(threshold)
	}
	assert.False(t, detectorPass(lastHeartbeat.Add(46*time.Second)), "[AC-005] the pass at +46 s finds a fresh heartbeat: no offline transition")
	d.LastHeartbeat = tp(lastHeartbeat.Add(50 * time.Second)) // the first heartbeat of the new process
	assert.False(t, detectorPass(lastHeartbeat.Add(80*time.Second)), "and none after the heartbeats resume")
	assert.Empty(t, f.eventsOfType(models.EventDeviceOffline), "no device_offline event")
	assert.Empty(t, f.disp.Alerts, "no push")

	// Without the refresh the same timeline would have gone offline at +46 s: the
	// threshold is 45 s from the last stored heartbeat.
	stale := ex2T0
	assert.True(t, stale.Before(lastHeartbeat.Add(46*time.Second).Add(-models.OfflineHeartbeatThreshold)), "the old behaviour: a stale heartbeat at +46 s is offline")
}

func tp(t time.Time) *time.Time { return &t }

func TestEX2_Handler_RegistrationMovesOpenRecordAndRefreshesHeartbeat(t *testing.T) {
	f := newEX2Fixture(t)
	f.event("agent_update_started", "info", ex2Start, ex2Hawaii)
	rec := f.records(ex2Hawaii)[0]

	// Another device's registration changes nothing of hawaii's record (INFO-1).
	f.clock.Set(ex2T0.Add(60 * time.Second))
	require.Equal(t, http.StatusOK, f.register("key-vermont", ex2Vermont, "2.80.0").Code)
	require.Equal(t, http.StatusOK, f.register("key-vermont", ex2Vermont, "2.70.0").Code)
	assert.Equal(t, "running", f.records(ex2Hawaii)[0].Status, "[AC-018] vermont's registrations leave hawaii's record running")
	assert.True(t, f.dev.Devices[ex2Vermont].LastHeartbeat.Equal(ex2T0.Add(60*time.Second)), "[X-20] every device's registration refreshes its heartbeat")

	f.clock.Set(ex2T0.Add(100 * time.Second))
	require.Equal(t, http.StatusOK, f.register("key-hawaii", ex2Hawaii, "2.80.0").Code)
	got := f.records(ex2Hawaii)[0]
	assert.Equal(t, rec.ID, got.ID)
	assert.Equal(t, "verifying", got.Status, "[FR-011 c] a registration on <to> moves the record to verifying")
	require.NotNil(t, got.VerifyingAt)
	assert.True(t, got.VerifyingAt.Equal(ex2T0.Add(100*time.Second)))

	// A fresh record, a registration on the old version.
	g := newEX2Fixture(t)
	g.event("agent_update_started", "info", ex2Start, ex2Hawaii)
	g.clock.Set(ex2T0.Add(100 * time.Second))
	require.Equal(t, http.StatusOK, g.register("key-hawaii", ex2Hawaii, "2.78.0").Code)
	wrong := g.records(ex2Hawaii)[0]
	assert.Equal(t, "failed", wrong.Status, "[FR-019] a registration on another version fails the record")
	assert.Equal(t, models.SelfUpdateFailureWrongVersion, wrong.FailureStep)
	assert.Equal(t, "hawaii came back on agent 2.78.0 after Agent update 2.78.0 → 2.80.0 by agent auto-update (expected 2.80.0)", wrong.ResultMsg)
}

func TestEX2_AC024_AgentReportNotFoundHTTPAndWS(t *testing.T) {
	f := newEX2Fixture(t)
	f.event("agent_update_started", "info", ex2Start, ex2Hawaii)
	rec := f.records(ex2Hawaii)[0]
	before := f.cmds.All()
	path := "/api/v1/devices/" + ex2Hawaii + "/commands/" + rec.ID + "/report"

	bodies := map[string]string{
		"ack":      `{"kind":"ack"}`,
		"progress": `{"kind":"progress","state":"running","step":"download","progress":50}`,
		"result":   `{"kind":"result","result":{"command_id":"` + rec.ID + `","status":"success","outcome":"succeeded","message":"ok"}}`,
	}
	for name, body := range bodies {
		resp := f.do("POST", path, body, ex2Hawaii)
		assert.Equal(t, http.StatusNotFound, resp.Code, "[AC-024] HTTP %s on a self-update record is 404", name)
		assert.JSONEq(t, `{"error":"command not found","code":"command_not_found"}`, resp.Body.String(), "the body of a missing command")
	}

	// WebSocket: only a result gets a receipt, and it reads not_found with no status.
	sock := &fakeSocket{}
	ac := &AgentConn{DeviceID: ex2Hawaii, Conn: sock}
	ctx := context.Background()
	f.h.handleAgentCommandMessage(ctx, ac, wsMsg(t, "command_ack", models.CommandAck{CommandID: rec.ID, At: ex2T0}))
	f.h.handleAgentCommandMessage(ctx, ac, wsMsg(t, "command_progress", models.CommandProgress{CommandID: rec.ID, State: "running", Step: "download", At: ex2T0}))
	assert.Empty(t, sock.messages("command_receipt"), "an ack and a progress message get no receipt")
	f.h.handleAgentCommandMessage(ctx, ac, wsMsg(t, "command_result", models.CommandResult{CommandID: rec.ID, Status: "success", Outcome: models.OutcomeSucceeded, Message: "ok"}))
	receipts := sock.messages("command_receipt")
	require.Len(t, receipts, 1)
	var r models.CommandReceipt
	require.NoError(t, json.Unmarshal(receipts[0].Data, &r))
	assert.Equal(t, models.ReportNotFound, r.Result, "[AC-024] the WebSocket receipt reads not_found")
	assert.Empty(t, r.Status, "and reveals no status")
	assert.Equal(t, before, f.cmds.All(), "the record is untouched by every report")
}

func TestEX2_AC024_CancelNotCancellableOverHTTP(t *testing.T) {
	f := newEX2Fixture(t)
	f.event("agent_update_started", "info", ex2Start, ex2Hawaii)
	rec := f.records(ex2Hawaii)[0]
	resp := f.do("POST", "/api/v1/devices/"+ex2Hawaii+"/commands/"+rec.ID+"/cancel", "", "")
	assert.Equal(t, http.StatusConflict, resp.Code, "[AC-024] cancel is 409")
	var body models.ReportResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, models.CodeNotCancellable, body.Code)
	assert.Equal(t, "running", body.Status, "and carries the status")
	assert.Equal(t, "running", f.records(ex2Hawaii)[0].Status)
}
