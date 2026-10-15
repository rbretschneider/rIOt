# REBOOT-ETA-001 — Implementation Report

**Story:** REBOOT-ETA-001 — Estimated-reboot-time tooltip for fleet devices pending a reboot.
**Engineer:** senior_dev (Model: Opus 4.8). **Date:** 2026-10-15.
**Branch:** `feat/reboot-eta-tooltip` (off `main`).

## Detected stack
- Go 1.26.0 (module `github.com/DesyncTheThird/rIOt`), chi v5, pgx v5, testify.
- React + TypeScript + Tailwind (Vite), vitest + Testing Library, TanStack Query. Node v24.19.0.
- Active specialists: Backend Engineer, Frontend Engineer.

## Result
- All three test runs green. Working tree clean; everything committed.
- Every file in ADD Section 4 implemented. All 22 ACs have AC-named tests.
- Only agent edits: two additive spots in `internal/agent/telemetry.go` and the new field on `internal/models/telemetry.go`. Nothing from PR #1 or PR #2, and nothing in the PR #2 worktree, was touched.
- No migration, no new dependency (Go or npm), no env var.

## Commits (oldest first)
1. `c34d57c` feat: agent reports allow_reboot on update telemetry
2. `6f38933` feat: add nextMaintenanceWindowStart with injected clock
3. `9f39fc0` feat: patch-status rows carry reboot-ETA fields
4. `68678e3` test: gofmt window tests
5. `a4844c8` feat: reboot-ETA tooltip helper, grid indicator, and panel badge title
6. `ab95b9b` docs: add FRD, ADD, and security review (committed so the tree is clean)

Each message is prefixed `[REBOOT-ETA-001]` and ends with the Opus 4.8 Co-Authored-By line.

## Files changed
**Go:**
- `internal/models/telemetry.go`: appended `AllowReboot *bool` (`json:"allow_reboot,omitempty"`) to `UpdateInfo`.
- `internal/agent/telemetry.go`: in the `case *models.UpdateInfo:` arm, `if v != nil { allow := a.config.Commands.AllowReboot; v.AllowReboot = &allow }`.
- `internal/server/handlers/auto_update.go`: added `nextMaintenanceWindowStart(w, now)` + const `nextWindowSearchDays = 366`; reuses `validTimeStr`, `parseTimeStr`, `frequencyAllowsDay` verbatim.
- `internal/server/handlers/fleet.go`: `PatchStatus` loads automation config once and computes next start once per request (`time.Now()` only here); adds `os_patch_mode`, `next_window_start`, `os_patch_cooldown_minutes`, `allow_reboot` to rows in both detail and non-detail modes.
- `internal/server/handlers/auto_update_window_test.go` (new).
- `internal/server/handlers/fleet_patch_eta_test.go` (new — not in ADD §4; see Deviations).
- `internal/server/handlers/fleet_handler_test.go` (one-line mock fix; see Deviations).

**Web:**
- `web/src/api/client.ts`: four optional fields on `DevicePatchInfo`.
- `web/src/types/models.ts`: `UpdateInfo.allow_reboot?`.
- `web/src/utils/rebootEta.ts` (new): `formatUtc`, `rebootEtaTooltip`.
- `web/src/utils/rebootEta.test.ts` (new).
- `web/src/pages/FleetOverview.tsx`: `patchMap` now `Map<string, DevicePatchInfo>`; new grid indicator with `role="img"`, `aria-label="Reboot required"`, `title={rebootEtaTooltip(info)}`; panel badge now `title={rebootEtaTooltip(d)}`. No existing content removed.
- `web/src/pages/FleetOverview.test.tsx`: component-level AC tests appended; existing tests untouched.

## AC → test mapping
Key: W = `auto_update_window_test.go`, F = `fleet_patch_eta_test.go`, H = `web/src/utils/rebootEta.test.ts`, P = `web/src/pages/FleetOverview.test.tsx`.

| AC | Tests |
|---|---|
| 001 | P `[AC-001] panel badge renders when reboot required` |
| 002 | P `[AC-002] main-grid indicator renders when reboot required` |
| 003 | P `[AC-003] no indicator when reboot not required` |
| 004 | P `[AC-004] identical tooltip text in both placements` |
| 005 | P `[AC-005] native title attribute in both placements`; H `[AC-005] tooltip is plain text` |
| 006 | H `[AC-006] window mode shows Est. reboot as absolute UTC`; P `[AC-006] in rendered tooltip`; F `TestPatchStatus_AC006_WindowModeCarriesNextWindowStart` |
| 007 | H `[AC-007] anytime mode`; F `TestPatchStatus_AC007_AnytimeModeCarriesCooldownNoStart` |
| 008 | H `[AC-008] disabled mode`; F `TestPatchStatus_AC008_DisabledModeOmitsNextWindowStart` |
| 009 | H `[AC-009] no window configured`; F `TestPatchStatus_AC009_DefaultConfigOmitsNextWindowStart` |
| 010 | H `[AC-010] estimate-not-guarantee labelling` |
| 011 | H `[AC-011] caveat discloses dependencies` |
| 012 | H `[AC-012] allow_reboot=false shows manual reboot, no timestamp`; F `TestPatchStatus_AC012_AllowRebootTriState` (+3 subtests) |
| 013 | W `_AC013_SameDayBeforeStartReturnsToday` |
| 014 | W `_AC014_OvernightReturnsStartDayStart`, `_AC014_OvernightTailResolvesToTonightsStart` |
| 015 | W `_AC015_WeeklyReturnsNextListedDay`, `_WeeklyTodayEligibleBeforeStartReturnsToday`, `_WeeklyTodayEligibleAfterStartReturnsNextWeek`, `_WeeklySundayOnlyWithinHorizon` |
| 016 | W `_AC016_MonthlyReturnsLastDayOfMonth`, `_MonthlyAfterLastDayStartRollsToNextMonth`, `_MonthlyLeapFebruary` |
| 017 | W `_AC017_DailyAfterStartReturnsTomorrow`, `_EmptyFrequencyIsDaily` |
| 018 | W `_AC018_InsideWindowReturnsNextOccurrence`, `_OneMinuteAfterStartReturnsNextOccurrence`, `_ExactStartInstantReturnsNow`, `_SecondsAfterStartIsPast` |
| 019 | W `_AC019_NotFoundCases` (10 subcases); F `TestPatchStatus_AC019_UnusableStartOmitsNextWindowStart`; H `[AC-019] honest fallback`; P `[AC-019] fallback tooltip` |
| 020 | F `TestPatchStatus_AC020_AdditiveContractInBothModes`; P `[AC-020] no additional network request` |
| 021 | W `_AC021_FollowsInjectedClock`, `_NonUTCInputIsNormalised`, `_Deterministic` |
| 022 | P `[AC-022] existing badge non-regression` |

