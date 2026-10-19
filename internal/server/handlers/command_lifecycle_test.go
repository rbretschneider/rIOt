package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/commands"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// router mounts the command routes exactly as the real router does, minus auth.
func (e *lcEnv) router() http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1/devices/{id}", func(r chi.Router) {
		r.Post("/commands", e.h.SendCommand)
		r.Get("/commands", e.h.ListDeviceCommands)
		r.Get("/commands/{commandId}", e.h.GetCommand)
		r.Post("/commands/{commandId}/cancel", e.h.CancelCommand)
		r.Get("/commands/{commandId}/output", e.h.GetCommandOutput)
		r.Post("/commands/{commandId}/report", e.h.AgentCommandReport)
		r.Post("/docker/bulk-update", e.h.BulkDockerUpdate)
	})
	return r
}

func (e *lcEnv) call(method, path, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	e.router().ServeHTTP(rec, req)
	return rec
}

func (e *lcEnv) report(deviceID, cmdID, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.call("POST", "/api/v1/devices/"+deviceID+"/commands/"+cmdID+"/report", body)
}

// disconnect removes dev-1's agent socket so commands stay queued.
func (e *lcEnv) disconnect() { e.reg.unregister("dev-1", e.conn) }

func decode(t *testing.T, rec *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), v), rec.Body.String())
}

// ---------------------------------------------------------------------------
// AC-016, AC-023, AC-024, AC-090, AC-091: the HTTP report endpoint
// ---------------------------------------------------------------------------

func TestCL_AC016_ReportEndpointRecordsTerminal(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-1", "docker_update", "running", nil)

	rec := e.report("dev-1", uid(1), `{"kind":"result","result":{"status":"success","outcome":"succeeded","message":"updated","step":"cleanup","output":"pulled"}}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp models.ReportResponse
	decode(t, rec, &resp)
	assert.Equal(t, uid(1), resp.CommandID, "the 200 must echo command_id so the agent can trust it")
	assert.Equal(t, models.ReportAccepted, resp.Result)
	assert.Equal(t, "succeeded", resp.Status)
	got := e.get(uid(1))
	assert.Equal(t, "succeeded", got.Status, "[AC-016] the server records the HTTP result as terminal")
	assert.Equal(t, "updated", got.ResultMsg)
	assert.Equal(t, "pulled", e.repo.Outputs[uid(1)][0].Content)
}

func TestCL_AC023_DuplicateFinalIs2xxRowUnchanged(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-1", "fetch_logs", "dispatched", nil)
	body := `{"kind":"result","result":{"status":"success","message":"done"}}`
	require.Equal(t, http.StatusOK, e.report("dev-1", uid(1), body).Code)
	before := e.get(uid(1))
	e.clock.Advance(time.Hour)

	rec := e.report("dev-1", uid(1), body)

	assert.Equal(t, http.StatusOK, rec.Code, "[AC-023] the same final result again is 2xx")
	var resp models.ReportResponse
	decode(t, rec, &resp)
	assert.Equal(t, models.ReportDuplicate, resp.Result)
	assert.Equal(t, before, e.get(uid(1)), "[AC-023] the row, including updated_at, must be unchanged")
}

func TestCL_AC024_ForeignDeviceReportRejected(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-1", "fetch_logs", "dispatched", nil)
	before := e.get(uid(1))

	rec := e.report("dev-2", uid(1), `{"kind":"result","result":{"status":"success","message":"forged"}}`)

	assert.Equal(t, http.StatusNotFound, rec.Code, "[AC-024] device B must not be able to report on device A's command")
	assert.Contains(t, rec.Body.String(), `"code":"command_not_found"`)
	assert.Equal(t, before, e.get(uid(1)), "[AC-024] the row must be unchanged")
	assert.Empty(t, e.repo.Outputs)
}

func TestCL_AC090_DisallowedHTTPReport409(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-1", "fetch_logs", "running", nil)

	rec := e.report("dev-1", uid(1), `{"kind":"ack"}`)

	assert.Equal(t, http.StatusConflict, rec.Code, "[AC-090] a disallowed transition over HTTP is 409")
	var resp models.ReportResponse
	decode(t, rec, &resp)
	assert.Equal(t, models.CodeInvalidTransition, resp.Code)
	assert.Equal(t, "running", resp.Status)
	assert.NotEmpty(t, resp.Error)
	assert.Equal(t, "running", e.get(uid(1)).Status)

	e.seed(uid(2), "dev-1", "fetch_logs", "succeeded", nil)
	rec = e.report("dev-1", uid(2), `{"kind":"result","result":{"status":"error"}}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	decode(t, rec, &resp)
	assert.Equal(t, models.CodeAlreadyTerminal, resp.Code, "a different final outcome on an immutable row is already_terminal")
}

func TestCL_AC091_GetByIDHasPerStateTimestamps(t *testing.T) {
	e := newLCEnv(t)
	e.disconnect()
	rec := e.call("POST", "/api/v1/devices/dev-1/commands", `{"action":"docker_update","params":{"container":"web"}}`)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created models.Command
	decode(t, rec, &created)
	_, err := e.svc.ClaimForHeartbeat(context.Background(), "dev-1")
	require.NoError(t, err)
	for _, body := range []string{
		`{"kind":"ack"}`,
		`{"kind":"progress","state":"running","step":"pull"}`,
		`{"kind":"progress","state":"verifying","step":"verify"}`,
		`{"kind":"result","result":{"status":"success","outcome":"succeeded"}}`,
	} {
		e.clock.Advance(2 * time.Second)
		require.Equal(t, http.StatusOK, e.report("dev-1", created.ID, body).Code)
	}

	rec = e.call("GET", "/api/v1/devices/dev-1/commands/"+created.ID, "")

	require.Equal(t, http.StatusOK, rec.Code)
	var raw map[string]interface{}
	decode(t, rec, &raw)
	for _, key := range []string{"dispatched_at", "acknowledged_at", "started_at", "verifying_at", "finished_at", "created_at", "updated_at", "expires_at"} {
		assert.NotNil(t, raw[key], "[AC-091] %s must be present", key)
	}
	assert.Equal(t, "succeeded", raw["status"])
	assert.Equal(t, "web", e.get(created.ID).Params["container"])
	_, leaksInternal := raw["pre_lifecycle"]
	_, leaksAmbiguous := raw["send_ambiguous"]
	assert.False(t, leaksInternal || leaksAmbiguous, "internal flags are not part of the API")
}

// ---------------------------------------------------------------------------
// AC-028, AC-074: heartbeat delivery
// ---------------------------------------------------------------------------

func heartbeatEnv(t *testing.T) (*lcEnv, func() map[string]interface{}) {
	t.Helper()
	e := newLCEnv(t)
	e.disconnect()
	hh, _, devs := newHeartbeatTestHandlers(t)
	devs.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "h"}
	hh.commandRepo, hh.commands, hh.agents, hh.now = e.repo, e.svc, e.reg, e.clock.Now
	return e, func() map[string]interface{} {
		t.Helper()
		rec := postHeartbeat(t, hh, "dev-1", `{"device_id":"dev-1","data":{"uptime":1}}`)
		require.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]interface{}
		decode(t, rec, &resp)
		return resp
	}
}

