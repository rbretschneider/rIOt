# Architecture Decision Document

| Field         | Value                                      |
|---------------|--------------------------------------------|
| Story ID      | FLEET-DASH                                 |
| FRD Reference | docs/requirements/FLEET-DASH-frd.md        |
| Author        | Architect Agent                            |
| Date          | 2026-04-27                                 |
| Status        | FINAL                                      |

---

## 1. Summary

Ship a new `/dashboard` route as a frontend-only addition that reuses the existing
`useDevices` WebSocket subscription, the existing `recharts` chart primitives,
the existing `GET /api/v1/devices`, `GET /api/v1/events`, and a new lightweight
helper endpoint `GET /api/v1/fleet/heartbeats?window=60m` that returns 60 minutes
of heartbeats for every device in a single round trip. Aggregation, stress-score
ranking, and KPI computation happen client-side; the server keeps shipping
heartbeats and telemetry it already produces. No schema changes, no new auth, no
new WS topic.

---

## 2. Technical Context

### What exists

- **Server**:
  - chi v5 router with REST + a single broadcast WS hub at `GET /ws`
    (`internal/server/websocket/hub.go`). `BroadcastHeartbeat`,
    `BroadcastTelemetry`, `BroadcastEvent`, `BroadcastDeviceUpdate`,
    `BroadcastDeviceRemoved`, `BroadcastDockerUpdate` already fire on every
    relevant ingest path.
  - `TelemetryRepository.GetHeartbeatHistory(ctx, deviceID, since)` returns
    decoded heartbeats for one device, backed by index
    `idx_heartbeats_device_time` on `heartbeats(device_id, timestamp DESC)`.
  - `DeviceRepository.List` returns the full device roster used by the existing
    fleet page. Offline detection runs every 30s with a 45s threshold.
  - `EventRepository.ListAll(limit, offset)` already powers `GET /api/v1/events`.
  - Event types are prefixed (`container_*`, `agent_update_*`, `cert_*`,
    `auth_*`, `disk_smart_*`, `service_*`, etc. — see `internal/models/events.go`).
  - Latest telemetry per device is exposed via `TelemetryRepository.GetAllLatestSnapshots`
    (full blobs) and `GetAllLatestSummaries` (lightweight projection — not used
    here because we need Docker.Containers).
  - `stripHeavyTelemetry` already trims big fields out of WS telemetry broadcasts;
    `Docker.Containers` is preserved with CPU%, mem, restart count, labels — exactly
    what the leaderboard needs for live updates.
- **Frontend**:
  - Singleton WS connection in `web/src/contexts/WebSocketProvider.tsx`. All hooks
    subscribe through `useWebSocket(handler)`. No second connection allowed.
  - `useDevices` hook returns `{ data: Device[], wsConnected, ... }` and applies
    heartbeat / telemetry / docker_update / device_update / device_removed
    messages directly to the React Query cache.
  - Chart primitives: `Sparkline.tsx` (recharts LineChart), `MetricChart.tsx`
    (recharts AreaChart with gradient fill), `ContainerMetricChart.tsx`,
    `GaugeBar.tsx`. `recharts` v3 is already a dependency.
  - Tailwind v4 with the project's gray-950 / amber / emerald / red severity
    palette. `Section`, `StatusBadge`, `SeverityBadge` components exist.
  - Routing in `web/src/App.tsx` — `<Routes>` block plus desktop `<NavLink>` list
    and the mobile-menu `<NavLink>` list (these must stay in sync).
- **Models / types**:
  - `HeartbeatData` carries everything needed for stress-score and small-multiples
    rendering: `cpu_percent`, `mem_percent`, `disk_root_percent`, `load_avg_1m`,
    `disk_read_bytes_sec`, `disk_write_bytes_sec`. Network bytes per device come
    from `FullTelemetryData.Network.Interfaces[].BytesSent/BytesRecv` (counter,
    not a rate — must be diffed client-side).
  - `Event.severity` is `'info' | 'warning' | 'critical'`.
  - `WSMessage` envelope: `{ type, device_id?, data? }`.

### What's missing

- No fleet-wide endpoint that returns 60 minutes of heartbeats across all devices
  in one call. The dashboard SLO of <2 s on a 30-device fleet (NFR-001) cannot
  rely on issuing 30 sequential `GET /api/v1/devices/{id}/heartbeats` calls.
- No `/dashboard` route, no Dashboard nav link, no dashboard page or sub-components.
- No client-side stress-score utility, no per-device fleet aggregator, no event
  river component, no heatmap grid, no container leaderboard, no fleet KPI strip.

---

## 3. Architecture Decisions

### AD-001: Time-series read path — bulk fleet heartbeats endpoint, client-side aggregation

**Decision**: Add **one** new server endpoint:
`GET /api/v1/fleet/heartbeats?window=60m` → returns
`{ since: "<RFC3339>", until: "<RFC3339>", devices: { [deviceId]: Heartbeat[] } }`.

The server runs a single query against `heartbeats` filtered by
`timestamp >= now - window` and groups results by device in Go. The handler
caps `window` to 60 minutes (matches BR-001 and prevents abuse). The client
computes every aggregate (KPI averages/peaks, stress score, per-device sparklines,
small-multiples lines, network/disk-IO rollups) in-browser.

**Rationale**:
- The SLO is "<2 s on a 30-device LAN fleet". A 30-device × 60-minute heartbeat
  payload at ~15 s heartbeat cadence is ~7,200 heartbeats × ~150 bytes JSON
  ≈ 1 MB uncompressed (well under 200 KB gzipped). One round trip; one query;
  one index scan. This trivially hits the SLO.
- Going purely client-side (Option A from the FRD) would require 30 parallel
  HTTP calls on first paint. Even with HTTP/2 multiplexing, the chi handler chain
  + DB pool contention pushes the 30-device case toward (and over) the 2 s budget.
  Sequential calls would blow it outright.
- A full pre-aggregation endpoint (Option B per FRD wording) is over-scoped.
  Pre-computing fleet KPIs server-side adds branching that has to be re-derived
  in JS anyway when WS heartbeats arrive (because we update KPIs every heartbeat
  per FR-061). Server-side aggregation buys nothing for cadence — only for
  initial-load network footprint, which is already small.
- All other dashboard data sources reuse existing endpoints:
  - Latest device snapshot list (containers, GPU, disk filesystems, alerts) →
    derived from `GET /api/v1/devices` (already returns `docker_container_count`
    and per-device status) plus existing per-device data already cached by
    `useDevices` via WS telemetry pushes.
  - Container leaderboard data → fetched on dashboard mount via a single new
    helper that walks `GET /api/v1/devices` for hostnames and a single new
    fleet-containers endpoint (see AD-006).
  - Active events (river) → existing `GET /api/v1/events?limit=50`.
  - Pending updates → existing `GET /api/v1/fleet/patch-status`.

**Alternatives Considered**:
- **Option A (pure client-side, parallel per-device fetches)**: Rejected.
  Breaks SLO at 30 devices and definitely breaks at 100. We lose the SLO margin
  with no compensating benefit.
- **Option B (full fleet metric aggregation endpoint)**: Rejected as over-scoped.
  A second endpoint that pre-computes per-time-bucket fleet averages buys nothing
  the client can't do trivially with the raw heartbeats it already has, and would
  need to be invalidated/recomputed on every heartbeat anyway.
- **WebSocket-only initial load (subscribe and wait)**: Rejected — needs a
  multi-second backfill on page mount, blows the SLO.

**Consequences**:
- Time-series rendering scales linearly with device count and heartbeat density.
  Empirically the breaking point of pure client-side rendering with recharts is
  ~150 devices × 240 points (60 min @ 15 s); past that the small-multiples row
  drops below 30 fps. This story targets up to 100 devices (NFR-002) so we are
  comfortably inside that envelope. If the product later requires >150 devices,
  add server-side downsampling (LTTB or 1-minute-bucket averaging) to the same
  endpoint without breaking the contract.
- Heartbeat retention is 7 d, well above the 60-minute window — no risk of
  partial windows.
- The endpoint is read-only and admin-authenticated like the other
  `/api/v1/fleet/*` routes.

### AD-002: WebSocket strategy — reuse the existing single broadcast topic

**Decision**: The dashboard subscribes to the existing `/ws` connection through
`useWebSocket(handler)` and reacts to the existing message types — no new WS
topic, no new payload shape, no parallel connection.

| FRD requirement                                  | Existing WS message  | Handler behavior                                                              |
|--------------------------------------------------|----------------------|-------------------------------------------------------------------------------|
| FR-061 KPI flash on heartbeat                    | `heartbeat`          | KPIs recompute, tile triggers a CSS class transition (200 ms scale/opacity).  |
| FR-032 status-dot pulse on heartbeat             | `heartbeat`          | Card subscribes to a per-device "last pulse timestamp" map; CSS pulse on change. |
| FR-052 new event fade in                         | `event`              | River prepends, enforces 20-cap, applies `enter` CSS class.                   |
| FR-062 batched chart redraw at 5 s               | `heartbeat`          | A single `useDashboardTick` timer drives chart memo invalidation; the per-heartbeat handler only mutates a buffered ring, never re-renders charts directly. |
| FR-063 disconnected banner                       | n/a (uses `connected` from `useWebSocket`) | Banner watches `wsConnected` and renders when false.                          |
| Live container leaderboard updates               | `telemetry`, `docker_update` | Leaderboard re-derives top-10 from the cached telemetry on the same 5 s tick. |

