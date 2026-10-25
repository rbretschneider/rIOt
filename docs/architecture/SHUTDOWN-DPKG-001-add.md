# Architecture Decision Document

- **Story ID:** SHUTDOWN-DPKG-001
- **FRD Reference:** `docs/requirements/SHUTDOWN-DPKG-001-frd.md`
- **Reuses:** REBOOT-DPKG-001 (`docs/architecture/REBOOT-DPKG-001-add.md`, FINAL revision 3; security review
  `docs/security/REBOOT-DPKG-001-security-review.md`, conditions C-1 to C-7)
- **Amends:** the power-class budgets of CMD-LIFECYCLE-001 AD-005 / AD-013 and REBOOT-DPKG-001 AD-004, for `shutdown`
  only (AD-005 below)
- **Author:** Architect Agent
- **Date:** 2026-10-25
- **Status:** DRAFT, revision 1. Ready for the security review (FRD D-007). Engineering starts after that review.

## Changelog

### Revision 1

- First version.
- **FRD product decisions.** OQ-001 to OQ-009 are adopted as decided. OQ-004 keeps the default values: 12 minutes for
  the agent and 20 minutes on the server, for the reason given in AD-005.
- **No blocker.** One FRD premise is factually wrong but does not block the design. NFR-002 says "the poweroff keeps
  using its existing sudo rule", but `scripts/install.sh` writes no `systemctl poweroff` rule. See R-6 and §12 note 9.
  This story keeps the poweroff argv byte-identical and adds no privilege, so the design is the same either way.
- **AC-019 reading.** Three REBOOT-DPKG-001 tests pinned that shutdown had no gate and kept the old shutdown budgets.
  That is exactly the behaviour this story changes, so those three tests are retired or edited. Every reboot assertion
  is kept. Other REBOOT tests get only mechanical identifier renames. AD-008 lists every edit.

**For Ryan (not blocking; each item is in §10):**
- R-4: the local-user veto from REBOOT-DPKG-001 SEC-002 now also covers rIOt shutdowns. Each veto is visible.
- R-6: install.sh grants no sudo rule for `systemctl poweroff`. On a host installed by the current installer, the
  shutdown command probably reports `shutdown initiated` while `sudo` fails behind it. This is a pre-existing problem
  that this story does not change. A follow-up story is recommended.
- R-7: a waiting shutdown cannot be cancelled from the dashboard. It can power the host off up to 10 minutes after
  the click, and the host then stays off.

---

## 1. Summary

The `shutdown` command goes through the same gate as reboot: the REBOOT-DPKG-001 dpkg-lock probe, then the bounded
wait, then the guards and a final lock check. The gate loop is not copied. It is parameterized by a small
`powerGate` spec: the action, the wait bound, and the wait-started log message. The single-use permit becomes an
**action-bound** permit, `powerPermit{id, action}`. It has one sequence and one live slot. `execReboot` refuses any
permit that is not a reboot permit, and the new `execShutdown` refuses any permit that is not a shutdown permit.

`handleShutdown(cr) commandExecResult` uses the `a.runner` test seam. It reports the six FR-013 outcomes through a
skip mapping that it shares with reboot.

Shutdown gets its own budget constants: wait bound 10 minutes, agent hard maximum 12 minutes, server execution
deadline 20 minutes. The queued TTL stays 15 minutes.

Reboot results, events, log lines, budgets and call sites stay byte-identical. The story adds no migration, no UI
change, no sudoers or `install.sh` change, and no new dependency.

## 2. Technical Context

Verified on `feat/shutdown-dpkg-001`, whose base `09c0ecf` contains PR #34.

- **Shutdown path today** (`internal/agent/commands.go`):
  - `executeAction` handles `case "shutdown":` with `status, message = a.handleShutdown(payload)` (line 173).
  - `handleShutdown(payload) (string, string)` (line 387) checks `Commands.AllowShutdown`. It then runs
    `exec.Command("sudo","systemctl","poweroff")`, or `shutdown /s /t 5` on Windows, followed by `cmd.Start()`. The
    messages are `shutdown not allowed by agent config (set commands.allow_shutdown: true)`, `shutdown: <err>` and
    `shutdown initiated`.
  - It does **not** use `a.runner`, so no test can call it. REBOOT-DPKG-001 §12 note 10 says "never call
    `handleShutdown` in tests".
  - It is the only poweroff call site in the code base: `grep poweroff` finds `commands.go:396` only.
- **Before dispatch.** `executeAction` already applies the host maintenance-blocker precheck (line 101) and the host
  target lock (line 110). `shutdown` is a host-target action (`command_policy.go:361`). `runAction` releases the lock
  when the handler returns. `newCmdRun` (`command_lifecycle.go:355`) sets `cr.ctx` to
  `models.AgentHardMax(action)`, which is 1 minute for shutdown today. The handler runs synchronously, and its return
  value is the result. No watchdog reports independently.
- **Restart.** `recoverInterruptedCommands` (`command_lifecycle.go:641`) clears `shutdown` markers without a result
  and without a re-run. The seen-ID store deduplicates a redelivery.
- **What REBOOT-DPKG-001 built** (`internal/agent/reboot_clearance.go`, `dpkglock*.go`):
  - The probe `a.probeDpkg()` and the seam `Agent.dpkgLockProbe`. Non-Linux hosts get `Absent`.
  - The gate `awaitRebootClearance(ctx, path, commandID, guards) rebootClearance`. `models.DpkgLockWaitBound`
    appears in it in three places: the bound check, the sleep cap, and `reason()`. The Info message
    `"dpkg lock held, waiting before reboot"` is a literal. The gate also writes `"dpkg lock wait ended"` with the
    attributes `path`, `command_id`, `outcome`, `waited` and `holder`.
  - `commonRebootGuards(ownID)`, in this order: `shuttingDown`, then `AllowReboot`, then `maintenanceBlockerFor`.
  - Permit: `rebootPermit{id}`, `mintRebootPermit()`, and the agent fields `rebootPermitSeq` / `rebootPermitLive`.
    `execReboot` CAS-consumes the permit, and `errRebootNotCleared` reports a refusal.
  - `rebootSkippedResult(c)`: the reason is `shutdown` for an agent stop and `conflict` for `host busy: …`.
    `ReasonTimeout` is never used.
  - `reboot_callsites_test.go`: the per-reference AST scan for `execReboot`, the C-8 test, the mint test, and
    `TestRebootCallSites_RD_AC020_ShutdownHasNoGate`.
- **Budgets** (`internal/models/command_policy.go:96-233`):
  - `ClassPower` = `reboot` and `shutdown`.
  - `reboot`: 12 minutes for the agent, 20 minutes on the server, through `if action == ActionReboot` branches.
  - `shutdown`: 1 minute and 10 minutes, through the class fallback.
  - The queued TTL is 15 minutes for both. `TestCL_DeadlineInvariant` requires `ExecDeadline > AgentHardMax + 5m`.
- **Pinned shutdown values** that this story changes:
  - `TestCL_AC029_ExecDeadlineTable` (`{"shutdown", 0, 10m}`);
  - `TestCommandPolicy_RD_AC020_ShutdownBudgetUnchanged`;
  - `TestCommandPolicy_QA_AC020_RebootAndShutdownBudgetsAreIndependent`;
  - `server/commands/service_test.go:211` (`{"shutdown", nil, 15m, 600}`);
  - `sweeper_test.go:248` (subtest "shutdown keeps the 10m deadline").
- **Server.** `service.go:224` stores `exec_deadline_s` from `models.ExecDeadlineSeconds` when it creates a command,
  and the sweeper reads each row's own value. No server code outside `models` is specific to shutdown.
- **Harnesses.**
  - `newLifecycleHarnessAt` sets a `Free` probe and **no runner**.
  - `newAuthHarness` sets a runner that counts **every** `sudo systemctl …` as a reboot.
  - `newGateRig` wraps that harness with a scripted probe and a pause that advances the fake clock.
- **Sudoers.** `install.sh:436` grants `systemctl reboot`. **It grants no `systemctl poweroff` rule.** This is already
  logged in `docs/technical-writing/AGENT-HARDEN-001-docs-report.md:72` and `REG-HARDEN-001-docs-report.md:63` (R-6).
- **Web.** `DeviceDetail.tsx` has the power menu and confirm dialog (`Shutting down…`). No deadline text appears
  there, and nothing in it changes.

**Missing:** a lock wait on the shutdown path; a testable shutdown exec; shutdown budgets that fit a 10-minute wait;
and call-site enforcement for poweroff.

## 3. Architecture Decisions

### AD-001: One gate, parameterized by a `powerGate` spec (NFR-001, FR-001, FR-003 to FR-012, FR-017)

**Decision.** In `internal/agent/reboot_clearance.go`, which keeps its file name (§12 note 2), the gate loop becomes
`awaitPowerClearance`. Two fixed-spec entry points wrap it.

