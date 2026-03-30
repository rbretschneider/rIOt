# Implementation Report

| Field      | Value                                      |
|------------|--------------------------------------------|
| Story ID   | GPU-001                                    |
| Title      | GPU Telemetry Collector with Alert Support |
| Engineer   | Senior Dev Agent                           |
| Date       | 2026-03-30                                 |
| Commit     | 4eaf6d6                                    |

---

## Detected Stack

| Layer     | Technology                                  |
|-----------|---------------------------------------------|
| Agent     | Go 1.23, module `github.com/DesyncTheThird/rIOt` |
| Server    | Go 1.23, chi v5 router, pgx v5              |
| Frontend  | React 18, TypeScript, Tailwind CSS, Vite    |
| Test (Go) | `go test ./...`, testify v1                 |
| Test (Web)| Vitest, @testing-library/react              |

---

## Completed Components

| File | Action | Notes |
|------|--------|-------|
| `internal/agent/collectors/gpu.go` | CREATE | Linux-only build tag; invokes nvidia-smi with `exec.CommandContext`; delegates to `parseNvidiaSMIOutput` |
| `internal/agent/collectors/gpu_other.go` | CREATE | Non-Linux stub; always returns empty `GPUTelemetry` |
| `internal/agent/collectors/gpu_parse.go` | CREATE | Parsing helpers with no build tag — enables unit testing on all platforms |
| `internal/agent/collectors/gpu_test.go` | CREATE | Unit tests for parsing, sentinel handling, multi-GPU, filter |
| `internal/agent/collectors/collector.go` | MODIFY | Added `r.Register(&GPUCollector{})` at end of `RegisterDefaultsWithDocker` |
| `internal/agent/telemetry.go` | MODIFY | Added `case *models.GPUTelemetry: data.GPUTelemetry = v` to type-switch |
| `internal/agent/doctor.go` | MODIFY | Added `"gpu": {"nvidia-smi"}` to `collectorDeps`; added `"gpu"` to `allCollectors` slice |
| `internal/agent/doctor_test.go` | CREATE | Tests for `collectorDeps` and `allCollectors` GPU entries |
| `internal/models/telemetry.go` | MODIFY | Added `GPUDeviceMetrics`, `GPUTelemetry` structs; `GPUTelemetry *GPUTelemetry` field on `FullTelemetryData` |
| `internal/models/events.go` | MODIFY | Added `EventGPUTemp` and `EventGPUMetric` constants |
| `internal/server/events/generator.go` | MODIFY | Added `CheckGPUAlerts` method; called it from `CheckTelemetryThresholds` |
| `internal/server/events/gpu_alerts_test.go` | CREATE | Unit tests for all GPU alert metrics, per-GPU evaluation, cooldown, nil safety, SEC-001 cap |
| `internal/server/events/templates.go` | MODIFY | Added `gpu_temp_warn` and `gpu_temp_crit` templates in `gpu` category |
| `web/src/types/models.ts` | MODIFY | Added `GPUDeviceMetrics`, `GPUTelemetry` interfaces; `gpu_telemetry?: GPUTelemetry` on `FullTelemetryData` |
| `web/src/pages/DeviceDetail.tsx` | MODIFY | Added GPU Telemetry card with per-GPU sub-sections, temperature color-coding, GaugeBar for utilization and memory, fan speed, power draw, one-click alert button |
| `web/src/components/CreateAlertDialog.tsx` | MODIFY | Added `gpu_temp` to `METRIC_DEFAULTS` and `metricLabels` |
| `web/src/pages/DeviceDetail.test.tsx` | MODIFY | Added 8 GPU tests for AC-007, AC-008, AC-010, AC-014 |

---

## Security Condition Addressed

**SEC-001** (HIGH): The `GPUs` slice is capped at `maxGPUs = 32` at the top of `CheckGPUAlerts` before any iteration occurs. This prevents a compromised agent sending a pathological payload from triggering unbounded database queries. The cap covers SEC-003 (alert evaluation amplification) as a consequence.

**SEC-002** (MEDIUM, no code change required): GPU name, UUID, and PCI bus ID are rendered via standard React JSX interpolation only (`{gpu.name}`, `{gpu.uuid}`, `{gpu.pci_bus_id}`). No `dangerouslySetInnerHTML` or URI construction from these values. React's auto-escaping provides the XSS protection.

---

## Test Summary

### AC Coverage

| AC ID  | Test File(s) | Test Count | Status |
|--------|-------------|------------|--------|
| AC-001 | `gpu_test.go` | `TestParseNvidiaSMIOutput_TwoGPUs_AllFieldsPopulated` | PASS |
| AC-002 | `gpu_test.go` | `TestGPUCollector_ReturnsEmptyWhenNoNvidiaSMI` | PASS |
| AC-003 | `gpu_test.go` | `TestParseNvidiaSMIOutput_FourGPUs_DistinctIndicesAndUUIDs` | PASS |
| AC-004 | `gpu_test.go` | `TestParseNvidiaSMIOutput_NotSupportedField_IsNil`, `TestParseNvidiaSMIOutput_NAFields_AreNil` | PASS |
| AC-005 | `gpu_alerts_test.go` | `TestCheckGPUAlerts_AC005_FiresWhenOneGPUExceedsThreshold` | PASS |
| AC-006 | `gpu_alerts_test.go` | `TestCheckGPUAlerts_AC006_NoFireWhenAllBelowThreshold` | PASS |
| AC-007 | `DeviceDetail.test.tsx` | `[AC-007] renders GPU Telemetry section...` | PASS |
| AC-008 | `DeviceDetail.test.tsx` | `[AC-008] does not render...` (2 cases) | PASS |
| AC-009 | `gpu_alerts_test.go` | `TestAlertTemplates_GPUTemplatesPresent` | PASS |
| AC-010 | `DeviceDetail.test.tsx` | `[AC-010] GPU temperature color-coding` (4 cases) | PASS |
| AC-011 | `doctor_test.go` | `TestDoctor_CollectorDeps_GPUEntry`, `TestDoctor_AllCollectors_IncludesGPU` | PASS |
| AC-012 | `gpu_test.go` | `TestGPUCollector_FilterEnabled_ExcludesGPUByDefault`, `TestGPUCollector_RegistrationName` | PASS |
| AC-013 | Covered by model field addition + existing telemetry pipeline tests | No new integration test needed — data flows through unmodified JSONB path | PASS |
| AC-014 | `DeviceDetail.test.tsx` | `[AC-014] clicking GPU Temp Alert button...` | PASS |

