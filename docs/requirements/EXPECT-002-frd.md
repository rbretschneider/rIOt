# Formal Requirements Document

- **Story ID:** EXPECT-002
- **Title:** Agent self-update as expected activity: an agent's own scheduled auto-update does not raise "device offline", the fleet and device views show "Updating agent 2.78.0 → 2.80.0 by agent auto-update", and an update that fails or does not come back still alerts, naming the update and its versions
- **Author:** Business Developer Agent
- **Date:** 2026-10-26
- **Revision:** 1
- **Status:** DRAFT. Ryan was not available for an interview. Every clarification is settled by a default set on Ryan's behalf (Section 10 and Section 11.1). Ryan reviews them in the PR. Section 11.2 lists the decisions that need Ryan's call, each with a recommendation; the FRD is written to the recommendation.
- **Epic:** EXPECT (expected activity). Second story. Parent: `docs/requirements/EXPECT-001-frd.md` (EXPECT-001), whose Section 13 item 2 this story narrows to its server-side part (Section 13 of this FRD).

---

## 1. Executive Summary

Every rIOt agent updates itself on its own schedule when a new release is published. On 2026-10-26, after v2.80.0 was released, all four agents (hawaii, vermont, PiVPN, PiCUPS) did exactly that between 17:51 and 17:56 UTC, and each one dropped offline for under a minute while it restarted. Ryan's phone got four "device offline" pushes for something that was working as designed, and that will repeat on every release. EXPECT-001 already stops these alerts when Ryan starts an agent update from the fleet page, because that update is a rIOt command. A self-update is not a command, so nothing covers it. This story makes the server recognise an agent's own self-update from the message the agent already sends ("Agent update started: 2.78.0 → 2.80.0"), record it as a **self-update record** with its from and to versions, and open an EXPECT-001 agent-restart window for that device, bounded at 10 minutes. Inside that window, "device offline" is stored as an info event that names the update, and it is not notified. The fleet and device views show "Updating agent 2.78.0 → 2.80.0 by agent auto-update". The window ends when the agent reports the update completed, failed, or comes back on a version. If the device is still offline after 10 minutes, Ryan gets exactly one alert naming the update and both versions. A failed update with the device still online, or one that comes back on the old version, is recorded as a warning event naming both versions. This story changes the server, plus one dashboard label. It needs no agent change, no agent upgrade and no `install.sh` re-run, and it works with every agent already deployed.

## 2. Background & Context

### 2.1 Ryan's problem

From the orchestrating session (2026-10-26): every release produces one "device offline" push per device, because the agents update themselves and restart. Ryan already knows a release went out. These pushes are noise of the same kind EXPECT-001 removed for actions Ryan or a schedule starts (EXPECT-001 Section 2.1, BR-001).

### 2.2 Evidence (2026-10-26, v2.78.0 → v2.80.0)

For each of hawaii, vermont, PiVPN and PiCUPS, between 17:51 and 17:56 UTC:

| Step | What the server received | What Ryan got |
|------|--------------------------|---------------|
| 1 | `agent_update_available` "Agent update available: 2.78.0 → 2.80.0" | Nothing (info event) |
| 2 | `agent_update_started` "Agent update started: 2.78.0 → 2.80.0", with no command ID | Nothing (info event) |
| 3 | About 1 minute later, heartbeats stop; the device is marked offline (`device_offline`, warning) | **A push to his phone via ntfy** |
| 4 | 30 to 60 s later, the agent registers again with version 2.80.0 (`device_online`) | Nothing |
| 5 | `agent_update_completed` "signed installation completion verified against running executable", from the new agent process | Nothing |

Four devices, four pushes, one release.

### 2.3 What exists today (verified in code at commit c55e4bd, v2.80.0 plus PR #39)

- **The agent's self-update** (`internal/agent/updater.go`, scheduled update check, `auto_update` on by default): reports `agent_update_available` "Agent update available: <current> → <latest>", takes the host lock, reports `agent_update_started` "Agent update started: <current> → <latest>" (type, severity and message only; no command ID), then applies the update.
  - Signed path (all current installs): the root helper installs the new binary and restarts the agent. The **new** agent process reports the outcome, without a command ID: `agent_update_completed` "signed installation completion verified against running executable", or `agent_update_failed` with one of "signed agent update error: ...", "signed agent update deferred: ...", "signed update helper stopped without verified completion", or "installed digest does not match the running executable; completion remains unconfirmed" (severity warning). **So the completion normally arrives after the device has already registered again**, unlike an `agent_update` command, whose result arrives before the restart.
  - An error before the apply step is reported by the old process as `agent_update_failed` "Agent update failed: <error>". The device stays online.
  - Update events are sent once, with no retry. The update-check interval limits how often a self-update can be attempted.
- **Message format.** "Agent update started: %s → %s" has existed since 2026-03-06 (commit 41a20bc), so every deployed agent sends it.
- **The command path sends the same event.** `handleTriggerUpdate` (`internal/agent/commands.go`, the `agent_update` command, single or fleet bulk) sends the same `agent_update_started` event, also without a command ID. The server cannot tell the two apart from the event alone.
- **The server stores agent events as received.** `ReceiveAgentEvent` (`internal/server/handlers/handlers.go`, device-key authenticated) validates the type, stores the event with the agent's severity and message, and broadcasts it. Nothing links it to any suppression.
- **Registration.** `RegisterDevice` stores the agent version the agent reports and tells the EXPECT-001 Tracker about the registration on every re-registration.
- **EXPECT-001** (`internal/server/expect`, `docs/architecture/EXPECT-001-add.md`): windows are derived from command records (AD-001). The `agent_update` policy covers `device_offline` only, settles after 1 min online, has a 10 min return bound, and needs an observed return after the command finished before it can close early (AD-003). The hard bound is dispatch + 15 min execution deadline + 10 min. Every window ends through one routine that runs the escalation backstop (AD-009). Escalations go through the existing rules, exactly once, with a ceiling of 5 pushes per device per hour (AD-010). Labels are server-derived (AD-014). Messages and the actor phrase follow AD-015.

### 2.4 Gaps this story closes

