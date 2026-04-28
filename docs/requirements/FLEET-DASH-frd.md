# Formal Requirements Document

- **Story ID:** FLEET-DASH
- **Title:** Fleet-Wide Metrics Dashboard
- **Author:** Business Developer Agent
- **Date:** 2026-04-27
- **Status:** FINAL

---

## 1. Executive Summary

Add a new top-level route `/dashboard` to the rIOt web UI providing a Grafana/InfluxDB-style at-a-glance view of fleet health. The existing `/` route (the sortable device table in `FleetOverview.tsx`) remains untouched and continues to serve device management workflows. The new dashboard surfaces fleet-wide KPIs, time-series small multiples, a per-device heatmap grid, a container leaderboard, and a live event river — all driven by existing heartbeat/telemetry/event data and the existing WebSocket channel. The intent is to give an operator a single screen that answers "is my fleet healthy right now?" without requiring them to click into individual devices.

## 2. Background & Context

The current landing page (`web/src/pages/FleetOverview.tsx`) is a sortable device table optimized for management tasks (find a device, sort by metric, click in). It is not optimized for at-a-glance fleet health: there is no fleet aggregate, no time-series visualization, no event stream, and no visual signal for "which devices are stressed right now."

rIOt already collects all of the underlying data:
- Heartbeats are pushed at high frequency and retained 7 days (`HeartbeatData` in `internal/models/telemetry.go`).
- Rich telemetry is retained 30 days.
- Events are retained 90 days.
- A WebSocket fan-out exists in `internal/server/websocket/`.
- Reusable chart primitives exist: `Sparkline`, `MetricChart`, `ContainerMetricChart`, `GaugeBar`, `MiniScore`.

This story adds the *presentation layer* on top of data and primitives that already exist. No new collectors, no new retention policy, and no new auth model are introduced.

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Authenticated Operator | Any user logged into the rIOt web UI | Read access to fleet metrics, telemetry, events, and the new dashboard route — same level as `/`, `/probes`, `/security`, `/alerts` |
| Unauthenticated Visitor | A user who has not authenticated | Must be redirected by the existing auth flow; cannot access `/dashboard` |
| Agent | rIOt agent running on a monitored device | No interaction with this feature; supplies the heartbeats and telemetry the dashboard reads |
| Server (rIOt backend) | Go server providing REST + WS | Serves dashboard data and pushes WS updates |

## 4. Functional Requirements

### Routing & Navigation

- **FR-001:** The system must expose a new client-side route at the path `/dashboard`.
- **FR-002:** The system must render a header navigation link labeled "Dashboard" alongside the existing top-level links (Devices, Probes, Security, Alerts, etc.).
- **FR-003:** The system must mark the "Dashboard" header link as active when the user is on `/dashboard`.
- **FR-004:** The system must not modify the existing `/` route or the existing `FleetOverview.tsx` device table.
- **FR-005:** The system must require the same authentication as other authenticated pages; an unauthenticated request to `/dashboard` must follow the existing auth-redirect flow.

### Section 1 — Hero KPI Strip

- **FR-010:** The system must render a row of KPI tiles at the top of the dashboard. The tile set must be:
  1. Devices online / total
  2. Fleet CPU average + peak (last 5 min)
  3. Fleet memory average + peak (last 5 min)
  4. Worst-disk device (gauge of the single highest-utilization root mount across the fleet)
  5. Containers running / total
  6. Active alerts grouped by severity
  7. Pending updates (sum of OS + Docker image updates)
  8. GPU utilization (fleet average), conditionally rendered only when at least one device in the fleet reports GPU telemetry
- **FR-011:** Each KPI tile must display: (a) one primary numeric value, (b) a delta indicator comparing the current value to the value 5 minutes prior, (c) a 60-minute sparkline as a background visual.
- **FR-012:** When a numeric KPI value changes due to a WebSocket update, the displayed number must briefly visually transition (a "flash") to indicate change.
- **FR-013:** The "Worst-disk device" tile must compute "worst" as the single device with the highest root-mount utilization percentage across the fleet at the current moment, and must display the device hostname alongside the gauge.
- **FR-014:** The GPU utilization tile must not render at all (the tile slot must collapse) when no device in the fleet has reported GPU telemetry within the visible time window.
- **FR-015:** Delta indicators must visually distinguish positive, negative, and zero change (color and direction glyph).

