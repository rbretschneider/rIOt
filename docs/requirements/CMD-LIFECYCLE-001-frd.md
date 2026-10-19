# Formal Requirements Document

**Story ID:** CMD-LIFECYCLE-001
**Title:** Reliable command lifecycle and Docker update execution
**Author:** Business Developer Agent
**Date:** 2026-10-17
**Status:** DRAFT

---

## 1. Executive Summary

Commands sent from the rIOt dashboard to agents (Docker updates, reboots, OS
patches) currently round-trip with misleading statuses, are lost when the agent
WebSocket is down, can run twice, can fire days late, and Docker updates fail or
time out midway, sometimes leaving containers removed or network-dependent
containers stopped. Phase 1 fixes nine confirmed defects in auto-update
targeting, agent WebSocket writes, result delivery, compose network dependents,
standalone update rollback, timeouts, update detection, concurrency, and live
telemetry ordering. Phase 2 adds an explicit, server-enforced command lifecycle
(queued → dispatched → acknowledged → running → verifying → terminal), so the
dashboard follows each command by ID and shows its real state and current step.
All of this must work with old agents and old servers during a mixed-version
rollout.

## 2. Background & Context

Problems confirmed in code (grounding: base `main@a7b9f9e`):

- Per-container auto-update always fails: the server sends `container`, but the
  agent requires `container_id`. The failure then starts the 7-day cooldown.
- The agent writes to its WebSocket from several goroutines with no locking.
  gorilla/websocket panics on concurrent writes, which kills the agent during a
  compose update.
- Results are sent only over the WebSocket. If it is down, the result is
  silently dropped and the row stays `sent` forever. No sweeper exists, and
  queued commands have no TTL.
- Network-dependent containers are stopped and never restarted.
- A standalone update ignores pull errors, and its remove-then-create sequence
  cannot be rolled back.
- One 10-minute limit covers compose pull and up, and hitting it SIGKILLs the
  process. The `update_failed` event is then sent on an already-expired context.
- Update detection checks the tag's local image, not the image the container is
  actually running. Failed checks are cached for 30 minutes, and after any
  update the whole cache is wiped.
- There are no in-flight locks. Heartbeat delivery can deliver the same command
  twice. When an agent reconnects, the old connection's cleanup can remove the
  new connection. Reconnect backoff never resets.
- The dashboard applies older buffered telemetry after fresher telemetry, uses
  the browser clock, tracks actions only by HTTP round-trip, and briefly shows
  `exited` during a recreate.

Current statuses are `pending | queued | sent | success | error`. The dispatch
logic is copied in four places: manual send, bulk update, auto-update, and
auto-patch.

**Delivery slicing (guidance, not a requirement):** each Phase 1 item (§4.1–4.9)
can ship on its own. Phase 2 (§4.10) depends on §4.3 and §4.8. The architect may
split delivery into increments, but all acceptance criteria in this FRD apply to
the story as a whole.

## 3. Actors

| Actor | Description | Permissions |
|---|---|---|
| Operator | Authenticated dashboard user | Sends commands, views command state, cancels **queued** commands |
| rIOt server | Go API, dispatcher, sweeper, WS hub | Creates, delivers, and transitions commands, enforces the state machine, runs auto-update and auto-patch |
| Auto-update / auto-patch dispatcher | Server-internal automation | Creates commands with origin `auto_update` / `auto_patch`, subject to policy, window, cooldown, and backoff |
| New agent | Agent built from this story | Acknowledges, reports progress, and keeps final results in a durable outbox. Authenticates with `X-rIOt-Key` |
| Legacy agent | Agent released before this story | Sends only a final `command_result` (`success`/`error`) over WS. Requires `container_id` |
| Legacy server | Server released before this story | Ignores unknown WS message types. Has no HTTP result endpoint (returns 404) |

## 4. Functional Requirements

### 4.1 Item 1: Auto-update targeting and retry

- **FR-001:** For a container-level policy, auto-update dispatch must include both the container name (`container`) and the container's current ID (`container_id`), taken from the latest telemetry.
- **FR-002:** When `container` (name) is present, the agent must resolve the `docker_update` target by name. It must use `container_id` only when no name is given. A leading `/` must not affect name matching.
- **FR-003:** If the target cannot be resolved, the agent must report `failed` with step `resolve` and a message that names the target.
- **FR-004:** The server must set a policy's last-triggered time only when a command that the policy originated reaches `succeeded`. It must use the time of that transition.
- **FR-005:** When a policy-originated command ends `failed`, `timed_out`, `expired`, or `cancelled`, the server must increment the policy's consecutive-failure count *n*. The policy must not dispatch again before terminal time + min(15 min × 2^(n−1), 6 h).
- **FR-006:** When a policy-originated command succeeds, the server must reset the policy's consecutive-failure count to 0.
- **FR-007:** A policy must not dispatch while a command it originated is non-terminal.
- **FR-008:** A one-time data change must clear last-triggered on every container-level (non-stack) Docker auto-update policy. Stack policies must not change.
- **FR-009:** Backoff retries must still obey the existing maintenance-window and stagger rules. Backoff must never let a dispatch happen outside a configured window.

### 4.2 Item 2: Serialized agent WebSocket writes

- **FR-010:** The agent must serialize all writes to its WebSocket connection, so that at most one write is in progress at any instant. This covers command results, lifecycle messages, terminal output, and host-terminal output.
- **FR-011:** The agent must synchronize every read and write of its current-connection reference, including connect, disconnect, and send.
- **FR-012:** A failed WebSocket write must return an error to the caller and must not terminate the agent process.

### 4.3 Item 3: Result durability, sweeper, TTL

- **FR-013:** The agent must write each final command result to a durable outbox before its first delivery attempt.
- **FR-014:** The agent must deliver outbox entries over the WebSocket when connected. Otherwise it must use an HTTP result endpoint authenticated with the device key.
- **FR-015:** The agent must remove an outbox entry only after one of these:
  - the HTTP endpoint returns a 2xx, or reports that the command is already terminal;
  - a server receipt for that command arrives over the WebSocket;
  - in legacy-server mode (FR-022), the WebSocket write succeeds.
