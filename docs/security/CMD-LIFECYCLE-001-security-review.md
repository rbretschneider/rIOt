# Security Review Report

**Story ID:** CMD-LIFECYCLE-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-17
**Verdict:** APPROVED WITH CONDITIONS

There are no CRITICAL findings and no design-level blockers. The core security design is
sound:

- every agent write is scoped by `device_id` in SQL;
- foreign and missing commands get the same 404;
- terminal states are immutable;
- delivery claims are atomic single-row guarded `UPDATE`s;
- the queued TTL is enforced inside the claim predicate itself;
- the down migration never re-arms a cancelled or expired row;
- `compose_work_dir` is now checked against real container labels.

The pre-existing cross-device `command_result` injection (`terminal.go:169`) is confirmed. The
AD-009 design closes it for `command_ack`, `command_progress`, and `command_result`.

There is one HIGH finding, **SEC-001**. The ADD bounds the *result* fields (`stderr` 8 KiB and
`result_msg` 64 KiB) but leaves the rest of the agent-to-server report surface unbounded and
unthrottled:

- `command_id`, `step`, the progress `message`, and `output` have no bound;
- the report rate has no limit;
- the new Warn-level rejection logs have no limit, and those logs are **persisted to Postgres**
  by `logstore.DBHandler`.

As a result, one device credential can exhaust the server's database disk or memory. The
resolution is a set of concrete validation and rate rules, so it is a condition, not a
revision. Five MEDIUM findings and ten LOW/INFO findings follow. All MEDIUM findings and the
LOW findings marked "mandatory" are implementation conditions listed under "Mandatory
conditions".

**Read in full:**
- `docs/requirements/CMD-LIFECYCLE-001-frd.md` (§3, §7.2, §7.3, NFRs, decisions)
- `docs/architecture/CMD-LIFECYCLE-001-add.md` (all sections)
- the grounding notes

**Code inspected at `main@a7b9f9e`:**
- `internal/server/router.go`
- `internal/server/middleware/{auth,admin_auth,mtls_auth,ratelimit}.go`
- `internal/server/handlers/{commands,terminal,auto_update}.go` and `handlers.go`
  (heartbeat and DeleteDevice)
- `internal/server/websocket/hub.go`
- `internal/server/server.go` (logger wiring)
- `internal/server/logstore/handler.go`
- `internal/server/server_error_alert_worker.go`
- `internal/server/events/generator.go` (`CommandCompleted`)
- `internal/agent/{ws_client,commands,docker_update,heartbeat,agent,config}.go`
- `internal/models/automation.go`
- `scripts/install.sh` (service user, docker group, sudoers, systemd unit)
- `.github/workflows/ci.yml`
- migrations `000001` and `000017`
- `web/src` (searched for raw-HTML sinks)

---

## Threat Model Summary

### Assets

| Asset | Where | Why it matters |
|---|---|---|
| Command state integrity | `commands` rows (status, timestamps, target_keys) | The state machine is now a **safety control**. It decides whether a reboot, shutdown, os_update or docker_update is delivered, whether a target is locked (409), and whether auto-update backs off |
| Host and workload availability | Agent hosts and their containers | A stale reboot, a double execution, or a half-finished recreate takes services down |
| Server availability | Postgres (`commands`, `command_output`, `server_logs`), the hub broadcast channel, server memory | One shared server for the whole fleet. Deployments with an existing Postgres bloat problem are especially exposed (PART-001) |
| Agent-reported text | `stderr_tail`, `result_msg`, `step`, `command_output`, `command_update` broadcasts | It may contain secrets (it is subprocess output), and it is rendered in the admin dashboard |
| Device credentials | `X-rIOt-Key`, the mTLS client cert | They authorize the new report endpoint |
| Agent local state | `/var/lib/riot/{command-outbox,seen-commands,inflight-commands}.json` (riot:riot) | Dedupe is the only barrier against double execution across a reboot |

### Threat Actors

1. **A compromised or buggy enrolled device (a valid `X-rIOt-Key`).** This is the primary
   actor for this story. It wants to change another device's commands (IDOR), resurrect or
   suppress commands, or degrade the server for the whole fleet. Devices are less trusted than
   the server. One compromised homelab host must not be able to take down fleet monitoring.
2. **A network attacker on an `http://` deployment.** It can capture and replay reports. It
   is mitigated by immutability; key capture is pre-existing.
3. **A compromised server or a MITM against the agent.** It controls command payloads,
   including `command_id`, `container`, and `compose_work_dir`. This story hardens it
   (`compose_work_dir` validation). A compromised server already controls the agent's command
   set by design.
4. **A local user on an agent host** who owns a compose working directory but is not in the
   docker group (SEC-005).
5. **The admin operator.** Trusted. Relevant only for the injection-shaped API surface
   (SEC-006).
6. **CI and the supply chain.** The new Postgres service container (SEC-013).

### Attack Surface Introduced

- `POST /api/v1/devices/{id}/commands/{commandId}/report` (deviceAuth, plus mTLS when enabled):
  a new agent-writable endpoint that accepts `ack`, `progress`, and `result`.
- New agent→server WS message types, `command_ack` and `command_progress`, and an extended
  `command_result`. The new server→agent message is `command_receipt`.
