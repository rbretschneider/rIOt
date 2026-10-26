# Architecture Decision Document

- **Story ID:** EXPECT-001
- **FRD Reference:** `docs/requirements/EXPECT-001-frd.md` (revision 1, commit 0248c56; DEC-1 to DEC-5 at the recommended option)
- **Security review:** `docs/security/EXPECT-001-security-review.md` (commit 6a9b0dd, APPROVED WITH CONDITIONS C-1 to C-8). This revision folds them in; see the Security addendum
- **Builds on:** CMD-LIFECYCLE-001 (`docs/architecture/CMD-LIFECYCLE-001-add.md`: commands record, sweeper, target keys, Docker event attribution), REBOOT-DPKG-001, SHUTDOWN-DPKG-001, PATCH-FLOW-001
- **Author:** Architect Agent
- **Date:** 2026-10-26
- **Status:** DRAFT, revision 2 (security conditions folded in; for implementation)

## Changelog

### Revision 2: security review conditions C-1 to C-8

| Item | Source | What changed |
|---|---|---|
| Powered-off row is never a coverage reason for a new offline transition; invalidated by durable evidence of life; never re-stamped | C-1 (SEC-001) | AD-006, AD-007 rewritten; new repo method `PurgeInvalidPoweredOff`; §6.3, §9 |
| No window ends without its backstop; pending windows exempt from deletion; overflow order; lapse runs the full close; FR-022 re-check on return | C-2 (SEC-002), SEC-002 (d) decision | AD-009 rewritten (single `finishWindow`), AD-011 (deferred re-check), AD-012; `docker_retention.go` and `command_budget.go` exemptions; new event type `expect_backstop_skipped`; R-1 removed |
| Escalation publish off-loop with bounded concurrency and deadline; readiness also fails open on loop non-liveness; per-tick time budget; reconcile on its own goroutine | C-3 (SEC-003) | AD-002, AD-009, AD-010; new `publisher.go` |
| Offline detection and the Tracker treat every non-`offline` status as live | C-4 (SEC-004) | AD-006, AD-008 |
| Per-device ceiling on escalation pushes (5 per rolling hour) with a held-count summary; README notes the operator name | C-5 (SEC-005) decision | AD-010; new event type `expect_notifications_held` |
| Collision-free marker identity; `SafeName` strips Unicode Cf; every agent-sourced field of new events sanitized; AC-046 extended | C-6 (SEC-006) | AD-010, AD-011, AD-015; §5 |
| Durable restart-series bound on the no-offline-detection period (4 min per series), start-up `Warn` when shortened | C-7 (SEC-007) decision | AD-008; one `admin_config` row |
| QA gate; AC-047 scan file set made automatic; AC-045 extended (INFO-2) | C-8, INFO-2, INFO-6 | §8, §12 note 9, §13 |
| §10 wording on same-device trust; URL-encoded link; `offlineAt` clock comment; rollback note | INFO-1, INFO-4, INFO-5, INFO-3 | §10, §12 |

### Revision 1

The first version.

---

## 1. Summary

A new server package, `internal/server/expect`, holds a **Tracker**. The Tracker derives every expected-activity window from the existing `commands` table and answers "is this death or offline transition expected, and by which command?" from an in-memory cache. The cache is loaded at start, kept current by a hook on every command transition, and reconciled from the database every 30 s on its own goroutine.

The event generator asks the Tracker before it alerts:
- a covered `container_died` or `device_offline` is stored as an `info` event linked to the command and is never notified;
- an uncovered or undecidable one alerts exactly as today (fail open: the cache is unloaded, the reconcile is stale, or the loop is not live).

A 1-second evaluation loop ends windows through **one** routine, `finishWindow`. Whatever the reason (settle, return bound, hard bound, lapse, overflow), that routine runs the backstop: FR-020, FR-021, and FR-022, which is deferred to the device's return when the device is offline. Escalations go through the existing alert rules. They are published off-loop, with bounded concurrency, and capped at 5 pushes per device per hour. A server-computed activity per device goes to the dashboard (`GET /api/v1/activity` plus the `activity_update` WebSocket message).

Durable additions are bounded:
- two columns on `commands`;
- the exactly-once marker table `command_escalations` (at most 22 rows per command, cascaded);
- `device_powered_off` (at most 1 row per device; never a coverage reason by itself);
- one `admin_config` row for the restart series;
- two partial indexes.

Unprocessed windows are exempt from command deletion. The per-device "update in progress" counter is removed. Offline detection pauses after a start: 2 minutes per start, at most 4 minutes per rapid-restart series. The agent is not changed.

## 2. Technical Context

### 2.1 What exists (verified at 0248c56)

| Area | Where | Relevant facts |
|---|---|---|
| Durable action record | `commands` table (migrations 000001, 000023, 000032); `internal/models/commands.go`; `internal/server/db/command_repo.go` | `dispatched_at` and `deadline_at` are set at claim (`ClaimQueued`, `ClaimByID`, and the agent edge out of queued). A revert to queued sets them back to NULL. `finished_at` is set on every terminal transition. `timed_out` accepts one late agent result, which rewrites `finished_at`. Target keys: `host`, `docker:container:<name>`, `docker:compose:<wd>`, `docker:id:<id>` |
| Single writer | `internal/server/commands/service.go` | Every accepted transition (create, claim, revert, agent report, cancel, sweep) ends in `Service.broadcast(cmd)`. That is the single choke point for the Tracker hook (AD-004) |
| Budgets | `internal/models/command_policy.go` | `ExecDeadline`: reboot and shutdown 20m, os_update 40m, docker_update 35m, `docker_stack_*` 35m, bulk min(35m×N+5m, 6h), agent_update 15m, docker_restart and docker_stop 15m (ClassOther) |
| Deletion of commands | `internal/server/db/docker_retention.go` (90 d); `internal/server/storage/command_budget.go` (global, by age, under the results budget) | Neither knows about windows. C-2 adds an exemption (AD-009) |
| Container death | `internal/server/events/generator.go` `CheckDockerEvent` | Downgraded only by the per-device `activeUpdates` counter (G1 to G5) |
| Offline / online | `generator.go` `DeviceOffline` / `DeviceOnline`; `server.go` `checkOfflineDevices` (30 s, 45 s threshold, **`status == "online"` only**) | Device statuses are `online`, `offline`, `warning` (`internal/models/device.go`). A `warning` device is never moved offline today (SEC-004). `DeviceOnline` is emitted only by `RegisterDevice` when the device was not online. A heartbeat return is silent (`UpdateHeartbeatTime` sets `online`) |
| Docker attribution | `internal/server/handlers/docker_attribution.go` | Computes the event's keys (the name key, plus the compose key from the latest snapshot), then reads `ListByDevice(…,100)`. Same device only |
| Notifications | `internal/server/notify/dispatcher.go` | `Dispatch` is synchronous over every enabled channel; not every backend bounds its time (SEC-003) |
| Hub | `internal/server/websocket/hub.go` | `broadcastMsg` is a channel send (it can block) |
| Key/value config | `admin_config (key, value, updated_at)`; `db.AdminRepository.GetConfig` / `SetConfig` | Reused for the restart series (AD-008) |
| Fleet actions | `internal/server/handlers/fleet.go` | `BulkUpdateAgents` and `BulkPatchDevices` dispatch **without actor fields** (AD-016) |
| Dashboard | `web/src/pages/FleetOverview.tsx` (status dot only), `DeviceDetail.tsx`, `web/src/hooks/useDevices.ts` (WS handler) | No server-side activity view exists |
| Clock injection | `commands.Options.Now`, `HandlerDeps.Clock` | The generator reads `time.Now` directly; a seam is added (AD-013) |

### 2.2 What is missing

A window model with a guaranteed backstop, durable escalation markers, the powered-off state, a bounded restart grace, an activity API, and the dashboard labels.

### 2.3 What must change

See Section 4. No agent file changes. No `install.sh` change. No new dependency.

---

## 3. Architecture Decisions

### AD-001: Windows are derived from `commands`; only "processing finished" is stored

**Decision.** A window is a pure function of one command row (action, status, `dispatched_at`, `deadline_at` / `exec_deadline_s`, `finished_at`), the device's observed online state, and the clock (AD-003). Only the **end** of processing is persisted: `commands.window_closed_at` (and `window_outcome`), written by `finishWindow` after the backstop has run (AD-009).

**Rationale.** FRD A-002 asks to reuse the commands record; deriving the window survives restart by construction (NFR-006). The close marker is needed because:
- (a) the loader needs a bounded "still to process" set;
- (b) the deletion exemption keys on it (C-2);
- (c) a late agent result after `timed_out` must not re-open a window that was already processed.

**Alternatives considered.** A separate windows table (rejected: duplicates the lifecycle). In-memory windows only (rejected: G3, NFR-006).

**Consequences.**
- `models.Command` gains `WindowClosedAt *time.Time` (`json:"-"`), selected in `commandColumns`.
- A command whose window is unprocessed cannot be deleted by retention or the budget (AD-009). A device deletion still cascades.

### AD-002: Decisions come from an in-memory cache; three-part readiness

**Decision.** `expect.Tracker` keeps, per device:
- the window-candidate commands;
- the device state: `online` (AD-006 predicate), `onlineSince`, `offlineSince`, `returnedAt`, `lastHeartbeat`, hostname, agent version;
- at most one powered-off record (label only, AD-007);
- at most one open device escalation (label);
- the push-ceiling state (AD-010).

It is filled by:
1. `Load` at start;
2. the commands hook (AD-004);
3. `Reconcile` every 30 s, on a **dedicated goroutine** (C-3);
4. the device hooks (AD-006).

`CoverContainer` and the `CoverOffline` decision read the cache under a lock. They make **no database read**; the only write is the powered-off entry (AD-007).

**Readiness (NFR-009, AC-036, C-3).** Every `Cover*` call returns an error, and the generator then takes today's alert path, when any of these holds:
- (a) `Load` has never succeeded;
- (b) the last successful reconcile is older than `models.ExpectStaleAfter` = **90 s**;
- (c) the evaluation loop has not **completed** a tick within `models.ExpectLoopStaleAfter` = **15 s**. The loop's worst-case tick is ≤ 8 s (AD-009 tick budget), so a healthy loop never trips this.

**Rationale.** NFR-007 and the hot-path rule. Condition (c) ties fail-open to the liveness of the component that runs the backstops (SEC-003).

**Alternatives considered.** One DB query per Docker event (rejected: a query on every die/offline, and in-memory online state is still needed).

