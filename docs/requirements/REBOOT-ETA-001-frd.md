# Formal Requirements Document

**Story ID:** REBOOT-ETA-001
**Title:** Estimated reboot-time tooltip for reboot-required devices on Fleet Overview
**Author:** Business Developer Agent
**Date:** 2026-10-15
**Status:** DRAFT

---

## 1. Executive Summary

When a fleet device is pending an OS patch that requires a reboot, the Fleet
Overview page must tell the operator, on hover, when that reboot is estimated to
occur. The estimate is the next occurrence of the configured OS-patch
maintenance window, expressed in UTC. The feature extends the existing
"reboot required" badge in the Pending OS Patches panel and adds a new,
visually small reboot-required indicator to each affected main-grid row, both
carrying the identical hover tooltip. The tooltip must be explicitly labelled an
estimate (not a guarantee), must disclose the factors the estimate depends on,
and must degrade honestly — never displaying a timestamp the server cannot
stand behind.

## 2. Background & Context

rIOt is a self-hosted homelab fleet-monitoring platform (Go server with an
embedded React/TypeScript dashboard). Agents report an OS-update telemetry
signal that already includes `reboot_required` and
`reboot_required_reasons`, and the server already surfaces `reboot_required`
per device through `GET /api/v1/fleet/patch-status`.

Today the operator can see *that* a reboot is required (a badge in the Pending
OS Patches panel) but not *when* it will happen. Automated OS patching, when
enabled, runs only inside a configured maintenance window and may apply
reboot-class packages only under a "gated" reboot-class policy. The operator has
no visibility into the timing implied by that configuration. This story closes
that gap on the Fleet Overview page by computing and displaying the next
maintenance-window start as an estimate.

The maintenance-window model, the reboot-required signal, and the gating logic
already exist. The genuinely new work is (a) a server-side "next window start"
computation and (b) two front-end tooltip surfaces. There is one unresolved
design decision — whether the server can know a device's agent-side
`allow_reboot` flag — which is captured as FR-017 and Open Question OQ-001 and
is routed to the architect.

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Fleet operator (Ryan / dashboard user) | Authenticated dashboard user viewing the Fleet Overview page | Read-only for this story; hovers to read the tooltip. No new mutation. |
| rIOt server | Go server serving the REST API and embedded frontend; computes the next-window-start estimate | Reads automation config and per-device patch status; performs no new write |
| rIOt agent | Go daemon on each device; owns the authoritative `allow_reboot` flag (agent YAML today) | Reports telemetry; may (Option A only, per OQ-001) additionally report `allow_reboot` |

## 4. Functional Requirements

Each requirement uses "must" / "must not" and is independently verifiable.

### Placement & rendering

- **FR-001:** The system must render a reboot-required indicator for a device in
  the Pending OS Patches panel when, and only when, that device's patch-status
  record has `reboot_required == true`.
- **FR-002:** The system must render a reboot-required indicator in the main
  Fleet Overview grid row for a device when, and only when, that device's
  patch-status record has `reboot_required == true`.
- **FR-003:** The system must not render any reboot-required indicator (in
  either placement) for a device whose patch-status record has
  `reboot_required == false` or for which no patch-status record exists.
- **FR-004:** Both placements must expose their reboot-required indicator's
  explanatory text via a native HTML `title` attribute. The system must not
  introduce any third-party tooltip library or dependency.
- **FR-005:** For any given device, the tooltip text shown by the main-grid
  indicator must be byte-for-byte identical to the tooltip text shown by the
  Pending OS Patches panel indicator.
- **FR-006:** The main-grid reboot-required indicator must be an addition only;
  the system must not remove, relabel, or alter any other content, column, or
  control already present in the grid row.

### Tooltip content (conditional on OS-patch maintenance-window state)

- **FR-007:** When the OS-patch maintenance window mode is `window` and a next
  window start can be computed, the tooltip must include the text
  `Est. reboot:` followed by the computed next window start rendered as an
  absolute UTC time.
- **FR-008:** When the OS-patch maintenance window mode is `anytime`, the
  tooltip must describe the reboot as occurring on the next patch run within the
  configured cooldown and must not display an absolute clock time as the
  estimate.
- **FR-009:** When the OS-patch maintenance window mode is `disabled`, the
  tooltip must state that a reboot is required and must not display any
  estimated time.
