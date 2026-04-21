# Architecture Decision Document

**Story ID:** LOG-001
**Title:** Near-Real-Time Auth Failure Alerting
**FRD Reference:** `docs/requirements/LOG-001-frd.md`
**Author:** Architect Agent
**Date:** 2026-04-20
**Status:** FINAL

---

## Revision History

| Rev | Date       | Summary |
|-----|------------|---------|
| 1   | 2026-04-20 | Initial FINAL. |
| 2   | 2026-04-20 | Security review response: harden auth-failure matcher against `logger(1)` forgery via `_UID=0` + `_SYSTEMD_UNIT`/`SYSLOG_IDENTIFIER` allow-list (SEC-001); add `riot-agent doctor` journal-read preflight (SEC-003); document collector-serialization invariant on the counter (SEC-005); server-side clamp negative `FailedLoginsInterval` at ingest (SEC-006); emit synthetic "auth failure detector initialized" info event on restart to acknowledge first-push-zero blind window (SEC-002, deferred behavior); operator-facing warning on the `> 0` default for internet-facing SSH hosts (SEC-004, deferred behavior). Original data flow (counter handoff, `*int` field, no seeded default rule, runtime OS check) unchanged.

---

## 1. Summary

Add a per-interval auth-failure counter to the agent's existing `security`
collector payload, fed from the single journald read the `LogsCollector`
already performs. Ship an `auth_failure` alert template in a new `security`
category so the metric is selectable from the existing template picker, and
route evaluation through the existing `evaluateMetric` path. Verification of
the pre-existing `log_errors` template in the dashboard picker requires no
code change.

---

## 2. Technical Context

### Current state

- **Agent**
  - `internal/agent/collectors/collector.go` registers all collectors into an
    ordered slice. Iteration order in `telemetry.go:collectAll` is
    registration order.
  - `internal/agent/collectors/security.go` runs a 24-hour `journalctl --since
    "24 hours ago" -u sshd` call every interval and populates
    `SecurityInfo.FailedLogins24h` with a count of `Failed password` /
    `authentication failure` substring matches. SecurityCollector is
    registered *before* LogsCollector today.
  - `internal/agent/collectors/logs.go` maintains its own `lastSeen time.Time`
    cursor under a mutex; on each tick it runs a second, independent
    `journalctl --since <lastSeen> --priority=0..6 -o json -n 500` call,
    emits `[]models.LogEntry`, and advances the cursor. First-tick cursor is
    zero → it backfills the previous 5 minutes (intentional behavior for the
    logs panel). The per-line parse **already reads** `_SYSTEMD_UNIT` and
    `SYSLOG_IDENTIFIER` from the same journald JSON payload — adding a
    `_UID` read (required by AD-004 below) is additive, not a new parse.
  - `internal/agent/telemetry.go` type-switches collector output into
    `FullTelemetryData` fields; there is no inter-collector shared state
    today.
  - Non-Linux platform split in `gpu.go` / `gpu_other.go` uses build tags
    (`//go:build linux` and `//go:build !linux`). `security.go` uses a
    runtime `runtime.GOOS != "linux"` early-return. `logs.go` also uses a
    runtime early-return.
  - `internal/agent/doctor.go` already enumerates collector dependencies
    (`collectorDeps` map) and performs a `smartctl` sudo preflight as the
    precedent for per-collector runtime capability checks. New doctor check
    for journald read permission (AD-010) follows this pattern.
- **Server**
  - `internal/server/events/generator.go` exposes `evaluateMetric(ctx,
    deviceID, metric, value, hostname, eventType, msgFn)` as the single entry
    point for numeric-threshold rule evaluation. It handles cooldown, rule
    matching, event creation, and notification dispatch. Telemetry-level
    metrics are dispatched from `CheckTelemetryThresholds`.
  - `internal/server/events/templates.go` returns a hard-coded
    `[]models.AlertTemplate`. Existing categories in use: `service`,
    `network`, `process`, `system`, `container`, `ups`, `usb`, `webserver`,
    `disk`, `gpu`. No `security` category exists yet.
  - `internal/server/server.go:646 seedDefaultAlertRules` inserts six default
    rules only when the `alert_rules` table is empty on startup.
  - `internal/models/alerts.go` `AlertRule` doc-comment lists known metrics:
    `mem_percent`, `disk_percent`, `updates`, `container_died`,
    `container_oom`, `device_offline`, `service_state`, `nic_state`,
    `process_missing`. `AlertTemplate.Category` is free-form string; the
    doc-comment lists examples (`service, network, process, system,
    container`) but is not enum-enforced.
  - `internal/server/events/generator.go:1094 evaluateMetric` has a hardcoded
    fallback-threshold block for `mem_percent`, `disk_percent`, `updates`
    only. Unknown metrics with no matching rule cleanly no-op.
- **Dashboard**
  - `web/src/pages/settings/AlertRuleSettings.tsx` — `TemplatePicker`
    component (lines 594–642) is entirely data-driven. It calls
    `settingsApi.getAlertTemplates()`, groups results by `tpl.category` using
    `[...new Set(templates.map(t => t.category))]`, and renders each group.
    There is no category whitelist, no template whitelist. Only filter: the
    Docker feature flag hides `category === 'container'`.
  - `web/src/types/models.ts:366 SecurityInfo` mirrors the Go struct with
    `failed_logins_24h: number` as non-optional.
  - `web/src/components/CreateAlertDialog.tsx` is a separate dialog invoked
    from device/service drill-down pages, driven by a
    `METRIC_DEFAULTS` map. It already has `log_errors` in the map. It does
    NOT drive the Settings template picker — those are independent paths.

### What is missing

1. A per-interval auth-failure count exposed alongside `FailedLogins24h`.
2. A mechanism for the `security` collector to read auth-match counts from
   the `LogsCollector`'s single journald read without a second read.
3. An `auth_failure` alert template.
4. Wiring `failed_logins_interval` through `CheckTelemetryThresholds` →
   `evaluateMetric`.
5. A doctor preflight that detects a journald permission gap before it
   manifests as silent zeros in production telemetry.

### What stays untouched

- `FailedLogins24h` field, its pattern set, the 24h `journalctl` read, and
  any code that consumes it (security score calculation in
  `internal/server/summary/` and `internal/server/handlers/security.go`).
- `LogsCollector`'s emitted `[]models.LogEntry` contract (its 5-minute
  first-tick backfill for the log entries list is intentional and is
  preserved for the logs panel).
- The `TemplatePicker` React component (already data-driven).
- The `CreateAlertDialog` React component.
- No DB migration, no API contract change for existing endpoints.

---

## 3. Architecture Decisions

### AD-001: Counter-handoff between LogsCollector and SecurityCollector

**Decision:** Introduce a new private type `authFailureCounter` in
`internal/agent/collectors/auth_counter.go` with three methods:
- `Add(n int)` — atomic increment.
- `Drain() int` — returns the current value and resets to zero; atomic.
- `MarkReady()` / `IsReady() bool` — one-way latch set after the first
  successful `LogsCollector.Collect` completes, used to implement FR-005
  (first-interval zero).

Both `LogsCollector` and `SecurityCollector` hold a pointer to the *same*
counter instance, wired up at registration time in
`collector.go:RegisterDefaultsWithDocker`. On each tick:
- `LogsCollector` iterates the journald JSON lines it already reads. For
  each line, it applies the **trusted-origin filter** from AD-004 (not just
  the pattern match). Only entries that pass the filter AND match an
  auth-failure pattern increment `authFailureCounter.Add(1)`.
- After the LogsCollector scan completes **for an interval where the cursor
  was non-zero coming in** (i.e. this is not the first-ever scan), it calls
  `counter.MarkReady()`.
- `SecurityCollector` calls `counter.Drain()`. If `counter.IsReady()` is
  false (first interval, or LogsCollector has never run yet), it sets
  `FailedLoginsInterval = intPtr(0)` unconditionally and discards whatever
  Drain returned. Otherwise it sets it to `intPtr(drained)`.

**Rationale:**
- FR-004 / NFR-002 mandate no second journald read. The counter uses the
  same in-memory loop the LogsCollector already runs over the JSON output.
- FR-005 mandates first-interval zero. The `MarkReady` latch is the cheapest
  enforcement point: it's set *after* the first real scan completes, so
  anything drained on or before that first scan is dropped.
- A shared counter keeps the two collectors' output contracts unchanged —
  no type-switch wiring in `telemetry.go:collectAll` changes.
- The collectors remain independently testable: LogsCollector's unit tests
  do not need to know what downstream consumer reads the counter;
  SecurityCollector's unit tests inject a pre-populated counter fake.

