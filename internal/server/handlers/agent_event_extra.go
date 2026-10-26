package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Outcomes an extra-updater end event may carry. An updater that was skipped
// never ran, so it emits no event at all.
var extraEventOutcomes = map[string]string{
	"success":     "finished",
	"failed":      "failed",
	"timed_out":   "timed out",
	"interrupted": "was interrupted",
}

// receiveExtraUpdaterEvent stores the start or end of an extra-updater step
// (EXTRA-UPD-001 AD-011, SEC-005). The agent is not trusted for anything it
// states here: the command must be a canonical UUID of an os_update that
// belongs to this device, the updater must be one this server knows, the
// outcome must be in the enum, at most one start and one end are stored per
// command, and the stored message is composed here from validated facts, with
// the origin taken from the command row. Anything else is a 400 and no event.
func (h *Handlers) receiveExtraUpdaterEvent(w http.ResponseWriter, r *http.Request, deviceID string, evt models.AgentEvent) {
	reject := func() { http.Error(w, `{"error":"invalid extra updater event"}`, http.StatusBadRequest) }
	if h.commandRepo == nil || !models.IsCanonicalUUID(evt.CommandID) || !extraupd.IsImplemented(evt.Updater) {
		reject()
		return
	}
	finished := evt.Type == models.EventExtraUpdaterFinished
	outcomeWords := ""
	if finished {
		var ok bool
		if outcomeWords, ok = extraEventOutcomes[evt.Outcome]; !ok {
			reject()
			return
		}
	} else if evt.Outcome != "" {
		reject()
		return
	}
	cmd, err := h.commandRepo.GetForDevice(r.Context(), evt.CommandID, deviceID)
	if err != nil || cmd == nil || cmd.Action != models.ActionOSUpdate {
		reject()
		return
	}

	h.extraEventMu.Lock()
	defer h.extraEventMu.Unlock()
	exists, err := h.events.HasCommandEvent(r.Context(), deviceID, evt.CommandID, evt.Type)
	if err != nil {
		slog.Error("extra updater event dedupe", "device_id", deviceID, "command_id", evt.CommandID, "error", err.Error())
		http.Error(w, `{"error":"failed to create event"}`, http.StatusInternalServerError)
		return
	}
	if exists {
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
		return
	}

	hostname := deviceID
	if len(hostname) > 8 {
		hostname = hostname[:8]
	}
	if h.devices != nil {
		if d, derr := h.devices.GetByID(r.Context(), deviceID); derr == nil && d != nil {
			hostname = d.Hostname
		}
	}
	label := extraLabel(evt.Updater)
	severity := models.SeverityInfo
	msg := label + " update started on " + hostname + " (command " + cmd.ID + ", origin " + cmd.Origin + ")"
	if finished {
		msg = label + " update " + outcomeWords + " on " + hostname + " (command " + cmd.ID + ", origin " + cmd.Origin + ")"
		if evt.Outcome != "success" {
			severity = models.SeverityWarning
		}
	}
	e := &models.Event{
		DeviceID:  deviceID,
		CommandID: cmd.ID,
		Type:      evt.Type,
		Severity:  severity,
		Message:   msg,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.events.Create(r.Context(), e); err != nil {
		slog.Error("create extra updater event", "error", err.Error())
		http.Error(w, `{"error":"failed to create event"}`, http.StatusInternalServerError)
		return
	}
	if h.hub != nil {
		h.hub.BroadcastEvent(e)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
