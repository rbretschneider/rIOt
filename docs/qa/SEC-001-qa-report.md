# QA Report
**Story ID:** SEC-001
**Title:** Enhance Security Page with Score Column and Additional Security Insights
**QA Engineer:** QA Agent
**Date:** 2026-03-23
**Verdict:** PASS WITH NOTES

**Re-review Date:** 2026-03-23
**Re-review Result:** Previous FAIL verdict resolved. CRIT-001 fix verified. Verdict upgraded to PASS WITH NOTES.

---

## Test Run Summary

### Go tests (`go test ./...`)

- **Before QA additions (initial run):** All packages passing (handlers cached at 0.570s)
- **After QA additions (initial run):** 1 failing test in `internal/server/handlers` (intentional — demonstrated a real implementation bug)
- **After CRIT-001 fix (re-review run):** All packages passing. 11 test packages pass, 0 failing, 0 skipped.
  - `internal/server/handlers`: PASS — including `TestSecurityOverview_AC008_CertsCountedWhenNoSecurityTelemetry` which is now GREEN.

### Frontend tests (`npm run test:run`)

- **Before QA additions (initial run):** 136 tests passing across 11 files
- **After QA additions (initial run):** 137 tests passing across 11 files (1 new AC-010 row-order test added and passing)
- **After CRIT-001 fix (re-review run):** 137 tests passing across 11 files. No change.

### Noise in test output

The stderr `ECONNREFUSED` errors and `No queryFn` warnings in the frontend test run are pre-existing — they come from `ActivityLog` and `FleetOverview` tests that do not mock all queries. All tests pass despite this noise. Not introduced by this story.

---

## AC Coverage Audit

| AC ID | Status | Tests Covering It | Gap Description |
|-------|--------|-------------------|-----------------|
| AC-001 | COVERED | `FleetOverview.test.tsx`: `[AC-001]` describe block (2 tests) | Asserts no Score column header and no MiniScore title attribute in rendered DOM |
| AC-002 | COVERED | `Security.test.tsx`: `[AC-002]` (3 tests); `security.test.ts`: `[AC-002]` (16 tests) | Column position, grade color coding, grade threshold boundaries all tested |
| AC-003 | COVERED | `Security.test.tsx`: `[AC-003]` (1 test) | Clicks score button, asserts SecurityScoreModal appears |
| AC-004 | COVERED | `Security.test.tsx`: `[AC-004]` (1 test) | Mock rejects score query; asserts "-" with `text-gray-600` class |
| AC-005 | COVERED | `Security.test.tsx`: `[AC-005]` (2 tests) | Fleet Score banner value and emerald color for A grade |
| AC-006 | COVERED | `Security.test.tsx`: `[AC-006]` (4 tests); `security_handler_test.go`: AC-006 tests (2 tests) | Column header, red color for >0, muted 0, dash for no telemetry; backend field population |
| AC-007 | COVERED | `Security.test.tsx`: `[AC-007]` (4 tests); `security_handler_test.go`: AC-007 tests (3 tests) | Column header, green/amber/dash display; backend *bool true/false/nil |
| AC-008 | COVERED | `Security.test.tsx`: `[AC-008]` (2 tests); `security_handler_test.go`: AC-008 tests (4 tests including QA regression test) | Frontend and backend cert counting covered; boundary conditions tested. CRIT-001 fix confirmed: cert counting block now runs before the `sec == nil` guard. QA regression test `TestSecurityOverview_AC008_CertsCountedWhenNoSecurityTelemetry` is GREEN. |
| AC-009 | COVERED | `Security.test.tsx`: `[AC-009]` (1 test); `security_handler_test.go`: AC-009 (1 test) | Frontend: no card when `total_certs=0`; backend: zero counts when no web server data |
| AC-010 | COVERED | `Security.test.tsx`: `[AC-010]` (5 tests — 4 original + 1 added by QA) | Header arrow indicators covered by original tests; actual row order covered by QA-added test (passing) |

---

## Test Quality Findings

### TQF-001: AC-010 sort tests only verified arrow indicators, not row order (fixed)

The four original AC-010 tests each assert on the column header `↑`/`↓` indicators but do not verify that table rows actually appear in the correct order. If the comparator in `SortableSecurityTable.sorted` returned `0` for all pairs, the arrow would still appear correctly but FR-021 ("worst scores first") would be violated silently.

**Action taken:** QA added a fifth AC-010 test (`default Score ascending sort places lower-scored device above higher-scored device`) that provides two devices with different scores in reversed order, resolves their scores via mocked `getSecurityScore`, and asserts the DOM row order. This test passes, confirming the implementation is correct; the gap was only in coverage expressiveness.

### TQF-002: AC-004 and AC-007 dash tests are not specific enough to column

The AC-004 test for no-score dashes and the AC-007 test for null `unattended_upgrades` dashes both look for any element with `text-gray-600` containing "-". The table has multiple columns that can show "-" simultaneously (SELinux, AppArmor, Firewall, Score, Sec. Updates, Auto-Updates). The tests would pass even if the wrong column showed the dash. The assertions are not false — the dash IS rendered with the right styling — but they are not specific to the target column. This is a quality-of-life issue, not a blocking one, since the surrounding tests (AC-006 showing "0" in gray-500, AC-007 showing "Enabled"/"Disabled") together constrain the behavior sufficiently.

### TQF-003: AC-005 fleet average computed from score cache state, not from a deterministic fixture

