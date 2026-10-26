# Architecture Decision Document

- **Story ID:** SHUTDOWN-DPKG-001
- **FRD Reference:** `docs/requirements/SHUTDOWN-DPKG-001-frd.md` (revision 2)
- **Security review:** `docs/security/SHUTDOWN-DPKG-001-security-review.md` (APPROVED WITH CONDITIONS C-1 to C-6)
- **Blockers / architect questions:** `docs/architecture/SHUTDOWN-DPKG-001-blockers.md` (resolved by revision 3)
- **QA report:** `docs/qa/SHUTDOWN-DPKG-001-qa-report.md`
- **Reuses:** REBOOT-DPKG-001 (`docs/architecture/REBOOT-DPKG-001-add.md`, FINAL revision 3; security review
  `docs/security/REBOOT-DPKG-001-security-review.md`, conditions C-1 to C-7)
- **Amends:** the power-class budgets of CMD-LIFECYCLE-001 AD-005 / AD-013 and REBOOT-DPKG-001 AD-004, for `shutdown`
  only (AD-005)
- **Follow-ups:**
  - SHUTDOWN-SUDO-001: a poweroff privilege through the AGENT-PRIV capability path;
  - an unnumbered follow-up: a dashboard cancel and progress display for a waiting shutdown (NFR-007).
- **Author:** Architect Agent
- **Date:** 2026-10-25
- **Status:** FINAL, revision 3 (implementation questions and a QA note resolved)

## Changelog

### Revision 3: implementation questions (senior_dev blockers file) and QA note 3

| Item | Source | Decision |
|---|---|---|
| AQ-1: C-4(b) forbids the poweroff text placed in `poweroff.go` | blockers file AQ-1 | **Accepted as implemented.** `errPoweroffStillRunning` is removed from `poweroff.go`. `execShutdown`, which C-4 exempts, builds the error itself, and the result text is unchanged: `shutdown: a previous poweroff command is still running`. C-4 rule (b) stays strict, with no exemption for `poweroff.go`. The fallback text in `classifyPoweroff` for an unclassified launch now reads `shutdown command did not start`. §4 and AD-009 are updated |
| AQ-2: the reaper Warn has no command id | blockers file AQ-2 | **Accepted as implemented.** `execShutdown` keeps its single permit argument, and the permit carries no command id, so the late-failure Warn carries `error` only. The line stays attributable: one child per agent and at most one Warn per command. §6 and AD-009 are updated |
| `poweroffRunning` cleared before the exit hand-off | QA report note 3 (fixed after QA) | The reaper clears `poweroffRunning` **before** it hands the exit over to `execShutdown` or logs the late Warn, not in a `defer` after the hand-off. A shutdown sent after the child has exited is therefore never refused as `still running` while the hand-off is still in progress. AD-009 step 2 is updated |
| `pendingTimers()` accessor | implementation | **Not added.** Tests use the existing `fakeClock.timerDurations()` to wait for the start-check timer. §4 and §12 note 5 are updated |

### Revision 2: security conditions C-1 to C-6 and FRD revision 2

