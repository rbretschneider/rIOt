package probes

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// executeDNS runs a DNS resolution probe.
// Config: hostname, server (optional DNS server), expected_ips (optional list).
func executeDNS(ctx context.Context, probe models.Probe) *models.ProbeResult {
	hostname, _ := probe.Config["hostname"].(string)
	if hostname == "" {
		hostname, _ = probe.Config["name"].(string) // legacy frontend compat
	}
	if hostname == "" {
		return &models.ProbeResult{
			ProbeID:  probe.ID,
			Success:  false,
			ErrorMsg: "hostname not configured",
			Metadata: make(map[string]interface{}),
		}
	}

	resolver := net.DefaultResolver
	if server, ok := probe.Config["server"].(string); ok && server != "" {
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: time.Duration(probe.TimeoutSeconds) * time.Second}
				return d.DialContext(ctx, "udp", server+":53")
			},
		}
	}

	start := time.Now()
	ips, err := resolver.LookupHost(ctx, hostname)
	latency := time.Since(start)

	if err != nil {
		return &models.ProbeResult{
			ProbeID:   probe.ID,
			Success:   false,
			LatencyMs: float64(latency) / float64(time.Millisecond),
			ErrorMsg:  err.Error(),
			Metadata:  make(map[string]interface{}),
		}
	}

	success := len(ips) > 0

	// Check expected IPs if configured
	if expectedRaw, ok := probe.Config["expected_ips"].([]interface{}); ok && len(expectedRaw) > 0 {
		expected := make(map[string]bool)
		for _, v := range expectedRaw {
			if s, ok := v.(string); ok {
				expected[s] = true
			}
		}
		found := false
		for _, ip := range ips {
			if expected[ip] {
				found = true
				break
			}
		}
		if !found {
			success = false
			return &models.ProbeResult{
				ProbeID:   probe.ID,
				Success:   false,
				LatencyMs: float64(latency) / float64(time.Millisecond),
				ErrorMsg:  fmt.Sprintf("none of the expected IPs found in results: %v", ips),
				Metadata: map[string]interface{}{
					"resolved_ips": ips,
				},
			}
		}
	}

	return &models.ProbeResult{
		ProbeID:   probe.ID,
		Success:   success,
		LatencyMs: float64(latency) / float64(time.Millisecond),
		Metadata: map[string]interface{}{
			"resolved_ips": ips,
		},
	}
}
