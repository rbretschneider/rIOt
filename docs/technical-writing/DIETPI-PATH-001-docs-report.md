# Documentation Report
**Story ID:** DIETPI-PATH-001 (bugfix, EXTRA-UPD-001 follow-up)
**Author:** Technical Writer Agent
**Date:** 2026-10-26
**QA verdict:** PASS WITH NOTES
**Branch:** `fix/dietpi-run-dir`, base main `c55e4bd`

## Audience

| Audience | Affected | Where documented |
|----------|----------|------------------|
| Operator (Ryan, an administrator of DietPi hosts) | Yes: new badge, new refusal reason, per-host re-run steps, host hardening | `README.md`, `docs/ops/os-maintenance.md`, `CHANGELOG.md` |
| API integrator | Yes: new `patch_badge.kind` value and `explanation.blocked` list | `README.md` (API fields), `CHANGELOG.md` |
| Developer and debugger | Yes: trust rule, launcher steps, flag trust, outcome rule | `docs/ops/os-maintenance.md`, existing ADD and security review |
| End user of the dashboard | Yes: amber Blocked badge, "N blocked" filter | `README.md` (Patch badge) |

There is no new endpoint, environment variable, collector, sudoers line, config key or migration. `internal/agent/doctor.go` is unchanged by this story, so the collector-whitelist rule in project memory does not apply.

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `docs/ops/os-maintenance.md` | Updated | Trust-walk section rewritten (parents, `/boot` trees, three exact `/var/tmp/dietpi` paths, what is not checked and why). `rundir_not_private` row added; `unsafe_dietpi_path` row rewritten as a possible tampering signal; `mount_propagation_unverified` row notes the re-check. New sections: flag trust (with the DP-SEC-006 user-namespace statement), private `/run/dietpi`, flag cleanup, badge classes (Blocked and Needs you), host hardening (DP-SEC-008), upgrading a host refused for `/run/dietpi`, downgrade note. Results section rewritten for the new outcome rule and the old-worker reinterpretation. Known limits and residual risks extended. Three stale sentences corrected (the `insufficient_time` fails-the-command sentence, the reboot bullet, the "two named above" launcher sentence). |
| `README.md` | Updated | Feature bullet and Patch badge section: Blocked kind, "N blocked" filter, two new reason-code rows, a new "Refused updates: Blocked and Needs you" section with the reason classes, API field description. DietPi section: Result bullet corrected, two "How it works" items, "What ships where" rows, and a new "Fix for stock DietPi hosts (DIETPI-PATH-001)" section with numbered per-server steps for hawaii, vermont, PiCUPS and PiVPN. |
| `CHANGELOG.md` | Updated | Unreleased entries, no version chosen. Added: `blocked` badge kind, `rundir_not_private`, flag cleanup, ops-guide additions. Changed: event `reason` field, `unsafe_dietpi_path` meaning, flag trust, result behaviour change, server-side parsing bounds. Fixed: DietPi runs on stock DietPi, truthful outcome. Upgrade note with the server-first order, the client rule for `blocked`, the downgrade note and the five per-server steps. |
| `docs/implementation/DIETPI-PATH-001-impl-report.md` | Corrected | Deviation #7 and the matching test-change row now carry a "Superseded by `87c9092`" note. The original text stays as history. This file is an upstream pipeline document; the edit was made on the coordinator's instruction, and only adds the note. |
| `docs/technical-writing/DIETPI-PATH-001-docs-report.md` | Created | This report. |
| Inline code comments | Reviewed, no change | The changed Go files already carry comments that give the reason and cite FR, AD and DP-SEC numbers (`launcher.go`, `flags.go`, `extra_trust.go`, `dietpi.go`, `protocol.go`, `patch_badge_refusals.go`). None restates the code. No code was touched. |
| `CONTRIBUTING.md` | Not changed | The file does not exist. The project has one contributor, so the instruction to create it does not apply. |

## What was verified

