# Architecture Decision Document

**Story ID:** REBOOT-ETA-001
**FRD Reference:** docs/requirements/REBOOT-ETA-001-frd.md
**Author:** Architect Agent
**Date:** 2026-10-15
**Status:** DRAFT

---

## 1. Summary

A new pure server-side function `nextMaintenanceWindowStart(window, now)` computes
the next OS-patch maintenance-window start (UTC) beside the existing window helpers
in `auto_update.go`. The existing `GET /api/v1/fleet/patch-status` response is
extended with four additive, backward-compatible per-row fields (OS-patch mode,
computed next window start, cooldown, and the device's agent `allow_reboot`). A
single pure TypeScript helper maps those fields to one tooltip string, reused
byte-for-byte by the existing Pending OS Patches badge and a new small main-grid
reboot indicator. The agent additively reports `allow_reboot` on existing update
telemetry (Option A), chosen because it is a genuine one-line stamp at the
telemetry-assembly point with no collector-framework change.

## 2. Technical Context

- **Reboot-required signal (already plumbed):** `models.UpdateInfo.RebootRequired`
  (`internal/models/telemetry.go:275`) is produced by the agent updates collector,
  carried in `FullTelemetryData`, and surfaced per device by `PatchStatus`
  (`internal/server/handlers/fleet.go:15`) as `reboot_required` on the
  `devicePatchInfo` array. The client type `DevicePatchInfo`
  (`web/src/api/client.ts:17`) already has `reboot_required: boolean`.
- **Placement 1 (exists):** the "reboot required" badge in `PatchReviewModal`
  (`web/src/pages/FleetOverview.tsx:433`) with a static `title`.
- **Placement 2 (missing):** the main grid "Updates" column
  (`FleetOverview.tsx:285-308`) is fed by `patchMap`
  (`FleetOverview.tsx:31-35`), a `Map<device_id, pending_updates:number>` that
  carries counts only, so `reboot_required` is not reachable per row today.
- **Maintenance-window model:** `models.MaintenanceWindow`
  (`internal/models/automation.go`) with the existing helpers
  `inMaintenanceWindow`, `frequencyAllowsDay`, `parseTimeStr`, `validTimeStr`
  (`internal/server/handlers/auto_update.go:186-267`). There is **no**
  next-window-start computation today — this is the core new server logic.
- **The `allow_reboot` gap:** `allow_reboot` is agent YAML only
  (`internal/agent/config.go:32`, `Config.Commands.AllowReboot`). It is consumed
  in `internal/agent/commands.go` and never reported to the server. Critically,
  `internal/agent/telemetry.go:40-64` (`collectAll`) is the assembly point where
  the collector's `*models.UpdateInfo` is attached to `FullTelemetryData` and
  where `a.config` (hence `a.config.Commands.AllowReboot`) is directly in scope.
  This makes Option A a one-line additive stamp (see AD-001).
- **Storage:** telemetry is persisted as a JSON blob; `UpdateInfo` is a nested
  struct, so a new field rides existing storage with **no database migration**.
- **Tooltip convention:** native `title=` throughout; no tooltip library exists
  and none may be added.
- **PR fence:** PR #1 (agent-heal-dpkg: `internal/agent/commands.go`,
  `internal/agent/collectors/updates.go`) and PR #2 (AGENT-HARDEN-001, worktree
  `.claude/worktrees/ecstatic-pike-97d475/`: agent auto-update/signing path) must
  not be modified. The Option A change touches `internal/models/telemetry.go`
  (struct field append) and `internal/agent/telemetry.go` (`collectAll` stamp) —
  neither is an update-collector or auto-update/signing file, and both edits are
  purely additive (see AD-001 consequences).

## 3. Architecture Decisions

### AD-001: Resolve OQ-001 — Option A (agent additively reports `allow_reboot`)