### Section 2 — Fleet Small-Multiples Row

- **FR-020:** The system must render a row of four time-series charts covering the last 60 minutes:
  1. CPU% per device (one line per device)
  2. Memory% per device (one line per device)
  3. Network in/out (stacked area, fleet-summed)
  4. Disk I/O read/write (fleet-summed)
- **FR-021:** Per-device charts must render one distinct line per device with a gradient area fill beneath each line.
- **FR-022:** Hovering any line in the per-device charts must visually isolate that line (other lines de-emphasized) and display a tooltip with device hostname and current value.
- **FR-023:** When a device goes offline mid-render, its line must continue to display historical data but must not extend past the device's last reported timestamp.

### Section 3 — Device Heatmap Grid

- **FR-030:** The system must render a grid of compact per-device cards, targeting approximately 8–10 cards per row at desktop widths.
- **FR-031:** Each device card must display: (a) a status dot, (b) device hostname, (c) mini-bars for CPU/memory/disk-root, (d) a 5-minute CPU sparkline, (e) a container-count badge.
- **FR-032:** The status dot must visually pulse each time a heartbeat for that device arrives via WebSocket.
- **FR-033:** Cards for devices with one or more active alerts must display a hairline red border on the card edge.
- **FR-034:** The default sort order of the grid must be by a composite "stress score" descending (most stressed first). The composite score must be defined as:
  ```
  stress = 0.4 * cpu_percent
         + 0.3 * mem_percent
         + 0.2 * disk_root_percent
         + 0.1 * min(load_avg_1m * 25, 100)
  ```
  computed from the most recent heartbeat per device. Devices that are offline must sort to the end of the grid regardless of their last-known score.
- **FR-035:** Clicking a device card must navigate to `/devices/:id` for that device.
- **FR-036:** When the fleet has zero registered devices, the grid must display an empty state with copy explaining no devices have been registered and must not render an empty grid frame.

### Section 4 — Container Leaderboard

- **FR-040:** The system must render a single sortable table containing fleet-wide containers.
- **FR-041:** The table must support sorting by: top by CPU, top by memory, restart-count anomalies (containers with more than 3 restarts in the last hour), and update-available count.
- **FR-042:** The table must display at most the top 10 rows for the active sort.
- **FR-043:** Each row must display: device hostname, container name, stack name, the current value of the active sort metric, and a sparkline of that metric.
- **FR-044:** The table must offer filter chips: "with updates", "restarted recently", and a per-stack filter chip generated from the set of stacks present in the fleet.
- **FR-045:** When the fleet has no containers reported, the table must display an empty state and must not render an empty table frame.

### Section 5 — Activity River

- **FR-050:** The system must render a narrow horizontal strip at the bottom of the dashboard displaying the live event stream from the existing events table.
- **FR-051:** Events must be color-coded by severity, consistent with the existing severity color scheme used elsewhere in the app.
- **FR-052:** New events arriving via WebSocket must fade in from the top of the strip.
- **FR-053:** The strip must display at most 20 events at any time; older events must be dropped from the bottom as new events arrive.
- **FR-054:** The strip must offer filter chips: "alerts", "docker", "agent", "security". Chips must be additive (multi-select); when no chip is selected, all event types must be shown.

### Real-Time Updates

- **FR-060:** The dashboard must subscribe to the existing WebSocket channel using the existing `useDevices` hook (or equivalent existing WS plumbing) without introducing a parallel WS connection.
- **FR-061:** KPI numeric values must update on every WS heartbeat that affects them.
- **FR-062:** Sparklines and time-series charts must update on a fixed batched cadence of 5 seconds rather than on every WS heartbeat, to avoid re-render thrash on large fleets.
- **FR-063:** When the WebSocket disconnects, the dashboard must display a non-blocking banner indicating live updates are paused, and must continue to display the last-known data until the connection is re-established.

