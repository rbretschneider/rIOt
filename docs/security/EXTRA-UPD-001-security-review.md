# Security Review Report

**Story ID:** EXTRA-UPD-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-26
**Inputs:** `docs/requirements/EXTRA-UPD-001-frd.md` (rev 2), `docs/architecture/EXTRA-UPD-001-add.md` (rev 1, at `f5b225d`),
the DietPi source copies at MichaIng/DietPi @65a12a6 (`dietpi-update`, `func/dietpi-globals`, `cron.daily/dietpi`), and the code
listed in ADD §4 at `186a575`.
**Verdict:** APPROVED (revision 3 re-review; rev 2 APPROVED; rev 1 APPROVED WITH CONDITIONS)

---

## Threat Model Summary

### Assets

- Root on the DietPi hosts (PiVPN, PiCUPS). The new capability: `riot-maintenance` (root) runs `/boot/dietpi/dietpi-update 1`.
  That script downloads code from GitHub and runs it as root (`pre-patches`, the tarball copied into `/boot` and `/`).
- The integrity of the DietPi installation. A step killed mid-run leaves services stopped and the system partly patched, and
  on PiVPN that can mean losing remote access.
- The root-side opt-in: `/etc/riot-maintenance/extra-updaters.json` and the unit drop-in `50-riot-extra-updaters.conf`.
- The root maintenance record store (`/var/lib/riot-maintenance`, root:riot 0750), and the request inbox (riot-owned).
- The reboot gate (PATCH-FLOW-001, REBOOT-DPKG-001) and the server's command, event and badge data.

### Threat Actors

- **Compromised unprivileged `riot` agent.** It controls the inbox request, telemetry, events and `agent.yaml`, and it can run
  as a local process that races root-owned work in shared directories.
- **Rogue server or API client.** It controls command params and can trigger dispatch.
- **Other local non-root user.** On DietPi the default `dietpi` user usually has passwordless sudo, so `riot` is the
  meaningful non-root actor.
- **Supply chain.** DietPi's GitHub repo, or a non-default `DEV_GITOWNER` fork.

### Attack Surface Introduced

- A root execution of DietPi, gated by request v2 ∩ root policy ∩ fresh root detection ∩ safe-exec ∩ dpkg-lock probe.
- Request/record protocol v2, the `--capabilities` probe (run as riot), the root policy file, the drop-in, and the
  installer's classifier and `current.json` validator changes.
- The server-owned param `extra_updater_budget`, the operator param `exclude_extra_updaters`, two new agent event types,
  the scheduler trigger on extra-pending, and the badge classification.

---

## Findings

### HIGH

#### SEC-001: The DietPi run works in a shared `/tmp`, so `riot` can race it to get root

**Severity:** HIGH
**Domain:** Business logic / TOCTOU, privilege escalation
**Location:** ADD AD-007 and §0.1 OQ-A4 ("the unit must keep a shared `/tmp` (no `PrivateTmp`)"); AD-004 `Apply()` `Dir /`;
DietPi `func/dietpi-globals` `G_INIT` L106–155; `dietpi-update` L92–96, L265–286

**Description:**
`Dir=/` does not hold. DietPi's `G_INIT` always changes into `G_WORKING_DIR=/tmp/DietPi-Update`.

1. If that directory exists and no other `dietpi-update` is running, root deletes it with `rm -R`.
2. Root then runs `mkdir -p "$G_WORKING_DIR" && cd "$G_WORKING_DIR"`. `mkdir -p` also succeeds on an existing directory,
   and the check is not repeated.
3. In that directory, as root, the script:
   - downloads `version` and sources it (`. version`);
   - downloads `pre-patches`, runs `chmod +x` and executes it;
   - unpacks the update tarball and `cp -a`s it into `/boot` and `/`.

A directory owned by `riot` inside sticky `/tmp` lets `riot` rename or replace any of those files. The ADD keeps `/tmp`
shared on purpose, so DietPi's concurrency guard can see the cron check.

**Attack Scenario:**
1. On PiVPN (opted in, update pending), the compromised `riot` runs a tight loop: `mkdir /tmp/DietPi-Update; chmod 777` on
   success.
