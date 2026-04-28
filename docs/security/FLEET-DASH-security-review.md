# Security Review Report

**Story ID:** FLEET-DASH
**Reviewer:** Security Researcher Agent
**Date:** 2026-04-27
**Verdict:** APPROVED WITH CONDITIONS

---

## Threat Model Summary

### Assets

- **Fleet operational telemetry** (CPU, memory, disk, load, network, container state) — operationally sensitive (reveals what is running, when stress events occur, and which devices are exposed/unprotected).
- **Container metadata** (image names, container names, compose stack labels, hostnames). Container labels in particular are an exfiltration vector when operators follow the env-var-as-label antipattern.
- **Event stream** (`Event.Message`, `container_name`, `hostname`) — agent-controlled strings rendered in the operator's browser.
- **Admin session JWT cookie** (`riot_session`) — authentication for the entire dashboard; `HttpOnly` + `SameSite=Strict` per `internal/server/handlers/auth.go`. Theft = full operator compromise.
- **Heartbeat history table** — 7-day retention, ~7,200 rows per 30-device fleet per hour. A query that scans a multi-day window across all devices is a server resource.

### Threat Actors

- **Unauthenticated external attacker** — must be blocked from `/dashboard`, the new REST endpoints, and the `/ws` channel.
- **Authenticated operator (intended user)** — has full read access by design. Single-tenant deployment per CLAUDE.md; no per-user device ACL exists.
- **Compromised or malicious agent** — already authenticated to its own device key; can push fabricated heartbeats, telemetry, events, and container metadata. These payloads are broadcast to every dashboard tab via `/ws` and stored for retention. This is a pre-existing trust assumption of the rIOt model; FLEET-DASH inherits it.
- **Cross-site attacker leveraging an operator's browser** — must not be able to drive the dashboard via CSRF, ride the operator's WS connection, or inject content via XSS.
- **Supply chain** — no new third-party deps introduced (recharts already present).

### Attack Surface Introduced

| New Surface | Auth | State-changing | Notes |
|---|---|---|---|
| `GET /api/v1/fleet/heartbeats?window=60m` | `adminAuth` (JWT cookie) | No | Bulk fleet-wide heartbeat read; `window` is user-controlled |
| `GET /api/v1/fleet/containers` | `adminAuth` (JWT cookie) | No | Fleet-wide container projection from latest telemetry snapshots |
| `/dashboard` SPA route | Existing client-side auth gate (login redirect) | No | Authentication enforced server-side at the API layer (no auth on the static asset itself; expected — the SPA fetches data which IS auth-gated) |

No new ingest paths, no new agent-facing endpoints, no new WS topics, no new schema, no new dependencies.

---

## Findings

### CRITICAL

None.

---

### HIGH

None.

---

### MEDIUM

#### SEC-001: Container leaderboard handler decodes full telemetry blobs for the fleet on every request

**Severity:** MEDIUM
**Domain:** Resource Exhaustion / DoS
**Location:** `docs/architecture/FLEET-DASH-add.md` AD-006, Section 11 ("Performance Considerations"); `TelemetryRepository.GetAllLatestSnapshots` in `internal/server/db/telemetry_repo.go:64`.

**Description:**
The new `GET /api/v1/fleet/containers` handler is specified to call `TelemetryRepository.GetAllLatestSnapshots()`, which returns the full `FullTelemetryData` blob per device — process lists, hardware inventory, cron jobs, container logs, web-server configs, certificates, mounts, networks, env vars. The handler then projects out only the slim fields needed for the leaderboard. This means the *server* decodes multi-MB JSONB blobs per device per request even though only a tiny projection is returned to the client.

For a 30-device fleet this is observed at <100 ms (the ADD cites this from existing patch-status experience). For a 100-device fleet (the NFR-002 target), this approaches 200–500 MB of JSONB decoding per request, multiplied by however many dashboard tabs hit the endpoint on mount/refocus. Existing `PatchStatus` deliberately uses the slimmer `GetAllLatestSummaries` (`telemetry_repo.go:89`) for exactly this reason — JSONB sub-projection in PostgreSQL.

The endpoint is admin-only, so the threat model is constrained: an authenticated operator with multiple stale dashboard tabs (or a buggy frontend that refetches on focus) can drive sustained server CPU/memory pressure. There is no rate limit on admin routes (consistent with the existing pattern).