### Layout & Responsive Behavior

- **FR-080:** The dashboard layout must be fully designed and polished for desktop viewport widths of 768px and above. At >= 768px the dashboard must render all five sections (KPI strip, small-multiples row, heatmap grid, container leaderboard, activity river) in their full-density form per Sections 1–5.
- **FR-081:** At viewport widths below 768px the dashboard must degrade gracefully by stacking sections vertically and shrinking the heatmap grid and small-multiples charts (including collapsing the heatmap grid toward a single column where necessary). Below 768px, full visual density and polish are not required; readability and absence of broken layout are.
- **FR-082:** The dashboard must not produce horizontal page scroll at any viewport width down to and including 360px. Individual scrollable subcomponents (e.g., a wide table within its own container) are permitted to scroll horizontally inside their own bounds, but the dashboard page itself must not.

### Out-of-Scope Guards

- **FR-070:** The dashboard must not provide a geographic map view.
- **FR-071:** The dashboard must not provide drag-and-drop panel layout customization.
- **FR-072:** The dashboard must not provide drill-down navigation inside the dashboard surface; navigation away from the dashboard must be via existing route links (e.g., device card click navigates to `/devices/:id`).

## 5. Non-Functional Requirements

- **NFR-001:** [Performance] On a fleet of 30 devices over LAN, the dashboard must render its initial above-the-fold content (KPI strip + small multiples) within 2 seconds of route navigation, measured from route mount to first paint of all KPI values.
- **NFR-002:** [Performance] The dashboard must remain interactive (hover, sort, filter) on a fleet of up to 100 devices without dropping below 30 frames per second during chart re-renders, on a baseline desktop browser (Chrome, current stable, mid-tier hardware).
- **NFR-003:** [Performance] The 5-second batched chart re-render cadence must hold even when WS heartbeat traffic exceeds one heartbeat per device per second.
- **NFR-004:** [Availability] The dashboard must function in a degraded but readable state when the WebSocket connection is unavailable: KPIs and charts must show the last successfully fetched values, and a banner must indicate that live updates are paused.
- **NFR-005:** [Compatibility] The dashboard's full design and polish target is desktop viewports of 768px and above. Below 768px the dashboard must remain readable and functional in a degraded form (sections stack vertically, heatmap and charts shrink), but feature density and polish are best-effort. At every viewport width down to and including 360px, the dashboard page must not produce horizontal scroll. Mobile (<768px) is not a polish target for v1.
- **NFR-006:** [Accessibility] All interactive elements (filter chips, sortable headers, device cards) must be keyboard-navigable and must expose accessible names. Color must not be the sole signal for severity or status; an icon or text label must accompany color cues.
- **NFR-007:** [Audit/Logging] No new server-side audit logging is required for read-only dashboard rendering beyond what the existing API endpoints already log.
- **NFR-008:** [Security] The dashboard must enforce the same authentication as other authenticated routes. No new role or permission is introduced.

## 6. Business Rules

- **BR-001:** Sparklines display the last 60 minutes of heartbeat data, sourced from the existing 7-day heartbeat retention window.
- **BR-002:** "Recently restarted" in the container leaderboard means more than 3 container restarts within the trailing 60 minutes.
- **BR-003:** "Worst disk" considers root-mount utilization only. Non-root mounts are out of scope for the v1 KPI tile.
- **BR-004:** The fleet "stress score" formula is fixed for v1 (see FR-034). The formula is a product decision and is not user-configurable in this story.
- **BR-005:** A device is considered "online" using the existing online/offline definition the rest of the application uses (last heartbeat within the existing offline threshold). This story does not introduce a new online/offline definition.
- **BR-006:** "Pending updates" sums OS package updates and Docker image updates per device, then sums across the fleet.
- **BR-007:** "Active alerts" are alerts in an unresolved state per the existing alerts model. This story does not change alert lifecycle semantics.

## 7. Data Requirements

### Entities Read

