# Architecture Decision Document

- **Story ID:** EXPECT-002
- **FRD Reference:** `docs/requirements/EXPECT-002-frd.md` (revision 1, commit 4730048; DEC-1 to DEC-5 at the recommended option)
- **Parent design:** `docs/architecture/EXPECT-001-add.md` (revision 2: AD-001 to AD-016, security conditions C-1 to C-8)
- **Builds on:** CMD-LIFECYCLE-001 (commands record, state machine, sweeper), EXPECT-001 (Tracker, windows, `finishWindow`, escalation markers, publisher and push ceiling, activity API, retention exemption), AGENT-HARDEN-001 (signed self-update path), REG-HARDEN-001 (re-registration proof)
- **Author:** Architect Agent
- **Date:** 2026-10-26
- **Status:** DRAFT, revision 1 (for security review, then implementation)

## Changelog

### Revision 1

The first version.

---

## 1. Summary

Each accepted agent self-update becomes one **`commands` row** with `action = 'agent_update'` and a new origin, **`agent_self_update`**. The server creates the row directly in `running` with `dispatched_at = started_at = receive time` and `deadline_at = start + 10m`. A database CHECK forbids the row from ever being `queued`, so it can never be claimed, sent, expired, reverted or cancelled. Every agent or operator transition carries a guard `origin <> 'agent_self_update'`, so an agent can never report against it.

Because the row is an `agent_update` command row, EXPECT-001 picks it up without a parallel system. That covers the Tracker hook, the pending-window loader, the deletion exemption, the escalation markers, the publisher and push ceiling, the activity API and labels, and restart durability. The Tracker selects a dedicated window policy for the new origin. Its coverage:
- covers `device_offline` only;
- has no settle and no post-terminal period;
- ends at the registration that reports the new version, at an attributed failure, at an attributed completion once a return has been observed, or at the 10-minute bound, whichever is first.

The Tracker's single `finishWindow` routine resolves the outcome at the bound and runs the backstop. The backstop escalates once through the `device_offline` rule when the suppressed offline is still ongoing. It also writes the one outcome event per record, exactly once, through the existing marker table.

Recognition is server-only. It parses the existing `agent_update_started` message (strict grammar, untrusted input) in the agent-event handler. Command attribution, the one-open-record rule and the 6-per-24h cap are checked and the row is inserted in **one transaction** under the per-device command advisory lock. A small server change and one dashboard label are all this story needs: no agent change, no `install.sh` change, no new dependency. It works with deployed agents v2.78.0 to v2.80.0.

## 2. Technical Context

### 2.1 What exists (verified at 4730048, main c55e4bd plus the FRD)

| Area | Where | Relevant facts |
|---|---|---|
| Self-update, old process | `internal/agent/updater.go:124-218` | First check at start + 30 s (`updateStartupDelay`, l.59), then every 6 h (`updateCheckInterval`, l.58). Sends `agent_update_available` (l.168), takes the host lock `automatic-agent-update` (l.187), sends `agent_update_started` "Agent update started: %s → %s" (l.199), then `apply`. Signed path: `applyStaged` returns `errUpdateScheduled` on `accepted` (l.471) and the lock is kept (l.204). A pre-apply error sends `agent_update_failed` "Agent update failed: %v" (l.209). The `installed` fast path (l.472-473) and pre-AGENT-HARDEN agents send `agent_update_completed` "Agent updated to %s" **before** `restart()` (l.214-217). No event carries a command ID (`reportUpdateEvent`, l.598-611) |
| Self-update, new process | `internal/agent/agent.go:249,280`; `internal/agent/signed_update_handoff.go:104-151` | `register()` runs **before** `recoverAutomaticSignedHandoff()`, so the new process registers first. `finishSignedHandoff` with `CommandID == ""` sends `agent_update_completed` "signed installation completion verified against running executable" or `agent_update_failed` (l.127-133). With a command ID it completes the **command** (a command result, l.109-126) and sends **no** agent event |
| Command path | `internal/agent/commands.go:1071-1133` | `handleTriggerUpdate` sends `agent_update_started` (l.1111). It sends `agent_update_failed` only on a pre-apply error (l.1118), and `agent_update_completed` "Agent updated to X" only on the unsigned or `installed` path (l.1123). It returns `pending` for a signed handoff (l.1116). No command ID on any of these events |
| Agent registration callers | `internal/agent/agent.go:249` (startup), `:438-472` (`reregisterLoop`, every 24 h) | These are the only two callers of `register()`, and `register.go:164` is the only call of `client.Register`. A dropped WebSocket, a 401 or a 404 never re-registers. A failed startup registration is retried only by the 24 h loop |
| Agent event endpoint | `internal/server/handlers/handlers.go:1165-1201` | Device-key auth (`middleware/auth.go:31-36` rejects a key that does not match `{id}`). Validates the type, stores the event as received (`EventRepo.Create`: sanitize, 2048-byte cut, redact), broadcasts. `CreatedAt: time.Now()` (not the injected clock). No rule routing, no notification |
| Registration | `handlers.go:324-416` | Existing-device path: proof first, `devices.Update` (stores `reg.AgentVersion`), `activity.NoteRegistration` (l.389), response, broadcast, then `eventGen.DeviceOnline` only when the device was not live (l.412-414) |
| Heartbeat | `handlers.go:474-539`; `db/device_repo.go:136-145` | `UpdateHeartbeatTime` also stores the heartbeat's agent version and sets `online` |
| Command state machine | `internal/models/command_state.go:111-145` | Server edges: claim, expire, revert, `→ timed_out`. Operator: `queued → cancelled`. Agent: everything else, including `timed_out → succeeded/failed` (a late result). No server edge reaches `succeeded` or `failed` |
| Transition SQL | `internal/server/db/command_repo.go:367-446` | One guarded UPDATE. An agent edge also fills `dispatched_at` and `deadline_at` (l.428-435) |
| Origin CHECK | migration `000023_command_lifecycle.up.sql:36` | `commands_origin_check`: `user`, `auto_update`, `auto_patch`. `models.IsValidOrigin` gates `Dispatch` (`service.go:177`) |
| Sweeper | `commands/sweeper.go:89-121` | `ListOverdue` (in-flight past `deadline_at`) → `timed_out` → `afterSweep`: policy outcome, broadcast, **`CommandCompleted` event** (`generator.go:284-311`) |
| Report rejection | `commands/service.go:742-765` | `classifyRejected` reveals the row's status (409) for a row of the device |
| EXPECT-001 Tracker | `internal/server/expect/*` | `CommandChanged` (`tracker.go:300`) uses `ExpectPolicy(c.Action)`. Agent windows take `fromVersion` from the device. `evaluate` (`window.go:106`): the agent class needs an observed return after F. `finishWindow` (`finish.go:132`): device backstop, container check, guarded close. Eager FR-020 in `plan` (`tick.go:102`). Labels: `activity.go`, `web/src/utils/activity.ts:68-73` ("Updating agent <from> → <newest>") |
| Deletion exemption | `db/expect_repo.go:320`, `docker_retention.go:29`, `storage/command_budget.go:28` | `NOT (window_closed_at IS NULL AND action IN (<ExpectActions>))`, which already includes `agent_update` |
| Marker table | migration 000033 `command_escalations` | PK `(command_id, target)`, target ≤ 64 bytes, `ON DELETE CASCADE` |
| Command history (web) | `web/src/components/ActivityLog.tsx`, `components/docker/CommandProgress.tsx` | The table shows the action label, status and result summary. Cancel appears only for `queued`. There is no retry control and no retry endpoint (`router.go:415-420`) |

### 2.2 What is missing

- The server does not recognise a self-update.
- No record and no window exist for one.
- No attribution between agent update events and records or commands.
- No outcome events for a self-update that fails or does not come back.
- No version-bearing labels.

### 2.3 What must change

See Section 4. No agent file, no `install.sh`, no sudoers file and no dependency changes.

---

## 3. Architecture Decisions

### AD-001: A self-update record is a `commands` row with origin `agent_self_update`

**Decision.** One accepted start produces one row:

| Column | Value |
|---|---|
| `action` | `agent_update` |
| `origin` | `agent_self_update` (`models.OriginAgentSelfUpdate`) |
| `status` | `running` |
| `created_at`, `updated_at`, `dispatched_at`, `started_at` | the server receive time of the start (S) |
| `deadline_at` | S + 10m |
| `exec_deadline_s` | 600 |
| `expires_at` | S |
| `target_keys` | `{}` |
| `params` | `{"from_version": <from>, "to_version": <to>}` |
| `step` | `waiting_for_restart` |
| `actor_kind` | `system` |
| `actor_name`, `actor_auth`, `actor_sub` | `''` |
| `policy_id` | NULL |

No `docker_audit` row is written.

Migration 000034 widens `commands_origin_check` and adds three database guards:
- the CHECK `commands_self_update_shape`: a self-update row is always an `agent_update`, is never `queued` (or `pending`), `dispatched`, `acknowledged`, `cancelled` or `expired`, and always has `dispatched_at` and `deadline_at`;
- a unique partial index, so a device has at most one row in `running` or `verifying`;
- a partial index for the 24 h count.

`models.IsValidOrigin` is **not** widened, so `Service.Dispatch` (every API, fleet, auto-update and auto-patch path) can never create one.

**Rationale.**
- The FRD (7.1) and the brief require reuse of EXPECT-001 without a parallel window system. EXPECT-001 derives every window from a `commands` row (its AD-001). A row with `action = 'agent_update'` is therefore already:
  - covered by `idx_commands_window_pending`;
  - loaded by `ListOpenWindows`;
  - exempt from deletion while pending;
  - a valid FK for `command_escalations` markers;
  - shown in the device command history (FR-027, DEC-5).
- Keeping the action unchanged means no index predicate, no `ExpectActions()` list and no PG predicate test changes.
- The origin column is the discriminator that every path can filter on, in SQL, with a typed constant.

**Alternatives considered.**
- *A separate `self_updates` table plus its own window code.* Rejected: it duplicates the window lifecycle, the markers (FK to `commands`), the retention exemption and the history view. That is the parallel system the brief forbids.
- *A new action `agent_self_update`.* Rejected:
  - it changes the EXPECT-001 partial-index predicate, which needs an index rebuild in the migration;
  - it changes `ExpectActions()`, the retention exemption list and the PG predicate test;
  - the web needs a second action vocabulary for the same human action ("Agent update").
