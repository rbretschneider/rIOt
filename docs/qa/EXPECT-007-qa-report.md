# QA Report

**Story ID:** EXPECT-007
**QA Engineer:** QA Agent (Sonnet 5.5)
**Date:** 2026-10-27
**Branch:** `claude/expect-007-473e29` (implementation `bee8e23..69ee43b`, 12 commits, plus the QA test commit)
**Verdict:** PASS WITH NOTES

---

## Test Run Summary

Environment: Windows host (`GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`), a throwaway `postgres:16` container (`riot-ex7-qa-pg`, port 55432) for the PG-gated tests, and a throwaway `golang:1.24` container (`riot-ex7-qa-race`, source mounted read-only, `host.docker.internal` to the PG container, its own database) for the race detector. Both containers were started by QA and are gone. No other container was touched.

| Run | Command | Result |
|---|---|---|
| Windows, full | `go build ./... && go vet ./... && go test -count=1 ./...` with `RIOT_TEST_PG_URL` set | build and vet clean; 31 packages `ok`, exit 0 (second full run; see flake note) |
| Windows, story tests | `go test -count=1 ./... -run TestEX7_ -v` with PG | 97 top-level `TestEX7_` PASS (92 from the implementer + 5 added by QA), 117 subtests PASS, 0 FAIL, **0 SKIP** |
| PG-gated | the 6 tests in `internal/server/db/self_update_attempt_pg_test.go` | all 6 ran and passed against PostgreSQL 16, including `TestEX7_PG_ConcurrentSameAttemptOneRecord` (50 concurrent starts, one record). Not skipped. |
| Linux + race | `go test -race -count=1 ./internal/agent/... ./internal/server/... ./internal/models/...` in `golang:1.24` with PG | 24 packages `ok`, exit 0, no `DATA RACE`. This run also exercised the Linux-only agent test files. |
| Web | `cd web && npx tsc --noEmit && npm run test:run` | `tsc` exit 0; vitest 127 files, 2304 tests passed (same count as the implementer's run) |

- Coverage percentage: not collected (no coverage gate in the project).
- **Flaky test found (one occurrence):** in the first full Windows run, `internal/server/db` `TestDUX_SEC022_RenumberedPart001NoOpOnConvertedDB` failed once (`cannot scan NULL into *string` from `string_agg` over `partition_conversion`). It is a migration-fixture test from DIETPI/partition work; the story adds no migration and does not touch that code. It passed twice when run alone, passed in the whole `db` package on the second full Windows run, and passed in the full `db` package under Linux with `-race`. Treated as an environment flake, not a story defect. The three known pre-existing flakes (extraupd back-to-back, quota stdout sample, TestCL_AC100 TempDir) did not occur.
- A rebase onto current `main` is still needed before merge: the branch is based on `e120031`; `main` is at `7988ad7` (PRs #49, #50, #51 landed since).

## Safety (hard rule) verification

- No EX7 file is in `ex2Preexisting` or `ex7Preexisting`. QA checked independently: the exception table entries (all `internal/models/expect.go`, `observer_test`, `sweeper_test`, `activity_test`, `host_test`, handler fixtures, `server.go`) are pre-existing files that are not in the story diff, and `safety_ex2_test.go` is unchanged. `ex7Preexisting` is a copy of the EX2 table. The Z test asserts this in code (`ex7IsStoryFile`).
- The scan lists printed with `-v` (server: 154 files) include every new or modified Go file of the server diff, and the QA test files. The agent scan covers `power_wait_step_test.go`, `safety_ex7_test.go`, `update_events_test.go` and the QA file.
- C-1: `TestEX7_AC037_UpdatePipelineRefusesRealProcessInTest`, `..._HandoffWatcherRefusesRealProcessInTest` and `..._RealRunnerFallbacksGuarded` pass. The AST pin forbids `if X == nil { X = execRunner }`, requires `if inTestBinary()` before `execRunner` in `processRunner`, and lists every other use. `grep execRunner` confirms the only non-doctor use is `processRunner`. Honest limit: two os_update helpers (`runCommand`, `runUpgradeCommand`) still fall back to a real `exec.Command` behind `a.runner != nil`; the story records them as follow-on F-1 (ADD AD-015, R-5) and no EX7 test reaches them. I did not mutate the guard itself, since removing it could run a real process.
- Nothing QA wrote or ran invokes reboot, shutdown, poweroff, apt, dpkg, dietpi-update, systemctl, systemd-run, or an installed `riot-agent`. QA tests use the implementer's fakes (fake wire, fake clock, fake runner, fake notifier); the agent and server scans pass over them.

## AC Coverage Audit

"Covered" means a test exists that fails if the AC were violated. Beyond reading, QA checked this with 23 mutation probes in a scratch copy of the tree (outside the repo, never committed): 19 were killed by the existing tests. The 4 survivors are analysed below; 2 are equivalent mutants (M4, M15) and 2 exposed real gaps (M11, M16), which QA closed with new tests.

| AC | Status | Tests | Gap / note |
|---|---|---|---|
| AC-001 | COVERED | AW `AC001_RebootWaitReportsStepOnce`; H7 `AC001_WaitingLabelWhileOnline` | |
| AC-002 | COVERED | AW `AC002_PowerRunsRightAfterFinalProbe`, `Q2_EnterStepDoesNotDelayGate`; H7 `AC002_StepChangeClearsSuffix` | Mutation M1 (`leave()` before the power runner) and M2 (synchronous `enter`) both killed. Final probe -> runner adjacency and "marker step is not `power_action` at the runner call" asserted on the fake clock. |
| AC-003 | COVERED | AW `AC003_TimedOutWaitFailsInWaitStep`; H7 `AC003_TimeoutClearsSuffixNoNotification`; WL | Reboot only at the agent level; the shutdown skip is covered by the shared `powerSkippedResult`. |
| AC-004 | COVERED | AW `AC004_...`; H7 `AC004_...` | |
| AC-005 | COVERED | AW `AC005_FreeLockNoStep`; H7 | |
| AC-006 | COVERED | AW `AC006_RetakenLockReportsOnce...`; **QA** `AC006_QA_ReporterEnterIsIdempotent` | Gate tests could not tell a non-idempotent reporter (mutation M16 survived). QA test added and verified to kill it. |
| AC-007 | COVERED | AW `AC007_StepReportFailureChangesNothing` (400, 500, dropped, blocked); AZ `AC007_GateCallsOnlyEnterOnReporter` | Mutation "gate also calls `settle`" killed. |
| AC-008 | COVERED | H7 `AC008_OfflineHidesSuffix` | Mutation M10 (drop `dev.online`) killed. |
| AC-009 | COVERED | AW `AC009_PostPatchRebootReportsNoWaitStep`; AZ `AC009_OnlyStandaloneHandlersAttachWaitReporter`; **QA** `AC009_QA_NilReporterIsInert` | |
| AC-010 | COVERED | H7 `AC010_OldAgentPlainRebooting` | |
| AC-011 | COVERED | AU, S, H7, R (PG) | |
| AC-012 | COVERED | AU `AC012_AttemptIDUniqueAndShaped`, `AC012_RestartedInstanceReusesHandoffAttempt` | Mutation M13 (substitute running version) killed. |
| AC-013 | COVERED | AU `AC013_OldHandoffYieldsNoReference` (both paths); S | |
| AC-014 | COVERED | AU `AC014_MessagesByteForByte` | Mutation M14 (reword the start message) killed. Also `git diff` shows the `fmt.Sprintf` texts unchanged. |
| AC-015 | COVERED | S `AC015_...`; H7 | Mutation M8 (message beats structured) killed. |
| AC-016 | COVERED | M, S | |
| AC-017 | COVERED | S, H7 | |
| AC-018 | COVERED | S (zero observer calls, zero broadcasts); R (PG, replay after close; 50 concurrent starts); H7 | Mutation M4 survived: equivalent, because the repo returns nil `Command`/`Retired` for `attempt_match` (asserted by the PG test), so the service-level skip is defence in depth. |
| AC-019 | COVERED | S `AC019_UnknownAttemptOnlyNoAttemptRecord` | Mutation M18 killed. |
| AC-020 | COVERED | H7 `AC020_FleetUpdateTiedToCommand` | |
| AC-021 | COVERED | S `AC021_...` | |
| AC-022 | COVERED | S `AC022_...` | |
| AC-023 | COVERED | M, S (10 cases), HT; **QA** `AC023_QA_WrongTypedReferenceFieldsNeverCreateOrLink` | Includes vermont's real dispatched command ID and the `installer` synthetic origin. Mutations M5/M5b (not device-scoped), M9/M9b (nil `finished_at`), M20 (no 10-minute bound), M21 (deny-list instead of allow-list) all killed. Boundary at exactly 10m00s tested (inclusive) and 10m01s (invalid). |
| AC-024 | COVERED | M, S | Mutation M12 (both references -> attempt) killed. |
| AC-025 | COVERED | H7 `AC025_PR46ReturnKept` | `window.go` untouched (diff). |
| AC-026 | COVERED | H7 `AC026_OldAgentTimelinesUnchanged` | |
| AC-027 | COVERED | M, AU | Decodes into a frozen copy of the e120031 `AgentEvent` and runs the unchanged parser. A real old server was not run (accepted; same as impl report note 5). |
| AC-028 | COVERED | S, H7 | |
| AC-029 | COVERED | S; HT `AC029_CrossDeviceRefsIgnoredHTTP` (hawaii's real command ID and real attempt ID sent by vermont: identical status and body, nothing linked, nothing announced); **QA** `AC029_QA_SameAttemptIDOnTwoDevicesIsIndependent` | Added: the same attempt ID on two devices creates two records and each device's repeat is deduplicated independently. |
| AC-030 | COVERED | AU `AC030_...` | |
| AC-031 | COVERED | AU `AC031_BudgetBoundedOneWarn`; **QA** `AC031_QA_SlowAttemptsNeverStartPastBudget` | Mutation M11 (remove the explicit 90 s check) survived because the fixed schedule alone keeps the default policy inside 90 s. QA test makes each attempt take 60 s of fake time and requires only 2 attempts plus one Warn counting 2. Verified to kill M11. |
| AC-032 | COVERED | AU | Mutation M6 (retry 4xx) killed; M17 (no retry on 5xx) killed. |
| AC-033 | COVERED | AU `AC033_CommandPathNeverWaits`, `AC033_BusySlotAttemptOffCallerGoroutine` | Mutation M7 (ignore the agent context) killed. |
| AC-034 | COVERED | HT, S | Mutation M3 (dedupe any link) killed. `HasCommandEvent` is asserted never called with an empty command ID. |
| AC-035 | COVERED | HT, H7, AU, AW | |
| AC-036 | COVERED | AZ `AC036_NoNewSudoInvocation`; QA diff check | `git diff --name-only bee8e23~1..HEAD` contains no `scripts/`, `cmd/`, sudoers, unit, `config.go`, `doctor.go` or `collectors/` path. |
| AC-037 | COVERED | AZ x5, Z x2, WZ | See Safety section. |
| AC-038 | COVERED | HT `AC038_*`; whole suite green | Only one existing test line changed (third argument of `SelfUpdateParams`, no assertion change). |
| AC-039 | COVERED | QA diff check | No file under any `migrations` directory is in the diff; `IsValidOrigin` unchanged; `window.go`, `tracker.go`, `finish.go`, `selfupdate.go` untouched. |
| AC-040 | COVERED | D `AC040_ReadmeAndChangelog` | The test builds the package-lock word and the helper name from fragments (C-5). |

No AC is MISSING. No AC was PARTIAL after the QA additions.

## Security conditions

| Condition | Verified | How |
|---|---|---|
| C-1 | Yes | Guard tests pass; AST pin and self-tests pass; one `execRunner` fallback left, behind `inTestBinary()`. |
| C-2 | Yes, with the merge-second item below | `ValidAgentUpdateCommandRef` uses the `IsValidOrigin` allow-list. Mutation M21 (a deny-list on `agent_self_update` only) is killed by the synthetic `installer` origin case. |
| C-3 | Yes | (a) `attempt_match` creates no row, no observer call, no `command_update` (S + PG assert nil `Command`/`Retired`); (b) `Dedupe` requires a reference-derived link with a non-empty ID, and capped and fail-open starts are stored (HT); (c) a terminal row with nil `finished_at` is invalid (S, M); (d) the gate calls only `enter` (AZ AST rule; mutation killed). |
| C-4 | Yes | Agent-context end stops the retry with no Warn (AU, M7 killed); busy-slot single attempt is off the caller goroutine and the goroutine bound is 6 (AU); Warn carries only type and attempt count (AU). |
| C-5 | Yes | See Safety section for the exception tables and D; `TestEX7_AC002_*` shows the final probe is directly followed by the power runner and the leaving step has not been written at that call; AC-023 and AC-029 use another device's real command ID and attempt ID with identical 200 bodies; `TestEX7_PG_ConcurrentSameAttemptOneRecord` ran and passed against PostgreSQL 16 (not skipped); the AD-008 sentence is corrected (ADD line 327). |

## Probe results (the hardest points Ryan asked about)

- **C-2 installer origin.** The command reference never links a non-allowed origin: the test's `installer` row is not linked by reference (`command_attr` is not `agent`, `Dedupe` false). EXPECT-002's own timing rule in `CreateSelfUpdate` step 1 (`origin <> 'agent_self_update'`) is unchanged by this story and still attributes a start by timing to such a row. That is what the ADD intends (ADD lines 22, 255, 284, 292: C-2 governs the new reference path; EXPECT-008 AD-017 narrows the timing predicate). Recorded as the **merge-second re-check**: whichever of EXPECT-007 and EXPECT-008 merges second must re-run the non-allowed-origin case of `TestEX7_AC023_InvalidCommandRefsFallBack` against the real `installer` origin and name SEC-002 in its merge note.
- **FR-004 ordering.** `handleReboot`/`handleShutdown` call `rep.leave()` after `execReboot`/`execShutdown` returns. The gate calls only `enter()` (a goroutine start) in the first-held block, before the timeout check and sleep; the permit mint and final probe order are unchanged. Slow durable write, full notifier queue, and blocked/400/500/dropped progress channels each give identical probe, runner, result and fake-clock traces. Residual R-3 (result durability after a very fast reboot) is measured only on the fake clock (gap 0); nothing proves it on real hardware.
- **Retry bounds.** At most 5 attempts, none starting later than 90 s after the first (now also pinned for slow attempts), per-attempt 10 s context, 4xx stops at once (400 verified, 429 and 5xx retry), agent-context cancel stops with no Warn, 6 sender goroutines (3 retriers + 3 single-shot), command path never blocks (blocked transport, `handleTriggerUpdate` returns). The real per-attempt timeout is a real `context.WithTimeout`; the fake sinks return at once, so real-timeout behaviour is reasoned about, not exercised. `Retry-After` is ignored by design.
- **Dedupe scope.** Only validated references with a non-empty linked command ID are deduplicated; timing-linked and unlinked events are never dropped. Mutation M3 killed.
- **`attempt_match` replay.** Creates no record, counts nothing, extends and reopens no window, retires nothing, broadcasts nothing (PG and mock layers).
- **Cross-device.** Another device's real command ID and real attempt ID, from a different key or a mismatched `{id}`, give the same 200 and the same body as an unknown value and link nothing.
- **Message text.** `fmt.Sprintf` texts for started, failed and completed events are unchanged in the diff; the four signed texts are untouched; AC-014 compares byte for byte.
- **PR #46.** `window.go` and the tracker are untouched; `AC025_PR46ReturnKept` passes.
- **Bounded growth.** Matches ADD AD-014: attempt ID <= 64 bytes per record, <= 6 records per device per rolling day (cap untouched), reference-linked repeats stored zero extra times, waiting steps <= 2 per command inside the 64-step cap, hand-off file one file <= 512 bytes asserted, no migration, no table, no event type, no origin.

## Judgement of the seven implementer deviations

1. **`SelfUpdateParams` third argument touches one existing test line.** Accepted. Signature change only; no assertion changed.
2. **`activity.go` has `dev.online && !terminal && step == wait`.** Accepted. The extra terminal condition is required by FR-009 and BR-006 (a terminal command must not claim a wait) and is pinned by `AC002_StepChangeClearsSuffix/the_result`. It remains the only source change in `expect`.
3. **Three agent seams (`updateApply`, `restartHook`, `bindHandoff` split, `a.sleep` for the 1 s restart delay).** Accepted. Nil in production; `a.sleep` is a timer when `a.pause` is nil, so the delay is unchanged. Needed to test AC-014 and AC-033 without `os.Exit`.
4. **`deliverFirstSync` with a busy slot makes one synchronous attempt, no goroutine.** Accepted. It is the self-update path (not the command path C-4 b protects) and is the pre-story behaviour; the goroutine bound of 6 still holds.
5. **R tests in package `db_test`.** Accepted. Same PG gate (`RIOT_TEST_PG_URL`, database name contains "test").
6. **README/CHANGELOG written by the engineer, release given as "first release after v2.85.0".** Accepted with a note: the technical writer must put the real version number and own the wording before the release is tagged.
7. **Z scan scope excludes `internal/agent` (AZ covers it).** Accepted. Both scans pass and QA verified the union covers every changed file.

## Test Quality Findings

- Tests assert observable behaviour (stored events, links, broadcasts, step traces, fake-clock times, request bodies), not internals; mocks are checked against the real SQL by the PG parity test (`MockParityAttempt`).
- No test sleeps in real time. `time.After` is used only as a failure timeout (5 s), and the AZ scan forbids `time.Sleep`.
- Two surviving mutants are equivalent and need no action: M4 (service-level broadcast guard; the repo already returns nil rows) and M15 (explicit `res.Step = wait` on skip; `cr.currentStep()` yields the same value).
- Two survivors were real gaps, closed by the QA tests below: M11 (the explicit 90 s start bound was never reached by the fixed schedule) and M16 (the gate calls `enter` once per call, so only a direct test can tell a non-idempotent reporter).

## Adversarial Findings

1. **Wrong-typed JSON in a reference field returns 400, not 200 (Low, note).** `"attempt_id": 123`, `{"a":1}`, `"command_id": [..]` and `"from_version": 2.78` fail the decoder and get HTTP 400, while a well-typed invalid value (NUL, unicode lookalike hyphen, SQL payload, 300 bytes) gets 200 and is processed as no reference. ADD Section 9 says a malformed reference gives 200. Impact: none beyond the sender's own request; the event is not stored, nothing is linked, no record is created, so it fails toward alerting (NFR-008 is respected), and the field `command_id` already behaved this way before the story. The new agent only sends strings. Documented; the QA test `AC023_QA_...` asserts "never 5xx, never a reference link, never a record with a malformed attempt ID", and does not pin the 400.
2. **Same attempt ID on two devices** (a cloned disk image): independent. Test added, passes.
3. **SQL, NUL, unicode and overlong reference values:** rejected by `ValidRefToken` before any query; all answered 200 as no reference (test added).
4. **Replay of an old attempt ID, double delivery, concurrent starts:** no second record, no extended window (PG, 50 concurrent starts).
5. **Command reference to another device's, a non-agent_update, undispatched, stale or server-created-origin row:** all fall back to EXPECT-002 behaviour with identical responses.
6. **Residual, accepted by the ADD:** de-duplication holds for one server process (R-1); a timing-linked event with a lost response can be stored up to 5 times (R-2); result durability on a very fast reboot (R-3).

## Tests Added by QA

Committed separately as `d6ee92f` (`[EXPECT-007] test: QA coverage for the retry budget, reporter idempotence and hostile reference fields`). Each carries "Added by QA Engineer" and the AC it covers.

| File | Test | Covers |
|---|---|---|
| `internal/agent/update_events_qa_test.go` | `TestEX7_AC031_QA_SlowAttemptsNeverStartPastBudget` | AC-031, FR-032, FR-033, FR-036 |
| `internal/agent/update_events_qa_test.go` | `TestEX7_AC006_QA_ReporterEnterIsIdempotent` | AC-006, FR-003 |
| `internal/agent/update_events_qa_test.go` | `TestEX7_AC009_QA_NilReporterIsInert` | AC-009, FR-007 |
| `internal/server/handlers/agent_event_ref_qa_test.go` | `TestEX7_AC023_QA_WrongTypedReferenceFieldsNeverCreateOrLink` | AC-023, NFR-007, NFR-008 |
| `internal/server/handlers/agent_event_ref_qa_test.go` | `TestEX7_AC029_QA_SameAttemptIDOnTwoDevicesIsIndependent` | AC-029, FR-020, FR-035, BR-003 |

All five pass on Windows and under Linux `-race`. The first two were shown to fail against mutations M11 and M16.

## Deviations from ADD

None that change behaviour beyond the seven judged above. One wording gap: ADD Section 9 says a malformed reference is answered 200; a wrong JSON type is answered 400 (Adversarial finding 1).

## Deviations from FRD

None found. FR-040's "which agent release reports it" is satisfied with the placeholder "the first release after v2.85.0" (see note 3 below).

## Verdict Rationale

**PASS WITH NOTES.** All 40 ACs are covered by tests that fail when the AC is violated (23 mutation probes; every non-equivalent survivor is now closed). Every test is green on Windows (including all 6 PG-gated tests), under Linux with the race detector, and in the web suite. No implementation deviates from the FRD, no migration or installer/sudoers/unit change is present, C-1 through C-5 are verified, and the safety rules hold.

### Notes (non-blocking, address before or at merge)

1. **Merge-second re-check (C-2).** Whichever of EXPECT-007 and EXPECT-008 merges second re-runs the non-allowed-origin case of `TestEX7_AC023_InvalidCommandRefsFallBack` against the real `installer` origin, and the merge note names SEC-002. Until then the test uses a synthetic origin.
2. **Rebase.** The branch is based on `e120031`; `main` has moved to `7988ad7`. Rebase and re-run the full suite; expect textual overlap with EXPECT-008 in `internal/server/expect/` and in `README.md`.
3. **Release number.** README and CHANGELOG say "the first release after v2.85.0". The technical writer should insert the real version and own the final wording before tagging (FR-040).
4. **Wrong-typed reference fields answer 400** (Adversarial finding 1). Optional hardening: decode the three reference fields leniently so every malformed reference gets 200, as the ADD table says. Not required for safety.
5. **Unguarded os_update exec fallbacks** (`runCommand`, `runUpgradeCommand`): pre-existing, tracked as follow-on F-1; no EX7 test reaches them.
6. **Real hardware.** R-3 (result durability after a very fast reboot) and the real per-attempt HTTP timeout were verified only on fake clocks. Ryan's planned watched first update on one device covers this.
7. **Scratch copy left behind.** QA's mutation probes used a copy of the tree at `D:\rIOt_qa_mut` (outside any repository, not a worktree). The delete was blocked by a safety check, so it is left for Ryan to remove.

## Action Required

None (verdict is not FAIL).
