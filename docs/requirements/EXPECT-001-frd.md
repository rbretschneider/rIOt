# Formal Requirements Document

- **Story ID:** EXPECT-001
- **Title:** Expected-activity windows: no alerts for the disruption Ryan or a schedule started, a clear in-progress state on the fleet and device views, and one alert naming the action when something is still down afterwards
- **Author:** Business Developer Agent
- **Date:** 2026-10-26
- **Revision:** 1
- **Status:** DRAFT. Ryan was not available for an interview. Every clarification is settled by a default set on Ryan's behalf (Section 10 and Section 11). Ryan reviews them in the PR. Section 11.2 lists the decisions that need Ryan's call, each with a recommendation; the FRD is written to the recommendation.
- **Epic:** EXPECT (expected activity). This is the first of six stories. Section 13 lists the follow-on stories.

---

## 1. Executive Summary

Ryan gets a "container died" or "device offline" alert almost every time he, or one of his rIOt schedules, updates a container, recreates a stack, reboots a host, patches a host or updates agents from the fleet page. He already knows these things are happening, so the alerts are noise, and the noise hides the alerts that matter. This story introduces an **expected-activity window**: a bounded period, tied to a rIOt command, during which the disruption that command is expected to cause is recorded but not alerted. Inside a window, the matching "container died" and "device offline" events are stored as info events linked to the command, and they name the action and who or what started it. They are never dropped and never notified. The fleet and device views show what is happening ("Rebooting", "Updating agent 2.77.0 → 2.78.0", "Powered off by ryan at 14:02") instead of "offline". When the window ends and the device or container is still down, or the action failed, Ryan gets exactly one alert that names the action it followed. Every window has a hard upper bound, so normal alerting always resumes. Windows survive a server restart. This story changes the server and the dashboard only: no agent change and no `install.sh` re-run.

## 2. Background & Context

### 2.1 Ryan's request (verbatim)

"I have alerts for containers that die. But 90% of the time that's because I just initiated an update. If it's a scheduled or pushed action just skip that, I know already. There's process flows like this that need more insight. If I update agents on the fleet page, it shouldn't just show them drop off dead. It should be clear about what's happening in all aspects."

### 2.2 What exists today (verified in code at commit 08cd071, v2.78.0)

- **Durable action record.** Every rIOt-initiated action is a row in the commands record (CMD-LIFECYCLE-001). The row holds the action, the actor (`actor_kind`, `actor_name`), the origin (`user`, `auto_update`, `auto_patch`) and the schedule (`policy_id`), the timestamps (created, dispatched, started, finished), the deadline, the lifecycle status (queued, dispatched, acknowledged, running, verifying, then succeeded, failed, timed_out, cancelled or expired), the reported step and progress, and the target keys (`host` for host actions; `docker:container:<name>` and `docker:compose:<working dir>` for Docker actions). Scheduled container and stack updates (`auto_update`), automated patch runs (`auto_patch`) and the fleet "Update agents" button (one `agent_update` command per device) all create command rows.
- **Bounds per action** (`internal/models/command_policy.go`): reboot and shutdown 20 min execution deadline (they may wait up to 10 min for the dpkg lock); os_update 40 min; docker_update 35 min; Docker stack actions 35 min; docker_bulk_update min(35 min × N + 5 min, 6 h); agent_update 15 min. A sweeper marks overdue commands `timed_out`. Queued commands expire after their queued time-to-live (power 15 min, os_patch 30 min, Docker and agent update 60 min).
- **Docker event attribution** (`internal/server/handlers/docker_attribution.go`): a Docker event is linked to a command when the agent names one, or when a non-terminal command (or one that finished at most 10 s earlier) on the same device holds the container's target key. The event stores the command ID. Nothing uses the link for alerting.
- **Reboot and patch results are sent before the host goes down.** The reboot and shutdown results say "initiated" before the host restarts or powers off. An os_update that is authorized to reboot reports its result before it starts the reboot (PATCH-FLOW-001 FR-023). An agent_update reports its result, then the agent restarts about 1 s later (`internal/agent/commands.go`, handleTriggerUpdate). So a command is normally already `succeeded` when the device drops off.

### 2.3 Why Ryan still gets false alerts (verified in `internal/server/events/generator.go` and `internal/server/server.go`)

| # | Gap | Effect |
|---|-----|--------|
| G1 | A container death is downgraded only while a per-device in-memory counter is above zero. Only the agent's `update_started`/`update_completed`/`update_failed` events for `docker_update` and `docker_bulk_update` feed it. | `docker_restart`, `docker_stop`, and stack redeploy/restart/up/down raise "container died" alerts. |
| G2 | The death and the `update_started` event travel separately. | A death that arrives before `update_started`, or after `update_completed`, alerts. |
| G3 | The counter lives only in server memory. | A server restart, including riot-server's own auto-update on hawaii, forgets every in-progress update. |
| G4 | The counter is per device, not per container. | Any unrelated container that dies on that device during any update is silently downgraded. A real failure is hidden. |
| G5 | The counter is pruned after 30 min. | A legitimate bulk update of up to 6 h alerts after 30 min. |
| G6 | "Device offline" is downgraded only for a reboot or os_update **created** in the last 2 minutes. | A reboot that waits for the dpkg lock (up to 10 min), an os_update that reboots after a patch of up to about 31 min, and a reboot queued while the agent was busy all alert. |
| G7 | Shutdown is not treated as disruptive. | Every remote shutdown raises a "device offline" alert. |
| G8 | agent_update is excluded on purpose, and the agent's own `agent_update_*` events never feed any suppression. | A fleet agent update that leaves an agent down shows the device as plain "offline", with no link to the update. |
| G9 | Host actions (reboot, shutdown, os_update) stop or restart Docker. | Every container on the host raises "container died". |
| G10 | Offline detection starts 30 s after server start, using heartbeats that are older than the server's own downtime. | After a riot-server restart, a device whose agent is slow to reconnect raises "device offline". |
| G11 | The fleet page shows only online/offline. The "Updating..." label belongs to the browser that pressed the button. | Agents "drop off dead" during a fleet update. A scheduled action is invisible. |
| G12 | A failed or timed-out command produces a warning event and no notification. | A real failure after a suppressed disruption can go unnoticed. EXPECT-001 closes this for the case "still down afterwards" (Section 4.D). The general case is EXPECT-004. |

### 2.4 Inventory of disruptive flows

