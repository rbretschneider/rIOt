# Implementation Report

- **Story ID:** SHUTDOWN-SUDO-001
- **Engineer:** Senior Dev Agent
- **Date:** 2026-10-26
- **Branch:** `feat/shutdown-sudo-001`
- **Detected stack:** Go 1.24 module `github.com/DesyncTheThird/rIOt` (agent, server; `gopkg.in/yaml.v3` v3.0.1, testify), Bash installer (`scripts/install.sh`), Python 3 `unittest` container fixtures, GitHub Actions CI. React/TypeScript front end (Vitest) is untouched. Specialists active: backend (Go), infrastructure (installer, CI). No database work, no migration, no new dependency.

## Completed Components

| File | Action | Notes |
|---|---|---|
| `scripts/install.sh` | MODIFY | New Linux-only prompt after the reboot prompt. The prompt block (`ask_yn`, Docker detection, prompts) moved to directly after asset verification (AD-001). `SHUTDOWN_GRANT` decision and the AD-008 transparency line (AD-002). Fail-closed bash and awk classifier `config_enables_shutdown` between the marker comments (AD-003). `refuse_unsafe_config_path` (AD-007 plus the SEC-001 hard-link refusal) early and after the lockdown. `require_shutdown_agreement` (AD-005) after the lockdown and again after the config write. `GRANT_PENDING` and the trap restore (AD-006). One conditional sudoers line. `COMMANDS_SECTION` writes `allow_shutdown: true` only when `SHUTDOWN_GRANT=true`. |
| `scripts/test_install_maintenance.py` | MODIFY | `curl` stub logs `DOWNLOAD <name>`; `systemctl` stub gains `/fixture/fail-stop` and `/fixture/on-stop`; power tools become tripwires; `tearDown` fails on any power verb. Helpers and 26 new `test_SS_*` methods. Golden bytes from the pre-story installer. |
| `scripts/shutdown_grant_test.go` | CREATE | Static installer checks and the classifier differential test against `agent.LoadConfig`. |
| `internal/agent/shutdown_doctor.go` | CREATE | `shutdownProbeTimeout` (10 s), `shutdownSudoCommand`, `shutdownProbeArgs`, `reportShutdownPrivilege`. |
| `internal/agent/shutdown_doctor_test.go` | CREATE | Doctor tests with an injected runner, the `execShutdown` argv pin, the SEC-002 reference rule and its self-test. |
| `internal/agent/doctor.go` | MODIFY | One call after the Permissions block (4 added lines). The `Shutdown: <bool>` line is unchanged. |
| `internal/agent/maintenance_doctor.go` | MODIFY | `agentPolicyRunner` extracted unchanged; `checkMaintenancePolicy` uses it. |
| `internal/agent/power_paths_test.go` | MODIFY | Additive C-4 allowance (`powerListingFuncs`, `listingFuncShapeProblem`) and `TestPowerPaths_SS_C4_ListingAllowanceSelfTest`. Only additions: `git diff main` shows no removed line. The two existing `TestPowerPaths_SD_C4_*` functions are byte-identical. |
| `.github/workflows/ci.yml` | MODIFY | The fixture step runs on `debian:bookworm-slim` and `debian:trixie-slim`. |
| `README.md`, `CHANGELOG.md` | NOT DONE | Technical writer's files (CLAUDE.md pipeline). See Deviations D-1. |

Untouched, as required: `internal/agent/commands.go`, `reboot_callsites_test.go`, `shutdown_clearance_test.go`, `shutdown_qa_test.go`, `internal/server/**`, `web/**`, `cmd/**`, `go.mod`, `go.sum`, the `AGENT-PRIV` branch.

### Commits

| Commit | Subject |
|---|---|
| `1e73491` | `[SHUTDOWN-SUDO-001] feat: opt-in poweroff grant in the installer` |
| `c80f726` | `[SHUTDOWN-SUDO-001] test: installer container fixtures for the poweroff grant` |
| `ecec2d5` | `[SHUTDOWN-SUDO-001] feat: doctor checks the poweroff sudo rule` |
| `90ca347` | `[SHUTDOWN-SUDO-001] test: doctor shutdown check and the C-4 reference rule` |
| `cfc61f0` | `[SHUTDOWN-SUDO-001] test: installer source checks and classifier differential test` |
| `781c0d3` | `[SHUTDOWN-SUDO-001] config: run the installer fixtures on Debian 13 as well` |

## Security review conditions

| Condition | How it is met | Verified by |
|---|---|---|
| SEC-001 (stricter option) | `refuse_unsafe_config_path` refuses a non-regular `agent.yaml` (dangling symlink, directory, FIFO, device) and any regular file whose link count is not 1 (`stat -c %h`). It runs before any mutation and again right after `chown root:root` and `chmod 0700` of `/etc/riot`. Every root write, `chown` and `chmod` of `agent.yaml` is inside the branch that runs only when the path does not exist as a regular file. | Fixture `test_SS_SEC001_hardlinked_config_refused_before_any_change` (plain reinstall and bootstrap reinstall), `test_SS_SEC001_late_checks_run_after_the_stop_and_roll_back_the_grant` (hard link, dangling symlink and directory appearing while the agent is being stopped), `test_SS_AC011_reinstall_fails_closed` h1 to h3. Go `TestInstallSh_SS_SEC001_AgentYamlIsOnlyWrittenWhenAbsentAndLinksAreCounted`. |
| SEC-002 | AD-011 implemented additively and exactly. `powerListingFuncs` allows only `shutdownSudoCommand`, only in package `agent`, directory `.`, only for the shape `listingFuncShapeProblem` accepts. The reference rule pins `shutdownSudoCommand` to exactly two references and `shutdownProbeArgs` to one, the probe only as `run(ctx, "sudo", shutdownProbeArgs()...)`. | `TestPowerPaths_SS_C4_ListingAllowanceSelfTest` (12 negatives, 2 positives and a neighbour check), `TestShutdownDoctor_SS_C4_ListingCommandOnlyFeedsTheProbe`, `TestShutdownDoctor_SS_C4_ReferenceRuleSelfTest` (12 violations). `git diff main -- internal/agent/power_paths_test.go` has no removed lines. |
| SEC-003 | The late refusal and the agreement re-read run after the lockdown and before the bootstrap `rm` and the config write. `GRANT_PENDING` is set on the line after the drop-in install, and cleared directly before every `restore_policy` and on agreement success. `cleanup` restores the policy, then kills the lock helper, then removes `$STAGE`. | Go `TestInstallSh_SS_SEC003_RuntimeChecksFollowTheLockdownAndPrecedeAnyWrite` (offset order, set and clear points, all four `restore_policy` sites, trap order). Fixtures `test_SS_AC017_agreement_holds_when_the_config_changes_under_the_run`, `test_SS_OQA2_*`, `test_SS_AC009_*`. |
| SEC-004 | Every Linux reinstall prints exactly one AD-008 line (granting, not enabled, or re-enrolment). A fresh install prints none. The "not enabled" line states that only a plain `allow_shutdown: true` under `commands:` in a regular file counts. | `assert_transparency` in the AC-010, AC-011, AC-012, AC-013, AC-014, AC-015 fixtures (exact text) and the fresh-install fixtures (zero lines). |
| SEC-005 | Out of scope. `checkUpdatePrivilege` is unchanged. | Diff review. |

