# Documentation Report
**Story ID:** SHUTDOWN-SUDO-001
**Author:** Technical Writer Agent
**Date:** 2026-10-26

QA verdict: PASS WITH NOTES (revision 2, QA-1 verified fixed). Inputs read: FRD (revision 1), ADD (revision 1), security review, implementation report (including the QA-1 addendum), QA report (revisions 1 and 2), the SHUTDOWN-DPKG-001 documentation report, and `git diff main...HEAD` for `scripts/install.sh`, `internal/agent` and `.github/workflows/ci.yml`. The prompt texts, the three `==> Remote shutdown:` lines, the sudoers line, the doctor strings, the 10 second timeout and the refusal messages in the README were copied from `scripts/install.sh` and `internal/agent/shutdown_doctor.go`, not from the ADD. The code and the ADD agree on every user-visible point. No accuracy discrepancy to flag.

## Audience

| Audience | Affected | Artifact |
|---|---|---|
| Operators and administrators (Ryan, root on each host) | Yes: a new installer question, a new opt-in privilege, a per-server action to get remote shutdown, a revoke path, a new doctor section, new installer refusals | README, CHANGELOG |
| Support staff | Yes: the three `==> Remote shutdown:` lines, the `doctor` pass and fail lines, the installer error messages | README troubleshooting table |
| Developers extending or debugging | Yes, lightly: the code, tests and ADD already carry AD, FR and SEC references | Existing inline comments, ADD |
| API integrators | No. No endpoint, schema or server change | None |

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | **New subsection "Installer questions"** (after "Optional flags"). It lists all five questions in order with the config key each controls. It covers the interactive, `-y`, `--non-interactive` and no-terminal answers, and that only a fresh install asks. It covers the reinstall behaviour (no questions, config untouched, one `==> Remote shutdown:` line, the three variants quoted), the `--bootstrap-key` reset, the earlier question order (questions run before any lock or change, Ctrl-C leaves the host untouched), and the refusal of a non-regular or hard-linked `agent.yaml`. **Agent Diagnostics:** a paragraph for the **Remote Shutdown** doctor section (when it appears, listing only, 10 s timeout, `riot` user's rules when run as root, pass and fail lines, `Permissions` line unchanged). **Sudoers Rules table:** a new `systemctl poweroff` row, opt-in, what it does not allow. **Config reference:** the `commands.allow_shutdown` row no longer says "reports `failed` until SHUTDOWN-SUDO-001"; it says the key does not add the rule and points to the section. **Upgrading existing agents:** a paragraph saying the same root re-run applies the poweroff rule. **Reboots and the dpkg lock:** the intro sentence "needs no install step ... no `install.sh` re-run is needed" is scoped to the dpkg wait. **"Shutdown on Linux and the sudo rule"** (heading and anchor kept) rewritten: the stale "grants no sudo rule" and "stays failed until SHUTDOWN-SUDO-001 ships" text is gone. New sub-headings: *What the installer grants* (the one line, which runs enable it, what counts as enabled), *Turn on remote shutdown on an existing server* (numbered steps, the fleet servers hawaii, vermont, PiVPN and PiCUPS, combine with the AGENT-HARDEN-001 and PATCH-FLOW-001 re-runs), *Turn off remote shutdown* (the revoke steps and the stale-rule note), *If the installer stops or the result looks wrong* (troubleshooting table), *Security note* (the OQ-A3 accepted residual). **Known limits:** the "until SHUTDOWN-SUDO-001" bullet is replaced and two bullets are added (stale rule after setting `false`; strict reading of `true`). **Features bullet (L60):** the "grants no poweroff sudo rule ... no re-run is needed" sentence is replaced by the opt-in description |
| `CHANGELOG.md` | Updated | `[Unreleased]`. **Added:** three SHUTDOWN-SUDO-001 entries (the question, its default, `-y` and `--non-interactive`, the one sudoers line `riot ALL=(root) NOPASSWD: <systemctl path> poweroff` and what it does not grant; the reinstall and bootstrap behaviour and the revoke path; the doctor section). **Changed:** four entries (prompt ordering moved ahead of any lock or change; the new `agent.yaml` refusals and the narrow rollback; CI fixtures now also run on `debian:trixie-slim`; this story supersedes the SHUTDOWN-DPKG-001 "stays failed until SHUTDOWN-SUDO-001" statements). **Upgrade notes:** one entry with the per-server action (set `allow_shutdown: true`, re-run `install.sh` as root; servers without it need nothing; combine with the pending AGENT-HARDEN-001 and PATCH-FLOW-001 re-runs; no upgrade order), and the known fleet state from the 2026-10-25 read-only check. The earlier SHUTDOWN-DPKG-001 entries (Upgrade notes bullet 3, Changed "BREAKING (result only)") are history and are not edited. No version is assigned and no tag was created |
| `docs/technical-writing/SHUTDOWN-SUDO-001-docs-report.md` | Created | This report |

No code, test or workflow file was touched.

**Inline comments and help text.** None added or removed. The doctor strings in `internal/agent/shutdown_doctor.go`, the `doctor.go` call-site comment and the config template line `allow_shutdown: false    # Allow remote shutdown commands` (`internal/agent/config.go` L252) are accurate. The installer's header usage text is unchanged, as FR-035 requires, and still reads correctly ("`-y, --yes` ... enable all remote features"). `grep -rn "SHUTDOWN-SUDO" internal cmd` finds only the new doctor code and tests, so there is no stale code comment to remove.

**AC-027 and AC-028 checks (ADD section 8), run as greps on the committed text.**
- README: 0 matches for `until SHUTDOWN-SUDO-001`; 0 matches for ``grants the `riot` user no sudo rule for `systemctl poweroff` ``; contains `Allow remote shutdown from dashboard?`; contains `hawaii`, `vermont`, `PiVPN`, `PiCUPS` and `allow_shutdown: true`.
- CHANGELOG `[Unreleased]` (text between `## [Unreleased]` and the next `## [` heading): contains `hawaii`, `vermont`, `PiVPN`, `PiCUPS`, `allow_shutdown: true`, and `riot ALL=(root) NOPASSWD:` on a line with `poweroff`.
- FR-036 (question, default, `-y`, `--non-interactive`, fresh-install only, enable, revoke, agent-only updates, doctor), FR-037 (named servers, "servers without it need nothing"), FR-038 and FR-039 are each covered by the README and CHANGELOG text above.

**Docs checklist (ADD section 13).**
- README and CHANGELOG updated per FR-036 to FR-039.
- The ADD section 12 note 11 items are present: fleet named, "set `allow_shutdown: true`, then re-run `install.sh` as root, only where remote shutdown is wanted"; the same re-run covers the pending AGENT-HARDEN-001 and PATCH-FLOW-001 re-runs; agent-only updates never change the rule; revoke steps; the AD-008 line; the doctor section.
- Exploit-grade detail of SEC-OBS-1 and SEC-001 is kept out. The README says only what the installer refuses (a non-regular or multiply-linked `agent.yaml`) and what to do.

## Not Done: the two documentation tests

`TestDocs_SS_AC027_NoStaleShutdownStatements` and `TestDocs_SS_AC028_FleetActionNamed` are **not written**. The implementation report (D-1) assigns them to the technical writer, in `scripts/shutdown_grant_test.go`. This task forbids editing test files, so they are left for the caller. The README and CHANGELOG now satisfy the criteria listed above, so the tests should pass once written. The ADD section 8 rows AC-027 and AC-028 give the exact strings. Note the repository working tree uses CRLF, so the tests should read the files and match on substrings, not on line endings.

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `CHANGELOG.md` `[Unreleased]` | Holds shipped stories. `git tag --contains 71a4dea` shows SHUTDOWN-DPKG-001 (PR #35) is in `v2.77.0`, and the PATCH-FLOW-001 entry says it ships as v2.76.0. REBOOT-DPKG-001 entries are in the same block | Roll shipped entries under their version headings in a separate pass. Release this story under its own heading when it is tagged |
| `CHANGELOG.md` Upgrade notes (SHUTDOWN-DPKG-001, bullet 3) and Changed ("BREAKING (result only)") | Say the installer grants no poweroff rule and the failure stays until SHUTDOWN-SUDO-001. Left as history. The SHUTDOWN-SUDO-001 Changed entry says it supersedes them | None, unless the history is rolled up. Then keep the superseded wording out of the rolled-up entry |
| `README.md` L18 | "README last updated for v2.38.0". The README documents much later work (the same finding is open from SHUTDOWN-DPKG-001) | Update or remove the marker in a separate pass |
| `README.md` (structure) | No top-level Requirements, Getting Started or Environment Variables sections in the form the writing standard asks for. Install steps live under "Server Installation" and "Agent Installation". Still open | Audit the README structure as its own task |
| `README.md` "Sudoers Rules" table | Lists the apt, update, reboot and collector-read rules. Whether it lists every rule the installer writes was not verified in this story, which added only the poweroff row | Compare the table with the drop-in the installer writes on a fixture host, in a separate pass |
| `README.md` "What ships where" (PATCH-FLOW-001) and the server-first banners | The server-first order is stated three times (PATCH-FLOW-001, REBOOT-DPKG-001, SHUTDOWN-DPKG-001). This story adds a fourth place that says re-run `install.sh` (Upgrading existing agents, shutdown section) | Consider one shared "Upgrade order and re-runs" section |
| `README.md` "Agent Installation" "This will:" list | Does not mention that a fresh install asks questions. The new "Installer questions" subsection covers it, but the numbered list above it still reads as unattended | Add a step 4a, "Ask which remote features to allow (fresh install only)", in a separate pass |
| `docs/technical-writing/SHUTDOWN-DPKG-001-docs-report.md` ("Notes for Future Writers", bullet 6; "Follow-ups" bullet 1) | Says the README states "no `install.sh` re-run or sudoers change is needed" and that SHUTDOWN-SUDO-001 would use the AGENT-PRIV capability path. Both are now superseded (this story uses the installer's fixed block, ADD AD-012) | Historical. Add a superseded-by note, or leave as history |
| `docs/architecture/SHUTDOWN-DPKG-001-add.md`, `docs/requirements/SHUTDOWN-DPKG-001-frd.md`, `docs/security/SHUTDOWN-DPKG-001-security-review.md`, `docs/implementation/SHUTDOWN-DPKG-001-impl-report.md`, `docs/qa/SHUTDOWN-DPKG-001-qa-report.md` | State that the installer grants no poweroff rule and that SHUTDOWN-SUDO-001 adds one "through the AGENT-PRIV capability path" | Historical. Add a superseded-by note pointing to the SHUTDOWN-SUDO-001 ADD (AD-012), or leave as history |
| `docs/architecture/SHUTDOWN-SUDO-001-add.md` section 13 | The Definition of Done boxes are unchecked | Tick them when the story merges |
| `internal/agent/doctor.go` `checkUpdatePrivilege` | Lists the invoking user's policy, not `-U riot`, so as root it can false-pass (security review SEC-005, ADD section 10). Not a documentation defect. The README's doctor text for the update check does not mention it | Track as a follow-up story. When fixed, update the Agent Diagnostics text |

## Accuracy Flags

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| ADD AD-005 puts the agreement check once, after the config write. The code runs it twice for a reinstall: right after the late refusal (before any `agent.yaml` write), and again after the config write (impl report D-2, accepted by QA) | `scripts/install.sh` `require_shutdown_agreement` | README says "The config and the rule always agree after a successful run" and lists the error message. It does not describe the two reads |
| ADD AD-007 describes only a non-regular-file refusal. The code also refuses a regular file whose link count is not 1 (SEC-001, impl report D-3) | `refuse_unsafe_config_path` | README and CHANGELOG say "not a regular file or has more than one hard link". The late message does not claim a policy restore, so the README says the installer "restores the previous sudo policy where it had added a poweroff rule" |
| ADD AD-008 says a fresh install prints nothing extra. Confirmed in the code. The fresh-install record is the prompt line, or nothing under `--non-interactive` | `install.sh` L498-513 | README: "A fresh install prints no such line" |
| ADD section 6.4 shows the doctor timeout text as `timed out after 10s`. The code formats the timeout with `%s`, which prints `10s` | `shutdown_doctor.go` | README: `timed out after 10s` |
| FRD FR-017 and ADD AD-003 describe the classifier as accepting plain `true`. QA-1 (an indented first content line) was a gap, fixed in `39aba1a` | `config_enables_shutdown` | README lists "a first line that is indented" among the not-enabled forms |
| Residual R-1: a YAML error outside the classifier's rules can make the installer read `true` while the agent cannot load the file | ADD section 10 | Not documented in the README. The agent does not run in that state, and `doctor` prints `Config load error`. Left to the existing doctor documentation |

## Notes for Future Writers

- **README anchors.** The shutdown section keeps its anchor `#shutdown-on-linux-and-the-sudo-rule`. It is linked from the Features bullet, the Sudoers Rules table, the `commands.allow_shutdown` config row, the doctor paragraph, the Known limits bullets, the Upgrading existing agents paragraph, the "Installer questions" subsection and the CHANGELOG. New anchors: `#installer-questions`, and the h4 sections under the shutdown section. Search for all of them before renaming a heading.
- **Where the installer's reinstall behaviour is documented.** "Installer questions" has the per-mode answers and the three `==> Remote shutdown:` lines. The shutdown section has the enable, revoke and troubleshooting steps. If a prompt text, default or order changes, update both, the Sudoers Rules row, and the CHANGELOG.
- **The three `==> Remote shutdown:` lines are quoted verbatim from `install.sh` (L506-511) and from the `assert_transparency` fixtures.** A wording change in the installer needs a README change.
- **Fleet wording.** The README names the four servers in the "Turn on" section only. The dated fleet state (vermont's flags, the three servers that could not be read) is in the CHANGELOG, because it ages. Do not copy it into the README.
- **The README says "you" and the CHANGELOG says "you".** The FRD and ADD say "Ryan". User-facing text should not.
- **Line endings.** The working tree uses CRLF (`core.autocrlf=true`). `git diff` shows only the changed lines.
- **A forward mapping is recorded, not documented for users.** When AGENT-PRIV lands, shutdown becomes its own capability derived from `commands.allow_shutdown` (ADD AD-012). The classifier and the agreement check are then retired, and `yes` or `True` will grant. The README sentence "A valid but unusual spelling of `true` counts as not enabled" and the Known limits bullet will then be wrong. Update both.
- **First production checks (owner: Ryan).** Re-run `install.sh` as root on each server where remote shutdown is wanted, read the `==> Remote shutdown:` line, then run `riot-agent doctor` and compare the **Remote Shutdown** output with the README. No pipeline agent ran the installer or doctor on a fleet host. A-003 (the bare `systemctl` resolves to the granted path) is proven on stock bookworm and trixie fixtures only. hawaii, PiVPN and PiCUPS images are not tested.
- **Release.** The branch contains installer and doctor code, so it needs a tag when it is released. The README and CHANGELOG edits alone are docs-only.