| # | Flow | Started by | Disruption | Covered by |
|---|------|-----------|------------|------------|
| F1 | Manual container update (`docker_update`) | Ryan | Target container dies and is recreated | EXPECT-001 |
| F2 | Scheduled container or stack update (auto-update policy) | Schedule | Same as F1 | EXPECT-001 |
| F3 | Docker bulk update (`docker_bulk_update`) | Ryan | Each target dies and is recreated, up to 6 h | EXPECT-001 |
| F4 | Container restart (`docker_restart`) | Ryan | Target dies and starts | EXPECT-001 |
| F5 | Container stop (`docker_stop`) | Ryan | Target dies, stays down on purpose | EXPECT-001 |
| F6 | Stack redeploy, restart, up (`docker_stack_*`) | Ryan | Stack containers die and are recreated | EXPECT-001 |
| F7 | Stack down (`docker_stack_down`) | Ryan | Stack containers die, stay down on purpose | EXPECT-001 |
| F8 | Container start, stack pull, remove items, inventory refresh, check updates | Ryan | None expected; a death here is a real failure | EXPECT-001 (explicitly not suppressed) |
| F9 | Reboot, including the up-to-10-min dpkg-lock wait | Ryan | Device offline; all containers die | EXPECT-001 |
| F10 | Shutdown, including the dpkg-lock wait | Ryan | Device offline indefinitely, on purpose; all containers die | EXPECT-001 |
| F11 | Manual os_update without reboot | Ryan | Docker daemon or services restart; brief network loss | EXPECT-001 (offline, containers); EXPECT-003 (services, processes, NICs) |
| F12 | Manual os_update with "Reboot automatically if the patch requires it" | Ryan | F11 plus a reboot after the result | EXPECT-001; EXPECT-003 |
| F13 | Automated patch run (`auto_patch`), with or without reboot | Schedule | Same as F11/F12 | EXPECT-001; EXPECT-003 |
| F14 | Fleet "Update agents" (bulk) and single `agent_update` command, including the signed-update helper restart | Ryan | Agent restarts; device may briefly go offline | EXPECT-001 |
| F15 | Agent self-initiated auto-update on its own schedule | Agent | Agent restarts; no command record | EXPECT-002 |
| F16 | Agent restart through AGENT-PRIV one-click apply / `riot-agent-apply` | Ryan | Agent restarts | EXPECT-002 |
| F17 | riot-server update or restart (riot-server runs in Docker on hawaii) | Schedule or Ryan | All heartbeats pause; stale offline detection | EXPECT-001 (restart grace, durable windows) |
| F18 | Reboot or patch of hawaii, which hosts riot-server | Ryan or schedule | F9/F13 plus F17 | EXPECT-001 |
| F19 | Telemetry rules after a patch or boot: `service_state`, `process_missing`, `nic_down`, `usb_disconnected`, `reboot_required`, `log_errors`, container CPU/memory | Any host action | Transient failures during patch and boot settle | EXPECT-003 |
| F20 | A failed or timed-out command whose targets are healthy | Any | No notification today | EXPECT-004 |
| F21 | Per-container in-progress and "stopped on purpose" display in Docker pages | Any Docker action | Containers look dead or exited | EXPECT-005 |
| F22 | Work outside rIOt: ssh reboot, hardware work, unattended-upgrades | Ryan or OS | Offline, deaths | Not expected activity; EXPECT-006 candidate |

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Ryan (operator) | Starts actions from the dashboard (Docker, stack, power, patch, fleet agent update), receives notifications, reads the fleet, device and events views. | Existing authenticated dashboard API. Nothing changes. |
| Schedule | A rIOt auto-update policy (`origin auto_update`, with a policy ID) or the automated patch orchestration (`origin auto_patch`). Starts commands without Ryan. | Unchanged. Its commands are treated exactly like Ryan's (BR-009). |
| rIOt Server | Creates and tracks commands, receives heartbeats and Docker events, decides whether an event is expected, records events, sends notifications, serves the dashboard. | Unchanged. Gains the expected-activity rules of this story. |
| rIOt Agent (any released version) | Executes commands, sends heartbeats, telemetry, Docker events and command results. | Unchanged. No agent change in this story (NFR-008). |
| Notification channels | The existing notification dispatcher and its configured channels. | Unchanged. |

## 4. Functional Requirements

### Definitions

- **In-scope action:** an action listed in Table 4.1.
- **Expected-activity window ("window"):** the period, tied to one command of an in-scope action, during which the disruption listed for that action in Table 4.1 is expected.
- **Terminal success:** the command reached `succeeded`. **Terminal non-success:** the command reached `failed`, `timed_out`, `cancelled` or `expired`.
- **Return phase:** for a host action, the period after terminal success during which the device is expected to go down and come back.
- **Settle period:** a short period after the end of an action, or after the device is back online, during which follow-on disruption is still expected.
- **Escalation:** the single alert that fires when a window ends and its target is still down, or the action failed with its target down (Section D).
- **Actor phrase:** the text that says who or what started a command (FR-018).

### Table 4.1: In-scope actions and their windows

| Action(s) | Window class | Suppresses (only these) | Settle / return bound after terminal success | Still-down escalation |
|-----------|--------------|-------------------------|---------------------------------------------|----------------------|
| `reboot` | Host | `device_offline` for the device; `container_died` for every container on the device | Closes when the device has been back online for 3 min, and no later than 15 min after terminal success (return bound) | Device not back online at the return bound; container still not running at close |
| `os_update` (any mode, with or without reboot authorization, manual or `auto_patch`) | Host | Same as `reboot` | Same as `reboot` | Same as `reboot` |
| `shutdown` | Host, intentional off | `device_offline`; `container_died` for every container on the device | After terminal success, the device enters the **powered-off** state (FR-028) when it goes offline, with no time bound (DEC-1). If it never goes offline, the window closes 15 min after terminal success. | None after success. Non-success: FR-020 |
| `agent_update` (single command or fleet bulk) | Agent restart | `device_offline` for the device only | Closes when the device has been back online for 1 min, and no later than 10 min after terminal success | Device not back online at the return bound |
| `docker_update`, `docker_bulk_update`, `docker_restart`, `docker_stack_redeploy`, `docker_stack_restart`, `docker_stack_up` | Container | `container_died` for the command's target containers only | Closes 2 min after the terminal state (any outcome) | Target container still not running at close |
| `docker_stop`, `docker_stack_down` | Container, intentional down | `container_died` for the command's target containers only | Closes 2 min after the terminal state (any outcome) | None (the container is meant to be down) |

Actions not in the table open no window: `docker_start`, `docker_stack_pull`, `docker_remove_items`, `docker_inventory_refresh`, `docker_check_updates`, `enable_auto_updates`, `fetch_logs`, `run_device_probe`, `agent_uninstall` and any other action. The architect may adjust the 3 min, 1 min and 2 min settle periods and the 15 min and 10 min return bounds with a stated reason (DEC-2), provided NFR-002 still holds.

### A. The expected-activity window

