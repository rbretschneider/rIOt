# Architecture Decision Document

- **Story ID:** REBOOT-DPKG-001
- **FRD Reference:** `docs/requirements/REBOOT-DPKG-001-frd.md`
- **Origin:** PATCH-FLOW-001 security review INFO-4
- **Amends:** CMD-LIFECYCLE-001 AD-005 / AD-013 power-class budgets, for `reboot` only (AD-004 below)
- **Author:** Architect Agent
- **Date:** 2026-10-24
- **Status:** FINAL (pending security review, FRD D-007)

**Product decisions adopted.** OQ-001 to OQ-005 take the FRD defaults as decided:
- a fixed 10-minute bound;
- an unknown lock state skips immediately;
- the standalone reboot uses the full bound, with the budget reconciled by the architect (AD-004);
- the phase A wording is unchanged;
- skipped reboots are never retried.

**FRD review (Stage 1).** No blocking questions. Every AC can be fulfilled as written. One related observation is
recorded as residual R-6: the unauthorized path's result says "reboot initiated" before phase B, the same pattern as
OQ-004.

---

## 1. Summary

A new agent-side probe finds out whether the dpkg lock is held, using only `stat()` on the two lock files and a
bounded read of `/proc/locks`. Lines are matched on device and inode, and the holder is named from
`/proc/<pid>/comm`. The probe needs no privilege, no exec and no new dependency. One shared gate,
`awaitRebootClearance`, sits immediately before `execReboot` in all three reboot paths. It runs four steps in order:
a bounded wait, the path's guards, a final lock check, and then a return to the caller, which calls `execReboot` at
once. To fit the 10-minute wait, the `reboot` action, and only `reboot`, gets its own budget: an agent hard max of
12 min and a server execution deadline of 20 min. `shutdown` keeps 1 min and 10 min, and both keep the 15-minute
queued TTL. The story adds no migration, no persistent state, no sudoers or `install.sh` change, and no UI change.

## 2. Technical Context

Verified on `feat/reboot-dpkg-001` (base `0e0579b`):

- **Reboot call sites.** `execReboot()` (`commands.go:324`) runs `sudo systemctl reboot` on Linux and
  `shutdown /r /t 5` on Windows. When the test `runner` is set, it goes through `runner`. It has exactly three
  callers, and C-8 (`reboot_callsites_test.go`) enforces this with an AST check: each enclosing function's body text
  before the call must contain `Commands.AllowReboot`.
  1. `handleReboot(payload) (string, string)` runs synchronously inside the command. `cr.ctx` has the timeout
     `AgentHardMax("reboot") = 1m` (`newCmdRun`, `command_lifecycle.go:355`). `executeAction` has already applied
     the maintenance-blocker precheck and the host lock.
  2. `maybeRebootAfterPatch(r)` is the unauthorized PATCH-GATE path, which maintenance-window auto-patch also uses.
     Before the goroutine starts it checks `AllowReboot`, then `maintenanceBlockerFor`, then takes the
     `TryLockAll(host, "patch-reboot:<id>")` reservation and calls `claimPatchReboot`. The goroutine then:
     1. calls `a.sleep(commandContext, rebootDelay)`;
     2. re-checks `shuttingDown` and `maintenanceBlockerFor`, and on failure **only triggers telemetry, with no
        event**;
     3. calls `execReboot`.

     The reservation is released by the goroutine's `defer Unlock`.
  3. `rebootAfterDelivery(id)` is the authorized phase B, running in a goroutine that holds the same reservation.
     It runs `awaitResultDelivery` (at most 30 s), then checks `shuttingDown`, `maintenanceBlockerFor` and
     `AllowReboot`, then calls `execReboot`. Every failure goes through `abortAuthorizedReboot(id, reason)`, which
     emits one Warn log, one warning agent event and one `triggerTelemetry()`.
- **Shutdown semantics.** On SIGTERM, `Run` sets `shuttingDown`, cancels the loop context, and calls
  `waitForCommands(AgentShutdownGrace = 20s)`. `cmdCtx` (`commandContext()`) is cancelled **only after** that grace
  period. So a wait that watches only `ctx` would hold the stop for up to 20 s. The wait must poll `shuttingDown`.
  Post-patch goroutines are not counted in `cmdWG`.
- **Seams.**
  - `a.sleep(ctx, d)` goes through `a.pause` (tests) or a timer.
  - `a.clock()` returns the injected `clock`, and `fakeClock.Advance` exists.
  - `a.runner` captures `execReboot`.
  - `lifecycleHarness` (`command_lifecycle_test.go`) and `newOSUpdateTestAgent` (`commands_test.go`) build every
    agent that reaches a reboot path in tests.
- **Budgets** (`models/command_policy.go`):
  - `ClassPower` (`reboot` and `shutdown`) has `QueuedTTL` 15m, `ExecDeadline` 10m and `AgentHardMax` 1m.
  - `TestCL_DeadlineInvariant` requires `ExecDeadline > AgentHardMax + DeadlineSlack (5m)` for both actions.
- **Server consumers of those budgets.**
  - `service.go:224` stores `ExecDeadlineSeconds(action)` in `commands.exec_deadline_s` at creation.
  - `command_repo.go` stamps `deadline_at = dispatched_at + exec_deadline_s` at dispatch.
  - `sweeper.go` times out rows past `deadline_at` and reads the per-row `exec_deadline_s` for its message.
  - `expiredMessage` reads `QueuedTTL` only as a fallback.
  - No server code reads `AgentHardMax`. The web reads `exec_deadline_s` from the row and has no fixed timer.
- **Tests that pin the numbers.**
  - `models/command_policy_test.go`: `TestCL_AC029_ExecDeadlineTable` (reboot 10m), `TestCL_AC029_ExpiresAtPerActionClass`
    and `TestCL_AC030_*` (reboot TTL 15m, unchanged), and `TestCL_DeadlineInvariant`.
  - `server/commands/service_test.go:210`: `{"reboot", nil, 15m, 600}`.
- **Platform split pattern.** `maintenance_linux.go` / `maintenance_other.go` (`//go:build !linux`), and the same
  for `docker_read_*` and `update_preflight_*`.
- **Dependencies.** `golang.org/x/sys` is only `// indirect`. This design does not import it (AD-001 decodes
  `st_dev` in pure Go).

**Missing:** any dpkg lock awareness on the reboot paths, a reboot budget that fits a 10-minute wait, and a skip event
on the unauthorized path's post-delay guard failures.

## 3. Architecture Decisions

### AD-001: Lock detection through `stat()` + `/proc/locks` (FR-001, FR-002, FR-006, FR-007, FR-010, FR-018, NFR-001, NFR-003)

**Decision.** A pure-Go probe in `internal/agent/dpkglock.go` has no build tag, so it compiles and is tested on every
platform:

```go
type fileID struct{ Major, Minor uint32; Ino uint64 }

type dpkgLockState int // dpkgLockAbsent | dpkgLockFree | dpkgLockHeld | dpkgLockUnknown

type dpkgLockStatus struct {
    State dpkgLockState
    PID   int64  // holder pid as printed in /proc/locks; <= 0 = not visible
    Comm  string // sanitized /proc/<pid>/comm; "" = not visible
    Cause string // set only for dpkgLockUnknown, <= 200 bytes
}
func (s dpkgLockStatus) holderText() string // "held by unattended-upgr (pid 1234)" | "holder not visible" | "holder not visible (pid 1234)"

type dpkgProbeEnv struct {
    LockPaths []string                              // {"/var/lib/dpkg/lock-frontend", "/var/lib/dpkg/lock"}
    StatID    func(path string) (fileID, error)     // errors.Is(err, fs.ErrNotExist) = file absent
    Open      func(path string) (io.ReadCloser, error)
    ProcLocks string                                // "/proc/locks"
    ProcRoot  string                                // "/proc"
    Now       func() time.Time
}
func probeDpkgLock(env dpkgProbeEnv) dpkgLockStatus
func parseProcLocksLine(line string) (procLock, bool /*skip*/, error)
func lockMatches(l procLock, t fileID) bool
func decodeLinuxDev(dev uint64) (major, minor uint32) // glibc gnu_dev_major/minor encoding
```

