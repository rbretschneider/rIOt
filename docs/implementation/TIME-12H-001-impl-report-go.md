# Implementation Report (Go / server half)

- **Story ID:** TIME-12H-001
- **Engineer:** Senior Dev Agent (backend + infra/docs)
- **Date:** 2026-10-27
- **Detected stack:** Go 1.24 module (toolchain go1.26.0, windows/amd64), chi v5, pgx v5, testify, golang-jwt. No database change. The web half is reported separately.
- **Scope:** every Go file in ADD section 4, the Go tests, the static guards, and `README.md`, `.env.example`, `docker-compose.yml`. Untouched: `web/**`, `CHANGELOG.md`, `web/src/components/ui/README.md`.

## Completed components

| File | Action | Notes |
|---|---|---|
| `cmd/riot-server/main.go` | modified | blank import `time/tzdata` (AD-002) |
| `internal/models/display_time.go` | created | `FormatDisplayFull/Minute/Clock/Date`, `DisplayZoneLabel` (1 to 6 ASCII letters else `GMT[+-H[:MM]]`, C-1); nil zone is UTC; no mutation |
| `internal/models/expect.go` | modified | `OfflineCoverage.Zone`; `Message` uses `FormatDisplayClock`; no `.Local()` |
| `internal/server/config.go` | modified | `DisplayTZRaw`/`TZRaw`, `DisplayZone`, source constants, `ResolveDisplayZone`, `validDisplayZoneName`, `sanitizeZoneValue` (valid UTF-8, strip Cc+Cf, 64 runes). Shape checks run before `LoadLocation`; its error is never logged |
| `internal/server/server.go` | modified | `Server.DisplayZone`; `ResolveDisplayZone` called once in `Start` right after the log handler/budget; `DisplayZone` passed in `expect.Options` |
| `internal/server/router.go` | modified | `HandlerDeps.DisplayZone`; `GET /api/v1/server/time-display` inside the `adminAuth` group |
| `internal/server/handlers/handlers.go` | modified | `HandlerDeps.DisplayZone`, `Handlers.displayZone`, `zone()`; `DeviceSummary` uses `h.clock()`, `h.zone()`, `Render(..., generated, zone)` and `FormatDisplayDate` for the filename |
| `internal/server/handlers/time_display.go` | created | `TimeDisplay` handler: `{"display_zone":...}`, `Cache-Control: no-store` |
| `internal/server/handlers/patch_badge.go` | modified | `patchBadgeInput.Zone`, `rebootFacts.zone`, `asOfText(..., loc)`; the four renders via the helpers; `reboot_at` stays the UTC value |
| `internal/server/handlers/fleet.go` | modified | `Zone: h.zone()` |
| `internal/server/summary/summary.go` | modified | `Render(device, snapshot, generated, zone)`; Generated via `FormatDisplayFull` |
| `internal/server/expect/tracker.go` | modified | `Options.DisplayZone`; `cov.Zone` at the single construction point |
| `internal/server/expect/publisher.go` | modified | `publisher.zone` passed to `heldSummaryMessage` |
| `internal/server/expect/messages.go` | modified | `heldSummaryMessage(n, host, since, loc)` |
| `README.md` | modified | Time Display section rewritten; env table rows `RIOT_DISPLAY_TZ` (preferred) and `TZ`; API row; the "always show UTC" sentence and the l.860 sentence replaced; the four event-message examples in the Expected Activity section (`back by 14:32`, `at 14:02`, `held since <HH:MM>`, `back by 18:01`) updated |
| `.env.example`, `docker-compose.yml` | modified | commented `RIOT_DISPLAY_TZ=America/New_York` with explanation |

Existing tests updated (AD-013, none loosened, deleted or skipped): `models/expect_test.go`, `expect/harness_test.go` (additive `displayZone` option, `local()` uses the helper), `events/generator_expect_test.go`, `handlers/patch_badge_test.go`, `handlers/patch_badge_extra_test.go`, `summary/summary_test.go` (signature only, new `fixedGenerated`).

## Test summary

