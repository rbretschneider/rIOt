# QA Report

**Story ID:** SHUTDOWN-SUDO-001
**QA Engineer:** QA Agent
**Date:** 2026-10-26
**Branch / commit reviewed:** `feat/shutdown-sudo-001` at `e187a3c` (QA test commits `00f010a` and `02d9acf` sit on top)
**Verdict (revision 2, 2026-10-26, after the QA-1 fix):** PASS WITH NOTES
**Verdict (revision 1, `e187a3c`):** FAIL, superseded. Revision 1 text below is kept as the record of what was found.

## Revision 2: QA-1 re-review

**Reviewed:** `feat/shutdown-sudo-001` at `91c0f8c`, which adds `39aba1a` (the fix) and `91c0f8c` (impl-report addendum) on top of the revision 1 commits.

**The fix.** `39aba1a` changes `scripts/install.sh` by exactly one line, `if (seen == 0 && ind > 0) reject()`, directly after the tab check, as suggested. It adds 7 cases to `classifierCorpus` in `scripts/shutdown_grant_test.go`: six "not enabled" cases for an indented first line (plain, one space, after a blank line, after a comment, after a `---` marker, orphaned indented `url`) and one "enabled" control (a leading comment and blank line before a column-0 `commands:`, which must still be accepted). Nothing else in the installer or the agent changed.

**QA tests untouched.** `git diff 02d9acf HEAD` for `scripts/shutdown_grant_qa_test.go` and `scripts/test_install_maintenance.py` is empty.

**Re-run on a fresh LF export** (`git -c core.autocrlf=false -c core.eol=lf archive HEAD` into the new directory `D:\rIOt_qa_rev2_repo`; `install.sh`, the fixture file and the QA Go file compared byte-identical with `git show HEAD:...`, 0 carriage returns):

| Run | Result |
|---|---|
| `go test ./... -count=1` (Windows, `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`) | 29 packages `ok`, 0 failing, `exit 0`. Includes `internal/agent` 23.9 s and `scripts` 54.8 s, with both QA Go tests now green |
| `go vet ./...` | no output, `exit 0` |
| `GOOS=linux go vet ./...` | no output, `exit 0` |
| QA mutation test, `golang:1.24` container, unprivileged, read-only mounts, `QA_FUZZ_N=1000`, default awk and mawk | `PASS`. Both awks: 1000 mutants, agent reads true 218, agent cannot load 649, classifier enables 178, none of them read false by the agent (64 are accepted residual R-1) |
| Same, `QA_FUZZ_N=3000` | `PASS`. 3000 mutants, classifier enables 573 (169 residual R-1), zero cases of agent-reads-false |
| QA minimal repro `TestInstallSh_QA_FR017_IndentedFirstLineHidesCommandsFromAgent` | `PASS` (was red) |
| Story differential test `TestInstallSh_SS_FR017_ClassifierNeverExceedsAgentLoader`, default awk and mawk, now including the 7 new cases | `PASS` |
| Installer fixture `debian:bookworm-slim`, as CI runs it, LF export mounted read-only | `Ran 49 tests ... OK` (44 story/legacy plus 5 QA) |
| Installer fixture `debian:trixie-slim`, same | `Ran 49 tests ... OK` |

I did not re-run `npm run test:run` or the `-race` run: the web sources and the agent Go sources are unchanged since revision 1 (the diff since `e187a3c` touches only `install.sh`, test files and docs).

**Result.** QA-1 is fixed and the previously red tests are green. AC-011 moves from PARTIAL to COVERED. No new finding. Verdict is PASS WITH NOTES, not PASS, because:

1. AC-027 and AC-028 are pending the technical writer (README and CHANGELOG, FR-036 to FR-039). Accepted, not failed.
2. `busybox awk` and `gawk` were not available in the golang image, so my mutation runs used default awk and mawk only (the implementer's differential runs cover all four).
3. The DNF/Fedora path has no fixture, fleet images were not tested (A-003 proven on stock bookworm and trixie only), and GitHub Actions itself was not run.

The sections below are the revision 1 text. Where they say QA-1 is open or the Go tests are red, that is superseded by this section.

---

## Revision 1 (superseded)

One defect blocks sign-off: the installer's config reader can report "shutdown enabled" for a file the agent reads as false. That breaks FR-017 ("must never treat a config as shutdown-enabled when the agent would read `commands.allow_shutdown` as false"). It does not give an attacker anything new (see Finding QA-1), and the fix is one awk line plus a corpus case. Everything else passes: all story tests are green on both Debian images, all four security conditions are met, the sudoers drop-in differs from main only by the one conditional line, and `commands.go` is identical to main.

---

## Test Run Summary

All commands ran with `GOCACHE=D:\rIOt_gocache` and `GOTMPDIR=D:\rIOt_fuzz_tmp`. Windows host unless stated.

| Run | Result |
|---|---|
| `go test ./... -count=1` at `e187a3c` (before QA tests) | All packages `ok` (29 packages with tests, `exit 0`). `internal/agent` 23.6 s, `scripts` 15.8 s |
| `go vet ./...` | no output, `exit 0` |
| `GOOS=linux go vet ./...` | no output, `exit 0` |
| `cd web && npm run test:run` (`npm_config_cache=D:\rIOt_npm_cache`) | 95 files, 1707 tests passed, `exit 0` |
| Installer fixture, `debian:bookworm-slim`, as CI runs it, LF export of `HEAD` mounted read-only | `Ran 44 tests ... OK` (44 `ok`, 0 skipped) |
| Installer fixture, `debian:trixie-slim`, same | `Ran 44 tests ... OK` |
| Same fixture plus the 5 QA fixtures, bookworm | `Ran 49 tests ... OK` |
| Same fixture plus the 5 QA fixtures, trixie | `Ran 49 tests ... OK` |
| Fixture with `GRANT_PENDING` mutated to never be set (disposable copy only) | 6 failures, including the new QA SIGTERM fixture, so the fixtures have teeth |
| `go test -count=1 -v -run 'QA_FR017\|FR017' ./scripts` in `golang:1.24` (default awk and mawk), with QA tests | story differential test `PASS` (58 cases); QA mutation test `FAIL`; QA minimal repro `FAIL` (see QA-1) |
| `go test -race -count=1 ./internal/agent/...` in `golang:1.24` (unprivileged, read-only mounts) | `ok` for `internal/agent` (114.6 s) and `internal/agent/collectors` (80.3 s), `exit 0`. `./scripts` was not run with `-race` (its Linux run is in the rows above, without `-race`) |

With the two QA Go tests added, `go test ./scripts` is red on purpose until QA-1 is fixed. No other test fails.

- Flaky tests: none seen. Fixture and Go runs were repeated and gave the same result.
- The bookworm fixture log has no suspicious output. The installer's expected failure messages appear only in tests that assert them.
- Worktree note: the checkout is CRLF (`core.autocrlf=true`). `git archive` also emits CRLF there, so the LF export for Docker used `git -c core.autocrlf=false -c core.eol=lf archive HEAD scripts`. Compared with `git show HEAD:...`, `install.sh` and `test_install_maintenance.py` are byte-identical.

## AC Coverage Audit

"Would the test fail if the AC were broken?" was checked by reading each test, by the implementer's five mutation checks, and by one mutation of my own (`GRANT_PENDING`).

| AC ID | Status | Tests covering it | Gap |
|---|---|---|---|
| AC-001 | COVERED | `test_SS_AC001_fresh_yes_grants_poweroff_and_config_agrees` (config bytes equal the expected template, one grant line, `sudo -n -U riot -l` listing allows it) | none |
| AC-002 | COVERED | `test_SS_AC002_fresh_no_or_unrecognised_answer_grants_nothing` (empty, `n`, `no`, `maybe`, unreadable terminal) | none |
| AC-003 | COVERED | `test_SS_AC003_prompt_text_and_order` (pty, recorded prompt sequence, `[y/N]` hints, Docker stub), `TestInstallSh_SS_AC003_PromptOrder` | none |
| AC-004 | COVERED | `test_SS_AC004_reboot_and_shutdown_independent` (4 combinations, config bytes equal pre-story layout), `..._template_helper_matches_pre_story_golden` | none |
| AC-005 | COVERED | `test_SS_AC005_yes_flag_answers_yes` (exact `y (auto)` line), QA `test_QA_fresh_yes_with_docker_key_and_pin_keeps_agreement` | none |
| AC-006 | COVERED | `test_SS_AC006_non_interactive_and_no_terminal_answer_no` (`/dev/tty` moved aside for the no-terminal case) | none |
| AC-007 | COVERED | `test_SS_AC007_existing_lines_byte_identical` (golden from `71a4dea`), `TestInstallSh_SS_AC007_...` (pins every sudoers echo, in order) | none |
| AC-008 | COVERED | `test_SS_AC008_only_exact_poweroff_is_granted` (9 refused variants checked by real `sudo -l` as `riot`, with the paths proved to exist), `TestInstallSh_SS_AC008_NoOtherPowerGrant` | none |
| AC-009 | COVERED | `test_SS_AC009_policy_validation_still_guards_yes_install` (candidate and effective failures, no second restore) | only the fresh `-y` path; the reinstall path runs the same code |
| AC-010 | COVERED | `test_SS_AC010_reinstall_true_emits_grant_without_question` (pty, no URL and `-y`), QA `test_QA_second_run_after_a_yes_install_is_stable` | none |
| AC-011 | PARTIAL (revision 1; COVERED in revision 2) | `test_SS_AC011_reinstall_fails_closed` (a to f, g1 to g4, `True`, `yes`, unparseable, h1 to h3), `TestInstallSh_SS_FR017_ClassifierNeverExceedsAgentLoader`, QA FIFO fixture | A config whose first content line is indented is read as enabled while the agent reads false. Not in the corpus. See QA-1. Test written, currently red |
| AC-012 | COVERED | `test_SS_AC012_flags_do_not_override_config` | none |
| AC-013 | COVERED | `test_SS_AC013_reinstall_revokes_stale_grant` (drop-in equals the pre-story bytes afterwards) | none |
| AC-014 | COVERED | `test_SS_AC014_preserved_config_untouched` (bytes, owner, mode, odd formatting) | none |
| AC-015 | COVERED | `test_SS_AC015_bootstrap_reenrolment_resets_shutdown` (pty and `-y`, exact re-enrolment line) | none |
| AC-016 | COVERED | `test_SS_AC016_bootstrap_on_fresh_install_asks_and_grants` | none |
| AC-017 | COVERED | `assert_agreement()` after every successful `test_SS_*` run (an independent Python reader), `test_SS_AC017_agreement_holds_when_the_config_changes_under_the_run` (3 cases) | The agreement is measured by the installer's own reader and a second reader written for the shapes the tests use, so it cannot catch QA-1 |
| AC-018 | COVERED | `test_SS_AC018_stubbed_darwin_install_unchanged` (output and config equal pre-story bytes), `TestInstallSh_SS_AC018_ShutdownIsLinuxOnly` | none |
| AC-019 | COVERED | `TestShutdownDoctor_SS_AC019_PassNamesTheGrant` | none |
| AC-020 | COVERED | `TestShutdownDoctor_SS_AC020_FailWithGuidance` (4 subtests), `..._TimeoutBounded` | none |
| AC-021 | COVERED | `TestShutdownDoctor_SS_AC021_RootAsksAboutRiot`, `TestAgentPolicyRunner_SS_RewriteMatchesMaintenance` (8 cases, slice not mutated) | none |
| AC-022 | COVERED | `TestShutdownDoctor_SS_AC022_NoCheckWhenOffOrNotLinux`, `..._DoctorWiring` (AST: one call, real runtime values; `Shutdown:` line text pinned) | none |
| AC-023 | COVERED | `TestShutdownDoctor_SS_AC023_ProbeMatchesExecShutdownArgv` (AST of `execShutdown`, both launch sites) | none |
| AC-024 | COVERED | `TestShutdownDoctor_SS_AC024_OnlyListingInvocations`, `..._C4_ListingCommandOnlyFeedsTheProbe`, `TestPowerPaths_SS_C4_ListingAllowanceSelfTest`, fixture `tearDown` tripwire | none |
| AC-025 | COVERED | existing `TestShutdownCallSites_SD_*`, `TestPowerPaths_SD_C4_*`, clearance and QA tests pass unchanged; `git diff main -- commands.go reboot_callsites_test.go shutdown_clearance_test.go shutdown_qa_test.go` is empty | none |
| AC-026 | COVERED | `TestInstallSh_SS_AC026_FlagsAndUsageUnchanged` (usage, flag parsing, URL check pinned to `71a4dea`); `git diff --stat main -- internal/server web cmd go.mod go.sum` is empty | none |
| AC-027 | PENDING TECHNICAL WRITER | `TestDocs_SS_AC027_*` not written (accepted deviation D-1) | README text not yet written |
| AC-028 | PENDING TECHNICAL WRITER | `TestDocs_SS_AC028_*` not written (accepted deviation D-1) | README and CHANGELOG text not yet written |
| AC-029 | COVERED | `test_SS_AC029_bounded_growth` (path sets, backup file lists, drop-in line delta of exactly 1, `DOWNLOAD` trace), `TestShutdownDoctor_SS_AC029_DoctorWritesNothing` (import allow-list) | none |

AC-027 and AC-028 are not counted as failures, as agreed.

## Security Conditions

| Condition | Result | Evidence |
|---|---|---|
| SEC-001 (link count and non-regular refusal, early and late) | MET | `refuse_unsafe_config_path` runs at `install.sh` L405 (before any change) and L789 (after the `/etc/riot` `root:root 0700` lockdown). It refuses non-regular files and `stat -c %h` above 1. Every `chown`, `chmod` and `cat >` of `agent.yaml` (L897, L899, L866) is inside the `[ ! -f ]` branch; `TestInstallSh_SS_SEC001_...` scans for this. Fixtures cover the hard link (plain and bootstrap), and a hard link, dangling symlink and directory appearing during the stop. My FIFO fixture (`test_QA_fifo_agent_yaml_is_refused_without_hanging`) also passes |
| SEC-002 (AD-011 additive, reference rule) | MET | `git diff main -- internal/agent/power_paths_test.go` has 0 removed lines, so both `TestPowerPaths_SD_C4_*` functions are byte-identical. `powerListingFuncs` is limited to package `agent`, dir `.`, with a shape check. `TestShutdownDoctor_SS_C4_ListingCommandOnlyFeedsTheProbe` pins `shutdownSudoCommand` to 2 references and `shutdownProbeArgs` to 1, and the probe only as `run(ctx, "sudo", shutdownProbeArgs()...)`; its self-test has 12 violations. Minor: the shape check accepts any second string, not only the poweroff verb; the reference rule and AC-023 bound it |
| SEC-003 (ordering, `GRANT_PENDING`, trap order) | MET | Source order: lockdown L785 to L786, late refusal L789, pre-write agreement L791, bootstrap `rm` L857, config write L866, final agreement L921, first binary L923. `GRANT_PENDING` is set at L765, directly after the drop-in install, and cleared before each of the four `restore_policy` sites and on agreement success. `cleanup` restores policy, then kills the lock helper, then removes `$STAGE`. Static test plus fixtures. My added SIGTERM fixture shows the trap restores the previous policy when TERM arrives in the window, and fails when `GRANT_PENDING` is mutated |
| SEC-004 (AD-008 transparency line) | MET | One line on every Linux reinstall (granting, not enabled, re-enrolment), none on a fresh install. Fixtures compare the exact text. The "not enabled" text says only a plain `allow_shutdown: true` under `commands:` in a regular file counts |

Also checked:

- Existing sudoers lines: in `git diff main...HEAD -- scripts/install.sh` the only change inside the drop-in block is the added `if [ "$SHUTDOWN_GRANT" = true ]; then echo "riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} poweroff"; fi` after the reboot echo. The "no" drop-in equals the pre-story golden and the "yes" drop-in minus that line equals the "no" drop-in (fixture AC-007).
- `internal/agent/commands.go`, `reboot_callsites_test.go`, `shutdown_clearance_test.go`, `shutdown_qa_test.go`, `internal/server`, `web`, `cmd`, `go.mod` and `go.sum`: no diff against `main`.
- SC-001 to SC-005: no real power command was run by me. The fixture replaces `poweroff`, `halt`, `shutdown`, `reboot` with tripwires inside the disposable container, and `tearDown` fails on any power verb. Policy was only listed. No `riot-agent` was run, no SSH, and the installer ran only in disposable containers.

## Test Quality Findings

- Behaviour over implementation: the fixtures assert files, policy listings and output. The Go static tests pin installer source text, which is brittle by design (guard rails for a privileged script); they are labelled as such and each has a behavioural twin in the fixtures.
- Isolation: fixture tests reset state in `setUp`; they do not depend on order. One thing to know: backup directories under `/var/backups` accumulate across tests, so a test that checks "no backup exists" must compare with a before-set (my Ctrl-C fixture does).
- Boundaries: prompt answers cover empty, `n`, `no`, `maybe` and an unreadable terminal. Classifier boundaries cover 65536 and 65537 bytes, NUL, CRLF, BOM, tabs and multi-document files.
- Mocking: the doctor tests use an injected runner and also an AST test against the real `execShutdown`, so the mock cannot drift from the agent's real argv.
- Observed-but-minor: `python_reads_enabled` (the independent reader behind `assert_agreement`) shares the installer's assumption that a plain column-0 `commands:` is what the agent reads, so it does not detect QA-1.
- The impl report's PASS list says AC-017 is covered by `assert_agreement`. That holds for the installer's own decision, not for agreement with the real agent loader; QA-1 is the case where those two differ.

## Adversarial Findings

### QA-1 (MEDIUM, correctness; FR-017 violation; test written, red): classifier reads a file as enabled when the agent reads false

**Where:** `config_enables_shutdown`, awk body in `scripts/install.sh` (between the `shutdown-config-classifier` marker comments), no rule for an indented first content line.

**What happens:** if the first content line of `agent.yaml` is indented (for example `  a: 1`, a stray leading space, or a deleted `server:` header that leaves `  url:` orphaned), yaml.v3 closes that first mapping at the next column-0 line and starts a second implicit document. `Unmarshal` decodes only the first document and ignores the rest without an error. So `agent.LoadConfig` returns no error and `Commands.AllowShutdown` is false. The classifier has no rule against an indented first line, so it parses the later column-0 `commands:` block and prints `enabled`. The installer then grants `systemctl poweroff` and prints "granting" while the agent's own gate refuses shutdown.

**Minimal reproduction** (4 variants, all fail): `"  a: 1\ncommands:\n  allow_shutdown: true\n"`. Found by the seeded mutation test: 9 of 600 mutants on Linux with both default awk and mawk (all of the same root cause), plus 4 of 300 on Windows.

**Impact:** no new privilege. The agent gate (`allow_shutdown`) and the clearance call-site checks still refuse remote shutdown, a compromised `riot` can already write a valid `true` itself, and the AD-008 line shows "granting" at re-run time. The result is the FRD "Stale grant" state (grant present, agent refuses) created by the installer, and doctor will not flag it because `allow_shutdown` reads false. It still violates the FRD's "must never" in FR-017 and the ADD's claim in AD-003 that "every rule rejects, none widens", and the story's differential test did not catch it because its corpus has no file with an indented first line.

**Suggested fix (not applied, QA does not change implementation):** in the awk program, right after the tab check, add `if (seen == 0 && ind > 0) reject()`. I applied exactly that to a disposable copy of the tree in a container: the QA mutation test (800 mutants, default awk and mawk), the QA minimal repro and the story's own differential test all pass. Please also add a corpus case such as `"  a: 1\ncommands:\n  allow_shutdown: true\n"` pinned "not enabled" to `classifierCorpus`.

**Tests:** `TestInstallSh_QA_FR017_IndentedFirstLineHidesCommandsFromAgent` and `TestInstallSh_QA_FR017_MutationsNeverExceedAgentLoader` in `scripts/shutdown_grant_qa_test.go`. Both are red now and turn green with the fix.

### Residual R-1 observed, accepted

Among the mutants, a further share (38 of 130 classifier-enabled in the 600-mutant Linux run) are files the agent cannot load at all (a YAML error outside the classifier's rules). `agent.New` returns the load error, so no agent runs and the grant cannot be used. This is the documented, accepted residual R-1 (ADD section 10, SEC-006). The QA test counts these and does not fail on them.

### Probes that held

- Fresh install, everything yes, with Docker, API key containing `#` and `:`, and a certificate pin: the installer-written config passes its own agreement check, one grant (`test_QA_fresh_yes_with_docker_key_and_pin_keeps_agreement`).
- Same action twice: the second run is a reinstall, asks nothing, and leaves drop-in and config byte-identical (`test_QA_second_run_after_a_yes_install_is_stable`).
- TERM sent to the installer between the drop-in install and the agreement check: previous policy back, no binary replaced, no config written, no restore warning (`test_QA_sigterm_between_install_and_agreement_restores_previous_policy`).
- Ctrl-C at the patching prompt after answering yes to shutdown: no drop-in change, no backup directory, no config, stage directory removed, no `systemctl stop` (`test_QA_ctrl_c_at_a_prompt_leaves_the_host_untouched`).
- FIFO in place of `agent.yaml`: refused in the early check, before the classifier's 5 s bound matters (`test_QA_fifo_agent_yaml_is_refused_without_hanging`).
- Reasoned from the code, not run: operator-supplied values (URL, key, pin) cannot smuggle a `commands:` block into a fresh config unnoticed, because the final agreement check re-reads the written file and a mismatch with the decision fails the run and restores the policy.
- The 600-mutant run found no case where the agent loads the file, reads false, and the classifier says enabled, other than the QA-1 root cause.

### Not probed

- DNF/Fedora path (no fixture exists; the poweroff echo is outside the `dnf` branch and the classifier is awk only). Carried from the impl report.
- Real fleet images (A-003 is proven on stock bookworm and trixie only). Doctor on hawaii, vermont, PiVPN and PiCUPS is Ryan's action (SC-002).
- `busybox awk` and `gawk` were not available in the `golang:1.24` image; only default awk and mawk ran in my Linux runs. The implementer ran all four.
- GitHub Actions itself was not run. The CI step's shell body was run locally for both images.

## Tests Added by QA

| File | Lines (in `HEAD`) | Covers | State |
|---|---|---|---|
| `scripts/test_install_maintenance.py` | `test_QA_fresh_yes_with_docker_key_and_pin_keeps_agreement`, `test_QA_second_run_after_a_yes_install_is_stable`, `test_QA_sigterm_between_install_and_agreement_restores_previous_policy`, `test_QA_ctrl_c_at_a_prompt_leaves_the_host_untouched`, `test_QA_fifo_agent_yaml_is_refused_without_hanging` (116 added lines at the end of the class) | AC-001, AC-005, AC-010, AC-011 (h), AC-017, AC-029, OQ-A1, OQ-A2, SEC-001, SEC-003 | green on bookworm and trixie (commit `00f010a`) |
| `scripts/shutdown_grant_qa_test.go` | `TestInstallSh_QA_FR017_MutationsNeverExceedAgentLoader`, `TestInstallSh_QA_FR017_IndentedFirstLineHidesCommandsFromAgent` | AC-011, FR-017 | red until QA-1 is fixed (commit `02d9acf`) |

All carry an "Added by QA Engineer" comment and name the AC they cover. The mutation test is seeded (`20261026`), defaults to 150 mutants (a few seconds on Linux, about 30 s on Windows), and `QA_FUZZ_N` raises it.

## Deviations from ADD

- D-2 (two agreement re-reads) and D-3 (hard-link refusal) from the implementation report are consistent with the security review and with SEC-001 and SEC-003; accepted.
- AD-003 "every rule rejects; none widens" does not hold for an indented first content line (QA-1).
- No other deviation found.

## Deviations from FRD

- FR-017: the installer can treat a config as shutdown-enabled where the agent loads it and reads false (QA-1).
- FR-036 to FR-039 (README and CHANGELOG) and AC-027/AC-028: pending the technical writer, accepted.
- All other FRs hold on the evidence above.

## Verdict Rationale

FAIL, because of one demonstrated FRD deviation (FR-017, QA-1) that the story's own tests could not see. It is narrow, not a privilege escalation, and has a one-line fix that I verified in a disposable copy; after the fix and one corpus case, a re-review is a re-run of the same suites. Everything else is solid: 44 story fixtures pass on both Debian images, the Go and web suites are green, security conditions SEC-001 to SEC-004 are met in code and tests, and the doctor check cannot run a power command.

## Action Required (for senior_dev)

1. `scripts/install.sh`, `config_enables_shutdown`, the awk program: reject a file whose first content line is indented. Add `if (seen == 0 && ind > 0) reject()` directly after `if (substr(rest, 1, 1) == tab) reject()` (the `seen` counter is already incremented only after this point for content lines, and `---` lines `next` before it). Keep the program free of single quotes and portable to mawk and busybox awk.
2. `scripts/shutdown_grant_test.go`, `classifierCorpus`: add a case such as `"  a: 1\ncommands:\n  allow_shutdown: true\n"` pinned `false`, and one with a leading blank line before the indented line.
3. Re-run and confirm green: `go test ./scripts -count=1` (this includes the two QA tests), the fixture on `debian:bookworm-slim` and `debian:trixie-slim`, and the classifier differential tests on all four awks as before.
4. Then route to the technical writer for FR-036 to FR-039 and the two doc tests (AC-027, AC-028).