```go
type powerAction uint8

const (
    powerActionNone     powerAction = iota // zero value: never valid for any exec
    powerActionReboot
    powerActionShutdown
)

// powerGate is everything that differs between the reboot and shutdown gates.
type powerGate struct {
    action    powerAction
    waitBound time.Duration
    waitMsg   string // Info message when a wait starts
}

func rebootGate() powerGate   // {powerActionReboot,   models.DpkgLockWaitBound,         "dpkg lock held, waiting before reboot"}
func shutdownGate() powerGate // {powerActionShutdown, models.ShutdownDpkgLockWaitBound, "dpkg lock held, waiting before shutdown"}

type powerGuards func(ctx context.Context) string // was rebootGuards

type powerClearance struct { // was rebootClearance
    Cleared bool
    Permit  powerPermit   // valid only when Cleared (AD-002)
    Waited  time.Duration
    Skip    string        // "" | skipLockTimeout | skipLockUnknown | skipShutdown | skipGuard
    Detail  string
    Bound   time.Duration // the wait bound of the gate that produced it; set on every return
}
func (c powerClearance) reason() string

func (a *Agent) awaitPowerClearance(ctx context.Context, g powerGate, path, commandID string, guards powerGuards) powerClearance
func (a *Agent) awaitRebootClearance(ctx context.Context, path, commandID string, guards powerGuards) powerClearance // = awaitPowerClearance(ctx, rebootGate(), …)
func (a *Agent) awaitShutdownClearance(ctx context.Context, commandID string, guards powerGuards) powerClearance     // = awaitPowerClearance(ctx, shutdownGate(), "shutdown", …)
```

**The body of `awaitPowerClearance` is today's `awaitRebootClearance` body, moved, with exactly these changes:**
1. `models.DpkgLockWaitBound` becomes `g.waitBound`, in the bound check and in the sleep cap
   `min(dpkgLockPoll, g.waitBound-elapsed)`.
2. The literal `"dpkg lock held, waiting before reboot"` becomes `g.waitMsg`. The attributes are unchanged.
3. `a.mintRebootPermit()` becomes `a.mintPowerPermit(g.action)` (AD-002).
4. `skip()` and `cleared()` set `Bound: g.waitBound`.

Nothing else changes: the order of the checks, the `sawHeld` rule, the fail-closed `default` case, the `shuttingDown`
polling, the guard-then-fresh-probe sequence, the single deadline at `firstHeld + bound`, and the `"dpkg lock wait
ended"` line.

`reason()` formats the timeout as `dpkg lock held past the timeout (<FormatDurationShort(c.Bound)>), <Detail>`. For
reboot, `Bound` is `models.DpkgLockWaitBound` (10 minutes), so the text is unchanged: `(10m)`.

**Shutdown placement.**

| Path | Before the gate (kept, fail-fast) | Gate call | Skip | Clearance |
|---|---|---|---|---|
| `handleShutdown(cr)` | `if !a.config.Commands.AllowShutdown` → the existing message | `c := a.awaitShutdownClearance(cr.ctx, cr.id, a.commonShutdownGuards())` | `shutdownSkippedResult(c)` (AD-004) | `a.execShutdown(c.Permit)` |

The `path` label is `"shutdown"`. The three reboot call sites keep their current text exactly, because
`awaitRebootClearance` keeps its signature.

**Properties.** These are inherited unchanged from REBOOT-DPKG-001 AD-002 and hold for shutdown with
`g.waitBound = 10m`:
- A lock that is free at the first check gives probe → guards → probe → clear, with zero sleeps and zero wait
  lines (FR-005).
- An `Absent` result, on a non-dpkg host or on Windows, follows the same sequence (FR-006).
- One wait per attempt, with one deadline at `firstHeld + 10m` (FR-003, FR-008, FR-011).
- `Held` followed by `Absent` skips as `Unknown` (FR-009, C-1 c).
- A release reaches the poweroff in ≤ 9 s: a 2 s poll, a 1 s probe, ≤ 5 s of guards and a 1 s final probe (FR-004).
- An agent stop ends the wait in ≤ 3 s. If the stop arrives during a guard evaluation, it ends in ≤ 5 s plus one probe
  (the `maintenance.go:38` cap, accepted in REBOOT-DPKG-001) (FR-012).
- The worst-case wall time is about 10m07s.

**Rationale.**
- NFR-001 forbids a second loop. Only the bound, the log message and the permit action differ, so a three-field spec
  is the smallest correct generalization.
- The fixed-spec entry points mean no call site can pair one action with the other action's spec. They also keep the
  reboot call sites and their AST evidence (`assigned from a.awaitRebootClearance`) unchanged.

**Alternatives considered.**
- *Copy the loop into `awaitShutdownClearance`.* Rejected by NFR-001.
- *Pass the bound and message as loose arguments at each call site.* Rejected: four call sites could each get them
  wrong, and the AST test would have to inspect literals.
- *Package-level `var rebootGate = powerGate{…}`.* Rejected: a mutable global. Functions that return literals cannot
  be reassigned.
- *Keep `rebootClearance` / `rebootGuards` as type aliases.* Rejected: the alias names would be misleading for
  shutdown, and they would weaken the AST literal rule (AD-006). The renames are mechanical (AD-008).
- *Rename `reboot_clearance.go` to `power_clearance.go`.* Rejected: `dpkglock_test.go:787` (AC-018 of REBOOT-DPKG-001)
  scans that file by name, and the REBOOT ADD, security review and DoD greps cite it.

**Consequences.** `awaitPowerClearance` may be called only from the two entry points (AD-006). A future power action
adds one spec function and one entry point.

---

### AD-002: Action-bound, single-use `powerPermit` (C-3 extended; FR-015, FR-022, AC-020)

**Decision.**

```go
// powerPermit is proof that the gate cleared one attempt for one action.
type powerPermit struct {
    id     uint64
    action powerAction
}

var (
    errRebootNotCleared   = errors.New("reboot refused: no dpkg-lock clearance for this attempt")   // unchanged
    errShutdownNotCleared = errors.New("shutdown refused: no dpkg-lock clearance for this attempt")
)

func (a *Agent) mintPowerPermit(action powerAction) powerPermit          // id = a.powerPermitSeq.Add(1); a.powerPermitLive.Store(id); return powerPermit{id, action}
func (a *Agent) consumePowerPermit(p powerPermit, want powerAction) bool // p.id != 0 && p.action == want && a.powerPermitLive.CompareAndSwap(p.id, 0)
func (a *Agent) execReboot(p powerPermit) error                          // !consumePowerPermit(p, powerActionReboot)   → errRebootNotCleared, nothing run
func (a *Agent) execShutdown(p powerPermit) error                        // !consumePowerPermit(p, powerActionShutdown) → errShutdownNotCleared, nothing run
```

The `Agent` fields `rebootPermitSeq` and `rebootPermitLive` are renamed `powerPermitSeq` and `powerPermitLive`, both
`atomic.Uint64`.

**Semantics.**
- **Refused, with no state change:** a zero permit, a permit for the other action, a permit already used, and a stale
  permit (a newer one has been minted). The action check comes before the CAS, so a wrong-action permit is refused
  without consuming the live slot.
- **One live slot for both actions.** Minting any power permit invalidates every older one, whatever its action. The
  host key makes power gates mutually exclusive (FR-023), so two permits never compete legitimately. If they ever did,
  the older one would fail closed.
- **Compile-time layer.** The type is unexported, so code outside package `agent` cannot build one. Inside the
  package, the AST rule of AD-006 allows a `powerPermit` literal only in `mintPowerPermit`.
- **Runtime layer.** Single use and the action match.
- **Call-site layer.** The per-reference AST check of AD-006 for both exec functions.

**`execShutdown` body** after the permit check:
- With `a.runner` set: `a.runner(context.Background(), "sudo", "systemctl", "poweroff")`. That is the same argv as
  today, and it mirrors `execReboot`'s seam.
- Otherwise: `shutdown /s /t 5` on Windows and `sudo systemctl poweroff` elsewhere, then `cmd.Start()`. This is
  unchanged from today.

**`execReboot` change:** its first line `if p.id == 0 || !a.rebootPermitLive.CompareAndSwap(p.id, 0)` becomes
`if !a.consumePowerPermit(p, powerActionReboot)`. For every permit the reboot gate mints, the result is identical
(AD-008).

**Rationale.** The brief requires that a shutdown attempt's permit cannot start a reboot, and the reverse. A Go
type parameter per action would duplicate the gate's return type, and two permit types would be two permit systems.
One type with an immutable action field gives the property at runtime, and the AST test gives it at the call sites.

**Alternatives considered.**
- *Separate `shutdownPermit` type with its own seq/live.* Rejected: it would be a second permit system (NFR-001).
- *Encode the action in the live slot value.* Equivalent, but less readable. Rejected.
- *Consume on an action mismatch.* Rejected: a refusal must have no side effects (REBOOT C-3 "refusal runs nothing").

---

### AD-003: Shutdown guards and `handleShutdown` through the runner seam (FR-002, FR-010, FR-016, NFR-006)

**Decision.**

```go
const guardAllowShutdownOff = "commands.allow_shutdown is off"

// commonPowerGuards is the one guard sequence for every power path: shuttingDown, then allowed(), then maintenanceBlockerFor(ownID).
func (a *Agent) commonPowerGuards(allowed func() bool, offText, ownID string) powerGuards
func (a *Agent) commonRebootGuards(ownID string) powerGuards // = commonPowerGuards(func() bool { return a.config.Commands.AllowReboot }, guardAllowRebootOff, ownID)
func (a *Agent) commonShutdownGuards() powerGuards           // = commonPowerGuards(func() bool { return a.config.Commands.AllowShutdown }, guardAllowShutdownOff, "")

func (a *Agent) handleShutdown(cr *cmdRun) commandExecResult
```

