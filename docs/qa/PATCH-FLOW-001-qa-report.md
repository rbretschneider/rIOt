# QA Report

**Story ID:** PATCH-FLOW-001
**QA Engineer:** QA Agent (Sonnet 5.5)
**Date:** 2026-10-24
**Branch / range:** `feat/patch-flow-001`, `main...4d6c9f3` plus one QA commit (`488f31d`). Nothing pushed.
**Verdict:** PASS WITH NOTES

Every FRD acceptance criterion (AC-001 to AC-038) and every security condition (C-1 to C-8) maps to a test that fails when the behavior is broken. All suites are green on Windows and on Linux with `-race`. About 85 production-code mutants were run. Seven showed real weaknesses in the developer tests, and QA closed them with 15 new tests. One finding is a design limitation in the kernel comparison (N-1). It is not an FRD violation, but it must be checked on the fleet before the first authorized Patch. Details below.

---

## Test Run Summary

All numbers were produced on this machine, not copied from the implementation report.

### Windows (host), after the QA commit

| Suite | Result |
|---|---|
| `go vet ./...` | clean |
| `go test -count=1 ./...` | 29 packages `ok`, 0 failing |
| `go test -count=1 -v ./...`, top-level tests | **2,002 PASS, 0 FAIL, 48 SKIP** (1,889 sub-tests PASS, 0 FAIL, 58 SKIP including sub-tests). 142 top-level tests carry `PF_`, plus 15 added by QA. The skips are pre-existing platform- or Postgres-gated tests (for example `TestCL_AC086_QA_PG_*`); none is a PF test. |
| `cd web && npm run test:run` | 95 files, **1,707 tests passed**, 0 failed |
| `cd web && npx tsc --noEmit` | clean |
| `cd web && npm run build` | ok (the usual chunk-size warning only) |

The implementer's counts (1,987 top-level tests before QA, 1,707 web tests) reproduce exactly. 1,987 + 15 QA tests = 2,002.

### Linux (Docker `golang:1.24`, `-race`, root)

The tree was exported with `git archive` into a scratch directory on `D:` and run in throwaway `--rm` containers. No container is left behind. The scratch copies were deleted.

| Run | Result |
|---|---|
| `go vet ./...` | clean |
| `go test -race -count=1 ./...`, first on `4d6c9f3`, then on `488f31d` (with QA tests) | every package `ok`, both times. This includes `cmd/riot-maintenance` (the C-5 `touchDirNoFollow` tests) and the Linux-only helper tests. |
| `go test -race -count=3 -run "QA_\|PF_"` on `internal/agent`, `internal/agent/collectors`, `internal/maintenance`, `cmd/riot-maintenance` | stable, no failures in any iteration |
| C-5 mutation (no `O_NOFOLLOW`, no owner check) | both `TestTouchDirNoFollow_PF_C5_*` tests go red |

The `RIOT_TEST_PG_URL` integration tests did not run (no Postgres), same as the implementer's run. Nothing in this story touches SQL.

**Flaky tests:** none. The Windows Go suite ran three times, the web suite twice and the Linux suite twice. The timing-sensitive phase B tests (1 to 40 ms real timers) passed under `-race -count=3`.

**Environment.** C: has 2.6 GB free, so Go ran with `GOCACHE`, `GOTMPDIR` and `TEMP` on `D:` (`D:\rIOt_gocache` and `D:\rIOt_fuzz_tmp`, the same locations the implementer used). `riot-server` on this workstation's local Docker is in a restart loop. That was already so when QA started, and QA did not touch it.

---

## Mutation Testing

QA applied each mutant to the real file, ran the affected package, and reverted with `git checkout` (the tree was clean again after every batch). **Killed** means a test went red. Two or three batches first showed "killed" only because the mutant did not compile; those were redone as compilable mutants and are not counted below.

