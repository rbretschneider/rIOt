# Implementation Report

- **Story ID:** EXPECT-002 (agent self-update as expected activity)
- **Engineer:** Senior Dev Agent (Go side)
- **Date:** 2026-10-26
- **ADD:** `docs/architecture/EXPECT-002-add.md` revision 2 (security conditions C-1 to C-6, INFO-4, INFO-5, severity clamp)
- **Branch:** `feat/expect-002`
- **Scope of this report:** every row of ADD section 4 outside `web/`. The web rows are reported in the Web section below.

## Detected stack

- Go 1.26 module `github.com/DesyncTheThird/rIOt` (server, models, migrations, testutil). Windows host, Git Bash; `gofmt`, `go vet`, `go test` (no `-race`: Windows has no CGO, the Linux CI runs it).
- PostgreSQL 17 (a throwaway `postgres:17-alpine` container, started and removed for this work) for the PG-gated tests; the migrations use golang-migrate (`cmd/riot-server/migrations`).
- Test frameworks: Go `testing` with testify (`assert`, `require`); the EXPECT-001 harness (`internal/server/expect/harness_test.go`) and the `internal/testutil` mocks.
- Conventions followed: `CLAUDE.md`-level rules from the task (clock injection, no sleeps, bounded growth stated at each bound, gofmt, commit format `[EXPECT-002] <type>: <what>` with the Co-Authored-By line).

## Commits

| Commit | Type | Change |
|---|---|---|
| `b620398` | feat | Models: origin, `ActorSelfUpdate` with its 7 edges, self-update policy and `ExpectPolicyFor`, the strict start parser, `SafeText`, message renderers, severity clamp, event types |
| `41ad78c` | migration | `000034_agent_self_update` up and down; the EXPECT-001 up/down/up test now migrates down to 32 (see Deviations) |
| `7a17ee0` | feat | `CreateSelfUpdate`, `OpenSelfUpdate`, `LatestSelfUpdateSince`, origin guards in `buildTransitionSQL`, `ListOverdue` exclusion, `ListByCommandSplit`, mocks, PG tests, mock parity test |
| `9ace32d` | feat | `commands.Service`: `NoteAgentUpdateEvent`, `NoteSelfUpdateRegistration`, `ResolveSelfUpdate`, `classifyRejected`, sweeper skip |
| `5fac637` | feat | Tracker: `evaluateSelf`, `finishSelfUpdate`, detector gate, stuck event, labels, activity, escalation state, harness helpers, H2 tests |
| `b540402` | feat | Handlers: `ReceiveAgentEvent`, `RegisterDevice`, events endpoint, route, `server.go` wiring |
| `f4bab22` | test | AC-029 Go safety scan and its self-test |
| `50f51b4` | docs | README, CHANGELOG, AC-030 test |
| `505fe37` | refactor | Dropped an unused method |

## Completed components (ADD section 4, non-web rows)

