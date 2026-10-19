# Implementation Report: GROWTH-001 PR-C (agent-side bounds)

**Story ID:** GROWTH-001, PR-C (agent)
**Engineer:** Senior Dev Agent
**Date:** 2026-10-19
**Base:** `main@8873911`
**Branch:** `growth-001-pr-c-impl`
**Detected stack:** Go 1.24 (`go.mod`), chi v5 / pgx v5 on the server (untouched here), `modernc.org/sqlite` for the agent buffer, `gopkg.in/yaml.v3`, `testify`, `docker/docker` client. POSIX shell (`scripts/install.sh`) and a systemd unit. No frontend change.

This PR carries code, tests, README, CHANGELOG and this report only. It does not carry the FRD, the ADD or the security review (SEC-037, D-77). It does not edit `internal/agent/docker_update.go` or `internal/server/handlers/auto_update.go`. It needs no server change, no migration, no new dependency (`go.mod` and `go.sum` are unchanged) and no new collector or sudoers rule.

Per ADD rev 3.1 section 3.1 the release-content rule applies (D-66): this PR must not ship in a release without PR-A1.

## Completed components

| File | Action | Notes |
|---|---|---|
| `internal/agent/config.go` | modified | Six optional keys (`agent.buffer_max_mb`, `collectors.container_logs.{max_lines,max_bytes,max_line_bytes}`, `collectors.logs.{max_entries_per_pass,max_line_bytes}`) using a lenient `LimitInt`. `Validate()` logs one Warn per adjusted key and is called by `LoadConfig`. `Limits()` returns the resolved, always in-range values. `Save` is now atomic (reuses `atomicJSONFile`) and `SaveWith` takes the injectable file system. Template comments added |
| `internal/agent/http_client.go` | modified | `StatusError` (bounded body, parsed `Retry-After` in seconds or HTTP date), telemetry-only client-wide `backoffUntil`, `ErrBackoff`, injected `now`. 429, 401 and 403 back off (`Retry-After` clamped to [1 s, 1 h], else 30 s doubling to 10 min, reset by a success). A 5xx honours `Retry-After` when present, else retries on the next tick. Heartbeats and the other routes are untouched |
| `internal/agent/buffer.go` | rewritten | Byte cap plus the 100-item cap, oldest evicted first with a Warn; `Oldest`/`Delete` per-item acknowledgement; `auto_vacuum=INCREMENTAL` with an explicit `PRAGMA incremental_vacuum` after eviction and after each flush; one-time `VACUUM` conversion of old files that never stops the agent; `quick_check` on open; corrupt file moved aside with its `-journal`, `-wal`, `-shm` siblings replacing the previous set; undecodable rows deleted; item larger than the cap refused (`errItemTooLarge`) |
| `internal/agent/telemetry.go` | modified | Per-item flush (2xx removes, permanent 4xx drops with a Warn, 401/403/408/429/5xx/transport keeps and stops), live-push classification (a permanent 4xx is no longer buffered), 6 MiB body guard (container logs first, then the journal) |
| `internal/agent/register.go` | rewritten | Identity pre-flight (4 KiB written and fsynced through the atomic helper in the ID directory and the config directory, then removed), in-process backoff 5 s doubling to 10 min, never calls `Register` while failing. ID is written first, then the config. Fatal (`errIdentityNotPersistable`, carries the path) only when the identity changed; a same-ID re-registration only warns |
| `internal/agent/agent.go` | modified | Buffer cap from config, agent-start staging sweep, exit path (`exitUnpersistableIdentity`: log path and error, 60 s pause, return the error so the process exits 1), `fatalCh` plus `reregister` so the 24 h re-registration can end the process on an ID mismatch, collector limits wired in `New` |
| `internal/agent/updater.go` | modified | 64 MiB download cap (announced length refused unread; `LimitReader(64 MiB + 1)`), partial file deleted on any failure, `sweepStaleStaging` |
| `internal/agent/collectors/docker_logs.go` | rewritten | `Tail = max_lines` plus `Since`, per-container ring (`capRing`) bounded by lines and bytes, scan bound 20,000 lines and 8 MiB, pass bound 5,000 lines and 4 MiB (containers without budget are deferred, their cursor does not move), `TruncateLogLine` (rune-safe, ` [truncated]`), test seam `Host` |
| `internal/agent/collectors/logs.go` | rewritten | Journal streamed without `-n` through a ring of the newest entries (5,000) bounded to 2 MiB, scan bound 500,000 lines or 256 MiB (then `journalctl` is killed and the cursor continues next pass), 1 MiB per-line bound, exact skipped count, auth counting stays exact for every line read. `parseAndCount` and `collectFromOutput` keep their signatures |
| `internal/agent/collectors/collector.go` | modified | `SetContainerLogLimits`, `SetJournalLimits`; `TailLines` removed (see deviation 7) |
| `internal/agent/commands.go` | modified | `fetch_logs` truncation only |
| `internal/agent/doctor.go` | modified | **Collection Limits** section (resolved values) and, on Linux, a **Memory Limit** section (`MemoryMax` bytes, `infinity` fails, `OOMPolicy`, `KillMode`, `GOMEMLIMIT`) |
| `cmd/riot-agent-apply/main.go` | modified | Stale temp sweep in both modes. Name match derived from `tempSuffixBytes` (`tempNameHexLen = 2 * tempSuffixBytes`, lowercase hex), `Lstat` regular files only, older than 1 h, directory owned by root and not writable by group or others, `os.Remove` |
| `scripts/install.sh` | modified | Unit gains `MemoryMax=50%`, `Environment=GOMEMLIMIT=200MiB`, `OOMPolicy=continue` |
| `scripts/riot-agent.service` | modified | Same three lines (deviation 3) |
| `README.md`, `CHANGELOG.md` | modified | Config keys, "Agent Resource Bounds", memory limit caveat (D-29 residual), "re-run install.sh", doctor |
| Tests | created/modified | `config_test.go`, `http_client_test.go`, `buffer_test.go`, `telemetry_flush_test.go`, `register_test.go`, `updater_growth_test.go`, `doctor_test.go`, `legacy_server_test.go`, `collectors/docker_logs_test.go`, `collectors/logs_test.go`, `cmd/riot-agent-apply/main_test.go` |

