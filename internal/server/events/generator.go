package events

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
)

// Dispatcher is the interface for sending notifications.
type Dispatcher interface {
	Dispatch(ctx context.Context, alert models.Alert)
}

// Generator creates and stores events based on telemetry data and alert rules.
type Generator struct {
	repo          db.EventRepository
	hub           *websocket.Hub
	alertRuleRepo db.AlertRuleRepository
	dispatcher    Dispatcher
	commandRepo   db.CommandRepository

	mu            sync.Mutex
	lastSent      map[string]time.Time // key: "deviceID:ruleID" or "deviceID:eventType"
	activeUpdates map[string]int       // key: deviceID → count of in-progress container updates
	updateStarted map[string]time.Time // key: deviceID → when the first update_started was received

	// Short-lived cache for ListEnabled. Each telemetry snapshot fans out into
	// many alert check paths (service, nic, process, container, UPS, GPU, …),
	// each of which used to hit the DB for the full enabled-rules set. Under
	// DB contention this caused cascades of "context deadline exceeded" errors
	// once the per-snapshot 30s context expired. A 5s TTL collapses the fan-out
	// to roughly one query per snapshot while keeping rule edits near-real-time.
	rulesMu      sync.Mutex
	rulesCache   []models.AlertRule
	rulesCacheAt time.Time
	// singleflight coalesces concurrent cold-cache loads into a single DB
	// query. Without it, N simultaneous callers on a cold cache all fire
	// parallel queries — the original thundering-herd pattern the cache was
	// meant to prevent.
	rulesLoad singleflight.Group
}

// rulesCacheTTL is how long a cached enabled-rules snapshot stays valid. Short
// enough that edits in the Settings UI feel immediate.
const rulesCacheTTL = 5 * time.Second

func NewGenerator(repo db.EventRepository, hub *websocket.Hub, alertRuleRepo db.AlertRuleRepository, dispatcher Dispatcher, commandRepo db.CommandRepository) *Generator {
	return &Generator{
		repo:          repo,
		hub:           hub,
		alertRuleRepo: alertRuleRepo,
		dispatcher:    dispatcher,
		commandRepo:   commandRepo,
		lastSent:      make(map[string]time.Time),
		activeUpdates: make(map[string]int),
		updateStarted: make(map[string]time.Time),
	}
}

// StartCleanup runs a background goroutine that periodically prunes stale
// entries from the lastSent and activeUpdates maps to prevent unbounded growth.
func (g *Generator) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				g.pruneStaleEntries()
			}
		}
	}()
}

func (g *Generator) pruneStaleEntries() {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Prune lastSent entries older than 1 hour — well beyond any cooldown period
	cutoff := time.Now().Add(-1 * time.Hour)
	for key, t := range g.lastSent {
		if t.Before(cutoff) {
			delete(g.lastSent, key)
		}
	}

	// Prune activeUpdates entries stuck for over 30 minutes — the agent
	// likely crashed mid-update if it hasn't sent a completion event by now.
	updateCutoff := time.Now().Add(-30 * time.Minute)
	for deviceID, started := range g.updateStarted {
		if started.Before(updateCutoff) {
			delete(g.activeUpdates, deviceID)
			delete(g.updateStarted, deviceID)
		}
	}
}

// listEnabledRules returns enabled alert rules, serving from an in-process cache
// when fresh. The cache is invalidated either by age (rulesCacheTTL) or by a
// direct call to InvalidateRulesCache (used by the settings handlers). Cold-cache
// loads are coalesced through a singleflight group so a burst of callers only
// produces one DB query.
func (g *Generator) listEnabledRules(ctx context.Context) ([]models.AlertRule, error) {
	g.rulesMu.Lock()
	if g.rulesCache != nil && time.Since(g.rulesCacheAt) < rulesCacheTTL {
		rules := g.rulesCache
		g.rulesMu.Unlock()
		return rules, nil
	}
	g.rulesMu.Unlock()

	v, err, _ := g.rulesLoad.Do("rules", func() (interface{}, error) {
		rules, err := g.alertRuleRepo.ListEnabled(ctx)
		if err != nil {
			return nil, err
		}
		g.rulesMu.Lock()
		g.rulesCache = rules
		g.rulesCacheAt = time.Now()
		g.rulesMu.Unlock()
		return rules, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]models.AlertRule), nil
}

// InvalidateRulesCache drops the cached enabled-rules snapshot so the next
// caller refreshes from the DB. Handlers that create, update, or delete alert
// rules must call this so edits take effect immediately instead of waiting for
// the TTL to expire.
func (g *Generator) InvalidateRulesCache() {
	g.rulesMu.Lock()
	g.rulesCache = nil
	g.rulesCacheAt = time.Time{}
	g.rulesMu.Unlock()
}

// onCooldown returns true if an event with this key was created within the cooldown period.
func (g *Generator) onCooldown(key string, cooldown time.Duration) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if last, exists := g.lastSent[key]; exists && time.Since(last) < cooldown {
		return true
	}
	g.lastSent[key] = time.Now()
	return false
}

func (g *Generator) createEvent(ctx context.Context, e *models.Event) {
	if err := g.repo.Create(ctx, e); err != nil {
		slog.Error("create event", "error", err.Error())
		return
	}
	g.hub.BroadcastEvent(e)
}

// createEventAndNotify creates the event, broadcasts it, and dispatches notifications for matching rules.
func (g *Generator) createEventAndNotify(ctx context.Context, e *models.Event, rule *models.AlertRule, hostname string, value float64) {
	g.createEvent(ctx, e)
	if rule != nil && rule.Notify && g.dispatcher != nil {
		g.dispatcher.Dispatch(ctx, models.Alert{
			Rule:     rule,
			Event:    e,
			DeviceID: e.DeviceID,
			Hostname: hostname,
			Value:    value,
		})
	}
}

// disruptiveActions are command actions that are expected to take a device offline.
// agent_update is intentionally excluded — it's just a trigger; the actual update
// lifecycle is tracked by agent_update_* events reported by the agent itself.
var disruptiveActions = map[string]string{
	"reboot":    "reboot",
	"os_update": "OS update",
}