| File | Action | Notes |
|---|---|---|
| `cmd/riot-server/migrations/000034_agent_self_update.up.sql` / `.down.sql` | CREATE | As Section 5.1 and 5.2. Down deletes the self-update rows (markers cascade), drops the indexes and the shape CHECK, restores the 3-value origin CHECK. Rollback note (R-5): the self-update history is lost; events keep a dangling `command_id`, the `agent_self_update_*` events stay as plain warnings |
| `internal/models/self_update.go` | CREATE | Constants (bounds, param keys, step and failure-step tokens), `ParseSelfUpdateStart`, `ValidAgentVersion`, `AgentVersionsMatch`, `SafeText`, `SelfUpdateStart`, `SelfUpdateCreateResult`, `SelfUpdateOutcome`, `CommandEvents`, `IsServerAuthoredEvent`, `ClampAgentUpdateSeverity`, the AD-012 message renderers |
| `internal/models/self_update_test.go` | CREATE | Section 8 "M" |
| `internal/models/command_policy.go` | MODIFY | `OriginAgentSelfUpdate`; `IsValidOrigin` deliberately excludes it |
| `internal/models/command_state.go` | MODIFY | `ActorSelfUpdate` (`bitSelfUpdate`), seven edges, `String()` |
| `internal/models/expect.go` | MODIFY | `WindowPolicy.SelfUpdate`, `ExpectSelfUpdatePolicy`, `ExpectPolicyFor`, `ExpectSelfUpdateBound`, `ActorPhrase` case, `DeviceActivity.AgentToVersion`, `EscalationTargetOutcome`/`Stuck`, `SafeName` is `SafeText(s, 128)` |
| `internal/models/events.go` | MODIFY | Three event types; `CommandAttrInferred`, `CommandAttrOutcome` |
| `internal/server/db/self_update_repo.go` | CREATE | `CreateSelfUpdate` (one transaction under the per-device advisory lock: FR-004, stale retire, FR-006, FR-007 with the once-per-24h event, insert), `OpenSelfUpdate`, `LatestSelfUpdateSince`; a unique-index violation re-reads and returns `open_record` |
| `internal/server/db/command_repo.go` | MODIFY | `buildTransitionSQL`: `AND origin = 'agent_self_update'` for `ActorSelfUpdate` (and `DeviceID` mandatory), `AND origin <> 'agent_self_update'` for server, agent and operator; `ListOverdue` excludes self-update rows |
| `internal/server/db/event_repo.go` | MODIFY | `ListByCommandSplit` (two selects, two counts) |
| `internal/server/db/interfaces.go` | MODIFY | `SelfUpdateRepository`; `EventRepository.ListByCommandSplit`; assertions |
| `internal/server/db/self_update_repo_pg_test.go` | CREATE | Section 8 "R" |
| `internal/testutil/mocks.go` | MODIFY | `MockCommandRepo`: the four origin guards, CHECK emulation (status, `deadline_at <= D+10m`, `exec_deadline_s <= 600`, no keys), unique-index emulation, `CreateSelfUpdate`/`OpenSelfUpdate`/`LatestSelfUpdateSince` (cap event appended to the linked `MockEventRepo`, `LinkEvents`), `FailOn` works for them; `MockEventRepo.ListByCommandSplit` |
| `internal/server/commands/self_update.go` | CREATE | `NoteAgentUpdateEvent`, `NoteSelfUpdateRegistration`, `ResolveSelfUpdate`: ActorSelfUpdate transitions plus broadcast only; no `completed()`, no policy recorder, no progress gate, no result columns (C-6) |
| `internal/server/commands/service.go` | MODIFY | `Options.SelfUpdates`; `classifyRejected` returns `not_found` for a self-update row |
| `internal/server/commands/sweeper.go` | MODIFY | Defensive skip |
| `internal/server/commands/self_update_test.go` | CREATE | Section 8 "S" |
| `internal/server/expect/selfupdate.go` | CREATE | `evaluateSelf`, `finishSelfUpdate` (detector gate, backstop first, resolution, outcome event, close, stuck event), `windowActionLabel`, `SelfUpdateResolver` |
| `internal/server/expect/window.go`, `tracker.go`, `finish.go`, `tick.go`, `escalate.go`, `activity.go` | MODIFY | As ADD section 4: `ExpectPolicyFor` at every policy lookup, `selfFrom`/`selfTo`, `trimCommand` copies `VerifyingAt`, versioned labels in `CoverOffline`/`NoteOnline`, the `NoteOnline` self rule, `openEscalation` versions and the rebuild, `SetSelfUpdateResolver`, detector-gate flag set by `ObserveDevices`, delegation in `finishWindow`, eager FR-020 skips self-update windows, escalation text and state, activity versions |
| `internal/server/expect/harness_test.go` | MODIFY | Additive: `withoutSelfUpdateResolver`, `selfStart`, `agentCompleted`, `agentFailed`, `registerOn`, `heartbeatOn`, `detectorPass`, `selfRecords`, `eventsLinked`, `logLevelLines`; `build` wires `SelfUpdates` and the resolver |
| `internal/server/expect/selfupdate_test.go` | CREATE | Section 8 "H2" |
| `internal/server/expect/safety_ex2_test.go` | CREATE | AC-029 Go scan (parts a, b, c; named `must` list; reviewed `ex2Preexisting` table with stale-entry check) and the scanner self-test |
| `internal/server/handlers/handlers.go` | MODIFY | `ReceiveAgentEvent`: `h.clock()`, severity clamp, self-update link, broadcast of returned events. `RegisterDevice`: `LastHeartbeat = h.clock()` before `Update`, then `NoteRegistration`, then `NoteSelfUpdateRegistration`. Default `cmdSvc` wires `SelfUpdates` when the repository implements it |
| `internal/server/handlers/agent_event_self_update.go` | CREATE | Thin helper: authenticated-ID check (`authenticatedDeviceID == {id}`), hostname read, service call |
| `internal/server/handlers/command_events.go` | CREATE | `GET .../commands/{commandId}/events`; one identical 404 body for a malformed, missing and foreign id |
| `internal/server/handlers/agent_event_self_update_test.go`, `command_events_test.go` | CREATE | Section 8 "HT" and "CE" |
| `internal/server/router.go` | MODIFY | The new admin route |
| `internal/server/server.go` | MODIFY | `commands.Options.SelfUpdates`; `SetSelfUpdateResolver(s.Commands)` before `Load` |
| `internal/server/expect2_docs_test.go` | CREATE | AC-030 |
| `README.md`, `CHANGELOG.md` | MODIFY | FR-032, FR-033, the AD-010 budget (C-5), the new endpoint and fields |

