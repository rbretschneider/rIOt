# Implementation Report: WP-GO-SERVER

**Story ID:** CMD-LIFECYCLE-001
**Work package:** WP-GO-SERVER (ADD §4.1 minus `internal/models`, which was done in commit 421ffb3)
**Engineer:** Senior Dev Agent
**Date:** 2026-10-18

**Detected stack:** Go 1.24 module (toolchain on the dev host: go1.26.0, Windows), chi v5,
pgx v5, golang-migrate v4 (iofs source), gorilla/websocket, testify; PostgreSQL 16. No Go or
npm dependency was added (`go.mod` and `go.sum` are untouched).

## Commits

| # | ADD §13.2 item | Commit | Notes |
|---|---|---|---|
| 2 | migration 000023 | d43ed15 | up and down, smoke-tested on PG 16 |
| 3 | guarded repository and faithful mocks | cc1adce | transitional unguarded methods kept until item 6 |
| 5 | validate, sanitize, rate-limit | 265022f | committed before item 4 (leaf utilities the service imports) |
| 4 | command service, sweeper, targets | 1cefad8 | includes progress coalescing and the 1s gate |
| 6+7 | all creation paths through the service; agent registry and scoped WS reports | c07de6e | combined, see Deviations 1 |
| 8 | get, cancel, report endpoints, router, server wiring | 6bb0397 | |
| 9 | auto-update targeting, success-only cooldown, backoff | e3d5ae1 | |
| 10 | telemetry ordering | 9b22a22 | |
| 11 | rune-safe completion events | 3dcbb75 | |
| 21 | PG-gated tests and CI Postgres service | 6a03827 | |
| – | agent-contract tests | e895bfd | added after the coordinator's agent-expectation checklist |

## Files changed

| File | Action | Notes |
|---|---|---|
| `cmd/riot-server/migrations/000023_command_lifecycle.{up,down}.sql` | CREATE | per ADD §5; down compares `include_reboot_class` as jsonb (see Deviations 9) |
| `internal/server/db/command_repo.go` | MODIFY | `CreateIfNoConflict`, `ClaimQueued`, `ClaimByID`, `Transition`, `ListActive`, `ListExpiredQueued`, `ListOverdue`, `GetForDevice`, pure `buildFilteredQuery`/`buildActiveQuery`/`buildTransitionSQL`; `UpdateStatus`, `UpdateCommandResult`, `ListPending`, `Create` removed |
| `internal/server/db/interfaces.go` | MODIFY | new `CommandRepository`; `AutoUpdateRepository` (`RecordSuccess`/`RecordFailure`, `SetLastTriggered` removed); `TelemetryRepository.LatestSnapshotTime` |
| `internal/server/db/auto_update_repo.go` | MODIFY | new columns, `RecordSuccess`/`RecordFailure` |
| `internal/server/db/telemetry_repo.go` | MODIFY | `LatestSnapshotTime`; six "latest" queries bounded by `NOW() + 120s` |
| `internal/server/db/{command_repo_test,command_repo_pg_test,pgtest_helper_test}.go` | CREATE | |
| `internal/server/commands/{service,validate,ratelimit,logthrottle,targets,sweeper}.go` and their `_test.go` | CREATE | |
| `internal/server/handlers/{handlers,commands,terminal,auto_update,fleet,device_probes}.go` | MODIFY | |
| `internal/server/handlers/{command_lifecycle,dispatch_paths,agent_registry,telemetry_order}_test.go` | CREATE | |
| `internal/server/handlers/{auto_update,commands}_test.go` | MODIFY | adapted; AC-001, 008, 010, 012, 030, 072 added |
| `internal/server/events/generator.go`, `generator_test.go` | MODIFY | rune-safe reason |
| `internal/server/websocket/hub.go`, `hub_test.go` | MODIFY | envelope timestamps, `BroadcastCommandUpdate`; `BroadcastCommandResult` removed |
| `internal/server/{server,router}.go`, `router_test.go` | MODIFY / CREATE | `registerCommandRoutes`; `Server.Commands`; sweeper goroutine |
| `internal/testutil/mocks.go` | MODIFY | faithful `MockCommandRepo` (records calls, `FailOn`), policy and telemetry mocks, `FakeAgents`/`FakeBroadcaster`/`FakeCompletionEvents` |
| `.github/workflows/ci.yml` | MODIFY | `permissions: contents: read`, digest-pinned `postgres:16` service with literal throwaway credentials, `RIOT_TEST_PG_URL` |

