# Implementation Report

- **Story ID:** SHUTDOWN-DPKG-001
- **Engineer:** Senior Dev Agent
- **Date:** 2026-10-25
- **Branch:** `feat/shutdown-dpkg-001` (base `fa8f924`; nothing pushed, tagged or merged)
- **Inputs:** FRD revision 2, ADD revision 2 (FINAL), security review (conditions C-1 to C-6)
- **Follow-up story recorded for C-2:** **SHUTDOWN-SUDO-001** (a poweroff privilege through the AGENT-PRIV capability path). An unnumbered follow-up also remains for a dashboard cancel and progress display (NFR-007).

## Detected stack

- Go module `github.com/DesyncTheThird/rIOt`, `go.mod` at Go 1.24, toolchain Go 1.26.0 on Windows. Tests use the standard `testing` package with `testify` (`assert`, `require`).
- Frontend (`web/`): React with Vitest. Not touched.
- No new dependency, no migration, no `go.mod` / `go.sum` change.

## Completed Components

| File | Action | Notes |
|---|---|---|
| `internal/models/command_policy.go` | MODIFY | `ShutdownDpkgLockWaitBound` 10m, `ShutdownAgentHardMax` 12m, `ShutdownExecDeadline` 20m, `ShutdownStartCheckBound` 3s. `ExecDeadline` and `AgentHardMax` switch on the action. No `Shutdown*` constant references a reboot constant |
| `internal/models/command_policy_test.go` | MODIFY | AC029 shutdown row 20m. `…_RD_AC020_ShutdownBudgetUnchanged` replaced by `…_SD_AC017_*`. Added `…_SD_AC031_*` and `…_SD_AC018_*` (AST) |
| `internal/server/commands/service_test.go` | MODIFY | Shutdown `exec_deadline_s` 600 to 1200 |
| `internal/server/commands/sweeper_test.go` | MODIFY | Shutdown subtest replaced by `TestSweep_SD_AC016_ShutdownNotTimedOutBefore20m` |
| `internal/agent/powerguard.go` | CREATE | `inTestBinary()` (`testing.Testing()`), `errPowerProcessInTest` (C-1) |
| `internal/agent/powerguard_test.go` | CREATE | State check, AST check on `execReboot` and `execShutdown`, self-test |
| `internal/agent/reboot_clearance.go` | MODIFY | `powerAction`, `powerGate` (with `allowed`, `allowOffText`), `rebootGate()`, `shutdownGate()`, `powerClearance` with `Bound`, `awaitPowerClearance` (the moved loop with the five AD-001 changes), `awaitRebootClearance` (same signature), `awaitShutdownClearance` (binds its own guards), `powerPermit{permitID, permitAction}`, `mintPowerPermit`, `consumePowerPermit`, `errShutdownNotCleared`, `commonPowerGuards`, `commonRebootGuards`, `commonShutdownGuards`. File name kept |
| `internal/agent/commands.go` | MODIFY | `case "shutdown": return a.handleShutdown(cr)`. New `handleShutdown`, `shutdownSkippedResult`, `powerSkippedResult` (`rebootSkippedResult` delegates). `execReboot`: `consumePowerPermit` plus the C-1 guard, otherwise unchanged (still `Start()` with no wait). New `execShutdown`: permit, one child per agent, launch closure (runner seam, C-1 guard, real branch), 3 s start check, single reaper |
| `internal/agent/poweroff.go` | CREATE | `poweroffExitKind`, `poweroffExit`, `poweroffExitFrom`, `classifyPoweroff`, `sanitizePowerErr`, `cappedWriter`, `poweroffOutputCap`, `poweroffErrMax` |
| `internal/agent/poweroff_test.go` | CREATE | Exit mapping, sanitizer, capped writer, helper-process pattern (re-executes the test binary, never a power tool) |
| `internal/agent/agent.go` | MODIFY | `powerPermitSeq` / `powerPermitLive` (renamed), new `poweroffRunning atomic.Bool` |
| `internal/agent/shutdown_clearance_test.go` | CREATE | `shutdownRig` (always installs a runner) and every `TestShutdown_SD_*`, `TestShutdownClearance_SD_*`, `TestPowerClearance_SD_*`, `TestExecShutdown_SD_*`, `TestExecReboot_SD_*`, `TestRebootGate_SD_*` test |
| `internal/agent/reboot_callsites_test.go` | MODIFY | `execSpec` generalization; `…_ShutdownHasNoGate` and `…_PermitMintedOnlyByGate` removed; AD-006.1 and AD-006.2 tests and self-tests added |
| `internal/agent/power_paths_test.go` | CREATE | AD-006.3 recursive C-4 scan, constant folder, self-test |
| `internal/agent/reboot_clearance_test.go` | MODIFY | Mechanical renames only (AD-008) |
| `internal/agent/reboot_clearance_qa_test.go` | MODIFY | Rename of `powerPermitSeq`; shutdown budget assertions removed (AD-008) |
| the fake clock helper | NO CHANGE | `fakeClock.timerDurations()` already exists, so the `pendingTimers()` accessor of §4 was not needed |

