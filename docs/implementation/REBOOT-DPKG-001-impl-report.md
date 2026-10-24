# Implementation Report

- **Story ID:** REBOOT-DPKG-001
- **Engineer:** Senior Dev Agent (Opus 5.5)
- **Date:** 2026-10-24
- **Branch:** `feat/reboot-dpkg-001` (not pushed, not tagged). Base: `679499e` (ADD revision 2).
- **Detected stack:**
  - Backend and agent: Go 1.24 (`go.mod`), chi v5, pgx v5, `log/slog`, golang-migrate. No new dependency, `go.mod` and `go.sum` unchanged.
  - Frontend: React 19 + TypeScript, Vite, Tailwind, vitest. Not touched.
  - Tests: `go test` with testify, vitest. CI runs `go test -race`, `go vet`, `npm run test:run`.
  - Specialists active: Backend (agent and models). No Database work (no migration). No Infrastructure work (no env var, no unit, no `install.sh`, no sudoers change). No Frontend work.

Standards applied: the repo `CLAUDE.md` conventions and the surrounding code's style (no `standards` skill was available in this session). Named constants for every new bound, small single-purpose functions, nil-means-production test seams (`dpkgLockProbe`, `pause`, `clk`).

## Completed Components

Commits (oldest first, all end with the required `Co-Authored-By` line):

| Commit | Subject |
|---|---|
| `59b1827` | feat: split the reboot budget from shutdown (agent 12m, server 20m) |
| `fd74818` | feat: add the dpkg lock probe and the shared reboot gate |
| `e76ac4b` | feat: gate all three reboot paths on the dpkg lock |
| `6168c58` | test: check every execReboot reference and the permit minting by AST |
| (docs) | docs: blockers file and implementation report |

| File | Action | Notes |
|---|---|---|
| `internal/models/command_policy.go` | MODIFY | `DpkgLockWaitBound` (10m), `RebootAgentHardMax` (12m), `RebootExecDeadline` (20m); `reboot` branch in `AgentHardMax` and `ExecDeadline`; doc comments. `QueuedTTL` and `shutdown` unchanged |
| `internal/models/command_policy_test.go` | MODIFY | `TestCL_AC029_ExecDeadlineTable` reboot row 20m; `RD_AC017` and `RD_AC020` budget tests |
| `internal/server/commands/service_test.go` | MODIFY | `TestCL_AC029_ExpiresAtPerActionClass` reboot `exec_deadline_s` 600 to 1200 (TTL stays 15m) |
| `internal/server/commands/sweeper_test.go` | MODIFY | `TestSweep_RD_AC017_RebootNotTimedOutBefore20m` (4 sub-tests, includes shutdown still 10m) |
| `internal/agent/dpkglock.go` | CREATE | `fileID`, `procLock`, `dpkgLockStatus` + `holderText`, `dpkgProbeEnv`, `probeDpkgLock`, `parseProcLocksLine`, `lockMatches`, `parseNSpid`, `parseStatusUID`, `sanitizeHolderName`, `decodeLinuxDev`, `(a *Agent) probeDpkg`, constants |
| `internal/agent/dpkglock_linux.go` | CREATE | `statFileID` (`syscall.Stat_t`), `platformDpkgProbe` with the real paths |
| `internal/agent/dpkglock_other.go` | CREATE | `//go:build !linux`, `platformDpkgProbe` returns `Absent` |
| `internal/agent/reboot_clearance.go` | CREATE | `awaitRebootClearance`, `commonRebootGuards`, `rebootClearance.reason`, `rebootPermit`, `mintRebootPermit`, `errRebootNotCleared`, `abortPostPatchReboot`, `dpkgLockPoll` |
| `internal/agent/commands.go` | MODIFY | `handleReboot(cr)` returns `commandExecResult` (AD-003 table); `rebootSkippedResult`; `executeAction` `case "reboot"`; `maybeRebootAfterPatch` goroutine re-check through `abortPostPatchReboot`, then gate; `execReboot(p rebootPermit)` with the compare-and-swap check |
| `internal/agent/reboot_auth.go` | MODIFY | `rebootAfterDelivery`: gate after the existing checks; skip goes to `abortAuthorizedReboot`; the Info line gains `waited` |
| `internal/agent/agent.go` | MODIFY | seam `dpkgLockProbe`; fields `rebootPermitSeq`, `rebootPermitLive` |
| `internal/agent/command_lifecycle_test.go`, `commands_test.go` | MODIFY | harness defaults `dpkgLockProbe` to `freeDpkgLockProbe` |
| `internal/agent/reboot_callsites_test.go` | MODIFY | helper records one entry per `execReboot` reference; C-8 assertions kept; AD-007 checks 1 to 4; self-tests |
| `internal/agent/dpkglock_test.go` | CREATE | parser, kernel variants, matcher, NSpid, markers, caps, holder and uid, AC-018 AST, C-1 unit-file test |
| `internal/agent/dpkglock_linux_test.go` | CREATE | real `/proc/locks` with `syscall.FcntlFlock` on a temp file, real `self/status`, `statFileID` |
| `internal/agent/dpkglock_other_test.go` | CREATE | `!linux`: platform probe is `Absent` |
| `internal/agent/reboot_clearance_test.go` | CREATE | gate, three-path, permit tests |
| `docs/architecture/REBOOT-DPKG-001-blockers.md` | CREATE | ARCH-QUESTION on the `NSpid` check, note on `_` (see Deviations) |

