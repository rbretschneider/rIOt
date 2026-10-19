# Implementation Report: WP-GO-AGENT

**Story ID:** CMD-LIFECYCLE-001
**Work package:** WP-GO-AGENT (ADD section 4.2: `internal/agent/**`)
**Engineer:** Senior Dev Agent
**Date:** 2026-10-18

**Detected stack:** Go 1.24 module (`go.mod`), testify, gorilla/websocket, Docker SDK v28.3.3.
Local toolchain is Go 1.26 on Windows. The web stack (React 19, vitest) is out of scope here.
`go.mod` and `go.sum` are unchanged.

## Commits

| # | Commit | ADD 13.2 item | Content |
|---|---|---|---|
| 1 | `cf7f048` | 12 | Serialized WS writer, synchronized conn, backoff reset, terminal files lose the raw conn |
| 2 | `8ffcabf` | 13 | `cmdstate.go`, `outbox.go`, `PostCommandReport`, config paths |
| 3 | `6607b24` | 14 | `target_lock.go`, `command_lifecycle.go`, `executeAction`, heartbeat, `agent.go`, legacy-server test |
| 4 | `6292f49` | 15 | `proclimit.go` (SIGTERM, grace, SIGKILL on an injectable clock) |
| 5 | `191fcde` | 16 and 17 | Docker rewrite: `docker_api.go`, `docker_deps.go`, `docker_update.go`, `compose_sanitize.go`, `fileowner_*.go`, `InvalidateFreshness` (see Deviations, item 1) |
| 6 | `760652d` | 18 | `docker_recovery.go`, recovery driver, recovery tests |
| 7 | `c132b1c` | 19 | Sanitized-file tests (the code landed in commit 5) |
| 8 | `e6989cc` | 20 | Per-container freshness, negative TTL, per-ref invalidation |

## Completed components

| File | Action | Notes |
|---|---|---|
| `internal/agent/ws_client.go` | MODIFY | `connMu` for `conn`, `writeMu` serializing writes, `errNotConnected`, 10s write deadline, a failed write closes and forgets the conn, injectable `dial`/`sleep`, backoff resets after a successful dial, `command_receipt` and `OnWSConnected` hooks |
| `internal/agent/cmdstate.go` | CREATE | `atomicJSONFile` (exclusive 0600 temp, file fsync, rename, dir fsync, `stateFS` seam), `seenStore`, `inflightStore` with `dockerRecord`, `clock` |
| `internal/agent/outbox.go` | CREATE | WS first, HTTP fallback, 429/5xx/401/403 retried 5s doubling to 5m, legacy mode 1h, bounds 200 entries, 1 MiB file, 256 KiB entry, 24h |
| `internal/agent/target_lock.go` | CREATE | `TryLockAll`, `Unlock` (holder-checked), `HeldWithPrefix`, conflict text |
| `internal/agent/command_lifecycle.go` | CREATE | `submitCommand` (UUID, shutdown gate, fail-closed seen, marker, sync ack), `cmdRun`, notifier, panic recovery, result to outbox, startup recovery driver, `waitForCommands` |
| `internal/agent/commands.go` | MODIFY | `handleCommand` replaced by `executeAction(cr)`; host lock; `agent_update`/`agent_uninstall` Docker exclusion; `dockerCommand` refused while the target is locked; results carry `Step`/`Reason`/`Stderr` |
| `internal/agent/heartbeat.go` | MODIFY | Goes through `submitCommand` |
| `internal/agent/agent.go` | MODIFY | New fields, construction before the loops, recovery, outbox loop, `shuttingDown` plus 20s wait |
| `internal/agent/config.go` | MODIFY | `OutboxPath`, `SeenCommandsPath`, `InflightPath` |
| `internal/agent/http_client.go` | MODIFY | `PostCommandReport` with `url.PathEscape` on both IDs, 1 MiB reply cap |
| `internal/agent/proclimit.go` | CREATE | `runLimited`, `execProcess` |
| `internal/agent/docker_api.go` | CREATE | `dockerAPI`, `sdkDocker` adapter, factory seams |
| `internal/agent/docker_deps.go` | CREATE | Dependents by name, `/name`, full ID, 12+ char ID prefix, `service:`; `restoreDependents` with a validated compose fallback |
| `internal/agent/docker_update.go` | REWRITE | Name-first resolve, locks before any Docker call, compose steps, standalone rename-aside rollback, bulk, `composeArgs`, marker-before-mutation |
| `internal/agent/docker_recovery.go` | CREATE | `reconcileInterrupted`, `cleanupSanitizedLeftovers` |
| `internal/agent/compose_sanitize.go` | CREATE | `sanitizeComposeFile` (moved), `writeSanitizedCompose` (Lstat, owner check, `O_EXCL` 0600), `removeSanitized` |
| `internal/agent/fileowner_unix.go`, `fileowner_windows.go` | CREATE | `ownedByEUID` |
| `internal/agent/terminal.go`, `host_terminal.go`, `host_terminal_windows.go` | MODIFY | Connection parameter removed, writes via `a.sendWS` |
| `internal/agent/collectors/docker.go` | MODIFY | `ImageID`, per-container verdict, per-ref cache, sha256 pinning, `InvalidateFreshness`, `freshnessAPI` seam |
| `internal/agent/collectors/docker_cache.go` | MODIFY | New entry shape, per-entry TTL (30m, 5m failed), injectable clock |
| `internal/agent/collectors/docker_test.go` | MODIFY | Fixtures adapted to the new entry shape |
| Tests | CREATE | `ws_client_test.go`, `cmdstate_test.go`, `outbox_test.go`, `http_client_test.go`, `target_lock_test.go`, `command_lifecycle_test.go`, `legacy_server_test.go`, `proclimit_test.go`, `docker_deps_test.go`, `docker_update_test.go`, `docker_recovery_test.go`, `compose_sanitize_test.go`, `fakes_test.go`, `fakes_docker_test.go`, `collectors/docker_freshness_test.go` |

