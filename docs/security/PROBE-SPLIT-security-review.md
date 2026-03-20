# Security Review Report
**Story ID:** PROBE-SPLIT
**Reviewer:** Security Researcher Agent
**Date:** 2026-03-20
**Verdict:** APPROVED

---

## Threat Model Summary

### Assets
- **Device probe configurations** (`device_probes` table): Contains probe names, types, and config maps that may include shell commands, URLs, file paths, container exec commands, and port targets. This is operational configuration data visible only to the admin.
- **Device probe results** (`device_probe_results` table): Execution output, success/failure status, latency, and error messages. May contain stdout/stderr from shell commands.
- **Device hostnames**: Internal infrastructure naming that is now returned alongside probe data in a new enriched response.
- **Business logic integrity**: The new endpoint is read-only and does not introduce any mutation paths.

### Threat Actors
- **Unauthenticated external attacker**: Attempts to access the new `GET /api/v1/device-probes` endpoint without a session.
- **Authenticated admin (single-user system)**: The only legitimate user. This is a single-admin system with no multi-tenancy, so horizontal privilege escalation between users is not applicable.
- **Automated scanners/bots**: Attempting to probe the new endpoint for information disclosure.

### Attack Surface Introduced
- **One new GET endpoint**: `GET /api/v1/device-probes` -- read-only, no user input parameters, returns JSON.
- **One new frontend data flow**: React fetches and renders device probe data including `device_hostname` and `config` fields.
- **No new database schema changes**: The new `ListAll` query reads from the existing `device_probes` table.
- **No new dependencies**: No new libraries or external services introduced.
- **No new tokens, secrets, or credentials**: No new auth mechanisms.

---

## Findings

### CRITICAL

No critical findings.

---

### HIGH

No high findings.

---

### MEDIUM

No medium findings.

---

### LOW / INFORMATIONAL

#### SEC-001: Probe config field may contain operationally sensitive data in API response
**Severity:** LOW
**Domain:** Data Exposure
**Location:** ADD Section 6 (API response shape), `internal/models/device_probes.go` (`Config map[string]interface{}`)
**Description:**
The `config` field on `DeviceProbe` is a free-form JSON map that can contain shell commands (`{"command": "curl -s localhost"}`), file paths, container exec commands, and URLs with potential credentials in query strings. The new endpoint returns this field for all device probes across all devices in a single response. This is not new exposure -- the same data is already available via per-device endpoints (`GET /api/v1/devices/{id}/device-probes`) which are also behind `AdminAuth`. However, the new aggregated endpoint increases the blast radius if the admin session cookie were to be compromised: one request now returns all probe configs instead of requiring N device-scoped requests.
**Attack Scenario:**
1. Attacker obtains an admin session cookie (e.g., via XSS on a same-origin page, physical access to browser, or cookie theft).
2. Attacker calls `GET /api/v1/device-probes` and receives all device probe configurations in one response, including any embedded credentials in shell commands or URLs.
**Required Resolution:**
No resolution required -- this is informational. The data was already accessible to the admin via existing endpoints. The single-admin, self-hosted architecture means the admin is the only intended consumer. The aggregation does not change the authorization model. If the project ever adds multi-user roles, this endpoint should be restricted to admin-only (which it already is).
**Blocks:** Nothing

---

#### SEC-002: No pagination on the new endpoint could enable resource exhaustion at extreme scale
**Severity:** LOW
**Domain:** Rate Limiting & Abuse Prevention
**Location:** ADD Section 6 and Section 11
**Description:**
The new `GET /api/v1/device-probes` endpoint returns all device probes with no pagination or limit. Per ADD Section 11, the handler also performs 2N additional queries (LatestResult + SuccessRate per probe) for enrichment. At the documented target scale of 1,000 probes, this is ~2,001 queries per request. Combined with the 30-second auto-refresh, this creates a steady-state load. The ADD acknowledges this and deems it acceptable for homelab scale, with a note that batching can be added later.
**Attack Scenario:**
1. An authenticated admin (or an attacker with a stolen session) opens the probes page and leaves it running.
2. Every 30 seconds, the endpoint executes ~2,001 queries.
3. At extreme scale (well beyond the 50-device/1,000-probe target), this could saturate the database connection pool.
**Required Resolution:**
No resolution required for the current story. The ADD correctly identifies this as a future optimization. The FRD explicitly puts pagination out of scope. The homelab scale target (50 devices, 1,000 probes) is reasonable for the current N+1 pattern. This is documented for awareness in case scale expectations change.
**Blocks:** Nothing

