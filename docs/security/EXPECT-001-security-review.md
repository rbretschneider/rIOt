# Security Review Report
**Story ID:** EXPECT-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-26
**Verdict:** APPROVED WITH CONDITIONS

Inputs:
- `docs/requirements/EXPECT-001-frd.md` (revision 1, commit 0248c56)
- `docs/architecture/EXPECT-001-add.md` (revision 1, commit 7585021)
- Code at 7585021:
  - `internal/server/events/generator.go`
  - `internal/server/commands/service.go`, `creation_limit.go`, `sweeper.go`
  - `internal/models/command_state.go`, `command_policy.go` (`SanitizeText`, `TruncateHead`), `events.go`
  - `internal/server/db/command_repo.go` (`buildTransitionSQL`, the claims), `device_repo.go`
  - `internal/server/handlers/docker_attribution.go`, `handlers.go` (`RegisterDevice`, `Heartbeat`,
    `ReceiveDockerEvent`, `ReceiveAgentEvent`, telemetry ingest), `fleet.go`, `docker_runtime.go`
    (`dockerDispatchRequest`, `dockerActor`), `docker_rate.go`, `auto_update.go` (`checkAutoUpdates`,
    `checkAutoPatch`)
  - `internal/server/router.go`, `internal/server/middleware/auth.go`, `admin_auth.go`
  - `internal/server/websocket/hub.go`
  - `internal/server/notify/dispatcher.go` and the channel backends
  - `internal/server/storage/command_budget.go`, `internal/server/db/docker_retention.go`, `internal/server/server.go`
    (`checkOfflineDevices`)
  - `internal/server/db/db.go` (migration runner); migrations 000028 and 000032
- Production database on hawaii, read-only (`SELECT` and `SHOW` only): table sizes, status distribution, migration
  version, timeouts.

Pre-existing weaknesses found during this review are referenced here only generically. Their details are in Ryan's
private review notes, not in this public repository.

---

## Threat Model Summary

### Assets
- **Alerting integrity, the asset this story changes.** EXPECT-001 adds a control that **turns alerts off**.
  `device_offline` and `container_died` are the signals that tell Ryan a host is down, compromised and pulled, or that
  a service has stopped. Any path that opens, extends or keeps a window, or that loses the escalation backstop after a
  suppression, hides a real outage.
- **Escalation backstop.** Suppression is acceptable only because FR-020, FR-021 and FR-022 fire afterwards. A
  suppressed occurrence whose escalation can be lost silently is a hidden failure.
- **Powered-off state.** This is the only suppression with no time bound (DEC-1, FR-029).
- **Notification channels and Ryan's attention.** A new cooldown-exempt path (FR-024, X-8) can push to third-party
  services.
- **Dashboard truth.** Labels such as "Powered off by ryan" or "Rebooting, back by 14:32" replace "offline" (FR-034,
  FR-043). A wrong label is a misleading security signal.
- **Database and server memory.** New tables, a partial index, the Tracker cache and the 1 s loop.

### Threat Actors
- **Compromised device or agent (holds its own API key or client certificate).**
  - Already controls everything its own device reports: heartbeats, telemetry, Docker events, command results.
  - New in this story: it can shape windows on its own device. It can ack a queued host command early, turn
    `timed_out` into `succeeded`, choose which container names "die" inside a host window, report `start` events, and
    trigger `auto_patch` / `auto_update` dispatch through its telemetry.
  - The question that matters is whether it can reach **other** devices' windows or global state.
- **Authenticated dashboard user.** Single admin role; nothing lower exists. Already holds every command. The
  insider case is out of scope beyond bounds.
- **Unauthenticated network attacker.** Reaches only `/health`, `/readyz`, the auth routes, `/api/v1/devices/register`
  (device proof required for an existing device) and the agent WebSocket (device auth).
- **Environment.** riot-server restarts and auto-updates, DB errors, clock steps, slow notification endpoints, a
  riot-server crash loop.
- **Future developer (regression).** A test or helper that runs a real power action or an installed `riot-agent`
  (NFR-001).