**Rationale**:
- The hub fires one fan-out broadcast per heartbeat already — no need to invent a
  fleet topic. All dashboard clients receive every heartbeat already; the
  bottleneck is *render*, not *delivery*.
- The 5-second batched cadence (FR-062, NFR-003) is a **client** concern: every
  WS heartbeat updates a low-cost buffer ref; a single React effect with a 5 s
  `setInterval` flushes the buffer into state, triggering chart re-renders.
- KPI flashes ride the per-heartbeat path (FR-061) but are a tiny, isolated
  re-render of a numeric tile — they don't touch the charts and won't cause
  thrash.

**Alternatives Considered**:
- **Per-device WS subscription**: Rejected. We don't have one; the hub is a
  broadcast hub, not a topic-router. Inventing one is out of scope and would
  duplicate existing fan-out.
- **A new dashboard-specific aggregated WS frame** (e.g. fleet KPI snapshot every
  N seconds): Rejected. KPIs must update on every heartbeat (FR-061) — server-side
  batching would either add latency or duplicate the existing per-heartbeat send.

**Consequences**:
- Every dashboard tab receives the same heartbeat fan-out as every device-detail
  tab, which is what the existing fleet page already does — no new bandwidth.
- WS message rate stays at the existing ceiling (≤ 1 heartbeat per device per
  second per FR-062 design assumption).

### AD-003: Stress-score computation — client-side

**Decision**: Compute the FR-034 / BR-004 stress score in TypeScript inside a
pure utility (`web/src/utils/stress.ts`) using the latest heartbeat fields the
client already holds (CPU%, mem%, disk_root%, load_avg_1m). Offline devices
short-circuit to `-Infinity` so they sort to the end regardless of last-known
values (AC-016).

**Rationale**:
- The formula is a fixed weighted sum over four fields the heartbeat already
  carries. The cost is negligible (4 multiplies, 1 min, 1 add per device per
  render). Doing it server-side adds an endpoint with no caching benefit because
  the inputs change on every heartbeat anyway.
- Keeps the formula visible to product (one TS file) and easy to test with
  Vitest.

**Alternatives Considered**:
- Server-side stress score (a new field on `Device` or a `/api/v1/fleet/stress`
  endpoint): Rejected. The formula is fixed for v1 (BR-004), so caching is
  pointless; live updates on every heartbeat would force the server to recompute
  per-broadcast anyway.

**Consequences**: One source of truth in `web/src/utils/stress.ts`. Server
remains simpler.

### AD-004: Charting library — reuse `recharts` only

**Decision**: All five sections use `recharts` (already a dependency). The
small-multiples per-device CPU/Memory charts use a single
`<AreaChart>` with one `<Area>` per device, gradient defs reused via stable
`linearGradient` IDs derived from device ID hashes. Hover-to-isolate (FR-022)
is implemented by tracking `activeDeviceId` in component state and
opacity-dimming non-active `<Area>` lines.

**Rationale**:
- `recharts` already powers `MetricChart` and `ContainerMetricChart`. Adding
  visx/d3/echarts would add ~100–200 KB to the bundle and force the team to
  learn a second charting model for marginal gain.
- Hover-to-isolate is straightforward with `recharts` `onMouseMove` /
  `activePayload`. The "gradient area fill beneath each line" (FR-021) is the
  same pattern `MetricChart` already uses.

**Alternatives Considered**:
- visx, echarts, d3 directly: Rejected — bundle cost and inconsistency with
  existing chart code.
- A light SVG-only sparkline path (no recharts on the small-multiples row to
  squeeze fps): Considered, but recharts at 100 devices × 240 points performs
  fine in current Chrome; not worth the divergence from `MetricChart`.

**Consequences**: One chart engine across the app. New dashboard work cannot
introduce non-recharts visualization without an ADD addendum.

### AD-005: "Worst disk" tile — root mount only, max-of-max

**Decision**: The tile reads `device.last_heartbeat`'s `disk_root_percent`
(already on `HeartbeatData`) for every online device, picks the device with the
highest value, and renders `device.hostname` + a `GaugeBar` with the value.
Offline devices are excluded. Per BR-003 / A-004, only the root mount is
considered for v1 — non-root filesystems on `FullTelemetryData.disks.filesystems`
are intentionally ignored.

**Rationale**:
- `disk_root_percent` is already in every heartbeat — no extra fetch needed,
  no extra computation. Computing across non-root mounts would require pulling
  full telemetry for every device on dashboard load, which adds latency and
  conflicts with BR-003.

**Alternatives Considered**:
- All-mount worst disk (using `FullTelemetryData.disks.filesystems`): Rejected
  per BR-003. Out of v1 scope.

**Consequences**: A device with a 99%-full data drive but a 30%-full root will
not surface in this tile. That is the documented v1 behavior. A future story
can extend the heartbeat with a `worst_mount_percent` field if non-root mounts
need representation.

### AD-006: Container leaderboard data path — one new fleet-containers endpoint

**Decision**: Add `GET /api/v1/fleet/containers` returning a flattened array
of `{ device_id, hostname, container_name, image, stack, state, cpu_percent,
mem_usage, mem_limit, restart_count, update_available }` derived from
`telemetry.GetAllLatestSnapshots()` server-side. Stack name is read from the
Docker label `com.docker.compose.project`.

For sparkline data per row (FR-043), the leaderboard uses the existing
`GET /api/v1/devices/{id}/containers/{cname}/metrics?hours=1` endpoint, called
**only for the visible top-10 rows**, on demand when the row mounts. Restart
anomalies (FR-041, "more than 3 restarts in the last hour") are computed from
the `restart_count` field on the latest telemetry — not from a 1-hour delta —
because the existing data does not store historical restart counts. This is a
documented v1 simplification: "more than 3 restarts ever observed by the agent
since container creation, with current reset on restart_policy". See
"Implementation Notes" Section 12 for the precise semantics.

**Rationale**:
- The existing `GET /api/v1/devices/{id}/containers` returns containers for one
  device. Calling it 30× to build a fleet leaderboard repeats the multi-snapshot
  decode on the server. A single fleet endpoint reads the same `latest_snapshots`
  query that `GetAllLatestSummaries` already optimizes — and returns a slim
  projection.
- Sparklines per row are ≤10 series at any time (FR-042). Fetching 10 per-row
  hour-windows is cheap. Doing it lazily on row mount means a sort-switch only
  refetches sparklines that change visibility.

**Alternatives Considered**:
- Embed sparkline data in `GET /api/v1/fleet/containers`: Rejected. Returns 60
  minutes of metrics × every container in the fleet × every device — many MB
  of payload for data the user only needs for ten visible rows.
- Walk per-device endpoints client-side (no new endpoint): Rejected. Same SLO
  argument as AD-001.

**Consequences**: One new server route, one new repository method
(`ContainerMetricRepository.GetFleetLatestContainers` is **not** added — the
handler reuses `TelemetryRepository.GetAllLatestSnapshots` and walks the
in-memory snapshots; no DB-layer change). The "restart anomaly" semantic is a
v1 approximation — flagged for a future story to add proper 1-hour restart
deltas.

> **Superseded in part by Addendum AD-011 (SEC-001).** The handler no longer
> calls `GetAllLatestSnapshots`; it calls a new projected method
> `GetFleetContainerLeaderboard` that reads only container-shaped JSONB out of
> the latest snapshot per device. The endpoint contract and projection shape in
> this AD-006 are unchanged. See the addendum at the end of this document.

### AD-007: Activity river data path — existing events endpoint + WS event push

**Decision**: On mount, the river fetches `GET /api/v1/events?limit=50` for an
initial window. Live events arrive via the existing `event` WS message type
(handled by `GlobalWSHandler` in `App.tsx`, which already prepends to
`['events']` query data). The river component reads from the React Query cache
and applies its 20-event cap and chip filtering as a derived selector.

**Rationale**:
- Eliminates duplicate state. The alerts bell already pushes events into the
  same cache; the river reads from the same cache; no double-fetch.
- Filter chips (FR-054) map to event-type prefixes:
  - "alerts": severity `warning` or `critical`
  - "docker": type starts with `container_`
  - "agent": type starts with `agent_`
  - "security": type in {`auth_failure`, `cert_expiring`, `cert_expired`,
    `disk_smart_failing`, `disk_smart_temp`}
  See `internal/models/events.go` for the full type list.

**Alternatives Considered**:
- A new fleet-river endpoint with built-in chip filtering: Rejected. Filtering
  20 events client-side is free; the existing endpoint is sufficient.

**Consequences**: River component is purely a read+filter view over an
already-maintained cache.

### AD-008: GPU tile conditional render — derived from latest device list

**Decision**: The KPI strip reads `device.has_gpu_telemetry` from a new
boolean derived client-side in the `useFleetMetrics` hook by inspecting whether
any device's cached `latest_telemetry.data.gpu_telemetry?.gpus.length > 0`. The
GPU tile renders only if that flag is true; otherwise it's not in the JSX
output (no empty placeholder).

**Rationale**:
- Latest telemetry is already in the React Query cache for any device the user
  has visited and is updated by WS telemetry pushes. For devices the user has
  not visited, `useFleetMetrics` will issue a single batch fetch on dashboard
  mount via `GET /api/v1/devices/{id}` *only if a cheaper signal is missing*.

