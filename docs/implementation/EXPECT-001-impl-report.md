# Implementation Report (consolidated)

- **Story ID:** EXPECT-001 (expected-activity windows)
- **Engineer:** Senior Dev Agent (Go backend and database; web by the frontend engineer)
- **Date:** 2026-10-26
- **Branch:** `feat/expected-activity-001`
- **Replaces nothing:** `EXPECT-001-impl-report-part1.md` (WP-1, WP-3, WP-4) and `EXPECT-001-impl-report-web.md` (WP-7) stay as written. This report consolidates them with WP-2, WP-5, WP-6 and WP-8.

## Detected stack

- Go 1.24 (`go.mod`), module `github.com/DesyncTheThird/rIOt`; chi v5, pgx v5, golang-migrate, `log/slog`; `testing` plus testify. Migrations in `cmd/riot-server/migrations`. PostgreSQL 16 in production; PG-gated tests ran on a throwaway `postgres:17-alpine` container (removed afterwards).
- Web: React 19 + TypeScript (strict), Tailwind 4, Vite, TanStack Query 5, Vitest 4 (happy-dom). No lint script exists; `tsc --noEmit` is the static check.
- No new dependency. No agent, `install.sh`, sudoers or environment-variable change (nothing to document in `.env.example`).

## Commits

| Part | SHA | Message |
|---|---|---|
| Docs | 0248c56, 7585021, 6a9b0dd, 2859c40 | FRD, ADD, security review, ADD rev 2 |
| WP-7 web | 5cfe205, 1d58d4f, 59aae4e, 93c06f5, e7b6347 | activity types and hooks; labels on the pages; refetch race fix; tests; web report |
| WP-1 | c0c3641 | migration 000033 |
| WP-1 | 924d698 | models, repository, mocks, deletion exemptions |
| WP-3 | ba52d82 | `commands.Service` observer hook |
| WP-4 | 8ef32ae, 46ea168 | `expect` package; harness and AC / SEC-001 to SEC-006 tests |
| Part 1 report | 062cf86 | implementation report part 1 |
| Retired tests (X-13) | 6d5fd20 | retire per-device update downgrade tests (FR-014) |
| WP-2 | e64d223, 8eed309 | generator; generator oracle tests |
| WP-2 / WP-4 | 36137af | the expect harness now drives the real `events.Generator` |
| WP-5 | bc1007c | hub `BroadcastActivity` and `activity_update` |
| WP-6 | 8904ba6, a760c61 | activity endpoint, registration hook, event keys, fleet actors; handler tests |
| WP-5 | 31c592d, f35d824, 9528f70 | offline detector, restart series, server wiring and route; tests; clock race fix |
| WP-8 | 2e18bd2 | AC-047 static scan |
| Contract | 4d54401 | activity envelope and one-entry-per-device test |

## Completed components (ADD section 4)