func TestCL_AC028_HeartbeatNeverDeliversExpiredReboot(t *testing.T) {
	e, heartbeat := heartbeatEnv(t)
	e.seed(uid(1), "dev-1", "reboot", "queued", func(c *models.Command) { c.ExpiresAt = e.clock.Now().Add(-time.Minute) })

	resp := heartbeat()

	assert.NotContains(t, resp, "pending_commands", "[AC-028] an expired reboot must not be in the heartbeat response")
	assert.Equal(t, "expired", e.get(uid(1)).Status, "[AC-028] its state must be expired")
}

func TestCL_AC074_HeartbeatExcludesWSClaimed(t *testing.T) {
	e, heartbeat := heartbeatEnv(t)
	e.reg.replace("dev-1", e.conn) // connected: the create claims and sends over WS
	rec := e.call("POST", "/api/v1/devices/dev-1/commands", `{"action":"fetch_logs"}`)
	require.Equal(t, http.StatusCreated, rec.Code)
	var cmd models.Command
	decode(t, rec, &cmd)
	require.Equal(t, "dispatched", cmd.Status)

	resp := heartbeat()

	assert.NotContains(t, resp, "pending_commands", "[AC-074] a command already claimed via WS must not be in the heartbeat response")
}

func TestHeartbeat_DeliversOnlyItsOwnClaims(t *testing.T) {
	e, heartbeat := heartbeatEnv(t)
	e.seed(uid(1), "dev-1", "fetch_logs", "queued", nil)
	e.seed(uid(2), "dev-2", "fetch_logs", "queued", nil)

	first := heartbeat()
	second := heartbeat()

	pending, ok := first["pending_commands"].([]interface{})
	require.True(t, ok)
	require.Len(t, pending, 1, "only dev-1's command")
	assert.Equal(t, uid(1), pending[0].(map[string]interface{})["command_id"])
	assert.NotContains(t, second, "pending_commands", "a second heartbeat must not re-deliver a claimed command")
	assert.Equal(t, "queued", e.get(uid(2)).Status, "another device's command is untouched")
}

// ---------------------------------------------------------------------------
// AC-068..AC-071: conflicts over HTTP
// ---------------------------------------------------------------------------

