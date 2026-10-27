# QA Report
**Story ID:** DIETPI-REFRESH-001
**QA Engineer:** QA Agent
**Date:** 2026-10-27
**Branch:** `fix/dietpi-post-refresh` (compared against `origin/main` e120031, which is also the merge base)
**Verdict:** PASS WITH NOTES

---

## Test Run Summary

Windows, `go test -count=1`, no `-race`, `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`.

- `go test ./internal/maintenance/... ./internal/server/handlers/...`: both packages `ok`.
- Full `go test ./...`: every package `ok` or no test files, exit 0. `./scripts` took 55 s and `./internal/agent` 24 s. There were no timeouts, so no `-timeout 20m` re-run was needed.
- `go vet ./...`: exit 0, no output.
- Flakiness: `go test -count=5 ./internal/maintenance/` passed, and several other full-package runs were identical. No flaky test was seen. The impl report mentions one transient `detection_failed` on this machine. It did not recur (see note N-3).
- Not run locally: the Linux-only `*_linux_test.go` files (`extra_linux_test.go`, `flags_linux_test.go`, `io_linux_test.go`, `launcher_linux_test.go`, `lifecycle_linux_test.go`, `qa_dietpi_linux_test.go`) and `-race`. None of them is touched by this story, and CI on Linux covers them. The new tests are portable and also run on Linux.
- The v1 pin test `TestWorker_DR_AC002_V1PostRefreshRecordPinned` was run against a scratch worktree at `45b3f14`, which is the pre-story production code plus only that test. It passes there, together with the golden test. This confirms that the pinned bytes describe the pre-story helper. The scratch worktree was removed.

## AC Coverage Audit

"Would the tests catch it?" was decided by reading the assertions and by the mutation checks below.