**Decision refinement**: Rather than 30 per-device fetches on mount, expose a
new minimal helper in the same `GET /api/v1/fleet/heartbeats` response — add a
`devices_with_gpu: string[]` field listing device IDs whose latest telemetry
includes GPU data. The server computes this from `GetAllLatestSummaries`
extended to also project `data->'gpu_telemetry'->'gpus'` length. This costs one
additional JSONB projection in the existing query.

**Rationale (refinement)**: Avoids a fan-out of per-device fetches purely to
decide whether one tile renders. Keeps the SLO budget intact.

**Alternatives Considered**:
- Server pushes a "fleet has GPU" boolean on the WS: Rejected — bespoke message
  type for one tile.
- Always render the tile, show a placeholder when empty: Rejected — explicitly
  forbidden by FR-014 / AC-005.

**Consequences**: The `/api/v1/fleet/heartbeats` response gains a small
sibling field. `TelemetryRepository.GetAllLatestSummaries` is extended to also
project the GPU presence list (see Section 5).

### AD-009: Disconnected banner

**Decision**: A dashboard-local component reads `wsConnected` from `useDevices`
(which already exposes it). When false, render a single non-blocking banner
above the KPI strip. Last-known values continue to render because React Query
keeps cached data on disconnect — no special handling required (the existing
`useDevices` pattern proves this works).

**Rationale**: Reuses the existing connection-state signal; no new state.

**Alternatives Considered**: None.

**Consequences**: None.

### AD-010: Layout, responsive breakpoints, no horizontal page scroll

**Decision**: Use Tailwind responsive utilities. KPI strip uses
`grid grid-cols-2 sm:grid-cols-4 xl:grid-cols-8 gap-3`. Small-multiples row uses
`grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4`. Heatmap uses
`grid grid-cols-2 sm:grid-cols-4 md:grid-cols-6 lg:grid-cols-8 xl:grid-cols-10
gap-2`. Container leaderboard table is wrapped in
`overflow-x-auto scrollbar-thin` (per CLAUDE.md memory rule) so horizontal
scroll stays inside the leaderboard, not the page.

**Rationale**: Matches FR-080/081/082 directly. The heatmap target of "8–10
cards per row at desktop" is `xl:grid-cols-10`. Below 768 px we hit
`grid-cols-2`, satisfying graceful-degrade. Page-level horizontal scroll is
prevented by Tailwind's default `box-border` plus the page wrapper's existing
`max-w-7xl mx-auto px-4` which already clamps width at every viewport.

**Alternatives Considered**: A CSS grid auto-fit-minmax approach. Rejected —
less predictable card count per row.

**Consequences**: None — this is the project's standard responsive pattern.

---

## 4. Component Changes

### Backend (Go)

| Action | File Path                                                     | Purpose                                                                                                                                                                                              |
|--------|---------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| CREATE | `internal/server/handlers/fleet_dashboard.go`                 | Two new HTTP handlers: `FleetHeartbeats` (GET `/api/v1/fleet/heartbeats?window=60m`) and `FleetContainers` (GET `/api/v1/fleet/containers`). Uses `telemetry` and `devices` repos.                    |
| CREATE | `internal/server/handlers/fleet_dashboard_test.go`            | Table-driven unit tests covering `FleetHeartbeats` (window cap, grouping by device, empty case, malformed window) and `FleetContainers` (flatten, hostname join, stack label, empty case).           |
| MODIFY | `internal/server/db/interfaces.go`                            | Add `GetFleetHeartbeats(ctx, since time.Time) (map[string][]models.Heartbeat, error)` to `TelemetryRepository`. Add `GetGPUDeviceIDs(ctx) ([]string, error)` to `TelemetryRepository`. Add `GetFleetContainerLeaderboard(ctx) ([]FleetContainerProjection, error)` to `TelemetryRepository` (see Addendum AD-011). |
| MODIFY | `internal/server/db/telemetry_repo.go`                        | Implement the new methods. `GetFleetHeartbeats` runs `SELECT device_id, timestamp, data FROM heartbeats WHERE timestamp >= $1 ORDER BY device_id, timestamp ASC` — leverages `idx_heartbeats_device_time`. `GetGPUDeviceIDs` projects `jsonb_array_length(data->'gpu_telemetry'->'gpus') > 0` from `GetAllLatestSummaries`'s underlying query. `GetFleetContainerLeaderboard` projects only the containers JSONB sub-tree per device (see Addendum AD-011 for the exact query). |
| MODIFY | `internal/server/router.go`                                   | Add `r.Get("/api/v1/fleet/heartbeats", h.FleetHeartbeats)` and `r.Get("/api/v1/fleet/containers", h.FleetContainers)` inside the `adminAuth` group, alongside existing `/api/v1/fleet/*` routes.       |
| MODIFY | `internal/testutil/mocks.go`                                  | Implement the new methods on `MockTelemetryRepo` (`GetFleetHeartbeats`, `GetGPUDeviceIDs`, `GetFleetContainerLeaderboard`) so existing tests continue to compile and the new handler tests can mock data. |

### Frontend (TypeScript / React)

| Action | File Path                                                     | Purpose                                                                                                                                                                                              |
|--------|---------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| CREATE | `web/src/pages/Dashboard.tsx`                                 | Top-level dashboard page. Composes the five sections, owns the disconnected banner, owns the 5 s tick effect, owns the `useFleetMetrics` aggregator.                                                  |
| CREATE | `web/src/pages/dashboard/KPIStrip.tsx`                        | Renders the eight tiles (GPU tile conditional). Uses `Sparkline` as background per tile.                                                                                                             |
| CREATE | `web/src/pages/dashboard/KPITile.tsx`                         | One tile component. Props: `label`, `value`, `delta`, `sparklineData`, optional `subtext`. Owns the value-flash CSS (FR-012).                                                                        |
| CREATE | `web/src/pages/dashboard/SmallMultiples.tsx`                  | The four small-multiples charts. Each is a recharts `<AreaChart>`; per-device charts iterate `<Area>` per device with hover-isolate.                                                                  |
| CREATE | `web/src/pages/dashboard/HeatmapGrid.tsx`                     | Grid of `DeviceCard`s sorted by stress-score descending; offline devices forced to end. Empty state when device count is zero.                                                                        |
| CREATE | `web/src/pages/dashboard/DeviceCard.tsx`                      | Single card: status dot (pulses on heartbeat), hostname, three mini-bars, 5 m sparkline, container badge, hairline red border when alerting. Click navigates to `/devices/:id` via `react-router-dom`. |
| CREATE | `web/src/pages/dashboard/ContainerLeaderboard.tsx`            | Sortable top-10 table with the four sort modes and three filter chips. Empty state when no containers.                                                                                                |
| CREATE | `web/src/pages/dashboard/ActivityRiver.tsx`                   | Reads from `['events']` cache; applies chip filter; caps at 20 with fade-in CSS class on enter.                                                                                                       |
| CREATE | `web/src/pages/dashboard/DisconnectedBanner.tsx`              | Renders when `wsConnected` is false.                                                                                                                                                                  |
| CREATE | `web/src/hooks/useFleetMetrics.ts`                            | Encapsulates: fetching `/api/v1/fleet/heartbeats`, fetching `/api/v1/fleet/containers`, computing per-tick aggregates, exposing `{ kpis, perDeviceSeries, networkSeries, diskIOSeries, hasGPU }`.       |
| CREATE | `web/src/hooks/useDashboardTick.ts`                           | A `setInterval(5000)` ticker that components subscribe to so chart re-renders are batched (FR-062, NFR-003).                                                                                          |
| CREATE | `web/src/hooks/usePerDevicePulse.ts`                          | Subscribes to WS `heartbeat` messages, exposes `{ [deviceId]: lastPulseAt }` so `DeviceCard` can pulse the status dot (FR-032).                                                                       |
| CREATE | `web/src/utils/stress.ts`                                     | Pure function `stressScore(hb, online): number` implementing FR-034 / BR-004.                                                                                                                        |
| CREATE | `web/src/utils/eventCategory.ts`                              | Maps `Event.type` / `Event.severity` to one of `'alerts' \| 'docker' \| 'agent' \| 'security' \| 'other'` for chip filtering.                                                                          |
| CREATE | `web/src/utils/stress.test.ts`                                | Vitest unit tests for the stress formula (offline devices last, exact weighted-sum, load_avg cap at 100).                                                                                            |
| CREATE | `web/src/utils/eventCategory.test.ts`                         | Vitest unit tests for category mapping.                                                                                                                                                              |
| CREATE | `web/src/pages/dashboard/Dashboard.test.tsx`                  | Vitest + Testing Library: smoke render, AC-mapped tests (route, KPI tiles, GPU conditional, disconnected banner, empty fleet, sort order).                                                            |
| CREATE | `web/src/pages/dashboard/HeatmapGrid.test.tsx`                | Vitest tests for stress-sort + offline-end + empty state + alert hairline border.                                                                                                                    |
| CREATE | `web/src/pages/dashboard/ContainerLeaderboard.test.tsx`       | Vitest tests for the four sort modes, three chips, empty state, top-10 cap.                                                                                                                          |
| CREATE | `web/src/pages/dashboard/ActivityRiver.test.tsx`              | Vitest tests for cap-at-20 + chip multiselect + fade-in class.                                                                                                                                        |
| MODIFY | `web/src/App.tsx`                                             | Add `import Dashboard from './pages/Dashboard'`. Add `<NavLink to="/dashboard">Dashboard</NavLink>` to **both** the desktop nav block and the mobile-menu block. Add `<Route path="/dashboard" element={<Dashboard />} />`. **Do not** alter the existing `/` route or its label. |
| MODIFY | `web/src/api/client.ts`                                       | Add `getFleetHeartbeats(window: string)` and `getFleetContainers()` and the corresponding response type definitions.                                                                                  |
| MODIFY | `web/src/types/models.ts`                                     | Add `FleetHeartbeatsResponse` and `FleetContainerRow` interfaces matching the new endpoint shapes.                                                                                                   |