// commandLabels maps all command actions to human-readable labels for event messages.
var commandLabels = map[string]string{
	"docker_stop":          "Docker stop",
	"docker_restart":       "Docker restart",
	"docker_start":         "Docker start",
	"docker_update":        "Docker update",
	"docker_check_updates": "Docker check updates",
	"docker_bulk_update":   "Docker bulk update",
	"reboot":               "Reboot",
	"agent_update":         "Agent update",
	"os_update":            "OS update",
	"fetch_logs":           "Log fetch",
	"enable_auto_updates":  "Enable auto-updates",
	"run_device_probe":     "Device probe",
}

// recentDisruptiveCommand returns the most recent disruptive command sent to a device
// within the last 2 minutes, or nil if none found.
func (g *Generator) recentDisruptiveCommand(ctx context.Context, deviceID string) *models.Command {
	if g.commandRepo == nil {
		return nil
	}
	cmds, err := g.commandRepo.ListByDevice(ctx, deviceID, 5)
	if err != nil {
		return nil
	}
	cutoff := time.Now().UTC().Add(-2 * time.Minute)
	for i := range cmds {
		c := &cmds[i]
		if _, ok := disruptiveActions[c.Action]; ok && c.CreatedAt.After(cutoff) {
			return c
		}
	}
	return nil
}

// CommandSent creates an informational event when any command is dispatched.
func (g *Generator) CommandSent(ctx context.Context, deviceID, hostname, action string, params map[string]interface{}) {
	label, ok := commandLabels[action]
	if !ok {
		label = action
	}
	msg := fmt.Sprintf("%s initiated on %s", label, hostname)
	// Add container context for Docker commands
	if cname, _ := params["container_id"].(string); cname != "" {
		msg = fmt.Sprintf("%s initiated for %s on %s", label, cname, hostname)
	}
	g.createEvent(ctx, &models.Event{
		DeviceID:  deviceID,
		Type:      models.EventCommandSent,
		Severity:  models.SeverityInfo,
		Message:   msg,
		CreatedAt: time.Now().UTC(),
	})
}

// CommandCompleted creates an event when any command finishes (success or failure).
func (g *Generator) CommandCompleted(ctx context.Context, deviceID, hostname, action, status, message string) {
	label, ok := commandLabels[action]
	if !ok {
		label = action
	}

	severity := models.SeverityInfo
	msg := fmt.Sprintf("%s completed on %s", label, hostname)
	if status == "error" {
		severity = models.SeverityWarning
		// Include a brief reason — truncate long output
		reason := message
		if len(reason) > 200 {
			reason = reason[:200] + "..."
		}
		msg = fmt.Sprintf("%s failed on %s: %s", label, hostname, reason)
	}

	g.createEvent(ctx, &models.Event{
		DeviceID:  deviceID,
		Type:      models.EventCommandCompleted,
		Severity:  severity,
		Message:   msg,
		CreatedAt: time.Now().UTC(),
	})
}

func (g *Generator) DeviceOnline(ctx context.Context, deviceID, hostname string) {
	key := deviceID + ":" + string(models.EventDeviceOnline)
	if g.onCooldown(key, 5*time.Minute) {
		return
	}
	msg := fmt.Sprintf("Device %s came online", hostname)
	if cmd := g.recentDisruptiveCommand(ctx, deviceID); cmd != nil {
		msg += fmt.Sprintf(" (after %s)", disruptiveActions[cmd.Action])
	}
	g.createEvent(ctx, &models.Event{
		DeviceID:  deviceID,
		Type:      models.EventDeviceOnline,
		Severity:  models.SeverityInfo,
		Message:   msg,
		CreatedAt: time.Now().UTC(),
	})
}

func (g *Generator) DeviceOffline(ctx context.Context, deviceID, hostname string) {
	msg := fmt.Sprintf("Device %s went offline", hostname)
	severity := models.SeverityWarning
	if cmd := g.recentDisruptiveCommand(ctx, deviceID); cmd != nil {
		label := disruptiveActions[cmd.Action]
		ago := time.Since(cmd.CreatedAt).Truncate(time.Second)
		msg += fmt.Sprintf(" (%s sent %s ago)", label, ago)
		severity = models.SeverityInfo // expected downtime, not a warning
	}

	e := &models.Event{
		DeviceID:  deviceID,
		Type:      models.EventDeviceOffline,
		Severity:  severity,
		Message:   msg,
		CreatedAt: time.Now().UTC(),
	}

	// Check for device_offline alert rules
	rule := g.findMatchingRule(ctx, "device_offline", deviceID, hostname, 1)
	if rule != nil {
		key := fmt.Sprintf("%s:rule:%d", deviceID, rule.ID)
		cd := time.Duration(rule.CooldownSeconds) * time.Second
		if g.onCooldown(key, cd) {
			return
		}
		if severity != models.SeverityInfo {
			e.Severity = models.EventSeverity(rule.Severity)
		}
		g.createEventAndNotify(ctx, e, rule, hostname, 1)
	} else {
		// Fallback: create event without notification
		key := deviceID + ":" + string(models.EventDeviceOffline)
		if g.onCooldown(key, 15*time.Minute) {
			return
		}
		g.createEvent(ctx, e)
	}
}

func (g *Generator) CheckHeartbeatThresholds(ctx context.Context, deviceID, hostname string, data *models.HeartbeatData) {
	g.evaluateMetric(ctx, deviceID, "mem_percent", data.MemPercent, hostname, models.EventMemHigh,
		func(val float64) string { return fmt.Sprintf("RAM usage at %.1f%%", val) })

	g.evaluateMetric(ctx, deviceID, "disk_percent", data.DiskRootPercent, hostname, models.EventDiskHigh,
		func(val float64) string { return fmt.Sprintf("Disk usage at %.1f%%", val) })

	if data.LogErrors > 0 {
		g.evaluateMetric(ctx, deviceID, "log_errors", float64(data.LogErrors), hostname, models.EventLogErrors,
			func(val float64) string { return fmt.Sprintf("%d log errors detected since last heartbeat", int(val)) })
	}
}