- **FR-010:** When no OS-patch maintenance window is configured (equivalent to
  the disabled/default state), the tooltip must state that a reboot is required
  and must not display any estimated time.
- **FR-011:** The tooltip must include an explicit indication that the time is an
  estimate and not a guarantee. The tooltip must not present the estimated time
  as a committed or guaranteed reboot time.
- **FR-012:** The tooltip caveat text must state that the estimate depends on
  reboot-class gating, the cooldown, and the device's `allow_reboot` /
  `hold_reboot_class` flags.
- **FR-013:** All times rendered by the tooltip must be in UTC. The system must
  not convert estimated times to any other timezone.

### Next-window-start computation (server-side)

- **FR-014:** The system must provide a server-side computation that, given an
  OS-patch maintenance window configuration and a reference "now" instant,
  returns the next window-start instant as a UTC `time.Time` together with a
  found / not-found result. The reference instant must be injectable (a clock
  seam) so the computation is unit-testable without reading the wall clock.
- **FR-015:** The next-window-start computation must correctly handle each of the
  following cases, returning the earliest window-start instant at or after the
  reference instant that satisfies the window's frequency rule:
  - same-day window (start time of day earlier than or equal to end time of day);
  - overnight window (start time of day later than end time of day), using the
    existing start-day convention (an overnight window is attributed to its
    start day);
  - `frequency` of empty/`daily` (every day eligible);
  - `frequency` of `weekly` (only days listed in `days_of_week`, 0=Sun..6=Sat);
  - `frequency` of `monthly` (only the last calendar day of the month).
- **FR-016:** When the reference instant is already inside an active window, the
  computation must resolve the estimate deterministically to a single defined
  value (either the current window's start or the following eligible
  occurrence) and that choice must be covered by an explicit unit test; it must
  not return an ambiguous or undefined result.

### Honest degradation & the allow_reboot gap

- **FR-017:** The system must not display an automatic-reboot timestamp for a
  device when it cannot establish that the device permits automatic reboot.
  The mechanism by which "the device permits automatic reboot" is determined is
  deferred to the architect per OQ-001, and this FR must be satisfied by
  whichever option (A or B) is chosen:
  - **Option A (agent reports `allow_reboot`):** when the device's reported
    `allow_reboot == false`, the tooltip must read that a manual reboot is
    required and must not display any estimated automatic-reboot time.
  - **Option B (server cannot assert `allow_reboot`):** when automatic patching
    and reboot-class gating are both enabled, the tooltip must present the window
    time conditionally (qualified by the device permitting automatic reboot) and
    must never assert a guaranteed automatic reboot; a distinct manual-reboot
    statement must be shown when automatic patching or gating is off.
- **FR-018:** The system must not display an estimated time in any state not
  explicitly enumerated in FR-007 through FR-010; the honest fallback is the
  reboot-required-with-no-time text (FR-009/FR-010).
- **FR-019:** When the next-window-start computation returns not-found for a
  `window`-mode configuration, the system must fall back to the
  reboot-required-with-no-time text rather than rendering an empty, partial, or
  placeholder time.

## 5. Non-Functional Requirements

- **NFR-001 [Performance]:** The feature must not introduce any additional
  network request from the Fleet Overview page; the main-grid indicator must be
  driven by data already returned by the existing patch-status query.
- **NFR-002 [Performance]:** The next-window-start computation must be a pure,
  in-memory computation (no database or network access) and must complete within
  the existing patch-status request without measurable added latency.
- **NFR-003 [Maintainability]:** The next-window-start computation must have Go
  unit tests, with test names referencing the acceptance criteria they cover,
  and must inject its clock rather than calling `time.Now()` directly in
  testable code.
- **NFR-004 [Dependencies]:** The feature must be implemented with no new runtime
  dependencies in either the Go server or the web frontend.
- **NFR-005 [Compatibility]:** The feature must not alter the response contract
  of `GET /api/v1/fleet/patch-status` in a way that breaks existing consumers;
  any added field must be additive.
- **NFR-006 [Accessibility]:** The reboot-required indicator's meaning must be
  available as text via the `title` attribute (consistent with the existing
  badge convention) so it is not conveyed by color/icon alone.

## 6. Business Rules

- **BR-001:** The estimated reboot time equals the next occurrence of the
  OS-patch maintenance window start; no other schedule source may be used.
