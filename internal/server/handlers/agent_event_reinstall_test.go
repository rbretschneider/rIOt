package handlers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-008 handler tests (ADD section 8, "HT8" and section 4.5): the installer's
// reinstall notice goes through the real chi route with the real DeviceAuth (and
// MTLSDeviceAuth for the mTLS case), the response carries the disposition the
// installer prints, and the notice never creates an events row. The clock is
// injected into the Handlers; nothing sleeps. PiCUPS is the EXPECT-002 fixture's
// first device and PiVPN its second.

const (
	ex8PiCUPS = ex2Hawaii
	ex8PiVPN  = ex2Vermont
	ex8Notice = `{"type":"agent_reinstall_started","severity":"info","message":"Agent reinstall started by installer"}`
)

type ex8Fx struct {
	*ex2Fx
	auth chi.Router // the agent routes behind DeviceAuth
	mtls chi.Router // the same routes behind MTLSDeviceAuth, then DeviceAuth
}

func newEX8Fixture(t *testing.T) *ex8Fx {
	t.Helper()
	f := &ex8Fx{ex2Fx: newEX2Fixture(t)}
	f.dev.Devices[ex8PiCUPS].Hostname = "PiCUPS"
	f.dev.Devices[ex8PiVPN].Hostname = "PiVPN"
	f.cmds.LinkDevices(f.dev) // a notice reads the device status
	build := func(mw ...func(http.Handler) http.Handler) chi.Router {
		r := chi.NewRouter()
		r.Route("/api/v1/devices/{id}", func(r chi.Router) {
			r.With(mw...).Post("/events", f.h.ReceiveAgentEvent)
			r.With(mw...).Post("/commands/{commandId}/report", f.h.AgentCommandReport)
		})
		return r
	}
	f.auth = build(middleware.DeviceAuth(f.dev))
	f.mtls = build(middleware.MTLSDeviceAuth(f.dev, &testutil.MockCARepo{}), middleware.DeviceAuth(f.dev))
	return f
}

// post sends a notice-shaped body to the device route with an X-rIOt-Key header
// ("" sends none).
func (f *ex8Fx) post(r chi.Router, deviceID, key, body string) *httptest.ResponseRecorder {
	return f.postTLS(r, deviceID, key, body, nil)
}

func (f *ex8Fx) postTLS(r chi.Router, deviceID, key, body string, state *tls.ConnectionState) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/"+deviceID+"/events", strings.NewReader(body))
	if key != "" {
		req.Header.Set("X-rIOt-Key", key)
	}
	req.TLS = state
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func (f *ex8Fx) notice(deviceID, key string) *httptest.ResponseRecorder {
	return f.post(f.auth, deviceID, key, ex8Notice)
}

func (f *ex8Fx) reinstallRows(deviceID string) []models.Command {
	var out []models.Command
	for _, c := range f.cmds.All() {
		if c.DeviceID == deviceID && c.Origin == models.OriginInstaller {
			out = append(out, c)
		}
	}
	return out
}

func (f *ex8Fx) allEvents() []models.Event {
	all, err := f.evts.ListAll(context.Background(), 100000, 0)
	require.NoError(f.t, err)
	return all
}

func clientCert(cn string, serial int64) *tls.ConnectionState {
	return &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: cn}, SerialNumber: big.NewInt(serial)}}}
}

