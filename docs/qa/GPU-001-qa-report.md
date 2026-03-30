# QA Report
**Story ID:** GPU-001
**QA Engineer:** QA Agent
**Date:** 2026-03-30
**Verdict:** PASS WITH NOTES

---

## Test Run Summary

### Go (`go test ./... -count=1`)
- Total: all packages passing, 0 failing, 0 skipped
- Packages: 16 tested, 4 with no test files
- `go vet ./...`: clean, no output
- Flaky tests found: none (run twice, identical results)

### Frontend (`cd web && npm run test:run`)
- Test Files: 12 passed (12)
- Tests: 191 passed (191)
- Flaky tests found: none
- Pre-existing DOMException AbortError output at teardown: confirmed pre-existing, not caused by GPU-001, all assertions pass
- Pre-existing `act(...)` warning in AC-014 test: noted under Test Quality Findings

---

## AC Coverage Audit

| AC ID | Status | Tests Covering It | Gap Description |
|-------|--------|-------------------|-----------------|
| AC-001 | COVERED | `gpu_test.go:TestParseNvidiaSMIOutput_TwoGPUs_AllFieldsPopulated` | — |
| AC-002 | COVERED | `gpu_test.go:TestGPUCollector_ReturnsEmptyWhenNoNvidiaSMI` | — |
| AC-003 | COVERED | `gpu_test.go:TestParseNvidiaSMIOutput_FourGPUs_DistinctIndicesAndUUIDs` | — |
| AC-004 | COVERED | `gpu_test.go:TestParseNvidiaSMIOutput_NotSupportedField_IsNil`, `TestParseNvidiaSMIOutput_NAFields_AreNil` | — |
| AC-005 | COVERED | `gpu_alerts_test.go:TestCheckGPUAlerts_AC005_FiresWhenOneGPUExceedsThreshold` | — |
| AC-006 | COVERED | `gpu_alerts_test.go:TestCheckGPUAlerts_AC006_NoFireWhenAllBelowThreshold` | — |
| AC-007 | COVERED | `DeviceDetail.test.tsx:[AC-007] renders GPU Telemetry section...` | — |
| AC-008 | COVERED | `DeviceDetail.test.tsx:[AC-008]` (2 cases: absent field, empty gpus array) | — |
| AC-009 | COVERED | `gpu_alerts_test.go:TestAlertTemplates_GPUTemplatesPresent` | — |
| AC-010 | COVERED | `DeviceDetail.test.tsx:[AC-010]` (4 cases: 45C/75C/85C/92C) | — |
| AC-011 | COVERED | `doctor_test.go:TestDoctor_CollectorDeps_GPUEntry`, `TestDoctor_AllCollectors_IncludesGPU` | — |
| AC-012 | COVERED | `gpu_test.go:TestGPUCollector_FilterEnabled_ExcludesGPUByDefault`, `TestGPUCollector_RegistrationName` | — |
| AC-013 | COVERED | `device_handler_test.go:TestGetDevice_GPUTelemetryInResponse` (added by QA) | Was MISSING before QA pass — see Tests Added by QA |
| AC-014 | COVERED | `DeviceDetail.test.tsx:[AC-014] clicking GPU Temp Alert button...` | — |

---

## Test Quality Findings

### TQF-001: SEC-001 cap test does not actually assert the cap boundary

**File:** `internal/server/events/gpu_alerts_test.go:181-205`

`TestCheckGPUAlerts_SliceCappedAt32` builds a slice of 50 GPUs all above threshold and asserts `len(events) <= 1`. However, this assertion holds regardless of whether the cap exists. Because all 50 GPUs share the same rule ID (1), the cooldown mechanism limits events to 1 even if all 50 are iterated. Removing the `gpus = gpus[:maxGPUs]` line from `CheckGPUAlerts` would not cause this test to fail.

The cap IS correctly implemented in `generator.go:870-872`. The test demonstrates the cap does not panic and does not produce unbounded events, but it does not prove that only 32 GPUs were evaluated. This is a brittle coverage claim for SEC-001.

**Impact:** Non-blocking. The cap is correctly in the code and the test provides regression protection against a panic. A stronger test would use distinct rule IDs per GPU so cooldown does not suppress events, then assert `len(events) <= 32` with 50 input GPUs. This is flagged as a note for a follow-up improvement.

### TQF-002: AC-014 frontend test uses `.click()` not `userEvent.click()`

**File:** `web/src/pages/DeviceDetail.test.tsx:635`

The test uses `alertBtn.click()` (raw DOM click) instead of `userEvent.click()` from `@testing-library/user-event`. This produces a React `act(...)` warning in the test output:

```
An update to DeviceDetail inside a test was not wrapped in act(...).
```

The test still passes and the assertion is valid, but the warning indicates that state updates triggered by the click are not being properly flushed before assertions are checked. Testing Library's `userEvent.click()` wraps interactions in `act` automatically. This is a pre-existing pattern issue in the test suite (not unique to GPU-001) and is non-blocking.

### TQF-003: `TestDoctor_AllCollectors_IncludesGPU` tests a local copy of the slice, not the live code

**File:** `internal/agent/doctor_test.go:21-37`

The test replicates the `allCollectors` slice literal inline rather than calling `Doctor()` and inspecting its output. If `doctor.go` is modified to rename or remove `"gpu"` from the slice, this test would NOT catch the divergence — it would still pass because the test is asserting on its own local copy. The implementation report notes this limitation inline.

The real value is in `TestDoctor_CollectorDeps_GPUEntry`, which directly exercises the exported `collectorDeps` map variable and would catch a deletion.

