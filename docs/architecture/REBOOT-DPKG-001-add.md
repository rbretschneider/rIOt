# Architecture Decision Document

- **Story ID:** REBOOT-DPKG-001
- **FRD Reference:** `docs/requirements/REBOOT-DPKG-001-frd.md`
- **Security review:** `docs/security/REBOOT-DPKG-001-security-review.md` (APPROVED WITH CONDITIONS)
- **Origin:** PATCH-FLOW-001 security review INFO-4
- **Amends:** the power-class budgets in CMD-LIFECYCLE-001 AD-005 / AD-013, for `reboot` only (AD-004 below)
- **Author:** Architect Agent
- **Date:** 2026-10-24
- **Status:** FINAL, revision 2 (security conditions C-1 to C-6 folded in)

## Security conditions changelog (revision 2)

| Condition | Finding | What changed in this ADD |
|---|---|---|
| C-1 | SEC-001 | **AD-001:** the probe returns `Unknown` when the agent is not in the initial pid namespace (checked through `/proc/self/status` `NSpid`). It also returns `Unknown` when dpkg is present (`/var/lib/dpkg/status` or `/usr/bin/dpkg`) but both lock files are missing or hidden. `Absent` now means "not a dpkg host". The pid semantics are corrected: holders that are invisible in our namespace are omitted from `/proc/locks`, and pids ≤ 0 mean "not visible". **AD-002:** within one attempt, `Held` followed by `Absent` skips as `Unknown` and never clears. **§8:** new tests, including a unit-file regression test for `PrivatePIDs=`. **R-2** rewritten |
| C-2 | SEC-002 | **AD-001:** holder text carries the uid from `/proc/<pid>/status` when it is readable, and marks inode-only matches. **R-3 / R-4** rewritten: any local user can veto reboots on any filesystem. README note added to the docs list |
| C-3 | SEC-003 | **AD-007 (new):** `execReboot(p rebootPermit)` accepts only a single-use permit, which only the gate mints on `Cleared`. Checked at compile time (unexported type) and at runtime (the permit id is consumed with compare-and-swap). The call-site test checks every reference to `execReboot`, including method values. Negative tests added |
| C-4 | SEC-004 | **R-5** corrected for the automatic agent updater. CHANGELOG/README upgrade-order note added |
| C-5 | INFO-1 | **AD-001:** the read is capped at the reader (`io.LimitReader` plus overflow detection), with a 64 KiB read buffer and kernel-variant fixtures. The comm/status path is built only from a parsed pid > 0. Holder text is restricted to `[A-Za-z0-9._+:-]` |
| C-6 | INFO-2, INFO-3 | The inode-only path is **unverified on real hardware**: only Hawaii (ext4) has been sampled. The implementation report must record a real major-0 sample or restate "unverified". **R-1** wording corrected to "sub-second to a few seconds" |

**Product decisions adopted.** OQ-001 to OQ-005 take the FRD defaults as decided:
- a fixed 10-minute bound;
- an unknown lock state skips immediately;
- the standalone reboot uses the full bound, with the budget reconciled by the architect (AD-004);
- the phase A wording is unchanged;
- skipped reboots are never retried.

**For Ryan (from the security review, not blocking):**
- SEC-002: any local user can veto rIOt reboots. Each veto is visible, and it is the price of failing closed.
- INFO-4: a waiting reboot cannot be cancelled from the dashboard, and it can fire up to 10 minutes late.

---

## 1. Summary

A new agent-side probe finds out whether the dpkg lock is held. It uses `stat()` on the dpkg lock files and on dpkg's
own presence markers, checks that the agent's `/proc` shows the whole host (initial pid namespace), and reads
`/proc/locks` with a cap at the reader. Lines are matched on device and inode, and the holder is named from
`/proc/<pid>/comm` and the uid in `/proc/<pid>/status`. The probe needs no privilege, no exec and no new dependency.

One shared gate, `awaitRebootClearance`, runs a bounded wait, the path's guards and a final lock check. When it clears,
it mints a single-use `rebootPermit`. `execReboot` accepts only that permit, so no code path can start a reboot
without passing the gate.

The `reboot` action, and only `reboot`, gets its own budget: an agent hard max of 12 min and a server execution
deadline of 20 min. `shutdown` keeps 1 min and 10 min, and both keep the 15-minute queued TTL. The story adds no
migration, no persistent state, no sudoers or `install.sh` change, and no UI change.

## 2. Technical Context

Verified on `feat/reboot-dpkg-001` (base `0e0579b`):

- **Reboot call sites.** `execReboot()` (`commands.go:324`) runs `sudo systemctl reboot` on Linux and
  `shutdown /r /t 5` on Windows, or the test `runner` when one is set. It has exactly three callers. C-8
  (`reboot_callsites_test.go`) checks them by AST, but it stores one boolean per function and only sees
  `x.execReboot(...)` calls (SEC-003).
  1. `handleReboot(payload) (string, string)` runs synchronously inside the command, under `cr.ctx` with timeout
     `AgentHardMax("reboot") = 1m` (`newCmdRun`, `command_lifecycle.go:355`). `executeAction` has already applied the
     maintenance-blocker precheck and the host lock.
  2. `maybeRebootAfterPatch(r)` is the unauthorized PATCH-GATE path, also used by maintenance-window auto-patch. Before
     the goroutine it checks `AllowReboot`, then `maintenanceBlockerFor`, then takes the
     `TryLockAll(host, "patch-reboot:<id>")` reservation, then calls `claimPatchReboot`. The goroutine:
     1. sleeps `rebootDelay`;
     2. re-checks `shuttingDown` and `maintenanceBlockerFor`; a failure there **only triggers telemetry**;
     3. calls `execReboot`.

     A `defer Unlock` releases the reservation.
  3. `rebootAfterDelivery(id)` is the authorized phase B, a goroutine holding the same reservation. It runs
     `awaitResultDelivery` (≤ 30 s), then checks `shuttingDown`, `maintenanceBlockerFor` and `AllowReboot`, then calls
     `execReboot`. Every failure goes through `abortAuthorizedReboot`: one Warn, one warning agent event, one telemetry
     trigger.
- **Shutdown semantics.** On SIGTERM, `Run` sets `shuttingDown`, cancels the loop context and calls
  `waitForCommands(20s)`. `cmdCtx` is cancelled only after that grace period, so the wait must poll `shuttingDown`.
  Post-patch goroutines are not in `cmdWG`.
- **Seams.**
  - `a.sleep` goes through `a.pause` (tests) or a timer.
  - `a.clock()` returns the injected clock; `fakeClock.Advance` exists.
  - `a.runner` captures `execReboot`.
  - `lifecycleHarness` and `newOSUpdateTestAgent` build every test agent that reaches a reboot path.
  - `installerUnit(t)` (`doctor_test.go`) extracts the unit text that `install.sh` writes.
- **Budgets** (`models/command_policy.go`):
  - `ClassPower` (`reboot` and `shutdown`) has `QueuedTTL` 15m, `ExecDeadline` 10m and `AgentHardMax` 1m.
  - `TestCL_DeadlineInvariant` requires `ExecDeadline > AgentHardMax + 5m`.
- **Server consumers.**
  - `service.go:224` stores `exec_deadline_s` at creation.
  - `command_repo.go` stamps `deadline_at = dispatched_at + exec_deadline_s`.
  - `sweeper.go` reads each row's own values.
  - No server code reads `AgentHardMax`.
- **Pinned tests:**
  - `TestCL_AC029_ExecDeadlineTable` (reboot 10m);
  - `server/commands/service_test.go:210` (`{"reboot", nil, 15m, 600}`);
  - `TestCL_DeadlineInvariant`.
- **Units.**
  - `scripts/riot-agent.service`: `ProtectSystem=strict`, `PrivateTmp=true`.
  - The `install.sh` heredoc unit: `ProtectSystem=false`, `PrivateTmp=true`.
  - Neither has `PrivatePIDs=`, `ProtectProc=` or `ProcSubset=`.
- **Platform split pattern.** `maintenance_linux.go` / `maintenance_other.go` (`//go:build !linux`).
- **Dependencies.** `golang.org/x/sys` is indirect only. This design does not import it.

**Missing:** any dpkg lock awareness on the reboot paths, a reboot budget that fits a 10-minute wait, a skip event on
the unauthorized path's post-delay guard failures, and any non-textual enforcement of "gate before reboot".

## 3. Architecture Decisions

### AD-001: Lock detection via `stat()`, a pid-namespace check and `/proc/locks` (FR-001, FR-002, FR-006, FR-007, FR-010, FR-018, NFR-001, NFR-003; C-1, C-2, C-5)

**Decision.** A pure-Go probe lives in `internal/agent/dpkglock.go`. It has no build tag, so it is tested everywhere.