- *Insert the row as `queued` and claim it immediately.* Rejected: it would sit in the claimable set for a moment, and `ClaimQueued` would deliver it to the agent.
- *No row at all: the start event only opens an in-memory window.* Rejected: it fails NFR-006 (durability) and FR-027 (history).

**Consequences.**
- Every command path must respect the new origin. AD-007 enumerates them and states what each does (Q-3).
- `target_keys = {}` means a self-update row never conflicts with, and never blocks, any command. A reboot, a patch or a fleet agent update can still be dispatched while a self-update record is open (FR-028).
- The down migration must delete self-update rows (the narrowed CHECK cannot hold them). History is lost on rollback (Section 5.2).

### AD-002: Recognition and command attribution in one transaction

**Decision.** `ReceiveAgentEvent` routes the three types `agent_update_started`, `agent_update_completed` and `agent_update_failed` through `commands.Service.NoteAgentUpdateEvent` before it stores the event. It does so only when `authenticatedDeviceID(r)` is non-empty and equals `{id}`. The service returns a link (`command_id`, `command_attr`) and, at most, a cap event to broadcast. It never returns an error: every failure yields an empty link and one throttled Warn (NFR-009). The agent's event is then stored exactly as today, plus the link, with `CreatedAt` = `h.clock()`.

**Start (`agent_update_started`):**
1. `models.ParseSelfUpdateStart(raw)` (AD-011). Invalid → no link, no record (FR-003). One `Debug`.
2. `SelfUpdateRepository.CreateSelfUpdate(ctx, start)` runs **one transaction**:
   1. `pg_advisory_xact_lock(hashtextextended('riot.cmd.' || device_id, 0))`. This is the same key as `CreateIfNoConflict`, so it serialises with every command creation on the device, including the `agent_update` command (host key).
   2. **FR-004.** Select the newest `agent_update` row of the device with `origin <> 'agent_self_update'`, `dispatched_at IS NOT NULL`, and either a status in the in-flight list (`dispatched`, `sent`, `acknowledged`, `running`, `verifying`) or `finished_at >= S − 10m`. Found → disposition `attributed_command`, link = that command. A queued command (`dispatched_at` NULL, including after a revert) never counts.
   3. **FR-006.** Select the device's self-update row in `running` or `verifying`. Found → disposition `open_record`, link = that row. The window is neither extended nor reopened.
   4. **FR-007.** Count the device's self-update rows with `created_at > S − 24h`. If the count is ≥ 6, the disposition is `capped`: no link. If no `agent_self_update_paused` event exists for the device with `created_at > S − 24h`, insert one (AD-012) and return it for broadcast.
   5. Insert the row (AD-001). Disposition `created`, link = the new row.
3. `created` → `Service.broadcast(row)`. That runs the Tracker hook (EXPECT-001 AD-004) and the dashboard `command_update`.
4. Link attribute: `inferred` (`models.CommandAttrInferred`; the server inferred it, the same value Docker attribution uses).

**Completed or failed:** see AD-004.

**Rationale.**
- Doing all four checks plus the insert under the per-device lock makes them atomic:
  - 50 concurrent starts produce exactly one row (AC-017);
  - the cap event is written once;
  - a command claimed in parallel cannot slip between the check and the insert, except for a claim that does not take the lock. A heartbeat claim is that case, which is residual R-4.
- All three facts are durable, so the cap and attribution survive restarts (NFR-006 (d)) and need no memory (NFR-004).

**Alternatives considered.**
- *In-memory dedupe and rate cap.* Rejected: they are not restart-safe and give no DB-level bound.
- *Recognition inside the Tracker.* Rejected: the Tracker does no request-path DB writes, and command creation belongs to the single writer.

**Consequences.**
- One extra PK read (hostname) and one short transaction per `agent_update_*` event. At Ryan's scale this means a handful per release.
- The agent-supplied `command_id` on these three types is **ignored**, as today, where it is not stored. Attribution never trusts it (X-2).

### AD-003: Outcome model mapped onto the existing state machine, with a new restricted actor

**Decision.** The FRD outcome states map to command states:

| FRD (7.3) | Command state | `step` | `failure_step` |
|---|---|---|---|
| Open, window open | `running` | `waiting_for_restart` | |
| Closed, outcome pending (registration reported <to>) | `verifying` (`verifying_at` = registration time) | `waiting_for_confirmation` | |
| `succeeded` | `succeeded` | `confirmed` | |
| `failed` (agent reported) | `failed` | | `agent_failed` |
| `failed` (came back on another version) | `failed` | | `wrong_version` |
| `not confirmed`, device online at the bound | `timed_out` | | `unconfirmed_online` |
| `not confirmed`, device offline at the bound | `timed_out` | | `unconfirmed_offline` |

All tokens match `models.StepPattern`.

- New actor **`models.ActorSelfUpdate`** ("server acting on a device's own self-update evidence"). Its only edges:
  - `running → verifying`;
  - `running → succeeded`;
  - `running → failed`;
  - `verifying → succeeded`;
  - `verifying → failed`.
- The bound's `running → timed_out` uses the existing `ActorServer` edge.
- `buildTransitionSQL` appends an **origin guard**, rendered from the typed constant:
  - `ActorAgent` and `ActorOperator` → `AND origin <> 'agent_self_update'`;
  - `ActorSelfUpdate` → `AND origin = 'agent_self_update'`. `DeviceID` is mandatory for it, as for `ActorAgent`.
- `ActorServer` gets no guard. Its edges cannot touch a self-update row except `→ timed_out`, because the row is never `queued` and never `dispatched`.

**Side effects.** A self-update transition goes through new `Service` methods (Section 6.5). Each does `Transition` then `broadcast` (observer plus dashboard), and **never** `completed()` (no `command_completed` event) or the policy recorder. The `result_msg` of each transition is the server-rendered text of AD-012, at most 512 bytes.

**Rationale.**
- The history needs a real status (FR-027).
- `verifying` is the natural name for "returned, confirming".
- A dedicated actor keeps the CMD-LIFECYCLE rule "every status change is one guarded Transition" and keeps the agent's `timed_out → succeeded` late-result edge away from these rows.

**Alternatives considered.**
- *Use `ActorAgent` for the server's outcome writes.* Rejected: the agent guard must reject every agent write on these rows (Q-3).
- *A new status or new columns.* Rejected: they are not needed, and they would widen every status list.

**Consequences.** The existing transition-table tests for the three existing actors must pass unchanged. The new actor is tested on its own (`TestEX2_AC024_ActorSelfUpdateEdges`).

### AD-004: Attribution of completed and failed events (Q-2)

**Decision.** `NoteAgentUpdateEvent` with `agent_update_completed` or `agent_update_failed`:
1. `LatestSelfUpdateSince(device, at − 10m)` returns the newest self-update row of the device with `created_at ≥ at − 10m`, or none.
2. None → no link, no record, no server event (FR-014, AC-021). These events never create a record. They are **not** linked to commands: the FRD as written (X-16).
3. Found:
   - **Outcome open** (status `running` or `verifying`):
     - completed → `ActorSelfUpdate` to `succeeded` (`result_msg` = `completed: ` + `SafeText(message, 200)`);
     - failed → `ActorSelfUpdate` to `failed` with `failure_step = agent_failed` and `result_msg` = the FR-018 message (AD-012; reason = `SafeText(message, 200)`).
   - **Outcome set** → link only, no transition, no event (FR-022). A guarded transition that does not apply (race) also falls back to link only.
4. Link attribute `inferred`.

**Why the two attribution rules never claim the same event (Q-2).**
- Each event has exactly one `command_id` column, so it is linked to one subject at most.
- A start is resolved command-first (AD-002 step 2). A completion or failure is resolved record-only (step 1 above).
- A record is never created while an attributable `agent_update` command exists. A record and a command can only coexist when the command was dispatched **after** the record started (FR-028).
- In that period the agent holds the host lock `automatic-agent-update`. The old process keeps it through the handoff (`updater.go:204`). The new process re-takes it in `recoverAutomaticSignedHandoff` (`signed_update_handoff.go:146`). The command therefore either fails at the lock with a command result and no agent event, or finds the agent already up to date and succeeds with no agent event.
- A signed **command** update's completion is a command result, never an agent event (`signed_update_handoff.go:109-126`).
- So in practice no command-path `agent_update_completed` or `agent_update_failed` reaches the server inside a record's 10 minutes. If one does, it is attributed to the record. That can only close the record early or mark it `failed`, which fails toward alerting (residual R-3).

### AD-005: The self-update window policy and its coverage function

**Decision.** `models.ExpectSelfUpdatePolicy`:

| Field | Value |
|---|---|
| Class | `agent` |
| CoversOffline | yes |
| Containers | none (FR-010) |
| Settle | 0 |
| PostTerminal | 0 |
| RequireObservedReturn | no |
| EscalateNotBack | yes |
| CheckContainers | no |
| `SelfUpdate` (new field) | true |

`models.ExpectPolicyFor(c Command)` returns it for `origin = agent_self_update` (and `action = agent_update`). Otherwise it returns `ExpectPolicy(c.Action)`. Every Tracker call site that picks a policy for a row uses `ExpectPolicyFor`:
- `CommandChanged`;
- `mergeLocked` (twice);
- the reconcile's escalation-label rebuild.

`ExpectPolicy(action)` and the 12-row table are unchanged (EXPECT-001 AC-008 stays green).

**Coverage function** (pure; `expect.evaluateSelf`, called from `evaluate` when `policy.SelfUpdate`).

Notation:
- D = `dispatched_at` = S;
- `hard` = `deadline_at` = D + 10m (fallback D + `exec_deadline_s`; never later than D + 10m);
- V = `verifying_at`;
- F = `finished_at`;
- R = the Tracker's `returnedAt` (registration, offline→online, or Load).

| Row state | Phase | Offline coverage ends (exclusive) | `closeAt` (entry processed) | Label |
|---|---|---|---|---|
| `running` | `in_progress` | `hard` | `hard` (reason `hard_bound`) | yes ("Updating agent <from> → <to>"; `hard` feeds only the internal timeline, the activity response omits `back_by`, AD-009) |
| `verifying` | `closed` | `min(V, hard)` | `hard` (outcome resolved at the bound) | no |
| `succeeded`, V set (return seen before completion: the signed flow) | `closed` | `min(V, hard)` | F (due now) | no |
| `succeeded`, V NULL, R > F (return observed after the completion) | `closed` | `min(R, hard)` | `min(R, hard)` | no |
| `succeeded`, V NULL, no return after F yet | `in_progress` | `hard` | `hard` | yes |
| `failed`, `timed_out` | `closed` | `min(F, hard)` | F | no |
| any, due and 24 h past `closeAt` | | | reason `lapsed` | |