| Entity | Source | Used By |
|--------|--------|---------|
| Device list + status | existing devices API / `useDevices` | All sections |
| Heartbeat history (last 60m) | existing heartbeat storage (`HeartbeatData`) | KPI sparklines, small multiples, heatmap mini-bars and 5m sparklines |
| Container telemetry | existing telemetry (`internal/models/telemetry.go`) | Container leaderboard, container-count badge |
| Events (last N) | existing events table | Activity river |
| Alerts | existing alerts source | KPI "active alerts", red-hairline-border signal on device cards |
| GPU telemetry presence | existing telemetry | GPU KPI conditional render |

### Entities Created / Updated

This story is read-only at the data layer. No new entities are created, no existing entities are modified.

### Validation Rules

- The dashboard must tolerate devices missing optional telemetry fields (GPU, UPS, hardware) without erroring.
- The dashboard must tolerate heartbeats with stale or zero values; these must render as the literal value, not silently substituted.
- The dashboard must not display data older than the 7-day heartbeat retention window in any sparkline.

### State Transitions

| State | Trigger | Next State | Visible Effect |
|-------|---------|------------|----------------|
| Loading | Route mounted, initial data fetch in flight | Loaded | Skeleton placeholders for KPIs, charts, grid |
| Loaded | Initial fetch completes | Live | Full dashboard rendered |
| Live | WS heartbeat arrives | Live | KPI flash, status-dot pulse on relevant card, 5s batched chart update |
| Live | WS disconnects | Stale | Banner "Live updates paused"; last-known values remain visible |
| Stale | WS reconnects | Live | Banner clears; updates resume |
| Loaded | Fleet has zero devices | Empty | Empty-state copy in heatmap grid; KPIs show 0/0 where applicable |

## 8. Acceptance Criteria