```go
type fileID struct{ Major, Minor uint32; Ino uint64 }

type dpkgLockState int // dpkgLockAbsent | dpkgLockFree | dpkgLockHeld | dpkgLockUnknown

type dpkgLockStatus struct {
    State     dpkgLockState
    PID       int64  // holder pid from /proc/locks; <= 0 = not visible (OFD -1, remote/NFS negative)
    Comm      string // sanitized /proc/<pid>/comm; "" = not visible
    UID       int64  // real uid from /proc/<pid>/status; -1 = not readable
    InodeOnly bool   // match came from the anonymous-device leniency
    Cause     string // set only for dpkgLockUnknown, <= 200 bytes
}
func (s dpkgLockStatus) holderText() string

type dpkgProbeEnv struct {
    LockPaths   []string                          // {"/var/lib/dpkg/lock-frontend", "/var/lib/dpkg/lock"}
    DpkgMarkers []string                          // {"/var/lib/dpkg/status", "/usr/bin/dpkg"}
    StatID      func(path string) (fileID, error) // errors.Is(err, fs.ErrNotExist) = absent
    Open        func(path string) (io.ReadCloser, error)
    ProcRoot    string                            // "/proc": reads <root>/locks, <root>/self/status, <root>/<pid>/{comm,status}
    Now         func() time.Time
}
func probeDpkgLock(env dpkgProbeEnv) dpkgLockStatus
func parseProcLocksLine(line string) (procLock, bool /*skip*/, error)
func lockMatches(l procLock, t fileID) (match, inodeOnly bool)
func parseNSpid(statusText string) (levels int, ok bool)
func parseStatusUID(statusText string) (uid int64, ok bool)
func sanitizeHolderName(b []byte) string
func decodeLinuxDev(dev uint64) (major, minor uint32) // glibc gnu_dev_major/minor
```

Constants:
- `dpkgProbeMaxBytes = 1 << 20` and `dpkgProbeReadBuf = 64 << 10`;
- `dpkgLineMax = 4 << 10`;
- `dpkgSmallReadMax = 8 << 10`, used for `self/status`, `<pid>/status` and `<pid>/comm`;
- `dpkgProbeTimeout = 1 * time.Second`;
- `dpkgHolderNameMax = 16` and `dpkgCauseMaxBytes = 200`.

**Algorithm of `probeDpkgLock`** (one call is one "lock check"):

1. `start := env.Now()`.
2. **Lock files.** `StatID` each of the `LockPaths`, in order.
   - `fs.ErrNotExist` → skip that path.
   - Any other error → `Unknown`, cause `cannot stat <path>: <err>`.
   - Otherwise add the result to the targets.
3. **No targets, so check whether this is a dpkg host (C-1 b).** `StatID` each of the `DpkgMarkers`.
   - Any marker exists → `Unknown`, cause `dpkg is installed but its lock files are missing or hidden`.
   - Any error other than not-exist → `Unknown`, cause `cannot stat <path>: <err>`.
   - All markers not-exist → `Absent`. Nothing under `/proc` is read, so dnf and other hosts are unaffected (FR-006).
   - `/usr/bin/dpkg` is the second marker so that a mount namespace hiding `/var/lib` cannot read as `Absent`.
4. **Pid namespace (C-1 a).** Read `<ProcRoot>/self/status` through `Open`, capped at 8 KiB. `parseNSpid` takes the
   `NSpid:` line and counts its fields.
   - Exactly one field → the agent is in the initial pid namespace, so continue.
   - More than one field → `Unknown`, cause `agent is not in the host pid namespace`.
   - The line is missing (kernel before 4.1) or the read fails → `Unknown`, cause
     `cannot verify the pid namespace: <err|no NSpid>`.

   This file is read from the same `/proc` mount as `locks`, so a `/proc` that belongs to another namespace cannot
   pass. In that case `self` does not resolve, which is an error and therefore `Unknown`.
5. **Read `/proc/locks` with a cap at the reader (C-5).**
   1. `Open(<ProcRoot>/locks)`. **Any** error → `Unknown`, cause `cannot read /proc/locks: <err>`. That includes
      `ENOENT`, which is what `ProcSubset=pid` produces.
   2. `r := bufio.NewReaderSize(io.LimitReader(f, dpkgProbeMaxBytes+1), dpkgProbeReadBuf)`. The 64 KiB buffer keeps a
      typical file to one `read()`, which keeps the kernel lock-list chunks few (INFO-1).
   3. A `bufio.Scanner` over `r` uses `Buffer(make([]byte, 0, dpkgLineMax), dpkgLineMax)`.
   4. Count consumed bytes, including newlines. More than `dpkgProbeMaxBytes` → `Unknown`, cause
      `lock information exceeds 1 MiB`. The `+1` byte detects the overflow, and the reader never reads beyond it.
   5. A token over 4 KiB, or a scanner error → `Unknown`, cause `cannot read /proc/locks: <err>`.
   6. After each line, `env.Now().Sub(start) > dpkgProbeTimeout` → `Unknown`, cause `lock check exceeded 1s`.
   7. The file is read to EOF (within the cap), so the outcome never depends on line order.
6. **Parse each line** (`parseProcLocksLine`):
   1. `fields := strings.Fields(line)`. `fields[0]` must be a decimal number followed by `:`.
   2. Any `->` tokens after it mark a blocked **waiter**, including nested and indented waiters. A waiter line is
      skipped, because it does not hold the lock.
   3. The rest needs ≥ 7 fields: `TYPE MODE ACCESS PID DEV START END`. Each of `TYPE`, `MODE` and `ACCESS` is any
      non-empty token, and every type counts as a lock. That includes `POSIX`, `ACCESS`, `OFDLCK`, `FLOCK`, `LEASE`,
      `DELEG` and `UNKNOWN`, with modes such as `ADVISORY`, `MANDATORY`, `*NOINODE*`, `MSNFS`, `ACTIVE`, `BREAKING`,
      `BREAKER` and `UNKNOWN`.
   4. `PID` must parse with `strconv.ParseInt(tok, 10, 64)`. Values ≤ 0 mean "not visible": `-1` is OFD, and other
      negatives are remote or NFS holders.
   5. `DEV` is `MAJ:MIN:INO`, with hex `MAJ`/`MIN` and decimal `INO`. `<none>:0` means no inode, and the line is
      skipped.
   6. Anything else → `Unknown`, cause `unrecognized /proc/locks line <n>`. The line content is never echoed.
   7. **Pid semantics (C-1 d).** The kernel **omits** locks whose holder pid does not translate into the reader's pid
      namespace, rather than printing pid 0. Step 4 guarantees the agent reads the initial namespace's view, where
      every holder is present.
7. **Match.** `lockMatches(l, t)` is a match when `l.Ino == t.Ino` and one of the following holds:
   - **exact:** `l.Major == t.Major && l.Minor == t.Minor`;
   - **inode-only:** `t.Major == 0 && l.Major == 0`. This is the anonymous-device leniency, and it sets
     `InodeOnly = true`.
8. **Holder.** The first matching non-waiter line, in file order, gives `Held`. When `PID > 0`, the paths are built
   **only** from `strconv.FormatInt(pid, 10)` and never from the raw token (C-5):
   - `<ProcRoot>/<pid>/comm` (≤ 8 KiB read): `sanitizeHolderName` trims trailing whitespace, maps every byte outside
     `[A-Za-z0-9._+:-]` to `?` and keeps ≤ 16 bytes.
   - `<ProcRoot>/<pid>/status` (≤ 8 KiB read): `parseStatusUID` takes the first (real) value of the `Uid:` line.

   A read or parse error leaves `Comm = ""` and `UID = -1`. Visibility of the holder is not lock state, so it never
   makes the result `Unknown`. Both reads count toward the 1 s deadline, and exceeding it gives `Unknown`.
9. No match → `Free`.

Causes are `models.TruncateHead(models.SanitizeText(cause), 200)`.

**`holderText()` grammar (FR-018, C-2):**

```
held by <comm> (pid <n>[, uid <u>][, inode-only match])     comm readable, pid > 0
holder not visible (pid <n>[, uid <u>][, inode-only match]) pid > 0, comm unreadable
holder not visible[ (inode-only match)]                     pid <= 0
```

`<u>` is a base-10 integer. A non-root uid on "the dpkg lock" therefore stands out even if the attacker chose the
comm to look like `unattended-upgr`.

**Platform split.**
- **`dpkglock_linux.go`:**
  - `statFileID`: `os.Stat` (follows symlinks), then `fi.Sys().(*syscall.Stat_t)`, then
    `decodeLinuxDev(uint64(st.Dev))` and `uint64(st.Ino)`. A missing `Stat_t` is an error.
  - `platformDpkgProbe(now)`: the real paths, `os.Open` and `"/proc"`.
- **`dpkglock_other.go`** (`//go:build !linux`): `platformDpkgProbe` returns `Absent`. Windows is unchanged
  (FR-006).
- **`decodeLinuxDev`:**
  - `major = ((dev >> 8) & 0xfff) | ((dev >> 32) &^ 0xfff)`
  - `minor = (dev & 0xff) | ((dev >> 12) &^ 0xff)`

  Hawaii's `66306` decodes to `259:2` (`103:02`), which is a fixture.

**Agent seam.** `Agent.dpkgLockProbe func() dpkgLockStatus`. When it is nil, `a.probeDpkg()` calls
`platformDpkgProbe(a.clock().Now)`.