Constants (agent-internal): `dpkgProbeMaxBytes = 1 << 20`, `dpkgProbeTimeout = 1 * time.Second`,
`dpkgCommMaxBytes = 64`, `dpkgCauseMaxBytes = 200`.

**Algorithm of `probeDpkgLock`.** One call is one "lock check".

1. `start := env.Now()`.
2. For each path in `LockPaths`, in order, call `StatID(path)`.
   - `fs.ErrNotExist` → skip the path.
   - Any other error → `Unknown`, cause `cannot stat <path>: <err>`.
   - Success → add the result to the targets.
3. No targets → `Absent`. `/proc/locks` is **not** read (FR-006: dnf and other hosts are unaffected by any
   `/proc/locks` restriction).
4. `Open(ProcLocks)`. **Any** error → `Unknown`, cause `cannot read /proc/locks: <err>`. This includes `ENOENT`,
   which is what a unit with `ProcSubset=pid` produces. A missing `/proc/locks` must never read as "free".
5. Stream the file through `bufio.Scanner` with a 4 KiB maximum line size, counting bytes.
   - More than `dpkgProbeMaxBytes` read → `Unknown`, cause `lock information exceeds 1 MiB`.
   - A line longer than 4 KiB, or any scanner error → `Unknown`, cause `cannot read /proc/locks: <err>`.
   - After each line, `env.Now().Sub(start) > dpkgProbeTimeout` → `Unknown`, cause `lock check exceeded 1s`.
   - The whole file is always read to EOF (within the cap), so the outcome does not depend on line order.
6. Parse each line with `parseProcLocksLine`:
   - `fields := strings.Fields(line)`. `fields[0]` must end in `:`. If `fields[1] == "->"`, the line is a blocked
     **waiter** and is skipped, because a waiter does not hold the lock.
   - The remaining fields need at least 7 entries: `TYPE MODE ACCESS PID DEV START END`. Type is any token
     (`POSIX`, `FLOCK`, `OFDLCK`, `LEASE`, `DELEG`); every type counts.
   - `PID` must parse as a signed decimal (`0` = not visible in our pid namespace; `-1` for OFD locks).
   - `DEV` is `MAJ:MIN:INO`. `MAJ` and `MIN` are hex, `INO` is unsigned decimal. The kernel token `<none>:0` means
     no inode: the line is skipped.
   - Anything else → `Unknown`, cause `unrecognized /proc/locks line <n>`. The line content is not echoed.
7. `lockMatches(l, t)` is true when `l.Ino == t.Ino` and either:
   - `l.Major == t.Major && l.Minor == t.Minor` (exact match), or
   - `t.Major == 0 && l.Major == 0` (**anonymous-device leniency**, see the rationale).
8. The first matching non-waiter line in file order is the holder → `Held`, with `PID` from the line. If `PID > 0`,
   read `<ProcRoot>/<pid>/comm` through `Open`, capped at 64 bytes.
   - Trim trailing whitespace. Map every byte outside `0x20–0x7e` to `?`. Keep at most 16 bytes.
   - A read error leaves `Comm = ""`. Holder visibility is not lock state, so this is never `Unknown`.
   - The comm read counts toward the 1 s deadline. If the deadline is exceeded, the result is `Unknown`.
9. No match → `Free`.

Causes are `models.TruncateHead(models.SanitizeText(cause), 200)`.

**Platform split.**

- `dpkglock_linux.go`:
  - `statFileID(path)`: `os.Stat` (follows symlinks), then `fi.Sys().(*syscall.Stat_t)`, then
    `decodeLinuxDev(uint64(st.Dev))` and `uint64(st.Ino)`. A missing `Stat_t` is an error.
  - `platformDpkgProbe(now)`: calls `probeDpkgLock` with the real paths, `os.Open`, `"/proc/locks"` and `"/proc"`.
- `dpkglock_other.go` (`//go:build !linux`): `platformDpkgProbe(now)` returns `dpkgLockStatus{State: dpkgLockAbsent}`.
  On Windows, reboot behavior is byte-identical (FR-006).
- `decodeLinuxDev`:
  - `major = ((dev >> 8) & 0xfff) | ((dev >> 32) &^ 0xfff)`
  - `minor = (dev & 0xff) | ((dev >> 12) &^ 0xff)`

  Hawaii's verified `66306` decodes to `259:2` (= `103:02` hex). That value is a fixture.

**Agent seam.** `Agent.dpkgLockProbe func() dpkgLockStatus`. When it is nil, `a.probeDpkg()` calls
`platformDpkgProbe(a.clock().Now)`.

**Rationale.**
- The agent user cannot open the 0640 `root:root` lock files, so `F_GETLK` is impossible without a new sudo rule.
  `stat()` and `/proc/locks` (0444) both work unprivileged; this was verified on Hawaii.
- The probe never opens the lock files, so it cannot acquire a lock or contend with dpkg (FR-007). Reading
  `/proc/locks` takes the kernel's lock-list lock only for the duration of each `seq_file` chunk, which is the same
  thing `lslocks` does.
- **Why the anonymous-device leniency.** `/proc/locks` prints `inode->i_sb->s_dev`. On btrfs, `stat()` returns the
  subvolume's anonymous `st_dev` instead, so an exact device match never fires and the check would fail open. Both
  values are anonymous devices (major 0). On every block-device filesystem (ext4, xfs: majors 8, 253, 259, …) the
  match stays exact. So the leniency only applies where the device number is unreliable, and there it fails closed
  (BR-001). Residual R-3 covers the cost.
- Strict parsing fails closed when the kernel format is unknown (FR-010). The known variants (waiter, `<none>`, every
  lock type, pid 0 and -1) are accepted explicitly.

**Alternatives considered.**
- *`F_GETLK` via a sudo helper or a new `riot-maintenance` query.* Rejected: a new sudoers rule or protocol change
  needs an `install.sh` re-run on every device (NFR-001, AGENT-PRIV).
- *`fuser`/`lsof` exec, or scanning `/proc/*/fd`.* Rejected: root processes' fd tables cannot be read as `riot`, and
  both spawn processes.
- *Process-name heuristics (`apt`, `dpkg`, `unattended-upgr` in `/proc/*/comm`).* Rejected:
  `unattended-upgrade-shutdown --wait-for-signal` runs permanently (a false "held"), and other tools are missed.
- *`systemctl reboot --check-inhibitors=yes`.* Rejected: it changes the sudoers-pinned argv (install.sh re-run), it
  needs systemd 248 or later, and apt holds no logind inhibitor anyway.
- *Matching on device and inode only, with no leniency.* Rejected: it fails open on btrfs.
- *Matching on inode only, everywhere.* Rejected: a coincidental inode on another block-device filesystem would
  suppress reboots permanently on common ext4 hosts.

**Consequences.** One cheap probe per poll. The probe needs `/proc/locks` to be readable. Hosts where it is not get
a visible "lock state unknown" skip (R-4).

---

### AD-002: One shared gate immediately before `execReboot` (FR-003–FR-005, FR-008, FR-009, FR-011–FR-013, FR-021)

**Decision.** New file `internal/agent/reboot_clearance.go`:

```go
// rebootGuards returns "" when every guard of the path passes, else the failed guard's text.
type rebootGuards func(ctx context.Context) string

type rebootClearance struct {
    Cleared bool
    Waited  time.Duration // first "held" observation to clearance or skip; 0 when never held
    Skip    string        // "" | skipLockTimeout | skipLockUnknown | skipShutdown | skipGuard
    Detail  string        // holder text (timeout), cause (unknown), guard text (guard)
}
func (c rebootClearance) reason() string // shared reason text, see AD-005

// awaitRebootClearance runs the wait, the guards and the final lock check for one reboot attempt.
// When it returns Cleared, the caller must call execReboot immediately, with no other check in between.
func (a *Agent) awaitRebootClearance(ctx context.Context, path, commandID string, guards rebootGuards) rebootClearance

// guards shared by all three paths, in this order
func (a *Agent) commonRebootGuards(ownID string) rebootGuards
```