func (g *Generator) CheckTelemetryThresholds(ctx context.Context, deviceID, hostname string, data *models.FullTelemetryData) {
	if data.Memory != nil {
		g.evaluateMetric(ctx, deviceID, "mem_percent", data.Memory.UsagePercent, hostname, models.EventMemHigh,
			func(val float64) string { return fmt.Sprintf("RAM usage at %.1f%%", val) })
	}
	if data.Disks != nil {
		for _, fs := range data.Disks.Filesystems {
			g.evaluateMetric(ctx, deviceID, "disk_percent", fs.UsagePercent, hostname, models.EventDiskHigh,
				func(val float64) string { return fmt.Sprintf("Disk %s usage at %.1f%%", fs.MountPoint, val) })
		}
	}
	// Pending updates are shown as dashboard status, not as alert events.

	// Check service, NIC, and process alerts
	if data.Services != nil {
		g.CheckServiceAlerts(ctx, deviceID, hostname, data.Services)
	}
	if data.Network != nil && data.Network.Interfaces != nil {
		g.CheckNICAlerts(ctx, deviceID, hostname, data.Network.Interfaces)
	}
	if data.Procs != nil {
		g.CheckProcessAlerts(ctx, deviceID, hostname, data.Procs)
	}
	if data.UPS != nil {
		g.CheckUPSAlerts(ctx, deviceID, hostname, data.UPS)
	}
	if data.Docker != nil && data.Docker.Available {
		g.CheckContainerThresholds(ctx, deviceID, hostname, data.Docker.Containers)
	}
	if data.WebServers != nil {
		g.CheckWebServerAlerts(ctx, deviceID, hostname, data.WebServers)
	}
	if data.USB != nil {
		g.CheckUSBAlerts(ctx, deviceID, hostname, data.USB)
	}
	if data.Hardware != nil {
		g.CheckDiskSmartAlerts(ctx, deviceID, hostname, data.Hardware.DiskDrives)
	}
	if data.GPUTelemetry != nil {
		g.CheckGPUAlerts(ctx, deviceID, hostname, data.GPUTelemetry)
	}
	// Per-interval auth-failure metric (LOG-001, AD-005).
	// Gated on both Security != nil AND FailedLoginsInterval != nil so that
	// non-Linux agents (which omit the field) never trigger this path (FR-011).
	// Negative values are clamped to 0 before evaluation (SEC-006).
	if data.Security != nil && data.Security.FailedLoginsInterval != nil {
		v := *data.Security.FailedLoginsInterval
		if v < 0 {
			v = 0
		}
		g.evaluateMetric(ctx, deviceID, "failed_logins_interval",
			float64(v), hostname,
			models.EventAuthFailure,
			func(f float64) string {
				return fmt.Sprintf("%d authentication failure(s) on %s in last interval",
					int(f), hostname)
			})
	}
}

// CheckServiceAlerts checks service state against service_state alert rules.
func (g *Generator) CheckServiceAlerts(ctx context.Context, deviceID, hostname string, services []models.ServiceInfo) {
	rules, err := g.listEnabledRules(ctx)
	if err != nil {
		slog.Error("check service alerts", "error", err.Error())
		return
	}

	for i := range rules {
		r := &rules[i]
		if r.Metric != "service_state" || r.TargetName == "" {
			continue
		}
		if !matchesDeviceScope(r.IncludeDevices, r.ExcludeDevices, deviceID, hostname) {
			continue
		}

		for _, svc := range services {
			if !strings.EqualFold(svc.Name, r.TargetName) {
				continue
			}
			// Check if service state matches the target state(s)
			if !matchesTargetState(r.TargetState, svc.State) {
				continue
			}

			key := fmt.Sprintf("%s:rule:%d:%s", deviceID, r.ID, svc.Name)
			cd := time.Duration(r.CooldownSeconds) * time.Second
			if g.onCooldown(key, cd) {
				continue
			}

			eventType := models.EventServiceStopped
			if strings.Contains(strings.ToLower(svc.State), "failed") {
				eventType = models.EventServiceFailed
			}

			e := &models.Event{
				DeviceID:  deviceID,
				Type:      eventType,
				Severity:  models.EventSeverity(r.Severity),
				Message:   fmt.Sprintf("Service %s is %s", svc.Name, svc.State),
				CreatedAt: time.Now().UTC(),
			}
			g.createEventAndNotify(ctx, e, r, "", 1)
		}
	}
}

// CheckNICAlerts checks network interface state against nic_state alert rules.
func (g *Generator) CheckNICAlerts(ctx context.Context, deviceID, hostname string, interfaces []models.NetworkInterface) {
	rules, err := g.listEnabledRules(ctx)
	if err != nil {
		slog.Error("check nic alerts", "error", err.Error())
		return
	}

	for i := range rules {
		r := &rules[i]
		if r.Metric != "nic_state" || r.TargetName == "" {
			continue
		}
		if !matchesDeviceScope(r.IncludeDevices, r.ExcludeDevices, deviceID, hostname) {
			continue
		}

		for _, iface := range interfaces {
			if !strings.EqualFold(iface.Name, r.TargetName) {
				continue
			}
			// Check if interface state matches the target state(s)
			if r.TargetState == "" || strings.EqualFold(r.TargetState, "any") {
				// "any" or empty = any non-UP state (existing behavior)
				if iface.State == "UP" {
					continue
				}
			} else if !matchesTargetState(r.TargetState, iface.State) {
				continue
			}

			key := fmt.Sprintf("%s:rule:%d:%s", deviceID, r.ID, iface.Name)
			cd := time.Duration(r.CooldownSeconds) * time.Second
			if g.onCooldown(key, cd) {
				continue
			}

			e := &models.Event{
				DeviceID:  deviceID,
				Type:      models.EventNICDown,
				Severity:  models.EventSeverity(r.Severity),
				Message:   fmt.Sprintf("Network interface %s is %s", iface.Name, iface.State),
				CreatedAt: time.Now().UTC(),
			}
			g.createEventAndNotify(ctx, e, r, "", 1)
		}
	}
}

