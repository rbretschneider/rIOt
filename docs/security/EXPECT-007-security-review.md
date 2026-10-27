# Security Review Report
**Story ID:** EXPECT-007
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-27
**Verdict:** APPROVED WITH CONDITIONS (C-1 to C-5)

Inputs:
- `docs/requirements/EXPECT-007-frd.md` (revision 1, commit d1f5a14)
- `docs/architecture/EXPECT-007-add.md` (revision 1, commit daa4115, AD-001 to AD-015)
- Precedent: `docs/architecture/EXPECT-002-add.md`, `docs/security/EXPECT-002-security-review.md` (X-2, C-1 to C-6)
- Coexistence: the EXPECT-008 ADD on branch `feat/expect-008` (origin `installer`), read only for the AD-011 predicate
- Code at daa4115:
  - server: `internal/server/handlers/handlers.go` (`ReceiveAgentEvent`), `agent_event_self_update.go`,
    `agent_event_extra.go`; `internal/server/commands/self_update.go`, `service.go` (`broadcast`, `warnThrottled`);
    `internal/server/db/self_update_repo.go`, `command_repo.go` (`GetForDevice`), `event_repo.go`
    (`HasCommandEvent`, `Create`); `internal/server/expect/selfupdate.go`, `safety_ex2_test.go`;
    `internal/server/ingest/limits.go`
  - models: `events.go`, `self_update.go`, `command_policy.go` (`IsValidOrigin`, `IsCanonicalUUID`, `StepPattern`)
  - agent: `reboot_clearance.go`, `commands.go` (`handleReboot`, `handleShutdown`, `execReboot`, `execShutdown`),
    `command_lifecycle.go` (`setStep`, `notifier.enqueue`, `completeCommand`), `powerguard.go`, `updater.go`
    (`runUpdateCheck`, `performUpdate`, `applyStaged`, `reportUpdateEvent`), `signed_update_handoff.go`,
    `cmdstate.go` (`atomicJSONFile`), `http_client.go` (`ReportEvent`, `StatusError`), `update_preflight_linux.go`
  - `cmd/riot-agent-apply/main.go` (hand-off readers), `scripts/install.sh` (unit hardening lines),
    `web/src/components/ActivityLog.tsx`
  - `ReceiveAgentEvent` at v2.78.0, v2.80.0 and v2.81.0 (git history), for the old-server case

No production system was queried. Pre-existing weaknesses found during this review are referenced here only
generically; their details are in Ryan's private review notes, not in this public repository.

---

## Threat Model Summary

### Assets
- **Alerting integrity.** The new server inputs (`command_id`, `attempt_id`, `from_version`, `to_version` on
  `agent_update_*`) decide whether a self-update record, and therefore a `device_offline` suppression window, is
  created. Anything that opens, extends, reopens or chains a window beyond the EXPECT-002 budget hides an outage.
- **Operator command integrity.** A device can now name a command by ID. It must only ever reach its own
  operator- or schedule-started `agent_update` command, and only to link an event to it.
- **The dpkg/power gate (REBOOT-DPKG-001, SHUTDOWN-DPKG-001).** Permit minting, the final fresh lock probe before the
  power action, the 10-minute bound and the guard order must be unchanged by the new wait reporter.
- **Event log and dashboard truth.** Linked events, the "(waiting for dpkg lock)" label and the record views.
- **Agent host.** No new privilege, no new root-read file, no new process execution; the hand-off file stays
  agent-owned.
- **Ryan's safety rule (NFR-001).** No test may run a real power action, package manager, `systemd-run`, agent
  update or an installed agent binary.
- **Bounded growth.** Rows, params bytes, events, goroutines, retries and logs.

### Threat Actors
- **Compromised or buggy agent holding its own device key.** It controls every field of its update events,
  their timing and its progress steps. The questions that matter: can it reach another device; can it buy more
  silence than the EXPECT-002 budget; can it flood rows; can it break the server's handling of the request?
- **Local process running as the `riot` user.** Can rewrite the agent-owned hand-off file.
- **Unauthenticated network attacker.** Nothing new is reachable without a device key.
- **Environment.** Server restart on release day, DB read errors, lost responses, a full notifier queue, slow disk.
- **Future developer (regression).** A test that reaches a real exec path; a later origin that silently becomes a
  valid command-reference target.

