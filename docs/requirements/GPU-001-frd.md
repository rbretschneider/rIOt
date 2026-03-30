# Formal Requirements Document

| Field   | Value                                      |
|---------|--------------------------------------------|
| Story ID | GPU-001                                   |
| Title   | GPU Telemetry Collector with Alert Support |
| Author  | Business Developer Agent                   |
| Date    | 2026-03-30                                 |
| Status  | FINAL                                      |

---

## 1. Executive Summary

Add a new `gpu` agent collector that reads GPU telemetry data (temperature, utilization, memory usage, fan speed, power draw) from NVIDIA GPUs via `nvidia-smi`, transmits this data as part of regular telemetry pushes, displays GPU metrics on the device detail page in the dashboard, and enables threshold-based alert rules for GPU temperature and other numeric GPU metrics.

---

## 2. Background & Context

rIOt currently collects basic GPU identity information (vendor, model, PCI slot, driver, VRAM) through the existing `hardware` collector, which reads from sysfs and the DRM subsystem. However, it does not collect any runtime GPU telemetry: temperature, utilization, memory consumption, fan speed, or power draw. Users running GPU-intensive workloads (machine learning, transcoding, rendering) on servers with multiple high-end GPUs (e.g., multiple NVIDIA 3090s) need visibility into GPU thermal state and utilization. Without this data, users cannot set alerts for overheating GPUs or monitor GPU workload distribution across their fleet.

The existing `hardware` collector provides static GPU identification. This story adds a new, separate `gpu` collector focused on dynamic/runtime GPU metrics, following the same pattern as other collectors (whitelist-based, Linux-only where appropriate).

---

## 3. Actors

| Actor            | Description                                                       | Permissions                                       |
|------------------|-------------------------------------------------------------------|---------------------------------------------------|
| Agent            | Go daemon running on monitored devices; collects and pushes data  | Runs as `riot` user; executes `nvidia-smi`        |
| Server           | Receives telemetry, evaluates alert rules, serves API/WebSocket   | Full read/write to database                       |
| Dashboard User   | Authenticated admin viewing the web UI                            | Views GPU telemetry, creates/manages alert rules  |
| Alert Engine     | Server-side subsystem that evaluates rules against incoming data  | Reads telemetry, creates events, triggers notifications |

---

## 4. Functional Requirements

### Agent / Collector

**FR-001:** The system must provide a new collector named `gpu` that implements the existing `Collector` interface (`Name() string`, `Collect(ctx context.Context) (interface{}, error)`).

**FR-002:** The `gpu` collector must be registered in the collector registry alongside all other collectors and must be filterable via the `collectors.enabled` whitelist in the agent YAML configuration.

**FR-003:** The `gpu` collector must detect the presence of the `nvidia-smi` command on the system. If `nvidia-smi` is not found, the collector must return an empty result (no error), consistent with how `ups` handles a missing `upsc`.

**FR-004:** The `gpu` collector must invoke `nvidia-smi` with a structured query format (CSV) to retrieve the following fields for every detected GPU on the host:

| Field              | Unit       | Required |
|--------------------|------------|----------|
| GPU index          | integer    | Yes      |
| GPU name/model     | string     | Yes      |
| GPU UUID           | string     | Yes      |
| Temperature        | Celsius    | Yes      |
| Fan speed          | percent    | Yes      |
| GPU utilization    | percent    | Yes      |
| Memory utilization | percent    | Yes      |
| Memory used        | MiB        | Yes      |
| Memory total       | MiB        | Yes      |
| Power draw         | Watts      | Yes      |
| Power limit        | Watts      | Yes      |
| PCI bus ID         | string     | Yes      |

**FR-005:** The `gpu` collector must support multiple GPUs per host. Each GPU must be represented as a separate entry in the returned data structure.

**FR-006:** If `nvidia-smi` returns a value of `[Not Supported]` or `N/A` for any field on a given GPU, the collector must treat that field as absent (nil/omitted) rather than storing a sentinel value.

**FR-007:** The `gpu` collector must execute within the same telemetry poll cycle as all other collectors (default 60 seconds). No independent polling interval is required.

**FR-008:** The `gpu` collector must operate on Linux. On non-Linux platforms, it must return an empty result (no error).

### Data Model

**FR-009:** The system must define a `GPUMetrics` (or equivalent) model struct containing the fields listed in FR-004, with each numeric field as a pointer type to represent absent values.

**FR-010:** The system must define a `GPUTelemetry` (or equivalent) model struct containing a slice of `GPUMetrics`, representing all GPUs on a single host.