- **FR-001:** For every command of an in-scope action, the system must maintain a window for that command, whatever its origin (`user`, `auto_update`, `auto_patch`).
- **FR-002:** A window must open when its command is dispatched to the agent. While the command is queued and not yet dispatched, the window must not suppress anything. The dashboard shows the queued state (FR-035).
- **FR-003:** On terminal success, the window must stay open for the settle period or return bound in Table 4.1, and close at the earlier of: (a) the settle condition being met, or (b) the bound being reached.
- **FR-004:** On terminal non-success, the window must stop suppressing `device_offline` at once (FR-020 applies). For container-class windows and for `container_died` under host-class windows, the window must close 2 min after the terminal state, so that a rollback or a Docker restart can be observed before the FR-022 check.
- **FR-005:** No window may stay open longer than its command's execution deadline plus its settle period or return bound (whichever applies), counted from dispatch. This must hold even if no heartbeat, event, telemetry or result ever arrives again from the device (NFR-002 states the numbers). The powered-off state (FR-028) is the only exception, and it is bounded to one per device.
- **FR-006:** When a window closes or reaches its bound, normal alerting for its targets must resume at once: a later death or offline transition alerts exactly as it does when no window exists.
- **FR-007:** Several windows may be open on the same device at the same time. Each must be evaluated on its own. An occurrence is suppressed when at least one open window covers it, and the event must be linked to one covering command, preferring a command that holds the event's exact container target over a host-class command.

### B. Scope of suppression

- **FR-008:** A container-class window must suppress `container_died` only for containers that the command targets: a container whose name matches one of the command's container target keys, or a container that belongs to a compose project whose working-directory key the command holds.
- **FR-009:** A host-class window (`reboot`, `shutdown`, `os_update`) must suppress `device_offline` for its device and `container_died` for every container on its device.
- **FR-010:** An `agent_update` window must suppress `device_offline` for its device and nothing else. Container deaths during an agent update must alert normally.
- **FR-011:** `container_oom` must never be suppressed. When an OOM event occurs while a window covers that container, the alert must still be raised and its message must name the in-progress action (DEC-4).
- **FR-012:** During a container-class window, the death of a container that the command does not target must alert exactly as it would with no window. (This replaces today's per-device downgrade, G4.)
- **FR-013:** Suppression must not depend on the agent sending `update_started`, `update_completed` or `update_failed`. A death that arrives before `update_started` or after `update_completed`, inside the window, must be suppressed.
- **FR-014:** A container death must not be downgraded only because another container on the same device is being updated. Today's per-device in-progress counter must no longer decide whether a death alerts.
- **FR-015:** The existing per-rule container include/exclude behavior must not change. A container excluded from the `container_died` rule stays excluded, inside or outside a window.
- **FR-016:** Alert rules other than `device_offline`, `container_died` and `container_oom` must behave exactly as today (telemetry rules are EXPECT-003).

### C. Recording suppressed occurrences

- **FR-017:** Every suppressed occurrence must be stored as an event of its usual type (`device_offline` or `container_died`) with severity `info`, linked to the covering command's ID. Suppressed occurrences must never be dropped.
- **FR-018:** The message of a suppressed event must name: the action (its human-readable label, FR-035), the target (container or stack name, for container events), and the actor phrase. The actor phrase must be:
  - `by <actor name>` for `origin user` (for example "by ryan");
  - `by auto-update policy for <policy target>` for `origin auto_update`, or `by auto-update policy #<id>` when the target cannot be resolved;
  - `by the auto-patch schedule` for `origin auto_patch`.

  Example: "Container plex died during Docker update by auto-update policy for plex (expected)". Example: "Device vermont went offline during Reboot by ryan (expected, back by 14:32)".
- **FR-019:** A suppressed occurrence must never send a notification on any channel.
- **FR-044:** When a device comes back online while a window covers it, or within the settle period after it, the `device_online` event must name the action and the actor phrase and say how long the device was offline, for example "Device vermont came online after Reboot by ryan (offline 1m42s)". Today's 2-minute "created" rule (G6) must no longer decide this text.

### D. Real failures still alert, and name the action

- **FR-020:** When a host-class or `agent_update` command reaches terminal non-success while its device is offline, the system must raise one escalation within 60 s. Its message must name the device, the action, the actor phrase, the outcome and the command ID, for example "hawaii is offline: Reboot by ryan timed out (command 1a2b3c4d)".
- **FR-021:** When a host-class (except `shutdown`) or `agent_update` command reached terminal success and the device is not back online when the return bound is reached, the system must raise one escalation within 60 s of the bound. Example: "vermont still offline 15m after Reboot by ryan (command 1a2b3c4d)".
- **FR-022:** When a window closes, for every container whose death that window suppressed, the system must check whether the container has been observed running after its last suppressed death (a start event, or a container state report received after the death). If it has not, the system must raise one escalation for that container, naming the container, the action, the actor phrase, the command's outcome and the command ID. Example: "Container sonarr still not running 2m after Docker stack redeploy by ryan (succeeded, command 9f8e7d6c)". This check does not apply to `docker_stop`, `docker_stack_down` or `shutdown` windows.
- **FR-023:** An escalation must go through the existing matching alert rule for its event type (`device_offline` for devices, `container_died` for containers), including the rule's container scope, severity and notify setting. When no enabled rule matches, the escalation must be stored as a `warning` event without a notification, as today's fallback does.
- **FR-024:** Each (command, device) pair and each (command, container) pair must produce at most one escalation, ever, including across server restarts. An escalation must not be blocked by a rule cooldown that was started by a different event.
- **FR-025:** One command must produce at most 20 container escalations. Beyond 20, the system must raise one summary escalation that gives the number of further containers still not running and names the action (NFR-003).
- **FR-026:** An escalation must be stored as an event of its type with the rule's severity (or `warning` without a rule) and must be linked to the command ID.
- **FR-027:** After a window closes, a later `device_offline` or `container_died` for its targets must alert normally (FR-006). It is not an escalation and does not count toward FR-024.

### E. Shutdown: powered off on purpose

- **FR-028:** When a `shutdown` command reached terminal success and its device then goes offline, the device must enter the powered-off state. The offline transition must be recorded as an `info` event linked to the command (FR-017), with the message "Device <host> powered off by <actor phrase> at <time>".
- **FR-029:** While a device is powered off, the system must not raise any `device_offline` alert or escalation for it.
- **FR-030:** The powered-off state must end when the device next reports online. The `device_online` event must say "came online after Shutdown by <actor phrase> (powered off <duration>)".
- **FR-031:** A `shutdown` command that reaches terminal non-success must not create the powered-off state. FR-020 applies when the device is offline at that moment.
- **FR-032:** The powered-off state must survive a server restart and must be held at most once per device. A newer powered-off state for the same device replaces the older one.

### F. riot-server restart grace

- **FR-033:** For 2 minutes after the server starts, the server must not move any device from online to offline. After the grace period, the normal 45 s heartbeat threshold applies, measured from each device's last heartbeat. A device that was already offline before the restart stays offline and raises no new event. (This covers riot-server's own update or restart, and a reboot of hawaii, the host that runs riot-server.)