| AC / condition | Test file | Tests | Status |
|---|---|---|---|
| AC-042..AC-048, C-2, C-4 log phrases | `internal/server/config_display_zone_test.go` | `TestTIME12H_AC042_*` to `AC048_*`, `AC046_*` (subtests for every listed value, `format characters`, `invalid UTF-8`, 64-rune cut, sanitiser table) | pass |
| AC-049 | `config_display_zone_test.go`, `models/expect_time_test.go`, `time12h_guard_test.go` | `AC049_ResolveLeavesTimeLocalUnchanged`, `AC049_LocalIsNeverAdoptedAsTheProcessZone`, `AC049_PoweredOffIgnoresProcessZone`, `AC049_NoProcessLocalZoneInServerText` | pass |
| AC-050, AC-051, C-4 gate 1 | `internal/server/router_time_display_test.go` | `AC050_TimeDisplayRequiresAdminSession` (no cookie, key only, expired JWT, bad-signature JWT, garbage), `AC050_TimeDisplayReportsZone`, `AC050_TimeDisplayRouteIsGetOnly`, `AC050_AuthCheckResponseUnchanged`, `AC051_*` | pass |
| AC-052..AC-056, AC-065 (JSON), AC-066 (window math) | `handlers/patch_badge_time_test.go` | `AC052..AC056_*`, `AC065_PatchStatusTimeFieldsStayUTC`, `AC066_MaintenanceWindowIgnoresProcessAndDisplayZone` | pass |
| AC-057, AC-063 | `summary/summary_time_test.go`, `models/display_time_test.go` | `AC057_*`, `AC063_NumericAbbreviationsNormalised`, `AC063_ZoneLabelRules`, `AC063_FormattedNumericAbbreviations` | pass |
| AC-058 | `handlers/summary_handler_time_test.go` | `AC058_FilenameUsesDisplayZoneDate`, `AC058_FilenameWithoutZoneUsesUTCDate` | pass |
| AC-059, AC-060, AC-062 | `models/expect_time_test.go`, `models/display_time_test.go`, `expect/messages_time_push_test.go` | `AC059_*`, `AC060_*`, `AC062_*` (six instants), `AC059_TrackerStampsDisplayZoneOnPoweredOff` | pass |
| AC-061, AC-064 (push) | `expect/messages_time_test.go`, `expect/messages_time_push_test.go` | `AC061_*`, `AC064_HeldSummaryPushCarriesLabelledTime` | pass |
| AC-064 (not pushed), AC-065 (stored) | `events/generator_time_test.go` | `AC064_PoweredOffEventLabelledAndNeverPushed`, `AC064_ExpectedOfflineEventLabelledAndNeverPushed`, `AC065_StoredEventCreatedAtIsUTC` | pass |
| AC-066 | `partition/plan_time_test.go`, `quota/daily_time_test.go` | `AC066_PartitionPlanIgnoresProcessZone`, `AC066_QuotaDayIgnoresProcessZone` | pass |
| AC-067, C-1 | `models/display_time_test.go` | `AC067_*`, `SEC001_HostileAbbreviationFallsBackToOffset`, `SEC001_LabelPropertyHoldsForEveryOffset` | pass |
| AC-068, AC-069 | `internal/server/time12h_docs_test.go` | `AC068_ReadmeTimeDisplay`, `AC069_ConfigExamplesAndUIReadme`, `AC069_UIReadmeDocumentsTwelveHourForms` | first two pass; **the UI README test is red until `web/src/components/ui/README.md` is updated by the web half** |
| OQ-001, C-4 gate 3 | `internal/server/time12h_guard_test.go` | `OQ001_NoTwentyFourHourHumanLayouts` (+ self-tests), `C4_ResolveDisplayZoneCalledOnlyFromStart`; scanned 170 and 327 files | pass |
| AC-070, AC-073 | QA | `go.mod`, `go.sum` and `cmd/riot-server/migrations` have no diff (`git diff --stat` empty) | n/a |

## Test run output

New tests (`go test ./... -run TIME12H -v`): 67 top-level tests, 66 pass, 1 fail (`TestTIME12H_AC069_UIReadmeDocumentsTwelveHourForms`, waiting on the UI README), 69 passing subtests. Every package with a new test reports `ok` except `internal/server`.

Full run (`go test ./...`, no `-race`, Windows):

```
ok  	.../cmd/riot-signer
ok  	.../internal/agent  26.609s
ok  	.../internal/agent/collectors  13.108s
ok  	.../internal/extraupd
ok  	.../internal/logbudget
ok  	.../internal/maintenance
ok  	.../internal/models
ok  	.../internal/redact
ok  	.../internal/releasekey
ok  	.../internal/resilient
FAIL	.../internal/server   (only TestTIME12H_AC069_UIReadmeDocumentsTwelveHourForms)
ok  	.../internal/server/{auth,ca,commands,db,events,expect,handlers,ingest,middleware,notify,oidc,partition,probes,quota,scoring,storage,summary,updates,websocket}
ok  	.../scripts  62.176s
```

Baseline before any change: every package `ok`.

`go build ./...` and `go vet ./...`: clean. `gofmt -l` on all new files: clean. Modified files that were already unformatted at HEAD (`server.go`, `summary.go`) were not reformatted, to avoid drift.

## Deviations from the ADD

1. **Extra test file** `internal/server/expect/messages_time_push_test.go` (package `expect_test`). `heldSummaryMessage` is unexported, so AC-061 lives in `messages_time_test.go` (package `expect`), while the harness-based AC-064 push test and the tracker-zone test need package `expect_test`. One file cannot hold both packages.
2. `TestTIME12H_AC069_UIReadmeDocumentsTwelveHourForms` is a separate function from `AC069_ConfigExamplesAndUIReadme`, so the Go-owned half (.env.example, compose) is green on its own. It asserts the UI README mentions `12-hour`, `clock`, `formatDateOnly`, `acceptServerZone` and `riot-hour-cycle`; it will pass once the UI README is updated per ADD section 4.
3. The README also updates the four event-message example texts in the Expected Activity section (`back by 14:32`, `at 14:02`, `held since <HH:MM>`, `back by 18:01`). The ADD named only l.860, but the AC-068 test and R-4 intent require the 24-hour examples to go.
4. The guards use `go/parser` instead of regexes, so comments and strings never match. The ADD's patterns and intent are preserved, including the self-tests and the file-count floor.
5. `docker-compose.prod.yml` was not changed (not in ADD section 4).

No blocker file was needed.

## Notes for QA

- **Stale README text left for the technical writer** (outside the ADD's README scope, now partly wrong): features bullet l.27 ("switch between UTC and your browser's local time"), l.58 and l.2824 (patch badge reboot estimate "in UTC") and l.2830 ("As of ... in UTC"). The maintenance-window UTC statements (for example l.3137) are still correct.
- The `time.Local`-changing tests (AC-049, AC-066) are not parallel and restore the zone in `t.Cleanup`.
- `RIOT_DISPLAY_TZ` has no effect on Hawaii until the coordinator adds it to `/home/docker/riot` (RS-1).
- C-4 gate checks: route is inside `adminAuth` (router.go, next to `/api/v1/update/server`); `ResolveDisplayZone` has one non-test caller, `Server.Start`; no captured log record contains `unknown time zone` or `invalid location name`.
- Files are CRLF in the working tree (autocrlf); new files are LF and git will normalise on commit.
