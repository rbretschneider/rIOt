# QA Report: GROWTH-001 PR-C (agent-side bounds)

**Story ID:** GROWTH-001, PR-C (agent)
**QA Engineer:** QA Agent
**Date:** 2026-10-19
**Branch:** `growth-001-pr-c-qa` (the implementation branch `growth-001-pr-c-impl`, rebased onto current `main`)
**Verdict:** PASS WITH NOTES

---

## Rebase onto main

`growth-001-pr-c-impl` (13 commits on `8873911`) was rebased onto `origin/main`, which now contains AUTOUPD-HARDEN-001 (SEC-002) and the other merged GROWTH-001 PRs. Three commits conflicted. Each was resolved by keeping both sides.

| Commit | File | Conflict | Resolution |
|---|---|---|---|
| bounded agent config keys | `internal/agent/config.go` | Import list (`fmt` kept by main, `errors` added by PR-C); the end of `LoadConfig` (`cfg.Docker.normalizeComposeRoots()` from SEC-002, `cfg.Validate()` from PR-C) | Both imports. Both calls, `normalizeComposeRoots()` first, then `Validate()` |
| bounded agent config keys | `internal/agent/config_test.go` | SEC-002's three `compose_roots` tests and PR-C's `loadYAML` block were added at the same place | Kept all tests of both sides; closed SEC-002's last function and started PR-C's after it |
| identity pre-flight | `internal/agent/agent.go` | `Agent` struct: SEC-002 added `trustFS`/`trustPlatform`, PR-C added `fsys`/`idPath`/`fatalCh` in the same spot | Kept both field groups (SEC-002's first) |
| memory limit in the unit, doctor | `internal/agent/doctor.go` | Import list (`sort` used by SEC-002's compose section) | Kept `sort` |
| memory limit in the unit, doctor | `internal/agent/doctor_test.go` | SEC-002's compose-trust tests and PR-C's memory-limit tests appended at the same place | Kept all tests of both sides |

`scripts/install.sh`, `scripts/riot-agent.service`, `README.md`, `CHANGELOG.md` and `cmd/riot-agent-apply/main.go` merged without a textual conflict. Checks after the rebase:

- `git diff origin/main -- scripts/install.sh scripts/riot-agent.service` shows only PR-C's `MemoryMax=50%`, `Environment=GOMEMLIMIT=200MiB`, `OOMPolicy=continue` and their comment. SEC-002's `ExecStartPre=-+/usr/local/lib/riot/riot-agent-apply --boot` unit, sudoers and root-owned binary changes are untouched. `bash -n scripts/install.sh` is clean.
- No test function of either side was lost: the diff of the five conflicted files against `origin/main` shows no removed line except the four intended PR-C replacements (`NewBuffer(` call, `<-done`, the periodic re-registration call, `os.WriteFile` in `Save`), and the diff against the pre-rebase branch removes nothing in the two test files.
- `git diff origin/main --stat` lists neither `internal/agent/docker_update.go`, `internal/server/handlers/auto_update.go`, `go.mod` nor `go.sum`.

## Test Run Summary

| Run | Result |
|---|---|
| `go build ./...`, `go vet ./...` (Windows) | clean |
| `GOOS=linux go vet ./...` | clean |
| `go test -count=1 ./...` (Windows, no `-race`) | all packages ok, 0 failing, 0 skipped by the suite (PG-gated server tests skip without `RIOT_TEST_PG_URL`, as at baseline) |
| `go test -race -count=1 ./internal/agent/... ./cmd/riot-agent-apply/` in `golang:1.24` (Linux, worktree mounted read-only, writable `GOCACHE`/`GOPATH`) | `internal/agent` ok (34.6 s), `internal/agent/collectors` ok (35.6 s), `cmd/riot-agent-apply` ok (1.3 s) |
| `go test -count=3 ./internal/agent/ ./internal/agent/collectors/ -run GROWTH` | ok, no flaky test seen |

- Coverage percentage: not measured. AC coverage was judged by mutation (below).
- Flaky tests: none.
- Suspicious output: none. The expected `WARN` lines come from the code under test.

### Mutation check

For each control below, the code was broken on purpose (one edit, restored with `git checkout`) and the named tests were run. In every case at least one test failed, except the one gap that was closed.

| Mutation | Failing test |
|---|---|
| 401 treated as a permanent rejection | `AC062_PermanentStatusesDropAndTransientOnesKeep`, `AC062_401And403KeepItem`, `AC062_LivePushKeptOnRetryableStatuses` |
| Byte condition removed from `Store`; item-count condition removed | `AC060_BufferByteCapEvictsOldest` (both) |
| `Delete` after 2xx skipped | `AC061_AckedItemNeverResent`, `AC062_*`, `AC063_429HonoursRetryAfter_Flush` |
| `LimitReader` removed; announced-length refusal removed; partial-file cleanup removed | `AC066_DownloadStopsAt64MiBNoPartial` (each) |
| `Retry-After` clamp removed | `AC063_429HonoursRetryAfter` |
| Sibling files not moved aside | `SEC022_SiblingFilesMovedVacuumENOSPCContinues` |
| `incremental_vacuum` never run | `AC064_BufferFileShrinksAfterDelivery` |
| Pre-flight writes 0 bytes | `SEC016_PreflightWritesAndSyncsAtLeast4KiBThroughTheAtomicPath` |
| Fatal flag never set on an ID change | four `AC053_*` fatal tests |
| Ring byte cap removed (container logs; journal) | `AC056_ByteAndPassBoundsHold`, `AC056_CapRingKeepsNewest...`, `SEC022_JournalRingBoundedInBytesAsWellAsEntries` |
| `Tail` asked as `max_lines + 1`; cursor not advanced; 5,000-line pass cap raised | `AC056_*` |
| Truncation marker removed | `AC057_*` (both) |
| Skipped count zeroed; auth counting disabled | `AC058_*`, `AC058_AuthCountingStaysExactForEvictedEntries` |
| `OOMPolicy` line removed from `install.sh`; `MemoryMax=infinity` in the service file | `AC059_InstallerUnitAndDoctorMemoryLimit` |
| Body guard disabled | `D36_BodyGuardDropsContainerLogsThenJournal` |
| Clamp to the maximum removed | `AC068_EveryAgentBoundFiniteInRange` |
| Staging sweep age check removed | `AC067_AgentStartSweepsStaleStaging` |
| Helper: exact name length, regular-file check, owner check, group/other-writable check, 1 h age, hex alphabet, sweep call removed | `AC067_HelperSweepsStaleTempOnly` / `SEC023_HelperSweepRequiresRootOwnedDirExactName` (each) |
| **20,000-line container scan bound removed** | **none. This was a gap**, closed by `TestGROWTH_QA_AC056_ScanLineBoundStopsTinyLines` (now fails under the mutation) |

## AC Coverage Audit

Test names are in the `TestGROWTH_` namespace; every test named in ADD section 8 and 8.1 exists.

| AC / condition | Status | Tests covering it | Gap description |
|---|---|---|---|
| AC-053 / C-17 (SEC-016) | COVERED | `AC053_UnwritableIdentityNoRegistration`, `AC053_ENOSPCOnWriteNoRegistration` (create works, write fails), `SEC016_PreflightBackoff`, `SEC016_PreflightWritesAndSyncs...`, `AC053_IDWriteFailingAfterRegistration...`, `AC053_ConfigWriteFailing...`, `AC053_PeriodicReRegistration*`, `AC053_ExitPath...`; QA: `QA_AC053_RealFilesystemUnwritableIdentityNeverRegisters` | The first tests use a fault-injecting wrapper over the real file system; QA added a real-file-system case |
| AC-056 | COVERED | `AC056_NewestThousandLinesBoundedRead`, `AC056_ByteAndPassBoundsHold`, `AC056_CapRingKeeps...`; QA: `QA_AC056_ScanLineBoundStopsTinyLines` | The 20,000-line scan bound was untested (see mutation table); fixed by QA |
| AC-057 | COVERED | `AC057_LongLinesTruncatedAtRuneBoundary`, `AC057_JournalMessagesTruncatedAtRuneBoundary`; QA: `QA_AC057_HugeAndHostileLinesStayBounded`, `QA_AC057_OneMegabyteFrameIsTruncatedNotKeptWhole` | Boundary (max, max+1), invalid UTF-8 expansion, NUL bytes and a 1 MiB frame without a newline added by QA |
| AC-058 | COVERED | `AC058_JournalKeepsNewest5000Logs2000Skipped`, `..._SkippedCountIsExact`, `..._AuthCountingStaysExactForEvictedEntries`, `..._CursorAdvancesPastSkippedEntries`, `SEC022_*` | |
| AC-059 / C-18 (SEC-017) | COVERED | `AC059_InstallerUnitAndDoctorMemoryLimit` reads the real `install.sh` and `riot-agent.service` text; README states the caveat | FRD says `MemoryMax=256M`; the ADD replaced it with D-29 (50 %, `GOMEMLIMIT`, `OOMPolicy`), listed in ADD section 14 |
| AC-060 | COVERED | `AC060_BufferByteCapEvictsOldest`, `AC060_OversizedLiveSnapshotIsDroppedNotStored`; QA: `QA_AC060_ConcurrentStoreKeepsByteAndItemCap` | Concurrency added by QA (8 writers plus a reader/deleter/compactor; counters equal the table; file within cap + 256 KiB) |
| AC-061 | COVERED | `AC061_AckedItemNeverResent`, `AC061_SuccessfulLivePushFlushesTheBacklog`; QA: `QA_AC061_ItemThatFailsDeleteRepeatedlyDoesNotLoopOrGrow` | A failing `Delete` after a 2xx re-sends once per tick (see notes); QA test pins that it is bounded |
| AC-062 / C-9 (SEC-009 agent side) | COVERED | `AC062_Rejected400ItemDroppedOthersDelivered`, `AC062_PermanentStatusesDropAndTransientOnesKeep` (400, 404, 413, 422 drop; 401, 403, 408, 429, 500, 503 keep), `AC062_401And403KeepItem`, `AC062_LivePush*`, `AC062_401And403BackOffLikeA429` | 408 kept is an approved deviation |
| AC-063 | COVERED | `AC063_RetryAfterParsing`, `AC063_429HonoursRetryAfter` (injected clock, 29 s held, 30 s allowed, clamp, doubling, reset), `AC063_429HonoursRetryAfter_Flush`, `AC063_OtherStatusesDoNotBackOff`; QA: `QA_AC063_AbsurdRetryAfterIsBounded` | Year-9999 date, int64 overflow and 5xx cases added |
| AC-064 | COVERED | `AC064_BufferFileShrinksAfterDelivery` (40 MiB legacy file ends below 1 MiB) | |
| AC-065 / SEC-022 | COVERED | `AC065_CorruptBufferRecoveredOneAsideSet`, `SEC022_SiblingFilesMovedVacuumENOSPCContinues` | |
| AC-066 | COVERED | `AC066_DownloadStopsAt64MiBNoPartial` (chunked 100 MiB, announced oversize, 64 MiB exactly, 64 MiB + 1, full pipeline), `AC066_FailedDownloadIsReportedAsAgentUpdateFailed`; QA: `QA_AC066_ShortBodyUnderLyingContentLengthLeavesNoPartial`, `QA_AC066_LyingLowContentLengthCannotExceedAnnounced` | Lying `Content-Length` in both directions added |
| AC-067 (agent) | COVERED | `AC067_AgentStartSweepsStaleStaging` (2 h removed, 10 min kept, 1 h boundary, directory and symlink left alone) | |
| AC-067 (helper) / C-19 (SEC-023) / C-20 | COVERED | `AC067_HelperSweepsStaleTempOnly` (both modes, planted symlink and prefixed directory untouched), `SEC023_HelperSweepRequiresRootOwnedDirExactName` (name length derived from `tempSuffixBytes`) | Linux only; run in the container |
| AC-068 (agent) | COVERED | `AC068_EveryAgentBoundFiniteInRange` (6 keys x 8 inputs, one Warn each), `..._InRangeAndUnsetValuesDoNotWarn`, `..._DefaultsMatch...`; QA: `QA_AC068_OddYAMLScalarsStayInRange` (6 keys x 16 scalars: null, tilde, inf, nan, hex, octal, exponent, quoted, sequence, mapping) | |
| AC-073 | COVERED | `AC073_NewAgentDeliversToLegacyServer` | |
| SEC-016 atomic config | COVERED | `SEC016_ConfigSaveIsAtomicAndLeavesNoTempFile` | |
| Body guard (D-36) | COVERED | `D36_BodyGuardDropsContainerLogsThenJournal` | |

No AC is MISSING or PARTIAL after the QA additions.

## Test Quality Findings

- The PR-C tests assert observable behaviour: server request counts, queue contents, file existence and size, log lines, resolved config values. The internal seams (`bufferConvertVacuum`, `installer.dirOwnerUID`, `Agent.fsys`) are used to inject faults, not to assert on private state, except `drainLabels`, which queries the buffer table directly (acceptable for a test of the buffer).
- Mocking: the fake Docker daemon and the scripted telemetry server are real HTTP servers, so the real client code is exercised. The fake daemon honours `Tail` and `Since` like the real one and has a switch to ignore `Tail`.
- Isolation: no ordering dependence found; `-count=3` passes.
- The one false-coverage gap (20,000-line scan bound) was closed. Everything else listed in the mutation table is caught.
- `TestGROWTH_AC053_ENOSPCOnWriteNoRegistration` alone does not fail when the pre-flight probe writes zero bytes (the fake fails every `Write`, even an empty one). The 4 KiB requirement is pinned by `SEC016_PreflightWritesAndSyncsAtLeast4KiBThroughTheAtomicPath`, so the control is covered, but the two tests together are needed.

## Adversarial Findings

None blocks the PR. Each is bounded; none grows without limit.

| # | Probe | Result | Test |
|---|---|---|---|
| 1 | Byte cap under concurrent pushes plus a concurrent flush | Caps hold, counters equal the table, race detector clean | `QA_AC060_Concurrent...` |
| 2 | Item that fails `Delete` repeatedly (a trigger refuses DELETE) | One send per tick, queue neither grows nor shrinks, item cap holds even when eviction fails. The acknowledged item is re-sent each tick: duplicates, not growth (documented by the implementer). | `QA_AC061_ItemThatFailsDelete...` |
| 3 | Journal ring bytes | Bounded by 2 MiB and by `max_line_bytes`; existing test, mutation-checked | `SEC022_JournalRingBounded...` |
| 4 | Container-log single huge line with no newline | Docker frames are at most 1 MiB; a larger frame ends the stream read. A 1 MiB frame is cut to 4 KiB plus marker. `TruncateLogLine` on a 16 MiB string, invalid UTF-8 and NULs stays within `max + 12` bytes | `QA_AC057_*` |
| 5 | Download cap: chunked, lying `Content-Length` (high and low), exact limit | Fails at 64 MiB + 1; short body leaves no partial; a low lie cannot exceed the announced length | `AC066_*`, `QA_AC066_*` |
| 6 | Sweeps with symlinks and hostile names | Agent sweep touches only its three fixed paths and skips non-regular files; helper requires exact name length (derived from `tempSuffixBytes`), lowercase hex, regular file, age over 1 h, root-owned directory without group/other write. Mutation-checked | `AC067_*`, `SEC023_*` |
| 7 | Identity pre-flight on read-only or full disk | Never reaches `Register`, backs off 5 s doubling to 10 min, one log line per step, leaves no temp file; also with a real (not faulted) file system | `AC053_*`, `SEC016_*`, `QA_AC053_Real...` |
| 8 | Back-off clamp for absurd `Retry-After` | Year-9999 date and a 5xx with 86400 clamp to one hour; an integer beyond int64 is treated as absent (30 s schedule) | `QA_AC063_AbsurdRetryAfterIsBounded` |
| 9 | Config garbage | 16 odd scalars per key all resolve in range; load never fails | `QA_AC068_OddYAML...` |

## Notes (non-blocking)

1. **Journal scan bound is checked between lines.** `scanJournal` tests the 500,000-line / 256 MiB bound only at the top of its loop; `readBoundedLine` reads one line to its newline. One pathological line is therefore read in full (memory stays at most 1 MiB plus the 64 KiB read buffer; time is bounded by the line size, and `journalctl -o json` always ends a line with a newline). Not reachable with a real journal in a way that grows anything. No change requested.
2. **A `Retry-After` integer beyond int64 is treated as absent**, so the agent waits 30 s (doubling) instead of the one-hour ceiling. Conservative in the safe direction for data (it retries sooner) and still bounded.
3. **Buffer size is the payload cap, not a file cap.** `agent.buffer_max_mb` bounds the sum of payload bytes. The measured file stayed within cap + 256 KiB at a 2 MiB cap. SQLite's rollback journal can transiently add up to the size of the modified pages during a `Store`; this was not measured at 500 MiB. The bounds table says "at most 50 MiB on disk"; "about 50 MiB plus SQLite overhead" would be exact.
4. **Atomic `Config.Save` needs write access to the config directory**, not only to `agent.yaml`. The installer chowns `/etc/riot` to the agent user, so installer installs are fine. A manual install with a root-owned `/etc/riot` and a riot-writable `agent.yaml`, a symlinked `agent.yaml`, or a single-file container bind mount of `agent.yaml` would now fail to save (rename), where the in-place write worked. A failed save after registration of a new identity is fatal by design (AD-018). Worth one line in the PR description.
5. **A new agent whose config directory is read-only waits in the pre-flight for ever** (logging every backoff step) instead of registering. This is the intended FR-057/AD-018 behaviour, and `riot-agent doctor` cannot show it because the agent has not started its loops.
6. **`PRAGMA quick_check` on every start** is O(file size); at the 500 MiB maximum this is noticeable on slow disks (implementer's note, agreed).
7. **An offline buffer that is a valid SQLite file with a foreign `buffer` table** fails open (agent runs without a buffer), not as "corrupt". Bounded; no growth.
8. **The 60 s exit delay does not stop the cycle** when the disk fills between the pre-flight and the ID write: the unit restarts after about 65 s, the next start's pre-flight blocks (disk still full) and does not register again, so at most one ghost per fill event.
9. **Report location.** The ADD names `GROWTH-001-impl-report-C.md`; the file is `GROWTH-001-PR-C-impl-report.md`, as the orchestrator instructed (deviation 4 in the report). This QA report is the only other document in the PR; the story documents (FRD, ADD, security review) are not copied in (SEC-037).

## Tests Added by QA

All are marked "Added by QA Engineer" and pass.

| File | Test | Covers |
|---|---|---|
| `internal/agent/growth_qa_test.go` | `TestGROWTH_QA_AC060_ConcurrentStoreKeepsByteAndItemCap` | AC-060, NFR-001 |
| `internal/agent/growth_qa_test.go` | `TestGROWTH_QA_AC061_ItemThatFailsDeleteRepeatedlyDoesNotLoopOrGrow` | AC-061, NFR-001 |
| `internal/agent/growth_qa_test.go` | `TestGROWTH_QA_AC063_AbsurdRetryAfterIsBounded` (3 subtests) | AC-063 |
| `internal/agent/growth_qa_test.go` | `TestGROWTH_QA_AC068_OddYAMLScalarsStayInRange` (96 subtests) | AC-068 |
| `internal/agent/growth_qa_test.go` | `TestGROWTH_QA_AC066_ShortBodyUnderLyingContentLengthLeavesNoPartial` | AC-066 |
| `internal/agent/growth_qa_test.go` | `TestGROWTH_QA_AC066_LyingLowContentLengthCannotExceedAnnounced` | AC-066 |
| `internal/agent/growth_qa_test.go` | `TestGROWTH_QA_AC053_RealFilesystemUnwritableIdentityNeverRegisters` | AC-053 |
| `internal/agent/collectors/growth_qa_test.go` | `TestGROWTH_QA_AC056_ScanLineBoundStopsTinyLines` | AC-056 (closes a false-coverage gap) |
| `internal/agent/collectors/growth_qa_test.go` | `TestGROWTH_QA_AC057_HugeAndHostileLinesStayBounded` (5 subtests) | AC-057 |
| `internal/agent/collectors/growth_qa_test.go` | `TestGROWTH_QA_AC057_OneMegabyteFrameIsTruncatedNotKeptWhole` | AC-056, AC-057 |

No implementation file was changed by QA.

## Deviations from ADD

All are recorded in the implementation report; the orchestrator approved items 1 to 3.

1. 408 keeps the item (ADD: permanent 4xx is anything but 401, 403, 429).
2. SQLite `auto_vacuum=INCREMENTAL` with an explicit `incremental_vacuum` instead of `FULL`.
3. `scripts/riot-agent.service` carries the same three unit lines (not in ADD section 4.5).
4. `collector.go` is modified (`TailLines` removed, limit setters added) and `Validate()` only logs while `Limits()` clamps (ADD section 4.5 says "`Validate()` clamps with one Warn"). Behaviour matches AC-068.
5. First container-log pass after upgrade reads up to 1,000 lines per container (it read 50). Follows AD-017.

No other deviation found. The helper's name length is derived from `tempSuffixBytes`, as SEC-023 requires.

## Deviations from FRD

FR-064 asks for `MemoryMax=256M`; the ADD (D-29, confirmed by the product owner, ADD section 14) replaces it with `MemoryMax=50%`, `GOMEMLIMIT=200MiB`, `OOMPolicy=continue` and an accepted residual that the README states. No other FRD deviation.

## Standards and disclosure checks

- Project rule on new config keys and unit lines: README documents all six keys in the Agent Config Reference and the unit lines in "Memory limit"; `internal/agent/doctor.go` reports the resolved limits (Collection Limits) and the unit's `MemoryMax`, `OOMPolicy`, `KillMode` and `GOMEMLIMIT` (Memory Limit); the sample `agent.yaml` template carries commented examples. No new collector, so no `collector.enabled` change.
- The implementation report states which changes need an `install.sh` re-run (the unit memory lines and the helper sweep) and which arrive with the auto-updated binary. README ("Upgrading existing agents", item 5) and CHANGELOG say the same.
- Public-repository scrub: the added lines contain no host name, LAN address, local or home path, domain or personal name. The only IP is the documentary `1.2.3.4` in a test fixture. Commit messages are clean. No FRD, ADD or security review is copied in.
- `go.mod` / `go.sum` unchanged.

## Verdict Rationale

PASS WITH NOTES: every PR-C AC and condition has a named test that fails when the control is broken (one false-coverage gap found and closed by QA), all tests are green on Windows and under the Linux race detector, the rebase kept both SEC-002 and PR-C behaviour, and the adversarial probes found only bounded behaviour. The notes above are minor and non-blocking.

## Action Required

None. Notes 3 and 4 are worth a sentence in the PR description.