- Admin endpoints `GET /commands/{commandId}` and `POST /commands/{commandId}/cancel`, plus
  `active` and `target_key` filters on the list endpoint.
- A new `command_update` dashboard broadcast that carries agent-supplied `step` and `message`.
- Agent persisted state files: outbox, seen IDs, and in-flight markers.
- Agent Docker changes:
  - rename-aside recreate and rollback;
  - dependent `docker compose ... up --force-recreate <svc>` invocations;
  - name-based resolution;
  - the retained `sanitizeComposeFile`.
- Server telemetry ordering, which trusts the agent-supplied `snap.Timestamp`.
- Migration 000023, which rewrites statuses, backfills data, and resets `last_triggered_at`.
  The first sweep relabels historical rows.
- A `postgres:16` service in CI.

---

## Findings

### CRITICAL

None.

---

### HIGH

#### SEC-001: The agent report surface is unbounded and unthrottled. One device credential can exhaust server DB disk, `server_logs`, and memory

**Severity:** HIGH
**Domain:** Input validation / Rate limiting / DoS
**Location:**
- ADD AD-008 (only `stderr` and `result_msg` are bounded), AD-009 (4 MiB WS read limit),
  §6.3 (512 KiB HTTP cap), §6.4, §6.5, §9 (Warn logs on every rejection), §10.11
- `internal/server/logstore/handler.go` (Warn+ records, with all attrs, are persisted to
  Postgres)
- `internal/server/websocket/hub.go` (256-slot broadcast channel of marshalled messages)

**Description.**
The ADD treats the report endpoint as low-risk because "it is device-authenticated and touches
only the device's own rows" (§10.11). That holds for *integrity*. It does not hold for
*availability*, because:

- `command_id` (from the WS body or the URL path), `step`, the progress `message`, `reason`,
  and `output` have no length or charset bound in the ADD. Only `stderr_tail` (8 KiB) and
  `result_msg` (64 KiB) are truncated. A WS frame can be 4 MiB.
- Every rejected or unknown report writes a Warn log with `command_id`. The log entry includes
  "command report for unknown or foreign command" with the agent-supplied `command_id` as an
  attr. `server.go` installs `logstore.NewDBHandler(…, LevelWarn)`, so **every one of those
  records, attrs included, becomes a row in `server_logs`**.
- `running → running` is a legal, repeatable transition. Each accepted progress report does a
  row `UPDATE` (a dead tuple, plus TOAST for long values) and a `command_update` broadcast to
  every dashboard socket. The hub queues up to 256 marshalled messages.
- There is no rate limit on the HTTP endpoint and none on agent WS messages. The existing
  limiters are per-IP, apply only to login and register, and are not used here.
- `output` is saved to `command_output` on acceptance. The server applies no cap beyond the
  transport limit (4 MiB on WS).

**Attack Scenario.**
The actor is any enrolled device whose agent is compromised, or an agent stuck in a bug loop.

1. It opens `/ws/agent` with its own valid key.
2. It sends `command_ack` frames in a tight loop, each with a random 4 MiB `command_id`. Each
   one fails classification, returns `not_found`, and writes a Warn log with the 4 MiB attr.
   That becomes a 4 MiB `server_logs` row. At LAN speed this is hundreds of MB per second
   into the shared Postgres volume. Disk fills and every device's telemetry and heartbeat
   writes fail. Fleet monitoring is down.
3. Alternatively, it takes one of its own in-flight commands and sends `command_progress`
   `running` frames with a 4 MiB `step` or `message`. Each one is a 4 MiB row rewrite plus a
   4 MiB broadcast. 256 queued broadcasts is about 1 GiB of server memory, and slow dashboard
   clients are disconnected by the hub's drop policy.
4. The HTTP endpoint does the same at 512 KiB per request, with no rate limit.

**Required Resolution.** These properties must hold, and each must be tested:

1. **Identifier validation before any use.** `command_id`, whether from the URL path or a body
   (WS or HTTP), must be a canonical lowercase UUID. If it is not, the server rejects the
   report:
   - HTTP 400 `invalid_report`;
   - WS: dropped, no receipt.

   It must not run a DB query or write a log containing the raw value. A log may record only
   that a malformed ID was seen and the device ID.
2. **Field bounds on every agent-supplied string, enforced server-side on both WS and HTTP**:
   - `step` and `failure_step` ≤ 64 bytes, charset `[a-z0-9_]`; otherwise stored as `""`
     (the transition still applies) or rejected 400. Pick one rule and apply it on both paths.
   - progress `message` ≤ 1 KiB, truncated on a rune boundary.
   - `reason` must be in the documented enum; otherwise `""`.
   - `output` ≤ 256 KiB server-side, with head+tail truncation, matching the agent's
     `maxOutputBytes`.
   - the existing `stderr_tail` (8 KiB) and `result_msg` (64 KiB) bounds stay.
3. **Progress coalescing.**
   - A `running→running` report that changes neither `step` nor `progress` must not write or
     broadcast.
   - Accepted progress writes are limited to at most one per command per second. Excess
     reports are silently ignored and are not errors.