| Item | Source | What changed |
|---|---|---|
| **Truthful result (Option A)** | SEC-002, C-2; FR-027 to FR-032; OQ-010, OQ-013 | **New AD-009.** `execShutdown` launches the unchanged argv and waits for its exit for at most **`models.ShutdownStartCheckBound` = 3 s**, timed with the agent clock. The result is: exit 0 → initiated; a normal non-zero exit, or a launch error → failed `shutdown: <err>` (printable only, ≤ 200 bytes); still running at the bound, or killed by a signal → initiated. A single goroutine always reaps the child, and at most one poweroff child runs per agent. The seam and the production path share one mapping. Windows is included. Reboot's launch is untouched |
| Start-check bound sizing | FR-027, AC-031 | **AD-005 / AD-009:** 3 s. That is well above a sudo refusal (milliseconds) and a `systemctl poweroff` enqueue (well under 1 s), well below the 20 s SIGTERM grace, and 10m + about 7 s + 3 s < 12m |
| **C-1** test-binary power guard | SEC-001 | **New AD-010.** `inTestBinary()` (`return testing.Testing()`) is checked at the start of the real-process branch of `execReboot` and of `execShutdown`. In any test binary, whatever the harness, no power process can start. The proof is an AST check plus a state check, never a call to the real branch. The production argv is unchanged |
| **C-2** docs and follow-up | SEC-002 | README requirements in §4 (FR-033). The follow-up ID is SHUTDOWN-SUDO-001, recorded in the implementation report (§13). R-6 is rewritten. No privilege is added and the argv is unchanged |
| **C-3** permit bound to the allow flag | SEC-003 | **AD-001:** the gate spec now carries the action's allow flag. The gate checks it itself, in every guard step, before the caller's guards, so `nil` or wrong guards can no longer mint a permit. `awaitShutdownClearance(ctx, commandID)` binds its own guards. **AD-002:** the permit fields are renamed `permitID` / `permitAction`. **AD-006:** AST rules pin the guards arguments, the `commonPowerGuards` callers, every selector of the permit fields and of `powerPermitLive` / `powerPermitSeq`, and the self-test violations |
| **C-4** only power path | SEC-004 | **AD-006** is rewritten as a recursive scan of `internal/agent/...` and `cmd/riot-agent/...`. It flags power-tool launches (with `sudo`/`doas` stripped and shell tokens checked), `systemctl` power verbs and targets, case-insensitive `poweroff` / `power-off` / `halt` strings including constant-folded ones, and `syscall`/`unix` reboot calls or `windows` shutdown calls, everywhere outside `execReboot` / `execShutdown`. The self-test includes the Windows `-s` form and a subpackage file |
| **C-5** docs fixes | SEC-005, INFO-3, INFO-4 | R-7: about 25 minutes worst case; cancel by restarting the agent on the host. §7.5 and AC-010 are marked test-only (the config is read once at start-up). The §10 event statement is corrected. The stale README sudoers-row item is removed (INFO-4: already resolved) |
| **C-6** QA gate | | §13 lists it |
| R-4, SEC-005 | NFR-007, NFR-008; OQ-011, OQ-012 | Accepted (Ryan's defaults). They stay in §10 as accepted residuals |
| Signal termination of the poweroff child | AD-009 interpretation of FR-028 | FR-028's "exits with a non-zero status" is read as a **normal** exit with a code. A child killed by a signal has no exit status. That is what systemd's cgroup stop does to `sudo`/`systemctl` once the poweroff is underway, so it is treated like FR-030 ("still running") → initiated. Residual R-10 |

**AC-019 reading (unchanged from revision 1).** Three REBOOT-DPKG-001 tests pinned "shutdown has no gate" and the old
shutdown budgets, and they are retired or edited. Every reboot assertion is kept. AD-008 lists every edit. Revision 2
adds no reboot test edit.

### Revision 1

The first version: one gate with a parameter, an action-bound permit, shutdown budgets of 10m / 12m / 20m, call-site
enforcement, and reboot invariance.

---

## 1. Summary

The `shutdown` command goes through the same gate as reboot: the REBOOT-DPKG-001 dpkg-lock probe, then the bounded
wait, then the guards and a final lock check. The loop is not copied. It is parameterized by a `powerGate` spec, which
holds the action, the wait bound, the wait-started log message, and the action's allow flag. The gate checks the allow
flag itself before it can mint a permit.

The permit is an action-bound, single-use `powerPermit`. `execReboot` accepts only reboot permits and `execShutdown`
only shutdown permits.

`execShutdown` launches the unchanged poweroff argv and observes the exit for at most 3 seconds, so the result is
truthful:
- a refusal, such as sudo with no poweroff rule, reports `shutdown: <err>`;
- an accepted command, or one still running at the bound, reports `shutdown initiated`.

In a test binary, neither power exec can start a real process.

Shutdown gets its own budgets: wait 10 minutes, agent 12 minutes, server 20 minutes, start check 3 seconds. The queued
TTL stays 15 minutes.

Reboot results, events, logs, budgets, call sites and its launch stay byte-identical in production. The story adds no
migration, no UI change, no sudoers or `install.sh` change, and no new dependency.

## 2. Technical Context

Verified on `feat/shutdown-dpkg-001` (base `09c0ecf`, which contains PR #34). The revision 2 additions are marked †.

- **Shutdown path today** (`internal/agent/commands.go`):
  - `executeAction`: `case "shutdown": status, message = a.handleShutdown(payload)` (L173).
  - `handleShutdown(payload) (string, string)` (L387) checks `Commands.AllowShutdown`. It then runs
    `exec.Command("sudo","systemctl","poweroff")`, or `shutdown /s /t 5` on Windows, then `cmd.Start()` **with no
    `Wait()`**.
  - It does not use `a.runner`.
  - It is the only poweroff call site: `grep -i poweroff` finds `commands.go:396` only in non-test code under
    `internal/agent` and `cmd/riot-agent` (†).
- **Before dispatch.** `executeAction` already applies the host maintenance precheck (L101) and the host target lock
  (L110). `runAction` releases the lock when the handler returns. `newCmdRun` (`command_lifecycle.go:355`) sets
  `cr.ctx` to `AgentHardMax(action)`, which is 1 minute for shutdown today. The handler runs synchronously, and its
  return value is the result.
- **Results and events (†).** `completeCommand` masks the message. For an error result, it also sends the existing
  `EventCommandCompleted` warning `Command shutdown failed: <message>`, which the notifiers forward (security INFO-3).
- **Config (†).** `agent.yaml` is read once at start-up (`agent.go:139`). There is no reload, so `allow_shutdown`
  cannot change during a wait outside tests.
- **Agent stop (†).** `Run` handles SIGTERM (`agent.go:348`): it sets `shuttingDown`, then `waitForCommands(20s)`
  (`AgentShutdownGrace`). `riot-agent.service` has `After=network-online.target` and the default
  `KillMode=control-group`. Two consequences during a host poweroff:
  - systemd stops the agent before the network;
  - the agent's children (`sudo`, `systemctl`) receive SIGTERM along with the agent.
- **Clock seam (†).** `a.clock().NewTimer(d)` returns a `clockTimer`. The fake clock fires it on `Advance`.
- **Restart.** `recoverInterruptedCommands` (`command_lifecycle.go:641`) clears `shutdown` markers with no result and
  no re-run.
- **What REBOOT-DPKG-001 built** (`reboot_clearance.go`, `dpkglock*.go`):
  - the probe `a.probeDpkg()`;
  - the gate `awaitRebootClearance`, with `DpkgLockWaitBound` in three places and the literal wait message;
  - `commonRebootGuards`;
  - `rebootPermit` / `mintRebootPermit` / `rebootPermitSeq` / `rebootPermitLive`;
  - `rebootSkippedResult`;
  - the AST tests in `reboot_callsites_test.go`, including `…_ShutdownHasNoGate`.
- **The gate accepts `guards == nil` (†).** It then skips the guard step (`reboot_clearance.go:166`). That is the
  SEC-003 gap.
- **Budgets** (`models/command_policy.go:96-233`):
  - `reboot`: 12 minutes for the agent, 20 minutes on the server.
  - `shutdown`: 1 minute and 10 minutes, through the class fallback.
  - The queued TTL is 15 minutes for both.
  - `TestCL_DeadlineInvariant` requires the server deadline to exceed the agent maximum by more than 5 minutes.
- **Pinned shutdown values** that this story changes:
  - `TestCL_AC029_ExecDeadlineTable`;
  - `TestCommandPolicy_RD_AC020_ShutdownBudgetUnchanged`;
  - `TestCommandPolicy_QA_AC020_RebootAndShutdownBudgetsAreIndependent`;
  - `service_test.go:211`;
  - `sweeper_test.go:248`.
- **Harnesses.**
  - `newLifecycleHarnessAt` sets a `Free` probe and **no runner** (SEC-001).
  - `newAuthHarness` counts every `sudo systemctl …` call as a reboot.
  - `newGateRig` adds a scripted probe and a pause that advances the fake clock.
- **Sudoers.** `install.sh:436` grants `systemctl reboot` only. **No installer version ever granted `systemctl
  poweroff`** (`git log -S poweroff`, security review). The README has no poweroff row (INFO-4).
- **Binaries (†).** `cmd/riot-agent` is the agent binary. `cmd/riot-agent-apply` is a separate root helper and is out
  of the C-4 scope.

## 3. Architecture Decisions

### AD-001: One gate, parameterized by a `powerGate` spec that owns the allow flag (NFR-001, FR-001 to FR-012, FR-017; C-3 a)

**Decision.** The gate lives in `internal/agent/reboot_clearance.go`, which keeps its file name (§12 note 2).

```go
type powerAction uint8
const (
    powerActionNone     powerAction = iota // zero value: never valid for any exec
    powerActionReboot
    powerActionShutdown
)

type powerGate struct {
    action       powerAction
    waitBound    time.Duration
    waitMsg      string               // Info message when a wait starts
    allowed      func(a *Agent) bool  // the action's config flag; nil config → false
    allowOffText string               // guard text when allowed() is false
}

func rebootGate() powerGate   // {powerActionReboot, models.DpkgLockWaitBound, "dpkg lock held, waiting before reboot",
                              //  AllowReboot, "commands.allow_reboot is off"}
func shutdownGate() powerGate // {powerActionShutdown, models.ShutdownDpkgLockWaitBound, "dpkg lock held, waiting before shutdown",
                              //  AllowShutdown, "commands.allow_shutdown is off"}

type powerGuards func(ctx context.Context) string // was rebootGuards

type powerClearance struct { // was rebootClearance
    Cleared bool
    Permit  powerPermit
    Waited  time.Duration
    Skip    string        // "" | skipLockTimeout | skipLockUnknown | skipShutdown | skipGuard
    Detail  string
    Bound   time.Duration // set on every return to g.waitBound
}
func (c powerClearance) reason() string // the timeout text uses FormatDurationShort(c.Bound)

func (a *Agent) awaitPowerClearance(ctx context.Context, g powerGate, path, commandID string, guards powerGuards) powerClearance
func (a *Agent) awaitRebootClearance(ctx context.Context, path, commandID string, guards powerGuards) powerClearance // signature unchanged → awaitPowerClearance(ctx, rebootGate(), path, commandID, guards)
func (a *Agent) awaitShutdownClearance(ctx context.Context, commandID string) powerClearance                         // → awaitPowerClearance(ctx, shutdownGate(), "shutdown", commandID, a.commonShutdownGuards())
```

**The body of `awaitPowerClearance` is today's `awaitRebootClearance` body, moved, with exactly five changes:**
1. `models.DpkgLockWaitBound` becomes `g.waitBound`, in the bound check and in the sleep cap.
2. The wait-started message literal becomes `g.waitMsg`. The attributes are unchanged.
3. `a.mintRebootPermit()` becomes `a.mintPowerPermit(g.action)`.
4. `skip()` and `cleared()` set `Bound: g.waitBound`.
5. **The guard step (C-3).** Today's `if !guardsPassed { if guards != nil { … } }` becomes the following, evaluated
   at the same point, on every free observation that has not yet passed the guards:
   1. `if a.shuttingDown.Load()` → `skip(skipShutdown, guardShuttingDown)`;
   2. `if !g.allowed(a)` → `skip(skipGuard, g.allowOffText)`;
   3. `if guards != nil`, then the caller's guards with the existing mapping.

   `guardsPassed` is reset on every `Held` observation, as today. A permit can therefore be minted only after the
   action's own allow flag evaluated true in the same attempt, after the last wait. This is independent of what the
   caller passes.

**Shutdown placement.**

| Path | Before the gate (kept, fail-fast) | Gate call | Skip | Clearance |
|---|---|---|---|---|
| `handleShutdown(cr)` | `if !a.config.Commands.AllowShutdown` → the existing message | `c := a.awaitShutdownClearance(cr.ctx, cr.id)` | `shutdownSkippedResult(c)` | `a.execShutdown(c.Permit)` (AD-009) |

**Properties** (inherited from REBOOT-DPKG-001 AD-002, with `g.waitBound` = 10m):
- A free lock gives probe → guards → probe → clear, with no sleep (FR-005).
- `Absent` on a non-dpkg host or Windows gives the same sequence (FR-006).
- One wait per attempt, with one deadline at `firstHeld + 10m` (FR-003, FR-008, FR-011).
- `Held` followed by `Absent` skips as `Unknown` (FR-009).
- A release reaches the poweroff launch in ≤ 9 s (FR-004).
- An agent stop ends the wait in ≤ 3 s, or ≤ 5 s plus one probe during a guard evaluation (FR-012).
- The worst-case gate wall time is about 10m07s.

**Why the allow flag lives in the spec (C-3 a).** It is a runtime property of the one place that mints. A `nil` guard,
the other action's guards, or a future helper that reuses the gate cannot produce a permit without the action's own
flag. The entry points fix the spec, so the flag cannot be mismatched. The AST rules of AD-006 back this up.

**Alternatives considered.**
- *Copy the loop.* Rejected by NFR-001.
- *Loose bound and message arguments at each call site.* Rejected.
- *Mutable package-level spec variables.* Rejected.
- *Keep `rebootClearance` / `rebootGuards` aliases.* Rejected.
- *Rename the file.* Rejected (§12 note 2).
- *`execShutdown` re-checks `allow_shutdown` (C-3 option 3).* Rejected as the primary control: it would add a check
  after the final lock probe, against FR-011's "the last check is a lock check". The spec-owned check covers it.

---

### AD-002: Action-bound, single-use `powerPermit` (REBOOT C-3 extended; C-3 b)

**Decision.**

```go
type powerPermit struct {
    permitID     uint64      // unique names: the AST can find every selector use (C-3 b)
    permitAction powerAction
}

var (
    errRebootNotCleared   = errors.New("reboot refused: no dpkg-lock clearance for this attempt")   // unchanged
    errShutdownNotCleared = errors.New("shutdown refused: no dpkg-lock clearance for this attempt")
)

func (a *Agent) mintPowerPermit(action powerAction) powerPermit          // id := a.powerPermitSeq.Add(1); a.powerPermitLive.Store(id); return powerPermit{permitID: id, permitAction: action}
func (a *Agent) consumePowerPermit(p powerPermit, want powerAction) bool // p.permitID != 0 && p.permitAction == want && a.powerPermitLive.CompareAndSwap(p.permitID, 0)
func (a *Agent) execReboot(p powerPermit) error                          // !consume(p, powerActionReboot)   → errRebootNotCleared, nothing run
func (a *Agent) execShutdown(p powerPermit) error                        // !consume(p, powerActionShutdown) → errShutdownNotCleared, nothing run (AD-009)
```

The `Agent` fields `rebootPermitSeq` / `rebootPermitLive` are renamed `powerPermitSeq` / `powerPermitLive`.

**Semantics.**
- **Refused, with no state change:** a zero permit, a permit for the other action, a used permit, and a stale permit.
  The action check comes before the CAS, so a wrong-action permit leaves the live slot intact (INFO-1).
- **One live slot for both actions.** The host key serializes power gates (R-9).
- **Who may touch what (AD-006):**
  - only `mintPowerPermit` builds a literal;
  - only `consumePowerPermit` selects `permitID` / `permitAction`;
  - only those two functions select `powerPermitSeq` / `powerPermitLive`.

  A permit therefore cannot be forged through field assignment (`var p powerPermit; p.permitID = …`).

**`execReboot` change.** Its first line becomes `if !a.consumePowerPermit(p, powerActionReboot)`. The AD-010 guard is
added to its real branch. Nothing else changes: the same argv, `cmd.Start()` with no wait, and the same errors.

**Alternatives considered.**
- *A separate `shutdownPermit` with its own seq and live slot.* A second permit system (NFR-001).
- *Field names `id` / `action`.* Rejected: `cmdRun` has `id` and `action` too, so a name-based AST rule would need
  `go/types`.
- *Consume on an action mismatch.* Rejected.

---

### AD-003: Shutdown guards and `handleShutdown` (FR-002, FR-010, NFR-006)

**Decision.**

```go
const guardAllowShutdownOff = "commands.allow_shutdown is off"

func (a *Agent) commonPowerGuards(allowed func() bool, offText, ownID string) powerGuards // shuttingDown, then allowed(), then maintenanceBlockerFor(ownID)
func (a *Agent) commonRebootGuards(ownID string) powerGuards // = commonPowerGuards(AllowReboot, guardAllowRebootOff, ownID)
func (a *Agent) commonShutdownGuards() powerGuards           // = commonPowerGuards(AllowShutdown, guardAllowShutdownOff, "")

func (a *Agent) handleShutdown(cr *cmdRun) commandExecResult
```

The guard order is: `agent shutting down`, then `commands.allow_shutdown is off`, then `host busy: <blocker>`. The
maintenance check is strict (OQ-005). The first two repeat the gate-level checks of AD-001 step 5 with identical
texts, so the first failure reported is the same either way.

**`handleShutdown(cr)`:**
1. If `!a.config.Commands.AllowShutdown`, return `error` with the **unchanged** message. No probe runs and nothing
   waits (AC-009).
2. `c := a.awaitShutdownClearance(cr.ctx, cr.id)`. If `!c.Cleared`, return `shutdownSkippedResult(c)`.
3. If `err := a.execShutdown(c.Permit)` is not nil, return `error` with
   `"shutdown: " + sanitizePowerErr(err.Error())` (AD-009).
4. Otherwise return `success` with `shutdown initiated after waiting <D> for the dpkg lock` if `c.Waited > 0`, else
   `shutdown initiated`.

`executeAction`'s `case "shutdown":` becomes `return a.handleShutdown(cr)`. The status strings become the
`models.ResultStatus*` constants, which have the same values.

**Windows and non-dpkg hosts (FR-006).** The sequence is `Absent`, guards, `Absent`, clear, launch. The only
differences from today:
- the shutting-down guard, as for reboot (FR-010);
- the truthful result (AD-009, OQ-013).

---

### AD-004: Shutdown result contract, shared skip mapping (FR-013 to FR-016, FR-020, FR-028 to FR-030)

**Decision.** `powerSkippedResult(verb string, c powerClearance)` holds today's `rebootSkippedResult` body. Both
`rebootSkippedResult(c)` and `shutdownSkippedResult(c)` delegate to it.

| FR-013 | When | Status | Reason | Message |
|---|---|---|---|---|
| (a) | Cleared with no wait; poweroff exit 0, still running at 3 s, or killed by a signal (AD-009) | `success` | `""` | `shutdown initiated` |
| (b) | The same, after a wait | `success` | `""` | `shutdown initiated after waiting <D> for the dpkg lock` |
| (c) | Timeout | `error` | `""` | `shutdown skipped: dpkg lock held past the timeout (10m), <holder>` |
| (d) | Unknown | `error` | `""` | `shutdown skipped: dpkg lock state unknown: <cause>` |
| (e) | Agent stopping | `error` | `shutdown` | `shutdown skipped: agent shutting down` |
| (f) | Guard | `error` | `conflict` for `host busy: …`, else `""` | `shutdown skipped: guard failed: <guard>` |
| (g) | Launch error, normal non-zero exit within 3 s, permit refused, or a poweroff child still running | `error` | `""` | `shutdown: <err>` (printable, ≤ 200 bytes) |
| — | `allow_shutdown` false before the gate | `error` | `""` | `shutdown not allowed by agent config (set commands.allow_shutdown: true)` (unchanged) |

`<guard>` is one of `commands.allow_shutdown is off`, `host busy: <blocker>`, or `agent command time budget
exhausted`.

**Truthfulness (FR-015, BR-006).**
- Success requires that the OS did not refuse the command within the start-check bound.
- The gate and the launch run synchronously inside the handler.
- The permit is single-use and action-bound.
- The seen-ID store blocks a redelivery.
- An expired `cr.ctx` gives skip (f), never a poweroff.

`ReasonTimeout` is never set.

---

### AD-005: Shutdown budgets, split from reboot (FR-017 to FR-020, FR-027, BR-004)

**Decision.** New constants in `internal/models/command_policy.go`, in their own block:

```go
ShutdownDpkgLockWaitBound = 10 * time.Minute                           // FR-017
ShutdownAgentHardMax      = ShutdownDpkgLockWaitBound + 2*time.Minute  // 12m
ShutdownExecDeadline      = 20 * time.Minute                           // > 12m + DeadlineSlack
ShutdownStartCheckBound   = 3 * time.Second                            // FR-027, AD-009
```

In `ExecDeadline` and `AgentHardMax`, the `ClassPower` case becomes a `switch action`. The unreachable fallback keeps
10m / 1m. The doc comments are updated as in revision 1.

| Budget | `shutdown` before → after | `reboot` (unchanged) |
|---|---|---|
| Wait bound | none → **10m** | 10m (`DpkgLockWaitBound`) |
| Start check | none → **3 s** | none (reboot's launch is unchanged) |
| `AgentHardMax` | 1m → **12m** | 12m |
| `ExecDeadline` | 10m → **20m** (`exec_deadline_s` 600 → 1200) | 20m |
| `QueuedTTL` | 15m (unchanged) | 15m |
| Invariant | 20m > 17m ✓ | 20m > 17m ✓ |

**Fit (AC-031).** The worst-case wall time is the gate (about 10m07s) plus the start check (3 s), about 10m10s. That
is below 12m, which leaves about 1m50s for intake and the result. Revision 1's 12m / 20m therefore stand.

**Independence (AC-018).** No `Shutdown*` constant references `DpkgLockWaitBound` or a `Reboot*` constant, and the
reverse holds too. An AST test enforces this. The values are equal at launch, so no test asserts that they differ.

**Consequences.**
- A lost shutdown result is detected after 20 minutes instead of 10.
- A hard kill keeps the host-keyed server row blocking until that sweep.
- Version skew: R-5.

---

### AD-006: Structural enforcement: call sites, permit, gate binding and the only power paths (C-3, C-4; REBOOT C-3 / C-8; AC-020)

**6.1 Exec call sites** (`reboot_callsites_test.go`). The scanner is generalized as `scanExecRefs(t, filename, src,
execSpec)`:

```go
rebootSpec   = execSpec{Exec: "execReboot",   Gate: "awaitRebootClearance",   Callers: rebootCallers,              Allow: "Commands.AllowReboot"}
shutdownSpec = execSpec{Exec: "execShutdown", Gate: "awaitShutdownClearance", Callers: []string{"handleShutdown"}, Allow: "Commands.AllowShutdown"}
```

Every reference to `spec.Exec` must pass all of these checks:
1. it is the `Fun` of a call;
2. it is inside one of `spec.Callers`;
3. `spec.Allow` appears before it (the C-8 fail-fast check, kept as is);
4. its argument is `<x>.Permit`, where `x` is assigned from `a.<spec.Gate>(` and `if !x.Cleared` precedes the call.

All existing reboot assertions are kept. `execShutdown` has exactly one reference, in `handleShutdown`. A reboot
clearance passed to `execShutdown`, or the reverse, fails check 4.

**6.2 Permit and gate binding** (`TestPowerCallSites_SD_C3_GateEntryPointsBindTheAction`). It replaces
`TestRebootCallSites_RD_C3_PermitMintedOnlyByGate`. These rules apply to non-test files of `internal/agent`:
1. There is exactly one `powerPermit` composite literal, inside `mintPowerPermit`.
2. There is exactly one `mintPowerPermit` call, inside `awaitPowerClearance`.
3. Every `*ast.SelectorExpr` whose `Sel` is `permitID` or `permitAction` is inside `consumePowerPermit`. This covers
   reads, assignment, `++` and `&p.permitID`. Keys of the keyed literal in `mintPowerPermit` are `*ast.Ident` in a
   `KeyValueExpr`, not selectors.
4. Every selector `powerPermitSeq` or `powerPermitLive` is inside `mintPowerPermit` or `consumePowerPermit`.
5. `consumePowerPermit` is called exactly twice: from `execReboot` with the argument `powerActionReboot`, and from
   `execShutdown` with `powerActionShutdown`.
6. `awaitPowerClearance` is called exactly twice:
   - from `awaitRebootClearance`, with its gate argument a call to `rebootGate()`;
   - from `awaitShutdownClearance`, with its gate argument a call to `shutdownGate()` and its guards argument
     `a.commonShutdownGuards()`.
7. There are exactly two `powerGate` composite literals, one in `rebootGate` and one in `shutdownGate`.
8. Every call of `awaitRebootClearance` passes, as its fourth argument, a call to `a.commonRebootGuards(…)`.
9. `commonPowerGuards` is called only from `commonRebootGuards` and `commonShutdownGuards`.

**Self-test** (`TestPowerCallSites_SD_C3_SelfTest`). Each of these synthetic sources must be flagged:
- `p.permitID = a.powerPermitLive.Load()` in a third function (rules 3 and 4);
- `var p powerPermit; p.permitAction = powerActionShutdown` (rule 3);
- `a.powerPermitSeq.Add(1)` outside mint (rule 4);
- `a.awaitRebootClearance(ctx, "x", id, nil)` (rule 8);
- `a.awaitRebootClearance(ctx, "x", id, a.commonShutdownGuards())` (rule 8);
- `a.commonPowerGuards(func() bool { return a.config.Commands.AllowReboot }, …)` called from `handleShutdown`
  (rule 9);
- `a.awaitPowerClearance(ctx, shutdownGate(), …)` from a third function (rule 6);
- `awaitShutdownClearance` passing `rebootGate()` (rule 6).

The well-formed shapes pass.

**Runtime counterpart of C-3 (a)**, independent of the AST:
`TestPowerClearance_SD_C3_AllowFlagGatesTheMintWhateverTheGuards`. It is a table over both entry points. Each row sets
the action's own flag false and the other action's flag true, and passes `nil`, `noGuards` or the other action's
guards, with a free lock. Each row expects a `skipGuard` naming the right flag, and `powerPermitSeq == 0`.

**6.3 The only power paths (C-4)** (`internal/agent/power_paths_test.go`,
`TestPowerPaths_SD_C4_OnlyExecRebootAndExecShutdownLaunchPowerActions`).

*Scope:* every non-test `.go` file, recursively, under `internal/agent/` (`.` from the test) and `cmd/riot-agent/`
(`../../cmd/riot-agent`). The one exemption is the `FuncDecl`s named `execReboot` and `execShutdown` in package
`agent` at the root of `internal/agent`, including the func literals inside them.

*Constant folding* (AST only, no `go/types`, no new dependency). The fold of a string expression covers:
- `BasicLit` strings;
- `+` of foldable operands, and `ParenExpr`;
- identifiers naming a package-level string `const` of the same package directory, resolved to a fixpoint across that
  package's files;
- `pkg.Name` where `pkg` imports a package of this module (`github.com/DesyncTheThird/rIOt/…`), resolved by reading
  that package directory's `const` declarations on demand.

Anything else is unfoldable. The residual is §10 R-11.

*Rules.* Each is flagged outside the exemption.
- **(a) Process launches.**
  - *Which calls count:* `exec.Command`, `exec.CommandContext` (any alias of `os/exec`), `os.StartProcess`,
    `syscall.Exec`, `syscall.ForkExec` and `syscall.StartProcess`. Also any call whose function or method name is
    `runner`, `runCommand`, `runUpgradeCommand`, or matches `(?i)^run\w*command$`.
  - *Finding the program:* skip leading non-string arguments such as a context. The first foldable string is the
    program. If its lower-cased base name is `sudo` or `doas`, the next argument is the program instead.
  - *A power tool is flagged:* `base(lower(trimSuffix(program, ".exe")))` is one of `shutdown`, `halt`, `poweroff`,
    `reboot`, `init`, `telinit` or `kexec`.
  - *`systemctl` with a power verb or target is flagged:* the program is `systemctl` and any later folded argument,
    lower-cased, is one of `poweroff`, `halt`, `reboot`, `kexec`, `poweroff.target`, `halt.target`, `reboot.target`
    or `kexec.target`.
  - *Shells are flagged too:* the program is `sh`, `bash`, `dash`, `cmd`, `powershell` or `pwsh` (an optional `.exe`
    is ignored). Any later folded argument is split on whitespace and `;&|()`, and a token whose base name is in the
    power-tool set is flagged.
- **(b) Strings.** Any foldable string expression, anywhere in a file, whose value matches
  `(?i)(^|[^a-z0-9])(power-?off|halt)([^a-z0-9]|$)`. This covers `poweroff.target`, `PowerOff` (login1), and
  `"power" + "off"`.
- **(c) System calls.**
  - Any selector on an import of `syscall` or `golang.org/x/sys/unix` whose name is `Reboot` or starts with
    `LINUX_REBOOT_` or `RB_`.
  - Any selector on `golang.org/x/sys/windows` named `InitiateSystemShutdownEx`, `InitiateShutdown` or
    `ExitWindowsEx`.

**Self-test** (`TestPowerPaths_SD_C4_SelfTest`). It scans in-memory files. Each of these must be flagged:
- `collectors/power_windows.go`, a subpackage file: `exec.Command("shutdown", "-s", "-t", "0")`;
- `exec.Command("sudo", "/sbin/shutdown", "-h", "now")`;
- `exec.Command("systemctl", "start", "poweroff.target")`;
- `exec.Command("systemctl", "halt")`;
- `a.runner(ctx, "sudo", "systemctl", "reboot")` in a function other than `execReboot`;
- `runCommand(ctx, "init", "0")`;
- `exec.Command("sh", "-c", "sleep 1; telinit 0")`;
- `const verb = "power" + "off"` used in a call;
- `"org.freedesktop.login1.Manager.PowerOff"`;
- `unix.Reboot(unix.LINUX_REBOOT_CMD_POWER_OFF)`;
- `syscall.Reboot(…)`.

These must not be flagged:
- the real `execReboot` and `execShutdown` shapes;
- `exec.Command("systemctl", "show", …)`;
- `"/var/run/reboot-required"`;
- `models.ActionShutdown`.

**Retired:** `TestRebootCallSites_RD_AC020_ShutdownHasNoGate`, and revision 1's literal-only argv test.

---

### AD-007: Bounded growth (NFR-003, house rule)

| Item | Bound per `shutdown` command (or per agent, where stated) |
|---|---|
| Waits | ≤ 1, with one deadline `firstHeld + 10m`, never retried |
| Lock checks | ≤ 303 probes, each ≤ 1 MiB + 1 byte read, ≤ 8 KiB per small read, ≤ 1 s |
| Guard evaluations | only on a free observation, ≤ 1 per poll; ≤ 1 `systemctl show` each (≤ 5 s) |
| Poweroff processes | ≤ 1 launched per command; **≤ 1 alive per agent** (`poweroffRunning`; a second launch is refused with failure (g)) |
| Goroutines | ≤ 1 reaper per live poweroff child. It ends when the child exits and the hand-off completes (AD-009) |
| Captured output | ≤ `poweroffOutputCap` = 4 KiB per child (a capped writer that discards the rest) |
| Result `<err>` | ≤ 200 bytes, printable only |
| Timers | 1 gate sleep at a time; 1 start-check timer, stopped on return |
| Info-or-above log lines | ≤ 2 from the gate, only when it waited; ≤ 1 Warn from the reaper, only for a refusal after the start check; none per check |
| Command results / events | exactly 1 result. A failed result adds only the existing failed-command warning event. No new event type, no telemetry refresh |
| Permits | ≤ 1 minted, consumed at most once; 1 live slot per agent |
| Concurrent shutdown waits | ≤ 1 per agent (the host key) |
| Persistent state | **none** |

**Failure paths:** timeout, unknown, guard, agent stop, budget expiry, launch error, non-zero exit, a second launch
while a child runs, and a permit refusal. Each ends the command with one result, releases the host key in
`runAction`, and leaves no file, row or unreaped child. A child that never exits is held by its single reaper until the
agent exits. Every further `shutdown` on that agent then fails with `a previous poweroff command is still running`.
That is bounded at one.

---

### AD-008: Reboot invariance: what changes in reboot code, and the exact test edits (FR-021, FR-031, AC-019, AC-032)

**Reboot production code that changes, and why the production output is byte-identical:**

| Change | Why it is identical |
|---|---|
| The loop moves into `awaitPowerClearance`; `awaitRebootClearance` keeps its signature | `rebootGate()` uses the same bound, the same message literal and the same attributes |
| The gate-level guard step (AD-001 step 5) | It evaluates `shuttingDown`, then `AllowReboot`, with the same texts and the same order as the first two checks of `commonRebootGuards`, which all three callers pass (AD-006 rule 8). The first failure reported is unchanged in every case. The extra config read happens only where the old code already read it |
| `reason()` reads `c.Bound` | The reboot gate sets 10m, so the text is `(10m)` |
| `powerPermit{permitID, permitAction}`, `mintPowerPermit`, `consumePowerPermit`, and the renamed fields | The reboot gate mints with `powerActionReboot`. Single use and refusal are unchanged, with the same `errRebootNotCleared` |
| `execReboot`: `consumePowerPermit`, plus the AD-010 guard before the real branch | `inTestBinary()` is false in the agent binary. The argv, `cmd.Start()` with no `Wait()` (FR-031), the runner call and the errors are unchanged. **The reboot launch gets no start check** |
| `commonRebootGuards` → `commonPowerGuards`; `rebootSkippedResult` → `powerSkippedResult("reboot", …)`; type renames | Same order, texts and mapping |

**Unchanged:** the three reboot call sites, `abortPostPatchReboot`, `abortAuthorizedReboot`, the reboot budgets, and
every reboot log message. **No reboot-visible string changes.**

**Test edits.** These are the revision 1 list. Revision 2 adds none to reboot tests.

| File | Edit | Kind |
|---|---|---|
| `reboot_clearance_test.go` | L182 `rebootGuards` / `rebootClearance` → `powerGuards` / `powerClearance` | rename |
| `reboot_clearance_test.go` | L857, L969 `rebootPermitSeq` → `powerPermitSeq` | rename |
| `reboot_clearance_test.go` | L923, L977 `rebootPermit{}` → `powerPermit{}` | rename |
| `reboot_clearance_test.go` | L930 `rebootClearance{…}` → `powerClearance{…}` | rename |
| `reboot_clearance_test.go` | L939, L949, L950 `mintRebootPermit()` → `mintPowerPermit(powerActionReboot)` | rename |
| `reboot_clearance_test.go` | L990–L996 → `powerClearance`; the timeout row gains `Bound: models.DpkgLockWaitBound` (expected text unchanged) | rename + fixture field |
| `reboot_clearance_qa_test.go` | L68 `rebootPermitSeq` → `powerPermitSeq` | rename |
| `reboot_clearance_qa_test.go` | `TestCommandPolicy_QA_AC020_…`: delete L110, L111, L112, L116; keep L113–L115 | pinned old shutdown values |
| `reboot_callsites_test.go` | `execSpec` generalization; assertions and messages kept | refactor |
| `reboot_callsites_test.go` | `…_PermitMintedOnlyByGate` → superseded by AD-006.2 (same counts, plus new rules) | rename/extend |
| `reboot_callsites_test.go` | `…_RD_AC020_ShutdownHasNoGate` deleted | pinned old shutdown behaviour |
| `models/command_policy_test.go` | AC029 shutdown row → 20m; `…_RD_AC020_ShutdownBudgetUnchanged` deleted (its TTL and class checks move to SD_AC017) | pinned old shutdown values |
| `server/commands/service_test.go` | L211 600 → 1200 | pinned old shutdown value |
| `server/commands/sweeper_test.go` | Subtest L248–L260 → `TestSweep_SD_AC016_*` | pinned old shutdown value |

---

### AD-009: Truthful poweroff result: start check, shared mapping, single reaper (SEC-002 Option A; FR-027 to FR-032; OQ-013)

**Decision.** In `execShutdown(p)`, after the permit check:
1. **One child per agent.** `if !a.poweroffRunning.CompareAndSwap(false, true)`, return an error that
   `execShutdown` builds itself (revision 3, AQ-1). Its text is `a previous poweroff command is still running`, and no
   package-level variable holds it, so C-4 rule (b) stays strict.
2. **Launch in one goroutine** (a func literal inside `execShutdown`, so it is lexically covered by AD-006.3 and
   AD-010). The goroutine:
   1. computes `x := poweroffExitFrom(out, err)`, where `out, err` come from:
      - **the seam**, when `a.runner != nil`: `a.runner(context.Background(), "sudo", "systemctl", "poweroff")`;
      - **the AD-010 guard** otherwise: `if inTestBinary() { return …errPowerProcessInTest }`;
      - **production** otherwise:
        - `cmd := exec.Command(...)` with the unchanged argv (`shutdown /s /t 5` on Windows, `sudo systemctl
          poweroff` elsewhere), **not** `CommandContext`, because a cancelled command context must never kill a
          poweroff;
        - `cmd.Stdout = cmd.Stderr = &cappedWriter{max: poweroffOutputCap}`;
        - `cmd.WaitDelay = time.Second`, so a grandchild that holds the pipe cannot hold up `Wait`;
        - `err = cmd.Start()`; if that succeeds, `err = cmd.Wait()`;
        - `out` is the captured output;
   2. runs `a.poweroffRunning.Store(false)` as soon as the child has exited, **before** the hand-off (revision 3,
      QA note 3);
   3. hands `x` over with `select { case done <- x: case <-abandoned: if !x.initiated() { slog.Warn("shutdown:
      poweroff command failed after the start check", "error", <sanitized>) } }`. The Warn carries no `command_id`,
      because `execShutdown` takes only the permit (revision 3, AQ-2).
3. **Start check.** `t := a.clock().NewTimer(models.ShutdownStartCheckBound)`, then `defer t.Stop()`, then
   `select`:
   - `x := <-done` → `return classifyPoweroff(x)`;
   - `<-t.C()` → `close(abandoned)`; `return nil` (initiated, FR-030).

   `done` and `abandoned` are unbuffered. The goroutine always completes exactly one branch, so it never blocks
   forever. It ends when the child exits.

**Shared mapping (FR-032).** Both paths produce `(out []byte, err error)` and go through the same two functions in
`internal/agent/poweroff.go`:

```go
type poweroffExit struct {
    Kind   poweroffExitKind // exitedZero | exitedNonZero | signaled | launchFailed
    Code   int
    Err    error
    Output string           // ≤ poweroffOutputCap
}
func poweroffExitFrom(out []byte, err error) poweroffExit
func classifyPoweroff(x poweroffExit) error // nil = initiated
```

`poweroffExitFrom` sorts the outcome:

| Input | Kind |
|---|---|
| `err == nil` | `exitedZero` |
| `errors.Is(err, exec.ErrWaitDelay)` with no exit code error | `exitedZero` (the process exited 0; only output was pending) |
| `err` has `ExitCode() int` (`*exec.ExitError`, or the tests' `fakeExit`), and the code is ≥ 0 and not 0 | `exitedNonZero` |
| `err` has `ExitCode() int`, and the code is −1 | `signaled` |
| any other `err` (`exec.ErrNotFound`, a `Start` error, `errPowerProcessInTest`, a runner's plain error) | `launchFailed` |

`classifyPoweroff` maps the kinds:

| Kind | Result |
|---|---|
| `exitedZero`, `signaled` | `nil` (initiated) |
| `exitedNonZero` | `errors.New(err.Error() + optional ": " + Output)`, for example `exit status 1: sudo: a terminal is required to read the password; …` |
| `launchFailed` | `err`; with no error text, the fallback `shutdown command did not start` (revision 3, AQ-1) |

`handleShutdown` turns any error into `shutdown: ` + `sanitizePowerErr(err.Error())`:
1. `models.SanitizeText`;
2. every rune for which `!unicode.IsPrint` becomes a space (this covers `\n`, `\t`, ESC and other control runes);
3. runs of spaces collapse to one, and the ends are trimmed;
4. `models.TruncateHead(…, 200)`, which is rune-safe.

**Why 3 seconds** (`models.ShutdownStartCheckBound`):
- **Refusal side.** sudo with no matching rule and no TTY exits 1 within milliseconds. PAM or NSS latency adds well
  under a second. 3 s separates refusal from acceptance with a wide margin (FRD A-005).
- **Acceptance side.** `systemctl poweroff` enqueues the job and returns, normally in under a second, even on a slow
  Pi. A slower return is the FR-030 case (initiated).
- **SIGTERM race.** On a working host, systemd starts stopping units right after the enqueue. Under the default
  `KillMode=control-group`, the agent **and** its `sudo`/`systemctl` children get SIGTERM.
  - The agent's handler sets `shuttingDown` and gives commands 20 s (`AgentShutdownGrace`).
  - Our result is ready ≤ 3 s after launch: typically milliseconds, or at the latest at the bound. That leaves
    ≥ 17 s for `completeCommand` to write the durable outbox and send it (`LifecycleSendTimeout` 10 s).
  - The unit is `After=network-online.target`, so systemd stops the agent before the network goes down. The result
    is posted while the network is still up.
  - A child that SIGTERM kills mid-exit is `signaled`, which means initiated (see below).
- **Budget.** 10m (wait) + about 7 s (gate tail) + 3 s < 12m (`ShutdownAgentHardMax`). `TestCommandPolicy_SD_AC031_*`
  asserts this.

**Why a signal counts as initiated.** sudo's refusal, the only refusal FR-028 targets, is always a normal exit with
code 1. Windows `shutdown.exe` refusals are normal exits too, for example 5 or 1190. A signal-killed child, with no
exit code, is what the cgroup stop does once the poweroff is underway. Reporting it as failed would make a working
host report a false failure. The residual (an unrelated signal in the 3 s window) is R-10.

**Windows (OQ-013).** `shutdown /s /t 5` schedules and returns 0 at once, so it reports initiated as today. A refusal
is now reported truthfully. There is no signal case on Windows.

**Reboot.** None of this applies. `execReboot` keeps `cmd.Start()` with no wait (FR-031).

**Alternatives considered.**
- *`cmd.Run()` with no bound.* Rejected: a hung `systemctl` would hold the command until `cr.ctx` expired, and FR-030
  requires the bound.
- *Kill the child at the bound.* Rejected: it does not cancel an enqueued poweroff, and the agent user may not be
  able to signal a setuid-root `sudo`.
- *Treat a signal as failed.* Rejected: it gives false failures on working hosts.
- *Require `shuttingDown` to be set for a signal to count as initiated.* Rejected: the agent's own SIGTERM handler
  races the child's `Wait`. It would also add nothing in the operator-restart case.
- *`sudo -n`.* Rejected by FR-031: the argv must not change.
- *A context-cancelled `exec.CommandContext`.* Rejected: a poweroff must never be killed by the command context.

---

### AD-010: Test-binary power guard (C-1; SEC-001)

**Decision.** New file `internal/agent/powerguard.go`:

```go
// inTestBinary reports whether this process is a `go test` binary (Go 1.21+). Power commands never run there.
func inTestBinary() bool { return testing.Testing() }

var errPowerProcessInTest = errors.New("refusing to start a real power command inside a test binary")
```

- **Where it is checked.** In `execReboot`, and inside `execShutdown`'s launch closure, the real-process branch
  begins with `if inTestBinary() { return …errPowerProcessInTest }`. This comes after the `a.runner != nil` seam and
  before any `exec.Command`.
  - In a test binary, any harness that reaches a power exec without a runner gets an error result
    (`reboot: refusing …` / `shutdown: refusing …`), never a process. That holds for `newLifecycleHarness`, any
    other harness, and any package's test binary that links `internal/agent`.
  - In the agent binary, `testing.Testing()` is false, so the argv and behaviour are unchanged (AC-024).
- **Why `testing.Testing()` is safe in production code.** Importing `testing` from a non-test file registers no flags
  (since Go 1.13 they are registered in `testing.Init`). `testing.Testing()` exists for exactly this purpose. `go.mod`
  is at Go 1.24.

**Verification that never runs the real branch** (C-1 b):
- `TestPowerGuard_SD_C1_TestBinaryIsDetected` asserts `inTestBinary()` is true. That is the state.
- `TestPowerGuard_SD_C1_RealBranchesConsultTheGuardFirst` is AST only, on `execReboot` and `execShutdown`
  (including its func literal):
  1. there is exactly one `IfStmt` whose condition is the call `inTestBinary()` and whose body ends in `return`;
  2. that `IfStmt` is a direct statement of a `BlockStmt` that transitively contains every `exec.Command` /
     `exec.CommandContext` call of the function, and it comes before all of them;
  3. `inTestBinary`'s body is exactly `return testing.Testing()`, and `inTestBinary` is a function, not a variable.
- `TestPowerGuard_SD_C1_SelfTest` uses synthetic sources. Each of these is flagged: an `exec.Command` before the
  guard; a guard in a sibling `if` branch; a guard whose body does not return; and `var inTestBinary = func() bool {…}`.
- AD-006.3 guarantees that no other function launches a power tool, so the guard covers every real power launch.

**Alternatives considered.**
- *A package variable set by `init()` in a `_test.go` file.* Rejected: the test binaries of other packages that link
  `internal/agent` (such as `cmd/riot-agent`) do not compile `agent`'s `_test.go` files.
- *A build tag.* Rejected: a missing tag in one invocation fails open.
- *A default refusing runner in `newLifecycleHarnessAt`.* Rejected: it protects one harness only, which is exactly
  SEC-001's objection.
- *Calling the real branch to prove it refuses.* Rejected by C-1 b.

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| MODIFY | `internal/agent/reboot_clearance.go` | Header comment. `powerAction`, `powerGate` (with `allowed` and `allowOffText`), `rebootGate()`, `shutdownGate()`. `powerGuards`, `powerClearance` with `Bound`, and `reason()`. `awaitPowerClearance` (the moved loop with the five AD-001 changes), `awaitRebootClearance` (wrapper, same signature), `awaitShutdownClearance(ctx, commandID)` (binds its own guards). `powerPermit{permitID, permitAction}`, `mintPowerPermit`, `consumePowerPermit`, `errShutdownNotCleared`. `guardAllowShutdownOff`. `commonPowerGuards`, `commonRebootGuards`, `commonShutdownGuards` |
| MODIFY | `internal/agent/commands.go` | `case "shutdown": return a.handleShutdown(cr)`. `handleShutdown(cr) commandExecResult` (AD-003). `powerSkippedResult`, `rebootSkippedResult`, `shutdownSkippedResult`. `execReboot`: `consumePowerPermit`, plus the AD-010 guard; otherwise unchanged. New `execShutdown`: permit, `poweroffRunning` (the "still running" error is built here, revision 3), launch closure (seam, guard, real branch), start-check `select`, reaper hand-off (AD-009) |
| CREATE | `internal/agent/poweroff.go` | `poweroffExitKind`, `poweroffExit`, `poweroffExitFrom`, `classifyPoweroff` (fallback `shutdown command did not start`), `sanitizePowerErr`, `cappedWriter`, `poweroffOutputCap = 4 << 10`, `poweroffErrMax = 200`. **It contains no `poweroff` / `halt` string literal**, so C-4 rule (b) applies to it unchanged (revision 3, AQ-1) |
| CREATE | `internal/agent/powerguard.go` | `inTestBinary()`, `errPowerProcessInTest` (AD-010) |
| MODIFY | `internal/agent/agent.go` | `rebootPermitSeq` / `rebootPermitLive` → `powerPermitSeq` / `powerPermitLive`; new `poweroffRunning atomic.Bool`; comments |
| MODIFY | `internal/models/command_policy.go` | `ShutdownDpkgLockWaitBound`, `ShutdownAgentHardMax`, `ShutdownExecDeadline`, `ShutdownStartCheckBound`; `ClassPower` `switch action`; doc comments |
| MODIFY | `internal/models/command_policy_test.go` | AC029 row; delete `…_RD_AC020_ShutdownBudgetUnchanged`; SD tests for AC-017, AC-018 and AC-031 |
| MODIFY | `internal/agent/reboot_clearance_test.go` | Mechanical renames only (AD-008) |
| MODIFY | `internal/agent/reboot_clearance_qa_test.go` | AD-008 rows |
| MODIFY | `internal/agent/reboot_callsites_test.go` | `execSpec` generalization; delete `…_ShutdownHasNoGate`; add the AD-006.1 and AD-006.2 tests and self-tests |
| CREATE | `internal/agent/power_paths_test.go` | AD-006.3: the C-4 recursive scan, the constant folder, and the self-test |
| CREATE | `internal/agent/powerguard_test.go` | AD-010: the C-1 state, AST and self-tests |
| CREATE | `internal/agent/poweroff_test.go` | `poweroffExitFrom`, `classifyPoweroff` and `sanitizePowerErr` tests, including the helper-process pattern (§12 note 6) |
| CREATE | `internal/agent/shutdown_clearance_test.go` | `shutdownRig` (§12 note 4) and every `TestShutdown_SD_*`, `TestShutdownClearance_SD_*`, `TestPowerClearance_SD_*`, `TestExecShutdown_SD_*`, `TestExecReboot_SD_*` and `TestRebootGate_SD_*` test of §8 |
| UNCHANGED | the fake clock test helper | **No `pendingTimers()` accessor was added** (revision 3). Tests use the existing `fakeClock.timerDurations()` to wait for the start-check timer before they `Advance` (§12 note 5) |
| MODIFY | `internal/server/commands/service_test.go` | Shutdown `dl` 600 → 1200 |
| MODIFY | `internal/server/commands/sweeper_test.go` | Move the shutdown subtest; add `TestSweep_SD_AC016_ShutdownNotTimedOutBefore20m` |

**Explicitly unchanged:**
- `internal/agent/dpkglock*.go`, `reboot_auth.go`, `command_lifecycle.go`, `maintenance*.go`, `doctor.go`;
- `internal/server/**` non-test code and `internal/server/db/*`; all migrations;
- `scripts/install.sh`, `scripts/riot-agent.service`, and sudoers;
- `web/` (DeviceDetail power menu and confirm dialog);
- `cmd/**` (scanned by a test, not changed);
- `go.mod` / `go.sum`.

**Technical writer (post-QA).** The README section "Reboots and the dpkg lock" (L2645–L2762) keeps its heading, so
the anchor `#reboots-and-the-dpkg-lock` stays valid.
- L2647 upgrade note: add that shutdown is covered by the same server-first rule.
- L2649: "Every reboot **and every shutdown** the agent starts waits …".
- L2660: the table becomes "Which power actions wait", and the Shutdown row becomes "Yes".
- "How the wait works" step 3: for a shutdown, the agent re-checks `commands.allow_shutdown`.
- L2671: a waiting shutdown also holds the host's command slot.
- "What you see": add a **Shutdown** results table with the AD-004 rows, including (g).
- **FR-033 / C-2 / C-5**, in a new subsection "Shutdown on Linux and the sudo rule" and in "Known limits":
  - The installer grants no `systemctl poweroff` sudo rule. On installer-managed Linux hosts, `shutdown` reports
    **failed**, for example `shutdown: exit status 1: sudo: a terminal is required …`, until SHUTDOWN-SUDO-001 ships.
    This is the truthful result: shutdown has never worked there.
  - `shutdown initiated` means the OS accepted the poweroff command, or the command was still running 3 seconds after
    launch. It does not mean the host has finished powering off.
  - A shutdown can power the host off up to about 10 minutes after the click, or up to about 25 minutes if the device
    was disconnected when it was sent (15 minutes queued, plus the wait). The host then stays off.
  - The dashboard cannot cancel a waiting shutdown. To cancel one, restart the agent on the host
    (`systemctl restart riot-agent`). The command then ends as `shutdown skipped: agent shutting down` and is never
    re-run.
  - **Do not** present editing `commands.allow_shutdown` as a cancel path. The agent reads its config only at
    start-up.
  - Any local user who holds the dpkg lock can delay or block a rIOt shutdown. Each block shows in the result with
    the holder's pid and uid.
- "Agent log": add the message `dpkg lock held, waiting before shutdown`, the `path` value `shutdown`, and the reaper
  Warn `shutdown: poweroff command failed after the start check`, which carries an `error` field only.
- "Time limits": shutdown rows (wait 10 minutes, start check 3 seconds, agent 12 minutes, server 20 minutes /
  `exec_deadline_s` 1200, queued 15 minutes). State that the reboot and shutdown limits are independent. Delete L2747.
- "Local users can block reboots" → "… reboots and shutdowns".
- "Known limits": delete L2761 "Shutdown is not gated."
- **README power budget table L2171:** shutdown `15 minutes | 20 minutes (a shutdown can wait up to 10 minutes for
  the dpkg lock, see …)`.
- **Do not add a poweroff row to the README Sudoers Rules table** (INFO-4).
- **`web/src/pages/LearnMore.tsx` L432–L433:** "rIOt reboots and shutdowns wait up to 10 minutes … the reboot or
  shutdown is skipped and reported. rIOt never forces it." This is help text, not the power menu or dialog.
- **CHANGELOG `[Unreleased]`:**
  - **Added:** the shutdown wait and the truthful shutdown result.
  - **Changed:** the shutdown deadline is 20m and the agent budget 12m; on installer-managed Linux hosts the shutdown
    result now reports failed (SHUTDOWN-SUDO-001).
  - **Upgrade notes:** upgrade the server before the agents; after a shutdown shows `timed_out`, check whether the
    host is off before re-sending.
  - **Conditional:** if `git tag --contains 09c0ecf` prints nothing, amend the REBOOT-DPKG-001 clauses L14 ("`shutdown`
    is not gated") and L38 ("`shutdown` keeps 10 minutes and 1 minute"). Otherwise log them as stale.
- **Stale content to log, not edit:**
  - REBOOT-DPKG-001 ADD:
    - §1 shutdown budgets;
    - the AD-004 shutdown column;
    - §4 "unchanged: `handleShutdown`";
    - §8 AC-020 rows;
    - §12 note 10;
    - the §13 shutdown DoD line;
    - the DoD grep `rebootPermit{`.
  - REBOOT-DPKG-001 FRD FR-020.
  - REBOOT-DPKG-001 security review INFO-4.
  - CMD-LIFECYCLE-001 ADD AD-005 / AD-013.

## 5. Data Model Changes

**Postgres: none.** No migration, table, column or index. New `shutdown` rows store `exec_deadline_s = 1200`.

**Models (code only):** four constants and two `switch action` branches.

**Agent on-disk state: none new.**

**In-memory state** (never persisted):
- per attempt: the gate locals; the start-check timer; the `done` and `abandoned` channels;
- per agent: `powerPermitSeq` and `powerPermitLive` (renamed), and **`poweroffRunning` (new)**.

| Before | After |
|---|---|
| `rebootPermit{id}` | `powerPermit{permitID, permitAction}` |
| `rebootClearance{Cleared, Permit, Waited, Skip, Detail}` | `powerClearance{…, Bound}` |
| `rebootGuards` | `powerGuards` |
| `Agent.rebootPermitSeq` / `rebootPermitLive` | `Agent.powerPermitSeq` / `powerPermitLive` |
| none | `powerAction`, `powerGate`, `poweroffExit`, `Agent.poweroffRunning` |

## 6. API / Interface Contract

No new or changed endpoint. The command result shape is unchanged (NFR-005). Only the message text changes, and, for
refused poweroffs, the status.

### Command record (existing)

| Field | Change |
|---|---|
| `exec_deadline_s` | new `shutdown` rows `1200` (was `600`) |
| `deadline_at` | `dispatched_at + 1200 s` for new `shutdown` rows |
| `expires_at` | unchanged (`+15m`) |
| `result_msg`, `status`, `outcome`, `reason` | the AD-004 table |

### Agent command result for `shutdown`

```
shutdown initiated
shutdown initiated after waiting <D> for the dpkg lock
shutdown skipped: dpkg lock held past the timeout (10m), <holderText>
shutdown skipped: dpkg lock state unknown: <cause>
shutdown skipped: agent shutting down                       (reason "shutdown")
shutdown skipped: guard failed: <guard text>                (reason "conflict" for "host busy: …")
shutdown: <err>                                             (printable, <err> ≤ 200 bytes)
shutdown not allowed by agent config (set commands.allow_shutdown: true)
```

Typical `<err>` values:
- `exit status 1: sudo: a terminal is required to read the password; either use the -S option …` (truncated at 200
  bytes);
- `exec: "sudo": executable file not found in $PATH`;
- `a previous poweroff command is still running` (built inside `execShutdown`, revision 3);
- `shutdown command did not start` (the `classifyPoweroff` fallback for a launch error with no text, revision 3);
- `shutdown refused: no dpkg-lock clearance for this attempt`;
- `exit status 1190: …` (Windows);
- `refusing to start a real power command inside a test binary` (test binaries only).

`<holderText>` and `<cause>` follow the REBOOT-DPKG-001 §6 grammar.

### Agent log (existing format)

```
INFO dpkg lock held, waiting before shutdown  path=shutdown command_id=<uuid> holder="<holderText>"
INFO dpkg lock wait ended                     path=shutdown command_id=<uuid> outcome=<…> waited=<d> holder="<holderText>"
WARN shutdown: poweroff command failed after the start check  error="<sanitized>"   (≤ 1 per command, only after a 3 s timeout)
```

The Warn has no `command_id` (revision 3, AQ-2): `execShutdown` takes only the permit. It is still attributable,
because at most one poweroff child runs per agent and each command writes at most one such line.

### Agent events

None new. A failed result raises the existing `command_completed` warning `Command shutdown failed: <message>` (§10).

## 7. Sequence / Flow

### 7.1 Shutdown, lock held 3 minutes, working host (AC-003, AC-027)

1. The server stores `exec_deadline_s = 1200`.
2. Agent intake: `cr.ctx` with a 12m timeout, the maintenance precheck, the host lock.
3. `handleShutdown`: `AllowShutdown` passes, then `awaitShutdownClearance(cr.ctx, cr.id)`.
4. The probe returns `Held`. One Info line. A poll every 2 s.
5. At T0+3m the probe returns `Free`. The guard step runs: `shuttingDown`, `allowed` (AllowShutdown), then
   `commonShutdownGuards`. The final probe returns `Free`. The gate calls `mintPowerPermit(powerActionShutdown)` and
   writes one Info line (`wait ended`).
6. `execShutdown`: the permit is consumed, `poweroffRunning` goes false → true, the goroutine launches `sudo systemctl
   poweroff`, and a 3 s timer starts.
7. `systemctl` enqueues the job and exits 0 after about 50 ms. The goroutine clears `poweroffRunning`, then hands over
   `exitedZero`, and the result is `nil`.
8. The result `success, "shutdown initiated after waiting 3m for the dpkg lock"` goes to the outbox. `runAction`
   releases the host lock.
9. systemd stops `riot-agent.service`, sending SIGTERM to the agent and to any child still alive. `shuttingDown` is
   set, and the outbox send completes within the 20 s grace, before the network stops. The host powers off.

### 7.2 Installer-managed Linux host, no sudo rule (AC-028)

Steps 1–6 are as in 7.1. Then:
1. `sudo` exits 1 within milliseconds, with `sudo: a terminal is required to read the password; …` on stderr.
2. The goroutine clears `poweroffRunning` and hands over `exitedNonZero(1)`.
3. The result is `error, "shutdown: exit status 1: sudo: a terminal is required to read the password; either use the
   -S …"` (≤ 200 bytes). The existing failed-command warning event follows. The host stays up, and the result says so.

### 7.3 `systemctl` slow (FR-030, AC-030)

1. The 3 s timer fires first. `abandoned` is closed, and the result is initiated.
2. The goroutine keeps waiting.
3. When the child exits, the goroutine clears `poweroffRunning` first, then:
   - exit 0 or killed by a signal: nothing is logged;
   - non-zero: one Warn line.
4. A second `shutdown` sent while the child is alive fails with `shutdown: a previous poweroff command is still
   running`.

### 7.4 Timeout, unknown, held then absent

These are as in revision 1, and as for reboot: (c) and (d) results, no permit, no launch.

### 7.5 `allow_shutdown` false during the wait (AC-010: unit test only)

The test flips the config. When the lock is released, the gate-level `allowed` check fails, giving
`shutdown skipped: guard failed: commands.allow_shutdown is off`. **A running agent never reloads its config**, so this
is not an operator control. The operator cancel path is 7.6.

### 7.6 Operator cancels by restarting the agent (C-5, AC-013)

1. `systemctl restart riot-agent` sends SIGTERM. Within one poll (≤ 3 s), the gate returns `skipShutdown`.
2. The result `shutdown skipped: agent shutting down` (reason `shutdown`) is written to the durable outbox.
3. Recovery after the restart does not re-run the command.

### 7.7 Version skew: new agent, old server (R-5)

This is as in revision 1. The old 600 s deadline can produce `timed_out`, and the late result is rejected.

## 8. Acceptance Criteria Mapping

**Naming.**
- Test names contain `SD_ACnnn` or `SD_Cn`. Messages start with `[AC-nnn]` or `[C-n]`.
- Shutdown tests use `shutdownRig` (§12 note 4). Its runner exits 0 by default.
- Unless a row says otherwise, tests are in `internal/agent/shutdown_clearance_test.go`.

| AC / condition | Fulfilled By | Tests |
|---|---|---|
| AC-001 | AD-001; AD-003; AD-009 | `TestShutdown_SD_AC001_FreeLockPowersOffWithNoWait` (`shutdown initiated`, 1 poweroff, 2 probes, 0 gate pauses); `TestShutdownClearance_SD_AC001_FreeRunsGuardsThenFinalProbe` (probe → guards → probe → launch) |
| AC-002 | AD-001 held | `TestShutdown_SD_AC002_HeldLockNeverPowersOffWithinBound` |
| AC-003 | AD-001; AD-004 (b) | `TestShutdown_SD_AC003_ReleaseAfter3mPowersOffWithin10s` |
| AC-004 | AD-001 timeout | `TestShutdown_SD_AC004_TimeoutSkipsAndNeverForces`; `TestShutdown_SD_AC004_NoSecondWaitAfterSkip` |
| AC-005 | AD-004 (c) | `TestShutdown_SD_AC005_TimeoutResultFailedAndNamesHolder` (exact text; not `timeout`; no `initiated`) |
| AC-006 | `holderText` | `TestShutdown_SD_AC006_TimeoutReportSaysHolderNotVisible` |
| AC-007 | AD-001 unknown and `sawHeld` | `TestShutdown_SD_AC007_UnknownSkipsImmediatelyWithoutWait`; `…_HeldThenAbsentNeverPowersOff`; `…_ZeroStatusSkipsAsUnknown` |
| AC-008 | AD-003; `dpkglock_other.go`; AD-009 | `TestShutdown_SD_AC008_AbsentPowersOffAsToday` (exactly `shutdown initiated`, no `dpkg`, no wait line); `TestShutdown_SD_AC008_RunnerGetsUnchangedArgv`; `TestExecShutdown_SD_AC008_WindowsArgvUnchanged` (AST: the literals `"shutdown"`, `"/s"`, `"/t"`, `"5"` inside `execShutdown`'s real branch); `TestPlatformProbe_RD_AC012_NonLinuxIsAbsent` (existing) |
| AC-009 | AD-003 step 1 | `TestShutdown_SD_AC009_AllowShutdownOffRefusedWithoutLockCheck` (unchanged message; 0 probes; 0 launches) |
| AC-010 | AD-001 step 5; AD-003 | `TestShutdown_SD_AC010_AllowShutdownOffDuringWaitSkipsNamingGuard` (unit-test-only state); `TestShutdownClearance_SD_AC010_GuardOrderIsShutdownThenAllowShutdownThenMaintenance` |
| AC-011 | AD-003 strict maintenance | `TestShutdown_SD_AC011_MaintenanceStartsDuringWaitSkipsNamingGuard` (reason `conflict`) |
| AC-012 | AD-001 | `TestShutdown_SD_AC012_FinalCheckRetakenKeepsWaitingWithinBound` |
| AC-013 | AD-001; AD-004 (e) | `TestShutdown_SD_AC013_AgentStopEndsWaitWithin5s`; `TestShutdown_SD_AC013_ShutdownResultReasonShutdown` |
| AC-014 | AD-002; AD-004 | `TestShutdown_SD_AC014_NoPoweroffAfterSkippedResult`; `TestShutdown_SD_AC014_CommandBudgetExhaustedIsATruthfulSkip` |
| AC-015 | AD-009 `launchFailed` | `TestShutdown_SD_AC015_LaunchErrorIsFailedResult` (the runner returns a plain error → `shutdown: <err>`, `error`) |
| AC-016 | AD-004 (b); AD-005 | `TestShutdown_SD_AC016_NineMinutesFiftyWaitReportsStarted`; `TestSweep_SD_AC016_ShutdownNotTimedOutBefore20m` in `server/commands/sweeper_test.go` |
| AC-017 | AD-005 | `TestCommandPolicy_SD_AC017_ShutdownBudgetCoversWait` in `models/command_policy_test.go`; the updated `TestCL_AC029_*`; `TestCL_DeadlineInvariant` (unchanged) |
| AC-018 | AD-001 `Bound`; AD-005 | `TestPowerClearance_SD_AC018_EachGateSkipsAtItsOwnBound` (custom specs with 3m and 7m bounds); `TestShutdownClearance_SD_AC018_ShutdownGateUsesShutdownBound`; `TestCommandPolicy_SD_AC018_BudgetConstantsDoNotReferenceEachOther` (AST) |
| AC-019 | AD-008 | Every existing `*_RD_*`, `*_QA_*` and `*_PF_C8_*` test, after the AD-008 edits only; `TestRebootGate_SD_AC019_RebootSpecUnchanged` (`rebootGate()` bound, message and `allowOffText == "commands.allow_reboot is off"`; budgets 10 / 12 / 20) |
| AC-020 | AD-002; AD-006 | `TestShutdownCallSites_SD_AC020_ExecShutdownHasExactlyOneGuardedCallSite`; `…_GateClearanceFeedsExecShutdown`; `TestShutdownCallSites_SD_C3_SelfTest` (the revision 1 shapes, plus a cross-action clearance); `TestPowerPaths_SD_C4_OnlyExecRebootAndExecShutdownLaunchPowerActions`; `TestRebootAuth_PF_C8_*` and `TestRebootCallSites_RD_AC020_GateClearanceFeedsEveryExecReboot` (unchanged) |
| AC-021 | host key (existing) | `TestShutdown_SD_AC021_SecondShutdownOrRebootDuringWaitIsHostBusy` |
| AC-022 | recovery (existing) | `TestShutdown_SD_AC022_RestartDoesNotRerunShutdown`; `TestCL_AC022_SelfUpdateMarkerNotReported` (existing) |
| AC-023 | AD-007 | `TestShutdownClearance_SD_AC023_FullWaitBoundedOutput` (≤ 303 probes; no permit; state dir unchanged; exactly 2 INFO `dpkg lock` lines; 0 DEBUG lines); `TestShutdown_SD_AC023_FullWaitOneResultOnlyTheFailedCommandEvent` (1 result; exactly 1 agent event, the existing `Command shutdown failed: …` warning; `telemetryPending() == 0`) |
| AC-024 | AD-002; AD-010 | `TestShutdown_SD_AC024_RunsOnlyTheExistingPoweroffArgv`; `TestExecShutdown_SD_AC008_WindowsArgvUnchanged`; `TestPowerGuard_SD_C1_RealBranchesConsultTheGuardFirst` (the Linux argv literals `"sudo"`, `"systemctl"`, `"poweroff"` unchanged); `TestDpkgProbe_RD_AC021_NoExecNoSudo` (existing); DoD: no diff under `scripts/` |
| AC-025 | No web change | DoD: `git diff --stat origin/main -- web/src/pages/DeviceDetail.tsx web/src/components` is empty; `npm run test:run` green |
| AC-026 | Writer | QA greps (§13) |
| AC-027 | AD-009 `exitedZero` | `TestShutdown_SD_AC027_ExitZeroReportsInitiated` (table: no wait → `shutdown initiated`; 3m wait → `… after waiting 3m …`) |
| AC-028 | AD-009 `exitedNonZero` | `TestShutdown_SD_AC028_NonZeroExitReportsFailed` (a `fakeExit{code: 1}` with sudo's stderr → `shutdown: exit status 1: sudo: a terminal is required …`, `error`, no `initiated`) |
| AC-029 | `sanitizePowerErr` | `TestSanitizePowerErr_SD_AC029_PrintableAndAtMost200Bytes` in `poweroff_test.go` (ESC sequences, `\r\n\t`, NUL, invalid UTF-8, a 4 KiB input, a multi-byte rune at byte 200 → printable only, ≤ 200 bytes, valid UTF-8); `TestShutdown_SD_AC029_LongErrorIsBoundedInTheResult` |
| AC-030 | AD-009 timer and reaper | `TestShutdown_SD_AC030_StillRunningAtBoundIsInitiatedAndReaped` (the runner blocks; the fake clock advances 3 s; the result is `success` with fake time from launch to result == 3 s; release the runner → `poweroffRunning` false, by `require.Eventually`); `TestShutdown_SD_AC030_SecondLaunchWhileChildRunsIsRefused`; `TestShutdown_SD_AC030_LateNonZeroExitLogsOneWarn` (and a late exit 0 logs nothing); `TestExecShutdown_SD_AC030_ProductionBranchStartsThenWaits` (AST: the real branch calls `Start` and then `Wait` on the same `*exec.Cmd`, sets `WaitDelay`, and does not use `CommandContext`) |
| AC-031 | AD-005 | `TestCommandPolicy_SD_AC031_StartCheckBoundFitsBudget` (`ShutdownStartCheckBound > 0`; `ShutdownDpkgLockWaitBound + 7s + ShutdownStartCheckBound < ShutdownAgentHardMax`) |
| AC-032 | AD-009 shared mapping; AD-008 | `TestPoweroffExit_SD_AC032_SeamAndProductionErrorsMapAlike` in `poweroff_test.go`. Real errors from the helper-process pattern (exit 0, exit 1; on Linux a self-SIGTERM, giving a code of −1) and a real `Start` error for a missing program are compared with the seam's `nil`, `fakeExit{1}`, `fakeExit{-1}` and a plain error: the same `Kind` and the same `classifyPoweroff` outcome. `TestShutdown_SD_AC032_SignalTerminationIsInitiated`; `TestExecReboot_SD_AC032_RebootLaunchUnchanged` (AST: `execReboot` has no `Wait`, no `NewTimer`, the same argv literals, and `Start()`) |
| AC-033 | Writer (§4) | QA checklist (§13) |
| **C-1** | AD-010 | `TestPowerGuard_SD_C1_TestBinaryIsDetected`; `TestPowerGuard_SD_C1_RealBranchesConsultTheGuardFirst`; `TestPowerGuard_SD_C1_SelfTest` (all in `powerguard_test.go`). **No test calls a power exec with a nil runner** |
| **C-2** | AD-009; writer; follow-up | The AC-027 to AC-032 tests; README per §4; SHUTDOWN-SUDO-001 recorded in the implementation report; DoD: no `scripts/` diff |
| **C-3** | AD-001 step 5; AD-002; AD-006.2 | `TestPowerCallSites_SD_C3_GateEntryPointsBindTheAction`; `TestPowerCallSites_SD_C3_SelfTest`; `TestPowerClearance_SD_C3_AllowFlagGatesTheMintWhateverTheGuards`; runtime: `TestExecShutdown_SD_C3_NonClearedPermitStartsNoPoweroff`; `TestExecShutdown_SD_C3_RebootPermitIsRefusedAndNotConsumed`; `TestExecReboot_SD_C3_ShutdownPermitIsRefusedAndNotConsumed`; `TestExecShutdown_SD_C3_RefusalRunsNothing`; `TestExecShutdown_SD_C3_ClearedPermitStartsExactlyOnce` |
| **C-4** | AD-006.3 | `TestPowerPaths_SD_C4_OnlyExecRebootAndExecShutdownLaunchPowerActions`; `TestPowerPaths_SD_C4_SelfTest` (in `power_paths_test.go`) |
| **C-5** | Writer; §10 | QA checklist (§13) |

## 9. Error Handling

| Failure | Behavior | Log |
|---|---|---|
| `allow_shutdown` false at start | Refused as today; no probe | none new |
| Not a dpkg host / Windows | `Absent`; launch | none |
| Any REBOOT `Unknown` cause; `Held` then `Absent`; an unrecognized state | Immediate skip (d) | `wait ended` if it waited |
| Held past 10m | (c) with the holder | both gate lines |
| Guard fails, including the gate-level allow flag | (f) naming the guard | `wait ended` if it waited |
| Agent stopping (including an operator restart) | (e), reason `shutdown` | `wait ended` if it waited |
| `cr.ctx` deadline | (f) `agent command time budget exhausted` | `wait ended` |
| A permit that is zero, stale, used or for the wrong action | (g) `shutdown: shutdown refused: …`; nothing runs | none |
| A poweroff child is still alive from an earlier command | (g) `shutdown: a previous poweroff command is still running` | none |
| Launch error | (g) `shutdown: <err>` | none |
| Normal non-zero exit within 3 s | (g) `shutdown: exit status <n>[: <output>]` | none |
| Exit 0, or killed by a signal, within 3 s | (a) / (b) | none |
| Still running at 3 s | (a) / (b); the reaper continues | ≤ 1 Warn if it later exits non-zero |
| A real power exec in a test binary with no runner | `errPowerProcessInTest` → (g) / `reboot: …`; no process | none |
| Agent hard-killed mid-wait | No poweroff; the marker is cleared without a result; the server times out at 20m | the existing recovery line |

## 10. Security Considerations

- **No privilege added (NFR-002, FR-031).** The probe is unchanged. The poweroff argv is byte-identical. The start
  check only reads the exit status of a child the agent already launched. No `scripts/` change. SHUTDOWN-SUDO-001 owns
  the privilege question.
- **Fail closed (BR-001).** Every `Unknown`, zero or unrecognized state skips.
- **The gate can only prevent or delay (BR-003).**
  - The pre-check and the gate's own allow-flag step both require `allow_shutdown`.
  - Only the gate mints, and only after that step in the same attempt.
  - Nothing retries.
- **Permit hardening.** Four layers:
  - the type is unexported, and its literal exists only in `mintPowerPermit`;
  - selectors of the permit fields and of the live/seq fields are confined to mint and consume, so no permit can be
    forged by assignment;
  - the runtime check is action-bound and single-use, and a wrong-action permit is refused without being consumed;
  - the AST checks cover call sites, entry points and guards (AD-006).
- **Only power paths (C-4).** A recursive structural scan of the agent code and its binary covers process launches,
  `systemctl` verbs and targets, shells, constant-folded strings, and `syscall`/`unix`/`windows` power calls. Rule (b)
  has no file exemption: `poweroff.go` holds no `poweroff` / `halt` string literal (revision 3, AQ-1).
- **Test safety (C-1).** In a test binary, no power process can start, whatever the harness (AD-010).
- **Truthful result (BR-006).** A refusal is reported as failed. Success means the OS accepted the command, or it was
  still running or signal-killed at the 3 s bound (R-10).
- **Notifier exposure (corrected, INFO-3).** Every failed shutdown result, (c) to (g), produces the existing
  `EventCommandCompleted` warning `Command shutdown failed: <message>` in `completeCommand`. Discord, Slack, ntfy and
  gotify forward it unescaped. So the skip texts reach the notifiers, including the holder name, pid and uid. Three
  things bound this:
  - REBOOT C-5: holder names limited to `[A-Za-z0-9._+:-]`, no `[`, `]`, `/` or whitespace; causes are fixed strings
    of ≤ 200 bytes;
  - for (g), `sanitizePowerErr` (printable, ≤ 200 bytes);
  - `<err>` comes from `sudo`, `systemctl` or `shutdown.exe` running a fixed argv, so it carries no attacker-chosen
    input.

  The exposure is the same as the standalone reboot's. It is not new.
- **REBOOT-DPKG-001 conditions.**
  - C-1, C-2, C-5 and C-6: the probe is untouched.
  - C-3: extended.
  - C-4: the server-first rule is extended to shutdown.
  - C-7: extended in §13.

**Residual risks:**
- **R-1 (NFR-004):** the gap between the final probe and the launch is sub-second to a few seconds. Accepted.
- **R-2:** inherited pid-namespace and `/proc` limits. A containerized agent, or one with `PrivatePIDs=yes`, skips
  every shutdown on a dpkg host, visibly.
- **R-3:** inherited inode-only match, unverified on real hardware.
- **R-4 (accepted, NFR-008):** any local user who holds the dpkg lock can delay or veto rIOt shutdowns. Each veto is
  visible with the pid and uid.
- **R-5:** version skew, as in revision 1. Upgrade the server first. A re-sent shutdown after a misleading
  `timed_out` can power the host off again if it is powered on within the 15-minute TTL.
- **R-6 (resolved for truthfulness; privilege deferred).** The installer has never granted `systemctl poweroff`. With
  AD-009, those hosts now report **failed** (`shutdown: exit status 1: sudo: …`) instead of a false `shutdown
  initiated`. This is expected and documented (FR-033). Granting the privilege is SHUTDOWN-SUDO-001.
- **R-7 (accepted, NFR-007; corrected per C-5).**
  - A shutdown can power the host off up to about **25 minutes** after the click: 15 minutes queued while the device
    is disconnected, plus up to about 10 minutes of wait. When the device is connected, the delay is up to about 10
    minutes.
  - The host stays off until someone powers it on.
  - The dashboard cannot cancel. **Restarting the agent on the host (`systemctl restart riot-agent`) cancels a waiting
    shutdown** as outcome (e), with no re-run.
  - Editing `allow_shutdown` is not a cancel path: the config is read at start-up only.
  - A dashboard cancel and progress display are a follow-up.
- **R-8:** the host-busy window is up to about 10m07s; after a hard kill, the server-side block lasts up to 20
  minutes.
- **R-9:** the shared live permit slot is serialized by the host key. If it ever overlapped, the result would fail
  closed.
- **R-10 (new):** a poweroff child killed by a signal within 3 s counts as initiated. If an unrelated signal (an
  operator restart of the agent in that window, or the OOM killer) killed `sudo` before `systemctl` had enqueued the
  job, the result would say initiated while nothing happened. The window is under 3 s and needs a coincident external
  kill. A refusal is never a signal.
- **R-11 (new):** the C-4 scan is AST-only.
  - Constants folded through other modules, `fmt.Sprintf`, runtime string building, or D-Bus `Reboot` method strings
    are not caught.
  - A D-Bus or `x/sys/windows` library would need a `go.mod` change, which the DoD forbids for this story and review
    catches later.
  - `cmd/riot-agent-apply` is out of scope.
- **R-12 (new):** C-1 relies on `testing.Testing()`. A test that sets `a.runner` to a function that itself runs a
  real power tool is outside any in-code guard. AD-006.3 does not scan `_test.go` files.

## 11. Performance Considerations

- **Common case:** two probes and one guard evaluation (milliseconds), then the poweroff launch. The handler returns
  as soon as the child exits, normally under 1 s, and at most 3 s after launch.
- **During a wait:** ≤ 303 probes in O(1) memory.
- **Poweroff child:** ≤ 4 KiB of output captured, one goroutine, one timer.
- **Server:** no code or query change. A `running` shutdown row may stay 10 minutes longer.
- **No caching, indexing or rate limiting is required.**

## 12. Implementation Notes for Engineers

1. **Commits** (`[SHUTDOWN-DPKG-001] <type>: <what>`), in this order:
   1. models, including `ShutdownStartCheckBound`, plus the policy and server tests;
   2. `powerguard.go` and its tests. **Land C-1 before anything makes the shutdown path testable**;
   3. the gate and permit generalization, including the AD-001 step 5 guard step, plus the AD-008 mechanical edits.
      **Reboot tests must be green here**;
   4. `poweroff.go` and its tests;
   5. `handleShutdown`, `execShutdown`, the guards and the results, plus `shutdown_clearance_test.go`;
   6. the call-site, binding and power-path AST tests.
2. **Keep `reboot_clearance.go` as the file name.** `dpkglock_test.go:787` scans it.
3. **Moving the loop.** Cut and paste, then make the five AD-001 changes. Review with `git diff --color-moved`.
4. **`shutdownRig`** (in `shutdown_clearance_test.go`):
   - It embeds `*gateRig` and sets `AllowShutdown = true`.
   - It wraps `a.runner`. For `sudo systemctl poweroff` it records the fake time, increments `poweroffs`, optionally
     blocks on `r.poweroffBlock` (a channel), and returns `r.poweroffResult`, which is one of: `nil`, `fakeExit{code}`
     (a type with `Error()` and `ExitCode() int`), or a plain error. It does **not** delegate, so `r.reboots` counts
     only reboots.
   - `newShutdownRig` ends with `require.NotNil(t, r.a.runner)`.
   - It is the only way tests submit a `shutdown`. AD-010 backs this up if a test forgets.
5. **The start-check timer under the fake clock.** `execShutdown` uses `a.clock().NewTimer`. In AC-030 tests, block
   the runner, wait until the fake clock has the start-check timer pending (with `require.Eventually` on the existing
   `fakeClock.timerDurations()`; revision 3: no new accessor), then `Advance(models.ShutdownStartCheckBound)`. Runners
   that do not block return before the timer matters.
6. **The AC-032 helper-process pattern.** `poweroff_test.go` re-executes `os.Args[0]` with
   `-test.run=TestPoweroffHelperProcess` and an environment variable that makes it exit with a given code. On Linux
   it can also send itself SIGTERM. This yields a real `*exec.ExitError` without any power tool. `TestPoweroffHelperProcess` returns at once when the
   variable is not set. A missing program (a random name) gives a real `Start` error.
7. **Never call a power exec with a nil runner in a test, even to "prove" the guard** (C-1 b).
8. **Never use `exec.CommandContext` for the poweroff.** Never `Kill` the child. Always reach `Wait` in the goroutine.
9. **`sanitizePowerErr` is applied once, in `handleShutdown`**, to every `execShutdown` error. Only `poweroff.go`
   builds the raw text, with one exception: `execShutdown` builds the "still running" error itself (revision 3).
10. **The constant folder** (AD-006.3) reads `const` declarations only. Keep it in the test file, with no `go/types`
    and no new dependency.
11. **Host verification (read-only, optional).** Record in the implementation report whether `riot` has a poweroff
    rule on Hawaii (for example `sudo -n -l` run as `riot`). **Do not run a real shutdown on any host.** Only Ryan may,
    because a powered-off host needs physical or out-of-band access.
12. **Follow-up ID:** the implementation report records SHUTDOWN-SUDO-001 (C-2 b).

## 13. Definition of Done

- [ ] Every Section 4 change is implemented. `git diff --stat` shows nothing under `scripts/`, `web/`, `cmd/`,
      `internal/server/` non-test code, `internal/agent/dpkglock*.go`, the migrations or `go.mod` / `go.sum`
- [ ] Every Section 8 test exists with an `SD_ACnnn` / `SD_Cn` name and `[AC-nnn]` / `[C-n]` messages, and passes
- [ ] Every REBOOT-DPKG-001 test passes with only the AD-008 edits
- [ ] `go test ./...` (Windows; `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`) and `go test -race ./...`
      (Linux CI) are green; `cd web && npm run test:run` is green
- [ ] `go vet ./...` is clean, and `make build-agent-all` succeeds for every target
- [ ] No migration
- [ ] Non-test greps under `internal/agent`:
  - `powerPermit{` matches only in `mintPowerPermit`;
  - `rebootPermit\b` matches nothing;
  - `-i '"poweroff"'` (the quoted string literal; identifiers such as `poweroffExit` do not count) matches only in
    `execShutdown`;
  - `testing.Testing` matches only in `powerguard.go`;
  - `time.Now()\|time.Sleep` matches nothing in `dpkglock*.go`, `reboot_clearance.go` or `poweroff.go`.
- [ ] Budgets:
  - `AgentHardMax("shutdown",1) == 12m`, `ExecDeadline == 20m`, `ExecDeadlineSeconds == 1200`, `QueuedTTL == 15m`,
    `ShutdownStartCheckBound == 3s`;
  - the reboot values are unchanged.
- [ ] Every test that submits a `shutdown` uses `shutdownRig`
- [ ] The implementation report records SHUTDOWN-SUDO-001 and the optional read-only sudo finding
- [ ] **QA gate (security C-6, extending REBOOT C-7).** These pass:
  - the AC-004, AC-007, AC-009, AC-012, AC-013, AC-014, AC-016, AC-018, AC-019, AC-020, AC-023 and AC-027 to AC-032
    tests;
  - the C-1 tests (`TestPowerGuard_SD_C1_*`);
  - the C-3 runtime and AST tests, including both cross-action refusal tests and
    `TestPowerClearance_SD_C3_AllowFlagGatesTheMintWhateverTheGuards`;
  - the C-4 tests (`TestPowerPaths_SD_C4_*`);
  - the unchanged C-8 test `TestRebootAuth_PF_C8_ExecRebootHasExactlyThreeGuardedCallSites`.

  QA also confirms that the DoD greps are clean and that `git diff --stat` shows nothing under `scripts/`,
  `web/src/pages/DeviceDetail.tsx`, `web/src/components`, `internal/agent/dpkglock*.go`, the migrations or `go.mod`.
- [ ] **Docs QA checklist (AC-026, AC-033, C-2, C-5).**
  - `grep -n "not gated" README.md web/src/pages/LearnMore.tsx` finds nothing.
  - README L2171 shows 20 minutes.
  - The shutdown section states:
    - no poweroff sudo rule, so failed until SHUTDOWN-SUDO-001;
    - the meaning of "initiated";
    - about 10 minutes, or about 25 minutes if disconnected;
    - restart the agent to cancel;
    - the local-user veto.
  - Nothing presents editing `allow_shutdown` as a cancel path.
  - No poweroff row was added to the Sudoers Rules table.
  - The CHANGELOG has the server-first upgrade note.
