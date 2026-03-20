package models

import "time"

// DeviceProbe represents a custom check configured to run on an agent.
type DeviceProbe struct {
	ID              int64                  `json:"id"`
	Name            string                 `json:"name"`
	DeviceID        string                 `json:"device_id"`
	Type            string                 `json:"type"` // shell, http, container_exec, port, file
	Enabled         bool                   `json:"enabled"`
	Config          map[string]interface{} `json:"config"`
	Assertions      []ProbeAssertion       `json:"assertions"`
	IntervalSeconds int                    `json:"interval_seconds"`
	TimeoutSeconds  int                    `json:"timeout_seconds"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

// ProbeAssertion defines a declarative assertion on probe output.
type ProbeAssertion struct {
	Field    string `json:"field"`    // exit_code, stdout, stderr, status_code, body, latency_ms, connected, exists, size, content
	Operator string `json:"operator"` // eq, ne, contains, regex, gt, lt
	Value    string `json:"value"`
}

// DeviceProbeResult holds the outcome of a single probe execution.
type DeviceProbeResult struct {
	ID               int64                  `json:"id"`
	ProbeID          int64                  `json:"probe_id"`
	DeviceID         string                 `json:"device_id"`
	Success          bool                   `json:"success"`
	LatencyMs        float64                `json:"latency_ms"`
	Output           map[string]interface{} `json:"output"`
	FailedAssertions []ProbeAssertion       `json:"failed_assertions,omitempty"`
	ErrorMsg         string                 `json:"error_msg,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
}

// DeviceProbeWithResult extends DeviceProbe with latest result and stats.
type DeviceProbeWithResult struct {
	DeviceProbe
	LatestResult *DeviceProbeResult `json:"latest_result,omitempty"`
	SuccessRate  *float64           `json:"success_rate,omitempty"`
	TotalChecks  int                `json:"total_checks"`
}

// DeviceProbeWithResultEnriched extends DeviceProbeWithResult with the device hostname
// for use in the all-device-probes list endpoint.
type DeviceProbeWithResultEnriched struct {
	DeviceProbeWithResult
	DeviceHostname string `json:"device_hostname"`
}