| # | Gap | Effect |
|---|-----|--------|
| S1 | A self-update has no command record, so no EXPECT-001 window exists for it (EXPECT-001 flow F15). | Every self-update that restarts the agent for more than 45 s raises "device offline" and pushes. |
| S2 | The fleet and device views know nothing about a self-update. | The device just "drops off dead", the same complaint EXPECT-001 fixed for fleet updates (EXPECT-001 G11). |
| S3 | A self-update that fails, or leaves the agent down, is not linked to the update. | The offline alert, or the agent's failed event, does not say which update, from which version to which. |

### 2.5 Flows

| # | Flow (EXPECT-001 Section 2.4 numbering) | Covered by |
|---|------|------------|
| F14 | Fleet "Update agents" and single `agent_update` command, including the signed-helper restart | EXPECT-001, unchanged by this story |
| F15 | Agent self-initiated auto-update on its own schedule, including the signed-helper restart | **This story** |
| F16 | Agent restart through AGENT-PRIV one-click apply / `riot-agent-apply` | Follow-on EXPECT-007 (Section 13) |

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Ryan (operator) | Receives notifications, reads the fleet, device, events and command history views. Does not start self-updates. | Existing authenticated dashboard API. Nothing changes. |
| rIOt Agent (any deployed version) | Updates itself on its own schedule and sends `agent_update_available`, `agent_update_started`, `agent_update_completed` and `agent_update_failed` events, heartbeats and registrations. | Unchanged. Device-key authenticated. No agent change in this story (NFR-008). Its event text is untrusted input (NFR-010). |
| Agent auto-update (the agent's own schedule) | The originator of a self-update, as shown to Ryan. Actor phrase: "by agent auto-update". | None. A label, not a principal. |
| rIOt Server | Receives agent events and registrations, recognises self-updates, keeps self-update records and their windows, decides whether an offline transition is expected, escalates, serves the dashboard. | Unchanged. Gains the rules of this story. |
| Notification channels | The existing dispatcher and channels. | Unchanged. |

## 4. Functional Requirements

### Definitions

- **Start message:** the message of an `agent_update_started` event, in the exact form "Agent update started: <from> → <to>" (the arrow is U+2192), validated by Section 7.2.
- **Self-update record ("record"):** the server's durable record of one agent self-update on one device: device, from version, to version, start time (the server's receive time of the start event), originator "agent auto-update", outcome, and the time the outcome was set.
- **Self-update window ("window"):** the EXPECT-001 expected-activity window that belongs to a record (EXPECT-001 Section 4 definitions), with agent-restart scope (EXPECT-001 FR-010).
- **Bound:** 10 minutes after the record's start time (EXPECT-001 DEC-2, agent restart).
- **Attributed:** linked to a record or to a command of the same device.
- **Version match:** two versions match when they are equal after removing one leading "v" from each.
- **Action label:** "Agent update <from> → <to>", for example "Agent update 2.78.0 → 2.80.0".
- **Actor phrase:** "by agent auto-update".

### A. Recognising a self-update

- **FR-001:** When the server receives an `agent_update_started` event from a device, it must create a record for that device when all of the following hold:
  - (a) the message is a valid start message (FR-002);
  - (b) the start is not attributable to an `agent_update` command (FR-004);
  - (c) the device has no open record (FR-006);
  - (d) the device is under the rate cap (FR-007).
- **FR-002:** A start message is valid only when, after removing leading and trailing whitespace, it is at most 256 bytes of valid UTF-8 with no control characters, has exactly the form "Agent update started: <from> → <to>", both versions pass the Section 7.2 rules, and <from> and <to> do not match each other.
- **FR-003:** When the start message is not valid, the server must not create a record and must not open a window. The event must be stored and broadcast exactly as today, and all alerting for the device must behave exactly as today.
- **FR-004:** A start must be attributed to an `agent_update` command, and must not create a record, when the same device has an `agent_update` command that has been dispatched and is not yet terminal, or that reached a terminal state at most 10 minutes before the start was received. The start event must be linked to that command. A queued `agent_update` command that has not been dispatched must not count.
- **FR-005:** The `agent_update_started` event must be stored with the type, severity and message the agent sent, as today. When it creates a record, or is attributed to one, it must be linked to that record.
- **FR-006:** A device must have at most one open record. A valid start received while the device has an open record must not create a record and must not extend or reopen the window. It must be stored as today and linked to the open record.
- **FR-007:** The server must create at most 6 records per device in any rolling 24 hours. A valid start beyond the cap must be treated as in FR-003. The first start refused by the cap in a 24-hour period must also produce one `warning` event for the device, with no notification: "Agent self-update tracking paused on <host>: more than 6 agent updates started in 24h; offline alerts during agent updates are not suppressed". At most one such event per device per rolling 24 hours.
- **FR-008:** A record must never be sent to an agent, claimed by an agent, queued, cancelled, retried or acted on from the dashboard. It is a record of something the agent did on its own.

### B. The window

- **FR-009:** The window must open when the record is created, at the start time.
- **FR-010:** The window must cover `device_offline` for the record's device only. It must not cover `container_died`, `container_oom` or any other rule. A container that dies during a self-update must alert exactly as without a window (EXPECT-001 FR-010).
- **FR-011:** The window must close at the earliest of:
  - (a) an `agent_update_completed` event attributed to the record (FR-014);
  - (b) an `agent_update_failed` event attributed to the record (FR-014);
  - (c) a registration of the device reporting a version that matches <to>;
  - (d) a registration of the device reporting any other version (FR-019);
  - (e) the bound.
- **FR-012:** No window may stay open after its bound, even if no event, heartbeat or registration ever arrives again from the device. There is no settle period and no return phase after the bound.
- **FR-013:** When the window closes, normal alerting for the device must resume at once (EXPECT-001 FR-006). A later offline transition alerts exactly as with no window.
- **FR-014:** An `agent_update_completed` or `agent_update_failed` event without a command ID must be attributed to the device's most recent record when that record started at most 10 minutes before the event was received, whether or not its window is still open. Otherwise it must be stored as today with no link. These events must never create a record. Their stored type, severity and message must not change.

### C. Outcome and alerting