// CheckProcessAlerts checks for missing processes against process_missing alert rules.
func (g *Generator) CheckProcessAlerts(ctx context.Context, deviceID, hostname string, procs *models.ProcessInfo) {
	rules, err := g.listEnabledRules(ctx)
	if err != nil {
		slog.Error("check process alerts", "error", err.Error())
		return
	}

	// Build a set of running process names
	processNames := make(map[string]bool)
	if procs.TopByCPU != nil {
		for _, p := range procs.TopByCPU {
			processNames[strings.ToLower(p.Name)] = true
		}
	}
	if procs.TopByMemory != nil {
		for _, p := range procs.TopByMemory {
			processNames[strings.ToLower(p.Name)] = true
		}
	}

	for i := range rules {
		r := &rules[i]
		if r.Metric != "process_missing" || r.TargetName == "" {
			continue
		}
		if !matchesDeviceScope(r.IncludeDevices, r.ExcludeDevices, deviceID, hostname) {
			continue
		}

		// Check if the target process is running
		if processNames[strings.ToLower(r.TargetName)] {
			continue // Process is running, no alert needed
		}

		key := fmt.Sprintf("%s:rule:%d:%s", deviceID, r.ID, r.TargetName)
		cd := time.Duration(r.CooldownSeconds) * time.Second
		if g.onCooldown(key, cd) {
			continue
		}

		e := &models.Event{
			DeviceID:  deviceID,
			Type:      models.EventProcessMissing,
			Severity:  models.EventSeverity(r.Severity),
			Message:   fmt.Sprintf("Process %s not found in running processes", r.TargetName),
			CreatedAt: time.Now().UTC(),
		}
		g.createEventAndNotify(ctx, e, r, "", 1)
	}
}

// CheckUPSAlerts checks UPS state and creates events for power transitions.
func (g *Generator) CheckUPSAlerts(ctx context.Context, deviceID, hostname string, ups *models.UPSInfo) {
	if ups.Name == "" {
		return
	}

	wasOnBatteryKey := deviceID + ":ups_was_on_battery"

	if ups.OnBattery {
		// On battery — fire critical event
		if ups.LowBattery {
			// Low battery is more urgent
			charge := ""
			if ups.BatteryCharge != nil {
				charge = fmt.Sprintf(" (%.0f%%)", *ups.BatteryCharge)
			}
			e := &models.Event{
				DeviceID:  deviceID,
				Type:      models.EventUPSLowBattery,
				Severity:  models.SeverityCrit,
				Message:   fmt.Sprintf("UPS %s low battery%s", ups.Name, charge),
				CreatedAt: time.Now().UTC(),
			}
			rule := g.findMatchingRule(ctx, "ups_on_battery", deviceID, hostname, 1)
			if rule != nil {
				key := fmt.Sprintf("%s:rule:%d", deviceID, rule.ID)
				if !g.onCooldown(key, time.Duration(rule.CooldownSeconds)*time.Second) {
					e.Severity = models.EventSeverity(rule.Severity)
					g.createEventAndNotify(ctx, e, rule, "", 1)
				}
			} else {
				key := deviceID + ":" + string(models.EventUPSLowBattery)
				if !g.onCooldown(key, 5*time.Minute) {
					g.createEvent(ctx, e)
				}
			}
		} else {
			e := &models.Event{
				DeviceID:  deviceID,
				Type:      models.EventUPSOnBattery,
				Severity:  models.SeverityCrit,
				Message:   fmt.Sprintf("UPS %s running on battery", ups.Name),
				CreatedAt: time.Now().UTC(),
			}
			rule := g.findMatchingRule(ctx, "ups_on_battery", deviceID, hostname, 1)
			if rule != nil {
				key := fmt.Sprintf("%s:rule:%d", deviceID, rule.ID)
				if !g.onCooldown(key, time.Duration(rule.CooldownSeconds)*time.Second) {
					e.Severity = models.EventSeverity(rule.Severity)
					g.createEventAndNotify(ctx, e, rule, "", 1)
				}
			} else {
				key := deviceID + ":" + string(models.EventUPSOnBattery)
				if !g.onCooldown(key, 15*time.Minute) {
					g.createEvent(ctx, e)
				}
			}
		}

		// Track that we were on battery
		g.mu.Lock()
		g.lastSent[wasOnBatteryKey] = time.Now()
		g.mu.Unlock()
	} else {
		// On line power — check if we were previously on battery
		g.mu.Lock()
		_, wasOnBattery := g.lastSent[wasOnBatteryKey]
		if wasOnBattery {
			delete(g.lastSent, wasOnBatteryKey)
		}
		g.mu.Unlock()

		if wasOnBattery {
			e := &models.Event{
				DeviceID:  deviceID,
				Type:      models.EventUPSRestored,
				Severity:  models.SeverityInfo,
				Message:   fmt.Sprintf("UPS %s restored to line power", ups.Name),
				CreatedAt: time.Now().UTC(),
			}
			rule := g.findMatchingRule(ctx, "ups_on_battery", deviceID, hostname, 1)
			if rule != nil {
				g.createEventAndNotify(ctx, e, rule, "", 0)
			} else {
				g.createEvent(ctx, e)
			}
		}
	}

	// Battery charge threshold check via evaluateMetric
	if ups.BatteryCharge != nil {
		g.evaluateMetric(ctx, deviceID, "ups_battery_percent", *ups.BatteryCharge, hostname, models.EventUPSLowBattery,
			func(val float64) string { return fmt.Sprintf("UPS %s battery at %.0f%%", ups.Name, val) })
	}
}

