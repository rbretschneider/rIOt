# Formal Requirements Document

- **Story ID:** EXPECT-007
- **Title:** Agent-side activity reporting: a reboot or shutdown that waits for the dpkg lock says so on the fleet and device views, agent update events carry their versions and a correlation reference so the server attributes them exactly instead of by message text and timing, a restart from a Ryan-pressed agent update is tied to that exact command, and a lost update-start event is retried
- **Author:** Business Developer Agent
- **Date:** 2026-10-27
- **Revision:** 1
- **Status:** DRAFT. Ryan was not available for an interview. Every clarification is settled by a default set on Ryan's behalf (Section 10 and Section 11.1). Ryan reviews them in the PR. Section 11.2 lists the decisions that need Ryan's call, each with a recommendation; the FRD is written to the recommendation.
- **Epic:** EXPECT (expected activity). Seventh story. Parents: `docs/requirements/EXPECT-001-frd.md` (A-007, flow F16) and `docs/requirements/EXPECT-002-frd.md` (Section 13, which defines this story's scope).
- **Base:** main at e120031 (v2.85.0 plus PR #47).

---

## 1. Executive Summary

EXPECT-001 and EXPECT-002 taught the server to recognise expected activity from what the agent already sends. Three of the things they promised need the agent to say more. (1) A reboot or shutdown can wait up to 10 minutes for the dpkg lock. The dashboard already has the label "Rebooting (waiting for dpkg lock)", but no agent ever reports that step, so Ryan only ever sees "Rebooting". (2) The server links agent update events to a command or a self-update record by parsing the message text and by timing rules (an `agent_update` command in flight or finished within 10 minutes; the newest self-update record within 10 minutes). It works, but it is a guess: a failure or completion from a Ryan-pressed update can be linked to an unrelated self-update record, and any change to the message wording would silently switch recognition off. (3) If the server misses the single "update started" event, for example because it is restarting at release time, the agent's restart raises a "device offline" push. This story makes the agent report the dpkg-wait step for standalone reboot and shutdown commands. It also makes `agent_update_started`, `_completed` and `_failed` carry their from and to versions and a correlation reference in structured fields: the command's ID on a Ryan- or schedule-started update, or an attempt ID the agent generates on its own self-update. The server prefers these fields, validates them as untrusted input, and falls back to today's EXPECT-002 behaviour whenever they are absent or invalid. As a lower-priority part, the agent retries these events within a fixed bound, and the server stores each one only once. The AGENT-PRIV one-click-apply restart named in EXPECT-001 flow F16 does not exist on main. This story re-scopes that item: a `riot-agent-apply` restart after a Ryan-pressed `agent_update` is tied to that exact command through the correlation reference, and any future rIOt-started agent restart must use the same mechanism. The change reaches devices by the normal agent self-update. It needs no `install.sh` re-run and no new agent privilege, and every mix of old and new agents and servers keeps working.

## 2. Background & Context

### 2.1 Why this story exists

EXPECT-002 Section 13 moved the agent-side parts of EXPECT-001 Section 13 item 2 into this story:
- the agent reports a step while a reboot or shutdown waits for the dpkg lock (EXPECT-001 A-007);
- an agent restart through AGENT-PRIV one-click apply or `riot-agent-apply` becomes an activity record with the actor phrase of whoever pressed it (flow F16);
- `agent_update_*` events carry from and to versions and a correlation ID in structured fields, and EXPECT-002's message parsing stays as the fallback;
- optional: update events are retried, so a lost start event no longer causes an alert (EXPECT-002 NFR-011).

### 2.2 What exists today (verified in code at e120031)

**dpkg-lock wait**
- The server and the dashboard already understand the waiting step. `models.StepWaitDpkgLock = "wait_dpkg_lock"` is defined in `internal/models/expect.go:87`. `internal/server/expect/activity.go:103` marks the activity as waiting when the command's current step equals it, and `web/src/utils/activity.ts` adds the "(waiting for dpkg lock)" suffix.
- The agent never reports that step. The shared gate `awaitPowerClearance` (`internal/agent/reboot_clearance.go:201`) waits at most 10 minutes from the first "held" observation. It writes Info log lines when a wait starts and ends, and it receives only a path and a command ID, with no handle on the command's progress reporter.
- The callers `handleReboot` (`internal/agent/commands.go:324`) and `handleShutdown` (`:400`) do hold the command's run context. Its `Step(...)` method (`internal/agent/command_lifecycle.go:370`) persists the step marker and sends a progress report.
- When the gate clears, the power action starts right after the gate's final lock check (REBOOT-DPKG-001 FR-012, release-to-poweroff under 10 seconds). The result reads "reboot initiated after waiting <d> for the dpkg lock" or "shutdown initiated ...". A skipped wait ends the command `failed` with "reboot skipped: dpkg lock held past the timeout (10m), <holder>" and similar texts (`powerSkippedResult`).
- Post-patch reboots (`maybeRebootAfterPatch`, `rebootAfterDelivery`) run after the `os_update` command has already reached a terminal state, so no live command remains whose step could be reported.

**Agent update events**
- `models.AgentEvent` (`internal/models/events.go:95`) has `Type`, `Severity`, `Message`, `CommandID`, `Updater`, `Outcome` and `Reason`. It has no version fields. `CommandID` is set only on the extra-updater event types.
- `reportUpdateEvent` (`internal/agent/updater.go:598`) sends one best-effort request with no command ID and no retry. A failure is logged at debug level only.
- Self-update path (`runUpdateCheck`, `updater.go:124`):
  1. sends `agent_update_available`;
  2. takes the host lock `automatic-agent-update`;
  3. sends `agent_update_started` "Agent update started: <from> → <to>" (l.199);
  4. applies the update.

  The signed hand-off is persisted in `agent-update-handoff.json` (`updateHandoff`: digest, request time, command ID, reported flag) before the root helper takes over. The restarted process's `finishSignedHandoff` (`signed_update_handoff.go:104`) sends `agent_update_completed` or `agent_update_failed` when the hand-off has no command ID.
- Command path (`handleTriggerUpdate`, `commands.go:1071`):
  - sends the same `agent_update_started` with no command ID (l.1111);
  - sends `agent_update_failed` on an error before the apply step (l.1118);
  - sends `agent_update_completed` "Agent updated to <v>" only on the unsigned or `installed` fast path (l.1123);
  - returns `pending` for a signed hand-off. The restarted process then completes the **command** with a command result, not an event (`signed_update_handoff.go:109-126`).
- Server: `ReceiveAgentEvent` (`internal/server/handlers/handlers.go:1198`) decodes the body leniently (unknown fields are ignored) and never reads the agent's `command_id` for `agent_update_*` types (EXPECT-002 ADD X-2). `linkAgentUpdateEvent` → `commands.Service.NoteAgentUpdateEvent` → `models.ParseSelfUpdateStart(message)`.
  - A start is attributed command-first by timing: the newest `agent_update` command of the device that was dispatched and is not terminal, or finished at most 10 minutes earlier.
  - A completed or failed event is attributed record-only, to the newest self-update record of the device started at most 10 minutes earlier. It is never linked to a command (X-16).
- A self-update record is a `commands` row with origin `agent_self_update` and `params` `{from_version, to_version}` (EXPECT-002 ADD AD-001, migration 000034).
- PR #46 (`325d81c`): an `agent_update` command window treats a re-registration after dispatch as the agent's return and closes 1 minute after it.

**AGENT-PRIV and `riot-agent-apply` (flow F16)**
- AGENT-PRIV was never merged. It sits on the local branch `AGENT-PRIV` (9 commits, last 2026-08-25). Its `sync_privileges` rewrites sudoers through `cmd/riot-apply-privileges` and does **not** restart the agent.
- `riot-agent-apply` (`cmd/riot-agent-apply`, AGENT-HARDEN-001) is launched only by the signed update hand-off (`sudo systemd-run --unit=riot-agent-update /usr/local/lib/riot/riot-agent-apply`), so only inside a Ryan- or schedule-started `agent_update` command (EXPECT-001 F14) or the agent's own self-update (EXPECT-002 F15). Its `--boot` mode runs in the unit's `ExecStartPre` and does not cause a restart of its own.
- `restartSelf` is called only from `updater.go` and `handleTriggerUpdate`. `agent_uninstall` exits; it does not restart.
- Therefore F16 has no distinct live path today.

### 2.3 Gaps this story closes

| # | Gap | Effect |
|---|-----|--------|
| S1 | The agent never reports the dpkg-wait step. | "Rebooting (waiting for dpkg lock)" and "Shutting down (waiting for dpkg lock)" never appear; a 10-minute wait looks like a hung reboot. |
| S2 | Command-path `agent_update_*` events carry no command ID; the server links them by timing. A completed or failed event is only ever linked to a self-update record. | A Ryan-pressed update's failure or completion within 10 minutes of a self-update is linked to the wrong record (EXPECT-002 ADD R-3). The start is linked to "the newest" `agent_update` command, not to the one being executed. |
| S3 | Self-update recognition depends on the exact message wording. | Rewording the message would silently stop suppression and fall back to offline pushes. |
| S4 | A completed or failed self-update event is matched to "the newest record within 10 minutes". | Exact only while at most one update happens per 10 minutes; nothing proves the match. |
| S5 | `agent_update_*` events are sent once. | A start lost while riot-server restarts (both update on release day) leaves no record, so the agent's restart pushes "device offline" (EXPECT-002 NFR-011). |
| S6 | Flow F16 (AGENT-PRIV apply restart) was promised but has no live path. | The requirement is untestable as written; a future restart path has no rule to follow. |

### 2.4 Flows

| # | Flow (EXPECT-001 Section 2.4 numbering) | Covered by |
|---|------|------------|
| F9 | Reboot, including the up-to-10-min dpkg-lock wait | EXPECT-001; the waiting label: **this story** |
| F10 | Shutdown, including the dpkg-lock wait | EXPECT-001; the waiting label: **this story** |
| F12, F13 | `os_update` with a post-patch reboot (manual or `auto_patch`) | EXPECT-001; the post-patch dpkg wait is **out of scope** (Section 9, OQ-003) |
| F14 | Fleet "Update agents" and single `agent_update` command, including the `riot-agent-apply` restart | EXPECT-001; exact attribution by command reference: **this story** |
| F15 | Agent self-update on its own schedule | EXPECT-002; structured versions, attempt reference, retries: **this story** |
| F16 | Agent restart through AGENT-PRIV one-click apply / `riot-agent-apply` | Re-scoped by **this story** (DEC-1): the only live `riot-agent-apply` restarts are F14 and F15; the forward rule for any future rIOt-started restart is BR-005 |

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Ryan (operator) | Presses reboot, shutdown and "Update agents"; reads the fleet, device, events and command history views; receives notifications. | Existing authenticated dashboard API. Nothing changes. |
| Schedule | An auto-update policy or the auto-patch orchestration that starts commands. | Unchanged. Treated like Ryan (EXPECT-001 BR-009). |
| rIOt Agent, new (this story's release or later) | Reports the dpkg-wait step; sends `agent_update_*` events with structured versions and a correlation reference; retries them within a bound. | Unchanged privileges. Device-key authenticated. Everything it sends is untrusted input to the server. |
| rIOt Agent, old (any earlier release) | Sends events as today, with no structured fields and no step. | Unchanged. Must keep getting EXPECT-001 and EXPECT-002 behaviour. |
| Root update helper (`riot-agent-apply`) | Installs a verified update and restarts the agent. | Unchanged. Not modified by this story. |
| rIOt Server, new | Validates and prefers structured fields, attributes by reference, de-duplicates retried events, shows the waiting label. | Unchanged. Gains the rules of this story. |
| rIOt Server, old (any earlier release) | Ignores unknown fields; recognises self-updates from the message (from v2.82.0, EXPECT-002) or not at all. | Unchanged. Must keep working with new agents. |
| Notification channels | The existing dispatcher and channels. | Unchanged. |

## 4. Functional Requirements

### Definitions

- **Standalone power command:** a `reboot` or `shutdown` command executed by the agent's reboot or shutdown handler. It does not include the post-patch reboot of an `os_update`.
- **dpkg wait:** for one attempt of a standalone power command, the period from the gate's first observation that the dpkg lock is held until the gate returns, either cleared or skipped (REBOOT-DPKG-001, SHUTDOWN-DPKG-001).
- **Waiting step:** the command step `wait_dpkg_lock` (`models.StepWaitDpkgLock`).
- **Update attempt:** one run of the agent's update procedure that sends `agent_update_started`. It runs on one of two paths:
  - the **command path**: an `agent_update` command, single or fleet bulk, started by Ryan or a schedule;
  - the **self-update path**: the agent's own scheduled update.
- **Update events:** `agent_update_started`, `agent_update_completed` and `agent_update_failed`. `agent_update_available` is not an update event in this FRD (OQ-006).
- **Structured versions:** the attempt's from and to versions, carried in dedicated fields of an update event, separate from its message.
- **Correlation reference:** a field of an update event that identifies its attempt. There are two kinds:
  - a **command reference** carries the ID of the `agent_update` command being executed;
  - an **attempt reference** carries an **attempt ID** that the agent generates for one self-update attempt.
- **Valid command reference:** a command reference received from the device that authenticated the request for its own ID, that passes Section 7.2, and that names a command which:
  - (a) belongs to that device;
  - (b) has action `agent_update`;
  - (c) is not a self-update record;
  - (d) has been dispatched; and
  - (e) is not terminal, or reached a terminal state at most 10 minutes before the event was received.
- **EXPECT-002 rules:** EXPECT-002 FR-001 to FR-022, in particular FR-002 (start message), FR-004 (command attribution by timing), FR-006 (one open record), FR-007 (rate cap) and FR-014 (completed/failed attribution by timing).
- **Old / new agent, old / new server:** a release before or including this story.

### A. The dpkg-wait step (agent)

- **FR-001:** When the dpkg wait of a standalone power command begins, the agent must report the waiting step for that command, as a progress report of that command.
- **FR-002:** When the gate finds the dpkg lock free (or absent) at its first check, the agent must not report the waiting step.
- **FR-003:** The agent must report the waiting step at most once per command, including when the lock is taken again after the guards passed and the wait continues under the same deadline.
- **FR-004:** When the dpkg wait ends cleared, the agent must report a step other than the waiting step for the command, and then send the result as today ("reboot initiated after waiting <d> for the dpkg lock", "shutdown initiated after waiting <d> for the dpkg lock"). The step change must be queued without waiting for its delivery: nothing may be added between the gate's final dpkg-lock check and the start of the power action (REBOOT-DPKG-001 FR-012 and its 10-second release-to-poweroff bound).
- **FR-005:** When the dpkg wait ends skipped (lock timeout, lock state unknown, a guard, agent shutting down), the command must end `failed` with the same message and reason as today, and its result must name the dpkg wait as the step in which it failed.
- **FR-006:** Reporting a step must never delay, block, retry, shorten or cancel the dpkg wait or the power action. A step report that is rejected, times out or is lost must not change the command's execution, result or outcome.
- **FR-007:** The post-patch reboot of an `os_update` must not report the waiting step and must behave exactly as today (OQ-003).

### B. The dpkg-wait label (server and dashboard)

- **FR-008:** While a dispatched or running standalone `reboot` command's current step is the waiting step and its device is online, the Fleet Overview and the device page header must show "Rebooting (waiting for dpkg lock)", with the actor phrase and the start time (EXPECT-001 FR-035, FR-036, FR-039). For `shutdown` the label is "Shutting down (waiting for dpkg lock)".
- **FR-009:** The "(waiting for dpkg lock)" suffix must disappear within 5 seconds, with no manual reload, after any of these: the command's step changes, the command reaches a terminal state, or the device goes offline. The label then follows EXPECT-001 FR-035 ("Rebooting", "Rebooting, back by <time>", "Shutting down", "Powered off by <actor phrase> at <time>", or the escalated text) (OQ-004).
- **FR-010:** The command's history entry must show the waiting step while it is current. After the command ends, the entry must show whether the wait cleared or the command failed in it.

### C. Structured versions and correlation reference (agent)

- **FR-011:** Every update event the new agent sends must carry structured versions: from = the agent version running when the attempt started, to = the version the attempt installs. A completed or failed event sent by the restarted process must carry the attempt's from and to versions, not its own running version as from.
- **FR-012:** On the command path, every update event must carry a command reference to the `agent_update` command being executed, and no attempt reference.
- **FR-013:** On the self-update path, every update event must carry an attempt reference and no command reference. The agent must:
  - generate a new attempt ID for every self-update attempt and never reuse one on that device;
  - persist the attempt ID and the attempt's versions with the signed hand-off before the root helper is launched, so the restarted process's completed or failed event carries the same attempt ID and versions.
- **FR-014:** When the restarted process finishes a hand-off that holds no attempt ID or no versions (the first update from an old agent to a new one), its completed or failed event must carry no attempt reference and no structured versions. The agent must never invent them.
- **FR-015:** The message text of every update event must stay byte-for-byte as today, in particular the start message "Agent update started: <from> → <to>" (U+2192). This lets old servers keep recognising self-updates (EXPECT-002 FR-002).

### D. Structured versions (server)

- **FR-016:** When an `agent_update_started` event carries structured versions that both pass Section 7.2 and do not match each other, the server must use them as the record's from and to versions, and in every label and message derived from them. In that case it must not consult the message.
- **FR-017:** When structured versions are absent, either one fails Section 7.2, or they match each other, the server must apply EXPECT-002 FR-002 to the message. When that also fails, EXPECT-002 FR-003 applies (no record, today's alerting).
- **FR-018:** When the structured versions and the versions in the message are both valid and differ, the structured versions must win (FR-016). The disagreement must produce no event and no notification, and at most one debug-level log line. The agent's message must be stored unchanged.
- **FR-019:** Structured versions on `agent_update_completed` and `agent_update_failed` must not change a record's versions or outcome. A disagreement with the attributed record's versions must not prevent attribution.

### E. Correlation reference (server)

- **FR-020:** The server must read a correlation reference on an update event only when the device that authenticated the request is the device named in the request (the EXPECT-002 ADD AD-002 gate). Otherwise the reference must be ignored.
- **FR-021:** An update event with a valid command reference must be linked to that command. It must not create a self-update record, must not count toward the rate cap (EXPECT-002 FR-007), and must not set or change any self-update record's outcome. This holds whatever the EXPECT-002 timing rules (FR-004, FR-014) would have chosen.
- **FR-022:** A command reference that is not valid must be ignored, and the event must be processed exactly as EXPECT-002 processes an event without a reference. Not valid covers:
  - a malformed reference;
  - an unknown command;
  - another device's command;
  - another action;
  - a self-update record;
  - a command that was not dispatched;
  - a command that reached a terminal state more than 10 minutes earlier.

  The request must not fail because of the reference. The event must never be linked to a command of another device.
- **FR-023:** When an `agent_update_started` event with an attempt reference creates a self-update record, the server must store the attempt ID with the record.
- **FR-024:** An `agent_update_started` event whose attempt reference matches a self-update record of the same device, open or closed, must not create a record, count toward the rate cap, or extend or reopen any window. It must be linked to that record.
- **FR-025:** An `agent_update_completed` or `agent_update_failed` event whose attempt reference matches a self-update record of the same device must be attributed to that record, whatever the record's age. EXPECT-002 FR-015 and FR-022 then apply.
- **FR-026:** An `agent_update_completed` or `agent_update_failed` event whose attempt reference matches no record of the device may be attributed by the EXPECT-002 FR-014 rule only to a record that has no attempt ID. Otherwise it must be stored as today, with no link.
- **FR-027:** An update event that carries both a command reference and an attempt reference, or whose reference fails Section 7.2, must be treated as carrying no reference.
- **FR-028:** An update event without any reference (an old agent, or FR-014) must be processed exactly as EXPECT-002 processes it today.

### F. Agent restarts from a Ryan-pressed update (flow F16, re-scoped, DEC-1)

- **FR-029:** When an `agent_update` command started by Ryan or a schedule leads to a `riot-agent-apply` restart, the attempt's update events must be linked to that exact command through the command reference. No self-update record may be created for that attempt. Every label and message for it must use the command's EXPECT-001 activity label and actor phrase (for example "Updating agent 2.85.0 → 2.86.0" and "by ryan"), and the restart's offline and online events must be linked to that command (EXPECT-001 FR-017, FR-044).
- **FR-030:** The `agent_update` command window must keep the PR #46 behaviour: a re-registration of the device after dispatch counts as the agent's return, and the window closes 1 minute after it, no later than its EXPECT-001 bound.

### G. Retry and de-duplication (optional part, DEC-2)

Priority: optional. The architect may move Section G to a follow-on story without blocking Sections A to F. If it is built, these requirements are mandatory as written.

- **FR-031:** The new agent must resend an update event when sending fails with a transport error, a timeout, an HTTP 5xx or an HTTP 429. It must not resend after any 2xx response or any other 4xx response.
- **FR-032:** The agent must make at most 5 sending attempts per update event. It must not start an attempt more than 90 seconds after the first attempt of that event. Each resend must carry the same type, severity, message, structured versions and correlation reference as the first.
- **FR-033:** On the self-update path, the agent must not start applying the update until `agent_update_started` has been accepted (any 2xx) or its FR-032 budget is exhausted. It must then proceed whether or not the event was delivered. The host lock stays held while it waits.
- **FR-034:** On the command path, the agent must not delay the update while it waits for delivery of an update event; the command's window already covers the restart.
- **FR-035:** The new server must store at most one event per (device, correlation reference, event type) for update events it links to a self-update record or a command. A repeat must:
  - be answered with success;
  - not be stored or broadcast again;
  - not create, extend, reopen or change any record, window, outcome or command.
- **FR-036:** When the budget of an event is exhausted without acceptance, the agent must write at most one Warn-level log line for that event and keep no further state for it.

### H. Unchanged behaviour

- **FR-037:** `agent_update_available` must be sent, stored, broadcast and alerted exactly as today (OQ-006).
- **FR-038:** The extra-updater event types (`extra_updater_started`, `extra_updater_finished`) and their existing `command_id` handling must not change.
- **FR-039:** EXPECT-001 windows, EXPECT-002 records and windows, their bounds, escalations, outcome events and labels must not change, except as FR-008 to FR-010 and FR-016 to FR-029 explicitly state.

### I. Documentation

- **FR-040:** The README expected-activity section must describe:
  - the reliable dpkg-wait label and that it shows only while the device is online;
  - which agent release reports it;
  - that update events carry versions and a correlation reference, and that the server prefers them and falls back to message parsing and timing for older agents;
  - that the update **into** this release still uses the EXPECT-002 behaviour (FR-014);
  - the retry bound and the 90-second self-update delay (if Section G is built);
  - the F16 re-scope and the BR-005 rule;
  - the residuals in NFR-013.
- **FR-041:** The CHANGELOG must have an entry for this story that says:
  - it is an agent and server change;
  - agents receive it through the normal agent self-update or "Update agents";
  - no `install.sh` re-run and no new privilege is required;
  - mixed fleets keep working (NFR-005).

## 5. Non-Functional Requirements

- **NFR-001 [Safety, hard rule]:** No test, test helper, fixture, script or tool written for this story may run a real reboot, shutdown, poweroff, `systemctl` power verb, `apt`, `apt-get`, `dpkg` operation, `dietpi-update` or agent self-update on any machine. Nothing may execute an installed `riot-agent` or `riot-agent-apply` binary on any host. Agent tests must use injected fakes:
  - the dpkg-lock probe;
  - the command runner;
  - the transport and server responses;
  - the hand-off file system;
  - the clock and the sleeper.

  A restart is simulated by a second agent instance over the same fake state, never by a process restart. Server tests use fakes or the test database and an injected clock.
- **NFR-002 [Bounded growth, database]:**
  - No new table, no new event type and no new kind of record.
  - The only new stored data is the attempt ID on a self-update record: at most 64 bytes per record. Records stay capped at 6 per device per rolling 24 hours (EXPECT-002 FR-007), so at most 384 bytes per device per day and at most about 35 KB per device over the 90-day retention.
  - Waiting-step reports: at most 2 step changes per standalone power command (enter and leave), inside the existing per-command limit of 64 steps.
  - Retried update events add **zero** stored events on a new server for events it links (FR-035).
  - An event the server cannot link (no matching record or command, or rate-capped) can be stored at most 5 times, and only when the server accepted it but the response was lost.
  - Per update attempt: at most 3 update events stored on a new server (started, plus completed or failed, plus at most one late completed or failed linked only), the same as today.
  - Event retention (90 days) and command retention are unchanged.
- **NFR-003 [Bounded growth, agent]:**
  - The hand-off file grows by at most one attempt ID (≤ 64 bytes) and two versions (≤ 32 bytes each). It stays one file per device and is overwritten per attempt.
  - Retry state: at most one pending event per update event type per process (≤ 3), released on acceptance, on a non-retryable answer or when the budget is exhausted.
  - No queue, file or outbox grows with retries.
  - Logs: no line above debug per attempt or per step report, and at most one Warn per abandoned event (FR-036).
- **NFR-004 [Bounded delay]:**
  - A self-update is delayed by start-event delivery by at most 90 seconds plus one request timeout (FR-033).
  - A command-path update is not delayed (FR-034).
  - A power action is not delayed by any step report (FR-004, FR-006).
  - A standalone power command's total duration is unchanged.
- **NFR-005 [Compatibility, mixed fleets]:** Every combination must work:
  - **Old agent, new server:** behaviour is exactly EXPECT-001 and EXPECT-002. No waiting label (EXPECT-001 A-007 fallback "Rebooting"). Attribution by message and timing.
  - **New agent, new server:** this FRD.
  - **New agent, old server (v2.82.0 or later, EXPECT-002):** the server ignores the new fields and the `command_id` of update events (lenient decoding, X-2) and recognises self-updates from the unchanged message (FR-015). It shows the waiting label because it already understands the step (v2.79.0 or later). Retried events can duplicate only when a response was lost (at most 4 extra copies per event).
  - **New agent, older server (before v2.79.0):** the step is stored as an ordinary step. No label, and no change to the command's outcome (FR-006).
  - **Upgrade order:** server first, agents first, or interleaved must all work. The first update **from** an old agent **into** this release uses EXPECT-002 behaviour (FR-014). The next update uses this FRD.
- **NFR-006 [Delivery, no installer re-run]:**
  - Agent changes reach devices only by the agent's self-update or an `agent_update` command.
  - This story must not change `install.sh`, any sudoers rule, the systemd unit, `riot-agent-apply`, `riot-apply-privileges`, the agent's configuration schema or its collector whitelist.
  - No `install.sh` re-run is required. The attempt ID lives in the agent-owned hand-off file. Step reports and update events use the existing device-key-authenticated channels. No new command or privilege is executed.
- **NFR-007 [Security, untrusted references and versions]:**
  - Every reference and version is untrusted and validated by Section 7.2.
  - An invalid reference falls back to EXPECT-002 behaviour; it never fails the request or causes an error response.
  - A device's reference can only link to that device's own commands and records (FR-020, FR-022).
  - A replayed or duplicated attempt ID never creates a record or extends suppression (FR-024).
  - A device's ability to delay its own offline alert stays within the EXPECT-002 budget: at most 6 records per rolling 24 hours, each ending at most 10 minutes after its start (EXPECT-002 NFR-010, ADD AD-010).
  - References and versions are never shown to Ryan unless they passed validation.
- **NFR-008 [Fail open to alerting]:** If the server cannot read the commands or records needed to validate a reference, it must process the event exactly as EXPECT-002 does without a reference. It must never fail the request, drop the event or suppress an offline transition that EXPECT-002 would not suppress.
- **NFR-009 [Performance]:**
  - Validating and attributing a reference must add no more than 100 ms at the 95th percentile to handling an update event at Ryan's scale (4 devices).
  - De-duplication must be within the same budget.
  - Label changes from FR-008 and FR-009 appear within 5 seconds.
- **NFR-010 [Durability]:**
  - The attempt ID and versions survive the agent's restart (FR-013).
  - On the server, attribution by reference and de-duplication must be derivable from durable data after a server restart (EXPECT-002 NFR-006).
  - A server restart between a start and its completion must not lose the record's attempt ID.
- **NFR-011 [Coexistence with EXPECT-008]:**
  - EXPECT-008 (installer re-run opens an expected window, origin `installer`, migration 000035) is in flight in `internal/server/expect/`. This story must not need migration 000035 and must not change the commands origin set or its CHECK constraints.
  - The requirements are written so that no migration is expected: the attempt ID can live with the existing record data. If the architect finds one is needed, its number is assigned at merge time, never fixed in advance.
  - Changes to `internal/server/expect/` must be limited to what FR-008 to FR-010 need.
- **NFR-012 [Testability]:** Every FR must be verifiable by automated tests with an injected clock and test doubles, without real elapsed time, real network or real power actions.
- **NFR-013 [Accepted residuals]:**
  - Old agents keep EXPECT-002 behaviour and the plain "Rebooting" label until they update.
  - The update into this release is recognised by message and timing only (FR-014).
  - If the step change and the result are both lost because the host went down first, the label falls back to "Rebooting" as soon as the device is marked offline (FR-009). While it is still online, it can say "waiting" for at most the 45-second offline threshold after the real reboot began.
  - A start lost because the server was unreachable for longer than the FR-032 budget still leads to an offline alert, unless the EXPECT-001 server restart grace covers it.
  - A command that was reverted to queued and then executed by the agent has no valid command reference (not dispatched, FR-022). Its start falls back to EXPECT-002 and may create a self-update record "by agent auto-update". This fails toward suppression with the wrong actor phrase, as today.
  - On an old server, a retried event whose response was lost can be stored up to 5 times.
- **NFR-014 [No regression]:** These acceptance tests must pass unchanged, except where this FRD explicitly extends a label or attribution rule:
  - EXPECT-001, EXPECT-002 and PR #46;
  - REBOOT-DPKG-001, SHUTDOWN-DPKG-001 and SHUTDOWN-SUDO-001;
  - AGENT-HARDEN-001's signed-update tests;
  - the extra-updater tests.

## 6. Business Rules

- **BR-001:** Evidence beats inference. When the agent names the command or attempt an event belongs to, and the name checks out, the server uses it. Message parsing and timing are the fallback for agents that do not name one.
- **BR-002:** Never worse than today. Any missing, invalid or unreadable reference gives exactly the EXPECT-002 behaviour.
- **BR-003:** A device's own data can only affect that device. A reference never links one device's event to another device's command or record.
- **BR-004:** Who started an agent restart is shown truthfully: a Ryan- or schedule-started update shows its actor phrase (for example "by ryan"); the agent's own update shows "by agent auto-update".
- **BR-005 (forward rule):** Any future agent restart that rIOt starts, including an AGENT-PRIV one-click apply if it is ever merged with a restart, must be a command whose events carry that command's reference. It must be attributed by the mechanism of Section E, and must never rely on a timing rule or create a self-update record. Building that is a follow-on (Section 13), not part of this story.
- **BR-006:** A label tells the truth or says less. It never claims a dpkg wait that the server knows has ended, or for a device that is offline.
- **BR-007:** Mixed fleets are normal. No part of this story may require the server and all agents to be upgraded together, or require an `install.sh` re-run.
- **BR-008:** Reporting never gets in the way of the action. A power action or update is never blocked by a failure to report it, and is delayed at most by the stated bounds.

## 7. Data Requirements

### 7.1 Entities

| Entity | New or existing | Change |
|--------|-----------------|--------|
| Command (`reboot`, `shutdown`) | Existing | The current step can be the waiting step. The step history shows the wait and how it ended (FR-010). No new field. |
| Command (`agent_update`) | Existing | Target of a valid command reference (FR-021). Unchanged. |
| Self-update record (EXPECT-002, `commands` row with origin `agent_self_update`) | Existing | Gains the attempt ID (≤ 64 bytes) when the start carried one (FR-023). The architect places it; the recommended place is the record's existing parameter data, so no migration is needed (NFR-011). |
| Update event (agent payload) | Existing | Gains structured from and to versions and one correlation reference (command or attempt kind). The message and other fields are unchanged. The field names and the way the kind is expressed are the architect's choice, but the server must be able to tell the two kinds apart without guessing from the value's format. |
| Stored event | Existing | Unchanged content. Linked to a command or record as today. Repeats are not stored (FR-035). |
| Signed hand-off file (agent) | Existing | Gains the attempt ID and the attempt's versions for self-update attempts (FR-013). |

### 7.2 Validation

- **Version (structured from and to):** the EXPECT-002 Section 7.2 version rules:
  - 1 to 32 bytes;
  - optional leading "v";
  - three dot-separated groups of 1 to 5 digits;
  - optionally "-" or "+" followed by 1 to 16 letters, digits, dots or hyphens.

  From and to must not match (leading "v" ignored).
- **Command reference:** 1 to 64 bytes, ASCII letters, digits and hyphens only, and valid by the Section 4 definition.
- **Attempt ID:**
  - The agent generates 16 to 64 bytes of ASCII letters, digits and hyphens, unpredictable, and unique per attempt on a device.
  - The server accepts 1 to 64 bytes of ASCII letters, digits and hyphens, and treats anything else as no reference (FR-027).
- **Both references present:** treated as no reference (FR-027).
- **Message:** unchanged EXPECT-002 rules, used only on the fallback path (FR-017).
- **Linking:** an event is linked only to a command or record of the same device.

### 7.3 States

**Standalone power command with a dpkg wait**

| From | Event | To | Effect |
|------|-------|----|--------|
| Running, no step or another step | Gate sees the lock held for the first time | Running, waiting step | One step report. Label "<Action> (waiting for dpkg lock)" while the device is online. |
| Running, waiting step | Lock taken again after the guards passed | Same | No new report (FR-003). |
| Running, waiting step | Wait cleared | Running, power step, then `succeeded` "initiated after waiting <d>" | Step change queued without waiting. Power action starts at once. Suffix cleared within 5 s of the step change or result. |
| Running, waiting step | Wait skipped | `failed`, failed in the dpkg wait | Same message as today. Suffix cleared. EXPECT-001 FR-004 and FR-020 apply. |
| Running, waiting step | Device marked offline | Same | Suffix hidden; "Rebooting" or "Shutting down" (FR-009). |

**Update event at the new server**

| Reference on the event | Condition | Outcome |
|------|-----------|---------|
| Command reference | Valid | Linked to that command. No record. No outcome change (FR-021). |
| Command reference | Not valid | EXPECT-002 behaviour, as if no reference (FR-022). |
| Attempt reference, started | No record with that ID | EXPECT-002 FR-001 conditions with FR-016 versions. A created record stores the ID (FR-023). |
| Attempt reference, started | A record of the device has that ID | Linked. No new record, no window change (FR-024). |
| Attempt reference, completed or failed | A record of the device has that ID | Attributed. EXPECT-002 FR-015 and FR-022 (FR-025). |
| Attempt reference, completed or failed | No record has that ID | Attributed by timing only to a record without an attempt ID, else unlinked (FR-026). |
| Both kinds, or malformed | Any | As no reference (FR-027). |
| None | Any | EXPECT-002 (FR-028). |
| Any linked event | Same (device, reference, type) already stored | Success, not stored, no effect (FR-035). |

## 8. Acceptance Criteria

### dpkg-wait step and label

**AC-001: Reboot waiting for the dpkg lock shows it** [FR-001, FR-008]
- Given vermont online on a new agent, and a `reboot` by ryan dispatched at 10:00:00, and a fake dpkg probe that reports the lock held by "apt-get (pid 1234)" from 10:00:01
- When the agent's gate first observes the lock held
- Then the agent sends exactly one progress report for the command with step `wait_dpkg_lock`
- And within 5 s of the server receiving it, the Fleet Overview and vermont's device page show "Rebooting (waiting for dpkg lock)", "by ryan" and the start time 10:00

**AC-002: Wait clears, power action is not delayed** [FR-004, FR-006, FR-009]
- Given the AC-001 state, and a fake transport that blocks every request indefinitely after the waiting-step report
- When the fake probe reports the lock free at 10:04:00 and the guards pass
- Then the fake power runner is invoked right after the gate's final lock check, with no transport call awaited between them
- And the agent queues a step other than `wait_dpkg_lock` and then the result "reboot initiated after waiting 4m for the dpkg lock"
- And, in a run where the transport delivers, the label no longer contains "(waiting for dpkg lock)" within 5 s of the server receiving the step change or the result

**AC-003: Wait times out** [FR-005, FR-009, FR-010]
- Given a `reboot` by ryan on vermont whose fake probe reports the lock held for the whole 10-minute bound
- When the gate gives up
- Then the command ends `failed` with the message "reboot skipped: dpkg lock held past the timeout (10m), apt-get (pid 1234)" exactly as today, and the result names the dpkg wait as the failed step
- And the command history shows the wait and that it failed, the waiting label clears within 5 s, and with vermont online no notification is sent (EXPECT-001 AC-017)

**AC-004: Shutdown waiting for the dpkg lock** [FR-001, FR-004, FR-008]
- Given a `shutdown` by ryan on PiCUPS and a fake probe that reports the lock held for 2 minutes, then free
- Then PiCUPS shows "Shutting down (waiting for dpkg lock)" while it waits, the fake poweroff runner is invoked once, and the result reads "shutdown initiated after waiting 2m for the dpkg lock"

**AC-005: No wait, no step** [FR-002]
- Given a `reboot` whose fake probe reports the lock free at the first check
- Then no `wait_dpkg_lock` report is sent and the label is "Rebooting" until the result

**AC-006: Lock re-taken after the guards passed reports once** [FR-003]
- Given a `reboot` whose fake probe reports held, free, held again, then free
- Then exactly one `wait_dpkg_lock` report is sent for the command, and the wait deadline counts from the first held observation

**AC-007: Step report failure changes nothing** [FR-006]
- Given, in turn, a fake transport that rejects the progress report with HTTP 400, one that returns 500, and one that drops it
- When the AC-002 timeline runs in each case
- Then the gate's observations, the power runner invocation, the result text and the fake-clock times are identical to a run with a working transport

**AC-008: Offline hides the waiting suffix** [FR-009, BR-006]
- Given a running `reboot` on vermont whose current step is `wait_dpkg_lock`
- When vermont is marked offline
- Then within 5 s the label is "Rebooting" without the suffix, and the device is counted under "Expected offline" (EXPECT-001 FR-038)

**AC-009: Post-patch reboot unchanged** [FR-007]
- Given an `os_update` with reboot authorization that succeeded, followed by a post-patch reboot that waits 3 minutes for the dpkg lock
- Then no `wait_dpkg_lock` report is sent, and the agent's behaviour, messages and the dashboard labels match today's tests

**AC-010: Old agent falls back** [NFR-005]
- Given an old agent that never reports the waiting step, and a `reboot` that waits 4 minutes for the dpkg lock
- Then the label shows "Rebooting" throughout the wait, and the command's handling is identical to today

### Structured versions and attempt reference

**AC-011: Self-update with structured fields** [FR-011, FR-013, FR-016, FR-023, FR-025]
- Given hawaii on a new agent 2.86.0, online, with no `agent_update` command
- When it sends `agent_update_started` with message "Agent update started: 2.86.0 → 2.87.0", structured versions 2.86.0 and 2.87.0 and attempt ID A; goes offline; registers on 2.87.0; and the restarted process sends `agent_update_completed` with attempt ID A and versions 2.86.0 and 2.87.0
- Then one self-update record exists with from 2.86.0, to 2.87.0 and attempt ID A, its outcome is `succeeded`, both events are linked to it, the offline transition is an info event, and no notification is sent

**AC-012: Attempt ID is unique and survives the restart** [FR-011, FR-013, NFR-010]
- Given a fake agent with a fake hand-off file system that runs two self-update attempts
- Then the two attempt IDs differ and each is 16 to 64 bytes of letters, digits and hyphens
- And when a second agent instance on the same fake state finishes the first hand-off, its completed event carries the first attempt's ID and versions, not its own running version as from

**AC-013: First update from an old agent** [FR-014, FR-028]
- Given a hand-off file written by an old agent (no attempt ID, no versions), and a new agent instance that finishes it
- Then its completed event carries no attempt reference and no structured versions
- And the server attributes it exactly as EXPECT-002 AC-001 does

**AC-014: Message text unchanged** [FR-015]
- Given the new agent's update events on both paths
- Then every message is byte-for-byte equal to today's ("Agent update started: <from> → <to>" with U+2192, "Agent update failed: <err>", "Agent updated to <v>", and the signed completion and failure texts)

**AC-015: Structured versions win over the message** [FR-016, FR-018]
- Given hawaii sends a start with structured versions 2.86.0 and 2.87.0, attempt ID A, and the message "Agent update started: 2.86.0 → 2.99.0"
- Then the record's versions are 2.86.0 and 2.87.0, the label reads "Updating agent 2.86.0 → 2.87.0", a registration on 2.87.0 closes the window as succeeded-pending, no extra event or notification is produced, and at most one debug line records the disagreement

**AC-016: Invalid structured versions fall back to the message** [FR-017]
- Given, in turn, structured versions "latest" and "2.87.0", versions "2.87.0" and "2.87.0", and to-version of 300 bytes, each with the valid message "Agent update started: 2.86.0 → 2.87.0"
- Then each creates a record with from 2.86.0 and to 2.87.0 from the message

**AC-017: Both invalid means no record** [FR-017, NFR-008]
- Given a start with invalid structured versions and the message "Agent update started: dev → 2.87.0"
- When hawaii then goes offline
- Then no record is created, the event is stored as today, and the `device_offline` alert notifies

**AC-018: Replayed attempt ID never extends suppression** [FR-024, NFR-007]
- Given a record for hawaii with attempt ID A that started at 10:00:00 and ended `failed` at 10:01:00
- When hawaii sends a valid start with attempt ID A at 10:20:00 and goes offline at 10:21:00
- Then no record is created, no window opens, the start is linked to the existing record, the 6-per-24h count is unchanged, and the offline transition alerts and notifies

**AC-019: Completion with an unknown attempt ID** [FR-025, FR-026]
- Given an open record R1 for hawaii with attempt ID A, started at 10:00:00
- When `agent_update_completed` with attempt ID B arrives at 10:02:00
- Then it is stored with no link and R1's outcome is unchanged
- And given instead a record R0 for hawaii without an attempt ID started at 10:00:00, the same event is attributed to R0 by the EXPECT-002 timing rule

### Command reference and F16 re-scope

**AC-020: Ryan's fleet update is tied to its command** [FR-012, FR-021, FR-029]
- Given Ryan presses "Update agents" and command C for vermont (from 2.86.0 to 2.87.0) is dispatched at 10:00:00
- When vermont sends `agent_update_started` with command reference C at 10:00:02, the signed helper restarts the agent, vermont is offline from 10:00:50 and re-registers on 2.87.0 at 10:01:40, and the restarted process completes C with a command result
- Then no self-update record exists, the start is linked to C, vermont shows "Updating agent 2.86.0 → 2.87.0" with "by ryan", the offline transition is an info event linked to C, the online event names "Agent update by ryan", and no notification is sent

**AC-021: A command-path failure no longer touches a self-update record** [FR-021]
- Given a self-update record R for hawaii started at 10:00:00 that ended `failed` at 10:01:00, and Ryan's `agent_update` command C for hawaii dispatched at 10:05:00
- When hawaii sends `agent_update_failed` "Agent update failed: download: connection reset" with command reference C at 10:05:30
- Then the event is linked to C, not to R; R's outcome, links and events are unchanged; and no self-update outcome event is written

**AC-022: A command-path completion is linked to its command** [FR-021]
- Given a self-update record R for hawaii that ended `succeeded` at 10:02:00, and Ryan's `agent_update` command C dispatched at 10:06:00 on the unsigned fast path
- When hawaii sends `agent_update_completed` "Agent updated to 2.88.0" with command reference C at 10:06:20
- Then the event is linked to C and not to R

**AC-023: Invalid command references fall back safely** [FR-020, FR-022, NFR-007]
- Given hawaii sends, in turn, `agent_update_started` with these command references:
  - the ID of vermont's dispatched `agent_update` command;
  - an unknown ID;
  - the ID of hawaii's dispatched `docker_update` command;
  - the ID of hawaii's own self-update record;
  - the ID of hawaii's queued, undispatched `agent_update` command;
  - the ID of hawaii's `agent_update` command that finished 11 minutes earlier;
  - a 300-byte string;
  - a string with control characters.

  Each is sent with a valid message and no attributable command.
- Then every request is answered with success, every event is stored, each is processed exactly as EXPECT-002 processes a start without a reference (a record is created when EXPECT-002 would create one), no event is ever linked to vermont's command, and vermont's command history and window are unchanged
- And the same event sent with a different device's key, or for a different device ID than the authenticated one, has its reference ignored

**AC-024: Both references on one event** [FR-027]
- Given a start that carries a valid command reference and an attempt ID
- Then it is processed as carrying no reference (EXPECT-002 behaviour)

**AC-025: PR #46 behaviour is kept** [FR-030]
- Given command C for vermont dispatched at 10:00:00, vermont offline from 10:00:50, and vermont re-registering at 10:01:30 before C's result arrives
- Then the offline transition is an info event linked to C, the window closes at 10:02:30, and an offline transition at 10:03:00 alerts normally

### Mixed fleets and fail-open

**AC-026: Old agent, new server** [FR-028, NFR-005]
- Given an agent that sends update events with no structured versions and no reference
- When the EXPECT-002 AC-001, AC-006, AC-009 and AC-012 timelines run on the new server
- Then every outcome, link, event and notification is identical to EXPECT-002's expected results

**AC-027: New agent, old server** [FR-015, NFR-005]
- Given the new agent's update event payloads on both paths
- When they are decoded with the AgentEvent shape and the handler of the EXPECT-002 release (the current main handler before this story)
- Then decoding succeeds and the event is accepted, the self-update start is recognised from the message, and the command reference is not used
- And a `wait_dpkg_lock` progress report is accepted by the EXPECT-001 step validation

**AC-028: Fail open when references cannot be checked** [NFR-008]
- Given the command and record store cannot be read
- When hawaii sends a start with a command reference, and separately one with an attempt reference, then goes offline
- Then each request is answered as EXPECT-002 answers it, the events are stored, and the offline alert behaves exactly as under EXPECT-002 AC-020

**AC-029: One device cannot affect another** [FR-020, FR-022, BR-003]
- Given an open record for hawaii with attempt ID A and Ryan's command C for hawaii
- When vermont sends `agent_update_completed` with attempt ID A and `agent_update_failed` with command reference C
- Then hawaii's record, command, labels and events are unchanged, and vermont's events are processed as for no reference

### Retry and de-duplication (optional part)

**AC-030: Start is retried and the self-update waits for it** [FR-031, FR-032, FR-033]
- Given a self-update on a new agent, and a fake transport that refuses the connection for the first two attempts and returns 200 on the third, 15 s after the first
- Then the agent makes exactly three attempts with identical content, the fake apply step is invoked only after the 200, and the host lock is held throughout

**AC-031: Retry budget is bounded and the update proceeds** [FR-032, FR-033, FR-036, NFR-004]
- Given a fake transport that always fails with a timeout
- Then the agent makes at most 5 attempts, none starting more than 90 s after the first, the fake apply step is invoked no later than 90 s plus one request timeout after the first attempt, and exactly one Warn line is written for the event

**AC-032: No retry on a rejection** [FR-031]
- Given a fake transport that answers the start with 400
- Then exactly one attempt is made and the update proceeds at once

**AC-033: Command path does not wait** [FR-034]
- Given an `agent_update` command and a fake transport that blocks the start event indefinitely
- Then the fake apply step is invoked without waiting for the event, and the command's result is not delayed by it

**AC-034: Retried events are stored once** [FR-035, NFR-002]
- Given the server receives the same start with attempt ID A three times (the agent never saw the responses), and the same `agent_update_failed` with command reference C twice
- Then exactly one start and one failed event are stored and broadcast, every request is answered with success, exactly one record exists, its window ends at its original bound, and C is unchanged by the repeat

### Bounds, delivery, safety, coexistence, docs

**AC-035: Bounded growth** [NFR-002, NFR-003]
- Given the AC-011 timeline with every update event delivered twice, and the AC-001 to AC-002 timeline
- Then the server stored exactly one started event, one completed event, one info offline event and one online event for the attempt, plus the record with an attempt ID of at most 64 bytes
- And no new event type or table exists
- And the power command recorded at most 2 step changes for the wait
- And the agent wrote no log line above debug except the existing dpkg-wait Info lines
- And the hand-off file is one file of bounded size

**AC-036: No installer re-run and no privilege change** [NFR-006]
- Given the story's diff
- When it is inspected
- Then `scripts/install.sh`, every sudoers rule, the systemd unit, `cmd/riot-agent-apply`, `cmd/riot-apply-privileges` and the agent configuration schema and collector whitelist are unchanged, and the agent invokes no new command through sudo

**AC-037: No real power actions, updates or agent execution in tests** [NFR-001, NFR-012]
- Given the tests, helpers and scripts added or changed by this story
- When they are inspected and run
- Then none of them:
  - invokes reboot, shutdown, poweroff, a `systemctl` power verb, `apt`, `apt-get`, `dpkg`, `dietpi-update` or `systemd-run`;
  - performs a real agent self-update;
  - executes an installed `riot-agent` or `riot-agent-apply`;
  - sleeps in real time.
- And all agent behaviour is simulated with injected fakes and an injected clock

**AC-038: No regression** [FR-037, FR-038, FR-039, NFR-014]
- Given these acceptance tests:
  - EXPECT-001, EXPECT-002 and PR #46;
  - REBOOT-DPKG-001, SHUTDOWN-DPKG-001 and SHUTDOWN-SUDO-001;
  - AGENT-HARDEN-001's signed-update tests;
  - the extra-updater tests;
  - the `agent_update_available` tests.
- When they run after this story
- Then they pass, changed only where this FRD extends a label or an attribution rule

**AC-039: Coexistence with EXPECT-008** [NFR-011]
- Given the story's diff
- Then it adds no migration numbered 000035, does not change the commands origin set or its CHECK constraints, and any migration it does add has its number assigned at merge time

**AC-040: Documentation** [FR-040, FR-041]
- Given the README and CHANGELOG after this story
- When they are read
- Then the README covers:
  - the dpkg-wait label and its online-only rule;
  - the reporting agent release;
  - structured versions and references with their fallback;
  - the first-update caveat;
  - the retry bound and the self-update delay, if built;
  - the F16 re-scope and BR-005;
  - the NFR-013 residuals.
- And the CHANGELOG says that agents receive the change by self-update or "Update agents", that no `install.sh` re-run or new privilege is needed, and that mixed fleets keep working

## 9. Out of Scope

- The dpkg-lock wait of a post-patch reboot (`maybeRebootAfterPatch`, `rebootAfterDelivery`). The `os_update` command is already terminal, so a "waiting" state would need a new server-side state after a terminal command (OQ-003, follow-on candidate in Section 13).
- The dpkg-lock wait of an `os_update` before it patches.
- Building AGENT-PRIV, or any new agent restart path. BR-005 is a rule for a future story, not built here.
- Any change to `agent_update_available` (OQ-006).
- Changing the EXPECT-001 command-window label, which takes <from> from the device's version when the command was created and <to> from the newest known version. It does not use the structured versions of a command-path event (OQ-007).
- A signed version signal or anti-rollback for updates (AGENT-HARDEN-001 AD-010 follow-up).
- Notifications for failed updates or failed power commands (EXPECT-004).
- Retrying any event other than the three update events: Docker events, extra-updater events and command results keep their own delivery rules.
- Any change to `install.sh`, sudoers, the systemd unit, `riot-agent-apply` or the collector whitelist.

## 10. Assumptions

- **A-001:** All defaults in this FRD were set by the Business Developer Agent on Ryan's behalf on 2026-10-27, from the orchestrating session's grounding (Clod) and the code at e120031. Ryan reviews them in the PR.
- **A-002:** The agent's existing progress channel (`cmdRun.Step`, `notifyProgress`) delivers a step to the server asynchronously, so queuing a step change does not block the caller (FR-004). The architect confirms (Q-2).
- **A-003:** Command IDs are at most 64 bytes of letters, digits and hyphens (the server generates them). The architect confirms (Q-5).
- **A-004:** `ReceiveAgentEvent` on every server release since EXPECT-002 decodes the body without rejecting unknown fields (verified at e120031: `json.NewDecoder(...).Decode` with no `DisallowUnknownFields`). The architect confirms for releases back to the oldest server Ryan may still run (Q-6).
- **A-005:** Servers from v2.79.0 accept and display the `wait_dpkg_lock` step (verified in `expect/activity.go` and `activity.ts`).
- **A-006:** riot-server and the agents update on the same release day, so a start event sent while riot-server restarts is the realistic cause of a lost start (EXPECT-002 Section 2.2). A server restart normally takes well under 90 seconds.
- **A-007:** No other agent restart path exists on main (verified: `restartSelf` callers, `riot-agent-apply` launch sites, `agent_uninstall`).
- **A-008:** Holding the host lock for up to 90 more seconds during a self-update is acceptable. Commands that need the host lock meet the existing conflict handling, as they already do during a self-update.

## 11. Open Questions and Decisions

### 11.1 Clarifications settled by default (Ryan reviews in PR)

- **OQ-001 What happens to the step when the wait ends:**
  - Cleared: the agent reports a non-waiting step, queued without delay, and then the "initiated" result, as today (FR-004).
  - Skipped: the command ends `failed` exactly as today and names the wait as the failed step (FR-005).
- **OQ-002 Report only real waits:** the step is reported only when the lock was actually observed held, at most once per command (FR-002, FR-003).
- **OQ-003 Post-patch reboots:** out of scope. The `os_update` is already terminal when its reboot waits, so there is no live command to carry the step. Its label is the EXPECT-001 return-phase label "Rebooting after patch, back by <time>" (Section 9).
- **OQ-004 Waiting suffix only while online:** the suffix is hidden when the device goes offline (FR-009). This keeps the label truthful when the step change and the result are lost because the host went down first.
- **OQ-005 Correlation kinds:** the command ID for the command path; an agent-generated attempt ID for self-updates; never both on one event (FR-012, FR-013, FR-027).
- **OQ-006 `agent_update_available`:** unchanged. No structured fields, no reference, no retry. It precedes the attempt, often by a long time, and the server has no use for it (EXPECT-002 DEC-3).
- **OQ-007 Command labels keep their EXPECT-001 versions:** structured versions on a command-path event are not used for the command's label (Section 9).
- **OQ-008 Completion with an unknown attempt ID:** never linked to a record that has a different attempt ID; the timing fallback applies only to records without an attempt ID (FR-026).
- **OQ-009 Replayed attempt ID:** it never creates a record or extends a window, even after the matching record closed (FR-024).
- **OQ-010 De-duplication scope:** events the new server links are stored once. Unlinked repeats are bounded by the agent's retry cap (FR-035, NFR-002), which avoids a new persistent index of every reference.

### 11.2 Open decisions for Ryan

- **DEC-1 Re-scope flow F16.** EXPECT-001 and EXPECT-002 promised that an AGENT-PRIV one-click apply or `riot-agent-apply` restart becomes its own activity record "with the actor phrase of whoever pressed it". AGENT-PRIV was never merged and does not restart the agent, and `riot-agent-apply` only runs inside an `agent_update` command or a self-update, both of which already have windows.
  - Options:
    - (a) Re-scope to "a `riot-agent-apply` restart after a Ryan-pressed `agent_update` is tied to that exact command by the command reference, not by the timing rule", and record the forward rule BR-005 for any future rIOt-started restart;
    - (b) build AGENT-PRIV with a restart in this story;
    - (c) drop F16.
  - **Recommendation:** (a). It delivers the part of F16 that exists, keeps this story small, and stops a future restart path from shipping with a timing guess (FR-029, BR-005). The FRD is written to this.
- **DEC-2 Retries and the 90-second wait (Section G).**
  - Options:
    - (a) build it now: at most 5 attempts within 90 s; the self-update waits for its start event up to that bound and then proceeds; the command path never waits;
    - (b) retry without waiting, which does not fix the lost-start alert, because the restart kills the retrying process;
    - (c) defer Section G to a follow-on.
  - **Recommendation:** (a). The only realistic loss is a release-day riot-server restart, which takes well under 90 s, and a self-update has no deadline that a 90-second delay could hurt. The FRD is written to this, and the architect may still defer Section G without blocking the rest.
- **DEC-3 Structured versions disagree with the message.**
  - Options:
    - (a) structured versions win and the message is ignored (FR-018);
    - (b) treat the event as unrecognised: no record, offline alerts as today;
    - (c) write a warning event.
  - **Recommendation:** (a). Both come from the same agent, the structured fields exist so the message can stop being parsed, and (b) would turn an agent wording bug into offline pushes for every release. The FRD is written to this.
- **DEC-4 A reference that names a command reverted to queued.** The agent is executing it, but it was not dispatched.
  - Options:
    - (a) treat it as not valid and fall back to EXPECT-002, which may create a self-update record "by agent auto-update" (NFR-013);
    - (b) accept it, which links the start to a command that has no window, so the restart alerts.
  - **Recommendation:** (a). It keeps suppression and only the actor phrase is wrong, in a rare case. The FRD is written to this.

### 11.3 Questions for the architect (not for Ryan)

- **Q-1:** Where the attempt ID is stored (recommended: the self-update record's existing parameter data) and how FR-035 de-duplication is done without a migration, for example by checking for an event of the same type already linked to the same record or command. If a migration is unavoidable, its number is assigned at merge time and must not be 000035 (NFR-011).
- **Q-2:** Confirm that `cmdRun.Step` and the progress notifier never block the caller (A-002). If they can block, state how FR-004's "queued without waiting" is met without changing REBOOT-DPKG-001's final-check-then-poweroff order.
- **Q-3:** When a standalone reboot's result is still undelivered as the host goes down, does the agent re-send it from its durable state after boot? This determines how long a command can stay `running` with the waiting step, and so how often the FR-009 offline rule is what clears the label.
- **Q-4:** How the event payload expresses the two reference kinds so the server never infers the kind from the value (Section 7.1). Note that the existing `command_id` field is already sent by the agent on the extra-updater types and is ignored by every server for `agent_update_*` (X-2). Reusing it for the command reference is safe with old servers, but needs the explicit-kind rule.
- **Q-5:** Confirm the command ID format against Section 7.2 (A-003).
- **Q-6:** Confirm that no server release Ryan might still run rejects unknown JSON fields on the agent event endpoint (A-004).
- **Q-7:** The request timeout of `ReportEvent` and how it counts toward the 90-second budget (FR-032, NFR-004).
- **Q-8:** Whether the restarted process's hand-off recovery (`recoverAutomaticSignedHandoff`) needs the attempt ID for a hand-off written by an old agent, and confirm FR-014 holds on that path.

## 12. Dependencies

- **D-001:** EXPECT-001 (v2.79.0): windows, activity labels, `StepWaitDpkgLock`, the waiting suffix, restart grace.
- **D-002:** EXPECT-002 (v2.82.0): self-update records, `ParseSelfUpdateStart`, timing attribution, rate cap, X-2 precedent. PR #41 tooltip fix.
- **D-003:** PR #46 (`325d81c`): agent_update window return on re-registration.
- **D-004:** REBOOT-DPKG-001 (PR #34), SHUTDOWN-DPKG-001 (PR #35), SHUTDOWN-SUDO-001 (PR #36): the shared dpkg gate, its bounds and messages.
- **D-005:** AGENT-HARDEN-001: signed hand-off, `riot-agent-apply`, hand-off file.
- **D-006:** CMD-LIFECYCLE-001: command progress reports, step markers, command results.
- **D-007:** EXPECT-008 (in flight, migration 000035): coexistence only (NFR-011). Merge order with this story is free.
- **D-008:** Pipeline: architect (ADD), security-researcher, senior-dev, qa-engineer, technical-writer. No new third-party dependency.

## 13. Follow-on stories

- **EXPECT-007 (this story):** dpkg-wait step for standalone reboot and shutdown; structured versions and correlation references on update events with EXPECT-002 as the fallback; exact command attribution for `riot-agent-apply` restarts after a Ryan-pressed update (F16 re-scoped); optional bounded retries with de-duplication.
- **Candidate, post-patch dpkg wait:** show "Rebooting after patch (waiting for dpkg lock)" while an `os_update`'s post-patch reboot waits. This needs a server-side state after the terminal command (OQ-003).
- **Candidate, AGENT-PRIV restart (only if AGENT-PRIV is merged with a restart):** the restart is a command with its own agent-restart window whose events carry its command reference, attributed by Section E (BR-005).
- EXPECT-003 to EXPECT-006 and EXPECT-008 keep their IDs and scope.
