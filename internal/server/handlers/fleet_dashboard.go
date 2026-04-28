package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// windowRegex enforces the AD-013 acceptance grammar:
// ^[1-9][0-9]{0,4}[ms]$
// - numeric prefix 1..99999, no leading zero, no sign, no whitespace
// - exactly one suffix: 'm' (minutes) or 's' (seconds)
// - max 5 prefix digits to bound input size before parsing
var windowRegex = regexp.MustCompile(`^[1-9][0-9]{0,4}[ms]$`)

// parseWindowParam parses and validates the `window` query parameter per AD-013.
// Rules:
//   - Parameter MISSING (key absent)               → default 60 * time.Minute
//   - Parameter PRESENT BUT EMPTY ("?window=")     → 400
//   - Does not match ^[1-9][0-9]{0,4}[ms]$         → 400
//   - Resolved duration <= 0 or > 60m              → 400
//   - Never silently clamps; always rejects on out-of-bounds
//
// On success returns the resolved duration. On failure returns 0 and writes a
// 400 response; the caller must return immediately.
func parseWindowParam(w http.ResponseWriter, r *http.Request) (time.Duration, bool) {
	raw := r.URL.Query().Get("window")

	// Distinguish missing key (→ default) from present-but-empty (→ 400).
	if !r.URL.Query().Has("window") {
		// Key absent → default 60 minutes.
		return 60 * time.Minute, true
	}

	// Key present but value is empty → reject.
	if raw == "" {
		// Truncate raw value in log to defend against log injection.
		logRaw := truncateForLog(raw)
		slog.Warn("invalid window parameter", slog.String("raw", logRaw))
		http.Error(w, `{"error":"invalid window parameter"}`, http.StatusBadRequest)
		return 0, false
	}

	// Validate against strict acceptance grammar.
	if !windowRegex.MatchString(raw) {
		logRaw := truncateForLog(raw)
		slog.Warn("invalid window parameter", slog.String("raw", logRaw))
		http.Error(w, `{"error":"invalid window parameter"}`, http.StatusBadRequest)
		return 0, false
	}

	// Parse: last character is the unit suffix.
	unit := raw[len(raw)-1]
	numStr := raw[:len(raw)-1]

	// strconv.Atoi cannot overflow here because the grammar limits to 5 digits
	// (max 99999), well within int range.
	n, err := strconv.Atoi(numStr)
	if err != nil {
		// Should be unreachable due to regex, but be defensive.
		logRaw := truncateForLog(raw)
		slog.Warn("invalid window parameter", slog.String("raw", logRaw))
		http.Error(w, `{"error":"invalid window parameter"}`, http.StatusBadRequest)
		return 0, false
	}

	var duration time.Duration
	switch unit {
	case 'm':
		duration = time.Duration(n) * time.Minute
	case 's':
		duration = time.Duration(n) * time.Second
	default:
		// Unreachable due to regex.
		logRaw := truncateForLog(raw)
		slog.Warn("invalid window parameter", slog.String("raw", logRaw))
		http.Error(w, `{"error":"invalid window parameter"}`, http.StatusBadRequest)
		return 0, false
	}

	// Bounds: duration must be in (0, 60m]. Zero is impossible from the grammar
	// (prefix starts at 1) but check defensively.
	if duration <= 0 || duration > 60*time.Minute {
		logRaw := truncateForLog(raw)
		slog.Warn("invalid window parameter", slog.String("raw", logRaw))
		http.Error(w, `{"error":"invalid window parameter"}`, http.StatusBadRequest)
		return 0, false
	}

	return duration, true
}

// truncateForLog returns at most 32 bytes of s to bound log output against
// absurd inputs.
func truncateForLog(s string) string {
	const maxLogLen = 32
	if len(s) <= maxLogLen {
		return s
	}
	return s[:maxLogLen] + "…"
}

// FleetHeartbeatsResponse is the JSON shape returned by GET /api/v1/fleet/heartbeats.
type FleetHeartbeatsResponse struct {
	Since          time.Time                  `json:"since"`
	Until          time.Time                  `json:"until"`
	Devices        map[string][]HeartbeatJSON `json:"devices"`
	DevicesWithGPU []string                   `json:"devices_with_gpu"`
}

