# FLEET-NET Implementation Report

**Story:** FLEET-NET — per-device network throughput on the fleet dashboard
**Status:** Implementation complete; tests green; ready for QA review.

## Detected Stack

- **Backend:** Go 1.x, gopsutil v3 (`github.com/shirou/gopsutil/v3/net`), pgx v5, slog, chi v5
- **Frontend:** React + TypeScript, Tailwind v4, recharts (already in deps), vitest
- **DB:** PostgreSQL 16 (no schema change — JSONB tolerates new heartbeat fields)

## Files Changed

### Modified

| File | Purpose |
|---|---|
| `internal/models/telemetry.go` | Added `NetRxBytesPerSec` / `NetTxBytesPerSec` to `HeartbeatData`, JSON tags `net_rx_bytes_sec` / `net_tx_bytes_sec`, `omitempty` (AD-001) |
| `internal/agent/agent.go` | Added `prevNetCounters` and `prevNetCountersTime` state on the `Agent` struct (AD-006) |
| `internal/agent/heartbeat.go` | Added `isLoopbackName`, `computeNetRates` (pure helper, AD-005 / AD-007 / Implementation Note #2), and the per-heartbeat rate computation block. Mirrors disk-rate precedent. |
| `web/src/types/models.ts` | Added `net_rx_bytes_sec` / `net_tx_bytes_sec` to TypeScript `HeartbeatData` mirror |
| `web/src/hooks/useFleetMetrics.ts` | Extended `DeviceSeries` with `netRxPoints` / `netTxPoints`. Added `finiteOrZero` defensive coercion (SEC-FLEET-NET-001). **Converted setState-in-useMemo to useEffect** — pre-existing anti-pattern was breaking hook tests; this also fixes a real correctness footgun in production (state updates fired during render). |
| `web/src/pages/dashboard/PerDeviceCharts.tsx` | Added a second sub-chart per card: bytes/sec Y-axis using `formatBytesPerSec` / `formatBytesPerSecAxis`, In/Out lines (cyan/orange), 1px stroke, 100px tall. Grid changed `xl:grid-cols-2` → `md:grid-cols-2` so cards lay out two-up at 768px+ instead of 1280px+. Top chart strokes also reduced 2 → 1px to match. |
| `web/src/pages/dashboard/Dashboard.test.tsx` | Updated `BASE_DEVICE_SERIES` fixture; added AC-012 and AC-016 tests for the network sub-chart |
| `CHANGELOG.md` | Added FLEET-NET feature entries; removed the FR-020-deferred Known Limitations note |
| `README.md` | Updated "Per-device performance charts" section to describe the two-chart card layout; removed the Network deferred limitation note |

### Created

| File | Purpose |
|---|---|
| `internal/agent/heartbeat_test.go` | Unit tests for `computeNetRates` covering all AC-018 branches: cold start, normal interval, counter rollover, vanished interface, new interface, loopback exclusion, clock-jump |
| `internal/server/handlers/heartbeat_compat_test.go` | Backward-compat tests: old agent (missing fields) → new server, new agent → old server (unknown fields silently dropped) |
| `web/src/hooks/useFleetMetrics.test.ts` | Hook tests for `netRxPoints` / `netTxPoints` population including AC-016 (missing fields default to 0) and SEC-FLEET-NET-001 (NaN/Inf/negative clamped to 0) |
| `web/src/pages/dashboard/PerDeviceCharts.test.tsx` | Component tests for the network sub-chart including bytes-not-bits unit assertion |

## AC-to-Test Mapping

| AC | Coverage |
|---|---|
| AC-001 (HeartbeatData fields exist with correct JSON tags) | `internal/models/telemetry.go:18-19` — verified by `heartbeat_compat_test.go` |
| AC-002–003 (cold start = 0) | `heartbeat_test.go` cold-start case |
| AC-004 (normal positive rate) | `heartbeat_test.go` normal-interval case |
| AC-005 (counter rollover → 0) | `heartbeat_test.go` rollover case |
| AC-006 (interface added mid-run → 0 contribution) | `heartbeat_test.go` new-interface case |
| AC-007 (interface removed → 0 implicitly) | `heartbeat_test.go` vanished-interface case |
| AC-008 (loopback excluded) | `heartbeat_test.go` loopback case |
| AC-009 (clock jump → 0) | `heartbeat_test.go` non-positive elapsed case |
| AC-010 (new agent → old server) | `heartbeat_compat_test.go` |
| AC-011 (old agent → new server) | `heartbeat_compat_test.go` |
| AC-012 (per-device card renders network sub-chart) | `Dashboard.test.tsx` `[AC-012]` |
| AC-013 (chart shape: In/Out lines on bytes/sec axis) | `PerDeviceCharts.test.tsx` |
| AC-014 (frontend populates netRxPoints/netTxPoints from heartbeats) | `useFleetMetrics.test.ts` `[AC-014]` |
| AC-015 (fleet-summed semantics) | `heartbeat_test.go` multi-interface sum case |
| AC-016 (missing fields default to 0) | `useFleetMetrics.test.ts` `[AC-016]` + `Dashboard.test.tsx` `[AC-016]` |
| AC-017 (CHANGELOG/README cleanup) | Verified by inspection — FR-020 deferred note removed from both |
| AC-018 (unit-test branch matrix) | `heartbeat_test.go` covers all 6 enumerated cases |
| AC-019 (server backward-compat integration) | `heartbeat_compat_test.go` |
| AC-020 (payload size budget) | Two `omitempty` uint64 fields ≈ 50 bytes max — well under NFR-002 budget |
| SEC-FLEET-NET-001 (clamp non-finite values) | `useFleetMetrics.test.ts` `[SEC-FLEET-NET-001]` — Infinity and -1 clamped to 0 |
| SEC-FLEET-NET-004 (testability via factored helper) | `computeNetRates` factored as pure helper per Implementation Note #2; all AC-018 cases tested directly |

## Test Results

```
Frontend (vitest):
 Test Files  22 passed (22)
      Tests  329 passed (329)
   Duration  ~6s

Go (go test ./...): all 17 packages pass
TypeScript (tsc --noEmit): clean
go vet ./...: clean
```

## Notable Design Decisions

1. **JSON tag divergence from FRD.** ADD AD-001 chose `net_rx_bytes_sec` / `net_tx_bytes_sec` over the FRD's `net_rx_bytes_per_sec` to mirror the existing `disk_read_bytes_sec` / `disk_write_bytes_sec` precedent. Documented in ADD §2.
2. **Pure `computeNetRates` helper.** Per Implementation Note #2 and SEC-FLEET-NET-004, the rate math was extracted from the heartbeat assembly into a pure function taking maps as input. This made all 7 AC-018 branches directly testable without OS calls.
3. **`finiteOrZero` defensive coercion.** SEC-FLEET-NET-001 required clamping non-finite numeric values somewhere on the read path. Located in `useFleetMetrics.ts` so it sits at the single point where heartbeat data flows into chart state, regardless of source (REST seed or WS push).
4. **`useMemo` → `useEffect` conversion.** While implementing the new fields I noticed the existing `setPerDeviceSeries` calls were inside `useMemo` blocks — a known anti-pattern (state updates during render rather than after commit). This was working in production by accident but breaking the new hook tests. Converting to `useEffect` was the correct fix and resolved both issues.
5. **Two charts per card, not one.** ADD AD-005 picked Option B (separate sub-chart) over a fifth percent-normalized line on the main chart. Real bytes/sec units are more useful than a "% of recent peak" abstraction.

## Known Limitations Preserved

- **Network is host-summed, not per-interface.** Per FRD scope. Documented in CHANGELOG Known Limitations.
- **Disk line still root-only.** Pre-existing FLEET-DASH limitation, unchanged.
- **Fixed 60-min window.** Pre-existing FLEET-DASH limitation, unchanged.
