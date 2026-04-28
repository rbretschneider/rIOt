# Security Review Report

- **Story ID:** FLEET-NET
- **Reviewer:** Security Researcher Agent
- **Date:** 2026-04-27
- **Verdict:** APPROVE WITH NOTES

---

## Threat Model Summary

### Assets

- **Heartbeat data integrity.** Aggregate per-device telemetry rendered on the
  fleet dashboard. Stored in `heartbeats.data` JSONB. Read by every operator
  loading `/dashboard`.
- **Agent process stability.** The agent emits heartbeats every 15 s; a panic
  or hang in the rate-computation block would silently degrade fleet
  observability.
- **Browser stability.** The dashboard renders rates from JSONB through
  recharts; pathological values (NaN, Inf, extremely large floats) reaching
  the chart layer must not crash the SPA.

### Threat Actors

- **Authenticated agent (compromised or buggy).** Holds a valid `X-rIOt-Key`,
  pushes heartbeats with attacker-controlled `data` payload. This is the
  realistic threat actor for this story — anything reachable through the
  heartbeat body.
- **Authenticated low-privilege dashboard user.** Reads the rendered chart;
  no mutating action exists.
- **Unauthenticated external attacker.** Cannot reach the heartbeat endpoint
  without a key (existing auth middleware on
  `POST /api/v1/devices/{id}/heartbeat`); out of scope for *this* story since
  the auth surface is unchanged.
- **Operator on the agent host.** Trusted by design — the agent runs as a
  privileged process owned by the operator; interface-naming tricks are
  self-inflicted.

### Attack Surface Introduced

- **Two new optional numeric JSON keys** on the heartbeat request body:
  `net_rx_bytes_sec` and `net_tx_bytes_sec`. No new endpoints. No new auth
  paths. No new headers. No new query parameters. No new database columns or
  indexes (JSONB carries the keys directly).
- **Two new TypeScript fields** consumed by `useFleetMetrics` and rendered by
  `PerDeviceCharts.tsx`. No new external requests on the dashboard side.
- **One additional gopsutil syscall** per heartbeat (`net.IOCountersWithContext`),
  already used by `internal/agent/collectors/network.go`. No new dependency.

The attack surface is genuinely minimal — this is a numeric-field schema
extension on an already-authenticated, already-trusted channel.

---

## Findings

### CRITICAL

None.

---

### HIGH

None.

---

### MEDIUM

#### SEC-FLEET-NET-001: Frontend tooltip and Y-axis must not break on `NaN`/`Infinity` reaching the chart

- **Severity:** MEDIUM
- **Domain:** Data Exposure / Client Robustness
- **Location:** `web/src/utils/format.ts`, `web/src/hooks/useFleetMetrics.ts`,
  `web/src/pages/dashboard/PerDeviceCharts.tsx`
- **Description:** `formatBytesPerSec` and `formatBytesPerSecAxis` already
  guard against negative and non-finite numbers (return `'0 B/s'` / `'0'`),
  so the rendered string is safe. However, the `useFleetMetrics` projection
  uses `data.net_rx_bytes_sec ?? 0`, which only substitutes for `null` /
  `undefined` — it does **not** coerce `NaN`, `Infinity`, or `-Infinity` to
  `0`. A buggy or malicious agent that holds a valid `X-rIOt-Key` could
  emit JSON where the float decodes to `NaN` (impossible from Go's encoder
  but possible from any other agent implementation a user might write) or
  to `Infinity`, and that value would propagate into the recharts data
  array. recharts handles `NaN` non-deterministically across versions —
  some charts render gaps, some throw on tooltip hover, some compute axis
  ranges that collapse all other series to a flat line.
- **Attack Scenario:** A device-key holder writes a heartbeat client (e.g.
  a Python or curl-based push) that includes
  `"net_rx_bytes_sec": 1e400` or `"net_rx_bytes_sec": "NaN"`. Go's
  `json.Decoder` rejects literal `NaN`/`Infinity` per RFC 8259, so that
  specific case is blocked at the server boundary. But `1e400` overflows
  to `+Inf` in IEEE 754 — and Go's `encoding/json` *does* decode this
  silently to `math.Inf(1)`. The Inf is stored verbatim in JSONB as the
  string `+Inf` (pgx will fail the marshal there, returning a 500), or
  the broadcast to WebSocket consumers serializes through
  `json.Marshal(math.Inf(1))` which **errors silently** and may produce
  malformed JSON. End-state: the dashboard either crashes on the next
  tick or the WS heartbeat broadcast is dropped, blinding operators.