The guards return the first failure, in this order:
1. `agent shutting down`, which the gate maps to `skipShutdown`;
2. `commands.allow_shutdown is off`;
3. `host busy: <blocker>`.

The maintenance check is strict (`ownID == ""`, OQ-005), so no run is exempt.

**`handleShutdown(cr)`:**
1. If `!a.config.Commands.AllowShutdown`, return `error` with the **unchanged** message. No probe runs and nothing
   waits (FR-002, AC-009).
2. `c := a.awaitShutdownClearance(cr.ctx, cr.id, a.commonShutdownGuards())`.
3. If `!c.Cleared`, return `shutdownSkippedResult(c)`.
4. If `a.execShutdown(c.Permit)` returns an error, return `error` with `shutdown: <err>`, unchanged (FR-016).
5. Otherwise return `success` with `shutdown initiated after waiting <D> for the dpkg lock` when `c.Waited > 0`, or
   `shutdown initiated`.

`executeAction`'s `case "shutdown":` becomes `return a.handleShutdown(cr)`. The status strings become
`models.ResultStatusError` and `models.ResultStatusSuccess`, which have the same values as the old literals.

**Windows and non-dpkg hosts (FR-006).**
- The probe returns `Absent` on both checks.
- The guards run once. On Windows `maintenanceBlockerFor` returns `""`, because the helper is not present. On a
  non-dpkg Linux host it repeats the check `executeAction` made milliseconds earlier.
- The result is `shutdown initiated` and the argv is unchanged.

The one visible difference is the shutting-down guard. A shutdown that arrives while the agent itself is stopping is
skipped. FR-010 requires this for every shutdown, and reboot behaves the same way on every platform.

**Alternatives considered.**
- *A separate three-line `commonShutdownGuards` body.* Rejected: two copies of the guard order could drift (NFR-001
  says "same wait-and-guard behavior").
- *Exempt the command's own run (`ownID = cr.id`).* Rejected: a shutdown never owns a maintenance run (OQ-005).

---

### AD-004: Shutdown result contract, shared skip mapping (FR-013, FR-014, FR-015, FR-020, OQ-006)

**Decision.** `rebootSkippedResult` is generalized:

```go
func powerSkippedResult(verb string, c powerClearance) commandExecResult // the body of today's rebootSkippedResult, "reboot" replaced by verb
func rebootSkippedResult(c powerClearance) commandExecResult   { return powerSkippedResult("reboot", c) }
func shutdownSkippedResult(c powerClearance) commandExecResult { return powerSkippedResult("shutdown", c) }
```

`<D>` is `models.FormatDurationShort(c.Waited)`. `<holder>` is `holderText()`, which is unchanged.

| FR-013 | Status | Reason | Message |
|---|---|---|---|
| (a) no wait | `success` | `""` | `shutdown initiated` (unchanged) |
| (b) after a wait | `success` | `""` | `shutdown initiated after waiting <D> for the dpkg lock` |
| (c) timeout | `error` | `""` | `shutdown skipped: dpkg lock held past the timeout (10m), <holder>` |
| (d) unknown | `error` | `""` | `shutdown skipped: dpkg lock state unknown: <cause>` |
| (e) agent stopping | `error` | `shutdown` | `shutdown skipped: agent shutting down` |
| (f) guard | `error` | `conflict` for `host busy: …`, else `""` | `shutdown skipped: guard failed: <guard>` |
| exec error | `error` | `""` | `shutdown: <err>` (unchanged) |
| pre-gate `allow_shutdown` false | `error` | `""` | `shutdown not allowed by agent config (set commands.allow_shutdown: true)` (unchanged) |

`<guard>` is one of `commands.allow_shutdown is off`, `host busy: <blocker>`, or `agent command time budget
exhausted`.

**Truthfulness (FR-015).**
- The gate and `execShutdown` run synchronously inside the handler.
- The permit is single-use and action-bound.
- The seen-ID store blocks a redelivery.
- If `cr.ctx` expires, the gate's sleep returns false and the outcome is the truthful (f) skip
  `agent command time budget exhausted`, never a poweroff.

`ReasonTimeout` is never set (FR-020).

**What "initiated" means.** As today and as for reboot, `shutdown initiated` means that `cmd.Start()` (or the runner)
succeeded. It does not mean that `sudo` was authorized. See R-6.

---

### AD-005: Shutdown budget split from reboot (FR-017 to FR-020, BR-004, OQ-003, OQ-004)

**Decision.** New constants in `internal/models/command_policy.go`, in their own `const` block after the reboot block:

```go
ShutdownDpkgLockWaitBound = 10 * time.Minute                           // FR-017; not configurable
ShutdownAgentHardMax      = ShutdownDpkgLockWaitBound + 2*time.Minute  // 12m
ShutdownExecDeadline      = 20 * time.Minute                           // > 12m + DeadlineSlack (17m)
```

In `ExecDeadline` and `AgentHardMax`, the `ClassPower` case becomes a `switch action` with `ActionReboot` returning
the `Reboot*` constants and `ActionShutdown` returning the `Shutdown*` constants. The unreachable class fallback keeps
the old 10 minutes and 1 minute. The doc comments are updated:
- `AgentHardMax`: "reboot 12m, shutdown 12m, …";
- `ExecDeadline`: "reboot and shutdown are 20m because each may wait up to 10m for the dpkg lock";
- the reboot block: "than shutdown, which does not wait" is removed;
- `DpkgLockWaitBound`: "the reboot wait bound; shutdown has its own".

| Budget | `shutdown` before → after | `reboot` (unchanged) | Consumed by |
|---|---|---|---|
| Wait bound | none → **10m** (`ShutdownDpkgLockWaitBound`) | 10m (`DpkgLockWaitBound`) | `shutdownGate()` / `rebootGate()` |
| `AgentHardMax` | 1m → **12m** | 12m | `newCmdRun` `cr.ctx` |
| `ExecDeadline` | 10m → **20m** (`exec_deadline_s` 600 → 1200) | 20m | `service.go:224`; `deadline_at`; sweeper |
| `QueuedTTL` | 15m (unchanged) | 15m | `ExpiresAt`; sweeper `expireQueued` |
| Invariant | 20m > 17m ✓ | 20m > 17m ✓ | `TestCL_DeadlineInvariant` (unchanged) |

**Independence (FR-019, AC-018).**
- No `Shutdown*` constant references `DpkgLockWaitBound`, `RebootAgentHardMax` or `RebootExecDeadline`, and no
  `Reboot*` constant references a `Shutdown*` one. An AST test enforces this (§8 AC-018).
- The values are equal at launch (BR-004), so no test may assert that they differ.

**Why 12m and 20m (OQ-004).**
- The worst-case gate time is about 10m07s: a 10-minute bound, plus at most one probe at the bound, plus one guard
  evaluation of ≤ 5 s and one final probe after a last free observation. The 12-minute budget leaves about 1m50s for
  intake, the poweroff start and the result.
- 20m exceeds 12m + 5m.
- Matching reboot keeps one mental model for the operator and the docs. Nothing argues for different values.

**TTL unchanged.** A longer TTL would let a stale shutdown fire later (REBOOT-DPKG-001 AD-004).

**Sweeper.** No code change. A new shutdown row with no result times out at about 20–21 minutes, with
`no result from agent within 20m`.

| Outcome | Agent | Server row |
|---|---|---|
| Started, no wait | (a) within ms | `succeeded` |
| Started after 9m50s (AC-016) | (b) at about 9m50s | `succeeded` at about 9m50s |
| Timeout | (c) at 10m + ≤ 7 s | `failed` |
| Unknown / guard | (d) / (f) | `failed` |
| Agent stopping | (e) within ≤ 3 s (≤ 5 s + one probe during guards), durable in the outbox | `failed`, reason `shutdown` |

**Alternatives considered.**
- *Reuse `DpkgLockWaitBound` for shutdown.* Rejected by FR-017 and FR-019.
- *`ShutdownAgentHardMax = RebootAgentHardMax`.* Rejected by AC-018: no cross-family derivation.
- *A shorter shutdown bound.* Not requested. OQ-001 fixes 10 minutes.

**Consequences.**
- A lost shutdown result is detected after 20 minutes instead of 10.
- A hard-killed agent mid-wait leaves the host-keyed server row blocking host-level creation until that sweep.
- Version skew: R-5.

---

### AD-006: Call-site enforcement extended to `execShutdown` and to the gate entry points (C-3, C-8, FR-022, AC-020)

**Decision.** The scanner in `reboot_callsites_test.go` is generalized. Every existing assertion is kept.

```go
type execSpec struct {
    Exec    string   // "execReboot" | "execShutdown"
    Gate    string   // "awaitRebootClearance" | "awaitShutdownClearance"
    Callers []string // rebootCallers | {"handleShutdown"}
    Allow   string   // "Commands.AllowReboot" | "Commands.AllowShutdown"
}
var rebootSpec   = execSpec{"execReboot", "awaitRebootClearance", rebootCallers, "Commands.AllowReboot"}
var shutdownSpec = execSpec{"execShutdown", "awaitShutdownClearance", []string{"handleShutdown"}, "Commands.AllowShutdown"}

func scanExecRefs(t *testing.T, filename string, src []byte, spec execSpec) []execRef   // was scanExecRebootRefs (rebootSpec reproduces it exactly)
func gateEvidence(fn *ast.FuncDecl, gate, name string, pos token.Pos) (assigned, checked bool)
```