| Area (requested) | Mutants | Killed by developer tests | Notes |
|---|---|---|---|
| (a) `patch_badge` reboot semantics | 8: `requiredNow` also true for a pending kernel (the original vermont bug), `pendingRC` ignoring stale, NY-10 triggered by pending, Reboot-line wording for pending, stale note removed, "outside rIOt" action removed, `autoCovers` reduced | 8 of 8 | The vermont bug mutant is killed by 6 tests (`PF_AC001`, `PF_AC002`, `PF_AC003`, `PF_AC006`, `DecisionTable_RebootDateInWindowMode` and others). |
| (b) server boundary | 12: case-sensitive strip, case variant of `reboot_if_required` accepted, non-boolean accepted, `false` stored, repair allowed, `allow_reboot` false ignored, capability ignored, no snapshot allowed, `ErrNoRows` as 500, `include_reboot_class` never set | 12 of 12 | The strict boolean, both case rules and every rejection each have a dedicated test. |
| (c) reboot decision | 8: failed run can reboot, phase A without the `allow_reboot` veto, phase B without the re-check, claim ignored, "not required" ignored, `claimAlreadyClaimed` treated as OK, no delivery wait, phase B reboots when not started | 8 of 8 | Mutant c3 is killed twice (behavior test and AST call-site test). |
| (d) intent store | 11: max age 24 h and 71.5 h, min keep 24 h and 71.5 h, eviction floor, no clock guard, corrupt heal immediate and at 24 h, future mtime | 7 of 11 | **Survivors, now killed by QA tests:** min keep at 24 h and at 71h30m, corrupt-store heal at 24 h. One survivor is equivalent (future mtime: `now - mtime` is already negative, so the extra `After(now)` term is redundant). |
| (e) freshness | 17: threshold shifted by 1 h, equal-is-stale, missing lists, no `_Packages`, Ubuntu stamp, lists mtime ignored, skew guard removed, `.sources` / `.pref` / `preferences.d` / `sources.list` / `sources.list.d` not scanned, backups counted | 16 of 17 | **Survivor, now killed by a QA test:** skew guard widened from 5 min to 5 h. |
| (f) helper | 10: always refresh, refresh twice, no budget skip, no stamp (both calls), phase not saved, note not in summary, either fingerprint error ignored | 8 of 10 | **Survivors, now killed by QA tests:** a "before" fingerprint error and an "after" fingerprint error each ignored. The existing error test returned different fingerprints, so the fingerprint difference decided the outcome and the error handling never did. One more survivor is equivalent (setting `Outcome = "error"` inside the helper is overwritten by the deferred finisher). |
| C-3 / C-2 / telemetry | 10: own unit counted as busy, any record excuses, non-terminal own record excuses, no phase B lock, C-2 event removed on both paths, empty-applied branch without telemetry, no immediate first send, recovery without telemetry | 9 of 10 | **Survivor, now killed by a QA test:** a non-terminal record of the same command excused the busy check. |
| Web | 8: capability ignored, stale ignored, Reboot primary with pending, params ignore `enabled`, no reset on open or close, always send flag, held statement wrong, fleet totals include stale | 7 of 8 | **Open survivor:** `confirmParams` re-checking `rebootOption().enabled` (see N-3). |

---

## AC Coverage Audit

COVERED means the listed tests fail when the AC is broken, checked by reading the test bodies and, for the risky rules, by the mutants above. File names are under `internal/...` unless marked `web/`.