Constants: `dpkgLockPoll = 2 * time.Second`. The wait bound is `models.DpkgLockWaitBound` (10 min), shared with the
budget (AD-004).

**`commonRebootGuards(ownID)`** returns the first failure, in this order:
1. `a.shuttingDown.Load()` → `agent shutting down` (reported as `skipShutdown`).
2. `!a.config.Commands.AllowReboot` → `commands.allow_reboot is off`.
3. `a.maintenanceBlockerFor(ctx, ownID)` non-empty → `host busy: <blocker>`. `ownID` is `""` for the standalone path,
   which keeps today's strict precheck, and the command ID for the post-patch paths (C-3).

These are exactly the guards each path applies today (FR-011). The standalone path's intake shutdown refusal and its
`executeAction` maintenance precheck are the same checks.

**Loop (pseudocode):**

```
var firstHeld time.Time; waiting := false; guardsPassed := false
for {
    if a.shuttingDown.Load()                  -> return skip(skipShutdown)
    st := a.probeDpkg()
    switch st.State {
    case Unknown:                             -> return skip(skipLockUnknown, st.Cause)          // FR-010, no wait
    case Held:
        guardsPassed = false                  // FR-012: guards run again after any further wait
        now := a.clock().Now()
        if !waiting { waiting, firstHeld = true, now; Info "dpkg lock held, waiting before reboot" }
        elapsed := now.Sub(firstHeld)
        if elapsed >= DpkgLockWaitBound       -> return skip(skipLockTimeout, st.holderText())   // FR-008
        if !a.sleep(ctx, min(dpkgLockPoll, DpkgLockWaitBound-elapsed)) {
            -> return skip(skipShutdown) if a.shuttingDown.Load() else skip(skipGuard, "agent command time budget exhausted")
        }
    case Free, Absent:
        if !guardsPassed {
            if g := guards(ctx); g != ""      -> return skip(skipGuard or skipShutdown, g)        // FR-011
            guardsPassed = true
            continue                          // FR-012: the final check is a fresh lock probe
        }
        -> return cleared(Waited: waiting ? a.clock().Now().Sub(firstHeld) : 0)
    }
}
```

When the gate returns after waiting at least once, it writes exactly one `Info "dpkg lock wait ended"` line with the
`path`, `command_id`, `outcome`, `waited` and `holder` attributes, whatever the outcome.

**Properties.**
- **Lock free at the first check (FR-005):** probe → guards → probe → cleared. There are no sleeps and no wait logs.
- **No lock files (FR-006):** the same sequence, with `Absent` both times. On Windows, `platformDpkgProbe` always
  returns `Absent`.
- **One wait per attempt (FR-009, FR-012):** one deadline, `firstHeld + 10m`. A re-taken lock continues the same wait.
  The function returns exactly once, and no caller loops.
- **The final check is the last check (FR-012):** after `Cleared`, nothing runs between the gate's return and
  `execReboot` except the call itself.
- **Release to reboot (FR-004):** at most one poll (2 s), plus guards (`maintenanceBlockerFor`'s `systemctl show` is
  capped at 5 s), plus two probes (≤ 1 s each), which is ≤ 9 s.
- **Shutdown (FR-013):** `shuttingDown` is read before every probe, and every sleep is at most 2 s. So the wait ends at
  most 2 s (sleep) + 1 s (probe) = 3 s after SIGTERM. The wait does not depend on `cmdCtx`, which is cancelled only
  after the 20 s grace period.
- **Total wait (FR-003, FR-012):** probe starts never come later than `firstHeld + 10m`. The worst wall time for one
  attempt is 10m + 1 s (last probe) + 5 s (guards) + 1 s (final probe), about 10m07s.

**Placement in each path.** The C-8 call sites are unchanged, and `execReboot` stays a raw executor.

| Path | Before the gate (unchanged, fail-fast) | Gate call | After a skip | After clearance |
|---|---|---|---|---|
| `handleReboot(cr)` | `if !a.config.Commands.AllowReboot` → existing error result | `awaitRebootClearance(cr.ctx, "reboot", cr.id, a.commonRebootGuards(""))` | AD-003 result (b)–(e) | `execReboot()` → (a) |
| `maybeRebootAfterPatch` goroutine | the existing `AllowReboot`, `maintenanceBlockerFor`, reservation and claim (before the goroutine); `sleep(rebootDelay)`; then the existing `shuttingDown`/`maintenanceBlockerFor` re-check, which now reports through `abortPostPatchReboot` (AD-005) | `awaitRebootClearance(commandContext, "post_patch", id, a.commonRebootGuards(id))` | `abortPostPatchReboot(id, c.reason())` | `execReboot()` |
| `rebootAfterDelivery` | the existing `awaitResultDelivery`, `shuttingDown`, `maintenanceBlockerFor`, `AllowReboot` (all kept, all `abortAuthorizedReboot`) | `awaitRebootClearance(commandContext, "authorized_post_patch", id, a.commonRebootGuards(id))` | `abortAuthorizedReboot(id, c.reason())` | Info log (adds a `waited` attr), `execReboot()` |

Each caller keeps its literal `Commands.AllowReboot` check before the gate. So the C-8 test passes unchanged
(FR-021). A new structural test asserts that the gate call comes before `execReboot` in all three functions (AC-020).

**Rationale.**
- One function gives one bound, one log policy and one test surface for three paths.
- The fail-fast pre-gate checks keep the paths from waiting 10 minutes only to fail a guard that was already failing.
- Guards run only when the lock is free, never on every poll, which keeps `systemctl show` spawns rare (§11).

**Alternatives considered.**
- *Putting the wait inside `execReboot`.* Rejected: `execReboot` must stay a raw executor, and it cannot report
  path-specific outcomes.
- *A context cancelled at SIGTERM, instead of polling `shuttingDown`.* Rejected for this story: it adds a second
  agent-wide context for one consumer. A 2 s poll meets FR-013 with margin.
- *Adding the post-patch goroutines to `cmdWG`.* Rejected: `awaitResultDelivery` does not watch `shuttingDown`, so it
  could stretch the agent's stop by up to 20 s.
- *Per-check Debug logs.* Rejected: NFR-002 says nothing may grow with the wait, so no per-check log is written at
  any level.

---

### AD-003: Standalone `reboot` result contract (FR-014, FR-015, FR-016)

**Decision.** `handleReboot` becomes `func (a *Agent) handleReboot(cr *cmdRun) commandExecResult`, and
`executeAction`'s `case "reboot":` returns it directly. The outcomes are exact strings. `<D>` is
`models.FormatDurationShort(c.Waited)`.

| FR-014 | Status | Reason | Message |
|---|---|---|---|
| (a) no wait | `success` | `""` | `reboot initiated` (unchanged, AC-012) |
| (a) after a wait | `success` | `""` | `reboot initiated after waiting <D> for the dpkg lock` |
| (b) timeout | `error` | `""` | `reboot skipped: dpkg lock held past the timeout (10m), <holderText>` |
| (c) unknown | `error` | `""` | `reboot skipped: dpkg lock state unknown: <cause>` |
| (d) shutdown | `error` | `shutdown` | `reboot skipped: agent shutting down` |
| (e) guard | `error` | `conflict` for `host busy: …`, otherwise `""` | `reboot skipped: guard failed: <guard text>` |
| `execReboot` error | `error` | `""` | `reboot: <err>` (unchanged) |
| pre-gate `allow_reboot` false | `error` | `""` | unchanged text |

`ReasonTimeout` is deliberately **not** used for (b). FR-016 forbids recording the command as timed out, and the
closed `reason` enum renders `timeout` as a time-budget failure.

**Truthfulness (FR-015).** The gate and `execReboot` run synchronously inside `executeAction`. The result is built
only after `execReboot` has returned (a) or after the gate has returned a skip. Nothing holds the command ID after
the result is built, so no later code path can reboot for that command. The seen-ID store already blocks
re-execution on redelivery. The `cr.ctx` deadline (12 min, AD-004) comes after the worst-case gate time (about
10m07s). If it fired anyway, the outcome would be (e) `agent command time budget exhausted`, which is truthful and is
not a reboot.

