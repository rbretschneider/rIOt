# QA Report

**Story ID:** EXPECT-002 (agent self-update as expected activity)
**QA Engineer:** QA Agent (Sonnet 5.5)
**Date:** 2026-10-26
**Branch:** `feat/expect-002` (reviewed at `6cc3a74`, base `c55e4bd`)
**Verdict:** PASS WITH NOTES

No blocking findings. No implementation file was changed by QA. No test was added to the repository (the edge probes were throwaway and deleted; four proposed permanent tests are kept outside the repo, see "Tests Added by QA").

---

## Test Run Summary

Windows host, `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`. PG-gated tests ran against a throwaway `postgres:17-alpine` container (`qa_expect002_pg`, removed afterwards). Nothing touched a remote host.

| Run | Result |
|---|---|
| `go build ./...`, `go vet ./...` | exit 0, no output |
| `go test ./... -count=1` with `RIOT_TEST_PG_URL` | 30 of 31 packages `ok`; 1 FAIL in `internal/extraupd` (see Finding N1: pre-existing flake, package untouched by the diff, `ok` on 5 isolated runs) |
| `go test -count=1 -p 1 -v -run TestEX2_` over `models`, `server`, `db`, `commands`, `expect`, `handlers` (with PG) | 112 `--- PASS`, 0 FAIL, 0 SKIP (110 distinct functions; `TestEX2_AC024_AgentReportNotFoundHTTPAndWS` and `TestEX2_AC020_StoreFailureFailsOpen` each exist in two packages) |
| `go test -count=3 -shuffle=on` over `models`, `expect`, `commands`, `handlers`, `server` | all `ok`, no order dependence |
| `cd web && npx tsc --noEmit -p .` | clean |
| `cd web && npm run test:run` | 114 files, 2015 tests passed |
| `-race` | not run by QA (Windows, no CGO). The orchestrator reports a Linux `golang:1.24` run, `-race -count=1 -p 1 ./...`, all ok |

Flaky tests found: `TestDetect_EU_AC009_BackToBackCallsNeverSeeInFlight` (once, under full-suite load; unrelated to this story). `TestDUX_PG_AttachAuditStormBoundedAndCoalesced` failed once in one mutation run and passed 3 of 3 alone and in the full run; also unrelated (it does not touch self-update code).

Name audit: every `TestEX2_` name in ADD sections 8, 8.1 and 8.2 exists in the code under that exact name (extracted from the ADD and diffed; the only residue is the prefix text `TestEX2_`). Every web `describe` named in section 8 exists (`EXPECT-002 [AC-001]`, `[AC-008]`, `[AC-016]`, `[AC-020]`, `[AC-023]`, `[AC-029]`).

## AC Coverage Audit

"Would the tests catch it?" was checked for the load-bearing rules by mutation: a copy of the tree was mutated one line at a time and the story's tests were run (results in "Mutation check" below). All ACs are COVERED. Tests named `S` are in `internal/server/commands/self_update_test.go`, `H2` in `internal/server/expect/selfupdate_test.go`, `HT` in `internal/server/handlers/agent_event_self_update_test.go`, `CE` in `command_events_test.go`, `R` in `internal/server/db/self_update_repo_pg_test.go`, `M` in `internal/models/self_update_test.go`.

