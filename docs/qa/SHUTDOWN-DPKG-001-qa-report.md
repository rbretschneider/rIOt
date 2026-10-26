# QA Report
**Story ID:** SHUTDOWN-DPKG-001
**QA Engineer:** QA Agent
**Date:** 2026-10-25
**Branch:** `feat/shutdown-dpkg-001` (base `fa8f924`; nothing pushed, tagged or merged)
**Verdict:** PASS WITH NOTES

The code, the tests and every security condition that can be checked in code are green. Two things keep this from a plain PASS: the documentation gate (AC-026, AC-033, and the README parts of C-2 and C-5) is deferred to the technical writer and is still open, and there are a few non-blocking findings (Section "Findings"). The story is not releasable until the technical writer has run.

---

## Test Run Summary

Windows, `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`, no `-race` (needs CGO).

| Run | Result |
|---|---|
| `go test ./... -count=1 -v` (after QA tests were added) | 29 packages ok, 0 failed. 2195 top-level tests pass, 2121 subtests pass, 0 fail, 59 skipped (pre-existing platform skips plus the Windows SIGTERM subtest) |
| Same run before QA tests | 2185 top-level pass, 2109 subtests pass, 0 fail, 59 skipped. The QA tests add 10 tests and 12 subtests |
| `go vet ./...` | clean |
| `GOOS=linux go vet ./...` | clean (compiles the Linux-only files and every test file for Linux) |
| `go build ./cmd/riot-agent` | linux and windows both ok |
| `cd web && npm run test:run` | 95 files, 1707 tests passed (unchanged from baseline) |
| Story, reboot and policy subsets, `-count=5` (`SD_`, `ExecShutdown`, `PowerGuard`, `RD_`, `QA_`, `PF_C8`, `CL_`) | ok, no flake |
| Shutdown, gate and call-site subset, `-count=10 -cpu 1,4` | ok, no flake |
| QA tests alone, `-count=20 -cpu 1,4` | ok, no flake |

- **Coverage percentage:** not collected (no coverage tooling in the DoD).
- **Flaky tests in the delivered suite:** none.
- **Flaky test found in my own first draft:** `TestExecShutdown_QA_AC030_NoGoroutineOrFlagLeakAcrossEveryExit` failed under `-count=5`. This exposed finding F-3 below. It is fixed in the test, and the production behaviour is recorded as a non-blocking note.
- **Not run (and why):** `-race` and the Linux SIGTERM subtest cannot run on this Windows machine. The real production branch of `execShutdown` and `execReboot` is never executed, by design (C-1). Nothing reached a real `systemctl poweroff|reboot`, `shutdown /s|/r`, halt or poweroff. No host was contacted and no installed `riot-agent` was executed.

## Security Condition Gate (C-6)

| Condition | Result | Evidence |
|---|---|---|
| C-1 test-binary power guard | PASS | `TestPowerGuard_SD_C1_*` (state, AST, self-test). Mutations: guard neutered in `execShutdown` fails the AST test; guard removed from `execReboot` fails the AST test. No test calls the real branch |
| C-2 truthful result, no privilege, no argv change | PASS for code. README part deferred | `TestShutdown_SD_AC027` to `AC032`, `TestPoweroff*`. No diff under `scripts/`. SHUTDOWN-SUDO-001 is recorded in the implementation report |
| C-3 permit bound to the action and to the allow flag | PASS | `TestPowerCallSites_SD_C3_*`, `TestPowerClearance_SD_C3_AllowFlagGatesTheMintWhateverTheGuards`, `TestExecShutdown_SD_C3_RebootPermitIsRefusedAndNotConsumed`, `TestExecReboot_SD_C3_ShutdownPermitIsRefusedAndNotConsumed` (both cross-action refusals, no consumption) |
| C-4 only power paths | PASS (with note F-2) | `TestPowerPaths_SD_C4_*`. Mutations below |
| C-5 docs and ADD corrections | ADD part PASS (the "sends no event" bullet is corrected, R-7 says 25 minutes). README part deferred | |
| C-6 QA gate list (ADD section 13) | PASS | All 32 named gate tests pass. `TestRebootAuth_PF_C8_ExecRebootHasExactlyThreeGuardedCallSites` passes unchanged. All 66 test names listed in ADD section 8 exist |

