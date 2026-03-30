# Architecture Decision Document

| Field         | Value                                          |
|---------------|------------------------------------------------|
| Story ID      | GPU-001                                        |
| FRD Reference | docs/requirements/GPU-001-frd.md               |
| Author        | Architect Agent                                |
| Date          | 2026-03-30                                     |
| Status        | FINAL                                          |

---

## 1. Summary

Add a `gpu` collector to the agent that invokes `nvidia-smi` in CSV mode to collect runtime GPU metrics (temperature, utilization, memory, fan speed, power draw) for all NVIDIA GPUs on a host. The data flows through the existing telemetry pipeline (JSONB storage, WebSocket broadcast, REST API) with no new endpoints or migrations. The server's alert engine gains four new GPU metrics evaluated per-GPU, two new alert templates in a `gpu` category, and the React dashboard gains a GPU telemetry card on the device detail page with temperature color-coding and one-click alert creation.

---

## 2. Technical Context

### What exists

- **Collector framework**: `Collector` interface with `Name()` and `Collect()`, a `Registry` with `RegisterDefaults`/`FilterEnabled`, and a type-switch in `agent/telemetry.go:collectAll` that maps results to `FullTelemetryData` fields.
- **Hardware collector**: Already collects static GPU identity (`GPUInfo`: vendor, model, PCI slot, driver, VRAM) from sysfs/DRM under the `hardware` key. This is separate from the new runtime metrics.
- **UPS collector**: Reference pattern for optional-CLI collectors -- returns empty struct when `upsc` is missing, Linux-only guard, pointer types for nullable fields.
- **Telemetry pipeline**: Agent serializes `FullTelemetryData` as JSON, server deserializes into the same struct, stores as JSONB in `telemetry_snapshots.data`, broadcasts via WebSocket. No schema change needed.
- **Alert engine**: `Generator.CheckTelemetryThresholds` dispatches to per-subsystem check methods. `evaluateMetric` handles numeric threshold checks with cooldown. `CheckDiskSmartAlerts` iterates over multiple drives -- closest pattern to per-GPU evaluation.
- **Alert templates**: Hard-coded in `events/templates.go`, served via `GET /api/v1/settings/alert-templates`. Categories include service, network, process, system, container, ups, usb, webserver, disk.
- **Frontend**: `DeviceDetail.tsx` uses `Section`, `InfoItem`, `GaugeBar`, `AlertIcon`/`AlertButton` helper components. `CreateAlertDialog` has `METRIC_DEFAULTS` for pre-filling one-click rules. `FullTelemetryData` TypeScript interface mirrors the Go struct. Temperature color-coding for disk SMART uses inline ternary chains.
- **Doctor**: `collectorDeps` map in `doctor.go` plus `allCollectors` slice for the status listing.

### What's missing

- No `GPUTelemetry`/`GPUDeviceMetrics` model types.
- No `gpu` collector.
- No `gpu_telemetry` field on `FullTelemetryData` (Go or TypeScript).
- No GPU-related event types, alert metrics, or templates.
- No GPU card in the dashboard.
- No `gpu` entry in doctor's collector list or dependency map.

---

## 3. Architecture Decisions

### AD-001: nvidia-smi invocation strategy

**Decision**: Invoke `nvidia-smi` once per collection cycle with `--query-gpu=index,name,uuid,temperature.gpu,fan.speed,utilization.gpu,utilization.memory,memory.used,memory.total,power.draw,power.limit,pci.bus_id --format=csv,noheader,nounits`. Parse the CSV output line-by-line, one line per GPU.

**Rationale**: This is the standard approach documented by NVIDIA. A single invocation returns all GPUs and all fields. CSV with `nounits` gives clean numeric parsing. The `noheader` flag removes the need to skip a header row.

**Alternatives Considered**:
- `nvidia-smi -x` (XML output): heavier to parse, no benefit for the fields we need.
- NVML library (`libnvidia-ml.so`): requires CGO, complicates cross-compilation, explicitly out of scope per FRD.
- Multiple `nvidia-smi` calls per field: unnecessary overhead.

**Consequences**: Depends on `nvidia-smi` being in PATH. CSV parsing is fragile to format changes, but `--query-gpu` output has been stable since driver 400+.