4. **Per-device rate limit** on the report endpoint and on agent WS report messages, keyed by
   the **authenticated device ID** (not the IP). For example, a token bucket of 120/min with
   burst 60, shared between HTTP and WS.
   - HTTP excess returns **429**. The agent outbox already treats unknown statuses as
     retry-with-backoff, which is correct.
   - WS excess is dropped.
   - The budget must comfortably exceed a legitimate 50-container bulk update.
5. **Rejection-log throttling.** The "command transition rejected" and "unknown or foreign
   command" Warn logs are limited per device (for example, ≤ 10 per minute, then one summary
   line with a count). They carry only validated fields.

**Blocks:** No. This is a condition of implementation (see "Mandatory conditions", C-1).

---

### MEDIUM

#### SEC-002: A NUL byte in any agent-reported string makes the report fail forever (500 loop, ERROR-rate alert, result lost)

**Severity:** MEDIUM
**Domain:** Input validation / Availability
**Location:**
- ADD AD-008 (`TruncateTail` assumes clean UTF-8)
- `commands.result_msg`, `stderr_tail`, `step`, `command_output.content` (all TEXT)
- `events.Generator.CommandCompleted`, `reason[:200]` byte slice
  (`internal/server/events/generator.go:261-263`)

**Description.**
Go's `encoding/json` decodes `"\u0000"` to a literal NUL byte. Postgres `TEXT` and `JSONB`
reject `0x00` with "invalid byte sequence for encoding UTF8". Subprocess output (compose, apt,
`docker pull` error streams, `fetch_logs`) can contain NUL or other control bytes even from a
benign agent.

Per ADD §9, a DB error during a report returns HTTP 500 with no receipt and an Error log.
AD-010 retries that for up to 24 hours with backoff capped at 5 minutes, which is about 290
attempts and about 290 ERROR log rows. That trips the server ERROR-rate alert
(`server_error_alert_worker.go`). The command then never records its real result; it becomes
`timed_out` and its auto-update policy backs off.

Separately, `CommandCompleted` slices `reason[:200]` by bytes. That can split a multi-byte rune
and produce invalid UTF-8, which then fails the events insert. This is pre-existing, but this
story pushes more agent text through that path.

**Attack Scenario.**
1. An agent's `os_update` output contains a NUL byte. This is benign, but a compromised agent
   can do it deliberately for any of its commands.
2. Every delivery attempt fails with 500.
3. Over 24 hours the operator gets repeated server-error alerts. Server logs fill with Error
   rows. The command shows "Timed out: outcome unknown" even though it finished.

**Required Resolution.**
- Before any DB write or broadcast, every agent-derived string must be NUL-free and valid UTF-8.
  This includes strings derived by server-side truncation, such as the event reason.
  Sanitization (stripping NUL and replacing invalid sequences) must never turn into a 500.
- A report whose only problem is content must be stored, not rejected.

**Blocks:** No (condition C-2).

---

#### SEC-003: A future-dated telemetry timestamp makes all later real telemetry "stale", which silently suppresses alerting and auto-update for that device

**Severity:** MEDIUM
**Domain:** Business logic / Monitoring integrity
**Location:** ADD AD-018 (`stale := ok && !snap.Timestamp.After(prev)`, which skips broadcast,
alert-threshold evaluation, UPS warnings, and auto-update/auto-patch evaluation); §6.7 (the
dashboard discards anything `<=` the current timestamp)

**Description.**
`snap.Timestamp` comes from the agent's clock. Only a *zero* timestamp is replaced with
`receivedAt`. A single snapshot dated in the future becomes the device's
`LatestSnapshotTime`. From then until the agent's clock passes that instant, every genuine
snapshot is "stale":

- it is stored to history only;
- no alert thresholds are evaluated;
- there is no live broadcast;
- the dashboard keeps the frozen future snapshot, because the dashboard rule rejects anything
  older.

Heartbeats keep flowing, so the device looks online and healthy. This is a silent monitoring
blind spot. It does not need an attacker: a host whose clock was briefly wrong (an RTC-less
board, a bad NTP source, a VM resumed with a skewed clock) and then corrected will trigger it.

**Attack Scenario.**
1. An attacker with root on a host sends one telemetry POST with `timestamp: 2099-01-01`
   using the agent's own key. Alternatively, a benign clock jump to the future and back does
   the same thing.
2. From then on, threshold alerts (disk, security, auth-failure, UPS) for that device never
   fire, and the dashboard shows the stale state.
3. Nothing indicates the device is unmonitored.

**Required Resolution.**
- The timestamp used for ordering must be bounded by server time. A snapshot more than a
  small tolerance (for example, 2 minutes) ahead of `receivedAt` is ordered as `receivedAt`.
- The stored and broadcast `collected_at` must be that bounded value, so the server, the REST
  device view, and the dashboard rule all agree.
- A Debug or Warn log records the clamp. This log must also be rate-limited per device.

**Blocks:** No (condition C-3).

---

#### SEC-004: Rollback and dependent restore are not crash-safe, and `agent_update` can kill an in-flight `docker_update` mid-recreate

**Severity:** MEDIUM
**Domain:** Business logic / Availability of managed workloads / Process lifecycle
**Location:**
- ADD AD-011 (startup recovery only reports `failed`, then clears markers); AD-014 (deferred
  restore and rollback, "runs on every outcome")
