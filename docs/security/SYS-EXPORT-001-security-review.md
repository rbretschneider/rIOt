# Security Review Report
**Story ID:** SYS-EXPORT-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-04-08
**Verdict:** APPROVED WITH CONDITIONS

---

## Threat Model Summary

### Assets
- **Device telemetry data** (read-only): system identity, hardware serial numbers, UUIDs, network MACs/IPs, USB device serials, Docker container inventory
- **Container environment variables** (`ContainerInfo.Env`): present in the `FullTelemetryData` struct and could contain secrets (API keys, database passwords, tokens) -- must never appear in the summary output
- **Container labels** (`ContainerInfo.Labels`): could contain secrets or internal URLs
- **Session integrity**: admin JWT cookie used for authentication

### Threat Actors
- **Unauthenticated external attacker**: attempting to access device summaries without a session
- **Authenticated low-privilege user**: not applicable -- rIOt has a single admin role; all authenticated users are admins
- **Automated systems**: bots attempting to enumerate device data or DoS the endpoint

### Attack Surface Introduced
- One new GET endpoint: `GET /api/v1/devices/{id}/summary`
- One new response content type: `text/markdown` (all existing endpoints return JSON)
- New `Content-Disposition` header with dynamic hostname value
- New frontend fetch pattern using raw `fetch()` instead of `fetchJSON()`
- New `text/template` rendering pipeline consuming telemetry data

---

## Findings

### HIGH

#### SEC-001: Content-Disposition header injection via unsanitized hostname
**Severity:** HIGH
**Domain:** Input Validation / HTTP Header Injection
**Location:** ADD Section 12, item 7 (filename generation); handler `Content-Disposition` header
**Description:**
The ADD specifies computing the filename as `fmt.Sprintf("%s-summary-%s.md", device.Hostname, ...)` and notes hostname should be sanitized to "remove characters invalid in filenames (replace non-alphanumeric except hyphens/underscores with underscores)." However, the sanitization requirement is mentioned only in an implementation note, not as a firm constraint. The `Device.Hostname` field comes from agent self-registration (`DeviceRegistration.Hostname`) and is agent-supplied. If the hostname contains newline characters (`\r\n`), double quotes, or other HTTP header control characters, the `Content-Disposition` header value could be corrupted or injected with additional headers (HTTP response splitting). While Go's `net/http` package mitigates full CRLF injection in headers since Go 1.7, malformed filenames with quotes, semicolons, or unicode can still break the `Content-Disposition` parsing in browsers, leading to unexpected download behavior or file path issues on the client filesystem.
**Attack Scenario:**
1. An attacker registers a device with a hostname containing crafted characters, e.g., `evil"; filename=malware.exe; x="` or embedded null bytes.
2. Admin user views the device and clicks "Download Summary."
3. The `Content-Disposition` header is set with the unsanitized hostname, resulting in a malformed header.
4. Depending on the browser, the downloaded file could have an unexpected name or the download could fail.
**Required Resolution:**
The hostname used in the `Content-Disposition` header filename MUST be sanitized to ASCII alphanumeric characters, hyphens, and underscores only. All other characters must be stripped or replaced. The filename must also be quoted in the header value per RFC 6266. This must be specified as a requirement in the ADD, not just an implementation note.
**Blocks:** Implementation

---

### MEDIUM