No agent file, `scripts/install.sh`, sudoers or dependency change (`git diff c55e4bd HEAD -- internal/agent scripts/install.sh` is empty).

## Items the ADD asks the report to state (Section 12 note 13)

- **Rollback (5.2):** the down migration deletes the self-update history (markers cascade). Events keep a dangling `command_id`.
- **`ReceiveAgentEvent`** uses the injected clock (`h.clock()`) for `CreatedAt` and clamps the severity of `agent_update_*` events (AD-015, X-19).
- **A re-registration now refreshes `last_heartbeat`** for every device (X-20). No existing test broke because of it: the full suite passed unmodified on this point.
- **`ListOverdue` excludes self-update rows**, and the server transition edge cannot match them either.
- **`ex2Preexisting` entries** (all hits that predate the story, each with a reason in the table; a stale entry fails the test):

| File | Pattern | Why it is there |
|---|---|---|
| `internal/models/expect.go` | dpkg | `StepWaitDpkgLock` is the wait-step token EXPECT-001 reserved |
| `internal/server/commands/observer_test.go` | dpkg | EXPECT-001 test reports the wait step token as data |
| `internal/server/commands/sweeper_test.go` | dpkg | CMD-LIFECYCLE test names a reboot that waits for the lock, in prose |
| `internal/server/expect/activity_test.go` | dpkg | EXPECT-001 label tests |
| `internal/server/expect/host_test.go` | dpkg | EXPECT-001 host test message |
| `internal/server/handlers/activity_test.go` | dpkg | the DTO field name `waiting_...` |
| `internal/server/handlers/fleet_patch_badge_test.go` | dpkg | a failure-reason fixture |
| `internal/server/handlers/patch_badge_test.go` | package manager word with a space | a failure-reason fixture |
| `internal/server/handlers/extra_updaters_server_test.go` | package manager word with a space | an EXTRA-UPD-001 test name |
| `internal/server/handlers/reg_harden_qa_test.go` | import of `internal/agent` | REG-HARDEN-001 QA drives the agent's HTTP registration client against an in-process handler; it starts no process and runs no update |
| `internal/server/server.go` | import of `syscall` | graceful shutdown registers for `syscall.SIGTERM`; nothing is sent or run |

## Test summary: AC to test

Test files: M = `internal/models/self_update_test.go`, R = `internal/server/db/self_update_repo_pg_test.go` (and `self_update_parity_pg_test.go`), S = `internal/server/commands/self_update_test.go`, H2 = `internal/server/expect/selfupdate_test.go`, HT = `internal/server/handlers/agent_event_self_update_test.go`, CE = `internal/server/handlers/command_events_test.go`, Z = `internal/server/expect/safety_ex2_test.go`, D = `internal/server/expect2_docs_test.go`. Every test name from ADD sections 8, 8.1 and 8.2 exists under that exact name (checked by extracting the names from the ADD and comparing them with the code: none missing). 110 distinct `TestEX2_` functions; 112 run, because `TestEX2_AC024_AgentReportNotFoundHTTPAndWS` exists in S (service level) and HT (HTTP and WebSocket).