**Consequences.** Memory is bounded (AD-012).

### AD-003: Window policy table and phases (Table 4.1, FR-002 to FR-006)

`internal/models/expect.go` holds `ExpectPolicy(action) (WindowPolicy, bool)`. Only these 12 actions return `ok`:

| Action(s) | Class | Covers `device_offline` | Covers `container_died` | Post-terminal period | Early close needs an observed return | FR-021 | FR-022 |
|---|---|---|---|---|---|---|---|
| `reboot` | host | yes | every container on the device | settle 3m online, return bound 15m | **yes** | yes | yes |
| `os_update` | host | yes | every container | settle 3m, bound 15m | no (A-005) | yes | yes |
| `shutdown` | shutdown | yes | every container | powered off on its first offline; else bound 15m | n/a | no | no |
| `agent_update` | agent | yes | none | settle 1m, bound 10m | **yes** | yes | no |
| `docker_update`, `docker_bulk_update`, `docker_restart`, `docker_stack_redeploy`, `docker_stack_restart`, `docker_stack_up` | container | no | target containers only | 2m | n/a | no | yes |
| `docker_stop`, `docker_stack_down` | container-down | no | target containers only | 2m | n/a | no | no |

Constants in `models`:
- `ExpectContainerSettle` = 2m, `ExpectHostSettle` = 3m, `ExpectHostReturnBound` = 15m;
- `ExpectAgentSettle` = 1m, `ExpectAgentReturnBound` = 10m;
- `ExpectShutdownReturnBound` = 15m, `ExpectNonSuccessSettle` = 2m.

The DEC-2 values are unchanged.

**Notation.** D = `dispatched_at`; E = `deadline_at − D` (fallback `exec_deadline_s`); F = `finished_at`; P = the post-terminal period of the class (2m container, 15m host/shutdown, 10m agent); `graceEnd` from AD-008.

**Hard bound (FR-005, NFR-002).** `hard = D + E + P`. No rule ever covers at or after `hard`.

**Phases (pure function `expect.Evaluate(w, dev, now, graceEnd) Eval`):**

| Command state | Phase | Covers offline | Covers containers | Coverage ends (closeAt) |
|---|---|---|---|---|
| `queued`, D = NULL | queued | no | no | never opens. Terminal with D = NULL: `finishWindow(not_opened)`, no backstop needed |
| D set, active | in_progress | per class | per class | `hard` |
| `succeeded`, container class | settling | n/a | targets | `min(F + 2m, hard)` |
| `succeeded`, host or agent | return | yes | host: all; agent: none | the earliest of: the **early close**, `F + returnBound`, `hard` |
| `succeeded`, shutdown | return | yes | all | the first offline inside it enters powered-off (AD-007); else `min(F + 15m, hard)` |
| `failed` or `timed_out` | settling | **no**, from F (FR-004) | container and host: per class; agent: none | `min(F + 2m, hard)` (agent: F) |

**Early close (FR-003a).**
- Base: the device is online and `now − max(F, onlineSince, returnedAt*, graceEnd) ≥ settle`.
- `*returnedAt` counts only for `reboot` and `agent_update`. For these two, the early close also requires an **observed return after F**: an agent registration (AD-006) or an offline→online transition with a timestamp > F.

**Coverage at time t.** A window covers t iff `D ≤ t < closeAt`, and the window is still cached. Container deaths: `CoverContainer` at the receive time. Offline: `CoverOffline` at `offlineAt = min(now, lastHeartbeat + 45s)`. Coverage is strict at `closeAt` (AC-007).

### AD-004: Commands hook (`commands.Options.Observer`)

**Decision.**
- New optional interface `commands.CommandObserver { CommandChanged(c models.Command) }`.
- `Service.broadcast` calls it first, for every accepted transition, including creation and including when the broadcaster is nil.
- The Tracker copies the needed fields under its lock and ignores rows with `WindowClosedAt != nil` or a non-scope action.
- It sends a non-blocking signal on a 1-buffered wake channel. It does no I/O.

**Rationale.** It is the single choke point. It gives FR-041 latency and immediate FR-020 handling.

**Consequences.** The typed-nil rule of `Options` applies.

### AD-005: Suppression in the generator (FR-007 to FR-019, FR-011)

**Decision.** `events.Generator.SetExpectedActivity(ExpectedActivity)`. The interface is in `package events`:

```
CoverContainer(deviceID string, keys []string, at time.Time) (models.Coverage, bool, error)
CoverOffline(ctx, deviceID string, offlineAt time.Time) (models.OfflineCoverage, bool, error)
NoteOnline(ctx, deviceID string, at time.Time) *models.OnlineNote
```

**`CheckDockerEvent "die"`:**
1. `isContainerExcluded` → return (FR-015, X-9).
2. `CoverContainer(deviceID, evt.TargetKeys, now)`:
   - **covered** → store an `info` `container_died` event with `CommandID` = the covering command, `CommandAttr = "expected"`, `ContainerName = models.ContainerNameKey(evt.ContainerName)`, `ContainerID` = the 12-character short ID restricted to `[0-9a-f]` (C-6), and the AD-015 message. No cooldown read or write, no dispatch.
   - **not covered, or an error** → today's path, unchanged.
3. `activeUpdates`, `updateStarted`, their increments and decrements, and their prune are **deleted** (FR-014).

**Coverage selection (FR-007).**
- A container-class window whose `TargetKeys` intersect `evt.TargetKeys` is preferred.
- Otherwise a host-class window.
- Ties go to the latest D.
- Only the path device's windows are searched (NFR-010).

**`"oom"` (FR-011).** Never suppressed. When covered, the message is `Container <name> OOM killed (during <Action> <actor phrase>, command <id8>)`.

**`DeviceOfflineAt(ctx, id, host, lastHeartbeat *time.Time)`.** `DeviceOffline` remains and delegates with nil.
- **Kind `window`** → `info` event, linked, `CommandAttr = "expected"`, no cooldown, no notify.
- **Kind `powered_off`** → `info` event `Device <host> powered off <actor phrase> at <HH:MM of F>`. Only from AD-007's entry rule.
- **Not covered, or an error** → today's rule path, minus the old `recentDisruptiveCommand` downgrade.

**`DeviceOnline`.** Calls `NoteOnline` **before** its 5-minute cooldown check. When a note comes back, the message is `came online after <Action> <actor phrase> (offline <d>)` or `(powered off <d>)`. `recentDisruptiveCommand` and `disruptiveActions` are deleted.

**`commandLabels`** gains:
- `shutdown` "Shutdown";
- `docker_stack_redeploy`, `docker_stack_restart`, `docker_stack_up`, `docker_stack_down`, `docker_stack_pull` → "Docker stack redeploy", "Docker stack restart", "Docker stack up", "Docker stack down", "Docker stack pull";
- `docker_remove_items` "Docker remove items", `docker_inventory_refresh` "Docker inventory refresh", `agent_uninstall` "Agent uninstall".

### AD-006: Device state hooks and the liveness predicate (C-1, C-4)

**Liveness predicate (C-4).** One function, `models.DeviceIsLive(status)`, returns `status != "offline"`. It is used by the offline detector (AD-008), `Load`, `ObserveDevices`, and powered-off invalidation (AD-007). `online` and `warning` are both live.

The Tracker learns the device state from four places:
1. `CoverOffline`, from `DeviceOfflineAt`: `online=false`, `offlineSince=offlineAt`, `lastHeartbeat` recorded.
2. `NoteOnline`, from `DeviceOnline` (registration when the device was not live):
   - `online=true`, `onlineSince=returnedAt=at`;
   - **always** (ready or not; C-1) calls `DeletePoweredOff(deviceID)` and `ClearDeviceEscalations(deviceID, at)` with a 5 s context;
   - triggers the FR-022 re-check of deferred windows (AD-011);
   - returns the note.
3. `NoteRegistration(deviceID, at)`, from `RegisterDevice`, existing-device path, **every** re-registration including `wasOnline=true`:
   - `returnedAt = at`, `online=true`;
   - if a powered-off record is cached, or the device's cache is not loaded, calls `DeletePoweredOff(deviceID)` (5 s context). A registration is evidence of life (C-1 b).
4. `ObserveDevices(devices, at)`, from the offline worker after each pass:
   - refreshes hostname, agent version and `lastHeartbeat`;
   - a device that is live in the DB while the Tracker had it offline → the same transition and durable clears as `NoteOnline`, without an event;
   - a cached powered-off record whose device is live, or whose `lastHeartbeat > offline_at` → `DeletePoweredOff` and drop.

**Initial state at `Load`:**
- `PurgeInvalidPoweredOff` runs first (AD-007);
- live devices: `online=true, onlineSince=returnedAt=start`;
- `offline` devices: `online=false, offlineSince = lastHeartbeat + 45s`.

### AD-007: Powered-off state: entry-only, durably invalidated, never a coverage reason by itself (FR-028 to FR-032, C-1)

**Record.** Table `device_powered_off`: PK `device_id`. It holds a snapshot of `command_id`, `action_label`, `actor_phrase`, `shutdown_at` (F) and `offline_at`, so the label survives the command's retention.

**Entry.** Kind `powered_off` is returned **only** when all of these hold:
- the device has a cached **succeeded** `shutdown` window;
- `offlineAt ∈ [D, closeAt)` of that window, where closeAt is the shutdown return-phase end (`min(F + 15m, hard)`);
- `UpsertPoweredOff` succeeds.

The loop's restart-case entry, for a succeeded shutdown whose device the Tracker sees offline, applies the same test with `offlineSince ∈ [D, closeAt)`. On entry, the shutdown window is finished with outcome `powered_off`.

**Never a coverage reason by itself (C-1 a).** An existing powered-off row (cached or durable) **never** covers a new offline transition. A new live→offline transition proves the device was live after the power-off. `CoverOffline` therefore:
- calls `DeletePoweredOff` (5 s context);
- drops the cached record;
- returns *not covered*.

The transition alerts normally. A failed delete also returns not covered (C-1 c).

**No re-stamp (C-1 d).** `UpsertPoweredOff` is

```
INSERT … ON CONFLICT (device_id) DO UPDATE SET … WHERE device_powered_off.command_id <> EXCLUDED.command_id AND EXCLUDED.shutdown_at > device_powered_off.shutdown_at
```

A newer shutdown replaces an older row (FR-032). The same command never refreshes `offline_at` or `created_at`.

