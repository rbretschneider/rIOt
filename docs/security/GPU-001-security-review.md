# Security Review Report
**Story ID:** GPU-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-03-30
**Verdict:** APPROVED WITH CONDITIONS

---

## Threat Model Summary

### Assets
- **Telemetry data integrity**: GPU metrics (temperature, utilization, memory, power) stored in the existing JSONB telemetry column. Corruption or fabrication could trigger false alerts or mask real thermal events.
- **Agent host stability**: The agent shells out to `nvidia-smi`. Misbehavior in parsing or process management could affect the agent daemon.
- **Alert engine correctness**: GPU metrics feed into threshold-based alert evaluation. Crafted values could cause spurious alerts or suppress legitimate ones.
- **GPU hardware identifiers**: GPU UUIDs and PCI bus IDs are hardware fingerprints. Not secrets, but inventory data that should remain within the authenticated telemetry pipeline.

### Threat Actors
- **Unauthenticated external attacker**: Cannot interact with the telemetry pipeline. The `POST /api/v1/devices/{id}/telemetry` endpoint requires a valid `X-rIOt-Key` header matched to the device ID (verified in `middleware/auth.go`). This actor is out of scope for this story's new surface.
- **Compromised agent / rogue device**: A device with a valid API key could send crafted GPU telemetry payloads to the server. This is the primary threat actor for server-side concerns.
- **Local attacker on monitored host**: Could manipulate `nvidia-smi` output (e.g., replace the binary, modify PATH). This is an existing risk pattern shared with `upsc`, `smartctl`, `getenforce`, etc. -- not new to this story.

### Attack Surface Introduced
- **New `exec.Command` invocation**: `nvidia-smi` with hardcoded arguments. No new user-controlled input to the command line.
- **New CSV parsing logic**: Parses stdout from `nvidia-smi`. New string-to-numeric conversion code.
- **New JSONB field (`gpu_telemetry`)**: Added to the existing `FullTelemetryData` struct. Deserialized from agent-supplied JSON on the server side.
- **New alert metrics** (`gpu_temp`, `gpu_util_percent`, `gpu_mem_percent`, `gpu_power_watts`): Four new metric names in the alert evaluation path.
- **New event types** (`gpu_temp`, `gpu_metric`): Two new event type constants for the event log.
- **Frontend rendering of agent-supplied strings**: GPU name, UUID, PCI bus ID rendered in the dashboard.

---

## Findings

### HIGH

#### SEC-001: No request body size limit on telemetry ingestion endpoint
**Severity:** HIGH
**Domain:** Resource Exhaustion / Denial of Service
**Location:** `internal/server/handlers/handlers.go:301-306` (Telemetry handler), `internal/server/middleware/`
**Description:**
The telemetry handler calls `json.NewDecoder(r.Body).Decode(&snap)` with no body size limit. There is no `http.MaxBytesReader` wrapper and no middleware enforcing a maximum request body size anywhere in the server. While this is a pre-existing condition (not introduced by GPU-001), the GPU story adds another unbounded data structure (`GPUTelemetry` with a `[]GPUDeviceMetrics` slice) to the payload. A compromised agent could send a telemetry payload with a `gpu_telemetry.gpus` array containing millions of entries, exhausting server memory during JSON deserialization and during the subsequent `json.Marshal` for JSONB storage.

This is not unique to GPU data -- the same attack is possible today via `services`, `container_logs`, `usb.devices`, or any other slice field. However, the GPU story is an opportunity to flag this systemic issue, and the ADD should acknowledge it.

**Attack Scenario:**
1. Attacker compromises an agent or obtains a valid API key.
2. Attacker sends a POST to `/api/v1/devices/{id}/telemetry` with a JSON body containing `"gpu_telemetry": {"gpus": [<1,000,000 entries>]}`.
3. Server allocates memory proportional to the array size during `json.Decode`.
4. Server may OOM or become unresponsive, affecting all monitored devices.

**Required Resolution:**
The ADD should specify a maximum count for the `GPUs` slice during server-side processing (e.g., cap at 16 or 32 GPUs, which covers any realistic hardware configuration). Additionally, the telemetry handler should enforce a request body size limit via `http.MaxBytesReader`. The body size limit is a systemic fix that benefits all collectors, not just GPU. The per-GPU-count cap is specific to this story.

