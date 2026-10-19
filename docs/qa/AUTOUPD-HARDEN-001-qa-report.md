# QA Report

**Story ID:** AUTOUPD-HARDEN-001 (security fix SEC-002)
**QA Engineer:** QA Agent
**Date:** 2026-10-19
**Branch / range:** `autoupd-harden-001`, `3e11148..242407f` on top of main `8873911`
**Verdict:** PASS WITH NOTES (round 2, see "Round 2" at the end). Round 1 was FAIL; the sections up to "Round 2" describe round 1.

Round 1: two functional defects (QA-D1, QA-D3) break an FRD guarantee and are reproduced by intentionally red tests that QA committed. Everything else in the story held up, including a live end-to-end probe on Linux against a real Docker daemon and compose v5.6.0. Details and the fix list are at the end.

---

## Test Run Summary

All numbers were produced on this machine, not copied from the implementation report.

### Windows (host)

| Suite | Result |
|---|---|
| `go test -count=1 ./...` | 21 packages `ok`, 0 failing, before QA changes. After QA changes, the only failures are the two intentionally red repros (below). |
| `go vet ./...` | clean |
| `cd web && npm run test:run` | 66 files, **1410 tests passed**, 0 failed |
| `cd web && npx tsc --noEmit` | clean |

### Linux (Docker, `golang:1.24`, matching `go.mod` `go 1.24.0` and the `golang:1.24-alpine` build image)

The tree was exported with `git archive` into a scratch directory and normalised to LF, then run in a container with `-race` (CGO available).

| Run | Result |
|---|---|
| `go test -count=1 -race ./internal/agent/... ./internal/server/... ./internal/models/...` as **root**, original commit `242407f` | **1 FAIL**: `TestComposeTrust_AC021_WindowsRootsCaseInsensitive` (`compose_trust_test.go:768`). All other packages `ok`. |
| same test binary (`-race`) as **non-root uid 1234** | the same single failure, nothing else (322 top-level PASS) |
| `compose_trust_unix_test.go` (`TestOSTrustFS_ReportsOwnerAndNoACL`, the real `osTrustFS` and the Linux xattr ACL path) | **PASS as root and as non-root uid 1234** |
| after QA test fix and QA additions: `go vet ./...` and the same `-race` run as root | only the two intentionally red repros fail (`TestQA_D1_...`, `TestQA_D3_...`); every other package `ok` |
| after QA changes, non-root, `-test.count=3` | no flakiness; the same two repros fail on every iteration, nothing else |

**The Linux-only failure (found and fixed by QA).** The assertion used Windows drive paths (`C:\Stacks`) with `filepath`. On Linux a backslash is an ordinary character, so `within` returned false. Production code is fine (`windows()` is only true on a Windows `GOOS`, where `filepath` understands those paths). It would have turned the ubuntu CI job red. QA changed the test to use slash paths for the platform-independent fold check and to keep the drive-letter assertion Windows-only. This is test code only; no implementation file was touched.

**Flaky tests:** none (Windows suite twice, Linux race run twice, Linux count=3).

### Mutation testing (QA ran about 45 production-code mutants against the suite)

Each mutant was applied to a scratch copy only, run, and reverted. Killed means a test went red.

Killed by the existing suite: owner check, world-writable leaf, sticky-ancestor rule, ACL rule (both the has-ACL and read-error branch), socket `0060` condition, separator-aware `within` (two variants), provenance by key presence (condition 5), `COMPOSE_*` and `DOCKER_HOST` stripping, data-root rule, container-writable rule, unresolvable-mount refusal (condition 2), component type walk (C-7), project mismatch (C-4), UPG member check, systemd userdb gate (condition 3), duplicate `.env` key, `.env` unreadable rule, AD-017 label drop, recovery gate in `reconcileInterrupted` and `cleanupSanitizedLeftovers`, `-f` token emission, `DOCKER_HOST` pin, server skip, server limiter (window and sweep), project-only key fallback.

**Survived (false coverage), then closed by QA tests:**

