# Formal Requirements Document

**Story ID:** LOG-001
**Title:** Near-Real-Time Auth Failure Alerting
**Author:** Business Developer Agent
**Date:** 2026-04-20
**Status:** FINAL

---

## 1. Executive Summary

rIOt must detect authentication failures (SSH, sudo, console, su) on Linux
devices within a single telemetry cycle (default 60 seconds) and surface them
through the existing alert rule and notification pipeline. Today the `security`
collector only reports a 24-hour rolling count (`FailedLogins24h`) that feeds
the security score — no alert rule can be written against it with sub-daily
latency. This story introduces a per-interval auth failure count, a matching
"Auth Failure" alert template, and verifies the existing `log_errors` template
is usable from the dashboard.

## 2. Background & Context

A homelab operator rotated SSH keys on one of their hosts. A stale script on
another host continued attempting to authenticate with the old key for hours
before the operator noticed — by that point the daily failure count had
climbed high enough to drag the security score down, but no actionable alert
ever fired.

Current state:

- The agent's `security` collector reports `FailedLogins24h`, a count of failed
  authentication attempts over the last 24 hours, sourced from a narrow log
  pattern.
- That field is consumed only by the server-side security score calculation.
- No `AlertRule` can express "alert me within one minute of a new auth
  failure" because the only available metric is a rolling 24h total.
- The agent's `LogsCollector` already streams journald entries per interval
  using a `lastSeen` cursor, so per-interval extraction of matching lines is
  a natural extension.
- The existing alert template catalog in `internal/server/alerts/templates.go`
  already includes a `log_errors` template; it is unclear whether the
  dashboard's template picker currently surfaces it.

This story closes the latency gap without disturbing the 24h metric or the
security score.

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Homelab Operator | Authenticated dashboard user managing their fleet | Create, edit, delete alert rules; view events and notifications |
| rIOt Agent (Linux) | Lightweight Go daemon running on a monitored device | Reads local journald; pushes telemetry to server via API key |
| rIOt Agent (non-Linux) | Agent running on Windows/macOS/BSD | Same telemetry path; does not populate the new metric |
| rIOt Server | Ingests telemetry, evaluates alert rules, dispatches notifications | Persists events and notification log |
| Notification Channel | External sink (existing: email, webhook, etc.) | Receives dispatched notifications |

## 4. Functional Requirements

### Per-interval auth failure metric

- **FR-001:** The Linux `security` collector must compute, on every collection
  interval, the count of authentication failure log lines that appeared in
  journald since the previous collection interval for that agent process.
- **FR-002:** The metric must be named `failed_logins_interval` on the wire
  and transported as a nullable integer (pointer-to-int semantics: present
  with a value on Linux, absent / null on all other platforms).
- **FR-003:** The pattern set for `failed_logins_interval` must match all of
  the following journald message patterns:
  - `Failed password` (SSH password auth)
  - `authentication failure` (general PAM)
  - `Invalid user` (SSH invalid user)
  - `sudo: pam_unix(sudo:auth): authentication failure` (sudo PAM)
  - Console and `su` login failures surfaced by PAM under the above patterns.
- **FR-004:** The metric must reuse the existing `LogsCollector` journald
  read and `lastSeen` cursor. The collector must not issue a second journald
  read to compute the count.
- **FR-005:** On the first telemetry push after agent start (i.e. when no
  prior cursor exists for the process), the agent must report
  `failed_logins_interval = 0`. It must not backfill the count from a
  historical window.
- **FR-006:** If journald is unavailable or `journalctl` returns an error
  during the interval, the agent must report `failed_logins_interval = 0`
  (fail-open) and log the underlying error through the existing agent
  logger.
- **FR-007:** The metric must be a raw count of matching log lines in the
  interval. The agent must not de-duplicate by source IP, user, or any other
  field.
- **FR-008:** The metric must not apply a noise-floor threshold at the agent
  or server level. Suppression of repeated alerts is the alert rule
  cooldown's job.
- **FR-009:** The `FailedLogins24h` field, its existing pattern set, its
  semantics, and the security score calculation that consumes it must remain
  unchanged.
- **FR-010:** `failed_logins_interval` must be exposed to the server-side
  alert rule evaluator as a metric selectable when authoring an
  `AlertRule` — the same way any other device metric is.
- **FR-011:** Non-Linux agents must omit `failed_logins_interval` from
  their telemetry payload (nil pointer); the server must treat absence as
  "metric not reported" and not evaluate alert rules that reference it
  against that device for that interval.
- **FR-012:** The new metric must travel inside the existing `security`
  collector payload — no new collector name, so existing deployments'
  `collector.enabled` whitelists continue to work without operator action.

### "Auth Failure" alert template

- **FR-020:** The server must ship a built-in alert template with ID
  `auth_failure`.
