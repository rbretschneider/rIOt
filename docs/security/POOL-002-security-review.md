# Security Review Report
**Story ID:** POOL-002
**Reviewer:** Security Researcher Agent
**Date:** 2026-03-23
**Verdict:** APPROVED

---

## Threat Model Summary

### Assets
- **Telemetry data integrity**: The `is_pool` boolean classification on filesystem telemetry. Misclassification is a display-only concern, not a security concern.
- **Agent stability**: The disk collector must not crash or degrade due to new logic paths.

### Threat Actors
No threat actors are materially relevant to this story. The change modifies internal classification logic that operates on data already collected locally by the agent from `/proc/mounts`. There is:
- No new user-facing input
- No new network-facing surface
- No authentication or authorization change
- No data that crosses a trust boundary in a new way

The device path and filesystem type values originate from the kernel via gopsutil's reading of `/proc/mounts`. These are not attacker-controlled unless the attacker already has root on the monitored device, at which point the agent is fully compromised regardless of this change.

### Attack Surface Introduced
**None.** This story adds pure classification logic (string prefix matching) inside an existing function, operating on existing data fields. No new endpoints, no new inputs, no new dependencies, no new configuration, no new data storage.

---

## Findings

### CRITICAL

None.

### HIGH

None.

### MEDIUM

None.

### LOW / INFORMATIONAL

#### SEC-001: Prefix match on `/dev/md` could match non-mdraid devices
**Severity:** LOW
**Domain:** Business Logic Security
**Location:** ADD Section 5, `IsPoolFilesystem` function, mdraid prefix check
**Description:**
The prefix `/dev/md` would match hypothetical devices like `/dev/mdisk` or similar non-mdraid paths. The ADD acknowledges this in Section 9 (Error Handling) for the exact path `/dev/md` with no suffix, noting it is not a real device path in practice.
**Attack Scenario:**
No attack scenario exists. This is a correctness observation, not a security vulnerability. On Linux systems, `/dev/md*` paths are exclusively used by the md (multiple devices) driver. There is no realistic scenario where a non-mdraid device would appear under this prefix on a homelab system.
**Required Resolution:**
None required. This is documented for awareness. If the project later targets environments with exotic block device naming, the prefix could be tightened to `/dev/md` followed by a digit, but this is not warranted for the current homelab scope.
**Blocks:** Nothing

#### SEC-002: Detection logic duplicated across Go and TypeScript
**Severity:** LOW
**Domain:** Maintainability (defense-in-depth)
**Location:** ADD Section 12 (Implementation Notes), three-location list maintenance
**Description:**
The pool detection logic (fs-type list, device-path prefixes, exclusion list) is maintained in three locations: Go `IsPoolFilesystem()`, TypeScript `isPoolFilesystem()`, and the two `PoolFSTypes`/`POOL_FS_TYPES` constants. A future edit that updates one but not the others could cause classification divergence between the agent and the frontend fallback. This is not a security vulnerability -- the worst outcome is a display inconsistency -- but it is a maintenance risk that the ADD correctly identifies and documents.
**Attack Scenario:**
None. Classification divergence would cause a pool to appear in the wrong UI section, not a security breach.
**Required Resolution:**
None required. The ADD already documents this constraint and instructs engineers to update all three locations. This is informational.
**Blocks:** Nothing

---

## Positive Observations

1. **No new attack surface.** The story is scoped precisely to internal classification logic with no new endpoints, inputs, dependencies, or configuration. This is the ideal security profile for a feature change.

2. **No new dependencies.** The only new import is `strings` in the Go standard library, which is already used elsewhere in the codebase (e.g., `disk.go` line 5).

3. **Exclusion list is well-specified.** The Docker device-mapper exclusion (`/dev/mapper/docker-`) and live-boot exclusion (`/dev/mapper/live-rw`, `/dev/mapper/live-base`) prevent false positives that could clutter the user's pool view. The exclusions are specific prefix/exact matches, not overly broad patterns.

4. **No data exposure changes.** The device path is already present in the telemetry payload and already displayed in the regular filesystems table. Showing it on pool cards does not expose new information.

5. **Pure function design.** `IsPoolFilesystem` is a stateless pure function with no side effects, no I/O, and no error paths. This makes it trivially testable and incapable of introducing runtime failures.

6. **Backward compatibility preserved.** The `is_pool` field remains `omitempty`, old agents continue to work, and the frontend fallback handles missing values gracefully.

---

## Verdict Rationale

**APPROVED**: No CRITICAL or HIGH findings. The two LOW/INFORMATIONAL findings are maintenance observations, not security vulnerabilities. The story introduces zero new attack surface -- it adds string prefix matching logic to an existing pure function operating on kernel-provided data already in the telemetry pipeline. Implementation may proceed without security conditions.