**Blocks:** Implementation

---

### MEDIUM

#### SEC-002: Frontend rendering of agent-supplied strings without explicit sanitization consideration
**Severity:** MEDIUM
**Domain:** Data Exposure / XSS
**Location:** ADD Section 12 (Frontend GPU card in `DeviceDetail.tsx`)
**Description:**
The GPU card renders `gpu.name`, `gpu.uuid`, and `gpu.pci_bus_id` directly from telemetry data. These strings originate from `nvidia-smi` output on the agent, which is then transmitted as JSON to the server and stored in JSONB. React's JSX escapes interpolated values by default, which prevents XSS in standard rendering paths. However, the ADD does not explicitly acknowledge this dependency on React's auto-escaping, and does not specify that these values must never be rendered via `dangerouslySetInnerHTML` or inserted into DOM attributes that bypass escaping (e.g., `href`).

**Attack Scenario:**
1. A compromised agent sends a GPU name containing `<script>alert(1)</script>` or a javascript: URI.
2. If a future refactor renders this value via `dangerouslySetInnerHTML` or uses it in an `href` attribute, XSS would occur.
3. Under current React JSX interpolation, this is safe. The risk is that the safety is implicit rather than documented.

**Required Resolution:**
The ADD should note that GPU name, UUID, and PCI bus ID are agent-supplied strings and must only be rendered via standard React JSX interpolation (which auto-escapes). No `dangerouslySetInnerHTML` or URI construction from these values. This is a documentation/awareness item for the engineering team, not a code change.