- **FR-015:** A record's outcome must be set exactly once, to one of `succeeded`, `failed` or `not confirmed`, by the first of these to occur:
  - `agent_update_completed` attributed to it: `succeeded`;
  - `agent_update_failed` attributed to it: `failed`;
  - a registration reporting a version other than <to> while the window is open: `failed` (FR-019);
  - the bound: `succeeded` if a registration reporting <to> was received inside the window, otherwise `not confirmed`.

  A registration reporting <to> closes the window (FR-011 c) but leaves the outcome open until a completed or failed event arrives or the bound is reached, so that an older agent that never sends `agent_update_completed` still ends `succeeded`, and a signed install whose digest does not match still ends `failed`.
- **FR-016:** Every offline transition covered by the window must be stored as a `device_offline` event with severity `info`, linked to the record, and must never be notified. Its message must name the device, the action label, the actor phrase and the bound time, for example "Device hawaii went offline during Agent update 2.78.0 → 2.80.0 by agent auto-update (expected, back by 18:01)".
- **FR-017:** When a device comes online after an offline period that began inside the window, the `device_online` event must name the action label and the actor phrase and say how long the device was offline, for example "Device hawaii came online after Agent update 2.78.0 → 2.80.0 by agent auto-update (offline 42s)" (EXPECT-001 FR-044).
- **FR-018:** When the outcome is set to `failed` because of an `agent_update_failed` event, the server must store exactly one `warning` event for the record, linked to it, naming the device, the action label, the actor phrase and the agent's reason, with no notification (DEC-1). Example: "Agent update 2.78.0 → 2.80.0 by agent auto-update failed on vermont: signed agent update deferred: ...". The reason is agent text and follows NFR-010.
- **FR-019:** When the device registers during an open window reporting a version that does not match <to>, the server must set the outcome to `failed` and store exactly one `warning` event linked to the record, with no notification (DEC-2). The message must name the device, the reported version, the action label and the actor phrase, for example "PiVPN came back on agent 2.78.0 after Agent update 2.78.0 → 2.80.0 by agent auto-update (expected 2.80.0)". A later offline transition alerts normally (FR-013).
- **FR-020:** When the bound is reached, the outcome is not yet set, and the device is offline, the server must raise exactly one escalation within 60 s of the bound, through the existing `device_offline` rule (EXPECT-001 FR-023), exempt from unrelated cooldowns, at most once ever per record including across server restarts (EXPECT-001 FR-024), and under the existing per-device push ceiling (EXPECT-001 ADD AD-010). Its message must name the device, the action label, the actor phrase and the record's short ID, for example "hawaii still offline 10m after Agent update 2.78.0 → 2.80.0 by agent auto-update (record 1a2b3c4d)". The outcome becomes `not confirmed`.
- **FR-021:** When the bound is reached, the outcome is not yet set, no registration reporting <to> was received, and the device is online, the server must store exactly one `warning` event linked to the record, with no notification: "Agent update 2.78.0 → 2.80.0 by agent auto-update on hawaii was not confirmed within 10m (agent reports 2.78.0)". The reported version is the device's current stored agent version.
- **FR-022:** A record must produce at most one outcome event (FR-018, FR-019 or FR-021) and at most one escalation (FR-020). An `agent_update_failed` or `agent_update_completed` event attributed to a record whose outcome is already set must be stored as today and linked, and must not change the outcome or produce another server event.
- **FR-023:** An escalated device must show the EXPECT-001 escalated state ("Not back 10m after Agent update 2.78.0 → 2.80.0", failure styling) until it next comes online, or for 24 hours, whichever is first (EXPECT-001 FR-035 last row, FR-037).

### D. Dashboard

- **FR-024:** While a device has an open window, the Fleet Overview and the device page header must show the activity label "Updating agent <from> → <to>", for example "Updating agent 2.78.0 → 2.80.0", whether the device is online or offline, with the actor phrase "by agent auto-update" and the start time next to it (EXPECT-001 FR-034, FR-036, FR-039). The label must clear when the window closes.
- **FR-025:** A device that is offline inside an open window must be counted under "Expected offline" and not in the offline count, and must still be visibly marked offline (EXPECT-001 FR-038, FR-043). An escalated device counts as offline.
- **FR-026:** The label must come from the server, appear in any browser and after a reload, and appear, change or clear within 5 seconds of the change that caused it, with no manual reload (EXPECT-001 FR-040, FR-041).
- **FR-027:** The record must appear in the device's command history, read-only, with the action label, the actor phrase "by agent auto-update", start time, outcome and the linked agent events (DEC-5). The device page label must link to it (EXPECT-001 FR-039). It must offer no cancel, retry or other action (FR-008).
- **FR-028:** When an `agent_update` command window and a self-update window would both apply to the same device (only possible when FR-004 does not apply, for example a command dispatched after the record was created), each must be evaluated on its own (EXPECT-001 FR-007) and the device must show one label, the one with the latest start.

### E. Unchanged behaviour

- **FR-029:** `agent_update` commands (single and fleet bulk) must keep their EXPECT-001 windows, labels, bounds and escalations exactly as today.
- **FR-030:** The `agent_update_available` event must be stored, broadcast and alerted exactly as today (DEC-3).
- **FR-031:** Container, host and shutdown windows, the powered-off state and the server restart grace from EXPECT-001 must not change.

### F. Documentation

- **FR-032:** The README expected-activity section must describe agent self-updates: how they are recognised, the 10-minute bound, what is suppressed (device offline only), how the window ends, the outcomes and their events (FR-018 to FR-021), the rate cap, the label, the fallback to today's behaviour for unrecognised messages, and the residuals in NFR-011.
- **FR-033:** The CHANGELOG must have an entry for this story that says no agent upgrade and no `install.sh` re-run are required, and that it works with already-deployed agents.

## 5. Non-Functional Requirements