## Commits

```
[GROWTH-001] feat: bounded agent config keys with lenient parsing and atomic Save
[GROWTH-001] feat: telemetry back-off on 429/401/403 with Retry-After and StatusError
[GROWTH-001] feat: byte-capped self-healing buffer with per-item acknowledgement
[GROWTH-001] feat: cap self-update download at 64 MiB and sweep stale staging files
[GROWTH-001] feat: identity pre-flight with backoff and fail-fast on an unpersistable identity
[GROWTH-001] feat: bound container-log and journal collection passes
[GROWTH-001] feat: apply configured collector limits and truncate fetch_logs messages
[GROWTH-001] feat: helper sweep of stale apply temp files with exact name and root-owned directory checks
[GROWTH-001] config: cap agent memory in the service unit and report limits in doctor
[GROWTH-001] test: a new agent delivers its pushes and backlog to a v2.67.0 server
[GROWTH-001] test: periodic re-registration fails hard only on an unpersistable ID mismatch
[GROWTH-001] docs: agent resource bounds, memory limit caveat and install.sh re-run
```

(Plus this report, committed last.)

## Test summary

Test names carry the AC or condition they cover. "Fail path" lists the failure behaviour the product owner asked to be tested.

| AC / condition | Test file | Tests | Status |
|---|---|---|---|
| AC-053, C-17 (SEC-016) | `internal/agent/register_test.go` | `TestGROWTH_AC053_UnwritableIdentityNoRegistration`, `..._ENOSPCOnWriteNoRegistration` (full disk: create works, write fails; zero `Register` calls, no temp file left), `..._RegistersAndPersistsWhenWritable`, `..._IDWriteFailingAfterRegistrationIsFatalAndNamesThePath`, `..._ConfigWriteFailingAfterRegistrationIsFatal`, `..._PeriodicReRegistrationWithTheSameIDOnlyWarns`, `..._PeriodicReRegistrationWithADifferentIDIsFatal`, `..._PeriodicLoopSignalsRunOnlyForAnUnpersistableIDMismatch`, `..._ExitPathLogsPathWaitsSixtySecondsAndReturnsTheError`; `TestGROWTH_SEC016_PreflightBackoff` (5 s doubling, cap 10 min, one log per step), `..._PreflightWritesAndSyncsAtLeast4KiBThroughTheAtomicPath` | pass |
| AC-056 | `collectors/docker_logs_test.go` | `TestGROWTH_AC056_NewestThousandLinesBoundedRead` (50,000 lines to newest 1,000; Tail and Since asserted against a fake daemon; a daemon that ignores Tail is cut off at the scan bound), `..._ByteAndPassBoundsHold` (256 KiB per container, 5,000 lines and 4 MiB per pass, deferred containers served next pass), `..._CapRingKeepsNewestAndCountsEvictions` | pass |
| AC-057 | `collectors/docker_logs_test.go`, `collectors/logs_test.go` | `TestGROWTH_AC057_LongLinesTruncatedAtRuneBoundary` (10 KiB to 4 KiB plus marker; a euro-sign string cut at 4,095 bytes), `TestGROWTH_AC057_JournalMessagesTruncatedAtRuneBoundary` | pass |
| AC-058, SEC-022 | `collectors/logs_test.go` | `TestGROWTH_AC058_JournalKeepsNewest5000Logs2000Skipped`, `..._SkippedCountIsExact`, `..._MaxEntriesFromConfigIsHonoured`, `..._AuthCountingStaysExactForEvictedEntries`, `..._CursorAdvancesPastSkippedEntries`; `TestGROWTH_SEC022_JournalRingBoundedInBytesAsWellAsEntries`, `..._OversizeJournalLineIsSkippedAndCounted`, `..._JournalScanStopsAtItsBounds` (500,000 lines; 256 MiB), `..._StoppedScanKillsTheProcessAndNormalEndDoesNot` | pass |
| AC-059, C-18 | `internal/agent/doctor_test.go` | `TestGROWTH_AC059_InstallerUnitAndDoctorMemoryLimit` (install.sh unit and `riot-agent.service` carry the three lines in `[Service]`; doctor shows bytes, `OOMPolicy`, `KillMode`, `GOMEMLIMIT`; `infinity` and the legacy 2^64-1 fail; query failure warns) | pass |
| AC-060 | `internal/agent/buffer_test.go`, `telemetry_flush_test.go` | `TestGROWTH_AC060_BufferByteCapEvictsOldest` (cap holds, oldest evicted, Warn, item cap, oversize item refused without evicting, counters survive restart), `TestGROWTH_AC060_OversizedLiveSnapshotIsDroppedNotStored` | pass |
| AC-061 | `telemetry_flush_test.go` | `TestGROWTH_AC061_AckedItemNeverResent`, `..._SuccessfulLivePushFlushesTheBacklog` | pass |
| AC-062, C-9 (SEC-009 agent) | `telemetry_flush_test.go`, `buffer_test.go`, `http_client_test.go` | `TestGROWTH_AC062_Rejected400ItemDroppedOthersDelivered` (rejected item offered once, never again), `..._PermanentStatusesDropAndTransientOnesKeep` (400, 404, 413, 422 drop; 401, 403, 408, 429, 500, 503 keep), `..._401And403KeepItem`, `..._LivePushRejectedIsDroppedNotBuffered`, `..._LivePushKeptOnRetryableStatuses`, `..._LivePushDuringBackoffIsBufferedWithoutNetwork`, `..._UndecodableBufferedItemDroppedNotRetried`, `TestGROWTH_AC062_401And403BackOffLikeA429` | pass |
| AC-063 | `http_client_test.go`, `telemetry_flush_test.go` | `TestGROWTH_AC063_RetryAfterParsing`, `..._429HonoursRetryAfter` (injected clock: held at 29 s, allowed at 30 s; clamp to 1 h; 30 s doubling to 10 min; success resets), `..._OtherStatusesDoNotBackOff` (heartbeats not gated), `..._429HonoursRetryAfter_Flush` | pass |
| AC-064 | `buffer_test.go` | `TestGROWTH_AC064_BufferFileShrinksAfterDelivery` (an existing 40 MiB legacy file ends below 1 MiB; a new file shrinks after deliveries) | pass |
| AC-065, SEC-022 | `buffer_test.go` | `TestGROWTH_AC065_CorruptBufferRecoveredOneAsideSet`, `TestGROWTH_SEC022_SiblingFilesMovedVacuumENOSPCContinues` (siblings moved, previous set replaced; `VACUUM` failing with `ENOSPC` keeps the file and the data, retried and completed at the next start) | pass |
| AC-066 | `updater_growth_test.go` | `TestGROWTH_AC066_DownloadStopsAt64MiBNoPartial` (100 MiB unannounced stream; announced oversize refused unread; exactly 64 MiB accepted; 64 MiB + 1 fails; whole pipeline leaves none of the three staging files), `..._FailedDownloadIsReportedAsAgentUpdateFailed` | pass |
| AC-067 (agent), C-19 | `updater_growth_test.go` | `TestGROWTH_AC067_AgentStartSweepsStaleStaging` (2 h old removed, 10 min kept, 1 h boundary, directory and (Linux) symlink left alone) | pass |
| AC-067 (helper), C-19 (SEC-023) | `cmd/riot-agent-apply/main_test.go` | `TestGROWTH_AC067_HelperSweepsStaleTempOnly` (both modes, planted symlink and prefixed directory untouched), `TestGROWTH_SEC023_HelperSweepRequiresRootOwnedDirExactName` (name derived from `tempSuffixBytes`; too short, too long, upper-case and non-hex names kept; wrong owner, group-writable and other-writable directories not swept) | pass (Linux only, see run output) |
| AC-068 (agent) | `config_test.go` | `TestGROWTH_AC068_EveryAgentBoundFiniteInRange` (each of the six keys x 0, negative, huge, int64 overflow, garbage word, float, bool: in range, exactly one Warn naming the key), `..._InRangeAndUnsetValuesDoNotWarn`, `..._DefaultsMatchTheDocumentedValues`, `..._SaveKeepsConfiguredLimitsAndOmitsUnset`; `TestGROWTH_SEC016_ConfigSaveIsAtomicAndLeavesNoTempFile` | pass |
| AC-073 | `legacy_server_test.go` | `TestGROWTH_AC073_NewAgentDeliversToLegacyServer` (503 plain text while down, no `Retry-After`, no back-off; live push plus backlog all delivered; no new top-level snapshot field) | pass |
| Body guard (D-36) | `telemetry_flush_test.go` | `TestGROWTH_D36_BodyGuardDropsContainerLogsThenJournal` | pass |

