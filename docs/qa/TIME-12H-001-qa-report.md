# QA Report

**Story ID:** TIME-12H-001 (12-hour display time, Server / Local / UTC zones, `RIOT_DISPLAY_TZ`)
**QA Engineer:** QA Agent (Sonnet 5.5)
**Date:** 2026-10-27
**Branch:** `feat/time-12h-001` (reviewed at `57bb526`, base `origin/main`)
**Verdict:** PASS WITH NOTES

No blocking findings. No production file was changed by QA (three temporary one-line mutations were applied to production files to prove the tests bite, and each was reverted with `git checkout -- <file>`; the tree was clean afterwards apart from the two new QA test files). Nothing was committed.

---

## Test Run Summary

Windows host, `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`. All runs by QA, independent of the orchestrator's runs.

| Run | Result |
|---|---|
| `go build ./...` | exit 0, no output |
| `go vet ./...` | exit 0, no output |
| `go test -count=1 ./...` (no `-race`, Windows) | 31 of 31 packages with tests `ok` (`internal/server` 4.6 s, `handlers` 9.5 s, `models`, `expect`, `summary`, `events`, `partition`, `quota`, `scripts` 67.6 s, all `ok`). No FAIL, no skip. The Go implementation report's red `TestTIME12H_AC069_UIReadmeDocumentsTwelveHourForms` is green now that the web half updated the UI README |
| `cd web && npx tsc --noEmit` | exit 0, no output |
| `cd web && npm run test:run` | 126 files, **2291 tests passed** (125 files / 2282 tests from senior_dev plus the 1 file / 9 tests QA added). Baseline on `main` was 116 files / 2034 tests, so the count is higher, not lower |
| `npx vite build` and `VITE_DEMO=true npx vite build` (output redirected to scratch) | both built, exit 0 |
| Go tests with `-race` | not run (Windows, no CGO). Not required by the story; no goroutine code was added except the tracker stamping a pointer |

Flaky tests: none observed. `internal/server`, `models`, `handlers`, `expect`, `summary`, `events`, `partition`, `quota` were each run three or more times (cached, then `-count=1`, then again after QA tests were added) with identical results.

AC-070 / AC-073: `git diff --name-only origin/main...HEAD` lists no path under `cmd/riot-server/migrations/` and none of `go.mod`, `go.sum`, `web/package.json`, `web/package-lock.json`.

## AC Coverage Audit

"Would the tests catch it?" was verified by mutation for the load-bearing rules (see "Mutation check"), and by reading the assertions of every Go test and the web tests for AC-014, 018, 020, 029, 071, 072 and the guards. Test ids: the Go tests are named `TestTIME12H_ACxxx_*`; the web tests are `describe('TIME-12H-001 [AC-0xx] ...')`. Files as in ADD section 8: `C` = `config_display_zone_test.go`, `RT` = `router_time_display_test.go`, `SG` = `time12h_guard_test.go`, `D` = `time12h_docs_test.go`, `DT` = `models/display_time_test.go`, `MT` = `models/expect_time_test.go`, `PB` = `handlers/patch_badge_time_test.go`, `SM` = `summary/summary_time_test.go`, `SH` = `handlers/summary_handler_time_test.go`, `XM` = `expect/messages_time_test.go`, `XP` = `expect/messages_time_push_test.go`, `G` = `events/generator_time_test.go`, `PT` = `partition/plan_time_test.go`, `QT` = `quota/daily_time_test.go`, `WF` = `utils/format.time12h.test.ts`, `WP` = `TimePreferenceProvider.time12h.test.tsx`, `WH` = `HeaderClock.time12h.test.tsx`, `WS` = `ServerZoneSync.test.tsx`, `WT` = `Timestamp.time12h.test.tsx`, `WC` = `time12h.callsites.test.tsx`, `WA` = `ActivityLabel.time12h.test.tsx`, `WCR` = `cron.test.ts`, `WDD` = `DeviceDetail.time12h.test.tsx`, `WD` = `demo-time.test.ts`, `WCW` = `chartTimeWiring.test.tsx`, `WG` = `test/static-guards.test.ts`, `QA` = the two QA files.

