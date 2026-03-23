package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var validActions = map[string]bool{
	"docker_stop":          true,
	"docker_restart":       true,
	"docker_start":         true,
	"docker_update":        true,
	"docker_check_updates": true,
	"reboot":               true,
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

	// Create command record
	cmd := &models.Command{
		ID:       uuid.New().String(),
		DeviceID: deviceID,
		Action:   req.Action,
		Params:   req.Params,
		Status:   "pending",
	}
	if err := h.commandRepo.Create(r.Context(), cmd); err != nil {
		slog.Error("create command", "error", err.Error())
		http.Error(w, `{"error":"failed to create command"}`, http.StatusInternalServerError)
		return
	}

	// Try to send via WS if agent is connected; otherwise queue for heartbeat pickup
	agentConnections.RLock()
	ac := agentConnections.m[deviceID]
	agentConnections.RUnlock()

	if ac != nil {
		payload := models.CommandPayload{
			CommandID: cmd.ID,
			Action:    cmd.Action,
			Params:    cmd.Params,
		}
		payloadJSON, _ := json.Marshal(payload)
		if err := ac.Send(agentWSMessage{
			Type: "command",
			Data: payloadJSON,
		}); err != nil {
			slog.Warn("send command via ws failed, queued for heartbeat", "error", err.Error())
			h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "queued", "ws send failed, queued for heartbeat delivery")
			cmd.Status = "queued"
		} else {
			h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "sent", "")
			cmd.Status = "sent"
		}
	} else {
		h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "queued", "agent not connected, queued for heartbeat delivery")
		cmd.Status = "queued"
		slog.Info("command queued for heartbeat delivery", "id", cmd.ID, "device", deviceID, "action", req.Action)
	}

	// Emit informational event for all commands
	if device, err := h.devices.GetByID(r.Context(), deviceID); err == nil {
		h.eventGen.CommandSent(r.Context(), deviceID, device.Hostname, req.Action, req.Params)
	}

	slog.Info("command created", "id", cmd.ID, "device", deviceID, "action", req.Action, "status", cmd.Status)
	writeJSON(w, http.StatusCreated, cmd)
}

// BulkDockerUpdate handles POST /api/v1/devices/{id}/docker/bulk-update.
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

	// Create a single command for the bulk update
	cmd := &models.Command{
		ID:       uuid.New().String(),
		DeviceID: deviceID,
		Action:   "docker_bulk_update",
		Params:   map[string]interface{}{"container_ids": req.ContainerIDs},
		Status:   "pending",
	}
	if err := h.commandRepo.Create(r.Context(), cmd); err != nil {
		slog.Error("create bulk update command", "error", err.Error())
		http.Error(w, `{"error":"failed to create command"}`, http.StatusInternalServerError)
		return
	}

	// Try WS delivery
	agentConnections.RLock()
	ac := agentConnections.m[deviceID]
	agentConnections.RUnlock()

	if ac != nil {
		payload := models.CommandPayload{
			CommandID: cmd.ID,
			Action:    cmd.Action,
			Params:    cmd.Params,
		}
		payloadJSON, _ := json.Marshal(payload)
		if err := ac.Send(agentWSMessage{Type: "command", Data: payloadJSON}); err != nil {
			h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "queued", "ws send failed")
			cmd.Status = "queued"
		} else {
			h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "sent", "")
			cmd.Status = "sent"
		}
	} else {
		h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "queued", "agent not connected")
		cmd.Status = "queued"
	}

	writeJSON(w, http.StatusCreated, cmd)
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
		commands, err = h.commandRepo.ListByDeviceFiltered(r.Context(), deviceID, limit, offset, statuses, action)
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