Not touched: `internal/models`, `internal/agent/**`, `web/**`, `go.mod`, `go.sum`.
`handlers` does not import `internal/agent` (verified; `go build ./...` is green).

## AC to test table (server-side ACs)

All test names are exactly those of ADD §8 and §8.1. U = mock-level unit, PG = PG-gated.

| AC / condition | Test | File | Level | Status |
|---|---|---|---|---|
| AC-001 | TestCL_AC001_AutoUpdateDispatchIncludesNameAndID | handlers/auto_update_test.go | U | pass |
| AC-005 | TestCL_AC005_FailedPolicyCommandKeepsLastTriggered (failed, timed_out, expired, cancelled) | commands/service_test.go | U | pass |
| AC-006 | TestCL_AC006_SucceededSetsLastTriggeredToTransitionTime | commands/service_test.go | U | pass |
| AC-008 | TestCL_AC008_BackoffGatesDispatch | handlers/auto_update_test.go | U | pass |
| AC-009 | TestCL_AC009_SuccessResetsFailures | commands/service_test.go | U | pass |
| AC-010 | TestCL_AC010_NoDispatchWhilePolicyCommandRunning | handlers/auto_update_test.go | U | pass |
| AC-011 | TestCL_AC011_PG_MigrationResetsContainerPoliciesOnly | db/command_repo_pg_test.go | PG | pass (local PG 16) |
| AC-012 | TestCL_AC012_OutsideWindowNoDispatch | handlers/auto_update_test.go | U | pass |
| AC-016 | TestCL_AC016_ReportEndpointRecordsTerminal | handlers/command_lifecycle_test.go | U | pass |
| AC-023 | TestCL_AC023_DuplicateFinalIs2xxRowUnchanged; TestCL_AC023_PG_DuplicateKeepsUpdatedAt | handlers/command_lifecycle_test.go; db/command_repo_pg_test.go | U, PG | pass |
| AC-024 | TestCL_AC024_ForeignDeviceReportRejected; TestCL_AC024_WSForeignResultIgnored; TestCL_AC024_WSForeignAckAndProgressIgnored | handlers/command_lifecycle_test.go; handlers/agent_registry_test.go | U | pass |
| AC-026 / 027 | TestCL_AC026_SweeperTimesOutOverdueRunning; TestCL_AC027_SweeperLeavesInDeadlineRunning | commands/sweeper_test.go | U | pass |
| AC-028 | TestCL_AC028_HeartbeatNeverDeliversExpiredReboot | handlers/command_lifecycle_test.go | U | pass |
| AC-029 | TestCL_AC029_ExpiresAtPerActionClass (+ `_WindowEndCapsExpiry`) | commands/service_test.go | U | pass |
| AC-030 | TestCL_AC030_AutoPatchExpiryCappedAtWindowEnd | handlers/auto_update_test.go | U | pass |
| AC-031 | TestCL_AC031_SweeperRunsBeforeFirstTick | commands/sweeper_test.go | U | pass |
| AC-068..071 | TestCL_AC068_ConflictReturns409NoRow, AC069_TerminalPreviousAllowsNew, AC070_RebootAcceptedSecondRebootConflicts, AC071_BulkSkipsConflictingTargets | handlers/command_lifecycle_test.go | U | pass |
| AC-072 | TestCL_AC072_AutoUpdateConflictSkipLogsNoBackoff | handlers/auto_update_test.go | U | pass |
| AC-073 | TestCL_AC073_ConcurrentClaimsExactlyOne; TestCL_AC073_PG_ClaimRace | commands/service_test.go; db PG | U, PG | pass |
| AC-074 | TestCL_AC074_HeartbeatExcludesWSClaimed | handlers/command_lifecycle_test.go | U | pass |
| AC-075 | TestCL_AC075_AllDispatchPathsUniform (7 paths x offline, connected, second run) | handlers/dispatch_paths_test.go | U | pass |
| AC-076 / 077 | TestCL_AC076_OldHandlerExitKeepsNewConn; TestCL_AC077_NewRegistrationClosesPrevious (+ an end-to-end real-WebSocket test) | handlers/agent_registry_test.go | U | pass |
| AC-079 | TestCL_AC079_TelemetryBroadcastHasTimestamps | handlers/telemetry_order_test.go | U | pass |
| AC-083 / 084 | TestCL_AC083_OlderSnapshotHistoryOnly; TestCL_AC084_MissingTimestampUsesReceivedAt | handlers/telemetry_order_test.go | U | pass |
| AC-085..089 | TestCL_AC085_RejectedTransitionLogged, AC086_SucceededIgnoresLateFailed, AC087_TimedOutAcceptsOneLateResult, AC088_AckAfterRunningIgnored, AC089_ReportsOnExpiredCancelledIgnoredLogged | commands/service_test.go | U | pass |
| AC-090 / 091 | TestCL_AC090_DisallowedHTTPReport409; TestCL_AC091_GetByIDHasPerStateTimestamps | handlers/command_lifecycle_test.go | U | pass |
| AC-094..098 | TestCL_AC094_StderrTail8KiBAndFailureStep, AC095_OutOfRangeProgressStoredNull, AC096_OnlyAcceptedBroadcast, AC097_LegacySuccessFromDispatched, AC098_LegacyErrorFromDispatched | commands/service_test.go | U | pass |
| AC-101 | TestCL_AC101_LegacyStatusesCanonical (scan); TestCL_AC101_PG_LegacyRowsReadCanonical | db/command_repo_test.go; db PG | U, PG | pass |
| AC-102 | TestCL_AC102_LegacySuccessTreatedAsSucceeded | commands/service_test.go | U | pass |
| AC-103 | TestCL_AC103_LegacyQueuedRebootExpires; TestCL_AC103_PG_MigratedLegacyQueuedExpires | commands/sweeper_test.go; db PG | U, PG | pass |
| AC-104 | TestCL_AC104_SendFailureRevertUnlessReported | commands/service_test.go | U | pass |
| AC-105 / 106 | TestCL_AC105_CancelledNeverDelivered; TestCL_AC106_CancelDispatched409 | handlers/command_lifecycle_test.go | U | pass |
| AC-107 | TestCL_AC107_CancelVsClaimExactlyOne (50 iterations); TestCL_AC107_PG_CancelClaimRace (25 iterations) | commands/service_test.go; db PG | U, PG | pass |
| NFR-002 / 007 | TestCL_NFR002_PG_DownMigrationRestoresLegacyValues; TestCL_NFR007_PG_SweepOver100kRowsUnder1s (~5s incl. 100k-row insert; the two queries are well under 1s) | db PG | PG | pass |
| SEC C-1 | NonUUIDCommandIDRejectedWithoutQueryOrPayloadLog (HTTP path, HTTP body, WS, 1 MiB), StepAndMessageBounded (HTTP, WS), SanitizeReportTable, OutputCappedServerSide, IdenticalProgressNoWriteNoBroadcast, ProgressThrottledPerCommand, ReportRateLimitedPerDevice, DeviceLimiterTokenBucket, RejectionLogsThrottledPerDevice, LogThrottleSummary | handlers/command_lifecycle_test.go; commands/{validate,service,ratelimit,logthrottle}_test.go | U | pass |
| SEC C-2 | SEC002_ReportWithNULAndInvalidUTF8Stored, SEC002_SanitizeNULAndInvalidUTF8, SEC002_CompletionEventReasonRuneSafe, SEC002_PG_NULReportPersists | commands; events/generator_test.go; db PG | U, PG | pass |
| SEC C-3 | SEC003_FutureTimestampClampedToReceivedAt, FutureSnapshotDoesNotSuppressLaterTelemetry, BroadcastCollectedAtIsClampedValue, SEC003_PG_LatestQueriesIgnoreFutureRows | handlers/telemetry_order_test.go; db PG | U, PG | pass |
| SEC C-6 | SEC006_StatusFilterRejectsUnknownValues (400, repository never called), SEC006_FilteredQueryUsesOnlyPlaceholders, SEC006_TargetKeyFilterIsBound, SEC006_PG_TargetKeyFilterIsBound | handlers; db | U, PG | pass |
| SEC C-10 | SEC013_PGTestsSkipOnNonTestDatabase; `ci.yml` has no `secrets.*`, no `pull_request_target`, `permissions: contents: read`, digest-pinned image | db/pgtest_helper_test.go; ci.yml | U | pass |
| SEC C-11 | SEC_RouteAuthMatrix, SEC_ForeignAndUnknownReportIndistinguishable (HTTP and WS receipts), SEC_BodyCommandIDMismatch400, SEC_ReportBodyOver512KiB413, SEC_ActiveNonQueuedRowsAlwaysHaveDeadline (+ PG), SEC_ProgressDoesNotExtendDeadline, SEC_ClaimSkipsExpiredEvenIfExpireStepFails (+ PG), SEC_ReportContentNotLogged | router_test.go; handlers; commands; db PG | U, PG | pass |
| SEC-009 / 010 / 014 / 016 | SEC009_PG_DownMigrationFailsQueuedPowerActions; SEC010_LegacyRowsSweptSilently; SEC014_TerminalMessagesScopedToDevice; SEC016_AmbiguousSendMessages | db PG; commands/sweeper_test.go; handlers/agent_registry_test.go; commands/service_test.go | U, PG | pass |

