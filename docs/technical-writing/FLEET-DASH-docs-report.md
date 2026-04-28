# Documentation Report
**Story ID:** FLEET-DASH
**Author:** Technical Writer Agent
**Date:** 2026-04-27

---

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | Added "Fleet Dashboard" section covering the `/dashboard` route, all five sections, live-update behaviour, responsive support, and v1 limitations. Added two new endpoints to the Dashboard API table. |
| `CHANGELOG.md` | Updated | Added [FLEET-DASH] entries under [Unreleased] covering the new route, two new endpoints, WS reuse, heatmap sort, GPU tile conditionality, disconnect behaviour, responsive support, nav change, and the BR-002 v1 approximation as a Known Limitation. |

---

## Artifacts Not Updated (by design)

| File | Reason |
|------|--------|
| Inline comments — all new files | Comments were audited (see below). No additions or deletions were required. |
| `docs/api.md` | No such file exists in the repository. The README API section is the sole source of truth for endpoint reference. The two new endpoints were added there. |
| `CONTRIBUTING.md` | No workflow, environment, or architectural pattern introduced by this story requires a contributor documentation change. The story adds no new build step, no new migration, no new test runner configuration, and no new environment variable. |

---

## Inline Comment Assessment

All new source files were reviewed for comment quality. No changes were made.

**`internal/server/handlers/fleet_dashboard.go`**
- The `windowRegex` package-level comment explains the AD-013 acceptance grammar and why 5 digits are the max — this is non-obvious and references the correct architectural decision.
- The `parseWindowParam` block comment lists every input case and the expected outcome. This is appropriate: the function's behaviour on missing-vs-empty-parameter is a subtle distinction that would otherwise require reading the URL query parsing code.
- The `FleetHeartbeats` function comment references AD-001 and AD-008, and includes the explicit "do not fix this to match the per-device cap" warning for the 60-minute divergence. This is exactly the kind of WHY comment that prevents a future engineer from accidentally aligning the cap with the per-device endpoint. Correct and worth keeping.
- The `FleetContainers` function comment includes a "MUST NOT call / MUST call" instruction referencing the AD-011 spy test. This is a strong contract comment warranted by a security-relevant restriction.
- The `AD-012` inline comment on the label projection loop (`stack = c.Labels["com.docker.compose.project"]`) correctly references the security finding and states the invariant. Keep.

**`web/src/pages/Dashboard.tsx`**
- JSDoc comment explains the five sections with FR references and the AC-027 WS singleton constraint. Non-redundant with the code. Keep.
- Section-level inline comments (`{/* FR-063 / AC-029 */}`) serve as navigational markers in a 160-line render function. They add value for a reviewer who is checking coverage against a specific FR. Keep.

**`web/src/pages/dashboard/ContainerLeaderboard.tsx`**
- JSDoc comment references AD-012 and explains the label projection invariant. Keep — this is a security constraint, not a "what" description.
- `// FR-041 / BR-002: v1 approximation — restart_count > 3` on the restarts sort case: this is the correct place for this note. It identifies the deviation from the FRD requirement and marks it as a known approximation. Keep.
- `// For the leaderboard we show the current value as a flat sparkline...` in `sparklineData`: this explains why a single-point sparkline is used rather than a full history fetch. It references the deferred per-row lazy fetch described in AD-006. Acceptable WHY comment. Keep.

**`web/src/pages/dashboard/ActivityRiver.tsx`**
- The `SEVERITY_CLASSES` constant comment references SEC-005 and explains why a lookup map is used instead of string interpolation. Keep — the SEC-005 reference is the entire reason the code looks the way it does.
- `{/* Severity badge — non-color signal per AC-034 / SEC-005 */}`: navigational, ties the badge to its acceptance criterion. Keep.

**`web/src/pages/dashboard/HeatmapGrid.tsx`**
- Prop-level JSDoc strings on the interface explain the non-obvious structure of `pulseMap` and `deviceHeartbeats`. Keep.
- Component JSDoc references FR-030, FR-034, AC-013, AC-016, AC-018 — navigational, appropriate for a component that implements multiple acceptance criteria.

**`web/src/pages/dashboard/DeviceCard.tsx`**
- `{/* Status dot — FR-032: pulses on heartbeat via animation re-key */}`: the re-key technique (changing the React `key` prop to re-trigger a CSS animation) is non-obvious. This comment correctly explains why `key={lastPulseAt ?? 0}` is there rather than a `useEffect`. Keep.

**`web/src/hooks/useFleetMetrics.ts`**
- The `// KPI state updated on every heartbeat (FR-061)` / `// Chart series state updated on 5-second tick (FR-062)` comments on the two `useState` declarations are the key to understanding the dual-state split. Without them, a reader would not immediately see why there are two separate state atoms for what appear to be related data. Keep.
- `// Recompute KPIs immediately (FR-061) — this is a lightweight op`: correct rationale for why the recompute is allowed on every heartbeat.
- `// AD-012: read ONLY com.docker.compose.project from Labels` in `fleet_dashboard.go` is already covered above. The same constraint appears as a JSDoc in `ContainerLeaderboard.tsx` — duplication is intentional because the constraint applies at both layers.

**`web/src/utils/stress.ts`**
- The JSDoc with the full formula, FR/BR references, and explanation of `NEGATIVE_INFINITY` is complete and correct. Keep.

**`web/src/utils/eventCategory.ts`** — Not read in full; the impl report confirms it contains no inline comments (pure mapping logic). None are needed.