### G. Dashboard

- **FR-034:** The Fleet Overview must show, for each device with an open window, a queued host or agent command, or the powered-off state, an activity label in place of the plain online/offline status text. A device without any of these keeps today's display.
- **FR-035:** The activity label must use this vocabulary:

  | Situation | Label |
  |-----------|-------|
  | Host or agent command queued, not dispatched | "<Action> queued" (for example "Reboot queued") |
  | `reboot` dispatched or running | "Rebooting"; "Rebooting (waiting for dpkg lock)" while the command's reported step or progress says it is waiting for the dpkg lock |
  | `reboot` succeeded, return phase | "Rebooting, back by <time>" |
  | `shutdown` dispatched or running | "Shutting down"; "Shutting down (waiting for dpkg lock)" as for reboot |
  | Powered-off state | "Powered off by <actor phrase> at <time>" |
  | `os_update` running | "Patching" (with the reported progress percentage when one is reported) |
  | `os_update` succeeded, return phase, device offline | "Rebooting after patch, back by <time>" |
  | `agent_update` dispatched or running | "Updating agent <from> → <to>", where <from> is the device's agent version when the command was created and <to> is the newest agent version the server knows; "Updating agent" when either is unknown |
  | `agent_update` succeeded, device offline | "Restarting agent, back by <time>" |
  | One container-class command open, no host-class window | "Docker: <action label> <target>" (for example "Docker: recreating stack media", "Docker: updating plex") |
  | Two or more container-class commands open, no host-class window | "Docker: <N> actions in progress" |
  | Escalated device (FR-020, FR-021) | Failure styling, "<Action> failed: offline" or "Not back <bound> after <Action>" |

  When a host-class or `agent_update` window and container-class windows are open together, the host-class label must be shown.
- **FR-036:** Next to the label, the fleet card or row and the device page must show the actor phrase and the time the action started.
- **FR-037:** The escalated state (failure styling and text, FR-035 last row) must stay visible until the device next reports online, or for 24 hours after the escalation, whichever comes first.
- **FR-038:** The Fleet Overview offline count must not include devices that are offline inside an open window or in the powered-off state. The Fleet Overview must show those devices in a separate count labelled "Expected offline". Escalated devices must be counted as offline.
- **FR-039:** The device detail page header must show the same label, actor phrase and start time as the fleet view, and a link to the command's existing detail or history entry.
- **FR-040:** The labels must come from the server's view of commands and device state, not from the browser that started the action. A label must appear in a second browser, after a page reload, and for actions started by a schedule.
- **FR-041:** An open dashboard must show a new, changed or cleared label within 5 seconds of the change that caused it, with no manual reload.
- **FR-042:** The existing patch badges (PatchBadge, RebootClassBadge) and the existing bulk "Update agents" button behavior must not change, apart from the device status text replaced under FR-034.
- **FR-043:** The activity labels must not hide that a device is offline: a device that is offline inside a window must still be visibly distinguishable from an online device (for example by its status dot), while its text shows the activity label.

### H. Documentation

- **FR-045:** The README must describe expected-activity windows: Table 4.1, the settle periods and bounds, what is suppressed, that suppressed events are kept as info events, what still alerts (OOM, non-target containers, escalations), the powered-off state, the server restart grace, and the residuals in NFR-011.
- **FR-046:** The CHANGELOG must have an entry for this story that says no agent upgrade or `install.sh` re-run is required.

## 5. Non-Functional Requirements

- **NFR-001 [Safety, hard rule]:** No test, test helper, fixture, script or tool written for this story may run a real reboot, shutdown or poweroff on any machine, and nothing may execute an installed `riot-agent` binary on any host. Tests must use fakes for the agent, the device and the clock. Reboot, shutdown and agent restarts are simulated by state changes in test doubles only.
- **NFR-002 [Bounded windows, self-healing]:** Every window closes by itself, without operator action, at the latest:
  - host-class (`reboot`, `os_update`): execution deadline + 15 min return bound. Worst case `os_update`: 40 + 15 = 55 min after dispatch; `reboot`: 20 + 15 = 35 min.
  - `shutdown` that never went offline: 20 + 15 = 35 min after dispatch.
  - `agent_update`: 15 + 10 = 25 min after dispatch.
  - container-class: execution deadline + 2 min. Worst case `docker_bulk_update`: 6 h + 2 min; single `docker_update` and stack actions: 37 min.

  A queued command that is never dispatched opens no window; its label ends at queue expiry (at most 60 min). The only unbounded-duration state is the powered-off state, at most one per device (FR-032). After a window's bound, normal alerting resumes (FR-006).
- **NFR-003 [Bounded growth, database]:**
  - Suppressed events replace the warning events that are stored today. They add no extra event per occurrence.
  - At most one escalation event per (command, device) and per (command, container), and at most 21 container escalation events per command (FR-025).
  - Any new persistent record this story needs must be bounded by one record per command (and removed no later than the command record itself), or by one record per device (the powered-off state).
  - Event retention (90 days) is unchanged.
- **NFR-004 [Bounded growth, memory]:** Server memory held for windows must be bounded by the number of open windows plus one entry per device. Memory for a window must be released within 5 minutes of its close. No per-event state may accumulate.
- **NFR-005 [Bounded growth, logs]:** No log line above debug level per suppressed occurrence. At most one info-level line per escalation. Repeated errors while evaluating windows must go through the existing log budget.
- **NFR-006 [Durability]:** All suppression and escalation decisions must be derivable from durable records after a server restart. A restart at any point must not (a) turn a suppressed occurrence into an alert, (b) lose an escalation that became due during the downtime (it must fire within 2 min after start, FR-033 window included), or (c) send an escalation twice (FR-024).
- **NFR-007 [Performance]:** At Ryan's scale (4 devices, up to 200 containers, up to 20 open commands), deciding whether a Docker event or an offline transition is expected must add no more than 100 ms at the 95th percentile to its processing. Escalations must fire within 60 s of becoming due. Window checks must run at least every 30 s.
- **NFR-008 [Compatibility]:** No agent change, no new agent privilege, no `install.sh` re-run. The behavior must hold with v2.78.0 agents and with older agents that do or do not send `update_started` events, attach command IDs to Docker events, or report steps.
- **NFR-009 [Fail open to alerting]:** If the server cannot determine whether an occurrence is covered by a window (for example, the command records cannot be read), the occurrence must alert as it would without windows. It must never be silently dropped or downgraded.
- **NFR-010 [Security, untrusted input]:** Container names, actor names and agent-supplied text that appear in event messages and labels must be sanitized (printable, valid UTF-8) and bounded (container and actor names at most 128 bytes each in messages). An agent-supplied command ID must not let one device's command suppress another device's events (existing attribution rule: same device only).
- **NFR-011 [Accepted residuals]:**
  - Inside a host-class window, a real device outage or a real container crash on that device is hidden until the window ends. Maximum delay before the escalation: the return bound (15 min) after terminal success, or immediately (within 60 s) after terminal non-success.
  - A container that dies inside a host-class window, comes back, and fails again after the window closes alerts normally. One that dies and comes back inside the window raises nothing (a recorded info event only).
  - A failed or timed-out command whose targets are all running again raises a warning event and no notification, as today (EXPECT-004 adds notifications).
  - Disruption from actions not started through rIOt (an ssh reboot, unattended-upgrades) is not expected activity and still alerts (EXPECT-006 candidate).
