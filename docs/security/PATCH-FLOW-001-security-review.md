# Security Review Report
**Story ID:** PATCH-FLOW-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-23
**Verdict:** APPROVED WITH CONDITIONS

Inputs: `docs/requirements/PATCH-FLOW-001-frd.md`, `docs/architecture/PATCH-FLOW-001-add.md` (revision 2, with
AD-012), and the code they reference or affect:
- server: `internal/server/handlers/commands.go`, `fleet.go` (`BulkPatchDevices`, `PatchStatus`),
  `auto_update.go` (`checkAutoPatch`), `handlers.go` (`Telemetry` ingest), `router.go`,
  `middleware/sameorigin.go`, `commands/service.go` (`Dispatch`), `events/generator.go`, `db/telemetry_repo.go`
- agent: `internal/agent/commands.go`, `maintenance.go`, `command_lifecycle.go`, `cmdstate.go`, `telemetry.go`,
  `collectors/updates.go`
- helper: `internal/maintenance/worker.go`, `protocol.go`, `runner_linux.go`, `cmd/riot-maintenance/main_linux.go`,
  `scripts/riot-maintenance.service`, the sudoers block in `scripts/install.sh`
- prior reviews: `PATCH-GATE-security-review.md` (SEC-PATCH-GATE-009), `MAINT-RECOVERY-001-security-review.md` (SEC-5)

---

## Threat Model Summary

### Assets
- **Host availability.** A reboot is the only new destructive effect. The kernel/GPU hold release is the second: it
  lets a kernel install without a planned reboot window.
- **Package-state integrity.** A reboot during a dpkg transaction leaves half-configured packages.
- **At-most-once reboot per command**, enforced by the `patch-reboot-intents.json` store (AD-012).
- **Truth of the audit trail.** The command record, the `command_sent` event, and the FR-023 result suffix.
- **Security-patch activation.** Installed kernels only protect the host after a reboot. A store that silently
  suppresses post-patch reboots leaves hosts running vulnerable kernels.
- **Root helper integrity.** `riot-maintenance` runs as root with no systemd sandboxing, and it gains a filesystem
  write: the mtime of `/var/lib/apt/lists`.

### Threat Actors
- **Holder of a stolen admin session or a CSRF attacker.** They already reach `reboot` and `os_update` through the
  same route.
- **Holder of a device API key**, or a compromised agent. Such an actor can spoof `allow_reboot`,
  `supports_reboot_if_required` and `lists_freshness`. The `riot` user already has sudo for `systemctl reboot`,
  `apt-mark hold/unhold <any package>` and starting the helper. A compromised agent therefore already controls the
  whole patch and reboot path, and this story gives it nothing new.
- **Compromised server or MITM on the WebSocket.** It can send any params. The PATCH-GATE claim must still hold: the
  agent-side `allow_reboot` veto cannot be bypassed.
- **Local unprivileged user on the host.** Cannot write `/etc/apt`, `/var/lib/apt`, `/boot`, `/run` or
  `/var/lib/riot`.
- **Environment faults** (not malicious): disk corruption, clock jumps on hosts without an RTC, agent downgrade.
  These drive the self-heal questions.

### Attack Surface Introduced
- A new boolean `reboot_if_required` on the existing admin route `POST /api/v1/devices/{id}/commands`. It is the
  second release path for `include_reboot_class` (amends PATCH-GATE BR-005).
- Two new self-reported telemetry fields, `lists_freshness` and `supports_reboot_if_required`. They gate a server
  decision, but only toward rejection.
- A new agent reboot trigger, `runAuthorizedReboot`: a third `execReboot` call site.
- A rewritten, self-pruning intent store (v2 format, legacy v1 load).
- Agent filesystem reads: `/etc/apt/**`, `/var/lib/apt/lists`, `/boot`, `/proc/sys/kernel/osrelease`.
- Root helper: two fingerprint passes (stat only), one extra allowlisted `apt-get update`, and two `Chtimes` calls on a
  fixed path.

---

## Findings

No CRITICAL or HIGH findings.

