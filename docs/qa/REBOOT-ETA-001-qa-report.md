# QA Report
**Story ID:** REBOOT-ETA-001
**QA Engineer:** QA Agent (Sonnet 5.5)
**Date:** 2026-10-15
**Verdict:** PASS WITH NOTES

---

## Test Run Summary
- `go build ./...`: OK. `go vet ./...`: clean. `npx tsc --noEmit`: clean.
- `go test ./... -count=1` (Windows, no -race): 19 packages `ok`, 0 FAIL (the remaining packages report "no test files"). Run twice, identical results.
- `cd web && npm run test:run`: 27 files, **402 passed**, 0 failed, 0 skipped. This is the implementation's 399 plus 3 QA-added. Baseline was 359.
- Implementation claims (19 packages ok, 399 web tests) are confirmed. After QA additions the web count is 402.
- Flaky tests: none observed. The `ECONNREFUSED ::1:3000` noise in the vitest output is a logged, handled network failure from an unrelated test and does not fail anything.
- Coverage percentage: not measured. Coverage was audited per AC below.

## AC Coverage Audit

| AC | Status | Tests | Notes |
|----|--------|-------|-------|
| AC-001 | COVERED | FleetOverview.test.tsx `[AC-001] panel badge renders...` | Opens the panel and asserts the badge. |
| AC-002 | COVERED | FleetOverview.test.tsx `[AC-002]` (2 tests) | Asserts the indicator, the pending count and the IP in the row. Does not assert the Auto badge. The diff to `FleetOverview.tsx` is purely additive, so non-removal is confirmed by review. |
| AC-003 | COVERED | `[AC-003]` (3 tests) | Covers false, a missing row, and no panel badge when false. |
| AC-004 | COVERED | `[AC-004]` (2 tests) + QA test | Compares grid vs panel `title` for window/true and for allow_reboot=false. QA added the unknown-allow_reboot case. |
| AC-005 | COVERED | `[AC-005]` page + helper tests | `package.json`, lockfile, `go.mod` and `go.sum` are unchanged in `git diff main`. Only the native `title` is used. |
| AC-006 | COVERED | helper `[AC-006]` (4), page `[AC-006]`, Go `AC006` | Asserts the `Est. reboot: 2026-10-19 02:00 UTC` token. Tests a `-05:00` input and shows it resolves to UTC. |
| AC-007 | COVERED | helper `[AC-007]` (4), Go `AC007`, QA page test | Cooldown wording, no `Est. reboot:`, no `\d\d:\d\d`, no `UTC`. Go asserts `next_window_start` is absent from the wire. |
| AC-008 | COVERED | helper `[AC-008]`, Go `AC008`, QA page test | Exact string equality. Tests that a stray `next_window_start` is ignored. |
| AC-009 | COVERED | helper `[AC-009]`, Go `AC009` | Go test uses no saved config, so the default is `disabled`. |
| AC-010 | COVERED | helper `[AC-010]` (3) | Checks `estimate, not a guarantee` on both window variants and anytime. |
| AC-011 | COVERED | helper `[AC-011]` (2) | Checks gating, cooldown, allow_reboot and hold_reboot_class. |
| AC-012 | COVERED (gap closed by QA) | helper `[AC-012]` (3), Go `AC012` tri-state (3), QA agent stamp tests (3), QA page test | The original suite never tested that the agent actually stamps `allow_reboot`. See Test Quality Findings. |
| AC-013 | COVERED | Go `AC013_SameDay...` | |
| AC-014 | COVERED | Go `AC014_Overnight...` (2) + QA weekly overnight | QA added the start-day convention test for weekly overnight. |
| AC-015 | COVERED | Go `AC015_*` (4) + QA (2) | QA added exact-start on an eligible day and mixed valid/invalid day values. |
| AC-016 | COVERED | Go `AC016_*` (3) + QA (4) | QA added exact-start on the last day, non-leap Feb (2027, 2100), and Dec→Jan year roll. |
| AC-017 | COVERED | Go `AC017_*` (2) + QA year-boundary | |
| AC-018 | COVERED | Go `AC018_*` (4) + QA (2) | Pins both sides of the boundary: inside the window skips to the next occurrence; `now == start` returns `now`; +1s and +1ns are skipped. QA added the 00:00 boundary and the 1ns case. |
| AC-019 | COVERED | Go `AC019_NotFoundCases` (10 subcases), Go `AC019` handler, helper `[AC-019]` (3), page `[AC-019]`, QA unknown-frequency | Covers weekly with empty days, out-of-range days, bad start, and non-window modes. Unparseable and empty `next_window_start` also degrade. |
| AC-020 | COVERED | Go `AC020` (plain and detail), page `[AC-020]` | Asserts `getPatchStatus` is called once and detail only on panel open. The array contract and existing keys are verified. |
| AC-021 | COVERED | Go `AC021_*` (3) | Year-2001 and year-2090 clocks, a UTC+5 input, and determinism. The function body has no `time.Now()`. |
| AC-022 | COVERED | page `[AC-022]` | The badge text is unchanged. The `title` check is weak (`contains('reboot')`), but the title content is covered by the other ACs. |