| AC | Test file | Tests | Status |
|---|---|---|---|
| AC-001 | H2 | `TestEX2_AC001_ReleaseTimelineNoNotification`, `TestEX2_AC001_LabelFromStartUntilRegistration` | pass |
| AC-002 | H2 | `TestEX2_AC002_FleetReleaseFourRecordsZeroPushes` | pass |
| AC-003 | H2 | `TestEX2_AC003_ContainerDeathDuringSelfUpdateAlerts` | pass |
| AC-004 | H2 | `TestEX2_AC004_OfflineAfterCompletionAlerts` | pass |
| AC-005 | H2, HT, +server | `TestEX2_AC005_FastRestartClosesOnRegistration`; `TestEX2_AC005_StaleHeartbeatAfterFastReturnNoAlert`; extra `TestEX2_AC005_RealDetectorSeesTheRegistrationHeartbeat` (real `offlineDetector` + `RegisterDevice`) | pass |
| AC-006 | S, H2 | `TestEX2_AC006_StartDuringDispatchedCommandAttributed`, `TestEX2_AC006_StartWithinTenMinutesAfterCommandAttributed`, `TestEX2_AC006_FleetUpdateShowsCommandLabel` | pass |
| AC-007 | S, R | `TestEX2_AC007_OldCommandDoesNotCapture`, `TestEX2_AC007_QueuedCommandDoesNotCapture`, `TestEX2_AC007_PG_AttributionPredicate` | pass |
| AC-008 | H2 | `TestEX2_AC008_NotBackEscalatesOnceNamingVersions`, `..._EscalationIgnoresUnrelatedCooldown`, `..._NoSecondEscalationAfterRestart`, `..._EscalatedLabelAndOfflineCount` | pass |
| AC-009 | H2 | `TestEX2_AC009_FailedWhileOnlineWarningNoPush` (+ S `TestEX2_AC009_CompletedAndFailedMoveTheRecord`) | pass |
| AC-010 | H2 | `TestEX2_AC010_FailureAfterReturnSetsFailed` | pass |
| AC-011 | H2 | `TestEX2_AC011_ReturnOnNewVersionWithoutCompletionSucceedsAtBound` | pass |
| AC-012 | H2 | `TestEX2_AC012_ReturnOnOldVersionFails` | pass |
| AC-013 | H2 | `TestEX2_AC013_NotConfirmedWhileOnline` | pass |
| AC-014 | H2 | `TestEX2_AC014_RestartMidWindow` | pass |
| AC-015 | H2 | `TestEX2_AC015_BoundDuringDowntimeEscalatesOnce`, `TestEX2_AC015_DeadDuringDowntimeEscalatesNotUnconfirmed` (+ `..._GateOpensAfterTheCapWithoutADetectorPass`) | pass |
| AC-016 | M, H2, HT | `TestEX2_AC016_ParseRejectsMalformed`, `TestEX2_AC016_MalformedStartAlertsAsToday`, `TestEX2_AC016_MalformedStartStoredAsToday` | pass |
| AC-017 | S, R, H2 | `TestEX2_AC017_RepeatedStartsOneRecord`, `TestEX2_AC017_RateCapSixPerDayOneWarning`, `TestEX2_AC017_PG_ConcurrentStartsOneRecordOneCapEvent`, `TestEX2_AC017_CappedStartOfflineAlertsNoDebugAbove` | pass |
| AC-018 | H2, HT | `TestEX2_AC018_OtherDeviceUnaffected`, `TestEX2_AC018_OtherDeviceRegisterCompleteFailLeavesRecord`, `TestEX2_AC018_MismatchedAuthenticatedDeviceIgnored` | pass |
| AC-019 | M, H2 | `TestEX2_AC019_ReasonSanitizedAndBounded`, `TestEX2_AC019_FailedEventMessageSanitized` | pass |
| AC-020 | H2, HT | `TestEX2_AC020_StoreFailureFailsOpen`, `TestEX2_AC020_EventStoredWhenRecordStoreFails` (+ S) | pass |
| AC-021 | S | `TestEX2_AC021_CompletionWithoutRecordUnlinked` | pass |
| AC-022 | H2 | `TestEX2_AC022_BoundedEventsLogsMemory` | pass |
| AC-023 | H2, CE | `TestEX2_AC023_ActivityCarriesVersionsAndActor`, `TestEX2_AC023_CommandEventsEndpoint` (200 agent events: all server events returned, 50 agent events, `agent_events_omitted` 150 + the start), `TestEX2_AC023_CommandEventsNotFoundIdentical` | pass |
| AC-024 | M, S, R | `TestEX2_AC024_ActorSelfUpdateEdges`, `..._NeverClaimedOrDelivered`, `..._AgentReportNotFoundHTTPAndWS` (S and HT), `..._CancelNotCancellable`, `..._PG_CheckForbidsQueued`, `..._PG_CheckBoundsDeadlineAndKeys`, `..._PG_AgentOperatorServerGuards`, `..._MockGuardParity` | pass |
| AC-025 | H2 | `TestEX2_AC025_CommandAndRecordOneLabel` | pass |
| AC-026 | H2 | `TestEX2_AC026_OlderAgentNoCompletionSucceeds`, `..._CompletionBeforeRestartStillCovered`, `..._OtherWordingAsToday` | pass |
| AC-027 | HT | `TestEX2_AC027_UpdateAvailableUnchanged` | pass |
| AC-028 | H2 + all `TestEX_*` | `TestEX2_AC028_CommandAgentUpdateUnchanged`; every existing `TestEX_*` and CMD-LIFECYCLE test passes (one hard-coded migration test needed a one-line change, see Deviations) | pass |
| AC-029 | Z | `TestEX2_AC029_NoPowerUpdateOrAgentExecution`, `TestEX2_AC029_ScannerCatchesEachForbiddenForm` | pass |
| AC-030 | D | `TestEX2_AC030_ReadmeAndChangelog` | pass |
| 8.1 | various | `TestEX2_Q1_RegistrationAfterBoundIgnored`, `TestEX2_Q2_OneLinkPerEvent`, `TestEX2_X1_CompletionBeforeReturnCoversRestart`, `..._X1_NoReturnByBoundEscalates`, `..._X3_HeartbeatVersionConfirmsAtBound`, `..._X7_FailedWithCoveredOfflineEscalatesOnce`, `..._X19_AgentUpdateSeverityClamped`, `..._NoteOnlineOnlyForCoveredOffline`, `..._SweeperSkipsSelfUpdate`, `..._PG_ListOverdueExcludesSelfUpdate`, `..._ExistingActorEdgesUnchanged`, `..._PG_Migration000034UpDownUp`, `..._PG_UniqueOpenRecord`, `..._PG_OriginCheck`, `..._NoCompletionEventForSelfUpdate`, `..._SelfUpdateTransitionsLeaveResultColumnsAndGateUntouched`, `..._OutcomeEventsNeverDispatchedNorCeilinged`, `..._ResolverUnsetFailsSafe`, `..._StaleOpenRecordRetiredByNextStart`, `..._PG_StaleRetireInTx` | pass |

