# Architecture Decision Document

**Story ID:** CMD-LIFECYCLE-001
**FRD Reference:** docs/requirements/CMD-LIFECYCLE-001-frd.md
**Security Review:** docs/security/CMD-LIFECYCLE-001-security-review.md (APPROVED WITH CONDITIONS; conditions folded in, see §15)
**Author:** Architect Agent
**Date:** 2026-10-17
**Status:** FINAL (rev 2: security addendum)

**Detected stack:** Go 1.24 (chi v5, pgx v5, golang-migrate v4, gorilla/websocket,
Docker SDK v28.3.3, modernc sqlite in the agent buffer, testify), PostgreSQL 16,
React 19 + TypeScript + Vite + Tailwind, vitest 4, Testing Library 16, and
TanStack Query 5. **This story adds no Go or npm dependency (NFR-012).**

**How to read rev 2.** Text that a security condition introduced or changed is
tagged **[SEC C-n]** where it appears in the AD sections. §15 is the
consolidated index of conditions: where each one lands and which tests cover
it.

---

## 0. FRD review outcome

The FRD's business decisions (D-001 to D-020) stand. The Stage-1 review found no
blockers and nine technical contradictions or gaps. Each one is resolved in this
ADD and listed in §14. The three that matter most:

1. **The §7.3 invariant conflicts with the mandated limits.** Pull 15m + 60s
   grace, up 10m + 60s grace, and the 2m dependent restore together leave less
   than 1m before a 35m deadline is hit. AD-013 makes that budget explicit and
   enforced. Bulk gets an extra 5m, because the formula as written cannot meet
   the invariant at N=1.
2. **FR-020 conflicts with FR-072/AC-090** for a final result that arrives on an
   immutable row. AD-008 resolves it: the same outcome returns 200 "duplicate",
   a different outcome returns 409 `already_terminal`, and the agent removes the
   entry in both cases.
3. **"Four dispatch paths" is really seven.** The code has seven
   command-creation sites: SendCommand, BulkDockerUpdate, dispatchAutoUpdate,
   checkAutoPatch, RunDeviceProbe, BulkUpdateAgents, and BulkPatchDevices.
   FR-025 requires `expires_at` on every command, so all seven go through the
   new dispatcher (AD-002).

**Pre-existing security defect.** `handleCommandResult` (`terminal.go:169`)
applies a `command_result` from any authenticated agent socket to any command
ID. AD-009 fixes it under FR-021/NFR-014.

## 1. Summary

A new server-side command service, `internal/server/commands`, replaces the
seven copy-pasted create/send/queue blocks. It owns:

- creation and the target conflict check (409);
- TTL and deadline stamping;
- the atomic delivery claim;
- guarded state transitions;
- agent report ingestion, with validation, sanitization, coalescing, and a
  per-device rate limit;
- cancellation;
- a sweeper that runs at startup and then every 60s.

Every status write goes through one pure, table-driven state machine in
`internal/models`. The database enforces it with
`UPDATE … WHERE status IN (allowed)`.

The agent gains:

- a serialized WebSocket writer;
- a bounded, file-backed outbox for final results, with an HTTP fallback and a
  legacy-server mode;
- a persisted seen-ID store and crash-safe in-flight markers;
- startup reconciliation of interrupted Docker updates;
- per-target locks;
- explicit step limits with a SIGTERM grace period;
- name-based target resolution;
- rename-aside rollback for standalone containers;
- guaranteed restore of network dependents;
- per-container image freshness verdicts.

The dashboard tracks each command by ID, using a new `command_update` WS
message plus a 5s polling fallback. It drops stale telemetry by comparing the
server-supplied, clamped `collected_at` and `received_at`.

## 2. Technical Context

Line numbers refer to base `main@a7b9f9e`.

**Server command pipeline**

- **`commands` table** (`000001_init.up.sql:131`, plus `duration_ms` and
  `exit_code` added in `000017`). Statuses are
  `pending|queued|sent|success|error`. There is no TTL, deadline, step, origin,
  or target.
- **`db.CommandRepository`** (`interfaces.go:119`) has unconditional
  `UpdateStatus` and `UpdateCommandResult`. `ListPending` selects
  `pending|queued` with LIMIT 10, then marks each row `sent` with no guard.
- **Duplicated dispatch blocks** at `commands.go:32` and `:111`,
  `auto_update.go:438` and `:496`, `device_probes.go:187`, and
  `fleet.go:101` and `:181`. `DeleteDevice` (`handlers.go:705`) sends a
  row-less `agent_uninstall`; that behavior is kept.
- **Heartbeat delivery:** `handlers.go:306-321`.
- **`terminal.go`:**
  - The `agentConnections` global's defer at `:95` deletes the entry
    unconditionally (defect 8).
  - `handleCommandResult` at `:169` is unscoped.
  - `terminal_output` and `terminal_close` at `:142-160` are not scoped to a
    device (SEC-014).
- **`websocket.Hub`** has a 256-slot broadcast channel. Its envelope is
  `{type, device_id, data}`, and `telemetry` and `heartbeat` messages carry no
  timestamps.
- **`server.Start`** (`server.go:74`) starts the workers. `setupRouter()` calls
  `handlers.New` every time, including from `applyTLSAndRestart`, so long-lived
  services must be built in `Start`. `logstore.NewDBHandler(…, LevelWarn)`
  persists every Warn+ log record, with its attributes, to `server_logs`. That
  is why rejection logs must be throttled (SEC-001).
- **`inMaintenanceWindow`** (`auto_update.go:186`) calls `time.Now()`
  internally. `SetLastTriggered` runs at dispatch time, and the default Docker
  cooldown is 7 days.
- **`events.Generator.CommandCompleted`** expects legacy `success`/`error` and
  truncates the reason with a byte slice, `reason[:200]`, which can split a
  UTF-8 rune (SEC-002).
- **`middleware.RateLimiter`** is keyed by IP and uses the wall clock. It is
  not reused here; see AD-020.

**Agent**

- **`ws_client.go`:**
  - `send()` has no mutex and returns nil when there is no connection;
  - `c.conn` is unsynchronized;
  - backoff never resets;
  - `terminal.go` and `host_terminal*.go` receive the raw `*websocket.Conn`.
- **`a.wsClient` race.** `a.wsClient` is assigned in `Run` after
  `heartbeatLoop` has already started, so the two race.
- **`agent.go` shutdown.** On SIGTERM, `agent.go:250-258` cancels and
  `wg.Wait()`s only the loops. Command goroutines are abandoned (SEC-004).
  install.sh sets no `TimeoutStopSec`, so systemd's default of 90s applies.
- **`commands.go:72` `handleCommand`** reports results only over WS. A
  `runner` seam exists. `runUpgradeCommand` (`:61`) uses SIGTERM plus
  `WaitDelay = 90s`.
- **`docker_update.go`:**
  - one 10m context covers both pull and up;
  - pull output is discarded;
  - stop → remove → create has no rollback;
  - dependents are never restarted;
  - lifecycle events are sent on an already-expired context;
  - `sanitizeComposeFile` writes `<workDir>/.compose.riot-sanitized.yml` with
    `os.WriteFile(…, 0644)`. It follows symlinks and its path is predictable
    (SEC-005).
- **`collectors/docker.go:428-549`** caches per-ref verdicts for 30m, failures
  included, and compares only the local tag.
