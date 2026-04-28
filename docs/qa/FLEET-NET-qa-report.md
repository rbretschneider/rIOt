# FLEET-NET QA Report

**Verdict:** PASS WITH NOTES.

## Test Results

- **Frontend (vitest):** 22 test files, 329 tests, all passing.
- **Go (`go test ./...`):** all 17 packages pass.
- **`tsc --noEmit`:** clean.
- **`go vet ./...`:** clean.

## AC Coverage Audit

All 20 FRD ACs covered by named tests in the impl report's mapping table. Spot-verified:

- **AC-001** — `HeartbeatData` fields verified at `internal/models/telemetry.go:18-19`. `omitempty` present on both new fields. Matches AD-001 (JSON tag override of `_per_sec` → `_sec` for disk-rate consistency).
- **AC-005, AC-006, AC-007, AC-008, AC-009** — `internal/agent/heartbeat_test.go` exercises rollover, new-interface, vanished-interface, loopback, and clock-jump branches against the pure `computeNetRates` helper.
- **AC-010 / AC-011** — `internal/server/handlers/heartbeat_compat_test.go` confirms backward compatibility in both directions.
- **AC-012** — `web/src/pages/dashboard/Dashboard.test.tsx` checks for `device-net-chart-${id}` testid alongside the percent chart.
- **AC-014** — `web/src/hooks/useFleetMetrics.test.ts` `[AC-014]` group validates `netRxPoints[0].value === 1000` and `netTxPoints[0].value === 500` for a fixture heartbeat.
- **AC-016** — covered in two places: `useFleetMetrics.test.ts` (missing fields default to 0) and `Dashboard.test.tsx` (zero-net chart renders without error).
- **AC-017** — verified by inspection. CHANGELOG line for FR-020 deferred note has been removed; README's "v1 limitations" section no longer contains the network-deferred bullet.
- **AC-020** (payload size budget) — by inspection: two `omitempty` `uint64` fields add at most ~50 bytes per heartbeat. Well under NFR-002 budget.

## Security Verifications

The security review (`docs/security/FLEET-NET-security-review.md`) issued APPROVE WITH NOTES with five findings:

| Finding | Resolution | Verified |
|---|---|---|
| **SEC-FLEET-NET-001** (clamp NaN/Inf/negative) | `finiteOrZero` helper in `useFleetMetrics.ts` clamps non-finite and negative values to 0 at three population sites (REST seed, tick update, new-device branch). | YES — `useFleetMetrics.test.ts` `[SEC-FLEET-NET-001]` test asserts `Infinity` and `-1` both clamp to 0. |
| **SEC-FLEET-NET-002** (JSON tag naming) | Documented in ADD AD-001 and impl report. | YES — informational only. |
| **SEC-FLEET-NET-003** (loopback name filter is a category, not a security boundary) | Acknowledged in code comment. | YES — informational only. |
| **SEC-FLEET-NET-004** (testability via factored helper) | `computeNetRates` extracted as a pure function. | YES — all 7 AC-018 branch cases tested directly without OS calls. |
| **SEC-FLEET-NET-005** (no mutex on `prevNetCounters`) | Mirrors disk-rate precedent (`prevDiskIO` is also unprotected, single-goroutine access). Not introducing a new race surface. | YES — same threading model as existing disk-rate code. |

## Regression Audit

Files modified outside the FLEET-NET scope: none. Every backend change is contained to the agent's heartbeat path; every frontend change is contained to the per-device chart card and its hook. The `useMemo` → `useEffect` conversion in `useFleetMetrics.ts` is in-scope: it was required to make the new hook tests pass and is a documented correctness improvement.

## Notes (non-blocking)

- **`useMemo` → `useEffect` conversion is a real bug fix carried alongside FLEET-NET.** State updates inside `useMemo` were a pre-existing anti-pattern from the FLEET-DASH initial implementation. The fix is small and contained but worth flagging in the FLEET-NET commit message so future archaeology finds it.
- **Browser verification deferred.** No dev server / agents available in this environment. The user must verify in a browser before merge per CLAUDE.md UI-change rules. Specific things to eyeball: the network sub-chart populates with non-zero values within ~30s of agent restart; both lines (cyan In, orange Out) render at 1px stroke; the bytes/sec Y-axis auto-scales correctly when traffic crosses 1KB/s, 1MB/s thresholds.

## Pre-Merge Manual Verification Required

1. `make dev` → navigate to `/dashboard` → confirm two-up card layout at desktop viewport.
2. Confirm each card has the percent chart (CPU/RAM/Disk/Load) on top and the network chart (In/Out) below.
3. Wait ~60s with active network traffic (e.g., load a YouTube video on a host); confirm the network chart shows non-zero values.
4. Restart an agent; confirm the first heartbeat after restart shows 0 for the network rates (cold start), then non-zero on subsequent heartbeats.
5. Resize to 360px viewport; confirm cards stack to one column with no horizontal scroll.

## Verdict Rationale

**PASS WITH NOTES.**

All ACs covered. All security findings resolved or documented. Tests pass green. The `useMemo` → `useEffect` correctness fix is in-scope and necessary. Browser verification is the only remaining check, and it's a manual-only step the user must perform before merging.