**Durable invalidation (C-1 b).** `PurgeInvalidPoweredOff(ctx)`:

```
DELETE FROM device_powered_off p USING devices d
 WHERE d.id = p.device_id
   AND (d.status <> 'offline' OR d.last_heartbeat > p.offline_at)
```

It runs at `Load` and at every reconcile, **before** `ListPoweredOff`. `ListPoweredOff` applies the same predicate negated, so it can never return a row for a live device. A reconcile never resurrects a row.

Decision-time checks are the AD-006 hooks (registration, `NoteOnline`, `ObserveDevices`). Every one of them deletes durably.

**Use of the row.** Labels ("Powered off by ryan at 14:02"), the "Expected offline" count, and the online-message duration. FR-029 holds because a powered-off device stays `offline` and produces no transition.

**Bound.** ≤ 1 row per device; deleted on any evidence of life; `ON DELETE CASCADE` from `devices`.

### AD-008: Offline detection, restart grace and restart-series bound (FR-033, C-4, C-7)

**Detector.** `offlineDetector.check(ctx)` (moved from `checkOfflineDevices`). After `graceEnd`, a device goes offline when `models.DeviceIsLive(status)` (C-4) and `last_heartbeat < now − models.OfflineHeartbeatThreshold` (45s). Then:
1. `SetStatus(offline)`;
2. `EventGen.DeviceOfflineAt(ctx, id, host, lastHeartbeat)`;
3. `Expect.ObserveDevices(list with updated statuses, now)`.

The 30 s ticker stays, plus a one-shot timer at exactly `graceEnd`. A device already `offline` has no transition. `offlineAt` carries a code comment: `last_heartbeat` is PostgreSQL `NOW()` and command times are the app clock, both hawaii's kernel clock (INFO-5).

**Restart series (C-7, Ryan decision S-3).** At start, before any worker, `server.go` reads `admin_config` key `expect_restart_series`. The value is `{"series_start":RFC 3339,"last_start":RFC 3339}`.
- If `last_start` exists and `now − last_start < models.OfflineDetectionGrace` (2m), this start continues the series and keeps `series_start`.
- Otherwise `series_start = now`.
- It writes `{series_start, last_start: now}` (`SetConfig`).
- `graceEnd = min(now + 2m, series_start + models.RestartSeriesGraceCap)`, where `RestartSeriesGraceCap` = **4m**; `graceEnd` is never earlier than `now`.

**Bound.** The cumulative period without offline detection is ≤ 4 minutes from the first start of any series of rapid restarts. A restart loop gets no grace after that, until a gap of ≥ 2 minutes between starts.

**Warn.** When this start continues a series, one `slog.Warn("restart grace shortened", "series_start", …, "previous_start", …, "grace", …)`.

**Failure.** When the read or the write fails: `graceEnd = now` (no grace; when in doubt, alert, BR-006) and one `Warn`.

**Storage.** One `admin_config` row, overwritten on each start. No new table.

**Tracker side.** Before `graceEnd` the Tracker makes no online-state decision:
- no FR-020 or FR-021 escalation;
- no early close;
- no powered-off entry from the loop.

Suppression lookups work throughout. Due escalations fire on the first tick at or after `graceEnd` (X-1).

### AD-009: Ending windows: one routine, always with the backstop (C-2, C-3)

**`finishWindow(w, reason)`** is the only way a window leaves processing. The reasons are `settled`, `return_bound`, `hard_bound`, `nonsuccess_settle`, `powered_off`, `not_opened`, `lapsed`, `deleted`. It runs, outside the lock, in this order:

1. **Device backstop (C-2 a).** It applies to a host, agent or shutdown window (except reason `powered_off` and `not_opened`), once the grace is over, with the device offline at this moment:
   - kind `device_nonsuccess` if the command is `failed` or `timed_out` (FR-020);
   - kind `device_no_result` if the command is still active (hard bound, no sweeper result);
   - kind `device_not_back` otherwise (FR-021).

   The escalation goes through AD-010 and is idempotent (marker `device`). FR-020 also runs **eagerly**, on the first tick after the hook reports `failed` or `timed_out`, without waiting for the window to end.
2. **Container backstop (FR-022).** For a policy with FR-022:
   - device online → run the AD-011 check now;
   - device offline → **defer** it: the window stays pending, `phase = awaiting_return`, and it no longer covers anything (AD-011).
3. `CloseWindow(id, now, outcome)`, guarded by `WHERE window_closed_at IS NULL`. Skipped while deferred.
4. Drop the window from the cache.

If a step fails, the window stays and the next tick retries. Markers and the guarded close make every retry, crash or restart harmless (FR-024).

**Tick (1 s, `Tracker.Tick`):**
- eager FR-020;
- due windows → `finishWindow`;
- deferred re-checks whose device returned (AD-011);
- drop 24 h-old open device escalations (FR-037);
- activity diff → enqueue broadcasts (AD-010 publisher).

**Tick budget (C-3).** No new DB call starts after **3 s** of tick time; the rest carries over. Every DB call has a 5 s context. The tick therefore completes in ≤ 8 s, and its completion time feeds readiness (c). No publish happens on the loop (AD-010).

**Reconcile (30 s, its own goroutine):**
1. `PurgeInvalidPoweredOff`; `ListPoweredOff`.
2. `ListOpenWindows(now, 1000)`: pending in-scope rows that are active, or terminal with `finished_at ≥ now − 24h`, **newest first** (`ORDER BY created_at DESC`).
3. `ListDueBacklog(now, 200)`: pending rows that are terminal, oldest `finished_at` first.
4. `ListLapseCandidates(cutoff = now − 24h, 100)`: pending rows terminal before the cutoff, or terminal with D = NULL. These are processed by `finishWindow(lapsed)` with the full backstop, **not** a SQL-only close.
5. Rebuild the cache:
   - priority 1: active and covering windows, newest first;
   - priority 2: due and deferred windows, oldest first;
   - priority 3: queued label entries;
   - per-device cap **200**.

**Overflow (C-2 c).** Entries beyond the cap or the `LIMIT` are not cached: they do not suppress (fail open), and they stay pending in the DB, so their backstop is **deferred, not skipped**. Each reconcile with overflow writes a throttled `Warn`, and at most one durable `warning` event per device per hour: type `expect_backstop_skipped`, message `<N> expected-activity windows on <host> exceed the tracking limit; their suppression is off and their checks are delayed`. The event is not routed through rules.

**Deleted rows.** A cached window whose row is missing from the reconcile results, and that this process did not close (device deletion, manual SQL), is dropped. If its device still exists, one `warning` event is written: `expect_backstop_skipped`, `Checks for <Action> <actor phrase> on <host> were skipped: the command record was removed (command <id8>)`.

**Eviction.** A window whose `finishWindow` keeps failing for 5 minutes after `closeAt` is evicted from the cache only. Its row stays pending, so it is reloaded and retried. Eviction never closes and never skips the backstop. Throttled `Warn`.

**Deletion exemption (C-2 b).** Both `docker_retention.go` (the `commands` DELETE) and `command_budget.go` (`purgeCommandResults`) add `AND NOT (window_closed_at IS NULL AND action IN (<ExpectActions>))`. The list is rendered from typed constants. The exempt set is the pending set, bounded in AD-012.

### AD-010: Escalation: exactly once, through the rules, published off-loop, ceiling per device (FR-020 to FR-027, C-3, C-5, C-6)

1. The Tracker builds a `models.EscalationRequest`: kind (`device_nonsuccess`, `device_no_result`, `device_not_back`, `container`, `container_summary`), command snapshot, hostname, container key name and count.
2. `Escalator.PrepareEscalation(ctx, req)` (`events.Generator`):
   - finds the matching enabled rule: `device_offline` (device scope) or `container_died` (container scope; "" for the summary);
   - returns `skip=true` when the container is excluded;
   - builds the event: rule severity, or `warning` without a rule; `CommandID`; `CommandAttr = "escalation"`; `ContainerName = ContainerNameKey`; the AD-015 message.
3. `Repo.RecordEscalation(ctx, marker, event)`, in **one transaction**: marker `INSERT … ON CONFLICT (command_id, target) DO NOTHING RETURNING`; if inserted, `INSERT INTO events`; set `event_id`; commit. It returns `inserted`.
4. Only if `inserted` → `publisher.Enqueue(push{evt, rule, hostname})` (non-blocking), plus one `slog.Info("expected-activity escalation", …)`.

**Marker identity (C-6 a).**
- `device`;
- `summary`;
- `container:` + the first 32 hex characters of SHA-256 over the event's `ContainerName`. That name is `ContainerNameKey(raw)`, collision-free for distinct raw names (AD-015).

Target length ≤ 42 bytes. Distinct containers never share a marker.

**FR-025 cap.** `ListEscalationTargets(commandID)` counts the existing `container:*` markers. At most 20 are emitted in total, then one `summary` with the number remaining. At most 22 rows per command.

**Publisher (C-3).** `expect/publisher.go`: a queue of **64** and **2** worker goroutines. Each push gets a 30 s context:
- the hub broadcast;
- then, when `rule != nil && rule.Notify`, the ceiling check and `dispatcher.Dispatch(ctx, alert)`.

Activity broadcasts use the same queue. A full queue drops the push, writes a throttled `Warn`, and does not advance the activity "last sent" state, so the activity is retried next tick. The event is already committed (R-3). A stuck backend holds at most its own worker. The loop never waits on a publish. Escalations neither check nor start rule cooldowns (X-8).

**Push ceiling per device (C-5, Ryan decision S-1).**
- At most **5** escalation notifications per device per rolling **60 minutes**. Kept in memory per device: ≤ 5 timestamps, a held count, the first held time, and the first held rule.
- Past the ceiling, the escalation **event** is still stored and broadcast (exactly once, FR-024); only the push is held.
- When capacity frees, one summary is pushed through the first held escalation's rule (at most 1 per device per hour): event type `expect_notifications_held`, severity `warning`, message `<N> escalation notifications for <host> were held since <HH:MM>; see Alerts`.
- Bound: ≤ 6 escalation pushes per device per hour.
- The state is in memory, so a restart resets it (residual R-5).
- The README states that escalation pushes carry the operator's display name (`by <name>`, possibly an OIDC full name or email).

### AD-011: The FR-022 "still not running" check, with re-check on return (SEC-002 d, Ryan decision S-2)