Extra tests beyond the mapping: end-to-end `HandleAgentWS` over a real WebSocket (receipt, reconnect, wrong key),
Transition SQL builder tests, conflict serialization on PG (12 racing creates admit one), report-response contract
table (`TestContract_AgentReportResponsesCarryCodeAndEchoID`), active-list/`target_keys` contract, bulk 409 shape,
stack payload (`TestAutoUpdate_StackDispatchSendsComposeWorkDirOnly`), hub envelope shape.

## Test run output

Commands run from the worktree root on Windows (no `-race`; the CI job runs with `-race`).

### Build and vet

```
go build ./...                                             build exit: 0
go vet ./internal/server/... ./cmd/... ./internal/testutil/   vet exit: 0
```

### Server packages, models and cmd (no `RIOT_TEST_PG_URL`: PG tests skip cleanly)

```
ok  	github.com/DesyncTheThird/rIOt/internal/server	1.929s
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	0.504s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	0.511s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	1.033s
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	1.545s
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	0.506s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	4.197s
?   	github.com/DesyncTheThird/rIOt/internal/server/logstore	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	1.103s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	1.588s
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc	3.289s
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	1.602s
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	0.738s
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary	0.935s
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	0.488s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	0.633s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.435s
?   	github.com/DesyncTheThird/rIOt/internal/testutil	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-server	[no test files]
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer	1.364s
```