### SEC-001 residual (scrubbed)

- **Closed:** a hard-linked `agent.yaml` is never classified, written, chowned or chmodded by a run that sees it, at either check.
- **Remaining 1:** the early classification read (before the lockdown) reads a path the agent user could still change just before the read. The decision it feeds is not trusted on its own: the re-read after the lockdown (agent stopped, directory root-only) decides the final state, and a disagreement restores the previous policy.
- **Remaining 2:** after the late check, the agent user cannot reach `/etc/riot`, so it cannot add a link to the file or replace it before the final read.
- **Remaining 3:** the kernel default `fs.protected_hardlinks=1` on the fleet's distros was relied on by the review and is not re-tested here.
- **Accepted by the review (OQ-A3, NFR-008):** a compromised agent can set `allow_shutdown: true` in its own file. The next root re-run then grants poweroff. The AD-008 line shows Ryan that decision at re-run time.

## Test Summary

Test-file locations follow the repository: Go tests next to the code (`internal/agent`, `scripts`), installer fixtures in `scripts/test_install_maintenance.py`. Fixture methods are `InstallerFixtures.test_SS_*`.

| AC ID | Test file | Tests | Status |
|---|---|---|---|
| AC-001 | `scripts/test_install_maintenance.py` | `test_SS_AC001_fresh_yes_grants_poweroff_and_config_agrees` | PASS |
| AC-002 | `scripts/test_install_maintenance.py` | `test_SS_AC002_fresh_no_or_unrecognised_answer_grants_nothing` (empty, `n`, `no`, `maybe`, unreadable terminal) | PASS |
| AC-003 | `scripts/test_install_maintenance.py`, `scripts/shutdown_grant_test.go` | `test_SS_AC003_prompt_text_and_order`, `TestInstallSh_SS_AC003_PromptOrder` | PASS |
| AC-004 | `scripts/test_install_maintenance.py` | `test_SS_AC004_reboot_and_shutdown_independent` (4 subtests), `test_SS_AC004_template_helper_matches_pre_story_golden` | PASS |
| AC-005 | `scripts/test_install_maintenance.py` | `test_SS_AC005_yes_flag_answers_yes` | PASS |
| AC-006 | `scripts/test_install_maintenance.py` | `test_SS_AC006_non_interactive_and_no_terminal_answer_no` | PASS |
| AC-007 | `scripts/test_install_maintenance.py`, `scripts/shutdown_grant_test.go` | `test_SS_AC007_existing_lines_byte_identical`, `TestInstallSh_SS_AC007_RebootLineUnchangedAndPoweroffLineExact` | PASS |
| AC-008 | `scripts/test_install_maintenance.py`, `scripts/shutdown_grant_test.go` | `test_SS_AC008_only_exact_poweroff_is_granted` (9 refused variants, listing only), `TestInstallSh_SS_AC008_NoOtherPowerGrant` | PASS |
| AC-009 | `scripts/test_install_maintenance.py` | `test_SS_AC009_policy_validation_still_guards_yes_install` | PASS |
| AC-010 | `scripts/test_install_maintenance.py` | `test_SS_AC010_reinstall_true_emits_grant_without_question` (no URL, `-y`; pty) | PASS |
| AC-011 | `scripts/test_install_maintenance.py`, `scripts/shutdown_grant_test.go` | `test_SS_AC011_reinstall_fails_closed` (a to f, g1 to g4, True, yes, unparseable, h1 to h3), `TestInstallSh_SS_FR017_ClassifierNeverExceedsAgentLoader` | PASS |
| AC-012 | `scripts/test_install_maintenance.py` | `test_SS_AC012_flags_do_not_override_config` | PASS |
| AC-013 | `scripts/test_install_maintenance.py` | `test_SS_AC013_reinstall_revokes_stale_grant` | PASS |
| AC-014 | `scripts/test_install_maintenance.py` | `test_SS_AC014_preserved_config_untouched` | PASS |
| AC-015 | `scripts/test_install_maintenance.py` | `test_SS_AC015_bootstrap_reenrolment_resets_shutdown` (pty, `-y`) | PASS |
| AC-016 | `scripts/test_install_maintenance.py` | `test_SS_AC016_bootstrap_on_fresh_install_asks_and_grants` | PASS |
| AC-017 | `scripts/test_install_maintenance.py` | `assert_agreement()` at the end of every successful `test_SS_*` run, plus `test_SS_AC017_agreement_holds_when_the_config_changes_under_the_run` | PASS |
| AC-018 | `scripts/test_install_maintenance.py`, `scripts/shutdown_grant_test.go` | `test_SS_AC018_stubbed_darwin_install_unchanged` (output and config equal the pre-story bytes), `TestInstallSh_SS_AC018_ShutdownIsLinuxOnly` | PASS |
| AC-019 | `internal/agent/shutdown_doctor_test.go` | `TestShutdownDoctor_SS_AC019_PassNamesTheGrant` | PASS |
| AC-020 | `internal/agent/shutdown_doctor_test.go` | `TestShutdownDoctor_SS_AC020_FailWithGuidance` (4 subtests), `TestShutdownDoctor_SS_AC020_TimeoutBounded` | PASS |
| AC-021 | `internal/agent/shutdown_doctor_test.go` | `TestShutdownDoctor_SS_AC021_RootAsksAboutRiot`, `TestAgentPolicyRunner_SS_RewriteMatchesMaintenance` | PASS |
| AC-022 | `internal/agent/shutdown_doctor_test.go` | `TestShutdownDoctor_SS_AC022_NoCheckWhenOffOrNotLinux`, `TestShutdownDoctor_SS_AC022_DoctorWiring` | PASS |
| AC-023 | `internal/agent/shutdown_doctor_test.go` | `TestShutdownDoctor_SS_AC023_ProbeMatchesExecShutdownArgv` | PASS |
| AC-024 | `internal/agent/shutdown_doctor_test.go`, `internal/agent/power_paths_test.go` | `TestShutdownDoctor_SS_AC024_OnlyListingInvocations`, `TestShutdownDoctor_SS_C4_ListingCommandOnlyFeedsTheProbe`, `TestPowerPaths_SS_C4_ListingAllowanceSelfTest`; fixture `tearDown` tripwire | PASS |
| AC-025 | existing tests | `TestShutdownCallSites_SD_*`, `TestPowerPaths_SD_C4_*`, shutdown clearance and QA tests all pass unchanged; `git diff --exit-code main -- commands.go reboot_callsites_test.go shutdown_clearance_test.go shutdown_qa_test.go` is clean | PASS |
| AC-026 | `scripts/shutdown_grant_test.go` | `TestInstallSh_SS_AC026_FlagsAndUsageUnchanged` (usage text, flag parsing and URL check pinned to `71a4dea`); `git diff --stat main -- internal/server web cmd` is empty | PASS |
| AC-027 | (technical writer) | `TestDocs_SS_AC027_NoStaleShutdownStatements` | NOT WRITTEN, see D-1 |
| AC-028 | (technical writer) | `TestDocs_SS_AC028_FleetActionNamed` | NOT WRITTEN, see D-1 |
| AC-029 | `scripts/test_install_maintenance.py`, `internal/agent/shutdown_doctor_test.go` | `test_SS_AC029_bounded_growth`, `TestShutdownDoctor_SS_AC029_DoctorWritesNothing` | PASS |
| OQ-A1 | `scripts/shutdown_grant_test.go` | `TestInstallSh_SS_OQA1_PromptsPrecedeLocksAndStop` | PASS |
| OQ-A2 | `scripts/test_install_maintenance.py` | `test_SS_OQA2_failure_before_agreement_restores_previous_policy`, `test_SS_OQA2_failure_with_no_previous_dropin_leaves_none` | PASS |
| A-003 | `scripts/test_install_maintenance.py` | `test_SS_A003_bare_systemctl_resolves_to_granted_path` (bookworm and trixie) | PASS |
| AD-003 | `scripts/shutdown_grant_test.go` | `TestInstallSh_SS_ClassifierIsPortableAwk` | PASS |
| SEC-001 | `scripts/test_install_maintenance.py`, `scripts/shutdown_grant_test.go` | see the conditions table | PASS |
| SEC-002 | `internal/agent/*_test.go` | see the conditions table | PASS |
| SEC-003 | `scripts/shutdown_grant_test.go`, fixtures | see the conditions table | PASS |