**Alternatives considered:**
1. *Have `LogsCollector` write the count directly into `SecurityInfo`.* Rejected:
   inverts collector data-ownership, requires LogsCollector to know about
   `SecurityInfo`, and creates a coupling where the logs collector mutates
   another collector's output.
2. *Have `SecurityCollector` run its own journald read filtered to auth
   patterns.* Rejected: violates NFR-002 (second journald read per interval).
3. *Parse `models.LogEntry` slice downstream on the server.* Rejected: FRD
   explicitly places the delta computation agent-side, and the server would
   need to know the exact pattern set. This would also couple the
   `LogsCollector`'s log-retention semantics (500 line cap, priority filter)
   to alert latency.
4. *Reorder registry so LogsCollector runs first, no counter.* This is
   required (see AD-002) but alone it is insufficient — the handoff still
   needs an explicit, testable data path.

**Consequences:**
- Adds one new file (`auth_counter.go`) and a dependency between two
  previously independent collectors.
- Collector registration order becomes load-bearing (documented in
  `collector.go`).
- The counter is per-agent-process state; on agent restart it is zero,
  which aligns with FR-005 / A-005.
- Collector *serialization* (sequential execution within `collectAll`) is
  ALSO load-bearing — see SEC-005 / AD-001 code comment requirement in
  Section 12 note 13.

---

### AD-002: Collector registration order

**Decision:** In
`internal/agent/collectors/collector.go:RegisterDefaultsWithDocker`, move
`LogsCollector` registration to run *before* `SecurityCollector`. All other
collectors keep their existing positions.

**Rationale:** The counter handoff (AD-001) requires LogsCollector to
populate the counter before SecurityCollector drains it within the same
interval. Registration order determines `collectAll` iteration order.

**Alternatives considered:**
- *Two-tick latency (drain next interval).* Rejected: doubles user-visible
  alert latency and violates the NFR-001 "T + poll_interval" envelope.

**Consequences:**
- Any existing test that asserts exact collector ordering needs to be
  audited. Grep result: no such assertion exists (verified via
  `collector_test.go` review during ADD authoring).
- The `collector.go:RegisterDefaultsWithDocker` function is documented with
  an inline comment explaining the constraint.

---

### AD-003: Field placement, name, and JSON tag

**Decision:** Add a single field to `models.SecurityInfo`:

```go
// FailedLoginsInterval is the count of authentication-failure log lines
// observed in journald during the last telemetry interval. It is a pointer
// so non-Linux agents can omit the field (nil → absent in JSON). A value
// of zero is explicitly reported on Linux when no failures were observed
// or journald was unavailable (fail-open, FR-006).
FailedLoginsInterval *int `json:"failed_logins_interval,omitempty"`
```

- Field lives on `SecurityInfo` (not a new top-level telemetry field).
- Wire name (`json` tag): `failed_logins_interval`.
- Type: `*int` with `omitempty`. Nil ⇒ field absent in JSON.
- `FailedLogins24h int` is untouched — purely additive.

**Rationale:**
- FR-002 mandates pointer-to-int + absent on non-Linux.
- FR-012 mandates the metric rides inside the existing `security` collector
  payload (no new collector, no new top-level telemetry field).
- `*int` with `omitempty` matches the prevailing pattern in the codebase
  (`GPUDeviceMetrics.TemperatureC`, `BlockDevice.SmartTemp`, etc.).
- Compatibility: older agents that never set the field marshal it as nil →
  absent, which the server already handles (see AD-005).

**Alternatives considered:**
- *Non-pointer `int` with `omitempty`.* Rejected: `omitempty` treats `0` as
  absent, but zero is a meaningful reported value on Linux (AC-005, AC-006).
- *Separate top-level `AuthFailureInfo` struct.* Rejected: violates FR-012
  and complicates `collectAll` switch logic for no benefit.

**Consequences:**
- Downstream `SecurityInfo` consumers (score engine, security UI) see a
  nil pointer and ignore the field. The TypeScript mirror gets a new
  optional field (see §4).

---

### AD-004: Pattern matching — trusted-origin filter + substring list

**Decision:** Each journald JSON entry must satisfy BOTH an origin filter
AND a content filter before it increments the counter. Implemented as a
single unexported function `matchesAuthFailure(raw map[string]interface{}) bool`
that returns true iff:

**Origin filter (defense in depth, ALL of the below):**

1. `_UID == "0"` — the journal entry was written by a root-owned process.
   Journald stamps `_UID` from the peer credentials of the writing socket;
   userspace cannot forge this field. This alone blocks the `logger(1)`
   forgery path because `logger` runs as the invoking shell user.
2. AND `_SYSTEMD_UNIT` is in the allow-list
   `{"ssh.service", "sshd.service", "sudo.service", "systemd-logind.service", "login.service"}`
   OR `SYSLOG_IDENTIFIER` is in the allow-list
   `{"sshd", "sudo", "login", "su"}`. Journald stamps `_SYSTEMD_UNIT`
   from the cgroup of the writing process — userspace cannot set it.
   `SYSLOG_IDENTIFIER` is a header field on the log message and IS
   forgeable by userspace, but combined with the `_UID=0` check above,
   forgery would require a root-owned process to intentionally identify
   as `sshd` — well outside this feature's threat model.

**Content filter (unchanged; applied only after origin passes):**

A case-sensitive substring match against the `MESSAGE` field:

1. `Failed password`
2. `authentication failure`
3. `Invalid user`
4. `pam_unix(sudo:auth): authentication failure`

A line matches if **any** substring is present. Matching short-circuits on
the first hit. The origin filter short-circuits the entry BEFORE the
content match is attempted, so non-trusted entries impose no string-scan
cost.

**Journald fields consumed:** `_UID`, `_SYSTEMD_UNIT`, `SYSLOG_IDENTIFIER`,
`MESSAGE`. All four are already present in the JSON object the
`LogsCollector.Collect` loop unmarshals (`_SYSTEMD_UNIT` and
`SYSLOG_IDENTIFIER` are read by the existing log-panel path; `_UID` and
`MESSAGE` are available in the same `map[string]interface{}`). No new
journalctl flag, no new exec, no new parse — this is a read of three
additional map keys per line that were already in the JSON payload.

**Rationale:**
- **SEC-001 mitigation.** Without this filter, any local user can run
  `logger -p auth.warning "Failed password for root ..."` and inflate the
  counter. The `_UID=0` check blocks that because `logger` writes with the
  invoking user's UID. The `_SYSTEMD_UNIT` / `SYSLOG_IDENTIFIER` allow-list
  provides a second independent gate (defense in depth): a forgery would
  have to come from a root-owned process AND identify itself as one of the
  five known auth daemons. Combined, the attack reduces to "be root" — at
  which point the attacker has already won the game this feature is
  playing, and the alert signal integrity is out of scope.
- **`_UID` is trustworthy.** Journald derives `_UID` from `SO_PEERCRED` on
  the `/run/systemd/journal/socket` Unix domain socket. Userspace writers
  cannot set it; it is the kernel-attested uid of the writer process.
- **`_SYSTEMD_UNIT` is trustworthy.** Derived from the cgroup of the writer
  process, also via kernel-attested peer cred lookup; userspace cannot set
  it.
- **`SYSLOG_IDENTIFIER` is NOT trustworthy alone.** It's a freeform header
  field in the log entry. Combined with `_UID=0` it's sufficient — a root
  process typing the wrong identifier is still a root process and therefore
  not in scope.
- FR-003 specifies the four content substrings. All four survive as plain
  substrings — no regex capture groups needed.
- `strings.Contains` is O(n) in message length, zero allocations. A
  compiled regex alternation would allocate per-match on hot paths and is
  not justified for four literal substrings.
- Pattern 4 (`pam_unix(sudo:auth): authentication failure`) is more
  specific than pattern 2 (`authentication failure`). Pattern 2 will
  already match sudo PAM lines; pattern 4 is listed for FRD clarity but is
  redundant in practice. The early-exit loop ensures no double-count.
- Matching the `MESSAGE` field only (not the whole JSON line) avoids false
  positives from journald metadata keys.

**Alternatives considered:**
- *No origin filter; rely only on content.* Rejected: SEC-001 blocker —
  trivially forgeable via `logger(1)`.
- *Origin filter only (`_UID=0`), no unit allow-list.* Considered; rejected
  in favor of defense-in-depth. Adds ~5 map comparisons per entry — no
  measurable perf cost — and hardens against a hypothetical root-owned
  misbehaving daemon that happens to emit "Failed password" in its
  diagnostics. Cost is trivial; benefit is non-zero.
- *Unit allow-list only, no `_UID` check.* Rejected: `SYSLOG_IDENTIFIER` is
  userspace-settable; `_SYSTEMD_UNIT` would be sufficient on its own for
  systemd-managed daemons but fails closed for any auth events not
  routed through a systemd unit (rare, but the `_UID=0` check provides
  coverage for those edge cases and is the stronger primary gate).