- **Required Resolution:** Two complementary controls, either of which is
  sufficient on its own:
  1. **Server-side sanitization.** When ingesting the heartbeat, clamp
     non-finite (`math.IsInf` / `math.IsNaN`) `NetRxBytesPerSec` and
     `NetTxBytesPerSec` to `0`. The ADD's §10 explicitly declines server-
     side validation on the grounds that the agent's guards make it
     impossible — but that reasoning only holds for *our* agent. Any
     valid-key holder can post arbitrary JSON.
  2. **Frontend coercion.** Replace `?? 0` with a finite-or-zero helper
     (`Number.isFinite(x) ? x : 0`) at every population site in
     `useFleetMetrics.ts` (REST seed, WS append, new-device
     fall-through). This is a single 5-line helper and defends regardless
     of upstream behavior.
  Fix during implementation. The QA engineer must verify that an agent
  payload containing `1e400` does not crash the dashboard or the WS
  broadcast.
- **Blocks:** Does not block implementation; must be addressed during
  the impl phase and validated by QA.

---

### LOW / INFORMATIONAL

#### SEC-FLEET-NET-002: ADD field-name divergence from FRD AC-001 may mask a contract regression

- **Severity:** LOW
- **Domain:** Specification Hygiene
- **Location:** ADD §3 AD-001 vs FRD AC-001
- **Description:** The FRD specifies JSON tags `net_rx_bytes_per_sec` /
  `net_tx_bytes_per_sec`. The ADD overrides this to `net_rx_bytes_sec` /
  `net_tx_bytes_sec` to match the existing disk-rate naming. The override
  is well-reasoned (FRD A-001 explicitly delegates naming to the architect)
  and the ADD calls it out clearly in AD-001 §Consequences. This is not a
  security finding in itself; the concern is that *if* a future agent
  implementation (e.g. a third-party agent built against the FRD literal)
  emits the FRD-spelled keys, the server will silently decode them to zero
  and downstream consumers will see all-zero rates with no error. This is
  a fail-closed mode (no false alerts, no data corruption), but a
  hard-to-diagnose one.
- **Attack Scenario:** None — this is a specification cleanliness concern,
  not an exploit path.
- **Required Resolution:** Defense-in-depth. Either (a) document in the
  agent README which JSON keys the heartbeat body accepts, or (b) accept
  that this is internal-consistency-over-FRD-literalism and move on. No
  code change required.
- **Blocks:** No.

#### SEC-FLEET-NET-003: Loopback-name filter is a categorization choice, not a security boundary

- **Severity:** INFORMATIONAL
- **Domain:** Trust Boundary / Categorization
- **Location:** ADD AD-004 (`isLoopbackName`)
- **Description:** AD-004's name-based filter (`lo`, `lo0`,
  `HasPrefix("Loopback")`) is checked inline. A user-renamed interface
  called `lo-fancy` would be counted, and a non-loopback interface
  contrived to start with `Loopback` would be excluded. The agent runs
  with privileges sufficient to read every interface; the host operator
  is the trusted party and can already see all traffic by other means.
  This is therefore a *categorization* choice (do we want this in the
  fleet sum?), not a security control. The ADD acknowledges this in the
  Consequences section.
- **Attack Scenario:** None within scope. An attacker who can rename
  network interfaces on a monitored host already controls the host.
- **Required Resolution:** None. Documented for awareness.
- **Blocks:** No.

#### SEC-FLEET-NET-004: AC-018 unit-test coverage requires the `computeNetRates` factor-out

- **Severity:** INFORMATIONAL
- **Domain:** Test Coverage / Defense in Depth
- **Location:** ADD §12 Implementation Notes #2
- **Description:** FRD NFR-006 / AC-018 requires named unit tests for
  seven branches (cold start, normal, rollover, iface added, iface
  removed, loopback excluded, zero/negative elapsed). The ADD strongly
  recommends factoring the rate math into an unexported pure function
  `computeNetRates(prev, curr, elapsed) -> (rx, tx)`. If the engineer
  inlines the logic in `sendHeartbeat` instead, the rollover guard, the
  per-iface added/removed handling, and the elapsed-≤-0 guard become
  difficult to exercise in isolation without a real OS. Failure to factor
  this out doesn't introduce a vulnerability per se, but it *does* create
  a coverage gap on safety-critical branches (panic-prevention,
  divide-by-zero prevention, sign correctness) that the QA engineer
  cannot recover by adding tests later — they would have to refactor the
  agent code, which is out of QA's scope.
- **Attack Scenario:** None directly. Indirect: an undetected regression
  in the rollover guard ships, an agent emits a negative or NaN rate,
  see SEC-FLEET-NET-001.
- **Required Resolution:** Engineer must factor out `computeNetRates`
  per ADD §12 #2 before declaring AC-018 satisfied. QA must verify the
  function exists and is tested directly, not via an `Agent` integration
  fixture.
- **Blocks:** No.

#### SEC-FLEET-NET-005: `prevNetCounters` data-race risk if a future story adds a second heartbeat caller