// HeartbeatJSON is the per-heartbeat shape in the response.
type HeartbeatJSON struct {
	DeviceID  string      `json:"device_id"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// FleetContainerRow is the per-container shape returned by GET /api/v1/fleet/containers.
// Fields mirror only what the dashboard leaderboard renders (AD-011, AD-012).
type FleetContainerRow struct {
	DeviceID        string  `json:"device_id"`
	Hostname        string  `json:"hostname"`
	ContainerID     string  `json:"container_id"`
	ContainerName   string  `json:"container_name"`
	Image           string  `json:"image"`
	Stack           string  `json:"stack,omitempty"`
	State           string  `json:"state"`
	CPUPercent      float64 `json:"cpu_percent"`
	MemUsage        int64   `json:"mem_usage"`
	MemLimit        int64   `json:"mem_limit"`
	RestartCount    int     `json:"restart_count"`
	UpdateAvailable *bool   `json:"update_available,omitempty"`
}

// FleetHeartbeats handles GET /api/v1/fleet/heartbeats?window=60m.
// Returns 60 minutes of heartbeats for every device in a single round trip
// plus a list of device IDs that have GPU telemetry (AD-001, AD-008).
//
// Window cap is 60 minutes. The per-device endpoint allows up to 168 hours;
// the fleet endpoint caps at 60 minutes deliberately to prevent combinatorial
// blowup (AD-013 divergence note — do not "fix" this to match the per-device cap).
func (h *Handlers) FleetHeartbeats(w http.ResponseWriter, r *http.Request) {
	duration, ok := parseWindowParam(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	until := time.Now().UTC()
	since := until.Add(-duration)

	heartbeats, err := h.telemetry.GetFleetHeartbeats(ctx, since)
	if err != nil {
		slog.Error("failed to fetch fleet heartbeats", slog.String("err", err.Error()))
		http.Error(w, `{"error":"failed to fetch heartbeats"}`, http.StatusInternalServerError)
		return
	}

	gpuDeviceIDs, err := h.telemetry.GetGPUDeviceIDs(ctx)
	if err != nil {
		// Non-fatal: GPU tile simply won't render. Log and continue.
		slog.Warn("failed to fetch GPU device IDs", slog.String("err", err.Error()))
		gpuDeviceIDs = []string{}
	}

	if gpuDeviceIDs == nil {
		gpuDeviceIDs = []string{}
	}

	// Build the response. Convert the map[string][]models.Heartbeat to the
	// JSON-friendly shape (preserving timestamp and data as-is).
	deviceMap := make(map[string][]HeartbeatJSON, len(heartbeats))
	for deviceID, hbs := range heartbeats {
		rows := make([]HeartbeatJSON, len(hbs))
		for i, hb := range hbs {
			rows[i] = HeartbeatJSON{
				DeviceID:  hb.DeviceID,
				Timestamp: hb.Timestamp,
				Data:      hb.Data,
			}
		}
		deviceMap[deviceID] = rows
	}

	resp := FleetHeartbeatsResponse{
		Since:          since,
		Until:          until,
		Devices:        deviceMap,
		DevicesWithGPU: gpuDeviceIDs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// FleetContainers handles GET /api/v1/fleet/containers.
// Returns a flattened list of containers across the fleet, using a JSONB
// projection that avoids full telemetry blob decode (AD-011).
//
// IMPORTANT: This handler MUST NOT call h.telemetry.GetAllLatestSnapshots.
// It MUST call h.telemetry.GetFleetContainerLeaderboard (the projected method).
// This is verified by the AD-011 spy test in fleet_dashboard_test.go.
func (h *Handlers) FleetContainers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projections, err := h.telemetry.GetFleetContainerLeaderboard(ctx)
	if err != nil {
		slog.Error("failed to fetch fleet containers", slog.String("err", err.Error()))
		http.Error(w, `{"error":"failed to fetch fleet containers"}`, http.StatusInternalServerError)
		return
	}

	devices, err := h.devices.List(ctx)
	if err != nil {
		slog.Error("failed to fetch device list for fleet containers", slog.String("err", err.Error()))
		http.Error(w, `{"error":"failed to fetch fleet containers"}`, http.StatusInternalServerError)
		return
	}

	// Build hostname lookup.
	hostnameByID := make(map[string]string, len(devices))
	for _, d := range devices {
		hostnameByID[d.ID] = d.Hostname
	}

	var rows []FleetContainerRow
	for _, proj := range projections {
		hostname := hostnameByID[proj.DeviceID]
		for _, c := range proj.Containers {
			// AD-012: read ONLY com.docker.compose.project from Labels.
			// No other label key may flow into the response.
			stack := ""
			if c.Labels != nil {
				stack = c.Labels["com.docker.compose.project"]
			}

			rows = append(rows, FleetContainerRow{
				DeviceID:        proj.DeviceID,
				Hostname:        hostname,
				ContainerID:     c.ID,
				ContainerName:   c.Name,
				Image:           c.Image,
				Stack:           stack,
				State:           c.State,
				CPUPercent:      c.CPUPercent,
				MemUsage:        c.MemUsage,
				MemLimit:        c.MemLimit,
				RestartCount:    c.RestartCount,
				UpdateAvailable: c.UpdateAvailable,
			})
		}
	}

	if rows == nil {
		rows = []FleetContainerRow{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rows)
}
