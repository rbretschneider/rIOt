package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/commands"
)

// PatchStatus handles GET /api/v1/fleet/patch-status.
// Returns pending update details per device from latest telemetry.
// Use ?detail=true to include the full package list per device.
func (h *Handlers) PatchStatus(w http.ResponseWriter, r *http.Request) {
	summaries, err := h.telemetry.GetAllLatestSummaries(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to get telemetry"}`, http.StatusInternalServerError)
		return
	}

	// Build hostname lookup
	hostnames := map[string]string{}
	if devices, err := h.devices.List(r.Context()); err == nil {
		for _, d := range devices {
			hostnames[d.ID] = d.Hostname
		}
	}

	detail := r.URL.Query().Get("detail") == "true"

	type devicePatchInfo struct {
		DeviceID         string                 `json:"device_id"`
		Hostname         string                 `json:"hostname"`
		PendingUpdates   int                    `json:"pending_updates"`
		SecurityCount    int                    `json:"security_count"`
		RebootClassCount int                    `json:"reboot_class_count"`
		RebootRequired   bool                   `json:"reboot_required"`
		PackageManager   string                 `json:"package_manager,omitempty"`
		Updates          []models.PendingUpdate `json:"updates,omitempty"`
		// REBOOT-ETA-001 additive fields. The first three are server-wide
		// (identical on every row); AllowReboot is the device's self-reported
		// flag and is display-only (never read by any dispatch/gating path).
		OSPatchMode     string     `json:"os_patch_mode,omitempty"`
		NextWindowStart *time.Time `json:"next_window_start,omitempty"`
		CooldownMinutes int        `json:"os_patch_cooldown_minutes,omitempty"`
		AllowReboot     *bool      `json:"allow_reboot,omitempty"`
	}

	// Computed once per request, not per device. time.Now() lives here at the
	// composition root; the window computation itself takes an injected clock.
	cfg := h.loadAutomationConfig(r.Context())
	var nextStart *time.Time
	if t, ok := nextMaintenanceWindowStart(cfg.OSPatch, time.Now().UTC()); ok {
		nextStart = &t
	}

	result := make([]devicePatchInfo, 0, len(summaries))
	for _, s := range summaries {
		// Include a device when it has pending packages OR a standing reboot
		// requirement. A fully-patched device awaiting only a reboot has
		// PendingUpdates == 0 but RebootRequired == true; without this it was
		// dropped entirely, so the fleet reboot indicator and its ETA tooltip
		// never rendered (REBOOT-ETA-001 follow-up).
		if s.Updates != nil && (s.Updates.PendingUpdates > 0 || s.Updates.RebootRequired) {
			info := devicePatchInfo{
				DeviceID:         s.DeviceID,
				Hostname:         hostnames[s.DeviceID],
				PendingUpdates:   s.Updates.PendingUpdates,
				SecurityCount:    s.Updates.PendingSecurityCount,
				RebootClassCount: s.Updates.PendingRebootClassCount,
				RebootRequired:   s.Updates.RebootRequired,
				OSPatchMode:      cfg.OSPatch.Mode,
				NextWindowStart:  nextStart,
				CooldownMinutes:  cfg.OSPatch.CooldownMinutes,
				AllowReboot:      s.Updates.AllowReboot,
			}
			if detail {
				info.PackageManager = s.Updates.PackageManager
				info.Updates = s.Updates.Updates
			}
			result = append(result, info)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

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
// Devices that already have an in-flight host action are counted in conflicts.
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

	var tally dispatchTally
	for _, d := range devices {
		if d.Status != models.DeviceStatusOnline {
			tally.skipped++
			continue
		}
		tally.dispatch(r.Context(), h, commands.DispatchRequest{
			DeviceID: d.ID, Action: models.ActionAgentUpdate, Params: map[string]interface{}{},
		}, "bulk update: dispatch command")
	}

	slog.Info("bulk agent update", "version", req.Version, "sent", tally.sent, "queued", tally.queued,
		"skipped", tally.skipped, "conflicts", tally.conflicts)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sent":      tally.sent,
		"queued":    tally.queued,
		"skipped":   tally.skipped,
		"conflicts": tally.conflicts,
		"total":     len(devices),
	})
}

// dispatchTally counts the outcomes of a fleet-wide dispatch loop.
type dispatchTally struct {
	sent, queued, skipped, conflicts int
}

// dispatch creates one command through the command service and counts it as
// sent (delivered over the agent WebSocket), queued, a conflict (the device
// already has an in-flight command on that target) or skipped (error).
func (t *dispatchTally) dispatch(ctx context.Context, h *Handlers, req commands.DispatchRequest, errMsg string) {
	res, err := h.cmdSvc().Dispatch(ctx, req)
	switch {
	case err != nil:
		slog.Error(errMsg, "device", req.DeviceID, "error", err.Error())
		t.skipped++
	case res.Conflict != nil:
		t.conflicts++
	case models.CommandState(res.Command.Status) == models.CommandDispatched:
		t.sent++
	default:
		t.queued++
	}
}

// BulkPatchDevices handles POST /api/v1/fleet/bulk-patch.
// Sends os_update command to online devices that have pending OS patches.
func (h *Handlers) BulkPatchDevices(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.Mode == "" {
		req.Mode = "full"
	}

	// Get lightweight telemetry summaries to find devices with pending patches
	summaries, err := h.telemetry.GetAllLatestSummaries(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to get telemetry"}`, http.StatusInternalServerError)
		return
	}
	needsPatch := make(map[string]bool, len(summaries))
	for _, s := range summaries {
		if s.Updates != nil && s.Updates.PendingUpdates > 0 {
			needsPatch[s.DeviceID] = true
		}
	}

	devices, err := h.devices.List(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list devices"}`, http.StatusInternalServerError)
		return
	}

	var tally dispatchTally
	for _, d := range devices {
		if !needsPatch[d.ID] || d.Status != models.DeviceStatusOnline {
			tally.skipped++
			continue
		}
		tally.dispatch(r.Context(), h, commands.DispatchRequest{
			DeviceID: d.ID, Action: models.ActionOSUpdate, Params: map[string]interface{}{"mode": req.Mode},
		}, "bulk patch: dispatch command")
	}

	slog.Info("bulk patch", "mode", req.Mode, "sent", tally.sent, "queued", tally.queued,
		"skipped", tally.skipped, "conflicts", tally.conflicts)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sent":      tally.sent,
		"queued":    tally.queued,
		"skipped":   tally.skipped,
		"conflicts": tally.conflicts,
		"total":     len(devices),
	})
}