| AC | Status | Tests covering it | Gap / note |
|---|---|---|---|
| AC-001 | COVERED | `server/handlers/patch_badge_test.go`: `PatchBadge_PF_AC001_PendingKernelRaisesNoManualReboot`, `…_NoBadgeTextTellsToRebootWhenNotRequired` (4 sub-tests scan every text field) | Mutant a1 and a3 killed |
| AC-002 | COVERED | `PatchBadge_PF_AC002_PendingKernelDescribedAsFutureManualReboot` (exact string) | — |
| AC-003 | COVERED | `PatchBadge_PF_AC003_PendingKernelWithGatedAutoDescribedAsAutomatic` (3 sub-tests) | Mutant a7 killed |
| AC-004 | COVERED | `PatchBadge_PF_AC004_RequiredNowRaisesManualRebootWithRebootNowAction`, `…CausesNeverClaimNoKernelPending`, `…CauseListNeverEmpty`, `…CoveredByAutomaticPath`, `…WithoutPatchSupportStillSaysRebootNow` | DoD grep for the removed phrase is empty (re-run by QA) |
| AC-005 | COVERED | `agent/collectors/kernelreboot_test.go` (`KernelReboot_PF_AC005_*`, `CompareDebVersion_PF_AC005_Table`, `FuzzCompareDebVersion`), `updates_test.go` `PF_AC005_KernelSource…`; **QA** `KernelReboot_QA_AC005_VermontBootListing` (real `/boot` listing) | See N-1 for the coexisting-flavor limitation |
| AC-006 | COVERED | `PatchBadge_PF_AC006_PendingOnlyIsNotRebootRequired`, `PatchStatus_PF_AC006_PendingOnlyRowIsNotRebootRequired`; web `DeviceDetail.patchflow` and `FleetOverview.patchflow` AC-006 blocks | Mutant w3 killed |
| AC-007 | COVERED | web `PatchConfirmOptions.test.tsx`, `DeviceDetail.patchflow.test.tsx` "unchecked every time the dialog opens" (open, tick, cancel, reopen) | Mutant w5 killed |
| AC-008 | COVERED | web `patchFlow.test.ts` `rebootOption`, `PatchConfirmOptions.test.tsx` (reason visible, `aria-describedby`) | — |
| AC-009 | COVERED | web (same files): neither fact; `allow_reboot` true without capability | Mutant w1 killed |
| AC-010 | COVERED | web `patchFlow.test.ts` / `PatchConfirmOptions.test.tsx` statements (a), (b), (c), none when `rc = 0` | Mutant w7 killed. See N-2 |
| AC-011 | COVERED | web `patchDialogStatements` and page test (already-required reboot) | — |
| AC-012 | COVERED | `SendCommand_PF_AC012_AuthorizedDispatchSetsIncludeRebootClass`; web ticked confirm sends `reboot_if_required: true` | Mutant b10 killed |
| AC-013 | COVERED | `SendCommand_PF_AC013_FalseAuthorizationStoredAsAbsent`, `…AbsentAuthorizationUnchanged`; agent `OSUpdate_PF_AC013_UnauthorizedRunNeverRebootsWhenRequired`, `RunOSUpdate_PF_AC013_UnauthorizedPlanKeepsPatchGatePhrases`; web unticked sends mode only | Mutants b4, w6 killed |
| AC-014 | COVERED | `SendCommand_PF_AC014_ClientIncludeRebootClassStrippedWithoutAuthorization` (5 spellings incl. `ſ`), `…AuthorizationValidationIsScopedToOSUpdate`, existing `[AC-013]` strip test | Mutant b1 killed |
| AC-015 | COVERED | `SendCommand_PF_AC015_NonBooleanAuthorizationRejected` (6 values, each asserts no `Create` call), `…RepairWithAuthorizationRejected` | Mutants b3b, b5 killed |
| AC-016 | COVERED | `SendCommand_PF_AC016_*` (4 tests incl. 500 on telemetry error), `UpdateInfo_PF_AC016_CapabilityOnlyTrueCounts`, `TelemetryIngest_PF_AC016_…`, `Telemetry_PF_AC016_AgentReportsCapability` | Mutants b6 to b9b killed. Distinct codes `reboot_not_allowed` and `reboot_authorization_unsupported` asserted |
| AC-017 | COVERED | `agent/reboot_auth_test.go` `RebootAuth_PF_AC017_RequiredAfterSuccessRebootsOnceAfterResultQueued` (asserts `out.Has` at the moment of reboot), `…_WaitsForDeliveryAck`, `…_DeliveryWaitBoundedThenReboots` | Mutants c7, c8 killed |
| AC-018 | COVERED | `RebootAuth_PF_AC018_NotRequiredNoReboot` | Mutant c5 killed |
| AC-019 | COVERED | `RebootAuth_PF_AC019_FailedRunNoReboot` (failed, timed out, interrupted), `maintenance/worker_test.go` `Worker_PF_AC019_HoldsRestoredOnFailedAuthorizedRun`; `maintenanceResult` maps hold-restore failures to `error` | Mutant c1 killed. The three run kinds are fed as synthetic results; the real helper-record mapping is pre-existing and covered by `maintenanceResult` tests |
| AC-020 | COVERED | `RebootAuth_PF_AC020_AgentVetoBeatsAuthorization`, `…AllowRebootTurnedOffBeforeTheRebootAborts`, `events/generator_test.go` `PatchGateEvent_PF_AC020_RebootRequiredEventFires`, AST `RebootAuth_PF_C8_…` | Mutants c2, c3 killed |
| AC-021 | COVERED (after QA) | `RebootAuth_PF_AC021_SecondExecutionSameIDSuppressed`, `RebootIntents_PF_AC021_ClaimedIDInsideWindowNeverPruned`, `RecoverMaintenance_PF_AC021_RecoveredAuthorizedResultNeverReboots`, `…MarkerCarriesAuthorizationAtIntake`; **QA** `RebootIntents_QA_AC021_*` (3 tests at 71h59m and 72h01m) | The developer tests used 10 h and 71 h entries, so the 72 h floor moved freely between 24 h and 71.5 h. Closed by QA |
| AC-022 | COVERED | `server/handlers/auto_update_test.go` `AutoPatch_PF_AC022_DispatchParamsUnchanged` (gated, off, out of window), agent `OSUpdate_PF_AC022_AutomatedRebootRuleUnchanged`; `auto_update.go` unchanged in `git diff` | — |
| AC-023 | COVERED | web `DeviceDetail.patchflow.test.tsx` (Reboot now primary, existing confirmation, `reboot` sent), `patchFlow.test.ts` S4 | Mutant w3 killed |
| AC-024 | COVERED | web (same): disabled with the visible outside-rIOt reason | — |
| AC-025 | COVERED (after QA) | `collectors/listsfresh_test.go` `PF_AC025_*` (6 tests), `maintenance/aptsources_test.go` `PF_AC025_*`; **QA** `ListsFreshness_QA_AC025_Vermont*` (3 tests on the real vermont tree) | Mutants e1 to e16 killed |
| AC-026 | COVERED (after QA) | `listsfresh_test.go` `PF_AC026_*` (stale for missing lists, unknown for stat error, budget, deadline, future mtime, no files created, at most one debug line); **QA** `ListsFreshness_QA_AC026_ClockSkewGuardIsFiveMinutes` | The skew boundary was unpinned (mutant e7) |
| AC-027 | COVERED | `PatchStatus_PF_AC027_RowCarriesStaleFreshness`; web page and fleet panel/table tests | Mutants w2, w8 killed |
| AC-028 | COVERED | `PatchBadge_PF_AC028_*` (4 tests) | Mutants a2b, a5 killed |
| AC-029 | COVERED | `models/telemetry_patchflow_test.go` (11 decode cases incl. wrong case), `TelemetryIngest_PF_AC029_OldAndBogusAccepted2xxNoWarn`, `PatchStatus_PF_AC029_AbsentAndBogusServeUnknown`, `PatchBadge_PF_AC029_*`, web "unknown keeps Patch (N)" | — |
| AC-030 | COVERED | `UpdatesCollector_PF_AC030_DnfNeverReportsStale` | — |
| AC-031 | COVERED | `AutoPatch_PF_AC031_FreshnessDoesNotChangeDispatch` | No production change; no dispatch path reads freshness |
| AC-032 | COVERED (after QA) | `maintenance/worker_test.go` `Worker_PF_AC032_*` (once, output bound, error as changed, stamp failure), `Worker_PF_AC038_PostinstRewriteTriggersOneRefresh`; **QA** `Worker_QA_AC032_Before…` and `…After…FingerprintErrorRefreshesOnce…` | The error test used differing fingerprints (mutants f2, f3b) |
| AC-033 | COVERED | `Worker_PF_AC033_NoChangeNoExtraRefresh`, `…RepairModeNoRefreshAtAll`, `…FailedUpgradeNoExtraRefresh`; **QA** `Worker_QA_AC033_EqualReadableFingerprintsSkipTheRefresh` | — |
| AC-034 | COVERED | `Worker_PF_AC034_*` (4 tests): failed refresh keeps success and `Validate()` passes; interrupted sub-refresh is not `interrupted`; low budget; state not persisted | Mutants f5b, f10, f11 killed |
| AC-035 | COVERED | `OSUpdate_PF_AC035_EveryNonRebootOutcomeTriggersOneTelemetry`, `…TriggersCoalesce`, `RebootAuth_PF_AC035_NoTriggerWhenRebootStarts` | Mutants c1, h1b, i2 killed |
| AC-036 | COVERED | `TelemetryLoop_PF_AC036_FirstSendImmediateOnStart` | Mutant i1 killed. See N-6 for the recovery ordering |
| AC-037 | COVERED | `SendCommand_PF_AC037_StoredParamsShowAuthorizationAndIncludeRebootClass`, `CommandSent_PF_AC037_EventMentionsAuthorization`, `RebootAuth_PF_AC037_SuffixGrammarAllReasons`, `…AuthorizationParamValidation` | — |
| AC-038 | COVERED (automated part) | `PatchBadge_PF_AC038_VermontBeforeRunNeverSaysRebootByHand`, `RebootAuth_PF_AC038_VermontRunReportsThenRebootsOnce`, `Worker_PF_AC038_PostinstRewriteTriggersOneRefresh`; **QA** vermont fixtures. The end-to-end on a real host needs a deployed build (post-deploy steps below) | — |