- **FR-016:** Undelivered entries must be retried with backoff that starts at 5 s, doubles each time, and is capped at 5 min.
- **FR-017:** The outbox must hold at most 200 entries and must drop entries older than 24 h. When it is over the bound, the agent must drop the oldest entry first and write a warning log.
- **FR-018:** Outbox entries must survive an agent restart and be delivered after it.
- **FR-019:** At startup, the agent must report each command that was executing when the agent stopped as `failed`. The report must carry the last recorded step and the message "agent restarted during execution; outcome unknown". Agent self-update and reboot actions are excluded and keep their existing completion reporting.
- **FR-020:** The server must accept a duplicate final result for an already-terminal command without changing it. The HTTP response must be 2xx.
- **FR-021:** The HTTP result endpoint must reject a result for a command that does not belong to the authenticated device. The command must not change.
- **FR-022:** If the HTTP result endpoint returns 404, the agent must switch to legacy-server mode, which delivers over WS only and removes entries after a successful write. It must retry HTTP detection no more often than once per hour.
- **FR-023:** The server sweeper must move any `dispatched`, `acknowledged`, `running`, or `verifying` command past its execution deadline (§7.3) to `timed_out`, with the message "no result from agent within <deadline>".
- **FR-024:** The sweeper must move any `queued` command past its `expires_at` to `expired`.
- **FR-025:** Every command must get an `expires_at` at creation, per the TTL table (§7.3). For auto-dispatched commands under a `window`-mode maintenance window, `expires_at` must be the earlier of the TTL and the current window's end.
- **FR-026:** The server must never deliver a command whose `expires_at` has passed. This must be checked when the command is claimed, not only by the sweeper.
- **FR-027:** The sweeper must run at server startup and then at least every 60 s.

### 4.4 Item 4: Compose network dependents

- **FR-028:** The agent must treat a container as a network dependent of a parent when its network mode references the parent by name (with or without a leading `/`), by full container ID, or by compose `service:` name.
- **FR-029:** For a single-service update, the agent must stop only the dependents of the containers being recreated.
- **FR-030:** When an update ends, every dependent that was running beforehand must be running and attached to the network namespace of the current (post-update or restored) parent. This applies to every outcome: success, failure, step timeout, error, or recovered panic.
- **FR-031:** The agent must not start a dependent that was not running before the update.
- **FR-032:** Restoring dependents must have its own 2-minute budget, separate from any expired operation limit.
- **FR-033:** If any dependent cannot be restored, the command must end `failed` with step `restart_dependents`. The message must list the affected dependents and state the outcome of the main update.

### 4.5 Item 5: Standalone update safety

- **FR-034:** The agent must read the full pull stream. Any error entry in it must end the command `failed` with step `pull`, with the registry error text in stderr. The running container must not be stopped.
- **FR-035:** If the image pulled for the tag has the same ID as the running container's image, the agent must not recreate the container and must report `succeeded` with the message "already up to date".
- **FR-036:** Before creating the replacement, the agent must stop the old container and rename it to a non-conflicting name. It must not remove the old container before verification passes.
- **FR-037:** The replacement must take the original name and the old container's configuration. It must end in the same run state (running or stopped) that the old container had before the update.
- **FR-038:** If the old container was running, verification passes only when the replacement is running, and is not restarting or exited, 10 s after start.
- **FR-039:** If create, start, or verification fails, the agent must take these steps and then report `failed` with the failing step and the message "rolled back to previous container":
  - remove the replacement, if one was created;
  - give the old container back its original name;
  - return the old container to its prior run state.
- **FR-040:** The agent must remove the old container only after verification passes.
- **FR-041:** If rollback fails, the agent must report `failed` with step `rollback` and a message that names the set-aside container. The old container must never be removed in this case.
- **FR-042:** If a set-aside container from an earlier failed rollback still exists for the target, a new update must end `failed` with step `precheck` and name that container. The agent must not delete it.

### 4.6 Item 6: Timeouts

- **FR-043:** The agent must enforce separate limits: compose pull 15 min, compose up 10 min, standalone pull 15 min.
- **FR-044:** When a limit is reached, the agent must send SIGTERM to the child process and wait a 60-second grace period. It must send SIGKILL only if the process is still running after that.
- **FR-045:** A step that hits its limit must end the command `failed`, with that step and the message "timed out after <limit>".
- **FR-046:** Lifecycle events (including `update_failed`) and command results must be sent on a fresh 10-second budget, not one derived from the operation's limit. Final results must also go through the outbox (FR-013).

### 4.7 Item 7: Update detection

- **FR-047:** A container must be reported update-available when either: (a) the registry digest for its tag differs from the local digest of that tag, or (b) the container's running image ID differs from the image ID the tag currently resolves to locally.
- **FR-048:** An update-available result must carry a reason: `registry_newer` for case (a), `pulled_not_recreated` for case (b).
- **FR-049:** The verdict must be computed per container. Registry lookups must be cached per image reference, so containers on the same tag can get different verdicts.
- **FR-050:** For an image reference that is a bare `sha256:` ID or contains `@sha256:`, the agent must make no registry call and must report status `pinned`, not update-available.
- **FR-051:** A failed registry check must be reported as `check_failed`, never as up-to-date. Failed checks must be cached for 5 min; successful checks keep the existing 30-minute cache.
- **FR-052:** When a `docker_update` ends on the agent, only the cache entries for the image references involved (before and after the update) may be invalidated, both in memory and on disk. All other entries must remain.
- **FR-053:** The dashboard must not show `check_failed` or `pinned` as update-available. The update-available indicator's `title` text must state the reason.

### 4.8 Item 8: Concurrency

- **FR-054:** The agent must hold an in-flight lock per target. The lock key is the compose project for compose containers and stacks, the container name for standalone containers, and the device for host-scoped actions.
- **FR-055:** A command for a target that is already locked must end `failed` with step `precheck` and the message "update already in progress for <target> (command <id>)". Docker state must not change.
- **FR-056:** The lock must be released on every outcome, including a recovered panic.
- **FR-057:** The agent must not run the same command ID twice. Seen IDs must be persisted for at least 24 h, up to 1000 entries.
- **FR-058:** When a device+target already has a non-terminal command, the server must reject a new command for it with HTTP 409. The response must include the existing command's ID, and no new row may be created.
- **FR-059:** A Docker target is a container name or a compose project. Host-scoped actions (reboot, OS patch/upgrade, agent update) share a single device-level target. Docker targets and the host target do not conflict with each other (see OQ-001).
- **FR-060:** A bulk update must skip targets that conflict, report each one as `conflict` with the existing command ID, and still create commands for the targets that do not conflict.
- **FR-061:** When an auto-dispatcher hits a conflict, it must skip the dispatch and write a log entry. The conflict must not count toward backoff.
- **FR-062:** Delivery must use one atomic claim: `queued` and not expired → `dispatched`. At most one claim may succeed per command. Heartbeat responses must contain only commands claimed by that heartbeat.
- **FR-063:** All four dispatch paths (manual, bulk, auto-update, auto-patch) must apply FR-025, FR-058, and FR-062 identically.
- **FR-064:** Server cleanup of an agent WebSocket must remove the device's registration only if it still points to that same connection. A new registration for a device must close the device's previous connection.
- **FR-065:** After a successful connect, the agent must reset its WebSocket reconnect backoff to the initial delay.