| AC | Status | Tests | Notes |
|---|---|---|---|
| AC-001 | COVERED | H2 `AC001_ReleaseTimelineNoNotification` (L34), `AC001_LabelFromStartUntilRegistration`; web `[AC-001]`, FleetOverview/DeviceDetail | Read in full. Asserts the exact FR-016 and FR-017 texts, record versions and `succeeded`, verifying_at = registration time, started/completed events linked and stored as sent, zero alerts and pushes, label gone |
| AC-002 | COVERED | H2 `AC002_FleetReleaseFourRecordsZeroPushes` | Four devices, four records, four info offlines, zero alerts |
| AC-003 | COVERED | H2 `AC003_ContainerDeathDuringSelfUpdateAlerts` | `Suppressed=false`, alert notified |
| AC-004 | COVERED | H2 `AC004_OfflineAfterCompletionAlerts` | Offline at 10:08, inside the 10-minute bound but after the registration, alerts |
| AC-005 | COVERED | H2 `AC005_FastRestartClosesOnRegistration`; HT `AC005_StaleHeartbeatAfterFastReturnNoAlert`; `internal/server/expect2_register_test.go` `AC005_RealDetectorSeesTheRegistrationHeartbeat` | The last one drives the real `offlineDetector` and `RegisterDevice`. Mutation M5 (no heartbeat refresh) is killed |
| AC-006 | COVERED | S `AC006_StartDuringDispatchedCommandAttributed`, `AC006_StartWithinTenMinutesAfterCommandAttributed` (9m59s and exactly 10m attributed); H2 `AC006_FleetUpdateShowsCommandLabel` (label "by ryan", 10m01s becomes a record) | No double record |
| AC-007 | COVERED | S `AC007_OldCommandDoesNotCapture` (10m01s), `AC007_QueuedCommandDoesNotCapture`; R `AC007_PG_AttributionPredicate` | PG predicate boundary is killed by mutation M14 (`>=` to `>`) and M24 (queued captures) |
| AC-008 | COVERED | H2 `AC008_NotBackEscalatesOnceNamingVersions` (L228), `..._EscalationIgnoresUnrelatedCooldown`, `..._NoSecondEscalationAfterRestart`, `..._EscalatedLabelAndOfflineCount`; web `[AC-008]` | Exact FR-020 text, one push, `timed_out`/`unconfirmed_offline`, window outcome `escalated`, 1 h later still one, marker list is exactly `device`, label survives restart with versions |
| AC-009 | COVERED | H2 `AC009_FailedWhileOnlineWarningNoPush`; S `AC009_CompletedAndFailedMoveTheRecord` | Exact FR-018 text, no alert, broadcast, later offline alerts |
| AC-010 | COVERED | H2 `AC010_FailureAfterReturnSetsFailed`; S subtest "failed after the return" | `verifying` to `failed`, one event, no alert |
| AC-011 | COVERED | H2 `AC011_ReturnOnNewVersionWithoutCompletionSucceedsAtBound` | "v2.80.0" accepted, `succeeded` at 10:10:00 with no further event |
| AC-012 | COVERED | H2 `AC012_ReturnOnOldVersionFails`; S `Q1_RegistrationAfterBoundIgnored/wrong version`; HT registration test | Exact FR-019 text, later completed linked without change, later offline alerts |
| AC-013 | COVERED | H2 `AC013_NotConfirmedWhileOnline` | Exact FR-021 text, exactly one after 1 h |
| AC-014 | COVERED | H2 `AC014_RestartMidWindow` | Online note still names the update after restart, completed event attributed, no `device_offline` alert |
| AC-015 | COVERED | H2 `AC015_BoundDuringDowntimeEscalatesOnce`, `AC015_DeadDuringDowntimeEscalatesNotUnconfirmed` (INFO-4), `AC015_GateOpensAfterTheCapWithoutADetectorPass` | Mutation M15 (gate removed) is killed by the two INFO-4 tests |
| AC-016 | COVERED | M `AC016_ParseRejectsMalformed`, `..._ParseAcceptsTheAgentForm`, `..._ParseBounds`; H2 `AC016_MalformedStartAlertsAsToday`; S `MalformedStartCreatesNothing`; HT `AC016_MalformedStartStoredAsToday`; web `[AC-016]` | All seven FRD inputs. QA's own parser probe (below) found no divergence |
| AC-017 | COVERED | S `AC017_RepeatedStartsOneRecord` (50 starts, bound unchanged, one create broadcast, no log above debug), `AC017_RateCapSixPerDayOneWarning`; R `AC017_PG_ConcurrentStartsOneRecordOneCapEvent`; H2 `AC017_CappedStartOfflineAlertsNoDebugAbove` | Cap off-by-one (M9) is killed |
| AC-018 | COVERED | H2 `AC018_OtherDeviceUnaffected`, `AC018_OtherDeviceRegisterCompleteFailLeavesRecord` (INFO-1); HT `AC018_MismatchedAuthenticatedDeviceIgnored` | M7 (auth-id check removed) is killed |
| AC-019 | COVERED | M `AC019_ReasonSanitizedAndBounded`; H2 `AC019_FailedEventMessageSanitized`; S `AC019_FailedReasonSanitizedInResult` | NUL, bidi override, zero-width, newline, invalid UTF-8, 2 KB; quoted reason at most 200 bytes. M8 (raw reason) is killed |
| AC-020 | COVERED | H2 `AC020_StoreFailureFailsOpen`; S `AC020_StoreFailureFailsOpen`; HT `AC020_EventStoredWhenRecordStoreFails` | Event stored, 200 returned, alert fires. See N4 for the one untested error path |
| AC-021 | COVERED | S `AC021_CompletionWithoutRecordUnlinked` | Includes 10m00s linked and 10m01s unlinked, and never linked to a command (X-16) |
| AC-022 | COVERED | H2 `AC022_BoundedEventsLogsMemory` | Exact event counts per device, zero Warn/Error, exactly one Info line, `Stats().Windows == 0` and `ClosedRecent == 0` after 6 minutes |
| AC-023 | COVERED | H2 `AC023_ActivityCarriesVersionsAndActor`; CE `AC023_CommandEventsEndpoint`, `..._NotFoundIdentical`, `..._EmptyArraysNotNull`, `..._ServerError`; web FleetOverview (label, actor, start time, Expected offline count, offline dot, live set and clear, escalated danger styling), DeviceDetail (link opens the read-only record), `SelfUpdateDetails` (no controls, omitted count) | The 5 s requirement rests on the 1 s tracker tick (`tracker.go:1116`) and the web live channel test |
| AC-024 | COVERED | M `AC024_ActorSelfUpdateEdges`; S `NeverClaimedOrDelivered`, `AgentReportNotFoundHTTPAndWS` (ack, progress x2, result x2, late result on `timed_out`, foreign device), `CancelNotCancellable`, `MockRejectsOtherActorsOnSelfUpdate`; HT `AgentReportNotFoundHTTPAndWS` (HTTP 404 body, WS receipt `not_found` with no status), `CancelNotCancellableOverHTTP`; R `PG_CheckForbidsQueued`, `PG_CheckBoundsDeadlineAndKeys`, `PG_AgentOperatorServerGuards`, `TransitionSQLOriginGuards`, `MockGuardParity` (about 1,400 combinations, PG vs mock) | Mutations M3 and M3b (origin guards) are killed by three tests each |
| AC-025 | COVERED | H2 `AC025_CommandAndRecordOneLabel` | Latest dispatch takes the label and the link |
| AC-026 | COVERED | H2 `AC026_OlderAgentNoCompletionSucceeds`, `AC026_CompletionBeforeRestartStillCovered` (X-1/X-17), `AC026_OtherWordingAsToday` | |
| AC-027 | COVERED | HT `AC027_UpdateAvailableUnchanged` | Even a start-looking text under `agent_update_available` creates nothing; repository never called |
| AC-028 | COVERED | All pre-existing `TestEX_*` and CMD-LIFECYCLE tests pass; H2 `AC028_CommandAgentUpdateUnchanged` | One existing test line changed (Go deviation 1, judged below) and one web count (web deviation 2) |
| AC-029 | COVERED | `safety_ex2_test.go` `AC029_NoPowerUpdateOrAgentExecution`, `AC029_ScannerCatchesEachForbiddenForm`; web `selfUpdate.safety.test.ts` | See "Safety" below |
| AC-030 | COVERED | `expect2_docs_test.go` `AC030_ReadmeAndChangelog` | QA also read the README section and CHANGELOG entries: every FR-032 item, the C-5 budget text and the FR-033 "no agent upgrade, no `install.sh` re-run, already-deployed agents" wording are present |