`internal/agent/doctor.go` and `scripts/install.sh` are unchanged. No collector was added, so the
collector whitelist is unchanged.

## AC to test table

All names are `TestCL_<name>`. "Status" is the result on Windows. The two Linux-only tests are
skipped there and pass in the Linux run below.

| AC / condition | Test(s) | File | Status |
|---|---|---|---|
| AC-002 | `AC002_ResolvesByNameOverStaleID` | docker_update_test.go | pass |
| AC-003 | `AC003_ResolveIgnoresLeadingSlash` | docker_update_test.go | pass |
| AC-004 | `AC004_UnresolvedTargetFailsAtResolve` | docker_update_test.go | pass |
| AC-013 | `AC013_ConcurrentSendsNeverOverlap` | ws_client_test.go | pass |
| AC-014 | `AC014_ConnectDisconnectSendRaceFree` (race detector on Linux) | ws_client_test.go | pass, pass under `-race` |
| AC-015 | `AC015_SendReturnsWriteError`, `AC015_WriteErrorKeepsOutboxEntry` | ws_client_test.go, outbox_test.go | pass |
| AC-016 | `AC016_DisconnectedResultPersistedThenPosted`, `AC016_ReceiptRemovesEntryElseHTTPFallback`, `AC016_ResultOfDisconnectedCommandPostedOverHTTP` | outbox_test.go, command_lifecycle_test.go | pass |
| AC-017 | `AC017_Http500BackoffThenRemovedOn2xx`, `AC017_ReplyClassificationTable` | outbox_test.go | pass |
| AC-018 | `AC018_EntrySurvivesRestart` | outbox_test.go | pass |
| AC-019 | `AC019_OverflowDropsOldestWithWarning`, `AC019_OversizedOutputFitsPerEntryBound` | outbox_test.go | pass |
| AC-020 | `AC020_EntryOlderThan24hPruned` | outbox_test.go | pass |
| AC-021 | `AC021_InterruptedDockerUpdateReportedFailed`, `AC021_MarkerWithQueuedResultNotReportedTwice`, `AC021_InflightMarkerSurvivesRestart`, `AC021_InflightDockerRecordsAreKeyed` | command_lifecycle_test.go, cmdstate_test.go | pass |
| AC-022 | `AC022_SelfUpdateMarkerNotReported` (agent_update, reboot, shutdown) | command_lifecycle_test.go | pass |
| AC-025 | `AC025_404EntersLegacyModeRemovesAfterWSWrite`, `AC025_NonJSONReplyYieldsEmptyResponse` | outbox_test.go, http_client_test.go | pass |
| AC-032 | `AC032_DependentByFullParentID`, `AC032_ShortIDPrefixNeedsTwelveCharacters` | docker_deps_test.go | pass |
| AC-033 | `AC033_DependentByNameSlashNameService` | docker_deps_test.go | pass |
| AC-034 | `AC034_SingleServiceStopsOnlyItsDependents`, `AC034_BulkUpdatesChildrenBeforeParents` | docker_update_test.go | pass |
| AC-035 | `AC035_DependentOnNewParentNamespace` | docker_update_test.go | pass |
| AC-036 | `AC036_DependentRestoredAfterPullFailure` | docker_update_test.go | pass |
| AC-037 | `AC037_RestoreHasOwnBudgetAfterUpTimeout` | docker_update_test.go | pass |
| AC-038 | `AC038_StoppedDependentStaysStopped` | docker_update_test.go | pass |
| AC-039 | `AC039_FailedRestoreFailsCommand` | docker_update_test.go | pass |
| AC-040 to AC-048 | `AC040_PullErrorDetailFailsAtPull`, `AC041_SameImageSkipsRecreate`, `AC042_OldRenamedAsideRemovedAfterVerify`, `AC043_CreateFailureRollsBack`, `AC044_StartFailureRemovesReplacement`, `AC045_ExitDuringVerifyRollsBack`, `AC046_StoppedStaysStopped`, `AC047_RollbackRenameFailureKeepsAside`, `AC048_LeftoverAsideBlocksUpdate` | docker_update_test.go | pass |
| AC-049 | `AC049_UpGetsFullBudgetAfterSlowPull`, `AC049_ProcessFinishingBeforeLimitIsNotSignalled`, `AC049_BulkSkipsRemainingWhenBudgetExhausted` | docker_update_test.go, proclimit_test.go | pass |
| AC-050 | `AC050_SigtermThenSigkillAfterGrace` (+ well-behaved child, parent cancel, start failure, real exec wrapper) | proclimit_test.go | pass |
| AC-051 | `AC051_ExitWithinGraceNoSigkill` | proclimit_test.go | pass |
| AC-052 | `AC052_PullLimitMessage` | docker_update_test.go | pass |
| AC-053 | `AC053_EventAndResultAfterOpCtxExpired`; `update_failed` after expiry also asserted in AC-037 | command_lifecycle_test.go, docker_update_test.go | pass |
| AC-054 to AC-060 | `AC054_PulledNotRecreated`, `AC055_RegistryNewer`, `AC056_AllMatchUpToDate`, `AC057_SameTagDifferentVerdictsOneLookup`, `AC058_DigestRefsPinnedNoRegistryCall`, `AC059_RegistryErrorIsCheckFailed`, `AC060_FailedCheckCachedFiveMinutes` (+ 30m success cache, manual-pull-in-window, local-inspect failure, ImageID carried) | collectors/docker_freshness_test.go | pass |
| AC-061 | `AC061_InvalidateOnlyInvolvedRefs` (memory and disk), `AC061_DockerUpdateInvalidatesItsRefs` | collectors/docker_freshness_test.go, docker_update_test.go | pass |
| AC-063 | `AC063_SecondUpdateFailsPrecheckNoDockerCalls` (+ lock table, lifecycle, bulk conflict) | docker_update_test.go, target_lock_test.go, command_lifecycle_test.go | pass |
| AC-064 | `AC064_StackRejectedDuringServiceUpdate` | docker_update_test.go | pass |
| AC-065 | `AC065_DifferentStandaloneTargetsBothRun` | docker_update_test.go | pass |
| AC-066 | `AC066_LockReleasedAfterFailureAndPanic` (lock unit), `AC066_LockReleasedAfterPanickingCommand` (lifecycle; renamed, see Deviations) | target_lock_test.go, command_lifecycle_test.go | pass |
| AC-067 | `AC067_SameIDViaWSAndHeartbeatRunsOnce` (+ seen store persistence, cap 1000, 48h) | command_lifecycle_test.go, cmdstate_test.go | pass |
| AC-078 | `AC078_BackoffResetsAfterConnect` | ws_client_test.go | pass |
| AC-092 | `AC092_AckBeforeFirstStep` (WS, HTTP, legacy skip) | command_lifecycle_test.go | pass |
| AC-093 | `AC093_ComposeProgressOrder` (+ verify failure) | docker_update_test.go | pass |
| AC-094 | `AC094_StderrTail8KiBAndFailureStep` (agent side) | command_lifecycle_test.go | pass |
| AC-099 | `AC099_FinalResultLegacyStatusPlusFields` | command_lifecycle_test.go | pass |
| AC-100 | `AC100_NewAgentAgainstLegacyServer` (real WS over httptest, legacy 404) | legacy_server_test.go | pass |
| NFR-004 | `NFR004_OldContainerNeverRemovedBeforeVerify` (table), `NFR004_RecoveryNeverRemovesContainers` (table) | docker_update_test.go, docker_recovery_test.go | pass |
| SEC C-1 (agent) | `SEC001_Outbox429RetriesWithBackoff` | outbox_test.go | pass |
| SEC C-4 | `SEC004_MarkerPersistedBeforeRenameAndDependentStop`, `SEC004_RecoveryRenamesSetAsideBackWhenOriginalMissing`, `SEC004_RecoveryLeavesBothWhenOriginalExists`, `SEC004_RecoveryRestartsRecordedStoppedDependents`, `SEC004_AgentUpdateRejectedWhileDockerLockHeld`, `SEC004_AgentUninstallRejectedWhileDockerLockHeld`, `SEC004_NoNewCommandsAcceptedAfterSIGTERM` (+ marker-write abort, shutdown wait, symmetric refusal, Docker unreachable) | docker_update_test.go, docker_recovery_test.go, command_lifecycle_test.go | pass |
| SEC C-5 | `SEC005_SanitizedFileRefusesPreexistingSymlink` (Linux), `SEC005_SanitizedFileMode0600`, `SEC005_SanitizedFileRemovedOnEveryExit` (success, pull failure, up failure, panic), `SEC005_StartupRemovesLeftoverSanitizedFile` | compose_sanitize_test.go | pass (symlink: skipped on Windows, pass on Linux) |
| SEC C-7 | `SEC007_NonUUIDCommandIDNotExecuted` (WS and heartbeat, 1 MiB id), `SEC007_ReportURLPathEscaped` | command_lifecycle_test.go, http_client_test.go | pass |
| SEC C-8 | `SEC008_SeenPersistFailureFailsPowerActionsClosed` (reboot, shutdown, os_update, agent_update), `SEC008_SeenPersistedBeforeExecute` | command_lifecycle_test.go | pass |
| SEC C-9 | `SEC011_DependentServiceNameValidatedOrSeparated`, `SEC011_CleanupAndRollbackUseOldContainerID` (+ `SEC011_ComposeArgsHygiene`) | docker_deps_test.go, docker_update_test.go | pass |
| SEC C-11 (agent) | `SEC_AgentStateFilesMode0600` (Linux) | cmdstate_test.go | skipped on Windows, pass on Linux |

