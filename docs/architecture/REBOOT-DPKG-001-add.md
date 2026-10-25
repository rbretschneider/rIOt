# Architecture Decision Document

- **Story ID:** REBOOT-DPKG-001
- **FRD Reference:** `docs/requirements/REBOOT-DPKG-001-frd.md`
- **Security review:** `docs/security/REBOOT-DPKG-001-security-review.md` (APPROVED WITH CONDITIONS)
- **Blockers / architect questions:** `docs/architecture/REBOOT-DPKG-001-blockers.md` (resolved by revision 3)
- **Origin:** PATCH-FLOW-001 security review INFO-4
- **Amends:** the power-class budgets in CMD-LIFECYCLE-001 AD-005 / AD-013, for `reboot` only (AD-004 below)
- **Author:** Architect Agent
- **Date:** 2026-10-24
- **Status:** FINAL, revision 3 (implementation questions resolved)

## Changelog

### Revision 3: implementation questions (senior_dev blockers file)

| Item | Source | Decision |
|---|---|---|
| ARCH-QUESTION 1: `NSpid` alone is ineffective | blockers file, deviation 1 | **AD-001 step 4 is rewritten.** The kernel prints `NSpid` starting from the pid namespace of the `/proc` mount being read. An agent in a private pid namespace that has its own `/proc` (`PrivatePIDs=yes`, a container) therefore sees one field and passed the revision 2 check: the check failed open. The primary check is now `readlink(<ProcRoot>/self/ns/pid) == "pid:[4026531836]"`. That value is `PROC_PID_INIT_INO`, a fixed kernel constant since Linux 3.8, and reading one's own link needs no privilege. `NSpid` is kept as a secondary check. An unreadable or malformed link gives `Unknown`. `dpkgProbeEnv` gains `Readlink`. New tests include a non-init namespace fixture (§8 C-1). **R-2** is rewritten |
| NOTE 2: `_` in holder names | blockers file, deviation 2 | `_` is **kept**, as AD-001, §10 and the security review say. The §8 C-5 row is corrected. senior_dev's implementation already matches |
| Probe and gate in one commit | deviation 3 | **Accepted.** The AC-018 AST test scans both files, so the probe cannot be green alone. §12 note 1 is updated |
| `readSmallFile` trims a capped read to its last complete line | deviation 4 | **Accepted and specified** (AD-001, "Small reads"). A line cut by the cap is never parsed |
| A blank `/proc/locks` line gives `Unknown`; the gate's `default` case fails closed | deviation 5 | **Accepted and specified** (AD-001 step 6, AD-002) |
| `ReaderStopsAtCap` asserts ≤ 1 MiB + 1 byte served | deviation 6 | **Accepted.** It is stricter than revision 2; the §8 AC-011 row is updated |
| Extra tests (deviation 7) and the documented 5 s guard cap assumption (deviation 8) | deviations 7, 8 | **Accepted.** The 5 s cap is `maintenance.go:38` (`context.WithTimeout(ctx, 5*time.Second)` around `systemctl show`) |
| Zero value of `dpkgLockState` | impl report, notes for QA | **New hardening.** `dpkgLockUnknown` becomes the zero value, so a hand-built or zero `dpkgLockStatus{}` fails closed instead of reading as `Absent`. One test is added |

### Revision 2: security conditions C-1 to C-6

| Condition | Finding | What changed |
|---|---|---|
| C-1 | SEC-001 | **AD-001:** `Unknown` when the agent is not in the initial pid namespace (completed in revision 3), or when dpkg is present (`/var/lib/dpkg/status` or `/usr/bin/dpkg`) but both lock files are missing or hidden. `Absent` means "not a dpkg host". Pid semantics corrected. **AD-002:** `Held` then `Absent` skips as `Unknown`. Unit-file `PrivatePIDs=` regression test. **R-2** |
| C-2 | SEC-002 | **AD-001:** holder text carries the uid and marks inode-only matches. **R-3 / R-4** rewritten. README note |
| C-3 | SEC-003 | **AD-007:** `execReboot(p rebootPermit)`, with a compile-time type, a runtime single-use check and a per-reference call-site test |
| C-4 | SEC-004 | **R-5** corrected for the automatic agent updater. Server-first upgrade note |
| C-5 | INFO-1 | **AD-001:** cap at the reader, kernel-variant fixtures, pid-only paths, holder charset `[A-Za-z0-9._+:-]` |
| C-6 | INFO-2, INFO-3 | Inode-only path **unverified on real hardware**. **R-1:** "sub-second to a few seconds" |

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

A new agent-side probe finds out whether the dpkg lock is held. It uses `stat()` on the dpkg lock files and on
dpkg's own presence markers. It checks that the agent is in the initial pid namespace, through its own
`/proc/self/ns/pid` link plus `NSpid`, so that `/proc/locks` shows every holder on the host. It then reads
`/proc/locks` with a cap at the reader. Lines are matched on device and inode, and the holder is named from
`/proc/<pid>/comm` and the uid in `/proc/<pid>/status`. The probe needs no privilege, no exec and no new dependency.

One shared gate, `awaitRebootClearance`, runs a bounded wait, the path's guards and a final lock check. When it clears,
it mints a single-use `rebootPermit`. `execReboot` accepts only that permit.

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
- **Kernel facts used by AD-001 step 4** (revision 3):
  - `task_state()` (`fs/proc/array.c`) prints `NSpid` for levels `proc_pid_ns(sb)->level` through the task's own
    level. The field count is therefore relative to the namespace of the `/proc` mount being read, not to the initial
    namespace.
  - The initial pid namespace has the fixed nsfs inode `PROC_PID_INIT_INO = 0xEFFFFFFC = 4026531836`. Every other pid
    namespace gets a dynamically allocated inode.
  - `readlink("/proc/self/ns/pid")` returns `pid:[<inode>]` for the reader's own namespace. It needs only
    `ptrace_may_access` on self, which always holds.

**Missing:** any dpkg lock awareness on the reboot paths, a reboot budget that fits a 10-minute wait, a skip event on
the unauthorized path's post-delay guard failures, and any non-textual enforcement of "gate before reboot".

## 3. Architecture Decisions

### AD-001: Lock detection via `stat()`, a pid-namespace check and `/proc/locks` (FR-001, FR-002, FR-006, FR-007, FR-010, FR-018, NFR-001, NFR-003; C-1, C-2, C-5)

**Decision.** A pure-Go probe lives in `internal/agent/dpkglock.go`. It has no build tag, so it is tested everywhere.