Every one of the 22 ACs is COVERED. No MISSING or PARTIAL ACs remain.

## Security Conditions

- **SEC-001: HOLDS.** A grep of non-test code shows `AllowReboot` is read from telemetry in exactly one place, `fleet.go:72` (`AllowReboot: s.Updates.AllowReboot`), as a copy into the response. `checkAutoPatch` and the dispatch/gating path do not reference it. All other hits are agent-side config reads in `commands.go`, `config.go` and `doctor.go`.
- **SEC-002: HOLDS.** Both placements use `title={rebootEtaTooltip(...)}`: the grid span and the panel badge. `rebootEta.ts` returns plain strings, and a test asserts no `<` or `>` in any variant. There is no `dangerouslySetInnerHTML` or `innerHTML` in `web/src` outside the unrelated `Probes.test.tsx`. No tooltip library was added.
- **SEC-004: HOLDS.** `nextMaintenanceWindowStart` (`auto_update.go:256`):
  - It guards `w.Mode != "window" || !validTimeStr(w.StartTime)` before iterating.
  - The loop is `for d := 0; d <= 366; d++`, so 367 iterations at most.
  - It returns `(time.Time{}, false)` on exhaustion.
  - It reuses `frequencyAllowsDay`, `parseTimeStr` and `validTimeStr` verbatim, and has no `time.Now()` call.
  - The in-window boundary is pinned by tests.

## Test Quality Findings

1. **FIXED by QA. Agent stamp untested.** `collectAll` stamping `commands.allow_reboot` onto `UpdateInfo` had no test. Removing the stamp would have kept every test green, while in production every device would read "unknown" and the AC-012 manual-reboot path could never fire. QA added `internal/agent/telemetry_allow_reboot_test.go`:
   - true is reported as a non-nil true;
   - false is reported as an explicit false and survives `omitempty` on the wire;
   - a nil `*UpdateInfo` does not panic.
2. **FIXED by QA. Window edge cases.** Added the weekly and monthly inclusive-start boundaries, non-leap and century February, the Dec→Jan year roll, weekly overnight start-day convention, mixed valid/invalid `days_of_week`, an unknown frequency string, a 00:00 start and a 1ns-past-start case.
3. **FIXED by QA. Rendered-title wiring.** Added page-level tests for allow_reboot unknown/absent (conditional wording, not "manual", identical in both placements), anytime mode, and disabled mode.
4. Minor. AC-022 asserts only `title.contains('reboot')`. This is non-blocking.
5. Minor. AC-002 does not assert that the "Auto" badge is preserved. The diff is additive, so this is non-blocking.

## Adversarial Findings

