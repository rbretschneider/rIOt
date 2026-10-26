package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Outcomes an extra-updater end event may carry. "skipped" is a refusal: the
// updater never ran, and the event carries the reason of the refusal
// (DIETPI-PATH-001 FR-014).
var extraEventOutcomes = map[string]string{
	"success":     "finished",
	"failed":      "failed",
	"timed_out":   "timed out",
	"interrupted": "was interrupted",
	"skipped":     "skipped",
}

// knownSkipReasons is the closed set a skipped event's reason must belong to.
var knownSkipReasons = func() map[string]bool {
	out := map[string]bool{}
	for _, r := range maintenance.SkipReasons() {
		out[r] = true
	}
	return out
}()

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
		// The reason is accepted only from the closed skip-reason set, and only
		// on a skipped event. An unknown reason is dropped and the event kept
		// (ADD AD-5), so a newer agent's reason never costs Ryan the event; the
		// stored message is composed here and never contains agent text.
		if evt.Outcome != "skipped" || !knownSkipReasons[evt.Reason] {
			evt.Reason = ""
		}
	} else if evt.Outcome != "" || evt.Reason != "" {
		reject()
		return
	}
	cmd, err := h.commandRepo.GetForDevice(r.Context(), evt.CommandID, deviceID)
	if err != nil || cmd == nil || cmd.Action != models.ActionOSUpdate {
		reject()
		return
	}

	// SEC-005: the lock makes "check, then insert" atomic inside this process
	// only. There is no database uniqueness constraint, so two server replicas
	// could both store the event. That is accepted for today's single-process
	// deployment (QA N-2). The key is (device, command, type), not the updater:
	// add the updater to the key when a second updater is built.
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
		if evt.Outcome == "skipped" && evt.Reason != "" {
			msg = label + " update skipped on " + hostname + ": " + refusalWords(evt.Reason, "") + " (command " + cmd.ID + ", origin " + cmd.Origin + ")"
		}
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