- D-013 (a host key never conflicts with Docker keys)
- `internal/agent/agent.go:250-258` (on SIGTERM, cancel and `wg.Wait()` cover only the loops,
  not command goroutines)
- `scripts/install.sh` unit (no `TimeoutStopSec`, so the default is 90s; cgroup kill)

**Description.**
The ADD's guarantee that "the deferred restore … runs on every outcome including a recovered
panic" is true only *inside a living process*. The process can end mid-update in several ways:

- agent self-update via `systemd-run` restarting `riot-agent.service`;
- `systemctl restart`;
- OOM;
- a reboot command.

When it does:

- `Run` returns after `wg.Wait()` without waiting for command goroutines, so the deferred
  restore and rollback never execute.
- Even if the agent waited, `RollbackBudget` (60s) plus `DependentRestoreBudget` (2m) exceeds
  systemd's 90s default stop timeout, so it would be SIGKILLed anyway.
- Startup recovery then reports `failed: agent restarted…` and **clears the markers** without
  reconciling anything.

The standalone container stays renamed to `<name>.riot-old-*` and stopped. No container holds
the original name, so the service is down. FR-042 then blocks every future update of that
container. Network dependents (for example, the VPN-namespace children of a gluetun parent)
stay stopped. That is defect 4, which this story exists to fix, coming back through a
different path.

Because `agent_update` holds only the `host` key, and D-013 says host never conflicts with
Docker, a fleet bulk agent update (`POST /fleet/bulk-update`) issued during the maintenance
window, while auto-updates are recreating containers, triggers this deterministically.

**Attack Scenario** (operational, no attacker needed).
1. At 03:00 auto-update recreates `vpn` (standalone; dependents `qbittorrent` and `sonarr`
   use `container:vpn`).
2. The operator, or a scheduled fleet action, issues an agent update.
3. The agent restarts between the rename-aside and the create.
4. After restart, `vpn` does not exist, `vpn.riot-old-1a2b3c4d` is stopped, and both
   dependents are stopped. The dashboard reports one failed command. Every later update of
   `vpn` fails precheck.

**Required Resolution.**
- **(a)** The in-flight marker for `docker_update` and `docker_bulk_update` must be durably
  updated *before* each corresponding Docker mutation with:
  - the original name;
  - the set-aside name and the old container ID;
  - `wasRunning`;
  - the IDs of dependents this command stopped.
- **(b)** Startup recovery must reconcile from that marker before reporting:
  - If the set-aside container exists and no container holds the original name, rename it
    back and start it if `wasRunning`.
  - If both exist, leave both untouched and name them in the failure message.
  - Restart recorded dependents that are still stopped.
  - The failure result must say what was reconciled.
- **(c)** `agent_update` and `agent_uninstall` must fail `precheck` (reason `conflict`) while
  any `docker:*` lock is held on the agent. The server-side conflict model (D-013) does not
  need to change.
- **(d)** On SIGTERM the agent stops accepting new commands. How long it waits for in-flight
  critical sections is up to the architect, but it must stay below the unit's stop timeout.
  Correctness must not depend on it, because (b) is the backstop.

**Blocks:** No (condition C-4). The architect should add (a) through (c) to AD-011, AD-012 and
AD-014 as an addendum. This is not a redesign.

---

#### SEC-005: `sanitizeComposeFile` (kept unchanged) writes a predictable path in a foreign-owned directory, follows symlinks, and leaves a world-readable copy of the compose file

