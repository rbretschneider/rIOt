# OS maintenance and bounded recovery

MAINT-RECOVERY-001 runs supported apt patching in an independent root service, with a fixed operation list and durable results. The agent requests and observes work; it cannot supply a root command, package list, repository, environment, service name or output path. Existing authenticated command dispatch and `commands.allow_patching: true` remain required.

This feature is unreleased until a release containing the new assets is published. **v2.72.0 does not include the worker.** Do not run the new installer against that release. QA passed with notes against `e12436a`; a real-systemd smoke test below remains a production rollout gate.

## Supported hosts and prerequisites

The detached path requires Linux, apt 2+, systemd 241+ and sudo 1.9.10+. The root installer also requires preinstalled Python 3 with standard-library `fcntl` for native package locks, plus either OpenSSL 3 or Python 3 with `cryptography` for release verification. It requires the existing system tools including `visudo`, `flock` and `systemd-analyze`. Distribution names alone do not establish compatibility.

The installer does not install apt/pip prerequisites or run an implicit package transaction, except `dbus`: when `/run/dbus/system_bus_socket` is missing it installs `dbus` (apt or dnf) and starts `dbus.socket` after release verification and before taking the native package locks, refusing the install if that fails. The agent checks the maintenance unit with unprivileged `systemctl`, which needs the system bus; root-run checks use systemd's private socket and cannot detect its absence. Without it every host action is refused with `cannot inspect riot-maintenance.service: D-Bus system bus unavailable`, and `doctor` reports the missing socket. Arrange missing prerequisites through a separate reviewed administrator operation. An unsupported or missing privileged setup refuses apt maintenance; monitoring can continue. DNF keeps its existing inline update/hold behavior and does not gain detached apt recovery. macOS and Windows gain no apt-specific privileges.

## Modes and fixed transaction

The existing `os_update` command accepts these apt modes:

| Mode | Behavior after clean recovery checks |
|---|---|
| `full` (default) | Refresh indexes, then guarded `dist-upgrade`, then one more refresh if apt configuration changed during the upgrade (see [Post-run refresh](#post-run-package-list-refresh)) |
| `security` | Refresh indexes, then guarded `upgrade`, then the same conditional refresh; this retains conservative upgrade behavior and **does not select security origins only** |
| `repair` | Finish after configuration/dependency verification; no refresh, general upgrade, managed-hold release or automatic reboot |

Each run first configures interrupted packages using fixed noninteractive conffile options, then checks `dpkg --audit` and `apt-get check`. A nonempty audit is failure even when dpkg exits zero. Unresolved configuration/dependencies permit **one** `--fix-broken install` with no package operands and `--no-remove`, followed by one configure/audit/check verification. Failure stops the run. A dependency missing from the existing indexes/cache may need administrator review; the worker does not loop through refresh and repair.

Only clean full/security runs proceed to refresh and upgrade, followed by final audit/check. Every mutating apt invocation uses `--no-remove`, a 120-second native lock timeout, zero acquisition retries and the fixed conffile policy. Removal-required plans, authentication/repository failures, held dependency conflicts and failing package scripts stop without force, removal, downgrade, insecure-repository or force-unlock fallback. Native package locks are never deleted.

Package tools run with null input, working directory `/` and a replaced environment: fixed system PATH, root HOME, C locale, noninteractive debconf and `NEEDRESTART_MODE=l`. Caller environment and apt options are not forwarded. Root-managed apt configuration, repositories and package scripts remain trusted local policy.

The worker has a 30-minute transaction budget, SIGTERM with 90-second process-tree grace, and a separate two-minute hold-cleanup budget. The service has a 35-minute runtime backstop and 90-second stop timeout. Forced interruption reports uncertain package state rather than success. These bounds are not an atomic rollback promise.

A host that opted in to an extra updater (DietPi) gets longer bounds for runs that include it. See [Extra updaters (DietPi)](#extra-updaters-dietpi). Every other host keeps the bounds in this section.

## Post-run package list refresh

PATCH-FLOW-001 adds one conditional step to `full` and `security` runs. A package postinst can rewrite apt configuration after the run's refresh step. The lists then no longer match the configuration, and `apt list --upgradable` reports packages that do not exist.

The worker takes a fingerprint (path, modification time, size) of the files apt reads, immediately before its refresh. These are `/etc/apt/sources.list`, `*.list` and `*.sources` in `sources.list.d`, and the apt-readable files in `preferences.d`. It takes a second fingerprint after the upgrade and final verification. At most 1,000 directory entries are examined per fingerprint, and only metadata is read.

| Condition | Behavior |
|---|---|
| Fingerprints equal | No extra refresh. |
| Fingerprints differ, or either could not be read | One extra `/usr/bin/apt-get -o Acquire::Retries=0 update`, with a 5-minute limit. Its output is appended to the run output after a `[post_refresh]` line, within the 256 KiB bound. The record phase is `post_refresh`. |
| Fewer than 7 minutes left of the 30-minute worker budget | The extra refresh is skipped. The summary ends with `post-run package list refresh skipped: worker time budget nearly exhausted`. |
| The record cannot be saved before the extra refresh | Skipped, with `post-run package list refresh skipped: state not persisted`. |
| The extra refresh fails | The outcome stays `success`. The summary ends with `post-run package list refresh failed: <error>`. |
| Upgrade or verification failed | No extra refresh. The failure outcome stands. |
| `repair` mode | No fingerprint and no refresh. |

The extra refresh never changes the exit code or marks the record interrupted. The 7-minute threshold keeps the worker budget from expiring during it. The argv is the one already allowed to the worker, so `install.sh` and sudoers are unchanged. The request and record formats are unchanged: a new agent works with an old helper, and an old agent works with a new helper.

After every successful refresh, the worker also sets the modification time of `/var/lib/apt/lists` to the current time. A refresh that downloads nothing leaves that time unchanged, and the agent compares apt configuration times with it. The worker opens the path with `O_NOFOLLOW|O_DIRECTORY`, requires a directory owned by root, and changes the time on that open descriptor. It changes nothing else. On failure, the run output gets a `[lists_stamp]` line and the run carries on.

This behavior reaches a device only when a matching release's `install.sh` is re-run as root. Agent-only updates do not refresh the worker. See [Manual patch and reboot](../../README.md#manual-patch-and-reboot) for how the agent reads the result.

## Extra updaters (DietPi)

EXTRA-UPD-001 adds a generic "extra updater" step to the OS patch flow. An extra updater is a platform updater that works outside apt. DietPi is the only one today. DietPi's own `dietpi-update` upgrades DietPi itself, which apt cannot do.

The step is opt-in per host. A host that has not opted in behaves exactly as described in the sections above. The per-server steps for the fleet are in the [README](../../README.md#dietpi-updates).

### What the step does

- The step runs inside the root worker `riot-maintenance`, in the same service start as apt. The agent never runs DietPi.
- It runs after apt: after the upgrade, the final verification, the post-run refresh and the restoration of rIOt's managed holds.
- It runs `/boot/dietpi/dietpi-update 1`, with null input, a fixed environment and working directory `/`. Neither the agent nor the server can supply an argument, environment value or path.
- It runs only in the `full` and `security` modes. It never runs in `repair` mode.
- It runs at most once per command. Redelivery, an agent restart or recovery of an interrupted run never starts it again.
- It never runs when the apt part did not succeed, including a failed hold restoration. The result then says `skipped (apt_not_successful)`.
- A refusal before it starts never fails a good apt run. See [Results and truthful outcomes](#results-and-truthful-outcomes).
- It never reboots the host. See [Reboots](#reboots-and-the-extra-updater-step).

The root worker starts the step only when every gate below passes. The first gate that fails decides the result, and DietPi is not started.

1. The apt part succeeded.
2. The agent asked for it, and the root policy file lists it.
3. A fresh read of DietPi's state says an update is waiting.
4. The update source is the default owner (see [Update source rule](#update-source-rule)).
5. DietPi's files pass the root trust walk (see [The root trust rule](#the-root-trust-rule-and-vartmpdietpi)).
6. The service really has the 85-minute backstop and no mount sandbox.
7. At least 47 minutes 30 seconds of the worker's 80-minute budget remain.
8. DietPi was not started in the last 60 minutes.
9. No other DietPi program runs and the dpkg lock is free. The worker waits up to 5 minutes for this.

After the gates pass, the launcher gives the step a private `/tmp` and a private `/run/dietpi`, and proves both. If it cannot, DietPi is not started and the result says `skipped (tmp_not_private)`, `skipped (rundir_not_private)` or `skipped (mount_propagation_unverified)`. See [How the step is isolated](#how-the-step-is-isolated).

### Opt in

Both sides must agree. The agent side alone never runs a root updater.

1. **Agent side.** Add `dietpi` to `commands.allow_extra_updaters` in `/etc/riot/agent.yaml`. Also keep `commands.allow_patching: true`:

   ```yaml
   commands:
     allow_patching: true
     allow_extra_updaters: [dietpi]
   ```

   Write the list in flow style with plain lowercase names, as above, or as a block list of plain names. The installer reads the file as untrusted input. A quoted name, a scalar, a list under another section or a file it cannot parse counts as "no updaters". An identifier the agent does not implement is ignored, with one warning in the agent log per config load.

2. **Root side.** Re-run `install.sh` as root, with the existing server URL and without `--bootstrap-key`. The installer derives the root opt-in from the final agent config. It records `dietpi` only when the list contains it and DietPi is present on the host. On a fresh install on a DietPi host it asks `Allow rIOt to apply DietPi updates during OS patching?` with the default no.

The installer writes two root-owned files and prints one line that says what it decided:

| File | Content |
|---|---|
| `/etc/riot-maintenance/extra-updaters.json` | `{"version":1,"extra_updaters":["dietpi"]}` when opted in, or an empty list when revoked. At most 512 bytes. Mode 0644, owned by root, outside the `riot`-owned `/etc/riot` |
| `/etc/systemd/system/riot-maintenance.service.d/50-riot-extra-updaters.conf` | `[Service]` and `RuntimeMaxSec=85min` when opted in, `35min` when revoked. It sets nothing else |

The write order is fixed. On opt-in, the installer writes the drop-in, reloads systemd and checks the effective `RuntimeMaxUSec` is `1h 25min`. It checks that `PrivateTmp` is `no` and `MountFlags` is empty. Only then does it write the policy file. A non-empty policy therefore always means an extended service. The installer prints `==> DietPi updates: root-side opt-in recorded (/etc/riot-maintenance/extra-updaters.json); maintenance backstop 85min`. Read this line on every re-run.

`/etc/riot/agent.yaml` belongs to the `riot` user. A compromised agent could add `dietpi` there, and your next root re-run would record the opt-in. The `==> DietPi updates:` line is the check. The effect is bounded: a run needs an update that is pending from the default `MichaIng` source, and the worker starts DietPi at most once per 60 minutes.

No sudoers line is added. The sudoers file stays byte-identical. The installer refuses, before it changes anything, when `dietpi` is listed on a host that cannot use the detached maintenance path (it needs apt 2+, systemd 241+, sudo 1.9.10+ and Python 3). A listed `dietpi` on a host without DietPi records nothing and prints that.

### Revoke the opt-in

To revoke, remove `dietpi` from `commands.allow_extra_updaters` (or set the list to `[]`), then re-run `install.sh` as root. The installer writes the policy `[]` first, then sets the drop-in to `35min`, reloads and verifies. It prints `==> DietPi updates: root-side opt-in revoked`. It deletes no file. Until you re-run the installer, the root side still lists `dietpi`. The agent stops asking for DietPi once it restarts with the new config, so no DietPi step runs after that restart.

### Bounds

The bounds are strictly nested, so each outer limit outlasts the inner ones.

| Bound | Value |
|---|---|
| DietPi step limit | 40 minutes. At the limit the process group gets SIGTERM, then SIGKILL after 90 seconds |
| Wait for a busy DietPi program or the dpkg lock, before the step | At most 5 minutes, polled every 5 seconds |
| Launcher setup | At most 10 seconds |
| Start threshold | At least 47 minutes 30 seconds of the worker budget left |
| Apt part | 30 minutes plus 2 minutes of cleanup, as before |
| Worker overall budget | 80 minutes |
| Service backstop (`RuntimeMaxSec`) | 85 minutes. The worker refuses the step unless the effective value is at least 83 minutes 30 seconds |
| Agent hard maximum | 88 minutes |
| Command deadline | 95 minutes |
| Minimum between two DietPi starts | 60 minutes |

These longer bounds apply only when the server stamps the command with `extra_updater_budget`. The server stamps an `os_update` when all of these hold:

- the mode is not `repair`;
- the operator did not exclude extra updaters;
- the device's latest telemetry shows an updater that is enabled.

An operator cannot send the stamp. The server removes every capitalization of it from every command. Every other command keeps today's bounds: 30 minutes of apt, a 35-minute backstop, 31 minutes 30 seconds at the agent and a 40-minute deadline. If an enabled DietPi update is pending but the server did not stamp the command, the result says `skipped (insufficient_time)`. DietPi is not started. The command stays `success` when apt succeeded, and the badge shows an action item (see [Results and truthful outcomes](#results-and-truthful-outcomes)).

**Choose a patch window of at least 90 minutes.** A run on an opted-in host can last up to 88 minutes at the agent, and the host stays reserved for up to about 87 minutes. A shorter window can end while DietPi is still running. The window and its times are in UTC (see [Time display](../../README.md#time-display)).

**Avoid DietPi's `cron.daily` time, 06:25 by default.** DietPi checks for updates from `/etc/cron.daily/dietpi`. The worker waits while that job runs, but a check that starts after the worker's last poll is not seen. See [Residual risks](#residual-risks). `cron.daily` runs in the host's local time zone, and the patch window is in UTC. Convert before you compare them.

### Reboots and the extra-updater step

The step never starts a reboot, and the DietPi step cannot start one. A reboot goes only through the existing reboot gate (see [Manual patch and reboot](../../README.md#manual-patch-and-reboot)).

- After the step, the worker records whether a reboot is required. On DietPi a reboot is required when `/run/reboot-required` exists, or when `/lib/modules/<running kernel>` does not exist.
- If the host reboots during the step, the outcome is `interrupted`.
- A manual run reboots only when the operator ticked **Reboot automatically if the patch requires it**, `commands.allow_reboot` is `true` and a reboot is required after the run.
- A scheduled run treats "DietPi succeeded and a reboot is now required" as a reboot-class change. It adds the entry `dietpi` to the applied reboot-class list only when all four hold: the run released reboot-class holds, the run succeeded, DietPi succeeded and a reboot is required after it. The usual rules then apply: `commands.allow_reboot`, the dpkg lock wait and one reboot per command.
- A DietPi step that started and did not succeed makes the command `failed`, so "reboot if required" does not reboot. A step that was refused before it started does not make the command fail, so the reboot rules apply to the apt part as usual.

### The meaning of unknown

rIOt never reports DietPi as up to date unless it has proof that DietPi's own check finished since boot and found nothing. DietPi leaves no such proof, so a quiet host reports **unknown**, not "up to date". Unknown never raises a "Needs you" item. The dashboard says why:

| Reason | Meaning | Fix |
|---|---|---|
| `check_disabled` | `CONFIG_CHECK_DIETPI_UPDATES` in `/boot/dietpi.txt` is not exactly `1` | Set `CONFIG_CHECK_DIETPI_UPDATES=1` |
| `not_checked_since_boot` | The check is on, but DietPi has not found an update since boot | None. This is the normal quiet state |
| `config_unreadable` | `/boot/dietpi.txt` could not be read | Check the file's permissions |
| `version_unreadable` | `/boot/dietpi/.version` is missing a version field or has a non-numeric one | Check the file. Do not edit it by hand |
| `detection_failed` | Detection hit its 1-second limit or a read error | None. The next collection retries |

An update is "waiting" only when DietPi's own flag file `/run/dietpi/.update_available` names a newer version, or `/run/dietpi/.live_patches` exists. DietPi writes the flag from its own update check, which runs from its daily cron job and its post-boot job. rIOt reports `check_disabled` when `CONFIG_CHECK_DIETPI_UPDATES` is not `1`.

rIOt reads these files as data. It never runs DietPi to detect an update.

#### Which flags rIOt believes

`/run/dietpi` is world-writable on stock DietPi, so any local user can create a file there. The agent and the worker therefore believe a flag only when root wrote it. A flag counts only when all of these hold, checked on the file that is actually read:

- it is a regular file, and not a symlink;
- it is owned by root (uid 0);
- no group or other user can write to it;
- it has exactly one link.

A flag that fails any check counts as absent. It is not an error and never produces `detection_failed`. A forged flag therefore cannot make a host look like it has a pending update, and cannot start auto-patch. DietPi, running as root with umask 0022, writes its flags as root-owned 0644 files, so a real flag passes.

A containerised agent that reads the host through a mount of `/` and runs in a user namespace with a uid mapping is **not supported** for DietPi detection (DP-SEC-006). With such a mapping, the uid that the container sees as 0 may not be host root, so the owner check cannot prove that root wrote the flag. Where host root is not mapped into the container, it appears as the overflow uid, so no flag is believed and the host reads `unknown`. Run the agent on the host for DietPi detection.

### What DietPi does during a run

rIOt does not change DietPi's behavior. `dietpi-update` does these things by itself:

- It stops and restarts the services it manages. This includes software installed through `dietpi-software`, such as CUPS or VPN servers. They are down until the step ends, up to the 40 minutes limit.
- It runs `apt-get autopurge`. This removes packages that apt considers no longer needed, which can include a package you installed by hand and that apt marked as automatic.
- It sends a survey upload only if the host has opted in to DietPi's survey.
- A major upgrade (for example DietPi 9 to 10) can change the kernel or firmware and needs a reboot.

The dashboard's Patch dialog discloses the first two points.

**The `CONFIG_CHECK_APT_UPDATES=2` caveat.** With `CONFIG_CHECK_APT_UPDATES=2` in `/boot/dietpi.txt`, DietPi upgrades apt packages by itself, outside rIOt's control. rIOt reports a notice and does nothing about it. That upgrade runs apt, so it can take the dpkg lock during a run. A run that meets the lock fails at once, and the result says so.

### Results and truthful outcomes

The result names each updater's outcome, and a first line that states both parts, for example `apt: succeeded; dietpi: failed (exit 1)`.

| Outcome | Meaning |
|---|---|
| `success` | DietPi exited 0 |
| `failed` | DietPi exited non-zero, or the step could not start (for example launcher setup failed). The result carries the exit status and the last lines of output |
| `timed_out` | The 40-minute limit expired. The whole process tree was terminated. DietPi's state is uncertain: services may be stopped. Check `/var/lib/dietpi/logs/dietpi-update.log` |
| `interrupted` | The service stopped or the host rebooted during the step. State is uncertain. The worker never runs the step again |
| `skipped` | DietPi was not started. The reason says why (see below) |

The command is `failed` only when one of these is true:

- the apt part did not succeed;
- an updater **started** and did not succeed (`failed`, `timed_out` or `interrupted`).

An updater that was skipped before it started leaves a successful apt run `success`, whatever the reason. Nothing on the host changed, so the command is not red. This replaces the EXTRA-UPD-001 rule that made any refusal fail the whole command (DIETPI-PATH-001). A launcher problem that is reported as `failed` (`launcher setup failed: ...`) is not a skip, so it still fails the command. Apt's own results (version changes and hold restoration) are reported unchanged.

A refusal is not hidden. The first line of the result still names it, for example `apt: succeeded; dietpi: skipped (unsafe_dietpi_path)`, and the detail line stays. A refusal is any skip except the neutral ones: `not_enabled`, `status_unknown`, `nothing_pending`, `excluded_by_operator` and `apt_not_successful`. For a refusal, rIOt also does two things:

- The agent sends one warning event, `DietPi update skipped on <host>: <reason in words> (command <id>, origin <origin>)`. It sends at most one per command and updater, across agent restarts. A neutral skip sends none. The server composes the message from its own table of reasons. If an event carries a reason the server does not know, it keeps the event with generic text and drops the reason.
- The patch badge reports the refusal. See [The patch badge on DietPi hosts](#the-patch-badge-on-dietpi-hosts).

**Results from an older worker.** A worker older than this change recorded a refused updater as an `error`, so the dashboard showed a red failed command even though apt had succeeded. That history stays as it was. The server recognises such a run when the first line is `apt: succeeded; ...`, every other part of it is a refusal, and the result has no other failure signal. It then shows the Blocked badge, not "the last patch run failed". Any other failure signal keeps the red item: an apt failure, a hold restoration error, a version verification error, an interrupted part, a timeout, or an extra line in the result.

DietPi's output is captured in its own labelled section. The section is capped at 128 KiB inside the existing 256 KiB cap for the whole run. When output is cut, the end is kept and the cut is marked. The section always ends with DietPi's own log path, `/var/lib/dietpi/logs/dietpi-update.log`.

The result also records the update source that was used, whether a reboot is required after the step, and the version before and after.

### Refusal reasons

Each reason below means the step did not start, so DietPi changed nothing on the host. The reason appears as `skipped (<reason>)` in the result. A skip never fails a successful apt run (see [Results and truthful outcomes](#results-and-truthful-outcomes)). The badge class of each refusal is in [The patch badge on DietPi hosts](#the-patch-badge-on-dietpi-hosts).

| Reason | Meaning | Fix |
|---|---|---|
| `apt_not_successful` | The apt part, including hold restoration, did not succeed. DietPi is never started after an apt failure | Fix the apt failure that the result describes, then run Patch again |
| `not_enabled` | The agent did not ask for DietPi, or the root policy file does not list it. A note `root policy not usable: ...` means the policy file failed its safety checks | Opt in on both sides. Re-run `install.sh` as root. Neutral: the command does not fail |
| `status_unknown` | A fresh read of DietPi's state at the start of the step said unknown (see [The meaning of unknown](#the-meaning-of-unknown)) | Fix the reason named in the note. Neutral |
| `nothing_pending` | DietPi had no update at the start of the step | None. Neutral |
| `insufficient_time` | One of these: the service's effective backstop is below 83 minutes 30 seconds, or the worker cannot read it; fewer than 47 minutes 30 seconds of the 80-minute budget remain; or the server did not stamp the extended budget | Re-run `install.sh` as root so the drop-in is in force. Check with `riot-agent doctor`. If apt took very long, run Patch again later. Make sure `commands.allow_patching` is true |
| `non_default_update_source` | The resolved `DEV_GITOWNER` in `/boot/dietpi.txt` is not `MichaIng`, or the file could not be read | Remove or empty `DEV_GITOWNER` in `/boot/dietpi.txt`. See [Update source rule](#update-source-rule) |
| `unsafe_dietpi_path` | A path that root executes or reads failed the root trust walk: DietPi's code or settings under `/boot`, a parent directory, or the three entries of `/var/tmp/dietpi` that root interprets. The note names the first problem. On a stock DietPi host the walk passes, so this reason is a **possible tampering signal**: something that should be root-only is not. It does not clear by retrying | Investigate the named path as root: check its owner and mode, and find out who changed it. Restore root ownership and remove group and other write permission. rIOt does not run DietPi until the path is root-only. See [The root trust rule](#the-root-trust-rule-and-vartmpdietpi). A note that starts with `/run/dietpi` comes from a worker older than this fix: re-run `install.sh` as root (see [Upgrading a host that was refused for `/run/dietpi`](#upgrading-a-host-that-was-refused-for-rundietpi)) |
| `min_interval` | DietPi was started less than 60 minutes ago, or the start history could not be read | Wait, then run Patch again. If the note says the history cannot be read, review `/var/lib/riot-maintenance/` as root |
| `tmp_not_private` | The launcher could not give the step its own empty `/tmp` (see [How the step is isolated](#how-the-step-is-isolated)). `/tmp` is not its own mount point, is not tmpfs, or is not empty | DietPi normally mounts `/tmp` as tmpfs from `/etc/fstab`. Restore that default and reboot |
| `rundir_not_private` | The launcher could not give the step its own empty `/run/dietpi` (see [How the step is isolated](#how-the-step-is-isolated)). It stopped before DietPi started. Causes: `/run` is not its own mount point; making `/run` private failed; mounting the tmpfs failed; the proof of the new tmpfs failed; `/run/dietpi` exists but is a symlink, is not a directory or is not owned by root; or the check of `/run/dietpi` failed with an error other than "does not exist" | The host's mount layout prevented the isolation, and a retry does not change that. On DietPi `/run` is a tmpfs from systemd, so a layout where it is not its own mount is unusual. Run `findmnt /run` and `ls -ld /run/dietpi` on the host, restore the default layout, and make sure `/run/dietpi` is a plain directory owned by root. Then run Patch again. Doctor does not check this yet (see [Known limits](#known-limits)) |
| `mount_propagation_unverified` | The launcher could not prove that `/`, `/boot` (and `/boot/firmware`, when mounted) are shared with the host's mounts. Or the service itself has `PrivateTmp` or `MountFlags` set. Or, after the `/run/dietpi` setup, the same check failed again | Run `systemctl show riot-maintenance.service -p PrivateTmp -p MountFlags`. It must print `PrivateTmp=no` and an empty `MountFlags=`. Remove any drop-in that sandboxes the unit, then re-run `install.sh` as root |
| `dietpi_busy` | Another DietPi program ran for the whole 5-minute wait. This covers any `/boot/dietpi/` program except the cloudshell, and the daily `cron.daily` job | Wait for the other run to end, and keep the patch window clear of 06:25. Then run Patch again |
| `dpkg_locked` | Another apt or dpkg process held the dpkg lock for the whole 5-minute wait, or the lock state could not be read | Wait for the other package work to end, then run Patch again |
| `excluded_by_operator` | The operator unticked **Apply DietPi update in this run**. Written by the agent. Neutral | None |

A launcher problem other than the three isolation reasons above (`tmp_not_private`, `rundir_not_private`, `mount_propagation_unverified`) is reported as `failed` with `launcher setup failed: ...`. DietPi was not started in that case either, but the command fails, because the result is `failed`, not `skipped`.

### Update source rule

DietPi can update from a different GitHub owner or branch. rIOt only runs DietPi when the update source owner is exactly `MichaIng`.

- The worker resolves the source with DietPi's own rule: the first `DEV_GITOWNER` line in `/boot/dietpi.txt` wins, and an empty value means `MichaIng`. A carriage return or trailing space in the value fails the check.
- A `DEV_GITOWNER` of any other value is refused with `non_default_update_source`. The agent reports readiness `non_default_update_source`, and the badge shows an action item.
- The worker checks the owner only. A different `DEV_GITBRANCH` does not block the step, but the dashboard shows a notice, and the result records the owner and branch it used.
- To fix a refusal, remove `DEV_GITOWNER` from `/boot/dietpi.txt` or set it to `MichaIng`.

This rule exists because the step runs DietPi as root. A compromised agent cannot point DietPi at another repository.

### The root trust rule and /var/tmp/dietpi

Before it runs DietPi, the worker checks as root the files that root executes or reads. It refuses with `unsafe_dietpi_path` when a non-root user could change any of them. DietPi runs as root, so a file that another user controls would run as root.

The check has three parts.

**Parents.** `/`, `/boot`, `/run`, `/var` and `/var/tmp` must be root-owned. A parent that is group- or world-writable fails unless it has the sticky bit. `/run` itself stays a checked parent.

**Trees.** These are walked entry by entry, to a depth of 4 and 4096 entries:

- `/boot/dietpi`, the code that DietPi executes;
- `/boot/dietpi.txt`, the settings that DietPi reads.

Every entry must be owned by root, with no group or world write permission. Symlinks are allowed only when root owns them. A limit that is reached counts as a failure. A FAT `/boot` that is mounted with permissive file modes (for example world-writable) fails the walk, because FAT has no owners.

**Exact paths.** Only these three entries under `/var/tmp/dietpi` are checked, each by one `lstat`. Nothing below them is walked:

- `/var/tmp/dietpi`, a directory;
- `/var/tmp/dietpi/logs`, a directory, when it exists;
- `/var/tmp/dietpi/dietpi-update_reinstalls`, a regular file, when it exists.

Each must be owned by root, must not be a symlink and must not be group- or world-writable. A sticky bit does not excuse a writable mode here: root-owned `0777` and `1777` both fail. The file must also have exactly one link.

**What is not checked, and why.**

- `/run/dietpi` is not checked. Stock DietPi creates it at every boot as `0777`, root-owned and without the sticky bit, so the old check refused every DietPi host. `dietpi-update` never reads it. It only writes and removes flag files there. The launcher gives the step a private empty one instead (see [How the step is isolated](#how-the-step-is-isolated)).
- The contents of `/var/tmp/dietpi/logs` are not checked. With DietPi-RAMlog, the default, `logs/dietpi-ramlog_store` mirrors `/var/log` with its original owners and modes. That includes `0664` files and service-owned directories, so a tree walk could never pass on DietPi 9. Root copies those entries as data (DietPi's Patch_10_4 uses `cp -a`, which does not follow links and keeps the owner), and never runs them. The only file root acts on is `dietpi-update_reinstalls`, and it is checked.
- During a DietPi 9 run, root also writes into `logs/` (the update log, the apt log and a backup database), and follows symlinks when it does. That is safe because `logs` and `/var/tmp/dietpi` are proved root-only and cannot be replaced by a non-root user.

**Reading an `unsafe_dietpi_path` refusal.** On a stock DietPi host the check passes. A refusal therefore means that DietPi's code or settings, or one of the three exact paths, is not root-only. Treat it as a possible tampering signal. The badge shows **Needs you** with an action to investigate the named path. rIOt will not run DietPi until the path is root-only, and retrying does not change that.

DietPi 10.4 and later migrate `/var/tmp/dietpi` to `/var/lib/dietpi`. That migration runs as root during the update, and it reads from `/var/tmp/dietpi`. While the installed DietPi is older than 10.4, the directory must therefore exist and pass the walk. Otherwise the step is refused.

**Never copy, move or sync anything out of `/var/tmp/dietpi` by hand.** `/var/tmp` is writable by every user. A root copy of content that a non-root user created can bring symlinks and foreign ownership into `/var/lib/dietpi`, where DietPi later writes as root. DietPi's own migration handles this directory, and the root trust walk guards it. If the walk refuses a host, find out why a non-root user owns or can write the path, and fix the ownership and permissions as root. If the directory is missing on a DietPi older than 10.4, update DietPi once by hand with its own tools. After 10.4 the directory is not required.

### How the step is isolated

DietPi's working directory is `/tmp/DietPi-Update`. If a non-root user owned that directory when DietPi started, the user could replace files that DietPi then runs as root. To prevent this, the worker starts DietPi through its own launcher, `riot-maintenance --run-extra dietpi`. The launcher:

1. creates a private mount namespace for the step only;
2. makes only `/tmp` private and read-only in that namespace, then mounts a fresh empty tmpfs on it;
3. proves, before DietPi starts, that the new `/tmp` is a different, empty, root-owned tmpfs that the host cannot see;
4. then starts the fixed `dietpi-update 1`.

The tmpfs, and DietPi's working directory in it, disappear when the last step process ends. No stale `/tmp/DietPi-Update` survives a killed run.

#### A private /run/dietpi

Stock DietPi makes `/run/dietpi` world-writable (`0777`) without the sticky bit. During `dietpi-update 1`, root writes flag files there and removes others, and each of those operations follows a symlink. A local user without root rights could plant a symlink there and make root truncate or overwrite an arbitrary file with a version string or a package count. The kernel protection against this (`fs.protected_symlinks`) does not apply to a directory without the sticky bit. To close this for the runs that rIOt starts, the launcher gives the step its own `/run/dietpi`. When `/run/dietpi` exists, the launcher:

1. proves, without following links, that `/run/dietpi` is a directory owned by root;
2. checks that `/run` is a mount point of its own;
3. makes `/run` and what is mounted below it private in the step's namespace only;
4. mounts a fresh tmpfs on `/run/dietpi` with mode `0755`, `nosuid`, `nodev` and `noexec`, limited to 1 MiB and 64 inodes;
5. proves that the new `/run/dietpi` is a tmpfs, owned by root, mode `0755`, empty, not the host's directory and not shared with the host;
6. checks again that `/`, `/boot` and `/boot/firmware` still propagate to the host, and only then starts DietPi.

Any failure stops the step before DietPi starts, with `skipped (rundir_not_private)`. A failed re-check in step 6 is reported as `mount_propagation_unverified`. When the host has no `/run/dietpi`, the launcher creates and mounts nothing, and DietPi creates the directory itself as root.

**What DietPi sees.** An empty `/run/dietpi`. Everything it writes there, and every removal it makes, dies with the step. DietPi never reads these files during `dietpi-update 1`, so this changes nothing about what the update does. The rest of `/run` is the host's own: the systemd sockets and `/run/lock` are the same mounts, so `dietpi-services` and systemd work as before. The host's `/run/dietpi` is not touched by the step.

**Side effect.** Mounts and unmounts that happen under `/run` on the host while the step runs do not reach the step. For example, the `/run/user/<uid>` directory of a user who logs out stays in place until the step ends. The 85-minute service backstop bounds this. It is the same kind of effect as the private `/tmp`.

#### Flag cleanup after a successful step

Because the step's own writes land in the private directory, its removal of DietPi's two flags also dies with the step. DietPi removes `.update_available` and `.live_patches` itself after a successful update. To keep the host's flags truthful, the worker does the same on the host after a step with outcome `success`:

- It removes `/run/dietpi/.update_available` and `/run/dietpi/.live_patches`. It removes nothing else: `.apt_updates` and `.dietpi_motd` stay.
- It opens `/run/dietpi` without following a symlink and requires the directory to be owned by root. If it is not, nothing is removed and the updater result carries the note `/run/dietpi not root-owned; flags left`.
- It removes each name through that directory handle. A symlink with a flag's name is removed itself, and its target is never touched. A missing file is not an error.
- It never creates or writes anything there.
- It runs after a `success` only, never after `failed`, `timed_out`, `interrupted` or a skip.
- It runs before the worker reads DietPi's state again after the step. A stale `.live_patches` would otherwise read as a pending update and trigger auto-patch again.
- Any other problem becomes a note of at most 200 bytes on the updater result, such as `flag cleanup failed: ...`. A note never changes the outcome, which stays `success`.

**DietPi's own mounts take effect normally.** The launcher keeps every mount except `/tmp` shared with the host, and proves it against the host's mount table. Anything DietPi mounts or unmounts, and every edit of `/etc/fstab`, behaves as if DietPi ran unconfined as root. The service unit itself carries no `PrivateTmp`, `MountFlags` or `Protect*` settings. The worker refuses a unit that does (`mount_propagation_unverified`).

This matters for the DietPi 9 to 10 update on a Raspberry Pi. That update (DietPi's `rpi_firmware_migration`) moves the FAT boot partition from `/boot` to `/boot/firmware`, rewrites `/etc/fstab` and remounts. DietPi's files in `/boot/dietpi` then live on the root file system. The change is visible to the host at once, so rIOt's detection keeps working. The host still needs a reboot afterwards. See [Reboots](#reboots-and-the-extra-updater-step).

### Residual risks

- **The `cron.daily` race.** Because DietPi's own guard cannot see a run in the host's `/tmp`, the worker scans for other DietPi programs before it starts and waits up to 5 minutes. A DietPi run that starts after the last poll is not seen. In practice this is the daily `cron.daily` check at 06:25, or an administrator's manual run. The overlap lasts at most the 40-minute step. A check-only run can rewrite the flag file. With `CONFIG_CHECK_APT_UPDATES` set to 1 or 2 it can also run apt. The run that meets the apt lock second fails at once, and the result says so. Keep the patch window away from 06:25.
- **DietPi's own exposure, which exists without rIOt.** DietPi's `cron.daily` job and its post-boot job run `dietpi-update 2` as root in the host's shared `/tmp`, with the same working directory. A local user who can win a race there could attack those runs. rIOt's step avoids this exposure, but it cannot remove it from DietPi's own jobs. Consider reporting this to DietPi upstream at <https://github.com/MichaIng/DietPi/issues>.
- **DietPi's own exposure in `/run/dietpi`, which also exists without rIOt.** DietPi's `cron.daily` job runs `dietpi-update 2` as root every day. It writes `.update_available` into the world-writable, non-sticky `/run/dietpi`, and the write follows a symlink. A local user who plants a symlink there can make that root run truncate or overwrite a file with a version string. rIOt's private `/run/dietpi` protects only the runs that rIOt starts. It cannot protect DietPi's own jobs. The next section lists host settings that reduce this exposure.
- **A remount of `/tmp` inside the step.** A `mount -o remount /tmp` by DietPi would act on the step's private tmpfs. `dietpi-update` does not do this today.

### Host hardening for DietPi hosts (recommended)

These are recommendations for the DietPi hosts you run, PiVPN and PiCUPS. rIOt does not apply them: it does not change DietPi-owned system state outside the step, and DietPi's tmpfiles rule recreates `/run/dietpi` at every boot. Each one is optional. The realistic attacker is a compromised network-facing service account on the host, such as the account that runs CUPS.

1. **Make `/run/dietpi` sticky.** DietPi creates the directory at every boot with the line `d /run/dietpi 0777` in its tmpfiles file, which is `dietpi.conf` in `/etc/tmpfiles.d`. Do not edit that file: a DietPi update can overwrite it. Add a local override instead. `systemd-tmpfiles` reads the files in name order, uses the first line for a path and ignores later lines for the same path. The override must therefore have a name that sorts before `dietpi.conf`, for example `/etc/tmpfiles.d/00-dietpi-run.conf`, with this content:

   ```
   d /run/dietpi 1777 root root -
   ```

   Check the result with `systemd-tmpfiles --cat-config`: the `1777` line must come before DietPi's line. The reverse order keeps `0777`, because the later line is ignored with a `Duplicate line for path` message. With the sticky bit, a user cannot replace or remove another user's file in the directory, and `fs.protected_symlinks` then applies to root's writes. Test on one host first: DietPi's own scripts must still write their flag files there after a reboot.

2. **Turn on the kernel's link and file protections.** On PiCUPS, `fs.protected_symlinks` and `fs.protected_regular` were `0` when the DIETPI-PATH-001 security review looked. Read the current values on each host first with `sysctl fs.protected_symlinks fs.protected_hardlinks fs.protected_regular`. Then create `/etc/sysctl.d/99-riot-protected.conf` with:

   ```
   fs.protected_symlinks = 1
   fs.protected_hardlinks = 1
   fs.protected_regular = 2
   ```

   Apply it with `sudo sysctl --system`. These settings protect every sticky temporary directory on the host, `/tmp` and `/var/tmp` included, not only `/run/dietpi`. They do not protect a non-sticky directory, which is why item 1 comes first.

3. **Report it upstream.** Tell the DietPi project that `/run/dietpi` is created world-writable without the sticky bit, and that root writes follow symlinks there. Open an issue at <https://github.com/MichaIng/DietPi/issues> and ask for `1777` in the tmpfiles rule, or for a root-only directory.

Check each step on one host first. A wrong tmpfiles file or sysctl value takes effect at the next boot or the next `sysctl --system`, and rIOt does not monitor it.

### Upgrading a host that was refused for /run/dietpi

A worker older than this fix refuses every stock DietPi host with `unsafe_dietpi_path` and a note that starts with `/run/dietpi `, because DietPi makes that directory `0777`. This is a false positive. The fix is in the worker, and a worker reaches a host only through a root run of `install.sh`:

1. Update the rIOt server first. The server alone changes the badge of such a host from **Needs you** to **Blocked** and removes its "last patch run failed" item, with the text `DietPi update blocked: /run/dietpi permissions (fixed by this rIOt release; re-run install.sh as root on this host to apply the fix)`. It changes nothing on the host.
2. On the host, re-run `install.sh` as root with the matching `--version` (without `v`) and your existing server URL, and without `--bootstrap-key`. The re-run installs the new worker and agent together. Never copy the worker binary alone: a new worker with an old agent is not a supported pairing.
3. Run doctor, then one manual Patch.

The **Blocked** badge does not clear when the installer finishes. The badge reads the latest finished OS patch run, so it stays until the next OS patch run ends after the fix is installed. The manual Patch in step 3 clears it. The per-host steps for the fleet are in the [README](../../README.md#fix-for-stock-dietpi-hosts-dietpi-path-001).

### Downgrading below this release

Archive `/var/lib/riot-maintenance/current.json` as root before you downgrade the worker or the agent below this release. The new release records a `success` result that carries a refused updater. A worker or agent from before this release treats that combination as invalid and can reject the stored history. Do not delete the file; copy it somewhere safe. Downgrades are otherwise not supported.

### Known limits

- **A backward clock jump disables the 60-minute interval while it lasts.** The worker measures the 60 minutes from the start times it recorded in its root-owned history. It ignores a record dated in the future. If the host clock jumps backward (for example a Pi without a hardware clock that boots with a stale time before NTP corrects it), the newest recorded start can lie in the future. The worker then skips the interval check for as long as the clock stays behind that time, not only once. The other gates still apply: an update must be pending from the `MichaIng` source, the trust walk, the busy and lock checks, and the time budget. A compromised agent cannot move the clock. The fix is tracked as a follow-up.
- **Event de-duplication works for a single server process only.** The server stores one "DietPi update started" and one "finished" event per command. It drops repeats with an in-memory lock around a database check, not with a database constraint. With one rIOt server process, which is today's deployment, this is exact. Two server replicas on one database could store a duplicate event. The check also keys on the command and the event type, not on the updater, so a second updater will need the key extended.
- **DietPi's own daily job still writes into a world-writable `/run/dietpi`.** `cron.daily` runs `dietpi-update 2` as root, outside rIOt's step. That exposure is upstream's. See [Residual risks](#residual-risks) and [Host hardening for DietPi hosts](#host-hardening-for-dietpi-hosts-recommended).
- **The Blocked badge stays until the next OS patch run ends after the fix is installed.** The badge reads the latest finished OS patch run. Re-running `install.sh` does not change that run's stored result, so a host that was refused before the fix keeps its Blocked badge until a run finishes with the new worker. Run one manual Patch after the re-run.
- **Doctor has no isolation probe yet.** For `tmp_not_private`, `rundir_not_private` and `mount_propagation_unverified`, the badge action says to run `riot-agent doctor`. Doctor checks that DietPi is present, the root-side opt-in, the helper, and the service backstop and sandbox settings. It does not check that `/tmp` or `/run` is its own mount, or the mount propagation. Doctor can therefore be green on a host that the launcher refuses. Look at the mounts yourself with `findmnt /tmp`, `findmnt /run` and `ls -ld /run/dietpi`. A doctor check for the isolation preconditions is a recorded follow-up (DIETPI-PATH-001 QA note 1).
- **Real Raspberry Pi hardware has not been tested.** The first DietPi 9 to 10 update on PiVPN or PiCUPS is the first time the launcher meets `rpi_firmware_migration` on a real kernel. Run `riot-agent doctor` and one manual Patch before you rely on the schedule.

### Check with doctor

With `commands.allow_extra_updaters` set, run doctor as the service account:

```bash
sudo -u riot /usr/local/bin/riot-agent -config /etc/riot/agent.yaml doctor
```

The **Extra Updaters** section prints one pass or fail line for each check, with the fix on each failure. It reads only. It never runs DietPi.

| Check | Passing line |
|---|---|
| DietPi is present | `dietpi detected (<version>)` |
| The root side lists it | `dietpi: the root-side opt-in lists it (/etc/riot-maintenance/extra-updaters.json)` |
| The helper supports it | `dietpi: the maintenance helper can run it (request version 2)` |
| The service allows it | `dietpi: the maintenance service allows 1h25m0s with no mount sandbox` |
| The source is trusted | no line. A failure prints `its update source is not the trusted default` |

On a host without DietPi, doctor says `dietpi: not applicable here`. Doctor prints a warning when `commands.allow_patching` is false.

### The patch badge on DietPi hosts

EXTRA-UPD-001 also changed when the badge says **Needs you**. A host shows **Needs you** only when work is waiting and nothing will apply it:

- updates are pending (apt or DietPi), or a reboot is required now, and
- nothing will apply them, for example auto-patch is off, or DietPi is pending but not enabled.

A setting by itself, such as "auto-patch off" on a host with nothing pending, is shown as neutral information, under **Information** in the badge text. The badge reads **Manual** when nothing needs you and nothing patches the host automatically. **Manual** is grey and does not count toward "N hosts need you". The full rules are in the [README](../../README.md#patch-badge).

#### Refused updaters: Blocked and Needs you

DIETPI-PATH-001 adds a fourth badge, **Blocked** (amber). The badge looks at the latest finished OS patch run. It reports a refusal when that run was `success` and refused an updater, that updater is still pending and enabled, and the first line of the result names the refusal. A neutral skip raises nothing. If the updater is no longer pending, the refusal becomes one line under **Information**: `The last patch run skipped DietPi: <reason in words>.`

The reason decides the class:

| Refusal | Badge | Text and action |
|---|---|---|
| `tmp_not_private`, `mount_propagation_unverified`, `rundir_not_private` | Blocked | `DietPi update blocked: <reason in words>; this host's mount layout prevented rIOt from isolating the step`. Action: `Run riot-agent doctor on this host.` The text never says that a retry fixes it, because the cause is the host's mount layout |
| `dietpi_busy`, `min_interval`, `dpkg_locked` | Blocked | `DietPi update blocked: <reason in words>`. Action: `Nothing to do; rIOt retries on the next patch run.` These conditions are temporary |
| `unsafe_dietpi_path` with a note that starts with `/run/dietpi ` | Blocked | The false positive of a worker older than this fix: `DietPi update blocked: /run/dietpi permissions (fixed by this rIOt release; re-run install.sh as root on this host to apply the fix)`. Action: `Re-run install.sh as root on this host to apply the fix.` |
| `unsafe_dietpi_path`, any other path | Needs you | `DietPi update blocked: <path> permissions`. Action: `Investigate the named path on this host (owner and permissions); rIOt will not run DietPi until it is root-only.` This is a possible tampering signal and never clears by retrying |
| `non_default_update_source` | Needs you | Action: `Set DEV_GITOWNER back to MichaIng in /boot/dietpi.txt.` |
| `insufficient_time` | Needs you | Action: `Lengthen this host's patch window to at least 90 minutes.` |

Each text also carries the bounded detail from the run, up to 160 bytes, with control characters removed.

**Precedence.** Any action item makes the badge **Needs you**. If there is no action item and at least one Blocked item, the badge is **Blocked**. Otherwise the badge is **Auto**, **Manual** or its other values, as before. When a host has both, **Needs you** wins and the Blocked reasons are still listed.

**Counting.** The **N hosts need you** button counts **Needs you** hosts only. A **Blocked** host has its own **N blocked** button and filter beside it. The two filters exclude each other. The badge tooltip lists blocked items under **Blocked**, with the line `rIOt did not start these updates. This is not a failure of the patch run.`

**Old worker results.** A run from a worker older than this change is `failed`, but the server reads it the same way when the refusal is the only cause (see [Results and truthful outcomes](#results-and-truthful-outcomes)). The server update alone therefore turns such a host's red badge into **Blocked** or **Needs you**, and removes its "last patch run failed" item, without a new patch run.

**API.** `GET /api/v1/fleet/patch-status` returns `patch_badge.kind: "blocked"` and `patch_badge.explanation.blocked`, a list of items in the same shape as `needs_you`. The list is always present and can be empty. A client that matches `kind` exactly must accept `blocked`.

## Repair through the existing API

Send this JSON through your existing authenticated administrator API client to `POST /api/v1/devices/{device-id}/commands`:

```json
{"action":"os_update","params":{"mode":"repair"}}
```

This adds no endpoint or Repair UI button. Cookie-authenticated API writes still need the normal same-origin/Origin and JSON content-type requirements. The agent's `commands.allow_patching` must be enabled. Do not write the worker inbox or start the service manually as a substitute for authenticated dispatch.

Wrong mode types and unknown mode values are refused. The server removes every client-supplied `include_reboot_class`. Two paths can release reboot-class holds: the scheduled patch policy, and a manual `full` or `security` run whose request carries `reboot_if_required: true` (PATCH-FLOW-001). The server validates that parameter against the device's latest telemetry and then sets `include_reboot_class` itself. The server rejects `reboot_if_required: true` together with `mode: repair`. Repair always preserves those holds and never requests reboot.

## Installed privilege boundary

| Path | Owner and purpose |
|---|---|
| `/usr/local/lib/riot/riot-maintenance` | root:root 0755, signed fixed worker. With no arguments it runs the worker. It accepts only two other forms: `--capabilities` (read-only probe) and `--run-extra dietpi` (the DietPi launcher, started only by the worker itself). Any other argument is an error |
| `/etc/riot-maintenance/extra-updaters.json` | root:root 0644, written only by `install.sh`. Lists the extra updaters the root side allows. Absent on hosts that never opted in |
| `/etc/systemd/system/riot-maintenance.service.d/50-riot-extra-updaters.conf` | root:root 0644, written only by `install.sh`. Sets `RuntimeMaxSec` to 85 minutes (opted in) or 35 minutes (revoked) |
| `/usr/local/lib/riot/riot-agent-apply` | root:root 0755, signed agent installer with shared exclusion protocol |
| `/etc/systemd/system/riot-maintenance.service` | root:root 0644, fixed independent service |
| `/var/lib/riot/maintenance-request.json` | Agent-written bounded input; never a root output destination |
| `/var/lib/riot-maintenance/` | root:riot 0750; root-owned results readable by the agent, files 0640 |
| `/run/lock/riot-maintenance.lock` | root:root 0600, shared nonblocking flock; never unlink or replace it to bypass contention |

The worker request is capped at 4096 bytes and accepts exactly protocol version, canonical command UUID, closed mode and reboot-class boolean. A request that includes an extra updater carries version 2 and one more field, a list of updater identifiers. It never carries a command, argument, environment value or path. Missing/duplicate/unknown/null/trailing/malformed fields fail. Safe file access rejects symlinks, unsafe owners/modes/parents, nonregular files and oversized data. Root publishes only beneath its validated state directory.

The extra-updater step adds no sudoers line. The privileged path is the root worker, a root-owned policy file and a unit drop-in.

The only new service grant is the exact `systemctl start --no-block riot-maintenance.service`, invoked with `sudo -n`. It grants no arbitrary unit, shell, property, helper argument or environment. Direct apt/dpkg upgrade grants are removed from the managed apt policy. Retained hold/unhold grants use these complete argument expressions with sudo 1.9.10+:

```sudoers
riot ALL=(root) NOPASSWD: /usr/bin/apt-mark ^hold [a-z0-9][a-z0-9+._:-]*( [a-z0-9][a-z0-9+._:-]*)*$
riot ALL=(root) NOPASSWD: /usr/bin/apt-mark ^unhold [a-z0-9][a-z0-9+._:-]*( [a-z0-9][a-z0-9+._:-]*)*$
```

Use the installer to publish and validate policy rather than appending these rules beside old wildcard grants. Each operand starts with a lowercase alphanumeric; options, paths, equals, shell syntax and control-character separators cannot match. Agent-side package validation alone cannot constrain a compromised agent invoking sudo directly. Broader grants elsewhere in effective policy must also be reviewed; the installer preserves unrelated administrator policies.

The service uses root, `Type=exec`, `Restart=no`, journal output and `KillMode=control-group`, with no dependency tying its lifetime to `riot-agent.service`. It does not inherit the agent memory cap. It is started on an explicit request, not enabled as a boot-time package job.

## Holds, restart recovery and update coordination

The worker snapshots existing managed holds, releases only tracked/installed/held/classified packages for authorized updates, restores holds after success/failure/interruption and records version changes. Operator holds stay intact. Corrupt or unsafe metadata releases nothing. Restoration failure prevents a clean-success result. Agent hold reconciliation pauses behind a barrier during maintenance. Historical agent-owned hold metadata remains an existing trust limitation, not cryptographic proof of ownership.

The root worker, normal/boot signed apply and installer share the protected lock. Active, activating, deactivating or unresolved maintenance blocks conflicting agent replacement before it stops the agent. Manual/automatic updates also reserve the host and respect Docker work. A scheduled verifier or staged binary is not installation success: acknowledgement must match the expected digest and handoff timestamp, with the running binary verified after restart. A refused/deferred handoff keeps the agent alive and reports its real state.

Agent restart reattaches the same UUID, reserves the host and delivers the matching final result through the durable command outbox. Observer cancellation does not signal or relaunch the worker. A vanished worker is interrupted/unknown, never automatically replayed. A subsequent **explicit** request may first restore the prior durable terminal result or attempt bounded prior hold cleanup, then refuse fresh work and require operator review and another request. Do not interpret that recovery refusal as a completed repair/upgrade. Missing, unreadable or mismatched state is not permission to start over.

Two things can lead to a reboot after a successful update, both after result queuing. The scheduled patch policy reboots after proven reboot-class changes. A manual run with `reboot_if_required: true` reboots when a reboot is required after the run, whatever packages changed. Both need `commands.allow_reboot: true` on the agent. Repair, failed/uncertain outcomes and recovered results do not replay a reboot. See [Manual patch and reboot](../../README.md#manual-patch-and-reboot). Either reboot first waits up to 10 minutes for the dpkg lock and is skipped, with a warning event, if the lock stays held or its state is unknown (REBOOT-DPKG-001). See [Reboots and the dpkg lock](../../README.md#reboots-and-the-dpkg-lock). Durable intent prevents a duplicate after restart. Existing reboot privilege remains separate; host administrators and already privileged accounts can still interrupt the host.

## Root migration

Before production rollout, complete the real-systemd validation below. Then obtain and review the matching released `install.sh` and its signed release manifest through the established release trust process. The manifest includes the installer, both Linux helpers and fixed service. Verify with the pinned release key using an already trusted verifier; a newly downloaded binary cannot authenticate itself.

Run the reviewed installer explicitly as root, with the matching `--version` (without `v`) and **the existing server URL**. Preserve configuration and identity. Do not add `--bootstrap-key` for an ordinary migration: that flag intentionally resets enrollment state. No release version is prescribed here until the worker assets exist.

| Existing installation | Required action |
|---|---|
| New supported apt host | Install a matching signed release with all prerequisites present |
| Current hardened signed updater | Root re-run still required: both worker and lock-aware apply helper/policy/unit must be installed |
| Agent-only upgrade | No new root capabilities are granted; missing/old helper or unsafe policy refuses maintenance with doctor guidance |
| March-style root-shell sudo / blind boot copy / riot-owned executable | Explicit root migration replaces reviewed managed files with fresh signed root-owned inodes and signed boot verification; review retained administrator policy/drop-ins |
| Unsupported apt/systemd/sudo or unsafe privileged paths | Leave monitoring available; resolve compatibility or policy through an explicit administrator change, without an inline apt fallback |
| DNF / non-Linux | Existing supported behavior; no claim of detached apt recovery |

The installer authenticates all required assets before replacement, validates candidate/effective sudo policy and units, checks shared maintenance state and takes native apt/dpkg POSIX locks before stopping the agent. Busy package work or unsafe/unresolved state aborts. Backups preserve prior managed files; validation failure preserves/restores the managed policy. It does not remove unknown administrator rules. Use the printed backup path for review. Privileged assets are **not** refreshed by subsequent agent-only updates; root installation is required again when they change.

## Read-only verification and results

With patching enabled, run doctor as the service account (flags precede the subcommand):

```bash
sudo -u riot /usr/local/bin/riot-agent -config /etc/riot/agent.yaml doctor
```

The **Safe OS Maintenance** section checks trusted paths, supported versions, fixed unit/boot policy, exact effective sudo grants and lock-aware apply protocol. Root-run doctor lists privileges for `riot`, rather than treating root's capabilities as proof. Denial probes use `sudo -n -l` only; they never execute the listed commands. When `commands.allow_extra_updaters` lists an updater, doctor also prints an **Extra Updaters** section (see [Check with doctor](#check-with-doctor)). Doctor does not repair packages, start services, download helpers or overwrite state.

Administrators can inspect local evidence without starting maintenance:

```bash
sudo visudo -cf /etc/sudoers
systemctl show riot-maintenance.service -p ActiveState -p SubState -p Result
systemctl cat riot-maintenance.service
sudo journalctl -u riot-maintenance.service --no-pager
sudo cat /var/lib/riot-maintenance/current.json
```

Match the record `command_id` to the dashboard/API command before attributing a result. Records include phases, timestamps, outcome/exit status, interruption, hold cleanup and proven version changes. Command results use existing output redaction and delivery. Captured output is capped at 256 KiB with truncation marked; journald retains fuller local output subject to host journal policy and may contain operational details.

Terminal history is immutable and never automatically expired or deleted. A capacity gate of **32 terminal records or 16 MiB**, reserving space for a maximum-size next record, refuses new work before package mutation. At capacity, review evidence and arrange explicitly approved root archival/cleanup. Preserve current/unfinished records and hold-recovery evidence; do not casually delete records or the exclusion/native package locks. This guide intentionally provides no automatic history purge.

## Validation gate and remaining operational risks

QA exercised real Linux locks/process groups, observer death, signed installer/visudo/OpenSSL/systemd-analyze checks and isolated actual apt recovery/removal-refusal fixtures. Those containers did **not** run systemd as PID 1. Before production enablement, record a disposable supported systemd host's versions and test actual fixed-unit activation, harmless maintenance surviving agent stop/restart, same-UUID reconnect/result delivery, apply/boot refusal during active/deactivating cleanup, and runtime/TERM/KILL cgroup cleanup. Unit syntax validation does not establish those service lifecycle properties. See the [QA report](../qa/MAINT-RECOVERY-001-qa-report.md).

No-removal is not rollback. Trusted package scripts may restart services, migrate applications, change configuration or mounts; host OOM, power loss and root intervention can still interrupt work. Review application-specific recovery before authorizing a repair on a host with pending migrations.

Vermont's separate legacy privilege hardening was verified, but this new worker has not been deployed there by this story. Its pending OpenMediaVault writecache repair/migration has not been run. Neither implementation nor QA authorizes that package/storage-changing operation or an automatic release/deployment.
