# Architecture Decision Document

- **Story ID:** FLEET-NET
- **FRD Reference:** `docs/requirements/FLEET-NET-frd.md`
- **Author:** Architect Agent
- **Date:** 2026-04-27
- **Status:** FINAL

---

## 1. Summary

Extend `models.HeartbeatData` with two `omitempty` byte-per-second fields
(`NetRxBytesPerSec`, `NetTxBytesPerSec`) computed agent-side as fleet-summed
deltas across all non-loopback interfaces, mirroring the existing disk I/O
rate precedent in `internal/agent/heartbeat.go`. On the dashboard, the FRD's
four-chart small-multiples row no longer exists (replaced by per-device cards
in v2.60.0); we satisfy the FRD's *intent* by rendering a second, smaller
stacked-area chart underneath each device card's existing 4-line percent chart,
with a bytes/sec Y-axis (Option B). Schema is unchanged — heartbeats are
JSONB and tolerate the new keys without migration.

---

## 2. Technical Context

### Existing patterns we are reusing

- **Heartbeat assembly** lives in `internal/agent/heartbeat.go`
  (`Agent.sendHeartbeat`). Per-interval rate computation is performed *here*,
  not inside the corresponding collector. The disk-rate pattern uses
  `disk.IOCountersWithContext` directly, snapshots the result into
  `a.prevDiskIO map[string]diskIOSnapshot`, captures the wall-clock time
  in `a.prevDiskIOTime`, and computes deltas on the next heartbeat:

  ```
  // internal/agent/heartbeat.go:57–103
  if counters, err := disk.IOCountersWithContext(ctx); err == nil {
      ...
      if a.prevDiskIO != nil && !a.prevDiskIOTime.IsZero() {
          elapsed := now.Sub(a.prevDiskIOTime).Seconds()
          if elapsed > 0 { ... }
      }
      a.prevDiskIO = curr
      a.prevDiskIOTime = now
  }
  ```

  Per-counter rollover is guarded with `cur >= prev` checks. Elapsed time is
  guarded with `elapsed > 0`. State lives on the `Agent` struct
  (`internal/agent/agent.go:25–48`), declared right next to `prevDiskIO` /
  `prevDiskIOTime`.