Note: the pre-existing `TestPatchStatus_AC021_RebootClassFields` and an `[AC-021]` vitest describe belong to the earlier PATCH-GATE story (same AC numbers, unrelated); both still pass.

## Security conditions
- **SEC-001 (allow_reboot display-only):** Outside `internal/agent`, the telemetry field is read in exactly one place — `fleet.go` `AllowReboot: s.Updates.AllowReboot` (copy into response). `checkAutoPatch` and dispatch/gating are untouched and do not reference it. Other `AllowReboot` hits are agent-side config reads (`commands.go`, `config.go`, `doctor.go`), unchanged.
- **SEC-002 (no XSS sink):** Both placements use native `title={rebootEtaTooltip(...)}`; `rebootEta.ts` returns plain strings; no `dangerouslySetInnerHTML`/`innerHTML`; no tooltip library added.
- **SEC-004 (bounded/panic-free):** Returns `(zero,false)` unless `w.Mode=="window"` and `validTimeStr(w.StartTime)`; loop `for d:=0; d<=366` (367 iters max); exhaustion returns `(time.Time{},false)`; unsatisfiable weekly rules covered by `AC019_NotFoundCases`; in-window boundary + exact-start pinned by the four `AC018` tests.

## Deviations from ADD (flagged for QA/architect)
1. **Two Go test-file changes not in ADD §4.** The existing `TestPatchStatus_AC021_RebootClassFields` built `&Handlers{telemetry, devices}` with a nil `adminRepo`; `PatchStatus` now calls `loadAutomationConfig` (per ADD) which dereferences `adminRepo`, so the test would panic. Added `adminRepo: testutil.NewMockAdminRepo("hash")` (one line, no assertion changed). New handler tests live in new file `fleet_patch_eta_test.go`.
2. **`formatUtc` returns `string | null`** (ADD said `string`). `null` on an unparseable instant lets the tooltip fall back to honest no-time text (FR-019) instead of "Invalid Date". Valid-input output unchanged.
3. **Anytime wording with no cooldown:** the "(within the ~N min cooldown)" parenthetical is omitted rather than rendering `~undefined`. With a cooldown the text matches the ADD.
4. **`FleetOverview.test.tsx` modified** to hold component-level AC tests (additive only); ADD named no file for them.
5. **Grid indicator is a glyph (U+21BB)** with `role="img"` + `aria-label="Reboot required"`, not the literal words, to avoid colliding with the panel's `findByText('reboot required')`.

No blockers, no ARCH-QUESTION ambiguities.

## Test runs
**1. New Go tests** (`go test -count=1 ./internal/server/handlers/ -run "NextMaintenanceWindowStart|PatchStatus" -v`): PASS — 41 `--- PASS`, 0 `--- FAIL`.

**2. Full Go suite** (`go build ./...` exit 0; `go vet ./...` clean; `go test -count=1 ./...`, Windows, no -race): 19 packages `ok`, 0 FAIL.

**3. Web suite** (`cd web && npm run test:run`): `Test Files 27 passed (27)`, `Tests 399 passed (399)` (baseline 359; +40 new, no regressions). `npx tsc --noEmit` clean; no `any` introduced.

## Lint / static checks
- `go vet ./...` clean; `gofmt -l` clean on new files. (`fleet.go`/`models/telemetry.go` show whole-file gofmt diffs due to pre-existing CRLF line endings, unchanged by this story.)
- Web project has no eslint script; `tsc` is the check.

## Notes for QA
- Go window tests use fixed UTC instants (2026-10-15 Thu; 2026-10-31 Sat). Only `TestPatchStatus_AC006` reads wall clock (daily window — cannot flake).
- Agents report `allow_reboot` only after upgrade; until then UI shows conditional "if this device permits automatic reboot" wording.
- No collector whitelist change needed (field rides the existing `updates` collector).
- `os_patch_mode`/`next_window_start`/`os_patch_cooldown_minutes` are identical on every row by design (AD-004).
