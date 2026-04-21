# Security Review Report
**Story ID:** LOG-001
**Title:** Near-Real-Time Auth Failure Alerting
**Reviewer:** Security Researcher Agent
**Date:** 2026-04-20
**Verdict:** PASS WITH NOTES

---

## Threat Model Summary

### Assets
- **Alert signal integrity.** The `failed_logins_interval` metric is itself the
  asset. Its value is the trigger for operator-visible notifications whose
  whole purpose is to expose real auth-attack activity. False negatives let
  attackers slip past the operator; false positives create alert fatigue that
  effectively suppresses real attacks.
- **Notification-pipeline availability.** Per-channel notification fan-out
  (ntfy, telegram, discord, slack, smtp, gotify, pushover, webhook) is
  outbound from the rIOt server and subject to provider rate limits, SMTP
  throttling, etc. An attacker who drives many alert fires can degrade this
  pipeline for unrelated alerts.
- **Events table and event WebSocket stream.** One event row and one
  notification_log row per channel are written per firing. An attacker who
  causes repeated fires (within the constraints of cooldown) can contribute
  to DB growth and WS broadcast noise.

### Threat Actors
- **Authenticated low-privilege local user on a monitored Linux host.** Can
  write arbitrary text to the journal via `logger(1)` on any distro where
  the syslog socket is world-writable (the default on every systemd-based
  distro in the supported matrix).
- **Unauthenticated remote attacker driving real SSH/sudo failures.** Real
  failures are exactly what the feature is designed to surface — but they
  produce real resource cost.
- **Compromised rIOt agent.** Valid mTLS client cert + API key; can submit
  any `FailedLoginsInterval` value it wants for its own device only.
- **Insider with a valid non-root account on a monitored host.** Can
  restart the agent (via local DoS, not via sudoers — the installed sudoers
  rules do not permit arbitrary service control by `riot`) or otherwise
  trigger a restart window.

### Attack Surface Introduced
- **New input channel (indirect):** the `MESSAGE` string of every journal
  entry in the `--priority=0..6` slice read by `LogsCollector`. Journal
  messages are attacker-influencible from both the network (real sshd logs
  include attacker-supplied usernames/IPs) and local-user land (logger(1),
  compromised services, custom PAM modules).
- **New wire field:** `security.failed_logins_interval` on
  `POST /api/v1/devices/{id}/telemetry`. Protected by existing
  `X-rIOt-Key` + optional mTLS; inherits the existing device-auth trust
  model and the 20 MB snapshot size limit in `handlers.Telemetry`.
- **New alert template:** `auth_failure` / metric
  `failed_logins_interval` / threshold `> 0` / 300s cooldown. Appears in
  `GET /api/v1/settings/alert-templates`.
- **New event type constant:** `EventAuthFailure = "auth_failure"`. No new
  handler, no new endpoint — events flow through the existing
  `createEventAndNotify` / dispatcher path.
- **New cross-collector shared state:** `authFailureCounter` (in-memory,
  per-agent-process). One new mutable shared object. Tested via `-race`
  per AD Section 13.

---

## Findings

### HIGH

#### SEC-001: Unprivileged local users can forge the signal via journald (false positives → alert fatigue + real-attack masking)
**Severity:** HIGH
**Domain:** Input Validation / Log Injection / Business Logic Security
**Location:** AD-004, `internal/agent/collectors/logs.go` per-line loop

**Description:**
The pattern set (`Failed password`, `authentication failure`, `Invalid user`,
`pam_unix(sudo:auth): authentication failure`) is matched with
`strings.Contains` against the raw `MESSAGE` field of every journal entry
the `LogsCollector` reads at `--priority=0..6`. There is no filter on the
originating `SYSLOG_IDENTIFIER`, `_COMM`, `_UID`, or `_SYSTEMD_UNIT`.

On every systemd-based Linux distro in the supported matrix, any local
user can call:

    logger -p auth.warning "Failed password for root from 1.2.3.4 port 22 ssh2"

…and that line will be written to the journal with
`PRIORITY=4`, `SYSLOG_IDENTIFIER=<username>` (or whatever tag the caller
chose), and `_UID=<local uid>`. It will then be read by `LogsCollector` on
the next tick, matched by `matchesAuthFailure`, and counted as a genuine
auth failure. Journald stamps each entry with a trusted `_UID` / `_COMM`
by design specifically so consumers *can* distinguish real sshd messages
from forgeries — but this story's matcher ignores those fields.