2. `riot` writes a v2 request with a fresh UUID and starts the unit through its existing sudo grant.
3. The worker passes every gate and starts `dietpi-update 1`. `G_INIT` sees the directory, `pidof` shows a single instance,
   and it runs `rm -R`. Within microseconds the loop re-creates the directory, and root's `mkdir -p` and `cd` land in the
   directory `riot` owns.
4. `riot` watches with inotify. When `pre-patches` appears it swaps the file between root's `chmod +x` and the exec. The
   payload runs as root.
5. If the race is lost, `riot` retries on the next run. Today DietPi's daily cron check already offers one such attempt per
   day. rIOt makes the attempts on demand, and the apply mode has much wider windows (tarball → `cp -a /`).

**Required Resolution:**
- When the worker runs a DietPi step, no non-root user may be able to create, own or modify DietPi's working directory. The
  step's `/tmp` must not be a world-writable location shared with the host.
- The worker must check, before it starts the step, that this isolation is in effect. If it is not, the worker must fail
  closed: no run, and a recorded skip or failure with a note.
- Taking away the shared `/tmp` removes DietPi's own guard against the cron check. The worker must therefore refuse to start
  while any other `dietpi-update` process is running. The residual case (a cron check that starts mid-step fails on the
  apt lock, or the step fails truthfully) must be documented.
- Amend AD-007 and OQ-A4 to replace the "must keep a shared `/tmp`" rationale.
- The ops docs must state the remaining pre-existing exposure: DietPi's own cron and postboot run `dietpi-update 2` as root
  in the shared `/tmp`. Ryan should consider reporting it upstream.

**Blocks:** Implementation of the step (not the rest of the story). The fix is unit-scoped and a pre-start check, with no
architectural change.

---

### MEDIUM

#### SEC-002: The worker's 80-minute budget does not depend on the 85-minute backstop actually being in force

**Severity:** MEDIUM
**Domain:** Business logic / availability and integrity
**Location:** AD-003, AD-006, §7.1 step 1; ADD §12 note 7 ("policy file and drop-in are written … before `daemon-reload`")

**Description:** For any v2 request, the worker uses `ExtendedWorkerBudget = 80m`. Only the agent's readiness check looks at
the effective `RuntimeMaxUSec`, and the agent is the actor this review assumes may be compromised. Suppose the policy file
lists `dietpi` while systemd still enforces 35 min. That happens after an installer interrupted between writing the files
and `daemon-reload`, after a root-side drift, or because the write order allows it. The worker then starts DietPi at about
minute 3–32, and systemd SIGTERMs and SIGKILLs it at 35 min, in the middle of `cp -a` or the patches, with services stopped.

**Attack Scenario:** On a host in that state, a compromised `riot` sends a v2 request. A benign agent would decline. DietPi
is killed mid-update, so PiVPN's VPN service stays stopped and DietPi is half-patched.

**Required Resolution:**
- The worker must not start an extra-updater step unless it has verified that the service's **effective** runtime backstop
  is at least the extended budget plus the termination grace. Otherwise it fails closed with `insufficient_time` and a note.
- The installer must order its writes so that the policy lists an updater only after the extended backstop is in effect
  (written, reloaded, verified). On revocation, the policy must be emptied before the backstop is lowered.

#### SEC-003: A non-default `DEV_GITOWNER` makes rIOt run third-party code as root, unattended, and the source resolution differs from DietPi's

**Severity:** MEDIUM
**Domain:** Supply chain
**Location:** AD-004 notices; §10 OQ-A7; DietPi `dietpi-update` L48–51

**Description:**
- DietPi fetches from `DEV_GITOWNER` in `dietpi.txt` (first match, default `MichaIng`). It does **not** fall back to
  `.version`'s `G_GITOWNER`, which the ADD's "effective" value does.
- A notice in the dashboard is not a control. Before this story, fork code ran only when Ryan ran `dietpi-update` by hand.
  With it, the fork code runs as root in every patch window.