Coverage is strict at its end. Nothing covers at or after `hard` (FR-012, NFR-002).

**How the window closes promptly without hiding a later outage (the brief's question).**
- The EXPECT-001 agent return phase and its "observed return after F" rule are **not** used. That rule exists because a command's result precedes its restart. A signed self-update's completion follows the registration.
- Coverage ends **at the registration** that reports <to> (V, FR-011 c): from that instant a new offline transition alerts normally (FR-013, AC-004).
- It ends at once at an attributed failure (F, FR-011 b) or at a registration on another version (F, FR-011 d).
- Completion closes it at once when a return has already been seen (V set). That is the normal signed flow (AC-001).
- Only a completion that arrives **before** any return keeps coverage until the first observed return after it, capped at `hard`. These are the `installed` fast path and pre-AGENT-HARDEN agents, which send "Agent updated to X" and then restart (X-1). This is the one case where the restart is still to come. Without it the restart's offline would alert.
- There is never a settle period. The worst case for hiding a real outage is the bound itself: 10 minutes from S.

**Selection with other windows (FR-028).** Coverage and labels use EXPECT-001's rules unchanged: the latest D wins. A command dispatched after the record started wins both the label and the link (AC-025, X-12).

### AD-006: Ending a self-update window: `finishSelfUpdate`, always with the backstop

**Decision.** `finishWindow` delegates to `finishSelfUpdate` when `policy.SelfUpdate`. It keeps the EXPECT-001 contract:
- it is the only exit;
- every step is idempotent;
- a failing step leaves the entry for the next tick (and eviction after 5 min, reload later);
- nothing runs before `graceEnd`, because `Tick` plans nothing before it.

Steps, outside the lock:
1. **Device backstop (FR-020; X-7).**
   - Condition: grace over, the device is offline now, and its current offline began inside the coverage (`offlineSince ∈ [D, offlineEnd)` from `evaluateSelf`).
   - Then `escalate` with marker `device` (exactly once, through the `device_offline` rule, ceiling and publisher: EXPECT-001 AD-010).
   - Kind and text:
     - row `running`, or `succeeded` without a return → `device_not_back`, FR-020 text;
     - row `failed` → `device_nonsuccess`, failed-variant text (AD-012).
   - It records the dashboard escalation (`markDeviceEscalated`, AD-009). It runs **before** the outcome is resolved, so a resolution failure never delays the page.
2. **Resolve the outcome at the bound** (row still `running` or `verifying`; only reached with reason `hard_bound` or `lapsed`), through `SelfUpdateResolver.ResolveSelfUpdate`:
   - `verifying` → `succeeded` (`ActorSelfUpdate`; `result_msg` `confirmed: the agent registered on <to>`);
   - `running`, device online, and its stored agent version matches <to> → `succeeded` (`confirmed: the agent reports <to>`; X-3);
   - `running`, device online → `timed_out`, `failure_step = unconfirmed_online`, `result_msg` = the FR-021 text;
   - `running`, device offline → `timed_out`, `failure_step = unconfirmed_offline`, `result_msg` = `not confirmed: <host> was offline 10m after the start`.

   A transition that does not apply (an event won the race) is followed by a re-read. If the row is still not terminal → `noteFailure`, retry next tick.
3. **Outcome event (FR-018, FR-019, FR-021; FR-022).**
   - Re-read the row (`CommandReader.GetByID`).
   - When `failure_step` ∈ {`agent_failed`, `wrong_version`} → event type `agent_self_update_failed`.
   - When it is `unconfirmed_online` → `agent_self_update_unconfirmed`.
   - Severity `warning`, message = `result_msg`, `command_id` = the row, `command_attr` = `outcome`.
   - Stored through `Repo.RecordEscalation` with marker target **`outcome`**, which gives exactly once across retries and restarts. On insert it is queued to the publisher as a broadcast-only job (`rule = nil`, never dispatched).
4. `CloseWindow(id, now, closed|escalated)` (guarded), then `dropWindow` (the ID is kept in `closedRecent` for 5 min).

**Eager FR-020** (`tick.go` `plan`) skips `SelfUpdate` windows. Their failed or timed-out rows are due at F anyway (one tick).

**Rationale.** It keeps EXPECT-001 C-2: "no window ends without its backstop". The outcome writes are derived from durable row fields (status, `failure_step`, `result_msg`, `params`), so a crash between any two steps is harmless (NFR-006).

**Why the Tracker, not the sweeper, resolves the bound.** The sweeper's `afterSweep` writes a `command_completed` event, which FR-022 forbids. It would also time out a `verifying` row, which must end `succeeded`. And it would race the Tracker by up to 60 s. The sweeper therefore excludes self-update rows (AD-007, path 7). The Tracker already is the durable backstop: an active row is loaded on every reconcile, whatever its age. So a row past its bound is resolved on the first tick after `graceEnd` once the server runs.

### AD-007: Every command path honours "never sent to an agent" (Q-3)

| # | Path | What it does with a self-update row |
|---|---|---|
| 1 | `Service.Dispatch` → `CreateIfNoConflict` (API, fleet, auto-update, auto-patch, bulk Docker) | It can never create one: `IsValidOrigin` rejects the origin. A self-update row holds no target keys, so it is never a conflict holder and never blocks a command |
| 2 | `DispatchBulkDocker` (`ListActive` held keys) | No keys, so it is ignored |
| 3 | `deliver` → `ClaimByID` (WebSocket send) | `queued` only. The row is never `queued` (CHECK), so it is never claimed or sent |
| 4 | `ClaimForHeartbeat` → `ClaimQueued` | Same: never returned in `pending_commands` |
| 5 | `expireQueued` / `ListExpiredQueued` (sweeper and heartbeat) | `queued` only → never |
| 6 | `revertDispatch` (FR-085) | From `dispatched` only → never. The CHECK also rejects a self-update row in `queued` |
| 7 | Sweeper `timeOutOverdue` / `ListOverdue` | `ListOverdue` adds `AND origin <> 'agent_self_update'`. `timeOutOverdue` also skips such a row defensively. The bound is resolved by the Tracker (AD-006) |
| 8 | Agent report: HTTP `POST /api/v1/devices/{id}/commands/{commandId}/report` and WebSocket `command_ack` / `command_progress` / `command_result` (`terminal.go:353`), both through `HandleAgentReport` | The `ActorAgent` origin guard means the transition never applies. `classifyRejected` returns `not_found` for a self-update row, so the response is indistinguishable from a missing or foreign command: HTTP 404 `command_not_found`, WebSocket receipt `not_found`. No `completed()`, no policy, no broadcast. This includes the `timed_out → succeeded` late-result edge |
| 9 | Operator cancel `POST .../commands/{commandId}/cancel` | The `ActorOperator` guard means it never applies. The response is 409 `not_cancellable` with the current status (the operator can already see the row in the history). The web shows no cancel control (it renders cancel only for `queued`) |
| 10 | Retry | No retry endpoint exists (`router.go:415-420`) and the web has no retry control. Nothing to change |
| 11 | `ListByDevice`, `ListByDeviceFiltered`, `ListActive`, `GetCommand`, `GetForDevice` (admin API) | Returned read-only. The web renders them with `SelfUpdateDetails` (FR-027) |
| 12 | `GetCommandOutput` | No output rows, so it returns `[]` |
| 13 | `LatestFinishedOSUpdates` (patch badge) | `os_update` only → never |
| 14 | `DockerRepo.ActivityCommands` | `docker_%` only → never |
| 15 | Docker event attribution (`docker_attribution.go`) | No target keys → never attributed |
| 16 | Docker inventory refresh `EXISTS` | `docker_inventory_refresh` only → never |
| 17 | Retention (`docker_retention.go`) | A terminal, processed self-update row is deleted at 90 d like any command. An unprocessed row (`window_closed_at IS NULL`, action `agent_update`) is exempt through the existing predicate (bounded, AD-013) |
| 18 | Storage budget (`purgeCommandResults`) | Same exemption. The row has no output and no `result_bytes`, so only the age-based purge can remove it |
| 19 | EXPECT-001 loaders (`ListOpenWindows`, `ListDueBacklog`, `ListLapseCandidates`, `CloseWindow`) | Included on purpose (action `agent_update`). Its policy comes from `ExpectPolicyFor` |
| 20 | Escalation markers | Targets `device` and `outcome` only. ≤ 2 rows per record, cascade |
| 21 | Policy outcome recorder, `CommandCompleted` event | Never called for a self-update row (AD-003) |
| 22 | `command_update` WebSocket broadcast | Sent to dashboards (history refresh). Agents never receive dashboard broadcasts |
| 23 | Device deletion | Cascades |
| 24 | Creation rate gate (`creationGate`) | Not used (rows are created outside `Dispatch`). FR-007 bounds them instead |

**Actor phrase (Q-3).** `models.ActorPhrase` gains `case OriginAgentSelfUpdate: return "by agent auto-update"`.

### AD-008: Registration hook (FR-011 c, d; FR-015; FR-019) and the Q-1 decision

**Decision.** In `RegisterDevice`, on the existing-device path only, after `devices.Update` and `activity.NoteRegistration` and **before** `eventGen.DeviceOnline`, the handler calls:

`Service.NoteSelfUpdateRegistration(ctx, device.ID, SafeName(reg.Hostname), reg.AgentVersion, h.clock())`

1. `OpenSelfUpdate(device)`. None, or status ≠ `running`, or `at ≥ deadline_at` → nothing. A registration after the bound never counts (X-15).
2. `AgentVersionsMatch(reg.AgentVersion, to)`:
   - match → `running → verifying` (`ActorSelfUpdate`, `step = waiting_for_confirmation`, `verifying_at = at`);
   - no match → `running → failed` (`failure_step = wrong_version`, `result_msg` = the FR-019 text; the reported version is shown only if it passes Section 7.2, else as `an unrecognised version`: X-4).
3. On every write: `broadcast`. The Tracker sees the new state synchronously, before `DeviceOnline` → `NoteOnline` runs.

**`NoteOnline` for self-update windows.** A note is produced only when the offline that just ended began inside the window's coverage (`offlineSince ∈ [D, offlineEnd)` as evaluated at `at`). The message names the versioned action label (FR-017). Because the transition already happened, `offlineEnd` is V or F = `at`, which is after `offlineSince`.

**Q-1 decision: FR-019 as written, no extra "after an offline" guard.**
- The code shows exactly two registration callers: startup and the 24 h `reregisterLoop` (Section 2.1).
- Self-update checks run at T0 + 30 s + 6 h·k, and re-registrations at T0 + 24 h·k. So a periodic re-registration precedes the aligned update check by about 30 s and can never fall between that start and the restart.
- No reconnect, 401 or 404 path re-registers.
- A guard such as "only after an offline transition" would keep suppression alive after a fast rollback restart (< 45 s, never offline). That fails toward silence.
- The residual (NFR-011 fourth bullet) stays, and it fails toward alerting.

### AD-009: Activity, labels and the escalated state

**Decision.**
- `models.DeviceActivity` gains **`agent_to_version`** (string; `""` for command windows).
- For a self-update window in `in_progress`:
  - `kind = agent`, `phase = in_progress`, `action = agent_update`;
  - `command_id` = the row, `actor_phrase = "by agent auto-update"`, `started_at = S`;
  - `agent_from_version` and `agent_to_version` from `params` (re-validated by Section 7.2; an invalid pair gives `""`, and the label falls back to "Updating agent");
  - `covers_offline = true`, `device_online`.
- `back_by` is omitted: the FR-024 label has none.
- The `window` struct gains `selfFrom` and `selfTo`, filled when the row is admitted (`CommandChanged`, `mergeLocked`). `trimCommand` also copies `VerifyingAt`.
- `openEscalation` gains `fromVersion` and `toVersion`. For a self-update row the dashboard escalation has:
  - `reason = not_back` (or `failed` for the failed variant);
  - `bound = "10m"` (`models.ExpectSelfUpdateBound`);
  - the versions, both from the live escalation and from the reconcile rebuild (which reads the row's `params`). Today the rebuild maps `timed_out` to `timed_out`; for self-update rows it maps to `not_back` instead.
- The escalated `DeviceActivity` carries `agent_from_version` and `agent_to_version`.

**Web (`activity.ts`):**
- agent, `in_progress`, `agent_to_version` non-empty → `Updating agent <from> → <to>`. Otherwise the EXPECT-001 rule applies unchanged (FR-029).
- escalated with both versions → `Not back <bound> after Agent update <from> → <to>`, and `Agent update <from> → <to> failed: offline` for the failed reason (FR-023).

**FR-025:** `isExpectedOffline` is unchanged (agent, `covers_offline`).

**FR-027:** `activityLink` is unchanged (agent → modal). The modal's `CommandProgress` and the history's `CommandDetailModal` render **`SelfUpdateDetails`** for `origin = agent_self_update`:
- the action label with versions and `by agent auto-update`;
- the start time;
- the outcome (`running` "In progress", `verifying` "Returned, confirming", `succeeded` "Succeeded", `failed` "Failed", `timed_out` "Not confirmed");
- `result_msg` as text;
- the linked agent and server events from `GET .../commands/{commandId}/events` (Section 6.3).

There are no action controls.

### AD-010: Bound, rate cap and the abuse budget (FR-007, NFR-010)

| Constant | Value |
|---|---|
| `ExpectSelfUpdateBound` | 10m (= `ExpectAgentReturnBound`) |
| `SelfUpdateAttributionWindow` | 10m (FR-004, FR-014) |
| `SelfUpdateRateCap` | 6 per `SelfUpdateRateWindow` = 24h |

The cap stays at the FRD's 6. The abuse budget is ≤ 6 × 10 = 60 minutes of delayed offline alerting per device per rolling 24 h. Every covered transition is still stored as an info event, and an offline device escalates at each bound. The cap is computed from durable rows, so restarts do not reset it (X-13 notes the emergency-purge caveat).

### AD-011: Parsing and validation of untrusted agent text

**`models.ParseSelfUpdateStart(raw string) (from, to string, ok bool)`:**
1. If `len(raw) > 4096` → reject. This cap applies before any other work (AC-016's 10 KB message).
2. `s = strings.TrimSpace(raw)`. Reject if `len(s) > 256`, or `!utf8.ValidString(s)`, or `s` contains any rune of category Cc, Cf, Zl or Zp. This is stricter than "control characters" (X-11).
3. `s` must start with exactly `Agent update started: ` (case-sensitive). The remainder must contain exactly one ` → ` (space, U+2192, space). The parts before and after are <from> and <to>.
4. Each must pass `ValidAgentVersion`: 1 to 32 bytes, matching `^v?[0-9]{1,5}\.[0-9]{1,5}\.[0-9]{1,5}([-+][A-Za-z0-9.-]{1,16})?$`.
5. `AgentVersionsMatch(from, to)` must be false. Matching means equal after removing one leading `v` from each.

**`models.SafeText(s, max)`** is `SafeName`'s algorithm with a byte limit. `SafeName = SafeText(s, 128)`, unchanged behaviour. Failure reasons use `SafeText(message, 200)`. Hostnames use `SafeName`.

Versions shown to Ryan are the validated strings, exactly as received (FRD 7.2). A version reported at registration is shown only if `ValidAgentVersion`; otherwise it reads `an unrecognised version`.

### AD-012: Events and messages (FR-016 to FR-021, FR-007)

New event types (`models/events.go`). None is routed through rules and none is ever dispatched:

| Type | Severity | Written by | Bound |
|---|---|---|---|
| `agent_self_update_failed` | warning | Tracker step 3, marker `outcome` | ≤ 1 per record |
| `agent_self_update_unconfirmed` | warning | Tracker step 3, marker `outcome` | ≤ 1 per record (exclusive with the previous row) |
| `agent_self_update_paused` | warning | `CreateSelfUpdate` tx | ≤ 1 per device per rolling 24 h |

New `command_attr` values: `inferred` (now a named constant; the value already exists) and `outcome`.

**Templates.** `<host>` = `SafeName(hostname)` (fallback `device`). `<from>`, `<to>` = validated. `<id8>` = `ShortCommandID`. Times are `15:04` in `time.Local`.

| Use | Template |
|---|---|
| Action label | `Agent update <from> → <to>` |
| Actor phrase | `by agent auto-update` |
| Suppressed offline (FR-016; the existing `OfflineCoverage.Message`) | `Device <host> went offline during Agent update <from> → <to> by agent auto-update (expected, back by <HH:MM of hard>)` |
| Online after (FR-017; the existing `OnlineNote.Message`) | `Device <host> came online after Agent update <from> → <to> by agent auto-update (offline <dur>)` |
| Failed (FR-018) | `Agent update <from> → <to> by agent auto-update failed on <host>: <SafeText(reason,200)>` |
| Wrong version (FR-019) | `<host> came back on agent <reported> after Agent update <from> → <to> by agent auto-update (expected <to>)` |
| Not back (FR-020) | `<host> still offline 10m after Agent update <from> → <to> by agent auto-update (record <id8>)` |
| Offline after failure (X-7) | `<host> is offline: Agent update <from> → <to> by agent auto-update failed (record <id8>)` |
| Not confirmed (FR-021) | `Agent update <from> → <to> by agent auto-update on <host> was not confirmed within 10m (agent reports <stored version or "an unrecognised version">)` |
| Rate cap (FR-007) | `Agent self-update tracking paused on <host>: more than 6 agent updates started in 24h; offline alerts during agent updates are not suppressed` |

`CoverOffline`, `NoteOnline` and the escalation request use a helper, `windowActionLabel(w)`. It returns the versioned label for a self-update window and `ExpectActionLabel(action)` for every other window. Every message is ≤ 512 bytes by construction.

### AD-013: Bounds (Ryan's standing rule)

| Thing | Bound | Cleanup |
|---|---|---|
| Self-update rows | ≤ 1 non-terminal per device (unique index); ≤ 6 created per device per rolling 24 h (tx); ≤ 540 per device over 90 d | 90 d retention, budget purge |
| Pending (deletion-exempt) self-update rows | The open row, plus a terminal row for ≤ 1 tick before `finishSelfUpdate` closes it; past bound only while the server is down | `finishSelfUpdate` |
| `command_escalations` | ≤ 2 rows per record (`device`, `outcome`) | Cascade |
| Agent `agent_update_*` events | Stored 1:1 as today (link only, no extra rows) | 90 d |
| Server events | Suppressed offline replaces today's warning 1:1. The online event is the existing one, re-worded. ≤ 1 outcome event and ≤ 1 escalation per record. ≤ 1 rate-cap event per device per 24 h | 90 d |
| Notifications | ≤ 1 push per record (the escalation), under the EXPECT-001 ceiling (5 per device per hour, plus 1 summary) | n/a |
| Tracker memory | ≤ 1 self-update window per device (the same entry through `running` and `verifying`), the trimmed row plus two ≤ 32-byte strings; `closedRecent` ID for 5 min (NFR-004). No rate-cap state in memory. **No per-event state** | Dropped on close; ID after 5 min |
| Request work | Per `agent_update_*` event: 1 device read plus 1 transaction (≤ 4 statements) or 1 to 2 statements. Per registration: 1 select plus ≤ 1 update | n/a |
| Logs | Recognition, refusal, link, suppression: `Debug`. Escalation: 1 `Info` (existing). Errors: the Service `logThrottle` (per device) or the Tracker `warnThrottled` (per kind per minute), plus the global log budget | n/a |
| WebSocket | `command_update` ≤ 3 per record (create, verifying, terminal); `activity_update` only on change | n/a |
| Parse work | ≤ 4096 bytes examined per message | n/a |

### AD-014: Clock injection and test safety

- The handler uses `h.clock()` for the self-update calls, for `ReceiveAgentEvent`'s `CreatedAt` (replacing `time.Now()`), and for the registration hook.
- The Service methods take `at` from the caller. The Tracker uses `Options.Now`.
- No test sleeps.
- No test, helper or fixture of this story runs a power action, a package manager or the agent updater, imports `internal/agent`, or executes an installed `riot-agent`. Agents are simulated by direct calls with fake clocks.
- The static scan `TestEX2_AC029_*` enforces this over a computed file set (Section 12, note 7).

---

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `cmd/riot-server/migrations/000034_agent_self_update.up.sql` / `.down.sql` | Section 5 |
| CREATE | `internal/models/self_update.go` | `ParseSelfUpdateStart`, `ValidAgentVersion`, `AgentVersionsMatch`, `SafeText`, constants (AD-010), `ParamFromVersion`/`ParamToVersion`, step and failure-step tokens, `SelfUpdateVersions(c) (from, to, ok)`, `SelfUpdateActionLabel`, message renderers (AD-012), `SelfUpdateStart`, `SelfUpdateDisposition`, `SelfUpdateCreateResult`, `SelfUpdateOutcome` (confirmed, confirmed_by_version, unconfirmed_online, unconfirmed_offline) |
| CREATE | `internal/models/self_update_test.go` | AC-016, AC-019, AC-024 (actor edges), templates |
| MODIFY | `internal/models/command_policy.go` | `OriginAgentSelfUpdate`; comment that `IsValidOrigin` deliberately excludes it |
| MODIFY | `internal/models/command_state.go` | `ActorSelfUpdate` (`bitSelfUpdate`), its five edges, `String()` |
| MODIFY | `internal/models/expect.go` | `WindowPolicy.SelfUpdate`, `ExpectSelfUpdatePolicy`, `ExpectPolicyFor`, `ExpectSelfUpdateBound`, `ActorPhrase` case, `DeviceActivity.AgentToVersion` (`json:"agent_to_version"`), `EscalationTargetOutcome = "outcome"`, `SafeName` delegating to `SafeText` |
| MODIFY | `internal/models/events.go` | Three event types; `CommandAttrInferred`, `CommandAttrOutcome` |
| CREATE | `internal/server/db/self_update_repo.go` | `CreateSelfUpdate` (AD-002 tx), `OpenSelfUpdate`, `LatestSelfUpdateSince` on `*CommandRepo` |
| MODIFY | `internal/server/db/command_repo.go` | Origin guards in `buildTransitionSQL` (AD-003); `ListOverdue` excludes self-update rows (AD-007, path 7) |
| MODIFY | `internal/server/db/event_repo.go` | `ListByCommand(ctx, deviceID, commandID, limit)` (`idx_events_command`) |
| MODIFY | `internal/server/db/interfaces.go` | `SelfUpdateRepository`; `EventRepository.ListByCommand`; compile-time assertions |
| CREATE | `internal/server/db/self_update_repo_pg_test.go` | PG tests (Section 8) |
| MODIFY | `internal/testutil/mocks.go` | `MockCommandRepo`: origin guards, the CHECK and unique-index emulation, `SelfUpdateRepository` methods (cap event appended to the linked `MockEventRepo`), `FailOn`. `MockEventRepo.ListByCommand` |
| CREATE | `internal/server/commands/self_update.go` | `NoteAgentUpdateEvent`, `NoteSelfUpdateRegistration`, `ResolveSelfUpdate` (AD-002, AD-004, AD-006, AD-008); transitions plus broadcast only |
| MODIFY | `internal/server/commands/service.go` | `Options.SelfUpdates db.SelfUpdateRepository` (optional; nil = feature off = today's behaviour); `classifyRejected` returns `not_found` for a self-update row |
| MODIFY | `internal/server/commands/sweeper.go` | Defensive skip of self-update rows in `timeOutOverdue` |
| CREATE | `internal/server/commands/self_update_test.go` | Section 8 |
| CREATE | `internal/server/expect/selfupdate.go` | `evaluateSelf`, `finishSelfUpdate`, `windowActionLabel`, self-update escalation helpers, the `SelfUpdateResolver` interface |
| MODIFY | `internal/server/expect/window.go` | `window.selfFrom`, `window.selfTo`; `evaluate` delegates when `policy.SelfUpdate` |
| MODIFY | `internal/server/expect/tracker.go` | `ExpectPolicyFor` at every policy site; `trimCommand` copies `VerifyingAt`; self versions on admit and merge; `CoverOffline` and `NoteOnline` use `windowActionLabel`; the `NoteOnline` self-update rule (AD-008); `openEscalation` versions; reconcile rebuild for self-update rows; `SetSelfUpdateResolver` |
| MODIFY | `internal/server/expect/finish.go` | Delegate to `finishSelfUpdate` |
| MODIFY | `internal/server/expect/tick.go` | Eager FR-020 skips `SelfUpdate` windows |
| MODIFY | `internal/server/expect/escalate.go` | `markDeviceEscalated` takes the bound and versions |
| MODIFY | `internal/server/expect/activity.go` | `agent_from_version` and `agent_to_version` for self-update windows and escalations |
| MODIFY | `internal/server/expect/harness_test.go` | Additive: wire a real `commands.Service` (Observer = Tracker, `SelfUpdates` = mock) and helpers `selfStart`, `agentCompleted`, `agentFailed`, `register(version)` that call the same Service methods as the handlers, in the same order |
| CREATE | `internal/server/expect/selfupdate_test.go` | H2 tests (Section 8) |
| CREATE | `internal/server/expect/safety_ex2_test.go` | AC-029 scan |
| MODIFY | `internal/server/handlers/handlers.go` | `ReceiveAgentEvent` (AD-002, AD-004; `h.clock()`); `RegisterDevice` hook (AD-008) |
| CREATE | `internal/server/handlers/agent_event_self_update.go` | Thin helper: hostname read, authenticated-ID check, call the Service, apply the link, broadcast the cap event |
| CREATE | `internal/server/handlers/command_events.go` | `GET /api/v1/devices/{id}/commands/{commandId}/events` |
| CREATE | `internal/server/handlers/agent_event_self_update_test.go`, `command_events_test.go` | Section 8 |
| MODIFY | `internal/server/router.go` | The new route (admin group, beside `/commands/{commandId}/output`) |
| MODIFY | `internal/server/server.go` | `commands.Options.SelfUpdates = CommandRepo`; `Tracker.SetSelfUpdateResolver(svc)` before `Load` (Section 7.7) |
| MODIFY | `web/src/types/models.ts` | `CommandOrigin` adds `'agent_self_update'`; `DeviceActivity.agent_to_version: string` |
| MODIFY | `web/src/api/client.ts`, `web/src/api/demo-client.ts`, `web/src/api/demo-data.ts` | `getCommandEvents`; demo `agent_to_version: ''` |
| CREATE | `web/src/utils/selfUpdate.ts` | `isSelfUpdate`, `selfUpdateVersions` (re-validates the version shape), `selfUpdateOutcomeLabel` |
| MODIFY | `web/src/utils/activity.ts` | AD-009 labels |
| MODIFY | `web/src/utils/activity.test.ts` | Additive `EXPECT-002 [AC-0xx]` cases |
| CREATE | `web/src/components/SelfUpdateDetails.tsx` / `.test.tsx` | FR-027 read-only details (`scrollbar-thin` on the events list) |
| MODIFY | `web/src/components/ActivityLog.tsx` | Row label `Agent update <from> → <to>` plus `by agent auto-update`; outcome label; modal renders `SelfUpdateDetails` |
| MODIFY | `web/src/components/docker/CommandProgress.tsx` | Renders `SelfUpdateDetails` for self-update rows |
| MODIFY | `web/src/components/ui/labels.ts` | `EVENT_TYPES` and labels: `agent_self_update_failed` "Agent self-update failed", `agent_self_update_unconfirmed` "Agent self-update not confirmed", `agent_self_update_paused` "Agent self-update tracking paused" |
| CREATE | `web/src/pages/FleetOverview.selfupdate.test.tsx`, `web/src/pages/DeviceDetail.selfupdate.test.tsx` | AC-023 (the EXPECT-001 web tests stay unmodified) |
| MODIFY | `README.md`, `CHANGELOG.md` | FR-032, FR-033 (technical writer) |
| CREATE | `internal/server/expect2_docs_test.go` | AC-030 (technical writer) |

No agent file, `install.sh`, sudoers or dependency change. `internal/server/events/generator.go` needs **no** change: it already takes its messages and links from the Tracker's coverage and note values.

---

## 5. Data Model Changes

### 5.1 `000034_agent_self_update.up.sql`

```sql
ALTER TABLE commands DROP CONSTRAINT commands_origin_check;
ALTER TABLE commands ADD CONSTRAINT commands_origin_check
  CHECK (origin IN ('user','auto_update','auto_patch','agent_self_update'));

ALTER TABLE commands ADD CONSTRAINT commands_self_update_shape CHECK (
  origin <> 'agent_self_update' OR (
    action = 'agent_update'
    AND status IN ('running','verifying','succeeded','failed','timed_out')
    AND dispatched_at IS NOT NULL
    AND deadline_at IS NOT NULL));

CREATE UNIQUE INDEX idx_commands_self_update_open ON commands (device_id)
  WHERE origin = 'agent_self_update' AND status IN ('running','verifying');

CREATE INDEX idx_commands_self_update_recent ON commands (device_id, created_at)
  WHERE origin = 'agent_self_update';
```

There are no existing self-update rows, so the new CHECK validates instantly. The table scan for `ADD CONSTRAINT` is the same size class as 000033 at hawaii's scale.

### 5.2 `000034_agent_self_update.down.sql`

```sql
DELETE FROM commands WHERE origin = 'agent_self_update';   -- markers cascade
DROP INDEX IF EXISTS idx_commands_self_update_recent;
DROP INDEX IF EXISTS idx_commands_self_update_open;
ALTER TABLE commands DROP CONSTRAINT IF EXISTS commands_self_update_shape;
ALTER TABLE commands DROP CONSTRAINT commands_origin_check;
ALTER TABLE commands ADD CONSTRAINT commands_origin_check
  CHECK (origin IN ('user','auto_update','auto_patch'));
```

**Rollback note.** Down deletes the self-update history. Events keep their `command_id` text (no FK), and the links simply resolve to nothing. The `agent_self_update_*` events remain as plain warning events.

### 5.3 Before and after

- `commands`: no column change. One more origin value, one CHECK, two partial indexes.
- New row shape: AD-001. `params` gains the server-owned keys `from_version` and `to_version` (validated, ≤ 32 bytes each).
- `events`: no column change. Three new types (AD-012). New `command_attr` value `outcome`. The existing `inferred` value is now also used on `agent_update_*` events.
- `command_escalations`: new target value `outcome`.

---

## 6. API / Interface Contract

### 6.1 `GET /api/v1/activity` (extended)

`DeviceActivity` gains `agent_to_version: string`. It is non-empty only for self-update windows and their escalations. For a self-update window:
- `kind: "agent"`, `phase: "in_progress"`, `action: "agent_update"`;
- `actor_phrase: "by agent auto-update"`;
- `agent_from_version`, `agent_to_version`;
- `covers_offline: true`, `back_by: null`.

For its escalation:
- `kind: "escalated"`, `phase: "escalated"`;
- `escalation: {"reason": "not_back" | "failed", "bound": "10m", "at": ...}`;
- `agent_from_version`, `agent_to_version`.

Every other field and status code is unchanged.

### 6.2 Commands API (behaviour for self-update rows)

| Endpoint | Self-update row |
|---|---|
| `GET /api/v1/devices/{id}/commands[?…]`, `GET …/commands/{commandId}` | 200. The row as in AD-001 and AD-003. `origin: "agent_self_update"`, `params: {"from_version","to_version"}` |
| `POST …/commands/{commandId}/cancel` | 409 `{"error":"command cannot be cancelled after it has been dispatched","code":"not_cancellable","status":<status>}` |
| `POST …/commands/{commandId}/report` (device key) | 404 `{"error":"command not found","code":"command_not_found"}`. WebSocket: receipt `{"command_id":…,"result":"not_found","status":""}` |
| `GET …/commands/{commandId}/output` | 200 `[]` |

### 6.3 `GET /api/v1/devices/{id}/commands/{commandId}/events` (new)

- **Auth:** the admin session group (same as `/commands/{commandId}/output`).
- **Request:** path parameters only. `commandId` must be a canonical UUID.
- **200:** `[Event]`, the device's events with `command_id = commandId`, oldest first, at most **50**. Fields as `GET /api/v1/events`, including `command_id` and `command_attr`.
- **404** `{"error":"command not found","code":"command_not_found"}`: malformed ID, missing, or another device's command. One body for all three.
- **500** `{"error":"failed to list events"}`.

It works for any command, not only self-update rows.

### 6.4 Agent endpoints (unchanged contracts)

- **`POST /api/v1/devices/{id}/events`:** same request and same responses (200 `{"status":"ok"}`, 400 for an invalid body or type, 500 when the event cannot be stored). A self-update storage failure never changes the response.
- **`POST /api/v1/devices/register`:** same request and responses.

### 6.5 Go interfaces

```go
// db
type SelfUpdateRepository interface {
    CreateSelfUpdate(ctx context.Context, s models.SelfUpdateStart) (models.SelfUpdateCreateResult, error)
    OpenSelfUpdate(ctx context.Context, deviceID string) (*models.Command, error)               // running|verifying; nil when none
    LatestSelfUpdateSince(ctx context.Context, deviceID string, since time.Time) (*models.Command, error) // newest created_at >= since; nil when none
}
// models
type SelfUpdateStart struct { DeviceID, Hostname, From, To string; At time.Time }
type SelfUpdateCreateResult struct {
    Disposition SelfUpdateDisposition // created | attributed_command | open_record | capped
    Command     *Command              // created row (created only)
    LinkID      string                // created, attributed_command, open_record
    CapEvent    *Event                // capped and this call wrote the 24 h event
}
// commands.Service
type AgentUpdateEventInput struct { DeviceID, Hostname string; Type models.EventType; Message string; At time.Time }
type AgentUpdateEventLink  struct { CommandID, CommandAttr string; CapEvent *models.Event }
func (s *Service) NoteAgentUpdateEvent(ctx context.Context, in AgentUpdateEventInput) AgentUpdateEventLink
func (s *Service) NoteSelfUpdateRegistration(ctx context.Context, deviceID, hostname, reportedVersion string, at time.Time)
func (s *Service) ResolveSelfUpdate(ctx context.Context, commandID, deviceID string, outcome models.SelfUpdateOutcome, resultMsg string, at time.Time) (*models.Command, bool, error)
// expect
type SelfUpdateResolver interface { ResolveSelfUpdate(...same signature...) (*models.Command, bool, error) } // *commands.Service
func (t *Tracker) SetSelfUpdateResolver(r SelfUpdateResolver) // before Load; never pass a typed nil
// db.EventRepository
ListByCommand(ctx context.Context, deviceID, commandID string, limit int) ([]models.Event, error)
```

`expect` does not import `commands`, so there is no import cycle.

### 6.6 Labels (web, additions to EXPECT-001 Section 6.2)

| kind / phase / condition | Label |
|---|---|
| agent, in_progress, `agent_to_version` set | `Updating agent <agent_from_version> → <agent_to_version>` |
| escalated, not_back, versions set | `Not back <bound> after Agent update <from> → <to>` |
| escalated, failed, versions set | `Agent update <from> → <to> failed: offline` |

Beside the label: `by agent auto-update · started <RelativeTime>` (the existing actor line).

---

## 7. Sequence / Flow

### 7.1 Start (`agent_update_started`)

1. Device auth → `ReceiveAgentEvent`.
2. The type is allowed. `at = h.clock()`. Authenticated ID == `{id}`.
3. Hostname read (fallback `device`).
4. `NoteAgentUpdateEvent`:
   - parse, invalid → empty link;
   - `CreateSelfUpdate` tx (AD-002) → disposition;
   - `created` → `broadcast(row)` → `Tracker.CommandChanged` (window admitted, wake) and `command_update`.
5. `events.Create(agent event + link)` → `hub.BroadcastEvent`; the cap event, if any → `hub.BroadcastEvent`.
6. 200.
7. Next tick: activity diff → `activity_update` ("Updating agent 2.78.0 → 2.80.0").

### 7.2 Offline inside the window

The EXPECT-001 detector runs → `DeviceOfflineAt` → `CoverOffline(offlineAt)`. The self-update window covers `[S, hard)` → `info` event, linked, with `CommandAttr = expected` and the versioned message. No notification.

### 7.3 Registration (existing device)

1. Proof, `devices.Update`.
2. `NoteRegistration` (sets `returnedAt`).
3. **`NoteSelfUpdateRegistration`**: an open `running` row with `at < hard`:
   - version match → `verifying`;
   - otherwise → `failed` (`wrong_version`).

   Either way: broadcast → `CommandChanged`.
4. Response, device broadcast.
5. If the device was not live: `DeviceOnline` → `NoteOnline` → the versioned online note (AD-008).
6. Next tick:
   - `verifying`: no label;
   - `failed`: `finishSelfUpdate` → FR-019 event → close.

### 7.4 Completed or failed

1. `ReceiveAgentEvent` → `NoteAgentUpdateEvent` → `LatestSelfUpdateSince(at − 10m)`.
2. Outcome open → `succeeded`, or `failed` with the FR-018 `result_msg` → broadcast → `CommandChanged`.
3. Store the agent event with the link.
4. Next tick → `finishSelfUpdate`: backstop if the device is offline with covered offline; outcome event (failed only) → close.

### 7.5 The bound (S + 10m)

1. The tick plans the entry as due (`hard_bound`) → `finishSelfUpdate`.
2. Backstop: offline with covered offline → escalation (`device`, rule, ceiling, publisher), label "Not back 10m after Agent update …".
3. Resolve:
   - `verifying` → `succeeded`;
   - online with matching stored version → `succeeded`;
   - online → `timed_out`/`unconfirmed_online`;
   - offline → `timed_out`/`unconfirmed_offline`.
4. Outcome event (`unconfirmed_online` only).
5. Close and drop. `activity_update` null, or the escalated activity.

### 7.6 Legacy or `installed` completion before the restart (X-1)

1. Start → `running`.
2. "Agent updated to 2.80.0" → `succeeded` (V NULL). Coverage continues (phase `in_progress`).
3. Restart → the offline is covered.
4. Registration → `returnedAt` > F → coverage ends at R → due → close.

The registration hook does nothing (the outcome is set). If no return happens by `hard`, the backstop escalates (X-7).

### 7.7 Server start (wiring additions to EXPECT-001 Section 7.6)

1. Migrations (000034); repos; hub; generator.
2. `restartSeriesGrace` → `graceEnd`.
3. `Tracker = expect.New(…)`.
4. `svc = commands.New(…, Observer: Tracker, SelfUpdates: CommandRepo)`.
5. `Tracker.SetSelfUpdateResolver(svc)`.
6. `Tracker.Load`.
7. `go Tracker.Run`.
8. Sweeper: it never touches self-update rows.
9. Offline detector; router.

A self-update row past its bound during downtime is resolved, with its backstop, on the first tick after `graceEnd` (≤ 2 min, NFR-006 (b); X-1 of EXPECT-001).

### 7.8 Fleet update during an open record (FR-028, AC-025)

1. Record R (S = 10:00) is open.
2. Command C is dispatched at 10:00:30 (no conflict: R has no keys).
3. Labels and coverage pick the latest D, which is C.
4. The agent refuses C at the host lock, or no-ops it. Its result ends C.
5. R continues to its own registration or bound.

---

## 8. Acceptance Criteria Mapping

Test files:
- **H2** = `internal/server/expect/selfupdate_test.go` (package `expect_test`). It uses the EXPECT-001 harness: a real `events.Generator`, `Tracker` and `commands.Service`, `testutil` mocks, a fake clock, a recording dispatcher, a fake broadcaster, a captured `slog` handler, and `harness.restart()`.
- **S** = `internal/server/commands/self_update_test.go`.
- **M** = `internal/models/self_update_test.go`.
- **R** = `internal/server/db/self_update_repo_pg_test.go` (PostgreSQL 16, skipped without a test DB, as the existing PG tests).
- **HT** = `internal/server/handlers/agent_event_self_update_test.go`.
- **CE** = `internal/server/handlers/command_events_test.go`.
- **Z** = `internal/server/expect/safety_ex2_test.go`.
- **D** = `internal/server/expect2_docs_test.go`.
- **WA** = `web/src/utils/activity.test.ts`.
- **WS** = `web/src/components/SelfUpdateDetails.test.tsx`.
- **WF** = `web/src/pages/FleetOverview.selfupdate.test.tsx`.
- **WD** = `web/src/pages/DeviceDetail.selfupdate.test.tsx`.

Every Go test is named `TestEX2_<AC or tag>_…`. Every web `describe` starts with `EXPECT-002 [AC-0xx]`.

| AC | Fulfilled by | Test |
|---|---|---|
| AC-001 | AD-002, AD-005, AD-008, AD-012, AD-009 | H2: `TestEX2_AC001_ReleaseTimelineNoNotification` (record, info offline text, online note, links, zero dispatches); `TestEX2_AC001_LabelFromStartUntilRegistration` (activity broadcasts) |
| AC-002 | Per-device records | H2: `TestEX2_AC002_FleetReleaseFourRecordsZeroPushes` |
| AC-003 | `Containers: none` | H2: `TestEX2_AC003_ContainerDeathDuringSelfUpdateAlerts` |
| AC-004 | Coverage ends at V/F | H2: `TestEX2_AC004_OfflineAfterCompletionAlerts` |
| AC-005 | Registration → `verifying` | H2: `TestEX2_AC005_FastRestartClosesOnRegistration` |
| AC-006 | AD-002 step 2 | S: `TestEX2_AC006_StartDuringDispatchedCommandAttributed`, `TestEX2_AC006_StartWithinTenMinutesAfterCommandAttributed`; H2: `TestEX2_AC006_FleetUpdateShowsCommandLabel` |
| AC-007 | `dispatched_at IS NOT NULL`, 10 min | S: `TestEX2_AC007_OldCommandDoesNotCapture`, `TestEX2_AC007_QueuedCommandDoesNotCapture`; R: `TestEX2_AC007_PG_AttributionPredicate` |
| AC-008 | AD-006 steps 1 and 2; AD-009 | H2: `TestEX2_AC008_NotBackEscalatesOnceNamingVersions`, `TestEX2_AC008_EscalationIgnoresUnrelatedCooldown`, `TestEX2_AC008_NoSecondEscalationAfterRestart`, `TestEX2_AC008_EscalatedLabelAndOfflineCount`; WA: `EXPECT-002 [AC-008] not-back label names versions` |
| AC-009 | AD-004, AD-006 step 3 | H2: `TestEX2_AC009_FailedWhileOnlineWarningNoPush` |
| AC-010 | `verifying → failed` | H2: `TestEX2_AC010_FailureAfterReturnSetsFailed` |
| AC-011 | `verifying → succeeded` at the bound | H2: `TestEX2_AC011_ReturnOnNewVersionWithoutCompletionSucceedsAtBound` |
| AC-012 | AD-008 `wrong_version` | H2: `TestEX2_AC012_ReturnOnOldVersionFails` (includes a later completion linked without change, and a later offline alerting) |
| AC-013 | `unconfirmed_online` | H2: `TestEX2_AC013_NotConfirmedWhileOnline` |
| AC-014 | Durable row, Load | H2: `TestEX2_AC014_RestartMidWindow` |
| AC-015 | Markers, grace | H2: `TestEX2_AC015_BoundDuringDowntimeEscalatesOnce` |
| AC-016 | AD-011 | M: `TestEX2_AC016_ParseRejectsMalformed` (the seven FRD cases plus a Cf rune); H2: `TestEX2_AC016_MalformedStartAlertsAsToday`; HT: `TestEX2_AC016_MalformedStartStoredAsToday` |
| AC-017 | AD-002 tx, unique index, cap | S: `TestEX2_AC017_RepeatedStartsOneRecord`, `TestEX2_AC017_RateCapSixPerDayOneWarning`; R: `TestEX2_AC017_PG_ConcurrentStartsOneRecordOneCapEvent`; H2: `TestEX2_AC017_CappedStartOfflineAlertsNoDebugAbove` |
| AC-018 | Device-scoped | H2: `TestEX2_AC018_OtherDeviceUnaffected`; HT: `TestEX2_AC018_MismatchedAuthenticatedDeviceIgnored` |
| AC-019 | `SafeText(…, 200)` | M: `TestEX2_AC019_ReasonSanitizedAndBounded`; H2: `TestEX2_AC019_FailedEventMessageSanitized` |
| AC-020 | Fail open | H2: `TestEX2_AC020_StoreFailureFailsOpen` (`FailOn` `CreateSelfUpdate`); HT: `TestEX2_AC020_EventStoredWhenRecordStoreFails` |
| AC-021 | AD-004 step 2 | S: `TestEX2_AC021_CompletionWithoutRecordUnlinked` |
| AC-022 | AD-013 | H2: `TestEX2_AC022_BoundedEventsLogsMemory` (counts events, markers, log records by level, `Tracker.Stats()` 5 min after each outcome) |
| AC-023 | AD-009, Section 6.3 | H2: `TestEX2_AC023_ActivityCarriesVersionsAndActor`; CE: `TestEX2_AC023_CommandEventsEndpoint`; WF: `EXPECT-002 [AC-023] fleet label, actor, start and expected-offline count`; WD: `EXPECT-002 [AC-023] header link opens the read-only record`; WS: `EXPECT-002 [AC-023] details show versions, outcome and events with no controls`; WA: `EXPECT-002 [AC-023] label clears on activity_update null` |
| AC-024 | AD-003, AD-007 | M: `TestEX2_AC024_ActorSelfUpdateEdges`; S: `TestEX2_AC024_NeverClaimedOrDelivered`, `TestEX2_AC024_AgentReportNotFoundHTTPAndWS`, `TestEX2_AC024_CancelNotCancellable`; R: `TestEX2_AC024_PG_CheckForbidsQueued`, `TestEX2_AC024_PG_AgentAndOperatorGuards` |
| AC-025 | Latest D | H2: `TestEX2_AC025_CommandAndRecordOneLabel` |
| AC-026 | AD-005 (V or R), X-1 | H2: `TestEX2_AC026_OlderAgentNoCompletionSucceeds`, `TestEX2_AC026_CompletionBeforeRestartStillCovered`, `TestEX2_AC026_OtherWordingAsToday` |
| AC-027 | `available` untouched | HT: `TestEX2_AC027_UpdateAvailableUnchanged` |
| AC-028 | No EXPECT-001 change | Every existing `TestEX_*` test passes unmodified; H2: `TestEX2_AC028_CommandAgentUpdateUnchanged` |
| AC-029 | AD-014 | Z: `TestEX2_AC029_NoPowerUpdateOrAgentExecution`; the existing web `activity.safety.test.ts` scans the new web tests (they contain `EXPECT`) |
| AC-030 | Docs | D: `TestEX2_AC030_ReadmeAndChangelog` |

### 8.1 Supporting tests

| Topic | Test |
|---|---|
| Q-1: registration after the bound is ignored | S: `TestEX2_Q1_RegistrationAfterBoundIgnored` |
| Q-2: the start goes to the command, the completion to the record, never both | S: `TestEX2_Q2_OneLinkPerEvent` |
| X-1: completion before return | H2: `TestEX2_X1_CompletionBeforeReturnCoversRestart`, `TestEX2_X1_NoReturnByBoundEscalates` |
| X-3: heartbeat version at the bound | H2: `TestEX2_X3_HeartbeatVersionConfirmsAtBound` |
| X-7: backstop on failed while offline | H2: `TestEX2_X7_FailedWithCoveredOfflineEscalatesOnce` |
| Online note only for covered offline | H2: `TestEX2_NoteOnlineOnlyForCoveredOffline` |
| Sweeper | S: `TestEX2_SweeperSkipsSelfUpdate`; R: `TestEX2_PG_ListOverdueExcludesSelfUpdate` |
| Transition table for the existing actors unchanged | M: `TestEX2_ExistingActorEdgesUnchanged` |
| Migration | R: `TestEX2_PG_Migration000034UpDownUp`, `TestEX2_PG_UniqueOpenRecord`, `TestEX2_PG_OriginCheck` |
| No `command_completed` event from a self-update transition | S: `TestEX2_NoCompletionEventForSelfUpdate` |
| Typed-nil wiring | `TestEX2_ResolverUnsetFailsSafe` (no resolver: the entry stays, coverage still ends at `hard`, the escalation still fires) |

---

## 9. Error Handling

| Failure | Behaviour | Log |
|---|---|---|
| Start message invalid | No record, event stored as today, alerting as today (FR-003) | Debug |
| Hostname read fails | `device` in messages; processing continues | Debug |
| `CreateSelfUpdate` fails (DB) | Empty link; event stored as today; offline alerts (AC-020) | Warn via the Service `logThrottle` |
| Unique-index violation (a race outside the lock, for example a manual SQL) | Treated as `open_record` after a re-read; else empty link | Debug |
| `LatestSelfUpdateSince` or the outcome transition fails | Event stored without a link; the record ends at the bound (fails toward alerting or an FR-021 warning) | Warn (throttled) |
| Registration hook fails | No change; the window stays to the bound; registration response unaffected | Warn (throttled) |
| Agent event insert fails after a record was created | 500 to the agent (as today); the record stands and ends normally | Error (existing) |
| `finishSelfUpdate` step fails (escalation, resolve, re-read, outcome event, close) | The entry stays; retried next tick; evicted from the cache after 5 min and reloaded at reconcile; coverage still ends at `hard` | Warn (throttled per kind) |
| Resolver not wired | Same as the previous row (`ErrNoResolver`) | Warn (throttled) |
| Tracker not ready | `CoverOffline` fails open (EXPECT-001); records are still created and resolved later | Existing |
| Publisher queue full | Outcome event or escalation already stored; the push or broadcast is dropped (EXPECT-001 R-3) | Warn (throttled) |
| `GET …/events` for a foreign or malformed ID | 404 `command_not_found` | none |

---

## 10. Security Considerations

| Vector | Mitigation |
|---|---|
| A device suppresses its own offline alerts with forged starts (NFR-010) | Strict grammar. ≤ 1 open record. 10 min per record. 6 per 24 h → ≤ 60 min of delayed alerting per day. Every covered transition stored as `info`. Escalation at each bound if still offline. One durable `paused` warning when the cap is hit |
| Cross-device effect | Only the authenticated device (`DeviceAuth` binds `{id}`; the handler re-checks `authenticatedDeviceID == {id}`). Every query is scoped by `device_id`. Coverage searches only that device's windows (EXPECT-001 AD-005) |
| Agent writes to a record (report, late result) | `ActorAgent` origin guard in SQL. `classifyRejected` returns `not_found`. The ID is never in any agent payload (rows are never claimed) |
| Operator or API mutation | Cancel guard → 409. `Dispatch` cannot create the origin. No retry endpoint. DB CHECK against `queued` |
| Forged completion or failure | Can only end suppression early or set the label and outcome. It can never extend or create suppression (completed and failed never create records) |
| Untrusted text in events and the UI | Versions pass a closed grammar. Reasons pass `SafeText(…, 200)` (Cc, Cf, Zl, Zp removed, valid UTF-8). Hostnames pass `SafeName`. Reported versions are shown only if valid. Event insert sanitizes and redacts again. React renders text only; no raw HTML |
| Agent-supplied `command_id` on update events | Ignored for attribution (X-2) |
| Parsing cost and DoS | ≤ 4 KB examined; one short transaction under a per-device lock; logs at Debug |
| Clock skew | Only the server receive time is used; agent timestamps are never read |
| SQL injection | Bind parameters; the origin literal and status lists come from typed constants |
| Registration spoofing | The hook runs only after REG-HARDEN-001 proof succeeded |
| Real execution in tests (NFR-001) | `TestEX2_AC029_*` static scan plus the EXPECT-001 scan |

---

## 11. Performance Considerations

| Path | Added cost | Index |
|---|---|---|
| `agent_update_started` | 1 PK read, 1 tx: advisory lock, 3 selects, ≤ 2 inserts | `idx_commands_device`, `idx_commands_self_update_open`, `idx_commands_self_update_recent`, `idx_events_device_time` |
| `agent_update_completed`/`failed` | 1 PK read, 1 select, ≤ 1 update | `idx_commands_self_update_recent` |
| Registration | 1 select, ≤ 1 update | `idx_commands_self_update_open` |
| Tracker | ≤ 1 extra window per device; ≤ 4 DB calls per record end | Existing |
| Sweeper `ListOverdue` | One extra predicate | Existing |
| `GET …/events` | 1 indexed select, ≤ 50 rows | `idx_events_command` |

NFR-007 (≤ 100 ms p95 added) has wide headroom at 4 devices.

---

## 12. Implementation Notes for Engineers

1. **Lock discipline (EXPECT-001 Section 12, note 2).**
   - The Tracker never holds `t.mu` while calling the resolver, a repo or the publisher.
   - `Service.broadcast` calls `CommandChanged` synchronously, and that only takes `t.mu` briefly. There is no deadlock with `tickMu`.
2. **Order in `RegisterDevice`.** `Update` → `NoteRegistration` → `NoteSelfUpdateRegistration` → response and broadcast → `DeviceOnline`. Changing it breaks AC-001's online note.
3. **Typed nils.** Never pass a nil `*commands.Service` to `SetSelfUpdateResolver`, or a nil `*db.CommandRepo` to `Options.SelfUpdates`.
4. **Origin guard everywhere.**
   - The guard lives in `buildTransitionSQL` and in the mock's `Transition`. Keep them in parity, the same way as EXPECT-001's `WindowClosedAt` note.
   - The mock must also emulate the CHECK (reject a self-update row in a forbidden status) and the unique index.
5. **No `completed()` and no policy recorder** for self-update transitions. Do not route them through `afterAgentTransition` or `afterSweep`.
6. **`trimCommand`** must copy `VerifyingAt`. The self-update versions come from `params` **before** trimming (params are dropped).
7. **AC-029 scan (`safety_ex2_test.go`).** It reuses `scanSource`. The file set is computed:
   - every `*_test.go` in the repository containing `TestEX2_`;
   - `internal/models/self_update.go`, `internal/server/commands/self_update.go`, `internal/server/expect/selfupdate.go`, `internal/server/handlers/agent_event_self_update.go`.

   Besides the EXPECT-001 patterns, it fails on:
   - an import of `github.com/DesyncTheThird/rIOt/internal/agent`;
   - a string literal matching `apt-get`, `apt `, `dpkg`, `dietpi-update`, `riot-agent-apply`, `systemd-run`.

   It prints the file list. The EXPECT-001 marker (`TestEX_`) does not match `TestEX2_`, so both scans run.
8. **Existing tests stay unmodified:** the EXPECT-001 `TestEX_*` tests, the CMD-LIFECYCLE transition-table tests and the web EXPECT-001 tests. New cases go into new files, or are additive in `activity.test.ts` and `harness_test.go`.
9. **Version strings.** Never trust `params` blindly in the Tracker or the web. Re-validate them with `ValidAgentVersion` / `selfUpdateVersions`. Invalid → the label without versions.
10. **`ReceiveAgentEvent`** must keep its exact response codes. The self-update work happens before `events.Create`, and its failures never change the response.
11. **Build environment.** `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`. `-race` in Linux CI.
12. **Work packages.**
    - WP-1: models, migration, repo, mocks, PG tests;
    - WP-2: commands Service methods, sweeper, `classifyRejected`;
    - WP-3: expect (policy, `evaluateSelf`, `finishSelfUpdate`, activity, escalation labels) and H2;
    - WP-4: handlers, route, wiring;
    - WP-5: web;
    - WP-6: safety scan;
    - WP-7: docs (technical writer).

    Run `go test ./...` and `cd web && npm run test:run` after each.
13. **Implementation report.** It must state:
    - the 5.2 rollback note;
    - that `ReceiveAgentEvent` now uses the injected clock;
    - that `ListOverdue` excludes self-update rows.

---

## 13. Definition of Done

- [ ] Every Section 4 change is implemented. No agent, `install.sh`, sudoers or dependency change.
- [ ] Every AC in Section 8 and every test in 8.1 exists under its name and passes with an injected clock. No test sleeps.
- [ ] `go test ./...` and `cd web && npm run test:run` are green; `-race` in Linux CI.
- [ ] Every existing `TestEX_*` test and the CMD-LIFECYCLE transition tests pass **unmodified** (AC-028).
- [ ] Migration 000034 runs up, down and up on PostgreSQL 16. The CHECK and the unique index are verified by PG tests.
- [ ] No new lint errors (`go vet`, frontend lint, `tsc`).
- [ ] The AD-013 bounds appear as code comments at each bound.
- [ ] `TestEX2_AC029_NoPowerUpdateOrAgentExecution` passes, and its printed file list includes every new Go test file in the diff (QA confirms).
- [ ] QA gate: AC-016, AC-018, AC-019, AC-020, AC-024 and `TestEX2_Q2_OneLinkPerEvent` pass.
- [ ] No test or helper runs a power action, a package manager or the agent updater, or executes `riot-agent`.

---

## FRD Deviations and Clarifications

| ID | FRD item | Resolution | Rationale |
|---|---|---|---|
| X-1 | FR-011 (a), FR-013 | A completion received **before any return** (row still `running`, no registration yet) sets the outcome `succeeded`. Offline coverage then continues until the first observed return after it, or the bound. When the registration came first (the signed flow, AC-001), completion closes at once, as written | The `installed` fast path (`updater.go:472`) and pre-AGENT-HARDEN agents send "Agent updated to X" **before** restarting. Closing there would let the restart alert, which contradicts NFR-008 and BR-001. Same reasoning as EXPECT-001 AD-003 |
| X-2 | FR-001, FR-014 "without a command ID" | Any `command_id` an agent puts on `agent_update_*` is ignored for attribution | No deployed agent sends one. Agent-supplied IDs are never trusted (EXPECT-001 AD-005) |
| X-3 | FR-015 bound rule | Also `succeeded` when the device is online at the bound and its stored agent version (heartbeat) matches <to> | A lost startup registration is retried only after 24 h. The heartbeat still carries the new version. Without this, FR-021 would warn falsely. It does not affect suppression |
| X-4 | FR-019 message | The reported version is shown only if it passes Section 7.2; otherwise `an unrecognised version` | Registration text is untrusted (NFR-010) |
| X-5 | FR-006 "open record" | Open = outcome not set (`running` or `verifying`), enforced by a unique index | One durable invariant; a start during the confirmation phase links to the record |
| X-6 | FR-015 outcome storage | `succeeded`, `failed`, `timed_out` (shown as "Not confirmed" for self-update rows); `verifying` = window closed, outcome pending | Reuses the state machine (AD-003) |
| X-7 | FR-020 scope | A self-update window that ends **by any reason** while its suppressed offline is still ongoing escalates once (`device` marker). This includes the failed-while-offline and completed-without-return cases, with the failed-variant text | EXPECT-001 C-2: no window ends without its backstop. Fails toward alerting. FR-022's "at most one escalation" holds |
| X-8 | FR-018, FR-019, FR-021, FR-007 event identity | New types `agent_self_update_failed`, `agent_self_update_unconfirmed`, `agent_self_update_paused`; outcome events are exactly-once through the marker `outcome` | Distinct and filterable; exactly once across restarts (NFR-006 (c)) |
| X-9 | FR-027 "linked agent events" | Served by the new `GET …/commands/{commandId}/events` | No existing API lists events by command |
| X-10 | AC-016 10 KB message | Rejected by a 4 KB pre-check before trimming | Bounded work |
| X-11 | FR-002 "no control characters" | Also rejects Unicode format characters (Cf: bidi, zero-width) and line or paragraph separators | Stricter only; rejection = today's behaviour |
| X-12 | AC-025 "linked to one of the two" | Linked to the window with the latest dispatch (the command's) | EXPECT-001 FR-007 selection rule |
| X-13 | FR-007 cap | Counted from durable rows. An emergency age-based storage purge of rows under 24 h old would undercount | Restart-safe; the purge is an extreme-pressure path |
| X-14 | NFR-011, Q-1 residual | Verified not to occur in normal operation (Section AD-008) | Code facts |
| X-15 | FR-011 (c)/(d), FR-019 "during an open window" | Counted only on the existing-device re-registration path, with the row `running` and receive time < bound | Precise definition |
| X-16 | FR-014 "otherwise no link" | Kept. Completed and failed events are linked only to records, never to commands. FR-004 links only the start | As written; simplest unambiguous Q-2 answer |
| X-17 | NFR-008 / AC-026 claim about older agents | Agents before AGENT-HARDEN (v2.64.0) send a completion **before** their restart; X-1 is what makes them work. AC-026 is extended with `TestEX2_AC026_CompletionBeforeRestartStillCovered` | The FRD's fake 2.40.0 agent "never sends completed", which is narrower than the real old agent |
| X-18 | FR-016 "back by 18:01" | Server local time `15:04` | EXPECT-001 AD-015 |

## Residuals (in addition to the FRD's NFR-011)

- **R-1.** A record whose bound passes while the server is down stays `running` in the history until the first tick after `graceEnd`.
- **R-2.** If the Tracker cannot write (DB failure), outcome events and escalations are delayed until it can. Coverage still ends at the bound, and offline alerts fail open once readiness lapses (EXPECT-001 AD-002).
- **R-3.** A command-path `agent_update_failed`/`completed` arriving within a record's 10 minutes is attributed to the record (AD-004). This can only close or fail the record early (toward alerting).
- **R-4.** A heartbeat claim of an `agent_update` command does not take the advisory lock. A start racing that claim within milliseconds could create a record and leave the command unattributed. The two windows are then evaluated separately (FR-028).
- **R-5.** Rollback (000034 down) deletes the self-update history.

## Open points for Ryan

- **X-1:** a completion before any return keeps offline coverage until the return or the bound (≤ 10 min). Clod recommends keeping it: without it, the `installed` path and older agents page on every update.
- **X-3:** a heartbeat-reported version confirms success at the bound. Clod recommends keeping it.
- **X-7:** backstop escalation on any self-update window end with an ongoing suppressed offline. Clod recommends keeping it.
- **Rate cap:** 6 per device per 24 h (≤ 60 min of delayed alerting per day). Kept at the FRD value.
- **R-5:** rolling back 000034 deletes the self-update history.