```go
type fileID struct{ Major, Minor uint32; Ino uint64 }

// dpkgLockUnknown is deliberately the zero value: a zero or hand-built status fails closed (revision 3).
type dpkgLockState int // dpkgLockUnknown (0) | dpkgLockAbsent | dpkgLockFree | dpkgLockHeld

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
    Readlink    func(path string) (string, error) // revision 3; production os.Readlink
    ProcRoot    string                            // "/proc": <root>/self/ns/pid, <root>/self/status, <root>/locks, <root>/<pid>/{comm,status}
    Now         func() time.Time
}
const initPidNamespaceLink = "pid:[4026531836]" // PROC_PID_INIT_INO (0xEFFFFFFC), fixed since Linux 3.8

func probeDpkgLock(env dpkgProbeEnv) dpkgLockStatus
func parseProcLocksLine(line string) (procLock, bool /*skip*/, error)
func lockMatches(l procLock, t fileID) (match, inodeOnly bool)
func parseNSpid(statusText string) (levels int, ok bool)
func parseStatusUID(statusText string) (uid int64, ok bool)
func readSmallFile(env dpkgProbeEnv, path string) (string, error) // ≤ 8 KiB; trimmed to the last complete line when capped
func sanitizeHolderName(b []byte) string
func decodeLinuxDev(dev uint64) (major, minor uint32) // glibc gnu_dev_major/minor
```

Constants:
- `dpkgProbeMaxBytes = 1 << 20` and `dpkgProbeReadBuf = 64 << 10`;
- `dpkgLineMax = 4 << 10`;
- `dpkgSmallReadMax = 8 << 10`;
- `dpkgProbeTimeout = 1 * time.Second`;
- `dpkgHolderNameMax = 16` and `dpkgCauseMaxBytes = 200`.

**Small reads** (`readSmallFile`, used for `self/status`, `<pid>/comm` and `<pid>/status`; revision 3):
- The function reads at most `dpkgSmallReadMax + 1` bytes.
- Within the cap, it returns the full content.
- When the cap is hit, it returns the content up to and including the last `\n`. A line cut by the cap is never
  parsed, so a truncated `NSpid:` or `Uid:` line cannot be misread. With no complete line at all, it returns `""`.

**Algorithm of `probeDpkgLock`** (one call is one "lock check"):

1. `start := env.Now()`.
2. **Lock files.** `StatID` each of the `LockPaths`, in order.
   - `fs.ErrNotExist` → skip that path.
   - Any other error → `Unknown`, cause `cannot stat <path>: <err>`.
   - Otherwise add the result to the targets.
3. **No targets, so check whether this is a dpkg host (C-1 b).** `StatID` each of the `DpkgMarkers`.
   - Any marker exists → `Unknown`, cause `dpkg is installed but its lock files are missing or hidden`.
   - Any error other than not-exist → `Unknown`, cause `cannot stat <path>: <err>`.
   - All markers not-exist → `Absent`. Nothing under `/proc` is read and `Readlink` is not called, so dnf and other
     hosts are unaffected (FR-006).
4. **Pid namespace (C-1 a; revision 3).** Both checks must pass, in this order:
   1. **Primary: `Readlink(<ProcRoot>/self/ns/pid)`.**
      - An error (missing link, `/proc/self` not resolvable because the `/proc` belongs to another namespace,
        `EACCES`) → `Unknown`, cause `cannot verify the pid namespace: <err>`.
      - A value that is not of the form `pid:[<decimal>]` → `Unknown`, cause
        `cannot verify the pid namespace: unexpected link`. The value is not echoed.
      - Any well-formed value other than `initPidNamespaceLink` → `Unknown`, cause
        `agent is not in the host pid namespace`.
      - This catches the `PrivatePIDs=yes` / container case, where the namespace's own `/proc` makes `NSpid` show
        one field.
   2. **Secondary: `readSmallFile(<ProcRoot>/self/status)` and `parseNSpid`.**
      - Exactly one field → pass.
      - More than one field → `Unknown`, cause `agent is not in the host pid namespace`.
      - Line missing (kernel before 4.1, or cut by the cap) or read error → `Unknown`, cause
        `cannot verify the pid namespace: <err|no NSpid>`.
      - In the initial namespace this check is redundant. It is kept as a cheap second signal, and it still refuses
        an ancestor's `/proc` that has been bind-mounted in front of the agent.

   Both values are read from the same `ProcRoot` as `locks`.
5. **Read `/proc/locks` with a cap at the reader (C-5).**
   1. `Open(<ProcRoot>/locks)`. **Any** error → `Unknown`, cause `cannot read /proc/locks: <err>`. That includes
      `ENOENT`, which is what `ProcSubset=pid` produces.
   2. `r := bufio.NewReaderSize(io.LimitReader(f, dpkgProbeMaxBytes+1), dpkgProbeReadBuf)`.
   3. A `bufio.Scanner` over `r` uses `Buffer(make([]byte, 0, dpkgLineMax), dpkgLineMax)`.
   4. Count consumed bytes, including newlines. More than `dpkgProbeMaxBytes` → `Unknown`, cause
      `lock information exceeds 1 MiB`. The reader never serves more than `dpkgProbeMaxBytes + 1` bytes.
   5. A token over 4 KiB, or a scanner error → `Unknown`, cause `cannot read /proc/locks: <err>`.
   6. After each line, `env.Now().Sub(start) > dpkgProbeTimeout` → `Unknown`, cause `lock check exceeded 1s`.
   7. The file is read to EOF (within the cap).
6. **Parse each line** (`parseProcLocksLine`):
   1. `fields := strings.Fields(line)`. A blank or whitespace-only line is unrecognized, so the result is `Unknown`
      (revision 3: the kernel never emits one). `fields[0]` must be a decimal number followed by `:`.
   2. Any `->` tokens after it mark a blocked **waiter**, including nested and indented waiters. A waiter line is
      skipped.
   3. The rest needs ≥ 7 fields: `TYPE MODE ACCESS PID DEV START END`. Each of `TYPE`, `MODE` and `ACCESS` is any
      non-empty token, and every type counts. That includes `POSIX`, `ACCESS`, `OFDLCK`, `FLOCK`, `LEASE`, `DELEG`
      and `UNKNOWN`, with modes such as `ADVISORY`, `MANDATORY`, `*NOINODE*`, `MSNFS`, `ACTIVE`, `BREAKING`,
      `BREAKER` and `UNKNOWN`.
   4. `PID` is parsed with `strconv.ParseInt(tok, 10, 64)`. Values ≤ 0 mean "not visible".
   5. `DEV` is `MAJ:MIN:INO`, with hex `MAJ`/`MIN` and decimal `INO`. `<none>:0` means no inode, and the line is
      skipped.
   6. Anything else → `Unknown`, cause `unrecognized /proc/locks line <n>`. The line content is never echoed.
   7. **Pid semantics (C-1 d).** The kernel **omits** locks whose holder pid does not translate into the reader's pid
      namespace, rather than printing pid 0. Step 4 guarantees the agent reads the initial namespace's view.
7. **Match.** `lockMatches(l, t)` is a match when `l.Ino == t.Ino` and one of the following holds:
   - **exact:** `l.Major == t.Major && l.Minor == t.Minor`;
   - **inode-only:** `t.Major == 0 && l.Major == 0`. This sets `InodeOnly = true`.
8. **Holder.** The first matching non-waiter line, in file order, gives `Held`. When `PID > 0`, the paths are built
   **only** from `strconv.FormatInt(pid, 10)` (C-5):
   - `readSmallFile(<ProcRoot>/<pid>/comm)` → `sanitizeHolderName`: trim trailing whitespace, map every byte outside
     `[A-Za-z0-9._+:-]` to `?`, keep ≤ 16 bytes. **`_` is kept.**
   - `readSmallFile(<ProcRoot>/<pid>/status)` → `parseStatusUID`: the first (real) value of the `Uid:` line.

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