- **FR-021:** The template's display name must be `Auth Failure`.
- **FR-022:** The template must target the metric `failed_logins_interval`.
- **FR-023:** The template must use comparison operator `>`.
- **FR-024:** The template's default threshold must be `0`.
- **FR-025:** The template's default severity must be `warning`.
- **FR-026:** The template's default cooldown must be `300` seconds.
- **FR-027:** When the operator creates a rule from this template, the rule
  must be created with empty `IncludeDevices` (global scope) so the operator
  can narrow scope after creation.
- **FR-028:** When the operator creates a rule from this template, cooldown
  must be editable through the standard `AlertRule.CooldownSeconds` field —
  no schema change and no template-specific cooldown plumbing.
- **FR-029:** The template must belong to a new category named `security`.
  This category value must be added to the category list wherever the
  dashboard groups or filters templates.
- **FR-030:** Rules created from this template must fire through the
  existing notification-channel pipeline unchanged.

### `log_errors` template dashboard verification

- **FR-040:** The existing `log_errors` alert template defined in
  `internal/server/alerts/templates.go` must be selectable by the operator
  from the "Create from Template" picker in Settings > Alert Rules in the
  embedded dashboard.
- **FR-041:** If FR-040 is already satisfied by current code, no code change
  is required; the verification itself is the deliverable and the test
  evidence is the pass criterion. If FR-040 is not satisfied (template is
  filtered out, missing required field, or otherwise hidden), the minimum
  fix required to surface it is in scope.

## 5. Non-Functional Requirements

- **NFR-001 [Latency]:** An auth failure written to journald at time T must
  be reflected in an event and any resulting notification no later than
  `T + poll_interval + notification_dispatch_latency`. At the default
  `poll_interval: 60s`, worst-case user-visible latency is approximately
  60 seconds plus the existing notification dispatch time.
- **NFR-002 [Performance — agent]:** Computing `failed_logins_interval` must
  not add a second journald read per interval. The count must be derived
  from the same read the `LogsCollector` already performs.
- **NFR-003 [Performance — server]:** Alert rule evaluation for
  `failed_logins_interval` must be O(1) per device per interval (stateless
  on the server; threshold comparison only).
- **NFR-004 [Compatibility]:** Existing deployments must not require any
  change to agent YAML config, `collector.enabled` whitelists, or operator
  intervention to begin reporting the new metric after upgrade.
- **NFR-005 [Security score stability]:** The server-computed security score
  must not change value for any device solely as a result of this story —
  `FailedLogins24h` stays on its existing narrower pattern set.
- **NFR-006 [Audit]:** Each auth failure alert that fires must produce the
  existing `events` row and `notification_log` row. No new audit store is
  introduced.
- **NFR-007 [Availability]:** If the agent cannot read journald for any
  reason, the metric must degrade to zero rather than block telemetry
  submission.

## 6. Business Rules

- **BR-001:** The 24-hour count (`FailedLogins24h`) and the per-interval
  count (`failed_logins_interval`) are independent metrics with
  independent pattern sets. Changing one must not change the other.
- **BR-002:** The new metric is Linux-only for LOG-001. Windows Event Log
  coverage is LOG-003.
- **BR-003:** The pattern set for `failed_logins_interval` is fixed for
  LOG-001. Per-pattern toggles and custom regex are LOG-002.
- **BR-004:** Global-by-default scope for the Auth Failure template is the
  correct initial posture because the homelab operator's intent is fleet-
  wide visibility; scoping down is an affirmative act after creation.
- **BR-005:** Cooldown defaults to 300 seconds because a single misbehaving
  script can produce many failures per minute; 5 minutes is the minimum
  gap that prevents notification flooding while remaining actionable.

## 7. Data Requirements

### Entities involved

- **Telemetry payload (agent → server):** Extends the `security` collector
  payload with `failed_logins_interval *int`. No other fields change.
- **AlertRuleTemplate (server, in-memory catalog):** Gains one entry with
  id `auth_failure`, category `security`.
- **AlertRule (existing DB table):** No schema change. Rules created from
  the new template populate existing columns only.
- **Event (existing DB table):** Written by the existing alert evaluator
  when a rule fires. No schema change.
- **NotificationLog (existing DB table):** Written by the existing
  notification dispatcher. No schema change.

### Validation rules

- `failed_logins_interval`, when present, must be a non-negative integer.
- `failed_logins_interval` must be `null`/omitted on non-Linux agents.
- Template threshold, when a rule is created from the template, must be a
  non-negative integer (existing `AlertRule` validation applies).
- Template cooldown, when a rule is created from the template, must be a
  non-negative integer of seconds (existing `AlertRule` validation applies).

### State transitions

The metric itself is stateless per interval. The only stateful element is
the agent-side `lastSeen` journald cursor, which is already owned by the
`LogsCollector`.

