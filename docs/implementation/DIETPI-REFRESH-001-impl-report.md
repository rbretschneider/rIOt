# Implementation Report

- **Story ID:** DIETPI-REFRESH-001
- **Engineer:** Senior Dev Agent
- **Date:** 2026-10-27
- **Branch:** `fix/dietpi-post-refresh`
- **Spec:** `docs/architecture/DIETPI-REFRESH-001-add.md` revision 2; security review mitigations M-1..M-4 and QA conditions C-1..C-4 included.

## Detected stack

| Item | Value |
|---|---|
| Language | Go, module `github.com/DesyncTheThird/rIOt`, `go.mod` says `go 1.24.0`; local toolchain is go1.26.0 windows/amd64 |
| Test runner | `go test` (standard `testing`, `testify` in the server packages); no `-race` locally (needs CGO) |
| Active specialists | Backend only. No schema, frontend, installer or CI change |
| Lint | `go vet ./...` and `gofmt` (no separate linter configured for these packages) |
| Working tree | CRLF checkout (`core.autocrlf=true`), so raw `gofmt -l` lists every file in the repo. gofmt was checked with CR stripped (see runs below) |

## Commits (oldest first)

| Commit | Message |
|---|---|
| `45b3f14` | `[DIETPI-REFRESH-001] test: pin v1 post-run refresh record` (written and run against the unchanged production code, before any production change) |
| `4818c3f` | `[DIETPI-REFRESH-001] refactor: extract shared post-run refresh helpers` (v1 and the golden test green) |
| `76ba63c` | `[DIETPI-REFRESH-001] feat: post-run refresh after the extra-updater step for v2 runs` |
| `693a970` | `[DIETPI-REFRESH-001] test: v2 post-run refresh, summary arms and server classification` |
| `6bc0740` | `[DIETPI-REFRESH-001] test: keep the server note test clear of the EX2 package-manager literal scan` |
| (this report) | `[DIETPI-REFRESH-001] docs: implementation report` |

## Completed components

| File | Action | Notes |
|---|---|---|
| `internal/maintenance/worker_postrefresh_v1_test.go` | CREATE | `TestWorker_DR_AC002_V1PostRefreshRecordPinned`: pinned JSON bytes, phase sequence, call index 7 and stamp count for a v1 `full` run with a fingerprint change (plain, reboot class, failed post-run refresh). Bytes were derived by hand from the golden layout and passed first time on the unchanged code. |
| `internal/maintenance/worker.go` | MODIFY | `aptState` gains `listsRefreshed`, `sourcesBefore`, `sourcesBeforeErr`. `executeApt` stores the baseline immediately before the `refresh` step (same call position), sets `listsRefreshed` only after `step("refresh")` returned nil, and calls `postRunRefresh` only when `req.Version == ProtocolVersion`. `postRunRefresh` split into `sourcesChanged` and `runPostRefresh` (same strings, same `aptRefreshArgs`, same `appendBounded` call, identical v1 call order). New `postRunRefreshV2` (AD-003 rev 2, AD-004). New constant `postRefreshBudgetNote` holds the existing FR-016 string so both paths share it. |
| `internal/maintenance/extra.go` | MODIFY | `extraRun.backstopVerified`, set on `run` immediately after check 6's `RuntimeMax >= ExtraMinBackstop` passes, nowhere else. `executeExtras` picks `(runCtx, ExtendedWorkerBudget)` if any run verified the backstop, else `(aptCtx, WorkerBudget)`, and calls `postRunRefreshV2` once between the updater loop and `finalizeV2`. `summaryV2` error arm appends `"; "+note`; the interrupted arm does not. Header and `executeExtras` comments updated. |
| `internal/maintenance/extra_test.go` | MODIFY | Only `TestExtras_EU_AC020_AfterVerifyPostRefreshAndHoldRestore`, exactly as ADD section 4: `"post-run refresh"` removed from the before-DietPi map; "last thing that ran" subtest replaced by "only the post-run refresh runs after the DietPi call"; "really ran" subtest asserts `postRefresh > step`. |
| `internal/maintenance/extra_postrefresh_test.go` | CREATE | 33 test functions (see map). Helpers: `cfgState`, `postRefreshRunner`, `loggingTouch`, `drRig`, `oracleRig`. |
| `internal/server/handlers/patch_badge_postrefresh_test.go` | CREATE | Server half of AC-023 including multi-line notes. Test only. |
| `docs/ops/os-maintenance.md`, `CHANGELOG.md` | not touched | Owned by the technical writer (ADD section 4). |

Unchanged as required: `golden_v1_test.go` (`git diff main` is empty), `aptsources.go`, `protocol.go`, `runner_linux.go`, `cmd/`, `scripts/`, `internal/agent`, non-test `internal/server`, `web/`, migrations.

## AC to test map

