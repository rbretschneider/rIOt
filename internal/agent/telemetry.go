package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/shirou/gopsutil/v3/host"
)

// bootGracePeriod is the duration after system boot during which log errors
// are collected and displayed but not counted toward the heartbeat error metric.
// This prevents harmless boot-time noise (e.g. ACPI BIOS errors) from firing alerts.
const bootGracePeriod = 10 * time.Minute

// maxTelemetryBodyBytes is the agent-side body guard (GROWTH-001 D-36). The
// server accepts 25 MiB; staying near a quarter of that keeps a runaway
// collector from ever producing a body the server must reject.
const maxTelemetryBodyBytes = 6 << 20

func (a *Agent) sendTelemetry(ctx context.Context) {
	data := a.collectAll(ctx)

	snap := &models.TelemetrySnapshot{
		DeviceID:  a.config.Agent.DeviceID,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}
	guardTelemetryBody(snap, maxTelemetryBodyBytes)

	err := a.client.SendTelemetry(ctx, a.config.Agent.DeviceID, snap)
	outcome, status := classifyPush(err)
	switch outcome {
	case pushRetry:
		// 401, 403, 408, 429, 5xx, a transport error or a back-off: keep it.
		if errors.Is(err, ErrBackoff) {
			slog.Debug("telemetry push held back, buffering", "error", err)
		} else {
			slog.Warn("telemetry push failed, buffering", "error", err)
		}
		a.bufferSnapshot(snap)
		return
	case pushRejected:
		// A permanent 4xx will never succeed: buffering it would only make the
		// next flush fail the same way (GROWTH-001 FR-068).
		slog.Warn("server rejected telemetry, dropping it", "status", status, "error", err)
	}

	// The server answered, so deliver what was buffered while it was away.
	if a.buffer != nil {
		a.flushBuffer(ctx)
	}
}

func (a *Agent) bufferSnapshot(snap *models.TelemetrySnapshot) {
	if a.buffer == nil {
		return
	}
	if err := a.buffer.Store(snap); err != nil {
		slog.Warn("could not buffer telemetry, dropping it", "error", err)
	}
}

// pushOutcome is what the agent does with a telemetry item after one attempt.
type pushOutcome int

const (
	// pushAcked: the server accepted it (2xx).
	pushAcked pushOutcome = iota
	// pushRejected: a permanent 4xx; the item can never succeed and is dropped.
	pushRejected
	// pushRetry: keep the item and back off.
	pushRetry
)

// classifyPush maps a SendTelemetry result to an outcome (GROWTH-001 AD-017).
// Every 4xx is permanent except 401 and 403 (a database blip on the server can
// answer 401, and a revoked-then-restored key must not lose data), 408 (a
// transient timeout) and 429 (rate limited). 5xx and transport errors retry.
func classifyPush(err error) (outcome pushOutcome, status int) {
	if err == nil {
		return pushAcked, http.StatusOK
	}
	var se *StatusError
	if !errors.As(err, &se) {
		return pushRetry, 0 // transport error or ErrBackoff
	}
	switch {
	case se.StatusCode == http.StatusUnauthorized,
		se.StatusCode == http.StatusForbidden,
		se.StatusCode == http.StatusRequestTimeout,
		se.StatusCode == http.StatusTooManyRequests,
		se.StatusCode >= 500:
		return pushRetry, se.StatusCode
	case se.StatusCode >= 400:
		return pushRejected, se.StatusCode
	}
	return pushRetry, se.StatusCode
}

// guardTelemetryBody keeps the encoded snapshot within limit bytes by dropping
// the elastic sections: container logs first, then the journal.
func guardTelemetryBody(snap *models.TelemetrySnapshot, limit int) {
	size := encodedSize(snap)
	if size <= limit {
		return
	}
	if n := len(snap.Data.ContainerLogs); n > 0 {
		// Preserve loss visibility when the entire elastic section is dropped.
		gaps := make(map[string]*models.ContainerLogGap)
		for _, entry := range snap.Data.ContainerLogs {
			gap := gaps[entry.ContainerID]
			if gap == nil {
				gap = &models.ContainerLogGap{ContainerID: entry.ContainerID, ContainerName: entry.ContainerName, Reason: "agent_cap", Timestamp: entry.Timestamp}
				gaps[entry.ContainerID] = gap
			}
			gap.Count++
			if entry.Timestamp.After(gap.Timestamp) {
				gap.Timestamp = entry.Timestamp
			}
		}
		for _, gap := range gaps {
			snap.Data.ContainerLogGaps = append(snap.Data.ContainerLogGaps, *gap)
		}
		snap.Data.ContainerLogs = nil
		slog.Warn("telemetry body over the guard, dropped container logs",
			"dropped_lines", n, "was_bytes", size, "limit_bytes", limit)
		if size = encodedSize(snap); size <= limit {
			return
		}
	}
	if n := len(snap.Data.Logs); n > 0 {
		snap.Data.Logs = nil
		slog.Warn("telemetry body over the guard, dropped journal entries",
			"dropped_entries", n, "was_bytes", size, "limit_bytes", limit)
		if size = encodedSize(snap); size <= limit {
			return
		}
	}
	slog.Error("telemetry body still over the guard after dropping logs", "bytes", size, "limit_bytes", limit)
}

