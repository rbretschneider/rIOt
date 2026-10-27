# Formal Requirements Document

- **Story ID:** EXTRA-UPD-001
- **Title:** Extra updaters in the OS patch flow (generic step, DietPi first), and "Needs you" only when work is waiting
- **Author:** Business Developer Agent
- **Date:** 2026-10-26
- **Revision:** 2
- **Status:** DRAFT. Ryan was not reachable during the pipeline, so Clod set the product decisions on Ryan's behalf. Section 11.1 lists each one as a "Decision for Ryan's review", and the FRD is written to those defaults.
- **Related:** PATCH-GATE, PATCH-FLOW-001 (manual Patch dialog, `reboot_if_required`, reboot-required definition), REBOOT-DPKG-001 (dpkg-lock wait before reboot), SHUTDOWN-SUDO-001 (opt-in pattern read by the installer), MAINT-RECOVERY-001 (privileged maintenance path), MAINT-DBUS-001, EXPECT-001 (unmerged).
- **Amends:** the PATCH-BADGE-001 needs-you rules NY-1 to NY-9 and its rule "any needs-you reason makes the badge Needs you". No PATCH-BADGE-001 FRD or ADD exists in the repository; the rules are recorded in `internal/server/handlers/patch_badge.go` (`computePatchBadge`). Rule NY-10 as amended by PATCH-FLOW-001 (FR-003 to FR-006) does not change. See group K.

## Changelog

- **Revision 1 (2026-10-26):** First draft.
- **Revision 2 (2026-10-26):** Scope addition from Ryan: "it shouldn't say needs me unless there ARE updates". Added group K (FR-068 to FR-084), AC-046 to AC-055, DEC-016 to DEC-021, OQ-A8 and R-006. Rewrote FR-051, FR-052 and DEC-009 to follow group K.

---

## 1. Executive Summary

Some hosts have a platform updater that works outside the package manager. On DietPi this is `dietpi-update`. rIOt does not see these updaters today, so PiVPN and PiCUPS never install DietPi updates and Ryan cannot tell when they will go down for one. This story adds a small, generic "extra updater" step to rIOt's OS patch flow. The agent detects which extra updaters exist on a host and reports their pending updates. The dashboard shows them next to apt updates and includes them in the patch badge. On hosts Ryan has opted in, the updates are applied after apt, in both the scheduled patch window and the manual Patch action. The step goes through the existing privileged maintenance path and runs one exact command. It has a time limit, a capped log and a truthful result for each updater. It never reboots on its own, and any reboot goes through the existing reboot gate. DietPi is the only updater built in this story. Snap and flatpak must fit the same contract later without a redesign.

The story also corrects the patch badge. A host now shows "Needs you" only when updates are waiting (apt or an extra updater) or a reboot is required, and nothing will apply them. Settings alone, such as automatic patching being off, become neutral information.

## 2. Background & Context

- **Ryan's goal:** rIOt manages and schedules platform updaters outside the package manager, so Ryan knows when machines go down.
- **Fleet:** hawaii and vermont run Debian/OpenMediaVault without DietPi. PiVPN (10.0.10.32) and PiCUPS (10.0.20.120) run DietPi. Both Pis show DietPi v10.8.2 available, which means they run Bookworm or newer. Both are waiting for one `install.sh` re-run that covers this feature. The agent runs as the unprivileged user `riot`.
- **DietPi facts (verified against DietPi source, master @ 65a12a6, 2026-10-22):**
  - `/boot/dietpi/.version` holds shell assignments for the installed version (`G_DIETPI_VERSION_CORE`, `_SUB`, `_RC`), the update source (`G_GITBRANCH`, `G_GITOWNER`) and live-patch status lines. It must be parsed as data and never executed.
  - `/run/dietpi/.update_available` contains one line `CORE.SUB.RC` when DietPi's check found an update. `/run` is tmpfs, so the file is absent after boot until a check runs. The check runs from the daily cron job and at boot, and only when `CONFIG_CHECK_DIETPI_UPDATES=1` in `/boot/dietpi.txt`.
  - `/run/dietpi/.live_patches` is an empty flag file. It means applicable live patches exist.
  - `/boot/dietpi.txt` also holds `CONFIG_CHECK_APT_UPDATES`. The value `2` makes DietPi's own cron job run `apt-get upgrade` daily, outside rIOt's control. `DEV_GITBRANCH` and `DEV_GITOWNER` set the update source.
  - DietPi has no auto-apply for its own updates. `CONFIG_AUTO_DIETPI_UPDATES` does not exist.
  - The apply command is exactly `/boot/dietpi/dietpi-update 1`. When stdin is not a terminal it runs non-interactively and every dialog takes its default.
  - During a run it stops all DietPi-managed services (for example CUPS or VPN software installed through dietpi-software) and restarts them at the end. It runs `apt-get update`, `apt-get upgrade` (which respects apt holds), patches and `apt-get -y autopurge`. The autopurge removes packages, which departs from rIOt's `--no-remove` policy. It sends DietPi's survey only if the host opted in to the survey.
  - DietPi's apt calls do not wait for the dpkg lock: a held lock makes the run fail at once. A second concurrent `dietpi-update` cancels itself with exit 1. Network, time-sync and free-space checks fail non-zero.
  - On an installed system it does not reboot by itself. A reboot is needed afterwards when `/lib/modules/$(uname -r)` is missing or `/run/reboot-required` exists. The 9 to 10 major update on a Raspberry Pi migrates the kernel and firmware stack and therefore needs a reboot.
  - Its runtime is not bounded by DietPi. On a Pi it plausibly takes 10 to 40 minutes.
  - It writes its own full log to `/var/lib/dietpi/logs/dietpi-update.log`.
- **Current rIOt state:**
  - The `os_update` command runs apt through the root `riot-maintenance` worker. The request has exactly 4 fields. Output is capped at 256 KiB, the worker budget is 30 minutes, the service backstop is 35 minutes and the command deadline is 40 minutes.
  - The scheduler (`checkAutoPatch`) fires only when apt reports pending updates (`PendingUpdates > 0`). A host whose only pending update is a DietPi update would never be patched automatically.
  - Update telemetry comes from the existing `updates` collector. A new collector name would need `collector.enabled` edits on every agent.
- **Patch badge today:** `computePatchBadge` raises reasons NY-1 to NY-10, and any reason makes the badge red "Needs you". The Fleet Overview counts "N hosts need you" from hosts whose badge kind is `needs_you`. vermont shows red "Needs you" with 0 OS updates and no reboot required, only because automatic patching is off for it (NY-1). vermont has unattended-upgrades installed, which rIOt cannot see. Ryan's rule: "it shouldn't say needs me unless there ARE updates."
- **EXPECT-001 (expected activity)** is unmerged and has no PR. It treats the `commands` table as the durable record of actions. This story therefore reports the extra-updater run through the existing command and event path and does not depend on EXPECT code.

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Ryan (operator) | Uses the dashboard, edits `/etc/riot/agent.yaml`, re-runs `install.sh` as root, runs doctor on the hosts. | Existing authenticated dashboard access. Root on each host. |
| rIOt Server | Stores telemetry, renders the patch badge, validates manual dispatches, runs the auto-patch scheduler. | Creates commands. Agent-reported facts can only narrow what it allows. |
| rIOt Agent | Detects extra updaters, reports them, runs `os_update`, reports results and events. | Runs as `riot`. Cannot choose the command, arguments or environment of any root operation. |
| Privileged maintenance path | The root worker and the installer-managed grants that run apt today. | Root. Runs only fixed operations. Gains at most the one exact extra-updater command, and only on hosts opted in root-side. |
| Installer (`scripts/install.sh`) | Asks the prompts, reads the agent config and writes the root-side opt-in. | Runs as root. |
| DietPi (`dietpi-update`) | Third-party updater on DietPi hosts. | Runs as root when rIOt invokes it. Its internal behaviour is outside rIOt's control. |
| Older agent / older helper | Released before this story, or `install.sh` not re-run. | Keep working (NFR-005). |

## 4. Functional Requirements

Terms used below:

- **Extra updater:** a platform updater outside the package manager. In this story the only one is DietPi, with identifier `dietpi`.
- **Opted in (agent side):** the identifier appears in `commands.allow_extra_updaters` in the agent config (FR-017).
- **Opted in (root side):** the identifier appears in the root-side opt-in that the installer wrote on its last successful root run (FR-019).
- **Enabled:** the updater is present, opted in on both sides, and `commands.allow_patching` is true.
- **Pending:** the updater's status is `update_available`.
- **Included:** the updater will be applied in a given run (FR-028).
- Group K defines its own terms (FR-068).

### A. Generic updater contract

- **FR-001:** The agent must handle every extra updater through one shared contract. Each updater supplies detection, status reporting and one fixed apply operation. DietPi must be the only implementation shipped in this story.
- **FR-002:** Each updater must have a fixed identifier that matches `^[a-z][a-z0-9_-]{0,31}$`. DietPi's identifier must be `dietpi`.
- **FR-003:** Detection must use only read-only local checks. It must not use the network, run the updater or any of its scripts, write any file, or need root.
- **FR-004:** For each present updater, the agent must report in updates telemetry:
  - the identifier;
  - status: `update_available`, `up_to_date` or `unknown`;
  - when status is `unknown`, a reason code from a fixed list (Section 7);
  - installed version, or empty when unknown;
  - available version, or empty when unknown;
  - pending item count (0 or more);
  - apply readiness: `enabled`, or a reason code from a fixed list (FR-024);
  - notices: zero or more codes from a fixed list (Section 7), with their bounded values.
