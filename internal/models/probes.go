package models

import "time"

// Probe represents a synthetic monitoring check.
type Probe struct {
	ID              int64                  `json:"id"`
	Name            string                 `json:"name"`
	Type            string                 `json:"type"`     // ping, dns, http
	Enabled         bool                   `json:"enabled"`
	Config          map[string]interface{} `json:"config"`
	IntervalSeconds int                    `json:"interval_seconds"`
	TimeoutSeconds  int                    `json:"timeout_seconds"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

// ProbeResult is a single probe execution result.
type ProbeResult struct {
	ID         int64                  `json:"id"`
	ProbeID    int64                  `json:"probe_id"`
	Success    bool                   `json:"success"`
	LatencyMs  float64                `json:"latency_ms"`
	StatusCode *int                   `json:"status_code,omitempty"`
	ErrorMsg   string                 `json:"error_msg"`
	Metadata   map[string]interface{} `json:"metadata"`
	CreatedAt  time.Time              `json:"created_at"`
}

// Event types for probes
const (
	EventProbeDown      EventType = "probe_down"
	EventProbeRecovered EventType = "probe_recovered"
	EventTLSExpiring    EventType = "tls_expiring"
)