### Supporting tests (ADD 8.1 and 8.2)

All present and passing: Q-1 `Q1_RegistrationAfterBoundIgnored`, Q-2 `Q2_OneLinkPerEvent`, X-1 (two), X-3, X-7, X-19 `X19_AgentUpdateSeverityClamped` (`""`, `"panic"`, `"WARNING"`, 1 KB; known values kept), `NoteOnlineOnlyForCoveredOffline`, `SweeperSkipsSelfUpdate`, `PG_ListOverdueExcludesSelfUpdate`, `ExistingActorEdgesUnchanged`, `PG_Migration000034UpDownUp`, `PG_UniqueOpenRecord`, `PG_OriginCheck`, `NoCompletionEventForSelfUpdate`, `SelfUpdateTransitionsLeaveResultColumnsAndGateUntouched`, `OutcomeEventsNeverDispatchedNorCeilinged`, `ResolverUnsetFailsSafe`, `StaleOpenRecordRetiredByNextStart`, `PG_StaleRetireInTx`.

### Mutation check (QA, on a scratch copy; the real tree was never edited)

| Mutation | Result |
|---|---|
| M2: Offline coverage runs 1 minute past the bound (`evaluateSelf`) | killed (`ResolverUnsetFailsSafe` only, see N6) |
| M4: Device backstop escalation removed | killed (AC-008 x4, AC-015 x2) |
| M5: Registration no longer refreshes `last_heartbeat` | killed (AC-005 HT, registration test) |
| M6: Severity clamp off | killed |
| M7: Authenticated-id check removed in `linkAgentUpdateEvent` | killed (`AC018_MismatchedAuthenticatedDeviceIgnored`) |
| M8: Failure reason not sanitized | killed (4 tests) |
| M12: `classifyRejected` no longer returns `not_found` for a record | killed (`AC024_AgentReportNotFoundHTTPAndWS` x2) |
| M15: Detector gate removed | killed (both INFO-4 tests) |
| M16: Registration at or after the bound accepted | killed (`Q1`) |
| M17: `NoteOnline` self rule removed | killed |
| M18: Events endpoint distinct 404 body for a foreign command | killed (`NotFoundIdentical`) |
| M3: Origin guard removed for agent/operator/server edges | killed (PG guards, SQL-text test, parity) |
| M3b: Origin guard removed for `ActorSelfUpdate` | killed |
| M14: Attribution boundary `>=` to `>` (PG) | killed |
| M24: Queued command captures a start (PG) | killed |
| M9: Rate cap `>=` to `>` (PG) | killed |
| M25: Stale-record retire removed (PG) | killed |
| M23: `ListOverdue` includes self-update rows (PG) | killed |
| M22: Unique-violation fallback in `CreateSelfUpdate` removed | **survived** (N4) |
| M26: Defensive sweeper skip removed | survived (N5; the `ListOverdue` exclusion and the server-edge guard cover it) |

