# Implementation Report

- **Story ID:** AUTOUPD-HARDEN-001 (security fix SEC-002)
- **Title:** Compose updates never run in an untrusted directory
- **Engineer:** Senior Dev Agent
- **Date:** 2026-10-19
- **Stack:** Go module (chi/pgx server, Go agent, testify tests; `go vet` as the Go linter) plus a React 18 / Vite / TypeScript web UI tested with vitest. Detected from `go.mod`, `web/package.json`, `web/vitest.config.ts`. The web package has no `lint` script, so `npx tsc --noEmit` is the static check.
- **Inputs:** FRD (amended), ADD revision 2 (FINAL), security review with the re-review conditions 1 to 6. No blockers were found, so no `AUTOUPD-HARDEN-001-blockers.md` exists. Nothing was pushed, tagged or deployed.

---

## Pre-Implementation Baseline

| Check (before any change) | Result |
|---|---|
| `go test ./...` (no `-race`: needs CGO, unavailable on Windows) | all packages `ok` |
| `cd web && npm run test:run` (after `npm ci` in the fresh worktree; `package.json` and the lockfile are unchanged) | 66 files, **1409** tests passed |

Environment note: the working tree is CRLF (`core.autocrlf=true`, index LF). `gofmt -l` lists many files on this machine, which is pre-existing. Every file this story created is `gofmt` clean. Existing files I edited were normalised to LF in the working tree, which git treats as no change.

---

## Completed Components

| File | Action | Notes |
|---|---|---|
| `internal/agent/compose_trust.go` | CREATE | `trustedDir`, `trustRequest`, `trustFS`, `fileMeta`, `composeTrust.Check`, account database parse, nsswitch and systemd userdb enumeration gate, trust set (socket owner/group, user-private groups), component walk, ACL rule, file set and `-f` selection, `.env` `COMPOSE_FILE` parser, `composeTrustError` (length-capped, `%q` quoted), `within()` (separator-aware, optional case fold) |
| `internal/agent/compose_snapshot.go` | CREATE | `daemonSnapshot`, `loadDaemonSnapshot` (Info, list, per-image provenance, per-volume bind-device), `projectsForDir`, `resolveProject`, `scopeOf` |
| `internal/agent/compose_trust_unix.go` | CREATE | `osTrustFS` on `!windows`: owner from `syscall.Stat_t`, `EUID` |
| `internal/agent/compose_trust_windows.go` | CREATE | `osTrustFS` without owner or ACL |
| `internal/agent/compose_trust_acl_linux.go`, `compose_trust_acl_other.go` | CREATE | `accessACLPresent`: `syscall.Getxattr("system.posix_acl_access")` on Linux (ENODATA/ENOTSUP mean none); an error on other Unix so a group-writable path fails closed. Split from `compose_trust_unix.go` because `syscall.Getxattr` does not compile on darwin (deviation 1) |
| `internal/agent/docker_api.go` | MODIFY | `dockerAPI` gains `Info`, `VolumeInspect` (the SDK client already has them); `newProc(name, env, args...)` |
| `internal/agent/proclimit.go` | MODIFY | `newExecProcess(name, env, args...)` sets `cmd.Env` when `env != nil` |
| `internal/agent/docker_update.go` | MODIFY | `updateTarget.Project`; `composeSpec{WorkDir, Project, Service, SourceID}`; `composeArgs(td, project, sanitized, sub, services...)`; `composeEnv`; `checkCompose`; `updateStack` (project resolution, server path as cross-check, old-server form); `composeCore` checks #1 to #3; `runCompose` env; `configWithoutComposeWorkDir` (AD-017); bounded `refusal` |
| `internal/agent/docker_deps.go` | MODIFY | `recreateComposeDependent(ctx, api, insp)` takes a snapshot and runs `Check`; restore-failure reasons capped at 1200 bytes |
| `internal/agent/compose_sanitize.go` | MODIFY | `sanitizeComposeFile(td)` reads `td.Files()[0]`, writes into `td.Path()` |
| `internal/agent/docker_recovery.go` | MODIFY | `removeRecordedSanitized` (marker path removal gated by `Check`, bounded summary line); `cleanupSanitizedLeftovers` iterates compose sources and gates each directory |
| `internal/agent/command_lifecycle.go` | MODIFY | `markerTarget` falls back to `compose_project` |
| `internal/agent/agent.go` | MODIFY | seams `trustFS`, `trustPlatform`; new `procFactory` signature |
| `internal/agent/config.go` | MODIFY | `DockerConfig.ComposeRoots`, unexported `rootsErr`, `normalizeComposeRoots`, template line `# compose_roots: []` |
| `internal/agent/doctor.go` | MODIFY | `checkComposeTrust`, `reportComposeTrust` (every printed line bounded) |
| `scripts/install.sh` | MODIFY | generated `agent.yaml` `docker:` section shows `# compose_roots: []` as a commented example |
| `internal/server/handlers/auto_update.go` | MODIFY | stack dispatch sends `compose_project` + `compose_work_dir`; a project whose containers carry several working-dir labels is dispatched by `compose_project` alone (QA-D1 round, replaces the first-round skip) |
| `internal/server/commands/targets.go` | MODIFY | `compose_project` key fallback (the working-dir keys of the project in telemetry; all of them when labels disagree) |
| `web/src/components/ComposeStackSection.tsx` | MODIFY | manual stack update sends `{ compose_project: stack.name, compose_work_dir: stack.workDir }` |
| `README.md` | MODIFY | `docker.compose_roots` row, "Compose directory trust" section (rules, UPG rule, `.env` readability, Windows, INFO-1 transitive trust, condition 6 residual risks), recovery bullet |
| Tests | CREATE / MODIFY | see the AC table; fakes in `fakes_docker_test.go` (`fakeTrustFS`, `rootOwnedFS`, `fakeDocker.Info/VolumeInspect/imageLabels/Mounts`, `fakeProcs` records env); existing tests moved to the new signatures |

