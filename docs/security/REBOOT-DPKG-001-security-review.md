# Security Review Report
**Story ID:** REBOOT-DPKG-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-24
**Verdict:** APPROVED WITH CONDITIONS

Inputs: `docs/requirements/REBOOT-DPKG-001-frd.md`, `docs/architecture/REBOOT-DPKG-001-add.md`, the origin finding
INFO-4 in `docs/security/PATCH-FLOW-001-security-review.md`, the Hawaii grounding notes, and the code the ADD changes
or depends on:
- agent: `internal/agent/commands.go` (`executeAction`, `handleReboot`, `maybeRebootAfterPatch`, `execReboot`,
  `handleShutdown`), `reboot_auth.go`, `reboot_callsites_test.go`, `maintenance.go` (`maintenanceBlockerFor`),
  `command_lifecycle.go` (`submitCommand`, `newCmdRun`, `runAction`, `completeCommand`, `waitForCommands`,
  `recoverInterruptedCommands`), `updater.go`
- models: `internal/models/command_policy.go`
- server: `internal/server/commands/service.go` (creation, conflict check, cancel), `sweeper.go`,
  `internal/server/notify/*`, `internal/server/updates/checker.go`
- units: `scripts/riot-agent.service`, the unit written by `scripts/install.sh`

---

## Threat Model Summary

### Assets
- **Package-state integrity.** The story exists to protect it: no rIOt reboot may cut a dpkg transaction.
- **Host availability and security-patch activation.** A reboot that is skipped leaves the host on the old kernel.
  A reboot that is delayed can land after the operator expected it, or after a maintenance window.
- **Truth of the audit trail.** The standalone result, the post-patch warning event and the server row.
- **The C-8 invariant.** `execReboot` is reachable only from the three reviewed, `allow_reboot`-guarded callers.

### Threat Actors
- **Local unprivileged user on a managed host.** This actor gains something new in this story. They can read
  `/proc/locks`, create their own locks, name their own processes, create btrfs subvolumes and, where user
  namespaces are enabled, mount a private tmpfs. They cannot open the 0640 root lock files.
- **Root on the host and environment faults.** An admin deletes lock files ("rm lock-frontend" is common advice), a
  future unit hardening narrows the agent's view, a kernel changes the `/proc/locks` format, the agent is OOM-killed
  (`MemoryMax=50%`, `OOMPolicy=continue`), or agent and server versions skew.
- **Admin-session holder.** Can already issue `reboot`. This story only changes the command's timing.
- **Device-key holder or compromised agent.** Already holds `sudo systemctl reboot`. Nothing new.

### Attack Surface Introduced
- A new agent read of untrusted-volume kernel data (`/proc/locks`) and of attacker-nameable text (`/proc/<pid>/comm`).
  Both flow into the command result, a warning event (forwarded unescaped by the notifiers), and logs.
- A new wait of up to 10 minutes inside every reboot path, holding the host key on the agent (and, for the standalone
  path, the host-keyed server row for up to 20 minutes).
- New budget values for `reboot` (agent 12m, server 20m).
- No new endpoint, privilege, sudoers rule, file, column or dependency.

---

## Findings

No CRITICAL or HIGH findings. The design fails in the safe direction (no reboot) on every branch the ADD
enumerates. The findings below are about branches it does not enumerate, where a narrowed view reads as "free".

### MEDIUM

#### SEC-001: A narrowed view of the host reads as "free" or "absent" and fails open, silently
**Severity:** MEDIUM
**Domain:** Business logic / fail-closed integrity (BR-001, FR-010)
**Location:** ADD AD-001 steps 3, 6 and 7, AD-002 loop (`case Free, Absent`), §10 R-2
**Description:**
The probe distinguishes "unknown" from "free" only for errors it can see. Three conditions produce no error, only
an empty answer, and each one leads to a reboot during a running transaction:

1. **Pid-namespace filtering.** In current kernels, `locks_show` omits every lock whose holder pid does not
   translate into the pid namespace of the `/proc` mount being read. Such locks are skipped entirely. They do not
   appear with pid 0. The ADD's statement "`0` = not visible in our pid namespace" and R-2's claim that detection
   "works whatever the namespaces" are therefore wrong. If the agent ever reads a `/proc` belonging to a private pid
   namespace, every probe returns `Free`. That happens with `PrivatePIDs=yes` (available in systemd 257, which Debian
   13 and Hawaii ship, and a natural next step for the AGENT-HARDEN series) or with any containerised agent.
   Today's unit has no private pid namespace, so the exposure is latent, not current.