| AC | Status | Tests | Gap |
|---|---|---|---|
| AC-001..AC-004 | COVERED | WF `[AC-001]` to `[AC-004]` | exact strings from the FRD |
| AC-005 | COVERED | WF `[AC-005]`; `format.test.ts` `[UIF AC-104] [TIME-12H-001 AC-005]` | 24-hour forms pinned |
| AC-006, AC-007 | COVERED | WF `[AC-006]`, `[AC-007]`; `format.test.ts` `[UIF AC-105] [TIME-12H-001 AC-007]` | |
| AC-008, AC-009 | COVERED | WF `[AC-008]`, `[AC-009]`; QA `[AC-008] [AC-009]` (clock and time forms, 12:59:59, 1:00 PM) | |
| AC-010 | COVERED | WF `[AC-010]`; QA `[AC-011] [AC-012]` date-only case | |
| AC-011, AC-012 | COVERED | WF `[AC-011]`, `[AC-012]`; QA adds the clock, time, 24-hour and tick forms | |
| AC-013 | COVERED | WF `[AC-013]`; WCW `[UIF AC-118] [TIME-12H-001 AC-013]` (both chart components, 12h, 24h, midnight) | |
| AC-014 | COVERED | WF `[AC-014]`: every precision x zone x cycle for the three inputs, plus `formatDateOnly`; asserts no throw and `—` | |
| AC-015, AC-016 | COVERED | WF `[AC-015]`; WT `[AC-016]` | |
| AC-017 | COVERED | WP `[AC-017]`; WH `[AC-017]` | |
| AC-018 | COVERED | WF `[AC-018]` (all 12 listed values plus object, 4 segments, digit-led segment); WS `[AC-018]` | |
| AC-019 | COVERED | WS `[AC-019]` (same node, no remount) | |
| AC-020 | COVERED | WF `[AC-020]` (Intl spy separates `{timeZone}`-only acceptance from formatting calls); WG `[AC-020]`; WP `[AC-029]` for `America/Chicago` from storage | |
| AC-021..AC-026 | COVERED | WH `[AC-021]` to `[AC-026]` | |
| AC-027, AC-028, AC-029 | COVERED | WP `[AC-027]` (6 combos), `[AC-028]`, `[AC-029]` (5 cases, `Intl` spy for `America/Chicago`) | |
| AC-030 | COVERED | WH `[AC-030]` (Tab, Enter, arrows, Escape, focus on clock) | |
| AC-031 | COVERED | `docs/implementation/TIME-12H-001-screenshots/after-header-time-panel.png`, viewed by QA: both groups, Server (America/New_York) and 12-hour checked, clock `10:24:50 AM EDT` | The demo shows `Local (America/New_York)` because the capture browser was in New York; expected |
| AC-032, AC-033 | COVERED | WC `[AC-032]`, `[AC-033]` | |
| AC-034, AC-035 | COVERED | WA `[AC-034]`, `[AC-035]`; `FleetOverview.activity`, `DeviceDetail.activity` updated to `... UTC` | |
| AC-036 | COVERED | WDD `[AC-036]` (ISO and `1793120709`); WCR; WG `[AC-036]` argument-count guard with self-tests | |
| AC-037, AC-038 | COVERED | WCR `[AC-037]`, `[AC-038]` (all 11 rows) | `*-*-01` row: see Note N4 |
| AC-039 | COVERED | WC `[AC-039]` | |
| AC-040, AC-041 | COVERED | WD | |
| AC-042..AC-045 | COVERED | C `AC042_*`, `AC043_*`, `AC044_*`, `AC045_*` (info count, warn count, source attribute asserted) | |
| AC-046 | COVERED | C `AC046_*`: 11 listed values plus `format characters`, `invalid UTF-8`, a 500-rune value and a sanitiser table. Asserts exactly one warning, `source`, the exact logged value, <= 64 runes, no Cc/Cf, no LoadLocation text. QA `AC046_AdversarialInputsStayBounded` adds 5000-segment, 5000 x U+202E, NUL, SQL, HTML, percent-encoded, double-slash, trailing and leading slash, embedded space, BOM | |
| AC-047, AC-048 | COVERED | C `AC047_*`, `AC048_*` (two warnings, one info) | |
| AC-049 | COVERED | C `AC049_ResolveLeavesTimeLocalUnchanged`, `AC049_LocalIsNeverAdoptedAsTheProcessZone`; MT `AC049_PoweredOffIgnoresProcessZone` (`time.Local` = Asia/Tokyo); SG `AC049_NoProcessLocalZoneInServerText` (go/parser, 170+ files, floor asserted) | |
| AC-050 | COVERED | RT `AC050_TimeDisplayRequiresAdminSession` (no session, key only, expired JWT, bad-signature JWT, garbage; each 401 with exact body and no zone disclosure), `AC050_TimeDisplayReportsZone` (exact body, `no-store`, content type), `AC050_TimeDisplayRouteIsGetOnly`, `AC050_AuthCheckResponseUnchanged` | |
| AC-051 | COVERED | RT `AC051_FallbackZoneReportedAsUTC`, `AC051_UnsetZoneReportedAsUTC` | |
| AC-052..AC-056 | COVERED | PB `AC052`..`AC056` (exact strings; AC-054 marshals and checks `reboot_at` = `2026-10-28T02:00:00Z`; AC-055 both ages; nil zone is UTC) | |
| AC-057 | COVERED | SM `AC057_GeneratedInDisplayZone`, `AC057_NilZoneIsUTC` | |
| AC-058 | COVERED | SH `AC058_FilenameUsesDisplayZoneDate` (injected clock `2026-10-28T02:00:00Z`), `AC058_FilenameWithoutZoneUsesUTCDate`; WDD `[AC-058]` | |
| AC-059..AC-061 | COVERED | MT `AC059_*`, `AC060_*`; XM `AC061_*`; XP `AC059_TrackerStampsDisplayZoneOnPoweredOff` | |
| AC-062 | COVERED | MT `AC062_MidnightNoonAndDST` (six instants); DT `AC062_*`; QA `AC062_DSTEdgesInFullForm` (full form) | |
| AC-063 | COVERED | SM `AC063_NumericAbbreviationsNormalised`; DT `AC063_ZoneLabelRules`, `AC063_FormattedNumericAbbreviations`; QA `AC063_ResolvedZonesFormatLikeTheDashboard` | |
| AC-064 | COVERED | XP `AC064_HeldSummaryPushCarriesLabelledTime` (push path); G `AC064_PoweredOffEventLabelledAndNeverPushed`, `AC064_ExpectedOfflineEventLabelledAndNeverPushed` | |
| AC-065 | COVERED | PB `AC065_PatchStatusTimeFieldsStayUTC` (walks the whole JSON, every RFC 3339 value ends in `Z`, asserts `reboot_at`, `next_window_start`, `updates_at` were exercised); G `AC065_StoredEventCreatedAtIsUTC` | |
| AC-066 | COVERED | PT, QT, PB `AC066_*` with `time.Local` = Pacific/Kiritimati, restored in `t.Cleanup`, not parallel | |
| AC-067 | COVERED | DT `AC067_NilZoneFormatsUTCWithoutPanic` (all four helpers), `AC067_HelpersDoNotMutateTheirInput` | |
| AC-068, AC-069 | COVERED | D (`AC068_ReadmeTimeDisplay`, `AC069_ConfigExamplesAndUIReadme`, `AC069_UIReadmeDocumentsTwelveHourForms`); QA read the README, `.env.example`, `docker-compose.yml` diffs | |
| AC-070, AC-073 | COVERED | QA `git diff` check (empty) | |
| AC-071 | COVERED | WF `[AC-071]`: 1,000 calls per option/cycle/precision plus 1,000 rejected zones, and 20 distinct accepted zones, each asserting `<= 12`. The 20-zone test is the one that actually exercises eviction | |
| AC-072 | COVERED | WP `[AC-072]`: fake timers, 5 s, asserts the clock did tick and the consumer render count is unchanged; plus a same-server-zone case | |
| AC-074 | COVERED | QA run above. No `.skip`, `.only`, `.todo`, `t.Skip` added (`git diff -U0` grep empty). `it(` and `describe(` counts per pre-existing test file are equal or higher. Every `[UIF AC-104]`, `[UIF AC-105]`, `[UIF AC-113]` to `[UIF AC-118]` describe still exists (grep) | |