| WP | File | Action | Notes |
|---|---|---|---|
| WP-1 | `cmd/riot-server/migrations/000033_expected_activity.up.sql` / `.down.sql` | created | Part 1 |
| WP-1 | `internal/models/expect.go`, `expect_test.go`; `commands.go`, `events.go` | created / modified | Part 1 |
| WP-1 | `internal/server/db/expect_repo.go`, `expect_repo_pg_test.go`, `interfaces.go`, `command_repo.go`, `docker_retention.go` | created / modified | Part 1 |
| WP-1 | `internal/server/storage/command_budget.go`, `command_budget_expect_pg_test.go` | modified / created | Part 1 |
| WP-1 | `internal/testutil/mocks_expect.go`, `mocks.go` | created / modified | Part 1 (deviation 1) |
| WP-3 | `internal/server/commands/service.go`, `observer_test.go` | modified / created | Part 1 |
| WP-4 | `internal/server/expect/{tracker,window,finish,escalate,publisher,activity,messages,tick}.go` and `*_test.go` | created | Part 1; `tick.go` is extra (deviation 3) |
| WP-2 | `internal/server/events/generator.go` | modified | `ExpectedActivity`, `SetExpectedActivity`, `SetClock`, `DeviceOfflineAt`, `PrepareEscalation`, `PublishEscalation`; `activeUpdates`, `updateStarted`, `recentDisruptiveCommand`, `disruptiveActions` removed; labels |
| WP-2 | `internal/server/events/generator_expect_test.go` | created | 30 tests with an oracle |
| WP-2 | `internal/server/events/generator_integration_test.go` | modified | Two tests retired (below) |
| WP-5 | `internal/server/websocket/hub.go` | modified | `BroadcastActivity`; `data` is an explicit null when the activity ends |
| WP-5 | `internal/server/offline.go`, `offline_test.go` | created | `offlineDetector`, `restartSeriesGrace` |
| WP-5 | `internal/server/server.go` | modified | Restart series, `Expect` tracker, `Load`, `go Expect.Run`, detector; `offlineDetectionWorker` and `checkOfflineDevices` removed |
| WP-5 | `internal/server/router.go` | modified | `GET /api/v1/activity` in the admin group; `HandlerDeps.Activity` assigned only when non-nil |
| WP-6 | `internal/server/handlers/handlers.go` | modified | `HandlerDeps.Activity`; `NoteRegistration` on every re-registration, before `DeviceOnline` |
| WP-6 | `internal/server/handlers/activity.go`, `activity_test.go` | created | `ActivitySource`, `GetActivity` |
| WP-6 | `internal/server/handlers/docker_attribution.go`, `docker_attribution_keys_test.go` | modified / created | Keys before the nil-repo return, into `e.TargetKeys` |
| WP-6 | `internal/server/handlers/fleet.go`, `fleet_actor_test.go` | modified / created | `dockerDispatchRequest` for both fleet dispatches |
| WP-8 | `internal/server/expect/safety_test.go` | created | AC-047 |
| WP-7 | all web files of ADD section 4 | see `EXPECT-001-impl-report-web.md` | |
| Docs | `README.md`, `CHANGELOG.md`, `internal/server/expect_docs_test.go` | not done | Technical writer (AC-048) |

### The two retired tests (ADD 12 note 8, X-13)

`TestCheckDockerEvent_DieDowngradedDuringUpdate` and `TestCheckDockerEvent_DieNoNotifyDuringUpdate` in `internal/server/events/generator_integration_test.go`, commit 6d5fd20. They test the per-device `activeUpdates` downgrade that FR-014 removes. Nothing else in that file changed (`TestCheckDockerEvent_DieWarningAfterUpdateCompletes` still passes: a death after `update_completed` is a warning).

### Rollback note (INFO-3)

After `000033` down then up, the escalation markers of commands that finished in the last hour are gone, so those commands can escalate a second time. The down migration drops only the new state. Up, down, up is run by `TestEX_Repo_PG_MigrationUpDownUp`.

## Test summary

195 Go tests named `TestEX_` pass (all PG-gated ones included, 11 of them); 0 fail, 0 skip. By package: models 12, commands 1, db 10, storage 1, events 30, expect 100, handlers 19, websocket 2, server 20. Web: 1813 tests in 101 files (the web report has the per-file counts).

H = `internal/server/expect/harness_test.go` (package `expect_test`; a real `Tracker`, a real `commands.Service` and a real `events.Generator` over the mocks). The H files are `container_test.go`, `host_test.go`, `agent_shutdown_test.go`, `durability_test.go`, `sec001_test.go`; W = `window_test.go`; E = `escalate_test.go`; F = `finish_test.go`; P = `publisher_test.go`; A = `activity_test.go`; G = `internal/server/events/generator_expect_test.go`; O = `internal/server/offline_test.go`; HD = `internal/server/handlers/`.

### Acceptance criteria