**Rationale.** The result already reaches every surface the FRD names, and the server stores `message` as is. No
wire change is made (NFR-005).

**Alternatives considered.** *A progress step `dpkg_lock_wait` while waiting.* Rejected: the FRD rules out any UI
change, and the step would stay as the result's `step`. It is a candidate follow-up.

---

### AD-004: Reboot budget split from shutdown (FR-016, FR-020, OQ-003)

**Decision.** `reboot` gets its own agent and server budget inside `ClassPower`. `shutdown` is unchanged. The new
constants live in `internal/models/command_policy.go`:

```go
// DpkgLockWaitBound is the longest a reboot waits for the dpkg lock (REBOOT-DPKG-001 BR-004).
DpkgLockWaitBound = 10 * time.Minute
// RebootAgentHardMax = wait bound + 2m for guards, the final check and execReboot.
RebootAgentHardMax = DpkgLockWaitBound + 2*time.Minute // 12m
// RebootExecDeadline must exceed RebootAgentHardMax + DeadlineSlack (17m).
RebootExecDeadline = 20 * time.Minute
```

| Budget | `reboot` before → after | `shutdown` | Where it is consumed |
|---|---|---|---|
| `ActionClass` | `ClassPower` (unchanged) | `ClassPower` | `QueuedTTL` and the class switches |
| `QueuedTTL` | 15m → **15m (unchanged)** | 15m | `ExpiresAt` at creation; the claim-time expiry check; the sweeper's `expireQueued` |
| `ExecDeadline` | 10m → **20m** (`exec_deadline_s` 600 → 1200) | 10m | `service.go:224` at creation; `deadline_at = dispatched_at + exec_deadline_s`; the sweeper's `timeOutOverdue` |
| `AgentHardMax` | 1m → **12m** | 1m | `newCmdRun`'s `cr.ctx` timeout |
| `DeadlineSlack` invariant | 20m > 12m + 5m ✓ | 10m > 1m + 5m ✓ | `TestCL_DeadlineInvariant` (unchanged test) |

Code shape: `AgentHardMax` and `ExecDeadline` each gain `if action == ActionReboot { return Reboot… }` at the top of
their `ClassPower` case. Their doc comments change to `power: reboot 12m / shutdown 1m` and
`power: reboot 20m / shutdown 10m`.

**Why the queued TTL is unchanged.** The TTL bounds how long an undelivered reboot may wait before it is dropped. The
dpkg wait happens after dispatch, so it is irrelevant here. Lengthening the TTL would only let a stale reboot fire
later.

**Sweeper.** No code change. The sweeper reads each row's own `exec_deadline_s` and `deadline_at`, so new reboot rows
time out at about 20–21 min (60 s tick), and the message reads `no result from agent within 20m`. Rows created before
the server upgrade keep 600 s. That only matters for a reboot that is in flight at the moment the server is upgraded.

**Outcome truthfulness by case (FR-016):**

| Outcome | Agent | Server row |
|---|---|---|
| Started, no wait | result (a) within ms | `succeeded`, long before 20m |
| Started after a 9m wait (AC-017) | result (a) at about 9m, `cr.ctx` 12m not reached | `succeeded` at about 9m, `deadline_at` +20m not reached |
| Timeout skip | result (b) at 10m plus ≤ 7 s | `failed` with the (b) message |
| Unknown | result (c) at once | `failed` |
| Guard | result (e) at once or after the wait | `failed` |
| Shutdown | result (d) within 3 s, durable in the outbox, delivered after the restart | `failed`, reason `shutdown` |

**Rationale.** This is the smallest change that keeps the command synchronous and truthful. It amends two table cells
of CMD-LIFECYCLE-001 and touches no schema or protocol.

**Alternatives considered.**
- *Raise all of `ClassPower`.* Rejected by FR-020: shutdown must not change.
- *A detached reboot (`Detached: true`, result later).* Rejected: it needs in-flight recovery semantics for reboot
  markers, which recovery currently discards. That is far larger than a budget change.
- *Report success at once and wait in the background, like the post-patch paths.* Rejected: it violates FR-015.
- *Cap the standalone wait under 1 minute.* Rejected: OQ-003 decided the full bound.
- *Let progress extend `deadline_at`.* Rejected: the CMD-LIFECYCLE security rule says progress never moves the
  deadline.

**Consequences.**
- A lost reboot result is detected after 20 min instead of 10.
- Version skew is residual R-5.
- `exec_deadline_s` for reboot is visible in the command JSON as `1200`.

---

### AD-005: Post-patch skip reporting and reservation release (FR-017, FR-018, FR-019)

**Decision.**

- **Authorized path.** It uses the existing `abortAuthorizedReboot(id, reason)`: one Warn log, one warning
  `EventCommandCompleted` agent event `authorized reboot for command <id> was not started: <reason>`, and one
  `triggerTelemetry()`.
- **Unauthorized path.** A new `abortPostPatchReboot(id, reason string)` in `reboot_clearance.go` mirrors it:
  - one `slog.Warn("os_update: post-patch reboot not started", "command_id", id, "reason", reason)`;
  - one `a.notifyEvent(... sendAgentEvent(ctx, models.EventCommandCompleted, models.SeverityWarning, "post-patch reboot for command <id> was not started: <reason>"))`;
  - one `a.triggerTelemetry()`.

  It is used for every non-reboot exit **inside** the goroutine: the existing post-delay
  `shuttingDown`/`maintenanceBlockerFor` re-check (today it only triggers telemetry; now it also emits the event,
  which FR-017 requires and NFR-005 permits) and every gate skip. Exits before the goroutine (no reboot-class applied,
  `allow_reboot` off, blocker, reservation conflict, claim failure) are unchanged.
- **Reason text** (`rebootClearance.reason()`), shared by both events and by the logs:
  - timeout: `dpkg lock held past the timeout (10m), <holderText>`
  - unknown: `dpkg lock state unknown: <cause>`
  - shutdown: `agent shutting down`
  - guard: the guard text (`commands.allow_reboot is off` | `host busy: <blocker>` | `agent command time budget exhausted`)

  The existing PATCH-FLOW-001 event wording (`commands.allow_reboot is off`, `host busy: …`) is kept, so its tests
  stay valid.
- **Reservation (FR-019).** Both goroutines already `defer a.locks.Unlock({host}, "patch-reboot:<id>")`. The gate runs
  inside them, so every skip returns and releases the reservation. No new code is needed, and AC-009 locks it with a
  test.
- **Shutdown exception.** The Warn line is always written. The event is queued best-effort: the notifier lives on
  `cmdCtx` until the grace period ends. If the process exits first, the line can be lost; see R-7.
- **Counts per attempt.** The gate returns once, and each path calls its abort function at most once. So there is at
  most one event and one telemetry trigger. `telemetryNow` has capacity 1 and coalesces.

---

### AD-006: Bounded growth (NFR-002, NFR-003, house rule)

| Item | Bound per reboot attempt |
|---|---|
| Wait | ≤ 1 per attempt; one deadline `firstHeld + 10m`, also when the lock is re-taken |
| Lock checks | ≤ 10m / 2s + 3 = **303** probes; each probe reads ≤ 1 MiB from `/proc/locks` (streamed, 4 KiB buffer, no 1 MiB allocation), plus ≤ 64 B of `comm`, and takes ≤ 1 s |
| Guard evaluations | only when the lock is observed free, so ≤ 1 per poll (≤ 301; normally 1); each spawns ≤ 1 `systemctl show` (5 s cap) |
| Log lines, Info or above, from the wait | ≤ 2 (`wait started`, `wait ended`), written only when a wait happened; no per-check log at any level |
| Path outcome lines | unchanged count: one existing line per path outcome (standalone `command: failed` Warn, or the abort Warn) |
| Events | standalone: the existing single failed-command event; post-patch: ≤ 1 warning event |
| Telemetry triggers | post-patch: ≤ 1 (coalescing channel) |
| Persistent state | **none**: no files, no columns, no migration. `/proc` is read-only |
| Memory | O(1): the scanner buffer and one `dpkgLockStatus` |