**Platform split.**
- **`dpkglock_linux.go`:**
  - `statFileID`: `os.Stat`, then `fi.Sys().(*syscall.Stat_t)`, then `decodeLinuxDev(uint64(st.Dev))` and
    `uint64(st.Ino)`.
  - `platformDpkgProbe(now)`: the real paths, `os.Open`, **`os.Readlink`** and `"/proc"`.
- **`dpkglock_other.go`** (`//go:build !linux`): `platformDpkgProbe` returns `Absent`. Windows is unchanged
  (FR-006).
- **`decodeLinuxDev`:**
  - `major = ((dev >> 8) & 0xfff) | ((dev >> 32) &^ 0xfff)`
  - `minor = (dev & 0xff) | ((dev >> 12) &^ 0xff)`

  Hawaii's `66306` decodes to `259:2`.

**Agent seam.** `Agent.dpkgLockProbe func() dpkgLockStatus`. When it is nil, `a.probeDpkg()` calls
`platformDpkgProbe(a.clock().Now)`.

**Rationale.**
- **Why `/proc/locks`.** The agent user cannot open the 0640 `root:root` lock files, so `F_GETLK` is impossible
  without a new sudo rule. `stat()`, `readlink` on its own `ns` link, and `/proc` (0444) all work unprivileged.
- **FR-007.** The lock files are never opened. The `/proc/locks` read costs the same as `lslocks` (INFO-1).
- **Empty answers fail closed (SEC-001).**
  - A non-initial pid namespace is refused by identity: the namespace inode, not a field count.
  - Deleted or hidden lock files on a dpkg host are refused.
  - Only a host without dpkg reads as `Absent`.
  - The zero state is `Unknown`.
- **Why the anonymous-device leniency.** `/proc/locks` prints the superblock device. On btrfs and similar
  filesystems, `stat()` returns a different anonymous `st_dev`, so an exact match would fail open there. INFO-2 agrees
  with the trade-off. **This path is unverified on real hardware** (C-6).
- **Parsing.** Strict parsing fails closed on an unknown format. Every variant the kernel is known to print has a
  fixture (C-5).

**Alternatives considered.**
- *`F_GETLK` through a sudo helper.* Rejected by NFR-001.
- *`fuser`/`lsof` or `/proc/*/fd`.* Rejected: unreadable for root holders, and both spawn a process.
- *Process-name heuristics.* Rejected.
- *`--check-inhibitors`.* Rejected: it needs a new sudoers argv.
- *`NSpid` field count as the only namespace check (revision 2).* **Rejected in revision 3:** the count is relative
  to the `/proc` mount, so a private namespace with its own `/proc` passes (blockers file, ARCH-QUESTION 1).
- *Comparing `/proc/self/ns/pid` with `/proc/1/ns/pid`.* Rejected: reading pid 1's link needs ptrace access to it,
  which means root.
- *`stat()` of `/proc/self/ns/pid`, comparing the nsfs inode number.* Equivalent to the readlink check, but it also
  needs the nsfs device, and the string form is simpler and testable on Windows. Rejected.
- *Checking `/proc/1/comm == systemd` or similar.* A heuristic. Rejected.
- *Dropping `NSpid` once the link check exists.* Rejected: it is a cheap, already-tested second signal, and it covers
  the bind-mounted-ancestor case.
- *Exact device match only, or inode-only everywhere.* Rejected (INFO-2).

**Consequences.**
- Per probe: up to 4 `stat` calls, 1 `readlink`, one 8 KiB `self/status` read, one capped `/proc/locks` read, and up
  to two 8 KiB holder reads.
- Hosts that cannot satisfy the checks get a visible `lock state unknown` skip (R-2, R-4, R-9).

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
   and the command ID on the post-patch paths.

**Loop (pseudocode):**

```
var firstHeld time.Time; waiting := false; sawHeld := false; guardsPassed := false
for {
    if a.shuttingDown.Load()                  -> return skip(skipShutdown)
    st := a.probeDpkg()
    switch st.State {
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
    default: // Unknown, and any value outside the four states (revision 3: fails closed)
        -> return skip(skipLockUnknown, st.Cause or "lock state not recognized")                   // FR-010
    }
}
```

When the gate returns after waiting at least once, it writes exactly one `Info "dpkg lock wait ended"` line with the
`path`, `command_id`, `outcome`, `waited` and `holder` attributes.

**Properties.**
- **Free at the first check (FR-005):** probe → guards → probe → cleared, with zero sleeps and zero wait logs.
- **Not a dpkg host, and Windows (FR-006):** the same sequence with `Absent` both times.
- **One wait per attempt, one deadline `firstHeld + 10m` (FR-009, FR-012).**
- **`Held` then `Absent` never clears (C-1 c).**
- **Release to reboot (FR-004):** ≤ 9 s. **Shutdown (FR-013):** ≤ 3 s. **Worst wall time:** about 10m07s.

**Placement.** Every `execReboot` call receives the gate's `Permit` (AD-007).

| Path | Before the gate (kept, fail-fast) | Gate call | Skip | Clearance |
|---|---|---|---|---|
| `handleReboot(cr)` | `if !a.config.Commands.AllowReboot` → existing error | `c := a.awaitRebootClearance(cr.ctx, "reboot", cr.id, a.commonRebootGuards(""))` | AD-003 (b)–(e) | `a.execReboot(c.Permit)` → (a) |
| `maybeRebootAfterPatch` goroutine | the existing checks, reservation and claim; `sleep(rebootDelay)`; then the existing re-check, now reported through `abortPostPatchReboot` | `c := a.awaitRebootClearance(commandContext, "post_patch", id, a.commonRebootGuards(id))` | `abortPostPatchReboot(id, c.reason())` | `a.execReboot(c.Permit)` |
| `rebootAfterDelivery` | the existing delivery wait and checks | `c := a.awaitRebootClearance(commandContext, "authorized_post_patch", id, a.commonRebootGuards(id))` | `abortAuthorizedReboot(id, c.reason())` | Info log (adds `waited`), `a.execReboot(c.Permit)` |

**Alternatives considered.**
- *The wait inside `execReboot`.* Rejected.
- *A SIGTERM-cancelled context.* Rejected.
- *Adding the post-patch goroutines to `cmdWG`.* Rejected.
- *Per-check Debug logs.* Rejected by NFR-002.
- *Keep waiting on `Held`→`Absent`.* Rejected in favour of an immediate `Unknown` skip.

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
- The gate and `execReboot` run synchronously.
- The permit is single-use (AD-007).
- The seen-ID store blocks redelivery.
- If the `cr.ctx` deadline (12m) ever fired, the outcome would be the truthful skip (e).

**Alternatives considered.** *A `dpkg_lock_wait` progress step.* Deferred: the FRD rules out any UI change. It is the
follow-up to INFO-4.

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

- **TTL is unchanged:** lengthening it would let a stale reboot fire later.
- **Sweeper:** no code change. New reboot rows time out at about 20–21 min with `no result from agent within 20m`.

