package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ListAutoUpdates handles GET /api/v1/devices/{id}/auto-updates.
func (h *Handlers) ListAutoUpdates(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	policies, err := h.autoUpdateRepo.ListByDevice(r.Context(), deviceID)
	if err != nil {
		http.Error(w, `{"error":"failed to list auto-update policies"}`, http.StatusInternalServerError)
		return
	}
	if policies == nil {
		policies = []models.AutoUpdatePolicy{}
	}
	writeJSON(w, http.StatusOK, policies)
}

// SetAutoUpdate handles PUT /api/v1/devices/{id}/auto-updates.
func (h *Handlers) SetAutoUpdate(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	var body struct {
		Target         string `json:"target"`
		IsStack        bool   `json:"is_stack"`
		ComposeWorkDir string `json:"compose_work_dir"`
		Enabled        bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.Target == "" {
		http.Error(w, `{"error":"target is required"}`, http.StatusBadRequest)
		return
	}

	p := &models.AutoUpdatePolicy{
		DeviceID:       deviceID,
		Target:         body.Target,
		IsStack:        body.IsStack,
		ComposeWorkDir: body.ComposeWorkDir,
		Enabled:        body.Enabled,
	}
	if err := h.autoUpdateRepo.Upsert(r.Context(), p); err != nil {
		slog.Error("upsert auto-update policy", "error", err)
		http.Error(w, `{"error":"failed to save auto-update policy"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteAutoUpdate handles DELETE /api/v1/devices/{id}/auto-updates/{target}.
func (h *Handlers) DeleteAutoUpdate(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	target := chi.URLParam(r, "target")
	if err := h.autoUpdateRepo.Delete(r.Context(), deviceID, target); err != nil {
		slog.Error("delete auto-update policy", "error", err)
		http.Error(w, `{"error":"failed to delete auto-update policy"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetAutomationConfig handles GET /api/v1/settings/automation.
func (h *Handlers) GetAutomationConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.loadAutomationConfig(r.Context())
	writeJSON(w, http.StatusOK, cfg)
}

// SetAutomationConfig handles PUT /api/v1/settings/automation.
func (h *Handlers) SetAutomationConfig(w http.ResponseWriter, r *http.Request) {
	var cfg models.AutomationConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Validate modes
	for _, mw := range []models.MaintenanceWindow{cfg.OSPatch, cfg.DockerUpdate} {
		switch mw.Mode {
		case "anytime", "window", "disabled":
			// ok
		default:
			http.Error(w, fmt.Sprintf(`{"error":"invalid mode: %s"}`, mw.Mode), http.StatusBadRequest)
			return
		}
		if mw.Mode == "window" {
			if !validTimeStr(mw.StartTime) || !validTimeStr(mw.EndTime) {
				http.Error(w, `{"error":"invalid time format, expected HH:MM"}`, http.StatusBadRequest)
				return
			}
		}
		if mw.CooldownMinutes < 1 {
			http.Error(w, `{"error":"cooldown must be at least 1 minute"}`, http.StatusBadRequest)
			return
		}
	}

	data, _ := json.Marshal(cfg)
	if err := h.adminRepo.SetConfig(r.Context(), "automation_config", string(data)); err != nil {
		slog.Error("save automation config", "error", err)
		http.Error(w, `{"error":"failed to save automation config"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("automation config updated",
		"os_patch_mode", cfg.OSPatch.Mode,
		"docker_update_mode", cfg.DockerUpdate.Mode)
	writeJSON(w, http.StatusOK, cfg)
}

// loadAutomationConfig reads the automation config from the admin config store.
func (h *Handlers) loadAutomationConfig(ctx context.Context) models.AutomationConfig {
	raw, err := h.adminRepo.GetConfig(ctx, "automation_config")
	if err != nil || raw == "" {
		return models.DefaultAutomationConfig()
	}
	var cfg models.AutomationConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return models.DefaultAutomationConfig()
	}
	return cfg
}

// inMaintenanceWindow checks if the current UTC time falls within the maintenance window.
func inMaintenanceWindow(w models.MaintenanceWindow) bool {
	switch w.Mode {
	case "disabled":
		return false
	case "anytime":
		return true
	case "window":
		now := time.Now().UTC()
		currentMin := now.Hour()*60 + now.Minute()
		startMin := parseTimeStr(w.StartTime)
		endMin := parseTimeStr(w.EndTime)

		if startMin <= endMin {
			// Same-day window (e.g. 09:00 - 17:00)
			return currentMin >= startMin && currentMin < endMin
		}
		// Overnight window (e.g. 23:00 - 05:00)
		return currentMin >= startMin || currentMin < endMin
	default:
		return true
	}
}

// parseTimeStr parses "HH:MM" to minutes from midnight.
func parseTimeStr(s string) int {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0
	}
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	return h*60 + m
}

// validTimeStr checks if a string is a valid "HH:MM" format.
func validTimeStr(s string) bool {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return false
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return false
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return false
	}
	return true
}

// checkAutoUpdates looks at incoming telemetry for containers with update_available=true,
// matches them against auto-update policies, and sends docker_update commands.
func (h *Handlers) checkAutoUpdates(ctx context.Context, deviceID string, data *models.FullTelemetryData) {
	if h.autoUpdateRepo == nil || h.commandRepo == nil || data.Docker == nil {
		return
	}

	// Check maintenance window
	cfg := h.loadAutomationConfig(ctx)
	if !inMaintenanceWindow(cfg.DockerUpdate) {
		return
	}
	cooldown := time.Duration(cfg.DockerUpdate.CooldownMinutes) * time.Minute

	policies, err := h.autoUpdateRepo.ListByDevice(ctx, deviceID)
	if err != nil || len(policies) == 0 {
		return
	}

	// Build lookup: target name -> policy
	policyMap := make(map[string]*models.AutoUpdatePolicy, len(policies))
	for i := range policies {
		if policies[i].Enabled {
			policyMap[policies[i].Target] = &policies[i]
		}
	}
	if len(policyMap) == 0 {
		return
	}

	// Track which stacks we've already triggered (avoid duplicate commands)
	triggeredStacks := make(map[string]bool)

	for _, c := range data.Docker.Containers {
		if c.UpdateAvailable == nil || !*c.UpdateAvailable {
			continue
		}

		// Check for stack-level policy (by compose project name)
		project := c.Labels["com.docker.compose.project"]
		workDir := c.Labels["com.docker.compose.project.working_dir"]

		if project != "" && workDir != "" {
			if pol, ok := policyMap[project]; ok && pol.IsStack && !triggeredStacks[project] {
				if !recentlyTriggered(pol, cooldown) {
					h.dispatchAutoUpdate(ctx, deviceID, pol, map[string]interface{}{
						"compose_work_dir": workDir,
					})
					triggeredStacks[project] = true
				}
				continue
			}
		}

		// Check for container-level policy
		name := c.Name
		if pol, ok := policyMap[name]; ok && !pol.IsStack {
			if !recentlyTriggered(pol, cooldown) {
				h.dispatchAutoUpdate(ctx, deviceID, pol, map[string]interface{}{
					"container": name,
				})
			}
		}
	}
}

// recentlyTriggered returns true if the policy was triggered within the cooldown period.
func recentlyTriggered(p *models.AutoUpdatePolicy, cooldown time.Duration) bool {
	return p.LastTriggeredAt != nil && time.Since(*p.LastTriggeredAt) < cooldown
}

// dispatchAutoUpdate creates a docker_update command and sends it via WS or queues it.
func (h *Handlers) dispatchAutoUpdate(ctx context.Context, deviceID string, pol *models.AutoUpdatePolicy, params map[string]interface{}) {
	cmd := &models.Command{
		ID:       uuid.New().String(),
		DeviceID: deviceID,
		Action:   "docker_update",
		Params:   params,
		Status:   "pending",
	}
	if err := h.commandRepo.Create(ctx, cmd); err != nil {
		slog.Error("auto-update: create command", "error", err)
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
			h.commandRepo.UpdateStatus(ctx, cmd.ID, "queued", "auto-update: ws send failed")
		} else {
			h.commandRepo.UpdateStatus(ctx, cmd.ID, "sent", "auto-update")
		}
	} else {
		h.commandRepo.UpdateStatus(ctx, cmd.ID, "queued", "auto-update: agent not connected")
	}

	h.autoUpdateRepo.SetLastTriggered(ctx, pol.ID)
	slog.Info("auto-update triggered", "device", deviceID, "target", pol.Target, "is_stack", pol.IsStack)
}

// SetAutoPatch handles PUT /api/v1/devices/{id}/auto-patch.
func (h *Handlers) SetAutoPatch(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if err := h.devices.UpdateAutoPatch(r.Context(), deviceID, body.Enabled); err != nil {
		slog.Error("update auto-patch", "error", err)
		http.Error(w, `{"error":"failed to update auto-patch setting"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"auto_patch": body.Enabled})
}

// checkAutoPatch checks if a device has auto-patch enabled and pending OS updates,
// and dispatches an os_update command if so.
func (h *Handlers) checkAutoPatch(ctx context.Context, deviceID string, data *models.FullTelemetryData) {
	if h.commandRepo == nil || data.Updates == nil || data.Updates.PendingUpdates == 0 {
		return
	}

	autoPatch, err := h.devices.GetAutoPatch(ctx, deviceID)
	if err != nil || !autoPatch {
		return
	}

	// Check maintenance window
	cfg := h.loadAutomationConfig(ctx)
	if !inMaintenanceWindow(cfg.OSPatch) {
		return
	}
	cooldown := time.Duration(cfg.OSPatch.CooldownMinutes) * time.Minute

	// Check for recent os_update commands to avoid duplicates
	cmds, err := h.commandRepo.ListByDevice(ctx, deviceID, 10)
	if err != nil {
		return
	}
	for _, cmd := range cmds {
		if cmd.Action == "os_update" && time.Since(cmd.CreatedAt) < cooldown {
			return // Already sent recently
		}
	}

	cmd := &models.Command{
		ID:       uuid.New().String(),
		DeviceID: deviceID,
		Action:   "os_update",
		Params:   map[string]interface{}{"mode": "full"},
		Status:   "pending",
	}
	if err := h.commandRepo.Create(ctx, cmd); err != nil {
		slog.Error("auto-patch: create command", "error", err)
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
			h.commandRepo.UpdateStatus(ctx, cmd.ID, "queued", "auto-patch: ws send failed")
		} else {
			h.commandRepo.UpdateStatus(ctx, cmd.ID, "sent", "auto-patch")
		}
	} else {
		h.commandRepo.UpdateStatus(ctx, cmd.ID, "queued", "auto-patch: agent not connected")
	}

	slog.Info("auto-patch triggered", "device", deviceID, "pending_updates", data.Updates.PendingUpdates)
}
