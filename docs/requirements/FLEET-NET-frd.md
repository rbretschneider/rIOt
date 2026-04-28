# Formal Requirements Document

- **Story ID:** FLEET-NET
- **Title:** Fleet-Summed Network Throughput on Heartbeat + Dashboard Network In/Out Chart
- **Author:** Business Developer Agent
- **Date:** 2026-04-27
- **Status:** FINAL

---

## 1. Executive Summary

Extend the agent heartbeat payload with two fleet-summed network throughput fields (receive bytes/sec and transmit bytes/sec) computed as a per-interval delta across all non-loopback interfaces, and restore the previously deferred "Network in/out" small-multiples chart on the fleet dashboard so the four-chart row originally specified in FLEET-DASH is complete. This closes the gap left by FLEET-DASH FR-020 without requiring any database migration or full-telemetry round-trip on the dashboard's high-frequency refresh path.

## 2. Background & Context

FLEET-DASH (shipped as v2.59.0) introduced a fleet-wide dashboard at `/dashboard` with a small-multiples row designed for four charts: CPU per device, memory per device, disk I/O, and network in/out. The fourth chart was deferred because the heartbeat schema carried no per-second network byte rate fields and the dashboard's refresh cadence cannot tolerate a full telemetry round-trip. As a result, v2.59.0 ships three charts in an `xl:grid-cols-3` layout with a documented v1 limitation in CHANGELOG and README.

The existing heartbeat already carries `disk_read_bytes_sec` and `disk_write_bytes_sec` computed agent-side as deltas between consecutive heartbeats. This story applies the same proven pattern to network counters, sums across all non-loopback interfaces, and reintroduces the deferred chart.

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| rIOt Agent | Go daemon running on monitored devices, emits heartbeats to the server. | Pushes heartbeats via `X-rIOt-Key` header; reads OS network counters. |
| rIOt Server | Go binary that ingests heartbeats and serves dashboard data. | Stores heartbeats; serves `/api/v1/` and dashboard. |
| Dashboard User | Operator viewing `/dashboard` in the browser. | Reads fleet metrics; no mutating actions in this story. |
| Older Agent (pre-FLEET-NET) | Agents that have not yet been upgraded. | Continues to push heartbeats without the new fields. |

## 4. Functional Requirements

### Agent — Heartbeat Schema

- **FR-001:** The agent's heartbeat payload must include a field named `NetRxBytesPerSec` (Go struct) serialized with JSON tag `net_rx_bytes_per_sec,omitempty`.
- **FR-002:** The agent's heartbeat payload must include a field named `NetTxBytesPerSec` (Go struct) serialized with JSON tag `net_tx_bytes_per_sec,omitempty`.
- **FR-003:** Both new fields must be expressed in **bytes per second** (not bits per second) to match the existing `DiskReadBytesPerSec` / `DiskWriteBytesPerSec` convention.
- **FR-004:** Both new fields must be non-negative numeric values. The agent must not emit negative rates under any circumstances.

### Agent — Rate Computation

- **FR-005:** On every heartbeat after the first, the agent must compute `NetRxBytesPerSec` as the sum across all non-loopback interfaces of `(current_rx_bytes - previous_rx_bytes) / elapsed_seconds`.
- **FR-006:** On every heartbeat after the first, the agent must compute `NetTxBytesPerSec` as the sum across all non-loopback interfaces of `(current_tx_bytes - previous_tx_bytes) / elapsed_seconds`.
- **FR-007:** On the first heartbeat after agent start, both `NetRxBytesPerSec` and `NetTxBytesPerSec` must be `0` (no prior counter snapshot exists to delta against).
- **FR-008:** The agent must exclude loopback interfaces (`lo`, `lo0`, and any interface whose name matches the OS-native loopback identifier) from the sum.
- **FR-009:** The agent must include all non-loopback interfaces in the sum, including physical NICs, bridges, virtual NICs, container bridges, VPN tunnels, and wireless interfaces.
- **FR-010:** If a per-interface counter goes backwards between consecutive heartbeats (counter rollover, interface re-creation, kernel reset), the agent must contribute `0` for that interface for that interval rather than a negative delta.
- **FR-011:** If an interface is present in the previous snapshot but not the current snapshot (interface removed mid-run), the agent must contribute `0` for that interface for that interval and not include it in the sum.
- **FR-012:** If an interface is present in the current snapshot but not the previous snapshot (interface added mid-run), the agent must contribute `0` for that interface for that interval (no prior counter to delta against) and begin contributing real deltas on subsequent heartbeats.
- **FR-013:** Elapsed time between heartbeats must be measured by wall-clock seconds between consecutive heartbeat emissions on the same agent instance.
- **FR-014:** If the measured elapsed time between consecutive heartbeats is less than or equal to zero (clock jumped backwards via NTP sync, or zero-duration tick), the agent must emit `0` for both fields for that heartbeat. The agent must not divide by zero or negative time.