The AC-005 tests use the global `mockGetSecurityScore` which returns `scoreA` (92) for both devices. The fleet average assertion checks the banner shows `92`. This is correct but tightly coupled to the `beforeEach` default. There is no test with devices returning different scores to verify that the arithmetic mean is computed correctly (e.g., one device at 80 and one at 60 should produce 70). The test confirms the banner appears and is color-coded, but not the averaging math. The averaging math itself is trivial and covered implicitly, so this is a minor observation only.

### TQF-004: Backend test helper `makeSecuritySnap` creates snapshots with nil Updates and nil WebServers

The `makeSecuritySnap` helper initializes only `Security`. Tests for AC-007 null case and AC-006 no-certs case use this helper appropriately. No issue.

---

## Adversarial Findings

### CRIT-001: `SecurityOverview` cert counting silently skips devices without security telemetry — FIXED

**Location:** `internal/server/handlers/security.go`, `SecurityOverview` handler

**Status:** RESOLVED. Fix verified in re-review on 2026-03-23.

**Original description:** The handler's main loop contained an early `continue` when `snap.Data.Security == nil`. The web server cert counting block was inside this loop body, after the `continue`. Any device running the `webservers` collector but not the `security` collector had its SSL/TLS certificates silently excluded from `CertsExpiringSoon` and `TotalCerts`.

**Fix applied:** The cert counting block (`if ws := snap.Data.WebServers; ws != nil { ... }`) was moved to lines 55-64, before the `sec := snap.Data.Security` assignment and the `if sec == nil { continue }` guard at line 66. Cert counting now runs unconditionally for every snapshot regardless of whether security telemetry is present.

**Fix verification:**
- Code inspection of `security.go:54-68` confirms the correct structure: cert loop runs first, then `sec == nil` check.
- `TestSecurityOverview_AC008_CertsCountedWhenNoSecurityTelemetry` now passes GREEN.
- Full `go test ./internal/server/handlers/` run: all 11 security tests PASS.

**Classification:** Correctness defect against AC-008 / FR-017 — now resolved.

---

### INFO-001: `SecurityDevices` handler also skips cert counting for devices without security telemetry

**Location:** `internal/server/handlers/security.go:111-143`

**Description:** The `SecurityDevices` handler's cert counting is also inside the `if sec == nil { continue }` guard. For this handler, this is intentional by design — the endpoint only returns rows for devices with security telemetry. A device running only webservers would not appear in the table at all. No action required for this handler specifically.

**Classification:** Informational. Consistent with existing behavior (devices without security telemetry are not in the security devices list, per implementation note #4 in the impl report).

---

### INFO-002: `pending_security_count` semantic ambiguity (inherited from security review)

The `pending_security_count` defaults to `0` for devices with no update telemetry, while `unattended_upgrades` is `null`. The frontend correctly uses `unattended_upgrades === null` as the signal for "no data" and shows "-" in both columns. The implementation is consistent with AD-005. Noted in the security review as SEC-001-L01; still present in implementation by design.

**Classification:** Informational. No action required for this story.

---

## Tests Added by QA

| File | Test Name | Covers | Result |
|------|-----------|--------|--------|
| `internal/server/handlers/security_handler_test.go` | `TestSecurityOverview_AC008_CertsCountedWhenNoSecurityTelemetry` | AC-008 / CRIT-001 | GREEN (was RED before fix, confirms fix is correct) |
| `web/src/pages/Security.test.tsx` | `default Score ascending sort places lower-scored device above higher-scored device` | AC-010 row order | GREEN |

---

## Deviations from ADD

### DEV-001 (from impl report): Fleet average score displayed as a banner strip, not an overview card

The ADD (Section 12, item 5) specifies computing and displaying the fleet average score. The impl report documents that this was rendered as a banner row above the table column headers rather than as a StatCard in the overview grid.

**QA assessment:** The banner satisfies the FRD's AC-005 requirements: it shows the mean score rounded to the nearest integer, it is color-coded by grade using `gradeColor(gradeFromScore(fleetAvg))`, and the label reads "Fleet Score". The FRD says "A card labeled 'Fleet Score' (or similar)" — "or similar" grants latitude. The deviation is cosmetic and the AC tests confirm the behavior. This deviation does not warrant a FAIL.

### `security_score` feature flag description updated

The `useFeatures.ts` file now shows `security_score` described as "Security score gauge on device detail" — the "fleet dashboard" reference was removed per ADD Section 4. The feature flag no longer appears anywhere in `FleetOverview.tsx`. Confirmed.

---

## Deviations from FRD

### CRIT-001 (resolved): `SecurityOverview` cert counting excluded devices without security telemetry

Previously a deviation from FR-017 and AC-008. Fix verified on re-review 2026-03-23. No longer a deviation.

---

## Verdict Rationale

**PASS WITH NOTES** — all ACs covered, all tests green, no blocking defects remain.

The single FAIL condition from the initial review was CRIT-001: the `SecurityOverview` handler's cert counting block was inside the `if sec == nil { continue }` guard, causing the `Certs Expiring` card to undercount for devices with webserver telemetry but no security telemetry.

The fix has been applied and verified:
- `internal/server/handlers/security.go` lines 55-64: cert counting now executes unconditionally before the `sec == nil` guard.
- `TestSecurityOverview_AC008_CertsCountedWhenNoSecurityTelemetry` is GREEN.
- Full Go suite: all 11 packages passing.
- Full frontend suite: 137 tests passing across 11 files.

All ten ACs are now fully covered. Minor quality findings (TQF-001 through TQF-004) remain documented. TQF-001 was addressed by a QA-added row-order test. TQF-002 and TQF-003 are non-blocking observations that do not affect correctness.

---

## Action Required

None. All issues from the initial FAIL verdict have been resolved. This story is cleared for handoff to the technical writer.
