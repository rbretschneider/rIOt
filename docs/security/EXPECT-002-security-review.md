# Security Review Report
**Story ID:** EXPECT-002
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-26
**Verdict:** APPROVED WITH CONDITIONS

Inputs:
- `docs/requirements/EXPECT-002-frd.md` (revision 1, commit 4730048)
- `docs/architecture/EXPECT-002-add.md` (revision 1, commit 4790abc)
- Parent design and review: `docs/architecture/EXPECT-001-add.md`, `docs/security/EXPECT-001-security-review.md`
- Code at 4790abc:
  - `internal/server/handlers/handlers.go` (`ReceiveAgentEvent`, `RegisterDevice`, `Heartbeat`),
    `register_auth.go` (`decideReRegistration`), `commands.go` (`authenticatedDeviceID`, `GetCommandOutput`, the agent
    report endpoint), `agent_event_extra.go`, `docker_attribution.go`
  - `internal/server/router.go`, `internal/server/middleware/auth.go`, `internal/server/ingest/limits.go`
  - `internal/server/commands/service.go` (`Dispatch`, `deliver`, `revertDispatch`, `ClaimForHeartbeat`, `Cancel`,
    `HandleAgentReport`, `classifyRejected`, the progress gate), `sweeper.go`, `targets.go`
  - `internal/server/db/command_repo.go` (`CreateIfNoConflict`, the claims, `buildTransitionSQL`, `ListOverdue`),
    `expect_repo.go`, `event_repo.go`, `device_repo.go`, `docker_retention.go`
  - `internal/server/storage/command_budget.go`, `headroom.go`
  - `internal/server/expect/tracker.go`, `window.go`, `finish.go`, `tick.go`, `safety_test.go`
  - `internal/server/events/generator.go` (`DeviceOfflineAt`), `internal/server/offline.go`
  - `internal/models/command_state.go`, `command_policy.go`, `expect.go`
  - `internal/agent/agent.go`, `register.go`, `updater.go` (Q-1 check only; no agent code changes in this story)
  - `cmd/riot-server/migrations/000023`, `000033`
  - `web/src/utils/activity.safety.test.ts`, `web/src/test/static-guards.test.ts`, `web/src/hooks/useCommandTracker.ts`

No production system was queried for this review. The migration-size facts are those of the EXPECT-001 review (same
day).

Pre-existing weaknesses found during this review are referenced here only generically. Their details are in Ryan's
private review notes, not in this public repository.

---

## Threat Model Summary

### Assets
- **Alerting integrity.** This story adds a second way to **turn `device_offline` alerts off**, and this time the
  trigger is text the device's own agent sends, not a command Ryan or a schedule created. Any path that opens,
  extends or chains a self-update window, or that ends one without its backstop, hides an outage.
- **The escalation backstop.** Suppression is acceptable only because the FR-020 escalation fires at the bound, and
  because X-7 escalates any window that ends while its suppressed offline is still ongoing.
- **Operator command integrity.** Self-update rows live in the `commands` table next to operator, auto-update and
  auto-patch commands. They must never be claimed, delivered, cancelled, reported on, or able to block, capture or
  confuse an operator command.
- **Dashboard truth.** "Updating agent 2.78.0 → 2.80.0" and the "Expected offline" count replace "offline" for up to
  10 minutes.
- **Notification channels and Ryan's attention.** The new path adds at most one escalation push per record.
- **Database, memory and logs (bounded growth).** New rows, a CHECK, two partial indexes, three event types, one
  Tracker window per device and a new read endpoint.

### Threat Actors
- **Compromised or buggy agent (holds its own device key, optionally its client certificate).** It controls every
  `agent_update_*` event and every registration of its own device, including message text, the reported versions,
  the hostname and the timing. It can also stop heartbeating while still sending events. The questions that matter:
  - can it reach **another** device's alerts, labels or records?
  - can it buy more silence than the FRD's budget (10 min per record, 60 min per day)?
  - can it flood rows, events or pushes?
  - can it tamper with operator commands?
- **Honest agent with an unlucky outage.** A real power or network loss that coincides with a legitimate self-update.
  This is the accepted NFR-011 residual.
- **Authenticated dashboard user.** Single admin role. The new endpoint and the self-update rows are read-only for it.
- **Unauthenticated network attacker.** Reaches `/api/v1/devices/register` (device proof required for an existing
  device, rate limited) and nothing else new.
- **Environment.** Server restarts inside a window, DB errors, a missing resolver wiring, clock steps.
- **Future developer (regression).** A test or helper that runs a real power action, a package manager, the agent
  updater or an installed `riot-agent` (NFR-001); a refactor that lets a generic command path touch a self-update row.