**Attack Scenario:**
1. An authenticated operator opens the dashboard in 5 browser tabs and each tab focuses periodically (refetch on window focus is a React Query default behavior).
2. Each focus triggers `GET /api/v1/fleet/containers`, which calls `GetAllLatestSnapshots()` → 5 concurrent full-fleet JSONB decodes.
3. On a 100-device fleet with ~20 containers per device and rich telemetry, this is sustained 500 MB+ of allocation churn per minute. Server GC pressure rises; heartbeat ingest latency degrades.

**Required Resolution:**
The architect should specify one of:
(a) A new repository method that performs JSONB projection in PostgreSQL (analogous to `GetAllLatestSummaries`), returning only the container array with the leaderboard's projected fields. This eliminates the server-side full-blob decode.
(b) A short-lived in-memory cache (e.g., 5–10 s TTL) on the handler so that bursty refetches share one decode pass.
(c) An explicit acknowledgement in the ADD that the v1 budget is sized for the 30-device target and not the 100-device NFR-002 target, with a follow-up story for the projected query at higher device counts.

The QA engineer should verify the chosen approach is implemented and that a sustained-load test against this endpoint at the documented device count does not regress server responsiveness.

**Blocks:** Not implementation; must be addressed before QA sign-off.

---

#### SEC-002: WebSocket-fed container labels reach the dashboard cache without server-side projection

**Severity:** MEDIUM
**Domain:** Information Disclosure / Defense in Depth
**Location:** `internal/server/handlers/handlers.go:1095-1123` (`stripHeavyTelemetry` retains full `Labels` map); ADD AD-002 ("the leaderboard re-derives top-10 from the cached telemetry on the same 5 s tick"); ADD Section 10 (states REST projection excludes labels except compose project label, but is silent on the WS path).

**Description:**
The ADD's Section 10 (Security Considerations) correctly states the new `GET /api/v1/fleet/containers` REST endpoint deliberately projects only a slim `FleetContainerRow` shape — no `Env`, no `Mounts`, no `Networks`, no `Labels` except the compose project label. This is a thoughtful, narrow projection.

However, the dashboard's container leaderboard is also designed to update live by re-deriving from cached telemetry on each 5 s tick (ADD AD-002, AD-006, Section 7 "Live update — 5-second tick"). The cache is fed by the WS `telemetry` broadcasts, which pass through `stripHeavyTelemetry` server-side. That helper drops `Hardware`, `WebServers`, `CronJobs`, `Procs`, `USB`, `Mounts`, `Networks`, `Env` — but **explicitly keeps `Labels`** (`handlers.go:1116`):

```
Labels:        c.Labels,
```

So the leaderboard's REST initial-fetch shape is narrowly projected, but its live-update shape is not. Any browser tab on the dashboard receives every container's full Labels map for every device on every telemetry broadcast. Container labels in real-world Docker deployments routinely contain:
- Compose project metadata
- Traefik routing rules with hostnames and authentication providers
- `org.opencontainers.image.*` build metadata
- Custom labels that operators sometimes use as a poor-man's env store ("env-as-label" antipattern)

This is a **pre-existing** WS shape — FLEET-DASH does not introduce the `Labels` field into the broadcast. The existing fleet page already receives it. But FLEET-DASH significantly expands the *attack surface for label exposure* by introducing a UI surface (the leaderboard) that consumes this cache and could, through a future change, surface labels to the operator's screen — at which point label content becomes screenshot/logging-exposure.

**Attack Scenario:**
1. An operator deploys a container with a label `traefik.http.middlewares.auth.basicauth.users=admin:$apr1$...` (a real-world Traefik configuration pattern).
2. The agent reports this container in telemetry. The server WS-broadcasts the container with `Labels` intact via `stripHeavyTelemetry`.
3. The dashboard cache holds the full label map. A future regression in `ContainerLeaderboard.tsx` (e.g., a "show labels" affordance, or a hover tooltip iterating all label keys, or an inadvertent `JSON.stringify(container)` in a debug view) leaks the htpasswd credential to anyone who can shoulder-surf the dashboard or capture a screenshot.

**Required Resolution:**
The ADD should specify, for the new dashboard components, that container `Labels` are **not read** from the cached telemetry except to project out the compose project name (`com.docker.compose.project`). Specifically:

- `ContainerLeaderboard.tsx` MUST only consume from the leaderboard cache the fields that map to `FleetContainerRow` (the REST shape).
- Stack name MUST be derived from `Labels['com.docker.compose.project']` only; no other label keys may flow into UI state.