- *`regexp.MustCompile` with alternation.* Rejected: higher startup cost,
  ~3x slower per-line in Go benchmarks, more allocations, and LOG-002 is
  the story that introduces a user-configurable pattern catalog — deferring
  the regex cost until it's actually needed.

**Consequences:**
- LOG-002 will replace or extend `matchesAuthFailure` with a configurable
  catalog. The function is deliberately scoped as unexported for easy
  replacement. The origin filter must be preserved in any replacement
  unless LOG-002 explicitly re-opens the trust-boundary question.
- Pattern list is frozen per BR-003. Changes require a new story.
- A non-root-owned auth daemon (exotic configurations, e.g., `sshd`
  running under a dedicated non-root service user) would not be counted.
  This is a known false-negative in exchange for blocking SEC-001. If a
  deployment needs to count non-root auth events, LOG-002's configurable
  catalog is the forward path.

---

### AD-005: Server-side routing via `evaluateMetric`

**Decision:** Add a single call in
`internal/server/events/generator.go:CheckTelemetryThresholds`, gated on
both `data.Security != nil` **and** `data.Security.FailedLoginsInterval !=
nil`. The dereferenced value is **clamped** to `max(0, v)` before being
passed to `evaluateMetric` (SEC-006):

```
if data.Security != nil && data.Security.FailedLoginsInterval != nil {
    v := *data.Security.FailedLoginsInterval
    if v < 0 {
        v = 0
    }
    g.evaluateMetric(ctx, deviceID, "failed_logins_interval",
        float64(v), hostname,
        models.EventAuthFailure,
        func(f float64) string {
            return fmt.Sprintf("%d authentication failure(s) on %s in last interval",
                int(f), hostname)
        })
}
```

Introduce a new event type constant `EventAuthFailure EventType =
"auth_failure"` alongside the other `EventType` constants in
`internal/models/events.go` (file exists; new constant is additive).

**Rationale:**
- `evaluateMetric` is the established path for all numeric-threshold rules
  (GPU, nginx, UPS battery, disk temp). Using it gets cooldown, rule
  matching, device-scope filtering, and `createEventAndNotify` for free
  (NFR-003, NFR-006, FR-010).
- Nil-pointer guard enforces FR-011 on the server: when a non-Linux agent
  submits telemetry, `FailedLoginsInterval` is nil and
  `evaluateMetric` is never called — alert rules referencing the metric
  simply do not fire for that device that interval.
- **Negative-value clamp (SEC-006).** A buggy or hostile agent could
  submit a negative integer. The default `auth_failure` template uses
  `>` against threshold 0 and would ignore negatives, but operators may
  create custom rules with `!=` or `<` operators against this metric
  (AD-006 explicitly permits any operator in `compareValue`). Clamping
  at the server boundary is defense-in-depth consistent with other caps
  in `CheckTelemetryThresholds` (e.g., the 32-GPU cap, 16-proxy cap).
- The `evaluateMetric` function has a hardcoded-fallback switch for
  `mem_percent`, `disk_percent`, `updates`. The new metric is **not** added
  to that switch — absent a user rule, no fallback event fires. This
  matches the nginx/GPU pattern and BR-005 (cooldown is the only spam
  defense, which implies a user-created rule must exist).

**Alternatives considered:**
- *Bespoke `CheckAuthFailureAlerts` function.* Rejected: pattern mismatch
  — existing codebase uses `evaluateMetric` for single numeric metrics;
  bespoke functions exist only where multi-entity iteration is needed
  (per-service, per-container, per-GPU).
- *Reject telemetry snapshot entirely on negative value.* Rejected: a
  single malformed field should not drop an otherwise-valid snapshot.
  Clamp is the lower-friction choice consistent with existing caps.

**Consequences:**
- The `EventAuthFailure` event type is available for event-stream
  rendering; no UI work is required — the dashboard renders any event type
  generically (verified: no enum whitelist in `web/src/components/Events*`
  exists).

---

### AD-006: Template placement — new `security` category

**Decision:** Append one entry to the slice returned by
`internal/server/events/templates.go:AlertTemplates()`:

```
{
    ID:              "auth_failure",
    Name:            "Auth Failure",
    Category:        "security",
    Metric:          "failed_logins_interval",
    Operator:        ">",
    Threshold:       0,
    Severity:        "warning",
    CooldownSeconds: 300,
    NeedsTargetName: false,
    Description:     "Fires when a device reports one or more authentication failures since the last telemetry interval. Note: on internet-facing SSH hosts, routine bot traffic will trigger this at every cooldown window — raise the threshold and/or narrow IncludeDevices before enabling.",
}
```

No other template is modified. No category enum is introduced — the
dashboard's `TemplatePicker` derives the category list at render time via
`[...new Set(templates.map(t => t.category))]`, so a new category string
appears automatically.

**Rationale:**
- FR-020 through FR-026, FR-029 specify every field directly.
- The `Description` carries an explicit operator-facing warning about the
  `> 0` default threshold on internet-facing hosts (SEC-004 deferred
  resolution; see Section 12 note 14).
- The FRD's FR-029 references
  `internal/server/alerts/templates.go`. The actual path in the codebase
  is `internal/server/events/templates.go`. This is a path-typo in the FRD,
  not an architectural conflict; the template catalog returned by
  `AlertTemplates()` is the single source of truth wired through
  `handlers/settings.go:ListAlertTemplates` → `GET /api/v1/settings/alert-templates`.

**Alternatives considered:**
- *Reuse `system` category.* Rejected: FR-029 explicitly mandates a new
  `security` category.
- *Raise default threshold to `> 5` or `> 10`.* Rejected per user decision
  (SEC-004 deferred). Description-field warning is the accepted mitigation.

**Consequences:**
- Other future security-related templates (LOG-003 Windows auth, possible
  firewall-block-rate templates) can join this category without further
  schema work.

---

### AD-007: No default seeded `AlertRule` for `auth_failure`

**Decision:** Do **not** add a default `AlertRule` to the
`seedDefaultAlertRules` slice in `server.go:656`. Operators create one from
the template picker.

**Rationale:**
- FR-027 says rules created from the template have global scope so the
  operator narrows afterwards. A seeded default would fire on every device
  for every single failed login by default — this is the "notification
  flood on fresh install" failure mode BR-005 explicitly warns against.
- The six existing seeded defaults are either noise-tolerant (memory,
  disk, updates — infrequent transitions) or discrete events
  (`container_died`, `container_oom`, `device_offline`). `failed_logins_interval`
  is a high-frequency, per-interval metric — seeding it on would guarantee
  a complaint ticket on day one for any deployment with a public-facing SSH
  port.
- FRD does not require a seeded default. The template is the opt-in surface.

**Alternatives considered:**
- *Seed disabled by default (`Enabled: false`).* Rejected: mixes metaphors
  — the existing six defaults are all `Enabled: true`. A disabled default
  rule carries no informational value and clutters the rules list.

**Consequences:**
- Upgrade path: existing deployments see the new template in the picker
  with no behavior change until the operator creates a rule.

---

### AD-008: Platform split — runtime check on `security` collector, no build tags

**Decision:** Keep the existing `if runtime.GOOS != "linux" { return info,
nil }` early-return at the top of `SecurityCollector.Collect`. On non-Linux,
`SecurityInfo.FailedLoginsInterval` is left as its zero value (nil) → JSON
absent per AD-003.

No new build-tagged files are introduced. The existing `LogsCollector` is
already Linux-only via the same runtime check, and its counter stays empty
on non-Linux, which is fine — SecurityCollector exits before trying to
drain it.

**Rationale:**
- `security.go` and `logs.go` already use runtime checks; introducing build
  tags for one new field would be inconsistent with the file's current
  pattern. Build tags are used in this codebase only when the non-Linux
  implementation needs a materially different code path (e.g., `gpu.go` has
  Linux-specific exec logic worth isolating).
- The counter type is a simple sync-wrapped int; compiling it into the
  non-Linux binary costs ~50 bytes of binary size and has no runtime cost
  when unused.

**Alternatives considered:**
- *`auth_counter_linux.go` + stub `auth_counter_other.go`.* Rejected: no
  OS-specific API is involved; pure overhead.

**Consequences:**
- Single code path, single test binary on all platforms.

---

### AD-009: `log_errors` template dashboard verification — no code change

**Decision:** FR-040 is satisfied by the current codebase. The
`TemplatePicker` component in
`web/src/pages/settings/AlertRuleSettings.tsx:594` has no template
whitelist, no category whitelist, and no filter that would hide a
`system`-category template. `GET /api/v1/settings/alert-templates` returns
the full `AlertTemplates()` slice including the `log_errors` entry.

