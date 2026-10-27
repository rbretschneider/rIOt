# Architecture Decision Document

- **Story ID:** EXPECT-008
- **FRD Reference:** `docs/requirements/EXPECT-008-frd.md` (revision 1, commit 05144a7; DEC-1 to DEC-8 at the recommended option)
- **Parent designs:** `docs/architecture/EXPECT-001-add.md` (windows, Tracker, `finishWindow`, escalation markers, push ceiling, restart grace, activity API) and `docs/architecture/EXPECT-002-add.md` (self-update record pattern: a `commands` row with its own origin, restricted actor, shape CHECK, unique open index, cap, stuck-record handling)
- **Security reviews inherited:** `docs/security/EXPECT-001-security-review.md` (C-1 to C-8) and `docs/security/EXPECT-002-security-review.md` (C-1 to C-6). The Security addendum maps each condition to this design
- **Author:** Architect Agent
- **Date:** 2026-10-27
- **Status:** DRAFT, revision 1 (for security review, then implementation)

---

## 0. FRD review (Stage 1) and the four architect questions

Ryan was not reachable. No business question blocks the design; every open point is decided below on Ryan's behalf and listed again under "Open points for Ryan".

| Item | Answer |
|---|---|
| **Q-1** Mechanism, and does it work for agents ≤ 2.83.0? | **(a) + (c).** The installer sends one authenticated notice before it stops the agent (AD-001). The server stores it as a `commands` row with a new origin, `installer`, on the EXPECT-002 pattern (AD-003). No agent change. It works on the very next installer re-run on every device that runs any deployed agent version, because the agent is not involved. (b) is rejected: it needs a new agent on every device first, so the next re-run on each device would still page, and SIGTERM cannot tell a reinstall from a reboot, an `apt` restart or a manual stop |
| **Q-2** Can the installer honour every trust setting with stock tools? | Yes for the configurations the installer and the agent write themselves: plain `http://`, `https://` with the TOFU-pinned `/etc/riot/server.crt`, `https://` with a CA file in `/etc/riot`, `https://` against public roots, and mTLS with `/etc/riot/client.crt` and `client.key`. It uses `curl`, `awk`, `head`, `stat`, `timeout` and `wc`, all present on stock Debian, Raspberry Pi OS and DietPi. **Skipped** (one "not notified" line, alerting as today): `tls_verify: false` without a pinned server certificate; a `server_cert_pin` without a pinned server certificate; any certificate or key path outside `/etc/riot`, or one that is not a plain regular file; a non-literal `tls_verify`; a host with `wget` but no `curl` (AD-015, AD-016, X-2) |
| **Q-3** Any interactive prompt between stop and start? | **No.** Verified: `systemctl stop riot-agent` (install.sh l.991) and `systemctl start riot-agent` (l.1192) have no prompt between them. `ask_yn` (l.497) runs only on a fresh install, before any lock. `require_shutdown_agreement` (l.406, called at l.1003 and l.1138) is a non-interactive consistency check. Its failure, and every other `fail` after the stop (l.994, l.1001, l.1173, l.1177, l.1178), leaves the agent stopped: the device then escalates once at the bound (NFR-010). The notice stays before the stop. The README states that the 10-minute budget runs from the notice to the agent's registration and that no prompt sits inside it |
| **Q-4 / FR-029** Rollout | Section 14. A server release and a hawaii deploy are required. No device needs a new agent or any extra step. The installer change is live on merge; until the server is deployed, a re-run prints "not notified" and pages as today. The first silent re-run is the first one after the hawaii deploy is verified |

---

## 1. Summary

Before `install.sh` stops the agent on a registered device, it sends one notice: `POST /api/v1/devices/{id}/events` with the new type `agent_reinstall_started`. The notice is authenticated with the device key read from `/etc/riot/agent.yaml`, and it goes to the same URL under the same trust settings the agent uses. The key travels only through `curl`'s standard input, never on a command line. The attempt is hard-bounded to 10 s and can never change the installer's result.

The server turns an accepted notice into one `commands` row with `action = 'agent_update'` and a new origin, `installer`. The row is created in `running`, with a 10-minute deadline. This reuses the whole EXPECT-001/002 machinery: the window loader, the deletion exemption, the escalation markers, the push ceiling, the activity API and the command history.