DoD diff and greps, all clean:
- `git diff --stat fa8f924..HEAD` shows nothing under `scripts/`, `web/`, `cmd/`, `go.mod`, `go.sum`, `internal/agent/dpkglock*.go`, any migration, or `internal/server` non-test code. The only server files are `service_test.go` and `sweeper_test.go`.
- `powerPermit{` appears only in `mintPowerPermit`. `rebootPermit` matches nothing.
- The quoted string `"poweroff"` appears only in `execShutdown`. `testing.Testing` appears only in `powerguard.go`.
- No `time.Now()` or `time.Sleep` in `dpkglock*.go`, `reboot_clearance.go` or `poweroff.go`.

## AC Coverage Audit

"Covered" means a mutation of the code would fail the cited test. Test file for AC rows is `internal/agent/shutdown_clearance_test.go` unless stated.

| AC | Status | Tests | Gap / note |
|---|---|---|---|
| AC-001 | COVERED | `SD_AC001_FreeLockPowersOffWithNoWait`, `ClearanceSD_AC001_FreeRunsGuardsThenFinalProbe` | probe, guards, probe order and zero pauses asserted |
| AC-002 | COVERED | `SD_AC002_HeldLockNeverPowersOffWithinBound` | asserts inside the probe script on every poll |
| AC-003 | COVERED | `SD_AC003_ReleaseAfter3mPowersOffWithin10s`, `QA_AC004_ExactBoundary` | |
| AC-004 | COVERED | `SD_AC004_TimeoutSkipsAndNeverForces`, `_NoSecondWaitAfterSkip`; QA `QA_AC004_ExactBoundary`, `QA_AC004_SkippedAttemptsLeaveNoLivePermit` | QA added the exact-bound pair (free at 10m proceeds, held until 10m+1s skips at 10m) |
| AC-005 | COVERED | `SD_AC005_...NamesHolder` | exact text, not `timeout`, no `initiated` |
| AC-006 | COVERED | `SD_AC006_...HolderNotVisible` | |
| AC-007 | COVERED | `SD_AC007_UnknownSkips...`, `_HeldThenAbsentNeverPowersOff`, `_ZeroStatusSkipsAsUnknown` | one probe, zero pauses |
| AC-008 | COVERED | `SD_AC008_AbsentPowersOffAsToday`, `_RunnerGetsUnchangedArgv`, `ExecShutdown_SD_AC008_WindowsArgvUnchanged`; mutation `/s` to `/p` fails | Windows behaviour is covered through the Absent probe, not a Windows run |
| AC-009 | COVERED | `SD_AC009_...WithoutLockCheck`; mutation (pre-check removed) fails | zero probes, zero pauses, zero launches |
| AC-010 | COVERED | `SD_AC010_...`, `ClearanceSD_AC010_GuardOrder...` | unit-test-only state, documented as such |
| AC-011 | COVERED | `SD_AC011_...` | reason `conflict` asserted |
| AC-012 | COVERED | `SD_AC012_FinalCheckRetaken...`, `ClearanceSD_AC012_GuardsRunAgain...` | one deadline, one wait-started line |
| AC-013 | COVERED | `SD_AC013_AgentStopEndsWaitWithin5s`, `_ShutdownResultReasonShutdown`; QA `QA_AC013_AlreadyShuttingDownNeverPowersOff`; reboot `QA_AC015_RealTimerShutdownEndsWaitWithin5s` (same gate code) | QA added the already-stopping case |
| AC-014 | COVERED | `SD_AC014_NoPoweroffAfterSkippedResult`, `_CommandBudgetExhaustedIsATruthfulSkip` | see F-4 (inherited, budget is checked on the wait path only) |
| AC-015 | COVERED | `SD_AC015_LaunchErrorIsFailedResult`; `poweroff_test.go` real `Start` error | |
| AC-016 | COVERED | `SD_AC016_NineMinutesFiftyWait...`; `TestSweep_SD_AC016_*` (server); QA `QA_AC016_AgentCommandContextIsTheShutdownBudget` | QA added the agent side: a real shutdown command context lasts 12m. Mutation (shutdown `AgentHardMax` back to 1m) is caught |
| AC-017 | COVERED | `TestCommandPolicy_SD_AC017_*`, `TestCL_AC029_*`, `TestCL_DeadlineInvariant`, `service_test.go` | mutations (agent 1m, server 10m) fail |
| AC-018 | COVERED after QA | `SD_AC018_EachGateSkipsAtItsOwnBound`, `ClearanceSD_AC018_*`, `TestCommandPolicy_SD_AC018_*` (AST); QA `TestPowerGate_QA_AC018_GateBoundsAreWiredToTheirOwnConstants` | Was PARTIAL, see F-1. A shutdown gate wired to the reboot bound survived the delivered suite. The QA test closes it |
| AC-019 | COVERED | reboot tests unchanged except the AD-008 renames; `RebootGate_SD_AC019_RebootSpecUnchanged` | see "Reboot invariance" |
| AC-020 | COVERED | `ShutdownCallSites_SD_AC020_*`, `PowerCallSites_SD_C3_*`, `PowerPaths_SD_C4_*`, `PF_C8_*` | see mutations |
| AC-021 | COVERED | `SD_AC021_SecondShutdownOrRebootDuringWaitIsHostBusy` | no extra probe, both refused as `conflict` |
| AC-022 | COVERED | `SD_AC022_RestartDoesNotRerunShutdown` | |
| AC-023 | COVERED | `ClearanceSD_AC023_FullWaitBoundedOutput`, `SD_AC023_FullWaitOneResultOnlyTheFailedCommandEvent` | at most 303 probes, 2 Info lines, no files, 1 result, 1 event, no telemetry |
| AC-024 | COVERED | `SD_AC024_RunsOnlyTheExistingPoweroffArgv`, AST argv tests, empty `scripts/` diff | |
| AC-025 | COVERED | empty `web/` diff, `npm run test:run` green | |
| AC-026 | DEFERRED TO TECHNICAL WRITER | none | README L2660 and L2761 still say "Shutdown is not gated". Expected |
| AC-027 | COVERED | `SD_AC027_ExitZeroReportsInitiated` | no wait and 3m wait |
| AC-028 | COVERED | `SD_AC028_NonZeroExitReportsFailed`, `ClassifyPoweroff_SD_AC028_*`; QA `QA_AC028_RefusalIsFinalForTheAttemptAndFreesTheAgent` | mutation (non-zero counted as initiated) fails four tests |
| AC-029 | COVERED | `SanitizePowerErr_SD_AC029_*`, `SD_AC029_LongErrorIsBoundedInTheResult`; QA `SanitizePowerErr_QA_AC029_UnicodeControlAndFormatCharacters`, `Shutdown_QA_AC029_ControlOnlyOutputKeepsTheForm` | QA added bidi override, zero-width, line separators, C1 controls, DEL, OSC links. Mutations (limit 300, no sanitizer) fail |
| AC-030 | COVERED | `SD_AC030_StillRunningAtBound...`, `_SecondLaunchWhileChildRunsIsRefused`, `_LateNonZeroExitLogsOneWarn`, `ExecShutdown_SD_AC030_ProductionBranchStartsThenWaits`; QA `ExecShutdown_QA_AC030_NoGoroutineOrFlagLeakAcrossEveryExit`, `_HungChildHoldsOneGoroutineThenReleasesIt` | The real `Wait` is checked by AST only (by design). Mutations (no `Wait`, no flag clear, no `abandoned` close, no one-child guard, timer 0) all fail |
| AC-031 | COVERED | `TestCommandPolicy_SD_AC031_*` | |
| AC-032 | COVERED | `PoweroffExit_SD_AC032_SeamAndProductionErrorsMapAlike` (real helper-process errors), `SD_AC032_SignalTerminationIsInitiated`, `ExecReboot_SD_AC032_RebootLaunchUnchanged` | SIGTERM row skipped on Windows (see F-5) |
| AC-033 | DEFERRED TO TECHNICAL WRITER | none | README text not yet written. Expected |

