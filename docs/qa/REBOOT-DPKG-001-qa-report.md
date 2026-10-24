# QA Report

**Story ID:** REBOOT-DPKG-001
**QA Engineer:** QA Agent (Sonnet 5.5)
**Date:** 2026-10-24
**Branch / range:** `feat/reboot-dpkg-001`, `origin/main..50c45e5` plus one QA test commit (`d5fe9ce`) and this report. Nothing pushed, tagged or deployed.
**Verdict:** PASS WITH NOTES

Every FRD acceptance criterion (AC-001 to AC-021) and every security condition that has a test (C-1, C-2, C-3, C-5, and the kept C-8 test) maps to a test that goes red when the behavior is broken. The C-7 gate holds. About 45 production-code mutants were run. Four survived the developer tests, and QA closed them with 10 new tests, one of which is a test the ADD lists but the implementation lacked. There is no defect. The notes are residual risks and one literal FRD wording gap, all non-blocking.

---

## Test Run Summary

All numbers were produced on this machine, not copied from the implementation report. Go ran with `GOCACHE=D:\rIOt_gocache` and `GOTMPDIR=D:\rIOt_fuzz_tmp`; npm with `npm_config_cache=D:\rIOt_npm_cache`. No `-race` (Windows has no CGO).

| Suite | Result |
|---|---|
| `go test -count=1 ./...` (before QA tests) | 29 packages `ok`, 0 failing, exit 0 |
| `go test -count=1 -v ./...` (after QA tests) | **2,117 top-level PASS, 0 FAIL, 48 SKIP**; 2,075 sub-tests PASS, 0 FAIL; 29 packages `ok`. The developer's 2,107 reproduces exactly; 2,107 + 10 QA = 2,117. The 48 skips are the pre-existing platform- or Postgres-gated ones; none is an `RD_` test |
| `RD_` tests | 105 top-level PASS on Windows (104 from the developer, 1 added by QA; the 7 in `dpkglock_linux_test.go` are build-tagged out) |
| `go vet ./...` | clean (exit 0) |
| `GOOS=linux go vet ./...` | clean (exit 0). This compiles `dpkglock_linux.go` and `dpkglock_linux_test.go` |
| `cd web && npm run test:run` | 95 files, **1,707 tests passed**, 0 failed (no web change in the diff) |
| Agent cross-builds: linux/amd64, 386, arm64, arm (GOARM=6), darwin/arm64, windows/amd64 | all build; `GOOS=linux GOARCH=386 go vet ./internal/agent` clean (checks the `syscall.Stat_t` conversions on 32-bit) |

Trimmed output of the full run:

```
ok  github.com/DesyncTheThird/rIOt/cmd/riot-signer
ok  github.com/DesyncTheThird/rIOt/internal/agent            ~19s
ok  github.com/DesyncTheThird/rIOt/internal/agent/collectors
ok  github.com/DesyncTheThird/rIOt/internal/models
ok  github.com/DesyncTheThird/rIOt/internal/server/commands
ok  github.com/DesyncTheThird/rIOt/internal/server/handlers
... 29 packages ok, 0 FAIL, exit 0
web: Test Files 95 passed (95)  Tests 1707 passed (1707)
```

**Flaky tests:** none. The full Go suite ran three times (twice with the QA tests), the new QA tests ran with `-count=2`, and the web suite once. The only real-time test (the QA shutdown test, about 2 s) was stable.

**Not covered by QA (stated limits):**
- The Linux-only tests in `internal/agent/dpkglock_linux_test.go` (real `/proc/locks` with `FcntlFlock`, real `self/ns/pid`, real `self/status`, `statFileID`) are compiled by `GOOS=linux go vet` but **not executed** here. CI's Linux job runs them.
- No `-race` run. The new QA tests only touch atomics and a mutex-guarded rig, but the CI `go test -race` run is the check.
- No Docker run. A container is a private pid namespace, so the real-`/proc` tests would skip by design (`requireInitPidNamespace`).
- Only Hawaii (ext4 and tmpfs) was sampled on real hardware, and as a login user (uid 1000), not inside the real `riot-agent.service` unit (`ProtectSystem=strict` mount namespace, user `riot`). The inode-only (major-0) path is unverified on real hardware (security condition C-6, accepted).

---

## Mutation Testing

Each mutant was applied to the real file, the affected tests were run, and the file was reverted with `git checkout -- <file>`. The tree was clean after every run. **Killed** means a test went red (or the test run hit its timeout, noted below).