## Deviations from the ADD

These trigger architect review.

1. **Commit grouping.** Items 12 to 15 and 18 to 20 are one commit each. Items 16 and 17
   (and the code of 19) are one commit (`191fcde`): `docker_update.go`, `docker_deps.go`,
   `compose_sanitize.go` and `composeArgs` reference each other and would not build apart.
   Commit `c132b1c` (item 19) carries only the sanitize tests. `InvalidateFreshness` also
   landed in `191fcde` because the update path calls it; the rest of the freshness work is in `e6989cc`.
2. **`dockerAPI` is satisfied by an adapter, not `*client.Client` directly.** `sdkDocker` wraps the client.
   The SDK's `ContainerCreate` takes `*ocispec.Platform`, and naming that type needs a direct import of
   `github.com/opencontainers/image-spec`, which is an `// indirect` entry in `go.mod`. That would make
   `go.mod` tidy-dirty, and the DoD requires it byte-identical. The adapter drops the platform argument
   (the agent always passed nil) and the variadic options of `ImageInspect`. A compile-time assertion
   (`var _ dockerAPI = sdkDocker{}`) is in place.
3. **Intake order.** The UUID check runs before the shutdown gate, not after. A non-UUID ID must never
   reach the seen store or the outbox (C-7), and a refusal at the gate writes both.