- **BR-002:** Automated reboot after a reboot-class patch occurs only when all
  of: the device has auto-patch enabled, the OS-patch reboot-class policy is
  `gated`, and the device's agent `allow_reboot` is true. The tooltip must not
  imply an automatic reboot when any of these is not known to be satisfied.
- **BR-003:** All maintenance-window times are defined and stored in UTC; the
  product has no in-scope timezone-conversion behavior.
- **BR-004:** The estimate is advisory only. It is never a commitment, and the
  tooltip language must reflect that at all times.

## 7. Data Requirements

### Entities involved (read-only for this story unless OQ-001 → Option A)

- **Device patch status** (`devicePatchInfo` server-side /
  `DevicePatchInfo` client-side): includes `device_id`, `hostname`,
  `pending_updates`, `security_count`, `reboot_class_count`, `reboot_required`
  (existing). The main-grid placement requires `reboot_required` to be reachable
  per row (today the grid uses a counts-only map).
- **AutomationConfig.OSPatch** (`MaintenanceWindow`): `mode`
  (`anytime`|`window`|`disabled`), `start_time`/`end_time` (`HH:MM`, UTC),
  `cooldown_minutes`, `stagger_seconds`, `frequency`
  (``|`daily`|`weekly`|`monthly`), `days_of_week` (0=Sun..6=Sat),
  `reboot_class` (``|`off`|`gated`).
- **Device agent `allow_reboot`** (and `hold_reboot_class`): authoritative on
  the agent (agent YAML) today; not known to the server. Whether this becomes a
  reported field is the subject of OQ-001.

### Validation rules (consumed, not re-defined here)

- `mode` is one of `anytime`, `window`, `disabled`.
- `start_time`/`end_time` are `HH:MM` 24-hour UTC strings.
- `frequency` is empty/`daily`/`weekly`/`monthly`; `weekly` requires at least one
  `days_of_week` entry in 0..6; `monthly` means the last calendar day of the
  month.
- These are already enforced by the automation-config write path; this story must
  tolerate any stored value and degrade per FR-018/FR-019 if a value is unusable.

### State → tooltip mapping (decision table)

| Condition (evaluated in order) | Tooltip outcome | FR |
|--------------------------------|-----------------|----|
| `reboot_required == false` or no record | No indicator rendered | FR-003 |
| allow_reboot resolved false (Option A) | "manual reboot required", no time | FR-017 |
| OS-patch mode `window` and next start computed | `Est. reboot: <UTC time>` + caveat + estimate label | FR-007, FR-011, FR-012 |
| OS-patch mode `window` and next start not found | reboot-required, no time | FR-019 |
| OS-patch mode `anytime` | next patch run within cooldown, no clock time | FR-008 |
| OS-patch mode `disabled` or no window configured | reboot-required, no time | FR-009, FR-010 |

### Next-window-start state transitions (conceptual)

| Reference instant relative to window | Result |
|--------------------------------------|--------|
| Before today's eligible start | today's start |
| Inside an active window | deterministic per FR-016 (current start or next occurrence), test-pinned |
| After today's window / today not eligible | next eligible day's start (daily/weekly/monthly rule) |
| Mode not `window` | not-found (caller uses mode-specific text) |

## 8. Acceptance Criteria

Gherkin-style. Each AC maps to the FR(s) it validates.

**AC-001 — Panel badge renders when reboot required [FR-001]**
Given a device whose patch status has `reboot_required == true`
When the operator views the Pending OS Patches panel
Then a reboot-required indicator is shown for that device.

**AC-002 — Main-grid indicator renders when reboot required [FR-002, FR-006]**
Given a device whose patch status has `reboot_required == true`
When the operator views that device's row in the main Fleet Overview grid
Then a small reboot-required indicator is shown in that row
And no other existing content in the row is removed or altered.

**AC-003 — No indicator when reboot not required [FR-003]**
Given a device whose patch status has `reboot_required == false`, and a device with no patch-status record
When the operator views the panel and the main grid
Then no reboot-required indicator is shown for either device in either placement.

**AC-004 — Identical tooltip text in both placements [FR-005]**
Given a single device with `reboot_required == true`
When the operator hovers the panel indicator and the main-grid indicator
Then the `title` text of both indicators is identical character-for-character.

**AC-005 — Native title attribute, no new library [FR-004, NFR-004]**
Given the reboot-required indicators
When the frontend is built
Then the explanatory text is delivered via the native `title` attribute
And no new tooltip dependency is added to the frontend dependency manifest.