Nothing grows with the length of the wait apart from the probe count, which is capped by the bound. A skip never
retries (FR-009), so a host with a permanently held or unreadable lock produces one skip per operator or window
attempt and nothing else.

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `internal/agent/dpkglock.go` | `fileID`, `procLock`, `dpkgLockState`, `dpkgLockStatus` + `holderText`, `dpkgProbeEnv`, `probeDpkgLock`, `parseProcLocksLine`, `lockMatches`, `decodeLinuxDev`, the probe constants, `(a *Agent) probeDpkg()` (AD-001) |
| CREATE | `internal/agent/dpkglock_linux.go` | `statFileID` (`syscall.Stat_t`), `platformDpkgProbe` with the real paths |
| CREATE | `internal/agent/dpkglock_other.go` | `//go:build !linux`; `platformDpkgProbe` returns `Absent` |
| CREATE | `internal/agent/dpkglock_test.go` | Parser, matcher, probe and holder tests on every platform (fake `StatID`/`Open`/`Now`) |
| CREATE | `internal/agent/dpkglock_linux_test.go` | Real `/proc/locks` + `syscall.FcntlFlock` on a temp file; `statFileID` not-exist |
| CREATE | `internal/agent/dpkglock_other_test.go` | `//go:build !linux`; the platform probe is `Absent` (runs on Windows) |
| CREATE | `internal/agent/reboot_clearance.go` | `rebootGuards`, `rebootClearance` + `reason()`, `awaitRebootClearance`, `commonRebootGuards`, `abortPostPatchReboot`, skip constants, `dpkgLockPoll` (AD-002, AD-005) |
| CREATE | `internal/agent/reboot_clearance_test.go` | Gate and three-path AC tests (§8) |
| MODIFY | `internal/agent/commands.go` | `handleReboot(cr *cmdRun) commandExecResult` with the gate and the AD-003 results; `executeAction` `case "reboot": return a.handleReboot(cr)`; `maybeRebootAfterPatch` goroutine: post-delay re-check through `abortPostPatchReboot`, then gate, then `execReboot`; `execReboot` doc comment names the gate |
| MODIFY | `internal/agent/reboot_auth.go` | `rebootAfterDelivery`: the gate after the existing checks; skip → `abortAuthorizedReboot(id, c.reason())`; the Info log gains `waited` |
| MODIFY | `internal/agent/agent.go` | Seam `dpkgLockProbe func() dpkgLockStatus` (nil = platform probe), with a comment |
| MODIFY | `internal/agent/reboot_callsites_test.go` | Add `TestRebootCallSites_RD_AC020_GatePrecedesEveryExecReboot` and `…_ShutdownHasNoGate` (AST). The existing C-8 test is unchanged |
| MODIFY | `internal/agent/command_lifecycle_test.go` | `newLifecycleHarnessAt` sets `h.a.dpkgLockProbe` to a fixed `Free` status, so every existing harness-based reboot test (including `reboot_auth_test.go` and `maintenance_test.go`) stays deterministic on Linux CI hosts with real dpkg activity |
| MODIFY | `internal/agent/commands_test.go` | `newOSUpdateTestAgent` sets the same `Free` probe |
| MODIFY | `internal/models/command_policy.go` | `DpkgLockWaitBound`, `RebootAgentHardMax`, `RebootExecDeadline`; `AgentHardMax` and `ExecDeadline` split for `reboot`; doc comments (AD-004) |
| MODIFY | `internal/models/command_policy_test.go` | `TestCL_AC029_ExecDeadlineTable` reboot row → 20m; new RD budget tests (§8) |
| MODIFY | `internal/server/commands/service_test.go` | `TestCL_AC029_ExpiresAtPerActionClass` reboot row `dl` 600 → 1200 (TTL stays 15m) |
| MODIFY | `internal/server/commands/sweeper_test.go` | `TestSweep_RD_AC017_RebootNotTimedOutBefore20m` |

**Explicitly unchanged:**
- `internal/server/commands/sweeper.go` and `service.go`, and `internal/server/db/*`
- all migrations
- `scripts/install.sh`, the sudoers rules and the systemd unit
- `internal/agent/doctor.go` (no collector added)
- `handleShutdown`
- `internal/maintenance/*`
- all of `web/`
- `go.mod` / `go.sum` (no new or promoted dependency)

**Technical writer (post-QA):**
- **README**:
  - the reboot command's dpkg wait and the five outcomes (AD-003);
  - the post-patch skip event;
  - the "Queued TTLs and execution deadlines" table: split the Power row into `reboot` (15 min / **20 min**) and
    `shutdown` (15 min / 10 min);
  - the `/proc/locks` requirement and the btrfs note.
- **`web/src/pages/LearnMore.tsx`** manual patch and reboot guide: one paragraph saying that reboots wait up to 10 min
  for a running package manager and are skipped, not forced.
- **CHANGELOG** `[Unreleased]`: Added (dpkg wait) and Changed (reboot execution deadline 20 min).
- **Stale-content log:** `docs/architecture/CMD-LIFECYCLE-001-add.md` §AD-005 table (power 10m/1m) is amended by this
  ADD and is not edited.

## 5. Data Model Changes

**Postgres: none.** No migration and no new table, column or index. `commands.exec_deadline_s` already exists. New
`reboot` rows store `1200` instead of `600`, which is a value change computed in Go and not a schema change.

**Models (code only):** the three constants and the `reboot` branches in `AgentHardMax` and `ExecDeadline` (AD-004).
`CommandResult`, `CommandPayload`, `AgentEvent` and `UpdateInfo` are unchanged.

**Agent on-disk state: none new.** The probe reads `/var/lib/dpkg/lock{,-frontend}` metadata, `/proc/locks` and
`/proc/<pid>/comm`. It writes nothing. The existing in-flight marker and outbox entries for a reboot are unchanged
in shape.

**In-memory attempt state** (never persisted): `waiting`, `firstHeld`, `guardsPassed` (AD-002). This matches FRD §7.

**Bounded growth:** see AD-006.

## 6. API / Interface Contract

No new or changed endpoints. Contract-visible changes:

### Command record (`GET /api/v1/devices/{id}/commands`, WS broadcasts; existing)

| Field | Change |
|---|---|
| `exec_deadline_s` | `reboot` rows created after the upgrade: `1200` (was `600`). `shutdown`: `600` (unchanged) |
| `deadline_at` | `dispatched_at + 1200 s` for new `reboot` rows |
| `expires_at` | unchanged (`created_at + 15 min`) |
| `result_msg`, `status`, `outcome`, `reason` | the AD-003 table for `reboot` |

Timed-out message for reboot: `no result from agent within 20m`.

### Agent command result for `reboot` (WS / `POST …/commands/{id}/report`; existing shape)

`message` follows the AD-003 grammar:

```
reboot initiated
reboot initiated after waiting <D> for the dpkg lock
reboot skipped: dpkg lock held past the timeout (10m), held by <comm> (pid <n>)
reboot skipped: dpkg lock held past the timeout (10m), holder not visible[ (pid <n>)]
reboot skipped: dpkg lock state unknown: <cause>
reboot skipped: agent shutting down                       (reason "shutdown")
reboot skipped: guard failed: <guard text>                (reason "conflict" for "host busy: …")
```

`<cause>` ∈ `cannot stat <path>: <err>` | `cannot read /proc/locks: <err>` | `lock information exceeds 1 MiB` |
`lock check exceeded 1s` | `unrecognized /proc/locks line <n>` (≤ 200 bytes).

### Agent event (`POST /api/v1/devices/{id}/events`; existing shape)

`type = command_completed`, `severity = warning`:

```
authorized reboot for command <uuid> was not started: <reason>     (existing format)
post-patch reboot for command <uuid> was not started: <reason>     (new message on an existing event type)
```

### Internal (agent)

`awaitRebootClearance`, `probeDpkgLock` and the `dpkgLockProbe` seam (AD-001, AD-002).

## 7. Sequence / Flow