func encodedSize(snap *models.TelemetrySnapshot) int {
	b, err := json.Marshal(snap)
	if err != nil {
		return 0
	}
	return len(b)
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
			if v != nil {
				allow := a.config.Commands.AllowReboot
				v.AllowReboot = &allow
				allowPatching := a.config.Commands.AllowPatching
				v.AllowPatching = &allowPatching
				holdRebootClass := a.config.Commands.HoldRebootClass
				v.HoldRebootClass = &holdRebootClass
				// This agent understands os_update's reboot_if_required (PATCH-FLOW-001 AD-004).
				v.SupportsRebootIfRequired = true
			}
		case []models.ServiceInfo:
			data.Services = v
		case *models.ProcessInfo:
			data.Procs = v
		case *models.DockerInfo:
			data.Docker = v
			a.observeStackRegistry(ctx, v)
		case *models.SecurityInfo:
			data.Security = v
		case *models.UPSInfo:
			data.UPS = v
		case *models.WebServerInfo:
			data.WebServers = v
		case *models.USBInfo:
			data.USB = v
		case *models.HardwareInfo:
			data.Hardware = v
		case *models.CronInfo:
			data.CronJobs = v
		case *models.GPUTelemetry:
			data.GPUTelemetry = v
		case collectors.ContainerLogBatch:
			data.ContainerLogs = v.Entries
			data.ContainerLogGaps = v.Gaps
		case *models.DockerInventory:
			data.DockerInventory = v
		case []models.ContainerLogEntry:
			data.ContainerLogs = v
		case []models.LogEntry:
			data.Logs = v
			// Count priority<=3 entries (error and above) for heartbeat metric,
			// but skip entries that fall within the boot grace period to avoid
			// alerting on harmless boot-time noise (ACPI errors, etc.).
			var bootTime time.Time
			if uptime, err := host.UptimeWithContext(ctx); err == nil {
				bootTime = time.Now().Add(-time.Duration(uptime) * time.Second)
			}
			var errCount int64
			for _, e := range v {
				if e.Priority <= 3 {
					if !bootTime.IsZero() && e.Timestamp.Before(bootTime.Add(bootGracePeriod)) {
						continue
					}
					errCount++
				}
			}
			a.logErrors.Add(errCount)
		}
	}

	if data.Docker != nil {
		data.Docker.InventoryEnabled = false
		for _, c := range a.registry.Collectors() {
			if c.Name() == "docker_inventory" {
				data.Docker.InventoryEnabled = true
			}
			if c.Name() == "container_logs" {
				enabled := true
				data.Docker.ContainerLogsEnabled = &enabled
			}
		}
	}
	return data
}

// flushBuffer sends the buffered snapshots oldest first, one at a time. Each
// item leaves the buffer as soon as the server has decided its fate: a 2xx
// removes it (it is never sent again, FR-067) and a permanent 4xx removes it
// with a Warn (FR-068). Anything else keeps it and stops the flush.
func (a *Agent) flushBuffer(ctx context.Context) {
	delivered, dropped := 0, 0
	// The buffer holds at most maxBuffered items and nothing else adds to it
	// during a flush, so this bound is never the reason the loop ends.
	for i := 0; i <= maxBuffered; i++ {
		item, err := a.buffer.Oldest()
		if err != nil {
			slog.Warn("could not read the offline buffer", "error", err)
			break
		}
		if item == nil {
			break
		}
		deviceID := item.Snapshot.DeviceID
		if deviceID == "" {
			deviceID = a.config.Agent.DeviceID
		}

		err = a.client.SendTelemetry(ctx, deviceID, item.Snapshot)
		outcome, status := classifyPush(err)
		if outcome == pushRetry {
			if errors.Is(err, ErrBackoff) {
				slog.Debug("buffer flush held back", "error", err)
			} else {
				slog.Warn("buffer flush failed, will retry later", "error", err)
			}
			break
		}
		if outcome == pushRejected {
			slog.Warn("server rejected a buffered item, dropping it", "status", status, "error", err)
			dropped++
		} else {
			delivered++
		}
		if err := a.buffer.Delete(item.ID); err != nil {
			slog.Error("could not remove a handled item from the offline buffer", "error", err)
			break
		}
	}
	if delivered+dropped > 0 {
		a.buffer.Compact()
		slog.Info("buffer flushed", "delivered", delivered, "dropped", dropped, "remaining", a.buffer.Len())
	}
}