**Attack Scenario:**
A low-privilege local user on a monitored host (e.g. a CI runner, a
shared-service container, a non-root tenant on a homelab VM) wants to
either (a) create notification noise for the operator, or (b) mask a real
concurrent attack:
1. Attacker runs `for i in $(seq 1 50); do logger -p auth.warning
   "Failed password for root from 10.0.0.$i"; done`.
2. Within ~60s the agent's LogsCollector reads these; the counter drains
   to 50; `FailedLoginsInterval = 50` is reported.
3. The rule fires; one event row + one notification per channel is
   written. Operator gets a warning-severity page at 3am for a nonexistent
   attack.
4. After the 300s cooldown, the attacker repeats. Operator's trust in
   the alert is eroded within a few days.
5. Meanwhile, a real attacker on the internet brute-forcing SSH produces
   identical events — which are now indistinguishable from the noise.
   During the 300s cooldown window after a forged fire, the real
   auth-failure burst produces no notification at all (events are still
   written once per cooldown, but the operator is not paged).

This is not a theoretical concern — on any multi-tenant or shared Linux
host the agent monitors, this is trivially exploitable without any
privilege beyond "has a shell account".

**Required Resolution:**
Before the rule increments the counter, `matchesAuthFailure` (or the
caller loop) must establish that the journal entry originated from a
trusted source. Acceptable properties the matcher must satisfy (one of
these, not prescribed):
- Only count entries whose `_UID` is 0 (written by a root process), OR
- Only count entries whose `SYSLOG_IDENTIFIER` / `_COMM` is in an
  allow-list of known auth daemons (`sshd`, `sudo`, `su`, `login`,
  `systemd-logind`, `polkitd`, `pam_unix`, `cron`), OR
- Only count entries whose `_SYSTEMD_UNIT` matches a known auth-producing
  unit (`ssh.service`, `sshd.service`, `sudo.service`, `systemd-logind.service`).

The `_UID` check alone is the simplest and most defensible — genuine auth
failures always come from a root-owned daemon in the standard distros.