### Attack Surface Introduced
- Three optional fields on `POST /api/v1/devices/{id}/events` and a newly-read `command_id` on the three
  `agent_update_*` types (AD-004, AD-011).
- One device-scoped PK read per command reference; one select inside the existing `CreateSelfUpdate` transaction
  (step 0); one new repo read (`SelfUpdateByAttempt`); one `EXISTS` under a new process-wide mutex (AD-009).
- A new `params.attempt_id` key on self-update records, returned by the commands API.
- A new response body `{"status":"duplicate"}`.
- Agent: a context-carried wait reporter inside the power gate, one goroutine per standalone power command, a new
  step token `power_action`, a retrying event sender (≤ 3 slots), three new hand-off keys, `crypto/rand`.
- No new endpoint, privilege, sudo call, file, dependency, migration or installer change.

---

## Findings

There are no CRITICAL and no HIGH findings.

The architect's six focus points were checked against the code (INFO-1 to INFO-6). In summary:
- **AD-011 holds.** A command reference can only name the authenticated device's own command, its only effect is a
  link plus the absence of self-update record logic, and every invalid form falls back to EXPECT-002 without
  changing the status code. One predicate is a deny-list that becomes too wide once EXPECT-008 merges (SEC-002).
- **AD-006 step 0 holds.** A replayed attempt ID cannot create a record or move a window, and outcome transitions
  stay restricted to `running`/`verifying` under `ActorSelfUpdate`. Two edge properties must be stated (SEC-003).
- **AD-009 holds** within its stated single-process scope (R-1, same as EXTRA-UPD-001 N-2).
- **AD-001/AD-002 do not weaken the gate.** Signatures, permit minting and the final probe order are unchanged.
- **AD-010 is bounded.** It cannot amplify against the server. Two stop conditions need stating (SEC-004).
- **AD-012 holds.** No sudoers, unit, helper or installer change is needed.

The one MEDIUM item is a safety-rule gap. The update and hand-off code paths this story's tests drive fall back to
real process execution, and the test-binary guard the ADD relies on covers only the power actions (SEC-001).

### MEDIUM

