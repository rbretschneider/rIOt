# Architecture Decision Document

- **Story ID:** EXPECT-002
- **FRD Reference:** `docs/requirements/EXPECT-002-frd.md` (revision 1, commit 4730048; DEC-1 to DEC-5 at the recommended option)
- **Security review:** `docs/security/EXPECT-002-security-review.md` (commit 09df74e, APPROVED WITH CONDITIONS C-1 to C-6). This revision folds them in; see the Security addendum
- **Parent design:** `docs/architecture/EXPECT-001-add.md` (revision 2: AD-001 to AD-016, security conditions C-1 to C-8)
- **Builds on:** CMD-LIFECYCLE-001 (commands record, state machine, sweeper), EXPECT-001 (Tracker, windows, `finishWindow`, escalation markers, publisher and push ceiling, activity API, retention exemption), AGENT-HARDEN-001 (signed self-update path), REG-HARDEN-001 (re-registration proof)
- **Author:** Architect Agent
- **Date:** 2026-10-26
- **Status:** DRAFT, revision 2 (security conditions folded in; for implementation)

## Changelog

### Revision 2: security review conditions C-1 to C-6, INFO-1, INFO-3 to INFO-6

| Item | Source | What changed |
|---|---|---|
| The safety scan covers every Go file of the story by name, every test file in a touched package directory, `os.StartProcess` and `x/sys` imports (with self-tests), plus a new web EXPECT-002 scan. The claim that the EXPECT-001 web scan covers the new tests is removed | C-1 (SEC-001) | AD-014, Section 4, Section 8 AC-029, Section 12 note 7 |
| `ActorSelfUpdate` is the only actor whose UPDATE can match a self-update row. The bound's `running → timed_out` moves to it. Every `ActorServer` edge gets `AND origin <> 'agent_self_update'`. The shape CHECK adds the 10-minute deadline, `exec_deadline_s <= 600` and no target keys | C-2 (SEC-002) | AD-003, AD-006 step 2, AD-007 row 7, Section 5.1, Section 8 AC-024 |
| The linked-events endpoint returns every server-authored linked event separately from the agent-sourced ones, and reports how many agent rows it omitted. AD-013 states the per-record bounds | C-3 (SEC-003) | Section 6.3, AD-013, Section 8 AC-023 |
| A self-update row still unresolved 1 hour after its bound writes one marker-guarded `warning` event (no push) and one throttled Warn. The open-record check retires such a row, so it never blocks new records | C-4 (SEC-004) | AD-002 step 2, AD-006 step 5, Section 9, AD-013, `TestEX2_ResolverUnsetFailsSafe` |
| Abuse budget accepted on Ryan's behalf; the cap stays at 6; the README states the budget | C-5 (SEC-005) | AD-010, Open points |
| QA gate list | C-6 | Section 13 |
| A registration counts as a heartbeat, so a fast return no longer produces a stale-heartbeat "went offline" push | INFO-5 (coordinator: resolve in design) | AD-005, AD-008, X-20, `TestEX2_AC005_StaleHeartbeatAfterFastReturnNoAlert` |
| Bound processing waits for the first offline-detector pass at or after `graceEnd`, so a device that died during downtime is not labelled "not confirmed while online" | INFO-4 | AD-006, `TestEX2_AC015_DeadDuringDowntimeEscalatesNotUnconfirmed` |
| The agent-supplied severity of `agent_update_*` events is clamped to known values | INFO-3 rider (coordinator: fix in this story) | New AD-015, X-19, `TestEX2_X19_AgentUpdateSeverityClamped` |
| AC-018 extended: another device's registration, completion and failure leave hawaii's record unchanged | INFO-1 | Section 8 AC-018 |
| A self-update that takes over a shutdown's offline attribution is accepted as residual R-6, with a follow-on candidate | INFO-6 | Residuals, Follow-on candidates |
| The AD-005 table no longer suggests the activity DTO carries `back_by` | Revision 1 nit | AD-005 |

### Revision 1

The first version.

---

## 1. Summary

Each accepted agent self-update becomes one **`commands` row** with `action = 'agent_update'` and a new origin, **`agent_self_update`**. The server creates the row directly in `running` with `dispatched_at = started_at = receive time` and `deadline_at = start + 10m`.

A database CHECK fixes the row's shape:
- it is never `queued`, so it can never be claimed, sent, expired, reverted or cancelled;
- its bound is at most 10 minutes;
- it holds no target keys.

In SQL, only the new restricted actor `ActorSelfUpdate` can change it. Agent, operator and server edges all carry `origin <> 'agent_self_update'`.

Because the row is an `agent_update` command row, EXPECT-001 picks it up without a parallel system. That covers the Tracker hook, the pending-window loader, the deletion exemption, the escalation markers, the publisher and push ceiling, the activity API and labels, and restart durability.

The Tracker selects a dedicated window policy for the new origin. Its coverage:
- covers `device_offline` only;
- has no settle period and no post-terminal period;
- ends at the earliest of: the registration that reports the new version, an attributed failure, an attributed completion once a return has been seen, or the 10-minute bound.

A registration also counts as a heartbeat, so a fast return no longer leaves a stale heartbeat behind that would alert. The Tracker's single `finishWindow` routine:
- resolves the outcome at the bound;
- runs the backstop: one escalation through the `device_offline` rule when the suppressed offline is still ongoing;
- writes the one outcome event per record, exactly once, through the existing marker table;
- makes a record that cannot be resolved visible after one hour.

Recognition is server-only. The agent-event handler parses the existing `agent_update_started` message with a strict grammar, as untrusted input. One transaction, under the per-device command advisory lock, runs command attribution, the one-open-record rule and the 6-per-24h cap, then inserts the row.

No agent change, no `install.sh` change and no new dependency. Works with deployed agents v2.78.0 to v2.80.0.

## 2. Technical Context

### 2.1 What exists (verified at 4730048, main c55e4bd plus the FRD)

| Area | Where | Relevant facts |
|---|---|---|
| Self-update, old process | `internal/agent/updater.go:124-218` | First check at start + 30 s (`updateStartupDelay`, l.59), then every 6 h (`updateCheckInterval`, l.58). Sends `agent_update_available` (l.168), takes the host lock `automatic-agent-update` (l.187), sends `agent_update_started` "Agent update started: %s → %s" (l.199), then `apply`. Signed path: `applyStaged` returns `errUpdateScheduled` on `accepted` (l.471) and the lock is kept (l.204). A pre-apply error sends `agent_update_failed` "Agent update failed: %v" (l.209). The `installed` fast path (l.472-473) and pre-AGENT-HARDEN agents send `agent_update_completed` "Agent updated to %s" **before** `restart()` (l.214-217). No event carries a command ID (`reportUpdateEvent`, l.598-611) |
| Self-update, new process | `internal/agent/agent.go:249,280,387-397`; `signed_update_handoff.go:104-151` | `register()` runs **before** `recoverAutomaticSignedHandoff()`. The heartbeat loop sends its first heartbeat immediately when it starts. `finishSignedHandoff` with `CommandID == ""` sends `agent_update_completed` "signed installation completion verified against running executable" or `agent_update_failed` (l.127-133). With a command ID it completes the **command** (a command result, l.109-126) and sends **no** agent event |
| Command path | `internal/agent/commands.go:1071-1133` | `handleTriggerUpdate` sends `agent_update_started` (l.1111). It sends `agent_update_failed` only on a pre-apply error (l.1118), and `agent_update_completed` "Agent updated to X" only on the unsigned or `installed` path (l.1123). It returns `pending` for a signed handoff (l.1116). No command ID |
| Agent registration callers | `agent.go:249` (startup), `:438-472` (`reregisterLoop`, every 24 h) | These are the only callers of `register()`, and `register.go:164` is the only `client.Register` call. A dropped WebSocket, a 401 or a 404 never re-registers. A failed startup registration is retried only by the 24 h loop |
| Agent event endpoint | `internal/server/handlers/handlers.go:1165-1201` | Device-key auth (`middleware/auth.go:31-36` rejects a key that does not match `{id}`). Validates the type, then stores the event as received. That includes the agent-supplied **severity, unvalidated**. `EventRepo.Create` sanitizes, cuts at 2048 bytes and redacts. The event is broadcast; there is no rule routing and no notification. `CreatedAt: time.Now()` |
| Registration | `handlers.go:324-416`; `db/device_repo.go:67-77` | Existing-device path: proof, then `devices.Update` (writes `existing.LastHeartbeat` back **unchanged**), then `activity.NoteRegistration` (l.389), the response, the broadcast, and `eventGen.DeviceOnline` only when the device was not live (l.412-414) |
| Heartbeat and detector | `handlers.go:474-539`; `device_repo.go:136-145`; `internal/server/offline.go` | `UpdateHeartbeatTime` stores the heartbeat's agent version and sets `online`. The detector moves a live device offline when `last_heartbeat < now − 45s`, dated `last_heartbeat + 45s`. It runs every 30 s, plus a one-shot at `graceEnd`, and calls `Tracker.ObserveDevices` after every pass |
| Command state machine | `internal/models/command_state.go:111-145` | Server edges: claim, expire, revert, `→ timed_out`. Operator: `queued → cancelled`. Agent: everything else, including `timed_out → succeeded/failed` |
| Transition SQL | `internal/server/db/command_repo.go:367-446` | One guarded UPDATE. An agent edge also fills `dispatched_at` and `deadline_at` (l.428-435). Existing tests assert fragments with `Contains` (`command_repo_test.go:143-181`) |
| Origin CHECK | migration `000023`, l.36 | `commands_origin_check`: `user`, `auto_update`, `auto_patch`. `models.IsValidOrigin` gates `Dispatch` (`service.go:177`) |
| Sweeper | `commands/sweeper.go:89-121` | `ListOverdue` → `timed_out` → `afterSweep`: policy outcome, broadcast, **`CommandCompleted` event** (`generator.go:284-311`), Info log |
| Report rejection | `commands/service.go:742-765` | `classifyRejected` reveals the row's status (409) for a row of the device |
| EXPECT-001 Tracker | `internal/server/expect/*` | `CommandChanged` (`tracker.go:300`) uses `ExpectPolicy(c.Action)`. The agent class needs an observed return after F (`window.go:106`). `finishWindow` (`finish.go:132`) runs the device backstop, the container check and the guarded close. Eager FR-020 runs in `plan` (`tick.go:102`). Labels: `activity.go`; `web/src/utils/activity.ts:68-73` |
| Deletion exemption | `db/expect_repo.go:320`, `docker_retention.go:29`, `storage/command_budget.go:28` | `NOT (window_closed_at IS NULL AND action IN (<ExpectActions>))`, which already includes `agent_update`. The budget purge without a date selects only `result_bytes > 0`. The emergency purge keeps a 7-day floor |
| Marker table | migration 000033 `command_escalations` | PK `(command_id, target)`, target ≤ 64 bytes, `ON DELETE CASCADE` |
| Safety scans | `internal/server/expect/safety_test.go` (marker `TestEX_`); `web/src/utils/activity.safety.test.ts` (path `activity[./]` or text `EXPECT-001\|activity_update\|…`) | Neither matches EXPECT-002 files |
| Command history (web) | `web/src/components/ActivityLog.tsx`, `components/docker/CommandProgress.tsx` | The table shows the action label, status and summary. Cancel appears only for `queued`. There is no retry control and no retry endpoint (`router.go:415-420`) |
| Ingest limit | `internal/server/ingest` | Per device: a burst of 120, then 2 per second |

### 2.2 What is missing

- Self-update recognition.
- A record and a window per self-update.
- Attribution between agent update events and records or commands.
- Outcome events.
- Version-bearing labels.
- A truthful record view.
- Self-heal for a stuck record.

### 2.3 What must change

See Section 4. No agent file, `install.sh`, sudoers or dependency change.

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

Migration 000034:
- widens `commands_origin_check`;
- adds the CHECK `commands_self_update_shape` (Section 5.1);
- adds a unique partial index: ≤ 1 row per device in `running` or `verifying`;
- adds a partial index for the 24 h count.