The amended BR-005 is sound at the API boundary:
- **Who can send it.** `SendCommand` sits behind `AdminAuthSameOrigin`. That means the JWT cookie, plus
  `Sec-Fetch-Site`/`Origin` checks and a JSON-only body, which closes CSRF.
- **Existing powers.** Any principal who can tick the box can already press Reboot, and can already release holds by
  setting `reboot_class: gated` with auto-patch on. This story grants no new power to a stolen session.
- **Replay.** A replay needs the session and passes the origin check. Each replay is a new command ID, so it causes at
  most one reboot, which equals pressing Reboot. The creation rate limiter still applies.
- **Strict boolean.** Decoding into `map[string]interface{}` yields a Go `bool` only for JSON `true` or `false`.
  Strings, numbers, `null` and objects are rejected (AC-015).
- **No parser differential.** Duplicate keys resolve last-wins once, on the server, and the stored params are
  re-serialised from the map.
- **Client `include_reboot_class` still stripped.** The strip runs first and unconditionally (AD-004 step 1).
- **Other creation sites.** I checked every `os_update` creation site. `BulkPatchDevices` builds `{"mode": ...}`
  itself. `checkAutoPatch` builds `{"mode":"full"}` plus the gated flag. No other path carries client params. The
  automated paths stay unchanged.
- **Capability trust.** `supports_reboot_if_required` and `allow_reboot` can only turn a dispatch into a 400
  (NFR-003). They never create one. Spoofing them `true` makes the server accept an operator's tick, and then the
  real agent's `allow_reboot` decides. Spoofing them `false` only denies the convenience feature.
- **Old or downgraded agent with spoofed or stale capability (R-5).** That agent falls back to PATCH-GATE rules: holds
  are released, and it reboots only on applied reboot-class packages with local `allow_reboot`. That outcome is still
  inside what the operator authorised.

### MEDIUM

#### SEC-001: A corrupt intent store suppresses every post-patch reboot forever, including the automated path (R-6)
**Severity:** MEDIUM
**Domain:** Business logic / availability of security-patch activation / bounded growth and self-heal (BR-008)
**Location:** ADD AD-012 claim step 2, §10 R-6; `claimPatchReboot` (`maintenance.go:398`); `atomicJSONFile.Load`
(`cmdstate.go:114`)
**Description:**
On a read or parse error, the store returns `claimPersistFailed` on every claim and never recovers. The same store
gates the automated PATCH-GATE path (`maybeRebootAfterPatch`). On that path a suppressed claim is silent: it only
triggers telemetry, and nothing reaches the dashboard. Meanwhile the badge can keep saying
`Required now. Automatic on <D> UTC (estimate).` for a host that will never reboot. The ADD's reason to stay closed,
"reset would break at-most-once", holds only while the file could contain an intent whose command can still reach the
reboot decision. AD-012 itself proves that window is at most 72 h (`rebootIntentMinKeep`).

The codebase's own precedent is weaker still. The seen-ID store, which is the primary dedupe, starts empty on
corruption (`seenStore.Load`). It also evicts oldest-first past 1,000 entries, even inside 48 h. So the intent store is
the real at-most-once guarantee, and it must be the strict one. But a permanent fail-closed state is not bounded, and
it is not self-healing.

`Load` also reads the whole file with no size bound. The file is owned by `riot`, so this is low-risk, but it is
unbounded.

**Attack Scenario (fault, not adversary):**
1. A power loss on an SD-card host (a Raspberry Pi) corrupts `/var/lib/riot/patch-reboot-intents.json` (a torn block).
2. The host is on gated auto-patch. Nightly runs install kernels.
3. Every claim fails, so there is no reboot, and only a Warn line appears in the agent journal.
4. For months the host runs the vulnerable kernel. The dashboard says an automatic reboot is scheduled.
5. Ryan sees no needs-you reason, because `autoCovers` is true.

