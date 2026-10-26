# Implementation Report

**Story ID:** DIETPI-PATH-001 (bugfix, EXTRA-UPD-001 follow-up)
**Engineer:** Senior Dev Agent (Sonnet 5.5)
**Date:** 2026-10-26
**Branch:** `fix/dietpi-run-dir`, from main `c55e4bd`
**Commits:** `0517504` to `42307c1` (8 commits, listed under Completed Components), plus this report's own commit.
**Inputs followed:** FRD FR-001 to FR-017 and AC-001 to AC-017 (with AC-014a); ADD section 0 (Revision 2, binding) and sections 1 to 5; security review DP-SEC-001 to DP-SEC-008.

## Detected stack

| File | Result |
|---|---|
| `go.mod` | Go module `github.com/DesyncTheThird/rIOt`, `go 1.24.0`. Local toolchain go1.26.0 (Windows); CI and the container runs use `golang:1.24`. |
| `web/package.json`, `web/tsconfig.json`, `web/vitest.config.ts` | React 19 + TypeScript + Tailwind, Vite, vitest with Testing Library. |
| `.github/workflows/ci.yml` | `go test -race -count=1 ./...`, root boundary container run of `./internal/maintenance ./cmd/riot-agent-apply ./cmd/riot-maintenance`, installer fixtures on `debian:bookworm-slim` and `debian:trixie-slim`. |
| Database | No schema or migration change. |

Active specialists: Backend (Go agent, worker, server), Frontend (React), Infrastructure (installer record validator only).

## Completed Components

| Commit | Files | Action / notes |
|---|---|---|
| `0517504` | `internal/extraupd/extraupd.go`, `dietpi.go`, `fs.go`, `fs_linux.go`, `fs_other.go`, new `testseam.go` | AD-1 spec: `TrustRoots` is now `/boot/dietpi`, `/boot/dietpi.txt`; new `ExactRoots` (`/var/tmp/dietpi`, `/var/tmp/dietpi/logs`, `dietpi-update_reinstalls`) and `ReconcileFlags`. AD-4 / DP-SEC-002: `FS.ReadTrustedFlag` opens `O_RDONLY\|O_NOFOLLOW\|O_NONBLOCK\|O_CLOEXEC`, then checks the opened descriptor (regular, uid == owner, `mode&022 == 0`, nlink 1) before reading; `detect()` reads `.update_available` and `.live_patches` only through it. `OSRoot()` and `DirFS()` trust uid 0 only. `DirFSOwnedBy` lives in `testseam.go`. |
| `c08f612` | `internal/maintenance/protocol.go`, `extra.go`, new `flags.go`, `flags_linux.go`, `flags_other.go`, `cmd/riot-maintenance/main_linux.go`, `scripts/install.sh`, `scripts/test_install_maintenance.py` | AD-5 worker part: `Failing()` fires only for `failed`, `timed_out`, `interrupted` (and `running`); new `Refused()`, `SkipRunDirNotPrivate`, `SkipReasons()`. AD-3: `reconcileFlags` opens `/run/dietpi` `O_DIRECTORY\|O_NOFOLLOW`, proves uid 0 on the descriptor, `unlinkat(fd, name, 0)` for the two flags, bounded note (200 bytes); called after a successful step and before the post-run detection. Installer record validator accepts the new reason and a success record that carries a refusal. |
| `ddd34c9` | `internal/maintenance/extra_trust.go` | AD-1 worker part: tree walk for the two `/boot` roots; `ExactRoots` are lstat-only (root-owned, not a symlink, right kind, `mode&022 == 0` even when sticky, nlink 1 for the file); `trustStat.Nlink`. Required roots not named in either list keep the old tree walk. |
| `f557b44` | `internal/maintenance/launcher.go`, `nsops.go`, `nsops_linux.go`, `extra_linux.go` | AD-2 with DP-SEC-003: no-follow probe (only not-exist skips), prove root-owned directory, `/run` must be its own mount, `MS_REC\|MS_PRIVATE` on `/run`, tmpfs `mode=0755,size=1m,nr_inodes=64` with `nosuid,nodev,noexec`, `proveRunDirIsolation`, then `verifyPropagation` again, then exec. `nsOps.StatNoFollow`, `FileID.Mode`. Production `trustFS` sets `Nlink`. |
| `27ce5a2` | `internal/models/events.go`, `internal/agent/extra_updaters.go`, `commands.go`, `internal/agent/collectors/updates.go` | AD-5 agent part: `AgentEvent.Reason`; one warning `extra_updater_finished` event (outcome `skipped`, reason) per refusal under the shared durable `<id>:end` marker, no start event, no step; the agent-side `insufficient_time` skip (enabled, pending, no budget stamp) no longer fails the run and raises the same event. |
| `557be30` | `internal/server/handlers/patch_badge.go`, new `patch_badge_refusals.go`, `agent_event_extra.go`, `internal/maintenance/protocol.go`, `worker.go` | AD-5 server part: `parseRefusals` (2048 bytes, 8 entries, Go regexp, 160-byte stripped detail), `blocked` kind and `explanation.blocked` (always present), Blocked and Action classes, FR-016 reinterpretation of an old worker's failed run only when the refusal is the sole cause, server-composed event message, closed reason set. |
| `663b8c9` | `web/src/api/client.ts`, `demo-data.ts`, `components/PatchBadge.tsx`, `pages/FleetOverview.tsx`, `utils/patchFlow.ts` | Amber Blocked badge and tooltip list, "N blocked" chip and filter, "N hosts need you" excludes blocked, types, demo host. |
| `42307c1` | `internal/maintenance/extra_linux.go` and tests | `gateOutcome` extracted so the launcher-reason-to-skip mapping is unit tested. |