### Test run output

Windows (the developer machine), no `-race`:

```
$ go build ./...                      (clean)
$ go vet ./...                        (clean)
$ GOOS=linux go vet ./...             (clean)
$ go test ./... -count=1
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-server	[no test files]
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer	1.495s
ok  	github.com/DesyncTheThird/rIOt/internal/agent	21.251s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	10.470s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.619s
ok  	github.com/DesyncTheThird/rIOt/internal/releasekey	0.852s
ok  	github.com/DesyncTheThird/rIOt/internal/resilient	1.734s
ok  	github.com/DesyncTheThird/rIOt/internal/server	3.158s
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	0.603s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	0.614s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	0.666s
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	0.851s
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	0.660s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	8.158s
?   	github.com/DesyncTheThird/rIOt/internal/server/logstore	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	0.688s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	0.930s
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc	2.270s
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	0.988s
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	0.562s
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary	0.684s
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	0.585s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	0.752s
?   	github.com/DesyncTheThird/rIOt/internal/testutil	[no test files]
```

The server packages ran unchanged (PG-gated tests skip without `RIOT_TEST_PG_URL`, as at baseline).

Linux, with the race detector, as a non-root user, in a `golang:1.24` container with the worktree mounted read-only (WSL could not be used from this environment, so the Linux-only helper tests were executed this way):