**Rationale.**
- **Why `/proc/locks`.** The agent user cannot open the 0640 `root:root` lock files, so `F_GETLK` is impossible
  without a new sudo rule. `stat()` and `/proc` (0444) work unprivileged, which was verified on Hawaii.
- **FR-007.** The probe never opens the lock files, so it cannot take or contend for the lock. Reading
  `/proc/locks` takes the kernel lock-list lock briefly for each chunk, like `lslocks` does (INFO-1). The large
  buffer keeps the chunks few.
- **Empty answers fail closed (SEC-001).**
  - A pid-namespaced `/proc` silently omits holders, so it is detected and refused.
  - Deleted or hidden lock files on a dpkg host are refused.
  - Only a host where dpkg is absent reads as `Absent`.
- **Why the anonymous-device leniency.** `/proc/locks` prints `inode->i_sb->s_dev`. On btrfs (and overlay or
  tmpfs roots), `stat()` returns a different anonymous `st_dev`, so an exact match would fail open there. On
  block-device filesystems the match stays exact. The security review (INFO-2) agrees with the trade-off: the lock
  flood (R-4) already gives every local user the same veto, so the leniency costs nothing in practice.
  **This path is unverified on real hardware.** Only Hawaii (ext4, 259:2) has been sampled, and the btrfs fixture is
  synthetic (C-6, see §12 note 11).
- **Parsing.** Strict parsing fails closed on an unknown kernel format (FR-010). Every variant the kernel is known to
  print has a fixture, so a known variant can never cause a fleet-wide skip (C-5).

**Alternatives considered.**
- *`F_GETLK` through a sudo helper or a `riot-maintenance` query.* Rejected: it needs an `install.sh` re-run on every
  device (NFR-001).
- *`fuser`/`lsof`, or scanning `/proc/*/fd`.* Rejected: unreadable for root holders, and both spawn a process.
- *Process-name heuristics.* Rejected: `unattended-upgrade-shutdown --wait-for-signal` is always running, and other
  tools would be missed.
- *`systemctl reboot --check-inhibitors=yes`.* Rejected: it needs a new argv in sudoers, it needs systemd 248 or later,
  and apt holds no logind inhibitor.
- *Comparing `/proc/self/ns/pid` with `/proc/1/ns/pid`.* Rejected: reading `/proc/1/ns` needs ptrace access to pid 1,
  which means root. `NSpid` needs no privilege.
- *`/var/lib/dpkg` (the directory) as the dpkg marker.* Rejected in favour of `status` plus `/usr/bin/dpkg`. These are
  the database and the binary, and the binary survives a `/var/lib` mask.
- *Exact match only.* Rejected: it fails open on btrfs. *Inode-only everywhere.* Rejected: it would give false skips
  on common ext4 hosts.

**Consequences.**
- Per probe: up to 4 `stat` calls, one 8 KiB `self/status` read, one capped `/proc/locks` read, and up to two
  8 KiB holder reads.
- Hosts that cannot satisfy the checks get a visible `lock state unknown` skip (R-2, R-4).
- A non-Debian host with the `dpkg` binary installed but no lock files skips with
  `dpkg is installed but its lock files are missing or hidden` (R-9).

---

### AD-002: One shared gate immediately before `execReboot` (FR-003–FR-005, FR-008, FR-009, FR-011–FR-013, FR-021; C-1 c)

**Decision.** New file `internal/agent/reboot_clearance.go`:

```go
type rebootGuards func(ctx context.Context) string // "" = pass; else the failed guard's text

type rebootClearance struct {
    Cleared bool
    Permit  rebootPermit  // valid only when Cleared (AD-007)
    Waited  time.Duration // first "held" observation to return; 0 when never held
    Skip    string        // "" | skipLockTimeout | skipLockUnknown | skipShutdown | skipGuard
    Detail  string        // holder text, cause, or guard text
}
func (c rebootClearance) reason() string

func (a *Agent) awaitRebootClearance(ctx context.Context, path, commandID string, guards rebootGuards) rebootClearance
func (a *Agent) commonRebootGuards(ownID string) rebootGuards
```

Constants: `dpkgLockPoll = 2 * time.Second` and the shared `models.DpkgLockWaitBound` (10 min).

**`commonRebootGuards(ownID)`** returns the first failure, in this order:
1. `a.shuttingDown.Load()` → `agent shutting down`, reported as `skipShutdown`.
2. `!a.config.Commands.AllowReboot` → `commands.allow_reboot is off`.
3. `a.maintenanceBlockerFor(ctx, ownID)` non-empty → `host busy: <blocker>`. `ownID` is `""` on the standalone path
   (strict) and the command ID on the post-patch paths (C-3 of PATCH-FLOW-001).

**Loop (pseudocode):**

```
var firstHeld time.Time; waiting := false; sawHeld := false; guardsPassed := false
for {
    if a.shuttingDown.Load()                  -> return skip(skipShutdown)
    st := a.probeDpkg()
    switch st.State {
    case Unknown:                             -> return skip(skipLockUnknown, st.Cause)            // FR-010
    case Absent:
        if sawHeld                            -> return skip(skipLockUnknown,
                                                   "dpkg lock files disappeared during the wait") // C-1 c
        fallthrough
    case Free:
        if !guardsPassed {
            if g := guards(ctx); g != ""      -> return skip(skipGuard or skipShutdown, g)          // FR-011
            guardsPassed = true
            continue                          // FR-012: the final check is a fresh probe
        }
        -> return cleared(Permit: a.mintRebootPermit(), Waited: waiting ? now-firstHeld : 0)
    case Held:
        sawHeld, guardsPassed = true, false
        now := a.clock().Now()
        if !waiting { waiting, firstHeld = true, now; Info "dpkg lock held, waiting before reboot" }
        elapsed := now.Sub(firstHeld)
        if elapsed >= DpkgLockWaitBound       -> return skip(skipLockTimeout, st.holderText())     // FR-008
        if !a.sleep(ctx, min(dpkgLockPoll, DpkgLockWaitBound-elapsed)) {
            -> return skip(skipShutdown) if a.shuttingDown.Load() else skip(skipGuard, "agent command time budget exhausted")
        }
    }
}
```

When the gate returns after waiting at least once, it writes exactly one `Info "dpkg lock wait ended"` line with the
`path`, `command_id`, `outcome`, `waited` and `holder` attributes.

**Properties.**
- **Free at the first check (FR-005):** probe → guards → probe → cleared, with zero sleeps and zero wait logs.
- **Not a dpkg host, and Windows (FR-006):** the same sequence with `Absent` both times.
- **One wait per attempt, one deadline `firstHeld + 10m` (FR-009, FR-012).** The gate returns once, and no caller
  loops.
- **`Held` then `Absent` never clears (C-1 c).** On a dpkg host the probe already returns `Unknown` in this case
  (AD-001 step 3). The gate rule also covers a host where the markers vanished at the same time.
- **Release to reboot (FR-004):** ≤ 2 s poll + ≤ 5 s guards + 2 × ≤ 1 s probes ≤ 9 s.
- **Shutdown (FR-013):** the gate ends ≤ 3 s after SIGTERM (2 s poll plus 1 s probe). It does not rely on `cmdCtx`.
- **Worst wall time per attempt:** about 10m07s.

**Placement.** The C-8 call sites are unchanged. Every `execReboot` call receives the gate's `Permit` (AD-007).

| Path | Before the gate (kept, fail-fast) | Gate call | Skip | Clearance |
|---|---|---|---|---|
| `handleReboot(cr)` | `if !a.config.Commands.AllowReboot` → existing error | `c := a.awaitRebootClearance(cr.ctx, "reboot", cr.id, a.commonRebootGuards(""))` | AD-003 (b)–(e) | `a.execReboot(c.Permit)` → (a) |
| `maybeRebootAfterPatch` goroutine | the existing checks, reservation and claim; `sleep(rebootDelay)`; then the existing re-check, now reported through `abortPostPatchReboot` | `c := a.awaitRebootClearance(commandContext, "post_patch", id, a.commonRebootGuards(id))` | `abortPostPatchReboot(id, c.reason())` | `a.execReboot(c.Permit)` |
| `rebootAfterDelivery` | the existing delivery wait and checks (all `abortAuthorizedReboot`) | `c := a.awaitRebootClearance(commandContext, "authorized_post_patch", id, a.commonRebootGuards(id))` | `abortAuthorizedReboot(id, c.reason())` | Info log (adds `waited`), `a.execReboot(c.Permit)` |

**Alternatives considered.**
- *The wait inside `execReboot`.* Rejected: the outcomes are path-specific.
- *A SIGTERM-cancelled context.* Rejected: it adds a second agent-wide context for one consumer, and a 2 s poll meets
  FR-013.
- *Adding the post-patch goroutines to `cmdWG`.* Rejected: `awaitResultDelivery` would stretch the agent's stop by up
  to 20 s.
- *Per-check Debug logs.* Rejected by NFR-002.
- *Keep waiting on `Held`→`Absent`.* Rejected in favour of an immediate `Unknown` skip: the disappearance is itself
  anomalous, and continuing to wait would end in the same skip 10 minutes later.

---

### AD-003: Standalone `reboot` result contract (FR-014, FR-015, FR-016)

