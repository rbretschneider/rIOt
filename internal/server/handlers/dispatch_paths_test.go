package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// pathEnv wires Handlers built by New (so the default command service is used)
// for the seven command-creating paths.
type pathEnv struct {
	t      *testing.T
	h      *Handlers
	repo   *testutil.MockCommandRepo
	reg    *agentRegistry
	sock   *fakeSocket
	policy *testutil.MockAutoUpdateRepo
}

func newPathEnv(t *testing.T, connected bool) *pathEnv {
	t.Helper()
	e := &pathEnv{t: t, repo: testutil.NewMockCommandRepo(), reg: newAgentRegistry(), sock: &fakeSocket{}, policy: testutil.NewMockAutoUpdateRepo()}

	devs := testutil.NewMockDeviceRepo()
	devs.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "host-1", Status: models.DeviceStatusOnline, AutoPatch: true, AgentVersion: "1.0.0"}

	tel := testutil.NewMockTelemetryRepo()
	tel.Snapshots["dev-1"] = []models.TelemetrySnapshot{{DeviceID: "dev-1", Data: models.FullTelemetryData{
		Docker:  &models.DockerInfo{Containers: []models.ContainerInfo{{ID: "id-web", Name: "web"}}},
		Updates: &models.UpdateInfo{PendingUpdates: 3},
	}}}

	admin := testutil.NewMockAdminRepo("hash")
	cfg, err := json.Marshal(models.AutomationConfig{
		OSPatch:      models.MaintenanceWindow{Mode: "anytime", CooldownMinutes: 60},
		DockerUpdate: models.MaintenanceWindow{Mode: "anytime", CooldownMinutes: 60},
	})
	require.NoError(t, err)
	require.NoError(t, admin.SetConfig(context.Background(), "automation_config", string(cfg)))

	probes := testutil.NewMockDeviceProbeRepo()
	require.NoError(t, probes.Create(context.Background(), &models.DeviceProbe{DeviceID: "dev-1", Type: "http", Enabled: true}))

	hub := websocket.NewHub()
	go hub.Run()
	gen := events.NewGenerator(testutil.NewMockEventRepo(), hub, testutil.NewMockAlertRuleRepo(), testutil.NewMockDispatcher(), e.repo)

	e.h = New(HandlerDeps{
		Devices: devs, Telemetry: tel, Events: testutil.NewMockEventRepo(), Hub: hub, EventGen: gen,
		AdminRepo: admin, CommandRepo: e.repo, AutoUpdateRepo: e.policy, DeviceProbeRepo: probes,
	})
	e.h.agents = e.reg
	if connected {
		e.reg.replace("dev-1", &AgentConn{DeviceID: "dev-1", Conn: e.sock})
	}
	return e
}