Not changed (verified by `git diff --stat fa8f924..HEAD`): `scripts/`, `web/`, `cmd/`, `go.mod`, `go.sum`, migrations, `internal/server/**` non-test code, `internal/agent/dpkglock*.go`.

## Commits

```
3d89be9 [SHUTDOWN-DPKG-001] feat: shutdown budgets split from reboot (wait 10m, agent 12m, server 20m)
32504bb [SHUTDOWN-DPKG-001] feat: refuse real power processes inside a test binary (C-1)
de7794c [SHUTDOWN-DPKG-001] refactor: one power gate and an action-bound permit for reboot and shutdown
34a6f9a [SHUTDOWN-DPKG-001] feat: poweroff exit mapping shared by the test seam and production
0876b87 [SHUTDOWN-DPKG-001] feat: shutdown waits for the dpkg lock and reports the poweroff truthfully
ae0a923 [SHUTDOWN-DPKG-001] test: call-site and binding rules for execShutdown, the permit and the gate
936862e [SHUTDOWN-DPKG-001] test: only execReboot and execShutdown may launch a power action (C-4)
```

C-1 landed before `handleShutdown` became reachable from any test. The reboot tests were green at every commit.

## Test Summary

Every test named in ADD §8 exists (checked mechanically against the §8 table), with an `SD_ACnnn` / `SD_Cn` name and `[AC-nnn]` / `[C-n]` assertion messages.

| AC / condition | Test file | Tests | Status |
|---|---|---|---|
| AC-001 to AC-016, AC-018 (gate and runtime), AC-019, AC-021 to AC-024, AC-027 to AC-030, AC-032 | `internal/agent/shutdown_clearance_test.go` | `TestShutdown_SD_*`, `TestShutdownClearance_SD_*`, `TestPowerClearance_SD_AC018_*`, `TestRebootGate_SD_AC019_*`, `TestExecShutdown_SD_AC008_*` / `_AC030_*`, `TestExecReboot_SD_AC032_*` | PASS |
| AC-016 (server), AC-017, AC-018 (AST), AC-031 | `internal/models/command_policy_test.go`, `internal/server/commands/sweeper_test.go` | `TestCommandPolicy_SD_AC017_*`, `_SD_AC018_*`, `_SD_AC031_*`, `TestSweep_SD_AC016_*` | PASS |
| AC-015, AC-028, AC-029, AC-030, AC-032 (mapping) | `internal/agent/poweroff_test.go` | `TestPoweroffExitFrom_SD_AC032_*`, `TestClassifyPoweroff_SD_*`, `TestSanitizePowerErr_SD_AC029_*`, `TestCappedWriter_SD_AC030_*`, `TestPoweroffExit_SD_AC032_SeamAndProductionErrorsMapAlike` | PASS (the SIGTERM row is skipped on Windows) |
| AC-020 | `internal/agent/reboot_callsites_test.go` | `TestShutdownCallSites_SD_AC020_*`, `TestRebootAuth_PF_C8_*` (unchanged), `TestRebootCallSites_RD_AC020_*` (unchanged) | PASS |
| C-1 | `internal/agent/powerguard_test.go` | `TestPowerGuard_SD_C1_*` | PASS |
| C-3 | `reboot_callsites_test.go`, `shutdown_clearance_test.go` | `TestPowerCallSites_SD_C3_*`, `TestShutdownCallSites_SD_C3_SelfTest`, `TestPowerClearance_SD_C3_AllowFlagGatesTheMintWhateverTheGuards`, `TestExecShutdown_SD_C3_*`, `TestExecReboot_SD_C3_*` | PASS |
| C-4 | `internal/agent/power_paths_test.go` | `TestPowerPaths_SD_C4_*` | PASS |
| AC-025 | none (no `web/` change) | web suite | PASS |
| AC-026, AC-033, C-2 (README part), C-5 (README part) | none | **Open: documentation is the technical writer's post-QA step (ADD §4). See Deviations 1.** | OPEN |

New tests are stable: `go test ./internal/agent -run 'SD_|ExecShutdown|PowerGuard' -count=10` passes.

### Test Run Output

Commands ran with `GOCACHE=D:\rIOt_gocache` and `GOTMPDIR=D:\rIOt_fuzz_tmp`. The full verbose log is about 4,700 lines, so the per-package result lines are pasted here instead.