// CheckContainerThresholds checks per-container CPU and memory against alert rules.
func (g *Generator) CheckContainerThresholds(ctx context.Context, deviceID, hostname string, containers []models.ContainerInfo) {
	rules, err := g.listEnabledRules(ctx)
	if err != nil {
		slog.Error("check container thresholds", "error", err.Error())
		return
	}

	for i := range rules {
		r := &rules[i]
		if r.TargetName == "" {
			continue // container threshold rules require a target name
		}
		if !matchesDeviceScope(r.IncludeDevices, r.ExcludeDevices, deviceID, hostname) {
			continue
		}

		var metricName string
		var eventType models.EventType
		switch r.Metric {
		case "container_cpu_percent":
			metricName = "CPU"
			eventType = models.EventContainerHighCPU
		case "container_mem_percent":
			metricName = "Memory"
			eventType = models.EventContainerHighMem
		case "container_cpu_limit_percent":
			metricName = "CPU (of limit)"
			eventType = models.EventContainerCPUOverLimit
		default:
			continue
		}

		for _, c := range containers {
			if c.State != "running" {
				continue
			}
			if !strings.EqualFold(c.Name, r.TargetName) {
				continue
			}
			if !MatchesContainerScope(r.IncludeContainers, r.ExcludeContainers, c.Name) {
				continue
			}

			var value float64
			switch r.Metric {
			case "container_cpu_percent":
				value = c.CPUPercent
			case "container_mem_percent":
				if c.MemLimit <= 0 {
					continue
				}
				value = float64(c.MemUsage) / float64(c.MemLimit) * 100
			case "container_cpu_limit_percent":
				if c.CPULimit <= 0 {
					continue // no CPU limit configured
				}
				// CPULimit is NanoCPUs; convert to max CPU% = NanoCPUs / 1e9 * 100
				cpuLimitPercent := float64(c.CPULimit) / 1e9 * 100
				value = c.CPUPercent / cpuLimitPercent * 100
			}

			if !compareValue(value, r.Operator, r.Threshold) {
				continue
			}

			key := fmt.Sprintf("%s:rule:%d:%s", deviceID, r.ID, c.Name)
			cd := time.Duration(r.CooldownSeconds) * time.Second
			if g.onCooldown(key, cd) {
				continue
			}

			e := &models.Event{
				DeviceID:      deviceID,
				Type:          eventType,
				Severity:      models.EventSeverity(r.Severity),
				Message:       fmt.Sprintf("Container %s %s at %.1f%%", c.Name, metricName, value),
				ContainerID:   c.ShortID,
				ContainerName: c.Name,
				CreatedAt:     time.Now().UTC(),
			}
			g.createEventAndNotify(ctx, e, r, hostname, value)
		}
	}
}

// CheckDockerEvent creates an event from a Docker container state change.
func (g *Generator) CheckDockerEvent(ctx context.Context, deviceID, hostname string, evt *models.DockerEvent) {
	now := time.Now().UTC()
	// Short ID matches ContainerInfo.ShortID used by the dashboard's container detail
	// route. Docker IDs are typically 64 hex chars; short form is first 12.
	shortID := evt.ContainerID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}
	switch evt.Action {
	case "start":
		g.createEvent(ctx, &models.Event{
			DeviceID: deviceID, Type: models.EventContainerStart, Severity: models.SeverityInfo,
			Message:     fmt.Sprintf("Container %s started (%s)", evt.ContainerName, evt.Image),
			ContainerID: shortID, ContainerName: evt.ContainerName, CreatedAt: now,
		})
	case "stop":
		g.createEvent(ctx, &models.Event{
			DeviceID: deviceID, Type: models.EventContainerStop, Severity: models.SeverityInfo,
			Message:     fmt.Sprintf("Container %s stopped", evt.ContainerName),
			ContainerID: shortID, ContainerName: evt.ContainerName, CreatedAt: now,
		})
	case "die":
		g.mu.Lock()
		updating := g.activeUpdates[deviceID] > 0
		g.mu.Unlock()

		if updating {
			// Container died as part of a running update — expected, downgrade to info
			g.createEvent(ctx, &models.Event{
				DeviceID: deviceID, Type: models.EventContainerDied, Severity: models.SeverityInfo,
				Message:     fmt.Sprintf("Container %s died (update in progress)", evt.ContainerName),
				ContainerID: shortID, ContainerName: evt.ContainerName, CreatedAt: now,
			})
		} else {
			// User explicitly opted this container out of container_died alerts —
			// suppress both the stored event and any notification.
			if g.isContainerExcluded(ctx, "container_died", deviceID, hostname, evt.ContainerName) {
				return
			}
			e := &models.Event{
				DeviceID: deviceID, Type: models.EventContainerDied, Severity: models.SeverityWarning,
				Message:     fmt.Sprintf("Container %s died", evt.ContainerName),
				ContainerID: shortID, ContainerName: evt.ContainerName, CreatedAt: now,
			}
			rule := g.findMatchingRuleForContainer(ctx, "container_died", deviceID, hostname, evt.ContainerName, 1)
			if rule != nil {
				key := fmt.Sprintf("%s:rule:%d", deviceID, rule.ID)
				if !g.onCooldown(key, time.Duration(rule.CooldownSeconds)*time.Second) {
					e.Severity = models.EventSeverity(rule.Severity)
					g.createEventAndNotify(ctx, e, rule, "", 1)
				}
			} else {
				g.createEvent(ctx, e)
			}
		}
	case "oom":
		if g.isContainerExcluded(ctx, "container_oom", deviceID, hostname, evt.ContainerName) {
			return
		}
		e := &models.Event{
			DeviceID: deviceID, Type: models.EventContainerOOM, Severity: models.SeverityCrit,
			Message:     fmt.Sprintf("Container %s OOM killed", evt.ContainerName),
			ContainerID: shortID, ContainerName: evt.ContainerName, CreatedAt: now,
		}
		rule := g.findMatchingRuleForContainer(ctx, "container_oom", deviceID, hostname, evt.ContainerName, 1)
		if rule != nil {
			key := fmt.Sprintf("%s:rule:%d", deviceID, rule.ID)
			if !g.onCooldown(key, time.Duration(rule.CooldownSeconds)*time.Second) {
				e.Severity = models.EventSeverity(rule.Severity)
				g.createEventAndNotify(ctx, e, rule, "", 1)
			}
		} else {
			g.createEvent(ctx, e)
		}
	case "create":
		g.createEvent(ctx, &models.Event{
			DeviceID: deviceID, Type: models.EventContainerCreated, Severity: models.SeverityInfo,
			Message:     fmt.Sprintf("Container %s created (%s)", evt.ContainerName, evt.Image),
			ContainerID: shortID, ContainerName: evt.ContainerName, CreatedAt: now,
		})
	case "destroy":
		g.createEvent(ctx, &models.Event{
			DeviceID: deviceID, Type: models.EventContainerDestroyed, Severity: models.SeverityInfo,
			Message:     fmt.Sprintf("Container %s removed", evt.ContainerName),
			ContainerID: shortID, ContainerName: evt.ContainerName, CreatedAt: now,
		})
	case "pause":
		g.createEvent(ctx, &models.Event{
			DeviceID: deviceID, Type: models.EventContainerPaused, Severity: models.SeverityInfo,
			Message:     fmt.Sprintf("Container %s paused", evt.ContainerName),
			ContainerID: shortID, ContainerName: evt.ContainerName, CreatedAt: now,
		})
	case "unpause":
		g.createEvent(ctx, &models.Event{
			DeviceID: deviceID, Type: models.EventContainerUnpaused, Severity: models.SeverityInfo,
			Message:     fmt.Sprintf("Container %s unpaused", evt.ContainerName),
			ContainerID: shortID, ContainerName: evt.ContainerName, CreatedAt: now,
		})
	case "update_started":
		g.mu.Lock()
		if g.activeUpdates[deviceID] == 0 {
			g.updateStarted[deviceID] = time.Now()
		}
		g.activeUpdates[deviceID]++
		g.mu.Unlock()
		g.createEvent(ctx, &models.Event{
			DeviceID: deviceID, Type: models.EventContainerUpdateStarted, Severity: models.SeverityInfo,
			Message:     fmt.Sprintf("Container %s update started", evt.ContainerName),
			ContainerID: shortID, ContainerName: evt.ContainerName, CreatedAt: now,
		})
	case "update_completed":
		g.mu.Lock()
		if g.activeUpdates[deviceID] > 0 {
			g.activeUpdates[deviceID]--
		}
		if g.activeUpdates[deviceID] == 0 {
			delete(g.activeUpdates, deviceID)
			delete(g.updateStarted, deviceID)
		}
		g.mu.Unlock()
		g.createEvent(ctx, &models.Event{
			DeviceID: deviceID, Type: models.EventContainerUpdateDone, Severity: models.SeverityInfo,
			Message:     fmt.Sprintf("Container %s updated successfully", evt.ContainerName),
			ContainerID: shortID, ContainerName: evt.ContainerName, CreatedAt: now,
		})
	case "update_failed":
		g.mu.Lock()
		if g.activeUpdates[deviceID] > 0 {
			g.activeUpdates[deviceID]--
		}
		if g.activeUpdates[deviceID] == 0 {
			delete(g.activeUpdates, deviceID)
			delete(g.updateStarted, deviceID)
		}
		g.mu.Unlock()
		g.createEvent(ctx, &models.Event{
			DeviceID: deviceID, Type: models.EventContainerUpdateFailed, Severity: models.SeverityWarning,
			Message:     fmt.Sprintf("Container %s update failed", evt.ContainerName),
			ContainerID: shortID, ContainerName: evt.ContainerName, CreatedAt: now,
		})
	}
}