Result: 74 of 74 ACs COVERED, none PARTIAL, none MISSING.

## Security conditions

| Condition | Result |
|---|---|
| C-1 hostile abbreviation | Implemented in `internal/models/display_time.go` (`isZoneAbbreviation`: 1 to 6 ASCII letters, byte by byte, else `GMT[+-H[:MM]]`). `TestTIME12H_SEC001_HostileAbbreviationFallsBackToOffset` builds real TZif blobs with `LoadLocationFromTZData` for CRLF injection, `<!channel>`, space, non-ASCII letter, 7 letters and 200 bytes, asserts the zone database really returned the hostile name (so the test is not vacuous), the `GMT-4` label, and <= 9 bytes; a second subtest keeps EDT/EST/IST/UTC/GMT/BST. `SEC001_LabelPropertyHoldsForEveryOffset` sweeps -14h..+14h in 15-minute steps. Mutation M2 below makes four tests fail |
| C-2 sanitiser | `sanitizeZoneValue` in `config.go`: `ToValidUTF8`, drops Cc and Cf, stops at 64 runes. Tests: `format characters` (U+202E, U+2066, U+200B, U+FEFF), `invalid UTF-8` (exact `\ufffd`), the all-Cc/Cf hostile string, 500 x `é`, the table. Mutation M3 (drop the Cf test) fails two tests |
| C-3 AcceptedZone and `localTimeZone` guard | `WG [SEC-003]`: cast regex covers `as AcceptedZone`, `as unknown as AcceptedZone`, `<AcceptedZone>x`; exactly one cast in `format.ts`, in `acceptServerZone`; `localTimeZone` named only by the provider and `format.ts`; never a JSX attribute in production. Each pattern has a positive and a negative self-test. Mutations W1 (cast in `HeaderClock.tsx`) and W2 (`localTimeZone=` prop in `ActivityLabel.tsx`) are caught (1 and 2 failures). D-2 is judged below |
| C-4 gate 1: admin-only endpoint | `r.Get("/api/v1/server/time-display", h.TimeDisplay)` is inside the `adminAuth` group (`router.go`, next to `/api/v1/update/server`). RT covers no cookie, key only, expired JWT, bad-signature JWT, garbage: all 401 with the exact `{"error":"unauthorized"}` body and no zone text; valid session gives 200, exact body, `Cache-Control: no-store`. Mutation M1 (route moved outside `adminAuth`) fails `AC050_TimeDisplayRequiresAdminSession` |
| C-4 gate 2: shape checks before `LoadLocation`; no LoadLocation text in logs | `ResolveDisplayZone` calls `validDisplayZoneName` (length, `Local`, regex) first and never logs the `LoadLocation` error. `assertNoLoadLocationText` runs in AC-046, AC-047, AC-048 and the QA adversarial test. Mutation M5 (log the `LoadLocation` error) fails AC-046 and AC-048 |
| C-4 gate 3: resolved once, from `Start` only | One non-test caller, `server.go` `Start` (after the DB log handler is installed). `setupRouter` only reads `s.DisplayZone`. `SG C4_ResolveDisplayZoneCalledOnlyFromStart` parses cmd and internal (go/parser) and requires exactly one call, in `Start` in `server.go`. Mutation M4 (call it from `setupRouter`) fails it. AC-048 pins the log counts (2 warn, 1 info) |
| C-4 gate 4 | AC-054, AC-065, AC-066 green (see audit) |
| C-4 gate 5 | AC-018, AC-020, AC-071 green; the `[UIF SEC-005]` HTML-sink guard is part of the green 70-test guard file |
| INFO-5 | `WS [NFR-006]`: 404 and `200 text/html` (the `res.json()` rejection) both leave Server as UTC; verified in `ServerZoneSync.test.tsx` and in the `fetchJSON` path |