**Required Resolution (properties):**
- (a) **Self-heal.** An unreadable, unparseable or oversized intent store fails closed only while it could hold an
  intent younger than `rebootIntentMinKeep` (72 h). The age comes from evidence the agent wrote itself, for example
  the store's last successful write time, which every claim updates. A timestamp in the future, or one that cannot be
  read, counts as young.
- (b) **Recovery.** Once that age has passed, the next claim keeps the bad content as **one** bounded set-aside copy
  (it overwrites any previous copy), starts an empty v2 store, and proceeds. There is no operator action and no
  unbounded growth.
- (c) **Bounded read.** A store larger than a fixed bound (comfortably above 256 entries) counts as corrupt, and the
  agent never reads it whole.
- (d) **Visible suppression.** A post-patch reboot suppressed for `claimPersistFailed` or `claimStoreFull` emits one
  agent warning event per command, on **both** the authorized and the automated path. A log line alone is not enough.
- (e) **Tests:** corrupt file younger than 72 h stays fail-closed; older than 72 h self-heals with exactly one aside
  copy; future mtime stays fail-closed; oversized file counts as corrupt; the automated path emits the event.
- Update ADD §10 R-6 and the technical-writer note 9 to match.

**Blocks:** No. This is condition C-1. It changes the failure branch of AD-012 only.

#### SEC-002: Phase B can consume the claim, report "reboot started: yes", and then not reboot, systematically on its own helper unit
**Severity:** MEDIUM
**Domain:** Business logic / audit integrity (NFR-010, FR-023)
**Location:** ADD AD-005 phase B step 2, R-2; `maintenanceBlocker` (`maintenance.go:115`); the unit `active` probe
(`maintenance.go:37-57`)
**Description:**
Phase A claims the intent and writes `reboot started: yes` before the result is queued. Phase B then checks
`maintenanceBusy` immediately. `maintenanceBusy` treats the unit states `activating`, `active` and `deactivating` as
busy. In the normal apt flow, `observeMaintenance` returns as soon as `ReadTerminal(id)` succeeds. The helper writes
that terminal record inside `Execute`'s defer, and the process exits after that write. So the helper unit of the very
run being reported can still be `active` or `deactivating` when phase B runs. In that case phase B aborts. The claim is
consumed, the result already says the reboot started, and nothing retries.

The ADD calls the race a "microsecond window" (R-2). This variant is not a microsecond window. It depends on how long
the helper takes to exit after `Finish`, which includes `store.Close`, the lock close, and systemd's own unit-state
transition, measured against a 1 s observer poll. The existing automated path (`maybeRebootAfterPatch`) has the same
latent race, which may explain why no post-patch reboot has been observed in production.

The direction is safe: no reboot. But the command record then states a false fact. The operator may also leave the
host on the old kernel, believing it rebooted.

**Attack Scenario (fault):**
1. Ryan ticks the box. The vermont run succeeds and installs a kernel.
2. The agent reads the terminal record 50 ms before the helper process exits.
3. Phase B sees `deactivating` and aborts.
4. The command shows `reboot started: yes`. The host is still on the old kernel.
5. A later tick with a new command does reboot it, but nothing told Ryan the first one failed except a warning event
   that is easy to miss.

**Required Resolution (properties):**
- (a) Phase B must not abort only because the helper unit of **this same command** is still `active` or
  `deactivating` after its terminal record exists. The busy test must count only other maintenance work. Or it must
  wait, inside the existing bounded wait, for the unit to become inactive before it decides.
- (b) Every phase B abort that follows `reboot started: yes` emits the warning event that names the command ID and the
  reason. The ADD already specifies this; QA verifies it. Telemetry is also triggered.
- (c) **Tests:** unit state `deactivating` at phase B start that turns `inactive` within the wait → the reboot proceeds;
  a different command's helper active → abort with the event. Apply the same fix to `maybeRebootAfterPatch`. That
  changes no reboot rule (FR-026); it only removes a false "busy".

**Blocks:** No. Condition C-2.

---

### LOW / INFORMATIONAL