**Attack Scenario:**
1. Ryan points PiCUPS at a contributor's fork to test a fix, as DietPi documents for testing PRs, and forgets to revert it.
2. The fork's owner account is later compromised and pushes a malicious `pre-patches`.
3. The next scheduled window runs it as root on PiCUPS. Nobody acts on the device-view notice.

**OQ-A7 answer:**
- Accept DietPi's own repository, any branch (DietPi switches branches itself, for example to `9`).
- Do **not** accept a non-default owner for rIOt-initiated runs.

**Required Resolution:**
- The worker must resolve the owner exactly as DietPi does.
- It must not start the step when the resolved owner is not exactly `MichaIng`, or is unreadable.
- The skip and the resolved owner and branch must be recorded in the root record and shown in the result.
- The telemetry notice must use the same resolution.

---

### LOW / INFORMATIONAL

#### SEC-004: The root trust check covers only the entry point, and the ADD's vfat premise is inaccurate

**Severity:** LOW
**Location:** AD-005 safe-exec; AD-004 detector via `OSRoot`

**Description:**
- DietPi sources `func/dietpi-globals`, `.version` and `.hw_model`, reads `dietpi.txt`, and runs other `/boot/dietpi/*`
  scripts. The run decision also trusts `/run/dietpi/.update_available` and `.live_patches`, but `OSRoot` checks only
  `O_NOFOLLOW` and that the file is regular, not its owner or mode.
- Today `/run/dietpi` is created by root with umask 0022, so a non-root user cannot write it. Nothing in the design enforces
  that, though.
- On Bookworm Raspberry Pis, FAT is mounted at `/boot/firmware` and `/boot/dietpi` is on ext4 with real permissions. On
  other boards, vfat permissions come from mount options, and a `umask=000` or `uid=` mount makes everything writable.

**Scenario:** A file in `/boot/dietpi` is made group- or world-writable (manual chmod, a lax vfat mount on the files the
check skips), or a `/run/dietpi` created by a non-root process. Either lets `riot` inject code into the root run or forge
"pending".

**Required Resolution:**
- In the worker, before it starts the step, require root ownership and no group/world write on:
  - the `/boot/dietpi` and `/boot/dietpi/func` directories and their parents;
  - `func/dietpi-globals`, `.version` and `dietpi.txt`;
  - the `/run/dietpi` flag files and their parents.
- Fail closed when any check fails.
- No vfat special-casing. Tests cover both the ext4 and vfat layouts.

#### SEC-005: The two-events-per-updater bound is enforced only by the agent

**Severity:** LOW
**Location:** AD-011; `handlers.go` `ReceiveAgentEvent`

**Scenario:** A compromised agent posts `extra_updater_finished` thousands of times against one valid os_update command ID.
Each post creates and broadcasts an event row.

**Required Resolution:**
- The server must accept at most one `started` and one `finished` event per (command_id, updater), and treat duplicates as
  no-ops.
- The command must belong to the device and be an `os_update`, as specified.

#### SEC-006: A compromised agent can make DietPi stop services over and over

**Severity:** LOW
**Location:** §10 "Compromised agent" (understates it: DietPi runs `dietpi-services stop`, apt does not)

**Scenario:**
1. `riot` fills `/` after DietPi's free-space check, so the update fails after services are stopped.
2. The flag stays, so `riot` loops new-UUID v2 requests and keeps PiVPN's VPN down.

**Required Resolution:** A root-side minimum interval between DietPi step starts, derived from root-owned history. When the
interval has not passed, the step is skipped with a recorded reason.

#### SEC-007: Hardening notes for the senior dev and QA

**Severity:** LOW / INFO

- **dpkg lock probe:** add `O_NONBLOCK` to the opens, and check that the files are root-owned regular files, as `readSafe`
  does.
- **Capability probe:**
  - include `ctime` in the cache key;
  - enforce a timeout and a 256-byte cap at the runner, not only in the parser;
  - keep the same trusted-file check as the `riot-agent-apply --maintenance-protocol` probe;
  - `--capabilities` must be handled before any file, lock, env or root access.