The shape CHECK makes these properties **database invariants**:
- never `queued`, `dispatched`, `acknowledged`, `cancelled` or `expired`;
- `dispatched_at` and `deadline_at` always present;
- `deadline_at ≤ dispatched_at + 10 minutes`;
- `exec_deadline_s ≤ 600`;
- no target keys.

`models.IsValidOrigin` is **not** widened, so `Service.Dispatch` (every API, fleet, auto-update and auto-patch path) can never create one.

**Rationale.**
- The FRD (7.1) and the brief require reuse of EXPECT-001 without a parallel window system, and EXPECT-001 derives every window from a `commands` row (its AD-001).
- A row with `action = 'agent_update'` is therefore already:
  - covered by `idx_commands_window_pending`;
  - loaded by `ListOpenWindows`;
  - exempt from deletion while pending;
  - a valid FK for `command_escalations` markers;
  - shown in the device command history (FR-027, DEC-5).
- The origin column is the discriminator every path can filter on, in SQL, with a typed constant.

**Alternatives considered.**
- *A separate `self_updates` table with its own window code.* Rejected: that is the parallel system the brief forbids.
- *A new action `agent_self_update`.* Rejected: it changes the EXPECT-001 partial-index predicate (an index rebuild), `ExpectActions()`, the retention list and the PG predicate test, and the web would need a second vocabulary for "Agent update".
- *Insert as `queued` and claim at once.* Rejected: the row would be claimable for a moment.
- *In-memory window only.* Rejected: it fails NFR-006 and FR-027.

**Consequences.**
- AD-007 lists every command path and what it does with these rows (Q-3).
- `target_keys = {}` (enforced) means a self-update row never conflicts with, and never blocks, any command (FR-028).
- The down migration deletes self-update rows (Section 5.2).

### AD-002: Recognition and command attribution in one transaction

**Decision.** `ReceiveAgentEvent` routes the three types `agent_update_started`, `agent_update_completed` and `agent_update_failed` through `commands.Service.NoteAgentUpdateEvent` **before** it stores the event. It does so only when `authenticatedDeviceID(r)` is non-empty and equals `{id}`.
- The service returns a link (`command_id`, `command_attr`) and, at most, one warning event to broadcast.
- It never returns an error. Every failure yields an empty link and one throttled Warn (NFR-009).
- The agent's event is then stored as today (with the AD-015 severity clamp), plus the link, with `CreatedAt = h.clock()`.

**Start (`agent_update_started`):**
1. `models.ParseSelfUpdateStart(raw)` (AD-011). Invalid → no link, no record (FR-003). One `Debug`.
2. `SelfUpdateRepository.CreateSelfUpdate(ctx, start)` runs **one transaction**:
   1. `pg_advisory_xact_lock(hashtextextended('riot.cmd.' || device_id, 0))`. This is the same key as `CreateIfNoConflict`, so it is serialised with every command creation on the device.
   2. **FR-004.** Select the newest `agent_update` row of the device that has:
      - `origin <> 'agent_self_update'`;
      - `dispatched_at IS NOT NULL`;
      - a status in the in-flight list (`dispatched`, `sent`, `acknowledged`, `running`, `verifying`) or `finished_at >= S − 10m`.

      Found → disposition `attributed_command`, link = that command. A queued command (`dispatched_at` NULL, including after a revert) never counts.
   3. **Stale open row (C-4 b).** If the device's open self-update row (`running` or `verifying`) has `deadline_at < S − 1h` (`SelfUpdateStuckAfter`), retire it in the same transaction:
      - `→ timed_out`, `failure_step = unresolved`, `result_msg = not resolved within 1h after the bound`;
      - the guarded UPDATE with `origin = 'agent_self_update'`, through the same SQL builder as `ActorSelfUpdate`;
      - the row is returned as `Retired` for broadcast.
   4. **FR-006.** Select the device's open self-update row. Found → disposition `open_record`, link = that row. The window is neither extended nor reopened.
   5. **FR-007.** Count the device's self-update rows with `created_at > S − 24h`. If the count is ≥ 6 → disposition `capped`, no link. Also, if no `agent_self_update_paused` event exists for the device with `created_at > S − 24h`, insert one (AD-012) and return it for broadcast.
   6. Insert the row (AD-001). Disposition `created`, link = the new row.
3. A `Retired` row, then a `created` row → `Service.broadcast(row)`. That fires the Tracker hook (EXPECT-001 AD-004) and the dashboard `command_update`.
4. Link attribute: `inferred` (`models.CommandAttrInferred`).

**Completed or failed:** see AD-004.

**Rationale.**
- One transaction under the per-device lock makes the checks and the insert atomic: 50 concurrent starts produce one row (AC-017), and the cap event is written once.
- All facts are durable, so they survive restarts (NFR-006 (d)) and cost no memory (NFR-004).
- The FR-004 race with a heartbeat claim is benign (INFO-2). An agent can only send a command-path start after it received the command, which happens after the claim committed.

**Consequences.**
- Each `agent_update_*` event costs one extra PK read (the hostname) and one short transaction. The ingest bucket bounds this to ≤ 2 per second per device.
- An agent-supplied `command_id` on these three types is **ignored** (X-2).

### AD-003: Outcome model on the existing state machine; `ActorSelfUpdate` is the only writer

**Decision.**

| FRD (7.3) | Command state | `step` | `failure_step` |
|---|---|---|---|
| Open, window open | `running` | `waiting_for_restart` | |
| Closed, outcome pending (registration reported <to>) | `verifying` (`verifying_at` = registration time) | `waiting_for_confirmation` | |
| `succeeded` | `succeeded` | `confirmed` | |
| `failed` (agent reported) | `failed` | | `agent_failed` |
| `failed` (came back on another version) | `failed` | | `wrong_version` |
| `not confirmed`, device online at the bound | `timed_out` | | `unconfirmed_online` |
| `not confirmed`, device offline at the bound | `timed_out` | | `unconfirmed_offline` |
| Retired as stale (C-4) | `timed_out` | | `unresolved` |

All tokens match `models.StepPattern`.

- **New actor `models.ActorSelfUpdate`.** Its only edges:
  - `running → verifying`;
  - `running → succeeded`;
  - `running → failed`;
  - `running → timed_out`;
  - `verifying → succeeded`;
  - `verifying → failed`;
  - `verifying → timed_out`.
- **Origin guards** in `buildTransitionSQL`, rendered from the typed constant and appended to the WHERE clause:
  - `ActorAgent`, `ActorOperator` **and `ActorServer`** → `AND origin <> 'agent_self_update'` (C-2 a);
  - `ActorSelfUpdate` → `AND origin = 'agent_self_update'`. `DeviceID` is mandatory for it.

  Together with the CHECK, `ActorSelfUpdate` is the only actor whose UPDATE can match a self-update row. `ClaimQueued` and `ClaimByID` are not `Transition`s, but they match `queued` only, which the CHECK forbids.
- **Side effects of a self-update transition.**
  - A new `Service` method (Section 6.5) runs `Transition` then `broadcast` (observer plus dashboard).
  - It **never** calls `completed()` (no `command_completed` event) or the policy recorder.
  - It **never** touches the progress gate.
  - It **never** sets `result_bytes`, `steps`, `target_results`, `stderr_tail`, `exit_code`, `duration_ms` or command output (C-6).
  - `result_msg` is the server-rendered text of AD-012, ≤ 512 bytes.

**Rationale.**
- The history needs a real status (FR-027), and `verifying` is the natural "returned, confirming" state.
- Making the restricted actor the only writer turns "nothing else can end or move a self-update row" into an SQL property (SEC-002).

**Consequences.**
- The existing transition-table tests for the three existing actors pass unchanged: the table itself is unchanged, only the SQL guard is added.
- The existing `buildTransitionSQL` tests assert with `Contains`, so they stay valid.
- The new actor and the guards are tested on their own.

### AD-004: Attribution of completed and failed events (Q-2)

**Decision.** `NoteAgentUpdateEvent` with `agent_update_completed` or `agent_update_failed`:
1. `LatestSelfUpdateSince(device, at − 10m)` returns the newest self-update row of the device with `created_at ≥ at − 10m`.
2. None → no link, no record, no server event (FR-014, AC-021). These events never create a record, and they are never linked to commands (X-16).
3. Found, outcome open (`running` or `verifying`):
   - completed → `ActorSelfUpdate` to `succeeded`, `result_msg = completed: ` + `SafeText(message, 200)`;
   - failed → `ActorSelfUpdate` to `failed`, `failure_step = agent_failed`, `result_msg` = the FR-018 text.
4. Found, outcome set → link only (FR-022). A transition that does not apply also falls back to link only.
5. Link attribute `inferred`.

**Why the two rules never claim the same event (Q-2).**
- Each event has one `command_id` column. Starts are resolved command-first (AD-002 step 2.2); completions and failures are resolved record-only.
- A record is never created while an attributable command exists. A record and a command only coexist when the command was dispatched **after** the record started (FR-028).
- In that period the agent holds the `automatic-agent-update` host lock (`updater.go:204`, `signed_update_handoff.go:146`). The command therefore fails at the lock or finds the agent up to date, either way with no agent event.
- A signed command update's completion is a command result, not an agent event (`signed_update_handoff.go:109-126`).
- A command-path event inside a record's 10 minutes would be attributed to the record. That fails toward alerting (R-3).

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

`models.ExpectPolicyFor(c Command)` returns this policy for `origin = agent_self_update` (with `action = agent_update`), and `ExpectPolicy(c.Action)` otherwise. Every Tracker call site that picks a policy for a row uses it:
- `CommandChanged`;
- `mergeLocked` (twice);
- the reconcile's escalation-label rebuild.

`ExpectPolicy(action)` and its 12-row table are unchanged.

**Coverage function** (pure; `expect.evaluateSelf`, called from `evaluate` when `policy.SelfUpdate`).

Notation:
- D = `dispatched_at` = S;
- `hard` = `min(deadline_at, D + 10m)`. The CHECK guarantees `deadline_at ≤ D + 10m`; the `min` is defence in depth;
- V = `verifying_at`;
- F = `finished_at`;
- R = the Tracker's `returnedAt`.

| Row state | Phase | Offline coverage ends (exclusive) | `closeAt` (entry processed) | Activity label |
|---|---|---|---|---|
| `running` | `in_progress` | `hard` | `hard` (reason `hard_bound`) | yes. The internal timeline value `backBy = hard` only feeds the FR-016 "back by" text; the DTO has no `back_by` |
| `verifying` | `closed` | `min(V, hard)` | `hard` (outcome resolved at the bound) | no |
| `succeeded`, V set (return before completion: the signed flow) | `closed` | `min(V, hard)` | F | no |
| `succeeded`, V NULL, R > F (return after an early completion) | `closed` | `min(R, hard)` | `min(R, hard)` | no |
| `succeeded`, V NULL, no return after F yet | `in_progress` | `hard` | `hard` | yes |
| `failed`, `timed_out` | `closed` | `min(F, hard)` | F | no |
| any, due and 24 h past `closeAt` | | | reason `lapsed` | |

Coverage is strict at its end. Nothing covers at or after `hard` (FR-012, NFR-002).

**Prompt close without hiding a later outage.**
- The EXPECT-001 agent return phase is **not** used.
- Coverage ends **at the registration** that reports <to> (V, FR-011 c).
- It ends at an attributed failure, or at a registration on another version (F, FR-011 b, d).
- A completion closes it at once when a return was already seen (the normal signed flow, AC-001).
- Only a completion that arrives **before** any return keeps coverage until the first return after it, capped at `hard` (X-1). Those are the `installed` path and pre-AGENT-HARDEN agents, which complete and then restart.
- There is never a settle period. The worst case is the bound: 10 minutes from S.