### Security conditions

| Condition | Status | Evidence |
|---|---|---|
| C-1 intent store self-heal | COVERED (after QA) | `RebootIntents_PF_C1_*` (5 tests) plus **QA** `RebootIntents_QA_C1_*` (71h59m closed, 72h01m heals, huge file set-aside bounded). Mutants d7, d8 killed |
| C-2 suppressed-reboot event | COVERED | `RebootAuth_PF_C2_PersistFailureEmitsOneWarningEvent` (authorized and automated). Mutants j1, j2 killed |
| C-3 own unit is not busy | COVERED (after QA) | `RebootAuth_PF_C3_*` (4 tests), `MaintenanceBlockerFor_PF_C3_StrictWithoutOwnID`; **QA** `MaintenanceBlockerFor_QA_C3_OwnRunStillInProgressIsStillBusy`. Mutants g1, g2, g3 killed. The adapted `TestMaintenance_AC009_*` still asserts no reboot and no replay for another command's worker (diff reviewed) |
| C-4 case variants | COVERED | `SendCommand_PF_C4_ReservedKeyVariantsOfRebootIfRequiredRejected`, strip test with `ſ`. Mutants b1, b2 killed |
| C-5 no-follow stamp | COVERED (Linux) | `cmd/riot-maintenance/main_linux_test.go` `TouchDirNoFollow_PF_C5_*`; the Docker run is green and mutated copies go red |
| C-6 comparator total | COVERED | `CompareDebVersion_PF_AC005_Table` (30-digit runs), `FuzzCompareDebVersion`. QA ran the fuzz target for 20 s (1.3 million inputs): no failure |
| C-7 chunked reads | COVERED | `AptSourceEntries_PF_Bound_ChunkedReadStopsAtBudget`, `KernelReboot_PF_AC005_EntryBoundReachedNotOK`, `ListsFreshness_PF_AC026_EntryBudgetIsUnknown`. Mutant e12b killed |
| C-8 DoD gate | COVERED | QA re-ran the greps (below); both AST tests exist and pass |

