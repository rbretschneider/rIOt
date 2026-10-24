# Documentation Report
**Story ID:** REBOOT-DPKG-001
**Author:** Technical Writer Agent
**Date:** 2026-10-24

QA verdict: PASS WITH NOTES. Inputs read: FRD, ADD (revision 3), security review, implementation report, QA report, `git diff origin/main..HEAD`, and the changed source files (`dpkglock.go`, `dpkglock_linux.go`, `reboot_clearance.go`, `commands.go`, `reboot_auth.go`, `command_policy.go`). Where the ADD and the code differ, this report says so.

## Audience

| Audience | Affected | Artifact |
|---|---|---|
| Operators and administrators | Yes: new skip results and events, a new fail-closed requirement on the agent's pid namespace, the upgrade order | README section, CHANGELOG, LearnMore |
| Support staff | Yes: they read the skip messages and the holder text | README "What you see" |
| Developers extending or debugging | Yes: why-comments in the gate and probe | Inline comments |
| API integrators | No new or changed endpoint. Only the stored `exec_deadline_s` for new `reboot` rows changes (600 to 1200) | README time-limits table |

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | New section **Reboots and the dpkg lock**: server-first upgrade banner (C-4); which reboots wait; how the wait works; how the lock is detected (stat plus `/proc/locks`, unprivileged); the fail-closed cases with their exact cause texts; the result, event and log wording; the 10 min / 12 min / 20 min budget split; the local-user veto (C-2); the accepted limits. Also: a new Features bullet; the Command Lifecycle TTL table splits the Power row (`reboot` 15 min / 20 min, `shutdown` 15 min / 10 min); step 7 added to "What the agent does after a ticked run"; the Reboot-Class Package Gating step 4 no longer says the agent reboots unconditionally |
| `CHANGELOG.md` | Updated | `[Unreleased]`: Added (the dpkg wait, results, events, detection, limits); Changed (reboot deadline 20 min and agent budget 12 min; post-patch skip is now one warning event); new Upgrade notes entry with the server-first note (C-4). No version assigned, no tag |
| `docs/ops/os-maintenance.md` | Updated | One sentence in the reboot paragraph: either post-update reboot waits for the dpkg lock; link to the README section |
| `web/src/pages/LearnMore.tsx` | Updated (text only) | One paragraph in the kernel-update section: reboots wait up to 10 minutes for a running package manager and are skipped, never forced. `cd web && npm run test:run`: 95 files, 1,707 tests passed. `npx tsc --noEmit` clean |
| `internal/agent/reboot_clearance.go` | Inline comments | Why the loop polls `shuttingDown` (the command context is cancelled only after the 20 s grace; post-patch goroutines are untracked); why `Absent` after `Held` never clears (C-1 c); why a re-taken lock resets the guards (FR-012, AC-014) |
| `internal/agent/dpkglock.go` | Inline comment | Why a holder that cannot be read never turns `Held` into `Unknown` (FR-018) |
| `internal/agent/reboot_auth.go` | Inline comment | Why the gate follows the fail-fast checks, and that the host reservation is held for the wait |
| `internal/agent/commands.go` | Inline comment | Why the unauthorized post-patch path reports a skip as an event, and that it holds the reservation for the wait |
| `docs/technical-writing/REBOOT-DPKG-001-docs-report.md` | Created | This report |

No behavior changed. No test file was touched. Existing comments in the story's files already carried FR, AC and condition references; they were kept.