// CheckWebServerAlerts checks SSL certificates across all proxy servers for expiry,
// and evaluates nginx access log metrics against configured alert rules.
func (g *Generator) CheckWebServerAlerts(ctx context.Context, deviceID, hostname string, ws *models.WebServerInfo) {
	for _, srv := range ws.Servers {
		for _, cert := range srv.Certs {
			if cert.DaysLeft <= 0 {
				// Expired
				g.evaluateMetric(ctx, deviceID, "cert_days_left", float64(cert.DaysLeft), hostname, models.EventCertExpired,
					func(val float64) string {
						return fmt.Sprintf("SSL certificate %s (%s) has expired", cert.Subject, srv.Name)
					})
			} else if cert.DaysLeft < 30 {
				// Expiring soon
				g.evaluateMetric(ctx, deviceID, "cert_days_left", float64(cert.DaysLeft), hostname, models.EventCertExpiring,
					func(val float64) string {
						return fmt.Sprintf("SSL certificate %s (%s) expires in %d days", cert.Subject, srv.Name, cert.DaysLeft)
					})
			}
		}
	}
	g.CheckNginxAccessAlerts(ctx, deviceID, hostname, ws)
}

// CheckNginxAccessAlerts evaluates nginx access log metrics from telemetry against
// user-configured alert rules. Only fires when explicit rules exist — no hardcoded
// fallback thresholds (AD-006, FR-018). The servers slice is capped at
// maxNginxProxyServers to prevent a compromised agent from flooding the server
// with database queries (SEC-005).
func (g *Generator) CheckNginxAccessAlerts(ctx context.Context, deviceID, hostname string, ws *models.WebServerInfo) {
	// Cap iteration at a reasonable maximum consistent with CheckGPUAlerts (SEC-005).
	const maxNginxProxyServers = 16

	servers := ws.Servers
	if len(servers) > maxNginxProxyServers {
		servers = servers[:maxNginxProxyServers]
	}

	for i := range servers {
		srv := &servers[i]
		if srv.AccessMetrics == nil {
			continue
		}
		m := srv.AccessMetrics

		g.evaluateMetric(ctx, deviceID, "nginx_5xx_count", float64(m.Status5xx), hostname, models.EventNginx5xxHigh,
			func(val float64) string {
				return fmt.Sprintf("Nginx 5xx errors: %d in last interval on %s", int(val), hostname)
			})

		g.evaluateMetric(ctx, deviceID, "nginx_4xx_count", float64(m.Status4xx), hostname, models.EventNginx4xxHigh,
			func(val float64) string {
				return fmt.Sprintf("Nginx 4xx errors: %d in last interval on %s", int(val), hostname)
			})

		g.evaluateMetric(ctx, deviceID, "nginx_request_count", float64(m.TotalRequests), hostname, models.EventNginxRequestHigh,
			func(val float64) string {
				return fmt.Sprintf("Nginx request count: %d in last interval on %s", int(val), hostname)
			})
	}
}