#### SEC-003: Reserved param keys are matched case-sensitively; any future typed decode becomes a strip bypass
**Severity:** LOW. **Domain:** Input validation / defense in depth. **Location:** AD-004 steps 1–2; `SendCommand`.
**Description:** The server deletes only the exact key `include_reboot_class` and passes every other key through to
storage and the agent. Today the agent reads params by exact map lookup, so `"INCLUDE_REBOOT_CLASS": true` is inert.
If any future refactor decodes `os_update` params into a Go struct, `encoding/json` matches field names
case-insensitively. That variant would then survive the strip, and holds would be released without authorization.
**Attack Scenario:** An admin-session holder, or a CSRF attacker if origin checks ever regress, sends
`{"mode":"full","Include_Reboot_Class":true}` against a future agent that uses a typed params struct. Holds are
released on a manual run with no tick.
**Required:** At the API boundary, after processing, a manual `os_update` holds no key that case-insensitively equals
`include_reboot_class` unless AD-004 set it. A case variant of `reboot_if_required` other than the exact key is
rejected with `400 invalid_request`. An allowlist of `os_update` param keys is an acceptable way to satisfy this.
Add a test (C-3).

#### SEC-004: `TouchLists` checks with `Lstat` and then calls `Chtimes`, which follows symlinks, as root
**Severity:** LOW. **Domain:** Privileged filesystem operation. **Location:** AD-007 production wiring;
`cmd/riot-maintenance/main_linux.go`.
**Description:** `os.Chtimes` calls `utimensat` without `AT_SYMLINK_NOFOLLOW`. A swap between the `Lstat` and the
`Chtimes` would redirect a root mtime write. Today only root can swap it, because `/var`, `/var/lib` and
`/var/lib/apt` are root-owned and not group- or world-writable. The impact is limited to timestamps. Still, the helper
is unsandboxed root, and this is the first write it makes outside its own state directory.
**Attack Scenario:** On a host where `/var/lib/apt` was made group-writable (a misconfiguration, or a package with a
bad postinst), a local user races `lists` → symlink to `/etc/shadow`. Root then sets that file's mtime. That defeats
the agent's stale detection or tampers with mtime-based forensics, though it does not escalate privilege.
**Required:** The timestamp update must not follow a symlink at the final component. It must act on the same object
that was verified as a root-owned real directory, using a no-follow or descriptor-based update. On any doubt, skip
with the existing bounded note. The test in `main_linux_test.go` must cover a symlink swapped in after verification,
where feasible (C-4).

#### SEC-005: `compareDebVersion` takes length-limited but untrusted file names; overflow and panics must be impossible
**Severity:** LOW. **Domain:** Input handling. **Location:** AD-002.
**Description:** Release strings come from `/boot` file names. The charset and the 64-byte limit are enforced. A
naive `verrevcmp` port accumulates digit runs into an `int`. A 64-digit run overflows silently, which gives a wrong
order. Index arithmetic on `~` and the end of the string is also a classic source of panics. A wrong order flips
`reboot_required`, and an authorized run then reboots, or does not. It is bounded to once per command, so this is
LOW. A panic would happen in the collector, on every cycle.
**Required:** The comparator is total and never panics on any string that passes the charset. It compares digit runs
without integer overflow (leading zeros stripped, then length, then lexical). Add a Go fuzz test (`FuzzCompareDebVersion`) that
checks antisymmetry and that nothing panics, plus table cases with digit runs longer than 19 digits (C-5).

#### SEC-006: Directory bounds must bound the reads, not just the decision
**Severity:** LOW. **Domain:** Bounded growth / DoS. **Location:** AD-002 (`/boot`, 256 entries), AD-003 (1,000
entries, 2 s).
**Description:** `os.ReadDir` reads and sorts the whole directory before any count is checked. The 256 and 1,000 caps
only bound memory and syscalls when the code reads in chunks. The ADD specifies `ReadDir(128)` chunks for freshness,
but says nothing about the `/boot` read. The 2 s deadline is checked only between chunks. A `stat` that blocks (for
example, a symlink in `sources.list.d` into a hung network mount) cannot be pre-empted, and the same holds for the
root helper's fingerprint. Only root can create such entries, so this is informational for the deadline.
**Required:** Every new directory read, in the agent and in the helper, is a chunked read that stops at the cap
(C-6). Document in README that the 2 s bound is checked between chunks, not enforced per syscall.