| From | Event | To |
|------|-------|----|
| Agent start (no cursor) | First collection tick | Cursor initialized; `failed_logins_interval = 0` reported |
| Cursor exists | Collection tick, journald OK | Count of matching lines since cursor reported; cursor advanced |
| Cursor exists | Collection tick, journald error | `0` reported; cursor unchanged (existing LogsCollector behavior) |

Alert rule state transitions (FIRING / COOLDOWN / CLEARED) are the
existing evaluator's responsibility and are unchanged by this story.

## 8. Acceptance Criteria

### Cluster 1 — Per-interval metric

**AC-001:** [Maps to FR-001, FR-002, FR-010]
- Given a Linux agent with the `security` collector enabled
- When the agent completes a collection interval
- Then the telemetry payload contains an integer field
  `failed_logins_interval` in the security collector section
- And the server persists/forwards it such that an alert rule can reference
  it by that metric name

**AC-002:** [Maps to FR-003]
- Given a Linux agent with active journald
- When each of the following lines is written to journald during an
  interval: a `Failed password` entry, an `authentication failure` entry,
  an `Invalid user` entry, and a
  `sudo: pam_unix(sudo:auth): authentication failure` entry
- Then at the end of the interval `failed_logins_interval` equals `4`

**AC-003:** [Maps to FR-003]
- Given a Linux agent
- When a log line that does not match any auth-failure pattern is written
  to journald during an interval (e.g. `systemd: Started Session 17`)
- Then `failed_logins_interval` for that interval is `0`

**AC-004:** [Maps to FR-004, NFR-002]
- Given the agent is running the existing `LogsCollector` with its
  journald cursor
- When the `security` collector computes `failed_logins_interval`
- Then no additional `journalctl` invocation or journald read is issued
  beyond what `LogsCollector` already performs

**AC-005:** [Maps to FR-005]
- Given an agent process that has just started and has no prior cursor
- When the first telemetry interval completes
- Then `failed_logins_interval` is reported as `0`
- And no count derived from pre-start historical journald entries is
  included

**AC-006:** [Maps to FR-006, NFR-007]
- Given a Linux agent
- When journald is unreachable or `journalctl` exits with an error during
  the interval
- Then `failed_logins_interval` is reported as `0`
- And the agent logs the underlying error
- And the telemetry submission is not aborted

**AC-007:** [Maps to FR-007]
- Given 10 `Failed password` lines for the same source IP within a single
  interval
- When the interval closes
- Then `failed_logins_interval` equals `10` (no per-IP de-duplication)

**AC-008:** [Maps to FR-002, FR-011]
- Given an agent running on a non-Linux platform (Windows, macOS, BSD)
- When a telemetry interval completes
- Then the security collector payload omits `failed_logins_interval` or
  sends `null`
- And the server does not evaluate `failed_logins_interval`-based alert
  rules against that device for that interval

**AC-009:** [Maps to FR-009, NFR-005]
- Given a device producing auth failures
- When telemetry is ingested
- Then `FailedLogins24h` is computed using its pre-existing narrower
  pattern set
- And the device's security score is identical to what it would have been
  without this story's changes

**AC-010:** [Maps to FR-012, NFR-004]
- Given an existing deployment with `collector.enabled` set to its current
  whitelist that includes `security`
- When the agent is upgraded to the LOG-001 build without any config edit
- Then the agent reports `failed_logins_interval` on the next interval
- And no new collector name needs to be added to the whitelist

### Cluster 2 — Auth Failure alert template

**AC-020:** [Maps to FR-020, FR-021, FR-022, FR-023, FR-024, FR-025, FR-026]
- Given the server binary built from this story
- When the alert template catalog is enumerated (in code or via the API
  the dashboard uses to populate the template picker)
- Then an entry exists with: id `auth_failure`, display name `Auth Failure`,
  metric `failed_logins_interval`, operator `>`, default threshold `0`,
  default severity `warning`, default cooldown `300`

**AC-021:** [Maps to FR-027]
- Given the operator creates an alert rule from the `auth_failure` template
  without modifying scope
- When the rule is persisted
- Then `IncludeDevices` is empty (global scope)

**AC-022:** [Maps to FR-028]
- Given an alert rule created from the `auth_failure` template
- When the operator edits `CooldownSeconds` on the rule
- Then the rule persists the new cooldown using the standard
  `AlertRule.CooldownSeconds` column with no template-specific code path
- And no database schema change is required for this edit

**AC-023:** [Maps to FR-029]
- Given the alert template catalog
- When the dashboard groups or filters templates by category
- Then a category named `security` is available
- And the `auth_failure` template appears under it

**AC-024:** [Maps to FR-030, NFR-006]
- Given an alert rule created from the `auth_failure` template with at
  least one notification channel attached