**Check:**
1. `SuppressedDeaths(deviceID, commandID, 1000)` gives, per `container_name` (already in key form), the last `container_died` with `command_id = X AND command_attr = 'expected'`. It uses `idx_events_command`.
2. A container counts as observed running after its last suppressed death if either holds:
   - a `container_started` event: `ContainerStartsSince(deviceID, D, 1000)`; the names are mapped through `ContainerNameKey` before comparing;
   - the latest telemetry snapshot has a timestamp after the death and lists the name (in key form) with state `running`.
3. Each container that is not running → a `container` escalation (AD-010), in ascending key order, under the cap.

Skipped for `docker_stop`, `docker_stack_down`, `shutdown` and `agent_update`.

**Deferred re-check on return.** When the device is offline at the window's end, the check is deferred:
- `phase = awaiting_return`; the window covers nothing and its row stays pending.
- When the device next becomes live (`NoteOnline`, `ObserveDevices` or a registration), the check runs after the device has been live for `ExpectHostSettle` (3m). That gives containers time to start and telemetry time to arrive.
- Then `CloseWindow(outcome = closed or escalated)`.
- **Bound.** If the device has not returned **24 h** after `closeAt`, `finishWindow(lapsed)` closes the window without the container check and writes one `expect_backstop_skipped` warning event: `Container checks for <Action> <actor phrase> on <host> were not run: the device did not return within 24h (command <id8>)`. The device's own `device_offline` alert or escalation has already fired, because container windows do not cover offline and host windows escalate in step 1.

### AD-012: Bounds (Ryan's standing rule)

| Thing | Bound | Cleanup |
|---|---|---|
| `commands.window_closed_at` and `window_outcome` | On existing rows | With the command |
| Pending set (`idx_commands_window_pending`, the deletion exemption) | In-scope rows with `window_closed_at IS NULL`: open windows; queued in-scope commands (≤ 60m TTL); deferred re-checks (≤ 24h after `closeAt`); unprocessed backlog (≤ 24h, then lapse at 100 per 30 s with the full backstop) | `finishWindow` |
| `command_escalations` | ≤ 22 rows per command | `ON DELETE CASCADE` |
| `device_powered_off` | ≤ 1 row per device | Deleted on any evidence of life; cascade from `devices` |
| `admin_config` `expect_restart_series` | 1 row, ≤ 128 bytes, overwritten each start | n/a |
| `idx_events_command` | Linked events only | 90 d event retention |
| Events | Suppressed events replace today's warning events 1:1. Escalations ≤ 22 per command. `expect_backstop_skipped` ≤ 1 per window plus ≤ 1 per device per hour (overflow). `expect_notifications_held` ≤ 1 per device per hour | 90 d |
| Notifications | Escalation pushes ≤ 6 per device per hour (5 plus 1 summary) | n/a |
| Tracker memory | ≤ 200 windows per device (AD-009 priority); one device entry per device (state, ≤ 1 powered-off record, ≤ 1 open escalation, ≤ 5 push timestamps); a policy-target map ≤ the number of policies; an activity cache ≤ 1 per device; publisher queue 64 and 2 workers. **No per-event state** | Closed windows dropped on the same tick; evicted ≤ 5 min after `closeAt` (still pending in the DB); deleted devices dropped at reconcile |
| Logs | Suppression: `Debug`. Escalation: 1 `Info`. Errors, overflow, eviction and dropped pushes: `Warn` at most once per minute per kind, plus the global `logbudget`. Restart-series: 1 `Warn` per shortened start | n/a |
| Removed | `Generator.activeUpdates` and `updateStarted` | n/a |

### AD-013: Clock injection

- `expect.Options.Now`.
- `events.Generator.SetClock`, used by every new or modified path.
- `offlineDetector.now` and the restart-series computation's `now`.

No test sleeps (NFR-012). The publisher tests use channels, not time.

### AD-014: Server-computed activity (FR-034 to FR-043)

**Decision.**
- `Tracker.Activities()` returns `models.ActivityResponse` (Section 6.1).
- The dashboard reads `GET /api/v1/activity` plus WS `activity_update`.
- The web builds the label text in `activityLabel(...)`, with times in the browser locale.

**Selection per device**, in priority order:
1. a host or agent window in `in_progress` or `return` (latest D);
2. an open device escalation (< 24 h, not cleared);
3. a powered-off record (valid per AD-007);
4. a queued host or agent command;
5. container-class windows in `in_progress` or `settling`.

`awaiting_return` windows produce no label.

### AD-015: Messages, actor phrase and name handling (FR-018, NFR-010, C-6)

**`models.ActorPhrase(cmd, policyTarget)`:**
- `user` → `by <actor_name>` (`by an operator` when empty, X-2);
- `auto_update` → `by auto-update policy for <target>`, `#<id>` when unresolved, `by an auto-update policy` when there is no ID;
- `auto_patch` → `by the auto-patch schedule`.

**`models.SafeName(s)`:**
1. `SanitizeText`;
2. remove every rune in Unicode category **Cc**, **Cf** (bidi overrides U+202A–U+202E and U+2066–U+2069, zero-width characters, BOM; C-6 b) and U+007F;
3. `TruncateHead(…,128)`.

It is applied to actor names, policy targets, hostnames and target labels.

**`models.ContainerNameKey(raw)` (C-6 a, c).**
- `n = TrimLeft(raw, "/")`.
- If `SafeName(n) == n`, return `n`. Ordinary names are unchanged.
- Otherwise return `TruncateHead(SafeName(n), 111) + "~" + hex(SHA-256(n))[:16]`: ≤ 128 bytes, printable.
- The result is idempotent (`Key(Key(x)) == Key(x)`) and distinct for distinct raw names (64-bit hash suffix).

It is used for `Event.ContainerName` on every new event (suppressed, escalation), inside messages, and for all FR-022 comparisons.

**Templates** (`<id8>` = the first 8 characters of the command ID; server times `15:04` in `time.Local`):

| Use | Template |
|---|---|
| Suppressed death | `Container <key> died during <Action>[ of <target>] <actor phrase> (expected)` |
| Suppressed offline | `Device <host> went offline during <Action> <actor phrase> (expected[, back by <HH:MM>])` |
| Powered off | `Device <host> powered off <actor phrase> at <HH:MM of F>` |
| Online after window | `Device <host> came online after <Action> <actor phrase> (offline <dur>)` |
| Online after powered off | `Device <host> came online after Shutdown <actor phrase> (powered off <dur>)` |
| OOM inside a window | `Container <key> OOM killed (during <Action> <actor phrase>, command <id8>)` |
| `device_nonsuccess` (FR-020) | `<host> is offline: <Action> <actor phrase> <failed\|timed out> (command <id8>)` |
| `device_no_result` | `<host> is offline: <Action> <actor phrase> sent no result within <E+P> (command <id8>)` |
| `device_not_back` (FR-021) | `<host> still offline <bound> after <Action> <actor phrase> (command <id8>)` |
| `container` (FR-022) | `Container <key> still not running <p> after <Action> <actor phrase> (<outcome>, command <id8>)` |
| `container_summary` | `<N> more containers still not running after <Action> <actor phrase> (<outcome>, command <id8>)` |
| Held summary | `<N> escalation notifications for <host> were held since <HH:MM>; see Alerts` |
| Backstop skipped | the AD-009 / AD-011 texts |

`<dur>` = `FormatDurationShort`. In the container template, `p` = 2m (container window and non-success), 3m (host settle and deferred re-check), or 15m (host bound).

### AD-016: Fleet dispatches carry the operator

`BulkUpdateAgents` and `BulkPatchDevices` build their requests with `dockerDispatchRequest(r, device, action, params)` (AC-024).

