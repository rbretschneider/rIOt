package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var validActions = map[string]bool{
	"docker_stop":    true,
	"docker_restart": true,
	"docker_start":   true,
	"reboot":         true,
	"agent_update":   true,
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

	// Check agent is connected
	agentConnections.RLock()
	ac := agentConnections.m[deviceID]
	agentConnections.RUnlock()
	if ac == nil {
		http.Error(w, `{"error":"agent not connected"}`, http.StatusBadGateway)
		return
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
		slog.Error("create command", "error", err)
		http.Error(w, `{"error":"failed to create command"}`, http.StatusInternalServerError)
		return
	}

	// Send to agent via WS
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
		slog.Error("send command to agent", "error", err)
		h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "error", "failed to send to agent")
		http.Error(w, `{"error":"failed to send command to agent"}`, http.StatusBadGateway)
		return
	}

	// Mark as sent
	h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "sent", "")
	cmd.Status = "sent"

	slog.Info("command sent", "id", cmd.ID, "device", deviceID, "action", req.Action)
	writeJSON(w, http.StatusCreated, cmd)
}

// ListDeviceCommands handles GET /api/v1/devices/{id}/commands.
func (h *Handlers) ListDeviceCommands(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 50
	}
	commands, err := h.commandRepo.ListByDevice(r.Context(), deviceID, limit)
	if err != nil {
		http.Error(w, `{"error":"failed to list commands"}`, http.StatusInternalServerError)
		return
	}
	if commands == nil {
		commands = []models.Command{}
	}
	writeJSON(w, http.StatusOK, commands)
}