4. **`submitCommand` is synchronous up to the execution goroutine.** The WS loop and the heartbeat call it
   directly, without `go`, so the seen record, the marker and the ack are on the wire before execution
   and `cmdWG.Add` happens before SIGTERM handling can miss the command. The cost is that an HTTP-fallback
   ack (at most 5s) can briefly hold the read loop or the heartbeat loop.
5. **`clock`/`realClock` live in `cmdstate.go`**, not `proclimit.go`, because the outbox (commit 13) is its
   first user. `sleepContext` lives in `ws_client.go`.
6. **`dockerRecord` has an extra `key` field** (the container name, or `compose:<dir>`) so bulk updates
   can keep one record per container. This is additive to the section 5.4 schema.
7. **Test file added.** `fakes_docker_test.go` holds the fake daemon, fake compose processes and the
   Docker harness. The ADD lists only `fakes_test.go`.
8. **Test name.** The ADD gives `AC066_LockReleasedAfterFailureAndPanic` for two files in one package.
   The lifecycle variant is named `AC066_LockReleasedAfterPanickingCommand`.
9. **Standalone pull limit uses a `context` deadline, not the injected clock.** The 15m standalone pull
   timeout (`StandalonePullLimit`) therefore has no deterministic unit test. The compose pull/up limits
   (AC-049, AC-052) are tested with the fake clock. The code path is the same `pullFailure` mapping to
   `timed out after 15m` with reason `timeout`.
