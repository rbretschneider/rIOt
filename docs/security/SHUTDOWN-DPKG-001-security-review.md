# Security Review Report
**Story ID:** SHUTDOWN-DPKG-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-25
**Verdict:** APPROVED WITH CONDITIONS

Inputs:
- `docs/requirements/SHUTDOWN-DPKG-001-frd.md`, `docs/architecture/SHUTDOWN-DPKG-001-add.md` (revision 1)
- `docs/security/REBOOT-DPKG-001-security-review.md` (conditions C-1 to C-7), `docs/architecture/REBOOT-DPKG-001-add.md`
- Code: `internal/agent/reboot_clearance.go`, `commands.go` (`executeAction`, `handleReboot`, `maybeRebootAfterPatch`,
  `execReboot`, `handleShutdown`), `reboot_auth.go` (`runAuthorizedReboot`, `rebootAfterDelivery`),
  `command_lifecycle.go` (`newCmdRun`, `runAction`, `completeCommand`, `recoverInterruptedCommands`),
  `maintenance.go` (`maintenanceBlockerFor`), `config.go`, `reboot_callsites_test.go`,
  `command_lifecycle_test.go`, `internal/models/command_policy.go`, `internal/server/commands/service.go` (`Cancel`),
  `scripts/install.sh` (sudoers block), `README.md`, `.github/workflows/*.yml`
- `git log -S poweroff -- scripts/install.sh` (no commit has ever added or removed a poweroff rule)

---

## Threat Model Summary

### Assets
- **Package-state integrity.** A poweroff must not cut a dpkg transaction (BR-001).
- **Host availability.** Unlike a reboot, a shutdown leaves the host off until someone powers it on (physical or
  out-of-band access).