### Documentation (technical writer scope, not engineering)

The technical writer will update `README.md`, `CHANGELOG.md`, and the API
section after QA. Engineers should not pre-empt that work.

---

## 5. Data Model Changes

### Schema / Migration

**None required.** Justification:

- All time-series data is read from the existing `heartbeats` table, indexed
  by `idx_heartbeats_device_time` (device_id, timestamp DESC). The new
  `GetFleetHeartbeats` query filters on `timestamp >= $1` only and groups in
  Go. PostgreSQL evaluates that as an index-only-ish scan over the time-range;
  for a 60-minute window on a 30-device fleet this returns ~7,200 rows, well
  inside a 50 ms budget.
- All container leaderboard data is read from the existing `telemetry_snapshots`
  table via the existing `GetAllLatestSnapshots` query. The new
  `GetGPUDeviceIDs` reuses the same `DISTINCT ON (device_id) … ORDER BY
  timestamp DESC` pattern as `GetAllLatestSummaries`.
- Events come from the existing `events` table.
- Active-alert detection per device is derived client-side by joining the
  cached events list to the device list — no server-side change.

### New Go types

```go
// internal/server/handlers/fleet_dashboard.go (new)

type FleetHeartbeatsResponse struct {
    Since          time.Time                       `json:"since"`
    Until          time.Time                       `json:"until"`
    Devices        map[string][]models.Heartbeat   `json:"devices"`
    DevicesWithGPU []string                        `json:"devices_with_gpu"`
}

type FleetContainerRow struct {
    DeviceID        string  `json:"device_id"`
    Hostname        string  `json:"hostname"`
    ContainerID     string  `json:"container_id"`
    ContainerName   string  `json:"container_name"`
    Image           string  `json:"image"`
    Stack           string  `json:"stack,omitempty"`
    State           string  `json:"state"`
    CPUPercent      float64 `json:"cpu_percent"`
    MemUsage        int64   `json:"mem_usage"`
    MemLimit        int64   `json:"mem_limit"`
    RestartCount    int     `json:"restart_count"`
    UpdateAvailable *bool   `json:"update_available,omitempty"`
}
```

### New TypeScript types

`FleetHeartbeatsResponse` and `FleetContainerRow` mirroring the Go shapes.

---

## 6. API / Interface Contract

### `GET /api/v1/fleet/heartbeats`

- **Auth**: Admin (JWT cookie). Sits inside the existing `adminAuth` group.
- **Query params**:
  - `window` (optional, default `60m`, max `60m`). Format: integer minutes
    suffixed with `m` or integer seconds with `s`. Anything else → `400`.
    See Addendum AD-013 for the precise acceptance grammar and edge-case
    behavior.
- **200 OK** body:
  ```json
  {
    "since":  "2026-04-27T17:00:00Z",
    "until":  "2026-04-27T18:00:00Z",
    "devices": {
      "<device-id>": [
        { "id": 123, "device_id": "<device-id>", "timestamp": "2026-04-27T17:00:15Z",
          "data": { "cpu_percent": 12.3, "mem_percent": 45.1,
                    "load_avg_1m": 0.8, "disk_root_percent": 41.2,
                    "disk_read_bytes_sec": 0, "disk_write_bytes_sec": 0,
                    "uptime": 12345, "agent_version": "1.4.2" } }
      ]
    },
    "devices_with_gpu": ["<device-id-a>", "<device-id-b>"]
  }
  ```
- **400 Bad Request** body: `{ "error": "invalid window parameter" }`
- **500 Internal Server Error** body: `{ "error": "failed to fetch heartbeats" }`
- **401**: handled by the existing `adminAuth` middleware (consistent with
  every other admin endpoint).

### `GET /api/v1/fleet/containers`

- **Auth**: Admin (JWT cookie). Inside `adminAuth` group.
- **Query params**: none.
- **200 OK** body:
  ```json
  [
    {
      "device_id": "<id>",
      "hostname":  "homelab-01",
      "container_id": "abc123",
      "container_name": "plex",
      "image": "linuxserver/plex:latest",
      "stack": "media",
      "state": "running",
      "cpu_percent": 12.5,
      "mem_usage": 1048576000,
      "mem_limit": 4294967296,
      "restart_count": 0,
      "update_available": true
    }
  ]
  ```
- **500 Internal Server Error** body: `{ "error": "failed to fetch fleet containers" }`

### Existing endpoints reused (no contract change)

- `GET /api/v1/devices`
- `GET /api/v1/events?limit=50`
- `GET /api/v1/fleet/patch-status`
- `GET /api/v1/devices/{id}/containers/{cname}/metrics?hours=1`
- `GET /ws` (broadcast WS)

---

## 7. Sequence / Flow

### Initial dashboard load (cold cache)

1. User clicks "Dashboard" in nav → `react-router-dom` navigates to `/dashboard`.
2. `Dashboard` mounts. `useDevices()` is already active globally; the device list
   is in cache.
3. `useFleetMetrics` issues two parallel requests:
   - `GET /api/v1/fleet/heartbeats?window=60m` (initial KPI + small-multiples
     data)
   - `GET /api/v1/fleet/containers` (initial leaderboard rows)
   - `GET /api/v1/events?limit=50` (river backfill — only if not already in
     React Query cache)
4. While in-flight, the page renders skeleton placeholders for KPIs, charts,
   grid, leaderboard, river.
5. On response, `useFleetMetrics` derives:
   - KPIs (CPU avg/peak from heartbeats; mem avg/peak; worst-disk device;
     containers running/total from `useDevices` data; active alerts from cached
     events; pending updates from cached patch-status; GPU avg from
     `devices_with_gpu` × cached telemetry).
   - Per-device CPU% and Mem% time series (4 lines per chart × N devices).
   - Network in/out series (sum interface-rate diffs across devices and
     timestamps).
   - Disk I/O read/write series (sum across devices, per heartbeat).
6. Heatmap grid sorts devices by stress score, places offline at end, renders
   cards.
7. Activity river renders top 20 from cached events, applies chip filters.
8. `usePerDevicePulse` map starts empty; populates on subsequent WS heartbeats.

### Live update — heartbeat arrives

1. WS `heartbeat` message: `useDevices` updates the device's `last_heartbeat`
   (already implemented).
2. `useFleetMetrics` receives the same WS event and pushes the heartbeat into a
   **ref-backed** ring buffer keyed by device.
3. KPI tile recompute is triggered immediately for KPIs that depend on the new
   heartbeat (CPU avg/peak, mem avg/peak, worst-disk). The tile component plays
   its flash transition.
4. `usePerDevicePulse` updates `lastPulseAt[deviceId] = now`. The corresponding
   `DeviceCard` re-renders its status dot; the dot's CSS animation re-keys.
5. Chart re-render is **not** triggered. The buffer is read on the next 5 s
   tick.

### Live update — 5-second tick

1. `useDashboardTick` fires.
2. `useFleetMetrics` flushes the ring buffer into derived series state.
3. `SmallMultiples` re-renders charts. `ContainerLeaderboard` re-derives top-10
   from cached telemetry.

### Live update — event arrives

1. WS `event` message: `GlobalWSHandler` (already in `App.tsx`) prepends to the
   `['events']` cache.
2. `ActivityRiver` is already subscribed to the same cache via `useQuery`, so
   it re-renders. CSS `enter` class on the new top item drives fade-in.
3. River caps to 20 in the derived selector.

### WebSocket disconnect

1. WS closes. `useDevices().wsConnected` flips to `false`.
2. `DisconnectedBanner` renders.
3. All cached data continues to render.
4. WS reconnects → `wsConnected = true` → banner unmounts → updates resume.

### Heatmap card click

1. `DeviceCard` is wrapped in `<Link to={\`/devices/\${device.id}\`}>`.
2. `react-router-dom` navigates; `Dashboard` unmounts; `DeviceDetail` mounts.

---

## 8. Acceptance Criteria Mapping

