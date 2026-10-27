# Formal Requirements Document

- **Story ID:** EXPECT-008
- **Title:** Installer re-run as expected activity: re-running `install.sh` as root on a registered device does not page "device offline", the fleet and device views show "Reinstalling agent by installer", and a device that does not come back within 10 minutes pages once
- **Author:** Business Developer Agent
- **Date:** 2026-10-27
- **Revision:** 1
- **Status:** DRAFT. Ryan was not available for an interview. Every clarification is settled by a default set on Ryan's behalf (Section 10 and Section 11.1). Section 11.2 lists the decisions that need Ryan's call (DEC-1 to DEC-8), each with a recommendation; the FRD is written to the recommendation. Ryan reviews them in the PR.
- **Epic:** EXPECT (expected activity). Siblings: `docs/requirements/EXPECT-001-frd.md` (windows, escalation, labels), `docs/requirements/EXPECT-002-frd.md` (agent self-update records; this FRD mirrors its structure and bounds).

---

## 1. Executive Summary

Re-running `scripts/install.sh` as root on a device that is already registered stops `riot-agent`, replaces the binary and starts it again. The server sees a heartbeat gap and raises a warning `device_offline`, which pushes to Ryan's phone. On 2026-10-27 at 13:15 UTC, PiCUPS and PiVPN each paged "Device X went offline" during an installer re-run Ryan had been asked to do (2.82.0 → 2.83.0, no reboot). Dashboard-started actions (EXPECT-001) and agent self-updates (EXPECT-002) are already silent; an installer re-run sends the server no signal at all. This story makes an installer re-run on a registered device open a short **reinstall window** on the server, bounded at 10 minutes from the notice. Inside it, "device offline" is stored as an info event naming "Agent reinstall by installer" and is not notified, and the fleet and device views show "Reinstalling agent". The window ends when the agent registers again or at the bound. A device still offline at the bound pages exactly once, as in EXPECT-002. The notice is best effort: the installer never waits more than 10 seconds for it, never fails because of it, and never exposes the device key. Fresh installs and bootstrap-key re-enrollments open no window. Each device can open at most 6 reinstall windows per 24 hours.

## 2. Background & Context

### 2.1 Evidence (2026-10-27, 13:15 UTC, PiCUPS and PiVPN, 2.82.0 → 2.83.0)

| Step | What the server received | What Ryan got |
|------|--------------------------|---------------|
| 1 | Nothing (the installer contacts GitHub, not the rIOt server) | Nothing |
| 2 | Heartbeats stop when the installer stops `riot-agent`; more than 45 s later the device is marked offline (`device_offline`, warning) | **A push via ntfy** |
| 3 | The new agent registers on 2.83.0 (`device_online`) | Nothing |

No command row, no agent event, no reboot (uptime unchanged).

### 2.2 What exists today (verified at 8a16f2a, v2.83.0 plus PR #44)

- **Installer** (`scripts/install.sh`, bash, run as `curl ... | sudo bash -s -- [URL]`). It is fetched from the `main` branch on GitHub, so a merged installer change reaches a device at its next re-run without a release tag. A re-run on a registered device is the path where `/etc/riot/agent.yaml` already exists; the URL argument is then optional.
  - Order: host lock, signature and candidate validation, backup, sudoers validation, then `systemctl stop riot-agent` (about line 991), then config and file replacement, then `systemctl start riot-agent` (about line 1192).
  - A failure after the stop can leave the agent stopped ("the agent is left stopped; re-run the installer").
  - `--bootstrap-key` together with an existing config deletes that config and enrolls again.
  - The existing YAML readers in the installer use `awk` and must work on `mawk` (Debian slim) as well as `gawk`.
- **agent.yaml** holds `server.url`, `server.api_key` (the device key), `server.tls_verify`, `server.ca_cert_file`, mTLS client certificate and key, `server.server_cert_pin` (TOFU SHA-256) and `agent.device_id`.
- **Agent event route:** `POST /api/v1/devices/{id}/events`, device-key authenticated (`X-rIOt-Key`), type validated against a known list. EXPECT-002 hooks `agent_update_*` events there.
- **EXPECT-002** (shipped v2.82.0, schema 34): a self-update record is a `commands` row with action `agent_update` and origin `agent_self_update`, created `running`, never queued; one open per device; 10-minute bound; cap 6 per device per 24 h plus one "paused" warning event; stuck-record warning; escalation through the `device_offline` rule once per record. Label "Updating agent <from> → <to>", actor phrase "by agent auto-update".
- **Agent stop:** on SIGTERM the agent (≤ 2.83.0) sends nothing to the server.