Every reference to `spec.Exec` in non-test files must pass all of these checks:
1. It is the `Fun` of a call. Method values and method expressions are flagged.
2. Its enclosing function is in `spec.Callers`.
3. The body text before it contains `spec.Allow`.
4. Its single argument is `<ident>.Permit`, where `<ident>` is assigned from `a.<spec.Gate>(` earlier in the same
   function, and `if !<ident>.Cleared` precedes the call.

For shutdown that means exactly **one** reference, in `handleShutdown`. Check 4 with `spec.Gate` is also what makes
a reboot clearance passed to `execShutdown` fail at the call site, and the reverse.

**New structural rules** (`TestPowerCallSites_SD_C3_GateEntryPointsBindTheAction`). These replace and extend
`TestRebootCallSites_RD_C3_PermitMintedOnlyByGate`:
- Exactly one `powerPermit` composite literal, and it is inside `mintPowerPermit`.
- Exactly one `mintPowerPermit` call, and it is inside `awaitPowerClearance`.
- Exactly two `awaitPowerClearance` calls: one in `awaitRebootClearance`, whose argument is a call to `rebootGate()`,
  and one in `awaitShutdownClearance`, whose argument is a call to `shutdownGate()`.
- Exactly two `powerGate` composite literals: one in `rebootGate`, one in `shutdownGate`.
- `consumePowerPermit` is called only from `execReboot` and `execShutdown`.

**Poweroff argv** (`TestShutdownCallSites_SD_AC020_PoweroffArgvOnlyInExecShutdown`). Across the non-test files of
`internal/agent`:
- the string literal `"poweroff"` appears only inside `execShutdown`;
- the literal `"/s"` appears only inside `execShutdown`.

This makes "the `shutdown` command is the only poweroff path" structural (FR-022).

**Retired:** `TestRebootCallSites_RD_AC020_ShutdownHasNoGate`. Its premise ("shutdown gets no dpkg wait") is the
behaviour this story reverses.

---

### AD-007: Bounded growth (NFR-003, house rule)

| Item | Bound per `shutdown` command |
|---|---|
| Waits | ≤ 1, with one deadline `firstHeld + 10m`. A skip is never retried (FR-008) |
| Lock checks | ≤ 10m / 2s + 3 = **303** probes, each ≤ 1 MiB + 1 byte of `/proc/locks`, ≤ 8 KiB per small read, ≤ 1 s |
| Memory | O(1): the probe's 64 KiB buffer, a 4 KiB line buffer and one status; the gate holds 4 locals |
| Guard evaluations | only on a free observation, ≤ 1 per poll; ≤ 1 `systemctl show` each, capped at 5 s |
| Info-or-above lines from the gate | ≤ 2 (`dpkg lock held, waiting before shutdown`, `dpkg lock wait ended`), only when it waited; none per check; nothing at debug per check |
| Command results | exactly 1, through the existing outbox, which is already bounded (200 entries, 1 MiB) |
| Events / telemetry triggers | **0** new. The shutdown path has no event and no telemetry call; the existing failed-command lifecycle event is unchanged |
| Permits | ≤ 1 minted, consumed at most once. One live slot per agent across both actions |
| Concurrent shutdown waits per agent | ≤ 1 (the host key, FR-023) |
| Persistent state | **none.** No file, row, column or migration. The inflight marker and the seen ID are the existing per-command entries, bounded by `SeenMaxEntries` / `SeenMaxAge` |

**Failure paths:**
- timeout, unknown, guard, agent stop and budget expiry each end with one result, release the host key in
  `runAction`, and leave nothing behind;
- a poweroff start failure gives one `shutdown: <err>` result;
- a hard kill of the agent leaves its marker, which recovery clears without a result; the server sweeps the row at
  20 minutes.

Nothing grows with the length of the wait or the number of failures.

---

### AD-008: Reboot invariance: what changes in reboot code, and the exact test edits (FR-021, AC-019)

**Reboot production code that changes, and why the output is byte-identical:**

| Change | Why reboot output is unchanged |
|---|---|
| The gate loop moves into `awaitPowerClearance`; `awaitRebootClearance` keeps its signature and calls it with `rebootGate()` | `rebootGate()` = `{powerActionReboot, models.DpkgLockWaitBound, "dpkg lock held, waiting before reboot"}`: the same bound, the same literal message, the same attributes. The loop is otherwise moved verbatim (AD-001) |
| `reason()` reads `c.Bound` | The reboot gate sets `Bound = DpkgLockWaitBound = 10m`, so the text is `(10m)` as before. The post-patch event texts are built from `reason()` and stay identical |
| `rebootPermit` → `powerPermit{id, action}`; `mintRebootPermit` → `mintPowerPermit(powerActionReboot)`; the fields are renamed | The reboot gate mints with `powerActionReboot`, and `execReboot` accepts exactly those. Single use, staleness and refusal are as before |
| `execReboot`'s first line → `consumePowerPermit(p, powerActionReboot)` | The same CAS, plus an action check that every reboot-gate permit passes. The same `errRebootNotCleared` |
| `commonRebootGuards` delegates to `commonPowerGuards` | The same order, the same texts (`agent shutting down`, `commands.allow_reboot is off`, `host busy: …`), the same `ownID` |
| `rebootSkippedResult` delegates to `powerSkippedResult("reboot", c)` | The same mapping and strings, with `"reboot"` substituted back |
| `rebootGuards` → `powerGuards`, `rebootClearance` → `powerClearance` | Renames only |

**Unchanged:** the three reboot call sites (`handleReboot`, `maybeRebootAfterPatch`, `rebootAfterDelivery`), their
`AllowReboot` checks, `abortPostPatchReboot` and `abortAuthorizedReboot`, the reboot argv, the reboot budget
constants and branches, and every reboot log message.

**No reboot-visible string changes.**

**Test edits.** The list is exhaustive. No reboot assertion or expected string changes.

| File | Edit | Kind |
|---|---|---|
| `reboot_clearance_test.go` | L182 `clearanceFor(guards rebootGuards) rebootClearance` → `powerGuards` / `powerClearance` | rename |
| `reboot_clearance_test.go` | L857, L969 `rebootPermitSeq` → `powerPermitSeq` | rename |
| `reboot_clearance_test.go` | L923, L977 `rebootPermit{}` → `powerPermit{}` | rename |
| `reboot_clearance_test.go` | L930 `rebootClearance{Skip: skipLockTimeout}` → `powerClearance{…}` | rename |
| `reboot_clearance_test.go` | L939, L949, L950 `r.a.mintRebootPermit()` → `r.a.mintPowerPermit(powerActionReboot)` | rename |
| `reboot_clearance_test.go` | L990–L996 `rebootClearance` → `powerClearance`; the timeout row gains `Bound: models.DpkgLockWaitBound` (the expected text `(10m)` is unchanged) | rename + fixture field |
| `reboot_clearance_qa_test.go` | L68 `rebootPermitSeq` → `powerPermitSeq` | rename |
| `reboot_clearance_qa_test.go` | `TestCommandPolicy_QA_AC020_RebootAndShutdownBudgetsAreIndependent`: delete L110 (the `NotEqual` precondition, false by BR-004) and the shutdown lines L111, L112, L116; keep the reboot lines L113–L115 | pinned old shutdown values |
| `reboot_callsites_test.go` | Scanner generalized with `execSpec` (AD-006); `rebootSpec` reproduces the old behaviour; C-8, AC-020 and C-3 self-test assertions and messages unchanged (the synthetic sources keep `rebootPermit{}`; they are only parsed) | refactor |
| `reboot_callsites_test.go` | `TestRebootCallSites_RD_C3_PermitMintedOnlyByGate` → identifiers `powerPermit` / `mintPowerPermit` / `awaitPowerClearance` (the same counts: 1 and 1); extended by AD-006 | rename |
| `reboot_callsites_test.go` | `TestRebootCallSites_RD_AC020_ShutdownHasNoGate` deleted | pinned old shutdown behaviour |
| `models/command_policy_test.go` | `TestCL_AC029_ExecDeadlineTable` shutdown row 10m → 20m; `TestCommandPolicy_RD_AC020_ShutdownBudgetUnchanged` deleted (its TTL and class checks move into `TestCommandPolicy_SD_AC017_*`) | pinned old shutdown values |
| `server/commands/service_test.go` | L211 `{"shutdown", nil, 15m, 600}` → `1200` | pinned old shutdown value |
| `server/commands/sweeper_test.go` | Subtest "shutdown keeps the 10m deadline" (L248–L260) moved out of `TestSweep_RD_AC017_*` and rewritten as `TestSweep_SD_AC016_*` | pinned old shutdown value |

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| MODIFY | `internal/agent/reboot_clearance.go` | Header comment (reboot **and** shutdown). `powerAction`, `powerGate`, `rebootGate()`, `shutdownGate()`. `powerGuards`, `powerClearance` with `Bound`, and `reason()` reading `c.Bound`. `awaitPowerClearance` (the moved loop with the four AD-001 substitutions), `awaitRebootClearance` and `awaitShutdownClearance` as wrappers. `powerPermit`, `mintPowerPermit`, `consumePowerPermit`, `errShutdownNotCleared`. `guardAllowShutdownOff`. `commonPowerGuards`, `commonRebootGuards` (delegating), `commonShutdownGuards` |
| MODIFY | `internal/agent/commands.go` | `executeAction` `case "shutdown": return a.handleShutdown(cr)`. `handleShutdown(cr *cmdRun) commandExecResult` (AD-003). `powerSkippedResult`, `rebootSkippedResult` (delegating), `shutdownSkippedResult`. `execReboot(p powerPermit)` through `consumePowerPermit`. New `execShutdown(p powerPermit) error`, which holds the only poweroff argv and honours `a.runner`. Doc comments: execReboot ("power permit"), handleShutdown |
| MODIFY | `internal/agent/agent.go` | `rebootPermitSeq` / `rebootPermitLive` → `powerPermitSeq` / `powerPermitLive`; comment updated |
| MODIFY | `internal/models/command_policy.go` | `ShutdownDpkgLockWaitBound`, `ShutdownAgentHardMax`, `ShutdownExecDeadline`; `ClassPower` `switch action` in `ExecDeadline` and `AgentHardMax`; doc comments (AD-005) |
| MODIFY | `internal/models/command_policy_test.go` | AC029 shutdown row; delete `TestCommandPolicy_RD_AC020_ShutdownBudgetUnchanged`; add the SD tests of §8 (AC-017, AC-018) |
| MODIFY | `internal/agent/reboot_clearance_test.go` | Mechanical renames only (AD-008) |
| MODIFY | `internal/agent/reboot_clearance_qa_test.go` | AD-008 rows |
| MODIFY | `internal/agent/reboot_callsites_test.go` | `execSpec` generalization; delete `…_ShutdownHasNoGate`; add the AD-006 shutdown, entry-point and argv tests (§8 AC-008, AC-020) |
| CREATE | `internal/agent/shutdown_clearance_test.go` | `shutdownRig` (§12 note 4) and every `TestShutdown_SD_*` / `TestShutdownClearance_SD_*` / `TestPowerClearance_SD_*` / `TestRebootGate_SD_*` test of §8 |
| MODIFY | `internal/server/commands/service_test.go` | Shutdown `dl` 600 → 1200 |
| MODIFY | `internal/server/commands/sweeper_test.go` | Move the shutdown subtest; add `TestSweep_SD_AC016_ShutdownNotTimedOutBefore20m` |