### AD-002: Collector pattern -- empty result on missing binary

**Decision**: Follow the UPS collector pattern. Check `exec.LookPath("nvidia-smi")` first. If not found, return `&models.GPUTelemetry{}` with nil error. On non-Linux, return empty immediately. Use `exec.CommandContext` with the parent context to respect the telemetry cycle timeout.

**Rationale**: Consistent with existing conventions. The FRD explicitly requires this behavior (FR-003, FR-008).

**Alternatives Considered**: None -- this is the established pattern.

**Consequences**: No log noise on hosts without NVIDIA GPUs.

### AD-003: Data model -- pointer types for nullable numeric fields

**Decision**: Define `GPUDeviceMetrics` with `*int` for integer metrics (temperature, fan speed, utilization, memory) and `*float64` for power values. Non-nullable fields: `Index int`, `Name string`, `UUID string`, `PCIBusID string`. Wrap in `GPUTelemetry` struct with `GPUs []GPUDeviceMetrics`. Add `GPUTelemetry *GPUTelemetry` to `FullTelemetryData` with JSON key `"gpu_telemetry"`.

**Rationale**: Matches the FRD data requirements exactly (FR-009, FR-010, FR-011). Pointer types allow `omitempty` to drop `[Not Supported]` fields from JSON. The `gpu_telemetry` key avoids conflict with `hardware.gpus` (the static GPU identity data).

**Alternatives Considered**:
- Using `float64` for all numeric fields: temperature and utilization are always integers from nvidia-smi, so `*int` is more precise and matches the FRD type spec.

**Consequences**: Consumers must nil-check pointer fields before use.

### AD-004: Parsing `[Not Supported]` and `N/A` values

**Decision**: After splitting each CSV line on `,`, trim whitespace from each field. If a field equals `[Not Supported]`, `N/A`, or is empty, leave the corresponding pointer as nil. Otherwise parse as int or float64. If parsing fails, treat as nil (do not error).

**Rationale**: FR-006 requires absent values for unsupported fields. Defensive parsing avoids crashing on unexpected nvidia-smi output.

**Alternatives Considered**: Returning an error on parse failure -- rejected because a single unparseable field should not discard the entire GPU's data.

**Consequences**: Silently drops malformed values. This is acceptable for telemetry.

### AD-005: Alert engine -- per-GPU evaluation

**Decision**: Add a `CheckGPUAlerts` method on `Generator` following the `CheckDiskSmartAlerts` pattern. Iterate over each GPU in `GPUTelemetry.GPUs`. For each GPU, extract the relevant metric value and call `evaluateMetric` with a message function that includes the GPU name and index. Support four metrics: `gpu_temp`, `gpu_util_percent`, `gpu_mem_percent`, `gpu_power_watts`.

**Rationale**: The disk SMART alert pattern already handles iterating over multiple items and evaluating thresholds per-item. GPU alerts follow the same structure. Including GPU name/index in the event message satisfies FR-019.

**Alternatives Considered**:
- Evaluating only the max value across all GPUs: rejected because FR-015 explicitly says "each GPU independently" and the event must identify which GPU triggered.

**Consequences**: A device with 8 GPUs will generate up to 8 events per metric per evaluation cycle (subject to cooldown). This is correct behavior per the FRD.

### AD-006: Two new event types for GPU alerts

**Decision**: Add `EventGPUTemp EventType = "gpu_temp"` and `EventGPUMetric EventType = "gpu_metric"` to the events model. `gpu_temp` is used for temperature alerts (both warn and crit templates map to this). `gpu_metric` is used for utilization, memory, and power alerts.

**Rationale**: Follows the pattern where related alerts share event types (e.g., `EventDiskSmartTemp` covers both warn and crit disk temp templates). Keeping a separate `gpu_temp` type allows future filtering/display of GPU temperature events specifically.

**Alternatives Considered**:
- One event type per metric (4 total): over-granular for the event log display.
- Single `gpu_alert` type for everything: loses the ability to distinguish temperature events from other GPU metrics.

**Consequences**: Event log will show "gpu_temp" and "gpu_metric" as event types.

### AD-007: Frontend GPU card placement