### 4.9 Item 9: Live telemetry ordering

- **FR-066:** Every live telemetry broadcast must include `collected_at` (agent time) and `received_at` (server time).
- **FR-067:** The dashboard must discard any live telemetry message whose `collected_at` is at or before the latest `collected_at` already applied for that device.
- **FR-068:** The dashboard must not use the browser clock as the timestamp of telemetry data or as a device's last-seen value. Last-seen must come from `received_at`.
- **FR-069:** The server must not replace a device's current telemetry state with a snapshot whose `collected_at` is older than the current one. The older snapshot must still be stored in history.
- **FR-070:** If an agent snapshot has no collection timestamp, the server must use `received_at` as its `collected_at`.

### 4.10 Phase 2: Command lifecycle

**Server state machine**

- **FR-071:** Commands must use these canonical states: `queued`, `dispatched`, `acknowledged`, `running`, `verifying`, `succeeded`, `failed`, `timed_out`, `cancelled`, `expired`.
- **FR-072:** The server must allow only the transitions in §7.2. A disallowed transition must leave the state unchanged and write a log entry. Over HTTP it must be rejected with 409; over WS it must be ignored.
- **FR-073:** `succeeded`, `failed`, `cancelled`, and `expired` are immutable. The only exit from `timed_out` is one late final result from the agent, which moves it to `succeeded` or `failed`.
- **FR-074:** The server must record a timestamp for each transition. The command API must expose state, step, progress, message, origin, and the per-state timestamps.

**Agent reporting**

- **FR-075:** A new agent must send an acknowledgement for a command after receiving it and before it starts executing.
- **FR-076:** A new agent must send a progress report at each step change. The `docker_update` steps are: `precheck`, `resolve`, `pull`, `stop_dependents`, `recreate`, `verify`, `restart_dependents`, `rollback`, `cleanup`. The `verify` step is reported as state `verifying`.
- **FR-077:** A `failed` result must carry the failing step and the last 8 KiB, at most, of stderr or error text.
- **FR-078:** Progress may include a whole-number percent from 0 to 100. The server must drop an out-of-range percent and still apply the state change.
- **FR-079:** The server must broadcast every accepted transition to dashboard clients with command ID, device, target, state, step, progress, and message. Rejected transitions must not be broadcast.

**Compatibility**

- **FR-080:** The server must accept a legacy agent's final `success` or `error` as `succeeded` or `failed` from any non-terminal state, and from `timed_out` per FR-073.
- **FR-081:** A new agent's final result must keep the legacy status vocabulary (`success` / `error`) and add extra fields (step, outcome, stderr). The new server derives the canonical state from these.
- **FR-082:** A new agent connected to a legacy server must complete commands and deliver final results. Legacy servers ignore ack and progress messages; HTTP 404 is handled per FR-022.
- **FR-083:** Legacy stored values must map to canonical states: `pending` → `queued`, `sent` → `dispatched`, `success` → `succeeded`, `error` → `failed`, `queued` → `queued`. The API must return canonical values, and server logic must treat the two forms the same.
- **FR-084:** A legacy `pending` or `queued` row with no `expires_at` must be treated as expiring at `created_at` + the TTL for its action class.
- **FR-085:** `dispatched` may return to `queued` only when the server's own synchronous send failed and the agent has not reported anything for the command.

**Cancellation**

- **FR-086:** The operator must be able to cancel a `queued` command, which moves it to `cancelled`. A cancelled command must never be delivered.
- **FR-087:** A cancel request for a command in any other state must return 409, leave the state unchanged, and state that a command cannot be cancelled after it has been dispatched.
- **FR-088:** Cancel and delivery claim must be mutually exclusive: exactly one succeeds.

**Dashboard: ContainerDetail and ComposeStackSection**

- **FR-089:** Starting an action must return the command ID and its initial state. The component must then track that command by ID until it is terminal.
- **FR-090:** While the command is non-terminal, the component must show these labels:

  | State | Label |
  |---|---|
  | `queued` | "Queued" |
  | `dispatched` | "Sent to agent" |
  | `acknowledged` | "Starting" |
  | `running` | "Updating… <step label>" |
  | `verifying` | "Verifying" |

- **FR-091:** Update controls for the target must be disabled while its tracked command is non-terminal.
- **FR-092:** When the component mounts, it must fetch the non-terminal commands for its target and track them.
- **FR-093:** On a 409 conflict, the component must attach to the existing command ID that the server returned.
- **FR-094:** While a tracked command for a target is non-terminal, the dashboard must not show `exited` for that target's containers because of stop or die events. It must show the "Updating…" state instead.
- **FR-095:** Terminal states must be shown as follows:

  | State | Display |
  |---|---|
  | `succeeded` | "Updated" for 5 s, then the normal view |
  | `failed` | "Failed at <step>: <message>", until dismissed or a new action starts |
  | `timed_out` | "Timed out: outcome unknown" |
  | `expired` | "Expired: never delivered" |
  | `cancelled` | "Cancelled" |

- **FR-096:** No fixed timer may end tracking of a non-terminal command.
- **FR-097:** Wherever command history is displayed, legacy rows must show canonical labels.
- **FR-098:** A Cancel control must be shown only while the tracked command is `queued`.
- **FR-099:** If no push update arrives for 5 s while the command is non-terminal, the component must re-fetch the command state, and keep re-fetching at least every 5 s.

## 5. Non-Functional Requirements