### 2.3 Gap and flow

| # | Flow (EXPECT-001 Section 2.4 numbering) | Covered by |
|---|------|------------|
| F14 | Fleet / single `agent_update` command | EXPECT-001, unchanged |
| F15 | Agent self-update | EXPECT-002, unchanged |
| F16 | AGENT-PRIV one-click apply / `riot-agent-apply` | EXPECT-007, unchanged |
| **F23** | **`install.sh` re-run as root on a registered device (stop, replace, start)** | **This story** |
| F22 | Restarts outside rIOt (ssh `systemctl restart`, crash, reboot) | Not expected activity, unchanged |

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Ryan (operator) | Runs the installer as root on a device; receives notifications; reads the fleet, device, events and command history views. | Root on the device (unchanged). Existing dashboard API (unchanged). |
| Installer (`install.sh`) | Root script that stops, replaces and starts the agent. Actor phrase "by installer". | Reads the existing `agent.yaml` as root. Gains no new privilege on the device or the server. |
| rIOt Agent (any deployed version) | Heartbeats, registers, sends events. | Unchanged. |
| rIOt Server | Accepts the reinstall notice, keeps reinstall records and windows, decides whether an offline transition is expected, escalates, serves the dashboard. | Gains the rules of this story. All notice content is untrusted input. |
| Notification channels | Existing dispatcher and channels. | Unchanged. |

## 4. Functional Requirements

### Definitions

- **Registered device:** a device whose existing `agent.yaml` holds a device ID and a device key, and that the server knows under that ID.
- **Reinstall notice ("notice"):** the signal, whatever its mechanism (Section 13), that tells the server "the installer is about to stop and replace the agent on this device".
- **Reinstall record ("record"):** the server's durable record of one accepted notice: device, start time (server receive time of the notice), originator "installer", outcome and outcome time.
- **Reinstall window ("window"):** the EXPECT-001 expected-activity window belonging to a record, agent-restart scope.
- **Bound:** 10 minutes after the record's start time.
- **Action label:** "Agent reinstall". **Activity label:** "Reinstalling agent". **Actor phrase:** "by installer" (DEC-3).

### A. Installer side

- **FR-001:** On a re-run on a registered device, the installer must attempt exactly one notice, after every validation step that can abort the run without stopping the agent, and before the agent is stopped.
- **FR-002:** The installer must not attempt a notice on a fresh install (no existing `agent.yaml`), on a run with `--bootstrap-key`, when the existing config lacks a device ID or a device key, or when the agent service is not loaded or not running at that point.
- **FR-003:** The notice must reach the server only through the same server URL and the same trust settings the agent uses (`tls_verify`, CA file, mTLS client certificate, certificate pin). When the installer cannot apply one of these settings exactly as the agent does, it must not send the notice (NFR-006).
- **FR-004:** The notice attempt must end, successfully or not, within 10 seconds of starting, under any network condition (server down, DNS failure, black-holed packets, TLS failure, slow response) (DEC-7).
- **FR-005:** The outcome of the notice attempt must never change the installer's exit status, never abort, pause or prompt, and never change any later step of the run.
- **FR-006:** The installer must print exactly one line about the notice: "==> Server notified: agent reinstall (offline alerts paused up to 10m)" when the server accepted it, or "==> Server not notified (<short reason>); a device-offline alert may follow" otherwise (DEC-5). The reason must be one of a fixed set of short phrases and must not contain server response bodies, URLs with credentials, or any part of the device key.

### B. Server side: accepting a notice

- **FR-007:** The server must accept a notice only when it is authenticated with the current device key of the device it names. A notice with a missing, wrong or revoked key, or naming another device, must be rejected, must create no record and must change no alerting.
- **FR-008:** An accepted notice must create a record when all of the following hold:
  - (a) the device is currently online (DEC-4);
  - (b) the device has no open reinstall record (FR-009);
  - (c) the device is under the rate cap (FR-010).

  Otherwise it must create no record, and alerting must behave exactly as today.
- **FR-009:** A device must have at most one open reinstall record. A notice received while one is open must not create a record and must not extend or reopen the window.
- **FR-010:** The server must create at most 6 reinstall records per device in any rolling 24 hours (DEC-1). The first notice refused by the cap in a 24-hour period must produce one `warning` event for the device, with no notification: "Agent reinstall tracking paused on <host>: more than 6 reinstalls in 24h; offline alerts during reinstalls are not suppressed". At most one such event per device per rolling 24 hours.
- **FR-011:** A record must never be sent to, claimed by or acted on by an agent, and must offer no cancel, retry or other action in the dashboard.
- **FR-012:** Any text, version or other field in a notice beyond the authenticated device identity must not be required for any decision in this story, and any such field that is shown to Ryan must follow the EXPECT-002 untrusted-text rules (printable valid UTF-8, no control or format characters, at most 200 bytes).