### Attack Surface Introduced
- Recognition of agent free text (`agent_update_started` message) as a trigger for suppression.
- Agent-driven state changes on server-owned rows: `agent_update_completed`/`failed` and the registration's reported
  version move a self-update row through the new restricted actor `ActorSelfUpdate`.
- A new `commands` origin (`agent_self_update`), a new actor, an origin guard in the transition SQL, a widened origin
  CHECK, a shape CHECK and two partial indexes (migration 000034).
- One per-device advisory-lock transaction per `agent_update_*` event.
- `GET /api/v1/devices/{id}/commands/{commandId}/events` (admin session).
- `DeviceActivity.agent_to_version` and three event types.
- No new agent endpoint, agent privilege, dependency, `install.sh` or sudoers change.

---

## Findings

There are no CRITICAL and no HIGH findings.

**Cross-device isolation holds** (INFO-1):
- the event route binds the key to `{id}`, and the ADD adds a second check that the authenticated ID equals `{id}`;
- re-registration requires proof of the named device, and the registration-key path applies only to keyless devices;
- every new query is scoped by `device_id`;
- coverage only searches the path device's windows;
- the agent-supplied `command_id` is ignored for attribution.

**The silence budget is as the FRD states and cannot be chained** (SEC-005):
- `hard` is server time (S + 10m), and nothing can move `deadline_at` on these rows;
- X-7 escalates any record that ends with its suppressed offline still ongoing, so a second record cannot extend a
  first one's outage.

**AD-007 is correct against the code** for all 24 paths (INFO-2). The findings below harden it and close gaps in its
verification.

### MEDIUM

