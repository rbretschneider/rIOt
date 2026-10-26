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
// today (the agent's own command_id is never read here either, X-2).
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
	return h.cmdSvc().NoteAgentUpdateEvent(r.Context(), commands.AgentUpdateEventInput{
		DeviceID: deviceID, Hostname: hostname, Type: evt.Type, Message: evt.Message, At: at,
	})
}