### Server — Ingestion & Backward Compatibility

- **FR-015:** The server must accept heartbeats from older agents that do not include `net_rx_bytes_per_sec` and `net_tx_bytes_per_sec` fields, treating absent fields as `0` for downstream consumers.
- **FR-016:** The server must persist or expose the new fields through whatever mechanism currently surfaces `disk_read_bytes_sec` / `disk_write_bytes_sec` to the dashboard, so the dashboard's existing fleet-metrics access pattern works without bespoke handling.
- **FR-017:** No database migration must be required for this story. The new fields ride the existing heartbeat path.

### Dashboard — Network In/Out Chart

- **FR-018:** The dashboard at `/dashboard` must render a fourth small-multiples chart titled "Network in/out" alongside the existing CPU, memory, and disk I/O charts.
- **FR-019:** The "Network in/out" chart must be visually identical in shape to the existing "Disk I/O" chart (a single In/Out fleet-summed pair, stacked or paired per the existing `FleetStackedChart` component).
- **FR-020:** The In series must plot the fleet sum of `net_rx_bytes_per_sec` across all reporting devices' latest heartbeats.
- **FR-021:** The Out series must plot the fleet sum of `net_tx_bytes_per_sec` across all reporting devices' latest heartbeats.
- **FR-022:** When the small-multiples row contains four charts, its grid layout at `xl` breakpoint must use `xl:grid-cols-4` (restored from the current `xl:grid-cols-3`).
- **FR-023:** The chart's tooltip must display values in human-readable units that auto-scale: `B/s`, `KB/s`, `MB/s`, `GB/s`. The unit must be chosen per displayed value such that the numeric portion is greater than or equal to 1 and less than 1024 wherever possible.
- **FR-024:** The chart's Y-axis label or tooltip context must indicate that the unit is bytes per second (not bits per second).
- **FR-025:** When all reporting agents are pre-FLEET-NET (sending no network rate fields), the chart must render with both series at `0` rather than failing, breaking, or showing a blank/error state.

### Documentation Cleanup

- **FR-026:** The CHANGELOG entry that documents the FLEET-DASH v1 limitation regarding deferred FR-020 (Network in/out) must be removed or superseded by a FLEET-NET entry.
- **FR-027:** The README note that documents the FLEET-DASH v1 limitation regarding deferred FR-020 must be removed.
- **FR-028:** The CHANGELOG must contain a new entry under the FLEET-NET release version describing the addition of fleet network throughput on the heartbeat and the restored fourth chart.

## 5. Non-Functional Requirements

- **NFR-001:** [Performance] Computing `NetRxBytesPerSec` and `NetTxBytesPerSec` on the agent must add no more than 5 ms of CPU time per heartbeat on a host with up to 32 network interfaces.
- **NFR-002:** [Performance] The new fields must not increase heartbeat payload size by more than 64 bytes (two numeric fields plus JSON keys).
- **NFR-003:** [Backward compatibility] An older agent that does not emit the new fields must continue to be accepted by a server running this story's code, with no error response and no log spam.
- **NFR-004:** [Backward compatibility] An updated agent must continue to be accepted by an older server that does not recognize the new fields. (The server simply ignores unknown JSON fields per existing convention.)
- **NFR-005:** [Reliability] A counter rollover, interface change, or NTP clock jump must never cause the agent to crash, panic, or stop emitting heartbeats. The agent must emit `0` for the affected interval and continue.
- **NFR-006:** [Test coverage] The agent's delta computation must have unit tests covering: cold start, normal interval, counter rollover, interface added mid-run, interface removed mid-run, loopback excluded, and zero-or-negative elapsed time.
- **NFR-007:** [Observability] No new logging is required at INFO level or above on the agent's hot path. DEBUG-level logs are permitted for delta computation diagnostics.