The rules are the same kinds as EXPECT-002, with their own constants and SQL:
- one open record per device;
- 6 records per device per 24 h (a budget separate from EXPECT-002's, DEC-1);
- a shape CHECK;
- a restricted actor, `ActorReinstall`, as the only writer.

The window covers `device_offline` only. It closes at the device's next registration (outcome `succeeded`) or at the bound (outcome `not confirmed`). At the bound, a device that is still offline escalates once. A device that is online at the bound gets one `warning` event and no push.

One migration (000035). No agent change, no new dependency.

## 2. Technical Context

### 2.1 What exists (verified at 05144a7: v2.83.0 plus PR #43 and #44)

| Area | Where | Relevant facts |
|---|---|---|
| Installer, reinstall path | `scripts/install.sh` | `set -euo pipefail` (l.2). `PATH` fixed (l.156), `umask 077` (l.158). `STAGE` is a 0700 dir under `/tmp`, removed by the `EXIT` trap (l.163-181). `CONFIG_EXISTED` (l.489). Prompts only when no config exists (l.553). The lock and validation block (l.700-988): tool checks, the maintenance lock (l.775), native package locks, the candidate sudoers check (l.933), unit verification (l.963), the backup (l.965), the effective policy check (l.978), user and groups (l.983-988). Stop at l.990-992, guarded by `LoadState != not-found`. Bootstrap re-enrolment deletes the config (l.1072). Start at l.1192 |
| Installer config readers | l.199-388 | `config_facts` snapshots `agent.yaml` into `STAGE` (`timeout 5 head -c 65537`), rejects NUL and over 64 KiB, and parses it with one mawk-safe awk pass. It sits between the `BEGIN`/`END shutdown-config-classifier` markers, which `test_EU_AC016_classifier_agrees_on_every_awk` sources. The config is **riot-writable**, so it is untrusted input |
| Installer downloads | `download_file` l.419 | `curl` preferred, `wget` fallback |
| Agent config | `internal/agent/config.go` l.151-171 | `server.url`, `server.api_key`, `server.tls_verify` (**default true when absent**, l.208), `ca_cert_file`, `client_cert`, `client_key`, `bootstrap_key`, `server_cert_pin`, `agent.device_id`. The agent saves the file with `yaml.v3` (4-space indent, plain or quoted scalars, l.355-368). The installer writes 2-space indent with double quotes (install.sh l.1083) |
| Agent identity | `register.go` l.106-216; `handlers.go` l.444-446, l.1522 | Device ID = canonical UUID. Device key = `riot_` + 64 hex. Both are saved to `agent.yaml` after the first registration |
| Agent TLS | `agent.go` l.204-231, l.504-593; `http_client.go` l.159-253 | **https with `/etc/riot/server.crt` present:** at start the agent sets `CACertFile = server.crt` and `TLSVerify = true` in memory, whatever the file says. Its root pool is then exactly that certificate. **https without it:** TOFU found no self-signed certificate. `tls_verify: false` means `InsecureSkipVerify`. Otherwise the pool is `ca_cert_file` (if readable), else the system roots. **mTLS:** the agent presents the client certificate only when both `client_cert` and `client_key` are set and load. `server_cert_pin` is compared only during the TOFU fetch, never at the handshake. Runtime pinning **is** the `server.crt` trust anchor. No proxy is used (custom `Transport`, systemd environment) |
| Agent stop | `agent.go` l.353-371 | SIGTERM → `shuttingDown`, wait `AgentShutdownGrace`, no message to the server (≤ 2.83.0) |
| Agent event route | `router.go` l.200-213; `middleware/auth.go` | `DeviceAuth`: a missing key → 401; an unknown or revoked key → 401; a key for another `{id}` → 403. Then `MTLSDeviceAuth` when a CA is on, and the ingest bucket (120 burst, 2/s per device) |
| `ReceiveAgentEvent` | `handlers.go` l.1181-1229 | **An unknown type returns 400 `{"error":"unsupported event type"}`** before any storage. The extra-updater types branch to their own handler (l.1194). EXPECT-002 types are stored with a link |
| EXPECT-002 record | `db/self_update_repo.go`, `commands/self_update.go`, `expect/selfupdate.go`, migration 000034 | Pattern reused here. **Its FR-004 attribution query (l.82-88) matches any `agent_update` row with `origin <> 'agent_self_update'`, `dispatched_at` set and in flight, so it would capture a reinstall row** (AD-017) |
| Transition SQL guard | `db/command_repo.go` l.447-454 | `ActorSelfUpdate` → `origin = 'agent_self_update'`; every other actor → `origin <> 'agent_self_update'`. `ListOverdue` excludes self-update rows (l.500) |
| Tracker | `expect/*` | `ExpectPolicyFor` (models/expect.go l.189). `evaluate` delegates on `policy.SelfUpdate` (window.go l.129). So do `finishWindow` (finish.go l.149), `NoteOnline` (tracker.go l.601), the eager FR-020 skip (tick.go l.105), `escalateDevice` and `markDeviceEscalated` (escalate.go l.132, l.157), activity (activity.go l.109) and the escalation rebuild (tracker.go l.1055). `trimCommand` keeps `Origin` (l.272) |
| Messages | `models/expect.go` l.418, l.439 | Suppressed offline: `Device %s went offline during %s %s (expected%s)` with action label plus actor phrase. Online: `Device %s came online after %s %s (offline %s)` |
| Web | `utils/activity.ts`, `utils/selfUpdate.ts`, `components/SelfUpdateDetails.tsx`, `ActivityLog.tsx`, `docker/CommandProgress.tsx`, `ActivityLabel.tsx`, `pages/FleetOverview.tsx` l.327-354 | `isAgentUpdating` turns any `agent_update` host or agent activity into the compact Version-cell spinner (PR #43). Other activities use the compact one-line `ActivityLabel`, with the actor and start time in the hover title (PR #44) |
| Installer CI | `.github/workflows/ci.yml` l.111-123 | `debian:bookworm-slim` and `trixie-slim`, with network on (for `apt-get`). It installs `python3 openssl sudo systemd ca-certificates`. awk is **mawk**. There is **no curl** in the image: the fixture writes a stub `/usr/bin/curl` (test file l.136) |
| Fixture `systemctl` stub | test file l.157-203 | `is-active` falls to `*) exit 3` today, so every existing reinstall fixture has an "inactive" agent |

### 2.2 What is missing

- A signal from the installer to the server.
- A record, a window and an outcome per installer re-run.
- The "Reinstalling agent" label and the record view.

### 2.3 What must change

See Section 4: three independent parts (Go server, installer, web) plus docs.

---

## 3. Architecture Decisions

### AD-001: Mechanism: an installer notice, stored as an EXPECT-002-style record (Q-1)

**Decision.** On a reinstall run, `install.sh` sends one notice just before `systemctl stop riot-agent` (AD-013 to AD-016). The server records an accepted notice as a `commands` row with origin `installer` (AD-003). The agent is not changed.

**Rationale.**
- It is the only candidate that covers the very next re-run on every deployed agent. The installer is fetched from `main`, so the change reaches every device at its next run with no per-device step.
- The installer knows it is reinstalling. An agent seeing SIGTERM does not.
- The storage choice reuses every EXPECT-001/002 guarantee: durable windows, markers, ceiling, labels, retention exemption, restart durability. That satisfies FRD 7.1, "no parallel window system".

**Alternatives considered.**
- *(b) The agent reports on SIGTERM.* Rejected. It needs a new agent everywhere first, and it either over-suppresses (reboots, `apt` restarts, manual stops) or needs an installer signal anyway. It overlaps EXPECT-007.
- *(a) only, with an in-memory window.* Rejected: it fails NFR-009 (restart durability) and FR-026 (history).
- *Reuse origin `agent_self_update` with a marker in `params`.* Rejected. It shares EXPECT-002's unique open index (a self-update and a reinstall could not both be open, against AC-023) and its cap (against DEC-1), and it changes EXPECT-002 wording (FR-027).

**Consequences.**
- One migration (000035).
- The record's start is the server receive time of the notice.

### AD-002: Wire format: a new event type on the existing agent event route, never stored as an event

**Decision.**
- The notice is `POST {server.url}/api/v1/devices/{device_id}/events` with header `X-rIOt-Key`.
- Its body is the fixed JSON `{"type":"agent_reinstall_started","severity":"info","message":"Agent reinstall started by installer"}`.
- `ReceiveAgentEvent` gains a branch before its type switch's `default`: `case models.EventAgentReinstallStarted: h.receiveReinstallNotice(w, r, deviceID, agentEvt); return`.
- `receiveReinstallNotice` (new file `handlers/agent_event_reinstall.go`):
  1. **Identity.** `authenticatedDeviceID(r)` must equal `{id}`. `DeviceAuth` has already rejected a missing key (401), an unknown or revoked key (401) and a key for another device (403), so this is defence in depth. A mismatch answers disposition `unavailable`.
  2. **Inputs.** `at := h.clock()`. The hostname is read through `h.devices.GetByID`. A failed read gives `""`, rendered as `device` in messages.
  3. **Record.** `res := h.cmdSvc().NoteReinstallNotice(ctx, commands.ReinstallNoticeInput{DeviceID, Hostname, At: at})`.
  4. **Broadcast** `res.Events` (at most the rate-cap warning).
  5. **Response.** `200 {"status":"ok","reinstall":"<disposition>"}`. The disposition is one of `created`, `open_record`, `capped`, `device_offline` or `unavailable`.
  6. **No `events` row is written for the notice** (NFR-005, AC-016, AC-021). `message`, `severity` and every other field of the body are ignored (FR-012).
- A body that is not valid JSON → 400 `{"error":"invalid request body"}`, as today.

**Rationale.**
- **Old servers fail closed.** On every deployed server, an unknown type is answered 400 before anything is stored (`handlers.go` l.1197-1199). A new installer against an old server therefore records nothing and prints "not notified" (FR-028, AC-024).
- A new route could fall through to the SPA catch-all on an old server and answer `200 text/html`. That is a false "accepted".
- Reusing the route reuses `DeviceAuth`, `MTLSDeviceAuth` and the ingest bucket unchanged.
- The disposition lets the installer print a truthful line (FR-006, X-6).

**Consequences.**
- A 4 KB message is decoded and discarded. Body size is bounded by the existing ingest path.
- The extra-updater and `agent_update_*` branches are untouched.

### AD-003: A reinstall record is a `commands` row with origin `installer`

**Decision.** One accepted notice produces one row:

| Column | Value |
|---|---|
| `action` | `agent_update` |
| `origin` | `installer` (`models.OriginInstaller`) |
| `status` | `running` |
| `created_at`, `updated_at`, `dispatched_at`, `started_at`, `expires_at` | S, the server receive time of the notice |
| `deadline_at` | S + 10m |
| `exec_deadline_s` | 600 |
| `target_keys` | `{}` |
| `params` | `{}` (no versions: DEC-3) |
| `step` | `waiting_for_restart` |
| `actor_kind` | `system` |
| `actor_name`, `actor_auth`, `actor_sub` | `''` |
| `policy_id` | NULL |

No `docker_audit` row is written. `models.IsValidOrigin` is **not** widened, so `Service.Dispatch` can never create one.

Migration 000035 (Section 5) adds:
- the origin value;
- the CHECK `commands_reinstall_shape`: action `agent_update`; status only `running`, `succeeded` or `timed_out`; `dispatched_at` and `deadline_at` set; `deadline_at ≤ dispatched_at + 10 minutes`; `exec_deadline_s ≤ 600`; no target keys; `params = '{}'`;
- a unique partial index allowing at most one `running` row per device;
- a partial index for the 24 h count.

**Rationale.**
- The same reasons as EXPECT-002 AD-001. Action `agent_update` is already in `idx_commands_window_pending`, `ListOpenWindows`, the retention and budget exemptions, the escalation-marker FK and the command history.
- The origin column is the discriminator every path filters on.
- Own indexes give the reinstall its own one-open rule and its own budget (DEC-1), and they allow one open self-update and one open reinstall on the same device (AC-023).

**Alternatives considered.** A new action `agent_reinstall` was rejected for the same reason as EXPECT-002's rejected new action: it rebuilds the EXPECT-001 partial index, changes `ExpectActions()` and its PG predicate test (an EXPECT-001 test), and the retention lists.

### AD-004: Recognition in one transaction (`CreateReinstall`)

**Decision.** `ReinstallRepository.CreateReinstall(ctx, models.ReinstallNotice{DeviceID, Hostname, At})` runs one transaction:

1. **Lock.** `pg_advisory_xact_lock(hashtextextended('riot.cmd.' || device_id, 0))`, the same key as command creation and `CreateSelfUpdate`.
2. **Device online (FR-008 a, DEC-4).** `SELECT status FROM devices WHERE id=$1`. No row, or `!models.DeviceIsLive(status)` → disposition `device_offline`, commit, no record.
3. **Stale open record (NFR-004, EXPECT-002 C-4 b).** If the device's open reinstall row has `deadline_at < At − 1h` (`ReinstallStuckAfter`), retire it in this transaction:
   - `→ timed_out`, `failure_step = unresolved`, `result_msg = not resolved within 1h after the bound`;
   - through `buildTransitionSQL` with `ActorReinstall`;
   - return it as `Retired` for broadcast.
4. **Open record (FR-009).** An open (`running`) row → disposition `open_record`, commit. The window is neither extended nor reopened.
5. **Cap (FR-010).** Count the device's reinstall rows with `created_at > At − 24h`. If the count is ≥ 6 → disposition `capped`. In the same transaction, insert one `agent_reinstall_paused` warning event (`command_id = ''`), unless one with `command_id = ''` already exists for the device with `created_at > At − 24h`. Commit.
6. **Insert (AD-003).** Re-read the row. Disposition `created`.

A unique-index violation (23505) is re-read as `open_record` (as `CreateSelfUpdate`). Any other error returns an error.

`commands.Service.NoteReinstallNotice`:
- calls it;
- broadcasts `Retired`, then `Command` (the Tracker hook plus the dashboard `command_update`);
- logs one `Debug` with the disposition.

On error it returns disposition `unavailable` and writes one throttled Warn through the Service `logThrottle`. When `Options.Reinstalls` is nil it returns `unavailable`. It never returns an error to the handler.

**Rationale.**
- The checks and the insert are atomic under the per-device lock: 50 notices make one row (AC-016), and the cap event is written once.
- Every fact is durable (NFR-009). No memory is held (NFR-005).

**Note.** No command attribution step. An installer run is never the result of a dashboard command.

### AD-005: State model and the restricted writer `ActorReinstall`

**Decision.**

| FRD 7.3 | `status` | `step` | `failure_step` |
|---|---|---|---|
| Open | `running` | `waiting_for_restart` | |
| Closed, `succeeded` (registration before the bound) | `succeeded` | `confirmed` | |
| `not confirmed`, device online at the bound | `timed_out` | | `unconfirmed_online` |
| `not confirmed`, device offline at the bound (escalated) | `timed_out` | | `unconfirmed_offline` |
| Retired as stale | `timed_out` | | `unresolved` |

- **New actor `models.ActorReinstall`** (`bitReinstall`, `String()` = `reinstall`). It has exactly two edges, `running → succeeded` and `running → timed_out`. Neither is a new (from, to) pair in the transition table (the agent and the self-update actor already have `running → succeeded`; the server and the self-update actor already have `running → timed_out`), so the CMD-LIFECYCLE table tests and the EXPECT-002 edge tests are unaffected.
- **Guards in `buildTransitionSQL`**, rendered from typed constants:
  - `ActorSelfUpdate` → `AND origin = 'agent_self_update'` (unchanged);
  - `ActorReinstall` → `AND origin = 'installer'`, and `DeviceID` is mandatory;
  - every other actor → the existing `AND origin <> 'agent_self_update'` plus a new, separate `AND origin <> 'installer'`.

  Together with the CHECK, `ActorReinstall` is the only actor whose UPDATE can match a reinstall row, and it matches nothing else (EXPECT-002 C-2 carried over).
- **Side effects of a reinstall transition** (`Service` methods, Section 6.5): `Transition`, then `broadcast` only. It never calls `completed()`, the policy recorder or the progress gate. It never sets `result_bytes`, `steps`, `target_results`, `stderr_tail`, `exit_code`, `duration_ms` or output (EXPECT-002 C-6). `result_msg` is server text of at most 512 bytes.

### AD-006: Window policy and coverage

**Decision.** `models.ExpectReinstallPolicy`: `Action agent_update`, `Class agent`, `CoversOffline true`, `Containers none`, `Settle 0`, `PostTerminal 0`, `RequireObservedReturn false`, `EscalateNotBack true`, and a new field `Reinstall true`. `SelfUpdate` stays false.

`ExpectPolicyFor(c)` returns:
- the reinstall policy for `origin = installer` with `action = agent_update`;
- the self-update policy for `agent_self_update` (unchanged);
- `ExpectPolicy(action)` otherwise.

**Coverage**, a pure function `expect.evaluateReinstall`, called from `evaluate` when `policy.Reinstall`, before the `SelfUpdate` check. Notation: D = `dispatched_at` = S; `hard = min(deadline_at, D + 10m)`; F = `finished_at`.

| Row state | Phase | Offline coverage ends (exclusive) | `closeAt` (reason) | Activity |
|---|---|---|---|---|
| `running` | `in_progress` | `hard` | `hard` (`hard_bound`) | yes; internal `backBy = hard` feeds the FR-017 "back by" text only |
| `succeeded` | `closed` | `min(F, hard)` | F (`settled`) | no |
| `timed_out` | `closed` | `min(F, hard)` | F (`nonsuccess_settle`) | no |
| due and 24 h past `closeAt` | | | `lapsed` | |

Coverage is strict at its end. Nothing covers at or after `hard` (FR-015, NFR-004).

**Rationale.** FR-015 closes the window at the first registration after S, with no version condition (A-003). The registration hook sets F. No settle period, so a later outage alerts immediately (FR-016, AC-011). The stale-heartbeat push after a fast return is already prevented: a registration counts as a heartbeat (EXPECT-002 X-20).

**Selection with other windows (FR-019, DEC-8).** EXPECT-001's rule is unchanged: each window is evaluated on its own, and the latest D wins the label, the coverage link and the online note.

### AD-007: Registration hook

**Decision.** In `RegisterDevice`, existing-device path, right after `NoteSelfUpdateRegistration` (handlers.go l.406): `h.cmdSvc().NoteReinstallRegistration(r.Context(), device.ID, regAt)`.
- It reads the open reinstall row (`OpenReinstall`).
- If there is none, or the row is not `running`, or `regAt ≥ hard` → nothing.
- Otherwise: `ActorReinstall`, `running → succeeded`, `step = confirmed`, `result_msg = confirmed: the agent registered again`, `finished_at = regAt`. Then broadcast.

**`NoteOnline` for reinstall windows.** The EXPECT-002 rule is extended. tracker.go l.601 reads `(w.policy.SelfUpdate || w.policy.Reinstall) && !tl.coversAt(tl.offlineEnd, offlineSince)` → skip. So the FR-018 note is produced only when the offline that just ended began inside the coverage.

### AD-008: Ending a reinstall window: `finishReinstall`

**Decision.** `finishWindow` delegates to `finishReinstall` when `policy.Reinstall`. It keeps the EXPECT-001 contract:
- it is the only exit;
- every step is idempotent;
- a failing step leaves the entry for the next tick;
- the entry is evicted after 5 min and reloaded;
- nothing runs before `graceEnd`.

Steps:
0. **Detector gate** (EXPECT-002 INFO-4). The existing `detectorGateOpen`, for `hard_bound`, `lapsed` and active rows.
1. **Backstop (FR-021).** If the grace is over, the device is offline, and `offlineSince ∈ [D, offlineEnd)`: `escalateDevice` with kind `EscDeviceNotBack`. The message is `models.ReinstallNotBackMessage(host, id8)`. It goes through the `device_offline` rule, exempt from unrelated cooldowns, under the ceiling, once per record (marker `device`). `markDeviceEscalated` records reason `not_back`, bound `10m` and origin `installer`.
2. **Resolve a still-`running` row at the bound** through `ReinstallResolver.ResolveReinstall` (all `ActorReinstall`):
   - device offline → `timed_out` / `unconfirmed_offline`, `result_msg = not confirmed: <host> was offline 10m after the start`;
   - device online → `timed_out` / `unconfirmed_online`, `result_msg` = the FR-022 text.

   A miss is re-read. Still open → `noteFailure`, retry.
3. **Outcome event (FR-022).** Re-read the row. If `failure_step = unconfirmed_online` and the row is terminal: one `agent_reinstall_unconfirmed` `warning` event, `command_attr = outcome`, message = `result_msg`. Marker `outcome`, written through `recordSelfUpdateEvent` (it is generic). A broadcast-only job (`rule = nil`): never dispatched, never counted against the ceiling.
4. `CloseWindow(id, now, closed | escalated | lapsed)`, then `dropWindow`.
5. **Stuck record (NFR-004).** When step 2 or 3 fails and `now ≥ hard + 1h`: one `agent_reinstall_paused` `warning` event (marker `stuck`, no push) and one throttled Warn `reinstall_stuck`. The entry keeps retrying. The next accepted notice retires the row (AD-004 step 3).

The eager FR-020 pass (`tick.go` l.105) skips reinstall windows as it skips self-update windows.

**Why the Tracker, not the sweeper.**
- `ListOverdue` gains `AND origin <> 'installer'`, and the sweeper gets a defensive Go skip.
- `afterSweep` would write a `command_completed` event and an Info line, against FR-023 and NFR-005.

### AD-009: Every command path honours "never sent to an agent" (FR-011)

Same table as EXPECT-002 AD-007, with origin `installer`:

| Path | Effect on a reinstall row |
|---|---|
| `Dispatch` / `CreateIfNoConflict` | Never creates one (`IsValidOrigin`). No target keys, so never a conflict holder |
| `deliver` / `ClaimByID` / `ClaimQueued` / `expireQueued` / `revertDispatch` | Match `queued` or `dispatched` only, which the CHECK forbids; the server guard excludes the origin too |
| Sweeper `ListOverdue` | Excluded in SQL; defensive Go skip |
| Agent reports (HTTP, WebSocket ack, progress, result) | `ActorAgent` guard → never applied. `classifyRejected` returns `not_found` for origin `installer` (the same branch as `agent_self_update`) |
| Operator cancel | `ActorOperator` guard → 409 `not_cancellable`. The web shows no cancel |
| Retry | No endpoint or control exists |
| History reads, `GetCommand` | Returned read-only |
| `GetCommandOutput` | `[]` |
| Retention, budget purge | Pending rows are exempt (existing predicate). Processed rows go at 90 d. No `result_bytes`, so only the dated purge with its 7-day floor applies |
| EXPECT-001 loaders, `CloseWindow` | Included on purpose. Policy from `ExpectPolicyFor` |
| EXPECT-002 attribution | **Excluded** (AD-017) |
| Markers | `device`, `outcome`, `stuck`; at most 3 per record; cascade |
| Policy recorder, `CommandCompleted` | Never called |

**Actor phrase:** `models.ActorPhrase` returns `by installer` for origin `installer`.

### AD-010: Activity, labels and the record view

**Server.**
- `models.DeviceActivity` gains `origin` (JSON `origin`, string): the origin of the activity's command. It is set in `hostActivityLocked` and in the escalated activity (`openEscalation` gains `origin`, filled by `markDeviceEscalated` and by the reconcile rebuild). It is `""` elsewhere.
- A reinstall window in `in_progress` gives: `kind = agent`, `phase = in_progress`, `action = agent_update`, `origin = installer`, `command_id`, `actor_phrase = by installer`, `started_at = S`, `covers_offline = true`, `back_by = null`, empty agent versions.
- The rebuild (tracker.go l.1050) adds `case pol.Reinstall: bound 10m, reason not_back` (a `timed_out` reinstall row is "not back").
- `windowActionLabel` (selfupdate.go l.47) returns `Agent reinstall` when `w.policy.Reinstall`.

**Web.**

| Condition | Label |
|---|---|
| `agent`, `in_progress`, `origin === 'installer'` | `Reinstalling agent` |
| `escalated`, `not_back`, `origin === 'installer'` | `Not back 10m after Agent reinstall` (FR-021), failure styling |
| actor line | `by installer · started <RelativeTime>` |

- `isAgentUpdating` returns **false** for `origin === 'installer'`. A reinstall has no target version, so the fleet shows the compact `ActivityLabel` row: spinner plus "Reinstalling agent", with "by installer · started HH:MM" in its title (X-1).
- `isExpectedOffline` is unchanged (agent and `covers_offline`).
- `activityLink` is unchanged (agent → modal → `CommandProgress`).

**Record view (FR-026, DEC-6).**
- `utils/reinstall.ts` exports `isReinstall(cmd)` (origin `installer` and action `agent_update`) plus the labels.
- `SelfUpdateDetails` renders a reinstall row: the action label `Agent reinstall`, the actor `by installer`, the start time, the outcome (`running` "In progress", `succeeded` "Succeeded", `timed_out` "Not confirmed"), `result_msg` as text, and the linked events from the existing `GET …/commands/{commandId}/events`. It has no controls.
- `ActivityLog` and `CommandProgress` route reinstall rows to it exactly as they route self-update rows.

### AD-011: Events and messages

**New event types** (`models/events.go`):

| Type | Direction | Stored? | Severity | Bound |
|---|---|---|---|---|
| `agent_reinstall_started` | installer → server | **never** | n/a | n/a |
| `agent_reinstall_unconfirmed` | server | yes, marker `outcome` | warning | ≤ 1 per record |
| `agent_reinstall_paused` | server | yes: cap (`command_id ''`) or stuck (marker `stuck`) | warning | cap ≤ 1 per device per 24 h; stuck ≤ 1 per record |

None is routed through rules or dispatched, and none consumes the ceiling.

**Templates.** `<host>` = `SafeName(hostname)`, or `device` when there is none. `<id8>` = `ShortCommandID`. Times are `15:04` in `time.Local`.

| Use | Text |
|---|---|
| Action label / actor phrase | `Agent reinstall` / `by installer` |
| Suppressed offline (FR-017), existing template | `Device <host> went offline during Agent reinstall by installer (expected, back by <HH:MM of hard>)` |
| Online (FR-018), existing template | `Device <host> came online after Agent reinstall by installer (offline <dur>)` |
| Escalation (FR-021) | `<host> still offline 10m after Agent reinstall by installer (record <id8>)` |
| Not confirmed (FR-022) | `Agent reinstall by installer on <host> was not confirmed within 10m (agent did not restart)` |
| Rate cap (FR-010) | `Agent reinstall tracking paused on <host>: more than 6 reinstalls in 24h; offline alerts during reinstalls are not suppressed` |
| Stuck (NFR-004) | `Agent reinstall tracking paused on <host>: record <id8> (Agent reinstall) could not be completed 1h after its bound; the next reinstall replaces it` |

All texts are at most 512 bytes by construction. The cap event goes through `SanitizeText`, `TruncateHead` and `redact.MaskLine`, as in `CreateSelfUpdate`.

### AD-012: Bounds (Ryan's standing rule)

| Thing | Bound | Cleanup |
|---|---|---|
| Reinstall rows | ≤ 1 `running` per device (unique index). ≤ 6 created per device per rolling 24 h. **≤ 540 per device over 90 d.** At 4 devices: ≤ 24 per day, ≤ 2,160 rows in total | 90 d retention, budget purge (7-day floor) |
| Pending (deletion-exempt) rows | The open row, plus a terminal row for ≤ 1 tick. Past the bound only while the server is down or resolution fails. A stuck row is retired by the next notice, or lapses 24 h after `closeAt` | `finishReinstall`, AD-004 step 3 |
| Bound of a row | `deadline_at ≤ dispatched_at + 10m`, `exec_deadline_s ≤ 600` (CHECK) | n/a |
| `command_escalations` | ≤ 3 per record (`device`, `outcome`, `stuck`) | Cascade |
| Server events per record | Suppressed offline ≤ 1 per live→offline transition inside `[S, S+10m)`; each needs ≥ 45 s without a heartbeat, so ≤ 14, **each replacing today's warning 1:1**. Online notes ≤ 2. ≤ 1 outcome event **or** ≤ 1 escalation. ≤ 1 stuck event. **Total ≤ 18** | 90 d |
| Server events per device | Plus ≤ 1 rate-cap event per rolling 24 h | 90 d |
| Notice requests | Rejected (401/403/400) and refused (`open_record`, `capped`, `device_offline`, `unavailable`) notices store **no row and no event**. The ingest bucket caps the request rate at 120 burst plus 2/s per device | n/a |
| Notifications | ≤ 1 push per record (the escalation), under the EXPECT-001 ceiling (5 per device per hour plus 1 summary). Outcome, cap and stuck events: 0 | n/a |
| Tracker memory | ≤ 1 reinstall window per device (the trimmed row). `closedRecent` ID for 5 min, so released ≤ 5 min after the outcome. No per-notice state; the cap is counted in SQL | Dropped on close |
| Request work | Per notice: 1 device read plus 1 transaction (lock, ≤ 4 selects, ≤ 3 writes). Per registration: 1 select plus ≤ 1 update | n/a |
| Logs | Per notice, accepted or refused: Debug only. Per suppressed transition: Debug (existing). Escalation: 1 Info (existing). Errors: Service `logThrottle` per device, Tracker `warnThrottled` per kind per minute | n/a |
| WebSocket | `command_update` ≤ 3 per record (create, terminal, retire). `activity_update` on change only | n/a |
| Installer, on the device | **0 files left**: the notice dir `$STAGE/notice` (0700, root) is removed at the end of the attempt, and `$STAGE` by the existing `EXIT` trap. 0 log files. Exactly 1 stdout line when a notice is attempted, 0 when skipped under FR-002 | n/a |
| Installer, time | ≤ 10 s per run (AD-016) | n/a |

**Abuse budget (NFR-007; accepted on Ryan's behalf, as EXPECT-002 C-5):**
- A holder of a device's key can delay that device's offline alert by < 10 min per record and < 60 min per rolling 24 h (6 records).
- Combined with EXPECT-002's separate budget, that is < 120 min.
- Every covered transition is stored as `info`. A device still offline at a bound escalates once.
- Records cannot be chained across an outage: a notice for an offline device is refused (DEC-4), and each record escalates at its own bound.
- A notice can never affect another device: `DeviceAuth` binds `{id}`, and every query is device-scoped.

### AD-013: Installer: when and where the notice is sent (FR-001, FR-002)

**Decision.** At install.sh l.990, the existing stop block becomes:

```
if [ "$(systemctl show riot-agent.service -p LoadState --value)" != not-found ]; then
    if [ "$CONFIG_EXISTED" = true ] && [ -z "$RIOT_BOOTSTRAP_KEY" ] && systemctl is-active --quiet riot-agent.service; then
        reinstall_notice || true
    fi
    systemctl stop riot-agent
fi
```

This is the only call site. It is inside the Linux branch, after every pre-stop abort point: lock, signature, assets, unit verification, backup, the candidate and effective sudoers checks, and user and groups. So an aborted run never sends a notice (AC-003).

Skipped silently (no output line, no network): a fresh install; `--bootstrap-key`; a unit that is not loaded or not active; macOS. Inside `reinstall_notice`, an absent or empty `agent.device_id` or `server.api_key` is also a silent skip (FR-002).

The URL given on the command line is **ignored** for the notice. The existing config is kept on a reinstall, and the agent uses its URL.

### AD-014: Installer: the config reader (untrusted input, mawk-safe)

**Decision.** New functions inside `# BEGIN reinstall-notice` and `# END reinstall-notice` markers, placed right after `download_file` (l.427). The tests source this block on its own, as they do the classifier.

- **`notice_snapshot <src> <dst>`.** Requires `[ -f src ] && [ ! -L src ]` and `stat -c %h` = 1. Then `timeout 1 head -c 65537 -- src > dst`, size ≤ 65,536 bytes, no NUL (the `config_facts` checks). `dst` is in `$STAGE/notice` (0700).
- **`notice_facts <snapshot>`** is one awk pass. It prints `name=value` lines for `url`, `key`, `id`, `verify`, `ca`, `cert`, `ckey`, `pin`, or exits non-zero to reject the file.
  - **Whole-file reject:** a CR; a tab in the indentation; a second document (`---` after content, or `...`); a `%` directive; a top-level key seen twice.
  - **Sections:** only top-level `server:` and `agent:` (exactly `name:`, optional ` # comment`). The first indented line fixes the child indent (2 for installer-written files, 4 for `yaml.v3`-written files). Only lines at exactly that indent are read; deeper lines and other sections are ignored.
  - **Fields read:**
    - in `server`: `url`, `api_key`, `tls_verify`, `ca_cert_file`, `client_cert`, `client_key`, `server_cert_pin`;
    - in `agent`: `device_id`.

    A field seen twice → whole-file reject (yaml.v3 refuses duplicate keys, so the agent could not have started).
  - **Accepted value forms:**
    - empty;
    - a plain scalar (after removing ` #…` and trailing spaces) made only of `A-Za-z0-9._:/@+=~%-`;
    - `"…"` with no `"` or `\` inside;
    - `'…'` with no `'` inside;

    each optionally followed by ` # comment`. Anything else (anchors, aliases, tags, flow or block scalars, escapes) for a read field → whole-file reject.
  - **No interval expressions (`{n,m}`) and no repeated groups in any awk regex.** mawk 1.3.4-20200120 (bookworm, DietPi) does not support them. Strict validation happens in bash `[[ =~ ]]`.
- **Validation in bash** (any failure → "not notified (agent.yaml not readable)"):
  - `id`: `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`;
  - `key`: `^[A-Za-z0-9._~+/=-]{16,256}$` (header- and curl-config-safe: no quote, backslash, space, CR or LF);
  - `url`: `^(https?)://([A-Za-z0-9.-]+|\[[0-9A-Fa-f:.]+\])(:[0-9]{1,5})?(/[A-Za-z0-9._~%/-]*)?$`, so no userinfo, query or fragment;
  - `verify`: exactly `true`, `false`, or absent (= `true`, the agent default). Any other spelling → "trust settings not supported";
  - `pin`: absent or `^SHA256:[0-9a-fA-F]{64}$`.
- **Target:** `${url}/api/v1/devices/${id}/events`, concatenated as the agent does (`baseURL + path`, no slash trimming).

### AD-015: Installer: trust parity table (FR-003, Q-2)

| Config (after AD-014) | Agent behaviour | Notice |
|---|---|---|
| `http://…` | Sends the key in clear on every heartbeat | Sent, `--proto =http`. No new exposure (R-2) |
| `https://…`, `/etc/riot/server.crt` present | Root pool = exactly `server.crt`; `tls_verify` and `ca_cert_file` ignored | Sent with `--cacert <snapshot of server.crt>`. A server presenting any other certificate fails the handshake before any header is sent. This is the pin (AC-005) |
| `https://…`, no `server.crt`, `tls_verify: false` | `InsecureSkipVerify` | **Skipped**: "TLS verification is off in agent.yaml" (X-2) |
| `https://…`, no `server.crt`, `server_cert_pin` set | TOFU pin check failed at start; falls back to CA or system roots | **Skipped**: "trust settings not supported" (X-2) |
| `https://…`, no `server.crt`, `ca_cert_file` set | Pool = that file (system roots if unreadable) | Sent with `--cacert <snapshot>` when the path is `/etc/riot/ca.crt` or `/etc/riot/server.crt` and passes `notice_snapshot`. Otherwise **skipped**: "trust settings not supported" |
| `https://…`, no `server.crt`, no CA file | System roots | Sent with curl's default bundle (the same Debian store Go reads) |
| `client_cert` **and** `client_key` set (with any row above) | Presents the pair if it loads | Sent with `--cert` and `--key <snapshots> --pass ''` when the paths are exactly `/etc/riot/client.crt` and `/etc/riot/client.key` and both pass `notice_snapshot`. Otherwise **skipped**: "trust settings not supported" |
| Only one of the pair set | Ignores both | Ignored (parity) |
| `curl` absent (`wget` only, or busybox) | n/a | **Skipped**: "curl not available" (X-2) |

All the `--cacert`, `--cert` and `--key` inputs are snapshots in `$STAGE/notice`, never the live `/etc/riot` paths.

**Rationale.**
- Each "sent" row trusts exactly what the running agent trusts.
- Each "skipped" row is a configuration the stock installer and agent never write. Skipping fails toward alerting (BR-007, NFR-006).
- Restricting file inputs to the four agent-owned paths means root never opens a file elsewhere on the riot user's say-so.

**Alternative rejected.** Running `curl` as the `riot` user (`setpriv`) would give exact file-access parity, but it adds a util-linux dependency and fixture complexity for a threat (a compromised `riot`) that already holds the device key (R-1).

### AD-016: Installer: secret handling, the exact invocation, time bound and output (FR-004 to FR-006, NFR-003)

**Decision.** `reinstall_notice` is a **subshell function**, `reinstall_notice() ( … )`. In order:
1. `{ set +x; } 2>/dev/null`. Xtrace is off before the config is read, so no traced line can contain the key. The subshell does not inherit the `EXIT` trap, so it cannot run `cleanup`.
2. `set +e +o pipefail`; `unset CURL_CA_BUNDLE CURL_HOME SSL_CERT_FILE SSL_CERT_DIR`; `SECONDS=0`.
3. `command -v curl` or reason 1. `NS="$STAGE/notice"; mkdir -m 0700 "$NS"`. Snapshot, parse, validate, then apply AD-015.
4. `left=$((8 - SECONDS))`. If `left < 3` → reason "timed out".
5. **The invocation** (the key goes only through the `printf` builtin into curl's stdin as a curl config line, so it is never in any process's argv, any file or any trace):

```
code=$(printf 'header = "X-rIOt-Key: %s"\n' "$key" |
  timeout -k 1 "$left" curl -q --config - \
    --silent --noproxy '*' --proto "=$scheme" --max-redirs 0 \
    --connect-timeout 5 --max-time "$((left - 1))" \
    $TLS_ARGS \
    --user-agent riot-installer \
    --header 'Content-Type: application/json' \
    --data-binary '{"type":"agent_reinstall_started","severity":"info","message":"Agent reinstall started by installer"}' \
    --max-filesize 4096 --output "$NS/response" --write-out '%{http_code}' \
    --url "$target" 2>/dev/null)
rc=$?
```

   - `-q` must be the first argument (it ignores `.curlrc`).
   - `$TLS_ARGS` is built from AD-015 as an array, expanded as `"${TLS_ARGS[@]}"`:
     - `--tlsv1.2` for https;
     - `--cacert "$NS/ca.pem"`;
     - `--cert "$NS/client.crt" --key "$NS/client.key" --pass ''`.
   - There is no `--location`. The device ID in the URL is not a secret.
6. **Reason mapping** (the fixed set, FR-006; never a response body):

| Condition | Line |
|---|---|
| `rc = 0`, `code = 200`, body (≤ 4096 bytes) matches `"reinstall":"(created\|open_record)"` | `==> Server notified: agent reinstall (offline alerts paused up to 10m)` |
| 200 with `capped` / `device_offline` / `unavailable` | `…not notified (reinstall tracking paused on server)` / `(device already offline on server)` / `(server could not record it)` |
| 200 with anything else, 3xx, rc 63 | `(unexpected server response)` |
| 400, 404, 405 | `(server does not support reinstall notices)` |
| 401, 403 | `(device key rejected)` |
| 429 | `(server busy)` |
| 5xx | `(server error)` |
| rc 6 | `(server name not resolved)` |
| rc 7 | `(server unreachable)` |
| rc 28, 124, 137, or the budget check | `(timed out)` |
| rc 35, 51, 53, 54, 58, 59, 60, 64, 66, 77, 80, 82, 83, 90, 91 | `(TLS verification failed)` |
| other rc ≠ 0 | `(connection failed)` |
| AD-014 / AD-015 skips | `(curl not available)`, `(agent.yaml not readable)`, `(TLS verification is off in agent.yaml)`, `(trust settings not supported)` |

   Every "not" line has the form `==> Server not notified (<reason>); a device-offline alert may follow`.
7. `rm -rf -- "$NS"`; `exit 0`.

**Time bound.**
- Each snapshot is bounded by `timeout 1`, and the budget check (`left = 8 − SECONDS`, skip when below 3) absorbs any delay before curl starts.
- curl is killed at `left` seconds, plus 1 s for `-k`.
- Total ≤ 8 + 1 + 1 s of `SECONDS` granularity, so **≤ 10 s** under any network condition.

**Rationale.**
- No temp file ever holds the key. The only `$STAGE` copy of it is the config snapshot (0700, root, removed at the end and by the trap, as `config_facts` already does).
- Strict validation before the key is written into the curl config line makes config injection (a newline adding `output = …`) impossible.
- `wget` is not used. It can carry headers only on argv or in a wgetrc file, and busybox `wget` has neither the TLS options nor `--config`. The documented install command (`curl … | sudo bash`) already requires curl.

### AD-017: Coexistence with EXPECT-001 and EXPECT-002 (FR-019, FR-027)

**Decision.**
- **EXPECT-002 attribution excludes reinstall rows.** `CreateSelfUpdate` step 1 gains `AND origin <> 'installer'`, and the mock mirrors it. Without this, a self-update started within 10 minutes of a reinstall would be attributed to the reinstall row and get no self-update record. Its restart would then page. That would change EXPECT-002 behaviour, so the exclusion is needed to keep FR-027.
- Both records can be open at once (separate unique indexes). Each reaches its own outcome by its own bound (AC-023). Label, coverage link and online note go to the latest D (DEC-8, EXPECT-001 rule).
- No EXPECT-001/002 constant, wording, cap or index changes. EXPECT-002's guard literals stay byte-identical; the installer guard is a separate, appended clause, so the existing `Contains` assertions still pass.

### AD-018: Clock injection and test safety (NFR-001, NFR-011; EXPECT-002 C-1 carried over)

- The handler uses `h.clock()`. The Service takes `at`. The Tracker uses `Options.Now`. No Go test sleeps.
- **Go scan `TestEX8_AC026_NoPowerUpdateOrAgentExecution`** (`internal/server/expect/safety_ex8_test.go`) reuses `scanSource` and the EX2 forbidden set (imports of `os/exec`, `syscall`, `golang.org/x/sys/unix`, `golang.org/x/sys/windows`, `internal/agent`; `exec.Command`, `exec.CommandContext`, `os.StartProcess`; literals `apt-get`, `apt `, `dpkg`, `dietpi-update`, `riot-agent-apply`, `systemd-run`, plus the EXPECT-001 power patterns). Its file set:
  - (a) every Go file of Section 4.1, asserted by name;
  - (b) every `_test.go` containing `TestEX8_`;
  - (c) every `_test.go` in a package directory that Section 4.1 adds files to;
  - a reviewed `ex8Preexisting` table (file plus pattern plus reason; stale entries fail).

  Self-test: `TestEX8_AC026_ScannerCatchesEachForbiddenForm`. It prints the list. The EX2 scan already covers these directories; new EX8 Go tests must not contain its literals either.
- **Web scan** `web/src/utils/reinstall.safety.test.ts` (describe `EXPECT-008 [AC-026] …`). The set is every test mentioning `EXPECT-008`, plus `utils/reinstall.ts`, `utils/activity.ts`, `components/SelfUpdateDetails.tsx`, `components/ActivityLog.tsx` and `components/docker/CommandProgress.tsx`. It uses the EX2 web patterns, and asserts the three new test files are present.
- **Installer.**
  - No real power, package or agent action. The existing tripwires apply.
  - For every EX8 test, both the old `/usr/local/bin/riot-agent` and the signed candidate asset are tripwires that append `AGENT TRIPWIRE` to the trace (re-signed with the existing `resign()` helper).
  - `systemctl` and `curl` are the fixture stubs.
  - The only network is `127.0.0.1` inside the container.
  - `test_EX8_AC026_no_power_package_or_agent_execution` statically scans the EX8 test methods' source for power verbs, `apt`/`dpkg` and the agent path in an execution position. It also asserts that no EX8 run left `POWER TRIPWIRE`, `AGENT TRIPWIRE`, `PACKAGE` or `UNEXPECTED PACKAGE` lines.

---

## 4. Component Changes

Three parts that touch **disjoint files** and can be built in parallel. The contracts between them are Section 6: 6.1 (installer ↔ server) and 6.3 (server ↔ web).

### 4.1 Part G: Go server (one engineer)

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `cmd/riot-server/migrations/000035_installer_reinstall.up.sql` / `.down.sql` | Section 5 |
| CREATE | `internal/models/reinstall.go` | Constants (`ExpectReinstallBound` 10m, `ReinstallRateCap` 6, `ReinstallRateWindow` 24h, `ReinstallStuckAfter` 1h, `ReinstallExecDeadlineS` 600); step and failure-step tokens; `ReinstallActionLabel`, `ReinstallActorPhrase`; message renderers (AD-011); `ReinstallNotice`, `ReinstallDisposition` (+ 5 values), `ReinstallCreateResult`, `ReinstallOutcome` (+ `State`, `FailureStep`) |
| CREATE | `internal/models/reinstall_test.go` | M8 |
| MODIFY | `internal/models/command_policy.go` | `OriginInstaller = "installer"`; comment that `IsValidOrigin` excludes it |
| MODIFY | `internal/models/command_state.go` | `ActorReinstall`, `bitReinstall`, 2 edges, `String()` |
| MODIFY | `internal/models/expect.go` | `WindowPolicy.Reinstall`, `ExpectReinstallPolicy`, `ExpectPolicyFor` branch, `ActorPhrase` case, `DeviceActivity.Origin` |
| MODIFY | `internal/models/events.go` | Three event types |
| CREATE | `internal/server/db/reinstall_repo.go` | `CreateReinstall` (AD-004), `OpenReinstall` |
| CREATE | `internal/server/db/reinstall_repo_pg_test.go` | R8 |
| MODIFY | `internal/server/db/command_repo.go` | `ActorReinstall` guard and the appended `origin <> 'installer'` clause (AD-005); `ListOverdue` exclusion |
| MODIFY | `internal/server/db/self_update_repo.go` | AD-017 attribution exclusion (one predicate) |
| MODIFY | `internal/server/db/interfaces.go` | `ReinstallRepository`; compile-time assertion for `*CommandRepo` |
| MODIFY | `internal/testutil/mocks.go` | `MockCommandRepo`: `ReinstallRepository` (device-status read from the linked `MockDeviceRepo`, cap event into `MockEventRepo`), guard parity, CHECK and unique-index emulation, `ListOverdue` and attribution exclusions, `FailOn` hooks |
| CREATE | `internal/server/commands/reinstall.go` | `NoteReinstallNotice`, `NoteReinstallRegistration`, `ResolveReinstall` (transitions plus broadcast only) |
| CREATE | `internal/server/commands/reinstall_test.go` | S8 |
| MODIFY | `internal/server/commands/service.go` | `Options.Reinstalls` (nil = feature off); `classifyRejected` `not_found` for origin `installer` |
| MODIFY | `internal/server/commands/sweeper.go` | Defensive skip |
| CREATE | `internal/server/expect/reinstall.go` | `ReinstallResolver`, `SetReinstallResolver`, `evaluateReinstall`, `finishReinstall` (gate, backstop, resolve, outcome, close, stuck) |
| CREATE | `internal/server/expect/reinstall_test.go` | H8 |
| CREATE | `internal/server/expect/safety_ex8_test.go` | AC-026 Go scan and self-test |
| MODIFY | `internal/server/expect/window.go` | `evaluate` delegates on `policy.Reinstall` |
| MODIFY | `internal/server/expect/finish.go` | Delegate to `finishReinstall` |
| MODIFY | `internal/server/expect/tracker.go` | `reinstallResolver` field; `NoteOnline` rule (AD-007); rebuild case and `origin` (AD-010); `openEscalation.origin` |
| MODIFY | `internal/server/expect/tick.go` | Eager FR-020 skips reinstall windows |
| MODIFY | `internal/server/expect/escalate.go` | Reinstall message; `markDeviceEscalated` bound, reason, origin |
| MODIFY | `internal/server/expect/activity.go` | `Origin` on host and escalated activities; `BackBy = nil` for reinstall |
| MODIFY | `internal/server/expect/selfupdate.go` | `windowActionLabel` reinstall branch only |
| MODIFY | `internal/server/expect/harness_test.go` | Additive helpers `reinstallNotice(dev)`, reusing `register`, `detectorPass`, `restart` |
| CREATE | `internal/server/handlers/agent_event_reinstall.go` | `receiveReinstallNotice` (AD-002) |
| CREATE | `internal/server/handlers/agent_event_reinstall_test.go` | HT8 (through the real router and `DeviceAuth`) |
| MODIFY | `internal/server/handlers/handlers.go` | `ReceiveAgentEvent` case; `RegisterDevice` hook (AD-007); `cmdSvc` wires `opts.Reinstalls` via a type assertion (as `SelfUpdates`) |
| MODIFY | `internal/server/server.go` | `Reinstalls: s.CommandRepo`; `s.Expect.SetReinstallResolver(s.Commands)` before `Load` |

No router, agent, sudoers or dependency change.

### 4.2 Part I: Installer and its CI (one engineer)

| Action | File Path | Purpose |
|---|---|---|
| MODIFY | `scripts/install.sh` | The `# BEGIN/END reinstall-notice` block (`notice_snapshot`, `notice_facts`, `reinstall_notice`) after `download_file`; the call site at l.990 (AD-013 to AD-016). Nothing else changes |
| MODIFY | `scripts/test_install_maintenance.py` | **setUpClass:** copy the real `/usr/bin/curl` to `/usr/bin/curl.fixture-real` (once) before writing the stub. The stub gains a notice branch, taken when argv contains `--config`. It appends `NOTICE argv=<argv> parent=<cat /proc/$PPID/cmdline>` and `NOTICE header-sha256=<sha256 of the stdin header line>` to the trace (**never the raw key**). Then: with `/fixture/notice-real` present it execs the real curl with the same stdin; otherwise it emulates `/fixture/notice-mode` (`ok:<disposition>`, `http:<code>`, `rc:<n>`), printing the code and writing the body to `--output`. The `systemctl` stub gains `is-active) [ -f /fixture/agent-active ] && exit 0 \|\| exit 3`. Existing behaviour is unchanged when the flag is absent. **New helpers:** a local `127.0.0.1` HTTP/HTTPS listener in a thread (python `http.server` plus `ssl`; certificates from `openssl req -x509` with SAN `IP:127.0.0.1,DNS:localhost`; optional client-CA verification for mTLS). It records path and `sha256(X-rIOt-Key)`, answers per scenario, and has a never-answering mode and a closed port. **New tests** `test_EX8_*` (Section 8) |
| MODIFY | `.github/workflows/ci.yml` | "Signed installer…" step: apt line adds `curl gawk`, then `update-alternatives --set awk /usr/bin/mawk`, so full runs stay on mawk while the agreement test can also use gawk (AC-007). `--network` usage unchanged |

### 4.3 Part W: Web (one engineer)

| Action | File Path | Purpose |
|---|---|---|
| MODIFY | `web/src/types/models.ts` | `CommandOrigin` adds `'installer'`; `DeviceActivity.origin?: CommandOrigin \| ''` |
| CREATE | `web/src/utils/reinstall.ts` | `isReinstall`, `REINSTALL_ACTION_LABEL` "Agent reinstall", `REINSTALL_ACTOR_PHRASE` "by installer", `REINSTALL_ACTIVITY_LABEL` "Reinstalling agent", `reinstallOutcomeLabel` |
| CREATE | `web/src/utils/reinstall.test.ts` | WR |
| CREATE | `web/src/utils/reinstall.safety.test.ts` | AC-026 web scan |
| MODIFY | `web/src/utils/activity.ts` | AD-010 labels; `isAgentUpdating` false for installer |
| MODIFY | `web/src/utils/activity.test.ts` | Additive `EXPECT-008 [AC-0xx]` cases |
| MODIFY | `web/src/components/SelfUpdateDetails.tsx` | Reinstall variant: label, actor, outcome wording; same test IDs; no controls; `scrollbar-thin` kept |
| CREATE | `web/src/components/SelfUpdateDetails.reinstall.test.tsx` | WS8 |
| MODIFY | `web/src/components/ActivityLog.tsx` | Row label, actor and outcome for reinstall rows; the read-only modal |
| MODIFY | `web/src/components/docker/CommandProgress.tsx` | Route reinstall rows to the record view |
| MODIFY | `web/src/components/ui/labels.ts` | `agent_reinstall_unconfirmed` "Agent reinstall not confirmed", `agent_reinstall_paused` "Agent reinstall tracking paused"; origin label `installer` "Installer" if the origin map exists |
| CREATE | `web/src/pages/FleetOverview.reinstall.test.tsx`, `web/src/pages/DeviceDetail.reinstall.test.tsx` | WF8, WD8 |

### 4.4 Part D: Docs (after G, I and W; the technical writer reviews)

| Action | File Path | Purpose |
|---|---|---|
| MODIFY | `README.md` | FR-030 (Section 12 note 9). EXPECT-002 strings asserted by `expect2_docs_test.go` must stay |
| MODIFY | `CHANGELOG.md` | Entry with the Section 14 steps |
| CREATE | `internal/server/expect8_docs_test.go` | D8 |

---

## 5. Data Model Changes

### 5.1 `000035_installer_reinstall.up.sql`

```sql
ALTER TABLE commands DROP CONSTRAINT commands_origin_check;
ALTER TABLE commands ADD CONSTRAINT commands_origin_check
  CHECK (origin IN ('user','auto_update','auto_patch','agent_self_update','installer'));

ALTER TABLE commands ADD CONSTRAINT commands_reinstall_shape CHECK (
  origin <> 'installer' OR (
    action = 'agent_update'
    AND status IN ('running','succeeded','timed_out')
    AND dispatched_at IS NOT NULL
    AND deadline_at IS NOT NULL
    AND deadline_at <= dispatched_at + INTERVAL '10 minutes'
    AND exec_deadline_s <= 600
    AND cardinality(target_keys) = 0
    AND params = '{}'::jsonb));

CREATE UNIQUE INDEX idx_commands_reinstall_open ON commands (device_id)
  WHERE origin = 'installer' AND status = 'running';

CREATE INDEX idx_commands_reinstall_recent ON commands (device_id, created_at)
  WHERE origin = 'installer';
```

There are no existing rows with this origin, so validation is instant (< 1 s at hawaii's size).

### 5.2 `000035_installer_reinstall.down.sql`

```sql
DELETE FROM commands WHERE origin = 'installer';   -- markers cascade
DROP INDEX IF EXISTS idx_commands_reinstall_recent;
DROP INDEX IF EXISTS idx_commands_reinstall_open;
ALTER TABLE commands DROP CONSTRAINT IF EXISTS commands_reinstall_shape;
ALTER TABLE commands DROP CONSTRAINT commands_origin_check;
ALTER TABLE commands ADD CONSTRAINT commands_origin_check
  CHECK (origin IN ('user','auto_update','auto_patch','agent_self_update'));
```

Existing PG tests that migrate down to 32 or 33 pass through this down unchanged.

### 5.3 Before and after

- **`commands`.** No column change. One origin value, one CHECK and two partial indexes.
- **`events`.** No column change. Two new stored types.
- **`command_escalations`.** Existing targets only.
- **`devices`.** None.

---

## 6. API / Interface Contract

### 6.1 `POST /api/v1/devices/{id}/events` with `type = agent_reinstall_started` (installer ↔ server)

**Request.**
- `X-rIOt-Key: <device key>` (required); `Content-Type: application/json`.
- Body: `{"type":"agent_reinstall_started","severity":"info","message":"Agent reinstall started by installer"}`. Only `type` is read; every other field is ignored.

**Responses.**

| Status | Body | When |
|---|---|---|
| 200 | `{"status":"ok","reinstall":"created"}` | Record opened |
| 200 | `{"status":"ok","reinstall":"open_record"}` | A record is already open |
| 200 | `{"status":"ok","reinstall":"capped"}` | 6 in 24 h |
| 200 | `{"status":"ok","reinstall":"device_offline"}` | The device is offline on the server |
| 200 | `{"status":"ok","reinstall":"unavailable"}` | Store error, feature not wired, or identity mismatch |
| 400 | `{"error":"invalid request body"}` | Body is not JSON |
| 401 | `{"error":"missing X-rIOt-Key header"}` / `{"error":"invalid api key"}` | `DeviceAuth` |
| 403 | `{"error":"api key does not match device"}` | `DeviceAuth` |
| 429 | existing ingest body | Bucket |

**Old servers** answer 400 `{"error":"unsupported event type"}`.

The response struct has fixed field order (`status`, then `reinstall`). The installer matches the token without depending on the order.

### 6.2 Commands API for reinstall rows

| Endpoint | Behaviour |
|---|---|
| `GET /api/v1/devices/{id}/commands[?…]`, `GET …/commands/{commandId}` | 200, `origin: "installer"`, `params: {}` |
| `POST …/cancel` | 409 `not_cancellable` |
| `POST …/report` (device key), WebSocket ack, progress and result | 404 `command_not_found` / receipt `not_found` |
| `GET …/output` | `[]` |
| `GET …/commands/{commandId}/events` (EXPECT-002 6.3) | Unchanged. Offline info (`expected`), online note, escalation and outcome or stuck (`outcome`) events are classified server-authored. No agent events are ever linked |

### 6.3 `GET /api/v1/activity` (server ↔ web)

`DeviceActivity` gains `origin: string`. For a reinstall window:
- `kind: "agent"`, `phase: "in_progress"`, `action: "agent_update"`, `origin: "installer"`;
- `actor_phrase: "by installer"`;
- `covers_offline: true`, `back_by: null`, `agent_from_version: ""`, `agent_to_version: ""`.

For its escalation:
- `kind: "escalated"`, `origin: "installer"`;
- `escalation: {"reason": "not_back", "bound": "10m", "at": …}`.

The `activity_update` WebSocket payload is the same DTO.

### 6.4 Go interfaces

```go
// db
type ReinstallRepository interface {
    CreateReinstall(ctx context.Context, n models.ReinstallNotice) (models.ReinstallCreateResult, error)
    OpenReinstall(ctx context.Context, deviceID string) (*models.Command, error)
}
// models
type ReinstallNotice struct { DeviceID, Hostname string; At time.Time }
type ReinstallDisposition string // created | open_record | capped | device_offline | unavailable
type ReinstallCreateResult struct {
    Disposition ReinstallDisposition
    Command     *Command // created row
    Retired     *Command // stale open row retired in the same tx
    CapEvent    *Event
}
type ReinstallOutcome string // unconfirmed_online | unconfirmed_offline
// commands.Service
type ReinstallNoticeInput  struct { DeviceID, Hostname string; At time.Time }
type ReinstallNoticeResult struct { Disposition models.ReinstallDisposition; Events []models.Event }
func (s *Service) NoteReinstallNotice(ctx context.Context, in ReinstallNoticeInput) ReinstallNoticeResult
func (s *Service) NoteReinstallRegistration(ctx context.Context, deviceID string, at time.Time)
func (s *Service) ResolveReinstall(ctx context.Context, commandID, deviceID string, outcome models.ReinstallOutcome, message string, at time.Time) (*models.Command, bool, error)
// expect
type ReinstallResolver interface { ResolveReinstall(...same...) (*models.Command, bool, error) }
func (t *Tracker) SetReinstallResolver(r ReinstallResolver) // before Load; never a typed nil
```

There is no import cycle (same layering as EXPECT-002).

---

## 7. Sequence / Flow

### 7.1 Re-run on a registered device (the 2026-10-27 timeline, AC-008)

1. 13:14:xx: the installer passes every check. `systemctl is-active riot-agent` → active.
2. `reinstall_notice` runs: snapshot, parse, validate, trust (server.crt present → `--cacert`).
3. 13:15:00: curl POSTs → `DeviceAuth` → `ReceiveAgentEvent` → `receiveReinstallNotice` → `CreateReinstall` (online, no open record, count 0) → row R (`running`, D = 13:15:00, deadline 13:25:00) → broadcast → `Tracker.CommandChanged` → `200 created`.
4. The installer prints "Server notified …" and runs `systemctl stop riot-agent`.
5. The next tick sends `activity_update`: "Reinstalling agent", "by installer", 13:15.
6. 13:15:50: the detector marks the device offline. `CoverOffline(13:15:50)` → R covers `[13:15:00, 13:25:00)` → `info` event "…went offline during Agent reinstall by installer (expected, back by 13:25)", linked. No push.
7. 13:16:10: the new agent registers. `LastHeartbeat = at` → `Update` → `NoteRegistration` → `NoteSelfUpdateRegistration` (no record) → **`NoteReinstallRegistration` → R `succeeded`, F = 13:16:10** → broadcast → `DeviceOnline` → `NoteOnline` (offlineSince 13:15:50 ∈ `[D, F)`) → "…came online after Agent reinstall by installer (offline 20s)".
8. The next tick: R is due at F → `finishReinstall` (no backstop, no resolution, no outcome event) → close → `activity_update` null.

### 7.2 Not back (AC-012)

At 13:25:00, R is due (`hard_bound`) → gate → backstop: the device is offline, offlineSince ∈ `[D, hard)` → one escalation (marker `device`) → resolve `timed_out` / `unconfirmed_offline` → no outcome event → close `escalated`. The label reads "Not back 10m after Agent reinstall" until the device registers or 24 h pass.

### 7.3 Online at the bound, never restarted (AC-013)

At 13:25:00: no backstop → resolve `timed_out` / `unconfirmed_online` (FR-022 text) → one `agent_reinstall_unconfirmed` warning (marker `outcome`, broadcast only) → close.

### 7.4 Server down across the bound (AC-019)

Restart → migration (no-op) → `Load` (R pending) → grace → the detector's one-shot pass opens the gate → R is processed as in 7.2 within 2 minutes after start. The marker makes a second restart a no-op.

### 7.5 Server start wiring

1. Migration 000035.
2. `Tracker = expect.New(…)`.
3. `svc = commands.New(…, SelfUpdates: CommandRepo, Reinstalls: CommandRepo)`.
4. `SetSelfUpdateResolver(svc)`; `SetReinstallResolver(svc)`.
5. `Load` → `Run`.

### 7.6 Mixed versions (AC-024)

- New installer, old server: 400 → "Server not notified (server does not support reinstall notices); a device-offline alert may follow" → the install continues → the offline pages as today.
- Old installer, new server: no request → no record → it pages as today.

---

## 8. Acceptance Criteria Mapping

**Test files.**
- Go:
  - **M8** = `internal/models/reinstall_test.go`;
  - **R8** = `internal/server/db/reinstall_repo_pg_test.go` (PostgreSQL 16);
  - **S8** = `internal/server/commands/reinstall_test.go`;
  - **H8** = `internal/server/expect/reinstall_test.go` (the EXPECT-001 harness: real Generator, Tracker and Service, fake clock, recording dispatcher, captured slog, `restart()`, `detectorPass()`);
  - **HT8** = `internal/server/handlers/agent_event_reinstall_test.go` (through the real router and `DeviceAuth`);
  - **Z8** = `internal/server/expect/safety_ex8_test.go`;
  - **D8** = `internal/server/expect8_docs_test.go`.
- **I** = `scripts/test_install_maintenance.py`, methods `test_EX8_*`. They run in CI on bookworm-slim and trixie-slim.
- Web:
  - **WA** = `utils/activity.test.ts`;
  - **WR** = `utils/reinstall.test.ts`;
  - **WZ8** = `utils/reinstall.safety.test.ts`;
  - **WS8** = `components/SelfUpdateDetails.reinstall.test.tsx`;
  - **WF8** = `pages/FleetOverview.reinstall.test.tsx`;
  - **WD8** = `pages/DeviceDetail.reinstall.test.tsx`.

Go tests are named `TestEX8_…`. Web `describe` titles start with `EXPECT-008 [AC-0xx]`. Python tests start with `test_EX8_AC0xx_`.

| AC | Fulfilled by | Tests |
|---|---|---|
| AC-001 | AD-013, AD-016 | I: `test_EX8_AC001_one_notice_after_validation_before_stop` (real curl to the local HTTPS listener with a pinned `server.crt`: exactly one request; the trace order is `INSTALLED /etc/sudoers.d/riot-agent` < `NOTICE` < `systemctl stop riot-agent`; header sha256 equals the expected key's), `test_EX8_AC001_success_line_once_and_exit_status_unchanged` |
| AC-002 | AD-013, AD-014 | I: `test_EX8_AC002_no_notice_without_identity_or_running_agent` (subTests: fresh install, `--bootstrap-key`, no `device_id`, no `api_key`, inactive unit: no `NOTICE` line, no notice output line, exit status and installed files as for the same run with the notice code removed) |
| AC-003 | AD-013 placement | I: `test_EX8_AC003_abort_before_stop_sends_no_notice` (subTests: invalid signature, unit validation failure, candidate policy failure, effective policy failure; agent active: no `NOTICE`, no `systemctl stop`) |
| AC-004 | AD-016 | I: `test_EX8_AC004_server_failures_never_block_install` (emulated rc 7, rc 28, http 500, http 404, rc 6: the matching reason line, later steps done, exit 0), `test_EX8_AC004_curl_runs_under_the_time_bound` (recorded parent argv starts `timeout -k 1 8`; own argv has `--connect-timeout 5 --max-time 7`, `-q` first, `--max-redirs 0`, `--noproxy *`), `test_EX8_AC004_real_hang_ends_within_10s` (real curl to a listener that accepts and never answers: the notice phase from the `NOTICE` trace line to `systemctl stop` is ≤ 10.5 s; X-4) |
| AC-005 | AD-015 | I: `test_EX8_AC005_untrusted_certificate_no_key_sent` (real curl, `tls_verify: true`, no `server.crt`, listener with a self-signed certificate not in the default bundle: the listener records 0 HTTP requests; one "TLS verification failed" line), `test_EX8_AC005_pinned_certificate_mismatch_no_key_sent` (`server.crt` = certificate A, listener presents B: 0 requests), `test_EX8_AC005_mtls_presents_client_certificate` (the listener requires a client certificate from a test CA: request received with the certificate; with `client.key` a symlink → no request, "trust settings not supported"), `test_EX8_AC005_tls_verify_off_skipped`, `test_EX8_AC005_pin_without_pinned_certificate_skipped`, `test_EX8_AC005_paths_outside_etc_riot_skipped` |
| AC-006 | AD-016 | I: `test_EX8_AC006_key_never_in_argv_output_or_files` (a random marker key; runs with the listener accepting and with a closed port, each also under `bash -x`; argv-recording wrappers installed for the test's duration around `awk`, `head`, `stat`, `wc`, `tr`, `timeout`, `mkdir`, `rm` and `cat`, plus the curl stub's own and parent argv: the marker is in none; not in stdout or stderr; `grep -r` over `/tmp /var /run /root /etc /fixture /usr/local`, excluding `/etc/riot` and `/var/backups/riot_install.*`, finds nothing; `$STAGE` is gone), `test_EX8_AC006_key_reaches_only_the_printf_builtin` (static: in the `reinstall-notice` block the key variable appears only in its assignment, its `[[ =~ ]]` check and the `printf … \| … curl -q --config -` line) |
| AC-007 | AD-014 mawk rules; CI | I: `test_EX8_AC007_reader_agrees_on_every_awk` (mawk and gawk required, busybox if present; a table of 2-space quoted, 4-space yaml.v3, comments, duplicates, anchors, CR, tabs, flow maps, second document → identical facts or identical reject), `test_EX8_AC007_full_runs_use_mawk` (`readlink -f $(command -v awk)` ends in `mawk`) |
| AC-008 | AD-002 to AD-007, AD-011 | H8: `TestEX8_AC008_TimelineNoNotification`, `TestEX8_AC008_LabelFromNoticeUntilRegistration`; HT8: `TestEX8_AC008_NoticeCreatesRecordStoresNoEvent` (200 `created`, the events table unchanged) |
| AC-009 | Per-device records | H8: `TestEX8_AC009_TwoDevicesBackToBackZeroPushes` |
| AC-010 | `Containers: none` | H8: `TestEX8_AC010_ContainerDeathDuringReinstallAlerts` |
| AC-011 | Coverage ends at F | H8: `TestEX8_AC011_OfflineAfterCloseAlerts` |
| AC-012 | AD-008 step 1, AD-010 | H8: `TestEX8_AC012_NotBackEscalatesOnce`, `TestEX8_AC012_EscalationIgnoresUnrelatedCooldown`, `TestEX8_AC012_NoSecondEscalationAfterRestart`, `TestEX8_AC012_EscalatedLabelAndOfflineCount`; WA: `EXPECT-008 [AC-012] not-back label names the reinstall` |
| AC-013 | AD-008 steps 2-3 | H8: `TestEX8_AC013_NotConfirmedWhileOnlineWarningNoPush` |
| AC-014 | `DeviceAuth`, AD-002 | HT8: `TestEX8_AC014_RejectedNoticesCreateNothing` (no key, wrong key, revoked key, PiCUPS key naming PiVPN: 401/401/401/403, no row, no event); H8: `TestEX8_AC014_OfflineAfterRejectedNoticeAlerts` |
| AC-015 | AD-004 step 2 | S8: `TestEX8_AC015_DeviceOfflineNoRecord`; R8: `TestEX8_AC015_PG_OfflineDeviceNoRow` |
| AC-016 | AD-004, unique index | S8: `TestEX8_AC016_FiftyNoticesOneRecord`, `TestEX8_AC016_RateCapSixPerDayOneWarning`; R8: `TestEX8_AC016_PG_ConcurrentNoticesOneRecordOneCapEvent`; H8: `TestEX8_AC016_CappedNoticeOfflineAlertsNoLogAboveDebug` |
| AC-017 | AD-002 ignores fields | HT8: `TestEX8_AC017_UntrustedFieldsIgnored` (10 KB message, controls, bidi, invalid UTF-8, extra keys: same disposition, same row, nothing stored from them) |
| AC-018 | Fail open | H8: `TestEX8_AC018_StoreFailureFailsOpen`; HT8: `TestEX8_AC018_RepoErrorAnswersUnavailable` |
| AC-019 | Durable row, gate, markers | H8: `TestEX8_AC019_RestartMidWindowSucceeds`, `TestEX8_AC019_BoundDuringDowntimeEscalatesOnce` |
| AC-020 | AD-004 step 3, AD-008 step 5 | H8: `TestEX8_AC020_ResolverUnsetFailsSafe` (no resolver: coverage ends at `hard`, escalation fires, at `hard + 1h` one `agent_reinstall_paused`, no push, one Warn, also across a restart); S8: `TestEX8_AC020_StaleOpenRecordRetiredByNextNotice`; R8: `TestEX8_AC020_PG_StaleRetireInTx` |
| AC-021 | AD-012 | H8: `TestEX8_AC021_BoundedEventsLogsMemory` (AC-008/012/013 timelines: the row and event counts of the FRD; only Info line = the escalation; the window is dropped and `closedRecent` empty 5 min after the outcome); I: `test_EX8_AC021_no_file_left_behind` (file-system listing before and after differs only by the documented install paths and the backup) |
| AC-022 | AD-009, AD-010 | WF8: `EXPECT-008 [AC-022] fleet row shows Reinstalling agent with actor and start in the title, counts expected offline, dot offline`; WD8: `EXPECT-008 [AC-022] header label, actor line, link opens the read-only record`; WS8: `EXPECT-008 [AC-022] record view shows label, actor, start, outcome, events and no controls`; WA: `EXPECT-008 [AC-022] label clears on activity_update null`; WR: `EXPECT-008 [AC-022] isReinstall and outcome labels`; S8: `TestEX8_AC022_NeverClaimedOrDelivered`, `TestEX8_AC022_AgentReportNotFoundHTTPAndWS`, `TestEX8_AC022_CancelNotCancellable` |
| AC-023 | AD-017 | H8: `TestEX8_AC023_ReinstallDuringSelfUpdate`; S8: `TestEX8_AC023_SelfUpdateStartNotAttributedToReinstall`; R8: `TestEX8_AC023_PG_SelfUpdateAttributionIgnoresReinstall` |
| AC-024 | AD-002 | I: `test_EX8_AC024_new_installer_against_old_server` (listener answers 400 `unsupported event type`, then 404: one "server does not support reinstall notices" line each, exit and files unchanged); H8: `TestEX8_AC024_ReinstallWithoutNoticeAlertsAsToday` |
| AC-025 | AD-017 | Every existing `TestEX_*`, `TestEX2_*`, CMD-LIFECYCLE, web EXPECT-001/002 and installer fixture test passes **unmodified**; H8: `TestEX8_AC025_SelfUpdateAndCommandWindowsUnchanged` |
| AC-026 | AD-018 | Z8: `TestEX8_AC026_NoPowerUpdateOrAgentExecution`, `TestEX8_AC026_ScannerCatchesEachForbiddenForm`; WZ8: `EXPECT-008 [AC-026] new web tests and sources spawn nothing`; I: `test_EX8_AC026_no_power_package_or_agent_execution` |
| AC-027 | Section 14, Part D | D8: `TestEX8_AC027_ReadmeAndChangelog`. This ADD's Section 14 holds the list; Clod copies it into the PR body |

### 8.1 Supporting tests

| Topic | Test |
|---|---|
| Actor edges and guards | M8: `TestEX8_ActorReinstallEdges`, `TestEX8_ExistingActorEdgesUnchanged`; R8: `TestEX8_PG_Guards` (agent, operator, server and self-update cannot update a reinstall row; `ActorReinstall` cannot update an ordinary or self-update row); S8/mock: `TestEX8_MockGuardParity` |
| CHECK and migration | R8: `TestEX8_PG_Migration000035UpDownUp`, `TestEX8_PG_CheckShape` (queued, failed, deadline > D+10m, exec 601, a target key, non-empty params: each rejected), `TestEX8_PG_UniqueOpenRecord`, `TestEX8_PG_OriginCheck` |
| Sweeper | S8: `TestEX8_SweeperSkipsReinstall`; R8: `TestEX8_PG_ListOverdueExcludesReinstall` |
| No completion side effects | S8: `TestEX8_TransitionsLeaveResultColumnsAndGateUntouched`, `TestEX8_NoCompletionEventForReinstall` |
| Outcome jobs never dispatched | H8: `TestEX8_OutcomeEventsNeverDispatchedNorCeilinged` |
| Registration after the bound | S8: `TestEX8_RegistrationAfterBoundIgnored` |
| Online note only for covered offline | H8: `TestEX8_NoteOnlineOnlyForCoveredOffline` |
| Messages | M8: `TestEX8_MessagesMatchFRD` (each AD-011 template, `SafeName` host, ≤ 512 bytes) |
| Reader rejects injection | I: `test_EX8_reader_rejects_values_that_could_inject_curl_config` (key with `"`, `\`, a space, or a newline via a quoted scalar → "agent.yaml not readable", no curl) |

### 8.2 Inherited security-condition tests

| Condition | Tests |
|---|---|
| EXPECT-002 C-1 (scan coverage) | Z8 and WZ8 above (named `must` list, three-part set, preexisting table, self-test) |
| EXPECT-002 C-2 (restricted writer, CHECK incl. bound and keys, server guard) | `TestEX8_PG_Guards`, `TestEX8_PG_CheckShape`, `TestEX8_MockGuardParity`, `TestEX8_ActorReinstallEdges` |
| EXPECT-002 C-3 (truthful record view) | WS8 (server events shown; reinstall rows have no agent events) |
| EXPECT-002 C-4 (stuck) | `TestEX8_AC020_*` |
| EXPECT-002 C-5 (budget recorded) | `TestEX8_AC027_ReadmeAndChangelog` asserts the budget text |
| EXPECT-002 C-6 / EXPECT-001 C-8 (QA gate) | Section 13 |
| EXPECT-001 C-2 (no window end without backstop) | `TestEX8_AC012_*`, `TestEX8_AC020_ResolverUnsetFailsSafe` |
| EXPECT-001 C-5 (ceiling) | `TestEX8_OutcomeEventsNeverDispatchedNorCeilinged`; the escalation goes through `t.escalate` |
| EXPECT-001 C-6 (SafeName) | `TestEX8_MessagesMatchFRD` with a hostile hostname |

---

## 9. Error Handling

| Failure | Behaviour | Log / output |
|---|---|---|
| **Installer:** `curl` missing, config unreadable, invalid value, unsupported trust | No request; one "not notified (<reason>)" line; the install continues | stdout line only |
| **Installer:** DNS, refused, TLS, timeout, HTTP error, old server | As above, per the AD-016 table; the install continues; exit status unchanged | stdout line only |
| **Installer:** `systemctl stop` fails after an accepted notice | The install aborts as today; the device stays online; the record ends `not confirmed` with one warning, no push (NFR-010) | existing `ERROR:` |
| **Installer:** abort after the stop | The agent is left stopped (existing message); the device escalates once at the bound | existing `ERROR:` |
| Notice body not JSON | 400, as today | none |
| Hostname read fails | `device` in messages | Debug |
| `CreateReinstall` fails | `unavailable`; no record; the offline alerts as today | Warn (Service `logThrottle`) |
| Unique-index race | Re-read → `open_record` | Debug |
| Registration hook fails | Record ends at the bound (`not confirmed`, one warning) | Warn (throttled) |
| `finishReinstall` step fails | Retry each tick; evict and reload after 5 min; coverage still ends at `hard`; the backstop is not delayed | Warn (per kind) |
| Resolver unset or keeps failing | At `hard + 1h`: one `agent_reinstall_paused` event (marker `stuck`), no push; the next notice retires the row | Warn `reinstall_stuck` (throttled) |
| Tracker not ready | `CoverOffline` fails open (existing) | existing |
| Publisher queue full | Event stored; push or broadcast dropped (EXPECT-001 R-3) | Warn |

---

## 10. Security Considerations

| Vector | Mitigation |
|---|---|
| Device key exposure on the device (NFR-003) | The key passes only through the `printf` builtin into curl's stdin as a curl config line; it is never on any argv (curl, timeout, awk, head). Xtrace is off inside the subshell. Output is fixed text. No temp file holds it, except the root-only 0700 config snapshot, removed at the end and by the trap (pre-existing `config_facts` practice). curl's stderr is discarded |
| Config injection (riot-writable `agent.yaml` read by root) | Strict bash validation of each field before use. The key charset excludes quote, backslash, space, CR and LF, so no extra curl config line can be smuggled. The URL charset excludes spaces and quotes. The ID is a canonical UUID (no path traversal). `-q` ignores `.curlrc`. The proxy and CA environment variables are unset |
| Root reading riot-chosen files | Only the four agent paths in `/etc/riot`, regular files with one link, no symlink, ≤ 64 KiB, snapshotted into `$STAGE`. Residual symlink race R-1 |
| Key sent to the wrong server | Only to `server.url` under the agent's own trust anchor (AD-015). Configurations without verification are skipped. No redirects |
| Forged or replayed notice suppresses alerts (NFR-007) | `DeviceAuth`. ≤ 1 open record. A 10-minute bound in a DB CHECK. 6 per 24 h. Refused when the device is already offline (no chaining across an outage). Escalation at each bound. Every covered transition stored as `info`. Budget AD-012 |
| Cross-device effect | `DeviceAuth` binds `{id}`, re-checked in the handler. Every query and the coverage are device-scoped |
| Writes to a record by anything else | `ActorReinstall` is the only matching actor. Guards on all other actors. CHECK on status, bound, keys and params. `classifyRejected` returns `not_found`. `IsValidOrigin` excludes the origin |
| Notice flooding | Ingest bucket. One short transaction under the per-device lock. Debug logs only. Nothing stored for refused notices |
| Untrusted notice fields | Ignored entirely (nothing displayed comes from the notice) |
| Old server misreading the notice | It answers 400 before storing (verified) |
| Test safety (NFR-001) | AD-018 scans, tripwires, stubs, loopback-only listener |

---

## 11. Performance Considerations

| Path | Added cost | Index |
|---|---|---|
| Notice | 1 PK read plus 1 transaction (lock, ≤ 4 selects, ≤ 3 writes) | `devices_pkey`, `idx_commands_reinstall_open`, `idx_commands_reinstall_recent`, `idx_events_device_time` |
| Registration | 1 select (open row) plus ≤ 1 update | `idx_commands_reinstall_open` |
| Tracker | ≤ 1 window per device; ≤ 5 DB calls per record end | existing |
| Sweeper | One more predicate | existing |
| Installer | ≤ 10 s worst case; typically < 1 s | n/a |

NFR-008's 100 ms p95 has wide headroom at 4 devices. No index beyond Section 5.

---

## 12. Implementation Notes for Engineers

1. **Lock discipline** as EXPECT-002 note 1: `broadcast` → `CommandChanged` takes `t.mu` briefly, never `tickMu`.
2. **`RegisterDevice` order:** `LastHeartbeat` → `Update` → `NoteRegistration` → `NoteSelfUpdateRegistration` → **`NoteReinstallRegistration`** → response and broadcast → `DeviceOnline`.
3. **Typed nils.** `cmdSvc` assigns `opts.Reinstalls` only through a successful type assertion, and `server.go` passes the concrete `*CommandRepo`.
4. **Guards.** Append the installer clause; do **not** rewrite the existing `origin <> 'agent_self_update'` literal (existing `Contains` tests). Mirror everything in `MockCommandRepo`; `TestEX8_MockGuardParity` compares mock and PG over actor × origin × status.
5. **No `completed()`, policy recorder, progress gate or result columns** for reinstall transitions. Do not route through `afterAgentTransition` or `afterSweep`.
6. **Existing tests stay unmodified:** `TestEX_*`, `TestEX2_*` (incl. `expect2_docs_test.go`, the EX2 safety scan and its `ex2Preexisting` table), CMD-LIFECYCLE, the web EXPECT-001/002 tests and every existing installer fixture test.
   - Existing installer fixtures keep the agent "inactive" (`is-active` exits 3 without `/fixture/agent-active`), so they never reach the notice. They do gain one `systemctl is-active` trace line when `CONFIG_EXISTED` is true and no bootstrap key is given.
   - If any existing test fails **only** because of that line, or because a re-registration now also closes a reinstall record, the implementation report names it and Ryan decides. Do not edit it silently.
7. **Installer style.** Match the file: `|| return 1` inside `if` conditions, `local` variables, no `echo` of config values, `printf` for all output. The block must source cleanly on its own (the tests source it with only `STAGE` and `RIOT_CONFIG_DIR` set). No new `trap`. Never touch `/etc/riot` contents. Keep every awk regex free of `{}` intervals and repeated groups.
8. **Fixture.** The curl stub must never write the raw stdin; record only its sha256. The listener binds `127.0.0.1` only, in a daemon thread, closed in `tearDown`. Real-curl tests set `/fixture/notice-real`. Remove every new flag file in `setUp`, as the existing list does.
9. **README (FR-030), Part D.** It must cover:
   - when a window opens (registered device, root re-run, active agent, notice accepted) and when it does not (fresh install, `--bootstrap-key`, missing ID or key, agent not running, device already offline, open record, cap, the skipped trust configurations of AD-015, `wget`-only hosts, old server);
   - the 10-minute bound from the notice; offline-only suppression; the close at the first registration;
   - the one escalation, and the not-confirmed warning with no push;
   - the cap of 6 per 24 h, separate from agent auto-update's, and the budget (< 60 min per day from reinstalls, < 120 min combined);
   - the "Reinstalling agent" / "by installer" label;
   - the 10-second best-effort notice and both installer output lines;
   - no prompt between stop and start (Q-3), and that a failed install after the stop escalates at 10 minutes;
   - the NFR-010 residuals.

   CHANGELOG: the Section 14 steps.
10. **Build environment:** `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`; `-race` in Linux CI. The installer fixture runs only in the disposable container (`RIOT_INSTALLER_FIXTURE=1`); never run it on a workstation or on Hawaii.
11. **Work packages:**
    - G1: models, migration, repo, mocks, R8;
    - G2: commands Service, sweeper, `classifyRejected`, S8;
    - G3: expect, H8;
    - G4: handlers, wiring, HT8;
    - G5: Z8;
    - I1: installer block and call site;
    - I2: fixture and CI;
    - W1: web;
    - D1: docs.

    G, I and W run in parallel; D runs last. Run `go test ./...`, `cd web && npm run test:run`, and the fixture container command from ci.yml after each.
12. **The implementation report must state:**
    - the 5.2 rollback note;
    - the AD-017 change to an EXPECT-002 query;
    - the CI apt and `update-alternatives` change;
    - any `ex8Preexisting` entries;
    - the X-4 real-time test.

---

## 13. Definition of Done

- [ ] Every Section 4 change is implemented. No agent, sudoers, router or dependency change.
- [ ] Every test in Section 8, 8.1 and 8.2 exists under its name and passes. Go tests use an injected clock and never sleep. Exactly one installer test (X-4) uses real time, ≤ 10.5 s.
- [ ] `go test ./...` and `cd web && npm run test:run` are green; `-race` in Linux CI. The installer fixtures are green on bookworm-slim and trixie-slim.
- [ ] Existing `TestEX_*`, `TestEX2_*`, CMD-LIFECYCLE, web EXPECT-001/002 and installer fixture tests pass **unmodified** (AC-025).
- [ ] Migration 000035 runs up, down and up on PostgreSQL 16; the CHECK and the unique index are PG-tested; the EXPECT-001/002 PG migration tests still pass.
- [ ] No new lint errors (`go vet`, `tsc`, frontend lint). `bash -n scripts/install.sh` passes.
- [ ] The AD-012 bounds appear as code comments at each bound (Go and installer).
- [ ] **QA gate** (EXPECT-002 C-6 / EXPECT-001 C-8, carried over). QA verifies:
  - reinstall transitions never touch the progress gate or the result columns;
  - outcome, cap and stuck events are never dispatched and never consume the ceiling;
  - the notice stores no event row;
  - agent reports on a reinstall row return `not_found` (HTTP and WebSocket);
  - the installer's key never appears on argv, output or disk (`test_EX8_AC006_*`);
  - the printed Z8 and WZ8 lists include every new or modified Go and web file of the diff.
- [ ] Section 14 is copied into the PR body by Clod.

---

## 14. Rollout (FR-029, Q-4): Ryan's steps after merge

**Ordered list for Ryan:**
1. **Review and merge the PR.** From that moment the new installer is served from `main` (raw.githubusercontent.com may serve the old file for up to about 5 minutes after the merge).
2. **Release and deploy the server.** Clod tags the next release (v2.84.0 or later, whatever follows the latest tag at merge time) and deploys `riot-server` on hawaii: `docker compose pull && docker compose up -d` in the compose directory listed in `D:\Repos\Hawaii\docs\laboverview.md`. Clod then verifies the running image digest and that the schema is at version 35. **Required:** without it, the notice is answered 400 and re-runs page as today.
3. **Devices:** nothing. No agent upgrade, no config change, and no extra installer re-run is needed for this feature. Every registered device with a running agent qualifies, whatever agent version it runs.
4. **Order:** server deploy (step 2) before relying on silence. The installer is live at step 1, which is harmless: a re-run between steps 1 and 2 prints "Server not notified (server does not support reinstall notices); a device-offline alert may follow", completes normally and pages exactly as today.
5. **First silent re-run:** the first `install.sh` re-run on any registered device that starts after step 2 is verified. Clod suggests that the next planned re-run (for example the pending PiVPN or PiCUPS root re-run) serves as the check: it should print "==> Server notified: agent reinstall (offline alerts paused up to 10m)" and produce no push.

**Mixed states:**

| Installer | Server | Result |
|---|---|---|
| old (before merge) | old | Pages as today |
| new | old | One "not notified (server does not support reinstall notices)" line; install unchanged; pages as today |
| old | new | No notice, no record; pages as today |
| new | new | Silent; "Reinstalling agent"; escalates once if not back in 10 min |

**Rollback:** roll the server back to the previous image only after migrating down to 34. Down deletes the reinstall history (R-5). The installer needs no rollback: against an old server it degrades to "not notified".

---

## Security addendum (inherited conditions)

| Condition | How this design meets it |
|---|---|
| EXPECT-001 C-1 (no stale suppression) | Reinstall windows are bounded by a CHECK-enforced `hard`; there is no powered-off state |
| EXPECT-001 C-2 (backstop on every window end) | `finishReinstall` step 1 runs before anything that can fail, for every reason, including `lapsed` |
| EXPECT-001 C-3 (bounded publish) | Existing publisher; outcome jobs are broadcast-only |
| EXPECT-001 C-4 (liveness predicate) | `DeviceIsLive` is used in the DEC-4 check |
| EXPECT-001 C-5 (ceiling) | The escalation goes through `t.escalate`, under the per-device ceiling |
| EXPECT-001 C-6 (SafeName) | Hostnames through `SafeName`; no other notice text is displayed |
| EXPECT-001 C-7 (restart grace) | Existing grace and detector gate |
| EXPECT-001 C-8 / EXPECT-002 C-6 (QA gate) | Section 13 |
| EXPECT-002 C-1 (scan coverage) | AD-018: named Go list, the three-part set, the preexisting table, self-tests, a separate web scan, plus installer static and dynamic checks |
| EXPECT-002 C-2 (restricted writer and SQL invariants) | AD-005 and Section 5.1: `ActorReinstall` only; guards on all other actors incl. the server; CHECK on bound, `exec_deadline_s`, keys and params |
| EXPECT-002 C-3 (truthful record view) | Existing endpoint; reinstall rows have only server-authored linked events |
| EXPECT-002 C-4 (stuck record) | AD-008 step 5 and AD-004 step 3 |
| EXPECT-002 C-5 (budget acceptance) | AD-012, Open points |
| EXPECT-002 INFO-5 (registration as a heartbeat) | Reused; no fast-return push |

**New surface for the security review:** the root-run notice (AD-014 to AD-016). The specific questions are the config-injection argument, the four-path restriction and R-1, the xtrace handling, and the reason set.

---

## FRD Deviations and Clarifications

| ID | FRD item | Resolution | Rationale |
|---|---|---|---|
| X-1 | FR-024 "Fleet Overview must show … actor phrase … start time" | The fleet row shows the compact one-line label "Reinstalling agent", with "by installer · started HH:MM" in its hover title. The device header shows all three as text | PR #44: Ryan asked for one-line fleet rows; every other activity already follows that rule. `isAgentUpdating` is false for reinstall, because there is no target version for the Version-cell spinner |
| X-2 | FR-003 / Q-2 | Stricter than the agent in five cases: `tls_verify: false` without a pinned certificate; a pin without a pinned certificate; trust files outside the four `/etc/riot` paths or not plain files; a non-literal `tls_verify`; no `curl` | BR-007, NFR-006. None is written by the stock installer or agent. Each fails toward alerting with a visible reason |
| X-3 | "Registered device" definition | Device ID from `agent.device_id` only, not `/etc/riot/id` | FRD definition. The agent saves the ID to `agent.yaml` at its first registration |
| X-4 | NFR-011 "without real elapsed time" | One installer test waits for a real hung connection, ≤ 10.5 s | The bound is an OS `timeout`; only a real wait proves curl is killed. Every other installer and Go test is time-free |
| X-5 | Notice "store" | The notice is a request type on the agent event route, never an `events` row | NFR-005 and AC-021 count no notice event |
| X-6 | FR-006 "when the server accepted it" | Accepted means the response says a record is open (`created` or `open_record`). A server refusal prints "not notified (<reason>)" from the fixed set | Keeps the success line truthful ("offline alerts paused") |
| X-7 | FR-027 | One predicate is added to EXPECT-002's attribution query (AD-017) | Without it, EXPECT-002 behaviour would change |
| X-8 | FR-009 | A notice during the few seconds between an unresolved bound and its resolution answers `open_record` | Resolution runs within 1 s of the bound (≤ 2 min after a restart). Toward alerting at worst |
| X-9 | AC-005 | Proven with real curl against a `127.0.0.1` TLS listener in the fixture container. CI installs `curl` and `gawk`, keeping mawk as the default awk | A stub cannot prove "no key sent to an untrusted server" |
| X-10 | FR-006 "exactly one line" vs AC-002 "exactly as today" | FR-002 skips print nothing; every attempted notice prints exactly one line | AC-002 |
| X-11 | FR-015 (a) | Any existing-device registration after S closes the window `succeeded`, including the agent's 24 h periodic re-registration if it falls inside a run that aborted before the stop | Toward alerting (the window ends early). A version check is excluded by A-003 |
| X-12 | DEC-3 | `params = '{}'` is enforced by the CHECK | No untrusted text in the record |

## Residuals (in addition to the FRD's NFR-010)

- **R-1.** A compromised `riot` user can swap an `/etc/riot` certificate file for a symlink between the installer's check and its read. At worst root then presents a root-readable certificate, or signs one TLS handshake with a root-readable key, to the server `riot` configured. No file content leaves the host. A compromised `riot` already holds the device key.
- **R-2.** On an `http://` URL the notice carries the key in clear, as every heartbeat already does.
- **R-3.** If the installer is SIGKILLed, `$STAGE` (0700, root, in `/tmp`) can remain with the config snapshot. This is pre-existing behaviour for `config_facts`.
- **R-4.** The raw.githubusercontent.com cache can serve the old installer for about 5 minutes after the merge.
- **R-5.** Rolling back 000035 deletes the reinstall history.
- **R-6.** If a self-update record is open when a reinstall installs a different version, EXPECT-002 ends that record `failed` / `wrong_version` with one warning and no push. This is unchanged EXPECT-002 behaviour.
- **R-7.** The abuse budget grows by < 60 min per device per day (AD-012).

## Open points for Ryan

Decisions Clod made on Ryan's behalf (Ryan reviews them in the PR):
- **DEC-1 to DEC-8 as recommended in the FRD.**
- **Budget accepted:** < 60 min per device per day from reinstalls, < 120 min combined with agent auto-update, as at most 6 short outages each. Every one is logged as `info`, and every outage past 10 minutes pages.
- **X-1:** fleet rows stay one line, with the actor and start time in the hover title.
- **X-2:** the five configurations that skip the notice. None exists on a stock install.
- **X-4:** one 10-second real-time installer test.
- **CI change:** the installer fixture job installs `curl` and `gawk`, and pins awk to mawk.

**Blockers:** none.