- **NFR-012 [Testability]:** Every FR must be verifiable by automated tests with an injected clock and test doubles for devices, agents and the database, without real elapsed time.

## 6. Business Rules

- **BR-001:** An action started by Ryan or by one of Ryan's rIOt schedules is known to Ryan. Its direct, expected disruption must not notify him.
- **BR-002:** Nothing is dropped. Every suppressed signal is kept as an info event that says which action caused it and who or what started it.
- **BR-003:** Suppression covers the action's targets only. Unrelated failures alert.
- **BR-004:** Every window is bounded. Past the bound, normal alerting resumes on its own.
- **BR-005:** A real failure after expected activity alerts once and names the action it followed.
- **BR-006:** When in doubt, alert (NFR-009).
- **BR-007:** A state that is down on purpose (stopped container, stack down, host powered off by a successful shutdown) is not a failure.
- **BR-008:** An out-of-memory kill is always a failure.
- **BR-009:** A user command and a scheduled command are treated the same. Only the actor phrase differs.

## 7. Data Requirements

### 7.1 Entities

| Entity | New or existing | Used for |
|--------|-----------------|----------|
| Command | Existing | The action record: action, actor, origin, policy, timestamps, deadline, status, step, progress, target keys. The window is derived from it. |
| Event | Existing (already has command ID and attribution fields) | Suppressed info events and escalation events, linked to the command. |
| Device | Existing | Online/offline status, last heartbeat, agent version. |
| Auto-update policy | Existing | Policy target for the actor phrase. |
| Escalation marker | New, if needed | Records that a (command, target) escalation was sent (FR-024). At most 1 per (command, device) and 21 per command for containers; removed no later than its command. |
| Powered-off state | New, if needed | One per device (FR-032): command ID, actor phrase, time. Cleared when the device comes online. |

### 7.2 Validation

- Actor phrase and container name in messages: sanitized and at most 128 bytes each (NFR-010).
- An event is linked only to a command of the same device.
- Only the actions in Table 4.1 open windows.

### 7.3 Window states

| From | Event | To | Effect |
|------|-------|----|--------|
| (none) | Command created | Queued | Label "<Action> queued" for host and agent actions. No suppression. |
| Queued | Command expires or is cancelled | Closed | Label cleared. No escalation. |
| Queued | Command dispatched | Open | Suppression per Table 4.1 begins. |
| Open | Terminal success, host-class except shutdown, or agent_update | Return phase | Suppression continues. Label "..., back by <time>". |
| Open | Terminal success, shutdown | Return phase (shutdown) | Suppression continues. |
| Return phase (shutdown) | Device goes offline | Powered off | Info event "powered off by ...". No offline alert. |
| Powered off | Device comes online | Closed | Info "came online after Shutdown ...". |
| Return phase (shutdown) | 15 min with the device still online | Closed | Nothing raised. |
| Return phase | Device online for the settle period | Closed | FR-022 container check. |
| Return phase | Return bound reached, device offline | Escalated | One escalation (FR-021). |
| Return phase | Return bound reached, device online | Closed | FR-022 container check. |
| Open | Terminal success, container-class | Settling | Suppression continues for 2 min. |
| Open | Terminal non-success, any class | Settling (containers only) | Device offline suppression ends; FR-020 escalation if offline. |
| Settling | 2 min elapsed | Closed | FR-022 container check (not for stop, stack down, shutdown). |
| Escalated | Device comes online, or 24 h | Closed | Failure label cleared (FR-037). |
| Any open state | Hard bound (NFR-002) | Closed | Normal alerting resumes. |
| Any | Server restart | Same state | Rebuilt from durable records (NFR-006). |

## 8. Acceptance Criteria

### Container windows

**AC-001: Container death during a Docker update is recorded, not alerted** [FR-001, FR-008, FR-017, FR-019]
- Given an enabled `container_died` rule with notify on, and a dispatched `docker_update` for container `plex` started by user `ryan`
- When `plex` dies before the command reaches a terminal state
- Then an `info` `container_died` event linked to the command is stored, naming "Docker update", `plex` and "by ryan"
- And no notification is sent

**AC-002: Scheduled update is treated the same** [FR-001, FR-018, BR-009]
- Given a dispatched `docker_update` with origin `auto_update` from the policy for `plex`
- When `plex` dies inside the window
- Then the info event message names "by auto-update policy for plex" and no notification is sent

**AC-003: Restart, stop and stack actions are covered** [FR-008]
- Given, in turn, a dispatched `docker_restart` of `plex`, `docker_stop` of `plex`, and `docker_stack_redeploy`, `docker_stack_restart`, `docker_stack_up` and `docker_stack_down` of compose project `media` (working directory `/srv/media`)
- When a targeted container dies inside each window
- Then each death is stored as an info event linked to its command, and no notification is sent

**AC-004: Bulk update is covered for its whole duration** [FR-005, NFR-002]
- Given a dispatched `docker_bulk_update` of 8 containers with an execution deadline of 4h45m
- When a targeted container dies 3 h after dispatch, with the command still running
- Then the death is suppressed and recorded as info