func TestCL_AC068_ConflictReturns409NoRow(t *testing.T) {
	e := newLCEnv(t)
	e.disconnect()
	first := e.call("POST", "/api/v1/devices/dev-1/commands", `{"action":"docker_update","params":{"container":"web"}}`)
	require.Equal(t, http.StatusCreated, first.Code)
	var created models.Command
	decode(t, first, &created)

	second := e.call("POST", "/api/v1/devices/dev-1/commands", `{"action":"docker_update","params":{"container":"/web"}}`)

	require.Equal(t, http.StatusConflict, second.Code, "[AC-068] a second update of the same target is 409")
	var body map[string]interface{}
	decode(t, second, &body)
	assert.Equal(t, "conflict", body["code"])
	assert.Equal(t, created.ID, body["existing_command_id"], "[AC-068] the 409 names the existing command")
	assert.Equal(t, "queued", body["existing_status"])
	assert.Equal(t, "docker:container:web", body["target_key"])
	assert.NotEmpty(t, body["error"])
	assert.Len(t, e.repo.All(), 1, "[AC-068] no row may be created")
}

func TestCL_AC069_TerminalPreviousAllowsNew(t *testing.T) {
	e := newLCEnv(t)
	e.disconnect()
	e.seed(uid(1), "dev-1", "docker_update", "succeeded", func(c *models.Command) { c.TargetKeys = []string{"docker:container:web"} })

	rec := e.call("POST", "/api/v1/devices/dev-1/commands", `{"action":"docker_update","params":{"container":"web"}}`)

	assert.Equal(t, http.StatusCreated, rec.Code, "[AC-069] a terminal previous command must not block a new one")
}

func TestCL_AC070_RebootAcceptedSecondRebootConflicts(t *testing.T) {
	e := newLCEnv(t)
	e.disconnect()
	e.seed(uid(1), "dev-1", "docker_update", "running", func(c *models.Command) { c.TargetKeys = []string{"docker:container:web"} })

	first := e.call("POST", "/api/v1/devices/dev-1/commands", `{"action":"reboot"}`)
	second := e.call("POST", "/api/v1/devices/dev-1/commands", `{"action":"reboot"}`)

	assert.Equal(t, http.StatusCreated, first.Code, "[AC-070] a reboot is accepted while a docker_update is in flight")
	assert.Equal(t, http.StatusConflict, second.Code, "[AC-070] a second reboot while the first is non-terminal gets 409")
	var body map[string]interface{}
	decode(t, second, &body)
	assert.Equal(t, "host", body["target_key"])
}