### Attack Surface Introduced
- `GET /api/v1/activity` (admin session) and the WS message `activity_update` (dashboard hub only).
- The decision path from agent-supplied Docker events and agent command reports to suppression.
- A durable, unbounded-duration suppression record (`device_powered_off`).
- A cooldown-exempt notification path (escalations).
- Migration 000033:
  - two columns and a backfill `UPDATE` on `commands`;
  - two indexes;
  - two tables.
- A 1 s server loop, a 30 s reconcile and an in-memory cache.
- A 2-minute window after each server start in which no device can go offline.
- Fleet bulk dispatches now record the operator (AD-016).
- No new agent endpoint, agent privilege, dependency or `install.sh` change.

---

## Findings

There are no CRITICAL findings.

Cross-device isolation holds:
- The device key is bound to the `{id}` path.
- `TargetKeys` is `json:"-"` and computed server-side.
- Coverage searches only the path device's windows.
- Transitions and attribution are device-scoped in SQL.
- The agent self-event endpoint cannot set command fields.

The findings are about **what happens when suppression ends without its backstop**. One of them (SEC-001) is an
unbounded silent suppression.

### HIGH

#### SEC-001: A stale powered-off row silently suppresses every later outage of that device, with no time bound
**Severity:** HIGH
**Domain:** Business logic / fail-open (BR-004, BR-006, NFR-009)
**Location:** ADD AD-007 ("`CoverOffline` returns kind `powered_off` ... when ... a powered-off row already exists"),
AD-006 (paths 2 and 4, `Load` initial state), §9 rows "`DeletePoweredOff` ... fails in `NoteOnline`"; FRD FR-029,
FR-032

**Description:**
The powered-off state is cleared only when the Tracker observes an offline→online transition:
- `NoteOnline` from `RegisterDevice` when the device was not online; or
- `ObserveDevices` seeing DB `online` where the Tracker had offline.

Once the row exists, AD-007 makes it a coverage reason **by itself**: any later `device_offline` becomes an `info`
event, with no escalation and no time bound.

The row outlives the device's return in several realistic ways:
1. **A heartbeat return across a server restart.** `Heartbeat` sets `status='online'` silently
   (`DeviceRepo.UpdateHeartbeatTime`). If riot-server restarts before the next offline-detector pass, the next `Load`
   sees a DB-online device **and** a powered-off row. `ObserveDevices` never runs during the 2-minute grace, so a
   restart inside the grace has the same effect. The ADD does not say how `Load` reconciles the two. With
   `online=true` from the DB there is never a transition to observe, so the row is never deleted. riot-server
   auto-updates on hawaii, so restarts are routine.
2. **A failed delete.** §9 says that when `DeletePoweredOff` fails, "the in-memory state is cleared anyway; the row is
   retried by the next reconcile, which re-reads". If reconcile re-reads `ListPoweredOff` into memory, the stale state
   comes back, the device is already online, and nothing deletes it again.
3. **Registration while DB-online.** A heartbeat arrives before registration, so `wasOnline=true`. `DeviceOnline`
   (and `NoteOnline`) is never called. Only `NoteRegistration` runs, and it does no I/O.
4. **`NoteOnline` before the Tracker is ready** (Load failed, retrying). The ADD does not say whether the durable
   delete still happens.

The key observation: a **new online→offline transition can only happen if the device was observed online after it
powered off**. The detector moves only online devices to offline. So "a row already exists" as a coverage reason for a
new transition is only ever true in the stale case. The legitimate entry paths do not need it:
- `CoverOffline` in the shutdown return phase;
- the loop's restart-case entry from `offlineSince`.

**Attack Scenario:**
1. Ryan shuts down PiCUPS from the dashboard. The row is created ("Powered off by ryan at 14:02").
2. Ryan powers PiCUPS back on during a riot-server auto-update. The agent's first contact is a heartbeat, which sets
   DB `online`. The server restarts, `Load` puts PiCUPS online with the powered-off row still in place, and nothing
   deletes it.