**Decision.** `handleReboot` becomes `func (a *Agent) handleReboot(cr *cmdRun) commandExecResult`, and
`executeAction`'s `case "reboot":` returns it. `<D>` is `models.FormatDurationShort(c.Waited)`.

| FR-014 | Status | Reason | Message |
|---|---|---|---|
| (a) no wait | `success` | `""` | `reboot initiated` (unchanged, AC-012) |
| (a) after a wait | `success` | `""` | `reboot initiated after waiting <D> for the dpkg lock` |
| (b) timeout | `error` | `""` | `reboot skipped: dpkg lock held past the timeout (10m), <holderText>` |
| (c) unknown | `error` | `""` | `reboot skipped: dpkg lock state unknown: <cause>` |
| (d) shutdown | `error` | `shutdown` | `reboot skipped: agent shutting down` |
| (e) guard | `error` | `conflict` for `host busy: …`, else `""` | `reboot skipped: guard failed: <guard text>` |
| `execReboot` error | `error` | `""` | `reboot: <err>` (unchanged) |
| pre-gate `allow_reboot` false | `error` | `""` | unchanged |

`ReasonTimeout` is never used for (b) (FR-016).

**Truthfulness (FR-015).**
- The gate and `execReboot` run synchronously, and the result is built after either one returns.
- The permit is single-use (AD-007), so nothing can reboot later for that command.
- The seen-ID store blocks redelivery.
- If the `cr.ctx` deadline (12m) ever fired, the outcome would be the truthful skip (e).

**Alternatives considered.** *A `dpkg_lock_wait` progress step.* Deferred: the FRD rules out any UI change. It is the
natural follow-up to INFO-4.

---

### AD-004: Reboot budget split from shutdown (FR-016, FR-020, OQ-003)

**Decision.** New constants in `internal/models/command_policy.go`:

```go
DpkgLockWaitBound  = 10 * time.Minute                     // BR-004
RebootAgentHardMax = DpkgLockWaitBound + 2*time.Minute    // 12m
RebootExecDeadline = 20 * time.Minute                     // > 12m + DeadlineSlack (17m)
```

| Budget | `reboot` before → after | `shutdown` | Consumed by |
|---|---|---|---|
| `ActionClass` | `ClassPower` (unchanged) | `ClassPower` | the class switches |
| `QueuedTTL` | 15m → **15m (unchanged)** | 15m | `ExpiresAt`; the claim-time expiry; sweeper `expireQueued` |
| `ExecDeadline` | 10m → **20m** (`exec_deadline_s` 600 → 1200) | 10m | `service.go:224`; `deadline_at`; sweeper `timeOutOverdue` |
| `AgentHardMax` | 1m → **12m** | 1m | `newCmdRun` `cr.ctx` |
| Invariant | 20m > 17m ✓ | 10m > 6m ✓ | `TestCL_DeadlineInvariant` (unchanged) |

`AgentHardMax` and `ExecDeadline` each gain an `ActionReboot` branch at the top of their `ClassPower` case, and their
doc comments are updated.

- **TTL is unchanged.** It bounds undelivered commands only. Lengthening it would let a stale reboot fire later.
- **Sweeper.** No code change: it reads each row's own `exec_deadline_s`/`deadline_at`. New reboot rows time out at
  about 20–21 min with `no result from agent within 20m`. Rows created before the upgrade keep 600 s.

| Outcome | Agent | Server row |
|---|---|---|
| Started, no wait | (a) in ms | `succeeded` |
| Started after 9m (AC-017) | (a) at about 9m; `cr.ctx` 12m not reached | `succeeded` at about 9m; 20m not reached |
| Timeout | (b) at 10m + ≤ 7 s | `failed` |
| Unknown / guard | (c) / (e) | `failed` |
| Shutdown | (d) within 3 s, durable in the outbox | `failed`, reason `shutdown` |

**Alternatives considered.**
- *Raise all of `ClassPower`.* Rejected by FR-020.
- *A detached reboot.* Rejected: recovery discards reboot markers.
- *Report success, then wait.* Rejected by FR-015.
- *Cap the wait.* Rejected by OQ-003.
- *Let progress extend the deadline.* Forbidden by the CMD-LIFECYCLE security rule.

**Consequences.**
- A lost reboot result is detected after 20 min instead of 10.
- If the agent is hard-killed mid-wait, the host-keyed row blocks host commands until the 20-minute sweep (INFO-4).
  No reboot happens.
- Version skew: R-5.

---

### AD-005: Post-patch skip reporting and reservation release (FR-017, FR-018, FR-019)

- **Authorized path.** Uses the existing `abortAuthorizedReboot(id, reason)`: one Warn, one warning
  `EventCommandCompleted` event `authorized reboot for command <id> was not started: <reason>`, and one telemetry
  trigger.
- **Unauthorized path.** A new `abortPostPatchReboot(id, reason)` mirrors it:
  - one `slog.Warn("os_update: post-patch reboot not started", "command_id", id, "reason", reason)`;
  - one warning `EventCommandCompleted` event `post-patch reboot for command <id> was not started: <reason>`;
  - one `triggerTelemetry()`.

  It is used for every non-reboot exit inside the goroutine: the existing post-delay re-check, which today triggers
  telemetry only, and every gate skip. Exits before the goroutine are unchanged.
- **Reason text** (`rebootClearance.reason()`):
  - `dpkg lock held past the timeout (10m), <holderText>`;
  - `dpkg lock state unknown: <cause>`;
  - `agent shutting down`;
  - the guard text: `commands.allow_reboot is off` | `host busy: <blocker>` | `agent command time budget exhausted`.

  The existing PATCH-FLOW-001 wording is kept. Holder text is already restricted to `[A-Za-z0-9._+:-]` (AD-001), so
  notifier output (Discord, Slack, ntfy, gotify, which do not escape) cannot carry markup.
- **Reservation (FR-019).** Both goroutines `defer a.locks.Unlock({host}, "patch-reboot:<id>")`, and the gate runs
  inside them. A skip therefore returns and releases the reservation (AC-009).
- **Shutdown exception.** The Warn is always written, and the event is best-effort (R-7).
- **Counts.** At most one event and one telemetry trigger per attempt.

---

### AD-006: Bounded growth (NFR-002, NFR-003, house rule)

| Item | Bound per reboot attempt |
|---|---|
| Wait | ≤ 1; one deadline `firstHeld + 10m` |
| Lock checks | ≤ 10m / 2s + 3 = **303** probes |
| Reads per probe | ≤ 4 `stat`; `self/status` ≤ 8 KiB; `/proc/locks` ≤ 1 MiB + 1 byte (cap at the reader); `<pid>/comm` and `<pid>/status` ≤ 8 KiB each; ≤ 1 s total |
| Memory | O(1): a 64 KiB read buffer, a 4 KiB line buffer, and one status |
| Guard evaluations | only on a free observation: ≤ 1 per poll (≤ 301, normally 1); ≤ 1 `systemctl show` each, capped at 5 s |
| Info-or-above log lines from the gate | ≤ 2, and only when a wait happened; none per check at any level |
| Path outcome lines | unchanged (one per outcome) |
| Events | standalone: the existing failed-command event; post-patch: ≤ 1 |
| Telemetry triggers | post-patch: ≤ 1 (coalesced) |
| Permits | ≤ 1 minted per attempt and consumed at most once; an in-memory `atomic.Uint64` |
| Persistent state | **none** |

---

### AD-007: `execReboot` accepts only a gate-minted, single-use permit (C-3, SEC-003)

**Decision.**

```go
// rebootPermit is proof that awaitRebootClearance returned Cleared for one attempt.
// The type is unexported, so code outside package agent cannot build one. Inside the package it is minted only by
// mintRebootPermit, which is called only by awaitRebootClearance; an AST test enforces both.
type rebootPermit struct{ id uint64 }

var errRebootNotCleared = errors.New("reboot refused: no dpkg-lock clearance for this attempt")

func (a *Agent) mintRebootPermit() rebootPermit // id = a.rebootPermitSeq.Add(1); a.rebootPermitLive.Store(id)
func (a *Agent) execReboot(p rebootPermit) error
```

New `Agent` fields: `rebootPermitSeq atomic.Uint64` and `rebootPermitLive atomic.Uint64`.

**`execReboot` runtime check.** Before doing anything else,
`if p.id == 0 || !a.rebootPermitLive.CompareAndSwap(p.id, 0) { return errRebootNotCleared }`. This means:
- a zero permit (any skipped clearance) is refused;
- a permit can be used once only;
- a permit older than the most recently minted one is refused.

Only one gate can run at a time, because every path holds the host key. On refusal it does not call the runner or
`exec`. The rest of the body (runner, Windows and Linux branches) is unchanged.

**Call-site test** (`reboot_callsites_test.go`). The helper is rewritten to record **every** `*ast.SelectorExpr`
whose `Sel.Name == "execReboot"` in non-test files, one entry per reference (`file:line`). Each reference must pass
all of these checks:
1. It is the `Fun` of a `*ast.CallExpr`. A method value (`f := a.execReboot`) or any other non-call reference fails.
2. Its enclosing function is one of the three C-8 callers.
3. The body text before it contains `Commands.AllowReboot`, as in C-8.
4. Its single argument is `<ident>.Permit`, where `<ident>` is assigned from `a.awaitRebootClearance(` earlier in the
   same function and is checked with `if !<ident>.Cleared` before the call.

