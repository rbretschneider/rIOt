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
	"github.com/DesyncTheThird/rIOt/internal/server/commands"
	"github.com/go-chi/chi/v5"
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
	// One conditional statement: an enabled policy of the other kind under the
	// same name is never overwritten, whatever the new enabled value is.
	stored, err := h.autoUpdateRepo.UpsertUnlessKindClash(r.Context(), p)
	if err != nil {
		slog.Error("upsert auto-update policy", "error", err.Error())
		http.Error(w, `{"error":"failed to save auto-update policy"}`, http.StatusInternalServerError)
		return
	}
	if !stored {
		existing := autoUpdatePolicyKind(!body.IsStack)
		slog.Info("auto-update policy refused: name clash", "device_id", deviceID, "code", autoUpdateNameClashCode)
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":         "an enabled " + existing + " auto-update policy already uses this name",
			"code":          autoUpdateNameClashCode,
			"existing_kind": existing,
		})
		return
	}
	h.auditDockerPolicy(r, deviceID, "auto_update", map[string]interface{}{
		"target":      auditTarget(body.Target),
		"policy_kind": autoUpdatePolicyKind(body.IsStack),
		"enabled":     body.Enabled,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// autoUpdateNameClashCode is the 409 code of a save that would overwrite an
// enabled policy of the other kind.
const autoUpdateNameClashCode = "auto_update_name_clash"

// autoUpdatePolicyKind names a policy kind in API bodies and the audit.
func autoUpdatePolicyKind(isStack bool) string {
	if isStack {
		return "stack"
	}
	return "container"
}

// DeleteAutoUpdate handles DELETE /api/v1/devices/{id}/auto-updates/{target}.
func (h *Handlers) DeleteAutoUpdate(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	target := chi.URLParam(r, "target")
	if err := h.autoUpdateRepo.Delete(r.Context(), deviceID, target); err != nil {
		slog.Error("delete auto-update policy", "error", err.Error())
		http.Error(w, `{"error":"failed to delete auto-update policy"}`, http.StatusInternalServerError)
		return
	}
	h.auditDockerPolicy(r, deviceID, "delete_auto_update", map[string]interface{}{"target": auditTarget(target)})
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
		if mw.StaggerSeconds < 0 {
			http.Error(w, `{"error":"stagger_seconds must be >= 0"}`, http.StatusBadRequest)
			return
		}
		switch mw.Frequency {
		case "", "daily", "weekly", "monthly":
			// ok
		default:
			http.Error(w, fmt.Sprintf(`{"error":"invalid frequency: %s"}`, mw.Frequency), http.StatusBadRequest)
			return
		}
		if mw.Frequency == "weekly" {
			if len(mw.DaysOfWeek) == 0 {
				http.Error(w, `{"error":"weekly frequency requires at least one day_of_week"}`, http.StatusBadRequest)
				return
			}
			for _, d := range mw.DaysOfWeek {
				if d < 0 || d > 6 {
					http.Error(w, `{"error":"days_of_week values must be 0 (Sun) through 6 (Sat)"}`, http.StatusBadRequest)
					return
				}
			}
		}
	}

	// Reboot-class policy (PATCH-GATE AD-009): only meaningful on the
	// OSPatch window; a non-empty value on DockerUpdate is rejected to keep
	// the stored blob clean.
	switch cfg.OSPatch.RebootClass {
	case "", "off", "gated":
		// ok
	default:
		http.Error(w, fmt.Sprintf(`{"error":"invalid reboot_class: %s"}`, cfg.OSPatch.RebootClass), http.StatusBadRequest)
		return
	}
	if cfg.DockerUpdate.RebootClass != "" {
		http.Error(w, `{"error":"reboot_class is only valid on os_patch"}`, http.StatusBadRequest)
		return
	}

	data, _ := json.Marshal(cfg)
	if err := h.adminRepo.SetConfig(r.Context(), "automation_config", string(data)); err != nil {
		slog.Error("save automation config", "error", err.Error())
		http.Error(w, `{"error":"failed to save automation config"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("automation config updated",
		"os_patch_mode", cfg.OSPatch.Mode,
		"docker_update_mode", cfg.DockerUpdate.Mode)
	writeJSON(w, http.StatusOK, cfg)
}

// loadAutomationConfig reads the automation config from the admin config store.
// Falls back to DefaultAutomationConfig (all disabled) on any error — the safe
// default is to NOT auto-update when we can't confirm the user's preferences.
func (h *Handlers) loadAutomationConfig(ctx context.Context) models.AutomationConfig {
	raw, err := h.adminRepo.GetConfig(ctx, "automation_config")
	if err != nil {
		slog.Warn("auto-update: failed to load automation config, falling back to disabled",
			"error", err.Error())
		return models.DefaultAutomationConfig()
	}
	if raw == "" {
		return models.DefaultAutomationConfig()
	}
	var cfg models.AutomationConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("auto-update: failed to parse automation config, falling back to disabled",
			"error", err.Error())
		return models.DefaultAutomationConfig()
	}
	return cfg
}

// inMaintenanceWindow checks if now (UTC) falls within the maintenance window.
func inMaintenanceWindow(w models.MaintenanceWindow, now time.Time) bool {
	switch w.Mode {
	case "disabled":
		return false
	case "anytime":
		return true
	case "window":
		now = now.UTC()
		if !frequencyAllowsDay(w, now) {
			return false
		}
		currentMin := now.Hour()*60 + now.Minute()
		startMin := parseTimeStr(w.StartTime)
		endMin := parseTimeStr(w.EndTime)

		if startMin <= endMin {
			// Same-day window (e.g. 09:00 - 17:00)
			return currentMin >= startMin && currentMin < endMin
		}
		// Overnight window (e.g. 23:00 - 05:00). Day-of-week / last-day-of-month
		// is evaluated against the START day only — a window starting Sunday
		// 23:00 that runs into Monday 05:00 counts as Sunday's maintenance pass.
		return currentMin >= startMin || currentMin < endMin
	default:
		return false // unknown mode — safe default is to not auto-update
	}
}

// frequencyAllowsDay returns true if the window's frequency rule permits a
// trigger on `now`'s calendar day. For same-day windows the caller evaluates
// `now` directly; for overnight windows the caller evaluates `now`'s current
// day (the start-day convention — see inMaintenanceWindow). An empty or
// unknown frequency is treated as "daily" for back-compat.
func frequencyAllowsDay(w models.MaintenanceWindow, now time.Time) bool {
	switch w.Frequency {
	case "", "daily":
		return true
	case "weekly":
		wd := int(now.Weekday())
		for _, d := range w.DaysOfWeek {
			if d == wd {
				return true
			}
		}
		return false
	case "monthly":
		// Last day of current month: first of next month minus one day.
		firstOfNext := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
		lastDay := firstOfNext.Add(-24 * time.Hour).Day()
		return now.Day() == lastDay
	default:
		return true
	}
}

// nextWindowSearchDays bounds the nextMaintenanceWindowStart search. Daily,
// weekly and monthly recurrence all resolve within 31 days; a full leap year
// of slack guarantees termination for any stored config.
const nextWindowSearchDays = 366

// nextMaintenanceWindowStart returns the earliest UTC instant at which the
// window's start-of-day occurs at or after `now`, honoring the frequency rule.
// ok is false when mode != "window", the start time is invalid, or no eligible
// day is found within the search horizon. `now` is injected for testability
// (no time.Now() here).
//
// A `now` strictly inside an active window resolves to the NEXT eligible
// occurrence (today's start is already in the past); `now` exactly equal to a
// start returns that start. Frequency is evaluated on the start day, matching
// the overnight convention in inMaintenanceWindow.
func nextMaintenanceWindowStart(w models.MaintenanceWindow, now time.Time) (time.Time, bool) {
	if w.Mode != "window" || !validTimeStr(w.StartTime) {
		return time.Time{}, false
	}
	ref := now.UTC()
	startMin := parseTimeStr(w.StartTime)
	today := time.Date(ref.Year(), ref.Month(), ref.Day(), 0, 0, 0, 0, time.UTC)

	for d := 0; d <= nextWindowSearchDays; d++ {
		day := today.AddDate(0, 0, d)
		if !frequencyAllowsDay(w, day) {
			continue
		}
		candidate := time.Date(day.Year(), day.Month(), day.Day(), startMin/60, startMin%60, 0, 0, time.UTC)
		if !candidate.Before(ref) {
			return candidate, true
		}
	}
	return time.Time{}, false
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

// checkAutoUpdatesSuppressed returns true if a docker_check_updates command was
// sent to this device recently (within 10 minutes). This prevents "check for
// updates" from silently triggering an auto-update — the user should see the
// result first and decide whether to update manually.
func (h *Handlers) checkAutoUpdatesSuppressed(ctx context.Context, deviceID string, now time.Time) bool {
	cmds, err := h.commandRepo.ListByDevice(ctx, deviceID, 10)
	if err != nil {
		return false
	}
	for _, cmd := range cmds {
		if cmd.Action == "docker_check_updates" && now.Sub(cmd.CreatedAt) < 10*time.Minute {
			return true
		}
	}
	return false
}

// checkAutoUpdates looks at incoming telemetry for containers with update_available=true,
// matches them against auto-update policies, and sends docker_update commands.
//
// When stagger_seconds > 0, at most one docker_update command is dispatched per
// telemetry cycle and no dispatch happens at all until stagger_seconds have
// passed since the last docker_update command on this device. This throttles
// fan-out so a device with many auto-update targets does not pull N images in
// parallel and exhaust Docker Hub's anonymous 100-pull-per-6h rate limit.
func (h *Handlers) checkAutoUpdates(ctx context.Context, deviceID string, data *models.FullTelemetryData) {
	if h.autoUpdateRepo == nil || h.commandRepo == nil || data.Docker == nil {
		return
	}
	now := h.clock()

	// Suppress auto-updates when a manual "check for updates" was recently issued.
	// The user expects to see the result and decide — not have it auto-applied.
	if h.checkAutoUpdatesSuppressed(ctx, deviceID, now) {
		return
	}

	// Check maintenance window
	cfg := h.loadAutomationConfig(ctx)
	if !inMaintenanceWindow(cfg.DockerUpdate, now) {
		return
	}
	cooldown := time.Duration(cfg.DockerUpdate.CooldownMinutes) * time.Minute
	windowEnd := maintenanceWindowEnd(cfg.DockerUpdate, now)
	stagger := time.Duration(cfg.DockerUpdate.StaggerSeconds) * time.Second

	// Stagger gate: if the last docker_update for this device was within the
	// stagger window, skip the entire pass.
	if stagger > 0 {
		if last := h.lastDockerUpdateAt(ctx, deviceID); !last.IsZero() && now.Sub(last) < stagger {
			return
		}
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
	// A project whose containers report more than one working-dir label is
	// dispatched by project name alone (AUTOUPD-HARDEN-001 AD-011). The server
	// cannot resolve symlinks, so it cannot tell a link and its target from two
	// directories; the agent can, and refuses a genuinely ambiguous project
	// itself, which shows as a failed command with the usual backoff.
	multiDir := projectsWithSeveralWorkDirs(data.Docker.Containers)

	for _, c := range data.Docker.Containers {
		if c.UpdateAvailable == nil || !*c.UpdateAvailable {
			continue
		}

		// Check for stack-level policy (by compose project name)
		project := c.Labels["com.docker.compose.project"]
		workDir := c.Labels["com.docker.compose.project.working_dir"]

		if project != "" && workDir != "" {
			if pol, ok := policyMap[project]; ok && pol.IsStack && !triggeredStacks[project] {
				if !recentlyTriggered(pol, cooldown, now) && !pol.InBackoff(now) {
					params := map[string]interface{}{"compose_project": project, "compose_work_dir": workDir}
					if multiDir[project] {
						delete(params, "compose_work_dir")
					}
					dispatched := h.dispatchAutoUpdate(ctx, deviceID, pol, windowEnd, params)
					triggeredStacks[project] = true
					if dispatched && stagger > 0 {
						return
					}
				}
				continue
			}
		}

		// Check for container-level policy. The name is the stable identity
		// (container IDs change on every recreate); the current ID is sent as
		// well because legacy agents resolve by ID only (FR-001, D-008).
		name := c.Name
		if pol, ok := policyMap[name]; ok && !pol.IsStack {
			if !recentlyTriggered(pol, cooldown, now) && !pol.InBackoff(now) {
				dispatched := h.dispatchAutoUpdate(ctx, deviceID, pol, windowEnd, map[string]interface{}{
					"container":    name,
					"container_id": c.ID,
				})
				if dispatched && stagger > 0 {
					return
				}
			}
		}
	}
}

// lastDockerUpdateAt returns the creation time of the most recent docker_update
// or docker_bulk_update command sent to this device, or the zero time if none
// appears in the last 20 commands. Used by the stagger gate to spread updates
// out across telemetry cycles.
func (h *Handlers) lastDockerUpdateAt(ctx context.Context, deviceID string) time.Time {
	cmds, err := h.commandRepo.ListByDevice(ctx, deviceID, 20)
	if err != nil {
		return time.Time{}
	}
	for _, cmd := range cmds {
		if cmd.Action == "docker_update" || cmd.Action == "docker_bulk_update" {
			return cmd.CreatedAt
		}
	}
	return time.Time{}
}

// recentlyTriggered returns true if the policy was triggered within the cooldown period.
func recentlyTriggered(p *models.AutoUpdatePolicy, cooldown time.Duration, now time.Time) bool {
	return p.LastTriggeredAt != nil && now.Sub(*p.LastTriggeredAt) < cooldown
}

// dispatchAutoUpdate creates a docker_update command through the command
// service and reports whether one was created. The cooldown is not started
// here: the service records the policy outcome when the command reaches a
// terminal state (AD-007). A target conflict skips the dispatch and is logged;
// it does not count toward the backoff (FR-061). windowEnd caps expires_at for a
// window-mode maintenance window (FR-025).
func (h *Handlers) dispatchAutoUpdate(ctx context.Context, deviceID string, pol *models.AutoUpdatePolicy, windowEnd time.Time, params map[string]interface{}) bool {
	policyID := pol.ID
	res, err := h.cmdSvc().Dispatch(ctx, commands.DispatchRequest{
		DeviceID: deviceID, Action: models.ActionDockerUpdate, Params: params,
		Origin: models.OriginAutoUpdate, PolicyID: &policyID, WindowEnd: windowEnd,
	})
	if err != nil {
		slog.Error("auto-update: dispatch command", "device_id", deviceID, "target", pol.Target, "error", err.Error())
		return false
	}
	if res.Conflict != nil {
		slog.Info("auto-dispatch conflict skip", "device_id", deviceID, "origin", models.OriginAutoUpdate,
			"target", pol.Target, "existing_command_id", res.Conflict.ID, "target_key", res.ConflictKey)
		return false
	}
	slog.Info("auto-update triggered", "device", deviceID, "target", pol.Target, "is_stack", pol.IsStack)
	return true
}

// maintenanceWindowEnd returns the end of the window now falls in, or the zero
// time when the mode is not "window" (anytime has no end). It is only
// meaningful while inMaintenanceWindow(w, now) holds. An overnight window that
// started before midnight ends on the next calendar day.
func maintenanceWindowEnd(w models.MaintenanceWindow, now time.Time) time.Time {
	if w.Mode != "window" {
		return time.Time{}
	}
	now = now.UTC()
	startMin := parseTimeStr(w.StartTime)
	endMin := parseTimeStr(w.EndTime)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	end := midnight.Add(time.Duration(endMin) * time.Minute)
	if startMin > endMin && now.Hour()*60+now.Minute() >= startMin {
		end = end.AddDate(0, 0, 1)
	}
	return end
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
		slog.Error("update auto-patch", "error", err.Error())
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
	now := h.clock()

	autoPatch, err := h.devices.GetAutoPatch(ctx, deviceID)
	if err != nil || !autoPatch {
		return
	}

	// Check maintenance window
	cfg := h.loadAutomationConfig(ctx)
	if !inMaintenanceWindow(cfg.OSPatch, now) {
		return
	}
	cooldown := time.Duration(cfg.OSPatch.CooldownMinutes) * time.Minute

	// Check for recent os_update commands to avoid duplicates
	cmds, err := h.commandRepo.ListByDevice(ctx, deviceID, 10)
	if err != nil {
		return
	}
	for _, cmd := range cmds {
		if cmd.Action == "os_update" && now.Sub(cmd.CreatedAt) < cooldown {
			return // Already sent recently
		}
	}

	// Reboot-class gate (PATCH-GATE AD-009): checkAutoPatch is the ONLY
	// os_update creation site that may set include_reboot_class, and only
	// under the "gated" policy. The early inMaintenanceWindow return above
	// guarantees this dispatch is in-window (FR-021).
	params := map[string]interface{}{"mode": "full"}
	if cfg.OSPatch.RebootClass == "gated" {
		params["include_reboot_class"] = true
	}

	res, err := h.cmdSvc().Dispatch(ctx, commands.DispatchRequest{
		DeviceID: deviceID, Action: models.ActionOSUpdate, Params: params, Origin: models.OriginAutoPatch,
		WindowEnd: maintenanceWindowEnd(cfg.OSPatch, now),
	})
	if err != nil {
		slog.Error("auto-patch: dispatch command", "device_id", deviceID, "error", err.Error())
		return
	}
	if res.Conflict != nil {
		slog.Info("auto-dispatch conflict skip", "device_id", deviceID, "origin", models.OriginAutoPatch,
			"existing_command_id", res.Conflict.ID, "target_key", res.ConflictKey)
		return
	}

	slog.Info("auto-patch triggered", "device", deviceID, "pending_updates", data.Updates.PendingUpdates)
}

// projectsWithSeveralWorkDirs reports the compose projects whose containers
// carry more than one distinct working-dir label (compared by target key, so a
// trailing slash is not a second directory). Two labels may still be one real
// directory reached through a symlink, which only the agent can resolve.
func projectsWithSeveralWorkDirs(containers []models.ContainerInfo) map[string]bool {
	dirs := map[string]map[string]bool{}
	for _, c := range containers {
		project := c.Labels[models.LabelComposeProject]
		wd := c.Labels[models.LabelComposeWorkingDir]
		if project == "" || wd == "" {
			continue
		}
		if dirs[project] == nil {
			dirs[project] = map[string]bool{}
		}
		dirs[project][models.TargetKeyCompose(wd)] = true
	}
	out := map[string]bool{}
	for project, set := range dirs {
		if len(set) > 1 {
			out[project] = true
		}
	}
	return out
}

// auditTarget is the target as stored in the audit: valid UTF-8, no NUL, at
// most 128 bytes.
func auditTarget(target string) string {
	return models.TruncateHead(models.SanitizeText(target), 128)
}

// auditDockerPolicy writes one kind=policy docker_audit row for the acting
// admin. A failed write is logged without the target or any body.
func (h *Handlers) auditDockerPolicy(r *http.Request, device, action string, data map[string]interface{}) {
	audit := h.dockerAudit
	if audit == nil {
		if h.dockerRepo == nil {
			return
		}
		audit = h.dockerRepo.Audit
	}
	_, sub, _ := dockerActor(r)
	if err := audit(r.Context(), device, "policy", action, sub, data); err != nil {
		slog.Warn("docker policy audit write failed", "device_id", device, "action", action,
			"policy_kind", data["policy_kind"], "error", err.Error())
	}
}