New and changed tests are listed in the AC map and in "EXTRA-UPD-001 tests changed" below.

## AC-to-test map

All Go names carry `_DP_`. Subtests are named `[AC-nnn] ...` as the project convention requires.

| AC | Test file | Tests | Status |
|---|---|---|---|
| AC-001 | `internal/maintenance/extra_trust_dp_test.go` | `TestTrustWalk_DP_AC001_StockDietPiPasses` (stock PiCUPS layout for 9.20.1 and 10.8.2; `/run` stays a checked ancestor; the walk never touches `/run/dietpi` or below `logs/`) | pass |
| | `internal/extraupd/dietpi_test.go` | `TestDietPiApply_EU_SEC004_RequiredRootsFollowVersion/[DP-AC001]` (tree roots are exactly the two `/boot` paths) | pass |
| AC-002 | `internal/maintenance/extra_trust_dp_test.go` | `TestTrustWalk_DP_AC002_BootTreeStillStrict` (11 cases) | pass |
| AC-003 | `internal/maintenance/extra_trust_dp_test.go` | `TestTrustWalk_DP_AC003_VarTmpExactPaths` (owner, 0777, 1777, group-writable, symlink, wrong kind, hard link, absent on 9.20.1 fails, absent on 10.8.2 passes, optional paths, huge or deep mirror below `logs/` passes) | pass |
| | `internal/extraupd/dietpi_test.go` | `TestDietPiApply_EU_SEC004.../[DP-AC003]` exact roots, `.../[DP-AC007]` `ReconcileFlags` | pass |
| AC-004 | `internal/maintenance/launcher_rundir_test.go` | `TestLauncher_DP_AC004_PrivateRunDir` (effect order, proof calls, re-check after the mounts, no mount outside `/tmp`, `/run`, `/run/dietpi`) | pass |
| | `internal/maintenance/launcher_linux_test.go` | `TestRunExtra_EU_OPS001_RealNamespace` extended: `[DP-AC004]` a file written to `/run/dietpi` inside stays invisible outside, the host dir keeps its marker and 0777, and a mount made under `/boot` inside still reaches the parent. Opt-in (`RIOT_NS_FIXTURE=1`); run in a disposable container, passed. | pass (opt-in) |
| AC-005 | `internal/maintenance/launcher_rundir_test.go` | `TestLauncher_DP_AC005_RunDirFailsClosed` (15 failure cases, all `rundir_not_private`, no exec, no ok line), `TestLauncher_DP_SEC003_ProbeAndPropagationFailClosed` | pass |
| | `internal/maintenance/extra_linux_test.go` | `TestGateOutcome_DP_AC005_LauncherReasonsMapToSkips` | pass (linux) |
| | `internal/maintenance/extra_dp_test.go` | `TestExtras_DP_AC010_RefusalKeepsSuccess` row `rundir_not_private` (worker records `skipped (rundir_not_private)`) | pass |
| AC-006 | `internal/maintenance/launcher_rundir_test.go` | `TestLauncher_DP_AC006_NoRunDirNoMount` | pass |
| AC-007 | `internal/maintenance/flags_linux_test.go` | `TestReconcileFlags_DP_AC007_RemovesOnlyTwoNeverFollows` (real temp dir; symlink flag removed itself, target and a directory behind it untouched; `.apt_updates`, `.dietpi_motd`, decoy symlink stay) | pass (linux) |
| | `internal/maintenance/flags_test.go` | `TestReconcileFlagsLogic_DP_AC007_OnlyTheTwoFlagsAreUnlinked` (fake dir: missing file is no error, other errors become one bounded note) | pass |
| | `internal/maintenance/extra_dp_test.go` | `TestExtras_DP_AC007_ReconcileAfterSuccessBeforeDetection` (order step, reconcile, post-run read; nothing on failed, timed out, refused; note keeps success; bound) | pass |
| AC-008 | `internal/maintenance/flags_linux_test.go` | `TestReconcileFlags_DP_AC008_UnsafeDirUntouched` (symlinked dir, foreign owner, regular file, absent dir, production wrapper trusts uid 0 only) | pass (linux) |
| | `internal/maintenance/flags_test.go` | `TestReconcileFlagsLogic_DP_AC008_UnsafeDirUntouched` | pass |
| AC-009 | `internal/extraupd/flags_test.go` | `TestDetect_DP_AC009_UntrustedFlagIsAbsent` (wrong uid, 0666, 0664, hard link, symlink, directory; both flags; trusted flag still yields the version), `TestOSRoot_DP_AC009_TrustsOnlyUIDZero`, `TestReadTrustedFlag_DP_SEC002_ChecksTheDescriptor` (swap after a probe, symlink swap, bounds, escaping names) | pass (linux checks skip on Windows) |
| | `internal/extraupd/flags_test.go` | `TestSeam_DP_SEC006_NoProductionReferenceToDirFSOwnedBy`, `TestSeam_DP_SEC006_TheGuardSeesTheDefiningFile` | pass |
| AC-010 | `internal/maintenance/extra_dp_test.go` | `TestExtras_DP_AC010_RefusalKeepsSuccess` (all 9 non-neutral reasons: outcome `success`, exit 0, first line `apt: succeeded; dietpi: skipped (<reason>)`; failed still fails; setup failure still fails; table covers every refusal reason), `TestOutcome_DP_AC010_FailingAndRefused` | pass |
| | `internal/maintenance/protocol_extra_test.go` | `TestRecord_EU_AC028_V2Validation/[DP-AC010]`, `/[DP-SEC-004]` | pass |
| | `internal/agent/extra_refusal_test.go` | `TestOSUpdate_DP_AC010_AgentSideSkipKeepsSuccessAndRaisesEvent` | pass |
| AC-011 | `internal/agent/extra_refusal_test.go` | `TestExtraEvents_DP_AC011_OneRefusalEvent` (9 reasons; durable marker; once across a restart; none for neutral or `apt_not_successful`; no step; marker write failure sends nothing) | pass |
| | `internal/server/handlers/agent_event_refusal_test.go` | `TestAgentEventExtra_DP_AC011_RefusalEventStored`, `TestAgentEventExtra_DP_AC011_UnknownReasonDropped` | pass |
| AC-012 | `internal/server/handlers/patch_badge_refusals_test.go` | `TestPatchBadge_DP_AC012_RefusalIsBlocked` | pass |
| AC-013 | `internal/maintenance/extra_dp_test.go` | `TestExtras_DP_AC013_PiCUPSLayoutRuns` (worker rig with the real `trustWalk` over the PiCUPS layout; the step runs; a hostile boot tree is still refused) | pass |
| AC-014 | `internal/server/handlers/patch_badge_refusals_test.go` | `TestPatchBadge_DP_AC014_ReasonClasses` | pass |
| AC-014a | same | `TestPatchBadge_DP_AC014a_UnsafePathIsAnActionExceptTheStockFalsePositive` | pass |
| AC-015 | same | `TestPatchBadge_DP_AC015_OldWorkerFailedRefusalIsBlocked` (PiCUPS today; apt failed keeps NY-9; DP-SEC-004: hold restore error, verify error, interrupted updater, interrupted apt, another failed updater, unexpected extra line, no line 2, timed out status) | pass |
| AC-016 | same | `TestPatchBadge_DP_AC016_ActionWinsOverBlocked` | pass |
| bounds | same | `TestParseRefusals_DP_Bounds` (2048-byte bound: byte 2049 never read; 160-byte detail; control, escape and bidi stripped; multi-byte safe; 8 entries; garbage table; hostile oversize) | pass |
| code list | `internal/server/handlers/patch_badge_test.go` | `TestPatchBadge_EveryReasonCodeHasATest` extended with `extra_refused` (action) and `extra_blocked` (blocked) | pass |
| AC-017 | `web/src/components/PatchBadge.blocked.test.tsx` | amber label, tooltip list, text not HTML, needs_you beside blocked | pass |
| | `web/src/pages/FleetOverview.blocked.test.tsx` | "N hosts need you" excludes blocked; "N blocked" chip and filter; mutual exclusion; no chip when none | pass |
| | `web/src/utils/extraUpdaters.test.ts` | `isBlocked`, `isNeedsYou('blocked') === false` | pass |
| | `web/src/api/demo-data.extra.test.ts` | one Blocked demo host | pass |
| DP-SEC-001..007 | as above | 001 `AC014`/`AC014a`; 002 `ReadTrustedFlag_DP_SEC002`; 003 launcher `SEC003`; 004 `AC015` table + `AC010` + `TestGateOutcome`; 005 `AC003` (0777 and 1777 for `logs` and `/var/tmp/dietpi`); 006 `TestSeam_DP_SEC006_*`; 007 `ParseRefusals_DP_Bounds` | pass |
| DP-SEC-008 and Q8 | none | ops-doc recommendations only (technical writer) | not code |

