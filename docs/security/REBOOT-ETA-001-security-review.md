# Security Review Report
**Story ID:** REBOOT-ETA-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-15
**Verdict:** APPROVED WITH CONDITIONS

---

## Threat Model Summary

### Assets
- **OS-patch maintenance-window configuration** (mode, start time, frequency,
  cooldown) — operational scheduling metadata. Already admin-readable via
  `GET /api/v1/settings/automation`.
- **Per-device agent `allow_reboot` capability flag** — a non-secret boolean
  describing whether a device permits automatic reboot. Newly surfaced on the
  patch-status response.
- **Business-logic integrity of the auto-reboot dispatch path** — the decision
  to send a reboot-class `os_update` command must remain under server+agent
  control and must not become influenceable by display-only telemetry.

### Threat Actors
- **Authenticated dashboard admin (only reachable actor):** `patch-status` is
  behind `adminAuth`. rIOt has a single admin tier (admin password + optional
  OIDC); there is no low-privilege dashboard role. There is therefore no
  horizontal or vertical privilege-escalation surface among dashboard users.
- **Compromised / spoofing agent:** an agent is trusted to self-report telemetry
  over its per-device API key channel and could lie about `allow_reboot`.
- **Unauthenticated external attacker:** cannot reach the endpoint or the new
  fields at all (no public surface added).

### Attack Surface Introduced
- **No new endpoint, no new route, no new request, no new client input.** The
  story extends the existing `GET /api/v1/fleet/patch-status` response with four
  additive, optional fields and adds one agent-reported telemetry boolean
  (`UpdateInfo.AllowReboot`) riding the existing authenticated telemetry channel.
- One new pure server function (`nextMaintenanceWindowStart`) operating only on
  already-validated stored config plus an injected clock.
- Two frontend render sites consuming server-provided strings via native
  `title=` attributes.

This is a read-only, display-oriented story. The attack surface added is
minimal and sits entirely behind existing authentication.

---

## Findings

### CRITICAL
None.

### HIGH
None.

### MEDIUM
None.

### LOW / INFORMATIONAL

#### SEC-001: Agent-self-reported `allow_reboot` is display-only; confirm it never reaches a control path
**Severity:** INFORMATIONAL
**Domain:** Business Logic Security / Trust Boundary
**Location:** `internal/agent/telemetry.go` (`collectAll` stamp),
`internal/server/handlers/fleet.go` (`PatchStatus`),
`internal/server/handlers/auto_update.go` (`checkAutoPatch`)
**Description:**
The agent self-reports `allow_reboot`. A compromised or spoofing agent could
report `allow_reboot=true` when the real agent config is `false` (or vice versa).
Reviewed against the actual server dispatch logic: `checkAutoPatch`
(`auto_update.go:460-531`) gates the reboot-class `os_update` command solely on
`Device.AutoPatch`, the maintenance window, and
`cfg.OSPatch.RebootClass == "gated"` — it does **not** read the telemetry-reported
`allow_reboot`. The authoritative enforcement remains agent-side in
`internal/agent/commands.go`. In `PatchStatus`, the field is only copied into the
response for the tooltip. The ADD (AD-001, Section 10) explicitly confirms the
field "drives only a DISPLAY tooltip, not any control decision."
**Attack Scenario:**
A compromised agent reports `allow_reboot=true` on a device whose real config is
`false`. Consequence: the operator's dashboard tooltip shows an "Est. reboot"
timestamp that will never actually fire. This is cosmetic misinformation to an
already-authenticated admin on their own dashboard; it causes no reboot, no
command dispatch, and no state change. The inverse (reporting `false`) only
suppresses the timestamp in favor of the honest "manual reboot required" text.
No trust boundary is widened: the agent is already fully trusted for
`reboot_required` and all other telemetry driving this UI.
**Required Resolution (QA verification condition):**
QA must confirm the implemented `PatchStatus` only reads `s.Updates.AllowReboot`
into the response and that no server-side dispatch, gating, or command-creation
path is changed to consume the telemetry-reported `allow_reboot`. No code change
is required if the implementation matches the ADD.
**Blocks:** Does not block implementation.

#### SEC-002: Tooltip rendered via native `title=`; confirm no HTML sink is introduced
**Severity:** INFORMATIONAL
**Domain:** Input Validation / XSS
**Location:** `web/src/pages/FleetOverview.tsx`, `web/src/utils/rebootEta.ts`
**Description:**
The tooltip is delivered through the native `title=` attribute. React escapes
attribute values and `title` is not HTML-parsed by the browser, so it is not an
XSS sink. The interpolated values are not attacker-controlled free text: the
helper consumes only `os_patch_mode` (constrained enum), `next_window_start`
(server-computed RFC3339, rendered via UTC `Date` getters in `formatUtc`),
`os_patch_cooldown_minutes` (server int), and `allow_reboot` (boolean). A
repo-wide check confirmed no `dangerouslySetInnerHTML` in the frontend source
(the only match is an unrelated test file, `web/src/pages/Probes.test.tsx`).
**Attack Scenario:**
No viable injection path. Even if a value were attacker-influenced, `title=`
attribute rendering through React escapes it and the browser does not parse it
as markup.
**Required Resolution (QA verification condition):**
QA must confirm both placements use `title={rebootEtaTooltip(...)}` (native
attribute) and that `rebootEta.ts` does not introduce any HTML/markup rendering.
No code change is required if the implementation matches the ADD.
**Blocks:** Does not block implementation.