## Test Quality Findings

- Behaviour versus implementation: the H2 tests assert observable outputs (stored events and their exact text, record rows, escalations, pushes, activity DTO, log levels), not internals. The S and HT tests assert HTTP codes and bodies, WebSocket receipts and stored rows. No brittle private-state assertions found, apart from `h.svc.gate.entries`/`gate.writes` in two S tests, which pin the C-6 requirement itself.
- Isolation: no sleeps, injected clocks everywhere, per-test PG schemas. Passed three shuffled runs.
- Boundaries tested exactly: 9m59s / 10m00s / 10m01s for FR-004 and FR-014; bound minus 1 s and bound for the registration (Q-1); bound plus 1 h exactly and plus 1 s for the stale retire; CHECK at D+10m and D+10m+1s, `exec_deadline_s` 600 and 601; cap 6th and 7th.
- Mocking hygiene: `MockCommandRepo` re-implements the SQL guards and the CHECKs. Its drift is covered by `AC024_MockGuardParity` (PG versus mock, about 1,400 combinations) and the mutation runs above show the mock-based tests and the PG tests are independent kills. The H2 helpers (`registerOn`, `agentEvent`, `detectorPass`) mirror the handlers' order by hand rather than calling the handlers, so they could drift; this is mitigated by the HT tests (real `ReceiveAgentEvent` and `RegisterDevice`) and by `expect2_register_test.go` (real `offlineDetector`). See N8.
- `AC005_StaleHeartbeatAfterFastReturnNoAlert` re-implements the detector predicate inline (the real detector is covered by `expect2_register_test.go`).

## Adversarial Findings

Throwaway probes, all run and deleted (nothing committed):