| AC | Test(s) | Status |
|---|---|---|
| AC-001 | H `TestEX_AC001_DockerUpdateDeathRecordedNotAlerted` | PASS |
| AC-002 | H `TestEX_AC002_ScheduledUpdateNamesPolicy`; models `TestEX_AC002_ActorPhraseOrigins` | PASS |
| AC-003 | H `TestEX_AC003_RestartStopAndStackActionsCovered` | PASS |
| AC-004 | W `TestEX_AC004_BulkWindowCoversThreeHours`; H `TestEX_AC004_BulkDeathAtThreeHoursSuppressed` | PASS |
| AC-005 | H `TestEX_AC005_NonTargetContainerAlerts` | PASS |
| AC-006 | H `TestEX_AC006_DeathBeforeUpdateStartedSuppressed`, `TestEX_AC006_NoUpdateStartedSuppressed`; G `TestEX_FR014_UpdateStartedNoLongerDowngradesDeaths` | PASS |
| AC-007 | W `TestEX_AC007_ContainerSettleBoundary`; H `TestEX_AC007_DeathAfterSettleAlerts` | PASS |
| AC-008 | H `TestEX_AC008_ActionsOutsideTableAlert`, `TestEX_AC008_ActionsOutsideTableOpenNoWindow`; models `TestEX_AC008_PolicyTableExact`, `TestEX_AC008_MigrationIndexPredicateEqualsActions` | PASS |
| AC-009 | H `TestEX_AC009_OOMAlertsAndNamesAction`; G `TestEX_AC009_OOMStillAlertsAndNamesTheAction` | PASS |
| AC-010 | H `TestEX_AC010_ExcludedContainerSameInsideAndOutside`; G `TestEX_G_ExcludedContainerNeverReachesTheTracker` | PASS |
| AC-011 | H `TestEX_AC011_RebootAfterDpkgWaitNoAlert` | PASS |
| AC-012 | H `TestEX_AC012_TwelveDeathsDuringReboot` | PASS |
| AC-013 | H `TestEX_AC013_PatchWithRebootNoAlert` | PASS |
| AC-014 | H `TestEX_AC014_DockerRestartDuringPatchNoEscalation` | PASS |
| AC-015 | H `TestEX_AC015_NotBackByBoundEscalatesOnce` | PASS |
| AC-016 | H `TestEX_AC016_TimedOutRebootOfflineEscalates` | PASS |
| AC-017 | H `TestEX_AC017_OfflineAfterFailedRebootAlerts` | PASS |
| AC-018 | H `TestEX_AC018_ContainerNotBackAfterRebootEscalates` | PASS |
| AC-019 | H `TestEX_AC019_StackRedeployContainerDownEscalates` | PASS |
| AC-020 | H `TestEX_AC020_IntentionalDownNoEscalation` | PASS |
| AC-021 | E `TestEX_AC021_TwentyPlusSummary`, `TestEX_AC021_CapHoldsAcrossRestart` | PASS |
| AC-022 | H `TestEX_AC022_EscalationWithoutRuleIsWarning`; G `TestEX_AC022_EscalationWithoutRuleIsWarning` | PASS |
| AC-023 | G `TestEX_AC023_EscalationIgnoresCooldown`, `TestEX_AC023_EscalationStartsNoCooldown` | PASS |
| AC-024 | HD `TestEX_AC024_FleetAgentUpdateRecordsActor`, `TestEX_AC024_FleetPatchRecordsActor`, `TestEX_AC024_FleetDispatchWithoutSessionIsUnknownActor`; A `TestEX_AC024_AgentActivityVersions`; web `[AC-024]` tests | PASS |
| AC-025 | H `TestEX_AC025_AgentRestartOfflineInfo`; A `TestEX_AC025_RestartingAgentBackBy` | PASS |
| AC-026 | H `TestEX_AC026_AgentNotBackEscalates`; A `TestEX_AC026_EscalatedActivity` | PASS |
| AC-027 | H `TestEX_AC027_AgentUpdateDoesNotHideDeaths` | PASS |
| AC-028 | H `TestEX_AC028_ShutdownPoweredOffNoAlertsThreeDays`; A `TestEX_AC028_PoweredOffActivity`; web `[AC-028]` tests | PASS |
| AC-029 | H `TestEX_AC029_PoweredOffEndsOnOnline` | PASS |
| AC-030 | H `TestEX_AC030_FailedShutdownNoPoweredOff` | PASS |
| AC-031 | H `TestEX_AC031_WindowsSurviveRestart` | PASS |
| AC-032 | H `TestEX_AC032_DowntimeEscalationOnceAcrossRestarts`, `TestEX_AC032_MarkerStopsDuplicateWhenWindowStaysPending` | PASS |
| AC-033 | W `TestEX_AC033_EveryWindowHasHardBound`; H `TestEX_AC033_SilentCommandsCloseAndAlertingResumes` | PASS |
| AC-034 | H `TestEX_AC034_QueuedRebootDoesNotSuppress`; A `TestEX_AC034_RebootQueuedActivity` | PASS |
| AC-035 | O `TestEX_AC035_RestartGraceNoOffline`, `TestEX_AC035_OfflineAfterGrace`, plus `..._OfflineIsDatedByTheLastHeartbeat`, `..._OneShotIsArmedForTheGraceEnd`, `..._OneShotRunsTheCheckOnceTheGraceIsOver`, `..._NoGraceMeansNoOneShot` | PASS |
| AC-036 | H `TestEX_AC036_FailOpenWhenNotLoaded`, `TestEX_AC036_FailOpenWhenStale`; G `TestEX_G_UncoveredAndUndecidableDeathsAlertAsToday`, `TestEX_G_UncoveredOfflineAlertsAsToday` | PASS |
| AC-037 | H `TestEX_AC037_RebootWindowEventCountsLogsMemory` | PASS |
| AC-038 | A `TestEX_AC038_SelectionPriorityHostOverDocker`; web `[AC-038]` tests | PASS |
| AC-039 | web `[AC-039]` tests | PASS (web) |
| AC-040 | E `TestEX_AC040_DispatchAndCloseBroadcastWithinOneTick`; web `[AC-040]` tests; hub `TestEX_Hub_BroadcastActivityEnvelope`, `TestEX_Hub_BroadcastActivityNilClearsTheDevice` | PASS |
| AC-041 | A `TestEX_AC041_EscalatedClearsAfter24h`, `TestEX_AC041_EscalatedClearsOnOnline` | PASS |
| AC-042 | web `[AC-042]` tests | PASS (web) |
| AC-043 | the existing patch-badge and fleet tests, unmodified | PASS |
| AC-044 | HD `TestEX_AC044_KeysWithoutAgentCommandID` and four siblings, `TestEX_AC044_ReceiveDockerEventAsksTheTrackerWithTheServersKeys`; H `TestEX_AC044_OlderAgentRestartSuppressed` | PASS |
| AC-045 | H `TestEX_AC045_CrossDeviceCommandIDIgnored`, `TestEX_AC045_HostWindowOnOtherDeviceIgnored` | PASS |
| AC-046 | models `TestEX_AC046_SafeName` (incl. the bidi override and zero-width cases); H `TestEX_AC046_SuppressedAndEscalatedMessagesSanitized` | PASS |
| AC-047 | `internal/server/expect/safety_test.go` `TestEX_AC047_NoPowerOrAgentExecution`, `TestEX_AC047_ScannerCatchesEachForbiddenForm`; web `[AC-047]` tests | PASS |
| AC-048 | `internal/server/expect_docs_test.go` | **NOT DONE: technical writer's file (ADD section 4), runs after QA** |