#### SEC-003: Information disclosure of maintenance-window timing is confined to the admin surface
**Severity:** INFORMATIONAL
**Domain:** Data Exposure
**Location:** `internal/server/handlers/fleet.go` (`PatchStatus` response),
`internal/server/router.go:276` (route), `router.go:207-208` (adminAuth group)
**Description:**
`os_patch_mode`, `next_window_start`, and `os_patch_cooldown_minutes` are
server-wide scheduling values; `allow_reboot` is a per-device capability boolean.
Knowing when devices reboot is mildly sensitive operationally (a predictable
reboot window is a window of opportunity), but this endpoint is inside the
`adminAuth` JWT-cookie group, and the same window configuration is already
exposed to admins via `GET /api/v1/settings/automation`. No new actor can see
this data; no secret, credential, or PII is exposed. The values are redundantly
repeated on every row (AD-004) — harmless given the trust level.
**Attack Scenario:**
An attacker would already need a valid admin session, at which point the window
config is available to them through existing endpoints. No new exposure.
**Required Resolution:** None. Documented for awareness.
**Blocks:** Does not block implementation.

#### SEC-004: `nextMaintenanceWindowStart` is bounded and panic-free on already-validated input
**Severity:** INFORMATIONAL
**Domain:** Business Logic Security (DoS / integer safety)
**Location:** `internal/server/handlers/auto_update.go` (new function, per ADD
Section 4)
**Description:**
The computation is pure time arithmetic with a bounded loop (`d = 0..366`,
inclusive — 367 iterations worst case) and is computed once per request, not
per device (AD-004). It reuses `validTimeStr` (guards hours 0-23, minutes 0-59)
as a precondition and `parseTimeStr`/`frequencyAllowsDay` verbatim. The only
input beyond the injected clock is the stored `MaintenanceWindow`, whose values
are validated on write by `SetAutomationConfig` (`auto_update.go:91-148`:
mode/time/frequency enumerations, `weekly` requires non-empty `days_of_week`,
cooldown >= 1). `time.Date(...).AddDate(0,0,d)` and the `month+1` last-day
computation are normalized by the Go stdlib (no overflow, no panic). Unsatisfiable
or malformed stored config (e.g. a pre-validation blob with empty `days_of_week`,
or a bad `start_time`) resolves to `(zero, false)` and the honest no-time
fallback (FR-019) — no panic, no unbounded loop, no partial time. The 366-day
horizon soundly covers daily (1 day), weekly (<=7 days) and monthly (<=31 days)
recurrence plus a full leap year of slack.
**Attack Scenario:**
No untrusted input reaches the function. An admin repeatedly hitting
`patch-status` triggers one bounded 367-iteration loop per request, dwarfed by
the existing `GetAllLatestSummaries` call — no meaningful DoS amplification.
**Required Resolution (QA verification condition):**
QA/engineering must confirm the implemented function (a) checks
`w.Mode == "window"` and `validTimeStr(w.StartTime)` before iterating, (b) caps
the loop at the 366-day horizon, and (c) returns `(time.Time{}, false)` on
horizon exhaustion rather than looping or panicking. Pin the "currently in
window" boundary (AD-003 / AC-018) by explicit test.
**Blocks:** Does not block implementation.

---

## Positive Observations
- **Honest-degradation design is a security property.** The tri-state `*bool`
  (AD-001/AD-002) ensures the UI never asserts an automatic reboot the system
  will not perform, and never fabricates a timestamp for a device whose
  permission is unknown. This correctly keeps the display truthful.
- **Correct trust-boundary placement.** Enforcement stays agent-side and on
  server config (`RebootClass == "gated"`); the new telemetry field is purely
  advisory. The design does not let a lying agent cause a reboot.
- **No new attack surface.** No new endpoint, no new client input, no new
  secret, no new dependency, no schema/migration change. Fully additive and
  backward compatible (NFR-005).
- **Clock injection** keeps the only new logic deterministic and testable, and
  confines `time.Now()` to the composition root.
- **Native `title=` only**, honoring the no-tooltip-library constraint and
  avoiding any HTML sink.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.** There are no CRITICAL and no HIGH findings. All
findings are INFORMATIONAL and describe security properties that are correctly
designed in the ADD but cannot be fully verified from the ADD alone — per review
policy they are passed to the engineering team / QA engineer as verification
conditions rather than blockers:

1. **SEC-001** — Verify the implemented `PatchStatus` only reads
   `allow_reboot` into the response and no server-side control/dispatch/gating
   path is changed to consume the telemetry-reported value.
2. **SEC-002** — Verify both placements use the native `title=` attribute via
   `rebootEtaTooltip(...)` and that `rebootEta.ts` introduces no HTML/markup
   rendering (no `dangerouslySetInnerHTML`, no raw HTML).
3. **SEC-004** — Verify `nextMaintenanceWindowStart` guards on
   `mode == "window"` + `validTimeStr`, caps the loop at the 366-day horizon,
   and returns `(zero, false)` on exhaustion (no panic, no unbounded loop);
   pin the in-window boundary test (AC-018).

These conditions are implementation-fidelity checks, not architectural changes.
The ADD does not require revision. Implementation may proceed; the QA engineer
must confirm the three conditions above before sign-off.

**Routing:** Invoke `senior_dev`, passing this security report alongside the ADD.