### 7.1 Standalone reboot, lock held 9 minutes (AC-017)

1. The server creates the `reboot` row with `exec_deadline_s = 1200`, dispatches it at T0, and sets
   `deadline_at = T0 + 20m`.
2. Agent intake: dedupe, marker, ack. `newCmdRun` sets the `cr.ctx` timeout to 12m.
3. `executeAction`: the maintenance precheck passes, then the host lock is taken. `handleReboot(cr)`: `AllowReboot`
   is true, so it calls `awaitRebootClearance`.
4. Probe → `Held` by `unattended-upgr` (pid 1234). `firstHeld = T0+1s`, and one Info `wait started` line is written.
5. Every 2 s: `shuttingDown`? no → probe → `Held` → sleep.
6. At T0+9m01s the probe returns `Free`. Guards: shuttingDown no, `allow_reboot` on, `maintenanceBlocker` "". Then
   the final probe returns `Free`, and the gate returns Cleared with Waited 9m. One Info `wait ended` line is written.
7. `execReboot()` starts `sudo systemctl reboot`. The result is
   `success, "reboot initiated after waiting 9m for the dpkg lock"`. `completeCommand` puts it in the durable outbox.
   It is delivered at once or after boot.
8. The server row becomes `succeeded` at about T0+9m, before `deadline_at`.

### 7.2 Standalone reboot, timeout

Steps 1–5 as in 7.1. At `firstHeld + 10m` the probe still returns `Held`, so the gate returns `skipLockTimeout` and
the result is
`error, "reboot skipped: dpkg lock held past the timeout (10m), held by unattended-upgr (pid 1234)"`. The host lock is
released by `runAction`. Nothing reboots for this command afterwards.

### 7.3 Unauthorized post-patch (auto-patch in a maintenance window), lock re-taken

1. The `os_update` result is queued (`… reboot-class applied: …; reboot initiated`), then `postResult` runs.
2. `maybeRebootAfterPatch`: `AllowReboot` true → no blocker → host reservation → claim OK → goroutine.
3. `sleep(2s)`. The `shuttingDown`/blocker re-check passes. Gate:
   1. `Held` at minute 0, so the wait starts.
   2. `Free` at minute 4. The guards pass.
   3. The final probe returns `Held` (re-taken), so `guardsPassed` is reset and the same deadline continues.
   4. At minute 10 the lock is still `Held` → `skipLockTimeout`.
4. `abortPostPatchReboot(id, "dpkg lock held past the timeout (10m), held by apt-get (pid 4321)")`: one Warn, one
   warning event, one telemetry trigger.
5. The goroutine returns, and the `defer` releases `patch-reboot:<id>`. The next host command is accepted.

### 7.4 Authorized phase B

`runAuthorizedReboot` reserves → `rebootAfterDelivery`: `awaitResultDelivery` → existing checks → gate → on skip,
`abortAuthorizedReboot(id, c.reason())`; on clearance, Info log and `execReboot()`. The phase A text says
"reboot started: yes" (OQ-004), and the event records any skip.

### 7.5 Agent stop during a wait

SIGTERM sets `shuttingDown`. Within one poll (≤ 2 s) plus one probe (≤ 1 s), the gate returns `skipShutdown`.
- **Standalone:** result (d), durable in the outbox, delivered after the restart.
- **Post-patch:** abort, which writes the Warn log and queues the event best-effort.

`waitForCommands` returns long before its 20 s grace period ends.

## 8. Acceptance Criteria Mapping

**Naming.**
- Go test names contain `RD_ACnnn`, and assertion messages start with `[AC-nnn]`.
- Tests are co-located in package `agent`, `models` or `commands`, following the existing convention.
- "Three paths" tests are table-driven over `standalone`, `unauthorized` and `authorized`. They use the
  `lifecycleHarness` and `fakeClock`, with `a.pause = func(ctx, d) bool { clk.Advance(d); return ctx.Err() == nil }`
  and a scripted `dpkgLockProbe` that returns a sequence of statuses keyed on the fake time. They use no real time and
  no real lock (NFR-006).

