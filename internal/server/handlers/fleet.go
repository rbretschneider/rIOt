package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/google/uuid"
)

// AgentVersionSummary handles GET /api/v1/fleet/agent-versions.
func (h *Handlers) AgentVersionSummary(w http.ResponseWriter, r *http.Request) {
	versions, err := h.devices.AgentVersionSummary(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to get version summary"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

// BulkUpdateAgents handles POST /api/v1/fleet/bulk-update.
// Sends agent_update command to all online devices with a specific version.
func (h *Handlers) BulkUpdateAgents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.Version == "" {
		http.Error(w, `{"error":"version is required"}`, http.StatusBadRequest)
		return
	}

	devices, err := h.devices.ListByVersion(r.Context(), req.Version)
	if err != nil {
		http.Error(w, `{"error":"failed to list devices"}`, http.StatusInternalServerError)
		return
	}

	sent := 0
	skipped := 0
	for _, d := range devices {
		if d.Status != models.DeviceStatusOnline {
			skipped++
			continue
		}

		// Check agent is connected
		agentConnections.RLock()
		ac := agentConnections.m[d.ID]
		agentConnections.RUnlock()
		if ac == nil {
			skipped++
			continue
		}

		cmd := &models.Command{
			ID:       uuid.New().String(),
			DeviceID: d.ID,
			Action:   "agent_update",
			Params:   map[string]interface{}{},
			Status:   "pending",
		}
		if err := h.commandRepo.Create(r.Context(), cmd); err != nil {
			slog.Error("bulk update: create command", "device", d.ID, "error", err)
			skipped++
			continue
		}

		payload := models.CommandPayload{
			CommandID: cmd.ID,
			Action:    "agent_update",
			Params:    cmd.Params,
		}
		payloadJSON, _ := json.Marshal(payload)
		if err := ac.Send(agentWSMessage{
			Type: "command",
			Data: payloadJSON,
		}); err != nil {
			h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "error", "failed to send to agent")
			skipped++
			continue
		}

		h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "sent", "")
		sent++
	}

	slog.Info("bulk agent update", "version", req.Version, "sent", sent, "skipped", skipped)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sent":    sent,
		"skipped": skipped,
		"total":   len(devices),
	})
}