```
$ go test -race ./internal/agent/... ./cmd/riot-agent-apply/ -count=1
ok  	github.com/DesyncTheThird/rIOt/internal/agent	45.007s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	34.897s
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	1.303s
```

This run also executes the symlink subtests that are skipped on Windows.

New tests only (`go test ./internal/agent/... -run TestGROWTH -v`, top-level lines): all 55 top-level tests pass (41 in `internal/agent`, 14 in `collectors`); the `cmd/riot-agent-apply` run (`-run GROWTH -v`) passes 2 top-level tests with 11 subtests.

Linter: `go vet` is the project linter and is clean on Windows and with `GOOS=linux`. `gofmt -l` is clean on every new or fully rewritten file. The existing files that were edited in place are not gofmt-clean at the base commit (struct alignment, as in `config.go`), and they were not reformatted, per the "match the style of the file" standard.

## Deviations from the ADD

Items 1, 2 and 3 were approved by the orchestrator before work started. All trigger architect review.

1. **408 is transient (keep and retry).** ADD AD-017 and G-08 say a permanent 4xx is "anything other than 401, 403 and 429". The implementation also keeps 408, so the dropped set is every 4xx except 401, 403, 408 and 429. A request timeout is transient by definition; dropping the item would lose data for a retryable condition.
2. **`auto_vacuum=INCREMENTAL`, not `FULL`.** The product owner's brief asked for INCREMENTAL. The buffer calls `PRAGMA incremental_vacuum` explicitly after an eviction and after each flush, and converts an existing file once with `VACUUM` (a failure, including `ENOSPC`, never stops the agent and is retried at the next start). Note for implementers: `Exec` of `PRAGMA incremental_vacuum` frees only one page, so the code drains the result rows (found by a failing test).
3. **`scripts/riot-agent.service` also edited** (not in section 4.5), with the same three unit lines as `install.sh`, so the two unit definitions in the repository do not disagree.
4. **Report path.** The ADD (section 4.5) names `docs/implementation/GROWTH-001-impl-report-C.md`; this report is at `docs/implementation/GROWTH-001-PR-C-impl-report.md` as the orchestrator instructed.
5. **`internal/agent/collectors/collector.go` is modified** though not listed in section 4.5: `TailLines` is removed (it was the old 50-line tail, replaced by `max_lines`) and `SetContainerLogLimits` / `SetJournalLimits` carry the clamped config into the collectors, mirroring `SetSMARTInterval`.
6. **First container-log pass reads up to 1,000 lines per container, not 50.** This follows AD-017 (`Tail = max_lines`) but is a visible change: after an upgrade the first pass sends more history. It is bounded by 256 KiB per container and 4 MiB per pass.
7. **`Validate()` only logs.** ADD section 4.5 says "`Validate()` clamps with one Warn". Clamping is done by `Limits()`, which returns the resolved values; `Validate()` (called from `LoadConfig`) emits the single Warn per adjusted key. The config struct itself keeps what the operator wrote, so `Config.Save` never rewrites a hand-edited value.
8. **ID file mode.** The ID is written through the atomic helper (mode 0600 temp file). After a successful write on a real file system the agent sets it back to 0644 (best effort), so `riot-agent doctor` run by another user still reads it as before.
9. **Small additions not spelled out in the ADD:** the journal `unit` field is also truncated (256 bytes); a single journal line over 1 MiB is skipped and counted; a snapshot larger than the whole buffer cap is refused instead of stored; an undecodable buffered row is deleted; `reregister` and `fatalCh` carry a fatal ID mismatch from the 24 h loop to `Run`. `parseDockerLogStream` was replaced by `readDockerLogStream` (it had no callers outside the collector).
10. **Test seams added** (exported only where the test is in another package): `ContainerLogCollector.Host`; `LogsCollector.openJournal`; `Agent.fsys`, `Agent.idPath`, `Agent.fatalCh`; the package variable `bufferConvertVacuum`; `installer.dirOwnerUID` and `installer.now` in the helper.

