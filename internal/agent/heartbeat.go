package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	psnet "github.com/shirou/gopsutil/v3/net"
)

// isLoopbackName reports whether a network interface name identifies a loopback
// interface across Linux (lo), macOS/BSD (lo0), and Windows
// ("Loopback Pseudo-Interface N"). Name-based detection avoids a second syscall
// per heartbeat to query interface flags.
func isLoopbackName(name string) bool {
	return name == "lo" || name == "lo0" || strings.HasPrefix(name, "Loopback")
}

// computeNetRates returns (rx, tx) bytes/sec given previous and current
// per-interface snapshots and elapsed seconds.
//
// Returns (0, 0) when prev is nil, elapsed <= 0, or no deltas are available.
// Per-interface counter rollover (cur < prev) contributes 0 from that interface.
// New interfaces (present in curr but not prev) contribute 0.
// Removed interfaces (present in prev but not curr) contribute 0 implicitly.
func computeNetRates(prev, curr map[string]netIOSnapshot, elapsed float64) (rx float64, tx float64) {
	if prev == nil || elapsed <= 0 {
		return 0, 0
	}
	var totalRxDelta, totalTxDelta uint64
	for name, cur := range curr {
		p, ok := prev[name]
		if !ok {
			// Interface added mid-run — no prior baseline; contributes 0.
			continue
		}
		if cur.BytesRecv >= p.BytesRecv {
			totalRxDelta += cur.BytesRecv - p.BytesRecv
		}
		// Else: per-interface rollover — contribute 0 for this interval.
		if cur.BytesSent >= p.BytesSent {
			totalTxDelta += cur.BytesSent - p.BytesSent
		}
		// Else: per-interface rollover — contribute 0 for this interval.
	}
	return float64(totalRxDelta) / elapsed, float64(totalTxDelta) / elapsed
}

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

	// Max disk usage across all physical, non-network filesystems
	if partitions, err := disk.PartitionsWithContext(ctx, false); err == nil {
		netFS := map[string]bool{"nfs": true, "nfs4": true, "cifs": true, "smb": true, "sshfs": true, "fuse.sshfs": true}
		for _, p := range partitions {
			if netFS[p.Fstype] {
				continue
			}
			if usage, err := disk.UsageWithContext(ctx, p.Mountpoint); err == nil && usage.Total > 0 {
				if usage.UsedPercent > data.DiskRootPercent {
					data.DiskRootPercent = usage.UsedPercent
				}
			}
		}
	}

	// Disk I/O throughput and utilization (delta between heartbeats)
	if counters, err := disk.IOCountersWithContext(ctx); err == nil {
		now := time.Now()
		curr := make(map[string]diskIOSnapshot, len(counters))
		for name, c := range counters {
			curr[name] = diskIOSnapshot{
				ReadBytes:  c.ReadBytes,
				WriteBytes: c.WriteBytes,
				IoTime:     c.IoTime,
			}
		}

		if a.prevDiskIO != nil && !a.prevDiskIOTime.IsZero() {
			elapsed := now.Sub(a.prevDiskIOTime).Seconds()
			if elapsed > 0 {
				var totalReadDelta, totalWriteDelta uint64
				var totalIOTimeDelta uint64
				var deviceCount int
				for name, cur := range curr {
					if prev, ok := a.prevDiskIO[name]; ok {
						if cur.ReadBytes >= prev.ReadBytes {
							totalReadDelta += cur.ReadBytes - prev.ReadBytes
						}
						if cur.WriteBytes >= prev.WriteBytes {
							totalWriteDelta += cur.WriteBytes - prev.WriteBytes
						}
						if cur.IoTime >= prev.IoTime {
							totalIOTimeDelta += cur.IoTime - prev.IoTime
						}
						deviceCount++
					}
				}
				data.DiskReadBytesPerSec = float64(totalReadDelta) / elapsed
				data.DiskWriteBytesPerSec = float64(totalWriteDelta) / elapsed
				if deviceCount > 0 {
					// IoTime is ms spent doing I/O; normalize to percentage of wall time
					data.DiskIOPercent = float64(totalIOTimeDelta) / (elapsed * 1000) * 100 / float64(deviceCount)
					if data.DiskIOPercent > 100 {
						data.DiskIOPercent = 100
					}
				}
			}
		}

		a.prevDiskIO = curr
		a.prevDiskIOTime = now
	}

	// Network I/O throughput (delta between heartbeats, sum across non-loopback interfaces)
	if ifaces, err := psnet.IOCountersWithContext(ctx, true); err == nil {
		now := time.Now()
		curr := make(map[string]netIOSnapshot, len(ifaces))
		for _, c := range ifaces {
			if isLoopbackName(c.Name) {
				continue
			}
			curr[c.Name] = netIOSnapshot{
				BytesRecv: c.BytesRecv,
				BytesSent: c.BytesSent,
			}
		}

		if a.prevNetCounters != nil && !a.prevNetCountersTime.IsZero() {
			elapsed := now.Sub(a.prevNetCountersTime).Seconds()
			data.NetRxBytesPerSec, data.NetTxBytesPerSec = computeNetRates(a.prevNetCounters, curr, elapsed)
		}

		a.prevNetCounters = curr
		a.prevNetCountersTime = now
	}

	// Log errors since last heartbeat
	data.LogErrors = int(a.logErrors.Swap(0))

	hb := &models.Heartbeat{
		DeviceID:  a.config.Agent.DeviceID,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}

	resp, err := a.client.SendHeartbeat(ctx, a.config.Agent.DeviceID, hb)
	if err != nil {
		slog.Warn("heartbeat failed", "error", err)
		return
	}

	// Process any pending commands delivered via heartbeat
	for _, payload := range resp.PendingCommands {
		slog.Info("heartbeat: received command", "id", payload.CommandID, "action", payload.Action)
		payloadJSON, _ := json.Marshal(payload)
		go a.handleCommand(ctx, AgentWSMessage{
			Type: "command",
			Data: payloadJSON,
		})
	}
}