Explicitly unchanged, confirmed with `git diff --stat 679499e..HEAD`: `go.mod`, `go.sum`, `scripts/`, all migrations, `web/`, `internal/server/commands/sweeper.go` and `service.go`, `internal/server/db`, `internal/agent/doctor.go`, `internal/maintenance/`, `handleShutdown`.

## Security conditions

- **C-1** `probeDpkgLock` returns `Unknown` for a nested pid namespace (`NSpid` with more than one field), for an unreadable or `NSpid`-less `self/status`, and for a dpkg host (`/var/lib/dpkg/status` or `/usr/bin/dpkg`) with both lock files missing. `Absent` only means "not a dpkg host" and reads nothing under `/proc`. The gate turns `Held` then `Absent` into `Unknown`. `TestAgentUnit_RD_C1_NoPrivatePIDs` checks both unit files. **See ARCH-QUESTION 1 below: the `NSpid` check does not detect a namespace that mounts its own `/proc`.**
- **C-2** Holder text carries the uid and marks inode-only matches; exact strings are tested.
- **C-3** `execReboot(rebootPermit)` refuses a zero, used or stale permit with `errRebootNotCleared` before anything runs. The AST test checks every reference, including method values and method expressions, and that a permit is built and minted only inside the gate path.
- **C-4** Documentation item (technical writer): upgrade the server first.
- **C-5** The `/proc/locks` read is capped at the reader (`io.LimitReader(f, 1 MiB + 1)` under a 64 KiB `bufio` reader, 4 KiB line cap). The `ReaderStopsAtCap` test asserts the source served at most 1 MiB + 1 byte (stricter than the ADD's "plus one buffer fill"), and a mutation that removed the limit made it fail. Kernel-variant fixtures cover `ACCESS`, `*NOINODE*`, `FLOCK MSNFS`, `LEASE BREAKING/BREAKER`, `UNKNOWN`, `DELEG`, plain and nested waiters, `OFDLCK` pid -1 and a negative remote pid. The `/proc/<pid>` path is built from the parsed integer only, for pid > 0. Holder names are limited to `[A-Za-z0-9._+:-]`.
- **C-6** Host verification: see "Real-host verification (C-6)".
- **C-7** QA gate tests exist and pass: AC-010, AC-011, AC-012, AC-014, AC-015, AC-016, AC-018, AC-020, the C-8 test, and the C-1 and C-3 tests.

## Test Summary

Go test names carry `RD_ACnnn` or `RD_Cn`; assertion messages start with `[AC-nnn]` or `[C-n]`. All gate tests run on the fake clock with a scripted probe: no real sleep, no real `/proc`, no network. Only `dpkglock_linux_test.go` touches the real `/proc`, and only for its own temp file and `self/status`.

| AC | Test file(s) | Tests | Status |
|---|---|---|---|
| AC-001 | `reboot_clearance_test.go` | `RD_AC001_FreeLockRebootsWithNoSleepOnAllThreePaths` (3 sub), `RD_AC001_FreeRunsGuardsThenFinalProbe` | PASS |
| AC-002 | `dpkglock_test.go`, `dpkglock_linux_test.go`, `reboot_clearance_test.go` | `ProcLocks_RD_AC002_*` (posix line, hex/decimal, block device, anonymous device, kernel variants, waiter), `DecodeLinuxDev_RD_AC002_*`, `RebootClearance_RD_AC002_HeldLockNeverReboots…` (3 sub), Linux `DpkgLock_RD_AC002_RealProcLocksSeesFcntlLock…` | PASS (Linux test compiled, not run here) |
| AC-003 | same | `ProcLocks_RD_AC003_LegacyLockAloneIsHeld`, `DpkgProbe_RD_AC003_FrontendAbsent…`, `RebootClearance_RD_AC003_LegacyHeldWaitsLikeFrontend` | PASS |
| AC-004 | `reboot_clearance_test.go` | `RD_AC004_ReleaseAfter3mRebootsWithin10s` (3 sub), `RD_AC004_PollIntervalAndProbeTimeoutFitBound` | PASS |
| AC-005 | same | `RD_AC005_TimeoutSkipsAndNeverForces` (3), `NoSecondWaitAfterSkip`, `ProbeAtExactBoundHeldSkips`, `ProbeAtExactBoundFreeProceeds` | PASS |
| AC-006 | `reboot_clearance_test.go`, `dpkglock_test.go` | `HandleReboot_RD_AC006_TimeoutResultFailedAndNamesHolder`, `DpkgProbe_RD_AC006_HolderNamedFromCommWithUid`, `…CommPathFromParsedPidOnly`, `RebootClearance_RD_AC006_ReasonTexts` | PASS |
| AC-007 | same | `DpkgProbe_RD_AC007_*` (4), `HandleReboot_RD_AC007_TimeoutReportSaysHolderNotVisible` | PASS |
| AC-008 | `reboot_clearance_test.go` | `PostPatchReboot_RD_AC008_AuthorizedTimeoutOneEventOneTelemetry`, `…UnauthorizedTimeoutOneEventOneTelemetry` | PASS |
| AC-009 | same | `PostPatchReboot_RD_AC009_SkipReleasesHostReservation` (2 sub) | PASS |
| AC-010 | `dpkglock_test.go`, `reboot_clearance_test.go` | `DpkgProbe_RD_AC010_*` (11: stat error, `/proc/locks` missing and denied, nested pid ns, `NSpid` missing, `self/status` unreadable, dpkg markers, marker stat error, bounded cause, malformed line after a match), `ProcLocks_RD_AC010_MalformedLineIsUnknown` (12 sub), `RebootClearance_RD_AC010_HeldThenAbsent…` (3), `…UnknownSkipsImmediately…` (3), `…UnknownMidWaitSkipsAtOnce` | PASS |
| AC-011 | same | `DpkgProbe_RD_AC011_*` (7: over 1 MiB, exactly 1 MiB, reader stops at cap, line over 4 KiB, slow read, fast read, slow holder read), `HandleReboot_RD_AC011_OversizedLockInfoSkipsReboot` | PASS |
| AC-012 | `dpkglock_test.go`, `dpkglock_other_test.go`, `dpkglock_linux_test.go`, `reboot_clearance_test.go` | `DpkgProbe_RD_AC012_NoDpkgNoLockFilesIsAbsentWithoutReadingProc`, `RebootClearance_RD_AC012_AbsentRebootsAsToday`, `PlatformProbe_RD_AC012_NonLinuxIsAbsent` (Windows), `StatFileID_RD_AC012_MissingPathIsNotExist` (Linux) | PASS |
| AC-013 | `reboot_clearance_test.go` | `RD_AC013_AllowRebootOffDuringWaitSkipsNamingGuard` (3), `…MaintenanceStartsDuringWaitSkipsNamingGuard` (3), `…GuardOrder…` | PASS |
| AC-014 | same | `RD_AC014_FinalCheckRetakenKeepsWaitingWithinBound` (2 sub: re-taken to the bound, re-taken then released with guards re-run) | PASS |
| AC-015 | same | `RD_AC015_ShutdownEndsWaitWithin5s`, `…CancelledContextWithShutdownIsAShutdownSkip`, `HandleReboot_RD_AC015_ShutdownResultReasonShutdown`, `PostPatchReboot_RD_AC015_ShutdownSkipLogsWarn` (2) | PASS |
| AC-016 | same | `HandleReboot_RD_AC016_NoRebootAfterSkippedResult`, `RebootClearance_RD_AC016_CommandBudgetExhaustedIsATruthfulSkip` | PASS |
| AC-017 | `reboot_clearance_test.go`, `command_policy_test.go`, `sweeper_test.go`, `service_test.go` | `HandleReboot_RD_AC017_NineMinuteWaitReportsStartedAndWaited`, `CommandPolicy_RD_AC017_RebootBudgetCoversWait`, `Sweep_RD_AC017_RebootNotTimedOutBefore20m`, updated `TestCL_AC029_ExecDeadlineTable` and `TestCL_AC029_ExpiresAtPerActionClass` | PASS |
| AC-018 | `dpkglock_test.go`, `dpkglock_linux_test.go` | `DpkgProbe_RD_AC018_NeverOpensLockFiles`, `DpkgLock_RD_AC018_NoLockingCallsInSource` (AST), Linux `DpkgLock_RD_AC018_ProbeDoesNotDisturbHeldLock` | PASS |
| AC-019 | `reboot_clearance_test.go` | `RD_AC019_FullWaitBoundedOutput`, `…PostPatchSkipProducesOneEventOneTelemetry` (2), `…WaitLogLinesCarryContext` | PASS |
| AC-020 | `reboot_callsites_test.go`, `command_policy_test.go`, `reboot_clearance_test.go` | `RebootAuth_PF_C8_ExecRebootHasExactlyThreeGuardedCallSites` (assertions kept, plus a reference count), `RebootCallSites_RD_AC020_GateClearanceFeedsEveryExecReboot`, `…ShutdownHasNoGate`, `CommandPolicy_RD_AC020_ShutdownBudgetUnchanged`, `HandleReboot_RD_AC020_PreGateAllowRebootOffIsUnchanged` | PASS |
| AC-021 | `dpkglock_test.go`, `reboot_clearance_test.go`, `dpkglock_linux_test.go` | `DpkgProbe_RD_AC021_NoExecNoSudo`, `ProbeDpkg_RD_AC021_SeamDefaultsToPlatformProbe`, `RebootClearance_RD_AC021_GateRunsNoProcessExceptTheReboot`, Linux `DpkgLock_RD_AC021_RealProbeWorksUnprivileged` (skipped as root) | PASS |
| C-1 | `dpkglock_test.go`, `dpkglock_linux_test.go` | `AgentUnit_RD_C1_NoPrivatePIDs`, `ParseNSpid_RD_C1_CountsNamespaceLevels`, `ReadSmallFile_RD_C1_CutNSpidLineIsNeverTrusted`, Linux `DpkgLock_RD_C1_RealSelfStatusIsInitialNamespace` | PASS |
| C-2 | `dpkglock_test.go` | `DpkgProbe_RD_C2_HolderTextCarriesUidAndInodeOnly` (6), `…NonRootHolderUidReported`, `ParseStatusUID_RD_C2_ParsesRealUidOnly` | PASS |
| C-3 | `reboot_clearance_test.go`, `reboot_callsites_test.go` | `ExecReboot_RD_C3_NonClearedPermitStartsNoReboot` (4 sub), `…ClearedPermitStartsExactlyOnce`, `…RefusalRunsNothing`, `RebootCallSites_RD_C3_MethodValueReferenceIsFlagged` (6 sub), `…PermitMintedOnlyByGate` | PASS |
| C-5 | `dpkglock_test.go` | `SanitizeHolderName_RD_C5_CharsetAndLength` (8 sub) plus the C-5 rows under AC-002, AC-006 and AC-011 | PASS |
| C-8 (kept) | `reboot_callsites_test.go` | assertions unchanged (three callers, each `AllowReboot`-guarded) | PASS |

Counts:
- **New tests:** 101 new top-level Go test functions plus their sub-tests. 95 run on Windows; the 6 in `dpkglock_linux_test.go` compile only on Linux (the 1 in `dpkglock_other_test.go` runs on Windows).
- **Go (Windows, `go test -count=1 -v ./...`):** 2,098 top-level and 1,992 sub-tests PASS, 0 FAIL, 48 SKIP (all pre-existing, platform- or Postgres-gated; none is an `RD_` test). The 6 Linux-only tests are not in this count.
- **Web:** 95 files, 1,707 tests passed (unchanged, no web change).
- **Mutation spot checks** (changes made to the source and reverted; each was caught by a named test): ignoring `sawHeld` (`HeldThenAbsent`), dropping the `NSpid` check (`NestedPidNamespaceIsUnknown`), not refusing a dpkg host with no lock files (`DpkgStatusPresent…`, `DpkgBinaryPresent…`), skipping the guards (`FreeRunsGuardsThenFinalProbe`, `AC013`), removing the permit check (all `C3` tests), exact match relaxed to inode-only for every major (`BlockDeviceRequiresExactDevice`), and removing the reader cap (`ReaderStopsAtCap`).

## Test Run Output

### Baseline before any change (`679499e`)

`go test ./...`: 29 packages `ok`, 0 failed.

### 1. New tests only

`go test -count=1 -v ./internal/agent ./internal/models ./internal/server/commands -run 'RD_|PF_C8|AC029_ExecDeadlineTable|AC029_ExpiresAtPerActionClass|DeadlineInvariant'`:

```
102 top-level PASS, 0 FAIL, 153 sub-tests PASS
ok  	github.com/DesyncTheThird/rIOt/internal/agent
ok  	github.com/DesyncTheThird/rIOt/internal/models
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands
```

### 2. Full Go suite, Windows (`go test -count=1 -v ./...`, final state)

```
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer	0.493s
ok  	github.com/DesyncTheThird/rIOt/internal/agent	19.792s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	15.450s
ok  	github.com/DesyncTheThird/rIOt/internal/logbudget	0.222s
ok  	github.com/DesyncTheThird/rIOt/internal/maintenance	0.385s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.265s
ok  	github.com/DesyncTheThird/rIOt/internal/redact	0.267s
ok  	github.com/DesyncTheThird/rIOt/internal/releasekey	0.282s
ok  	github.com/DesyncTheThird/rIOt/internal/resilient	1.103s
ok  	github.com/DesyncTheThird/rIOt/internal/server	2.696s
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	0.275s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	0.289s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	1.505s
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	1.051s
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	0.636s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	9.529s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ingest	0.411s
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	1.145s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	1.051s
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc	1.503s
ok  	github.com/DesyncTheThird/rIOt/internal/server/partition	0.374s
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	1.071s
ok  	github.com/DesyncTheThird/rIOt/internal/server/quota	0.270s
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	0.366s
ok  	github.com/DesyncTheThird/rIOt/internal/server/storage	0.922s
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary	0.407s
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	0.340s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	0.778s
ok  	github.com/DesyncTheThird/rIOt/scripts	0.423s
(29 ok; the 5 packages without test files are not listed)
```

### 3. Linters, builds, web

```
go vet ./...                                   clean (exit 0)
GOOS=linux GOARCH=amd64 go vet ./internal/agent/ ./internal/models/    clean (compiles the Linux-only tests)
go build ./cmd/riot-agent for linux/amd64, linux/386, linux/arm64, linux/armv7, linux/armv6,
  darwin/amd64, darwin/arm64, windows/amd64   all 8 targets ok (the make build-agent-all set)
gofmt (CRLF stripped) on every file changed since 679499e: clean, except internal/agent/commands_test.go,
  which was already not gofmt-clean at the base (a struct alignment in newOSUpdateTestAgent, not reformatted)
cd web && npm run test:run     Test Files 95 passed (95), Tests 1707 passed (1707)
```

DoD greps:

```
$ grep -rn "rebootPermit{" internal/agent --include=*.go | grep -v _test.go
internal/agent/reboot_clearance.go:56:	return rebootPermit{id: id}      <- inside mintRebootPermit
$ grep -n "time.Now()\|time.Sleep" internal/agent/dpkglock*.go internal/agent/reboot_clearance.go
(no output)
$ git diff --stat 679499e..HEAD -- go.mod go.sum scripts web cmd/riot-server/migrations
(no output)
```

## Real-host verification (C-6)

**Pending.** ADD §12 note 11 needs a real host, and this task did not deploy or touch Hawaii. The orchestrator (Clod) will run a cross-compiled probe check on Hawaii (Debian 13, ext4, as the `riot` user) after this work:

1. at rest the probe returns `Free`;
2. `/proc/self/status` `NSpid` has one field;
3. while `/var/lib/dpkg/lock-frontend` is held for 30 s the probe returns `Held` with comm, pid and `uid 0`;
4. the `stat` device and inode equal the `/proc/locks` line.

Only Hawaii's ext4 (device 259:2) has been sampled. **The inode-only (major-0) path is unverified on real hardware.** The anonymous-device fixture in `TestProcLocks_RD_AC002_AnonymousDeviceMatchesOnInodeAndMarksInodeOnly` is synthetic; no managed host with btrfs, ZFS or an overlay root is known. A real major-0 sample should be added as a fixture if one is ever taken. The standalone reboot during a held lock (note 11.3) is Ryan's to trigger.

## Deviations from ADD

None changes the API contract, a security property or a file outside ADD §4 apart from the two docs files. Items 1 and 2 need an architect decision.

1. **ARCH-QUESTION: the `NSpid` check does not detect a private pid namespace with its own `/proc` (C-1 a).** Written up in `docs/architecture/REBOOT-DPKG-001-blockers.md`. The kernel prints `NSpid` from the namespace of the `/proc` mount being read down to the task's own, so an agent in a private pid namespace (`PrivatePIDs=yes`, a container) reading its own `/proc` sees one field, passes the check, and reads a `/proc/locks` that omits the host's holders (fail open). The check does refuse the reverse case (an ancestor's `/proc`). Implemented as written; latent today because no shipped unit sets `PrivatePIDs=` and `TestAgentUnit_RD_C1_NoPrivatePIDs` guards that. Suggested fix: also require `readlink /proc/self/ns/pid` to equal `pid:[4026531836]` (the kernel constant for the initial namespace), which needs no privilege. It adds one function to `dpkgProbeEnv`.
2. **ADD internal inconsistency on `_` in holder names.** AD-001, §10 and the security review list `[A-Za-z0-9._+:-]` (underscore kept); the §8 C-5 test row lists `_` among characters mapped to `?`. Implemented the charset (underscore kept) and the test asserts it. A one-line change if `_` should go.
3. **Commit order.** ADD §12 note 1 lists the probe and the gate as separate commits. They are one commit (`fd74818`) because `TestDpkgLock_RD_AC018_NoLockingCallsInSource` scans `reboot_clearance.go` too, so the probe alone would not have a green suite.
4. **`readSmallFile` cuts a capped read back to its last complete line.** The ADD caps the `self/status` read at 8 KiB. Without the cut, a status text cut inside the `NSpid:` line (for example after `NSpid:\t4242\t`) would read as one level. Covered by `ReadSmallFile_RD_C1_CutNSpidLineIsNeverTrusted`.
5. **Strictness choices the ADD leaves open:** a blank `/proc/locks` line is unrecognized (`Unknown`); the gate's `switch` has a `default` that fails closed (`Unknown`) for a state value outside the four defined.
6. **Stricter reader-cap assertion.** `ReaderStopsAtCap` asserts at most 1 MiB + 1 byte served (the ADD allows one extra buffer fill), so it detects a missing `io.LimitReader`.
7. **Extra tests beyond the ADD §8 list:** waiter lines do not hold the lock, large `decodeLinuxDev` values, `ParseNSpid`, `ParseStatusUID`, `RD_AC006_ReasonTexts`, guard order, command-budget-exhausted skip, cancelled-context-during-shutdown skip, wait log context, the pre-gate `allow_reboot` text, and `ExecReboot_RD_C3_RefusalRunsNothing`.
8. **`TestRebootClearance_RD_AC004_PollIntervalAndProbeTimeoutFitBound`** assumes the guards' `systemctl show` is capped at 5 s (ADD AD-002). That cap is stated in the ADD, not asserted against the maintenance client.

## Notes for QA

- **Linux-only tests were compiled, not run.** `dpkglock_linux_test.go` (real `/proc/locks`, `FcntlFlock`, `self/status`) cannot run on this Windows box. `GOOS=linux go vet` compiles it. It needs a Linux run (CI, or Docker `golang:1.24`). The race detector was not run either (no CGO toolchain here); the CI `go test -race` run is the check. Shared test state is guarded by a mutex or touched by one goroutine, and post-patch results are read only after the goroutine's last act (the reservation release).
- **The sandbox's own pid namespace.** `TestDpkgLock_RD_C1_RealSelfStatusIsInitialNamespace` skips with a logged reason when `NSpid` shows more than one level. Per ARCH-QUESTION 1, a Docker run of the real-`/proc` tests reads one level inside its own namespace, so those tests still run there.
- **A zero-value `dpkgLockStatus{}` is `Absent`** (the ADD fixes the state order `Absent, Free, Held, Unknown`), and `Absent` lets a reboot proceed. Only the probe and the test seam produce statuses today, and every test harness sets a probe. Worth remembering if a new caller ever builds a status by hand.
- **Edge cases worth probing on a real host:**
  - an `apt-get install` that holds `lock-frontend` for several minutes (the gate should log one wait-started and one wait-ended Info line and nothing per check);
  - `rm /var/lib/dpkg/lock*` during a wait (immediate skip: `dpkg is installed but its lock files are missing or hidden`);
  - a non-root process holding byte-range locks on a file it owns (the report names its uid);
  - an agent updated before its server (ADD R-5: the old server's 600 s reboot deadline).
- **Test data setup:** none beyond temp dirs. Probe tests use in-memory `/proc` fixtures (`fakeDpkgHost`); gate tests use `lifecycleHarness`, `fakeClock` and a scripted probe.
- **Documentation items for the technical writer** are listed in ADD §4 (README, LearnMore, CHANGELOG with the server-first upgrade note, the local-user veto note, the residual window).
- **Environment artifacts created outside the repo:** `D:\rIOt_gocache` (Go build cache), `D:\rIOt_fuzz_tmp` (logs, temp scripts, commit message files) and `D:\rIOt_npm_cache`. All are safe to remove.