**AC-005: Non-target container still alerts** [FR-012, FR-014, BR-003]
- Given a dispatched `docker_update` of `plex` on hawaii
- When container `sonarr` (not targeted, not in plex's compose project) dies on hawaii inside the window
- Then the `container_died` alert fires and notifies exactly as with no window

**AC-006: Order of update events does not matter** [FR-013]
- Given a dispatched `docker_update` of `plex` from an agent that sends `update_started` after the death, and separately one that sends no `update_started` at all
- When `plex` dies inside the window
- Then in both cases the death is suppressed

**AC-007: Death after the settle period alerts** [FR-003, FR-006, FR-027]
- Given a `docker_update` of `plex` that reached `succeeded` at 10:00:00
- When `plex` dies at 10:01:50, and again in a separate run at 10:02:10
- Then the 10:01:50 death is suppressed, and the 10:02:10 death alerts normally

**AC-008: Actions outside the table open no window** [FR-001, FR-016]
- Given a dispatched `docker_start` of `plex`, and separately a dispatched `docker_stack_pull` of `media`
- When the targeted container dies inside the command's execution
- Then the `container_died` alert fires as today

**AC-009: OOM is never suppressed** [FR-011, BR-008]
- Given a dispatched `docker_update` of `plex`
- When `plex` is OOM-killed inside the window
- Then the `container_oom` alert fires and notifies, and its message names the in-progress Docker update

**AC-010: Container exclusions unchanged** [FR-015]
- Given `plex` excluded from the `container_died` rule
- When `plex` dies with and without a window open
- Then the behavior is identical to today's excluded-container behavior in both cases

### Host windows

**AC-011: Reboot after a 10-minute dpkg wait does not alert** [FR-002, FR-009, FR-017, FR-044]
- Given a `reboot` by `ryan` dispatched at 10:00, which waits 9m30s for the dpkg lock and reports success at 10:09:40
- When vermont goes offline at 10:10:20 and is back online at 10:12:00
- Then the offline transition is an info event linked to the reboot, naming "Reboot" and "by ryan", with no notification
- And the online event says "came online after Reboot by ryan (offline 1m40s)"

**AC-012: Containers that stop during a reboot do not alert** [FR-009]
- Given an open `reboot` window on hawaii
- When 12 containers on hawaii die
- Then 12 info events linked to the reboot are stored and no notification is sent

**AC-013: Patch with reboot does not alert** [FR-003, FR-009, FR-018]
- Given an `os_update` with reboot authorization dispatched at 02:00 by the auto-patch schedule, which succeeds at 02:28 and reports that it is rebooting
- When the device goes offline at 02:29 and is back at 02:31
- Then the offline event is info, linked to the os_update, naming "by the auto-patch schedule", with no notification

**AC-014: Docker daemon restart during a patch does not alert** [FR-009, FR-022]
- Given a running `os_update` without reboot authorization on hawaii
- When every container on hawaii dies and starts again within the window
- Then every death is an info event linked to the os_update and no escalation fires

**AC-015: Device not back by the return bound escalates once** [FR-021, FR-023, FR-024, BR-005]
- Given a `reboot` by `ryan` that reached `succeeded` at 10:00 and a device that went offline at 10:01
- When the device is still offline at 10:15
- Then exactly one `device_offline` escalation is raised by 10:16, through the `device_offline` rule, saying the device is still offline 15m after Reboot by ryan and giving the command ID
- And no second escalation is raised for that command afterwards

**AC-016: Failed or timed-out host action with the device offline escalates at once** [FR-004, FR-020]
- Given a dispatched `reboot` by `ryan` and a device that went offline during it, and the sweeper marks the reboot `timed_out`
- When the timeout is recorded
- Then one escalation naming the reboot, "by ryan" and "timed out" is raised within 60 s

**AC-017: Offline after a failed host action is not suppressed** [FR-004]
- Given a `reboot` that reached `failed` (dpkg lock held past its bound) at 10:00 with the device online
- When the device goes offline at 10:01
- Then the `device_offline` alert fires normally

**AC-018: Container that does not come back after a reboot escalates** [FR-022]
- Given a `reboot` window on hawaii during which containers `plex` and `sonarr` died, and after the reboot only `plex` is observed running
- When the window closes (device online for 3 min)
- Then one `container_died` escalation for `sonarr` is raised naming the reboot and the actor, and none for `plex`

**AC-019: Container still down after a stack redeploy escalates** [FR-022, FR-026]
- Given a `docker_stack_redeploy` of `media` that succeeded, during which `sonarr` died and was not observed running again
- When the window closes 2 min after success
- Then one `container_died` escalation for `sonarr` names "Docker stack redeploy", the actor, "succeeded" and the command ID

**AC-020: Intentionally down containers do not escalate** [FR-022, BR-007]
- Given a `docker_stop` of `plex` and a `docker_stack_down` of `media` that succeeded, with the containers still stopped
- When their windows close
- Then no escalation is raised

**AC-021: Escalation cap** [FR-025, NFR-003]
- Given a `docker_bulk_update` window during which 30 targeted containers died and none came back
- When the window closes
- Then 20 container escalations and one summary escalation naming 10 further containers are raised, and no more

**AC-022: Escalation without a matching rule** [FR-023, FR-026]
- Given no enabled `device_offline` rule
- When an FR-021 escalation becomes due
- Then a `warning` `device_offline` event linked to the command is stored and no notification is sent

**AC-023: Escalation is not blocked by an earlier cooldown** [FR-024]
- Given a `device_offline` rule with a 300 s cooldown that was started 60 s ago by an unrelated offline event of the same device
- When an FR-021 escalation becomes due
- Then the escalation notifies

### Agent update

**AC-024: Fleet agent update shows what is happening** [FR-034, FR-035, FR-036, FR-040]
- Given 3 devices on agent 2.77.0, the server's newest known agent version 2.78.0, and Ryan presses "Update agents" on the fleet page
- When the commands are dispatched
- Then each device shows "Updating agent 2.77.0 → 2.78.0" with "by ryan" and the start time, in the browser that pressed the button and in a second browser opened afterwards

**AC-025: Agent restart offline does not alert** [FR-010, FR-017, FR-035]
- Given an `agent_update` that succeeded at 10:00 and a device that goes offline at 10:00:50 and comes back at 10:02
- Then the offline transition is an info event linked to the agent_update, with no notification
- And the device shows "Restarting agent, back by 10:10" while offline

**AC-026: Agent that does not come back escalates** [FR-021, FR-035]
- Given an `agent_update` that succeeded at 10:00 and a device offline since 10:01
- When the device is still offline at 10:10
- Then one escalation names "Agent update", the actor and the command ID, and the fleet shows the failure state

**AC-027: Agent update does not hide container deaths** [FR-010]
- Given an open `agent_update` window on hawaii
- When container `plex` dies
- Then the `container_died` alert fires normally

### Shutdown

**AC-028: Successful shutdown leaves the device powered off without alerts** [FR-028, FR-029, FR-038]
- Given a `shutdown` by `ryan` that succeeded at 14:02 and PiCUPS going offline at 14:03
- When 3 days pass with the device offline
- Then an info event "Device PiCUPS powered off by ryan at 14:02" exists, no `device_offline` alert or escalation is raised, the fleet shows "Powered off by ryan at 14:02", and the device is counted under "Expected offline", not offline

**AC-029: Powered-off state ends when the device returns** [FR-030]
- Given PiCUPS in the powered-off state
- When it reports online
- Then the online event says "came online after Shutdown by ryan (powered off <duration>)", the label clears, and a later offline transition alerts normally

**AC-030: Failed shutdown does not create the powered-off state** [FR-031]
- Given a `shutdown` that reached `failed` (poweroff refused) with the device online
- When the device later goes offline
- Then the `device_offline` alert fires normally

### Durability and bounds

**AC-031: Windows survive a server restart** [FR-005, FR-032, NFR-006]
- Given an open `docker_update` window for `plex`, a `reboot` in its return phase for vermont, and PiCUPS in the powered-off state
- When the server restarts and then `plex` dies and vermont goes offline
- Then both are suppressed and recorded as info events linked to their commands, and PiCUPS still shows as powered off

**AC-032: Escalation due during downtime fires once after restart** [FR-024, NFR-006]
- Given a reboot whose return bound passed while the server was down, with the device still offline
- When the server starts and runs for 3 minutes, and is then restarted again
- Then exactly one escalation exists for that command

**AC-033: Every window closes on its own** [FR-005, FR-006, NFR-002, BR-004]
- Given commands of each in-scope action that are dispatched and then receive no further report, heartbeat, event or telemetry
- When the clock passes each action's NFR-002 bound
- Then no window remains open for any of them, and a new death or offline transition for their targets alerts normally

**AC-034: Queued command does not suppress** [FR-002, FR-035]
- Given a `reboot` for vermont that is queued and not dispatched
- When vermont goes offline
- Then the `device_offline` alert fires normally, and the fleet label shows "Reboot queued" until the command expires or is dispatched

**AC-035: Server restart grace** [FR-033]
- Given three devices online before a server restart, whose agents reconnect 70 s after the server starts
- When the server has been running for 90 s
- Then none of them has been marked offline and no `device_offline` event exists for them
- And a device that sends no heartbeat is marked offline once 2 min have passed and its last heartbeat is older than 45 s

**AC-036: Fail open when command records cannot be read** [NFR-009]
- Given the command records cannot be read
- When a container dies or a device goes offline
- Then the normal alert fires

**AC-037: Bounded growth** [NFR-003, NFR-004, NFR-005]
- Given a `reboot` window during which 12 containers die and the device goes offline, and the window ends with one container still down
- Then exactly 13 suppressed info events, 1 online event and 1 escalation event were stored, no per-occurrence log line above debug was written, and at most one info log line was written for the escalation
- And within 5 minutes of the window's close no server memory is held for that window

### Dashboard

**AC-038: Label vocabulary** [FR-034, FR-035, FR-036]
- Given each situation in the FR-035 table, including a reboot whose reported step says it is waiting for the dpkg lock
- When the fleet view and the device page are rendered
- Then each shows the specified label with the actor phrase and start time, and the host label wins over container labels when both apply

**AC-039: Offline count** [FR-038, FR-043]
- Given 4 devices: one online, one rebooting in its return phase and offline, one powered off, and one escalated
- When the Fleet Overview is rendered
- Then the offline count is 1, "Expected offline" is 2, and the two expected-offline devices are visibly marked as offline while their text shows the activity label

**AC-040: Live update** [FR-040, FR-041]
- Given an open Fleet Overview
- When a schedule dispatches a `docker_update` on hawaii, and later the command's window closes
- Then the "Docker: updating <target>" label appears within 5 s and clears within 5 s of the window closing

**AC-041: Escalated state display** [FR-037]
- Given an escalated device that stays offline
- When 24 hours pass after the escalation
- Then the failure label is no longer shown (the device shows plain offline), and it also clears earlier if the device comes online

**AC-042: Device page link** [FR-039]
- Given an open window on vermont
- When the device page is opened
- Then its header shows the label, actor phrase, start time and a working link to the command's entry

**AC-043: Existing patch badges unchanged** [FR-042]
- Given the PATCH-FLOW-001 acceptance tests for PatchBadge and RebootClassBadge, and the bulk "Update agents" button tests
- When they run after this story
- Then they pass unchanged

### Compatibility, security, safety, docs

**AC-044: Older agents** [FR-013, NFR-008]
- Given an agent that attaches no command ID to Docker events and sends no `update_started`
- When it runs a `docker_restart` of `plex` and `plex` dies
- Then the death is suppressed and linked to the command

**AC-045: Cross-device isolation** [NFR-010]
- Given an open `docker_update` window for `plex` on hawaii, and a Docker event from vermont that names that command's ID
- When container `plex` on vermont dies
- Then the vermont death alerts normally and is not linked to the hawaii command

**AC-046: Message sanitization** [NFR-010]
- Given a container name of 300 bytes with control characters and invalid UTF-8
- When its death is suppressed and later escalated
- Then both messages contain only printable valid UTF-8 and the name part is at most 128 bytes

**AC-047: No real power actions and no agent execution in tests** [NFR-001, NFR-012]
- Given the test suite, test helpers and scripts added by this story
- When they are inspected and run
- Then none invokes reboot, shutdown, poweroff, `systemctl` power verbs or an installed `riot-agent` binary, and all power and agent behavior is simulated by test doubles

**AC-048: Documentation** [FR-045, FR-046]
- Given the README and CHANGELOG after this story
- When they are read
- Then the README contains Table 4.1's actions, settle periods and bounds, the suppressed and still-alerting cases, the powered-off state, the restart grace and the NFR-011 residuals
- And the CHANGELOG entry says no agent upgrade or `install.sh` re-run is required

## 9. Out of Scope

- Any agent change (EXPECT-002 adds agent reporting).
- The agent's own self-initiated auto-update and agent restarts that are not `agent_update` commands (EXPECT-002).
- Telemetry rules during a window or after boot: `service_state`, `process_missing`, `nic_down`, `usb_disconnected`, `reboot_required`, `log_errors`, container CPU/memory, UPS, GPU, certificate rules (EXPECT-003).
- Notifications for failed or timed-out commands whose targets are running again (EXPECT-004).
- Per-container in-progress state in the Docker containers and stacks pages (EXPECT-005).
- Operator-declared maintenance windows and disruption from actions not started through rIOt (EXPECT-006 candidate).
- Changes to command deadlines, queued time-to-live, the sweeper, the patch badges or the Docker attribution rule for agent-named IDs.
- A dashboard control to cancel, dismiss or extend a window.

## 10. Assumptions

- **A-001:** All defaults in this FRD were set by the Business Developer Agent on Ryan's behalf on 2026-10-26, from the product defaults the orchestrating session (Clod) recorded. Ryan reviews them in the PR.
- **A-002:** The commands record is the durable action record (reuse, no second action store). The architect may add bounded records under NFR-003 only if derivation from commands alone cannot meet FR-024 or FR-032.
- **A-003:** Windows open at dispatch, not at creation, because an action cannot cause disruption before the agent receives it. Queued commands get a label only.
- **A-004:** Reboot, shutdown, authorized os_update and agent_update all report their result before the host or agent goes down (verified for reboot, shutdown and agent_update in code, and for os_update in PATCH-FLOW-001 FR-023). If a result is lost, the command times out and FR-020 applies.
- **A-005:** Every `os_update` gets the 15-min return bound, whether or not a reboot happens, because the server cannot always know in advance whether the agent will reboot. A device that stays online closes the window 3 min after success.
- **A-006:** Return bounds: 15 min for a host reboot (Raspberry Pi and hawaii boots are well under 5 min), 10 min for an agent restart, including the signed-update helper path that restarts the agent shortly after the result.
- **A-007:** The "waiting for dpkg lock" label depends on the agent reporting a step or progress during the wait. If v2.78.0 agents do not report it, the label falls back to "Rebooting" or "Shutting down" until EXPECT-002 adds the reporting.
- **A-008:** A container death is attributed to a host-class command even though the existing attribution rule matches only Docker target keys. Docker-to-Docker attribution keeps the existing rule.
- **A-009:** Ryan's scale is 4 devices (hawaii, vermont, PiVPN, PiCUPS) and fewer than 200 containers. NFR-007 is sized for that.
- **A-010:** The fleet bulk "Update agents" creates one `agent_update` command per device, each with its own window.

## 11. Open Questions and Decisions

### 11.1 Clarifications settled by default (Ryan reviews in PR)

- **OQ-001 Scope of a host action:** reboot, shutdown and os_update suppress all container deaths on the device; agent_update suppresses only device offline (FR-009, FR-010).
- **OQ-002 Window start:** at dispatch (A-003).
- **OQ-003 Settle periods:** 3 min online after a host action, 1 min after an agent restart, 2 min after a container action (Table 4.1).
- **OQ-004 Escalation channel:** through the existing `device_offline` and `container_died` rules, once per command and target, exempt from unrelated cooldowns, capped at 20 containers plus a summary (FR-023 to FR-025).
- **OQ-005 Server restart grace:** 2 min (FR-033).
- **OQ-006 Escalated label lifetime:** until online or 24 h (FR-037).
- **OQ-007 Fleet counts:** "Expected offline" separate from the offline count (FR-038).
- **OQ-008 Actor phrase wording:** FR-018.

### 11.2 Open decisions for Ryan

- **DEC-1 Powered off forever?** After a successful shutdown, the device shows "Powered off by ryan at <time>" and never raises an offline alert until it comes back. Alternative: a reminder alert after N days. **Recommendation:** no time bound, no reminder (FR-028 to FR-030). The FRD is written to this.
- **DEC-2 Return bounds:** 15 min for reboot and patch reboots, 10 min for agent restarts. A real outage on that device is hidden for up to that long after the action. **Recommendation:** keep 15 min and 10 min. The FRD is written to this.
- **DEC-3 Command-failure notifications:** EXPECT-001 alerts on failure only when something is still down. A failed reboot, patch or agent update with the device still healthy stays a warning event with no push. **Recommendation:** keep it that way in EXPECT-001; ship notifications as EXPECT-004 with per-action rule control.
- **DEC-4 OOM during an update:** **Recommendation:** never suppress OOM; the alert names the in-progress action (FR-011).
- **DEC-5 EXPECT-006 (operator-declared maintenance window for ssh reboots and hardware work):** **Recommendation:** keep it as a candidate; build it only if Ryan still gets alerts for his own out-of-band work after EXPECT-001 to EXPECT-003.

## 12. Dependencies

- **D-001:** CMD-LIFECYCLE-001 commands record, sweeper, budgets and Docker event attribution (existing).
- **D-002:** REBOOT-DPKG-001, SHUTDOWN-DPKG-001, SHUTDOWN-SUDO-001 (reboot and shutdown waits, truthful shutdown result).
- **D-003:** PATCH-FLOW-001 (os_update result before reboot; patch badges unchanged).
- **D-004:** Existing alert rules, container scope, notification dispatcher and event retention.
- **D-005:** Fleet Overview and Device Detail pages and their live update channel.
- **D-006:** Pipeline: architect (ADD), security-researcher, senior-dev, qa-engineer, technical-writer. No new third-party dependency.

## 13. Follow-on stories

Ordered. Each gets its own FRD.

1. **EXPECT-001 (this story):** server-side expected-activity windows for container and host actions, suppression recorded as info events, escalation naming the action, powered-off state, server restart grace, fleet and device activity labels.
2. **EXPECT-002, agent self-update and agent restart reporting:** the agent's own scheduled auto-update (`internal/agent/updater.go`), the signed-update helper restart, and the AGENT-PRIV one-click apply become server-side activity records with from and to versions and the actor phrase "by agent auto-update". They open an agent-restart window, show "Updating agent 2.77.0 → 2.78.0" on the fleet page, escalate when the agent does not come back or comes back on the old version, and record a failed self-update as an event naming the versions. The agent also reports a step during the dpkg-lock wait so the "waiting for dpkg lock" label is reliable (A-007). Agent and server change. *(Update: EXPECT-002 was delivered as the server-only part, the agent's scheduled self-update, in `docs/requirements/EXPECT-002-frd.md`. The agent-side parts (dpkg-wait step, AGENT-PRIV apply restart, structured from/to versions) moved to the follow-on EXPECT-007.)*
3. **EXPECT-003, telemetry rules during patch and boot settle:** `service_state` (stopped or failed), `process_missing`, `nic_down`, `usb_disconnected`, `reboot_required`, `log_errors` and container CPU/memory rules are suppressed inside host-class windows and for a bounded boot-settle period after a host returns. Anything still failing when the settle ends alerts once, naming the action.
4. **EXPECT-004, command-failure notifications:** a new alert rule type for failed and timed-out commands, selectable per action and origin (for example: notify on a failed auto-patch or agent update, not on a failed manual log fetch), with cooldown and the auto-update backoff state in the message. Replaces "warning event only" for failed commands (DEC-3).
5. **EXPECT-005, per-container in-progress state in Docker views:** the containers and stacks pages show, per container, "Updating (pull 40%)", "Recreating", "Stopped by ryan", "Stack down by ryan" or "Died, not restarted after Docker update" instead of plain exited or dead, from server state, including actions started by schedules, with a link to the command.
6. **EXPECT-006 (candidate, DEC-5), operator-declared maintenance window:** Ryan declares a bounded window (maximum duration enforced) for a device, for work outside rIOt (ssh reboot, hardware swap). Same suppression, recording and escalation rules as a host-class window, with "by ryan (manual maintenance)" as the actor phrase.
