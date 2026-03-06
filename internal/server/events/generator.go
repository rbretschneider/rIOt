package events

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

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

	mu       sync.Mutex
	lastSent map[string]time.Time // key: "deviceID:ruleID" or "deviceID:eventType"
}

func NewGenerator(repo db.EventRepository, hub *websocket.Hub, alertRuleRepo db.AlertRuleRepository, dispatcher Dispatcher, commandRepo db.CommandRepository) *Generator {
	return &Generator{
		repo:          repo,
		hub:           hub,
		alertRuleRepo: alertRuleRepo,
		dispatcher:    dispatcher,
		commandRepo:   commandRepo,
		lastSent:      make(map[string]time.Time),
	}
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
		slog.Error("create event", "error", err)
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
var disruptiveActions = map[string]string{
	"reboot":       "reboot",
	"os_update":    "OS update",
	"agent_update": "agent update",
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

// CommandSent creates an informational event when a disruptive command is dispatched.
func (g *Generator) CommandSent(ctx context.Context, deviceID, hostname, action string) {
	label, ok := disruptiveActions[action]
	if !ok {
		return
	}
	g.createEvent(ctx, &models.Event{
		DeviceID:  deviceID,
		Type:      models.EventCommandSent,
		Severity:  models.SeverityInfo,
		Message:   fmt.Sprintf("%s initiated on %s", strings.Title(label), hostname),
		CreatedAt: time.Now().UTC(),
	})
}

// CommandCompleted creates an event when a disruptive command finishes (success or failure).
func (g *Generator) CommandCompleted(ctx context.Context, deviceID, hostname, action, status, message string) {
	label, ok := disruptiveActions[action]
	if !ok {
		return
	}

	severity := models.SeverityInfo
	msg := fmt.Sprintf("%s completed on %s", strings.Title(label), hostname)
	if status == "error" {
		severity = models.SeverityWarning
		// Include a brief reason — truncate long output
		reason := message
		if len(reason) > 200 {
			reason = reason[:200] + "..."
		}
		msg = fmt.Sprintf("%s failed on %s: %s", strings.Title(label), hostname, reason)
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
	rule := g.findMatchingRule(ctx, "device_offline", deviceID, 1)
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

func (g *Generator) CheckHeartbeatThresholds(ctx context.Context, deviceID string, data *models.HeartbeatData) {
	g.evaluateMetric(ctx, deviceID, "mem_percent", data.MemPercent, "", models.EventMemHigh,
		func(val float64) string { return fmt.Sprintf("RAM usage at %.1f%%", val) })

	g.evaluateMetric(ctx, deviceID, "disk_percent", data.DiskRootPercent, "", models.EventDiskHigh,
		func(val float64) string { return fmt.Sprintf("Root disk usage at %.1f%%", val) })
}

func (g *Generator) CheckTelemetryThresholds(ctx context.Context, deviceID string, data *models.FullTelemetryData) {
	if data.Memory != nil {
		g.evaluateMetric(ctx, deviceID, "mem_percent", data.Memory.UsagePercent, "", models.EventMemHigh,
			func(val float64) string { return fmt.Sprintf("RAM usage at %.1f%%", val) })
	}
	if data.Disks != nil {
		for _, fs := range data.Disks.Filesystems {
			g.evaluateMetric(ctx, deviceID, "disk_percent", fs.UsagePercent, "", models.EventDiskHigh,
				func(val float64) string { return fmt.Sprintf("Disk %s usage at %.1f%%", fs.MountPoint, val) })
		}
	}
	// Pending updates are shown as dashboard status, not as alert events.

	// Check service, NIC, and process alerts
	if data.Services != nil {
		g.CheckServiceAlerts(ctx, deviceID, data.Services)
	}
	if data.Network != nil && data.Network.Interfaces != nil {
		g.CheckNICAlerts(ctx, deviceID, data.Network.Interfaces)
	}
	if data.Procs != nil {
		g.CheckProcessAlerts(ctx, deviceID, data.Procs)
	}
}

// CheckServiceAlerts checks service state against service_state alert rules.
func (g *Generator) CheckServiceAlerts(ctx context.Context, deviceID string, services []models.ServiceInfo) {
	rules, err := g.alertRuleRepo.ListEnabled(ctx)
	if err != nil {
		slog.Error("check service alerts", "error", err)
		return
	}

	for i := range rules {
		r := &rules[i]
		if r.Metric != "service_state" || r.TargetName == "" {
			continue
		}
		if !matchesDeviceFilter(r.DeviceFilter, deviceID) {
			continue
		}

		for _, svc := range services {
			if !strings.EqualFold(svc.Name, r.TargetName) {
				continue
			}
			// Check if service state matches the target state
			if r.TargetState != "" && !strings.EqualFold(svc.State, r.TargetState) {
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
func (g *Generator) CheckNICAlerts(ctx context.Context, deviceID string, interfaces []models.NetworkInterface) {
	rules, err := g.alertRuleRepo.ListEnabled(ctx)
	if err != nil {
		slog.Error("check nic alerts", "error", err)
		return
	}

	for i := range rules {
		r := &rules[i]
		if r.Metric != "nic_state" || r.TargetName == "" {
			continue
		}
		if !matchesDeviceFilter(r.DeviceFilter, deviceID) {
			continue
		}

		for _, iface := range interfaces {
			if !strings.EqualFold(iface.Name, r.TargetName) {
				continue
			}
			// If target_state is set and not "any", only match that specific state
			if r.TargetState != "" && !strings.EqualFold(r.TargetState, "any") {
				if !strings.EqualFold(iface.State, r.TargetState) {
					continue
				}
			} else {
				// "any" or empty = any non-UP state (existing behavior)
				if iface.State == "UP" {
					continue
				}
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
func (g *Generator) CheckProcessAlerts(ctx context.Context, deviceID string, procs *models.ProcessInfo) {
	rules, err := g.alertRuleRepo.ListEnabled(ctx)
	if err != nil {
		slog.Error("check process alerts", "error", err)
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
		if !matchesDeviceFilter(r.DeviceFilter, deviceID) {
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

// CheckDockerEvent creates an event from a Docker container state change.
func (g *Generator) CheckDockerEvent(ctx context.Context, deviceID string, evt *models.DockerEvent) {
	now := time.Now().UTC()
	switch evt.Action {
	case "start":
		g.createEvent(ctx, &models.Event{
			DeviceID: deviceID, Type: models.EventContainerStart, Severity: models.SeverityInfo,
			Message: fmt.Sprintf("Container %s started (%s)", evt.ContainerName, evt.Image), CreatedAt: now,
		})
	case "stop":
		g.createEvent(ctx, &models.Event{
			DeviceID: deviceID, Type: models.EventContainerStop, Severity: models.SeverityInfo,
			Message: fmt.Sprintf("Container %s stopped", evt.ContainerName), CreatedAt: now,
		})
	case "die":
		e := &models.Event{
			DeviceID: deviceID, Type: models.EventContainerDied, Severity: models.SeverityWarning,
			Message: fmt.Sprintf("Container %s died", evt.ContainerName), CreatedAt: now,
		}
		rule := g.findMatchingRule(ctx, "container_died", deviceID, 1)
		if rule != nil {
			key := fmt.Sprintf("%s:rule:%d", deviceID, rule.ID)
			if !g.onCooldown(key, time.Duration(rule.CooldownSeconds)*time.Second) {
				e.Severity = models.EventSeverity(rule.Severity)
				g.createEventAndNotify(ctx, e, rule, "", 1)
			}
		} else {
			g.createEvent(ctx, e)
		}
	case "oom":
		e := &models.Event{
			DeviceID: deviceID, Type: models.EventContainerOOM, Severity: models.SeverityCrit,
			Message: fmt.Sprintf("Container %s OOM killed", evt.ContainerName), CreatedAt: now,
		}
		rule := g.findMatchingRule(ctx, "container_oom", deviceID, 1)
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
}

// evaluateMetric checks a numeric metric against all matching alert rules.
func (g *Generator) evaluateMetric(ctx context.Context, deviceID, metric string, value float64, hostname string, eventType models.EventType, msgFn func(float64) string) {
	rule := g.findMatchingRule(ctx, metric, deviceID, value)
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

	// No matching rule — use hardcoded fallback thresholds
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
func (g *Generator) findMatchingRule(ctx context.Context, metric, deviceID string, value float64) *models.AlertRule {
	rules, err := g.alertRuleRepo.ListEnabled(ctx)
	if err != nil {
		slog.Error("find matching rule", "error", err)
		return nil
	}
	for i := range rules {
		r := &rules[i]
		if r.Metric != metric {
			continue
		}
		if !matchesDeviceFilter(r.DeviceFilter, deviceID) {
			continue
		}
		if !compareValue(value, r.Operator, r.Threshold) {
			continue
		}
		return r
	}
	return nil
}

// matchesDeviceFilter checks if a device matches the rule's filter.
// Empty filter matches all devices.
func matchesDeviceFilter(filter, deviceID string) bool {
	if filter == "" {
		return true
	}
	for _, f := range strings.Split(filter, ",") {
		if strings.TrimSpace(f) == deviceID {
			return true
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

