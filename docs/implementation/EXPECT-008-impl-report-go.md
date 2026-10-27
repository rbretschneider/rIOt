# Implementation Report: Part G (Go server)

- **Story ID:** EXPECT-008
- **Part:** G, the Go server (installer = Part I, web = Part W, docs = Part D are reported separately)
- **Engineer:** Senior Dev Agent (Backend)
- **Date:** 2026-10-27
- **Inputs:** FRD rev 1, ADD rev 2 (Part G design unchanged from rev 1; rev 2 section 4.5 adds four handler tests), security review (C-7 is this part's)

## Detected stack

- Go 1.x module `github.com/DesyncTheThird/rIOt` (go.mod), chi v5, pgx v5, golang-migrate, log/slog.
- Test runner: `go test` with testify; PostgreSQL-gated tests via `RIOT_TEST_PG_URL` (PostgreSQL 16).
- Frontend (React, TypeScript, Vitest) and installer (bash, Python fixture) exist but are other parts.
- Environment: Windows with Git Bash; `GOCACHE=/d/rIOt_gocache`, `GOTMPDIR=/d/rIOt_fuzz_tmp`; no `-race` (Windows).

## Completed components

| File | Action | Notes |
|---|---|---|
| `cmd/riot-server/migrations/000035_installer_reinstall.up.sql` / `.down.sql` | CREATE | Origin `installer`, `commands_reinstall_shape` CHECK (10-minute bound, empty params, no keys), `idx_commands_reinstall_open` (unique, one running row per device), `idx_commands_reinstall_recent`. Down deletes installer rows (markers cascade) and restores the 000034 origin CHECK |
| `internal/models/reinstall.go` | CREATE | Bounds, tokens, labels, message renderers, value types, `ReinstallOutcome` |
| `internal/models/command_policy.go` | MODIFY | `OriginInstaller`; `IsValidOrigin` still excludes it |
| `internal/models/command_state.go` | MODIFY | `ActorReinstall` (`bitReinstall`), two edges (`running -> succeeded`, `running -> timed_out`), no new (from, to) pair |
| `internal/models/expect.go` | MODIFY | `WindowPolicy.Reinstall`, `ExpectReinstallPolicy`, `ExpectPolicyFor` branch, `ActorPhrase` case, `DeviceActivity.Origin` |
| `internal/models/events.go` | MODIFY | `agent_reinstall_started`, `agent_reinstall_unconfirmed`, `agent_reinstall_paused` |
| `internal/server/db/reinstall_repo.go` | CREATE | `CreateReinstall` (AD-004, one transaction under the per-device advisory lock), `OpenReinstall` |
| `internal/server/db/command_repo.go` | MODIFY | `buildTransitionSQL`: `ActorReinstall` guard (`origin = 'installer'`, device scope mandatory) and a separate appended `origin <> 'installer'` clause for every other non-self-update actor; `ListOverdue` excludes installer rows |
| `internal/server/db/self_update_repo.go` | MODIFY | AD-017: the FR-004 attribution query gains `AND origin <> 'installer'` (one predicate) |
| `internal/server/db/interfaces.go` | MODIFY | `ReinstallRepository`, compile-time assertion for `*CommandRepo` |
| `internal/testutil/mocks.go` | MODIFY | `MockCommandRepo`: `CreateReinstall`/`OpenReinstall`, `LinkDevices` (device status read), `QueueReinstallIDs`, five-actor guard, reinstall CHECK and unique-index emulation, `ListOverdue` and AD-017 attribution exclusions, `FailOn` hooks |
| `internal/server/commands/reinstall.go` | CREATE | `NoteReinstallNotice`, `NoteReinstallRegistration`, `ResolveReinstall` (ActorReinstall transitions plus broadcast only) |
| `internal/server/commands/service.go` | MODIFY | `Options.Reinstalls`; `classifyRejected` answers `not_found` for origin `installer` |
| `internal/server/commands/sweeper.go` | MODIFY | Defensive skip |
| `internal/server/expect/reinstall.go` | CREATE | `ReinstallResolver`, `SetReinstallResolver`, `evaluateReinstall`, `finishReinstall` (gate, backstop, resolve, outcome, close, stuck) |
| `internal/server/expect/window.go`, `finish.go`, `tick.go`, `escalate.go`, `tracker.go`, `activity.go`, `selfupdate.go` | MODIFY | Delegation on `policy.Reinstall`; eager pass skip; escalation message, marker and `origin`; `NoteOnline` rule; reconcile rebuild case; activity `Origin` and no `back_by`; "Agent reinstall" action label |
| `internal/server/handlers/agent_event_reinstall.go` | CREATE | `receiveReinstallNotice` (identity re-check, `200 {"status":"ok","reinstall":"<disposition>"}`, no events row) |
| `internal/server/handlers/handlers.go` | MODIFY | `ReceiveAgentEvent` case; `RegisterDevice` hook after `NoteSelfUpdateRegistration`; `cmdSvc` wires `opts.Reinstalls` through a type assertion |
| `internal/server/server.go` | MODIFY | `Reinstalls: s.CommandRepo`; `s.Expect.SetReinstallResolver(s.Commands)` before `Load` |
| Tests | CREATE / MODIFY | See the next section |

No router, agent, sudoers or dependency change.

### Notes the ADD asked for (section 12, item 12)

- **5.2 rollback:** down migration 000035 deletes every `installer` row (the `command_escalations` markers cascade) and restores the narrower origin CHECK. Roll the server image back only after migrating down to 34. `TestEX8_PG_Migration000035UpDownUp` runs up, down (to 34) and up again.
- **AD-017 change to an EXPECT-002 query:** `internal/server/db/self_update_repo.go`, `createSelfUpdate` step 1 gains `AND origin <> 'installer'`. It is a pure narrowing. The mock mirrors it (`MockCommandRepo.CreateSelfUpdate`), and it is covered by `TestEX8_AC023_PG_SelfUpdateAttributionIgnoresReinstall`, `TestEX8_AC023_SelfUpdateStartNotAttributedToReinstall` and `TestEX8_C7_MockGuardParityIncludesAttribution` (plus the PG half in `TestEX8_MockGuardParity`).
- **CI apt / `update-alternatives` change:** Part I, not this part.
- **`ex8Preexisting` entries:** none new. `ex8Preexisting` is the reviewed `ex2Preexisting` table (same package directories); no stale entry.
- **X-4 real-time test:** installer test, Part I.

## Test summary

AC IDs are the FRD's; the ADD section 8 map was followed. Go tests are named `TestEX8_...`. 74 `TestEX8_` functions exist (73 distinct names; `TestEX8_AC018_StoreFailureFailsOpen` exists in both `commands` and `expect`). Clocks are injected everywhere; nothing sleeps; no network; no test runs a reboot, shutdown, apt operation or an installed agent binary (`TestEX8_AC026_*`).

| AC | Test file | Tests | Status |
|---|---|---|---|
| AC-008 | `expect/reinstall_test.go`, `handlers/agent_event_reinstall_test.go` | `TestEX8_AC008_TimelineNoNotification`, `TestEX8_AC008_LabelFromNoticeUntilRegistration`, `TestEX8_AC008_NoticeCreatesRecordStoresNoEvent`, `TestEX8_AC008_RegistrationClosesRecordAtFirstRegistration` | pass |
| AC-009 | `expect/reinstall_test.go` | `TestEX8_AC009_TwoDevicesBackToBackZeroPushes` | pass |
| AC-010 | `expect/reinstall_test.go` | `TestEX8_AC010_ContainerDeathDuringReinstallAlerts` | pass |
| AC-011 | `expect/reinstall_test.go` | `TestEX8_AC011_OfflineAfterCloseAlerts` | pass |
| AC-012 | `expect/reinstall_test.go` | `TestEX8_AC012_NotBackEscalatesOnce`, `..._EscalationIgnoresUnrelatedCooldown`, `..._NoSecondEscalationAfterRestart`, `..._EscalatedLabelAndOfflineCount` | pass |
| AC-013 | `expect/reinstall_test.go` | `TestEX8_AC013_NotConfirmedWhileOnlineWarningNoPush` | pass |
| AC-014 | `handlers/agent_event_reinstall_test.go`, `expect/reinstall_test.go` | `TestEX8_AC014_RejectedNoticesCreateNothing`, `TestEX8_AC014_HandlerWithoutAuthenticationRecordsNothing`, `TestEX8_AC014_OfflineAfterRejectedNoticeAlerts` | pass |
| AC-015 | `commands/reinstall_test.go`, `db/reinstall_repo_pg_test.go` | `TestEX8_AC015_DeviceOfflineNoRecord`, `TestEX8_AC015_PG_OfflineDeviceNoRow` | pass (PG one needs a database) |
| AC-016 | `commands`, `db` (PG), `expect` | `TestEX8_AC016_FiftyNoticesOneRecord`, `..._RateCapSixPerDayOneWarning`, `..._PG_ConcurrentNoticesOneRecordOneCapEvent`, `..._CappedNoticeOfflineAlertsNoLogAboveDebug`, `..._RepeatedNoticeDoesNotExtendWindow` | pass |
| AC-017 | `handlers/agent_event_reinstall_test.go` | `TestEX8_AC017_UntrustedFieldsIgnored` | pass |
| AC-018 | `commands`, `expect`, `handlers` | `TestEX8_AC018_StoreFailureFailsOpen` (x2), `TestEX8_AC018_RepoErrorAnswersUnavailable` | pass |
| AC-019 | `expect/reinstall_test.go` | `TestEX8_AC019_RestartMidWindowSucceeds`, `..._BoundDuringDowntimeEscalatesOnce`, `..._DeadDuringDowntimeEscalatesNotUnconfirmed` | pass |
| AC-020 | `expect`, `commands`, `db` (PG) | `TestEX8_AC020_ResolverUnsetFailsSafe`, `..._StaleOpenRecordRetiredByNextNotice`, `..._PG_StaleRetireInTx` | pass |
| AC-021 | `expect/reinstall_test.go` | `TestEX8_AC021_BoundedEventsLogsMemory` (installer half `test_EX8_AC021_*` is Part I) | pass |
| AC-022 | `commands`, `handlers` | `TestEX8_AC022_NeverClaimedOrDelivered`, `..._AgentReportNotFoundHTTPAndWS`, `..._CancelNotCancellable`, `..._AgentReportNotFoundAndCancelConflictOverHTTP` | pass |
| AC-023 | `expect`, `commands`, `db` (PG) | `TestEX8_AC023_ReinstallDuringSelfUpdate`, `..._ReinstallDuringOpenSelfUpdateRecord`, `..._SelfUpdateStartNotAttributedToReinstall`, `..._PG_SelfUpdateAttributionIgnoresReinstall` | pass |
| AC-024 | `expect/reinstall_test.go` | `TestEX8_AC024_ReinstallWithoutNoticeAlertsAsToday` (installer half is Part I) | pass |
| AC-025 | `expect/reinstall_test.go` + every existing `TestEX_*`, `TestEX2_*`, CMD-LIFECYCLE test, unmodified | `TestEX8_AC025_SelfUpdateAndCommandWindowsUnchanged` | pass |
| AC-026 | `expect/safety_ex8_test.go` | `TestEX8_AC026_NoPowerUpdateOrAgentExecution`, `TestEX8_AC026_ScannerCatchesEachForbiddenForm` | pass |
| AC-001..007, AC-027 | not Part G | installer (Part I) and docs (Part D) | n/a |
| Supporting (ADD 8.1) | `models/reinstall_test.go`, `db`, `commands`, `expect` | `TestEX8_ActorReinstallEdges`, `TestEX8_ExistingActorEdgesUnchanged`, `TestEX8_MessagesMatchFRD`, `TestEX8_PG_Migration000035UpDownUp`, `TestEX8_PG_CheckShape`, `TestEX8_PG_UniqueOpenRecord`, `TestEX8_PG_OriginCheck`, `TestEX8_PG_Guards`, `TestEX8_MockGuardParity` (PG), `TestEX8_MockGuardMatchesDesign` (no PG), `TestEX8_SweeperSkipsReinstall`, `TestEX8_PG_ListOverdueExcludesReinstall`, `TestEX8_TransitionsLeaveResultColumnsAndGateUntouched`, `TestEX8_NoCompletionEventForReinstall`, `TestEX8_OutcomeEventsNeverDispatchedNorCeilinged`, `TestEX8_RegistrationAfterBoundIgnored`, `TestEX8_NoteOnlineOnlyForCoveredOffline`, plus small ones (`IsValidOrigin`, policy, actor phrase, outcome mapping, bounds, resolve, other device, create shape) | pass |
| C-7 (ADD 4.5) | `handlers/agent_event_reinstall_test.go` | `TestEX8_C7_ServerAuthoredTypesRejectedOnAgentRoute`, `TestEX8_C7_NoticeStoresNoEventOnEveryDisposition`, `TestEX8_C7_ForeignKeyForbiddenWithMTLS`, `TestEX8_C7_MockGuardParityIncludesAttribution` (PG half: `TestEX8_MockGuardParity`, `TestEX8_AC023_PG_SelfUpdateAttributionIgnoresReinstall`) | pass |

### PostgreSQL-backed tests

Without `RIOT_TEST_PG_URL`, these 12 skip: `TestEX8_PG_Migration000035UpDownUp`, `TestEX8_PG_OriginCheck`, `TestEX8_PG_CheckShape`, `TestEX8_PG_UniqueOpenRecord`, `TestEX8_AC015_PG_OfflineDeviceNoRow`, `TestEX8_PG_CreateShapeAndSecondNoticeAbsorbed`, `TestEX8_AC016_PG_ConcurrentNoticesOneRecordOneCapEvent`, `TestEX8_AC020_PG_StaleRetireInTx`, `TestEX8_PG_Guards`, `TestEX8_PG_ListOverdueExcludesReinstall`, `TestEX8_AC023_PG_SelfUpdateAttributionIgnoresReinstall`, `TestEX8_MockGuardParity`. They were run against a throwaway local `postgres:16` container (started and removed by the engineer, never against hawaii): all 12 pass.

## Linter and formatting

Linter: `go vet ./...` clean. `gofmt` was run on every file this part created; edited pre-existing files were not reformatted wholesale (several repo files were already not gofmt-clean, and `server.go` was kept to a three-line diff).

## Deviations from the ADD

1. **`TestEX8_MockGuardParity` lives in `internal/server/db/reinstall_parity_pg_test.go`** (a new file, external package `db_test`, because `testutil` imports `db`), as the EXPECT-002 parity test does. Section 4.1 lists only `reinstall_repo_pg_test.go` for R8. Reason: the same import-cycle constraint EXPECT-002 hit. The no-database companion `TestEX8_MockGuardMatchesDesign` is in `commands/reinstall_test.go`.
2. **Hostname read in the handler:** the ADD says a failed `devices.GetByID` renders `device`. Implemented as written (empty hostname, `selfUpdateHost` turns it into `device`).
3. **`MockCommandRepo.Devices` unlinked means "every device is live".** The ADD says the mock reads device status from the linked `MockDeviceRepo`; it does not say what an unlinked mock does. The conservative choice for existing tests (none links devices) is "live". All EXPECT-008 tests link the device repository, so a missing or offline device is refused as in SQL.
4. **`harness_test.go` (additive, as the ADD allows):** besides the `reinstallNotice` helper it links the device repository to the command mock, wires `Reinstalls` and the resolver, adds `withoutReinstallResolver()`, and `registerOn` now also calls `NoteReinstallRegistration` (the handler's order). No existing test changed behaviour. `gofmt` realigned the `hConfig` struct fields (whitespace only).
5. **No change to `internal/server/db/event_repo.go` or `internal/models/self_update.go`** (the two server-authored classification lists): the reinstall events carry `command_attr = outcome` (stuck, unconfirmed) or no link (cap), so the existing attr-based classification already treats them as server-authored. Not in ADD section 4.1, so not touched.

None of these triggers an architect review in the engineer's view, but items 1, 3 and 4 touch test code or test doubles, so QA may want a look.

## Existing tests

- No existing test file was modified except the additive `internal/server/expect/harness_test.go` (item 4). The EXPECT-001 and EXPECT-002 Go tests, the CMD-LIFECYCLE tests, `expect2_docs_test.go` and the EX2 safety scan pass unchanged (`DeviceActivity.Origin` is populated for command windows too, which no existing test compared).
- `TestCL_AC100_NewAgentAgainstLegacyServer` did not fail in the runs below.

## Notes for QA

- Rate-cap and stuck events: only the cap event is stored without a link (`command_id ''`); the stuck event is linked with `command_attr = outcome` and marker `stuck`.
- `NoteReinstallRegistration` ignores a registration at or after the bound (`hard`), and one before the record's start.
- A notice never stores an event row on any disposition. The only event a notice can cause is the rate-cap warning (server-authored, broadcast once, no push).
- Probe ideas: a notice while an operator reboot window is open (latest start takes the label, EXPECT-001 backstop independent); two quick notices across a server restart (the unique index and the lock agree); `agent_reinstall_unconfirmed`/`agent_reinstall_paused` posted by a device (400).
- PostgreSQL 16 is required for the R8 tests; `RIOT_TEST_PG_URL` must name a database containing "test".

## Test run output

Run 1: build, vet and the full suite without a database (the 12 PostgreSQL tests skip):

```
$ go build ./...
(exit 0)
$ go vet ./...
(exit 0)
$ go test ./...  (no RIOT_TEST_PG_URL)
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-maintenance	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-server	[no test files]
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer	0.965s
ok  	github.com/DesyncTheThird/rIOt/internal/agent	29.919s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	15.331s
ok  	github.com/DesyncTheThird/rIOt/internal/extraupd	4.435s
ok  	github.com/DesyncTheThird/rIOt/internal/logbudget	0.593s
ok  	github.com/DesyncTheThird/rIOt/internal/maintenance	4.081s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.995s
ok  	github.com/DesyncTheThird/rIOt/internal/redact	0.594s
ok  	github.com/DesyncTheThird/rIOt/internal/releasekey	0.882s
ok  	github.com/DesyncTheThird/rIOt/internal/resilient	1.421s
ok  	github.com/DesyncTheThird/rIOt/internal/server	4.201s
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	0.565s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	0.499s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	1.928s
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	0.683s
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	0.524s
ok  	github.com/DesyncTheThird/rIOt/internal/server/expect	2.731s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	10.974s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ingest	0.430s
?   	github.com/DesyncTheThird/rIOt/internal/server/logstore	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	0.813s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	0.731s
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc	2.337s
ok  	github.com/DesyncTheThird/rIOt/internal/server/partition	0.455s
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	0.724s
ok  	github.com/DesyncTheThird/rIOt/internal/server/quota	0.349s
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	0.452s
ok  	github.com/DesyncTheThird/rIOt/internal/server/storage	1.152s
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary	0.436s
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	0.698s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	0.576s
?   	github.com/DesyncTheThird/rIOt/internal/testutil	[no test files]
ok  	github.com/DesyncTheThird/rIOt/scripts	64.980s
```

Run 2: the packages that use PostgreSQL, against a throwaway local `postgres:16` container (`RIOT_TEST_PG_URL` set, `-count=1`); the whole `db` package including every pre-existing PG test and the 12 EXPECT-008 ones ran:

```
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	539.856s
ok  	github.com/DesyncTheThird/rIOt/internal/server/partition	0.979s
ok  	github.com/DesyncTheThird/rIOt/internal/server/expect	5.925s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	4.060s
```

The container was removed afterwards. Nothing connected to hawaii.