2. **Lock files gone while the holder runs.** `Absent` is inferred only from `stat` ENOENT on two paths. Suppose root
   deletes the lock files while `unattended-upgrades` holds them; this is the usual "could not get lock" advice. The
   holder keeps its lock on the unlinked inode, both paths return ENOENT, and the probe returns `Absent` without
   reading `/proc/locks`. The reboot proceeds. A mount namespace that hides `/var/lib` (for example
   `TemporaryFileSystem=`) has the same effect.
3. **Held, then Absent within one attempt.** The loop treats `Absent` like `Free`, so a wait that observed `Held`
   clears as soon as the files disappear (case 2 during the wait).

**Attack Scenario (fault):**
1. At 03:00 `unattended-upgrades` runs a kernel upgrade on vermont. An admin's `apt install` fails with "could not
   get lock", and the admin deletes `/var/lib/dpkg/lock*` as a search result suggests.
2. A maintenance-window auto-patch on the same host finishes its run and starts the post-patch reboot. The probe
   sees `Absent` and the reboot starts.
3. `unattended-upgrades` is killed mid-`dpkg --configure`. The vermont incident class recurs, and nothing records
   that the gate was bypassed.

**Required Resolution (properties):**
- (a) The probe returns `Unknown` when the `/proc` it reads does not show the whole host's locks, that is, when the
  agent is not in the initial pid namespace. The check must be unprivileged and cheap. Reading the agent's own
  `NSpid` in `/proc/self/status` is one way to do it.
- (b) `Absent` is returned only when the host is not a dpkg host, corroborated by something other than the two lock
  paths: dpkg's own presence. When dpkg is present and both lock files are missing or invisible, the result is
  `Unknown`. This reads FR-006 by its intent ("a dnf or other non-Debian host") and needs no FRD change.
- (c) Within one attempt, a transition from `Held` to `Absent` never clears. It is either `Unknown` or it keeps
  waiting.
- (d) Correct the pid-0 semantics in AD-001 and R-2. Pid `0` lines are not expected. Negative pids (remote/NFS
  holders) and `-1` (OFD) parse as "not visible".
- (e) Tests: a `Free` result where `NSpid` shows nesting becomes `Unknown`; dpkg present with both lock files absent
  becomes `Unknown`; a host without dpkg and without lock files is `Absent` (AC-012 unchanged); `Held` then `Absent`
  never reboots. Add a structural test that fails if the agent unit gains `PrivatePIDs=` without this check.

**Blocks:** No. Condition C-1.

---

### LOW

#### SEC-002: A local unprivileged user can veto rIOt reboots on any host, and disguise the holder
**Severity:** LOW
**Domain:** Abuse / availability of security-patch activation
**Location:** ADD AD-001 (1 MiB cap, anonymous-device leniency, `holderText`), §10 R-3 and R-4
**Description:**
The gate gives every local user a way to prevent rIOt reboots. That is the cost of BR-001, and it is acceptable
because each skip is reported. But the ADD understates it in three ways:
- **Every filesystem, not only btrfs.** POSIX locks on alternate bytes of a file the user owns do not merge, and
  Linux does not enforce `RLIMIT_LOCKS`. About 20,000 such locks push `/proc/locks` past 1 MiB, and every probe then
  returns `Unknown` ("lock information exceeds 1 MiB"). R-3's "ext4/xfs hosts are unaffected" is therefore false. No
  holder is named.
- **Deterministic on major-0 filesystems.** The inode collision R-3 describes needs no luck. An unprivileged user can
  `btrfs subvolume create` in their home and allocate objectids up to the target inode. Where user namespaces are
  enabled, they can mount a private tmpfs and do the same. That lock holds the gate for the full 10 minutes per
  attempt, along with the host key.
- **The named holder is chosen by the attacker.** `comm` is set with `prctl(PR_SET_NAME)`, so the report can read
  "held by unattended-upgr (pid 4242)". "Diagnosable" holds only if the operator checks the pid.

Exact matching would not remove this veto, because the flood still works. It would only make btrfs and overlay roots
fail open. So the leniency does not change the security posture. It adds a second route with a better disguise (see
INFO-2).

**Attack Scenario:** A local user on a shared homelab host wants time to use a kernel LPE that the nightly auto-patch
has already fixed. They keep 25,000 byte-range locks open. Every post-patch reboot skips with a warning event that
names no process, and the host stays on the vulnerable kernel until someone investigates.

**Required Resolution (properties):**
- (a) R-3 and R-4 state the real capability: any local user, any filesystem, by lock volume; deterministic inode
  collision on major-0 filesystems. Ryan accepts this as the price of fail-closed. The README notes it.