| AC | Status | Tests | Gap |
|---|---|---|---|
| AC-001 | COVERED | `TestExecute_EU_AC044_V1Golden`. `git diff origin/main -- golden_v1_test.go` is empty. | none |
| AC-002 | COVERED | `TestWorker_DR_AC002_V1PostRefreshRecordPinned` pins JSON bytes, the phase list, call index 7 and the stamp count (plain, reboot class, failed refresh). It was committed in `45b3f14`, before any production change, and passes on both sides. Removing or always-running the v1 branch is caught (M5, M5b). | none |
| AC-003 (PiCUPS) | COVERED | `…AC003_DietPiRewritesPreferencesD` uses the real `AptSourceEntries` and `AptSourcesFingerprint` over the rig root. A same-size `os.Chtimes` runs in the step. It asserts 2 refresh calls, the second after `step:dietpi`, the last stamp after the step, success/0, and the apt line equal to `SuccessSummary`. A no-rewrite control proves the fingerprint is stable. A mutation that always refreshes (M11) fails it, as does the "refresh twice" mutation (M4). | none |
| AC-004 | COVERED | `…AC004`: no refresh between the upgrade and the step, one after it, and `[post_refresh]` after `[extra:dietpi]` | none |
| AC-005 | COVERED | `…AC005`, plus QA `…QA_AC005_FingerprintReadTwiceAtMost` | none |
| AC-006 | COVERED | `…AC006`: no `post_refresh` phase, JSON equal to the `Sources=nil` oracle rig, 2 fingerprint reads | The oracle is the same code with `Sources=nil`, not the old binary. This is valid because the old helper made no check in that case, and v1 is pinned separately. |
| AC-007 | COVERED | `…AC007` (second observation errors) plus QA `…QA_AC007_UnreadableBaselineRefreshesOnce` | Baseline-error case added by QA. |
| AC-008 | COVERED | `…AC008` (change during repair, 1 refresh) | none |
| AC-009 | COVERED | `…AC009`: no updater present, 2 refreshes, last phases `post_refresh, finished`, `[post_refresh]` is the last section | none |
| AC-010 | COVERED | `…AC010`, 2 subtests, assert 0 refreshes and **0 fingerprint reads** (C-4). Dropping `listsRefreshed` is caught (M2). | none |
| AC-011 | COVERED | `…AC011_OwnRefreshFailsNoRetry` and `…AC011_RefreshPhaseNotPersisted` (M-4). Setting the flag early is caught (M12). | none |
| AC-012 | COVERED | `…AC012`: 2 refreshes, the second is the last apt call, error/error/100, `g.result` DeepEqual to the oracle, updater `skipped/apt_not_successful` | none |
| AC-013 | COVERED | `…AC013`: oracle DeepEqual of the updater result, error/1 | The fingerprint is changed by `bump()`, not by a file (impl deviation 3, accepted). |
| AC-014 | COVERED | `…AC014`: timed_out and one refresh | Same as AC-013. |
| AC-015 | COVERED | `…AC015`: lock held, `skipped/dpkg_locked`, result and outcome equal the oracle | Same as AC-013. |
| AC-016 | COVERED | `…AC016` (2 subtests) plus `TestSummaryV2_DR_AC016_InterruptedArmNeverCarriesNote` (M-3). Dropping the apt-interrupted guard (M3a) and appending the note in the interrupted arm (M7) are both caught. | none |
| AC-017 | COVERED after QA | `…AC017_UpdaterInterruptedNoRefresh`, `…AC017_StoppedServiceSkipsSilently`, and QA `…QA_AC017_InterruptedUpdaterAloneSkipsSilently` | Dropping only the updater-interrupted guard survived the original tests (see F-2). QA test added. |
| AC-018 | COVERED | `…AC018` (injected clock plus real-deadline subtests). Dropping either clock check is caught (M10a, M10b). | none |
| AC-019 | COVERED | `…AC019`: exactly 7m00s refreshes, 6m59s skips. Changing `<` to `<=` is caught (M8). | none |
| AC-020 | COVERED after QA | `…AC020_RefreshLimit` (see F-3), plus QA `…QA_AC022_ServiceStopDuringRefreshReachesItAndKeepsOutcome` | The original deadline assertions cannot fail (F-3). The QA test is the one that proves the refresh context derives from the run context (M13). |
| AC-021 | COVERED | `…AC021`: no refresh, note `state not persisted`, success | none |
| AC-022 | COVERED | `…AC022`: success/0/not interrupted, exact line `SuccessSummary; failed: <200 B>`, `Validate()` nil, updater untouched. QA test adds a service stop during the refresh. | none |
| AC-023 | COVERED | `…AC023_NoteAfterAptFailure` (1500-byte cut plus note), `TestSummaryV2_DR_AC023_ErrorArmAppendsNoteOnce`, and the server test `TestPatchBadge_DR_AC023_PostRefreshNoteKeepsClassification` (8 subtests, single and multi-line notes, statuses failed and succeeded). Dropping the note from the error arm (M7b) is caught. | One server subtest asserts "not less alarming" instead of "equal" (deviation 1, accepted). |
| AC-024 | COVERED | `…AC024`: `[lists_stamp]` after `[post_refresh]`, no marker when the stamp works, outcome and apt line equal | The comparison covers the apt result line, not the whole summary (deviation 2, accepted). |
| AC-025 | COVERED | `…AC025`: output <= 256 KiB, truncated flag and marker, ends with `ENDTAIL`, summary <= 4096, reason exactly 200 B | none |
| AC-026 | COVERED | `…AC026`: the two refresh invocations are DeepEqual | none |
| AC-027 | COVERED | `…AC027`: 15 scenarios, each with `Validate()`, `parseRecord`, `Terminal()`, last saved phase `finished` | none |
| M-1 (a, b, c) | COVERED | `…SEC001a/b/c` and `…SEC001_VerifiedBackstopIsRecordedOnTheRun`. Each asserts the skip at start+24m, the refresh at start+20m, and 0 `Unit` reads for pre-check-6 skips. See the M-1 section below. | none |
| M-2 | COVERED | Server test with multi-line notes, as above | none |
| M-3 | COVERED | Two direct `summaryV2` tests | none |
| M-4 | COVERED | `…AC011_RefreshPhaseNotPersisted` | none |

No AC is MISSING. Nothing is PARTIAL after the QA tests.

### M-1: 30-minute and 80-minute basis tied to backstop verification