Counts: `go test -run DP_` runs 232 passing and 9 skipped on Windows (the 9 are Linux-only ownership, mode, link-count and real-directory checks), and 265 passing and 0 skipped on Linux as a non-root user. Web: 4 new test files or blocks, 1886 tests in total.

## EXTRA-UPD-001 tests changed (they asserted the old FR-038 behaviour or the old trust roots)

`internal/maintenance/extra_test.go`
- `TestExtras_EU_AC024_InsufficientTimeFails`: refusal now returns no error and `success`.
- `TestExtras_EU_AC026_LockWaitHeldUnknown`: two subtests (held more than 5 minutes; unknown lock state): `dpkg_locked` now keeps `success`. Second subtest renamed "leaving the run successful".
- `TestExtras_EU_SEC001_LauncherErrMapsToSkip`: the `tmp_not_private` and `mount_propagation_unverified` subtests now expect `success`, exit 0. Renamed "is a skip that keeps the run successful". The setup-failure subtest is unchanged (still `failed`, DP-SEC-004).
- `TestExtras_EU_SEC001_BusyAfterWaitSkips`, `TestExtras_EU_SEC002_Effective35minRefused` (3 subtests), `TestExtras_EU_OPS001_UnitSandboxRefused` (4), `TestExtras_EU_SEC003_NonMichaIngOwnerRefused` (5), `TestExtras_EU_SEC004_ForgedFlagNotBelieved`, `TestExtras_EU_SEC006_MinInterval` (2): expect no error and `success`.
- Fixture FS: `extraupd.DirFS` replaced by `extraupd.DirFSOwnedByCaller` (also in `extra_helpers_test.go`).