- **Persistence:** `/var/lib/riot/` (or `%PROGRAMDATA%\riot\`), which the agent
  can already write (NFR-009).

**Web**

- `useDevices.ts:57-139` merges every telemetry message, stamps
  `new Date()`, and maps stop/die events to `exited`.
- `ContainerDetail.tsx` and `ComposeStackSection.tsx` use
  `useMutation.isPending` plus a 5s reset.
- `ActivityLog.tsx` shows legacy statuses.

**Tests and CI**

- `MockCommandRepo` is at `internal/testutil/mocks.go:767`.
- CI runs `go test -race ./...` on PRs, with no Postgres service.

**Parallel branches**

- The unmerged branches `fix/pg-autovacuum-bloat` (000023) and
  `part-001-partitioning` (000024-000028) also claim migration numbers (§16).

## 3. Architecture Decisions

### AD-001: Canonical state machine is a pure table in `internal/models`

**Decision:** Create `internal/models/command_state.go` containing:

- `type CommandState string` with the 10 canonical constants (FR-071), plus two
  typed legacy alias constants, `LegacyPending` and `LegacySent`.
- `type CommandActor int`: `ActorServer`, `ActorAgent`, `ActorOperator`.
- An unexported transition table, `map[from]map[to]actorSet`. It encodes §7.2
  exactly: `running → running` is allowed, the agent edges out of `queued` are
  allowed, and `verifying → verifying` is **not** allowed.
- `CanTransition(from, to, actor) bool`.
- `AllowedFrom(to, actor) []CommandState`. It returns canonical states plus
  their legacy aliases.
- `CanonicalStatus(stored string) CommandState`. It maps legacy values per
  FR-083. An unknown value maps to `failed`, with one Warn per distinct value.
- `LegacyStatus(CommandState) string`:

  | Canonical | Legacy |
  |---|---|
  | succeeded | success |
  | failed, timed_out, cancelled, expired | error |
  | queued | queued |
  | dispatched, acknowledged, running, verifying | sent |

- `IsTerminal` (succeeded, failed, cancelled, expired, timed_out),
  `IsImmutable` (succeeded, failed, cancelled, expired), and `IsActive`.
- **[SEC C-6]** `SQLStatusList(states ...CommandState) string` builds a quoted
  SQL literal list. It accepts **only** the typed constants. Passing a
  `string` requires an explicit conversion, and the function panics on any
  value that is not one of the 12 known constants. It must never be called
  with request data. Request filters use `ParseStatusFilter` (AD-003) and bind
  parameters.

**Rationale:** One pure function is used by every write path, and AC-085 can
test it over all 10×10 pairs. The database guard makes each transition atomic.

**Alternatives considered:** A DB trigger was rejected: it is hard to test
without PG and splits the logic across two languages. A separate package was
rejected because `models` is already shared by agent and server.

**Consequences:** `timed_out` is terminal for conflicts, FR-007, and the UI, but
it is not immutable (D-004).

### AD-002: A single `commands.Service` owns all command writes

**Decision:** Create package `internal/server/commands` with a `Service`. It is
constructed once in `server.Start` and injected through
`handlers.HandlerDeps.Commands`. If that field is nil, `handlers.New` builds a
default service; existing tests rely on this.

- All seven creation sites call `Service.Dispatch`.
- Every status change goes through `Service.transition`, which calls
  `CommandRepository.Transition`.
- `UpdateStatus`, `UpdateCommandResult`, and `ListPending` are **removed**.

**Dependencies, all narrow interfaces:**

| Interface | Methods / purpose |
|---|---|
| `db.CommandRepository` | Command persistence |
| `db.AutoUpdateRepository` | Policy outcome recording |
| `TelemetryReader` | `GetLatestSnapshot` |
| `AgentSender` | `SendCommand(deviceID, models.CommandPayload) (sent bool, err error)` |
| `Broadcaster` | `BroadcastCommandUpdate(models.CommandUpdate)` |
| `CompletionEvents` | `CommandCompleted` |

The service also takes `Now func() time.Time` and `*slog.Logger`, and owns the
report limiter, the progress gate, and the log throttle (AD-020).

**Rationale:** FR-063/AC-075 require uniform behavior. Removing the unguarded
methods turns an unguarded write into a compile error. Construction in `Start`
survives the router rebuild.

**Consequences:** Never assign a nil `*websocket.Hub` or `*events.Generator` to
an interface (the typed-nil trap). Pass a nil interface instead, and nil-check
before use.

### AD-003: Atomic claim, guarded transitions, and the conflict check under an advisory lock

**Decision.** These are repository semantics in
`internal/server/db/command_repo.go`.

- **`ClaimQueued(ctx, deviceID, now, limit)`** (heartbeat path). One statement:
  - Sets `status='dispatched'`, `dispatched_at=now`,
    `deadline_at = now + exec_deadline_s seconds`, and `updated_at=now`.
  - Applies to `id IN (SELECT id … WHERE device_id=$1 AND status IN
    ('queued','pending') AND expires_at > $now ORDER BY created_at LIMIT $n
    FOR UPDATE SKIP LOCKED)`.
  - The outer `WHERE` repeats `status IN ('queued','pending') AND expires_at >
    $now`.
  - Uses `RETURNING`; the caller sorts the result by `created_at`.
- **`ClaimByID(ctx, id, now)`**: the same guard, for one ID.
- **`Transition(ctx, models.CommandTransition)`**: one
  `UPDATE … WHERE id=$id [AND device_id=$dev] AND status IN (<AllowedFrom>)
  RETURNING <cols>`. Timestamp rules:

  | Transition | Effect |
  |---|---|
  | → acknowledged | `acknowledged_at = now` |
  | → running or verifying | `started_at = COALESCE(started_at, now)` |
  | → verifying | `verifying_at = now` |
  | any terminal | `finished_at = now` |
  | → queued (revert) | Clears `dispatched_at` and `deadline_at`. Sets `send_ambiguous = true` [SEC-016] |
  | agent edge out of `queued` | COALESCE-fills `dispatched_at` and `deadline_at` |

  Progress never moves `deadline_at`.
- **`CreateIfNoConflict(ctx, cmd)`**: one transaction.
  1. `pg_advisory_xact_lock(hashtextextended('riot.cmd.' || device_id, 0))`.
  2. If `target_keys` is non-empty, look for the oldest row where
     `device_id=$1 AND status IN (<active literals>) AND target_keys && $keys`.
  3. If one is found, return it as the conflict. Otherwise `INSERT`.
- **Status lists that must use a partial index are `models.SQLStatusList(...)`
  literals built from typed constants.**
- **[SEC C-6] Request-supplied filters only ever reach SQL as bind
  parameters.**
  - `handlers.parseStatusFilter` validates each comma-separated value against
    the canonical-plus-legacy allowlist (`models.ParseStatusFilterValue`) and
    alias-expands it.
  - An unknown value returns **400** with `code=invalid_filter`.
  - The expanded `[]string` is bound as `status = ANY($n::text[])`.
  - `target_key` is bound as `$n = ANY(target_keys)`, after a length check
    (≤ 512 bytes, otherwise 400 `invalid_filter`).
  - SQL text for the filtered and active list queries is built by pure
    functions, `buildFilteredQuery` and `buildActiveQuery`, so tests can assert
    that the SQL contains only placeholders.

**Rationale:** In PG, a single-row guarded UPDATE serializes on the row lock.
Under READ COMMITTED the losing transaction re-evaluates the WHERE clause and
gets 0 rows, so claim/claim, cancel/claim, and expire/claim each have exactly
one winner. Multi-key bulk conflicts cannot use a partial unique index, so a
per-device advisory transaction lock closes the check-then-insert race.

**Alternatives considered:** SERIALIZABLE retries, and a partial unique index on
`(device_id, target_key)`. Both were rejected.

**Consequences:** `MockCommandRepo` implements the same semantics under one
mutex. PG-gated tests verify the real SQL (AD-017).

### AD-004: Target keys

**Decision:** Freeze the following key strings, built by helpers in
`models/command_policy.go`:

| Key | Used for |
|---|---|
| `host` | reboot, shutdown, os_update, enable_auto_updates, agent_update |
| `docker:compose:<working_dir>` | A compose container or a stack. The value is the `com.docker.compose.project.working_dir` label, or the `compose_work_dir` param, verbatim with trailing `/` removed |
| `docker:container:<name>` | A standalone container (no leading `/`) |
| `docker:id:<container_id>` | Fallback, when the server cannot resolve the container from the latest telemetry |

- Only `docker_update`, `docker_bulk_update` (the union of its targets), and the
  host actions carry keys. Every other action has no target and never returns
  409.
- The server resolves a `docker_update` from its params:
  - `compose_work_dir` maps to the compose key;
  - otherwise `container`, then `container_id`, is looked up in
    `GetLatestSnapshot(device).Data.Docker.Containers`;
  - a container with compose labels maps to the compose key, and anything else
    maps to the container key.
- The agent uses the same helpers for its lock keys (AD-012).

**Rationale:** The compose project is the lock unit (D-014), and its working
directory is unambiguous on both server and agent. Because the key is frozen,
WP-WEB can compute it locally.

**Consequences:** Two single-service updates in the same project conflict. A
host action never conflicts with a Docker key on the server (D-013). On the
agent, `agent_update`/`agent_uninstall` and active Docker updates exclude each
other (AD-012, **[SEC C-4]**).

### AD-005: TTL, execution deadline, and agent limit tables in `models/command_policy.go`

**Decision:** Add `models.ActionClass(action)`, `QueuedTTL(action)`, and
`ExecDeadline(action, nTargets)`.

- `expires_at = min(now + TTL, windowEnd)`. The auto dispatchers pass
  `windowEnd` only when the window mode is `window`.
- `exec_deadline_s` is stored at creation. `deadline_at` is stamped when the
  command is claimed.

| Class | Actions | Queued TTL | Exec deadline | Agent hard max |
|---|---|---|---|---|
| power | reboot, shutdown | 15m | 10m | 1m |
| os_patch | os_update, enable_auto_updates | 30m | **40m** | 31m30s / 5m |
| docker_update | docker_update | 60m | 35m | **29m55s** |
| docker_bulk_update | docker_bulk_update | 60m | **min(35m×N + 5m, 6h)** | deadline − 5m − 30s |
| agent_update | agent_update | 60m | 15m | 9m |
| other | all other actions | 30m | 15m | 9m |

Other helpers in the same file:

- `AutoUpdateBackoff(n)` = `min(15m × 2^(n−1), 6h)`, and 0 for n ≤ 0.
- `FormatDurationShort(d)` produces strings like "35m" and "1h10m".
- **[SEC C-1/C-2]** The report bounds constants and the text sanitizers live
  here too (§15, C-1/C-2).

### AD-006: Migration 000023 maps legacy rows in place and keeps a read-time mapping

**Decision:**

- The up migration rewrites legacy statuses, backfills `expires_at` and
  `exec_deadline_s` (both become NOT NULL), and stamps deadlines on ex-`sent`
  rows. It sets `finished_at` on terminal rows. It backfills `target_keys` and
  `origin` on a best-effort basis.
- **[SEC-010]** It marks every pre-existing row `pre_lifecycle = true`. The
  column is added with `DEFAULT true`, then the default is changed to `false`,
  so the table is not rewritten.
- Code also normalizes at read time (`scanCommand` calls `CanonicalStatus`),
  and the guard lists are alias-aware.
- The down migration first expires overdue queued rows.
  **[SEC-009, adopted]** It also fails **every** queued `reboot` and
  `shutdown`, and every queued `os_update` with `include_reboot_class`,
  regardless of expiry. It then maps canonical values back to legacy ones
  (`cancelled` and `expired` become `error`, never `queued`) and drops the new
  columns and indexes.

**Consequences:**

- The first sweep relabels historical rows **silently** (§15, SEC-010).
- The FR-008 reset of `last_triggered_at` cannot be undone.

### AD-007: Auto-update cooldown starts on success; failures use backoff

**Decision:** Dispatch no longer touches the policy. On any accepted transition
to a terminal state for a command that has a `policy_id`, the service calls one
of:

| Outcome | Call | Effect |
|---|---|---|
| succeeded | `RecordSuccess(id, at)` | `last_triggered_at = at`, `consecutive_failures = 0`, `last_failure_at = NULL` |
| failed, timed_out, expired, cancelled | `RecordFailure(id, at)` | `consecutive_failures += 1`, `last_failure_at = at` |

- `checkAutoUpdates` adds a backoff gate after the existing window and stagger
  gates (FR-009): skip when `n > 0` and
  `now < last_failure_at + AutoUpdateBackoff(n)`.
- FR-007 is covered by the target conflict check. A conflict is skipped and
  logged, and `n` does not change (FR-061).
- `SetLastTriggered` is removed.
- Auto-patch keeps its existing cooldown rule, and also gets the host conflict
  check and `origin=auto_patch`.

### AD-008: Agent report semantics (one rule for WS and HTTP)

**Decision:** `Service.HandleAgentReport(ctx, deviceID, report, via)` returns
one of `accepted | duplicate | already_terminal | invalid_transition |
not_found | coalesced`. Every report is scoped to `device_id` in SQL.

| Kind | Target state | Allowed from (agent) |
|---|---|---|
| `ack` | acknowledged | queued, dispatched |
| `progress`, state `running` | running | queued, dispatched, acknowledged, running |
| `progress`, state `verifying` | verifying | queued, dispatched, acknowledged, running |
| `result` | `outcome` if it is `succeeded` or `failed`. Otherwise status `success` → succeeded, `error` → failed, anything else → failed (Warn) | queued, dispatched, acknowledged, running, verifying, timed_out |

**Steps before the guarded update [SEC C-1, C-2]:**

1. Rate-limit by the authenticated device (AD-020).
2. Validate that `command_id` (path and body) is a canonical lowercase UUID.
   A malformed ID is rejected before any query, and no log records the raw
   value.
3. Run `sanitizeReport`:
   - Every string is made NUL-free and valid UTF-8.
   - `step` and `failure_step` must match `^[a-z0-9_]{1,64}$`. Otherwise they
     are stored as `""` and the transition still applies; this one rule is
     used on both paths.
   - The progress `message` is cut to ≤ 1 KiB at a rune boundary.
   - `reason` must be in the enum, otherwise it becomes `""`.
   - `output` is cut to ≤ 256 KiB, head and tail.
   - `stderr_tail` is cut to ≤ 8 KiB, tail.
   - `result_msg` is cut to ≤ 64 KiB, tail.
   - A report whose only problem is its content is stored, not rejected
     (SEC-002).
4. **Progress coalescing:**
   - A `running → running` report that changes neither `step` nor `progress`
     does not write or broadcast. It returns `coalesced`.
   - Accepted `running → running` writes are gated to at most one per command
     per second by the in-memory progress gate. Excess reports return
     `coalesced`, silently.
   - State changes (to running from an earlier state, to verifying) and results
     are never gated.

**When the guarded update matches 0 rows:**

| Situation | Outcome |
|---|---|
| Row missing, or belongs to another device (device-scoped read) | `not_found` |
| `result` on an immutable row with the same outcome | `duplicate` (no write) |
| `result` on an immutable row with a different outcome | `already_terminal` |
| Anything else | `invalid_transition` |

- Rejections log the NFR-013 Warn `command transition rejected` through the
  per-device **log throttle** (AD-020), with validated fields only.
- **[SEC-016]** A late report on a row where `send_ambiguous = true` logs Warn
  `agent reported on command after ambiguous send`, with `command_id`, `from`,
  and `to`. This log is exempt from the throttle, because only the server can
  set that flag.
- `Output` is saved only when the result is `accepted`.

### AD-009: Agent WS connection registry, scoped reports, receipts, and terminal scoping

**Decision:**

- **Registry.** Introduce `type agentRegistry struct{mu sync.RWMutex; m
  map[string]*AgentConn}`.
  - `register(deviceID, ac)` returns the previous connection. The caller closes
    it (FR-064, AC-077).
  - `unregister(deviceID, ac)` deletes the entry only if it still holds `ac`
    (AC-076).
- `AgentConn.Conn` becomes the `agentSocket` interface. `handlers.Agents()`
  returns an `AgentSender`.
- `HandleAgentWS` routes `command_ack`, `command_progress`, and
  `command_result` to `HandleAgentReport`, using the socket's authenticated
  device ID.
- After a `command_result`, the server replies with `command_receipt` for every
  outcome except an internal error or a rate-limit drop. A WS message that is
  malformed or rate-limited is dropped with no receipt **[SEC C-1]**.
- `conn.SetReadLimit(4 MiB)`.
- **[SEC-014, adopted]** `terminalBrowserConns` stores `{conn, deviceID}`.
  `terminal_output` and `terminal_close` from an agent socket apply only when
  the session's device matches the socket's device.

### AD-010: Agent outbox (file-backed JSON, bounded, WS first, HTTP fallback, legacy mode)

**Decision:** Create `internal/agent/outbox.go`.

**Storage**

- Location: `OutboxPath()`, which is `/var/lib/riot/command-outbox.json`.
- Atomic writes: `os.CreateTemp` in the same directory (exclusive create, mode
  0600), fsync the file, rename, then fsync the directory (best-effort on
  Windows). A missing directory is created with mode 0700.
- Bounds: at most 200 entries, a file of at most 1 MiB, and at most 256 KiB per
  entry (Output is head+tail truncated to fit). Entries older than 24h are
  pruned.
- When a bound forces the oldest entry out, log Warn `outbox entry dropped`.

**Delivery** (one goroutine)

1. **WS connected:** write over WS, then wait for a receipt until
   `next_attempt_at`.
2. **Otherwise, or on a write error or a missing receipt:** HTTP
   `POST …/commands/{url.PathEscape(id)}/report` **[SEC C-7]**.

HTTP outcomes:

| Response | Action |
|---|---|
| 2xx and the JSON echoes `command_id` | Remove the entry |
| 409 `already_terminal` or `invalid_transition` | Remove the entry |
| 404 with `code=command_not_found` | Remove the entry |
| 404 or 405 with no code | Enter legacy mode for 1h (FR-022). Remove the entry if it was already written over WS |
| **429** [SEC C-1], 5xx, 401, 403, network error | `attempts++`; next attempt after `min(5s × 2^(attempts−1), 5m)` |

- **Legacy mode:** WS only. Remove the entry after a successful write.
- **Receipt:** remove the entry.
- **Degraded mode:** if the file cannot be written, keep entries in memory and
  log Error.

### AD-011: Agent command lifecycle wrapper (intake, dedupe, marker, ack, progress, final, recovery, shutdown)

**Decision:** Create `internal/agent/command_lifecycle.go`.
`(*Agent).submitCommand(payload, via)` is the only entry point; both WS and
heartbeat call it.

**Intake order**

0. **[SEC C-4(d)]** If `a.shuttingDown` is set, record the ID as seen and
   enqueue `failed` at `precheck`, reason `shutdown`, message
   `agent shutting down`. It is delivered after the restart. Do not execute.
1. **[SEC C-7]** `models.IsCanonicalUUID(command_id)` must hold. Otherwise log
   Warn `invalid command_id from server` with its length only, and drop. Do not
   execute, do not touch the seen store or the outbox, and send no report.
2. `seen.CheckAndAdd(id)`, durable with file fsync and directory fsync. A
   duplicate is dropped (FR-057).
   - **[SEC C-8]** On a persistence error, the actions in
     `models.DedupeCriticalActions` (`reboot`, `shutdown`, `os_update`,
     `agent_update`) end `failed` at `precheck`, reason `persistence`, with
     message `command dedupe store unavailable; refusing to run <action>`. They
     are **not executed**.
   - Other actions continue in degraded mode with an Error log.
3. `inflight.Put({id, action, target, step:"", started_at})`, durable.
   - **[SEC C-4]** For `docker_update` and `docker_bulk_update`, a persistence
     failure here, or at any later marker write, aborts **before** the next
     Docker mutation. The result is `failed` at the current step, reason
     `persistence`. The normal rollback and restore still run for anything
     already mutated.
4. Send a synchronous ack within 5s, over WS or HTTP. Skip it in legacy mode
   (FR-075, AC-092).
5. `a.cmdWG.Add(1)`, then `go execute` with:
   - `defer recover()`;
   - the class action context;
   - a reporter whose progress goes through the **notifier**. The notifier is
     one ordered goroutine with a queue bound of 64 and a fresh 10s context per
     send. Percent-only updates for the same command are coalesced to at most 1
     per second. Step and state changes are always sent.
   - On finish: `outbox.Add`, then `inflight.Remove`, then `outbox.Kick`.

**Startup recovery [SEC C-4(b)]**

Recovery runs in `Run` before any loop starts, with a total budget of 2m. For
each marker whose action is not `agent_update`, `reboot`, or `shutdown`:

1. For Docker actions, call `reconcileInterrupted(ctx, marker)`
   (`docker_recovery.go`, AD-014) and collect a summary.
2. If `!marker.reported` and the outbox has no entry for that ID, enqueue
   `failed` with step `marker.step`, reason `restarted`, and message
   `agent restarted during execution; outcome unknown` plus
   `; reconciled: <summary>` when there is a summary. Set `reported = true`.
3. Delete the marker when reconciliation finished, or when there was nothing to
   reconcile. If Docker was unreachable, keep it and retry on the next start
   without reporting again.

Recovery then calls `cleanupSanitizedLeftovers()` **[SEC C-5]** (AD-014).

**Shutdown [SEC C-4(d)]**

On SIGTERM:

1. Set `a.shuttingDown` first.
2. `cancel()` the loops.
3. Wait for `a.cmdWG` for up to `models.AgentShutdownGrace` (20s). That is
   below systemd's default 90s `TimeoutStopSec`, and install.sh is unchanged.
4. Exit. Correctness does not depend on the wait, because recovery is the
   backstop.

**Final result wire format (FR-081):** `status` stays `success`/`error`. Add
`step`, `outcome`, `stderr`, and `reason`. The reason enum is
`timeout|rolled_back|restarted|conflict|shutdown|persistence|""`, defined in
`models`.

**Lifecycle events** use the notifier and a fresh 10s context (FR-046).

### AD-012: Per-target in-flight locks

**Decision:** Create `internal/agent/target_lock.go` with `targetLocks`, a
`map[key]lockHolder{cmdID, action}` guarded by a mutex.

- **Methods:**
  - `TryLockAll(keys, cmdID, action)` is all-or-nothing and non-blocking. It
    sorts the keys and releases any partial acquisition.
  - **[SEC C-4(c)]** `HeldWithPrefix(prefix) (key, holder, ok)`.
- **Locks taken:**
  - Host actions lock `host`.
  - `docker_update` locks `docker:container:<name>` **before any Docker call**,
    then resolves the container. A compose container also gets
    `docker:compose:<wd>`.
  - A stack update locks `docker:compose:<wd>`.
  - Bulk locks the union of its targets. Its inner updates run with
    `lockHeld=true`.
  - `docker_stop`, `docker_start`, and `docker_restart` take no lock. They fail
    `precheck` while the target is held.
- **Conflict result:** `update already in progress for <label> (command <id>)`,
  step `precheck`, reason `conflict`.
- **[SEC C-4(c)] Restart/update exclusion:**
  - `agent_update` and `agent_uninstall` fail `precheck`, reason `conflict`,
    with message
    `docker update in progress for <label> (command <id>); refusing agent restart`
    when `HeldWithPrefix("docker:")` is true.
  - The symmetric check (a cheap extension the review did not require):
    `docker_update` and `docker_bulk_update` fail `precheck` when `host` is held
    by `agent_update` or `agent_uninstall`.
  - Reboot and os_update are not restricted (D-013). Recovery is the backstop
    for those.
- Release is deferred, so it covers panics (FR-056).

### AD-013: Step limits with SIGTERM → grace → SIGKILL, and an enforced wall-time budget

**Decision:** Create `internal/agent/proclimit.go` with
`runLimited(ctx, p limitedProcess, limit, grace, clk) (out, timedOut, err)`.
`limitedProcess` provides `Start`, `Wait`, `Terminate` (SIGTERM), `Kill`, and
`Output`. On Windows, `Terminate` falls back to `Kill`. `runUpgradeCommand` is
unchanged.

**Budget constants** (`models`):

| Constant | Value |
|---|---|
| `AckBudget` | 5s |
| `DockerPrecheckBudget` | 15s |
| `ComposePullLimit` | 15m (the sanitize retry shares this budget) |
| `ComposeUpLimit` | 10m |
| `StandalonePullLimit` | 15m |
| `ChildGrace` | 60s |
| `StopDependentsBudget` | 20s |
| `VerifyWindow` | 10s |
| `VerifyBudget` | 15s |
| `DependentRestoreBudget` | 2m |
| `StandaloneRecreateBudget` | 40s |
| `RollbackBudget` | 60s |
| `CleanupBudget` | 15s |
| `AgentShutdownGrace` | 20s |

Maximum wall time:

- Compose: 5s + 15s + 16m + 20s + 11m + 15s + 2m = **29m55s**.
- Standalone: **19m50s**.
- Bulk: before each container, check that
  `elapsed + 29m50s ≤ ExecDeadline − 5m − 30s`. If not, skip the remaining
  containers with message `skipped: bulk time budget exhausted`.

A step that hits its limit ends `failed` with message
`timed out after <limit>` and reason `timeout`.

### AD-014: Docker update execution (rename-aside, dependents, verify, crash safety, argv hygiene)

**Decision:** Rewrite `docker_update.go` against `dockerAPI` (`docker_api.go`;
`*client.Client` satisfies it, checked by a compile-time assertion). Use the
injectable `a.dockerFactory`.

**Resolve**

1. If `container` (a name) is given, match it with
   `strings.TrimPrefix(name, "/")` against `ContainerList(All)`.
2. Otherwise inspect `container_id`.
3. Otherwise use the stack path (`compose_work_dir`).
4. If nothing matches, fail at `resolve` with
   `container "<target>" not found`.

The resolve step **captures the old container's immutable ID** for every later
operation **[SEC C-9]**.

**Stack security check:** `compose_work_dir` must equal the `working_dir` label
of at least one existing container. It must also be an absolute path
(`filepath.IsAbs`; on Linux it must start with `/`) **[SEC C-9]**. Otherwise
fail at `precheck`.

**Compose argv [SEC C-9]**

All compose invocations go through `composeArgs(wd, sanitizedFile, sub,
services...)`. It emits the following, as single tokens:

- `--project-directory=<wd>`;
- `-f=<file>` (only when a sanitized file is in use);
- the sub-command and its flags;
- then a literal `--`;
- then the service names.

Each service name must match `^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`. A label that does
not match fails the step at `restart_dependents` (for dependents) or at
`precheck` (for the main service). The charset alone already rules out a
leading `-`, so `--` is a second layer. The engineer confirms in the
implementation report that the installed compose v2 accepts `--`. If it does
not, drop `--` and rely on the validation.

**Dependents** (`docker_deps.go`, FR-028 to FR-033; standalone parents are
included per BR-007)

- **Identification:** a container is a dependent when its network mode is
  `container:<name>`, `container:/<name>`, `container:<ID>`, or
  `service:<svc>` (same project).
- **Scope:** single-service and standalone updates touch only that parent's
  dependents.
- **Before stopping:** the IDs of the running dependents that will be stopped
  are written durably to the in-flight marker **[SEC C-4(a)]**.
- **Stop:** with a 20s budget.
- **Restore:** a deferred function with its own 2m context (FR-032). For each
  dependent: start it, check that its network mode resolves to the current
  parent ID, and run the compose `up -d --no-deps --force-recreate` fallback
  (through `composeArgs`, with a 90s budget) for a stale compose dependent.
- **Failure:** fail at `restart_dependents` with
  `dependents not restored: <names>; main update <outcome>`, and log Error.

**Crash-safe marker [SEC C-4(a)]**

The in-flight marker's `docker` records are updated durably before each
mutation, in this order:

1. `stopped_dependent_ids`, before stopping dependents.
2. `original_name`, `old_id`, `set_aside_name`, and `was_running`, before stop
   and rename.
3. `replacement_id`, right after create.
4. The set-aside fields are cleared after a successful rename-back in rollback,
   and after cleanup.
5. `sanitized_file`, before the sanitized file is created.

Bulk keeps one record per container.

**Compose step order** (a stack update shows steps 1, 2, 4, and 5 in AC-093)

1. `precheck`
2. `pull` (15m)
3. `stop_dependents`
4. `recreate` (`up -d`, 10m)
5. `verify`, reported as state `verifying`: services that were running are
   running and not restarting 10s after `up`. There is no compose rollback.
6. `restart_dependents`

**Sanitized compose file [SEC C-5]** (`compose_sanitize.go`)

`writeSanitizedCompose(workDir, data)`:

- **Location:** stays in `workDir`, at the fixed name
  `.compose.riot-sanitized.yml`. Relative `extends`, `include`, and `env_file`
  paths resolve against the compose file's own location, so moving it would
  break them.
- **Before writing:**
  - `Lstat` the path.
  - If an entry exists and is a regular file owned by the agent's effective UID
    (`fileowner_unix.go`), remove it. This clears legacy leftovers. On Windows
    (`fileowner_windows.go`), any regular file counts.
  - Anything else, including any symlink, fails the step with
    `refusing to write sanitized compose file: <path> exists and is not an agent-owned regular file`.
- **Create:** `os.OpenFile(path, O_WRONLY|O_CREATE|O_EXCL, 0600)`. `O_EXCL`
  never follows a symlink. Then write and fsync.
- **Removal:**
  - The path is recorded in the marker before creation.
  - The caller defers `removeSanitized(path)` immediately, so the file is
    removed on every exit path, including panics.
  - Startup recovery removes any marker-recorded file.
  - `cleanupSanitizedLeftovers()` uses one `ContainerList` to find the
    `working_dir` labels and removes any agent-owned regular file of that name
    in each directory. Symlinks are unlinked and their targets are never
    touched.

**Standalone step order**

1. `resolve`
2. `precheck`: locks, plus the leftover `<name>.riot-old-*` check (FR-042).
3. `pull`: decode the JSON stream. An `error` or `errorDetail` fails at `pull`
   with stderr set and nothing stopped. If the image ID equals the running
   container's image ID, return `succeeded` with message `already up to date`.
4. `stop_dependents`
5. `recreate`:
   - stop `old_id` if it was running;
   - `ContainerRename(old_id, <name>.riot-old-<cmdid[:8]>)`;
   - create the replacement under the original name with the old config;
   - start it only if the old container was running.
6. `verify` (only if it was running): after 10s, require
   `Running && !Restarting`.
7. `cleanup`: `ContainerRemove(old_id)`. **This is the only removal of the old
   container, and it addresses it by ID [SEC C-9].** If removal fails, the
   command is still `succeeded`, with a message naming the set-aside container.

**Rollback** (fresh 60s context)

1. `ContainerRemove(replacement_id, Force)`, only if a replacement was created.
2. `ContainerRename(old_id, original_name)`.
3. `ContainerStart(old_id)` if it was running.
4. End `failed` with message `rolled back to previous container`, reason
   `rolled_back`, and log Warn.

If the rollback itself fails, end `failed` at `rollback` and name the set-aside
container. No path removes `old_id` except cleanup after a passed verify
(NFR-004).

**Startup reconciliation [SEC C-4(b)]**

`docker_recovery.go`, `reconcileInterrupted(ctx, marker)`, handles each `docker`
record:

- If `old_id` exists under `set_aside_name`:
  - and no container holds `original_name`, rename it back and start it if it
    was running. Summary: `restored <name> from <aside>`;
  - if a container does hold `original_name`, leave both untouched. Summary:
    `left <aside> (previous) and <name> (replacement) untouched; manual check required`.
- Restart every `stopped_dependent_ids` entry that still exists and is not
  running, using the same dependents restore helper. Summary:
  `restarted dependents: …`, plus any failures.
- Remove `sanitized_file`.
- **Reconciliation never removes a container.**

**After any `docker_update` ends:** invalidate the freshness entries for the
refs involved (AD-016), then call `triggerTelemetry`.

### AD-015: Serialized agent WS writer, synchronized connection, backoff reset

**Decision:**

- `connMu sync.RWMutex` guards `conn`, which is an `agentWSConn` interface.
- `writeMu sync.Mutex` serializes every write.
- `send` returns `errNotConnected` when there is no connection, sets a 10s
  write deadline, and closes the connection and returns the error when a write
  fails.
- `dial` and `sleep` are injectable.
- Backoff resets after a successful dial.
- `command_receipt` calls `outbox.Ack`. `command` calls `submitCommand`.
- On connect: `triggerTelemetry`, then `outbox.Kick`.
- The terminal files no longer receive the raw connection.
- `Run` constructs every component before starting any loop.

### AD-016: Freshness verdict per container, registry cache per ref, negative TTL, per-ref invalidation

**Decision:** `ContainerInfo` gains three fields:

| Field | Values |
|---|---|
| `image_id` | the container's running image ID |
| `update_status` | `up_to_date`, `update_available`, `pinned`, `check_failed` |
| `update_reason` | `registry_newer`, `pulled_not_recreated` |

`update_available` stays: it is true only when `update_status` is
`update_available`, false when it is `up_to_date` or `pinned`, and nil when it
is `check_failed`.

**Verdict, per container:**

1. A `sha256:` or `@sha256:` ref is `pinned`, with no registry call.
2. For each unique ref, inspect the local tag once. The registry check result
   is cached by `{local_image_id, result, checked_at}`: 30m for newer/same, 5m
   when the check failed.
3. Per container:
   - `image_id` differs from the local tag's image ID → `pulled_not_recreated`;
   - otherwise newer in the registry → `registry_newer`;
   - same → `up_to_date`;
   - check failed → `check_failed`.

**Invalidation:**

- `InvalidateFreshness(refs...)` clears only the given refs, in memory and on
  disk.
- `docker_check_updates` still clears the whole cache.

Seams: `freshnessAPI`, `manifestInspect`, and `now`.

### AD-017: DB fidelity tests are PG-gated; CI gets a Postgres service

**Decision:** `command_repo_pg_test.go` uses helpers from
`pgtest_helper_test.go`:

- `pgTestURL(t)` reads `RIOT_TEST_PG_URL`.
- **[SEC C-10]** It skips unless the URL's database name contains `test`
  (case-insensitive) or `RIOT_TEST_PG_ALLOW=1` is set. The decision lives in a
  pure function, `pgTestAllowed(rawURL, allowEnv) bool`.
- Each test creates a unique schema `cl_<random>`, migrates through `iofs` over
  `os.DirFS("../../../cmd/riot-server/migrations")`, and drops the schema in
  cleanup.

**`.github/workflows/ci.yml` [SEC C-10]:**

- Top-level `permissions: contents: read`.
- The trigger stays `pull_request`.
- The `test-go` job gets a `postgres:16` service. The engineer pins it by digest
  and records the digest in the implementation report.
- Literal throwaway values: `POSTGRES_USER: riot_test`,
  `POSTGRES_PASSWORD: riot_test`, `POSTGRES_DB: riot_test`.
- `RIOT_TEST_PG_URL=postgres://riot_test:riot_test@localhost:5432/riot_test?sslmode=disable`.
- No `secrets.*` anywhere in the job.

### AD-018: Telemetry ordering with a server-bounded timestamp

**Decision (server):**

1. `receivedAt := h.now().UTC()`. A zero `snap.Timestamp` becomes
   `receivedAt`.
2. **[SEC C-3]** If `snap.Timestamp` is more than
   `models.TelemetryFutureTolerance` (2m) ahead of `receivedAt`, replace it with
   `receivedAt`. This bounded value is what gets stored, compared, and
   broadcast as `collected_at`. The clamp logs Warn
   `telemetry timestamp clamped` (device_id, skew), through the handler's own
   per-device `commands.LogThrottle`.
3. **[SEC C-3]** `TelemetryRepo` "latest" queries ignore rows more than 2m in
   the future (`timestamp <= NOW() + $tolerance`). This lets rows stored before
   this story was deployed age out harmlessly. The queries are
   `GetLatestSnapshot`, `GetAllLatestSnapshots`, `GetAllLatestSummaries`,
   `GetFleetContainerLeaderboard`, `GetGPUDeviceIDs`, and the new
   `LatestSnapshotTime`.
4. `stale := ok && !snap.Timestamp.After(prev)`.
5. Always store the snapshot, logs, and container metrics.
6. When stale, skip the broadcast, primary IP, server-host detection, Docker
   availability update, alerts, UPS status, and auto-update and auto-patch
   evaluation.
7. The envelope gains `collected_at` and `received_at` (telemetry) and
   `received_at` (heartbeat).

**Decision (web):** see AD-019. No contract change: the clamped value arrives in
the same field.

### AD-019: Dashboard command tracking

**Decision:**

- **`web/src/utils/commandLifecycle.ts`** holds the pure helpers: types,
  `isTerminal`, `normalizeStatus`, step labels, `commandLabel`, the target-key
  helpers, `applyCommandUpdate`, `updateIndicatorTitle`, and
  `shouldSuppressExited`.
- **`useCommandTracker(deviceId, targetKey)`:**
  - On mount, fetches the active commands (FR-092).
  - `start` tracks the command on 201, and the existing ID on 409 (FR-093).
  - Applies `command_update` messages.
  - Re-fetches after 5s with no push (FR-099).
  - No fixed timer ends tracking (FR-096).
  - Shows the succeeded state for 5s, then returns to the normal view.
  - Failed stays until dismissed or until a new start.
  - Cancel is available only while queued.
- **`useDevices`** maintains `['commands-active', deviceId]` from
  `command_update`. It suppresses the stop/die → exited mapping when
  `shouldSuppressExited` holds, and merges telemetry through `telemetryMerge.ts`.
- **[SEC C-11]** Every agent-originated string (`result_msg`, `step`, and the
  failure text) is rendered as a React text node. None is passed to
  `dangerouslySetInnerHTML`, `href`, `src`, or `title` with markup.
  `updateIndicatorTitle` uses server enums only.

### AD-020 (new, [SEC C-1]): Per-device report rate limit, progress gate, and log throttle

**Decision:** Three small in-memory components in `internal/server/commands`.
Each has an injected clock and needs no background goroutine, so it does not
leak in tests.

1. **`ratelimit.go`, `DeviceLimiter`**
   - A token bucket keyed by the **authenticated device ID**.
   - Defaults, as package constants: `ReportRatePerMinute = 120` and
     `ReportBurst = 60`.
   - Idle buckets (no traffic for 10m) are evicted lazily, every 256 calls.
   - One instance per `Service`, shared by HTTP and WS through
     `Service.AllowReport(deviceID) bool`.
   - Over the limit, HTTP returns **429**
     `{"error":"too many requests","code":"rate_limited"}`. WS messages are
     dropped with no receipt, and the agent outbox falls back to HTTP and
     retries with backoff.
2. **Progress gate** (in `service.go`)
   - `map[commandID]lastAcceptedProgressAt`.
   - An entry is deleted on any terminal transition and pruned when idle for
     more than 1m.
   - A minimum interval of 1s applies to `running → running` writes.
3. **`logthrottle.go`, `LogThrottle`**
   - A fixed one-minute window per device. Up to 10 records pass, then they are
     suppressed.
   - The first allowed call in a new window emits one summary Warn,
     `rejection logs suppressed` (device_id, count), when the previous window
     suppressed anything.
   - The `Service` uses one instance for its rejection, unknown, and malformed
     logs. `Handlers` uses another for telemetry clamp logs.

**Budget check:** for a 50-container bulk update, there is one command. The
agent sends step changes as they happen and coalesces percent-only updates to
1/s, and the server gate keeps accepted `running → running` writes to 1/s. A
fast "already up to date" sweep peaks at about 3 reports per second for
seconds, which the burst of 60 absorbs. Sustained traffic is under 60 per
minute, well within 120 per minute.

**Alternatives considered:** Reusing `middleware.RateLimiter` was rejected. It
is keyed by IP, uses the wall clock with a goroutine, and is HTTP-only.

---

## 4. Component Changes

Each file belongs to exactly one work package. **WP-GO** is everything outside
`web/`. **WP-WEB** is `web/` only. Rows added in rev 2 are marked **[SEC]**.

### 4.1 WP-GO: models, migration, server

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `internal/models/command_state.go` | AD-001: state machine, aliases, `CanonicalStatus`/`LegacyStatus`, **typed `SQLStatusList`**, `ParseStatusFilterValue` |
| CREATE | `internal/models/command_state_test.go` | AC-085, 101, 102; **SEC: NoResurrectionEdges, SQLStatusList rejects unknown** |
| CREATE | `internal/models/command_policy.go` | Classes, TTL, deadlines, budgets, backoff, `FormatDurationShort`, target keys, step constants. **[SEC]** Report bounds (`MaxReportStepBytes = 64`, `StepPattern`, `MaxProgressMessageBytes = 1024`, `MaxResultMsgBytes = 65536`, `MaxStderrTailBytes = 8192`, `MaxReportOutputBytes = 262144`, `MaxTargetKeyFilterBytes = 512`), the `ReportReasons` enum, `IsCanonicalUUID`, `SanitizeText`, the rune-safe `TruncateTail`/`TruncateHead`/`TruncateHeadTail`, `DedupeCriticalActions`, `AgentShutdownGrace` |
| CREATE | `internal/models/command_policy_test.go` | AC-007, 029, 094; DeadlineInvariant; **SEC002 sanitizer table** |
| MODIFY | `internal/models/commands.go` | `Command` gains the new fields, plus `PreLifecycle` and `SendAmbiguous` (both `json:"-"`). `CommandResult` gains its new fields. New types: `CommandTransition`, `CommandReport`, `CommandAck`, `CommandProgress`, `CommandReceipt`, `CommandUpdate`, `CommandConflictResponse`, `ReportResponse`. `AutoUpdatePolicy` gains its new fields |
| MODIFY | `internal/models/telemetry.go` | `ContainerInfo.ImageID`, `UpdateStatus`, `UpdateReason`. **[SEC]** `TelemetryFutureTolerance = 2 * time.Minute` |
| CREATE | `cmd/riot-server/migrations/000023_command_lifecycle.up.sql` | §5 (includes `pre_lifecycle` and `send_ambiguous`) |
| CREATE | `cmd/riot-server/migrations/000023_command_lifecycle.down.sql` | §5 (includes the SEC-009 power-row failure) |
| MODIFY | `internal/server/db/interfaces.go` | New `CommandRepository`. `AutoUpdateRepository` and `TelemetryRepository` changes |
| MODIFY | `internal/server/db/command_repo.go` | AD-003 methods. **[SEC]** Pure `buildFilteredQuery`/`buildActiveQuery`, filters bound with `ANY($n)`. Removes `UpdateStatus`, `UpdateCommandResult`, `ListPending` |
| CREATE | `internal/server/db/command_repo_test.go` | AC-101 scan. **SEC006 query builders use only placeholders; target key bound** |
| CREATE | `internal/server/db/command_repo_pg_test.go` | PG tests: AC-011, 023, 073, 101, 103, 107; NFR-002, NFR-007; **SEC002, SEC003, SEC006, SEC009, SEC_ActiveRowsHaveDeadline, SEC_ClaimSkipsExpired** |
| CREATE | `internal/server/db/pgtest_helper_test.go` | **[SEC]** `pgTestAllowed`, `pgTestURL`, schema setup and teardown. `TestCL_SEC013_PGTestsSkipOnNonTestDatabase` |
| MODIFY | `internal/server/db/auto_update_repo.go` | `RecordSuccess`/`RecordFailure`; new columns; removes `SetLastTriggered` |
| MODIFY | `internal/server/db/telemetry_repo.go` | `LatestSnapshotTime`. **[SEC]** Latest queries ignore rows more than 2m in the future |
| CREATE | `internal/server/commands/service.go` | AD-002, AD-008 (incl. the progress gate), `AllowReport` |
| CREATE | `internal/server/commands/validate.go` | **[SEC]** `validateReportIDs`, `sanitizeReport` (C-1, C-2) |
| CREATE | `internal/server/commands/validate_test.go` | **[SEC]** `TestCL_SEC001_SanitizeReportTable`, `TestCL_SEC002_SanitizeNULAndInvalidUTF8` |
| CREATE | `internal/server/commands/ratelimit.go` | **[SEC]** AD-020 `DeviceLimiter` |
| CREATE | `internal/server/commands/ratelimit_test.go` | **[SEC]** `TestCL_SEC001_DeviceLimiterTokenBucket` |
| CREATE | `internal/server/commands/logthrottle.go` | **[SEC]** AD-020 `LogThrottle` (exported; also used by handlers) |
| CREATE | `internal/server/commands/logthrottle_test.go` | **[SEC]** `TestCL_SEC001_LogThrottleSummary` |
| CREATE | `internal/server/commands/targets.go` | AD-004 resolution |
| CREATE | `internal/server/commands/sweeper.go` | Sweep and RunSweeper. **[SEC-010]** Silent handling of pre-lifecycle rows |
| CREATE | `internal/server/commands/service_test.go` | AC-005, 006, 009, 029, 073, 085-089, 094-098, 102, 104, 107; **SEC001 coalescing, throttle, and logs; SEC002; SEC016; SEC_ProgressDoesNotExtendDeadline; SEC_ClaimSkipsExpired; SEC_ReportContentNotLogged; SEC_ActiveRowsHaveDeadline** |
| CREATE | `internal/server/commands/sweeper_test.go` | AC-026, 027, 031, 103; **SEC010** |
| CREATE | `internal/server/commands/targets_test.go` | Resolution table |
| MODIFY | `internal/server/handlers/handlers.go` | `HandlerDeps.Commands` and `Clock`; heartbeat claim; telemetry ordering. **[SEC]** Clamp plus `telemetryLogThrottle` |
| MODIFY | `internal/server/handlers/commands.go` | `SendCommand`, `BulkDockerUpdate`, list filters, `GetCommand`, `CancelCommand`, `AgentCommandReport`. **[SEC]** `parseStatusFilter` (400 `invalid_filter`), limiter and UUID checks before decode, `MaxBytesReader` |
| MODIFY | `internal/server/handlers/auto_update.go` | AD-007, clock, window end |
| MODIFY | `internal/server/handlers/terminal.go` | AD-009 registry, scoped reports, receipts, read limit. **[SEC]** WS limiter, UUID drop, terminal session scoping (SEC-014) |
| MODIFY | `internal/server/handlers/fleet.go` | Through the service; `conflicts` count |
| MODIFY | `internal/server/handlers/device_probes.go` | Through the service |
| CREATE | `internal/server/handlers/command_lifecycle_test.go` | AC-016, 023, 024, 028, 068-071, 074, 090, 091, 105, 106; **SEC001 (NonUUID, StepAndMessageBounded, OutputCapped, RateLimited), SEC006 handler, SEC_ForeignAndUnknownIndistinguishable, SEC_BodyCommandIDMismatch400, SEC_ReportBodyOver512KiB413** |
| CREATE | `internal/server/handlers/dispatch_paths_test.go` | AC-075 |
| CREATE | `internal/server/handlers/agent_registry_test.go` | AC-076, 077, AC-024 (WS); **AC-024 WSForeignAckAndProgressIgnored, SEC014** |
| CREATE | `internal/server/handlers/telemetry_order_test.go` | AC-079, 083, 084; **SEC003 (all three)** |
| MODIFY | `internal/server/handlers/auto_update_test.go` | Adapt; AC-001, 008, 010, 012, 030, 072 |
| MODIFY | `internal/server/handlers/commands_test.go` | Adapt to canonical values and `Transition` |
| MODIFY | `internal/server/handlers/heartbeat_compat_test.go` | Adapt to claim semantics |
| MODIFY | `internal/server/handlers/handlers_test.go` | Adapt fixtures |
| MODIFY | `internal/server/events/generator.go` | **[SEC C-2]** `CommandCompleted` sanitizes the reason and truncates it with `models.TruncateHead(SanitizeText(msg), 200)` |
| MODIFY | `internal/server/events/generator_test.go` | **[SEC]** `TestCL_SEC002_CompletionEventReasonRuneSafe` |
| MODIFY | `internal/server/events/generator_integration_test.go` | Adapt to `MockCommandRepo` API changes, if needed |
| MODIFY | `internal/server/websocket/hub.go` | Envelope timestamps, `BroadcastCommandUpdate`; removes `BroadcastCommandResult` |
| MODIFY | `internal/server/websocket/hub_test.go` | Envelope marshalling |
| MODIFY | `internal/server/server.go` | Constructs `Commands`, runs the sweeper |
| MODIFY | `internal/server/router.go` | Passes deps. **[SEC]** Extract `registerCommandRoutes(r, h, deviceAuth, adminAuth)` so the auth matrix is testable. The report route uses the full `deviceAuth...` stack |
| CREATE | `internal/server/router_test.go` | **[SEC]** `TestCL_SEC_RouteAuthMatrix` |
| MODIFY | `internal/testutil/mocks.go` | Faithful `MockCommandRepo`, policy and telemetry mocks, fakes. **[SEC]** Records every call, so tests can assert "repository never called" |
| MODIFY | `.github/workflows/ci.yml` | **[SEC C-10]** Postgres service with literal creds, pinned digest, `permissions: contents: read`, `RIOT_TEST_PG_URL` |

### 4.2 WP-GO: agent

| Action | File Path | Purpose |
|---|---|---|
| MODIFY | `internal/agent/ws_client.go` | AD-015, receipts, `submitCommand` |
| CREATE | `internal/agent/ws_client_test.go` | AC-013, 014, 015, 078 |
| CREATE | `internal/agent/outbox.go` | AD-010 (429 is retried) |
| CREATE | `internal/agent/outbox_test.go` | AC-015 to 020, 025; **SEC001 Outbox429RetriesWithBackoff** |
| CREATE | `internal/agent/cmdstate.go` | `atomicJSONFile` (exclusive-create temp file 0600, file and dir fsync, injectable fs seam for fault tests), `seenStore`, `inflightStore` (marker with `docker` records and `reported`) |
| CREATE | `internal/agent/cmdstate_test.go` | Persistence and caps; **SEC_AgentStateFilesMode0600 (Linux)** |
| CREATE | `internal/agent/target_lock.go` | AD-012 including `HeldWithPrefix` and lock holder actions |
| CREATE | `internal/agent/target_lock_test.go` | AC-066 |
| CREATE | `internal/agent/command_lifecycle.go` | AD-011 including UUID intake, seen fail-closed, shutdown gate, `cmdWG`, recovery driver |
| CREATE | `internal/agent/command_lifecycle_test.go` | AC-021, 022, 053, 066, 067, 092, 094, 099; **SEC004 AgentUpdate/AgentUninstall rejected and NoNewCommandsAfterSIGTERM; SEC007 NonUUID; SEC008 (both)** |
| CREATE | `internal/agent/legacy_server_test.go` | AC-100 |
| MODIFY | `internal/agent/commands.go` | `executeAction`, class contexts, host lock and C-4(c) checks, results to the outbox |
| MODIFY | `internal/agent/heartbeat.go` | Through `submitCommand` |
| MODIFY | `internal/agent/agent.go` | Fields, construction order, recovery, outbox loop. **[SEC C-4(d)]** `shuttingDown`, `cmdWG`, the 20s shutdown wait |
| MODIFY | `internal/agent/config.go` | `OutboxPath`, `SeenCommandsPath`, `InflightPath` |
| MODIFY | `internal/agent/http_client.go` | `PostCommandReport`. **[SEC C-7]** `url.PathEscape` for the device and command IDs |
| CREATE | `internal/agent/http_client_test.go` | **[SEC]** `TestCL_SEC007_ReportURLPathEscaped` |
| MODIFY | `internal/agent/docker_update.go` | AD-014 rewrite including marker writes before mutations, `composeArgs`, removal by ID. **The sanitize function moves to `compose_sanitize.go`** |
| CREATE | `internal/agent/docker_api.go` | `dockerAPI` interface and factory |
| CREATE | `internal/agent/docker_deps.go` | Dependents identification, stop, restore (via `composeArgs`) |
| CREATE | `internal/agent/docker_deps_test.go` | AC-032, 033; **SEC011 ServiceNameValidatedOrSeparated** |
| CREATE | `internal/agent/docker_recovery.go` | **[SEC C-4(b)]** `reconcileInterrupted`; **[SEC C-5]** `cleanupSanitizedLeftovers` |
| CREATE | `internal/agent/docker_recovery_test.go` | **[SEC]** SEC004 recovery tests (3); `TestCL_NFR004_RecoveryNeverRemovesContainers` |
| CREATE | `internal/agent/compose_sanitize.go` | **[SEC C-5]** `sanitizeComposeFile` (moved), `writeSanitizedCompose`, `removeSanitized` |
| CREATE | `internal/agent/compose_sanitize_test.go` | **[SEC]** SEC005 (four tests) |
| CREATE | `internal/agent/fileowner_unix.go` | **[SEC]** `//go:build !windows`: `ownedByEUID(fi)` via `syscall.Stat_t` |
| CREATE | `internal/agent/fileowner_windows.go` | **[SEC]** `ownedByEUID` returns `fi.Mode().IsRegular()` |
| CREATE | `internal/agent/proclimit.go` | AD-013 |
| CREATE | `internal/agent/proclimit_test.go` | AC-050, 051 |
| CREATE | `internal/agent/docker_update_test.go` | AC-002 to 004, 034 to 049, 052, 061, 063 to 065, 093; NFR-004 table; **SEC004 MarkerPersistedBeforeRenameAndDependentStop; SEC011 CleanupAndRollbackUseOldContainerID** |
| CREATE | `internal/agent/fakes_test.go` | Shared fakes, plus a fault-injecting fs for the state stores |
| MODIFY | `internal/agent/terminal.go` | Removes the connection parameter |
| MODIFY | `internal/agent/host_terminal.go` | Same |
| MODIFY | `internal/agent/host_terminal_windows.go` | Same |
| MODIFY | `internal/agent/collectors/docker.go` | AD-016 |
| MODIFY | `internal/agent/collectors/docker_cache.go` | AD-016 entry shape |
| MODIFY | `internal/agent/collectors/docker_test.go` | Adapt fixtures |
| CREATE | `internal/agent/collectors/docker_freshness_test.go` | AC-054 to 061 |
| (none) | `internal/agent/doctor.go`, `scripts/install.sh` | **Unchanged.** No collector or privilege change, and no `TimeoutStopSec` change (the AD-011 shutdown grace is below the default) |

### 4.3 WP-WEB (web/ only)

| Action | File Path | Purpose |
|---|---|---|
| MODIFY | `web/src/types/models.ts` | Canonical `Command`, `CommandState`, `CommandUpdate`, `ContainerInfo` fields, `WSMessage` timestamps |
| MODIFY | `web/src/api/client.ts` | `CommandConflictError`; `getCommand`, `cancelCommand`, `getActiveCommands`; bulk conflicts |
| MODIFY | `web/src/api/client.test.ts` | AC-111 |
| MODIFY | `web/src/api/demo-client.ts` | Stubs |
| MODIFY | `web/src/api/demo-data.ts` | Canonical statuses |
| CREATE | `web/src/utils/commandLifecycle.ts` | AD-019 helpers |
| CREATE | `web/src/utils/commandLifecycle.test.ts` | AC-062, 114, 115; target keys |
| CREATE | `web/src/hooks/telemetryMerge.ts` | Pure merge |
| CREATE | `web/src/hooks/telemetryMerge.test.ts` | AC-080, 081, 082 |
| CREATE | `web/src/hooks/useCommandTracker.ts` | AD-019 |
| CREATE | `web/src/hooks/useCommandTracker.test.tsx` | AC-108 (no fixed timer), 110, 111, 117 |
| MODIFY | `web/src/hooks/useDevices.ts` | Merge, received_at, active cache, suppression |
| MODIFY | `web/src/hooks/useDevices.test.ts` | AC-082, 112, 113 |
| MODIFY | `web/src/components/ContainerDetail.tsx` | Tracker, labels, disabled state, Cancel/Dismiss, name param, reason titles |
| CREATE | `web/src/components/ContainerDetail.test.tsx` | AC-108, 109, 112, 114, 116; **[SEC] agent failure text renders as text** |
| MODIFY | `web/src/components/ComposeStackSection.tsx` | Tracker for the stack key |
| CREATE | `web/src/components/ComposeStackSection.test.tsx` | AC-108, 109, 111; **[SEC] agent failure text renders as text** |
| MODIFY | `web/src/components/CompactContainerTile.tsx` | Indicator title |
| MODIFY | `web/src/components/CompactContainerTile.test.tsx` | AC-062 |
| MODIFY | `web/src/components/ActivityLog.tsx` | Canonical labels and filter (allowlisted values only), `command_update` |
| MODIFY | `web/src/components/ActivityLog.test.tsx` | AC-115; **[SEC] agent failure text renders as text** |
| MODIFY | `web/src/pages/ContainerDetailPage.test.tsx` | Mock the new API methods |
| MODIFY | `web/src/pages/DeviceDetail.test.tsx` | Mock the new API methods, if needed |

No other file may change. Global styles, tokens, and the Tailwind config are not
touched.

## 5. Data Model Changes

### 5.1 `commands` (000023 up)

Columns added:

| Column | Type | Null | Default | Meaning |
|---|---|---|---|---|
| `expires_at` | TIMESTAMPTZ | NOT NULL (after backfill) | none | Queued TTL deadline |
| `exec_deadline_s` | INTEGER | NOT NULL (after backfill) | none | AD-005 |
| `deadline_at` | TIMESTAMPTZ | NULL | | Set at claim |
| `dispatched_at`, `acknowledged_at`, `started_at`, `verifying_at`, `finished_at` | TIMESTAMPTZ | NULL | | Per-state timestamps |
| `step` | TEXT | NOT NULL | `''` | Sanitized, ≤ 64 bytes |
| `progress` | SMALLINT | NULL | | 0..100 |
| `failure_step` | TEXT | NOT NULL | `''` | Sanitized, ≤ 64 bytes |
| `stderr_tail` | TEXT | NOT NULL | `''` | ≤ 8192 bytes, sanitized |
| `origin` | TEXT | NOT NULL | `'user'` | CHECK in (user, auto_update, auto_patch) |
| `policy_id` | INTEGER | NULL | | FK to `auto_update_policies(id)` ON DELETE SET NULL |
| `target_keys` | TEXT[] | NOT NULL | `'{}'` | AD-004 |
| **`pre_lifecycle`** [SEC-010] | BOOLEAN | NOT NULL | Added as `true` (existing rows), then the default is set to `false` | Marks rows that existed before the migration |
| **`send_ambiguous`** [SEC-016] | BOOLEAN | NOT NULL | `false` | Set by the FR-085 revert |

`result_msg` (existing) is now sanitized and ≤ 64 KiB on agent writes.
`command_output.content` is sanitized and ≤ 256 KiB.

**Data steps,** in one file, with no `CONCURRENTLY`:

1. Add the columns.
2. Map statuses: pending → queued, sent → dispatched, success → succeeded,
   error → failed.
3. Backfill `exec_deadline_s` per action:

   | Action | Seconds |
   |---|---|
   | reboot, shutdown | 600 |
   | os_update, enable_auto_updates | 2400 |
   | docker_update | 2100 |
   | docker_bulk_update | `LEAST(2100*GREATEST(n,1)+300, 21600)`, where `n` is the size of the `container_ids` array |
   | agent_update | 900 |
   | anything else | 900 |

4. Set `expires_at = created_at + TTL`, using 15, 30, 60, 60, 60, and 30 minutes
   for the classes in AD-005.
5. For `dispatched` rows, set `dispatched_at` and `deadline_at` from
   `updated_at`.
6. Set `finished_at = updated_at` on terminal rows.
7. Best-effort `origin`, from the `result_msg` prefixes `auto-update` and
   `auto-patch`.
8. Best-effort `target_keys` for active rows, from `params`: host actions,
   `compose_work_dir`, `container`, `container_id`.
9. Set `expires_at` and `exec_deadline_s` to NOT NULL.
10. Create three partial indexes:
    - `idx_commands_active_device ON commands (device_id, created_at) WHERE status IN ('queued','pending','dispatched','sent','acknowledged','running','verifying')`
    - `idx_commands_queued_expiry ON commands (expires_at) WHERE status IN ('queued','pending')`
    - `idx_commands_inflight_deadline ON commands (deadline_at) WHERE status IN ('dispatched','sent','acknowledged','running','verifying')`

### 5.2 `auto_update_policies`

- Add `consecutive_failures INTEGER NOT NULL DEFAULT 0` and
  `last_failure_at TIMESTAMPTZ NULL`.
- FR-008: `UPDATE auto_update_policies SET last_triggered_at = NULL WHERE is_stack = false`.

### 5.3 Down migration (000023 down)

1. **[SEC-009, adopted]** Fail queued power actions regardless of expiry:

   ```sql
   UPDATE commands SET status='error', result_msg='not delivered: power action dropped on downgrade', updated_at=NOW()
   WHERE status IN ('queued','pending')
     AND (action IN ('reboot','shutdown')
          OR (action='os_update' AND (params->>'include_reboot_class')::boolean IS TRUE))
   ```

2. Expire overdue queued rows:

   ```sql
   UPDATE commands SET status='error', result_msg='expired: not delivered within TTL', updated_at=NOW()
   WHERE status IN ('queued','pending') AND expires_at <= NOW()
   ```

3. Map statuses back:
   - dispatched, acknowledged, running, verifying → `sent`;
   - succeeded → `success`;
   - failed, timed_out, cancelled, expired → `error`.
4. Drop the indexes and every added column on both tables. The FR-008 reset is
   not restored.

### 5.4 Agent-side persisted files (mode 0600, `/var/lib/riot`)

| File | Shape | Bounds |
|---|---|---|
| `command-outbox.json` | `{"version":1,"entries":[{"command_id","result","created_at","attempts","next_attempt_at","ws_written"}]}` | ≤ 200 entries, ≤ 1 MiB total, ≤ 256 KiB per entry, 24h |
| `seen-commands.json` | `{"version":1,"seen":{"<id>":"<first_seen>"}}` | ≤ 1000, 48h |
| `inflight-commands.json` | `{"version":1,"inflight":{"<id>":{"action","target","step","started_at","reported":false,"docker":[{"original_name","old_id","set_aside_name","was_running","replacement_id","stopped_dependent_ids":[],"sanitized_file":""}]}}}` [SEC C-4] | Active commands, plus records whose reconciliation was deferred |
| `docker-freshness-cache.json` (existing path) | `{"<ref>":{"local_image_id","result","checked_at"}}` | 30m / 5m |

### 5.5 Go structs

As in rev 1, with these additions: `Command.PreLifecycle` and
`Command.SendAmbiguous`, both `json:"-"`, and the `models.ReportReasons` enum.
`CommandResult` gains `Step`, `Outcome`, `Stderr`, and `Reason`.
`AutoUpdatePolicy` gains `ConsecutiveFailures` and `LastFailureAt`.
`ContainerInfo` gains `ImageID`, `UpdateStatus`, and `UpdateReason`.

## 6. API / Interface Contract (FROZEN for WP-WEB)

Items tagged **[SEC]** are rev-2 additions that the security conditions forced.
**None changes a shape WP-WEB consumes.** They add error cases, or tighten field
contents that WP-WEB already handles (an empty `step` renders as "Working").

### 6.1 Command JSON

```
{ "id","device_id","action","params",
  "status": "queued|dispatched|acknowledged|running|verifying|succeeded|failed|timed_out|cancelled|expired",
  "result_msg","duration_ms","exit_code","created_at","updated_at","expires_at","exec_deadline_s",
  "dispatched_at"|"acknowledged_at"|"started_at"|"verifying_at"|"finished_at"|"deadline_at": RFC3339|absent,
  "step","progress":int|null,"failure_step","stderr_tail","origin","policy_id":int|null,
  "target_key","target_keys":[...] }
```

`pre_lifecycle` and `send_ambiguous` are **not** exposed.

### 6.2 Admin endpoints (JWT cookie)

| Method and path | Request | Responses |
|---|---|---|
| `POST /api/v1/devices/{id}/commands` | `{"action","params"}` | **201** Command. **409** `{"error":"a command is already in progress for this target","code":"conflict","existing_command_id","existing_status","target_key"}`. **400**. **500** |
| `POST /api/v1/devices/{id}/docker/bulk-update` | `{"container_ids":[...]}` | **201** Command plus `"conflicts":[{"container_id","target_key","existing_command_id"}]`. **409** (all targets conflict) `{"error","code":"conflict","existing_command_id","conflicts":[...]}`. **400** |
| `GET /api/v1/devices/{id}/commands` | `limit`, `offset`, `action`, `status` (canonical or legacy, comma-separated), `active=true`, `target_key` | **200** `Command[]`. **[SEC C-6] 400** `{"error":"invalid status filter","code":"invalid_filter"}` for any status value outside the canonical-plus-legacy allowlist, or a `target_key` over 512 bytes |
| `GET /api/v1/devices/{id}/commands/{commandId}` | none | **200** Command. **404** `{"error":"command not found","code":"command_not_found"}` (also returned for a foreign command). **[SEC] 404** (same body) for a non-UUID `commandId` |
| `POST /api/v1/devices/{id}/commands/{commandId}/cancel` | empty | **200** Command. **409** `{"error":"command cannot be cancelled after it has been dispatched","code":"not_cancellable","status"}`. **404** |
| `GET …/commands/{commandId}/output` | unchanged | unchanged |
| `POST /api/v1/fleet/bulk-update`, `/bulk-patch` | unchanged | Adds `"conflicts": <int>` |

### 6.3 Agent endpoint (full `deviceAuth...` stack: X-rIOt-Key plus mTLS when enabled)

`POST /api/v1/devices/{id}/commands/{commandId}/report`. The body is capped at
512 KiB.

Body: `{kind: "ack"|"progress"|"result", command_id?, state?, step?, progress?, message?, result?}`.

**Processing order [SEC C-1]:**

1. Rate limit (by device).
2. Validate that the path `commandId` is a canonical UUID.
3. `MaxBytesReader`, then decode.
4. Validate that the body `command_id`, when present, equals the path ID.
5. Validate `kind` and `state`.
6. Sanitize and apply AD-008.

| Status | Body | Agent action |
|---|---|---|
| 200 | `{"command_id","result":"accepted"\|"duplicate"\|"coalesced","status"}` | Remove the entry |
| 409 | `{"error","code":"already_terminal"\|"invalid_transition","status"}` | Remove or drop |
| 404 | `{"error":"command not found","code":"command_not_found"}` | Remove |
| 404 or 405 with no code | (legacy server) | Legacy mode for 1h |
| 400 | `{"error","code":"invalid_report"}`: body/path ID mismatch, bad `kind`/`state`, **[SEC] non-UUID ID** | Drop (log Error) |
| 413 | | Drop |
| **429** [SEC C-1] | `{"error":"too many requests","code":"rate_limited"}` | Retry with backoff |
| 401, 403, 5xx | | Retry with backoff |

**Field rules [SEC C-1/C-2],** applied on both HTTP and WS:

| Field | Rule |
|---|---|
| `step`, `failure_step` | Must match `^[a-z0-9_]{1,64}$`, otherwise stored as `""` |
| progress `message` | ≤ 1 KiB |
| `reason` | Must be in `timeout\|rolled_back\|restarted\|conflict\|shutdown\|persistence`, otherwise `""` |
| `output` | ≤ 256 KiB, head and tail |
| `stderr` | Last 8 KiB |
| `message` (result) | Last 64 KiB |
| All strings | NUL removed, invalid UTF-8 replaced with U+FFFD |

### 6.4 WebSocket, agent and server

| Direction | `type` | `data` |
|---|---|---|
| server → agent | `command` | Unchanged |
| server → agent | `command_receipt` | `{"command_id","result":"accepted"\|"duplicate"\|"coalesced"\|"already_terminal"\|"invalid_transition"\|"not_found","status"}` |
| agent → server | `command_ack` | `{"command_id","at"}` |
| agent → server | `command_progress` | `{"command_id","state","step","progress"?,"message"?,"at"}` |
| agent → server | `command_result` | `CommandResult` plus `step`, `outcome`, `stderr`, `reason` |

**[SEC C-1]** On WS, a non-UUID `command_id`, or any report over the device
rate limit, is dropped with no receipt and no repository call. The §6.3 field
rules apply.

### 6.5 WebSocket, server to dashboard

- `command_update` (once per accepted transition; coalesced reports produce no
  message): `{"command_id","device_id","action","target_key","target_keys","status","step","progress","message","failure_step","origin","updated_at"}`.
  **[SEC-010]** It is not emitted for pre-lifecycle rows swept after the
  migration.
- `telemetry`: the envelope gains `collected_at` and `received_at`.
  **[SEC C-3]** `collected_at` is never more than 2m ahead of `received_at`.
- `heartbeat`: the envelope gains `received_at`.

### 6.6 Go interfaces

`db.CommandRepository`:

- `CreateIfNoConflict`, `GetByID`, `GetForDevice`, `ListByDevice`,
  `ListByDeviceFiltered` (filters bound), `ListActive`, `ClaimQueued`,
  `ClaimByID`, `Transition`, `ListExpiredQueued`, `ListOverdue`,
  `SaveCommandOutput`, `GetCommandOutput`.

`models.CommandTransition`:

- `ID`, `DeviceID`, `To`, `From`, `Step`, `ProgressSet`/`Progress`,
  `ResultMsg`, `FailureStep`, `StderrTail`, `ExitCode`, `DurationMs`, `Now`.

`db.AutoUpdateRepository`:

- Adds `RecordSuccess` and `RecordFailure`; removes `SetLastTriggered`.

`db.TelemetryRepository`:

- Adds `LatestSnapshotTime`.

`commands.Service` adds:

- `AllowReport(deviceID) bool`, used by both the HTTP and the WS handler.

### 6.7 Frozen dashboard rules (WP-WEB, unchanged by rev 2)

**Target keys.** A compose container (one with project, service, and
working_dir labels) uses `docker:compose:<wd with trailing / removed>`. Any
other container uses `docker:container:<name without a leading />`. A stack
uses `docker:compose:<workDir with trailing / removed>`. A command matches a
target when its `target_keys` includes the key.

**Non-terminal statuses:** queued, dispatched, acknowledged, running,
verifying.

**Legacy normalization:** `pending` → queued, `sent` → dispatched, `success` →
succeeded, `error` → failed.

**Step labels:**

| Step | Label |
|---|---|
| precheck | Checking |
| resolve | Resolving container |
| pull | Pulling image |
| stop_dependents | Stopping dependents |
| recreate | Recreating |
| verify | Verifying |
| restart_dependents | Restarting dependents |
| rollback | Rolling back |
| cleanup | Cleaning up |
| anything else (including `""`) | Working |

**Update-action labels.** The ellipsis is U+2026.

| Status | Label |
|---|---|
| queued | "Queued" |
| dispatched | "Sent to agent" |
| acknowledged | "Starting" |
| running | "Updating… \<step label\>" |
| verifying | "Verifying" |
| succeeded | "Updated" (for 5s) |
| failed | "Failed at \<failure_step \|\| step\>: \<result_msg\>", or "Failed: \<result_msg\>" when there is no step |
| timed_out | "Timed out: outcome unknown" |
| expired | "Expired: never delivered" |
| cancelled | "Cancelled" |

Non-update actions use "Running… \<step\>" while running and "Done" when
succeeded.

**History chips:** Queued, Sent to agent, Acknowledged, Running, Verifying,
Succeeded, Failed, Timed out, Cancelled, Expired.

**Applying a `command_update`:** apply it only if
`updated_at >= current.updated_at`, and never move a command from terminal back
to non-terminal.

**Telemetry merge:**

- Discard the update if `collected_at <= latest_telemetry.timestamp`.
- On apply, set `latest_telemetry.timestamp = collected_at` and
  `device.last_telemetry = received_at`.
- A heartbeat sets `last_heartbeat = received_at`.
- When a timestamp field is absent, leave the stored value unchanged. Never use
  `new Date()` for data.

**Indicator title:**

| Reason | Title |
|---|---|
| `registry_newer` | "Update available: newer image in registry" |
| `pulled_not_recreated` | "Update available: newer image pulled, container not recreated" |
| none | "Update available" |

`pinned` and `check_failed` render no indicator. ContainerDetail's Status
section shows "Pinned by digest" or "Update check failed" instead.

**[SEC] Rendering rule** (restating AD-019; no shape change): agent-originated
text is rendered only as a text node.

## 7. Sequence / Flow

**7.1 Manual update (new agent, WS up)**

1. POST the command. `Dispatch` resolves the target keys and computes the
   expiry.
2. `CreateIfNoConflict`. On a conflict, return 409. Otherwise the command is
   `queued` and is broadcast.
3. If the agent is connected: `ClaimByID` moves it to `dispatched`, it is
   broadcast, and it is sent.
4. If the send fails: revert to `queued`, set `send_ambiguous`, and set
   `result_msg` to "ws send failed; delivery uncertain; re-queued for heartbeat
   delivery" [SEC-016].
5. On the agent:
   1. UUID check.
   2. Seen check, persisted.
   3. Marker written.
   4. Synchronous ack.
   5. Steps run, each mutation preceded by a marker update.
   6. Deferred restore.
   7. Outbox.
6. The agent sends the result over WS. On the server: limiter, then validate,
   then sanitize, then the guarded transition. The server saves output,
   records the policy outcome, emits the event and the broadcast, and sends a
   receipt. The agent removes the outbox entry.

**7.2 Heartbeat delivery**

1. Expire due queued rows for the device.
2. `ClaimQueued`.
3. Return only the claimed payloads.

**7.3 Sweeper** (runs immediately, then every 60s)

1. Expired queued rows move to `expired` with "expired: not delivered within
   \<TTL\>". **[SEC-016]** If `send_ambiguous` is set, the message is
   "expired: delivery unconfirmed within \<TTL\>; an earlier send attempt may
   have reached the agent".
2. Overdue rows move to `timed_out` with "no result from agent within \<d\>".
3. For non-legacy rows: broadcast, policy outcome, CommandCompleted("error"),
   and an Info log `command swept`.
4. **[SEC-010]** For rows with `pre_lifecycle = true`: the transition only. No
   broadcast, no event, and no policy outcome. Each pass with at least one
   such row writes one Info log line, `legacy commands relabelled`, with
   `expired` and `timed_out` counts.

**7.4 Cancel**

Guarded `queued → cancelled`. Applied: 200 and broadcast. Not applied: 404 or
409. **[SEC-016]** If `send_ambiguous` is set, the message is "cancelled by
operator; an earlier send attempt may have reached the agent".

**7.5 Result while the WS is down**

HTTP report:

| Response | Action |
|---|---|
| 2xx | Remove the entry |
| 429 or 5xx | Back off: 5s, 10s, … up to 5m |
| 404 with no code | Legacy mode |

**7.6 Telemetry**

1. Clamp the future timestamp and log through the throttle.
2. Read `LatestSnapshotTime` (rows more than 2m in the future are ignored).
3. Store the snapshot.
4. Not stale: broadcast and run the full post-work. Stale: history only.

**7.7 Agent startup [SEC C-4, C-5]**

1. Load config and stores, and prune them.
2. Reconcile interrupted Docker markers (2m budget) and report failures.
3. `cleanupSanitizedLeftovers`.
4. Start the loops.

**7.8 Agent shutdown [SEC C-4(d)]**

1. On SIGTERM, set `shuttingDown`. New commands are refused with `failed` /
   `shutdown`.
2. Cancel the loops.
3. Wait for in-flight commands for up to 20s.
4. Exit.

## 8. Acceptance Criteria Mapping

Go tests are named `TestCL_AC0xx_*`, `TestCL_SEC0xx_*`, or `TestCL_SEC_*`.
Vitest names start with `[AC-0xx]` or `[SEC]`. Levels: U is unit, C is
in-process component, PG is PG-gated integration.

| AC | Fulfilled by | Test (file) | Level |
|---|---|---|---|
| AC-001 | auto-update params | TestCL_AC001_AutoUpdateDispatchIncludesNameAndID (handlers/auto_update_test.go) | U |
| AC-002 | resolve, name first | TestCL_AC002_ResolvesByNameOverStaleID (agent/docker_update_test.go) | U |
| AC-003 | TrimPrefix "/" | TestCL_AC003_ResolveIgnoresLeadingSlash (same) | U |
| AC-004 | resolve failure | TestCL_AC004_UnresolvedTargetFailsAtResolve (same) | U |
| AC-005 | RecordFailure path | TestCL_AC005_FailedPolicyCommandKeepsLastTriggered (commands/service_test.go) | U |
| AC-006 | RecordSuccess | TestCL_AC006_SucceededSetsLastTriggeredToTransitionTime (same) | U |
| AC-007 | AutoUpdateBackoff | TestCL_AC007_BackoffTable (models/command_policy_test.go) | U |
| AC-008 | backoff gate | TestCL_AC008_BackoffGatesDispatch (handlers/auto_update_test.go) | U |
| AC-009 | reset n | TestCL_AC009_SuccessResetsFailures (commands/service_test.go) | U |
| AC-010 | conflict check | TestCL_AC010_NoDispatchWhilePolicyCommandRunning (handlers/auto_update_test.go) | U |
| AC-011 | 000023 §5.2 | TestCL_AC011_PG_MigrationResetsContainerPoliciesOnly (db/command_repo_pg_test.go) | PG |
| AC-012 | window gate | TestCL_AC012_OutsideWindowNoDispatch (handlers/auto_update_test.go) | U |
| AC-013 | writeMu | TestCL_AC013_ConcurrentSendsNeverOverlap (agent/ws_client_test.go) | U |
| AC-014 | connMu | TestCL_AC014_ConnectDisconnectSendRaceFree (same, -race) | U |
| AC-015 | send error, outbox | TestCL_AC015_SendReturnsWriteError (ws_client_test.go); TestCL_AC015_WriteErrorKeepsOutboxEntry (outbox_test.go) | U |
| AC-016 | outbox HTTP plus endpoint | TestCL_AC016_DisconnectedResultPersistedThenPosted (outbox_test.go); TestCL_AC016_ReportEndpointRecordsTerminal (handlers/command_lifecycle_test.go) | U |
| AC-017 | backoff | TestCL_AC017_Http500BackoffThenRemovedOn2xx (outbox_test.go) | U |
| AC-018 | reload | TestCL_AC018_EntrySurvivesRestart (outbox_test.go) | U |
| AC-019 | cap | TestCL_AC019_OverflowDropsOldestWithWarning (outbox_test.go) | U |
| AC-020 | 24h prune | TestCL_AC020_EntryOlderThan24hPruned (outbox_test.go) | U |
| AC-021 | recovery | TestCL_AC021_InterruptedDockerUpdateReportedFailed (agent/command_lifecycle_test.go) | U |
| AC-022 | exclusion | TestCL_AC022_SelfUpdateMarkerNotReported (same) | U |
| AC-023 | duplicate | TestCL_AC023_DuplicateFinalIs2xxRowUnchanged (handlers/command_lifecycle_test.go); TestCL_AC023_PG_DuplicateKeepsUpdatedAt | U, PG |
| AC-024 | scoped Transition | TestCL_AC024_ForeignDeviceReportRejected (command_lifecycle_test.go); TestCL_AC024_WSForeignResultIgnored and **TestCL_AC024_WSForeignAckAndProgressIgnored** (handlers/agent_registry_test.go) | U |
| AC-025 | legacy mode | TestCL_AC025_404EntersLegacyModeRemovesAfterWSWrite (outbox_test.go) | U |
| AC-026 | sweeper | TestCL_AC026_SweeperTimesOutOverdueRunning (commands/sweeper_test.go) | U |
| AC-027 | sweeper | TestCL_AC027_SweeperLeavesInDeadlineRunning (same) | U |
| AC-028 | heartbeat claim | TestCL_AC028_HeartbeatNeverDeliversExpiredReboot (handlers/command_lifecycle_test.go) | U |
| AC-029 | TTL | TestCL_AC029_ExpiresAtPerActionClass (models/command_policy_test.go, commands/service_test.go) | U |
| AC-030 | window cap | TestCL_AC030_AutoPatchExpiryCappedAtWindowEnd (handlers/auto_update_test.go) | U |
| AC-031 | immediate sweep | TestCL_AC031_SweeperRunsBeforeFirstTick (commands/sweeper_test.go) | U |
| AC-032 | dependents | TestCL_AC032_DependentByFullParentID (agent/docker_deps_test.go) | U |
| AC-033 | dependents | TestCL_AC033_DependentByNameSlashNameService (same) | U |
| AC-034 | scope | TestCL_AC034_SingleServiceStopsOnlyItsDependents (agent/docker_update_test.go) | U |
| AC-035 | restore | TestCL_AC035_DependentOnNewParentNamespace (same) | U |
| AC-036 | restore | TestCL_AC036_DependentRestoredAfterPullFailure (same) | U |
| AC-037 | restore budget | TestCL_AC037_RestoreHasOwnBudgetAfterUpTimeout (same) | U |
| AC-038 | stopped stays | TestCL_AC038_StoppedDependentStaysStopped (same) | U |
| AC-039 | restore failure | TestCL_AC039_FailedRestoreFailsCommand (same) | U |
| AC-040 | pull stream | TestCL_AC040_PullErrorDetailFailsAtPull (same) | U |
| AC-041 | image ID | TestCL_AC041_SameImageSkipsRecreate (same) | U |
| AC-042 | rename-aside | TestCL_AC042_OldRenamedAsideRemovedAfterVerify (same) | U |
| AC-043 | rollback | TestCL_AC043_CreateFailureRollsBack (same) | U |
| AC-044 | rollback | TestCL_AC044_StartFailureRemovesReplacement (same) | U |
| AC-045 | verify | TestCL_AC045_ExitDuringVerifyRollsBack (same) | U |
| AC-046 | run state | TestCL_AC046_StoppedStaysStopped (same) | U |
| AC-047 | rollback failure | TestCL_AC047_RollbackRenameFailureKeepsAside (same) | U |
| AC-048 | leftover | TestCL_AC048_LeftoverAsideBlocksUpdate (same) | U |
| AC-049 | up limit | TestCL_AC049_UpGetsFullBudgetAfterSlowPull (same) | U |
| AC-050 | runLimited | TestCL_AC050_SigtermThenSigkillAfterGrace (agent/proclimit_test.go) | U |
| AC-051 | runLimited | TestCL_AC051_ExitWithinGraceNoSigkill (same) | U |
| AC-052 | message | TestCL_AC052_PullLimitMessage (agent/docker_update_test.go) | U |
| AC-053 | notifier | TestCL_AC053_EventAndResultAfterOpCtxExpired (agent/command_lifecycle_test.go) | U |
| AC-054 | freshness | TestCL_AC054_PulledNotRecreated (collectors/docker_freshness_test.go) | U |
| AC-055 | freshness | TestCL_AC055_RegistryNewer (same) | U |
| AC-056 | freshness | TestCL_AC056_AllMatchUpToDate (same) | U |
| AC-057 | dedupe | TestCL_AC057_SameTagDifferentVerdictsOneLookup (same) | U |
| AC-058 | pinned | TestCL_AC058_DigestRefsPinnedNoRegistryCall (same) | U |
| AC-059 | check_failed | TestCL_AC059_RegistryErrorIsCheckFailed (same) | U |
| AC-060 | negative TTL | TestCL_AC060_FailedCheckCachedFiveMinutes (same) | U |
| AC-061 | invalidation | TestCL_AC061_InvalidateOnlyInvolvedRefs (same); TestCL_AC061_DockerUpdateInvalidatesItsRefs (agent/docker_update_test.go) | U |
| AC-062 | indicator | [AC-062] (commandLifecycle.test.ts, CompactContainerTile.test.tsx) | U, C |
| AC-063 | name lock | TestCL_AC063_SecondUpdateFailsPrecheckNoDockerCalls (agent/docker_update_test.go) | U |
| AC-064 | compose lock | TestCL_AC064_StackRejectedDuringServiceUpdate (same) | U |
| AC-065 | independent keys | TestCL_AC065_DifferentStandaloneTargetsBothRun (same) | U |
| AC-066 | release | TestCL_AC066_LockReleasedAfterFailureAndPanic (target_lock_test.go, command_lifecycle_test.go) | U |
| AC-067 | seen | TestCL_AC067_SameIDViaWSAndHeartbeatRunsOnce (command_lifecycle_test.go) | U |
| AC-068 | 409 | TestCL_AC068_ConflictReturns409NoRow (handlers/command_lifecycle_test.go) | U |
| AC-069 | terminal | TestCL_AC069_TerminalPreviousAllowsNew (same) | U |
| AC-070 | host vs Docker | TestCL_AC070_RebootAcceptedSecondRebootConflicts (same) | U |
| AC-071 | bulk split | TestCL_AC071_BulkSkipsConflictingTargets (same) | U |
| AC-072 | skip | TestCL_AC072_AutoUpdateConflictSkipLogsNoBackoff (handlers/auto_update_test.go) | U |
| AC-073 | claim | TestCL_AC073_ConcurrentClaimsExactlyOne (commands/service_test.go); TestCL_AC073_PG_ClaimRace | U, PG |
| AC-074 | claim | TestCL_AC074_HeartbeatExcludesWSClaimed (handlers/command_lifecycle_test.go) | U |
| AC-075 | all paths | TestCL_AC075_AllDispatchPathsUniform (handlers/dispatch_paths_test.go) | U |
| AC-076 | CAS | TestCL_AC076_OldHandlerExitKeepsNewConn (handlers/agent_registry_test.go) | U |
| AC-077 | close previous | TestCL_AC077_NewRegistrationClosesPrevious (same) | U |
| AC-078 | backoff reset | TestCL_AC078_BackoffResetsAfterConnect (agent/ws_client_test.go) | U |
| AC-079 | envelope | TestCL_AC079_TelemetryBroadcastHasTimestamps (handlers/telemetry_order_test.go) | U |
| AC-080 | merge | [AC-080] (telemetryMerge.test.ts) | U |
| AC-081 | merge | [AC-081] (same) | U |
| AC-082 | received_at | [AC-082] (telemetryMerge.test.ts, useDevices.test.ts) | U |
| AC-083 | staleness | TestCL_AC083_OlderSnapshotHistoryOnly (handlers/telemetry_order_test.go) | U |
| AC-084 | zero timestamp | TestCL_AC084_MissingTimestampUsesReceivedAt (same) | U |
| AC-085 | table, log | TestCL_AC085_TransitionTableAllPairs (models/command_state_test.go); TestCL_AC085_RejectedTransitionLogged (commands/service_test.go) | U |
| AC-086 | immutability | TestCL_AC086_SucceededIgnoresLateFailed (service_test.go) | U |
| AC-087 | timed_out once | TestCL_AC087_TimedOutAcceptsOneLateResult (same) | U |
| AC-088 | no backward move | TestCL_AC088_AckAfterRunningIgnored (same) | U |
| AC-089 | immutable | TestCL_AC089_ReportsOnExpiredCancelledIgnoredLogged (same) | U |
| AC-090 | 409 | TestCL_AC090_DisallowedHTTPReport409 (handlers/command_lifecycle_test.go) | U |
| AC-091 | timestamps | TestCL_AC091_GetByIDHasPerStateTimestamps (same) | U |
| AC-092 | sync ack | TestCL_AC092_AckBeforeFirstStep (agent/command_lifecycle_test.go) | U |
| AC-093 | order | TestCL_AC093_ComposeProgressOrder (agent/docker_update_test.go) | U |
| AC-094 | tail | TestCL_AC094_StderrTail8KiBAndFailureStep (commands/service_test.go, models test) | U |
| AC-095 | progress | TestCL_AC095_OutOfRangeProgressStoredNull (service_test.go) | U |
| AC-096 | broadcast | TestCL_AC096_OnlyAcceptedBroadcast (same) | U |
| AC-097 | legacy | TestCL_AC097_LegacySuccessFromDispatched (same) | U |
| AC-098 | legacy | TestCL_AC098_LegacyErrorFromDispatched (same) | U |
| AC-099 | wire format | TestCL_AC099_FinalResultLegacyStatusPlusFields (agent/command_lifecycle_test.go) | U |
| AC-100 | legacy server | TestCL_AC100_NewAgentAgainstLegacyServer (agent/legacy_server_test.go) | C |
| AC-101 | mapping | TestCL_AC101_LegacyStatusesCanonical (models test, db/command_repo_test.go); TestCL_AC101_PG_LegacyRowsReadCanonical | U, PG |
| AC-102 | alias logic | TestCL_AC102_LegacySuccessTreatedAsSucceeded (commands/service_test.go) | U |
| AC-103 | expiry | TestCL_AC103_LegacyQueuedRebootExpires (commands/sweeper_test.go); TestCL_AC103_PG_MigratedLegacyQueuedExpires | U, PG |
| AC-104 | revert | TestCL_AC104_SendFailureRevertUnlessReported (commands/service_test.go) | U |
| AC-105 | cancel | TestCL_AC105_CancelledNeverDelivered (handlers/command_lifecycle_test.go) | U |
| AC-106 | cancel | TestCL_AC106_CancelDispatched409 (same) | U |
| AC-107 | race | TestCL_AC107_CancelVsClaimExactlyOne (commands/service_test.go); TestCL_AC107_PG_CancelClaimRace | U, PG |
| AC-108 | tracker | [AC-108] (ContainerDetail.test.tsx, ComposeStackSection.test.tsx, useCommandTracker.test.tsx) | C |
| AC-109 | disabled | [AC-109] (ContainerDetail.test.tsx, ComposeStackSection.test.tsx) | C |
| AC-110 | mount fetch | [AC-110] (useCommandTracker.test.tsx, ContainerDetail.test.tsx) | C |
| AC-111 | 409 attach | [AC-111] (client.test.ts, useCommandTracker.test.tsx, ComposeStackSection.test.tsx) | U, C |
| AC-112 | suppression | [AC-112] (useDevices.test.ts, ContainerDetail.test.tsx) | U, C |
| AC-113 | no suppression | [AC-113] (useDevices.test.ts) | U |
| AC-114 | labels | [AC-114] (commandLifecycle.test.ts, ContainerDetail.test.tsx) | U, C |
| AC-115 | history | [AC-115] (ActivityLog.test.tsx, commandLifecycle.test.ts) | U, C |
| AC-116 | cancel | [AC-116] (ContainerDetail.test.tsx) | C |
| AC-117 | re-fetch | [AC-117] (useCommandTracker.test.tsx) | C |

**NFR and invariant tests:**

- TestCL_NFR007_PG_SweepOver100kRowsUnder1s
- TestCL_NFR002_PG_DownMigrationRestoresLegacyValues
- TestCL_DeadlineInvariant
- TestCL_NFR004_OldContainerNeverRemovedBeforeVerify (docker_update_test.go)
- TestCL_NFR004_RecoveryNeverRemovesContainers (docker_recovery_test.go)
- `-race` runs over the WS client, outbox, locks, registry, claim, limiter,
  and throttles

### 8.1 Security condition tests (from the review; all mandatory)

| Condition | Test | File | WP | Level |
|---|---|---|---|---|
| C-1 | TestCL_SEC001_NonUUIDCommandIDRejectedWithoutQueryOrPayloadLog (subtests: HTTP path, HTTP body, WS, 1 MiB ID) | internal/server/handlers/command_lifecycle_test.go | GO | U |
| C-1 | TestCL_SEC001_StepAndMessageBounded (WS and HTTP) | same | GO | U |
| C-1 | TestCL_SEC001_SanitizeReportTable | internal/server/commands/validate_test.go | GO | U |
| C-1 | TestCL_SEC001_OutputCappedServerSide | internal/server/handlers/command_lifecycle_test.go | GO | U |
| C-1 | TestCL_SEC001_IdenticalProgressNoWriteNoBroadcast | internal/server/commands/service_test.go | GO | U |
| C-1 | TestCL_SEC001_ProgressThrottledPerCommand | same | GO | U |
| C-1 | TestCL_SEC001_ReportRateLimitedPerDevice | internal/server/handlers/command_lifecycle_test.go | GO | U |
| C-1 | TestCL_SEC001_DeviceLimiterTokenBucket | internal/server/commands/ratelimit_test.go | GO | U |
| C-1 | TestCL_SEC001_RejectionLogsThrottledPerDevice | internal/server/commands/service_test.go | GO | U |
| C-1 | TestCL_SEC001_LogThrottleSummary | internal/server/commands/logthrottle_test.go | GO | U |
| C-1 | TestCL_SEC001_Outbox429RetriesWithBackoff | internal/agent/outbox_test.go | GO | U |
| C-2 | TestCL_SEC002_ReportWithNULAndInvalidUTF8Stored | internal/server/commands/service_test.go | GO | U |
| C-2 | TestCL_SEC002_SanitizeNULAndInvalidUTF8 | internal/server/commands/validate_test.go | GO | U |
| C-2 | TestCL_SEC002_CompletionEventReasonRuneSafe | internal/server/events/generator_test.go | GO | U |
| C-2 | TestCL_SEC002_PG_NULReportPersists | internal/server/db/command_repo_pg_test.go | GO | PG |
| C-3 | TestCL_SEC003_FutureTimestampClampedToReceivedAt | internal/server/handlers/telemetry_order_test.go | GO | U |
| C-3 | TestCL_SEC003_FutureSnapshotDoesNotSuppressLaterTelemetry | same | GO | U |
| C-3 | TestCL_SEC003_BroadcastCollectedAtIsClampedValue | same | GO | U |
| C-3 | TestCL_SEC003_PG_LatestQueriesIgnoreFutureRows | internal/server/db/command_repo_pg_test.go | GO | PG |
| C-4 | TestCL_SEC004_MarkerPersistedBeforeRenameAndDependentStop | internal/agent/docker_update_test.go | GO | U |
| C-4 | TestCL_SEC004_RecoveryRenamesSetAsideBackWhenOriginalMissing | internal/agent/docker_recovery_test.go | GO | U |
| C-4 | TestCL_SEC004_RecoveryLeavesBothWhenOriginalExists | same | GO | U |
| C-4 | TestCL_SEC004_RecoveryRestartsRecordedStoppedDependents | same | GO | U |
| C-4 | TestCL_SEC004_AgentUpdateRejectedWhileDockerLockHeld | internal/agent/command_lifecycle_test.go | GO | U |
| C-4 | TestCL_SEC004_AgentUninstallRejectedWhileDockerLockHeld | same | GO | U |
| C-4 | TestCL_SEC004_NoNewCommandsAcceptedAfterSIGTERM | same | GO | U |
| C-4 | TestCL_NFR004_RecoveryNeverRemovesContainers | internal/agent/docker_recovery_test.go | GO | U |
| C-5 | TestCL_SEC005_SanitizedFileRefusesPreexistingSymlink (Linux only) | internal/agent/compose_sanitize_test.go | GO | U |
| C-5 | TestCL_SEC005_SanitizedFileMode0600 | same | GO | U |
| C-5 | TestCL_SEC005_SanitizedFileRemovedOnEveryExit (success, pull failure, up failure, panic) | same | GO | U |
| C-5 | TestCL_SEC005_StartupRemovesLeftoverSanitizedFile | same | GO | U |
| C-6 | TestCL_SEC006_StatusFilterRejectsUnknownValues (400, repository not called) | internal/server/handlers/command_lifecycle_test.go | GO | U |
| C-6 | TestCL_SEC006_FilteredQueryUsesOnlyPlaceholders | internal/server/db/command_repo_test.go | GO | U |
| C-6 | TestCL_SEC006_TargetKeyFilterIsBound (`'` and `%`; pure, plus a PG subtest) | internal/server/db/command_repo_test.go, command_repo_pg_test.go | GO | U, PG |
| C-6 | TestCL_SEC006_SQLStatusListRejectsUnknown (plus code review: builders take only `models.CommandState`) | internal/models/command_state_test.go | GO | U |
| C-7 | TestCL_SEC007_NonUUIDCommandIDNotExecuted (WS and heartbeat) | internal/agent/command_lifecycle_test.go | GO | U |
| C-7 | TestCL_SEC007_ReportURLPathEscaped | internal/agent/http_client_test.go | GO | U |
| C-8 | TestCL_SEC008_SeenPersistFailureFailsPowerActionsClosed | internal/agent/command_lifecycle_test.go | GO | U |
| C-8 | TestCL_SEC008_SeenPersistedBeforeExecute | same | GO | U |
| C-9 | TestCL_SEC011_DependentServiceNameValidatedOrSeparated | internal/agent/docker_deps_test.go | GO | U |
| C-9 | TestCL_SEC011_CleanupAndRollbackUseOldContainerID | internal/agent/docker_update_test.go | GO | U |
| C-10 | TestCL_SEC013_PGTestsSkipOnNonTestDatabase | internal/server/db/pgtest_helper_test.go | GO | U |
| C-10 | QA inspection of ci.yml: no `secrets.*`, `permissions: contents: read`, no `pull_request_target`, digest-pinned image | .github/workflows/ci.yml | GO | QA |
| C-11 | TestCL_SEC_RouteAuthMatrix | internal/server/router_test.go | GO | U |
| C-11 | TestCL_SEC_ForeignAndUnknownReportIndistinguishable (ack, progress, and result over HTTP and WS) | internal/server/handlers/command_lifecycle_test.go | GO | U |
| C-11 | TestCL_AC024_WSForeignAckAndProgressIgnored | internal/server/handlers/agent_registry_test.go | GO | U |
| C-11 | TestCL_SEC_BodyCommandIDMismatch400 | internal/server/handlers/command_lifecycle_test.go | GO | U |
| C-11 | TestCL_SEC_ReportBodyOver512KiB413 | same | GO | U |
| C-11 | TestCL_SEC_NoResurrectionEdges | internal/models/command_state_test.go | GO | U |
| C-11 | TestCL_SEC_ActiveNonQueuedRowsAlwaysHaveDeadline (plus TestCL_SEC_PG_ActiveNonQueuedRowsAlwaysHaveDeadline) | internal/server/commands/service_test.go, db/command_repo_pg_test.go | GO | U, PG |
| C-11 | TestCL_SEC_ProgressDoesNotExtendDeadline | internal/server/commands/service_test.go | GO | U |
| C-11 | TestCL_SEC_ClaimSkipsExpiredEvenIfExpireStepFails (plus a PG variant) | internal/server/commands/service_test.go, db/command_repo_pg_test.go | GO | U, PG |
| C-11 | TestCL_SEC_ReportContentNotLogged | internal/server/commands/service_test.go | GO | U |
| C-11 | TestCL_SEC_AgentStateFilesMode0600 (Linux only) | internal/agent/cmdstate_test.go | GO | U |
| C-11 | `[SEC] agent failure text renders as text`, with result_msg `<img src=x onerror=alert(1)>` | web/src/components/ContainerDetail.test.tsx, ComposeStackSection.test.tsx, ActivityLog.test.tsx | **WEB** | C |
| C-11 | QA grep: `dangerouslySetInnerHTML` absent from `web/src`, and agent strings never bound to `href` or `src` | web/src | WEB | QA |

**Recommended items adopted (§15.2):**

| Item | Test | File |
|---|---|---|
| SEC-009 | TestCL_SEC009_PG_DownMigrationFailsQueuedPowerActions | db/command_repo_pg_test.go |
| SEC-010 | TestCL_SEC010_LegacyRowsSweptSilently | commands/sweeper_test.go |
| SEC-014 | TestCL_SEC014_TerminalMessagesScopedToDevice | handlers/agent_registry_test.go |
| SEC-016 | TestCL_SEC016_AmbiguousSendMessages | commands/service_test.go |

## 9. Error Handling

| Failure | Handling | HTTP / WS | Log |
|---|---|---|---|
| Conflict on create | No row | 409 `conflict` | Info `command conflict`. Auto-update logs `auto-dispatch conflict skip` |
| WS send fails after claim | Revert to queued, `send_ambiguous` | 201 `queued` | Warn |
| Revert rejected | Keep state | none | Warn `command transition rejected` (throttled) |
| Disallowed agent report | Unchanged, no broadcast | WS receipt `invalid_transition`; HTTP 409 | Warn (throttled) |
| Foreign or unknown command | Unchanged | 404 `command_not_found` / receipt `not_found` | Warn (throttled) |
| **[SEC] Non-UUID command_id** | No query | HTTP 400 `invalid_report`; WS dropped | Warn `malformed command_id from agent` (device_id, length; throttled) |
| **[SEC] Over the device rate limit** | Not processed | HTTP 429 `rate_limited`; WS dropped | Warn `agent report rate limited` (throttled) |
| **[SEC] Identical or too-frequent progress** | No write, no broadcast | 200 or receipt `coalesced` | none |
| **[SEC] NUL or invalid UTF-8 in content** | Sanitized and stored | 200 | none |
| **[SEC] Unknown status filter** | No query | 400 `invalid_filter` | none |
| DB error in a report | No receipt | 500 | Error |
| Cancel after dispatch | Unchanged | 409 `not_cancellable` | Info |
| Sweeper DB error | Retried next tick | none | Error |
| Stale telemetry | History only | 200 | Debug |
| **[SEC] Future telemetry timestamp** | Clamped to received_at | 200 | Warn `telemetry timestamp clamped` (throttled) |
| Outbox persistence failure | In-memory mode | none | Error |
| Outbox drop | Oldest entry dropped | none | Warn `outbox entry dropped` |
| **[SEC] Seen-store persistence failure** | Power and patch actions fail closed; others run degraded | result `failed`/`precheck`/`persistence` | Error |
| **[SEC] Marker persistence failure (Docker)** | Abort before the next mutation, then roll back or restore | result `failed`/`persistence` | Error |
| **[SEC] Command received while shutting down** | Not executed | result `failed`/`precheck`/`shutdown` | Info |
| **[SEC] Interrupted update at startup** | Reconciled; failure reported with summary | result | Warn `interrupted docker update reconciled` |
| **[SEC] Sanitized path occupied by a foreign entry** | Step fails | result `failed`/`pull` | Warn |
| Step limit | `failed`, "timed out after …" | result | Warn |
| Rollback / failed rollback / failed restore / panic | As in AD-014 | result | Warn / Error |

## 10. Security Considerations

The full review, with its threat model and the conditions it sets, is
`docs/security/CMD-LIFECYCLE-001-security-review.md`. Conditions are indexed in
§15.

1. **Report authorization.**
   - Auth: `deviceAuth`, plus mTLS when enabled.
   - Ownership: `device_id` is enforced in the same SQL statement as the write,
     so there is no TOCTOU gap.
   - Foreign and missing commands return identical 404s and receipts.
   - The body ID must equal the path ID.
   - Rate limit: per device (AD-020).
   - Validation: the UUID check and the field rules run before any DB access
     (C-1).
2. **WS scoping.** The socket's device ID scopes every report. Terminal
   sessions are bound to their device (SEC-014).
3. **Replay and double execution.**
   - Server: immutable terminal states and atomic claims; expired and cancelled
     commands are never delivered.
   - Agent: durable seen IDs, failing closed for power and patch actions (C-8).
   - Agent: UUID validation at intake (C-7).
4. **Rollback safety.** The down migration fails all queued power actions
   (SEC-009) and maps cancelled and expired rows to `error`.
5. **Stored text.** Content is sanitized, NUL-free, valid UTF-8, and bounded
   (C-1, C-2). Logs record lengths only, never content. The UI renders agent
   text as text only (C-11).
6. **Agent files.**
   - Mode 0600, written with exclusive-create temp files and rename.
   - Fixed paths.
   - No new privileges.
   - The sanitized compose file is created exclusively, without following
     symlinks, mode 0600, and is always removed (C-5).
7. **Docker inputs.**
   - No shell.
   - `compose_work_dir` must be absolute and must match a real label.
   - Service names are validated and come after `--`.
   - The old container is removed only by its ID, and only after verify (C-9).
   - Crash-safe markers and startup reconciliation (C-4).
8. **Telemetry integrity.** Future timestamps are clamped, and "latest" queries
   ignore implausible future rows (C-3).
9. **Injection.** Typed literal builders. Request filters are allowlisted and
   bound (C-6).
10. **CI.** Throwaway credentials, read-only permissions, and a test-database
    guard (C-10).
11. **Not adopted in this story:** two deferred hardening items (SEC-012 and
    SEC-015) are tracked separately as follow-up work.

## 11. Performance Considerations

- **Indexes.** Only the three partial indexes in §5.1.
- **Sweeper.** Bounded at 500 + 500 rows per pass. NFR-007 has a PG test.
- **Claim and create.** One statement each. The advisory lock is per device.
- **Telemetry.** One extra index probe. The "latest" queries gain a
  `timestamp <=` bound that the existing `(device_id, timestamp DESC)` index
  serves.
- **[SEC] Limiter and throttles.**
  - Memory is O(devices + active commands), a few hundred bytes per entry,
    evicted lazily.
  - The progress gate cuts write and broadcast volume to at most one per
    command per second. That reduces dead tuples on `commands`, which matters
    on deployments that already show table bloat (PART-001).
- **WS fan-out.** About 10 `command_update` messages per update. The first
  legacy sweep is silent (SEC-010), so it does not flood the 256-slot hub.
- **Dashboard.** Polls only after 5s with no push. One active-commands fetch is
  shared per device.
- **Agent.** Whole-file rewrites of at most 1 MiB. Marker writes happen at most
  about 6 times per update. Each fsync costs milliseconds.

## 12. Implementation Notes for Engineers

1. **Typed literals.** Use `models.SQLStatusList` only with typed constants.
   Request filters go through `parseStatusFilter` and `ANY($n)` (C-6).
2. **Typed-nil trap.** Never pass nil `*websocket.Hub` or `*events.Generator`
   values as interfaces.
3. **No raw clock calls.** No `time.Now()` or `time.Since()` in testable logic.
   Clocks are injected: `Service.Options.Now`, `Handlers.now`, the agent clocks,
   the limiter and throttle clocks, and the vitest fake timers.
4. **Remove the unguarded methods.** `UpdateStatus`, `UpdateCommandResult`,
   `ListPending`, and `SetLastTriggered` must be gone. No shims.
5. **Validation order.** On HTTP and WS reports, validate in this order:
   1. limiter;
   2. UUID;
   3. size;
   4. decode;
   5. ID match;
   6. sanitize;
   7. service.

   **A malformed ID must never reach `slog` attributes.** Log its length only.
6. **Sanitize before every write and broadcast.** `models.SanitizeText` runs
   before any DB write and before any broadcast, including the server's own
   truncation of the event reason (C-2).
7. **Bulk.** Bulk keeps children-first ordering and does a single invalidation
   and telemetry trigger at the end. Its marker holds one record per container.
8. **Sanitized compose file.** The pull budget covers the sanitize retry, which
   creates the file through `writeSanitizedCompose`.
9. **Marker writes before mutations (C-4).** A Docker mutation may run only
   after its marker write returns nil. Tests assert this ordering through the
   fake Docker call log, interleaved with the fake filesystem's write log.
10. **Shutdown grace.** 20s. Do not change install.sh. Correctness relies on
    startup reconciliation.
11. **Notifier.** Bounded at 64. Coalesce percent-only progress to 1 per second
    per command. Lifecycle events block for at most 1s while enqueueing.
12. **Existing tests.** Server-side assertions on legacy statuses move to
    canonical values. Agent `commandExecResult.Status` stays
    `success`/`error`.
13. **Out of scope.** Do not touch `runUpgradeCommand` or the PATCH-GATE
    files. No collector change. No install.sh change.
14. **PG tests.** Call `pgTestURL(t)` first, which guards against non-test
    databases (C-10). Use a unique schema and always drop it.
15. **Web.**
    - No `any` in new code. Use `scrollbar-thin`. No global styles.
    - Never `dangerouslySetInnerHTML`.
    - `title` attributes receive only enum-derived strings.
    - ActivityLog's status filter sends only the canonical values from
      `commandLifecycle.ts`, because the server now returns 400 for unknown
      values.
16. **Windows dev.** Run `go test ./...` without `-race`. PG tests skip.
    Linux-only SEC tests use `t.Skip` on Windows.
17. **compose `--`.** Confirm that the installed compose v2 accepts `--` before
    service names. Record the result in the implementation report (AD-014).
18. **Release.** Do not tag or push.

## 13. Definition of Done

- [ ] All component changes in §4 are implemented, and no other files changed.
- [ ] Every AC row in §8 and **every condition row in §8.1** has a test with
      the exact name, and it passes.
- [ ] `go build ./...`, `go vet ./...`, and `go test ./...` pass on Windows.
      CI `go test -race -count=1 ./...` passes, including PG-gated tests
      against the `postgres:16` service.
- [ ] `cd web && npx tsc --noEmit && npm run test:run` passes.
- [ ] Migration 000023 runs up, down, then up cleanly. The NFR-002 and SEC-009
      assertions pass.
- [ ] TestCL_DeadlineInvariant passes.
- [ ] No new Go or npm dependency. `go.mod`, `go.sum`, `web/package.json`, and
      `web/package-lock.json` are byte-identical to the base.
- [ ] No unguarded command status write. A grep for `UPDATE commands SET status`
      finds only `Transition`, the claim methods, and the migrations.
- [ ] No `time.Now()` or `time.Since()` in `commands/`, `outbox.go`,
      `target_lock.go`, `proclimit.go`, `command_lifecycle.go`,
      `docker_recovery.go`, the freshness logic, `telemetryMerge.ts`, or
      `useCommandTracker.ts` (composition-root defaults only).
- [ ] The NFR-013 slog messages exist with the specified fields.
- [ ] **Security conditions:**
  - [ ] **C-1:** The report bounds constants exist in `models/command_policy.go`
        with the §15 values. The `DeviceLimiter` defaults are 120/min with a
        burst of 60, shared by HTTP and WS. The progress gate allows 1/s per
        command. The log throttle allows 10/min per device and then emits a
        summary.
  - [ ] **C-2:** No agent-derived string reaches the DB or a broadcast without
        `SanitizeText`. `CommandCompleted` is rune-safe.
  - [ ] **C-3:** The 2m clamp is applied to the stored and broadcast value.
        "Latest" queries ignore future rows.
  - [ ] **C-4:** Marker writes come before every Docker mutation. Startup
        reconciliation is in place. `agent_update` and `agent_uninstall` are
        refused while a `docker:*` lock is held. The SIGTERM gate and the 20s
        wait are in place.
  - [ ] **C-5:** The sanitized compose file is exclusive-create, 0600, removed
        on every exit, and cleared at startup.
  - [ ] **C-6:** The typed `SQLStatusList` is in place. `parseStatusFilter`
        rejects unknown values with 400. Filters are bound.
  - [ ] **C-7:** Command IDs are UUID-validated at agent intake. Report URLs
        are path-escaped.
  - [ ] **C-8:** Seen IDs are fsynced before execution. Dedupe-critical
        actions fail closed.
  - [ ] **C-9:** `composeArgs` validates service names and uses `--`.
        `--project-directory` takes an absolute path. The old container is
        removed and restored only by ID.
  - [ ] **C-10:** ci.yml has throwaway credentials, `permissions: contents:
        read`, no `pull_request_target`, and a digest-pinned image. The PG-test
        guard is in place.
  - [ ] **C-11:** The auth matrix test, the indistinguishability test, and the
        `[SEC]` vitest case pass. The QA grep for `dangerouslySetInnerHTML`
        finds nothing.
- [ ] The adopted recommendations (SEC-009, 010, 014, 016) are implemented and
      tested.
- [ ] The implementation report records:
  - the migration-number check against `fix/pg-autovacuum-bloat` and
    `part-001-partitioning` (§16);
  - the postgres image digest;
  - the result of the compose `--` check.

### 13.1 Compatibility matrix (NFR-001)

| Agent \ Server | Old server | New server |
|---|---|---|
| **Old agent** | Unchanged | **Works.** The server sends both name and ID. The old agent never acks, so the command stays `dispatched` until its result is accepted (FR-080). The old agent ignores `command_receipt`. Old-agent defects remain until the agent is upgraded. It sends only UUID IDs and stays well under the report rate limit |
| **New agent** | **Works.** The old server ignores ack and progress, accepts `command_result`, and sends no receipt. The agent's HTTP report gets a 404 with no code, so it enters legacy mode. Dedupe absorbs double delivery. The old server's `{"container": name}` payload now works. The old server sends UUIDs, so they pass C-7 | Full lifecycle |

### 13.2 Suggested commit sequence

**WP-GO:**

1. `[CMD-LIFECYCLE-001] feat: canonical command state machine and lifecycle policy tables`
2. `[CMD-LIFECYCLE-001] migration: 000023 command lifecycle columns, legacy mapping, policy backoff, container cooldown reset`
3. `[CMD-LIFECYCLE-001] feat: guarded command repository (claim, transition, conflict, bound filters) and faithful mocks`
4. `[CMD-LIFECYCLE-001] feat: command service with dispatch, agent reports, cancel and sweeper`
5. `[CMD-LIFECYCLE-001] fix: validate, sanitize, coalesce and rate-limit agent reports`
6. `[CMD-LIFECYCLE-001] refactor: route all command creation paths through the command service`
7. `[CMD-LIFECYCLE-001] fix: agent connection registry compare-and-delete, device-scoped WS reports and terminal sessions`
8. `[CMD-LIFECYCLE-001] feat: command get, cancel and agent report endpoints`
9. `[CMD-LIFECYCLE-001] fix: auto-update targeting, success-only cooldown and failure backoff`
10. `[CMD-LIFECYCLE-001] fix: telemetry ordering with clamped timestamps and broadcast timestamps`
11. `[CMD-LIFECYCLE-001] fix: rune-safe and NUL-free command completion events`
12. `[CMD-LIFECYCLE-001] fix: serialize agent WS writes and reset reconnect backoff`
13. `[CMD-LIFECYCLE-001] feat: agent durable result outbox, seen-ID store and in-flight markers`
14. `[CMD-LIFECYCLE-001] feat: agent command lifecycle with ack, progress, target locks and shutdown gate`
15. `[CMD-LIFECYCLE-001] fix: separate step limits with SIGTERM grace`
16. `[CMD-LIFECYCLE-001] fix: restore network dependents on every update outcome`
17. `[CMD-LIFECYCLE-001] fix: standalone update pull-error detection and rename-aside rollback`
18. `[CMD-LIFECYCLE-001] fix: crash-safe docker update markers and startup reconciliation`
19. `[CMD-LIFECYCLE-001] fix: exclusive 0600 sanitized compose file and hardened compose argv`
20. `[CMD-LIFECYCLE-001] fix: per-container image freshness with negative TTL and per-ref invalidation`
21. `[CMD-LIFECYCLE-001] test: PG-gated repository and migration tests; CI postgres service`

**WP-WEB:**

22. `[CMD-LIFECYCLE-001] feat: command lifecycle types, API client and pure helpers`
23. `[CMD-LIFECYCLE-001] fix: drop stale live telemetry using server timestamps`
24. `[CMD-LIFECYCLE-001] feat: useCommandTracker and exited suppression during tracked updates`
25. `[CMD-LIFECYCLE-001] feat: track updates by command ID in ContainerDetail and ComposeStackSection`
26. `[CMD-LIFECYCLE-001] fix: canonical command labels in history and update-reason titles`

### 13.3 Work packages

- **WP-GO** covers §4.1 and §4.2. That includes the migration, `ci.yml`, and
  every C-1 to C-10 component, plus the Go side of C-11.
- **WP-WEB** covers §4.3 only. It codes against the frozen §6.1, §6.2, §6.5,
  and §6.7. The rev-2 tags in those sections are additive error cases only, so
  they need no WP-WEB code change. WP-WEB owns the C-11 `[SEC]` vitest case.
- **No shared files.** Integration happens only through the contract.

## 14. FRD deviations and resolutions

| # | FRD item | Resolution | Why |
|---|---|---|---|
| 1 | §7.3 bulk deadline | `min(35m×N + 5m, 6h)` plus an agent budget | The invariant cannot hold at N=1 |
| 2 | §7.3 os_patch | 40m | Keeps the invariant strictly true |
| 3 | §7.3 docker_update 35m | Kept. The agent budget is enforced at 29m55s | AC-026 |
| 4 | FR-020 vs FR-072 | 200 duplicate / 409 `already_terminal` | Honors both |
| 5 | FR-063 "four paths" | All seven paths | FR-025 |
| 6 | FR-059 host target | Adds shutdown and enable_auto_updates | Same hazard |
| 7 | FR-054 lock scope | Update and host actions only. stop/start/restart are refused while a target is locked | UX |
| 8 | D-014 key | Uses the working dir | Unambiguous |
| 9 | FR-019 exclusions | Adds shutdown | Same rationale as reboot |
| 10 | D-016 | Best-effort HTTP fallback for ack and progress | Heartbeat-delivered commands get progress |
| 11 | FR-079 | `command_update` replaces `command_result` | AC-096 |
| 12 | Compose verify | New verify step, no rollback | AC-093 |
| 13 | BR-007 | Dependents handling also covers standalone parents | Business rule |
| 14 | FR-052 | `docker_check_updates` still clears the whole cache | Explicit user request |
| 15 | FR-017 vs NFR-008 | The 1 MiB total is binding | Both hold |
| 16 | FR-090/095 | "Running…" and "Done" for non-update actions | Honest labels |
| 17 | FR-008 down migration | Not reversible | By design |
| 18 | **[SEC]** FR-078 progress | `running → running` is coalesced to at most 1/s per command, and identical reports are dropped. Out-of-range percent is still dropped | C-1. Progress loss is cosmetic (D-016) |
| 19 | **[SEC]** FR-069 | `collected_at` is clamped to at most received_at + 2m | C-3 |
| 20 | **[SEC]** D-013 on the agent | `agent_update` and `agent_uninstall` are refused while a Docker update holds a lock, and the reverse. The server conflict model is unchanged | C-4(c) |

## 15. Security addendum

The security review (`docs/security/CMD-LIFECYCLE-001-security-review.md`)
returned APPROVED WITH CONDITIONS. Each condition below lands in exactly one
component. Tests are in §8.1. Every item belongs to **WP-GO**, except the
`[SEC]` vitest case, which is **WP-WEB**.

### 15.1 Mandatory conditions

| # | Finding | Lands in (file :: function or type) | Constants and defaults | AD |
|---|---|---|---|---|
| **C-1** | SEC-001: report bounds and throttling | `internal/server/commands/validate.go` :: `validateReportIDs`, `sanitizeReport`. `internal/server/commands/ratelimit.go` :: `DeviceLimiter` (`Allow(deviceID)`), reached via `Service.AllowReport`, called from `handlers/commands.go` :: `AgentCommandReport` (before decode) and `handlers/terminal.go` :: `HandleAgentWS` (per `command_*` message). `internal/server/commands/service.go` :: progress gate in `HandleAgentReport`. `internal/server/commands/logthrottle.go` :: `LogThrottle`. `internal/agent/outbox.go` :: 429 treated as retry | `models`: `MaxReportStepBytes = 64`, `StepPattern = ^[a-z0-9_]{1,64}$` (invalid ⇒ stored `""` on both paths), `MaxProgressMessageBytes = 1024`, `MaxResultMsgBytes = 65536`, `MaxStderrTailBytes = 8192`, `MaxReportOutputBytes = 262144`, `ReportReasons` enum. `commands`: `ReportRatePerMinute = 120`, `ReportBurst = 60`, `ProgressMinInterval = 1s`, `RejectionLogsPerMinute = 10`, `LimiterIdleEviction = 10m`. HTTP body cap 512 KiB. WS read limit 4 MiB | AD-008, AD-009, AD-010, AD-020 |
| **C-2** | SEC-002: NUL and invalid UTF-8 | `internal/models/command_policy.go` :: `SanitizeText`, rune-safe `TruncateTail`/`TruncateHead`/`TruncateHeadTail`, applied in `commands/validate.go` :: `sanitizeReport` before every write and broadcast. `internal/server/events/generator.go` :: `CommandCompleted` (rune-safe reason) | NUL is stripped and invalid sequences become U+FFFD. Content problems never produce a 500 | AD-008 |
| **C-3** | SEC-003: future telemetry timestamp | `internal/server/handlers/handlers.go` :: `Telemetry` (clamp before the staleness check; throttled clamp log). `internal/server/db/telemetry_repo.go` :: the six "latest" queries | `models.TelemetryFutureTolerance = 2m` | AD-018 |
| **C-4** | SEC-004: crash-safe update | (a) `internal/agent/docker_update.go` and `docker_deps.go` :: marker writes through `cmdstate.go` :: `inflightStore.UpdateDocker`. (b) `internal/agent/docker_recovery.go` :: `reconcileInterrupted`, driven by `command_lifecycle.go` :: startup recovery. (c) `internal/agent/target_lock.go` :: `HeldWithPrefix`, checked in `commands.go` :: `executeAction` for `agent_update` and `agent_uninstall`, plus the reverse check in `docker_update.go`. (d) `internal/agent/agent.go` :: `Run` shutdown with `shuttingDown`, `cmdWG`, and `submitCommand`'s gate | `models.AgentShutdownGrace = 20s` (systemd default is 90s; install.sh unchanged). Recovery budget 2m. Reason values `shutdown` and `persistence` | AD-011, AD-012, AD-014 |
| **C-5** | SEC-005: sanitized compose file | `internal/agent/compose_sanitize.go` :: `writeSanitizedCompose`, `removeSanitized`. `fileowner_unix.go` / `fileowner_windows.go` :: `ownedByEUID`. `docker_recovery.go` :: `cleanupSanitizedLeftovers` | Location stays `workDir/.compose.riot-sanitized.yml` (relative-path semantics). `O_CREATE\|O_EXCL`, mode 0600 | AD-014 |
| **C-6** | SEC-006: filter injection | `internal/models/command_state.go` :: typed `SQLStatusList`, `ParseStatusFilterValue`. `internal/server/handlers/commands.go` :: `parseStatusFilter` (400 `invalid_filter`). `internal/server/db/command_repo.go` :: `buildFilteredQuery`, `buildActiveQuery` (bound `ANY`) | `MaxTargetKeyFilterBytes = 512` | AD-001, AD-003 |
| **C-7** | SEC-007: agent ID intake | `internal/agent/command_lifecycle.go` :: `submitCommand` (`models.IsCanonicalUUID`). `internal/agent/http_client.go` :: `PostCommandReport` (`url.PathEscape`) | Canonical lowercase UUID | AD-010, AD-011 |
| **C-8** | SEC-008: dedupe fail-closed | `internal/agent/cmdstate.go` :: `atomicJSONFile` (file and dir fsync), `seenStore.CheckAndAdd`. `command_lifecycle.go` :: intake step 2 | `models.DedupeCriticalActions = {reboot, shutdown, os_update, agent_update}` | AD-011 |
| **C-9** | SEC-011: argv and removal by ID | `internal/agent/docker_update.go` :: `composeArgs`, standalone cleanup and rollback (by `old_id` and `replacement_id`). `docker_deps.go` :: restore through `composeArgs` | Service charset `^[a-zA-Z0-9][a-zA-Z0-9_.-]*$` plus `--`. Single-token `--project-directory=<abs>` | AD-014 |
| **C-10** | SEC-013: CI | `.github/workflows/ci.yml`. `internal/server/db/pgtest_helper_test.go` :: `pgTestAllowed`, `pgTestURL` | `riot_test` literal credentials, `permissions: contents: read`, digest-pinned `postgres:16`. Guard: DB name contains `test`, or `RIOT_TEST_PG_ALLOW=1` | AD-017 |
| **C-11** | Verify the designed controls | `internal/server/router.go` :: `registerCommandRoutes` (testable auth matrix). The tests listed in §8.1. WP-WEB: the `[SEC]` text-rendering case in three component tests | none | AD-008, AD-009, AD-019 |

### 15.2 Recommended items: decisions

| Item | Decision | Where |
|---|---|---|
| **SEC-009** (down migration and queued power actions) | **Adopted.** The down migration fails every queued `reboot` and `shutdown`, and every queued `os_update` with `include_reboot_class`, regardless of expiry. Result: `error` with "not delivered: power action dropped on downgrade" | `000023_command_lifecycle.down.sql` step 1 (§5.3). Test TestCL_SEC009_PG_DownMigrationFailsQueuedPowerActions |
| **SEC-010** (first-sweep fan-out) | **Adopted: silent relabel plus one log line.** Pre-existing rows are marked `pre_lifecycle = true` in the migration (fast default, no rewrite). The sweeper and the heartbeat expire step transition these rows **without** broadcasts, CommandCompleted events, or policy outcomes. Each pass writes one Info log line, `legacy commands relabelled`, with counts. No summary event, so nothing reaches notification channels. A release note covering FR-008 first activation (per-container auto-updates firing for the first time) goes to the technical writer | `sweeper.go`, `service.go` :: `ClaimForHeartbeat`; migration §5.1. Test TestCL_SEC010_LegacyRowsSweptSilently |
| **SEC-016** (ambiguous sends) | **Adopted: honest wording, no contract change.** The FR-085 revert sets `send_ambiguous = true` and `result_msg` "ws send failed; delivery uncertain; re-queued for heartbeat delivery". If that row later expires: "expired: delivery unconfirmed within \<TTL\>; an earlier send attempt may have reached the agent". If it is cancelled: "cancelled by operator; an earlier send attempt may have reached the agent". A late agent report on such a row logs an unthrottled Warn, `agent reported on command after ambiguous send` (command_id, from, to). The FR-095 UI labels are unchanged; the nuance appears in the history Summary (`result_msg`) | `service.go` (revert, cancel), `sweeper.go`, migration column. Test TestCL_SEC016_AmbiguousSendMessages |
| SEC-014 (terminal session scoping) | **Adopted.** It is cheap, because `terminal.go` is being restructured anyway | `handlers/terminal.go`. Test TestCL_SEC014_TerminalMessagesScopedToDevice |
| SEC-012, SEC-015 | **Deferred.** Hardening items tracked separately as follow-up work | §10.11 |

### 15.3 Frozen-contract impact

No condition changes a shape that WP-WEB consumes. The rev-2 contract
additions, all marked **[SEC]** in §6, are:

- the agent report's 429 and 400 cases, and the `coalesced` result value
  (agent-facing only);
- 400 `invalid_filter` on the list endpoint (WP-WEB sends only allowlisted
  values);
- `step` may be `""` (already rendered as "Working");
- `collected_at` is clamped (same field);
- `command_update` is not emitted for silently relabelled legacy rows.

## 16. Risks, open questions, and notes for the caller

**Open risks**

- **Migration number collision.** `fix/pg-autovacuum-bloat` (000023) and
  `part-001-partitioning` (000024-000028) are unmerged. Whichever branch merges
  second must renumber above the current maximum. Check the target
  database's `schema_migrations` table before deploying.
- **Merge conflicts with PART-001.** Expected in `telemetry_repo.go` (rev 2
  adds future-row bounds there as well) and in `ci.yml`.
- **Old agents** keep their own defects until upgraded.
- **First sweep.** Relabels historical `sent` rows as `timed_out`, silently
  (OQ-005, SEC-010).
- **FR-008 reset burst** (OQ-003). Release note required.
- **Cleanup-failed set-aside container.** Blocks updates of that container
  until it is removed manually.
- **Shutdown cut-off.** A docker_update in progress at SIGTERM can be cut off
  after 20s. Recovery reconciles it on restart, but the container may be down
  until the agent comes back.
- **Deferred hardening items** (SEC-012 and SEC-015) are tracked separately.

**Open questions for the product owner** (none blocking)

- FRD OQ-001 to OQ-006 still stand.
- **Q-A:** Should two single-service updates in the same compose project
  conflict?
- **Q-B:** Is the 40m OS-patch deadline acceptable?
- **Q-C:** Should the 120/min, burst-60 report limit stay a code constant? It
  is not configurable, per FRD §9.