### C. The window

- **FR-013:** The window must open when the record is created.
- **FR-014:** The window must cover `device_offline` for the record's device only. Container rules and every other rule must alert exactly as without a window.
- **FR-015:** The window must close at the earliest of (a) a registration of the device received after the record's start time, or (b) the bound. No window may stay open after its bound, even if nothing more ever arrives from the device.
- **FR-016:** When the window closes, normal alerting resumes at once. A later offline transition alerts exactly as with no window.
- **FR-017:** An offline transition that begins inside the window must be stored as a `device_offline` event with severity `info`, linked to the record, and must never be notified. Message: "Device <host> went offline during Agent reinstall by installer (expected, back by <HH:MM>)".
- **FR-018:** When the device comes online after an offline period that began inside the window, the `device_online` event must name the action and say how long the device was offline: "Device <host> came online after Agent reinstall by installer (offline 52s)".
- **FR-019:** When a reinstall window and another agent-restart window (EXPECT-001 `agent_update` command, EXPECT-002 self-update) apply to the same device, each must be evaluated on its own, and the device must show one label, the one with the latest start (DEC-8).

### D. Outcome and alerting

- **FR-020:** A record's outcome must be set exactly once: `succeeded` when the window closes on a registration; otherwise at the bound, `not confirmed`.
- **FR-021:** When the bound is reached without a registration and the device is offline, the server must raise exactly one escalation within 60 s of the bound, through the existing `device_offline` rule, exempt from unrelated cooldowns, at most once per record including across server restarts, and under the existing per-device push ceiling (EXPECT-001 AD-010). Message: "<host> still offline 10m after Agent reinstall by installer (record <id8>)". The device must show the EXPECT-001 escalated state ("Not back 10m after Agent reinstall", failure styling) until it next comes online or for 24 hours, whichever is first.
- **FR-022:** When the bound is reached without a registration and the device is online, the server must store exactly one `warning` event linked to the record, with no notification: "Agent reinstall by installer on <host> was not confirmed within 10m (agent did not restart)" (DEC-2).
- **FR-023:** A record must produce at most one outcome event (FR-022) and at most one escalation (FR-021).

### E. Dashboard

- **FR-024:** While a device has an open window, the Fleet Overview and the device page header must show "Reinstalling agent", the actor phrase "by installer" and the start time, whether the device is online or offline. The label must clear within 5 s of the window closing, come from the server, appear in any browser and after a reload.
- **FR-025:** A device offline inside an open window must be counted under "Expected offline", not in the offline count, and must still be visibly marked offline. An escalated device counts as offline.
- **FR-026:** The record must appear read-only in the device's command history with the action label, actor phrase, start time, outcome and linked events, and the device page label must link to it (DEC-6).

### F. Unchanged behaviour, rollout and documentation

- **FR-027:** EXPECT-001 and EXPECT-002 windows, records, caps, labels and escalations must not change.
- **FR-028:** A new installer run against a server without this story, and an old installer run against a server with this story, must each complete exactly as today (same exit status, same installed result), with today's alerting.
- **FR-029:** The ADD and the PR body must each state, as a numbered list for Ryan, what Ryan has to do after merge: whether a release tag and a server deploy on hawaii are needed, whether any device needs a new agent version or an installer re-run before the behaviour applies, the required order (for example server before installer), and which re-run is the first one expected to be silent.
- **FR-030:** The README expected-activity section must describe installer re-runs: when a window opens and when it does not (FR-002, FR-008), the 10-minute bound, offline-only suppression, how the window ends, the escalation and outcome event, the rate cap, the label, the 10-second best-effort notice and its installer output line, and the residuals in NFR-010. The CHANGELOG entry must state the FR-029 rollout steps.

## 5. Non-Functional Requirements