`go test ./... -count=1 -v` (Windows, no `-race`): 29 packages ok, 0 failed. 2185 top-level tests passed, 2162 subtests passed, 0 failed, 48 skipped (pre-existing platform skips plus the one Windows SIGTERM subtest).

```
ok   github.com/DesyncTheThird/rIOt/cmd/riot-signer 1.134s
ok   github.com/DesyncTheThird/rIOt/internal/agent 22.330s
ok   github.com/DesyncTheThird/rIOt/internal/agent/collectors 11.324s
ok   github.com/DesyncTheThird/rIOt/internal/logbudget 0.402s
ok   github.com/DesyncTheThird/rIOt/internal/maintenance 0.610s
ok   github.com/DesyncTheThird/rIOt/internal/models 0.540s
ok   github.com/DesyncTheThird/rIOt/internal/redact 0.369s
ok   github.com/DesyncTheThird/rIOt/internal/releasekey 0.593s
ok   github.com/DesyncTheThird/rIOt/internal/resilient 0.993s
ok   github.com/DesyncTheThird/rIOt/internal/server 3.277s
ok   github.com/DesyncTheThird/rIOt/internal/server/auth 0.583s
ok   github.com/DesyncTheThird/rIOt/internal/server/ca 0.304s
ok   github.com/DesyncTheThird/rIOt/internal/server/commands 1.484s
ok   github.com/DesyncTheThird/rIOt/internal/server/db 1.018s
ok   github.com/DesyncTheThird/rIOt/internal/server/events 0.746s
ok   github.com/DesyncTheThird/rIOt/internal/server/handlers 9.143s
ok   github.com/DesyncTheThird/rIOt/internal/server/ingest 0.437s
ok   github.com/DesyncTheThird/rIOt/internal/server/middleware 1.181s
ok   github.com/DesyncTheThird/rIOt/internal/server/notify 1.115s
ok   github.com/DesyncTheThird/rIOt/internal/server/oidc 1.650s
ok   github.com/DesyncTheThird/rIOt/internal/server/partition 0.598s
ok   github.com/DesyncTheThird/rIOt/internal/server/probes 1.218s
ok   github.com/DesyncTheThird/rIOt/internal/server/quota 0.242s
ok   github.com/DesyncTheThird/rIOt/internal/server/scoring 0.400s
ok   github.com/DesyncTheThird/rIOt/internal/server/storage 0.970s
ok   github.com/DesyncTheThird/rIOt/internal/server/summary 0.378s
ok   github.com/DesyncTheThird/rIOt/internal/server/updates 0.615s
ok   github.com/DesyncTheThird/rIOt/internal/server/websocket 0.858s
ok   github.com/DesyncTheThird/rIOt/scripts 0.595s
```

- `go vet ./...` (Windows): clean.
- `GOOS=linux go vet ./...`: clean. This also compiles the Linux-only files and every test file for Linux.
- `go build ./cmd/riot-agent` for linux/amd64, linux/arm64, linux/arm and windows/amd64: ok.
- `cd web && npm run test:run`: 95 files, 1707 tests passed (unchanged from baseline).

DoD greps (non-test code under `internal/agent`): `powerPermit{` only in `mintPowerPermit`; `rebootPermit` matches nothing; the quoted string `"poweroff"` only in `execShutdown`; `testing.Testing` only in `powerguard.go`; no `time.Now()` / `time.Sleep` in `dpkglock*.go`, `reboot_clearance.go` or `poweroff.go`.

## Deviations from ADD

These trigger architect review.

1. **Documentation not written (AC-026, AC-033, FR-026, FR-033, C-2 README part, C-5 README part).** ADD §4 assigns the README, `web/src/pages/LearnMore.tsx` and CHANGELOG changes to the technical writer after QA, and the engineering brief forbids edits under `web/`. The code and tests for this story are complete. The README, LearnMore and CHANGELOG changes in ADD §4 "Technical writer (post-QA)" still need to be done before the story is releasable. The upgrade-order note (server before agents, BR-005) is part of that work.
2. **`errPoweroffStillRunning` removed (AQ-1 in `SHUTDOWN-DPKG-001-blockers.md`).** ADD §4 puts it in `poweroff.go`, but C-4(b) (AD-006.3) forbids a `poweroff` string outside `execReboot` / `execShutdown`. The error is now created inside `execShutdown`. The result text is the same. The fallback text in `classifyPoweroff` was reworded to "shutdown command did not start" for the same reason. Architect to confirm or relax rule (b).
3. **Reaper Warn has no `command_id` (AQ-2).** `execShutdown(p powerPermit)` has one argument by AD-002 / AD-006.1, and the permit carries no command id. The line is `shutdown: poweroff command failed after the start check` with `error=<sanitized>` only. ADD §6 shows a `command_id` attribute.
4. **Commit split.** ADD §12 note 1 lists six commits. The call-site / binding tests and the C-4 power-path scan are two commits here (seven in total), to keep one logical change per commit. The order is otherwise as specified.
5. **AD-008 test edit detail.** In `TestCommandPolicy_QA_AC020_RebootAndShutdownBudgetsAreIndependent`, the `require.NotEqual` precondition line (it compared reboot and shutdown budgets, which are now equal by value) was removed together with the four shutdown assertions listed in AD-008, and the now-unused `require` import was dropped. The test's comment was updated. All reboot assertions are kept.
6. **No `pendingTimers()` accessor.** The existing `fakeClock.timerDurations()` serves the AC-030 tests (`launchedAndArmed` helper), so the fake clock file is unchanged.
7. **Host verification (ADD §12 note 11) not performed.** The coordinating session instructed that no real-host check be made for this story, and Clod ran none. The question "does `riot` have a `systemctl poweroff` sudo rule on Hawaii" is therefore unanswered here. `git log -S poweroff -- scripts/install.sh` (security review) says no installer version ever granted one. No files were left under `/tmp` on any host.