- **Truth of the command result.** The operator may send `shutdown` as a containment action ("take this box off the
  network now"). A false `shutdown initiated` result gives false assurance.
- **The power-path invariants.** Only `handleShutdown` can power off. Only the three reviewed reboot callers can reboot.
  A permit for one action can never start the other.
- **The developer workstation and CI runners.** This story makes `handleShutdown` reachable from tests for the first
  time.

### Threat Actors
- **Local unprivileged user on a managed host.** Can hold or flood locks, so can delay or veto rIOt shutdowns (R-4,
  inherited from REBOOT SEC-002). Gains nothing else here.
- **Admin-session holder (dashboard).** Can already send `shutdown` where `allow_shutdown` is on. This story only
  changes the timing.
- **Device-key holder or compromised agent.** Already holds every agent privilege. Nothing new.
- **Future developer (regression).** The realistic actor for the permit, call-site and argv rules: a refactor or a
  new test that bypasses the gate or launches a real power action.
- **Environment and version skew.** An agent that auto-updates ahead of the server; a hard-killed agent; a host whose
  sudoers lacks a poweroff rule.

### Attack Surface Introduced
- A new wait of up to about 10m07s inside the `shutdown` command, holding the host key on the agent and (on a hard
  kill) the host-keyed server row for up to 20 minutes.
- New budget values for `shutdown` (agent 12m, server 20m). The queued TTL is unchanged at 15m.
- The REBOOT probe output (holder name, pid, uid, causes) now also reaches the `shutdown` result, and through the
  generic failed-command warning event, the notifiers.
- A shared, action-bound permit (`powerPermit{id, action}`) with one live slot for both power actions.
- A test seam (`a.runner`) on the poweroff path.
- No new endpoint, sudoers rule, capability, file, column, migration or dependency.

---

## Findings

There are no CRITICAL or HIGH findings. The gate generalization keeps the REBOOT-DPKG-001 failure direction: every
enumerated branch either delays the shutdown or skips it, and none forces it. The findings cover two things. First,
safety properties the ADD rests on convention rather than code (SEC-001, SEC-003, SEC-004). Second, a pre-existing
truthfulness defect that this story makes more visible (SEC-002).

### MEDIUM

#### SEC-001: Only test convention keeps the test suite from powering off Ryan's workstation or a CI runner
**Severity:** MEDIUM
**Domain:** Business logic safety / test isolation (NFR-006)
**Location:** ADD AD-002 (`execShutdown` non-runner branch), §12 note 5; `commands.go` `execReboot` (same pattern,
pre-existing)
**Description:**
Before this story, no test could reach a real poweroff, because `handleShutdown` did not use the runner seam and the
REBOOT rule said "never call `handleShutdown` in tests". The ADD makes the handler testable. Its only protection
against a real poweroff is that every shutdown test uses `shutdownRig` and that the rig asserts
`require.NotNil(t, r.a.runner)`. That assertion protects the rig only. Any other harness can still reach the real
branch:
- `newLifecycleHarnessAt` sets a `Free` probe and **no runner**.
- The gate clears at once on a free or absent lock.
- `execShutdown` then runs `shutdown /s /t 5` on Windows, or `sudo systemctl poweroff` on Linux.

The DoD runs `go test ./...` on Ryan's Windows machine. CI runs on `ubuntu-latest`, which has passwordless sudo.
`execReboot` has had the same latent hazard since REBOOT-DPKG-001 (`shutdown /r /t 5`).

**Attack Scenario (regression):**
1. A later story adds a lifecycle test that sends `shutdown` with `AllowShutdown = true`, for example a table test over
   every host-keyed action. It uses `newLifecycleHarness` because nothing tells the author not to.
2. Ryan runs `go test ./...` on the Windows workstation. The gate clears, `execShutdown` takes the non-runner branch,
   and Windows powers off 5 seconds later, closing every open session and losing unsaved work.
3. In CI the same test powers off the GitHub runner, and the job dies with no useful error.

**Required Resolution (properties):**
- (a) In a test binary, the real-process branch of `execShutdown` **and** `execReboot` cannot start a process,
  whatever harness, config or runner state the test sets up. This must be enforced in package code, not by a harness
  convention.
- (b) This property is verified without ever executing the real branch, for example by checking the guard's state
  plus an AST check that the real branch consults it. A test that invokes the real branch to prove it is refused is
  itself the hazard.
- (c) The production binary's behaviour is unchanged (argv byte-identical, AC-024).

**Blocks:** No. Condition C-1.

---

#### SEC-002: `shutdown initiated` is reported when sudo refuses the poweroff (pre-existing; ADD R-6)
**Severity:** MEDIUM (pre-existing; not introduced by this story)
**Domain:** Data integrity / truthfulness of a security-relevant control (FR-015, FR-016)
**Location:** ADD AD-002 `execShutdown` body, AD-004 "What 'initiated' means", §10 R-6; `scripts/install.sh:436`;
`commands.go:392-402`
**Description:**
- **No poweroff rule, ever.** `install.sh` writes `riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} reboot` and no poweroff
  rule. `git log -S poweroff -- scripts/install.sh` returns no commit, so no installer version ever granted one. On
  every Linux host whose sudo policy comes only from the installer, `sudo systemctl poweroff` therefore fails:
  - `riot` has no rule for it;
  - the agent runs under systemd with no TTY;
  - sudo exits non-zero at once ("a terminal is required to read the password"), and the attempt is logged as an
    authentication failure.
- **Why the result is wrong.** The agent calls `cmd.Start()` and never waits, so the result is
  `success, "shutdown initiated"` and the host stays up.
- **What this story changes.** It keeps that behaviour. It makes the message more convincing
  (`shutdown initiated after waiting 3m for the dpkg lock`). It also adds unit tests that pin `shutdown initiated` as
  the success text.
- **Test path and production path differ.** The runner seam returns the command's exit status, so the AC-015 test
  proves a failure mapping that the production path (`Start()` without `Wait()`) can never produce.
- **Zombie process.** Each failed sudo stays an unreaped zombie child of the agent until the agent restarts. This is
  bounded by operator clicks, so it is not unbounded growth.
- **FR-015 conflict.** FR-015 says the result must never say initiated "unless the agent started the poweroff". On
  these hosts the agent did not start the poweroff. AD-004 redefines "initiated" as "`cmd.Start()` succeeded", which
  is a narrower reading than the FRD's.

Fixing the privilege is correctly out of scope (NFR-002, AGENT-PRIV). Fixing the truthfulness needs no privilege and no
argv change: the sudo child's exit status can be observed within a short bound. `systemctl poweroff` enqueues the job
and returns, and a sudo denial returns within milliseconds.

**Attack Scenario (incident response):**
1. Ryan sees signs of compromise on a Raspberry Pi and clicks **Shutdown** to contain it.
2. The result reads `shutdown initiated`, and the dashboard row is `succeeded`.
3. sudo refused the poweroff. The host stays up, on the network, and the attacker keeps access. Ryan believes it is
   off and does not pull the plug.

**Required Resolution:** This needs Ryan's decision (see below). Whichever option Ryan picks, the minimum is:
- (a) The README shutdown section ("What you see" and "Known limits") states plainly that the installer grants no
  poweroff rule, and that on such hosts `shutdown initiated` means only that the command was launched, not that the
  host is powering off.
- (b) The R-6 follow-up story is filed: either a privilege grant through the AGENT-PRIV capability path or a truthful
  start check. The implementation report records its ID.
- (c) No change in this story adds privilege or alters the poweroff argv.

**Option A (recommended).** Also make the result truthful in this story:
- the shutdown result reflects sudo's exit status, observed within a short, stated bound;
- a non-zero exit, or no exit within the bound, gives the existing failed form `shutdown: <err>` (FR-016);
- the child is reaped;
- the argv and sudoers are unchanged;
- the runner seam and the production path then have the same semantics;
- reboot is untouched (FR-021).

**Option B.** Defer the whole fix to the follow-up. In that case, AD-004's definition of "initiated" stands, and the
FRD records it as an accepted deviation from FR-015.

**Blocks:** No. Condition C-2.

---

### LOW

#### SEC-003: The `allow_shutdown` guarantee and the permit rest on what the caller passes and on composite-literal counting
**Severity:** LOW
**Domain:** Authorization invariant (BR-003, FR-022, REBOOT C-3)
**Location:** ADD AD-001 (`awaitShutdownClearance(ctx, commandID, guards)`), AD-003 (`commonPowerGuards`), AD-006
checks 3 and the structural rules; `reboot_clearance.go:166-174` (`if guards != nil`)
**Description:**
- **The guards are caller-supplied, and nil is accepted.** The gate mints a shutdown permit after running *whatever
  guards the caller passes*, and it skips the guard step entirely when `guards == nil`.
  - The only thing tying the shutdown permit to `allow_shutdown` is AD-006 check 3, a **text** search for
    `Commands.AllowShutdown` in the function body before the call. A comment satisfies it.
  - A call such as `a.awaitShutdownClearance(ctx, id, nil)`, or one that passes
    `a.commonPowerGuards(func() bool { return a.config.Commands.AllowReboot }, …)`, mints a valid shutdown permit with
    no `allow_shutdown` check.
- **A permit can be forged without a composite literal.** The structural rule counts `powerPermit` composite
  literals. A permit can also be built as `var p powerPermit; p.id = a.powerPermitLive.Load();
  p.action = powerActionShutdown`. That passes the runtime CAS and the action check, and it uses no composite
  literal. The REBOOT rule had the same gap.

Both are regression risks, not exploitable paths. Only package `agent` code can do either, and no current code does.

**Attack Scenario (regression):** A later "shutdown after maintenance" helper reuses the gate. It passes
`a.commonRebootGuards(id)` because that function is already in scope. Shutdowns now run on hosts where `allow_shutdown`
is false but `allow_reboot` is true. The gate reports "cleared", and every test stays green.

**Required Resolution (properties):**
- (a) A shutdown permit can be minted only in an attempt in which the `allow_shutdown` guard evaluated true. This must
  hold structurally or at runtime, not by source text. Acceptable forms include:
  - the shutdown entry point binds its own guards;
  - an AST rule pins the guards argument of every `awaitShutdownClearance` and `awaitRebootClearance` call to
    `a.commonShutdownGuards()` / `a.commonRebootGuards(…)`, and `commonPowerGuards` is called only from those two;
  - `execShutdown` re-checks `allow_shutdown` and refuses.

  The same property applies to reboot and `allow_reboot`.
- (b) `powerPermitLive` and `powerPermitSeq` are referenced in non-test code only inside `mintPowerPermit` and
  `consumePowerPermit`. No non-test code assigns to a `powerPermit` field outside `mintPowerPermit`.
- (c) The self-test (`TestShutdownCallSites_SD_C3_SelfTest` or a sibling) includes synthetic sources that violate (a)
  and (b), and they are flagged.

**Blocks:** No. Condition C-3.

---

#### SEC-004: "Shutdown is the only poweroff path" is checked for two exact literals in one directory
**Severity:** LOW
**Domain:** Authorization invariant (FR-022, AC-020)
**Location:** ADD AD-006 `TestShutdownCallSites_SD_AC020_PoweroffArgvOnlyInExecShutdown`; `agentSourceFiles`
(`filepath.Glob("*.go")`, package directory only)
**Description:**
The rule allows `"poweroff"` and `"/s"` only inside `execShutdown`. Each of the following still powers off a host and
passes the rule:
- Windows `shutdown -s` or `shutdown /p`;
- `exec.Command("shutdown", "-h", "now")` or `"-P"`;
- `systemctl halt`, or `systemctl start poweroff.target` if the match is exact rather than substring;
- `init 0` / `telinit 0`;
- a constant built as `"power" + "off"`;
- `unix.Reboot(LINUX_REBOOT_CMD_POWER_OFF)`;
- a login1 `PowerOff` D-Bus call;
- any such code in `internal/agent/collectors` or `cmd/riot-agent`, which the glob does not scan.

The literal `"shutdown"` cannot be banned, because it is also the action name.

**Attack Scenario (regression):** A "Windows parity" change writes `exec.Command("shutdown", "-s", "-t", "0")` in a
new `power_windows.go` helper that is called after `allow_shutdown`, with no gate. The argv test and the call-site test
both pass. Shutdowns on Windows skip the gate and the permit.

**Required Resolution (properties):**
- (a) Outside `execReboot` and `execShutdown`, no non-test code under `internal/agent/...` (recursively) or
  `cmd/riot-agent` launches a process whose program is a power tool (`shutdown`, `halt`, `poweroff`, `reboot`,
  `init`, `telinit`), or `systemctl` with a power verb or target. This covers direct `exec.Command*` calls and calls
  through `a.runner` / `runCommand`.
- (b) No such code contains a string literal that matches `poweroff`, `power-off` or `halt` case-insensitively, or that
  folds to one through constant concatenation.
- (c) No such code calls `Reboot` from `syscall` or `golang.org/x/sys/unix`.
- (d) The self-test includes at least the Windows `-s` form and a subpackage file.

**Blocks:** No. Condition C-4.

---

#### SEC-005: A waiting shutdown can fire about 25 minutes after the click, cannot be cancelled from the dashboard, and leaves the host off
**Severity:** LOW
**Domain:** Availability / operator safety (FR-025, OQ-008)
**Location:** ADD §10 R-7, AD-005 (TTL unchanged), §4 README "Known limits"; `service.go` `Cancel` (queued only);
`config.go` (no hot reload)
**Description:**
- **R-7 understates the worst case.** It says "up to 10 minutes after the click". A shutdown sent while the agent is
  disconnected stays queued for up to 15 minutes. It then waits up to about 10m07s. That is about 25 minutes in
  total.
- **No dashboard cancel.** `Cancel` applies to queued rows only.
- **Flipping the config is not a cancel path.** The agent reads `agent.yaml` once at start-up (`LoadConfig` in
  `agent.go:139`; no reload). Changing `allow_shutdown` during a wait has no effect until the agent restarts. AC-010
  and §7.3 can therefore occur only in tests. Docs that present them as an operator control would mislead.
- **The real escape hatch is not documented.** Restarting the agent on the host ends the wait within about 3 seconds.
  The result is outcome (e), and recovery never re-runs the shutdown.

**Attack Scenario (operator surprise):**
1. Ryan clicks **Shutdown** on a host that is mid-`unattended-upgrades`.
2. Eight minutes later he SSHes in to see why it is still up and starts work.
3. The lock frees and the host powers off under him. It stays off until someone is physically present.

**Required Resolution:**
- (a) Correct R-7 and the README "Known limits" text: up to about 25 minutes after the click if the device was not
  connected when the command was sent, otherwise up to about 10 minutes.
- (b) The README states the escape hatch: restart the agent on the host (`systemctl restart riot-agent`) to cancel a
  waiting shutdown.
- (c) The README does not present editing `allow_shutdown` as a way to stop a waiting shutdown.

The cancel or progress follow-up is Ryan's call.

**Blocks:** No. Condition C-5.

---

### INFORMATIONAL

#### INFO-1: The action-bound permit is sound; the shared live slot is a liveness edge only
- **Forgery from outside the package.** Impossible: the type is unexported.
- **Forgery inside the package.** Possible only through the field-assignment route in SEC-003 (b).
- **Reuse.** Refused by the CAS. A stale permit is refused, because any newer mint replaces the live id.
- **Cross-use.** `consumePowerPermit` checks `p.action == want` **before** the CAS. A reboot permit handed to
  `execShutdown`, or the reverse, is therefore refused and leaves the live slot intact, so the rightful exec still
  works. The zero `powerActionNone` is never accepted. These properties are correct.
- **Shared slot.** A shutdown mint invalidates an unconsumed reboot permit, and the reverse. Overlap is not reachable
  today, because all four power paths take `TargetKeyHost` before entering the gate:
  - `executeAction` for standalone `reboot` and `shutdown`;
  - `TryLockAll` in `maybeRebootAfterPatch`;
  - `TryLockAll` in `runAuthorizedReboot`.

  If an overlap ever happened, the loser's exec would be refused with `err*NotCleared`. That is no power action at
  all, the safe direction, and visible in the result or event. It is a liveness edge, not a safety issue. A
  cross-action power event cannot result.
- **Effect on REBOOT-DPKG-001 conditions.**
  - C-1, C-2, C-5 and C-6 sit in the unchanged probe, and the gate body is moved verbatim (§12 note 3,
    `--color-moved` review).
  - C-3 is strengthened by the action field and by the entry-point binding rules, subject to SEC-003.
  - C-4 is extended to shutdown.
  - No condition is weakened. One small residual: a `powerClearance` built without `Bound` prints `(0s)` in
    `reason()`. Only test fixtures can build one, and the AD-008 fixture edit covers it.

#### INFO-2: Budgets, host key and version skew are bounded
- **Budgets.** The agent budget of 12m sits above the about 10m07s gate worst case. If `cr.ctx` expires, the outcome
  is the truthful (f) skip, never a poweroff. The server deadline of 20m exceeds 12m + 5m.
- **TTL.** Keeping the queued TTL at 15m is correct, because a longer TTL would let a stale shutdown fire later.
- **Host key.** It is held for at most about 10m07s, and for up to 20m server-side after a hard kill (R-8). Every
  failure path releases it through `runAction`'s deferred `releaseLocks`.
- **New agent, old server.** R-5 is accurate. The old 600 s deadline can mark `timed_out` while the agent is still
  waiting, and the late result is rejected. If the lock frees near the end, the host is off while the row says
  `timed_out`.
- **Re-sent shutdown.** A shutdown re-sent after such a `timed_out` sits queued for 15m. It powers the host off again if
  someone powers it on within that window. Same class as REBOOT SEC-004. The server-first note (C-4 extended) is the
  correct and sufficient response.
- **Old agent, new server.** No behaviour change. A lost result is detected at 20m instead of 10m.

#### INFO-3: Information disclosure is the same as for the standalone reboot; one ADD statement is inaccurate
- **What the result carries.** The holder text (`[A-Za-z0-9._+:-]`-limited name, pid, uid, lenient-match marker) and
  the causes (fixed strings, at most 200 bytes) reach the `shutdown` result exactly as they reach the reboot result.
  The result is masked and sanitized in `completeCommand`.
- **The inaccurate statement.** ADD §10 says "the shutdown path sends no event, so no new notifier exposure". In fact,
  `completeCommand` sends `EventCommandCompleted` (warning) with `"Command shutdown failed: <message>"` for every
  error result. Skip texts, including holder name and uid, therefore reach Discord, Slack, ntfy and gotify.
- **Why it is not a new risk.** The standalone reboot has the same exposure, and the C-5 charset keeps it markdown- and
  link-inert: no `[`, `]`, `/` or whitespace in the attacker-controlled part. The uid disclosure is low-sensitivity.
  The §10 bullet must be corrected (C-5).

#### INFO-4: Stale-content note in the ADD
ADD §4 lists "README Sudoers Rules table `sudo systemctl poweroff` row" as stale content to log. The current README
has no poweroff row: `README.md:551` lists only `/usr/bin/systemctl reboot`, and `grep -i poweroff README.md` finds
nothing. The item is already resolved. The technical writer should not reintroduce it.

#### INFO-5: Bounded growth holds
Per shutdown command:
- at most one wait and at most 303 probes, each capped at 1 MiB and O(1) memory;
- at most two Info lines from the gate;
- exactly one result and one existing failed-command event;
- no persistent state;
- at most one live permit per agent.

The only per-failure residue is the unreaped sudo zombie (SEC-002). It is bounded by operator actions, and Option A
removes it.

---

## Positive Observations
- **The gate is generalized, not copied.** It uses a three-field spec, returned by functions rather than held in
  mutable globals. Each entry point is pinned by AST to its own spec, so no call site can pair one action with the
  other's bound.
- **The permit is action-bound.** A wrong-action permit is refused *before* the CAS, so a cross-action mistake cannot
  consume the rightful permit.
- **The fail-fast pre-check is unchanged.** The `allow_shutdown` refusal keeps its text and runs no probe and no wait
  (AC-009).
- **The maintenance guard is strict for shutdown.** No run is exempt.
- **`ReasonTimeout` is never used for a dpkg skip.**
- **Shutdown and reboot budgets are independent.** Each has its own constants with no cross-derivation, enforced by
  AST (AC-018). The queued TTL is deliberately unchanged.
- **No privilege is added.** No sudoers, unit or `install.sh` change, and the argv is byte-identical.
- **The ADD discloses R-6 itself.** It also recognizes that the old REBOOT tests pinning "shutdown has no gate" must be
  retired, rather than leaving contradictory assertions.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.** There are no CRITICAL or HIGH findings. The generalized gate and the action-bound permit
can only delay or prevent a power action. They cannot cause one, cross actions, or override `allow_shutdown` in the
code as designed. The REBOOT-DPKG-001 conditions are preserved.
- **SEC-001** is a new test-safety hazard created by making the shutdown path testable. It is fixed by an in-code
  backstop with no architectural change.
- **SEC-002** is pre-existing and needs Ryan's decision on scope. Its minimum conditions are documentation and a
  follow-up.
- **SEC-003 and SEC-004** harden regression enforcement.
- **SEC-005** corrects the operator-facing description of the delayed, uncancellable shutdown.

### Conditions (senior_dev implements; QA verifies)
1. **C-1 (SEC-001).** In a test binary, the real-process branch of `execShutdown` and `execReboot` cannot start a
   process, whatever the harness or config. This is enforced in package code and verified without invoking the real
   branch. The production argv is unchanged.
2. **C-2 (SEC-002).**
   - The README states that the installer grants no poweroff rule, and that on such hosts `shutdown initiated` means
     launched, not powering off.
   - The R-6 follow-up story is filed and its ID is recorded in the implementation report.
   - Nothing adds privilege or changes the argv.
   - **If Ryan chooses Option A:** the shutdown result reflects sudo's exit status within a stated bound, a non-zero
     exit or a timeout gives `shutdown: <err>`, the child is reaped, and reboot is untouched.
3. **C-3 (SEC-003).**
   - A shutdown permit can be minted only in an attempt in which the `allow_shutdown` guard passed, and a reboot
     permit only in one in which the `allow_reboot` guard passed. This is enforced structurally (the guards arguments
     and `commonPowerGuards` callers pinned by AST) or at runtime in the exec functions.
   - `powerPermitLive` and `powerPermitSeq` are referenced only in `mintPowerPermit` and `consumePowerPermit`.
   - No `powerPermit` field is assigned outside `mintPowerPermit`.
   - The self-test flags each violation.
4. **C-4 (SEC-004).** The poweroff-path test covers `internal/agent/...` recursively and `cmd/riot-agent`. It flags:
   - any power-tool process launch, or `systemctl` power verb or target, outside `execReboot` and `execShutdown`;
   - case-insensitive `poweroff`, `power-off` or `halt` literals, including constant-folded ones;
   - `syscall.Reboot` and `unix.Reboot`.

   The self-test includes the Windows `-s` form and a subpackage file.
5. **C-5 (SEC-005, INFO-3).**
   - R-7 and the README "Known limits" give the worst case as about 25 minutes when the device was disconnected at
     send time.
   - The README documents "restart the agent on the host" as the way to cancel a waiting shutdown, and does not
     present editing `allow_shutdown` as a cancel path.
   - ADD §10's "sends no event" bullet is corrected to say that the failed-command warning event carries the skip
     text, bounded by REBOOT C-5.
6. **C-6 (QA gate).** These pass:
   - the ADD §13 QA-gate tests;
   - the C-3 runtime tests, including both cross-action refusal tests;
   - `TestShutdown_SD_AC009_*` (no probe when disabled);
   - the C-1, C-3 and C-4 additions above;
   - the unchanged C-8 test.

   QA also confirms that the DoD greps are clean and that `git diff --stat` shows nothing under `scripts/`, `web/`
   (DeviceDetail and components), `internal/agent/dpkglock*.go`, the migrations or `go.mod`.

### For Ryan's decision
- **SEC-002 / R-6 (recommend Option A).** Should this story make the shutdown result truthful (sudo exit status
  observed within a short bound, no privilege, no argv change), or should it ship with today's "launched = initiated"
  semantics and leave everything to the follow-up? With Option A, every installer-managed Linux host will start
  reporting shutdown as **failed**, which is the truth: shutdown has never worked there.
- **SEC-005 / R-7.** Accept that a shutdown can fire up to about 25 minutes after the click with no dashboard cancel,
  leaving the host off. The cancel or progress step stays a follow-up.
- **R-4.** Accept that any local user can veto rIOt shutdowns, as already accepted for reboots (REBOOT SEC-002). Each
  veto is visible.