The C-8 assertions (exactly three callers, each `AllowReboot`-guarded) are kept. Only their data source changes from
one boolean per function to one entry per reference.

**Rationale.**
- The compiler stops any caller outside the package from building a permit, and the AST test stops struct literals
  inside it.
- The runtime check makes a discarded or stale clearance harmless even if both of the above were bypassed.
- Source-text checks are kept only as a regression signal, not as the control.

**Alternatives considered.**
- *`execReboot(c rebootClearance)` refusing when `!Cleared`.* Rejected: any code can build
  `rebootClearance{Cleared: true}`.
- *Exported or interface-based capabilities.* Over-engineered for one package-internal call.

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `internal/agent/dpkglock.go` | `fileID`, `procLock`, `dpkgLockState`, `dpkgLockStatus` + `holderText`, `dpkgProbeEnv`, `probeDpkgLock`, `parseProcLocksLine`, `lockMatches`, `parseNSpid`, `parseStatusUID`, `sanitizeHolderName`, `decodeLinuxDev`, constants, `(a *Agent) probeDpkg()` (AD-001) |
| CREATE | `internal/agent/dpkglock_linux.go` | `statFileID` (`syscall.Stat_t`), `platformDpkgProbe` with the real paths |
| CREATE | `internal/agent/dpkglock_other.go` | `//go:build !linux`; `platformDpkgProbe` returns `Absent` |
| CREATE | `internal/agent/dpkglock_test.go` | Parser, kernel-variant fixtures, matcher, `NSpid`, dpkg markers, caps, holder and uid tests on every platform (fake `StatID`/`Open`/`Now`); unit-file `PrivatePIDs=` regression test (uses `installerUnit`) |
| CREATE | `internal/agent/dpkglock_linux_test.go` | Real `/proc/locks` + `syscall.FcntlFlock` on a temp file; real `/proc/self/status` `NSpid`; `statFileID` not-exist |
| CREATE | `internal/agent/dpkglock_other_test.go` | `//go:build !linux`; the platform probe is `Absent` (runs on Windows) |
| CREATE | `internal/agent/reboot_clearance.go` | `rebootGuards`, `rebootClearance` + `reason()`, `awaitRebootClearance` (including the `sawHeld` rule), `commonRebootGuards`, `abortPostPatchReboot`, `rebootPermit`, `mintRebootPermit`, `errRebootNotCleared`, `dpkgLockPoll` (AD-002, AD-005, AD-007) |
| CREATE | `internal/agent/reboot_clearance_test.go` | Gate, three-path and permit tests (§8) |
| MODIFY | `internal/agent/commands.go` | `handleReboot(cr *cmdRun) commandExecResult` with the gate and AD-003 results; `executeAction` `case "reboot"`; `maybeRebootAfterPatch` goroutine re-check through `abortPostPatchReboot`, then gate, then `execReboot(c.Permit)`; **`execReboot(p rebootPermit) error`** with the runtime check and an updated doc comment |
| MODIFY | `internal/agent/reboot_auth.go` | `rebootAfterDelivery`: gate after the existing checks; skip → `abortAuthorizedReboot(id, c.reason())`; `execReboot(c.Permit)`; the Info log gains `waited` |
| MODIFY | `internal/agent/agent.go` | Seam `dpkgLockProbe func() dpkgLockStatus`; fields `rebootPermitSeq`, `rebootPermitLive atomic.Uint64` |
| MODIFY | `internal/agent/reboot_callsites_test.go` | Helper rewritten to one entry per `execReboot` reference (AD-007); C-8 assertions kept; new RD/C-3 tests (§8), including a synthetic-source self-test showing that a method value is flagged |
| MODIFY | `internal/agent/command_lifecycle_test.go` | `newLifecycleHarnessAt` sets `h.a.dpkgLockProbe` to a fixed `Free` status (covers `reboot_auth_test.go` and `maintenance_test.go`) |
| MODIFY | `internal/agent/commands_test.go` | `newOSUpdateTestAgent` sets the same `Free` probe |
| MODIFY | `internal/models/command_policy.go` | `DpkgLockWaitBound`, `RebootAgentHardMax`, `RebootExecDeadline`; the `reboot` branches in `AgentHardMax`/`ExecDeadline`; doc comments (AD-004) |
| MODIFY | `internal/models/command_policy_test.go` | `TestCL_AC029_ExecDeadlineTable` reboot row → 20m; new RD budget tests |
| MODIFY | `internal/server/commands/service_test.go` | `TestCL_AC029_ExpiresAtPerActionClass` reboot `dl` 600 → 1200 (TTL stays 15m) |
| MODIFY | `internal/server/commands/sweeper_test.go` | `TestSweep_RD_AC017_RebootNotTimedOutBefore20m` |

**Explicitly unchanged:**
- `internal/server/commands/sweeper.go`, `service.go` and `internal/server/db/*`
- all migrations
- `scripts/install.sh`, `scripts/riot-agent.service`, the sudoers rules (they are read by a test, never edited)
- `internal/agent/doctor.go`
- `handleShutdown`
- `internal/maintenance/*`
- `web/`
- `go.mod` / `go.sum`

**Technical writer (post-QA):**
- **README**:
  - the reboot dpkg wait and its five outcomes (AD-003), and the post-patch skip event;
  - split the Power row of the TTL/deadline table into `reboot` (15 min / **20 min**) and `shutdown`
    (15 min / 10 min);
  - requirements: `/proc/locks` must be readable and the agent must run in the host pid namespace (no `PrivatePIDs=`,
    no container); a dpkg host with missing lock files skips;
  - **local-user veto (C-2):** any local user can block rIOt reboots by holding many locks or, on btrfs and other
    major-0 filesystems, by an inode collision. Every block is reported. Check the reported uid: a non-root holder
    of "the dpkg lock" is suspicious;
  - **upgrade order (C-4):** upgrade the server before the release reaches agents; the automatic agent updater
    installs any release the server advertises;
  - the residual window between the final check and shutdown is sub-second to a few seconds (INFO-3).
- **`web/src/pages/LearnMore.tsx`** manual patch and reboot guide: reboots wait up to 10 min for a running package
  manager and are skipped, not forced.
- **CHANGELOG** `[Unreleased]`:
  - Added: the dpkg wait;
  - Changed: the reboot execution deadline is now 20 min;
  - **Upgrade note:** server first.
- **Stale content to log, not edit:**
  - CMD-LIFECYCLE-001 ADD §AD-005 table (power 10m/1m);
  - FRD NFR-004 "milliseconds" (INFO-3: sub-second to a few seconds).

## 5. Data Model Changes

**Postgres: none.** No migration, table, column or index. New `reboot` rows store `exec_deadline_s = 1200`, which is a
value computed in Go.

**Models (code only):** three constants and two `reboot` branches (AD-004). No wire struct changes.

**Agent on-disk state: none new.** The probe reads lock-file metadata, the dpkg markers, `/proc/self/status`,
`/proc/locks` and `/proc/<pid>/{comm,status}`. It writes nothing.

**In-memory state** (never persisted):
- per attempt: `waiting`, `firstHeld`, `sawHeld`, `guardsPassed`;
- per agent: `rebootPermitSeq`, `rebootPermitLive`.

**Bounded growth:** AD-006.

## 6. API / Interface Contract

No new or changed endpoints.

### Command record (existing)

| Field | Change |
|---|---|
| `exec_deadline_s` | new `reboot` rows `1200` (was `600`); `shutdown` `600` |
| `deadline_at` | `dispatched_at + 1200 s` for new `reboot` rows |
| `expires_at` | unchanged |
| `result_msg`, `status`, `outcome`, `reason` | the AD-003 table |

Timed-out text: `no result from agent within 20m`.

### Agent command result for `reboot` (existing shape)

```
reboot initiated
reboot initiated after waiting <D> for the dpkg lock
reboot skipped: dpkg lock held past the timeout (10m), <holderText>
reboot skipped: dpkg lock state unknown: <cause>
reboot skipped: agent shutting down                       (reason "shutdown")
reboot skipped: guard failed: <guard text>                (reason "conflict" for "host busy: …")
```

`<holderText>` follows the AD-001 grammar, for example
`held by unattended-upgr (pid 1234, uid 0)` or `holder not visible (pid 812, uid 1000, inode-only match)`.

`<cause>` is one of:
- `cannot stat <path>: <err>`
- `dpkg is installed but its lock files are missing or hidden`
- `agent is not in the host pid namespace`
- `cannot verify the pid namespace: <err|no NSpid>`
- `cannot read /proc/locks: <err>`
- `lock information exceeds 1 MiB`
- `lock check exceeded 1s`
- `unrecognized /proc/locks line <n>`
- `dpkg lock files disappeared during the wait`

All causes are ≤ 200 bytes.

### Agent event (existing shape; `command_completed`, `warning`)