### Security conditions (ADD 8.1)

| Condition | Test(s) | Status |
|---|---|---|
| C-1 (SEC-001) | H `TestEX_SEC001_HeartbeatReturnThenRestartInsideGrace`, `..._OutsideGrace`, `..._DeleteFailureThenReconcile`, `..._RegistrationWhileOnlineClearsRow`, `..._NoteOnlineWhileNotReadyDeletes`, `..._SecondOfflineAlerts` (6 scenarios), `..._RowNeverRestamped`, `..._LoopEntryRequiresOfflineInsideWindow`, `..._UnverifiableRowAlerts`; PG `TestEX_SEC001_PG_PurgeInvalidPoweredOff`, `TestEX_SEC001_PG_UpsertNoRestamp`; HD `TestEX_SEC001_RegistrationNotesBeforeDeviceOnline`, `..._RegistrationWhileOnlineStillNotesTheTracker`, `..._RejectedRegistrationNotesNothing`, `..._NewDeviceRegistrationHasNoWindowsToNote`, `..._RegistrationWithoutATrackerStillWorks` | PASS |
| C-2 (SEC-002) | F `TestEX_SEC002_HardBoundWithoutSweeperEscalates`, `..._LapseRunsBackstopAndWarns`, `..._OverflowKeepsNewestAndWarns`, `..._DeletedRowWritesBackstopSkipped`, `..._EvictionDoesNotClose`, `..._ContainerRecheckOnReturn`, `..._RecheckDeferralBounded24h`, `..._AnyEndWithDeviceOfflineEscalatesOnce` (8 end reasons); PG `TestEX_SEC002_PG_RetentionSkipsPendingWindows`, `TestEX_SEC002_PG_BudgetSkipsPendingWindows` | PASS |
| C-3 (SEC-003) | P `TestEX_SEC003_BlockingPublisherDoesNotStallLoop`, `TestEX_SEC003_PublishQueueFullDropsPushKeepsEvent`; H `TestEX_SEC003_CoverFailsOpenWhenLoopStalled`; F `TestEX_SEC003_TickBudgetCarriesOver`; G `TestEX_G_PublishEscalationDoesNotBroadcast` | PASS |
| C-4 (SEC-004) | O `TestEX_SEC004_EveryNonOfflineStatusGoesOffline` (online, warning), `..._OfflineDevicesAndMissingHeartbeatsMakeNoTransition`, `..._FailedStatusWriteMakesNoEvent`, `..._DetectorWithoutTrackerStillWorks`; H `TestEX_SEC004_TrackerUsesSameLivenessPredicate`; models `TestEX_SEC004_DeviceIsLive`; HD `TestEX_SEC004_RegistrationOfAWarningDeviceIsNotAReturn` | PASS |
| C-5 (SEC-005) | P `TestEX_SEC005_PushCeilingHoldsAndSummarizes`, `TestEX_SEC005_HeldEscalationsStillStoredExactlyOnce`, `TestEX_SEC005_SummaryAtMostOncePerHour` | PASS |
| C-6 (SEC-006) | models `TestEX_SEC006_SafeNameStripsFormatChars`, `TestEX_SEC006_ContainerNameKeyCollisionFreeAndIdempotent`, `TestEX_SEC006_MarkerTargetBounded`; H `TestEX_SEC006_EventFieldsSanitized`; G `TestEX_G_ContainerNameInEventsUsesTheKeyForm`, `TestEX_G_HexShortID` | PASS |
| C-7 (SEC-007) | O `TestEX_SEC007_RestartSeriesBoundsGrace` (5 starts 90 s apart), `..._ShortenedGraceWarns`, `..._SeriesResetsAfterGap`, `..._ConfigErrorMeansNoGrace` (read, write, unusable value), `..._FirstStartHasTheFullGrace`, `..._SeriesIsOneBoundedRow`, `..._ClockGoingBackDoesNotStretchTheCap` | PASS |
| C-8 | The QA gate (ADD 13): AC-033, AC-036, AC-045, AC-046, AC-047, `TestEX_LateResultAfterTimeoutDoesNotReopen`, every SEC-001 to SEC-004 test, all listed above and passing. The AC-047 scan prints its file list (33 files, below) | PASS |