### PG-gated tests against a real PostgreSQL 16

A local Docker daemon was available, so the PG-gated tests were actually run (not just compiled):
`postgres:16@sha256:ca0bd484cb98bf4b24eb1010e73fb3fcbd6714d240fbc1a10eea5b7dbecb641d`, database `riot_test`,
`RIOT_TEST_PG_URL=postgres://riot_test:riot_test@localhost:55432/riot_test?sslmode=disable`
(`go test -count=1 ./internal/server/db/ -v`, 32 tests, all PASS):

```
--- PASS: TestCL_AC011_PG_MigrationResetsContainerPoliciesOnly (2.11s)
--- PASS: TestCL_AC073_PG_ClaimRace (2.28s)
--- PASS: TestCL_AC107_PG_CancelClaimRace (2.45s)
--- PASS: TestCL_AC023_PG_DuplicateKeepsUpdatedAt (2.17s)
--- PASS: TestCL_AC101_PG_LegacyRowsReadCanonical (2.50s)
--- PASS: TestCL_AC103_PG_MigratedLegacyQueuedExpires (2.24s)
--- PASS: TestCL_NFR007_PG_SweepOver100kRowsUnder1s (5.01s)
--- PASS: TestCL_NFR002_PG_DownMigrationRestoresLegacyValues (2.42s)
--- PASS: TestCL_SEC009_PG_DownMigrationFailsQueuedPowerActions (2.16s)
--- PASS: TestCL_SEC002_PG_NULReportPersists (2.74s)
--- PASS: TestCL_SEC006_PG_TargetKeyFilterIsBound (3.18s)
--- PASS: TestCL_SEC_PG_ActiveNonQueuedRowsAlwaysHaveDeadline (2.16s)
--- PASS: TestCL_SEC_PG_ClaimSkipsExpiredEvenIfExpireStepFails (2.39s)
--- PASS: TestCL_PG_CreateIfNoConflictSerializesOverlappingKeys (2.44s)
--- PASS: TestCL_PG_TransitionWritesTimestampsProgressAndLateResult (2.19s)
--- PASS: TestCL_PG_RevertClearsClaimAndMarksAmbiguous (2.18s)
--- PASS: TestCL_PG_ListFiltersUseAliasExpansion (1.91s)
--- PASS: TestCL_SEC003_PG_LatestQueriesIgnoreFutureRows (2.27s)
... plus the 14 pure db tests (scan, query builders, SEC013)
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	45.993s
```

