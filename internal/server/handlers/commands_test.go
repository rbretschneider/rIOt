package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCommandTestHandlers(t *testing.T) (*Handlers, *testutil.MockCommandRepo, *testutil.MockDeviceRepo) {
	t.Helper()
	cmdRepo := testutil.NewMockCommandRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	hub := websocket.NewHub()
	go hub.Run()
	h := &Handlers{
		commandRepo: cmdRepo,
		devices:     deviceRepo,
		hub:         hub,
	}
	return h, cmdRepo, deviceRepo
}

func TestListDeviceCommands_Empty(t *testing.T) {
	h, _, _ := newCommandTestHandlers(t)

	r := chi.NewRouter()
	r.Get("/api/v1/devices/{id}/commands", h.ListDeviceCommands)

	req := httptest.NewRequest("GET", "/api/v1/devices/dev-1/commands", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var commands []models.Command
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&commands))
	assert.Empty(t, commands)
}

func TestListDeviceCommands_WithResults(t *testing.T) {
	h, cmdRepo, _ := newCommandTestHandlers(t)

	dur := int64(1500)
	ec := 0
	cmdRepo.Commands["cmd-1"] = &models.Command{
		ID: "cmd-1", DeviceID: "dev-1", Action: "os_update",
		Status: "success", ResultMsg: "Updated 5 packages",
		DurationMs: &dur, ExitCode: &ec,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	r := chi.NewRouter()
	r.Get("/api/v1/devices/{id}/commands", h.ListDeviceCommands)

	req := httptest.NewRequest("GET", "/api/v1/devices/dev-1/commands", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var commands []models.Command
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&commands))
	assert.Len(t, commands, 1)
	assert.Equal(t, "cmd-1", commands[0].ID)
	assert.NotNil(t, commands[0].DurationMs)
	assert.Equal(t, int64(1500), *commands[0].DurationMs)
	assert.NotNil(t, commands[0].ExitCode)
	assert.Equal(t, 0, *commands[0].ExitCode)
}