- **FR-005:** An updater that is not present must produce no telemetry entry. Hosts without DietPi (hawaii, vermont) must report an empty extra-updater list.
- **FR-006:** The agent must never report `up_to_date` unless it has positive evidence that the updater's own check completed since the current boot and found nothing. In every other case where no update is known, the status must be `unknown` with a reason.
- **FR-007:** The extra-updater data must be collected by the existing `updates` collector. No new collector name may be required, and no agent config must need a `collector.enabled` change for this story.

### B. DietPi detection

- **FR-008:** DietPi must count as present if, and only if, `/boot/dietpi/.version` and `/boot/dietpi/dietpi-update` both exist as regular files.
- **FR-009:** The agent must parse `/boot/dietpi/.version` as data. It must never source, execute or pass the file to a shell. The installed version is `CORE.SUB.RC` from the three version keys. If any of the three is missing or not an integer, the installed version must be empty and the status must be `unknown` with reason `version_unreadable`.
- **FR-010:** The DietPi status must be decided by the table in Section 7 ("DietPi status decision"). In summary:
  - A valid `/run/dietpi/.update_available` whose version is newer than the installed version, or whose version cannot be parsed, means `update_available`.
  - A flag whose version is parsed and is not newer than the installed version must be ignored (treated as absent).
  - Without a usable flag: `up_to_date` only under FR-006 and only when `CONFIG_CHECK_DIETPI_UPDATES` is exactly `1`. Otherwise `unknown` with reason `check_disabled`, `config_unreadable` or `not_checked_since_boot`.
- **FR-011:** When `/run/dietpi/.live_patches` exists, the agent must add the notice `live_patches_available` and count it as one pending item. A host with live patches and no version update must report `update_available`.
- **FR-012:** When `CONFIG_CHECK_APT_UPDATES` is `2`, the agent must add the notice `dietpi_auto_apt_upgrade`.
- **FR-013:** When the update source (`DEV_GITBRANCH` / `DEV_GITOWNER`, or the branch and owner in `.version`) is not `master` / `MichaIng`, the agent must add the notice `non_default_update_source` with the branch and owner values. Each value must be at most 64 printable ASCII characters. Longer or non-printable values must be reported as `unreadable`.
- **FR-014:** When the available major version (CORE) is greater than the installed major version, the agent must add the notice `major_upgrade`.
- **FR-015:** Detection must read at most 64 KiB from each file and must finish within 1 second. If a limit is reached, or a file cannot be read, the affected facts must be reported as unknown, never as up to date.
- **FR-016:** On a DietPi host, the agent's `reboot_required` (PATCH-FLOW-001 FR-002) must also be true when the directory `/lib/modules/<running kernel release>` does not exist.

### C. Opt-in

- **FR-017:** The agent config must accept `commands.allow_extra_updaters`, a list of updater identifiers. The default is an empty list. Identifiers the agent does not implement must be ignored, with one warning log line per config load.
- **FR-018:** The agent must apply an updater only when it is enabled (present, opted in on both sides, `commands.allow_patching` true). The agent-side setting alone must not be enough to run any root updater.
- **FR-019:** The root-side opt-in must be written only by a root run of `install.sh`. It must be derived from the final agent config of that run, and the `riot` user must not be able to change it. The installer must record `dietpi` only when the list contains `dietpi` and DietPi is present on the host at install time.
- **FR-020:** The installer must read `commands.allow_extra_updaters` fail-closed. Only a YAML list of strings directly under the top-level `commands` mapping counts. An absent key, a commented line, a scalar value, a value under another section, an unparseable file or an unreadable file must all mean "no updaters".
- **FR-021:** The privileged path must be able to run only the exact command `/boot/dietpi/dietpi-update 1`. It must not run `dietpi-update` with any other argument and must not run any other DietPi program (for example `dietpi-software`, `dietpi-services` or `dietpi-set_software`) directly. If the design adds a sudoers line, it must be exactly one line, and only on hosts opted in root-side. Nothing else may be granted.
- **FR-022:** On a Linux fresh install where DietPi is present, the installer must ask `Allow rIOt to apply DietPi updates during OS patching?` with hint `[y/N]`, directly after the OS patching question. The default must be no. `-y` must answer yes. `--non-interactive` and a missing terminal must answer no. The question must not be asked on a reinstall or on a host without DietPi.
- **FR-023:** On a reinstall, the installer must not edit the preserved config, and the root-side opt-in must follow it (FR-019). After every successful installer run, `dietpi` must be opted in root-side if, and only if, the final config lists it and DietPi is present.
- **FR-024:** The agent must report apply readiness for each present updater as `enabled`, or with one of these reasons: `not_opted_in` (agent side), `root_side_missing` (installer not re-run, or the root side does not list it), `patching_disabled` (`commands.allow_patching` false), `unsupported_helper` (the installed privileged path cannot run extra updaters).

### D. Applying updates in a patch run

- **FR-025:** In an `os_update` run with mode `full` or `security`, each included updater must run after the apt steps have finished (upgrade, verification, post-run refresh) and after rIOt's managed holds have been restored.
  > Superseded for version 2 runs by DIETPI-REFRESH-001: the post-run refresh now follows the updater step, not the other way round.
- **FR-026:** No extra updater may run in mode `repair`.
- **FR-027:** No extra updater may run when the apt part of the run did not succeed, including failed hold restoration. The result must record the updater as skipped with reason `apt_not_successful`.
- **FR-028:** An updater must be included in a run if, and only if, at the start of its step: it is enabled, its freshly re-read status is `update_available`, and the run was not dispatched with extra updaters excluded (FR-050). Otherwise it must be recorded as skipped with a reason (`not_enabled`, `nothing_pending`, `status_unknown`, `excluded_by_operator`).
- **FR-029:** The updater must run with null input, no terminal and non-interactive mode, with a fixed environment and working directory. The agent and the server must not be able to supply any argument, environment value, path or option to it.
- **FR-030:** The DietPi step must have its own time limit. The ADD states the value, which must be between 30 and 45 minutes. On expiry the whole process tree must be terminated with a grace period, and the outcome must be `timed_out` with the note that DietPi's state is uncertain.
- **FR-031:** The DietPi step must not start when less time remains in the run's overall budget than a threshold stated in the ADD. It must then be recorded as skipped with reason `insufficient_time`.
- **FR-032:** The run's bounds (step limit, worker budget, service backstop, command deadline) must stay strictly nested so that each outer bound outlasts the inner ones. The ADD states every value. On hosts with no enabled updater, the apt run's own limits and behaviour must not change.
- **FR-033:** Before starting the DietPi step, rIOt must check whether another process holds the dpkg lock. If it does, rIOt must wait at most 5 minutes. If the lock is still held, or its state cannot be determined, the step must be skipped with reason `dpkg_locked`.
- **FR-034:** When `dietpi-update` exits because of a concurrent DietPi run, or fails any of its own checks (network, time, space, read-only root), the outcome must be `failed` with the exit status and the last lines of output. The step must not be retried within the run.
- **FR-035:** Each updater must run at most once per command. It must not run again on command redelivery, agent restart or recovery of an interrupted run.
- **FR-036:** The updater's output must be captured and appended to the run output in its own labelled section. The DietPi section must not exceed 128 KiB, and the whole run output must stay within the existing 256 KiB cap. When output is cut, the end of the output must be kept and the cut must be marked. The result must name DietPi's own log path, `/var/lib/dietpi/logs/dietpi-update.log`.
- **FR-037:** The result must state each updater's outcome: `success`, `failed`, `timed_out`, `interrupted` or `skipped` (with reason), plus its exit status when it ran.
- **FR-038:** The command's overall outcome must be success only when the apt part succeeded and every included updater succeeded. When the apt part succeeded and an included updater did not succeed (failed, timed out, interrupted, or skipped for `dpkg_locked` or `insufficient_time`), the overall outcome must be `failed`. The first line of the summary must state both parts, for example `apt: succeeded; dietpi: failed (exit 1)`. The apt part's own results (version changes, hold restoration) must still be reported unchanged.
- **FR-039:** An updater skipped for `not_enabled`, `nothing_pending`, `status_unknown` or `excluded_by_operator` must not change the overall outcome.
- **FR-040:** rIOt must never start a reboot because of, or from inside, the updater step. If the host reboots during the step, the outcome must be `interrupted`.
- **FR-041:** After all steps, the agent must evaluate `reboot_required` (including FR-016) once for the whole command. The existing reboot decision must then apply once:
  - a manual run with `reboot_if_required: true` reboots when a reboot is required;
  - a scheduled run treats "an included updater succeeded and a reboot is now required" as a reboot-class change under the policy's existing reboot rules (DEC-008).

  Every reboot needs `commands.allow_reboot: true`, waits for the dpkg lock under REBOOT-DPKG-001, is reported before it starts, and happens at most once per command.