- **Installer:**
  - write the policy file from fixed literals, never interpolating config text;
  - keep the v2 `current.json` validator closed (exact key sets, enums, `running` rejected, note ≤ 256, versions ≤ 32,
    2 MiB);
  - verify the effective `RuntimeMaxUSec` after `daemon-reload`, as it already does for the agent's ExecStartPre (see SEC-002).
- **`extra_updater_budget` stripping:** it lives in `Dispatch`, which every command path reaches. That is correct. QA
  verifies that case variants are stripped for every action, before the params are marshalled.
- **OQ-A6, accepted.** The residual is that `agent.yaml`, which `riot` can write, drives the root opt-in on Ryan's next
  reinstall. This is the same precedent as `allow_shutdown`. The transparency line is required (§10).

---

## Positive Observations

- No sudoers line is added. The DietPi argv, env and path are compile-time constants (`ExtraCommandAllowed`). The request
  can only *ask*, and the root policy is intersected with it. That is fail closed.
- The policy file lives outside `riot`-owned `/etc/riot`, is safe-opened with root-owned parents, `nlink == 1` and 512 B,
  and treats any error as "not enabled".
- `ProtocolVersion` is untouched. v2 is gated on the probe of the binary that is really installed, the parsers stay closed
  on both sides, and v1 stays byte-identical with a golden test.
- `.version` is parsed as data with an AST-asserted absence of `exec`. Reads are bounded with a 1 s deadline, and a stuck
  read cannot pile up goroutines.
- The reboot path is unchanged. The `dietpi` pseudo-entry comes only from a root-written record and a server-set
  `include_reboot_class`, and recovered results never reboot. A package named `dietpi` gains nothing, because any non-empty
  entry already counts.
- Persist-before-act plus `Store.Begin` gives at most one run, and recovery never re-runs.
- DietPi output goes through the same `SanitizeText` + server `redact.MaskText` path as apt output (parity holds). Event
  messages are composed by the server and the origin comes from the command row.
- The badge stays display-only: `computePatchBadge` is consumed only by `fleet.go` patch-status.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.** There is no CRITICAL finding. SEC-001 (HIGH) is serious, but its resolution is limited to the
DietPi step's execution environment (unit-scoped isolation, a pre-start verification and a concurrency check) and does not
change the architecture. The senior dev must implement SEC-001, SEC-002 and SEC-003, and amend the AD-007/OQ-A4 text along
with the docs. QA must verify:

- SEC-001: a test where a non-root-owned `/tmp/DietPi-Update` cannot affect the step, and the pre-start isolation check
  fails closed;
- SEC-002: the step is refused under an effective 35-minute backstop, and the installer write order is correct;
- SEC-003: the step is refused for `DEV_GITOWNER` other than `MichaIng`, and the resolution matches DietPi's.

SEC-004 to SEC-007 are recommended in the same PR.

---

## Revision 2 Re-review (ADD revision 2, 2026-10-26)

**Final verdict: APPROVED.** No CRITICAL or HIGH finding remains. One new LOW finding (SEC-008, docs only) must be fixed
in the implementation PR. QA verifies the SEC tests named in ADD §8.

### Closure of SEC-001 to SEC-007

| Finding | Status | Evidence in ADD rev 2 and reviewer notes |
|---|---|---|
| SEC-001 HIGH | **Closed** | See the notes below this table. |
| SEC-002 MEDIUM | **Closed** | See the notes below this table. |
| SEC-003 MEDIUM | **Closed** | AD-004 and AD-005 check 4: the source is resolved with DietPi's own `sed` rule (first match, verbatim, defaults `MichaIng`/`master`), and `.version` is no longer used. The owner must equal `MichaIng` exactly; a CR or trailing blank therefore fails closed. The resolved source is stored in the record. A readiness value and a badge action item are added. A TOCTOU between this check and DietPi's own read would need root. |
| SEC-004 LOW | **Closed** | See the notes below this table. |
| SEC-005 LOW | **Closed** | AD-011: a known-ID allowlist, plus `HasCommandEvent` under a handler mutex. This holds for a single server process, which is today's deployment. A multi-replica server would need a DB uniqueness constraint (INFO). |
| SEC-006 LOW | **Closed** | AD-005 check 8: a 60-minute interval taken from root-owned history. There is no new state file, and a read error fails closed. `riot` cannot evict DietPi records: `Store.capacity` refuses at 32 records with `ErrHistoryFull` and never prunes. INFO: future-dated records are ignored, so a backward clock jump (Pi without an RTC, before NTP sync) can shorten the interval once. The impact is bounded by the other gates. |
| SEC-007 INFO | **Closed** | `O_NONBLOCK` and an ownership check on the lock probe. Probe cache key includes `ctime`, with the timeout and cap enforced at the runner. `--capabilities` is handled first. Installer writes fixed literals. v2 validator is closed. Budget stripping is tested for every action. |