#### SEC-007: Stale state hides pending security-update counts with no escalation
**Severity:** LOW. **Domain:** Data exposure / security visibility. **Location:** FR-034, FR-035, AD-010 (fleet
aggregates exclude stale rows).
**Description:** While a host is `stale`, its rows lose their pending and security counts, and `totalSecurity`
excludes it. A host can stay stale for a long time, for example with `apt-daily` disabled and no patch runs. Its
security count is then invisible on every surface, and no signal escalates. A device-key holder can already hide
counts by reporting `pending_updates=0`, so there is no new adversarial vector. The concern is visibility after a
fault.
**Recommendation (Ryan's product call; not a condition):**
- Keep the raw counts discoverable as secondary text or a tooltip on the stale row and on the device page.
- Or raise a note once a host has been stale for more than a day. FR-034 forbids a count on the Patch button, not
  everywhere.

#### INFO-1: Agent and helper filesystem reads: symlinks and TOCTOU are acceptable
- `/etc/apt`, `/var/lib/apt/lists`, `/boot`, `/run/reboot-required` and `/proc` are writable only by root. An
  unprivileged local user cannot forge `reboot_required` or freshness.
- The kernel check uses file **names** only, and opens only `osrelease`, with a 256-byte cap. A symlinked `vmlinuz-*`
  cannot leak anything.
- The freshness `os.Stat` follows symlinks but reads no content. Its result is display-only (FR-039).
- The helper fingerprint only stats files, and its result feeds a single bounded decision ("refresh once more").
  Errors fail toward one extra allowlisted `apt-get update`.
- `post_refresh` is a free-form `phase` value. `Record.Validate` accepts it, so old and new readers stay compatible.

#### INFO-2: Lenient telemetry decoding and UI rendering are sound
- `Telemetry` decodes into `models.TelemetrySnapshot` and stores the **re-marshalled** struct. So `lists_freshness`
  is stored already normalised (`fresh|stale|unknown`), and the capability is stored as a bool. A hostile value cannot
  grow storage or reach the UI raw.
- The UI still normalises again (`normalizeFreshness`, `=== true`), which is correct for older stored rows.
- No new HTML sink appears. QA must confirm that `PatchConfirmOptions.tsx`, `patchFlow.ts` and the `DeviceDetail` and
  `FleetOverview` changes add no `dangerouslySetInnerHTML` or `innerHTML` (UI README SEC-005).
- The kernel reason string passes the charset filter and is limited to 160 bytes. It also flows into the
  `reboot_required` event message (`generator.go:686`), which goes through `createEvent`'s existing handling.

#### INFO-3: Widened `reboot_required` cannot cause a reboot loop
A host pinned to an older kernel reports `reboot_required=true` permanently. Nothing automated keys a reboot on
`reboot_required`: `checkAutoPatch` and `maybeRebootAfterPatch` key on applied reboot-class packages, and the server
only emits an event. An authorized run reboots at most once per command, and nothing re-dispatches on its own. This
property is load-bearing. AC-022 and the DoD `execReboot` grep lock it, and they must stay (C-7).

#### INFO-4: Reboot while a third-party dpkg transaction runs (pre-existing)
`execReboot` runs `sudo systemctl reboot` as root, without a TTY. That bypasses the logind inhibitor check, apart from
delay locks. If `unattended-upgrades` or an admin's `apt` is mid-transaction when phase B fires, the host can come
back with interrupted dpkg state, the failure class the `updates` collector already cannot self-heal (project memory).
All three reboot paths share this, so it is out of scope. A follow-up story should make every rIOt-initiated reboot
defer while the dpkg frontend lock is held.

#### INFO-5: Accepted residuals
- **R-1 (TOCTOU between the server check and execution).** Closed by the agent veto, re-checked in phase A and
  immediately before `execReboot`.
- **R-4 (reboot with the result still queued).** The result is durable in the outbox and is delivered after boot.
  Acceptable.
- **Agent killed during the 30 s wait** (after `inflight.Remove`). There is no marker, so recovery does not replay the
  reboot. The claim is consumed and the record says "started". This is the safe direction, and it is covered by
  SEC-002 (b) in spirit.
- **Clock jumps on hosts without an RTC.** These affect intent pruning exactly as they affect the seen-ID store. With
  TLS, an agent with a 1970 clock cannot connect, so it cannot receive commands. Acceptable.
- **Rejected authorization attempts (400) are not logged** (ADD §9). That is acceptable for an admin-only route. An
  `slog.Info` naming the code and the device would help diagnosis, but it is optional.
- **Updates collector disabled.** `Updates` is then nil, and the server answers
  `reboot_authorization_unsupported` ("agent must be updated"). The message misleads but the outcome is safe. The
  technical writer should mention it.

---

## Assessment of the specific questions

| Question | Assessment |
|---|---|
| Who can send `reboot_if_required` | Admin JWT only, behind the same-origin and JSON-body checks (`registerCommandRoutes`, `AdminAuthSameOrigin`). No device-key or viewer path reaches it. |
| Strict boolean validation | Correct. Only a Go `bool` passes. `null`, `"true"`, `1` and `{}` get 400 with no `Create` call (AC-015). `false` is deleted, so stored params match today's. |
| Client `include_reboot_class` stripped | Yes. The strip is first and unconditional. Exactly two setter sites remain (DoD grep). Case variants: SEC-003. |
| Capability trust | Narrow-only (NFR-003). A spoof can make the server accept a tick, but the real agent's `allow_reboot` is authoritative. An old or downgraded agent falls back to PATCH-GATE semantics (R-5). Acceptable. |
| CSRF / replay | CSRF is closed by the origin checks and the JSON requirement. A replay needs the session, and each replay is a new command ID, so at most one reboot each, the same as the Reboot button. |
| Audit trail | The stored params carry both flags, the actor is recorded by `dockerDispatchRequest`, the event carries the suffix, and the result carries the FR-023 facts. Falsity risk: SEC-002. |
| Automated paths unchanged | Yes. `checkAutoPatch` and `BulkPatchDevices` build their own params and never read freshness or `reboot_if_required`. `maybeRebootAfterPatch` keeps its rule. |
| At-most-once | Sound. The claim happens before the result is queued. The 72 h floor covers every redelivery and replay window: 70 min on the server, 48 h for the seen store, and the seen store's 1,000-entry eviction makes this floor the real guarantee. The clock-backwards guard is correct. The cap evicts only entries older than 72 h. |
| `allow_reboot` honoured | Checked in phase A and again immediately before `execReboot`. The server check is only UX. |
| Recovered results / failed runs | Never reboot. Recovered results are suppressed explicitly, and any non-success result maps to `run_not_successful` before any claim, so no claim is consumed. |
| Result before reboot | `out.Add` comes first. A failed `out.Add` returns before `postResult`, so there is no reboot. The ack wait is bounded at 30 s. Acceptable. |
| Host lock / helper race | The phase B reservation blocks other host commands. Own-unit false busy: SEC-002. |
| Corrupt intents file (R-6) | Should self-heal, and can do so safely after 72 h with no writes (SEC-001). Resetting immediately would be unsafe. |
| Stale detection: symlinks, TOCTOU, bounds, filenames | Root-only inputs, display-only output, names only from `/boot`. Bounds: SEC-006. Comparator robustness: SEC-005. |
| Helper change | No new argv (the exact allowlisted refresh), no protocol change, sudoers unchanged. The time budget is safe: the extra refresh is skipped below 7 min of budget left, and a SIGTERM still yields `interrupted`, which is correct. The fingerprint is stat-only. `TouchLists` no-follow: SEC-004. |
| Server lenient decode / UI | Normalised at ingest and stored normalised. No HTML sink. Bounded (INFO-2). |
| Bounded growth | Every new path has a stated bound. The exceptions are the permanent fail-closed store and its unbounded read (SEC-001) and unchunked directory reads (SEC-006). |

---

## Positive Observations
- A boundary-only release path. The tick is the only widening input, the server derives `include_reboot_class`, and
  telemetry can only narrow it. This is the right shape for amending BR-005.
- Two distinct rejection codes, and no silent downgrade (OQ-006). The operator always learns why a tick was refused.
- Claiming before the result is queued makes FR-023's facts true when they are sent, and makes the decision
  at-most-once even after the seen store is lost.
- The 72 h floor is derived from the actual redelivery windows, with a margin. The cap never evicts inside it.
- The legacy v1 load is conservative: entries get `now` as their claim time and are never pruned early.
- The helper protocol is unchanged (AD-008). The extra refresh runs through `Runner.Run` with the exact allowlisted
  argv, so `Interrupted` can never contaminate a success record.
- The kernel check and freshness checks spawn no processes, create no files, use name-only `/boot` parsing with a
  charset filter, and are display-only (FR-039).
- `allow_reboot` is re-checked immediately before `execReboot`, and the DoD greps lock the call sites.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.** No CRITICAL or HIGH finding.
- The amended hold-release path is admin-only and CSRF-protected.
- It is validated strictly at the API boundary, narrowed (never widened) by agent facts, and vetoed by the agent.
- A stolen admin session gains nothing it lacked before.
- The two MEDIUM findings concern failure-mode truth and self-heal in AD-012 and phase B. Both fail in the safe
  direction (no reboot), and both can be fixed inside the existing design without an architectural change.

### Conditions (engineers implement; QA verifies)
1. **C-1 (SEC-001).**
   - A corrupt, unparseable or oversized intent store fails closed only while it could hold an intent younger than
     72 h, judged from the store's own last-write evidence (a future or unreadable time counts as young).
   - After that, it self-heals: one bounded set-aside copy, then a fresh v2 store.
   - The read size is bounded.
   - Tests as listed in SEC-001 (e).
2. **C-2 (SEC-001 d).** A post-patch reboot suppressed for `claimPersistFailed` or `claimStoreFull` emits one agent
   warning event per command, on both the authorized and the automated path.
3. **C-3 (SEC-002).**
   - Phase B, and `maybeRebootAfterPatch`, must not treat this command's own just-finished helper unit (`active` or
     `deactivating` after its terminal record) as busy.
   - Every abort after `reboot started: yes` emits the warning event naming the command and the reason.
   - Tests as listed in SEC-002 (c).