Mutation spot checks (not committed): each of these changes to `install.sh` made its named fixture fail, which shows the tests have teeth.

- `GRANT_PENDING` never set: `test_SS_OQA2_failure_before_agreement_restores_previous_policy` failed.
- The late `agent.yaml` refusal deleted: all three `test_SS_SEC001_late_checks_*` cases failed.
- Both `require_shutdown_agreement` calls deleted: all three `test_SS_AC017_agreement_holds_*` cases failed.
- The link-count refusal made a no-op: both `test_SS_SEC001_hardlinked_config_refused_*` cases failed.
- The grant decision made to ignore the config: `test_SS_AC011_reinstall_fails_closed` failed on every non-enabled form.

### Test Run Output

#### Go: `go test ./... -count=1` (Windows host)

```
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-maintenance	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-server	[no test files]
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer	0.814s
ok  	github.com/DesyncTheThird/rIOt/internal/agent	22.941s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	11.672s
ok  	github.com/DesyncTheThird/rIOt/internal/logbudget	0.641s
ok  	github.com/DesyncTheThird/rIOt/internal/maintenance	1.251s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.907s
ok  	github.com/DesyncTheThird/rIOt/internal/redact	0.696s
ok  	github.com/DesyncTheThird/rIOt/internal/releasekey	0.757s
ok  	github.com/DesyncTheThird/rIOt/internal/resilient	1.357s
ok  	github.com/DesyncTheThird/rIOt/internal/server	3.325s
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	0.660s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	0.855s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	1.527s
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	1.280s
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	0.823s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	9.884s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ingest	0.318s
?   	github.com/DesyncTheThird/rIOt/internal/server/logstore	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	0.555s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	0.531s
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc	1.345s
ok  	github.com/DesyncTheThird/rIOt/internal/server/partition	0.290s
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	0.543s
ok  	github.com/DesyncTheThird/rIOt/internal/server/quota	0.240s
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	0.299s
ok  	github.com/DesyncTheThird/rIOt/internal/server/storage	0.454s
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary	0.378s
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	0.321s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	0.482s
?   	github.com/DesyncTheThird/rIOt/internal/testutil	[no test files]
ok  	github.com/DesyncTheThird/rIOt/scripts	15.213s
exit 0
```