All new v2 tests are in `internal/maintenance/extra_postrefresh_test.go` unless a file is named.

| AC / mitigation | Test | Status |
|---|---|---|
| AC-001 | `TestExecute_EU_AC044_V1Golden` (`golden_v1_test.go`, unmodified) | PASS |
| AC-002 | `TestWorker_DR_AC002_V1PostRefreshRecordPinned` (`worker_postrefresh_v1_test.go`) | PASS before and after the production change |
| AC-003 | `TestPostRefreshV2_DR_AC003_DietPiRewritesPreferencesD` (real `AptSourceEntries` over the rig root, `os.Chtimes`; plus a no-rewrite control) | PASS |
| AC-004 | `TestPostRefreshV2_DR_AC004_AptChangeRefreshedAfterUpdater` | PASS |
| AC-005 | `TestPostRefreshV2_DR_AC005_AptAndUpdaterChangeRefreshOnce` | PASS |
| AC-006 | `TestPostRefreshV2_DR_AC006_NoChangeRecordIdenticalToOracle` | PASS |
| AC-007 | `TestPostRefreshV2_DR_AC007_AfterUpdaterFingerprintErrorRefreshes` | PASS |
| AC-008 | `TestPostRefreshV2_DR_AC008_ChangeDuringRecoveryIsBaseline` | PASS |
| AC-009 | `TestPostRefreshV2_DR_AC009_NoUpdaterPresentStillChecks` | PASS |
| AC-010 | `TestPostRefreshV2_DR_AC010_FailureBeforeOwnRefresh` (two subtests; asserts 0 fingerprint calls, C-4) | PASS |
| AC-011 | `TestPostRefreshV2_DR_AC011_OwnRefreshFailsNoRetry` | PASS |
| M-4 | `TestPostRefreshV2_DR_AC011_RefreshPhaseNotPersisted` | PASS |
| AC-012 | `TestPostRefreshV2_DR_AC012_UpgradeFailureStillRefreshes` (oracle DeepEqual) | PASS |
| AC-013 | `TestPostRefreshV2_DR_AC013_UpdaterFailedStillRefreshes` | PASS |
| AC-014 | `TestPostRefreshV2_DR_AC014_UpdaterTimedOutStillRefreshes` | PASS |
| AC-015 | `TestPostRefreshV2_DR_AC015_UpdaterRefusedStillRefreshes` | PASS |
| AC-016 | `TestPostRefreshV2_DR_AC016_AptInterruptedNoRefresh` (two subtests) | PASS |
| M-3 | `TestSummaryV2_DR_AC016_InterruptedArmNeverCarriesNote` (empty and one-updater runs) | PASS |
| AC-017 | `TestPostRefreshV2_DR_AC017_UpdaterInterruptedNoRefresh`, `TestPostRefreshV2_DR_AC017_StoppedServiceSkipsSilently` (direct call, 0 runner calls, 0 fingerprint calls) | PASS |
| AC-018 | `TestPostRefreshV2_DR_AC018_BudgetNearlyExhausted` (injected clock and real-deadline subtests) | PASS |
| AC-019 | `TestPostRefreshV2_DR_AC019_BudgetBoundary` (exactly 7m00s runs, 6m59s skips) | PASS |
| AC-020 | `TestPostRefreshV2_DR_AC020_RefreshLimit` (real clock; short service deadline subtest, C-3) | PASS |
| AC-021 | `TestPostRefreshV2_DR_AC021_PhaseNotPersisted` | PASS |
| AC-022 | `TestPostRefreshV2_DR_AC022_RefreshFailsAfterFullSuccess` | PASS |
| AC-023 | `TestPostRefreshV2_DR_AC023_NoteAfterAptFailure` | PASS |
| M-3 (mirror) | `TestSummaryV2_DR_AC023_ErrorArmAppendsNoteOnce` | PASS |
| AC-023 server, M-2 | `TestPatchBadge_DR_AC023_PostRefreshNoteKeepsClassification` (`internal/server/handlers/patch_badge_postrefresh_test.go`, 8 subtests, single-line and multi-line notes) | PASS |
| AC-024 | `TestPostRefreshV2_DR_AC024_StampFailureIsOutputOnly` | PASS |
| AC-025 | `TestPostRefreshV2_DR_AC025_OutputAndSummaryBounds` | PASS |
| AC-026 | `TestPostRefreshV2_DR_AC026_SameRefreshCommand` | PASS |
| AC-027 | `TestPostRefreshV2_DR_AC027_TerminalRecordsValidAndParse` (15 scenarios: Validate, `parseRecord`, `Terminal`, last saved phase `finished`) | PASS |
| M-1 (a) | `TestPostRefreshV2_DR_SEC001a_UnitLimit35MinUsesWorkerBudget` | PASS |
| M-1 (b) | `TestPostRefreshV2_DR_SEC001b_UnitErrorUsesWorkerBudget` | PASS |
| M-1 (c) | `TestPostRefreshV2_DR_SEC001c_PreCheck6SkipUsesWorkerBudget` (`status_unknown` via Execute with 0 `Unit` reads; `nothing_pending` via `runExtra` with `backstopVerified == false`) | PASS |
| extra | `TestPostRefreshV2_DR_SEC001_VerifiedBackstopIsRecordedOnTheRun` (positive control for the flag; not in the ADD map) | PASS |