**Blocks:** No (defense-in-depth, covered by React's default behavior)

---

#### SEC-003: Alert evaluation performs unbounded iteration over GPUs slice
**Severity:** MEDIUM
**Domain:** Resource Exhaustion
**Location:** ADD Section 7 (Alert evaluation flow), ADD Section 12 (`CheckGPUAlerts` method)
**Description:**
The proposed `CheckGPUAlerts` method iterates over every GPU in `GPUTelemetry.GPUs` and calls `evaluateMetric` for up to 4 metrics per GPU. `evaluateMetric` calls `alertRuleRepo.ListEnabled(ctx)` internally (via `findMatchingRule`). If the `GPUs` slice is not bounded before reaching alert evaluation, a payload with N GPUs results in up to 4N database queries to the alert rules table. With the pre-existing pattern (e.g., `CheckDiskSmartAlerts` iterates over all drives), this is consistent but still worth noting.

Combined with SEC-001, if the GPU count is unbounded at the deserialization layer, this becomes a database query amplification vector.

**Attack Scenario:**
1. Compromised agent sends telemetry with 10,000 GPU entries.
2. Alert evaluation calls `findMatchingRule` up to 40,000 times.
3. Each call queries the alert rules table, causing database load spike.

**Required Resolution:**
This is mitigated if SEC-001's GPU count cap is implemented. The ADD should specify that `CheckGPUAlerts` must validate the length of `GPUs` before iteration, or rely on the deserialization-time cap from SEC-001. Either approach is acceptable; the requirement is that the iteration is bounded.

**Blocks:** No (mitigated by SEC-001 resolution)

---

### LOW / INFORMATIONAL

#### SEC-004: nvidia-smi command injection risk is properly mitigated
**Severity:** LOW (Informational -- no finding, positive confirmation)
**Domain:** Command Injection
**Location:** ADD Section 3 (AD-001), ADD Section 10
**Description:**
The ADD correctly specifies that `nvidia-smi` is invoked with entirely hardcoded arguments via `exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=...", "--format=csv,noheader,nounits")`. Go's `exec.Command` does not invoke a shell and passes arguments as a string array, so there is no shell interpolation. No user-controlled input is incorporated into the command or its arguments. This matches the existing patterns in `ups.go` (calls `upsc` with discovered UPS name, but that name comes from `upsc -l` output, not user input) and `security.go` (all hardcoded commands).

The `exec.CommandContext` usage ensures the parent context's deadline will kill the process if `nvidia-smi` hangs, satisfying NFR-001.

No finding. This is correctly designed.

#### SEC-005: CSV parsing failure mode is safe
**Severity:** LOW (Informational)
**Domain:** Input Validation
**Location:** ADD Section 3 (AD-004), ADD Section 9
**Description:**
The ADD specifies that parsing failures (unexpected field count, unparseable numeric values, `[Not Supported]`/`N/A` sentinel values) result in nil pointer fields or skipped GPU entries, never in errors that halt the collector. This is defensive and appropriate for telemetry. The `strings.Split` and `strings.TrimSpace` approach does not involve shell interpretation or regex with catastrophic backtracking.

One minor note: the ADD does not specify a maximum line length when reading `nvidia-smi` output. In practice, `exec.CommandContext.Output()` reads the entire stdout into memory. A pathologically long output from a compromised `nvidia-smi` binary could consume agent memory. However, this is a local-attacker-on-the-agent-host scenario, and such an attacker already has more direct means of disruption. No actionable finding.

#### SEC-006: GPU UUID exposure is acceptable
**Severity:** LOW (Informational)
**Domain:** Data Exposure
**Location:** ADD Section 10, FRD FR-004
**Description:**
GPU UUIDs (e.g., `GPU-12345678-abcd-efgh-ijkl-123456789012`) are hardware identifiers exposed in the telemetry payload and visible in the dashboard. The ADD correctly notes these are not secrets. They are similar to other hardware identifiers already in the system (MAC addresses in `NetworkInterface`, serial numbers in `SystemInfo`, PCI slot IDs in `HardwareInfo`). The telemetry pipeline is authenticated via `X-rIOt-Key`, so these identifiers are not exposed to unauthenticated users.

No finding.

#### SEC-007: No fallback thresholds for GPU metrics in evaluateMetric
**Severity:** LOW (Informational)
**Domain:** Business Logic
**Location:** `internal/server/events/generator.go:881-900` (evaluateMetric fallback switch)
**Description:**
The `evaluateMetric` function has hardcoded fallback thresholds for `mem_percent`, `disk_percent`, and `updates` metrics. For any other metric (including the new `gpu_temp`, `gpu_util_percent`, `gpu_mem_percent`, `gpu_power_watts`), the function returns early at the `default` case when no user-configured rules exist. This means GPU alerts will only fire when a user explicitly creates an alert rule (or uses a template). This is consistent with how disk SMART temperature alerts work and is the correct behavior per the FRD -- GPU alerting is opt-in via templates. No finding, but noted for awareness.

---

## Positive Observations

1. **No new API endpoints or database migrations.** The story reuses the existing telemetry pipeline end-to-end. This minimizes new attack surface.

2. **Hardcoded nvidia-smi arguments with no user input.** The command injection surface is zero. The ADD explicitly calls this out in Section 10.

3. **Pointer types for nullable fields.** Using `*int` and `*float64` with `omitempty` is the correct approach for handling `[Not Supported]` GPU fields. It avoids sentinel values that could be misinterpreted as real data.

4. **Consistent collector pattern.** Following the UPS collector pattern (empty result on missing binary, Linux-only guard, `exec.CommandContext` for timeout) means no new security patterns to audit -- the GPU collector inherits the safety properties of an established, reviewed pattern.

5. **Authentication on telemetry endpoint.** The `DeviceAuth` middleware validates the `X-rIOt-Key` header and confirms it matches the device ID in the URL path. This prevents unauthorized telemetry injection.

6. **Alert cooldown mechanism.** The existing `onCooldown` mechanism prevents alert flooding, which applies equally to GPU alerts without any additional work.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS**: No CRITICAL findings. One HIGH finding (SEC-001) exists but has a clear, non-architectural resolution: add a GPU count cap during deserialization/processing and enforce a request body size limit on the telemetry handler. The body size limit is a pre-existing systemic gap, but the GPU story should address the GPU-specific count cap as part of implementation. The engineering team must address SEC-001 as part of implementation. The QA engineer must verify that:

- The `GPUs` slice is bounded to a reasonable maximum (e.g., 16-32) during server-side processing or alert evaluation.
- Agent-supplied GPU strings (name, UUID, PCI bus ID) are rendered only via standard React JSX interpolation in the frontend (SEC-002).
- Alert evaluation does not iterate over an unbounded GPU count (SEC-003, mitigated by SEC-001).