| AC      | Fulfilled By                                                                                                                              | Test Strategy                                                                                                                                                        |
|---------|-------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| AC-001  | `App.tsx` `<Route path="/dashboard">` + `<NavLink to="/dashboard">` in both nav blocks; `Dashboard.tsx` mounts                              | Vitest in `Dashboard.test.tsx`: render `<App>` with route at `/dashboard`, assert `Dashboard` content visible and the link has the active class.                     |
| AC-002  | `App.tsx` route ordering — `/` still maps to `FleetOverview`; no redirect added                                                            | Vitest: render at `/`, assert `FleetOverview` test ID is present and no redirect to `/dashboard`.                                                                    |
| AC-003  | Existing auth flow — `Dashboard` is reached only via the authenticated app shell which already gates on `useAuth`                          | Vitest: render `<App>` with `authenticated=false`, assert the `Login` component renders even when initial URL is `/dashboard`.                                       |
| AC-004  | `KPIStrip.tsx` renders 7 mandatory tiles + GPU when present; `KPITile.tsx` renders primary value, delta, sparkline                         | Vitest in `Dashboard.test.tsx`: seed devices/heartbeats/events/patch-status; assert each tile by label.                                                              |
| AC-005  | `useFleetMetrics.hasGPU` derived from `devices_with_gpu`; `KPIStrip` conditionally renders the tile                                       | Vitest: `devices_with_gpu` empty → assert no element with role/label "GPU" in DOM.                                                                                   |
| AC-006  | Same as AC-005, inverse path                                                                                                              | Vitest: `devices_with_gpu` non-empty → assert GPU tile in DOM.                                                                                                       |
| AC-007  | `KPITile.tsx` adds a CSS class on value change, removed via `setTimeout`; trigger via WS `heartbeat` handler in `useFleetMetrics`         | Vitest: simulate WS heartbeat that changes a KPI value; assert the tile gets the `flash` class then loses it.                                                        |
| AC-008  | `useFleetMetrics.kpis.worstDisk = max-by-disk_root_percent over online devices`; `KPIStrip` renders hostname + GaugeBar                    | Vitest: seed 2 devices (70% / 95%); assert tile renders hostname of the 95% device and value 95.                                                                     |
| AC-009  | `KPITile.tsx` renders an up/down/zero glyph and color based on `delta` prop                                                               | Vitest: pass delta values +5, -3, 0; assert glyph + color class for each.                                                                                            |
| AC-010  | `SmallMultiples.tsx` renders 4 charts in a grid                                                                                           | Vitest: render with seeded heartbeats; assert 4 chart containers by data-testid.                                                                                     |
| AC-011  | `SmallMultiples.tsx` per-device chart tracks `activeDeviceId` on hover; opacity-dims non-active `<Area>`                                  | Vitest: simulate `mouseMove` on chart, assert non-active areas receive the dimmed-opacity class; tooltip text contains hostname + value.                              |
| AC-012  | `useFleetMetrics` truncates per-device series at the device's last heartbeat timestamp; `<Area>` `connectNulls={false}`                    | Vitest: seed device with last heartbeat 20 min into the window; assert that chart data array for that device has no entries past the cutoff.                          |
| AC-013  | `HeatmapGrid.tsx` + `DeviceCard.tsx` render all required pieces                                                                            | Vitest: render with 12 devices; assert each card has status dot, hostname, three GaugeBar elements, sparkline svg, container badge.                                  |
| AC-014  | `usePerDevicePulse` updates a device's last-pulse timestamp; `DeviceCard` keys its dot animation on that timestamp                        | Vitest: simulate WS heartbeat for device X; assert the dot element re-renders with the `pulse` class.                                                                |
| AC-015  | `DeviceCard.tsx` reads cached events filtered by device with severity `warning|critical` and unacknowledged; renders `border-red-500/40`   | Vitest: seed event for device X; assert card receives the red border class.                                                                                          |
| AC-016  | `useFleetMetrics` sorts devices by `stressScore(hb, online)` desc, with offline devices forced to end                                     | Vitest: seed devices with mixed scores + 1 offline; assert order matches expected; assert offline at end regardless of last-known score. Also `stress.test.ts` covers the formula. |
| AC-017  | `DeviceCard.tsx` wraps content in `<Link to={`/devices/${id}`}>`                                                                          | Vitest: click card; assert navigation called with `/devices/<id>`.                                                                                                   |
| AC-018  | `HeatmapGrid.tsx` returns an empty-state component when device count is zero                                                              | Vitest: seed empty devices array; assert empty-state copy renders and no `.heatmap-grid` element.                                                                    |
| AC-019  | `ContainerLeaderboard.tsx` re-sorts on chip selection; uses `cpu_percent`, `mem_usage`, `restart_count`, `update_available` fields         | Vitest: seed >10 containers; assert top-10 by each sort matches expected order.                                                                                      |
| AC-020  | Each row renders hostname / name / stack / metric value / metric sparkline (lazy-fetched from `/devices/{id}/containers/{cname}/metrics`)  | Vitest: render leaderboard; assert one row's columns; mock the metrics fetch and assert sparkline data flows in.                                                     |
| AC-021  | Three chips bound to derived filters in `ContainerLeaderboard.tsx`                                                                         | Vitest: activate each chip; assert visible rows match expectation.                                                                                                   |
| AC-022  | `ContainerLeaderboard.tsx` renders empty-state when filtered list is empty AND no chip restricts                                          | Vitest: zero containers; assert empty-state copy and no table frame.                                                                                                 |
| AC-023  | `ActivityRiver.tsx` reads `['events']` cache; renders severity-colored items                                                              | Vitest: seed events of mixed severity; assert color classes.                                                                                                         |
| AC-024  | `ActivityRiver.tsx` applies `enter` CSS class on new event added since previous render                                                    | Vitest: prepend a new event via cache update; assert the new top item has the enter class then loses it.                                                              |
| AC-025  | River selector `.slice(0, 20)`                                                                                                            | Vitest: push 25 events; assert exactly 20 rendered.                                                                                                                  |
| AC-026  | Multi-select chip state in `ActivityRiver.tsx` with `eventCategory.ts` mapping                                                            | Vitest: activate alerts + docker; assert only matching rows; deactivate all → all rows.                                                                              |
| AC-027  | Dashboard uses `useDevices()` (which uses the singleton `useWebSocket`); does not call `new WebSocket(...)` anywhere                       | Vitest: spy on `WebSocket` global constructor; navigate to `/dashboard`; assert constructor not called a second time.                                                 |
| AC-028  | `useDashboardTick` 5 s `setInterval` drives chart redraws; per-heartbeat handler only mutates a ref ring buffer                            | Vitest with fake timers: push 50 heartbeats over 1 s; assert chart render count ≤ 1; advance 5 s; assert render count increments by 1; KPI render count tracks heartbeats. |
| AC-029  | `DisconnectedBanner.tsx` renders when `wsConnected=false`; cache values continue rendering                                                 | Vitest: flip `wsConnected` to false; assert banner present, charts/cards still rendering last values; flip back; banner gone.                                          |
| AC-030  | `GET /api/v1/fleet/heartbeats` returns full window in ≤1 round trip; `GET /api/v1/fleet/containers` uses the projected query (Addendum AD-011); client renders incrementally | Manual perf test in QA on a 30-device dev fleet (devtools "Performance" trace from route mount to KPI paint). Backend `fleet_dashboard_test.go` asserts query is single-shot for both endpoints and that `FleetContainers` does not invoke `GetAllLatestSnapshots`. |
| AC-031  | Tailwind responsive classes in each section                                                                                               | Vitest with `matchMedia` mock at 1280px width; assert all five sections render in full grid layouts.                                                                  |
| AC-032  | Tailwind responsive classes degrade at <768px                                                                                             | Vitest with `matchMedia` mock at 480px width; assert sections render stacked, heatmap collapses to fewer columns.                                                    |
| AC-033  | Page wrapper uses existing `max-w-7xl mx-auto px-4` plus per-section `overflow-x-auto` only on the leaderboard table                       | Vitest snapshot: at 360px, assert the dashboard root element's `scrollWidth` equals `clientWidth` (no overflow).                                                      |
| AC-034  | `SeverityBadge`, status dots, delta glyphs all carry an icon or text label per existing convention                                        | Vitest: assert each color-bearing element also has either an `aria-label`, an icon child, or visible text.                                                            |
| AC-035  | No map, no drag-drop library, no embedded drill-down panels — by construction (none added)                                                | Vitest: assert the rendered DOM does not contain `data-testid="map"`, `data-testid="drag-handle"`, `data-testid="drilldown-panel"`. Lint for absence of `react-dnd` / `leaflet` imports. |

> Addendum AC mapping additions (Section 8 cross-references):
>
> - **AC-019, AC-020, AC-021** are additionally constrained by Addendum AD-012:
>   `ContainerLeaderboard.tsx` and any helper that consumes the cached
>   `telemetry` for live updates MUST project to a strict allowlist of fields.
>   Test owner: `ContainerLeaderboard.test.tsx` adds an `[AD-012]`-prefixed
>   case asserting that a fixture with sensitive labels does not surface label
>   keys other than `com.docker.compose.project` in any rendered text or any
>   value held in component state. QA additionally grep-verifies that
>   `Labels[` does not appear in `web/src/pages/dashboard/`,
>   `web/src/hooks/useFleetMetrics.ts`, or any other dashboard module other
>   than the one-line stack-name extraction.
> - **AC-030** test coverage now also asserts that `FleetContainers` does not
>   call `GetAllLatestSnapshots` (Addendum AD-011).
> - **AD-013 window parser** test coverage lives in
>   `fleet_dashboard_test.go` under a dedicated table-driven suite. The full
>   input matrix is enumerated in the addendum.

---

## 9. Error Handling