#### SEC-001: The AC-029 safety scan does not cover everything the ADD says it covers
**Severity:** MEDIUM
**Domain:** Safety (NFR-001, Ryan's hard rule) / verification integrity
**Location:** ADD AD-014, Section 12 note 7, Section 8 row AC-029; `web/src/utils/activity.safety.test.ts`;
`internal/server/expect/safety_test.go`

**Description:**
The scan is the only automated control for Ryan's top safety rule. It does not cover the story as the ADD claims:
- **Web.** The ADD says "the existing web `activity.safety.test.ts` scans the new web tests (they contain `EXPECT`)".
  It does not:
  - its test filter matches the path `activity[./]` or the text `EXPECT-001|activity_update|getActivity|useActivity|utils/activity`;
  - `EXPECT-002 [AC-0xx]` does not match `EXPECT-001`;
  - `SelfUpdateDetails.test.tsx` matches none of these unless it happens to mention one of the tokens;
  - the spawn scan's `NEW_SOURCES` list does not include `utils/selfUpdate.ts` or `SelfUpdateDetails.tsx`.

  (The global static guard does cover raw HTML sinks in all non-test sources, so this gap is about process spawning
  and power commands, not XSS.)
- **Go file set.** The EX2 set is "every `*_test.go` containing `TestEX2_`" plus four named sources. It misses:
  - `internal/testutil/mocks.go`, a **test helper** this story modifies (NFR-001 names helpers explicitly);
  - `internal/server/db/self_update_repo.go`;
  - `internal/server/handlers/command_events.go`;
  - any new helper `_test.go` outside `internal/server/expect/` that carries no test function with the marker.
- **Go patterns.** The inherited forbidden list blocks `os/exec` and `syscall` imports and `exec.Command*` calls.
  It does not block `os.StartProcess` or an import of `golang.org/x/sys/unix` (which exposes `Reboot` and `Exec`).

**Attack Scenario (regression):**
1. During WP-1 a developer adds a PG-test helper to `internal/testutil/mocks.go` or to a new `selfupdate_helpers_test.go`
   that shells out to a local binary (for example to "simulate the agent" by starting a real `riot-agent` build with
   `os.StartProcess`).
2. The helper is not in either scan set and the call shape is not in the pattern list. `TestEX2_AC029_*` passes and
   prints a list without the file.
3. QA's DoD check ("the printed list includes every new Go test file in the diff") does not catch a modified non-test
   helper or a helper file without the marker.
4. On a developer machine or a self-hosted runner, the helper executes an agent binary. That is exactly what Ryan's
   rule forbids.

**Required Resolution (properties):**
- (a) The EX2 Go scan set includes every Go file this story adds or modifies: the Section 4 list, asserted by name like
  the EXPECT-001 `must` list (including `internal/testutil/mocks.go` and every new non-test source), plus every
  `_test.go` file that contains `TestEX2_` **or** lives in a package directory this story adds files to.
- (b) The forbidden patterns also cover `os.StartProcess`, an import of `golang.org/x/sys/unix` or
  `golang.org/x/sys/windows`. The existing self-test style ("the pattern really catches the call shapes") is extended
  to the new patterns.
- (c) The web half has an EXPECT-002 scan whose computed set includes every test file that mentions `EXPECT-002`, and
  whose source list includes `utils/selfUpdate.ts` and `components/SelfUpdateDetails.tsx`. It asserts the presence of
  `SelfUpdateDetails.test.tsx`, `FleetOverview.selfupdate.test.tsx` and `DeviceDetail.selfupdate.test.tsx` in the set.
- (d) The ADD's Section 8 AC-029 row and Section 12 note 7 state (a) to (c) and drop the "they contain EXPECT" claim.

**Blocks:** No. Condition C-1.

---

### LOW

#### SEC-002: Only the agent and operator edges are origin-guarded; a server edge can still end a self-update row, and the 10-minute bound is a code invariant only
**Severity:** LOW
**Domain:** Authorization (state machine) / defense in depth
**Location:** ADD AD-003 ("`ActorServer` gets no guard"), AD-006 (the bound's `running → timed_out` "uses the existing
`ActorServer` edge"), AD-007 row 7, Section 5.1

**Description:**
- `ActorServer` may move `dispatched`, `acknowledged`, `running` and `verifying` to `timed_out`. The ADD keeps the
  sweeper away from self-update rows with a predicate in `ListOverdue` plus a Go-level skip. The SQL of the transition
  itself would still accept a self-update row from any server caller.
- If a self-update row is timed out by the sweeper, `afterSweep` runs:
  - it writes a `command_completed` event, which FR-022 forbids;
  - it writes an Info log line, against NFR-005;
  - it turns a `verifying` row (returned, confirming) into `timed_out` instead of `succeeded`;
  - it races the Tracker's own resolution.
- The bound "never later than D + 10m" holds only through `evaluateSelf`'s `min()`. Nothing in the schema stops a
  self-update row from carrying a longer `deadline_at` or `exec_deadline_s`. A later code path that copies
  `deadline_at` into the window's `hard`, without the `min()`, would extend suppression.

**Attack Scenario (regression, not reachable as designed):**
1. A later refactor merges `listDue` predicates, or adds a "time out stuck commands" admin action, using `ActorServer`.
2. At S + 10m the sweeper times out hawaii's `verifying` self-update row first. It writes "Agent update timed_out",
   and the Tracker's resolution finds the row terminal with no `failure_step`. hawaii's history says "Not confirmed"
   for an update that worked, and the Events page shows a server event the FRD says must not exist.
3. In the bound variant, a future code path seeds a self-update row with the generic `agent_update` deadline
   (15 minutes). Any code that trusts `deadline_at` then covers offline for 15 minutes, not 10.

**Required Resolution (properties):**
- (a) Every write to a self-update row is origin-guarded in SQL. Either:
  - the bound's `running → timed_out` moves to `ActorSelfUpdate`, and every `ActorServer` edge gets
    `AND origin <> 'agent_self_update'`; or
  - an equivalent rule that makes `ActorSelfUpdate` the only actor whose UPDATE can match a self-update row.
- (b) Migration 000034's shape CHECK also requires, for self-update rows:
  - `deadline_at <= dispatched_at + interval '10 minutes'`;
  - `exec_deadline_s <= 600`;
  - `cardinality(target_keys) = 0`.

  The 10-minute bound and "never a conflict holder" then become DB invariants.
- (c) `TestEX2_AC024_PG_AgentAndOperatorGuards` also proves that a server-actor `→ timed_out` on a self-update row
  does not apply. The mock emulates the same guard (ADD Section 12 note 4 parity).

**Blocks:** No. Condition C-2.

---

#### SEC-003: The linked-events view can be flooded by the device's own agent, hiding the server's outcome and escalation events
**Severity:** LOW
**Domain:** Data integrity of a security-relevant view / abuse
**Location:** ADD Section 6.3 (oldest first, at most 50), AD-002 step 3 (every valid start during an open record is
linked, FR-006), AD-004 (every completed/failed within 10 min is linked, FR-022), AD-013

**Description:**
- The FRD requires each repeated start, completed and failed event of the device to be linked to the record.
- The event ingest bucket allows a burst of 120 and then 2 per second per device. Over a record's 10 minutes that is
  roughly 1,300 linked agent events.
- The endpoint returns the **oldest** 50. The server-authored events come later:
  - the suppressed `device_offline` (info);
  - the online note;
  - the FR-018 to FR-021 outcome event;
  - the FR-020 escalation.

  So they fall outside the page.
- No growth is added (the events would be stored anyway). The problem is that FR-027's read-only record view, which
  the device label links to, shows only agent-controlled rows.

**Attack Scenario:**
1. A compromised vermont sends one valid start, then 200 more starts and completions over the next minute (within
   the ingest limit), then stops heartbeating for 8 minutes and comes back.
2. Ryan sees "Updating agent ..." on the fleet page. Later he opens the record from the device page to see what
   happened.
3. The record view lists the start and 49 agent copies. The server's info event "Device vermont went offline during
   ..." and the "came online after ... (offline 8m)" note are not shown. The record reads like a noisy but harmless
   update, and the 8-minute outage is visible only in the global Events page.

**Required Resolution (properties):**
- (a) The record view always contains every **server-authored** event linked to the record (offline, online, outcome,
  escalation, at most a small fixed number per record by AD-013), whatever the agent volume. Either:
  - the endpoint returns them first or separately; or
  - agent-sourced links per record are capped at a stated N, and further repeats are stored unlinked (a stated, minimal
    deviation from FR-006 and FR-022).
- (b) The response says when agent-sourced rows were truncated (for example a count of omitted rows).
- (c) AD-013 states the per-record bound on linked rows, and `TestEX2_AC023_CommandEventsEndpoint` covers a flood of
  more than 50 agent events with the outcome event still returned.

**Blocks:** No. Condition C-3.

---

#### SEC-004: A self-update row that is never resolved silently turns tracking off for its device
**Severity:** LOW
**Domain:** Fail-safe visibility / self-healing (Ryan's bounded-growth and self-heal rule)
**Location:** ADD AD-006 ("a failing step leaves the entry for the next tick"), Section 9 ("Resolver not wired"),
AD-007 row 7 (the sweeper never touches these rows), Section 5.1 (`idx_commands_self_update_open`)

**Description:**
- The Tracker is the only component that can end a self-update row. If resolution never succeeds, the row stays
  `running` or `verifying` with no time bound. Causes include:
  - a typed-nil or missing resolver wiring (`ErrNoResolver`);
  - a persistent DB error on that row;
  - the row never being loaded.
- Suppression is not affected: coverage ends at `hard` by time, and the backstop runs before resolution. That part is
  well designed.
- The unique open-record index, however, then refuses every later record on that device, **forever**. It does so
  without the `agent_self_update_paused` warning, because the refusal comes from `open_record`, not the cap.
- The history shows "In progress" indefinitely. The only signal is a throttled Warn in the server log.
- Ryan starts getting "device offline" pushes on every release again and has no way to tell why.

**Attack Scenario (failure, not an attacker):**
1. A server.go refactor passes a nil `*commands.Service` into `SetSelfUpdateResolver` (the typed-nil trap the ADD
   warns about), or the resolver call fails on a schema drift after a rollback and re-upgrade.
2. hawaii's first self-update after the deploy creates a row that is never resolved.
3. Every later start on hawaii links to that row (`open_record`) and creates nothing. Releases page Ryan again, and the
   dashboard says hawaii's update from weeks ago is still "In progress".

**Required Resolution (properties):**
- (a) A self-update row that is still non-terminal at a stated limit after its bound (for example bound + 1 hour,
  stated in AD-013) produces exactly one durable `warning` event per row (marker-guarded, no push), with a message
  that names the device and says tracking is paused for it until resolved. It also produces one throttled Warn.
- (b) Optionally, the open-record check ignores a row whose bound is more than that limit in the past, so a stuck row
  never blocks new records. Only if (a) is also in place.
- (c) `TestEX2_ResolverUnsetFailsSafe` also asserts (a).

**Blocks:** No. Condition C-4.

---

#### SEC-005: A device's own agent can make up to six short outages a day silent (the accepted abuse budget, quantified)
**Severity:** LOW (a designed, bounded residual; needs Ryan's recorded acceptance)
**Domain:** Business logic / alerting integrity (NFR-010, NFR-011)
**Location:** FRD NFR-010, AD-010, AD-005, X-7

**Description, quantified against the code:**
- **Per record:** coverage is `[S, S + 10m)` in server time. The offline time is backdated to the last heartbeat plus
  45 s.
  - An outage that starts right after S and ends before S + 10m produces only an `info` event and the "Updating
    agent" label: about 9.5 minutes of offline time with no push.
  - An outage still ongoing at S + 10m escalates once within 60 s: a delay of at most about 10.5 minutes.
- **Per day:** at most 6 records per rolling 24 h, counted from durable rows. Restarts do not reset the cap. The
  emergency purge keeps a 7-day floor, and the budget purge only selects rows with result bytes, so the X-13
  undercount cannot happen if C-6 holds. That gives **at most about 57 minutes of unalerted offline time per day,
  split into at most 6 outages of under about 10 minutes each.** Every one is stored as an `info` event that names
  the update. After the cap, one `paused` warning is stored per 24 h (no push).
- **No chaining:** a second record cannot extend the first. X-7 escalates the first record at its end when the
  suppressed offline is still ongoing, and coverage is evaluated per window.
- **Context:** against a compromised agent, offline alerting was never a strong control, because such an agent can
  simply keep heartbeating. The new capability is narrow: it can make a **real** reboot or power-cycle of its own
  device (for example into a modified system) silent, up to 6 times a day, if each takes under about 10 minutes.
- **Honest agent:** a real outage that starts during a legitimate self-update is hidden for up to 10 minutes and then
  escalates once (NFR-011, first bullet).

**Attack Scenario:**
1. An attacker controls PiVPN's agent and wants to reboot it into a modified image without Ryan noticing.
2. The agent sends "Agent update started: 2.80.0 → 2.80.1" and reboots. The device is back in 3 minutes and registers
   on 2.80.0.
3. The offline is stored as `info` ("went offline during Agent update 2.80.0 → 2.80.1 by agent auto-update"). The
   registration on another version closes the record as `failed` with one `warning` event ("came back on agent
   2.80.0 ..."). No push is sent.

**Required Resolution (properties):**
- (a) Ryan accepts the budget as stated above (about 57 minutes a day, at most 6 outages of under about 10 minutes,
  same device only, each leaving an `info` event, and the wrong-version or not-confirmed `warning` where applicable).
- (b) The README residuals (FR-032) state the budget in those terms, including that a self-update which comes back on
  another version leaves a `warning` event but no push.
- (c) The cap stays at 6. The legitimate rate can approach 5 per day (an update check every 6 h, plus one 30 s after
  every agent start). A lower cap would pause tracking on busy release days, which is the noise this story removes.

**Blocks:** No. Condition C-5 (Ryan's decision, below).

---

### INFORMATIONAL

#### INFO-1: Cross-device isolation verified
- `DeviceAuth` rejects a key whose device differs from `{id}`. With mTLS on, the certificate check runs first. The ADD
  adds `authenticatedDeviceID(r) == {id}` before any self-update work.
- `decideReRegistration` rejects a key or certificate of another device (403). The registration-key path only applies
  to a device with no key. The hook runs only after the proof, on the existing-device path.
- The agent WebSocket has no agent-event message type. `command_ack`, `command_progress` and `command_result` go
  through `HandleAgentReport`, which is device-scoped in SQL and, after this story, origin-guarded.
- `CreateSelfUpdate`, `OpenSelfUpdate` and `LatestSelfUpdateSince` take `device_id`. The advisory lock key is
  per device.
- Coverage, `NoteOnline` and the activity label only read the path device's windows.
- Shared resources are bounded per device: the escalation push ceiling, the ingest bucket and the service log
  throttle. A device cannot fill the publisher queue (at most 2 jobs per record).
- AC-018 must also cover vermont registering, and sending completed and failed, while hawaii has an open record:
  hawaii's record is unchanged.

#### INFO-2: AD-007 verified against the code
- **Claims.** `ClaimByID` and `ClaimQueued` only match `queued`/`pending` (with `expires_at > now`). The CHECK forbids
  those statuses on self-update rows, so they are never claimed, returned in `pending_commands` or sent.
  `revertDispatch` matches only `dispatched`. Expiry matches only `queued`.
- **Agent reports.** The ADD's `ActorAgent` guard covers the HTTP and WebSocket reports and the late-result
  `timed_out → succeeded/failed` edge. The progress gate answers before the transition but only for IDs it has seen
  accepted. Self-update writes must never feed it (C-6).
- **`Dispatch`.** `IsValidOrigin` is not widened, so no API, fleet, auto-update or auto-patch path can create the
  origin. Self-update rows hold no target keys, so they never conflict with or block any command (`CreateIfNoConflict`,
  `DispatchBulkDocker`, the web's target-key tracker).
- **Docker attribution.** It requires a target-key match, so it never selects a self-update row.
- **Retention and budget.** The pending-window exemption applies through `action = 'agent_update'`.
  - Retention deletes processed rows at 90 days.
  - The budget purge without a date only selects rows with `result_bytes > 0`.
  - The emergency purge keeps a 7-day floor.

  So the FR-007 cap cannot be undercounted by a purge, as long as self-update rows never carry result bytes (C-6).
- **FR-004 attribution.** The race R-4 is benign. An agent can only send a command-path start after it received the
  command, which happens after the claim committed. So a real command-path start always sees `dispatched_at`. Only a
  coincidental self-update at the same instant can race, and both windows are then evaluated separately.
- **Down migration.** Deleting self-update rows cascades their markers. Events keep a dangling `command_id`.
  Acceptable (R-5).

#### INFO-3: Untrusted text verified; one pre-existing rider
- `ParseSelfUpdateStart` is sound: a 4 KB pre-check, trim, a 256-byte cap, valid UTF-8, no Cc/Cf/Zl/Zp, an exact prefix
  and arrow, and an anchored ASCII grammar (RE2, linear time).
- Look-alike arrows, full-width digits and bidi or zero-width characters all fall back to today's behaviour.
- `SafeName` already strips Cc and Cf (EXPECT-001 C-6). `SafeText(…, 200)` reuses it. `EventRepo.Create` and
  `RecordEscalation` sanitize, cut and redact again.
- Escalation pushes carry only the hostname, validated versions and the record short ID, never agent free text.
- The failure reason appears only in a non-notified `warning` event and in `result_msg`.
- The FR-019 and FR-021 versions are shown only if they pass Section 7.2.
- **Rider.** The agent-supplied severity of agent self-events is still stored as received (pre-existing; details in
  Ryan's private notes). These events now appear in the record's history view, so validating the severity of the
  three `agent_update_*` types in the handler this story already changes is a cheap improvement. It does not change
  FR-005 for the values real agents send.
- Device names in all messages and pushes are agent-reported hostnames. That is pre-existing and also covered in the
  private notes.

#### INFO-4: Fail-open review: no path ends in silence beyond the designed budget
Each failure fails toward alerting or keeps the bound:
- **Parse failure, cap, `CreateSelfUpdate` error, attribution error.** No record; today's behaviour.
- **Commit succeeds but returns an error.** The Tracker learns the row at the next reconcile (≤ 30 s). An offline
  before then alerts.
- **Registration hook failure.** Coverage stays until `hard`, and the backstop escalates an offline begun inside it.
- **Tracker not ready.** `CoverOffline` errors, and the generator alerts.
- **Resolution failure.** The backstop runs first. See SEC-004 for the visibility gap.
- **Bound passed during downtime.** `Load` reconstructs `offlineSince` from the last heartbeat. The detector's
  backdated `offlineAt` is still covered and then escalated.

  One wrinkle: when the device died while the server was down, its DB status is still `online` until the first
  detector pass. If `finishSelfUpdate` runs first, it records "not confirmed while online" (FR-021) for a dead device.
  The detector then alerts normally, because the window is gone. This is a wrong label, not a lost alert. QA should
  include it in `TestEX2_AC015_*`. The architect may let the FR-021 resolution wait one detector interval after
  `graceEnd`.

#### INFO-5: Coverage ending exactly at the registration can produce a spurious push after a fast return
- `RegisterDevice` writes the stored `last_heartbeat` back unchanged. The detector judges offline at
  `lastHeartbeat + 45s`.
- Suppose a restart registers before that instant, and the detector passes before the new process's first heartbeat.
  The transition is then dated after V, is not covered, and pushes "went offline" right after the device came back.
- This fails toward alerting and is pre-existing in kind. But it is the AC-005 / BR-001 case this story exists to
  quiet.
- QA should test it: registration at last heartbeat + 40 s, detector pass at + 46 s, first heartbeat at + 50 s. The
  architect may end coverage at the first heartbeat after V, capped at `hard`. That keeps the 10-minute bound
  unchanged.

#### INFO-6: A self-update started after an operator command takes over that command's offline attribution
- Coverage and labels pick the latest D (FR-028, X-12).
- Suppose a self-update starts after Ryan dispatched a shutdown, and before the shutdown's offline. Then:
  - the offline is attributed to the self-update;
  - its FR-020 escalation pushes "still offline 10m after Agent update ..." for a host Ryan deliberately powered off;
  - the shutdown window still enters the powered-off state through the loop.
- This is one extra push, toward alerting. An honest agent rarely does this; a compromised agent can raise false
  pushes in simpler ways.
- Acceptable. A follow-on could let a self-update window yield to an in-flight host or shutdown window of the same
  device.

#### INFO-7: Migration 000034 is safe at hawaii's size
`commands` had about 1.8k rows on 2026-10-26.
- Dropping and re-adding the origin CHECK and adding the shape CHECK each scan the table once under ACCESS EXCLUSIVE.
- The two partial indexes are built non-concurrently.

All of this takes well under a second. The C-2 (b) additions do not change that.

#### INFO-8: Bounded growth holds, subject to SEC-003 and SEC-004
| Item | Bound |
|---|---|
| Rows | ≤ 1 open per device (unique index); ≤ 6 per device per 24 h; ≤ 540 per device over 90 days |
| Markers | ≤ 2 per record |
| Server events per record | ≤ 1 outcome event, ≤ 1 escalation, and the 1:1 replacement of today's offline warning |
| Rate-cap events | ≤ 1 per device per 24 h |
| Tracker memory | ≤ 1 window per device, no per-event state |
| Logs | Debug on the hot path |
| Request work | Bounded by the ingest bucket (≤ 2 transactions per second per device) |

The one unstated bound is linked rows per record (SEC-003). The one missing self-heal is a stuck open row (SEC-004).

#### INFO-9: Q-1, Q-2 and the security view of the ADD's open points
- **Q-1.** Verified: the agent calls `register()` only at startup (`agent.go:249`) and from the 24-hour loop
  (`agent.go:459`). The update check runs at start + 30 s and then every 6 h.
  - A periodic re-registration that is delayed more than about 30 s can land after a start. FR-019 then closes the
    record as `failed`, and the restart alerts. Toward alerting; accepted.
- **Q-2.** Agreed with AD-004.
- **X-1** (a completion before any return keeps coverage until the return or `hard`). Safe. It never extends past
  `hard`, and X-7 escalates if no return comes. **Keep.**
- **X-3** (the heartbeat version confirms at the bound). Affects the outcome only, never coverage. **Keep.**
- **X-7** (backstop on any end). This is the property that prevents chaining (SEC-005). **Keep. It is required.**

#### INFO-10: NFR-001 safety in the design
- The design adds no execution: handlers, the commands service, the Tracker and the repository only read and write the
  database.
- The EXPECT-001 scan already covers every file under `internal/server/expect/`, including `harness_test.go` and the
  new `selfupdate.go`.
- The gaps are in what the EX2 scan adds (SEC-001).

---

## Positive Observations
- **The record is a `commands` row, not a parallel system.** Windows, markers, the retention exemption, durability and
  the history view come from EXPECT-001 unchanged. The new origin is a single SQL discriminator.
- **Defense in depth on "never sent to an agent".**
  - The DB CHECK forbids `queued` and `dispatched`.
  - `IsValidOrigin` stays narrow.
  - The transition SQL has an origin guard.
  - `classifyRejected` returns `not_found` for these rows.
  - There are no target keys.

  The CHECK alone already stops every claim and delivery path; the others guard reports, cancels and conflicts.
- **Atomic recognition.** Attribution, the open-record check, the cap and the insert run in one transaction under the
  same per-device lock as command creation, backed by a unique index.
- **Server time only.** S, the bound and the 24-hour window never read agent timestamps.
- **The agent can only shorten or fail a record.** Completed and failed never create one, and a forged completion
  before a return does not extend coverage beyond `hard`.
- **The backstop runs before resolution**, so a resolution failure never delays the page (EXPECT-001 C-2 kept).
- **Strict parsing with fallback.** Anything unusual gets today's behaviour, and the parser rejects Cf and bidi
  characters, which is stricter than the FRD.
- **No agent free text reaches a notification.**
- **The sweeper is kept away from self-update rows, and the Tracker owns the bound.** This avoids a
  `command_completed` event and a race.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.** There are no CRITICAL and no HIGH findings.

- Cross-device isolation holds by construction.
- Every AD-007 path is correct against the code.
- The silence budget matches the FRD (60 minutes a day; about 57 minutes of real offline time) and cannot be chained.
- Every failure path found fails toward alerting.

The conditions:
- strengthen the safety scan that enforces Ryan's hard rule (SEC-001);
- turn two code-level invariants into SQL invariants (SEC-002);
- keep the record view truthful under agent flooding (SEC-003);
- make a stuck record visible (SEC-004);
- record Ryan's acceptance of the budget (SEC-005).

None needs a new component or an FRD change, except the minimal, optional FR-006 deviation in SEC-003 (a).

### Conditions (architect amends the ADD where stated; senior_dev implements; QA verifies)
1. **C-1 (SEC-001). ADD changes in AD-014, Section 8 row AC-029 and Section 12 note 7:**
   - The EX2 Go scan set is every Go file in the Section 4 list, asserted by name, including
     `internal/testutil/mocks.go`, `internal/server/db/self_update_repo.go` and
     `internal/server/handlers/command_events.go`. It adds every `_test.go` containing `TestEX2_` or in a package
     directory the story adds files to.
   - The forbidden patterns add `os.StartProcess` and imports of `golang.org/x/sys/unix` and `golang.org/x/sys/windows`,
     with self-tests.
   - A web EX2 scan: its computed set includes every test mentioning `EXPECT-002`, plus `utils/selfUpdate.ts` and
     `components/SelfUpdateDetails.tsx` as sources. It asserts the three new web test files are present.
   - Remove the claim that the EXPECT-001 web scan covers EX2 tests.
2. **C-2 (SEC-002). ADD changes in AD-003, AD-006 step 2, AD-007 row 7 and Section 5.1:**
   - The bound's `running → timed_out` uses `ActorSelfUpdate` (add that edge to its list).
   - Every `ActorServer` edge renders `AND origin <> 'agent_self_update'`.
   - `commands_self_update_shape` adds `deadline_at <= dispatched_at + interval '10 minutes'`,
     `exec_deadline_s <= 600` and `cardinality(target_keys) = 0`.
   - The PG guard test and mock parity cover the server edge.
3. **C-3 (SEC-003). ADD changes in Section 6.3 and AD-013:**
   - The events endpoint always returns every server-authored event linked to the command, and reports how many
     agent-sourced rows it omitted. Alternatively, agent-sourced links per record are capped at a stated N; record the
     FR-006/FR-022 deviation in the deviations table.
   - AD-013 states the per-record bound.
   - `TestEX2_AC023_CommandEventsEndpoint` covers more than 50 agent events.
4. **C-4 (SEC-004). ADD changes in AD-006, Section 9 and AD-013:**
   - A self-update row still non-terminal at bound + a stated limit writes one marker-guarded `warning` event (no push)
     and one throttled Warn.
   - Optionally, the open-record check ignores such a row.
   - `TestEX2_ResolverUnsetFailsSafe` asserts the event.
5. **C-5 (SEC-005).**
   - Ryan's acceptance of the budget is recorded in the ADD's "Open points for Ryan".
   - The README residuals (FR-032) state the budget as quantified in SEC-005.
   - The cap stays at 6.
6. **C-6 (QA gate, verification of properties the ADD states but the review cannot see in code yet):**
   - Self-update transitions never touch the progress gate, and never set `result_bytes`, `steps`, `target_results`,
     `stderr_tail`, `exit_code` or command output.
   - `agent_self_update_*` events and outcome jobs are never dispatched to a channel (nil rule) and never consume the
     per-device push ceiling.
   - `ReceiveAgentEvent` ignores any agent `command_id`, and runs self-update work only when the authenticated ID
     equals `{id}`.
   - The events endpoint returns one identical 404 body for malformed, missing and foreign IDs.
   - Agent reports on a self-update row return `not_found` for ack, progress and result, over HTTP and WebSocket.
   - These pass:
     - AC-016, AC-018 (with the INFO-1 extension), AC-019, AC-020, AC-024;
     - `TestEX2_Q2_OneLinkPerEvent`;
     - `TestEX2_AC015_*` (with the INFO-4 case);
     - `TestEX2_AC005_*` (with the INFO-5 timing).
   - QA confirms the AC-029 printed lists (Go and web) include every new or modified file of the diff.

### For Ryan's decision
- **SEC-005, the abuse budget.** A device's own agent can make up to 6 outages a day of under about 10 minutes each
  silent (about 57 minutes in total). Each one is stored as an `info` event and escalates if it lasts past 10 minutes.
  **Recommendation: accept, with the cap kept at 6.** A compromised agent can already fake liveness. The legitimate
  self-update rate can reach about 5 a day, so a lower cap would bring back the release-day noise.
- **ADD open points X-1, X-3, X-7.** **Recommendation: keep all three.** X-7 is what makes the budget hold.
- **INFO-6, a self-update after an operator shutdown.** It causes one extra push. **Recommendation: accept now**, and
  add "self-update yields to an in-flight host or shutdown window" to the EXPECT follow-on list.
- **Pre-existing items** referenced generically above (agent-event severity, hostname-based device identification,
  and two minor oracles or throttles in existing code) are described in Ryan's private review notes. They should be
  filed as separate fixes. The severity validation (INFO-3) can ride along with this story at no design cost.
