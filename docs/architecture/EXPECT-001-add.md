# Architecture Decision Document

- **Story ID:** EXPECT-001
- **FRD Reference:** `docs/requirements/EXPECT-001-frd.md` (revision 1, commit 0248c56; DEC-1 to DEC-5 at the recommended option)
- **Builds on:** CMD-LIFECYCLE-001 (`docs/architecture/CMD-LIFECYCLE-001-add.md`: commands record, sweeper, target keys, Docker event attribution), REBOOT-DPKG-001, SHUTDOWN-DPKG-001, PATCH-FLOW-001
- **Author:** Architect Agent
- **Date:** 2026-10-26
- **Status:** DRAFT, revision 1 (for security review)

---

## 1. Summary

A new server package, `internal/server/expect`, holds a **Tracker**. The Tracker derives every expected-activity window from the existing `commands` table and answers "is this death / offline transition expected, and by which command?" from an in-memory cache. The cache is loaded at start, kept current by a hook on every command transition, and reconciled from the database every 30 s.

The event generator asks the Tracker before it alerts:
- a covered `container_died` or `device_offline` is stored as an `info` event linked to the command and is never notified;
- an uncovered one, or one the Tracker cannot decide (fail open), alerts exactly as today.

A 1-second evaluation loop closes windows at their settle condition or hard bound, raises the escalations (FR-020 to FR-022) through the existing alert rules, and pushes a server-computed activity per device to the dashboard (`GET /api/v1/activity` plus the `activity_update` WebSocket message).

Durable additions are small and bounded:
- two columns on `commands`: `window_closed_at`, `window_outcome`;
- one exactly-once marker table `command_escalations`: at most 22 rows per command, cascade-deleted with it;
- one row per device in `device_powered_off`;
- two partial indexes.

The per-device in-memory "update in progress" counter is removed. riot-server gets a 2-minute offline-detection grace after start. The agent is not changed.

## 2. Technical Context

### 2.1 What exists (verified at 0248c56)

| Area | Where | Relevant facts |
|---|---|---|
| Durable action record | `commands` table (migrations 000001, 000023, 000032); `internal/models/commands.go`; `internal/server/db/command_repo.go` | `dispatched_at` and `deadline_at` are set at claim (`ClaimQueued`, `ClaimByID`, and the agent edge out of queued). A revert to queued sets them back to NULL. `finished_at` is set on every terminal transition. `timed_out` accepts one late agent result, which rewrites `finished_at`. Target keys: `host`, `docker:container:<name>`, `docker:compose:<wd>`, `docker:id:<id>` |
| Single writer | `internal/server/commands/service.go` | Every accepted transition (create, claim, revert, agent report, cancel, sweep) ends in `Service.broadcast(cmd)`. That is the single choke point for the Tracker hook (AD-004) |
| Budgets | `internal/models/command_policy.go` | `ExecDeadline`: reboot and shutdown 20m, os_update 40m, docker_update 35m, `docker_stack_*` 35m, bulk min(35m×N+5m, 6h), agent_update 15m, docker_restart and docker_stop 15m (ClassOther) |
| Retention | `internal/server/db/docker_retention.go`; `internal/server/storage/command_budget.go` | Terminal commands are deleted after 90 days, or earlier under the command-results budget. `command_output` cascades |
| Container death | `internal/server/events/generator.go` `CheckDockerEvent` | Downgraded only by the per-device `activeUpdates` counter (G1 to G5). Excluded containers store no event outside the "updating" branch |
| Offline / online | `generator.go` `DeviceOffline` / `DeviceOnline`; `server.go` `checkOfflineDevices` (30 s ticker, 45 s threshold) | `recentDisruptiveCommand` uses a 2-minute `created_at` rule (G6, G7, G8). `DeviceOnline` is emitted only by `RegisterDevice` when the device was not online (the agent registers on every start: `internal/agent/agent.go`). A heartbeat brings a device back silently (`DeviceRepo.UpdateHeartbeatTime`) |
| Docker attribution | `internal/server/handlers/docker_attribution.go` | Computes the event's keys (the name key, plus the compose key from the latest snapshot), then reads `ListByDevice(…,100)`. Same device only |
| Fleet actions | `internal/server/handlers/fleet.go` | `BulkUpdateAgents` and `BulkPatchDevices` call `Dispatch` **without actor fields**, so today the actor is `unknown` with an empty name. AC-024 needs "by ryan" (AD-016) |
| Dashboard | `web/src/pages/FleetOverview.tsx` (status dot only; "Updating…" is the confirm-button pending label); `web/src/pages/DeviceDetail.tsx`; `web/src/hooks/useDevices.ts` (WS handler) | No server-side activity view exists |
| Clock injection | `commands.Options.Now`, `HandlerDeps.Clock` | The generator reads `time.Now` directly. A clock seam is added (AD-013) |

### 2.2 What is missing

A window model, durable escalation markers, the powered-off state, a restart grace, an activity API, and the dashboard labels.

### 2.3 What must change

See Section 4. No agent file changes. No `install.sh` change. No new dependency.

---

## 3. Architecture Decisions

### AD-001: Windows are derived from `commands`; window state is not stored separately

**Decision.** A window is a pure function of one command row (action, status, `dispatched_at`, `deadline_at` / `exec_deadline_s`, `finished_at`), the device's observed online state, and the clock (AD-003). Nothing stores "window open". Only the **end** of processing is persisted: `commands.window_closed_at` (and `window_outcome`), written once the close actions (escalation checks) are done.

**Rationale.** FRD A-002 asks to reuse the commands record. Deriving the window keeps one source of truth and survives restart by construction (NFR-006). A close marker is needed because:
- (a) the loader needs a bounded "still to process" set;
- (b) a late agent result after `timed_out` must not re-open a window that was already closed and escalated.

**Alternatives considered.**
- A separate `expected_windows` table with one row per window: duplicates the command lifecycle and needs an insert in the creation transaction. Rejected.
- In-memory windows only: fails G3 and NFR-006. Rejected.

**Consequences.**
- `models.Command` gains `WindowClosedAt *time.Time` (`json:"-"`), selected in `commandColumns`.
- Any command row that is deleted, for example by the command budget, takes its window with it. The result is fail-open: normal alerting.

### AD-002: Decisions are made from an in-memory cache; the database is the truth

**Decision.** `expect.Tracker` keeps, per device:
- the window-candidate commands (snapshots of the fields in AD-001);
- the device's online state, `onlineSince`, `offlineSince`, `returnedAt`, hostname and agent version;
- at most one powered-off state;
- at most one open device escalation (for the label).

It is filled by:
1. `Load` at start: `ListWindowCandidates`, `ListPoweredOff`, `ListOpenDeviceEscalations`, `DeviceRepo.List`, `AutoUpdateRepo.ListAll`;
2. the commands hook (AD-004) on every accepted transition;
3. `Reconcile` every 30 s, which replaces the command snapshots with the DB rows and lapses stale rows (AD-009);
4. device hooks (AD-006).

`CoverContainer` and the `CoverOffline` decision are lock-protected map lookups with **no database access**.

**Rationale.** NFR-007 (≤ 100 ms p95) and the hot-path rule: `CheckDockerEvent` and the 30 s offline worker then add O(open windows on the device), with no query. The hook makes the cache current within the same process, because `commands.Service` is the single writer. Reconcile self-heals any drift.

**Fail open (NFR-009, AC-036).** Every `Cover*` call returns an error when:
- the Tracker has never loaded successfully; or
- the last successful reconcile is older than `models.ExpectStaleAfter` = **90 s** (three reconcile intervals).

The generator then takes today's alert path.

**Alternatives considered.** One DB query per Docker event, with a partial index. It is simpler to reason about, but it puts a query on every die/offline and still needs in-memory online state. Rejected.

**Consequences.**
- Memory is bounded (AD-012).
- The cache can be at most one reconcile interval behind for commands written outside `commands.Service`. None exist today.

### AD-003: Window policy table and close rules (Table 4.1, FR-002 to FR-006)

`internal/models/expect.go` holds `ExpectPolicy(action) (WindowPolicy, bool)`. Only these 12 actions return `ok`:

| Action(s) | Class | Covers `device_offline` | Covers `container_died` | Post-terminal period | Early close needs an observed return | FR-021 | FR-022 |
|---|---|---|---|---|---|---|---|
| `reboot` | host | yes | every container on the device | settle 3m online, return bound 15m | **yes** | yes | yes |
| `os_update` | host | yes | every container | settle 3m, bound 15m | no (A-005) | yes | yes |
| `shutdown` | shutdown | yes | every container | powered off on going offline; else bound 15m | n/a | no | no |
| `agent_update` | agent | yes | none | settle 1m, bound 10m | **yes** | yes | no |
| `docker_update`, `docker_bulk_update`, `docker_restart`, `docker_stack_redeploy`, `docker_stack_restart`, `docker_stack_up` | container | no | target containers only | 2m | n/a | no | yes |
| `docker_stop`, `docker_stack_down` | container-down | no | target containers only | 2m | n/a | no | no |

Constants in `models`:
- `ExpectContainerSettle` = 2m, `ExpectHostSettle` = 3m, `ExpectHostReturnBound` = 15m;
- `ExpectAgentSettle` = 1m, `ExpectAgentReturnBound` = 10m;
- `ExpectShutdownReturnBound` = 15m, `ExpectNonSuccessSettle` = 2m.