Supporting tests: `TestEX_Observer_FiresOnAcceptedTransitionsOnly`, `TestEX_LateResultAfterTimeoutDoesNotReopen`, `TestEX_Tracker_CapPerDevice`, `TestEX_Repo_PG_*` (7), `TestEX_Contract_ActivityEnvelopeAndOneEntryPerDevice`, and the route tests `TestEX_Activity_RouteRequiresAnAdminSession`, `..._RouteWithoutATrackerIs503`, `..._RouteServesTheTrackersActivity`.

Mutation checks done in this part (temporary edits, reverted): the generator ignoring the tracker fails 55 harness tests; dropping `NoteRegistration` fails 3 handler tests; passing no actor in the fleet dispatch fails `TestEX_AC024_FleetAgentUpdateRecordsActor`.

### AC-047 scanned file list (C-8)

```
internal/models/expect_test.go
internal/server/commands/observer_test.go
internal/server/db/expect_repo_pg_test.go
internal/server/events/generator_expect_test.go
internal/server/expect/{activity,escalate,finish,messages,publisher,tick,tracker,window}.go
internal/server/expect/{activity,agent_shutdown,container,durability,escalate,finish,harness,host,misc,publisher,safety,sec001,window}_test.go
internal/server/handlers/activity_test.go
internal/server/handlers/docker_attribution_keys_test.go
internal/server/handlers/fleet_actor_test.go
internal/server/offline.go
internal/server/offline_test.go
internal/server/router_activity_test.go
internal/server/storage/command_budget_expect_pg_test.go
internal/server/websocket/hub_test.go
```

The list is computed at run time (33 files) and printed with `go test -v -run TestEX_AC047_NoPowerOrAgentExecution ./internal/server/expect/`. The test also asserts that thirteen named files are in the set.

## Test run output

### New tests only (`go test ./... -run TestEX_ -count=1 -v`, Windows, no `-race`, PG-gated tests enabled via `RIOT_TEST_PG_URL`)

195 `--- PASS: TestEX_*`, 0 FAIL, 0 SKIP. Package lines of packages that have such tests:

```
ok   internal/models 1.036s
ok   internal/server 2.692s
ok   internal/server/commands 0.855s
ok   internal/server/db 14.219s
ok   internal/server/events 0.955s
ok   internal/server/expect 1.266s
ok   internal/server/handlers 1.957s
ok   internal/server/storage 1.007s
ok   internal/server/websocket 0.697s
```

### Full Go suite (`go test ./... -count=1`, no database, Windows, no `-race`)

30 packages ok, no FAIL.

```
?    cmd/riot-agent [no test files]
?    cmd/riot-agent-apply [no test files]
?    cmd/riot-maintenance [no test files]
?    cmd/riot-server [no test files]
ok   cmd/riot-signer 0.964s
ok   internal/agent 23.193s
ok   internal/agent/collectors 11.766s
ok   internal/logbudget 0.577s
ok   internal/maintenance 1.168s
ok   internal/models 1.040s
ok   internal/redact 0.567s
ok   internal/releasekey 0.538s
ok   internal/resilient 1.367s
ok   internal/server 3.581s
ok   internal/server/auth 0.525s
ok   internal/server/ca 0.444s
ok   internal/server/commands 1.409s
ok   internal/server/db 1.170s
ok   internal/server/events 0.790s
ok   internal/server/expect 0.918s
ok   internal/server/handlers 10.513s
ok   internal/server/ingest 0.315s
?    internal/server/logstore [no test files]
ok   internal/server/middleware 2.589s
ok   internal/server/notify 2.516s
ok   internal/server/oidc 1.397s
ok   internal/server/partition 2.234s
ok   internal/server/probes 2.544s
ok   internal/server/quota 2.174s
ok   internal/server/scoring 2.267s
ok   internal/server/storage 2.422s
ok   internal/server/summary 2.331s
ok   internal/server/updates 0.597s
ok   internal/server/websocket 0.815s
?    internal/testutil [no test files]
ok   scripts 49.615s
```

With `RIOT_TEST_PG_URL` set, `go test ./internal/server/db/ ./internal/server/storage/ ./internal/server/commands/ ./internal/server/handlers/ ./internal/server/ -count=1` also passes (`db` 78.5 s), so every existing PG test still passes against migration 000033 together with this part.

Race detector (not available on this Windows host: no C compiler), run in a `golang:1.24` container started with `--rm`: `go test -race -count=1 ./internal/server/ ./internal/server/expect/ ./internal/server/events/ ./internal/server/handlers/ ./internal/server/websocket/ ./internal/server/commands/ ./internal/models/` is clean. The first run found one race in this part's own test (`TestEX_AC035_OneShotRunsTheCheckOnceTheGraceIsOver` moved the test clock from another goroutine); fixed in 9528f70 and re-run clean. No container is left behind.

### Static checks

```
go build ./...                 exit 0, no output
go vet ./...                   exit 0, no output
GOOS=linux go vet ./...        exit 0, no output
cd web && npx tsc --noEmit     exit 0, no output
```

The repository has no other Go linter, and `web/package.json` has no lint script. `gofmt` is clean on every file this story created; the working tree is CRLF on Windows and the index is LF, so `gofmt -l` lists every file there is (including untouched ones such as `internal/server/websocket/client.go`).

### Web (`cd web && npx vitest run`)

```
 Test Files  101 passed (101)
      Tests  1813 passed (1813)
   Duration  16.80s
```

## Contract with the web (honoured by the server)

| Web expectation | Server |
|---|---|
| One `activities` entry per device; WS payload `device_id` equals the envelope `device_id` | `Tracker.Activities` keys by device; `BroadcastActivity(deviceID, a)` is called with the same ID. `TestEX_Contract_ActivityEnvelopeAndOneEntryPerDevice`, `TestEX_Hub_BroadcastActivityEnvelope` |
| `covers_offline` true only for host and agent windows that cover offline; false for queued, docker, escalated | `hostActivityLocked` sets it from the policy; queued, docker, escalated and powered-off entries leave it false (`TestEX_AC034_RebootQueuedActivity`, `TestEX_AC026_EscalatedActivity`, `TestEX_AC028_PoweredOffActivity`) |
| `powered_off_at`, `agent_from_version`, `newest_agent_version`, non-empty `command_id` | present (`TestEX_AC024_AgentActivityVersions`, `TestEX_AC028_PoweredOffActivity`, `TestEX_Contract_...`); `newest_agent_version` is `UpdateChecker.ServerUpdateInfo().LatestVersion` |
| `escalation.bound` a string such as `"15m"` | `models.FormatDurationShort(PostTerminal)`; `TestEX_Activity_ReturnsTheTrackersCurrentState` checks the JSON |
| `GET /api/v1/activity` returns current state; empty list | memory read; `[]` not `null` (`TestEX_Activity_EmptyListIsAnArrayNotNull`) |
| `activity_update` with `data: null` clears | explicit `null` (`TestEX_Hub_BroadcastActivityNilClearsTheDevice`) |