3. Weeks later PiCUPS dies, or an attacker who has compromised it unplugs it to cover their tracks.
4. The offline transition is stored as `info` "Device PiCUPS powered off by ryan at 14:02". There is no notification
   and no escalation, now or ever (DEC-1).
5. The fleet shows "Powered off by ryan" and counts it under "Expected offline", not offline.

**Required Resolution (properties):**
- (a) A pre-existing powered-off state must **never** be a coverage reason for a new online→offline transition. A
  transition proves the device was alive after it powered off: it must end the state and take the normal path. Only
  the first offline after a **succeeded** `shutdown`, judged inside that shutdown's return phase, may enter or
  extend the state. This includes the loop's restart-case entry: `offlineSince` must lie in `[D, closeAt)` of that
  shutdown window.
- (b) Any evidence of life newer than the row's `offline_at` invalidates the row. That means a heartbeat, a
  registration or a DB status other than `offline`. Invalidation must be checked against durable data at `Load`, at
  every reconcile and at decision time. It must not depend only on observing an in-memory transition. A reconcile must
  never resurrect a row for a device that is live.
- (c) If the row cannot be verified or deleted (DB error), the decision fails open: the offline transition alerts.
- (d) A powered-off row must never be re-stamped by a later offline transition. A later transition does not refresh
  `offline_at` or `created_at`.
- (e) Tests cover, with an injected clock and mocks:
  - heartbeat return then restart (inside and outside the grace);
  - `DeletePoweredOff` failure followed by reconcile;
  - registration with `wasOnline=true`;
  - `NoteOnline` while not ready;
  - a second offline transition after any of these.

  In every case the second transition alerts.

**Blocks:** No (clear, non-architectural correction of AD-007 and AD-006). Condition C-1. The FRD needs no change:
FR-029 still holds, because a device that has come back is not powered off.

---

### MEDIUM