| Mutant | Why it survived | QA test that now kills it |
|---|---|---|
| `td2.same(td)` removed (check #2) | check #3 backstopped it, and the one `dir_changed` test changed the set before the first pull | `TestQA_AC013_Check2ComparesFileSetBeforeSanitizedCopyIsWritten` |
| `td3.same(td)` removed (check #3) | check #2 backstopped it | `TestQA_AC013_Check3ComparesFileSetBeforeUp` |
| whole-message cap (`maxRefusalBytes`) removed | the 200-byte per-value cap kept the only tested message under 1024 | `TestQA_AC027_WholeMessageCapHoldsWhenEveryValueIsLong` |
| per-value cap (`maxRefusalValueBytes`) removed | the whole-message cap kept it under 1024 | `TestQA_AC027_LongValueIsCutBeforeTheWholeMessageCap` |
| skip-limiter sweep removed | no test checked eviction | `TestQA_SkipLimiterEvictsExpiredKeys` |

One mutant (`cleanupSanitizedLeftovers` without its `Check`) first looked like a survivor, but only because a zero `trustedDir` has an empty path. A fair variant (remove the file by label directory) is killed by `TestCleanupSanitized_AC013_SkipsRefusedDir`.

---

## AC Coverage Audit

Legend: COVERED means the test would fail if the AC were broken (checked by reading the bodies and, for the security-critical rules, by mutation). The "E2E" column is the live probe described below.

| AC ID | Status | Tests covering it | E2E | Gap / note |
|-------|--------|-------------------|-----|------------|
| AC-001 | COVERED | `TestComposeTrust_AC001_RelativeOrEmptyDirRefused`, `TestDockerUpdate_AC001_RelativeWorkDirStartsNoComposeProcess`; QA `TestQA_AC001_HostileDirectoryStringsNeverAccepted` (NUL, `..`, missing) | invalid and relative names refused | — |
| AC-002 | COVERED | `..._SymlinkedDirValidatedAtResolvedPath`, `..._SymlinkToUntrustedTargetRefused`, `TestDockerUpdate_AC002_ComposeRunsAtResolvedPathWithPinnedProject`, `TestComposeCore_AC002_VerifyMatchesResolvedWorkingDirLabel` | link to a trusted dir runs at the resolved path; link to an owner-untrusted dir refused with `owner` | Single-service case only. The partial-recreate case is **defect QA-D1** |
| AC-003 | COVERED | six `TestComposeTrust_AC003_*` tests; M1 killed | owner refusals for dir and for a file in a trusted dir | — |
| AC-004 | COVERED | six `..._AC004_*`; M2 and M22 killed | `world_writable` (0777), `group_writable` (untrusted group, mode 0775) | — |
| AC-005 | COVERED | four `..._AC005_*` | root-owned, and a user-private-group stack owned by a docker-group member (dir 0775, files 0664): accepted | — |
| AC-006 | COVERED | three `..._AC006_*` | `no_compose_file` after the file was removed | — |
| AC-007 | COVERED | 9-name table plus writable, symlink-escape, symlink-inside, non-regular | owner refusal on a compose file and on an override file | — |
| AC-008 | COVERED | `..._AC008_*` (six), `RootPrefixIsSeparatorAware`, config tests; M5 and M5b killed; QA `TestQA_AC008_DotDotPrefixedDirectoryNameIsInsideRoot` | roots `/srv/roots`: `/srv/roots/..foo` accepted, sibling `/srv/roots-x/app` refused, outside dir refused, relative root gives `compose_roots_invalid` | — |
| AC-009 | COVERED | four `TestUpdateStack_AC009_*`, `TestSnapshot_AC009_ProjectResolution`; QA `TestQA_AC009_EmptyWorkingDirLabelIsNotASource` | project-only update succeeds; `-f=/evil.yaml`, `../x` refused as `invalid_project` | — |
| AC-010 | COVERED | `..._MismatchedWorkDirRefused`, `..._TrailingSlashWorkDirAccepted`; QA `TestQA_AC010_TrailingSlashAndDoubleSeparatorsAreCleaned` | mismatch gives `work_dir_mismatch`; trailing slash accepted | — |
| AC-011 | COVERED | three `..._AC011_*`; QA `TestQA_AC011_WorkDirOnlyFormAcceptsEitherSpellingOfASymlinkedDir` | old-server form (`compose_work_dir` only) succeeds | — |
| AC-012 | COVERED | single-service, bulk, dependent, `composeArgs` zero-value tests | single update refused (`container_writable`); dependent whose dir became owner-untrusted was **not** recreated and the command failed at `restart_dependents` while the main update succeeded; a trusted dependent was recreated through real compose | — |
| AC-013 | COVERED (after QA) | `..._SanitizedNotWrittenWhenRecheckFails`, `..._DirChangedBetweenChecksRefused`, `..._SanitizedRemovedWhenThirdCheckRefuses`, `..._SkipsRefusedDir`, `..._SanitizedRemovalSkipsRefusedDir`; QA tests for checks #2 and #3 individually | sanitize retry ran end to end on a real compose validation error: copy written into the validated dir, used, removed | The existing `dir_changed` test pinned neither check alone (mutation) |
| AC-014 | COVERED | server tests in `stack_skip_test.go`, `targets_test.go`, `auto_update_test.go`; M24 to M27 killed | — | — |
| AC-015 | COVERED | vitest `[AC-015] manual stack update sends compose_project and compose_work_dir` (`stack.name` is the project label, see `groupContainersV2`) | — | — |
| AC-016 | COVERED | four `TestDoctor_AC016_*` | `riot-agent doctor` printed the roots line and one pass/fail line per project, for root and for the agent user | — |
| AC-017 | COVERED | table over 23 rules, precheck test | every refusal quoted above names the path and the rule | — |
| AC-018 | COVERED | the twelve tests in the impl table; M6 killed | a container whose image defines the label was ignored by doctor and update resolution; `container` update of it gave `label_provenance`; old-server form on its directory gave `project_not_found`; the real project updated normally beside it | — |
| AC-019 | COVERED | the AD-014 tests; M8, M12 killed; QA `TestQA_AC019_SymlinkedBindSourceIntoStackDirRefused` | `./:/app` rw refused, naming container and mount; `:ro` bind accepted; `./config:/config` accepted | See **QA-D2** for a side effect of condition 2 |
| AC-020 | PARTIAL, see QA-D3 | eight tests plus a 13-case table; M15 killed; QA CRLF tests | explicit `-f` overrode `.env` `COMPOSE_FILE`; CRLF `.env` honoured with two files; world-writable `COMPOSE_FILE` target refused; unreadable `.env` refused with the hint | A BOM-prefixed `.env` is silently misread (**QA-D3**) |
| AC-021 | COVERED (Windows rules); Linux test fixed by QA | four tests | Windows rules are seam-tested only (no Windows host here) | The Linux CI failure was in this AC's test |
| AC-022 | COVERED | three tests; M3 killed | a real xattr ACL on the container filesystem gave `acl` (so the Linux `Getxattr` path works) | — |
| AC-023 | COVERED | `..._SocketWithoutGroupRWDoesNotTrustMembers`; M4 killed | with the socket at mode 0640 the group-member stack was refused as `owner`; with 0660 it was accepted | — |
| AC-024 | COVERED | five tests plus condition-3 tests; M13b, M14 killed | UPG accepted; a group with an untrusted member refused | — |
| AC-025 | COVERED | `..._AC025_*` (three plus two) | two projects on one dir gave `ambiguous_project` | — |
| AC-026 | COVERED | `TestComposeEnv_AC026_*`, `TestDockerUpdate_AC026_*`; M7, M7b, M32 killed | real compose ran against the pinned socket (a proxy socket with a non-default path) | — |
| AC-027 | COVERED (after QA) | `..._ControlCharsQuotedAndErrnoCollapsed`; QA cap tests | all messages quote values | The cap assertions in the original test were not discriminating (mutation) |
| AC-028 | COVERED | two tests; M10 killed | — | — |

### Re-review conditions 1 to 6

| # | Condition | Verdict | Evidence |
|---|---|---|---|
| 1 | Verification gate | Met, independently re-verified | On compose **v5.6.0** (Linux): with `.env` `COMPOSE_FILE=other.yaml`, no `-f` loads `other.yaml`; an explicit `-f` loads only the named file (so `-f` overrides `.env`). `--project-directory`, `--project-name=` and repeated `-f=` are accepted and `pull`/`up` ran with them in every e2e probe. The `.env` was still read from the project directory. |
| 2 | Unresolvable mount source refuses | Met in code and tests, but see **QA-D2** (availability impact) | |
| 3 | systemd/homed enumeration and `+`/`-` lines | Met | M14 and `..._CompatPlusAndMinusLinesMarkDBUnavailable` |
| 4 | Visible skip | Met, with the log-rate gap in **QA-D5** | event tests; limiter eviction now tested |
| 5 | Key-presence provenance | Met | M6 killed by `TestSnapshot_AC018_EmptyValueImageLabelExcluded` |
| 6 | README residual risks | Met | `README.md` "Not covered" section contains the three items |

---

## End-to-end probe on Linux (live Docker daemon)

**Setup.** A Linux container with the Docker CLI and compose v5.6.0 mounted the host Docker socket. A socat proxy inside it exposed the socket as `root:docker` mode 0660 on a non-default path, so group trust could be exercised without touching the real socket. Users: `riot` (docker group, the agent), a docker-group stack owner with a private group, an untrusted owner, and an untrusted group member. Stacks were created under `/srv/stacks` with `docker compose up` from inside the container, so labels carry container paths. The agent was built for Linux without cgo. A throwaway Go test (never committed; kept outside the repo) drove `docker_update` through the real command intake, the real Docker client, the real `osTrustFS`, and real `docker compose` child processes, run as `riot`. All objects used the `qa-autoupd-` prefix and were removed afterwards.

**`riot-agent doctor` (as root and as `riot`, same results for the stacks below).**

| Stack | Expected | Observed |
|---|---|---|
| root-owned | pass | pass |
| user-private-group (dir 0775, files 0664, owner and group a docker-group user) | pass | pass |
| `./:/app` rw bind | `container_writable` naming container and mount | `container_writable`: `"/srv/stacks/rw" is under "/srv/stacks/rw", the read-write mount ("/srv/stacks/rw" → "/app") of container "qa-autoupd-rw-svc-1"` |
| `./:/app:ro` bind | pass | pass |
| `./config:/config` bind (ancestor of the source) | pass | pass |
| image with the working-dir label | warning `ignored as a compose source: its image defines the compose working-dir label`, other projects unaffected | as expected |
| `.env` `COMPOSE_FILE` pointing at a 0666 file | refused | `world_writable` naming that file |
| dir 0777 | refused | `world_writable` |
| dir owned by an untrusted user | refused | `owner`, `uid 1002` |
| file owned by an untrusted user (compose file; override file) | refused | `owner` naming the file |
| dir group-writable by a group with an untrusted member | refused | `group_writable`, gid and mode in the message |
| dir with a real POSIX ACL | refused | `acl` |
| no compose file | refused | `no_compose_file` |
| `.env` mode 0600 root-owned, agent is `riot` | refused with hint | `env_unreadable` with "grant the agent user read access (e.g. group docker, mode 0640)" |
| symlink to a dir owned by an untrusted user | refused | `owner` |
| roots set: nested `..foo`, sibling `roots-x`, outside, relative entry | accept, refuse, refuse, fail closed | as expected, including `compose_roots_invalid` |
| socket at mode 0640 | docker-group stack no longer trusted | refused as `owner`; root-owned still passes |

**Real updates through the agent code path (as `riot`):** project plus work dir, project only, old-server form (work dir only), trailing slash, symlinked dir (label is the link path), CRLF `.env` with two files, single-service update, and the sanitize-retry path all **succeeded**. These were **refused at `precheck` with the right rule and nothing touched**: `rw` bind, `.env` `COMPOSE_FILE` to an untrusted file, label-provenance container, image-label directory in the old-server form (`project_not_found`), `work_dir_mismatch`, `invalid_project` for `-f=/evil.yaml` and `../x`, roots violations. A network dependent whose directory had become untrusted was not recreated and surfaced as `restart_dependents` failure with the main update succeeded; a trusted dependent was recreated through real compose. No `.compose.riot-sanitized.yml` remained anywhere after the sanitize probe.

**Found by the probe (not visible in unit tests):** QA-D1, QA-D2, QA-D3, QA-D4 below.

**Cleanup:** all `qa-autoupd-*` containers, compose networks, images and the Go cache volume were removed, along with the dangling images the probe's builds left. The pulled base images (`golang:1.24`, `docker:cli`, `alpine:3`) were left as shared cache. The other containers on the machine were not touched.

---

## Test Quality Findings

- Behaviour over implementation: the validator is tested through `Check` and the lifecycle harness, not private helpers. Fakes (`fakeTrustFS`, `rootOwnedFS`) are map-backed and platform independent, and the only real-FS test (`compose_trust_unix_test.go`) is robust under root and non-root (verified).
- **False coverage found (closed):** the three-way `dir_changed` re-check and the two message caps were each protected only by a redundant sibling guard. See the mutation table.
- **Platform bug in a test (fixed):** the Linux CI failure above.
- Mock drift: `fakeProcs.envs` records the env the real `newExecProcess` receives, and the e2e probe confirmed the real child gets the pinned `DOCKER_HOST` (compose reached the proxy socket, not the default one).
- Boundary conditions: `..foo`, sibling prefix, trailing and doubled separators, empty label value, empty and 1-byte unquoted values are covered (some added by QA).

## Adversarial Findings

| ID | Severity | Finding | Test written |
|----|----------|---------|--------------|
| QA-D1 | **Medium (blocks)** | A symlinked stack directory ends up with mixed working-dir labels after a partial recreate, then every stack update is refused as `ambiguous_project` and the server raises a misleading "may have copied the project label" skip event | `TestQA_D1_MixedLinkAndResolvedLabelsDoNotBlockStackUpdate` (red) |
| QA-D3 | **Medium (blocks)** | A BOM-prefixed `.env` that sets `COMPOSE_FILE` is silently misread; the agent starts the default `compose.yaml` services instead | `TestQA_D3_BOMPrefixedEnvComposeFileIsNotSilentlyIgnored` (red) |
| QA-D2 | Medium, needs the product owner | One container with a read-write bind mount whose path the agent user cannot traverse (a 0750 home directory) blocks **every** compose update on the host; the doctor, run as root, says all is well | none (design decision, current behaviour already pinned by `..._AC019_UnresolvableMountSourceRefused`) |
| QA-D4 | Low | Any unrelated `.env` key whose name merely starts with `COMPOSE_FILE` (for example `COMPOSE_FILES_DIR=/data`) is refused as `env_unparsed`, with a message that wrongly says the file sets `COMPOSE_FILE` | none |
| QA-D5 | Low | The server's skip `Warn` log is not rate-limited: one line per telemetry cycle per skipped stack (about 1440 per day at the default poll). Capped at ~250 bytes each, so about 360 KB per policy per day. The Bounds section covers the event but not this line | none |
| INFO | Info | The Bounds accounting counts one agent `Warn` line per refusal; two are written (`docker_update: compose directory refused` and `command: failed`, each with the full message). Doubles that estimate to about 22 KB per refused policy per day at steady state. Still bounded | — |
| INFO | Info | README says "Path components must be plain directories, not symlinks". The directory is resolved first, so a symlinked stack directory is accepted (verified). Wording could mislead operators | — |

No security bypass was found. The adversarial paths exercised live and in tests all fail closed: image-label hijack (old-server form, container update, project update beside it), `.env` `COMPOSE_FILE` to an untrusted file, `-f=`/`..` injection through the project name, NUL and control characters, symlinked mount source into the stack directory, ACL, group trust with an untrusted member, socket mode, and a dependent whose directory turned untrusted mid-flight.

### QA-D1 detail (reproduced live)

Compose runs at the resolved path (AD-006), so a container it recreates gets the **resolved** directory in its working-dir label, while containers it leaves alone keep the **link** path. With a two-service stack under a symlinked directory and a config change to one service only:

1. Stack update 1 (project plus link path): success. Labels afterwards: service `a` has the real directory, service `b` still has the link path.
2. Stack update 2 (project only): `compose directory refused (ambiguous_project): compose project "..." has containers in more than one working directory: "<link>", "<real>"`.
3. A single-service update of `b` succeeds but recreates nothing, so the labels do not heal. The stack stays blocked until every service has been recreated.

The same applies when only a parent directory is a symlink (for example `/opt/stacks` pointing at another volume), which is a common layout. The server has the same blind spot: `composeProjectWorkDirs` sees two directories and withholds the dispatch and raises `stack_update_skipped`, which tells the operator a container "may have copied the project label". The server cannot resolve symlinks, so the fix should treat two labels that resolve to one real directory as one directory on the agent (the agent already has `EvalSymlinks`), and the server skip should not fire on a mix that the agent would accept (for example, let the agent decide and drop the server skip, or have the agent report resolved directories).

Red test: `TestQA_D1_MixedLinkAndResolvedLabelsDoNotBlockStackUpdate` in `internal/agent/compose_trust_qa_defects_test.go`.

### QA-D3 detail (reproduced live)

Compose v5.6.0 honours `COMPOSE_FILE` in a `.env` that starts with a UTF-8 BOM, and in a CRLF `.env`. The agent handles CRLF but not the BOM: the first line starts with the BOM bytes, so it is not recognised as a candidate line and the file is accepted with no `COMPOSE_FILE`. The agent then passes the default `compose.yaml` as `-f`. Live result: an update of a stack whose `.env` was `<BOM>COMPOSE_FILE=custom.yaml` created the `svc` service from `compose.yaml` next to the operator's real `other` service. This is not a privilege problem (all files were validated), but AC-020 says an unparseable-with-certainty `.env` is refused, and a silent wrong file set is worse than a refusal. Either strip a leading BOM before parsing (compose does) or refuse a `.env` that starts with one, with a message that says so.

Red test: `TestQA_D3_BOMPrefixedEnvComposeFileIsNotSilentlyIgnored` (accepts either outcome: refusal with `env_unparsed`, or the file list `[base.yml]`).

### QA-D2 detail (reproduced live)

Condition 2 requires that a writable mount source that fails `EvalSymlinks` for a reason other than not-exist refuses the check. Live: one stopped-or-running container with `-v /home/<user>/data:/data` (home directory mode 0750, agent user not in that group) made `EvalSymlinks` fail with a permission error as `riot`, so **every** compose update on the host failed with `container_writable ... cannot resolve "/home/<user>/data", the read-write mount ... of container "<name>"`. Run as root, `riot-agent doctor` printed "trusted" for the same stack, because root can traverse. Ubuntu 21.04 and later create home directories at 0750, so this is a likely real-world layout. The refusal message does name the container and mount, which is what an operator needs to act. The design is as the security review required, so this is the product owner's call. Suggested mitigations: document it next to `container_writable` in the README, and make the doctor warn when it runs with more privilege than the agent user (or evaluate the mount-source resolution as the agent user), so a green doctor does not hide a refusing agent.

---

## Compatibility pairings

| Pairing | Result |
|---|---|
| New server to new agent (`compose_project` + `compose_work_dir`) | Verified live, success and cross-check refusal |
| New server to old agent | The pre-change `targetFromParams` and `markerTarget` read only named keys and ignore `compose_project` (checked in `8873911`); no strict param validation on either side, and the server forwards params generically. Not executed against an old binary |
| Old server (`compose_work_dir` only) to new agent | Verified live and in `AC011` tests |
| Dashboard manual update | `ComposeStackSection` sends both; vitest passes |
| `.env` line endings | LF and CRLF OK; **BOM not OK (QA-D3)** |

## Standing requirement: self-healing and bounded

| Item | Result |
|---|---|
| Every new log line, event, stored string size-capped | Yes for refusals (1024 / 200), recovery summary (300), dependent reason (1200), server event (700 / 5 dirs / 200 each), doctor lines (1100). Cap tests now discriminate. Result messages are also capped by the existing `MaxResultMsgBytes`. The server Warn skip line is capped but not rate-limited (QA-D5) |
| Refusals do not cause retry storms | A refusal is an ordinary failed run, so the existing backoff applies (`TestAUTOUPD_RefusalCountsAsFailureForBackoff`, `..._RefusedStackDispatchBacksOff`). Agent side: one precheck result per attempt, no Docker change |
| One event per cooldown | `SkipEventRaisedOncePerWindow`, `SkipEventWindowFollowsLongerCooldown` |
| Temp files cleaned on every error path | `defer removeSanitized`, refusal at check #2 (never written) and #3 (removed), write/sync/close failure paths remove the file, startup cleanup is gated; live sanitize probe left nothing behind |
| In-memory map bounded | `skipLimiter` keys are (device, policy) pairs and are swept on every call; `TestQA_SkipLimiterEvictsExpiredKeys` shows 5000 expired keys collapse to 1. The agent snapshot is per check and discarded |
| Self-healing | **QA-D1** is a self-inflicted state that does not heal |

## Tests Added by QA

| File | Tests | Covers |
|---|---|---|
| `internal/agent/compose_trust_qa_test.go` | `TestQA_AC013_Check2ComparesFileSetBeforeSanitizedCopyIsWritten`, `TestQA_AC013_Check3ComparesFileSetBeforeUp`, `TestQA_AC027_LongValueIsCutBeforeTheWholeMessageCap`, `TestQA_AC027_WholeMessageCapHoldsWhenEveryValueIsLong`, `TestQA_AC008_DotDotPrefixedDirectoryNameIsInsideRoot`, `TestQA_AC010_TrailingSlashAndDoubleSeparatorsAreCleaned`, `TestQA_AC009_EmptyWorkingDirLabelIsNotASource`, `TestQA_AC020_CRLFEnvIsParsed`, `TestQA_AC020_CRLFSeparatorLine`, `TestQA_AC019_SymlinkedBindSourceIntoStackDirRefused`, `TestQA_AC001_HostileDirectoryStringsNeverAccepted`, `TestQA_AC011_WorkDirOnlyFormAcceptsEitherSpellingOfASymlinkedDir` | AC-001, 008, 009, 010, 011, 013, 019, 020, 027 (all pass on Windows and Linux) |
| `internal/server/handlers/stack_skip_qa_test.go` | `TestQA_SkipLimiterEvictsExpiredKeys`, `TestQA_SkipLimiterNilHoldsNothing` | bounded in-memory state |
| `internal/agent/compose_trust_qa_defects_test.go` | `TestQA_D1_MixedLinkAndResolvedLabelsDoNotBlockStackUpdate`, `TestQA_D3_BOMPrefixedEnvComposeFileIsNotSilentlyIgnored` | **intentionally red repros** of QA-D1 and QA-D3; they go green when the defects are fixed |
| `internal/agent/compose_trust_test.go` (edit) | `TestComposeTrust_AC021_WindowsRootsCaseInsensitive` | Linux CI failure fix (test-only) |

## Deviations from ADD

None that change a security property beyond those the implementation report lists (nine, all reviewed and accepted as non-blocking). One addition: QA-D1 shows AD-006's "second key covers the label compose writes after running at R" handles one label spelling at a time but not a project that carries both.

## Deviations from FRD

- FRD section 5 ("stack updates keep working for trusted directories") is not met for a symlinked stack directory after a partial recreate (QA-D1).
- AC-020 ("a `.env` that cannot be parsed with certainty is refused") is not met for a BOM-prefixed `.env` (QA-D3).

## Verdict Rationale

**FAIL.** The security design held up under live probing, and the AC coverage is complete. But two defects break FRD guarantees and are reproduced by red tests: QA-D1 leaves a stack unable to update (and misreports it as a possible forged label), and QA-D3 can silently run the wrong services. Both are self-contained fixes in `internal/agent`. The remaining items are notes.

## Action Required (senior dev)

1. **QA-D1** (`internal/agent/compose_snapshot.go`, `resolveProject` L165-192, `projectsForDir` L143-160, `scopeOf` L199-218; `internal/server/handlers/stack_skip.go` `composeProjectWorkDirs` and `internal/server/handlers/auto_update.go` L377-396): two working-dir labels that resolve (via `EvalSymlinks`) to the same real directory must count as one directory. The agent has `trustFS.EvalSymlinks`; pass it into the snapshot helpers or resolve labels once in `loadDaemonSnapshot`. Decide what the server does about a mix it cannot resolve: it should not raise `stack_update_skipped` with the "copied the project label" text for a mix the agent accepts (the agent now refuses a genuinely ambiguous project itself). Repro: `go test ./internal/agent -run TestQA_D1`, and the live steps above.
2. **QA-D3** (`internal/agent/compose_trust.go`, `parseEnvComposeFile` L1060-1095): strip a leading `\xEF\xBB\xBF` before the line loop (compose accepts it), or refuse a `.env` that starts with one as `env_unparsed` with a message that says "remove the byte order mark". Repro: `go test ./internal/agent -run TestQA_D3`.
3. Run the full suite on Linux (`go test -race`) before re-review. After steps 1 and 2 the two `TestQA_D*` tests must be green and the rest must stay green (QA verified the baseline: only those two fail today).

## Notes (non-blocking, for the product owner and follow-up)

- **QA-D2:** decide whether an unrelated container's untraversable read-write mount should block all compose updates on the host. At least document it (README, next to `container_writable`) and make the doctor honest when it runs as root (it passes where the agent refuses).
- **QA-D4:** match `COMPOSE_FILE` and `COMPOSE_PATH_SEPARATOR` as whole keys (the next character must be `=`, whitespace, or end of line) so an unrelated key is not refused. Low impact.
- **QA-D5:** rate-limit the server skip `Warn` log with the same limiter as the event, and fix the Bounds estimate to count both agent log lines per refusal.
- README: reword "Path components must be plain directories, not symlinks" to say a symlinked stack directory is resolved first.

---

# Round 2

**Date:** 2026-10-19
**Inputs:** senior dev's fixes `79e6135`, `c7487ef`, `3edfaf8`, `edfd896`, `626366d` on top of the security ruling `16d9a40`; the "QA round 1 fixes" section of the implementation report; the "QA-D2 ruling" at the end of the security review. Orchestrator decisions taken as given: the server no longer skips a multi-directory stack (it dispatches `compose_project` only and the agent decides; the `stack_update_skipped` event was removed), and a project-only dispatch holding the compose keys of every reported working directory is accepted.

**Verdict: PASS WITH NOTES.** All round 1 defects are fixed and verified live. No blocking findings remain. Notes are at the end.

## Re-run of the suites

| Run | Result |
|---|---|
| Windows `go test -count=1 ./...` | 21 packages ok, 0 failing (the two former red repros are green) |
| `go vet ./...` | clean |
| `cd web && npm run test:run` | 66 files, 1410 tests passed |
| `cd web && npx tsc --noEmit` | clean |
| Linux `golang:1.24`: `go vet`, then `go test -count=1 -race ./internal/agent/... ./internal/server/... ./internal/models/...`, tree exported with `git archive`, LF | **all packages ok as root** |
| Same agent test binary (`-race`) as non-root uid 1234, `-test.count=2` | **ok; 352 top-level passes in one run, no failures** |
| After the QA round 2 additions: Linux `-race` for `internal/agent`, `server/handlers`, `server/commands` as root; agent binary as uid 1234 with `-test.count=2` | ok; 356 top-level passes |

No flaky tests.

## Live re-probe on Linux (same method as round 1)

A Linux container with the Docker CLI and compose v5.6.0 mounted the host Docker socket, with a socat proxy exposing a `root:docker` mode 0660 socket. The agent (built without cgo from the fixed tree) and a throwaway driver test (not committed) ran as the docker-group user `riot` against the real daemon and real `docker compose`. All objects used the `qa-autoupd-` prefix.

**QA-D1 (symlinked directory, mixed labels): fixed.** Two-service stack under a symlinked directory, config change to one service:

| Step | Result |
|---|---|
| update 1: project plus link path | success; labels afterwards mixed (`a` real directory, `b` link path), exactly the round 1 failing state |
| update 2: project only | **success** (round 1: `ambiguous_project`) |
| update 3: project plus link path | success |
| update 4: project plus target path | success |
| old-server form, link path | success |
| old-server form, target path | success |
| project plus a work dir belonging to another stack | refused `work_dir_mismatch` |
| a genuine two-directory project, project only | refused `ambiguous_project`, naming both directories, at precheck, nothing touched |

The old-server form with one directory of a genuine two-directory project still proceeds in that directory. That is the round 1 behaviour of AC-011 and is not new.

**QA-D3 (BOM) and QA-D4 (key match): fixed.** A stack whose `.env` is `<BOM>COMPOSE_FILE=custom.yaml` updated and only the service from `custom.yaml` remained (round 1: a second service from `compose.yaml` appeared). A stack whose `.env` held `COMPOSE_FILES_DIR=/data` updated normally (round 1: refused `env_unparsed`).

**QA-D2 (unreadable mount source, security ruling): behaves as ruled.** The agent user could not traverse a root-owned 0750 sandbox directory P, and a container had a read-write bind source below it.

| Scenario | Expected | Observed |
|---|---|---|
| P root-owned 0750, source below it | accepted | update **succeeded** (round 1: every update refused) |
| doctor as the agent user | info line for the source compared through its prefix | `i mount source "<src>" of container "<name>" was compared through its readable prefix "<P>" (the agent user cannot read below it)`, project line passes |
| doctor as root | euid warning naming the service user | `! the doctor runs as uid 0 but the agent runs as user "riot" (uid 1000): ... run it as that user, for example: sudo -u riot riot-agent doctor` |
| P owned by an untrusted account | refused | `container_writable ... cannot resolve ...; the last readable directory fails the ownership rules (owner)` |
| P group-writable by a group with an untrusted member | refused | same message with `(group_writable)` |
| another container has a read-write source at P (above the unresolved source) | refused | `...; another read-write mount of a container is above it and could redirect it` |
| symlink loop in the source (ELOOP) | refused | `cannot resolve ...`, no fallback |
| offending container removed | updates succeed | success |

The ruling's note holds: a home directory owned by an ordinary user still blocks the host, because that user can plant symlinks below it. This is intended and the README says so.

## Mutation spot-check of the new guards

Production mutants were applied to a scratch copy only and reverted.

| Mutant | Result |
|---|---|
| fallback allowed for any error, not only permission errors | killed |
| `typeWalk` of P removed | killed |
| owner/mode/ACL walk of P removed | killed |
| resolved-prefix ancestor test removed | killed |
| compare through the prefix instead of prefix plus remainder | killed |
| label resolver not applied (QA-D1) | killed (`TestQA_D1_...`, `TestUpdateStack_AC009_LinkAndTargetLabelsAreOneDirectory`) |
| verification scope without resolver (QA-D1) | killed |
| BOM trim removed (QA-D3) | killed |
| exact-key match replaced by a prefix match (QA-D4) | **survived, then killed by a QA test** (below) |
| lexical-ancestor test removed | **survived, then killed by a QA test** (below) |
| doctor euid warning disabled; fallback info line disabled | killed |
| server keeps `compose_work_dir` for multi-directory stacks | killed |
| server holds only the first reported key | killed |
| Windows guard of the fallback removed | survived; equivalent, since the trust set is empty on Windows and the owner rule refuses anyway (defence in depth) |
| `len(rem)==0` guard removed | survived; equivalent, a non-nil error always leaves at least one remaining component |

### Test gaps found and closed in round 2

1. **Two tests named in the implementation report were never committed.** The report lists `TestComposeTrust_AC020_BOMPrefixedEnvIsParsed` and `TestComposeTrust_AC020_OnlyExactKeysAreParsed`. Commit `3edfaf8` changes only `compose_trust.go`, and no test of either name exists. QA-D3 was protected only by QA's own repro, and QA-D4 by nothing (the prefix mutant survived). QA added `TestQA_AC020_BOMPrefixedEnvIsParsed`, `TestQA_AC020_BOMDoesNotWeakenRefusal` and `TestQA_AC020_OnlyExactKeysAreParsed` (five lookalike keys are ignored, five exact-key forms are still refused). The mutant is now killed. The implementation report should be corrected.
2. **Lexical-ancestor guard not pinned.** The existing "another writable source above" tests are also caught by the resolved-prefix guard, so removing the lexical test survived. The case only the lexical test catches is an ancestor source that is itself unreadable (a container could replace the inner path component with a symlink as root). `TestQA_AC019_FallbackRefusedWhenTheSourceAboveIsAlsoUnreadable` covers it and kills the mutant.

## Bounds accounting

Confirmed against the code and the corrected Bounds section:

- No server event type, limiter, map or log line remains from the skip path: a search finds no `skipLimiter`, `stack_update_skipped` or `noteStackSkipped` in Go, web or README. This closes the unbounded-map point and QA-D5.
- The agent writes two Warn lines per refusal, as the corrected report says. The recomputed numbers check out: about 3.9 KB per attempt, 8 attempts on the first day (about 31 KB per refused policy), 4 per day at the 6 hour backoff ceiling (about 16 KB), about 160 KB for ten refused policies. The backoff schedule (15, 45, 105, 225, 465, 825, 1185 minutes) was rechecked.
- The project-only duplicate guard (compose keys of every reported directory) is real and mutation-checked. QA accepts the orchestrator's ruling on it; it adds no new key kind.
- New in-memory state is the per-snapshot label memo, bounded by the container count and discarded with the snapshot. Temp-file handling was not touched; the live sanitize path was not re-run.
- The refusal size caps are pinned by the round 1 QA tests, which still pass.

## Tests added by QA in round 2

| File | Tests | Covers |
|---|---|---|
| `internal/agent/compose_trust_qa_test.go` | `TestQA_AC019_FallbackRefusedWhenTheSourceAboveIsAlsoUnreadable`, `TestQA_AC020_BOMPrefixedEnvIsParsed`, `TestQA_AC020_BOMDoesNotWeakenRefusal`, `TestQA_AC020_OnlyExactKeysAreParsed` | AC-019 (QA-D2 ruling), AC-020 (QA-D3, QA-D4) |
| `internal/agent/compose_trust_qa_defects_test.go` | header comment only; both tests are now green regression guards | QA-D1, QA-D3 |
| `internal/server/handlers/stack_skip_qa_test.go` | removed by senior dev in `79e6135` along with the code it tested | n/a |

## Notes (non-blocking)

- **Implementation report accuracy.** It names two tests that do not exist (above). Correct the report or add tests under those names.
- **Old agents.** A project-only dispatch to an agent older than this story fails its existing "required" precheck, so a multi-directory stack never updates on an old agent. It fails visibly with the normal backoff, as the orchestrator decided.
- **Old-server form.** With a work dir naming one directory of a genuine two-directory project, the agent proceeds in that directory (AC-011 as built). Unchanged by this round.
- **Cleanup.** All `qa-autoupd-*` containers, networks, the cache volume and the image were removed. The pulled base images `golang:1.24`, `docker:cli` and `alpine:3` remain as shared cache. A container named `growth001-pg-test` appeared on the machine during the session; it is not QA's.

## Final verdict

**PASS WITH NOTES.** The round 1 blockers QA-D1 and QA-D3 are fixed and verified on a live daemon. QA-D4, QA-D5 and the QA-D2 security ruling are implemented as specified and verified live. The Linux suites are green as root and non-root. The new guards are mutation-checked, with two gaps closed by QA tests.