**No mismatch between the ADD and the web report was found that needs a web change.** One note for the architect, the web's deviation 1: the web renders times in the app's UTC/Local preference, not "the browser locale" of AD-014; the server's own message texts still use the server's local time (`time.Local`), so a label and an event message can show different zones.

## Deviations from the ADD

Architect review is triggered by every item below. Items 1 to 13 are part 1's (unchanged), W1 to W7 are the web's, S1 onward are this part's.

### Part 1

1. `MockExpectedActivityRepo` is in `internal/testutil/mocks_expect.go`, not appended to `mocks.go` (only `copyCommand` changed there).
2. The message templates of events the generator stores are methods on `models.Coverage`, `OfflineCoverage` and `OnlineNote`, so the generator renders them without importing `expect`; `expect/messages.go` holds the escalation, summary and warning texts.
3. `evaluate` is unexported; `tick.go` is an extra file.
4. `PrepareEscalation` returns `models.PreparedEscalation{Event, Rule, Skip}`; `PublishEscalation` dispatches only; `Broadcaster` has `BroadcastEvent` and `BroadcastActivity`; `Options.Commands` is an extra collaborator (labels an escalation after a restart).
5. Existing PG test helpers edited for the new column (`command_repo_pg_test.go` migrates to the latest version; the stub `commands` table of `command_budget_pg_test.go` gets `action` and `window_closed_at`). No assertion changed.
6. `SafeName` also strips Zl and Zp (line and paragraph separators).
7. Before the end of the restart grace `Tick` finishes no window at all (not only the online-state decisions).
8. A container check that falls due right after a restart waits 3 minutes for the device to be observed (bounds a restart-delayed escalation at 3 minutes).
9. `NoteRegistration` keeps the powered-off record it ended (`endedPO`, dropped after 5 minutes or consumed by `NoteOnline`) so FR-030's "powered off 72h" text survives the call order (registration first).
10. The loop's powered-off entry stores no info event (restart case only); a transition that alerted because the row could not be stored sets an in-memory `noPoweredOff` flag.
11. **Resolved in this part:** the H tests used a stand-in for the generator; the four seam methods now drive the real `events.Generator` (36137af).
12. The H test for AC-033 is `TestEX_AC033_SilentCommandsCloseAndAlertingResumes`; the W test keeps `TestEX_AC033_EveryWindowHasHardBound`.
13. The generator changes and the two retired tests were left to this part (done); the README / CHANGELOG notes are the technical writer's.

### Web (WP-7)

W1. Times in labels use the app's UTC/Local preference (`formatChartTick`), not the browser locale (a static guard forbids `toLocale*Time`). W2. `web/src/components/ui/labels.test.ts` modified (49 to 51 event types). W3. `web/src/api/demo-client.ts` and `demo-data.ts` modified (the demo-parity test needs `getActivity`). W4. The "Expected offline" card shows only when its count is above 0. W5. An `activity_update` before the first fetch invalidates `['activity']` instead of being dropped. W6. The label sits under the hostname; the dot title is "Offline (expected)". W7. Two extra web test files (`useActivity.test.tsx`, `CommandProgress.activity.test.tsx`).

### This part (WP-2, WP-5, WP-6, WP-8)