- Production: `executeExtras` defaults to `(aptCtx, WorkerBudget)` and switches to `(runCtx, ExtendedWorkerBudget)` only if some `extraRun.backstopVerified` is true. `runExtra` sets that flag in exactly one place, right after the `RuntimeMax >= ExtraMinBackstop` check and before check 7. A grep confirms there is no other writer.
- M1, "always use the extended basis": KILLED by SEC001a, SEC001b and SEC001c.
- M1b, "never verified": KILLED by AC019 and the positive-control test.
- M1c, "verified even when the backstop is too short": KILLED by SEC001 and SEC001a.
- Positive side: AC-019 proves a verified run really gets the 80-minute basis (a refresh runs at 73 minutes in, which is past 30).
- Unit read error: SEC001b. `status_unknown` through Execute and `nothing_pending` through `runExtra`: SEC001c, with 0 `Unit` reads.

## Conditions C-1 to C-4 (verified mechanically)

- **C-1: holds.**
  - `git diff origin/main --stat` has nothing under `cmd/`, `scripts/` or `internal/agent`. The only `internal/server/handlers` change is the new test file.
  - `aptsources.go`, `protocol.go` and `runner_linux.go` have an empty diff.
  - `git diff -U0` shows no changed line that mentions `aptRefreshArgs` or `CommandAllowed`.
  - Grep of non-test code: one `Runner.Run` for the post-run refresh (`worker.go:428`, in `runPostRefresh`), one `postRunRefresh(` call site (`worker.go:281`, in `executeApt`), and one `postRunRefreshV2(` call site (`extra.go:179`, in `executeExtras`). `Sources()` is called only inside `sourcesChanged` and `fingerprintSources`.
  - The "one `Runner.Run` for the refresh" requirement is also proven behaviourally. M4 (run the refresh twice) fails nine or more tests.
- **C-2: holds.**
  - The v1 branch is the positive test `req.Version == ProtocolVersion`.
  - `golden_v1_test.go` is byte-unchanged.
  - The pin test was added in `45b3f14`, before the refactor `4818c3f` and the feature `76ba63c`. It passes on the pre-story code.
- **C-3: holds, with a weakness in the original assertions.** See F-3.
- **C-4: holds.** AC-010 asserts 0 fingerprint reads, AC-017 asserts 1 (the baseline) and 0 for the direct call, AC-016 and M-4 assert at most 1. The new QA test asserts 0 for an interrupted updater. `Sources` is never called when the run is ineligible. M2, M3a, M3b and M3c all fail on these assertions.

## Mutation checks (production code mutated one line at a time, then reverted)

The tree was verified clean afterwards. Result per mutation, "killed" meaning at least one test failed:

| Mutation | Result | Killed by |
|---|---|---|
| M1 always extended basis | killed | SEC001a, b, c |
| M1b `backstopVerified` never set | killed | AC019, SEC001 positive control |
| M1c `backstopVerified` set before the backstop test | killed | SEC001, SEC001a |
| M2 drop `listsRefreshed` guard | killed | AC010, AC011 (both) |
| M3a drop apt-interrupted guard | killed | AC016 |
| M3b drop `svc.Err()` guard | killed | AC017_StoppedService |
| M3c drop updater-interrupted guard | **survived the original suite** | now killed by the QA test |
| M4 refresh runs twice | killed | AC003, 004, 005, 007, 009, 012, 013 and the changed EU_AC020 |
| M5 v1 branch removed | killed | golden test, EU_AC021, 022, 044 |
| M5b v1 branch always on, so v2 also checks in the apt part | killed | about 10 v2 tests |
| M7 interrupted arm appends the note | killed | `TestSummaryV2_DR_AC016` |
| M7b error arm drops the note | killed | AC018, 019, 021, 022, 025, SEC001 |
| M8 `<` becomes `<=` | killed | AC019 |
| M10a drop injected-clock check | killed | AC018, 019, SEC001a, b, c |
| M10b drop real-clock check | killed | AC018 only |
| M11 skip the fingerprint comparison | killed | AC003 control, AC006, AC008 |
| M12 `listsRefreshed` set before the step | killed | AC011 (both) |
| M13 refresh context from `context.Background()` | **survived the original suite** | now killed by the QA test |
| M14 / M14b unreadable observation not counted as changed | killed | AC007, QA_AC007, PF QA_AC032 |
| M15 baseline error dropped | killed | QA_AC007_UnreadableBaseline (the original AC007 did not cover it) |

