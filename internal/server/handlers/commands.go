package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/commands"
	"github.com/go-chi/chi/v5"
)

var validActions = map[string]bool{
	"docker_stop":          true,
	"docker_restart":       true,
	"docker_start":         true,
	"docker_update":        true,
	"docker_check_updates": true,
	"reboot":               true,
	"shutdown":             true,
	"agent_update":         true,
	"os_update":            true,
	"fetch_logs":           true,
	"enable_auto_updates":  true,
	"docker_bulk_update":   true,
	"run_device_probe":     true,
}

// SendCommand handles POST /api/v1/devices/{id}/commands.
func (h *Handlers) SendCommand(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")

	var req struct {
		Action string                 `json:"action"`
		Params map[string]interface{} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if !validActions[req.Action] {
		http.Error(w, `{"error":"invalid action"}`, http.StatusBadRequest)
		return
	}
	if req.Params == nil {
		req.Params = make(map[string]interface{})
	}
	// Manual dispatches never carry include_reboot_class (PATCH-GATE FR-023,
	// BR-005): the only release path for reboot-class holds is the in-window
	// automated run dispatched by checkAutoPatch. Enforced at the API
	// boundary rather than trusting UI callers.
	if req.Action == "os_update" {
		delete(req.Params, "include_reboot_class")
	}

	res, err := h.cmdSvc().Dispatch(r.Context(), commands.DispatchRequest{
		DeviceID: deviceID, Action: req.Action, Params: req.Params,
	})
	if err != nil {
		slog.Error("dispatch command", "device_id", deviceID, "action", req.Action, "error", err.Error())
		http.Error(w, `{"error":"failed to create command"}`, http.StatusInternalServerError)
		return
	}
	if res.Conflict != nil {
		writeCommandConflict(w, res.Conflict, res.ConflictKey)
		return
	}
	cmd := res.Command

	// Emit informational event for all commands
	if device, err := h.devices.GetByID(r.Context(), deviceID); err == nil {
		h.eventGen.CommandSent(r.Context(), deviceID, device.Hostname, req.Action, req.Params)
	}

	slog.Info("command created", "id", cmd.ID, "device", deviceID, "action", req.Action, "status", cmd.Status)
	writeJSON(w, http.StatusCreated, cmd)
}

// writeCommandConflict answers 409 for a create that hit an in-flight command
// on the same target (BR-008).
func writeCommandConflict(w http.ResponseWriter, existing *models.Command, targetKey string) {
	writeJSON(w, http.StatusConflict, models.CommandConflictResponse{
		Error:             models.ConflictMessage,
		Code:              models.CodeConflict,
		ExistingCommandID: existing.ID,
		ExistingStatus:    existing.Status,
		TargetKey:         targetKey,
	})
}

// BulkDockerUpdate handles POST /api/v1/devices/{id}/docker/bulk-update.
// Targets that conflict with an in-flight command are skipped and reported; the
// rest get one docker_bulk_update. When every target conflicts the answer is 409.
func (h *Handlers) BulkDockerUpdate(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")

	var req struct {
		ContainerIDs []string `json:"container_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if len(req.ContainerIDs) == 0 {
		http.Error(w, `{"error":"container_ids is required"}`, http.StatusBadRequest)
		return
	}

	res, err := h.cmdSvc().DispatchBulkDocker(r.Context(), deviceID, req.ContainerIDs)
	if err != nil {
		slog.Error("dispatch bulk update", "device_id", deviceID, "error", err.Error())
		http.Error(w, `{"error":"failed to create command"}`, http.StatusInternalServerError)
		return
	}
	conflicts := res.Conflicts
	if conflicts == nil {
		conflicts = []models.BulkConflict{}
	}
	if res.Command == nil && len(conflicts) == 0 {
		http.Error(w, `{"error":"container_ids is required"}`, http.StatusBadRequest)
		return
	}
	if res.Command == nil {
		first := conflicts[0]
		writeJSON(w, http.StatusConflict, models.CommandConflictResponse{
			Error:             models.ConflictMessage,
			Code:              models.CodeConflict,
			ExistingCommandID: first.ExistingCommandID,
			TargetKey:         first.TargetKey,
			Conflicts:         conflicts,
		})
		return
	}
	writeJSON(w, http.StatusCreated, models.BulkCommandResponse{Command: *res.Command, Conflicts: conflicts})
}

// ListDeviceCommands handles GET /api/v1/devices/{id}/commands.
func (h *Handlers) ListDeviceCommands(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	statusParam := r.URL.Query().Get("status")
	var statuses []string
	if statusParam != "" {
		for _, s := range strings.Split(statusParam, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				statuses = append(statuses, s)
			}
		}
	}
	action := r.URL.Query().Get("action")

	var commands []models.Command
	var err error
	if len(statuses) > 0 || action != "" || offset > 0 {
		commands, err = h.commandRepo.ListByDeviceFiltered(r.Context(), deviceID, limit, offset, statuses, action, "")
	} else {
		commands, err = h.commandRepo.ListByDevice(r.Context(), deviceID, limit)
	}
	if err != nil {
		http.Error(w, `{"error":"failed to list commands"}`, http.StatusInternalServerError)
		return
	}
	if commands == nil {
		commands = []models.Command{}
	}
	writeJSON(w, http.StatusOK, commands)
}

// GetCommandOutput handles GET /api/v1/devices/{id}/commands/{commandId}/output.
func (h *Handlers) GetCommandOutput(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	commandID := chi.URLParam(r, "commandId")

	cmd, err := h.commandRepo.GetByID(r.Context(), commandID)
	if err != nil {
		http.Error(w, `{"error":"command not found"}`, http.StatusNotFound)
		return
	}
	if cmd.DeviceID != deviceID {
		http.Error(w, `{"error":"command does not belong to this device"}`, http.StatusForbidden)
		return
	}

	outputs, err := h.commandRepo.GetCommandOutput(r.Context(), commandID)
	if err != nil {
		http.Error(w, `{"error":"failed to get command output"}`, http.StatusInternalServerError)
		return
	}
	if outputs == nil {
		outputs = []models.CommandOutput{}
	}
	writeJSON(w, http.StatusOK, outputs)
}