## Test runs

All run on Windows, `go test -count=1`, no `-race`, with `GOCACHE=D:\rIOt_gocache` and `GOTMPDIR=D:\rIOt_fuzz_tmp`, on the committed tree at `6bc0740`.

### 1. New tests

`go test -count=1 -v ./internal/maintenance/ -run 'PostRefreshV2|SummaryV2_DR|TestWorker_DR_AC002|TestExtras_EU_AC020|TestExecute_EU_AC044'`

```
36 top-level tests PASS (87 PASS lines including subtests), 0 FAIL
ok  	github.com/DesyncTheThird/rIOt/internal/maintenance	0.892s
```

`go test -count=1 -v ./internal/server/handlers/ -run TestPatchBadge_DR_AC023`

```
--- PASS: TestPatchBadge_DR_AC023_PostRefreshNoteKeepsClassification (8 subtests)
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	1.899s
```

### 2. Touched packages

`go test -count=1 ./internal/maintenance/... ./internal/server/handlers/...`

```
ok  	github.com/DesyncTheThird/rIOt/internal/maintenance	1.656s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	9.721s
```

### 3. Full suite

`go test -count=1 ./...` exit 0. Every package `ok` or `[no test files]`:

```
ok  .../cmd/riot-signer 0.929s            ok  .../internal/server/commands 2.039s
ok  .../internal/agent 25.601s            ok  .../internal/server/db 1.336s
ok  .../internal/agent/collectors 12.988s ok  .../internal/server/events 0.809s
ok  .../internal/extraupd 2.859s          ok  .../internal/server/expect 2.328s
ok  .../internal/logbudget 0.627s         ok  .../internal/server/handlers 11.328s
ok  .../internal/maintenance 3.333s       ok  .../internal/server/ingest 0.672s
ok  .../internal/models 0.970s            ok  .../internal/server/middleware 1.371s
ok  .../internal/redact 0.540s            ok  .../internal/server/notify 1.256s
ok  .../internal/releasekey 0.608s        ok  .../internal/server/oidc 2.812s
ok  .../internal/resilient 1.546s         ok  .../internal/server/partition 0.692s
ok  .../internal/server 4.134s            ok  .../internal/server/probes 1.365s
ok  .../internal/server/auth 0.463s       ok  .../internal/server/quota 0.329s
ok  .../internal/server/ca 0.423s         ok  .../internal/server/scoring 0.640s
ok  .../internal/server/storage 1.031s    ok  .../internal/server/summary 0.869s
ok  .../internal/server/updates 0.405s    ok  .../internal/server/websocket 0.533s
ok  .../scripts 53.554s                   (cmd/riot-agent, riot-agent-apply, riot-maintenance, riot-server, logstore, testutil: no test files)
```

`./scripts` did not time out (53.6s), so no re-run with `-timeout 20m` was needed.

### 4. Lint

- `go vet ./...`: exit 0, no output.
- gofmt: every changed `.go` file is clean when checked with CR stripped (`tr -d '\r' < f | gofmt -l`). Raw `gofmt -l` lists every file in the repo because of the CRLF checkout; that predates this story.

An earlier full run failed once: `TestEX2_AC029_NoPowerUpdateOrAgentExecution` flagged string literals in the new server test (`apt `, `dpkg`). Fixed in `6bc0740` by building the lock reason from `maintenance.SkipDpkgLocked` and rewording two subtest names. The guard file was not touched.

### Skipped locally

The Linux-only files do not build on Windows, so their tests did not run here: `internal/maintenance/extra_linux_test.go`, `flags_linux_test.go`, `io_linux_test.go`, `launcher_linux_test.go`, `lifecycle_linux_test.go`, `qa_dietpi_linux_test.go`. None was changed by this story; CI (`make test` with `-race` on Linux) covers them. The new tests are portable and also run on Linux.

## QA conditions