func TestEX8_AC008_NoticeCreatesRecordStoresNoEvent(t *testing.T) {
	f := newEX8Fixture(t)
	f.clock.Set(ex2T0.Add(5 * time.Second))
	rec := f.notice(ex8PiVPN, "key-vermont")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"status":"ok","reinstall":"created"}`, rec.Body.String(), "[AD-002] the disposition the installer reads")
	assert.Equal(t, `{"status":"ok","reinstall":"created"}`+"\n", rec.Body.String(), "fixed field order: status, then reinstall")

	rows := f.reinstallRows(ex8PiVPN)
	require.Len(t, rows, 1, "[AC-008] one record")
	assert.Equal(t, "running", rows[0].Status)
	assert.True(t, rows[0].DispatchedAt.Equal(ex2T0.Add(5*time.Second)), "[AD-014] the start is the injected clock's time")
	assert.Empty(t, f.allEvents(), "[NFR-005] the events table is unchanged: the notice is never stored as an event")
	assert.Empty(t, f.hub.eventsOf(models.EventAgentReinstallStarted), "and never broadcast")
	assert.Empty(t, f.reinstallRows(ex8PiCUPS), "another device is untouched")
}

func TestEX8_AC008_RegistrationClosesRecordAtFirstRegistration(t *testing.T) {
	f := newEX8Fixture(t)
	require.Equal(t, http.StatusOK, f.notice(ex8PiVPN, "key-vermont").Code)

	f.clock.Set(ex2T0.Add(40 * time.Second))
	require.Equal(t, http.StatusOK, f.register("key-hawaii", ex8PiCUPS, "2.83.0").Code)
	assert.Equal(t, "running", f.reinstallRows(ex8PiVPN)[0].Status, "[NFR-007] another device's registration leaves the record running")

	f.clock.Set(ex2T0.Add(70 * time.Second))
	require.Equal(t, http.StatusOK, f.register("key-vermont", ex8PiVPN, "2.83.0").Code)
	got := f.reinstallRows(ex8PiVPN)[0]
	assert.Equal(t, "succeeded", got.Status, "[FR-015 a] the device's registration ends the record")
	assert.Equal(t, models.ReinstallStepConfirmed, got.Step)
	require.NotNil(t, got.FinishedAt)
	assert.True(t, got.FinishedAt.Equal(ex2T0.Add(70*time.Second)), "finished_at is the registration time")
	assert.True(t, f.dev.Devices[ex8PiVPN].LastHeartbeat.Equal(ex2T0.Add(70*time.Second)), "the heartbeat is refreshed (EXPECT-002 X-20)")

	// A late registration (after the bound) leaves a fresh record alone.
	g := newEX8Fixture(t)
	require.Equal(t, http.StatusOK, g.notice(ex8PiVPN, "key-vermont").Code)
	g.clock.Set(ex2T0.Add(10 * time.Minute))
	require.Equal(t, http.StatusOK, g.register("key-vermont", ex8PiVPN, "2.83.0").Code)
	assert.Equal(t, "running", g.reinstallRows(ex8PiVPN)[0].Status, "a registration at the bound belongs to no window")
}

func TestEX8_AC014_RejectedNoticesCreateNothing(t *testing.T) {
	f := newEX8Fixture(t)
	// A revoked key is a once-valid key that is gone from the table.
	f.dev.APIKeys["key-revoked"] = ex8PiVPN
	delete(f.dev.APIKeys, "key-revoked")

	cases := []struct {
		name   string
		device string
		key    string
		code   int
	}{
		{"no key", ex8PiVPN, "", http.StatusUnauthorized},
		{"wrong key", ex8PiVPN, "key-wrong", http.StatusUnauthorized},
		{"revoked key", ex8PiVPN, "key-revoked", http.StatusUnauthorized},
		{"PiCUPS key naming PiVPN", ex8PiVPN, "key-hawaii", http.StatusForbidden},
	}
	for _, tc := range cases {
		rec := f.notice(tc.device, tc.key)
		assert.Equal(t, tc.code, rec.Code, "[AC-014] %s: %s", tc.name, rec.Body.String())
		assert.NotContains(t, rec.Body.String(), "reinstall", "%s: no disposition is revealed", tc.name)
	}
	assert.Empty(t, f.reinstallRows(ex8PiVPN), "[AC-014] no record for PiVPN")
	assert.Empty(t, f.reinstallRows(ex8PiCUPS), "and none for PiCUPS")
	assert.Empty(t, f.cmds.Calls(), "the command repository was never called")
	assert.Empty(t, f.allEvents(), "and no event exists")
}

func TestEX8_AC014_HandlerWithoutAuthenticationRecordsNothing(t *testing.T) {
	// Defence in depth (AD-002): if the handler ever ran without DeviceAuth, or
	// with another device's identity, it records nothing and says so.
	f := newEX8Fixture(t)
	for name, authAs := range map[string]string{"no identity": "", "another device": ex8PiCUPS} {
		rec := f.do("POST", "/api/v1/devices/"+ex8PiVPN+"/events", ex8Notice, authAs)
		require.Equal(t, http.StatusOK, rec.Code, name)
		assert.JSONEq(t, `{"status":"ok","reinstall":"unavailable"}`, rec.Body.String(), "[AD-002] %s", name)
	}
	assert.Empty(t, f.reinstallRows(ex8PiVPN))
	assert.Zero(t, f.cmds.CallCount("CreateReinstall"), "the repository is never called")
}

func TestEX8_AC017_UntrustedFieldsIgnored(t *testing.T) {
	hostile := "\x1b[31m" + strings.Repeat("A‮\u0000", 3000) // control characters and a bidi override
	// The well-formed variant escapes the control characters and the bidi override;
	// the invalid UTF-8 variant below sends raw bytes inside the JSON string.
	enc, err := json.Marshal(map[string]any{
		"type": "agent_reinstall_started", "severity": hostile, "message": strings.Repeat("m‮\x01", 3500),
		"command_id": "0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c", "extra": map[string]any{"a": []int{1, 2, 3}},
	})
	require.NoError(t, err)
	require.Greater(t, len(enc), 10<<10, "the message is above 10 KB")

	plain := newEX8Fixture(t)
	require.Equal(t, http.StatusOK, plain.notice(ex8PiVPN, "key-vermont").Code)
	baseline := plain.reinstallRows(ex8PiVPN)[0]

	f := newEX8Fixture(t)
	rec := f.post(f.auth, ex8PiVPN, "key-vermont", string(enc))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"status":"ok","reinstall":"created"}`, rec.Body.String(), "[AC-017] the same disposition as a plain notice")
	rows := f.reinstallRows(ex8PiVPN)
	require.Len(t, rows, 1)
	got := rows[0]
	assert.Equal(t, baseline.Status, got.Status)
	assert.Equal(t, baseline.Step, got.Step)
	assert.Equal(t, baseline.ResultMsg, got.ResultMsg, "[FR-012] nothing from the body reaches the record")
	assert.Equal(t, baseline.Params, got.Params)
	assert.Equal(t, baseline.TargetKeys, got.TargetKeys)
	assert.True(t, got.DeadlineAt.Sub(*got.DispatchedAt) == baseline.DeadlineAt.Sub(*baseline.DispatchedAt), "the same 10-minute bound")
	assert.Empty(t, f.allEvents(), "and no event carries the text")

	// Invalid UTF-8 inside the JSON string: the decoder replaces it, the result is the same.
	g := newEX8Fixture(t)
	bad := `{"type":"agent_reinstall_started","severity":"info","message":"` + strings.Repeat("\xff\xfe", 5000) + `","command_id":"x"}`
	rec = g.post(g.auth, ex8PiVPN, "key-vermont", bad)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok","reinstall":"created"}`, rec.Body.String(), "[AC-017] invalid UTF-8 changes nothing")
}

