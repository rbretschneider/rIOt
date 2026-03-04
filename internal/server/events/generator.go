package events

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
)

// Generator creates and stores events based on telemetry data.
type Generator struct {
	repo *db.EventRepo
	hub  *websocket.Hub
}

func NewGenerator(repo *db.EventRepo, hub *websocket.Hub) *Generator {
	return &Generator{repo: repo, hub: hub}
}

func (g *Generator) createEvent(ctx context.Context, e *models.Event) {
	if err := g.repo.Create(ctx, e); err != nil {
		slog.Error("create event", "error", err)
		return
	}
	g.hub.BroadcastEvent(e)
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
	g.createEvent(ctx, &models.Event{
		DeviceID:  deviceID,
		Type:      models.EventDeviceOffline,
		Severity:  models.SeverityWarning,
		Message:   fmt.Sprintf("Device %s went offline", hostname),
		CreatedAt: time.Now().UTC(),
	})
}

func (g *Generator) CheckHeartbeatThresholds(ctx context.Context, deviceID string, data *models.HeartbeatData) {
	if data.MemPercent > 90 {
		g.createEvent(ctx, &models.Event{
			DeviceID:  deviceID,
			Type:      models.EventMemHigh,
			Severity:  models.SeverityWarning,
			Message:   fmt.Sprintf("RAM usage at %.1f%%", data.MemPercent),
			CreatedAt: time.Now().UTC(),
		})
	}
	if data.DiskRootPercent > 90 {
		g.createEvent(ctx, &models.Event{
			DeviceID:  deviceID,
			Type:      models.EventDiskHigh,
			Severity:  models.SeverityCrit,
			Message:   fmt.Sprintf("Root disk usage at %.1f%%", data.DiskRootPercent),
			CreatedAt: time.Now().UTC(),
		})
	}
}

func (g *Generator) CheckTelemetryThresholds(ctx context.Context, deviceID string, data *models.FullTelemetryData) {
	if data.Memory != nil && data.Memory.UsagePercent > 90 {
		g.createEvent(ctx, &models.Event{
			DeviceID:  deviceID,
			Type:      models.EventMemHigh,
			Severity:  models.SeverityWarning,
			Message:   fmt.Sprintf("RAM usage at %.1f%%", data.Memory.UsagePercent),
			CreatedAt: time.Now().UTC(),
		})
	}
	if data.Disks != nil {
		for _, fs := range data.Disks.Filesystems {
			if fs.UsagePercent > 90 {
				g.createEvent(ctx, &models.Event{
					DeviceID:  deviceID,
					Type:      models.EventDiskHigh,
					Severity:  models.SeverityCrit,
					Message:   fmt.Sprintf("Disk %s usage at %.1f%%", fs.MountPoint, fs.UsagePercent),
					CreatedAt: time.Now().UTC(),
				})
			}
		}
	}
	if data.Updates != nil && data.Updates.PendingUpdates > 0 {
		g.createEvent(ctx, &models.Event{
			DeviceID:  deviceID,
			Type:      models.EventUpdateAvail,
			Severity:  models.SeverityInfo,
			Message:   fmt.Sprintf("%d updates available", data.Updates.PendingUpdates),
			CreatedAt: time.Now().UTC(),
		})
	}
}

// CheckDockerEvent creates an event from a Docker container state change.
func (g *Generator) CheckDockerEvent(ctx context.Context, deviceID string, evt *models.DockerEvent) {
	now := time.Now().UTC()
	switch evt.Action {
	case "start":
		g.createEvent(ctx, &models.Event{
			DeviceID:  deviceID,
			Type:      models.EventContainerStart,
			Severity:  models.SeverityInfo,
			Message:   fmt.Sprintf("Container %s started (%s)", evt.ContainerName, evt.Image),
			CreatedAt: now,
		})
	case "stop":
		g.createEvent(ctx, &models.Event{
			DeviceID:  deviceID,
			Type:      models.EventContainerStop,
			Severity:  models.SeverityInfo,
			Message:   fmt.Sprintf("Container %s stopped", evt.ContainerName),
			CreatedAt: now,
		})
	case "die":
		g.createEvent(ctx, &models.Event{
			DeviceID:  deviceID,
			Type:      models.EventContainerDied,
			Severity:  models.SeverityWarning,
			Message:   fmt.Sprintf("Container %s died", evt.ContainerName),
			CreatedAt: now,
		})
	case "oom":
		g.createEvent(ctx, &models.Event{
			DeviceID:  deviceID,
			Type:      models.EventContainerOOM,
			Severity:  models.SeverityCrit,
			Message:   fmt.Sprintf("Container %s OOM killed", evt.ContainerName),
			CreatedAt: now,
		})
	}
}