- (b) A `Held` report does not rest on `comm` alone. It carries the holder's real uid when that is readable, so that
  a non-root holder of "the dpkg lock" stands out, and it marks inode-only (lenient) matches as such. Both are
  additive text (NFR-005). Holder text that cannot be read stays "holder not visible".

**Blocks:** No. Condition C-2.

#### SEC-003: "Gate precedes execReboot" is enforced textually, so a discarded gate result passes
**Severity:** LOW
**Domain:** Authorization invariant (C-8, FR-021)
**Location:** ADD AD-002 placement, §8 AC-020; `reboot_callsites_test.go`
**Description:**
The new test passes when the source text before `execReboot` contains `awaitRebootClearance`. That is also true for
a comment, for `_ = a.awaitRebootClearance(...)`, or for a call whose `Cleared` is never checked. The existing C-8
helper has two related gaps:
- It stores one boolean per function (`sites[fn.Name.Name] = …`), so a second, unguarded `execReboot` earlier in the
  same function is overwritten by a later guarded one.
- It only detects calls through `a.execReboot(...)`. A method value (`f := a.execReboot; f()`) is invisible.

The gate is now the main safety control of three paths. Its enforcement should not be weaker than a code review.

**Attack Scenario (regression):** A later story adds a "force after timeout" branch, or refactors one caller to log
the clearance and call `execReboot` unconditionally. Both tests stay green, and BR-002/FR-008 are silently broken.

**Required Resolution (properties):**
- (a) `execReboot` cannot start a reboot unless it is handed proof that the gate returned `Cleared` for this attempt.
  For example, it takes the clearance value and refuses when `!Cleared`. The proof must be checked by the compiler or
  at runtime, not by source text.
- (b) The call-site test records every reference to `execReboot`, including method values, and checks each one; it
  does not store one result per function. C-8's three-caller set and its `AllowReboot` requirement stay as they are.
- (c) Test: a clearance with `Cleared == false` passed to `execReboot` starts no reboot and returns an error.

**Blocks:** No. Condition C-3.

#### SEC-004: Version skew is not prevented by release order, because agents can update ahead of the server
**Severity:** LOW
**Domain:** Audit truthfulness (FR-016)
**Location:** ADD AD-004, §10 R-5
**Description:**
R-5 relies on "upgrade the server first, which is the normal flow". But the agent's automatic updater installs any
release the server advertises, and an old server polls GitHub for new releases (`internal/server/updates/checker.go`).
So new agents can run against an old server (600 s reboot deadline) without anyone choosing that order. If the lock
frees in the last minute before the old deadline, the row becomes `timed_out` and the late result is rejected, but
the host still reboots. An operator who reads `timed_out` may re-issue the reboot. The result is at most one extra,
operator-driven reboot, so the impact is low.

**Required Resolution:** Correct R-5 to describe the auto-update path. The CHANGELOG and README upgrade notes tell Ryan
to upgrade the server before the release reaches agents. No code change is required (C-4).

---

### INFORMATIONAL

#### INFO-1: `/proc/locks` parsing and `comm` handling are sound, with small tightenings
- **Kernel-formatted input.** Userland cannot inject text into `/proc/locks`: every field is a number or a fixed
  token, and a line is under about 200 bytes. The only hostile dimension is volume (SEC-002). The 1 MiB cap must stop
  the read at the cap, through a limited reader. A cap checked after reading does not bound the read. The 4 KiB line
  cap is sufficient.
- **Known variants need fixtures, not `Unknown`.** Strict parsing means any kernel format change makes every reboot
  on every dpkg host skip. That fails safe and is visible, but it is fleet-wide. Add fixtures for:
  - the `ACCESS` type, `*NOINODE*` with `<none>:0`, and `FLOCK  MSNFS`;
  - `LEASE BREAKING` and `LEASE BREAKER` (a `<none>:0` line);
  - `UNKNOWN UNKNOWN`;
  - nested waiters (`1:  -> …`, with extra indentation);
  - `OFDLCK` with pid `-1`, and a negative remote pid.
- **`comm` path.** Build `/proc/<pid>/comm` only from the parsed base-10 integer and only for pid > 0. Never splice in
  the raw token. A reused pid can misname the holder; that is cosmetic.
- **`comm` charset.** Printable ASCII is enough for logs, which are JSON-escaped, and for the dashboard, which React
  escapes. But warning events reach Discord, Slack, ntfy and gotify unescaped (`internal/server/notify/*`), and 16
  printable bytes can carry a markdown link (`[ok](http://a.b)`). Restrict `comm` to letters, digits and `._-+:`.
  Map everything else to `?`. The same applies to any holder text added for SEC-002.