**Decision**: Place the GPU telemetry card in `DeviceDetail.tsx` after the UPS section and before the Hardware Details section. The card renders only when `tel?.gpu_telemetry?.gpus?.length > 0`. Each GPU is rendered as a sub-section within the card showing name, temperature (color-coded), utilization gauge, memory usage (used/total MiB + percentage gauge), fan speed, and power draw (current/limit W).

**Rationale**: The UPS section is the closest analog (hardware subsystem with nullable metrics, alert buttons). Placing GPU runtime metrics before static hardware details groups dynamic data together.

**Alternatives Considered**:
- Extracting to a separate `GPUCard.tsx` component: the existing codebase puts all cards inline in DeviceDetail.tsx. Follow the pattern; refactoring can happen later.

**Consequences**: DeviceDetail.tsx grows larger. This is consistent with how all other telemetry cards are implemented.

### AD-008: Temperature color-coding thresholds

**Decision**: Use the following GPU temperature thresholds:
- Green: < 60C
- Yellow/Amber: 60-79C
- Orange: 80-89C
- Red: >= 90C

Implement as inline ternary chain matching the disk SMART temperature pattern.

**Rationale**: FRD Assumption A-004 specifies these ranges. They align with typical NVIDIA GPU thermal characteristics (throttle at ~83C, max ~93C depending on model).

**Alternatives Considered**: Extracting to a shared utility function -- the disk SMART temps use different thresholds (35/45/55), so a shared function would need parameterization. Inline is simpler and matches existing code.

**Consequences**: Thresholds are hardcoded in the frontend. A future story could make them configurable.

---

## 4. Component Changes

| Action | File Path | Purpose |
|--------|-----------|---------|
| CREATE | `internal/agent/collectors/gpu.go` | GPU collector: nvidia-smi invocation, CSV parsing, Linux-only guard |
| CREATE | `internal/agent/collectors/gpu_test.go` | Unit tests for CSV parsing, `[Not Supported]` handling, multi-GPU, empty result |
| MODIFY | `internal/agent/collectors/collector.go` | Register `GPUCollector` in `RegisterDefaultsWithDocker` |
| MODIFY | `internal/agent/telemetry.go` | Add `*models.GPUTelemetry` case to `collectAll` type-switch |
| MODIFY | `internal/agent/doctor.go` | Add `"gpu"` to `collectorDeps` map and `allCollectors` slice |
| MODIFY | `internal/models/telemetry.go` | Add `GPUDeviceMetrics`, `GPUTelemetry` structs and `GPUTelemetry` field on `FullTelemetryData` |
| MODIFY | `internal/models/events.go` | Add `EventGPUTemp` and `EventGPUMetric` event type constants |
| MODIFY | `internal/server/events/generator.go` | Add `CheckGPUAlerts` method; call it from `CheckTelemetryThresholds` |
| CREATE | `internal/server/events/gpu_alerts_test.go` | Unit tests for GPU alert evaluation (per-GPU, multi-GPU, nil fields) |
| MODIFY | `internal/server/events/templates.go` | Add `gpu_temp_warn` and `gpu_temp_crit` templates in `gpu` category |
| MODIFY | `web/src/types/models.ts` | Add `GPUDeviceMetrics`, `GPUTelemetry` interfaces; add `gpu_telemetry` to `FullTelemetryData` |
| MODIFY | `web/src/pages/DeviceDetail.tsx` | Add GPU telemetry card section with per-GPU display, color-coded temps, gauges, alert buttons |
| MODIFY | `web/src/components/CreateAlertDialog.tsx` | Add `gpu_temp` to `METRIC_DEFAULTS` and `metricLabels` |

---

## 5. Data Model Changes

### New types (in `internal/models/telemetry.go`)