**SEC-001 notes (closed)**
- AD-007: the opted-in drop-in sets `PrivateTmp=yes`. systemd's outer directory is `0700 root`, so `/tmp/DietPi-Update` cannot be reached from the host namespace. That removes the race.
- AD-005 check 9: the worker compares `(dev, ino)` of `/tmp` with `/proc/1/root/tmp`. This is a sound proof: the service shares the host pid namespace, so `/proc/1/root` is the host root. An error fails closed.
- AD-008: a `/proc` scan with `dietpi_busy` replaces DietPi's now-blind guard.
- Docs cover the residual cron/postboot exposure.

**SEC-002 notes (closed)**
- AD-005 check 6: root runs `systemctl show` with fixed argv, 5 s and 4 KiB, a strict timespan parser, a minimum of 83m30s, and it also requires `PrivateTmp=yes`.
- AD-003: the installer write order is drop-in → reload → verify → policy, and revocation writes the policy `[]` first.
- Every interrupted state fails closed.
- The installer already refuses while the service is active, so the probed value matches the running instance.

**SEC-004 notes (closed)**
- The bounded `Lstat` trust walk covers `/boot/dietpi` (depth ≤ 4, ≤ 4096 entries), `dietpi.txt`, `/run/dietpi` and their parents. Flags are believed only after the walk passes. No vfat special case.
- Correction to this review's original SEC-004 text: PiVPN and PiCUPS on 9.x may still have FAT at `/boot` until DietPi 10's firmware migration. The walk handles both layouts, so nothing changes.
- INFO: a root-owned symlink in the tree is accepted without checking its target. Only root can create one.

### New surface introduced by revision 2

- **`/proc` scan (AD-008).** It reads only `cmdline` as root (≤ 4 KiB per process, ≤ 32768 processes, ≤ 2 s) and executes nothing.
  - `riot` can fake a matching cmdline to force `dietpi_busy`. That is a truthful skip, and it is a denial of service a compromised agent can already cause by simply not sending v2.
  - `riot` cannot hide a root DietPi run, because it cannot start one: `dietpi-update` re-executes through `sudo -n`, and `riot` has no such grant.
  - No finding.
- **Root `systemctl show` (AD-005 check 6).** It uses a fixed path and argv, the fixed environment, no pager (stdout is not a TTY), and a bounded output parsed with a closed grammar. `riot` cannot influence unit properties. No finding.
- **The 60-minute interval.** It is derived from existing root records and needs no new state file. No finding beyond the INFO note on clock jumps.
- **`PrivateTmp` on the apt part (opted-in hosts only).** For security this is a strict improvement, because apt and maintainer-script temp files leave shared `/tmp`.
  - Operational INFO for QA: DietPi 10's `rpi_firmware_migration` may change mounts (`/boot` → `/boot/firmware`) from inside the service's mount namespace.
  - QA should confirm that the effective mount propagation (`MountFlags`, default `shared`) makes such changes visible on the host. Otherwise the host keeps a stale view until the reboot that the migration requires anyway.
  - This is not a security issue.

#### SEC-008: The D-10 manual log copy, run as root, can import attacker-placed symlinks

**Severity:** LOW
**Domain:** Privilege escalation through a documented operator step
**Location:** ADD D-10; §12 note on the README (`cp -an /var/tmp/dietpi/logs/. /var/lib/dietpi/logs/`)