#### SEC-001: The update and hand-off paths have no test-binary refusal, and the EX7 agent scan does not require a fake runner there
**Severity:** MEDIUM
**Domain:** Safety (NFR-001, Ryan's hard rule) / verification integrity
**Location:** ADD AD-015 ("`runner` … `inTestBinary()` already refuses a real power exec when it is nil"), AD-010,
AD-005, Section 8 rows AC-012, AC-013, AC-030 to AC-033; `internal/agent/updater.go`, `signed_update_handoff.go`,
`update_preflight_linux.go`, `powerguard.go`

**Description:**
- `inTestBinary()` is consulted only by `execReboot` and `execShutdown`. The agent-update pipeline and the
  hand-off watcher fall back to a real process runner whenever the `Agent`'s `runner` is nil. Nothing refuses that
  fallback inside a test binary.
- This story adds 16 AU tests that drive exactly these functions: `runUpdateCheck`, `performUpdate`,
  `handleTriggerUpdate`, `finishSignedHandoff`, and both `recoverAutomaticSignedHandoff` and `watchSignedHandoff`
  (AC-013 names the latter explicitly).
- The AZ scan forbids execution *forms in test files* and flags an `Agent` literal that sets `dpkgLockProbe` without
  `runner`. It does not flag an EX7 test that reaches the update or hand-off path with a nil `runner`, nor an
  `Agent` built by a helper constructor rather than a literal.
- Today the end-to-end reach to the privileged launch is limited by other conditions (details in Ryan's private
  notes). But a read-only `systemctl` query and a sudo permission probe are reachable from a test under realistic
  conditions, and AC-037 forbids any `systemd-run` invocation. The ADD's safety statement is therefore true for power
  actions only.

**Attack Scenario (regression):**
1. During WP-3 a developer writes `TestEX7_AC013_OldHandoffYieldsNoReference` for the `watchSignedHandoff` path and
   builds the agent with the shared test constructor, leaving `runner` nil.
2. The watcher's unit-state check runs a real `systemctl` on the CI runner or on a Linux developer host.
3. On a host that has the rIOt helper installed, a sibling AU test that calls the real `performUpdate` also runs the
   real sudo permission probe.
4. `TestEX7_AC037_AgentTestsRunNothingReal` passes, because no test file contains an exec form.

**Required Resolution (properties):**
- (a) In a test binary, every agent code path this story's tests can reach must be unable to start a real process
  when no fake runner is injected. This covers the update pipeline (preflight, apply launch, unit reset) and the
  hand-off watcher's unit-state query. The refusal must be the same kind of state-independent guard the power
  actions already have.
- (b) A self-test proves (a) for each of those paths without starting a process. An AST check pins that every
  real-runner fallback in `internal/agent` non-test code is preceded by that guard.
- (c) AD-015 is corrected so that it no longer implies the existing guard covers the update path, and it states (a).

**Blocks:** No. Condition C-1.

---

### LOW

#### SEC-002: The command-reference validity predicate is a deny-list, so EXPECT-008's `installer` rows become valid targets
**Severity:** LOW
**Domain:** Authorization (object reference) / defense in depth / merge coordination
**Location:** ADD AD-006 step 2 ("`Origin != agent_self_update`"), AD-011; FRD Section 4 definition (c); EXPECT-008
ADD AD-003 and AD-017 on `feat/expect-008`

**Description:**
- The FRD defines a valid command reference as one naming an `agent_update` command started by Ryan or a
  schedule. The ADD implements this as `Action == agent_update && Origin != agent_self_update && DispatchedAt != nil
  && (not terminal || FinishedAt ≥ at − 10m)`.
- EXPECT-008 (in flight, free merge order) adds `commands` rows with `action = 'agent_update'`, origin `installer`,
  `dispatched_at` set and status `running`. Those rows pass the AD-006 predicate. EXPECT-008's AD-017 fixes the
  equivalent SQL predicate in `CreateSelfUpdate` step 1, but it cannot know about this story's new Go predicate.
- An honest agent never holds an installer row ID, so the honest path is unaffected. A compromised agent naming its
  own device's installer row gets only a link and the absence of a self-update record. That reduces suppression and
  cannot extend it. It does break BR-004 (a self-update attributed to the installer) and FR-021's definition.
  It also lets agent-sourced rows join the installer record's linked events (at most one per type, through
  AD-009).

**Attack Scenario:**
1. Both stories have merged. Ryan re-runs `install.sh` on PiVPN, which opens an installer record I.
2. A compromised PiVPN agent sends `agent_update_failed` and `agent_update_started` with `command_id = I`.
3. Both are linked to I with `command_attr = agent` and appear in the installer record's history as if the installer
   reported them. No record is created, so no extra silence is gained, but the history is misleading.

**Required Resolution (properties):**
- (a) A command reference is valid only when the command's origin is one of the origins an operator or a schedule
  can create (today exactly the `IsValidOrigin` set). Any other origin, present or future, is invalid by default.
- (b) `TestEX7_AC023_InvalidCommandRefsFallBack` covers a server-created origin other than `agent_self_update`. It
  can use a synthetic origin value in the mock if EXPECT-008 has not merged yet.
- (c) Whichever of EXPECT-007 and EXPECT-008 merges second re-runs that case against the real `installer` origin,
  and the merge note names this finding.

**Blocks:** No. Condition C-2.

---

#### SEC-003: Four edge properties of AD-006 and AD-009 are implied but not stated
**Severity:** LOW
**Domain:** Business logic (state machine) / integrity of the event log / robustness of the request path
**Location:** ADD AD-006 steps 2 to 4, AD-009, Section 6.5 (`AgentUpdateEventLink.Dedupe`), Section 12 note 7;
`commands/self_update.go` `noteStart` (it broadcasts `res.Retired` and `res.Command` unconditionally)