**FR-011:** The `FullTelemetryData` struct must include a new field for GPU telemetry (e.g., `GPU *GPUTelemetry`), with a JSON key (e.g., `"gpu_telemetry"`) that does not conflict with the existing `"hardware"` field's `GPUs` sub-field.

**FR-012:** The agent's `collectAll` function must include a type-switch case that maps the `gpu` collector's output to the new field on `FullTelemetryData`.

### Server / Storage

**FR-013:** The server must accept and store GPU telemetry as part of the existing telemetry snapshot JSON blob. No new database table is required; the data must be stored within the existing `data` JSONB column of the telemetry snapshots table.

**FR-014:** The server must expose GPU telemetry data through the existing device telemetry API endpoints (the same endpoints that return `FullTelemetryData`). No new API endpoints are required.

### Alert Engine

**FR-015:** The alert engine must support a new metric `gpu_temp` that extracts the temperature value from GPU telemetry. When a device has multiple GPUs, the alert engine must evaluate the rule against each GPU independently. If any single GPU's temperature satisfies the alert condition, the alert must fire.

**FR-016:** The alert engine must support a new metric `gpu_util_percent` that extracts GPU utilization percentage, evaluated per-GPU as described in FR-015.

**FR-017:** The alert engine must support a new metric `gpu_mem_percent` that extracts GPU memory utilization percentage, evaluated per-GPU as described in FR-015.

**FR-018:** The alert engine must support a new metric `gpu_power_watts` that extracts GPU power draw in watts, evaluated per-GPU as described in FR-015.

**FR-019:** Alert events generated from GPU metrics must include the GPU name/index in the event detail so the user can identify which GPU triggered the alert.

**FR-020:** The system must provide the following alert templates for GPU metrics:

| Template ID        | Name                    | Category | Metric            | Operator | Threshold | Severity | Cooldown | Description                                              |
|--------------------|-------------------------|----------|--------------------|----------|-----------|----------|----------|----------------------------------------------------------|
| `gpu_temp_warn`    | GPU Temperature Warning | gpu      | `gpu_temp`         | >        | 80        | warning  | 3600s    | Fires when any GPU temperature exceeds the threshold     |
| `gpu_temp_crit`    | GPU Temperature Critical| gpu      | `gpu_temp`         | >        | 90        | critical | 1800s    | Fires when any GPU temperature exceeds a critical threshold |

### Dashboard

**FR-021:** The device detail page must display a GPU telemetry card when GPU telemetry data is present in the latest telemetry snapshot. The card must not appear when no GPU telemetry data exists.

**FR-022:** The GPU telemetry card must display, for each GPU: the GPU name, temperature (in Celsius with color-coded thresholds), GPU utilization percentage, memory usage (used/total in MiB and percentage), fan speed percentage, and power draw (current/limit in Watts).

**FR-023:** Temperature values on the GPU card must use the same color-coding convention used elsewhere in the dashboard for temperatures (green/yellow/orange/red thresholds).

**FR-024:** The GPU telemetry card must update in real-time via WebSocket when new telemetry arrives, consistent with how other telemetry cards behave.

**FR-025:** The GPU telemetry card must support one-click alert creation for GPU temperature, consistent with the one-click alert pattern used for services, USB devices, and UPS metrics.

### Agent Diagnostics

**FR-026:** The `riot-agent doctor` command must include `nvidia-smi` as a dependency check for the `gpu` collector, consistent with how `smartctl` is listed for `hardware` and `upsc` for `ups`.

---

## 5. Non-Functional Requirements

**NFR-001:** [Performance] The `gpu` collector must complete its `nvidia-smi` invocation and parsing within 5 seconds under normal conditions. If `nvidia-smi` hangs, the context timeout from the telemetry cycle must cancel the operation.

**NFR-002:** [Reliability] A failure in the `gpu` collector must not prevent other collectors from running. The agent must log a warning and continue the telemetry cycle with GPU data absent.

**NFR-003:** [Compatibility] The collector must work with NVIDIA driver versions that ship `nvidia-smi` with CSV query support (`--query-gpu` flag). This covers all modern NVIDIA driver versions (400+).

**NFR-004:** [Data Size] GPU telemetry for a host with 8 GPUs must not add more than 4 KB to the telemetry JSON payload.

---

## 6. Business Rules

**BR-001:** The `gpu` collector must not be included in the default `collectors.enabled` list for new agent installs. Existing agent installs use a whitelist; users must manually add `gpu` to their `collectors.enabled` in `/etc/riot/agent.yaml` and restart the agent.