**Description:**
- `/var/tmp` is world-writable.
- If `/var/tmp/dietpi` or its `logs` were ever created or populated by a non-root user, `cp -a` run as root copies their symlinks and ownership into the root-trusted `/var/lib/dietpi/logs`.
- `-n` protects only names that already exist. After the 10.x migration, names such as `dietpi-update.log` may not exist yet.
- DietPi later `tee`s its run log there as root, and `/var/lib/dietpi/logs` is not sticky, so the symlink is followed.

**Attack Scenario:**
1. `riot` plants `/var/tmp/dietpi/logs/dietpi-update.log -> /etc/sudoers.d/zz`. This needs a host where it could create or write that directory.
2. Ryan follows the README and runs the copy as root.
3. On the next DietPi run, root writes the run log into `/etc/sudoers.d/zz`.

**Required Resolution:**
- The documented step must never carry symlinks or non-root ownership into `/var/lib/dietpi`. Either:
  - it tells Ryan to first confirm that `/var/tmp/dietpi` and everything under it is root-owned and not group- or world-writable; or
  - it is replaced by an instruction that copies regular files only, without preserving ownership.
- It must explicitly exclude `dietpi-update_reinstalls`, which DietPi consumes as input.
- The README docs test (AC-043) should assert this wording.

**Blocks:** Nothing. Fix it in the implementation PR, and QA verifies the docs.

### Final verdict rationale

The SEC-001 to SEC-007 fixes are root-side, fail closed, and do not depend on the possibly compromised agent. Revision 2
introduced no CRITICAL, HIGH or MEDIUM issue. **APPROVED.** The senior dev must:

- implement the ADD as revised;
- fix SEC-008 in the README and ops docs.

QA verifies every `…_SEC00N_…` test in ADD §8, plus SEC-008.

---

## Revision 3 Re-review (ADD revision 3, OPS-001, 2026-10-26)

**Final verdict: APPROVED.** Revision 3 replaces the unit-wide `PrivateTmp` with a per-step launcher
(`riot-maintenance --run-extra dietpi`, AD-007). SEC-001 stays closed. SEC-008 is closed by removing the manual copy step.
One new LOW finding (SEC-009) is a correctness condition on the launcher's proofs.

### Can `riot` or a local user use the launcher mode?

No.
- The binary is not setuid.
- `--run-extra` refuses unless all of these hold: euid is 0, argv is exact, the ID is known, fd 3 is a FIFO,
  `/proc/<ppid>/exe` is `HelperPath`, and `RIOT_EXTRA_LAUNCH=1` is set.
- A non-root caller that got past those checks would still get `EPERM` from `unshare(CLONE_NEWNS)`.
- `riot` could create a user namespace first, but DietPi would then run with `riot`'s real credentials, so it gains nothing.
- The only way the worker reaches the launcher is through the fixed `ExtraCommandAllowed` entry
  `(HelperPath, ["--run-extra","dietpi"])`.
- The launcher executes only the compiled spec, with the fixed env and `chdir("/")`.
- fd 3 is set close-on-exec before the exec, and Go's exec passes no other descriptors.
- The parent checks are defence in depth only. Anyone who can satisfy them is already root.

### Is the `/tmp` isolation still sound against the SEC-001 race?

Yes.

**Mount order.** The order is correct:
1. `unshare(CLONE_NEWNS)`, which keeps the peer groups;
2. require that `/tmp` is its own mount point; otherwise fail closed, because a new mount under the shared `/` would propagate to the host;
3. make `/tmp` private (`MS_PRIVATE`), which affects only the namespace's copy;
4. remount that copy read-only, so a later `umount /tmp` exposes only a read-only view of the host;
5. stack a fresh tmpfs, whose parent is private, so nothing propagates.

**Proof before exec.** The launcher checks that `/tmp` is tmpfs, empty and owned by root, has a `(dev, ino)` different
from `/proc/1/root/tmp`, and has no `shared:` tag. Only then does it exec.