func TestCL_AC071_BulkSkipsConflictingTargets(t *testing.T) {
	e := newLCEnv(t)
	e.disconnect()
	e.seed(uid(1), "dev-1", "docker_update", "running", func(c *models.Command) { c.TargetKeys = []string{"docker:id:web"} })

	rec := e.call("POST", "/api/v1/devices/dev-1/docker/bulk-update", `{"container_ids":["web","db"]}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var body struct {
		models.Command
		Conflicts []models.BulkConflict `json:"conflicts"`
	}
	decode(t, rec, &body)
	require.Len(t, body.Conflicts, 1)
	assert.Equal(t, models.BulkConflict{ContainerID: "web", TargetKey: "docker:id:web", ExistingCommandID: uid(1)}, body.Conflicts[0],
		"[AC-071] web is reported as conflict with the existing id")
	assert.Equal(t, []string{"docker:id:db"}, body.TargetKeys, "[AC-071] db is created")
	assert.Equal(t, "docker_bulk_update", body.Action)
}

func TestContract_BulkAllConflictingIs409WithConflictShape(t *testing.T) {
	e := newLCEnv(t)
	e.disconnect()
	e.seed(uid(1), "dev-1", "docker_update", "running", func(c *models.Command) { c.TargetKeys = []string{"docker:id:web"} })

	rec := e.call("POST", "/api/v1/devices/dev-1/docker/bulk-update", `{"container_ids":["web"]}`)

	require.Equal(t, http.StatusConflict, rec.Code)
	var body map[string]interface{}
	decode(t, rec, &body)
	assert.Equal(t, "conflict", body["code"], "the bulk 409 has the same shape as the single 409")
	assert.Equal(t, uid(1), body["existing_command_id"])
	assert.Equal(t, "docker:id:web", body["target_key"])
	assert.NotEmpty(t, body["error"])
	assert.NotEmpty(t, body["conflicts"])
}

func TestContract_BulkCreatedWithoutConflictsHasEmptyArray(t *testing.T) {
	e := newLCEnv(t)
	e.disconnect()

	rec := e.call("POST", "/api/v1/devices/dev-1/docker/bulk-update", `{"container_ids":["a"]}`)

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), `"conflicts":[]`, "conflicts must serialise as [] not null")
}

// ---------------------------------------------------------------------------
// AC-105, AC-106: cancel
// ---------------------------------------------------------------------------

func TestCL_AC105_CancelledNeverDelivered(t *testing.T) {
	e, heartbeat := heartbeatEnv(t)
	e.seed(uid(1), "dev-1", "reboot", "queued", nil)

	rec := e.call("POST", "/api/v1/devices/dev-1/commands/"+uid(1)+"/cancel", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var cmd models.Command
	decode(t, rec, &cmd)
	assert.Equal(t, uid(1), cmd.ID, "cancel returns the Command")
	assert.Equal(t, "cancelled", cmd.Status, "[AC-105]")
	resp := heartbeat()
	assert.NotContains(t, resp, "pending_commands", "[AC-105] a cancelled command is never delivered")
	assert.Equal(t, "cancelled", e.get(uid(1)).Status)
}

func TestCL_AC106_CancelDispatched409(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-1", "reboot", "dispatched", nil)

	rec := e.call("POST", "/api/v1/devices/dev-1/commands/"+uid(1)+"/cancel", "")

	assert.Equal(t, http.StatusConflict, rec.Code, "[AC-106]")
	var body models.ReportResponse
	decode(t, rec, &body)
	assert.Equal(t, models.CodeNotCancellable, body.Code)
	assert.Equal(t, "command cannot be cancelled after it has been dispatched", body.Error, "[FR-087] the operator-readable message")
	assert.Equal(t, "dispatched", body.Status)
	assert.Equal(t, "dispatched", e.get(uid(1)).Status, "[AC-106] the state is unchanged")
}

func TestCancelAndGet_NotFoundShapeIsUniform(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-2", "reboot", "queued", nil) // someone else's

	var bodies []string
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/devices/dev-1/commands/" + uid(1)},
		{"GET", "/api/v1/devices/dev-1/commands/" + uid(99)},
		{"GET", "/api/v1/devices/dev-1/commands/not-a-uuid"},
		{"POST", "/api/v1/devices/dev-1/commands/" + uid(1) + "/cancel"},
		{"POST", "/api/v1/devices/dev-1/commands/" + uid(99) + "/cancel"},
		{"POST", "/api/v1/devices/dev-1/commands/not-a-uuid/cancel"},
	} {
		rec := e.call(tc.method, tc.path, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, tc.method+" "+tc.path)
		bodies = append(bodies, rec.Body.String())
	}
	for _, b := range bodies[1:] {
		assert.Equal(t, bodies[0], b, "a foreign, unknown and malformed id must be indistinguishable")
	}
	assert.Contains(t, bodies[0], `"code":"command_not_found"`)
	assert.Equal(t, "queued", e.get(uid(1)).Status, "a foreign cancel must not cancel")
}

// ---------------------------------------------------------------------------
// List endpoint: filters, active, target_keys, canonical values
// ---------------------------------------------------------------------------

func TestCL_SEC006_StatusFilterRejectsUnknownValues(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-1", "reboot", "queued", nil)
	hostile := []string{
		"queued')--", "queued'%20OR%20'1'='1", "bogus", "queued,", ",queued", "QUEUED", "queued%20", "queued%3Bdrop",
		strings.Repeat("queued,", 20) + "queued",
	}
	for _, status := range hostile {
		rec := e.call("GET", "/api/v1/devices/dev-1/commands?status="+status, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, "[SEC-006] status=%q must be rejected", status)
		var body models.ReportResponse
		decode(t, rec, &body)
		assert.Equal(t, models.CodeInvalidFilter, body.Code)
	}
	assert.Zero(t, e.repo.CallCount("ListByDeviceFiltered")+e.repo.CallCount("ListByDevice")+e.repo.CallCount("ListActive"),
		"[SEC-006] the repository must never be called for a rejected filter")

	rec := e.call("GET", "/api/v1/devices/dev-1/commands?target_key="+strings.Repeat("k", models.MaxTargetKeyFilterBytes+1), "")
	assert.Equal(t, http.StatusBadRequest, rec.Code, "an over-long target_key is rejected")
}

func TestContract_StatusFilterAcceptsCanonicalAndLegacyAndReturnsCanonical(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-1", "reboot", "success", nil)
	e.seed(uid(2), "dev-1", "reboot", "error", nil)
	e.seed(uid(3), "dev-1", "reboot", "pending", nil)
	e.seed(uid(4), "dev-1", "reboot", "timed_out", nil)

	for _, tc := range []struct {
		status string
		want   []string
	}{
		{"succeeded", []string{"succeeded"}},
		{"success,error", []string{"succeeded", "failed"}},
		{"failed", []string{"failed"}},
		{"queued", []string{"queued"}},
		{"timed_out,expired,cancelled", []string{"timed_out"}},
	} {
		rec := e.call("GET", "/api/v1/devices/dev-1/commands?status="+tc.status, "")
		require.Equal(t, http.StatusOK, rec.Code, tc.status)
		var list []models.Command
		decode(t, rec, &list)
		var got []string
		for _, c := range list {
			got = append(got, c.Status)
		}
		assert.ElementsMatch(t, tc.want, got, "[AC-101] status=%s must filter alias-expanded and return canonical values", tc.status)
	}
}

func TestContract_ActiveListReturnsTargetKeys(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-1", "docker_update", "running", func(c *models.Command) {
		c.TargetKeys = []string{"docker:compose:/srv/stack"}
	})
	e.seed(uid(2), "dev-1", "docker_bulk_update", "queued", func(c *models.Command) {
		c.TargetKeys = []string{"docker:compose:/srv/stack", "docker:container:solo"}
	})
	e.seed(uid(3), "dev-1", "reboot", "succeeded", func(c *models.Command) { c.TargetKeys = []string{"host"} })
	e.seed(uid(4), "dev-2", "reboot", "queued", func(c *models.Command) { c.TargetKeys = []string{"host"} })

	rec := e.call("GET", "/api/v1/devices/dev-1/commands?active=true", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var list []models.Command
	decode(t, rec, &list)
	require.Len(t, list, 2, "every non-terminal command of the device, nothing terminal, nothing foreign")
	byID := map[string]models.Command{}
	for _, c := range list {
		byID[c.ID] = c
	}
	assert.Equal(t, []string{"docker:compose:/srv/stack"}, byID[uid(1)].TargetKeys, "a compose docker_update carries the compose key")
	assert.Equal(t, []string{"docker:compose:/srv/stack", "docker:container:solo"}, byID[uid(2)].TargetKeys, "a bulk carries the union")
	assert.Equal(t, "docker:compose:/srv/stack", byID[uid(1)].TargetKey)

	rec = e.call("GET", "/api/v1/devices/dev-1/commands?active=true&target_key=docker:container:solo", "")
	decode(t, rec, &list)
	require.Len(t, list, 1)
	assert.Equal(t, uid(2), list[0].ID, "target_key narrows the active list")
}

func TestContract_ListUnfilteredReturnsEmptyArrayNotNull(t *testing.T) {
	e := newLCEnv(t)
	rec := e.call("GET", "/api/v1/devices/dev-1/commands?active=true", "")
	assert.Equal(t, "[]", strings.TrimSpace(rec.Body.String()))
}

// ---------------------------------------------------------------------------
// SEC-001 / C-1: report bounds and throttling over both transports
// ---------------------------------------------------------------------------

func TestCL_SEC001_NonUUIDCommandIDRejectedWithoutQueryOrPayloadLog(t *testing.T) {
	const secret = "SECRET-ID-PAYLOAD"
	hugeID := strings.Repeat("a", 1<<20)

	t.Run("HTTP path", func(t *testing.T) {
		e := newLCEnv(t)
		for _, id := range []string{"not-a-uuid", "..%2F..%2Ftelemetry", secret, "0B6F1C3E-5A52-4E0C-9D57-3F8C2A4B7E10"} {
			rec := e.report("dev-1", id, `{"kind":"ack"}`)
			assert.Equal(t, http.StatusBadRequest, rec.Code, "[SEC-001] path id %q", id)
			assert.Contains(t, rec.Body.String(), `"code":"invalid_report"`)
		}
		assert.Empty(t, e.repo.Calls(), "[SEC-001] the repository must never be called")
		assert.NotContains(t, e.logs.String(), secret)
	})

	t.Run("HTTP body", func(t *testing.T) {
		e := newLCEnv(t)
		e.seed(uid(1), "dev-1", "fetch_logs", "dispatched", nil)
		rec := e.report("dev-1", uid(1), `{"kind":"ack","command_id":"`+secret+`"}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Empty(t, e.repo.Calls(), "[SEC-001] no query for a mismatching body id")
		assert.NotContains(t, e.logs.String(), secret)
		assert.Equal(t, "dispatched", e.get(uid(1)).Status)
	})

	t.Run("WS", func(t *testing.T) {
		e := newLCEnv(t)
		e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_ack", models.CommandAck{CommandID: secret}))
		e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_progress", models.CommandProgress{CommandID: secret, State: "running"}))
		e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_result", models.CommandResult{CommandID: secret, Status: "success"}))
		assert.Empty(t, e.sock.receipts(t), "[SEC-001] a malformed id gets no receipt")
		assert.Empty(t, e.repo.Calls(), "[SEC-001] the repository must never be called")
		assert.NotContains(t, e.logs.String(), secret)
		assert.Contains(t, e.logs.String(), "malformed command_id from agent", "a length-only record is allowed")
	})

	t.Run("1 MiB id", func(t *testing.T) {
		e := newLCEnv(t)
		rec := e.report("dev-1", hugeID, `{"kind":"ack"}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_result", models.CommandResult{CommandID: hugeID, Status: "success"}))
		assert.Empty(t, e.sock.receipts(t))
		assert.Empty(t, e.repo.Calls())
		assert.Less(t, e.logs.Len(), 4096, "[SEC-001] a 1 MiB id must not be echoed into the log")
	})
}

func TestCL_SEC001_StepAndMessageBounded(t *testing.T) {
	multibyte := strings.Repeat("é", 2000)
	check := func(t *testing.T, e *lcEnv, id string) {
		got := e.get(id)
		assert.Equal(t, "", got.Step, "[SEC-001] a step outside ^[a-z0-9_]{1,64}$ is stored as empty")
		assert.Equal(t, "running", got.Status, "the transition still applies")
		assert.LessOrEqual(t, len(got.ResultMsg), models.MaxProgressMessageBytes, "[SEC-001] message bounded to 1 KiB")
		assert.True(t, utf8.ValidString(got.ResultMsg), "truncation is rune-safe")
		assert.NotEmpty(t, got.ResultMsg)
	}

	t.Run("HTTP", func(t *testing.T) {
		e := newLCEnv(t)
		e.seed(uid(1), "dev-1", "docker_update", "dispatched", nil)
		body, err := json.Marshal(models.CommandReport{Kind: models.ReportKindProgress, State: "running", Step: "Not A Step/" + strings.Repeat("x", 100), Message: multibyte})
		require.NoError(t, err)
		rec := e.report("dev-1", uid(1), string(body))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		check(t, e, uid(1))
	})

	t.Run("WS", func(t *testing.T) {
		e := newLCEnv(t)
		e.seed(uid(1), "dev-1", "docker_update", "dispatched", nil)
		e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_progress",
			models.CommandProgress{CommandID: uid(1), State: "running", Step: strings.Repeat("a", 65), Message: multibyte}))
		check(t, e, uid(1))
	})

	t.Run("result step and failure step", func(t *testing.T) {
		e := newLCEnv(t)
		e.seed(uid(1), "dev-1", "docker_update", "running", nil)
		e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_result",
			models.CommandResult{CommandID: uid(1), Status: "error", Step: "BAD STEP", Reason: "hacked", Message: "m"}))
		got := e.get(uid(1))
		assert.Equal(t, "failed", got.Status)
		assert.Equal(t, "", got.FailureStep)
		assert.Equal(t, "", got.Step)
	})
}

func TestCL_SEC001_OutputCappedServerSide(t *testing.T) {
	big := "HEAD" + strings.Repeat("x", 2<<20) + "TAIL"

	t.Run("WS 2 MiB", func(t *testing.T) {
		e := newLCEnv(t)
		e.seed(uid(1), "dev-1", "fetch_logs", "dispatched", nil)
		e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_result",
			models.CommandResult{CommandID: uid(1), Status: "success", Output: big}))
		outs := e.repo.Outputs[uid(1)]
		require.Len(t, outs, 1)
		assert.LessOrEqual(t, len(outs[0].Content), models.MaxReportOutputBytes, "[SEC-001] stored output ≤ 256 KiB")
		assert.True(t, strings.HasPrefix(outs[0].Content, "HEAD") && strings.HasSuffix(outs[0].Content, "TAIL"), "head and tail are kept")
	})

	t.Run("HTTP 400 KiB", func(t *testing.T) {
		e := newLCEnv(t)
		e.seed(uid(1), "dev-1", "fetch_logs", "dispatched", nil)
		body, err := json.Marshal(models.CommandReport{Kind: models.ReportKindResult,
			Result: &models.CommandResult{Status: "success", Output: "HEAD" + strings.Repeat("y", 400<<10) + "TAIL"}})
		require.NoError(t, err)
		rec := e.report("dev-1", uid(1), string(body))
		require.Equal(t, http.StatusOK, rec.Code)
		outs := e.repo.Outputs[uid(1)]
		require.Len(t, outs, 1)
		assert.LessOrEqual(t, len(outs[0].Content), models.MaxReportOutputBytes)
	})
}

func TestCL_SEC_ReportBodyOver512KiB413(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-1", "fetch_logs", "dispatched", nil)
	body := `{"kind":"progress","state":"running","message":"` + strings.Repeat("m", 600<<10) + `"}`

	rec := e.report("dev-1", uid(1), body)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, "an oversize report body is 413")
	assert.Equal(t, "dispatched", e.get(uid(1)).Status)
	assert.Zero(t, e.repo.CallCount("Transition"))
}

func TestCL_SEC_BodyCommandIDMismatch400(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-1", "fetch_logs", "dispatched", nil)
	e.seed(uid(2), "dev-1", "fetch_logs", "dispatched", nil)

	rec := e.report("dev-1", uid(1), `{"kind":"ack","command_id":"`+uid(2)+`"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code, "the body id must equal the path id")
	assert.Equal(t, "dispatched", e.get(uid(1)).Status)
	assert.Equal(t, "dispatched", e.get(uid(2)).Status, "the other command must not be touched through the body id")
	assert.Empty(t, e.repo.Calls())
}

func TestReport_BadKindAndBadJSONAre400(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-1", "fetch_logs", "dispatched", nil)
	for _, body := range []string{`{"kind":"explode"}`, `{"kind":"progress","state":"succeeded"}`, `{"kind":"result"}`, `{not json`, ``} {
		rec := e.report("dev-1", uid(1), body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "body %q", body)
	}
	assert.Zero(t, e.repo.CallCount("Transition"))
}

func TestCL_SEC001_ReportRateLimitedPerDevice(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-1", "fetch_logs", "dispatched", nil)

	for i := 0; i < commands.ReportBurst; i++ {
		rec := e.report("dev-1", uid(1), `{"kind":"progress","state":"running","step":"pull"}`)
		require.Equal(t, http.StatusOK, rec.Code, "request %d is inside the burst", i+1)
	}
	limited := e.report("dev-1", uid(1), `{"kind":"progress","state":"running","step":"pull"}`)
	assert.Equal(t, http.StatusTooManyRequests, limited.Code, "[SEC-001] the next report is 429")
	assert.Contains(t, limited.Body.String(), `"code":"rate_limited"`)

	transitions := e.repo.CallCount("Transition")
	e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_result",
		models.CommandResult{CommandID: uid(1), Status: "success"}))
	assert.Equal(t, transitions, e.repo.CallCount("Transition"), "[SEC-001] the WS path shares the budget: the result is dropped")
	assert.Empty(t, e.sock.receipts(t), "[SEC-001] ... with no receipt")
	assert.Equal(t, "running", e.get(uid(1)).Status)

	e.seed(uid(2), "dev-2", "fetch_logs", "dispatched", nil)
	other := e.report("dev-2", uid(2), `{"kind":"ack"}`)
	assert.Equal(t, http.StatusOK, other.Code, "[SEC-001] device B must be unaffected while A is limited")

	e.clock.Advance(time.Minute)
	assert.Equal(t, http.StatusOK, e.report("dev-1", uid(1), `{"kind":"progress","state":"running","step":"pull"}`).Code, "the budget refills")
}