#### Go: new and related tests only, verbose (Windows host)

```
--- PASS: TestPowerPaths_SD_C4_OnlyExecRebootAndExecShutdownLaunchPowerActions (0.08s)
--- PASS: TestPowerPaths_SD_C4_SelfTest (0.00s)
--- PASS: TestPowerPaths_SS_C4_ListingAllowanceSelfTest (0.01s)
--- PASS: TestShutdownCallSites_SD_AC020_ExecShutdownHasExactlyOneGuardedCallSite (0.02s)
--- PASS: TestShutdownCallSites_SD_AC020_GateClearanceFeedsExecShutdown (0.02s)
--- PASS: TestShutdownCallSites_SD_C3_SelfTest (0.00s)
--- PASS: TestShutdownDoctor_SS_AC019_PassNamesTheGrant (0.00s)
--- PASS: TestShutdownDoctor_SS_AC020_FailWithGuidance (0.05s)
--- PASS: TestShutdownDoctor_SS_AC020_TimeoutBounded (0.00s)
--- PASS: TestShutdownDoctor_SS_AC021_RootAsksAboutRiot (0.00s)
--- PASS: TestAgentPolicyRunner_SS_RewriteMatchesMaintenance (0.00s)
--- PASS: TestShutdownDoctor_SS_AC022_NoCheckWhenOffOrNotLinux (0.00s)
--- PASS: TestShutdownDoctor_SS_AC022_DoctorWiring (0.00s)
--- PASS: TestShutdownDoctor_SS_AC023_ProbeMatchesExecShutdownArgv (0.00s)
--- PASS: TestShutdownDoctor_SS_AC024_OnlyListingInvocations (0.02s)
--- PASS: TestShutdownDoctor_SS_C4_ListingCommandOnlyFeedsTheProbe (0.04s)
--- PASS: TestShutdownDoctor_SS_C4_ReferenceRuleSelfTest (0.00s)
--- PASS: TestShutdownDoctor_SS_AC029_DoctorWritesNothing (0.00s)
PASS
ok  	github.com/DesyncTheThird/rIOt/internal/agent	1.793s
--- PASS: TestInstallSh_SS_AC003_PromptOrder (0.00s)
--- PASS: TestInstallSh_SS_AC007_RebootLineUnchangedAndPoweroffLineExact (0.00s)
--- PASS: TestInstallSh_SS_AC008_NoOtherPowerGrant (0.00s)
--- PASS: TestInstallSh_SS_AC018_ShutdownIsLinuxOnly (0.00s)
--- PASS: TestInstallSh_SS_AC026_FlagsAndUsageUnchanged (0.00s)
--- PASS: TestInstallSh_SS_OQA1_PromptsPrecedeLocksAndStop (0.00s)
--- PASS: TestInstallSh_SS_ClassifierIsPortableAwk (0.00s)
--- PASS: TestInstallSh_SS_SEC003_RuntimeChecksFollowTheLockdownAndPrecedeAnyWrite (0.00s)
--- PASS: TestInstallSh_SS_SEC001_AgentYamlIsOnlyWrittenWhenAbsentAndLinksAreCounted (0.00s)
    shutdown_grant_test.go:601: [FR-017] classifier runs with: [default-awk]
--- PASS: TestInstallSh_SS_FR017_ClassifierNeverExceedsAgentLoader (11.79s)
PASS
ok  	github.com/DesyncTheThird/rIOt/scripts	12.394s
```

#### Go: `go test -race -count=1 ./internal/agent/... ./scripts` (Linux, `golang:1.24` container, unprivileged, read-only source and module cache)

```
ok  	github.com/DesyncTheThird/rIOt/internal/agent	126.591s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	79.163s
ok  	github.com/DesyncTheThird/rIOt/scripts	1.983s
exit 0
```

#### Go: classifier differential test inside the fixture images (default awk, mawk, busybox awk and gawk, bookworm then trixie)

```
== container riot-fixture:bookworm (default awk, mawk, busybox, gawk)
--- PASS: TestInstallSh_SS_AC003_PromptOrder (0.01s)
--- PASS: TestInstallSh_SS_AC007_RebootLineUnchangedAndPoweroffLineExact (0.00s)
--- PASS: TestInstallSh_SS_AC008_NoOtherPowerGrant (0.00s)
--- PASS: TestInstallSh_SS_AC018_ShutdownIsLinuxOnly (0.00s)
--- PASS: TestInstallSh_SS_AC026_FlagsAndUsageUnchanged (0.00s)
--- PASS: TestInstallSh_SS_OQA1_PromptsPrecedeLocksAndStop (0.00s)
--- PASS: TestInstallSh_SS_ClassifierIsPortableAwk (0.00s)
--- PASS: TestInstallSh_SS_SEC003_RuntimeChecksFollowTheLockdownAndPrecedeAnyWrite (0.00s)
--- PASS: TestInstallSh_SS_SEC001_AgentYamlIsOnlyWrittenWhenAbsentAndLinksAreCounted (0.00s)
    shutdown_grant_test.go:601: [FR-017] classifier runs with: [default-awk mawk busybox gawk]
--- PASS: TestInstallSh_SS_FR017_ClassifierNeverExceedsAgentLoader (1.42s)
PASS
== container riot-fixture:trixie (default awk, mawk, busybox, gawk)
--- PASS: TestInstallSh_SS_AC003_PromptOrder (0.01s)
--- PASS: TestInstallSh_SS_AC007_RebootLineUnchangedAndPoweroffLineExact (0.00s)
--- PASS: TestInstallSh_SS_AC008_NoOtherPowerGrant (0.00s)
--- PASS: TestInstallSh_SS_AC018_ShutdownIsLinuxOnly (0.00s)
--- PASS: TestInstallSh_SS_AC026_FlagsAndUsageUnchanged (0.00s)
--- PASS: TestInstallSh_SS_OQA1_PromptsPrecedeLocksAndStop (0.00s)
--- PASS: TestInstallSh_SS_ClassifierIsPortableAwk (0.00s)
--- PASS: TestInstallSh_SS_SEC003_RuntimeChecksFollowTheLockdownAndPrecedeAnyWrite (0.00s)
--- PASS: TestInstallSh_SS_SEC001_AgentYamlIsOnlyWrittenWhenAbsentAndLinksAreCounted (0.00s)
    shutdown_grant_test.go:601: [FR-017] classifier runs with: [default-awk mawk busybox gawk]
--- PASS: TestInstallSh_SS_FR017_ClassifierNeverExceedsAgentLoader (1.45s)
PASS
```