**Severity:** MEDIUM
**Domain:** File handling (CWE-59, CWE-276) / Data exposure
**Location:** `internal/agent/docker_update.go` `sanitizeComposeFile` (fixed name
`.compose.riot-sanitized.yml`, `os.WriteFile(…, 0644)`); ADD §4.2 ("`sanitizeComposeFile` …
kept"); ADD AD-013 (the retry shares the pull budget)

**Description.**
When compose reports a validation error, the agent writes a full copy of the user's compose
file into `<workDir>/.compose.riot-sanitized.yml`. It runs as `riot` with
`SupplementaryGroups=docker`.

1. **Mode `0644` under the default umask.** Compose files routinely hold inline secrets
   (`environment:` DB passwords, API tokens). If the original was `0600` or `0640`, the copy
   is world-readable for the whole pull+up duration (up to about 25 minutes). If the agent
   dies, the copy persists indefinitely, because nothing removes it at startup.
2. **`os.WriteFile` follows a pre-existing symlink** at that predictable name. The writer
   does not need write permission on `workDir` itself. Whoever controls `workDir` can point the
   name at any `riot`-writable file (`/etc/riot/agent.yaml`, `/var/lib/riot/*.json`), and the
   agent overwrites it with YAML whose structure that person controls (it is their compose
   file). Practical escalation is limited, because a party who controls a compose directory
   that rIOt runs `up` on already gets root-equivalent execution through compose. The
   integrity and DoS impact on agent config and state is still real.

**Attack Scenario.**
1. User `alice`, not in the docker group, owns `/opt/alice-app`. That project is
   auto-updated by rIOt.
2. She writes a compose file with a Swarm-only `deploy` key, which triggers the validation
   path, and runs `ln -s /etc/riot/agent.yaml /opt/alice-app/.compose.riot-sanitized.yml`.
3. On the next update, the agent overwrites `agent.yaml` with her YAML.
4. On restart, the agent loses its key and device ID (DoS), or loads her values.

Separately, any local user can read the sanitized copy of a `0600` compose file while an
update runs.

**Required Resolution.** The sanitized file must:
- be created exclusively (fail if any entry already exists at that path, and never follow a
  symlink);
- have a mode no broader than `0600`;
- be removed on every exit path;
- have leftovers from a crash removed at startup.

Placing it in agent-owned state instead of `workDir` is acceptable if compose path resolution
still works (relative paths resolve against `--project-directory`). Location is the
architect's call.

**Blocks:** No (condition C-5).

---

#### SEC-006: The "status lists are SQL literals, never bind parameters" rule must be fenced off from request input

**Severity:** MEDIUM
**Domain:** Injection
**Location:** ADD AD-003 ("status lists … are SQL literals generated from `models`
constants"), §12.1, §6.2 (`GET …/commands?status=` is comma-separated, canonical or legacy,
alias-expanded; `target_key=`); §4 (`command_repo.go` "status-filter alias expansion")

**Description.**
The ADD tells implementers to build SQL text from status values, for a valid planner reason.
In the same change it adds a user-controlled, alias-expanded `status` filter. If the
alias-expansion helper and the literal builder are the same function, or the filter path
reuses the literal builder on request strings, the result is string-built SQL from a query
parameter. The admin is the only caller, so the realistic exploit path is narrow. But the
Postgres role in the stock compose deployment is a superuser. SQLi there reaches `COPY …
PROGRAM`, which is host command execution in the DB container. The design invites this
mistake.

**Attack Scenario.**
An admin session, or a future feature that lets a less-trusted principal call the list
endpoint, requests `GET /api/v1/devices/X/commands?status=queued') OR 1=1; --`. A
literal-builder path interpolates it.

**Required Resolution.**
- The literal builders accept only the typed `models.CommandState` constants. They are
  unexported or typed so a `string` cannot be passed without an explicit conversion, and they
  are never called with request data.
- Request `status` values are validated against the canonical-plus-legacy allowlist. Unknown
  values return 400 (or are ignored; pick one).
- Request `status` and `target_key` values reach SQL only as bind parameters.

**Blocks:** No (condition C-6).

---

### LOW / INFORMATIONAL

#### SEC-007 (LOW, mandatory): The agent does not validate `command_id` format at intake

**Domain:** Input validation (defense in depth against a forged server or MITM)
**Location:** ADD AD-011 (`submitCommand`), AD-010 (`POST …/commands/{id}/report`),
§10.7 (validation of the set-aside name only)

The ADD validates `cmdid[:8]` only when it builds the set-aside name. Other uses of the same
server-supplied string:

- the report URL path, where a crafted `../../telemetry` re-targets the authenticated POST to
  another endpoint on the same server;
- seen-store and outbox keys;
- logs.

`submitCommand` must reject (not execute, and log at Warn) any payload whose `command_id` is
not a canonical UUID. Report URLs must path-escape the ID anyway. The exception is the
row-less `agent_uninstall`, which already uses a UUID.

**Attack Scenario:** A MITM on an `http://` deployment injects a WS `command` with
`command_id: "../../logs"` and `action: fetch_logs`. The agent's report POSTs go to an
unintended path.

#### SEC-008 (LOW, mandatory): Seen-ID persistence failure behavior is unspecified, which matters for power actions

**Domain:** Business logic / Replay
**Location:** ADD AD-011 step 1; AD-010 "degraded mode" (which covers only the outbox)

The seen store is the only control against re-executing a `reboot` or `shutdown` that crosses
a reboot boundary. The crossing paths are:
- an FR-085 revert after an ambiguous WS write;
- the documented double-delivery by a legacy server.

If the seen file cannot be written (disk full, read-only remount), the ADD does not say
whether the command runs.

Required properties:
- The seen ID is durably persisted (file and directory fsync) before execution.
- If persistence fails, `reboot`, `shutdown`, `os_update`, and `agent_update` are reported
  `failed` at `precheck` and not executed. Other actions may run in degraded mode with an Error
  log.

**Attack Scenario:** `/var/lib/riot` is full. A reboot is delivered and executed, the WS write
of the ack fails, and the row is reverted to queued. After boot, the heartbeat claims the row
again within the 15-minute TTL. With no persisted seen ID, the host reboots a second time.

#### SEC-009 (LOW): The down migration leaves non-expired queued power actions deliverable by a legacy server that has no TTL

**Domain:** Business logic / Rollback safety
**Location:** ADD §5.3 step 1 (expires only rows with `expires_at <= NOW()`)

After a rollback, a `reboot` queued 5 minutes earlier stays `queued`. The old server has no
TTL, so it delivers that reboot whenever the agent next heartbeats, possibly days later. This
is the exact NFR-005 hazard. Recommendation: on the way down, also convert queued `reboot` and
`shutdown` rows, and queued `os_update` rows with `include_reboot_class`, to `error`
regardless of expiry. A rollback is a rare manual act, and failing safe is the right default.

#### SEC-010 (LOW): The first sweep and the FR-008 reset fan out events and recreations

**Domain:** Availability / Alert fatigue
**Location:** ADD §7.3 (every sweep transition emits `CommandCompleted("error")`), AD-006 /
D-010 (historical `sent` rows and backfilled queued rows sweep on the first pass), §5.2
(FR-008)

On the first pass after deploy, every historical `sent` row whose result was lost, and every
old `pending`/`queued` row (backfilled `expires_at = created_at + TTL`), becomes
`timed_out` or `expired`. Each one emits a warning event, which can reach notification
channels. That is up to 500+500 per pass.

Separately, defect 1 means per-container auto-update policies have *never* worked. After
FR-008 they recreate containers for the first time. With `stagger_seconds = 0`, they all fire
in one telemetry cycle.

Recommendation:
- Suppress completion events for rows whose `created_at` predates the migration, or emit one
  summary event.
- Call out the first-activation behavior in the release notes.

This is a product-owner note, not a code defect.

#### SEC-011 (LOW, mandatory): Harden Docker and compose argv, and remove the old container only by ID

**Domain:** Argument injection (defense in depth) / Integrity
**Location:** ADD AD-014 (dependent `docker compose --project-directory <wd> up -d --no-deps
--force-recreate <svc>`, where `<wd>` and `<svc>` come from container labels; `cleanup`; and
rollback)

- Label values are set by docker-socket holders, who are root-equivalent, so this is not a
  privilege boundary. Still, a service label starting with `-` is parsed by compose as a flag.
  Positional service names must be validated against the compose service-name charset
  (`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`) or placed after a `--` separator.
- `--project-directory` must receive an absolute path in a form that cannot be read as
  another flag.
- `cleanup` and rollback must address the old container by the **immutable ID** captured at
  resolve, never by the set-aside *name*. That keeps NFR-004 ("never remove the old container
  before verify") provable even if names change mid-operation.

#### SEC-012 (LOW, pre-existing): Deferred hardening item

Details are intentionally not reproduced in this public document. The item is pre-existing,
low severity, does not block this story, and is tracked separately as follow-up work.

#### SEC-013 (LOW, mandatory): The CI Postgres service must not involve secrets or reach non-test databases

**Domain:** CI / Supply chain
**Location:** ADD AD-017, §12.14; `.github/workflows/ci.yml`

- The `postgres:16` service credentials must be literal throwaway values in the workflow (for
  example `POSTGRES_PASSWORD: postgres`), never `secrets.*`.
- The workflow should declare `permissions: contents: read`. The trigger stays
  `pull_request` (no `pull_request_target`).
- Pinning the service image by digest is recommended.
- The PG-gated tests must refuse to run unless the database in `RIOT_TEST_PG_URL` is clearly a
  test database (for example, the name contains `test`, or an explicit
  `RIOT_TEST_PG_ALLOW=1`). The tests create schemas and run up and down migrations, and a
  developer who exports a production URL by mistake should get a skip, not a migration run
  against production.

#### SEC-014 (INFO, pre-existing): `terminal_output` and `terminal_close` on the agent socket are not scoped to the device

**Location:** `internal/server/handlers/terminal.go:140-160`

Any agent socket can write to, or close, any browser terminal session whose ID it knows.
Session IDs are 128-bit `crypto/rand` values that are sent only to the owning agent, so there
is no practical exploit. Since `terminal.go` is being restructured (AD-009), binding each
session to its device is a cheap improvement.

#### SEC-015 (INFO, pre-existing): Deferred hardening item

Details are intentionally not reproduced in this public document. The item is pre-existing,
informational, does not block this story, and is tracked separately as follow-up work.

#### SEC-016 (INFO): An ambiguous WS send can produce a misleading audit record

**Location:** ADD §7.1 step 4 (FR-085 revert), AD-001 (cancelled and expired are immutable)

If a WS write returns an error after the bytes actually reached the agent, the row reverts to
`queued`. It can then be cancelled or expire while the agent really ran it. The agent's later
result is rejected (the row is immutable), and history shows "Cancelled" or "Expired: never
delivered" for a command that executed. The NFR-013 Warn log is the only trace. This is
acceptable. QA should know it exists, and the Warn log must keep `command_id`, `from`, and
`to`.

---

## Positive Observations

- **The IDOR design is correct.** Ownership is enforced by `device_id = <authenticated
  device>` in the same SQL statement as the write. It is never a Go-side check after a read,
  so there is no TOCTOU gap. Foreign and missing commands return byte-identical 404s or
  receipts (§10.1, AD-008). The pre-existing unscoped `handleCommandResult` is confirmed, and
  AD-009 fixes it for all three agent message kinds using the socket's key-validated device ID.
- **The state machine works as a safety control.**
  - Immutable terminal states turn replays into no-ops.
  - No agent edge leaves a terminal state into a non-terminal one. The single `timed_out →
    final` exit happens at most once.
  - Progress does not extend `deadline_at`, so a malicious agent cannot hold a host or Docker
    conflict lock beyond the execution deadline.
  - Agent edges out of `queued` COALESCE-fill `deadline_at`, so no active row escapes the
    sweeper.
- **Stale power-action delivery is closed at the right layer.** `expires_at > now` sits inside
  the claim predicate itself, in both the inner `FOR UPDATE SKIP LOCKED` select and the outer
  guard. A failed expire step therefore still cannot deliver an expired reboot, including to
  legacy agents. The FR-025 window-end cap also fixes a latent PATCH-GATE hazard: before this
  story, a queued `os_update` with `include_reboot_class` could be delivered, and reboot the
  host, outside the maintenance window.
- **The down migration fails safe.** `cancelled`, `expired`, and `timed_out` map to `error`,
  never `queued`, and overdue queued rows are expired first.
- **Validating `compose_work_dir` against real container labels** is new hardening. A forged
  server can no longer point `compose` or the sanitize step at arbitrary directories.
- **Removing the unguarded repository methods** makes an unguarded status write a compile
  error.
- **Agent HTTP response trust.** An outbox entry is removed only for a JSON body that echoes
  `command_id`, so a captive portal's 200 cannot discard results. The agent caps response
  reads at 1 MiB.
- **Stored text policy.** Parameterized SQL, React text nodes, `dangerouslySetInnerHTML`
  forbidden (and absent from `web/src` today), and only content *lengths* logged.
- **Sound new admin routes.** The new admin routes sit in the existing
  `/api/v1/devices/{id}` subrouter with `adminAuth`, which matches every sibling. The session
  cookie is `SameSite=Lax`, so the new cancel POST adds no CSRF surface.
- **No new privilege.** No new sudoers rule, no new dependency, fixed agent file paths, and
  temp-plus-rename writes in an agent-owned directory.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.**

There are no CRITICAL findings. The one HIGH finding (SEC-001) has a clear, non-architectural
resolution: validation bounds, coalescing, and a per-device limiter at the service boundary.
The FRD and ADD security model is correct as written.

SEC-004 adds three small items to AD-011, AD-012, and AD-014: a richer marker, reconciliation
on startup, and a lock check for `agent_update`. These extend the existing design rather than
change it. The architect should fold them into an addendum before or during WP-GO.

`senior_dev` may proceed with this report alongside the ADD. The QA engineer must verify every
condition below. The suggested test names follow the ADD's `TestCL_` convention, with `SEC`
in place of `AC`.

### Mandatory conditions

**C-1 (SEC-001): agent report bounds and throttling.**
- `TestCL_SEC001_NonUUIDCommandIDRejectedWithoutQueryOrPayloadLog`: HTTP returns 400, WS
  drops with no receipt, the repository is never called, and captured logs do not contain the
  raw value. Cover both the path and body variants, and a 1 MiB ID.
- `TestCL_SEC001_StepAndMessageBounded`: covers `step` > 64 bytes or outside the charset,
  a progress `message` > 1 KiB, and `reason` outside the enum, on both WS and HTTP.
- `TestCL_SEC001_OutputCappedServerSide`: a WS result with 2 MiB of output stores ≤ 256 KiB
  (head+tail).
- `TestCL_SEC001_IdenticalProgressNoWriteNoBroadcast`.
- `TestCL_SEC001_ProgressThrottledPerCommand`: N reports within 1 second produce at most one
  write and one broadcast, using a fake clock.
- `TestCL_SEC001_ReportRateLimitedPerDevice`: HTTP returns 429 and WS drops. The bucket is
  keyed by device ID, so device B is unaffected while device A is limited.
- `TestCL_SEC001_RejectionLogsThrottledPerDevice`.
- The agent outbox treats 429 as retry-with-backoff:
  `TestCL_SEC001_Outbox429RetriesWithBackoff`.

**C-2 (SEC-002): NUL-free, valid UTF-8 storage.**
- `TestCL_SEC002_ReportWithNULAndInvalidUTF8Stored`: returns 200 or `accepted`, and the
  stored values contain no `0x00` and pass `utf8.ValidString`. Applies to `result_msg`,
  `stderr_tail`, `step`, and output.
- `TestCL_SEC002_CompletionEventReasonRuneSafe`: a 200-byte boundary inside a multi-byte rune
  still yields valid UTF-8.
- A PG-gated variant: `TestCL_SEC002_PG_NULReportPersists`.

**C-3 (SEC-003): bounded telemetry ordering timestamp.**
- `TestCL_SEC003_FutureTimestampClampedToReceivedAt`.
- `TestCL_SEC003_FutureSnapshotDoesNotSuppressLaterTelemetry`: after a +1-year snapshot, the
  next real snapshot is broadcast and alert evaluation runs.
- `TestCL_SEC003_BroadcastCollectedAtIsClampedValue`.

**C-4 (SEC-004): crash-safe Docker update.**
- `TestCL_SEC004_MarkerPersistedBeforeRenameAndDependentStop`: the fake Docker call order
  shows each marker write before the matching mutation.
- `TestCL_SEC004_RecoveryRenamesSetAsideBackWhenOriginalMissing`: includes the start when
  `wasRunning` and no start otherwise.
- `TestCL_SEC004_RecoveryLeavesBothWhenOriginalExists`.
- `TestCL_SEC004_RecoveryRestartsRecordedStoppedDependents`.
- `TestCL_SEC004_AgentUpdateRejectedWhileDockerLockHeld`, plus the same test for
  `agent_uninstall`.
- `TestCL_SEC004_NoNewCommandsAcceptedAfterSIGTERM`.
- The NFR-004 table (`TestCL_NFR004_*`) must include the recovery path.

**C-5 (SEC-005): `sanitizeComposeFile` file handling.**
- `TestCL_SEC005_SanitizedFileRefusesPreexistingSymlink`: Linux only. A symlink at the
  target name causes the sanitize step to fail, and the symlink target is unmodified.
- `TestCL_SEC005_SanitizedFileMode0600`.
- `TestCL_SEC005_SanitizedFileRemovedOnEveryExit`: covers success, pull failure, up failure,
  and panic.
- `TestCL_SEC005_StartupRemovesLeftoverSanitizedFile`.

**C-6 (SEC-006): status filter injection.**
- `TestCL_SEC006_StatusFilterRejectsUnknownValues`: `?status=queued')--` returns 400 (or is
  ignored), and no SQL text contains it.
- `TestCL_SEC006_TargetKeyFilterIsBound`: covers a target key containing `'` and `%`.
- Code review must confirm that the literal builders take only `models.CommandState`.

**C-7 (SEC-007): agent `command_id` intake validation.**
- `TestCL_SEC007_NonUUIDCommandIDNotExecuted`: covers WS and heartbeat. Nothing executes,
  nothing goes to the outbox, and no report is sent.
- `TestCL_SEC007_ReportURLPathEscaped`.

**C-8 (SEC-008): fail closed on dedupe persistence failure.**
- `TestCL_SEC008_SeenPersistFailureFailsPowerActionsClosed`: covers reboot, shutdown,
  os_update, and agent_update. None executes, and each fails at `precheck`.
- `TestCL_SEC008_SeenPersistedBeforeExecute`.

**C-9 (SEC-011): argv and removal by ID.**
- `TestCL_SEC011_DependentServiceNameValidatedOrSeparated`: a label value of `--file=/x`
  never appears as a flag in argv.
- `TestCL_SEC011_CleanupAndRollbackUseOldContainerID`.

**C-10 (SEC-013): CI.**
- QA checks `ci.yml`: no `secrets.*` in the PG service, `permissions: contents: read`, no
  `pull_request_target`.
- `TestCL_SEC013_PGTestsSkipOnNonTestDatabase`.

**C-11: verify the designed controls are implemented as written.** These come from ADD §10
and are not new findings, but they cannot be confirmed from the ADD alone.
- `TestCL_SEC_RouteAuthMatrix`:
  - `POST …/commands/{cid}/report` returns 401 with no key, 401 with only a JWT cookie, and
    403 with another device's key. It is registered with the full `deviceAuth...` stack,
    including mTLS when enabled.
  - `GET …/commands/{cid}` and `POST …/cancel` return 401 with a device key and no JWT.
- `TestCL_SEC_ForeignAndUnknownReportIndistinguishable`: for `ack`, `progress`, and `result`,
  over HTTP and WS, the response status and body (or receipt) are byte-identical for a
  foreign ID and a random ID. There is no row change, broadcast, `command_output` write, or
  policy update. The classification read after a 0-row update is device-scoped in SQL.
- `TestCL_AC024_WSForeignAckAndProgressIgnored`: extends AC-024 beyond `command_result`.
- `TestCL_SEC_BodyCommandIDMismatch400` and `TestCL_SEC_ReportBodyOver512KiB413`.
- `TestCL_SEC_NoResurrectionEdges`: across all actors, no edge leads from
  {succeeded, failed, cancelled, expired} anywhere, no agent edge leads from `timed_out` to a
  non-terminal state, and only the server may enter `queued` or `dispatched`.
- `TestCL_SEC_ActiveNonQueuedRowsAlwaysHaveDeadline`: service-level plus PG-gated, including
  migrated `sent` rows and agent edges out of `queued`.
- `TestCL_SEC_ProgressDoesNotExtendDeadline`.
- `TestCL_SEC_ClaimSkipsExpiredEvenIfExpireStepFails`: an expired queued reboot is never
  returned by `ClaimQueued` or `ClaimByID`.
- `TestCL_SEC_ReportContentNotLogged`: log capture shows lengths only, never
  `stderr`/`message`/`output` content.
- `TestCL_SEC_AgentStateFilesMode0600`: Linux only; covers the outbox, seen, and inflight
  files, with exclusive-create random temp names.
- `[SEC] agent failure text renders as text`: a vitest case where `result_msg` is
  `<img src=x onerror=alert(1)>` and appears as literal text in ContainerDetail,
  ComposeStackSection, and ActivityLog. QA also greps `web/src` to confirm
  `dangerouslySetInnerHTML` is still absent and that agent strings are never bound to
  `href` or `src`.

**Recommended, not mandatory:** SEC-009 (down-migration power rows), SEC-010 (first-sweep
event suppression and a release note), SEC-014 (terminal session scoping), SEC-016
(awareness), and two deferred hardening items (SEC-012, SEC-015) tracked separately.