| Outcome | Agent | Server row |
|---|---|---|
| Started, no wait | (a) in ms | `succeeded` |
| Started after 9m (AC-017) | (a) at about 9m | `succeeded` at about 9m |
| Timeout | (b) at 10m + ≤ 7 s | `failed` |
| Unknown / guard | (c) / (e) | `failed` |
| Shutdown | (d) within 3 s, durable in the outbox | `failed`, reason `shutdown` |

**Alternatives considered.**
- *Raise all of `ClassPower`.* Rejected by FR-020.
- *A detached reboot.* Rejected.
- *Report success, then wait.* Rejected by FR-015.
- *Cap the wait.* Rejected by OQ-003.
- *Let progress extend the deadline.* Forbidden.

**Consequences.**
- A lost reboot result is detected after 20 min instead of 10.
- A hard-killed agent leaves the host-keyed row blocking until the sweep (INFO-4).
- Version skew: R-5.

---

### AD-005: Post-patch skip reporting and reservation release (FR-017, FR-018, FR-019)

- **Authorized path.** Uses the existing `abortAuthorizedReboot(id, reason)`: one Warn, one warning
  `EventCommandCompleted` event `authorized reboot for command <id> was not started: <reason>`, and one telemetry
  trigger.
- **Unauthorized path.** A new `abortPostPatchReboot(id, reason)` mirrors it:
  - one `slog.Warn("os_update: post-patch reboot not started", ...)`;
  - one warning event `post-patch reboot for command <id> was not started: <reason>`;
  - one `triggerTelemetry()`.

  It is used for every non-reboot exit inside the goroutine. Exits before the goroutine are unchanged.
- **Reason text** (`rebootClearance.reason()`):
  - `dpkg lock held past the timeout (10m), <holderText>`;
  - `dpkg lock state unknown: <cause>`;
  - `agent shutting down`;
  - the guard text.

  Holder text is restricted to `[A-Za-z0-9._+:-]`, so the notifiers cannot be fed markup.
- **Reservation (FR-019).** The goroutines' `defer Unlock` releases it on every skip (AC-009).
- **Shutdown exception.** The Warn is always written; the event is best-effort (R-7).
- **Counts.** At most one event and one telemetry trigger per attempt.

---

### AD-006: Bounded growth (NFR-002, NFR-003, house rule)

| Item | Bound per reboot attempt |
|---|---|
| Wait | ≤ 1; one deadline `firstHeld + 10m` |
| Lock checks | ≤ 10m / 2s + 3 = **303** probes |
| Reads per probe | ≤ 4 `stat`; 1 `readlink`; `self/status` ≤ 8 KiB; `/proc/locks` ≤ 1 MiB + 1 byte (cap at the reader); `<pid>/comm` and `<pid>/status` ≤ 8 KiB each; ≤ 1 s total |
| Memory | O(1): a 64 KiB read buffer, a 4 KiB line buffer, ≤ 8 KiB small reads, one status |
| Guard evaluations | only on a free observation: ≤ 1 per poll (normally 1); ≤ 1 `systemctl show` each, capped at 5 s (`maintenance.go:38`) |
| Info-or-above log lines from the gate | ≤ 2, and only when a wait happened; none per check |
| Path outcome lines | unchanged |
| Events | standalone: the existing failed-command event; post-patch: ≤ 1 |
| Telemetry triggers | post-patch: ≤ 1 (coalesced) |
| Permits | ≤ 1 minted per attempt and consumed at most once |
| Persistent state | **none** |

---

### AD-007: `execReboot` accepts only a gate-minted, single-use permit (C-3, SEC-003)

**Decision.**

```go
// rebootPermit is proof that awaitRebootClearance returned Cleared for one attempt.
type rebootPermit struct{ id uint64 }

var errRebootNotCleared = errors.New("reboot refused: no dpkg-lock clearance for this attempt")

func (a *Agent) mintRebootPermit() rebootPermit // id = a.rebootPermitSeq.Add(1); a.rebootPermitLive.Store(id)
func (a *Agent) execReboot(p rebootPermit) error
```

New `Agent` fields: `rebootPermitSeq` and `rebootPermitLive`, both `atomic.Uint64`.

**`execReboot` runtime check.** Before doing anything else,
`if p.id == 0 || !a.rebootPermitLive.CompareAndSwap(p.id, 0) { return errRebootNotCleared }`.
- A zero permit, a reused permit and a stale permit are all refused.
- On refusal it does not call the runner or `exec`.

**Call-site test** (`reboot_callsites_test.go`). The helper records every `*ast.SelectorExpr` named `execReboot` in
non-test files, one entry per reference. Each reference must pass all of these checks:
1. It is the `Fun` of a call. A method value fails.
2. Its enclosing function is one of the three C-8 callers.
3. The body text before it contains `Commands.AllowReboot`.
4. Its single argument is `<ident>.Permit`, where `<ident>` is assigned from `a.awaitRebootClearance(` earlier in the
   same function and is checked with `if !<ident>.Cleared` before the call.

The C-8 assertions are kept.

**Alternatives considered.**
- *`execReboot(c rebootClearance)`.* Rejected: forgeable inside the package.
- *Exported or interface-based capabilities.* Over-engineered.

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `internal/agent/dpkglock.go` | `fileID`, `procLock`, `dpkgLockState` (zero = `Unknown`), `dpkgLockStatus` + `holderText`, `dpkgProbeEnv` (with `Readlink`), `initPidNamespaceLink`, `probeDpkgLock`, `parseProcLocksLine`, `lockMatches`, `parseNSpid`, `parseStatusUID`, `readSmallFile`, `sanitizeHolderName`, `decodeLinuxDev`, constants, `(a *Agent) probeDpkg()` (AD-001) |
| CREATE | `internal/agent/dpkglock_linux.go` | `statFileID` (`syscall.Stat_t`); `platformDpkgProbe` with the real paths, `os.Open` and `os.Readlink` |
| CREATE | `internal/agent/dpkglock_other.go` | `//go:build !linux`; `platformDpkgProbe` returns `Absent` |
| CREATE | `internal/agent/dpkglock_test.go` | Parser, kernel-variant fixtures, matcher, pid-namespace link and `NSpid`, dpkg markers, caps, small reads, holder and uid tests on every platform (fake `StatID`/`Open`/`Readlink`/`Now`); unit-file `PrivatePIDs=` regression test |
| CREATE | `internal/agent/dpkglock_linux_test.go` | Real `/proc/locks` + `syscall.FcntlFlock` on a temp file; real `/proc/self/ns/pid` and `self/status`; `statFileID` not-exist. A shared helper `requireInitPidNamespace(t)` skips, with a logged reason, the real-probe tests when the test process is not in the initial pid namespace (Docker) |
| CREATE | `internal/agent/dpkglock_other_test.go` | `//go:build !linux`; the platform probe is `Absent` (runs on Windows) |
| CREATE | `internal/agent/reboot_clearance.go` | `rebootGuards`, `rebootClearance` + `reason()`, `awaitRebootClearance` (including the `sawHeld` rule and the fail-closed `default`), `commonRebootGuards`, `abortPostPatchReboot`, `rebootPermit`, `mintRebootPermit`, `errRebootNotCleared`, `dpkgLockPoll` |
| CREATE | `internal/agent/reboot_clearance_test.go` | Gate, three-path and permit tests (§8) |
| MODIFY | `internal/agent/commands.go` | `handleReboot(cr *cmdRun) commandExecResult`; `executeAction` `case "reboot"`; `maybeRebootAfterPatch` goroutine through the gate and `abortPostPatchReboot`; `execReboot(p rebootPermit) error` with the runtime check |
| MODIFY | `internal/agent/reboot_auth.go` | `rebootAfterDelivery`: gate after the existing checks; skip → `abortAuthorizedReboot(id, c.reason())`; `execReboot(c.Permit)`; Info log gains `waited` |
| MODIFY | `internal/agent/agent.go` | Seam `dpkgLockProbe`; fields `rebootPermitSeq`, `rebootPermitLive` |
| MODIFY | `internal/agent/reboot_callsites_test.go` | Helper: one entry per `execReboot` reference; C-8 assertions kept; RD/C-3 tests |
| MODIFY | `internal/agent/command_lifecycle_test.go` | `newLifecycleHarnessAt` sets a fixed `Free` probe |
| MODIFY | `internal/agent/commands_test.go` | `newOSUpdateTestAgent` sets the same `Free` probe |
| MODIFY | `internal/models/command_policy.go` | `DpkgLockWaitBound`, `RebootAgentHardMax`, `RebootExecDeadline`; `reboot` branches; doc comments |
| MODIFY | `internal/models/command_policy_test.go` | `TestCL_AC029_ExecDeadlineTable` reboot row → 20m; RD budget tests |
| MODIFY | `internal/server/commands/service_test.go` | Reboot `dl` 600 → 1200 (TTL stays 15m) |
| MODIFY | `internal/server/commands/sweeper_test.go` | `TestSweep_RD_AC017_RebootNotTimedOutBefore20m` |

