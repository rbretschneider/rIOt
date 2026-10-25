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
| `/usr/local/lib/riot/riot-maintenance` | root:root 0755, signed fixed worker; accepts no arguments |
| `/usr/local/lib/riot/riot-agent-apply` | root:root 0755, signed agent installer with shared exclusion protocol |
| `/etc/systemd/system/riot-maintenance.service` | root:root 0644, fixed independent service |
| `/var/lib/riot/maintenance-request.json` | Agent-written bounded input; never a root output destination |
| `/var/lib/riot-maintenance/` | root:riot 0750; root-owned results readable by the agent, files 0640 |
| `/run/lock/riot-maintenance.lock` | root:root 0600, shared nonblocking flock; never unlink or replace it to bypass contention |

The worker request is capped at 4096 bytes and accepts exactly protocol version, canonical command UUID, closed mode and reboot-class boolean. Missing/duplicate/unknown/null/trailing/malformed fields fail. Safe file access rejects symlinks, unsafe owners/modes/parents, nonregular files and oversized data. Root publishes only beneath its validated state directory.

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

The **Safe OS Maintenance** section checks trusted paths, supported versions, fixed unit/boot policy, exact effective sudo grants and lock-aware apply protocol. Root-run doctor lists privileges for `riot`, rather than treating root's capabilities as proof. Denial probes use `sudo -n -l` only; they never execute the listed commands. Doctor does not repair packages, start services, download helpers or overwrite state.

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