```
AC-001: Dashboard route exists and is reachable [Maps to FR-001, FR-002, FR-003]
  Given an authenticated user is on any page
  When the user clicks the "Dashboard" link in the header
  Then the URL changes to /dashboard
  And the dashboard page renders
  And the "Dashboard" header link is marked active

AC-002: Existing fleet overview is preserved [Maps to FR-004]
  Given an authenticated user
  When the user navigates to /
  Then the existing FleetOverview device table renders unchanged
  And no redirect to /dashboard occurs

AC-003: Unauthenticated access is blocked [Maps to FR-005, NFR-008]
  Given an unauthenticated visitor
  When the visitor navigates to /dashboard
  Then the existing auth-redirect flow is invoked
  And the dashboard does not render

AC-004: KPI strip renders the full tile set [Maps to FR-010, FR-011]
  Given a fleet with at least one online device, at least one container, and at least one alert
  When the dashboard loads
  Then the KPI strip displays tiles for: devices online/total, fleet CPU avg+peak, fleet RAM avg+peak, worst-disk device, containers running/total, active alerts by severity, pending updates
  And each tile shows a primary number, a 5-minute delta, and a 60-minute background sparkline

AC-005: GPU tile is conditional [Maps to FR-014]
  Given a fleet where no device has reported GPU telemetry in the last 60 minutes
  When the dashboard loads
  Then the GPU utilization tile is not rendered
  And no empty placeholder for the GPU tile is visible

AC-006: GPU tile renders when GPUs are present [Maps to FR-010, FR-014]
  Given a fleet with at least one device reporting GPU telemetry
  When the dashboard loads
  Then the GPU utilization tile is rendered alongside the other KPI tiles

AC-007: KPI flash on WS update [Maps to FR-012, FR-061]
  Given the dashboard is rendered and connected via WebSocket
  When a heartbeat arrives that changes a KPI numeric value
  Then the affected KPI tile's number visually transitions briefly
  And the new value is displayed

AC-008: Worst-disk tile reflects worst single device [Maps to FR-013, BR-003]
  Given a fleet where Device A has root disk at 70% and Device B has root disk at 95%
  When the dashboard loads
  Then the worst-disk tile shows 95% and the hostname of Device B

AC-009: Delta indicators show direction [Maps to FR-015]
  Given a KPI value that has increased compared to 5 minutes ago
  When the KPI tile renders
  Then the delta indicator visually indicates an upward / positive change
  And given a decreased value the delta indicates a downward / negative change
  And given an unchanged value the delta indicates zero change

AC-010: Small-multiples row renders all four charts [Maps to FR-020, FR-021]
  Given a fleet with at least one device reporting heartbeats in the last 60 minutes
  When the dashboard loads
  Then the small-multiples row renders four charts: CPU% per device, Memory% per device, Network in/out stacked, Disk I/O read/write
  And per-device charts show one distinct line per device with gradient area fills

AC-011: Hover-to-isolate works on per-device charts [Maps to FR-022]
  Given the CPU% per-device chart is rendered with multiple device lines
  When the user hovers a single line
  Then that line is visually isolated and other lines are de-emphasized
  And a tooltip shows the device hostname and the current value

AC-012: Offline device line truncates [Maps to FR-023]
  Given a device went offline 20 minutes into the 60-minute window
  When the per-device CPU chart renders
  Then that device's line shows data up to its last reported timestamp
  And the line does not extend across the remaining 40 minutes

AC-013: Heatmap card content is complete [Maps to FR-030, FR-031]
  Given a fleet of 12 online devices
  When the dashboard loads
  Then the heatmap grid renders one card per device
  And each card displays: status dot, hostname, CPU/memory/disk mini-bars, 5-minute CPU sparkline, container-count badge

AC-014: Status dot pulses on heartbeat [Maps to FR-032]
  Given a device card is visible
  When a heartbeat for that device arrives via WebSocket
  Then the status dot on that device's card visually pulses

AC-015: Alerting devices show red hairline border [Maps to FR-033]
  Given a device has at least one active alert
  When its card renders in the heatmap grid
  Then the card displays a hairline red border on its edge

AC-016: Default sort is stress score descending [Maps to FR-034, BR-004]
  Given a fleet of devices with varying CPU, memory, disk, and load values
  When the dashboard loads
  Then the heatmap grid is ordered by the stress formula (0.4*cpu + 0.3*mem + 0.2*disk_root + 0.1*min(load_avg_1m*25,100)) descending
  And offline devices appear at the end of the grid regardless of their last-known score

AC-017: Card click navigates to device detail [Maps to FR-035]
  Given the heatmap grid is rendered
  When the user clicks a device card
  Then the application navigates to /devices/:id for that device

AC-018: Empty fleet shows empty state [Maps to FR-036]
  Given a fleet with zero registered devices
  When the dashboard loads
  Then the heatmap grid displays an empty state with explanatory copy
  And no empty grid frame is rendered

AC-019: Container leaderboard supports all sorts [Maps to FR-040, FR-041, FR-042]
  Given a fleet with more than 10 containers across multiple devices
  When the user selects "top by CPU"
  Then the table shows the 10 containers with the highest current CPU
  And switching to "top by memory" re-sorts to the top 10 by memory
  And switching to "restart anomalies" shows containers with more than 3 restarts in the last 60 minutes
  And switching to "update available" shows containers with pending image updates

AC-020: Container leaderboard row content [Maps to FR-043]
  Given the container leaderboard renders any sort
  When a row is displayed
  Then the row shows: device hostname, container name, stack, the current value of the active sort metric, a sparkline of that metric

AC-021: Container leaderboard filters [Maps to FR-044]
  Given the container leaderboard is rendered
  When the user activates the "with updates" chip
  Then only containers with pending image updates are listed
  And activating the "restarted recently" chip filters to containers matching BR-002
  And activating a per-stack chip filters to containers in that stack

AC-022: Container leaderboard empty state [Maps to FR-045]
  Given the fleet reports zero containers
  When the dashboard loads
  Then the leaderboard displays an empty state
  And no empty table frame is rendered

AC-023: Activity river renders events [Maps to FR-050, FR-051]
  Given the events table contains recent events of varying severity
  When the dashboard loads
  Then the activity river renders the most recent events
  And events are visually color-coded by severity

AC-024: New events fade in [Maps to FR-052]
  Given the activity river is rendered
  When a new event arrives via WebSocket
  Then the event appears at the top of the strip with a fade-in transition

AC-025: River caps at 20 [Maps to FR-053]
  Given the activity river contains 20 events
  When a new event arrives
  Then the strip still displays at most 20 events
  And the oldest event is removed

AC-026: River filter chips are additive [Maps to FR-054]
  Given the activity river is rendered with all event types visible
  When the user activates the "alerts" chip and the "docker" chip
  Then only events of type alerts or docker are shown
  And deactivating all chips returns the river to showing all event types

AC-027: Single WS subscription [Maps to FR-060]
  Given the user navigates from /devices to /dashboard
  When the dashboard mounts
  Then no second WebSocket connection is opened
  And the existing connection from useDevices is reused

AC-028: Charts update on 5s batched cadence [Maps to FR-062, NFR-003]
  Given a stream of WS heartbeats arrives at greater than 1 per second
  When the dashboard is rendered
  Then sparkline and time-series chart re-renders occur on a 5-second batched cadence
  And KPI numeric values continue to update on every relevant heartbeat

AC-029: Disconnected state is communicated [Maps to FR-063, NFR-004]
  Given the dashboard is rendered and live
  When the WebSocket connection drops
  Then a non-blocking banner appears indicating live updates are paused
  And the last-known KPI, chart, grid, leaderboard, and river values remain visible
  And when the connection is restored, the banner clears and updates resume

AC-030: Initial render SLO [Maps to NFR-001]
  Given a fleet of 30 devices reachable over LAN
  When the user navigates to /dashboard
  Then the KPI strip and small-multiples row render their values within 2 seconds of route mount

AC-031: Desktop viewport renders full design [Maps to FR-080, NFR-005]
  Given a viewport width of 768px or greater
  When the dashboard renders
  Then all five sections (KPI strip, small multiples, heatmap, leaderboard, activity river) render in their full-density form per Sections 1–5

AC-032: Sub-768px viewport degrades gracefully [Maps to FR-081, NFR-005]
  Given a viewport width below 768px (for example 480px)
  When the dashboard renders
  Then sections stack vertically
  And the heatmap grid shrinks toward a single column as needed
  And small-multiples charts shrink to fit the available width
  And the page remains readable without broken layout

AC-033: No horizontal page scroll at any viewport >= 360px [Maps to FR-082, NFR-005]
  Given a viewport width of 360px, 480px, 768px, 1024px, 1440px, or any width in between
  When the dashboard renders
  Then the dashboard page itself does not produce horizontal scroll
  And any horizontal scrolling is contained within individual subcomponents (such as a wide table inside its own container)

AC-034: Color is not the sole severity signal [Maps to NFR-006]
  Given any element whose state is conveyed by color (severity, status, delta direction)
  When the element renders
  Then a non-color signal (icon, glyph, or text label) is also present

AC-035: Out-of-scope features absent [Maps to FR-070, FR-071, FR-072]
  Given the dashboard is rendered
  Then no geographic map view is present
  And no drag-and-drop layout controls are present
  And no drill-down panels embedded inside the dashboard exist; navigation is via links to existing routes
```

