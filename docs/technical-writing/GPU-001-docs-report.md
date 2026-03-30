# Documentation Report
**Story ID:** GPU-001
**Author:** Technical Writer Agent
**Date:** 2026-03-30

---

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | Added `gpu` to Features bullet and collectors table; updated whitelist note; added GPU Monitoring section with requirements, enabling instructions, field table, relationship to `hardware` collector, and GPU alert reference; added GPU to One-Click Alert Creation bullet; added GPU Monitoring anchor link from collector table |
| `CHANGELOG.md` | Updated | Added five [GPU-001] entries under [Unreleased] > Added covering the collector, dashboard card, alert templates, alert metrics, and doctor check |

---

## Stale Content Found (Not Fixed)

Items found outside the scope of this story that need a documentation pass:

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` line 18 | States "README last updated for v2.38.0" — version marker will be wrong after the next release | Update version marker when next release is tagged; consider whether this marker adds value given the git log already tracks this |
| `README.md` Features bullet (temperature monitoring) | States "CPU and disk drive temperatures" — does not mention GPU temperatures now that the `gpu` collector exists | Extend to mention GPU temperatures after GPU-001 ships |

---

## Accuracy Flags

Discrepancies found between the ADD/FRD and what the code actually does:

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| `gpu_parse.go` line 44 emits `slog.Warn("gpu collector: unexpected field count", ...)` when `strconv.Atoi` fails on the index field. The error condition is a non-integer index value, not a field count mismatch. This is a misleading log message but does not affect behavior. | `internal/agent/collectors/gpu_parse.go:44` | Not documented in user-facing docs (log message is developer-facing). Flagged here for the engineering team. |
| The QA report (AF-002) notes the FRD AC-014 says "next to a GPU temperature reading" but the implementation places a single "GPU Temp Alert" button at the bottom of the GPU Telemetry card section, covering all GPUs on the device. The ADD implementation notes (Section 12) explicitly specify this single-button design. The README and GPU Monitoring section document the button correctly as creating a device-wide rule, which matches the implemented behavior. | `web/src/pages/DeviceDetail.tsx:878-885` | Documented as device-wide rule in the GPU Monitoring > GPU Alerts table note |

---

## Inline Comment Assessment

All changed source files were reviewed for comment quality. No additions or deletions were required:

- `internal/agent/collectors/gpu.go` — struct and method comments are accurate and non-redundant
- `internal/agent/collectors/gpu_other.go` — stub comment is accurate
- `internal/agent/collectors/gpu_parse.go` — function doc comments explain the 12-field CSV layout, sentinel handling, and nil semantics correctly; field-position comments on the parse loop are accurate
- `internal/agent/collectors/collector.go` — no new comments needed; `GPUCollector` registration is self-evident
- `internal/agent/doctor.go` — `collectorDeps` map comment is sufficient
- `internal/models/telemetry.go` — struct comments for `GPUDeviceMetrics` and `GPUTelemetry` are accurate
- `internal/models/events.go` — new constants follow the existing unadorned constant pattern; no comments needed
- `internal/server/events/generator.go` — `CheckGPUAlerts` block comment correctly references FR-019 and SEC-001 with the cap rationale; field-level nil checks are self-evident
- `internal/server/events/templates.go` — templates are data declarations; no comments needed
- `web/src/types/models.ts` — TypeScript interfaces mirror the Go JSON tags; no comments needed
- `web/src/components/CreateAlertDialog.tsx` — hint string in `METRIC_DEFAULTS["gpu_temp"]` accurately describes the metric scope
- `web/src/pages/DeviceDetail.tsx` — GPU Telemetry section follows the same inline pattern as UPS and other hardware sections; temperature ternary logic is clear from the threshold values

---

## Notes for Future Writers

**GPU collector is not in the default config.** This is the third collector (after `hardware` and `container_logs`) that requires manual addition to `collectors.enabled`. Every time a new collector is added with this requirement, both the collectors table note in README and the CLAUDE.md memory file (`Agent Collector Whitelist` section) must be updated. This story updated both.

**The `gpu` and `hardware` collectors cover different data for the same hardware.** `hardware` provides static GPU identity from sysfs/DRM (vendor, model, VRAM, PCI slot, driver). `gpu` provides runtime metrics from `nvidia-smi` (temperature, utilization, power). Both appear on the device detail page but in separate sections: Hardware Details (static) and GPU Telemetry (runtime). Future documentation should maintain this distinction clearly.

**GPU alert rules are device-wide, not per-GPU.** A single alert rule for `gpu_temp` applies to all GPUs on the device. The alert engine evaluates each GPU independently against that rule and fires a separate event per GPU that exceeds the threshold. The event message identifies the GPU. This design means users cannot currently scope an alert to a specific GPU by index or name. This is a deliberate scope decision from the FRD and should be noted if someone asks about per-GPU alert targeting.

**The SEC-001 gap (no `http.MaxBytesReader` on the telemetry handler) is a pre-existing systemic issue** flagged in both the security review and QA report. If a future story adds request body size limiting to the telemetry handler, the README should be checked for any deployment guidance that might be affected (currently none).

**Temperature color thresholds differ between GPU and disk SMART.** GPU card: green < 60°C, amber 60–79°C, orange 80–89°C, red ≥ 90°C. Disk SMART card uses different ranges. Both are hardcoded in `DeviceDetail.tsx` via inline ternary chains. A future writer documenting temperature display should not assume the thresholds are shared.