No AC is MISSING. No AC outside the two documentation ACs is PARTIAL after the QA tests.

## Mutation Results

Each mutation was applied to one file, the package tests were run, and the file was restored with `git checkout`. The tree was clean afterwards.

| Mutation | Expected | Result |
|---|---|---|
| Stray `"systemctl", "poweroff"` literal in `powerguard.go` | C-4 fails | FAIL (caught) |
| New file in `internal/agent/collectors` with `exec.Command("shutdown","-s","-t","0")` | C-4 fails | caught |
| New file in `cmd/riot-agent` with `exec.Command("/sbin/reboot")` | C-4 fails | caught |
| New file with `a.runner(ctx,"sudo","systemctl","halt")` | C-4 fails | caught |
| New file with `syscall.Reboot(...)` | C-4 fails | caught |
| `sudo -n systemctl reboot`, `systemctl --no-block reboot`, plain `sudo systemctl reboot` | C-4 fails | caught |
| `env reboot`, `timeout 5 reboot`, `systemd-run systemctl reboot` | C-4 fails | NOT caught (finding F-2, not a defect against the ADD rules) |
| Second `execShutdown` caller | AC-020 fails | caught (2 tests) |
| Third `execReboot` reference (method value) | PF_C8 fails | caught |
| Permit forged by field assignment; `powerPermitSeq` used outside mint; third `powerGate` literal; `mintPowerPermit` from a third function; `awaitPowerClearance` from a third function; `awaitRebootClearance` with nil or shutdown guards | C-3 fails | all caught |
| `consumePowerPermit` ignores the action | cross-action tests fail | caught (both) |
| Gate drops the allow-flag check | C-3 runtime fails | caught (9 rows) |
| C-1 guard neutered in `execShutdown`; removed from `execReboot` | C-1 AST fails | caught |
| Non-zero exit counted as initiated (in `initiated()` and in `classifyPoweroff`) | AC-028 fails | caught |
| Remove one-child guard; never clear the flag; no `Wait`; drop `close(abandoned)`; start-check bound 0 | AC-030 fails | all caught |
| Reboot launch gains a `Wait` | AC-032 fails | caught |
| Windows argv `/s` to `/p` | AC-008 fails | caught |
| Skip result sets `ReasonTimeout` | AC-005 fails | caught |
| `handleShutdown` pre-check removed | AC-009 fails | caught |
| Reboot wait message changed | AC-019 fails | caught (3 tests) |
| Shutdown agent max 1m; shutdown wait derived from reboot; shutdown server deadline 10m | policy tests fail | caught |
| Error text limit 200 to 300; error text not sanitized | AC-029 fails | caught |
| **Shutdown gate wired to `models.DpkgLockWaitBound`** | AC-018 fails | **SURVIVED the delivered suite**; caught by the QA test (F-1) |
| `elapsed > bound` instead of `>=` | boundary fails | the fake-clock loop never terminates, so the suite hangs. That is a detection by timeout, not a clean failure. Recorded, no action |