| Area | Mutants | Killed by developer tests | Notes |
|---|---|---|---|
| Gate (`reboot_clearance.go`) | 14: timeout `>=` to `>`; `sawHeld` rule dropped; `allow_reboot` guard dropped; shutdown guard dropped; loop-top shutdown check dropped; final probe skipped; guards not re-armed after a re-held lock; `Unknown` treated as `Free`; poll 2 s to 20 s; shutdown-vs-budget distinction lost; wait-started log per poll; last sleep uncapped; guard owner id wrong on two paths; unrecognized-state `default` removed | 13 of 14 | **Survivor, now killed by QA:** the last sleep is not capped to the time left in the bound (G2). Three kills come from the 90 s test timeout (an infinite loop), not an assertion: timeout `>=` to `>` (M1), per-poll wait-started log (G1) and `default` removed (G7). In production M1 is near-equivalent (a few ms later) |
| Probe (`dpkglock.go`) | 16: inode-only leniency widened; `[` allowed in comm; comm cap 16 to 64; cut-line trimming removed; 1 s bound `>` to `>=`; 1 MiB cap `>` to `>=`; waiter not skipped; `NSpid` check loosened; pid-namespace link not compared; pid <= 0 reads a holder path; blank line accepted; effective uid read; dpkg markers ignored; `/proc/locks` `LimitReader` removed; 8 KiB small-read cap removed | 13 of 16 | **Survivors, now killed by QA:** inode-only leniency widened (P1), 1 s bound `>=` (P5), small-read cap removed (P15). One mutant did not compile and is not counted |
| Wiring and results (`commands.go`, `reboot_auth.go`) | 8: skip returns `ReasonTimeout`; skip returns `success`; waited text dropped; post-patch skip without telemetry; without event; reservation not released; permit check removed; permit never stored live | 8 of 8 | |
| Models (`command_policy.go`) | 3: shutdown takes the reboot budget; reboot `ExecDeadline` back to 10 m; reboot `AgentHardMax` back to 1 m | 3 of 3 | |
| Post-patch pre-gate checks (`commands.go` blocker check before the gate) | 1 | 0 of 1 | **Equivalent mutant:** the pre-gate `maintenanceBlockerFor` and `shuttingDown` checks in the unauthorized goroutine are repeated by the gate's guards, so removing the early one changes only the order. Not a gap |
| G4/G5/G6 (QA-added, to confirm the new tests bite) | 3 | n/a | `sawHeld` reset on `Free`, comm sanitizer bypassed, loop-top shutdown removed: each is killed by a QA test (G6 by assertion after the test's own 8 s context, not by a hang) |

---

## AC Coverage Audit

COVERED means the listed tests fail when the AC is broken, checked by reading the assertions and by the mutants above. Test names are shortened (`RD_ACnnn_` prefix); all are in `internal/agent` unless marked.

| AC | Status | Tests covering it | Gap / note |
|---|---|---|---|
| AC-001 | COVERED | `RebootClearance_RD_AC001_FreeLockRebootsWithNoSleepOnAllThreePaths` (3 paths: one runner call, zero gate pauses, exactly 2 probes, unchanged text `reboot initiated`), `…FreeRunsGuardsThenFinalProbe` (order `probe, guards, probe, execReboot`) | Mutant "skip final probe" killed by both |
| AC-002 | COVERED | `RebootClearance_RD_AC002_HeldLockNeverRebootsWithinBoundOnAllThreePaths` (asserts no reboot at every probe, not only at the end), `ProcLocks_RD_AC002_*` (POSIX line, hex/decimal, block device, anonymous device, 12 kernel-variant fixtures, waiter), `DecodeLinuxDev_RD_AC002_*`; Linux `DpkgLock_RD_AC002_RealProcLocksSeesFcntlLockOnTempFile` (compiled, not run); **QA** `ProcLocks_QA_AC002_AnonymousTargetDoesNotMatchBlockDeviceLine` | The widened-leniency mutant survived the developer tests |
| AC-003 | COVERED | `ProcLocks_RD_AC003_LegacyLockAloneIsHeld`, `DpkgProbe_RD_AC003_FrontendAbsentLegacyPresentStillChecked`, `RebootClearance_RD_AC003_LegacyHeldWaitsLikeFrontend` (end to end through the real parser, 3 m wait, exact text); **QA** `DpkgProbe_QA_AC003_SingleLockFileHosts` (5 cases) | The reverse mixed case (frontend only) and "one file present and free is `Free`, not `Absent`" were untested |
| AC-004 | COVERED | `RebootClearance_RD_AC004_ReleaseAfter3mRebootsWithin10s` (3 paths; start time minus release time <= 10 s), `…PollIntervalAndProbeTimeoutFitBound` | Assumes the 5 s guard cap at `maintenance.go:38` (ADD-accepted) |
| AC-005 | COVERED | `…TimeoutSkipsAndNeverForces` (3 paths; wait ends at exactly 10 m), `…ProbeAtExactBoundHeldSkips`, `…ProbeAtExactBoundFreeProceeds`, `…NoSecondWaitAfterSkip`; **QA** `RebootClearance_QA_AC014_TotalWaitNeverExceedsBoundWhenGuardsTakeOddTime` | `NoSecondWaitAfterSkip` alone is weak (a retry would run before it samples), but the exact-10 m assertion in `TimeoutSkipsAndNeverForces` catches a second wait |
| AC-006 | COVERED | `HandleReboot_RD_AC006_TimeoutResultFailedAndNamesHolder` (exact message with name, pid, uid; status `error`; no `reboot initiated`; reason is not `timeout`), `DpkgProbe_RD_AC006_*`, `RebootClearance_RD_AC006_ReasonTexts` | Mutants "result is success" and "ReasonTimeout" killed |
| AC-007 | COVERED | `HandleReboot_RD_AC007_TimeoutReportSaysHolderNotVisible` (exact), `DpkgProbe_RD_AC007_*` (pid -1, negative remote pid, comm unreadable, status unreadable) | |
| AC-008 | COVERED | `PostPatchReboot_RD_AC008_AuthorizedTimeoutOneEventOneTelemetry`, `…UnauthorizedTimeoutOneEventOneTelemetry` (exact event text, one event, one telemetry, warning severity, no reboot) | Event-dropped and telemetry-dropped mutants killed |
| AC-009 | COVERED | `PostPatchReboot_RD_AC009_SkipReleasesHostReservation` (both post-patch paths; a `TryLockAll` for another command then succeeds) | Reservation-not-released mutant killed |
| AC-010 | COVERED (after QA) | `DpkgProbe_RD_AC010_*` (stat error, `/proc/locks` missing and denied, nested namespace, `NSpid` missing, `self/status` unreadable, dpkg markers present with no lock files, marker stat error, bounded cause, malformed line after a match), `ProcLocks_RD_AC010_MalformedLineIsUnknown` (12 lines incl. blank, never echoed), `RebootClearance_RD_AC010_HeldThenAbsentNeverRebootsOnAllThreePaths`, `…UnknownSkipsImmediatelyWithoutSleepOnAllThreePaths`, `…UnknownMidWaitSkipsAtOnce`; **QA** `RebootClearance_RD_AC010_UnrecognizedStateFailsClosed` (3 paths) and `RebootClearance_QA_C1_HeldThenFreeThenAbsentNeverClears` | The ADD section 8 test `UnrecognizedStateFailsClosed` was missing from the implementation; QA added it under the ADD's name. A removed `default` case would have looped forever without it |
| AC-011 | COVERED (after QA) | `DpkgProbe_RD_AC011_*` (over 1 MiB, exactly 1 MiB, `ReaderStopsAtCap` <= 1 MiB + 1 served, line over 4 KiB, slow read, fast read, slow holder read), `HandleReboot_RD_AC011_OversizedLockInfoSkipsReboot`; **QA** `DpkgProbe_QA_AC011_ExactlyOneSecondIsWithinTheBound`, `…SmallReadsStopAtTheReaderCap` | The 1 s boundary and the 8 KiB cap on the three small reads were unpinned |
| AC-012 | COVERED | `DpkgProbe_RD_AC012_NoDpkgNoLockFilesIsAbsentWithoutReadingProc` (no `Open`, no `Readlink`), `RebootClearance_RD_AC012_AbsentRebootsAsToday` (no pause, no log containing `dpkg lock`), `PlatformProbe_RD_AC012_NonLinuxIsAbsent` (**ran on Windows**), Linux `StatFileID_RD_AC012_MissingPathIsNotExist` (not run) | |
| AC-013 | COVERED | `RebootClearance_RD_AC013_AllowRebootOffDuringWaitSkipsNamingGuard`, `…MaintenanceStartsDuringWaitSkipsNamingGuard` (3 paths each; exact guard text; `conflict` reason), `…GuardOrderIsShutdownThenAllowRebootThenMaintenance` | Guard-dropped and wrong-owner mutants killed (the latter by `PF_C3_OwnFinishedHelperUnitIsNotBusy`) |
| AC-014 | COVERED (after QA) | `RebootClearance_RD_AC014_FinalCheckRetakenKeepsWaitingWithinBound` (2 sub-tests: to the bound, and released with guards re-run); **QA** `RebootClearance_QA_AC014_TotalWaitNeverExceedsBoundWhenGuardsTakeOddTime` | The cap on the last sleep (G2) was unpinned |
| AC-015 | COVERED (after QA) | `RebootClearance_RD_AC015_ShutdownEndsWaitWithin5s`, `…CancelledContextWithShutdownIsAShutdownSkip`, `HandleReboot_RD_AC015_ShutdownResultReasonShutdown`, `PostPatchReboot_RD_AC015_ShutdownSkipLogsWarn`; **QA** `RebootClearance_QA_AC015_RealTimerShutdownEndsWaitWithin5s` | The developer tests all use the fake clock and a pause seam. The QA test drives the production timer path (about 2 s real time) |
| AC-016 | COVERED | `HandleReboot_RD_AC016_NoRebootAfterSkippedResult` (redelivery of the same id is deduplicated, no probe, no reboot), `RebootClearance_RD_AC016_CommandBudgetExhaustedIsATruthfulSkip` | Depends on the seen-ID store, which is pre-existing |
| AC-017 | COVERED | `HandleReboot_RD_AC017_NineMinuteWaitReportsStartedAndWaited` (`reboot initiated after waiting 9m for the dpkg lock`, status success, reason not timeout), `models` `CommandPolicy_RD_AC017_RebootBudgetCoversWait`, server `Sweep_RD_AC017_RebootNotTimedOutBefore20m` (4 sub-tests: 19 m running, 11 m running, 20 m swept with the 20m text, shutdown still 10 m), `TestCL_AC029_ExecDeadlineTable`, `TestCL_AC029_ExpiresAtPerActionClass` (`exec_deadline_s` 1200, TTL 15 m) | |
| AC-018 | COVERED | `DpkgProbe_RD_AC018_NeverOpensLockFiles` (allow-list of opened paths), `DpkgLock_RD_AC018_NoLockingCallsInSource` (AST over `dpkglock*.go` and `reboot_clearance.go`), Linux `…ProbeDoesNotDisturbHeldLock` (not run) | |
| AC-019 | COVERED | `RebootClearance_RD_AC019_FullWaitBoundedOutput` (<= 303 probes, no permit, no file in the state dir, exactly 2 gate log lines both Info, none at debug), `…PostPatchSkipProducesOneEventOneTelemetry`, `…WaitLogLinesCarryContext` | See note N-2 on the literal wording |
| AC-020 | COVERED | `RebootAuth_PF_C8_ExecRebootHasExactlyThreeGuardedCallSites` (assertions kept; stricter), `RebootCallSites_RD_AC020_GateClearanceFeedsEveryExecReboot`, `…ShutdownHasNoGate` (AST, never calls `handleShutdown`), `models` `CommandPolicy_RD_AC020_ShutdownBudgetUnchanged`, `HandleReboot_RD_AC020_PreGateAllowRebootOffIsUnchanged`; **QA** `CommandPolicy_QA_AC020_RebootAndShutdownBudgetsAreIndependent` | |
| AC-021 | COVERED | `DpkgProbe_RD_AC021_NoExecNoSudo`, `RebootClearance_RD_AC021_GateRunsNoProcessExceptTheReboot`, `ProbeDpkg_RD_AC021_SeamDefaultsToPlatformProbe`, Linux `…RealProbeWorksUnprivileged` (not run; skips as root); QA diff check: `scripts/` has no diff | |

### Security conditions

| Condition | Status | Evidence |
|---|---|---|
| C-1 (namespace, markers, `Held`-then-`Absent`, zero value, unit file) | COVERED | `DpkgProbe_RD_C1_*` (init link passes; **non-init link with a single-field `NSpid` is `Unknown` and `/proc/locks` is not opened**; unreadable and malformed link; init link with nested `NSpid`), `ParseNSpid_RD_C1_*`, `ReadSmallFile_RD_C1_*`, `DpkgLockState_RD_C1_ZeroValueIsUnknown`, `RebootClearance_RD_C1_ZeroStatusSkipsAsUnknownOnAllThreePaths`, `AgentUnit_RD_C1_NoPrivatePIDs` (both unit files), **QA** `…QA_C1_HeldThenFreeThenAbsentNeverClears`. Mutants "link not compared", "`NSpid` loosened", "markers ignored", "`sawHeld` dropped or reset" all killed |
| C-2 (uid in the holder text, inode-only marker) | COVERED | `DpkgProbe_RD_C2_HolderTextCarriesUidAndInodeOnly` (6 grammar rows), `…NonRootHolderUidReported`, `ParseStatusUID_RD_C2_ParsesRealUidOnly`. The README note is a technical-writer item (below) |
| C-3 (single-use permit) | COVERED | `ExecReboot_RD_C3_*` (zero, skipped, reused, stale, cleared once, refusal runs nothing), `RebootCallSites_RD_C3_MethodValueReferenceIsFlagged` (6 self-tests incl. method value, method expression, discarded gate, unchecked `Cleared`, hidden second call), `…PermitMintedOnlyByGate`. Permit-check and `Store` mutants killed |
| C-4 (server-first upgrade note) | NOT A CODE TEST | Documentation item for the technical writer (CHANGELOG and README). Not in this diff, as designed |
| C-5 (reader cap, fixtures, pid-only paths, charset) | COVERED (after QA) | `ReaderStopsAtCap`, `LineOver4KiBIsUnknown`, `KernelVariantFixtures`, `CommPathFromParsedPidOnly`, `SanitizeHolderName_RD_C5_CharsetAndLength`; **QA** end-to-end `DpkgProbe_QA_C5_HostileCommIsNeutralizedInTheHolderText` (6 hostile names), `…SmallReadsStopAtTheReaderCap` |
| C-6 (real-host verification) | RECORDED | Impl report, "Real-host verification (C-6)": Hawaii ext4 and tmpfs, `self/ns/pid` = `pid:[4026531836]`, `NSpid` one field, `Held` with comm, pid and uid on a scratch file, `Free` after unlock. The inode-only path is explicitly stated as unverified on real hardware, which satisfies C-6's alternative |
| C-7 (QA gate) | MET | AC-010, AC-011, AC-012, AC-014, AC-015, AC-016, AC-018, AC-020, the C-8 test and the C-1 and C-3 tests all exist and pass. DoD greps clean (below) |
| C-8 (call-site assertions) | INTACT | `git diff` of `reboot_callsites_test.go` removes only the old one-boolean-per-function helper. The two assertions (callers are exactly `handleReboot`, `maybeRebootAfterPatch`, `rebootAfterDelivery`; each checks `Commands.AllowReboot` before the call) are kept, and a reference-count assertion is added |

### Definition-of-Done greps (re-run by QA at HEAD)

| Check | Result |
|---|---|
| `git diff origin/main..HEAD` for `go.mod`, `go.sum`, `scripts/`, `web/`, `cmd/riot-server/migrations` | empty |
| `time.Now()` or `time.Sleep` in `dpkglock.go`, `dpkglock_linux.go`, `dpkglock_other.go`, `reboot_clearance.go` | none |
| `rebootPermit{` outside tests | one match, inside `mintRebootPermit` (`reboot_clearance.go:60`) |
| `4026531836` in `dpkglock.go` | one definition (`initPidNamespaceLink`, line 274) |
| `fmt.Print*`, `println(`, `TODO`, `FIXME`, `console.log` in added non-test lines | none |
| Commented-out code in added non-test lines | none (the grep hits are prose doc comments) |
| Files changed outside ADD section 4 | only the five story documents under `docs/` |
| `AgentHardMax("shutdown")` 1 m, `ExecDeadline("shutdown")` 10 m, `QueuedTTL` 15 m | pinned by `CommandPolicy_RD_AC020_ShutdownBudgetUnchanged` and the QA budget test |

---

## Edge Cases Probed (requested by the ADD)

| Edge case | Result |
|---|---|
| Timeout boundary `>=` vs `>` (gate) | `elapsed >= 10m` skips when held; free at exactly 10 m proceeds. Both pinned (`ProbeAtExactBoundHeldSkips`, `…FreeProceeds`). Mutant `>` hangs the test (killed by timeout) |
| Probe 1 s boundary and 1 MiB boundary | 1 MiB exactly parses, +1 byte is `Unknown` (developer tests). 1 s exactly is within the bound, 1 s + 2 ms is `Unknown` (**QA**) |
| `Held` then `Absent` in one attempt | `Unknown` skip with `dpkg lock files disappeared during the wait`; also `Held`, `Free`, then `Absent` (**QA**). All three paths |
| Zero-value status | `Unknown` skip `lock state not recognized`; an out-of-range value behaves the same (**QA**) |
| Mixed `lock` / `lock-frontend` absence | Either file alone is checked; one present and free is `Free`; one missing plus one unstat-able is `Unknown`; first match in file order names the holder (**QA**) |
| Reader cap | `/proc/locks` stops at 1 MiB + 1 byte served (5 MiB source); the 8 KiB small reads stop at 8 KiB + 1 (**QA**, endless source) |
| Holder-name sanitization | Charset `[A-Za-z0-9._+:-]`, `?` elsewhere, 16 bytes, trailing whitespace trimmed; markdown link, mention, ANSI, NUL, HTML, UTF-8 and a 200-byte name checked end to end (**QA**) |
| Shutdown during the wait | Fake clock: ends within the 2 s poll. Real timer (**QA**): ends in about 2 s, well inside FR-013's 5 s. The production `a.sleep` does not wake on `shuttingDown` (only on the command context, cancelled after the 20 s grace), so the 2 s poll is what bounds it, as the ADD says |
| All guards re-checked after the wait | shutdown, `allow_reboot`, maintenance run (with the path's own id excused on post-patch paths). Order pinned. A guard that fails after a re-taken lock re-runs (AC-014 second sub-test) |
| Permit reuse | A used, stale or zero permit is refused with `errRebootNotCleared` and runs nothing; a newer permit invalidates an older one |
| Budget split | reboot 12 m agent / 20 m server / `exec_deadline_s` 1200; shutdown 1 m / 10 m / 600; queued TTL 15 m for both. Sweeper keeps a 19 m reboot running and sweeps a 20 m one |
| Bounded growth | <= 303 probes, no permit minted on a skip, no file created in the state directory, exactly 2 gate Info lines per waited attempt and none at debug, 1 event and 1 telemetry trigger per post-patch skip, 0 new persistent state. Reads capped (1 MiB, 8 KiB, 4 KiB line). Nothing grows with the wait length |

---

## Test Quality Findings

1. **Mutants killed only by a timeout.** Three mutants (gate `>`, per-poll wait log, `default` removed) are "killed" by the 90 s test timeout because they turn the gate into an infinite loop, not by an assertion. The tests do detect the regression but report it as a hang. QA's own real-timer test gives the gate an 8 s context so a regression fails fast. Optional follow-up: give the fake-clock rig a step cap.
2. **The 1 s probe bound, the 8 KiB small-read cap, the inode-only leniency scope and the last-sleep cap were unpinned** (four surviving mutants). Closed by QA tests.
3. **`ADD §8` lists `TestRebootClearance_RD_AC010_UnrecognizedStateFailsClosed`; it did not exist.** The zero-status test exercises the same `default` case, so behavior was covered for `0` but not for an arbitrary value. Added by QA under the ADD's name.
4. **Behavior over implementation.** The gate tests assert observable outputs: the exact result message and status, the count of runner reboots, the event text and count, the telemetry channel depth, the reservation state and the fake-clock elapsed time. The AST tests are deliberate structural locks (C-3, C-8, AC-018) and named as such.
5. **Isolation.** Each test builds its own rig and temp dir. No shared mutable state across tests. `captureLogs` swaps the global slog default, and the package has no `t.Parallel` test, so it is safe.
6. **Mocking hygiene.** The scripted probe is the only seam in the gate tests; probe tests use a fake `/proc` that records every path opened (AC-018) and every `readlink`. Two end-to-end tests (`AC003_LegacyHeldWaitsLikeFrontend`, `AC011_OversizedLockInfoSkipsReboot`) drive the real parser through the gate, so a mock that drifted from the probe's contract would show.
7. **`AC-005_NoSecondWaitAfterSkip` and `AC-016` are weak in isolation** (no retry exists to observe). Backed by the exact-10 m assertion and the seen-ID dedupe. Informational.

---

## Adversarial Findings

| # | Finding | Severity | Test written? |
|---|---|---|---|
| N-1 | **Shutdown before the wait is silent on two post-patch exits.** `maybeRebootAfterPatch` returns without a log when `a.sleep(…, rebootDelay)` is cancelled, and `rebootAfterDelivery` returns without a log when `awaitResultDelivery` is cancelled. FR-017 says a shutdown skip "must be logged". Both exits are pre-existing and happen before the dpkg wait begins, and the cancel only fires after the 20 s shutdown grace. Shutdown during the wait itself does log a Warn (tested). Not a regression | LOW, non-blocking | No (pre-existing behavior outside the gate) |
| N-2 | **AC-019's "at most two info-or-above log lines" is met for the gate, not for the whole attempt.** A post-patch attempt also writes the path's pre-existing Info (`reboot-class packages applied, rebooting host`) and the new outcome Warn. ADD AD-006 allows "path outcome lines unchanged". The tests assert exactly two gate lines. A literal reading of the FRD counts more | LOW, documentation | No |
| N-3 | **Real service environment not exercised.** The probe was run on Hawaii as a login user. `riot-agent.service` uses `ProtectSystem=strict` (a private mount namespace, same pid namespace) and user `riot`. The unit has no `PrivatePIDs=`, `ProtectProc=` or `ProcSubset=` (guarded by `AgentUnit_RD_C1_NoPrivatePIDs`), so `/proc/locks` and `/proc/self/ns/pid` should read the same. First production check is Ryan's standalone reboot during a held lock (ADD §12 note 11.3) | LOW | n/a |
| N-4 | **Local-user veto and disguised holder (SEC-002).** Any local user can hold a byte-range lock on an inode the probe matches, or flood `/proc/locks` past 1 MiB, and so block rIOt reboots. Each skip is visible and carries the holder's uid. On major-0 filesystems the match is deterministic and marked `inode-only`. Accepted by Ryan as the price of fail-closed. The README must say so | INFO | n/a |
| N-5 | **A waiting reboot cannot be cancelled and can fire up to 10 minutes late (INFO-4).** The host key is busy for about 10.5 minutes; a hard-killed agent leaves the server row until the 20 min sweep. Accepted in the ADD (R-8). The deferred `dpkg_lock_wait` progress step is the natural follow-up | INFO | n/a |
| N-6 | **Version skew (SEC-004, C-4).** New agents against an old server see a 600 s reboot deadline, so a wait that frees in the last minute is recorded `timed_out` while the host still reboots. The CHANGELOG and README must say "upgrade the server first". Not yet written | LOW, open for the technical writer | n/a |
| N-7 | **Inode-only path unverified on real hardware (C-6).** The anonymous-device fixture is synthetic. A real btrfs, ZFS or overlay-root sample should be added as a fixture if one is ever taken | INFO | n/a |
| N-8 | **Out-of-scope input attacks checked at the tests.** SQL, script and markup payloads in `comm`: neutralized (C-5 QA test). Null bytes, ANSI escapes, non-UTF-8: mapped to `?`. Oversized `/proc/locks`, lines over 4 KiB, malformed lines, blank lines: `Unknown` with no echo of the content. Negative, zero and huge pids: no holder path is built for pid <= 0. Replayed or double-run reboot: single-use permit plus the seen-ID store. No new endpoint, no auth surface, no SQL | none | covered above |

---

## Tests Added by QA

Committed as `d5fe9ce` (message file under `D:\rIOt_fuzz_tmp`). Each test has an `Added by QA Engineer` comment and an AC or condition reference.

| File | Tests | Covers |
|---|---|---|
| `internal/agent/dpkglock_qa_test.go` | `ProcLocks_QA_AC002_AnonymousTargetDoesNotMatchBlockDeviceLine`; `DpkgProbe_QA_AC011_ExactlyOneSecondIsWithinTheBound` (2 sub-tests); `DpkgProbe_QA_AC011_SmallReadsStopAtTheReaderCap` (2 sub-tests, endless source); `DpkgProbe_QA_C5_HostileCommIsNeutralizedInTheHolderText` (6 sub-tests); `DpkgProbe_QA_AC003_SingleLockFileHosts` (5 sub-tests) | AC-002, AC-003, AC-010, AC-011, AC-018, C-5 |
| `internal/agent/reboot_clearance_qa_test.go` | `RebootClearance_QA_AC014_TotalWaitNeverExceedsBoundWhenGuardsTakeOddTime`; `RebootClearance_QA_C1_HeldThenFreeThenAbsentNeverClears`; `RebootClearance_QA_AC015_RealTimerShutdownEndsWaitWithin5s` (about 2 s real time); `CommandPolicy_QA_AC020_RebootAndShutdownBudgetsAreIndependent`; `RebootClearance_RD_AC010_UnrecognizedStateFailsClosed` (3 sub-tests; ADD §8 name) | AC-005, AC-010, AC-014, AC-015, AC-017, AC-020, C-1 |

10 new top-level tests in 2 files. All green on Windows; `GOOS=linux go vet` is clean. None of them needs Linux-only facilities, so they also run on CI's Linux job. The trailer on the commit is `Co-Authored-By: Claude Sonnet 5.5`, which matches the model that made the commit. The task text asked for Opus 5.5; the attribution rule in force names the actual model, so QA used that, as the PATCH-FLOW-001 QA did. Amend the trailer if the branch should stay uniform.

---

## Deviations from ADD

1. **`TestRebootClearance_RD_AC010_UnrecognizedStateFailsClosed` (ADD §8) was not implemented.** QA added it.
2. All eight deviations in the implementation report were read against the code. QA agrees none changes a contract in ADD section 6 or a security property: probe and gate in one commit; `readSmallFile` trimming to the last complete line; blank `/proc/locks` line is `Unknown`; the fail-closed `default`; stricter `ReaderStopsAtCap`; extra tests; the 5 s guard-cap assumption; `_` kept in holder names.
3. Implementation report says the base was `679499e`; the diff against `origin/main` also contains the five story documents. No code outside ADD section 4 changed.

## Deviations from FRD

None blocking. One literal gap, N-2 (the per-attempt log-line count in AC-019 counts only the gate's lines). FR-017's "must be logged" for a shutdown skip holds for a shutdown during the wait; N-1 lists two earlier, pre-existing silent exits. FR-001 to FR-021 and NFR-001 to NFR-006 are met; no server change was needed beyond the pinned reboot deadline (NFR-005).

---

## Verdict Rationale

**PASS WITH NOTES.**
- Zero AC with missing coverage. 21 of 21 ACs and the testable security conditions map to tests that fail on mutation, after QA closed four surviving mutants and one missing ADD test.
- All tests are green: 2,117 Go top-level tests and 1,707 web tests on Windows; `go vet` is clean for the host and for `GOOS=linux`.
- No implementation deviation from the FRD that blocks release, and no critical adversarial finding. Every failure branch fails in the safe direction (no reboot).
- The notes: N-1 and N-2 (logging wording), N-3 (real unit not exercised), N-4 to N-7 (accepted residual risks, documentation duties).

## Open Items for the Technical Writer (not QA blockers)

From ADD section 4 and the security review conditions that are documentation only:
- README: the reboot dpkg wait and its five outcomes; the post-patch skip event; the TTL/deadline table row split (`reboot` 15 min / 20 min, `shutdown` 15 min / 10 min); the requirement that the agent run in the host (initial) pid namespace with a readable `/proc/locks`; the local-user veto (C-2) with the uid hint; the residual window "sub-second to a few seconds" (INFO-3).
- CHANGELOG `[Unreleased]`: Added (dpkg wait), Changed (reboot deadline 20 min), Upgrade note: server first (C-4).
- `web/src/pages/LearnMore.tsx`: reboots wait up to 10 minutes for a running package manager and are skipped, not forced. (The diff correctly contains no `web/` change; this is the writer's.)
- Log, do not fix: CMD-LIFECYCLE-001 ADD AD-005 (power 10m/1m) and FRD NFR-004 ("milliseconds") are now stale.

## Post-Deploy Verification Steps (Ryan)

Deploy order: server first, then agents (the automatic updater can put new agents ahead of an old server; see N-6).
1. CI: confirm the Linux job runs `dpkglock_linux_test.go` and `go test -race` green. These are the two things QA could not run.
2. On Hawaii, as the real `riot-agent` service, with a held `lock-frontend` (for example `sudo apt-get install` of a large package in another session): issue a standalone `reboot` from the dashboard. Expect no reboot while held, one `dpkg lock held, waiting before reboot` Info line, and after the release `reboot initiated after waiting <D> for the dpkg lock`. Journal: no per-check lines.
3. Same with the lock held for more than 10 minutes: expect a failed result `reboot skipped: dpkg lock held past the timeout (10m), held by <comm> (pid N, uid U)` and no reboot.
4. Check `readlink /proc/self/ns/pid` in the service context is `pid:[4026531836]` (`systemctl show riot-agent -p MainPID`, then `sudo readlink /proc/<pid>/ns/pid`).
5. A non-Debian host (if any) reboots as before with no wait.
6. Clear the Hawaii scratch files named in the implementation report (`/tmp/riot-reboot-dpkg-001/`).

## Action Required

None (not a FAIL). Recommended follow-ups, in priority order:
1. Technical writer: the documentation items above, especially the server-first upgrade note (C-4) and the local-user veto (C-2).
2. N-1: log the two silent pre-wait shutdown exits on the post-patch paths.
3. N-7: add a real major-0 `/proc/locks` sample as a fixture if a btrfs, ZFS or overlay-root host is ever available.
4. Optional: the `dpkg_lock_wait` progress step (INFO-4).