#### SEC-002: Several window-end paths drop the escalation backstop after a suppression ("fail-open" that is actually fail-silent)
**Severity:** MEDIUM
**Domain:** Business logic / alerting integrity (BR-005, NFR-006, NFR-009)
**Location:** ADD R-1, AD-009 (lapse, 5-min eviction), AD-012 (Tracker cap of 200 per device, "oldest window" dropped
first), §6.3 `ListWindowCandidates` (`ORDER BY created_at`, `LIMIT 1000`), AD-011 (FR-022 skipped "when the device
is offline at the close"); `internal/server/storage/command_budget.go`, `internal/server/db/docker_retention.go`

**Description:**
The ADD calls these paths fail-open because suppression stops. That is true for **future** occurrences. But the
occurrences already suppressed as `info` lose the FR-020, FR-021 or FR-022 check that justified suppressing them,
and nothing else will ever alert for them: the device is already offline, or the container is already dead, so no
new transition comes.
- **(a) Command deletion.** The command-results budget deletes terminal commands by age **globally, across devices**
  (detail in the private notes). It and the 90-day retention do not know about windows. Deleting a row in its return
  or settle phase cascades its `command_escalations` and erases the window. R-1 describes this as fail-open; for the
  backstop it is fail-silent.
- **(b) Hard-bound close of a non-terminal command.** AD-009 step 2 escalates "at its return bound". A host or agent
  window that reaches `hard` while still active is not clearly covered: for example, when the sweeper lags or fails,
  the command is never marked `timed_out`, so FR-020 never runs. It can close as `closed` or `lapsed` with the device
  offline and its offline already suppressed.
- **(c) Cap and limit overflow.** The 200-per-device cap drops "the oldest window", and `ListWindowCandidates` returns
  the **oldest** 1000 rows. Both can leave a window that has already suppressed something unprocessed until the
  24-hour lapse. The lapse then closes it as `lapsed` with no escalation and no warning.
- **(d) A container-class window closing while the device is offline.** FR-022 is skipped. The `device_offline` alert
  fires (container windows do not cover offline). But when the device returns, containers whose deaths were
  suppressed and that did not restart are never re-checked.

**Attack Scenario:**
1. Ryan reboots vermont. vermont goes offline inside the window; the transition is stored as `info`.
2. Before the 15-minute return bound, the reboot command row is evicted. The trigger is (a), result-budget pressure,
   or (b), a sweeper failure while the window runs to `hard`.
3. vermont never comes back. No `device_offline` alert was sent (suppressed), no escalation fires (row gone or window
   closed quietly), and vermont is already `offline` in the DB, so no later transition will alert. Ryan sees no alert
   for a dead host.

**Required Resolution (properties):**
- (a) **No silent end after a suppression.** Every path that ends a window must either run the FR-020, FR-021 and
  FR-022 checks or raise the escalation. That covers settle, return bound, hard bound with a non-terminal command,
  lapse, eviction and cap overflow. A host or agent window that ends for **any** reason while its device is offline
  and its offline was suppressed must escalate once (FR-024 marker).
- (b) Commands whose window is not yet processed (`window_closed_at IS NULL`) are exempt from budget and retention
  deletion. The set is bounded by AD-012 (open windows plus at most 24 hours of backlog), so the exemption keeps
  NFR-003.
- (c) Overflow (cap, `LIMIT`) must never pick the newest or currently-covering windows for exclusion ahead of older
  closed-but-unprocessed ones. Every overflow or lapse that skips close processing writes a `Warn` (through the
  existing throttle) **and** a durable `warning` event. A dropped backstop must be visible.
- (d) For (d) above, either re-run FR-022 for those containers when the device returns within a stated bound, or add
  it to the README residuals with Ryan's acceptance.
- (e) Tests: a command deleted mid-return-phase with the device offline escalates (or the delete is refused); hard
  bound with no sweeper escalates; lapse writes the warning event.

**Blocks:** No. Condition C-2.

---

#### SEC-003: The 1 s loop can be stalled by a synchronous notification, and staleness does not measure loop liveness
**Severity:** MEDIUM
**Domain:** Availability / fail-open (NFR-007, NFR-009, BR-006)
**Location:** ADD AD-010 step 4 (`PublishEscalation` → `dispatcher.Dispatch`), AD-009 ("Every DB call uses a 5 s
context"), AD-002 (`ExpectStaleAfter` = 90 s measured from the last successful reconcile);
`internal/server/notify/dispatcher.go`

**Description:**
- `Dispatcher.Dispatch` sends to every channel synchronously. Outbound notification delivery is not time-bounded on
  every path today (detail in the private notes).
- The ADD bounds DB calls with a 5 s context but says nothing about the publish step. `PublishEscalation` runs inside
  the Tracker loop. One slow or blackholed channel endpoint can therefore stall the loop: no window closes, no FR-020,
  FR-021 or FR-022 escalation, no `activity_update`.
- The fail-open signal measures reconcile success, not the liveness of the component that makes close and escalation
  decisions. If reconcile runs on a separate goroutine, coverage keeps answering "covered" from a fresh cache while
  every backstop is blocked.

**Attack Scenario:**
1. A notification endpoint stops answering TCP (outage or blackhole). Ryan has not configured anything wrong.
2. A reboot window with the device offline reaches its return bound. The loop enters `PublishEscalation` and blocks.
3. Every later escalation on every device waits behind it. Suppression decisions keep working from the cache, so new
   deaths inside other windows stay `info`, and their backstops are queued behind the stall indefinitely.

**Required Resolution (properties):**
- (a) The escalation publish (hub plus notification dispatch) has a bounded deadline, or runs off-loop with bounded
  concurrency. A stuck channel can delay at most its own notification, never window evaluation or other escalations.
  The escalation event is already committed first (AD-010), so dropping a timed-out push is R-3.
- (b) Tracker readiness (the `Cover*` error path) also fails open when the evaluation loop has not completed a tick
  within a stated bound, not only when reconcile is stale.
- (c) Test: a publisher that blocks forever does not stop a second window's close or escalation, and `Cover*` returns
  the fail-open error after the liveness bound.

**Blocks:** No. Condition C-3.

---

#### SEC-004: The offline-liveness predicate the Tracker inherits does not cover every live device status (pre-existing)
**Severity:** MEDIUM (pre-existing; this story moves and builds on the code)
**Domain:** Alerting integrity
**Location:** ADD AD-008 ("a device goes offline when `status == online` and ..."), AD-006 path 4 and `Load` ("DB
`online`"); `internal/server/server.go` `checkOfflineDevices`

**Description:**
- The offline detector and, by the ADD, the Tracker's view of "online" key on the literal status `online`.
- Devices can hold another live status, and a device in that status is never moved to offline by the detector. The
  details of the trigger are in the private notes.
- The ADD moves this predicate verbatim into the new `offlineDetector` and builds `ObserveDevices`, the powered-off
  clearing (SEC-001) and the early-close logic on it. The pre-existing gap therefore carries into the new state
  machine.

**Attack Scenario:** Refer to the private notes. In short: a live device in that state that then loses power raises no
`device_offline` alert, today and after this story.

**Required Resolution (properties):**
- (a) The offline detector moves **every** device whose status is not `offline` and whose heartbeat is stale to
  `offline`, through `DeviceOfflineAt`.
- (b) The Tracker's online and offline view (`Load`, `ObserveDevices`, powered-off invalidation) uses the same
  predicate.
- (c) Test: a device in each non-`offline` status with a stale heartbeat is marked offline after the grace and alerts
  when not covered.

**Blocks:** No. Condition C-4. The story is the right place to fix this, because it rewrites this exact function.

---

### LOW

#### SEC-005: Escalations bypass cooldown with a per-command cap only; agents can influence the fan-out
**Severity:** LOW
**Domain:** Abuse prevention / notification flooding; data exposure
**Location:** ADD AD-010 (X-8: escalations neither check nor start a cooldown), FR-025 (20 + 1 per command);
`internal/server/handlers/auto_update.go` (`checkAutoPatch` dispatches from agent-reported pending updates)

**Description:**
- **Agent-controlled fan-out.** A host-class window covers every container on the device, so any name the agent sends
  in a `die` event inside the window is suppressed. At close, FR-022 escalates up to 21 of them, each a notification
  that ignores cooldown and carries an agent-chosen (sanitized) name.
- **Agent-triggered windows.** With auto-patch enabled for the device, the agent can trigger a new `os_update` by
  reporting pending updates, then fail it at once. The 2-minute settle then produces another FR-022 round.
- **Existing limits.** The creation gate (30 burst, then 30 per hour for schedule origins), the policy dedupe and the
  auto-patch backoff limit this, but only per command. Nothing bounds escalation pushes per device over time.
- **Operator name to third parties.** Escalation messages now carry the operator's display name (`by <actor_name>`)
  to third-party notification services. For OIDC users this can be a full name or an email address. Today's alert
  messages carry no actor.

**Attack Scenario:** A compromised device with auto-patch enabled cycles patch → immediate failure → 21 escalation pushes
per cycle. Notification quotas run out (for example Pushover or ntfy rate limits), or Ryan mutes the channel and misses
real alerts.

**Required Resolution (properties):**
- (a) Escalation **notifications** have a per-device ceiling over time, stated in the ADD (for example per hour). Past
  it, escalations are still stored as events but not pushed, and one summary push says how many were held. Escalation
  **events** stay exactly-once per FR-024.
- (b) Alternatively, Ryan explicitly accepts the per-command cap as the only bound (recorded in the README residuals).
- (c) The README states that escalation notifications include the operator's display name.

**Blocks:** No. Condition C-5.

---

#### SEC-006: `SafeName` can merge distinct containers into one escalation marker, and lets display-spoofing characters through
**Severity:** LOW
**Domain:** Input validation / integrity of exactly-once (FR-024, NFR-010)
**Location:** ADD AD-010 ("Targets ... `container:<name>` (name sanitized, ≤ 128 bytes)"), AD-015 `SafeName`

**Description:**
- **Marker collisions.** The marker key is the **sanitized, truncated** name. Two distinct containers whose names map
  to the same `SafeName` collide on `(command_id, target)`. Examples: names that share the first 128 bytes, or names
  that differ only in stripped characters. The second container's escalation is silently dropped by `ON CONFLICT DO
  NOTHING`.
- **Display spoofing.** `SafeName` removes C0 and C1 controls and U+007F. It keeps Unicode format characters such as
  bidi overrides (U+202A–U+202E, U+2066–U+2069) and zero-width characters. These can make a name read differently in
  the dashboard and in notifications.
- **Unsanitized field.** The `Event.ContainerName` field of new suppressed and escalation events is not listed as
  sanitized. Only the message is.

**Attack Scenario:** A container named `plex` + U+202E + `...` renders as a different, benign-looking name in a
"still not running" notification. Separately, two long-named containers in one stack both fail after a redeploy, and
Ryan is told about only one.

**Required Resolution (properties):**
- (a) The marker identity is collision-free for distinct container names. For example, it can derive from the full
  name with a bounded, collision-resistant encoding. The displayed name stays the `SafeName`.
- (b) `SafeName` also removes Unicode format characters (category Cf).
- (c) Every string field of the new events that comes from an agent (`ContainerName`, the message parts) goes through
  `SafeName` or the same rules.
- (d) AC-046 is extended with a bidi-override case and a 129-byte shared-prefix pair.

**Blocks:** No. Condition C-6.

---

#### SEC-007: The 2-minute restart grace can be re-armed indefinitely by a restart loop
**Severity:** LOW
**Domain:** Availability of alerting
**Location:** ADD AD-008, FRD FR-033

**Description:**
- Each server start opens a fresh 2-minute period in which no device can go offline, and the Tracker makes no
  online-state decision in it.
- A riot-server that restarts more often than every 2 minutes therefore never detects an offline device. Causes
  include a bad auto-update, a panic on some input, OOM, or a container restart policy.
- Container alerts still flow in that state, so the server looks alive.

**Attack Scenario:** An input that crashes the server, or a bad release, causes a restart about every 90 s. Every
device can now go dark without an offline alert, for as long as the loop lasts.

**Required Resolution (properties):**
- (a) Repeated restarts cannot extend the no-offline period without bound. For example, the grace counts from the
  earliest start in a recent restart series, using a durable timestamp. The ADD states the bound.
- (b) Alternatively, Ryan accepts this as a README residual and each start logs a `Warn` when the previous start was
  less than the grace ago.

**Blocks:** No. Condition C-7.

---

### INFORMATIONAL

#### INFO-1: Same-device trust is unchanged in kind; the ADD's §10 wording overstates it
"Only rIOt-dispatched commands open windows" is true, but a device's own agent can influence those windows:
- it can trigger schedule-origin commands through its telemetry (`checkAutoPatch`, `checkAutoUpdates`);
- it can set D early by acking from `queued`;
- it can choose which names die inside a host window;
- it can report `start` events and telemetry that satisfy FR-022;
- it can turn `timed_out` into `succeeded`, including for a shutdown, which then permits the powered-off entry within
  the return phase.

All of this is bounded by NFR-002, and SEC-001 (a) bounds the powered-off case. It is equivalent to what a compromised
agent can already do: report healthy heartbeats and suppress its own Docker events. No path reaches another device.
The README and ADD §10 should say "a device's own agent can open, shape and close windows on its own device only".

#### INFO-2: Cross-device isolation verified
- `DeviceAuth` rejects a key whose device differs from `{id}`.
- `DockerEvent.TargetKeys` and `CommandAttr` are `json:"-"`.
- `validateDockerEventCommand` clears agent-supplied attribution and re-derives it with `GetForDevice` or a
  device-scoped list.
- Agent command reports are device-scoped in the `UPDATE ... WHERE device_id` guard.
- `ReceiveAgentEvent` cannot set `command_id` or `command_attr`.
- `SuppressedDeaths` and `ContainerStartsSince` take `deviceID`.

AC-045 must also assert that a hawaii event naming a vermont command ID is not linked and not suppressed when vermont
has a host window open.

#### INFO-3: Migration 000033 is safe at hawaii's size
Read-only facts from riot-db on 2026-10-26:
- `commands`: 1807 rows, 6.9 MB, no non-terminal rows;
- `events`: 9427 rows, 5.4 MB;
- schema version 32, clean;
- `statement_timeout` and `lock_timeout` are both 0.

The migration runs as one implicit transaction before the server serves:
- `ADD COLUMN ... NOT NULL DEFAULT ''` is metadata-only on PostgreSQL 16;
- the backfill rewrites about 1.8k rows;
- the plain `CREATE INDEX` on `events` takes well under a second.

Remaining notes:
- With no `lock_timeout`, the startup could wait behind a concurrent nightly `pg_dump`. That lasts seconds at this
  size; acceptable.
- The down migration loses only the new state. After down→up, markers for commands that finished within the last hour
  are gone, so those commands could escalate a second time. That is acceptable for a rollback, and the implementation
  report should say so.

#### INFO-4: Activity API and WebSocket exposure are acceptable
- `GET /api/v1/activity` sits in the `adminAuth` group. There is a single role, so no lower-privilege user exists.
- The payload repeats what the commands API already shows (action, actor phrase, versions, target) and adds no
  params, stderr or output.
- The dashboard hub is reachable only through `adminAuth` `/ws`. Agents use a separate socket.
- The endpoint costs a lock-protected read of in-memory state with no parameters, so it needs no rate limit.
- Web: React escaping covers labels. The X-3 link must URL-encode `host` and `command`, and no new code may use raw
  HTML rendering (the existing static guard should cover the new files).

#### INFO-5: Clock assumptions
- `last_heartbeat` comes from PostgreSQL `NOW()`. Command timestamps come from the application clock. Both run on
  hawaii's kernel clock, so this is fine. The implementation should say so in a comment at `offlineAt`.
- A backward clock step extends a window by at most its own length: coverage needs `D ≤ t`.
- A forward step closes windows early, which fails open.

#### INFO-6: NFR-001 safety
- The Tracker, the generator, the offline detector and the handlers execute nothing.
- The AC-047 AST scan (§12 note 9) is adequate for this story's files.
- Nothing in the design calls or probes an installed `riot-agent`.
- QA should confirm the scan's file list includes every new `_test.go` file the implementation adds, not only those
  named in §12.

#### INFO-7: Bounded growth holds, subject to C-2 (b)
- Suppressed events replace today's warning events 1:1, behind the existing per-name throttle.
- At most 22 markers per command (cascade).
- At most 1 powered-off row per device.
- The partial index is bounded by open windows plus 24 hours of backlog.
- Tracker memory is at most 200 windows per device.
- Activity broadcasts: at most one per device per change.
- No per-event state.

The C-2 (b) exemption adds no growth beyond the existing pending-set bound.

---

## Positive Observations
- **Fail-open is designed in.** Cover* errors when the cache is unloaded or stale, every DB failure on the decision
  path alerts, and coverage is computed from time, so suppression ends at its bound even if the loop does nothing.
- **Durable, exactly-once escalation.** The marker and the event commit in one transaction under a primary key, and
  the push follows the commit.
- **Cross-device isolation by construction.** Agent-supplied IDs are ignored for coverage, and keys are derived
  server-side.
- **The old per-device downgrade is removed.** That downgrade (G4) let any container on a device die silently during
  any update. Removing it is a real security improvement.
- **OOM is never suppressed (DEC-4).** Non-success ends offline coverage at once.
- **Offline is judged at `lastHeartbeat + 45s`.** A transition that happened during downtime is attributed honestly,
  and the host and agent backstops cover the backdated case.
- **The restart grace closes G10.** Servers restarting no longer page Ryan for every device.
- **AD-016 uses the same actor extraction as single-device commands.** The actor name is already sanitized and masked
  at creation in `Dispatch`.
- **Every growth path in AD-012 has a stated bound and cleanup.**

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.** There are no CRITICAL findings.

**SEC-001 (HIGH)** is the one unbounded silent suppression in the design: a stale powered-off row. Its resolution is
a narrow correction to AD-007 and AD-006, with no new component and no FRD change:
- a new offline transition is never covered by an old powered-off row;
- the row is invalidated against durable evidence of life.

The senior_dev implements it as condition C-1, and QA verifies it. If the architect prefers to revise AD-007 text
first, that is welcome but not required.

The MEDIUM findings close the gaps where a suppression can lose its backstop:
- SEC-002: window-end paths and command deletion;
- SEC-003: the loop stall;
- SEC-004: the inherited liveness predicate.

The LOW findings bound notification abuse (SEC-005), harden name handling (SEC-006) and bound the restart grace
(SEC-007).

### Conditions (senior_dev implements; QA verifies)
1. **C-1 (SEC-001).**
   - A pre-existing powered-off row is never a coverage reason for a new online→offline transition. That transition
     ends the state and alerts normally.
   - Entry happens only from a succeeded shutdown, judged inside its return phase. The loop entry requires
     `offlineSince ∈ [D, closeAt)`.
   - Any heartbeat, registration or non-`offline` DB status newer than `offline_at` invalidates the row. This is
     checked at `Load`, at reconcile and at decision time.
   - Unverifiable means alert.
   - The row is never re-stamped.
   - The SEC-001 (e) tests pass.
2. **C-2 (SEC-002).**
   - No window ends without its backstop: a host or agent window ending for any reason with its suppressed device
     still offline escalates once.
   - Rows with `window_closed_at IS NULL` are exempt from budget and retention deletion.
   - Overflow never excludes covering or newest windows first.
   - Every lapse or overflow that skips close processing writes a throttled `Warn` and a `warning` event.
   - Case (d) is either re-checked on return or documented as a residual Ryan accepts.
   - The SEC-002 (e) tests pass.
3. **C-3 (SEC-003).**
   - The escalation publish is time-bounded or off-loop with bounded concurrency.
   - Readiness also fails open on loop non-liveness within a stated bound.
   - Blocking-publisher test.
4. **C-4 (SEC-004).**
   - The detector and the Tracker treat every non-`offline` status with a stale heartbeat as going offline.
   - Per-status test.
5. **C-5 (SEC-005).**
   - Either a stated per-device ceiling on escalation pushes over time, with a held-count summary, or Ryan's recorded
     acceptance of the per-command cap.
   - The README notes that escalation pushes carry the operator's display name.
6. **C-6 (SEC-006).**
   - Collision-free marker identity.
   - `SafeName` strips category Cf.
   - All agent-sourced string fields of the new events are sanitized.
   - AC-046 extended.
7. **C-7 (SEC-007).** Either a stated bound on the cumulative no-offline period across rapid restarts, or a README
   residual plus a start-up `Warn`.
8. **C-8 (QA gate).** These pass, and QA confirms the AC-047 scan covers every new Go test file:
   - AC-033, AC-036, AC-045 (with the INFO-2 extension), AC-046 and AC-047;
   - `TestEX_LateResultAfterTimeoutDoesNotReopen`;
   - the tests added under C-1 to C-4.

### For Ryan's decision
- **SEC-005.** Choose between a per-device hourly ceiling on escalation pushes (recommended) and accepting the
  21-per-command cap as the only bound.
- **SEC-002 (d).** Choose between re-checking suppressed container deaths when a device returns after a
  container-window close (recommended) and documenting the gap.
- **SEC-007.** Choose between a durable restart-series grace (recommended if riot-server auto-update failures are
  plausible) and a documented residual with a start-up warning.
- **Pre-existing items** referenced generically above (SEC-002 (a) budget scope, SEC-003 notification timeouts,
  SEC-004 liveness predicate) and one unrelated agent-endpoint input-handling gap are described in Ryan's private review
  notes. They should be filed as separate fixes. SEC-004 is fixed inside this story under C-4.