## Reboot Invariance (AD-008, FR-021)

- The diff of `reboot_clearance_test.go`, `reboot_clearance_qa_test.go` and the reboot parts of `reboot_callsites_test.go` is exactly the AD-008 list: renames, one fixture field (`Bound`), and the four shutdown assertions removed from `TestCommandPolicy_QA_AC020_...`. Every reboot assertion is kept.
- Reboot production changes are the ones AD-008 lists. The gate-level guard step runs `shuttingDown` then `AllowReboot` with the same texts and order as `commonRebootGuards`, which all three reboot callers pass, so the first reported failure is unchanged. `execReboot` still uses `Start()` with no `Wait`, the same argv and no timer (AST test and mutation).
- Reboot budgets: `DpkgLockWaitBound` 10m, agent 12m, server 20m, `exec_deadline_s` 1200, pinned by `RebootGate_SD_AC019_*` and `QA_AC016_AgentCommandContextIsTheShutdownBudget`. Reboot messages and log text are pinned by the unchanged `RD_*` tests, and a changed wait message fails three of them.
- Shutdown budgets: wait 10m, agent 12m, server 20m, TTL 15m, start check 3s, and `10m + 7s + 3s < 12m`. They are independent of reboot's: the AST test forbids cross-references, and QA added a test that each gate reads its own constant.

## Edge Cases Examined