**Nothing uses the shared `/tmp` before exec.** Between `unshare` and `exec` the launcher makes mount, stat and statfs
syscalls and reads `/proc`. The Go runtime does not touch `/tmp`. DietPi's `G_INIT` therefore creates
`/tmp/DietPi-Update` inside a tmpfs that host users cannot reach. Reaching it through `/proc/<pid>/root` needs ptrace
rights over root processes.

**Mode 1777 on the tmpfs.** This is acceptable. Only processes inside the namespace can see it, and DietPi's working
directory there is created by root as `0755`.

**DietPi's mounts reach the host.** `/`, `/boot` and `/boot/firmware` stay peers of pid 1's mounts. DietPi's own mounts
and unmounts therefore propagate to the host. That fixes OPS-001 and adds no exposure, because it is the same as an
unconfined root run.

### `/var/tmp/dietpi` guard against DietPi's own use

- In the copied sources, the only use of `/var/tmp` is `Patch_10_4` (`.update_patches` L2402–2412). It runs
  `cp -an /var/tmp/dietpi/logs/* /var/lib/dietpi/logs/` and `cat …/dietpi-update_reinstalls >> …`. The second feeds
  `dietpi-software reinstall`.
- On 9.x hosts the old `dietpi-update` may also `tee` its log under `/var/tmp/dietpi/logs`.
- The revision 3 rule closes the injection and symlink-follow paths on the step's own run:
  - the tree must pass the root trust walk: root-owned, no group or world write, root-owned symlinks only, and sticky `/var/tmp` allowed only as a parent;
  - while `Patch_10_4` is pending (installed version below 10.4), the directory must exist.
- **Why this holds.** A root-owned `/var/tmp/dietpi` inside sticky `/var/tmp` cannot be renamed, replaced or written by
  `riot` after the walk.
- **When the directory is absent on 10.4 or later.** That is harmless, because nothing in the current scripts reads it.
- **Residual (INFO).** DietPi's own cron and manual runs are not covered by this guard. That exposure is pre-existing.

### SEC-008 status

**Closed by design.** `Patch_10_4` now runs normally under the shared `/var/tmp`, so the manual `cp -an` instruction is
gone. QA must confirm that the README and `os-maintenance.md` contain no manual copy of `/var/tmp/dietpi` content.

#### SEC-009: The launcher's "self" mountinfo proofs must read the calling thread's mount namespace

**Severity:** LOW (correctness of a security proof)
**Location:** AD-007 steps 2, 3 and 8 (`runtime.LockOSThread()`, then `unshare(CLONE_NEWNS)`, then parse `/proc/self/mountinfo`)

**Description:** The mount namespace is per thread. `/proc/self` names the thread-group leader. In a multithreaded Go
process the goroutine can be locked onto a non-leader thread. `unshare` then changes only that thread, while
`/proc/self/mountinfo` still shows the leader's (host) namespace. As a result:
- the propagation proof (step 3) compares the host with the host and always passes;
- the "no `shared:` tag on `/tmp`" proof (step 8) reads the wrong table.

**Scenario.**
- **Propagation.** A distro or systemd change makes `/boot` non-shared in the new namespace. The vacuous check passes, so
  DietPi's migration is hidden from the host again (OPS-001 reappears).
- **Isolation.** The statfs and `(dev, ino)` checks do resolve per thread, so isolation would still be refused correctly.
  This is why the severity is LOW.

**Required Resolution:**
- Every "self" proof must read the mount table of the thread that called `unshare` and will call `exec`. For example, use
  `/proc/thread-self/mountinfo`, or do all of the setup on the main thread, locked from `init`.
- A unit test must assert that the path used is thread-scoped.

**Blocks:** Nothing. Fix it in the implementation PR, and QA verifies it.

### Final verdict rationale

Revision 3 fixes OPS-001 without weakening SEC-001. It adds no new privilege beyond the fixed launcher entry, and every
new gate fails closed. **APPROVED.** The senior dev must:
- implement SEC-009;
- keep the docs free of any manual `/var/tmp/dietpi` copy (SEC-008).

QA verifies the AD-007 call-sequence tests, the `…_SEC00N_…` tests, and SEC-009.