func TestEX8_AC018_RepoErrorAnswersUnavailable(t *testing.T) {
	f := newEX8Fixture(t)
	f.cmds.FailOn("CreateReinstall", errors.New("database is down"))
	rec := f.notice(ex8PiVPN, "key-vermont")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok","reinstall":"unavailable"}`, rec.Body.String(), "[AC-018] the installer reads 'unavailable'")
	assert.Empty(t, f.reinstallRows(ex8PiVPN), "no record")
	assert.Empty(t, f.allEvents(), "no event")

	// A body that is not JSON is the existing 400.
	rec = f.post(f.auth, ex8PiVPN, "key-vermont", "not json")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":"invalid request body"}`, rec.Body.String())
}

func TestEX8_AC022_AgentReportNotFoundAndCancelConflictOverHTTP(t *testing.T) {
	f := newEX8Fixture(t)
	require.Equal(t, http.StatusOK, f.notice(ex8PiVPN, "key-vermont").Code)
	rec := f.reinstallRows(ex8PiVPN)[0]
	before := f.cmds.All()
	path := "/api/v1/devices/" + ex8PiVPN + "/commands/" + rec.ID + "/report"
	bodies := map[string]string{
		"ack":      `{"kind":"ack"}`,
		"progress": `{"kind":"progress","state":"running","step":"download","progress":50}`,
		"result":   `{"kind":"result","result":{"command_id":"` + rec.ID + `","status":"success","outcome":"succeeded","message":"ok"}}`,
	}
	for name, body := range bodies {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("X-rIOt-Key", "key-vermont")
		resp := httptest.NewRecorder()
		f.auth.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusNotFound, resp.Code, "[AC-022] HTTP %s on a reinstall record is 404", name)
		assert.JSONEq(t, `{"error":"command not found","code":"command_not_found"}`, resp.Body.String())
	}
	assert.Equal(t, before, f.cmds.All(), "the record is untouched by every report")

	resp := f.do("POST", "/api/v1/devices/"+ex8PiVPN+"/commands/"+rec.ID+"/cancel", "", "")
	assert.Equal(t, http.StatusConflict, resp.Code, "[AC-022] cancel is 409")
	var body models.ReportResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, models.CodeNotCancellable, body.Code)
	assert.Equal(t, "running", body.Status)
}