- **Exit mapping** (exit 0, non-zero, launch error, still running at 3s, signal): every row is covered through both the seam and real helper-process errors. A late refusal logs one Warn; a late exit 0 or late signal logs none.
- **Boundary at 10m:** free at exactly 10m proceeds with "after waiting 10m"; held until 10m+1s skips at exactly 10m (QA).
- **Already stopping:** a shutdown that arrives after SIGTERM never clears and mints no permit (QA).
- **Permit hygiene:** zero, used, stale, wrong-action and skipped-clearance permits are refused with nothing run. A wrong-action permit is not consumed. After a skip or a refusal no live permit remains (QA).
- **Bounded growth:** at most one poweroff child per agent, a second launch is refused with `a previous poweroff command is still running`, output capped at 4 KiB, `<err>` at most 200 bytes. 100 poweroffs across all four exit kinds leave no goroutine and no stuck flag. A hung child holds exactly one goroutine regardless of how many further attempts are refused (QA). Gate bound: at most 303 probes, 2 Info lines, no files.
- **Input attacks:** ESC sequences, OSC hyperlinks, NUL, DEL, C1 controls, bidi overrides, zero-width characters, U+2028/2029, invalid UTF-8, a 4 KiB input and a multi-byte rune across byte 200 all produce printable, valid, at-most-200-byte text.
- **Auth:** `allow_shutdown` false is refused with no probe. The gate re-checks the flag itself, so `nil` guards, the other action's guards, and a nil config cannot mint a permit.
- **System boundary:** a lock state of unknown, zero, or held-then-absent skips at once with the cause. The poweroff is not bound to the command context (AST: no `CommandContext`), so budget expiry cannot kill an accepted poweroff.

## Findings

All are non-blocking. None violates the FRD.

- **F-1 (test gap, fixed by QA).** The delivered suite could not detect a shutdown gate wired to the reboot wait bound, because both are 10 minutes. Added `TestPowerGate_QA_AC018_GateBoundsAreWiredToTheirOwnConstants` (AST, with a self-test of crossed wiring). The mutation now fails.
- **F-2 (C-4 scan limit, documentation only).** The scan does not see a power tool run through a wrapper other than `sudo`/`doas`/a shell, for example `exec.Command("env","reboot")`, `timeout 5 reboot`, `systemd-run systemctl reboot`, `nohup`/`setsid`, or a write to `/proc/sysrq-trigger`. This is within the ADD's stated residual R-11 (AST-only, fixed rule set) and meets every C-4 requirement as written, so it is not a defect. Recommend adding common wrappers (`env`, `nohup`, `setsid`, `timeout`, `nice`, `systemd-run`) to rule (a) in a follow-up. No test written, because it would fail by design.
- **F-3 (implementation, LOW, no fix required for this story).** `execShutdown`'s goroutine hands the exit over first and clears `poweroffRunning` in a deferred `Store(false)` afterwards. A second `execShutdown` that begins in the microseconds between the hand-off and the clear is refused with `a previous poweroff command is still running`, although the first child has already exited. Through the product this is practically unreachable: it needs the host key to be released and a new server dispatch to arrive and pass intake inside that window, and the result is a visible, safe failure. It showed up only in a tight test loop (QA's first draft, now waits for the flag). If it is ever worth removing, clear the flag before the hand-off.
- **F-4 (inherited from REBOOT-DPKG-001, LOW).** The command-context deadline is checked only on the gate's sleep path (`guardBudgetExpired`). A command whose context had already expired when it reached a free lock would still clear. With a 12m budget and a worst-case gate of about 10m07s plus 3s this is unreachable in practice. Unchanged by this story.
- **F-5 (coverage not run here).** The SIGTERM row of the AC-032 mapping test is skipped on Windows, and the real `exec.Command` branch is verified by AST only. Run `go test -race ./internal/agent` on Linux CI before release (DoD).
- **F-6 (information).** The `<err>` text of outcome (g) is printable and bounded, but unlike the holder text it is not limited to a safe charset. It comes from `sudo`, `systemctl` or `shutdown.exe` running a fixed argv, so it carries no attacker-chosen input (ADD section 10). ANSI remnants such as `[2J` survive after the ESC byte is removed. Cosmetic.

## ARCH-QUESTION Judgement (`SHUTDOWN-DPKG-001-blockers.md`)