- **Kernel cost of the read.** Each `/proc/locks` read chunk takes the global file-lock locks exclusively, so lock
  operations everywhere, dpkg's included, stall for microseconds to milliseconds per chunk. That is the same cost as
  `lslocks`, and any local user can already impose it. FR-007's "never delay" holds only in that approximate sense.
  Reading with a large buffer, so a typical file comes in one `read()`, keeps the number of chunks low.
- **The 1 s cap is checked between lines, not enforced per syscall.** This is acceptable, because a procfs read does
  not block on I/O.

#### INFO-2: Anonymous-device leniency is the right trade-off
- **False negative without leniency.** On btrfs, and on overlay and tmpfs-root setups such as Raspberry Pi overlay
  mode, the device `stat` reports and the device `/proc/locks` reports differ. The gate would fail open on exactly the
  hosts it cannot otherwise see, and the story would not deliver INFO-4 there.
- **False positive with leniency.** A skipped or delayed reboot, named in the report, at most once per attempt. Two
  realistic sources:
  - systemd-nspawn or LXC containers cloned from a snapshot of the host root keep the host's inode numbers, so apt
    in such a container delays a host reboot;
  - ZFS datasets are major 0 and widen the surface without needing the leniency.

  Both fail safe.
- **Exact match cannot be forged.** No unprivileged user can open or hard-link the 0640 root lock inode on its own
  device (`protected_hardlinks`). The leniency trades that property for coverage. Since the lock flood (SEC-002)
  already gives the same veto on every filesystem, the trade costs nothing in practice.
- **Unverified premise.** The grounding verified ext4 on NVMe (259:2) only, not btrfs. The ADD's btrfs claim is
  plausible (`lslocks` has the same mismatch), but the AC-002 "btrfs" fixture is synthetic. C-6 asks for a real
  major-0 sample, or an explicit "unverified" note.

#### INFO-3: The final-check-to-reboot window is wider than "milliseconds", and there is no cheap fix
The window runs from the final probe until systemd stops user sessions and services. That covers `sudo`/PAM start-up,
the job queueing, and session teardown: well under a second to a few seconds, not milliseconds. Inside it, an admin's
interactive `apt` can take the lock, and it is then killed. New `unattended-upgrades` runs cannot start, because
timers stop and the shutdown job conflicts. A run already in progress is covered by its own logind delay inhibitor,
which a root `systemctl reboot` still honours.

The real tightenings are holding the dpkg lock across the reboot, or `--check-inhibitors`. Both need a new sudo rule
or a new argv, and NFR-001 forbids that. The ADD already does what is cheap: guards before the final probe, and
nothing between the final probe and `execReboot`. Correct the wording of NFR-004/R-1 to "sub-second to a few seconds"
in the docs. No condition beyond the existing AC-001 ordering test.

#### INFO-4: Budget split and the host key: bounded, with operator-visible side effects
- **Positive.** At most one gate per agent: the standalone path and both post-patch reservations take the same host
  key, so waits cannot stack. `shutdown` keeps 1m/10m and gets no gate (AD-004, AC-020).
  `ExecDeadline(reboot) = 20m > 12m + 5m`. The `cr.ctx` 12m budget is above the about 10m07s worst case, and if it
  fires the outcome is a truthful skip, never a reboot.
- **Host-busy window.** For up to about 10.5 minutes, other host-keyed agent work is refused with `conflict`:
  `docker_*` mutations, `os_update`, `agent_update` and the automatic agent updater. Docker auto-updates in the same
  maintenance window count as failures for `AutoUpdateBackoff`, which caps at 6h. The window is bounded and its cause
  is visible.
- **Lost result.** If the agent is hard-killed mid-wait (OOM under `MemoryMax=50%`, SIGKILL), recovery drops the
  reboot marker without a result. The row then blocks host-keyed creation server-side until the 20-minute sweep (it
  was 10). No reboot happens, which is the safe direction.
- **No abort and a delayed surprise reboot.** `Cancel` applies only to queued commands. A dispatched reboot that is
  waiting cannot be withdrawn from the dashboard, and it may start up to 10 minutes after Ryan pressed the button, or
  up to 10 minutes after a maintenance window ends for auto-patch. That was already possible, by the length of the
  `os_update` run. For Ryan's awareness; the `dpkg_lock_wait` progress step the ADD defers is the natural follow-up.

