# Formal Requirements Document

- **Story ID:** REBOOT-DPKG-001
- **Title:** Agent-initiated reboots wait, with a bound, for the dpkg lock and never interrupt a package transaction
- **Author:** Business Developer Agent
- **Date:** 2026-10-24
- **Status:** DRAFT (product decisions are recommended defaults set on Ryan's behalf; Ryan reviews in the PR. Open questions in Section 11 carry defaults, and the FRD is written to them)
- **Origin:** PATCH-FLOW-001 security review, finding INFO-4 (`docs/security/PATCH-FLOW-001-security-review.md`)

---

## 1. Executive Summary

Every reboot the rIOt agent starts runs as root and does not check whether a package transaction is in progress. If `unattended-upgrades` or an administrator's `apt` is mid-transaction, the reboot cuts it off and the host comes back with interrupted dpkg state. The `updates` collector cannot repair that state on its own. This story makes all three agent reboot paths wait, for at most 10 minutes, until the dpkg lock is free. If the lock is still held at the end of the wait, or its state cannot be determined, the reboot is skipped and reported, never forced. The story adds no privileges, no persistent state and no UI.

## 2. Background & Context

- **Incident class:** Host `vermont` (Debian 13 + OpenMediaVault) ended up with an interrupted dpkg (`openmediavault-writecache` half-configured). After that the `updates` collector reported an error every night until someone ran `dpkg --configure -a` by hand. A reboot during a running dpkg transaction produces exactly this state.
- **INFO-4 finding:** The agent reboots through a root `systemctl reboot` with no TTY, which bypasses the logind inhibitor check. All three reboot paths share the hazard. The review deferred it to a follow-up story, and this is that story.
- **Who can hold the lock:** rIOt's own maintenance runs are already serialized against reboots by the existing maintenance-run guard. The remaining hazard is third-party package activity: `unattended-upgrades`, an administrator's shell, or another tool.
- **Reboot paths in scope (exactly three, each already gated by `commands.allow_reboot`):**
  1. **Standalone reboot:** the operator's `reboot` command. It runs synchronously within the command, under a 1-minute agent time budget.
  2. **Authorized post-patch reboot:** `reboot_if_required`, phase B, from PATCH-FLOW-001. It runs after the command result was reported.
  3. **Unauthorized post-patch reboot:** the PATCH-GATE path, taken when `allow_reboot` is on and reboot-class packages were applied. The server's maintenance-window auto-patch also uses it. It runs after the command result was reported.

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Operator (Ryan / dashboard user) | Issues the standalone `reboot` command, or a manual patch with reboot authorization. Reads results and events. | Existing authenticated dashboard API. Nothing changes. |
| rIOt Server | Dispatches commands, including maintenance-window auto-patch. Records results, events and telemetry. | Unchanged, apart from any budget change the architect needs for FR-016. |
| rIOt Agent | Starts reboots on the three paths, waits for the dpkg lock and reports outcomes. | Runs unprivileged as user `riot`, with its existing sudo rules only (NFR-001). |
| Third-party package process | `unattended-upgrades`, `apt`, `dpkg` or any other tool that holds the dpkg lock. | Outside rIOt's control. rIOt must never block or delay it (FR-007). |

## 4. Functional Requirements

### A. Bounded wait on every reboot path

- **FR-001:** Before starting a reboot on any of the three paths in Section 2, the agent must determine whether the dpkg lock is held.
- **FR-002:** "The dpkg lock is held" means that a process holds a lock on `/var/lib/dpkg/lock-frontend` or on `/var/lib/dpkg/lock`.
- **FR-003:** While the dpkg lock is held, the agent must not start the reboot. It must keep waiting for no more than 10 minutes, counted from the first check that found the lock held.
- **FR-004:** When the lock becomes free during the wait, the agent must move on to the FR-011 guards within 10 seconds of the release.
- **FR-005:** When the first check finds the lock free, the agent must move on to the FR-011 guards without any added delay.
- **FR-006:** When neither dpkg lock file exists (for example, a dnf or other non-Debian host), the agent must not wait and must reboot as it does today. On Windows, reboot behavior must not change.
- **FR-007:** Checking the lock must never acquire or hold the dpkg lock, and must never block or delay a package-manager process.

### B. Timeout: skip, never force

- **FR-008:** If the dpkg lock is still held when the 10-minute bound is reached, the agent must skip the reboot. It must not start the reboot for that attempt.
- **FR-009:** After a skip, the agent must not retry the reboot for that command or that patch run on its own. Each reboot attempt has at most one wait.

### C. Fail closed when the lock state is unknown

- **FR-010:** If the agent cannot determine whether the dpkg lock is held, it must skip the reboot immediately, without waiting out the bound, and report the cause. This covers:
  - a lock file whose presence cannot be checked, for any reason other than "does not exist";
  - lock-holder information that cannot be read, cannot be interpreted, or exceeds the NFR-003 bound.

### D. Guards after the wait

- **FR-011:** After the wait ends with the lock free, the agent must re-check, immediately before rebooting, every guard that the path applies today:
  - the agent is not shutting down;
  - `commands.allow_reboot` is true;
  - no rIOt maintenance run blocks the reboot;
  - any other existing check on that path.

  If any guard fails, the agent must skip the reboot and report which guard failed.
- **FR-012:** The last check before the reboot starts must be a dpkg lock check. If it finds the lock held, the agent must keep waiting within the same overall FR-003 bound, and the total wait for one attempt must never exceed 10 minutes. After any further wait, FR-011 applies again.

### E. Agent shutdown

- **FR-013:** If the agent starts shutting down during the wait, the wait must end within 5 seconds and the reboot must not start. The wait must not delay the agent's stop by more than 5 seconds.

### F. Reporting

- **FR-014:** The standalone reboot's command result must state exactly one of these outcomes:
  - (a) **reboot started**, with whether the agent waited for the dpkg lock and for how long;
  - (b) **skipped, dpkg lock held past the timeout**;
  - (c) **skipped, dpkg lock state unknown**, with the cause;
  - (d) **skipped, agent shutting down**;
  - (e) **skipped, a guard failed**, naming the guard.

  Outcomes (b) to (e) must be reported as a failed (non-success) result.
- **FR-015:** The standalone reboot's result must never say a reboot started unless the agent started it. Once the agent has reported a not-started outcome, or a timeout, for a reboot command, it must never start a reboot for that command.
- **FR-016:** The FR-014 outcome must be the outcome recorded for the standalone reboot command, including after a full 10-minute wait. The command must not be recorded as timed out by the agent's or the server's time budget while the wait is still within its bound. The 1-minute agent hard maximum for power commands conflicts with this. How to resolve it is the architect's decision (see OQ-003).
- **FR-017:** When a post-patch reboot (authorized or unauthorized, including maintenance-window auto-patch) is skipped for any reason in FR-008, FR-010, FR-011 or FR-013, the agent must:
  - emit one warning event for the device that names the command ID and the skip reason;
  - trigger one updates telemetry refresh, so that the host's reboot-required state stays visible.

  FR-017 does not apply to a skip caused by agent shutdown when the agent can no longer send events. That case must be logged.
- **FR-018:** Every report of a lock held past the timeout, whether a result, an event or a log line, must name the holder when it is visible: the process name and PID, for example "held by unattended-upgr (pid 1234)". When the holder is not visible, the report must say "holder not visible".
- **FR-019:** When a post-patch reboot is skipped, the agent must release the host command reservation that it holds for that pending reboot. Other commands for the host must then be accepted as they would be without the pending reboot.

### G. Unchanged behavior

- **FR-020:** The `shutdown` (poweroff) command's behavior and time budget must not change.
- **FR-021:** The agent must have no reboot paths other than the three in Section 2. Each must keep its existing `commands.allow_reboot` check.

## 5. Non-Functional Requirements

- **NFR-001 [Security, privilege]:**
  - The lock check must work as the unprivileged agent user.
  - No new sudoers rule, no new capability and no service unit change are allowed.
  - Re-running `install.sh` must not be required: the story ships with a normal agent update.
- **NFR-002 [Bounded growth]:**
  - No new persistent state: no files, database tables, columns or migrations.
  - Per reboot attempt: at most one wait, at most one warning event, at most one telemetry refresh, and at most two log lines at info level or above (wait started, wait ended).
  - Individual lock checks must not be logged above debug level.
  - Nothing grows with the length of the wait.
- **NFR-003 [Bounded reads]:** Each lock check must read no more than 1 MiB of lock-holder information and must finish within 1 second. If either bound is exceeded, the lock state is unknown (FR-010).
- **NFR-004 [Accepted residual]:** A gap of milliseconds between the final lock check and the start of the system shutdown is accepted. A package process that takes the lock in that gap can still be interrupted.
- **NFR-005 [Compatibility]:** The post-patch paths must need no server change. Results, events and telemetry must keep their existing shapes. Only additive fields or message text are allowed.
- **NFR-006 [Testability]:** Every FR must be verifiable by a unit test, without a real dpkg lock, a real reboot or real elapsed time.

## 6. Business Rules

- **BR-001:** A reboot must never interrupt a package transaction that rIOt can detect. When in doubt, rIOt does not reboot (fail closed).
- **BR-002:** A skipped reboot is never forced and never retried automatically. The host's reboot-required state stays visible, and the operator re-issues the reboot. This keeps the PATCH-FLOW-001 INFO-3 property: no automated reboot loop.
- **BR-003:** The dpkg wait can only prevent or delay a reboot. It never causes a reboot that would not have happened otherwise, and it never overrides `commands.allow_reboot`.
- **BR-004:** All three reboot paths use the same wait bound of 10 minutes (OQ-001).

## 7. Data Requirements

- **Entities:** none new. No schema, telemetry field or command parameter is required. The standalone reboot result, the post-patch warning event and the log lines carry the new outcome text in their existing structures.
- **Validation:** none new.
- **Reboot attempt states (in memory only, never persisted):**

| From | Event | To | Outcome |
|------|-------|----|---------|
| Requested | No dpkg lock file exists | Guards | No wait (FR-006) |
| Requested | Lock free | Guards | No delay (FR-005) |
| Requested | Lock held | Waiting | Wait starts, one log line |
| Requested / Waiting | Lock state unknown | Skipped | "lock state unknown: <cause>" (FR-010) |
| Waiting | Lock released | Guards | Within 10 s (FR-004) |
| Waiting | 10-minute bound reached | Skipped | "dpkg lock held past timeout, held by …" (FR-008, FR-018) |
| Waiting | Agent shutting down | Skipped | Ends within 5 s (FR-013) |
| Guards | A guard fails | Skipped | Names the guard (FR-011) |
| Guards | All pass, final lock check free | Rebooting | Reboot starts |
| Guards | Final lock check held, bound not reached | Waiting | Same overall bound (FR-012) |
| Skipped | (terminal) | — | Standalone: failed result (FR-014). Post-patch: warning event, telemetry refresh, reservation released (FR-017, FR-019) |

## 8. Acceptance Criteria

### AC-001: Lock free, reboot proceeds without delay [Maps to FR-001, FR-005]
- **Given** a host where the dpkg lock files exist and neither is held, and all guards pass
- **When** each of the three reboot paths starts a reboot
- **Then** the reboot starts with no added wait

### AC-002: Held lock delays the reboot on every path [Maps to FR-001, FR-002, FR-003]
- **Given** a host where `/var/lib/dpkg/lock-frontend` is held
- **When** each of the three reboot paths starts a reboot
- **Then** no reboot starts while the lock stays held within the bound

### AC-003: Legacy lock alone also counts as held [Maps to FR-002]
- **Given** a host where only `/var/lib/dpkg/lock` is held
- **When** a reboot path starts a reboot
- **Then** the agent waits exactly as in AC-002

### AC-004: Release during the wait lets the reboot proceed [Maps to FR-004]
- **Given** a reboot waiting on a held lock
- **When** the lock is released after 3 minutes
- **Then** the guards are evaluated and the reboot starts within 10 seconds of the release

### AC-005: Timeout skips the reboot and never forces it [Maps to FR-008, FR-009, BR-002]
- **Given** a lock that stays held for longer than 10 minutes
- **When** the bound is reached
- **Then** no reboot starts for that attempt
- **And** the agent does not start another wait or reboot for that command on its own

### AC-006: Standalone timeout gives a truthful failed result naming the holder [Maps to FR-014, FR-015, FR-018]
- **Given** a standalone `reboot` command, and a lock held past the bound by a visible process `unattended-upgr`, PID 1234
- **When** the bound is reached
- **Then** the command result is a failure that states "skipped, dpkg lock held past the timeout" and names `unattended-upgr` and PID 1234
- **And** the result does not say a reboot started

### AC-007: Holder not visible [Maps to FR-018]
- **Given** a lock held past the bound whose holder process cannot be identified
- **When** the skip is reported
- **Then** the report says "holder not visible"

### AC-008: Post-patch timeout gives a warning event and a telemetry refresh [Maps to FR-017, FR-018]
- **Given** an authorized `reboot_if_required` run, and separately an unauthorized PATCH-GATE post-patch reboot (as used by maintenance-window auto-patch), each with the lock held past the bound
- **When** the bound is reached
- **Then** for each, exactly one warning event names the command ID, the timeout reason and the holder
- **And** exactly one updates telemetry refresh is triggered
- **And** no reboot starts

### AC-009: Post-patch skip releases the host reservation [Maps to FR-019]
- **Given** a post-patch reboot holding the host command reservation, and then skipped
- **When** another command for the same host is submitted afterwards
- **Then** the reservation no longer blocks it

### AC-010: Unknown lock state fails closed immediately [Maps to FR-010, BR-001]
- **Given** a lock file whose presence check fails with an error other than "does not exist", and separately lock-holder information that cannot be read or interpreted
- **When** a reboot path starts a reboot
- **Then** no reboot starts and no wait takes place
- **And** the report states "dpkg lock state unknown" with the cause (as a failed result for the standalone path, and per FR-017 for the post-patch paths)

### AC-011: Oversized lock information fails closed [Maps to NFR-003, FR-010]
- **Given** lock-holder information larger than 1 MiB, or a check that takes longer than 1 second
- **When** the agent checks the lock
- **Then** the lock state is treated as unknown and the reboot is skipped

### AC-012: No lock file means no wait [Maps to FR-006]
- **Given** a host where neither dpkg lock file exists (a dnf host), and separately a Windows host
- **When** a reboot path starts a reboot
- **Then** the reboot proceeds as it does today, with no wait and no lock-related report

### AC-013: Guards are re-checked after the wait [Maps to FR-011]
- **Given** a reboot waiting on a held lock
- **When** during the wait `commands.allow_reboot` becomes false, or a rIOt maintenance run starts, and then the lock is released
- **Then** no reboot starts
- **And** the report names the failed guard

### AC-014: Final check finds the lock re-taken [Maps to FR-012]
- **Given** a wait that ended with the lock free at minute 4, and a lock that is taken again before the final check
- **When** the final check runs
- **Then** the agent keeps waiting, and the total wait for the attempt does not exceed 10 minutes

### AC-015: Agent shutdown cancels the wait [Maps to FR-013]
- **Given** a reboot waiting on a held lock
- **When** the agent is told to stop
- **Then** the wait ends within 5 seconds and no reboot starts

### AC-016: No reboot after a not-started result [Maps to FR-015]
- **Given** a standalone `reboot` command whose result has been reported as skipped or timed out
- **When** the lock is released afterwards
- **Then** the agent does not start a reboot for that command

### AC-017: Waiting standalone reboot reports the wait and is not recorded as timed out [Maps to FR-014, FR-016]
- **Given** a standalone `reboot` command, and a lock held for 9 minutes and then released, with all guards passing
- **When** the reboot starts
- **Then** the outcome recorded for the command is "reboot started", stating that the agent waited about 9 minutes
- **And** neither the agent nor the server recorded the command as timed out

### AC-018: Lock check never takes the lock [Maps to FR-007]
- **Given** an agent checking the lock repeatedly during a wait
- **When** a package process tries to acquire the dpkg lock
- **Then** the agent's check never acquires the lock and never blocks that process

### AC-019: Bounded growth during a wait [Maps to NFR-002]
- **Given** a full 10-minute wait that ends in a skip
- **When** the attempt ends
- **Then** no file or database record was created
- **And** at most two info-or-above log lines, one warning event (post-patch) and one telemetry refresh (post-patch) were produced

### AC-020: Shutdown and reboot call sites are unchanged [Maps to FR-020, FR-021]
- **Given** the agent after this story
- **When** the reboot call sites and the `shutdown` command are inspected and exercised
- **Then** exactly the three Section 2 paths can start a reboot, each checking `commands.allow_reboot`
- **And** `shutdown` behaves as before, with no dpkg wait and its unchanged time budget

### AC-021: No new privileges [Maps to NFR-001]
- **Given** a device whose `install.sh` was not re-run since before this story
- **When** the updated agent checks the dpkg lock
- **Then** the check works with the existing sudoers rules and service unit

## 9. Out of Scope

- `shutdown` / poweroff waiting for the dpkg lock (named follow-up story; it shares the hazard).
- dnf, rpm, pacman, apk or other package-manager locks.
- Any UI change. Outcomes appear in the existing command result, event and log surfaces.
- Automatic retry of a skipped reboot, by the agent or by a later maintenance window (OQ-005).
- Closing the millisecond gap between the final check and the start of shutdown (NFR-004).
- Changing the authorized path's phase A result text (OQ-004).
- Repairing an already-interrupted dpkg (the `updates` collector self-heal is a separate item).

## 10. Assumptions

- **A-001:** The product decisions in this FRD were set by Clod on Ryan's behalf on 2026-10-24 as recommended defaults. Ryan reviews them in the PR:
  - the 10-minute bound;
  - skip, never force;
  - fail closed on unknown state;
  - re-checking the guards after the wait;
  - naming the holder;
  - no retry;
  - no UI change.
- **A-002:** The agent can determine whether the dpkg lock is held, and by which process, without new privileges. This was verified on Hawaii (Debian 13, kernel 6.12, dpkg 1.22.22). The detection method is the architect's choice.
- **A-003:** apt holds `lock-frontend` for its whole run, and dpkg 1.19.1 or later takes it even when run directly. The legacy `lock` in FR-002 is defense in depth for older dpkg.
- **A-004:** rIOt's own maintenance runs are already kept apart from reboots by the existing maintenance-run guard. This story targets third-party package activity.
- **A-005:** The post-patch paths run after their command result is reported, so the command time budget does not limit them. They do hold the host command reservation for the length of the wait.

## 11. Open Questions

Each question has a recommended default. The FRD is written to the default.

- **OQ-001:** Should the 10-minute bound be configurable per agent? **Default: no.** It is a fixed 10 minutes on all three paths, and configurability can come later if a real case needs it.
- **OQ-002:** Should an unknown lock state be retried within the bound instead of skipping at once? **Default: no, skip immediately** (FR-010). The causes (permissions, unreadable or unexpected lock information) do not clear in practice, and waiting 10 minutes would only delay the report.
- **OQ-003:** The standalone reboot has a 1-minute agent hard maximum, which a 10-minute wait exceeds. **Default:** the standalone path uses the full 10-minute bound, and the architect chooses how to reconcile the budget. One option is raising the reboot budget in the server and agent command contract (CMD-LIFECYCLE-001); another is a different command shape. The `shutdown` budget, which shares the power class, must not change (FR-020).
- **OQ-004:** On the authorized path, the phase A result already says "reboot started: yes" before phase B waits. Should that wording change now that phase B can skip? **Default: no change in this story.** This is the pattern PATCH-FLOW-001 accepted, and the FR-017 warning event records the skip. Ryan to confirm.
- **OQ-005:** Should the next maintenance window retry a reboot that auto-patch skipped? **Default: no.** A later window reboots only if its own run applies reboot-class packages again, which keeps the INFO-3 no-loop property. The visible reboot-required state prompts the operator.

## 12. Dependencies

- **D-001:** PATCH-FLOW-001: the authorized `reboot_if_required` phase B and its abort reporting (`internal/agent/reboot_auth.go`), plus security review INFO-3 and INFO-4.
- **D-002:** PATCH-GATE: the unauthorized post-patch reboot and maintenance-window auto-patch (`internal/agent/commands.go`).
- **D-003:** Command time budgets for the power class (`internal/models/command_policy.go`) and CMD-LIFECYCLE-001 (OQ-003).
- **D-004:** The reboot call-site guard test (`internal/agent/reboot_callsites_test.go`, C-8). It must keep passing (FR-021).
- **D-005:** AGENT-PRIV / AGENT-HARDEN-001: the reason NFR-001 forbids new sudoers rules and `install.sh` re-runs.
- **D-006:** No new third-party dependency, no database migration, no new external service.
- **D-007:** Pipeline: architect (ADD), then security-researcher review, before engineering.