**Decision:** Adopt **Option A**. Add one tri-state field
`AllowReboot *bool json:"allow_reboot,omitempty"` to `models.UpdateInfo` and stamp
it from `a.config.Commands.AllowReboot` at the existing telemetry-assembly point
`collectAll` (`internal/agent/telemetry.go`), in the existing
`case *models.UpdateInfo:` arm. The server surfaces it per patch-status row; the
frontend maps `allow_reboot == false` to a literal "manual reboot required"
tooltip with no timestamp (satisfying FR-017 / AC-012).

**Rationale:** Investigation of the agent (not assumption) showed the collector
(`UpdatesCollector.Collect`) has no access to `Config`, but `collectAll` — which
already attaches the collector's `*models.UpdateInfo` to `FullTelemetryData` — has
`a.config` directly in scope. The change is therefore literally: take the address
of the already-loaded config bool and assign it to the struct. No collector
constructor change, no new registry setter, no new collector-framework plumbing,
no DB migration (telemetry is a JSON blob). Because it is genuinely the smallest
possible change, the product-owner guidance ("smallest change that is still
honest; honor the requirement when it is cheap") favors Option A: it honors FR-017
literally rather than only approximating it.

**Alternatives Considered:**
- **Option B (no agent change):** server infers only from `auto_patch` + reboot-class
  gating and shows a conditional window time. Rejected as the *primary* mechanism
  because (a) it cannot satisfy the literal `allow_reboot == false → manual`
  requirement (FR-017), and (b) it would force the tooltip helper to consume
  `auto_patch`/`reboot_class`, which are **not** present on `DevicePatchInfo` and
  are not available to the Pending OS Patches panel — breaking the
  byte-for-byte-identical-tooltip requirement (FR-005) unless those fields were
  *also* added to the row. Option A needs only the single `allow_reboot` field,
  which is per-device and belongs on the row anyway. Option B's *wording*, however,
  is reused for the honest-degradation of the transitional "unknown" state (see
  AD-002), so nothing from Option B is wasted.
- **Registry-injected collector field (`Registry.SetAllowReboot`) mirroring
  `SetHoldManager`:** rejected — more surface (a new `Registry` method, a new
  `UpdatesCollector` field, a wiring call in `agent.go` `NewAgent`, which is
  adjacent to PR #2's area) for no benefit over the one-line `collectAll` stamp.

**Consequences:**
- `*bool` (not `bool`) is used so the server distinguishes three states:
  `true` (device permits auto reboot), `false` (does not), and absent/`nil`
  (pre-upgrade agent that does not yet report the field). `omitempty` keeps old
  agents' telemetry byte-identical on the wire.
- New agents always stamp a non-nil pointer. Old agents omit the field; the server
  sees `nil` and the frontend degrades honestly (AD-002) — it never fabricates a
  guaranteed timestamp for a device whose permission is unknown (FR-017).
- The two edited files are additive-only: `telemetry.go` gains one struct field at
  the end of `UpdateInfo`; `collectAll` gains two statements inside the existing
  `case *models.UpdateInfo:` arm. This does not conflict with PR #1 / PR #2 files.
- **Agent-collector whitelist note (MEMORY.md):** no new collector is introduced;
  `allow_reboot` rides the existing `updates` collector output, so no agent-config
  whitelist edit is required.

### AD-002: Tri-state tooltip resolution (honest degradation for unknown)

**Decision:** The frontend tooltip helper resolves `allow_reboot` as:
- `=== true` → may present the absolute window time unconditionally.
- `=== false` → "Manual reboot required" wording, **no** time (FR-017 literal).
- `== null`/absent (unknown / pre-upgrade agent) → present the window time only in
  the Option-B-style **conditional** form ("… if this device permits automatic
  reboot"), never as a guarantee.

**Rationale:** FR-017 forbids displaying an automatic-reboot timestamp when the
server cannot establish that the device permits it. A plain `false` would
mislabel unknown devices as "manual required"; a pointer lets the unknown case
degrade to the honest conditional wording, which keeps the feature useful for a
fleet that has not yet upgraded agents while never asserting a guarantee.

**Alternatives Considered:** Non-pointer `bool` (unknown collapses into `false`) —
rejected: positively asserts "manual required" for devices whose permission is
merely unknown, which is less honest and would make the ETA invisible fleet-wide
until every agent upgrades.

**Consequences:** All conditional/true window variants still contain the exact
token `Est. reboot:` followed by the UTC time (FR-007), so AC-006 holds for both.

### AD-003: Currently-in-window resolution (resolve OQ-002)

**Decision:** `nextMaintenanceWindowStart` returns **the earliest eligible window
start instant `S` such that `S >= now` (inclusive lower bound)**. Consequently:
when `now` is strictly inside an active window (i.e. strictly after that day's
start), today's start is `< now` and is skipped, so the result is the **next
eligible occurrence's** start. At the exact instant `now == start`, that start
(`== now`) is returned.

**Rationale:** An ETA that points at a start time already in the past is
misleading. Resolving "already in window" to the next occurrence means the
displayed estimate is always a future instant the server can stand behind. The
rule is a single total order ("earliest eligible start `>= now`"), which is
trivially deterministic and test-pinnable (AC-018), satisfying FR-016.

**Alternatives Considered:** Return the *current* window's (past) start when
in-window — rejected: a past "estimated reboot" instant is confusing and is not an
estimate of a future event.

**Consequences:** AC-018 is pinned by an explicit test: a reference instant
strictly inside the window returns the next eligible day's start; a companion
assertion pins the exact-start boundary (`now == start` → returns `now`).

### AD-004: Extend the existing array response, additively (no new endpoint)

**Decision:** Keep `GET /api/v1/fleet/patch-status` returning a JSON **array** and
add four additive fields to each element. Do **not** introduce a new endpoint and
do **not** wrap the array in an object.

**Rationale:** NFR-001 forbids a new request from the page; NFR-005 forbids a
breaking contract change. The client calls `fetchJSON<DevicePatchInfo[]>` — turning
the top-level array into an object (to hold a single server-wide window block)
would break every existing consumer. Additive per-row fields are backward
compatible. `allow_reboot` is inherently per-device; `os_patch_mode`,
`next_window_start`, and `cooldown_minutes` are server-wide and identical on every
row — harmless redundancy that preserves the array contract and keeps the tooltip
helper fed entirely from a single row object (supporting FR-005).

**Alternatives Considered:** Top-level `{window, devices}` object (breaks array
consumers — rejected); separate `/fleet/os-patch-window` endpoint (violates
NFR-001 — rejected).

**Consequences:** The server computes `nextMaintenanceWindowStart` **once per
request** and stamps the same value onto every row (NFR-002), not once per device.

## 4. Component Changes

| Action | File Path | Purpose |
|--------|-----------|---------|
| MODIFY | `internal/models/telemetry.go` | Add `AllowReboot *bool` to `UpdateInfo` (Option A) |
| MODIFY | `internal/agent/telemetry.go` | Stamp `AllowReboot` from `a.config.Commands.AllowReboot` in `collectAll` |
| MODIFY | `internal/server/handlers/auto_update.go` | Add pure `nextMaintenanceWindowStart` beside existing helpers |
| MODIFY | `internal/server/handlers/fleet.go` | Extend `devicePatchInfo` + populate new fields in `PatchStatus` |
| CREATE | `internal/server/handlers/auto_update_window_test.go` | Go unit tests for `nextMaintenanceWindowStart` (injected clock) |
| MODIFY | `web/src/api/client.ts` | Extend `DevicePatchInfo` with the four new optional fields |
| MODIFY | `web/src/types/models.ts` | Add `allow_reboot?: boolean` to telemetry `UpdateInfo` (line 206) for model parity |
| CREATE | `web/src/utils/rebootEta.ts` | Pure tooltip helper + UTC formatter shared by both placements |
| CREATE | `web/src/utils/rebootEta.test.ts` | Vitest tests for the tooltip helper |
| MODIFY | `web/src/pages/FleetOverview.tsx` | Widen `patchMap`, add grid-row indicator, extend panel badge title |

### Per-file specifics

**`internal/models/telemetry.go` — `UpdateInfo` struct.** Append one field (end of
struct, after `UnattendedUpgrades`):
`AllowReboot *bool json:"allow_reboot,omitempty"` with a comment noting it is the
device agent's `commands.allow_reboot` (REBOOT-ETA-001, Option A); `nil` = not
reported by this agent version.

**`internal/agent/telemetry.go` — `collectAll`.** Inside the existing
`case *models.UpdateInfo:` arm, after `data.Updates = v`, add (guarded `if v != nil`):
take `allow := a.config.Commands.AllowReboot` and set `v.AllowReboot = &allow`.
No other arm or logic changes. (Smallest additive edit; does not touch the updates
collector owned by PR #1 nor the auto-update path owned by PR #2.)

**`internal/server/handlers/auto_update.go` — new function.** Add beside
`frequencyAllowsDay`/`inMaintenanceWindow`:

```
// nextMaintenanceWindowStart returns the earliest UTC instant at which the
// window's start-of-day occurs at or after `now`, honoring the frequency rule.
// ok is false when mode != "window", the start time is invalid, or no eligible
// day is found within the search horizon. `now` is injected for testability
// (no time.Now() here).
func nextMaintenanceWindowStart(w models.MaintenanceWindow, now time.Time) (time.Time, bool)
```

Required semantics (no code here — contract only):
- `w.Mode != "window"` → return `(time.Time{}, false)`. (anytime/disabled handled
  by caller via mode-specific text.)
- `!validTimeStr(w.StartTime)` → return `(time.Time{}, false)` (feeds FR-019 fallback).
- Work in UTC: `ref := now.UTC()`; `startMin := parseTimeStr(w.StartTime)`
  (hour = `startMin/60`, minute = `startMin%60`).
- Iterate candidate days `d = 0..366` inclusive:
  - `day := time.Date(ref.Year, ref.Month, ref.Day, 0,0,0,0, time.UTC).AddDate(0,0,d)`.
  - Skip unless `frequencyAllowsDay(w, day)` — reuse the existing helper; it is
    evaluated on the **start day**, matching the existing overnight start-day
    convention. (Empty/unknown frequency ⇒ daily; `weekly` ⇒ `DaysOfWeek`;
    `monthly` ⇒ last calendar day of month.)
  - `candidate := time.Date(day.Year, day.Month, day.Day, startMin/60, startMin%60, 0,0, time.UTC)`.
  - If `!candidate.Before(ref)` (i.e. `candidate >= ref`) → return `(candidate, true)`.
- Horizon exhausted (e.g. unsatisfiable `weekly` with empty `DaysOfWeek`) → return
  `(time.Time{}, false)`.

Semantics per FR-015 case:
- **Same-day window** (AC-013): end time is irrelevant to the *start* instant; a
  reference earlier than today's start returns today's start (`d == 0`).
- **Overnight window** (AC-014): start is still `startMin` on the eligible start
  day; the end-of-day crossing does not move the start, and `frequencyAllowsDay`
  on the start day preserves the existing convention.
- **Empty/`daily`** (AC-017): every day eligible; after today's start, today's
  candidate `< ref` so `d == 1` (tomorrow) is returned.
- **`weekly`** (AC-015): first `d` whose `day.Weekday()` is in `DaysOfWeek`.
- **`monthly`** (AC-016): first `d` landing on the last calendar day of the month.
- **Currently in window** (AC-018 / AD-003): in-window ⇒ today's candidate `< ref`
  ⇒ skipped ⇒ next eligible occurrence; exact-start ⇒ `candidate == ref` ⇒ returned.

**`internal/server/handlers/fleet.go` — `PatchStatus`.**
- Load automation config once: `cfg := h.loadAutomationConfig(r.Context())`.
- Compute once: `nextStart, nextOK := nextMaintenanceWindowStart(cfg.OSPatch, time.Now().UTC())`
  (composition root; `time.Now()` is acceptable here — the *testable* function
  takes an injected clock, satisfying NFR-003).
- Extend the inline `devicePatchInfo` struct with additive JSON fields (keep
  existing fields unchanged, append at the end):
  - `OSPatchMode string json:"os_patch_mode,omitempty"`
  - `NextWindowStart *time.Time json:"next_window_start,omitempty"`
  - `CooldownMinutes int json:"os_patch_cooldown_minutes,omitempty"`  (note: distinct
    from the existing per-device patch counts; this is the OS-patch window cooldown)
  - `AllowReboot *bool json:"allow_reboot,omitempty"`
- Populate per row (identical in both `detail` and non-detail paths so FR-005
  holds): `OSPatchMode = cfg.OSPatch.Mode`; `CooldownMinutes = cfg.OSPatch.CooldownMinutes`;
  `AllowReboot = s.Updates.AllowReboot`; and `NextWindowStart = &nextStart` only when
  `cfg.OSPatch.Mode == "window" && nextOK` (else leave nil ⇒ omitted ⇒ FR-019 fallback).

**`web/src/api/client.ts` — `DevicePatchInfo`.** Append optional fields:
`os_patch_mode?: 'window' | 'anytime' | 'disabled'`,
`next_window_start?: string` (RFC3339 UTC),
`os_patch_cooldown_minutes?: number`,
`allow_reboot?: boolean`.

**`web/src/types/models.ts` — `UpdateInfo` (line 206).** Add `allow_reboot?: boolean`
(model parity with the Go struct; optional/additive).

**`web/src/utils/rebootEta.ts` — new pure helper.** Export:
- `formatUtc(iso: string): string` — format an ISO instant as an absolute UTC
  string using UTC getters only (e.g. `YYYY-MM-DD HH:MM UTC`); never converts to
  local time (FR-013). Pure (input is a fixed timestamp; no `Date.now()`).
- `rebootEtaTooltip(info): string` where `info` is
  `Pick<DevicePatchInfo, 'os_patch_mode' | 'next_window_start' | 'os_patch_cooldown_minutes' | 'allow_reboot'>`.
  Resolution order (first match wins):
  1. `allow_reboot === false` → `"Manual reboot required — this device does not permit automatic reboot. Reboot it manually to activate the installed updates."` (no time) [FR-017/AC-012]
  2. `os_patch_mode === 'anytime'` → `"Reboot required. Will occur on the next automatic patch run (within the ~<cooldown> min cooldown). Estimate, not a guarantee; depends on reboot-class gating, the cooldown, and the device's allow_reboot / hold_reboot_class settings."` (no clock time) [FR-008/AC-007; caveat FR-012]
  3. `os_patch_mode === 'window'` and `next_window_start` present:
     - `allow_reboot === true` → `"Est. reboot: <formatUtc> — estimate, not a guarantee. Depends on reboot-class gating, the cooldown, and the device's allow_reboot / hold_reboot_class settings."` [FR-007/011/012/013]
     - `allow_reboot == null` (unknown) → `"Est. reboot: <formatUtc> — if this device permits automatic reboot; estimate, not a guarantee. Depends on reboot-class gating, the cooldown, and the device's allow_reboot / hold_reboot_class settings."` [AD-002; keeps `Est. reboot:` token]
  4. `os_patch_mode === 'window'` and `next_window_start` absent → `"A reboot is required to activate the installed updates."` (no time) [FR-019/AC-019]
  5. default (`disabled`/absent mode) → `"A reboot is required to activate the installed updates."` (no time) [FR-009/FR-010]
  The helper assumes the caller renders the indicator only when `reboot_required`
  is true (FR-003); it does not inspect `reboot_required` itself.

**`web/src/pages/FleetOverview.tsx`.**
- Widen `patchMap` (lines 31-35) from `Map<string, number>` to
  `Map<string, DevicePatchInfo>`: `patchStatus?.forEach(p => m.set(p.device_id, p))`.
  `patchMap.size` usages (StatCard, Review button) remain valid.
- Updates column (lines 285-308): replace `const count = patchMap.get(d.id)` with
  `const info = patchMap.get(d.id)`; render count from `info?.pending_updates`
  (preserve existing "-" / cyan behavior). When `info?.reboot_required`, render a
  small reboot indicator (e.g. an amber dot/glyph consistent with existing
  styling) with `title={rebootEtaTooltip(info)}`. Addition only — no existing
  content removed (FR-006).
- Panel badge (line 433-439): change the static `title="A reboot is required…"`
  to `title={rebootEtaTooltip(d)}` on the existing `reboot required` badge; keep
  the badge text/markup otherwise unchanged (AC-022). Both placements call the
  same helper with the same row data ⇒ byte-identical tooltips (FR-005).

## 5. Data Model Changes

No database schema change and no migration — telemetry is stored as a JSON blob;
the new field is nested inside `UpdateInfo`.

**Go `models.UpdateInfo` (before → after):** add one field at the end:
`AllowReboot *bool json:"allow_reboot,omitempty"`.

**Wire contract of `GET /api/v1/fleet/patch-status`:** top-level JSON array
unchanged; each element gains four optional fields (see Section 6). All additions
are `omitempty`/optional, so existing consumers that ignore unknown fields are
unaffected (NFR-005).

## 6. API / Interface Contract

**Endpoint:** `GET /api/v1/fleet/patch-status` (unchanged route, `router.go:276`).
Optional `?detail=true` unchanged.

**Response:** `200 OK`, JSON array. Existing element fields unchanged
(`device_id`, `hostname`, `pending_updates`, `security_count`,
`reboot_class_count`, `reboot_required`, and in detail mode `package_manager`,
`updates`). **Added, additive, per element:**

| Field | Type | Presence | Meaning |
|-------|------|----------|---------|
| `os_patch_mode` | string (`window`/`anytime`/`disabled`) | when non-empty | server-wide OS-patch window mode |
| `next_window_start` | string (RFC3339 UTC) | only when mode `window` and computed | next window start instant |
| `os_patch_cooldown_minutes` | number | when non-zero | OS-patch window cooldown (anytime wording) |
| `allow_reboot` | boolean | only when the device's agent reports it | device permits automatic reboot |

Error response: unchanged (`500 {"error":"failed to get telemetry"}`). No new
status codes, no request/body/header changes.

## 7. Sequence / Flow

1. Agent `collectAll` attaches `*UpdateInfo` to telemetry and stamps
   `AllowReboot = &a.config.Commands.AllowReboot` (new agents) or leaves it unset
   (old agents). Telemetry is pushed and stored as today.
2. Browser loads Fleet Overview; the existing `patch-status` query fires (no new
   request — NFR-001).
3. `PatchStatus` loads automation config, computes `nextMaintenanceWindowStart`
   once, and for each device with pending updates emits the existing row plus the
   four additive fields (`allow_reboot` taken from that device's telemetry).
4. Frontend builds `patchMap: Map<device_id, DevicePatchInfo>`.
5. For each grid row / panel entry with `reboot_required === true`, render the
   indicator with `title = rebootEtaTooltip(row)`.
6. `rebootEtaTooltip` resolves in order: `allow_reboot===false` → manual (no time);
   else `anytime` → cooldown wording; else `window`+start → absolute UTC estimate
   (conditional if `allow_reboot` unknown); else → reboot-required-no-time.

## 8. Acceptance Criteria Mapping

| AC | Fulfilled By | Test Strategy |
|----|--------------|---------------|
| AC-001 | Panel badge render guard `d.reboot_required` (unchanged) | Vitest: badge renders when true |
| AC-002 | New grid indicator gated on `info.reboot_required`; addition only | Vitest: indicator renders; sibling cells intact |
| AC-003 | Render guards in both placements; `patchMap.get` undefined ⇒ none | Vitest: no indicator for false / missing row |
| AC-004 | Both placements call `rebootEtaTooltip(row)` with same data | Vitest: identical `title` string for one device |
| AC-005 | Native `title=` only; no dependency added | Vitest asserts `title` attr; manifest unchanged (review) |
| AC-006 | `rebootEtaTooltip` window branch + `formatUtc` | Vitest: contains `Est. reboot:` + UTC; no local tz |
| AC-007 | `rebootEtaTooltip` anytime branch | Vitest: cooldown wording, no clock time |
| AC-008 | `rebootEtaTooltip` default branch (`disabled`) | Vitest: reboot-required, no time |
| AC-009 | `rebootEtaTooltip` default branch (no mode) | Vitest: reboot-required, no time |
| AC-010 | "estimate, not a guarantee" clause in estimate branches | Vitest: substring present |
| AC-011 | Caveat clause (gating/cooldown/allow_reboot/hold_reboot_class) | Vitest: caveat present on an estimate tooltip |
| AC-012 | `rebootEtaTooltip` `allow_reboot===false` branch; Option A field | Vitest: manual text, no timestamp |
| AC-013 | `nextMaintenanceWindowStart` same-day | Go: injected ref before start ⇒ today start |
| AC-014 | `nextMaintenanceWindowStart` overnight (start-day) | Go: start at startMin on start day |
| AC-015 | `nextMaintenanceWindowStart` weekly | Go: next listed day |
| AC-016 | `nextMaintenanceWindowStart` monthly | Go: last calendar day of month |
| AC-017 | `nextMaintenanceWindowStart` daily after start | Go: tomorrow's start |
| AC-018 | AD-003 "earliest start >= now" rule | Go: in-window ⇒ next occurrence; exact-start ⇒ now |
| AC-019 | `NextWindowStart` nil ⇒ omitted ⇒ helper branch 4 | Go: invalid start ⇒ `ok=false`; Vitest: no-time |
| AC-020 | Reuse existing query; additive fields | Review + Vitest: no new fetch; array contract intact |
| AC-021 | `now time.Time` injected; tests named `[AC-0xx]` | Go: no `time.Now()` in function |
| AC-022 | Panel badge markup preserved; only `title` sourced from helper | Vitest: badge still renders, text extended |

## 9. Error Handling

- **Invalid stored `start_time`:** `nextMaintenanceWindowStart` returns
  `(zero, false)` ⇒ `next_window_start` omitted ⇒ helper branch 4 ⇒
  reboot-required-no-time (FR-019). No panic, no partial time.
- **Unsatisfiable frequency** (e.g. `weekly` with empty `DaysOfWeek`): horizon
  exhausts ⇒ `(zero, false)` ⇒ same honest fallback.
- **Config load failure:** `loadAutomationConfig` already falls back to
  `DefaultAutomationConfig()` (OS-patch `disabled`) ⇒ `os_patch_mode:"disabled"`
  ⇒ no time. Existing logging preserved; no new error shape.
- **Agent does not report `allow_reboot`:** `nil` ⇒ helper conditional wording
  (AD-002); never a guaranteed timestamp (FR-017).
- **Frontend:** helper is total (always returns a string); missing/undefined row
  fields map to the honest no-time branches; no throw.

## 10. Security Considerations

- `allow_reboot` is a non-sensitive boolean capability flag; surfacing it on the
  authenticated patch-status endpoint discloses no secret. It is already an
  operator-visible concept (the panel footer references `commands.allow_patching`).
- No new endpoint, no new auth surface, no new input accepted from the client
  (read-only story). The agent→server field rides the existing authenticated,
  per-device-API-key telemetry channel.
- Tooltip text is static/derived; values are injected into a native `title`
  attribute via React (auto-escaped), so no injection vector is introduced.
- Honesty is itself a safety property here: the tri-state design ensures the UI
  never implies an automatic reboot the system will not actually perform
  (BR-002/BR-004).

## 11. Performance Considerations

- **No new query, no new request:** `PatchStatus` already loads telemetry
  summaries; it now also calls `loadAutomationConfig` once per request (an admin
  config read already performed by other handlers) and computes the window start
  **once** per request (AD-004), not per device. NFR-001/NFR-002.
- `nextMaintenanceWindowStart` is pure arithmetic with a bounded loop (≤ 367
  iterations worst case, typically 0-31), no allocation beyond the returned time,
  no I/O. Negligible added latency.
- Frontend: `patchMap` now stores the row object instead of a number — same entry
  count, same O(n) build. `rebootEtaTooltip` is O(1) string assembly per rendered
  indicator.
- **No new index** is required (no new query); this story adds nothing to ADD-11
  indexing concerns.

## 12. Implementation Notes for Engineers

- Reuse `frequencyAllowsDay`, `parseTimeStr`, `validTimeStr` verbatim — do not
  re-implement frequency logic. Pass UTC `time.Date(...)` values built with
  `time.UTC`.
- The in-window vs exact-start boundary (AD-003) is the subtle case: `candidate >=
  ref` is `!candidate.Before(ref)`. Pin both sub-cases in the AC-018 test.
- Keep `time.Now()` out of `nextMaintenanceWindowStart`; call it only in
  `PatchStatus` (composition root). Go tests inject fixed `time.Date` instants and
  are named `[AC-013]`…`[AC-018]`, `[AC-021]`.
- `*bool`: in `collectAll`, bind to a local (`allow := a.config.Commands.AllowReboot`)
  then take its address — do not take the address of the struct field.
- Do **not** edit `internal/agent/collectors/updates.go` (PR #1) or anything in the
  PR #2 worktree. The only agent edit is the two-statement stamp in
  `internal/agent/telemetry.go`.
- Frontend: no new dependency. Use native `title=`. Format UTC with
  `getUTCFullYear()/getUTCMonth()/...` — never `toLocaleString()` or bare
  `toISOString()`-with-local assumptions.
- Keep the four tooltip estimate-bearing strings' tokens exact: `Est. reboot:`,
  "estimate, not a guarantee", and the caveat list naming `allow_reboot` /
  `hold_reboot_class` (FR-007/011/012).
- Follow repo test placement: Go `_test.go` beside the handler; frontend
  `rebootEta.test.ts` beside `rebootEta.ts` in `web/src/utils/`.

## 13. Definition of Done

- [ ] All Section 4 component changes implemented.
- [ ] `models.UpdateInfo.AllowReboot` added; agent `collectAll` stamps it; no PR
      #1 / PR #2 files touched.
- [ ] `nextMaintenanceWindowStart` implemented with injected clock; all AC-013…
      AC-018 + AC-021 Go tests pass (named with AC refs).
- [ ] `patch-status` response carries the four additive fields in both detail and
      non-detail modes; top-level array contract preserved.
- [ ] `rebootEtaTooltip` + `formatUtc` implemented; Vitest covers AC-004, AC-006…
      AC-012, AC-019, AC-022.
- [ ] Grid indicator + panel badge both use the helper; tooltips byte-identical.
- [ ] `go build ./...` and `go test ./...` green (Windows, no `-race`).
- [ ] `cd web && npm run test:run` green; no new frontend dependency.
- [ ] No new linting errors; no `any`; no `time.Now()` in the pure function.
- [ ] No database migration added (confirm none needed).
```