This is a HIGH finding rather than CRITICAL because the blast radius is
bounded to the specific device the attacker has a shell on (horizontal
isolation — the attacker cannot inflate another device's counter), and the
cooldown clamps per-channel notification rate. It is not LOW because the
desired property of the feature ("the operator learns about real auth
failures within 60s") is directly undermined by exploitation.

**Blocks:** Implementation — must be addressed in the ADD before senior-dev
begins, OR explicitly re-scoped (e.g. "LOG-001 ships with a known caveat
that a local user can create noise; hardened under LOG-002") via FRD
update.

---

### MEDIUM

#### SEC-002: First-push zero creates an attacker-exploitable blind window across agent restarts
**Severity:** MEDIUM
**Domain:** Business Logic Security / Availability
**Location:** AD-001 (MarkReady latch), FR-005, sequence §7 "First-tick flow"

**Description:**
FR-005 and AD-001 specify that on the first telemetry interval after agent
start, `FailedLoginsInterval` is reported as zero regardless of how many
matching lines were read. This is correct with respect to "don't backfill
historical failures on fresh enrollment" — that's a defensible design
choice. The security consequence is that **any attacker-triggered or
attacker-timed agent restart produces exactly one 60-second window in
which auth failures on that device are not reported**, because the
LogsCollector's `since` on its first post-restart tick is zero (FR-005)
→ 5-minute backfill is read → counter is incremented → but MarkReady is
not called → the Drain result is discarded → zero is reported.

The restart trigger need not be privileged. The rIOt agent runs under a
`LimitNOFILE=65536` / `Restart=always` systemd unit; a low-privilege
local user who can cause any process to OOM-kill on a memory-pressured
host may trigger an agent restart. On a non-resource-constrained host,
the window is harder to trigger reliably — but any legitimate restart
(version upgrade via `riot-agent-update`, config edit, host reboot)
still produces the same blind 60s.

**Attack Scenario:**
1. Attacker plans an SSH brute-force burst.
2. Attacker (on the same host) triggers agent OOM, or waits for a
   legitimate `agent_update` via the auto-update path (which is
   advertised in events and predictable — `systemd-run --unit=riot-agent-update`
   triggers a visible restart).
3. Within the ~60s window after restart, attacker fires N failed logins.
4. Server receives `FailedLoginsInterval = 0` on the first post-restart
   interval. No alert fires.
5. On the second interval the attacker stops. Subsequent counts report
   zero normally; no alert is ever generated for that burst.

**Required Resolution:**
Option A (preferred, low-blast-radius): relax the first-push-zero rule to
allow counts from the backfill window to be reported iff the backfill
window is bounded to the LogsCollector's existing 5-minute lookback AND
the operator accepts this. Update FRD FR-005 to this semantic and
document the explicit tradeoff.

Option B (defer): accept the blind-window trade as a documented limitation
in the FRD's §9 "Out of scope" or §6 "Business rules", specifically
naming that post-restart auth bursts may be missed until interval 2.

Option C: emit a synthetic "agent restarted" event alongside the first
interval's zero, so the operator knows the detector was quiet for a
bounded window. This converts the blind-window into an acknowledged gap.

Any of the three resolves the finding. A silent blind window that is
unacknowledged by either the FRD or the operator-facing UX is the
problem; a consciously-chosen trade-off is not.

**Blocks:** Does not block implementation. Must be resolved at FRD or ADD
level before QA sign-off, not during implementation.

---

#### SEC-003: Agent may silently under-report on installs where `systemd-journal` group assignment is absent
**Severity:** MEDIUM
**Domain:** Infrastructure / Configuration
**Location:** `scripts/install.sh` lines 180–185, 416–418; interacts with
`internal/agent/collectors/logs.go` fail-open behavior

**Description:**
Verified: the installer (`scripts/install.sh`) does add `riot` to the
`systemd-journal` group when the group exists, and threads it through
the systemd unit's `SupplementaryGroups=`. This is correct. **However**,
the behavior is silent on any platform where:

1. The `systemd-journal` group does not exist (Alpine with musl/OpenRC,
   some minimal images), OR
2. The agent is installed via a non-scripted path (manual binary drop,
   Docker-in-host homelab, custom cloud-init), OR
3. The group exists but the user's shell session that started the agent
   didn't pick up the supplementary group (relevant for dev-mode /
   non-systemd starts).

In these cases:
- `journalctl --priority=0..6 --since <t>` without root or
  `systemd-journal` membership returns only the user's own logs
  (a non-root-owned shell will see empty output for system auth
  events), NOT system-wide auth events.
- LogsCollector's `exec.CommandContext("journalctl", ...)` returns an
  empty list with no error — the existing FR-006 fail-open path emits
  `FailedLoginsInterval = 0`.
- Operator sees "zero auth failures" on a device that in fact had
  failures the agent could not read. Silent false negatives.

The FR-006 fail-open behavior is correct for "journald crashed". It is
dangerous for "journald is fine but the agent lacks permission to read
it" — the observable telemetry is indistinguishable in both cases.

**Attack Scenario:**
An operator deploys a monitored host via a non-standard path (manual
install on Alpine; Docker sidecar on NixOS; ansible role that doesn't
replicate the installer's group assignment). The `auth_failure` rule is
enabled globally with the default template. All subsequent attacks on
that host go unreported — the operator has no signal that the detector
is blind. The device continues to report all other telemetry normally,
so the dashboard looks healthy.

**Required Resolution:**
Add one of the following properties to the ADD. All are acceptable:
- **Preflight check (preferred):** on LogsCollector first run (or agent
  start), the agent reads a known-system-only journal field (e.g.,
  `journalctl -n 0 -u systemd-journald.service --output=json` or
  similar distinguishing check) and logs a clear WARN at startup plus
  surfaces a per-device capability flag in telemetry that reads "agent
  cannot read system journal" so the dashboard can badge the device.
- **Startup self-test:** the installer's post-install step runs
  `sudo -u riot journalctl -n 1 -u ssh.service --output=json` (or the
  closest available auth daemon) and warns the operator if it returns
  empty or errors with a permission hint.
- **Doctor integration:** `internal/agent/doctor.go` (per the project's
  MEMORY.md "feedback_doctor_sync.md" convention) gets a new check
  that verifies the agent has read access to system-wide journal
  entries, and fails loudly if not.

At minimum, the ADD must explicitly acknowledge this failure mode so
senior-dev or the technical writer documents the prerequisite. A silent
fail-open path on a detection feature is a systemic defect, not a
feature.

**Blocks:** Does not block implementation. Must be addressed as a
condition before QA sign-off — QA engineer must verify the installed
agent's journal read capability as part of AC-002 / AC-006 testing.

---

#### SEC-004: Template default threshold `> 0` has no hysteresis — single legitimate failure per interval per device fires immediately, no grace
**Severity:** MEDIUM
**Domain:** Business Logic Security / Abuse Prevention
**Location:** FR-024, AD-006, template default Threshold: 0

**Description:**
The default threshold is `> 0`, with a 300s cooldown. The consequence is
that on every internet-exposed SSH host, a SINGLE legitimate mistyped
password or SINGLE botnet probe per 300s window fires a critical-path
notification. The threat-model table in ADD §10 notes that cooldown rate-
limits a compromised agent's spam. It does not note that a cooldown of
300s against a threshold of 0 means every internet-facing SSH device
will fire continuously — one notification per 5 minutes per device — the
moment the rule is created from the default template.

BR-005 explicitly identifies this as "the minimum gap that prevents
notification flooding while remaining actionable" but the math for an
operator with even 3 internet-facing SSH hosts facing routine bot
traffic is: 3 devices × 12 fires/hour × 24 hours = 864 notifications/day
at default settings.

This is not a vulnerability in the traditional sense — it's a design
choice that predictably produces alert fatigue. Alert fatigue IS a
security outcome: an operator who has muted the rIOt channel after
three days of noise will not respond to a real incident either.

**Attack Scenario:**
Adversarial: a motivated attacker whose goal is to exhaust the operator's
attention simply probes SSH at 1 attempt per 301 seconds to a single
device. Continuous paging. No rate-limit signal because cooldown resets.

Non-adversarial: the internet itself. Any public-facing SSH port receives
bot traffic; the default config makes every probe a notification.

**Required Resolution:**
Two defensible paths; pick one:
- **Option A (ADD change, no FRD change):** Template default threshold
  changes to something above baseline bot noise (e.g., `> 5` or `> 10`
  per interval). FR-024 specifies `0`; this requires an FRD update. The
  FRD's BR-005 already acknowledges the flood risk — raising the
  threshold is consistent.
- **Option B (FRD clarification):** Keep `> 0` as the default but add
  explicit operator-facing documentation in the template `Description`
  field warning "This default fires on every auth failure; on
  internet-facing hosts, raise the threshold and/or narrow IncludeDevices
  scope before enabling. Cooldown alone does not prevent noise."

Either path is acceptable. A silent default that ensures page-flood on
any realistic deployment is not.

This is flagged as a finding because the current ADD/FRD pair produces
predictable noise-level notifications on the very class of deployment
(internet-facing homelab SSH) the feature is meant to protect. The
security-relevant failure mode is alert fatigue → muted channel → real
attack missed.

**Blocks:** Does not block implementation. Architect must either raise
the default or add explicit operator-facing documentation on the
template; the latter requires an FRD text revision.

---

### LOW / INFORMATIONAL

#### SEC-005: Counter race semantics under concurrent collector execution (future-proofing)
**Severity:** LOW
**Domain:** Business Logic Security / Concurrency
**Location:** AD-001 (authFailureCounter), Performance §11

**Description:**
The ADD correctly notes that LogsCollector and SecurityCollector run
sequentially on the same ticker goroutine today, so the counter mutex is
effectively uncontended. The ADD also correctly requires `go test -race`
to pass on Linux.

The finding is that the "collectors run sequentially" invariant is
undocumented in the code comments of `RegisterDefaultsWithDocker` beyond
registration order (AD-002's required comment addresses ordering but not
concurrency). A future change that parallelizes `collectAll` (which is a
reasonable optimization as the collector count grows) could produce a
race where:
- LogsCollector is mid-loop, has added N to counter.
- SecurityCollector runs, Drains counter (takes N), calls IsReady (false
  on first tick) — reports 0 and drops N.
- LogsCollector finishes, calls MarkReady.
- Next tick, SecurityCollector Drains 0. The N auth failures are
  permanently lost (false negative; attacker benefit).

This is not exploitable today. It becomes exploitable the moment someone
parallelizes `collectAll`.

**Required Resolution:**
Add a load-bearing comment to `authFailureCounter` explicitly stating
"SecurityCollector MUST be registered after LogsCollector AND
`collectAll` MUST serialize the two collectors' Collect calls; parallel
execution would produce lost counts". This is documentation-only; no
code change required.

Alternatively, switch the Drain→IsReady ordering so that Drain is only
valid when IsReady is true from the caller's perspective, and lost
counts when IsReady is false are detectable (e.g., Drain returns
`(count, ready bool)` atomically under the same mutex hold).

**Blocks:** Nothing. Informational note for implementation quality.

---

#### SEC-006: Negative-value injection from malicious agent — ADD notes it but does not clamp
**Severity:** LOW
**Domain:** Input Validation
**Location:** AD-005 security-considerations row; ADD §5
`SecurityInfo.FailedLoginsInterval *int`

**Description:**
The ADD correctly observes in §10 that a malicious agent could submit
`FailedLoginsInterval: -1` or similar, and that `compareValue(-1, ">", 0)`
returns false so the rule would not fire. This analysis is correct for
the `auth_failure` template's `>` operator.

However, an operator may create a custom rule with operator `!=` or `<`
for the `failed_logins_interval` metric (see ADD §6 "Operators allowed:
any operator accepted by compareValue"). A negative-value payload from
a compromised agent would then fire spuriously. Also: `< 0` would match
any negative value, which is exactly the shape an attacker would use if
they compromised one agent and wanted to trigger alerts on their own
device-ID but against a differently-configured rule (though the rule-
matching path ensures the fired event attributes to the compromised
device's ID, so blast radius is still local).

**Required Resolution:**
On ingest in `CheckTelemetryThresholds`, clamp `*FailedLoginsInterval`
to `max(0, v)` before passing to `evaluateMetric`, OR reject the
telemetry snapshot entirely when the value is negative. The first is
lower-friction; either is acceptable.

This is LOW because the blast radius is the compromised device's own
rule-matching pipeline, and a compromised agent is already trusted for
its own telemetry. It is not zero because "clamp user-controlled
numbers at the boundary" is defense-in-depth the codebase already
practices elsewhere (the 20 MB snapshot cap, the 32-GPU cap in
CheckGPUAlerts, the 16-proxy cap in CheckNginxAccessAlerts).

**Blocks:** Nothing.

---

#### SEC-007: Notification channel content has no attacker-controlled strings — positive observation
**Severity:** INFORMATIONAL
**Domain:** Data Exposure / Information Leakage
**Location:** ADD §7 sequence step 8, event msgFn; `internal/server/notify/*.go`

**Description:**
Reviewed the notification backends (Telegram, Discord, Slack, SMTP,
Pushover, ntfy, webhook, gotify). The event message constructed in
AD-005 is:

    fmt.Sprintf("%d authentication failure(s) on %s in last interval",
        int(v), hostname)

`v` is a clamped integer count. `hostname` is `snap.Data.System.Hostname`
— self-reported by the agent, but already used in the existing trust
model for every other alert message (existing UPS/GPU/container alerts
do the same). No log-line content, no usernames, no source IPs flow
into the notification message. This is correct: FR-007 mandates raw
count, no per-user/per-IP state, so the notification cannot leak log
content even if an attacker forges log lines under SEC-001.

Consequence: the notification channel is not an injection sink for
attacker-controlled text via the logline path. Telegram's Markdown-mode
escape handling (`escapeMarkdown` in `telegram.go:110`) handles only
`_`, `*`, `` ` ``, `[` — which is incomplete for general
markdown-injection defense, but because the content is not attacker-
controlled in this feature, the incomplete escaper is not exploitable
from this story's surface.

This is an **explicit positive observation**: the data-shape choice
(count-only metric, no log content on the wire) eliminates a class of
log-smuggling → notification-channel-injection attacks that a
naive implementation (e.g., "attach the first matching log line as
context") would have introduced.

---

#### SEC-008: Event and notification_log worst-case write volume is bounded by cooldown — positive observation
**Severity:** INFORMATIONAL
**Domain:** Rate Limiting / Abuse Prevention

**Description:**
An attacker driving real or forged auth failures cannot force more than
one event row + N notification_log rows per cooldown window per device
per rule. With default cooldown of 300s, worst-case per-device write
rate is 12 events/hour + 12 × channel_count notifications/hour. For a
deployment with 10 devices × 3 channels, this caps at 360
notification_log rows/hour under sustained attack — well within DB
write budget.

Telemetry itself is rate-limited structurally by the agent's 60s
poll_interval (agent-controlled) and the 20 MB snapshot cap in
`handlers.Telemetry`. An attacker who compromises an agent to submit
telemetry at higher frequency already has a larger attack surface than
this feature introduces.

No finding. Documenting for QA awareness that cooldown enforcement IS
the abuse-prevention control and must be verified in tests AC-024.

---

## Positive Observations

- **Single journald read, no second exec.** AD-001 and AD-002 correctly
  avoid doubling journalctl exec cost. The counter-handoff pattern is
  the right choice.
- **Count-only metric, no log content on the wire.** The decision to
  transport a bare integer — rather than a list of matching log lines
  or attacker-controlled context — is a sound security-by-design
  choice that prevents a whole class of log-smuggling into notification
  channels (SEC-007).
- **Substring not regex.** AD-004's choice of `strings.Contains` over
  regex alternation avoids regex DoS surface and per-line allocation
  cost. Patterns are fixed, short, and ASCII — no catastrophic-
  backtracking risk.
- **24h metric is frozen.** BR-001 / NFR-005 / AD-003 correctly isolate
  the new field from the existing `FailedLogins24h` path and security
  score calculation. No collateral regression risk.
- **No new endpoints.** The story rides entirely on the existing
  `POST /api/v1/devices/{id}/telemetry` surface, which already has
  `X-rIOt-Key` + optional mTLS. No new auth surface to audit.
- **No default seeded rule.** AD-007 correctly declines to seed a
  default `auth_failure` rule. This preserves the upgrade path's "zero
  new notifications unless explicitly opted in".
- **Pointer-to-int semantics.** AD-003's choice of `*int` with
  `omitempty` correctly distinguishes "metric not reported" (nil) from
  "zero failures this interval" (explicit 0). The server-side nil-
  guard in `CheckTelemetryThresholds` makes non-Linux devices
  non-evaluable for the metric — no cross-platform false positives.

---

## Verdict Rationale

**PASS WITH NOTES.**

- **SEC-001 (HIGH)** is the most material finding. It undermines the
  feature's core promise on any multi-tenant Linux host: any local shell
  user can inflate the counter. The fix is small and scoped (add a
  `_UID` or `SYSLOG_IDENTIFIER` allow-list check inside the per-line
  match loop in `LogsCollector`). It is not a design-level redesign and
  does not change any acceptance criterion. Because it can be resolved
  by the senior-dev as an implementation detail within AD-004's scope
  without renegotiating the FRD, this is PASS WITH NOTES rather than
  REVISE (ADD). The architect should update AD-004 to codify the
  allow-list property before senior-dev starts; if the architect prefers
  to leave the decision to senior-dev, the implementation report must
  document which journald fields are consulted and why.

- **SEC-002 (MEDIUM)** and **SEC-003 (MEDIUM)** are real gaps but have
  acceptable "document the trade-off" resolutions that do not require
  redesigning the data flow. Both must be resolved (in FRD text,
  installer, or doctor) before QA sign-off — not before senior-dev
  starts.

- **SEC-004 (MEDIUM)** is a product-posture finding. The `> 0` default
  will produce predictable paging on internet-facing deployments. The
  resolution (either raise the default or document the noise expectation
  loudly in the template description) is cheap; leaving it unresolved
  predictably produces alert fatigue.

- **SEC-005 / SEC-006 (LOW)** are defense-in-depth items. No blocker.

The feature's underlying data-flow choices (count-only, no log content
on the wire, no new endpoints, single journald read, no new auth code)
are sound. The findings are all about trust boundaries **within** the
agent's journald read — not about the wire protocol, the server, or
the notification pipeline.

**Routing:** Invoke `senior-dev` with this report alongside the ADD.
SEC-001 must be addressed during implementation. SEC-002, SEC-003, and
SEC-004 must be verified as resolved (in ADD revision, FRD revision,
installer, or operator-facing documentation) before the QA engineer
issues PASS.