func TestListDeviceCommands_WithStatusFilter(t *testing.T) {
	h, cmdRepo, _ := newCommandTestHandlers(t)

	cmdRepo.Commands["cmd-1"] = &models.Command{
		ID: "cmd-1", DeviceID: "dev-1", Action: "os_update", Status: "success",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	cmdRepo.Commands["cmd-2"] = &models.Command{
		ID: "cmd-2", DeviceID: "dev-1", Action: "reboot", Status: "error",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	cmdRepo.Commands["cmd-3"] = &models.Command{
		ID: "cmd-3", DeviceID: "dev-1", Action: "os_update", Status: "pending",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	r := chi.NewRouter()
	r.Get("/api/v1/devices/{id}/commands", h.ListDeviceCommands)

	req := httptest.NewRequest("GET", "/api/v1/devices/dev-1/commands?status=success,error", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var commands []models.Command
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&commands))
	assert.Len(t, commands, 2)
	for _, c := range commands {
		assert.Contains(t, []string{"succeeded", "failed"}, c.Status)
	}
}

func TestListDeviceCommands_WithActionFilter(t *testing.T) {
	h, cmdRepo, _ := newCommandTestHandlers(t)

	cmdRepo.Commands["cmd-1"] = &models.Command{
		ID: "cmd-1", DeviceID: "dev-1", Action: "os_update", Status: "success",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	cmdRepo.Commands["cmd-2"] = &models.Command{
		ID: "cmd-2", DeviceID: "dev-1", Action: "reboot", Status: "success",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	r := chi.NewRouter()
	r.Get("/api/v1/devices/{id}/commands", h.ListDeviceCommands)

	req := httptest.NewRequest("GET", "/api/v1/devices/dev-1/commands?action=os_update", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var commands []models.Command
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&commands))
	assert.Len(t, commands, 1)
	assert.Equal(t, "os_update", commands[0].Action)
}

func TestListDeviceCommands_WithPagination(t *testing.T) {
	h, cmdRepo, _ := newCommandTestHandlers(t)

	for i := 0; i < 5; i++ {
		id := "cmd-" + string(rune('a'+i))
		cmdRepo.Commands[id] = &models.Command{
			ID: id, DeviceID: "dev-1", Action: "os_update", Status: "success",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
	}

	r := chi.NewRouter()
	r.Get("/api/v1/devices/{id}/commands", h.ListDeviceCommands)

	req := httptest.NewRequest("GET", "/api/v1/devices/dev-1/commands?limit=2&offset=2", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var commands []models.Command
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&commands))
	// With offset=2 and limit=2, we should get at most 2 results (out of 5 minus offset 2 = 3 remaining)
	assert.LessOrEqual(t, len(commands), 2)
}

func TestGetCommandOutput_Success(t *testing.T) {
	h, cmdRepo, _ := newCommandTestHandlers(t)

	cmdRepo.Commands["cmd-1"] = &models.Command{
		ID: "cmd-1", DeviceID: "dev-1", Action: "os_update", Status: "success",
	}
	cmdRepo.Outputs = map[string][]models.CommandOutput{
		"cmd-1": {
			{ID: 1, CommandID: "cmd-1", Stream: "combined", Content: "some output", CreatedAt: time.Now()},
		},
	}

	r := chi.NewRouter()
	r.Get("/api/v1/devices/{id}/commands/{commandId}/output", h.GetCommandOutput)

	req := httptest.NewRequest("GET", "/api/v1/devices/dev-1/commands/cmd-1/output", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var outputs []models.CommandOutput
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&outputs))
	assert.Len(t, outputs, 1)
	assert.Equal(t, "some output", outputs[0].Content)
	assert.Equal(t, "combined", outputs[0].Stream)
}

func TestGetCommandOutput_NotFound(t *testing.T) {
	h, _, _ := newCommandTestHandlers(t)

	r := chi.NewRouter()
	r.Get("/api/v1/devices/{id}/commands/{commandId}/output", h.GetCommandOutput)

	req := httptest.NewRequest("GET", "/api/v1/devices/dev-1/commands/nonexistent/output", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetCommandOutput_WrongDevice(t *testing.T) {
	h, cmdRepo, _ := newCommandTestHandlers(t)

	cmdRepo.Commands["cmd-1"] = &models.Command{
		ID: "cmd-1", DeviceID: "dev-2", Action: "os_update", Status: "success",
	}

	r := chi.NewRouter()
	r.Get("/api/v1/devices/{id}/commands/{commandId}/output", h.GetCommandOutput)

	req := httptest.NewRequest("GET", "/api/v1/devices/dev-1/commands/cmd-1/output", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestGetCommandOutput_EmptyOutput(t *testing.T) {
	h, cmdRepo, _ := newCommandTestHandlers(t)

	cmdRepo.Commands["cmd-1"] = &models.Command{
		ID: "cmd-1", DeviceID: "dev-1", Action: "reboot", Status: "success",
	}

	r := chi.NewRouter()
	r.Get("/api/v1/devices/{id}/commands/{commandId}/output", h.GetCommandOutput)

	req := httptest.NewRequest("GET", "/api/v1/devices/dev-1/commands/cmd-1/output", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var outputs []models.CommandOutput
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&outputs))
	assert.Empty(t, outputs)
}

func TestMockCommandRepo_TransitionAppliesResult(t *testing.T) {
	repo := testutil.NewMockCommandRepo()
	repo.Commands["cmd-1"] = &models.Command{
		ID: "cmd-1", DeviceID: "dev-1", Action: "os_update", Status: "sent",
	}

	dur := int64(5000)
	ec := 0
	msg := "done"
	got, applied, err := repo.Transition(context.Background(), models.CommandTransition{
		ID: "cmd-1", DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandSucceeded,
		ResultMsg: &msg, DurationMs: &dur, ExitCode: &ec, Now: time.Now(),
	})
	require.NoError(t, err)
	require.True(t, applied, "a legacy sent row accepts a final result like dispatched")

	assert.Equal(t, "succeeded", got.Status)
	assert.Equal(t, "done", got.ResultMsg)
	assert.Equal(t, int64(5000), *got.DurationMs)
	assert.Equal(t, 0, *got.ExitCode)
	assert.Equal(t, "succeeded", repo.Commands["cmd-1"].Status, "the stored row is canonical after a transition")
}

func TestMockCommandRepo_SaveAndGetOutput(t *testing.T) {
	repo := testutil.NewMockCommandRepo()

	output := &models.CommandOutput{
		CommandID: "cmd-1",
		Stream:    "combined",
		Content:   "hello world",
	}
	err := repo.SaveCommandOutput(nil, output)
	require.NoError(t, err)
	assert.Equal(t, int64(1), output.ID)

	outputs, err := repo.GetCommandOutput(nil, "cmd-1")
	require.NoError(t, err)
	assert.Len(t, outputs, 1)
	assert.Equal(t, "hello world", outputs[0].Content)

	// No output for other command
	outputs, err = repo.GetCommandOutput(nil, "cmd-2")
	require.NoError(t, err)
	assert.Nil(t, outputs)
}

func TestMockCommandRepo_ListByDeviceFiltered(t *testing.T) {
	repo := testutil.NewMockCommandRepo()
	repo.Commands["cmd-1"] = &models.Command{ID: "cmd-1", DeviceID: "dev-1", Action: "os_update", Status: "success"}
	repo.Commands["cmd-2"] = &models.Command{ID: "cmd-2", DeviceID: "dev-1", Action: "reboot", Status: "error"}
	repo.Commands["cmd-3"] = &models.Command{ID: "cmd-3", DeviceID: "dev-1", Action: "os_update", Status: "pending"}
	repo.Commands["cmd-4"] = &models.Command{ID: "cmd-4", DeviceID: "dev-2", Action: "os_update", Status: "success"}

	// Filter by status
	results, err := repo.ListByDeviceFiltered(nil, "dev-1", 50, 0, []string{"success"}, "", "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "succeeded", results[0].Status)

	// Filter by action
	results, err = repo.ListByDeviceFiltered(nil, "dev-1", 50, 0, nil, "os_update", "")
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Filter by both
	results, err = repo.ListByDeviceFiltered(nil, "dev-1", 50, 0, []string{"success"}, "os_update", "")
	require.NoError(t, err)
	assert.Len(t, results, 1)

	// Different device
	results, err = repo.ListByDeviceFiltered(nil, "dev-2", 50, 0, nil, "", "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

// [AC-013 server half / SEC-PATCH-GATE-009] Manual os_update dispatches
// never carry include_reboot_class: SendCommand strips it at the API
// boundary (FR-023, BR-005 — the in-window automated run is the only
// release path).
func TestSendCommand_AC013_StripsIncludeRebootClass(t *testing.T) {
	h, cmdRepo, _ := newCommandTestHandlers(t)

	r := chi.NewRouter()
	r.Post("/api/v1/devices/{id}/commands", h.SendCommand)

	body := `{"action":"os_update","params":{"mode":"full","include_reboot_class":true}}`
	req := httptest.NewRequest("POST", "/api/v1/devices/dev-1/commands",
		strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Len(t, cmdRepo.Commands, 1)
	for _, cmd := range cmdRepo.Commands {
		_, present := cmd.Params["include_reboot_class"]
		assert.False(t, present, "[AC-013] stored manual os_update params must lack include_reboot_class")
		assert.Equal(t, "full", cmd.Params["mode"], "other params pass through")
	}
}

// The strip is scoped to os_update: other actions keep their params intact.
func TestSendCommand_StripScopedToOSUpdate(t *testing.T) {
	h, cmdRepo, _ := newCommandTestHandlers(t)

	r := chi.NewRouter()
	r.Post("/api/v1/devices/{id}/commands", h.SendCommand)

	body := `{"action":"fetch_logs","params":{"include_reboot_class":true,"hours":4}}`
	req := httptest.NewRequest("POST", "/api/v1/devices/dev-1/commands",
		strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	for _, cmd := range cmdRepo.Commands {
		_, present := cmd.Params["include_reboot_class"]
		assert.True(t, present, "non-os_update actions are not rewritten")
	}
}

// --- PATCH-FLOW-001: manual os_update reboot authorization (AD-004) ---

// noRowsTelemetry reports "no snapshot" the way the real repository does.
type noRowsTelemetry struct{ *testutil.MockTelemetryRepo }

func (noRowsTelemetry) GetLatestSnapshot(context.Context, string) (*models.TelemetrySnapshot, error) {
	return nil, pgx.ErrNoRows
}

// rebootCapableUpdates is the latest telemetry of an agent that allows reboots
// and understands reboot_if_required.
func rebootCapableUpdates() *models.UpdateInfo {
	allow := true
	return &models.UpdateInfo{PackageManager: "apt", AllowReboot: &allow, SupportsRebootIfRequired: true}
}

func newAuthTestHandlers(t *testing.T, updates *models.UpdateInfo) (*Handlers, *testutil.MockCommandRepo, *testutil.MockTelemetryRepo) {
	t.Helper()
	h, cmdRepo, _ := newCommandTestHandlers(t)
	tel := testutil.NewMockTelemetryRepo()
	if updates != nil {
		tel.Snapshots["dev-1"] = []models.TelemetrySnapshot{{
			DeviceID: "dev-1", Timestamp: time.Now(), Data: models.FullTelemetryData{Updates: updates},
		}}
	}
	h.telemetry = tel
	return h, cmdRepo, tel
}

func postCommand(t *testing.T, h *Handlers, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/v1/devices/{id}/commands", h.SendCommand)
	req := httptest.NewRequest("POST", "/api/v1/devices/dev-1/commands", strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func onlyCommand(t *testing.T, repo *testutil.MockCommandRepo) *models.Command {
	t.Helper()
	require.Len(t, repo.Commands, 1)
	for _, c := range repo.Commands {
		return c
	}
	return nil
}

func assertNoCommandCreated(t *testing.T, repo *testutil.MockCommandRepo, msg string) {
	t.Helper()
	assert.Empty(t, repo.Commands, msg)
	assert.Zero(t, repo.CallCount("CreateIfNoConflict"), msg+" (no Create call)")
	assert.Zero(t, repo.CallCount("Create"), msg+" (no Create call)")
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	return body.Code
}

func TestSendCommand_PF_AC012_AuthorizedDispatchSetsIncludeRebootClass(t *testing.T) {
	h, repo, _ := newAuthTestHandlers(t, rebootCapableUpdates())

	rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"full","reboot_if_required":true}}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	cmd := onlyCommand(t, repo)
	assert.Equal(t, true, cmd.Params["reboot_if_required"], "[AC-012] the command carries the authorization")
	assert.Equal(t, true, cmd.Params["include_reboot_class"], "[AC-012] the server derives include_reboot_class")
	assert.Equal(t, "full", cmd.Params["mode"])
}

func TestSendCommand_PF_AC013_FalseAuthorizationStoredAsAbsent(t *testing.T) {
	h, repo, _ := newAuthTestHandlers(t, nil)
	h.telemetry = nil // an unticked dispatch must not even read telemetry

	rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"full","reboot_if_required":false}}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, map[string]interface{}{"mode": "full"}, onlyCommand(t, repo).Params, "[AC-013] stored params equal today's")
}

func TestSendCommand_PF_AC013_AbsentAuthorizationUnchanged(t *testing.T) {
	h, repo, _ := newAuthTestHandlers(t, nil)
	h.telemetry = nil

	rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"security"}}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, map[string]interface{}{"mode": "security"}, onlyCommand(t, repo).Params)
}

func TestSendCommand_PF_AC014_ClientIncludeRebootClassStrippedWithoutAuthorization(t *testing.T) {
	bodies := map[string]string{
		"no authorization":    `{"action":"os_update","params":{"mode":"full","include_reboot_class":true}}`,
		"authorization false": `{"action":"os_update","params":{"mode":"full","include_reboot_class":true,"reboot_if_required":false}}`,
		"upper case variant":  `{"action":"os_update","params":{"mode":"full","INCLUDE_REBOOT_CLASS":true}}`,
		"mixed case variant":  `{"action":"os_update","params":{"mode":"full","Include_Reboot_Class":true}}`,
		"folded letter":       `{"action":"os_update","params":{"mode":"full","include_reboot_claſs":true}}`,
	}
	for name, body := range bodies {
		t.Run("[AC-014] "+name, func(t *testing.T) {
			h, repo, _ := newAuthTestHandlers(t, rebootCapableUpdates())
			rec := postCommand(t, h, body)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			for key := range onlyCommand(t, repo).Params {
				assert.False(t, strings.EqualFold(key, "include_reboot_class"), "[AC-014/C-4] key %q survived the API boundary", key)
			}
		})
	}
}

func TestSendCommand_PF_C4_ReservedKeyVariantsOfRebootIfRequiredRejected(t *testing.T) {
	for _, key := range []string{"Reboot_If_Required", "REBOOT_IF_REQUIRED", "reboot_if_requireD"} {
		t.Run("[C-4] "+key, func(t *testing.T) {
			h, repo, _ := newAuthTestHandlers(t, rebootCapableUpdates())
			rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"full","`+key+`":true}}`)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, "invalid_request", errorCode(t, rec))
			assertNoCommandCreated(t, repo, "[C-4] a case variant must not reach the agent")
		})
	}
}

func TestSendCommand_PF_AC014_AuthorizationValidationIsScopedToOSUpdate(t *testing.T) {
	h, repo, _ := newAuthTestHandlers(t, rebootCapableUpdates())

	rec := postCommand(t, h, `{"action":"fetch_logs","params":{"reboot_if_required":"yes","hours":4}}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "yes", onlyCommand(t, repo).Params["reboot_if_required"], "other actions are not validated or rewritten")
}

func TestSendCommand_PF_AC015_NonBooleanAuthorizationRejected(t *testing.T) {
	values := map[string]string{"string": `"true"`, "number": `1`, "null": `null`, "object": `{}`, "array": `[true]`, "zero": `0`}
	for name, value := range values {
		t.Run("[AC-015] "+name, func(t *testing.T) {
			h, repo, _ := newAuthTestHandlers(t, rebootCapableUpdates())
			rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"full","reboot_if_required":`+value+`}}`)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, "invalid_request", errorCode(t, rec))
			assertNoCommandCreated(t, repo, "[AC-015]")
		})
	}
}

func TestSendCommand_PF_AC015_RepairWithAuthorizationRejected(t *testing.T) {
	h, repo, _ := newAuthTestHandlers(t, rebootCapableUpdates())

	rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"repair","reboot_if_required":true}}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_request", errorCode(t, rec))
	assertNoCommandCreated(t, repo, "[AC-015] repair never upgrades, so a reboot authorization is meaningless")
}

func TestSendCommand_PF_AC016_AllowRebootFalseRejectedWithRebootNotAllowed(t *testing.T) {
	updates := rebootCapableUpdates()
	off := false
	updates.AllowReboot = &off
	h, repo, _ := newAuthTestHandlers(t, updates)

	rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"full","reboot_if_required":true}}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, models.CodeRebootNotAllowed, errorCode(t, rec), "[AC-016] a distinct code names the reason")
	assert.Contains(t, rec.Body.String(), "commands.allow_reboot: false")
	assertNoCommandCreated(t, repo, "[AC-016]")
}

func TestSendCommand_PF_AC016_MissingCapabilityRejectedWithUnsupported(t *testing.T) {
	cases := map[string]func(*models.UpdateInfo){
		"capability absent":  func(u *models.UpdateInfo) { u.SupportsRebootIfRequired = false },
		"allow_reboot unset": func(u *models.UpdateInfo) { u.AllowReboot = nil },
	}
	for name, mutate := range cases {
		t.Run("[AC-016] "+name, func(t *testing.T) {
			updates := rebootCapableUpdates()
			mutate(updates)
			h, repo, _ := newAuthTestHandlers(t, updates)
			rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"full","reboot_if_required":true}}`)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, models.CodeRebootAuthUnsupported, errorCode(t, rec))
			assert.NotEqual(t, "invalid_request", errorCode(t, rec), "[AC-016] distinct from invalid_request")
			assertNoCommandCreated(t, repo, "[AC-016] a spoofable agent fact can only reject, never authorize")
		})
	}
}