#### `go vet ./...` (Windows)

```
(no output)
exit status 0
```

#### `GOOS=linux go vet ./...`

```
(no output)
exit status 0
```

#### Installer fixtures, `debian:bookworm-slim`

Run exactly as the CI step "Signed installer and legacy migration fixtures" (same `bash -ec` body: `apt-get install` of `python3 openssl sudo systemd ca-certificates`, then `RIOT_INSTALLER_FIXTURE=1 python3 /src/scripts/test_install_maintenance.py -v`). The only difference is the mount source, an LF-only copy of the worktree `scripts/` directory at `D:\rIOt_fixture_src`, because the Windows worktree has CRLF line endings (`core.autocrlf=true`) and the installer cannot run with them. The copy was compared byte for byte with `git show HEAD:scripts/install.sh` and `HEAD:scripts/test_install_maintenance.py`.

```
test_AC011_changed_signed_asset_preserves_running_install (__main__.InstallerFixtures.test_AC011_changed_signed_asset_preserves_running_install) ... ok
test_AC011_invalid_signature_preserves_running_install (__main__.InstallerFixtures.test_AC011_invalid_signature_preserves_running_install) ... ok
test_AC011_missing_helper_preserves_running_install (__main__.InstallerFixtures.test_AC011_missing_helper_preserves_running_install) ... ok
test_AC012_active_worker_refuses_before_stop (__main__.InstallerFixtures.test_AC012_active_worker_refuses_before_stop) ... ok
test_AC012_candidate_policy_failure_preserves_old_policy (__main__.InstallerFixtures.test_AC012_candidate_policy_failure_preserves_old_policy) ... ok
test_AC012_duplicate_status_keys_refuse_before_stop (__main__.InstallerFixtures.test_AC012_duplicate_status_keys_refuse_before_stop) ... ok
test_AC012_effective_policy_failure_restores_old_policy_before_stop (__main__.InstallerFixtures.test_AC012_effective_policy_failure_restores_old_policy_before_stop) ... ok
test_AC012_invalid_signed_unit_preserves_binary_unit_policy (__main__.InstallerFixtures.test_AC012_invalid_signed_unit_preserves_binary_unit_policy) ... ok
test_AC012_legacy_migration_uses_fresh_signed_inode_and_preserves_identity (__main__.InstallerFixtures.test_AC012_legacy_migration_uses_fresh_signed_inode_and_preserves_identity) ... ok
test_AC012_multilink_root_lock_refuses_before_stop (__main__.InstallerFixtures.test_AC012_multilink_root_lock_refuses_before_stop) ... ok
test_AC012_native_dpkg_lock_refuses_before_stop (__main__.InstallerFixtures.test_AC012_native_dpkg_lock_refuses_before_stop) ... ok
test_AC012_old_sudo_preserves_binary_unit_policy_before_mutation (__main__.InstallerFixtures.test_AC012_old_sudo_preserves_binary_unit_policy_before_mutation) ... ok
test_AC012_real_sudo_lists_safe_hold_names_and_denies_injection (__main__.InstallerFixtures.test_AC012_real_sudo_lists_safe_hold_names_and_denies_injection) ... ok
test_AC012_root_lock_contention_preserves_running_install (__main__.InstallerFixtures.test_AC012_root_lock_contention_preserves_running_install) ... ok
test_AC012_symlink_helper_directory_refuses_before_stop (__main__.InstallerFixtures.test_AC012_symlink_helper_directory_refuses_before_stop) ... ok
test_AC012_unfinished_root_record_refuses_before_stop (__main__.InstallerFixtures.test_AC012_unfinished_root_record_refuses_before_stop) ... ok
test_DBUS001_AC005_present_system_bus_needs_no_package_operation (__main__.InstallerFixtures.test_DBUS001_AC005_present_system_bus_needs_no_package_operation) ... ok
test_DBUS001_AC006_missing_system_bus_is_installed_and_started_before_stop (__main__.InstallerFixtures.test_DBUS001_AC006_missing_system_bus_is_installed_and_started_before_stop) ... ok
test_DBUS001_AC007_failed_dbus_install_preserves_running_install (__main__.InstallerFixtures.test_DBUS001_AC007_failed_dbus_install_preserves_running_install) ... ok
test_SS_A003_bare_systemctl_resolves_to_granted_path (__main__.InstallerFixtures.test_SS_A003_bare_systemctl_resolves_to_granted_path) ... ok
test_SS_AC001_fresh_yes_grants_poweroff_and_config_agrees (__main__.InstallerFixtures.test_SS_AC001_fresh_yes_grants_poweroff_and_config_agrees) ... ok
test_SS_AC002_fresh_no_or_unrecognised_answer_grants_nothing (__main__.InstallerFixtures.test_SS_AC002_fresh_no_or_unrecognised_answer_grants_nothing) ... ok
test_SS_AC003_prompt_text_and_order (__main__.InstallerFixtures.test_SS_AC003_prompt_text_and_order) ... ok
test_SS_AC004_reboot_and_shutdown_independent (__main__.InstallerFixtures.test_SS_AC004_reboot_and_shutdown_independent) ... ok
test_SS_AC004_template_helper_matches_pre_story_golden (__main__.InstallerFixtures.test_SS_AC004_template_helper_matches_pre_story_golden) ... ok
test_SS_AC005_yes_flag_answers_yes (__main__.InstallerFixtures.test_SS_AC005_yes_flag_answers_yes) ... ok
test_SS_AC006_non_interactive_and_no_terminal_answer_no (__main__.InstallerFixtures.test_SS_AC006_non_interactive_and_no_terminal_answer_no) ... ok
test_SS_AC007_existing_lines_byte_identical (__main__.InstallerFixtures.test_SS_AC007_existing_lines_byte_identical) ... ok
test_SS_AC008_only_exact_poweroff_is_granted (__main__.InstallerFixtures.test_SS_AC008_only_exact_poweroff_is_granted) ... ok
test_SS_AC009_policy_validation_still_guards_yes_install (__main__.InstallerFixtures.test_SS_AC009_policy_validation_still_guards_yes_install) ... ok
test_SS_AC010_reinstall_true_emits_grant_without_question (__main__.InstallerFixtures.test_SS_AC010_reinstall_true_emits_grant_without_question) ... ok
test_SS_AC011_reinstall_fails_closed (__main__.InstallerFixtures.test_SS_AC011_reinstall_fails_closed) ... ok
test_SS_AC012_flags_do_not_override_config (__main__.InstallerFixtures.test_SS_AC012_flags_do_not_override_config) ... ok
test_SS_AC013_reinstall_revokes_stale_grant (__main__.InstallerFixtures.test_SS_AC013_reinstall_revokes_stale_grant) ... ok
test_SS_AC014_preserved_config_untouched (__main__.InstallerFixtures.test_SS_AC014_preserved_config_untouched) ... ok
test_SS_AC015_bootstrap_reenrolment_resets_shutdown (__main__.InstallerFixtures.test_SS_AC015_bootstrap_reenrolment_resets_shutdown) ... ok
test_SS_AC016_bootstrap_on_fresh_install_asks_and_grants (__main__.InstallerFixtures.test_SS_AC016_bootstrap_on_fresh_install_asks_and_grants) ... ok
test_SS_AC017_agreement_holds_when_the_config_changes_under_the_run (__main__.InstallerFixtures.test_SS_AC017_agreement_holds_when_the_config_changes_under_the_run) ... ok
test_SS_AC018_stubbed_darwin_install_unchanged (__main__.InstallerFixtures.test_SS_AC018_stubbed_darwin_install_unchanged) ... ok
test_SS_AC029_bounded_growth (__main__.InstallerFixtures.test_SS_AC029_bounded_growth) ... ok
test_SS_OQA2_failure_before_agreement_restores_previous_policy (__main__.InstallerFixtures.test_SS_OQA2_failure_before_agreement_restores_previous_policy) ... ok
test_SS_OQA2_failure_with_no_previous_dropin_leaves_none (__main__.InstallerFixtures.test_SS_OQA2_failure_with_no_previous_dropin_leaves_none) ... ok
test_SS_SEC001_hardlinked_config_refused_before_any_change (__main__.InstallerFixtures.test_SS_SEC001_hardlinked_config_refused_before_any_change) ... ok
test_SS_SEC001_late_checks_run_after_the_stop_and_roll_back_the_grant (__main__.InstallerFixtures.test_SS_SEC001_late_checks_run_after_the_stop_and_roll_back_the_grant) ... ok

----------------------------------------------------------------------
Ran 44 tests in 24.386s

OK
```