`internal/maintenance/protocol_extra_test.go`: `TestRecord_EU_AC028_V2Validation`: the two "rejects success with dpkg_locked / insufficient_time" cases are replaced by "every refusal keeps a success record valid", a "launcher setup failure still rejects a success record" case and "success with an interrupted updater" rejection.

`internal/maintenance/extra_trust_test.go`: `TestTrustWalk_EU_SEC004_Layouts`: the three subtests "a non-root /run/dietpi fails", "a non-root flag file under /run/dietpi fails" and "a non-root entry in /var/tmp/dietpi fails" became "[DP-AC001]" subtests that pass (those paths are no longer walked). The "exactly 4096 entries" count now counts only `/boot/dietpi*`. The in-memory fixture gained `Nlink`.

`internal/extraupd/dietpi_test.go`: `TestDietPiApply_EU_SEC004`: the "trust roots cover ... /run/dietpi, /var/tmp/dietpi" subtest became the DP-AC001, DP-AC003 and DP-AC007 spec subtests. `TestDietPiDetect_EU_AC006`: "an unreadable update flag is detection_failed" now uses an oversized (65 KiB) trusted flag, because a directory in place of the flag is now an untrusted flag, which reads as absent (AD-4). `fixture_test.go`: `fs()` uses `DirFSOwnedByCaller`; `countingFS` and `blockingFS` implement `ReadTrustedFlag`.