## Mutation check

Each mutation was a single-line change to a production file, run against the relevant tests, then reverted with `git checkout -- <file>`.

| Id | Mutation | Tests that failed |
|---|---|---|
| M1 | Register `/api/v1/server/time-display` outside the `adminAuth` group | `AC050_TimeDisplayRequiresAdminSession` |
| M2 | `DisplayZoneLabel` always returns the raw database abbreviation | `AC063_ZoneLabelRules`, `AC063_FormattedNumericAbbreviations`, `SEC001_HostileAbbreviationFallsBackToOffset`, `SEC001_LabelPropertyHoldsForEveryOffset` |
| M3 | `sanitizeZoneValue` stops stripping Cf runes | `AC046_InvalidValuesRejectedSanitizedOneWarning`, `AC046_SanitizeZoneValue` |
| M4 | `ResolveDisplayZone` called from `setupRouter` | `C4_ResolveDisplayZoneCalledOnlyFromStart` |
| M5 | Log the `LoadLocation` error | `AC046_InvalidValuesRejectedSanitizedOneWarning`, `AC048_BothInvalidTwoWarningsOneInfo` |
| W1 | `x as AcceptedZone` appended to `HeaderClock.tsx` | `[SEC-003]` cast guard |
| W2 | `<TimePreferenceProvider localTimeZone="..." />` appended to `ActivityLabel.tsx` | both `[SEC-003]` seam guards |