```go
// GPUDeviceMetrics holds runtime metrics for a single NVIDIA GPU.
type GPUDeviceMetrics struct {
    Index           int      `json:"index"`
    Name            string   `json:"name"`
    UUID            string   `json:"uuid"`
    PCIBusID        string   `json:"pci_bus_id"`
    TemperatureC    *int     `json:"temperature_c,omitempty"`
    FanSpeedPercent *int     `json:"fan_speed_percent,omitempty"`
    UtilizationPct  *int     `json:"utilization_pct,omitempty"`
    MemUtilPct      *int     `json:"mem_util_pct,omitempty"`
    MemUsedMiB      *int     `json:"mem_used_mib,omitempty"`
    MemTotalMiB     *int     `json:"mem_total_mib,omitempty"`
    PowerDrawW      *float64 `json:"power_draw_w,omitempty"`
    PowerLimitW     *float64 `json:"power_limit_w,omitempty"`
}

// GPUTelemetry holds runtime GPU metrics for all GPUs on a host.
type GPUTelemetry struct {
    GPUs []GPUDeviceMetrics `json:"gpus"`
}
```

### Modified type (`FullTelemetryData`)

**Before**:
```go
type FullTelemetryData struct {
    // ... existing fields ...
    CronJobs       *CronInfo            `json:"cron_jobs,omitempty"`
    ContainerLogs  []ContainerLogEntry  `json:"container_logs,omitempty"`
}
```

**After**:
```go
type FullTelemetryData struct {
    // ... existing fields ...
    CronJobs       *CronInfo            `json:"cron_jobs,omitempty"`
    ContainerLogs  []ContainerLogEntry  `json:"container_logs,omitempty"`
    GPUTelemetry   *GPUTelemetry        `json:"gpu_telemetry,omitempty"`
}
```

### New event types (`internal/models/events.go`)

```go
EventGPUTemp   EventType = "gpu_temp"
EventGPUMetric EventType = "gpu_metric"
```

### Database changes

**None.** GPU telemetry is stored inside the existing `telemetry_snapshots.data` JSONB column. No migration required.

---

## 6. API / Interface Contract

### No new API endpoints

GPU telemetry is served through the existing endpoints:

- `POST /api/v1/devices/{id}/telemetry` -- agent pushes telemetry (already accepts any `FullTelemetryData` shape)
- `GET /api/v1/devices/{id}` -- returns latest telemetry snapshot including `gpu_telemetry` field
- WebSocket broadcast -- `FullTelemetryData` now includes `gpu_telemetry`

### Modified response shape

The `gpu_telemetry` field appears in all responses that include `FullTelemetryData`:

```json
{
  "gpu_telemetry": {
    "gpus": [
      {
        "index": 0,
        "name": "NVIDIA GeForce RTX 3090",
        "uuid": "GPU-12345678-abcd-efgh-ijkl-123456789012",
        "pci_bus_id": "00000000:01:00.0",
        "temperature_c": 72,
        "fan_speed_percent": 65,
        "utilization_pct": 85,
        "mem_util_pct": 42,
        "mem_used_mib": 10240,
        "mem_total_mib": 24576,
        "power_draw_w": 285.5,
        "power_limit_w": 350.0
      }
    ]
  }
}
```

When no GPU data is available, the field is omitted from JSON (due to `omitempty`).

### Alert templates endpoint

`GET /api/v1/settings/alert-templates` now includes two additional templates:

```json
[
  {
    "id": "gpu_temp_warn",
    "name": "GPU Temperature Warning",
    "category": "gpu",
    "metric": "gpu_temp",
    "operator": ">",
    "threshold": 80,
    "severity": "warning",
    "cooldown_seconds": 3600,
    "needs_target_name": false,
    "description": "Fires when any GPU temperature exceeds the threshold"
  },
  {
    "id": "gpu_temp_crit",
    "name": "GPU Temperature Critical",
    "category": "gpu",
    "metric": "gpu_temp",
    "operator": ">",
    "threshold": 90,
    "severity": "critical",
    "cooldown_seconds": 1800,
    "needs_target_name": false,
    "description": "Fires when any GPU temperature exceeds a critical threshold"
  }
]
```

---

## 7. Sequence / Flow

### Telemetry collection flow

1. Agent's `telemetryLoop` tick fires.
2. `collectAll` iterates registered collectors.
3. `GPUCollector.Collect(ctx)` is called:
   a. If `runtime.GOOS != "linux"`, return `&GPUTelemetry{}`, nil.
   b. `exec.LookPath("nvidia-smi")` -- if not found, return `&GPUTelemetry{}`, nil.
   c. Execute `nvidia-smi --query-gpu=... --format=csv,noheader,nounits` with context.
   d. If command fails, log warning, return `&GPUTelemetry{}`, nil.
   e. Split output by newlines. For each non-empty line, split by `,`, trim fields.
   f. Parse each field, mapping `[Not Supported]`/`N/A`/empty to nil pointers.
   g. Append `GPUDeviceMetrics` to slice.
   h. Return `&GPUTelemetry{GPUs: gpuSlice}`, nil.