C-8 greps re-run by QA at HEAD:
- `no kernel or GPU update is pending` under `internal/`, `cmd/`, `web/src`: no output.
- `include_reboot_class"] = true` outside tests: exactly `auto_update.go:580` and `os_update_auth.go:60`.
- `execReboot()` callers: `commands.go:238` (`maybeRebootAfterPatch`), `commands.go:314` (`handleReboot`) and `reboot_auth.go:234` (`rebootAfterDelivery`), each after an `AllowReboot` check.
- `claimPatchReboot` is defined only in `reboot_intents.go:58`.
- `dangerouslySetInnerHTML` / `innerHTML` in the four web files: none.
- `git diff main...HEAD` is empty for `protocol.go`, `auto_update.go`, `doctor.go`, `install.sh`, `PatchBadge.tsx` and the migrations.
- No `console.log`, `fmt.Println`, TODO or FIXME was added in non-test code.

---

## Test Quality Findings

1. **The 72 h floor was not pinned (fixed by QA).** Moving `rebootIntentMinKeep` to any value between 24 h and 71h30m passed all developer tests, as did moving the corrupt-store heal age to 24 h. The security argument for AD-012 rests on exactly 72 h. QA added tests one minute either side of 72 h for the cap eviction and for the corrupt-store heal.
2. **The fingerprint-error tests proved nothing about the error (fixed by QA).** See AC-032.
3. **The skew guard accepted any width (fixed by QA).** See AC-026.
4. **AC-019's "timed out" and "interrupted" are synthetic results.** The test feeds `failedRun(reason)` into the decision. That is adequate, because the decision keys on `Status != success` and `maintenanceResult` (pre-existing) maps any non-success or interrupted record to `error`. QA read that mapping and found no path that produces `success` for an interrupted or hold-failed run.
5. **Web `confirmParams` guard is untested (open, N-3).**
6. **Behavior over implementation.** The PF tests assert observable outputs: HTTP status and code, the absence of a `Create` call, the stored params, the result message, the count of runner reboots, and the queued-outbox state at the moment of reboot. The two AST tests are deliberate structural locks (C-8) and are named as such. They are brittle by design, and a new `execReboot` caller will fail them on purpose.
7. **Isolation.** No shared mutable state across tests. Every store test uses a temp file and a fake clock; the web tests mock the API per test. The Linux `-count=3` run showed no order dependence.
8. **Mocking hygiene.** Server tests assert interactions (no `Create` call on rejection) rather than just silencing the repo. The helper tests use a recording runner that checks the exact argv.

---

## Adversarial Findings