10. **Stale non-compose dependents are reported, not rebuilt.** A standalone dependent whose
    `container:<id>` reference points at a replaced parent cannot be restarted without recreating it;
    it is reported at `restart_dependents`. Compose dependents are rebuilt through the validated
    `compose up -d --no-deps --force-recreate -- <svc>` fallback. Name-based references resolve to the
    new parent on their own, which is the common case.
11. **`stop_dependents` and `restart_dependents` are reported only when there are dependents** (AC-093
    shows the steps without them).
12. **Dependents are stopped with a 10s container stop timeout** inside the 20s budget; the ADD does not
    give a per-container value.
13. **Bulk.** Containers skipped by the wall-time budget count as failed (result status `error`, step
    of the first failure). The bulk resolves the requested IDs from one container list and silently ignores
    IDs that no longer exist; it fails at `precheck` only when none exist.
14. **Existing collector tests adapted** (`docker_test.go`): fixtures use the new cache entry shape, and
    the QA cache-hit test now supplies a fake image inspect, because the tag is inspected locally every
    cycle (this is what detects a manual pull inside the cache window).
15. **Compose `--` check (ADD 12.17).** Docker Compose v2.26.1 (local) accepts `--project-directory=<dir>`,
    `-f=<file>` and `--` before service names for `up` and `pull`. `--` is kept.
16. **Recovery of `reboot`, `shutdown`, `agent_update` markers** is silent removal (they are excluded from
    FR-019 reporting). `agent_uninstall` is not excluded, but uninstall deletes the state directory.
17. **Reboot/shutdown ordering is unchanged:** the action is started, then the result is queued. The
    outbox persists it, so a result produced just before a reboot is delivered after the restart.

## Test run output

### Build, vet and cross-build

```
$ go build ./...
exit=0
$ go vet ./internal/agent/...
exit=0
$ GOOS=linux go build ./...
exit=0
$ GOOS=linux go vet ./internal/agent/...
exit=0
$ GOOS=windows go build ./...
exit=0
```

An earlier `go build ./...` run during development failed in `internal/server/handlers/handlers.go:517`
(`BroadcastTelemetry` arity), which is WP-GO-SERVER's in-progress work. It was fixed by that package
before the final runs above.

### Agent packages and models (Windows, no `-race`)

```
$ go test ./internal/agent/... ./internal/models/ -count=1
ok  	github.com/DesyncTheThird/rIOt/internal/agent	13.725s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	4.793s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.412s
```

### New tests (`go test ./internal/agent/... -run TestCL_ -v`, result lines)

118 pass, 2 skip (Linux-only, see the Linux run), 0 fail. Skipped on Windows:
`TestCL_SEC_AgentStateFilesMode0600`, `TestCL_SEC005_SanitizedFileRefusesPreexistingSymlink`.