- **NFR-001 [Safety, hard rule]:** No test, test helper, fixture, script or tool written for this story may run a real reboot, shutdown, poweroff or update (OS or agent) on any machine, and nothing may execute an installed `riot-agent` binary on any host. Tests must use injected clocks and fakes for the agent, the device, the events and the database. Agent restarts, registrations and update events are simulated by test doubles only.
- **NFR-002 [Bounded window, self-healing]:** Every self-update window closes by itself at most 10 minutes after its start, without operator action. Every record's outcome is set, and any FR-020 escalation is raised, within 10 minutes plus 60 s of its start (plus any EXPECT-001 server restart grace in force). After the bound, normal alerting resumes (FR-013).
- **NFR-003 [Bounded growth, database]:**
  - Records: at most 1 open per device (FR-006), at most 6 created per device per rolling 24 hours (FR-007), so at most 540 per device over the 90-day command retention; the realistic rate is one per release per device. Records are removed by the same retention as command records, and a record that is still being processed is protected from deletion only while pending, as in EXPECT-001 (at most 1 per device here).
  - Events: the agent's own `agent_update_*` events are stored exactly as today (no extra events; a link only). A suppressed offline event replaces today's warning event one for one. Per record: at most one outcome event (FR-018, FR-019, FR-021) and at most one escalation (FR-020). Per device: at most one rate-cap event per rolling 24 hours (FR-007).
  - Event retention (90 days) is unchanged.
- **NFR-004 [Bounded growth, memory]:** Server memory held for self-updates must be at most one open record per device, plus the rate-cap counter per device (at most 6 timestamps). Memory for a closed record must be released within 5 minutes of its outcome being set. No per-event state may accumulate.
- **NFR-005 [Bounded growth, logs and notifications]:** No log line above debug level per received start, completed or failed event, per refused start, or per suppressed offline transition. At most one info-level line per escalation. Repeated errors go through the existing log budget. Notifications: at most one escalation push per record, under the existing EXPECT-001 ceiling of 5 escalation pushes per device per hour; FR-007, FR-018, FR-019 and FR-021 never notify.
- **NFR-006 [Durability]:** All decisions must be derivable from durable records after a server restart. A restart at any point inside a window must not (a) turn a covered offline transition into an alert, (b) lose an escalation that became due during the downtime (it must fire within 2 minutes after start, EXPECT-001 restart grace included), (c) send an escalation or an outcome event twice, or (d) lose the record's attribution of a later completed or failed event.
- **NFR-007 [Performance]:** Handling an `agent_update_started`, `agent_update_completed` or `agent_update_failed` event must add no more than 100 ms at the 95th percentile to its processing at Ryan's scale (4 devices). Labels change within 5 s (FR-026). Escalations fire within 60 s of becoming due.
- **NFR-008 [Compatibility]:** No agent change, no new agent privilege, no agent upgrade and no `install.sh` re-run. The behaviour must hold for v2.78.0, v2.79.0 and v2.80.0 agents and for any older agent that sends the start message in the FR-002 form (every agent since 2026-03-06), on both the signed path (completion after registration) and any path that sends no `agent_update_completed` (FR-015, registration reporting <to>). An agent that sends any other message gets today's behaviour (FR-003).
- **NFR-009 [Fail open to alerting]:** If the server cannot parse a start message, cannot read or write records, cannot determine attribution, or cannot determine whether an offline transition is covered, the event and the device must behave exactly as today, and the offline transition must alert. Nothing may be silently dropped or downgraded (EXPECT-001 NFR-009).
- **NFR-010 [Security, untrusted input]:** All agent-supplied text is untrusted.
  - Versions are accepted only in the Section 7.2 form; anything else is treated as FR-003.
  - The failure reason quoted in FR-018 must be sanitized (printable, valid UTF-8, no control or format characters, the EXPECT-001 rules) and truncated to at most 200 bytes.
  - A device can open a record, and suppress offline alerts, only for itself. A start from one device must never affect another device's alerts or labels.
  - A device that abuses start messages can delay its own offline alert by at most 10 minutes per record and at most 60 minutes per rolling 24 hours (6 records), and every covered transition is still stored as an info event; an offline device at the bound still escalates (FR-020).
- **NFR-011 [Accepted residuals]:**
  - A real outage on a device that starts a self-update is hidden for up to 10 minutes, then escalates once (FR-020).
  - If the start event is lost (the agent sends it once, with no retry, for example while the server is restarting), no record exists and the offline transition alerts as today, unless the EXPECT-001 restart grace covers it.
  - An agent that restarts in place without its connection dropping for 45 s raises no offline event at all; the window then closes on the registration or the completion.
  - If an old agent process registers again during the update before the restart (Section 11.3, Q-1), the window closes early as `failed` (FR-019) and the restart's offline transition alerts normally. This fails toward alerting.
  - A self-update that fails while the device stays online produces a warning event and no push, as for commands today (EXPECT-001 DEC-3; EXPECT-004 adds pushes).
- **NFR-012 [Testability]:** Every FR must be verifiable by automated tests with an injected clock and test doubles, without real elapsed time.
- **NFR-013 [No regression]:** The EXPECT-001 acceptance tests must pass unchanged, except where this FRD explicitly extends a label vocabulary or a message.

## 6. Business Rules

- **BR-001:** A self-update the agent performs on its own schedule is a known, expected event. Its restart must not notify Ryan.
- **BR-002:** Nothing is dropped. Every suppressed offline transition is kept as an info event that names the update, both versions and "by agent auto-update".
- **BR-003:** A self-update suppresses only "device offline" for its own device. Container failures and every other rule alert normally.
- **BR-004:** Every self-update window is bounded at 10 minutes. Past the bound, normal alerting resumes on its own.
- **BR-005:** A self-update that leaves the device offline alerts exactly once and names the update and its versions.
- **BR-006:** When in doubt, alert (NFR-009). An unrecognised or untrusted message never suppresses anything.
- **BR-007:** An update Ryan or a schedule started through rIOt is one action with one record. A self-update record is never created for it.

## 7. Data Requirements

### 7.1 Entities