## Deliberately updated pre-existing tests (step 4)

Compared against `origin/main`:
- No pre-existing test was deleted or skipped: per-file `it(`/`test(` counts are equal or higher in all 10 modified web test files (`HeaderClock.test.tsx` 14 to 14, `TimePreferenceProvider.test.tsx` 15 to 15, `format.test.ts` 49 to 49, `FleetOverview.activity` 15 to 15, `DeviceDetail.activity` 7 to 7, `Timestamp` 6, `RelativeTime` 7, `chartTimeWiring` 4 to 7, `cron` 27 to 40, `static-guards` 32 to 49). The removed `it(`/`describe(` lines in the diff are renames (titles reworded to name the zone, cycle or panel), each re-added with the same body shape.
- No `toBe` was weakened to `toContain` or a regex. The changed assertions are exact strings in the new form: `Time display: UTC, 24-hour. Change time display`, `2026-10-17 2:03:22 PM UTC`, `Rebooting, back by 14:15 UTC`, `... · started 14:00 UTC`. The tests that kept 24-hour strings were pinned by seeding `UTC` + `24h` (or `Local` + `24h`) in storage, or by `hourCycle: '24h'` in the zone constants.
- Go: `models/expect_test.go`, `handlers/patch_badge_test.go`, `patch_badge_extra_test.go`, `summary/summary_test.go`, `events/generator_expect_test.go`, `expect/harness_test.go` changed to the new exact strings or the new signature; none loosened.
- One substantive semantic change to an old test is expected and correct: `[UIF AC-116]`/`[UIF SEC-006]` now expect the Server default (12-hour) where they used to expect UTC (the default itself changed, FR-040).

## Tests Added by QA

| File | Covers |
|---|---|
| `web/src/utils/format.time12h.qa.test.ts` (new, 9 tests) | AC-063/FR-034 dashboard labels for Sao_Paulo, Kathmandu, Kolkata, `Etc/GMT+5`; AC-011/AC-012 clock, time, 24-hour and tick forms across the DST edges, plus the date-only day boundary; AC-008/AC-009 clock and time forms around midnight and noon |
| `internal/server/config_display_zone_qa_test.go` (new, 3 tests, 12 subtests) | AC-063 resolved zones (Sao_Paulo, Kathmandu, Kolkata, `Etc/GMT+5`, a 3-segment zone, UTC) format like the dashboard; AC-062 DST fall-back, spring-forward, midnight and noon in the full form; AC-046 adversarial inputs (5,000-segment, 5,000 RLO, NUL, SQL, HTML, percent-encoded, empty segments, space, BOM) each give UTC, exactly one warning, <= 64 runes and no LoadLocation text |

Both files carry the `Added by QA Engineer` header and pass. They are uncommitted and untracked.

## Test Quality Findings