- **NFR-001 [Compatibility]:** All three combinations must work: old agent with new server, new agent with old server, and new agent with new server. No agent config change and no install.sh re-run may be required.
- **NFR-002 [Compatibility]:** Schema changes must be additive. The down migration must leave the data readable by the previous server release, with canonical values mapped back to legacy ones.
- **NFR-003 [Safety]:** Each command ID is claimed for delivery at most once per queued period, and a new agent executes it at most once.
- **NFR-004 [Safety]:** No code path may delete a pre-update container unless the replacement has passed verification.
- **NFR-005 [Safety]:** A command whose `expires_at` has passed, or that is `cancelled`, is never delivered by any path.
- **NFR-006 [Performance]:** An accepted transition must reach connected dashboards within 2 s at p95 on a LAN.
- **NFR-007 [Performance]:** One sweeper pass over 100,000 historical command rows must finish in 1 s or less.
- **NFR-008 [Resources]:** The agent outbox must use no more than 1 MiB on disk. The seen-ID store must hold no more than 1000 entries.
- **NFR-009 [Privilege]:** No new sudoers rules or OS privileges. Agent persistence must use a location the agent can already write.
- **NFR-010 [Correctness]:** `go test -race` (Linux CI) must be clean for the agent WS client, outbox, target lock, the server connection registry, and the claim path.
- **NFR-011 [Testability]:** TTL, sweeper, backoff, the verification window, and telemetry ordering logic must take an injected clock.
- **NFR-012 [Dependencies]:** No new Go or npm dependencies.
- **NFR-013 [Observability]:** Each of these events must write a structured `slog` entry with `command_id`, `device_id`, `from`, and `to` where they apply: rejected transition, sweeper transition, outbox drop, rollback, failed dependent restore, auto-dispatch conflict skip.
- **NFR-014 [Security]:** The HTTP result endpoint requires `X-rIOt-Key`, and a device can change only its own commands. Stderr and messages are stored as text and shown escaped.
- **NFR-015 [Scope]:** No new collector is added, so the collector whitelist does not change.

## 6. Business Rules

- **BR-001:** A container is identified for updates by **name**. Container IDs change on recreate.
- **BR-002:** Auto-update cooldown starts only on success. Failures use short, growing backoff.
- **BR-003:** A queued command past its TTL must never run, whenever the agent next appears.
- **BR-004:** `expired` means the command was guaranteed never delivered or run, so it is safe to retry. `timed_out` means it was delivered but the outcome is unknown, so it may have run.
- **BR-005:** The agent's own report outranks the server's inference. A real result after `timed_out` is recorded once.
- **BR-006:** The pre-update container is the rollback asset. It is kept until the replacement is verified.
- **BR-007:** Every dependent that was running before an update is running after it.
- **BR-008:** Each device+target has at most one in-flight command.
- **BR-009:** The live view never moves backward in time.
- **BR-010:** Cancel is offered only when it is guaranteed safe, meaning before dispatch.

## 7. Data Requirements

### 7.1 Entities

- **Command** (existing `commands` table) gains these fields:
  - `expires_at`
  - per-state timestamps: `dispatched_at`, `acknowledged_at`, `started_at`, `finished_at`
  - `step` (text) and `progress` (0–100, nullable)
  - `failure_step` and `stderr_tail` (≤ 8 KiB)
  - `origin` (`user` | `auto_update` | `auto_patch`)
  - `policy_id` (nullable)
  - `target_key` (the device+target conflict key, FR-059)

  `status` holds a canonical value. Legacy values are mapped per FR-083.
- **AutoUpdatePolicy** gains `consecutive_failures` (int, default 0) and a next-eligible time (or the data needed to compute it).
- **Agent outbox entry:** command ID, final result payload, created time, attempt count, next attempt time.
- **Agent seen-command record:** command ID and time first seen.
- **Agent in-flight marker:** command ID, action, target, last step. Used by FR-019.
- **Freshness result:**
  - `status`: `up_to_date` | `update_available` | `pinned` | `check_failed`
  - `reason`: `registry_newer` | `pulled_not_recreated`, or null
- **Telemetry broadcast** gains `collected_at` and `received_at`.

### 7.2 State transitions

Any transition not listed is disallowed (FR-072).

| From | To | Trigger | Actor |
|---|---|---|---|
| (new) | queued | Command created with `expires_at` | Server |
| queued | dispatched | Atomic claim, while not expired (WS or heartbeat) | Server |
| queued | expired | `expires_at` passed (sweeper or claim-time check) | Server |
| queued | cancelled | Operator cancel | Operator |
| queued | acknowledged / running / verifying / succeeded / failed | Agent report (proof of delivery after an FR-085 revert) | Agent |
| dispatched | queued | Synchronous server send failure, with no agent report (FR-085) | Server |
| dispatched | acknowledged | Ack | New agent |
| dispatched | running / verifying | Progress (ack lost) | New agent |
| dispatched | succeeded / failed | Final result (legacy agent, or ack lost) | Agent |
| dispatched | timed_out | Execution deadline passed | Server |
| acknowledged | running / verifying / succeeded / failed | Progress or final result | Agent |
| acknowledged | timed_out | Deadline passed | Server |
| running | running | Step or progress change | Agent |
| running | verifying / succeeded / failed | Progress or final result | Agent |
| running | timed_out | Deadline passed | Server |
| verifying | succeeded / failed | Final result | Agent |
| verifying | timed_out | Deadline passed | Server |
| timed_out | succeeded / failed | Late final result (once) | Agent |
| succeeded / failed / cancelled / expired | (any) | Disallowed: ignored or 409, and logged | n/a |

Backward moves (for example `running` → `acknowledged`, or `verifying` →
`running`) are disallowed and ignored.

### 7.3 TTL and execution deadlines (defaults)

The architect maps concrete action names to these classes. Invariant: each
execution deadline must be longer than the agent's own maximum limit for that
action plus 5 minutes.

| Action class | Queued TTL | Execution deadline (from `dispatched_at`) |
|---|---|---|
| Reboot / power | 15 min | 10 min |
| OS patch / upgrade | 30 min | Agent upgrade limit + 5 min |
| `docker_update` (container or stack) | 60 min | 35 min |
| `docker_bulk_update` | 60 min | 35 min × number of targets, capped at 6 h |
| Agent self-update | 60 min | 15 min |
| All other actions | 30 min | 15 min |

The window-end cap in FR-025 also applies to auto-dispatched commands.

### 7.4 Validation