**Revision 3 delta for senior_dev:**
- `dpkglock.go`: add `Readlink` to `dpkgProbeEnv`, add `initPidNamespaceLink`, add step 4.1, and reorder the
  `dpkgLockState` constants so that `Unknown` is 0.
- `dpkglock_linux.go`: wire `os.Readlink`.
- `reboot_clearance.go`: make sure the `default` case covers `Unknown`. It already fails closed.
- Tests: the new §8 C-1 rows, the zero-value test, the corrected C-5 row, and `requireInitPidNamespace` in
  `dpkglock_linux_test.go`.
- Every fake `dpkgProbeEnv` that reaches step 4 must now supply `Readlink`. Fixtures default to
  `pid:[4026531836]`.

**Explicitly unchanged:**
- `internal/server/commands/sweeper.go`, `service.go` and `internal/server/db/*`
- all migrations
- `scripts/install.sh`, `scripts/riot-agent.service`, sudoers (read by a test only)
- `internal/agent/doctor.go`
- `handleShutdown`
- `internal/maintenance/*`
- `web/`
- `go.mod` / `go.sum`

**Technical writer (post-QA):**
- **README**:
  - the reboot dpkg wait and its five outcomes, and the post-patch skip event;
  - split the Power row of the TTL/deadline table: `reboot` 15 min / **20 min**, `shutdown` 15 min / 10 min;
  - requirements: a readable `/proc/locks`, and the agent must run in the **host (initial) pid namespace** (no
    `PrivatePIDs=`, no container). Otherwise every reboot on a dpkg host skips with
    `agent is not in the host pid namespace`. A dpkg host with missing lock files skips;
  - **local-user veto (C-2):** any local user can block rIOt reboots; check the reported uid;
  - **upgrade order (C-4):** upgrade the server first;
  - the residual window is sub-second to a few seconds (INFO-3).
- **`web/src/pages/LearnMore.tsx`:** reboots wait up to 10 min for a running package manager and are skipped, not
  forced.
- **CHANGELOG** `[Unreleased]`: Added (the dpkg wait); Changed (reboot deadline 20 min); **Upgrade note:** server
  first.
- **Stale content to log, not edit:**
  - CMD-LIFECYCLE-001 ADD §AD-005 (power 10m/1m);
  - FRD NFR-004 "milliseconds".

## 5. Data Model Changes

**Postgres: none.** No migration, table, column or index. New `reboot` rows store `exec_deadline_s = 1200`.

**Models (code only):** three constants and two `reboot` branches (AD-004).

**Agent on-disk state: none new.** The probe reads lock-file metadata, the dpkg markers, the `self/ns/pid` link,
`/proc/self/status`, `/proc/locks` and `/proc/<pid>/{comm,status}`. It writes nothing.

**In-memory state** (never persisted):
- per attempt: `waiting`, `firstHeld`, `sawHeld`, `guardsPassed`;
- per agent: `rebootPermitSeq`, `rebootPermitLive`.

## 6. API / Interface Contract

No new or changed endpoints.

### Command record (existing)

| Field | Change |
|---|---|
| `exec_deadline_s` | new `reboot` rows `1200` (was `600`); `shutdown` `600` |
| `deadline_at` | `dispatched_at + 1200 s` for new `reboot` rows |
| `expires_at` | unchanged |
| `result_msg`, `status`, `outcome`, `reason` | the AD-003 table |

### Agent command result for `reboot` (existing shape)

```
reboot initiated
reboot initiated after waiting <D> for the dpkg lock
reboot skipped: dpkg lock held past the timeout (10m), <holderText>
reboot skipped: dpkg lock state unknown: <cause>
reboot skipped: agent shutting down                       (reason "shutdown")
reboot skipped: guard failed: <guard text>                (reason "conflict" for "host busy: …")
```

`<cause>` is one of:
- `cannot stat <path>: <err>`
- `dpkg is installed but its lock files are missing or hidden`
- `agent is not in the host pid namespace`
- `cannot verify the pid namespace: <err|unexpected link|no NSpid>`
- `cannot read /proc/locks: <err>`
- `lock information exceeds 1 MiB`
- `lock check exceeded 1s`
- `unrecognized /proc/locks line <n>`
- `dpkg lock files disappeared during the wait`
- `lock state not recognized`

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
3. `handleReboot(cr)`: `AllowReboot` passes, so it calls `awaitRebootClearance`.
4. The probe:
   1. stats the lock files (present);
   2. reads `/proc/self/ns/pid` = `pid:[4026531836]`;
   3. reads `NSpid` (one field);
   4. reads `/proc/locks` → `Held`, `held by unattended-upgr (pid 1234, uid 0)`.

   One Info `wait started` line is written.
5. Every 2 s: `shuttingDown`? → probe → `Held` → sleep.
6. At T0+9m01s the probe returns `Free`, the guards pass, and the final probe returns `Free`. The gate mints a permit
   and returns Cleared with Waited 9m. One Info `wait ended` line is written.
7. `execReboot(c.Permit)` consumes the permit and starts the reboot. The result
   `success, "reboot initiated after waiting 9m for the dpkg lock"` goes to the outbox.

### 7.2 Standalone reboot, timeout