Deliverable for AC-030 is a dedicated frontend test in
`web/src/pages/settings/AlertRuleSettings.test.tsx` asserting that given a
mocked API response containing the `log_errors` template, the
`TemplatePicker` renders a button labeled "Log Errors Detected" under the
`system` category.

**Rationale:** FR-041 explicitly permits this when current code already
surfaces the template. Verified by direct inspection during ADD authoring.

**Consequences:** No production code change for Cluster 3.

---

### AD-010: `riot-agent doctor` journal-read preflight

**Decision:** Extend `internal/agent/doctor.go` with a new check in the
"Dependencies" or a new "Journal access" section that runs when the `logs`
OR `security` collector is enabled. The check executes:

```
journalctl --priority=0..6 -n 1 --no-pager
```

(as the currently-executing user, which in production is `riot` under the
systemd unit; in dev it may be the developer's user account.)

- **If the command returns non-empty output** → `pass("Journal read access
  OK")`.
- **If the command returns empty output AND the user is not in the
  `systemd-journal` group** (checked via parsing `id -Gn` output for
  `systemd-journal`) → `warn` with the remediation text:
  `sudo usermod -a -G systemd-journal riot && sudo systemctl restart riot-agent`
  and a note that without journal read access the auth-failure detector
  will silently report zero.
- **If the command returns empty output AND the user IS in the
  `systemd-journal` group** → `warn("Journal read returned empty — journald
  may be inactive or priority filter matched nothing")`. Do not flag as
  permission issue (the group membership is satisfied).
- **If the command errors** (journalctl not found, exec error) → existing
  dependency-check path (`collectorDeps["logs"] = {"journalctl"}`) already
  warns. No double-warn.

The check is Linux-only; gated on `runtime.GOOS == "linux"`. On non-Linux
the check is skipped silently (the `logs` and `security` collectors
early-return on non-Linux so there is no capability question).

**Rationale:**
- **SEC-003 mitigation.** The installer's `systemd-journal` group
  assignment is not universal (Alpine without the group, manual installs,
  docker-host sidecar installs, ansible roles that skip group setup). On
  those systems, `journalctl` returns empty with no error, LogsCollector
  fails open per FR-006, `FailedLoginsInterval` reports zero, and the
  operator has no visible signal that the detector is blind. A doctor
  preflight surfaces the gap at install/diagnose time.
- **Consistent with MEMORY.md convention** ("all collectors with system
  dependencies are represented in doctor.go"). The `logs` / auth-failure
  dependency is a journald read capability, which is distinct from the
  `journalctl` binary availability that `collectorDeps` already checks.
- **Non-fatal.** This is a WARN not a FAIL, consistent with the rest of
  the doctor output. The operator fixes it and re-runs.

**Alternatives considered:**
- *Server-side capability flag in telemetry.* Rejected: larger surface
  (agent-side self-test + new wire field + dashboard badge). Doctor
  check is the minimum viable surface for SEC-003's documented resolution
  options.
- *Startup-time check in the agent (not doctor).* Rejected: the agent
  starts as a long-lived daemon; a WARN at startup gets lost in logs.
  Doctor is the operator-facing surface; integrating there makes the gap
  discoverable at exactly the moment the operator is already investigating.
- *No doctor check; accept silent zeros.* Rejected: this is SEC-003
  finding's exact failure mode — operators cannot distinguish "no
  failures" from "cannot read journal", and the feature silently
  under-reports on a class of deployments.

**Consequences:**
- One additional external exec in `riot-agent doctor` (`journalctl -n 1`
  plus a read of `/etc/group` via `id -Gn`). Negligible cost, runs once
  per `doctor` invocation.
- New AC (AC-031) — see Section 8.
- `doctor.go` is listed in Section 4 Component Changes below.
- FRD amendment: SEC-003 resolution adds AC-031. FRD is not rewritten;
  the amendment is tracked in Section 12 note 15.

---

## 4. Component Changes

| Action | File Path | Purpose |
|--------|-----------|---------|
| CREATE | `internal/agent/collectors/auth_counter.go` | `authFailureCounter` type with `Add(int)`, `Drain() int`, `MarkReady()`, `IsReady() bool`. Uses `sync.Mutex` or `sync/atomic` — implementer's choice, must be race-free. File-level comment MUST state the serialization invariant per SEC-005: "SecurityCollector MUST be registered after LogsCollector AND `collectAll` MUST serialize the two collectors' Collect calls; parallel execution would produce lost counts." |
| CREATE | `internal/agent/collectors/auth_counter_test.go` | Unit tests for counter semantics (add, drain resets to zero, concurrent Add race-free, MarkReady latch idempotent). |
| CREATE | `internal/agent/collectors/auth_match.go` | Unexported `matchesAuthFailure(raw map[string]interface{}) bool` implementing the origin-filter + content-filter logic in AD-004. Reads `_UID`, `_SYSTEMD_UNIT`, `SYSLOG_IDENTIFIER`, `MESSAGE` from the map. Allow-lists for units/identifiers are package-private constants. |
| CREATE | `internal/agent/collectors/auth_match_test.go` | Table-driven tests covering SEC-001 regression. Required cases: (a) forged `logger "Failed password..."` with `_UID=1000` and non-allow-list `SYSLOG_IDENTIFIER` → NOT counted; (b) real sshd failure (`_UID=0`, `_SYSTEMD_UNIT=ssh.service`, MESSAGE="Failed password...") → counted; (c) real sudo failure (`_UID=0`, `_SYSTEMD_UNIT=sudo.service`, MESSAGE="pam_unix(sudo:auth): authentication failure...") → counted; (d) entry with matching MESSAGE but `_SYSTEMD_UNIT=cron.service` and `SYSLOG_IDENTIFIER=cron` (neither in allow-list) → NOT counted; (e) matching MESSAGE with `_UID=0` but `_SYSTEMD_UNIT` absent and `SYSLOG_IDENTIFIER` absent → NOT counted; (f) four-pattern positive coverage against real sshd-origin entries; (g) empty/missing MESSAGE → NOT counted. |
| MODIFY | `internal/agent/collectors/logs.go` | Add an unexported `authCounter *authFailureCounter` field to `LogsCollector`. Inside the per-line loop in `Collect`, pass the raw JSON map to `matchesAuthFailure(raw)` and `c.authCounter.Add(1)` on match. (Retain the existing `message, _ := raw["MESSAGE"].(string)` read for the `LogEntry` return value; the matcher reads `MESSAGE` from the same map independently.) After the loop completes on a non-empty cursor pass, call `c.authCounter.MarkReady()`. Existing `[]models.LogEntry` return contract unchanged. |
| MODIFY | `internal/agent/collectors/security.go` | Add an unexported `authCounter *authFailureCounter` field to `SecurityCollector`. Inside `Collect`, on Linux: call `n := c.authCounter.Drain()`; if `c.authCounter.IsReady()` set `info.FailedLoginsInterval = intPtr(n)`, else set `info.FailedLoginsInterval = intPtr(0)`. On non-Linux early-return leave `FailedLoginsInterval` nil. |
| MODIFY | `internal/agent/collectors/collector.go` | In `RegisterDefaultsWithDocker`, construct a shared `authFailureCounter` instance, pass it to both `LogsCollector` and `SecurityCollector`, and reorder registrations so `LogsCollector` is registered before `SecurityCollector`. Add a comment block explaining BOTH the ordering constraint (AD-002) AND the serialization invariant (SEC-005): parallelizing `collectAll` would break the counter handoff. |
| MODIFY | `internal/agent/collectors/security_test.go` | New file (does not exist today) or extend if created. Tests that inject a pre-populated counter and assert `FailedLoginsInterval` is set correctly on Linux. On non-Linux (or via `runtime.GOOS` gating), asserts field is nil. |
| MODIFY | `internal/agent/collectors/logs_test.go` | New file (does not exist today) — covers: given a mocked journald JSON line set, the counter is incremented once per matching message that passes the origin filter; forged `logger` entries with non-root `_UID` are NOT counted; counter is **not** incremented on first interval when cursor was zero (MarkReady behavior). If mocking `exec.Command` is too invasive, use a test-only hook that lets the test feed raw JSON into the parse loop — document the hook in the ADD section 12. |
| MODIFY | `internal/agent/doctor.go` | Add a new check (per AD-010) that runs when `logs` or `security` collector is enabled: execute `journalctl --priority=0..6 -n 1 --no-pager` and, if empty, consult `id -Gn` for `systemd-journal` group membership. Warn with remediation text on empty-output + missing-group. Linux-only; skip on other OSes. |
| MODIFY | `internal/models/telemetry.go` | Add `FailedLoginsInterval *int json:"failed_logins_interval,omitempty"` field to `SecurityInfo` struct (after `FailedLogins24h`). |
| MODIFY | `internal/models/events.go` | Add `EventAuthFailure EventType = "auth_failure"` constant. Add `EventDetectorInitialized EventType = "detector_initialized"` constant (used by the synthetic "auth failure detector initialized" info event on agent restart, per SEC-002 deferred resolution; see Section 11). |
| MODIFY | `internal/agent/collectors/collector.go` (or agent startup path) | Emit one synthetic info-level event/log line on agent start: `"auth failure detector initialized — first interval will report zero regardless of prior journal content"`. This is a slog.Info entry in the agent and a server-side info-level event row generated on first telemetry arrival from a device whose prior `FailedLoginsInterval` was nil and current is 0 with `IsReady == false` semantics. Simplest implementation: emit via a one-shot log line in the agent — the server's event timeline shows agent-start via existing heartbeat gaps already; no new wire field required. Detailed mechanism in Section 11. |
| MODIFY | `internal/server/events/templates.go` | Append the `auth_failure` template entry per AD-006, including the SEC-004 operator-facing warning in the Description field. |
| MODIFY | `internal/server/events/generator.go` | In `CheckTelemetryThresholds`, add the `data.Security.FailedLoginsInterval` nil-guarded call to `evaluateMetric` per AD-005, with the `max(0, v)` clamp at dereference (SEC-006). No changes to `evaluateMetric` itself. |
| MODIFY | `internal/server/events/generator_test.go` | Add tests for the new metric: (a) fires when value > threshold and rule matches; (b) does not fire when `FailedLoginsInterval` is nil; (c) respects cooldown; (d) respects device scope; (e) SEC-006 regression — a telemetry payload with `FailedLoginsInterval = intPtr(-5)` is clamped to 0 before evaluateMetric, and a rule with operator `<` threshold `0` does NOT fire. |
| CREATE | `internal/server/events/auth_failure_templates_test.go` | Asserts the `auth_failure` template is present in `AlertTemplates()` with exactly the field values in AD-006, and asserts category `security` appears in the returned list. Pattern follows `gpu_alerts_test.go:TestAlertTemplates_GPUTemplatesPresent`. Description-field assertion includes the SEC-004 warning substring. |
| MODIFY | `web/src/types/models.ts` | Add optional `failed_logins_interval?: number` to `SecurityInfo` TypeScript interface. |
| CREATE | Test in `web/src/pages/settings/AlertRuleSettings.test.tsx` | Add one test asserting that with mocked API templates containing `log_errors`, the TemplatePicker renders the "Log Errors Detected" button (AC-030). |
| MODIFY | `README.md` | Document the new `failed_logins_interval` metric under the alert metrics list (owned by the technical-writer pipeline stage — noted here so senior-dev does not stub it). |

No migration file. No change to `seedDefaultAlertRules`. No change to
`TemplatePicker.tsx` (AD-009). No change to `CreateAlertDialog.tsx`
(unrelated surface).

---

## 5. Data Model Changes

### `internal/models/telemetry.go:SecurityInfo`

**Before:**

```go
type SecurityInfo struct {
    SELinux        string `json:"selinux,omitempty"`
    AppArmor       string `json:"apparmor,omitempty"`
    FirewallStatus string `json:"firewall_status,omitempty"`
    FailedLogins24h int   `json:"failed_logins_24h"`
    LoggedInUsers  int    `json:"logged_in_users"`
    OpenPorts      []int  `json:"open_ports,omitempty"`
}
```

**After:**

```go
type SecurityInfo struct {
    SELinux        string `json:"selinux,omitempty"`
    AppArmor       string `json:"apparmor,omitempty"`
    FirewallStatus string `json:"firewall_status,omitempty"`
    FailedLogins24h int   `json:"failed_logins_24h"`
    // FailedLoginsInterval is the count of authentication-failure log
    // lines observed in journald during the last telemetry interval.
    // *int so non-Linux agents can omit (nil → absent in JSON).
    // A value of 0 is explicitly reported on Linux when no failures
    // were observed or journald was unavailable (fail-open, FR-006).
    FailedLoginsInterval *int `json:"failed_logins_interval,omitempty"`
    LoggedInUsers  int    `json:"logged_in_users"`
    OpenPorts      []int  `json:"open_ports,omitempty"`
}
```

### `internal/models/events.go:EventType`

Additive constants:

```go
EventAuthFailure         EventType = "auth_failure"
EventDetectorInitialized EventType = "detector_initialized"
```

### `web/src/types/models.ts:SecurityInfo`

**Before:**

```ts
export interface SecurityInfo {
  selinux?: string
  apparmor?: string
  firewall_status?: string
  failed_logins_24h: number
  logged_in_users: number
  open_ports?: number[]
}
```

**After:**

```ts
export interface SecurityInfo {
  selinux?: string
  apparmor?: string
  firewall_status?: string
  failed_logins_24h: number
  failed_logins_interval?: number
  logged_in_users: number
  open_ports?: number[]
}
```

### New entity: `authFailureCounter`

Purely in-memory, per-agent-process. No persistence.

```
authFailureCounter {
    n       int             // current count
    ready   bool            // one-way latch, set after first real scan
    mu      sync.Mutex      // guards both n and ready
}
```

**Serialization invariant (SEC-005):** Add, Drain, MarkReady, and IsReady
are mutex-safe individually. The DESIGN of the Drain→IsReady handoff
assumes `LogsCollector.Collect` and `SecurityCollector.Collect` execute
sequentially within a single `collectAll` pass — LogsCollector finishes
(including its post-loop MarkReady call) before SecurityCollector begins
its Drain. Parallel execution of the two collectors would lose counts
without any user-visible error. This invariant is enforced today by
`collectAll`'s sequential iteration; a future parallelization must either
preserve this ordering for these two collectors OR replace the counter
with a different synchronization primitive (e.g., a channel-based
drain-on-barrier). A code comment on the type MUST state this.

### Database

No migration. No schema change. `AlertRule`, `Event`,
`NotificationLog` rows are written using existing columns.

---

## 6. API / Interface Contract

### No new endpoints. No modified endpoints.

Existing endpoints affected only by payload shape:

- `POST /api/v1/telemetry` — Linux agents now include
  `"failed_logins_interval": <int>` inside `"security"`. Non-Linux agents
  omit the key. Server accepts both — `SecurityInfo.FailedLoginsInterval`
  unmarshals as `nil` when the key is absent. Negative values are clamped
  server-side to 0 before rule evaluation (SEC-006).
- `GET /api/v1/settings/alert-templates` — response array gains one entry
  with `id: "auth_failure"`, `category: "security"`. No breaking change to
  existing fields.

### Metric contract

- Metric name (wire & alert rule): `failed_logins_interval`.
- Operators allowed: any operator accepted by `compareValue` in
  `generator.go` (`>`, `<`, `>=`, `<=`, `==`, `!=`).
- Value domain: non-negative integers, serialized as JSON number.
  Server clamps negatives to 0 at ingest before rule evaluation.

### Alert template contract

`GET /api/v1/settings/alert-templates` returns `AlertTemplate` with:

| Field | Value |
|-------|-------|
| id | `auth_failure` |
| name | `Auth Failure` |
| category | `security` |
| metric | `failed_logins_interval` |
| operator | `>` |
| threshold | `0` |
| severity | `warning` |
| cooldown_seconds | `300` |
| needs_target_name | `false` |
| description | `Fires when a device reports one or more authentication failures since the last telemetry interval. Note: on internet-facing SSH hosts, routine bot traffic will trigger this at every cooldown window — raise the threshold and/or narrow IncludeDevices before enabling.` |
| target_state | omitted/empty |

---

## 7. Sequence / Flow

### Per-interval flow (Linux agent)

1. Agent tick fires → `Agent.collectAll(ctx)` iterates registered
   collectors in registration order.
2. `LogsCollector.Collect`:
   - Acquires mutex, reads `since := c.lastSeen`, releases mutex.
   - Runs single `journalctl --since <since> --priority=0..6 -o json
     --no-pager -n 500` exec.
   - On exec error: returns `[]LogEntry{}`, leaves `authCounter` alone
     (existing fail-open behavior covers FR-006 / NFR-007).
   - On success: parses JSON lines into `raw map[string]interface{}`; for
     each line, passes `raw` to `matchesAuthFailure(raw)`. If the origin
     filter (`_UID=0` AND allow-listed `_SYSTEMD_UNIT` / `SYSLOG_IDENTIFIER`)
     AND the content filter pass, `c.authCounter.Add(1)`.
   - Appends parsed entries to `entries` slice; tracks `latest` timestamp.
   - After loop: if `!latest.IsZero()` advances `c.lastSeen = latest +
     1µs`.
   - If the `since` value on entry to this call was **non-zero** (cursor
     existed before this tick), calls `c.authCounter.MarkReady()`. If `since`
     was zero (first-ever tick), does **not** call `MarkReady`.
   - Returns `entries`.
3. `SecurityCollector.Collect`:
   - Builds `info := &SecurityInfo{…}` with existing fields.
   - On `runtime.GOOS != "linux"`: returns `info` with
     `FailedLoginsInterval == nil`.
   - On Linux: runs existing 24h journalctl/grep fallback to populate
     `FailedLogins24h` (unchanged). Then:
     - `n := c.authCounter.Drain()`.
     - If `c.authCounter.IsReady()` { `info.FailedLoginsInterval = &n` }
       else { `zero := 0; info.FailedLoginsInterval = &zero` }
   - Returns `info`.
4. `collectAll` type-switches `info` into `data.Security`.
5. `sendTelemetry` POSTs the snapshot.

### Server-side flow

6. Server ingests snapshot (existing handler).
7. `events.Generator.CheckTelemetryThresholds(ctx, deviceID, hostname,
   &snap.Data)` is called.
8. New block guards on `data.Security != nil && data.Security.FailedLoginsInterval != nil`;
   on pass, dereferences and clamps: `v := *ptr; if v < 0 { v = 0 }`;
   calls `g.evaluateMetric(..., "failed_logins_interval", float64(v), ...,
   EventAuthFailure, msgFn)`.
9. `evaluateMetric` loads enabled rules (cached), finds matching rule
   where `Metric == "failed_logins_interval"`, device is in scope, and
   `compareValue(value, rule.Operator, rule.Threshold)` is true.
10. If matched and not on cooldown: `createEventAndNotify` creates an
    `events` row (type `auth_failure`), broadcasts over websocket, and
    dispatches to each notification channel → one `notification_log` row
    per channel (NFR-006).
11. If no matching rule: `hasRulesForMetric("failed_logins_interval")` is
    checked. If false, the hardcoded-fallback switch in `evaluateMetric`
    has no case for this metric → function returns, no event.

### First-tick flow (including SEC-002 acknowledgement)

- Agent starts, `c.lastSeen.IsZero() == true`.
- LogsCollector's `since` becomes `time.Now().Add(-5 * time.Minute)`
  (preserved for the log panel).
- Matches found in that 5-minute backfill increment the counter.
- LogsCollector does **not** call `MarkReady` because the on-entry `since`
  was zero.
- SecurityCollector calls `Drain` (resets counter to zero for the next
  tick) but `IsReady` returns false → reports
  `FailedLoginsInterval = intPtr(0)`. FR-005 satisfied.
- **SEC-002 deferred acknowledgement.** The agent emits a single
  `slog.Info("auth failure detector initialized", "note", "first interval will report zero")`
  log line on startup, and the server records an info-level
  `detector_initialized` event on first telemetry arrival from a device.
  Operators can see restart windows in the event timeline without changing
  the first-push-zero behavior. This is the documented trade-off (Section
  11); no behavior change beyond the log event.
- Second tick: `since` is non-zero. Normal flow; `MarkReady` is called
  after the scan; next SecurityCollector drain reports the real count.

### Non-Linux flow

- Registry builds on non-Linux hosts as usual (counter is created but
  never populated by LogsCollector because it early-returns on non-Linux).
- SecurityCollector.Collect early-returns before the Drain call.
- `FailedLoginsInterval == nil` → JSON omits the key.
- Server-side nil-guard skips `evaluateMetric` for this device/interval.
- FR-011 / AC-008 satisfied.

---

## 8. Acceptance Criteria Mapping

| AC ID | Fulfilled By | Test Strategy |
|-------|-------------|---------------|
| AC-001 | `SecurityInfo.FailedLoginsInterval` field (AD-003); `evaluateMetric` call in `CheckTelemetryThresholds` (AD-005). | Unit: `security_test.go` asserts field is populated on Linux. Unit in `generator_test.go`: `CheckTelemetryThresholds` with a matching rule fires an event. |
| AC-002 | `matchesAuthFailure` enforces origin + content filters (AD-004); counter aggregation in `LogsCollector` (AD-001). | Unit in `auth_match_test.go`: feed four real-origin sshd/sudo lines, assert all match AND all four content patterns are hit. Unit in `logs_test.go`: feed those four lines via the test hook, call Collect, Drain returns 4. |
| AC-003 | `matchesAuthFailure` returns false on non-matching substrings AND on non-trusted-origin lines (AD-004). | Unit in `auth_match_test.go`: feed `systemd: Started Session 17` with `_UID=0, _SYSTEMD_UNIT=systemd.service`, assert no match (content miss). Feed `logger` forgery case, assert no match (origin miss). |
| AC-004 | LogsCollector does the only journald exec; SecurityCollector drains an in-memory counter (AD-001, AD-002). | Unit in `security_test.go`: assert `SecurityCollector.Collect` does not call `exec.Command("journalctl", ...)` for the per-interval count. Reviewer grep verification: no new journalctl invocation introduced in `security.go`. |
| AC-005 | `MarkReady` latch gates Drain output on first interval (AD-001). | Unit in `auth_counter_test.go`: after `Add(5)` and before `MarkReady`, `IsReady()` is false. Unit in `logs_test.go` + `security_test.go`: first Collect emits `FailedLoginsInterval = 0` even when match lines exist. |
| AC-006 | LogsCollector fails open on exec error; SecurityCollector reports zero when counter is empty (AD-001). | Unit in `logs_test.go` with a stubbed exec returning error: counter is not incremented, LogsCollector returns `[]LogEntry{}`. Combined: SecurityCollector Drain returns 0, reports 0. |
| AC-007 | Counter increments per matching line, no de-duplication (AD-001, AD-004). | Unit in `logs_test.go`: feed 10 identical sshd-origin `Failed password` lines, counter drains to 10. |
| AC-008 | Runtime GOOS check in both collectors; `*int` `omitempty` JSON tag (AD-003, AD-008); server nil-guard (AD-005). | Unit in `security_test.go` gated on `runtime.GOOS`: on non-Linux, `FailedLoginsInterval == nil`. Unit in `generator_test.go`: submit telemetry with nil field; evaluateMetric is not reached; no event fired even with a matching rule. JSON serialization test: `json.Marshal(SecurityInfo{…})` with nil pointer does not contain the key. |
| AC-009 | `FailedLogins24h` code path in `security.go` is not touched (scope rule). | Existing tests remain green; add no-op assertion in `security_test.go` that the 24h value is computed via the pre-existing branch (structural). |
| AC-010 | `SecurityCollector` remains registered under name `security`; no new collector name (AD-001, AD-002). | Unit in `collector_test.go` (or new test): `RegisterDefaults()` followed by `FilterEnabled([]string{"security"})` results in SecurityCollector being present. Verify by grep that no new collector Name() string is introduced. |
| AC-020 | Template entry appended to `AlertTemplates()` (AD-006). | Unit in `auth_failure_templates_test.go` (pattern from `gpu_alerts_test.go:TestAlertTemplates_GPUTemplatesPresent`): assert every field matches AD-006 values, including the SEC-004 warning substring in Description. |
| AC-021 | `AlertRule.IncludeDevices` is empty-string by default when the dashboard creates a rule from the template (existing `emptyRule` in `AlertRuleSettings.tsx:437-454` does not set `include_devices`). | Unit in `AlertRuleSettings.test.tsx`: simulate selecting the template, assert the created rule has `include_devices === ''`. |
| AC-022 | `AlertRule.CooldownSeconds` is the existing column; template selection maps `tpl.cooldown_seconds → rule.cooldown_seconds` in existing code (AlertRuleSettings.tsx:452). | Unit in `AlertRuleSettings.test.tsx`: load template, edit cooldown, assert mutation payload has updated `cooldown_seconds`, no new fields. |
| AC-023 | Template entry sets `Category: "security"` (AD-006); TemplatePicker derives categories from response (AD-009). | Unit in `auth_failure_templates_test.go`: assert `security` appears in `[]string{tpl.Category for tpl in AlertTemplates()}`. Frontend unit in `AlertRuleSettings.test.tsx`: given mocked API with auth_failure entry, TemplatePicker renders a `security` section header. |
| AC-024 | `evaluateMetric` → `createEventAndNotify` path (AD-005). | Integration test in `generator_test.go` with fake repo and fake dispatcher: one snapshot with `FailedLoginsInterval = intPtr(3)`, one rule `metric=failed_logins_interval op=> threshold=0` with one channel attached → exactly one event row, one notification dispatch. |
| AC-025 | Severity is written into `AlertRule.Severity` at creation time; template default is `warning` but UI allows override before save (existing `AlertRuleSettings.tsx` severity edit path). | Unit in `AlertRuleSettings.test.tsx`: load template, override severity to critical, submit, assert persisted severity. |
| AC-030 | Current code already renders `log_errors` in the picker (AD-009). | Unit in `AlertRuleSettings.test.tsx`: with mocked API containing the `log_errors` template, the picker renders a "Log Errors Detected" button under the `system` section. |
| AC-031 *(FRD amendment — SEC-003)* | `doctor.go` journal-read preflight (AD-010). | Unit in `doctor_test.go` (or manual doctor-run verification if the doctor function is hard to unit-test without stdout capture): simulated empty `journalctl` output + user not in `systemd-journal` group → WARN line present with remediation text. Simulated non-empty `journalctl` output → PASS line. Manual smoke: run `riot-agent doctor` on a host where `riot` is not in `systemd-journal` — WARN appears. |

---

## 9. Error Handling

| Failure | Handling | HTTP / Log |
|---------|----------|------------|
| `journalctl` exec error (LogsCollector) | Return `[]LogEntry{}`, leave counter untouched, do not call `MarkReady`. Next SecurityCollector Drain returns 0. Agent continues telemetry submission. Existing slog.Warn in `logs.go` path — ensure the underlying error is logged with `slog.Warn("journalctl exec failed", "error", err)` (FR-006 requires the underlying error to be logged). | No HTTP impact (agent-side). |
| `journalctl` returns empty output, agent lacks `systemd-journal` group (SEC-003) | Silent fail-open at collector; **detected at doctor-run time** via AD-010 preflight. Production telemetry still reports 0. Operator sees the WARN in `riot-agent doctor` output. | No HTTP impact; doctor surfaces it. |
| SecurityInfo missing from snapshot (e.g., collector disabled) | `data.Security == nil` guard in `CheckTelemetryThresholds` skips the evaluateMetric call. | No event, no notification. |
| `FailedLoginsInterval == nil` (non-Linux) | Nil-pointer guard in `CheckTelemetryThresholds` skips evaluateMetric. | No event, no notification. |
| `FailedLoginsInterval < 0` (hostile/buggy agent, SEC-006) | Server clamps to 0 before passing to `evaluateMetric`. Rules with operators `<` or `!=` against threshold 0 therefore cannot be triggered by negative-value injection. | 200 to agent; no event. |
| Server cannot load alert rules | Existing `slog.Error("check…", "error", err)` pattern; evaluateMetric returns without firing. | 200 to agent (telemetry accepted); server logs. |
| Counter overflow | `int` on all supported platforms is at least 32-bit; 2B failures/interval is not a realistic threat. No guard. | N/A |
| Race between Add and Drain | `authFailureCounter` uses a mutex or `sync/atomic`; implementer's choice. Race detector must pass `go test -race` on Linux. Parallel collector execution would lose counts — see SEC-005 invariant in Section 12 note 13. | N/A |

---

## 10. Security Considerations

| Threat | Mitigation |
|--------|------------|
| Compromised agent inflates count to exhaust server notification channel | Cooldown is enforced server-side (rule `CooldownSeconds`) per AD-005; a malicious agent sending `FailedLoginsInterval = 1_000_000` fires at most one notification per cooldown window. Template default is 300s. |
| **Unprivileged local user forges auth-failure lines via `logger(1)` (SEC-001, HIGH)** | **Origin filter in `matchesAuthFailure` (AD-004).** The matcher rejects entries unless `_UID == "0"` AND (`_SYSTEMD_UNIT` OR `SYSLOG_IDENTIFIER`) is in the known-auth-daemon allow-list. Journald derives `_UID` and `_SYSTEMD_UNIT` from kernel-attested peer credentials of the writing process — userspace cannot forge them. A non-root `logger` invocation cannot set `_UID=0`; therefore the forgery does not increment the counter. Unit tests in `auth_match_test.go` cover the forgery regression case directly. |
| Compromised root process on a monitored host forges auth-failure lines | Explicitly out of scope. A root-equivalent attacker on the monitored host has already compromised the device; alert-signal integrity from that device is not defensible by any agent-side filter. Server-side per-rule cooldown still caps the blast radius to one notification per 300s. |
| Silent zero-reporting when agent lacks `systemd-journal` group (SEC-003) | Doctor preflight (AD-010) surfaces the gap at install/diagnose time. Operator gets explicit remediation text. Telemetry still fails open (FR-006) — no behavior regression — but the blindness is detectable. |
| Alert fatigue from `> 0` default on internet-facing SSH hosts (SEC-004) | Template Description (AD-006) carries an explicit operator-facing warning. Operator is responsible for raising the threshold or narrowing IncludeDevices scope before enabling. No silent default-flood. |
| Blind window on agent restart (SEC-002) | First-push-zero is preserved per FR-005. A synthetic info event "auth failure detector initialized" is emitted on agent start (Section 11) so restarts are visible in the event timeline; operators can correlate missed-detection windows against restart events. |
| Negative-value injection (SEC-006) | Server-side clamp `v = max(0, v)` in `CheckTelemetryThresholds` before `evaluateMetric` call. Custom rules with `<` or `!=` operators cannot be spuriously fired. |
| Malicious journald injection (attacker controls real sshd-origin log contents, e.g., via probes with crafted usernames) | Matching is on fixed substrings of the `MESSAGE` field only, AFTER the origin filter. A probe that puts `Failed password` in the log message fires the alert — that is the desired behavior; a real auth failure IS what the metric measures. No regex, no eval, no shell. FR-007 mandates no log content on the wire, so the notification cannot echo attacker-supplied text. |
| Pattern DoS (attacker writes huge journald messages) | `strings.Contains` is O(n) in message length; journald caps message size at ~64KB. The origin filter short-circuits non-trusted entries BEFORE the content scan, so forged oversized messages from a non-root user impose zero scan cost. The existing 500-line cap on `LogsCollector` bounds per-interval work regardless of auth-failure volume. |
| Info disclosure | No new PII in telemetry beyond what `FailedLogins24h` already exposes (a scalar count). Source IPs / usernames are **not** captured — FR-007 mandates no per-IP/user de-dup, i.e. no per-IP/user state. |

---

## 11. Performance Considerations

### NFR-002 — no second journald read

Enforced by AD-001 and AD-002 together. Mechanical verification:
`git grep -n 'journalctl' internal/agent/collectors/` after implementation
must show the same number of invocation sites as before (one in `logs.go`,
one in `security.go` for the 24h read — the pre-existing `FailedLogins24h`
path). The new metric adds **zero** journalctl invocations. The senior-dev
implementation report must include this grep output. (The `doctor.go`
preflight in AD-010 DOES call `journalctl -n 1` — but only from
`riot-agent doctor`, never from the running agent's telemetry hot path, so
it does not affect NFR-002.)

### Per-line cost on Raspberry Pi

- Current LogsCollector per-line cost: JSON unmarshal, map lookups, string
  conversion, slice append. On a Pi 3B+, ~500 lines ≈ 5–10ms.
- Additional per-line cost from AD-001 / AD-004: three map lookups (`_UID`,
  `_SYSTEMD_UNIT`, `SYSLOG_IDENTIFIER`) plus the origin-filter short-circuit
  — for an entry that fails origin, the content scan is skipped entirely,
  so the hot path on Raspberry Pi is DOMINATED by the cheap map lookup.
  For entries that pass origin, four `strings.Contains` calls with early
  exit (SIMD byte-scan, 200-byte message × 4 patterns ≈ <1µs per entry).
  Net: negligible — and in practice FASTER than the original design on
  typical journal streams where most entries are non-auth.
- Zero additional allocations per line (no regex, no slice grows — counter
  lives on a single pre-allocated struct).

### Server-side cost

- One additional `evaluateMetric` call per telemetry snapshot. This hits
  the 5-second TTL rule cache (existing `rulesCache`) so it does not touch
  the DB on hot paths. NFR-003 satisfied.
- No new queries, no new indexes.
- SEC-006 clamp is a single integer compare before the existing
  `evaluateMetric` path — immeasurable.

### Counter contention and serialization invariant (SEC-005)

- `authFailureCounter` is written (Add) inside LogsCollector's per-line
  loop and read (Drain) once per interval by SecurityCollector. Both
  operations are on the same goroutine today — the telemetry ticker runs
  collectors sequentially via `collectAll`.
- The mutex is uncontended today.
- **The serialization of `LogsCollector.Collect` → `SecurityCollector.Collect`
  within `collectAll` is load-bearing for correctness, not just
  performance.** Parallel execution of the two collectors would lose counts
  silently. This is documented in:
  1. A comment block on the `authFailureCounter` type (AD-001 consequences).
  2. A comment block at the registration site in `collector.go`
     (Section 4 Component Changes).
  3. This ADD Section 9 (error handling note).
- Any future PR that parallelizes `collectAll` MUST either preserve the
  LogsCollector-before-SecurityCollector ordering with a barrier, OR
  replace `authFailureCounter` with a different primitive (e.g., a
  channel). The code comments prevent accidental regression.

### Restart-visibility event (SEC-002 deferred)

On agent startup, emit exactly one `slog.Info` log line and one
corresponding server-side info-level event of type
`detector_initialized`:

- Agent-side: `slog.Info("auth failure detector initialized", "note", "first interval reports zero regardless of journal backfill")`
  — written to the agent's stderr/journal once per process lifetime.
- Server-side: on first telemetry arrival from a device (detectable by the
  existing `last_seen_at` tracking in `devices` table), generate one
  `detector_initialized` event row. No notification dispatch (info-level
  events do not page by default in the existing event pipeline).
- Operator impact: restart windows are visible in the event timeline.
  Operators can correlate "detector initialized at 03:12" against "first
  notification at 03:14" to see there is a <60s blind window by design.
- This is additive to the existing behavior. FR-005 first-push-zero is
  preserved per user decision.
- Cost: one slog line per agent start, one event row per device per agent
  restart. Negligible.

---

## 12. Implementation Notes for Engineers

1. **Counter injection pattern.** Construct the counter once in
   `RegisterDefaultsWithDocker`, pass it by pointer. Do *not* expose it as
   a package-level singleton — collector tests must be able to use
   independent counters.

2. **LogsCollector test hook.** The existing `LogsCollector.Collect` is
   hard-coupled to `exec.CommandContext`. To unit-test auth-counter
   increment without a real journald, extract the JSON-line parse loop into
   an unexported helper (e.g., `(c *LogsCollector) parseAndCount(raw
   []byte) []models.LogEntry`) and test *that* helper. The exec path
   becomes a thin wrapper around the helper. This refactor is in scope for
   the senior-dev — it is the minimum surgery needed to make AC-002,
   AC-005, AC-007, and the SEC-001 regression tests unit-testable without
   a second ADD revision.

3. **First-interval gate.** `MarkReady` is called from LogsCollector only
   on intervals where `since` coming in was **non-zero**. Read `since`
   BEFORE running the exec; do not re-check `c.lastSeen` after the exec
   (it's been mutated).

4. **Pointer-to-int helper.** Use the existing pattern
   `func intPtr(v int) *int { return &v }` — there's already one in
   `internal/server/events/gpu_alerts_test.go:37` and
   `internal/agent/collectors/gpu_parse.go` (verify location, do not
   duplicate). If none exists in `collectors/` package scope, add a
   package-private helper in `security.go`.

5. **Do not modify the 24h pattern set.** `FailedLogins24h` continues to
   match `Failed password` and `authentication failure` only (its current
   behavior). BR-001 and NFR-005 mandate this. The SEC-001 origin filter
   applies ONLY to `failed_logins_interval`, not to the legacy 24h path.

6. **Do not add `failed_logins_interval` to the hardcoded-fallback switch**
   in `evaluateMetric`. This metric has no fallback threshold (see AD-005
   rationale).

7. **Do not add a default rule seed.** AD-007 is explicit.

8. **TemplatePicker does not need changes.** AD-009 is explicit — any
   modification to `AlertRuleSettings.tsx` TemplatePicker component during
   this story should be treated as out-of-scope and pushed back.

9. **The `FailedLogins24h` path retains its own `journalctl` call.** This
   is the *24-hour* window — distinct from the per-interval metric.
   NFR-002 refers to a second journald read *for the per-interval count*;
   the 24h read is pre-existing and BR-001-frozen.

10. **Category string is lowercase.** Match the existing casing of
    `service`, `system`, `container`, `gpu`, etc. Use `security` — not
    `Security`.

11. **FRD path-typo.** FRD Section 12 references
    `internal/server/alerts/templates.go`. The correct path is
    `internal/server/events/templates.go`. This is a requirements typo,
    not an architectural disagreement. Ignore the `alerts/` path.

12. **Race detector.** `go test -race ./internal/agent/collectors/...` on
    Linux must pass. The counter is the only new shared mutable state.

13. **SEC-005 serialization invariant on `authFailureCounter`.** Add a
    file-level doc comment on `auth_counter.go` stating:

    > This counter assumes sequential execution of LogsCollector.Collect
    > and SecurityCollector.Collect within a single `collectAll` pass.
    > LogsCollector MUST run first, increment the counter, and call
    > MarkReady before SecurityCollector calls Drain. Any future
    > parallelization of `collectAll` must preserve this ordering
    > (explicit barrier) OR replace this type with a channel-based
    > primitive. Parallel execution would lose counts silently (no
    > error, no test failure under `-race`, just a false-negative alert
    > signal). See docs/security/LOG-001-security-review.md SEC-005.

    Mirror this comment at the registration site in
    `RegisterDefaultsWithDocker` where the counter is wired between
    LogsCollector and SecurityCollector.

14. **SEC-004 deferred (internet-facing SSH noise on `> 0` default).** The
    template Description field carries the operator-facing warning (AD-006).
    Do NOT raise the default threshold in code. Senior-dev does not need
    to do anything beyond ensuring the Description string matches AD-006
    verbatim. Technical writer should surface the same warning in the
    README's alert-templates documentation.

15. **SEC-003 FRD amendment — AC-031.** The FRD as written does not list
    AC-031 ("doctor reports a warning when the agent cannot read the
    system journal"). This ADD revision adds AC-031 as an **FRD amendment**
    mapped to the AD-010 doctor check. The FRD document is NOT rewritten
    for this amendment; the amendment is tracked here and in the
    implementation report. If the BD agent re-opens the FRD for any reason
    later, this amendment should be folded in at that time.

16. **SEC-002 deferred — restart acknowledgement.** The first-push-zero
    behavior is preserved per FR-005 and user decision. The only added
    behavior is a single `slog.Info` line on agent startup and the
    corresponding server-side `detector_initialized` event row. Do NOT
    attempt to narrow the blind window by changing FR-005 semantics.

17. **Origin-filter allow-list constants.** In `auth_match.go`, define
    the allow-lists as package-private string sets (map or slice —
    implementer's choice, favor `map[string]struct{}` for O(1) lookup).
    Do NOT expose them outside the package. LOG-002 will introduce a
    user-configurable catalog; the constant form today is intentional
    scope-limiting.

---

## 13. Definition of Done

- [ ] All component changes in Section 4 implemented.
- [ ] All AC mappings in Section 8 (including AC-031) have corresponding
      named tests with `[AC-xxx]` prefix in the test name (team convention).
- [ ] `go test ./...` passes on Linux and Windows (Windows skips `-race`
      per project convention).
- [ ] `go test -race ./internal/agent/collectors/...` passes on Linux.
- [ ] `cd web && npm run test:run` passes.
- [ ] `make build-server` and `make build-agent-all` succeed.
- [ ] No new lint errors (`go vet ./...`, frontend lint clean).
- [ ] Grep check: exactly one `journalctl` invocation in
      `internal/agent/collectors/logs.go`, exactly one in
      `internal/agent/collectors/security.go` (the pre-existing 24h call),
      and one new invocation in `internal/agent/doctor.go` (AD-010
      preflight — does NOT count against NFR-002 because it is not in the
      telemetry hot path). Include the grep output in the senior-dev
      implementation report.
- [ ] `SecurityInfo.FailedLoginsInterval` JSON serialization test: with
      nil pointer, the key is absent from the marshaled output.
- [ ] SEC-001 regression test present and passing: `logger`-style forged
      entry with non-root `_UID` does NOT increment the counter.
- [ ] SEC-006 regression test present and passing: server-side
      `FailedLoginsInterval = intPtr(-5)` is clamped to 0 and does not
      fire a `<` or `!=` rule.
- [ ] `auth_counter.go` carries the SEC-005 serialization-invariant
      comment; `collector.go` registration site carries the same comment.
- [ ] `riot-agent doctor` manual run on a Linux host without
      `systemd-journal` group membership: WARN present with the documented
      remediation command.
- [ ] Template round-trip: `GET /api/v1/settings/alert-templates` returns
      the new `auth_failure` entry (including the SEC-004 warning in
      Description); manual smoke-test creating a rule from it via the
      dashboard succeeds and persists to the DB.
- [ ] No database migration file created.
- [ ] No new entry in any collector `enabled` whitelist (rider on FR-012).
- [ ] Implementation report at `docs/implementation/LOG-001-impl-report.md`
      written.