The DEC-2 values are unchanged.

**Notation.** D = `dispatched_at`; E = `deadline_at − D` (fallback `exec_deadline_s`); F = `finished_at`; P = the post-terminal period of the class (2m container, 15m host/shutdown, 10m agent); `graceEnd` = server start + 2m (AD-008).

**Hard bound (FR-005, NFR-002).** `hard = D + E + P`. No rule below ever yields a close later than `hard`.

**Phases (pure function `expect.Evaluate(w, dev, now, graceEnd) Eval`):**

| Command state | Phase | Covers offline | Covers containers | Closes at |
|---|---|---|---|---|
| `queued`, D = NULL | queued | no | no | never opens. Terminal with D = NULL: close outcome `not_opened`, no escalation |
| D set, active | in_progress | per class | per class | `hard` |
| `succeeded`, container class | settling | n/a | targets | `min(F + 2m, hard)` |
| `succeeded`, host or agent | return | yes | host: all; agent: none | the earliest of: the **early close** (below), `F + returnBound`, `hard` |
| `succeeded`, shutdown | return | yes | all | **powered off** when the device goes offline (AD-007); else `min(F + 15m, hard)` |
| `failed` or `timed_out`, any class | settling | **no**, from F (FR-004) | container and host: per class until `min(F + 2m, hard)`; agent: none | `min(F + 2m, hard)` (agent: F) |

**Early close (FR-003a).**
- Base: the device is online in the Tracker and `now − max(F, onlineSince, returnedAt*, graceEnd) ≥ settle`.
- `*returnedAt` counts only for `reboot` and `agent_update`. For these two, the early close also requires an **observed return after F**: an agent registration (AD-006) or an offline→online transition with a timestamp > F.
- Without an observed return, these windows run to the return bound. They close quietly there when the device is online.

**Coverage at time t.** A window covers t iff `D ≤ t < closeAt`, where closeAt is the early close if the loop already decided it, else the rule above. Container death: `CoverContainer` evaluates at the receive time. Offline: `CoverOffline` evaluates at `offlineAt = min(now, lastHeartbeat + 45s)` (AD-008), so an offline that happened during server downtime is judged at the time it happened.

**Boundary.** Coverage is strict at closeAt (AC-007: 10:01:50 covered, 10:02:10 not).

### AD-004: Commands hook — `commands.Options.Observer`

**Decision.**
- New optional interface `commands.CommandObserver { CommandChanged(c models.Command) }` in `commands.Options.Observer`.
- `Service.broadcast` calls it first, for every accepted transition including creation, even when `Broadcaster` is nil.
- The Tracker implementation copies the needed fields under its lock, ignores rows that have `WindowClosedAt != nil` or a non-scope action (except to drop a cached copy), and does a non-blocking send on a 1-buffered wake channel.
- It performs no I/O.

**Rationale.** It is the single choke point (Section 2.1). It gives FR-041 latency (≤ 1 s to the activity broadcast) and immediate FR-020 handling after a sweeper `timed_out`.

**Alternatives considered.** Polling only (30 s): too slow for FR-041. Rejected.

**Consequences.** The typed-nil rule of `Options` applies: server.go assigns the `*expect.Tracker` only when it is non-nil.

### AD-005: Suppression in the generator (FR-007 to FR-019, FR-011)

**Decision.** `events.Generator` gains `SetExpectedActivity(ExpectedActivity)`. The interface is defined in `package events`:

```
CoverContainer(deviceID string, keys []string, at time.Time) (models.Coverage, bool, error)
CoverOffline(ctx, deviceID string, offlineAt time.Time) (models.OfflineCoverage, bool, error)
NoteOnline(ctx, deviceID string, at time.Time) *models.OnlineNote
```

`CheckDockerEvent "die"`, new order:
1. `isContainerExcluded("container_died")` → return. This is today's excluded behavior and now applies inside a window too: FR-015, AC-010, deviation X-9.
2. Ask `CoverContainer(deviceID, evt.TargetKeys, now)`:
   - **covered** → store an `info` `container_died` event with `CommandID` = the covering command and `CommandAttr = "expected"`, with the FR-018 message; no cooldown read or write; no dispatch.
   - **not covered, or an error** → today's path, unchanged: the rule, its cooldown and notify, or the fallback `warning`.
3. The `activeUpdates` / `updateStarted` maps, their increments and decrements, and their prune are **deleted** (FR-014). `update_started`, `update_completed` and `update_failed` still store their events.

**Coverage selection (FR-007).**
- A container-class window whose `TargetKeys` intersect `evt.TargetKeys` is preferred.
- Otherwise a host-class window.
- Ties go to the latest D.
- Only windows of `deviceID` are searched (NFR-010, AC-045).

**`"oom"` (FR-011).** Never suppressed. If `CoverContainer` reports a covering window, the message becomes `Container <name> OOM killed (during <Action> <actor phrase>, command <id8>)`. Rule, severity and notify are unchanged.

**`DeviceOffline` → `DeviceOfflineAt(ctx, id, host, lastHeartbeat *time.Time)`.** `DeviceOffline` remains and delegates with nil.
- **Covered** (kind `window`) → `info` event linked to the command (`CommandAttr = "expected"`), FR-018 message, no cooldown, no notify.
- **Kind `powered_off`** → `info` event, `Device <host> powered off <actor phrase> at <HH:MM of F>` (FR-028), linked.
- **Not covered, or an error** → today's rule path, minus the old `recentDisruptiveCommand` downgrade (G6).

**`DeviceOnline`.**
- Calls `NoteOnline` **before** the 5-minute cooldown check, so the state transition always happens.
- When it returns a note, the message is `Device <host> came online after <Action> <actor phrase> (offline <d>)` or `(powered off <d>)` (FR-030, FR-044).
- `recentDisruptiveCommand` and `disruptiveActions` are deleted.

**`commandLabels`** gains:
- `shutdown` "Shutdown";
- `docker_stack_redeploy` "Docker stack redeploy", `docker_stack_restart` "Docker stack restart", `docker_stack_up` "Docker stack up", `docker_stack_down` "Docker stack down", `docker_stack_pull` "Docker stack pull";
- `docker_remove_items` "Docker remove items", `docker_inventory_refresh` "Docker inventory refresh", `agent_uninstall` "Agent uninstall".

**Alternatives considered.** Dropping suppressed events. This contradicts BR-002. Rejected.

### AD-006: Device state hooks

The Tracker learns the device state from four places:
1. `CoverOffline`, called by `DeviceOfflineAt`, sets `online=false, offlineSince=offlineAt`.
2. `NoteOnline`, called by `DeviceOnline` from `RegisterDevice` when the device was not online, sets `online=true, onlineSince=returnedAt=at`. It then **durably** deletes the powered-off row (`DeletePoweredOff`) and sets `cleared_at` on the device's open escalations (FR-030, FR-037). It returns the note with the offline or powered-off duration.
3. `NoteRegistration(deviceID, at)` is a new call in `RegisterDevice` (existing-device path, after the successful `Update`). It is called for **every** re-registration, including when `wasOnline`. It sets `returnedAt = at`, which records the agent restart for `reboot` and `agent_update` even when the device was never marked offline. It does no I/O.
4. `ObserveDevices(devices, at)` is called by the offline worker after each pass (AD-008):
   - it refreshes hostname and agent version;
   - a DB `online` where the Tracker had offline (a heartbeat return) applies the same transition as `NoteOnline`, without an event: today a heartbeat return produces no online event either.

**Initial state at `Load`:**
- DB-online devices: `online=true, onlineSince=returnedAt=start`. This is conservative: the early close cannot happen before `graceEnd + settle`.
- DB-offline devices: `online=false, offlineSince = lastHeartbeat + 45s`.

### AD-007: Powered-off state (FR-028 to FR-032)

**Decision.** Table `device_powered_off`: one row per device (PK). `CoverOffline` returns kind `powered_off` and upserts the row when:
- the device's latest **succeeded** `shutdown` window is in its return phase at `offlineAt`; or
- a powered-off row already exists (FR-029).

The 1 s loop also enters powered-off for a succeeded shutdown whose device the Tracker sees offline. This is the restart case. It uses `offlineSince`.

When entering powered-off, the loop closes the shutdown window with outcome `powered_off`. The row is deleted when the device comes online (AD-006 paths 2 and 4). A failed or timed-out shutdown never creates it (FR-031). A newer shutdown replaces the row (upsert, FR-032).

The row stores a snapshot of `command_id`, `action_label`, `actor_phrase`, `shutdown_at` (F) and `offline_at`, so the label survives the command's 90-day retention.

**Rationale.** It needs state that the command cannot hold, because it outlives the command. One row per device is the only unbounded-duration state (DEC-1, NFR-002).

**Fail open.** If the upsert fails, `CoverOffline` returns the error and the offline transition alerts (NFR-009).

### AD-008: Restart grace and durable offline time (FR-033, NFR-006)

