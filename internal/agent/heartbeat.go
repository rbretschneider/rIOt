package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
)

func (a *Agent) sendHeartbeat(ctx context.Context) {
	data := models.HeartbeatData{
		AgentVersion: a.version,
	}

	// CPU usage
	if percents, err := cpu.PercentWithContext(ctx, time.Second, false); err == nil && len(percents) > 0 {
		data.CPUPercent = percents[0]
	}

	// Memory usage
	if v, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		data.MemPercent = v.UsedPercent
	}

	// Uptime
	if uptime, err := host.UptimeWithContext(ctx); err == nil {
		data.Uptime = uptime
	}

	// Load average
	if l, err := load.AvgWithContext(ctx); err == nil {
		data.LoadAvg1m = l.Load1
	}

	// Root disk usage
	if usage, err := disk.UsageWithContext(ctx, "/"); err == nil {
		data.DiskRootPercent = usage.UsedPercent
	}

	hb := &models.Heartbeat{
		DeviceID:  a.config.Agent.DeviceID,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}

	if err := a.client.SendHeartbeat(ctx, a.config.Agent.DeviceID, hb); err != nil {
		slog.Warn("heartbeat failed", "error", err)
	}
}