- **C-1:** `git diff main --stat -- cmd/ scripts/ internal/agent 'internal/server/handlers/*.go'` shows only the new `patch_badge_postrefresh_test.go`. `aptRefreshArgs` and `CommandAllowed` are byte-unchanged. One `Runner.Run(sub, ...)` for the post-run refresh (`runPostRefresh`), one call site of `postRunRefresh` (`executeApt`, v1 branch), one call site of `postRunRefreshV2` (`executeExtras`).
- **C-2:** the v1 branch is `if req.Version == ProtocolVersion`. `golden_v1_test.go` is unchanged and passes. `worker_postrefresh_v1_test.go` is the first commit of the branch work, before any production change.
- **C-3:** AC-020 and SEC001a/b/c assert deadlines on the real clock; the 7m30s service-deadline subtest shows the refresh deadline is no later than the service deadline.
- **C-4:** AC-010 asserts 0 fingerprint calls, AC-017 asserts 1 (the baseline) and 0 for the direct call, AC-016 at most 1, M-4 at most 1.

## Bounds (ADD section 13)

- At most 1 post-run refresh per run (one call site), limited to 5 minutes plus the 90 second termination grace.
- A v2 run ends within 80 minutes when check 6 verified the unit backstop in this run (the refresh starts only with at least 7 minutes of the 80 left, on the injected clock and the real context deadline). Otherwise the basis is `aptCtx` and `WorkerBudget`, so the refresh ends within 30 minutes of the run start.
- Output stays at or below 256 KiB with the existing truncation marker; the note is at most 240 bytes (`"; "` + 38 + 200); the apt failure line at most 1,740 bytes; the summary at most 4,096 bytes.

## Deviations from the ADD (for architect review)

1. **AC-023 server test, one case is "not less alarming" rather than "equal".** For a refusal-only run stored with the legacy status `failed` (only a worker older than DIETPI-PATH-001 produced one), a multi-line note adds a line that is not an updater detail line. `parseRefusals` then no longer sets `Sole`, and the badge reads the run as failed (`needs_you` with the last-run-failed item) instead of `blocked`. This is the "conservative direction" the security review (DR-SEC-002) describes, so classification is never less alarming, but it is not equal. The test asserts equality in the other seven combinations and asserts `needs_you` plus `reasonLastRunFailed` in this one. The current worker never stores `failed` for a refusal-only run, so this does not occur in practice. No server code was changed.
2. **AC-024 compares the apt result line, outcome and exit code**, not the whole summary. On this Windows machine under load, DietPi detection returned `status_unknown/detection_failed` once for one of the two rigs (the trusted-flag read failed transiently), which changes the updater lines. The apt result line is the part a stamp failure could reach, and the test also asserts it equals `SuccessSummary`.
3. **AC-013, AC-014, AC-015 change the fingerprint with `cfgState.bump()`** in the step hook instead of `os.Chtimes` on a `preferences.d` file. AC-003 uses the real `AptSourceEntries` with `os.Chtimes`, as the ADD says, with a no-rewrite control.
4. **The server test avoids certain string literals** (reason from `maintenance.SkipDpkgLocked`, subtest names without `apt `) because the existing `TestEX2_AC029` guard scans `internal/server/handlers` test files. Adding exceptions to the guard would have meant editing a file outside ADD section 4.
5. **One extra test** beyond the ADD map: `TestPostRefreshV2_DR_SEC001_VerifiedBackstopIsRecordedOnTheRun`, a positive control that `backstopVerified` is set on a run that passes check 6.
6. **One extra private constant**, `postRefreshBudgetNote`, so the v1 and v2 paths share the FR-016 string. The string is byte-identical to before.
7. **AC-020 and SEC001 deadline upper bounds are weak on the real clock.** The refresh deadline is `min(call time + 5 min, basis deadline)`, so "no later than the basis deadline" holds trivially in a test whose real clock barely moves. The assertions that actually distinguish the two bases are the injected-clock skip cases (SEC001a/b/c `start+24m`, AC-018, AC-019) and the short service deadline subtest.

No blockers file was needed.

## Notes for QA

- Probe the DR-SEC-001 split: a v2 run whose DietPi was skipped before check 6 (`not_enabled`, `nothing_pending`, `status_unknown`, no updater, or an apt failure) must use the 30-minute basis; a run that passed check 6 uses the 80-minute basis. Mutating the default basis in `executeExtras` to `runCtx` must fail SEC001a/b/c.
- Mutating `postRunRefreshV2` to drop the `listsRefreshed` guard must fail AC-010, AC-011 and M-4; dropping the interrupted guards must fail AC-016 and AC-017.
- On Windows under load, DietPi detection on the fixture host can fail transiently (`detection_failed`), which makes DietPi tests flaky regardless of this story. It was seen once, in AC-024 before deviation 2; every other run of the suite was clean.
- Post-deploy (D-005, D-006), not covered by unit tests: the fix lives in the `riot-maintenance` helper, which reaches a device only through a root `install.sh` re-run. After the release, Ryan re-runs `install.sh` as root on PiCUPS (and PiVPN once it is opted in), then the next DietPi patch must leave a `[post_refresh]` section after the DietPi section in the record, and the agent's next updates report must show the package lists as fresh.
- No new environment variable, migration, dependency, sudoers line or systemd change.