**AC-006 — Window mode shows absolute UTC estimate [FR-007, FR-013]**
Given OS-patch mode is `window` with a computable next start
And a device with `reboot_required == true`
When the operator hovers either indicator
Then the tooltip contains `Est. reboot:` followed by the next window start as an absolute UTC time
And the time is not converted to any non-UTC timezone.

**AC-007 — Anytime mode shows cooldown wording, no clock time [FR-008]**
Given OS-patch mode is `anytime`
And a device with `reboot_required == true`
When the operator hovers either indicator
Then the tooltip states the reboot occurs on the next patch run within the cooldown
And the tooltip does not contain an absolute clock time as the estimate.

**AC-008 — Disabled mode shows no time [FR-009]**
Given OS-patch mode is `disabled`
And a device with `reboot_required == true`
When the operator hovers either indicator
Then the tooltip states a reboot is required
And the tooltip contains no estimated time.

**AC-009 — No window configured shows no time [FR-010]**
Given no OS-patch maintenance window is configured (default state)
And a device with `reboot_required == true`
When the operator hovers either indicator
Then the tooltip states a reboot is required
And the tooltip contains no estimated time.

**AC-010 — Estimate-not-guarantee labelling [FR-011]**
Given any tooltip that shows an estimated reboot time
When the operator reads the tooltip
Then the tooltip explicitly indicates the time is an estimate, not a guarantee.

**AC-011 — Caveat discloses dependencies [FR-012]**
Given a tooltip for a reboot-required device
When the operator reads the tooltip
Then the caveat states the estimate depends on reboot-class gating, the cooldown, and the device's `allow_reboot` / `hold_reboot_class` flags.

**AC-012 — allow_reboot=false → manual reboot (per chosen option) [FR-017]**
Given the architect's chosen option for OQ-001
When a device does not permit automatic reboot (Option A: reported `allow_reboot == false`; Option B: auto-patch or gating off)
Then the tooltip states a manual reboot is required
And the tooltip does not present a guaranteed automatic-reboot timestamp.

**AC-013 — Next start: same-day window [FR-014, FR-015]**
Given a `window` config with start time of day earlier than end time of day and daily frequency
And a reference instant earlier the same day than the start
When the next-window-start computation runs with that injected instant
Then it returns that same day's start instant (UTC) with found == true.

**AC-014 — Next start: overnight window [FR-014, FR-015]**
Given a `window` config with start time of day later than end time of day (overnight) and daily frequency
And a reference instant earlier the same day than the start
When the computation runs with that injected instant
Then it returns that day's start instant (UTC), consistent with the start-day convention, with found == true.

**AC-015 — Next start: weekly frequency [FR-014, FR-015]**
Given a `window` config with `frequency == weekly` and specific `days_of_week`
And a reference instant on a non-eligible day
When the computation runs with that injected instant
Then it returns the start instant on the next listed eligible day, with found == true.

**AC-016 — Next start: monthly frequency [FR-014, FR-015]**
Given a `window` config with `frequency == monthly`
And a reference instant earlier in the month than the last day
When the computation runs with that injected instant
Then it returns the start instant on the last calendar day of the month, with found == true.

**AC-017 — Next start: daily frequency after today's window [FR-014, FR-015]**
Given a `window` config with daily frequency
And a reference instant after today's window start
When the computation runs with that injected instant
Then it returns the following day's start instant (UTC), with found == true.

**AC-018 — Next start: currently in window is deterministic [FR-016]**
Given a reference instant that falls inside an active window
When the computation runs with that injected instant
Then it returns the single defined value specified by the implementation (current start or next occurrence)
And that value is pinned by an explicit unit test (not left ambiguous).

**AC-019 — Honest fallback when next start not found [FR-019, FR-018]**
Given OS-patch mode is `window` but the computation returns found == false
And a device with `reboot_required == true`
When the operator hovers either indicator
Then the tooltip shows reboot-required with no time
And no empty, partial, or placeholder time is rendered.

**AC-020 — No additional network request from the page [NFR-001, NFR-005]**
Given the Fleet Overview page using the existing patch-status data
When the main-grid reboot-required indicator is driven
Then no additional network request is issued beyond the existing patch-status query
And the patch-status response contract remains backward compatible (any new field is additive).

