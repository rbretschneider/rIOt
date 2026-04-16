# Security Review Report

**Story ID:** NGINX-ALERTS
**Reviewer:** Security Researcher Agent
**Date:** 2026-04-16
**Verdict:** APPROVED WITH CONDITIONS

---

## Threat Model Summary

### Assets
- **Agent filesystem access**: the agent reads an arbitrary file path from config (the nginx access log). The file may contain PII (client IPs, URIs, user-agents, cookies in query strings).
- **Offset state file**: `/etc/riot/nginx-access-log.offset` stores a byte offset. Integrity matters -- a corrupted or manipulated offset could cause the agent to seek to an arbitrary position in the file.
- **Telemetry payload integrity**: only aggregate integer counts must leave the agent. Any leakage of raw log content is a privacy violation.
- **Alert rule evaluation**: server-side business logic that creates events and dispatches notifications. Must not be triggerable without explicit user configuration (no hardcoded fallbacks).

### Threat Actors
- **Local unprivileged user on the monitored host**: could manipulate symlinks or log file content to influence agent behavior. The agent typically runs as root or a privileged service account.
- **Attacker who controls nginx traffic**: can craft requests that produce adversarial access log lines (extremely long lines, binary content, format-breaking characters) to attack the parser.
- **Compromised agent sending malicious telemetry**: could send fabricated `access_metrics` to the server to trigger spurious alerts or notification floods.

### Attack Surface Introduced
1. New file I/O: agent opens and reads a user-configured file path (access log) every poll interval.
2. New file I/O: agent reads/writes an offset file in `/etc/riot/`.
3. New line parser: processes untrusted content (nginx access log lines written by nginx based on external HTTP requests).
4. New telemetry fields: `access_metrics` on `ProxyServer` -- consumed by server alert evaluator.
5. New `evaluateMetric` calls: three new metric types in the alert engine.
6. New dashboard UI entries: metric dropdown and alert templates (low risk, display-only).

---

## Findings

### HIGH

#### SEC-001: No maximum line count or byte budget per collection cycle
**Severity:** HIGH
**Domain:** Denial of Service / Resource Exhaustion
**Location:** ADD Section 7 step 3f, ADD Section 11 (Performance), NFR-004
**Description:**
The ADD specifies that the agent processes all new lines since the last offset on every collection cycle. NFR-004 states "process as many lines as possible" if the log is too large. However, neither the FRD nor the ADD defines a maximum number of lines or bytes to process per cycle. If nginx is under heavy load (or an attacker sends a flood of requests), the access log could grow by hundreds of megabytes between 60-second poll intervals. The agent would spend its entire poll interval (or longer) in the scanner loop, starving other collectors and potentially causing the agent to miss heartbeat deadlines, triggering false device-offline alerts.

**Attack Scenario:**
1. Attacker sends a sustained flood of HTTP requests to the nginx instance (e.g., 100,000 requests/second).
2. Between two 60-second poll intervals, 6 million new lines are written (~1.2 GB at ~200 bytes/line).
3. The agent enters the scanner loop and processes lines for well beyond the poll interval.
4. Heartbeat interval (15s) is missed. Server marks the device offline.
5. Repeated cycles compound the backlog if the agent cannot catch up.

**Required Resolution:**
The implementation must enforce a maximum byte budget or line count per collection cycle. When the budget is exhausted, the agent must save its current offset and defer remaining lines to the next cycle. This cap must be documented in the ADD. A reasonable default would be on the order of 1-5 million lines or 256 MB per cycle, but the architect should specify the exact bound.

**Blocks:** Implementation

---

