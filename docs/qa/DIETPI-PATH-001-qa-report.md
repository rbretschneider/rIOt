# QA Report

**Story ID:** DIETPI-PATH-001 (bugfix, EXTRA-UPD-001 follow-up)
**QA Engineer:** QA Agent (Opus 5.5)
**Date:** 2026-10-26
**Branch:** `fix/dietpi-run-dir`, base main `c55e4bd`, reviewed at `87c9092` (includes the coordinator's AD-5 fix that drops an unknown skip reason and keeps the event; this reverses implementation deviation #7)
**Verdict:** PASS WITH NOTES

No production code was changed by QA. No production defect was found. Four test gaps (found by mutation) and one missing FIFO case were closed with tests added by QA (see "Tests Added by QA"). Nothing was run on a host: no `dietpi-update`, apt upgrade, reboot, shutdown or installed `riot-agent`, and no SSH.

---

## Test Run Summary

All Go commands ran with `GOCACHE=D:\rIOt_gocache` and `GOTMPDIR=D:\rIOt_fuzz_tmp`. Container runs used `MSYS_NO_PATHCONV=1`, the worktree and `$(go env GOMODCACHE)` mounted read-only, `--network none` (except the installer fixtures, which need apt), `golang:1.24`.

| # | Run | Result |
|---|-----|--------|
| 1 | Windows `go build ./...` | OK |
| 1 | Windows `go vet ./...` | OK |
| 1 | `GOOS=linux go vet ./...` | OK |
| 1 | Windows `go test -count=1 ./...` (with QA tests) | exit 0, 31 packages ok, 0 failing. Packages: `cmd/riot-signer`, `internal/agent` 26.5s, `agent/collectors`, `extraupd`, `logbudget`, `maintenance`, `models`, `redact`, `releasekey`, `resilient`, `server` and 19 `server/*` packages (`handlers` 11.5s), `scripts` 57.7s |
| 2 | Linux race, non-root `--user 1001:1001`, `go test -race -count=1 ./...` | Run twice (before and after the QA tests). Both exit 0, 0 `FAIL`. Final run: every package ok (`internal/agent` 134s, `collectors` 83s, `extraupd` 19.7s, `maintenance` 18.9s, `server/handlers` 35.3s, `scripts` 6.1s, `cmd/riot-agent-apply`, `cmd/riot-maintenance`, `cmd/riot-signer`) |
| 2 | Maintenance boundary as root, `go test -race -count=1 ./internal/maintenance ./cmd/riot-agent-apply ./cmd/riot-maintenance` | Run twice, both ok (final: `maintenance` 29.3s, `riot-agent-apply` 7.1s, `riot-maintenance` 1.1s) |
| 3 | Opt-in real namespace: `RIOT_NS_FIXTURE=1`, `--network none --cap-add SYS_ADMIN --security-opt seccomp=unconfined --security-opt apparmor=unconfined`, `go test -v -run TestRunExtra_EU_OPS001_RealNamespace ./internal/maintenance` | PASS, 6 of 6 subtests, 0 skipped |
| 4 | Installer fixtures as in `ci.yml` (`RIOT_INSTALLER_FIXTURE=1 python3 scripts/test_install_maintenance.py -v`, apt-get installed python3 openssl sudo systemd ca-certificates), CRLF stripped on a private `git archive` copy | `debian:bookworm-slim`: Ran 68 tests, OK. `debian:trixie-slim`: Ran 68 tests, OK |
| 5 | Web: `npx tsc --noEmit` | exit 0, no output |
| 5 | Web: `npm run test:run` | 111 files, 1886 tests passed |
| 5 | Web: `npm run build` | exit 0 (only the existing chunk-size warning) |

`-run DP_` counts:
- Windows: 243 pass, 0 fail, 9 skip. The 9 are Linux-only ownership, mode, link-count and real-directory checks (`requireLinuxOwnership`).
- Linux non-root: 279 pass, 0 fail, 0 skip. So the Linux-only checks really ran.

- **Coverage percentage:** not measured (not part of the CI gate).
- **Flaky tests:** none seen in two full Linux race runs and three Windows runs. The impl report's one-off `TestGROWTH_AC056_NewestThousandLinesBoundedRead` did not reproduce.
- **Not run:** the third CI step of `test-maintenance-boundary` (`scripts/tests/maintenance-apt-fixture.sh`, real apt recovery fixture). This story does not touch it, and it runs apt in a container. `test-go-integration` and the PG-gated tests need a database (they skip without `RIOT_TEST_PG_URL`; not touched by this story).
- `git status` is clean apart from the QA files; no EOL-only rewrites occurred.

### What the real-namespace test proves (item 3)

- **/run/dietpi privacy:** a file written to `/run/dietpi` inside the launcher's namespace is invisible in the parent. The step saw only its own file there, which means an empty private tmpfs. The parent `/run/dietpi` keeps its marker and its mode 0777. This was proved on a real kernel against a 0777 non-sticky `/run/dietpi` on its own `/run` tmpfs mount.
- **/boot propagation:** a tmpfs the step mounted at `/boot/riot-ns-fixture` is visible in the parent namespace. The `MS_REC|MS_PRIVATE` on `/run` therefore did not stop `/boot` changes reaching the host, and the OPS-001 behaviour (a `/` mount made inside reaches the parent) still holds.
- **Not proved:** the PiCUPS-specific peer groups (`shared:1`, `shared:82` for vfat `/boot/firmware`). The container's pid 1 table is not the Pi's. That layout is covered by the fake mount tables in `launcher_rundir_test.go`. The host-side effect on `/run/user/N` and `/run/credentials/*` pinning (security review answer 3) is documented, not tested.

---

## AC Coverage Audit

"Mutation-checked" means QA temporarily broke the production line (and reverted it) and confirmed a test fails. 55 mutations of production lines were run (each reverted). 6 survived: 2 equivalent mutants and 4 real test gaps, now closed (see Test Quality Findings).

| AC | Status | Tests covering it | Gap / note |
|----|--------|-------------------|------------|
| AC-001 | COVERED | `maintenance/extra_trust_dp_test.go:10` `TestTrustWalk_DP_AC001_StockDietPiPasses` (9.20.1 and 10.8.2 with the real `NewDietPi().Apply()` spec; `/run` stays a checked ancestor; spy proves nothing under `/run/dietpi` or below `logs/` is touched); `extraupd/dietpi_test.go` `[DP-AC001]` | Walk uses in-memory `trustFS`; production `osTrustFS.Lstat` now covered on Linux by QA test |
| AC-002 | COVERED | `extra_trust_dp_test.go:79` `_AC002_BootTreeStillStrict` (11 cases) | |
| AC-003 | COVERED | `extra_trust_dp_test.go:108` `_AC003_VarTmpExactPaths` (owner, 0777, 1777 for `/var/tmp/dietpi` and `logs`, symlink, wrong kind, hard link, 0666, absent 9.20.1 fails, absent 10.8.2 passes, 6000-entry and deep mirror passes). Mutation-checked: dropping the mode check or the nlink check fails it | The hard-link case needed a real-kernel link count: QA added `TestOSTrustFS_DP_QA_AC003_...` |
| AC-004 | COVERED | `maintenance/launcher_rundir_test.go:28` `_AC004_PrivateRunDir` (exact ordered effects: `/run` private, tmpfs `nosuid,nodev,noexec` `mode=0755,size=1m,nr_inodes=64`, proof, re-check, then exec); real-kernel `[DP-AC004]` subtests (opt-in, passed). Mutation-checked for each proof element (tmpfs type, uid, dir, mode, empty, host inode, shared), the mount flags and data, and removing the `/run` private mount | |
| AC-005 | COVERED | `launcher_rundir_test.go:105` `_AC005_RunDirFailsClosed` (15 failure cases, all `rundir_not_private`, no exec, no ok line); `extra_linux_test.go:662` `TestGateOutcome_DP_AC005_...` (launcher reasons map to skips); `extra_dp_test.go:88` table row `rundir_not_private` (worker records `skipped (rundir_not_private)`) | |
| AC-006 | COVERED | `launcher_rundir_test.go:224` `_AC006_NoRunDirNoMount` (only the lstat probe touches `/run`) | |
| AC-007 | COVERED | `maintenance/flags_linux_test.go:30` (real dir; symlink flags removed themselves; targets and a directory behind them untouched; `.apt_updates`, `.dietpi_motd`, decoy symlink stay; non-empty directory named like a flag gives a note); `flags_test.go:40` (logic); `extra_dp_test.go:164` (order step, reconcile, post-run read; nothing on failed, timed out, refused, launcher skip, setup failure; note keeps `success`). Mutation-checked: wrong flag set, extra `.apt_updates`, ENOENT as error, reconcile on failure, wrong directory, `AT_REMOVEDIR` | The 200-byte note bound was only checked against the production constant. QA added a literal-200 test |
| AC-008 | COVERED | `flags_linux_test.go:107` (symlinked dir, foreign owner, file in place, absent dir, production wrapper trusts uid 0 only); `flags_test.go:98`. Mutation-checked: dropping `O_NOFOLLOW`, dropping the owner check | |
| AC-009 | COVERED | `extraupd/flags_test.go:27` `TestDetect_DP_AC009_UntrustedFlagIsAbsent` (wrong uid, 0666, 0664, hard link, symlink, directory; both flags; trusted flag still yields its version); `:116` `OSRoot`/`DirFS` trust uid 0 only; `:128` descriptor tests; `:189` guard. Mutation-checked on Linux: drop uid, drop mode, mode o+w only, drop nlink, drop `O_NOFOLLOW`, non-zero owner in `OSRoot` | FIFO case added by QA |
| AC-010 | COVERED | `extra_dp_test.go:17` `TestOutcome_DP_AC010_FailingAndRefused`; `:88` `TestExtras_DP_AC010_RefusalKeepsSuccess` (all 9 non-neutral reasons through the worker rig: `success`, exit 0, first line names the skip; table-completeness subtest; `failed`, launcher setup failure still fail); existing `extra_test.go` timed-out (:250) and interrupted (:283) tests; `agent/extra_refusal_test.go:129`; `protocol_extra_test.go` validate cases. Mutation-checked: `Failing()` for skipped (44 failures), `Failing()` for failed (18 failures) | |
| AC-011 | COVERED | `agent/extra_refusal_test.go:42` (9 reasons: exactly one warning finished event with reason, no start event, no step, `dietpi:end` durable marker, once across a restart, none for neutral skips or `apt_not_successful`, none when the marker cannot be written); `server/handlers/agent_event_refusal_test.go:25,62` (stored, severity forced to warning, message composed server side, unknown, empty, markup, long, newline reasons dropped and event kept, reason on a start event rejected). Mutation-checked: no marker, neutral skip sends, severity, server keeps unknown reason | Consistent with the coordinator's AD-5 fix |
| AC-012 | COVERED | `handlers/patch_badge_refusals_test.go:42` | Follows ADD Rev 2 text (with the install.sh suffix); see Deviations from FRD |
| AC-013 | COVERED | `extra_dp_test.go:257` `TestExtras_DP_AC013_PiCUPSLayoutRuns` (real `trustWalk` over the PiCUPS layout; the step runs; a hostile boot tree is still refused with the named path) | |
| AC-014 | COVERED | `patch_badge_refusals_test.go:82` (each Blocked reason, both Action reasons with their action text, isolation text never promises retry, temporary reasons keep retry text, not pending gives one info line and Auto, neutral skip raises nothing, unreported updater raises nothing, pending but not enabled keeps its not-applied item). Mutation-checked: isolation suffix promising a retry | |
| AC-014a | COVERED | `:172` (`/boot/dietpi/...` gives `needs_you` with the investigate action; `/run/dietpi is writable...` gives `blocked` with the re-run text; `/run/dietpiX` is not the false positive; generic fallback; hostile path token not echoed; counts as "1 item needs you."). Mutation-checked: always-Blocked, wrong prefix | |
| AC-015 | COVERED | `:222` `_AC015_OldWorkerFailedRefusalIsBlocked`: PiCUPS today gives `blocked` with no last-run-failed item; apt failed keeps NY-9; DP-SEC-004 table (hold restore error, verify error, interrupted updater, interrupted apt, another updater failed, unexpected line after the details, no line 2, non-apt first line); `timed_out` keeps NY-9; apt note after the success summary allowed. Mutation-checked: ignore line 2, ignore extra lines, ignore other parts, reinterpret `timed_out` | QA added the `/boot` path variant (old worker, failed, tamper signal gives `needs_you`) |
| AC-016 | COVERED | `:294` (reboot required plus Blocked refusal gives `needs_you`, blocked reason still listed; one item per updater) | QA added a two-updater case |
| AC-017 | COVERED | `web/.../PatchBadge.blocked.test.tsx` (amber never red, tooltip list, text not HTML, empty or missing list, no duplicate keys, `needs_you` beside blocked); `FleetOverview.blocked.test.tsx` ("1 host needs you" excludes blocked, "2 blocked" chip, filters mutually exclusive, no needs-you button when only blocked, no chip when none); `utils/extraUpdaters.test.ts`; `demo-data.extra.test.ts`; Go `handlers/patch_badge_refusals_test.go:407` (JSON kind and always-present `blocked` list) | No server-side kind filters exist, so "filters accept the kind" is only the client chip and filter |

All FRs are covered: FR-001..005 by AC-001..003; FR-006/007 by AC-004..006; FR-008/009 by AC-007/008 and the order test; FR-010/011 by AC-009; FR-012/013 by AC-010; FR-014 by AC-011; FR-015..017 by AC-012, 014..017.

## DP-SEC conditions

| ID | Implemented | Tested (and mutation-checked) |
|----|-------------|-------------------------------|
| DP-SEC-001 | `patch_badge_refusals.go:190` `refusalClassOf`: `unsafe_dietpi_path` is Action except the `/run/dietpi ` prefix (:184); isolation text at :233 does not promise a retry; action "Run riot-agent doctor on this host." (:252) | `AC014`, `AC014a`. Always-Blocked mutation and wrong-prefix mutation both fail |
| DP-SEC-002 | `extraupd/fs.go` `ReadTrustedFlag`: `O_RDONLY\|O_NOFOLLOW\|O_NONBLOCK\|O_CLOEXEC`, `f.Stat()` on the open descriptor, regular, uid, `mode&0o022`, nlink 1 (`fs_linux.go:24`), bounded read; `detect()` uses it for both flags | Hard link (both flags), swapped-in 0666 file, symlink, wrong uid, 0666, 0664, directory, FIFO (QA). Each of nlink, uid, mode, `O_NOFOLLOW`, `O_NONBLOCK`, regular-file check mutation-checked |
| DP-SEC-003 | `launcher.go:224` `isolateRunDir`: only `ENOENT` skips, `StatNoFollow` proof of a root-owned directory before any mount, `reverifyPropagation` (:268) after the mounts and before exec | `SEC003_ProbeAndPropagationFailClosed`, `AC005`. Mutation-checked: any probe error skips, no uid or dir proof, no re-check. Production `linuxOps.StatNoFollow` (lstat) was untested: QA added a real-kernel test |
| DP-SEC-004 | `protocol.go` `Failing()` is outcome keyed; `gateOutcome` maps only the three isolation reasons to skips; `parseRefusals` Sole logic | Launcher `setup_failed`/`launcher_refused` stays failed (`TestGateOutcome_DP_AC005`, `AC010` setup subtest). Sole-cause table. Mutation-checked |
| DP-SEC-005 | `checkExact` rejects 0777 and 1777 even when root-owned; rationale in the `dietpi.go` comment | `AC003` (both modes for `/var/tmp/dietpi` and `logs`). Mutation-checked |
| DP-SEC-006 | `extraupd/testseam.go` holds `DirFSOwnedBy` and `DirFSOwnedByCaller`; guard walks `cmd/` and `internal/` for the substring in non-test Go | Guard fails on a bare reference and on a `DirFSOwnedByCaller` alias in another package (both mutation-checked). The ops-doc sentence about user-namespace mappings is not written yet (technical writer) |
| DP-SEC-007 | One bound, 2048 bytes, `patch_badge_refusals.go:21`; 8 entries; Go regexp; 160-byte detail with control, escape and bidi stripping | 2048 read whole and 2049 never read (literal sizes); entries 8 and 9; QA added literal 159/160/161/1000 for the detail. Bound mutations 2049, 161, 9 all fail |
| DP-SEC-008 | Documentation only | Not code; not yet written (technical writer) |

## Badge scenarios

| Scenario | Result | Evidence |
|----------|--------|----------|
| PiCUPS's current failed run (old worker: `apt: succeeded; dietpi: skipped (unsafe_dietpi_path)`, line 2 success summary, detail `/run/dietpi is writable by group or others (mode 0777)`) | `blocked`, text `DietPi update blocked: /run/dietpi permissions (fixed by this rIOt release; re-run install.sh as root on this host to apply the fix). /run/dietpi is writable by ...`, action "Re-run install.sh as root on this host to apply the fix.", no last-run-failed item, no "will be applied automatically". Server and dashboard only; no new run, no new agent or worker | `AC015` first subtest, `AC012`, `AC014a`. QA also checked the real chain: the agent stores the worker summary as the command `ResultMsg` unchanged (`maintenance.go:218`, `service.go` `fillResultTransition`), `LatestFinishedOSUpdates` selects `result_msg`, and `Status` is `failed`, so the parser sees exactly the field text |
| Not counted in "N hosts need you" | Correct: `isNeedsYou` is true only for `needs_you`; own "N blocked" chip and filter | `FleetOverview.blocked.test.tsx` |
| A `/boot/dietpi/...` path refusal | `needs_you`, action "Investigate the named path on this host (owner and permissions); rIOt will not run DietPi until it is root-only.", counted in "N hosts need you" | `AC014a`; QA added the same for an old worker's failed run |
| Isolation reasons (`tmp_not_private`, `mount_propagation_unverified`, `rundir_not_private`) | `blocked`, "this host's mount layout prevented rIOt from isolating the step", no "retries" or "next patch run" | `AC014` |
| Action item plus a blocked refusal | `needs_you`, blocked reason still listed | `AC016` |
| Failed run where the refusal is not the sole cause | NY-9 kept, no Blocked item | `AC015` table |

## Deviation #2 (install.sh record validator)

Accepted. Evidence:
- `scripts/install.sh:835` now accepts any `skipped` or `success` result when the overall outcome is `success`, and `rundir_not_private` as a skip reason. Without it, the first refusal run under the new worker would write a `success` record carrying `unsafe_dietpi_path`, and the next `install.sh` run would exit "unfinished or invalid maintenance state" (the old assertion allowed only the three neutral reasons). That is exactly the PiCUPS re-run path. PiCUPS's existing record (`outcome: error`, skipped `unsafe_dietpi_path`) was already valid, so the re-run after the old worker's refusal was never blocked either.
- It stays strict. The fixtures (bookworm and trixie, 68 tests each) refuse: success after a failed, timed-out or interrupted updater; success after a failed apt part; an unknown skip reason; a reason on a non-skip; a running entry; unknown keys; nine results; bad or duplicate ids; oversized note or version; version 1 with updater keys; version 3; empty or numeric requested lists. They accept the two new success-with-refusal records.
- Validator and `Record.Validate` agree (`SkipReasons()` includes `rundir_not_private`).

## Growth and bounds

| Bound | Verified by |
|-------|-------------|
| tmpfs 1 MiB, 64 inodes | `runDirTmpfsData` constant test plus the ordered-effects test (mutation to an unbounded mount fails 4 tests) |
| Reconcile: 2 `unlinkat` calls, note at most 200 bytes | Fake-dir test (exactly the two names, in order); QA literal-200 test (the old test used the production constant, so raising it to 2000 survived) |
| Result note at most 256 bytes | `AC007` long-note test (mutation without `boundedText` fails) |
| One event per command and updater | Durable `dietpi:end` marker; once across restart; same agent re-observing; `applyExtraV1Lines` re-planned 3 times sends once; server dedupes a repeated end event |
| `parseRefusals`: 2048 bytes, 8 entries, 160-byte detail | Bounds test, QA 160/161 test; 200,000-repetition hostile input is cheap |
| Trust walk | 3 lstat calls under `/var/tmp`; 6000-entry mirror below `logs/` costs nothing |

---

## Test Quality Findings

Fixed by QA tests:
1. **Circular detail bound.** `TestParseRefusals_DP_Bounds` "the detail is cut to 160 bytes" compared the result with `maxRefusalDetail`, so changing the constant to 161 survived. Fixed with literal 159/160/161/1000.
2. **Circular note bound.** `flags_test.go:40` compared the reconcile note with `maxFlagNote`, so raising it to 2000 survived (the 256-byte record bound hid it). Fixed with a literal-200 test.
3. **Production `osTrustFS.Lstat` link count untested.** The AC-003 hard-link case only used the in-memory FS; hard-coding `Nlink: 1` in production survived. Fixed with a real-kernel test.
4. **Production `linuxOps.StatNoFollow` untested.** Replacing `Lstat` with `Stat` survived, which would let a symlinked `/run/dietpi` pass the DP-SEC-003 probe. Fixed with a real-kernel test.
5. **FIFO flag.** No test for a named pipe in place of a flag (tests the `O_NONBLOCK` and regular-file checks together). Added; both mutations fail it, one by a 10s block.
6. **Weak assertion at the edge:** `TestExtras_DP_AC010_RefusalKeepsSuccess` has a subtest titled "failed, timed out or interrupted still fail the run" that only exercises `failed`. The timed-out and interrupted cases are covered by the existing EU tests (`extra_test.go:250`, `:283`), so the AC is covered; only the title overstates.

Accepted as-is:
- One surviving mutant is equivalent: dropping the symlink check in `checkExact`. A symlink fails the kind check anyway.
- The DP-SEC-002 "swap between calls" test is sequential (probe, swap, read), not a concurrent race. It is sound for what it proves, because `ReadTrustedFlag` has no separate probe and checks the opened descriptor; the descriptor checks are mutation-checked.
- A mutation that keeps a `Reason` on a non-skipped end event survives. It is equivalent, because the message uses the reason only for `skipped`.

## Adversarial Findings

No production defect found. Probes and results:
- **Forged or swapped flags:** wrong uid, 0666, 0664, hard link, symlink, directory, FIFO, swapped-in forged file all read as absent, never as `detection_failed`, never believed.
- **Symlink in `/run/dietpi`:** the flag cleanup removes the link itself and leaves the target (and a directory behind it) untouched. A symlinked or foreign-owned `/run/dietpi` is left alone with a note. The launcher refuses a symlinked, non-directory or non-root `/run/dietpi` before any mount.
- **Hostile result text:** garbage, upper-case ids, over-long ids, 9 entries, markup in the words, leading blank line, CR in id, a 200,000-repetition message: nothing raised, no crash. Control, ANSI and bidi characters are stripped from the detail; a hostile path token is not echoed.
- **Hostile events:** an invented, empty, markup, newline, long or path-like reason is dropped and the event kept with server-composed text; a reason on a start event is rejected; another device's command is rejected.
- **Old-worker laundering:** a failed run is reinterpreted only when apt succeeded, line 2 is the exact success summary, no other updater failed and every later line is an updater detail. A hold-restore error, verify error, interrupted apt or updater, extra line or `timed_out` status keeps the red item.
- **Guard bypass:** adding `DirFSOwnedBy` or `DirFSOwnedByCaller` to a production file in another package fails the guard.
- **Process kill or timeout:** a failed, timed-out, refused or setup-failed step reconciles nothing (tested).
- **Repeat triggering:** a refusal is now `success`, so auto-patch no longer sees a failed command. Auto-patch is still limited by `CooldownMinutes` per device (`auto_update.go`, `cooldown` check around line 560), so a persistent refusal sends at most one warning event per cooldown window; no loop.
- **Residual, by design (documented):** the host's own agent writes the detail text, so a compromised agent can fabricate the `/run/dietpi ` prefix and turn a tamper refusal into Blocked. A compromised agent can already report anything about its own host, which the threat model excludes.

## Tests Added by QA

Commit `[DIETPI-PATH-001] test: ...`, attributed with `// Added by QA Engineer` and the AC they cover.

| File | Test | Covers |
|------|------|--------|
| `internal/extraupd/flags_qa_linux_test.go:18` | `TestReadTrustedFlag_DP_QA_AC009_FIFOFlagIsAbsentAndNeverBlocks` | AC-009, DP-SEC-002 |
| `internal/maintenance/qa_dietpi_test.go:13` | `TestReconcileFlagsLogic_DP_QA_AC007_NoteNeverExceeds200Bytes` (3 subtests) | AC-007, AC-008, growth bound |
| `internal/maintenance/qa_dietpi_linux_test.go:18` | `TestOSTrustFS_DP_QA_AC003_LstatReportsRealLinkCountAndNeverFollows` | AC-003 |
| `internal/maintenance/qa_dietpi_linux_test.go:48` | `TestLinuxOps_DP_QA_SEC003_StatNoFollowAndModeAreReal` | AC-005, DP-SEC-003(b) |
| `internal/server/handlers/patch_badge_refusals_qa_test.go:18` | `TestParseRefusals_DP_QA_DetailBoundIsExactly160Bytes` (4 subtests) | AC-012, FR-015 bound |
| `internal/server/handlers/patch_badge_refusals_qa_test.go:43` | `TestPatchBadge_DP_QA_AC015_OldWorkerFailedBootPathRefusalNeedsYou` | AC-014a, AC-015 |
| `internal/server/handlers/patch_badge_refusals_qa_test.go:58` | `TestPatchBadge_DP_QA_AC016_TwoUpdatersOneBlockedOneActionGiveNeedsYou` | AC-016 |

Each was run green on Windows and on Linux (root and non-root), and each of the five gap-closing tests was re-checked against its mutation and fails when the production line is broken.

## Deviations from ADD

Impl deviations 1 to 12 reviewed. All are acceptable; none contradicts a binding Rev 2 item.
- #1 (non-Linux `ReadTrustedFlag` skips the mode check): fine, production is Linux only and Linux CI runs the checks.
- #2: see above, accepted and necessary.
- #3, #5, #6, #9, #11: harmless seams and exports.
- #4 (propagation regression after the `/run` mounts is reported as `mount_propagation_unverified`): sensible, tested.
- #7 is **superseded** by the coordinator's commit `87c9092` (unknown reason is dropped, event kept). The implementation now matches ADD AD-5. The impl report text for #7 and the renamed test row in "EXTRA-UPD-001 tests changed" are stale; the technical writer should not repeat them.
- #8, #10: fine.
- #12: docs not written yet (see Notes).

No other deviation from the ADD found.

## Deviations from FRD

None in behaviour. Editorial items in the FRD itself (not code):
- FR-015's text nests the Blocked wording ("shows a new warning state Blocked... not counted") under the "Every other `unsafe_dietpi_path` is an Action" bullet, which contradicts the sentence above it. The implementation follows ADD Rev 2 and AC-014a, which are unambiguous.
- AC-012 says the single item reads `DietPi update blocked: /run/dietpi permissions` with the bounded detail. Under AC-014a and ADD Rev 2 the text also carries the "(fixed by this rIOt release; re-run install.sh as root on this host to apply the fix)" suffix. The implementation follows Rev 2, and the test asserts the `HasPrefix` of the AC-012 wording plus the suffix.

## Notes (non-blocking; for follow-up and the technical writer)

1. **Doctor has no isolation probe.** The Blocked action for `tmp_not_private`, `mount_propagation_unverified` and `rundir_not_private` is "Run riot-agent doctor on this host." (`patch_badge_refusals.go:252`). `internal/agent/doctor.go:310` reads presence, opt-in, helper and unit backstop only; it does not check that `/tmp` or `/run` is its own mount or what the propagation is. Ryan would run doctor and see green (as he did before this bug). The wording is mandated by Rev 2, so it is not a defect. Recommend a follow-up: a doctor check of the isolation preconditions, or a different action text. Per project memory, `doctor.go` should be kept in step with what the worker checks.
2. **The Blocked badge persists after the `install.sh` re-run** until the next `os_update` finishes (it is derived from the latest finished run). Say so in the ops doc so Ryan does not expect it to clear at install time. A manual patch run clears it.
3. **Docs not yet updated** (technical writer): `docs/ops/os-maintenance.md:243` still lists `/run/dietpi` among trust-walk paths; the reasons table at :216 has no `rundir_not_private` row; DP-SEC-006 (user-namespace uid mapping unsupported), DP-SEC-008 (tmpfiles `1777`, sysctls, upstream report) and the Q8 downgrade note (archive `current.json`) are not written; CHANGELOG needs the `blocked` kind and `explanation.blocked`, and the FR-012 supersession of FR-038.
4. **`DirFSOwnedBy` links into the production `extraupd` package** (a `testseam.go` file plus a guard test, not a build tag). Allowed by Rev 2 DP-SEC-006, which states a guard test is the condition; the guard is sound. A `_test.go` placement is impossible because other packages' tests use it.
5. **Not testable here:** a real `dietpi-update` run, the Pi `rpi_firmware_migration` path, and PiCUPS's actual peer-group layout (EXTRA-UPD-001 N-4 still applies). The first real run after the `install.sh` re-run is the final proof; Ryan should expect `apt: succeeded; dietpi: succeeded`.
6. **Rollout reminder (AD-6):** the badge fix needs only the server and dashboard. The outcome rule, the `/run/dietpi` isolation and the trust-walk change reach PiCUPS and PiVPN only through a root `install.sh` re-run on each Pi, which installs the new worker and agent together.
7. **Real-world assumption checked:** DietPi's own scripts set `umask 0022` (`dietpi-globals`), so a root-written `.update_available` is mode 0644 and passes the new flag trust (mode, owner, one link). If a future DietPi wrote flags 0664, detection would silently report them absent; the unit tests would not notice, but the AC-013 rig and the field would.

## Verdict Rationale

**PASS WITH NOTES.**
- Every AC (AC-001 to AC-017 and AC-014a) is COVERED by tests that fail when the production behaviour is broken (mutation-checked, 55 mutations).
- All suites are green: Windows, Linux race as non-root, the root maintenance boundary, the real-namespace fixture, both installer fixtures, tsc, 1886 web tests and the web build.
- Every DP-SEC condition is implemented and tested, apart from the documentation items DP-SEC-006 (doc half) and DP-SEC-008, which belong to the technical writer.
- No implementation deviation from the FRD, and no critical or high adversarial finding.
- The notes are non-blocking: doctor does not probe isolation, the docs are pending, and the badge needs a run to clear after the re-run.

## Action Required

None for senior_dev. For the technical writer: item 3 above. For follow-up planning: items 1 and 2.