#### Installer fixtures, `debian:trixie-slim`

```
test_AC011_changed_signed_asset_preserves_running_install (__main__.InstallerFixtures.test_AC011_changed_signed_asset_preserves_running_install) ... ok
test_AC011_invalid_signature_preserves_running_install (__main__.InstallerFixtures.test_AC011_invalid_signature_preserves_running_install) ... ok
test_AC011_missing_helper_preserves_running_install (__main__.InstallerFixtures.test_AC011_missing_helper_preserves_running_install) ... ok
test_AC012_active_worker_refuses_before_stop (__main__.InstallerFixtures.test_AC012_active_worker_refuses_before_stop) ... ok
test_AC012_candidate_policy_failure_preserves_old_policy (__main__.InstallerFixtures.test_AC012_candidate_policy_failure_preserves_old_policy) ... ok
test_AC012_duplicate_status_keys_refuse_before_stop (__main__.InstallerFixtures.test_AC012_duplicate_status_keys_refuse_before_stop) ... ok
test_AC012_effective_policy_failure_restores_old_policy_before_stop (__main__.InstallerFixtures.test_AC012_effective_policy_failure_restores_old_policy_before_stop) ... ok
test_AC012_invalid_signed_unit_preserves_binary_unit_policy (__main__.InstallerFixtures.test_AC012_invalid_signed_unit_preserves_binary_unit_policy) ... ok
test_AC012_legacy_migration_uses_fresh_signed_inode_and_preserves_identity (__main__.InstallerFixtures.test_AC012_legacy_migration_uses_fresh_signed_inode_and_preserves_identity) ... ok
test_AC012_multilink_root_lock_refuses_before_stop (__main__.InstallerFixtures.test_AC012_multilink_root_lock_refuses_before_stop) ... ok
test_AC012_native_dpkg_lock_refuses_before_stop (__main__.InstallerFixtures.test_AC012_native_dpkg_lock_refuses_before_stop) ... ok
test_AC012_old_sudo_preserves_binary_unit_policy_before_mutation (__main__.InstallerFixtures.test_AC012_old_sudo_preserves_binary_unit_policy_before_mutation) ... ok
test_AC012_real_sudo_lists_safe_hold_names_and_denies_injection (__main__.InstallerFixtures.test_AC012_real_sudo_lists_safe_hold_names_and_denies_injection) ... ok
test_AC012_root_lock_contention_preserves_running_install (__main__.InstallerFixtures.test_AC012_root_lock_contention_preserves_running_install) ... ok
test_AC012_symlink_helper_directory_refuses_before_stop (__main__.InstallerFixtures.test_AC012_symlink_helper_directory_refuses_before_stop) ... ok
test_AC012_unfinished_root_record_refuses_before_stop (__main__.InstallerFixtures.test_AC012_unfinished_root_record_refuses_before_stop) ... ok
test_DBUS001_AC005_present_system_bus_needs_no_package_operation (__main__.InstallerFixtures.test_DBUS001_AC005_present_system_bus_needs_no_package_operation) ... ok
test_DBUS001_AC006_missing_system_bus_is_installed_and_started_before_stop (__main__.InstallerFixtures.test_DBUS001_AC006_missing_system_bus_is_installed_and_started_before_stop) ... ok
test_DBUS001_AC007_failed_dbus_install_preserves_running_install (__main__.InstallerFixtures.test_DBUS001_AC007_failed_dbus_install_preserves_running_install) ... ok
test_SS_A003_bare_systemctl_resolves_to_granted_path (__main__.InstallerFixtures.test_SS_A003_bare_systemctl_resolves_to_granted_path) ... ok
test_SS_AC001_fresh_yes_grants_poweroff_and_config_agrees (__main__.InstallerFixtures.test_SS_AC001_fresh_yes_grants_poweroff_and_config_agrees) ... ok
test_SS_AC002_fresh_no_or_unrecognised_answer_grants_nothing (__main__.InstallerFixtures.test_SS_AC002_fresh_no_or_unrecognised_answer_grants_nothing) ... ok
test_SS_AC003_prompt_text_and_order (__main__.InstallerFixtures.test_SS_AC003_prompt_text_and_order) ... ok
test_SS_AC004_reboot_and_shutdown_independent (__main__.InstallerFixtures.test_SS_AC004_reboot_and_shutdown_independent) ... ok
test_SS_AC004_template_helper_matches_pre_story_golden (__main__.InstallerFixtures.test_SS_AC004_template_helper_matches_pre_story_golden) ... ok
test_SS_AC005_yes_flag_answers_yes (__main__.InstallerFixtures.test_SS_AC005_yes_flag_answers_yes) ... ok
test_SS_AC006_non_interactive_and_no_terminal_answer_no (__main__.InstallerFixtures.test_SS_AC006_non_interactive_and_no_terminal_answer_no) ... ok
test_SS_AC007_existing_lines_byte_identical (__main__.InstallerFixtures.test_SS_AC007_existing_lines_byte_identical) ... ok
test_SS_AC008_only_exact_poweroff_is_granted (__main__.InstallerFixtures.test_SS_AC008_only_exact_poweroff_is_granted) ... ok
test_SS_AC009_policy_validation_still_guards_yes_install (__main__.InstallerFixtures.test_SS_AC009_policy_validation_still_guards_yes_install) ... ok
test_SS_AC010_reinstall_true_emits_grant_without_question (__main__.InstallerFixtures.test_SS_AC010_reinstall_true_emits_grant_without_question) ... ok
test_SS_AC011_reinstall_fails_closed (__main__.InstallerFixtures.test_SS_AC011_reinstall_fails_closed) ... ok
test_SS_AC012_flags_do_not_override_config (__main__.InstallerFixtures.test_SS_AC012_flags_do_not_override_config) ... ok
test_SS_AC013_reinstall_revokes_stale_grant (__main__.InstallerFixtures.test_SS_AC013_reinstall_revokes_stale_grant) ... ok
test_SS_AC014_preserved_config_untouched (__main__.InstallerFixtures.test_SS_AC014_preserved_config_untouched) ... ok
test_SS_AC015_bootstrap_reenrolment_resets_shutdown (__main__.InstallerFixtures.test_SS_AC015_bootstrap_reenrolment_resets_shutdown) ... ok
test_SS_AC016_bootstrap_on_fresh_install_asks_and_grants (__main__.InstallerFixtures.test_SS_AC016_bootstrap_on_fresh_install_asks_and_grants) ... ok
test_SS_AC017_agreement_holds_when_the_config_changes_under_the_run (__main__.InstallerFixtures.test_SS_AC017_agreement_holds_when_the_config_changes_under_the_run) ... ok
test_SS_AC018_stubbed_darwin_install_unchanged (__main__.InstallerFixtures.test_SS_AC018_stubbed_darwin_install_unchanged) ... ok
test_SS_AC029_bounded_growth (__main__.InstallerFixtures.test_SS_AC029_bounded_growth) ... ok
test_SS_OQA2_failure_before_agreement_restores_previous_policy (__main__.InstallerFixtures.test_SS_OQA2_failure_before_agreement_restores_previous_policy) ... ok
test_SS_OQA2_failure_with_no_previous_dropin_leaves_none (__main__.InstallerFixtures.test_SS_OQA2_failure_with_no_previous_dropin_leaves_none) ... ok
test_SS_SEC001_hardlinked_config_refused_before_any_change (__main__.InstallerFixtures.test_SS_SEC001_hardlinked_config_refused_before_any_change) ... ok
test_SS_SEC001_late_checks_run_after_the_stop_and_roll_back_the_grant (__main__.InstallerFixtures.test_SS_SEC001_late_checks_run_after_the_stop_and_roll_back_the_grant) ... ok

----------------------------------------------------------------------
Ran 44 tests in 23.514s

OK
```