`internal/agent/extra_updaters_test.go`
- `TestOSUpdate_EU_AC022_ExcludedAndNotEnabledLines` "enabled, no stamp": now expects `success`.
- `TestPlanExtras_EU_AC037_V2OnlyWhenEnabledStampedAndNotExcluded` "without the server's budget stamp ... fails the run": now "is skipped without failing the run", status success, exit code 0.
- Calls of `applyExtraV1Lines` pass the command id; `DirFS` replaced by `DirFSOwnedByCaller`.

`internal/agent/collectors/updates_extra_test.go`: uses the new `extraFS` seam with `DirFSOwnedByCaller`.

`internal/server/handlers/extra_updaters_server_test.go`: the bad-event key "skipped is never an end" renamed "skipped without a known reason is never an end" (the case still returns 400).

`internal/server/handlers/patch_badge_test.go`: `TestPatchBadge_EveryReasonCodeHasATest` extended for the two new codes.

`scripts/test_install_maintenance.py`: `test_EU_SEC007_current_json_v2_closed`: two new valid records (a success record with `unsafe_dietpi_path`, and with `rundir_not_private`), two new refused ones (success after a timed out or interrupted updater).

`web/src/utils/extraUpdaters.test.ts`: "only needs_you counts" now also lists `blocked`.

## Test Run Output

Commands run from the worktree. `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp` for every Go command on the PC.

**Build and vet**

```
go build ./...                  BUILD-OK
go vet ./...                    VET-OK
GOOS=linux go vet ./...         LINUX-VET-OK
```

**Windows: `go test -count=1 ./...`** (all packages with tests):

```
ok  cmd/riot-signer 1.077s
ok  internal/agent 24.898s
ok  internal/agent/collectors 12.476s
ok  internal/extraupd 2.768s
ok  internal/logbudget 0.585s
ok  internal/maintenance 2.521s
ok  internal/models 0.749s
ok  internal/redact 0.561s
ok  internal/releasekey 0.626s
ok  internal/resilient 1.340s
ok  internal/server 3.151s
ok  internal/server/{auth,ca,commands,db,events,expect,handlers,ingest,middleware,notify,oidc,partition,probes,quota,scoring,storage,summary,updates,websocket}  all ok (handlers 9.915s)
ok  scripts 54.284s
```

**Linux race as a non-root user, like CI** (`docker run --rm --network none --user 1001:1001 -e HOME=/tmp -e GOCACHE=/tmp/gocache`, worktree and Go mod cache mounted read-only, `GOPROXY=off GOSUMDB=off GOFLAGS=-buildvcs=false golang:1.24 go test -race -count=1 ./...`):