- **NFR-001 [Safety, hard rule]:** No test, helper, fixture or script for this story may run a real reboot, shutdown, poweroff, package manager (`apt`, `dpkg`) operation or agent update on any machine, or execute an installed `riot-agent` binary. Installer tests must replace `systemctl`, the network client and every other system command with fakes. Server tests use an injected clock and test doubles.
- **NFR-002 [Installer portability]:** Every installer change must pass the installer fixture tests on a `debian:*-slim` image (where `awk` is `mawk`) as well as on the existing `gawk` fixture, in CI.
- **NFR-003 [Secret handling]:** The device key must never appear in the argument list of any process (as visible to other users through `ps` or `/proc/<pid>/cmdline`), in installer stdout or stderr, in the installer's own log or trace output, or in any file other than the existing `agent.yaml` (and the existing root-only installer backup) that remains after the installer exits. Any transient file holding it must be readable by root only.
- **NFR-004 [Bounded window, self-healing]:** Every reinstall window closes by itself at most 10 minutes after its start. Every outcome is set, and any escalation raised, within 10 minutes plus 60 s of the start (plus any EXPECT-001 server restart grace in force). A record still unresolved 1 hour after its bound must be retired, produce one `warning` event without notification, and must not block new records (as EXPECT-002 C-4).
- **NFR-005 [Bounded growth]:**
  - Records: at most 1 open per device, at most 6 created per device per rolling 24 hours, so at most 540 per device over the 90-day command retention; removed by the same retention as command records.
  - Events per record: at most one suppressed-offline info event per offline transition (each replaces today's warning one for one), one online event, one outcome event (FR-022) or one escalation (FR-021), and one stuck-record warning (NFR-004). Per device: at most one rate-cap event per rolling 24 hours.
  - Rejected and refused notices (FR-007, FR-008) store no record and no event.
  - Server memory: at most one open record per device plus at most 6 timestamps for the cap; released within 5 minutes of the outcome.
  - Logs: no line above debug per accepted, refused or rejected notice or per suppressed transition; at most one info line per escalation; repeated errors go through the existing log budget.
  - Installer: the notice leaves no file, directory or log behind on the device beyond its one output line.
- **NFR-006 [Fail open to alerting]:** If the installer cannot read the config, cannot honour the agent's trust settings, or cannot reach the server, and if the server cannot read or write records or decide coverage, the offline transition must alert exactly as today. Nothing may be silently dropped or downgraded.
- **NFR-007 [Abuse bound]:** A holder of a device's key, or anyone replaying a captured notice, can delay that device's offline alert by at most 10 minutes per record and at most 60 minutes per rolling 24 hours (6 records; at most 120 minutes combined with EXPECT-002's separate budget, DEC-1). Every covered transition is still stored as an info event, and a device offline at the bound still escalates. A notice can never affect another device.
- **NFR-008 [Performance]:** The notice adds at most 10 s to an installer run (FR-004). Handling a notice adds at most 100 ms at the 95th percentile to server processing at Ryan's scale (4 devices). Labels change within 5 s; escalations fire within 60 s of becoming due.
- **NFR-009 [Durability]:** All decisions must be derivable from durable records after a server restart. A restart inside a window must not turn a covered transition into an alert, lose an escalation that became due during downtime (it must fire within 2 minutes after start), or send an escalation or outcome event twice.
- **NFR-010 [Accepted residuals]:**
  - A real outage that coincides with a reinstall is hidden for up to 10 minutes, then escalates once.
  - A run whose notice fails (server down, network, trust mismatch) pages as today.
  - A run that aborts after the notice and before the stop leaves the device online; the window stays open until the bound (at most 10 minutes) and ends `not confirmed` with one warning event and no push.
  - A run that aborts after the stop leaves the agent down; the device escalates once at the bound.
  - Time spent at any interactive prompt that runs after the agent is stopped counts against the 10-minute bound; a run left waiting longer pages once.
- **NFR-011 [Testability]:** Every FR must be verifiable by automated tests without real elapsed time and without a real network.
- **NFR-012 [No regression]:** EXPECT-001 and EXPECT-002 acceptance tests and the existing installer fixture tests must pass unchanged.

## 6. Business Rules

- **BR-001:** An installer re-run Ryan performs on a registered device is expected activity. Its restart must not notify Ryan.
- **BR-002:** Nothing is dropped. Every suppressed offline transition is kept as an info event naming "Agent reinstall by installer".
- **BR-003:** A reinstall suppresses only "device offline" for its own device, for at most 10 minutes.
- **BR-004:** A device not back at the bound alerts exactly once and names the reinstall.
- **BR-005:** No trusted identity, no window: fresh installs and bootstrap-key enrollments never open one.
- **BR-006:** The notice is a courtesy. The installer's job (replace and start the agent) never depends on it, and the device key never leaves the device except to the server the agent already trusts, the way the agent sends it.
- **BR-007:** When in doubt, alert.

## 7. Data Requirements

### 7.1 Entities

| Entity | New or existing | Used for |
|--------|-----------------|----------|
| Reinstall record | New | Device, start time, originator "installer", outcome (`succeeded`, `not confirmed`, or not yet set), outcome time. Bounded by NFR-005. The architect decides its form; it must reuse EXPECT-001 windows, escalation markers, labels and retention protection (no parallel window system), as EXPECT-002 did. |
| Event (server-generated) | Existing | Suppressed offline, online, outcome, escalation, rate-cap and stuck-record events, linked to the record. |
| Device | Existing | Online status, last heartbeat, registrations. |
| agent.yaml | Existing, read only | Device ID, device key, server URL and trust settings. Never modified by the notice. |

### 7.2 Validation

- Notice identity: authenticated device key matching the named device (FR-007). Nothing else in the notice is trusted (FR-012).
- Installer-side preconditions: FR-002.

### 7.3 Record and window states

| From | Event | To | Effect |
|------|-------|----|--------|
| (none) | Authenticated notice, device online, no open record, under cap | Open | Window covers `device_offline`; label "Reinstalling agent". |
| (none) | Rejected, device offline, open record exists, or over cap | (none) | Today's behaviour (FR-007 to FR-010). |
| Open | Device goes offline | Open | Info event, linked, no notification (FR-017). |
| Open | Registration of the device | Closed, `succeeded` | Online event names the reinstall when it ended an offline period; label clears. |
| Open | Bound, device offline | Escalated, `not confirmed` | One escalation (FR-021). |
| Open | Bound, device online | Closed, `not confirmed` | One warning event, no push (FR-022). |
| Escalated | Device online, or 24 h | Closed | Failure label cleared. |
| Any open | 1 h after bound, unresolved | Retired | One warning event, no push (NFR-004). |
| Any | Server restart | Same | Rebuilt from durable records (NFR-009). |

## 8. Acceptance Criteria

### Installer

**AC-001: Re-run on a registered device sends one notice before the stop** [FR-001, FR-005, FR-006]
- Given a fixture device with an existing `agent.yaml` (device ID, key, URL), an active fake `riot-agent` service, and a fake server that accepts notices
- When the installer runs to completion
- Then exactly one notice reaches the fake server, after the last validation step and before the fake `systemctl stop riot-agent` is called
- And the installer prints "==> Server notified: agent reinstall (offline alerts paused up to 10m)" once, and exits as it does today

**AC-002: No notice without a trusted identity or a running agent** [FR-002, BR-005]
- Given, in turn: no existing `agent.yaml`; an existing `agent.yaml` with `--bootstrap-key`; an `agent.yaml` without `device_id`; one without `api_key`; an inactive `riot-agent` service
- When the installer runs
- Then no notice is sent in any case, and the installer behaves exactly as today

**AC-003: Abort before the stop sends no notice** [FR-001]
- Given a registered fixture device and a candidate that fails signature validation (or unit validation, or sudoers validation)
- When the installer runs and aborts
- Then no notice was sent and the agent was never stopped

**AC-004: Server unreachable never blocks or fails the install** [FR-004, FR-005, FR-006, NFR-008]
- Given, in turn, a fake server that refuses connections, one that accepts the connection and never answers, one that answers 500, one that answers 404, and an unresolvable host name
- When the installer runs
- Then in each case the notice attempt ends within 10 s, the installer prints one "==> Server not notified (<short reason>); a device-offline alert may follow" line, completes every later step, and exits with the same status as today

**AC-005: Trust settings honoured or notice skipped** [FR-003, NFR-006]
- Given a config with `tls_verify: true` and a fake server presenting an untrusted certificate; a config with a `server_cert_pin` that does not match; and a config with mTLS client certificate and key
- When the installer runs
- Then no key is sent to the untrusted or pin-mismatched server (the install completes, one "not notified" line); and the mTLS case either presents the configured client certificate or sends no notice

**AC-006: Device key never exposed** [NFR-003]
- Given a registered fixture device whose key is a unique marker string, with every process start recorded by the fixture
- When the installer runs, once with the server accepting and once with it unreachable
- Then the marker appears in no recorded process argument list, not in installer stdout or stderr (including any shell trace output), and in no file outside `/etc/riot` and the installer backup after exit

**AC-007: mawk and gawk** [NFR-002]
- Given the installer fixture tests for this story
- When CI runs them on a `debian:*-slim` image (mawk) and on the existing fixture image (gawk)
- Then all pass on both

### Server: suppression and outcomes

**AC-008: The 2026-10-27 timeline produces no notification** [FR-007, FR-008, FR-013, FR-015, FR-017, FR-018, FR-020, FR-024]
- Given PiVPN online on 2.82.0 and an enabled `device_offline` rule with notify on
- When an authenticated notice arrives at 13:15:00, heartbeats stop at 13:15:05, PiVPN is marked offline at 13:15:50, and PiVPN registers on 2.83.0 at 13:16:10
- Then one record exists for PiVPN, start 13:15:00, outcome `succeeded`
- And the offline event is `info`, linked, reading "Device PiVPN went offline during Agent reinstall by installer (expected, back by 13:25)"
- And the online event reads "Device PiVPN came online after Agent reinstall by installer (offline 20s)"
- And no notification is sent, and the fleet and device page showed "Reinstalling agent", "by installer" and 13:15 until the registration

**AC-009: Two devices re-run back to back** [FR-008, BR-001]
- Given PiCUPS and PiVPN each send a notice, go offline and register again within 3 minutes
- Then two records exist, both `succeeded`, two info offline events exist, and zero notifications were sent

**AC-010: Container deaths still alert** [FR-014, BR-003]
- Given an open window for PiCUPS
- When a container on PiCUPS dies
- Then `container_died` alerts and notifies exactly as with no window

**AC-011: Offline after close alerts** [FR-015, FR-016]
- Given a record for PiVPN closed by registration at 13:16:10
- When PiVPN goes offline at 13:20
- Then `device_offline` alerts and notifies normally

**AC-012: Not back within 10 minutes escalates once** [FR-021, FR-023, NFR-004]
- Given a record for PiVPN started at 13:15:00 and PiVPN offline since 13:15:50
- When the clock reaches 13:25:00 with PiVPN still offline
- Then exactly one escalation is raised by 13:26:00 through the `device_offline` rule, reading "PiVPN still offline 10m after Agent reinstall by installer (record <id8>)", and it notifies
- And PiVPN shows "Not back 10m after Agent reinstall" with failure styling and counts as offline
- And no second escalation is raised, including after a server restart, and it still fires when an unrelated cooldown on the rule started 60 s earlier

**AC-013: Notice but the agent never stopped** [FR-020, FR-022, DEC-2, NFR-010]
- Given a record for PiCUPS started at 13:15:00, and PiCUPS keeps heartbeating and never registers
- When the clock reaches 13:25:00
- Then the window is closed, the outcome is `not confirmed`, exactly one `warning` event reads "Agent reinstall by installer on PiCUPS was not confirmed within 10m (agent did not restart)", no notification is sent, and a later offline transition alerts normally

### Server: bounds and untrusted input

**AC-014: Rejected notices change nothing** [FR-007, NFR-007]
- Given notices with no key, a wrong key, a revoked key, and PiCUPS's valid key naming PiVPN
- When each is received and the named device then goes offline
- Then no record and no event is created by the notice, and `device_offline` alerts and notifies

**AC-015: Device already offline** [FR-008, DEC-4]
- Given PiVPN is marked offline
- When an authenticated notice for PiVPN arrives
- Then no record is created and alerting is unchanged

**AC-016: Repeated and replayed notices are bounded** [FR-009, FR-010, NFR-005, NFR-007]
- Given PiVPN sends 50 identical notices within 1 minute
- Then exactly one record exists, the window is not extended past 10 minutes after the first notice, no event is stored for the repeats, and no log line above debug is written for them
- And given 10 notices over 20 hours, each after the previous record closed
- Then 6 records exist, notices 7 to 10 create no record, exactly one rate-cap warning event exists with no notification, and offline transitions after notices 7 to 10 alert normally

**AC-017: Untrusted notice fields** [FR-012]
- Given a notice carrying extra text fields of 10 KB with control characters, bidi overrides and invalid UTF-8
- Then any such text shown to Ryan is printable valid UTF-8 of at most 200 bytes, and the record's behaviour is identical to a notice without those fields

**AC-018: Fail open** [NFR-006]
- Given the record store cannot be read or written
- When an authenticated notice arrives and the device goes offline
- Then `device_offline` alerts and notifies

**AC-019: Server restart mid-window and bound during downtime** [NFR-009]
- Given a record for PiVPN started at 13:15:00, PiVPN offline since 13:15:50 (info event stored), and the server restarting at 13:16:30
- When PiVPN registers at 13:17:00
- Then no notification exists and the outcome is `succeeded`
- And given instead the server is down from 13:20 to 13:40 with PiVPN still offline, then exactly one escalation exists, raised within 2 minutes after start, also across a second restart

**AC-020: Stuck record retired** [NFR-004]
- Given a record whose outcome cannot be resolved
- When 1 hour passes after its bound
- Then it is retired with exactly one `warning` event and no notification, and a new notice for the device creates a record

**AC-021: Bounded growth** [NFR-005]
- Given the AC-008, AC-012 and AC-013 timelines
- Then AC-008 stored 1 record, 1 info offline event and 1 online event; AC-012 stored 1 record and 1 escalation; AC-013 stored 1 record and 1 warning event; the installer left no new file behind; no log line above debug was written except one info line for the AC-012 escalation; and within 5 minutes of each outcome no server memory is held for that record

### Dashboard, coexistence, compatibility, safety, docs

**AC-022: Label, count and history** [FR-024, FR-025, FR-026, FR-011]
- Given an open window for PiCUPS with PiCUPS offline
- When the Fleet Overview and the PiCUPS page are opened in a new browser
- Then both show "Reinstalling agent", "by installer" and the start time; PiCUPS is counted under "Expected offline" and its status dot shows offline
- And the label links to the record in the command history, shown read-only with no cancel or retry control; the record is never returned when the agent polls commands
- And the label clears within 5 s of the window closing, with no reload

**AC-023: Reinstall during another agent-restart window** [FR-019, FR-027]
- Given an open EXPECT-002 self-update record for PiVPN opened at 13:10:00
- When an authenticated reinstall notice arrives at 13:12:00 and PiVPN goes offline at 13:12:50
- Then a reinstall record is created, PiVPN shows "Reinstalling agent", the offline transition is suppressed and linked to one of the two records, and each record reaches its own outcome by its own bound

**AC-024: Mixed versions of installer and server** [FR-028]
- Given a new installer against a fake server that returns 404 for the notice, and the pre-story installer against the new server
- When each runs
- Then each completes with today's exit status and installed result; the first prints one "not notified" line; the server creates no record in the second case

**AC-025: EXPECT-001 and EXPECT-002 unchanged** [FR-027, NFR-012]
- When the EXPECT-001, EXPECT-002 and existing installer fixture tests run after this story
- Then they pass unchanged

**AC-026: No real power, package or agent execution in tests** [NFR-001, NFR-011]
- Given the tests, helpers, fixtures and scripts added or changed by this story
- When they are inspected and run
- Then none invokes reboot, shutdown, poweroff, `systemctl` power verbs, `apt`/`dpkg`, a real agent update, or an installed `riot-agent` binary; `systemctl` and the network client are fakes in installer tests, and server time is an injected clock

**AC-027: Rollout and documentation** [FR-029, FR-030]
- Given the ADD, the PR body, the README and the CHANGELOG
- Then the ADD and the PR body each list Ryan's post-merge steps (deploy, per-device action or none, order, first silent re-run); the README covers every item in FR-030; the CHANGELOG states the rollout steps

## 9. Out of Scope

- Agent restarts outside the installer: ssh `systemctl restart`, crashes, reboots (F22), and `riot-agent-apply` (F16, EXPECT-007).
- Installer first installs and bootstrap enrollments (BR-005).
- Any change to EXPECT-001 or EXPECT-002 bounds, caps or wording.
- Removing or reordering interactive prompts in `install.sh`.
- Notifications for a reinstall that ends `not confirmed` while online (EXPECT-004 territory).
- A dashboard control to start, extend or dismiss a reinstall window.

## 10. Assumptions

- **A-001:** All defaults here were set by the Business Developer Agent on Ryan's behalf on 2026-10-27 from Clod's grounding note. Ryan reviews them in the PR.
- **A-002:** An installer re-run normally restarts the agent well within 10 minutes (observed: about 1 minute on Raspberry Pis), unless Ryan leaves a prompt open.
- **A-003:** The new agent always registers on start (verified for EXPECT-002, A-005), so registration is a reliable "back" signal; a reinstall may legitimately install the same, a newer or an older version, so no version check is applied.
- **A-004:** The installer runs as root and can read `agent.yaml`, including the device key, as it already does today.
- **A-005:** A device marked offline when the notice arrives has nothing to suppress; its alert was already raised.

## 11. Open Questions and Decisions

### 11.1 Clarifications settled by default

- **OQ-001 Bound:** 10 minutes from the server's receipt of the notice, no settle period (FR-015).
- **OQ-002 Scope:** `device_offline` for the device only (FR-014).
- **OQ-003 Close:** first registration after the start, or the bound (FR-015).
- **OQ-004 Escalation:** once per record through the `device_offline` rule under the existing ceiling (FR-021).

### 11.2 Open decisions for Ryan (FRD written to each recommendation)

- **DEC-1 Daily budget.** (a) A separate cap of 6 reinstall records per device per 24 h; worst case with EXPECT-002 is 12 windows, 120 minutes of delayed offline alerts per device per day. (b) One budget of 6 shared with EXPECT-002 self-updates (60 minutes total), which changes EXPECT-002's cap wording. **Recommendation: (a)**; it leaves EXPECT-002 untouched and a debugging session of several re-runs on release day does not eat the self-update budget.
- **DEC-2 Notice sent but the agent never restarts (device online at the bound).** (a) One warning event, no push. (b) Silent close. (c) Push. **Recommendation: (a)**, consistent with EXPECT-002 FR-021.
- **DEC-3 Wording.** Activity label "Reinstalling agent", action label "Agent reinstall", actor phrase "by installer", no versions (the server learns the new version from the registration; versions from the installer would be untrusted extras). **Recommendation:** as written.
- **DEC-4 Notice for a device already offline.** (a) No window. (b) Open one anyway. **Recommendation: (a)**; the alert already fired and a window would only hide its recovery state.
- **DEC-5 Installer output.** One line on success, one on skip or failure with a short reason from a fixed set, never the server response body. **Recommendation:** as written in FR-006.
- **DEC-6 Command history.** Show the record read-only, as EXPECT-002 DEC-5. **Recommendation:** yes.
- **DEC-7 Notice time bound.** 10 seconds total for the attempt. **Recommendation:** 10 s; on a dead server the installer already waits on GitHub downloads far longer, and 10 s covers a slow Pi on Wi-Fi.
- **DEC-8 Reinstall during another agent-restart window.** (a) Open the reinstall record anyway and show the latest label. (b) Refuse while any agent-restart window is open. **Recommendation: (a)**; with (b), a self-update window that closes on its own registration would leave the installer's later restart uncovered.

### 11.3 Questions for the architect

- **Q-1:** Which notice mechanism (Section 13), and does it work for every agent already deployed (≤ 2.83.0) without an agent upgrade?
- **Q-2:** Can the installer honour every agent trust setting (TLS verify, CA file, mTLS, TOFU pin) with tools present on stock Debian, Raspberry Pi OS and DietPi images without adding a dependency? If not, FR-003 requires skipping the notice for that configuration; state which configurations are skipped.
- **Q-3:** Does any interactive prompt run between the stop and the start (for example the shutdown-grant agreement)? If so, state whether the notice can stay before the stop while respecting NFR-010, and how the README tells Ryan about the 10-minute budget.
- **Q-4:** Answer FR-029 explicitly, including what happens if the installer change reaches devices (it is served from `main`) before the server deploy.

## 12. Dependencies

- **D-001:** EXPECT-001 (windows, Tracker, escalation markers, push ceiling, restart grace, labels, retention protection).
- **D-002:** EXPECT-002 (self-update record pattern, cap and stuck-record handling, migration 000034).
- **D-003:** `scripts/install.sh` and its CI fixture (`scripts/test_install_maintenance.py`, "Signed installer and legacy migration fixtures" job), plus a `debian:*-slim` fixture run.
- **D-004:** Device-key authentication middleware (`X-rIOt-Key`) and the agent event route.
- **D-005:** Pipeline: architect, security-researcher, senior-dev, qa-engineer, technical-writer.

## 13. Notes for the architect (mechanism candidates, listed neutrally)

The ACs are written in terms of observable behaviour and must hold whichever mechanism is chosen.

- **(a) Installer posts an authenticated "agent reinstall started" notice** using the device ID, key and trust settings from the existing `/etc/riot/agent.yaml`, just before `systemctl stop riot-agent`. Works with every deployed agent; a server deploy plus the merged installer (served from `main`) is all that is needed. Must meet FR-003, FR-004 and NFR-003 (key off the command line; for example a header supplied from a root-only file or stdin rather than an argument) and NFR-002 (mawk).
- **(b) The old agent reports on SIGTERM.** Only helps once a device already runs an agent that sends it, so the next re-run on each device still pages; it cannot tell a reinstall from a reboot, an `apt` upgrade or a manual `systemctl stop`, so it either over-suppresses or needs an extra installer signal. Overlaps EXPECT-007 (agent-side activity reporting).
- **(c) Reuse the EXPECT-002 self-update record with a new origin** (for example `installer`). A storage choice, compatible with (a) or (b); it inherits the one-open-per-device index, the shape CHECK, the restricted actor and the stuck-record retirement, but the EXPECT-002 cap, attribution and wording must not change (FR-027) and DEC-1's separate budget must be preserved.