- **No false coverage found.** The assertions I read are exact strings or exact counts, and the seven mutations above were each caught. The two places where a bound could be trivially met (AC-071's 1,000-call loop uses only three cache keys) are backed by the 20-zone eviction test.
- **Guards are self-tested and floor-checked** (`minGuardedFiles = 100` in Go; non-empty scan assertions and argument-counter self-tests in web), so a wrong root cannot turn a guard into a no-op.
- **Local test-seam tests and `time.Local`**: AC-049 and AC-066 mutate the process-wide `time.Local`. They are not parallel and restore it in `t.Cleanup`; no order dependence observed.
- **Minor, non-blocking.** `TestTIME12H_AC050_AuthCheckResponseUnchanged` builds a router with only `AuthCheck`, because the endpoint is new (DV-8 in the ADD). It proves `auth/check` is unchanged but not that the full router is; acceptable.
- **Minor, non-blocking.** `AC-072`'s consumer test relies on `useTimePreference()` identity, so it would not catch a regression that re-renders only `Timestamp` children. The `Timestamp.time12h` and provider-memo tests cover that side adequately.

## Adversarial Findings

No vulnerability found. Probes run (Go via `ResolveDisplayZone`, web via `acceptServerZone`):

- Zone name inputs: `../../etc/passwd`, `/etc/passwd`, `:/etc/localtime`, `C:\Windows`, `America/New_York;rm -rf /`, `America//New_York`, trailing and leading slash, `A/B/C/D` (4 segments), digit-led segments, percent-encoded dots, SQL and HTML payloads, NUL, BOM, 65 letters, 5,000 segments, 500 multibyte runes, bidi override runs. All reject to UTC with one bounded, sanitised warning (Go) or `null` (web), never panic, never throw.
- `Local`, empty and whitespace-only: `Local` rejected with one warning (no adoption of the process zone); empty and whitespace are unset and silent. A value that is only whitespace plus U+2028 trims to a valid name (trim is `strings.TrimSpace`, as AC-043 specifies); this is correct, not a finding.
- Values accepted that a reviewer might not expect (all harmless, display only): `GMT`, `Zulu`, `Etc/UTC`, `Factory` (label `GMT`). Rejected: `utc`, `UTC+5`, `localtime`, `posixrules`, `america/new_york` (case-sensitive database), `posix/America/New_York` (not in the alpine image).
- Browser: a hostile API zone (`Mars/Olympus`, markup, 65 chars, number, null, object, `Local`) never reaches a formatting `Intl.DateTimeFormat` call (spy test); a zone name in `localStorage` is never read as a zone (provider reads only the enum strings).
- Auth: expired JWT, bad-signature JWT, device key only and no session on `GET /api/v1/server/time-display` all 401 with the generic body; POST gives 405.
- State: a second report of the same server zone does not re-render consumers; legacy `UTC` maps to Server and a post-story UTC choice persists (AC-028); `setItem` failing mid-pair cannot turn a legacy UTC into a post-story choice (zone is written first, cycle only if that succeeded).
- Partial failure: when the server zone request fails (404, HTML 200, network), Server renders as UTC and the panel reads `Server (UTC)`.

## Bounded growth (step 7)

- No migration, no table, column, index or file; `go.mod`, `go.sum`, `package.json` and the lockfile unchanged. `DisplayZone` is one `*time.Location` set once per process.
- Logs: at most 1 info and 2 warnings per process start, each value <= 64 runes (AC-046/AC-048 counts, `C4_` guard keeps the resolver out of `setupRouter`, which `applyTLSAndRestart` re-runs). Nothing per request or per render.
- Dashboard formatter cache capped at 12 entries with oldest-first eviction (`FORMATTER_CACHE_LIMIT`, tested with 20 distinct zones and 1,000 rejected strings); `acceptServerZone` caches nothing, so rejected strings cannot grow memory. The `['time-display']` query holds one small object.

## Deviations from ADD

None blocking. The senior_dev deviations are judged here:

| # | Deviation | Judgement |
|---|---|---|
| Go 1 | Extra `expect/messages_time_push_test.go` | Accepted. `heldSummaryMessage` is unexported (package `expect`), while the harness-based push test needs `expect_test`; one file cannot hold both |
| Go 2 | UI README half of AC-069 is a separate test function | Accepted. Green now; keeps the Go-owned half independent |
| Go 3 | README event-message examples updated beyond l.860 | Accepted and correct; the 24-hour examples would otherwise contradict the new text |
| Go 4 | `go/parser` instead of regex guards | Accepted, an improvement: comments and strings cannot cause false hits. Self-tests and the file-count floor are retained. The `.Local()` guard matches any zero-argument method named `Local`, which is stricter than needed but safe |
| Go 5 | `docker-compose.prod.yml` untouched | Accepted; not in ADD section 4 |
| Web D-1 | `renderWithProviders` pins UTC/24-hour when `timeZone.storage` is omitted (not in ADD section 4) | Accepted with a note. The alternative was rewriting every page test, which AD-013 forbids, and the helper's old `?? null` default would have broken six page-test files. The new call-site tests opt out explicitly (`storage: null`, `serverZone: 'America/New_York'`), so the real defaults are still exercised at page level. Risk: a future page test using the helper no longer sees the product default unless it opts out; the helper's doc comment says so. Architect should confirm |
| Web D-2 | SEC-003 guard exempts `src/test/*` for the `localTimeZone` checks | Accepted. `test/renderWithProviders.tsx` and `setup.ts` are test-support (they exist to drive that seam); every production file is still scanned, and the `AcceptedZone` cast check has no exemption. W2 above shows the guard still bites on a production file |
| Web D-3 | `Monthly on day 1` (FRD 7.5) vs `day 01` (unchanged code) | Accepted; see Note N4 |
| Web D-4..D-6 | `tabIndex={-1}` on the panel root; UTC formatter fallback for a rejected Local seam zone; effect on the reported value rather than on `data` | Accepted, each harmless and consistent with the FRD intent |

## Deviations from FRD

None blocking. FRD 7.5 and AC-037/AC-038 contain an internal inconsistency that the implementation resolves in favour of the "pre-existing tests pass unchanged" clause (Note N4).

## Notes (non-blocking, for the technical writer and Ryan)

- **N1. Stale README text** (also flagged by senior_dev): `README.md` l.27 ("switch every timestamp between UTC and your browser's local time"), l.58 (patch badge reboot estimate "in UTC") and l.2877 (As of "in UTC") are now wrong: the badge text is in the display zone, 12-hour, labelled. The AC-068 sentences are replaced, but these three lines contradict the new behaviour. Technical writer to fix.
- **N2. Label difference between surfaces is by design (FRD DEC-7).** For zones without an `en-US` short name, the server writes the database abbreviation and the dashboard writes `Intl` text: `Asia/Kolkata` is `IST` in server text and `GMT+5:30` on the dashboard. The QA web test pins the dashboard side. No action.
- **N3. Hawaii.** `RIOT_DISPLAY_TZ` has no effect until the coordinator adds it to `/home/docker/riot`. If that compose file already sets `TZ`, the new fallback chain (`RIOT_DISPLAY_TZ`, then `TZ`) adopts it as the display zone immediately on deploy, so a stray `TZ` value on Hawaii would change the Server zone. Check `TZ` there before deploying.
- **N4. FRD erratum.** FRD 7.5 lists `*-*-01 00:00:00` as `Monthly on day 1 at midnight`, but the code echoes the day text verbatim and the pre-existing `cron.test.ts` l.125 (pinned by AC-038) expects `Monthly on day 01 at midnight`. senior_dev kept the old behaviour and tests `*-*-1` for that row. Correct call; the FRD table should say `day 01`. A stale comment in `cron.ts` l.58 (pre-existing) also says `day 1`.
- **N5.** Safari does not focus a button on click, so clicking the clock while the panel is open can close and reopen it (senior_dev note; same as the existing `Popover`). Not tested; low impact.
- **N6.** `-race` was not run on Windows. Ryan's Linux CI runs it.

## Verdict Rationale

**PASS WITH NOTES.** All 74 ACs are covered by tests that fail when the AC is violated (verified by mutation for the security-relevant and structural ones). Both suites, `go vet`, `tsc` and both production builds are green. All four security conditions C-1 to C-4 are implemented and tested. No migration, dependency or stored-state change. No pre-existing test was deleted, skipped or loosened. The only findings are documentation staleness (N1), an FRD erratum (N4), and operational checks for deployment (N3).

## Action Required (if FAIL)

None.