```
ok  cmd/riot-agent-apply 1.329s
ok  cmd/riot-maintenance 1.037s
ok  cmd/riot-signer 1.043s
ok  internal/agent 132.246s
ok  internal/agent/collectors 82.776s
ok  internal/extraupd 29.570s
ok  internal/logbudget 1.015s
ok  internal/maintenance 14.929s
ok  internal/models 1.086s
ok  internal/redact 1.043s
ok  internal/releasekey 1.074s
ok  internal/resilient 1.108s
ok  internal/server 5.676s
ok  internal/server/... (19 packages)  all ok (handlers 34.627s)
ok  scripts 8.630s
```

**Maintenance boundary as root** (same container form, `--user 0:0`, `go test -race -count=1 ./internal/maintenance ./cmd/riot-agent-apply ./cmd/riot-maintenance`):

```
ok  internal/maintenance 21.320s
ok  cmd/riot-agent-apply 9.129s
ok  cmd/riot-maintenance 1.025s
```

**Opt-in real mount namespace** (`RIOT_NS_FIXTURE=1`, disposable container with `--cap-add SYS_ADMIN --security-opt seccomp=unconfined --security-opt apparmor=unconfined --network none`): `TestRunExtra_EU_OPS001_RealNamespace` PASS, 6 of 6 subtests, none skipped (including the three new `[DP-AC004]` ones).

**Installer fixtures** (`scripts/test_install_maintenance.py`, run as in `ci.yml`, with the CRLF checkout stripped on a private copy inside the container): `debian:bookworm-slim` Ran 68 tests OK; `debian:trixie-slim` Ran 68 tests OK. `install.sh` was edited only in the Python record validator; no awk was touched.

**Web** (`cd web`): `npx tsc --noEmit` clean; `npm run test:run`: 111 files, 1886 tests, all passed; `npm run build` OK.

**Lint:** `go vet` for both OS targets is the project's static check; `gofmt -l` on every Go file changed since `c55e4bd` reports nothing.

One note on stability: in the first full Linux race run, `TestGROWTH_AC056_NewestThousandLinesBoundedRead` (`internal/agent/collectors`, a docker-log scan-bound test, unrelated to this story) failed once with a timing-dependent parse error. It passed alone and in two further full runs; this report's run above is green.

## Deviations from ADD

These trigger architect review.