// CheckUSBAlerts checks for missing USB devices against usb_missing alert rules.
func (g *Generator) CheckUSBAlerts(ctx context.Context, deviceID, hostname string, usb *models.USBInfo) {
	rules, err := g.listEnabledRules(ctx)
	if err != nil {
		slog.Error("check usb alerts", "error", err.Error())
		return
	}

	// Build a set of present USB device identifiers (vendor_id:product_id and serial)
	presentByVidPid := make(map[string]bool)
	presentBySerial := make(map[string]bool)
	for _, dev := range usb.Devices {
		presentByVidPid[dev.VendorID+":"+dev.ProductID] = true
		if dev.Serial != "" {
			presentBySerial[dev.Serial] = true
		}
	}

	for i := range rules {
		r := &rules[i]
		if r.Metric != "usb_missing" || r.TargetName == "" {
			continue
		}
		if !matchesDeviceScope(r.IncludeDevices, r.ExcludeDevices, deviceID, hostname) {
			continue
		}

		// TargetName can be "vendor_id:product_id", a serial number, or a product description
		target := strings.TrimSpace(r.TargetName)
		found := false

		// Check by vendor:product ID (e.g., "1a6e:089a")
		if presentByVidPid[target] {
			found = true
		}
		// Check by serial
		if !found && presentBySerial[target] {
			found = true
		}
		// Check by description or product name (case-insensitive substring)
		if !found {
			for _, dev := range usb.Devices {
				if strings.EqualFold(dev.Description, target) ||
					strings.EqualFold(dev.Product, target) ||
					strings.EqualFold(dev.Serial, target) {
					found = true
					break
				}
			}
		}

		if found {
			continue // Device is present, no alert
		}

		key := fmt.Sprintf("%s:rule:%d:%s", deviceID, r.ID, target)
		cd := time.Duration(r.CooldownSeconds) * time.Second
		if g.onCooldown(key, cd) {
			continue
		}

		e := &models.Event{
			DeviceID:  deviceID,
			Type:      models.EventUSBDisconnected,
			Severity:  models.EventSeverity(r.Severity),
			Message:   fmt.Sprintf("USB device %s not found", target),
			CreatedAt: time.Now().UTC(),
		}
		g.createEventAndNotify(ctx, e, r, "", 1)
	}
}

// CheckDiskSmartAlerts fires alerts for SMART health failures and temperature thresholds.
func (g *Generator) CheckDiskSmartAlerts(ctx context.Context, deviceID, hostname string, drives []models.DiskDrive) {
	for _, d := range drives {
		if !d.SmartAvailable {
			continue
		}

		label := d.Name
		if d.Model != "" {
			label = fmt.Sprintf("%s (%s)", d.Name, d.Model)
		}

		// SMART health FAILED — always fire (hardcoded)
		if d.SmartHealth == "FAILED" {
			key := fmt.Sprintf("%s:smart:%s", deviceID, d.Name)
			if !g.onCooldown(key, 1*time.Hour) {
				g.createEvent(ctx, &models.Event{
					DeviceID:  deviceID,
					Type:      models.EventDiskSmartFailing,
					Severity:  models.SeverityCrit,
					Message:   fmt.Sprintf("Disk %s SMART health FAILED", label),
					CreatedAt: time.Now().UTC(),
				})
			}
		}

		// Temperature threshold — evaluate against disk_smart_temp rules
		if d.SmartTemp != nil {
			temp := float64(*d.SmartTemp)
			g.evaluateMetric(ctx, deviceID, "disk_smart_temp", temp, hostname,
				models.EventDiskSmartTemp,
				func(val float64) string {
					return fmt.Sprintf("Disk %s temperature at %.0f°C", label, val)
				})
		}
	}
}

// CheckGPUAlerts evaluates GPU metrics from telemetry against alert rules.
// Each GPU is evaluated independently so the event message identifies which GPU
// triggered the alert (FR-019). The GPUs slice is capped at 32 to prevent
// memory exhaustion from a compromised agent sending a pathological payload
// (SEC-001).
func (g *Generator) CheckGPUAlerts(ctx context.Context, deviceID, hostname string, gpuTel *models.GPUTelemetry) {
	if gpuTel == nil || len(gpuTel.GPUs) == 0 {
		return
	}

	// Cap iteration to a reasonable maximum (SEC-001 / SEC-003).
	const maxGPUs = 32
	gpus := gpuTel.GPUs
	if len(gpus) > maxGPUs {
		gpus = gpus[:maxGPUs]
	}

	for i := range gpus {
		gpu := &gpus[i]
		label := fmt.Sprintf("%s (index %d)", gpu.Name, gpu.Index)

		if gpu.TemperatureC != nil {
			temp := float64(*gpu.TemperatureC)
			g.evaluateMetric(ctx, deviceID, "gpu_temp", temp, hostname, models.EventGPUTemp,
				func(val float64) string {
					return fmt.Sprintf("GPU %s temperature at %.0f°C", label, val)
				})
		}

		if gpu.UtilizationPct != nil {
			util := float64(*gpu.UtilizationPct)
			g.evaluateMetric(ctx, deviceID, "gpu_util_percent", util, hostname, models.EventGPUMetric,
				func(val float64) string {
					return fmt.Sprintf("GPU %s utilization at %.0f%%", label, val)
				})
		}

		if gpu.MemUtilPct != nil {
			memUtil := float64(*gpu.MemUtilPct)
			g.evaluateMetric(ctx, deviceID, "gpu_mem_percent", memUtil, hostname, models.EventGPUMetric,
				func(val float64) string {
					return fmt.Sprintf("GPU %s memory utilization at %.0f%%", label, val)
				})
		}

		if gpu.PowerDrawW != nil {
			power := *gpu.PowerDrawW
			g.evaluateMetric(ctx, deviceID, "gpu_power_watts", power, hostname, models.EventGPUMetric,
				func(val float64) string {
					return fmt.Sprintf("GPU %s power draw at %.1fW", label, val)
				})
		}
	}
}

// evaluateMetric checks a numeric metric against all matching alert rules.
func (g *Generator) evaluateMetric(ctx context.Context, deviceID, metric string, value float64, hostname string, eventType models.EventType, msgFn func(float64) string) {
	rule := g.findMatchingRule(ctx, metric, deviceID, hostname, value)
	if rule != nil {
		key := fmt.Sprintf("%s:rule:%d", deviceID, rule.ID)
		cd := time.Duration(rule.CooldownSeconds) * time.Second
		if g.onCooldown(key, cd) {
			return
		}
		e := &models.Event{
			DeviceID:  deviceID,
			Type:      eventType,
			Severity:  models.EventSeverity(rule.Severity),
			Message:   msgFn(value),
			CreatedAt: time.Now().UTC(),
		}
		g.createEventAndNotify(ctx, e, rule, hostname, value)
		return
	}

	// If user-configured rules exist for this metric, respect them — don't
	// fall through to hardcoded fallback. This prevents alerts firing for
	// devices that are explicitly excluded from a rule.
	if g.hasRulesForMetric(ctx, metric) {
		return
	}

	// No rules configured at all — use hardcoded fallback thresholds
	var fallbackThreshold float64
	var fallbackCooldown time.Duration
	var fallbackSeverity models.EventSeverity
	switch metric {
	case "mem_percent":
		fallbackThreshold = 90
		fallbackCooldown = 1 * time.Hour
		fallbackSeverity = models.SeverityWarning
	case "disk_percent":
		fallbackThreshold = 90
		fallbackCooldown = 1 * time.Hour
		fallbackSeverity = models.SeverityCrit
	case "updates":
		fallbackThreshold = 0
		fallbackCooldown = 24 * time.Hour
		fallbackSeverity = models.SeverityInfo
	default:
		return
	}

	if !compareValue(value, ">", fallbackThreshold) {
		return
	}
	key := deviceID + ":" + string(eventType)
	if g.onCooldown(key, fallbackCooldown) {
		return
	}
	g.createEvent(ctx, &models.Event{
		DeviceID:  deviceID,
		Type:      eventType,
		Severity:  fallbackSeverity,
		Message:   msgFn(value),
		CreatedAt: time.Now().UTC(),
	})
}