#### SEC-002: Container environment variables reachable by template layer
**Severity:** MEDIUM
**Domain:** Data Exposure
**Location:** ADD Section 10 (Security Considerations); AD-006 (SummaryData struct)
**Description:**
The `FullTelemetryData.Docker.Containers` slice contains `ContainerInfo` structs, each with an `Env []EnvVar` field holding parsed KEY=VALUE environment variables. These frequently contain secrets (database passwords, API keys, tokens). The ADD correctly states these must not be rendered and AD-006 specifies a `SummaryData` struct that acts as a filter. However, the ADD does not explicitly require that the `SummaryData` struct for Docker containers omit the `Env`, `Labels`, `Mounts`, `Networks`, `Ports`, and other sensitive container detail fields. The template specification in Section 13 shows only Name, Image, State, Status for containers -- but an engineer could map the full `ContainerInfo` struct to the template data if the filtering requirement is not explicit.
**Attack Scenario:**
1. An engineer, following the template spec but not carefully filtering the struct, maps full `ContainerInfo` objects into the `SummaryData`.
2. A future template modification or debugging change accidentally renders `{{.Env}}` or iterates over all fields.
3. The exported Markdown file now contains database credentials, API tokens, and other secrets from container environment variables.
4. The user shares this "inventory document" in a support request or pastes it in a public forum.
**Required Resolution:**
The ADD must explicitly enumerate which `ContainerInfo` fields are mapped into the summary data struct for Docker containers: only `Name`, `Image`, `State`, and `Status`. All other fields (especially `Env`, `Labels`, `Mounts`, `Networks`) must be explicitly excluded from the template data structure. This is a defense-in-depth requirement -- the template cannot render what the data struct does not contain.
**Condition for QA:** Verify that the `SummaryData` container struct contains exactly four fields and that a test with containers having populated `Env` fields produces output containing none of those values.

---