## Impl report deviations

1. **AC-023 server test, "not less alarming" instead of "equal" for one case. Accepted.**
   - The case is a refusal-only run stored with legacy status `failed` plus a multi-line note.
   - The badge then reads the run as failed, which is the more alarming reading. It cannot make a run quieter, which is what BR-006 forbids.
   - Only workers older than DIETPI-PATH-001 stored `failed` for a refusal. The current agent keeps refusals as successful.
   - The other seven combinations assert full equality.
2. **AC-024 compares the apt result line, outcome and exit code, not the whole summary. Accepted with a note.**
   - The apt result line is the only part a stamp failure can reach, and the test also pins it to exactly `SuccessSummary`.
   - The reason given (transient DietPi detection failure on a loaded Windows host) is plausible but unverified. It did not reproduce in about ten runs (N-3).
3. **AC-013, AC-014 and AC-015 use `cfgState.bump()` instead of `os.Chtimes`. Accepted.** AC-003 uses the real fingerprint and the real file with a control. What these tests prove (eligibility, outcome isolation) does not depend on how the fingerprint changes.
4. **The server test avoids certain literals because of the existing EX2 AC-029 scan. Accepted.** Editing the guard would have meant touching an out-of-scope file.
5. **One extra test (the `backstopVerified` positive control). Accepted, and useful.** It kills M1b and M1c.
6. **One extra private constant, `postRefreshBudgetNote`. Accepted.** The string is byte-identical, and v1 bytes are pinned by AC-002 and the golden test.
7. **AC-020 deadline bounds are weak on the real clock. Confirmed, and the gap is closed by QA (F-3).**

## Test Quality Findings

- **F-1 (info, test written): unreadable baseline.** Only the second observation was failed in AC-007, so FR-007's "either observation" was half covered. QA test added (M15).
- **F-2 (low, test written): updater-interrupted guard not independently tested.**
  - In every end-to-end test, an interrupted updater also cancels the service context, so the `svc.Err()` guard hides the updater-outcome guard.
  - The real production path for an interrupted updater with a live service context is the 80-minute run context expiring: `runExtra` marks the step interrupted from `runCtx.Err()`.
  - Without the guard, such a run would add the "budget nearly exhausted" note and break FR-014 and BR-004.
  - The guard is present in the code. Only the test was missing.
  - QA added `…QA_AC017_InterruptedUpdaterAloneSkipsSilently`, which kills M3c.
- **F-3 (low, test written): the original AC-020 and C-3 assertions cannot fail.**
  - The refresh starts only with at least 7 minutes left on the basis, and its sub-context is `min(now+5m, basis deadline)`. That is always `now+5m`, so "no later than the basis or service deadline" holds even for a context built from `Background`.
  - M13 survived the original suite.
  - QA added a test that cancels the service context during the refresh and asserts that the refresh context sees the cancel. The same test shows that a failed, interrupted refresh leaves the outcome success/0/not-interrupted.
  - The impl report already admitted the weakness (deviation 7).
- **F-4 (info): `oracleRig` equals the pre-story helper only because both make no check with `Sources=nil`.** This is a sound oracle for the no-change, update-failed and refused cases, and v1 has a real pinned oracle. There is no direct old-binary comparison for v2. I judge this adequate because the one existing v2 test that changed is limited to what NFR-005 allows (see "Deviations from ADD").
- Isolation: no shared mutable state, and each test builds its own rig. Naming: AC and mitigation IDs are in the function names, subtest names and error messages as required.
- Mocks: `postRefreshRunner` wraps the real `orderRunner` and returns real error shapes. The fake `Runner` receives the real argv, and AC-026 pins it against the run's own refresh call.

## Adversarial Findings