| Failure Mode                                          | Detection                          | Response                                                                                                                                                            |
|-------------------------------------------------------|-------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `GET /api/v1/fleet/heartbeats` invalid window         | Handler validates regex + max 60m   | `400 { "error": "invalid window parameter" }`. Client falls back to default 60m. Edge-case behavior fully specified in Addendum AD-013.                             |
| `GET /api/v1/fleet/heartbeats` DB error               | `pgx` error from `GetFleetHeartbeats` | `500 { "error": "failed to fetch heartbeats" }`. Server logs via `slog.Error` per existing pattern.                                                                |
| `GET /api/v1/fleet/containers` DB error               | `pgx` error                          | `500 { "error": "failed to fetch fleet containers" }`. Server logs.                                                                                                |
| Frontend fetch fails on dashboard mount               | React Query `error` state            | Skeleton stays; an inline retry toast (using existing patterns) prompts the user. Dashboard does not render a permanent error page — partial data is acceptable.   |
| Device with no heartbeats in window                   | Empty array in `devices[deviceId]`  | Card renders with last-known values from `useDevices` cache; small-multiples line for that device is empty. No error.                                              |
| Heartbeat with stale/zero values                      | Pass-through                         | Render literal value per Validation Rule in FRD §7. No silent substitution.                                                                                        |
| Device missing optional telemetry (GPU, UPS, ...)     | Optional chaining in TS              | Skip silently. KPI tile collapses where applicable (GPU per FR-014).                                                                                               |
| WS disconnect                                         | `wsConnected=false`                  | Banner per FR-063. Cached data renders.                                                                                                                            |
| Empty fleet                                           | `devices.length === 0`               | KPIs show 0/0; heatmap shows empty-state copy (FR-036).                                                                                                            |
| Single device                                         | `devices.length === 1`               | All sections render normally with one card / one line per chart.                                                                                                   |

---

## 10. Security Considerations

- **Auth**: Both new endpoints sit inside the existing `adminAuth` (JWT cookie)
  group. No new role, no new permission. NFR-008 satisfied by reusing the
  existing middleware chain.
- **Information disclosure**: Both endpoints expose data the existing
  per-device endpoints already expose to the same admin role. No new sensitive
  fields are surfaced (e.g., environment variables, secrets, container env)
  because we deliberately project only the fields the dashboard needs. The
  fleet-containers projection excludes `Env`, `Mounts`, `Networks`, `Labels`
  (except for compose project label), `Riot.URL`. The WS-fed `telemetry` cache
  is additionally constrained by Addendum AD-012.
- **Rate limit**: The endpoints are admin-only and called once per dashboard
  mount. The existing rate-limiter is not applied to admin routes; this is
  consistent with `/api/v1/fleet/agent-versions` and `/api/v1/fleet/patch-status`.
- **Window cap**: `window` query parameter is hard-capped at 60 minutes server-side
  to prevent a user-supplied multi-day query from scanning a large window of
  the heartbeats table. Full edge-case handling specified in Addendum AD-013.
- **Input validation**: `window` parameter follows the strict acceptance
  grammar defined in Addendum AD-013. Anything outside that grammar → 400.
- **No CSRF concern**: GET endpoints, no state mutation.
- **Frontend**: No `dangerouslySetInnerHTML`, no `eval`, no new dependencies.
  Event message text is rendered as plain text via React (auto-escaped).

---

## 11. Performance Considerations

### Database

- `GET /api/v1/fleet/heartbeats?window=60m` runs:
  ```
  SELECT device_id, timestamp, data
  FROM heartbeats
  WHERE timestamp >= $1
  ORDER BY device_id, timestamp ASC
  ```
  This uses `idx_heartbeats_device_time` for the timestamp filter (the index
  is `(device_id, timestamp DESC)` which is suitable for range-scanning the
  recent window when sorted by `device_id`). For a 30-device fleet at 15 s
  heartbeat cadence over 60 min, that is ~7,200 rows. Expected p95 < 50 ms
  on PG 16 with the existing index. **No new index required.**
- If 100-device fleets routinely exceed budget, a partial index
  `idx_heartbeats_recent ON heartbeats(timestamp DESC) WHERE timestamp > now() - interval '7 days'`
  could be considered later, but is **not** added in this story (premature for
  the documented scale).
- `GET /api/v1/fleet/containers` uses `GetFleetContainerLeaderboard` —
  a JSONB-projected query (see Addendum AD-011). The full snapshot blob is
  never decoded server-side. Expected p95 < 100 ms on a 100-device fleet.
- `GetGPUDeviceIDs` adds a JSONB projection
  (`jsonb_array_length(data->'gpu_telemetry'->'gpus') > 0`) to the existing
  `latest snapshot per device` query. Negligible additional cost.

### WebSocket

- Message rate is unchanged. The hub already broadcasts every heartbeat to
  every connected client; the dashboard adds one more subscriber, identical to
  what `FleetOverview` already does.
- NFR-003 (5 s batched chart cadence under heartbeat traffic >1/sec/device): the
  per-heartbeat WS handler only writes to a ref-backed buffer; only the 5 s
  tick triggers `setState` for charts. KPI flashes are tiny re-renders of leaf
  components and do not propagate to charts.

### Frontend render budget

- Initial render to first paint of KPIs (NFR-001 / AC-030): single
  `/api/v1/fleet/heartbeats` request + client aggregation. On a mid-tier
  desktop with 30 devices, expected total < 1.5 s including network. Skeleton
  placeholders prevent layout jank during in-flight.
- 100-device interactive (NFR-002): heatmap grid = 100 cards × ~12 DOM nodes
  each ≈ 1,200 nodes. recharts `<AreaChart>` with 100 areas × 240 points ≈
  acceptable in current Chrome; if profiling shows fps drops, fallback is to
  downsample server-side at >100 devices (not in v1 scope).
- Memoization: `useFleetMetrics` returns memoized aggregate objects keyed by
  the most recent tick + device-list identity, so child components re-render
  only when their slice changes.

### Network

- One initial heartbeat fetch, one initial containers fetch, one initial events
  fetch. Total < 250 KB gzipped on a 30-device fleet. Subsequent updates ride
  the existing WS broadcast.

---

## 12. Implementation Notes for Engineers

- **Reuse `useDevices` directly.** Do not re-fetch the device list. The hook's
  `wsConnected` flag is also the source of truth for the disconnected banner —
  do not introduce a parallel WS state.
- **Single WS subscription.** Verified by AC-027: every WS-aware hook in this
  story (`useFleetMetrics`, `usePerDevicePulse`) must call `useWebSocket(handler)`
  from `web/src/hooks/useWebSocket.ts`, which goes through the singleton
  `WebSocketProvider`. **Do not** call `new WebSocket(...)` anywhere.
- **5 s batch enforcement.** Implementation pattern:
  ```
  // useFleetMetrics
  const bufferRef = useRef<Map<string, Heartbeat>>(new Map())
  // WS handler writes bufferRef.current.set(deviceId, hb) — no setState.
  // useDashboardTick fires every 5s and calls setState({...derived from buffer}).
  ```
  KPI numeric tiles can subscribe to a separate, finer-grained piece of state
  that **does** update on every heartbeat (FR-061). They must be leaf
  components so their re-render does not cascade into charts.
