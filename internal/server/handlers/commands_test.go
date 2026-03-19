package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
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
		assert.Contains(t, []string{"success", "error"}, c.Status)
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

func TestMockCommandRepo_UpdateCommandResult(t *testing.T) {
	repo := testutil.NewMockCommandRepo()
	repo.Commands["cmd-1"] = &models.Command{
		ID: "cmd-1", DeviceID: "dev-1", Action: "os_update", Status: "sent",
	}

	dur := int64(5000)
	ec := 0
	err := repo.UpdateCommandResult(nil, "cmd-1", "success", "done", &dur, &ec)
	require.NoError(t, err)

	cmd := repo.Commands["cmd-1"]
	assert.Equal(t, "success", cmd.Status)
	assert.Equal(t, "done", cmd.ResultMsg)
	assert.Equal(t, int64(5000), *cmd.DurationMs)
	assert.Equal(t, 0, *cmd.ExitCode)
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
	results, err := repo.ListByDeviceFiltered(nil, "dev-1", 50, 0, []string{"success"}, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "success", results[0].Status)

	// Filter by action
	results, err = repo.ListByDeviceFiltered(nil, "dev-1", 50, 0, nil, "os_update")
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Filter by both
	results, err = repo.ListByDeviceFiltered(nil, "dev-1", 50, 0, []string{"success"}, "os_update")
	require.NoError(t, err)
	assert.Len(t, results, 1)

	// Different device
	results, err = repo.ListByDeviceFiltered(nil, "dev-2", 50, 0, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
}