`go.mod` and `go.sum` are unchanged. No migration. No new environment variable (one new agent config key, `docker.compose_roots`, documented in the README, the generated config template and the installer's commented example).

### Commits (branch `autoupd-harden-001`, nothing pushed or tagged)

| Hash | Subject |
|---|---|
| `5e85191` | `[AUTOUPD-HARDEN-001] feat: compose directory trust validator and daemon snapshot` |
| `1f3b002` | `[AUTOUPD-HARDEN-001] feat: gate every compose invocation behind the trust check` |
| `f9f2b4f` | `[AUTOUPD-HARDEN-001] feat: doctor reports compose roots and per-project trust` |
| `14a16a3` | `[AUTOUPD-HARDEN-001] feat: server dispatches stacks by project and surfaces skipped stacks` |
| `cba7251` | `[AUTOUPD-HARDEN-001] feat: manual stack update names the compose project` |
| `d800bb6` | `[AUTOUPD-HARDEN-001] docs: document compose directory trust and docker.compose_roots` |
| (this report) | `[AUTOUPD-HARDEN-001] docs: implementation report` |

---

## Condition 1: compose verification gate (ADD section 12)

The orchestrator verified these on Docker Compose v2.26.1 (Windows Desktop) and v2.40.3 (Linux, `docker:28-cli` container). I re-ran a subset locally on v2.26.1 with throwaway directories in a temp directory.

| # | Check | Result |
|---|---|---|
| (a) | An explicit `-f` overrides `COMPOSE_FILE` from the project `.env`. Without `-f`, compose honors `COMPOSE_FILE` and resolves relative entries against the process cwd, not the project directory. | Pass. The agent resolves `COMPOSE_FILE` entries relative to the project directory and passes absolute `-f=` tokens, which is the intended behavior. My re-run: with `.env` holding `COMPOSE_FILE=other.yml`, no `-f` listed only `other.yml`'s service; `-f=<dir>/compose.yaml` listed only `compose.yaml`'s service. |
| (b) | With `--project-directory` plus explicit `-f`, `.env` is still read from the project directory. | Pass. My re-run: `${VAR}` in the `-f` file interpolated from the project `.env`. |
| (c) | `--project-name=x` with repeated `-f=` tokens is accepted. | Pass. |
| (d) | Compose itself fails with `open <dir>/.env: permission denied` when `.env` is unreadable, so the `env_unreadable` refusal is not a regression. | Pass. |

**Compose file discovery order** (ADD section 12 asks for it to be copied from the installed compose and cited). Measured on v2.26.1 and v2.40.3 with all candidate files present; both versions agree.

- Default names, first existing wins: `compose.yaml`, `compose.yml`, `docker-compose.yml`, `docker-compose.yaml`. Note that `docker-compose.yml` comes before `docker-compose.yaml`. The earlier `sanitizeComposeFile` list had them the other way round.
- Override names, first existing wins, merged over the base file, from any family: `compose.override.yml`, `compose.override.yaml`, `docker-compose.override.yml`, `docker-compose.override.yaml`.
- An override with no default file is "no configuration file provided".

These lists are `composeDefaultNames` and `composeOverrideNames` in `compose_trust.go`, with tests (`..._DefaultAndOverrideBecomeExplicitF`).

---

## Re-review conditions 1 to 6

| # | Condition | Status |
|---|---|---|
| 1 | Verification gate | Done (section above). |
| 2 | AD-014 unresolvable mount sources: a writable source outside the data root that fails `EvalSymlinks` for any reason other than not-exist refuses with `container_writable` and a "cannot resolve" message; no lexical fallback | Done in `checkState.evalMount`/`checkPlacement`. Tests `..._AC019_UnresolvableMountSourceRefused`, `..._NonexistentMountSourceFallsBackToLexical`. |
| 3 | AD-003 enumeration: with `systemd` in nsswitch, member enumeration is on only when no userdb user/group/membership records exist in the standard directories and no homed socket exists; a `+`/`-` line in passwd or group marks the account database unavailable | Done (`enumerationEnabled`, `userdbClear`, `parsePasswd`, `parseGroup`). Tests `..._SystemdUserdbRecordsDisableMemberRule` (3 record kinds), `..._HomedSocketDisablesMemberRule`, `..._UnreadableUserdbDirDisablesMemberRule`, `..._CompatPlusAndMinusLinesMarkDBUnavailable`, `..._NonFilesNSSDisablesMemberRule`. |
| 4 | AD-011 visible skip | Superseded in QA round 1 (orchestrator decision): the server no longer skips a multi-directory stack and raises no event. It dispatches `compose_project` alone and the agent, which can resolve symlinks, decides; a genuinely ambiguous project fails there as `ambiguous_project`, a visible failed command with the normal backoff. The first-round event, limiter and Warn log were removed. See "QA round 1 fixes". |
| 5 | Provenance by key presence | Done (`inspectImageProvenance` tests `_, has := Labels[key]`). Test `TestSnapshot_AC018_EmptyValueImageLabelExcluded`. |
| 6 | README residual risks (unit-level `Group=`/`SupplementaryGroups=`, `gshadow` passwords, writers from a since-removed container) | Done in the README section "Compose directory trust". |

---

## Test Summary

Every test named in ADD section 8 exists. Names follow the ADD (`TestComposeTrust_ACnnn_...`, `TestSnapshot_...`, `TestDockerUpdate_...`, and so on). Tests do not depend on real Unix ownership: the validator runs against `fakeTrustFS`, and the lifecycle harness uses `rootOwnedFS`. The Linux-only paths (xattr ACL check, passwd/group parsing, systemd userdb) are exercised through the seam. Only `compose_trust_unix_test.go` touches the real `osTrustFS` (owner of a temp dir, no ACL on a fresh directory); it is `!windows` and compiles under `GOOS=linux` and `GOOS=darwin` (`go vet`) but was not executed on this Windows host.

| AC | Test file(s) | Tests | Status |
|---|---|---|---|
| AC-001 | `compose_trust_test.go`, `docker_update_trust_test.go` | `TestComposeTrust_AC001_RelativeOrEmptyDirRefused`, `TestDockerUpdate_AC001_RelativeWorkDirStartsNoComposeProcess` | pass |
| AC-002 | same | `..._SymlinkedDirValidatedAtResolvedPath`, `..._SymlinkToUntrustedTargetRefused`, `TestDockerUpdate_AC002_ComposeRunsAtResolvedPathWithPinnedProject`, `TestComposeCore_AC002_VerifyMatchesResolvedWorkingDirLabel` | pass |
| AC-003 | `compose_trust_test.go` | `UntrustedOwnerRefused`, `UntrustedAncestorOwnerRefused`, `UnknownUIDRefused`, `SocketUnavailableTrustsRootOnly`, `AccountDBUnreadableFailsClosed`, `MalformedPasswdFailsClosed` | pass |
| AC-004 | same | `WorldWritableDirRefused`, `StickyLeafStillRefused`, `WorldWritableStickyAncestorAccepted`, `WorldWritableNonStickyAncestorRefused`, `GroupWritableUntrustedGroupRefused`, `GroupWritableSocketGroupAccepted` | pass |
| AC-005 | same | `RootOwnedAccepted`, `SupplementarySocketMemberAccepted`, `PrimaryGroupSocketMemberAccepted`, `SocketOwnerAccepted` | pass |
| AC-006 | same | `NoDefaultFileNoComposeFileRefused`, `OverrideOnlyRefused`, `ComposeFileWithoutDefaultFileAccepted` | pass |
| AC-007 | same | `UntrustedOwnedFileRefused` (9 names), `UntrustedWritableFileRefused`, `FileSymlinkEscapeRefused`, `FileSymlinkInsideAccepted`, `NonRegularFileRefused` | pass |
| AC-008 | `compose_trust_test.go`, `config_test.go` | `OutsideRootsRefused`, `InsideRootAccepted`, `RootsUnsetNotEnforced`, `SymlinkedRootResolved`, `InvalidRootsFailClosed`, `RootPrefixIsSeparatorAware`, `TestLoadConfig_AC008_ComposeRootsParsedAndCleaned`, `..._RelativeRootLoadsButSetsRootsErr`, `TestDefaultConfigTemplate_AC008_DocumentsComposeRoots` | pass |
| AC-009 | `docker_update_trust_test.go`, `compose_snapshot_test.go` | `TestUpdateStack_AC009_ProjectResolvesToOneDir`, `..._ProjectNotFoundRefused`, `..._ProjectAmbiguousRefused`, `..._InvalidProjectNameRefused`, `TestSnapshot_AC009_ProjectResolution` | pass |
| AC-010 | `docker_update_trust_test.go` | `..._MismatchedWorkDirRefused`, `..._TrailingSlashWorkDirAccepted` | pass |
| AC-011 | same | `..._WorkDirAloneMapsToOneProject`, `..._WorkDirAloneTwoProjectsRefused`, `..._WorkDirAloneUntrustedRefused` | pass |
| AC-012 | `docker_update_trust_test.go`, `docker_deps_test.go` | `TestDockerUpdate_AC012_SingleServiceUntrustedLabelRefused`, `TestDockerBulkUpdate_AC012_UntrustedComposeMemberRefused`, `TestRestoreDependents_AC012_UntrustedDependentDirNotRecreated`, `TestComposeArgs_AC012_ZeroTrustedDirRefused` | pass |
| AC-013 | `docker_update_trust_test.go` | `TestComposeCore_AC013_SanitizedNotWrittenWhenRecheckFails`, `..._DirChangedBetweenChecksRefused`, `..._SanitizedRemovedWhenThirdCheckRefuses` (cleanup on a failure path, added for the standing bounds requirement), `TestCleanupSanitized_AC013_SkipsRefusedDir`, `TestReconcile_AC013_SanitizedRemovalSkipsRefusedDir` | pass |
| AC-014 | `auto_update_stack_test.go`, `targets_test.go`, `service_test.go` | `TestAutoUpdate_AC014_StackDispatchSendsProjectAndWorkDir`, `..._ContainerPolicySendsNeither`, `..._SameDirTrailingSlashKeepsWorkDir`, `..._SeveralWorkDirsDispatchProjectOnly`, `..._MixedLinkAndResolvedLabelsDispatchProjectOnly`, `..._ProjectOnlyDispatchIsBackoffGated`, `..._ProjectOnlyDispatchHoldsKeysAgainstDuplicates`, `..._RefusedStackDispatchBacksOff`, `TestResolveDockerUpdateKeys_AC014_ProjectOnlyMapsViaTelemetry`, `..._ProjectOnlyHoldsEveryReportedDir`, `..._WorkDirStillWins`, `TestAUTOUPD_RefusalCountsAsFailureForBackoff` | pass |
| AC-015 | `ComposeStackSection.test.tsx` | `[AC-015] manual stack update sends compose_project and compose_work_dir` (the older AC-108 test's assertion updated as the ADD says) | pass |
| AC-016 | `doctor_test.go` | `TestDoctor_AC016_ReportComposeTrustRootsAndPerProjectLines`, `..._InvalidRootsFail`, `..._DockerUnreachableWarns`, `..._ContainerWritableNamesContainerAndMount` | pass |
| AC-017 | `compose_trust_test.go`, `docker_update_trust_test.go` | `TestComposeTrust_AC017_MessageNamesPathAndRule` (23 rules), `TestDockerUpdate_AC017_RefusalIsPrecheckWithRule` | pass |
| AC-018 | `compose_snapshot_test.go`, `docker_update_trust_test.go`, `doctor_test.go` | `TestSnapshot_AC018_ImageDefinedWorkingDirExcluded`, `..._ComposeBuiltImageLabelsAccepted`, `..._ImageInspectFailureExcludes`, `..._EmptyValueImageLabelExcluded`, `..._ImageInspectedOncePerImage`, `TestDockerUpdate_AC018_SingleServiceImageLabelRefused`, `TestDockerBulkUpdate_AC018_ImageLabelMemberRefused`, `TestRestoreDependents_AC018_ImageLabelDependentNotRecreated`, `TestUpdateStack_AC018_ImageLabelContainerCannotCauseAmbiguity`, `TestCleanupSanitized_AC018_IgnoresImageLabelDirs`, `TestDoctor_AC018_ListsExcludedContainers`, `TestStandaloneUpdate_AC018_RecreateDropsWorkingDirLabel` | pass |
| AC-019 | `compose_trust_test.go`, `compose_snapshot_test.go` | the 10 `TestComposeTrust_AC019_*` tests in the ADD, plus `_UnresolvableMountSourceRefused`, `_NonexistentMountSourceFallsBackToLexical`; `TestSnapshot_AC019_InfoOrListFailureIsDaemonUnavailable`, `..._VolumeInspectFailureFailsClosed`, `..._EmptyDataRootFailsClosed`, `..._BindDeviceVolumeRecorded` | pass |
| AC-020 | `compose_trust_test.go`, `docker_deps_test.go` | the 8 `TestComposeTrust_AC020_*` tests in the ADD (the unparseable table has 13 cases), plus `_UnrelatedEnvKeysIgnored`; `TestComposeArgs_AC020_ExplicitFTokensAndSanitizedReplaces` | pass |
| AC-021 | `compose_trust_test.go`, `doctor_test.go` | `WindowsWithoutRootsRefused`, `WindowsRootsCaseInsensitive`, `WindowsSkipsOwnershipKeepsPlacement`, `TestDoctor_AC021_WindowsExplainsRootsRequirement` | pass |
| AC-022 | `compose_trust_test.go` | `GroupWritableWithACLRefused`, `ACLReadErrorFailsClosed`, `NoGroupWriteSkipsACLLookup` | pass |
| AC-023 | same | `SocketWithoutGroupRWDoesNotTrustMembers` | pass |
| AC-024 | same | `UserPrivateGroupWritableAccepted`, `SharedGroupWithUntrustedMemberRefused`, `GroupWithUnknownMemberNameRefused`, `GroupWithoutFileEntryRefused`, `NonFilesNSSDisablesMemberRule`, plus condition 3 tests (systemd userdb, homed, unreadable userdb, compat `+`/`-`) | pass |
| AC-025 | `compose_trust_test.go`, `docker_update_trust_test.go` | `TestComposeTrust_AC025_DirMappedToOtherProjectRefused`, `..._RecoveryDerivesTheUniqueProject`, `..._TrailingSlashLabelStillMatches`, `TestDockerUpdate_AC025_SingleServiceProjectMustOwnDir`, `TestRestoreDependents_AC025_DependentProjectMismatchRefused` | pass |
| AC-026 | `docker_deps_test.go`, `docker_update_trust_test.go` | `TestComposeEnv_AC026_PinsDockerHostAndDropsComposeVars`, `TestDockerUpdate_AC026_ComposeProcessGetsPinnedEnv` | pass |
| AC-027 | `compose_trust_test.go` | `TestComposeTrust_AC027_ControlCharsQuotedAndErrnoCollapsed` (also asserts the length cap) | pass |
| AC-028 | same | `SymlinkComponentRefused`, `NonDirectoryComponentRefused` | pass |
| (extra) | `docker_update_trust_test.go`, `compose_trust_unix_test.go` | `TestMarkerTarget_IncludesComposeProject`, `TestOSTrustFS_ReportsOwnerAndNoACL` | pass |

### Definition-of-Done greps

- `composeArgs` has exactly 4 production call sites (`docker_update.go` pull, sanitized retry pull, up; `docker_deps.go` dependent recreate), each fed by a `Check` result.
- `trustedDir{` as a non-zero literal appears only in `compose_trust.go` (`return trustedDir{dir: r, files: files}`) and in tests (`testTrusted`); the other production occurrences are `trustedDir{}` zero values in refusal returns of `compose_trust.go`.
- Every production compose child is built with `composeChildEnv()` / `composeEnv` (`runCompose`, `recreateComposeDependent`).

### Final run output summary

```
go test -count=1 ./... -v   (Windows, no -race)
  1449 top-level and 1274 sub-test PASS lines, 0 FAIL
  ok  cmd/riot-signer, internal/agent, internal/agent/collectors, internal/models,
      internal/releasekey, internal/resilient, internal/server, internal/server/auth,
      internal/server/ca, internal/server/commands, internal/server/db,
      internal/server/events, internal/server/handlers, internal/server/middleware,
      internal/server/notify, internal/server/oidc, internal/server/probes,
      internal/server/scoring, internal/server/summary, internal/server/updates,
      internal/server/websocket
go vet ./...                           clean
GOOS=linux  go vet ./internal/agent/...  clean (includes the Linux/Unix-only files and tests)
GOOS=darwin go vet ./internal/agent/     clean
GOOS=linux  go build ./...               ok
cmd/riot-agent builds for every Makefile build-agent-all target:
  linux/amd64, linux/386, linux/arm64, linux/arm (GOARM=7), linux/arm (GOARM=6),
  darwin/amd64, darwin/arm64, windows/amd64                      all ok
cd web && npm run test:run     66 files, 1410 tests passed (baseline 1409 plus the new AC-015 test)
cd web && npx tsc --noEmit     clean
```

No Postgres-gated tests were affected (no repository or SQL change), so the throwaway Postgres container was not needed.

---

## Bounds (standing requirement: nothing this change writes may grow without limit)

Every new log line, event, stored message and file has a fixed cap, and every retry path is rate limited by an existing mechanism.

### Per-item caps

| Item | Cap | Where |
|---|---|---|
| Refusal message (command result message and Warn log) | 1024 bytes (`maxRefusalBytes`); each label- or filesystem-derived value is cut to 200 bytes (`maxRefusalValueBytes`) before it is `%q`-quoted | `composeTrustError.Error`, `newTrustError` |
| Daemon error text in a log | 200 bytes | `snapshotRefusal`, `inspectImageProvenance` |
| Recovery summary line for a refused directory | 300 bytes | `removeRecordedSanitized` |
| Dependent restore reason | 1200 bytes | `restoreDependents.fail` |
| Doctor lines carrying a derived value | 1100 bytes (terminal output, not stored) | `reportComposeTrust` |
| `.env` read | 1 MiB read cap, content never stored or echoed | `trustFS.ReadFile` |
| Passwd, group, nsswitch read | 1 MiB each | `maxTrustFileBytes` |
| Temp file | one sanitized copy per stack, fixed name, `O_EXCL`, 0600, removed by `defer` on every exit including panic, refusal at check #3, and recovery | `composeCore`, `writeSanitizedCompose` |

### Worst-case growth per device per day

**Server events and logs.** This change adds no server event type and no server log line. The first-round skip event, its limiter and its Warn log were removed in QA round 1. The only server-side growth is the existing command rows and `command_completed` events of the failed runs counted below.

A hostile image cannot multiply the failed-run chain either: runs are keyed by the operator's policies, not by containers, and a project-only dispatch holds the conflict keys of the reported directories, so a queued command is never duplicated by the next telemetry cycle (`TestAutoUpdate_AC014_ProjectOnlyDispatchHoldsKeysAgainstDuplicates`).

**Failed-run chain from agent refusals.** A refusal is an ordinary failed run, so the existing backoff `min(15m × 2^(n-1), 6h)` applies (`TestAUTOUPD_RefusalCountsAsFailureForBackoff`, `TestAutoUpdate_AC014_RefusedStackDispatchBacksOff`). For one persistently refused stack policy the dispatch times in the first day are t = 0, 15, 45, 105, 225, 465, 825 and 1185 minutes (gaps 15, 30, 60, 120, 240, 360, 360), so 8 attempts in the first 24 hours, and then 4 per day at the 6 h ceiling. Each attempt writes:

- one command result message of at most 1024 bytes, plus the existing fixed-size result fields;
- two existing lifecycle events (`command_sent` about 150 bytes, `command_completed` message capped by the existing 200-byte reason cut, about 350 bytes);
- two agent Warn log lines, each at most about 1.2 KB: `docker_update: compose directory refused` (the refusal, 1024 bytes plus framing) and the existing `command: failed` line, which carries the same message (about 2.4 KB in total).

Per attempt: 1024 + 150 + 350 + 2400 ≈ 3.9 KB. First day: 8 × 3.9 KB ≈ 31 KB per refused policy. Steady state: 4 × 3.9 KB ≈ 16 KB per refused policy per day. Ten refused policies: about 160 KB per device per day at steady state. This is the whole worst case of the change, since the skip event it replaces is gone: a truly ambiguous project now costs one such failed run per backoff step instead of one event per 6 hours. A refusal never reaches a stop, rename, create or remove (`TestDockerUpdate_AC017_RefusalIsPrecheckWithRule`), so it produces no Docker events.

**Manual and bulk updates.** A user-triggered update that is refused writes one result of at most 1024 bytes and two log lines of about 1.2 KB per command (about 3.4 KB); growth is bounded by the command rate limiter that already exists in the command service.

**Dependent recreates.** Each stale dependent whose directory is refused adds one Warn log line (at most about 1.2 KB) and one restore reason (at most 1200 bytes, in the existing result `stderr`, which is already capped at `MaxStderrTailBytes` = 8192), and the command then adds its own `command: failed` line. With D stale dependents on one command, the result stays at or below 8 KB and the log grows by at most D × 1.2 KB + 1.2 KB; D is the number of containers sharing a network namespace with the updated one, which is a handful. Even at D = 50 this is about 61 KB, once per backoff step.

**Agent startup recovery.** `cleanupSanitizedLeftovers` logs one Debug line (suppressed at the default Info level) per refused directory and one Info line per removed leftover, each at most about 1.2 KB. It runs once per agent start. `reconcileInterrupted` adds at most one 300-byte summary line per recorded marker record, and the in-flight store already bounds the number of markers. Agent restarts are limited by systemd's restart limit.

**Files and memory.** No new persistent file is created. The sanitized copy is at most one file per stack at a time (fixed name, replaced or removed) with the size of the stack's compose file, as before. `loadDaemonSnapshot` is rebuilt per check and discarded: three per compose command plus one per dependent recreate; memory is proportional to the container count and is released at once. There is no cross-call cache.

**Daemon calls.** A snapshot makes one `Info`, one list, one `ImageInspect` per distinct image of a working-dir-labelled container, and one `VolumeInspect` per distinct local volume, all on the local socket within `DockerPrecheckBudget` (15 s), so a slow daemon cannot hold a command open beyond that budget.

---

## Deviations from ADD

These trigger architect review. None changes a security property.

1. **Two extra files for the ACL lookup.** `compose_trust_acl_linux.go` and `compose_trust_acl_other.go` hold `accessACLPresent`. The ADD puts the Linux `Getxattr` and the error-returning stub in `compose_trust_unix.go`, but `syscall.Getxattr` does not exist on darwin, so one file would not compile for the `darwin` agent targets. Same behavior, build-tagged files.
2. **`trustFS` gains `ReadDirNames`.** Condition 3 needs to list the systemd userdb directories, and the ADD interface has no directory listing. It is an additive method on the seam (`osReadDirNames` in production, the map-backed fake in tests).
3. **File discovery follows the installed compose order,** not the order in the old `sanitizeComposeFile` (see Condition 1). This is what ADD section 12 asked for; it is listed because it changes which file `sanitizeComposeFile` reads when both `docker-compose.yml` and `docker-compose.yaml` exist.
4. **Unquoted `.env` values may have trailing whitespace.** The ADD regex for the unquoted form does not allow it while the quoted forms do. The line is right-trimmed before matching. Security is unaffected, because compose gets only the validated `-f` files.
5. **Empty `DockerRootDir` fails the snapshot.** The ADD does not say what an empty data root means. A daemon that does not report one cannot be checked, so the snapshot fails closed (`daemon_unavailable`).
6. **Test file split.** The ADD names `compose_trust_test.go` and `docker_update_trust_test.go`; the dashboard test for AC-015 is a new `it` next to the old assertion rather than a replacement of it.
7. **FRD file was not edited.** The ADD lists the FRD amendment as done in revision 2, and it was already in the repository.

## Follow-ups (out of scope)

- Pruning of superseded images is not implemented here (standing requirement item 4). Pulling new images leaves the old ones on disk until something prunes them; that is a separate story.
- The `ProtectSystem=strict` limitation on the sanitized copy (INFO-5) is unchanged.
- SEC-008 (FUSE with `user_allow_other`) and the INFO-1 transitive references are documented residual risks.
- An agent older than this change stays unhardened (R-1); there is no version gating.

## Notes for QA

- **Run the doctor on a real Linux host** with the four stacks the ADD names: a trusted stack, a user-private-group stack (`dir 0775`, owner and group the same Docker-group user), a `.:/app` stack (expect `container_writable` naming the container and mount), and a container whose image defines the working-dir label (expect the "ignored as a compose source" warning). The Linux-only `osTrustFS`/xattr path ran only through the compile checks and the seam on this Windows host.
- `.env` readability: a `.env` with mode 0640 owned by an operator is refused with `env_unreadable` until the agent user is in that group. This matches what compose itself would do as the same user.
- Probe a symlinked stack directory: compose runs at the resolved path with `--project-name` pinned, and the next verify finds the containers under the resolved working-dir label.
- Try an old agent against the new server (it ignores `compose_project`) and a new agent against an old server (`compose_work_dir` alone).
- Edge: `COMPOSE_FILE` entries that are absolute paths cannot be written with a backslash on Windows (the `.env` grammar rejects it); tests use slash-separated relative entries.
- Edge: a project name with uppercase letters is refused (`invalid_project`). Compose always lower-cases project names, so no real container has one.

---

## QA round 1 fixes

QA returned FAIL (`docs/qa/AUTOUPD-HARDEN-001-qa-report.md`) with two blocking defects and a ruling on a third. Everything below is on the same branch, nothing pushed.

### Commits

| Hash | Subject |
|---|---|
| `79e6135` | `fix: dispatch a multi-directory stack by project only instead of skipping it (QA-D1, QA-D5)` |
| `c7487ef` | `fix: treat working-dir labels that resolve to one directory as one (QA-D1)` |
| `3edfaf8` | `fix: .env parsing skips a UTF-8 BOM and matches COMPOSE_FILE keys exactly (QA-D3, QA-D4)` |
| `edfd896` | `fix: compare unreadable mount sources through their readable prefix (QA-D2)` |
| (this report) | `docs: implementation report, QA round 1 fixes and corrected bounds` |

### QA-D1 (blocking): symlinked stack directory stuck as ambiguous

- **Agent.** `composeTrust.bind` gives the snapshot a resolver: a working-dir label is compared by the key of the real directory (`EvalSymlinks` through `trustFS`). A label that does not resolve (not-exist, or any other error) is compared by its cleaned path, so it still counts as its own directory and can make a project ambiguous, which fails closed. `projectsForDir`, `resolveProject`, `scopeOf` (update scope and post-update verification), the `compose_work_dir` cross-check and the old-server form all use it. Verification finds containers under either label form. `resolveProject` returns the smallest label of the one directory, so the answer is deterministic. QA's `TestQA_D1_...` is green. Added `TestUpdateStack_AC009_LinkAndTargetLabelsAreOneDirectory` (project only, work dir as link, work dir as target, old-server form with link and with target), `..._TrulyDifferentDirsStayAmbiguous` (a link to a different directory is a different directory) and `TestComposeCore_AC002_VerifyFindsContainersUnderEitherLabelForm`.
- **Server (orchestrator decision, replaces the AD-011 skip).** When telemetry shows more than one working-dir label for a stack policy project, the server dispatches `docker_update` with `compose_project` only. An old agent gets no path and fails closed on its existing "required" precheck. The skip path, the `stack_update_skipped` event type, its limiter, the Warn log and their tests (including QA two limiter tests, which tested dead code) are removed. The single-directory case is unchanged.
- **Deviation from the decision as written.** "No key is reserved when the project key is ambiguous" caused a queue pile-up in my own test: with no target key there is no conflict, so every telemetry cycle after the backoff would queue another command while the first was still in flight (22 commands instead of 2). A project-only command therefore holds the existing `docker:compose:<dir>` key of every working directory the device reports for the project (sorted, trailing slashes removed). This uses only the frozen key kind, conflicts with a manual update of either directory, and the agent still takes its own lock on the resolved directory. Tests: `TestResolveDockerUpdateKeys_AC014_ProjectOnlyHoldsEveryReportedDir`, `TestAutoUpdate_AC014_ProjectOnlyDispatchHoldsKeysAgainstDuplicates`, `TestAutoUpdate_AC014_ProjectOnlyDispatchIsBackoffGated`. This is the one point where QA or the architect may want to overrule me.
- **QA-D5** (unthrottled server Warn) is resolved by removing the line.

### QA-D3 (blocking): BOM-prefixed `.env`

A leading UTF-8 BOM is stripped before parsing, as compose does. QA `TestQA_D3_...` is green, and QA's `TestQA_AC020_BOMPrefixedEnvIsParsed` and `TestQA_AC020_BOMDoesNotWeakenRefusal` check the honoured form and that an unreadable form after the BOM is still refused (the senior-dev tests named here originally were never committed; QA round 2 added these).

### QA-D4: key match

Only a key named exactly `COMPOSE_FILE` or `COMPOSE_PATH_SEPARATOR` is a candidate; the next character must be end of line, whitespace or `=`. `COMPOSE_FILES_DIR` and similar are ignored (also after `export`). The exact key in an unmodelled form (`COMPOSE_FILE`, `COMPOSE_FILE =a`, `export COMPOSE_FILE=a`) is still refused. Test `TestQA_AC020_OnlyExactKeysAreParsed` (added in QA round 2).

### QA-D2: unreadable mount sources (security ruling, five conditions)

Implemented in `resolveFallbacks` and `firstFailure` (`compose_trust.go`). A writable mount source that cannot be resolved is compared through its readable prefix only when all of these hold; otherwise it refuses as `container_writable` ("cannot resolve ...", with the reason appended):

1. The first component that fails to resolve fails with a permission error (EACCES or EPERM). ELOOP, ENOTDIR and every other error refuse; ENOENT keeps the old lexical comparison.
2. P, the last directory that resolves, is a real directory (`typeWalk`, which `Lstat`s every component), and the AD-003 owner, mode and ACL rules hold for P and every ancestor of P (`permWalk`, leaf rule for P). The trust set is now computed before the placement check for this.
3. No other writable source of a container without a socket mount is at or above P, tested both lexically (a proper ancestor of the unresolved source) and by resolved prefix (including through a symlink).
4. The comparison path is the resolved prefix joined with the cleaned remainder, not the original string.
5. Windows never uses the fallback.

Doctor: `reportPrivilegeMismatch` warns when the euid of the doctor differs from the UID of the service user of the agent (read from the `User=` line of the installed unit, default `riot`) and names that user and the `sudo -u` command. `reportComposeTrust` prints an info line per source compared through the fallback. Tests: `TestComposeTrust_AC019_UnreadableTrustedHomeSourceAccepted`, `..._FallbackComparesPrefixPlusRemainder`, `..._FallbackUntrustedOwnedPrefixRefused`, `..._FallbackGroupWritablePrefixWithACLRefused`, `..._FallbackRefusedWhenUntrustedAncestorOfPrefix`, `..._FallbackRefusedWhenAnotherWritableSourceIsAbove` (3 subtests), `..._FallbackOnlyForPermissionErrors` (ELOOP, ENOTDIR, EIO), `..._FallbackENOENTKeepsLexicalComparison`, `..._FallbackNeverOnWindows`, `TestDoctor_AC019_EuidWarningNamesServiceUser`, `..._ServiceUserFromUnitFile`, `..._InfoLinePerFallbackSource`. The existing `..._UnresolvableMountSourceRefused` still passes.

Consequence worth knowing: the QA live case was `/home/<user>/data` with the home directory owned by an ordinary user. That user is an untrusted account and could plant a symlink below its own home, so P fails the owner rule and the host is still blocked by that mount. The fallback helps for root-owned or Docker-account-owned parents (for example a root-owned 0750 sandbox directory). The README says this next to `container_writable`.

### Bounds recomputed

With the skip event and its log line removed, the change adds no server events and no server log lines. The agent writes two Warn lines per refusal (`docker_update: compose directory refused` and the existing `command: failed`), not one: each attempt is about 3.9 KB, the first day has 8 attempts (about 31 KB per refused policy), and the steady state is 4 per day (about 16 KB per refused policy per day, 160 KB for ten). See the corrected Bounds section above. New in this round: the label resolver memo is per snapshot, bounded by the number of containers, and discarded with it; doctor info lines are capped at 1100 bytes and are terminal output only.

### README

The line "Path components must be plain directories, not symlinks" now says the stack directory is symlink-resolved first and every walked component of the resolved path must be a real directory. The skip paragraph is replaced by the project-only dispatch description, and the `container_writable` bullet documents the unreadable-source rule.

### Test results after the fixes

| Run | Result |
|---|---|
| Windows `go test -count=1 ./...` | 21 packages ok, 1477 top-level and 1284 sub-test passes, 0 failures. QA red repros `TestQA_D1_...` and `TestQA_D3_...` are green. |
| Windows `go vet ./...`, `GOOS=linux go vet ./internal/agent/...`, `GOOS=darwin go vet ./internal/agent/`, `GOOS=linux go build ./...` | clean |
| `cmd/riot-agent` cross builds (linux amd64, 386, arm64, arm; darwin amd64, arm64; windows amd64) | all ok |
| Linux, `golang:1.24` container, `go test -count=1 -race ./internal/agent/... ./internal/server/... ./internal/models/...`, tree exported with `git archive` (LF) | **root: all packages ok. Non-root uid 1234: all packages ok.** The real `osTrustFS` test (`TestOSTrustFS_ReportsOwnerAndNoACL`) and the QA D1 and D3 repros pass in both. |
| `cd web && npm run test:run` / `npx tsc --noEmit` | 66 files, 1410 tests passed / clean |

One Windows-only test quirk: the ENOTDIR subtest of `..._FallbackOnlyForPermissionErrors` is skipped on Windows because `syscall.ENOTDIR` equals `ERROR_PATH_NOT_FOUND` there. It runs and passes in the Linux containers.