| Check | Result |
|-------|--------|
| Every documented behaviour was read from the changed code, not from the ADD alone | `launcher.go` (`isolateRunDir`, `proveRunDirIsolation`, `reverifyPropagation`), `flags.go`, `extra_trust.go` (`checkExact`), `dietpi.go` (specs), `protocol.go` (`Failing`, `Refused`), `patch_badge.go` and `patch_badge_refusals.go` (texts, classes, bounds), `agent_event_extra.go`, `PatchBadge.tsx`, `FleetOverview.tsx` |
| Badge texts quoted in the docs match the code | Compared with `refusalText`, `refusalWords`, the suffix constants and the tooltip string |
| tmpfiles override ordering (DP-SEC-008) | Tested in a throwaway container (`riot-fixture:bookworm`, `--network none`, `systemd-tmpfiles --create`). The first file in name order wins and a later line for the same path is ignored with `Duplicate line for path`. A `1777` line in `00-early.conf` beat a `0777` line in `dietpi.conf`; the reverse order kept `0777`. The docs therefore tell Ryan to name the override so it sorts before `dietpi.conf` (`00-dietpi-run.conf`). The task wording ("a file with the same name") would have replaced DietPi's own file, which a DietPi update can overwrite. |
| `go test ./scripts/... -count=1` (`GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`) | ok, 61.1s. All nine `TestDocs_*` tests ran and passed, none skipped, including the EXTRA-UPD-001 SEC-008 check that forbids telling Ryan to copy files out of `/var/tmp/dietpi` by hand |
| `go test ./... -count=1` | ok, every package passes |
| Line endings | `README.md` and `docs/ops/os-maintenance.md` are CRLF in the working tree and LF in the index (autocrlf). The edit tool kept CRLF. See the commit notes for the `git diff --ignore-cr-at-eol` check |
| Commands in the README steps | The installer and doctor commands are copied unchanged from the existing, tested EXTRA-UPD-001 steps. The one new command, `grep -n 'allow_patching\|allow_extra_updaters' /etc/riot/agent.yaml`, was run against a sample file and prints both lines. Nothing was run on a host. |

## Stale Content Found (Not Fixed)

Items found outside the scope of this story that need a documentation pass:

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` "DietPi updates" intro (around L2979 before this change) | Says the feature "is not part of any release until the release that contains EXTRA-UPD-001 is published". PiCUPS ran the DietPi step on v2.80.0, so it has shipped | Replace with the release number that first contained EXTRA-UPD-001, after Ryan confirms it |
| `README.md` "DietPi updates" steps (PiVPN and PiCUPS step 4) | "Replace `X.Y.Z` with the release that contains EXTRA-UPD-001" is now ambiguous: a re-run for the current fix needs the DIETPI-PATH-001 release | Name the minimum release once both are published, and point to the fix section |
| `docs/ops/os-maintenance.md` L5 | "This feature is unreleased until a release containing the new assets is published. v2.72.0 does not include the worker." MAINT-RECOVERY-001 has shipped in later releases | Update the status paragraph with the first release that contains the worker |
| `docs/ops/os-maintenance.md` "Real Raspberry Pi hardware has not been tested" (Known limits) | PiCUPS has now run the step as far as the path check. The DietPi 9 to 10 update through the launcher is still untested, so the bullet is partly right | Re-word to say what is still untested once a first successful run exists |
| `docs/ops/os-maintenance.md` "Root migration" and "Validation gate" | Still say "Before production rollout, complete the real-systemd validation" and "this new worker has not been deployed there". The worker has run on PiCUPS (v2.80.0), so these are likely stale. The writer could not verify the rollout state | Ask Ryan for the current state, then rewrite in an isolated pass |
| `README.md` | Two `#### PiCUPS` and two `#### PiVPN` headings now exist (the original steps and the DIETPI-PATH-001 steps). The second of each gets a `-1` anchor on GitHub | Rename if anchors for these headings are ever linked |

## Accuracy Flags