**Impact:** Non-blocking. The `doctor.go` source shows `"gpu"` is present in the slice at line 115. The test is acknowledged as a structural limitation by the engineering team.

---

## Adversarial Findings

### AF-001: SEC-001 cap only applies at alert evaluation, not at deserialization

The security review required the GPU count to be bounded at the server-side processing layer. The implementation caps the slice at 32 in `CheckGPUAlerts` only. A payload with 1,000,000 GPU entries would still be deserialized into memory in full by `json.NewDecoder(r.Body).Decode(&snap)` in the telemetry handler before `CheckGPUAlerts` is called. The cap prevents the database query amplification (SEC-003) but does not prevent memory exhaustion from deserialization (SEC-001's original concern about OOM).

The security review's "Required Resolution" stated: "add a GPU count cap during deserialization/processing" and "the telemetry handler should enforce a request body size limit via http.MaxBytesReader." Only the alert-evaluation-time cap was implemented; the body size limit on the telemetry handler was not.

**Assessment:** The implementation satisfies the letter of the security review's minimum requirement for SEC-001 ("The QA engineer must verify that the GPUs slice is bounded to a reasonable maximum during server-side processing or alert evaluation") because the cap occurs before alert evaluation. However, the memory exhaustion vector via deserialization remains open. This is a pre-existing systemic gap (the handler has no MaxBytesReader for any collector's data) and the security review labeled the body size limit as a systemic fix, not a GPU-001 blocker. This finding is non-blocking for GPU-001 but is documented for awareness.

**No test written** — this requires a real HTTP request with a multi-MB body and is outside the scope of unit tests. Recommend adding `http.MaxBytesReader` to the telemetry handler as a follow-up hardening task.

### AF-002: Alert button is positioned at section level, not per-GPU temperature row

**File:** `web/src/pages/DeviceDetail.tsx:878-885`

The FRD AC-014 states: "When the user clicks the alert icon **next to a GPU temperature reading**". The implementation renders a single "GPU Temp Alert" button at the bottom of the entire GPU Telemetry card section, not adjacent to each GPU's temperature value. For a device with 4 GPUs, there is still only one alert button.

This is a UX deviation from the FRD wording. However:
- The ADD implementation notes (Section 12) specify `onClick={() => setAlertDialog({ metric: 'gpu_temp', targetName: '' })}` as a single button at the section level, not per-GPU
- The alert creates a device-wide rule (not GPU-specific), consistent with `NeedsTargetName: false` in the templates
- The test for AC-014 passes against this implementation

**Assessment:** The ADD and FRD are slightly inconsistent here. The FRD wording implies per-GPU alert buttons; the ADD explicitly specifies a single section-level button. The implementation follows the ADD. This is a documentation inconsistency, not an implementation bug. The AC-014 acceptance criterion is satisfied by the implementation (dialog opens pre-filled with `gpu_temp`). Flagged as a note.

---

## Tests Added by QA

| File | Description | Covers |
|------|-------------|--------|
| `internal/server/handlers/device_handler_test.go` | `TestGetDevice_GPUTelemetryInResponse` — stores a snapshot with GPU telemetry, calls `GET /api/v1/devices/{id}`, asserts that the response JSON contains `latest_telemetry.data.gpu_telemetry.gpus` with the correct GPU name and index | AC-013 |

---

## Deviations from ADD

### Deviation 1: Parsing functions extracted to `gpu_parse.go` (approved)
The ADD specified a single `gpu.go`. The engineer split into `gpu.go` (Linux, build-tagged), `gpu_other.go` (non-Linux stub), and `gpu_parse.go` (parsing, no build tag). This is structurally correct and improves testability on all CI platforms including Windows. No behavior change.

### Deviation 2: `doctor_test.go` created (approved)
The ADD did not list a `doctor_test.go` in Section 4. The file was created to cover AC-011. This is an additive deviation with no negative consequences.

---

## Deviations from FRD

### Deviation 1: Single alert button at section level vs. per-GPU (see AF-002)
The FRD AC-014 says "next to a GPU temperature reading." The ADD implementation notes specify a single section-level button. The implementation follows the ADD. The AC-014 acceptance criterion (dialog opens with `gpu_temp` pre-filled) is satisfied. This is a minor UX deviation from the FRD wording, not a functional violation.

---

## Verdict Rationale

**PASS WITH NOTES:**

All 14 ACs are now covered (AC-013 was MISSING before QA added `TestGetDevice_GPUTelemetryInResponse`; that test now passes green). No tests fail. No implementation deviates from the FRD in a functional sense. The following minor quality issues exist but are non-blocking:

1. **TQF-001:** The SEC-001 cap test does not falsify the cap boundary — the cap is correctly implemented in code but the test would pass even without it. Recommend strengthening in a follow-up.
2. **TQF-002:** AC-014 frontend test uses raw `.click()` producing an `act(...)` warning. Non-blocking, pre-existing pattern in the test suite.
3. **TQF-003:** `TestDoctor_AllCollectors_IncludesGPU` tests a local slice copy rather than live code. Non-blocking.
4. **AF-001:** The telemetry handler lacks `http.MaxBytesReader` — the GPU count cap in alert evaluation does not prevent memory exhaustion from deserialization of a malformed payload. Pre-existing systemic gap; recommend as a follow-up hardening task.
5. **AF-002:** Single alert button at section level vs. per-GPU per FRD wording. The ADD implementation notes resolve the ambiguity in favor of the section-level button; the AC is satisfied.

The story may ship. The items above should be tracked as follow-up work, not blockers.