The C-6 QA gate tests are all in the list above and pass. Supporting tests added beyond the ADD names (extra coverage, same package): see `TestEX2_AC016_ParseAcceptsTheAgentForm`, `..._ParseBounds`, `TestEX2_SelfUpdateMessages`, `TestEX2_ClampAgentUpdateSeverity`, `TestEX2_ServerAuthoredClassification`, `TestEX2_AC024_TransitionSQLOriginGuards` (no database needed), `TestEX2_PG_CreateSelfUpdateRowShape`, `TestEX2_PG_ListByCommandSplit`, `TestEX2_PG_AttributionDoesNotRetire`, `TestEX2_ResolveSelfUpdate`, `TestEX2_MalformedStartCreatesNothing`, the `TestEX2_Handler_*` tests and the `TestEX2_AC023_CommandEvents*` extras.

## Test run output

Commands (all with `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`; PG tests with `RIOT_TEST_PG_URL` pointing at a throwaway local `postgres:17-alpine` database named `riot_test`):

```
go build ./...                      rc=0
go vet ./...                        rc=0, no output
go test ./... -count=1              rc=0 (31 packages ok, none failed)
```

New tests, `go test -count=1 -v -run TestEX2_` over the six packages: 112 `--- PASS`, 0 fail, 0 skip (with the PG URL set). Without `RIOT_TEST_PG_URL` the PG-gated tests skip, as the existing ones do.