#### Web suite (`cd web && npm run test:run`, unaffected)

`npm ci` was needed first, because the worktree had no `node_modules` (git-ignored; cache redirected to `D:`).

```
> vitest run
Test Files  95 passed (95)
Tests  1707 passed (1707)
Duration  13.49s (transform 11.38s, setup 20.55s, import 25.47s, tests 64.02s, environment 52.45s)
exit 0
```

#### Shell lint

`bash -n scripts/install.sh` is clean. `shellcheck` 0.9.0 (Debian bookworm package, in a container) reports identical findings for the pre-story and the new installer: 6 x SC2015 (note), 1 x SC2034 (warning, `SUPPLEMENTARY_GROUPS`, pre-existing), 1 x SC2001 (note). No new finding. `golangci-lint` is not installed here; the CI lint job is `go vet ./...`, which is clean.

## Deviations from ADD

These trigger architect review.

- **D-1. README and CHANGELOG are not changed, and `TestDocs_SS_AC027` / `TestDocs_SS_AC028` are not written.** The ADD lists both files in section 4 as the technical writer's work, and CLAUDE.md places the technical writer after QA. The two doc tests read the README and CHANGELOG, so they would fail until that text exists, and failing tests cannot be committed. The technical writer needs to add the text (FR-036 to FR-039) and the two tests in `scripts/shutdown_grant_test.go`, which already has the helpers. AC-027 and AC-028 are therefore **not covered** at this point.
- **D-2. Two agreement re-reads instead of one (SEC-003 against AD-005).** AD-005 puts the agreement check after the config write. The security review requires it after the lockdown and before any `agent.yaml` write or bootstrap `rm`. The code does both. `require_shutdown_agreement` runs right after the late refusal when the run preserves a config (reinstall without a bootstrap key), and again after the write block as the ADD says. A mismatch in either restores the previous policy and fails. On success only the second clears `GRANT_PENDING`.
- **D-3. Stricter SEC-001 option, as the task required.** The ADD only has AD-007's non-regular-file refusal. A link count above one is now refused at the same two points. The late message does not claim "previous managed policy restored", because that is true only when a grant was pending and the trap does it.
- **D-4. Fixture additions beyond the ADD list.**
  - A `/fixture/on-stop` hook in the `systemctl` stub, to change `agent.yaml` at the moment the agent is being stopped. It is needed to test the late refusal and the agreement check.
  - `poweroff`, `halt`, `shutdown` and `reboot` in `/usr/sbin` are replaced by tripwires, so `sudo -l` can prove a refusal for an existing path and so any call is caught.
  - Extra tests: `test_SS_AC017_agreement_holds_*`, `test_SS_OQA2_failure_with_no_previous_dropin_leaves_none`, the SEC-001 pair, and `PRE_STORY_DARWIN_STDOUT` (the non-Linux output is compared, not only the config).
  - `TestShutdownDoctor_SS_AC022_DoctorWiring` and `TestShutdownDoctor_SS_C4_ReferenceRuleSelfTest`.