// ---------------------------------------------------------------------------
// Security condition C-7 (ADD section 4.5)
// ---------------------------------------------------------------------------

func TestEX8_C7_ServerAuthoredTypesRejectedOnAgentRoute(t *testing.T) {
	f := newEX8Fixture(t)
	for _, typ := range []models.EventType{models.EventAgentReinstallUnconfirmed, models.EventAgentReinstallPaused} {
		body := agentEventBody(string(typ), "warning", "forged by a holder of the device key", nil)
		rec := f.post(f.auth, ex8PiVPN, "key-vermont", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "[C-7] %s with a valid device key is refused", typ)
		assert.JSONEq(t, `{"error":"unsupported event type"}`, rec.Body.String(), "[C-7] %s: the unknown-type answer", typ)
	}
	assert.Empty(t, f.allEvents(), "[C-7] no events row")
	assert.Empty(t, f.cmds.All(), "[C-7] no commands row")
	assert.Empty(t, f.hub.events, "and nothing was broadcast")
	assert.Zero(t, f.cmds.CallCount("CreateReinstall"), "the record store was never asked")
}

func TestEX8_C7_NoticeStoresNoEventOnEveryDisposition(t *testing.T) {
	f := newEX8Fixture(t)
	dispositions := map[string]bool{}
	do := func() string {
		rec := f.notice(ex8PiVPN, "key-vermont")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var resp struct{ Reinstall string }
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		dispositions[resp.Reinstall] = true
		return resp.Reinstall
	}
	noStartedEvent := func(when string) {
		for _, e := range f.allEvents() {
			assert.NotEqual(t, models.EventAgentReinstallStarted, e.Type, "[C-7] %s: no agent_reinstall_started row, ever", when)
		}
	}

	assert.Equal(t, "created", do())
	noStartedEvent("created")
	assert.Equal(t, "open_record", do())
	noStartedEvent("open_record")
	assert.Empty(t, f.allEvents(), "created and open_record store no event at all")

	// Fill the cap: five more records, each closed by a registration (the first
	// record is closed by one too).
	f.clock.Set(ex2T0.Add(time.Minute))
	require.Equal(t, http.StatusOK, f.register("key-vermont", ex8PiVPN, "2.83.0").Code)
	for i := 1; i < models.ReinstallRateCap; i++ {
		f.clock.Set(ex2T0.Add(time.Duration(i)*time.Hour + time.Minute))
		require.Equal(t, "created", do())
		f.clock.Set(ex2T0.Add(time.Duration(i)*time.Hour + 2*time.Minute))
		require.Equal(t, http.StatusOK, f.register("key-vermont", ex8PiVPN, "2.83.0").Code)
	}
	require.Len(t, f.reinstallRows(ex8PiVPN), models.ReinstallRateCap)
	f.clock.Set(ex2T0.Add(6 * time.Hour))
	assert.Equal(t, "capped", do())
	noStartedEvent("capped")
	capEvents := f.allEvents()
	require.Len(t, capEvents, 1, "[C-7] the cap warning is the only new event")
	assert.Equal(t, models.EventAgentReinstallPaused, capEvents[0].Type, "and it is server-authored")
	assert.Equal(t, models.SeverityWarning, capEvents[0].Severity)
	assert.Empty(t, capEvents[0].CommandID, "not linked to a record")
	assert.Equal(t, 1, len(f.hub.eventsOf(models.EventAgentReinstallPaused)), "and broadcast once")
	assert.Equal(t, "capped", do())
	assert.Len(t, f.allEvents(), 1, "a repeat stores no second cap event")

	// device_offline.
	f.dev.Devices[ex8PiVPN].Status = models.DeviceStatusOffline
	assert.Equal(t, "device_offline", do())
	noStartedEvent("device_offline")
	f.dev.Devices[ex8PiVPN].Status = models.DeviceStatusOnline

	// unavailable (store error).
	f.cmds.FailOn("CreateReinstall", errors.New("database is down"))
	assert.Equal(t, "unavailable", do())
	noStartedEvent("unavailable")

	assert.Len(t, f.allEvents(), 1, "[C-7] still only the cap warning after every disposition")
	assert.Len(t, dispositions, 5, "every disposition was exercised: %v", dispositions)
}