Discrepancies between the ADD or FRD and what the code does, or between pipeline documents:

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| FRD FR-015 / AC-012 text versus ADD revision 2 | The QA report listed two editorial mismatches. The coordinator fixed them in `acda2ef` before this pass | The docs follow the code and ADD revision 2: the legacy `/run/dietpi ` false positive is **Blocked** with the "re-run install.sh as root" suffix, and every other `unsafe_dietpi_path` is **Needs you** |
| Implementation report deviation #7 | Superseded by `87c9092`. The code now drops an unknown or empty reason and keeps the event | Documented as the current behaviour in the ops guide and the CHANGELOG. The impl report carries a superseded note |
| ADD AD-5 plain-words table versus `refusalWords` | The ADD table has no entry for `non_default_update_source` or `insufficient_time`. The code adds words for them ("the host's update source is not the default repository", "the patch window was too short for the step") | The docs give the per-reason action text, which the ADD specifies. The explanation words for these two are not quoted |
| ADD says the tmpfiles override uses "the same name" (task wording) | Verified to be wrong in practice: systemd-tmpfiles keeps the first line for a path | Documented as an override that sorts before `dietpi.conf`. Ryan should check the result with `systemd-tmpfiles --cat-config` |
| Production `trustFS` file | The ADD table says `io_linux.go`; the code is in `extra_linux.go` (impl deviation #6) | Not user-facing; not documented |
| Security review answer 5 | States that without a uid mapping, host root appears as the overflow uid. This reads oddly for a container without a user namespace | The ops guide repeats only the safe conclusion: where host root is not mapped, no flag is believed and the host reads `unknown`. Worth a second look by the security reviewer |

## Not documented

- The unit-test and fixture details (the opt-in `RIOT_NS_FIXTURE` real-namespace test, the guard test for `DirFSOwnedBy`, the installer record validator change in `scripts/install.sh`). They are covered by the impl and QA reports. The installer validator change has no operator-visible effect except that the re-run no longer refuses after a refusal.
- The `DirFSOwnedBy` test seam (DP-SEC-006, code half). It is a developer detail. The operator-facing half (user-namespace mapping is unsupported) is written.
- The server's bounded parse of the stored result is described in the CHANGELOG only, not in the README.
- A doctor check for the isolation preconditions does not exist (QA note 1), so the docs say so and give the manual `findmnt` and `ls -ld` checks.

## Notes for Future Writers

- **Line endings.** `README.md` and `docs/ops/os-maintenance.md` check out as CRLF on Ryan's PC (`core.autocrlf=true`) while the index stores LF. The Edit tool preserves CRLF. If any tool rewrites a tracked file to LF only, confirm with `git diff --ignore-cr-at-eol` and restore only EOL-only files.
- **Doc tests in `scripts/`.** `extra_updaters_docs_test.go` and `shutdown_grant_docs_test.go` read the three main documents. The SEC-008 regular expression rejects any line where the words `cp`, `mv`, `rsync`, `install`, `ln` or `tar` come before `/var/tmp/dietpi`. Put `/var/tmp/dietpi` before such words in a sentence, or leave them out of the line. The topic checks expect the phrases `after apt`, `repair`, `allow_extra_updaters`, `revoke`, `40 minutes`, `128 KiB`, `timed_out`, `stops and restarts`, `autopurge`, `survey`, `06:25`, `upstream`, `90 minutes` and `/boot/firmware` to stay in the ops guide.
- **Anchors.** Other documents link to these headings: `#refusal-reasons`, `#known-limits`, `#results-and-truthful-outcomes`, `#how-the-step-is-isolated`, `#host-hardening-for-dietpi-hosts-recommended`, `#the-patch-badge-on-dietpi-hosts` in the ops guide, and `#refused-updates-blocked-and-needs-you`, `#fix-for-stock-dietpi-hosts-dietpi-path-001`, `#patch-badge` in the README. Keep them stable.
- **Per-server steps.** Ryan's format: numbered plain steps, the server named in each step, commands run on that box, no ssh commands. The fleet is hawaii (runs the rIOt server), vermont (Debian, no DietPi), PiVPN and PiCUPS (DietPi). The installer invocation in the README uses `X.Y.Z` until a release exists.
- **Release.** No version was chosen. When the release is tagged, replace "the release that contains DIETPI-PATH-001" with the number in the README fix section, and move the CHANGELOG entries under the version heading. Docs-only commits need no tag.
- **Follow-ups worth planning.** A doctor check for the isolation preconditions (QA note 1). A note in the dashboard that explains why a Blocked badge persists until the next run. A stale-content pass over the items above.
