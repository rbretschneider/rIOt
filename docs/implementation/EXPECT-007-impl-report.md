# Implementation Report

**Story ID:** EXPECT-007
**Engineer:** Senior Dev Agent (Sonnet 5.5)
**Date:** 2026-10-27
**Branch:** `claude/expect-007-473e29` (base `bee8e23`, ADD rev 2)
**Status:** Complete. Not tagged, not pushed, not released, not deployed.

## Detected stack

| Source | Finding |
|---|---|
| `go.mod` | Go module `github.com/DesyncTheThird/rIOt`, Go 1.24 (local toolchain 1.26.0 on Windows) |
| Backend | chi v5, pgx v5, golang-migrate, `log/slog`, PostgreSQL 16 |
| Agent | Go daemon in `internal/agent` (Linux and Windows builds; Linux build and vet checked with `GOOS=linux`) |
| Frontend | React + TypeScript + Tailwind, Vite, vitest (`web/package.json`) |
| Go tests | `go test`, testify; PG-gated tests read `RIOT_TEST_PG_URL` |
| Web tests | `npm run test:run` (vitest) |

Active specialists: backend, frontend. No database specialist work: the ADD has no migration.

## Completed Components

Commits, in order (all `[EXPECT-007] <type>: ...`, one logical change each):

| # | Commit | Work package |
|---|---|---|
| 1 | `8b2c6b9` feat: models for update-event references, versions and retry bounds | WP-1 |
| 2 | `61b0cf2` feat: report the dpkg-lock wait of a standalone reboot or shutdown as a step | WP-3 |
| 3 | `404eddd` feat: refuse real processes on the update and hand-off paths in test binaries | WP-2 |
| 4 | `e4180c4` feat: agent update events carry versions and a reference, sent with a bounded retry | WP-4 |
| 5 | `0dfe72c` feat: store the attempt ID in a self-update record and match replays in the transaction | WP-5 |
| 6 | `ae6fdc2` feat: attribute agent update events by validated reference, structured versions first | WP-6 |
| 7 | `cf34b3a` feat: read update-event references at the handler and store reference-linked repeats once | WP-7 |
| 8 | `37e9f1a` feat: show "(waiting for dpkg lock)" only while the device is online and the command is live | WP-8 |
| 9 | `01154a6` feat: label the dpkg-lock wait and the power step in the command history | WP-9 |
| 10 | `67cba61` docs: README section and CHANGELOG entries for agent-side activity reporting | WP-10 |
| 11 | `7fcf323` test: server-side safety scan for the story's Go sources and tests | WP-8 (Z) |