| Entity | New or existing | Used for |
|--------|-----------------|----------|
| Self-update record | New | One per accepted start: device, from version, to version, start time, originator ("agent auto-update"), whether a registration reporting <to> was seen inside the window, outcome (`succeeded`, `failed`, `not confirmed`, or not yet set), outcome time. Bounded by NFR-003. The architect decides whether it is a command record with a new origin (the orchestrator's recommended direction) or a separate record; either way it must satisfy FR-008 and reuse EXPECT-001's windows, escalation markers, labels and retention protection (no parallel window system). |
| Agent event | Existing | `agent_update_started`, `agent_update_completed`, `agent_update_failed` gain a link to the record (or to the command, FR-004). Stored content unchanged. |
| Event (server-generated) | Existing | Suppressed offline events, online events, the outcome events and the escalation, linked to the record. The rate-cap event. |
| Device | Existing | Online/offline status, last heartbeat, stored agent version (updated at registration, as today). |
| Command | Existing | `agent_update` commands, for attribution (FR-004). Unchanged. |
| Escalation marker | Existing (EXPECT-001) | At most one per record. |

### 7.2 Validation

- **Start message:** FR-002. At most 256 bytes after trimming, valid UTF-8, no control characters, exact form "Agent update started: <from> → <to>".
- **Version (<from> and <to>):** 1 to 32 bytes; an optional leading "v"; then three dot-separated groups of 1 to 5 digits each (for example 2.80.0); then optionally "-" or "+" followed by 1 to 16 letters, digits, dots or hyphens. Nothing else. A build reporting "dev" or any other form is not a version (FR-003).
- **Versions must differ:** <from> and <to> must not match (Section 4 definition).
- **Version shown to Ryan:** exactly as received in the start message (already restricted by the rules above).
- **Failure reason:** NFR-010, at most 200 bytes after sanitizing.
- **Attribution:** an event is linked only to a record or command of the same device.

### 7.3 Record and window states

| From | Event | To | Effect |
|------|-------|----|--------|
| (none) | Valid start, not attributed to a command, no open record, under cap | Open | Window covers `device_offline`. Label "Updating agent <from> → <to>". |
| (none) | Invalid start, attributed to a command, open record exists, or over cap | (none) | Today's behaviour (FR-003, FR-004, FR-006, FR-007). |
| Open | Device goes offline | Open | Info event, linked, no notification (FR-016). |
| Open | `agent_update_completed` attributed | Closed, `succeeded` | Label clears. |
| Open | `agent_update_failed` attributed | Closed, `failed` | One warning event naming the versions and reason, no push (FR-018). |
| Open | Registration reporting <to> | Closed, outcome pending | Online event names the update (FR-017) when it ended an offline period. Label clears. |
| Closed, outcome pending | `agent_update_completed` attributed | `succeeded` | None. |
| Closed, outcome pending | `agent_update_failed` attributed | `failed` | One warning event (FR-018). |
| Closed, outcome pending | Bound | `succeeded` | None. |
| Open | Registration reporting any other version | Closed, `failed` | One warning event "came back on agent <version>" (FR-019). |
| Open | Bound, device offline | Escalated, `not confirmed` | One escalation through the `device_offline` rule (FR-020). |
| Open | Bound, device online | Closed, `not confirmed` | One warning event "not confirmed within 10m" (FR-021). |
| Escalated | Device comes online, or 24 h | Closed | Failure label cleared (FR-023). |
| Any closed state with outcome set | Further completed or failed event within 10 min of start | Same | Stored as today and linked; nothing else (FR-022). |
| Any | Server restart | Same state | Rebuilt from durable records (NFR-006). |

## 8. Acceptance Criteria

### Recognition and suppression

**AC-001: The 2026-10-26 timeline produces no notification** [FR-001, FR-002, FR-005, FR-009, FR-011, FR-015, FR-016, FR-017, FR-024]
- Given hawaii on agent 2.78.0, online, an enabled `device_offline` rule with notify on, and no `agent_update` command for hawaii
- When the server receives, at 17:51:00, `agent_update_started` "Agent update started: 2.78.0 → 2.80.0" with no command ID; hawaii's heartbeats stop at 17:52:00 and hawaii is marked offline; at 17:53:30 hawaii registers reporting agent 2.80.0; at 17:53:35 `agent_update_completed` "signed installation completion verified against running executable" arrives
- Then one record exists for hawaii: from 2.78.0, to 2.80.0, start 17:51:00, outcome `succeeded`
- And the offline transition is stored as an `info` `device_offline` event linked to the record, reading "Device hawaii went offline during Agent update 2.78.0 → 2.80.0 by agent auto-update (expected, back by 18:01)"
- And the online event names "Agent update 2.78.0 → 2.80.0 by agent auto-update" and the offline duration
- And the started and completed events are stored with the agent's type, severity and message, linked to the record
- And no notification is sent on any channel
- And from 17:51:00 until the registration, the fleet and device page show "Updating agent 2.78.0 → 2.80.0", "by agent auto-update" and the start time, and the label is gone after the registration

**AC-002: A release across the whole fleet produces no notification** [FR-001, FR-016, BR-001]
- Given hawaii, vermont, PiVPN and PiCUPS on 2.78.0
- When each sends a valid start between 17:51 and 17:56, goes offline about 1 minute later, comes back on 2.80.0 30 to 60 s after that, and sends `agent_update_completed`
- Then four records exist, one per device, each `succeeded`, four info offline events exist, and zero notifications were sent

**AC-003: Container deaths during a self-update still alert** [FR-010, BR-003]
- Given an open window for hawaii
- When container `plex` on hawaii dies
- Then the `container_died` alert fires and notifies exactly as with no window

**AC-004: Offline after the window closed alerts** [FR-011, FR-013]
- Given a record for hawaii closed by `agent_update_completed` at 17:53:35
- When hawaii goes offline at 17:58
- Then the `device_offline` alert fires and notifies normally

**AC-005: Fast restart without an offline transition** [FR-011, FR-015, NFR-011]
- Given an open window for vermont and vermont's heartbeat gap during the restart is 20 s (never marked offline)
- When vermont registers reporting 2.80.0 and then sends `agent_update_completed`
- Then no `device_offline` event exists, the window is closed at the registration, and the outcome is `succeeded`

### Command attribution

**AC-006: A fleet agent update is not double-recorded** [FR-004, FR-029, BR-007]
- Given Ryan presses "Update agents" on the fleet page and an `agent_update` command for vermont is dispatched at 10:00:00
- When vermont sends `agent_update_started` "Agent update started: 2.78.0 → 2.80.0" (no command ID) at 10:00:02, before the command's result
- Then no record is created, the event is linked to the command, and vermont shows the EXPECT-001 command label "Updating agent 2.78.0 → 2.80.0" with "by ryan"
- And the same holds when the start arrives after the command has already reached `succeeded`, up to 10 minutes later

**AC-007: An old or queued command does not capture a self-update** [FR-004]
- Given an `agent_update` command for PiVPN that reached `succeeded` 11 minutes ago, and separately an `agent_update` command for PiVPN that is queued and not dispatched
- When PiVPN sends a valid start
- Then a record is created in both cases

### Not back, failure, wrong version

**AC-008: Device not back within 10 minutes escalates once, naming the update** [FR-012, FR-020, FR-023, NFR-002]
- Given a record for vermont started at 10:00:00 from 2.78.0 to 2.80.0, and vermont offline since 10:01:00
- When the clock reaches 10:10:00 with vermont still offline
- Then exactly one `device_offline` escalation is raised by 10:11:00 through the `device_offline` rule, reading "vermont still offline 10m after Agent update 2.78.0 → 2.80.0 by agent auto-update (record <id8>)", and it notifies
- And the record's outcome is `not confirmed`, the fleet shows "Not back 10m after Agent update 2.78.0 → 2.80.0" with failure styling, and vermont counts as offline
- And no second escalation is raised for that record afterwards, including after a server restart
- And an escalation is still raised when a `device_offline` rule cooldown was started 60 s earlier by an unrelated event

**AC-009: Failed self-update with the device online** [FR-015, FR-018, FR-022, DEC-1]
- Given a record for PiCUPS started at 10:00:00 from 2.78.0 to 2.80.0
- When PiCUPS, still online, sends `agent_update_failed` "Agent update failed: download: connection reset" at 10:00:40
- Then the agent's event is stored as today and linked, the record's outcome is `failed`, the window is closed, and exactly one `warning` event linked to the record reads "Agent update 2.78.0 → 2.80.0 by agent auto-update failed on PiCUPS: Agent update failed: download: connection reset"
- And no notification is sent
- And when PiCUPS goes offline at 10:02, the `device_offline` alert fires normally

**AC-010: Failure reported by the new process after it came back** [FR-011, FR-015, FR-018]
- Given a record for hawaii, hawaii went offline and registered reporting 2.80.0 (window closed, outcome pending)
- When `agent_update_failed` "installed digest does not match the running executable; completion remains unconfirmed" arrives 5 s later
- Then the outcome is `failed` and exactly one warning event naming 2.78.0, 2.80.0, "by agent auto-update" and the reason is stored, with no notification

**AC-011: Reconnect on the new version without a completed event** [FR-011, FR-015, NFR-008]
- Given a record for PiVPN started at 10:00:00 from 2.78.0 to 2.80.0, and PiVPN offline from 10:01:00
- When PiVPN registers reporting "v2.80.0" at 10:01:40 and no `agent_update_completed` or `agent_update_failed` ever arrives
- Then the window closes at 10:01:40, no notification is sent, the online event names the update, and at 10:10:00 the outcome becomes `succeeded` with no further event

**AC-012: Reconnect on the old version** [FR-011, FR-015, FR-019, DEC-2]
- Given a record for PiVPN started at 10:00:00 from 2.78.0 to 2.80.0, and PiVPN offline from 10:01:00
- When PiVPN registers reporting 2.78.0 at 10:01:50
- Then the window closes, the outcome is `failed`, exactly one `warning` event reads "PiVPN came back on agent 2.78.0 after Agent update 2.78.0 → 2.80.0 by agent auto-update (expected 2.80.0)", and no notification is sent
- And a later `agent_update_completed` for the record is stored and linked without changing the outcome or creating another event
- And a later offline transition alerts normally

**AC-013: Not confirmed while online** [FR-015, FR-021]
- Given a record for vermont started at 10:00:00 from 2.78.0 to 2.80.0, vermont stays online, and nothing else arrives
- When the clock reaches 10:10:00
- Then the window is closed, the outcome is `not confirmed`, exactly one `warning` event reads "Agent update 2.78.0 → 2.80.0 by agent auto-update on vermont was not confirmed within 10m (agent reports 2.78.0)", and no notification is sent

### Durability

**AC-014: Server restart mid-window** [NFR-006, FR-016, FR-014]
- Given a record for hawaii started at 10:00:00, hawaii offline since 10:01:00 with the offline event stored as info, and the server restarts at 10:02:00 and is serving again at 10:02:30
- When hawaii registers on 2.80.0 at 10:03:00 and sends `agent_update_completed` at 10:03:05
- Then no `device_offline` notification exists for hawaii, the record is `succeeded`, and the completed event is linked to it

**AC-015: Bound passed during server downtime** [NFR-006, FR-020]
- Given a record for vermont started at 10:00:00, vermont offline since 10:01:00, and the server is down from 10:05:00 to 10:20:00
- When the server starts and runs for 3 minutes, and is then restarted again
- Then exactly one escalation exists for the record, raised no later than 2 minutes after the first start

### Bounds and untrusted input

**AC-016: Malformed start messages fall back to today** [FR-002, FR-003, NFR-009, NFR-010]
- Given, in turn, start messages "Agent update started: 2.78.0 -> 2.80.0" (ASCII arrow), "Agent update started: dev → 2.80.0", "Agent update started: 2.80.0 → 2.80.0", "Agent update started: 2.78.0 → 2.80.0\u0000x", "agent update started: 2.78.0 → 2.80.0", "Agent update started: 2.78.0 → " followed by a 300-byte version, and a 10 KB message
- When each is received from hawaii and hawaii then goes offline
- Then no record is created, each event is stored exactly as today, and the `device_offline` alert fires and notifies

**AC-017: Repeated starts are bounded** [FR-006, FR-007, NFR-003, NFR-005]
- Given hawaii sends 50 valid start messages within 1 minute
- Then exactly one record exists, the window is not extended past 10 minutes after the first start, all 50 events are stored as today, and no log line above debug was written for the repeats
- And given hawaii sends 10 valid starts over 20 hours, each after the previous record closed
- Then 6 records exist, the 7th to 10th starts create no record, exactly one rate-cap warning event exists for hawaii, offline transitions after the 7th to 10th starts alert normally, and no notification was sent for the rate-cap event

**AC-018: One device cannot affect another** [NFR-010]
- Given an open window for hawaii
- When vermont goes offline
- Then vermont's `device_offline` alert fires normally and vermont shows no update label

**AC-019: Failure reason is sanitized and bounded** [FR-018, NFR-010]
- Given an open record for hawaii
- When `agent_update_failed` arrives with a 2 KB message containing control characters, bidi override characters and invalid UTF-8
- Then the agent's event is stored as today, and the server's warning event contains only printable valid UTF-8 and its quoted reason is at most 200 bytes

**AC-020: Fail open when records cannot be read or written** [NFR-009]
- Given the record store cannot be read or written
- When hawaii sends a valid start and then goes offline
- Then the event is stored as today and the `device_offline` alert fires and notifies

**AC-021: Completed or failed without a start** [FR-014]
- Given hawaii has no record started in the last 10 minutes
- When `agent_update_completed` or `agent_update_failed` arrives with no command ID
- Then it is stored exactly as today, no record is created, and no server event is added

**AC-022: Bounded growth** [NFR-003, NFR-004, NFR-005]
- Given the AC-001 timeline, then the AC-008 timeline, then the AC-009 timeline
- Then AC-001 stored exactly 1 record, 1 info offline event and 1 online event besides the agent's own events; AC-008 stored 1 record and 1 escalation; AC-009 stored 1 record and 1 warning event; no log line above debug was written except one info line for the AC-008 escalation; and within 5 minutes of each outcome no server memory is held for that record

### Dashboard

**AC-023: Label, count and link** [FR-024, FR-025, FR-026, FR-027]
- Given an open window for vermont from 2.78.0 to 2.80.0, with vermont offline
- When the Fleet Overview and the vermont device page are open in a browser that did not exist when the window opened
- Then both show "Updating agent 2.78.0 → 2.80.0", "by agent auto-update" and the start time; vermont is counted under "Expected offline", not offline, and its status dot shows it is offline
- And the device page links to the record in the command history, which shows it read-only with no cancel or retry control
- And the label clears within 5 s of the window closing, with no reload

**AC-024: Record is never sent to an agent** [FR-008]
- Given an open record for hawaii
- When hawaii's agent polls or claims commands
- Then the record is never returned, claimed or dispatched

**AC-025: Two windows on one device** [FR-028]
- Given a record for hawaii opened at 10:00:00, and an `agent_update` command for hawaii dispatched at 10:00:30
- Then hawaii shows one label, the command's, and an offline transition at 10:01 is suppressed and linked to one of the two

### Compatibility, unchanged behaviour, safety, docs

**AC-026: Older agents** [NFR-008]
- Given a fake agent reporting version 2.40.0 that sends the start message "Agent update started: 2.40.0 → 2.80.0", restarts, registers on 2.80.0, and never sends `agent_update_completed`
- Then the offline transition is suppressed, the record ends `succeeded` at the bound, and no notification is sent
- And given a fake agent that sends a start message in any other wording
- Then behaviour is exactly today's

**AC-027: `agent_update_available` unchanged** [FR-030, DEC-3]
- Given hawaii sends `agent_update_available` "Agent update available: 2.78.0 → 2.80.0"
- Then it is stored, broadcast and alerted exactly as today, no record is created and no label appears

**AC-028: EXPECT-001 unchanged** [FR-029, FR-031, NFR-013]
- Given the EXPECT-001 acceptance tests
- When they run after this story
- Then they pass, including AC-024 to AC-027 of EXPECT-001 for `agent_update` commands

**AC-029: No real updates, power actions or agent execution in tests** [NFR-001, NFR-012]
- Given the tests, helpers and scripts added or changed by this story
- When they are inspected and run
- Then none invokes reboot, shutdown, poweroff, `systemctl` power verbs, a package manager, the agent updater against a real host, or an installed `riot-agent` binary; all agent behaviour is simulated by test doubles and all time by an injected clock

**AC-030: Documentation** [FR-032, FR-033]
- Given the README and CHANGELOG after this story
- When they are read
- Then the README describes self-update recognition, the 10-minute bound, offline-only suppression, how the window ends, the outcomes and their events, the rate cap, the label, the fallback and the NFR-011 residuals
- And the CHANGELOG entry says no agent upgrade and no `install.sh` re-run are required and that already-deployed agents are covered

## 9. Out of Scope

- Any agent change, including structured from and to versions or a correlation ID in `agent_update_*` events, and retrying update events (follow-on EXPECT-007).
- Agent reporting of a step while a reboot or shutdown waits for the dpkg lock (EXPECT-001 A-007; follow-on EXPECT-007).
- Agent restarts through AGENT-PRIV one-click apply or `riot-agent-apply` (flow F16; follow-on EXPECT-007).
- Agent restarts that are neither a command nor a self-update (a crash, a systemd restart, an operator restart over ssh).
- Notifications for a failed self-update while the device is online (EXPECT-004).
- Any change to `agent_update_available` (DEC-3), to the agent's update-check interval or `auto_update` setting, or to `agent_update` command bounds.
- Telemetry rules during a self-update (EXPECT-003).
- A dashboard control to dismiss, extend or cancel a self-update window.

## 10. Assumptions

- **A-001:** All defaults in this FRD were set by the Business Developer Agent on Ryan's behalf on 2026-10-26, from the orchestrating session's grounding (Clod). Ryan reviews them in the PR.
- **A-002:** Every deployed agent sends the start message in the FR-002 form, with U+2192 as the arrow and versions in the Section 7.2 form (verified: format unchanged since commit 41a20bc, 2026-03-06).
- **A-003:** The agent sends `agent_update_started` before the restart, while still online, so the server receives it before any offline transition (verified in `internal/agent/updater.go` ordering).
- **A-004:** A self-update restart takes well under 10 minutes (evidence: start to return about 1.5 to 2 minutes on all four devices, including Raspberry Pis). The 10-minute bound counts from the start, not from a result, because a self-update has no result before its restart.
- **A-005:** The agent reports its version at registration, and a newly started agent process always registers (verified: `RegisterDevice` stores the reported agent version).
- **A-006:** The 10-minute attribution period in FR-004 and FR-014 equals the EXPECT-001 agent-restart return bound. A self-update cannot legitimately follow a successful command update to the same version within 10 minutes, so attributing such a start to the command loses nothing.
- **A-007:** 6 self-updates per device per 24 hours is far above any legitimate rate (one per release), and low enough that abuse is bounded (NFR-010).
- **A-008:** "Version reported at registration matches <to>" is enough evidence of success for agents that never send `agent_update_completed` (FR-015).

## 11. Open Questions and Decisions

### 11.1 Clarifications settled by default (Ryan reviews in PR)

- **OQ-001 Trigger:** only `agent_update_started` with a valid message creates a record (FR-001, FR-002). `agent_update_available` never does (DEC-3).
- **OQ-002 Scope:** `device_offline` for the device only (FR-010), as for `agent_update` commands.
- **OQ-003 Bound:** 10 minutes from the start, no settle period after close (FR-011, FR-012, DEC-4).
- **OQ-004 Close conditions:** completed, failed, registration on any version, or bound, whichever is first; the outcome can be set after close (FR-011, FR-015).
- **OQ-005 Command attribution:** dispatched and non-terminal, or terminal within 10 minutes (FR-004).
- **OQ-006 Rate cap:** 6 per device per rolling 24 hours, one warning event when first exceeded (FR-007). The architect may change the number with a stated reason, provided NFR-010's 60-minute daily bound is restated.
- **OQ-007 Wording:** action label "Agent update <from> → <to>", actor phrase "by agent auto-update", activity label "Updating agent <from> → <to>" in every phase of the window, online or offline (FR-024).
- **OQ-008 Escalation:** through the existing `device_offline` rule, once per record, under the existing push ceiling (FR-020).

### 11.2 Open decisions for Ryan

- **DEC-1 Failed self-update while the device stays online.** Options: (a) a warning event naming the versions and the reason, no push; (b) a push through a rule. **Recommendation:** (a), consistent with EXPECT-001 DEC-3 for failed commands; EXPECT-004 adds failure pushes with per-action control, and self-updates should get that control there, not a one-off here (FR-018). The FRD is written to this.
- **DEC-2 Agent comes back on the old version.** Options: (a) treat it as failed: close the window, one warning event naming the reported and expected versions, no push, and escalate only if the device is offline at the bound; (b) keep the window open until the bound in case a completion follows. **Recommendation:** (a). It ends suppression early, which fails toward alerting, and the device is online, so there is nothing to page about. (FR-019.) The FRD is written to this.
- **DEC-3 Does `agent_update_available` need anything?** **Recommendation:** no change. It precedes the host lock and the start, often by a long time, and it does not mean a restart is coming (FR-030). The FRD is written to this.
- **DEC-4 Self-update bound is 10 minutes from the start.** A command-driven agent update gets up to 25 minutes (15 min execution deadline plus 10 min return bound). A self-update has no result before its restart, so the 10-minute agent-restart bound (EXPECT-001 DEC-2) is counted from the start. A real outage is hidden for at most 10 minutes. A self-update whose download is very slow could exceed it and then alert. **Recommendation:** keep 10 minutes from the start. The FRD is written to this.
- **DEC-5 Self-update records in the device command history.** Options: (a) show them read-only with "by agent auto-update", so the device page label has something to link to and Ryan can see each device's update history; (b) keep them hidden and show only events. **Recommendation:** (a) (FR-027). The FRD is written to this.

### 11.3 Questions for the architect (not for Ryan)

- **Q-1:** Does an agent process ever register again without restarting (for example after a dropped connection or a server-side 401 or 404), in particular between `agent_update_started` and the restart? If it can, FR-019 could mark an update as `failed` before the restart. The FRD accepts this residual because it fails toward alerting (NFR-011). If the code shows it happens in normal operation, the architect must state how FR-019 avoids it, for example by counting only a registration that follows an offline transition inside the window, and record the choice in the ADD.
- **Q-2:** On the `agent_update` command path with the signed helper, does the new process's completion event carry the command ID? If not, FR-014 must not attribute it to an older self-update record; verify that FR-004's 10-minute rule and FR-014's "most recent record started within 10 minutes" cannot both claim the same completion.
- **Q-3:** If the record is a command record with a new origin (recommended direction), confirm that every path that lists, claims, cancels, retries, budgets or expires commands honours FR-008 and the EXPECT-001 retention protection, and that the actor phrase mapping gains "by agent auto-update".

## 12. Dependencies

- **D-001:** EXPECT-001 (shipped v2.79.0): windows, Tracker, escalation markers, push ceiling, restart grace, activity labels, `device_offline` rule routing, retention protection for pending windows.
- **D-002:** CMD-LIFECYCLE-001 commands record and sweeper (for attribution, and for the record if the architect chooses a command record).
- **D-003:** AGENT-HARDEN-001 signed update path (the completion and failure messages listed in Section 2.3).
- **D-004:** Existing alert rules, notification dispatcher, event retention.
- **D-005:** Fleet Overview, Device Detail and command history views and their live update channel.
- **D-006:** Pipeline: architect (ADD), security-researcher, senior-dev, qa-engineer, technical-writer. No new third-party dependency.

## 13. Follow-on stories

This story is the server-side part of EXPECT-001 FRD Section 13 item 2. The agent-side parts of that item move to a new story, **EXPECT-007**. The numbering of EXPECT-001 FRD Section 13 is otherwise unchanged: EXPECT-003, EXPECT-004, EXPECT-005 and EXPECT-006 keep their IDs and scope.

- **EXPECT-002 (this story):** server-only recognition of agent self-updates from the existing start message, self-update records, 10-minute agent-restart windows, outcome events and escalation naming the versions, and the "Updating agent <from> → <to>" label "by agent auto-update". No agent change.
- **EXPECT-007, agent-side activity reporting (new; agent and server change):**
  - the agent reports a step while a reboot or shutdown waits for the dpkg lock, so the "waiting for dpkg lock" label is reliable (EXPECT-001 A-007);
  - an agent restart through AGENT-PRIV one-click apply or `riot-agent-apply` becomes an activity record with its own agent-restart window and the actor phrase of whoever pressed it (flow F16);
  - `agent_update_*` events carry from and to versions and a correlation ID in structured fields, so the server no longer depends on parsing the message (this story's FR-002 parsing stays as the fallback for older agents);
  - optional: update events are retried, so a lost start event (NFR-011) no longer causes an alert.