**Explicitly unchanged:**
- `internal/agent/dpkglock*.go` (the probe), `reboot_auth.go`, `command_lifecycle.go` (it reads `AgentHardMax`
  through models), `maintenance*.go`, `doctor.go`;
- `internal/server/**` non-test code and `internal/server/db/*`; all migrations;
- `scripts/install.sh`, `scripts/riot-agent.service`, and sudoers;
- `web/` (DeviceDetail power menu and confirm dialog);
- `go.mod` / `go.sum`.

**Technical writer (post-QA):**
- **README, section "Reboots and the dpkg lock"** (L2645–L2762). Keep the heading so that the anchor
  `#reboots-and-the-dpkg-lock`, which L2170, L2532 and the CHANGELOG link to, stays valid. Changes:
  - L2647 upgrade note: add that a new agent can also wait up to 10 minutes before a shutdown, and that an old
    server gives a shutdown 10 minutes in total. The server-first rule covers both.
  - L2649: "Every reboot **and every shutdown** the agent starts waits …".
  - L2660, table "Which reboots wait": the Shutdown row becomes "Yes". Rename the table to "Which power actions wait".
  - "How the wait works" step 3: for a shutdown, the agent re-checks `commands.allow_shutdown` in place of
    `commands.allow_reboot`.
  - L2671: a waiting shutdown also holds the host's command slot.
  - "What you see": add a **Shutdown** results table with the AD-004 rows. `<guard>` for shutdown is
    `commands.allow_shutdown is off`, `host busy: …` or `agent command time budget exhausted`. The pre-wait refusal
    keeps `shutdown not allowed by agent config (set commands.allow_shutdown: true)`.
  - "Agent log": add the message `dpkg lock held, waiting before shutdown` and the `path` value `shutdown`.
  - "Time limits": replace L2747 with shutdown rows (wait 10 minutes, agent 12 minutes, server 20 minutes,
    `exec_deadline_s` 1200, queued 15 minutes unchanged). State that the two sets of limits are independent. A
    shutdown created before the server upgrade keeps `600`.
  - "Local users can block reboots": becomes "… reboots and shutdowns".
  - "Known limits": delete L2761 "Shutdown is not gated." Add: a waiting shutdown cannot be cancelled from the
    dashboard; it can power the host off up to 10 minutes after it was sent, and the host then stays off until someone
    powers it on.
- **README power budget table L2171:** `Power: shutdown | shutdown | 15 minutes | 20 minutes (a shutdown can wait up
  to 10 minutes for the dpkg lock, see [Reboots and the dpkg lock](#reboots-and-the-dpkg-lock))`.
- **`web/src/pages/LearnMore.tsx` L432–L433:** "rIOt reboots and shutdowns wait up to 10 minutes for a running
  package manager … If it is still running, or rIOt cannot tell, the reboot or shutdown is skipped and reported. rIOt
  never forces it." This is help text, not a power menu or dialog change (FR-025 holds).
- **CHANGELOG `[Unreleased]`:**
  - **Added:** `[SHUTDOWN-DPKG-001]` the shutdown wait, its outcomes and messages, and that non-dpkg and Windows hosts
    are unchanged.
  - **Changed:** the shutdown server deadline is 20 minutes (`exec_deadline_s` 1200, was 600) and the agent budget
    12 minutes (was 1 minute). The queued expiry stays 15 minutes.
  - **Upgrade notes:** `[SHUTDOWN-DPKG-001]` **Upgrade the server before the agents**. Mirror the REBOOT-DPKG-001
    note: after a shutdown shows `timed_out`, check whether the host is off before sending another.
  - **Conditional:** if REBOOT-DPKG-001 is not yet in a tag (`git tag --contains 09c0ecf` prints nothing), amend its
    two `[Unreleased]` clauses "`shutdown` is not gated" (L14) and "`shutdown` keeps 10 minutes and 1 minute" (L38)
    to refer to the SHUTDOWN-DPKG-001 entry. If it is tagged, leave them and log them under stale content.
- **Stale content to log, not edit:**
  - REBOOT-DPKG-001 ADD:
    - §1 "`shutdown` keeps 1 min and 10 min";
    - the AD-004 table's `shutdown` column;
    - §4 "Explicitly unchanged: `handleShutdown`";
    - §8 AC-020 (`…_ShutdownHasNoGate`, `TestCommandPolicy_RD_AC020_ShutdownBudgetUnchanged`);
    - §12 note 10 "Never call `handleShutdown` in tests";
    - the §13 DoD line `AgentHardMax("shutdown",1) == time.Minute …`;
    - the DoD grep `rebootPermit{` (now `powerPermit{`).
  - REBOOT-DPKG-001 FRD FR-020.
  - REBOOT-DPKG-001 security review INFO-4 ("`shutdown` keeps 1m/10m and gets no gate").
  - CMD-LIFECYCLE-001 ADD AD-005 / AD-013 power budgets.
  - The README Sudoers Rules table `sudo systemctl poweroff` row. It is already logged by the AGENT-HARDEN-001 and
    REG-HARDEN-001 docs reports; see R-6.

## 5. Data Model Changes

**Postgres: none.** No migration, table, column or index. New `shutdown` rows store `exec_deadline_s = 1200`. Rows
created before the upgrade keep `600`.

**Models (code only):** three constants and two `switch action` branches (AD-005).

**Agent on-disk state: none new.** The probe is unchanged and writes nothing.

**In-memory state** (never persisted):
- per attempt: the gate's `firstHeld`, `waiting`, `sawHeld`, `guardsPassed` and `holder`;
- per agent: `powerPermitSeq` and `powerPermitLive`, renamed from the reboot fields, not added.

**Before → after (agent types):**

| Before | After |
|---|---|
| `rebootPermit{id uint64}` | `powerPermit{id uint64; action powerAction}` |
| `rebootClearance{Cleared, Permit, Waited, Skip, Detail}` | `powerClearance{Cleared, Permit, Waited, Skip, Detail, Bound}` |
| `rebootGuards` | `powerGuards` |
| `Agent.rebootPermitSeq`, `Agent.rebootPermitLive` | `Agent.powerPermitSeq`, `Agent.powerPermitLive` |
| none | `powerAction`, `powerGate` |

## 6. API / Interface Contract

No new or changed endpoint. The command result shape is unchanged (NFR-005). Only the message text and, for (e) and
(f), the existing `reason` values change.

### Command record (existing)

| Field | Change |
|---|---|
| `exec_deadline_s` | new `shutdown` rows `1200` (was `600`); `reboot` stays `1200` |
| `deadline_at` | `dispatched_at + 1200 s` for new `shutdown` rows |
| `expires_at` | unchanged (`+15m`) |
| `result_msg`, `status`, `outcome`, `reason` | the AD-004 table |

