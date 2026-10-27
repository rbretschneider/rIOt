package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/go-chi/chi/v5"
)

// GetCommandEvents handles GET /api/v1/devices/{id}/commands/{commandId}/events
// (EXPECT-002 Section 6.3, C-3): the events linked to one command or record, in
// two groups. Every server-authored event comes back in full (at most 500), the
// agent-sourced ones are capped at 50 with the number omitted, so an agent that
// floods its own record cannot push the server's outcome and escalation events
// out of the view. A malformed, missing and foreign id read exactly alike (one
// 404 body). Admin session only.
func (h *Handlers) GetCommandEvents(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	commandID := chi.URLParam(r, "commandId")
	if !models.IsCanonicalUUID(commandID) {
		commandNotFound(w)
		return
	}
	if h.commandRepo == nil || h.events == nil {
		http.Error(w, `{"error":"failed to list events"}`, http.StatusInternalServerError)
		return
	}
	// The device scope is part of the read: a foreign command is a missing one.
	if _, err := h.commandRepo.GetForDevice(r.Context(), commandID, deviceID); err != nil {
		if errors.Is(err, db.ErrCommandNotFound) {
			commandNotFound(w)
			return
		}
		slog.Error("get command events: read command", "device_id", deviceID, "command_id", commandID, "error", err.Error())
		http.Error(w, `{"error":"failed to list events"}`, http.StatusInternalServerError)
		return
	}
	res, err := h.events.ListByCommandSplit(r.Context(), deviceID, commandID, models.CommandEventsServerLimit, models.CommandEventsAgentLimit)
	if err != nil {
		slog.Error("get command events", "device_id", deviceID, "command_id", commandID, "error", err.Error())
		http.Error(w, `{"error":"failed to list events"}`, http.StatusInternalServerError)
		return
	}
	if res.ServerEvents == nil {
		res.ServerEvents = []models.Event{}
	}
	if res.AgentEvents == nil {
		res.AgentEvents = []models.Event{}
	}
	writeJSON(w, http.StatusOK, res)
}
