# Documentation Report
**Story ID:** SHUTDOWN-DPKG-001
**Author:** Technical Writer Agent
**Date:** 2026-10-25

QA verdict: PASS WITH NOTES. Inputs read: FRD (revision 2), ADD (revision 3), security review, implementation report (including the post-QA fix), QA report, `git diff fa8f924..HEAD`, and the changed source files (`commands.go`, `poweroff.go`, `reboot_clearance.go`, `agent.go`, `command_policy.go`). The gate texts, result messages, log messages, the 3 second start check and the 10 / 12 / 20 minute values in the README were copied from the code, not from the ADD. The code and the ADD agree. There is no accuracy discrepancy to flag.

## Audience

| Audience | Affected | Artifact |
|---|---|---|
| Operators and administrators | Yes: shutdown now waits, its result is truthful (a visible new failure on installer-managed Linux hosts), a delay of up to about 25 minutes, a restart-the-agent cancel, and the upgrade order | README section, CHANGELOG, LearnMore |
| Support staff | Yes: they read the new `shutdown: <error>` and skip messages | README "What you see" |
| Developers extending or debugging | Yes: how the gate is shared, but the code already carries FR, AD and condition references | Existing inline comments |
| API integrators | No new or changed endpoint. Only the stored `exec_deadline_s` for new `shutdown` rows changes (600 to 1200) | README time-limits tables |

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | Section **Reboots and the dpkg lock** (heading and anchor kept). A second upgrade banner for shutdown with the 600 to 1200 second deadline. The intro covers shutdown. "Which reboots wait" became "Which power actions wait", and the Shutdown row says Yes. "How the wait works" names both config flags and says a shutdown skipped by an agent stop is never run again. New **Shutdown** results table with the exact messages, the printable 200 byte `<error>`, the existing `Command shutdown failed` warning event, and what "shutdown initiated" means (3 second exit watch). Agent log paragraph gains `before shutdown`, `path=shutdown` and the late `WARN` line. New subsection **Shutdown on Linux and the sudo rule** (FR-033, C-2). New subsection **How long a shutdown can take, and how to cancel it** (FR-033, C-5): 10 minutes, about 25 minutes worst case, no dashboard cancel, restart the agent to cancel, editing `allow_shutdown` does not cancel. Time limits table gains the shutdown rows. "Local users can block reboots" is now "... reboots and shutdowns". Known limits lose "Shutdown is not gated" and gain the 25 minute, sudo, signal (R-10) and delay items. Other places: the Features bullet (L60), the TTL table Power: shutdown row (now 15 minutes / 20 minutes), the `commands.allow_shutdown` config row |
| `web/src/pages/LearnMore.tsx` | Updated (text only) | The reboot-wait paragraph now covers shutdowns. `cd web && npm run test:run`: 95 files, 1,707 tests passed |
| `CHANGELOG.md` | Updated | `[Unreleased]`: two Added entries (the wait; the truthful result), two Changed entries (budgets 12 min / 20 min; the result change marked **BREAKING (result only)**), three Upgrade notes entries (server first; installer hosts report failed; delay and host stays off). `git tag --contains 09c0ecf` prints nothing, so REBOOT-DPKG-001 is not tagged yet. Its entries were amended: the Added entry no longer says "`shutdown` is not gated", and the Changed entry no longer says "`shutdown` keeps 10 minutes and 1 minute". No version assigned, no tag |
| `docs/technical-writing/SHUTDOWN-DPKG-001-docs-report.md` | Created | This report |

No code or test file was touched.

**Inline comments.** None added or removed. ADD section 4 asks for none in code, and the story's files already explain why, with FR, AD and condition references (`handleShutdown`, `execShutdown`, `awaitPowerClearance` step 5, `ShutdownStartCheckBound`, `poweroffRunning`). The comments I checked match the behavior, including the post-QA reorder in `execShutdown` (clear the flag before the hand-off).