4. **C-4 (SEC-003).** No case variant of `include_reboot_class` survives the manual `os_update` boundary. A case
   variant of `reboot_if_required` gets 400 `invalid_request`. Add a test.
5. **C-5 (SEC-004).** `TouchLists` updates timestamps without following a final-component symlink, on the object
   verified as a root-owned real directory. On doubt it skips with the bounded note.
6. **C-6 (SEC-005).** `compareDebVersion` is total, never panics and cannot overflow. Add the fuzz test plus table
   cases with digit runs longer than 19 digits.
7. **C-7 (SEC-006).** Every new directory read (`/boot`, `/var/lib/apt/lists`, `/etc/apt/*`, both in the agent and the
   helper) is chunked and stops at its cap.
8. **C-8 (QA gate).** The security-relevant tests named in ADD §8 exist and pass:
   - AC-014, AC-015 and AC-016, each asserting no `Create` call;
   - AC-020, AC-021, AC-022 and the `PF_Bound` rows;
   - the DoD greps: exactly two `include_reboot_class = true` sites, and three `execReboot` call sites, each preceded
     by an `AllowReboot` check;
   - no `dangerouslySetInnerHTML` or `innerHTML` in the new or changed web files.

### For Ryan's decision (not blocking)
- SEC-007: keep raw security counts discoverable for stale hosts, or escalate after prolonged staleness.
- INFO-4: open a follow-up to make all rIOt reboots defer while the dpkg frontend lock is held.
