package probes

import (
	"context"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	probing "github.com/prometheus-community/pro-bing"
)

// executePing runs an ICMP ping probe.
// Config: target (hostname/IP), count (optional, default 3).
func executePing(ctx context.Context, probe models.Probe) *models.ProbeResult {
	target, _ := probe.Config["target"].(string)
	if target == "" {
		return &models.ProbeResult{
			ProbeID:  probe.ID,
			Success:  false,
			ErrorMsg: "target not configured",
			Metadata: make(map[string]interface{}),
		}
	}

	count := 3
	if c, ok := probe.Config["count"].(float64); ok && c > 0 {
		count = int(c)
	}

	pinger, err := probing.NewPinger(target)
	if err != nil {
		return &models.ProbeResult{
			ProbeID:  probe.ID,
			Success:  false,
			ErrorMsg: err.Error(),
			Metadata: make(map[string]interface{}),
		}
	}
	pinger.Count = count
	pinger.Timeout = time.Duration(probe.TimeoutSeconds) * time.Second
	if pinger.Timeout == 0 {
		pinger.Timeout = 10 * time.Second
	}
	pinger.SetPrivileged(false)

	go func() {
		<-ctx.Done()
		pinger.Stop()
	}()

	if err := pinger.Run(); err != nil {
		return &models.ProbeResult{
			ProbeID:  probe.ID,
			Success:  false,
			ErrorMsg: err.Error(),
			Metadata: make(map[string]interface{}),
		}
	}

	stats := pinger.Statistics()
	success := stats.PacketsRecv > 0

	return &models.ProbeResult{
		ProbeID:   probe.ID,
		Success:   success,
		LatencyMs: float64(stats.AvgRtt) / float64(time.Millisecond),
		ErrorMsg:  "",
		Metadata: map[string]interface{}{
			"packets_sent": stats.PacketsSent,
			"packets_recv": stats.PacketsRecv,
			"packet_loss":  stats.PacketLoss,
			"min_rtt_ms":   float64(stats.MinRtt) / float64(time.Millisecond),
			"max_rtt_ms":   float64(stats.MaxRtt) / float64(time.Millisecond),
			"avg_rtt_ms":   float64(stats.AvgRtt) / float64(time.Millisecond),
		},
	}
}