- Progress percent is a whole number from 0 to 100. Out-of-range values are dropped (FR-078).
- Step must come from the action's step vocabulary. An unknown step is stored as-is but shown as a generic "Working".
- `stderr_tail` is truncated to the last 8 KiB.
- `container` names are compared without a leading `/`.

## 8. Acceptance Criteria

### Item 1: Auto-update targeting and retry

**AC-001 [FR-001]:**
Given a container-level policy for "web" whose current container ID is `abc`,
When auto-update dispatches,
Then the params contain `container="web"` and `container_id="abc"`.

**AC-002 [FR-002]:**
Given params `container="web"` and a stale `container_id="old1"` that no longer exists, and a running "web" with ID `new2`,
When the agent runs `docker_update`,
Then it targets `new2`.

**AC-003 [FR-002]:**
Given a container named `/web`,
When params carry `container="web"`,
Then resolution succeeds.

**AC-004 [FR-003]:**
Given no container named "web",
When `docker_update` runs,
Then the result is `failed` with step `resolve`, and the message contains "web".

**AC-005 [FR-004]:**
Given a policy-originated command,
When it ends `failed`,
Then the policy's last-triggered time is unchanged.

**AC-006 [FR-004]:**
Given a policy-originated command,
When it ends `succeeded` at time T,
Then last-triggered equals T.

**AC-007 [FR-005]:**
Given consecutive-failure counts n = 1, 2, 3, 6, 10,
When the backoff is computed,
Then it is 15 min, 30 min, 60 min, 6 h, and 6 h.

**AC-008 [FR-005]:**
Given a failure at T with n = 1 (injected clock),
When telemetry arrives at T+14 min and at T+16 min,
Then there is no dispatch at T+14 min and one dispatch at T+16 min.

**AC-009 [FR-006]:**
Given n = 3,
When the policy's next command succeeds,
Then n = 0.

**AC-010 [FR-007]:**
Given a policy whose command is `running`,
When eligible telemetry arrives,
Then no new command is created.

**AC-011 [FR-008]:**
Given one container policy and one stack policy, both with last-triggered set,
When the migration runs,
Then the container policy's last-triggered is cleared and the stack policy's is unchanged.

**AC-012 [FR-009]:**
Given backoff has elapsed but the time is outside the configured window,
When telemetry arrives,
Then nothing is dispatched.

### Item 2: Serialized agent WebSocket writes

**AC-013 [FR-010]:**
Given a fake connection that fails the test on any overlapping write, and 50 goroutines sending results and terminal output at once,
When they all send,
Then no overlap is detected and all messages are written.

**AC-014 [FR-011]:**
Given concurrent connect, disconnect, and send loops,
When the test runs under `-race` (Linux CI),
Then no race is reported.

**AC-015 [FR-012]:**
Given a connection whose write returns an error,
When a result is sent,
Then the caller receives the error, the process keeps running, and the result stays in the outbox.

### Item 3: Result durability, sweeper, TTL

**AC-016 [FR-013, FR-014]:**
Given the WS is disconnected,
When a command completes,
Then the result is persisted and then POSTed to the HTTP endpoint, and the server records it as terminal.

**AC-017 [FR-015, FR-016]:**
Given the HTTP endpoint returns 500,
When delivery is attempted,
Then the entry is kept and retried after 5 s, then 10 s; and once a 2xx arrives, the entry is removed.

**AC-018 [FR-018]:**
Given an undelivered entry,
When the agent restarts,
Then the entry is delivered after the restart.

**AC-019 [FR-017]:**
Given 200 entries,
When a 201st is added,
Then the oldest entry is dropped and a warning is logged.

**AC-020 [FR-017]:**
Given an entry created 24 h + 1 s ago (injected clock),
When the outbox is pruned,
Then the entry is dropped.

**AC-021 [FR-019]:**
Given an in-flight marker for `docker_update` at step `recreate`,
When the agent starts,
Then a `failed` result is queued with step `recreate` and the message "agent restarted during execution; outcome unknown".

**AC-022 [FR-019]:**
Given an in-flight marker for an agent self-update,
When the agent starts,
Then no "agent restarted" failure is reported.

**AC-023 [FR-020]:**
Given a `succeeded` command,
When the same final result is submitted again,
Then the response is 2xx and the row (including `updated_at`) is unchanged.

**AC-024 [FR-021]:**
Given a command belonging to device A,
When device B submits a result for it,
Then the request is rejected and the row is unchanged.

**AC-025 [FR-022]:**
Given the HTTP endpoint returns 404,
When an entry is pending,
Then the agent enters legacy-server mode and removes the entry after a successful WS write.

**AC-026 [FR-023]:**
Given a `running` `docker_update` dispatched 36 min ago,
When the sweeper runs,
Then the command is `timed_out` with the message "no result from agent within 35m".

**AC-027 [FR-023]:**
Given a `running` `docker_update` dispatched 20 min ago,
When the sweeper runs,
Then the command is unchanged.

**AC-028 [FR-024, FR-026]:**
Given a queued reboot whose `expires_at` has passed,
When the agent heartbeats,
Then the reboot is not in the response and its state is `expired`.

**AC-029 [FR-025]:**
Given each action class in §7.3,
When a command is created at time T,
Then `expires_at` = T + the class TTL (table-driven test).

**AC-030 [FR-025]:**
Given a window that ends at T+10 min,
When auto-patch dispatches at T,
Then `expires_at` = T+10 min.

**AC-031 [FR-027]:**
Given a stale `dispatched` row at server start,
When the server starts,
Then the row is `timed_out` before the first 60 s interval elapses.

### Item 4: Compose network dependents

**AC-032 [FR-028]:**
Given a dependent whose network mode is `container:<full parent ID>`,
When dependents are identified,
Then it is included.

**AC-033 [FR-028]:**
Given dependents whose network mode is `container:<name>`, `container:/<name>`, or `service:<svc>`,
When dependents are identified,
Then each is included.

**AC-034 [FR-029]:**
Given dependents of service A and of service B in the same project,
When only service A is updated,
Then only A's dependents are stopped.

**AC-035 [FR-030]:**
Given a running dependent,
When the update succeeds,
Then the dependent is running and shares the new parent's network namespace.

**AC-036 [FR-030]:**
Given a running dependent,
When the update fails at `pull`,
Then the dependent is running.

**AC-037 [FR-030, FR-032]:**
Given the compose up limit is exceeded,
When the update ends,
Then the dependents are restored within their own 2-minute budget.

