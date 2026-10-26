# Documentation Report
**Story ID:** EXTRA-UPD-001
**Author:** Technical Writer Agent
**Date:** 2026-10-26

QA verdict going in: PASS WITH NOTES. The AC-043 documentation tests in `scripts/extra_updaters_docs_test.go` skipped until this pass. They now run and pass.

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | New section "DietPi updates" (what you get, how it works, bounds, per-server numbered steps for hawaii, vermont, PiVPN and PiCUPS, turn-off steps, what ships where). Patch badge section rewritten for **Manual** and the group K rule (reason table with action-versus-information conditions, API fields `manual`, `info`, `updater`, `extra_updaters`). New installer question row, `commands.allow_extra_updaters` config row, `exclude_extra_updaters` and `extra_updater_budget` rows in the `os_update` parameter table, updates-collector note, sudoers note ("no new rule"), two feature bullets |
| `docs/ops/os-maintenance.md` | Updated | New section "Extra updaters (DietPi)": order after apt and the modes, the nine worker gates, opt-in and revoke with the files `install.sh` writes, bounds chain, 90-minute window and 06:25 `cron.daily` advice, reboot rule, meaning of unknown, DietPi's service stop/restart, autopurge and survey, the `CONFIG_CHECK_APT_UPDATES=2` caveat, truthful outcomes and the 128 KiB cap, all 13 refusal reasons with meaning and fix, update-source (`MichaIng`, `DEV_GITOWNER`) rule, the root trust rule and `/var/tmp/dietpi`, launcher isolation and the `/boot` to `/boot/firmware` migration, residual risks (including the upstream note), known limits (clock jump, single-process event dedupe, untested real hardware), doctor, badge. Also corrected two stale statements: the worker "accepts no arguments" (it now accepts `--capabilities` and `--run-extra dietpi`) and the request/privilege descriptions; added the policy file and drop-in to the privilege table |
| `CHANGELOG.md` | Updated | `[Unreleased]`: eight Added entries, two Changed entries (one marked **BREAKING (badge meaning)**), two Upgrade-notes entries (per-server action and accepted known limits). No version number chosen, no tag |
| `internal/maintenance/extra.go` | Inline comment | Added a KNOWN LIMIT comment at the future-dated-record branch of the 60-minute interval check (QA N-1). No logic change |
| `internal/server/handlers/agent_event_extra.go` | Inline comment | Added a SEC-005 / N-2 comment at the dedupe mutex: single-process only, key is (device, command, type) not the updater. No logic change |
| `docs/technical-writing/EXTRA-UPD-001-docs-report.md` | Created | This report |

No web files were touched, so the web suite was not re-run.

## Verification

All with `GOCACHE=D:\rIOt_gocache` and `GOTMPDIR=D:\rIOt_fuzz_tmp`.

- `go test ./scripts/... -run Doc -v -count=1`: 9 PASS, 0 SKIP. The six AC-043 tests (`OpsDocCoversEveryTopic`, `ReadmeStepsPerServer`, `ChangelogEntry`, `SecurityStatements`, `QA_ResidualsAndAcceptedLimits`, `DocsFilesExist`) and the SEC-008 negative check (`NoManualCopyOfVarTmpDietpi`) pass. `TestDocs_SS_AC027` and `AC028` (SHUTDOWN-SUDO-001) still pass.
- `go test ./...`: every package `ok`, 0 FAIL.
- Written checks done by hand: no "simply", "just", "easily", "obviously" or hedging words in the added text; no line in any doc names `/var/tmp/dietpi` after the words cp, mv, rsync, install, ln or tar (the SEC-008 pattern).

## Stale Content Found (Not Fixed)

Items outside this story that need their own documentation pass:

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` L3 | "README last updated for v2.38.0". The README documents releases up to v2.78.0 | Update or remove the line |
| `README.md` (Agent Installation, "Upgrading existing agents") | Says the first release containing MAINT-RECOVERY-001 "is required" and that v2.72.0 lacks the assets. That wording reads as a future event | Rewrite against the actual release that shipped the worker |
| `docs/ops/os-maintenance.md` L5 and the last paragraph | "This feature is unreleased until a release containing the new assets is published", "QA passed with notes against `e12436a`", "Vermont's ... worker has not been deployed there". The worker has shipped, and vermont has since been hardened | Re-write the status paragraph; move the real-systemd smoke-test gate to a dated record |
| `CHANGELOG.md` `[Unreleased]` | Holds entries for work that has shipped and has no release headings: PATCH-FLOW-001 (v2.76.0), REBOOT-DPKG-001 and SHUTDOWN-DPKG-001 (v2.77.0), SHUTDOWN-SUDO-001 (v2.78.0). The section also repeats `### Added` and `### Changed` and runs into old Docker-area content | Cut release sections for each shipped version and restore one heading per kind |
| `requirements.md` (root) | "Version 0.1.0-draft, Last Updated 2026-03-03". Not audited against the code | Audit or archive |
| `docs/security/EXTRA-UPD-001-security-review.md` L280 and L286 | The revision 2 closure notes still say the drop-in sets `PrivateTmp=yes` and that check 6 requires it. Revision 3 reversed this (OPS-001). The later re-review section is correct | Owner of the review: add a "superseded" marker on the two notes |
| Project root | No `CONTRIBUTING.md`. The project has one maintainer, so none was created | Create one if a second contributor joins |