```
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.728s       (16 EX2 tests)
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	28.468s      (15)
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	0.594s   (22)
ok  	github.com/DesyncTheThird/rIOt/internal/server/expect	0.901s     (42)
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	2.024s   (15)
ok  	github.com/DesyncTheThird/rIOt/internal/server	1.461s            (2)
```

Full suite (`go test ./... -count=1`, PG URL set):

```
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-maintenance	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-server	[no test files]
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer	1.240s
ok  	github.com/DesyncTheThird/rIOt/internal/agent	27.890s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	14.383s
ok  	github.com/DesyncTheThird/rIOt/internal/extraupd	3.471s
ok  	github.com/DesyncTheThird/rIOt/internal/logbudget	0.904s
ok  	github.com/DesyncTheThird/rIOt/internal/maintenance	3.368s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.911s
ok  	github.com/DesyncTheThird/rIOt/internal/redact	0.819s
ok  	github.com/DesyncTheThird/rIOt/internal/releasekey	0.966s
ok  	github.com/DesyncTheThird/rIOt/internal/resilient	0.906s
ok  	github.com/DesyncTheThird/rIOt/internal/server	4.054s
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	0.734s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	0.665s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	1.438s
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	113.841s
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	0.759s
ok  	github.com/DesyncTheThird/rIOt/internal/server/expect	2.131s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	11.857s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ingest	0.369s
?   	github.com/DesyncTheThird/rIOt/internal/server/logstore	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	0.912s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	1.230s
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc	2.424s
ok  	github.com/DesyncTheThird/rIOt/internal/server/partition	0.390s
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	1.278s
ok  	github.com/DesyncTheThird/rIOt/internal/server/quota	0.268s
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	0.337s
ok  	github.com/DesyncTheThird/rIOt/internal/server/storage	1.371s
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary	0.828s
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	0.661s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	0.964s
?   	github.com/DesyncTheThird/rIOt/internal/testutil	[no test files]
ok  	github.com/DesyncTheThird/rIOt/scripts	61.229s
```

`gofmt`: every Go file added or changed by this story is formatted (checked on the LF-normalised content, because this checkout converts to CRLF). Two touched files were not gofmt-clean at the baseline commit and were left alone: `internal/server/server.go` (import order and field alignment) and `internal/server/events/generator_test.go` (alignment). My lines in both are formatted.

AC-029 printed list: `go test ./internal/server/expect -run TestEX2_AC029_NoPower -v` logs the scanned files (146 at the time of this report). Every named `must` file is asserted to exist and to be in the set.

## Deviations from the ADD

These trigger architect review.

1. **An existing PG test changed (one line).** `TestEX_Repo_PG_MigrationUpDownUp` (`internal/server/db/expect_repo_pg_test.go`) used `Steps(-1)` to go down from "the latest migration", which was 000033. With 000034 it rolled back the wrong migration and failed. It now migrates down to version 32 explicitly. The ADD says the `TestEX_*` tests stay unmodified; this one cannot, by construction.
2. **A stub added to an existing test mock.** `internal/server/events/generator_test.go` has its own `mockEventRepo` that implements `db.EventRepository`. Adding `ListByCommandSplit` to the interface (as the ADD says) required a three-line stub there. The file is not in ADD section 4.
3. **Two test files beyond section 4:** `internal/server/db/self_update_parity_pg_test.go` (the mock parity test is an external test package, `db_test`, because `testutil` imports `db`; it carries its own small PG setup) and `internal/server/expect2_register_test.go` (the AC-005 timeline with the real `offlineDetector`). Both are in the AC-029 `must` list.
4. **Rate-cap event lookup refined.** The "an `agent_self_update_paused` event exists in the last 24 hours" check also requires `command_id = ''`, so a stuck-record event (same type, linked to a record) does not silence the rate-cap warning. The ADD does not say; the once-per-device-per-24h bound still holds.
5. **Stuck event covers more failures.** The stuck-record event is written when any of the backstop, resolution, outcome event or close fails one hour after the bound, not only step 2 or 3.
6. **Detector gate wording.** The gate is open when the tracker has had an `ObserveDevices` call at or after `graceEnd`, or when `now >= graceEnd + 30s`. With no grace (`graceEnd` zero) it is always open.
7. **`TestEX2_AC024_MockGuardParity` skips without PostgreSQL.** It compares the mock with PostgreSQL over about 1,400 actor x origin x status x target combinations. The mock guards are also covered without a database (`TestEX2_AC024_MockRejectsOtherActorsOnSelfUpdate`, `TestEX2_AC024_TransitionSQLOriginGuards`, the S tests).
8. **README beyond FR-032.** The API reference also documents the events endpoint, `agent_to_version` and the new origin value.
9. **`-race` was not run** (Windows, no CGO). The new code paths hold locks as the ADD's note 1 describes; please let the Linux CI run `go test -race ./...`.