The QA engineer must verify (by code inspection of the new components) that no other label keys are read from the cached telemetry by the dashboard surfaces. A unit test asserting that the leaderboard component does not render or store arbitrary label content from a fixture with sensitive labels would close this gap.

A separate hardening follow-up (NOT in this story) should consider extending `stripHeavyTelemetry` to drop labels other than the compose project label from the WS broadcast.

**Blocks:** Not implementation; condition on implementation and on QA verification.

---

#### SEC-003: Window parameter parser regex is ambiguous for valid inputs

**Severity:** MEDIUM
**Domain:** Input Validation
**Location:** ADD Section 6 (`GET /api/v1/fleet/heartbeats` query params), Section 10 (input validation), Section 9 (error handling).

**Description:**
The ADD specifies the `window` parameter with mixed and self-contradicting language:

- Section 6: "Format: integer minutes suffixed with `m` or integer seconds with `s`. Anything else → 400."
- Section 10: "`window` parameter regex `^\d+[ms]$`; integer overflow guarded by parsing into `int` and bounds-checking. Anything unparseable → 400."

The regex `^\d+[ms]$` matches `60m`, `60s`, but is also satisfied by inputs whose semantic interpretation is unclear:

- `0m` → zero-window query. What does the handler return? An empty result is benign; a no-bound query (treating `0m` as "no filter") is a CRITICAL bug. Behavior must be specified.
- A leading-zero or extremely large numeric prefix like `999999999999m` → parses as `int` overflow (`strconv.Atoi` returns an error on values exceeding `int64` range, but values within `int64` but exceeding 60 minutes must be rejected, not silently capped). The ADD's "hard-capped at 60 minutes server-side" language is ambiguous — does it mean "reject" or "clamp"? Clamping silently would mask user error; rejecting with 400 is the safer choice and matches the ADD's stated behavior in Section 9.
- The regex permits `60m` and `60s`, but the ADD nowhere states the maximum for `s`. Is `3600s` allowed (= 60 minutes)? Is `3601s` rejected? The Section 6 wording "max `60m`" suggests the cap is on the *duration*, not the *literal value*, but the implementation must enforce this.
- The cap value `60m` is a magic constant that conflicts with the existing pattern for `/devices/{id}/heartbeats` (`hours <= 168`). Operators viewing the fleet endpoint may not realize the per-device endpoint allows much longer windows. This is a documentation issue, not a security one — but the architect should explicitly note this divergence.

The CRITICAL risk is the "no parameter" case: if `window` is missing, the ADD says "default `60m`". This is good. But if `window` is empty string (`?window=`), the regex does NOT match `^\d+[ms]$` → handler returns 400. Acceptable, but should be explicit: empty string ≠ missing parameter.

