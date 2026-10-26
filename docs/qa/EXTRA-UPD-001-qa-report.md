# QA Report

**Story ID:** EXTRA-UPD-001
**QA Engineer:** QA Agent (Claude Sonnet 5.5)
**Date:** 2026-10-26
**Branch / commit under test:** `feat/extra-upd-001` at `5ca5488` (merge base `08cd071`), plus the QA test commit that carries this report
**Verdict:** PASS WITH NOTES

No production code was changed. No FAIL items. One pre-existing defect (out of scope, fixed by PR #38) and four notes, listed below.

---

## Test Run Summary

All runs used `GOCACHE=D:\rIOt_gocache` and `GOTMPDIR=D:\rIOt_fuzz_tmp`. Nothing was run on any host. No `dietpi-update`, apt, dpkg, reboot, shutdown or installed `riot-agent` was executed. The only privileged execution was the opt-in real-namespace launcher test, run once inside a disposable `--network none` container (see below).

| Suite | Command | Result |
|---|---|---|
| Go, Windows | `go test ./...` | 30 packages ok, 0 FAIL, 6 "no test files". EXIT 0 (this run reused the Go build cache for packages whose inputs were unchanged since senior_dev's run; `scripts` and `internal/agent` ran fresh). Story packages re-run uncached with `-count=1` (extraupd, maintenance, models, handlers, commands, events, agent, agent/collectors, cmd): all ok |
| Go vet, Windows | `go vet ./...` | no output, EXIT 0 |
| Go vet, Linux | `GOOS=linux go vet ./...` | no output, EXIT 0 |
| Go, Linux race | `golang:1.24`, `--network none`, source bind-mounted read-only (from `git archive HEAD`), `go test -race -count=1 ./...` | 32 packages ok, 0 FAIL, EXIT 0 (`internal/agent` 150 s, `collectors` 92 s, `maintenance` and `riot-maintenance` included, run as root like the CI boundary job). Second full run on the final tree (with the QA tests) is recorded in "Late addendum" below |
| Real-namespace launcher | same container plus `--cap-add SYS_ADMIN`, seccomp and AppArmor unconfined, `RIOT_NS_FIXTURE=1`, `-run 'TestRunExtra_EU_OPS001_RealNamespace\|TestLauncherFixtureProcess'` | PASS, not skipped. A mount made by the `/bin/sh` fixture under the shared root reached the parent namespace; a file written to `/tmp` by the fixture was not visible in the parent; parent `/tmp` untouched |
| Installer fixtures | `riot-fixture:bookworm`, `riot-fixture:trixie`, `RIOT_INSTALLER_FIXTURE=1 python3 /src/scripts/test_install_maintenance.py -v`, read-only mount | bookworm: 67 tests OK (63.5 s). trixie: 67 tests OK (64.3 s). 0 failures, 0 skips |
| Web tests | `cd web && npm run test:run` | 103 files, 1767 tests passed, 0 failed |
| Web typecheck / build | `npx tsc --noEmit`; `npm run build` | tsc clean; vite build ok (only the existing chunk-size warning) |

Notes on the environment: the worktree's `web/node_modules` junction was missing at the start of QA and was recreated (`mklink /J`, git-ignored, nothing tracked changed).

### Pre-existing failure (not counted against this story)

`TestShutdown_SD_AC030_LateNonZeroExitLogsOneWarn/a_late_refusal` (`internal/agent/shutdown_clearance_test.go:901`, "expected 1 warn line, got 0").

- Independently confirmed on the merge base. QA created a temporary `git worktree add --detach <scratchpad>/base 08cd071`, ran the test in the same container, then removed the worktree (`git worktree list` shows only the story worktree).
- With `-race -count=10 -run <test> ./internal/agent` the test fails on `08cd071` and on HEAD, with identical output. It is repeat-sensitive: single runs (`-count=1`) passed 6 of 6 times on both trees, and it passed inside both full-suite container runs of this QA, so it does not show up in every run. senior_dev's "fails 10 of 10" matches the `-count=10` mode.
- Cause (coordinator): `execShutdown` clears `poweroffRunning` before logging the late-exit WARN, and the test reads the log once. It is a known bug on main, fixed by PR #38 (https://github.com/rbretschneider/rIOt/pull/38). Out of scope for EXTRA-UPD-001. No change made.

### Flaky tests

None attributable to this story. The only repeat-sensitive test is the pre-existing one above.

### Suspicious passes

None. The five positive AC-043 documentation tests and the new QA documentation test skip with an explicit message until the technical writer lands the docs (expected). `TestDocs_EU_SEC008_NoManualCopyOfVarTmpDietpi` runs and passes.

---

## AC Coverage Audit

"Would the tests fail if the AC were violated?" was asked for every row. Test names are given instead of line numbers where one test file holds many subtests; file paths are under the repo root.

| AC | Status | Tests proving it | Gap / note |
|---|---|---|---|
| AC-001 presence | COVERED | `extraupd/dietpi_test.go` `TestDietPiDetect_EU_AC001_PresenceNeedsBothFiles`; `agent/collectors/updates_extra_test.go` `TestUpdatesCollector_EU_AC001_EmptyListWithoutDietPi`; `extraupd/extraupd_test.go` `TestDetectAll_EU_AC001_*` | |
| AC-002 version as data | COVERED | `TestDietPiDetect_EU_AC002_VersionParsedAsDataNoProcess`; `agent/extra_safety_test.go` AST check for no exec | |
| AC-003 invalid version | COVERED | `TestDietPiDetect_EU_AC003_InvalidVersionIsUnknown` | |
| AC-004 update available + major | COVERED | `TestDietPiDetect_EU_AC004_UpdateAvailableMajor` | |
| AC-005 stale or garbage flag | COVERED | `TestDietPiDetect_EU_AC005_StaleAndGarbageFlag` | |
| AC-006 never up to date | COVERED | `TestDietPiDetect_EU_AC006_NeverUpToDateWithoutEvidence`. Row D3 is unreachable in production (ADD AF-1) and is driven through the `Evidence` seam; also `extra_test.go` `runWithFixedStatus` | Accepted ADD flag |
| AC-007 live patches | COVERED | `TestDietPiDetect_EU_AC007_LivePatchesOnly` | |
| AC-008 notices | COVERED | `TestDietPiDetect_EU_AC008_SettingsNotices`; `TestDietPiSource_EU_SEC003_MatchesDietPiSedRule` | |
| AC-009 detection bounds | COVERED | `TestDietPiDetect_EU_AC009_BoundsAndBlockingReader` (1 MiB file, blocking reader, 1 s); `TestDirFS_EU_AC009_ReadsAreBoundedAndRegularOnly` | |
| AC-010 modules missing | COVERED | `collectors` `TestUpdatesCollector_EU_AC010_MissingModulesRebootRequired`, `TestRebootRequiredNow_EU_AC010_SameRule`; `extraupd/reboot_test.go` | |
| AC-011 no new collector | COVERED | `collectors` and `agent` `TestTelemetry_EU_AC011_DietPiEntryWithTodaysCollectorList` | |
| AC-012 agent side alone | COVERED | `agent` `TestOSUpdate_EU_AC012_AgentSideOnlyNeverRunsDietPi` (request v1, runner sees only the `systemctl start`, line `skipped (not_enabled)`) | |
| AC-013 readiness | COVERED | `TestReadiness_EU_AC013_FiveReasons` (all five plus precedence order). QA added `TestPlanExtras_EU_AC037_QA_HelperAnswerDecidesTheRequestVersion` for the old-helper answers | |
| AC-014 unknown ids | COVERED | `agent/config_extra_test.go` `TestLoadConfig_EU_AC014_*`; `extra_updaters_doctor_test.go` `TestDoctorExtra_EU_AC014_UnknownIDWarns` | |
| AC-015 fresh-install question | COVERED | `scripts/test_install_maintenance.py` `test_EU_AC015_fresh_question_dietpi_only` (both images) | |
| AC-016 reinstall fail closed | COVERED | `test_EU_AC016_reinstall_fail_closed_shapes`, `..._transparency_line_...`, `..._shutdown_grant_survives_a_list_valued_key` | |
| AC-017 host and ownership | COVERED | `test_EU_AC017_non_dietpi_records_nothing_and_root_owned`, `..._agreement_holds_when_the_extra_list_changes_under_the_run` | |
| AC-018 revocation | COVERED | `test_EU_AC018_revocation_policy_first_then_35min`, `..._re_adding_...`; agent `TestReadiness_EU_AC018_NotOptedInAfterRevocation` | |
| AC-019 exact command | COVERED | `maintenance` `TestExtras_EU_AC019_ExactArgvEnvNullStdin`, `TestExtraCommandAllowed_EU_AC019_OnlyLauncherArgv`, `TestRunExtra_EU_AC019_OnlyRegistrySpec`, `TestParseRequest_EU_AC019_NoArgvField`; `extraupd` `TestDietPiApply_EU_AC019_FixedSpec`; installer `test_EU_AC019_sudo_lists_no_dietpi_program` | |
| AC-020 order after apt | COVERED | `TestExtras_EU_AC020_AfterVerifyPostRefreshAndHoldRestore` (DietPi last in the call log) | |
| AC-021 repair / apt failure | COVERED | `TestExtras_EU_AC021_NeverInRepairOrAfterAptFailure` (v1 repair, v2 repair refused, failed upgrade, failed hold restore); agent `TestPlanExtras_EU_AC021_RepairNeverPlansAnUpdater` | |
| AC-022 inclusion rules | COVERED | worker `TestExtras_EU_AC022_SkipReasonsKeepSuccess`; agent `TestOSUpdate_EU_AC022_ExcludedAndNotEnabledLines` (excluded_by_operator is agent-side, by design) | |
| AC-023 time limit | COVERED | `TestExtras_EU_AC023_TimeoutIsTimedOut` (limit override and the real 40 min context deadline); real process-group TERM then KILL in `extra_linux_test.go` `TestRunProcessSpec_EU_AC023_GroupTermThenKill` (Linux, ran in container) | |
| AC-024 not enough time | COVERED | `TestExtras_EU_AC024_InsufficientTimeFails` (threshold exact, one second under) | |
| AC-025 nested bounds | COVERED | `models` `TestBounds_EU_AC025_StrictlyNested`; `maintenance` `TestBounds_EU_AC025_WorkerSide`. **Weak as delivered**: each half restated literals (40/80/85 in models, 88 in maintenance), so no test tied the worker constants to the server and agent constants. QA added `TestBounds_EU_AC025_QA_WholeChainFromShippedConstants` (full chain from shipped constants, 40<80<85<88<95, backstop plus grace below 88, 88 plus slack at most 95, unstamped bounds 31m30s / 40 min / 2400 s) | Fixed by QA |
| AC-026 dpkg lock | COVERED | `TestExtras_EU_AC026_LockWaitHeldUnknown` (2 min, over 5 min, unknown); `TestDpkgLockState_EU_AC026_OFDQuery` | |
| AC-027 own failures | COVERED | `TestExtras_EU_AC027_DietPiFailuresNotRetried` (one call per run, exit status, last 20 lines) | |
| AC-028 partial failure | COVERED | `TestExtras_EU_AC028_AptSuccessDietPiFailIsFailed` (first line exactly `apt: succeeded; dietpi: failed (exit 1)`, version changes and holds unchanged) | |
| AC-029 at most once | COVERED | `TestExtras_EU_AC029_RecoverNeverReruns` (persist-before-act, recovery starts nothing, redelivered id refused by the store), `..._PersistenceFailureStartsNothing`. Agent recovery path (`recoverMaintenanceCommand`) contains no request write or plan call | Agent-level restart test absent, structurally safe |
| AC-030 output cap | COVERED | `TestExtras_EU_AC030_OutputCapKeepsTailAndLogPath` (1 MiB, section at most 128 KiB, whole at most 256 KiB, cut marked, error line kept, log path last, apt full output case) | |
| AC-031 no reboot from step | COVERED | `maintenance` `TestExtras_EU_AC031_RecordsRebootRequiredAfter`; agent `TestOSUpdate_EU_AC031_ManualWithoutAuthorizationNeverReboots` | |
| AC-032 reboot through the gate | COVERED | agent `TestOSUpdate_EU_AC032_RebootOnceThroughGate`, `TestMaintenanceResult_EU_AC032_PseudoEntryOnlyUnderEveryCondition` | |
| AC-033 fresh state | COVERED | `TestTelemetry_EU_AC033_FreshStateAfterRun`; worker `version_after` re-read subtest | |
| AC-034 steps and events | COVERED | `TestOSUpdate_EU_AC034_TwoEventsAndSteps`, `TestInflight_EU_AC034_ExtraEventsAreDurableDeduplicatedAndBounded`; server `TestReceiveAgentEvent_EU_AC034_OriginFromCommand`; `TestNoExpectCode_EU_AC034` | |
| AC-035 server bounds | COVERED | `models` `TestExtraUpdaterList_EU_AC035_Bounds`; `handlers` `TestIngest_EU_AC035_NoWarning2xx` (older agent, 12 entries keep 8, bogus status, 500-char version, hostile shapes, no warning) | |
| AC-036 auto-patch trigger | COVERED | `TestCheckAutoPatch_EU_AC036_ExtraOnlyHost` (enabled fires; unknown, root_side_missing, not_opted_in, up_to_date do not; apt-pending unchanged; out of window never) | |
| AC-037 exclusion parameter | COVERED | `TestManualOSUpdate_EU_AC037_ExclusionParam` (true, absent, false, six bad types, case variants, scoped to os_update); `TestDispatch_EU_AC037_BudgetStamp`; agent `TestPlanExtras_EU_AC037_V2OnlyWhenEnabledStampedAndNotExcluded` | |
| AC-038 badge items and panel | COVERED | `TestPatchBadge_EU_AC038_ExtraItems`, `TestPatchStatus_EU_AC038_PanelListsDietPi`, `..._UnknownAndUpToDateAreNotListedOrCarried`; web `FleetOverview.extra.test.tsx` | |
| AC-039 device view and button | COVERED | web `DeviceDetail.extra.test.tsx` ("Patch (3 + DietPi)", "Patch (DietPi)"), `ExtraUpdatersList.test.tsx`, `extraUpdaters.test.ts` | |
| AC-040 dialog | COVERED | `DeviceDetail.extra.test.tsx` (ticked on each open, disclosures, exclusion sent, unknown and not-enabled text), `PatchConfirmOptions.extra.test.tsx` | |
| AC-041 result view and demo | COVERED | `ActivityLog.extra.test.tsx`, `demo-data.extra.test.ts` | |
| AC-042 doctor | COVERED | `TestDoctorExtra_EU_AC042_PassFailNotApplicable`, `..._ProbeTimeoutIsBounded` | |
| AC-043 documentation | NOT YET TESTABLE | `scripts/extra_updaters_docs_test.go` (5 positive tests, plus 1 added by QA) skip until the docs exist. SEC-008 negative check runs and passes | Technical writer. Handoff list below |
| AC-044 unchanged paths | COVERED | `TestExecute_EU_AC044_V1Golden` (byte-pinned, written before the refactor); `TestParseRequest_EU_AC044_V1AndV2StayDistinct`; agent `TestPlanExtras_EU_AC044_...`, `TestTelemetry_EU_AC044_NoUpdaterNoStampNoProbe`; installer `test_EU_AC044_sudoers_golden_unchanged`, `..._non_dietpi_install_leaves_the_same_paths_as_before`; `scripts/riot-maintenance.service`, `cmd/riot-agent-apply/*` have an empty diff | |
| AC-045 safety scan | COVERED | `agent/extra_safety_test.go` three tests; installer `test_EU_AC045_no_dietpi_program_runs_in_any_installer_fixture`; every fixture `dietpi-update` is a 0644 tripwire | |
| AC-046 vermont | COVERED | `TestPatchBadge_EU_AC046_VermontIsManualNotNeedsYou` (kind `manual`, label Manual, no action items, "Nothing.", `auto_patch_off` in info with its text, unattended-upgrades note) | |
| AC-047 | COVERED | `..._AC047_AutoPatchOffWithAptPendingNeedsYou` | |
| AC-048 | COVERED | `..._AC048_OnlyDietPiPending` (a, b, c) | |
| AC-049 | COVERED | `..._AC049_RebootRequiredNothingReboots` | |
| AC-050 fleet count | COVERED | web `FleetOverview.extra.test.tsx` ("1 host needs you", filter, Manual not red), `PatchBadge.extra.test.tsx`, `extraUpdaters.test.ts` "only needs_you counts" | |
| AC-051 | COVERED | `..._AC051_SettingsAloneAreInformation` | |
| AC-052 | COVERED | `..._AC052_HoldsAreActionOnlyWithWaitingWork` (a to f) | |
| AC-053 | COVERED | `..._AC053_ReleaseFailedRunAndUnsupportedHost` (a to e, command link kept) | |
| AC-054 | COVERED | `..._AC054_StaleListsAreNotWaitingWork` | |
| AC-055 | COVERED | `..._AC055_EachCodeOnceAndOtherLinesUnchanged` (frozen pre-story logic for the Automatic, Reboot and As-of lines), `..._AC055_DisplayOnlyNeverTouchesDispatch`; existing `checkAutoPatch` tests unchanged in expected results | |

Result: 54 of 55 ACs COVERED. AC-043 is unverifiable until the documentation lands; it is a pipeline ordering, not a gap. Zero MISSING.

### SEC / OPS rows of ADD section 8

| Row | Status | Where |
|---|---|---|
| SEC-001 launcher isolation, busy scan | COVERED | `launcher_test.go` `PrivateTmpSequence` (exact call order, no `/` propagation change, no bind mount, raw `CLONE_NEWNS`, ok only after both proofs), `TmpNotMountPointRefused`, `SameDevInoRefused`, `NonEmptyOrNonTmpfsRefused`; `extra_test.go` `LauncherErrMapsToSkip`, `BusyAfterWaitSkips`; `extra_linux_test.go` `DietPiBusy_..._ScanMatchesAndBounds`, `RunProcessSpec_EU_SEC001_GateFailureKillsTheGroup`; real namespace test (passed in container) |
| SEC-002 backstop and write order | COVERED | `TestExtras_EU_SEC002_Effective35minRefused`, `TestParseSystemdTimespan_EU_SEC002`, `TestEffectiveUnit_EU_SEC002_RunnerErrorsFailClosed`, agent `TestReadiness_EU_SEC002_*`, installer `test_EU_SEC002_write_order_optin_and_revoke` |
| SEC-003 source gate | COVERED | `TestDietPiSource_EU_SEC003_*`, `TestExtras_EU_SEC003_NonMichaIngOwnerRefused` (CR and trailing space fail), `TestReadiness_EU_SEC003_*`, `TestPatchBadge_EU_SEC003_SourceActionItem` |
| SEC-004 trust walk | COVERED | `TestTrustWalk_EU_SEC004_Layouts`, `TestExtras_EU_SEC004_ForgedFlagNotBelieved`, `TestDietPiApply_EU_SEC004_RequiredRootsFollowVersion` |
| SEC-005 event dedupe | COVERED | `TestReceiveAgentEvent_EU_SEC005_DuplicateIsNoop`, `..._UnknownUpdaterRejected` (see Note N-2 for the limit) |
| SEC-006 minimum interval | COVERED | `TestExtras_EU_SEC006_MinInterval`, `TestStore_EU_SEC006_LatestExtraStart` (see Note N-1) |
| SEC-007 hardening | COVERED | `TestDpkgLockState_EU_SEC007_NonblockAndOwnership`, `TestCapabilities_EU_SEC007_FirstAndNoSideEffects`, `TestCapabilityProbe_EU_SEC007_*`, `TestDispatch_EU_SEC007_BudgetVariantsStrippedAllActions` (4 case variants, 6 actions), `TestManualOSUpdate_EU_SEC007_BudgetStampNeverFromOperator`, `test_EU_SEC007_current_json_v2_closed`, `TestRunExtra_EU_SEC007_RefusesWithoutRootFifoParentToken` |
| SEC-008 no manual copy | COVERED (negative, runs now) | `TestDocs_EU_SEC008_NoManualCopyOfVarTmpDietpi` |
| SEC-009 thread-scoped mountinfo | COVERED | `TestRunExtra_EU_SEC009_MountTableOfTheCallingThread` (path is `/proc/thread-self`, AST check that the launcher source never names `/proc/self`, both reads after the unshare); real-namespace test runs from a locked non-leader thread and passed |
| OPS-001 shared propagation | COVERED | `TestParseMountinfo_EU_OPS001_StrictAndBounded`, `TestRunExtra_EU_OPS001_RequiresSharedPeerOfPid1`, `TestExtras_EU_OPS001_UnitSandboxRefused`, `test_EU_OPS001_dropin_has_no_sandboxing`, real-namespace test |

---

## Test Quality Findings

1. **Behavior over implementation: good.** Worker, badge, server and web tests assert on records, summaries, requests and rendered output. The launcher tests assert the recorded syscall sequence through a fake `nsOps`; that is implementation-shaped by design (ADD AD-007 names the sequence as the contract) and it is backed by the opt-in real-namespace test, which passed.
2. **False-coverage check.** For each of the mutation points in the deliverable, a breaking change would fail a test: dropping the `PrivateTmp`/`MountFlags` check (`UnitSandboxRefused`), accepting `master`-only owner variants (SEC-003 CR/space cases), letting `unknown` fire the scheduler (AC-036), counting information items in the fleet count (AC-050), `v1` growing a fifth key (`V1AndV2StayDistinct`, golden).
3. **AC-025 literals** (weak as delivered). Fixed by the QA test above.
4. **Isolation.** Tests use `t.TempDir()` fixture trees and injected runners; no shared mutable state seen. The repeat-sensitive SD_AC030 test is the only order-sensitive one and it predates the story.
5. **Boundaries checked and present:** exact start threshold and one second under (AC-024), 2 min / over 5 min / unknown lock (AC-026), 12 entries to 8 and 500-char version to 32 (AC-035), 1 MiB output to 128 KiB (AC-030), 256-byte capabilities cap and key order (SEC-007), future-dated record, 30 and 61 minute intervals (SEC-006).
6. **Mocking hygiene.** The worker fakes (`StepRunner`, `Lock`, `Busy`, `Unit`, `Trust`, `LastStart`) are interface seams with Linux counterparts tested separately in `extra_linux_test.go` (OFD lock query, `/proc` scan, real TERM then KILL, launcher status protocol). The agent capability probe fake is checked against the real closed parser. The one remaining drift risk is the pair "installer drop-in value 85min" and `ExtraServiceBackstop`; the installer test pins the drop-in bytes and the Go test pins 85 minutes.
7. **Documentation tests.** The AC-043 tests are phrase checks. They skip (not pass) before the docs land, so a silent green is not possible. QA extended them (see below).

## Adversarial Findings

Probed from the code and the tests; "T" means a test already pins it, "QA" a test added by QA, "D" documentation-only.

| Probe | Result |
|---|---|
| Event dedupe holds for one server process only | Confirmed. `receiveExtraUpdaterEvent` uses an in-process mutex around `HasCommandEvent` then `Create` (`internal/server/handlers/agent_event_extra.go:52-54`), with no DB uniqueness constraint. ADD AD-011 and the security review (SEC-005 closure) state this explicitly and accept it for today's single-process deployment. Not yet in user docs (see N-2). Acceptable. |
| Backward clock jump shortens the 60-minute minimum | Bounded by the other gates but weaker than documented. See N-1. |
| `/boot` to `/boot/firmware` migration and shared propagation | `verifyPropagation` (`internal/maintenance/launcher.go:204-228`) requires `/`, the mount holding `/boot` and `/boot/firmware` (when mounted) to be `shared:N` in the step's namespace and the same `N` in pid 1's table. T: `TestRunExtra_EU_OPS001_RequiresSharedPeerOfPid1` covers master-only, a different peer group, `/boot` not shared, `/boot` peer mismatch, `/` missing from pid 1, `/boot/firmware` mismatch, `/boot/firmware` not shared, unreadable and malformed tables; each returns `mount_propagation_unverified` and asserts that no mount and no exec happened. `tmp_not_private` is enforced for no mount point, same dev/ino, non-tmpfs, non-empty, not root-owned, still shared, and stat failures (`launcher.go:153-156`, `proveIsolation`). A unit-level sandbox is refused too (`TestExtras_EU_OPS001_UnitSandboxRefused`). The real kernel confirmed propagation in the disposable container. What no test can prove is the real Pi `rpi_firmware_migration` on PiVPN (see N-4). |
| v2 request only when `--capabilities` supports it; v1 stays four fields | T both ways: v1 marshals to exactly four keys, golden bytes pinned, v2 round trips, strict parser rejects v1 with a list, v2 without a list, 9 ids and key variants. QA added a plan-level test that six wrong helper answers (v1-only, no updater, unknown updater, swapped keys, extra line, old-helper error text) and a missing probe all leave the request at version 1, while the same host with a good answer sends v2. |
| Bounds 40 < 80 < 85 < 88 < 95, only when the budget is stamped | T and QA. Unstamped: `TestDispatch_EU_AC037_BudgetStamp` (seven non-enabled readiness values, no telemetry, telemetry error, repair, exclusion keep 2400 s); agent `newCmdRun` extends only for a boolean `true` stamp (`command_lifecycle.go:355-360`); the string `"true"` does not (`TestPlanExtras_EU_AC037_...`). Client-supplied stamps, in 4 case variants and for all 6 actions, are stripped before marshal. |
| Output at most 128 KiB inside 256 KiB; at most 8 telemetry entries | T: `TestExtras_EU_AC030_*` (also with 300 KB of apt output), `TestExtraUpdaterList_EU_AC035_Bounds`, `TestIngest_EU_AC035_NoWarning2xx`. |
| Deadline single source | Confirmed. `Command.ExecDeadlineS` is set only at `internal/server/commands/service.go:221` through `execDeadlineSeconds` (`service.go:307-312`: 95 min when stamped, else `models.ExecDeadlineSeconds`). It is persisted by `command_repo.go` insert and read back; `deadline_at` is computed in SQL from `exec_deadline_s` (`command_repo.go:322, 349, 426, 434`); the sweeper message uses the stored value (`sweeper.go:99`). The agent's own 88 min limit is derived separately from the same server stamp (`command_lifecycle.go:355`), not from the stored deadline, which is the intended nesting. No migration and no schema check on the column. `checkAutoPatch` and bulk patch both dispatch through `Dispatch`, so they are extended identically. EXPECT-001 (PR #37, open, not merged, not touched) must read the stored `ExecDeadlineS`, not a constant. |
| Vermont: 0 updates, no reboot, auto-patch off | T: kind `manual`, no action item, "Nothing.", `auto_patch_off` information; the Fleet "N hosts need you" filter keys only on kind `needs_you` (`web/src/pages/FleetOverview.tsx:48-49` through `isNeedsYou`), tested with needs_you/manual/auto giving "1 host needs you". Pending updates with nothing applying them still give `needs_you` (AC-047, AC-048 a and b, AC-051, AC-053e). |
| `checkAutoPatch` for DietPi-only hosts | T: fires only for `update_available` plus `enabled` with zero apt updates (`internal/server/handlers/auto_update.go:546, 603-614`); does not fire for unknown, `root_side_missing`, `not_opted_in` or `up_to_date`; apt-pending hosts unchanged. Matches FR-048 and FR-049. |
| Failure paths that could grow disk, DB or logs | None found unbounded. Telemetry 8 entries and bounded strings; record at most 8 results and notes of 256 bytes; output 128 KiB and 256 KiB; events at most 2 per updater per command on the agent (durable marker, 16 slots) and one start and one end per command on the server; one tmpfs per step, freed with the namespace; one policy file of at most 512 bytes and one drop-in, backups counted in `test_EU_AC029_opt_in_growth_is_bounded`; agent logs one warning per config load and one line per step; the server logs nothing on hostile telemetry (AC-035 asserts no warning). A persistently failing DietPi creates at most one failed command per scheduler window (FRD NFR-002); the worker itself refuses a second start inside 60 minutes. |
| Input attacks (injection, long, unicode, control bytes) | Versions and ids go through the lenient decoder (pattern, length, enum), then `stripUnsafeText` before badge text; the web renders through React text nodes. Launcher status lines are one line and 200 bytes (`TestRunExtra_EU_SEC007_StatusLinesAreBoundedAndOneLine`). T. |
| Auth and state attacks | A compromised agent cannot supply argv, env or path (`TestParseRequest_EU_AC019_NoArgvField`); cannot start the launcher (root, FIFO fd 3, parent exe and token checks, all T); cannot post events for another device's or a non-`os_update` command (T); cannot repeat a start (`Begin`, persist-before-act, 60 minute rule). |
| Reboot | The step never reboots; the `dietpi` pseudo-entry reaches `RebootClassApplied` only when include-reboot-class, overall success, updater success and reboot-required-after all hold (T). |

## Tests Added by QA

| File | Test | Covers |
|---|---|---|
| `internal/agent/extra_updaters_qa_test.go` | `TestPlanExtras_EU_AC037_QA_HelperAnswerDecidesTheRequestVersion` (8 subtests) | AC-013, AC-037, AC-044, NFR-005: v1 request exactly four fields for six wrong helper answers and a missing probe, v2 only for the good answer |
| `internal/maintenance/extra_qa_test.go` | `TestBounds_EU_AC025_QA_WholeChainFromShippedConstants` (5 subtests) | AC-025, AC-044, FR-032: whole nested chain built from the shipped constants of both packages, ADD AD-006 values pinned, unstamped bounds unchanged |
| `scripts/extra_updaters_docs_test.go` | `TestDocs_EU_AC043_QA_ResidualsAndAcceptedLimits` (skips until docs land) | AC-043: `/var/tmp/dietpi` trust rule, 06:25 cron advice, upstream note, 60 minutes, clock-jump limit, single-process dedupe limit, 90-minute window, `/boot/firmware` |

All three pass (the first two on Windows; Linux result in the addendum). Each carries an "Added by QA Engineer" comment. Committed as `[EXTRA-UPD-001] test: QA additions ...`.

## Notes (non-blocking)

**N-1 (Low-Medium). A backward clock jump disables the 60-minute interval for as long as the jump lasts, not once.**
- Where: `internal/maintenance/extra.go:259-267` (`case last.After(now): // ignore`) and `internal/maintenance/io_linux.go:422-455` (`LatestExtraStart` returns the maximum recorded start, including future-dated ones).
- Why: if a record is dated T0 and the clock then steps back by D, the maximum record stays T0, which is in the future, so the check is skipped. A new start at T0-D+e writes a record that is older than T0 and therefore never becomes the maximum. Every later request, until the clock passes T0, finds the same future record and skips the check. The security review's closure text says "can shorten the interval once", which understates this.
- Impact: bounded by the other gates (an update must be pending, the source must be `MichaIng`, the dpkg lock and busy scan, the trust walk, the 80 minute budget, and a successful run clears the pending flag). Realistic only on a Pi that boots with a stale clock before NTP. A compromised `riot` cannot move the clock.
- ADD AD-005 check 8 also says the future record is ignored "with a note"; no note is added (`extra.go:262-263`).
- Suggested fix for a follow-up (not a story blocker): have the store take `now` and return the newest record that is not in the future, so the later records keep protecting the interval. Document the residual meanwhile. QA left it unpinned on purpose (a characterization test would lock in the weaker behaviour); the docs test now requires the docs to state the clock limit.

**N-2 (Low). Event dedupe is per server process and per (device, command, type), not per updater.**
- Where: `internal/server/handlers/agent_event_extra.go:52-54`.
- Single process: accepted in ADD AD-011 and the security review; a multi-replica server would need a DB uniqueness constraint. The docs test now requires the docs to say so.
- Per updater: with only `dietpi` implemented this is exact. When a second updater is added, its start and finish events would be answered `duplicate`. ADD AF-3 says items are unique on (code, updater); the event key should gain the updater then. No test fails today.

**N-3 (Info). Real-hardware behaviour cannot be proven by fixtures.** The first PiVPN or PiCUPS run (9.x to 10.8.2) is the first time `rpi_firmware_migration` meets the shared-propagation launcher on a real kernel. The trust walk has no vfat special case (SEC-004): if a host's `/boot` is a vfat mount with world-writable permissions, the step is refused with `unsafe_dietpi_path` and the command fails. That is fail-closed and truthful, but it would show on the first scheduled window. Doctor reports the effective unit; recommend Ryan runs doctor and a manual Patch outside a window first. The pre-story reboot after the migration is offered through the `dietpi` pseudo-entry.

**N-4 (Info). Accepted implementation deviations (impl report), reviewed.** Programmatic fixtures instead of checked-in trees (autocrlf risk; sound), no `nsops_other.go` (interface and logic portable, syscalls Linux-only; builds and vets on both), extra tests in new files, `agent_event_extra.go` and `utils/extraUpdaters.ts` outside ADD section 4, existing badge tests rewritten to group K (intended behaviour change, asserted against a frozen copy of the old Automatic, Reboot and As-of logic). ADD test names differ in two places (`TestRecover_EU_AC029_NoSecondRequest` is `TestExtras_EU_AC029_RecoverNeverReruns`; `TestOSUpdate_EU_AC044_NonDietPiRequestV1` is `TestPlanExtras_EU_AC044_...`); coverage is equivalent.

## Deviations from ADD

- None that change behaviour. Differences listed in N-4 plus the missing "with a note" on a future-dated start (N-1). `scripts/riot-maintenance.service`, the sudoers drop-in content and `cmd/riot-agent-apply/*` are unchanged as ADD section 4 requires. `go.mod`, `go.sum` and `web/package.json` unchanged; no migration.

## Deviations from FRD

- None found. FR-016 to FR-084 each trace to a passing test above. FR-063 to FR-065 (documentation) are not yet written; that is the technical writer's step and the matching tests skip until then.

## Verdict Rationale

**PASS WITH NOTES.** 54 of 55 ACs covered with tests that would fail on violation, AC-043 pending the documentation step by design, zero MISSING. All suites are green: Windows Go, Linux race Go (container, root, read-only source, network off), both installer fixture images (67 tests each), the web suite and build, `go vet` on both platforms. Every SEC and OPS test named in ADD section 8 exists and passes, including the opt-in real-namespace launcher test. The only red test seen anywhere is `TestShutdown_SD_AC030_LateNonZeroExitLogsOneWarn`, which is pre-existing on the merge base, repeat-sensitive, out of scope and fixed by PR #38. The notes above do not violate the FRD or a critical security property.

## Action Required

No FAIL items. For the pipeline, before merge:

1. Technical writer: land README, `docs/ops/os-maintenance.md` and CHANGELOG so the six skipped AC-043 tests turn into real checks. The docs must also state: the SEC-001 residual and upstream note, the 06:25 `cron.daily` advice and 90-minute window, the `/var/tmp/dietpi` trust rule, the owner rule (`MichaIng`, `DEV_GITOWNER`), DietPi's own mounts taking effect, the single-server-process limit of event dedupe (N-2), and the clock-jump limit of the 60-minute interval (N-1). Never a manual copy of `/var/tmp/dietpi` content (SEC-008).
2. Follow-up story (not blocking): N-1 store-side filtering of future-dated starts; N-2 key the event dedupe on the updater when a second updater lands.
3. EXPECT-001 (PR #37, open, not touched): its per-command window must read the stored `Command.ExecDeadlineS` (single source `execDeadlineSeconds`, `internal/server/commands/service.go:307`), and expect small textual conflicts in `internal/server/db/interfaces.go` and `internal/testutil/mocks.go` (both append one `EventRepository` method).
4. Ryan, on the first real run: confirm doctor on PiVPN and PiCUPS, run a manual Patch before relying on the schedule (N-3).

## Late addendum: final-tree Linux race run

Second full run, `go test -race -count=1 ./...` in the same `golang:1.24` container setup, on the tree that includes the three QA test additions (exported with `git archive` after the QA commit): 32 packages ok, 0 FAIL, EXIT 0. `internal/agent` 117 s, `internal/agent/collectors` 81 s, `internal/maintenance` 15 s, `cmd/riot-maintenance` 1 s, `scripts` 5 s. `TestShutdown_SD_AC030_...` passed in both full runs. The two full Linux runs agree, so no flakiness was seen in this story's tests.

## Addendum: rebase on main after EXPECT-001 (PR #37) merged

The branch was rebased on `origin/main` (edc1300, PR #37 merged). Three textual conflicts were resolved by keeping both sides: `internal/models/events.go` (event constants), `web/src/api/client.ts` and `web/src/api/demo-data.ts` (type imports), and `CHANGELOG.md`. No migration was added; EXPECT-001's `000033_expected_activity` is the highest.

`TestNoExpectCode_EU_AC034` then failed by design, because it asserted that the build contained no EXPECT-001 code. FR-046 and AC-034 were amended: the extra-updater code builds no expected-activity mechanism of its own, and EXPECT-001's os_update window derives from the stamped deadline (`execBudget` in `internal/server/expect/window.go` reads `deadline_at`, then `exec_deadline_s`). The test now scans only the extra-updater code, and the new `TestEX_Window_ExecBudgetExtraUpdaterDeadline` proves a stamped os_update window uses the 95-minute deadline. QA note 3 above is closed.

The post-rebase suite results are in the PR description.
