package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/google/uuid"
)

// PatchStatus handles GET /api/v1/fleet/patch-status.
// Returns pending update details per device from latest telemetry.
// Use ?detail=true to include the full package list per device.
func (h *Handlers) PatchStatus(w http.ResponseWriter, r *http.Request) {
	snaps, err := h.telemetry.GetAllLatestSnapshots(r.Context())
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
		DeviceID       string                `json:"device_id"`
		Hostname       string                `json:"hostname"`
		PendingUpdates int                   `json:"pending_updates"`
		SecurityCount  int                   `json:"security_count"`
		PackageManager string                `json:"package_manager,omitempty"`
		Updates        []models.PendingUpdate `json:"updates,omitempty"`
	}
	result := make([]devicePatchInfo, 0, len(snaps))
	for _, s := range snaps {
		if s.Data.Updates != nil && s.Data.Updates.PendingUpdates > 0 {
			info := devicePatchInfo{
				DeviceID:       s.DeviceID,
				Hostname:       hostnames[s.DeviceID],
				PendingUpdates: s.Data.Updates.PendingUpdates,
				SecurityCount:  s.Data.Updates.PendingSecurityCount,
			}
			if detail {
				info.PackageManager = s.Data.Updates.PackageManager
				info.Updates = s.Data.Updates.Updates
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
	queued := 0
	skipped := 0
	for _, d := range devices {
		if d.Status != models.DeviceStatusOnline {
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

		// Try WS first, fall back to heartbeat queue
		agentConnections.RLock()
		ac := agentConnections.m[d.ID]
		agentConnections.RUnlock()

		if ac != nil {
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
				h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "queued", "ws send failed, queued for heartbeat")
				queued++
			} else {
				h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "sent", "")
				sent++
			}
		} else {
			h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "queued", "queued for heartbeat delivery")
			queued++
		}
	}

	slog.Info("bulk agent update", "version", req.Version, "sent", sent, "queued", queued, "skipped", skipped)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sent":    sent,
		"queued":  queued,
		"skipped": skipped,
		"total":   len(devices),
	})
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

	// Get telemetry to find devices with pending patches
	snaps, err := h.telemetry.GetAllLatestSnapshots(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to get telemetry"}`, http.StatusInternalServerError)
		return
	}
	needsPatch := make(map[string]bool, len(snaps))
	for _, s := range snaps {
		if s.Data.Updates != nil && s.Data.Updates.PendingUpdates > 0 {
			needsPatch[s.DeviceID] = true
		}
	}

	devices, err := h.devices.List(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list devices"}`, http.StatusInternalServerError)
		return
	}

	sent := 0
	queued := 0
	skipped := 0
	for _, d := range devices {
		if !needsPatch[d.ID] || d.Status != models.DeviceStatusOnline {
			skipped++
			continue
		}

		cmd := &models.Command{
			ID:       uuid.New().String(),
			DeviceID: d.ID,
			Action:   "os_update",
			Params:   map[string]interface{}{"mode": req.Mode},
			Status:   "pending",
		}
		if err := h.commandRepo.Create(r.Context(), cmd); err != nil {
			slog.Error("bulk patch: create command", "device", d.ID, "error", err)
			skipped++
			continue
		}

		agentConnections.RLock()
		ac := agentConnections.m[d.ID]
		agentConnections.RUnlock()

		if ac != nil {
			payload := models.CommandPayload{
				CommandID: cmd.ID,
				Action:    "os_update",
				Params:    cmd.Params,
			}
			payloadJSON, _ := json.Marshal(payload)
			if err := ac.Send(agentWSMessage{
				Type: "command",
				Data: payloadJSON,
			}); err != nil {
				h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "queued", "ws send failed, queued for heartbeat")
				queued++
			} else {
				h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "sent", "")
				sent++
			}
		} else {
			h.commandRepo.UpdateStatus(r.Context(), cmd.ID, "queued", "queued for heartbeat delivery")
			queued++
		}
	}

	slog.Info("bulk patch", "mode", req.Mode, "sent", sent, "queued", queued, "skipped", skipped)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sent":    sent,
		"queued":  queued,
		"skipped": skipped,
		"total":   len(devices),
	})
}