```
authorized reboot for command <uuid> was not started: <reason>
post-patch reboot for command <uuid> was not started: <reason>
```

## 7. Sequence / Flow

### 7.1 Standalone reboot, lock held 9 minutes (AC-017)

1. The server creates the row with `exec_deadline_s = 1200`, dispatches it at T0, and `deadline_at = T0+20m`.
2. Agent intake. `cr.ctx` has a 12m timeout.
3. `executeAction`: precheck and host lock. `handleReboot(cr)`: `AllowReboot` passes, so it calls
   `awaitRebootClearance`.
4. The probe stats the lock files (present), checks `NSpid` (one level) and reads `/proc/locks`. It returns `Held`,
   `held by unattended-upgr (pid 1234, uid 0)`. `firstHeld = T0+1s`, and one Info `wait started` line is written.
5. Every 2 s: `shuttingDown`? → probe → `Held` → sleep.
6. At T0+9m01s the probe returns `Free`, the guards pass, and the final probe returns `Free`. The gate mints a permit
   and returns Cleared with Waited 9m. One Info `wait ended` line is written.
7. `execReboot(c.Permit)`: the compare-and-swap consumes the permit, and `sudo systemctl reboot` starts. The result
   `success, "reboot initiated after waiting 9m for the dpkg lock"` goes to the outbox.
8. The row becomes `succeeded` at about T0+9m.

### 7.2 Standalone reboot, timeout

Steps 1–5 as in 7.1. At `firstHeld + 10m` the probe still returns `Held`, so the skip is `skipLockTimeout` and the
result is
`error, "reboot skipped: dpkg lock held past the timeout (10m), held by unattended-upgr (pid 1234, uid 0)"`.
No permit is minted.

### 7.3 Unauthorized post-patch, lock re-taken

1. The `os_update` result is queued, then `maybeRebootAfterPatch` passes its checks, takes the reservation, claims,
   and starts the goroutine.
2. `sleep(2s)`, then the re-check passes. Gate:
   1. `Held` at minute 0, so the wait starts.
   2. `Free` at minute 4, and the guards pass.
   3. The final probe returns `Held` (re-taken): `guardsPassed` is reset and the same deadline continues.
   4. The lock is still `Held` at minute 10 → timeout.
3. `abortPostPatchReboot` writes one Warn, sends one event and triggers telemetry once. The `defer` releases the
   reservation.

### 7.4 Lock files deleted during a wait (SEC-001 scenario)

`Held` at minute 0, then an admin deletes `/var/lib/dpkg/lock*`. The next probe finds no targets, sees that
`/var/lib/dpkg/status` exists, and returns `Unknown` (`dpkg is installed but its lock files are missing or hidden`).
The skip is immediate and no reboot starts. If the markers had also vanished, the probe would return `Absent`, and the
gate's `sawHeld` rule would still skip as `Unknown`.

### 7.5 Authorized phase B

Reservation → `awaitResultDelivery` → the existing checks → gate. On a skip, `abortAuthorizedReboot`. On clearance,
the Info line and `execReboot(c.Permit)`.

### 7.6 Agent stop during a wait

`shuttingDown` is seen within ≤ 3 s, and the gate returns `skipShutdown`.
- **Standalone:** result (d).
- **Post-patch:** abort, with a best-effort event.

## 8. Acceptance Criteria Mapping

**Naming.**
- Go test names contain `RD_ACnnn`, or `RD_Cn` for security conditions without an AC.
- Assertion messages start with `[AC-nnn]` or `[C-n]`.
- Tests are co-located.
- "Three paths" tests are table-driven over `standalone`, `unauthorized` and `authorized`. They use the
  `lifecycleHarness` and `fakeClock`, with `a.pause = func(ctx, d) bool { clk.Advance(d); return ctx.Err() == nil }`
  and a scripted `dpkgLockProbe`. They use no real time and no real lock (NFR-006).
- Probe tests use a fake `dpkgProbeEnv` whose `Open` serves in-memory files and records the paths it was asked for.