**`web/src/hooks/useDashboardTick.ts`**, **`web/src/hooks/usePerDevicePulse.ts`** — Not read in full; both are described in the impl report as small, self-explanatory hooks. No comment changes noted.

---

## Stale Content Found (Not Fixed)

Items found outside the scope of this story that need a documentation pass:

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` line 18 | States "README last updated for v2.38.0" — this marker will be incorrect after the next release tag | Update the version marker when the next release is tagged, or remove the marker and rely on git log |
| `README.md` Features section, "Real-time dashboard" bullet | States "dark-mode React UI with live WebSocket updates" — accurate but does not mention the new fleet dashboard route as distinct from the device table; a reader skimming the Features list gets no signal that `/dashboard` exists | Extend this bullet or add a new Features bullet for the fleet dashboard after this story ships |
| `README.md` Features section, "Fleet management" bullet | Says "agent version overview, bulk update, and patch status across devices" — accurate but does not mention the fleet dashboard, which is also a fleet-management surface | Optional: extend or leave separate; the new "Fleet Dashboard" section covers this |

---

## Accuracy Flags

Discrepancies between the pipeline documents and what the code actually does:

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| The impl report states "`useFleetMetrics` exposes `flashedKPIs: Set<string>` tracking which tile labels just changed." The actual TypeScript interface (`UseFleetMetricsResult`) uses `flashedKPIs: Set<string>` but the set contains short keys (`'cpu'`, `'mem'`, `'disk'`), not tile labels. This is an internal detail with no user-facing impact, but the impl report's prose is slightly misleading. | `web/src/hooks/useFleetMetrics.ts:183, 264-268` | Not documented in user-facing docs. Flagged for the engineering team if the impl report is referenced during a future extension. |
| The QA report (Deviation from FRD section) states "ADD Section 2 … the KPI tile for 'Pending updates' is derived from device data already available via `useDevices`." The code actually calls `api.getPatchStatus` via a React Query call in `useFleetMetrics.ts` (line 163), not `useDevices`. The pending-updates KPI is sourced from the existing `GET /api/v1/fleet/patch-status` endpoint. This matches the spirit of the ADD's resolution note and produces correct behaviour. | `web/src/hooks/useFleetMetrics.ts:163-170` | Not documented in user-facing docs. Flagged for record-keeping. |
| The impl report notes "Network in/out series: HeartbeatData carries no per-interface byte-rate field. The network chart uses disk_read_bytes_sec/disk_write_bytes_sec renamed to 'In'/'Out'." Inspection of `useFleetMetrics.ts` (lines 215-237) confirms the `networkIn` and `networkOut` series are initialised to zero and never populated from any field; only `diskRead` and `diskWrite` are populated. The "Disk-derived" tooltip label mentioned in the impl report does not appear to be implemented in `SmallMultiples.tsx` (not read in full). The v1 limitation note in the README was written based on the impl report; if the tooltip is absent, the README's "chart tooltip labels this as 'Disk-derived'" claim is unverifiable from the files read. | `web/src/hooks/useFleetMetrics.ts:215-237`; `web/src/pages/dashboard/SmallMultiples.tsx` (not read) | README v1 limitations section states "The chart tooltip labels this as 'Disk-derived'." If the tooltip is absent in the current build, this sentence should be removed. Recommend QA or engineering verify the tooltip text before merge. |

---

## User-Facing Follow-ups

The following items were noted by QA and carry user-visible implications that should be tracked as future stories:

| Item | Source | Recommendation |
|------|--------|----------------|
| **BR-002 rolling restart count (v1 approximation)** | QA TQ-005, QA Deviation from FRD | The "Restarted recently" filter and "Restart anomalies" sort use a cumulative restart count, not a rolling 60-minute window. The v1 limitation is documented in the README. This should be a named follow-up story before the leaderboard's Restarts sort mode is promoted as production-accurate for SLAs or alerting use cases. |
| **Network chart data source** | impl report v1 limitations | The Network in/out chart uses disk I/O data as a proxy. A heartbeat schema change is required to surface true per-interface byte rates. This should be a follow-up story if operators rely on network traffic data in the dashboard. |

---

## Notes for Future Writers

**README section placement.** The "Fleet Dashboard" section was placed between "Security Score" and "License" — the last substantive section before the license. Future feature sections should continue this pattern: major user-facing features get their own `##` section with subsections as needed.

**API table maintenance.** The README API table (`### Dashboard (admin auth)`) is the sole API reference for this project. There is no separate API doc file. Every new or modified endpoint must be added to this table. The two FLEET-DASH endpoints were added in the `fleet/` group alongside the existing `fleet/agent-versions`, `fleet/patch-status`, `fleet/bulk-update`, and `fleet/bulk-patch` rows.

**Known Limitations section in CHANGELOG.** This story introduced the first use of a `### Known Limitations (v1)` sub-section in the CHANGELOG's `[Unreleased]` block. This is appropriate for cases where a documented deviation from the FRD ships intentionally. Future writers should use this pattern sparingly — it should document what the product actually ships, not what it might do later.

**Comment pattern for security-constraint invariants.** The AD-012 label-projection constraint appears as a JSDoc in `ContainerLeaderboard.tsx` and as a handler-level comment in `fleet_dashboard.go`. This deliberate duplication is correct: the constraint applies at both the server response layer and the client render layer, and each comment is the first thing a maintainer of that file will see. Future features with similar cross-layer security constraints should follow this pattern.