**AC-038 [FR-031]:**
Given a dependent that was stopped before the update,
When the update ends,
Then it is still stopped.

**AC-039 [FR-033]:**
Given dependent "vpn-client" fails to restart,
When the update ends,
Then the result is `failed` with step `restart_dependents`, and the message names "vpn-client" and states the main update's outcome.

### Item 5: Standalone update safety

**AC-040 [FR-034]:**
Given a pull stream that contains an `errorDetail` entry,
When the update runs,
Then the result is `failed` with step `pull` and the error text in stderr, and the original container keeps the same ID and stays running.

**AC-041 [FR-035]:**
Given the pulled image ID equals the running image ID,
When the update runs,
Then no stop or create happens, and the result is `succeeded` with "already up to date".

**AC-042 [FR-036, FR-040]:**
Given an update succeeds,
When it runs,
Then the old container is renamed aside (not removed) until verification passes and is removed afterwards, and the replacement has the original name.

**AC-043 [FR-039]:**
Given container create fails,
When the update ends,
Then the old container has its original name and is running, and the result is `failed` with step `recreate` and "rolled back to previous container".

**AC-044 [FR-039]:**
Given container start fails,
When the update ends,
Then the replacement is removed and the old container is restored and running.

**AC-045 [FR-038, FR-039]:**
Given the replacement exits within 10 s,
When verification runs,
Then a rollback is performed and the result is `failed` with step `verify`.

**AC-046 [FR-037]:**
Given the old container was stopped before the update,
When the update succeeds,
Then the replacement is stopped.

**AC-047 [FR-041]:**
Given the rollback's rename fails,
When the update ends,
Then the set-aside container still exists, and the result is `failed` with step `rollback` and a message that names it.

**AC-048 [FR-042]:**
Given a leftover set-aside container for "web",
When a new update for "web" arrives,
Then the result is `failed` with step `precheck` and names it, and nothing is deleted.

### Item 6: Timeouts

**AC-049 [FR-043]:**
Given a fake runner where pull takes 14 min,
When `up` starts,
Then `up` has a full 10-minute budget.

**AC-050 [FR-044]:**
Given `up` exceeds its limit,
When the limit fires,
Then SIGTERM is sent first, and SIGKILL only after 60 s if the process is still running.

**AC-051 [FR-044]:**
Given the process exits 5 s after SIGTERM,
When the grace period runs,
Then no SIGKILL is sent.

**AC-052 [FR-045]:**
Given the pull limit fires,
When the command ends,
Then the result is `failed` with step `pull` and "timed out after 15m".

**AC-053 [FR-046]:**
Given the operation context has expired,
When the update ends,
Then the `update_failed` event and the final result still reach the fake transport.

### Item 7: Update detection

**AC-054 [FR-047, FR-048]:**
Given a running image X, a tag that resolves locally to Y, and a registry digest equal to Y's,
When the check runs,
Then the status is `update_available` with reason `pulled_not_recreated`.

**AC-055 [FR-047, FR-048]:**
Given the registry digest differs from the local tag digest,
When the check runs,
Then the status is `update_available` with reason `registry_newer`.

**AC-056 [FR-047]:**
Given all digests and IDs match,
When the check runs,
Then the status is `up_to_date`.

**AC-057 [FR-049]:**
Given two containers on `nginx:latest`, one recreated and one not,
When checks run,
Then their verdicts differ and exactly one registry lookup is made.

**AC-058 [FR-050]:**
Given an image reference `sha256:abcd…` and one containing `@sha256:`,
When checks run,
Then no registry call is made and the status is `pinned`.

**AC-059 [FR-051]:**
Given a registry error,
When the check runs,
Then the status is `check_failed`, not `up_to_date`.

**AC-060 [FR-051]:**
Given a failed check at T (injected clock),
When checks run at T+4 min and T+6 min,
Then T+4 min uses the cache and T+6 min calls the registry again.

**AC-061 [FR-052]:**
Given cache entries for `nginx:latest` and `redis:7`,
When an update of `nginx:latest` completes,
Then the nginx entry is invalidated and the redis entry remains, both in memory and on disk.

**AC-062 [FR-053]:**
Given `check_failed` or `pinned`, and separately `update_available` with reason `pulled_not_recreated`,
When the dashboard renders,
Then there is no update-available indicator for the first case, and the indicator's `title` states the reason for the second.

### Item 8: Concurrency

**AC-063 [FR-054, FR-055]:**
Given an update of "web" is in progress,
When a second update of "web" arrives,
Then it ends `failed` with step `precheck` and "already in progress", makes no Docker calls, and leaves the first update unaffected.

**AC-064 [FR-054]:**
Given a single-service update in project P is in progress,
When a full update of stack P arrives,
Then it is rejected.

**AC-065 [FR-054]:**
Given updates for "web" and "db" (different targets),
When both arrive together,
Then both run.

**AC-066 [FR-056]:**
Given an update that fails or panics,
When the next update of the same target arrives,
Then it acquires the lock.

**AC-067 [FR-057]:**
Given the same command ID is delivered by WS and by heartbeat,
When the agent processes both,
Then the command runs once.

**AC-068 [FR-058]:**
Given "web" has a non-terminal command,
When a new update of "web" is POSTed,
Then the response is 409 with the existing ID and no row is created.

**AC-069 [FR-058]:**
Given the previous command for "web" is terminal,
When a new one is POSTed,
Then the response is 2xx.

**AC-070 [FR-059]:**
Given a `docker_update` in flight,
When a reboot is POSTed for the same device,
Then it is accepted. And when a second reboot is POSTed while the first is non-terminal, it gets 409.

**AC-071 [FR-060]:**
Given a bulk request for "web" (in flight) and "db",
When it is submitted,
Then "web" is reported as `conflict` with the existing ID, and "db" is created.

**AC-072 [FR-061]:**
Given an auto-update hits a conflict,
When it is evaluated,
Then there is no new row, a log entry is written, and n is unchanged.

**AC-073 [FR-062]:**
Given one queued command,
When two claims race,
Then exactly one succeeds.

**AC-074 [FR-062]:**
Given a command already claimed via WS,
When a heartbeat arrives,
Then the heartbeat response does not include it.

**AC-075 [FR-063]:**
Given each of the four dispatch paths,
When each creates a command (table-driven),
Then each sets `expires_at`, enforces 409/skip on conflict, and delivers only via claim.