## 6. Business Rules

- **BR-001:** Per-interface drilldown is explicitly out of scope for this story; the dashboard chart is fleet-summed and matches the shape of the Disk I/O chart.
- **BR-002:** The unit for both heartbeat fields is bytes per second to remain consistent with the existing `DiskReadBytesPerSec` / `DiskWriteBytesPerSec` fields. Conversion to bits per second is not provided.
- **BR-003:** Loopback traffic is never counted, regardless of volume.
- **BR-004:** The new fields use `omitempty` JSON tags so agents in environments where the values are unknown or unmeasurable serialize a heartbeat without these keys, and downstream consumers default to `0`.

## 7. Data Requirements

### Entities Involved
- **HeartbeatData** (agent → server): existing struct gains two new fields.

### Validation Rules
- `NetRxBytesPerSec >= 0`
- `NetTxBytesPerSec >= 0`
- Both fields are floating-point or integer numeric (architect's decision; must match existing disk-rate field type for consistency).

### State Transitions
The agent maintains an in-memory snapshot of per-interface counters between heartbeats. State transitions:

| From | Event | To | Emitted Rate |
|------|-------|----|--------------|
| No snapshot (cold start) | First heartbeat | Snapshot stored | 0 |
| Snapshot present | Next heartbeat, normal | Snapshot updated | (curr - prev) / elapsed, summed |
| Snapshot present | Next heartbeat, counter went backwards on iface | Snapshot updated | 0 contribution from that iface |
| Snapshot present | Next heartbeat, iface gone | Snapshot updated (without iface) | 0 contribution from that iface |
| Snapshot present | Next heartbeat, new iface appeared | Snapshot updated (incl. new iface) | 0 contribution from new iface (this interval only) |
| Snapshot present | Next heartbeat, elapsed <= 0 | Snapshot updated | 0 for both fields |

## 8. Acceptance Criteria

### AC-001: Heartbeat fields exist with correct names and JSON tags [Maps to FR-001, FR-002, FR-003]
- **Given** an agent built from the FLEET-NET branch is running
- **When** it emits a heartbeat
- **Then** the JSON payload contains a key `net_rx_bytes_per_sec` (when non-zero) of numeric type
- **And** the JSON payload contains a key `net_tx_bytes_per_sec` (when non-zero) of numeric type
- **And** the values represent bytes per second, not bits per second

### AC-002: Cold-start behavior emits zero [Maps to FR-007]
- **Given** the agent has just started and has no prior network counter snapshot
- **When** it emits its first heartbeat
- **Then** `net_rx_bytes_per_sec` is `0`
- **And** `net_tx_bytes_per_sec` is `0`

### AC-003: Normal interval produces positive rate [Maps to FR-005, FR-006]
- **Given** the agent has a prior snapshot taken `T` seconds ago
- **And** non-loopback interfaces have collectively received `R` additional bytes and transmitted `X` additional bytes since the prior snapshot
- **When** it emits its next heartbeat
- **Then** `net_rx_bytes_per_sec` equals `R / T` (within rounding tolerance of the chosen numeric type)
- **And** `net_tx_bytes_per_sec` equals `X / T` (within rounding tolerance of the chosen numeric type)

### AC-004: Counter rollover produces zero, not negative [Maps to FR-004, FR-010]
- **Given** the agent has a prior snapshot for interface `eth0` with `rx_bytes = 1000`
- **And** the current snapshot for `eth0` has `rx_bytes = 100` (counter wrapped or reset)
- **When** the heartbeat is computed
- **Then** `eth0` contributes `0` to the rx sum for this interval
- **And** the emitted `net_rx_bytes_per_sec` is non-negative

### AC-005: Interface added mid-run contributes zero for that interval [Maps to FR-012]
- **Given** the prior snapshot did not include interface `docker0`
- **And** the current snapshot includes `docker0` with non-zero counters
- **When** the heartbeat is computed
- **Then** `docker0` contributes `0` to both rx and tx sums for this interval
- **And** the agent stores `docker0`'s current counters in the snapshot for the next interval

### AC-006: Interface removed mid-run contributes zero for that interval [Maps to FR-011]
- **Given** the prior snapshot included interface `wg0`
- **And** the current snapshot does not include `wg0`
- **When** the heartbeat is computed
- **Then** `wg0` contributes `0` to both rx and tx sums for this interval
- **And** `wg0` is absent from the next snapshot

### AC-007: Loopback excluded [Maps to FR-008]
- **Given** the host has a loopback interface `lo` carrying significant traffic
- **And** the host has a non-loopback interface `eth0` carrying no traffic
- **When** the heartbeat is computed
- **Then** `net_rx_bytes_per_sec` is `0`
- **And** `net_tx_bytes_per_sec` is `0`
- **And** `lo` traffic is not included in the sum

### AC-008: Non-loopback interfaces of all kinds are included [Maps to FR-009]
- **Given** the host has a physical NIC, a bridge, a virtual NIC, and a container bridge — all non-loopback — each carrying traffic
- **When** the heartbeat is computed
- **Then** all four interfaces' deltas are summed into `net_rx_bytes_per_sec` and `net_tx_bytes_per_sec`

### AC-009: Clock jump backwards emits zero [Maps to FR-014]
- **Given** the agent's measured elapsed time between the prior heartbeat and the current heartbeat is `<= 0` (clock jumped backwards or zero-duration)
- **When** the heartbeat is computed
- **Then** `net_rx_bytes_per_sec` is `0`
- **And** `net_tx_bytes_per_sec` is `0`
- **And** the agent does not panic or divide by zero

### AC-010: Server accepts heartbeats from older agents [Maps to FR-015, NFR-003]
- **Given** the server is running FLEET-NET code
- **And** an older agent (pre-FLEET-NET) sends a heartbeat without `net_rx_bytes_per_sec` or `net_tx_bytes_per_sec`
- **When** the server ingests the heartbeat
- **Then** the heartbeat is accepted with a `2xx` response
- **And** downstream consumers (dashboard fleet metrics) treat the missing fields as `0`
- **And** no error or warning is logged for the missing fields

### AC-011: Updated agent accepted by older server [Maps to NFR-004]
- **Given** an updated agent is sending heartbeats with the new fields populated
- **And** a server running pre-FLEET-NET code is receiving them
- **When** the server ingests the heartbeat
- **Then** the heartbeat is accepted with a `2xx` response
- **And** the unknown fields are silently ignored

### AC-012: Dashboard renders four charts [Maps to FR-018, FR-022]
- **Given** at least one device is reporting heartbeats with the new fields
- **When** a user loads `/dashboard`
- **Then** the small-multiples row renders four charts: CPU per device, memory per device, disk I/O, and Network in/out
- **And** at the `xl` breakpoint the grid uses `xl:grid-cols-4`

### AC-013: Network in/out chart matches Disk I/O chart shape [Maps to FR-019]
- **Given** the dashboard is rendering both Disk I/O and Network in/out charts
- **When** a user inspects the two charts
- **Then** the Network in/out chart uses the same `FleetStackedChart` component shape (a single In/Out fleet-summed pair, identical visual structure to Disk I/O)
- **And** there is no per-device or per-interface breakdown shown on this chart

### AC-014: In/Out series are fleet-summed [Maps to FR-020, FR-021]
- **Given** N devices are reporting latest heartbeats with `net_rx_bytes_per_sec` and `net_tx_bytes_per_sec`
- **When** the dashboard renders the Network in/out chart
- **Then** the In series equals the sum of `net_rx_bytes_per_sec` across all N devices' latest heartbeats
- **And** the Out series equals the sum of `net_tx_bytes_per_sec` across all N devices' latest heartbeats

### AC-015: Tooltip shows human-readable auto-scaled units [Maps to FR-023, FR-024]
- **Given** the Network in/out chart is rendered with various rate magnitudes
- **When** the user hovers a data point representing 512 bytes/sec
- **Then** the tooltip displays `512 B/s`
- **When** the user hovers a data point representing 5000 bytes/sec
- **Then** the tooltip displays approximately `4.88 KB/s`
- **When** the user hovers a data point representing 5_000_000 bytes/sec
- **Then** the tooltip displays approximately `4.77 MB/s`
- **When** the user hovers a data point representing 5_000_000_000 bytes/sec
- **Then** the tooltip displays approximately `4.66 GB/s`
- **And** in all cases the unit string indicates bytes per second (not bits per second)

### AC-016: All-old-agent fleet renders zeroed chart, not error [Maps to FR-025]
- **Given** every reporting device is a pre-FLEET-NET agent that does not emit the new fields
- **When** the user loads `/dashboard`
- **Then** the Network in/out chart renders with both In and Out series at `0`
- **And** the chart does not throw an error, render blank, or show an error state

### AC-017: CHANGELOG/README v1-limitation note removed [Maps to FR-026, FR-027, FR-028]
- **Given** the FLEET-NET branch is merged
- **When** a reader inspects the CHANGELOG
- **Then** the prior FLEET-DASH note documenting deferred FR-020 (Network in/out) is removed or superseded
- **And** a new CHANGELOG entry under the FLEET-NET release version documents the new heartbeat fields and restored fourth chart
- **When** a reader inspects the README
- **Then** the prior FLEET-DASH note documenting deferred FR-020 is removed

### AC-018: Agent unit tests cover all delta-computation branches [Maps to NFR-006]
- **Given** the agent's network-rate computation code
- **When** the unit-test suite runs
- **Then** there is a named test for cold start (first heartbeat = 0)
- **And** a named test for normal interval (positive rate)
- **And** a named test for counter rollover (per-interface delta would be negative — contributes 0)
- **And** a named test for interface added mid-run (new interface contributes 0 for first interval)
- **And** a named test for interface removed mid-run (vanished interface contributes 0)
- **And** a named test for loopback exclusion (lo / lo0 not counted)
- **And** a named test for zero-or-negative elapsed time (emits 0)
- **And** each test name references AC-001 through AC-009 as appropriate

### AC-019: Server backward-compat integration test [Maps to FR-015, NFR-003]
- **Given** an integration test fixture posts a heartbeat JSON without `net_rx_bytes_per_sec` and `net_tx_bytes_per_sec`
- **When** the server's heartbeat handler processes it
- **Then** the response is `2xx`
- **And** the heartbeat is recorded with the network rate fields treated as `0`

### AC-020: Heartbeat payload size constraint [Maps to NFR-002]
- **Given** an agent emits a heartbeat with the new fields populated
- **When** the payload size is compared against a heartbeat from an older agent (same host, same other fields)
- **Then** the payload is no more than 64 bytes larger

## 9. Out of Scope

- Per-interface network rate breakdown on the dashboard (lives on the per-device detail page, separate story).
- Network rate fields in bits per second (decision: bytes per second only, matching disk-rate convention).
- Historical network rate retention beyond what existing heartbeat retention provides.
- Per-protocol or per-port traffic breakdown.
- Database schema migration for storing network rates separately from heartbeats.
- Alerting on network throughput thresholds.
- Aggregations beyond fleet-sum (e.g., per-tag, per-location).
- Changes to the agent's full telemetry network collector.

## 10. Assumptions

- **A-001:** The existing `DiskReadBytesPerSec` / `DiskWriteBytesPerSec` fields on `HeartbeatData` are the correct stylistic and structural template for the new fields. The architect will name the new fields and choose their numeric type to match.
- **A-002:** The dashboard's existing `FleetStackedChart` component (used for Disk I/O) supports a second instance configured for network throughput without component-level changes — only data wiring.
- **A-003:** The dashboard's `useFleetMetrics` hook (or its current equivalent) already exposes the fleet-summed disk-rate values; the architect will determine whether to extend the same hook or compute network sums adjacent to it.
- **A-004:** Older-server-accepts-newer-agent backward compatibility is already true by virtue of standard JSON unmarshalling ignoring unknown fields in the existing handler.
- **A-005:** The agent's heartbeat cadence is stable enough that wall-clock elapsed time is a reasonable divisor; the existing disk-rate computation already relies on this assumption.

## 11. Open Questions

None. The user has stated the scope is comprehensive and the document is FINAL. Any remaining decisions (counter source, exact field placement, hook extension strategy, test fixture placement) are explicitly delegated to the architect per the "Things the architect will decide" section of the story.

## 12. Dependencies

- **D-001:** FLEET-DASH (v2.59.0) — the dashboard, the small-multiples row, and the `FleetStackedChart` component must already be in place. Confirmed shipped.
- **D-002:** The agent's existing heartbeat lifecycle and the existing `DiskReadBytesPerSec` / `DiskWriteBytesPerSec` rate-computation pattern — the new fields follow the same approach. No new external dependencies.
- **D-003:** No database migration. No new external services. No new third-party libraries beyond what the agent already uses for network counter access (architect's choice between `gopsutil` and direct `/proc/net/dev` reads).