- **Stress score math** (do not deviate; this is the FRD's product decision):
  ```
  score = 0.4*cpu + 0.3*mem + 0.2*disk_root + 0.1*min(load_avg_1m * 25, 100)
  // offline devices return Number.NEGATIVE_INFINITY so they sort to the end.
  ```
- **Restart-anomaly semantic.** As called out in AD-006, "more than 3 restarts
  in the last hour" cannot be computed exactly from current data because the
  server only stores the latest `restart_count` per telemetry snapshot. The v1
  filter is: `latest.restart_count > 3 AND latest.timestamp >= now - 60m`. This
  is documented in the FR-041 implementation comment in
  `ContainerLeaderboard.tsx`. A follow-up story should add a
  `container_metrics` historical projection of `restart_count` for true
  rolling-window detection.
- **Network in/out series.** `HeartbeatData` does not carry network rate. Use
  the `disk_read_bytes_sec` / `disk_write_bytes_sec` fields for the disk-IO
  chart (already a rate). For network in/out, derive a rate client-side from
  the cached `FullTelemetryData.network.interfaces[].bytes_sent/recv` counters
  on each device's latest telemetry, snapshotted at each heartbeat tick.
  **Sum across non-loopback interfaces only** (skip `lo`). If telemetry has
  not arrived for a device yet (e.g., cold cache), that device contributes
  zero to the fleet sum — no error.
- **Sparkline backgrounds in KPI tiles.** Reuse `Sparkline.tsx` with a
  background-style `color` prop set to `rgba(...)` and absolute-positioned
  inside the tile at low opacity. Do not introduce a new sparkline component.
- **Hairline border on alerting cards.** `border border-red-500/40` Tailwind
  class. Active-alert query: cached events filtered to severity in
  {warning, critical}, unacknowledged, device_id matches.
- **Container leaderboard sparklines.** Use `Sparkline.tsx`, not
  `ContainerMetricChart.tsx` (the latter is full-size with axes). Lazy-fetch
  per visible row only.
- **Empty states must omit the frame.** Per FR-036 / FR-045: when empty, render
  empty-state copy *instead of* the grid/table, not in addition to.
- **Color is not the sole signal.** Per AC-034, every severity/status/delta
  visual must carry an icon or text label. Reuse `SeverityBadge` and existing
  glyphs (chevron-up / chevron-down for delta). The status dot already has a
  text label in adjacent hostname.
- **Nav link.** Add `<NavLink to="/dashboard">Dashboard</NavLink>` to **both**
  the desktop nav `<div className="hidden sm:flex gap-1">` block and the
  mobile `<div className="sm:hidden border-t...">` block in `App.tsx`.
  `NavLink` already handles the active styling via `useLocation`.
- **Test naming.** Per CLAUDE.md engineering standards, every test must
  reference the AC: `describe('[AC-016] Default sort is stress score descending', ...)`.
- **No `any` types.** TypeScript strict mode is in force. Use the new
  interfaces in `web/src/types/models.ts`.
- **Linter / format.** Match neighboring file styles. Tailwind utility ordering
  follows the project's existing convention (no Tailwind formatter is enforced).
- **Do not change `FleetOverview.tsx` or `/`.** Per FR-004 / AC-002.

---

## 13. Definition of Done

- [ ] All component changes in Section 4 implemented (every CREATE/MODIFY row).
- [ ] `GET /api/v1/fleet/heartbeats` and `GET /api/v1/fleet/containers` registered
      in `internal/server/router.go` inside the `adminAuth` group.
- [ ] `TelemetryRepository` interface and mock updated; `var _ db.TelemetryRepository = (*MockTelemetryRepo)(nil)` still compiles.
- [ ] Every AC in Section 8 has at least one named test (`describe('[AC-NNN] ...')`)
      passing.
- [ ] `make test-go` passes (no new failures).
- [ ] `cd web && npm run test:run` passes (no new failures).
- [ ] No new linter errors (`tsc` clean; Go `go vet` clean).
- [ ] No new dependency added to `package.json` or `go.mod`.
- [ ] No change to existing migrations.
- [ ] Manual smoke check: navigate to `/dashboard` on a dev server with at
      least one device registered; KPIs, small multiples, heatmap, leaderboard,
      activity river all render; clicking a heatmap card navigates to
      `/devices/:id`; reloading at `/dashboard` works (SPA fallback).
- [ ] Implementation report written at
      `docs/implementation/FLEET-DASH-impl-report.md`.
- [ ] Addendum addressed: AD-011 projected query implemented and `FleetContainers`
      no longer calls `GetAllLatestSnapshots`; AD-012 leaderboard does not
      reference container `Labels` except the compose-project key; AD-013 window
      parser table tests pass for the full input matrix.

---

## 14. Open Questions / Blockers

None. All four FRD-flagged open questions (OQ-001 through OQ-004) are resolved
in Section 3:

- OQ-001 / OQ-002 → AD-001, AD-006 (one new `fleet/heartbeats` endpoint, one
  new `fleet/containers` endpoint; client aggregates).
- OQ-003 → AD-003 (stress score client-side).
- OQ-004 → AD-002 (existing `heartbeat` WS message carries enough signal for
  FR-032; no new WS message type).

The only acknowledged v1 simplification is the restart-anomaly semantic
(AD-006 / Section 12) which follows directly from existing data shape and is
documented as such — not a blocker, but flagged for a future iteration.

---

## Addendum: Security Findings Resolution

**Date appended:** 2026-04-27
**Trigger:** Security review (`docs/security/FLEET-DASH-security-review.md`)
verdict APPROVED WITH CONDITIONS, with three MEDIUM findings (SEC-001, SEC-002,
SEC-003) requiring architectural decisions before implementation.

This addendum supersedes any conflicting language earlier in the document. It
adds three architecture decisions (AD-011, AD-012, AD-013), updates Section 4
(Component Changes) and Section 8 (AC mapping) by reference, and adds three
explicit Definition-of-Done items above.

### AD-011: Container leaderboard query — projected JSONB, never full-blob decode (resolves SEC-001)

**Decision**: `GET /api/v1/fleet/containers` does **not** call
`TelemetryRepository.GetAllLatestSnapshots`. A new repository method
`GetFleetContainerLeaderboard(ctx context.Context) ([]FleetContainerProjection, error)`
is added to `TelemetryRepository`. It performs JSONB projection in PostgreSQL,
returning only the container-shaped fields the leaderboard renders. Full
snapshot blobs are never decoded on the server.

**Rationale**:
- Option (a) from the security review. The slimmer `GetAllLatestSummaries`
  exists but its projection (`updates`, `security`, `web_servers.servers`)
  does not include containers, so it cannot be reused as-is.
- Option (b) (reuse `GetAllLatestSummaries` directly) is therefore not viable.
- Option (c) (cap at 30 devices in v1 with a deferred follow-up) is rejected
  because NFR-002 commits to 100 devices and the cost of the projected query
  is small and well-understood.
- Option (b-cache, short-lived in-memory cache around `GetAllLatestSnapshots`)
  is rejected: a cache mitigates burst-refetch but still pays the full-decode
  cost on every miss, and adds invalidation correctness work for a problem the
  database can solve directly.

**Query shape** (engineers implement this verbatim, modulo formatting):

```sql
SELECT
  device_id,
  jsonb_path_query_array(
    data,
    '$.docker.containers[*] ?
       (@.state == "running" || @.state == "restarting" || @.state == "exited" ||
        @.state == "paused"  || @.state == "created"    || @.state == "dead")'
  ) AS containers
FROM (
  SELECT DISTINCT ON (device_id) device_id, data
  FROM telemetry_snapshots
  ORDER BY device_id, timestamp DESC
) latest
WHERE data ? 'docker';
```

The handler decodes the per-device `containers` JSON array (already small —
typically ~20 containers × ~500 bytes per device) into the
`FleetContainerProjection` shape, joins to `DeviceRepository.List` for hostname
lookup, and emits the existing `FleetContainerRow` JSON shape defined in
Section 5. The projection struct mirrors only the fields consumed by the
leaderboard:

```go
// internal/server/db/telemetry_repo.go (new)

type FleetContainerProjection struct {
    DeviceID   string
    Containers []FleetContainerProjectionRow
}

type FleetContainerProjectionRow struct {
    ID              string  `json:"id"`
    Name            string  `json:"name"`
    Image           string  `json:"image"`
    State           string  `json:"state"`
    CPUPercent      float64 `json:"cpu_percent"`
    MemUsage        int64   `json:"mem_usage"`
    MemLimit        int64   `json:"mem_limit"`
    RestartCount    int     `json:"restart_count"`
    UpdateAvailable *bool   `json:"update_available,omitempty"`
    // Stack name comes from a single label key; see AD-012.
    Labels          map[string]string `json:"labels,omitempty"`
}
```

> **Important constraint on the projection struct.** The `Labels` field is
> retained on the *server-side* projection for one reason only: to extract
> `com.docker.compose.project` for the `Stack` column. The handler MUST read
> only that one key and MUST NOT serialize any other label into the response.
> See AD-012 for the symmetric client-side constraint and a stricter
> server-side alternative (label-strip in SQL) considered and deferred.

**Component-changes delta** (folds into Section 4 row "MODIFY
`internal/server/db/telemetry_repo.go`" and "MODIFY
`internal/server/db/interfaces.go`"):

- `interfaces.go` adds `GetFleetContainerLeaderboard(ctx) ([]FleetContainerProjection, error)`.
- `telemetry_repo.go` implements it with the query above.
- `internal/testutil/mocks.go` implements the new method on `MockTelemetryRepo`.
- `fleet_dashboard.go` (the new handler file) calls
  `h.telemetry.GetFleetContainerLeaderboard(ctx)` and joins to
  `h.devices.List(ctx)` for hostnames; it does **not** call
  `GetAllLatestSnapshots`.

**Test coverage** (folds into Section 8 AC-030 mapping):
- `fleet_dashboard_test.go` adds an assertion on a spy/mock that
  `GetAllLatestSnapshots` was **not** invoked during `FleetContainers`.
- The mock returns a fixture with exactly the projected shape; the handler
  must produce the documented `FleetContainerRow` JSON without ever touching
  the full snapshot blob.

**Consequences**:
- One additional repo method (small, mirrors the existing
  `GetAllLatestSummaries` pattern). No schema change. No new index.
- AD-006's note that "no DB-layer change" is **superseded** by this addendum;
  the leaderboard now has a dedicated DB-layer method.
- Rejects a future regression where someone re-introduces full-blob decode
  for the leaderboard: the QA-verified absence of `GetAllLatestSnapshots`
  in the leaderboard handler test is the guard.

### AD-012: Cached-telemetry projection allowlist for the leaderboard (resolves SEC-002)

**Decision**: `ContainerLeaderboard.tsx` and any helper or hook that hydrates
the leaderboard from the WS-fed `telemetry` cache MUST project the cached
container records to a strict allowlist of fields. The container `Labels` map
MUST NOT be referenced anywhere in the leaderboard's rendering or state, except
for one read of the single label key `com.docker.compose.project` to derive
the `stack` column. No other label key may flow into UI state, render output,
React Query cache shape, or component props.

**Engineer instruction (verbatim, to be cited in the impl report):**

> ContainerLeaderboard.tsx and any helper that hydrates from the WS telemetry
> cache must project to a strict allowlist of fields:
> `{ device_id, hostname, container_id, container_name, image, stack, state,
> cpu_percent, mem_usage, mem_limit, restart_count, update_available }`.
> The `Labels` map must not be referenced. The single allowed read is
> `labels['com.docker.compose.project']` solely to derive `stack`, and only
> within the projection function. No `Labels` value may reach component state,
> props, JSX, logs, or any cache key.

**Implementation pattern**:

```ts
// web/src/pages/dashboard/ContainerLeaderboard.tsx (illustrative — engineer
// follows this contract; do not copy as code, write it idiomatically).
function projectContainer(
  device: Device,
  c: CachedContainer,            // from useDevices() telemetry cache
): FleetContainerRow {
  return {
    device_id:       device.id,
    hostname:        device.hostname,
    container_id:    c.id,
    container_name:  c.name,
    image:           c.image,
    stack:           c.labels?.['com.docker.compose.project'] ?? '',
    state:           c.state,
    cpu_percent:     c.cpu_percent ?? 0,
    mem_usage:       c.mem_usage ?? 0,
    mem_limit:       c.mem_limit ?? 0,
    restart_count:   c.restart_count ?? 0,
    update_available: c.update_available,
  };
}
```

**Rationale**:
- The WS `telemetry` broadcast retains the full `Labels` map per
  `stripHeavyTelemetry` (`internal/server/handlers/handlers.go:1116`). This is
  pre-existing behavior; hardening that broadcast is a separate follow-up
  story (see SEC-002's stated separate hardening recommendation).
- Constraining the **consumer** is the correct in-scope mitigation. It
  prevents a future regression — a debug `JSON.stringify(container)`, a
  "show all labels" tooltip, or an inadvertent label-key iteration — from
  exfiltrating sensitive label content (Traefik basicauth strings,
  env-as-label patterns, build metadata).
- A whitelisted projection function is mechanically grep-verifiable.

**Test / QA contract** (folds into Section 8 AC-019/AC-020/AC-021 mapping;
also added explicitly in the AC-mapping addendum block in Section 8):
- `ContainerLeaderboard.test.tsx` adds a test labeled
  `[AD-012] Sensitive labels are not surfaced or stored`. The fixture seeds a
  container with labels like
  `traefik.http.middlewares.auth.basicauth.users=admin:$apr1$secret` and
  asserts:
  1. The rendered DOM does not contain the substring
     `traefik.http.middlewares.auth.basicauth.users` or
     `apr1$secret`.
  2. The component's exposed-for-test row data structure does not have a
     `labels` field, or — if `labels` is present for the projection function
     only — its serialized form contains no key other than
     `com.docker.compose.project`.
- QA verifies by grep that the string `Labels[` (or `.labels[` / `['Labels']`)
  does not appear in any file under `web/src/pages/dashboard/`,
  `web/src/hooks/useFleetMetrics.ts`, `web/src/hooks/usePerDevicePulse.ts`,
  or `web/src/hooks/useDashboardTick.ts`, **except** for the single line in
  the `projectContainer` function above that reads
  `labels?.['com.docker.compose.project']`.

**Consequences**:
- One disciplined projection function, one focused unit test. No runtime cost.
- A future "show labels" affordance becomes an explicit ADD-level decision —
  it can no longer be added accidentally via copy-paste from the cache.

### AD-013: `window` parameter — strict reject-on-anything-not-allowlisted grammar (resolves SEC-003)

**Decision**: The `window` query parameter on
`GET /api/v1/fleet/heartbeats` follows a strict allowlist grammar. The server
**rejects** with HTTP `400` for any input that does not parse cleanly to a
duration in the open interval `(0, 60m]`. The server never silently clamps.

**Acceptance grammar (Go-style spec, implement verbatim)**:

```go
// internal/server/handlers/fleet_dashboard.go
//
// Allowed grammar: ^[1-9][0-9]{0,4}[ms]$
//   - exactly one of the suffixes 'm' (minutes) or 's' (seconds)
//   - numeric prefix 1..99999, no leading zero, no sign, no whitespace,
//     ASCII digits only, no Unicode digits, no decimal point, no exponent
//   - maximum 5 prefix digits to bound input size before parsing
//
// Bounds (after parse, applied to the resulting time.Duration):
//   - duration > 0
//   - duration <= 60 * time.Minute
//
// Default behavior:
//   - Parameter MISSING (no `window` key in the query string)         -> default 60 * time.Minute
//   - Parameter PRESENT BUT EMPTY (`?window=`)                        -> 400
//   - Anything else not matching the grammar                          -> 400
//   - Grammar matches but resolved duration > 60m (e.g. "61m","3601s")-> 400
//   - Grammar matches but resolved duration <= 0 (impossible per      -> 400 (defensive)
//     grammar, listed for completeness)
//
// Concrete table:
//   "60m"            -> 60m  OK
//   "1m"             ->  1m  OK
//   "3600s"          -> 60m  OK (3600 * time.Second)
//   "1s"             ->  1s  OK
//   missing          -> 60m  OK (default)
//   ""               -> 400  reject (empty parameter is not the same as missing)
//   "0m"             -> 400  reject (zero duration)
//   "0s"             -> 400  reject (zero duration)
//   "01m"            -> 400  reject (leading zero — grammar requires [1-9])
//   "60M"            -> 400  reject (uppercase suffix not allowed)
//   "60h"            -> 400  reject (hour suffix not allowed)
//   "60min"          -> 400  reject (multi-char suffix not allowed)
//   "60"             -> 400  reject (missing unit)
//   "m"              -> 400  reject (missing prefix)
//   "61m"            -> 400  reject (over cap)
//   "3601s"          -> 400  reject (over cap)
//   "100000m"        -> 400  reject (prefix exceeds 5 digits)
//   "9999999999m"    -> 400  reject (prefix exceeds 5 digits; never reaches strconv.Atoi)
//   "-1m"            -> 400  reject (sign not in grammar)
//   "+1m"            -> 400  reject (sign not in grammar)
//   "1.5m"           -> 400  reject (decimal not in grammar)
//   "1e2m"           -> 400  reject (exponent not in grammar)
//   " 60m"           -> 400  reject (leading whitespace)
//   "60m "           -> 400  reject (trailing whitespace)
//   "60m,30m"        -> 400  reject (multiple values; chi r.URL.Query().Get returns first,
//                                    but reject if value contains a comma)
//   "abc"            -> 400  reject
//   "６０ｍ" (full-width) -> 400  reject (non-ASCII digits/letters)
//   non-ASCII bytes  -> 400  reject
//
// Error response:
//   HTTP 400
//   Body: { "error": "invalid window parameter" }
//   Log : slog.Warn("invalid window parameter", slog.String("raw", raw))
//          (raw value logged at warn for diagnostics — bounded to 32 bytes
//           before logging to defend the log against absurd inputs)
//
// Implementation note:
//   1. Read raw := r.URL.Query().Get("window").
//   2. If raw == "" AND the key is absent from r.URL.Query(): use default 60m.
//      (Detect by checking r.URL.Query().Has("window"); chi/stdlib supports this.)
//   3. Else: validate raw against the regex ^[1-9][0-9]{0,4}[ms]$. If no match,
//      return 400.
//   4. Parse the prefix into int via strconv.Atoi (cannot overflow due to grammar).
//   5. Multiply by time.Minute or time.Second based on the suffix.
//   6. If duration <= 0 || duration > 60*time.Minute: return 400.
//   7. since := time.Now().UTC().Add(-duration); pass to GetFleetHeartbeats.
```

**Whole-endpoint-family policy**: The same grammar and reject-not-clamp policy
applies to any future `window`-style parameter on the `/api/v1/fleet/*`
endpoint family added under FLEET-DASH or its successors. The 60-minute cap is
specific to this story; future endpoints may declare a different cap, but the
acceptance grammar (positive integer, single-char unit `m`|`s`, no leading
zero, max 5 prefix digits, no whitespace) is reused.

**Acknowledged divergence from per-device endpoint**: The existing
`GET /api/v1/devices/{id}/heartbeats` accepts `hours` up to 168. That endpoint
queries one device and is not in the fan-out hot path. The fleet endpoint
caps at 60 minutes deliberately to prevent combinatorial blowup across the
fleet. This divergence is intentional; it must be preserved through future
edits and is documented inline in the handler so reviewers do not "fix" the
divergence by accident.

**Test matrix** (folds into Section 8 AC-030 mapping; goes into
`fleet_dashboard_test.go` under
`describe('[AD-013] window parameter validation', …)`):

The implementation MUST have a table-driven test asserting the exact behavior
for every row in the concrete table above. At minimum the following cases are
mandatory: missing, empty, `60m`, `1m`, `3600s`, `1s`, `0m`, `0s`, `01m`,
`60M`, `60h`, `60min`, `60`, `m`, `61m`, `3601s`, `100000m`, `9999999999m`,
`-1m`, `+1m`, `1.5m`, `1e2m`, `" 60m"`, `"60m "`, `"60m,30m"`, `abc`,
`"６０ｍ"` (full-width), and one case asserting that the resolved `since`
timestamp equals `now - duration` to within a small epsilon.

**Consequences**:
- Closes SEC-003 with a single, fully specified parser. No silent clamping
  anywhere. Any future change to bounds requires an ADD update.
- The bounded prefix length (max 5 digits) defeats absurd inputs at the
  grammar layer, before `strconv` ever runs — eliminating overflow as an
  attack surface.
- Section 6 ("API / Interface Contract") and Section 10 ("Security
  Considerations") are updated by reference to point to this addendum for the
  authoritative grammar.

---

### Summary of finding resolution

| Finding | Status   | Decision                                                                                   |
|---------|----------|--------------------------------------------------------------------------------------------|
| SEC-001 | Resolved | AD-011 — new `GetFleetContainerLeaderboard` projected JSONB query; no full-blob decode.    |
| SEC-002 | Resolved | AD-012 — leaderboard projects WS-cached telemetry to a strict allowlist; `Labels` map not referenced except a single read of `com.docker.compose.project` for `stack`. |
| SEC-003 | Resolved | AD-013 — strict reject-not-clamp grammar `^[1-9][0-9]{0,4}[ms]$` bounded to `(0, 60m]`, full edge-case table specified for engineering and QA. |