---

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `cmd/riot-server/migrations/000033_expected_activity.up.sql` / `.down.sql` | Section 5 |
| CREATE | `internal/models/expect.go` | Policy table, `ExpectActions()`, AD-003 constants, `ExpectStaleAfter` (90s), `ExpectLoopStaleAfter` (15s), `OfflineDetectionGrace` (2m), `RestartSeriesGraceCap` (4m), `OfflineHeartbeatThreshold` (45s), `ExpectContainerEscalationCap` (20), `ExpectEscalationDisplayTTL` (24h), `ExpectPushCeiling` (5 per 1h), `StepWaitDpkgLock`, `WindowOutcome`, `DeviceIsLive`, `Coverage`, `OfflineCoverage`, `OnlineNote`, `EscalationRequest`, `Escalation`, `PoweredOffState`, `ContainerDeath`, `DeviceActivity`, `ActivityResponse`, `ActorPhrase`, `SafeName`, `ContainerNameKey`, `EscalationTarget`, `ShortCommandID` |
| CREATE | `internal/models/expect_test.go` | Policy, phrase, `SafeName`, `ContainerNameKey`, target |
| MODIFY | `internal/models/commands.go` | `Command.WindowClosedAt` |
| MODIFY | `internal/models/events.go` | `DockerEvent.TargetKeys` (`json:"-"`); `CommandAttrExpected`, `CommandAttrEscalation`; `EventExpectBackstopSkipped = "expect_backstop_skipped"`, `EventExpectNotificationsHeld = "expect_notifications_held"` |
| MODIFY | `internal/server/db/command_repo.go` | `window_closed_at` in `commandColumns` and `scanCommand` |
| CREATE | `internal/server/db/expect_repo.go` | `ExpectRepo` (§6.3) |
| CREATE | `internal/server/db/expect_repo_pg_test.go` | PG tests |
| MODIFY | `internal/server/db/interfaces.go` | `ExpectedActivityRepository`, compile-time assertion |
| MODIFY | `internal/server/db/docker_retention.go` | Pending-window exemption (AD-009) |
| MODIFY | `internal/server/storage/command_budget.go` | Pending-window exemption (AD-009) |
| CREATE | `internal/server/storage/command_budget_expect_pg_test.go` | C-2 (b) PG test |
| MODIFY | `internal/testutil/mocks.go` | `MockExpectedActivityRepo` (linked to `MockEventRepo`, `MockCommandRepo` and `MockDeviceRepo`; enforces the marker PK, the upsert rule and the purge predicate; `FailOn`); `MockCommandRepo` copies `WindowClosedAt` |
| CREATE | `internal/server/expect/tracker.go` | `Tracker`, `Options`, `New`, `Load`, `Reconcile`, `Run` (loop plus reconcile goroutine), `Tick`, `CommandChanged`, `CoverContainer`, `CoverOffline`, `NoteOnline`, `NoteRegistration`, `ObserveDevices`, `Activities`, `WindowCount`, `Ready` |
| CREATE | `internal/server/expect/window.go` | `Evaluate`, coverage selection |
| CREATE | `internal/server/expect/finish.go` | `finishWindow`, deferral, lapse, overflow, deleted rows (AD-009, AD-011) |
| CREATE | `internal/server/expect/escalate.go` | AD-010 orchestration; the `Escalator`, `Broadcaster`, `DeviceLister`, `PolicyLister` and `TelemetryReader` interfaces |
| CREATE | `internal/server/expect/publisher.go` | Bounded publisher and push ceiling (AD-010) |
| CREATE | `internal/server/expect/activity.go` | AD-014 |
| CREATE | `internal/server/expect/messages.go` | AD-015 templates |
| CREATE | `internal/server/expect/*_test.go` | Section 8 |
| MODIFY | `internal/server/events/generator.go` | AD-005, AD-010 (`PrepareEscalation`, `PublishEscalation`), AD-013; removals; `DeviceOfflineAt`; labels |
| CREATE | `internal/server/events/generator_expect_test.go` | Fake-oracle tests |
| MODIFY | `internal/server/events/generator_integration_test.go` | Retire `TestCheckDockerEvent_DieDowngradedDuringUpdate` and `TestCheckDockerEvent_DieNoNotifyDuringUpdate` (X-13); nothing else |
| MODIFY | `internal/server/commands/service.go` | `CommandObserver`, `Options.Observer`, the call in `broadcast` |
| CREATE | `internal/server/commands/observer_test.go` | Hook fires on accepted transitions only |
| MODIFY | `internal/server/websocket/hub.go` | `BroadcastActivity` |
| MODIFY | `internal/server/server.go` | Wiring (§7.6), restart series (AD-008), `go s.Expect.Run(ctx)` |
| CREATE | `internal/server/offline.go` | `offlineDetector` (predicate C-4, grace, one-shot) and `restartSeriesGrace(...)` |
| CREATE | `internal/server/offline_test.go` | AC-035, SEC-004, SEC-007 |
| MODIFY | `internal/server/router.go` | `GET /api/v1/activity` (admin group); `HandlerDeps.Activity` |
| MODIFY | `internal/server/handlers/handlers.go` | `HandlerDeps.Activity`; `RegisterDevice` → `NoteRegistration` on every re-registration |
| CREATE | `internal/server/handlers/activity.go` / `activity_test.go` | Endpoint |
| MODIFY | `internal/server/handlers/docker_attribution.go` | Keys computed before the nil-repo return, into `e.TargetKeys` |
| CREATE | `internal/server/handlers/docker_attribution_keys_test.go` | Keys |
| MODIFY | `internal/server/handlers/fleet.go` | AD-016 |
| CREATE | `internal/server/handlers/fleet_actor_test.go` | Actor recorded |
| CREATE | `internal/server/expect/safety_test.go` | AC-047 (§12 note 9) |
| MODIFY | `web/src/types/models.ts`, `web/src/api/client.ts` | Activity types, `getActivity` |
| CREATE | `web/src/hooks/useActivity.ts` | Query `['activity']`; refetch on reconnect |
| MODIFY | `web/src/hooks/useDevices.ts` | `activity_update` handling |
| CREATE | `web/src/utils/activity.ts` / `activity.test.ts` | Labels, `isExpectedOffline` |
| CREATE | `web/src/components/ActivityLabel.tsx` | Label, actor, start, link. The link is URL-encoded with `encodeURIComponent` (INFO-4); no raw HTML |
| MODIFY | `web/src/components/ui/labels.ts` | `EVENT_TYPES` and labels: `expect_backstop_skipped` "Expected-activity check skipped", `expect_notifications_held` "Escalation notifications held" |
| MODIFY | `web/src/pages/FleetOverview.tsx` | Label, hollow sky dot for expected offline, "Expected offline" card, offline count (FR-038) |
| CREATE | `web/src/pages/FleetOverview.activity.test.tsx` | AC-024, AC-028 (web), AC-039, AC-040 |
| MODIFY | `web/src/pages/DeviceDetail.tsx` | Banner and link (X-3) |
| CREATE | `web/src/pages/DeviceDetail.activity.test.tsx` | AC-042 |
| MODIFY | `web/src/components/docker/CommandProgress.tsx` | `showActivityLink?: boolean` |
| MODIFY | `web/src/hooks/useDevices.test.ts` | Additive `activity_update` cases |
| CREATE | `web/src/utils/activity.safety.test.ts` | No `child_process` / `dangerouslySetInnerHTML` in the new web files |
| MODIFY | `README.md`, `CHANGELOG.md` | FR-045, FR-046, plus the C-5 operator-name note, the SEC-007 series bound, the S-1 to S-3 decisions and the INFO-1 trust wording (technical writer) |
| CREATE | `internal/server/expect_docs_test.go` | AC-048 (technical writer) |

No agent file, `install.sh`, sudoers or dependency change.

---

## 5. Data Model Changes

### 5.1 `000033_expected_activity.up.sql`