#### SEC-002: No validation or sanitization of the configured access_log path
**Severity:** HIGH
**Domain:** Input Validation / File System Access
**Location:** ADD Section 3 (AD-002), ADD Section 10 (Security Considerations)
**Description:**
The `access_log` path comes from `agent.yaml` which the ADD notes is "root-owned (0600)." This is accurate -- the config file itself is protected. However, the ADD does not specify any validation of the path value. The implementation should verify that:
(a) The path is absolute (not relative, which would resolve against the agent's working directory unpredictably).
(b) The path does not traverse outside expected log directories (e.g., `/etc/shadow`, `/proc/kcore`).
(c) The path is a regular file, not a FIFO, device file, or character device (reading `/dev/urandom` would block indefinitely; reading `/dev/zero` would consume unbounded memory via the scanner).

While the threat actor here is limited to someone who can edit the root-owned config file (who already has root access), defense-in-depth is warranted because: (1) a typo or misconfiguration could cause the agent to read a device file and hang, and (2) the agent should validate inputs at the boundary regardless of trust level.

**Attack Scenario:**
1. Admin misconfigures `access_log: /dev/zero` (typo or copy-paste error).
2. Agent opens `/dev/zero`, which is a regular file from `os.Stat` perspective but produces infinite zero bytes.
3. `bufio.Scanner` reads endlessly, consuming CPU and potentially blocking the collection cycle indefinitely.
4. Agent becomes unresponsive.

**Required Resolution:**
The ADD must specify that the implementation validates the access_log path: (a) must be absolute, (b) `os.Stat` must confirm it is a regular file (`mode.IsRegular()`), (c) reject device files, FIFOs, and directories before attempting to read. Log a warning and return zero metrics for invalid paths.

**Blocks:** Implementation

---

### MEDIUM

#### SEC-003: Offset file write is not safe against symlink attacks in degraded permission scenarios
**Severity:** MEDIUM
**Domain:** File System Security
**Location:** ADD Section 3 (AD-003), ADD Section 12 note 4
**Description:**
The ADD specifies atomic write via temp file + rename in `/etc/riot/`. The directory is root-owned, so under normal operation a symlink attack is not feasible (only root can create files in `/etc/riot/`). However, the ADD also states in Section 9 (Error Handling) that if the offset file is not writable, the agent degrades to in-memory offset. This is acceptable. The concern is the temp file creation: the ADD does not specify that the temp file must be created in the same directory as the target (which is required for atomic rename on Linux). If the temp file is created in `/tmp/`, an attacker could race to replace it with a symlink before the rename.

**Attack Scenario:**
1. If the implementation creates the temp file in `/tmp/` instead of `/etc/riot/`, a local attacker could create a symlink at the temp file path pointing to a file they want the root-owned agent to overwrite.
2. The `os.Rename` would then move the symlink target, potentially corrupting an arbitrary file.

**Required Resolution:**
The ADD should explicitly state that the temp file must be created in the same directory as the offset file (`/etc/riot/`). This is implied by "atomic rename" (which requires same-filesystem) but should be explicit to prevent implementation error. The engineer must use `os.CreateTemp` with the directory parameter set to `/etc/riot/`.

**Blocks:** No (can be addressed during implementation), but must be verified during QA.

---

#### SEC-004: Scanner default buffer size permits single-line memory spike up to 64 KiB
**Severity:** MEDIUM
**Domain:** Denial of Service
**Location:** ADD Section 11 (Performance), ADD Section 4 (AD-004)
**Description:**
The ADD correctly uses `bufio.Scanner` with its default 64 KiB max token size. Lines longer than 64 KiB will trigger a scanner error and be skipped. This is acceptable. However, the ADD should explicitly document that the scanner's max buffer is NOT increased beyond the default. A future engineer might increase `MaxScanTokenSize` to "handle edge cases" and inadvertently allow an attacker to craft a single log line that consumes arbitrary memory.

An attacker who controls HTTP request content (e.g., extremely long URIs or user-agent strings) can craft nginx access log lines up to ~64 KiB. This is within the stated memory budget but should be called out.

**Attack Scenario:**
Attacker sends HTTP requests with 60 KiB URIs. Each log line is ~60 KiB. With the default scanner buffer, this is handled correctly. But if `MaxScanTokenSize` is ever increased, memory usage scales linearly with line length.

**Required Resolution:**
The ADD should include a note that `bufio.Scanner` must NOT have its buffer size increased beyond the default 64 KiB `MaxScanTokenSize`. This should be a code comment enforced during QA review.

**Blocks:** No.

---

#### SEC-005: No cap on number of ProxyServer entries evaluated in CheckNginxAccessAlerts
**Severity:** MEDIUM
**Domain:** Denial of Service
**Location:** ADD Section 7 (Server Alert Evaluation), `generator.go`
**Description:**
The ADD specifies that `CheckNginxAccessAlerts` iterates `ws.Servers` and for each server with `AccessMetrics != nil`, calls `evaluateMetric` three times. The existing `CheckGPUAlerts` correctly caps iteration at 32 GPUs (SEC-001 from the GPU story). The ADD does not specify a similar cap for proxy servers. A compromised agent could send a telemetry payload with thousands of `ProxyServer` entries, each with `AccessMetrics`, causing the server to make thousands of `evaluateMetric` calls (each of which queries the database for enabled rules).

**Attack Scenario:**
1. Compromised agent sends telemetry with 10,000 ProxyServer entries, each with non-nil AccessMetrics.
2. Server calls `evaluateMetric` 30,000 times (3 per server).
3. Each call invokes `findMatchingRule` which calls `alertRuleRepo.ListEnabled` -- a database query.
4. Server experiences sustained high database load and potential request timeout.

**Required Resolution:**
Add a cap on the number of servers iterated in `CheckNginxAccessAlerts`, consistent with the pattern established in `CheckGPUAlerts`. A cap of 8-16 proxy servers is reasonable for any real deployment.

**Blocks:** No, but must be addressed during implementation.

---

### LOW / INFORMATIONAL

#### SEC-006: Offset file permissions at 0644 expose byte offset value
**Severity:** LOW
**Domain:** Information Leakage
**Location:** ADD Section 3 (AD-003)
**Description:**
The ADD states "The offset file is not sensitive (it's just a number), so 0644 permissions are fine." This is correct -- the offset is a single integer and reveals no PII. However, it does reveal approximate nginx traffic volume (the rate at which the offset advances correlates with request volume). For consistency with other agent state files (config at 0600, ID at implicit default), 0600 would be more appropriate. This is informational -- the actual risk is negligible.

**Required Resolution:** None required. Informational note for the engineer to consider using 0600 for consistency.

---

#### SEC-007: evaluateMetric calls ListEnabled on every invocation (performance, not security)
**Severity:** LOW
**Domain:** Performance / Availability
**Location:** `generator.go:960-1005`, ADD Section 7
**Description:**
This is a pre-existing pattern, not introduced by this story. Every call to `evaluateMetric` -> `findMatchingRule` calls `alertRuleRepo.ListEnabled(ctx)`, which hits the database. With three new nginx metrics per telemetry cycle per device, the database gets three additional queries per device per poll interval. This is not a security vulnerability but is noted because it amplifies the impact of SEC-005 (a compromised agent can multiply database queries).

**Required Resolution:** None for this story. Consider caching `ListEnabled` results per evaluation cycle in a future optimization story.

---

## Positive Observations

1. **PII containment is well-designed.** The `NginxAccessMetrics` struct contains only integer fields. The ADD explicitly specifies working on `scanner.Bytes()` to avoid string allocations, and the line parser extracts only the 3-digit status code. No IP addresses, URIs, user-agents, or other PII can leave the agent through this data path. This is a strong design decision. (NFR-007)

2. **No hardcoded fallback thresholds for nginx metrics.** AD-006 correctly ensures that nginx alert rules only fire when explicitly configured by the user. This prevents surprise alerts on devices that happen to have nginx running. (FR-018)

3. **Opt-in design with two gates.** BR-001 requires both the `webservers` collector enabled AND the `access_log` path configured. This double opt-in prevents accidental activation and is a good security posture.

4. **Atomic offset write.** The ADD specifies write-to-temp-then-rename, which prevents offset file corruption from agent crashes mid-write. This is the correct pattern.

5. **First-run seeks to end of file.** The ADD correctly specifies that on first start, the agent does not process historical log data. This prevents a burst of false alerts from historical errors and avoids processing potentially enormous backlogs.

6. **O(1) memory design.** The entire parsing pipeline is designed around fixed-size counters with no per-line allocations. This makes memory exhaustion via log content structurally impossible (within the scanner buffer limit).

7. **Graceful degradation.** File not found, permission denied, and offset write failures all degrade to zero-metric or in-memory-offset behavior without crashing or affecting other collectors. (NFR-008)

---

## Verdict Rationale

**APPROVED WITH CONDITIONS**: No CRITICAL findings. Two HIGH findings exist (SEC-001 and SEC-002) with clear, non-architectural resolutions:

- **SEC-001** requires adding a per-cycle line/byte budget to the scanner loop. This is a bounded implementation change within the existing design.
- **SEC-002** requires adding path validation (absolute path check, regular file check) before opening the access log. This is a straightforward guard.

Neither finding requires redesigning the feature or changing the FRD. The engineering team must address SEC-001 and SEC-002 as part of implementation. The QA engineer must verify:
- A per-cycle processing cap is enforced and the agent does not block indefinitely on large logs.
- The access_log path is validated as absolute and a regular file before any read attempt.
- The temp file for offset persistence is created in the same directory as the offset file (SEC-003).
- The scanner buffer size is not increased beyond the default (SEC-004).
- A cap is applied to the number of proxy servers evaluated in `CheckNginxAccessAlerts` (SEC-005).