## 9. Out of Scope

- Geographic map view of the fleet.
- Drag-and-drop or user-customizable panel layout.
- Drill-down panels embedded inside the dashboard. Drill-down navigates out to existing routes (e.g., `/devices/:id`).
- A user-configurable time range selector. The dashboard window is fixed at "last 60 minutes" for v1.
- Mobile-phone (<768px viewport) feature parity, polish, and density. Sub-768px must remain readable, vertically stacked, and free of horizontal page scroll, but is not a polish target for v1.
- Changes to the existing `/` route or the existing `FleetOverview.tsx`.
- Changes to data retention, the heartbeat schema, the events schema, the alerts model, or the auth model.
- Server-side aggregation endpoints — flagged for the architect to decide (see Section 11) but not predetermined here.
- Per-user dashboard preferences or saved views.
- Export, snapshot, or share-link functionality.

## 10. Assumptions

The following items were originally proposed defaults. Items marked **CONFIRMED** have been explicitly approved by the user and are now binding requirements; the corresponding FR(s)/AC(s) are authoritative. Items not marked CONFIRMED remain as documented assumptions.

- **A-001 (CONFIRMED):** The dashboard lives at `/dashboard` alongside the existing `/` route. The existing `/` route is unchanged, no redirect is added, and `/` is not relabeled.
- **A-002:** The "Dashboard" link is added to the existing header navigation alongside Devices, Probes, Security, Alerts.
- **A-003:** Authorization is "any authenticated user" — same as the rest of the app. No new role or permission is introduced.
- **A-004:** "Worst disk" considers root mount only. Non-root mounts are deferred.
- **A-005 (CONFIRMED):** Container leaderboard top-N is 10.
- **A-006 (CONFIRMED):** Sparklines and time-series charts re-render on a 5-second batched cadence; KPI numeric values update per WS heartbeat.
- **A-007:** "Restarted recently" means more than 3 container restarts within the trailing 60 minutes.
- **A-008 (CONFIRMED):** "Stress score" formula: `0.4*cpu_percent + 0.3*mem_percent + 0.2*disk_root_percent + 0.1*min(load_avg_1m*25, 100)`. Fixed for v1, not user-configurable.
- **A-009 (CONFIRMED):** Initial-render SLO is <2 seconds on a 30-device LAN fleet, measured from route mount to first paint of KPI values.
- **A-010 (CONFIRMED, refined):** Desktop (>=768px) is the full-design and polish target. Below 768px the dashboard must degrade gracefully by stacking sections vertically and shrinking the heatmap and charts; full feature density and polish are not required below 768px. The dashboard page must not produce horizontal scroll at any viewport width down to and including 360px (individual subcomponents may scroll horizontally inside their own bounds).
- **A-011:** The 60-minute window is fixed for v1. No time-range selector.
- **A-012:** The dashboard reuses the existing WebSocket connection from `useDevices`; no parallel WS connection is opened.
- **A-013:** Empty-fleet, zero-container, no-GPU, and offline-mid-render states all degrade gracefully with explanatory copy or hidden tiles, never with errors or empty frames.
- **A-014:** Existing color schemes for severity and status are reused; no new design tokens are introduced as part of this story.