- **Network collector** in `internal/agent/collectors/network.go` already
  uses `psnet.IOCountersWithContext(ctx, true)` (gopsutil v3) with
  `pernic=true`. It does not currently compute rates; it only emits cumulative
  counters as part of full telemetry. We will not modify the collector for
  this story (FRD §9 explicitly excludes "Changes to the agent's full
  telemetry network collector").

- **Heartbeat ingestion** in `internal/server/handlers/handlers.go:240–280`
  decodes the body into `models.Heartbeat` (a struct), then persists
  `hb.Data` as JSONB via `TelemetryRepo.StoreHeartbeat`
  (`internal/server/db/telemetry_repo.go:30–37`). Unknown JSON keys are
  silently ignored by the standard library decoder; missing keys decode to
  Go zero values. Both directions of backward compatibility (NFR-003,
  NFR-004) come for free with the existing handler.

- **Fleet read path** is `GET /api/v1/fleet/heartbeats?window=60m`
  (`internal/server/handlers/fleet_dashboard.go:138–197`). The handler does
  not type-narrow `hb.Data` — it forwards it through `interface{}` so any
  new field on `HeartbeatData` shows up in the response automatically.

- **Per-device card** is `web/src/pages/dashboard/PerDeviceCharts.tsx`. It
  reads `DeviceSeries` from `useFleetMetrics` and renders four lines on a
  single 0–100% recharts `AreaChart`. Each card has its own
  `<ResponsiveContainer height={220}>`.

- **Byte-rate formatting** already exists at
  `web/src/utils/format.ts:formatBytesPerSec` and `formatBytesPerSecAxis`
  with exactly the 1024-based unit-scaling FR-023 specifies. Reuse — do not
  duplicate.

### What is missing / must change

- `HeartbeatData` struct has no `NetRxBytesPerSec` / `NetTxBytesPerSec`.
- Agent struct has no `prevNetCounters` / `prevNetCountersTime`.
- TypeScript `HeartbeatData` mirror lacks the two fields.
- `DeviceSeries` lacks `netRxPoints` / `netTxPoints`.
- `useFleetMetrics` does not project the new fields onto the series.
- `PerDeviceCharts.tsx` renders only one chart per card; we need a second.
- `CHANGELOG.md:71` and `README.md:1139` describe a now-stale "deferred"
  state and must be removed (FR-026, FR-027). A new CHANGELOG entry under
  the FLEET-NET version is required (FR-028).

### FRD-vs-current-codebase reconciliation (mandatory per prompt)

The FRD was authored against the v2.59.0 dashboard shape. Two specific
requirements quote that obsolete shape:

> **FR-018:** The dashboard at `/dashboard` must render a fourth small-multiples
> chart titled "Network in/out" alongside the existing CPU, memory, and disk I/O
> charts.
>
> **FR-019:** The "Network in/out" chart must be visually identical in shape to
> the existing "Disk I/O" chart (a single In/Out fleet-summed pair, stacked or
> paired per the existing `FleetStackedChart` component).
>
> **FR-022:** When the small-multiples row contains four charts, its grid
> layout at `xl` breakpoint must use `xl:grid-cols-4` (restored from the
> current `xl:grid-cols-3`).

Reality on `main` (commit `2cdefc7`, v2.60.0):

- There is no fleet small-multiples row. `web/src/pages/Dashboard.tsx`
  renders three sections: per-device cards, container leaderboard, activity
  river.
- There is no `FleetStackedChart` component anywhere in
  `web/src` (`grep -r FleetStackedChart web/` returns zero hits).
- The "Disk I/O" small-multiples chart no longer exists; disk pressure is
  represented as a percent line on each device card.
- The grid breakpoint is `xl:grid-cols-2` (per-device card grid), not 3 or 4.
- README §"Network throughput line deferred" (line 1139) explicitly
  describes the post-redesign intent: "A follow-up story (FLEET-NET) will
  extend the heartbeat schema and add a network throughput line to each card."

We satisfy the FRD's **intent** — surface fleet network throughput on the
dashboard so operators can see it without leaving the home page — by adding
per-card network rendering. The fleet-summed view of FR-018/FR-022 is not
recoverable as written; the stale small-multiples row would have to be
reintroduced and the per-card redesign torn out. That is out of scope. AD-005
documents the remap: AC-012/AC-013/AC-014 are reinterpreted as per-card
behaviors.

---

## 3. Architecture Decisions

### AD-001: Heartbeat field names mirror the disk-rate precedent (`net_rx_bytes_sec` / `net_tx_bytes_sec`)

**Decision.** Add two fields to `models.HeartbeatData`:

| Go field             | Type      | JSON tag                                    |
|----------------------|-----------|---------------------------------------------|
| `NetRxBytesPerSec`   | `float64` | `net_rx_bytes_sec,omitempty`                |
| `NetTxBytesPerSec`   | `float64` | `net_tx_bytes_sec,omitempty`                |

**Rationale.** The user prompt directly delegates this naming and explicitly
recommends mirroring `disk_read_bytes_sec` / `disk_write_bytes_sec` (existing
JSON tags on lines 15–16 of `internal/models/telemetry.go`). Using
`net_rx_bytes_sec` / `net_tx_bytes_sec` keeps the heartbeat schema internally
consistent — every "rate" field uses the same `_bytes_sec` suffix. `float64`
also matches the disk-rate type exactly.

**Alternatives considered.**

- *Use the FRD's `net_rx_bytes_per_sec` / `net_tx_bytes_per_sec` tags.*
  Rejected. The FRD itself acknowledges (Assumption A-001) that the
  architect names the fields to match the existing disk-rate template. The
  longer `_per_sec` suffix would be the only field in `HeartbeatData` that
  uses it, breaking the established pattern. The user prompt overrides the
  FRD on this point.
- *Use integer types (`uint64`).* Rejected. Disk rate is `float64` because
  the divisor is `elapsed.Seconds()` (a float). Mixing types here would
  cause silent precision loss when sub-1-byte/sec rates exist.

**Consequences.** AC-001 ("the JSON payload contains a key
`net_rx_bytes_per_sec`") technically references the wrong key string. The
implementation will use `net_rx_bytes_sec`, and the QA engineer should
verify against this ADD's spelling, not the FRD's. This is documented up
front so engineering does not flip back and forth. NFR-002 (≤64 bytes added
to payload) is satisfied: `"net_rx_bytes_sec":1234567.89,"net_tx_bytes_sec":1234567.89`
is ~63 bytes worst-case for realistic rates.

---

### AD-002: Rate computation lives in heartbeat assembly, not the collector

**Decision.** All delta math, snapshotting, rollover guarding, and elapsed-time
guarding happen inline in `Agent.sendHeartbeat` in
`internal/agent/heartbeat.go`, alongside the existing disk-rate block.
`internal/agent/collectors/network.go` is **not** modified.

**Rationale.** This is the proven precedent for `DiskReadBytesPerSec` /
`DiskWriteBytesPerSec`. The heartbeat path (15s cadence) has different
state-lifecycle semantics from the full-telemetry collectors (60s cadence,
plus on-demand pushes via `telemetryNow`). Putting state on the collector
would couple two cadences that the codebase has deliberately kept separate.
The disk-rate code already ignores the collector entirely and calls
`disk.IOCountersWithContext` directly; we mirror that for network.

**Alternatives considered.**

- *Compute the rate in `NetworkCollector.Collect`.* Rejected. The collector
  emits cumulative counters as part of full telemetry; rates are a
  heartbeat concern. Coupling these means the collector has to know about
  the heartbeat clock.
- *Add a new `RateCalculator` abstraction shared by disk and network.*
  Rejected. Premature abstraction with one consumer pair. If a third rate
  comes (e.g. GPU memory bandwidth) we revisit. Not now.

**Consequences.** Two near-identical blocks in `heartbeat.go` (one for disk,
one for network). Tolerable — they're each ~25 LOC and sit next to each
other.

---

### AD-003: Counter source is `gopsutil/v3/net.IOCountersWithContext(ctx, true)`

**Decision.** Use `psnet.IOCountersWithContext(ctx, true)` to read per-interface
byte counters. Do not parse `/proc/net/dev` directly.

**Rationale.** The existing `NetworkCollector` already imports and uses
gopsutil v3 net (`internal/agent/collectors/network.go:11, 26`). The agent
ships across Linux/macOS/Windows/FreeBSD; gopsutil is the established
cross-platform abstraction. Loopback exclusion (AD-004) is name-based, so
there is no information loss versus reading `/proc/net/dev`.

**Alternatives considered.**

- *Read `/proc/net/dev` directly.* Rejected. Linux-only, would need
  per-platform shims. The story does not justify a divergence from the
  collector's existing source.
- *Reuse the collector's already-cached per-tick counters.* Rejected.
  The collector runs on the telemetry cadence (60s), not the heartbeat
  cadence (15s). Reading independently inside the heartbeat path is
  cheaper than wiring up a shared cache that crosses goroutines.

**Consequences.** Each heartbeat performs one syscall sweep over network
interfaces, same as the existing disk-counter call. NFR-001 (≤5 ms for 32
interfaces) is satisfied with margin — gopsutil's IOCounters is implemented
as a single `getifaddrs`/`/proc/net/dev` read.

---

### AD-004: Loopback exclusion is a name-based prefix/exact match performed inline in heartbeat assembly

**Decision.** An interface is excluded from the rate sum iff its name
satisfies `isLoopbackName(name)`, defined as:

```
name == "lo"        // Linux, BSD
name == "lo0"       // macOS, some BSDs
strings.HasPrefix(name, "Loopback")  // Windows (gopsutil reports "Loopback Pseudo-Interface 1" etc.)
```

The check lives in `heartbeat.go` next to the rate computation. There is no
constants list module — three values inline as a small unexported helper.

**Rationale.** This matches the disk-rate precedent of inlining the netFS
filter in `heartbeat.go:44` (`netFS := map[string]bool{"nfs": true, ...}`).
The set is small and unlikely to change. gopsutil normalizes the names per
platform; we cover the three known forms.

**Alternatives considered.**

- *Use `gopsutil.Interfaces[].Flags` to detect the loopback flag.* Rejected.
  Requires a second syscall (`InterfacesWithContext`) for every heartbeat
  just to map names to flags. Name-based detection is one comparison.
- *Push the filter into a shared `internal/agent/iface_filter.go`.*
  Rejected. Single consumer. Inline is more local.

**Consequences.** A user-renamed interface called `lo-fancy` would slip
past. Acceptable — that is genuinely user-renamed loopback, which is
exotic. AC-007 only requires standard `lo` exclusion.

---

### AD-005: Dashboard rendering — Option B (separate small chart per device card with bytes/sec Y-axis)

**Decision.** Add a second `<ResponsiveContainer>` underneath the existing
4-line percent chart in each device card. The new chart is a recharts
stacked area chart with two series ("In", "Out"), bytes/sec Y-axis, formatted
via `formatBytesPerSecAxis`, with a tooltip using `formatBytesPerSec`.

**Rationale.** The user prompt offered three options and recommended B; I
agree.

- *Option A (5th line on the 0–100% chart):* "% of recent peak per device"
  is a non-standard unit. Operators expect to see "5 MB/s", not "82% of
  yesterday's peak". Composing fleet-wide views from per-device-normalized
  percentages is also impossible — you cannot meaningfully sum percentages
  with different denominators. Rejected.
- *Option C (dual Y-axis on the same chart):* Two-axis charts are
  notoriously hard to read; user prompt agrees. Rejected.
- *Option B (separate sub-chart):* Real units (bytes/sec) on a dedicated
  axis, no denominator gymnastics, no axis collision with the percent
  chart. The card grows ~120 px taller — acceptable on the per-device card
  layout that already uses `xl:grid-cols-2`.

**Alternatives considered.** See above.

**Consequences.**
- Card height grows from ~270 px to ~390 px. The dashboard scrolls more on
  large fleets. Acceptable; it is the home page, not a single-screen
  status board.
- The FRD's FR-022 ("`xl:grid-cols-4`") is not satisfied as written. AD-005
  reinterprets FR-018/FR-019/FR-022 against the current per-card shape.
  See §8 for the AC remap.
- The two charts on a single card share the same X-axis range
  (60-min window, same MAX_POINTS=240 sliding buffer). Time alignment is
  visual but not enforced via a shared axis instance — recharts does not
  support cross-chart synced axes without bespoke code, and the visual
  alignment is sufficient for this dashboard.

---

### AD-006: First-heartbeat baseline & state storage on the `Agent` struct

**Decision.** Add two unexported fields to `internal/agent/agent.go`'s
`Agent` struct, declared immediately after `prevDiskIO` / `prevDiskIOTime`:

```
prevNetCounters     map[string]netIOSnapshot
prevNetCountersTime time.Time
```

…where `netIOSnapshot` is a new unexported struct in `agent.go`:

```
type netIOSnapshot struct {
    BytesRecv uint64
    BytesSent uint64
}
```

On the first heartbeat: `prevNetCounters == nil` and `prevNetCountersTime`
is zero — emit `0` for both fields, then store `curr` and `now`. Same
nil-guard pattern as `prevDiskIO`.

**Rationale.** Mirrors the disk precedent verbatim. State is goroutine-local
(only the `heartbeatLoop` goroutine touches these), so no mutex is
required. The disk-rate code does not lock either.

**Alternatives considered.**

- *Persist counters to disk so cold restarts have a baseline.* Rejected.
  The disk-rate path doesn't do this either, and the FRD explicitly accepts
  zero on cold start (FR-007, AC-002).
- *Module-level package state.* Rejected. The agent is designed as a
  single instance per process with state on the struct; package-level
  globals would break the test pattern.

**Consequences.** A second goroutine that calls `sendHeartbeat`
concurrently would race. None exists today; the heartbeat loop is the only
caller. If that ever changes, both the disk and network state need to be
moved behind a mutex (or onto the loop's stack). Out of scope here.

---

### AD-007: Counter rollover, missing/added interfaces, and clock-jump handling

**Decision.** All three failure modes are handled inline in heartbeat
assembly with explicit checks:

1. **Per-interface rollover (`cur < prev`).** Skip that interface's
   contribution for the interval (contributes `0` to the sum). Still
   update the snapshot for the next interval. Mirrors the disk pattern
   `if cur.ReadBytes >= prev.ReadBytes`.
2. **Interface present in prev but not curr (interface removed).** It
   simply does not appear in the iteration over `curr`; its prior counters
   are dropped from the snapshot. Contributes `0` automatically.
3. **Interface present in curr but not prev (interface added).** The
   per-interface lookup `prev, ok := prevNet[name]` returns `ok == false`;
   the code falls through without contributing. The new interface's
   counters are stored in the snapshot for the next interval. Contributes
   `0` automatically.
4. **Elapsed time `<= 0` (clock jump backwards or zero-duration tick).**
   Skip the entire delta block; emit `0` for both fields. Still update
   the snapshot/time. Mirrors the disk pattern's `if elapsed > 0` guard.

**Rationale.** All four behaviors are stated in the FRD (FR-010, FR-011,
FR-012, FR-014) and they line up exactly with the disk precedent.

**Consequences.** None of these cases panic or log at INFO+ (NFR-005,
NFR-007). DEBUG-level logging is permitted but not required; we omit it
for now to keep the hot path lean.

---

### AD-008: Server-side and storage — JSONB tolerates the new fields, no migration

**Decision.** No SQL migration. The server's heartbeat handler decodes
`models.Heartbeat` (the wrapper struct), serializes `hb.Data` back to JSON
in `TelemetryRepo.StoreHeartbeat`, and stores it in the `heartbeats.data`
JSONB column. Adding fields to `HeartbeatData` is automatically reflected
in both the inbound decode and the outbound JSONB write.

**Rationale.**

- Inbound: standard library `json.Decoder` populates new fields when
  present, leaves zero-values when absent (NFR-003 — old agent → new
  server, AC-010).
- Outbound to old DB rows: `json.Unmarshal` into the new struct treats
  missing keys as Go zero values (`float64(0)`); the dashboard reads
  `0`. AC-016 (all-old-agent fleet → chart shows zero) is satisfied
  trivially.
- New agent → old server: the old server's `HeartbeatData` struct lacks
  the new fields; the decoder silently drops unknown keys (NFR-004,
  AC-011).

**Alternatives considered.**

- *Promote net rates to dedicated columns for query efficiency.* Rejected.
  Not justified by the read pattern; the dashboard reads the JSONB blob
  in full anyway via `GetFleetHeartbeats`.
- *Add a JSONB GIN index on the new keys.* Rejected. Same — no per-key
  query exists.

**Consequences.** Schema migration list for this story is empty.
`omitempty` on both fields keeps the heartbeat payload minimum size
identical for old agents (no key emitted at all when value is `0`).

---

### AD-009: Dashboard data flow — extend `DeviceSeries` in place; no sibling struct

**Decision.** Add two fields to the existing `DeviceSeries` interface in
`web/src/hooks/useFleetMetrics.ts`:

```
netRxPoints: ChartPoint[]
netTxPoints: ChartPoint[]
```

Populate them in `useFleetMetrics` from `h.data.net_rx_bytes_sec ?? 0` and
`h.data.net_tx_bytes_sec ?? 0`, alongside the existing `cpuPoints` /
`memPoints` / `diskPoints` / `loadPoints`. The same `MAX_POINTS=240`
rolling window applies. The `?? 0` fallback satisfies AC-016.

**Rationale.** A sibling `DeviceNetSeries` struct would force
`useFleetMetrics` to return two arrays and `PerDeviceCharts.tsx` to thread
them in lockstep. The existing struct already groups all per-device
time-series; adding two more keys is the smallest change and keeps
`devicesById` lookups single-pass.

**Alternatives considered.**

- *Sibling `DeviceNetSeries[]` returned alongside `perDeviceSeries`.*
  Rejected — requires zip-by-deviceId in the consumer.
- *A second hook `useFleetNetSeries`.* Rejected — duplicates the
  WS/REST plumbing for no benefit.

**Consequences.** `Dashboard.test.tsx` already constructs a `BASE_DEVICE_SERIES`
fixture with explicit keys; the test fixture must add `netRxPoints: []` and
`netTxPoints: []` (or the strict TS check on the mock will fail).

---

## 4. Component Changes

| Action | File | Purpose |
|--------|------|---------|
| MODIFY | `internal/models/telemetry.go` | Add `NetRxBytesPerSec` and `NetTxBytesPerSec` fields to `HeartbeatData`, both `float64` with `omitempty` JSON tags `net_rx_bytes_sec` / `net_tx_bytes_sec` (AD-001). |
| MODIFY | `internal/agent/agent.go` | Add `prevNetCounters map[string]netIOSnapshot` and `prevNetCountersTime time.Time` fields to `Agent` struct; declare `netIOSnapshot` struct (AD-006). |
| MODIFY | `internal/agent/heartbeat.go` | Add network-rate computation block after the disk-rate block: read `psnet.IOCountersWithContext(ctx, true)`, filter loopback by `isLoopbackName`, compute deltas with rollover/added/removed/elapsed-≤-0 guards, populate `data.NetRxBytesPerSec` / `data.NetTxBytesPerSec`, update snapshot state (AD-002, AD-003, AD-004, AD-007). Add unexported `isLoopbackName(string) bool` helper at file scope. |
| CREATE | `internal/agent/heartbeat_test.go` | Unit tests for the rate computation. Must extract the rate-math into a small testable pure function (see Implementation Notes §12) or use a synthetic `Agent` fixture and inject snapshots directly. Cover all NFR-006 cases: cold start, normal interval, rollover, interface added, interface removed, loopback excluded, zero/negative elapsed (AD-007, AC-002 through AC-009 mapping). |
| MODIFY | `internal/server/handlers/handlers.go` | No code change required (handler already passes `hb.Data` through unchanged). Add a comment block above `Heartbeat` referencing FLEET-NET to document why no validation is needed. *(Optional — engineer may skip if comments feel like noise.)* |
| CREATE | `internal/server/handlers/heartbeat_compat_test.go` | Server-side integration test: POST a heartbeat with no `net_rx_bytes_sec` / `net_tx_bytes_sec` keys; assert 2xx; assert that the resulting `Heartbeat.Data` has both fields equal to `0` (AC-010, AC-019). Use the existing test harness used by other handler tests in this directory. |
| MODIFY | `web/src/types/models.ts` | Add `net_rx_bytes_sec?: number` and `net_tx_bytes_sec?: number` to the `HeartbeatData` interface (AD-001). |
| MODIFY | `web/src/hooks/useFleetMetrics.ts` | Add `netRxPoints: ChartPoint[]` and `netTxPoints: ChartPoint[]` to `DeviceSeries`. Populate from heartbeats (initial REST seed, WS append, new-device path). Apply `?? 0` fallback per AC-016. (AD-009) |
| MODIFY | `web/src/pages/dashboard/PerDeviceCharts.tsx` | Add a second `<ResponsiveContainer>` underneath the main chart in `DeviceChartCard`. Render two stacked areas (In = `netRxPoints`, Out = `netTxPoints`) with bytes/sec Y-axis. Use `formatBytesPerSecAxis` for ticks, `formatBytesPerSec` in tooltip. Add `data-testid="device-net-chart-${deviceId}"` for tests. (AD-005, AC-012/013/014/015 remap) |
| MODIFY | `web/src/pages/dashboard/Dashboard.test.tsx` | Update `BASE_DEVICE_SERIES` fixture to include `netRxPoints: []` and `netTxPoints: []`. Add a test asserting `device-net-chart-dev-1` renders. Add a test asserting the chart still renders when both series are zero (AC-016). |
| CREATE | `web/src/pages/dashboard/PerDeviceCharts.test.tsx` *(if absent)* | Component-level tests for the new sub-chart: tooltip-format check (AC-015 remap), zero-value rendering (AC-016 remap). If a test file already covers `PerDeviceCharts` indirectly via `Dashboard.test.tsx`, the engineer may extend that file instead. |
| MODIFY | `CHANGELOG.md` | Remove line 71 (FLEET-DASH "deferred network throughput line" note). Add a new entry under the FLEET-NET release version (FR-026, FR-028, AC-017). The entry must list both the agent heartbeat schema change (`net_rx_bytes_sec`, `net_tx_bytes_sec`) and the dashboard per-card network sub-chart. |
| MODIFY | `README.md` | Remove the "Network throughput line deferred" bullet at line 1139 (FR-027, AC-017). Optionally add a single sentence noting that per-device cards now include network throughput, but do not introduce a new section. |

> Files explicitly **not** modified: `internal/agent/collectors/network.go`,
> any database migration directory, the WebSocket hub, the events generator.
> The events generator's `CheckHeartbeatThresholds` does not currently
> threshold network rate; alerting on network throughput is FRD §9 out of
> scope.

---

## 5. Data Model Changes

### Schema migration — none

**Justification.** `heartbeats.data` is a JSONB column. Adding two
`omitempty float64` fields to `HeartbeatData` does not change the storage
format — pgx serializes the new fields when present, and old rows without
them deserialize to Go zero values. No `cmd/riot-server/migrations/*.sql`
file is added, modified, or removed.

### `HeartbeatData` struct — before / after

**Before** (`internal/models/telemetry.go:9–20`):

```
type HeartbeatData struct {
    Uptime              uint64  `json:"uptime"`
    CPUPercent          float64 `json:"cpu_percent"`
    MemPercent          float64 `json:"mem_percent"`
    LoadAvg1m           float64 `json:"load_avg_1m"`
    DiskRootPercent     float64 `json:"disk_root_percent"`
    DiskReadBytesPerSec float64 `json:"disk_read_bytes_sec,omitempty"`
    DiskWriteBytesPerSec float64 `json:"disk_write_bytes_sec,omitempty"`
    DiskIOPercent       float64 `json:"disk_io_percent,omitempty"`
    AgentVersion        string  `json:"agent_version,omitempty"`
    LogErrors           int     `json:"log_errors,omitempty"`
}
```

**After**:

```
type HeartbeatData struct {
    Uptime              uint64  `json:"uptime"`
    CPUPercent          float64 `json:"cpu_percent"`
    MemPercent          float64 `json:"mem_percent"`
    LoadAvg1m           float64 `json:"load_avg_1m"`
    DiskRootPercent     float64 `json:"disk_root_percent"`
    DiskReadBytesPerSec float64 `json:"disk_read_bytes_sec,omitempty"`
    DiskWriteBytesPerSec float64 `json:"disk_write_bytes_sec,omitempty"`
    DiskIOPercent       float64 `json:"disk_io_percent,omitempty"`
    NetRxBytesPerSec    float64 `json:"net_rx_bytes_sec,omitempty"`
    NetTxBytesPerSec    float64 `json:"net_tx_bytes_sec,omitempty"`
    AgentVersion        string  `json:"agent_version,omitempty"`
    LogErrors           int     `json:"log_errors,omitempty"`
}
```

Field placement: directly after `DiskIOPercent` and before `AgentVersion`,
keeping all rate fields contiguous.

### New unexported agent struct — `netIOSnapshot`

```
type netIOSnapshot struct {
    BytesRecv uint64
    BytesSent uint64
}
```

Placed in `internal/agent/agent.go` immediately after the existing
`diskIOSnapshot` struct.

### `Agent` struct additions

```
prevNetCounters     map[string]netIOSnapshot
prevNetCountersTime time.Time
```

Placed immediately after `prevDiskIO` and `prevDiskIOTime`.

### TypeScript `HeartbeatData` mirror

Add two optional numeric fields:

```
net_rx_bytes_sec?: number
net_tx_bytes_sec?: number
```

### `DeviceSeries` interface

```
export interface DeviceSeries {
  deviceId: string
  hostname: string
  cpuPoints: ChartPoint[]
  memPoints: ChartPoint[]
  diskPoints: ChartPoint[]
  loadPoints: ChartPoint[]
  netRxPoints: ChartPoint[]   // NEW
  netTxPoints: ChartPoint[]   // NEW
}
```

---

## 6. API / Interface Contract

### `POST /api/v1/devices/{id}/heartbeat`

No path, method, header, or status-code change. The request body's `data`
object gains two **optional** numeric keys.

**Request body — incremental delta only:**

```
{
  "device_id": "...",
  "timestamp": "...",
  "data": {
    ...existing keys...,
    "net_rx_bytes_sec": 12345.67,   // optional, float, ≥ 0
    "net_tx_bytes_sec": 8910.11     // optional, float, ≥ 0
  }
}
```

Both keys are absent when the agent has no rate to report (cold start,
post-rollover, post-clock-jump). They are also absent on pre-FLEET-NET
agents.

**Response shape — unchanged.** Existing `200 {"status":"ok",
"pending_commands":[...]}` body stands.

**Server validation behavior.** The server does **not** validate the new
fields. `omitempty` and Go's zero-value semantics make `null`, missing key,
and `0` all behaviorally equivalent downstream. Negative values would be a
contract violation by the agent (FR-004); we do not defensively clamp at
the server because the agent's rollover/elapsed guards make it impossible
to emit a negative value, and adding server-side clamping would only mask
a bug.

### `GET /api/v1/fleet/heartbeats?window=60m`

No contract change. The response's `devices.<id>[].data` object surfaces
the new keys when present in the underlying JSONB blob. Client code reads
`data.net_rx_bytes_sec ?? 0` and `data.net_tx_bytes_sec ?? 0`.

### WebSocket `heartbeat` frame

No contract change. `Hub.BroadcastHeartbeat` forwards `*HeartbeatData`
verbatim; the new fields ride through.

---

## 7. Sequence / Flow

### Agent — heartbeat assembly with new fields

1. `heartbeatLoop` ticks (every 15s by default).
2. `sendHeartbeat(ctx)` builds a fresh `models.HeartbeatData{}`.
3. Existing CPU / memory / uptime / load / disk percent code runs unchanged.
4. Disk I/O rate block runs unchanged (lines 57–103 of `heartbeat.go`).
5. **NEW** Network I/O rate block runs:
   1. Call `psnet.IOCountersWithContext(ctx, true)`. If error: skip the
      entire block (do not touch `prevNetCounters` or `prevNetCountersTime`).
   2. `now := time.Now()`.
   3. Build `curr := map[string]netIOSnapshot{}` from the result, **excluding
      any interface where `isLoopbackName(name) == true`**.
   4. If `a.prevNetCounters != nil && !a.prevNetCountersTime.IsZero()`:
      1. `elapsed := now.Sub(a.prevNetCountersTime).Seconds()`.
      2. If `elapsed > 0`:
         1. `var totalRxDelta, totalTxDelta uint64`.
         2. For each `(name, cur)` in `curr`:
            1. If `prev, ok := a.prevNetCounters[name]; ok`:
               1. If `cur.BytesRecv >= prev.BytesRecv`: `totalRxDelta += cur.BytesRecv - prev.BytesRecv`.
               2. If `cur.BytesSent >= prev.BytesSent`: `totalTxDelta += cur.BytesSent - prev.BytesSent`.
               3. (Else: per-interface rollover — contribute 0; loop continues.)
            2. (Else: new interface — contribute 0; loop continues.)
         3. `data.NetRxBytesPerSec = float64(totalRxDelta) / elapsed`.
         4. `data.NetTxBytesPerSec = float64(totalTxDelta) / elapsed`.
      3. Else (`elapsed <= 0`): leave both fields at zero-value (`0`).
   5. (Else: cold start — leave both fields at zero-value.)
   6. `a.prevNetCounters = curr`.
   7. `a.prevNetCountersTime = now`.
6. `data.LogErrors = int(a.logErrors.Swap(0))` — unchanged.
7. Existing send + pending-command handling — unchanged.

### Dashboard — network sub-chart render

1. `Dashboard.tsx` mounts.
2. `useFleetMetrics(devices, events)` issues `GET /api/v1/fleet/heartbeats?window=60m`.
3. Response is mapped into `DeviceSeries[]` including `netRxPoints` /
   `netTxPoints` populated with `?? 0` fallbacks.
4. `useWebSocket` populates `hbBufferRef`. Every 5 s `useDashboardTick`
   fires; the hook appends one new `ChartPoint` to each of the six
   per-device series, capped at `MAX_POINTS=240`.
5. `PerDeviceCharts` renders one card per device. Each card now renders two
   `ResponsiveContainer`s:
   - The existing 4-line percent chart (height 220, 0–100% Y-axis).
   - **NEW** A two-area stacked chart (height 100, bytes/sec Y-axis,
     `formatBytesPerSecAxis` for ticks, `formatBytesPerSec` for tooltip
     values). Axis label `"B/s"` (the unit-string text uses bytes
     vocabulary, satisfying FR-024 / AC-015's bytes-per-second requirement).

---

## 8. Acceptance Criteria Mapping

> **Reconciliation note.** AC-012, AC-013, and AC-014 reference the
> obsolete fleet small-multiples row. AD-005 reinterprets these per-card.
> Each row below states both the original AC text and the per-card
> remap.

| AC ID  | Fulfilled By | Test Strategy |
|--------|--------------|---------------|
| AC-001 | `internal/models/telemetry.go` (struct fields with JSON tags `net_rx_bytes_sec` / `net_tx_bytes_sec` per AD-001) | `internal/agent/heartbeat_test.go` — assert that a marshaled `HeartbeatData` with non-zero values contains both keys; `internal/server/handlers/heartbeat_compat_test.go` — round-trip JSON. **Note:** field names diverge from FRD spelling per AD-001. |
| AC-002 | `internal/agent/heartbeat.go` cold-start branch (snapshot `nil` → emit 0) | `heartbeat_test.go` — `[AC-002] cold start emits zero rate`: synthetic agent with `prevNetCounters == nil`, run rate logic, expect both fields = 0. |
| AC-003 | `internal/agent/heartbeat.go` normal-interval branch | `heartbeat_test.go` — `[AC-003] normal interval produces R/T rate`: seed prev = {eth0: 1000/2000} at t=0; curr = {eth0: 11000/22000} at t=10s; expect rx=1000, tx=2000. |
| AC-004 | `internal/agent/heartbeat.go` rollover guard (`cur >= prev`) | `heartbeat_test.go` — `[AC-004] counter rollover contributes zero`: prev rx=1000, curr rx=100 → delta contribution = 0; final emitted value ≥ 0. |
| AC-005 | `internal/agent/heartbeat.go` `prev, ok := prevNet[name]; !ok` path | `heartbeat_test.go` — `[AC-005] interface added mid-run contributes zero for that interval`: prev = {eth0}, curr = {eth0, docker0}; rx/tx come only from eth0's delta; assert `prevNetCounters` after the call contains `docker0`. |
| AC-006 | Iteration over `curr` only — vanished interface naturally drops | `heartbeat_test.go` — `[AC-006] interface removed mid-run contributes zero for that interval`: prev = {eth0, wg0}, curr = {eth0}; rx/tx come only from eth0's delta; assert `prevNetCounters` after the call does not contain `wg0`. |
| AC-007 | `isLoopbackName` filter applied during `curr` construction | `heartbeat_test.go` — `[AC-007] loopback excluded from sum`: synthetic counters for `lo`, `lo0`, `eth0`; only `eth0`'s delta is counted. |
| AC-008 | No filter beyond loopback — bridges, virtual NICs, container bridges, VPN tunnels, wireless all included | `heartbeat_test.go` — `[AC-008] non-loopback interfaces of all kinds are summed`: synthetic counters for `eth0`, `br0`, `docker0`, `wg0`, `wlan0`; all five contribute. |
| AC-009 | `if elapsed > 0` guard | `heartbeat_test.go` — `[AC-009] zero-or-negative elapsed time emits zero`: prev time `now + 1s` (clock went backwards) → expect both fields = 0; second test with prev time `== now` → expect both fields = 0. |
| AC-010 | `internal/server/handlers/handlers.go` `Heartbeat` handler (no validation of new fields) + JSONB tolerance | `internal/server/handlers/heartbeat_compat_test.go` — `[AC-010] server accepts heartbeat without net rate fields`: POST body with only existing keys; assert 200; assert no warning log emitted; assert downstream `Heartbeat.Data.NetRxBytesPerSec == 0`. |
| AC-011 | Standard `json.Decoder` ignores unknown keys (existing handler behavior) | Covered by AC-010's round-trip pattern run in reverse: encode a `HeartbeatData` with the new fields populated, decode into a struct **without** the fields (use a local fixture struct), assert no error. |
| AC-012 | `web/src/pages/dashboard/PerDeviceCharts.tsx` second `<ResponsiveContainer>` under each card. **Original FRD wording ("four small-multiples charts at `xl:grid-cols-4`") is unfulfillable; AD-005 remap: "each device card renders both the existing 4-line percent chart AND the new bytes/sec sub-chart."** | `Dashboard.test.tsx` — `[AC-012] each device card renders network sub-chart`: render with one device; assert both `device-chart-dev-1` and `device-net-chart-dev-1` exist. |
| AC-013 | Both sub-chart and disk-related percent line live on the same card; visual structure: stacked-area In/Out pair. **AD-005 remap: chart shape is "single In/Out fleet-summed-equivalent stacked area" — but per-device, not fleet-summed.** | `Dashboard.test.tsx` — `[AC-013] network sub-chart uses two-area stacked structure`: snapshot or DOM check that two `Area` nodes render with `dataKey="netRxPoints"`-like keys. |
| AC-014 | `useFleetMetrics` populates `netRxPoints` / `netTxPoints` from `data.net_rx_bytes_sec` / `data.net_tx_bytes_sec`. **AD-005 remap: "fleet-summed In/Out across all devices' latest heartbeats" becomes "per-device In/Out from each device's latest heartbeats."** Aggregation is implicit — every card shows its device's series, and the dashboard collectively shows all of them. | `useFleetMetrics.test.ts` (CREATE if absent, otherwise extend existing test) — `[AC-014] netRxPoints/netTxPoints populated from heartbeat data`: feed a device with `net_rx_bytes_sec=1000`, assert `netRxPoints[0].value === 1000`. |
| AC-015 | `formatBytesPerSec` from `web/src/utils/format.ts` (already exists) wired into the chart's `Tooltip` formatter | `PerDeviceCharts.test.tsx` (or `Dashboard.test.tsx`) — `[AC-015] tooltip auto-scales bytes/sec units`: simulate tooltip render at 512, 5000, 5_000_000, 5_000_000_000 → assert formatted strings `512 B/s`, `4.9 KB/s`, `4.8 MB/s`, `4.66 GB/s`. (Use the actual function output; the FRD's "approximately" wording allows the format util's `.toFixed(1)` rounding.) |
| AC-016 | `useFleetMetrics` `?? 0` fallback when keys are missing | `Dashboard.test.tsx` — `[AC-016] all-old-agent fleet renders zero net chart not error`: provide a heartbeat fixture without the new keys; assert `device-net-chart-dev-1` renders without throwing; assert no error boundary / fallback content shows. |
| AC-017 | `CHANGELOG.md` line 71 removed; new FLEET-NET CHANGELOG entry added; `README.md` line 1139 removed | Not unit-testable. Verified by docs review during QA — search CHANGELOG/README for the deleted strings; confirm new CHANGELOG entry under FLEET-NET version exists. (Engineer must run `grep -n "Network throughput line deferred" README.md CHANGELOG.md` and confirm zero hits before commit.) |
| AC-018 | All seven NFR-006 cases tested in `heartbeat_test.go` with `[AC-00X]`-prefixed names | Covered by AC-002 through AC-009 unit tests above. The implementation report must list all seven test names and confirm each is prefixed with the appropriate AC reference. |
| AC-019 | `internal/server/handlers/heartbeat_compat_test.go` integration test | Same test file as AC-010. Must use the existing handler test harness (not a hand-rolled `httptest`), to match the rest of the directory's test pattern. |
| AC-020 | `omitempty` tags on both fields → no key serialized when value is `0`; numeric values are at most ~20 chars each plus key + punctuation = ~63 bytes worst case | `heartbeat_test.go` — `[AC-020] payload size delta within 64 bytes`: marshal old `HeartbeatData` shape and new shape with realistic non-zero rates; assert `len(new) - len(old) <= 64`. |

---

## 9. Error Handling

| Failure mode | Detection | Response |
|--------------|-----------|----------|
| `psnet.IOCountersWithContext` returns error | `err != nil` | Skip the network rate block entirely; do **not** zero out previous snapshot state, do **not** emit zero values that would smear the chart. The omitempty tags drop both keys from this heartbeat's payload. Mirrors the disk-rate block's silent skip. |
| Per-interface counter rollover | `cur < prev` | Contribute 0 from that interface for this interval. Update snapshot. No log. |
| Interface removed | Absent from `curr` | Contributes 0 implicitly. Removed from snapshot. No log. |
| Interface added | Absent from `prev` | Contributes 0 for first interval. Added to snapshot. No log. |
| Elapsed time `<= 0` | `now.Sub(prev) <= 0` | Skip rate computation, leave fields at zero. Update snapshot/time. No log. |
| JSON decode error on inbound heartbeat | Existing handler: `json.NewDecoder(...).Decode` error | Already handled by `handlers.go:243–246` — returns `400 {"error":"invalid request body"}`. New fields do not change this. |
| Storage error on heartbeat insert | Existing handler path | Already handled by `handlers.go:253–257` — returns `500`. Unchanged. |
| Old DB row missing the keys | `json.Unmarshal` into the new struct yields zero values | Dashboard renders 0/0 series. AC-016. |
| Dashboard receives a heartbeat where one of the two keys is present and the other is absent | TypeScript narrows via `?? 0` | Renders missing key as 0. No error. |

No new HTTP status codes. No new error response shapes. No new logging at
INFO+ on the agent's heartbeat hot path (NFR-007).

---

## 10. Security Considerations

The new fields carry only aggregate byte counts; they do not introduce any
new attack surface.

- **No new endpoints.** The heartbeat endpoint is unchanged; no new auth
  paths, rate limits, or input shapes added beyond two optional numerics.
- **Input validation.** The agent guarantees non-negative values by
  construction (rollover guard + elapsed guard). The server does not
  defensively re-validate. A malicious agent that already holds a valid
  `X-rIOt-Key` header could push arbitrary numeric values; this is no
  worse than the existing `cpu_percent` / `mem_percent` fields, which are
  trusted in the same way. Compromised-device exfiltration via the
  heartbeat field is bounded to ~20 bytes per heartbeat per field — not
  meaningful as a covert channel beyond what already exists with the disk
  rate fields.
- **Log injection / log spam.** The new path does not log at INFO+, so
  there is no agent-controlled string flowing into the server logs.
- **JSONB injection.** Standard library `json.Marshal` produces safe JSON;
  pgx parameterizes the JSONB insert. No format-string concerns.

---

## 11. Performance Considerations

### Agent

- **Heartbeat hot path cost** (NFR-001 ≤ 5 ms / 32 ifaces). One additional
  `psnet.IOCountersWithContext(ctx, true)` syscall per heartbeat. Measured
  against the existing disk-rate block, this is on the same order
  (low single-digit ms even on hosts with many interfaces). Loopback filter
  is two string comparisons per interface. Delta math is a single integer
  subtraction and one float divide per interface. Well within budget.
- **Memory.** `prevNetCounters` is `len(interfaces) * sizeof(netIOSnapshot)`
  ≈ 16 bytes/iface. 32 interfaces ≈ 512 bytes. Steady state.
- **Payload size** (NFR-002 ≤ 64 bytes). Worst-case key+value:
  `"net_rx_bytes_sec":1234567890.123,` is ~33 bytes; both fields ≈ 66 bytes
  including the comma between them. With realistic rates (well under
  10 GB/s, fewer significant digits), the addition stays inside 64 bytes
  per the JSON encoder's default float formatting. `omitempty` collapses
  the addition to zero on idle agents and pre-FLEET-NET agents.

### Server

- **No additional DB access.** No new query, no new index.
- **No new JSON keys parsed by hot read paths.** `GetFleetHeartbeats`
  returns the JSONB blob through `interface{}` — no per-key cost beyond
  what JSON-decoding already costs.

### Dashboard

- **Per-device card height** grows ~120 px (sub-chart). On a fleet of 50
  devices at `xl:grid-cols-2`, this lengthens the page by ~3000 px.
  Acceptable.
- **Re-render cadence.** The 5 s `useDashboardTick` already drives the
  existing 4-line chart; adding two more series points per tick is
  negligible.
- **Recharts tooltip bytes formatting.** `formatBytesPerSec` is O(1).
- **MAX_POINTS = 240** preserved — the rolling window cap prevents
  unbounded growth on long sessions.

### Backward compatibility (already covered in AD-008, restated as a perf concern)

- Old DB rows: Go `json.Unmarshal` cost is unchanged; missing keys are
  cheaper than present keys.
- Old agents: the new server's response payload is identical for them
  (no `omitempty` keys emitted).
- Old servers: an updated agent's payload is rejected for unknown keys
  only if the server uses `Decoder.DisallowUnknownFields`; the existing
  handler does **not** (verified at `handlers.go:243`), so the old server
  silently drops the new fields.

---

## 12. Implementation Notes for Engineers

1. **Mirror disk-rate code exactly.** The existing block in
   `internal/agent/heartbeat.go:57–103` is the template. Keep loop
   structure, guard ordering, and snapshot-update placement identical so
   the two blocks can be visually diffed for correctness during code
   review.

2. **Make the rate math testable without invoking the OS.** The cleanest
   path is to factor the inner math into an unexported pure function:

   ```
   // computeNetRates returns (rx, tx) bytes/sec given previous and current
   // per-interface snapshots and elapsed seconds. Returns (0, 0) when
   // prev is nil or elapsed <= 0. Skips per-interface rollover.
   func computeNetRates(
       prev map[string]netIOSnapshot,
       curr map[string]netIOSnapshot,
       elapsed float64,
   ) (rx float64, tx float64)
   ```

   `sendHeartbeat` calls this with already-filtered (loopback-excluded)
   maps. Tests then exercise `computeNetRates` directly with synthetic
   inputs — no syscalls, no goroutines, no fakes for `gopsutil`. This is
   the same shape the disk-rate code *should* have had; do not retrofit
   the disk-rate block, but do apply the pattern here.

3. **Loopback filter is its own helper.** `isLoopbackName(name string) bool`
   in `heartbeat.go`. Tests for AC-007 call it directly with `lo`, `lo0`,
   `Loopback Pseudo-Interface 1`, `eth0`, `lo-fancy`, asserting
   `true,true,true,false,false`.

4. **Do NOT add a new dependency.** `gopsutil/v3/net` is already imported
   by `internal/agent/collectors/network.go` and transitively available.
   Add `psnet "github.com/shirou/gopsutil/v3/net"` to `heartbeat.go`'s
   imports — same alias as the collector — and reuse it.

5. **Do NOT add `Decoder.DisallowUnknownFields()`.** Several backward-compat
   ACs depend on the current permissive behavior.

6. **TypeScript: be strict about optionals.** Both new fields are
   `number | undefined` — always read as `data.net_rx_bytes_sec ?? 0` at
   the boundary in `useFleetMetrics`. Do not let `undefined` flow into
   the chart data array.

7. **Recharts sub-chart height.** Use `height={100}` on the new
   `<ResponsiveContainer>`; this keeps the card from growing more than
   needed. Margins should match the main chart's `margin={{ top: 4, right: 8, bottom: 0, left: 0 }}`
   for visual alignment.

8. **Chart keys for stable React reconciliation.** The new sub-chart's
   gradient IDs must use a different prefix from the main chart (e.g.
   `grad-net-${deviceId}-rx` vs the main chart's `grad-${deviceId}-cpu`)
   to avoid recharts `<defs>` collisions inside one card.

9. **Tooltip unit string.** FR-024 / AC-015 require the unit to indicate
   bytes/sec, not bits/sec. `formatBytesPerSec` already emits `B/s` /
   `KB/s` / `MB/s` / `GB/s`. Do not rename or add a `b/s` (bits) variant.

10. **CHANGELOG entry version.** The user's project memory states "tag
    every push that changes code." The CHANGELOG entry placeholder should
    be added under whatever version tag the engineer uses for the FLEET-NET
    push (e.g. `v2.61.0`). Use the same heading style as the existing
    FLEET-DASH entry. List both the agent schema change and the
    dashboard sub-chart.

11. **Stop on missing files / contradictions.** If
    `internal/agent/heartbeat.go` or `internal/agent/agent.go` does not
    have the disk-rate block in the shape this ADD describes when work
    begins (e.g. some other story merged first and changed the precedent),
    write `docs/architecture/FLEET-NET-blockers.md` and stop.

12. **Field-name divergence reminder.** AC-001's literal text says
    `net_rx_bytes_per_sec` / `net_tx_bytes_per_sec`. **Use
    `net_rx_bytes_sec` / `net_tx_bytes_sec`** per AD-001. The QA engineer
    must validate against this ADD; if the QA engineer flags the
    divergence as a bug, point them at AD-001.

---

## 13. Definition of Done

- [ ] Every component change in §4 implemented.
- [ ] `models.HeartbeatData` carries `NetRxBytesPerSec` and `NetTxBytesPerSec`
      with `omitempty` JSON tags `net_rx_bytes_sec` / `net_tx_bytes_sec`.
- [ ] `Agent` struct carries `prevNetCounters` and `prevNetCountersTime`.
- [ ] `internal/agent/heartbeat.go` contains the new rate block and
      `isLoopbackName` helper, and (via `computeNetRates` or equivalent)
      is unit-testable without a real OS.
- [ ] `internal/agent/heartbeat_test.go` exists with named tests
      `[AC-002]` through `[AC-009]` and `[AC-020]` per §8.
- [ ] `internal/server/handlers/heartbeat_compat_test.go` (or extension of
      an existing handler test file) carries the `[AC-010]` /
      `[AC-019]` integration test.
- [ ] `web/src/types/models.ts` mirrors the new fields.
- [ ] `web/src/hooks/useFleetMetrics.ts` extends `DeviceSeries` and
      populates both new arrays in all three population sites
      (REST seed, WS append, new-device fall-through).
- [ ] `web/src/pages/dashboard/PerDeviceCharts.tsx` renders the second
      `<ResponsiveContainer>` with the two-area stacked sub-chart, using
      `formatBytesPerSec` and `formatBytesPerSecAxis`, and a
      `data-testid="device-net-chart-${deviceId}"` test hook.
- [ ] `Dashboard.test.tsx` fixtures updated; `[AC-012]`, `[AC-013]`,
      `[AC-014]`, `[AC-015]`, `[AC-016]` named tests present.
- [ ] `CHANGELOG.md` line 71 (FLEET-DASH "Network throughput line
      deferred" note) removed; new FLEET-NET entry added under the
      release version.
- [ ] `README.md` line 1139 ("Network throughput line deferred") removed.
- [ ] `make test` passes (Go + frontend).
- [ ] `make build-server` and `make build-agent` succeed without warnings.
- [ ] No new linting errors.
- [ ] No new dependencies added to `go.mod` or `package.json`.
- [ ] No database migration files created or modified.
- [ ] `grep -n "Network throughput line deferred" README.md CHANGELOG.md`
      returns zero matches.
- [ ] `grep -n "FleetStackedChart" web/src` returns zero matches (the
      component still does not exist; nothing in this story should
      introduce it).