```
--- PASS: TestCL_AC067_SeenStoreRemembersAcrossRestart
--- PASS: TestCL_AC067_SeenStoreCapsAtThousandEntries
--- PASS: TestCL_AC067_SeenStorePrunesAfterRetention
--- PASS: TestCL_AC067_SeenStoreReportsPersistFailure
--- PASS: TestCL_SEC008_AtomicWriteOrder
--- PASS: TestCL_SEC008_FailedWriteKeepsPreviousFileAndRemovesTemp
--- PASS: TestCL_AC021_InflightMarkerSurvivesRestart
--- PASS: TestCL_AC021_InflightDockerRecordsAreKeyed
--- SKIP: TestCL_SEC_AgentStateFilesMode0600
--- PASS: TestCL_AC067_SameIDViaWSAndHeartbeatRunsOnce
--- PASS: TestCL_AC092_AckBeforeFirstStep
--- PASS: TestCL_AC099_FinalResultLegacyStatusPlusFields
--- PASS: TestCL_AC094_StderrTail8KiBAndFailureStep
--- PASS: TestCL_AC021_InterruptedDockerUpdateReportedFailed
--- PASS: TestCL_AC021_MarkerWithQueuedResultNotReportedTwice
--- PASS: TestCL_AC022_SelfUpdateMarkerNotReported
--- PASS: TestCL_AC053_EventAndResultAfterOpCtxExpired
--- PASS: TestCL_AC066_LockReleasedAfterPanickingCommand
--- PASS: TestCL_AC063_ConflictingHostCommandFailsPrecheck
--- PASS: TestCL_SEC004_AgentUpdateRejectedWhileDockerLockHeld
--- PASS: TestCL_SEC004_AgentUninstallRejectedWhileDockerLockHeld
--- PASS: TestCL_SEC004_NoNewCommandsAcceptedAfterSIGTERM
--- PASS: TestCL_SEC004_ShutdownWaitsForCommandsThenCancels
--- PASS: TestCL_SEC007_NonUUIDCommandIDNotExecuted
--- PASS: TestCL_SEC008_SeenPersistFailureFailsPowerActionsClosed
--- PASS: TestCL_SEC008_SeenPersistedBeforeExecute
--- PASS: TestCL_AC016_ResultOfDisconnectedCommandPostedOverHTTP
--- SKIP: TestCL_SEC005_SanitizedFileRefusesPreexistingSymlink
--- PASS: TestCL_SEC005_SanitizedFileMode0600
--- PASS: TestCL_SEC005_StaleOwnFileReplacedDirectoryRefused
--- PASS: TestCL_SEC005_SanitizedFileRemovedOnEveryExit
--- PASS: TestCL_SEC005_SanitizedPathRecordedBeforeCreation
--- PASS: TestCL_SEC005_StartupRemovesLeftoverSanitizedFile
--- PASS: TestCL_AC032_DependentByFullParentID
--- PASS: TestCL_AC033_DependentByNameSlashNameService
--- PASS: TestCL_AC032_ShortIDPrefixNeedsTwelveCharacters
--- PASS: TestCL_SEC011_ComposeArgsHygiene
--- PASS: TestCL_SEC011_DependentServiceNameValidatedOrSeparated
--- PASS: TestCL_SEC004_RecoveryRenamesSetAsideBackWhenOriginalMissing
--- PASS: TestCL_SEC004_RecoveryLeavesBothWhenOriginalExists
--- PASS: TestCL_SEC004_RecoveryRestartsRecordedStoppedDependents
--- PASS: TestCL_NFR004_RecoveryNeverRemovesContainers
--- PASS: TestCL_SEC004_RecoveryDockerUnreachableKeepsMarkerAndReportsOnce
--- PASS: TestCL_AC002_ResolvesByNameOverStaleID
--- PASS: TestCL_AC003_ResolveIgnoresLeadingSlash
--- PASS: TestCL_AC004_UnresolvedTargetFailsAtResolve
--- PASS: TestCL_AC040_PullErrorDetailFailsAtPull
--- PASS: TestCL_AC041_SameImageSkipsRecreate
--- PASS: TestCL_AC042_OldRenamedAsideRemovedAfterVerify
--- PASS: TestCL_AC043_CreateFailureRollsBack
--- PASS: TestCL_AC044_StartFailureRemovesReplacement
--- PASS: TestCL_AC045_ExitDuringVerifyRollsBack
--- PASS: TestCL_AC046_StoppedStaysStopped
--- PASS: TestCL_AC047_RollbackRenameFailureKeepsAside
--- PASS: TestCL_AC048_LeftoverAsideBlocksUpdate
--- PASS: TestCL_NFR004_OldContainerNeverRemovedBeforeVerify
--- PASS: TestCL_SEC011_CleanupAndRollbackUseOldContainerID
--- PASS: TestCL_SEC004_MarkerPersistedBeforeRenameAndDependentStop
--- PASS: TestCL_SEC004_MarkerWriteFailureAbortsBeforeMutation
--- PASS: TestCL_AC035_DependentOnNewParentNamespace
--- PASS: TestCL_AC034_SingleServiceStopsOnlyItsDependents
--- PASS: TestCL_AC036_DependentRestoredAfterPullFailure
--- PASS: TestCL_AC037_RestoreHasOwnBudgetAfterUpTimeout
--- PASS: TestCL_AC038_StoppedDependentStaysStopped
--- PASS: TestCL_AC039_FailedRestoreFailsCommand
--- PASS: TestCL_AC049_UpGetsFullBudgetAfterSlowPull
--- PASS: TestCL_AC052_PullLimitMessage
--- PASS: TestCL_AC093_ComposeProgressOrder
--- PASS: TestCL_AC093_ComposeVerifyFailsWhenServiceDown
--- PASS: TestCL_AC063_SecondUpdateFailsPrecheckNoDockerCalls
--- PASS: TestCL_AC064_StackRejectedDuringServiceUpdate
--- PASS: TestCL_AC065_DifferentStandaloneTargetsBothRun
--- PASS: TestCL_AC061_DockerUpdateInvalidatesItsRefs
--- PASS: TestCL_SEC004_DockerUpdateRefusedWhileAgentRestartRunning
--- PASS: TestCL_AC034_BulkUpdatesChildrenBeforeParents
--- PASS: TestCL_AC063_BulkConflictFailsPrecheck
--- PASS: TestCL_AC049_BulkSkipsRemainingWhenBudgetExhausted
--- PASS: TestCL_SEC007_ReportURLPathEscaped
--- PASS: TestCL_AC025_NonJSONReplyYieldsEmptyResponse
--- PASS: TestCL_AC100_NewAgentAgainstLegacyServer
--- PASS: TestCL_AC015_WriteErrorKeepsOutboxEntry
--- PASS: TestCL_AC016_DisconnectedResultPersistedThenPosted
--- PASS: TestCL_AC016_ReceiptRemovesEntryElseHTTPFallback
--- PASS: TestCL_AC017_Http500BackoffThenRemovedOn2xx
--- PASS: TestCL_AC017_ReplyClassificationTable
--- PASS: TestCL_AC018_EntrySurvivesRestart
--- PASS: TestCL_AC019_OverflowDropsOldestWithWarning
--- PASS: TestCL_AC020_EntryOlderThan24hPruned
--- PASS: TestCL_AC025_404EntersLegacyModeRemovesAfterWSWrite
--- PASS: TestCL_SEC001_Outbox429RetriesWithBackoff
--- PASS: TestCL_AC019_OversizedOutputFitsPerEntryBound
--- PASS: TestCL_AC050_SigtermThenSigkillAfterGrace
--- PASS: TestCL_AC051_ExitWithinGraceNoSigkill
--- PASS: TestCL_AC050_WellBehavedChildStopsOnSigterm
--- PASS: TestCL_AC049_ProcessFinishingBeforeLimitIsNotSignalled
--- PASS: TestCL_AC050_ParentContextCancelUsesSameGrace
--- PASS: TestCL_AC050_StartFailureIsReturned
--- PASS: TestCL_AC050_ExecProcessCapturesOutput
--- PASS: TestCL_AC066_LockReleasedAfterFailureAndPanic
--- PASS: TestCL_AC063_TryLockAllIsAllOrNothing
--- PASS: TestCL_AC065_DifferentKeysDoNotConflict
--- PASS: TestCL_AC066_UnlockIgnoresForeignHolderAndRelockIsIdempotent
--- PASS: TestCL_SEC004_HeldWithPrefixFindsDockerLocks
--- PASS: TestCL_AC063_ConcurrentLockersHaveOneWinner
--- PASS: TestCL_AC013_ConcurrentSendsNeverOverlap
--- PASS: TestCL_AC014_ConnectDisconnectSendRaceFree
--- PASS: TestCL_AC015_SendReturnsWriteError
--- PASS: TestCL_AC078_BackoffResetsAfterConnect
ok  	github.com/DesyncTheThird/rIOt/internal/agent	10.984s
--- PASS: TestCL_AC054_PulledNotRecreated
--- PASS: TestCL_AC055_RegistryNewer
--- PASS: TestCL_AC056_AllMatchUpToDate
--- PASS: TestCL_AC057_SameTagDifferentVerdictsOneLookup
--- PASS: TestCL_AC058_DigestRefsPinnedNoRegistryCall
--- PASS: TestCL_AC059_RegistryErrorIsCheckFailed
--- PASS: TestCL_AC060_FailedCheckCachedFiveMinutes
--- PASS: TestCL_AC060_SuccessCachedThirtyMinutes
--- PASS: TestCL_AC054_ManualPullInsideCacheWindowDetected
--- PASS: TestCL_AC061_InvalidateOnlyInvolvedRefs
--- PASS: TestCL_AC059_LocalInspectFailureIsCheckFailed
--- PASS: TestCL_AC054_ContainerInfoCarriesImageID
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	1.422s
```