- **D-5. Classifier details beyond the ADD wording** (all reject-more, none widen):
  - any carriage return rejects the file;
  - a quoted value must close on its own line, with only a comment or a `:` after it;
  - a sequence item's remainder is checked as well as the value after its first `: `;
  - `---` handling uses a `seen` count of content lines.
  - The differential corpus has 58 cases, and `gawk` was added to the awk variants.
  - On the Windows host the CRLF case is pinned only on Linux, because Git Bash's awk reads files in text mode and drops the carriage return. The "classifier enabled implies agent enabled" check runs everywhere. The Linux container runs pin CRLF as not enabled.
- **D-6. Exploit-grade detail** about SEC-OBS-1 and SEC-001 is kept out of this report and the repo, as instructed.
- **D-7. Commit trailer.** The trailer says `Claude Opus 5.5`, as instructed. The executing model for this implementation was Sonnet 5.5.
- **D-8. No ARCH-QUESTION blockers.** No blockers file was needed.

Nothing in `execShutdown`, the C-3 call-site checks, the server, the API or the dashboard changed.

## Not run, or not run on a real host

- **No fleet host, no real `riot-agent`, no real doctor run** (SC-001 to SC-003). Doctor is covered with injected runners only. Ryan runs `riot-agent doctor` on hawaii, vermont, PiVPN and PiCUPS after re-running the installer. A-003 (bare `systemctl` resolving to the granted path) is proven in the bookworm and trixie fixtures, not on the fleet's own images.
- **DNF/Fedora path of the installer** was not exercised (no such container fixture exists). The DNF sudoers lines are byte-identical, the poweroff echo sits outside the `dnf` branch, and the classifier runs on four awks.
- **GitHub Actions** was not run. The workflow YAML was parsed and the step's shell body was run locally.
- **`golangci-lint`** is not installed; see Shell lint.
- No test ever ran, and no fixture could reach, a real `shutdown`, `poweroff`, `halt` or `reboot`. In the container `systemctl` is a stub and the power tools are tripwires. Every `tearDown` checks the trace for a power verb.

## Notes for QA

- **Run the fixtures only in a disposable root container** (the `setUpClass` guard enforces it). The Windows worktree is CRLF; mount an LF copy of `scripts/` (or use a Linux checkout, as CI does). The shellcheck and classifier container runs used the same copy.
- **Probe these by hand if time allows:**
  - `config_enables_shutdown` against odd but valid YAML, for example anchors, `yes`, quoted `true`. All must count as not enabled, and the AD-008 "not enabled" line must explain why.
  - A fresh install interrupted with Ctrl-C at a prompt must leave the host untouched, now that the prompts run first.
  - `kill -TERM` of the installer between the drop-in install and the agreement check must put the previous drop-in back (the trap).
  - The two re-reads (D-2) are redundant on purpose.
- **Timing:** the prompts now appear before prerequisite failures such as an old sudo or a busy maintenance service. A user can answer and then see a refusal, with no change made (ADD AD-001 consequence).
- **Residuals unchanged from the ADD:** R-1 to R-5. R-2 (SIGKILL or power loss between the drop-in install and the agreement check) still needs a re-run to reconcile.
- **Leftovers outside the repo, mine:** Docker images `riot-fixture:bookworm` and `riot-fixture:trixie`, the directory `D:\rIOt_fixture_src`, `D:\rIOt_npm_cache`, and `web/node_modules` (git-ignored). None of them is needed by the repo.