```
ALTER TABLE commands
  ADD COLUMN window_closed_at TIMESTAMPTZ,
  ADD COLUMN window_outcome  TEXT NOT NULL DEFAULT '';

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
  target      TEXT        NOT NULL,   -- 'device' | 'summary' | 'container:<32 hex of sha256(key name)>'
  device_id   TEXT        NOT NULL,
  event_id    BIGINT,
  created_at  TIMESTAMPTZ NOT NULL,
  cleared_at  TIMESTAMPTZ,            -- device target only
  PRIMARY KEY (command_id, target),
  CONSTRAINT command_escalations_target_len CHECK (octet_length(target) <= 64)
);
CREATE INDEX idx_command_escalations_device_open ON command_escalations (device_id, created_at DESC)
  WHERE target = 'device' AND cleared_at IS NULL;

CREATE TABLE device_powered_off (
  device_id     TEXT        PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
  command_id    TEXT        NOT NULL,
  action_label  TEXT        NOT NULL,
  actor_phrase  TEXT        NOT NULL,
  shutdown_at   TIMESTAMPTZ NOT NULL,
  offline_at    TIMESTAMPTZ NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

The restart series uses the existing `admin_config` table (key `expect_restart_series`), so it needs no DDL. A PG test asserts that the index predicate's list equals `models.ExpectActions()`. INFO-3 confirms the migration is safe at hawaii's size.

### 5.2 `000033_expected_activity.down.sql`

```
DROP TABLE IF EXISTS device_powered_off;
DROP TABLE IF EXISTS command_escalations;
DROP INDEX IF EXISTS idx_events_command;
DROP INDEX IF EXISTS idx_commands_window_pending;
ALTER TABLE commands DROP COLUMN IF EXISTS window_outcome, DROP COLUMN IF EXISTS window_closed_at;
DELETE FROM admin_config WHERE key = 'expect_restart_series';
```

**Rollback note (INFO-3).** After a down→up, the markers for commands that finished in the last hour are gone, so those commands can escalate a second time. The implementation report states this.

### 5.3 Before / after

`commands` gains two columns; nothing is dropped. `events` has no column change, and gains the `command_attr` values `expected` and `escalation` and the event types `expect_backstop_skipped` and `expect_notifications_held`.

---

## 6. API / Interface Contract

### 6.1 `GET /api/v1/activity`

- **Auth:** the admin session group (same as `/api/v1/fleet/*`).
- **Request:** no parameters.
- **200:** `{"generated_at": RFC 3339, "newest_agent_version": string, "activities": [DeviceActivity]}`.

`DeviceActivity`:

| Field | Type | Meaning |
|---|---|---|
| `device_id` | string | |
| `kind` | `host` \| `agent` \| `docker` \| `powered_off` \| `escalated` | |
| `phase` | `queued` \| `in_progress` \| `return` \| `settling` \| `powered_off` \| `escalated` | |
| `action` | string | |
| `command_id` | string | |
| `actor_phrase` | string | Sanitized |
| `started_at` | RFC 3339 | |
| `back_by` | RFC 3339 \| null | |
| `waiting_dpkg_lock` | bool | |
| `progress` | int \| null | |
| `agent_from_version` | string | |
| `target` | string | Key form or `SafeName`, ≤ 128 bytes |
| `docker_actions` | int | |
| `covers_offline` | bool | |
| `device_online` | bool | |
| `escalation` | null \| `{"reason":"failed"\|"timed_out"\|"no_result"\|"not_back","bound":string,"at":RFC 3339}` | |
| `powered_off_at` | RFC 3339 \| null | |

- **503:** `{"error":"activity unavailable","code":"activity_unavailable"}` when no Tracker is wired. When the Tracker is not ready, the server returns 200 with the last cached activities. Labels are informational; alert decisions fail open independently.

### 6.2 Labels (web)

Unchanged from revision 1, plus one row: escalated `no_result` → `<Action> failed: offline`.

| kind / phase / condition | Label |
|---|---|
| host or agent, queued | `<Action> queued` |
| reboot in_progress | `Rebooting`; `Rebooting (waiting for dpkg lock)` |
| reboot return | `Rebooting, back by <t>` |
| shutdown in_progress / return | `Shutting down` (dpkg variant as for reboot) |
| powered_off | `Powered off <actor_phrase> at <t>` |
| os_update in_progress | `Patching` / `Patching (<n>%)` |
| os_update return, offline | `Rebooting after patch, back by <t>` |
| os_update return, online | `Patched, settling until <t>` |
| agent_update in_progress | `Updating agent <from> → <to>` / `Updating agent` |
| agent_update return | `Restarting agent, back by <t>` |
| docker, 1 action | `Docker: <verb> <target>`. Verbs: update and bulk `updating`, restart `restarting`, stop `stopping`, stack_redeploy `recreating stack`, stack_restart `restarting stack`, stack_up `starting stack`, stack_down `stopping stack` |
| docker, N ≥ 2 | `Docker: <N> actions in progress` |
| escalated, reason failed, timed_out or no_result | `<Action> failed: offline` |
| escalated, reason not_back | `Not back <bound> after <Action>` |

Beside the label (except `powered_off`): `<actor_phrase> · started <RelativeTime>`.

### 6.3 Repository interface (`db.ExpectedActivityRepository`)

```
ListOpenWindows(ctx, now time.Time, limit int) ([]models.Command, error)        // newest first
ListDueBacklog(ctx, now time.Time, limit int) ([]models.Command, error)         // terminal pending, oldest finished_at first
ListLapseCandidates(ctx, cutoff time.Time, limit int) ([]models.Command, error) // terminal before cutoff, or terminal with D NULL
CloseWindow(ctx, commandID string, at time.Time, outcome models.WindowOutcome) (applied bool, err error)
RecordEscalation(ctx, esc *models.Escalation, evt *models.Event) (inserted bool, err error)
ListEscalationTargets(ctx, commandID string) ([]string, error)
ListOpenDeviceEscalations(ctx, since time.Time) ([]models.Escalation, error)
ClearDeviceEscalations(ctx, deviceID string, at time.Time) (int64, error)
SuppressedDeaths(ctx, deviceID, commandID string, limit int) ([]models.ContainerDeath, error)
ContainerStartsSince(ctx, deviceID string, since time.Time, limit int) (map[string]time.Time, error)
UpsertPoweredOff(ctx, p *models.PoweredOffState) (stored bool, err error)       // AD-007 no-re-stamp rule
DeletePoweredOff(ctx, deviceID string) (*models.PoweredOffState, error)
PurgeInvalidPoweredOff(ctx) (int64, error)                                     // AD-007 durable invalidation
ListPoweredOff(ctx) ([]models.PoweredOffState, error)                          // valid rows only
CreateWarningEvent(ctx, e *models.Event) error                                 // expect_backstop_skipped / held summary (plain insert)
```

All values are bind parameters; the only literal lists are typed constants.

### 6.4 WebSocket

`{"type":"activity_update","device_id":…,"data":DeviceActivity|null}`, sent on change, via the publisher.

### 6.5 Go interfaces

- `events.ExpectedActivity`, implemented by `*expect.Tracker`.
- `expect.Escalator` (`PrepareEscalation`, `PublishEscalation`), implemented by `*events.Generator`.
- `commands.CommandObserver`, `expect.Broadcaster`, `handlers.ActivitySource` (`Activities()`, `NoteRegistration(...)`).

There is no import cycle.

---

## 7. Sequence / Flow

### 7.1 Container death

1. Request → `dockerEventAllowed`.
2. `validateDockerEventCommand` fills `TargetKeys`.
3. `CheckDockerEvent`:
   - excluded? → return;
   - `CoverContainer` (memory; readiness a, b, c) → covered: `info` event (key name), no dispatch;
   - else → today's path.

### 7.2 Offline transition

1. Tick or one-shot → `check`; during the grace: return.
2. For each live device (C-4) with a stale heartbeat: `SetStatus(offline)` → `DeviceOfflineAt` → `CoverOffline(offlineAt)`:
   - a host or agent window → `info`;
   - a succeeded shutdown and `offlineAt` in its return phase → `UpsertPoweredOff` → `info` "powered off";
   - an existing powered-off row and no entry → `DeletePoweredOff`, then the alert;
   - none or an error → the alert.
3. `ObserveDevices`.

### 7.3 Command transition

Service → repo → `broadcast` → `CommandChanged` → wake → tick:
- eager FR-020;
- activity diff → publisher.

### 7.4 Window end

Tick → `Evaluate` says due → `finishWindow`:
1. device backstop when offline;
2. FR-022 now, or deferred;
3. `CloseWindow` (unless deferred);
4. drop.

Escalation: Prepare → `RecordEscalation` (tx) → `Enqueue` → worker (30 s): broadcast; ceiling; Dispatch.

### 7.5 Device returns

- `RegisterDevice` → `Update` → `NoteRegistration` (deletes the powered-off row).
- If the device was not live: `DeviceOnline` → `NoteOnline` (deletes the powered-off row, clears escalations, arms the deferred re-checks) → online event.

The deferred re-checks run 3 min later in the loop.

### 7.6 Server start

1. Migrations; repos; hub; generator.
2. `restartSeriesGrace` → `graceEnd` (AD-008).
3. `ExpectRepo`; `Tracker = expect.New(…, GraceEnd: graceEnd)`.
4. `EventGen.SetExpectedActivity(Tracker)`; `commands.New(… Observer: Tracker)`.
5. `Tracker.Load(ctx)`. A failure is logged and the Tracker stays not ready.
6. `go Tracker.Run` (loop plus the reconcile goroutine).
7. `go offlineDetector(graceEnd)`.
8. Router.

---

## 8. Acceptance Criteria Mapping

Test files:
- **H** = `internal/server/expect/harness_test.go` (package `expect_test`): a real `events.Generator` plus `Tracker`, `testutil` mocks, a fake clock, a recording dispatcher, a fake broadcaster; `harness.restart()`.
- **G** = `internal/server/events/generator_expect_test.go`.
- **W** = `internal/server/expect/window_test.go`.
- **E** = `internal/server/expect/escalate_test.go`.
- **F** = `internal/server/expect/finish_test.go`.
- **P** = `internal/server/expect/publisher_test.go`.
- **A** = `internal/server/expect/activity_test.go`.
- **O** = `internal/server/offline_test.go`.

| AC | Fulfilled by | Test |
|---|---|---|
| AC-001 | die + `CoverContainer` | H: `TestEX_AC001_DockerUpdateDeathRecordedNotAlerted` |
| AC-002 | `ActorPhrase` | H: `TestEX_AC002_ScheduledUpdateNamesPolicy`; models: `TestEX_AC002_ActorPhraseOrigins` |
| AC-003 | Container targets | H: `TestEX_AC003_RestartStopAndStackActionsCovered` |
| AC-004 | Hard bound from `deadline_at` | W: `TestEX_AC004_BulkWindowCoversThreeHours`; H: `TestEX_AC004_BulkDeathAtThreeHoursSuppressed` |
| AC-005 | Target keys only | H: `TestEX_AC005_NonTargetContainerAlerts` |
| AC-006 | No `update_started` dependency | H: `TestEX_AC006_DeathBeforeUpdateStartedSuppressed`, `TestEX_AC006_NoUpdateStartedSuppressed` |
| AC-007 | Strict boundary | W: `TestEX_AC007_ContainerSettleBoundary`; H: `TestEX_AC007_DeathAfterSettleAlerts` |
| AC-008 | Policy table | H: `TestEX_AC008_ActionsOutsideTableAlert`; models: `TestEX_AC008_PolicyTableExact` |
| AC-009 | OOM | H: `TestEX_AC009_OOMAlertsAndNamesAction` |
| AC-010 | Exclusion first | H: `TestEX_AC010_ExcludedContainerSameInsideAndOutside` |
| AC-011 | Host window, online note | H: `TestEX_AC011_RebootAfterDpkgWaitNoAlert` |
| AC-012 | Host covers all | H: `TestEX_AC012_TwelveDeathsDuringReboot` |
| AC-013 | os_update return | H: `TestEX_AC013_PatchWithRebootNoAlert` |
| AC-014 | FR-022 sees starts | H: `TestEX_AC014_DockerRestartDuringPatchNoEscalation` |
| AC-015 | FR-021 once | H: `TestEX_AC015_NotBackByBoundEscalatesOnce` |
| AC-016 | Eager FR-020 | H: `TestEX_AC016_TimedOutRebootOfflineEscalates` |
| AC-017 | Non-success ends offline coverage | H: `TestEX_AC017_OfflineAfterFailedRebootAlerts` |
| AC-018 | FR-022 host | H: `TestEX_AC018_ContainerNotBackAfterRebootEscalates` |
| AC-019 | FR-022 container | H: `TestEX_AC019_StackRedeployContainerDownEscalates` |
| AC-020 | No FR-022 for intentional down | H: `TestEX_AC020_IntentionalDownNoEscalation` |
| AC-021 | Cap | E: `TestEX_AC021_TwentyPlusSummary`, `TestEX_AC021_CapHoldsAcrossRestart` |
| AC-022 | No rule → warning | H: `TestEX_AC022_EscalationWithoutRuleIsWarning` |
| AC-023 | Cooldown ignored | G: `TestEX_AC023_EscalationIgnoresCooldown` |
| AC-024 | AD-016 + DTO + label | handlers: `TestEX_AC024_FleetAgentUpdateRecordsActor`; A: `TestEX_AC024_AgentActivityVersions`; web: `[AC-024] …` |
| AC-025 | Agent return | H: `TestEX_AC025_AgentRestartOfflineInfo`; A: `TestEX_AC025_RestartingAgentBackBy` |
| AC-026 | Agent bound | H: `TestEX_AC026_AgentNotBackEscalates`; A: `TestEX_AC026_EscalatedActivity` |
| AC-027 | Agent covers no container | H: `TestEX_AC027_AgentUpdateDoesNotHideDeaths` |
| AC-028 | Powered off | H: `TestEX_AC028_ShutdownPoweredOffNoAlertsThreeDays`; A: `TestEX_AC028_PoweredOffActivity`; web: `[AC-028] …` |
| AC-029 | Online clears | H: `TestEX_AC029_PoweredOffEndsOnOnline` |
| AC-030 | Failed shutdown | H: `TestEX_AC030_FailedShutdownNoPoweredOff` |
| AC-031 | Load | H: `TestEX_AC031_WindowsSurviveRestart` |
| AC-032 | Markers + grace | H: `TestEX_AC032_DowntimeEscalationOnceAcrossRestarts` |
| AC-033 | Hard bounds | W: `TestEX_AC033_EveryWindowHasHardBound`; H: `TestEX_AC033_SilentCommandsCloseAndAlertingResumes` |
| AC-034 | Queued | H: `TestEX_AC034_QueuedRebootDoesNotSuppress`; A: `TestEX_AC034_RebootQueuedActivity` |
| AC-035 | Grace, one-shot | O: `TestEX_AC035_RestartGraceNoOffline`, `TestEX_AC035_OfflineAfterGrace` |
| AC-036 | Readiness (a), (b) | H: `TestEX_AC036_FailOpenWhenNotLoaded`, `TestEX_AC036_FailOpenWhenStale` |
| AC-037 | Bounds | H: `TestEX_AC037_RebootWindowEventCountsLogsMemory` |
| AC-038 | Labels | A: `TestEX_AC038_SelectionPriorityHostOverDocker`; web `activity.test.ts`: `[AC-038] label vocabulary` |
| AC-039 | Counts | web: `[AC-039] offline and expected-offline counts` |
| AC-040 | Live | E: `TestEX_AC040_DispatchAndCloseBroadcastWithinOneTick`; web `useDevices.test.ts`: `[AC-040] …` |
| AC-041 | 24 h / online | A: `TestEX_AC041_EscalatedClearsAfter24h`, `TestEX_AC041_EscalatedClearsOnOnline` |
| AC-042 | Header | web: `[AC-042] header shows label, actor, start and link` |
| AC-043 | Unchanged | The existing patch-badge and fleet tests pass unmodified |
| AC-044 | Keys without agent ID | handlers: `TestEX_AC044_KeysWithoutAgentCommandID`; H: `TestEX_AC044_OlderAgentRestartSuppressed` |
| AC-045 | Same device only (+ INFO-2) | H: `TestEX_AC045_CrossDeviceCommandIDIgnored`, `TestEX_AC045_HostWindowOnOtherDeviceIgnored` |
| AC-046 | Names (+ C-6) | models: `TestEX_AC046_SafeName` (cases: a 300-byte name with control characters and invalid UTF-8; **a bidi override U+202E; zero-width characters**); H: `TestEX_AC046_SuppressedAndEscalatedMessagesSanitized` (**includes a 129-byte shared-prefix pair that yields two events and two markers**) |
| AC-047 | Static safety | `internal/server/expect/safety_test.go`: `TestEX_AC047_NoPowerOrAgentExecution`; web: `[AC-047] new web tests spawn nothing` |
| AC-048 | Docs | `internal/server/expect_docs_test.go`: `TestEX_AC048_ReadmeAndChangelog` (technical writer) |

### 8.1 Security condition tests

| Condition | Test |
|---|---|
| C-1 (SEC-001) | H: `TestEX_SEC001_HeartbeatReturnThenRestartInsideGrace`, `TestEX_SEC001_HeartbeatReturnThenRestartOutsideGrace`, `TestEX_SEC001_DeleteFailureThenReconcile`, `TestEX_SEC001_RegistrationWhileOnlineClearsRow`, `TestEX_SEC001_NoteOnlineWhileNotReadyDeletes`, `TestEX_SEC001_SecondOfflineAlerts` (table over all of the above; every second transition alerts), `TestEX_SEC001_RowNeverRestamped`, `TestEX_SEC001_LoopEntryRequiresOfflineInsideWindow`, `TestEX_SEC001_UnverifiableRowAlerts`; PG: `TestEX_SEC001_PG_PurgeInvalidPoweredOff`, `TestEX_SEC001_PG_UpsertNoRestamp` |
| C-2 (SEC-002) | F: `TestEX_SEC002_HardBoundWithoutSweeperEscalates`, `TestEX_SEC002_LapseRunsBackstopAndWarns`, `TestEX_SEC002_OverflowKeepsNewestAndWarns`, `TestEX_SEC002_DeletedRowWritesBackstopSkipped`, `TestEX_SEC002_EvictionDoesNotClose`, `TestEX_SEC002_ContainerRecheckOnReturn`, `TestEX_SEC002_RecheckDeferralBounded24h`, `TestEX_SEC002_AnyEndWithDeviceOfflineEscalatesOnce` (table over all eight reasons); PG: `TestEX_SEC002_PG_RetentionSkipsPendingWindows`, `TestEX_SEC002_PG_BudgetSkipsPendingWindows` |
| C-3 (SEC-003) | P: `TestEX_SEC003_BlockingPublisherDoesNotStallLoop` (a dispatcher blocking forever; a second window still closes and escalates), `TestEX_SEC003_PublishQueueFullDropsPushKeepsEvent`; H: `TestEX_SEC003_CoverFailsOpenWhenLoopStalled`; F: `TestEX_SEC003_TickBudgetCarriesOver` |
| C-4 (SEC-004) | O: `TestEX_SEC004_EveryNonOfflineStatusGoesOffline` (`online`, `warning`; each alerts when not covered); H: `TestEX_SEC004_TrackerUsesSameLivenessPredicate` |
| C-5 (SEC-005) | P: `TestEX_SEC005_PushCeilingHoldsAndSummarizes`, `TestEX_SEC005_HeldEscalationsStillStoredExactlyOnce`, `TestEX_SEC005_SummaryAtMostOncePerHour` |
| C-6 (SEC-006) | models: `TestEX_SEC006_SafeNameStripsFormatChars`, `TestEX_SEC006_ContainerNameKeyCollisionFreeAndIdempotent`, `TestEX_SEC006_MarkerTargetBounded`; H: `TestEX_SEC006_EventFieldsSanitized` |
| C-7 (SEC-007) | O: `TestEX_SEC007_RestartSeriesBoundsGrace` (5 starts 90 s apart: the total grace is ≤ 4m), `TestEX_SEC007_ShortenedGraceWarns`, `TestEX_SEC007_SeriesResetsAfterGap`, `TestEX_SEC007_ConfigErrorMeansNoGrace` |
| C-8 | The QA gate (§13) |

Supporting tests:
- `TestEX_Observer_FiresOnAcceptedTransitionsOnly`;
- `TestEX_LateResultAfterTimeoutDoesNotReopen`;
- `TestEX_Tracker_CapPerDevice`;
- `TestEX_Repo_PG_*`: migration up, down, up; predicate equals the list; `RecordEscalation` exactly once under concurrency; cascades.

---

## 9. Error Handling

| Failure | Behavior | Log |
|---|---|---|
| `Load` fails | Not ready → `Cover*` fail open; reconcile retries | Warn (throttled) |
| Reconcile fails (more than 90 s) | Fail open | Warn |
| Loop stalls (no completed tick for more than 15 s) | Fail open | Warn |
| `UpsertPoweredOff` fails | Not covered → the alert | Warn |
| `DeletePoweredOff` fails (any path) | The cached record is dropped. The durable row is removed by the next `PurgeInvalidPoweredOff`, and it can never cover in any case (AD-007) | Warn |
| `ClearDeviceEscalations` fails | The label is cleared in memory; the reconcile re-reads only rows that are uncleared **and** whose device is not live | Warn |
| `RecordEscalation` fails | Retried next tick; the window stays | Warn |
| `CloseWindow` fails | Retried; evicted from the cache after 5 min; reloaded later | Warn |
| FR-022 inputs fail | That window's close is postponed; retried | Warn |
| Publisher queue full, or a push exceeds 30 s | The push is dropped (the event is kept); activity is retried next tick | Warn |
| Restart-series config read or write fails | No grace (`graceEnd = now`) | Warn |
| `GET /api/v1/activity` without a Tracker | 503 `activity_unavailable` | none |

---

## 10. Security Considerations

| Vector | Mitigation |
|---|---|
| Cross-device suppression (NFR-010) | Coverage searches only the path device's windows. Agent-supplied IDs are ignored for coverage. AC-045 plus the INFO-2 extension |
| Same-device influence (INFO-1) | **A device's own agent can open, shape and close windows on its own device only.** It can trigger schedule-origin commands through its telemetry, ack early, choose which names die, report starts, and turn `timed_out` into `succeeded`. All of this is bounded by NFR-002, by AD-007 (the powered-off entry must fall inside a shutdown's return phase), by the escalation caps and by the push ceiling |
| A stale powered-off row hides outages (SEC-001) | AD-007: the row never covers a new transition, is durably invalidated by evidence of life, and is never re-stamped |
| A suppression loses its backstop (SEC-002) | AD-009 `finishWindow` on every path; deletion exemption; overflow order; deferred re-check bounded to 24 h with a warning event |
| Loop stall (SEC-003) | Off-loop publisher (2 workers, queue 64, 30 s); tick budget; readiness (c) |
| Liveness predicate gap (SEC-004) | `DeviceIsLive` = not `offline`, everywhere |
| Notification flooding; operator name sent to third parties (SEC-005) | 20 + 1 per command; ≤ 6 pushes per device per hour; README note |
| Name spoofing and marker merge (SEC-006) | `SafeName` (Cc, Cf, U+007F), `ContainerNameKey` (hash suffix), a SHA-256 marker target |
| Restart loop disables offline detection (SEC-007) | Durable series bound of 4 min; `Warn` |
| Injection | Bind parameters only; typed-constant lists; React escaping; URL-encoded links; no raw HTML |
| Activity exposure | Admin auth; the same data as the commands API (INFO-4) |
| Real power or agent execution in tests (NFR-001) | AC-047 scan (§12 note 9) |

---

## 11. Performance Considerations

| Path | Added cost | Index |
|---|---|---|
| die / oom | Map walk, ≤ 200 entries | none |
| Offline worker | Map lookup; one upsert or delete only for powered-off | PK |
| Hook | Copy under lock | none |
| Loop (1 s) | Memory, plus ≤ 3 s of DB work per tick | as below |
| Reconcile (30 s) | `ListOpenWindows` (1000), `ListDueBacklog` (200), `ListLapseCandidates` (100), `PurgeInvalidPoweredOff`, `ListPoweredOff`, `ListAll` policies | `idx_commands_window_pending`; PK `device_powered_off` |
| FR-022 | Two event queries plus one snapshot read | `idx_events_command`; `idx_events_device_time` |
| Deletion exemption | One extra predicate on the existing DELETE subqueries | none |
| Startup | Two `admin_config` statements | PK |

NFR-007 has wide headroom at Ryan's scale.

---

## 12. Implementation Notes for Engineers

1. **Wiring order and typed nils.** See §7.6. Never assign a nil `*expect.Tracker` to an interface.
2. **Lock discipline.**
   - The Tracker never calls the Escalator, the publisher, the Broadcaster or a repo while holding its mutex.
   - The generator never holds `g.mu` while it calls the Tracker.
   - `CommandChanged` never blocks.
   - Reconcile builds the new cache off-lock and swaps it under the lock.
3. **`WindowClosedAt` in `commandColumns`.** Update `scanCommand` and the mock's `copyCommand` together.
4. **Late result after `timed_out`.** Covered by `TestEX_LateResultAfterTimeoutDoesNotReopen`.
5. **Time.** Prefer `deadline_at`; everything in UTC. Messages use `time.Local`. Put the INFO-5 comment at `offlineAt`.
6. **Event keys.** Computed before the nil-repo return. `ContainerNameKey` applies only to new events and to the FR-022 comparisons; the existing start, stop and other events are unchanged.
7. **Excluded containers.** Excluded first (X-9).
8. **Retired tests.** Two, with commit `[EXPECT-001] test: retire per-device update downgrade tests (FR-014)`.
9. **AC-047 scan (C-8, INFO-6).** The set of Go files is computed, not listed:
   - every `.go` file under `internal/server/expect/`;
   - `internal/server/offline*.go`;
   - **every `*_test.go` file in the repository whose source contains `TestEX_`**.

   New test files are therefore picked up automatically. Fail on:
   - any import of `os/exec` or `syscall`;
   - any call to `exec.Command`;
   - any string literal containing `systemctl`, `poweroff`, `reboot -`, `shutdown -`, `halt`, or `riot-agent` followed by a path or `exec`.

   The test prints the scanned file list so QA can confirm it. The web: `activity.safety.test.ts` scans every `*.test.ts(x)` file containing `[AC-` or `EXPECT` for `child_process` and `dangerouslySetInnerHTML`.
10. **Mocks.**
    - `MockExpectedActivityRepo` enforces the marker PK, the AD-007 upsert rule and the purge predicate (it reads statuses and heartbeats from the linked `MockDeviceRepo`).
    - It appends events to the linked `MockEventRepo`.
    - `FailOn` per method.
11. **Publisher tests.** Inject a dispatcher fake that blocks on a channel. Never sleep.
12. **No new dependency.** Use `crypto/sha256` and `unicode` (stdlib). Use `scrollbar-thin` on any new overflow element.
13. **Work packages.**
    - WP-1: models, migration, repo, mocks, retention exemptions;
    - WP-2: generator;
    - WP-3: commands observer;
    - WP-4: `expect` (window, finish, escalate, publisher, activity) and the harness;
    - WP-5: server wiring, offline detector, restart series, hub, router;
    - WP-6: handlers;
    - WP-7: web;
    - WP-8: safety tests.

    Run `go test ./...` and `cd web && npm run test:run` after each WP.
14. **Build environment.** `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`.
15. **Implementation report.** It must state the INFO-3 rollback note and the two retired tests.

---

## 13. Definition of Done

- [ ] Every Section 4 change is implemented; no agent, `install.sh` or dependency change.
- [ ] Every AC in §8 and every condition test in §8.1 exists under its name and passes with an injected clock.
- [ ] `go test ./...` and `cd web && npm run test:run` are green; `-race` in Linux CI.
- [ ] Migration 000033 runs up, down and up on PostgreSQL 16; the predicate equals `ExpectActions()`.
- [ ] No new lint errors (`go vet`, the frontend lint and `tsc`).
- [ ] The AC-043 files are unmodified.
- [ ] The AD-012 bounds appear as code comments at each bound.
- [ ] **C-8 QA gate.** These pass:
  - AC-033, AC-036, AC-045 (including `TestEX_AC045_HostWindowOnOtherDeviceIgnored`), AC-046 (with the C-6 cases) and AC-047;
  - `TestEX_LateResultAfterTimeoutDoesNotReopen`;
  - every `TestEX_SEC001` to `TestEX_SEC004` test.

  QA confirms from the AC-047 output that the scanned file list includes every new Go test file in the diff.
- [ ] No test or helper runs a real power action or executes `riot-agent`.

---

## Security addendum (review 6a9b0dd, conditions C-1 to C-8)

| Condition | Design change | Sections | Tests |
|---|---|---|---|
| **C-1** SEC-001 powered-off | (a) A powered-off row is never a coverage reason. A new live→offline transition deletes it and alerts. Entry only for a succeeded shutdown with `offlineAt` / `offlineSince ∈ [D, closeAt)`. (b) `PurgeInvalidPoweredOff` (status not `offline` or `last_heartbeat > offline_at`) at `Load` and every reconcile, before `ListPoweredOff`, which returns only valid rows. Registration, `NoteOnline` (ready or not) and `ObserveDevices` delete durably. (c) Upsert or delete failure → the alert. (d) The upsert never re-stamps the same command | AD-006, AD-007, §6.3, §9 | §8.1 C-1 row |
| **C-2** SEC-002 backstop | (a) A single `finishWindow` for all eight end reasons runs the device backstop whenever the device is offline (`device_nonsuccess`, `device_no_result`, `device_not_back`), plus FR-022. (b) Pending in-scope rows are exempt in `docker_retention.go` and `command_budget.go`. (c) Cache priority: active and newest first, then due oldest first; per-device cap 200; overflow is deferred, not skipped, with a throttled `Warn` and an `expect_backstop_skipped` event ≤ 1 per device per hour. Lapse runs the full backstop. A deleted row writes a warning event. Eviction never closes. (d) **Ryan decision S-2:** deferred FR-022 re-check when the device returns (3 min after return, ≤ 24 h, then a warning event) | AD-009, AD-011, AD-012 | §8.1 C-2 row |
| **C-3** SEC-003 loop stall | Off-loop publisher (2 workers, queue 64, 30 s per push; drop on full, with a Warn). 3 s per-tick DB budget (tick ≤ 8 s). Reconcile on its own goroutine. Readiness (c): no completed tick for 15 s → fail open | AD-002, AD-009, AD-010 | §8.1 C-3 row |
| **C-4** SEC-004 liveness | `DeviceIsLive(status) = status != "offline"` in the detector, `Load`, `ObserveDevices` and the powered-off invalidation; `warning` devices now go offline | AD-006, AD-008 | §8.1 C-4 row |
| **C-5** SEC-005 flooding | **Ryan decision S-1:** ≤ 5 escalation pushes per device per rolling hour, then held, then one `expect_notifications_held` summary ≤ 1 per hour (≤ 6 pushes per device per hour). Events stay exactly once. README: pushes carry the operator's display name | AD-010, AD-012 | §8.1 C-5 row |
| **C-6** SEC-006 names | `SafeName` strips Cc, Cf and U+007F. `ContainerNameKey` (hash suffix when altered) for every new event's `ContainerName` and all FR-022 comparisons. Container IDs restricted to hex. Marker `container:<32 hex sha256>`; target ≤ 64 bytes (CHECK). AC-046 extended | AD-005, AD-010, AD-011, AD-015, §5 | AC-046, §8.1 C-6 row |
| **C-7** SEC-007 restart loop | **Ryan decision S-3:** durable series in `admin_config` `expect_restart_series`; grace = min(2m, series_start + 4m − now); cumulative no-detection ≤ 4 min per series; a start-up `Warn` when shortened; no grace when the config cannot be read or written | AD-008, §5.2 | §8.1 C-7 row |
| **C-8** QA gate | The §13 gate item; the AC-047 scan computes its file set from `TestEX_` and prints it; AC-045 extended (INFO-2) | §12 note 9, §13 | §13 |

**Decisions Clod made on Ryan's behalf (reviewer's recommended option; Ryan reviews them in the PR):**
- **S-1 (SEC-005):** a per-device escalation-push ceiling of 5 per rolling hour plus a held-count summary.
- **S-2 (SEC-002 d):** re-check suppressed container deaths when the device returns (bounded at 24 h).
- **S-3 (SEC-007):** a durable restart-series grace bound of 4 minutes cumulative, with a start-up `Warn`.

**Out of scope (filed separately, per the review):** the SEC-002 (a) budget scope across devices beyond the pending-window exemption; notification-backend timeouts beyond the publisher's 30 s context; the unrelated agent-endpoint input-handling gap. SEC-004 is fixed here (C-4).

**Not satisfied:** none.

---

## FRD Deviations and Clarifications

| ID | FRD item | Resolution | Rationale |
|---|---|---|---|
| X-1 | NFR-006(b) "within 2 min after start" | Due escalations fire on the first tick at or after `graceEnd`: ≤ 2m + 1 s after start, and earlier inside a restart series (AD-008) | Online state is unknown during the FR-033 grace |
| X-2 | FR-018 empty actor | `by an operator` | Some sessions have no display name |
| X-3 | FR-039 link | Docker actions → `/docker/activity?host=…&command=…` (URL-encoded); host and agent actions → a modal with `CommandProgress` (`showActivityLink=false`) | No host-command detail page exists |
| X-4 | FR-035 gaps | `Shutting down` (succeeded, still online); `Patched, settling until <t>`; `Restarting agent, back by <t>` (online) | The states exist; the wording is for Ryan's review |
| X-5 | FR-003 "back online" | Reboot and agent_update count from an observed return after F; os_update from max(F, onlineSince); never before `graceEnd` | Offline detection lags ≤ 75 s |
| X-6 | FR-044 trigger | The online event only on registration (today's trigger); a heartbeat return updates state silently | Unchanged trigger |
| X-7 | FR-022 "observed running" | A `container_started` event, or a newer snapshot with `running`, compared in `ContainerNameKey` form | Made precise |
| X-8 | FR-024 cooldown | Escalations neither check nor start rule cooldowns. Pushes are bounded instead by the per-device ceiling (AD-010) | Avoids hiding unrelated alerts |
| X-9 | FR-015 / AC-010 | Exclusion before coverage | Matches the outside-window behavior |
| X-10 | A-007 | Step token `wait_dpkg_lock` reserved | EXPECT-002 adds it |
| X-11 | NFR-009 | Unreadable = not loaded, reconcile stale for more than 90 s, or loop not live for more than 15 s | AD-002 |
| X-12 | Upgrade | Terminal commands older than 1 h are lapsed by the migration | Avoids stale escalations |
| X-13 | FR-014 | Two old generator tests retired | They test removed behavior |
| X-14 | FR-029 "while powered off, no offline alert" | Powered-off ends at any evidence of life; a later transition alerts (C-1) | A device that came back is not powered off |
| X-15 | FR-033 grace | Shortened during a rapid-restart series; total ≤ 4 min (C-7) | Bounds alerting blindness |
| X-16 | FR-023 notify | An escalation's push can be held by the per-device ceiling; the event is always stored (C-5) | Flood bound |

## Residuals (in addition to the FRD's NFR-011)

- **R-2.** An `os_update` that reboots more than about 105 s after its result can close by settle first; the later offline then alerts normally.
- **R-3.** A push dropped because the publisher queue was full or the 30 s deadline passed, or a crash after the commit, loses that one push. The event is stored.
- **R-4.** A reboot that reports "initiated" but never happens keeps container-death suppression until the 15-min bound.
- **R-5.** The push ceiling is in memory, so a server restart resets it. The restart series (AD-008) bounds how much a restart loop can contribute.
- **R-6.** A backend that ignores its context can occupy one of the two publisher workers indefinitely. Further pushes queue, then drop with a Warn. The loop and the events are unaffected.

R-1 from revision 1 is removed: pending windows are now exempt from deletion.

## Open points for Ryan

- **S-1, S-2, S-3 decisions** (Security addendum). Clod recommends keeping them.
- **X-4 label wording.**
- **The 1 h first-deploy lookback (X-12).**
- **Restart-grace escalation timing (X-1).**