- **FR-042:** The result must state, for each updater that ran, whether a reboot was required after it.
- **FR-043:** The fresh updates telemetry sent after the run (PATCH-FLOW-001 FR-046) must include the extra-updater state re-read after the run.

### E. Activity reporting (EXPECT-001 readiness)

- **FR-044:** The updater step must report progress through the existing `os_update` command progress steps: one step when it starts and one when it ends, naming the updater and its outcome.
- **FR-045:** The agent must emit one agent event when an updater step starts and one when it ends. Each event must carry the command ID, the updater identifier, the command origin (`user` or `auto_patch`) and, for the end event, the outcome. At most two such events may be emitted per updater per command.
- **FR-046:** This story must not depend on any EXPECT-001 code or schema. *Amended 2026-10-26, after EXPECT-001 (PR #37) merged first:* the extra-updater code must not build an expected-activity mechanism of its own. The os_update command's EXPECT-001 window derives from the command's stamped execution deadline, so a run with the extra-updater budget is covered for its 95-minute deadline.

### F. Server and scheduler

- **FR-047:** The server must accept the new telemetry fields from new agents and accept telemetry without them from older agents, with a 2xx response and no warning. It must keep at most 8 updater entries per host and drop the rest. It must treat an unknown status, reason, readiness or notice value as `unknown`, and must cut string values to their Section 7 bounds.
- **FR-048:** The auto-patch trigger must also fire when at least one updater entry has status `update_available` and readiness `enabled`, even when apt reports no pending updates. All other conditions of the trigger (policy, window, mode, existing guards) must not change.
- **FR-049:** An updater entry whose status is `unknown`, or whose readiness is not `enabled`, must never cause the auto-patch trigger to fire.
- **FR-050:** A manual `os_update` must accept an optional boolean parameter that excludes extra updaters from that run. Absent means included. A non-boolean value must be rejected with HTTP 400 `invalid_request`, and no command is created.
- **FR-051:** For each updater entry with status `update_available`, the server-rendered patch badge explanation must show the updater's name, the installed and available versions, and how it will be applied: automatically in the patch window, only by manual Patch, or not at all (with the readiness reason in words and what Ryan must do). Whether the badge is "Needs you" follows group K.
- **FR-052:** For an updater with status `unknown`, the badge explanation must show an information item naming the updater and the reason in words. It must never be an action item (group K), and the badge must not say the updater is up to date.
- **FR-053:** The Fleet Overview Pending OS Patches panel must list a host whose only pending update is an extra-updater update, and its row must name the updater (for example "DietPi 10.8.2").

### G. Dashboard

- **FR-054:** The device page's updates and patch view must show each reported updater next to the apt updates, with its name, installed version, available version, status, the unknown reason in words, and each notice in words.
- **FR-055:** The Patch action must be available when apt has pending updates, or when an updater is pending and enabled. When an updater is pending and enabled, the Patch button's label must name it (for example "Patch (3 + DietPi)" or "Patch (DietPi)").
- **FR-056:** When DietPi is pending and enabled, the manual Patch confirmation must:
  - show a checkbox labelled "Apply DietPi update in this run", ticked each time the dialog opens; unticking it sends the exclusion parameter (FR-050);
  - state the version change, that DietPi runs after apt, and how long it may take (the FR-030 limit);
  - disclose that DietPi stops and restarts its managed services during the run (for example CUPS or VPN software), and runs `apt-get autopurge`, which removes packages apt considers no longer needed.
- **FR-057:** When the `major_upgrade` notice is present, the dialog must also state that the kernel or firmware may change and a reboot is likely to be needed, and must point to the existing "Reboot automatically if the patch requires it" checkbox.
- **FR-058:** When DietPi is pending and not enabled, or its status is `unknown`, the dialog must state that DietPi will not be applied in this run and why.
- **FR-059:** The command result view must show each updater's outcome line (FR-037) and the overall outcome (FR-038).
- **FR-060:** Demo mode must include at least one host with a pending DietPi update and one with status `unknown`.

### H. Doctor

- **FR-061:** When `commands.allow_extra_updaters` lists `dietpi`, `riot-agent doctor` must report whether DietPi is present, whether the root-side opt-in lists it, and whether the privileged path can run it. Each check prints a pass line, or a fail line with guidance (set the list, re-run `install.sh` as root). Doctor must never run `dietpi-update` or any updater.
- **FR-062:** Doctor must print one warning line for each identifier in the list that the agent does not implement, and must report `dietpi` as not applicable on a host without DietPi.

### I. Documentation

- **FR-063:** `docs/ops/os-maintenance.md` and the README must describe: the extra-updater step and its order after apt; the modes it runs in; the opt-in on both sides and how to revoke it; the time limit, output cap and truthful outcomes; the reboot rule; the meaning of `unknown`; DietPi's service stop and restart, autopurge and survey behaviour; the `CONFIG_CHECK_APT_UPDATES=2` caveat; and the group K badge rule (when a host shows "Needs you", and that settings alone show as information).
- **FR-064:** The README must give Ryan numbered, plain steps per server, with commands run on that server:
  - **hawaii** and **vermont:** no action for this feature (no DietPi).
  - **PiVPN** and **PiCUPS:** check that `CONFIG_CHECK_DIETPI_UPDATES=1` in `/boot/dietpi.txt`; add `dietpi` to `commands.allow_extra_updaters` in `/etc/riot/agent.yaml` (and keep or set `allow_patching`, plus `allow_reboot` and `allow_shutdown` as Ryan wants); then run the matching release's `install.sh` once as root with the existing server URL and without `--bootstrap-key`; then run doctor.
  - The steps must say that this single re-run also applies the pending installer changes from AGENT-HARDEN-001, PATCH-FLOW-001 and SHUTDOWN-SUDO-001, and that the first DietPi 9 to 10 update on a Pi will need a reboot.
- **FR-065:** The CHANGELOG must have an entry naming the feature, the config key, the installer question, any new privilege and the badge rule change.

### J. Unchanged behaviour

- **FR-066:** On hosts with no enabled updater, apt patching, the worker request and record, the sudoers drop-in, outputs and bounds must not change, apart from the empty extra-updater list in telemetry.
- **FR-067:** dnf, macOS and Windows hosts must not change, apart from the badge rules in group K.

### K. "Needs you" only when work is waiting (amends PATCH-BADGE-001 NY-1 to NY-9)

- **FR-068:** The badge rules below use these terms:
  - **Apt pending:** package lists are not `stale` and `pending_updates` is greater than 0.
  - **Extra-updater pending:** an updater entry has status `update_available`.
  - **Reboot required:** `reboot_required` is true (PATCH-FLOW-001 FR-001).
  - **Waiting work:** apt pending, or any extra-updater pending, or reboot required.
  - **Automatic patching set up:** the existing condition: Auto-patch is on for the host, the server-wide OS-patch setting is usable, the agent does not refuse patching, and the package manager is supported.
  - **Unapplied updates:** apt pending while automatic patching is not set up; or an extra-updater pending entry where automatic patching is not set up or the entry's readiness is not `enabled`.
  - **Action item:** a reason shown under "Needs you". **Information item:** a reason shown as neutral information.
- **FR-069:** The badge kind must be `needs_you` (label "Needs you") if, and only if, at least one action item exists.
- **FR-070:** NY-1 (`auto_patch_off`), NY-2 (`patching_disabled_server`) and NY-3 (`agent_patching_not_allowed`) must be action items only when unapplied updates exist and their condition holds. When their condition holds and no unapplied updates exist, they must be information items.
- **FR-071:** The server must add one reason, `extra_update_not_applied`, for each extra-updater pending entry whose readiness is not `enabled`. It must be an action item. Its text must name the updater, the version change and the readiness reason in words. Its action must say what Ryan does (for example "Add dietpi to commands.allow_extra_updaters and re-run install.sh as root on this host").
- **FR-072:** NY-4 (`cannot_patch_host`) must be an action item only when apt pending holds for the host. Otherwise, including when no patch data has been reported, it must be an information item.
- **FR-073:** NY-5 (`held_packages`) must be an action item only when the telemetry shows that at least one of the listed held packages has a newer version available. When it shows none, or cannot tell, NY-5 must be an information item.
- **FR-074:** NY-6 (`reboot_class_held_no_release`) must be an action item only when the lists are not `stale` and `pending_reboot_class_count` is greater than 0. Otherwise it must be an information item.
- **FR-075:** NY-7 (`hold_enforcement_broken`) must always be an information item.
- **FR-076:** NY-8 (`manual_approval_update`) must stay an action item. An available OS release upgrade is waiting work.
- **FR-077:** NY-9 (`last_run_failed`) must be an action item only when waiting work exists. Otherwise it must be an information item whose text also says that nothing is pending now. It must keep its link to the failed command in both cases.
- **FR-078:** NY-10 (`manual_reboot_needed`) must not change. It stays an action item exactly as PATCH-FLOW-001 FR-003 to FR-006 define.
- **FR-079:** Information items must be carried in the badge explanation separately from action items. Each must keep its reason code, text and action. They must be shown in the badge tooltip and details in neutral styling, under a heading that is not "Needs you". Each reason code must appear at most once, either as an action item or as an information item, never both.
- **FR-080:** The "Needs you" summary line must count action items only. With no action items it must read "Nothing.", whatever the information items.
- **FR-081:** When no action item exists and automatic patching is not set up, the badge kind must be `manual` with the label "Manual", shown in neutral styling (not red). When no action item exists and automatic patching is set up, the existing kinds (`auto`, `auto_reboot_date`, `auto_reboot_unknown`) must apply unchanged.
- **FR-082:** The Fleet Overview "N hosts need you" count, and the host filter it opens, must include only hosts whose badge kind is `needs_you`. Hosts with only information items must not be counted.
- **FR-083:** Every dashboard place that reacts to the badge kind (the badge component, the Fleet Overview and the patch-flow helpers) must treat `manual` as not needing action.
- **FR-084:** The badge must stay display-only. Group K must not change `checkAutoPatch`, any dispatch decision, the Automatic line, the Reboot line or the As-of line.

## 5. Non-Functional Requirements

- **NFR-001 [Security, least privilege]:** Opting in adds the ability to run exactly `/boot/dietpi/dietpi-update 1` as root, and only on hosts opted in root-side. The opt-in is enforced root-side at install time. A compromised agent cannot add an updater, an argument, an environment value or a path.
- **NFR-002 [Bounded growth]:** Every new stored or logged item has a bound:
  - Telemetry: at most 8 updater entries per host. Each string is at most 64 characters. At most 8 notices per entry. Enum fields have fixed values. No new table and no new retained record type. Telemetry keeps its existing retention.
  - Command records: the extra-updater output is part of the existing output, at most 128 KiB for DietPi and 256 KiB per run. Command records keep their existing retention.
  - Events: at most 2 per updater per command (FR-045).
  - Agent log: at most one warning line per config load (FR-017) and one line per updater step.
  - Runs: at most one updater invocation per command (FR-035). At most one scheduled run per policy window, as today, so a persistently failing DietPi update creates at most one failed command per window.
  - Root side: the root-side opt-in is one small fixed-size record, rewritten (not appended) on each installer run. rIOt creates no new log file. DietPi's own log is managed by DietPi.
  - Badge: computed per request and never stored. At most one item per reason code (FR-079), so at most 11 reason codes plus one `extra_update_not_applied` item per updater entry (at most 8). Existing per-reason text bounds (10 listed package names, 200-character run message) still apply.
  - No retry loop on any failure path (FR-034).
- **NFR-003 [Performance]:** Detection finishes within 1 second and reads at most 64 KiB per file (FR-015). The dashboard additions use data the device page and Fleet Overview already load, with no extra request.
- **NFR-004 [Availability, disclosed impact]:** During a DietPi run, DietPi-managed services on that host are stopped until the run ends. The run time is bounded by FR-030. The dialog and docs disclose this (FR-056, FR-063).
- **NFR-005 [Compatibility]:**
  - New agent with an old helper: readiness `unsupported_helper` or `root_side_missing`; nothing is applied; apt patching works as today.
  - Old agent with a new server: no extra-updater entries; the scheduler behaves as today; the badge follows group K using apt data only, and NY-5 is an information item when the agent cannot report updates for held packages.
  - New agent with an old server: the server ignores the unknown fields.
  - No upgrade order is required. The badge rules ship with the server and the dashboard in the same release.
- **NFR-006 [Extensibility]:** The contract (FR-001 to FR-004) must, without a redesign, support an updater that:
  - has several pending items, each named (for example snap or flatpak); the contract must carry a pending item count and allow a bounded list of item names, at most 50 names of at most 64 characters each;
  - has no single version number (installed and available versions empty);
  - has its own time limit and output cap;
  - is opted in with the same `commands.allow_extra_updaters` list and the same root-side mechanism;
  - runs after apt in a fixed, documented order with the other updaters;
  - counts as extra-updater pending in group K without a badge redesign.

  Snap and flatpak are not deliverables of this story.
- **NFR-007 [Testability]:** Detection must be verifiable against fixture directories. The apply step must be verifiable with an injected command runner. Installer behaviour must be verifiable in a disposable container fixture. Badge rules must be verifiable as a pure function of its inputs.
- **NFR-008 [Auditability]:** The command record shows the origin, the included updaters, each updater's outcome and exit status, the reboot-required result and the overall outcome. The command-sent event carries the exclusion parameter.

### Hard safety constraints (binding on every pipeline agent, test and helper)

- **SC-001:** No test, helper, fixture or pipeline agent may run `dietpi-update` (any argument), `dietpi-services`, `dietpi-software`, a mutating `apt`, `apt-get` or `dpkg` command, `reboot`, `shutdown`, `poweroff` or `halt`, or any equivalent, on any host, including Ryan's PC, hawaii, vermont, PiVPN, PiCUPS and CI runners. Tests use fakes and injected runners only.
- **SC-002:** No pipeline agent may run an installed `riot-agent` binary on any host, including `doctor`.
- **SC-003:** The real `install.sh` may run only inside a disposable container fixture.
- **SC-004:** Detection tests must read fixture directories only. They must not read the real `/boot` or `/run` of the test machine as the system under test, and must make no network request.
- **SC-005:** Nothing may be deleted on hawaii.

## 6. Business Rules

- **BR-001:** Applying an extra updater is opt-in per device and per updater, and off by default.
- **BR-002:** The root side decides. The agent-side setting can only narrow it.
- **BR-003:** Extra updaters run after apt, and never before it, in a fixed order.
- **BR-004:** A status of `unknown` is never shown or treated as "up to date", and never causes an automatic run.
- **BR-005:** A run's result never hides a partial failure. Apt success with an updater failure is not success.
- **BR-006:** Reboots happen only through the existing reboot gate, at most once per command.
- **BR-007:** rIOt does not change DietPi's own settings (`dietpi.txt`) and does not trigger DietPi's own check.
- **BR-008:** Every failure path has a stated bound (NFR-002).
- **BR-009 (Ryan's rule):** A host shows "Needs you" only when something is actually waiting (updates, an OS release upgrade or a required reboot) and nothing will apply it. A setting by itself is information, not an action item.

## 7. Data Requirements

### Entities

- **Extra-updater entry (in updates telemetry, additive, omitted when empty):**

| Field | Values and bound |
|-------|------------------|
| identifier | `^[a-z][a-z0-9_-]{0,31}$` |
| status | `update_available` \| `up_to_date` \| `unknown` |
| unknown reason | `check_disabled` \| `config_unreadable` \| `not_checked_since_boot` \| `version_unreadable` \| `detection_failed` |
| installed version | at most 32 characters, may be empty |
| available version | at most 32 characters, may be empty |
| pending item count | integer, 0 to 10,000 |
| pending item names | at most 50 names, each at most 64 characters (unused by DietPi) |
| readiness | `enabled` \| `not_opted_in` \| `root_side_missing` \| `patching_disabled` \| `unsupported_helper` |
| notices | at most 8, from `live_patches_available`, `dietpi_auto_apt_upgrade`, `non_default_update_source` (with branch and owner, each at most 64 characters), `major_upgrade` |

- **`os_update` parameters:** gain an optional boolean that excludes extra updaters (absent = included).
- **Command result:** gains one outcome line per updater (FR-037) within the existing result and output limits.
- **Agent config:** `commands.allow_extra_updaters`, a list of identifiers, default empty.
- **Root-side opt-in:** the list of identifiers recorded by the last successful root installer run. Not writable by `riot`.
- **Patch badge (server-rendered, additive):** gains the kind `manual`, the reason code `extra_update_not_applied`, and a separate list of information items (code, text, action, optional command link). The needs-you list holds action items only.

### DietPi status decision

| # | Usable update flag (FR-010) | Live patches flag | `CONFIG_CHECK_DIETPI_UPDATES` | Check evidence since boot (FR-006) | Status | Reason / notice |
|---|---|---|---|---|---|---|
| D1 | yes | any | any | any | `update_available` | `live_patches_available` if flag set |
| D2 | no | yes | any | any | `update_available` | `live_patches_available` |
| D3 | no | no | `1` | yes | `up_to_date` | none |
| D4 | no | no | `1` | no | `unknown` | `not_checked_since_boot` |
| D5 | no | no | not `1` | any | `unknown` | `check_disabled` |
| D6 | no | no | file unreadable | any | `unknown` | `config_unreadable` |
| D7 | `.version` invalid | any | any | any | `unknown` | `version_unreadable` |

### Run decision for one updater

| Condition at the start of the updater step (checked in this order) | Result |
|---|---|
| Mode is `repair` | Not run, no line (FR-026) |
| Apt part not successful | Skipped `apt_not_successful` |
| Excluded by the operator | Skipped `excluded_by_operator` |
| Not enabled | Skipped `not_enabled` |
| Status `unknown` | Skipped `status_unknown` |
| Status `up_to_date` | Skipped `nothing_pending` |
| Not enough time left | Skipped `insufficient_time` (included; overall `failed`) |
| dpkg lock held after 5 minutes | Skipped `dpkg_locked` (included; overall `failed`) |
| Otherwise | Run once: `success`, `failed`, `timed_out` or `interrupted` |

### Badge reason classification (group K)

| Rule | Code | Action item when | Otherwise (condition holds) |
|---|---|---|---|
| NY-1 | `auto_patch_off` | unapplied updates exist | information |
| NY-2 | `patching_disabled_server` | unapplied updates exist | information |
| NY-3 | `agent_patching_not_allowed` | unapplied updates exist | information |
| NY-4 | `cannot_patch_host` | apt pending | information |
| NY-5 | `held_packages` | a listed held package has a newer version available | information |
| NY-6 | `reboot_class_held_no_release` | lists not stale and `pending_reboot_class_count` > 0 | information |
| NY-7 | `hold_enforcement_broken` | never | information |
| NY-8 | `manual_approval_update` | always | n/a |
| NY-9 | `last_run_failed` | waiting work exists | information, "nothing is pending now" |
| NY-10 | `manual_reboot_needed` | unchanged (PATCH-FLOW-001) | n/a |
| new | `extra_update_not_applied` | extra-updater pending and readiness not `enabled` | n/a |

### Badge kind decision

| Action items | Automatic patching set up | Kind | Label |
|---|---|---|---|
| 1 or more | any | `needs_you` | Needs you |
| 0 | no | `manual` | Manual |
| 0 | yes | `auto` / `auto_reboot_date` / `auto_reboot_unknown` (unchanged rules) | unchanged |

## 8. Acceptance Criteria

Detection criteria use fixture directories (SC-004). Apply criteria use an injected runner (SC-001). Installer criteria use a disposable container (SC-003). Badge criteria call the badge computation directly with constructed inputs.

### AC-001: DietPi presence [Maps to FR-005, FR-008]
- **Given** fixtures with both `/boot/dietpi/.version` and `/boot/dietpi/dietpi-update`, with only one of them, and with neither
- **When** the updates collector runs
- **Then** only the first fixture reports a `dietpi` entry
- **And** the others report an empty extra-updater list

### AC-002: Version file is parsed as data [Maps to FR-003, FR-009]
- **Given** a `.version` fixture with core 9, sub 20, rc 1, plus a line containing shell command syntax
- **When** detection runs with an injected runner that records every process start
- **Then** the installed version is `9.20.1`
- **And** no process was started, and no file was written

### AC-003: Invalid version file [Maps to FR-009]
- **Given** `.version` fixtures with a missing key, and with a non-integer core
- **When** detection runs
- **Then** the installed version is empty and the status is `unknown` with reason `version_unreadable`

### AC-004: Update available [Maps to FR-004, FR-010, FR-014]
- **Given** installed `9.20.1` and an update flag containing `10.8.2`
- **When** detection runs
- **Then** the status is `update_available`, the available version is `10.8.2`, the pending count is 1
- **And** the notice `major_upgrade` is present

### AC-005: Stale or unparseable flag [Maps to FR-010]
- **Given** installed `10.8.2` with a flag containing `10.8.2`, and separately installed `9.20.1` with a flag containing `garbage`
- **When** detection runs
- **Then** the first fixture ignores the flag and is decided by rows D3 to D6
- **And** the second reports `update_available` with an empty available version

### AC-006: Never claims up to date without evidence [Maps to FR-006, FR-010]
- **Given** no update flag, and fixtures matching rows D3, D4, D5 and D6
- **When** detection runs
- **Then** the statuses are `up_to_date`, `unknown`/`not_checked_since_boot`, `unknown`/`check_disabled` and `unknown`/`config_unreadable`

### AC-007: Live patches only [Maps to FR-011]
- **Given** no usable update flag and an existing `/run/dietpi/.live_patches`
- **When** detection runs
- **Then** the status is `update_available`, the pending count is 1, and `live_patches_available` is present

### AC-008: DietPi settings notices [Maps to FR-012, FR-013]
- **Given** `CONFIG_CHECK_APT_UPDATES=2`, and separately `DEV_GITBRANCH=dev`, and separately a 200-character owner value
- **When** detection runs
- **Then** the first reports `dietpi_auto_apt_upgrade`, the second reports `non_default_update_source` with branch `dev`, and the third reports the owner as `unreadable`

### AC-009: Detection bounds [Maps to FR-015, NFR-003]
- **Given** a 1 MiB `dietpi.txt` fixture and a reader that blocks
- **When** detection runs
- **Then** at most 64 KiB is read from each file, detection returns within 1 second, and the affected facts are unknown, never `up_to_date`

### AC-010: Missing kernel modules mean reboot required [Maps to FR-016]
- **Given** a DietPi fixture where `/lib/modules/<running release>` does not exist and no reboot-required marker exists
- **When** the updates collector runs
- **Then** `reboot_required` is true
- **And** when the directory exists, `reboot_required` follows PATCH-FLOW-001 FR-002 unchanged

### AC-011: No new collector name [Maps to FR-007]
- **Given** an agent config whose `collector.enabled` matches today's list
- **When** the agent runs on a DietPi fixture
- **Then** the `dietpi` entry is reported without any config change

### AC-012: Agent-side opt-in alone does not run DietPi [Maps to FR-018, FR-024, NFR-001]
- **Given** `allow_extra_updaters: [dietpi]` and `allow_patching: true`, with no root-side opt-in
- **When** an `os_update` full run executes with a pending DietPi update
- **Then** readiness is `root_side_missing`, the injected runner receives no DietPi call, and the result shows `dietpi: skipped (not_enabled)`

### AC-013: Readiness reasons [Maps to FR-024]
- **Given** a DietPi host with: not listed; listed without root side; listed with root side and `allow_patching: false`; listed with an older helper; listed with everything in place
- **When** the agent reports telemetry
- **Then** readiness is `not_opted_in`, `root_side_missing`, `patching_disabled`, `unsupported_helper` and `enabled` respectively

### AC-014: Unknown identifiers are ignored [Maps to FR-017, FR-062]
- **Given** `allow_extra_updaters: [dietpi, snap]`
- **When** the agent loads the config and doctor runs
- **Then** the agent logs one warning naming `snap`, and doctor prints one warning line for `snap`

### AC-015: Installer fresh-install question [Maps to FR-022]
- **Given** Linux fresh installs on a DietPi fixture and on a non-DietPi fixture
- **When** each runs interactively, with `-y`, and with `--non-interactive`
- **Then** only the DietPi fixture shows `Allow rIOt to apply DietPi updates during OS patching? [y/N]`, directly after the OS patching question
- **And** `y` and `-y` record `dietpi` on both sides, while an empty answer and `--non-interactive` record nothing

### AC-016: Reinstall follows the preserved config, fail-closed [Maps to FR-019, FR-020, FR-023]
- **Given** reinstalls on a DietPi fixture whose preserved config has: `[dietpi]`; no key; a commented line; a scalar `dietpi`; the list under `docker:`; unparseable YAML
- **When** each reinstall runs
- **Then** only the first records `dietpi` root-side
- **And** no question is asked, and the preserved config's bytes, owner and mode are unchanged

### AC-017: Root side respects host and ownership [Maps to FR-019]
- **Given** a non-DietPi fixture whose config lists `dietpi`, and a DietPi fixture after an opt-in install
- **When** the installer completes
- **Then** the first records nothing root-side
- **And** in the second, the root-side record is not writable by `riot`

### AC-018: Revocation [Maps to FR-023]
- **Given** a DietPi fixture opted in on both sides
- **When** Ryan removes `dietpi` from the list and re-runs the installer
- **Then** the root side no longer lists `dietpi`, and readiness is `not_opted_in`

### AC-019: Exact command only [Maps to FR-021, FR-029, NFR-001]
- **Given** an enabled, pending DietPi host
- **When** the step runs with an injected runner
- **Then** the runner receives exactly `/boot/dietpi/dietpi-update` with the single argument `1`, null input, no terminal and the fixed environment
- **And** the policy listing (listing only) refuses `dietpi-update` with any other argument and refuses `dietpi-software`, `dietpi-services` and `dietpi-set_software`
- **And** no request, command parameter or config value can change the argv, environment or path

### AC-020: Order after apt and hold restore [Maps to FR-025]
- **Given** a full run on an enabled, pending DietPi host
- **When** the run's steps are recorded
- **Then** the DietPi call comes after upgrade, verification, post-run refresh and hold restoration

> Superseded for version 2 runs by DIETPI-REFRESH-001: the post-run refresh now follows the DietPi call, so it is no longer among the steps that come before it.

### AC-021: Never in repair, never after an apt failure [Maps to FR-026, FR-027]
- **Given** an enabled, pending DietPi host
- **When** a `repair` run executes, and separately a full run whose upgrade fails, and separately one whose hold restoration fails
- **Then** the runner receives no DietPi call in any of them
- **And** the second and third results show `dietpi: skipped (apt_not_successful)`

### AC-022: Inclusion rules [Maps to FR-028, FR-039]
- **Given** a successful apt part, with DietPi excluded by the operator, not enabled, `unknown`, and `up_to_date` in separate runs
- **When** each run ends
- **Then** no DietPi call is made, the result shows the matching skip reason, and the overall outcome is success

### AC-023: Time limit [Maps to FR-030]
- **Given** an injected runner that never returns
- **When** the DietPi step runs
- **Then** the process tree is terminated at the stated limit plus grace, the outcome is `timed_out` with the uncertain-state note, and the overall outcome is `failed`

### AC-024: Not enough time left [Maps to FR-031, FR-038]
- **Given** an apt part that finishes with less than the stated threshold remaining
- **When** the run reaches the DietPi step
- **Then** no DietPi call is made, the result shows `dietpi: skipped (insufficient_time)`, and the overall outcome is `failed`

### AC-025: Nested bounds [Maps to FR-032, FR-066]
- **Given** the ADD's stated values and the shipped constants
- **When** they are compared in a test
- **Then** step limit < worker budget < service backstop < command deadline
- **And** on a host with no enabled updater, the run's limits and recorded steps equal the pre-story values

### AC-026: dpkg lock wait [Maps to FR-033]
- **Given** an injected lock probe that reports the lock held for 2 minutes, and separately for longer than 5 minutes, and separately as unknown
- **When** the DietPi step is reached
- **Then** the first runs DietPi after the lock is free; the second and third skip with `dpkg_locked` after at most 5 minutes, and the overall outcome is `failed`

### AC-027: DietPi's own failures are truthful, not retried [Maps to FR-034, FR-037]
- **Given** injected exits for concurrent execution (exit 1), no network and a read-only root
- **When** each run ends
- **Then** each shows `dietpi: failed` with the exit status and the last output lines
- **And** the runner received exactly one DietPi call per run

### AC-028: Apt success with DietPi failure is not success [Maps to FR-038, BR-005]
- **Given** an apt part that succeeded with version changes, and a DietPi step that exits non-zero
- **When** the result is reported
- **Then** the overall outcome is `failed`, the first summary line is `apt: succeeded; dietpi: failed (exit <n>)`, and the apt version changes and hold restoration are reported unchanged

### AC-029: At most once per command [Maps to FR-035, NFR-002]
- **Given** a run whose DietPi step has started
- **When** the command is redelivered and the agent restarts and recovers the run
- **Then** the runner receives no second DietPi call for that command

### AC-030: Output cap [Maps to FR-036, NFR-002]
- **Given** an injected DietPi output of 1 MiB ending with a known error line
- **When** the result is stored
- **Then** the DietPi section is at most 128 KiB, the whole output is at most 256 KiB, the cut is marked, the known error line is kept, and the result names `/var/lib/dietpi/logs/dietpi-update.log`

### AC-031: No reboot from the updater step [Maps to FR-040, BR-006]
- **Given** a successful DietPi step that leaves `/lib/modules/<running release>` missing, in a manual run without `reboot_if_required`
- **When** the run ends
- **Then** no reboot is requested, the result says a reboot is required after DietPi, and `reboot_required` is true in the next telemetry

### AC-032: Reboot through the gate, once [Maps to FR-041, FR-042]
- **Given** the AC-031 state, in a manual run with `reboot_if_required: true` and `allow_reboot: true`, and separately in a scheduled run whose policy permits reboot-class reboots
- **When** each run ends
- **Then** the result is reported first, the dpkg-lock wait runs, and exactly one reboot is requested
- **And** with `allow_reboot: false`, no reboot is requested in either case

### AC-033: Fresh state after the run [Maps to FR-043]
- **Given** a successful DietPi run that raised the installed version to the flag's version
- **When** the post-run telemetry is sent
- **Then** the `dietpi` entry no longer reports that version as pending

### AC-034: Progress steps and events [Maps to FR-044, FR-045, FR-046]
- **Given** a scheduled run and a manual run that include DietPi
- **When** each ends
- **Then** the command shows a start step and an end step for DietPi
- **And** exactly two agent events were emitted per run, carrying the command ID, `dietpi`, the origin (`auto_patch` or `user`) and, on the end event, the outcome
- **And** the extra-updater code contains no expected-activity mechanism of its own, and the EXPECT-001 window of an os_update stamped with the extra-updater budget uses its 95-minute deadline (amended 2026-10-26, see FR-046)

### AC-035: Server accepts and bounds telemetry [Maps to FR-047, NFR-005]
- **Given** telemetry from an older agent, telemetry with 12 updater entries, and an entry with status `bogus` and a 500-character version
- **When** the server receives each
- **Then** each gets a 2xx response with no warning logged; 8 entries are kept; `bogus` is stored as `unknown`; the version is cut to 32 characters

### AC-036: Auto-patch fires for an extra-updater-only host [Maps to FR-048, FR-049]
- **Given** an auto-patch policy in its window and a host with 0 apt pending updates
- **When** the scheduler evaluates the host with `dietpi` `update_available`/`enabled`; then `unknown`/`enabled`; then `update_available`/`root_side_missing`
- **Then** only the first creates an `os_update` command with origin `auto_patch`
- **And** a host with apt pending updates behaves as before

### AC-037: Exclusion parameter validation [Maps to FR-050]
- **Given** manual `os_update` requests with the exclusion parameter `true`, absent, and `"yes"`
- **When** the server handles them
- **Then** the first two create commands carrying the value (or its absence), and the third returns HTTP 400 `invalid_request` with no command created

### AC-038: Badge explanation and fleet panel for extra updaters [Maps to FR-051, FR-052, FR-053]
- **Given** hosts with: only DietPi pending and enabled under a set-up auto-patch policy; only DietPi pending and `not_opted_in`; DietPi `unknown`/`check_disabled` and no apt updates
- **When** the server renders the badges and the Fleet Overview panel
- **Then** the first two show both versions and say "automatically in the patch window" and "not applied: not opted in" (with what Ryan must do) respectively
- **And** the third shows an information item with the reason in words, has no action item for DietPi, and never says up to date
- **And** the panel lists the first two hosts with "DietPi 10.8.2"

### AC-039: Device view and Patch button [Maps to FR-054, FR-055]
- **Given** a host with 3 apt updates and DietPi pending and enabled, and a host with only DietPi pending and enabled
- **When** Ryan opens each device page
- **Then** DietPi is shown next to apt with installed version, available version, status and notices in words
- **And** the Patch buttons read "Patch (3 + DietPi)" and "Patch (DietPi)" and are enabled

### AC-040: Confirm dialog disclosure and exclusion [Maps to FR-056, FR-057, FR-058]
- **Given** a host with DietPi `9.20.1` to `10.8.2` pending and enabled
- **When** Ryan opens the Patch dialog, unticks "Apply DietPi update in this run", closes it and opens it again
- **Then** the box is ticked on each opening; the dialog states the version change, the order after apt, the time limit, the service stop and restart, the autopurge and the likely reboot pointing to the reboot checkbox
- **And** confirming with the box unticked sends the exclusion parameter
- **And** for a host with DietPi `unknown` or not enabled, the dialog states DietPi will not be applied and why

### AC-041: Result view and demo data [Maps to FR-059, FR-060]
- **Given** the AC-028 command and demo mode
- **When** Ryan views the command result and the demo fleet
- **Then** the result view shows the apt and DietPi outcome lines and the overall `failed`
- **And** demo mode has a DietPi-pending host and a DietPi-`unknown` host

### AC-042: Doctor [Maps to FR-061, FR-062, SC-002]
- **Given** `allow_extra_updaters: [dietpi]` with injected results: all in place; root side missing; non-DietPi host
- **When** doctor runs
- **Then** it prints pass lines, a fail line with the re-run guidance, and "not applicable" respectively
- **And** the injected runner never receives `dietpi-update`

### AC-043: Documentation [Maps to FR-063, FR-064, FR-065]
- **Given** the README, `docs/ops/os-maintenance.md` and the CHANGELOG after this story
- **When** they are read
- **Then** they contain every FR-063 topic, including the group K badge rule
- **And** the README has numbered steps for hawaii, vermont, PiVPN and PiCUPS as FR-064 states, including the single re-run covering AGENT-HARDEN-001, PATCH-FLOW-001 and SHUTDOWN-SUDO-001 and the reboot after the 9 to 10 update
- **And** the CHANGELOG names the feature, `commands.allow_extra_updaters`, the installer question, the new privilege and the badge rule change

### AC-044: Non-DietPi and non-apt hosts unchanged [Maps to FR-066, FR-067]
- **Given** an apt host without DietPi, a dnf host, and macOS and Windows agents
- **When** existing patch tests run and an `os_update` executes with injected runners
- **Then** the apt run's request, record, steps, sudoers and outputs equal the pre-story values, and the other platforms are unchanged apart from group K badge results

### AC-045: Safety scan [Maps to SC-001, SC-002, SC-004, NFR-007]
- **Given** the repository's tests and helpers after this story
- **When** a scan inspects every process the tests start
- **Then** none runs `dietpi-update`, a DietPi program, a mutating apt/apt-get/dpkg command, a power command or an installed `riot-agent`
- **And** detection tests read only fixture directories and make no network request

### AC-046: vermont case: automatic patching off, nothing waiting [Maps to FR-068, FR-069, FR-070, FR-079, FR-080, FR-081, BR-009]
- **Given** an apt host with Auto-patch off, fresh lists, `pending_updates` 0, `reboot_required` false, no extra-updater entries, `unattended_upgrades` true, and no other reason condition
- **When** the server computes the badge
- **Then** the kind is `manual`, the label is "Manual", and the needs-you list is empty
- **And** the "Needs you" summary reads "Nothing."
- **And** the information items contain `auto_patch_off` with its existing text
- **And** the notes still contain the unattended-upgrades note

### AC-047: Automatic patching off with apt updates pending [Maps to FR-069, FR-070]
- **Given** the AC-046 host with `pending_updates` 7
- **When** the server computes the badge
- **Then** the kind is `needs_you`, and `auto_patch_off` is an action item and not an information item

### AC-048: Only a DietPi update pending [Maps to FR-068, FR-069, FR-070, FR-071]
- **Given** apt hosts with 0 apt pending, no reboot required, and DietPi `update_available`, in three cases: (a) Auto-patch off, DietPi `enabled`; (b) Auto-patch set up, DietPi `not_opted_in`; (c) Auto-patch set up, DietPi `enabled`
- **When** the server computes each badge
- **Then** (a) is `needs_you` with action item `auto_patch_off`
- **And** (b) is `needs_you` with action item `extra_update_not_applied` naming DietPi, the version change, "not opted in" and what Ryan must do
- **And** (c) is not `needs_you` and has no action item

### AC-049: Reboot required and nothing reboots [Maps to FR-078, FR-069, FR-070]
- **Given** an apt host with Auto-patch off, 0 pending, `reboot_required` true, and `allow_reboot` false
- **When** the server computes the badge
- **Then** the kind is `needs_you` with action item `manual_reboot_needed`, its text and action unchanged from PATCH-FLOW-001
- **And** `auto_patch_off` is an information item, not an action item

### AC-050: Fleet count excludes information-only hosts [Maps to FR-082, FR-083]
- **Given** a fleet of three hosts with badges `needs_you`, `manual` (information items only) and `auto`
- **When** Ryan opens Fleet Overview and the "hosts need you" filter
- **Then** the button reads "1 host needs you" and the filter shows only the first host
- **And** the `manual` badge is shown in neutral styling, not red, in the badge component and wherever the patch-flow helpers use the kind

### AC-051: Server-wide and agent settings alone [Maps to FR-070]
- **Given** apt hosts with nothing waiting: one with the server-wide OS-patch setting unusable, one whose agent refuses patching
- **When** the server computes each badge
- **Then** neither is `needs_you`
- **And** `patching_disabled_server` and `agent_patching_not_allowed` respectively are information items

### AC-052: Held packages and reboot-class holds [Maps to FR-073, FR-074, FR-075]
- **Given** apt hosts with nothing else waiting and Auto-patch set up, in these cases: (a) held packages not placed by rIOt, none with an update; (b) the same where telemetry cannot tell; (c) one held package with a newer version available; (d) rIOt reboot-class holds active, `reboot_class` not gated, `pending_reboot_class_count` 0; (e) the same with `pending_reboot_class_count` 1 and fresh lists; (f) hold enforcement `no_privilege`
- **When** the server computes each badge
- **Then** (a), (b), (d) and (f) are not `needs_you`, with `held_packages`, `held_packages`, `reboot_class_held_no_release` and `hold_enforcement_broken` as information items
- **And** (c) and (e) are `needs_you` with `held_packages` and `reboot_class_held_no_release` as action items

### AC-053: Release upgrade, failed run and unsupported host [Maps to FR-072, FR-076, FR-077]
- **Given** apt hosts with Auto-patch set up, in these cases: (a) an OS release upgrade `available` and nothing else waiting; (b) the last run failed and nothing is waiting now; (c) the last run failed and 2 apt updates are pending; (d) a host with no patch data; (e) a host with an unsupported package manager reporting 4 pending updates and fresh lists
- **When** the server computes each badge
- **Then** (a) is `needs_you` with action item `manual_approval_update`
- **And** (b) is not `needs_you`; `last_run_failed` is an information item that says nothing is pending now and keeps its command link
- **And** (c) is `needs_you` with action item `last_run_failed`
- **And** (d) is not `needs_you`, with `cannot_patch_host` as an information item
- **And** (e) is `needs_you` with action item `cannot_patch_host`

### AC-054: Stale lists are not waiting work [Maps to FR-068, FR-070]
- **Given** an apt host with Auto-patch off, `stale` lists, a raw pending count of 38, and `reboot_required` false
- **When** the server computes the badge
- **Then** it is not `needs_you`, `auto_patch_off` is an information item, and the stale-lists note is present

### AC-055: Each code once, display-only, other lines unchanged [Maps to FR-079, FR-084, NFR-002]
- **Given** every badge case in AC-046 to AC-054
- **When** the outputs are inspected
- **Then** no reason code appears both as an action item and as an information item, or twice in either list
- **And** for the same inputs, the Automatic, Reboot and As-of lines equal the pre-story values
- **And** no test of `checkAutoPatch` or dispatch changes its expected result because of group K

## 9. Out of Scope

- Snap, flatpak or any updater other than DietPi (NFR-006 only states what the contract must support).
- Running DietPi's check (`dietpi-update 2`) from rIOt, or changing `dietpi.txt` (BR-007).
- Stopping DietPi's own `CONFIG_CHECK_APT_UPDATES=2` apt upgrades. rIOt only reports them (FR-012).
- Changing DietPi's autopurge, service stop and restart, survey or patch behaviour.
- Applying DietPi in `repair` mode, or applying it outside an `os_update` run (no separate DietPi command or button).
- Rolling back a failed or timed-out DietPi run, or cleaning up DietPi's lock directory.
- Wiring the start and finish events into EXPECT-001 suppression or display.
- Carrying `allow_extra_updaters` across a `--bootstrap-key` re-enrolment (existing reset behaviour applies).
- The AGENT-PRIV capability manifest. If it lands, DietPi maps to one capability derived from `commands.allow_extra_updaters` that grants exactly `/boot/dietpi/dietpi-update 1`.
- Detecting what unattended-upgrades applies, or counting it as automatic patching (the existing note stays).
- Changing the NY-10 reboot rule, the Automatic and Reboot lines, or any reason text other than FR-077's added sentence and the new `extra_update_not_applied` reason.
- Running the installer, doctor or any update on fleet servers. Ryan does this.

## 10. Assumptions

- **A-001:** Clod set the decisions in Section 11.1 on Ryan's behalf on 2026-10-26. Ryan reviews them in the PR.
- **A-002:** `dietpi-update 1` applies pending live patches even when no version update exists. If the architect finds this false, FR-011 changes so that live patches alone do not count as pending.
- **A-003:** `riot` can read `/boot/dietpi/.version`, `/boot/dietpi.txt` and `/run/dietpi/*` on PiVPN and PiCUPS. If not, the status is `unknown` (FR-015) and the docs say so.
- **A-004:** A successful `dietpi-update 1` leaves the installed version in `.version` equal to the version it applied, so FR-010's "not newer" rule clears the pending state even if DietPi leaves the flag file.
- **A-005:** DietPi's in-place update does not stop or restart `riot-agent` or the maintenance service, because they are not DietPi-managed services.
- **A-006:** Both Pis run Bookworm or newer (they show 10.8.2 available), so their next update is a direct jump from 9.x to 10.8.2 and includes the Raspberry Pi kernel and firmware migration.
- **A-007:** The Fleet Overview "hosts need you" count and filter are keyed only on badge kind `needs_you` (per the coordinator's code pointer), so FR-069 and FR-081 are enough to remove information-only hosts from the count. The architect confirms no other count reads the needs-you list length.

## 11. Open Questions

### 11.1 Decisions for Ryan's review

Each decision is set to its recommended default, and the FRD is written to it. Status of every item: **decided (default), Ryan reviews in the PR.**

- **DEC-001: Accept DietPi's inherent behaviour.** `apt-get autopurge` (removes packages), `dietpi-services stop` and `restart`, and the survey (only if the host opted in) are accepted as part of `dietpi-update`. They are disclosed in the dialog and docs (FR-056, FR-063). Changing them would mean editing DietPi.
- **DEC-002: Modes.** DietPi runs only in `full` and `security`, never in `repair` (FR-025, FR-026).
- **DEC-003: Opt-in key.** Use one list, `commands.allow_extra_updaters: [dietpi]`, rather than a boolean per updater. Reason: snap and flatpak then need no new config key, no new installer parsing and no new doctor section, and one fail-closed list parser covers all updaters. Revocation is removing the name and re-running the installer (FR-017 to FR-023).
- **DEC-004: Manual Patch includes DietPi by default.** When DietPi is pending and enabled, the manual Patch includes it, shows a disclosure line, and offers a ticked-by-default "Apply DietPi update in this run" checkbox to skip it for one run (FR-050, FR-056).
- **DEC-005: Check disabled means unknown.** With `CONFIG_CHECK_DIETPI_UPDATES` not `1` and no update flag, the status is `unknown` (`check_disabled`). It is never counted as up to date and never applied. A valid update flag still counts, because it proves a check found an update since boot (FR-010, row D1).
- **DEC-006: Apply only a known pending update.** DietPi is applied only when its status is `update_available` at the start of the step, in both manual and scheduled runs. `unknown` is never applied, even manually (FR-028).
- **DEC-007: Partial failure shows as `failed`.** No new outcome value is added. The overall outcome is `failed` and the summary names each part (FR-038). A new "partial" value would touch every consumer (badge, scheduler history, EXPECT-001).
- **DEC-008: Scheduled reboot after DietPi.** In a scheduled run, "DietPi succeeded and a reboot is now required" counts as a reboot-class change under the policy's existing reboot rules. A manual run uses `reboot_if_required`. Both need `allow_reboot` (FR-041).
- **DEC-009: DietPi-pending is waiting work.** A pending DietPi update lists the host in Pending OS Patches. It makes the badge "Needs you" only when nothing will apply it: automatic patching is not set up, or DietPi is not enabled (`extra_update_not_applied`). An enabled DietPi update under a set-up policy is not "Needs you" (FR-051, FR-053, FR-068, FR-071).
- **DEC-010: Live patches count as pending** (FR-011), subject to A-002.
- **DEC-011: rIOt does not trigger DietPi's check or edit `dietpi.txt`.** `CONFIG_CHECK_APT_UPDATES=2` is reported as a notice only (FR-012, BR-007).
- **DEC-012: Fresh-install question on DietPi hosts only.** The default is no, `-y` answers yes, and `--non-interactive` answers no (FR-022). This matches SHUTDOWN-SUDO-001.
- **DEC-013: dpkg-lock wait.** Wait up to 5 minutes before the DietPi step, then skip it, because DietPi's apt calls do not wait for the lock (FR-033).
- **DEC-014: No retry within a run.** A failed DietPi step is retried only by the next run, so a scheduled policy retries at most once per window (FR-034, NFR-002).
- **DEC-015: DietPi time limit between 30 and 45 minutes,** with the exact value set in the ADD. The output cap is 128 KiB within the run's 256 KiB (FR-030, FR-036).
- **DEC-016: Settings alone are information (Ryan's rule).** NY-1 (automatic patching off), NY-2 (disabled server-wide) and NY-3 (agent does not allow patching) are action items only when updates are pending and nothing will apply them. Otherwise they are neutral information and do not count toward "N hosts need you" (FR-070, FR-082).
- **DEC-017: Classification of the other existing reasons.** The rule: a reason is an action item when something is actually waiting, and information when it describes only configuration or the past.
  - NY-4 (`cannot_patch_host`): action only when the host reports apt updates pending; with no patch data or nothing pending it is information.
  - NY-5 (`held_packages`): action only when a listed held package has a newer version available; information when none has, or when telemetry cannot tell (fail toward not red).
  - NY-6 (`reboot_class_held_no_release`): action only when a reboot-class update is actually pending.
  - NY-7 (`hold_enforcement_broken`): always information. It is a configuration fault with nothing waiting by itself, and any pending update it affects raises its own action item.
  - NY-8 (`manual_approval_update`): stays an action item, because a release upgrade is waiting.
  - NY-9 (`last_run_failed`): action only when work is still waiting; a stale failure with nothing pending is information that says so.
  - NY-10 (`manual_reboot_needed`): unchanged.
  - (FR-072 to FR-078)
- **DEC-018: New neutral badge "Manual".** When nothing needs Ryan and automatic patching is not set up, the badge reads "Manual" in neutral styling, instead of the red "Needs you" (today) or "Auto" (which would contradict "Automatic patching: No"). Alternative: "Up to date". Recommended: "Manual" (FR-081).
- **DEC-019: Stale package lists are not waiting work.** A pending count read from stale lists does not make a host "Needs you", which is consistent with PATCH-FLOW-001 BR-005 (FR-068, AC-054).
- **DEC-020: Failed run with work still waiting stays an action item even when automatic patching is set up.** The next window would retry, but the last attempt failed, so Ryan is told (FR-077).
- **DEC-021: Reboot-only "Needs you" lists just the reboot item.** When the only waiting work is a required reboot, NY-1 to NY-3 stay information, because NY-10 already lists them as causes (FR-070, AC-049).

### 11.2 Questions for the architect and security reviewer

These must be answered in the ADD or the security review before engineering starts.

- **OQ-A1 (architect):** Which on-disk evidence proves that DietPi's check completed since boot with no update found (FR-006, row D3)? If there is no reliable evidence, row D3 cannot occur, and a host with no update reports `unknown` (`not_checked_since_boot`). State which applies.
- **OQ-A2 (architect / security):** How the root-side opt-in reaches the privileged path: a field in the worker request (today a closed 4-field protocol), a root-owned record read by the worker, or one sudoers line. Whichever is chosen must meet FR-019, FR-021 and NFR-001, and must keep old/new agent and helper combinations working (NFR-005).
- **OQ-A3 (architect):** The step limit, the start threshold (FR-031) and the new worker budget, service backstop and command deadline (FR-032). With apt allowed 30 minutes, the run may need about 75 minutes in total. State the effect on the reservation of the host and on EXPECT-001.
- **OQ-A4 (architect):** How DietPi's process tree is terminated on timeout, and what is reported if DietPi's lock directory (`/tmp/DietPi-Update`) is left behind.
- **OQ-A5 (architect):** How PATCH-GATE's scheduled reboot rule receives the DEC-008 signal without widening reboots for apt-only runs.
- **OQ-A6 (security):** `/etc/riot/agent.yaml` is writable by `riot`. A compromised agent could add `dietpi`, and the next root installer re-run would opt in. The exposure is limited to the exact `dietpi-update 1` and needs Ryan's root re-run. Accept, or require a mitigation (for example, the installer printing what it recorded).
- **OQ-A7 (security):** `dietpi-update` downloads and runs code from DietPi's GitHub as root. This is the same trust as running it by hand. Confirm acceptance, and confirm that the source shown by the `non_default_update_source` notice is enough visibility.
- **OQ-A8 (architect):** Whether today's telemetry can tell that a non-rIOt held package has a newer version available (FR-073). If it cannot, state whether this story adds a bounded fact for it (for example a count, at most the existing 50-hold bound), or leaves NY-5 as information until a later agent change. Either is acceptable under FR-073.

## 12. Dependencies

- **D-001:** MAINT-RECOVERY-001: the root worker, its request and record, the output cap and the bounds (`internal/maintenance`, `cmd/riot-maintenance`, `scripts/riot-maintenance.service`).
- **D-002:** PATCH-FLOW-001: the manual Patch dialog, `reboot_if_required`, the `reboot_required` definition, NY-10 and post-run telemetry.
- **D-003:** PATCH-GATE: the scheduler (`checkAutoPatch`), holds and scheduled reboot rules.
- **D-004:** REBOOT-DPKG-001: the dpkg-lock wait before any reboot.
- **D-005:** SHUTDOWN-SUDO-001: the installer pattern for opt-in privileges read from `agent.yaml`.
- **D-006:** MAINT-DBUS-001: D-Bus on DietPi, installed by the same installer re-run.
- **D-007:** EXPECT-001 (unmerged, no PR): will later consume the FR-044/FR-045 steps and events. Not a blocker; no code dependency (FR-046).
- **D-008:** PATCH-BADGE-001 rules as implemented in `internal/server/handlers/patch_badge.go` and its tests, and the dashboard badge consumers (`web/src/components/PatchBadge.tsx` and its tests, `web/src/pages/FleetOverview.tsx`, `web/src/utils/patchFlow.ts`). Group K amends them.
- **D-009:** A tagged release, after which Ryan re-runs `install.sh` once on PiVPN and PiCUPS (FR-064). The badge change needs only the server and dashboard release.
- **D-010:** Technical writer: README, `docs/ops/os-maintenance.md`, CHANGELOG.
- **D-011:** No new third-party dependency, no database migration and no new external service.

## 13. Risks

- **R-001:** The first DietPi run on each Pi is a major 9 to 10 update with a kernel and firmware migration. It is the longest and riskiest run. DietPi continues even if the migration errors. Mitigation: disclosure (FR-057), the time limit, a truthful result and reboot only through the gate.
- **R-002:** DietPi-managed services (CUPS on PiCUPS, VPN on PiVPN) are down for the length of the run. A timeout or kill can leave them stopped. Mitigation: disclosure and the `timed_out` outcome with an uncertain-state note. Restoring services is out of scope.
- **R-003:** Autopurge can remove packages that Ryan installed by hand and that apt marks as automatically installed. Mitigation: disclosure (DEC-001).
- **R-004:** `CONFIG_CHECK_APT_UPDATES=2` lets DietPi upgrade apt packages outside rIOt's window. Mitigation: a notice only (DEC-011).
- **R-005:** If OQ-A1 finds no evidence of a check, healthy hosts show `unknown` rather than `up_to_date`. This is deliberate (BR-004).
- **R-006:** Under group K, a host whose hold enforcement is broken, or whose last run failed, no longer turns red while nothing is pending. Ryan sees it only in the badge details until updates arrive. This follows Ryan's rule (BR-009). Mitigation: the information items stay visible in the tooltip, and any pending update turns the host red again.