## Findings that are not part of this story

- `Transition` with `ActorServer` to `dispatched` (the "claim" edge in the state table) fails in PostgreSQL with `inconsistent types deduced for parameter $3`. No production path uses it (claims go through `ClaimQueued` and `ClaimByID`), so it never fires. It exists at the baseline. I left it alone; the parity test skips that target for that reason.

## Notes for QA

- **PG tests:** start a throwaway database whose name contains `test`, then `RIOT_TEST_PG_URL=postgres://postgres:pw@localhost:55432/riot_test?sslmode=disable go test ./internal/server/db/ -run TestEX2_`. Each test makes its own schema.
- **C-6 gate:** run `go test ./internal/... -run 'TestEX2_(SelfUpdateTransitionsLeaveResultColumnsAndGateUntouched|OutcomeEventsNeverDispatchedNorCeilinged|AC018|AC016|AC019|AC020|AC024|Q2|AC015|AC005|ResolverUnsetFailsSafe|AC023_CommandEventsNotFoundIdentical)' -v`. All the names the ADD's section 13 lists exist and pass.
- **AC-029:** read the logged file lists (Go: `-v` of `TestEX2_AC029_NoPowerUpdateOrAgentExecution`; web: the web report). Run `git diff --name-only c55e4bd HEAD -- '*.go'` and compare: every file is in the Go list (the `must` list is asserted by name).
- **Edge cases worth probing by hand:** a start from device A while device B has an open record; a start exactly 10 minutes after a command finished (attributed) and 10m01s (record); a registration exactly at the bound (ignored); a completed event 10m01s after the start (unlinked); six starts in 24 hours and the seventh; a server restart with the bound inside the downtime and the device dead (escalates, no "not confirmed while online"); a registration at last heartbeat + 40 s with the detector pass at + 46 s (no offline).
- **Operational:** run migration 000034 on a copy first if you like; it takes a sub-second scan of `commands`. The down migration deletes the self-update rows.
- **Existing behaviour that changed for every device (X-20):** a re-registration refreshes `last_heartbeat`.

## Web

The web rows are reported in full in `docs/implementation/EXPECT-002-impl-report-web.md`: the commits (`8ffdfb8`, `a9bdfae`, `4b4dba4`, `d1ee067`), the web AC-to-test table, the deviations and the AC-029 web scan scope.

- **Results:** `npx tsc --noEmit -p .` is clean, and `npm run test:run` passes 114 files and 2015 tests (the baseline was 109 files and 1873 tests).
- **AC-029 web scan:** `web/src/utils/selfUpdate.safety.test.ts` prints its file list. That list includes `SelfUpdateDetails.test.tsx`, `FleetOverview.selfupdate.test.tsx`, `DeviceDetail.selfupdate.test.tsx`, `selfUpdate.ts` and `SelfUpdateDetails.tsx`.
- **Deviations:**
  - `DeviceActivity.agent_to_version` is optional in TypeScript.
  - `labels.test.ts` now counts 54 event types.
  - The web scan does not ban the Go-side literals `dpkg`, `apt` and `systemd-run`, because `activity.ts` contains "waiting for dpkg lock".