1. **Non-Linux `ReadTrustedFlag` skips the mode check as well as uid and nlink.** ADD (DP-SEC-002) says only uid and nlink are skipped. Go reports synthetic mode bits on Windows (0666 for any writable file), so a mode check would make every Windows fixture flag untrusted. Production is Linux only. The mode, uid, nlink, symlink and swap tests run on Linux and are skipped on Windows (they ran, unskipped, in the Docker runs).
2. **`scripts/install.sh` and `scripts/test_install_maintenance.py` are changed (not in ADD section 4).** The installer validates the previous `current.json` before replacing binaries. It rejected the new reason `rundir_not_private` and rejected any success record with a refusal, so after one refusal run `install.sh` would have refused to run, and the fix reaches PiCUPS only through an `install.sh` re-run. The validator now accepts the new reason and accepts `success` / `skipped` results for a success record; failed, timed out and interrupted still reject it.
3. **`maintenance.SuccessSummary` exported (`protocol.go`; `worker.go` now aliases it).** The server's FR-016 check must compare line 2 of a stored result to the worker's success summary (DP-SEC-004). `worker.go` is not in ADD section 4.
4. **A propagation regression found by the re-check after the `/run` mounts is reported as `mount_propagation_unverified`,** not `rundir_not_private` (the ADD names no reason for DP-SEC-003(c)). Every failure of the `/run` steps themselves, including the probe, is `rundir_not_private`.
5. **Test-only additions outside ADD section 4:** `DirFSOwnedByCaller` (in `testseam.go`, wraps `DirFSOwnedBy` with the caller's uid; the guard test covers it too), and `UpdatesCollector.extraFS` (a nil-by-default field in `internal/agent/collectors/updates.go`; the collector tests need a view that trusts the test user's flags, and production code may not reference the seam).
6. **`gateOutcome` extracted from `ExtraLaunchRunner.RunStep`** so the mapping of launcher reasons to skips has a unit test. No behaviour change. The production `trustFS` is in `extra_linux.go`, not `io_linux.go` as the ADD table says; `Nlink` was added there.
7. **Server event handling (ADD says "accepts Reason only from the set, otherwise dropped").** A `skipped` end event with an unknown, empty or hostile reason is rejected with 400 and nothing is stored (a message cannot be composed without a known reason). A `Reason` on a non-skipped end event is ignored and never stored; a `Reason` on a start event is rejected.
8. **The refusal event adds no progress step** (`cr.Step`). The ADD does not say. A refused step never started and the command ends `verify` as before.
9. **Required roots not named by `TrustRoots` or `ExactRoots` keep the tree walk** (`unlisted()`), so a future spec cannot silently lose its required-path proof. DietPi has none.
10. **Web: no separate skip-reason word table was added.** The server writes every explanation text (with its words table in `patch_badge_refusals.go`) and the dashboard renders it as React text, so a web table would be dead code. The "N blocked" chip and the "N hosts need you" filter are mutually exclusive when toggled. The Blocked demo host is `k3s-worker-02`.
11. **The server now imports `internal/maintenance`** (for `SkipReasons()` and the reason constants, as the ADD specifies). No new external dependency.
12. **Documentation not written.** `docs/ops/os-maintenance.md`, `README.md` and `CHANGELOG.md` belong to the technical writer. Items for them: DP-SEC-006 (a containerised agent with a user-namespace uid mapping is unsupported for DietPi detection), DP-SEC-008 (tmpfiles `1777` override for `/run/dietpi`, `fs.protected_symlinks=1`, `fs.protected_hardlinks=1`, `fs.protected_regular=2`, upstream report), Q8 (archive `/var/lib/riot-maintenance/current.json` before downgrading below this release), the `blocked` kind and `explanation.blocked` list for API clients (CHANGELOG), the new `rundir_not_private` row beside `tmp_not_private` in the reasons table, and that the older text about a skipped step failing the command is superseded by FR-012.

## Notes for QA

- **Reaching the hosts.** The fix reaches PiCUPS and PiVPN only after one more root `install.sh` re-run per Pi (new worker and agent together). The badge change needs only the server and dashboard: with the new server, PiCUPS's stored `failed` run (`apt: succeeded; dietpi: skipped (unsafe_dietpi_path)` and detail starting `/run/dietpi `) should show a Blocked badge with the "re-run install.sh as root" text, with no "last patch run failed" item. A new worker with an old agent is not a supported pairing (AD-6).
- **Do not run anything on a host.** No test or helper starts `dietpi-update`, apt upgrade, reboot, shutdown, poweroff or the installed `riot-agent`. DietPi fixtures use tripwire stubs; the real-namespace test runs `/bin/sh` only, in a throwaway container.
- **Probe these:**
  - the `/run/dietpi` symlink and hard-link cases of `ReadTrustedFlag`, on Linux as a non-root user (they skip on Windows);
  - an old worker's failed result with each DP-SEC-004 extra cause appended;
  - a detail of exactly 160 and 161 bytes, and a message of exactly 2048 and 2049 bytes;
  - `/run/dietpi` that exists but is not on its own `/run` mount (expected `rundir_not_private`);
  - a host with no `/run/dietpi` (launcher must not mount or create anything).
- **Things I could not test.** The real Pi `rpi_firmware_migration` path and a real `dietpi-update` run, as before (EXTRA-UPD-001 N-4). The opt-in namespace test proves the `/run/dietpi` and `/boot` propagation behaviour on a real kernel in a container, not on DietPi.
- **Environment.**
  - The worktree checks files out with CRLF (autocrlf). Go and web build and test fine; shell and Python fixtures need `\r` stripped inside Linux containers.
  - I converted the touched source files to LF in the working tree. Git normalises line endings on commit, so the diffs contain only real changes.
  - `gofmt -l` flags untouched CRLF files; run it only on changed files.