## Install.sh re-run needs on existing devices

**Needs an `install.sh` re-run (nothing else delivers it):**

- The memory limit: `MemoryMax=50%`, `GOMEMLIMIT=200MiB`, `OOMPolicy=continue` are unit-file lines; a new agent binary does not rewrite the unit. Until the re-run `riot-agent doctor` reports "no memory limit on the agent unit".
- The helper sweep: `riot-agent-apply` is not auto-updated; the installer re-installs it.

**Arrives with the auto-updated agent binary, no re-run and no config edit:** the six config keys (all optional), the byte-capped buffer and its conversion, per-item flush and 4xx handling, the 429/401/403 back-off, the identity pre-flight and fail-fast, the 64 MiB download cap, the agent-start staging sweep, the container-log and journal caps, `fetch_logs` truncation and the new doctor sections (the Memory Limit section will show a failure on a host that has not been re-run, which is the intended signal).

There is no new collector, no `collector.enabled` change and no sudoers change.

## Notes for QA

- **Linux-only code.** `cmd/riot-agent-apply` is `//go:build linux`. On Windows `go test ./cmd/riot-agent-apply/` reports "no test files"; run it on Linux (CI does) or in a container as done above. The helper tests need no root: the test user stands in for root through `installer.dirOwnerUID`.
- **systemd versions.** `MemoryMax=50%` needs systemd 239+, `OOMPolicy` needs 243+. An older systemd ignores unknown directives with a warning. cgroup v1 hosts do not enforce `MemoryMax`. These are documented in the README.
- **D-29 residual** (accepted by the product owner): an `apt`/`dpkg` started by the agent shares its cgroup, so an agent restart or a cgroup OOM can interrupt an upgrade. The follow-up (run package managers in their own transient unit, tracked with AGENT-PRIV) is not built here and is stated in the README.
- **Behaviour to probe:**
  - Fill the disk (or mount the config directory read-only) before the first start and confirm the agent never contacts the server, logs every backoff step, and recovers on its own when space returns.
  - Fill the disk during registration (hard to time): the agent must log the path and exit 1 after about 60 s.
  - A buffer file from the old agent (any size) converts once at start; on a full disk the agent logs "retrying at the next start" and keeps running.
  - A server restarting during a flush answers 5xx or 401: no buffered item may be lost.
  - Journal on a host with a very large backlog: the scan stops at 500,000 lines or 256 MiB and continues from the cursor on the next pass (the oldest part is read first, as specified).