At `firstHeld + 10m` the lock is still `Held`, so the result is
`error, "reboot skipped: dpkg lock held past the timeout (10m), held by unattended-upgr (pid 1234, uid 0)"`.
No permit is minted.

### 7.3 Unauthorized post-patch, lock re-taken

1. `Held` at minute 0, then `Free` at minute 4, and the guards pass.
2. The final probe returns `Held`, so the same deadline continues.
3. At minute 10 → timeout. `abortPostPatchReboot` writes one Warn, sends one event and triggers telemetry once. The
   reservation is released.

### 7.4 Lock files deleted during a wait

The probe sees no targets and finds `/var/lib/dpkg/status`, so it returns `Unknown` and the skip is immediate. If the
markers also vanished, the probe returns `Absent`, and the `sawHeld` rule still skips as `Unknown`.

### 7.5 Agent in a private pid namespace (revision 3)

`PrivatePIDs=yes` (or a container) gives the agent its own `/proc`. `NSpid` shows one field, but
`/proc/self/ns/pid` reads `pid:[40265324xx]`. The probe returns `Unknown` (`agent is not in the host pid namespace`)
before it reads `/proc/locks`, so every reboot attempt on a dpkg host skips visibly. Non-dpkg hosts are unaffected,
because step 3 returns `Absent` first.

### 7.6 Authorized phase B; agent stop

- **Phase B:** as in 7.1, but with `abortAuthorizedReboot` on a skip.
- **Agent stop:** the gate ends ≤ 3 s after SIGTERM with `skipShutdown`.

## 8. Acceptance Criteria Mapping

**Naming.**
- Go test names contain `RD_ACnnn`, or `RD_Cn` for security conditions without an AC.
- Assertion messages start with `[AC-nnn]` or `[C-n]`.
- "Three paths" tests are table-driven over `standalone`, `unauthorized` and `authorized`. They use the
  `lifecycleHarness`, `fakeClock`, a `pause` that advances the clock, and a scripted `dpkgLockProbe`.
- Probe tests use a fake `dpkgProbeEnv`. Its `Open` serves in-memory files and records the paths requested. Its
  `Readlink` returns `pid:[4026531836]` unless a test overrides it.

