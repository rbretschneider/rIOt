package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func (a *Agent) sendTelemetry(ctx context.Context) {
	data := a.collectAll(ctx)

	snap := &models.TelemetrySnapshot{
		DeviceID:  a.config.Agent.DeviceID,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}

	if err := a.client.SendTelemetry(ctx, a.config.Agent.DeviceID, snap); err != nil {
		slog.Warn("telemetry push failed, buffering", "error", err)
		if a.buffer != nil {
			a.buffer.Store(snap)
		}
		return
	}

	// Flush buffer on successful connection
	if a.buffer != nil {
		a.flushBuffer(ctx)
	}
}

func (a *Agent) collectAll(ctx context.Context) models.FullTelemetryData {
	data := models.FullTelemetryData{}

	for _, c := range a.registry.Collectors() {
		result, err := c.Collect(ctx)
		if err != nil {
			slog.Warn("collector failed", "collector", c.Name(), "error", err)
			continue
		}

		switch v := result.(type) {
		case *models.SystemInfo:
			data.System = v
		case *models.OSInfo:
			data.OS = v
		case *models.CPUInfo:
			data.CPU = v
		case *models.MemoryInfo:
			data.Memory = v
		case *models.DiskInfo:
			data.Disks = v
		case *models.NetworkInfo:
			data.Network = v
		case *models.UpdateInfo:
			data.Updates = v
		case []models.ServiceInfo:
			data.Services = v
		case *models.ProcessInfo:
			data.Procs = v
		case *models.DockerInfo:
			data.Docker = v
		case *models.SecurityInfo:
			data.Security = v
		}
	}

	return data
}

func (a *Agent) flushBuffer(ctx context.Context) {
	items, err := a.buffer.GetAll()
	if err != nil || len(items) == 0 {
		return
	}

	slog.Info("flushing buffered telemetry", "count", len(items))
	for _, snap := range items {
		if err := a.client.SendTelemetry(ctx, snap.DeviceID, snap); err != nil {
			slog.Warn("buffer flush failed, will retry later", "error", err)
			return
		}
	}
	a.buffer.Clear()
	slog.Info("buffer flushed successfully")
}