**Attack Scenario:**
1. An authenticated operator (or a browser-borne attacker who has CSRF'd the operator via a non-strict-SameSite gap, which does not apply here — SameSite=Strict is set) sends `GET /api/v1/fleet/heartbeats?window=0m`.
2. If the handler treats `0m` as "no filter", the query becomes `WHERE timestamp >= 1970-01-01` — full table scan of `heartbeats` (7 days × all devices). On a 100-device fleet that is ~600,000 rows decoded. Sustained DoS vector against the server.

**Required Resolution:**
The ADD must specify the parser's behavior for these edge cases explicitly, and the implementation must enforce them:

- `window` MUST be a positive duration: zero, negative-prefixed, and missing-prefix values reject with 400.
- `window` MUST resolve to a duration in `(0, 60m]`. Any duration that resolves to ≤ 0 or > 60 minutes rejects with 400 (no silent clamping).
- The regex MUST require the unit suffix and bound the numeric prefix length (e.g., `^\d{1,5}[ms]$`) to defeat absurd inputs without relying on `strconv.Atoi` overflow as the sole guard.
- The fleet endpoint's 60-minute cap should be noted in the ADD as a deliberate divergence from the per-device endpoint's 168-hour cap, so reviewers do not "fix" the divergence by accident.

The QA engineer must add a test in `fleet_dashboard_test.go` covering at minimum: `0m`, `61m`, `0s`, `3601s`, missing parameter (default), empty parameter (`?window=`), non-numeric prefix (`abc m`), Unicode digits, very long input (`9999999999m`), negative-looking input (`-1m`), and a value at the exact boundary (`60m`, `3600s`).

**Blocks:** Not implementation; must be addressed in the handler and tests.

---

### LOW / INFORMATIONAL

#### SEC-004: Pre-existing — no request body size limit on agent ingest endpoints

**Severity:** LOW (out of FLEET-DASH scope, noted for awareness)
**Domain:** Resource Exhaustion (server, transitively dashboard)
**Location:** `internal/server/handlers/handlers.go:240` (Heartbeat), `742-758` and surrounding (telemetry).

**Description:**
The agent-facing `Heartbeat`, `Telemetry`, and `ReceiveAgentEvent` handlers do not wrap the request body in `http.MaxBytesReader`. This was previously flagged by the GPU-001 and other prior security reviews as a systemic gap (`docs/security/GPU-001-security-review.md`, `docs/qa/GPU-001-qa-report.md`). It is not in scope for FLEET-DASH to fix.

The relevance to FLEET-DASH is that the new `GET /api/v1/fleet/heartbeats` endpoint reads back whatever an agent inserted. A compromised agent could insert pathologically large heartbeat payloads (the `HeartbeatData` schema is small in the legitimate path, but JSON decoding has no upstream byte cap). The fleet endpoint would then return an inflated payload to every dashboard client.

**Required Resolution:**
None for this story. Note in the implementation report that the dashboard's resilience to oversized heartbeats is bounded by the existing systemic gap. Recommend a follow-up hardening story to add `http.MaxBytesReader(w, r.Body, N)` to all agent ingest paths.

---

#### SEC-005: Severity classes built from agent-controlled `Event.Severity` should use a whitelist

**Severity:** LOW
**Domain:** Defense in Depth / XSS
**Location:** `internal/models/events.go` (`Event.Severity`); ADD Section 5 (`ActivityRiver.tsx`), AD-007.

**Description:**
The `ReceiveAgentEvent` handler (`handlers.go:838`) accepts `agentEvt.Severity` from agent input and stores it without explicit validation against the `SeverityInfo` / `SeverityWarning` / `SeverityCrit` constant set. The current type is `EventSeverity = string`, so any string the agent sends is preserved. The dashboard's activity river will color-code by severity; if the implementation builds a CSS class as `severity-${event.severity}`, a malicious agent could supply ` text-red-500" onclick="...` and break out of the class attribute context.

React's auto-escaping on text children does NOT apply to attribute interpolation in the same way; specifically, dynamic className strings are still passed through, so a className constructed from agent input can carry surprising tokens. Tailwind users typically avoid this by using a switch/lookup map, which the ADD already implies via "Existing severity color scheme is reused" (FRD A-014). This is a defense-in-depth note.

**Required Resolution:**
Engineering should ensure `ActivityRiver.tsx` (and any new component rendering severity from `Event.Severity`) maps severity through a whitelist (e.g., `const colorMap: Record<Severity, string> = { info: '...', warning: '...', critical: '...' }`) and renders an unknown severity as a default neutral class — never interpolating the raw severity string into a className. This is the existing pattern used elsewhere; the ADD should make it an explicit constraint for the new component.

The QA engineer should grep the new `ActivityRiver.tsx` and assert there is no `className={`...${event.severity}...`}` pattern.

---

#### SEC-006: GPU device list leak surface (none, confirmed)

**Severity:** INFORMATIONAL
**Domain:** Information Disclosure
**Location:** ADD AD-008 (`devices_with_gpu: string[]`).

**Description:**
The new `devices_with_gpu` sibling field in the heartbeat response returns a list of device IDs. These IDs are identical to those in the existing `GET /api/v1/devices` response (which is also `adminAuth` gated). No new information is disclosed beyond what the operator can already see. The device ID format is the existing `Device.ID` (UUID-style, per existing devices repo conventions). No change in exposure surface.

No action required.

---

#### SEC-007: Dashboard client should validate WS message shape before applying state mutations

**Severity:** LOW
**Domain:** Defense in Depth
**Location:** `web/src/contexts/WebSocketProvider.tsx:26-33` (current `JSON.parse` only), `web/src/hooks/useDevices.ts` (current `as Device` type assertion).

**Description:**
The frontend currently parses WS messages with `JSON.parse(event.data)` and trusts the message shape via TypeScript `as` casts. A buggy or malicious server (or a man-in-the-middle that defeats TLS — out of scope for this single-tenant deployment, but theoretically possible in a misconfigured environment without TLS) could deliver malformed messages that crash a render or land non-strings in fields that the dashboard interpolates into the DOM.

This is a pre-existing pattern; FLEET-DASH does not change it. The new `useFleetMetrics` and `usePerDevicePulse` hooks will follow the same pattern. The risk is bounded because (a) `/ws` is `adminAuth` gated, (b) Origin is checked server-side, and (c) the protocol is wss in any sane deployment.

No action required for this story. A follow-up hardening to add lightweight runtime validation (e.g., zod, valibot, or a hand-rolled discriminator check) of WS messages would be a useful project-wide investment, not a FLEET-DASH-specific need.

---

## Positive Observations

- **Auth model is correct.** The ADD correctly identifies the dashboard as user-facing and routes the new endpoints into the existing `adminAuth` (JWT cookie) group at `internal/server/router.go:171`. Agent API key auth (`X-rIOt-Key`) is the wrong model for these endpoints and the ADD does not use it. The `/ws` path used by the dashboard is also inside the `adminAuth` group (`router.go:176`), distinct from the agent-facing `/ws/agent`. The CSRF posture is solid: `riot_session` is `HttpOnly` + `SameSite=Strict` (`internal/server/handlers/auth.go:51`), and the new endpoints are GET-only, so CSRF is not a concern.

- **Window cap is the right design.** Capping `window` at 60 minutes server-side, despite 7-day retention, is a deliberate DoS-prevention choice. The existing per-device endpoint allows up to 168 hours, but that is for one device at a time; capping the fleet-wide endpoint at 60 minutes correctly recognizes the combinatorial blowup. (See SEC-003 for parser-correctness conditions.)

- **Container projection on the REST path is narrow.** The architect explicitly excluded `Env`, `Mounts`, `Networks`, `Labels` (except compose project), and `Riot.URL` from `FleetContainerRow`. This is an actively designed minimum-disclosure projection rather than a default echo-all-fields response. (See SEC-002 for the unaddressed WS path.)

- **No new dependencies.** The ADD reuses existing `recharts`, existing chart primitives, existing chi router, existing pgx layer. No supply-chain risk introduced.

- **Single WS subscription is enforced architecturally.** The `WebSocketProvider` singleton pattern (`web/src/contexts/WebSocketProvider.tsx`) plus the FRD's AC-027 ("no second WebSocket connection is opened") plus the ADD's explicit "Do not call `new WebSocket(...)` anywhere" instruction in Section 12 forms a strong layered control.

- **The WS Origin check is correctly enforced.** `middleware.CheckWSOrigin` (`internal/server/middleware/wsorigin.go`) is wired into `gorilla/websocket.Upgrader.CheckOrigin` (`internal/server/websocket/hub.go:15`). This blocks cross-origin browser-driven WS hijack of an authenticated operator's session.

- **No `dangerouslySetInnerHTML` in the codebase, by audit.** Confirmed via grep across `web/src/`. The ADD reaffirms this constraint for the new components.

- **Stress score formula is fixed and client-side.** No state-changing operation, no server-side amplification.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.**

There are no CRITICAL findings. The three MEDIUM findings (SEC-001, SEC-002, SEC-003) all have clear, non-architectural resolutions that the engineering team can address during implementation, and that the QA engineer can mechanically verify:

- **SEC-001** (full-blob decode in container leaderboard handler): the architect must choose between (a) a slim repo method analogous to `GetAllLatestSummaries`, (b) a short-lived in-memory cache, or (c) an explicit acknowledgement that the v1 budget is the 30-device target. The decision is small and isolated to the new handler.
- **SEC-002** (WS labels reach the leaderboard cache): the engineering team must constrain the new `ContainerLeaderboard.tsx` to read only the projected fields from cached telemetry and the compose project label specifically. QA verifies via code inspection and a unit test.
- **SEC-003** (window parser): the architect must specify the parser's behavior on edge inputs (zero, over-cap, missing unit, oversized prefix). Engineering implements and tests. QA verifies via the listed input matrix.

The LOW findings (SEC-004 through SEC-007) are documented for awareness; they are pre-existing, defense-in-depth, or out-of-scope hardening items.

Implementation may proceed. The engineering team must address SEC-001, SEC-002, and SEC-003 as part of this story. The QA engineer must verify all three before sign-off.