4. Type-switch maps `*models.GPUTelemetry` to `data.GPUTelemetry`.
5. Agent serializes `FullTelemetryData` to JSON and POSTs to server.
6. Server deserializes into `TelemetrySnapshot`, stores in DB.
7. Server calls `CheckTelemetryThresholds`, which now calls `CheckGPUAlerts` if `data.GPUTelemetry != nil`.
8. Server broadcasts via WebSocket.
9. Dashboard receives updated telemetry, GPU card re-renders.

### Alert evaluation flow (per-GPU)

1. `CheckGPUAlerts(ctx, deviceID, hostname, gpuTelemetry)` called.
2. If `gpuTelemetry` is nil or `GPUs` is empty, return immediately.
3. For each GPU in `gpuTelemetry.GPUs`:
   a. If `TemperatureC != nil`, call `evaluateMetric(ctx, deviceID, "gpu_temp", float64(*gpu.TemperatureC), hostname, EventGPUTemp, msgFn)` where `msgFn` includes GPU name and index.
   b. If `UtilizationPct != nil`, call `evaluateMetric(ctx, deviceID, "gpu_util_percent", float64(*gpu.UtilizationPct), hostname, EventGPUMetric, msgFn)`.
   c. If `MemUtilPct != nil`, call `evaluateMetric(ctx, deviceID, "gpu_mem_percent", float64(*gpu.MemUtilPct), hostname, EventGPUMetric, msgFn)`.
   d. If `PowerDrawW != nil`, call `evaluateMetric(ctx, deviceID, "gpu_power_watts", *gpu.PowerDrawW, hostname, EventGPUMetric, msgFn)`.
4. `evaluateMetric` handles rule matching, cooldown, event creation, and notification dispatch.

---

## 8. Acceptance Criteria Mapping

