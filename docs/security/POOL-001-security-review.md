# Security Review Report
**Story ID:** POOL-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-03-23
**Verdict:** APPROVED

---

## Threat Model Summary

### Assets
- **Telemetry data in transit:** The `is_pool` boolean is added to filesystem entries within the existing telemetry JSON payload, transmitted from agent to server over the existing authenticated channel.
- **Telemetry data at rest:** Stored in the existing JSONB column in PostgreSQL. No new columns, tables, or access patterns.
- **Dashboard presentation layer:** A new visual subsection is rendered using data already available to the authenticated dashboard user.

### Threat Actors
No new threat actors are introduced by this story. The existing threat model applies unchanged:
- **Unauthenticated external attacker** -- no new endpoints or attack surface exposed.
- **Authenticated dashboard user** -- already has read access to all filesystem telemetry; this story changes only how that data is visually presented.
- **Rogue agent** -- an agent could send a crafted `is_pool` value, but since this field is a display-only boolean classification with no effect on authorization, alerting, retention, or any server-side logic, the impact is nil (a false `is_pool: true` would at most cause a non-pool filesystem to render in the Storage Pools card area).

### Attack Surface Introduced
- **No new endpoints.** No API changes whatsoever.
- **No new data inputs.** The `is_pool` field is agent-derived, not user-supplied. It flows through the existing telemetry ingest pipeline.
- **No new authentication or authorization changes.**
- **No new external service integrations.**
- **No new database migrations or schema changes.**
- **No new dependencies.**
- **One new boolean field in existing JSONB payload.** Additive, backward-compatible.

The attack surface delta for this story is effectively zero.

---

## Findings

### CRITICAL

None.

### HIGH

None.

### MEDIUM

None.

### LOW / INFORMATIONAL

#### SEC-001: Dual-location pool type list may drift
**Severity:** LOW
**Domain:** Business Logic / Maintainability
**Location:** ADD Section 3 (AD-002), `internal/models/telemetry.go` (Go list), `web/src/utils/filesystem.ts` (TS fallback list)
**Description:**
The pool filesystem type list is defined in two locations: the Go `PoolFSTypes` variable (authoritative) and the TypeScript `POOL_FS_TYPES` constant (fallback for old agents). If these lists diverge in the future, the frontend fallback classification could produce incorrect results for devices running old agents.
**Attack Scenario:**
This is not an exploitable vulnerability. A future developer adds a new pool filesystem type to the Go list but forgets the TypeScript fallback. An old agent collecting that filesystem type would not have `is_pool` set (old agent code), and the frontend fallback would also miss it (stale TS list). The filesystem would render in the regular table instead of the Storage Pools section. The impact is purely cosmetic -- no data exposure, no privilege change, no integrity violation.
**Required Resolution:**
No resolution required before implementation. The ADD already documents this maintenance requirement in Section 12 (Implementation Notes) and AD-002. The engineering team should add cross-referencing comments in both files as specified. This is noted for awareness only.
**Blocks:** Nothing.

#### SEC-002: PoolFSTypes defined as mutable var
**Severity:** LOW
**Domain:** Data Integrity (defense-in-depth)
**Location:** ADD Section 5, `internal/models/telemetry.go` -- proposed `var PoolFSTypes = []string{...}`
**Description:**
The ADD specifies `PoolFSTypes` as a `var` (mutable package-level variable). In Go, any code importing the models package could modify this slice at runtime, potentially altering pool classification behavior. While no current code path does this, and it would require a bug or malicious internal code change, defense-in-depth suggests the list should not be mutable.
**Attack Scenario:**
Not externally exploitable. A hypothetical programming error or malicious internal code change could append to or truncate the slice. Impact would be limited to incorrect visual classification on the dashboard -- no security boundary is affected.
**Required Resolution:**
No resolution required. This is an informational note. The engineering team may choose to use a helper function with a hardcoded list rather than an exported mutable slice, but the security impact is negligible.
**Blocks:** Nothing.

---

## Positive Observations

1. **No new endpoints or API surface.** The story is purely additive to an existing data flow, which is the lowest-risk category of change.

2. **No database migration required.** Leveraging the existing JSONB storage for a new field avoids schema change risks entirely.

3. **Backward compatibility is well-designed.** The `omitempty` JSON tag on the Go side and the optional TypeScript field with client-side fallback classification is a sound approach that handles mixed agent versions gracefully without server-side transformation.

4. **No new dependencies.** Zero supply chain risk.

5. **The `is_pool` field is purely informational.** It affects only visual presentation -- no authorization decisions, alerting thresholds, retention policies, or any other security-relevant logic depends on this field. This means even if an agent sends a malicious value, the blast radius is zero.

6. **Existing authentication model unchanged.** The telemetry ingest endpoint remains protected by the `X-rIOt-Key` per-device API key. Dashboard access remains unchanged.

---

## Verdict Rationale

**APPROVED**: No CRITICAL or HIGH findings. The two LOW/informational findings are defense-in-depth observations with no exploitable attack path and no security boundary impact. This story adds a derived boolean display classification to an existing authenticated data flow with no new endpoints, no new inputs, no new dependencies, and no database changes. Implementation may proceed without conditions.