**BR-002:** The `gpu` collector is independent of the `hardware` collector. Enabling `gpu` must not require `hardware` to be enabled, and vice versa. The `hardware` collector continues to provide static GPU identity (vendor, model, VRAM via sysfs/DRM); the `gpu` collector provides runtime metrics via `nvidia-smi`.

**BR-003:** GPU alert templates must appear in the "Create from Template" UI alongside existing templates. The `gpu` category must be used for grouping.

**BR-004:** Only NVIDIA GPUs (via `nvidia-smi`) are in scope. AMD GPU support (via `rocm-smi` or sysfs hwmon) is explicitly out of scope for this story.

---

## 7. Data Requirements

### Entities

**GPUDeviceMetrics** (one per GPU per telemetry snapshot):

| Field           | Type     | Nullable | Validation                        |
|-----------------|----------|----------|-----------------------------------|
| Index           | int      | No       | >= 0                              |
| Name            | string   | No       | Non-empty                         |
| UUID            | string   | No       | Non-empty                         |
| PCIBusID        | string   | No       | Non-empty                         |
| TemperatureC    | *int     | Yes      | 0-150 when present                |
| FanSpeedPercent | *int     | Yes      | 0-100 when present                |
| UtilizationPct  | *int     | Yes      | 0-100 when present                |
| MemUtilPct      | *int     | Yes      | 0-100 when present                |
| MemUsedMiB      | *int     | Yes      | >= 0 when present                 |
| MemTotalMiB     | *int     | Yes      | > 0 when present                  |
| PowerDrawW      | *float64 | Yes      | >= 0 when present                 |
| PowerLimitW     | *float64 | Yes      | > 0 when present                  |

**GPUTelemetry** (one per device per telemetry snapshot):

| Field  | Type               | Nullable | Validation         |
|--------|--------------------|----------|--------------------|
| GPUs   | []GPUDeviceMetrics | No       | May be empty slice |

### State Transitions

No state machine applies. GPU telemetry is point-in-time snapshot data with no state transitions.

---

## 8. Acceptance Criteria

**AC-001:** [Maps to FR-001, FR-002, FR-007, FR-012]
```
Given: An agent with `gpu` in its `collectors.enabled` list and `nvidia-smi` installed
When:  A telemetry poll cycle runs
Then:  The telemetry payload sent to the server must contain a `gpu_telemetry` field with one entry per detected GPU
And:   Each entry must contain at minimum: index, name, UUID, PCI bus ID, and temperature
```

**AC-002:** [Maps to FR-003, FR-008]
```
Given: An agent with `gpu` in its `collectors.enabled` list
When:  `nvidia-smi` is not installed on the host
Then:  The collector must return an empty result without error
And:   The telemetry payload must omit the `gpu_telemetry` field (or include it as null)
And:   No error must be logged
```

**AC-003:** [Maps to FR-005]
```
Given: A host with 4 NVIDIA GPUs
When:  The `gpu` collector runs
Then:  The result must contain exactly 4 GPU entries
And:   Each entry must have a distinct index (0, 1, 2, 3) and distinct UUID
```

**AC-004:** [Maps to FR-006]
```
Given: A GPU where `nvidia-smi` reports fan speed as `[Not Supported]`
When:  The `gpu` collector parses the output
Then:  The FanSpeedPercent field for that GPU must be nil/omitted
And:   All other supported fields must be populated normally
```

**AC-005:** [Maps to FR-015, FR-019]
```
Given: An alert rule with metric `gpu_temp`, operator `>`, threshold `80`
And:   A device with 3 GPUs at temperatures 65, 85, 70
When:  The alert engine evaluates the rule against the latest telemetry
Then:  The alert must fire
And:   The generated event must identify the GPU (by name or index) that exceeded the threshold
```

**AC-006:** [Maps to FR-015]
```
Given: An alert rule with metric `gpu_temp`, operator `>`, threshold `80`
And:   A device with 3 GPUs at temperatures 65, 72, 70
When:  The alert engine evaluates the rule against the latest telemetry
Then:  The alert must not fire
```

**AC-007:** [Maps to FR-021, FR-022]
```
Given: A device with GPU telemetry data in its latest snapshot
When:  A user views the device detail page
Then:  A GPU telemetry card must be visible
And:   The card must show each GPU's name, temperature, utilization, memory usage, fan speed, and power draw
```

**AC-008:** [Maps to FR-021]
```
Given: A device with no GPU telemetry data (collector not enabled or no NVIDIA GPUs)
When:  A user views the device detail page
Then:  No GPU telemetry card must be displayed
```