- **Service stop during the refresh.** The refresh context derives from the run context and sees the cancel. The outcome stays success, there is no interrupted flag, and the note says `failed`. This is the intended behaviour (AD-004, DR-SEC-006). A QA test was written.
- **Multi-line notes.** The note reason comes from runner error text built with `errors.Join`. The server test covers it under both statuses, and the classification either stays equal or gets more alarming, never less.
- **Output and summary growth.** AC-025 covers a 64 KiB refresh output with a 1 KiB error on top of a 200 KiB run output. The output and summary stay within bounds, and the reason is cut to 200 bytes.
- **Repeated or concurrent rewrite.** The single call site means at most one refresh per run. The extra fingerprint reads are bounded, and M4 and the QA read-count test confirm it.
- **Privilege.** No new command, argv, environment or sudoers line (C-1).
- **Input injection.** No new input reaches the refresh. The decision reads only a stat fingerprint and worker-computed outcomes.
- **Not testable locally.**
  - Behaviour of the real `ExecRunner` kill sequence with a stray apt process.
  - A hung `stat` in the fingerprint walk. This is an accepted residual (DR-SEC-006).
  - A real `systemd` backstop kill during the refresh. Unit tests only prove the worker's own deadlines. M-1 keeps the worker inside the verified bound.

## Tests Added by QA

All in `internal/maintenance/extra_postrefresh_qa_test.go`, commit `62e6540`, all pass on repeated runs and are gofmt-clean.

| Test | Covers |
|---|---|
| `TestPostRefreshV2_QA_AC022_ServiceStopDuringRefreshReachesItAndKeepsOutcome` | AC-022, AC-020, FR-017, FR-019 (kills M13) |
| `TestPostRefreshV2_QA_AC007_UnreadableBaselineRefreshesOnce` | AC-007, FR-007 (kills M15) |
| `TestPostRefreshV2_QA_AC005_FingerprintReadTwiceAtMost` | AC-005, FR-009 |
| `TestPostRefreshV2_QA_AC017_InterruptedUpdaterAloneSkipsSilently` | AC-017, FR-014 (kills M3c) |

## Deviations from ADD

- `extra_test.go` changes only `TestExtras_EU_AC020_AfterVerifyPostRefreshAndHoldRestore`, exactly as ADD section 4 specifies (three edits). Permitted by NFR-005.
- Test function count: the ADD's count is 35 (32 new in the main file). The impl report says 33 test functions in the main file plus the extra positive control. This is a counting difference only, and every mapped test exists.
- `docs/ops/os-maintenance.md` and `CHANGELOG.md` are not yet updated. The ADD assigns them to the technical writer, so this is not a deviation.
- None otherwise. Production code matches the ADD rev 2 (AD-001 to AD-005, §6 signatures, `backstopVerified` placement).

## Deviations from FRD

None found. FR-001..FR-033 trace to code or tests. NFR-005 holds: the golden test is unchanged, v1 tests are unmodified, and one v2 ordering test changed in its allowed way.

## Notes (non-blocking)

- **N-1:** Ryan must re-run `install.sh` as root on PiCUPS, and on PiVPN once it is opted in (D-005). Until then the old helper runs. After that, do the D-006 post-deploy check: the next DietPi patch must show a `[post_refresh]` section after the DietPi section, and the agent's next report must show the lists as fresh. Unit tests cannot cover this.
- **N-2:** A-005 stands. In the worst case, with DietPi using its full lock wait and a long step, under 7 minutes remain and the refresh is skipped with a visible note. The badge then stays "Lists out of date" until the next refresh.
- **N-3:** Impl deviation 2 describes a transient `detection_failed` on a loaded Windows host. QA did not reproduce it. If it does appear in CI, it is an existing fixture flake and not caused by this story.
- **N-4:** The technical writer must state in `os-maintenance.md` that the note may span lines and that line 3 is not always the first updater detail line (AD-005).
- **N-5:** Linux-only test files and `-race` were not run locally. CI must be green before release.

## Verdict Rationale

**PASS WITH NOTES.**

- Zero AC with MISSING coverage.
- All tests green: the targeted packages, the full suite and `go vet`.
- No deviation from the FRD.
- M-1..M-4 and C-1..C-4 hold.
- The seven impl deviations are all accepted.
- QA found three test-strength gaps (F-1 to F-3). Each was closed with a passing test, and no production code was changed.

## Action Required

None for senior_dev. Before release: CI (Linux, `-race`) must be green, and the technical writer must complete the documentation items in N-4 and the ADD.