| # | Finding | Severity | Test written? |
|---|---|---|---|
| N-1 | **Coexisting kernel flavors cause a permanent false `reboot_required`.** `splitKernelFlavor` (`agent/collectors/kernelreboot.go:171-181`) takes only the token after the last `-`. On Debian, `6.12.48+deb13-rt-amd64` and `6.12.48+deb13-cloud-amd64` both have flavor `amd64`, so against a running `6.12.48+deb13-amd64` their base `6.12.48+deb13-rt` compares as newer (revision `rt` > empty). QA confirmed it with a probe (since deleted): `running=6.12.48+deb13-amd64, installed=[…-amd64, …-rt-amd64] => newer=true`; same with `cloud`. The reverse (running `rt`) and different upstream versions are correct. Ubuntu `generic` / `lowlatency` and Proxmox `-pve` are correct. Consequences: the host reports "Required now" and gets a "Reboot now" button for ever, and **every ticked Patch on that host reboots once**, even when nothing was installed (BR-004 in spirit). It never loops, because every reboot needs a ticked operator dispatch. It follows ADD AD-002 as written, so this is a design limitation, not an implementation deviation. | MEDIUM-LOW, non-blocking | No (a test asserting the wrong result would bake the bug in). Recommended follow-up: treat any extra token between the version and the final arch token as part of the flavor, or compare only releases whose text after the version is identical. **Pre-deploy check:** `ls /boot \| grep -E '^vmlinuz-.*-(rt\|cloud)-'` on each apt host. vermont has none (read-only SSH, listing in the real-host section). |
| N-2 | **Dialog states kernel/GPU facts from a stale list.** `patchDialogStatements` (`web/src/utils/patchFlow.ts:122-136`) shows statement (a), (b) or (c) from the raw `pending_reboot_class_count` even when the lists are stale, followed by the stale statement. The server badge, by contrast, deliberately derives nothing from the pending list while stale (FR-036). This follows ADD AD-010's stated order, and the stale caveat appears in the same list, so it is not wrong, but it says "will be installed" about a count the page itself calls unreliable. | LOW | No |
| N-3 | `DeviceDetail.tsx` `confirmParams` re-checks `rebootOption().enabled` before sending `reboot_if_required`, so a tick made before telemetry flips `allow_reboot` to false is dropped. The checked-and-disabled combination is hidden in the component, so the page test never reaches it. No test pins this guard (mutant w4). The server rejects the request anyway with `reboot_not_allowed`, so the effect of a regression would be a visible 400, not an unwanted reboot. | LOW | No (needs a live telemetry refetch in the page harness) |
| N-4 | apt also reads the single file `/etc/apt/preferences` (`apt-config dump` on vermont shows `Dir::Etc::preferences "preferences"`). The freshness rule does not scan it. The FRD names three locations (OQ-007), so this is by spec. Vermont has no such file. | LOW | No |
| N-5 | A result for an authorized `os_update` refused before it runs (maintenance blocker, host lock conflict) carries no FR-023 suffix (`commands.go:101`, `:115`). No reboot can happen on that path, and the command shows as refused, so the audit trail is truthful, but FR-023's wording ("the result must state…") is not met literally. Already noted by the implementer. | LOW | No |
| N-6 | `telemetryLoop` starts after startup recovery (bounded by 2 min), so the "within 60 s of start" bound in FR-047 can stretch on a host that must reconcile an interrupted Docker update. Accepted by Ryan in the ADD (flag 6). The test proves the immediate send once the loop runs. | INFO | n/a |
| N-7 | Phase A consumes the claim before `out.Add`. If `out.Add` fails, `completeCommand` returns before phase B: no reboot, and the claim stays consumed. This is the safe direction (the command is also not reported), and the same as the ADD's "result durable first" rule. | INFO | n/a |

Input attacks on the new API boundary were checked at the tests: SQL and script payloads in `reboot_if_required` are non-boolean and get 400; `null`, `"true"`, `1`, `{}` and `[true]` get 400 with no `Create` call; `Reboot_If_Required` gets 400; `INCLUDE_REBOOT_CLASS` and `Include_Reboot_Class` are deleted; `reboot_if_required: true` with `mode: repair` gets 400. A telemetry payload with a hostile `lists_freshness` (object, array, number, wrong case) or capability decodes without error and is stored normalized. Auth: the route sits behind the existing admin same-origin middleware; no new endpoint exists, and `BulkPatchDevices` and `checkAutoPatch` never read the new fields.

---

## Real-Host Read-Only Facts Check (vermont, Debian 13 + OMV, 2026-10-24 00:37 local)

Only `stat`, `ls`, `cat`-type reads, `apt-config dump`, `dpkg-query -W` and `journalctl` were run. Nothing was written, installed or restarted. The SSH session is root, so the file-permission rows below are read from the modes, not from a run as `riot`.

