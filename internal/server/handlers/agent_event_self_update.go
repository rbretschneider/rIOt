package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/commands"
)

// linkAgentUpdateEvent is the EXPECT-002 step of ReceiveAgentEvent (AD-002,
// AD-004): the thin handler side of self-update recognition. It runs only for
// the three event types the self-update path reads, and only when the device key
// that authenticated the request is the {id} device: a request that did not pass
// DeviceAuth, or that names another device, gets the event stored exactly as
// today. Since EXPECT-007 (AD-011) the agent's command_id, attempt_id and
// versions are read here, but only inside this gate and only as raw input the
// Service validates (it supersedes EXPECT-002 X-2 narrowly).
//
// It returns the link to store with the agent's event and the server-written
// events (at most the rate-cap warning) to broadcast after it. It never fails
// the request: the service returns an empty link on every error.
func (h *Handlers) linkAgentUpdateEvent(r *http.Request, deviceID string, evt models.AgentEvent, at time.Time) commands.AgentUpdateEventLink {
	switch evt.Type {
	case models.EventAgentUpdateStarted, models.EventAgentUpdateCompleted, models.EventAgentUpdateFailed:
	default:
		return commands.AgentUpdateEventLink{}
	}
	if authed := authenticatedDeviceID(r); authed == "" || authed != deviceID {
		return commands.AgentUpdateEventLink{}
	}
	hostname := ""
	if h.devices != nil {
		if d, err := h.devices.GetByID(r.Context(), deviceID); err == nil && d != nil {
			hostname = d.Hostname
		} else if err != nil {
			slog.Debug("agent update event: device read failed", "device_id", deviceID, "error", err.Error())
		}
	}
	// EXPECT-007 AD-011: the agent's command_id, attempt_id and versions are read
	// here and nowhere else, only inside the authenticated-ID gate above, and
	// only as raw input to the Service, which validates every one of them. An
	// invalid reference never changes the status code.
	return h.cmdSvc().NoteAgentUpdateEvent(r.Context(), commands.AgentUpdateEventInput{
		DeviceID: deviceID, Hostname: hostname, Type: evt.Type, Message: evt.Message,
		CommandRef: evt.CommandID, AttemptRef: evt.AttemptID, FromVersion: evt.FromVersion, ToVersion: evt.ToVersion,
		At: at,
	})
}

// storeAgentUpdateEvent stores one agent_update_* event, dropping a repeat of an
// event the server already linked by a validated reference (EXPECT-007 AD-009,
// FR-035). duplicate is true when nothing was stored.
//
// De-duplication runs only when the link is reference-derived (link.Dedupe) and
// its linked ID is non-empty: HasCommandEvent(device, "", type) would match any
// earlier unlinked event of that type (security condition C-3 b). The mutex
// makes check-then-insert atomic inside this process only; the deployment runs
// one server process, as for the extra-updater events (EXTRA-UPD-001 SEC-005,
// QA N-2). A failed check stores the event: NFR-008 forbids dropping one.
// Bound: one mutex, no per-event or per-reference state.
func (h *Handlers) storeAgentUpdateEvent(r *http.Request, e *models.Event, link commands.AgentUpdateEventLink) (duplicate bool, err error) {
	if !link.Dedupe || e.CommandID == "" {
		return false, h.events.Create(r.Context(), e)
	}
	h.agentUpdateEventMu.Lock()
	defer h.agentUpdateEventMu.Unlock()
	exists, derr := h.events.HasCommandEvent(r.Context(), e.DeviceID, e.CommandID, e.Type)
	if derr != nil {
		slog.Debug("agent update event: duplicate check failed, storing", "device_id", e.DeviceID)
	} else if exists {
		return true, nil
	}
	return false, h.events.Create(r.Context(), e)
}