**AC-076 [FR-064]:**
Given connection A is registered and connection B then registers for the same device,
When A's handler exits,
Then B is still registered and the device reports as connected.

**AC-077 [FR-064]:**
Given connection A is registered,
When B registers,
Then A is closed.

**AC-078 [FR-065]:**
Given the backoff has grown to 30 s,
When a connect succeeds and the connection then drops,
Then the next delay is the initial delay.

### Item 9: Live telemetry ordering

**AC-079 [FR-066]:**
Given a snapshot is ingested,
When it is broadcast,
Then the payload includes `collected_at` and `received_at`.

**AC-080 [FR-067]:**
Given a snapshot with `collected_at` = T2 has been applied,
When one with T1 < T2 arrives,
Then it is discarded and the state still reflects T2.

**AC-081 [FR-067]:**
Given T2 has been applied,
When a message with T2 arrives again,
Then it is discarded.

**AC-082 [FR-068]:**
Given the browser clock is skewed by +1 h (injected clock),
When telemetry is applied,
Then last-seen equals `received_at`.

**AC-083 [FR-069]:**
Given current state T2,
When the server ingests a snapshot from T1 < T2,
Then the current state stays T2 and T1 is stored in history.

**AC-084 [FR-070]:**
Given a snapshot with no timestamp,
When it is ingested,
Then `collected_at` = `received_at`.

### Phase 2: Command lifecycle

**AC-085 [FR-071, FR-072]:**
Given every (from, to) pair of canonical states,
When each transition is applied (table-driven),
Then the pairs listed in §7.2 are accepted, and every other pair is rejected with the state unchanged and a log entry.

**AC-086 [FR-073]:**
Given a `succeeded` command,
When a late `failed` arrives,
Then the state stays `succeeded`.

**AC-087 [FR-073]:**
Given a `timed_out` command,
When a late `success` arrives,
Then the state is `succeeded`. And when a later `error` arrives, it is ignored.

**AC-088 [FR-072]:**
Given a `running` command,
When an ack arrives,
Then the state stays `running`.

**AC-089 [FR-072]:**
Given an `expired` or `cancelled` command,
When an agent report arrives,
Then it is ignored and logged.

**AC-090 [FR-072]:**
Given a disallowed transition submitted over HTTP,
When it is processed,
Then the response is 409.

**AC-091 [FR-074]:**
Given a command has passed through dispatched, acknowledged, running, and succeeded,
When it is fetched by ID,
Then each of those timestamps is present.

**AC-092 [FR-075]:**
Given a new agent receives a command,
When the command executes,
Then the ack is written before the first execution step.

**AC-093 [FR-076]:**
Given a successful compose update,
When it runs,
Then progress steps arrive in this order: `precheck`, `pull`, `recreate`, `verifying`, and finally `succeeded`.

**AC-094 [FR-077]:**
Given stderr of 20 KiB,
When the command fails,
Then the stored `stderr_tail` is the last 8 KiB, and `failure_step` is set.

**AC-095 [FR-078]:**
Given a progress report with percent 150,
When it is processed,
Then the state is applied and the stored progress is null.

**AC-096 [FR-079]:**
Given one accepted and one rejected transition,
When both are processed,
Then exactly one broadcast is sent, with ID, device, target, state, step, progress, and message.

**AC-097 [FR-080]:**
Given a `dispatched` command,
When a legacy `success` result arrives (no ack or progress),
Then the state is `succeeded`.

**AC-098 [FR-080]:**
Given a `dispatched` command,
When a legacy `error` result arrives,
Then the state is `failed`.

**AC-099 [FR-081]:**
Given a new agent's final result,
When it is serialized,
Then `status` is `success` or `error`, and step and outcome fields are present.

**AC-100 [FR-082]:**
Given a fake legacy server that ignores ack and progress and returns 404 for HTTP results,
When a command runs on a new agent,
Then it completes and the final result is delivered over WS.

**AC-101 [FR-083]:**
Given rows with `pending`, `sent`, `success`, `error`, and `queued`,
When they are read via the API,
Then they return `queued`, `dispatched`, `succeeded`, `failed`, and `queued`.

**AC-102 [FR-083]:**
Given a legacy `success` row,
When server logic evaluates the command's outcome,
Then it treats the row as `succeeded`.

**AC-103 [FR-084]:**
Given a legacy `queued` reboot created 2 days ago with no `expires_at`,
When the sweeper runs,
Then the reboot is `expired`. And when the agent then heartbeats, the reboot is not delivered.

**AC-104 [FR-085]:**
Given a `dispatched` command whose server send failed,
When the dispatcher handles the failure,
Then the command is `queued`. And given an agent report has already arrived, the revert is rejected.

**AC-105 [FR-086]:**
Given a `queued` command,
When the operator cancels it,
Then it is `cancelled`. And when the agent later heartbeats, the command is not delivered.

**AC-106 [FR-087]:**
Given a `dispatched` command,
When the operator cancels it,
Then the response is 409 and the state is unchanged.

**AC-107 [FR-088]:**
Given a cancel and a claim racing on one `queued` command,
When both run,
Then exactly one succeeds. If the claim wins, the cancel gets 409.

**AC-108 [FR-089, FR-090, FR-096]:**
Given a command for ContainerDetail and, separately, ComposeStackSection,
When Update is clicked and states arrive over 30 s,
Then the label follows each state and step, and it is still tracking after 5 s.

**AC-109 [FR-091]:**
Given a tracked command is non-terminal,
When the component renders,
Then the update control is disabled.

**AC-110 [FR-092]:**
Given an in-flight command for the target,
When the component remounts,
Then it shows "Updating…" with the current step.

**AC-111 [FR-093]:**
Given a POST that returns 409 with the existing ID,
When the component handles it,
Then it tracks the existing ID.

**AC-112 [FR-094]:**
Given a tracked non-terminal update,
When stop or die events arrive for the target's containers,
Then `exited` is not shown and "Updating…" is shown.

**AC-113 [FR-094]:**
Given no tracked command,
When a stop event arrives,
Then `exited` is shown as today.

**AC-114 [FR-095]:**
Given each terminal state,
When the component renders (table-driven),
Then it shows the label from FR-095. For `succeeded`, the normal view returns after 5 s (injected timer).

**AC-115 [FR-097]:**
Given legacy history rows,
When they are shown,
Then they use the canonical labels.