**Fast return without a stale-heartbeat alert (INFO-5, resolved).**
- A registration now refreshes the device's `last_heartbeat` to the registration time (AD-008). The offline detector therefore sees a fresh heartbeat as soon as the new process registers.
- The new process sends its first heartbeat immediately after it registers (`agent.go:396-397`), well within the 45 s threshold.
- Without this, a restart that registers before `last_heartbeat + 45s` would be marked offline by the next detector pass. That transition is dated after V, so it is not covered, and it would push "went offline" right after the device came back (the AC-005 / BR-001 case).
- This adds no suppression: coverage still ends exactly at V. Alternatives (extending coverage to "first heartbeat after V" or "V + 45 s") were rejected. Both widen the suppressed period and need extra cache lifetime to outlast the detector interval.

**Selection with other windows (FR-028).** EXPECT-001's rules apply unchanged: the latest D wins the label and the link (AC-025, X-12).

### AD-006: Ending a self-update window: `finishSelfUpdate`, always with the backstop

**Decision.** `finishWindow` delegates to `finishSelfUpdate` when `policy.SelfUpdate`. It keeps the EXPECT-001 contract:
- it is the only exit;
- every step is idempotent;
- a failing step leaves the entry for the next tick;
- the entry is evicted after 5 min and reloaded at the next reconcile;
- nothing runs before `graceEnd`.

**Detector gate (INFO-4).** With reason `hard_bound` or `lapsed`, steps 1 and 2 run only after the Tracker has received the first `ObserveDevices` call at or after `graceEnd`. That is the offline detector's one-shot pass at `graceEnd`, so the wait is normally milliseconds. The gate is capped at `graceEnd + 30s`.
- A device that died while the server was down is then known to be offline before the bound is resolved. It escalates (FR-020) instead of being labelled "not confirmed while online".
- Its stale offline transition is still inside `[D, hard)` and is still covered by the cached entry, so it is stored as `info`, not pushed twice.
- NFR-006 (b) holds: the bound is processed within 2 minutes of start.

Steps, outside the lock:
1. **Device backstop (FR-020; X-7).**
   - Condition: grace over, the device is offline now, and its current offline began inside the coverage (`offlineSince ∈ [D, offlineEnd)` from `evaluateSelf`).
   - Then `escalate`: marker `device`, exactly once, through the `device_offline` rule, the ceiling and the publisher (EXPECT-001 AD-010).
   - Row `running`, or `succeeded` without a return → `device_not_back`, FR-020 text. Row `failed` → `device_nonsuccess`, failed-variant text.
   - It records the dashboard escalation (`markDeviceEscalated`, AD-009).
   - It runs **before** resolution, so a resolution failure never delays the page.
2. **Resolve the outcome at the bound.** The row is still `running` or `verifying`; this step is reached only with reason `hard_bound` or `lapsed`. It goes through `SelfUpdateResolver.ResolveSelfUpdate`, **all `ActorSelfUpdate`** (C-2):
   - `verifying` → `succeeded` (`confirmed: the agent registered on <to>`);
   - `running`, device online, stored agent version matches <to> → `succeeded` (`confirmed: the agent reports <to>`; X-3);
   - `running`, device online → `timed_out` / `unconfirmed_online` (FR-021 text);
   - `running`, device offline → `timed_out` / `unconfirmed_offline` (`not confirmed: <host> was offline 10m after the start`).

   A transition that does not apply is followed by a re-read. If the row is still not terminal → `noteFailure`, retry.
3. **Outcome event (FR-018, FR-019, FR-021; FR-022).**
   - Re-read the row (`CommandReader.GetByID`).
   - `failure_step` `agent_failed` or `wrong_version` → `agent_self_update_failed`. `unconfirmed_online` → `agent_self_update_unconfirmed`.
   - Severity `warning`, message = `result_msg`, `command_attr = outcome`.
   - Marker target **`outcome`** through `Repo.RecordEscalation`, exactly once.
   - Queued to the publisher as a **broadcast-only** job (`rule = nil`). It is never dispatched and never consumes the per-device push ceiling (C-6).
4. `CloseWindow(id, now, closed|escalated)` (guarded), then `dropWindow` (the ID is kept in `closedRecent` for 5 min).
5. **Stuck record (C-4 a).** When step 2 or 3 fails and `now ≥ hard + 1h` (`SelfUpdateStuckAfter`), before returning the Tracker writes one `agent_self_update_paused` warning event:
   - marker target **`stuck`**, exactly once per row, no push (broadcast-only job);
   - text per AD-012;
   - plus one throttled Warn (`self_update_stuck`).

   The entry keeps retrying. The next valid start on the device retires the row (AD-002 step 2.3), so a stuck row never blocks new records.

**Eager FR-020** (`plan`) skips `SelfUpdate` windows. Their failed or timed-out rows are due at F.

**Why the Tracker, not the sweeper, resolves the bound.**
- `afterSweep` would write a `command_completed` event and an Info line, against FR-022 and NFR-005.
- It would time out a `verifying` row that must end `succeeded`.
- It would race the Tracker.

The server-edge origin guard (AD-003) now makes this an SQL property, not only a list predicate. An active row is loaded on every reconcile, whatever its age, so a row past its bound is resolved on the first tick after the detector gate.

### AD-007: Every command path honours "never sent to an agent" (Q-3)

| # | Path | What it does with a self-update row |
|---|---|---|
| 1 | `Service.Dispatch` → `CreateIfNoConflict` (API, fleet, auto-update, auto-patch, bulk Docker) | Never creates one (`IsValidOrigin`). No target keys (CHECK), so never a conflict holder |
| 2 | `DispatchBulkDocker` (`ListActive` held keys) | No keys → ignored |
| 3 | `deliver` → `ClaimByID` (WebSocket) | `queued` only. Never `queued` (CHECK) → never claimed or sent |
| 4 | `ClaimForHeartbeat` → `ClaimQueued` | Same: never in `pending_commands` |
| 5 | `expireQueued` / `ListExpiredQueued` | `queued` only → never. The `ActorServer` guard also excludes it |
| 6 | `revertDispatch` (FR-085) | From `dispatched` only → never. `ActorServer` guard; CHECK |
| 7 | Sweeper `timeOutOverdue` / `ListOverdue` | `ListOverdue` adds `AND origin <> 'agent_self_update'`. The `ActorServer` transition guard makes even a mistaken attempt a no-op (C-2). A defensive Go skip stays. The bound is resolved by the Tracker (AD-006) |
| 8 | Agent report: HTTP `POST …/commands/{commandId}/report` and WebSocket ack, progress and result (`terminal.go:353`), via `HandleAgentReport` | `ActorAgent` guard → never applied. `classifyRejected` returns `not_found` for a self-update row: HTTP 404 `command_not_found`, WebSocket receipt `not_found`, the same for ack, progress and result. No `completed()`, no policy, no broadcast, no progress-gate entry. This includes the `timed_out` late-result edge |
| 9 | Operator cancel | `ActorOperator` guard → 409 `not_cancellable` with the status. The web shows no cancel (`queued` only) |
| 10 | Retry | No endpoint or control exists |
| 11 | `ListByDevice`, `ListByDeviceFiltered`, `ListActive`, `GetCommand`, `GetForDevice` | Returned read-only, rendered by `SelfUpdateDetails` |
| 12 | `GetCommandOutput` | `[]` (never written) |
| 13 | `LatestFinishedOSUpdates` | `os_update` only → never |
| 14 | `DockerRepo.ActivityCommands` | `docker_%` only → never |
| 15 | Docker event attribution | Needs a key match → never |
| 16 | Docker inventory refresh `EXISTS` | Never |
| 17 | Retention | Processed rows deleted at 90 d. Pending rows are exempt (existing predicate; bounded, AD-013) |
| 18 | Storage budget | Same exemption. No `result_bytes` (C-6), so only the age-based purge, with its 7-day floor |
| 19 | EXPECT-001 loaders, `CloseWindow` | Included on purpose. Policy from `ExpectPolicyFor` |
| 20 | Escalation markers | Targets `device`, `outcome`, `stuck`. ≤ 3 per record, cascade |
| 21 | Policy recorder, `CommandCompleted` | Never called |
| 22 | `command_update` broadcast | Dashboards only |
| 23 | Device deletion | Cascades |
| 24 | `creationGate` | Not used. FR-007 bounds creation instead |

**Actor phrase:** `models.ActorPhrase` returns `by agent auto-update` for the new origin.

### AD-008: Registration hook (FR-011 c, d; FR-015; FR-019), registration as a heartbeat, and the Q-1 decision

**Decision.** In `RegisterDevice`, existing-device path, after the proof:
1. **Registration counts as a heartbeat (INFO-5).** Set `existing.LastHeartbeat = &at` (`at = h.clock()`) before `devices.Update`. It is written in the same UPDATE, so no new repo method is needed. It applies to every re-registration of every device. A registration requires the same or stronger device proof as a heartbeat, and EXPECT-001 already treats it as evidence of life (its C-1 b).
2. `devices.Update`.
3. `activity.NoteRegistration`.
4. **`Service.NoteSelfUpdateRegistration(ctx, device.ID, SafeName(reg.Hostname), reg.AgentVersion, at)`:**
   - `OpenSelfUpdate(device)`. None, or status ≠ `running`, or `at ≥ hard` → nothing (X-15).
   - `AgentVersionsMatch(reg.AgentVersion, to)` → `running → verifying` (`ActorSelfUpdate`, `step = waiting_for_confirmation`, `verifying_at = at`).
   - Otherwise → `running → failed` (`failure_step = wrong_version`, FR-019 text; the reported version is shown only if valid, X-4).
   - Each write is broadcast. The Tracker sees it synchronously.
5. The response and the device broadcast.
6. `DeviceOnline` → `NoteOnline` (only when the device was not live).

**`NoteOnline` for self-update windows.** A note is produced only when the offline that just ended began inside the coverage: `offlineSince ∈ [D, offlineEnd)`, evaluated at `at`. Because the transition already ran, `offlineEnd` is V or F = `at`.

**Q-1 decision: FR-019 as written.**
- The agent registers only at startup and every 24 h.
- The 24 h re-registration precedes the 6-hourly check that lines up with it by about 30 s, so it falls before the start, not inside the window.
- No reconnect, 401 or 404 path re-registers.
- A guard such as "only after an offline" would keep suppression after a fast rollback. That fails toward silence.
- A periodic re-registration delayed by more than 30 s is an accepted residual that fails toward alerting (security review INFO-9).

### AD-009: Activity, labels and the escalated state

**Decision.**
- `models.DeviceActivity` gains **`agent_to_version`** (`""` for command windows).
- A self-update window in `in_progress` produces this activity:
  - `kind = agent`, `phase = in_progress`, `action = agent_update`;
  - `command_id` = the row;
  - `actor_phrase = "by agent auto-update"`;
  - `started_at = S`;
  - `agent_from_version` and `agent_to_version` from `params`, re-validated. When invalid, both are `""`.
  - `covers_offline = true`, `device_online`;
  - no `back_by`.
- The `window` struct gains `selfFrom`, `selfTo`. `trimCommand` copies `VerifyingAt`.
- `openEscalation` gains `fromVersion` and `toVersion`. For a self-update row:
  - `reason = not_back` or `failed`, and `bound = "10m"` (`ExpectSelfUpdateBound`);
  - the versions are carried both live and through the reconcile rebuild, which reads `params` and maps `timed_out` to `not_back` for these rows.
- The escalated `DeviceActivity` carries both versions.

**Web (`activity.ts`):**
- agent, `in_progress`, `agent_to_version` set → `Updating agent <from> → <to>`. Otherwise the EXPECT-001 rule is unchanged (FR-029).
- escalated with versions → `Not back <bound> after Agent update <from> → <to>`, or `Agent update <from> → <to> failed: offline` (FR-023).

**FR-025:** `isExpectedOffline` is unchanged.

**FR-027:** `activityLink` is unchanged (agent → modal). `CommandProgress` and `CommandDetailModal` render **`SelfUpdateDetails`** for this origin:
- the versioned action label with `by agent auto-update`;
- the start time;
- the outcome label: `running` "In progress", `verifying` "Returned, confirming", `succeeded` "Succeeded", `failed` "Failed", `timed_out` "Not confirmed";
- `result_msg` as text;
- the linked events (Section 6.3): server events always in full; agent events with the omitted count shown as "N more agent events not shown".