### Agent command result for `shutdown` (existing shape)

```
shutdown initiated
shutdown initiated after waiting <D> for the dpkg lock
shutdown skipped: dpkg lock held past the timeout (10m), <holderText>
shutdown skipped: dpkg lock state unknown: <cause>
shutdown skipped: agent shutting down                      (reason "shutdown")
shutdown skipped: guard failed: <guard text>               (reason "conflict" for "host busy: …")
shutdown: <err>
shutdown not allowed by agent config (set commands.allow_shutdown: true)
```

`<holderText>` and `<cause>` are exactly the REBOOT-DPKG-001 §6 grammar and cause list. Causes are ≤ 200 bytes, and
holder names are limited to `[A-Za-z0-9._+:-]`.

### Agent log (existing format)

```
INFO dpkg lock held, waiting before shutdown  path=shutdown command_id=<uuid> holder="<holderText>"
INFO dpkg lock wait ended                     path=shutdown command_id=<uuid> outcome=<cleared|lock_timeout|lock_unknown|shutdown|guard> waited=<d> holder="<holderText>"
```

### Agent events

None new.

## 7. Sequence / Flow

### 7.1 Shutdown, lock held 3 minutes (AC-003)

1. The server creates the row with `exec_deadline_s = 1200` and dispatches it at T0, so `deadline_at = T0+20m`.
2. Agent intake: the seen-ID check, the inflight marker, and `cr.ctx` with a 12-minute timeout. `executeAction` runs
   the maintenance precheck and takes the host lock.
3. `handleShutdown(cr)`: `AllowShutdown` passes, then `awaitShutdownClearance(cr.ctx, cr.id, commonShutdownGuards())`.
4. The probe returns `Held` (`held by unattended-upgr (pid 1234, uid 0)`). The gate writes one Info line, `dpkg lock
   held, waiting before shutdown`.
5. Every 2 s: check `shuttingDown`, probe, `Held`, sleep.
6. At T0+3m the probe returns `Free`. The guards run (`shuttingDown`, `allow_shutdown`, strict maintenance) and pass.
   The final probe returns `Free`. The gate calls `mintPowerPermit(powerActionShutdown)` and returns Cleared with
   Waited 3m. One Info line, `dpkg lock wait ended … outcome=cleared waited=3m0s`.
7. `execShutdown(c.Permit)` consumes the shutdown permit and starts `sudo systemctl poweroff`. The handler returns
   `success, "shutdown initiated after waiting 3m for the dpkg lock"`, and the lifecycle queues it in the outbox.
8. `runAction` releases the host lock. The host powers off.

### 7.2 Shutdown, timeout

At `firstHeld + 10m` the lock is still `Held`. The result is `error, "shutdown skipped: dpkg lock held past the timeout
(10m), held by unattended-upgr (pid 1234, uid 0)"`. No permit is minted, the host key is released, and nothing retries.

### 7.3 `allow_shutdown` turned off during the wait (AC-010)

1. The lock is held, the gate waits, and config changes so `AllowShutdown = false`.
2. The lock is released. The guards return `commands.allow_shutdown is off`, so the gate skips with `skipGuard`.
3. The result is `error, "shutdown skipped: guard failed: commands.allow_shutdown is off"`, with reason `""`.

### 7.4 Agent stop during the wait (AC-013)

1. SIGTERM sets `shuttingDown`. Within one poll (≤ 3 s), the gate returns `skipShutdown`.
2. The result is `error, reason shutdown, "shutdown skipped: agent shutting down"`, written to the durable outbox.
3. `waitForCommands(20s)` sees the command end. After the restart, recovery clears the marker and does not re-run it
   (AC-022).

### 7.5 Lock files deleted during a wait (AC-007)

The probe finds `/var/lib/dpkg/status`, so it returns `Unknown` and the skip is immediate. If the markers have also
gone, the probe returns `Absent`, and the `sawHeld` rule still skips with `dpkg lock files disappeared during the
wait`.

### 7.6 Windows or a non-dpkg Linux host (AC-008)

The sequence is probe `Absent`, guards (pass), probe `Absent`, clear, then `execShutdown`. The result is `shutdown
initiated` within milliseconds. The gate writes no log line.

### 7.7 Version skew: new agent, old server (R-5)

1. The old server stores `exec_deadline_s = 600`.
2. The lock is held from T0 and is still held at T0+10m.
3. The sweeper marks the row `timed_out` (`no result from agent within 10m`). A few seconds later the agent reports
   the (c) skip, and the server rejects that late result.
4. If the lock frees in the last seconds before the old deadline, the host powers off while the row may show
   `timed_out`.

## 8. Acceptance Criteria Mapping

**Naming.**
- Go test names contain `SD_ACnnn`, or `SD_Cn` for security conditions.
- Assertion messages start with `[AC-nnn]` or `[C-n]`.
- Shutdown tests use `shutdownRig` (§12 note 4): the lifecycle harness, `fakeClock`, a `pause` that advances it, a
  scripted `dpkgLockProbe`, `AllowShutdown = true`, and a runner that records `sudo systemctl poweroff` separately
  from reboots.
- Unless a row says otherwise, tests are in `internal/agent/shutdown_clearance_test.go`.

