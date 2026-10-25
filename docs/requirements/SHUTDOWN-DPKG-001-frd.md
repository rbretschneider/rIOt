# Formal Requirements Document

- **Story ID:** SHUTDOWN-DPKG-001
- **Title:** The shutdown command waits, with a bound, for the dpkg lock and never powers off during a package transaction
- **Author:** Business Developer Agent
- **Date:** 2026-10-25
- **Revision:** 2
- **Status:** DRAFT (product decisions are defaults set on Ryan's behalf; Ryan reviews in the PR. Section 11 lists each one, and the FRD is written to them)
- **Origin:** Follow-up named in REBOOT-DPKG-001 (`docs/requirements/REBOOT-DPKG-001-frd.md`, Section 9), merged in PR #34

## Changelog

- **Revision 2 (2026-10-25):** Changes for the security review (`docs/security/SHUTDOWN-DPKG-001-security-review.md`).
  - SEC-002, Option A: the shutdown result now reflects the poweroff command's exit status, observed within a stated bound. New FR-027 to FR-032 and AC-027 to AC-032. FR-013 gains outcome (g). FR-015 and FR-016 now point to the new FRs. On installer-managed Linux hosts, shutdown will now report failed (Section 2).
  - SEC-005: the worst-case delay of about 25 minutes, with no dashboard cancel, is accepted (NFR-007). New docs requirement FR-033 and AC-033.
  - R-4: the local-user veto is accepted (NFR-008).
  - INFO-3: AC-023 and NFR-003 now count the existing failed-command warning event.
  - New OQ-010 to OQ-013. Existing FR, NFR and AC numbers are unchanged.
- **Revision 1 (2026-10-25):** First draft.

---

## 1. Executive Summary

REBOOT-DPKG-001 made every agent reboot wait, for at most 10 minutes, until the dpkg lock is free. The `shutdown` command was left out. It still powers the host off at once, even while `apt` or `dpkg` is mid-transaction. A host powered off at that point comes back with interrupted dpkg state, the same damage a reboot causes. This story gives the `shutdown` command the same bounded wait. If the lock is still held at the end of the wait, or its state cannot be determined, the shutdown is skipped and reported, never forced. The story also makes the shutdown result truthful: it reports success only when the operating system did not refuse the poweroff command. The story reuses the REBOOT-DPKG-001 detection and wait. It adds its own shutdown time budgets, and no privileges, no persistent state and no UI.

## 2. Background & Context

- **Hazard:** Same as REBOOT-DPKG-001 Section 2. The agent powers off through a root `systemctl poweroff` with no TTY, which bypasses the logind inhibitor check. A poweroff during a dpkg transaction leaves interrupted dpkg state that the `updates` collector cannot repair on its own.
- **Shutdown path in scope (exactly one):** the operator's `shutdown` command. It is gated by `commands.allow_shutdown` and runs synchronously within the command. Today its agent time budget is 1 minute and its server execution deadline is 10 minutes. No post-patch or automated shutdown path exists.
- **Existing checks that stay as they are:** before the command starts, the agent already applies the host maintenance-blocker precheck and the host command reservation (shutdown is a host-level command). A shutdown is not re-run after an agent restart.
- **Untruthful result today (security review SEC-002):** the agent launches the poweroff command and reports "shutdown initiated" without checking whether it succeeded. `install.sh` has never granted a `systemctl poweroff` sudo rule. On every Linux host whose sudo rules come only from the installer, sudo refuses the poweroff, the host stays up, and the result still says "shutdown initiated". After this story those hosts report the shutdown as **failed**. That is the truthful outcome: shutdown has never worked there. Granting the missing sudo rule is follow-up story SHUTDOWN-SUDO-001 and is out of scope.
- **Current docs:** the README says "Shutdown is not gated. A later story may add it." This story is that later story.

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Operator (Ryan / dashboard user) | Issues the `shutdown` command and reads its result. | Existing authenticated dashboard API. Nothing changes. |
| rIOt Server | Dispatches the command and records its result or timeout. | Unchanged, apart from the shutdown budgets in FR-018. |
| rIOt Agent | Waits for the dpkg lock, re-checks guards, powers off or skips, and reports the outcome. | Runs unprivileged as user `riot`, with its existing sudo rules only (NFR-002). |
| Third-party package process | `unattended-upgrades`, `apt`, `dpkg` or any other tool that holds the dpkg lock. | Outside rIOt's control. rIOt must never block or delay it. |
| Local user on a managed host | Any account that can hold the dpkg lock. | Can delay or veto rIOt shutdowns (NFR-008). Gains nothing else. |

## 4. Functional Requirements

### A. Bounded wait before poweroff

- **FR-001:** Before powering off a host for a `shutdown` command, the agent must determine whether the dpkg lock is held. "Held", the detection rules and the "unknown" cases are exactly those of REBOOT-DPKG-001 (its FR-002, FR-007, FR-010 and NFR-003).
- **FR-002:** If `commands.allow_shutdown` is false when the command starts, the agent must refuse the command as it does today. It must not check the lock and must not wait.
- **FR-003:** While the dpkg lock is held, the agent must not power off. It must keep waiting for no more than the shutdown wait bound (10 minutes), counted from the first check that found the lock held.
- **FR-004:** When the lock becomes free during the wait, the agent must move on to the FR-010 guards within 10 seconds of the release.
- **FR-005:** When the first check finds the lock free, the agent must move on to the FR-010 guards without any added delay.
- **FR-006:** When neither dpkg lock file exists (for example, a dnf or other non-Debian host), the agent must not wait and must power off as it does today. On Windows, shutdown behavior must not change, apart from the truthful result in FR-027 to FR-030.

### B. Timeout: skip, never force

- **FR-007:** If the dpkg lock is still held when the shutdown wait bound is reached, the agent must skip the shutdown. It must not power off for that command.
- **FR-008:** After a skip, the agent must not retry the shutdown for that command on its own. Each `shutdown` command has at most one wait.

### C. Fail closed when the lock state is unknown

- **FR-009:** If the agent cannot determine whether the dpkg lock is held, it must skip the shutdown immediately, without waiting out the bound, and report the cause. This covers every "unknown" case defined by REBOOT-DPKG-001, including a held lock whose lock files then disappear during the wait.

### D. Guards after the wait

- **FR-010:** After the wait ends with the lock free, the agent must re-check, immediately before powering off:
  - the agent is not shutting down;
  - `commands.allow_shutdown` is true;
  - no rIOt maintenance run blocks the host. This check is strict: no run is exempt.

  If any guard fails, the agent must skip the shutdown and report which guard failed.
- **FR-011:** The last check before poweroff must be a fresh dpkg lock check. If it finds the lock held, the agent must keep waiting within the same overall FR-003 bound. The total wait for one command must never exceed the bound. After any further wait, FR-010 applies again.

### E. Agent stop

- **FR-012:** If the agent starts shutting down during the wait, the wait must end within 5 seconds and the host must not be powered off. The wait must not delay the agent's stop by more than 5 seconds.

### F. Reporting

- **FR-013:** The `shutdown` command result must state exactly one of these outcomes. The wording mirrors the REBOOT-DPKG-001 standalone reboot results, with "shutdown" in place of "reboot":
  - (a) **shutdown initiated** (no wait);
  - (b) **shutdown initiated after waiting** for the dpkg lock, with the wait duration;
  - (c) **skipped, dpkg lock held past the timeout**, with the bound;
  - (d) **skipped, dpkg lock state unknown**, with the cause;
  - (e) **skipped, agent shutting down**;
  - (f) **skipped, guard failed**, naming the guard;
  - (g) **shutdown failed**, in the form `shutdown: <err>` (FR-028).

  Outcomes (a) and (b) are reported only as FR-029 and FR-030 allow. Outcomes (c) to (g) must be reported as a failed (non-success) result.
- **FR-014:** Every report of a lock held past the timeout, whether a result or a log line, must name the holder when it is visible: the process name and PID, for example "held by unattended-upgr (pid 1234)". When the holder is not visible, the report must say "holder not visible".
- **FR-015:** The result must never say a shutdown was initiated unless the poweroff command was launched and met FR-029 or FR-030. Once the agent has reported a not-started outcome, or its own time budget has expired, it must never power off for that command.
- **FR-016:** If the poweroff command cannot be launched, or is refused, after all checks pass, the agent must report a failed result as specified in FR-028.

### G. Time budgets

- **FR-017:** The shutdown wait bound must be its own setting, separate from the reboot wait bound. Its value is 10 minutes. It is not configurable per agent.
- **FR-018:** The shutdown command must have its own agent time budget and its own server execution deadline, separate from reboot's, sized so that a full wait, the guards, the poweroff launch and the FR-027 start-check bound fit inside both:
  - agent hard maximum: 12 minutes (was 1 minute);
  - server execution deadline: 20 minutes (was 10 minutes);
  - queued time-to-live: unchanged at 15 minutes.

  The existing rule that the server execution deadline exceeds the agent hard maximum by more than 5 minutes must hold for shutdown.
- **FR-019:** Changing any shutdown budget (wait bound, agent hard maximum, server execution deadline) must not change any reboot budget, and changing any reboot budget must not change any shutdown budget.
- **FR-020:** The FR-013 outcome must be the outcome recorded for the command, including after a full 10-minute wait. While the wait is within its bound, neither the agent nor the server may record the command as timed out.

### H. Unchanged behavior

- **FR-021:** Reboot behavior must not change: the three REBOOT-DPKG-001 reboot paths, their outcomes, their budgets (wait bound 10 minutes, agent hard maximum 12 minutes, server execution deadline 20 minutes) and the way the reboot command is launched.
- **FR-022:** The `shutdown` command must remain the only agent path that powers the host off, and it must keep its `commands.allow_shutdown` check. This story adds no automated or post-patch shutdown.
- **FR-023:** While a shutdown waits, it must keep holding the host command reservation, as a running shutdown does today. Another `shutdown` or a `reboot` for the same host must be refused as "host busy", as today. At most one shutdown wait can run per agent at a time.
- **FR-024:** If the agent restarts during the wait, it must not resume or re-run the shutdown after the restart (unchanged: shutdown is excluded from restart reconciliation).
- **FR-025:** The dashboard must not change. The existing power menu and confirm dialog stay as they are.

### I. Documentation

- **FR-026:** The user documentation must:
  - replace "Shutdown is not gated" with a description of the shutdown wait, its bound and its outcomes;
  - show the new shutdown budgets in the power budget table;
  - mention shutdown alongside reboot where the in-app help explains the dpkg wait;
  - add a changelog entry that states the upgrade order (BR-005).

### J. Truthful poweroff result (security review SEC-002)

- **FR-027:** After launching the poweroff command, the agent must observe the command's exit status for no longer than a fixed start-check bound. The architect sets the value and states it in the ADD. It must be greater than zero, and the full shutdown wait plus the start-check bound must fit inside the FR-018 agent hard maximum.
- **FR-028:** If the poweroff command cannot be launched, or exits with a non-zero status within the start-check bound, the result must be failed, in the form `shutdown: <err>`. The `<err>` text must contain only printable characters and must not exceed 200 bytes. The result must not say a shutdown was initiated.
- **FR-029:** If the poweroff command exits with status 0 within the start-check bound, the result must be a success with outcome (a), or outcome (b) if the agent waited for the dpkg lock.
- **FR-030:** If the poweroff command is still running when the start-check bound is reached, the agent must treat the shutdown as initiated and report outcome (a) or (b). The agent must still collect the command's exit when it ends, so that no finished poweroff command remains as an unreaped child of the agent.
- **FR-031:** The truthful result must add no privilege and must not change the poweroff command or its arguments. The way reboots are launched must not change.
- **FR-032:** For the same exit status (launch error, non-zero, zero, or still running at the bound), the test seam and the production path must produce the same outcome.

### K. Documentation for the security review

- **FR-033:** The README shutdown section must state:
  - the installer grants no poweroff sudo rule, so on installer-managed Linux hosts the shutdown result is failed until SHUTDOWN-SUDO-001 ships;
  - "shutdown initiated" means the operating system accepted the poweroff command, or the command was still running at the start-check bound, not that the host has finished powering off;
  - a shutdown can power the host off up to about 10 minutes after the click, or up to about 25 minutes if the device was not connected when the command was sent;
  - the dashboard cannot cancel a waiting shutdown, and restarting the agent on the host (`systemctl restart riot-agent`) cancels it;
  - any local user who holds the dpkg lock can delay or block a rIOt shutdown, and each block shows in the result.

  The README must not present editing `commands.allow_shutdown` as a way to stop a waiting shutdown. The agent reads its config only at start-up.

## 5. Non-Functional Requirements

- **NFR-001 [Constraint, reuse]:** The shutdown wait must use the same lock detection and the same wait-and-guard behavior that REBOOT-DPKG-001 built for reboots. A second lock check, a second wait, or a second clearance mechanism is not allowed. Generalizing the existing one is allowed, provided FR-021 holds.
- **NFR-002 [Security, privilege]:**
  - The lock check must work as the unprivileged agent user.
  - No new sudoers rule, no new capability and no service unit change are allowed. The poweroff keeps using its existing sudo invocation.
  - Re-running `install.sh` must not be required.
- **NFR-003 [Bounded growth]:**
  - No new persistent state: no files, database tables, columns or migrations.
  - Per `shutdown` command:
    - at most one wait;
    - at most one lock check per poll interval (about 2 seconds) plus the final check, so about 300 checks over a full wait;
    - at most two log lines at info level or above from the wait (wait started, wait ended);
    - at most one poweroff process launched, observed for no longer than the start-check bound and always reaped (FR-030);
    - exactly one command result, and for a failed result, only the existing failed-command warning event;
    - no new event type and no telemetry refresh.
  - Each lock check reads no more than 1 MiB and finishes within 1 second (REBOOT-DPKG-001 NFR-003).
  - The `<err>` text in a failed result is at most 200 bytes (FR-028).
  - Individual lock checks must not be logged above debug level.
  - At most one shutdown wait per agent at a time (FR-023).
  - Every failure path (timeout, unknown, guard failed, agent stop, launch error, non-zero exit) ends the command with one result and leaves nothing behind: no file, no record, no unreaped process. Nothing grows with the length of the wait.
- **NFR-004 [Accepted residual]:** The same gap as REBOOT-DPKG-001 NFR-004, between the final lock check and the start of the poweroff, is accepted.
- **NFR-005 [Compatibility, version skew]:**
  - New server with an old agent: shutdown behaves as before. The longer server deadline is harmless.
  - New agent with an old server: the old 10-minute server deadline can expire while the agent is still waiting, and the server would record a timeout that FR-020 forbids. This is why BR-005 requires the server to be upgraded first.
  - The command result keeps its existing shape. Only the message text and, for refused poweroffs, the success flag change.
- **NFR-006 [Testability]:** Every FR must be verifiable by a unit test, without a real dpkg lock, a real poweroff or real elapsed time. The shutdown path must become testable without powering off the test machine.
- **NFR-007 [Accepted residual, availability] (SEC-005):** A shutdown can power the host off up to about 25 minutes after the click: up to 15 minutes queued while the device is disconnected, plus up to about 10 minutes of wait. The dashboard cannot cancel a waiting shutdown. The only cancel path is restarting the agent on the host, which ends the wait as outcome (e) and never re-runs the shutdown (FR-012, FR-024). The host stays off until someone powers it on. A dashboard cancel and progress display are a follow-up.
- **NFR-008 [Accepted residual, security] (R-4):** Any local user who can hold the dpkg lock can delay or veto rIOt shutdowns, as REBOOT-DPKG-001 accepted for reboots. Each veto is visible in the command result (outcome (c) or (d)).

## 6. Business Rules

- **BR-001:** A shutdown must never interrupt a package transaction that rIOt can detect. When in doubt, rIOt does not power off (fail closed).
- **BR-002:** A skipped shutdown is never forced and never retried automatically. The operator re-issues the command.
- **BR-003:** The dpkg wait can only prevent or delay a shutdown. It never causes a shutdown, and it never overrides `commands.allow_shutdown`.
- **BR-004:** Shutdown and reboot have independent budgets. At launch they have the same values (10-minute wait, 12-minute agent maximum, 20-minute server deadline), but either can change later without the other.
- **BR-005:** The server must be upgraded before the agents. The documentation must say so.
- **BR-006:** A shutdown result reports success only when the operating system did not refuse the poweroff command within the start-check bound. An operator who uses shutdown as a containment action must never see success for a host that refused to power off.

## 7. Data Requirements

- **Entities:** none new. No schema, telemetry field, event type or command parameter is required. The command result and the log lines carry the new outcome text in their existing structures.
- **Validation:** the `<err>` text in outcome (g) contains printable characters only and is at most 200 bytes (FR-028).
- **Shutdown attempt states (in memory only, never persisted):**

| From | Event | To | Outcome |
|------|-------|----|---------|
| Requested | `allow_shutdown` false | Refused | As today, no lock check (FR-002) |
| Requested | No dpkg lock file exists, or Windows | Launching | No wait (FR-006) |
| Requested | Lock free | Guards | No delay (FR-005) |
| Requested | Lock held | Waiting | Wait starts, one log line |
| Requested / Waiting | Lock state unknown | Skipped | "lock state unknown: <cause>" (FR-009) |
| Waiting | Lock released | Guards | Within 10 s (FR-004) |
| Waiting | Shutdown wait bound reached | Skipped | "held past the timeout", names holder (FR-007, FR-014) |
| Waiting | Agent shutting down | Skipped | Ends within 5 s (FR-012) |
| Guards | A guard fails | Skipped | Names the guard (FR-010) |
| Guards | All pass, final lock check free | Launching | Poweroff command launched |
| Guards | Final lock check held, bound not reached | Waiting | Same overall bound (FR-011) |
| Launching | Launch error | Failed | `shutdown: <err>` (FR-028) |
| Launching | Non-zero exit within start-check bound | Failed | `shutdown: <err>` (FR-028) |
| Launching | Exit 0 within start-check bound | Initiated | Outcome (a) or (b) (FR-029) |
| Launching | Still running at start-check bound | Initiated | Outcome (a) or (b); child reaped when it exits (FR-030) |
| Skipped / Failed | (terminal) | — | Failed result (FR-013) |

## 8. Acceptance Criteria

### AC-001: Lock free, shutdown proceeds without delay [Maps to FR-001, FR-005, FR-013]
- **Given** a host where the dpkg lock files exist and neither is held, all guards pass, and the poweroff command exits 0
- **When** the agent runs a `shutdown` command
- **Then** the poweroff starts with no added wait
- **And** the result is a success that says "shutdown initiated"

### AC-002: Held lock delays the shutdown [Maps to FR-001, FR-003]
- **Given** a host where the dpkg lock is held
- **When** the agent runs a `shutdown` command
- **Then** no poweroff starts while the lock stays held within the bound

### AC-003: Release during the wait lets the shutdown proceed [Maps to FR-004, FR-013]
- **Given** a shutdown waiting on a held lock, with all guards passing and a poweroff command that exits 0
- **When** the lock is released after 3 minutes
- **Then** the poweroff starts within 10 seconds of the release
- **And** the result is a success that says the shutdown was initiated after waiting about 3 minutes for the dpkg lock

### AC-004: Timeout skips the shutdown and never forces it [Maps to FR-007, FR-008, BR-002]
- **Given** a lock that stays held for longer than 10 minutes
- **When** the shutdown wait bound is reached
- **Then** no poweroff starts for that command
- **And** the agent does not start another wait or poweroff for that command on its own

### AC-005: Timeout result is a truthful failure naming the holder [Maps to FR-013, FR-014, FR-015]
- **Given** a lock held past the bound by a visible process `unattended-upgr`, PID 1234
- **When** the bound is reached
- **Then** the result is a failure that says the shutdown was skipped because the dpkg lock was held past the timeout (10m), and names `unattended-upgr` and PID 1234
- **And** the result does not say a shutdown was initiated

### AC-006: Holder not visible [Maps to FR-014]
- **Given** a lock held past the bound whose holder process cannot be identified
- **When** the skip is reported
- **Then** the result says "holder not visible"

### AC-007: Unknown lock state fails closed immediately [Maps to FR-009, FR-013, BR-001]
- **Given** a lock check that returns "unknown", and separately a held lock whose lock files disappear during the wait
- **When** the agent runs a `shutdown` command
- **Then** no poweroff starts and no further wait takes place
- **And** the result is a failure that says the dpkg lock state is unknown and gives the cause

### AC-008: No lock file means no wait [Maps to FR-006]
- **Given** a Linux host where neither dpkg lock file exists, and separately a Windows host, each with a poweroff command that exits 0
- **When** the agent runs a `shutdown` command
- **Then** the host powers off as it does today, with no wait and no lock-related text in the result

### AC-009: Shutdown disabled is refused without a lock check [Maps to FR-002, BR-003]
- **Given** `commands.allow_shutdown` is false
- **When** the agent receives a `shutdown` command
- **Then** the command is refused as it is today
- **And** no lock check and no wait take place

### AC-010: `allow_shutdown` turned off during the wait [Maps to FR-010, FR-013, BR-003]
- **Given** a shutdown waiting on a held lock (this state can be set up in a unit test; operators cannot change the config of a running agent)
- **When** `commands.allow_shutdown` becomes false, and then the lock is released
- **Then** no poweroff starts
- **And** the result is a failure that says a guard failed and names `allow_shutdown`

### AC-011: Maintenance run started during the wait [Maps to FR-010]
- **Given** a shutdown waiting on a held lock
- **When** a rIOt maintenance run starts on the host, and then the lock is released
- **Then** no poweroff starts
- **And** the result is a failure that says a guard failed and names the maintenance blocker

### AC-012: Final check finds the lock re-taken [Maps to FR-011]
- **Given** a wait that ended with the lock free at minute 4, and a lock that is taken again before the final check
- **When** the final check runs
- **Then** the agent keeps waiting, and the total wait for the command does not exceed 10 minutes

### AC-013: Agent stop cancels the wait [Maps to FR-012, FR-013]
- **Given** a shutdown waiting on a held lock
- **When** the agent is told to stop
- **Then** the wait ends within 5 seconds and no poweroff starts
- **And** the outcome is "skipped, agent shutting down"

### AC-014: No poweroff after a not-started outcome or an expired agent budget [Maps to FR-015]
- **Given** a `shutdown` command whose result has been reported as skipped, or whose agent time budget has expired
- **When** the lock is released afterwards
- **Then** the agent does not power off for that command

### AC-015: Poweroff launch failure is reported [Maps to FR-016, FR-028]
- **Given** all checks pass, and the poweroff command cannot be launched
- **When** the agent tries to power off
- **Then** the result is a failure in the form `shutdown: <err>`

### AC-016: Full wait is not recorded as timed out [Maps to FR-018, FR-020]
- **Given** a `shutdown` command, a lock held for 9 minutes 50 seconds and then released, all guards passing, and a poweroff command that exits 0
- **When** the poweroff starts
- **Then** the recorded outcome is "shutdown initiated after waiting" about 9m50s
- **And** neither the agent's nor the server's time budget recorded the command as timed out

### AC-017: Shutdown budgets have the required values [Maps to FR-017, FR-018]
- **Given** the command budget table after this story
- **When** the shutdown entry is read
- **Then** the wait bound is 10 minutes, the agent hard maximum is 12 minutes, the server execution deadline is 20 minutes and the queued time-to-live is 15 minutes
- **And** the server execution deadline exceeds the agent hard maximum by more than 5 minutes

### AC-018: Shutdown and reboot budgets are independent [Maps to FR-019, BR-004]
- **Given** the shutdown wait bound and the reboot wait bound set to different values
- **When** a shutdown and, separately, a reboot each wait on a lock held past every bound
- **Then** each skips at its own bound
- **And** the reboot budget entries do not reference or derive from the shutdown entries, and the shutdown entries do not reference or derive from the reboot entries

### AC-019: Reboot is unchanged [Maps to FR-021]
- **Given** the agent and server after this story
- **When** the REBOOT-DPKG-001 acceptance tests and the reboot budget table are run
- **Then** they pass unchanged, and the reboot budgets are still 10 minutes (wait), 12 minutes (agent) and 20 minutes (server)

### AC-020: Shutdown is the only poweroff path [Maps to FR-022, NFR-001]
- **Given** the agent code after this story
- **When** the poweroff call sites are inspected
- **Then** only the `shutdown` command can power the host off, after its `commands.allow_shutdown` check and after the shared lock wait has cleared it
- **And** the reboot call-site guard test keeps passing

### AC-021: Host busy during the wait [Maps to FR-023]
- **Given** a shutdown waiting on a held lock
- **When** a second `shutdown` or a `reboot` for the same host arrives
- **Then** it is refused as "host busy", as today
- **And** no second wait starts

### AC-022: Agent restart does not re-run the shutdown [Maps to FR-024]
- **Given** a shutdown waiting on a held lock
- **When** the agent restarts
- **Then** after the restart the agent does not resume or re-run that shutdown

### AC-023: Bounded growth during a wait [Maps to NFR-003]
- **Given** a full 10-minute wait that ends in a skip
- **When** the command ends
- **Then** no file or database record was created
- **And** at most two info-or-above log lines from the wait and exactly one command result were produced
- **And** the only event was the existing failed-command warning event, and no telemetry refresh was triggered
- **And** no more than one lock check ran per poll interval, plus the final check

### AC-024: No new privileges [Maps to NFR-002, FR-031]
- **Given** a device whose `install.sh` was not re-run since before this story
- **When** the updated agent checks the dpkg lock and launches the poweroff
- **Then** both use the existing sudoers rules and service unit
- **And** the poweroff command and its arguments are byte-identical to the previous release

### AC-025: No dashboard change [Maps to FR-025]
- **Given** the web app after this story
- **When** the power menu and shutdown confirm dialog are rendered
- **Then** they are unchanged

### AC-026: Documentation updated [Maps to FR-026, BR-005]
- **Given** the README, in-app help and changelog after this story
- **When** they are read
- **Then** none says shutdown is not gated
- **And** the power budget table shows the shutdown budgets from FR-018
- **And** the changelog says to upgrade the server before the agents

### AC-027: Exit 0 reports initiated [Maps to FR-027, FR-029, BR-006]
- **Given** all checks pass, and the poweroff command exits with status 0 within the start-check bound
- **When** the agent launches the poweroff, once with no wait and once after a 3-minute wait
- **Then** the results are successes that say "shutdown initiated" and "shutdown initiated after waiting" about 3 minutes, respectively

### AC-028: Refused poweroff reports failed [Maps to FR-015, FR-028, BR-006]
- **Given** all checks pass, and the poweroff command exits with a non-zero status within the start-check bound, as sudo does on an installer-managed host with no poweroff rule
- **When** the agent launches the poweroff
- **Then** the result is a failure in the form `shutdown: <err>`
- **And** the result does not say a shutdown was initiated

### AC-029: Error text is sanitized and bounded [Maps to FR-028, NFR-003]
- **Given** a poweroff command that fails with an error text of more than 200 bytes containing control characters
- **When** the failure is reported
- **Then** the `<err>` text contains only printable characters and is at most 200 bytes

### AC-030: Still running at the bound counts as initiated, and the child is reaped [Maps to FR-030, NFR-003]
- **Given** all checks pass, and the poweroff command is still running when the start-check bound is reached
- **When** the bound is reached
- **Then** the result is a success that says "shutdown initiated" (or the after-waiting form)
- **And** the result is reported no later than the start-check bound after launch
- **And** when the command later exits, its exit is collected and no unreaped child of the agent remains

### AC-031: Start-check bound fits the budget [Maps to FR-027, FR-018]
- **Given** the stated start-check bound and the shutdown budgets
- **When** they are compared
- **Then** the start-check bound is greater than zero
- **And** the shutdown wait bound plus the start-check bound is less than the 12-minute agent hard maximum

### AC-032: Test seam and production path agree; reboot launch unchanged [Maps to FR-031, FR-032, FR-021]
- **Given** each exit case (launch error, non-zero exit, exit 0, still running at the bound)
- **When** the outcome is produced through the test seam and through the production result mapping
- **Then** both produce the same outcome and success flag
- **And** the way reboots are launched and reported is unchanged from the previous release

### AC-033: Security-review documentation [Maps to FR-033, NFR-007, NFR-008]
- **Given** the README after this story
- **When** the shutdown section is read
- **Then** it states that the installer grants no poweroff rule and that such hosts report shutdown as failed until SHUTDOWN-SUDO-001
- **And** it states what "shutdown initiated" means, the worst-case delay of about 10 minutes (about 25 minutes if the device was disconnected when the command was sent), and that restarting the agent on the host cancels a waiting shutdown
- **And** it states that a local user who holds the dpkg lock can block a shutdown, visibly
- **And** it does not present editing `commands.allow_shutdown` as a way to stop a waiting shutdown

## 9. Out of Scope

- Any change to reboot behavior, budgets or launch (FR-021).
- A poweroff sudo rule or any other privilege change (follow-up SHUTDOWN-SUDO-001).
- A dashboard cancel or progress display for a waiting shutdown (follow-up; NFR-007).
- dnf, rpm, pacman, apk or other package-manager locks.
- Any UI change, including deadline or "waiting" text in the confirm dialog.
- Automatic retry of a skipped or failed shutdown.
- Closing the gap between the final check and the start of the poweroff (NFR-004).
- Confirming that the host actually finished powering off. "Initiated" means the operating system did not refuse the command (FR-029, FR-030).
- Any automated or post-patch shutdown path.
- Enforcing the upgrade order in software. It is documented only (BR-005).
- Repairing an already-interrupted dpkg.

## 10. Assumptions

- **A-001:** The product decisions in Section 11 were set by Clod on Ryan's behalf on 2026-10-25 as defaults. Ryan reviews them in the PR.
- **A-002:** The REBOOT-DPKG-001 lock detection works unchanged for shutdown. It was verified on Hawaii (Debian 13) for that story.
- **A-003:** rIOt's own maintenance runs are already kept apart from shutdown by the existing maintenance-blocker precheck and host reservation. This story targets third-party package activity.
- **A-004:** A host powered off mid-transaction suffers the same interrupted-dpkg damage as one rebooted mid-transaction.
- **A-005:** `systemctl poweroff` queues the poweroff and returns, and a sudo refusal returns within milliseconds (security review SEC-002). A short start-check bound therefore separates refusal from acceptance in practice.

## 11. Open Questions

Each question is decided by its default, and the FRD is written to it. Status of every item: **decided (default), Ryan reviews in PR.**

- **OQ-001: Wait behavior.** Default: same as reboot. Wait up to 10 minutes from the first "held" check, poll about every 2 seconds, skip on timeout, never retry, never force (FR-003, FR-007, FR-008). Decided (default), Ryan reviews in PR.
- **OQ-002: Unknown lock state.** Default: skip at once, no wait (FR-009). Decided (default), Ryan reviews in PR.
- **OQ-003: Own wait bound.** Default: shutdown has its own wait bound, value 10 minutes, not configurable per agent, so it can diverge from reboot's later (FR-017). Decided (default), Ryan reviews in PR.
- **OQ-004: Shutdown budgets.** Default: agent hard maximum 12 minutes (was 1), server execution deadline 20 minutes (was 10), queued time-to-live unchanged at 15 minutes. The architect may change these values with a stated reason (FR-018). Decided (default), Ryan reviews in PR.
- **OQ-005: Guards after the wait.** Default: agent not shutting down, `commands.allow_shutdown`, and the host maintenance blocker in strict form, with no run exempt (FR-010). Decided (default), Ryan reviews in PR.
- **OQ-006: Result messages.** Default: mirror the reboot results with "shutdown" in place of "reboot" (FR-013). Decided (default), Ryan reviews in PR.
- **OQ-007: Windows and non-dpkg hosts.** Default: unchanged, no wait (FR-006). Decided (default), Ryan reviews in PR.
- **OQ-008: UI.** Default: no change. The confirm dialog gets no waiting or deadline text (FR-025). Decided (default), Ryan reviews in PR.
- **OQ-009: Upgrade order.** Default: server first, then agents, documented in the changelog and README, not enforced in software (BR-005, NFR-005). Decided (default), Ryan reviews in PR.
- **OQ-010: SEC-002, truthful poweroff result.** Default: Option A. The agent observes the poweroff command's exit status within a short start-check bound that the architect states. A launch error or non-zero exit gives `shutdown: <err>` (sanitized, at most 200 bytes). Exit 0 gives "shutdown initiated". Still running at the bound counts as initiated, and the child is reaped. No new privilege, no argument change, and reboot launch is untouched (FR-027 to FR-032). On installer-managed Linux hosts shutdown will now report failed. The missing sudo rule is follow-up SHUTDOWN-SUDO-001. Decided (default), Ryan reviews in PR.
- **OQ-011: SEC-005, delayed and uncancellable shutdown.** Default: accepted. Worst case about 25 minutes from click to poweroff (15-minute queued time-to-live plus 10-minute wait). There is no dashboard cancel. Cancelling means restarting the agent on the host. Dashboard cancel and progress stay a follow-up (NFR-007, FR-033). Decided (default), Ryan reviews in PR.
- **OQ-012: R-4, local-user veto.** Default: accepted. Any local user can block rIOt shutdowns through the dpkg lock, as for reboot. Each block is visible in the result (NFR-008). Decided (default), Ryan reviews in PR.
- **OQ-013: Platform scope of the truthful result.** Default: FR-027 to FR-030 apply to the shutdown command on every platform, Windows included, so that there is one rule for the result. A successful Windows `shutdown /s /t 5` returns at once with status 0, so its result does not change. Only a refusal, which is now reported truthfully, changes (FR-006). Decided (default), Ryan reviews in PR.

## 12. Dependencies

- **D-001:** REBOOT-DPKG-001 (PR #34, merged): the lock detection, the wait-and-guard behavior and the reboot call-site guard test. This story reuses them (NFR-001) and must not change reboot behavior (FR-021).
- **D-002:** Command time budgets for the power class and their pinned tests (CMD-LIFECYCLE-001). The shutdown entries change; the reboot entries must not (FR-018, FR-019).
- **D-003:** The host maintenance-blocker check and host command reservation (existing).
- **D-004:** AGENT-PRIV / AGENT-HARDEN-001: the reason NFR-002 forbids new sudoers rules and `install.sh` re-runs.
- **D-005:** Technical writer: README section and budget table, in-app help, changelog (FR-026, FR-033).
- **D-006:** No new third-party dependency, no database migration, no new external service.
- **D-007:** Pipeline: architect (ADD), then security-researcher review, before engineering.
- **D-008:** Security review `docs/security/SHUTDOWN-DPKG-001-security-review.md` (revision 1 inputs): SEC-002 (FR-027 to FR-032), SEC-005 (NFR-007, FR-033), R-4 (NFR-008), INFO-3 (AC-023). Conditions C-1, C-3 and C-4 are engineering conditions and are tracked in the ADD, not as new FRs.
- **D-009:** Follow-up SHUTDOWN-SUDO-001: a poweroff privilege through the AGENT-PRIV capability path. Until it ships, installer-managed Linux hosts report shutdown as failed.
- **D-010:** Follow-up (unnumbered): dashboard cancel and progress for a waiting shutdown (NFR-007).