Interpretation recorded (not a deviation): `poweroffExitKind` declares `launchFailed` as the zero value, so a zero-value `poweroffExit` can never read as success. The ADD lists the kinds in a different order, with no semantic difference.

## Notes for QA

- **Safety.** No test reaches a real `systemctl poweroff|reboot` or `shutdown /s|/r`. Every shutdown test uses `shutdownRig` (asserts a non-nil runner). `execReboot` and `execShutdown` return `errPowerProcessInTest` in any test binary before any `exec.Command`. The C-1 tests check this by state and AST and never run the real branch. The helper-process tests re-execute the test binary itself (`-test.run=^TestPoweroffHelperProcess$`), never a power tool. No installed `riot-agent` was executed.
- **Linux-only coverage.**
  - The SIGTERM row of `TestPoweroffExit_SD_AC032_SeamAndProductionErrorsMapAlike` is skipped on Windows. It makes the helper child send itself SIGTERM and expects exit code −1 (kind `signaled`). It compiles and vets for Linux but was not executed. Please run `go test -race ./internal/agent` on Linux CI.
  - The real production branch of `execShutdown` (`exec.Command` with `Start` then `Wait`, `WaitDelay`) is verified by AST only. It was never run, by design.
- **Edge cases worth probing.**
  - A release at exactly the 10 minute boundary (`ProbeAtExactBound*` for reboot are unchanged; the shutdown equivalents are covered by the AC-004 and AC-016 tests).
  - A poweroff child that never returns: the next `shutdown` on that agent fails with `shutdown: a previous poweroff command is still running`, until the agent restarts (bounded at one child, R-9 / AD-007).
  - R-10: a signal-killed child within 3 s counts as initiated.
  - On installer-managed Linux hosts the result is now `shutdown: exit status 1: sudo: a terminal is required …` until SHUTDOWN-SUDO-001 ships. That is the intended truthful result.
- **QA gate (security C-6).** The AC-004, AC-007, AC-009, AC-012, AC-013, AC-014, AC-016, AC-018, AC-019, AC-020, AC-023 and AC-027 to AC-032 tests, `TestPowerGuard_SD_C1_*`, the C-3 runtime and AST tests including both cross-action refusal tests, `TestPowerPaths_SD_C4_*`, and `TestRebootAuth_PF_C8_ExecRebootHasExactlyThreeGuardedCallSites` all pass. The docs checklist of ADD §13 is open until deviation 1 is done.
- **New environment variables:** none. No new configuration key.
- **Migration:** none.

## Post-QA fix

QA note 3 found a microsecond race in `execShutdown`: `poweroffRunning` was cleared by a `defer` in the reaper goroutine, which runs after the exit is handed over on `done`. A second `shutdown` starting in that window was refused with "a previous poweroff command is still running", although the child had already exited.

- **Fix:** the goroutine now clears `poweroffRunning` right after the child's exit is known and before the hand-off. The at-most-one-live-child rule (AD-007) is unchanged, because the child has exited at that point. A child that never exits still holds the flag.
- **Regression test:** `TestExecShutdown_SD_AC030_FlagClearedBeforeHandOffAllowsBackToBackLaunches` makes 2000 back-to-back launches and requires each to succeed and the flag to be clear on return. With the flag cleared first, the clear happens-before the receive, so the test passes deterministically. The window cannot be forced without a test hook, so under the old ordering the test fails by repetition: it failed on 3 of 3 runs when the old `defer` was temporarily restored.
- The existing `TestShutdown_SD_AC030_SecondLaunchWhileChildRunsIsRefused` and the QA one-flag tests still pass.