| AC | Fulfilled By | Tests |
|---|---|---|
| AC-001 | AD-001 free branch; AD-003; AD-004 (a) | `TestShutdown_SD_AC001_FreeLockPowersOffWithNoWait` (result `success` / `shutdown initiated`, 1 poweroff, 2 probes, 0 gate pauses); `TestShutdownClearance_SD_AC001_FreeRunsGuardsThenFinalProbe` (sequence probe → guards → probe → execShutdown) |
| AC-002 | AD-001 held branch | `TestShutdown_SD_AC002_HeldLockNeverPowersOffWithinBound` (the probe script asserts 0 poweroffs at every probe up to 9m58s, then releases) |
| AC-003 | AD-001; AD-004 (b) | `TestShutdown_SD_AC003_ReleaseAfter3mPowersOffWithin10s` (poweroff ≤ 10 s after the release; message `shutdown initiated after waiting 3m for the dpkg lock`) |
| AC-004 | AD-001 timeout; FR-008 | `TestShutdown_SD_AC004_TimeoutSkipsAndNeverForces` (0 poweroffs; `sinceFirstProbe == ShutdownDpkgLockWaitBound`); `TestShutdown_SD_AC004_NoSecondWaitAfterSkip` (advance 1h plus a redelivery: no new probe, no poweroff) |
| AC-005 | AD-004 (c); `holderText` | `TestShutdown_SD_AC005_TimeoutResultFailedAndNamesHolder` (exact text `shutdown skipped: dpkg lock held past the timeout (10m), held by unattended-upgr (pid 1234, uid 0)`; `error`; reason ≠ `timeout`; no `initiated`) |
| AC-006 | `holderText` | `TestShutdown_SD_AC006_TimeoutReportSaysHolderNotVisible` (PID −1 → `… (10m), holder not visible`) |
| AC-007 | AD-001 unknown and `sawHeld`; C-1 | `TestShutdown_SD_AC007_UnknownSkipsImmediatelyWithoutWait` (cause in the text, 0 pauses, 1 probe); `TestShutdown_SD_AC007_HeldThenAbsentNeverPowersOff` (`dpkg lock files disappeared during the wait`); `TestShutdown_SD_AC007_ZeroStatusSkipsAsUnknown` (`lock state not recognized`) |
| AC-008 | AD-003 Windows/non-dpkg; `dpkglock_other.go` | `TestShutdown_SD_AC008_AbsentPowersOffAsToday` (message exactly `shutdown initiated`, no `dpkg` substring, 0 pauses, no wait log line); `TestShutdown_SD_AC008_RunnerGetsUnchangedArgv` (`sudo systemctl poweroff`); `TestShutdownCallSites_SD_AC008_WindowsArgvUnchanged` in `reboot_callsites_test.go` (AST: `execShutdown` holds the literals `"shutdown"`, `"/s"`, `"/t"`, `"5"`); `TestPlatformProbe_RD_AC012_NonLinuxIsAbsent` (existing) |
| AC-009 | AD-003 step 1 | `TestShutdown_SD_AC009_AllowShutdownOffRefusedWithoutLockCheck` (the unchanged message; 0 probes; 0 poweroffs) |
| AC-010 | AD-003 guards | `TestShutdown_SD_AC010_AllowShutdownOffDuringWaitSkipsNamingGuard` (`shutdown skipped: guard failed: commands.allow_shutdown is off`); `TestShutdownClearance_SD_AC010_GuardOrderIsShutdownThenAllowShutdownThenMaintenance` |
| AC-011 | AD-003 strict maintenance | `TestShutdown_SD_AC011_MaintenanceStartsDuringWaitSkipsNamingGuard` (`… guard failed: host busy: …`, reason `conflict`; uses `maintenanceFlip`) |
| AC-012 | AD-001 (FR-011) | `TestShutdown_SD_AC012_FinalCheckRetakenKeepsWaitingWithinBound` (`Waited == 10m`; exactly one `msg="dpkg lock held, waiting before shutdown"`) |
| AC-013 | AD-001 `shuttingDown`; AD-004 (e) | `TestShutdown_SD_AC013_AgentStopEndsWaitWithin5s`; `TestShutdown_SD_AC013_ShutdownResultReasonShutdown` (`shutdown skipped: agent shutting down`, reason `shutdown`) |
| AC-014 | AD-002; AD-004 truthfulness | `TestShutdown_SD_AC014_NoPoweroffAfterSkippedResult` (redelivery after release: 0 poweroffs, 0 probes); `TestShutdown_SD_AC014_CommandBudgetExhaustedIsATruthfulSkip` (pause returns false → `shutdown skipped: guard failed: agent command time budget exhausted`, 0 poweroffs) |
| AC-015 | AD-003 step 4 | `TestShutdown_SD_AC015_PoweroffStartFailureIsFailedResult` (the runner errors → `error`, `shutdown: <err>`) |
| AC-016 | AD-004 (b); AD-005 | `TestShutdown_SD_AC016_NineMinutesFiftyWaitReportsStarted` (`shutdown initiated after waiting 9m50s for the dpkg lock`, `success`, reason ≠ `timeout`); `TestSweep_SD_AC016_ShutdownNotTimedOutBefore20m` in `server/commands/sweeper_test.go` (running at 11m and 19m; `timed_out` with `no result from agent within 20m` at 20m) |
| AC-017 | AD-005 | `TestCommandPolicy_SD_AC017_ShutdownBudgetCoversWait` in `models/command_policy_test.go` (`ShutdownDpkgLockWaitBound == 10m`; `AgentHardMax(shutdown) == 12m > bound`; `ExecDeadline(shutdown) == 20m`; seconds `1200`; deadline > agent max + slack; `QueuedTTL(reboot, shutdown) == 15m`; `ActionClass == power`); the updated `TestCL_AC029_ExecDeadlineTable` and `TestCL_AC029_ExpiresAtPerActionClass`; `TestCL_DeadlineInvariant` (unchanged) |
| AC-018 | AD-001 `Bound`; AD-005 independence | `TestPowerClearance_SD_AC018_EachGateSkipsAtItsOwnBound` (`awaitPowerClearance` with a shutdown spec bound of 3m and a reboot spec bound of 7m: each skips at exactly its bound and `reason()` says `(3m)` / `(7m)`); `TestShutdownClearance_SD_AC018_ShutdownGateUsesShutdownBound` (`shutdownGate() == {powerActionShutdown, models.ShutdownDpkgLockWaitBound, "dpkg lock held, waiting before shutdown"}`); `TestCommandPolicy_SD_AC018_BudgetConstantsDoNotReferenceEachOther` (AST of `command_policy.go`: the `Shutdown*` const specs reference no `Reboot*` / `DpkgLockWaitBound` identifier, and the reverse) |
| AC-019 | AD-008 | Every existing `*_RD_*`, `*_QA_*` and `*_PF_C8_*` agent test, after the AD-008 edits only; `TestRebootGate_SD_AC019_RebootSpecUnchanged` (`rebootGate() == {powerActionReboot, models.DpkgLockWaitBound, "dpkg lock held, waiting before reboot"}`; reboot budgets 10m / 12m / 20m); `TestRebootClearance_RD_AC019_WaitLogLinesCarryContext` (existing; pins the reboot log text) |
| AC-020 | AD-002; AD-006 | `TestShutdownCallSites_SD_AC020_ExecShutdownHasExactlyOneGuardedCallSite`; `TestShutdownCallSites_SD_AC020_GateClearanceFeedsExecShutdown`; `TestShutdownCallSites_SD_AC020_PoweroffArgvOnlyInExecShutdown`; `TestShutdownCallSites_SD_C3_SelfTest` (synthetic: a good shape passes; a method value, a discarded gate, an unchecked `Cleared`, a second hidden call, and **a reboot clearance passed to `execShutdown`** are each flagged); `TestPowerCallSites_SD_C3_GateEntryPointsBindTheAction`; `TestRebootAuth_PF_C8_ExecRebootHasExactlyThreeGuardedCallSites` and `TestRebootCallSites_RD_AC020_GateClearanceFeedsEveryExecReboot` (unchanged assertions). All in `reboot_callsites_test.go` |
| AC-021 | FR-023 host key (existing) | `TestShutdown_SD_AC021_SecondShutdownOrRebootDuringWaitIsHostBusy` (the probe script submits `idB` shutdown and `idC` reboot at 1m: each gets a `conflict` result; probe calls come only from `idA`; one wait-started line) |
| AC-022 | `recoverInterruptedCommands` (existing) | `TestShutdown_SD_AC022_RestartDoesNotRerunShutdown` (a shutdown marker, then a new harness over the same dir: no probe, no poweroff, no result); `TestCL_AC022_SelfUpdateMarkerNotReported` (existing, shutdown row) |
| AC-023 | AD-007 | `TestShutdownClearance_SD_AC023_FullWaitBoundedOutput` (gate level: ≤ 303 probes; `powerPermitSeq == 0`; state dir unchanged; exactly 2 `msg="dpkg lock ` lines, both INFO; 0 DEBUG lines with `dpkg`); `TestShutdown_SD_AC023_FullWaitOneResultNoEventNoTelemetry` (command level: 1 result, 0 agent events, `telemetryPending() == 0`) |
| AC-024 | AD-002 argv; probe unchanged | `TestShutdown_SD_AC024_RunsOnlyTheExistingPoweroffArgv` (over a 10 s wait the runner sees only `sudo systemctl poweroff`); `TestDpkgProbe_RD_AC021_NoExecNoSudo` (existing); DoD: no diff under `scripts/`. **Caveat R-6:** the test proves "no new privilege", not that the poweroff is authorized on a host installed by the current installer |
| AC-025 | No web change | DoD `git diff --stat origin/main -- web/src/pages/DeviceDetail.tsx web/src/components` is empty; `cd web && npm run test:run` green (existing DeviceDetail tests) |
| AC-026 | Technical writer (§4) | QA: `grep -n "not gated" README.md web/src/pages/LearnMore.tsx` finds nothing; the README L2171 row shows 20 minutes; CHANGELOG has the SHUTDOWN Upgrade note (and, per the §4 conditional, no "`shutdown` is not gated") |
| C-3 (runtime) | AD-002 | `TestExecShutdown_SD_C3_NonClearedPermitStartsNoPoweroff` (zero permit; a skipped clearance's permit; used twice; stale); `TestExecShutdown_SD_C3_RebootPermitIsRefusedAndNotConsumed` (a permit minted for reboot → `errShutdownNotCleared`, and `execReboot` with it still succeeds once); `TestExecReboot_SD_C3_ShutdownPermitIsRefusedAndNotConsumed` (the mirror case); `TestExecShutdown_SD_C3_RefusalRunsNothing`; `TestExecShutdown_SD_C3_ClearedPermitStartsExactlyOnce` |
| C-1 / C-2 / C-5 | inherited probe | Existing `dpkglock_test.go` tests, unchanged; the AC-005, AC-006 and AC-007 rows above show they reach the shutdown result |

## 9. Error Handling

| Failure | Behavior | Log |
|---|---|---|
| `allow_shutdown` false at start | Refused as today; no probe | none new |
| Not a dpkg host / Windows | `Absent`; poweroff as today | none |
| Any REBOOT-DPKG-001 `Unknown` cause (namespace, markers, `stat`, `/proc/locks` read, cap, line, 1 s) | immediate skip (d) | the existing command-completion lines |
| `Held` then `Absent` | skip (d) `dpkg lock files disappeared during the wait` | `wait ended` Info |
| Unrecognized or zero state | skip (d) `lock state not recognized` | none from the gate |
| Held past 10 minutes | skip (c) with the holder | both gate Info lines |
| Guard fails | skip (f) naming the guard; `conflict` for host busy | `wait ended` Info if it waited |
| Agent stopping | skip (e), reason `shutdown` | `wait ended` Info if it waited |
| `cr.ctx` deadline (12m) | skip (f) `agent command time budget exhausted` | `wait ended` Info |
| `execShutdown` with a zero, stale, used or reboot permit | `errShutdownNotCleared`; nothing runs; result `shutdown: shutdown refused: …` | none |
| `execShutdown` start error | `shutdown: <err>` | none new |
| Agent hard-killed mid-wait | No poweroff; recovery clears the marker without a result; server `timed_out` at 20 minutes | the existing recovery line |

## 10. Security Considerations

- **No privilege added (NFR-002).** The probe is unchanged: `stat`, `readlink` on the agent's own namespace link, and
  world-readable `/proc` reads. The poweroff argv is byte-identical. No `scripts/` change.
- **Fail closed (BR-001).** Every `Unknown`, zero and unrecognized state skips. `Held` then `Absent` skips.
- **The gate can only prevent or delay (BR-003).** The gate runs only after the `AllowShutdown` pre-check, and the
  guard re-checks it. No path mints a shutdown permit without the gate, and nothing retries.
- **Permit hardening (C-3 extended).**
  - The permit type is unexported, and its literal exists only in `mintPowerPermit`.
  - The action-bound runtime check refuses a cross-action permit without consuming it.
  - The single-use CAS refuses a used or stale permit.
  - The per-reference AST check applies to both exec functions, including a cross-action gate check at the call
    site.
  - The entry points bind the gate spec structurally.
  - The poweroff argv exists only in `execShutdown`.
- **Untrusted text (C-5).** The holder text and causes reach only the command result and the agent log, which are
  sanitized as for reboot. The shutdown path sends no event, so no new notifier exposure.
- **REBOOT-DPKG-001 conditions:**
  - C-1, C-2, C-5 and C-6: the probe is untouched, so they are satisfied as before.
  - C-3: extended as above.
  - C-4: the server-first note is extended to shutdown (R-5).
  - C-7: the QA gate is extended in §13.

**Residual risks:**
- **R-1 (NFR-004):** the gap between the final probe and the start of the poweroff is sub-second to a few seconds.
  Accepted, as for reboot.
- **R-2 (pid namespace and hidden `/proc`):** inherited. A containerized agent, or one with `PrivatePIDs=yes` or
  `ProcSubset=pid`, skips **every shutdown** on a dpkg host visibly, as it does every reboot.
  `TestAgentUnit_RD_C1_NoPrivatePIDs` still guards the shipped units.
- **R-3 (inode-only match, unverified on real hardware):** inherited, unchanged.
- **R-4 (local-user veto, extended):** any local user can delay or block rIOt **shutdowns**: a lock flood on any
  filesystem, or an inode collision on major-0 filesystems. Each skip names the pid and uid. This is the price of
  failing closed. **Ryan's call; it mirrors the accepted SEC-002.**
- **R-5 (version skew):** a new agent with an old server (600 s deadline) can be recorded as `timed_out`, with the
  late result rejected (§7.7). If the lock freed near the end, the host still powers off. The agent's automatic
  updater can install the release before the server is upgraded (SEC-004), so the CHANGELOG and README say **upgrade
  the server first**.
  - Unlike a reboot, a re-sent shutdown after a misleading `timed_out` would sit queued for its 15-minute TTL. If
    someone powers the host back on within that TTL, it would power off again.
  - An old agent with a new server behaves as before; a lost result is detected at 20 minutes.
- **R-6 (pre-existing, out of scope): no sudoers rule for poweroff.** `install.sh` grants `systemctl reboot` but not
  `systemctl poweroff`.
  - On a host installed by the current installer, `sudo systemctl poweroff` probably fails for lack of a password
    after `cmd.Start()` has already succeeded, so the result says `shutdown initiated` and the host stays up. This
    pre-dates this story and is unchanged by it.
  - NFR-002 forbids fixing it here. The FRD's phrase "existing sudo rule" holds only on hosts whose sudoers grant it
    some other way.
  - **Recommended follow-up:** a story that either adds the rule through the AGENT-PRIV capability path or makes the
    start failure visible (for example `sudo -n` plus a short wait for the exit status).
  - Clod can read `sudo -l -U riot` on Hawaii to confirm it. That is a read-only check.
- **R-7 (INFO-4 for shutdown):** a waiting shutdown cannot be cancelled from the dashboard. It can power off up to
  10 minutes after the click, and unlike a reboot the host stays off. The confirm dialog does not say so (FR-025,
  OQ-008). The README "Known limits" says so.
- **R-8 (host-busy window):** for up to about 10m07s, other host-keyed agent work on that device gets `conflict`. That
  includes `os_update`, `agent_update`, the automatic agent updater and docker mutations. A hard kill extends the
  server-side block to the 20-minute sweep.
- **R-9 (shared live permit slot):** a shutdown mint invalidates an unused reboot permit, and the reverse. The host
  key prevents any overlap. If an overlap ever happened, the older permit would fail closed (no power action).

## 11. Performance Considerations

- **Common case** (lock free): 2 probes (≤ 4 `stat`, ≤ 2 `readlink`, 2 small reads, ≤ 2 `/proc/locks` reads, each
  usually under 1 ms) and one guard evaluation. The added latency before the poweroff is milliseconds, or up to 5 s
  if `systemctl show` is slow.
- **During a wait:** ≤ 303 probes in O(1) memory, with one 2 s timer at a time.
- **Server:** no code or query change. The sweeper keeps a `running` shutdown row 10 minutes longer.
- **No caching, indexing or rate limiting is required.**

## 12. Implementation Notes for Engineers

1. **Commits:** `[SHUTDOWN-DPKG-001] <type>: <what>`. Order:
   1. models: the constants, branches and policy tests, plus the server test rows;
   2. the gate and permit generalization, plus the AD-008 mechanical test edits. **Reboot tests must be green at the
      end of this commit, before any shutdown wiring**: that proves AC-019 on its own;
   3. `handleShutdown`, `execShutdown`, the guards and the result mapping, plus `shutdown_clearance_test.go`;
   4. the call-site test generalization and the new AST tests.
2. **Keep `reboot_clearance.go` as the file name.** `dpkglock_test.go:787` scans it.
3. **Moving the loop.** Cut and paste `awaitRebootClearance`'s body into `awaitPowerClearance`, then make only the
   four AD-001 substitutions. Review the diff with `git diff --color-moved`. Any other change to the moved lines is a
   defect.
4. **`shutdownRig`** (in `shutdown_clearance_test.go`):
   - It embeds `*gateRig`, from `newGateRig(t)`, and sets `a.config.Commands.AllowShutdown = true`.
   - It wraps `a.runner`. For `sudo systemctl poweroff` it records the fake-clock time, increments `poweroffs`, returns
     `r.poweroffErr`, and does **not** delegate, so `r.reboots` keeps counting only reboots. Everything else is
     delegated.
   - `drive()` submits `models.ActionShutdown` as `idA` and waits.
5. **Safety: never reach `handleShutdown` with `a.runner == nil`.** On a developer's Windows machine that runs
   `shutdown /s /t 5`. On a CI runner with passwordless sudo, it powers off the runner.
   - Every test that submits `shutdown` uses `shutdownRig`.
   - `newShutdownRig` ends with `require.NotNil(t, r.a.runner)`.
   - The REBOOT-DPKG-001 rule "never call `handleShutdown` in tests" becomes "only through `shutdownRig`".
6. **Call shape** (checked by AST): `c := a.awaitShutdownClearance(cr.ctx, cr.id, a.commonShutdownGuards())`, then
   `if !c.Cleared { return shutdownSkippedResult(c) }`, then `a.execShutdown(c.Permit)`. Keep the literal
   `a.config.Commands.AllowShutdown` check as the first statement of `handleShutdown`.
7. **Clocks and sleeps:** only `a.clock().Now()` and `a.sleep` in the gate. The DoD grep is unchanged.
8. **Never use `ReasonTimeout`** in `powerSkippedResult`.
9. **R-6 host check (read-only, optional, for the implementation report).** On Hawaii, `sudo -l -U riot` shows
   whether `systemctl poweroff` is permitted. Record the answer. **Do not run a real shutdown on any host.** Only Ryan
   can trigger one, because a powered-off host needs physical or out-of-band access to come back.
10. **No new dependency.** `go/ast` is already used in the test files.
11. **AC-021 test mechanics.** `submitCommand` runs a command in its own goroutine under `cmdWG`. Submit the second
    and third commands from inside the probe script at elapsed ≥ 1m, then `wait()` for all three. The first is still
    inside the gate, so it holds the host key.

## 13. Definition of Done

- [ ] Every Section 4 change is implemented, and every "explicitly unchanged" path is untouched (`git diff --stat`
      shows nothing under `scripts/`, `web/`, `internal/server/` non-test code, `internal/agent/dpkglock*.go`, the
      migrations or `go.mod`)
- [ ] Every Section 8 test exists with an `SD_ACnnn` / `SD_Cn` name and `[AC-nnn]` / `[C-n]` messages, and passes
- [ ] Every REBOOT-DPKG-001 test passes with only the AD-008 edits (reviewer: `git diff` on the reboot test files shows
      renames, the `Bound` fixture field, and the listed shutdown-pinned deletions only)
- [ ] `go test ./...` (Windows; `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`) and `go test -race ./...`
      (Linux CI) are green; `cd web && npm run test:run` is green
- [ ] `go vet ./...` is clean, and `make build-agent-all` succeeds for every target
- [ ] No migration
- [ ] `grep -rn "powerPermit{" internal/agent --include=*.go` (non-test) matches only inside `mintPowerPermit`;
      `grep -rn "rebootPermit\b" internal/agent --include=*.go` (non-test) matches nothing
- [ ] `grep -rn "poweroff" internal/agent --include=*.go` (non-test) matches only inside `execShutdown`
- [ ] `grep -rn "time.Now()\|time.Sleep" internal/agent/dpkglock*.go internal/agent/reboot_clearance.go` (non-test)
      returns nothing
- [ ] `AgentHardMax("shutdown",1) == 12m`, `ExecDeadline("shutdown",1) == 20m`, `ExecDeadlineSeconds == 1200`,
      `QueuedTTL("shutdown") == 15m`; the reboot values are 12m / 20m / 1200 / 15m, unchanged
- [ ] Every test that submits a `shutdown` uses `shutdownRig`, so the runner is never nil
- [ ] The implementation report records the R-6 read-only `sudo -l -U riot` finding, or "not checked"
- [ ] **QA gate (C-7 extended):**
  - the AC-004, AC-007, AC-009, AC-012, AC-013, AC-014, AC-016, AC-018, AC-019, AC-020 and AC-023 tests pass;
  - the C-3 runtime and AST shutdown tests pass;
  - the unchanged C-8 test passes;
  - the DoD greps are clean.