Verification: `go vet ./internal/agent` clean and `go test -count=1 ./internal/agent` ok after the comment edits. Cause texts, result messages, event wording, log field names and the 10 min / 12 min / 20 min values in the README were copied from `reboot_clearance.go`, `dpkglock.go`, `commands.go`, `reboot_auth.go` and `command_policy.go`, not from the ADD.

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `docs/architecture/CMD-LIFECYCLE-001-add.md` AD-005, and `docs/requirements/CMD-LIFECYCLE-001-frd.md` section 7.3 | List the Power class as one row (execution deadline 10 min, agent maximum 1 min). `reboot` is now 20 min and 12 min | Upstream pipeline documents. Add a superseded-by note pointing to REBOOT-DPKG-001 AD-004, or leave as history |
| `docs/requirements/REBOOT-DPKG-001-frd.md` NFR-004 and its out-of-scope line | Say the gap between the final check and shutdown is "milliseconds". The ADD (R-1) and the security review (INFO-3) say sub-second to a few seconds. The README says "up to a few seconds" | Upstream pipeline document. Correct the wording in a follow-up |
| `web/src/pages/LearnMore.tsx` line 428 | Says rIOt's **Reboot** command can be used to "schedule the reboot ... during a maintenance window". The Reboot command runs at once. Only the automatic post-patch reboot is tied to a window | Rewrite the sentence in a separate pass |
| `README.md` line 19 | Says "README last updated for v2.38.0". The README documents later work (PATCH-FLOW-001 ships as v2.76.0 per the CHANGELOG) | Update or remove the marker in a separate pass |
| `README.md` (structure) | No top-level Requirements, Getting Started or Environment Variables sections in the form the writing standard asks for. Install steps are under "Server Installation" and "Agent Installation" | Audit the README structure as its own task |
| `README.md` "What ships where" (PATCH-FLOW-001) | Says "Deploy the server first, then update the agents" only for that story. The same order now matters for REBOOT-DPKG-001 and is documented separately | None needed. Consider one shared "Upgrade order" section |
| `CHANGELOG.md` | The `[Unreleased]` block holds shipped stories (the PATCH-FLOW-001 entry says it ships as v2.76.0) | Roll shipped entries under their version headings |
| `docs/implementation/REBOOT-DPKG-001-impl-report.md` | Names base `679499e`, while the branch diff against `origin/main` also holds the story documents (QA deviation 3) | Historical. Leave as is |

## Accuracy Flags

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| ADD section 4 asks for README "requirements". The README has no top-level Requirements list for the agent | `README.md` | The pid-namespace requirement and the unit settings to avoid are in "When the agent cannot tell, it skips" |
| ADD AD-003 calls a skip "`error`" status. The dashboard calls it `failed` | README results table | Status column reads `failed`, matching the dashboard word used elsewhere in the README. The message text is exact |
| ADD section 6 lists `unrecognized /proc/locks line <n>` for malformed lines. A line over 4 KiB is reported as `cannot read /proc/locks: <error>`, not as an unrecognized line | `dpkglock.go` scanner error path | The README table lists the 4 KiB case under `cannot read /proc/locks` |
| QA N-1: two pre-wait shutdown exits on the post-patch paths are silent (pre-existing) | `commands.go` sleep before the gate; `awaitResultDelivery` | The README says a **waiting** reboot ends and is reported on agent stop. It does not claim a report for a stop before the wait starts |
| QA N-2: AC-019's "two Info lines" counts the gate only | Logs | README says "A reboot that waits writes exactly two `INFO` lines" from the wait, and does not claim the whole attempt writes only two |
| The phase A result of a ticked Patch is written before the wait (FRD OQ-004, ADD R-6) | `reboot_auth.go` | Documented: `reboot started: yes` can be followed by a skip, and the warning event is the final word (README step 7 and "Known limits") |

## Notes for Future Writers

- README sections that describe one story's feature are `##` headings placed after "Manual patch and reboot" and before "Fleet Dashboard". The new section follows that pattern. The README has no table of contents; cross-links use heading anchors, so renaming a heading breaks links. Search for `#reboots-and-the-dpkg-lock` before renaming.
- The working tree uses CRLF line endings while the index uses LF (`core.autocrlf`). The Edit tool keeps the file consistent. `gofmt -l` lists every Go file in the tree for that reason; strip CR before judging formatting.
- The CHANGELOG uses one bullet per story per heading, with the story ID in brackets. Deployment and upgrade notes sit in their own `###` headings inside `[Unreleased]`. This story added `### Upgrade notes` for the server-first order.
- Unverified on real hardware, and said so in the README: the inode-only match on filesystems with an anonymous device (btrfs, ZFS, overlay root). If a real sample is taken, update the README sentence and add a fixture.
- First production check (owner: Ryan, from the QA report): a standalone reboot during a held `lock-frontend` on Hawaii, with the real `riot-agent.service`. The README's wording for results and log lines should be checked against what the journal shows then.
- The README states that no `install.sh` re-run or sudoers change is needed. `git diff origin/main..HEAD` shows no change under `scripts/` or to `internal/agent/doctor.go`, and no new collector, so the PATCH-FLOW-style "re-run install.sh" note does not apply. Keep it that way, or the statement becomes false.
- If shutdown is gated in a later story, update four places: the "Which reboots wait" table, "Known limits", the CHANGELOG, and the Power row of the TTL table.