- **AQ-1 (`errPoweroffStillRunning` moved into `execShutdown`, fallback text reworded): accept.** The result text is identical (`shutdown: a previous poweroff command is still running`, pinned by `SD_AC030_SecondLaunchWhileChildRunsIsRefused`). The reworded fallback is reachable only for a zero-value `poweroffExit`, and `TestClassifyPoweroff_SD_AC015_*` pins that it is an error. Keeping C-4(b) strict is the safer choice, and the C-4 scan stays simple. The architect should record it in the ADD (section 4 lists the variable in `poweroff.go`).
- **AQ-2 (reaper Warn has no `command_id`): accept.** At most one poweroff child exists per agent and at most one Warn per command, the line carries the sanitized error, and the host key serializes shutdowns, so the line is attributable in practice. It stays within NFR-003 (at most one Warn, none per check). Adding the command id would need a second argument or a permit field, which the AD-002 and AD-006 rules deliberately forbid. A follow-up can add it without weakening any rule if operators ask.
- Implementation-report Deviations 4 (seven commits instead of six), 5 (the AD-008 edit detail), 6 (no `pendingTimers()` accessor) and 7 (no real-host check, which this task also forbade): all acceptable.

## Tests Added by QA

All in `internal/agent/shutdown_qa_test.go` (new file, commit `cd00a66`), each commented `Added by QA Engineer` and `Covers AC-nnn`.

| Test | Covers |
|---|---|
| `TestPowerGate_QA_AC018_GateBoundsAreWiredToTheirOwnConstants` | AC-018, FR-019, BR-004 (closes F-1) |
| `TestShutdown_QA_AC016_AgentCommandContextIsTheShutdownBudget` | AC-016, FR-018, FR-020 |
| `TestShutdown_QA_AC004_ExactBoundary` (2 subtests) | AC-003, AC-004, FR-003 |
| `TestShutdown_QA_AC013_AlreadyShuttingDownNeverPowersOff` | AC-013, FR-010, FR-012 |
| `TestExecShutdown_QA_AC030_NoGoroutineOrFlagLeakAcrossEveryExit` | AC-030, NFR-003 |
| `TestExecShutdown_QA_AC030_HungChildHoldsOneGoroutineThenReleasesIt` | AC-030, NFR-003 |
| `TestSanitizePowerErr_QA_AC029_UnicodeControlAndFormatCharacters` (8 subtests) | AC-029, FR-028 |
| `TestShutdown_QA_AC029_ControlOnlyOutputKeepsTheForm` | AC-029 |
| `TestShutdown_QA_AC028_RefusalIsFinalForTheAttemptAndFreesTheAgent` | AC-028, BR-006, FR-008 |
| `TestShutdown_QA_AC004_SkippedAttemptsLeaveNoLivePermit` (2 subtests) | AC-004, AC-007, C-3 |

## Deviations from ADD

None that change behaviour. Two ADD text items differ and are accepted above: AQ-1 (location of the still-running error, reworded fallback) and AQ-2 (no `command_id` on the reaper Warn). Commit count is seven rather than six (accepted).

## Deviations from FRD

None in code. FR-026 and FR-033 (documentation) are not yet met, by plan: they belong to the technical writer after this report.

## Verdict Rationale

**PASS WITH NOTES.** Every AC that can be verified in code is covered by tests that fail when the AC is broken (about 45 mutations tried; one real survivor, now closed by a QA test). All tests are green, with no flake over repeated runs. Reboot behaviour is unchanged on the evidence of the unchanged reboot tests, the AD-008 diff, and the AST launch test. The C-1, C-3 and C-4 gates hold under mutation. The two documentation ACs are deferred, as the brief allows, so the verdict is not a plain PASS.

## Action Required (before release, not before this verdict)

1. Technical writer: README (remove "Shutdown is not gated" at L2660 and L2761; the shutdown section per FR-033 and ADD section 4; power budget table row at L2171), `web/src/pages/LearnMore.tsx` help text, and the CHANGELOG entry with the server-first upgrade note (AC-026, AC-033, FR-026, FR-033, C-2 and C-5 README parts). QA then re-runs the docs checklist of ADD section 13.
2. CI: run `go test -race ./...` on Linux so the SIGTERM row and the race detector run.
3. Architect: record AQ-1 and AQ-2 as accepted in the ADD.
4. Optional follow-ups: F-2 (wrapper coverage in the C-4 scan), F-3 (clear `poweroffRunning` before the hand-off).