## Accuracy Flags

Discrepancies between the ADD, FRD, UI and the code. Each is documented as the code behaves.

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| The dashboard notice for `non_default_update_source` says "rIOt will not apply it" for any non-default source. The code (worker check 4 and agent readiness) compares the owner only. A host with owner `MichaIng` and a different branch still has readiness `enabled` and runs DietPi, while the notice says it will not | `web/src/utils/extraUpdaters.ts` L78 against `internal/maintenance/extra.go` L223 and `internal/agent/extra_updaters.go` readiness | "The worker checks the owner only. A different `DEV_GITBRANCH` does not block the step, but the dashboard shows a notice." Recommend the architect decide: change the notice text, or check the branch |
| ADD AD-005 check 8 says a future-dated start is ignored "with a note". The code adds no note | `internal/maintenance/extra.go` (check 8) | Documented as the clock-jump limit; the note is not promised. Already raised as QA N-1 |
| The security review's SEC-006 closure says a backward clock jump shortens the interval "once". The code disables it for as long as the future-dated record is the newest | `internal/maintenance/io_linux.go` `LatestExtraStart` | "disables the 60-minute interval while it lasts" (QA N-1 wording) |
| ADD section 5.3 lists the skip reasons without `excluded_by_operator`. The agent writes it for a run the operator excluded | `internal/agent/extra_updaters.go` L299 | Listed as an agent-side neutral reason in the refusal table and the outcome rules |
| The installer's drop-in and verify step use the systemd form `85min` and `1h 25min`. Doctor prints the value in Go's duration format | `internal/agent/extra_updaters_doctor.go` L68 | The doctor table shows `1h25m0s`; the installer line shows `85min` |

## What Could Not Be Documented or Verified

- **Nothing was run on a host.** The hard rules forbid SSH, the installer, doctor, `riot-agent`, DietPi tools, apt, reboot and shutdown. The installer lines, the doctor lines and their checkmarks come from `scripts/install.sh` and `internal/agent/extra_updaters_doctor.go`. The two `grep` commands in the per-server steps were run only against a sample file on this PC.
- **Real Raspberry Pi behavior** (QA N-3) is stated as a known limit. The 9 to 10 reboot, the `/boot` to `/boot/firmware` move and whether a FAT `/boot` passes the trust walk on PiVPN and PiCUPS are untested. The refusal table says a world-writable FAT `/boot` gives `unsafe_dietpi_path`. No mount-option fix is given, because none was verified.
- **The 06:25 default** of `cron.daily` comes from ADD decision D-2. It was not checked on a DietPi host. The docs say "by default" and ask the reader to confirm the host's time.
- **DietPi's own behavior** (service stop and restart, autopurge, the survey) is documented from the FRD (DEC-001) and the DietPi source facts recorded in the ADD. It was not re-read upstream.
- **No release version.** The install command carries `--version X.Y.Z` with the instruction to use the release that contains EXTRA-UPD-001. That matches the existing README convention (`--version X.Y.Z`). Replace it once the release is tagged.
- **Homelab IP addresses** (PiVPN and PiCUPS) are left out of the README on purpose, because the README is published. The steps name the servers.
- **Unverified README claim risk:** the per-server steps tell the reader that every reboot route needs `commands.allow_reboot: true` on PiVPN. This follows `maybeRebootAfterPatch` and the existing reboot gate, but the PiVPN config value itself is unknown.

## Notes for Future Writers

- The working tree is CRLF (`core.autocrlf=true`) and the index is LF. Edits through the editing tool kept the line endings consistent. `gofmt -l` lists many files in the working tree for that reason alone.
- `scripts/extra_updaters_docs_test.go` checks plain phrases, not headings. Keep the phrases `after apt`, `128 KiB`, `40 minutes`, `stops and restarts`, `autopurge`, `survey`, `CONFIG_CHECK_APT_UPDATES=2`, `06:25`, `upstream`, `60 minutes`, `90 minutes`, `/boot/firmware`, `clock`, `single`, `MichaIng` and `DEV_GITOWNER` in `docs/ops/os-maintenance.md`. Keep `AGENT-HARDEN-001`, `PATCH-FLOW-001`, `SHUTDOWN-SUDO-001` and a "9 ... 10 ... reboot" line in the README.
- The SEC-008 test scans for a copy-like word (`cp`, `mv`, `rsync`, `install`, `ln`, `tar`) before `/var/tmp/dietpi` on one line, in the README, the ops guide and the CHANGELOG. In Markdown a paragraph is one line, so a sentence such as "Run install.sh, then look at /var/tmp/dietpi" fails the test. Never tell a reader to copy anything out of `/var/tmp/dietpi`.
- The README holds the per-server steps, because the docs test expects them there. The ops guide holds the reasoning and the refusal table. Link between them; do not copy the text.
- Item codes in the badge table (`auto_patch_off`, `extra_update_pending` and so on) must match `internal/server/handlers/patch_badge.go`. A code that is added there needs a row in the README reason table.
- When a second extra updater is built, update: the `commands.allow_extra_updaters` row, the dialog checkbox label (today "Apply DietPi update in this run"), the event dedupe key (QA N-2), and the per-updater facts in the ops guide (bounds, trust roots, source rule).
