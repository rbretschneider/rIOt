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

	mu       sync.Mutex
	lastSent map[string]time.Time // key: "deviceID:ruleID" or "deviceID:eventType"
}

func NewGenerator(repo db.EventRepository, hub *websocket.Hub, alertRuleRepo db.AlertRuleRepository, dispatcher Dispatcher) *Generator {
	return &Generator{
		repo:          repo,
		hub:           hub,
		alertRuleRepo: alertRuleRepo,
		dispatcher:    dispatcher,
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

func (g *Generator) DeviceOnline(ctx context.Context, deviceID, hostname string) {
	g.createEvent(ctx, &models.Event{
		DeviceID:  deviceID,
		Type:      models.EventDeviceOnline,
		Severity:  models.SeverityInfo,
		Message:   fmt.Sprintf("Device %s came online", hostname),
		CreatedAt: time.Now().UTC(),
	})
}

func (g *Generator) DeviceOffline(ctx context.Context, deviceID, hostname string) {
	e := &models.Event{
		DeviceID:  deviceID,
		Type:      models.EventDeviceOffline,
		Severity:  models.SeverityWarning,
		Message:   fmt.Sprintf("Device %s went offline", hostname),
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
		e.Severity = models.EventSeverity(rule.Severity)
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
	if data.Updates != nil && data.Updates.PendingUpdates > 0 {
		g.evaluateMetric(ctx, deviceID, "updates", float64(data.Updates.PendingUpdates), "", models.EventUpdateAvail,
			func(val float64) string { return fmt.Sprintf("%d updates available", int(val)) })
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