| AC | Fulfilled By | Tests |
|---|---|---|
| AC-001 | AD-002 free branch; AD-007 | `TestRebootClearance_RD_AC001_FreeLockRebootsWithNoSleepOnAllThreePaths` (runner called once; zero gate `pause` calls; standalone message exactly `reboot initiated`); `TestRebootClearance_RD_AC001_FreeRunsGuardsThenFinalProbe` (probe, guards, probe, then `execReboot` with nothing in between) |
| AC-002 | AD-001; AD-002 held branch | `TestRebootClearance_RD_AC002_HeldLockNeverRebootsWithinBoundOnAllThreePaths`; `TestProcLocks_RD_AC002_PosixWriteLineMatchesLockFrontend`; `…_HexMajorMinorDecimalInode` (`00:16d:3947933`, `103:02:262338`); `…_BlockDeviceRequiresExactDevice`; `…_AnonymousDeviceMatchesOnInodeAndMarksInodeOnly` (synthetic; see C-6); `TestProcLocks_RD_AC002_KernelVariantFixtures` (C-5 table, each parsing without `Unknown`: `ACCESS` type; `*NOINODE*` with `<none>:0` skipped; `FLOCK  MSNFS`; `LEASE  BREAKING`; `LEASE  BREAKER` with `<none>:0`; `UNKNOWN UNKNOWN`; a waiter `1: -> POSIX …`; a nested, indented waiter; `OFDLCK` pid `-1`; a negative remote pid); `TestDecodeLinuxDev_RD_AC002_HawaiiDevIs259_2`; `TestDpkgLock_RD_AC002_RealProcLocksSeesFcntlLockOnTempFile` (Linux: `FcntlFlock F_SETLK` on a temp file, `LockPaths=[temp]`, real `/proc` → `Held`, `PID == os.Getpid()`, uid equal to `os.Getuid()`) |
| AC-003 | AD-001 targets | `TestProcLocks_RD_AC003_LegacyLockAloneIsHeld`; `TestDpkgProbe_RD_AC003_FrontendAbsentLegacyPresentStillChecked`; `TestRebootClearance_RD_AC003_LegacyHeldWaitsLikeFrontend` |
| AC-004 | AD-002 | `TestRebootClearance_RD_AC004_ReleaseAfter3mRebootsWithin10s` (each path); `…_PollIntervalAndProbeTimeoutFitBound` |
| AC-005 | AD-002 timeout; FR-009 | `TestRebootClearance_RD_AC005_TimeoutSkipsAndNeverForces`; `…_NoSecondWaitAfterSkip`; `…_ProbeAtExactBoundHeldSkips`; `…_ProbeAtExactBoundFreeProceeds` |
| AC-006 | AD-003 (b); `holderText` | `TestHandleReboot_RD_AC006_TimeoutResultFailedAndNamesHolder` (contains `dpkg lock held past the timeout`, `unattended-upgr`, `pid 1234`, `uid 0`; no `reboot initiated`); `TestDpkgProbe_RD_AC006_HolderNamedFromCommWithUid`; `TestDpkgProbe_RD_AC006_CommPathFromParsedPidOnly` (the fake `Open` sees exactly `/proc/1234/comm` and `/proc/1234/status`; no comm/status read for pid ≤ 0) |
| AC-007 | `holderText` | `TestDpkgProbe_RD_AC007_PidMinusOneHolderNotVisible`; `…_NegativeRemotePidHolderNotVisible`; `…_CommUnreadableSaysHolderNotVisibleWithPid`; `…_StatusUnreadableOmitsUid`; `TestHandleReboot_RD_AC007_TimeoutReportSaysHolderNotVisible` |
| AC-008 | AD-005 | `TestPostPatchReboot_RD_AC008_AuthorizedTimeoutOneEventOneTelemetry`; `…_UnauthorizedTimeoutOneEventOneTelemetry` (exactly one event with the command ID, `dpkg lock held past the timeout` and the holder; a telemetry signal; zero reboots) |
| AC-009 | goroutine `defer Unlock` | `TestPostPatchReboot_RD_AC009_SkipReleasesHostReservation` (both paths) |
| AC-010 | AD-001 `Unknown` cases; AD-002 | `TestDpkgProbe_RD_AC010_StatErrorOtherThanNotExistIsUnknown`; `…_ProcLocksMissingIsUnknown` (`ProcSubset=pid`); `…_ProcLocksPermissionDeniedIsUnknown`; `TestProcLocks_RD_AC010_MalformedLineIsUnknown` (table); **C-1:** `TestDpkgProbe_RD_AC010_NestedPidNamespaceIsUnknown` (`NSpid:\t4242\t1`, with a `/proc/locks` that has no match → `Unknown`, not `Free`); `…_NSpidMissingIsUnknown`; `…_SelfStatusUnreadableIsUnknown`; `…_DpkgStatusPresentLockFilesMissingIsUnknown`; `…_DpkgBinaryPresentVarLibHiddenIsUnknown`; `…_MarkerStatErrorIsUnknown`; `TestRebootClearance_RD_AC010_HeldThenAbsentNeverRebootsOnAllThreePaths` (probe: `Held`, `Held`, `Absent` → skip `dpkg lock files disappeared during the wait`, zero reboots); `TestRebootClearance_RD_AC010_UnknownSkipsImmediatelyWithoutSleepOnAllThreePaths`; `…_UnknownMidWaitSkipsAtOnce` |
| AC-011 | AD-001 caps | `TestDpkgProbe_RD_AC011_OverOneMiBIsUnknown`; `…_ExactlyOneMiBParses`; `…_ReaderStopsAtCap` (a counting fake reader serving 5 MiB has served ≤ 1 MiB + 1 byte plus one buffer fill when the probe returns); `…_LineOver4KiBIsUnknown`; `…_SlowReadOverOneSecondIsUnknown`; `…_SlowHolderReadIsUnknown`; `TestHandleReboot_RD_AC011_OversizedLockInfoSkipsReboot` |
| AC-012 | AD-001 `Absent`; `dpkglock_other.go` | `TestDpkgProbe_RD_AC012_NoDpkgNoLockFilesIsAbsentWithoutReadingProc` (all markers not-exist; the fake `Open` is never called, so neither `self/status` nor `locks` is read); `TestRebootClearance_RD_AC012_AbsentRebootsAsToday` (exactly `reboot initiated`, zero sleeps, zero wait logs); `TestPlatformProbe_RD_AC012_NonLinuxIsAbsent` (Windows); `TestStatFileID_RD_AC012_MissingPathIsNotExist` (Linux) |
| AC-013 | AD-002 guards | `TestRebootClearance_RD_AC013_AllowRebootOffDuringWaitSkipsNamingGuard`; `…_MaintenanceStartsDuringWaitSkipsNamingGuard` (all three paths) |
| AC-014 | AD-002 | `TestRebootClearance_RD_AC014_FinalCheckRetakenKeepsWaitingWithinBound` (skip at exactly `firstHeld + 10m`; one `wait started` line; guards re-run) |
| AC-015 | AD-002 | `TestRebootClearance_RD_AC015_ShutdownEndsWaitWithin5s`; `TestHandleReboot_RD_AC015_ShutdownResultReasonShutdown`; `TestPostPatchReboot_RD_AC015_ShutdownSkipLogsWarn` |
| AC-016 | AD-003; AD-007 single use | `TestHandleReboot_RD_AC016_NoRebootAfterSkippedResult` (the probe later flips to `Free`; the runner count stays 0; redelivery is deduped) |
| AC-017 | AD-003 (a); AD-004 | `TestHandleReboot_RD_AC017_NineMinuteWaitReportsStartedAndWaited`; `TestCommandPolicy_RD_AC017_RebootBudgetCoversWait`; `TestSweep_RD_AC017_RebootNotTimedOutBefore20m`; the updated `TestCL_AC029_ExecDeadlineTable` and server `TestCL_AC029_ExpiresAtPerActionClass` |
| AC-018 | AD-001 read-only | `TestDpkgProbe_RD_AC018_NeverOpensLockFiles` (`Open` sees only `/proc/self/status`, `/proc/locks` and `/proc/<pid>/{comm,status}`); `TestDpkgLock_RD_AC018_NoLockingCallsInSource` (AST over `dpkglock*.go` and `reboot_clearance.go`: no `FcntlFlock`, `Flock`, `F_SETLK`, `F_GETLK`, `os.OpenFile`, `syscall.Open`); `TestDpkgLock_RD_AC018_ProbeDoesNotDisturbHeldLock` (Linux) |
| AC-019 | AD-006 | `TestRebootClearance_RD_AC019_FullWaitBoundedOutput` (≤ 2 Info+ and 0 Debug records from the gate; ≤ 1 event; ≤ 1 telemetry signal; ≤ 303 probes; ≤ 1 permit minted; no new file in the state directory) |
| AC-020 | AD-002; AD-004; AD-007 | `TestRebootAuth_PF_C8_ExecRebootHasExactlyThreeGuardedCallSites` (assertions kept; the helper now yields one entry per reference); `TestRebootCallSites_RD_AC020_GateClearanceFeedsEveryExecReboot` (AD-007 checks 1–4 for each reference); `TestRebootCallSites_RD_AC020_ShutdownHasNoGate` (AST only; **never call `handleShutdown`**); `TestCommandPolicy_RD_AC020_ShutdownBudgetUnchanged` |
| AC-021 | AD-001 unprivileged | `TestDpkgProbe_RD_AC021_NoExecNoSudo` (the runner records no calls from the probe; the only gate calls are the guard's `systemctl show`); the Linux real-`/proc` test doubles as the unprivileged proof (skipped when euid is 0); DoD diff check on `scripts/` |
| C-1 (unit) | AD-001 step 4 | `TestAgentUnit_RD_C1_NoPrivatePIDs` (`scripts/riot-agent.service` and `installerUnit(t)` contain no `PrivatePIDs=`. The failure message says the `NSpid` check would make every reboot skip, so adding it needs an ADD change); `TestDpkgLock_RD_C1_RealSelfStatusIsInitialNamespace` (Linux CI; skipped with a logged reason when CI itself runs in a nested namespace) |
| C-2 | AD-001 holder | `TestDpkgProbe_RD_C2_HolderTextCarriesUidAndInodeOnly` (exact strings for the three grammar forms); `TestDpkgProbe_RD_C2_NonRootHolderUidReported` (`Uid:\t1000\t1000\t1000\t1000` → `uid 1000`) |
| C-3 | AD-007 | `TestExecReboot_RD_C3_NonClearedPermitStartsNoReboot` (table: zero permit; the permit field of a skipped clearance; a permit used twice; an older permit after a newer one was minted → each returns `errRebootNotCleared`, runner count 0); `TestExecReboot_RD_C3_ClearedPermitStartsExactlyOnce`; `TestRebootCallSites_RD_C3_MethodValueReferenceIsFlagged` (helper self-test on a synthetic source containing `f := a.execReboot`); `TestRebootCallSites_RD_C3_PermitMintedOnlyByGate` (AST: `rebootPermit{` composite literals appear only in `mintRebootPermit`, and `mintRebootPermit` is called only in `awaitRebootClearance`) |
| C-5 | AD-001 | `TestSanitizeHolderName_RD_C5_CharsetAndLength` (`[ok](http://a.b)` → `?ok??http:??a.b?`; control bytes, spaces, `/`, `*`, `_` → `?`; at most 16 bytes); plus the C-5 rows under AC-002, AC-006 and AC-011 |

## 9. Error Handling

| Failure | Behavior | Log |
|---|---|---|
| Not a dpkg host (no lock files, no markers) | `Absent`; reboot as today | none |
| dpkg present, lock files missing or hidden | `Unknown` → immediate skip | path outcome line |
| Not in the initial pid namespace, or `NSpid` unverifiable | `Unknown` → immediate skip | path outcome line |
| `stat` error other than not-exist | `Unknown` → immediate skip | path outcome line |
| `/proc/locks` missing, unreadable, over 1 MiB, line over 4 KiB, malformed, or over 1 s | `Unknown` → immediate skip | path outcome line |
| `Held` then `Absent` in one attempt | `Unknown` → immediate skip | `wait ended` Info + outcome line |
| `comm` / `status` unreadable | `Held` stays; text omits the name or the uid | none |
| Held past 10m | skip (b) / timeout event | `wait ended` Info + outcome line |
| Guard fails after the wait | skip (e) / guard event | `wait ended` Info (if waited) + outcome line |
| Agent stopping | skip (d) / best-effort event | outcome line |
| `cr.ctx` deadline (not reachable by design) | skip (e) `agent command time budget exhausted` | outcome line |
| `execReboot` without a valid permit | `errRebootNotCleared`; nothing executed; treated as an `execReboot` error on that path | that path's existing error handling |
| `execReboot` exec error | unchanged per path | unchanged |

## 10. Security Considerations

- **No privilege added (NFR-001).** The probe uses only `stat` and world-readable `/proc` reads. It runs no exec and no
  sudo, and the unit, sudoers and `install.sh` are unchanged.
- **FR-007.** The lock files are never opened (structural test). The `/proc/locks` read costs the same as `lslocks`
  (INFO-1).
- **Untrusted text (C-5).**
  - Holder names are limited to `[A-Za-z0-9._+:-]`, ≤ 16 bytes, so they are safe for notifiers that do not escape.
  - The uid is an integer.
  - Causes are sanitized and ≤ 200 bytes.
  - `/proc/locks` content is never echoed.
  - The `/proc/<pid>` paths come only from a parsed integer.
- **Fail closed (BR-001, C-1).** Every error and every "empty answer" the review found maps to `Unknown`. The gate can
  only delay or prevent a reboot (BR-003).
- **Gate enforcement (C-3).** A compile-time permit type, a runtime single-use check and a per-reference AST test.
  C-8's three callers and `AllowReboot` requirement are kept.
- **No reboot loop (BR-002).** One gate per attempt, no retry, and the intent store is unchanged.
- **Residual risks:**
  - **R-1 (NFR-004; INFO-3 wording):** the window from the final probe until systemd stops sessions and services is
    **sub-second to a few seconds** (sudo/PAM, job queueing, session teardown), not milliseconds.
    - An interactive `apt` started in that window can be killed.
    - New `unattended-upgrades` runs cannot start: their timers stop and they conflict with the shutdown job.
    - A run already in progress holds its own logind delay inhibitor, which a root `systemctl reboot` honours.
    - Closing the window needs a new sudo argv. Accepted.
  - **R-2 (pid namespaces; corrected per C-1):**
    - The kernel omits locks whose holder is invisible in the reader's pid namespace; it does not print pid 0. The
      probe therefore refuses to answer (`Unknown`) unless `/proc/self/status` `NSpid` shows exactly one level, read
      from the same `/proc` mount as `/proc/locks`.
    - A containerised agent, or a future `PrivatePIDs=yes` unit, makes every reboot on a dpkg host skip, which is
      visible.
    - `TestAgentUnit_RD_C1_NoPrivatePIDs` guards the shipped units. A local drop-in cannot be tested, and the runtime
      check covers it.
    - pid ≤ 0 lines (OFD `-1`, remote/NFS negatives) are "holder not visible".
  - **R-3 (local-user veto by inode collision; rewritten per C-2):** on btrfs, ZFS and other major-0 filesystems,
    the inode-only match lets an unprivileged user hold the gate deterministically:
    - by creating a btrfs subvolume in their home and allocating objectids up to the target inode;
    - where user namespaces are enabled, by mounting a private tmpfs and doing the same.

    The lock then holds the gate and the host key for the full 10 minutes per attempt, and the reboot skips. The
    report names the holder. `comm` is attacker-chosen, but the uid is not, and the match is marked `inode-only
    match`. Containers cloned from a host snapshot (nspawn, LXC) can cause the same delay without malice. Exact
    matching cannot be forged, because unprivileged users cannot open or hard-link the 0640 root lock inode. The
    leniency gives up that property in exchange for btrfs/overlay coverage; R-4 already gives the same veto
    everywhere. **Unverified on real hardware (C-6).**
  - **R-4 (local-user veto by lock volume, any filesystem; rewritten per C-2):** any local user can hold about
    20,000 byte-range POSIX locks on a file they own. Linux does not enforce `RLIMIT_LOCKS`. That pushes
    `/proc/locks` past 1 MiB, so every probe returns `Unknown` (`lock information exceeds 1 MiB`), with no holder
    named, and every rIOt reboot on that host skips with a visible report. Hardened kernels, an LSM, or a future
    `ProcSubset=pid` have the same visible effect. **Ryan accepts this as the price of failing closed. The README
    documents it.**
  - **R-5 (version skew; corrected per C-4):**
    - The automatic agent updater installs any release the server advertises, and an old server polls GitHub for new
      releases. So new agents can run against an old server (600 s reboot deadline) without anyone choosing that
      order.
    - If a standalone wait ends in the last moments before the old deadline, the row is marked `timed_out`, the late
      result is rejected, and the host still reboots. An operator who reads `timed_out` may re-issue the command,
      which causes at most one extra, operator-driven reboot.
    - The CHANGELOG and README tell Ryan to upgrade the server before the release reaches agents.
    - An old agent with a new server only gets a longer lost-result timeout.
  - **R-6:** the unauthorized path's result text says `…; reboot initiated)` before phase B, and a skip is recorded
    by the event (OQ-004 pattern; unchanged).
  - **R-7:** a post-patch shutdown skip is Warn-logged, and its event is best-effort. Both can be lost if the process
    exits first. No reboot can start.
  - **R-8 (INFO-4):**
    - A dispatched, waiting reboot cannot be cancelled from the dashboard, and it can fire up to 10 minutes late.
    - For about 10.5 minutes other host-keyed work gets `conflict`, and Docker auto-updates in that window count
      toward `AutoUpdateBackoff`.
    - Bounded and visible.
  - **R-9:** a non-Debian host with the `dpkg` binary installed but no lock files skips with a clear cause. Rare;
    fails safe.

## 11. Performance Considerations

- **Common case (free or absent):** ≤ 4 `stat` calls, 0–2 small `self/status` reads, and 0–2 `/proc/locks` reads,
  each usually under 4 KiB, one `read()` with the 64 KiB buffer, and under 1 ms. One guard evaluation.
- **During a wait:** ≤ 303 probes over 10 min, in O(1) memory. The kernel lock-list chunks are minimised by the large
  buffer (INFO-1).
- **Server:** no new queries, no index, no caching. The sweeper is unchanged.
- **No caching, rate limiting or indexing is required.**

## 12. Implementation Notes for Engineers

1. **Commits:** `[REBOOT-DPKG-001] <type>: <what>`. Order:
   1. models
   2. `dpkglock*`
   3. `reboot_clearance` (gate + permit)
   4. wire the three paths, `execReboot(p)` and the harnesses
   5. the call-site test rewrite
   6. server tests

   One logical change per commit.
2. **C-8.** Keep each caller's literal `a.config.Commands.AllowReboot` check before the gate, and add no fourth
   caller. Rewrite only the helper of `reboot_callsites_test.go`. Its assertions stay.
3. **Call shape AD-007 requires** (it is checked by AST):

   ```go
   c := a.awaitRebootClearance(...)
   if !c.Cleared { /* report */ return ... }
   if err := a.execReboot(c.Permit); err != nil { ... }
   ```

   Nothing goes between the `Cleared` check and `execReboot` except the authorized path's existing Info line. Never
   build a `rebootPermit` outside `mintRebootPermit`.
4. **Clocks and sleeps:** only `a.clock().Now()` and `a.sleep(ctx, d)` in the gate, and only `env.Now` in the probe.
5. **Test determinism on Linux CI.** Every test that reaches a reboot path sets `dpkgLockProbe`, which the harness
   defaults cover. Only `dpkglock_linux_test.go` touches the real `/proc`, and only for its own temp file and its own
   `self/status`.
6. **Existing pause-based tests:**
   - `TestMaintenance_AC009_*` must still see exactly one `pause` call: the free path never sleeps.
   - `TestRebootAuth_PF_AC020_*` still passes through the pre-gate check.
7. **`handleReboot`** returns `commandExecResult`. Never use `ReasonTimeout` for a dpkg skip.
8. **Reader cap.** Use `io.LimitReader(f, 1<<20+1)` under a 64 KiB `bufio.Reader`, with a scanner whose maximum
   token is 4 KiB. A check made after reading does not count as a cap.
9. **No new dependency.** Use `syscall` (Linux file only) and the pure `decodeLinuxDev`. Run `make build-agent-all`
   for all 8 targets.
10. **Never call `handleShutdown` in tests.** It runs a real shutdown.
11. **Host verification (C-6), recorded in `docs/implementation/REBOOT-DPKG-001-impl-report.md`:**
    1. **Hawaii (ext4, Debian 13), as `riot`:**
       1. at rest the probe returns `Free`;
       2. `/proc/self/status` `NSpid` has one field;
       3. while `/var/lib/dpkg/lock-frontend` is held for 30 s (a root `python3` call to `fcntl.lockf(..., LOCK_EX)`,
          or a real `apt-get update`), the probe returns `Held` with comm, pid and `uid 0`;
       4. the `stat` dev and inode equal the `/proc/locks` line.
    2. **Major-0 sample.** Record one real btrfs or overlay-root sample (`stat` dev/ino against the `/proc/locks`
       line), and add it as a fixture to the AC-002 lenient test. If none is available, state explicitly that the
       inode-only path is **unverified on real hardware**. As of this ADD, no managed host with btrfs is known.
    3. **A standalone reboot during a held lock** reports the waited or skip text. **Only Ryan triggers the actual
       reboot.**

## 13. Definition of Done

- [ ] Every Section 4 change implemented, and every "explicitly unchanged" path untouched (`git diff --stat`;
      `go.mod`/`go.sum`, `scripts/`, migrations and `web/` show no diff)
- [ ] Every Section 8 test exists, carries its `RD_ACnnn` / `RD_Cn` name and `[AC-nnn]` / `[C-n]` messages, and passes
- [ ] `go test ./...` (Windows) and `go test -race ./...` (Linux CI) green; `cd web && npm run test:run` green
      (unchanged)
- [ ] `go vet ./...` clean; `make build-agent-all` succeeds for every target
- [ ] No migration; `make migrate-up` / `make migrate-down` unaffected
- [ ] The C-8 test's assertions are unchanged; its helper records one entry per `execReboot` reference
- [ ] `grep -rn "rebootPermit{" internal/agent --include=*.go` (non-test) matches only inside `mintRebootPermit`
- [ ] `grep -rn "time.Now()\|time.Sleep" internal/agent/dpkglock*.go internal/agent/reboot_clearance.go` (non-test)
      returns nothing
- [ ] `AgentHardMax("shutdown",1) == time.Minute`, `ExecDeadline("shutdown",1) == 10*time.Minute`, and `QueuedTTL`
      is unchanged for both power actions
- [ ] C-6 host verification and the major-0 sample, or the "unverified" statement, recorded in the implementation
      report
- [ ] QA gate C-7: AC-010, AC-011, AC-012, AC-014, AC-015, AC-016, AC-018, AC-020, the C-8 test and the C-1/C-3
      tests pass