S1. **`NewGenerator` keeps its `commandRepo` parameter and field**, now unused: `recentDisruptiveCommand` was its only reader. Removing it would change a signature used by many tests and `server.go`; the ADD did not ask for it.
S2. **`PrepareEscalation` returns an error when the alert rules cannot be read** (the tracker retries on its next tick) instead of building a rule-less warning. The ADD says "rule severity, or `warning` without a rule"; a read failure is not "no rule", and escalating without the rule would silently drop the push.
S3. **`DeviceOnline` links the online event to the command** (`CommandID`, `CommandAttr = expected`) when the tracker returns a note. The ADD gives the message only; the link is what lets the dashboard and `idx_events_command` find it.
S4. **A covered OOM keeps the raw `ContainerName`**; only the message uses `ContainerNameKey`. The ADD applies the key form to new events (suppressed, escalation); an OOM event is not new.
S5. **`SetExpectedActivity` also guards a typed-nil pointer** with `reflect` (stdlib), on top of the wiring rule, so a nil `*expect.Tracker` can never be called.
S6. **`SetClock` drives the cooldown clock too** (`onCooldown`, pruning and the two `lastSent` stamps use `g.now`); it defaults to `time.Now`, so production behaviour is unchanged.
S7. **`RegisterDevice` uses `models.DeviceIsLive(existing.Status)`** where it used `status == online`. A registering `warning` device (a UPS on battery) therefore raises no "came online" event any more. Consistent with C-4 and AD-006 ("registration when the device was not live"); behaviour change for that one case.
S8. **No hook in the heartbeat paths.** The brief for this part mentioned "RegisterDevice/heartbeat paths"; the ADD (X-6, AD-006) says a heartbeat return is silent and the tracker learns it from `ObserveDevices` on the offline worker. `NoteRegistration` is called from `RegisterDevice` only.
S9. **`ReceiveDockerEvent` fills `TargetKeys`; the generator calls `CoverContainer`** (ADD AD-005). The brief said the handler calls it; the code follows the ADD.
S10. **`restartSeriesGrace` reads the series with `GetConfigMap`** (not `GetConfig`) so a missing key is distinguishable from a read error on both the real and the mock repository. It warns on every start that continues a series (the AD-008 text), not only when the 4-minute cap actually shortens the grace. An unusable stored value gives no grace, and the start rewrites the row. A series start in the future (clock moved back) is not trusted.
S11. **The detector skips the event when `SetStatus` fails** (the old code emitted it regardless), logs a Warn, and retries on the next pass. During the grace it does not call `ObserveDevices`.
S12. **`BroadcastActivity` is not added to the handlers' `dashboardHub` interface** (the tracker holds the hub as `expect.Broadcaster`; adding it would break every `dashboardHub` fake). It uses its own envelope type so `data` is an explicit `null`, which `WSMessage` (`omitempty`) cannot express.
S13. **`NewestAgentVersion` is `UpdateChecker.ServerUpdateInfo().LatestVersion`** (the agents are released with the server's tag).
S14. **Files outside ADD section 4 touched:** `internal/server/websocket/hub_test.go` (two tests appended), `internal/server/router_activity_test.go` (created: route auth and 503), `internal/server/expect/activity_test.go` (a contract test appended), `internal/server/expect/harness_test.go` (the seam; the harness rules now carry an operator and threshold like the UI's rules, and a cooldown of 0 because the cooldown path is pinned by `TestEX_G_OfflineCooldownUsesTheInjectedClock`).
S15. **AC-048** (`internal/server/expect_docs_test.go`, README, CHANGELOG) is not done here; it belongs to the technical writer.

## Notes for QA

- Run `go test -v -run TestEX_AC047_NoPowerOrAgentExecution ./internal/server/expect/` and compare the printed list with `git diff --name-only 08cd071..HEAD -- '*.go'`: every new `*_test.go` carrying `TestEX_` must be in it.
- The PG-gated tests skip without `RIOT_TEST_PG_URL`; run the `db`, `storage`, `commands`, `handlers` and `server` packages with it set (PostgreSQL 16 recommended; 17 was used here).
- Behaviour changes to probe: a `warning` device now goes offline when its heartbeat dies (C-4, it never did); a `warning` device that re-registers raises no online event (S7); restart loops stop getting a grace 4 minutes after their first start (C-7), and every start that continues a series logs a start-up Warn (`restart grace shortened`).
- Tests use injected clocks. The only real-time waits are `require.Eventually` in the async publisher tests, a 10 s guard in `TestEX_G_PublishEscalationDoesNotBroadcast` (fails only on a regression) and 5 s contexts on the mock DB calls.
- No test starts a process; the AC-047 scan proves it for the story's files.
- Not run here: a real server against the real PostgreSQL with live agents; the restart-series, tracker load and activity loop are exercised through their unit seams and the router test, not through `Server.Start`.