**AC-009:** [Maps to FR-020]
```
Given: A user navigates to Settings > Alert Rules > Create from Template
When:  The template list is displayed
Then:  Templates "GPU Temperature Warning" and "GPU Temperature Critical" must be present in a "gpu" category
```

**AC-010:** [Maps to FR-023]
```
Given: A GPU reporting a temperature of 45C
When:  Displayed on the GPU card
Then:  The temperature must be rendered in green

Given: A GPU reporting a temperature of 75C
When:  Displayed on the GPU card
Then:  The temperature must be rendered in yellow/orange

Given: A GPU reporting a temperature of 92C
When:  Displayed on the GPU card
Then:  The temperature must be rendered in red
```

**AC-011:** [Maps to FR-026]
```
Given: An agent with `gpu` in its `collectors.enabled` list
When:  `riot-agent doctor` is run
Then:  The output must include a dependency check for `nvidia-smi`
And:   If `nvidia-smi` is not found, the check must report a warning
```

**AC-012:** [Maps to FR-002, BR-001]
```
Given: A freshly installed agent using the default configuration
When:  The agent starts
Then:  The `gpu` collector must not run unless explicitly added to `collectors.enabled`
```

**AC-013:** [Maps to FR-013, FR-014]
```
Given: GPU telemetry is stored in a telemetry snapshot
When:  The existing device telemetry API endpoint is called
Then:  The response JSON must include GPU telemetry data under the `gpu_telemetry` key
And:   No new API endpoint must be required
```

**AC-014:** [Maps to FR-025]
```
Given: A device detail page showing the GPU telemetry card
When:  The user clicks the alert icon next to a GPU temperature reading
Then:  An alert rule creation dialog must open pre-filled with metric `gpu_temp` and severity `warning`
```

---

## 9. Out of Scope

- AMD GPU support (via `rocm-smi`, `amdgpu` sysfs hwmon, or ROCm)
- Intel GPU support (via `intel_gpu_top` or sysfs)
- GPU process list (which processes are using which GPU)
- GPU historical charts / time-series graphs (may be a follow-up story)
- Per-GPU naming/aliasing in the dashboard
- GPU overclocking or fan control from the dashboard
- Windows GPU monitoring (DirectX/WMI)
- macOS GPU monitoring (Metal/IOKit)
- NVML library direct integration (linking against `libnvidia-ml.so`); this story uses `nvidia-smi` CLI only
- New database tables or migrations for GPU data (stored in existing JSONB telemetry column)

---

## 10. Assumptions

- **A-001:** `nvidia-smi` is available in the system PATH or at `/usr/bin/nvidia-smi` on hosts with NVIDIA GPUs and the proprietary driver installed. The agent does not need to install or manage NVIDIA drivers.
- **A-002:** `nvidia-smi --query-gpu=... --format=csv,noheader,nounits` is the standard invocation and produces one line per GPU with comma-separated values. This is stable across NVIDIA driver versions 400+.
- **A-003:** The `riot` user (under which the agent runs) has permission to execute `nvidia-smi` without sudo. `nvidia-smi` is a user-space tool that does not require root privileges for read-only queries.
- **A-004:** Temperature color-coding thresholds for GPUs will follow reasonable defaults (e.g., green < 60C, yellow 60-79C, orange 80-89C, red >= 90C). Exact thresholds are a UI implementation detail.
- **A-005:** The JSON key for GPU telemetry in `FullTelemetryData` must not conflict with the existing `hardware.gpus` field. Using `gpu_telemetry` as the key distinguishes runtime metrics from static identity data.

---

## 11. Open Questions

None. All ambiguities have been resolved through the user story context and codebase analysis.

---

## 12. Dependencies

| Dependency                     | Type     | Description                                                                              |
|--------------------------------|----------|------------------------------------------------------------------------------------------|
| Existing collector framework   | Internal | The `Collector` interface, `Registry`, `FilterEnabled`, and `collectAll` type-switch      |
| Existing alert engine          | Internal | The alert rule evaluation loop, template system, and event generation                     |
| Existing telemetry API         | Internal | The `/api/v1/devices/{id}/telemetry` endpoint and WebSocket push                          |
| Existing dashboard patterns    | Internal | Device detail card layout, temperature color-coding utility, one-click alert creation     |
| `nvidia-smi` CLI               | External | NVIDIA System Management Interface; must be present on monitored host for data collection |
| `doctor.go` collector deps map | Internal | Must be updated to include `gpu` -> `nvidia-smi` mapping                                  |