**Docs QA checklist (ADD section 13).**
- `grep -n -i "not gated" README.md web/src/pages/LearnMore.tsx`: no matches.
- README TTL table, Power: shutdown row: 15 minutes / 20 minutes.
- The shutdown text states: no poweroff sudo rule and `failed` until SHUTDOWN-SUDO-001; the meaning of "initiated"; about 10 minutes, or about 25 minutes if disconnected; restart the agent to cancel; the local-user veto.
- Nothing presents editing `allow_shutdown` as a cancel path. The README says it is not one.
- No poweroff row was added to the Sudoers Rules table (INFO-4).
- The CHANGELOG has the server-first upgrade note.

## Stale Content Found (Not Fixed)

Items outside this story's scope, or historical pipeline documents, that need a pass:

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `docs/architecture/REBOOT-DPKG-001-add.md` L64, L100, L418 area (AD-004 shutdown column, L446), L594 ("unchanged: `handleShutdown`"), L638, L850 (note 10, "Never call `handleShutdown` in tests"), L875 (DoD shutdown line); also the DoD grep for `rebootPermit{` | State shutdown keeps 1 minute / 10 minutes, has no gate, and is not to be called in tests. Shutdown now has its own gate and budgets (12 min / 20 min). The type `rebootPermit` is renamed `powerPermit` | Historical. Add a superseded-by note pointing to SHUTDOWN-DPKG-001 AD-005 and AD-008, or leave as history |
| `docs/requirements/REBOOT-DPKG-001-frd.md` FR-020 (L95), AC-020 (L246-250) and out-of-scope L259 | Say shutdown behaves as before with no dpkg wait and an unchanged budget | Same: superseded by SHUTDOWN-DPKG-001 FRD |
| `docs/security/REBOOT-DPKG-001-security-review.md` INFO-4 (L245) | The budget-split note treats shutdown as ungated and at 10 minutes | Same: superseded note |
| `docs/architecture/CMD-LIFECYCLE-001-add.md` AD-005 (L355, `power` row 10m / 1m) and AD-013; `docs/requirements/CMD-LIFECYCLE-001-frd.md` section 7.3 | List the Power class as one row (execution deadline 10 min, agent maximum 1 min). `reboot` and `shutdown` are now 20 min and 12 min each. This was already stale for reboot (logged in the REBOOT-DPKG-001 report) | Add a superseded-by note pointing to REBOOT-DPKG-001 AD-004 and SHUTDOWN-DPKG-001 AD-005 |
| `docs/technical-writing/REBOOT-DPKG-001-docs-report.md` ("Notes for Future Writers", last bullet) | Says to update four places if shutdown is gated later. That has now happened | Historical. This report records where: the "Which power actions wait" table, "Known limits", the CHANGELOG, and the TTL table |
| `web/src/pages/LearnMore.tsx` L428 | Says rIOt's **Reboot** command can "schedule the reboot ... during a maintenance window". The Reboot command runs at once. Only the automatic post-patch reboot is tied to a window. (Also logged in the REBOOT-DPKG-001 report; still open) | Rewrite the sentence in a separate pass |
| `README.md` L18 | "README last updated for v2.38.0". The README documents much later work | Update or remove the marker in a separate pass |
| `README.md` (structure) | No top-level Requirements, Getting Started or Environment Variables sections in the form the writing standard asks for. Install steps are under "Server Installation" and "Agent Installation". Still open | Audit the README structure as its own task |
| `README.md` "What ships where" (PATCH-FLOW-001) and the two server-first banners | The server-first order is now stated three times (PATCH-FLOW-001, REBOOT-DPKG-001, SHUTDOWN-DPKG-001) | Consider one shared "Upgrade order" section |
| `CHANGELOG.md` | The `[Unreleased]` block holds shipped stories (the PATCH-FLOW-001 entry says it ships as v2.76.0) and now three stories that are not tagged yet | Roll shipped entries under their version headings |
| `docs/technical-writing/AGENT-HARDEN-001-docs-report.md` L72 | Logs a README Sudoers Rules `sudo systemctl poweroff` row as stale. The README has no such row (security review INFO-4) | Historical. Nothing to do, and do not reintroduce the row |

## Follow-ups Worth Recording (not documentation defects)