// findMatchingRule returns the first enabled rule that matches the metric, device, and threshold.
func (g *Generator) findMatchingRule(ctx context.Context, metric, deviceID, hostname string, value float64) *models.AlertRule {
	return g.findMatchingRuleForContainer(ctx, metric, deviceID, hostname, "", value)
}

// findMatchingRuleForContainer is like findMatchingRule but also filters rules
// by container name (via include_containers / exclude_containers). Pass an empty
// containerName for non-container metrics.
func (g *Generator) findMatchingRuleForContainer(ctx context.Context, metric, deviceID, hostname, containerName string, value float64) *models.AlertRule {
	rules, err := g.listEnabledRules(ctx)
	if err != nil {
		slog.Error("find matching rule", "error", err.Error())
		return nil
	}
	for i := range rules {
		r := &rules[i]
		if r.Metric != metric {
			continue
		}
		if !matchesDeviceScope(r.IncludeDevices, r.ExcludeDevices, deviceID, hostname) {
			continue
		}
		if !MatchesContainerScope(r.IncludeContainers, r.ExcludeContainers, containerName) {
			continue
		}
		if !compareValue(value, r.Operator, r.Threshold) {
			continue
		}
		return r
	}
	return nil
}

// hasRulesForMetric returns true if any enabled rules exist for the given metric.
func (g *Generator) hasRulesForMetric(ctx context.Context, metric string) bool {
	rules, err := g.listEnabledRules(ctx)
	if err != nil {
		return false
	}
	for i := range rules {
		if rules[i].Metric == metric {
			return true
		}
	}
	return false
}

// matchesTargetState checks if an actual state matches a comma-separated list
// of target states (case-insensitive). Empty target matches any state.
func matchesTargetState(targetState, actualState string) bool {
	if targetState == "" {
		return true
	}
	for _, s := range strings.Split(targetState, ",") {
		if strings.EqualFold(strings.TrimSpace(s), actualState) {
			return true
		}
	}
	return false
}

// matchesDeviceScope checks if a device matches the rule's include/exclude lists.
func matchesDeviceScope(include, exclude, deviceID, hostname string) bool {
	return MatchesDeviceScope(include, exclude, deviceID, hostname, nil)
}

// MatchesContainerScope checks if a container name matches include/exclude
// lists on a rule. Exclude always wins; empty include matches all.
// Callers pass an empty containerName to short-circuit to true (non-container rules).
func MatchesContainerScope(include, exclude, containerName string) bool {
	if containerName == "" {
		return true
	}
	// Container names sometimes come with a leading '/' from Docker — normalize.
	name := strings.TrimPrefix(containerName, "/")
	if containerInList(exclude, name) {
		return false
	}
	if include == "" {
		return true
	}
	return containerInList(include, name)
}

// containerInList reports whether name appears (case-insensitive) in a
// comma-separated list, ignoring leading slashes on either side.
func containerInList(list, name string) bool {
	if list == "" {
		return false
	}
	for _, s := range strings.Split(list, ",") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "/"))
		if s != "" && strings.EqualFold(s, name) {
			return true
		}
	}
	return false
}

// isContainerExcluded reports whether any enabled rule for the given metric —
// scoped to this device — explicitly lists the container in its ExcludeContainers.
// Used to fully suppress container_died / container_oom events (no stored event,
// no notification) when the user has opted out of alerts for a specific container.
func (g *Generator) isContainerExcluded(ctx context.Context, metric, deviceID, hostname, containerName string) bool {
	if containerName == "" {
		return false
	}
	name := strings.TrimPrefix(containerName, "/")
	rules, err := g.listEnabledRules(ctx)
	if err != nil {
		return false
	}
	for i := range rules {
		r := &rules[i]
		if r.Metric != metric {
			continue
		}
		if !matchesDeviceScope(r.IncludeDevices, r.ExcludeDevices, deviceID, hostname) {
			continue
		}
		if containerInList(r.ExcludeContainers, name) {
			return true
		}
	}
	return false
}

// MatchesDeviceScope checks if a device matches include/exclude hostname lists.
// Exclude always wins. Empty include matches all devices (minus excludes).
// Matches against device ID, hostname, and tags.
func MatchesDeviceScope(include, exclude, deviceID, hostname string, tags []string) bool {
	// Check exclude first — exclude always wins
	if exclude != "" {
		for _, e := range strings.Split(exclude, ",") {
			e = strings.TrimSpace(e)
			if e == "" {
				continue
			}
			if strings.EqualFold(e, hostname) || e == deviceID {
				return false
			}
			for _, tag := range tags {
				if e == tag {
					return false
				}
			}
		}
	}

	// Empty include means "all devices" (minus excludes above)
	if include == "" {
		return true
	}

	// Check include list
	for _, f := range strings.Split(include, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if strings.EqualFold(f, hostname) || f == deviceID {
			return true
		}
		for _, tag := range tags {
			if f == tag {
				return true
			}
		}
	}
	return false
}

// compareValue evaluates: value <operator> threshold.
func compareValue(value float64, operator string, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case "<":
		return value < threshold
	case ">=":
		return value >= threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	case "!=":
		return value != threshold
	default:
		return false
	}
}

