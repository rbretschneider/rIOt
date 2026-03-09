package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
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

// checkAutoUpdates looks at incoming telemetry for containers with update_available=true,
// matches them against auto-update policies, and sends docker_update commands.
func (h *Handlers) checkAutoUpdates(ctx context.Context, deviceID string, data *models.FullTelemetryData) {
	if h.autoUpdateRepo == nil || h.commandRepo == nil || data.Docker == nil {
		return
	}

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
				if !recentlyTriggered(pol, 30*time.Minute) {
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
			if !recentlyTriggered(pol, 30*time.Minute) {
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