func TestSendCommand_PF_AC016_NoTelemetryRejectedWithUnsupported(t *testing.T) {
	const ticked = `{"action":"os_update","params":{"mode":"full","reboot_if_required":true}}`
	t.Run("[AC-016] no snapshot at all", func(t *testing.T) {
		h, repo, tel := newAuthTestHandlers(t, nil)
		h.telemetry = noRowsTelemetry{tel}
		rec := postCommand(t, h, ticked)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, models.CodeRebootAuthUnsupported, errorCode(t, rec))
		assertNoCommandCreated(t, repo, "[AC-016]")
	})
	t.Run("[AC-016] snapshot without an updates section", func(t *testing.T) {
		h, repo, tel := newAuthTestHandlers(t, nil)
		tel.Snapshots["dev-1"] = []models.TelemetrySnapshot{{DeviceID: "dev-1", Timestamp: time.Now()}}
		rec := postCommand(t, h, ticked)
		assert.Equal(t, models.CodeRebootAuthUnsupported, errorCode(t, rec))
		assertNoCommandCreated(t, repo, "[AC-016]")
	})
	t.Run("[AC-016] no telemetry source configured", func(t *testing.T) {
		h, repo, _ := newAuthTestHandlers(t, nil)
		h.telemetry = nil
		rec := postCommand(t, h, ticked)
		assert.Equal(t, models.CodeRebootAuthUnsupported, errorCode(t, rec))
		assertNoCommandCreated(t, repo, "[AC-016]")
	})
}

func TestSendCommand_PF_AC016_TelemetryErrorIs500NoCommand(t *testing.T) {
	h, repo, tel := newAuthTestHandlers(t, rebootCapableUpdates())
	tel.Err = errors.New("database down")

	rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"full","reboot_if_required":true}}`)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to create command")
	assertNoCommandCreated(t, repo, "[AC-016] a fault must not turn into an authorization")
}

func TestSendCommand_PF_AC037_StoredParamsShowAuthorizationAndIncludeRebootClass(t *testing.T) {
	h, repo, _ := newAuthTestHandlers(t, rebootCapableUpdates())

	rec := postCommand(t, h, `{"action":"os_update","params":{"mode":"full","reboot_if_required":true}}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created models.Command
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, true, created.Params["reboot_if_required"], "[AC-037] the response shows the authorization")
	assert.Equal(t, true, created.Params["include_reboot_class"], "[AC-037] and the derived flag")
	stored := onlyCommand(t, repo)
	assert.Equal(t, true, stored.Params["reboot_if_required"])
	assert.Equal(t, true, stored.Params["include_reboot_class"])
}