#### SEC-003: Telemetry data rendered via text/template without escaping considerations
**Severity:** MEDIUM
**Domain:** Injection / Data Integrity
**Location:** AD-001 (text/template), ADD Section 12 item 2
**Description:**
The ADD specifies using `text/template` (not `html/template`) because the output is Markdown, not HTML. This is a reasonable choice. However, `text/template` performs zero escaping of any kind. Telemetry string fields (hostname, container names, container images, USB device descriptions, disk model names) are agent-supplied and could contain Markdown control characters (`|`, `#`, `*`, `[`, `]`, `` ` ``) or pipe characters that break table formatting. While this is not an XSS vector (the server returns plain text, not rendered HTML), it has two implications: (1) malformed Markdown output could confuse users, and (2) if the Markdown is later rendered in a context that supports HTML-in-Markdown (most Markdown renderers do), injected HTML/JavaScript in telemetry fields would execute. The ADD notes "the frontend downloads or copies the text without rendering it as HTML" -- but the user may paste this Markdown into GitHub, a wiki, or any tool that renders it.
**Attack Scenario:**
1. An attacker with access to register a rogue agent sets a hostname or container name to `<img src=x onerror=alert(1)>` or `[click me](javascript:alert(1))`.
2. Admin exports the device summary as Markdown.
3. Admin pastes the Markdown into a GitHub issue, wiki, or any Markdown renderer.
4. The injected content renders as HTML, potentially executing JavaScript in the context of the rendering platform.
**Required Resolution:**
The ADD should acknowledge that telemetry string fields may contain Markdown special characters. At minimum, pipe characters (`|`) in table cell values must be escaped (replaced with `\|`) to prevent table corruption. The ADD should note that downstream Markdown rendering of user-controlled content is outside the security boundary of rIOt, but that basic sanitization of Markdown-breaking characters in table values is required for output integrity.
**Condition for QA:** Verify that telemetry values containing `|` characters do not break table formatting in the generated Markdown.

---

### LOW / INFORMATIONAL

#### SEC-004: No dedicated rate limiting on the summary endpoint
**Severity:** LOW
**Domain:** Rate Limiting / Abuse Prevention
**Location:** ADD Section 10 (Rate limiting)
**Description:**
The ADD states "No additional rate limiting beyond the existing middleware" and notes the endpoint is lightweight (two DB reads + CPU rendering). This is an acceptable assessment. The endpoint requires admin JWT authentication, which limits the attack surface to authenticated sessions only. The existing global middleware stack does not include a general rate limiter (rate limiters are applied only to login and registration). An authenticated attacker could issue rapid requests to this endpoint, but the impact is limited: the endpoint is read-only, performs indexed lookups, and generates a small text response. The CPU cost of template rendering on a small data structure is negligible.
**Attack Scenario:**
An attacker with a stolen admin session cookie floods the endpoint with requests. Each request performs two indexed DB queries. At high volume, this could contribute to database connection pool exhaustion, but no more so than any other authenticated device endpoint.
**Required Resolution:**
None required. This is informational. If the application later adds a global authenticated-request rate limiter, this endpoint will benefit automatically. No endpoint-specific rate limiting is warranted for this read-only, low-cost endpoint behind admin auth.

---

#### SEC-005: Error message differentiation between "device not found" and "no telemetry"
**Severity:** LOW
**Domain:** Information Disclosure
**Location:** ADD Section 6 (Error Responses), Section 9 (Error Handling)
**Description:**
The endpoint returns two different 404 error messages: `"device not found"` vs `"no telemetry data available"`. This allows an authenticated admin to distinguish between a non-existent device ID and a valid device with no data. Since the endpoint requires admin authentication and admins already have full access to the device list, this differentiation reveals no information the admin does not already possess. However, it is worth noting for consistency: the ADD acknowledges this follows the existing `GetDevice` pattern.
**Attack Scenario:**
None meaningful. An admin can already list all devices via `GET /api/v1/devices`.
**Required Resolution:**
None. This is consistent with existing patterns and the information is already available to the authenticated user.

---

#### SEC-006: Frontend raw fetch pattern should verify Content-Type
**Severity:** LOW
**Domain:** Defense in Depth
**Location:** ADD Section 12, item 9 (API client method)
**Description:**
The ADD specifies the frontend API client will use raw `fetch()` instead of `fetchJSON()` for this endpoint. The proposed implementation parses error responses as JSON (`res.json()`) but reads success responses as text (`res.text()`). If a man-in-the-middle or a bug causes the endpoint to return unexpected content (e.g., an HTML error page from a reverse proxy), the frontend would pass it through to the clipboard or file download without validation. Checking that the response `Content-Type` is `text/markdown` before processing would add a defense layer.
**Required Resolution:**
None strictly required. This is a defense-in-depth suggestion. The connection is same-origin over TLS in any production deployment. Note for the QA engineer to verify the frontend handles non-200 responses gracefully.

---

## Positive Observations

1. **AD-006 (SummaryData struct as a filter)** is an excellent security decision. By creating a purpose-built struct rather than passing raw model types to the template, the architecture enforces data minimization at the type level. This is the right pattern for preventing accidental data exposure.

2. **Authentication reuse** is correctly specified. The endpoint sits behind the same `middleware.AdminAuth(s.JWTSecret)` as all other device admin endpoints, registered in the same route group. No custom auth is needed.

3. **Server-side rendering** prevents the frontend from having access to the raw `FullTelemetryData` for this feature (it only receives the pre-filtered Markdown text). This is a better security posture than having the frontend assemble the summary from the full telemetry JSON.

4. **No new database queries or schema changes** -- the feature operates entirely within the existing data access patterns, adding no new attack surface at the data layer.

5. **CORS configuration** is correctly restrictive (explicit origin allowlist, no wildcards). The new endpoint inherits this automatically.

6. **Read-only operation** -- no state mutations, no writes, no side effects. The blast radius of any issue is limited to information disclosure.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS**: No CRITICAL findings. One HIGH finding (SEC-001: Content-Disposition header injection via hostname) has a clear, non-architectural resolution -- the ADD already mentions sanitization but must elevate it from an implementation note to a firm requirement with a specific character allowlist. Two MEDIUM findings require explicit data field filtering documentation (SEC-002) and Markdown table escaping (SEC-003). The engineering team must address SEC-001 and SEC-002 as part of implementation. The QA engineer must verify that hostname sanitization is applied, that container environment variables do not appear in output, and that pipe characters in telemetry values do not break table formatting.