**AC-116 [FR-098]:**
Given a `queued` tracked command, then the same command after it becomes `dispatched`,
When each renders,
Then Cancel is visible while `queued` and hidden once `dispatched`.

**AC-117 [FR-099]:**
Given a non-terminal command with no push update for 5 s (injected timer),
When the timer elapses,
Then the command state is re-fetched.

## 9. Out of Scope

- Cancelling `dispatched` or `running` commands, or interrupting an agent-side execution.
- Rollback for compose stacks. FR-030 still applies to compose: dependents are always restored.
- Verification based on container health checks (OQ-004).
- Making timeouts, TTLs, or backoff configurable by the user, in the UI or in agent YAML.
- New collectors, changes to the collector whitelist, or install.sh changes.
- Redesigning the UI outside ContainerDetail, ComposeStackSection, and command-history status labels.
- Cleaning up old images after an update (existing behavior stays).
- Releasing, tagging, or pushing.

## 10. Assumptions and Decisions

| ID | Decision | Rationale |
|---|---|---|
| D-001 | `expired` is a separate state from `timed_out` | They mean different things to the operator: expired was guaranteed never to run and is safe to retry; timed_out has an unknown outcome and may have run (BR-004). |
| D-002 | Cancel ships, but only for `queued` commands, with a UI control | It covers the "stale reboot fires days later" fear with no agent-side risk. Interrupting a compose update mid-recreate is unsafe, so that is out of scope. |
| D-003 | An agent-side step limit is reported as `failed` (reason timeout), not `timed_out` | The agent knows what happened. `timed_out` is reserved for "server heard nothing". |
| D-004 | `timed_out` can accept one late final result | The agent's report is the truth (BR-005). Without this, slow WS or outbox delivery would leave a false failure. |
| D-005 | TTL and deadline values as in §7.3 | Reboots must go stale quickly. Docker updates allow for slow pulls. Deadlines exceed the agent's own limits so the server never declares a timeout early. |
| D-006 | Backoff of 15 min × 2^(n−1), capped at 6 h | "Short, growing", per the story. A failing image is still retried several times a day. |
| D-007 | One-time reset of non-stack last-triggered (FR-008) | Because of defect 1, every per-container auto-update has failed, so all existing non-stack last-triggered values come from failures. |
| D-008 | Server sends both name and ID (FR-001) | Legacy agents need `container_id`. New agents prefer the name. |
| D-009 | Final results keep `success`/`error` plus extra fields (FR-081) | Legacy servers store status text as-is. New values would show up there as unknown statuses. |
| D-010 | The sweeper also handles historical legacy `sent` rows, which become `timed_out` | That is the honest label for "no result ever arrived". (OQ-005) |
| D-011 | Failure to restore a dependent fails the command (FR-033) | A stopped dependent (often a VPN or sidecar) is an outage, and reporting "succeeded" would hide it. (OQ-006) |
| D-012 | Verification means "running 10 s after start". Health checks are not evaluated. | This keeps scope bounded and catches crash-on-start, the common failure. (OQ-004) |
| D-013 | Docker targets and the host target do not conflict (FR-059) | A stuck Docker command would otherwise block a reboot until the deadline passes. (OQ-001) |
| D-014 | The agent lock key for compose is the project | A single-service update and a full-stack update both run against the same project. |
| D-015 | Bulk updates skip conflicting targets instead of failing the whole batch | One stuck target should not block a fleet-wide update. |
| D-016 | Only final results are durable. Ack and progress are best-effort. | Final results decide outcomes. Progress loss is cosmetic and is recovered by the next report or the re-fetch (FR-099). |
| D-017 | The agent de-duplicates by command ID (FR-057) | This makes the FR-085 revert and redelivery safe on new agents. |
| D-018 | Disallowed transitions are ignored over WS and get 409 over HTTP | WS has no request/response channel. HTTP callers (the outbox) need a definite answer. |
| D-019 | One FRD for the whole epic, with slicing guidance in §2 | The orchestrator asked for a single document. The Phase 1 items stay independently traceable. |
| D-020 | A standalone update with no new image skips the recreate (FR-035) | It avoids pointless downtime and fits the ImageID-based detection. |

## 11. Open Questions (for the product owner; none block implementation)

- **OQ-001:** Should a reboot or OS patch be blocked while a Docker update is in flight on the same device? (Default: not blocked. See D-013.)
- **OQ-002:** Are the §7.3 TTLs acceptable? In particular, is 15 min right for a queued reboot?
- **OQ-003:** The FR-008 reset may cause a burst of per-container auto-updates on the first telemetry after deploy, limited by stagger and the window. Is that acceptable?
- **OQ-004:** Should a follow-up story add health-check-gated verification and rollback?
- **OQ-005:** Historical `sent` rows will be relabelled `timed_out` by the first sweep. Is that acceptable, or should rows older than some cutoff be left as they are?
- **OQ-006:** If dependents fail to restore after a successful main update, should the result be `failed` (default) or "succeeded with warning"?

## 12. Dependencies

- **Server:**
  - `internal/server/handlers/commands.go` (SendCommand, BulkDockerUpdate)
  - `auto_update.go` (dispatchAutoUpdate, checkAutoPatch, policy cooldown)
  - `handlers.go` (heartbeat delivery, telemetry broadcast)
  - `terminal.go` (agent WS handler, `command_result`, connection registry)
  - command repository and interfaces (`internal/server/db/interfaces.go`) and mocks (`internal/testutil/mocks.go`)
  - the WS hub
  - a new migration after `000022`
- **Agent:**
  - `internal/agent/ws_client.go`, `commands.go`, `heartbeat.go`, `docker_update.go`, `terminal.go`, `host_terminal.go`
  - `collectors/docker.go` (freshness cache)
  - the existing SIGTERM + WaitDelay pattern in `runUpgradeCommand`
- **Web:**
  - `web/src/hooks/useDevices.ts`
  - `ContainerDetail.tsx`
  - `ComposeStackSection.tsx`
  - the API client
  - any command-history view
- **New contracts (to be defined by the architect):**
  - agent HTTP result endpoint
  - WS ack, progress, and receipt messages
  - command-by-ID and in-flight-by-target reads
  - cancel endpoint
  - 409 conflict body
- **Pipeline:** the architect's ADD and the security-researcher review (for the new agent endpoint and the stderr storage) come before engineering.