**Decision.** `server.go` records `startedAt` (via an injectable clock). `checkOfflineDevices` moves into a testable `offlineDetector`:
- `check(ctx)` returns at once while `now < startedAt + models.OfflineDetectionGrace` (**2m**).
- After the grace, a device goes offline when `status == online` and `last_heartbeat < now − models.OfflineHeartbeatThreshold` (**45s**, today's literal, now a named constant). It is then `SetStatus(offline)`, followed by `EventGen.DeviceOfflineAt(ctx, id, host, lastHeartbeat)`, followed by `Expect.ObserveDevices(list, now)`.
- The worker keeps its 30 s ticker and adds a **one-shot timer at exactly `startedAt + grace`**, so detection runs at the grace end and not up to 30 s later.
- A device already `offline` in the DB has no transition, so no event fires (FR-033).

**Tracker side.** Before `graceEnd` the Tracker makes **no** decision that depends on the online state:
- no FR-020 or FR-021 escalations;
- no early close;
- no powered-off entry from the loop.

Suppression lookups work throughout the grace. Due escalations fire on the first loop tick at or after `graceEnd`: ≤ 2m + 1 s after start (deviation X-1).

**Rationale.** Heartbeats paused during downtime are not evidence of an outage (G10). Judging coverage at `lastHeartbeat + 45s` keeps an offline that happened during downtime inside the window it belonged to (NFR-006a).

### AD-009: Close processing and bounded lapse

**Decision.** Each 1 s tick (`Tracker.Tick`), outside the lock:
1. **FR-020.** For each host or agent window that has just reached `failed` or `timed_out`, once the grace is over: if the device is offline → escalate `device` (AD-010). Then the window continues as "settling" for containers.
2. **Due windows.**
   - Host or agent window at its return bound with the device offline → FR-021 escalate `device`.
   - Otherwise, if the policy has FR-022 and the device is online → the container check (AD-011).
   - Then `CloseWindow(id, now, outcome)`, guarded by `WHERE window_closed_at IS NULL`.
   - Then drop the window from memory.
   - Outcomes: `closed`, `escalated`, `powered_off`, `not_opened`, `lapsed`.
3. Drop open device escalations older than 24 h from memory (FR-037).
4. Recompute each device's activity (AD-014), and broadcast the devices whose activity changed.

**Per tick bound.** At most **50** close-or-escalation units per tick; the rest carry over. Every DB call uses a 5 s context.

**Failures.** A failed step leaves the window in memory, and the next tick retries.
- Escalations are idempotent (AD-010) and `CloseWindow` is guarded, so a retry or crash at any point never duplicates (FR-024).
- If a close keeps failing for more than 5 minutes after `closeAt`, the window is evicted from memory (NFR-004). Suppression has already ended, because coverage is computed. Reconcile reloads it, and it is retried.

**Reconcile (30 s).** `LapseStaleWindows(cutoff = now − 24h, limit 500)` sets `window_closed_at = now, window_outcome = 'lapsed'` on pending in-scope rows that are terminal with `finished_at < cutoff`, or that are terminal with D = NULL. Then `ListWindowCandidates(now, limit 1000)`.

**Rationale.** The lapse keeps the partial index `idx_commands_window_pending` bounded by recent activity, not by history.

### AD-010: Escalation: exactly once, through the existing rules (FR-020 to FR-027)

**Decision.**
1. The Tracker builds a `models.EscalationRequest`: kind (`device_nonsuccess`, `device_not_back`, `container`, `container_summary`), command snapshot, hostname, container name and count.
2. It calls `Escalator.PrepareEscalation(ctx, req)`, implemented by `events.Generator`:
   - it finds the matching enabled rule: `device_offline` with the device scope; `container_died` with the container scope (`findMatchingRuleForContainer`; "" for the summary);
   - it returns `skip=true` when `isContainerExcluded` (FR-015);
   - it builds the event: rule severity, or `warning` without a rule (FR-023, FR-026); `CommandID` set, `CommandAttr = "escalation"`, the AD-015 message.
3. It calls `Repo.RecordEscalation(ctx, marker, event)`. This is **one transaction**: `INSERT INTO command_escalations … ON CONFLICT (command_id, target) DO NOTHING RETURNING`; if a row was inserted, `INSERT INTO events …`; store `event_id` on the marker; commit. It returns `inserted`.
4. Only if `inserted` → `Escalator.PublishEscalation(ctx, evt, rule, hostname)`, which does the hub broadcast plus `dispatcher.Dispatch` when `rule.Notify`. It **neither checks nor starts** a rule cooldown (FR-024, AC-023, deviation X-8).
5. One `slog.Info("expected-activity escalation", command_id, device_id, kind)` per inserted escalation (NFR-005).

**Targets.** `device`, `container:<name>` (name sanitized, ≤ 128 bytes), `summary`.

**FR-025 cap.** Before the container escalations of a command, `ListEscalationTargets(commandID)` counts the existing `container:*` markers. New ones are emitted up to a total of 20. If any not-running containers remain beyond 20, one `summary` marker and event are emitted, naming the number remaining. At most 22 rows per command.

**Rationale.** The marker and the event commit together, so a crash cannot leave a marker without an event (lost escalation) or an event without a marker (duplicate after restart). The notification is sent after the commit, so a crash in the gap between them can lose that one push (residual R-3).

### AD-011: The FR-022 "still not running" check

At close, for a window with FR-022:
1. `SuppressedDeaths(deviceID, commandID, 1000)` gives, per container name, the time of the last `container_died` event with `command_id = X AND command_attr = 'expected'`. It uses `idx_events_command`.
2. "Observed running after the death" (taken from the latest suppressed death) is true if either holds:
   - `ContainerStartsSince(deviceID, D, 1000)` has a `container_started` event for that name later than the death (uses `idx_events_device_time`);
   - `TelemetryReader.GetLatestSnapshot(deviceID)` has a timestamp after the death, and that snapshot lists the container (name with the leading `/` trimmed) with state `running`.
3. Each container that is not observed running → a `container` escalation (AD-010), in ascending name order, under the FR-025 cap.

Skipped:
- for `docker_stop`, `docker_stack_down` and `shutdown` (BR-007);
- for `agent_update` (it covers no container);
- when the device is offline at the close: then only FR-021 applies (FRD 7.3).

### AD-012: Bounds (Ryan's standing rule: every growth path stated)

| Thing | Bound | Cleanup |
|---|---|---|
| `commands.window_closed_at` and `window_outcome` | On existing rows; no new rows | Deleted with the command (90 d or the budget) |
| `idx_commands_window_pending` | In-scope rows with `window_closed_at IS NULL`: open windows, plus queued in-scope commands (≤ the 60m TTL), plus at most 24 h of unprocessed rows | AD-009 close and lapse (500 per 30 s) |
| `command_escalations` | ≤ 22 rows per command (1 `device`, ≤ 20 `container:*`, 1 `summary`); PK `(command_id, target)` | `ON DELETE CASCADE` from `commands` |
| `device_powered_off` | ≤ 1 row per device (PK) | Deleted on online; `ON DELETE CASCADE` from `devices` |
| `idx_events_command` (partial, `command_id <> ''`) | Linked events only | Event retention 90 d (unchanged) |
| Events | Suppressed events **replace** today's warning events, 1:1 per occurrence. Escalations ≤ 22 per command | 90 d |
| Tracker memory | Windows: tracked in-scope commands with no `window_closed_at`, capped at **200 per device**. Past the cap the oldest *queued* entry is dropped first, then the oldest window (fail open). Plus one device entry per registered device (state, ≤ 1 powered-off, ≤ 1 open escalation). Policy-target map ≤ the number of policies (rebuilt each reconcile). Activity cache ≤ 1 per device. **No per-event state** | A closed window is dropped on the same tick; at the latest 5 min after `closeAt` (AD-009). A deleted device is dropped on the next reconcile |
| Logs | Suppression: `Debug` only. Escalation: 1 `Info`. Evaluation errors: `Warn` at most once per minute per error kind (Tracker throttle), plus the global `logbudget` | n/a |
| Removed state | `Generator.activeUpdates` and `updateStarted` maps | n/a |

### AD-013: Clock injection

- `expect.Options.Now`.
- `events.Generator.SetClock(func() time.Time)`, default `time.Now`. It is used by every code path this story adds or modifies: the die, oom, offline, online and escalation paths. `onCooldown` keeps `time.Since`.
- `offlineDetector.now`.

No test sleeps for real time (NFR-012).

### AD-014: Server-computed activity (FR-034 to FR-043)

**Decision.**
- `Tracker.Activities()` returns `models.ActivityResponse` (Section 6.1).
- The dashboard gets it from `GET /api/v1/activity` and from WS `activity_update` messages. One is sent per changed device within ≤ 1 s of the change, because the loop is woken by the hook.
- The web builds the label text in a pure function, `activityLabel(a, newestAgentVersion, deviceAgentVersion)`. The browser formats times in its own time zone.

**Selection per device**, in priority order:
1. a host or agent window in `in_progress` or `return` (latest D);
2. an open device escalation (< 24 h, not cleared);
3. the powered-off state;
4. a queued host or agent command;
5. container-class windows open (`in_progress` or `settling`): one gives `docker` with its target; several give `docker` with `docker_actions = N`.

A device with none of these has no entry.

**Rationale.** FR-040 asks for the server's view. Time zones belong in the browser.

### AD-015: Messages and the actor phrase (FR-018, NFR-010)

`models.ActorPhrase(cmd, policyTarget string)`:
- `user` → `by <actor_name>`; `by an operator` when the name is empty (deviation X-2);
- `auto_update` → `by auto-update policy for <target>`; `by auto-update policy #<id>` when the target cannot be resolved; `by an auto-update policy` when `policy_id` is NULL;
- `auto_patch` → `by the auto-patch schedule`.

`models.SafeName(s)` = `SanitizeText`, then remove every C0/C1 control rune and U+007F, then `TruncateHead(…,128)`. It is applied to container names, actor names, policy targets, hostnames and stack or target labels before they enter any event message, label DTO or marker target.

| Use | Template (`<id8>` = the first 8 characters of the command ID; times `15:04` in the server's local zone) |
|---|---|
| Suppressed death | `Container <name> died during <Action>[ of <target>] <actor phrase> (expected)`. ` of <target>` only when the command's target label differs from the container name (stack or bulk) |
| Suppressed offline | `Device <host> went offline during <Action> <actor phrase> (expected[, back by <HH:MM>])`. "back by" in the return phase = F + returnBound |
| Powered off | `Device <host> powered off <actor phrase> at <HH:MM of F>` |
| Online after window | `Device <host> came online after <Action> <actor phrase> (offline <dur>)` |
| Online after powered off | `Device <host> came online after Shutdown <actor phrase> (powered off <dur>)` |
| OOM inside a window | `Container <name> OOM killed (during <Action> <actor phrase>, command <id8>)` |
| FR-020 | `<host> is offline: <Action> <actor phrase> <failed\|timed out> (command <id8>)` |
| FR-021 | `<host> still offline <bound> after <Action> <actor phrase> (command <id8>)`. bound = `15m` or `10m` |
| FR-022 | `Container <name> still not running <p> after <Action> <actor phrase> (<outcome>, command <id8>)`. p = `2m` (container and non-success), `3m` (host early close), `15m` (host bound) |
| FR-025 summary | `<N> more containers still not running after <Action> <actor phrase> (<outcome>, command <id8>)` |

`<dur>` = `models.FormatDurationShort`. `<outcome>` = `succeeded`, `failed`, `timed out`.

### AD-016: Fleet dispatches carry the operator

`BulkUpdateAgents` and `BulkPatchDevices` build their requests with the existing `dockerDispatchRequest(r, device, action, params)`, which sets `ActorKind`, `ActorAuth`, `ActorSub` and `ActorName` from the JWT claims. Nothing else changes. This is required for AC-024 ("by ryan").

---

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `cmd/riot-server/migrations/000033_expected_activity.up.sql` | Section 5 |
| CREATE | `cmd/riot-server/migrations/000033_expected_activity.down.sql` | Section 5 |
| CREATE | `internal/models/expect.go` | `WindowClass`, `WindowPolicy`, `ExpectPolicy(action)`, `ExpectActions()` (the 12 actions in fixed order), the AD-003 and AD-008 constants, `ExpectStaleAfter`, `ExpectContainerEscalationCap` = 20, `ExpectEscalationDisplayTTL` = 24h, `StepWaitDpkgLock` = `"wait_dpkg_lock"`, `WindowOutcome` values, `Coverage`, `OfflineCoverage`, `OnlineNote`, `EscalationRequest`, `Escalation`, `PoweredOffState`, `ContainerDeath`, `DeviceActivity`, `ActivityResponse`, `ActorPhrase`, `SafeName`, `ShortCommandID` |
| CREATE | `internal/models/expect_test.go` | Policy table, actor phrase, `SafeName` |
| MODIFY | `internal/models/commands.go` | `Command.WindowClosedAt *time.Time` (`json:"-"`) |
| MODIFY | `internal/models/events.go` | `DockerEvent.TargetKeys []string` (`json:"-"`); constants `CommandAttrExpected = "expected"`, `CommandAttrEscalation = "escalation"` |
| MODIFY | `internal/server/db/command_repo.go` | Add `window_closed_at` to `commandColumns` and `scanCommand` (the INSERT is unchanged) |
| CREATE | `internal/server/db/expect_repo.go` | `ExpectRepo` implementing `ExpectedActivityRepository` (Section 6.3). The in-scope action list in SQL is rendered from `models.ExpectActions()` (typed constants only, as `SQLStatusList`) and must equal the migration predicate |
| CREATE | `internal/server/db/expect_repo_pg_test.go` | PG tests (skipped without a DB, as the existing `*_pg_test.go`) |
| MODIFY | `internal/server/db/interfaces.go` | `ExpectedActivityRepository` interface, plus a compile-time assertion for `*ExpectRepo` |
| MODIFY | `internal/testutil/mocks.go` | `MockExpectedActivityRepo`: in-memory; built with an optional `*MockEventRepo` and `*MockCommandRepo` so that `SuppressedDeaths`, `ContainerStartsSince`, `RecordEscalation` and `ListWindowCandidates` read and write the same state; `FailOn(method, err)`. `MockCommandRepo` copies `WindowClosedAt` and gains `SetWindowClosed` |
| CREATE | `internal/server/expect/tracker.go` | `Tracker`, `Options`, `New`, `Load`, `Reconcile`, `Run`, `Tick`, `CommandChanged`, `CoverContainer`, `CoverOffline`, `NoteOnline`, `NoteRegistration`, `ObserveDevices`, `Activities`, `WindowCount` (test accessor) |
| CREATE | `internal/server/expect/window.go` | The pure `Evaluate` (AD-003) and coverage selection (AD-005) |
| CREATE | `internal/server/expect/escalate.go` | The AD-009, AD-010 and AD-011 orchestration; the `Escalator`, `Broadcaster`, `DeviceLister`, `PolicyLister` and `TelemetryReader` interfaces |
| CREATE | `internal/server/expect/activity.go` | The AD-014 selection and DTO building |
| CREATE | `internal/server/expect/messages.go` | The AD-015 templates (the generator and the escalator call these, not their own `Sprintf`) |
| CREATE | `internal/server/expect/*_test.go` | Section 8 (package `expect` for the pure parts; package `expect_test` for the harness with a real `events.Generator`) |
| MODIFY | `internal/server/events/generator.go` | AD-005, AD-010 (`PrepareEscalation`, `PublishEscalation`), AD-013. Remove `activeUpdates`, `updateStarted`, their prune, `recentDisruptiveCommand` and `disruptiveActions`. Add `DeviceOfflineAt`. Extend `commandLabels` |
| CREATE | `internal/server/events/generator_expect_test.go` | Generator tests with a fake `ExpectedActivity` |
| MODIFY | `internal/server/events/generator_integration_test.go` | Retire `TestCheckDockerEvent_DieDowngradedDuringUpdate` and `TestCheckDockerEvent_DieNoNotifyDuringUpdate`: they assert the per-device downgrade that FR-014 removes, and are replaced by AC-005 and AC-006 (deviation X-13). No other edits |
| MODIFY | `internal/server/commands/service.go` | `CommandObserver`, `Options.Observer`, the call in `broadcast` (AD-004) |
| CREATE | `internal/server/commands/observer_test.go` | The hook fires once per accepted transition, never on a rejected one |
| MODIFY | `internal/server/websocket/hub.go` | `BroadcastActivity(deviceID string, a *models.DeviceActivity)` → `{"type":"activity_update","device_id":…,"data":a or null}` |
| MODIFY | `internal/server/server.go` | `Expect *expect.Tracker`, `ExpectRepo`, `startedAt`, clock; the wiring order (Section 12 note 1); `offlineDetector` with grace and a one-shot; `go s.Expect.Run(ctx)` |
| CREATE | `internal/server/offline.go` | `offlineDetector` (moved from `checkOfflineDevices`) |
| CREATE | `internal/server/offline_test.go` | AC-035 |
| MODIFY | `internal/server/router.go` | `r.Get("/api/v1/activity", h.Activity)` in the dashboard-authenticated group, next to `/api/v1/fleet/*`; `HandlerDeps.Activity` |
| MODIFY | `internal/server/handlers/handlers.go` | `HandlerDeps.Activity ActivitySource`; `RegisterDevice` calls `NoteRegistration` (AD-006); `ReceiveDockerEvent` is unchanged apart from the keys being filled before `CheckDockerEvent` |
| CREATE | `internal/server/handlers/activity.go` | The `ActivitySource` interface (`Activities() models.ActivityResponse`, `NoteRegistration(string, time.Time)`); `Activity` handler (200 JSON; 503 `{"error":"activity unavailable","code":"activity_unavailable"}` when the source is nil) |
| CREATE | `internal/server/handlers/activity_test.go` | The handler contract |
| MODIFY | `internal/server/handlers/docker_attribution.go` | Compute the keys **before** the `commandRepo == nil` return, and store them in `e.TargetKeys`: the name key, the compose key, plus `docker:id:<full id>` and `docker:id:<12-char prefix>`. Attribution itself is unchanged |
| CREATE | `internal/server/handlers/docker_attribution_keys_test.go` | Keys for standalone, compose and ID |
| MODIFY | `internal/server/handlers/fleet.go` | AD-016 |
| CREATE | `internal/server/handlers/fleet_actor_test.go` | The actor is recorded on fleet agent update and fleet patch |
| CREATE | `internal/server/expect/safety_test.go` | AC-047 static checks |
| MODIFY | `web/src/types/models.ts` | `DeviceActivity`, `ActivityResponse`, `ActivityKind`, `ActivityPhase` |
| MODIFY | `web/src/api/client.ts` | `getActivity(): Promise<ActivityResponse>` (`GET /api/v1/activity`) |
| CREATE | `web/src/hooks/useActivity.ts` | react-query key `['activity']`, no polling while the WS is connected, 30 s fallback, refetch on WS reconnect; returns `Map<deviceId, DeviceActivity>` and `newestAgentVersion` |
| MODIFY | `web/src/hooks/useDevices.ts` | Handle `activity_update`: upsert or remove in `['activity']` |
| CREATE | `web/src/utils/activity.ts` | `activityLabel(...)`, `activityTone(...)` (`'progress'`, `'failure'`, `'off'`), `isExpectedOffline(device, activity)`, `docker verb` map; labels per Section 6.2 |
| CREATE | `web/src/utils/activity.test.ts` | AC-038 vocabulary |
| CREATE | `web/src/components/ActivityLabel.tsx` | The label, plus the actor phrase and a `<RelativeTime>` start; failure styling for `escalated`; optional `link` |
| MODIFY | `web/src/pages/FleetOverview.tsx` | The label under the hostname; the dot `title` = label; the expected-offline dot is a hollow sky ring (`ring-2 ring-sky-400 bg-transparent`), distinct from both green and red (FR-043); a new StatCard "Expected offline"; the offline count excludes expected-offline devices (FR-038); the grid becomes `sm:grid-cols-6`. The patch badges and the "Update Agents" button are untouched |
| CREATE | `web/src/pages/FleetOverview.activity.test.tsx` | AC-024, AC-039, AC-040 |
| MODIFY | `web/src/pages/DeviceDetail.tsx` | An `ActivityLabel` banner directly under `PageHeader`, with the link (FR-039, deviation X-3) |
| CREATE | `web/src/pages/DeviceDetail.activity.test.tsx` | AC-042 |
| MODIFY | `web/src/components/docker/CommandProgress.tsx` | Prop `showActivityLink?: boolean` (default `true`). The host-command modal passes `false`, because the Docker activity page rejects non-Docker commands |
| MODIFY | `web/src/hooks/useDevices.test.ts` | Add the `activity_update` cases (AC-040). No existing case changes |
| MODIFY | `README.md`, `CHANGELOG.md` | FR-045, FR-046 (technical writer stage) |
| CREATE | `internal/server/expect_docs_test.go` | AC-048 (technical writer stage) |

No agent file, `install.sh`, sudoers or dependency change.

---

## 5. Data Model Changes

### 5.1 `000033_expected_activity.up.sql`

```
ALTER TABLE commands
  ADD COLUMN window_closed_at TIMESTAMPTZ,
  ADD COLUMN window_outcome  TEXT NOT NULL DEFAULT '';

-- History older than one hour never opens a window. Rows finished in the last
-- hour, and all active rows, are processed by the Tracker (deviation X-12).
UPDATE commands
   SET window_closed_at = COALESCE(finished_at, updated_at), window_outcome = 'lapsed'
 WHERE status IN ('succeeded','failed','timed_out','cancelled','expired','success','error')
   AND COALESCE(finished_at, updated_at) < now() - INTERVAL '1 hour';

CREATE INDEX idx_commands_window_pending ON commands (device_id, created_at)
  WHERE window_closed_at IS NULL
    AND action IN ('reboot','shutdown','os_update','agent_update',
                   'docker_update','docker_bulk_update','docker_restart','docker_stop',
                   'docker_stack_redeploy','docker_stack_restart','docker_stack_up','docker_stack_down');

CREATE INDEX idx_events_command ON events (command_id, created_at) WHERE command_id <> '';

CREATE TABLE command_escalations (
  command_id  TEXT        NOT NULL REFERENCES commands(id) ON DELETE CASCADE,
  target      TEXT        NOT NULL,          -- 'device' | 'container:<name>' | 'summary'
  device_id   TEXT        NOT NULL,
  event_id    BIGINT,
  created_at  TIMESTAMPTZ NOT NULL,
  cleared_at  TIMESTAMPTZ,                   -- device target only: set when the device next reports online
  PRIMARY KEY (command_id, target),
  CONSTRAINT command_escalations_target_len CHECK (octet_length(target) <= 160)
);
CREATE INDEX idx_command_escalations_device_open ON command_escalations (device_id, created_at DESC)
  WHERE target = 'device' AND cleared_at IS NULL;

CREATE TABLE device_powered_off (
  device_id     TEXT        PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
  command_id    TEXT        NOT NULL,      -- no FK: survives the command's retention
  action_label  TEXT        NOT NULL,
  actor_phrase  TEXT        NOT NULL,
  shutdown_at   TIMESTAMPTZ NOT NULL,      -- terminal success time (label "at <time>")
  offline_at    TIMESTAMPTZ NOT NULL,      -- offline transition (duration in the online message)
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

The migration runs in golang-migrate's transaction, like 000032. The `events` index is a plain `CREATE INDEX`, as 000032 did. A test asserts that the predicate's action list equals `models.ExpectActions()`.

### 5.2 `000033_expected_activity.down.sql`

```
DROP TABLE IF EXISTS device_powered_off;
DROP TABLE IF EXISTS command_escalations;
DROP INDEX IF EXISTS idx_events_command;
DROP INDEX IF EXISTS idx_commands_window_pending;
ALTER TABLE commands DROP COLUMN IF EXISTS window_outcome, DROP COLUMN IF EXISTS window_closed_at;
```

### 5.3 Before / after: `commands`

Before: the columns of migrations 000001, 000023 and 000032. After: the same, plus `window_closed_at TIMESTAMPTZ NULL` and `window_outcome TEXT NOT NULL DEFAULT ''`. Nothing is dropped. Existing queries are unaffected. `commandColumns` selects the new nullable column.

### 5.4 `events`

No column change. New `command_attr` values: `expected` (suppressed occurrence) and `escalation`.

---

## 6. API / Interface Contract

### 6.1 `GET /api/v1/activity`

- **Auth:** the dashboard session (the same middleware group as `/api/v1/fleet/*`). Agent keys are not accepted.
- **Request:** no parameters, no body.
- **200** `application/json`:

```
{
  "generated_at": "2026-10-26T14:02:05Z",
  "newest_agent_version": "2.78.0",            // "" when unknown
  "activities": [ DeviceActivity, ... ]          // only devices that have an activity; at most one per device
}
```

`DeviceActivity`:

| Field | Type | Meaning |
|---|---|---|
| `device_id` | string | |
| `kind` | `"host"` \| `"agent"` \| `"docker"` \| `"powered_off"` \| `"escalated"` | AD-014 selection |
| `phase` | `"queued"` \| `"in_progress"` \| `"return"` \| `"settling"` \| `"powered_off"` \| `"escalated"` | |
| `action` | string | Canonical action (`shutdown` for powered off; the failed command's action for escalated) |
| `command_id` | string | |
| `actor_phrase` | string | AD-015, sanitized |
| `started_at` | RFC 3339 | D (`created_at` when queued; `shutdown_at` for powered off; the escalation time for escalated) |
| `back_by` | RFC 3339 \| null | Return phase: F + returnBound. Settling os_update: the early-close time |
| `waiting_dpkg_lock` | bool | `step == "wait_dpkg_lock"` (A-007: v2.78.0 agents never send it) |
| `progress` | int \| null | os_update progress 0..100 |
| `agent_from_version` | string | The device's current `agent_version` (unchanged until the agent restarts) |
| `target` | string | Container name, stack project, or `"<N> containers"` (bulk); sanitized ≤ 128 bytes |
| `docker_actions` | int | Number of open container-class windows (kind `docker`) |
| `covers_offline` | bool | true when this activity would suppress `device_offline` now: a host, agent or shutdown window in `in_progress` or `return`, or powered off. false for escalated, queued and docker |
| `device_online` | bool | The Tracker's view of the device |
| `escalation` | null \| `{"reason":"failed"\|"timed_out"\|"not_back","bound":"15m"\|"10m"\|"","at":RFC 3339}` | |
| `powered_off_at` | RFC 3339 \| null | `shutdown_at` |

- **503** `{"error":"activity unavailable","code":"activity_unavailable"}` when the Tracker is not wired. The web treats it as "no activity" and keeps today's display.
- **401**: the existing middleware.

### 6.2 Labels (web, `activityLabel`; times `HH:MM` in the browser locale)

| kind / phase / condition | Label |
|---|---|
| host or agent, `queued` | `<Action> queued` (Reboot queued, Shutdown queued, OS update queued, Agent update queued) |
| `reboot` in_progress | `Rebooting`; `Rebooting (waiting for dpkg lock)` if `waiting_dpkg_lock` |
| `reboot` return | `Rebooting, back by <back_by>` |
| `shutdown` in_progress | `Shutting down`; `Shutting down (waiting for dpkg lock)` |
| `shutdown` return (not yet offline) | `Shutting down` (X-4) |
| `powered_off` | `Powered off <actor_phrase> at <powered_off_at>` |
| `os_update` in_progress | `Patching`; `Patching (<progress>%)` when there is progress |
| `os_update` return, device offline | `Rebooting after patch, back by <back_by>` |
| `os_update` return, device online | `Patched, settling until <back_by>` (X-4) |
| `agent_update` in_progress | `Updating agent <from> → <to>`; `Updating agent` if either is empty |
| `agent_update` return | `Restarting agent, back by <back_by>` (online or offline, X-4) |
| docker, 1 action | `Docker: <verb> <target>`. Verbs: update `updating`, bulk `updating`, restart `restarting`, stop `stopping`, stack_redeploy `recreating stack`, stack_restart `restarting stack`, stack_up `starting stack`, stack_down `stopping stack` |
| docker, N ≥ 2 | `Docker: <N> actions in progress` |
| escalated, reason failed or timed_out | `<Action> failed: offline` (failure styling) |
| escalated, not_back | `Not back <bound> after <Action>` (failure styling) |

Next to the label (FR-036), except for `powered_off` (the phrase is already in the label): `<actor_phrase> · started <RelativeTime started_at>`.

### 6.3 Repository interface (`db.ExpectedActivityRepository`)

```
ListWindowCandidates(ctx, now time.Time, limit int) ([]models.Command, error)
    // window_closed_at IS NULL AND action IN (ExpectActions) AND (status active OR finished_at >= now-24h), ORDER BY created_at
LapseStaleWindows(ctx, cutoff, now time.Time, limit int) (int64, error)
CloseWindow(ctx, commandID string, at time.Time, outcome models.WindowOutcome) (applied bool, err error)
RecordEscalation(ctx, esc *models.Escalation, evt *models.Event) (inserted bool, err error)   // one tx, AD-010
ListEscalationTargets(ctx, commandID string) ([]string, error)
ListOpenDeviceEscalations(ctx, since time.Time) ([]models.Escalation, error)
ClearDeviceEscalations(ctx, deviceID string, at time.Time) (int64, error)
SuppressedDeaths(ctx, deviceID, commandID string, limit int) ([]models.ContainerDeath, error)
ContainerStartsSince(ctx, deviceID string, since time.Time, limit int) (map[string]time.Time, error)
UpsertPoweredOff(ctx, p *models.PoweredOffState) error
DeletePoweredOff(ctx, deviceID string) (*models.PoweredOffState, error)          // nil, nil when no row
ListPoweredOff(ctx) ([]models.PoweredOffState, error)
```

All values are bind parameters. The only literal lists are the typed action and status constants.

### 6.4 WebSocket (dashboard)

`{"type":"activity_update","device_id":"<id>","data":DeviceActivity}`, or `"data":null` when the device's activity cleared. It is sent only on change.

### 6.5 Go interfaces (cross-package)

- `events.ExpectedActivity` (AD-005), implemented by `*expect.Tracker`.
- `expect.Escalator` (`PrepareEscalation(ctx, models.EscalationRequest) (*models.Event, *models.AlertRule, bool)`, `PublishEscalation(ctx, *models.Event, *models.AlertRule, hostname string)`), implemented by `*events.Generator`.
- `commands.CommandObserver`, `expect.Broadcaster`, `handlers.ActivitySource`.

There is no import cycle: `expect` imports neither `events` nor `commands`; `events` does not import `expect`.

---

## 7. Sequence / Flow

### 7.1 Container death

1. `POST /devices/{id}/docker-event` → `dockerEventAllowed` (unchanged).
2. `validateDockerEventCommand` fills `TargetKeys`, then performs the existing attribution.
3. `CheckDockerEvent "die"`:
   - excluded? → return;
   - `CoverContainer(id, keys, now)` (in memory) → covered: `info` event, `CommandAttr = expected`, broadcast, no dispatch;
   - error or not covered → today's rule path.
4. Response 200.

### 7.2 Offline transition

1. 30 s tick or the grace one-shot → `offlineDetector.check`. During the grace: return.
2. For each stale device: `SetStatus(offline)` → `DeviceOfflineAt(lastHeartbeat)` → `CoverOffline(offlineAt)`:
   - window → `info`;
   - powered off → upsert the row, `info`;
   - none or error → the rule path.
3. `ObserveDevices`.

### 7.3 Command transition

`Service.*` → `repo.Transition` → `broadcast` → `Observer.CommandChanged` (cache plus wake) → the loop tick ≤ 1 s:
- FR-020 when it applies;
- activity diff → `BroadcastActivity`.

### 7.4 Window close (loop)

`Evaluate` says closed → (FR-021 or FR-022 via `PrepareEscalation` → `RecordEscalation` (tx) → `PublishEscalation`)* → `CloseWindow` → drop from memory → activity broadcast.

### 7.5 Agent comes back

`RegisterDevice` → `Update` → `NoteRegistration` → if it was not online: `DeviceOnline` → `NoteOnline` (clear powered off and escalations, return the note) → online event with the action text.

### 7.6 Server start

1. Migrations.
2. Repos.
3. Hub.
4. Generator.
5. `ExpectRepo`, then `Tracker = expect.New(…)`.
6. `EventGen.SetExpectedActivity(Tracker)`.
7. `commands.New(… Observer: Tracker)`.
8. `Tracker.Load(ctx)`. A failure is logged and the Tracker stays not-ready (fail open); `Run` retries every 30 s.
9. `go Tracker.Run`.
10. `go offlineDetector` (grace).
11. Router (`HandlerDeps.Activity = Tracker`).

At `graceEnd` the one-shot detection runs, and the next loop tick processes escalations that became due during the downtime (AC-032).

---

## 8. Acceptance Criteria Mapping

Test files:
- **H** = `internal/server/expect/harness_test.go`: package `expect_test`; a real `events.Generator` plus `Tracker`, with `testutil` mocks, a fake clock, a fake broadcaster and a recording dispatcher. `harness.restart()` builds a new Generator and Tracker over the same mocks.
- **G** = `internal/server/events/generator_expect_test.go`.
- **W** = `internal/server/expect/window_test.go`.
- **E** = `internal/server/expect/escalate_test.go`.
- **A** = `internal/server/expect/activity_test.go`.
- Go test names are prefixed `TestEX_`. Vitest `describe('[AC-0xx] …')`.

| AC | Fulfilled by | Test (file: name) |
|---|---|---|
| AC-001 | `CheckDockerEvent` die + `CoverContainer` + messages | H: `TestEX_AC001_DockerUpdateDeathRecordedNotAlerted` |
| AC-002 | `ActorPhrase` auto_update + policy map | H: `TestEX_AC002_ScheduledUpdateNamesPolicy`; models: `TestEX_AC002_ActorPhraseOrigins` |
| AC-003 | Policy table: container class targets (restart, stop, stack × 4) | H: `TestEX_AC003_RestartStopAndStackActionsCovered` (table, 6 sub-tests) |
| AC-004 | `Evaluate` hard bound uses `deadline_at` (bulk 4h45m) | W: `TestEX_AC004_BulkWindowCoversThreeHours`; H: `TestEX_AC004_BulkDeathAtThreeHoursSuppressed` |
| AC-005 | Coverage by target keys only | H: `TestEX_AC005_NonTargetContainerAlerts` |
| AC-006 | No dependency on `update_started` | H: `TestEX_AC006_DeathBeforeUpdateStartedSuppressed`, `TestEX_AC006_NoUpdateStartedSuppressed` |
| AC-007 | Strict `F+2m` boundary | W: `TestEX_AC007_ContainerSettleBoundary`; H: `TestEX_AC007_DeathAfterSettleAlerts` |
| AC-008 | `ExpectPolicy` returns !ok for `docker_start` and `docker_stack_pull` | H: `TestEX_AC008_ActionsOutsideTableAlert`; models: `TestEX_AC008_PolicyTableExact` |
| AC-009 | OOM path | H: `TestEX_AC009_OOMAlertsAndNamesAction` |
| AC-010 | Exclusion first | H: `TestEX_AC010_ExcludedContainerSameInsideAndOutside` |
| AC-011 | Host window across the dpkg wait; `NoteOnline` duration | H: `TestEX_AC011_RebootAfterDpkgWaitNoAlert` |
| AC-012 | Host covers all containers | H: `TestEX_AC012_TwelveDeathsDuringReboot` |
| AC-013 | os_update return phase, auto_patch phrase | H: `TestEX_AC013_PatchWithRebootNoAlert` |
| AC-014 | os_update covers deaths; FR-022 sees the starts | H: `TestEX_AC014_DockerRestartDuringPatchNoEscalation` |
| AC-015 | FR-021 at F+15m, once | H: `TestEX_AC015_NotBackByBoundEscalatesOnce` |
| AC-016 | FR-020 after the sweeper `timed_out` | H: `TestEX_AC016_TimedOutRebootOfflineEscalates` (drives a real `commands.Service.Sweep` with the observer) |
| AC-017 | Non-success stops offline coverage | H: `TestEX_AC017_OfflineAfterFailedRebootAlerts` |
| AC-018 | FR-022 host close | H: `TestEX_AC018_ContainerNotBackAfterRebootEscalates` |
| AC-019 | FR-022 container close; message | H: `TestEX_AC019_StackRedeployContainerDownEscalates` |
| AC-020 | stop and stack_down: no FR-022 | H: `TestEX_AC020_IntentionalDownNoEscalation` |
| AC-021 | FR-025 cap | E: `TestEX_AC021_TwentyPlusSummary`; E: `TestEX_AC021_CapHoldsAcrossRestart` |
| AC-022 | No rule → warning, no dispatch | H: `TestEX_AC022_EscalationWithoutRuleIsWarning` |
| AC-023 | `PublishEscalation` ignores cooldown | G: `TestEX_AC023_EscalationIgnoresCooldown` |
| AC-024 | AD-016 + activity DTO + label | handlers: `TestEX_AC024_FleetAgentUpdateRecordsActor`; A: `TestEX_AC024_AgentActivityVersions`; web: `[AC-024] fleet shows Updating agent with actor in a fresh render` |
| AC-025 | agent return phase | H: `TestEX_AC025_AgentRestartOfflineInfo`; A: `TestEX_AC025_RestartingAgentBackBy` |
| AC-026 | FR-021 agent bound 10m | H: `TestEX_AC026_AgentNotBackEscalates`; A: `TestEX_AC026_EscalatedActivity` |
| AC-027 | agent covers no container | H: `TestEX_AC027_AgentUpdateDoesNotHideDeaths` |
| AC-028 | Powered off | H: `TestEX_AC028_ShutdownPoweredOffNoAlertsThreeDays`; A: `TestEX_AC028_PoweredOffActivity`; web: `[AC-028] powered off counted as expected offline` (in the AC-039 file) |
| AC-029 | `NoteOnline` clears | H: `TestEX_AC029_PoweredOffEndsOnOnline` |
| AC-030 | Failed shutdown | H: `TestEX_AC030_FailedShutdownNoPoweredOff` |
| AC-031 | `Load` from the mocks | H: `TestEX_AC031_WindowsSurviveRestart` |
| AC-032 | Markers + grace | H: `TestEX_AC032_DowntimeEscalationOnceAcrossRestarts` |
| AC-033 | Hard bounds for all 12 actions | W: `TestEX_AC033_EveryWindowHasHardBound` (table); H: `TestEX_AC033_SilentCommandsCloseAndAlertingResumes` |
| AC-034 | Queued: no coverage; label | H: `TestEX_AC034_QueuedRebootDoesNotSuppress`; A: `TestEX_AC034_RebootQueuedActivity` |
| AC-035 | `offlineDetector` grace + one-shot | `internal/server/offline_test.go`: `TestEX_AC035_RestartGraceNoOffline`, `TestEX_AC035_OfflineAfterGrace` |
| AC-036 | Fail open: not loaded, stale | H: `TestEX_AC036_FailOpenWhenNotLoaded`, `TestEX_AC036_FailOpenWhenStale` |
| AC-037 | Bounds | H: `TestEX_AC037_RebootWindowEventCountsLogsMemory` (counts events; captures slog with a test handler; asserts `WindowCount()==0` after close + 5m) |
| AC-038 | Labels | A: `TestEX_AC038_SelectionPriorityHostOverDocker`; web `activity.test.ts`: `[AC-038] label vocabulary` (one `it` per row of 6.2) |
| AC-039 | Counts and dot | web `FleetOverview.activity.test.tsx`: `[AC-039] offline and expected-offline counts` |
| AC-040 | Observer → loop → broadcast; WS cache | E: `TestEX_AC040_DispatchAndCloseBroadcastWithinOneTick`; web `useDevices.test.ts`: `[AC-040] activity_update upserts and clears` |
| AC-041 | 24 h / online clear | A: `TestEX_AC041_EscalatedClearsAfter24h`, `TestEX_AC041_EscalatedClearsOnOnline` |
| AC-042 | Device header banner and link | web `DeviceDetail.activity.test.tsx`: `[AC-042] header shows label, actor, start and link` |
| AC-043 | No change to the badges or the button | The existing `FleetOverview.patchflow.test.tsx`, `DeviceDetail.patchflow.test.tsx` and `FleetOverview.test.tsx` pass unmodified (QA diff check) |
| AC-044 | Keys from the name; no agent ID | handlers: `TestEX_AC044_KeysWithoutAgentCommandID`; H: `TestEX_AC044_OlderAgentRestartSuppressed` |
| AC-045 | Same device only | H: `TestEX_AC045_CrossDeviceCommandIDIgnored` |
| AC-046 | `SafeName` | models: `TestEX_AC046_SafeName`; H: `TestEX_AC046_SuppressedAndEscalatedMessagesSanitized` |
| AC-047 | Static safety | `internal/server/expect/safety_test.go`: `TestEX_AC047_NoPowerOrAgentExecution` (AST scan, Section 12 note 9) |
| AC-048 | README and CHANGELOG | `internal/server/expect_docs_test.go`: `TestEX_AC048_ReadmeAndChangelog` (technical writer stage) |

Supporting tests, not tied to an AC:
- `TestEX_Observer_FiresOnAcceptedTransitionsOnly`;
- `TestEX_Repo_PG_*`: the migration up and down, the predicate equals `ExpectActions()`, `RecordEscalation` exactly once under concurrency, cascade deletes, lapse;
- `TestEX_LateResultAfterTimeoutDoesNotReopen`;
- `TestEX_Tracker_CapPerDevice`.

---

## 9. Error Handling

| Failure | Behavior | Log |
|---|---|---|
| `Load` fails at start | Tracker not ready → every `Cover*` returns an error → alerts as today (NFR-009). `Run` retries `Reconcile` every 30 s; the first success makes it ready | Warn (throttled 1/min) |
| Reconcile fails | The cache keeps serving until 90 s after the last success, then fails open | Warn (throttled) |
| `UpsertPoweredOff` fails in `CoverOffline` | Returns an error → the offline transition alerts | Warn (throttled) |
| `DeletePoweredOff` or `ClearDeviceEscalations` fails in `NoteOnline` | The in-memory state is cleared anyway; the row is retried by the next reconcile, which re-reads; the online event is still emitted | Warn (throttled) |
| `RecordEscalation` fails | Not published; the window stays; retried next tick | Warn (throttled) |
| `CloseWindow` fails | Retried; evicted after 5 min (AD-009) | Warn (throttled) |
| `SuppressedDeaths`, `ContainerStartsSince` or telemetry read fails | The window close is postponed to the next tick (no guess); bounded by the 5-min eviction, after which a reconcile retries | Warn (throttled) |
| Generator store failure | The existing `slog.Error("create event")` behavior | unchanged |
| `GET /api/v1/activity` with no Tracker | 503 `activity_unavailable` | none |

HTTP shapes follow the existing `{"error":…,"code":…}` convention. No new client-facing errors exist on the agent endpoints.

---

## 10. Security Considerations

| Vector | Mitigation |
|---|---|
| One device's agent suppresses another device's alerts with a foreign command ID (NFR-010) | Coverage searches only windows of the authenticated path device. The agent-supplied ID is ignored for coverage. Attribution stays `GetForDevice`-scoped. AC-045 |
| An agent spoofs keys to widen suppression | `DockerEvent.TargetKeys` is `json:"-"` and computed server-side from the name, the server's stored snapshot and the ID. Coverage still needs an in-scope window dispatched by rIOt for that device |
| Control characters, invalid UTF-8 or huge names in messages, notifications, WS or labels | `SafeName`: printable, valid UTF-8, ≤ 128 bytes (AC-046). The `command_escalations.target` CHECK ≤ 160 bytes. React escapes on render |
| A die-event flood creates DB load | Coverage costs no query. The event inserts are 1:1 as today, behind the existing `dockerEventAllowed` limit. No per-event memory |
| An escalation flood | ≤ 22 per command (PK plus cap). Commands are rate-limited at creation (existing `creationGate`) |
| Suppression used to hide a real outage | Only rIOt-dispatched commands open windows. Every window has a hard bound. Non-success ends offline coverage at once. OOM is never suppressed. Residuals are as in NFR-011 |
| Activity endpoint information exposure | Dashboard auth only. It reveals action and actor phrase, which the commands API already shows |
| Fail-open bypass | An unreadable state alerts; it never suppresses |
| Tests run real power actions or the agent (NFR-001) | AC-047 AST check. The Tracker and the generator never exec anything |

---

## 11. Performance Considerations

| Path | Added cost | Index |
|---|---|---|
| `CheckDockerEvent` die/oom | One RLock'd map walk over the device's windows (≤ 200, typically ≤ 5) | none (no query) |
| Offline worker (30 s) | Map lookup per transition; one upsert only when powered off | PK `device_powered_off` |
| Commands hook | Copy under lock; non-blocking send | none |
| Loop (1 s) | O(tracked windows) in memory; DB only for closes and escalations, ≤ 50 units per tick | as below |
| Reconcile (30 s) | `ListWindowCandidates` (LIMIT 1000); `LapseStaleWindows` (LIMIT 500); `AutoUpdateRepo.ListAll` | `idx_commands_window_pending` (partial) |
| FR-022 at close | `SuppressedDeaths`; `ContainerStartsSince`; one `GetLatestSnapshot` | `idx_events_command (command_id, created_at)`; existing `idx_events_device_time` |
| Label load at start | `ListOpenDeviceEscalations(since now−24h)` | `idx_command_escalations_device_open` |

`idx_events_command` also serves the existing `docker_activity.go` query `WHERE device_id=$1 AND command_id=$2`. At Ryan's scale (4 devices, ≤ 200 containers, ≤ 20 open commands) the NFR-007 100 ms p95 has orders of magnitude of headroom. No caching beyond the Tracker. No new rate limit.

---

## 12. Implementation Notes for Engineers

1. **Wiring order and typed nils.**
   - Build the Tracker before `commands.New`.
   - Never assign a nil `*expect.Tracker` to `Options.Observer`, `SetExpectedActivity` or `HandlerDeps.Activity`.
   - `handlers.cmdSvc()` (test default) gets no observer; that is fine.
2. **Lock discipline (deadlock avoidance).**
   - The Tracker never calls the Escalator, the Broadcaster or a repo while it holds its mutex. Copy, unlock, then call.
   - The generator never holds `g.mu` while it calls the Tracker.
   - The Tracker's `CommandChanged` must not block. It is called on the commands service's goroutines, including the sweeper.
3. **`WindowClosedAt` in `commandColumns`.** This changes every `RETURNING` and select. Update `scanCommand` and the mock's `copyCommand` together. `CreateIfNoConflict`'s INSERT is unchanged.
4. **Late result after `timed_out`.** The `succeeded` row arrives via the hook. If `WindowClosedAt != nil`, drop it. If the close is still in flight in memory, `CloseWindow`'s guard plus the marker PKs make the re-evaluation harmless. Test: `TestEX_LateResultAfterTimeoutDoesNotReopen`.
5. **Time.**
   - Compare against `deadline_at` when it is set (it is the claim-time truth); fall back to `dispatched_at + exec_deadline_s`.
   - All comparisons are UTC.
   - Message `HH:MM` uses `t.In(time.Local)`: riot-server's container TZ. The dashboard formats in the browser.
6. **Event keys.** `TargetKeyContainer(name)` trims `/`. Compose keys come from the latest snapshot's labels for the same name; this is the existing logic, moved above the nil-repo return. A bulk command's target keys come from `DispatchBulkDocker`, unchanged.
7. **Excluded containers.** Excluded containers are checked before coverage. Today an excluded container that died during an "update" stored an info event. Now it stores nothing, which matches the outside-window behavior (X-9).
8. **Retired tests.** The two generator tests in Section 4 are deleted, with commit message `[EXPECT-001] test: retire per-device update downgrade tests (FR-014)`. Do not edit any other existing test, apart from the additive `useDevices.test.ts` cases.
9. **AC-047 static check.** Parse every `.go` file under `internal/server/expect/`, plus `internal/server/offline*.go`, `internal/server/events/generator_expect_test.go` and `internal/server/handlers/{activity,fleet_actor,docker_attribution_keys}_test.go`. Fail on:
   - any import of `os/exec` or `syscall`;
   - any string literal containing `systemctl`, `poweroff`, `reboot -`, `shutdown -`, `halt`, or `riot-agent` followed by a path or `exec`;
   - any call to `exec.Command`.

   For the new web tests, a vitest file `web/src/utils/activity.safety.test.ts` greps the new test sources for `child_process`.
10. **Mocks.** `MockExpectedActivityRepo.RecordEscalation` must enforce the `(command_id, target)` uniqueness and append the event to the linked `MockEventRepo`, so that harness counts (AC-037) are exact.
11. **No new dependency.** Use `slog`, `pgx`, `chi`, react-query, and the existing UI primitives (`RelativeTime`, `Badge`, `Modal`). Use the `scrollbar-thin` class on any new overflow container.
12. **Commit format.** `[EXPECT-001] <type>: …`. Work packages (WP) in order:
    - WP-1: models, migration, repo, mocks;
    - WP-2: generator (AD-005, AD-010, AD-013) with a fake oracle;
    - WP-3: commands observer;
    - WP-4: the `expect` package and harness tests;
    - WP-5: server wiring, offline grace, hub, router;
    - WP-6: handlers (activity, keys, registration note, fleet actor);
    - WP-7: web;
    - WP-8: safety test.

    Run `go test ./...` and `cd web && npm run test:run` after each WP.
13. **Build environment.** `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp` (the C: drive is nearly full).

---

## 13. Definition of Done

- [ ] Every Section 4 change is implemented; no agent, `install.sh` or dependency change.
- [ ] Every AC in Section 8 has the named test (`TestEX_AC0xx_*` / `[AC-0xx]`), and all pass with an injected clock.
- [ ] `go test ./...` and `cd web && npm run test:run` are green; `make test-go` with `-race` on Linux CI.
- [ ] Migration 000033 runs up, down, and up again cleanly on PostgreSQL 16 (PG test). The predicate equals `models.ExpectActions()`.
- [ ] No new lint errors (`go vet`, the frontend lint and `tsc`).
- [ ] The AC-043 files are unmodified (`git diff --stat` shows no change to `*.patchflow.test.tsx` or `FleetOverview.test.tsx`).
- [ ] The AD-012 bounds table is reflected in code comments at each bound (cap 200, 22 markers, 50 per tick, 5 min eviction, 24 h lapse).
- [ ] The implementation report lists the two retired tests and why.
- [ ] No test or helper runs a real power action or executes `riot-agent` (AC-047 passes).

---

## FRD Deviations and Clarifications

| ID | FRD item | Resolution | Rationale |
|---|---|---|---|
| X-1 | NFR-006(b) "fire within 2 min after start, FR-033 window included" | Due escalations fire on the first loop tick at or after `graceEnd`: start + 2m + ≤ 1 s. The offline detection one-shot also runs at exactly start + 2m | Device online state is unknown during the FR-033 grace. Deciding earlier would escalate devices whose agents are just reconnecting (AC-035 has them reconnect at 70 s) |
| X-2 | FR-018 has no rule for an empty actor name | `by an operator` | Old sessions and some dispatch paths have no display name. Fleet actions now record the actor (AD-016) |
| X-3 | FR-039 "link to the command's existing detail or history entry" | Docker actions link to `/docker/activity?host=…&command=…` (existing). Host and agent actions open a modal on the device page that shows the existing `GET /devices/{id}/commands/{cmd}` via `CommandProgress` (`showActivityLink=false`) | No host-command detail page exists; the Docker activity detail rejects non-`docker_` commands |
| X-4 | FR-035 does not cover: shutdown succeeded but still online; os_update return phase with the device online; agent_update return phase with the device online | `Shutting down`; `Patched, settling until <t>`; `Restarting agent, back by <t>` | These states exist under Table 4.1 and need a label (FR-034). The wording is for Ryan's review |
| X-5 | FR-003 "device back online for 3 min / 1 min" | For `reboot` and `agent_update`, the settle period counts only from an observed return after terminal success (an agent registration or offline→online). Otherwise the window runs to the return bound. For `os_update`, from max(F, onlineSince) (A-005). All counts start no earlier than `graceEnd` | The offline detector lags ≤ 75 s. Counting from F alone could close a reboot window before the host is even seen down |
| X-6 | FR-044 "device_online event" | The online event is still emitted only on agent registration (today's trigger). A heartbeat-only return updates the Tracker silently, as today | Unchanged trigger; the agent registers on every start, which covers reboot, shutdown and agent restart |
| X-7 | FR-022 "observed running" | A `container_started` event, or the latest telemetry snapshot (timestamp after the death) listing it as `running` | The FRD's two sources made precise |
| X-8 | FR-024 cooldown | Escalations neither check nor **start** a rule cooldown | Starting one would hide unrelated later alerts (the cooldown key is per device and rule) |
| X-9 | FR-015 / AC-010 | Exclusion is checked before coverage, so an excluded container never stores an event, inside or outside a window | "Identical to today's excluded behavior", which outside an update is: no event |
| X-10 | A-007 dpkg label | Step token `wait_dpkg_lock` reserved in `models`; v2.78.0 agents never send it, so the label falls back | EXPECT-002 adds the reporting |
| X-11 | NFR-009 "cannot be read" | The in-memory cache counts as unreadable when it was never loaded or is more than 90 s past its last successful reconcile | Decisions are cache-based (AD-002) |
| X-12 | Durability on upgrade | The migration closes (`lapsed`) terminal commands that finished more than 1 h before the upgrade. Commands that finished within the hour are processed, and may raise FR-021 or FR-022 escalations right after the first start | Longest post-terminal period is 15 min. One hour avoids a burst of stale escalations while keeping windows that are in flight across the upgrade (riot-server's own auto-update) |
| X-13 | FR-014 | Two existing generator tests that assert the per-device downgrade are retired | They test the removed behavior; AC-005 and AC-006 replace them |

No AC is unachievable as written. There is no blocker.

## Residuals (in addition to the FRD's NFR-011)

- **R-1.** A command row deleted early by the command-results budget ends its window early (fail open: normal alerting).
- **R-2.** An `os_update` that reboots more than about 105 s after its result can close by settle first; the later offline then alerts normally.
- **R-3.** A crash between the escalation commit and the notification dispatch loses that one push; the escalation event is still stored.
- **R-4.** A reboot that reports "initiated" but never happens keeps container-death suppression on that device until the 15-min return bound.

## Open points for Ryan

- **Label wording (X-4).** Clod recommends: keep it as written.
- **First-deploy lookback of 1 h (X-12).** Clod recommends: keep.
- **Restart-grace escalation timing (X-1).** At 2m + ≤ 1 s rather than "within 2 min". Clod recommends: accept.