| ADD §12 note 10 item / assumption | Observed | Result |
|---|---|---|
| Kernel being run vs `/boot` naming | `/proc/sys/kernel/osrelease` = `7.2.9+deb13-amd64` (mode 0444); `/boot` is `drwxr-xr-x root` with 76 entries (cap is 256), 19 of them `vmlinuz-*`; `vmlinuz-7.2.9+deb13-amd64` exists | Matches. Both are readable by `riot`. The new check reports `reboot_required=false` now (running = newest, no `/run/reboot-required`). Fixtured in `KernelReboot_QA_AC005_VermontBootListing` |
| Files apt reads | `apt-config`: `Dir::Etc::sourcelist=sources.list`, `sourceparts=sources.list.d`, `preferencesparts=preferences.d`, also `Dir::Etc::preferences=preferences` (absent on this host). Every vermont file matches the rules: five `.list`/`.sources` files, three `.pref` files. `sources.list~` is a backup apt ignores. apt is 3.0.3 | Matches, plus N-4 |
| Lists dir is real and root-owned | `/var/lib/apt/lists` is `drwxr-xr-x root:root`, `/var/lib/apt` likewise | OK for `touchDirNoFollow` (owner uid 0) |
| Incident classified stale by the new rule | `riot-maintenance` journal: refresh ran 22:44:06 to 22:44:37 and downloaded list files (`Get:` / `Fetched 737 kB` at 22:44:31-36), so the lists dir mtime was about 22:44:36 (the user's "about 22:44"). Config mtimes now: `omvextras.sources` 22:46:32.811, `omvextras.pref` 22:46:32.983. Both are after 22:44:36 | Classified **stale** (both the `.sources` and the `.pref` alone). Fixtured in `ListsFreshness_QA_AC025_Vermont*` (the 22:44 mtime is a reconstruction; the real value was overwritten by the 22:57 refresh) |
| State now | Lists dir mtime = 22:57:39.379 (same instant as `partial/`), later than every config file | Classified **fresh** (fixtured) |
| No-change `apt-get update` leaves the lists dir mtime unchanged | **Not verifiable read-only.** The journal shows `Hit:3 file:/var/cache/openmediavault/archives ./ Packages` at 22:44:06, so an unchanged OMV local index is a hit. The zero-byte `_var_cache_openmediavault_archives_._Packages` in the lists dir has mtime 22:46:32.000, which is when `omv-mkaptidx` (hook `99openmediavault-mkaptidx`, `DPkg::Post-Invoke`) regenerated the source, copied in by the later refresh | Post-deploy step 4 |
| Ubuntu `update-success-stamp` | Absent on this Debian host (`/var/lib/apt/periodic` has `update-stamp` only) | As designed: Debian relies on the lists dir mtime and the new helper's stamp |
| v1 `patch-reboot-intents.json` upgrade path | `/var/lib/riot` has no intent file on vermont | The v1 to v2 rewrite cannot be checked here (post-deploy step 7) |
| Helper | Old helper still deployed (`install.sh` not re-run) | Section D features need `install.sh` (AD-008) |

---

## Tests Added by QA

Committed as `488f31d`, 15 tests in 4 files, all green on Windows and Linux (`-race -count=3`). Each has an `// Added by QA Engineer` comment and an AC reference.

| File | Tests | Covers |
|---|---|---|
| `internal/agent/reboot_intents_qa_test.go` | `RebootIntents_QA_AC021_CapNeverEvictsAnIntentOneMinuteUnder72h`, `…_CapEvictsAnIntentOneMinuteOver72h`, `…_OnlyTheEntryPastTheFloorIsEvicted`, `RebootIntents_QA_C1_CorruptStoreOneMinuteUnder72hStaysFailClosed`, `…OneMinuteOver72hSelfHeals`, `…_HugeOldStoreSetAsideCopyIsBounded` | AC-021, NFR-006, C-1 |
| `internal/agent/reboot_auth_qa_test.go` | `MaintenanceBlockerFor_QA_C3_OwnRunStillInProgressIsStillBusy` | C-3 |
| `internal/agent/collectors/patchflow_qa_test.go` | `ListsFreshness_QA_AC025_VermontOmvextrasRewriteAfterRefreshIsStale`, `…_VermontPreferencesAloneMakeItStale`, `…_VermontAfterSecondPatchIsFresh`, `ListsFreshness_QA_AC026_ClockSkewGuardIsFiveMinutes` (4 and 6 minutes), `KernelReboot_QA_AC005_VermontBootListing` | AC-005, AC-025, AC-026, AC-038 |
| `internal/maintenance/worker_qa_test.go` | `Worker_QA_AC032_BeforeFingerprintErrorRefreshesOnceEvenIfFingerprintsMatch`, `…_AfterFingerprintErrorRefreshesOnceEvenIfFingerprintsMatch`, `Worker_QA_AC033_EqualReadableFingerprintsSkipTheRefresh` | AC-032, AC-033 |

The commit trailer is `Co-Authored-By: Claude Sonnet 5.5`, which matches the model that made the commit. The task text asked for Opus 5.5, as on the earlier commits of this branch; the commit-attribution rule in force for this session names the actual model, so QA used that. Amend the trailer if the branch should stay uniform.

---

## Deviations from ADD

The implementer lists 16 deviations. QA read each against the code and agrees none changes a contract in ADD §6 or a security property.

1. Items 1 to 3, 9 to 11 and 13 (signature shape of `applyManualOSUpdateAuth`, `AptSourceEntriesUntil`, the wall-clock deadline, the two extra AST test files, `parseAptUpgradable`, `reportSuppressedReboot` location, `futimes`): accepted, behavior-identical or stricter than the ADD.
2. Item 4 (SEC-007 ruling keeps raw security counts visible on stale rows): Ryan's ruling, not a defect. The fleet-panel row therefore shows both the stale label and "N security (lists out of date)". FR-035 and AC-027 ask that the stale label replace the *pending count*, and the tests assert that.
3. Item 6 (existing `TestMaintenance_AC009_*` adapted): correct. The old fixture produced exactly the false "busy" that C-3 removes. The adapted test still asserts no reboot and no replay for another command's worker.
4. Item 12 (phase B checks the maintenance blocker before and after the host reservation): both abort with the same event; fine.
5. Item 14 (a recovered failed run reads "suppressed", not "run not successful"): matches the ADD's literal text; cosmetic.
6. **ADD §13 Definition of Done, "host verification recorded":** still open in the implementation report. QA recorded the read-only half above. The rest are post-deploy steps.

## Deviations from FRD

None blocking. Two literal gaps, both non-blocking: N-5 (no FR-023 suffix on a refused-before-run result) and the FR-047 recovery ordering (N-6, accepted in the ADD). The reboot-semantics requirements (FR-001 to FR-007) are met without exception: no code path derives "reboot now" from anything but `reboot_required`.

---

## Verdict Rationale

**PASS WITH NOTES.**
- Zero AC with missing coverage; the 38 ACs and 8 security conditions map to tests that fail on mutation, after QA closed seven real gaps.
- All tests are green: 2,002 Go top-level tests and 1,707 web tests, on Windows and on Linux with `-race`. `go vet`, `tsc` and the production build are clean.
- No implementation deviation from the FRD that blocks release.
- The notes are: N-1 (needs a fleet check before the first ticked Patch and a follow-up fix), N-2, N-3, N-4, N-5.

## Post-Deploy Verification Steps

Deploy order: server, then agent (self-update), then `install.sh` per device for the helper. Check the running image digest after the server deploy.

0. **Before any ticked Patch, on every apt host:** `ls /boot | grep -E '^vmlinuz-.*-(rt|cloud)-'` (N-1). Any hit means that host will show a false "reboot required" and should not be offered the tick until N-1 is fixed. vermont has none.
1. **First telemetry after the agent update (60 s):** on vermont, `GET /api/v1/fleet/patch-status` shows `lists_freshness: "fresh"`; the device telemetry shows `supports_reboot_if_required: true`, `reboot_required: false`, `lists_freshness: "fresh"`. Server log shows no new warnings from older agents' telemetry.
2. **Checkbox gating:** the device page for an updated agent with `allow_reboot: true` shows the enabled box, unchecked on every open. An agent that has not updated shows it disabled with "The agent must be updated". `POST …/commands` with `reboot_if_required: true` against it returns 400 `reboot_authorization_unsupported`. A host with `commands.allow_reboot: false` returns 400 `reboot_not_allowed`.
3. **Stale flip with the old helper (needs one write, to be approved by Ryan):** create then remove `/etc/apt/preferences.d/zz-riot-qa.pref` on vermont. The device flips to `lists_freshness: "stale"` within one telemetry cycle, the Patch button reads "Package lists out of date — refresh pending", and the badge shows the stale note and no kernel wording. Then press Patch (unticked): with the old helper, a refresh that downloads nothing may leave the host stale (residual R-3); with the new helper the run stamps the lists dir and the host returns to `fresh`.
4. **ADD §12 note 10 item 1 (needs the old helper or a manual run):** `stat -c %y /var/lib/apt/lists`, run a refresh that downloads nothing, `stat` again. Expect an unchanged mtime with the old helper and a new mtime after a Patch run with the new helper (check the `[lists_stamp]` / `[post_refresh]` markers in the run output).
5. **Authorized Patch on a host with a pending kernel:** tick the box. Expect HTTP 201 with `reboot_if_required: true` and `include_reboot_class: true` in the stored params, the event text "(reboot authorized if required)", and a result message ending "Reboot authorized; reboot required after run: yes; reboot started: yes". `journalctl -b -1` shows exactly one `systemctl reboot`, issued after the result arrived. After boot, within 60 s: `reboot_required: false`, `lists_freshness: "fresh"`, correct pending count.
6. **Authorized Patch where nothing needs a reboot:** the host stays up and the result says "reboot not started: not required".
7. **v1 to v2 intent store:** on a host that already has `/var/lib/riot/patch-reboot-intents.json` (vermont does not), confirm it becomes `{"version":2,…}` after the first claim, with no reboot as a side effect.
8. **Vermont badge:** a host with pending kernel and `reboot_required: false` shows "The pending patch includes a kernel/GPU update; a reboot will be needed after it installs…" and no "Reboot this host" action.

## Action Required

None (not a FAIL). Recommended follow-ups, in priority order:
1. N-1: refine the flavor split in `kernelreboot.go` (`splitKernelFlavor`) so `-rt-` and `-cloud-` kernels are separate flavors, and add the missing probe cases to the tests.
2. N-3: add a page test for a tick followed by a telemetry change.
3. N-2 and N-5: decide whether to hide the kernel/GPU statements while stale, and whether to add the FR-023 suffix to refused-before-run results.