- **Severity:** INFORMATIONAL
- **Domain:** Concurrency / Future Risk
- **Location:** ADD AD-006 / `internal/agent/agent.go`
- **Description:** AD-006 places `prevNetCounters` and
  `prevNetCountersTime` on the `Agent` struct without mutex protection,
  mirroring the existing unprotected `prevDiskIO` / `prevDiskIOTime`
  pattern. Today this is safe — `heartbeatLoop` is the only goroutine
  that calls `sendHeartbeat`. But the codebase already has a
  `telemetryNow chan struct{}` pattern that triggers ad-hoc telemetry
  pushes; an analogous future feature ("force a heartbeat") would
  introduce a second concurrent caller and silently race the snapshot
  state. Because Go map access is not safe for concurrent
  read/iterate+write, the agent could panic with `concurrent map
  iteration and map write`. This is a pre-existing pattern smell, not a
  FLEET-NET regression — the disk-rate state has the same shape — but
  the new story doubles the surface.
- **Attack Scenario:** None directly. A future story that adds a
  `heartbeatNow chan struct{}` (mirroring `telemetryNow`) would silently
  introduce the race. Detecting this only via the `-race` race detector
  would require the test suite to exercise both call sites, which Go-test
  on Linux does and Windows-test does not.
- **Required Resolution:** Defense in depth. Engineer should add a brief
  comment on `prevNetCounters` (e.g. `// owned by heartbeatLoop only`)
  next to the analogous comment that should also exist on `prevDiskIO`.
  No mutex required for this story.
- **Blocks:** No.

---

## Positive Observations

- **Auth surface is unchanged.** No new endpoint, no new header, no new
  query parameter. The existing `X-rIOt-Key` middleware on
  `POST /api/v1/devices/{id}/heartbeat` continues to gate the path.
- **No new dependency.** `gopsutil/v3/net` is already in `go.mod` and
  imported by `internal/agent/collectors/network.go`. Supply chain risk
  delta is zero.
- **Backward-compat is verified by reading existing code.** Confirmed:
  - The handler at `internal/server/handlers/handlers.go:243` uses
    `json.NewDecoder(r.Body).Decode(&hb)` with no
    `DisallowUnknownFields()` — a `grep` across the repo confirms zero
    callers configure `DisallowUnknownFields` anywhere. Old agent → new
    server (NFR-003, AC-010) and new agent → old server (NFR-004,
    AC-011) are both safe by virtue of the standard decoder ignoring
    unknown keys and zero-defaulting missing keys.
  - `omitempty` on both fields means pre-FLEET-NET agents emit an
    identical-shape payload (no key bloat).
- **Storage path is JSONB-pass-through.** `TelemetryRepo.StoreHeartbeat`
  at `internal/server/db/telemetry_repo.go:31–37` calls `json.Marshal`
  and inserts via parameterized pgx. New fields ride for free; no SQL
  injection vector introduced.
- **Numeric-only payload.** No string fields added — no log-injection,
  template-injection, or path-traversal attack surface.
- **Heartbeat payload size budget (NFR-002) is comfortably met.**
  `"net_rx_bytes_sec":1234567890.123,"net_tx_bytes_sec":1234567890.123`
  is ~63 bytes worst case; with `omitempty`, idle agents emit nothing
  (zero added bytes).
- **No threshold/event generation on the new fields.**
  `CheckHeartbeatThresholds` at `internal/server/events/generator.go:335`
  evaluates `MemPercent`, `DiskRootPercent`, and `LogErrors` only — the
  new fields cannot trigger spurious alerts via crafted values (FRD §9
  out of scope confirmed).
- **Frontend formatters already defend against negatives and non-finites.**
  `formatBytesPerSec` and `formatBytesPerSecAxis` at
  `web/src/utils/format.ts:1–17` both `Number.isFinite` and `< 0` guards
  return safe fallbacks. Display-layer robustness is solid; the gap
  flagged in SEC-FLEET-NET-001 is in the upstream data array, not the
  formatter.

---

## Verdict Rationale

**APPROVE WITH NOTES.** No CRITICAL or HIGH findings. The story is a
narrow, low-risk schema extension on an already-authenticated channel
with a proven precedent (disk rate) that the ADD mirrors faithfully.

One MEDIUM finding (SEC-FLEET-NET-001) requires a defensive fix during
implementation: NaN/Infinity-coercion at either the server boundary or
the frontend hook, to defend against malformed or malicious heartbeats
from valid-key holders. The fix is a one-line helper at one of two well-
defined seams; it does not require an architectural change.

Three LOW/INFORMATIONAL findings document defense-in-depth concerns
(SEC-FLEET-NET-002 spec-hygiene, SEC-FLEET-NET-003 categorization,
SEC-FLEET-NET-004 test factor-out, SEC-FLEET-NET-005 concurrency comment)
that the engineer should address but that do not block.

Implementation may proceed. The QA engineer must verify SEC-FLEET-NET-001
is resolved before approving.

---

## Routing

- **Verdict:** APPROVE WITH NOTES
- **Next agent:** `senior-dev`
- **Pass alongside ADD:** This security report
- **QA must verify:** SEC-FLEET-NET-001 (NaN/Inf coercion at server
  ingestion or frontend hook), SEC-FLEET-NET-004 (`computeNetRates`
  factor-out per ADD §12 #2 with seven named tests `[AC-002]`–`[AC-009]`)