The throwaway container was removed afterwards. Running the real SQL found three test-side mistakes (a parameter
type deduction in a seed statement, a fixture that used a fixed date against `NOW()` in the down migration, a
missing device seed) and no production SQL defects.

### Full suite

```
go test -count=1 ./...
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-server	[no test files]
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer	1.783s
ok  	github.com/DesyncTheThird/rIOt/internal/agent	15.220s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	6.172s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.955s
ok  	github.com/DesyncTheThird/rIOt/internal/releasekey	0.792s
ok  	github.com/DesyncTheThird/rIOt/internal/resilient	2.001s
ok  	github.com/DesyncTheThird/rIOt/internal/server	2.037s
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	0.749s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	0.904s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	0.946s
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	2.023s
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	1.412s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	4.751s
?   	github.com/DesyncTheThird/rIOt/internal/server/logstore	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	0.909s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	1.934s
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc	3.577s
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	2.232s
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	0.980s
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary	1.065s
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	0.940s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	1.084s
?   	github.com/DesyncTheThird/rIOt/internal/testutil	[no test files]
```

No failure in any package, including `internal/agent` (WP-GO-AGENT had committed by then).
`gofmt -l` is not clean repo-wide (CRLF working tree and pre-existing files); new files were written gofmt-style.
`-race` was not run (Windows, no CGO); the race-sensitive tests (claim, cancel, registry, limiter, throttle) are
written to be meaningful under the CI `-race` job.

## Definition-of-done checks

- `UPDATE commands` appears in Go only in `Transition` (`buildTransitionSQL`) and the two claim statements.
- `UpdateStatus`, `UpdateCommandResult`, `ListPending`, `SetLastTriggered`, `BroadcastCommandResult` no longer exist
  outside `internal/agent`.
- No `time.Now()`/`time.Since()` in `internal/server/commands` (non-test). The repository's `CreateIfNoConflict`
  falls back to `time.Now()` only if the caller leaves `CreatedAt` zero (the service never does).