- When a telemetry push arrives with `failed_logins_interval > 0` for a
  device in scope and the rule is not in cooldown
- Then exactly one `events` row is written for that firing
- And exactly one `notification_log` row per attached channel is written
- And the notification is dispatched through the existing notification
  pipeline

**AC-025:** [Maps to FR-025]
- Given the operator creates a rule from the `auth_failure` template
- When the operator changes severity from `warning` to `critical` before
  saving
- Then the rule is persisted with `severity = critical`

### Cluster 3 — `log_errors` template verification

**AC-030:** [Maps to FR-040, FR-041]
- Given the server binary built from this story
- When the operator opens Settings > Alert Rules > Create from Template in
  the dashboard
- Then the `log_errors` template (display name "Log Errors Detected")
  appears in the picker and is selectable
- And the existing template definition in
  `internal/server/alerts/templates.go` is the source of truth — if the
  template is already visible, no code change is required; if it is not,
  the minimum change to surface it is acceptable

## 9. Out of Scope

- Per-pattern toggles (e.g. enable SSH failures, disable sudo failures
  independently) — LOG-002.
- Custom user-supplied regex patterns — LOG-002.
- Windows Event Log auth failure collection — LOG-003.
- Fleet-wide log search UI — LOG-004.
- Any change to `FailedLogins24h`, its pattern set, or the security score
  engine.
- Dashboards, trend charts, or historical visualizations of auth failures.
- New notification channel types.
- New collector (the metric rides inside the existing `security` collector
  by design).
- Renaming or re-categorizing the existing `log_errors` template beyond
  what is strictly required to make it visible in the picker.

## 10. Assumptions

Documented so the architect can trace each default to a decision point.

- **A-001:** The metric is named `failed_logins_interval` on the wire and
  in alert rule metric selectors.
- **A-002:** The wire type is `*int` (Go pointer-to-int). A nil pointer is
  serialized as absent/`null` on non-Linux platforms; a non-nil pointer
  is serialized as a JSON integer.
- **A-003:** The pattern set is SSH + sudo + console + su, covering
  `Failed password`, `authentication failure`, `Invalid user`, and
  `sudo: pam_unix(sudo:auth): authentication failure`.
- **A-004:** The metric reuses `LogsCollector`'s existing journald read
  and `lastSeen` cursor. Two cursors are not maintained.
- **A-005:** First-push behavior after agent start is to emit zero. No
  historical backfill window is read.
- **A-006:** Journald unavailability is fail-open (emit zero, log the
  error, do not block telemetry).
- **A-007:** No per-IP or per-user de-duplication. Raw line count.
- **A-008:** No noise-floor threshold. Cooldown is the only spam defense.
- **A-009:** `FailedLogins24h` and its pattern set are frozen in this
  story. Security score value does not change.
- **A-010:** No new collector name. Existing `collector.enabled`
  whitelists (and the installer's whitelist-builder) require no update.
- **A-011:** Template ID is `auth_failure`, display name is "Auth Failure",
  default severity is `warning`, default cooldown is `300` seconds,
  operator is `>`, threshold is `0`.
- **A-012:** Template scope on creation is global (empty
  `IncludeDevices`). Operator narrows after create.
- **A-013:** Cooldown is configured through the existing
  `AlertRule.CooldownSeconds` field — no schema change, no
  template-specific cooldown plumbing.
- **A-014:** A new template category `security` is introduced. No
  existing template currently uses this category.
- **A-015:** Notification dispatch uses the existing channel pipeline
  unchanged.
- **A-016:** The `log_errors` template already exists in
  `internal/server/alerts/templates.go`. Cluster 3 is verification-only
  unless the UI does not render it.

## 11. Open Questions

None. All clarifying questions from the Stage 1 intake were resolved in
the scope decisions above. Any architect-level technical questions
(e.g. exact handoff mechanism between `LogsCollector` and the `security`
collector for the shared cursor) are implementation concerns for the ADD,
not requirements gaps.

## 12. Dependencies

- **Internal:** Existing `LogsCollector` (journald read + `lastSeen`
  cursor) in `internal/agent/`. Existing `security` collector in
  `internal/agent/`. Existing alert template catalog at
  `internal/server/alerts/templates.go`. Existing alert rule evaluator,
  events pipeline, and notification dispatcher in `internal/server/`.
  Existing `AlertRule` schema (specifically `CooldownSeconds`,
  `IncludeDevices`, and severity fields).
- **External:** `journalctl` / systemd-journald on Linux target devices.
  No new external services.
- **Teams:** None — single-pipeline story.
- **Downstream stories:** LOG-002 (curated pattern catalog + custom
  regex), LOG-003 (Windows Event Log), LOG-004 (fleet log search UI) all
  build on this metric shape but are independently scheduled.