func TestCL_SEC_ForeignAndUnknownReportIndistinguishable(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-2", "docker_update", "dispatched", nil) // foreign to dev-1
	before := e.get(uid(1))

	bodies := map[string]string{
		"ack":      `{"kind":"ack"}`,
		"progress": `{"kind":"progress","state":"running","step":"pull"}`,
		"result":   `{"kind":"result","result":{"status":"success","message":"x","output":"y"}}`,
	}
	for kind, body := range bodies {
		foreign := e.report("dev-1", uid(1), body)
		unknown := e.report("dev-1", uid(77), body)
		assert.Equal(t, unknown.Code, foreign.Code, "%s: status must match", kind)
		assert.Equal(t, strings.ReplaceAll(unknown.Body.String(), uid(77), uid(1)), foreign.Body.String(), "%s: body must match byte for byte", kind)
	}
	assert.Equal(t, before, e.get(uid(1)), "no row change")
	assert.Empty(t, e.bc.Updates(), "no broadcast")
	assert.Empty(t, e.repo.Outputs, "no command_output write")

	// And the WS receipts for a result.
	for _, id := range []string{uid(1), uid(77)} {
		e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_result", models.CommandResult{CommandID: id, Status: "success"}))
	}
	receipts := e.sock.receipts(t)
	require.Len(t, receipts, 2)
	assert.Equal(t, receipts[0].Result, receipts[1].Result)
	assert.Equal(t, receipts[0].Status, receipts[1].Status)
}