| AC ID | Fulfilled By | Test Strategy |
|-------|-------------|---------------|
| AC-001 | `gpu.go:Collect()`, `telemetry.go` type-switch, `telemetry.go:GPUTelemetry` field on `FullTelemetryData` | Unit: mock nvidia-smi output with 2 GPUs, verify all fields populated in returned struct. Unit: verify type-switch maps to `data.GPUTelemetry`. |
| AC-002 | `gpu.go:Collect()` LookPath guard | Unit: test with exec helper that simulates missing binary, verify empty result and nil error. |
| AC-003 | `gpu.go` CSV parsing loop | Unit: feed 4-line CSV, verify 4 `GPUDeviceMetrics` entries with distinct indices and UUIDs. |
| AC-004 | `gpu.go` parseField helpers | Unit: feed CSV with `[Not Supported]` in fan_speed column, verify `FanSpeedPercent` is nil, other fields populated. |
| AC-005 | `generator.go:CheckGPUAlerts`, `evaluateMetric` | Unit: create rule `gpu_temp > 80`, feed 3 GPUs at 65/85/70, verify event created with GPU 1 identification in message. |
| AC-006 | `generator.go:CheckGPUAlerts`, `evaluateMetric` | Unit: create rule `gpu_temp > 80`, feed 3 GPUs at 65/72/70, verify no event created. |
| AC-007 | `DeviceDetail.tsx` GPU card section | Frontend unit: render DeviceDetail with mock telemetry containing `gpu_telemetry`, verify card visible with GPU name, temp, utilization, memory, fan, power. |
| AC-008 | `DeviceDetail.tsx` conditional rendering | Frontend unit: render DeviceDetail with telemetry lacking `gpu_telemetry`, verify no GPU card in DOM. |
| AC-009 | `templates.go` `gpu_temp_warn` and `gpu_temp_crit` entries | Unit: call `AlertTemplates()`, verify templates with IDs `gpu_temp_warn` and `gpu_temp_crit` exist with category `gpu`. |
| AC-010 | `DeviceDetail.tsx` temperature color-coding ternary | Frontend unit: render GPU card with temps 45/75/92, verify green/amber/red CSS classes applied. |
| AC-011 | `doctor.go` `collectorDeps["gpu"]` and `allCollectors` slice | Unit: verify `collectorDeps` contains `"gpu": ["nvidia-smi"]`. Verify `allCollectors` includes `"gpu"`. |
| AC-012 | `collector.go:RegisterDefaultsWithDocker` does NOT register GPU by default; `FilterEnabled` controls activation | Unit: create registry with defaults, verify `gpu` is registered but filtered out when not in enabled list. (GPU must be registered so FilterEnabled can include it, but default configs don't list it.) |
| AC-013 | Existing telemetry pipeline (JSONB storage), `FullTelemetryData.GPUTelemetry` field with `json:"gpu_telemetry,omitempty"` | Integration: store snapshot with GPU data, retrieve via API, verify `gpu_telemetry` present in response JSON. |
| AC-014 | `DeviceDetail.tsx` alert button, `CreateAlertDialog.tsx` `METRIC_DEFAULTS["gpu_temp"]` | Frontend unit: click alert button on GPU card, verify `CreateAlertDialog` opens with `metric="gpu_temp"` and `severity="warning"`. |

---

## 9. Error Handling

| Failure Mode | Handling | HTTP Status / Log |
|-------------|----------|-------------------|
| `nvidia-smi` not in PATH | Return empty `GPUTelemetry`, no error | No log (silent, consistent with UPS pattern) |
| `nvidia-smi` command fails (exit code != 0) | Return empty `GPUTelemetry`, no error | `slog.Warn("gpu collector: nvidia-smi failed", "error", err)` |
| `nvidia-smi` hangs beyond context deadline | Context cancellation kills process | Standard collector timeout handling in `collectAll` |
| CSV line has fewer fields than expected | Skip that GPU, continue parsing others | `slog.Warn("gpu collector: unexpected field count", "line", lineNum)` |
| Numeric field fails to parse (not `[Not Supported]` but garbage) | Set field to nil, continue | No log (defensive parsing) |
| Empty nvidia-smi output (no GPUs detected) | Return `GPUTelemetry{GPUs: nil}` | No log |
| Non-Linux platform | Return empty `GPUTelemetry`, no error | No log |

---

## 10. Security Considerations

| Vector | Mitigation |
|--------|-----------|
| Command injection via nvidia-smi | The collector uses hardcoded arguments with no user input in the command. No interpolation risk. |
| nvidia-smi output parsing | CSV parsing uses `strings.Split` and `strings.TrimSpace`. No shell interpretation. Malformed output results in nil fields, not crashes. |
| GPU data in telemetry payload | GPU UUIDs are included in telemetry. These are hardware identifiers, not secrets. They flow through the existing authenticated API (X-rIOt-Key header). |
| Denial of service via large nvidia-smi output | A system with 8 GPUs produces ~12 fields x 8 = ~96 values. Well within the 4KB NFR-004 budget. No unbounded growth path. |

---

## 11. Performance Considerations

| Concern | Approach |
|---------|----------|
| nvidia-smi execution time | Typical execution: 50-200ms. NFR-001 allows 5 seconds. Context timeout from the telemetry cycle (default 60s) provides a hard upper bound. |
| Per-GPU alert evaluation | 8 GPUs x 4 metrics = 32 `evaluateMetric` calls per cycle. Each call does a linear scan of enabled rules (typically < 50 rules). This is negligible. |
| JSON payload size | 12 fields per GPU, 8 GPUs max target = ~3KB. Well within NFR-004's 4KB budget. |
| No new database queries | GPU data is stored in existing JSONB column. No new indexes needed. |
| No additional API calls | Dashboard reads GPU data from the same telemetry snapshot already fetched. WebSocket broadcast already sends the full `FullTelemetryData`. |

---

## 12. Implementation Notes for Engineers

### Collector implementation (`gpu.go`)

- The nvidia-smi query string must be exactly: `--query-gpu=index,name,uuid,temperature.gpu,fan.speed,utilization.gpu,utilization.memory,memory.used,memory.total,power.draw,power.limit,pci.bus_id --format=csv,noheader,nounits`
- Field order in the CSV matches the query order. Parse positionally (index 0 = index, index 1 = name, etc.).
- Use `exec.CommandContext(ctx, "nvidia-smi", args...)` to respect context cancellation.
- The collector struct should be `type GPUCollector struct{}` with no state (unlike HardwareCollector which has SMART caching). GPU metrics are always fresh.
- For the `LookPath` check, only check PATH (do not check hardcoded paths like hardware.go does for smartctl). nvidia-smi is typically in `/usr/bin/` which is always in PATH.

### Registration order (`collector.go`)

- Register `&GPUCollector{}` after `&CronCollector{}` at the end of `RegisterDefaultsWithDocker`. This ensures it appears last in the ordered collector list, which is correct since it's a new optional collector.

### Type-switch (`telemetry.go`)

- Add the case before the `[]models.LogEntry` case (which has side-effect logic). The GPU case is simple assignment:
  ```
  case *models.GPUTelemetry:
      data.GPUTelemetry = v
  ```

### Doctor (`doctor.go`)

- Add `"gpu": {"nvidia-smi"}` to `collectorDeps`.
- Add `"gpu"` to the `allCollectors` slice, at the end (after `"cron"`).

### Alert templates (`templates.go`)

- Add templates at the end of the slice, before the closing `}`.
- Both templates use `NeedsTargetName: false` because GPU alerts fire for any GPU on the device (not a named target).

### Frontend GPU card (`DeviceDetail.tsx`)

- Guard with `{tel?.gpu_telemetry?.gpus && tel.gpu_telemetry.gpus.length > 0 && (`. No `isEnabled('gpu')` check needed because the data's presence/absence is the gate (the server does not know which collectors are enabled on the agent).
- Temperature color: `gpu.temperature_c != null ? (gpu.temperature_c >= 90 ? 'text-red-400' : gpu.temperature_c >= 80 ? 'text-orange-400' : gpu.temperature_c >= 60 ? 'text-amber-400' : 'text-emerald-400') : 'text-gray-500'`
- Use `GaugeBar` for utilization and memory percentage. Use `InfoItem` for static values.
- For power draw, display as `{gpu.power_draw_w?.toFixed(1)} / {gpu.power_limit_w?.toFixed(0)} W`.
- One-click alert button: `onClick={() => setAlertDialog({ metric: 'gpu_temp', targetName: '' })}` with label "GPU Temp Alert".
- Use `scrollbar-thin` class on any overflow containers per project convention.

### CreateAlertDialog (`CreateAlertDialog.tsx`)

- Add to `METRIC_DEFAULTS`:
  ```
  gpu_temp: { operator: '>', threshold: 80, severity: 'warning', cooldown: 3600, hint: 'GPU temperature in C (applies to all GPUs on the device)' },
  ```
- Add to `metricLabels`:
  ```
  gpu_temp: 'GPU Temperature',
  ```

### TypeScript types (`models.ts`)

- Add `GPUDeviceMetrics` and `GPUTelemetry` interfaces matching the Go JSON tags.
- Add `gpu_telemetry?: GPUTelemetry` to `FullTelemetryData`.

### Testing

- **Go collector tests**: Use a helper function to inject fake nvidia-smi output (write to a temp script or mock exec). Test cases: normal 2-GPU output, single GPU with `[Not Supported]` fields, empty output, malformed lines.
- **Go alert tests**: Use the existing mock pattern from `generator_test.go`. Create mock alert rule repo, feed `GPUTelemetry` data, verify event creation.
- **Frontend tests**: Use the existing DeviceDetail test pattern. Mock API responses with and without `gpu_telemetry` data.

---

## 13. Definition of Done

- [ ] All component changes in Section 4 implemented
- [ ] All AC mappings in Section 8 have corresponding tests with AC ID references
- [ ] `go test ./...` passes with no new failures
- [ ] `cd web && npm run test:run` passes with no new failures
- [ ] `go vet ./...` reports no new issues
- [ ] No new linting errors in Go or TypeScript
- [ ] No new migrations (confirmed: GPU data uses existing JSONB column)
- [ ] GPU collector registered but not in default enabled list (BR-001)
- [ ] Doctor includes `nvidia-smi` dependency check for `gpu` collector