### Test Run Output

#### Go (`go test ./...`)

```
?     github.com/DesyncTheThird/rIOt/cmd/riot-agent      [no test files]
?     github.com/DesyncTheThird/rIOt/cmd/riot-server     [no test files]
ok    github.com/DesyncTheThird/rIOt/internal/agent
ok    github.com/DesyncTheThird/rIOt/internal/agent/collectors
ok    github.com/DesyncTheThird/rIOt/internal/models
ok    github.com/DesyncTheThird/rIOt/internal/resilient
ok    github.com/DesyncTheThird/rIOt/internal/server
ok    github.com/DesyncTheThird/rIOt/internal/server/auth
ok    github.com/DesyncTheThird/rIOt/internal/server/ca
?     github.com/DesyncTheThird/rIOt/internal/server/db  [no test files]
ok    github.com/DesyncTheThird/rIOt/internal/server/events
ok    github.com/DesyncTheThird/rIOt/internal/server/handlers
?     github.com/DesyncTheThird/rIOt/internal/server/logstore [no test files]
ok    github.com/DesyncTheThird/rIOt/internal/server/middleware
ok    github.com/DesyncTheThird/rIOt/internal/server/notify
ok    github.com/DesyncTheThird/rIOt/internal/server/probes
ok    github.com/DesyncTheThird/rIOt/internal/server/scoring
ok    github.com/DesyncTheThird/rIOt/internal/server/updates
ok    github.com/DesyncTheThird/rIOt/internal/server/websocket
?     github.com/DesyncTheThird/rIOt/internal/testutil   [no test files]
```

`go vet ./...` — no output (clean).

#### Frontend (`cd web && npm run test:run`)

```
Test Files  12 passed (12)
Tests       191 passed (191)
```

(Was 183 before this story; 8 new tests added.)

---

## Deviations from ADD

### Deviation 1: Parsing functions extracted to `gpu_parse.go`

The ADD specified a single `gpu.go` file with a Linux build tag. To allow unit-testing of the CSV parsing logic on all platforms (including Windows CI), the parsing functions (`parseNvidiaSMIOutput`, `parseIntField`, `parseFloat64Field`, `isAbsent`) were extracted to `gpu_parse.go` with no build tag. The Linux-only `gpu.go` contains only the `GPUCollector` struct and its `Collect` method. The non-Linux stub is in `gpu_other.go`.

This is a structural split with no behavior change. The ADD's intent (Linux-only exec, platform-independent parsing) is fully preserved.

### Deviation 2: `doctor_test.go` is a new file not in ADD Section 4

The ADD lists `doctor.go` as modified but does not list a `doctor_test.go`. A separate test file was created to cover AC-011 (`collectorDeps` and `allCollectors` containing "gpu"). This is an additive deviation that satisfies the AC coverage mandate without modifying any file outside the ADD scope.

---

## Notes for QA

1. **nvidia-smi not present**: On any host without an NVIDIA GPU (or without the NVIDIA driver installed), the `gpu` collector silently returns empty. The `gpu_telemetry` field will be absent from the telemetry JSON. The GPU card will not appear in the dashboard. This is the intended behavior per FR-003, FR-008, AC-002.

2. **Adding gpu to agent config**: To enable GPU collection on a device, the user must add `gpu` to `collectors.enabled` in `/etc/riot/agent.yaml` and restart the agent. This is consistent with BR-001 and should be verified end-to-end on a host with an NVIDIA GPU.

3. **Alert templates**: The two new templates (`gpu_temp_warn`, `gpu_temp_crit`) appear in `GET /api/v1/settings/alert-templates` under category `"gpu"`. QA should verify they appear in the "Create from Template" UI in Settings > Alert Rules.

4. **SEC-001 cap at 32**: `CheckGPUAlerts` caps the GPU slice at 32 before iteration. Any device reporting more than 32 GPUs will have only the first 32 evaluated for alerts. This is intentional and documented. Real hardware configurations with 8 GPUs are well within this limit.

5. **Temperature color thresholds**: green < 60°C, amber 60–79°C, orange 80–89°C, red ≥ 90°C. These are hardcoded in the JSX ternary chain per AD-008.

6. **No new migrations**: Confirmed — GPU data flows through the existing `telemetry_snapshots.data` JSONB column. No migration files were created or modified.

7. **Cross-compilation**: The `//go:build linux` tag on `gpu.go` and `//go:build !linux` on `gpu_other.go` ensure the agent binary compiles cleanly on all 8 target platforms in the release workflow.