It has no action controls.

### AD-010: Bound, rate cap and the abuse budget (FR-007, NFR-010, C-5)

**Constants:**
- `ExpectSelfUpdateBound` = 10m, also a DB CHECK;
- `SelfUpdateAttributionWindow` = 10m;
- `SelfUpdateRateCap` = 6 per `SelfUpdateRateWindow` = 24h;
- `SelfUpdateStuckAfter` = 1h after the bound.

**Accepted budget** (SEC-005, accepted on Ryan's behalf; Ryan reviews it in the PR):
- A device's own agent can make at most about **57 minutes per rolling 24 h** of real offline time unalerted, as at most **6 outages of under about 10 minutes each**.
- Each one is stored as an `info` event naming the update.
- An outage still ongoing at the bound escalates once within 60 s.
- A self-update that comes back on another version leaves one `warning` event and no push.
- Records cannot be chained: X-7 escalates each record at its end.

The cap stays at 6, because the legitimate rate can approach 5 a day. The README residuals (FR-032) state this budget in these terms.

### AD-011: Parsing and validation of untrusted agent text

**`models.ParseSelfUpdateStart(raw string) (from, to string, ok bool)`:**
1. `len(raw) > 4096` → reject.
2. `s = strings.TrimSpace(raw)`. Reject if:
   - `len(s) > 256`;
   - or `!utf8.ValidString(s)`;
   - or `s` contains any rune in Cc, Cf, Zl or Zp (X-11).
3. `s` must start with exactly `Agent update started: `. The remainder must contain exactly one ` → ` (space, U+2192, space).
4. <from> and <to> must each pass `ValidAgentVersion`: 1 to 32 bytes, matching `^v?[0-9]{1,5}\.[0-9]{1,5}\.[0-9]{1,5}([-+][A-Za-z0-9.-]{1,16})?$`.
5. `AgentVersionsMatch(from, to)` must be false. Equal after removing one leading `v` counts as a match.

**Other helpers:**
- `models.SafeText(s, max)` is `SafeName`'s algorithm with a byte limit; `SafeName = SafeText(s, 128)`.
- Reasons go through `SafeText(message, 200)`; hostnames through `SafeName`.
- A version reported at registration is shown only if valid; otherwise it reads `an unrecognised version`.

### AD-012: Events and messages (FR-016 to FR-021, FR-007, C-4)

**New event types** (`models/events.go`). None is routed through rules, none is ever dispatched, and none consumes the push ceiling.

| Type | Severity | Written by | Bound |
|---|---|---|---|
| `agent_self_update_failed` | warning | Tracker step 3, marker `outcome` | ≤ 1 per record |
| `agent_self_update_unconfirmed` | warning | Tracker step 3, marker `outcome` | ≤ 1 per record (exclusive with the row above) |
| `agent_self_update_paused` | warning | the `CreateSelfUpdate` tx (cap), or Tracker step 5 (marker `stuck`) | cap: ≤ 1 per device per 24 h; stuck: ≤ 1 per record |

**New `command_attr` values:** `inferred` (a named constant; the value already exists) and `outcome`.

**Templates.**
- `<host>` = `SafeName(hostname)`, or `device` when there is none.
- `<from>`, `<to>` = validated.
- `<id8>` = `ShortCommandID`.
- Times are `15:04` in `time.Local`.

| Use | Template |
|---|---|
| Action label | `Agent update <from> → <to>` |
| Actor phrase | `by agent auto-update` |
| Suppressed offline (FR-016) | `Device <host> went offline during Agent update <from> → <to> by agent auto-update (expected, back by <HH:MM of hard>)` |
| Online after (FR-017) | `Device <host> came online after Agent update <from> → <to> by agent auto-update (offline <dur>)` |
| Failed (FR-018) | `Agent update <from> → <to> by agent auto-update failed on <host>: <SafeText(reason,200)>` |
| Wrong version (FR-019) | `<host> came back on agent <reported> after Agent update <from> → <to> by agent auto-update (expected <to>)` |
| Not back (FR-020) | `<host> still offline 10m after Agent update <from> → <to> by agent auto-update (record <id8>)` |
| Offline after failure (X-7) | `<host> is offline: Agent update <from> → <to> by agent auto-update failed (record <id8>)` |
| Not confirmed (FR-021) | `Agent update <from> → <to> by agent auto-update on <host> was not confirmed within 10m (agent reports <stored version or "an unrecognised version">)` |
| Rate cap (FR-007) | `Agent self-update tracking paused on <host>: more than 6 agent updates started in 24h; offline alerts during agent updates are not suppressed` |
| Stuck record (C-4) | `Agent self-update tracking paused on <host>: record <id8> (Agent update <from> → <to>) could not be completed 1h after its bound; the next agent update replaces it` |

`windowActionLabel(w)` gives the versioned label for self-update windows and `ExpectActionLabel(action)` otherwise. It is used in `CoverOffline`, `NoteOnline` and escalation requests. Every message is ≤ 512 bytes by construction.

### AD-013: Bounds (Ryan's standing rule)

| Thing | Bound | Cleanup |
|---|---|---|
| Self-update rows | ≤ 1 non-terminal per device (unique index). ≤ 6 created per device per rolling 24 h. ≤ 540 per device over 90 d | 90 d retention, budget purge |
| Pending (deletion-exempt) self-update rows | The open row, plus a terminal row for ≤ 1 tick. Past the bound only while the server is down or resolution fails. A stuck row is retired by the next valid start, or lapses through the Tracker 24 h after `closeAt` | `finishSelfUpdate`, AD-002 step 2.3 |
| Bound of a row | `deadline_at ≤ dispatched_at + 10m`, `exec_deadline_s ≤ 600` (CHECK) | n/a |
| `command_escalations` | ≤ 3 rows per record (`device`, `outcome`, `stuck`) | Cascade |
| Server-authored events linked to one self-update record | ≤ 1 suppressed offline per live→offline transition inside `[S, S+10m)`. Each needs ≥ 45 s without a heartbeat, so ≤ 14. Online notes ≤ 2 (5-min cooldown). ≤ 1 outcome event, ≤ 1 escalation, ≤ 1 stuck event. **Total ≤ 19** | 90 d |
| Agent-sourced events linked to one record | Stored 1:1 as today (FR-005, FR-006, FR-022: link only, no extra rows). Bounded by the ingest bucket over the attribution period: ≤ 120 + 2/s × 600 s ≈ **1,320** | 90 d |
| Other server events | Suppressed offline replaces today's warning 1:1. The online event is the existing one, re-worded. ≤ 1 rate-cap event per device per 24 h | 90 d |
| Notifications | ≤ 1 push per record (escalation), under the EXPECT-001 ceiling (5 per device per hour plus 1 summary). Outcome, cap and stuck events: 0 | n/a |
| Tracker memory | ≤ 1 self-update window per device (one entry through `running` and `verifying`): the trimmed row plus two ≤ 32-byte strings. `closedRecent` ID for 5 min. The detector-gate flag is one bool. **No per-event state**, no rate-cap state | Dropped on close |
| Request work | Per `agent_update_*` event: 1 device read plus 1 transaction (≤ 6 statements) or 1 to 2 statements. Per registration: 1 select plus ≤ 1 update | n/a |
| Events endpoint | ≤ 500 server events plus ≤ 50 agent events per response (Section 6.3) | n/a |
| Logs | Hot path `Debug`. Escalation: 1 `Info` (existing). Errors and stuck rows: the Service `logThrottle` (per device) or the Tracker `warnThrottled` (per kind per minute), plus the global budget | n/a |
| WebSocket | `command_update` ≤ 4 per record (create, verifying, terminal, retire). `activity_update` on change only | n/a |
| Parse work | ≤ 4096 bytes examined per message | n/a |

### AD-014: Clock injection and test safety (C-1)

**Clock.**
- The handler uses `h.clock()` for the self-update calls, for `ReceiveAgentEvent`'s `CreatedAt` and for the registration's `LastHeartbeat` and hook.
- Service methods take `at`. The Tracker uses `Options.Now`.
- No test sleeps.
- No test, helper or fixture runs a power action, a package manager or the agent updater, imports `internal/agent`, starts a process, or executes an installed `riot-agent`.

**Go scan `TestEX2_AC029_NoPowerUpdateOrAgentExecution`** (`internal/server/expect/safety_ex2_test.go`). It reuses `scanSource`.

The file set is the union of three parts:
- **(a) Every Go file in Section 4, asserted by name** (a `must` list like EXPECT-001's):
  - `internal/models/self_update.go`, `command_policy.go`, `command_state.go`, `expect.go`, `events.go`;
  - `internal/server/db/self_update_repo.go`, `command_repo.go`, `event_repo.go`, `interfaces.go`;
  - `internal/testutil/mocks.go`;
  - `internal/server/commands/self_update.go`, `service.go`, `sweeper.go`;
  - `internal/server/expect/selfupdate.go`, `window.go`, `tracker.go`, `finish.go`, `tick.go`, `escalate.go`, `activity.go`;
  - `internal/server/handlers/handlers.go`, `agent_event_self_update.go`, `command_events.go`;
  - `internal/server/router.go`, `internal/server/server.go`;
  - every new `_test.go` of Section 4.
- **(b) Every `_test.go` in the repository whose source contains `TestEX2_`.**
- **(c) Every `_test.go` in a package directory this story adds files to**, non-recursive:
  - `internal/models`;
  - `internal/server/db`;
  - `internal/server/commands`;
  - `internal/server/expect`;
  - `internal/server/handlers`;
  - `internal/server` (for `expect2_docs_test.go`).

**Forbidden.** The EXPECT-001 patterns, plus:
- any import of `os/exec`, `syscall`, `golang.org/x/sys/unix`, `golang.org/x/sys/windows` or `github.com/DesyncTheThird/rIOt/internal/agent`;
- a call to `exec.Command`, `exec.CommandContext` or **`os.StartProcess`**;
- a string literal matching `apt-get`, `apt `, `dpkg`, `dietpi-update`, `riot-agent-apply` or `systemd-run`.

**Pre-existing hits.** Files of part (c) that predate this story may contain a forbidden literal, for example a shutdown-command test asserting `"systemctl poweroff"` as data. Such a hit is listed in a reviewed table `ex2Preexisting` keyed by **file plus pattern plus a one-line reason**. Any hit not in the table fails, and so does a table entry that no longer matches (stale exceptions are not allowed).

**Output.** The test prints the scanned list.

**Self-test.** `TestEX2_AC029_ScannerCatchesEachForbiddenForm` feeds synthetic sources (assembled from fragments, so this file does not trip itself) for every pattern, including `os.StartProcess`, both `x/sys` imports and the `internal/agent` import, and asserts each is caught.

**Web scan** (`web/src/utils/selfUpdate.safety.test.ts`, describe `EXPECT-002 [AC-029] …`). The computed set is:
- every `*.test.ts(x)` under `web/src` whose text contains `EXPECT-002`;
- the sources `utils/selfUpdate.ts`, `components/SelfUpdateDetails.tsx`, `utils/activity.ts`, `components/ActivityLog.tsx` and `components/docker/CommandProgress.tsx`.

It fails on `child_process`, `node:child_process`, `dangerouslySetInnerHTML`, `process.kill`, `spawn(`, `execSync`, and the power and agent literals above.

It asserts that the set contains `SelfUpdateDetails.test.tsx`, `FleetOverview.selfupdate.test.tsx` and `DeviceDetail.selfupdate.test.tsx`, plus `selfUpdate.ts` and `SelfUpdateDetails.tsx`. It prints the list.

The EXPECT-001 web scan does **not** cover EXPECT-002 files, and this ADD no longer claims it does.

### AD-015: Agent-supplied severity of agent update events is clamped (INFO-3 rider)

**Decision.** In `ReceiveAgentEvent`, for the four types `agent_update_available`, `agent_update_started`, `agent_update_completed` and `agent_update_failed`:
- a severity in {`info`, `warning`, `critical`} is stored as sent;
- any other value is replaced by `info` for available, started and completed, and by `warning` for failed.

Real agents send `info` or `warning` (`updater.go:168-214`, `signed_update_handoff.go:129-131`), so FR-005 is unchanged for them. This applies whether or not self-update processing runs.

The extra-updater path and other pre-existing items are out of scope (X-19).

---

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `cmd/riot-server/migrations/000034_agent_self_update.up.sql` / `.down.sql` | Section 5 |
| CREATE | `internal/models/self_update.go` | Parser and validators (AD-011), `SafeText`, constants (AD-010), param keys, step and failure-step tokens, `SelfUpdateVersions`, `SelfUpdateActionLabel`, message renderers (AD-012), `SelfUpdateStart`, `SelfUpdateDisposition`, `SelfUpdateCreateResult`, `SelfUpdateOutcome`, `ClampAgentUpdateSeverity` (AD-015) |
| CREATE | `internal/models/self_update_test.go` | Section 8 |
| MODIFY | `internal/models/command_policy.go` | `OriginAgentSelfUpdate`; comment that `IsValidOrigin` excludes it |
| MODIFY | `internal/models/command_state.go` | `ActorSelfUpdate` (`bitSelfUpdate`), its seven edges, `String()` |
| MODIFY | `internal/models/expect.go` | `WindowPolicy.SelfUpdate`, `ExpectSelfUpdatePolicy`, `ExpectPolicyFor`, `ExpectSelfUpdateBound`, `ActorPhrase` case, `DeviceActivity.AgentToVersion`, `EscalationTargetOutcome`, `EscalationTargetStuck`, `SafeName` → `SafeText` |
| MODIFY | `internal/models/events.go` | Three event types; `CommandAttrInferred`, `CommandAttrOutcome` |
| CREATE | `internal/server/db/self_update_repo.go` | `CreateSelfUpdate` (AD-002 tx incl. stale retire), `OpenSelfUpdate`, `LatestSelfUpdateSince` |
| MODIFY | `internal/server/db/command_repo.go` | Origin guards for all four actors in `buildTransitionSQL`; `ListOverdue` excludes self-update rows |
| MODIFY | `internal/server/db/event_repo.go` | `ListByCommandSplit(ctx, deviceID, commandID, serverLimit, agentLimit)` (Section 6.3) |
| MODIFY | `internal/server/db/interfaces.go` | `SelfUpdateRepository`; `EventRepository.ListByCommandSplit`; assertions |
| CREATE | `internal/server/db/self_update_repo_pg_test.go` | Section 8 |
| MODIFY | `internal/testutil/mocks.go` | `MockCommandRepo`: the four origin guards, the CHECK (status, deadline ≤ D+10m, `exec_deadline_s ≤ 600`, no keys) and unique-index emulation, `SelfUpdateRepository` methods (cap event appended to the linked `MockEventRepo`), `FailOn`. `MockEventRepo.ListByCommandSplit` |
| CREATE | `internal/server/commands/self_update.go` | `NoteAgentUpdateEvent`, `NoteSelfUpdateRegistration`, `ResolveSelfUpdate`; transitions plus broadcast only (AD-003) |
| MODIFY | `internal/server/commands/service.go` | `Options.SelfUpdates` (optional; nil = today's behaviour); `classifyRejected` → `not_found` for self-update rows |
| MODIFY | `internal/server/commands/sweeper.go` | Defensive skip |
| CREATE | `internal/server/commands/self_update_test.go` | Section 8 |
| CREATE | `internal/server/expect/selfupdate.go` | `evaluateSelf`, `finishSelfUpdate` (incl. detector gate, stuck event), `windowActionLabel`, `SelfUpdateResolver` |
| MODIFY | `internal/server/expect/window.go` | `selfFrom`, `selfTo`; delegation |
| MODIFY | `internal/server/expect/tracker.go` | `ExpectPolicyFor`; `trimCommand` copies `VerifyingAt`; versions on admit and merge; `windowActionLabel` in `CoverOffline` and `NoteOnline`; the `NoteOnline` self rule; `openEscalation` versions; rebuild; `SetSelfUpdateResolver`; detector-gate flag set by `ObserveDevices` |
| MODIFY | `internal/server/expect/finish.go` | Delegate |
| MODIFY | `internal/server/expect/tick.go` | Eager FR-020 skips self-update windows |
| MODIFY | `internal/server/expect/escalate.go` | `markDeviceEscalated` takes the bound and versions |
| MODIFY | `internal/server/expect/activity.go` | Versions in self-update and escalated activities |
| MODIFY | `internal/server/expect/harness_test.go` | Additive: a real `commands.Service` and the helpers `selfStart`, `agentCompleted`, `agentFailed`, `register(version)`, `detectorPass()`, which mirror the handlers' order |
| CREATE | `internal/server/expect/selfupdate_test.go` | H2 |
| CREATE | `internal/server/expect/safety_ex2_test.go` | AC-029 Go scan and self-test |
| MODIFY | `internal/server/handlers/handlers.go` | `ReceiveAgentEvent` (AD-002, AD-004, AD-015, `h.clock()`); `RegisterDevice` (`LastHeartbeat`, hook; AD-008) |
| CREATE | `internal/server/handlers/agent_event_self_update.go` | Thin helper: hostname read, authenticated-ID check, call the Service, link, broadcast the returned events |
| CREATE | `internal/server/handlers/command_events.go` | `GET …/commands/{commandId}/events` |
| CREATE | `internal/server/handlers/agent_event_self_update_test.go`, `command_events_test.go` | Section 8 |
| MODIFY | `internal/server/router.go` | The new route |
| MODIFY | `internal/server/server.go` | `Options.SelfUpdates`; `SetSelfUpdateResolver(svc)` before `Load` |
| MODIFY | `web/src/types/models.ts` | `CommandOrigin` `'agent_self_update'`; `DeviceActivity.agent_to_version`; `CommandEventsResponse` |
| MODIFY | `web/src/api/client.ts`, `demo-client.ts`, `demo-data.ts` | `getCommandEvents`; demo `agent_to_version: ''` |
| CREATE | `web/src/utils/selfUpdate.ts` | `isSelfUpdate`, `selfUpdateVersions` (re-validates), `selfUpdateOutcomeLabel` |
| CREATE | `web/src/utils/selfUpdate.safety.test.ts` | AC-029 web scan |
| MODIFY | `web/src/utils/activity.ts` | AD-009 labels |
| MODIFY | `web/src/utils/activity.test.ts` | Additive `EXPECT-002 [AC-0xx]` cases |
| CREATE | `web/src/components/SelfUpdateDetails.tsx` / `.test.tsx` | FR-027 (`scrollbar-thin` on the lists) |
| MODIFY | `web/src/components/ActivityLog.tsx` | Row label, actor, outcome label; modal renders `SelfUpdateDetails` |
| MODIFY | `web/src/components/docker/CommandProgress.tsx` | Renders `SelfUpdateDetails` |
| MODIFY | `web/src/components/ui/labels.ts` | `agent_self_update_failed` "Agent self-update failed", `agent_self_update_unconfirmed` "Agent self-update not confirmed", `agent_self_update_paused` "Agent self-update tracking paused" |
| CREATE | `web/src/pages/FleetOverview.selfupdate.test.tsx`, `web/src/pages/DeviceDetail.selfupdate.test.tsx` | AC-023 |
| MODIFY | `README.md`, `CHANGELOG.md` | FR-032 (incl. the AD-010 budget), FR-033 (technical writer) |
| CREATE | `internal/server/expect2_docs_test.go` | AC-030 |

No agent file, `install.sh`, sudoers or dependency change. `internal/server/events/generator.go` is unchanged.

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
    AND deadline_at IS NOT NULL
    AND deadline_at <= dispatched_at + INTERVAL '10 minutes'
    AND exec_deadline_s <= 600
    AND cardinality(target_keys) = 0));

CREATE UNIQUE INDEX idx_commands_self_update_open ON commands (device_id)
  WHERE origin = 'agent_self_update' AND status IN ('running','verifying');

CREATE INDEX idx_commands_self_update_recent ON commands (device_id, created_at)
  WHERE origin = 'agent_self_update';
```

There are no existing self-update rows, so the new CHECK validates instantly. The migration takes under a second at hawaii's size (INFO-7).

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

**Rollback note.** Down deletes the self-update history. Events keep a dangling `command_id`, and the `agent_self_update_*` events remain as plain warnings.

### 5.3 Before and after

- **`commands`.** No column change. One origin value, one CHECK and two partial indexes are added. `params` gains the server-owned `from_version` and `to_version` keys.
- **`devices`.** No change. `last_heartbeat` is now also written by a re-registration.
- **`events`.** No column change. Three types are added. A new `command_attr` value `outcome` is added. `inferred` is now also used on `agent_update_*` events.
- **`command_escalations`.** New target values `outcome` and `stuck`.

---

## 6. API / Interface Contract

### 6.1 `GET /api/v1/activity` (extended)

`DeviceActivity` gains `agent_to_version: string`. It is non-empty only for self-update windows and their escalations.

For a self-update window:
- `kind: "agent"`, `phase: "in_progress"`, `action: "agent_update"`;
- `actor_phrase: "by agent auto-update"`;
- `agent_from_version`, `agent_to_version`;
- `covers_offline: true`, `back_by: null`.

For its escalation:
- `kind: "escalated"`;
- `escalation: {"reason": "not_back" | "failed", "bound": "10m", "at": …}`;
- both versions.

Everything else is unchanged.

### 6.2 Commands API (behaviour for self-update rows)

| Endpoint | Self-update row |
|---|---|
| `GET /api/v1/devices/{id}/commands[?…]`, `GET …/commands/{commandId}` | 200. `origin: "agent_self_update"`, `params: {"from_version","to_version"}`, status per AD-003 |
| `POST …/commands/{commandId}/cancel` | 409 `{"error":"command cannot be cancelled after it has been dispatched","code":"not_cancellable","status":<status>}` |
| `POST …/commands/{commandId}/report` (device key) | 404 `{"error":"command not found","code":"command_not_found"}` for ack, progress and result. WebSocket: receipt `{"command_id":…,"result":"not_found","status":""}` |
| `GET …/commands/{commandId}/output` | 200 `[]` |

### 6.3 `GET /api/v1/devices/{id}/commands/{commandId}/events` (new; C-3)

- **Auth:** the admin session group.
- **Request:** path parameters only. `commandId` must be a canonical UUID.
- **Classification:**
  - an event is **server-authored** when its `command_attr` is `expected`, `escalation` or `outcome`, or its type is one of the three `agent_self_update_*` types;
  - an event is **agent-sourced** when its `command_attr` is `inferred` or `agent`.
- **200:**

```
{
  "server_events":         [Event],  // every server-authored linked event, oldest first, at most 500
  "server_events_omitted": int,      // 0 for a self-update record (bounded at 19, AD-013)
  "agent_events":          [Event],  // agent-sourced linked events, oldest first, at most 50
  "agent_events_omitted":  int       // agent-sourced linked events not returned
}
```

  `Event` is the existing shape, including `command_id` and `command_attr`.
- **404** `{"error":"command not found","code":"command_not_found"}`: malformed, missing and foreign IDs, with one identical body.
- **500** `{"error":"failed to list events"}`.

**Repository.** Two indexed selects on `idx_events_command`, plus two counts:
- server-authored `LIMIT 500`;
- agent-sourced `LIMIT 50`.

It works for any command.

### 6.4 Agent endpoints

- **`POST /api/v1/devices/{id}/events`.** Same request and responses. Since AD-015, an unknown severity on `agent_update_*` is stored clamped. Self-update failures never change the response.
- **`POST /api/v1/devices/register`.** Same request and responses. A re-registration also refreshes `last_heartbeat`.

### 6.5 Go interfaces

```go
// db
type SelfUpdateRepository interface {
    CreateSelfUpdate(ctx context.Context, s models.SelfUpdateStart) (models.SelfUpdateCreateResult, error)
    OpenSelfUpdate(ctx context.Context, deviceID string) (*models.Command, error)
    LatestSelfUpdateSince(ctx context.Context, deviceID string, since time.Time) (*models.Command, error)
}
// models
type SelfUpdateStart struct { DeviceID, Hostname, From, To string; At time.Time }
type SelfUpdateCreateResult struct {
    Disposition SelfUpdateDisposition // created | attributed_command | open_record | capped
    Command     *Command              // created row
    Retired     *Command              // stale open row retired in the same tx (C-4)
    LinkID      string
    CapEvent    *Event
}
// commands.Service
type AgentUpdateEventInput struct { DeviceID, Hostname string; Type models.EventType; Message string; At time.Time }
type AgentUpdateEventLink  struct { CommandID, CommandAttr string; Events []models.Event } // events to broadcast
func (s *Service) NoteAgentUpdateEvent(ctx context.Context, in AgentUpdateEventInput) AgentUpdateEventLink
func (s *Service) NoteSelfUpdateRegistration(ctx context.Context, deviceID, hostname, reportedVersion string, at time.Time)
func (s *Service) ResolveSelfUpdate(ctx context.Context, commandID, deviceID string, outcome models.SelfUpdateOutcome, resultMsg string, at time.Time) (*models.Command, bool, error)
// expect
type SelfUpdateResolver interface { ResolveSelfUpdate(...same...) (*models.Command, bool, error) }
func (t *Tracker) SetSelfUpdateResolver(r SelfUpdateResolver) // before Load; never a typed nil
// db.EventRepository
ListByCommandSplit(ctx context.Context, deviceID, commandID string, serverLimit, agentLimit int) (models.CommandEvents, error)
```

There is no import cycle.

### 6.6 Labels (web, additions to EXPECT-001 Section 6.2)

| kind / phase / condition | Label |
|---|---|
| agent, in_progress, `agent_to_version` set | `Updating agent <agent_from_version> → <agent_to_version>` |
| escalated, not_back, versions set | `Not back <bound> after Agent update <from> → <to>` |
| escalated, failed, versions set | `Agent update <from> → <to> failed: offline` |

The actor line reads `by agent auto-update · started <RelativeTime>`.

---

## 7. Sequence / Flow

### 7.1 Start

1. Device auth → `ReceiveAgentEvent`.
2. The type is allowed. Severity is clamped (AD-015). `at = h.clock()`. The authenticated ID equals `{id}`.
3. Hostname read.
4. `NoteAgentUpdateEvent`: parse → `CreateSelfUpdate` tx (attribution, stale retire, open check, cap, insert) → `broadcast` of the retired row and the created row.
5. `events.Create` (agent event plus link) → broadcast. Returned warning events → broadcast.
6. 200.
7. Next tick: `activity_update` "Updating agent 2.78.0 → 2.80.0".

### 7.2 Offline inside the window

Detector → `DeviceOfflineAt` → `CoverOffline(offlineAt)`. The window `[S, hard)` covers it → an `info` event, linked, with the versioned text. No notification.

### 7.3 Registration

1. Proof.
2. `LastHeartbeat = at`; `devices.Update`.
3. `NoteRegistration`.
4. `NoteSelfUpdateRegistration`: the row is `running` and `at < hard` → `verifying`, or `failed` (`wrong_version`). Broadcast.
5. Response and device broadcast.
6. If the device was not live: `DeviceOnline` → `NoteOnline` → the versioned note.
7. The next detector pass sees a fresh heartbeat: no stale transition.

### 7.4 Completed or failed

1. `ReceiveAgentEvent` → `LatestSelfUpdateSince(at − 10m)`.
2. If the outcome is open → `succeeded`, or `failed` with the FR-018 `result_msg`. Broadcast.
3. Store the event with its link.
4. Next tick: `finishSelfUpdate` runs the backstop if needed, writes the outcome event (failed only) and closes.

### 7.5 The bound

1. Tick → due (`hard_bound`).
2. Detector gate (a no-op outside the post-restart window).
3. `finishSelfUpdate`:
   1. backstop (FR-020);
   2. resolve (`ActorSelfUpdate`);
   3. outcome event (FR-021);
   4. close and drop;
   5. `activity_update` null or escalated.

### 7.6 Completion before the restart (X-1)

1. Start → `running`.
2. "Agent updated to 2.80.0" → `succeeded` (V NULL). Coverage continues.
3. Restart → the offline is covered.
4. Registration → R > F → coverage ends, the entry closes.
5. No return by `hard` → the backstop escalates (X-7).

### 7.7 Stuck record (C-4)

1. The resolver is unset, or resolution fails.
2. At `hard`, the backstop has already run. The entry retries every tick and is evicted and reloaded.
3. At `hard + 1h`: one `agent_self_update_paused` event (marker `stuck`) and one throttled Warn.
4. The next valid start on the device retires the row as `timed_out` / `unresolved` and creates a new record.

### 7.8 Server start (additions to EXPECT-001 Section 7.6)

1. Migration 000034.
2. `Tracker = expect.New(…)`.
3. `svc = commands.New(…, Observer: Tracker, SelfUpdates: CommandRepo)`.
4. `Tracker.SetSelfUpdateResolver(svc)`.
5. `Load` → `Run`.
6. The offline detector's one-shot at `graceEnd` → `ObserveDevices` opens the detector gate.
7. Self-update rows past their bound are resolved, with the backstop, right after it (≤ 2 min after start).

### 7.9 Fleet update during an open record (FR-028, AC-025)

1. R (S = 10:00) is open.
2. C is dispatched at 10:00:30. There is no conflict, because R has no keys.
3. The latest D (C) takes the label and the coverage.
4. The agent refuses C at the host lock, or no-ops it.
5. R continues to its registration or its bound.

---

## 8. Acceptance Criteria Mapping

**Test files:**
- **H2** = `internal/server/expect/selfupdate_test.go` (package `expect_test`). It uses the EXPECT-001 harness: a real `events.Generator`, `Tracker` and `commands.Service`, `testutil` mocks, a fake clock, a recording dispatcher, a fake broadcaster, a captured `slog` handler, `harness.restart()` and `detectorPass()`.
- **S** = `internal/server/commands/self_update_test.go`.
- **M** = `internal/models/self_update_test.go`.
- **R** = `internal/server/db/self_update_repo_pg_test.go` (PostgreSQL 16).
- **HT** = `internal/server/handlers/agent_event_self_update_test.go`.
- **CE** = `internal/server/handlers/command_events_test.go`.
- **Z** = `internal/server/expect/safety_ex2_test.go`.
- **D** = `internal/server/expect2_docs_test.go`.
- Web:
  - **WA** = `web/src/utils/activity.test.ts`;
  - **WZ** = `web/src/utils/selfUpdate.safety.test.ts`;
  - **WS** = `web/src/components/SelfUpdateDetails.test.tsx`;
  - **WF** = `web/src/pages/FleetOverview.selfupdate.test.tsx`;
  - **WD** = `web/src/pages/DeviceDetail.selfupdate.test.tsx`.

Go tests are named `TestEX2_…`. Web `describe` titles start with `EXPECT-002 [AC-0xx]`.

| AC | Fulfilled by | Test |
|---|---|---|
| AC-001 | AD-002, AD-005, AD-008, AD-012, AD-009 | H2: `TestEX2_AC001_ReleaseTimelineNoNotification`, `TestEX2_AC001_LabelFromStartUntilRegistration` |
| AC-002 | Per-device records | H2: `TestEX2_AC002_FleetReleaseFourRecordsZeroPushes` |
| AC-003 | `Containers: none` | H2: `TestEX2_AC003_ContainerDeathDuringSelfUpdateAlerts` |
| AC-004 | Coverage ends at V or F | H2: `TestEX2_AC004_OfflineAfterCompletionAlerts` |
| AC-005 | Registration → `verifying`; registration as a heartbeat (INFO-5) | H2: `TestEX2_AC005_FastRestartClosesOnRegistration`; HT: `TestEX2_AC005_StaleHeartbeatAfterFastReturnNoAlert` (registration at last heartbeat + 40 s, detector pass at + 46 s, first heartbeat at + 50 s: no offline transition, no event, no push) |
| AC-006 | AD-002 step 2.2 | S: `TestEX2_AC006_StartDuringDispatchedCommandAttributed`, `TestEX2_AC006_StartWithinTenMinutesAfterCommandAttributed`; H2: `TestEX2_AC006_FleetUpdateShowsCommandLabel` |
| AC-007 | `dispatched_at IS NOT NULL`, 10 min | S: `TestEX2_AC007_OldCommandDoesNotCapture`, `TestEX2_AC007_QueuedCommandDoesNotCapture`; R: `TestEX2_AC007_PG_AttributionPredicate` |
| AC-008 | AD-006 steps 1 and 2; AD-009 | H2: `TestEX2_AC008_NotBackEscalatesOnceNamingVersions`, `TestEX2_AC008_EscalationIgnoresUnrelatedCooldown`, `TestEX2_AC008_NoSecondEscalationAfterRestart`, `TestEX2_AC008_EscalatedLabelAndOfflineCount`; WA: `EXPECT-002 [AC-008] not-back label names versions` |
| AC-009 | AD-004, AD-006 step 3 | H2: `TestEX2_AC009_FailedWhileOnlineWarningNoPush` |
| AC-010 | `verifying → failed` | H2: `TestEX2_AC010_FailureAfterReturnSetsFailed` |
| AC-011 | `verifying → succeeded` at the bound | H2: `TestEX2_AC011_ReturnOnNewVersionWithoutCompletionSucceedsAtBound` |
| AC-012 | AD-008 `wrong_version` | H2: `TestEX2_AC012_ReturnOnOldVersionFails` |
| AC-013 | `unconfirmed_online` | H2: `TestEX2_AC013_NotConfirmedWhileOnline` |
| AC-014 | Durable row, Load | H2: `TestEX2_AC014_RestartMidWindow` |
| AC-015 | Markers, grace, detector gate (INFO-4) | H2: `TestEX2_AC015_BoundDuringDowntimeEscalatesOnce`, `TestEX2_AC015_DeadDuringDowntimeEscalatesNotUnconfirmed` (DB status still `online` from before downtime; no FR-021 event, one escalation, one push) |
| AC-016 | AD-011, AD-015 | M: `TestEX2_AC016_ParseRejectsMalformed`; H2: `TestEX2_AC016_MalformedStartAlertsAsToday`; HT: `TestEX2_AC016_MalformedStartStoredAsToday` |
| AC-017 | AD-002 tx, unique index, cap | S: `TestEX2_AC017_RepeatedStartsOneRecord`, `TestEX2_AC017_RateCapSixPerDayOneWarning`; R: `TestEX2_AC017_PG_ConcurrentStartsOneRecordOneCapEvent`; H2: `TestEX2_AC017_CappedStartOfflineAlertsNoDebugAbove` |
| AC-018 | Device-scoped (INFO-1 extension) | H2: `TestEX2_AC018_OtherDeviceUnaffected`, `TestEX2_AC018_OtherDeviceRegisterCompleteFailLeavesRecord` (vermont registers on another version, sends completed and failed while hawaii's record is open: hawaii's row, label and coverage unchanged); HT: `TestEX2_AC018_MismatchedAuthenticatedDeviceIgnored` |
| AC-019 | `SafeText(…, 200)` | M: `TestEX2_AC019_ReasonSanitizedAndBounded`; H2: `TestEX2_AC019_FailedEventMessageSanitized` |
| AC-020 | Fail open | H2: `TestEX2_AC020_StoreFailureFailsOpen`; HT: `TestEX2_AC020_EventStoredWhenRecordStoreFails` |
| AC-021 | AD-004 step 2 | S: `TestEX2_AC021_CompletionWithoutRecordUnlinked` |
| AC-022 | AD-013 | H2: `TestEX2_AC022_BoundedEventsLogsMemory` |
| AC-023 | AD-009, Section 6.3 (C-3) | H2: `TestEX2_AC023_ActivityCarriesVersionsAndActor`; CE: `TestEX2_AC023_CommandEventsEndpoint` (**200 linked agent events plus an offline info, an online note and an outcome event: every server event returned, 50 agent events, `agent_events_omitted = 150`**), `TestEX2_AC023_CommandEventsNotFoundIdentical` (malformed, missing, foreign → identical 404 body); WF: `EXPECT-002 [AC-023] fleet label, actor, start and expected-offline count`; WD: `EXPECT-002 [AC-023] header link opens the read-only record`; WS: `EXPECT-002 [AC-023] details show versions, outcome, server events, the omitted count, and no controls`; WA: `EXPECT-002 [AC-023] label clears on activity_update null` |
| AC-024 | AD-003, AD-007 (C-2) | M: `TestEX2_AC024_ActorSelfUpdateEdges`; S: `TestEX2_AC024_NeverClaimedOrDelivered`, `TestEX2_AC024_AgentReportNotFoundHTTPAndWS` (ack, progress, result), `TestEX2_AC024_CancelNotCancellable`; R: `TestEX2_AC024_PG_CheckForbidsQueued`, `TestEX2_AC024_PG_CheckBoundsDeadlineAndKeys` (deadline > D + 10m, `exec_deadline_s` 601, a target key: each rejected), `TestEX2_AC024_PG_AgentOperatorServerGuards` (agent, operator and **server `→ timed_out`** on a self-update row do not apply; `ActorSelfUpdate` does not apply to an ordinary command); the mock parity test `TestEX2_AC024_MockGuardParity` |
| AC-025 | Latest D | H2: `TestEX2_AC025_CommandAndRecordOneLabel` |
| AC-026 | AD-005, X-1 | H2: `TestEX2_AC026_OlderAgentNoCompletionSucceeds`, `TestEX2_AC026_CompletionBeforeRestartStillCovered`, `TestEX2_AC026_OtherWordingAsToday` |
| AC-027 | `available` untouched apart from the AD-015 clamp | HT: `TestEX2_AC027_UpdateAvailableUnchanged` |
| AC-028 | No EXPECT-001 change | Every existing `TestEX_*` test passes unmodified; H2: `TestEX2_AC028_CommandAgentUpdateUnchanged` |
| AC-029 | AD-014 (C-1) | Z: `TestEX2_AC029_NoPowerUpdateOrAgentExecution` (named `must` list incl. `internal/testutil/mocks.go`, `self_update_repo.go`, `command_events.go`; parts (b) and (c); the `ex2Preexisting` table), `TestEX2_AC029_ScannerCatchesEachForbiddenForm` (incl. `os.StartProcess`, `x/sys/unix`, `x/sys/windows`, `internal/agent`); WZ: `EXPECT-002 [AC-029] new web tests and sources spawn nothing` (asserts the three test files and two sources are in the set) |
| AC-030 | Docs (incl. the AD-010 budget) | D: `TestEX2_AC030_ReadmeAndChangelog` |

### 8.1 Supporting tests

| Topic | Test |
|---|---|
| Q-1 | S: `TestEX2_Q1_RegistrationAfterBoundIgnored` |
| Q-2 | S: `TestEX2_Q2_OneLinkPerEvent` |
| X-1 | H2: `TestEX2_X1_CompletionBeforeReturnCoversRestart`, `TestEX2_X1_NoReturnByBoundEscalates` |
| X-3 | H2: `TestEX2_X3_HeartbeatVersionConfirmsAtBound` |
| X-7 | H2: `TestEX2_X7_FailedWithCoveredOfflineEscalatesOnce` |
| X-19 (AD-015) | HT: `TestEX2_X19_AgentUpdateSeverityClamped` (`""`, `"panic"`, `"WARNING"` and a 1 KB string become info or warning per type; `info`, `warning`, `critical` are kept) |
| Online note only for covered offline | H2: `TestEX2_NoteOnlineOnlyForCoveredOffline` |
| Sweeper | S: `TestEX2_SweeperSkipsSelfUpdate`; R: `TestEX2_PG_ListOverdueExcludesSelfUpdate` |
| Existing actors unchanged | M: `TestEX2_ExistingActorEdgesUnchanged` |
| Migration | R: `TestEX2_PG_Migration000034UpDownUp`, `TestEX2_PG_UniqueOpenRecord`, `TestEX2_PG_OriginCheck` |
| No completion event or result columns | S: `TestEX2_NoCompletionEventForSelfUpdate`, `TestEX2_SelfUpdateTransitionsLeaveResultColumnsAndGateUntouched` |
| Outcome jobs never dispatched or counted | H2: `TestEX2_OutcomeEventsNeverDispatchedNorCeilinged` |
| Stuck record (C-4) | H2: **`TestEX2_ResolverUnsetFailsSafe`** (no resolver: coverage ends at `hard`, the escalation fires, and at `hard + 1h` exactly one `agent_self_update_paused` event exists, with no push and one Warn, also across a restart); S: `TestEX2_StaleOpenRecordRetiredByNextStart`; R: `TestEX2_PG_StaleRetireInTx` |

### 8.2 Security condition tests

| Condition | Tests |
|---|---|
| C-1 | `TestEX2_AC029_NoPowerUpdateOrAgentExecution`, `TestEX2_AC029_ScannerCatchesEachForbiddenForm`, WZ `EXPECT-002 [AC-029] …` |
| C-2 | `TestEX2_AC024_PG_AgentOperatorServerGuards`, `TestEX2_AC024_PG_CheckBoundsDeadlineAndKeys`, `TestEX2_AC024_MockGuardParity`, `TestEX2_AC024_ActorSelfUpdateEdges` |
| C-3 | `TestEX2_AC023_CommandEventsEndpoint`, WS details test |
| C-4 | `TestEX2_ResolverUnsetFailsSafe`, `TestEX2_StaleOpenRecordRetiredByNextStart`, `TestEX2_PG_StaleRetireInTx` |
| C-5 | `TestEX2_AC030_ReadmeAndChangelog` asserts the budget text |
| C-6 | The Section 13 gate |

---

## 9. Error Handling

| Failure | Behaviour | Log |
|---|---|---|
| Start message invalid | No record; event as today; alerting as today | Debug |
| Unknown severity on `agent_update_*` | Clamped (AD-015) | none |
| Hostname read fails | `device` in messages | Debug |
| `CreateSelfUpdate` fails | Empty link; event stored; offline alerts | Warn (Service `logThrottle`) |
| Unique-index violation | Re-read → `open_record`, else empty link | Debug |
| Attribution or outcome transition fails | Event stored unlinked; record ends at the bound | Warn (throttled) |
| Registration hook fails | No change; window to the bound; registration unaffected | Warn (throttled) |
| `devices.Update` fails | 500, as today (the heartbeat refresh rides on it) | Error (existing) |
| Agent event insert fails after record creation | 500 to the agent; the record ends normally | Error (existing) |
| `finishSelfUpdate` step fails | Retry each tick; evict after 5 min, reload; coverage ends at `hard`; the backstop is not delayed | Warn (per kind) |
| Resolver not wired, or resolution keeps failing | As above. At `hard + 1h`: one `agent_self_update_paused` warning event (marker `stuck`, no push), and the next start retires the row (C-4) | Warn `self_update_stuck` (throttled) |
| Detector gate never opens (detector not wired) | Bound processing proceeds at `graceEnd + 30s` | none |
| Tracker not ready | `CoverOffline` fails open; records are still created and resolved later | Existing |
| Publisher queue full | Event stored; push or broadcast dropped (EXPECT-001 R-3) | Warn |
| Events endpoint with a bad ID | 404 `command_not_found` (identical body) | none |

---

## 10. Security Considerations

| Vector | Mitigation |
|---|---|
| Forged starts to suppress the device's own offline alerts (NFR-010, SEC-005) | Strict grammar. ≤ 1 open record. 10-minute CHECK-enforced bound. 6 per 24 h. Accepted budget ≈ 57 min per day as ≤ 6 outages of < 10 min (AD-010). Each is an `info` event. Escalation at each bound. No chaining (X-7). `paused` warning at the cap |
| Cross-device effect (INFO-1) | Authenticated device only (`DeviceAuth` binds `{id}`; re-checked). Every query is device-scoped. Coverage is per device |
| Writes to a record by anything but the self-update actor (SEC-002) | Origin guard on agent, operator **and server** edges; `ActorSelfUpdate` only matches self-update rows; `classifyRejected` returns `not_found`; CHECK on status, bound and keys |
| Operator or API mutation | Cancel 409. `Dispatch` cannot create the origin. No retry |
| Forged completion or failure | Can only shorten or fail a record, never extend or create one |
| Agent flooding hides the record's truth (SEC-003) | The events endpoint returns every server-authored event separately, plus the omitted agent count |
| Stuck record silently disables tracking (SEC-004) | One durable warning at `hard + 1h`; the next start retires the row |
| Untrusted text | Closed version grammar. Reasons through `SafeText(…, 200)`. Hostnames through `SafeName`. Reported versions only if valid. Severity clamped (AD-015). Insert-time sanitize and redact. React text only |
| Agent-supplied `command_id` | Ignored (X-2) |
| Registration as a heartbeat | Only after REG-HARDEN-001 proof of the named device, which is the same trust as a heartbeat. It adds no suppression |
| Parsing cost and DoS | ≤ 4 KB; one short tx under a per-device lock; ingest bucket; Debug logs |
| Clock skew | Server receive time only |
| SQL injection | Bind parameters; typed-constant literals |
| Real execution in tests (NFR-001) | The C-1 Go and web scans (AD-014) plus the EXPECT-001 scan |

---

## 11. Performance Considerations

| Path | Added cost | Index |
|---|---|---|
| `agent_update_started` | 1 PK read, 1 tx (lock, ≤ 4 selects, ≤ 3 writes) | `idx_commands_device`, `idx_commands_self_update_open`, `idx_commands_self_update_recent`, `idx_events_device_time` |
| `agent_update_completed`/`failed` | 1 PK read, 1 select, ≤ 1 update | `idx_commands_self_update_recent` |
| Registration | Same UPDATE (one more column), 1 select, ≤ 1 update | `idx_commands_self_update_open` |
| Tracker | ≤ 1 window per device; ≤ 5 DB calls per record end | Existing |
| Sweeper | One extra predicate | Existing |
| `GET …/events` | 2 selects plus 2 counts | `idx_events_command` |

NFR-007 has wide headroom at 4 devices.

---

## 12. Implementation Notes for Engineers

1. **Lock discipline.** As EXPECT-001 Section 12 note 2. `Service.broadcast` → `CommandChanged` takes `t.mu` briefly, never `tickMu`.
2. **`RegisterDevice` order.** Set `LastHeartbeat` → `Update` → `NoteRegistration` → `NoteSelfUpdateRegistration` → response and broadcast → `DeviceOnline`.
3. **Typed nils.** Never pass a nil `*commands.Service` or `*db.CommandRepo`.
4. **Guard parity.** The four-actor origin guard and the CHECK are mirrored in `MockCommandRepo` (`TestEX2_AC024_MockGuardParity` compares mock and PG outcomes over a table of actor × origin × status).
5. **No `completed()`, policy, progress gate or result columns** for self-update transitions (C-6). Do not route through `afterAgentTransition` or `afterSweep`.
6. **`trimCommand`.** It copies `VerifyingAt`. Versions are read from `params` **before** trimming.
7. **Safety scans (C-1).** See AD-014. Keep the Go `must` list equal to the Section 4 Go files (a test asserts every listed file exists). Keep `ex2Preexisting` minimal: each entry needs a reason, and stale entries fail. The web scan asserts its five required files. The EXPECT-001 scans stay unmodified.
8. **Existing tests stay unmodified:** `TestEX_*`, the CMD-LIFECYCLE transition-table and `buildTransitionSQL` tests (`Contains`-based, still valid), and the EXPECT-001 web tests. New cases go in new files, or are additive in `activity.test.ts` and `harness_test.go`. If an existing test breaks only because a re-registration now writes `last_heartbeat`, the implementation report names it and Ryan decides. Do not edit it silently.
9. **Version strings.** Re-validate `params` in the Tracker and the web.
10. **`ReceiveAgentEvent`** keeps its exact response codes.
11. **Build environment.** `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`; `-race` in Linux CI.
12. **Work packages.**
    - WP-1: models, migration, repo, mocks, PG;
    - WP-2: commands Service, sweeper, `classifyRejected`;
    - WP-3: expect and H2;
    - WP-4: handlers, route, wiring;
    - WP-5: web;
    - WP-6: safety scans;
    - WP-7: docs.

    Run `go test ./...` and `cd web && npm run test:run` after each.
13. **Implementation report.** It must state:
    - the 5.2 rollback note;
    - `ReceiveAgentEvent` uses the injected clock and clamps severity;
    - a re-registration now refreshes `last_heartbeat` for every device;
    - `ListOverdue` excludes self-update rows;
    - the `ex2Preexisting` entries, if any.

---

## 13. Definition of Done

- [ ] Every Section 4 change is implemented. No agent, `install.sh`, sudoers or dependency change.
- [ ] Every test in Section 8, 8.1 and 8.2 exists under its name and passes with an injected clock. No sleeps.
- [ ] `go test ./...` and `cd web && npm run test:run` are green; `-race` in Linux CI.
- [ ] The existing `TestEX_*` and CMD-LIFECYCLE tests pass **unmodified** (AC-028).
- [ ] Migration 000034 runs up, down and up on PostgreSQL 16. The CHECK (status, bound, `exec_deadline_s`, keys) and the unique index are PG-tested.
- [ ] No new lint errors (`go vet`, frontend lint, `tsc`).
- [ ] The AD-013 bounds appear as code comments at each bound.
- [ ] **QA gate (C-6):**
  - QA verifies:
    - self-update transitions never touch the progress gate and never set `result_bytes`, `steps`, `target_results`, `stderr_tail`, `exit_code`, `duration_ms` or command output (`TestEX2_SelfUpdateTransitionsLeaveResultColumnsAndGateUntouched`);
    - `agent_self_update_*` events and outcome jobs are never dispatched (nil rule) and never consume the per-device push ceiling (`TestEX2_OutcomeEventsNeverDispatchedNorCeilinged`);
    - `ReceiveAgentEvent` ignores any agent `command_id`, and runs self-update work only when the authenticated ID equals `{id}` (`TestEX2_AC018_MismatchedAuthenticatedDeviceIgnored`, `TestEX2_Q2_OneLinkPerEvent`);
    - the events endpoint returns one identical 404 body for malformed, missing and foreign IDs (`TestEX2_AC023_CommandEventsNotFoundIdentical`);
    - agent reports on a self-update row return `not_found` for ack, progress and result, over HTTP and WebSocket (`TestEX2_AC024_AgentReportNotFoundHTTPAndWS`).
  - These pass:
    - AC-016, AC-018 (with `TestEX2_AC018_OtherDeviceRegisterCompleteFailLeavesRecord`), AC-019, AC-020, AC-024 (all tests);
    - `TestEX2_Q2_OneLinkPerEvent`;
    - `TestEX2_AC015_*` (incl. `…DeadDuringDowntimeEscalatesNotUnconfirmed`);
    - `TestEX2_AC005_*` (incl. `…StaleHeartbeatAfterFastReturnNoAlert`);
    - `TestEX2_ResolverUnsetFailsSafe`.
  - QA confirms that the printed AC-029 lists (Go and web) include every new or modified file of the diff.
- [ ] No test or helper runs a power action, a package manager or the agent updater, starts a process, or executes `riot-agent`.

---

## Security addendum (review 09df74e, conditions C-1 to C-6)

| Condition | Design change | Sections | Tests |
|---|---|---|---|
| **C-1** SEC-001 scan coverage | The Go scan set has three parts: (a) every Section 4 Go file by name (incl. `internal/testutil/mocks.go`, `self_update_repo.go`, `command_events.go`); (b) every `_test.go` with `TestEX2_`; (c) every `_test.go` in a touched package directory, with a reviewed `ex2Preexisting` table. New patterns: `os.StartProcess`, `x/sys/unix`, `x/sys/windows` and `internal/agent` imports, with self-tests. A new web EX2 scan (tests mentioning `EXPECT-002` plus five sources; it asserts three test files). The claim that the EXPECT-001 web scan covers EX2 tests is removed | AD-014, Section 4, Section 8, Section 12 note 7 | Section 8.2 C-1 row |
| **C-2** SEC-002 server edge and SQL invariants | The bound's `→ timed_out` moves to `ActorSelfUpdate`, which gets 7 edges. `ActorServer`, `ActorAgent` and `ActorOperator` render `origin <> 'agent_self_update'`. The shape CHECK adds `deadline_at <= dispatched_at + 10 min`, `exec_deadline_s <= 600` and `cardinality(target_keys) = 0`. PG and mock parity tests | AD-001, AD-003, AD-006, AD-007 row 7, Section 5.1 | Section 8.2 C-2 row |
| **C-3** SEC-003 record view under flooding | The endpoint returns every server-authored linked event (≤ 500; ≤ 19 per self-update record) separately from the agent-sourced ones (≤ 50), with the omitted counts. No FRD deviation | Section 6.3, AD-009, AD-013 | `TestEX2_AC023_CommandEventsEndpoint` (200 agent events) |
| **C-4** SEC-004 stuck record | At `hard + 1h` a still-unresolved row gets one `agent_self_update_paused` event (marker `stuck`, no push) and one throttled Warn. The next valid start retires it (`timed_out` / `unresolved`) in the creation tx, so the unique index never blocks forever | AD-002, AD-006 step 5, AD-012, AD-013, Section 9 | `TestEX2_ResolverUnsetFailsSafe`, `TestEX2_StaleOpenRecordRetiredByNextStart`, `TestEX2_PG_StaleRetireInTx` |
| **C-5** SEC-005 budget | Accepted on Ryan's behalf; the cap stays at 6; the README states ≈ 57 min per day as ≤ 6 outages of < 10 min, `info` events, and a `warning` with no push for wrong-version returns | AD-010, Open points | `TestEX2_AC030_ReadmeAndChangelog` |
| **C-6** QA gate | The listed properties and tests | Section 13 | Section 13 |

**Informational items folded in:**
- INFO-1: AC-018 extended.
- INFO-3: severity clamp (AD-015).
- INFO-4: detector gate (AD-006).
- INFO-5: registration counts as a heartbeat (AD-005, AD-008).
- INFO-6: residual R-6 and a follow-on candidate.
- INFO-2, INFO-7, INFO-8, INFO-9 and INFO-10 confirm the design; X-13 is updated per INFO-2.

**Not satisfied:** none.

---

## FRD Deviations and Clarifications

| ID | FRD item | Resolution | Rationale |
|---|---|---|---|
| X-1 | FR-011 (a), FR-013 | A completion **before any return** sets `succeeded` but keeps offline coverage until the first return after it, or the bound | The `installed` path and pre-AGENT-HARDEN agents complete and then restart. Never past `hard`; X-7 escalates if no return comes |
| X-2 | FR-001, FR-014 "without a command ID" | An agent `command_id` on `agent_update_*` is ignored for attribution | Agent-supplied IDs are never trusted |
| X-3 | FR-015 bound rule | Also `succeeded` when the device is online at the bound and its heartbeat-reported version matches <to> | A lost startup registration is retried only after 24 h. Affects the outcome only |
| X-4 | FR-019 message | The reported version is shown only if valid, else `an unrecognised version` | Untrusted input |
| X-5 | FR-006 "open record" | Outcome not set (`running` or `verifying`), unique index. A row unresolved 1 h after its bound is retired by the next start (C-4) | One durable invariant, self-healing |
| X-6 | FR-015 storage | `succeeded`, `failed`, `timed_out` ("Not confirmed"); `verifying` = closed, outcome pending | AD-003 |
| X-7 | FR-020 scope | Any self-update window end with its suppressed offline still ongoing escalates once (failed variant text where it applies) | EXPECT-001 C-2. It is what prevents chaining (SEC-005) |
| X-8 | FR-018, FR-019, FR-021, FR-007 event identity | Three new types. Outcome events are exactly-once via the marker `outcome`. The stuck-record warning reuses `agent_self_update_paused` | Filterable; NFR-006 (c) |
| X-9 | FR-027 linked events | New endpoint with server and agent events separated (C-3) | No existing API |
| X-10 | AC-016 10 KB message | 4 KB pre-check | Bounded work |
| X-11 | FR-002 "no control characters" | Also rejects Cf, Zl, Zp | Stricter only |
| X-12 | AC-025 "linked to one of the two" | The latest dispatch (the command) | EXPECT-001 FR-007 |
| X-13 | FR-007 cap | Counted from durable rows. It cannot be undercounted by a purge: the dated budget purge selects only rows with `result_bytes`, which these rows never carry (C-6), and the emergency purge keeps a 7-day floor (INFO-2) | Restart-safe |
| X-14 | NFR-011, Q-1 | Not in normal operation; a periodic re-registration delayed > 30 s remains, toward alerting | Code facts |
| X-15 | FR-011 (c)/(d), FR-019 | Existing-device re-registration only, row `running`, receive time < bound | Precise definition |
| X-16 | FR-014 "otherwise no link" | Kept. Completed and failed events are never linked to commands | As written |
| X-17 | NFR-008 / AC-026 | Older agents complete **before** restarting; X-1 makes them work; an extra test is added | The FRD's fake agent is narrower than reality |
| X-18 | FR-016 "back by 18:01" | Server local time | EXPECT-001 AD-015 |
| X-19 | FR-005, FR-030 "stored as today" (severity) | An unknown agent severity on the four `agent_update_*` types is clamped: `info` (available, started, completed) or `warning` (failed). Known values are unchanged | Security review INFO-3, decided on Ryan's behalf. Real agents are unaffected |
| X-20 | (new behaviour, all devices) | A re-registration refreshes `last_heartbeat` | INFO-5. Prevents a stale-heartbeat "went offline" push right after a fast return. Adds no suppression |

## Residuals (in addition to the FRD's NFR-011)

- **R-1.** A record whose bound passes while the server is down stays `running` until the first tick after the detector gate.
- **R-2.** If the Tracker cannot write, outcome events and escalations are delayed. Coverage still ends at the bound, alerts fail open when readiness lapses, and C-4 makes a long stall visible.
- **R-3.** A command-path `agent_update_failed` or `agent_update_completed` within a record's 10 minutes is attributed to the record (toward alerting).
- **R-4.** A heartbeat claim does not take the advisory lock. A coincidental self-update at the same instant can race it. Both windows are then evaluated separately (benign; INFO-2).
- **R-5.** Rolling back 000034 deletes the self-update history.
- **R-6 (INFO-6).** A self-update that starts after Ryan dispatched a shutdown (or another host action) and before its offline takes over that offline's attribution, because the latest dispatch wins. This can produce one extra "still offline 10m after Agent update …" push for a host Ryan powered off on purpose. Toward alerting; accepted.

## Follow-on candidates

- **EXPECT follow-on (from INFO-6):** a self-update window yields to an in-flight host or shutdown window of the same device. Added to the EXPECT follow-on list, next to EXPECT-007.

## Open points for Ryan

Decisions Clod made on Ryan's behalf (reviewer's recommended option; Ryan reviews them in the PR):
- **SEC-005 budget accepted.** About 57 min per day of unalerted offline time, as at most 6 outages of under about 10 min each, same device only. Each one leaves an `info` event, plus a `warning` (no push) on a wrong-version or not-confirmed outcome. The cap stays at **6 per device per 24 h**. The README residuals state it (C-5).
- **X-1, X-3 and X-7 kept.** X-7 is required for the budget to hold.
- **INFO-6 accepted** as residual R-6, with the follow-on candidate above.
- **Agent update event severity is fixed in this story** (AD-015, X-19). Other pre-existing items from the review's private notes stay out of scope and are filed separately.
- **INFO-5 resolved by design:** a registration counts as a heartbeat for every device (X-20). If Ryan prefers a self-update-only fix, the alternative is to extend coverage to the first heartbeat after the return, capped at `hard`.

Still for Ryan:
- **R-5:** rolling back 000034 deletes the self-update history.