func TestSendCommand_BodyAndActionValidation(t *testing.T) {
	e := newLCEnv(t)
	e.disconnect()
	assert.Equal(t, http.StatusBadRequest, e.call("POST", "/api/v1/devices/dev-1/commands", `{nope`).Code)
	assert.Equal(t, http.StatusBadRequest, e.call("POST", "/api/v1/devices/dev-1/commands", `{"action":"rm_rf"}`).Code)
	assert.Equal(t, http.StatusBadRequest, e.call("POST", "/api/v1/devices/dev-1/docker/bulk-update", `{"container_ids":[]}`).Code)
	assert.Equal(t, http.StatusBadRequest, e.call("POST", "/api/v1/devices/dev-1/docker/bulk-update", `{"container_ids":[""]}`).Code)
	assert.Zero(t, e.repo.CallCount("CreateIfNoConflict"))
}

// ---------------------------------------------------------------------------
// Agent contract: what the agent's outbox relies on (WP-GO-AGENT)
// ---------------------------------------------------------------------------

// TestContract_AgentReportResponsesCarryCodeAndEchoID pins the HTTP semantics the
// agent outbox acts on: a 2xx must echo command_id (otherwise the entry is
// kept), and every error response of the report route carries a JSON code (a
// 404 or 405 without one makes the agent assume a legacy server and go WS-only
// for an hour).
func TestContract_AgentReportResponsesCarryCodeAndEchoID(t *testing.T) {
	type tc struct {
		name     string
		prepare  func(e *lcEnv)
		id       string
		body     string
		wantCode int
		wantJSON string // the "code" value, "" for 2xx
	}
	okResult := `{"kind":"result","result":{"status":"success","outcome":"succeeded"}}`
	cases := []tc{
		{"accepted ack", func(e *lcEnv) { e.seed(uid(1), "dev-1", "fetch_logs", "dispatched", nil) }, uid(1), `{"kind":"ack"}`, 200, ""},
		{"duplicate result", func(e *lcEnv) { e.seed(uid(1), "dev-1", "fetch_logs", "succeeded", nil) }, uid(1), okResult, 200, ""},
		{"coalesced progress", func(e *lcEnv) {
			e.seed(uid(1), "dev-1", "docker_update", "dispatched", nil)
			e.report("dev-1", uid(1), `{"kind":"progress","state":"running","step":"pull"}`)
		}, uid(1), `{"kind":"progress","state":"running","step":"pull"}`, 200, ""},
		{"already terminal", func(e *lcEnv) { e.seed(uid(1), "dev-1", "fetch_logs", "failed", nil) }, uid(1), okResult, 409, "already_terminal"},
		{"invalid transition", func(e *lcEnv) { e.seed(uid(1), "dev-1", "fetch_logs", "running", nil) }, uid(1), `{"kind":"ack"}`, 409, "invalid_transition"},
		{"unknown command", func(e *lcEnv) {}, uid(9), `{"kind":"ack"}`, 404, "command_not_found"},
		{"foreign command", func(e *lcEnv) { e.seed(uid(1), "dev-2", "fetch_logs", "dispatched", nil) }, uid(1), `{"kind":"ack"}`, 404, "command_not_found"},
		{"malformed id", func(e *lcEnv) {}, "not-a-uuid", `{"kind":"ack"}`, 400, "invalid_report"},
		{"bad kind", func(e *lcEnv) {}, uid(1), `{"kind":"x"}`, 400, "invalid_report"},
		{"body too large", func(e *lcEnv) {}, uid(1), `{"kind":"progress","state":"running","message":"` + strings.Repeat("m", 600<<10) + `"}`, 413, "invalid_report"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newLCEnv(t)
			c.prepare(e)

			rec := e.report("dev-1", c.id, c.body)

			require.Equal(t, c.wantCode, rec.Code, rec.Body.String())
			var body map[string]interface{}
			decode(t, rec, &body)
			if c.wantJSON == "" {
				assert.Equal(t, c.id, body["command_id"], "a 2xx must echo command_id or the agent keeps the entry")
				assert.NotEmpty(t, body["result"])
				return
			}
			assert.Equal(t, c.wantJSON, body["code"], "an error response must carry its JSON code")
		})
	}

	t.Run("rate limited carries its code", func(t *testing.T) {
		e := newLCEnv(t)
		for i := 0; i < commands.ReportBurst; i++ {
			e.report("dev-1", uid(1), `{"kind":"ack"}`)
		}
		rec := e.report("dev-1", uid(1), `{"kind":"ack"}`)
		require.Equal(t, http.StatusTooManyRequests, rec.Code)
		assert.Contains(t, rec.Body.String(), `"code":"rate_limited"`)
	})
}

// TestContract_WSReceiptEchoesCommandID: the agent removes the outbox entry
// whose id the receipt names.
func TestContract_WSReceiptEchoesCommandID(t *testing.T) {
	e := newLCEnv(t)
	e.seed(uid(1), "dev-1", "docker_update", "running", nil)

	e.h.handleAgentMessage(context.Background(), e.conn, wsMsg(t, "command_result", models.CommandResult{
		CommandID: uid(1), Status: "error", Outcome: models.OutcomeFailed, Step: "recreate", Stderr: "boom", Reason: models.ReasonRolledBack, Message: "rolled back to previous container",
	}))

	receipts := e.sock.receipts(t)
	require.Len(t, receipts, 1)
	assert.Equal(t, uid(1), receipts[0].CommandID)
	got := e.get(uid(1))
	assert.Equal(t, "failed", got.Status)
	assert.Equal(t, "recreate", got.FailureStep)
	assert.Equal(t, "boom", got.StderrTail)
}