// post runs a handler behind a chi router so URL params resolve.
func (e *pathEnv) post(pattern, url, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	e.t.Helper()
	r := chi.NewRouter()
	r.Post(pattern, handler)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func (e *pathEnv) commandsOf(action string) []models.Command {
	var out []models.Command
	for _, c := range e.repo.All() {
		if c.Action == action {
			out = append(out, c)
		}
	}
	return out
}

func TestCL_AC075_AllDispatchPathsUniform(t *testing.T) {
	ctx := context.Background()
	paths := []struct {
		name     string
		action   string
		conflict bool // a second run while the first is non-terminal must not create a row
		run      func(e *pathEnv)
	}{
		{"SendCommand", "reboot", true, func(e *pathEnv) {
			rec := e.post("/api/v1/devices/{id}/commands", "/api/v1/devices/dev-1/commands", `{"action":"reboot"}`, e.h.SendCommand)
			assert.Contains(t, []int{http.StatusCreated, http.StatusConflict}, rec.Code)
		}},
		{"BulkDockerUpdate", "docker_bulk_update", true, func(e *pathEnv) {
			rec := e.post("/api/v1/devices/{id}/docker/bulk-update", "/api/v1/devices/dev-1/docker/bulk-update", `{"container_ids":["id-web"]}`, e.h.BulkDockerUpdate)
			assert.Contains(t, []int{http.StatusCreated, http.StatusConflict}, rec.Code)
		}},
		{"dispatchAutoUpdate", "docker_update", true, func(e *pathEnv) {
			e.h.dispatchAutoUpdate(ctx, "dev-1", &models.AutoUpdatePolicy{ID: 7, DeviceID: "dev-1", Target: "web"}, time.Time{}, map[string]interface{}{"container": "web"})
		}},
		{"checkAutoPatch", "os_update", true, func(e *pathEnv) {
			e.h.checkAutoPatch(ctx, "dev-1", &models.FullTelemetryData{Updates: &models.UpdateInfo{PendingUpdates: 3}})
		}},
		{"RunDeviceProbe", "run_device_probe", false, func(e *pathEnv) {
			rec := e.post("/api/v1/devices/{id}/device-probes/{pid}/run", "/api/v1/devices/dev-1/device-probes/1/run", "", e.h.RunDeviceProbe)
			assert.Equal(t, http.StatusCreated, rec.Code)
		}},
		{"BulkUpdateAgents", "agent_update", true, func(e *pathEnv) {
			rec := e.post("/api/v1/fleet/bulk-update", "/api/v1/fleet/bulk-update", `{"version":"1.0.0"}`, e.h.BulkUpdateAgents)
			assert.Equal(t, http.StatusOK, rec.Code)
		}},
		{"BulkPatchDevices", "os_update", true, func(e *pathEnv) {
			rec := e.post("/api/v1/fleet/bulk-patch", "/api/v1/fleet/bulk-patch", `{}`, e.h.BulkPatchDevices)
			assert.Equal(t, http.StatusOK, rec.Code)
		}},
	}

	for _, p := range paths {
		t.Run(p.name+"/queued when the agent is offline", func(t *testing.T) {
			e := newPathEnv(t, false)
			p.run(e)

			cmds := e.commandsOf(p.action)
			require.Len(t, cmds, 1, "[AC-075] %s must create exactly one %s command", p.name, p.action)
			c := cmds[0]
			assert.True(t, c.ExpiresAt.After(time.Now().UTC()), "[AC-075] %s must set expires_at", p.name)
			assert.Positive(t, c.ExecDeadlineS, "[AC-075] %s must stamp the execution deadline", p.name)
			assert.Equal(t, "queued", c.Status)
			assert.Zero(t, e.repo.CallCount("ClaimByID"), "an offline agent means no claim")
			assert.Equal(t, 1, e.repo.CallCount("CreateIfNoConflict"), "[AC-075] creation always goes through the conflict-checked path")
		})

		t.Run(p.name+"/delivered only via claim when connected", func(t *testing.T) {
			e := newPathEnv(t, true)
			p.run(e)

			cmds := e.commandsOf(p.action)
			require.Len(t, cmds, 1)
			assert.Equal(t, "dispatched", cmds[0].Status)
			require.NotNil(t, cmds[0].DeadlineAt, "[AC-075] a WS delivery goes through the claim, which stamps the deadline")
			assert.Equal(t, 1, e.repo.CallCount("ClaimByID"))
			sent := e.sock.messages("command")
			require.Len(t, sent, 1)
			var payload models.CommandPayload
			require.NoError(t, json.Unmarshal(sent[0].Data, &payload))
			assert.Equal(t, cmds[0].ID, payload.CommandID, "the payload sent is the claimed command")
		})

		t.Run(p.name+"/second run while non-terminal", func(t *testing.T) {
			e := newPathEnv(t, false)
			p.run(e)
			p.run(e)

			n := len(e.commandsOf(p.action))
			if p.conflict {
				assert.Equal(t, 1, n, "[AC-075] %s must skip or 409 on conflict and create no second row", p.name)
			} else {
				assert.Equal(t, 2, n, "%s holds no target key, so it never conflicts", p.name)
			}
		})
	}
}

func TestCL_AC075_ManualConflictIs409WithExistingID(t *testing.T) {
	e := newPathEnv(t, false)
	first := e.post("/api/v1/devices/{id}/commands", "/api/v1/devices/dev-1/commands", `{"action":"reboot"}`, e.h.SendCommand)
	require.Equal(t, http.StatusCreated, first.Code)
	var created models.Command
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &created))

	second := e.post("/api/v1/devices/{id}/commands", "/api/v1/devices/dev-1/commands", `{"action":"reboot"}`, e.h.SendCommand)

	assert.Equal(t, http.StatusConflict, second.Code, "[AC-070] a second reboot while the first is non-terminal gets 409")
	var body models.CommandConflictResponse
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &body))
	assert.Equal(t, "conflict", body.Code)
	assert.Equal(t, created.ID, body.ExistingCommandID, "[AC-068] the 409 must name the existing command")
	assert.Equal(t, "queued", body.ExistingStatus)
	assert.Equal(t, "host", body.TargetKey)
	assert.Equal(t, models.ConflictMessage, body.Error)
	assert.Len(t, e.repo.Commands, 1, "[AC-068] no row may be created")
}

func TestAutoDispatch_CarriesOriginAndPolicy(t *testing.T) {
	e := newPathEnv(t, false)
	e.h.dispatchAutoUpdate(context.Background(), "dev-1", &models.AutoUpdatePolicy{ID: 7, DeviceID: "dev-1", Target: "web"}, time.Time{}, map[string]interface{}{"container": "web"})
	e.h.checkAutoPatch(context.Background(), "dev-1", &models.FullTelemetryData{Updates: &models.UpdateInfo{PendingUpdates: 1}})

	upd := e.commandsOf("docker_update")
	require.Len(t, upd, 1)
	assert.Equal(t, models.OriginAutoUpdate, upd[0].Origin)
	require.NotNil(t, upd[0].PolicyID)
	assert.Equal(t, 7, *upd[0].PolicyID)
	patch := e.commandsOf("os_update")
	require.Len(t, patch, 1)
	assert.Equal(t, models.OriginAutoPatch, patch[0].Origin)
	assert.Nil(t, patch[0].PolicyID)
}

func TestFleetBulk_ReportsConflictsCount(t *testing.T) {
	e := newPathEnv(t, false)
	e.post("/api/v1/fleet/bulk-update", "/api/v1/fleet/bulk-update", `{"version":"1.0.0"}`, e.h.BulkUpdateAgents)

	rec := e.post("/api/v1/fleet/bulk-update", "/api/v1/fleet/bulk-update", `{"version":"1.0.0"}`, e.h.BulkUpdateAgents)

	var body map[string]int
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 1, body["conflicts"], "[AC-071 contract] bulk responses add a conflicts count")
	assert.Equal(t, 0, body["queued"])
	assert.Equal(t, 1, body["total"])
}