### Linux run with the race detector

The Windows host has no C compiler, so `-race` cannot run locally. The same packages were run in a
throwaway `golang:1.24` container (image pulled locally for this; the worktree mounted read-only, the
host module cache mounted for dependencies). This run also executes the Linux-only tests.

```
$ docker run --rm -e CGO_ENABLED=1 -v <worktree>:/src:ro -v <modcache>:/go/pkg/mod -w /src golang:1.24 \
    go test -race -count=1 ./internal/agent/... ./internal/models/
ok  	github.com/DesyncTheThird/rIOt/internal/agent	26.198s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	1.190s
ok  	github.com/DesyncTheThird/rIOt/internal/models	1.071s

$ go test -race -count=1 -v -run "TestCL_SEC005|TestCL_SEC_AgentStateFilesMode0600" ./internal/agent/
--- PASS: TestCL_SEC_AgentStateFilesMode0600
--- PASS: TestCL_SEC005_SanitizedFileRefusesPreexistingSymlink
--- PASS: TestCL_SEC005_SanitizedFileMode0600
--- PASS: TestCL_SEC005_StaleOwnFileReplacedDirectoryRefused
--- PASS: TestCL_SEC005_SanitizedFileRemovedOnEveryExit   (success, pull_failure, up_failure, panic)
--- PASS: TestCL_SEC005_SanitizedPathRecordedBeforeCreation
--- PASS: TestCL_SEC005_StartupRemovesLeftoverSanitizedFile  (marker, container labels, symlink)
ok  	github.com/DesyncTheThird/rIOt/internal/agent	2.187s
```