---

#### SEC-003: Frontend renders device_hostname and probe name without explicit sanitization note
**Severity:** LOW
**Domain:** Input Validation (XSS)
**Location:** ADD Section 12 (Implementation notes 10, 11), `web/src/pages/Probes.tsx`
**Description:**
The new frontend code will render `device_hostname` and `probe.name` in table cells and link text. If either contains HTML or script content, there is a theoretical XSS vector. However, React's JSX rendering auto-escapes string interpolation by default (`{probe.name}` renders as text, not HTML). The existing `Probes.tsx` already renders `probe.name` this way (line 123) with no `dangerouslySetInnerHTML`. As long as the new implementation follows the same pattern (which the ADD instructs via "matching AlertRuleSettings pattern"), React's built-in escaping prevents XSS.
**Attack Scenario:**
1. An attacker would need to register a device with a hostname containing `<script>alert(1)</script>` or set a probe name with malicious content.
2. Device registration requires a valid API key (`X-rIOt-Key`), and probe creation requires admin auth. Both are privileged operations.
3. Even if malicious content were stored, React's default rendering escapes it.
**Required Resolution:**
No resolution required. React's default text rendering provides adequate XSS protection. This note is for awareness: the implementation must NOT use `dangerouslySetInnerHTML` when rendering `device_hostname` or `probe.name`. The QA engineer should verify this.
**Blocks:** Nothing

---

## Positive Observations

1. **Authentication is correctly specified.** The ADD explicitly places the new `GET /api/v1/device-probes` endpoint behind `AdminAuth` middleware in the admin-authed group (ADD Section 10, Section 12 note 3). This is consistent with all other admin-facing endpoints in `router.go` (lines 152-231). The endpoint will not be accessible without a valid JWT session cookie.

2. **No new input surface.** The endpoint is a pure GET with no query parameters, path parameters, or request body. There is zero user-controlled input to validate, eliminating injection risks entirely. The SQL query (`SELECT ... FROM device_probes ORDER BY id`) uses no user-supplied values.

3. **Parameterized queries throughout.** The existing `DeviceProbeRepo` uses parameterized queries via pgx for all database operations (visible in `device_probe_repo.go`). The new `ListAll` method is even simpler -- it has no parameters at all, just a static SELECT with ORDER BY. No SQL injection vector exists.

4. **No modification to existing endpoints.** The ADD explicitly preserves the existing per-device endpoint response shape by introducing a new `DeviceProbeWithResultEnriched` struct rather than modifying `DeviceProbeWithResult`. This avoids unintended data exposure changes on existing endpoints.

5. **CORS configuration is restrictive.** The CORS middleware (`middleware/cors.go`) only allows explicitly configured origins, not wildcards. Credentials (`Access-Control-Allow-Credentials: true`) are only set when the origin is in the allowlist. This prevents cross-origin credential theft targeting the new endpoint.

6. **Application-level enrichment avoids SQL complexity.** The decision to enrich with hostname in Go code (fetch devices, build map, merge) rather than a SQL JOIN avoids introducing new query patterns that could have injection risks if parameterized incorrectly. The approach mirrors the existing `ListDeviceProbes` handler pattern.

7. **Single-user authorization model is correctly identified.** The ADD acknowledges this is a single-admin system and correctly identifies that no additional authorization logic is needed beyond authentication. There are no IDOR risks because there is only one user role.

---

## Verdict Rationale

**APPROVED**: No CRITICAL or HIGH findings. The three LOW/INFORMATIONAL findings are defense-in-depth observations, not exploitable vulnerabilities. The new endpoint is read-only, behind admin authentication, introduces no user input surface, uses no dynamic SQL, and follows established codebase patterns. The architecture decisions are security-sound.

The implementation may proceed. The QA engineer should verify:
- The new route is registered inside the `adminAuth` group in `router.go`, not in a public or device-auth section.
- The frontend does not use `dangerouslySetInnerHTML` for rendering `device_hostname` or `probe.name`.
- The `ListAll` query contains no user-supplied parameters.