| AC | Fulfilled By | Tests |
|---|---|---|
| AC-001 | AD-002 free branch; AD-007 | `TestRebootClearance_RD_AC001_FreeLockRebootsWithNoSleepOnAllThreePaths`; `TestRebootClearance_RD_AC001_FreeRunsGuardsThenFinalProbe` |
| AC-002 | AD-001; AD-002 held branch | `TestRebootClearance_RD_AC002_HeldLockNeverRebootsWithinBoundOnAllThreePaths`; `TestProcLocks_RD_AC002_PosixWriteLineMatchesLockFrontend`; `…_HexMajorMinorDecimalInode`; `…_BlockDeviceRequiresExactDevice`; `…_AnonymousDeviceMatchesOnInodeAndMarksInodeOnly` (synthetic; C-6); `TestProcLocks_RD_AC002_KernelVariantFixtures` (ACCESS; `*NOINODE*` + `<none>:0`; `FLOCK  MSNFS`; `LEASE  BREAKING`; `LEASE  BREAKER` + `<none>:0`; `UNKNOWN UNKNOWN`; waiter; nested indented waiter; `OFDLCK` -1; negative remote pid); `TestDecodeLinuxDev_RD_AC002_HawaiiDevIs259_2`; Linux `TestDpkgLock_RD_AC002_RealProcLocksSeesFcntlLockOnTempFile` (calls `requireInitPidNamespace`) |
| AC-003 | AD-001 targets | `TestProcLocks_RD_AC003_LegacyLockAloneIsHeld`; `TestDpkgProbe_RD_AC003_FrontendAbsentLegacyPresentStillChecked`; `TestRebootClearance_RD_AC003_LegacyHeldWaitsLikeFrontend` |
| AC-004 | AD-002 | `TestRebootClearance_RD_AC004_ReleaseAfter3mRebootsWithin10s`; `…_PollIntervalAndProbeTimeoutFitBound` (assumes the 5 s guard cap of `maintenance.go:38`; accepted) |
| AC-005 | AD-002 timeout | `TestRebootClearance_RD_AC005_TimeoutSkipsAndNeverForces`; `…_NoSecondWaitAfterSkip`; `…_ProbeAtExactBoundHeldSkips`; `…_ProbeAtExactBoundFreeProceeds` |
| AC-006 | AD-003 (b); `holderText` | `TestHandleReboot_RD_AC006_TimeoutResultFailedAndNamesHolder`; `TestDpkgProbe_RD_AC006_HolderNamedFromCommWithUid`; `TestDpkgProbe_RD_AC006_CommPathFromParsedPidOnly` |
| AC-007 | `holderText` | `TestDpkgProbe_RD_AC007_PidMinusOneHolderNotVisible`; `…_NegativeRemotePidHolderNotVisible`; `…_CommUnreadableSaysHolderNotVisibleWithPid`; `…_StatusUnreadableOmitsUid`; `TestHandleReboot_RD_AC007_TimeoutReportSaysHolderNotVisible` |
| AC-008 | AD-005 | `TestPostPatchReboot_RD_AC008_AuthorizedTimeoutOneEventOneTelemetry`; `…_UnauthorizedTimeoutOneEventOneTelemetry` |
| AC-009 | goroutine `defer Unlock` | `TestPostPatchReboot_RD_AC009_SkipReleasesHostReservation` |
| AC-010 | AD-001 `Unknown` cases; AD-002 | `TestDpkgProbe_RD_AC010_StatErrorOtherThanNotExistIsUnknown`; `…_ProcLocksMissingIsUnknown`; `…_ProcLocksPermissionDeniedIsUnknown`; `TestProcLocks_RD_AC010_MalformedLineIsUnknown` (table, **including a blank line and a whitespace-only line**); `…_NestedPidNamespaceIsUnknown` (`NSpid` two fields with an init link → `Unknown`); `…_NSpidMissingIsUnknown`; `…_SelfStatusUnreadableIsUnknown`; `…_DpkgStatusPresentLockFilesMissingIsUnknown`; `…_DpkgBinaryPresentVarLibHiddenIsUnknown`; `…_MarkerStatErrorIsUnknown`; `TestRebootClearance_RD_AC010_HeldThenAbsentNeverRebootsOnAllThreePaths`; `…_UnknownSkipsImmediatelyWithoutSleepOnAllThreePaths`; `…_UnknownMidWaitSkipsAtOnce`; `TestRebootClearance_RD_AC010_UnrecognizedStateFailsClosed` (state value 99 → `Unknown` skip, no reboot) |
| AC-011 | AD-001 caps | `TestDpkgProbe_RD_AC011_OverOneMiBIsUnknown`; `…_ExactlyOneMiBParses`; `…_ReaderStopsAtCap` (**≤ 1 MiB + 1 byte served**: stricter than revision 2, and it proves the `io.LimitReader`); `…_LineOver4KiBIsUnknown`; `…_SlowReadOverOneSecondIsUnknown`; `…_SlowHolderReadIsUnknown`; `TestHandleReboot_RD_AC011_OversizedLockInfoSkipsReboot` |
| AC-012 | AD-001 `Absent`; `dpkglock_other.go` | `TestDpkgProbe_RD_AC012_NoDpkgNoLockFilesIsAbsentWithoutReadingProc` (`Open` **and `Readlink`** are never called); `TestRebootClearance_RD_AC012_AbsentRebootsAsToday`; `TestPlatformProbe_RD_AC012_NonLinuxIsAbsent`; `TestStatFileID_RD_AC012_MissingPathIsNotExist` |
| AC-013 | AD-002 guards | `TestRebootClearance_RD_AC013_AllowRebootOffDuringWaitSkipsNamingGuard`; `…_MaintenanceStartsDuringWaitSkipsNamingGuard` |
| AC-014 | AD-002 | `TestRebootClearance_RD_AC014_FinalCheckRetakenKeepsWaitingWithinBound` |
| AC-015 | AD-002 | `TestRebootClearance_RD_AC015_ShutdownEndsWaitWithin5s`; `TestHandleReboot_RD_AC015_ShutdownResultReasonShutdown`; `TestPostPatchReboot_RD_AC015_ShutdownSkipLogsWarn` |
| AC-016 | AD-003; AD-007 | `TestHandleReboot_RD_AC016_NoRebootAfterSkippedResult` |
| AC-017 | AD-003 (a); AD-004 | `TestHandleReboot_RD_AC017_NineMinuteWaitReportsStartedAndWaited`; `TestCommandPolicy_RD_AC017_RebootBudgetCoversWait`; `TestSweep_RD_AC017_RebootNotTimedOutBefore20m`; the updated `TestCL_AC029_*` tables |
| AC-018 | AD-001 read-only | `TestDpkgProbe_RD_AC018_NeverOpensLockFiles` (`Open` sees only `/proc/self/status`, `/proc/locks` and `/proc/<pid>/{comm,status}`; `Readlink` sees only `/proc/self/ns/pid`); `TestDpkgLock_RD_AC018_NoLockingCallsInSource`; Linux `…_ProbeDoesNotDisturbHeldLock` |
| AC-019 | AD-006 | `TestRebootClearance_RD_AC019_FullWaitBoundedOutput` |
| AC-020 | AD-002; AD-004; AD-007 | `TestRebootAuth_PF_C8_ExecRebootHasExactlyThreeGuardedCallSites` (assertions kept); `TestRebootCallSites_RD_AC020_GateClearanceFeedsEveryExecReboot`; `…_ShutdownHasNoGate` (AST only; **never call `handleShutdown`**); `TestCommandPolicy_RD_AC020_ShutdownBudgetUnchanged` |
| AC-021 | AD-001 unprivileged | `TestDpkgProbe_RD_AC021_NoExecNoSudo`; Linux `TestDpkgLock_RD_AC021_RealProbeWorksUnprivileged` (skipped as root or outside the initial pid namespace); DoD diff check on `scripts/` |
| C-1 (namespace, revision 3) | AD-001 step 4 | `TestDpkgProbe_RD_C1_InitPidNamespaceLinkPasses` (`pid:[4026531836]` + one `NSpid` field → proceeds to `/proc/locks`); **`TestDpkgProbe_RD_C1_NonInitPidNamespaceLinkIsUnknown`** (fixture `pid:[4026532481]` with a **single-field** `NSpid` and a `/proc/locks` with no match → `Unknown` `agent is not in the host pid namespace`, **not `Free`**; `/proc/locks` is not opened: this is the case revision 2 missed); `…_PidNamespaceLinkUnreadableIsUnknown` (`Readlink` errors: `ENOENT`, `EACCES` → `cannot verify the pid namespace`); `…_MalformedPidNamespaceLinkIsUnknown` (table: `""`, `pid:[]`, `pid:[abc]`, `net:[4026531836]`, `pid:[4026531836]x`; the value is not echoed in the cause); `…_InitLinkButNestedNSpidIsUnknown` (the secondary check still applies); `TestParseNSpid_RD_C1_CountsNamespaceLevels`; `TestReadSmallFile_RD_C1_CutNSpidLineIsNeverTrusted` (accepted deviation 4); `TestReadSmallFile_RD_C1_NoCompleteLineReturnsEmpty`; `TestDpkgLockState_RD_C1_ZeroValueIsUnknown` (`dpkgLockStatus{}.State == dpkgLockUnknown`, and the gate skips on a zero status); Linux `TestDpkgLock_RD_C1_RealSelfNsPidIsInit` and `…_RealSelfStatusIsInitialNamespace` (both skipped, with a logged reason, outside the initial namespace) |
| C-1 (unit) | AD-001 step 4 | `TestAgentUnit_RD_C1_NoPrivatePIDs` (`scripts/riot-agent.service` and `installerUnit(t)`). After revision 3 a `PrivatePIDs=` unit would fail closed (every reboot skips), not open, so the test is kept as an availability guard |
| C-2 | AD-001 holder | `TestDpkgProbe_RD_C2_HolderTextCarriesUidAndInodeOnly`; `…_NonRootHolderUidReported`; `TestParseStatusUID_RD_C2_ParsesRealUidOnly` |
| C-3 | AD-007 | `TestExecReboot_RD_C3_NonClearedPermitStartsNoReboot`; `…_ClearedPermitStartsExactlyOnce`; `…_RefusalRunsNothing`; `TestRebootCallSites_RD_C3_MethodValueReferenceIsFlagged`; `…_PermitMintedOnlyByGate` |
| C-5 | AD-001 | `TestSanitizeHolderName_RD_C5_CharsetAndLength`: `a.b_c+d:e-f` survives unchanged (**`_` is kept**); `[ok](http://a.b)` → `?ok??http:??a.b?`; control bytes, spaces, `/`, `*`, `[`, `]`, `(`, `)` → `?`; at most 16 bytes. Plus the C-5 rows under AC-002, AC-006 and AC-011 |

## 9. Error Handling

| Failure | Behavior | Log |
|---|---|---|
| Not a dpkg host | `Absent`; reboot as today | none |
| dpkg present, lock files missing or hidden | `Unknown` → immediate skip | path outcome line |
| `self/ns/pid` not the initial namespace, unreadable or malformed | `Unknown` → immediate skip | path outcome line |
| `NSpid` nested, missing or cut | `Unknown` → immediate skip | path outcome line |
| `stat` error other than not-exist | `Unknown` → immediate skip | path outcome line |
| `/proc/locks` missing, unreadable, over 1 MiB, line over 4 KiB, blank or malformed line, or over 1 s | `Unknown` → immediate skip | path outcome line |
| `Held` then `Absent` | `Unknown` → immediate skip | `wait ended` Info + outcome line |
| State value not recognized (incl. the zero value) | `Unknown` → immediate skip | outcome line |
| `comm` / `status` unreadable | `Held` stays; text omits the name or the uid | none |
| Held past 10m | skip (b) / timeout event | `wait ended` Info + outcome line |
| Guard fails after the wait | skip (e) / guard event | `wait ended` Info (if waited) + outcome line |
| Agent stopping | skip (d) / best-effort event | outcome line |
| `cr.ctx` deadline | skip (e) `agent command time budget exhausted` | outcome line |
| `execReboot` without a valid permit | `errRebootNotCleared`; nothing executed | that path's existing error handling |