1. Parser probe, 32 cases in `internal/models`: full-width and Arabic-Indic digits (rejected), NBSP around the arrow (rejected), NBSP and newline padding around the whole message (accepted after trim, as specified), embedded newline, zero-width space, bidi override, NUL, invalid UTF-8, SQL text in a version (all rejected); leading zeros accepted; 5-digit groups accepted and 6-digit rejected; suffix of 16 accepted and 17 or empty rejected; 33-byte version rejected; raw 4,097+ bytes rejected even if only whitespace; `v2.80.0 -> 2.80.0` and `v`/`v` equal versions rejected; downgrade `2.80.0 -> 2.78.0` accepted (harmless: only equality matters). No divergence from FRD 7.2 or AD-011.
2. Strict end of coverage: an offline dated 1 ms before the bound is covered, one dated exactly at the bound alerts and notifies.
3. Two starts in a row: 50 starts at the service level (one record, bound not extended, one create broadcast), and the PG concurrent test. A second start with different versions while the first record is `verifying` is absorbed into it and an offline 1 minute later alerts (see N7).
4. Hostile hostname (`ev<RLO>il<NUL>ho<ZWSP>st<LF>x`): every server-written message and the escalation carry the cleaned name only.
5. Start while a fleet `agent_update` command is dispatched, acknowledged, running (attributed), queued and never dispatched (record), and finished 9m59s, 10m00s (attributed) and 10m01s (record): covered by the story's S, H2 and PG tests; QA's mutations M14 and M24 confirm the PG predicate is what the tests pin.
6. Completion before any return (X-1): coverage continues to the first return after it, and escalates if none by the bound (`X1_*`, `AC026_CompletionBeforeRestartStillCovered`). A start on a device whose stored version already equals `<to>`: resolves `succeeded` at the bound through X-3 (QA's expectation of `timed_out` was wrong; the code follows the ADD).
7. A self-update row confusing operator commands: `TestEX2_AC024_NeverClaimedOrDelivered` shows an `agent_update` command is dispatched normally beside an open record (no target keys, FR-028). QA read every `ListActive`, `ListOverdue`, claim, expiry, revert, report, cancel and retention path: the record only appears, read-only, in `ListActive` and the history views, as designed. No path treats it as a conflict holder.
8. Migration 000034 down and up: `PG_Migration000034UpDownUp` creates a record, a marker and an ordinary command, migrates down (shape CHECK and both indexes gone, self-update rows deleted, markers cascaded, ordinary row kept, narrower origin CHECK back and enforced), migrates up again and creates a record. `TestEX_Repo_PG_MigrationUpDownUp` (EXPECT-001) passes with its one-line change.
9. Cross-device and authentication: a mismatched or missing authenticated id creates no record; another device's registration, completed and failed events leave hawaii's row and label unchanged; the events endpoint is behind `adminAuth` (`router.go:421`) and answers one 404 body for malformed, missing and foreign ids.

## Security C-6 QA Gate (walked item by item)

| # | Condition | Result | Evidence |
|---|---|---|---|
| 1 | Self-update transitions never touch the progress gate and never set `result_bytes`, `steps`, `target_results`, `stderr_tail`, `exit_code`, `duration_ms` or command output | PASS | `SelfUpdateTransitionsLeaveResultColumnsAndGateUntouched` (gate entries and writes zero, columns nil/empty, no output saved), `PG_AgentOperatorServerGuards` (`result_bytes` stays 0), `NoCompletionEventForSelfUpdate`. Code read: `commands/self_update.go` sets only `Step`, `FailureStep` and a bounded `ResultMsg` (512 bytes) |
| 2 | `agent_self_update_*` events and outcome jobs never dispatched (nil rule) and never consume the per-device push ceiling | PASS | `OutcomeEventsNeverDispatchedNorCeilinged` (five outcome events, then an escalation still pushes, nothing held). Code read: `selfupdate.go:387` enqueues `rule: nil`, `publisher.go:154` dispatches only when `j.rule != nil` |
| 3 | `ReceiveAgentEvent` ignores any agent `command_id` and runs self-update work only when the authenticated id equals `{id}` | PASS | `Handler_ValidStartCreatesRecordAndLinks` (agent-supplied `command_id` ignored), `AC018_MismatchedAuthenticatedDeviceIgnored` (other device and unauthenticated), `Q2_OneLinkPerEvent`; mutation M7 killed |
| 4 | Events endpoint returns one identical 404 body for malformed, missing and foreign ids | PASS | `AC023_CommandEventsNotFoundIdentical` (five variants including upper-case UUID and unknown device); mutation M18 killed |
| 5 | Agent reports on a self-update row return `not_found` for ack, progress and result over HTTP and WebSocket | PASS | HT and S `AC024_AgentReportNotFoundHTTPAndWS`; mutation M12 killed. WS ack and progress get no receipt by design; the result receipt reads `not_found` with an empty status |
| 6a | AC-016 passes | PASS | |
| 6b | AC-018 with the INFO-1 extension passes | PASS | `AC018_OtherDeviceRegisterCompleteFailLeavesRecord` |
| 6c | AC-019, AC-020 pass | PASS | |
| 6d | AC-024 (all tests) pass | PASS | 8 Go tests plus 3 mock/SQL guard tests |
| 6e | `TestEX2_Q2_OneLinkPerEvent` passes | PASS | |
| 6f | `TestEX2_AC015_*` including the INFO-4 case | PASS | three tests |
| 6g | `TestEX2_AC005_*` including the INFO-5 timing | PASS | three tests including the real-detector one |
| 6h | `TestEX2_ResolverUnsetFailsSafe` | PASS | one stuck event at bound + 1 h, one Warn, no push, no second event across restart, next start retires the row |
| 7 | The printed AC-029 lists (Go and web) include every new or modified file of the diff | PASS WITH NOTE | Go: the scan printed 146 files; of the 38 `.go` files in `git diff --name-only c55e4bd HEAD -- '*.go'`, 37 are in the list. Missing: `internal/server/events/generator_test.go` (Go deviation 2, a three-line `ListByCommandSplit` stub; QA read it, it is benign; N2). Web: the scan covers the five ADD sources, `models.ts`, `client.ts`, `demo-client.ts`, `labels.ts`, `labels.test.ts` and every test mentioning `EXPECT-002`. Not scanned: `demo-data.ts` (one data line, deliberate), `selfUpdate.safety.test.ts` itself (QA read it: it only holds the patterns as string literals) |

## Safety (hard rule)

- `git diff c55e4bd HEAD -- internal/agent scripts/install.sh` is empty (0 bytes). No file outside `internal/`, `cmd/riot-server/migrations/`, `web/`, `docs/`, `README.md` and `CHANGELOG.md` changed.
- Every added line of the diff (excluding the two scan files, which hold the patterns as data) was grepped for `os/exec`, `syscall`, `exec.Command`, `os.StartProcess`, `x/sys`, an `internal/agent` import, `child_process`, `spawn(`, `riot-agent `, `poweroff`, `shutdown -h`, `systemctl`, `apt-get`, `dpkg `, `dietpi-update`, `systemd-run`: no hit. The strings `reboot`, `shutdown` and `poweroff` that remain in touched Go files are action names and pre-existing data, listed in the reviewed `ex2Preexisting` table (11 entries, stale-entry check passes).
- All simulated agent behaviour (starts, registrations, completions, restarts) is in test doubles with an injected clock. No test starts a process, and QA's own runs executed only `go test`, `vitest`, `tsc` and Docker for the throwaway database. QA never executed `riot-agent`.
- The scanner's self-test (`ScannerCatchesEachForbiddenForm`) covers every pattern including `os.StartProcess`, both `x/sys` imports and the `internal/agent` import.

## Bounded growth (AD-013), enforced in code

| Bound | Enforced by |
|---|---|
| At most 1 open record per device | Unique partial index `idx_commands_self_update_open` (PG-tested) plus the advisory-locked open check |
| At most 6 created per device per rolling 24 h | `createSelfUpdate` count query (M9 killed) |
| At most 1 rate-cap event per device per 24 h | `events` existence check with `command_id=''` inside the same transaction (`AC017_PG_Concurrent...` asserts one) |
| Record bound at most 10 minutes, no keys, never queued | Shape CHECK (`PG_CheckBoundsDeadlineAndKeys`, `PG_CheckForbidsQueued`) |
| At most 3 markers per record | `command_escalations` PK (`command_id`, `target`), targets `device`, `outcome`, `stuck` |
| One outcome event and one escalation per record | Marker rows; `AC008_NoSecondEscalationAfterRestart`, `AC013`, `ResolverUnsetFailsSafe` |
| Pushes | Outcome, cap and stuck events use `rule: nil` (never notified); the escalation uses the existing publisher and ceiling |
| Tracker memory | One window per device; `Stats().Windows` and `ClosedRecent` are 0 after 6 minutes (`AC022`) |
| Logs | Hot path `Debug`; Warns go through the per-device `logThrottle` or the Tracker `warnThrottled` (constant keys); `AC017`/`AC022` assert zero Warn/Error and exactly one Info |
| Parse work | `len(raw) > 4096` rejected before any other work (`ParseBounds`, probe) |
| Events endpoint | LIMIT 500 and LIMIT 50 plus counts (`PG_ListByCommandSplit`, `AC023_CommandEventsEndpoint` with 200 flooded events) |
| Stuck record | One `paused` event at bound + 1 h (marker `stuck`), retired by the next start; the retire is PG-tested in the same transaction |

## Tests Added by QA

None committed. As instructed, the probes above were throwaway and are deleted (`git status` clean before this report). QA wrote four tests that are worth keeping and that pass, but they are not in the repository because the task asked for throwaway probes only. They are in
`C:\Users\Ryan\AppData\Local\Temp\claude\D--Repos-rIOt--claude-worktrees-nostalgic-volhard-9b5241\ab45495a-6459-452f-84bc-3d123b9a5b60\scratchpad\selfupdate_qa_test.go.proposed`
(package `expect_test`, uses the EXPECT-001 harness, gofmt clean, passes, and passes the AC-029 scan):

| Proposed test | Covers |
|---|---|
| `TestEX2_QA_FR012_CoverageStrictAtTheBound` | FR-012, NFR-002 (closes N6) |
| `TestEX2_QA_FR006_SecondStartWhileVerifyingIsAbsorbedAndNotSuppressed` | FR-006, FR-013, and pins N7 |
| `TestEX2_QA_NFR010_HostileHostnameNeverReachesMessages` | NFR-010 |
| `TestEX2_QA_X3_OldVersionHeartbeatAtBoundIsNotConfirmed` | FR-015, X-3 negative case |

## Deviations from ADD (the impl report's list, judged)

Go side:

1. `TestEX_Repo_PG_MigrationUpDownUp` migrates down to 32 instead of `Steps(-1)`. **Accepted.** `Steps(-1)` rolled back the wrong migration once 000034 existed; the assertions are unchanged and still check 000033's objects. Unavoidable.
2. A three-line `ListByCommandSplit` stub in `events/generator_test.go`. **Accepted**, forced by the new interface method; benign. Note N2 (not in the AC-029 list).
3. Two extra test files (`self_update_parity_pg_test.go`, `expect2_register_test.go`). **Accepted**; both are in the scan.
4. The rate-cap event lookup also requires `command_id=''`. **Accepted and an improvement**: a stuck-record event (same type) cannot silence the cap warning, and the once-per-24 h bound still holds (`AC017_PG_Concurrent...`).
5. The stuck event covers any failing step after bound + 1 h. **Accepted**; still once per record by marker, still no push.
6. Detector-gate wording (`ObserveDevices` at or after `graceEnd`, or `graceEnd` + 30 s; always open without grace). **Accepted**; matches AD-006, and mutation M15 shows the INFO-4 tests pin it.
7. `AC024_MockGuardParity` skips without PostgreSQL. **Accepted**; CI has a PG service (`ci.yml` sets `RIOT_TEST_PG_URL`), the mock guards are also covered without PG, and it ran here.
8. README documents the endpoint, `agent_to_version` and the new origin. **Accepted**, helpful.
9. `-race` not run on Windows. **Accepted**; the orchestrator's Linux `-race` run covers it.

Web side:

1. `agent_to_version?` optional in TypeScript. **Accepted**; the runtime check re-validates and an older server still works.
2. `labels.test.ts` count 51 to 54. **Accepted**, the same pattern EXPECT-001 used and allowed by AC-028.
3. No separate history test file (`SelfUpdateDetails.test.tsx` holds the ActivityLog cases). **Accepted**.
4. Extra helpers in `selfUpdate.ts`. **Accepted**.
5. Web scan does not ban `dpkg`/`apt`/`systemd-run`, excludes `demo-data.ts`, and scans extra files. **Accepted with note N3**: the web bundle cannot run those; the spawn and power bans that matter for Node-side tests are in place.

## Deviations from FRD

None found. The ADD's own FRD deviations (X-1 to X-20) were checked against the code: X-1, X-3, X-7 and X-19 each have passing tests; X-20 (re-registration refreshes `last_heartbeat`) is implemented at `handlers.go` in the existing-device path only and is covered by `Handler_RegistrationMovesOpenRecordAndRefreshesHeartbeat`.

## Notes (non-blocking, for the follow-up list)

- **N1.** `internal/extraupd` `TestDetect_EU_AC009_BackToBackCallsNeverSeeInFlight` failed once in the full parallel `go test ./...` run (call 1680 of 2000 returned `detection_failed`) and passed 5 of 5 isolated runs. The package is untouched by this story (only `internal/models` is in its dependency graph, and nothing the detector uses changed). The test's own comment records an earlier CI hit of the same race. Pre-existing; suggest its own ticket.
- **N2.** The AC-029 Go scan does not include `internal/server/events/generator_test.go`. The change is a three-line stub, so no risk; adding the file to the `must` list or the scanned directories would close the gap.
- **N3.** The web scan excludes `demo-data.ts` (one data line) and itself, and does not ban `apt`, `dpkg` or `systemd-run` literals (the web impl report explains why). Reviewed by hand: clean.
- **N4.** The unique-violation fallback in `CreateSelfUpdate` (`pgErr.Code == "23505"` re-read, ADD section 9) has no test: a mutation that disables it survives. Under the per-device advisory lock the path is unreachable in practice, it exists for a writer that skips the lock, and its failure mode is an unlinked start event with no second record. A deterministic test needs a hook to interleave a committed insert and was not written.
- **N5.** The defensive sweeper skip (`sweeper.go:99`) is redundant with the `ListOverdue` exclusion and the server-edge origin guard; a mutation removing only the Go skip survives. Fine as defence in depth.
- **N6.** Offline coverage ending exactly at the bound is pinned by one test only incidentally (`ResolverUnsetFailsSafe`). The proposed `QA_FR012` test makes it direct.
- **N7.** Behaviour to be aware of: a second self-update that starts inside the 10 minutes of the first record (even after the device came back and the record is `verifying`) is absorbed into that record and is not suppressed, so back-to-back releases alert on the second restart. This follows FR-006 and fails toward alerting; it is documented neither in the README residuals nor the ADD residuals. Consider one README sentence.
- **N8.** There is no test that `server.Start` wires `SelfUpdates` and `SetSelfUpdateResolver` (code reviewed at `server.go:257-261`, before `Load`, and `router.go:99` passes the same `s.Commands` to the handlers). If a future refactor drops either line, every record would hit the C-4 stuck path an hour after its bound, and the unresolved-at-bound outcome events would never be written. A one-line wiring assertion would be cheap insurance.
- **N9.** PG tests ran here on PostgreSQL 17; CI uses PostgreSQL 16. Nothing used is version-specific (`hashtextextended`, partial indexes, CHECK with `cardinality`), but QA did not run 16.
- **N10.** X-20 changes behaviour for every device (a re-registration refreshes `last_heartbeat`). It is stated in the impl report, README and CHANGELOG, and Ryan reviews it in the PR as an open point.
- **N11.** Rollback R-5: the down migration deletes all self-update history and leaves dangling `command_id` on events (documented in the migration, README and impl report).
- **N12.** Attribution line: the task asked for `Co-Authored-By: Claude Opus 5.5` on the QA commit; this agent runs as Sonnet 5.5 and the harness attribution says Sonnet 5.5, so the commit carries the Sonnet 5.5 line.

## Verdict Rationale

**PASS WITH NOTES.** All 30 ACs are COVERED by tests that fail when the rule is broken (verified by mutation for the load-bearing ones), all 112 `TestEX2_` tests and the full suite pass (the one full-suite failure is a pre-existing, unrelated flake that passes in isolation), every security C-6 gate item passes, the safety rule holds (no agent or `install.sh` change, no execution in any test), bounded growth is enforced in code and not only in prose, and no implementation deviates from the FRD. The notes above are quality gaps and awareness items, none of which blocks shipping.

## Action Required

None. Optional follow-ups for senior_dev: adopt the four proposed tests (N6, N7), add `events/generator_test.go` to the AC-029 Go scan (N2), add a `server.Start` wiring assertion (N8), mention N7 in the README residuals, and file N1 separately.