| AC | Fulfilled By | Tests |
|---|---|---|
| AC-001 | AD-002 free branch | `TestRebootClearance_RD_AC001_FreeLockRebootsWithNoSleepOnAllThreePaths` (reboot runner called once; zero `pause` calls from the gate; standalone message exactly `reboot initiated`); `TestRebootClearance_RD_AC001_FreeRunsGuardsThenFinalProbe` (probe, guards, probe order) |
| AC-002 | AD-001 match; AD-002 held branch | `TestRebootClearance_RD_AC002_HeldLockNeverRebootsWithinBoundOnAllThreePaths` (asserts zero reboot calls at every `pause` up to 9m58s); `TestProcLocks_RD_AC002_PosixWriteLineMatchesLockFrontend`; `…_HexMajorMinorDecimalInode` (`00:16d:3947933`, `103:02:262338`); `…_WaiterLineIgnored`; `…_NoneInodeLineSkipped`; `…_EveryLockTypeCounts`; `…_BlockDeviceRequiresExactDevice`; `…_AnonymousDeviceMatchesOnInode` (btrfs); `TestDecodeLinuxDev_RD_AC002_HawaiiDevIs259_2` (and the large major/minor encoding); `TestDpkgLock_RD_AC002_RealProcLocksSeesFcntlLockOnTempFile` (Linux only: `FcntlFlock F_SETLK` on a temp file, `LockPaths=[temp]`, real `/proc/locks` → `Held`, `PID == os.Getpid()`) |
| AC-003 | AD-001 targets | `TestProcLocks_RD_AC003_LegacyLockAloneIsHeld` (only `/var/lib/dpkg/lock` matches); `TestDpkgProbe_RD_AC003_FrontendAbsentLegacyPresentStillChecked`; `TestRebootClearance_RD_AC003_LegacyHeldWaitsLikeFrontend` |
| AC-004 | AD-002 poll and guards | `TestRebootClearance_RD_AC004_ReleaseAfter3mRebootsWithin10s` (fake time: reboot time minus release time ≤ 10 s, on each path); `TestRebootClearance_RD_AC004_PollIntervalAndProbeTimeoutFitBound` (`dpkgLockPoll + 5s + 2*dpkgProbeTimeout ≤ 10s`) |
| AC-005 | AD-002 timeout; FR-009 | `TestRebootClearance_RD_AC005_TimeoutSkipsAndNeverForces` (zero reboot calls on all paths); `…_NoSecondWaitAfterSkip` (probe and `pause` counts stop at return; advancing the clock 30m more causes no further calls); `…_ProbeAtExactBoundHeldSkips` and `…_ProbeAtExactBoundFreeProceeds` (boundary) |
| AC-006 | AD-003 (b); `holderText` | `TestHandleReboot_RD_AC006_TimeoutResultFailedAndNamesHolder` (status `error`; message contains `dpkg lock held past the timeout`, `unattended-upgr` and `pid 1234`; does not contain `reboot initiated`); `TestDpkgProbe_RD_AC006_HolderNamedFromComm`; `TestDpkgProbe_RD_AC006_CommSanitizedAndCapped` (control bytes → `?`, ≤ 16 bytes) |
| AC-007 | `holderText` | `TestDpkgProbe_RD_AC007_PidZeroHolderNotVisible`; `…_OFDPidMinusOneHolderNotVisible`; `…_CommUnreadableSaysHolderNotVisibleWithPid`; `TestHandleReboot_RD_AC007_TimeoutReportSaysHolderNotVisible` |
| AC-008 | AD-005 | `TestPostPatchReboot_RD_AC008_AuthorizedTimeoutOneEventOneTelemetry` and `TestPostPatchReboot_RD_AC008_UnauthorizedTimeoutOneEventOneTelemetry` (exactly one agent event, containing the command ID, `dpkg lock held past the timeout` and the holder; `telemetryNow` holds a signal; zero reboots) |
| AC-009 | goroutine `defer Unlock` | `TestPostPatchReboot_RD_AC009_SkipReleasesHostReservation` (both paths: after the skip, `TryLockAll({host}, idB, os_update)` succeeds) |
| AC-010 | AD-001 unknown; AD-002 | `TestDpkgProbe_RD_AC010_StatErrorOtherThanNotExistIsUnknown`; `…_ProcLocksMissingIsUnknown` (ENOENT, the `ProcSubset=pid` case); `…_ProcLocksPermissionDeniedIsUnknown`; `TestProcLocks_RD_AC010_MalformedLineIsUnknown` (table: too few fields, non-hex major, non-decimal inode, non-numeric pid, missing `:` suffix); `TestRebootClearance_RD_AC010_UnknownSkipsImmediatelyWithoutSleepOnAllThreePaths` (standalone message `reboot skipped: dpkg lock state unknown: …`; post-patch: one event with the cause); `…_UnknownMidWaitSkipsAtOnce` |
| AC-011 | AD-001 caps | `TestDpkgProbe_RD_AC011_OverOneMiBIsUnknown`; `…_ExactlyOneMiBParses` (boundary); `…_LineOver4KiBIsUnknown`; `…_SlowReadOverOneSecondIsUnknown` (fake `Now` advances per line); `…_SlowCommReadIsUnknown`; `TestHandleReboot_RD_AC011_OversizedLockInfoSkipsReboot` |
| AC-012 | AD-001 absent; `dpkglock_other.go` | `TestDpkgProbe_RD_AC012_BothLockFilesAbsentIsAbsentWithoutReadingProcLocks` (the fake `Open` is never called); `TestRebootClearance_RD_AC012_AbsentRebootsAsToday` (message exactly `reboot initiated`, zero sleeps, zero wait log lines); `TestPlatformProbe_RD_AC012_NonLinuxIsAbsent` (`!linux` build; runs on Windows); `TestStatFileID_RD_AC012_MissingPathIsNotExist` (Linux) |
| AC-013 | AD-002 guards | `TestRebootClearance_RD_AC013_AllowRebootOffDuringWaitSkipsNamingGuard` and `…_MaintenanceStartsDuringWaitSkipsNamingGuard` (all three paths; the flip happens inside `pause`; the standalone message contains `guard failed: commands.allow_reboot is off` / `host busy:`; post-patch events name the guard) |
| AC-014 | AD-002 `guardsPassed` reset and single deadline | `TestRebootClearance_RD_AC014_FinalCheckRetakenKeepsWaitingWithinBound` (Held until 4m, Free, then Held again → skip at exactly `firstHeld + 10m`, never later; one `wait started` line; guards evaluated twice when the lock frees again) |
| AC-015 | AD-002 `shuttingDown` poll | `TestRebootClearance_RD_AC015_ShutdownEndsWaitWithin5s` (set `shuttingDown` inside `pause`; the gate returns after ≤ 1 further probe; fake elapsed ≤ 3 s; no reboot); `TestHandleReboot_RD_AC015_ShutdownResultReasonShutdown`; `TestPostPatchReboot_RD_AC015_ShutdownSkipLogsWarn` |
| AC-016 | AD-003 synchronous contract | `TestHandleReboot_RD_AC016_NoRebootAfterSkippedResult` (after the skip result, the probe flips to `Free` and the clock advances; the reboot runner count stays 0; redelivering the same ID through `submitCommand` is deduped) |
| AC-017 | AD-003 (a); AD-004 | `TestHandleReboot_RD_AC017_NineMinuteWaitReportsStartedAndWaited` (status `success`, message `reboot initiated after waiting 9m for the dpkg lock`, `cr.ctx.Err() == nil` at the reboot); `TestCommandPolicy_RD_AC017_RebootBudgetCoversWait` (`RebootAgentHardMax ≥ DpkgLockWaitBound + 2m`; `ExecDeadline(reboot) > AgentHardMax(reboot) + DeadlineSlack`; `AgentHardMax(reboot)` used by `newCmdRun`); `TestSweep_RD_AC017_RebootNotTimedOutBefore20m` (a dispatched reboot is not timed out at T+10m01s, and is timed out after T+20m); updated `TestCL_AC029_ExecDeadlineTable` and `TestCL_AC029_ExpiresAtPerActionClass` (server) |
| AC-018 | AD-001 read-only probe | `TestDpkgProbe_RD_AC018_NeverOpensLockFiles` (the fake `Open` records only `/proc/locks` and `/proc/<pid>/comm`); `TestDpkgLock_RD_AC018_NoLockingCallsInSource` (AST over `dpkglock*.go` and `reboot_clearance.go`, excluding tests: no `FcntlFlock`, `Flock`, `F_SETLK`, `F_GETLK`, `os.OpenFile`, `syscall.Open`); `TestDpkgLock_RD_AC018_ProbeDoesNotDisturbHeldLock` (Linux: after 50 probes, the test's temp-file lock is still held and the probe still reports `Held`) |
| AC-019 | AD-006 | `TestRebootClearance_RD_AC019_FullWaitBoundedOutput` (full 10m wait ending in a skip: a capture handler counts ≤ 2 Info-or-above records from the gate and zero Debug records from it; ≤ 1 agent event; ≤ 1 telemetry signal (post-patch); probe calls ≤ 303; a snapshot of the harness state directory before and after shows no new file) |
| AC-020 | AD-002 placement; AD-004 shutdown | existing `TestRebootAuth_PF_C8_ExecRebootHasExactlyThreeGuardedCallSites` (unchanged, must pass); `TestRebootCallSites_RD_AC020_GatePrecedesEveryExecReboot` (AST: in each of the three functions, the body text before `execReboot` contains `awaitRebootClearance`); `TestRebootCallSites_RD_AC020_ShutdownHasNoGate` (AST: the `handleShutdown` body contains neither `awaitRebootClearance` nor `probeDpkg`. **Do not call `handleShutdown` in a test**: it executes a real shutdown); `TestCommandPolicy_RD_AC020_ShutdownBudgetUnchanged` (`AgentHardMax` 1m, `ExecDeadline` 10m, `QueuedTTL` 15m) |
| AC-021 | AD-001 unprivileged | `TestDpkgProbe_RD_AC021_NoExecNoSudo` (the fake `runner` records zero calls during probes and the gate, except the guard's `systemctl show`); `TestDpkgLock_RD_AC002_RealProcLocksSeesFcntlLockOnTempFile` doubles as the unprivileged proof (skipped when euid is 0); DoD diff check: `scripts/install.sh`, sudoers and the unit file are unchanged |

## 9. Error Handling

| Failure | Behavior | Log |
|---|---|---|
| Both lock files absent | `Absent`; reboot as today | none |
| `stat` error other than not-exist | `Unknown` → skip at once | the path's outcome line (Warn) |
| `/proc/locks` missing, unreadable, over 1 MiB, line over 4 KiB, malformed, or over 1 s | `Unknown` → skip at once, with the cause | the path's outcome line |
| `comm` unreadable | `Held` stays; holder `holder not visible (pid n)` | none |
| Lock held past 10m | skip (b) / timeout event | `wait ended` Info + the path's outcome line |
| Guard fails after the wait | skip (e) / guard event | `wait ended` Info (if a wait happened) + the outcome line |
| Agent stopping | skip (d) / shutdown event (best-effort) | the outcome line (Warn) |
| `cr.ctx` deadline (not reachable by design) | skip (e) `agent command time budget exhausted` | the outcome line |
| `execReboot` error | unchanged per path (standalone `reboot: <err>`; authorized abort; unauthorized Warn) | unchanged |

Standalone outcome line: the existing `completeCommand` `command: failed` Warn and failed-command event. Post-patch
outcome line: the abort Warn.

## 10. Security Considerations

- **No privilege added (NFR-001).** The probe only stats metadata and reads world-readable `/proc` files. It runs no
  exec and no sudo. The sudoers argv, `install.sh` and the unit file are unchanged, so the story ships with a normal
  signed agent update.
- **The probe cannot interfere with dpkg (FR-007).** It never opens the lock files (structural test). Reading
  `/proc/locks` acquires no file lock.
- **Untrusted text.** `comm` is set by its own process (`prctl`). It is reduced to ≤ 16 printable ASCII bytes before
  it reaches results, events or logs. Causes are sanitized and capped at 200 bytes. `/proc/locks` content is never
  echoed, only a line number.
- **Fail closed (BR-001).** Unknown state, parse errors and caps all skip. The gate can only delay or prevent a reboot
  (BR-003). `allow_reboot` is re-checked after the wait, and the C-8 call-site set is unchanged and test-enforced.
- **No reboot loop (BR-002).** One gate call per attempt, no retry, and the at-most-once intent store is unchanged.
- **Residual risks:**
  - **R-1 (NFR-004, accepted):** the millisecond window between the final probe and `systemctl reboot` starting. A
    package process that takes the lock in that window can still be interrupted. On Debian, the
    `unattended-upgrades` shutdown unit adds a partial safeguard for unattended runs.
  - **R-2 (pid namespace):** detection keys on device and inode, not pid, so it works whatever the namespaces. A holder
    outside the agent's pid namespace shows as pid 0, giving "holder not visible". The agent runs in the init
    namespace on supported installs. A unit hardened later with `ProtectProc=invisible` would hide root holders'
    `comm` (holder not visible) but not the lock.
  - **R-3 (anonymous-device leniency):** on btrfs and other major-0 filesystems, any lock on another major-0
    filesystem with the same inode number counts as held. An unprivileged local user on such a host could
    deliberately create a tmpfs file with a matching inode number and lock it, which delays and then skips rIOt
    reboots. The skip report names the holder process, so it is diagnosable. It is not a privilege escalation, and
    ext4/xfs hosts are unaffected. Possible follow-up: ignore lenient matches whose visible holder has a non-zero
    euid in `/proc/<pid>/status`.
  - **R-4 (`/proc/locks` restricted):** hardened kernels, an LSM, or a future `ProcSubset=pid` unit make every reboot
    on a dpkg host skip with `cannot read /proc/locks: …`. This is visible in the result or the event, and it fails
    safe. Hosts with more than about 15,000 active locks (over 1 MiB) behave the same way.
  - **R-5 (version skew):** a new agent with an old server (10m deadline) could be marked `timed_out` by the old
    server if a standalone wait ends in the last seconds before 10m after dispatch. The agent's late result is then
    rejected as already terminal, and the reboot may still start. The mitigation is release ordering: upgrade the
    server first, which is the normal flow. The technical writer notes this in the CHANGELOG. An old agent with a new
    server only gets a longer lost-result timeout.
  - **R-6:** the unauthorized path's `os_update` result still says `…; reboot initiated)` before phase B, and a later
    skip is recorded only by the event. This is the same accepted pattern as OQ-004 and is left unchanged.
  - **R-7:** a post-patch skip caused by agent shutdown is Warn-logged. If the process exits before the logger
    flushes, or before the notifier sends the event, either can be lost. No reboot can start in that case.

## 11. Performance Considerations

- **Common case (lock free or absent):** 2 to 4 `stat` calls, 0 to 2 `/proc/locks` reads (each typically under
  4 KiB and under 1 ms), and one guard evaluation. No added latency is visible to the operator.
- **During a wait:** one probe every 2 s for at most 10 min (≤ 303), with streamed parsing in O(1) memory. The 1 s
  probe cap bounds a pathological read.
- **Guards:** ≤ 1 `systemctl show` per free observation (normally 1 per attempt).
- **Server:** no new queries, no index and no caching. The sweeper cadence is unchanged. A reboot row stays in-flight
  up to 10 minutes longer in the worst case, which is negligible.
- **No caching, rate limiting or indexing is required.**

## 12. Implementation Notes for Engineers

1. **Commits:** `[REBOOT-DPKG-001] <type>: <what>`. Suggested order:
   1. models (budgets + tests)
   2. `dpkglock*` (probe + tests)
   3. `reboot_clearance` (gate + tests)
   4. wire the three paths and update the harnesses
   5. server test updates

   Use one logical change per commit.
2. **Keep C-8 green without editing it.** Each of the three functions keeps its literal
   `a.config.Commands.AllowReboot` check **before** the gate. Do not move the gate into `execReboot`, and add no
   fourth caller.
3. **Nothing between Cleared and `execReboot`.** No log line goes before the call except the authorized path's
   existing Info line. Do not re-check `AllowReboot` there either: the gate's guards already did, and FR-012 requires
   the lock probe to be the last check.
4. **Clocks and sleeps:** use only `a.clock().Now()` and `a.sleep(ctx, d)`, never `time.Now()` or `time.Sleep` in the
   gate or the probe. `probeDpkgLock` takes `Now` through `dpkgProbeEnv`.
5. **Test determinism on Linux CI:** GitHub runners have real dpkg, and apt-daily may hold the lock. Every test that
   reaches a reboot path must set `dpkgLockProbe`. The harness defaults (§4) cover the existing tests. New tests built
   outside the harness must set it too. Only the `dpkglock_linux_test.go` tests use the real `/proc/locks`, and only
   against their own temp file.
6. **Existing pause-based tests:**
   - `TestMaintenance_AC009_*` closes a channel in `pause`. With a `Free` probe the gate never sleeps, so it is still
     called once. Keep it that way: the free path must make zero `sleep` calls.
   - `TestRebootAuth_PF_AC020_AllowRebootTurnedOffBeforeTheRebootAborts` still passes through the pre-gate check.
7. **`handleReboot` signature change:** `executeAction` returns its `commandExecResult` directly. Set `Reason` only as
   in AD-003. Never use `models.ReasonTimeout` for a dpkg skip.
8. **`decodeLinuxDev` and the parser are pure.** Test them on Windows. `statFileID` must handle
   `syscall.Stat_t.Dev`, whose width differs by architecture: always convert through `uint64(st.Dev)`. Cross-compile
   check: `make build-agent-all` must succeed for all 8 targets.
9. **No new dependency.** Do not import `golang.org/x/sys/unix`. Use `syscall` (Linux file only) and the pure decoder.
10. **Do not call `handleShutdown` in tests.** It runs a real `shutdown` command. Use the AST test (§8 AC-020).
11. **Host verification** on a Debian host (Hawaii or vermont), recorded in the implementation report:
    1. As `riot`, the probe returns `Free` at rest.
    2. While `/var/lib/dpkg/lock-frontend` is held for 30 s, the probe returns `Held` with the holder named. Hold the
       lock with a root `python3` one-liner that calls `fcntl.lockf(..., LOCK_EX)`, or catch a real
       `apt-get update`.
    3. `stat` dev and inode match the `/proc/locks` line.
    4. A standalone reboot command issued during a held lock shows `running`, then `succeeded` with the waited text, or
       `failed` with the skip text. **Ryan approves the actual reboot.**

    Only Ryan triggers real reboots.

## 13. Definition of Done

- [ ] Every Section 4 change implemented, and every "explicitly unchanged" file untouched (`git diff --stat`
      reviewed; `go.mod`/`go.sum`, `scripts/`, migrations and `web/` show no diff)
- [ ] Every Section 8 test exists, carries its `RD_ACnnn` name and `[AC-nnn]` messages, and passes
- [ ] `go test ./...` (Windows) and `go test -race ./...` (Linux CI) green; `cd web && npm run test:run` green
      (unchanged)
- [ ] `go vet ./...` clean; `make build-agent-all` succeeds for every target
- [ ] No migration added; `make migrate-up` / `make migrate-down` unaffected
- [ ] `TestRebootAuth_PF_C8_ExecRebootHasExactlyThreeGuardedCallSites` passes unmodified
- [ ] `grep -rn "time.Now()\|time.Sleep" internal/agent/dpkglock*.go internal/agent/reboot_clearance.go` (non-test)
      returns nothing
- [ ] `models.AgentHardMax("shutdown",1) == time.Minute`, `ExecDeadline("shutdown",1) == 10*time.Minute`, and
      `QueuedTTL` is unchanged for both power actions
- [ ] Host verification (§12 note 11) recorded in `docs/implementation/REBOOT-DPKG-001-impl-report.md`
- [ ] Security review `docs/security/REBOOT-DPKG-001-security-review.md` completed before engineering starts
      (FRD D-007)