## 10. Security Considerations

- **No privilege added (NFR-001).** The probe uses only `stat`, `readlink` on the agent's own namespace link, and
  world-readable `/proc` reads. It runs no exec and no sudo.
- **FR-007.** The lock files are never opened.
- **Untrusted text (C-5).**
  - Holder names: `[A-Za-z0-9._+:-]`, ≤ 16 bytes.
  - The uid is an integer.
  - Causes are sanitized and ≤ 200 bytes.
  - Neither `/proc/locks` content nor the namespace link value is ever echoed.
  - `/proc/<pid>` paths come only from a parsed integer.
- **Fail closed (BR-001, C-1).** Every error, every "empty answer" and the zero state map to `Unknown`.
- **Gate enforcement (C-3).** A compile-time permit type, a runtime single-use check, and a per-reference AST test.
- **No reboot loop (BR-002).**
- **Residual risks:**
  - **R-1 (NFR-004; INFO-3):** the final-probe-to-shutdown window is **sub-second to a few seconds**. Accepted.
  - **R-2 (pid namespaces; revision 3):**
    - The kernel omits locks whose holder is invisible in the reader's pid namespace.
    - The probe refuses to answer unless the agent's own `/proc/self/ns/pid` is the initial namespace
      (`pid:[4026531836]`, `PROC_PID_INIT_INO`) **and** `NSpid` shows one level, both read from the same `/proc` as
      `/proc/locks`.
    - `NSpid` alone was insufficient: its field count is relative to the `/proc` mount, so a private namespace
      with its own `/proc` shows one field.
    - A containerised agent, or one under `PrivatePIDs=yes`, therefore fails **closed**: every reboot on a dpkg host
      skips visibly.
    - `TestAgentUnit_RD_C1_NoPrivatePIDs` guards the shipped units for availability.
    - The check relies on `PROC_PID_INIT_INO` staying fixed, as it has since Linux 3.8. If a future kernel changed
      it, the result would be `Unknown` everywhere, which fails safe and is visible.
  - **R-3 (local-user veto by inode collision, major-0 filesystems):** as revision 2. **Unverified on real hardware
    (C-6).**
  - **R-4 (local-user veto by lock volume, any filesystem):** as revision 2. **Ryan accepts it. The README documents
    it.**
  - **R-5 (version skew via the automatic agent updater):** as revision 2. Upgrade the server first.
  - **R-6:** the unauthorized path's "reboot initiated" text (OQ-004 pattern).
  - **R-7:** the post-patch shutdown-skip log and event are best-effort.
  - **R-8 (INFO-4):** a waiting reboot cannot be cancelled, and the host key is busy for about 10.5 min.
  - **R-9:** a non-Debian host with the `dpkg` binary installed but no lock files skips.

## 11. Performance Considerations

- **Common case:** ≤ 4 `stat` calls, ≤ 2 `readlink` calls, ≤ 2 small `self/status` reads, ≤ 2 `/proc/locks` reads
  (usually one `read()`, under 1 ms), and one guard evaluation.
- **During a wait:** ≤ 303 probes in O(1) memory.
- **Server:** no change.
- **No caching, rate limiting or indexing is required.**

## 12. Implementation Notes for Engineers

1. **Commits:** `[REBOOT-DPKG-001] <type>: <what>`. Order:
   1. models
   2. **probe and gate together**, accepted in revision 3: the AC-018 AST test scans both files
   3. wiring of the three paths, `execReboot(p)` and the harnesses
   4. the call-site test rewrite
   5. server tests

   The revision 3 delta (§4) is one `fix` commit plus one `test` commit.
2. **C-8.** Keep each caller's literal `AllowReboot` check before the gate. Rewrite only the helper of the test.
3. **Call shape** (checked by AST): `c := a.awaitRebootClearance(...)`, then `if !c.Cleared { … return }`, then
   `a.execReboot(c.Permit)`. Never build a `rebootPermit` outside `mintRebootPermit`.
4. **Clocks and sleeps:** only `a.clock().Now()` and `a.sleep` in the gate, and only `env.Now` in the probe.
5. **Test determinism on Linux CI.** The harnesses set `dpkgLockProbe`. Real-`/proc` tests call
   `requireInitPidNamespace(t)`, so a Docker run skips them with a reason instead of failing. The GitHub runner VM is
   in the initial namespace, so they run there.
6. **Existing pause-based tests:** the free path never sleeps.
7. **`handleReboot`** returns `commandExecResult`. Never use `ReasonTimeout`.
8. **Reader cap:** `io.LimitReader(f, 1<<20+1)` under a 64 KiB `bufio.Reader`, with a 4 KiB scanner token.
9. **No new dependency.** `os.Readlink` is in the standard library.
10. **Never call `handleShutdown` in tests.**
11. **Host verification (C-6),** in the implementation report:
    1. **Hawaii, as `riot`:**
       1. the probe returns `Free` at rest;
       2. **`readlink /proc/self/ns/pid` = `pid:[4026531836]`**;
       3. `NSpid` has one field;
       4. a held `lock-frontend` returns `Held` with comm, pid and `uid 0`;
       5. the `stat` dev and inode match the `/proc/locks` line.
    2. **Major-0 sample, or the statement "unverified on real hardware".** The current report states it is
       unverified, which is accepted.
    3. **A standalone reboot during a held lock:** only Ryan triggers it.

## 13. Definition of Done

- [ ] Every Section 4 change, including the revision 3 delta, implemented; every "explicitly unchanged" path
      untouched
- [ ] Every Section 8 test exists with `RD_ACnnn` / `RD_Cn` names and `[AC-nnn]` / `[C-n]` messages, and passes
- [ ] `go test ./...` (Windows) and `go test -race ./...` (Linux CI) green; `cd web && npm run test:run` green
- [ ] `go vet ./...` clean; `make build-agent-all` succeeds for every target
- [ ] No migration
- [ ] The C-8 test's assertions are unchanged; its helper records one entry per `execReboot` reference
- [ ] `grep -rn "rebootPermit{" internal/agent --include=*.go` (non-test) matches only inside `mintRebootPermit`
- [ ] `grep -rn "time.Now()\|time.Sleep" internal/agent/dpkglock*.go internal/agent/reboot_clearance.go` (non-test)
      returns nothing
- [ ] `grep -n "4026531836" internal/agent/dpkglock.go` shows exactly one definition (`initPidNamespaceLink`)
- [ ] `AgentHardMax("shutdown",1) == time.Minute`, `ExecDeadline("shutdown",1) == 10*time.Minute`, and `QueuedTTL`
      is unchanged
- [ ] C-6 host verification, including the `self/ns/pid` value, recorded in the implementation report
- [ ] QA gate C-7: AC-010, AC-011, AC-012, AC-014, AC-015, AC-016, AC-018, AC-020, the C-8 test and the C-1
      (including revision 3) and C-3 tests pass