**AC-021 — Computation uses an injected clock [NFR-003]**
Given the next-window-start computation under test
When its unit tests run
Then the reference instant is injected (no direct `time.Now()` in the testable path)
And test names reference their corresponding AC IDs.

**AC-022 — Existing badge non-regression [FR-001, FR-006]**
Given the existing Pending OS Patches panel behavior prior to this story
When the feature is implemented
Then the existing reboot-required badge still renders for reboot-required devices
And its prior meaning is preserved (now extended with the estimate text, not replaced or broken).

## 9. Out of Scope

- The per-device detail page and any surface other than the Fleet Overview page.
- Timezone conversion / localization of displayed times (UTC only).
- Introducing any tooltip library or UI component library.
- Changing the maintenance-window model, the gating logic, or the auto-patch
  dispatch logic.
- Triggering, scheduling, rescheduling, or cancelling reboots from the UI.
- Any Docker-update window behavior (OS-patch window only).
- Modifying files or branches owned by PR #1 (agent-heal-dpkg) or PR #2
  (AGENT-HARDEN-001).

## 10. Assumptions

- **A-001:** The reboot-required signal (`reboot_required`) is already plumbed
  agent → server → client and is trustworthy for driving both placements.
  (Pre-answered: verified in grounding brief.)
- **A-002:** The main-grid indicator will be fed by widening the existing
  patch-status data already returned to the page (no new fetch). The exact
  data-shaping mechanism is an implementation detail for the architect, but
  NFR-001 forbids a new request.
- **A-003:** Tooltip copy wording is finalized by the architect/engineering team
  within the constraints of FR-007 through FR-012; this FRD fixes the required
  tokens (`Est. reboot:`, UTC absolute time, estimate-not-guarantee label, the
  caveat dependency list) but not the full prose.
- **A-004:** "Next occurrence of the OS-patch maintenance window" is the sole
  schedule source for the estimate (BR-001).
- **A-005:** The cooldown referenced in the `anytime` wording is the OS-patch
  window `cooldown_minutes`.

## 11. Open Questions

- **OQ-001 (routed to architect — the central design decision):** The server
  does not currently know a device's agent-side `allow_reboot` flag; it is agent
  YAML only and is never reported. The literal requirement
  "`allow_reboot == false` → manual reboot required" (FR-017) cannot be honored
  today without new agent→server plumbing. The architect must choose:
  - **Option A (full):** the agent additively reports `allow_reboot` (e.g. one
    bool on telemetry `UpdateInfo` or a small capabilities object) → stored →
    surfaced on the patch-status row. Honors FR-017 literally. Cost: touches the
    agent collector, the telemetry model, and the fleet handler (additive,
    low-conflict, but must avoid PR #1 / PR #2 files/branches).
  - **Option B (no agent change):** the server cannot assert `allow_reboot`, so
    when auto-patch and gating are both on the tooltip presents the window time
    conditionally ("...if the device permits automatic reboot") and never claims
    a guaranteed auto-reboot; a distinct "manual reboot required" line is shown
    when auto-patch or gating is off. Honest and smaller, but does not satisfy
    the literal `allow_reboot == false` case.
  - **Decision owner:** architect. **Guidance:** choose the smallest change that
    is still honest; the tooltip must never invent a timestamp the server cannot
    stand behind.
- **OQ-002:** When the reference instant is inside an active window (FR-016), the
  architect/engineering team must pin whether the estimate resolves to the
  current window start or the next occurrence. This FRD requires only that the
  choice be deterministic and test-covered (AC-018).

## 12. Dependencies

- Existing `GET /api/v1/fleet/patch-status` handler and its `reboot_required`
  field (server `internal/server/handlers/fleet.go`, client
  `web/src/api/client.ts`).
- Existing `AutomationConfig.OSPatch` maintenance-window model
  (`internal/models/automation.go`) and the existing window helpers
  (`inMaintenanceWindow`, `frequencyAllowsDay`, `parseTimeStr`, `validTimeStr`
  in `internal/server/handlers/auto_update.go`), beside which the new
  next-window-start computation is expected to live.
- Existing Fleet Overview page and the current reboot-required badge in the
  Pending OS Patches panel (`web/src/pages/FleetOverview.tsx`).
- OQ-001's resolution gates FR-017 and AC-012; Option A additionally creates a
  dependency on an additive agent telemetry change (coordinated to avoid PR #1
  and PR #2).
- Architect (ADD) and security-researcher review precede engineering, per the
  project pipeline.