### Whole repository

```
$ go test ./... -count=1
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-server	[no test files]
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer	1.485s
ok  	github.com/DesyncTheThird/rIOt/internal/agent	11.934s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	5.707s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.827s
ok  	github.com/DesyncTheThird/rIOt/internal/releasekey	0.977s
ok  	github.com/DesyncTheThird/rIOt/internal/resilient	1.985s
ok  	github.com/DesyncTheThird/rIOt/internal/server	1.935s
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	0.843s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	0.876s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	1.816s
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	1.473s
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	1.017s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	4.026s
?   	github.com/DesyncTheThird/rIOt/internal/server/logstore	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	0.898s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	1.148s
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc	3.346s
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	1.633s
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	0.942s
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary	0.987s
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	0.728s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	1.002s
?   	github.com/DesyncTheThird/rIOt/internal/testutil	[no test files]
```

No failure in this run belongs to another work package. The server packages were passing at the time
of the run; their state moves with WP-GO-SERVER.

## Contract the server must match (agent to server)

- **Report body** (HTTP `POST /api/v1/devices/{id}/commands/{commandId}/report`, both IDs path-escaped):
  `models.CommandReport` JSON: `{kind: "ack"|"progress"|"result", command_id, state, step, progress, message, result}`.
  `X-rIOt-Key` header. A 2xx **must echo `command_id`** in the JSON body, otherwise the agent keeps
  the entry (captive-portal protection).
- **Reply handling:** 2xx with echoed ID, 409 `already_terminal`, 409 `invalid_transition` and 404
  `command_not_found` remove the entry. 400 and 413 drop it (Error log). 429, 5xx, 401, 403, network
  errors and any other status retry with 5s doubling to 5m. 404 or 405 **without a `code`** means a
  legacy server (WS-only for 1h).
- **WebSocket:** `command_ack` = `{command_id, at}`; `command_progress` = `{command_id, state:"running"|"verifying", step, progress?, message?, at}`;
  `command_result` = `models.CommandResult` with `step`, `outcome`, `stderr`, `reason`. Any
  `command_receipt` for an ID in the outbox removes it (the agent does not check the disposition).
  Without a receipt within 10s the agent falls back to HTTP, so a server that never replies on WS
  still gets every result by HTTP.
- **Steps reported:** `precheck`, `resolve`, `pull`, `stop_dependents`, `recreate`, `verify`
  (state `verifying`), `restart_dependents`, `rollback`, `cleanup`. A step is empty when an action has
  none. Reasons: `timeout`, `rolled_back`, `restarted`, `conflict`, `shutdown`, `persistence`.
- **Progress volume:** the agent sends one progress report per step change (about 4 to 8 per update,
  plus a percent update per container in a bulk), and acks every command once.
- **Command IDs** must be canonical lowercase UUIDs; anything else is dropped by the agent without a
  report.
- **Per-container update payload:** `container` (name) is preferred over `container_id`;
  `compose_work_dir` alone selects a stack update and must equal the working-dir label of a real
  container and be an absolute path.

## Notes for QA

- Docker behavior is covered by a fake daemon and fake compose processes. Nothing was run against a
  real daemon, so QA should do one manual standalone update, one compose service update and one
  stack update on a lab host, including a forced failure to see rollback and the dependents restore.
- Probe: kill the agent (not SIGTERM) between the rename-aside and the create of a standalone update,
  restart it, and check that the previous container is back and the failure result says
  `reconciled: restored <name> from <name>.riot-old-<id8>`.
- Probe: plant a symlink named `.compose.riot-sanitized.yml` in a compose directory and trigger a
  validation failure.
- Probe: fill the state directory (`/var/lib/riot`) and send a `reboot`; it must fail at `precheck`
  with reason `persistence` and the host must not reboot.
- Deferred hardening items (SEC-012, SEC-015) are tracked separately and are not covered by these probes.
- Pinned and `check_failed` images no longer report `update_available`; the dashboard (WP-WEB)
  reads `update_status` and `update_reason`.
- New environment variables: none. New dependencies: none.