- **Migration number check (§16).** `fix/pg-autovacuum-bloat` already holds `000023_autovacuum_tuning` (collision with
  this story's 000023); `part-001-partitioning` holds 000024 to 000028. Whichever branch merges second must renumber
  above the then-current maximum, and the target database's `schema_migrations` table must be checked before deploy. Nothing was renamed
  here.
- **Postgres image digest** for `ci.yml`: `postgres:16@sha256:ca0bd484cb98bf4b24eb1010e73fb3fcbd6714d240fbc1a10eea5b7dbecb641d`
  (confirmed with `docker image inspect`, and that digest is what the local test container ran).
- **Compose `--` check (ADD §12.17)** belongs to the agent package; not done here.
- New environment variables: none for the server. Test-only: `RIOT_TEST_PG_URL`, `RIOT_TEST_PG_ALLOW`.

## Deviations from the ADD (triggers architect review)

1. **Commit sequence.** Item 5 (validate/ratelimit/logthrottle) was committed before item 4 because the service imports
   them. Items 6 and 7 are one commit (c07de6e): removing the unguarded repository methods forces the terminal.go
   result path and the registry to change in the same commit for the tree to build. The coalescing/gate half of item 5
   lives in `service.go` (item 4), as the ADD file list puts it.
2. **`AgentSender` has an extra method `IsConnected(deviceID) bool`.** The ADD lists only `SendCommand`. The claim must
   come before the send (NFR-005: a cancelled command must never be sent), so the service needs to know the agent is
   connected before it claims.
3. **`CompletionEvents.CommandCompleted` has no hostname parameter.** `handlers.NewCompletionEvents` adapts the event
   generator and resolves the hostname from the device repository, which keeps the service free of a device
   dependency.
4. **Repository signatures chosen where the ADD left them implicit:**
   `Transition(ctx, t) (*Command, applied bool, error)`, `ClaimByID(ctx, id, now) (*Command, claimed bool, error)`,
   `CreateIfNoConflict(ctx, cmd) (conflict *Command, error)`, `ListExpiredQueued/ListOverdue(ctx, deviceID, now, limit)`
   (empty device = all, the heartbeat path uses one device), `ListByDeviceFiltered(..., statuses, action, targetKey)`,
   `GetForDevice`, and a `db.ErrCommandNotFound` sentinel returned by `GetByID`/`GetForDevice` (and the mock).
5. **`DispatchResult.ConflictKey` and `BulkResult`.** The 409 `target_key` is the first of the request's keys that the
   holder also holds; added so the contract in ADD §6.2 can be filled.
6. **WS receipts are sent for `command_result` only** (ADD §6.4/AD-009). The coordinator's checklist asked for a receipt
   per ack, progress and result; the agent only consumes receipts to clear outbox entries (final results), so no agent
   behaviour depends on ack/progress receipts. A processed result always gets one (including `not_found`); a dropped
   message (rate limit, malformed id, undecodable) and an internal error get none.
7. **Coalesced reports return `status: "running"`** in the 200 body (the gate only coalesces when the last accepted
   state was running); the ADD shows `status` without saying what it holds then.
8. **SEC-016 late-report warning is logged at most once per command** (bounded set of 1024), still exempt from the
   throttle. Without the bound a compromised agent could write up to 120 Warn rows a minute to `server_logs` for one
   row. On the accepted path the record has `to` but no `from` (the repository does not return the previous status).
9. **Down migration compares `params->'include_reboot_class' = 'true'::jsonb`** instead of the ADD's
   `(params->>'include_reboot_class')::boolean`, which raises an error on a non-boolean string and would abort the
   downgrade. Same result for real data.
10. **`handlers.Handlers.hub` is an interface (`dashboardHub`)**, and `Handlers` gained unexported test seams
    (`now`, `async`, `agents`). Needed to observe telemetry broadcasts and to give tests a private agent registry; the
    real `*websocket.Hub` satisfies the interface (compile-time assertion) and a nil `deps.Hub` stays a nil interface.
    The registry defaults to a package-level shared one so connections survive `applyTLSAndRestart`.
11. **`BroadcastHeartbeat` and `BroadcastTelemetry` signatures changed** (timestamps added), as the envelope change
    requires.
12. **Extra test helpers in `testutil`** beyond the ADD: `MockCommandRepo.All/Get/Calls/CallCount/FailOn`,
    `FakeAgents` hooks (`OnConnectCheck`, `OnSend`, `DisconnectAfterCheck`), `MockTelemetryRepo.Now`.
13. **`HandlerDeps` gained `Commands` and `Clock`; `Server` gained `Commands`.** As in the ADD (§4.1). `server.Start`
    builds the service once and starts the sweeper (`runCommandSweeper`, one pass at start then every 60s).
14. **`checkAutoUpdates` consumes the stagger slot only on a real dispatch.** A conflict skip (FR-061) no longer ends
    the evaluation pass early, otherwise one stuck target would starve every policy behind it.

No ADD contradiction or impossible component change was found; no blockers file was written. No file outside ADD §4.1
was changed, except the extra test files listed above (all `_test.go`).

## Contract points confirmed for WP-WEB and WP-GO-AGENT

WP-WEB's six assumptions all hold: (1) `collected_at`/`received_at` are top-level in the telemetry envelope,
`received_at` only on heartbeats; (2) `GET .../commands?active=true` returns every non-terminal command with
`target_keys` (compose key for a compose `docker_update`, union for a bulk), and `target_key` narrows it;
(3) `command_update.updated_at` is the row's server-written `updated_at`, never decreases for one command under a
monotonic server clock, `message` is `result_msg`, device id is in `data.device_id` and on the envelope;
(4) create 409 is `{error, code:"conflict", existing_command_id, existing_status, target_key}`, the all-conflict bulk
409 has the same shape plus `conflicts`, a bulk 201 always carries `conflicts` (`[]` when empty), and the bulk 409
omits `existing_status`; (5) `GET /commands/{id}` returns the canonical Command, cancel returns 200 with the Command
or 409 `{error:"command cannot be cancelled after it has been dispatched", code:"not_cancellable", status}`;
(6) the list endpoint accepts exactly the 14 canonical-plus-legacy tokens and answers 400 `invalid_filter` for
anything else, so the web client's ten canonical values are always accepted.

WP-GO-AGENT's expectations hold: the report endpoint takes a `models.CommandReport`; every 2xx echoes `command_id`;
409 carries `already_terminal` or `invalid_transition`; every 404, 400, 413 and 429 on the route carries a JSON `code`
(`command_not_found`, `invalid_report`, `rate_limited`), so a real 404 can never look like a legacy server; command
ids are `uuid.New().String()`; per-container auto-update sends `container` plus `container_id`; a stack update sends
`compose_work_dir` alone (the working-dir label, verbatim). Server-side nuance: the report body cap is 512 KiB (413),
the WS read limit 4 MiB, and the per-device report budget is 120/min with burst 60 shared by HTTP and WS (HTTP 429,
WS drop with no receipt, so the agent outbox falls back to HTTP and backs off).

## Notes for QA

- Run the PG-gated tests: `RIOT_TEST_PG_URL=postgres://riot_test:riot_test@localhost:5432/riot_test?sslmode=disable go test ./internal/server/db/`
  against any PostgreSQL 16. They create and drop a `cl_<random>` schema per test and refuse a database whose name does
  not contain `test` unless `RIOT_TEST_PG_ALLOW=1`.
- First deploy: the first sweep silently relabels historical `sent` rows as `timed_out` and old queued rows as
  `expired` (SEC-010; one Info line `legacy commands relabelled`, no events, no broadcasts). The FR-008 reset makes
  every container-level auto-update policy eligible once: expect a burst of recreations bounded by stagger and window
  (release note needed, OQ-003).
- `updated_at` monotonicity relies on the server wall clock; an NTP step backwards between two transitions of the same
  command could break it (the dashboard also guards with `updated_at >=`).
- An unmigrated rollback window row (legacy `success`/`sent` written by an old server) reads and behaves canonically
  (AC-102); covered with mock fixtures.
- Not covered by automated tests: the WS-level 4 MiB read limit (set on the connection, not exercised) and the real
  `applyTLSAndRestart` router rebuild.