- **Weekly with empty `days_of_week`** returns `(zero, false)` and the tooltip falls back to the no-time text. Covered by tests.
- **Stored start of `24:00`, `12:60`, garbage or empty** returns not-found, and the field is omitted from the wire (never zero-time or partial). Covered.
- **Clock / time-zone attacks.** A non-UTC `now` is normalised to UTC. Year and month boundaries and leap years were probed. No DST exposure exists because everything is UTC.
- **A lying agent** can only change the tooltip. No control path consumes the value (SEC-001).
- **Old agents** (`allow_reboot` absent) resolve to the conditional "if this device permits automatic reboot" wording, not a false "manual" and not a guarantee. Covered by tests.
- **Real repo path.** `GetAllLatestSummaries` projects the whole `data->'updates'` JSON blob and unmarshals it into `UpdateInfo`, so `allow_reboot` flows through the production Postgres path with no migration.
- **Documentation-only (design limitation, ADD-sanctioned).** For a device with `allow_reboot=true` in window mode, the tooltip shows `Est. reboot:` without the "if" qualifier, even when that device has `auto_patch` off or `reboot_class` is not `gated`. The server-wide mode cannot see per-device `auto_patch`, which is not on `DevicePatchInfo`. The mandatory caveat names gating and the other dependencies, and FR-017 and AC-012 are keyed on allow_reboot only. This is acceptable but is a residual honesty gap relative to BR-002; see Notes.
- **Documentation-only.** In `anytime` mode, with allow_reboot unknown, the wording says "Will occur on the next automatic patch run". It carries no timestamp, so FR-017 is not violated, but it is slightly more assertive than the window-mode conditional. The ADD specifies this wording.

## Tests Added by QA

| File | Covers |
|------|--------|
| `internal/agent/telemetry_allow_reboot_test.go` (new, 3 tests) | AC-012 (agent stamp, explicit-false wire, nil safety) |
| `internal/server/handlers/auto_update_window_test.go` (appended, 10 tests) | AC-014, AC-015, AC-016, AC-017, AC-018, AC-019 edge cases |
| `web/src/pages/FleetOverview.test.tsx` (appended, 3 tests) | AC-012 / AC-004 (unknown allow_reboot), AC-007, AC-008 rendered wiring |

All QA tests are marked `Added by QA Engineer` and cite their AC. All pass. Committed as `b31bf0f` with prefix `[REBOOT-ETA-001] test:`.

## Deviations from ADD (senior_dev's 5, judged)

1. **Test-file changes not in ADD §4: ACCEPTABLE.** The one-line `adminRepo` mock fix in `fleet_handler_test.go` was required because `PatchStatus` now calls `loadAutomationConfig`, as the ADD mandates. No assertion was changed. The new `fleet_patch_eta_test.go` is additive.
2. **`formatUtc` returns `string | null`: ACCEPTABLE.** It is strictly safer and implements FR-019 (no "Invalid Date" placeholder). Valid-input output is unchanged, and it is tested.
3. **Anytime wording drops the cooldown parenthetical when no cooldown is set: ACCEPTABLE.** It avoids `~undefined`, matches the ADD when a cooldown is present, and is tested.
4. **`FleetOverview.test.tsx` modified: ACCEPTABLE.** The change is additive. Existing tests are untouched, and the file is the natural home for the page tests.
5. **Glyph indicator (↻) with `role="img"` and `aria-label`: ACCEPTABLE.** The `title` carries the explanation, which satisfies NFR-006. It adds an accessible name and avoids colliding with the panel's `findByText('reboot required')`.

No unflagged deviations found. All files in ADD §4 are implemented. There is no migration and no new dependency. Neither PR #1 files (`commands.go` and `collectors/updates.go`) nor the PR #2 worktree were touched.

## Deviations from FRD
None.

## Verdict Rationale

**PASS WITH NOTES.** All 22 ACs are COVERED. All suites are green (Go 19 packages ok; web 402/402). All three security conditions hold in the actual code. QA closed one real false-coverage gap and several boundary gaps with passing tests. No defects were found in the implementation.

Non-blocking notes for follow-up:
1. Residual BR-002 honesty gap: the window-mode `allow_reboot=true` tooltip does not reflect per-device `auto_patch` or `reboot_class != gated`. A future story could add those fields to the row.
2. The anytime wording is unconditional when allow_reboot is unknown.
3. The AC-022 and AC-002 assertions could be tightened.
4. Operational: allow_reboot appears only after agents upgrade. Until then the UI shows the conditional wording. No collector whitelist change is needed.