(Commits 2 and 3 are in that order because the AZ rules file pins both the reporter attach sites and the process guard, and so needs both; WP-2's guard code is independent.)

ADD Section 4 files:

| File | Action | Notes |
|---|---|---|
| `internal/agent/power_wait_step.go` | CREATE | `powerWaitReporter` (`enter`, `settle`, `leave`, `entered`), context helpers |
| `internal/agent/reboot_clearance.go` | MODIFY | one `enter()` call in the first-held block; no signature change |
| `internal/agent/commands.go` | MODIFY | `handleReboot`, `handleShutdown`; `handleTriggerUpdate` (command reference, versions, `deliverAsync`) |
| `internal/agent/powerguard.go` | MODIFY | `processRunner()`, `refuseProcessInTest`, `errProcessInTest` |
| `internal/agent/update_events.go` | CREATE | attempt, event builder, sender, retry policy, slots |
| `internal/agent/updater.go` | MODIFY | attempt generation, `deliverAwait` / `deliverFirstSync`, hand-off fields, `bindHandoff` |
| `internal/agent/signed_update_handoff.go` | MODIFY | `attempt_id`, `from_version`, `to_version`; self branch sends hand-off values only; watcher uses `processRunner()` |
| `internal/agent/http_client.go` | MODIFY | `ReportEvent` returns a bounded `*StatusError` |
| `internal/agent/agent.go` | MODIFY | retry/slot fields; `Run` sets the policy |
| `internal/agent/{power_wait_step,update_events,safety_ex7}_test.go` | CREATE | AW, AU, AZ |
| `internal/models/agent_activity.go` (+ test) | CREATE | validators, predicate, bounds |
| `internal/models/{events,self_update}.go` | MODIFY | new fields, params, disposition |
| `internal/server/db/{self_update_repo,interfaces}.go` | MODIFY | step 0, `SelfUpdateByAttempt` |
| `internal/server/db/self_update_attempt_pg_test.go` | CREATE | R (PG-gated) |
| `internal/testutil/mocks.go` | MODIFY | mock step 0, `SelfUpdateByAttempt` |
| `internal/server/commands/self_update.go` (+ `_ref_test.go`) | MODIFY / CREATE | AD-006, AD-007; S |
| `internal/server/handlers/{agent_event_self_update,handlers}.go` (+ `agent_event_ref_test.go`) | MODIFY / CREATE | fields passed inside the gate, dedupe helper; HT |
| `internal/server/expect/activity.go` | MODIFY | the one source change in `expect` |
| `internal/server/expect/{harness_test,expect7_test,safety_ex7_test}.go` | MODIFY / CREATE | H7, Z |
| `internal/server/expect7_docs_test.go` | CREATE | D |
| `web/src/utils/commandLifecycle.ts` (+ test), `expect7.safety.test.ts` | MODIFY / CREATE | WL, WZ |
| `README.md`, `CHANGELOG.md` | MODIFY | FR-040, FR-041 (see Deviation 6) |

Explicitly unchanged and verified by `git diff --stat main...HEAD` (empty): `scripts/install.sh`, everything under `cmd/`, `internal/agent/config.go`, `doctor.go`, `maintenance_doctor.go`, `collectors/`, `internal/server/expect/{window,tracker,finish,selfupdate}.go`, `web/src/utils/activity.ts`, `web/src/components/ActivityLog.tsx`. No file under any `migrations` directory is in the diff. `models.IsValidOrigin` is unchanged.

## Test Summary

92 Go tests named `TestEX7_` (the ADD counts 89; the three extras are listed below) and 11 web tests (2 new describes, 3 + 8 tests).

| Group | File | Go tests | Status |
|---|---|---|---|
| AW | `internal/agent/power_wait_step_test.go` | 10 | pass |
| AU | `internal/agent/update_events_test.go` | 18 | pass |
| AZ | `internal/agent/safety_ex7_test.go` | 8 | pass |
| M | `internal/models/agent_activity_test.go` | 7 (5 + 2 extra) | pass |
| S | `internal/server/commands/self_update_ref_test.go` | 16 (15 + 1 extra) | pass |
| HT | `internal/server/handlers/agent_event_ref_test.go` | 8 | pass |
| R | `internal/server/db/self_update_attempt_pg_test.go` | 6 | pass against PostgreSQL 16; skip without `RIOT_TEST_PG_URL` |
| H7 | `internal/server/expect/expect7_test.go` | 16 | pass |
| Z | `internal/server/expect/safety_ex7_test.go` | 2 | pass |
| D | `internal/server/expect7_docs_test.go` | 1 | pass |
| WL / WZ | `web/src/utils/commandLifecycle.test.ts`, `expect7.safety.test.ts` | 2 describes | pass |

Extras: `TestEX7_AC023_CommandRefPredicate` and `TestEX7_AC011_SelfUpdateParamsAttempt` (M), `TestEX7_AC034_DedupeFlagOnlyForReferenceDerivedLinks` (S).

### AC to test

| AC | Tests |
|---|---|
| AC-001 | AW `TestEX7_AC001_RebootWaitReportsStepOnce`; H7 `TestEX7_AC001_WaitingLabelWhileOnline` |
| AC-002 | AW `TestEX7_AC002_PowerRunsRightAfterFinalProbe`; H7 `TestEX7_AC002_StepChangeClearsSuffix` |
| AC-003 | AW `TestEX7_AC003_TimedOutWaitFailsInWaitStep`; H7 `TestEX7_AC003_TimeoutClearsSuffixNoNotification`; WL `EXPECT-007 [AC-003] wait step labels and failed-in-wait label` |
| AC-004 | AW `TestEX7_AC004_ShutdownWaitReportsStepAndInitiates`; H7 `TestEX7_AC004_ShutdownWaitingLabel` |
| AC-005 | AW `TestEX7_AC005_FreeLockNoStep`; H7 `TestEX7_AC005_NoStepPlainRebooting` |
| AC-006 | AW `TestEX7_AC006_RetakenLockReportsOnceDeadlineFromFirstHeld` |
| AC-007 | AW `TestEX7_AC007_StepReportFailureChangesNothing` (400, 500, dropped, blocked); AZ `TestEX7_AC007_GateCallsOnlyEnterOnReporter` |
| AC-008 | H7 `TestEX7_AC008_OfflineHidesSuffix` |
| AC-009 | AW `TestEX7_AC009_PostPatchRebootReportsNoWaitStep`; AZ `TestEX7_AC009_OnlyStandaloneHandlersAttachWaitReporter` |
| AC-010 | H7 `TestEX7_AC010_OldAgentPlainRebooting` |
| AC-011 | AU `TestEX7_AC011_SelfUpdateEventsCarryAttemptAndVersions`; S `TestEX7_AC011_AttemptStartStoresAttemptID`; H7 `TestEX7_AC011_SelfUpdateWithAttemptNoNotification`; R `TestEX7_PG_AttemptStoredInParams` |
| AC-012 | AU `TestEX7_AC012_AttemptIDUniqueAndShaped`, `TestEX7_AC012_RestartedInstanceReusesHandoffAttempt` |
| AC-013 | AU `TestEX7_AC013_OldHandoffYieldsNoReference` (both paths); S `TestEX7_AC013_NoReferenceAttributedAsEX2` |
| AC-014 | AU `TestEX7_AC014_MessagesByteForByte` (self path, command path, four signed texts) |
| AC-015 | S `TestEX7_AC015_StructuredVersionsWinOneDebug`; H7 `TestEX7_AC015_LabelUsesStructuredVersions` |
| AC-016 | M `TestEX7_AC016_StructuredVersionsValidation`; S `TestEX7_AC016_InvalidStructuredFallsBackToMessage` |
| AC-017 | S `TestEX7_AC017_BothInvalidNoRecord`; H7 `TestEX7_AC017_BothInvalidOfflineAlerts` |
| AC-018 | S `TestEX7_AC018_ReplayedAttemptNoRecordNoCount` (zero observer calls, zero broadcasts); R `TestEX7_AC018_PG_ReplayAfterCloseMatchesInTx`, `TestEX7_PG_ConcurrentSameAttemptOneRecord`; H7 `TestEX7_AC018_ReplayedAttemptOfflineAlerts` |
| AC-019 | S `TestEX7_AC019_UnknownAttemptOnlyNoAttemptRecord` |
| AC-020 | H7 `TestEX7_AC020_FleetUpdateTiedToCommand` |
| AC-021 | S `TestEX7_AC021_CommandRefFailureLinksCommandNotRecord` |
| AC-022 | S `TestEX7_AC022_CommandRefCompletionLinksCommand` |
| AC-023 | M `TestEX7_AC023_RefTokenValidation`, `TestEX7_AC023_CommandRefPredicate`; S `TestEX7_AC023_InvalidCommandRefsFallBack` (10 cases); HT `TestEX7_AC023_InvalidRefsAnsweredOkAndStored` |
| AC-024 | M `TestEX7_AC024_BothReferencesMeanNone`; S `TestEX7_AC024_BothRefsProcessedAsEX2` |
| AC-025 | H7 `TestEX7_AC025_PR46ReturnKept` |
| AC-026 | H7 `TestEX7_AC026_OldAgentTimelinesUnchanged` |
| AC-027 | M `TestEX7_AC027_NewPayloadDecodesIntoLegacyShape`, `TestEX7_AC027_WaitStepPassesStepPattern`; AU `TestEX7_AC027_AgentEventJSONKeys` |
| AC-028 | S `TestEX7_AC028_StoreUnreadableFailsOpen`; H7 `TestEX7_AC028_FailOpenOfflineAlerts` |
| AC-029 | S `TestEX7_AC029_OtherDeviceRefsIgnored`; HT `TestEX7_AC029_CrossDeviceRefsIgnoredHTTP` (hawaii's real command ID and attempt ID sent by vermont) |
| AC-030 to AC-033 | AU `TestEX7_AC030_StartRetriedApplyWaitsLockHeld`, `TestEX7_AC031_BudgetBoundedOneWarn`, `TestEX7_AC032_NoRetryOn400`, `TestEX7_AC033_CommandPathNeverWaits`, `TestEX7_AC033_BusySlotAttemptOffCallerGoroutine` |
| AC-034 | HT `TestEX7_AC034_RetriedEventsStoredOnce`, `TestEX7_AC034_CappedAndFailOpenAttemptStartsStored`, `TestEX7_AC034_DedupeReadFailureStoresEvent`; S `TestEX7_AC034_DedupeFlagOnlyForReferenceDerivedLinks` |
| AC-035 | HT `TestEX7_AC035_DoubleDeliveryStoresOnePerType`; H7 `TestEX7_AC035_OneOfflineOneOnlinePerAttempt`; AU `TestEX7_AC035_AgentLogsAndHandoffBounded`; AW `TestEX7_AC035_AtMostTwoStepChanges` |
| AC-036 | AZ `TestEX7_AC036_NoNewSudoInvocation`; diff check below |
| AC-037 | AZ `TestEX7_AC037_AgentTestsRunNothingReal`, `..._ScannerCatchesEachForbiddenForm`, `..._UpdatePipelineRefusesRealProcessInTest`, `..._HandoffWatcherRefusesRealProcessInTest`, `..._RealRunnerFallbacksGuarded`; Z `TestEX7_AC037_NoPowerUpdateOrAgentExecution`, `..._ScannerCatchesEachForbiddenForm_Server`; WZ `EXPECT-007 [AC-037] web tests and sources spawn nothing` |
| AC-038 | HT `TestEX7_AC038_UpdateAvailableUnchanged`, `TestEX7_AC038_ExtraUpdaterIgnoresNewFields`; whole suite unmodified (Deviation 1) |
| AC-039 | diff check (no migration, no CHECK change, `IsValidOrigin` untouched) |
| AC-040 | D `TestEX7_AC040_ReadmeAndChangelog` |
| Supporting | AW `TestEX7_Q2_EnterStepDoesNotDelayGate`; AU `TestEX7_FR012_*`, `TestEX7_FR031_*` (2), `TestEX7_NFR003_*` (3); S `TestEX7_FR019_*`, `TestEX7_FR025_*`; R `TestEX7_PG_SelfUpdateByAttemptDeviceScoped`, `TestEX7_PG_MockParityAttempt`, `TestEX7_NFR009_PG_AttemptLookupUsesPartialIndex` |

Security conditions: C-1 `TestEX7_AC037_{UpdatePipeline,HandoffWatcher}RefusesRealProcessInTest`, `RealRunnerFallbacksGuarded`. C-2 `TestEX7_AC023_InvalidCommandRefsFallBack` (synthetic `installer` origin) and `TestEX7_AC023_CommandRefPredicate`. C-3 a `TestEX7_AC018_ReplayedAttemptNoRecordNoCount`, `TestEX7_AC034_RetriedEventsStoredOnce`; b `TestEX7_AC034_CappedAndFailOpenAttemptStartsStored`; c `TestEX7_AC023_InvalidCommandRefsFallBack` (nil `finished_at`); d `TestEX7_AC007_GateCallsOnlyEnterOnReporter`. C-4 `TestEX7_NFR003_AgentContextEndStopsRetryNoWarn`, `TestEX7_AC033_BusySlotAttemptOffCallerGoroutine`, `TestEX7_AC031_BudgetBoundedOneWarn`. C-5 as listed in the QA gate below.

### Test Run Output

Environment: Windows, `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`. A throwaway `postgres:16` container (`riot-ex7-pg`, database `riot_ex7_test`) was used for the PG-gated tests and stopped afterwards.

**Run 1: `go build ./... && go vet ./... && go test ./...`** (with `RIOT_TEST_PG_URL` set, so PG-gated tests ran, not skipped). Exit 0, 31 packages `ok`:

```
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer	0.958s
ok  	github.com/DesyncTheThird/rIOt/internal/agent	38.444s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	13.620s
ok  	github.com/DesyncTheThird/rIOt/internal/extraupd	2.926s
ok  	github.com/DesyncTheThird/rIOt/internal/logbudget	0.410s
ok  	github.com/DesyncTheThird/rIOt/internal/maintenance	2.832s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.889s
ok  	github.com/DesyncTheThird/rIOt/internal/redact	0.394s
ok  	github.com/DesyncTheThird/rIOt/internal/releasekey	0.415s
ok  	github.com/DesyncTheThird/rIOt/internal/resilient	1.439s
ok  	github.com/DesyncTheThird/rIOt/internal/server	3.701s
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	0.574s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	0.485s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	1.903s
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	336.433s
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	0.822s
ok  	github.com/DesyncTheThird/rIOt/internal/server/expect	2.278s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	12.398s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ingest	0.720s
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	1.271s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	1.258s
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc	2.438s
ok  	github.com/DesyncTheThird/rIOt/internal/server/partition	0.612s
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	1.238s
ok  	github.com/DesyncTheThird/rIOt/internal/server/quota	0.467s
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	0.642s
ok  	github.com/DesyncTheThird/rIOt/internal/server/storage	1.793s
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary	0.811s
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	0.663s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	0.931s
ok  	github.com/DesyncTheThird/rIOt/scripts	66.201s
```

(The known pre-existing flakes did not occur in this run.)

**Run 2: `cd web && npx tsc --noEmit && npm run test:run`:** `tsc` exit 0. vitest: `Test Files 127 passed (127)`, `Tests 2304 passed (2304)` (baseline 2293, +11).

**Run 3: `go test ./internal/models/... ./internal/agent/... ./internal/server/... -run TestEX7_ -v`** (PG env set): exit 0; 92 top-level `--- PASS: TestEX7_`, 117 passing subtests, 0 FAIL, 0 SKIP. Package lines: `internal/models`, `internal/agent`, `internal/server`, `.../commands`, `.../db` (28.3 s, the 6 R tests ran against PostgreSQL), `.../expect`, `.../handlers` all `ok`. Without `RIOT_TEST_PG_URL` the 6 R tests skip (the gate is `parityPGURL`, which also requires the database name to contain "test").

**Lint:** `go vet ./...` clean on Windows; `GOOS=linux go build ./...`, `GOOS=linux go vet ./internal/agent/...` and `GOOS=linux GOARCH=arm GOARM=7 go build ./cmd/riot-agent` clean. `gofmt` clean for every changed Go file (checked on LF-normalised content; the working tree uses CRLF through `core.autocrlf`, so a raw `gofmt -l` lists the whole repository). The web project has no lint script; `tsc --noEmit` is clean.

**Not run:** the race detector (`-race`). This host has no C toolchain, so `CGO_ENABLED=0`. Linux CI runs it. The sender, slots, reporter and dedupe mutex are the code to watch.

## Definition of Done (ADD Section 13)

- Section 4 changes implemented; the "explicitly unchanged" list is untouched (diff check above).
- Every test of Sections 8 and 8.1 exists under its ADD name and passes; no test sleeps in real time (`time.After` is used only as a failure timeout; the AZ scan forbids `time.Sleep`).
- No migration, no CHECK change, `IsValidOrigin` unchanged.
- AC-036 diff check: `git diff --stat main...HEAD -- scripts/install.sh cmd internal/agent/config.go internal/agent/doctor.go internal/agent/maintenance_doctor.go internal/agent/collectors` is empty. `TestEX7_AC036_NoNewSudoInvocation` passes.
- Bounds appear as code comments at each bound (sender, reporter, repo, handler).
- QA gate C-5: no EX7 file is in `ex2Preexisting` or `ex7Preexisting` (asserted by Z); `TestEX7_AC040_*` builds the package-lock word and the helper name from fragments; `TestEX7_AC002_*` shows on the fake clock that the final probe is directly followed by the power runner and that `power_action` has not been written at that point; `TestEX7_AC023_*` and `TestEX7_AC029_*` use another device's real command ID and attempt ID and assert identical 200 bodies; `TestEX7_PG_ConcurrentSameAttemptOneRecord` ran and passed against PostgreSQL (not skipped); the process-guard self-tests and the AST pin pass.
- The scan lists printed with `-v` (agent, server, web) include every new or modified file of the diff.
- **Merge coordination (C-2):** whichever of EXPECT-007 and EXPECT-008 merges second must re-run the non-allowed-origin case of `TestEX7_AC023_InvalidCommandRefsFallBack` against the real `installer` origin, and its merge note must name SEC-002. See Note for QA 1 for what that test pins today.

## Items ADD Section 12 note 14 asks to state

- `ReceiveAgentEvent` now reads `command_id` (and `attempt_id`, `from_version`, `to_version`) on the three `agent_update_*` types, only inside the authenticated-ID gate, as raw input the Service validates; a command reference needs an `IsValidOrigin` origin, `agent_update` action, dispatched, and live or finished at most 10 minutes earlier with a non-nil `finished_at`. It supersedes EXPECT-002 X-2 narrowly.
- A repeat of a reference-linked event is answered `200 {"status":"duplicate"}` and is neither stored nor broadcast.
- `HTTPClient.ReportEvent` now returns `*StatusError` (body read capped at 4 KiB) for status >= 400. The error text changed from `event report failed: <body>` to `event report failed: status N: <body>`; only logs show it.
- The update pipeline (preflight probe, unit reset, apply launch) and the hand-off watcher's unit-state query now refuse to start a process in a test binary when no fake runner is injected (`processRunner()`).
- Note 5 (existing tests): **none** needed a change because of FR-009 or the process guard. `activity_test.go` "a dpkg-lock wait is reported" and `host_test.go` `TestEX_AC011_*` pass unmodified (their harness device is online); no existing updater or hand-off test relied on a real process.

## Bounds as implemented (ADD AD-014)

| Datum | Implementation |
|---|---|
| `params.attempt_id` | at most 64 bytes per record (agent sends 32 hex), only when the start carried a valid reference; re-validated on read; the 6-per-24h cap is untouched; no migration, no new index |
| Wait-step reports | at most 2 step changes per standalone power command (`wait_dpkg_lock`, `power_action`) inside the existing 64-step cap (`TestEX7_AC035_AtMostTwoStepChanges`); at most one reporter goroutine per command, joined on every return path |
| Stored update events | reference-linked repeats stored zero extra times (`storeAgentUpdateEvent`, in-process mutex, `HasCommandEvent` only with a non-empty reference-derived link); timing-linked or unlinked events are bounded by the agent's 5 attempts and only when a response was lost |
| New types | no table, event type, record kind or origin |
| Agent hand-off file | one file, overwritten per attempt, plus at most 64 + 2 x 32 bytes (`TestEX7_AC035_AgentLogsAndHandoffBounded` asserts size <= 512 and one file) |
| Agent retry state | `models.UpdateEventMaxAttempts = 5`, `UpdateEventRetryBudget = 90s`, `UpdateEventAttemptTimeout = 10s`, schedule 0/5/15/35/75 s; one retry slot and one single-shot slot per event type: at most 6 sender goroutines (`TestEX7_NFR003_AtMostOneRetrierPerType` proves 6 in flight, a 3rd per type dropped); all counted in `cmdWG`, ended when the agent's context ends; nothing queued or persisted |
| Agent logs | Debug per attempt and per step; one Warn per abandoned event with only `type` and `attempts` (`TestEX7_AC031_*`); no Warn when the agent's context ended (`TestEX7_NFR003_AgentContextEndStopsRetryNoWarn`) |
| Server work per event | at most one device-scoped PK read (command reference), or step 0 inside the existing transaction, or one to two selects (attempt outcome), plus at most one `EXISTS`; one mutex, no per-event state |

## Deviations from ADD

These trigger architect review.

1. **`models.SelfUpdateParams` gained its third argument, so one existing test line changed.** `internal/models/self_update_test.go` calls it with `""` as the new argument. No assertion changed. This is the only edit to an existing test body; all other existing tests pass unmodified.
2. **`expect/activity.go` has two conditions, not one.** AD-003 specifies `dev.online && step == wait`. FR-009 and BR-006 also require the suffix to disappear when the command reaches a terminal state, and a terminal command whose current step is still the wait (a result that carries no step) would otherwise keep claiming a wait. The line is `dev.online && !w.cmd.State().IsTerminal() && w.cmd.Step == models.StepWaitDpkgLock`. `TestEX7_AC002_StepChangeClearsSuffix/the_result` pins it. Still the only `expect` source change.
3. **Three small agent seams not named in ADD Section 4**, each needed to test an ADD-required behaviour without a process, a socket or a real restart:
   - `Agent.updateApply` (replaces `performUpdate` in `handleTriggerUpdate`) and `Agent.restartHook` (replaces `restartSelf`), both nil in production. Without them the command-path "Agent updated to <v>" text (AC-014) and the command-path events (FR-012, AC-027, AC-033) cannot be driven without `os.Exit`.
   - `performUpdate` is split into `performUpdate` plus `bindHandoff(ctx, p)`, so AC-012 can exercise the real hand-off persistence without downloading a release.
   - The one-second delay before `restartSelf` in `handleTriggerUpdate` now goes through `a.sleep` (the pause seam) instead of `time.Sleep`. In production `a.pause` is nil and `a.sleep` is a timer, so behaviour is identical.
4. **`deliverFirstSync` with a busy retry slot makes no retries and spawns no goroutine.** AD-010 says the busy-slot single attempt of the background part of `deliverFirstSync` runs on a new goroutine. In this implementation the first attempt of `deliverFirstSync` is always the synchronous one (today's behaviour), and the retry slot is taken only when a retry is needed; when it is busy, the event simply gets its one synchronous attempt. The single-shot slot is used by `deliverAsync` only. The goroutine bound (6) holds, and the busy-slot rule for the command path is tested (`TestEX7_AC033_BusySlotAttemptOffCallerGoroutine`).
5. **R tests are in package `db_test`, not `db`.** `TestEX7_PG_MockParityAttempt` needs `testutil`, which imports `db`, so the file reuses the existing external-package helper `newParityPool` (same gate as `pgTestURL`: `RIOT_TEST_PG_URL`, database name must contain "test"). All six R tests live in `self_update_attempt_pg_test.go` as the ADD names.
6. **README and CHANGELOG were written by the engineer, not the technical writer.** The D test (`TestEX7_AC040_*`) is in the ADD's test list and needs the text to exist. The README section is `### Agent-side activity reporting` (inside `## Expected Activity`), and one sentence in `### Agent self-updates` ("The agent's own `command_id` is never read") now says it "was never read before" this feature, keeping the phrase the EXPECT-002 docs test requires. The reporting release is given as "the first release after v2.85.0" because no version exists yet; the technical writer should put the real number in. The writer should also own the final wording.
7. **Z scan scope.** `safety_ex7_test.go` (server) excludes `internal/agent` (its tests are scanned by AZ with agent rules, where package-manager words are data) and reuses the EXPECT-002 scan and its exception list as its pre-existing table. It checks staleness only for entries whose file is in its own scan set.
8. **Added tests beyond the ADD's 89:** three (listed above).

No blocker file was needed: the ADD was consistent and every component could be built as specified.

## Notes for QA

1. **C-2 and the `installer` origin (probe hardest).** `TestEX7_AC023_InvalidCommandRefsFallBack` includes a synthetic server-created origin `installer`. The *reference* correctly never links it (the link is a timing link, `command_attr` is not `agent`, `Dedupe` is false). But EXPECT-002's own timing rule in `CreateSelfUpdate` step 1 (`origin <> 'agent_self_update'`, SQL and mock) is unchanged by this story, so it still attributes a start to such a row by timing. The test asserts exactly that and says so in a comment. EXPECT-008 adds the real `installer` origin and narrows that predicate (its AD-017). Whoever merges second re-runs the case and cites SEC-002.
2. **FR-004 / Q-2 ordering.** Probe `handleReboot` / `handleShutdown` on a real host under a held dpkg lock only through fakes: the AW tests show the leaving step follows the power runner, a slow durable write or a full notifier queue does not delay the runner, and a blocked, rejecting, 5xx or dropping progress channel gives byte-identical probe, runner, result and fake-clock traces. The residual R-3 (result durability gap on a very fast reboot) is measured only on the fake clock (gap 0); nothing here proves it on real hardware.
3. **Retry boundaries.** `TestEX7_AC031_*` runs 5 attempts at 0/5/15/35/75 s on the fake clock. An attempt's own 10-second timeout is a real `context.WithTimeout` (the fake sinks return immediately), so real-timeout behaviour is only reasoned about, not exercised. Try: server down for 80 s at release time, a 429 with `Retry-After` (the sender ignores `Retry-After` and follows its own schedule), and agent SIGTERM during a retry.
4. **Dedupe scope.** The in-process mutex guards check-then-insert for one server process only (R-1). A timing-linked event with a lost response can be stored up to 5 times (X-2, R-2); `TestEX7_AC034_*` pins both the dropped repeats and the stored capped / fail-open ones.
5. **Mixed fleets.** `TestEX7_AC027_NewPayloadDecodesIntoLegacyShape` decodes into a frozen copy of the e120031 `AgentEvent` and runs the unchanged `ParseSelfUpdateStart`; a real old server was not run. An old agent's hand-off (no attempt, no versions) is covered on both the restarted-process and the old-process paths.
6. **Platform.** All agent tests ran on Windows; the Linux-only `update_preflight_linux.go` path was compile- and vet-checked with `GOOS=linux` but its tests were not run. `TestEX7_AC037_UpdatePipelineRefusesRealProcessInTest` asserts `p.preflight` fails closed, which on Linux depends on the helper not being trusted *or* the refusing runner; both end in an error and neither starts a process. A Linux CI run with `-race` is the remaining check.
7. **Installer re-run: none required.** No change to `install.sh`, sudoers, the systemd unit, `riot-agent-apply`, `riot-apply-privileges`, the agent configuration schema, the collector whitelist or `doctor.go`; the agent invokes no new command through sudo (pinned by `TestEX7_AC036_NoNewSudoInvocation`). Agents receive the change by the normal self-update or **Update agents**. The first update *into* this release is recognised by message and timing only (FR-014).
8. **Environment variables:** none added.