- **SHUTDOWN-SUDO-001** (a poweroff privilege through the AGENT-PRIV capability path). When it ships, update the README subsection "Shutdown on Linux and the sudo rule", the Known limits bullet, the Features bullet (L60), the `commands.allow_shutdown` config row, and the CHANGELOG Upgrade notes. Also add the new row to the Sudoers Rules table at that time.
- **QA finding F-2 (C-4 scan limit).** The structural scan does not see a power tool run through a wrapper other than `sudo`, `doas` or a shell: `env`, `nohup`, `setsid`, `timeout`, `nice`, `systemd-run`, or a write to `/proc/sysrq-trigger`. This is inside the ADD's accepted residual R-11. The README makes no claim about it, so no README change is needed. If the scan is extended in a follow-up, update ADD AD-006.3 and R-11.
- **Dashboard cancel and progress for a waiting shutdown** (NFR-007, unnumbered). The README says today that neither exists. Update "How long a shutdown can take, and how to cancel it" and the Known limits when it ships.
- **QA finding F-5.** `go test -race ./internal/agent` has not run on Linux yet. Nothing in the documentation depends on it.

## Accuracy Flags

Any discrepancy between the ADD/FRD and what the code actually does:

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| ADD AD-004 labels the failed statuses `error`. The dashboard calls them `failed` | README shutdown results table | Status column reads `failed`, matching the reboot table. The message text is exact |
| ADD section 10 and AD-001 say the gate checks `commands.allow_shutdown` again as a guard. A running agent reads its config once, so `guard failed: commands.allow_shutdown is off` is reachable in unit tests only | README `<guard>` list | The README lists only `host busy: ...` and `agent command time budget exhausted` as guards a user can see. It says editing `allow_shutdown` does not cancel a wait |
| ADD section 6 shows a `command_id` on the late `WARN` line. The code writes `error` only (AQ-2, accepted in ADD revision 3) | README agent log paragraph | Says the line has an `error` field and no `command_id` |
| ADD revision 3 AQ-1: the "still running" error is built in `execShutdown`, and the classify fallback reads `shutdown command did not start` | README results table | The "still running" message is listed as written in the code. The fallback appears only through the generic `shutdown: <error>` row |
| QA note F-3 (`poweroffRunning` cleared after the hand-off) is fixed in `b3430be` | `execShutdown` | The README does not describe the ordering. A second shutdown sent after the child exits is not refused as "still running" |
| FRD FR-033 says the dashboard "cannot cancel". The ADD and code add no cancel, and no test covers the dashboard | README | Stated as a limit with the restart-the-agent workaround. An unverified point: a command that is still queued for an offline device is delivered on reconnect unless it expires. The README says this from the existing queued-expiry rules, not from a shutdown-specific test |

## Notes for Future Writers

- The README section **Reboots and the dpkg lock** now covers two commands. Its anchor `#reboots-and-the-dpkg-lock` is linked from the Features bullet, the TTL table, the `commands.allow_shutdown` config row and the CHANGELOG. Two new anchors exist: `#shutdown-on-linux-and-the-sudo-rule` and `#how-long-a-shutdown-can-take-and-how-to-cancel-it`. Search for all three before renaming a heading.
- The working tree uses CRLF line endings. The Edit tool keeps them. `git diff --stat` shows only the changed lines.
- The CHANGELOG uses one bullet per story per heading, with the story ID in brackets. Upgrade-order notes go in `### Upgrade notes`. This story added three bullets there.
- The README says "reboot and shutdown limits are separate settings that happen to be equal". If one changes, update the Time limits table and the budget table in "Queued TTLs and execution deadlines".
- The README states that no `install.sh` re-run or sudoers change is needed, and that no sudoers row was added. `git diff fa8f924..HEAD` shows no change under `scripts/`. When SHUTDOWN-SUDO-001 ships, both statements change.
- Never describe editing `commands.allow_shutdown` as a way to stop a running wait. The agent reads its config only at start-up (security review C-5). Restarting the agent is the cancel, and it also reloads the config.
- First production check (owner: Ryan): the first real shutdown on a host with a poweroff privilege, and the first on an installer-managed host, which is expected to report `failed`. Compare the journal lines with the README wording in "What you see" and "Agent log". No real host was contacted for this story.