## 11. Open Questions

These items are explicitly flagged for the architect (not for product) and do not block FRD finalization. The product-level requirement is captured; the implementation choice is the architect's.

- **OQ-001:** Time-series data retrieval strategy — read directly from the existing heartbeat endpoint with `?since=1h` (client aggregates) vs. introduce a new fleet-aggregation endpoint. The product requirement is the SLO in NFR-001 and the cadence in FR-062; the architect chooses the data path.
- **OQ-002:** Container leaderboard data path — pull from existing per-device telemetry endpoints and aggregate client-side, vs. a new fleet-wide containers endpoint. Same SLO/cadence constraints apply.
- **OQ-003:** Whether the "stress score" computation lives client-side (per A-008's fixed formula) or is precomputed server-side. Either is acceptable as long as FR-034 and AC-016 hold.
- **OQ-004:** WS message shape for heatmap pulse — does the existing WS payload already carry enough per-device heartbeat signal for FR-032, or does the architect need to surface an existing event type that is currently filtered out?

## 12. Dependencies

- **D-001:** Existing authentication and route-guard infrastructure for the React app.
- **D-002:** Existing header navigation component (where Devices, Probes, Security, Alerts links live).
- **D-003:** Existing `useDevices` hook (`web/src/hooks/useDevices.ts`) and the WebSocket connection it manages.
- **D-004:** Existing chart primitives: `Sparkline.tsx`, `MetricChart.tsx`, `ContainerMetricChart.tsx`, `GaugeBar.tsx`, `MiniScore.tsx`.
- **D-005:** Existing heartbeat storage and retrieval (last 7 days), telemetry storage (last 30 days), events storage (last 90 days).
- **D-006:** Existing alerts model and the existing online/offline definition.
- **D-007:** Existing severity color scheme and design tokens (Tailwind, `bg-gray-900/60` card pattern, etc.).
- **D-008:** Existing device detail route `/devices/:id` for heatmap card click target.