func TestEX8_C7_ForeignKeyForbiddenWithMTLS(t *testing.T) {
	f := newEX8Fixture(t)
	piCUPSCert := clientCert(ex8PiCUPS, 7)

	rec := f.postTLS(f.mtls, ex8PiVPN, "key-hawaii", ex8Notice, piCUPSCert)
	assert.Equal(t, http.StatusForbidden, rec.Code, "[C-7] PiCUPS's key naming PiVPN is refused, with a valid PiCUPS client certificate: %s", rec.Body.String())
	assert.JSONEq(t, `{"error":"api key does not match device"}`, rec.Body.String())
	assert.Empty(t, f.reinstallRows(ex8PiVPN), "[C-7] no record for PiVPN")
	assert.Empty(t, f.reinstallRows(ex8PiCUPS), "and none for PiCUPS")
	assert.Empty(t, f.allEvents(), "and no event")
	assert.Zero(t, f.cmds.CallCount("CreateReinstall"), "the record store was never asked")

	// The certificate is required first.
	rec = f.postTLS(f.mtls, ex8PiCUPS, "key-hawaii", ex8Notice, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "without a client certificate the notice is refused")
	assert.Empty(t, f.reinstallRows(ex8PiCUPS))

	// Positive control: the same chain accepts PiCUPS's own notice.
	rec = f.postTLS(f.mtls, ex8PiCUPS, "key-hawaii", ex8Notice, piCUPSCert)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"status":"ok","reinstall":"created"}`, rec.Body.String(), "the control proves the chain works")
	assert.Len(t, f.reinstallRows(ex8PiCUPS), 1)
}

func TestEX8_C7_MockGuardParityIncludesAttribution(t *testing.T) {
	// AD-017: a running reinstall row never captures a self-update start. This is
	// the mock side, through the handler (the PostgreSQL side is
	// TestEX8_AC023_PG_SelfUpdateAttributionIgnoresReinstall and
	// TestEX8_MockGuardParity in internal/server/db).
	f := newEX8Fixture(t)
	require.Equal(t, http.StatusOK, f.notice(ex8PiCUPS, "key-hawaii").Code)
	re := f.reinstallRows(ex8PiCUPS)[0]

	f.clock.Set(ex2T0.Add(2 * time.Minute))
	rec := f.post(f.auth, ex8PiCUPS, "key-hawaii", agentEventBody("agent_update_started", "info", ex2Start, nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	started := f.eventsOfType(models.EventAgentUpdateStarted)
	require.Len(t, started, 1)
	assert.NotEqual(t, re.ID, started[0].CommandID, "[C-7, AD-017] the start is not attributed to the reinstall row")
	require.NotEmpty(t, started[0].CommandID, "it is linked to its own self-update record")
	selfRows := f.records(ex8PiCUPS)
	require.Len(t, selfRows, 1, "a self-update record exists next to the reinstall record")
	assert.Equal(t, selfRows[0].ID, started[0].CommandID)
	assert.Equal(t, "running", f.reinstallRows(ex8PiCUPS)[0].Status, "the reinstall row is untouched")
}