- **Known gaps / risks (none unbounded):**
  - Docker's `since` is inclusive, so the last line of a container can be sent again in the next pass. This predates the change and is unchanged.
  - If `Delete` fails after the server acknowledged an item (a read-only buffer file), the item is sent again on the next tick: at most one duplicate per tick, nothing grows.
  - `PRAGMA quick_check` runs at every start; on a 500 MiB buffer this is noticeable (well under a second to a few seconds on a Pi-class disk).
  - `fetch_logs` truncation uses the same `TruncateLogLine` that is unit-tested; the command handler itself shells out to `journalctl` and is not unit-tested.
  - The Windows agent has no `fsync` of directories (`SyncDir` is a no-op there, as for the existing command state files).

## What the PR description must say

1. This is GROWTH-001 PR-C (agent). It carries code, tests, README, CHANGELOG and its own implementation report only (SEC-037). The story documents merge with PR-A1. It must not ship in a release without PR-A1 (D-66); AC-073 shows it also works against a v2.67.0 server.
2. No server change, no migration, no dependency, no new collector, no sudoers change; `docker_update.go` and `auto_update.go` are untouched.
3. **Existing devices need an `install.sh` re-run for the memory limit and the helper sweep.** Everything else arrives with the auto-updated binary. The product owner confirmed D-29 (2026-10-19) and accepted the residual (interrupted `apt`/`dpkg`); the README states it.
4. Deviations 1, 2 and 6 (408 kept; INCREMENTAL vacuum; first container-log pass reads 1,000 lines).
5. The bounds table (AC-069), five columns:

| Path | Cap | Enforcement | Self-heal after failure | Worst-case growth per device per day |
|---|---|---|---|---|
| G-31 Agent collection passes | Container logs: `max_lines` 1,000 and `max_bytes` 256 KiB per container; 5,000 lines and 4 MiB per pass; scan at most 20,000 lines and 8 MiB per container; lines 4 KiB. Journal: 5,000 entries and 2 MiB ring, scan at most 500,000 lines or 256 MiB, lines 4 KiB. Body guard 6 MiB | `capRing`, scan bounds, `guardTelemetryBody` | Oldest dropped and counted; the cursor continues next pass | At most 1,440 pushes x 6 MiB sent |
| G-32 Agent buffer | 100 items and `agent.buffer_max_mb` (50 MiB; 5-500) | `Buffer.Store` evicts oldest; `incremental_vacuum` | Corrupt file moved aside (one set, with siblings); failed conversion retried at the next start; rejected or undecodable item dropped | At most 50 MiB on disk |
| G-33 Agent memory | `MemoryMax=50%`, `GOMEMLIMIT=200MiB`, `OOMPolicy=continue` | systemd unit | OOM kill, then `Restart=always` | 0 (accepted D-29 residual) |
| G-34 Self-update | 64 MiB download; staging and helper temp files older than 1 h swept | `LimitReader`, agent-start sweep, helper sweep | Partial file deleted | At most 64 MiB |
| Identity pre-flight (SEC-016) | 4 KiB probe per directory; retry delay 5 s doubling to 10 min | `identityPreflightOnce`, `waitForWritableIdentity` | In-process retry; no `Register` while failing | 0 ghost devices from a full disk |
| Telemetry back-off | `Retry-After` clamped to [1 s, 1 h]; 30 s doubling to 10 min | `HTTPClient.backoffUntil` | A success resets it | 0 (requests withheld) |