#### INFO-5: Bounded growth holds
Per attempt: at most one wait, at most 303 probes (each read at most 1 MiB, streamed, O(1) memory), at most one
`systemctl show` per free observation, at most 2 Info lines from the gate, one outcome line, at most one event and at
most one coalesced telemetry trigger. The story writes no files, rows or migrations. A permanently held, flooded or
unreadable lock produces one skip per operator or window attempt, never a loop (BR-002, INFO-3 of PATCH-FLOW-001
preserved). Events fall under the existing 90-day retention. Nothing grows with wait length or failure count.

---

## Positive Observations
- Detection uses only `stat` and world-readable `/proc` files: no exec, no sudo, no lock-file open. AC-018's AST ban on
  locking calls makes FR-007 structural, not just a convention.
- Every error the probe can see (stat errors other than ENOENT, a missing `/proc/locks` under `ProcSubset=pid`, an
  oversize file, a long line, a malformed line, a slow read) maps to `Unknown`, which means an immediate skip.
  `/proc/locks` content is never echoed, only a line number.
- A single gate with one deadline (`firstHeld + 10m`) that survives a re-taken lock, with guards re-run after any
  further wait and a fresh probe as the very last check (FR-012).
- Shutdown handling polls `shuttingDown` rather than relying on `cmdCtx`, which is cancelled only after the 20 s
  grace. This was found by reading the lifecycle code, not assumed.
- `ReasonTimeout` is deliberately not used for a lock timeout, so the record never claims a budget failure.
- The budget is split for `reboot` only, the queued TTL is left alone (a stale reboot cannot fire later), and the
  server consumes `exec_deadline_s` per row, so no schema or sweeper change is needed.
- The unauthorized path's silent post-delay abort now emits an event. This closes a visibility gap from PATCH-GATE.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.** No CRITICAL or HIGH finding. Every branch the ADD enumerates fails closed, and the gate
can only delay or prevent a reboot (BR-003). SEC-001 is a set of unenumerated "empty answer" branches. They are
latent today: the agent shares the host's pid namespace, and lock-file deletion needs root. They are fixed inside
AD-001 and AD-002 without any architectural change. The LOW findings correct residual statements and harden the C-8
enforcement.

### Conditions (engineers implement; QA verifies)
1. **C-1 (SEC-001).** The probe returns `Unknown` when it is not reading the initial pid namespace's `/proc`, and
   when dpkg is present but both lock files are missing or invisible. A `Held`→`Absent` transition within one
   attempt never clears. Correct the pid semantics in AD-001 and R-2. Add the tests in SEC-001 (e), including the
   unit-file regression test.
2. **C-2 (SEC-002).** Rewrite R-3 and R-4 (any filesystem, by lock volume; deterministic collision on major-0
   filesystems). A `Held` report carries the holder's uid when it is readable, and marks inode-only matches. The
   README notes the local-user veto.
3. **C-3 (SEC-003).** `execReboot` refuses unless it is handed a `Cleared` clearance for this attempt. The call-site
   test checks every reference to `execReboot` individually, including method values. Add the negative test.
4. **C-4 (SEC-004).** Correct R-5 for the automatic-updater path. The CHANGELOG and README say to upgrade the server
   before agents.
5. **C-5 (INFO-1).**
   - The `/proc/locks` read is limited at the reader.
   - Add the kernel-variant fixtures listed.
   - The `comm` path is built from the parsed integer, for pid > 0 only.
   - Holder text is limited to `[A-Za-z0-9._+:-]`, with everything else mapped to `?`.
6. **C-6 (INFO-2).** The implementation report records the §12 note 11 host verification on Hawaii (ext4). It also
   records either one real major-0 sample (btrfs or an overlay root: `stat` dev/ino against the `/proc/locks` line,
   as a test fixture) or an explicit statement that the lenient path is unverified on real hardware.
7. **C-7 (QA gate).** These tests exist and pass:
   - AC-010, AC-011, AC-012, AC-014, AC-015, AC-016, AC-018 and AC-020;
   - the unchanged C-8 test;
   - the new C-1 and C-3 tests.

   QA also confirms that the DoD greps are clean (no `time.Now`/`time.Sleep` in the gate or the probe) and that
   `scripts/`, `go.mod` and the migrations show no diff.

### For Ryan's decision (not blocking)
- SEC-002: accept that any local user on a managed host can veto rIOt reboots, which is visible each time, as the
  price of fail-closed.
- INFO-4: a waiting reboot cannot be cancelled from the dashboard and may fire up to 10 minutes late. Consider the
  deferred `dpkg_lock_wait` progress step as a follow-up.