**Description:**
The design is correct, but four of its properties rest on details the ADD does not state:
1. **`attempt_match` must not touch the tracker.** `noteStart` broadcasts `res.Retired` and `res.Command` to the
   observer (the expected-activity tracker's hook) and to the dashboards for every disposition. FR-024 and AC-018
   ("no window change") hold only if an `attempt_match` result carries neither. If an implementer fills
   `Command` with the matched record (it is convenient for the link), a replay re-announces an open or closed
   record to the tracker. The tracker's coverage is computed from durable timestamps, so this is very likely
   harmless today. It is still exactly the "replay touches a window" path that FR-024 rules out.
2. **De-duplication needs a non-empty key.** `HasCommandEvent(device, "", type)` is true for *any* earlier unlinked
   event of that type. If `Dedupe` were ever true with an empty `CommandID` (for example a capped start, or a
   fail-open empty link, flagged by reference kind instead of by link source), later events would be silently
   dropped as duplicates.
3. **A terminal row without `finished_at` must be invalid, not a crash.** The step-2 predicate dereferences
   `FinishedAt` on a terminal row. A nil there must yield "invalid" (fall back), never a panic. A panic would turn a
   reference into a failed request and a lost event, which FR-022 and NFR-008 forbid.
4. **The gate may only call the non-blocking reporter method.** AD-001 places `enter()` in the gate. `settle()` and
   `leave()` block. The planned AST rule pins where the reporter is *attached*, not which method the gate calls.

**Attack Scenario (regression):**
1. During WP-5 the implementer returns `SelfUpdateCreateResult{Disposition: attempt_match, Command: &rec, LinkID:
   rec.ID}` from step 0.
2. A compromised hawaii agent replays its last attempt ID every few seconds within the ingest limit. Each replay
   re-announces the record to the tracker and pushes a `command_update` to every open dashboard.
3. If a later tracker change derives anything from "record announced at time T", replays start moving a window. No
   EX7 test asserts the absence of the observer call, so nothing catches it.

**Required Resolution (properties):**
- (a) An `attempt_match` start produces no observer call and no dashboard `command_update`. AC-018 and AC-034 assert
  zero observer calls for the replay.
- (b) `Dedupe` is true only when the link's `CommandID` is non-empty and the link came from a validated reference
  (command reference, `attempt_match`, `created` with that attempt, or `SelfUpdateByAttempt`). A unit test covers a
  capped start and a fail-open start carrying an attempt reference: both are stored.
- (c) A terminal command with no `finished_at` is an invalid reference. The case is in
  `TestEX7_AC023_InvalidCommandRefsFallBack`.
- (d) The AZ AST rule also asserts that `awaitPowerClearance` calls no reporter method other than `enter`.

**Blocks:** No. Condition C-3.

---

#### SEC-004: The retry sender's stop conditions on agent shutdown and its busy-slot path are not specified
**Severity:** LOW
**Domain:** Abuse prevention / bounded resources (Ryan's bounded-growth rule)
**Location:** ADD AD-010 (classification table, "Bound per process", `deliverAsync`), AD-014 rows "Agent retry
state" and "Agent goroutines", Section 9

**Description:**
- The classification retries on "any other error (transport, timeout, context deadline)". It does not distinguish
  the per-attempt timeout from cancellation of the agent's own context. If the parent context is cancelled (agent
  stopping, `restart()`), a sender that treats that error as retryable keeps its slot and goroutine until the
  budget ends. The sleeps are context-aware, so this is bounded by about 100 s. But it delays a clean stop and can
  hold `cmdWG` past the shutdown grace.
- "An event whose slot is busy gets exactly one attempt and no retries" does not say where that attempt runs. On the
  command path (`deliverAsync`, FR-034, X-5), running it in the caller's goroutine would put up to one request
  timeout (10 s) in front of the update. Running it on a new goroutine is fine, but it is then outside the stated
  "≤ 3 retriers" bound.
- Neither gives an attacker anything. Both are about keeping the stated bounds true.

**Attack Scenario (failure, not an attacker):**
1. Release day: riot-server is restarting while vermont's agent receives SIGTERM from a package-triggered restart.
2. The `deliverFirstSync` retrier for `agent_update_failed` sees `context canceled`, classifies it as transport
   failure and keeps sleeping and retrying.
3. The agent's shutdown waits on `cmdWG` until the command context's grace ends, and the stop is slower than today.

**Required Resolution (properties):**
- (a) A retry loop ends at once, without a Warn, when the agent's own context (not the per-attempt timeout) is done.
- (b) The busy-slot single attempt never runs on the command-path caller's goroutine. AD-014 states the goroutine
  bound including it.
- (c) Lines above Debug from the sender carry only the event type and attempt count, never the `StatusError` body
  or the device key. `TestEX7_AC031_BudgetBoundedOneWarn` asserts the Warn's attribute set.
- (d) AU covers (a) and (b): `TestEX7_NFR003_*` with a cancelled agent context, and AC-033 with a busy slot.

**Blocks:** No. Condition C-4.

---

### INFORMATIONAL

#### INFO-1: AD-011 verified (focus point 1)
- **Gate.** `linkAgentUpdateEvent` runs only when `authenticatedDeviceID(r) == {id}`. The ADD passes the new raw
  fields only inside that gate, so another device's key, or a mismatched `{id}`, never has its reference read
  (AC-023 last bullet, AC-029).
- **Scope.** `GetForDevice` is `WHERE id=$1 AND device_id=$2`, and `IsCanonicalUUID` (exactly 36 bytes) runs before
  it. A foreign or unknown ID returns `ErrCommandNotFound`, which the ADD treats as invalid. There is no oracle:
  every invalid form, including another device's real command ID, yields the same 200 and the same EXPECT-002
  processing.
- **Effect.** A valid reference yields only `link = (C, agent)` and skips record logic. It never transitions C,
  never creates, extends or reopens a window, and never changes a record. A start linked this way is exactly what
  EXPECT-002 step 1 already did by timing (`attributed_command`), and a valid reference requires a dispatched,
  recent `agent_update` of the same device. So a compromised agent gains nothing over EXPECT-002: it can already
  avoid a record by not sending a start. Completed/failed linked to C no longer end a concurrent self-update record
  (AC-021). That is no gain either: the agent could simply not send them.
- **Storage.** The agent's `command_id` is never stored on an event. Only `link.CommandID`, computed by the server,
  is. Older servers (v2.78.0, v2.80.0, v2.81.0 checked) never stored or read it on these types.
- **Fallback.** Malformed, both-kinds, unknown, foreign, wrong action, self-update, undispatched, stale and
  read-error references all become `RefNone`, with a Debug line that names no agent text, and the status code is
  unchanged.
- Residual: the origin predicate (SEC-002).

#### INFO-2: AD-006 step 0 and FR-025 verified (focus point 2)
- Step 0 runs inside `createSelfUpdate`'s transaction, after `pg_advisory_xact_lock('riot.cmd.' || device)`, and is
  filtered by `device_id`. Concurrent replays of A serialise on the lock, and the second sees the first's row.
  Only `CreateSelfUpdate` inserts self-update rows, so uniqueness per (device, A) holds without an index.
- Step 0 sits before the stale-retire, open-record, cap and insert steps. A replay therefore cannot count toward
  the cap, cannot retire a stuck row, and cannot insert. A replay that matches a stuck row keeps that row in place.
  That blocks new records (less suppression) and is visible through EXPECT-002 C-4.
- A replay older than retention (90 days) behaves like a fresh attempt ID, which the agent can generate at will.
  The cap of 6 per 24 h and `hard = D + 10m` in server time still bound it. No gain.
- FR-025 "any age": outcome transitions keep `From = {running, verifying}` and `ActorSelfUpdate`. A terminal record
  is link-only. A completion before the return keeps coverage at most until `hard` (EXPECT-002 X-1). A failure ends
  coverage early. Both move toward alerting.
- FR-026: an unknown attempt ID can only fall back to a record with no attempt ID, so one attempt's completion can
  never close another attempt's record.
- Residual: SEC-003 (a).

#### INFO-3: AD-009 verified (focus point 3)
- The check and the insert share one process-wide mutex, held for one `EXISTS` and one `INSERT`. Link computation,
  including every state change, happens before the mutex and is idempotent for repeats, as the ADD argues:
  - a replayed start hits step 0;
  - a repeated command-referenced event is link-only;
  - a repeated outcome finds the record terminal.
- A failed duplicate check stores the event (NFR-008). If a first delivery committed a record but failed its event
  insert (500), the retry re-links through step 0, finds no stored event and stores it.
- Timing-linked events are not de-duplicated (X-2). They are bounded by the agent's 5 attempts and only when a
  response was lost.
- R-1 (one process) is the accepted EXTRA-UPD-001 N-2 scope. The mutex is global across devices. A device is
  bounded by its ingest bucket, so it cannot hold the mutex meaningfully longer than one slow insert.
- `command_attr = agent` is not in the server-authored predicate of `ListByCommandSplit`. The EXPECT-002 C-3
  record view therefore still shows server-authored rows first.
- Residual: SEC-003 (b).

#### INFO-4: AD-001/AD-002 verified against the gate (focus point 4)
- `enter()` sits in the existing `if !waiting` block, after `firstHeld` is set and before the timeout check and
  the sleep. It only starts a goroutine. The gate's probes, guard order, `sawHeld` and "absent after held" rule, the
  deadline from `firstHeld`, and the "fresh probe, then `cleared()` mints the permit" ending are unchanged. The
  reporter has no return value and no access to `mintPowerPermit` or the guards.
- The derived context carries only a value. Cancellation, guards and sleeps see the same deadline. The post-patch
  callers pass contexts not derived from a standalone `cmdRun`, so they never carry a reporter. The AST rule pins
  the attach sites.
- Between the gate's return and `execReboot`/`execShutdown`, the only code is the `!c.Cleared` branch. `leave()`
  runs after the exec returns, so REBOOT-DPKG-001 FR-012 and its 10-second bound are untouched.
- `setStep` takes `cr.mu`, so the goroutine and the handler do not race on `cr.steps`. The goroutine is joined on
  every return path (`settle()` on skip, `leave()` after exec).
- Side effect, not a security issue: `leave()` adds the goroutine join and one durable step write in front of the
  result's durable write after a power action has started. On a fast reboot this slightly widens the window in which
  the result is lost and the command ends `timed_out` (ADD Q-3). QA should confirm that `TestEX7_AC002_*` measures
  that gap on the fake clock.
- Residual: SEC-003 (d).

#### INFO-5: AD-010 and AD-005 verified (focus point 5)
- **Bounds.** At most 5 attempts per event, starts at most 90 s after the first, and a 10 s per-attempt context.
  That is at most 15 requests per update attempt, against an ingest bucket of 120 burst and 2/s per device. 401,
  403, 400 and other 4xx stop at once, so a revoked key is never hammered. 429 and 5xx back off on a fixed widening
  schedule. Only the self-update start blocks, for at most 100 s, with the host lock held (A-008). The command path
  never waits.
- **Body read.** `ReportEvent` moves from an unbounded body read to `newStatusError`'s bounded read. That is an
  improvement.
- **Secrets.** The device key travels only in the `X-rIOt-Key` header. Transport errors carry the URL, which holds
  the device ID only. The `StatusError` body is Debug-only, subject to SEC-004 (c).
- **Attempt ID.** It is 128 bits from `crypto/rand`, hex-encoded. It is a correlation value, not an authenticator:
  every use is gated by the device key and scoped to the device, so its secrecy is not a control. On Go 1.24
  (`go.mod`), `crypto/rand.Read` does not return errors (it aborts the process), so the R-4 "no ID" branch is
  effectively unreachable. That is harmless.
- **Hand-off file.** It is written through the existing `atomicJSONFile`: a temp file created 0600 in a 0700
  directory, fsync, then a rename. The rename replaces rather than follows a link at the destination. The file is
  read only by the agent itself (`riot-agent-apply` does not read it; verified). A local `riot`-user writer can
  change only what this device reports about itself, and the server validates all of it. The agent re-validates
  the attempt ID before sending, but not the versions. The server ignores outcome versions (FR-019), so that is
  harmless. The pre-existing read path's properties are noted privately.

#### INFO-6: AD-012 verified (focus point 6)
- New agent code runs no command. `crypto/rand`, writes to the existing hand-off path, and HTTP to the existing
  endpoint are all inside the current unit (`ReadWritePaths` covers the data directory) and need no capability.
- `power_action` and `wait_dpkg_lock` match `StepPattern` (`^[a-z0-9_]{1,64}$`), so old and new servers accept the
  progress report.
- `riot-agent-apply` does not read the hand-off file, so its new keys reach no root code.
- No sudoers, unit, helper, `riot-apply-privileges`, configuration-schema, collector or `doctor.go` change is
  needed. `TestEX7_AC036_NoNewSudoInvocation` and the Section 13 diff check are adequate.

#### INFO-7: Untrusted fields, sanitisation and per-device bounds
- **Validation before any use:**
  - `attempt_id` and `command_id`: 1 to 64 bytes of `[A-Za-z0-9-]` (`ValidRefToken`), and `IsCanonicalUUID` for the
    command kind; "both present" is checked first;
  - `from_version` and `to_version`: the EXPECT-002 version grammar (≤ 32 bytes, RE2) and must differ;
  - outcome versions are never stored or shown.
- **Logs and storage.** Nothing agent-supplied is logged above Debug or in `warnThrottled` (Section 12 note 9).
  `attempt_id` reaches storage only after validation and is re-validated on read.
- **Messages.** FR-016 decouples record creation from the message wording, so a record-opening start's stored
  message can now be any agent text. `EventRepo.Create` still sanitises it, masks it and cuts it to 2 KB. It never
  reaches a push, label or result message on the start path. This is no new capability: the agent controls both
  inputs.
- **Display.** AD-008 says the web does not render `params.attempt_id`. `ActivityLog.tsx` renders any command's
  `params` as JSON text (escaped by React). A validated attempt ID may therefore appear there. That is harmless, but
  the ADD sentence should say so (C-5).
- **Per-device-per-day bounds with a compromised device key:**

  | Item | Bound |
  |---|---|
  | Self-update records | ≤ 6 per rolling 24 h (unchanged), ≤ 540 per 90 days |
  | `params.attempt_id` | ≤ 64 B per record, so ≤ 384 B per day |
  | Unalerted offline time | EXPECT-002 SEC-005 budget, unchanged (about 57 min/day, ≤ 6 outages of under about 10 min) |
  | Stored events | the pre-existing ingest ceiling (2/s per device); reference-linked repeats are no longer stored |
  | Server work per event | ≤ 1 PK read, or one select in the existing transaction, or 1 to 2 selects, plus ≤ 1 `EXISTS` |
  | Server memory | one mutex, no per-device or per-reference state |

  No field lets one device's key reach another device's rows, windows, labels or de-duplication keys.

#### INFO-8: The EX2/EX7 scans and the new test files
- The EX2 scan's part (c) covers every `_test.go` in `internal/server`, `internal/server/{db,commands,expect,handlers}`.
  Its literal list forbids the package-manager lock name, the root apply helper's name and `systemd-run` inside
  any string literal.
- The new server test files S, HT, R, H7, Z and D all live there. D (`internal/server/expect7_docs_test.go`) must
  check README phrases required by FR-040 (the dpkg-wait label, the F16 re-scope naming the apply helper) that
  contain those words. Section 12 note 6 forbids the literals and new `ex2Preexisting` entries, but it does not say
  how D satisfies FR-040.
- **Risk.** Under pressure, an implementer adds an exception entry (to `ex2Preexisting` or the new
  `ex7Preexisting`) for an EX7 file, which weakens the scan for that file.
- **QA check (C-5).** No EX7 file appears in either exception table, and D meets FR-040 without a forbidden literal.
  The existing scan file shows the accepted technique.

---

## Positive Observations
- **The field name is the kind.** The server never infers a reference kind from its value, and "both present" is
  rejected before either is validated.
- **Evidence only narrows.** A valid command reference removes record logic and adds none. A valid attempt
  reference can only match, never create, beyond the EXPECT-002 rules.
- **Step 0 is inside the existing locked transaction**, so there is no TOCTOU between "does A exist" and the insert.
  No migration, no new index and no collision with EXPECT-008's 000035.
- **Fail-open is consistent.** Every lookup error falls back to the EXPECT-002 path with an unchanged status code.
  A failed dedupe check stores rather than drops.
- **The gate is left alone.** No signature change, the reporter is passed by context, its only gate-side method is
  fire-and-forget, and the leaving step is ordered after the power action.
- **The waiting suffix is decided server-side** with `dev.online`, so the API and the UI cannot disagree (BR-006).
- **The retry policy is nil by default.** Existing tests cannot start sleeping, and production opts in at the
  composition root.
- **No new privilege and no new root-read input.**
- **EXPECT-002 C-1 to C-6 are carried forward explicitly** (ADD Section 8.2).

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.** There are no CRITICAL and no HIGH findings.

- Cross-device isolation holds by construction: the authenticated-ID gate, a device-scoped PK read, and a
  device-scoped step 0.
- A reference can only reduce suppression or link. A replayed attempt cannot create a record or move a window.
- The silence budget is unchanged from EXPECT-002 SEC-005.
- The power gate's permit, guard and final-probe order are untouched.
- Retries are bounded and cannot amplify against the server.
- No privilege or installer change is needed.

The conditions:
- close a safety-rule gap that predates the story but which this story's tests exercise directly (SEC-001);
- turn a deny-list into an allow-list before EXPECT-008 merges (SEC-002);
- state four edge properties the design relies on (SEC-003);
- state the retry sender's stop conditions (SEC-004).

None needs a new component, a migration or an FRD change.

### Conditions (the architect amends the ADD where stated; senior_dev implements; QA verifies)
1. **C-1 (SEC-001). ADD changes in AD-015, Section 8 rows AC-012, AC-013, AC-030 to AC-033 and Section 12:**
   - In a test binary, no agent path these tests reach (update preflight, apply launch and unit reset, hand-off
     unit-state query) can start a real process without an injected fake runner. The refusal is state-independent,
     like the power guard.
   - Each such path gets a self-test, and an AST check pins the guard in front of every real-runner fallback in
     `internal/agent`.
   - AD-015 no longer implies that the power guard covers the update path.
2. **C-2 (SEC-002). ADD changes in AD-006 step 2 and AD-011:**
   - A command reference is valid only for an operator- or schedule-created origin (today the `IsValidOrigin` set).
     Every other origin is invalid by default.
   - `TestEX7_AC023_*` covers a server-created origin other than `agent_self_update`.
   - The second of EXPECT-007 and EXPECT-008 to merge re-runs that case against `installer`.
3. **C-3 (SEC-003). ADD changes in AD-001, AD-006 and AD-009:**
   - `attempt_match` produces no observer call and no dashboard `command_update`; AC-018 and AC-034 assert it.
   - `Dedupe` requires a non-empty linked `CommandID` and a reference-derived link. A capped start and a fail-open
     start that carry an attempt reference are both stored.
   - A terminal command with no `finished_at` is an invalid reference.
   - The AZ AST rule asserts that the gate calls only `enter` on the reporter.
4. **C-4 (SEC-004). ADD changes in AD-010 and AD-014:**
   - Retries stop at once, without a Warn, when the agent's own context ends.
   - The busy-slot single attempt never runs on the command-path caller's goroutine, and the goroutine bound
     includes it.
   - Sender lines above Debug carry only type and attempt count.
   - AU covers these cases.
5. **C-5 (QA gate, in addition to ADD Section 13):**
   - No EX7 file appears in `ex2Preexisting` or `ex7Preexisting`, and `TestEX7_AC040_*` meets FR-040 without a
     forbidden literal (INFO-8).
   - `TestEX7_AC002_*` shows on the fake clock that nothing runs between the gate's final probe and the power
     runner, and that the leaving step follows the runner call (INFO-4).
   - `TestEX7_AC023_*` and `TestEX7_AC029_*` include another device's real command ID and record attempt ID, and
     assert identical 200 responses and EXPECT-002 processing (INFO-1).
   - `TestEX7_PG_ConcurrentSameAttemptOneRecord` passes in CI with PostgreSQL (INFO-2).
   - The AD-008 sentence about the web not rendering `attempt_id` is corrected (INFO-7).

### For Ryan's decision
- **AD-011, reading the agent's `command_id` on update events (reverses EXPECT-002 X-2).** **Recommendation:
  accept**, with C-2. It cannot reach another device and cannot add silence.
- **Section G built now (AD-010), with up to 100 s of extra host-lock hold on a self-update when the server is
  unreachable.** **Recommendation: accept**, with C-4.
- **R-1, single-process de-duplication.** **Recommendation: accept**, as for EXTRA-UPD-001.
- **The pre-existing items** referenced generically in SEC-001 and INFO-5 are described in Ryan's private review
  notes. C-1 covers the part this story exercises.
