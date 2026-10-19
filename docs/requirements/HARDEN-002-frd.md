# Formal Requirements Document

**Story ID:** HARDEN-002
**Title:** Close live exposures and the backup gap (code-side): pprof lockdown, security headers, notification-secret redaction, constant-time registration key, honest health/readiness, notification retry claim
**Author:** Business Developer Agent
**Date:** 2026-10-17
**Status:** DRAFT

---

## 1. Executive Summary

A 2026-10-17 security review of the rIOt server in its default Docker deployment (riot-server
publishing `0.0.0.0:7331`) found several exposures and one documentation falsehood. This
story closes the code-side items. (1) The Go profiler (`/debug/pprof`) is reachable on the
public listener without authentication. It must become unreachable unauthenticated: off by
default, and opt-in only on a loopback-only listener. (2) No security response headers are
sent. Every response must carry a Content-Security-Policy that works with the embedded
dashboard and terminal, anti-framing, `nosniff`, a Referrer-Policy, and HSTS only when the
connection is HTTPS. (3) Notification-channel secrets (SMTP passwords, bot tokens,
token-bearing webhook URLs, push tokens) come back in cleartext from the settings API. They
must be redacted on every response and treated as write-only on update, so editing a channel
without retyping its secrets keeps them. (4) The device registration key is compared with a
plain string comparison, which leaks timing. It must use a constant-time comparison. (5)
`/health` returns HTTP 200 even when the database is down. It must return 503 when degraded,
keep its body shape, and gain a `/readyz` readiness endpoint. (6) The README promises an
automatic notification retry queue, but nothing calls that code. The README must become true,
either by wiring the queue in safely or by removing the claim. The agent API is unchanged by
all of this. A pg_dump backup sidecar for `riot-db` and its restore runbook are a related
deliverable handled separately as ops work. They are not part of this code story.

---

## 2. Background & Context

### 2.1 Findings (verified against the code at commit `a7b9f9e`)

| # | Finding | Location | Current behavior | Exposure |
|---|---|---|---|---|
| F-1 | pprof mounted with no auth | `internal/server/router.go` ~L203–205 (`r.Mount("/debug/pprof", pprofHandler())`) | The comment says it is "bound to the server's listen address (not exposed beyond the host/network)". That is false: the listen address is `:<port>` (all interfaces, `server.go` ~L198). `GET /debug/pprof/` returns 200 unauthenticated in the default deployment. | Anyone who can reach `:7331` can read goroutine stacks, heap profiles and `cmdline`. They can also start 30-second CPU profiles and execution traces (`/profile`, `/trace`), which is a cheap resource-exhaustion vector. |
| F-2 | No security headers | Global middleware stack in `router.go` ~L37–44 (RealIP, Logger, Recoverer, CORS, SetupGuard) | No CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy or HSTS on any response. | Clickjacking of the admin dashboard, MIME-sniffing, no script-injection containment, no HSTS pinning when served over HTTPS. |
| F-3 | Notification-channel secrets returned in cleartext | `internal/server/handlers/settings.go` `ListNotificationChannels` ~L102. `CreateNotificationChannel` ~L115 and `UpdateNotificationChannel` ~L136 also echo the full submitted channel back. | Every `config` key is serialized verbatim, including `password`, `bot_token`, `webhook_url`, `token`, `api_token`, `app_token`, webhook `url` and webhook header values. | Any holder of an admin session, any XSS, any browser or devtools cache, and any intermediary that logs response bodies receives the credentials for the operator's notification services. |
| F-3b | Secrets leak through error text (found during intake) | `notify/*.go` backends and `dispatcher.go`. Go HTTP client errors embed the full request URL (for example the Telegram `…/bot<TOKEN>/…` path, or the Discord/Slack webhook URL). | `TestNotificationChannel` returns `err.Error()` to the browser. `Dispatch`/`SendToChannel` store `err.Error()` in `notification_log.error_msg` and in an `slog.Error` line, which the DB log store persists (WARN and above) and `GET /api/v1/settings/logs` serves. | The same secrets as F-3, through the notification log, the server log viewer and test-send responses. |
| F-4 | Timing-unsafe registration key comparison | `internal/server/handlers/handlers.go` `RegisterDevice` ~L155–164 (`if apiKey != regKey`) | Go string inequality can short-circuit on the first differing byte or on a length mismatch. | Timing side channel on the registration key that gates who can enroll devices. Low practical risk behind the 10/min register rate limiter, but trivially fixable. |
| F-5 | Health always 200 | `handlers.go` `Health` ~L140–153 | Returns 200 with `{"status":"degraded","database":false,"time":…}` when the DB is unreachable. | Orchestrators, reverse proxies and uptime monitors that key on the status code see a healthy server while it cannot serve data. There is no readiness endpoint. |
| F-6 | Retry queue documented but dead | `internal/resilient/retry_queue.go`. README L37 ("…and automatic retry queue") and README §"Notification Retry Queue" ~L996–998. | Nothing imports `RetryQueue`. Failed notifications are logged as `failed` and never retried. | The README makes a false reliability claim, so the operator may rely on alerts being retried when they are not. |

### 2.2 Implementation realities found during intake (load-bearing for requirements)

1. **The SPA catch-all serves HTTP 200 for every unmatched GET.** `serveFrontend` (`router.go`
   ~L308–324) returns `index.html` with 200 for any path chi does not match. Removing the
   pprof mount alone would therefore make `GET /debug/pprof/` return **200 with the dashboard
   HTML**. That is safe, but it is indistinguishable from "still exposed" to a scanner and to
   a status-code test. The OIDC-001 work hit the same issue (see the router comment near
   L117–121). The pprof path must return an explicit 404 on the main listener (FR-002).
2. **Loopback inside a container is the container's own loopback.** In the shipped deployment riot-server runs
   in Docker. A pprof listener bound to `127.0.0.1:6060` inside the container is reachable only
   from inside that container (for example `docker exec riot-server wget -qO- http://127.0.0.1:6060/debug/pprof/heap`).
   A Docker port mapping cannot reach it. This is the intended secure posture, and it must be
   documented, because existing operator notes reference "pprof at /debug/pprof" as an
   operational tool.
3. **Migrations finish before the HTTP listener starts.** `server.go` `Start` runs
   `RunMigrations` and returns a fatal error on failure *before* `setupRouter`/`ListenAndServe`.
   So "migrations complete" is true by construction whenever `/readyz` can answer. The
   readiness model still represents it explicitly (FR-037), so that the property is tested and
   survives future refactors.
4. **Known consumers of `/health`.** (a) `riot-agent doctor` (`internal/agent/doctor.go`
   `checkServer` ~L469–484) issues `GET /health`. It checks only the status code (200 means
   pass, anything else means warn) and never parses the body. A 503 during a DB outage makes
   doctor print "Health endpoint returned 503", which is an accurate improvement and needs no
   agent change. (b) The repository `Dockerfile` has **no** `HEALTHCHECK`, and the repository
   `docker-compose.yml` defines a healthcheck only for `riot-db` (`pg_isready`), not for
   riot-server. (c) Operator-side compose files may add their own healthcheck; see OQ-4.
5. **The retry queue code, as written.** Observed facts that inform the FR-042 decision:
   - It is memory-bounded: `maxSize` defaults to 100 and drops the oldest item on overflow.
   - It owns no goroutine. Draining requires a new caller-driven loop. The server's background
     workers today run on `context.Background()`, which is never cancelled on shutdown, so
     "respects shutdown" is not available from existing plumbing.
   - `Drain` holds the queue mutex while it performs synchronous, context-less network sends.
     A slow or hung endpoint therefore blocks `Enqueue`, which sits on the alert dispatch path.
   - The item model is `{URL, raw JSON payload}`. That fits only HTTP-POST-style backends.
     SMTP, and Telegram (token in the URL path), do not map onto it. The README itself
     limits the claim to "ntfy, webhook".
   - Persistence writes the target URL and payload to a JSON file in cleartext. For
     token-bearing URLs that writes secrets to disk. The shipped compose gives riot-server no
     persistent volume, so "persists across server restarts" would not hold in the default
     Docker deployment anyway.
   - There is no attempt cap and no backoff. Persistence errors are silently discarded.
6. **Secure-cookie predicate already exists.** OIDC-001 added a trusted-proxy-gated resolver
   (`RIOT_TRUSTED_PROXIES`, `X-Forwarded-Proto`) that decides when the session cookie gets
   `Secure`. HSTS must use the **same** "is this request HTTPS" predicate (FR-014), so the two
   can never disagree.
7. **The frontend never re-reads secrets.** `web/src/api/settings.ts` lists channels with
   `GET /notification-channels`, updates with `PUT /notification-channels/{id}`, and tests with
   `POST /notification-channels/{id}/test` (by ID only, using stored config). Test-send
   therefore keeps working as long as the stored secret is preserved.

### 2.3 Settled constraints (inputs, not open for redesign)

| # | Constraint |
|---|---|
| C-1 | No breaking change to the agent API: `/api/v1/devices/*`, `/api/v1/devices/register`, `/api/v1/update/check`, `/ws/agent`, `/api/v1/enroll`, `/api/v1/renew`, `/api/v1/server-cert`, and `X-rIOt-Key` semantics stay byte-compatible for every released agent version. |
| C-2 | Existing tests stay green: `go test ./...` and `cd web && npm run test:run`. |
| C-3 | Every acceptance criterion must be verifiable by an automated test (Go `httptest`/unit or vitest), except where §8 explicitly marks an AC as a code-review check. |
| C-4 | Default configuration must be secure: no opt-in step is needed to close F-1 through F-5. |
| C-5 | The pg_dump backup sidecar and restore runbook are ops deliverables run separately by the orchestrator, not part of this code story (§9, §12.1). |

---

## 3. Actors

| Actor | Description | Permissions |
|---|---|---|
| Operator / admin | Single admin of the self-hosted deployment. Uses the dashboard (password or OIDC session) and the host shell. | Full dashboard access through the admin JWT cookie. Can set environment variables such as `RIOT_PPROF_ADDR`. Can reach the container loopback with `docker exec`. After this story, can **write** notification secrets but cannot **read them back** through the API. |
| Dashboard (browser) | The embedded React/Vite SPA, including the xterm.js terminal over `/ws/terminal/...` and the live `/ws` feed. | Same-origin fetches and WebSockets under the admin cookie. Bound by the CSP. |
| rIOt agent | Go daemon on each device. Registers with `X-rIOt-Key` and pushes heartbeat/telemetry. | Unchanged (C-1). Registration requires the configured registration key, if one is set. |
| Orchestrator / uptime monitor | Docker healthcheck, reverse proxy, Uptime Kuma or similar probing the server. | Unauthenticated `GET /health` and `GET /readyz`. Receives only booleans, a status word and a timestamp. |
| Reverse proxy (optional) | TLS-terminating proxy listed in `RIOT_TRUSTED_PROXIES`. | Its `X-Forwarded-Proto` is honored for the HTTPS predicate (cookie `Secure` and HSTS). Headers from untrusted peers are ignored. |
| OIDC identity provider (optional) | External IdP such as authentik, used in the SSO redirect flow. | Receives the browser redirect from `/api/v1/auth/oidc/start` and redirects back to `/api/v1/auth/oidc/callback`. Must keep working under the new headers. |
| Notification services | SMTP, Telegram, Discord, Slack, ntfy, Pushover, Gotify, generic webhooks. | Receive alerts using the stored secrets. Unaffected by redaction. |
| Attacker (threat model, not a user) | Network-adjacent party reaching `:7331`, a malicious page attempting to frame the dashboard, or an XSS payload or session thief reading API responses. | Must not obtain profiling data, must not frame the dashboard, must not read notification secrets, and must not gain a timing oracle on the registration key. |

---

## 4. Functional Requirements

### 4.1 Profiler (pprof) lockdown — F-1

**Security invariant (I-1):** In **no** configuration may the main HTTP(S) listener (the one
bound to `RIOT_PORT`) serve any pprof endpoint to an unauthenticated request. Profile data may
be served unauthenticated **only** on a dedicated listener bound to a loopback IP address.

- **FR-001:** The pprof handlers must not be mounted on the main router unauthenticated. The
  unconditional `r.Mount("/debug/pprof", pprofHandler())` and its incorrect "bound to the
  host" comment must be removed.
- **FR-002:** When no admin-authenticated pprof variant is enabled (FR-007), every request to
  `/debug/pprof` and `/debug/pprof/*` on the main listener must return HTTP 404 with a body
  that contains no pprof output. The SPA catch-all must not answer these paths with
  `index.html`/200.
- **FR-003:** pprof must be disabled by default. With `RIOT_PPROF_ADDR` unset or empty, the
  server must start no pprof listener.
- **FR-004:** When `RIOT_PPROF_ADDR` is set to `<ip>:<port>` and `<ip>` is a literal loopback
  IP address (any address in `127.0.0.0/8`, or `::1`), the server must start a separate HTTP
  listener on exactly that address, serving the same pprof endpoints the current
  `pprofHandler` serves (index, cmdline, profile, symbol, trace, heap, goroutine, allocs)
  under `/debug/pprof/`.
- **FR-005:** When `RIOT_PPROF_ADDR` is set to anything other than a literal loopback IP and
  port, the server must not start a pprof listener, must log an error naming the rejected
  value and the reason, and must continue starting normally. Rejected values include an empty
  host (`:6060`), `0.0.0.0`, `::`, any non-loopback IP, any hostname including `localhost`,
  a missing port, and an unparseable value.
- **FR-006:** When the pprof listener is started, the server must log at warning level that
  profiling is enabled and on which address. The pprof listener must be closed during server
  shutdown. A failure to bind the pprof listener must be logged and must not prevent the main
  server from starting.
- **FR-007 (optional, architect's choice):** The architect may additionally offer pprof on the
  main router behind the existing admin JWT middleware (`middleware.AdminAuth`). If offered,
  it must be disabled by default, enabled only by an explicit environment variable separate
  from `RIOT_PPROF_ADDR`, and must return 401 to unauthenticated requests, so invariant I-1
  holds. If not offered, FR-002 applies unconditionally.

### 4.2 Security response headers — F-2

- **FR-008:** The server must set the headers in FR-009 to FR-013 on every HTTP response
  produced by the main listener. That includes the SPA `index.html`, static assets, API JSON
  responses, agent endpoints, `/health`, `/readyz`, redirects (including the OIDC redirects),
  CORS preflight `OPTIONS` 204 responses, 401/403/404 responses, and 500 responses produced
  by panic recovery. The single exception is the `101 Switching Protocols` response of a
  WebSocket upgrade, whose header set the WebSocket library controls.
- **FR-009:** `Content-Security-Policy` must be present and must contain at least these
  directives:
  - `default-src 'self'`
  - `script-src 'self'`, with neither `'unsafe-inline'` nor `'unsafe-eval'`
  - `style-src 'self' 'unsafe-inline'` (see D-07: xterm.js and runtime-styled components
    inject styles)
  - `img-src 'self' data:`
  - `font-src 'self' data:`
  - `connect-src 'self'` plus an allowance for same-host WebSocket connections over both `ws:`
    and `wss:`, so `/ws`, `/ws/terminal/{deviceId}/{containerId}` and any other dashboard
    WebSocket connect under both plain-HTTP setup mode and HTTPS
  - `object-src 'none'`
  - `base-uri 'self'`
  - `frame-ancestors 'none'`
- **FR-010:** The CSP must not contain `upgrade-insecure-requests`, because it would break the
  plain-HTTP setup wizard and HTTP deployments. It must not contain a `form-action` directive
  (D-08), so the OIDC redirect chain cannot be blocked.
- **FR-011:** `X-Frame-Options: DENY` must be present.
- **FR-012:** `X-Content-Type-Options: nosniff` must be present.
- **FR-013:** `Referrer-Policy: strict-origin-when-cross-origin` must be present (D-09).
- **FR-014:** `Strict-Transport-Security: max-age=31536000` must be present **if and only if**
  the request is HTTPS. That means the connection itself is TLS, or the immediate peer is in
  `RIOT_TRUSTED_PROXIES` and supplied `X-Forwarded-Proto: https`. The predicate must be the
  same one that decides the session cookie's `Secure` attribute. The value must not include
  `includeSubDomains` or `preload` (D-10).
- **FR-015:** `Strict-Transport-Security` must never be sent on a plain-HTTP request, including
  when an untrusted peer supplies `X-Forwarded-Proto: https`.
- **FR-016:** Static assets served from the embedded frontend must carry a `Content-Type` that
  matches their type (HTML, JavaScript, CSS, JSON/webmanifest, images, fonts), so that
  `nosniff` does not cause the browser to reject them.
- **FR-017:** Adding the headers must not change the status code, body or other headers of any
  existing route. That includes OIDC `302` redirects and their `Location`, `Set-Cookie`, the
  CORS headers, and agent API responses (C-1).

### 4.3 Notification-channel secret redaction (write-only secrets) — F-3, F-3b

- **FR-018:** The server must keep an authoritative registry of secret config keys per channel
  type, at minimum as listed in §7.1 Table T-1. A channel whose `type` is not in the registry
  must have **every** config value treated as secret.
- **FR-019:** Every API response that serializes a notification channel must replace the value
  of each secret key with the redaction sentinel (§7.2) when the stored value is non-empty.
  That covers the `GET` list, the `POST` create response, the `PUT` update response, and any
  other HTTP or WebSocket output that includes channel config. Secret values must not appear
  anywhere in the response body.
- **FR-020:** When a secret key is absent or its stored value is the empty string, the response
  must return that key as the empty string (or omit it), never as the sentinel. A client can
  therefore tell "configured" from "not configured".
- **FR-021:** For the `webhook` type, the `headers` map must be returned with every header
  **name** visible and every header **value** replaced by the sentinel (FR-020 applies to
  empty values).
- **FR-022:** Non-secret config keys (for example SMTP `host`, `port`, `username`, `from`, `to`,
  `starttls`, Telegram `chat_id`, Gotify `server_url`, ntfy `server_url`/`priority`/`priority_map`)
  and the channel's `id`, `name`, `type` and `enabled` must be returned unchanged.
- **FR-023:** On `PUT /api/v1/settings/notification-channels/{id}`, each secret key must be
  resolved against the stored channel using the merge rules in §7.3 Table T-2:
  sentinel → keep stored; key omitted → keep stored; new non-empty value → replace;
  explicit empty string → clear.
- **FR-024:** The sentinel literal must never be persisted as a secret value. If a `PUT`
  submits the sentinel for a key that has no stored non-empty value (including after a channel
  `type` change), the key must be stored as empty.
- **FR-025:** For the `webhook` `headers` map on `PUT`: if `headers` is omitted entirely, the
  stored headers must be kept unchanged. If `headers` is present, it defines the complete
  desired set of header names. A name whose submitted value is the sentinel keeps its stored
  value (or is stored empty if it had none, per FR-024). A name with a new value takes that
  value. A stored name absent from the submitted map is removed.
- **FR-026:** Non-secret keys on `PUT` must keep today's full-replacement semantics. The
  write-only rules apply only to registry secret keys.
- **FR-027:** `PUT` to a channel ID that does not exist must return 404 and must not write
  anything.
- **FR-028:** `POST` (create) must store submitted secret values exactly as submitted. A
  submitted sentinel on create must be stored as empty (FR-024).
- **FR-029:** `POST /api/v1/settings/notification-channels/{id}/test` must keep sending with
  the stored (unredacted) config, so test-send works after an edit that did not retype
  secrets.
- **FR-030:** Any error text that leaves the notification subsystem must have every non-empty
  secret value of the channel involved replaced by the sentinel. That covers the test-send
  response `error` field, `notification_log.error_msg`, and the server log line emitted on
  send failure. It applies to both the full secret value and, for URL-type secrets, the full
  URL string.
- **FR-031:** The dashboard notification-channel editor must keep working under redaction.
  Opening and saving a channel without touching its secret inputs must submit each secret
  either as the sentinel or omitted, never as an empty string. A secret input whose received
  value is the sentinel must show a "configured / unchanged" state rather than the sentinel
  literal as editable text. Typing a new value must submit that value. The operator must have
  an explicit way to clear an optional secret (for example the ntfy `token`), which submits
  an empty string.
- **FR-032:** The demo frontend client (`web/src/api/demo-settings*`) must return
  sentinel-redacted secrets for any mock channels it serves, so the public demo models the
  real contract and never displays realistic-looking secrets.

### 4.4 Constant-time registration key comparison — F-4

- **FR-033:** `RegisterDevice` must compare the `X-rIOt-Key` request header with the
  configured registration key using a constant-time primitive from Go's `crypto/subtle`. The
  comparison must not short-circuit on the first differing byte and must not reveal the
  configured key's length through timing (D-11).
- **FR-034:** The externally visible behavior of `RegisterDevice` must be unchanged. With no
  key configured, registration is open. With a key configured, a missing or non-matching
  header returns 401 with body `{"error":"invalid registration key"}`, and a matching header
  proceeds exactly as today, including the re-registration and enrolled-device key-issuance
  paths.

### 4.5 Health and readiness — F-5

- **FR-035:** `GET /health` must return HTTP 200 with `"status":"ok"` when the database is
  reachable. It must return **HTTP 503** with `"status":"degraded"` when the database is not
  reachable.
- **FR-036:** The `/health` body must keep its existing shape exactly: a JSON object with
  `status` (string, `"ok"` or `"degraded"`), `database` (boolean) and `time` (RFC 3339 UTC
  timestamp), and no other keys. `Content-Type` must be `application/json`.
- **FR-037:** A new `GET /readyz` endpoint must return HTTP 200 when the server is ready, and
  HTTP 503 otherwise. Ready means the database is reachable **and** schema migrations have
  completed successfully in this process. The body must be a JSON object with `status`
  (`"ready"` or `"not_ready"`), `database` (boolean), `migrations` (boolean) and `time`
  (RFC 3339 UTC).
- **FR-038:** `/health` and `/readyz` must be reachable unauthenticated, must not be subject
  to the setup guard (they answer identically before and after setup completes), and must
  not be served by the SPA catch-all.
- **FR-039:** The database check behind both endpoints must be bounded in time (NFR-005). A
  check that does not finish within the bound counts as "database unreachable".
- **FR-040:** Neither endpoint's body may include error messages, connection strings, host
  names, versions or any other detail beyond the fields in FR-036 and FR-037.
- **FR-041:** The `riot-agent doctor` server check must keep working against the new
  semantics: 200 reports pass, any other status (including 503) reports a warning with the
  status code, and doctor neither crashes nor parses the body. No agent change is required
  beyond keeping this behavior covered by a test.

### 4.6 Notification retry claim — F-6

**Invariant (I-2):** After this story, the README must not describe any notification retry
behavior that the code does not perform.

- **FR-042:** The architect must choose exactly one of **Option A (wire in)** or **Option B
  (remove the claim)** and record the choice and its rationale in the architecture document.
  The BA recommendation and the FRD default is **Option B**, based on the code facts in §2.2
  item 5 (lock held across network I/O, no context or shutdown handling, a URL-only item model
  that does not fit most backends, secret-bearing URLs persisted to disk, no attempt cap,
  persistence not durable in the default compose). Option A is acceptable only if FR-045 to
  FR-052 are all met.

**Option B — remove the claim (default):**

- **FR-043:** The README Features bullet (L37) must no longer mention an automatic retry queue,
  and the README "Notification Retry Queue" subsection (~L996–998) must be removed. Any other
  README or docs statement claiming automatic notification retry must be removed or corrected.
- **FR-044:** `internal/resilient/retry_queue.go` and any test file dedicated to it must be
  deleted. Other files in `internal/resilient` (for example the agent DNS cache) must be left
  intact and working.

**Option A — wire in (only if chosen):**

- **FR-045:** When a delivery to a covered channel type fails in `Dispatch` or `SendToChannel`,
  the failure must be enqueued for retry. The README must name exactly the channel types
  covered. Test-send (`TestChannel`) failures must never be enqueued.
- **FR-046:** The queue must stay bounded at a configured maximum size (default 100) and drop
  the oldest item on overflow, logging the drop.
- **FR-047:** Each item must be retried with increasing delay and dropped, with a logged reason,
  after a bounded number of attempts or a bounded age (architect sets the bounds and documents
  them in the README).
- **FR-048:** A network send in progress during a retry pass must not block `Enqueue`. A hung
  endpoint must not stall alert dispatch.
- **FR-049:** The retry worker must stop when the server shuts down, finishing within the
  existing 10-second shutdown deadline, and must leave no goroutine running after stop.
- **FR-050:** Retry state persisted to disk (if any) must not contain secret values in
  cleartext. Retries must re-resolve channel credentials from the database at send time,
  identifying the channel by ID. If persistence is kept, the README must state where the
  state lives and that it survives restarts only when that path is on a persistent volume.
- **FR-051:** If an item's channel has been deleted or disabled when it comes up for retry,
  the item must be dropped without a send attempt.
- **FR-052:** Each retry outcome must be recorded in `notification_log` (successful retry as
  `sent`, final abandonment as `failed` with a reason), with secrets scrubbed per FR-030.

### 4.7 Cross-cutting

- **FR-053:** All changes must keep the agent API contract (C-1) unchanged: routes, methods,
  status codes, request and response bodies, and `X-rIOt-Key` and mTLS semantics.
- **FR-054:** The README must document `RIOT_PPROF_ADDR` (default off, loopback-only, reached
  with `docker exec` when containerized), the `/readyz` endpoint and the 503 semantics of
  `/health`, the write-only behavior of notification secrets, the fact that the dashboard can
  no longer be embedded in an iframe, and HSTS behavior behind a trusted proxy. `CHANGELOG`
  must record each behavior change.

---

## 5. Non-Functional Requirements

- **NFR-001 [Security]:** Invariant I-1 (§4.1) holds in every configuration and is covered by
  tests for the default, loopback-enabled, rejected-address and (if built) admin-variant
  configurations.
- **NFR-002 [Security]:** After a secret is written, no API response, WebSocket message,
  notification-log row or server-log row produced by rIOt contains that secret's cleartext
  value (I-3). Storage at rest in Postgres is unchanged by this story (residual risk, §9).
- **NFR-003 [Security]:** The pprof listener accepts only literal loopback IP addresses.
  Hostnames are rejected even when they would resolve to loopback, so no DNS or hosts-file
  manipulation can widen the bind.
- **NFR-004 [Performance]:** The security-headers middleware performs no I/O and no
  per-request allocation-heavy work beyond setting headers. Request latency on existing
  endpoints must not measurably regress (existing benchmarks, if any, stay within noise).
- **NFR-005 [Performance / Availability]:** `/health` and `/readyz` must return a response
  within 3 seconds even when the database hangs. The database probe is bounded to at most
  2 seconds.
- **NFR-006 [Availability]:** A pprof misconfiguration or bind failure must never prevent the
  main server from starting or serving (FR-005, FR-006).
- **NFR-007 [Compatibility]:** Released agents of every version keep registering, heartbeating
  and pushing telemetry without change (C-1). The `/health` body shape is unchanged (FR-036).
- **NFR-008 [Audit]:** pprof enablement (warning level) and pprof address rejection (error
  level) are logged at startup. The redaction itself must not log secret values.
- **NFR-009 [Testability]:** The health and readiness handlers must be testable without a live
  database. The database-health and migration-state inputs must be injectable, which is
  needed because `Health` currently takes a concrete `*db.DB`. The secret registry, redaction
  and merge logic must be testable as pure functions over channel values.
- **NFR-010 [Maintainability]:** The secret-key registry (FR-018) is the single source of truth
  for redaction, merge and error scrubbing. Adding a new channel type without registering its
  secrets falls under the "unknown type → redact all" rule rather than leaking.

---

## 6. Business Rules

- **BR-001:** Diagnostic surfaces that reveal process internals (profiles, stacks, traces,
  command line) are never public. They are either off or loopback-only.
- **BR-002:** Notification credentials are write-only through the API. The operator can set,
  replace or clear them, but rIOt never hands them back. Losing a credential means
  re-entering it from its source of truth (the notification service).
- **BR-003:** Editing a channel's non-secret fields must never silently wipe its secrets.
- **BR-004:** A health signal must not report healthy while the server cannot serve data. A
  degraded database means a non-2xx status.
- **BR-005:** Documentation claims about reliability features must match shipped behavior
  (I-2).
- **BR-006:** Hardening must not break the device fleet. The agent API is frozen for this
  story (C-1).
- **BR-007:** Defaults are secure. The operator opts in to diagnostics, not out of exposure (C-4).

---

## 7. Data Requirements

### 7.1 Entities

**NotificationChannel** (existing; `id`, `name`, `type`, `enabled`, `config` map). No schema
change. Secret handling is applied at the API boundary and in error text only.

**Table T-1: secret-key registry (minimum)**

| `type` | Secret keys (redacted, write-only) | Non-secret keys (returned as-is) |
|---|---|---|
| `smtp` | `password` | `host`, `port`, `username`, `from`, `to`, `starttls` |
| `ntfy` | `token`, `topic` (D-05) | `server_url`, `priority`, `priority_map` |
| `telegram` | `bot_token` | `chat_id` |
| `discord` | `webhook_url` | none |
| `slack` | `webhook_url` | none |
| `pushover` | `api_token`, `user_key` (D-06) | all other keys |
| `gotify` | `app_token` | `server_url`, all other keys |
| `webhook` | `url`; every **value** in `headers` | `headers` **names** |
| *any other type* | **all** config values | none |

**Health/readiness responses (no persistence).**

| Endpoint | Status code | Body |
|---|---|---|
| `GET /health` | 200 if DB reachable, else 503 | `{"status":"ok"\|"degraded","database":bool,"time":"<RFC3339 UTC>"}`, unchanged shape |
| `GET /readyz` | 200 if DB reachable and migrations complete, else 503 | `{"status":"ready"\|"not_ready","database":bool,"migrations":bool,"time":"<RFC3339 UTC>"}` |

**Configuration (new).**

| Variable | Default | Meaning |
|---|---|---|
| `RIOT_PPROF_ADDR` | empty (disabled) | `<loopback-ip>:<port>` starts the loopback-only pprof listener. Any other value is rejected (FR-005). |
| (optional, FR-007) admin-pprof toggle | off | Only if the architect builds the admin-gated variant. The name is the architect's choice. |

### 7.2 Validation rules

| Rule | Constraint |
|---|---|
| V-001 | Redaction sentinel: a single fixed, non-empty, documented string constant, identical for every secret field. It must not be a plausible real credential or a syntactically valid URL. It is shared between server and frontend. The architect picks the literal. |
| V-002 | `RIOT_PPROF_ADDR` must parse as `host:port`, where host is a literal IPv4 address in `127.0.0.0/8` or the literal IPv6 `::1` (bracketed form `[::1]:port`), and port is 1–65535. Anything else is rejected. |
| V-003 | Registration key match: true if and only if the header value equals the configured key byte-for-byte. The comparison is constant-time with respect to content and length. |
| V-004 | The health DB probe is bounded at ≤ 2 s. A timeout means `database:false`. |
| V-005 | A `PUT` notification-channel body never results in a stored config value equal to the sentinel (FR-024). |

### 7.3 State transitions

**Table T-2: secret field on `PUT` (per secret key, and per webhook header name when `headers` is present)**

| Stored value | Submitted value | Result stored | Response shows |
|---|---|---|---|
| non-empty S | sentinel | S (kept) | sentinel |
| non-empty S | key omitted | S (kept) | sentinel |
| non-empty S | new non-empty N | N (replaced) | sentinel |
| non-empty S | `""` | `""` (cleared) | `""` |
| empty / absent | sentinel | `""` (never the sentinel) | `""` |
| empty / absent | key omitted | `""` | `""` |
| empty / absent | new non-empty N | N | sentinel |
| (webhook) any headers | `headers` key omitted | stored headers unchanged | names + sentinel values |
| (webhook) header H stored | `headers` present without H | H removed | H absent |

**pprof configuration state (evaluated once at startup)**

| `RIOT_PPROF_ADDR` | Loopback listener | Main listener `/debug/pprof/*` | Log |
|---|---|---|---|
| unset / empty | not started | 404 (or 401 if FR-007 variant on and unauthenticated) | none |
| valid loopback `ip:port` | started on that address | 404 (same as above) | warning: enabled on `<addr>` |
| invalid / non-loopback | not started | 404 (same as above) | error: rejected `<value>`, reason |
| valid but bind fails | not started | 404 (same as above) | error: bind failed; server continues |

**Readiness state**

| DB probe | Migrations done | `/health` | `/readyz` |
|---|---|---|---|
| ok | yes | 200 `ok` | 200 `ready` |
| fail / timeout | yes | 503 `degraded` | 503 `not_ready` |
| ok | no (injected; unreachable in today's startup order) | 200 `ok` | 503 `not_ready` |
| fail / timeout | no | 503 `degraded` | 503 `not_ready` |

---

## 8. Acceptance Criteria

All ACs are automated tests (Go `httptest` or unit tests, or vitest) unless marked
**[review]**. "Main router" means the handler returned by `setupRouter` (or an equivalent
test harness that includes the global middleware and the SPA catch-all with a test
`FrontendFS`).

### 8.1 pprof (F-1)

**AC-001: pprof is not served on the main router by default** *(FR-001, FR-002, FR-003, NFR-001)*
- **Given** `RIOT_PPROF_ADDR` is unset and a main router with a non-nil test `FrontendFS`
- **When** unauthenticated `GET` requests are made to `/debug/pprof/`, `/debug/pprof/heap`,
  `/debug/pprof/cmdline` and `/debug/pprof/goroutine?debug=2`
- **Then** each response is HTTP 404
- **And** no body contains pprof output markers (for example `goroutine profile`, `heap profile`
  or the pprof index listing) or the SPA `index.html` content.

**AC-002: no pprof listener starts by default** *(FR-003)*
- **Given** the pprof address configuration is evaluated with `RIOT_PPROF_ADDR` unset, and
  again with it set to the empty string
- **When** the server's pprof setup runs
- **Then** it reports "disabled" and starts no listener.

**AC-003: loopback addresses are accepted** *(FR-004, V-002)*
- **Given** `RIOT_PPROF_ADDR` values `127.0.0.1:6060`, `127.0.0.2:6061` and `[::1]:6060`
- **When** the address is validated
- **Then** each is accepted as an enabled loopback address.

**AC-004: non-loopback and malformed addresses are rejected** *(FR-005, NFR-003, NFR-006, V-002)*
- **Given** `RIOT_PPROF_ADDR` values `:6060`, `0.0.0.0:6060`, `[::]:6060`, `192.168.1.5:6060`,
  `localhost:6060`, `127.0.0.1` (no port), `127.0.0.1:0`, `127.0.0.1:70000` and `garbage`
- **When** the address is validated
- **Then** each is rejected with a reason, pprof stays disabled, an error is logged naming the
  value, and server startup is not aborted.

**AC-005: the loopback listener serves pprof** *(FR-004)*
- **Given** pprof enabled on an accepted loopback address (a test may bind `127.0.0.1:0`)
- **When** `GET /debug/pprof/` and `GET /debug/pprof/heap` are requested on that listener
- **Then** both return HTTP 200 with pprof content.

**AC-006: enabling the loopback listener does not expose pprof on the main router** *(FR-002, NFR-001)*
- **Given** pprof enabled on a loopback address
- **When** unauthenticated `GET /debug/pprof/` is made against the main router
- **Then** the response is HTTP 404 (or 401 if the FR-007 variant is enabled), never 200.

**AC-007: pprof enablement is logged and shuts down cleanly** *(FR-006, NFR-008)*
- **Given** pprof enabled on a loopback address with a captured logger
- **When** the server starts and then shuts down
- **Then** a warning-level record naming the bound address was logged at startup
- **And** after shutdown the address no longer accepts connections.

**AC-008: admin-gated variant (conditional: only if FR-007 is built)** *(FR-007, NFR-001)*
- **Given** the admin-pprof toggle is **off** (default)
- **When** `GET /debug/pprof/` is made with a valid admin session
- **Then** the response is HTTP 404
- **And given** the toggle is **on**, an unauthenticated request returns 401, and a request
  with a valid admin JWT cookie returns 200 with pprof content.

### 8.2 Security headers (F-2)

**AC-009: baseline headers on the SPA** *(FR-008, FR-009, FR-011, FR-012, FR-013)*
- **Given** the main router with a test `FrontendFS` containing `index.html`
- **When** `GET /` is requested over plain HTTP
- **Then** the response carries `Content-Security-Policy`, `X-Frame-Options: DENY`,
  `X-Content-Type-Options: nosniff` and `Referrer-Policy: strict-origin-when-cross-origin`.

**AC-010: headers on every response class** *(FR-008, FR-017)*
- **Given** the main router
- **When** each of the following is requested: `GET /api/v1/devices` unauthenticated (401),
  `GET /api/v1/auth/check`, `POST /api/v1/devices/register` with a wrong key (401), a valid
  agent `POST /api/v1/devices/{id}/heartbeat`, `OPTIONS /api/v1/devices` with an allowed
  `Origin` (204), `GET /health` with the DB down (503), `GET /readyz`, a static asset, and a
  handler that panics (500 through Recoverer)
- **Then** every response carries all four baseline headers from AC-009
- **And** each response's status code and body equal what they were before this story (for
  existing routes).

**AC-011: CSP directive content** *(FR-009, FR-010)*
- **Given** the CSP header value from any response
- **When** it is parsed into directives
- **Then** it contains `default-src 'self'`, `object-src 'none'`, `base-uri 'self'`,
  `frame-ancestors 'none'`, `img-src` including `'self'` and `data:`, `font-src` including
  `'self'`, and `style-src` including `'self'`
- **And** `script-src` contains `'self'` and contains neither `'unsafe-inline'` nor
  `'unsafe-eval'`
- **And** the policy contains no `upgrade-insecure-requests` and no `form-action` directive.

**AC-012: CSP permits same-origin WebSockets** *(FR-009)*
- **Given** the CSP header value
- **When** the `connect-src` directive is inspected
- **Then** it includes `'self'` and permits WebSocket connections to the serving host over
  both `ws:` and `wss:` (the exact expression is the architect's choice. The test asserts that
  a `ws://<host>/ws` and a `wss://<host>/ws/terminal/x/y` URL match the directive under CSP
  source-matching rules).

**AC-013: HSTS on direct TLS** *(FR-014)*
- **Given** a request whose connection is TLS (`r.TLS` non-nil in the test)
- **When** any route is requested
- **Then** `Strict-Transport-Security` equals `max-age=31536000` and contains neither
  `includeSubDomains` nor `preload`.

**AC-014: HSTS behind a trusted proxy** *(FR-014)*
- **Given** `RIOT_TRUSTED_PROXIES=10.0.0.5/32` and a plain-HTTP request from remote address
  `10.0.0.5` with `X-Forwarded-Proto: https`
- **When** any route is requested
- **Then** `Strict-Transport-Security: max-age=31536000` is present
- **And** the same request causes a login response to set the session cookie with `Secure`
  (same predicate).

**AC-015: no HSTS on plain HTTP or from untrusted peers** *(FR-015)*
- **Given** (a) a plain-HTTP request with no forwarding headers, and (b) a plain-HTTP request
  from `203.0.113.9` (not trusted) carrying `X-Forwarded-Proto: https`
- **When** any route is requested
- **Then** neither response carries `Strict-Transport-Security`.

**AC-016: OIDC redirects are unaffected** *(FR-010, FR-017)*
- **Given** OIDC configured against a stub IdP and setup complete
- **When** `GET /api/v1/auth/oidc/start` is requested, and the callback is then exercised with
  a valid stub response
- **Then** the start response is a redirect with the same status code and a `Location`
  pointing at the stub IdP authorization endpoint as before this story, the callback
  redirects to the dashboard and sets the session cookie as before, and both responses carry
  the baseline headers.

**AC-017: WebSocket upgrades still succeed** *(FR-008, FR-017)*
- **Given** an `httptest` server running the full main router middleware chain
- **When** a WebSocket client performs the handshake on `/ws` with a valid admin cookie, and
  on `/ws/agent` with valid agent credentials
- **Then** both handshakes complete with `101 Switching Protocols` and a message can be
  exchanged.

**AC-018: static asset content types are correct under nosniff** *(FR-016)*
- **Given** a test `FrontendFS` containing `index.html`, `assets/app.js`, `assets/app.css`,
  `site.webmanifest`, `favicon.ico` and `assets/font.woff2`
- **When** each is requested
- **Then** each `Content-Type` is respectively an HTML type, a JavaScript type, `text/css`, a
  manifest or JSON type, an image type and a font type, and none is `text/plain` or
  `application/octet-stream`.

### 8.3 Notification-secret redaction (F-3, F-3b)

**AC-019: SMTP password is redacted on list** *(FR-019, FR-022)*
- **Given** a stored `smtp` channel with `password:"hunter2-SECRET"`, `host:"mail.example"`,
  `username:"alerts"`
- **When** `GET /api/v1/settings/notification-channels` is requested by an admin
- **Then** `config.password` equals the sentinel, `host` and `username` are returned verbatim
- **And** the raw response body does not contain `hunter2-SECRET`.

**AC-020: every registry type is redacted** *(FR-018, FR-019, FR-022, T-1)*
- **Given** one stored channel per type in T-1, each with a distinct non-empty canary value in
  every secret key and a distinct value in every non-secret key
- **When** the list endpoint is requested
- **Then** every secret key equals the sentinel, every non-secret key equals its stored value,
  and no canary secret value appears anywhere in the response body.

**AC-021: webhook URL and header values are redacted, names kept** *(FR-021)*
- **Given** a stored `webhook` channel with `url:"https://hook.example/T0KEN"` and
  `headers:{"Authorization":"Bearer abc123","X-Env":"prod"}`
- **When** the list endpoint is requested
- **Then** `url` is the sentinel, `headers` has exactly the keys `Authorization` and `X-Env`,
  each with the sentinel as value, and neither `T0KEN` nor `abc123` appears in the body.

**AC-022: unset secrets are distinguishable from set ones** *(FR-020)*
- **Given** a stored `ntfy` channel with no `token` key, and another with `token:""`
- **When** the list endpoint is requested
- **Then** `token` is returned as `""` (or omitted) for both, never as the sentinel.

**AC-023: unknown channel types are fully redacted** *(FR-018, NFR-010)*
- **Given** a stored channel with `type:"futurething"` and config `{"a":"x1","b":"y2"}`
- **When** the list endpoint is requested
- **Then** both `a` and `b` are the sentinel and neither `x1` nor `y2` appears in the body.

**AC-024: create stores cleartext and returns redacted** *(FR-019, FR-028)*
- **Given** a `POST` of a new `telegram` channel with `bot_token:"123:ABC"`, `chat_id:"42"`
- **When** the request completes
- **Then** the repository received `bot_token:"123:ABC"`, the `201` response shows `bot_token`
  as the sentinel and `chat_id:"42"`, and `123:ABC` is absent from the response body.

**AC-025: PUT with the sentinel keeps the stored secret** *(FR-023, T-2)*
- **Given** a stored `smtp` channel with `password:"P1"`
- **When** a `PUT` changes `name` and sends `password` as the sentinel
- **Then** the repository update receives `password:"P1"` and the new name
- **And** the response shows `password` as the sentinel.

**AC-026: PUT omitting a secret key keeps the stored secret** *(FR-023, T-2)*
- **Given** a stored `discord` channel with `webhook_url:"https://discord.example/W1"`
- **When** a `PUT` sends a config without a `webhook_url` key
- **Then** the stored `webhook_url` remains `https://discord.example/W1`.

**AC-027: PUT with a new value replaces the secret** *(FR-023, T-2)*
- **Given** a stored `gotify` channel with `app_token:"OLD"`
- **When** a `PUT` sends `app_token:"NEW"`
- **Then** the stored `app_token` is `NEW` and the response shows the sentinel.

**AC-028: PUT with an empty string clears the secret** *(FR-023, T-2)*
- **Given** a stored `ntfy` channel with `token:"tk"`
- **When** a `PUT` sends `token:""`
- **Then** the stored `token` is `""` and the response shows `token:""`.

**AC-029: the sentinel is never persisted** *(FR-024, FR-028, V-005)*
- **Given** (a) a stored `smtp` channel with no `password`, then a `PUT` sending `password` as
  the sentinel; (b) a stored `smtp` channel changed by `PUT` to `type:"pushover"` with
  `api_token` as the sentinel; (c) a `POST` create with a secret key set to the sentinel
- **When** each request completes
- **Then** in every case the value written to the repository for that key is `""`, and no
  config value written by any test in this group equals the sentinel.

**AC-030: webhook header merge** *(FR-025, T-2)*
- **Given** a stored `webhook` channel with headers `{"Authorization":"Bearer A","X-Old":"o"}`
- **When** (a) a `PUT` omits `headers` entirely; and separately (b) a `PUT` sends
  `headers:{"Authorization":<sentinel>,"X-New":"n"}`
- **Then** after (a) the stored headers are unchanged
- **And** after (b) the stored headers are exactly `{"Authorization":"Bearer A","X-New":"n"}`
  (`X-Old` removed).

**AC-031: non-secret keys keep replacement semantics** *(FR-026)*
- **Given** a stored `smtp` channel with `host:"h1"`, `from:"a@x"` and `password:"P"`
- **When** a `PUT` sends `host:"h2"`, `password` as the sentinel, and omits `from`
- **Then** the stored config has `host:"h2"`, `password:"P"` and no `from` (as today).

**AC-032: PUT to an unknown channel returns 404** *(FR-027)*
- **Given** no channel with ID 9999
- **When** `PUT /api/v1/settings/notification-channels/9999` is sent
- **Then** the response is 404 and the repository update method is not called.

**AC-033: test-send uses the stored secret after a redacted edit** *(FR-029)*
- **Given** a stored `slack` channel with `webhook_url:"https://slack.example/S1"`, edited by a
  `PUT` that sent `webhook_url` as the sentinel
- **When** `POST /api/v1/settings/notification-channels/{id}/test` is called against a
  dispatcher with a capturing stub backend or HTTP client
- **Then** the stub receives `https://slack.example/S1` as the target.

**AC-034: test-send errors are scrubbed** *(FR-030)*
- **Given** a `telegram` channel with `bot_token:"999:SECRETTOKEN"` whose send fails with an
  error string containing `https://api.telegram.org/bot999:SECRETTOKEN/sendMessage`
- **When** the test endpoint is called
- **Then** the response is `{"success":false,"error":…}` and `error` does not contain
  `999:SECRETTOKEN` (it contains the sentinel in its place).

**AC-035: dispatch failure logs are scrubbed** *(FR-030, NFR-002)*
- **Given** a `discord` channel with `webhook_url:"https://discord.example/api/webhooks/1/TOK"`
  whose send fails with an error containing that URL, and a captured logger
- **When** `Dispatch` and, separately, `SendToChannel` run
- **Then** the `NotificationLog.ErrorMsg` passed to the repository and every captured log
  record's attributes contain neither the URL nor `TOK`.

**AC-036: editor round-trip preserves secrets (frontend)** *(FR-031)*
- **Given** the notification-channel editor rendered (vitest) with a channel whose `password`
  is the sentinel, and a mocked `settingsApi.updateNotificationChannel`
- **When** the user changes only the channel name and saves
- **Then** the mock is called with a config where `password` is the sentinel or absent, and
  never `""`.

**AC-037: editor displays "configured" and sends new values (frontend)** *(FR-031)*
- **Given** the editor with a channel whose `bot_token` is the sentinel
- **When** it renders
- **Then** the sentinel literal is not shown as the input's editable text, and a
  "configured" or "unchanged" indication is visible
- **And when** the user types `NEWTOKEN` and saves, the update mock receives
  `bot_token:"NEWTOKEN"`.

**AC-038: editor can clear an optional secret (frontend)** *(FR-031)*
- **Given** the editor with an `ntfy` channel whose `token` is the sentinel
- **When** the user uses the clear control for `token` and saves
- **Then** the update mock receives `token:""`.

**AC-039: test button needs no secret re-entry (frontend)** *(FR-029, FR-031)*
- **Given** the channel list with a redacted channel
- **When** the user clicks "Test"
- **Then** `settingsApi.testNotificationChannel` is called with the channel ID only, and no
  secret input is required.

**AC-040: demo client returns redacted secrets (frontend)** *(FR-032)*
- **Given** the demo settings client
- **When** its notification-channel list is requested
- **Then** every T-1 secret key in the returned mock channels is the sentinel or empty.

### 8.4 Registration key (F-4)

**AC-041: correct key registers** *(FR-034)*
- **Given** a configured registration key `K-abc123`
- **When** `POST /api/v1/devices/register` is sent with `X-rIOt-Key: K-abc123` and a valid body
- **Then** registration proceeds with the same status and body shape as before this story.

**AC-042: wrong keys are rejected identically** *(FR-033, FR-034, V-003)*
- **Given** a configured registration key `K-abc123`
- **When** requests are sent with `X-rIOt-Key` set to `K-abc124` (same length), `K-abc` (a
  prefix), `K-abc1234` (longer), `""` and the header absent
- **Then** each returns 401 with body `{"error":"invalid registration key"}`.

**AC-043: open registration is unchanged** *(FR-034)*
- **Given** no registration key configured
- **When** a register request is sent with no `X-rIOt-Key`
- **Then** registration proceeds as before this story.

**AC-044: constant-time primitive is used** *(FR-033)* **[review + unit]**
- **Given** the registration-key comparison helper
- **When** its source is reviewed and its truth table is unit-tested (equal → true; every AC-042
  variant → false)
- **Then** it uses `crypto/subtle` (for example `ConstantTimeCompare` over fixed-length
  digests of both values), contains no `==`/`!=` on the key strings, and passes the truth
  table.

### 8.5 Health and readiness (F-5)

**AC-045: healthy `/health`** *(FR-035, FR-036)*
- **Given** an injected DB probe that succeeds
- **When** `GET /health` is requested
- **Then** the status is 200, `Content-Type` is `application/json`, and the body is an object
  with exactly the keys `status`, `database` and `time`, where `status:"ok"`,
  `database:true` and `time` parses as RFC 3339 in UTC.

**AC-046: degraded `/health` returns 503** *(FR-035, FR-036)*
- **Given** an injected DB probe that fails
- **When** `GET /health` is requested
- **Then** the status is 503 and the body has exactly the keys `status`, `database` and
  `time`, with `status:"degraded"` and `database:false`.

**AC-047: ready `/readyz`** *(FR-037)*
- **Given** a succeeding DB probe and migrations marked complete
- **When** `GET /readyz` is requested
- **Then** the status is 200 with body `status:"ready"`, `database:true`, `migrations:true` and
  an RFC 3339 UTC `time`.

**AC-048: `/readyz` returns 503 when the DB is down** *(FR-037)*
- **Given** a failing DB probe and migrations complete
- **When** `GET /readyz` is requested
- **Then** the status is 503 with `status:"not_ready"`, `database:false` and `migrations:true`.

**AC-049: `/readyz` returns 503 when migrations are incomplete** *(FR-037)*
- **Given** a succeeding DB probe and migration state injected as not complete
- **When** `GET /readyz` is requested
- **Then** the status is 503 with `status:"not_ready"`, `database:true` and `migrations:false`.

**AC-050: health endpoints are public and setup-independent** *(FR-038)*
- **Given** the main router, once with setup incomplete and once with setup complete, with no
  session cookie and a test `FrontendFS`
- **When** `GET /health` and `GET /readyz` are requested
- **Then** neither returns 401, 403 or a setup-guard response, and both return
  `application/json` (not the SPA HTML).

**AC-051: a hung DB probe is bounded** *(FR-039, NFR-005, V-004)*
- **Given** an injected DB probe that blocks until its context is cancelled
- **When** `GET /health` and `GET /readyz` are requested
- **Then** each returns 503 with `database:false` within 3 seconds.

**AC-052: health bodies leak nothing** *(FR-040)*
- **Given** an injected DB probe failing with the error
  `dial tcp riot-db:5432: password authentication failed for user "riot"`
- **When** `/health` and `/readyz` are requested
- **Then** neither body contains `riot-db`, `5432`, `password` or any substring of the error,
  and each body has only the keys in FR-036 or FR-037.

**AC-053: agent doctor handles 503** *(FR-041)*
- **Given** an `httptest` server whose `/health` returns 503
- **When** the doctor server check runs against it (with output captured)
- **Then** it prints a warning containing `503`, does not panic, and does not report the
  health check as passed. Against a server returning 200 it reports pass.

### 8.6 Notification retry claim (F-6)

**AC-054: the README makes no false retry claim** *(FR-042, I-2)*
- **Given** the repository README after this story
- **When** an automated text check scans it (for example a Go test reading `README.md`)
- **Then** under **Option B** it contains no case-insensitive match for `retry queue` or
  `retried automatically` in relation to notifications
- **And** under **Option A** the channel types the README names as retried equal exactly the
  set of types the code enqueues.

**AC-055: dead code removed and the rest of the package intact (Option B)** *(FR-043, FR-044)*
- **Given** Option B is chosen
- **When** `go build ./...` and `go test ./...` run
- **Then** no `RetryQueue` symbol exists in the repository, `retry_queue.go` is absent, and
  all remaining `internal/resilient` tests (for example the DNS cache) pass.

**AC-056: failed sends are enqueued and test-sends are not (Option A)** *(FR-045)*
- **Given** Option A, a covered channel whose stub backend fails, and a capturing queue
- **When** `Dispatch` runs, and separately `TestChannel` runs on the same channel
- **Then** exactly one item referencing the channel ID is enqueued by `Dispatch`, and none by
  `TestChannel`.

**AC-057: queue bound and attempt cap (Option A)** *(FR-046, FR-047)*
- **Given** Option A with maximum size 100
- **When** 101 failures are enqueued, and separately one item fails on every retry
- **Then** the queue length is 100 with the oldest item dropped and the drop logged
- **And** the always-failing item is dropped with a logged reason once the documented attempt
  or age bound is reached.

**AC-058: a hung endpoint does not block enqueue (Option A)** *(FR-048)*
- **Given** Option A, a retry pass in progress whose send blocks indefinitely
- **When** a new failure is enqueued
- **Then** `Enqueue` returns within 100 ms.

**AC-059: clean shutdown with no goroutine leak (Option A)** *(FR-049)*
- **Given** Option A with the retry worker running and a send blocked
- **When** the worker's context is cancelled (server shutdown)
- **Then** the worker returns within 10 seconds and the goroutine count returns to its
  pre-start baseline (checked with a goroutine-leak assertion).

**AC-060: no secrets at rest in retry state (Option A)** *(FR-050)*
- **Given** Option A with persistence enabled and a failed `discord` item whose channel
  `webhook_url` contains `TOK`
- **When** the persisted retry file is read
- **Then** it contains the channel ID and payload but not `TOK` or the webhook URL.

**AC-061: deleted or disabled channels are dropped and outcomes are logged (Option A)** *(FR-051, FR-052)*
- **Given** Option A with queued items for (a) a since-deleted channel, (b) a since-disabled
  channel and (c) a channel whose retry now succeeds
- **When** a retry pass runs
- **Then** (a) and (b) are dropped with no send attempt, and (c) is sent and recorded in
  `notification_log` with status `sent`.

### 8.7 Cross-cutting

**AC-062: agent API contract unchanged** *(FR-053, C-1, NFR-007)*
- **Given** the existing Go tests covering `/api/v1/devices/register`,
  `/api/v1/devices/{id}/heartbeat`, `/telemetry`, `/events`, `/logs`, `/probe-results`,
  `/api/v1/update/check` and `DeviceAuth`
- **When** `go test ./...` runs after this story
- **Then** all pass without changes to their request fixtures or expected status codes and
  bodies (header additions excepted).

**AC-063: full suites green** *(C-2)*
- **Given** the implemented story
- **When** `go build ./...`, `go vet ./...`, `go test ./...` and `cd web && npm run test:run`
  run
- **Then** all succeed with zero failures.

**AC-064: documentation updated** *(FR-054)* **[review + automated text check]**
- **Given** the README and CHANGELOG after this story
- **When** they are checked
- **Then** the README documents `RIOT_PPROF_ADDR` (default off, loopback-only, `docker exec`
  access), `/readyz` and the 503 behavior of `/health` (API table), write-only notification
  secrets, the iframe-embedding denial and HSTS behind a trusted proxy
- **And** CHANGELOG has an entry for each behavior change.

**Total: 64 ACs** (AC-008 is conditional on FR-007. AC-055 applies only under Option B.
AC-056 to AC-061 apply only under Option A. Exactly one of the two option sets applies.)

### 8.8 Post-deploy verification (not ACs; for the orchestrator after release)

These need a real browser or the live host and so are not unit-testable. They are listed so
the deploy step does not skip them:
- On the deployed server: `curl -i https://<server>:7331/debug/pprof/` returns 404. `curl -I` on `/` shows
  the new headers.
- Load the dashboard in a browser with devtools open. There are zero CSP violations on login,
  device page, container terminal (xterm over `/ws/terminal`), settings and charts. An OIDC
  login round-trip completes.
- `GET /health` returns 200 and `GET /readyz` returns 200 on the deployed server.
- The notification settings page shows channels as "configured", and test-send succeeds.

---

## 9. Out of Scope

1. **pg_dump backup sidecar for `riot-db` and its restore runbook.** This is a related ops
   deliverable, executed separately by the orchestrator (see §12.1). This story makes
   no code change for it.
2. Encrypting notification secrets at rest in Postgres. They remain plaintext in the DB, which
   is a residual risk that the backup work must also account for: dumps contain these secrets.
3. Rotating credentials that may already have been exposed through F-3 or F-3b. Whether to
   rotate is the operator's operational decision (OQ-9).
4. The registration-key settings endpoint (`GET /api/v1/settings/registration`) returning the
   key in cleartext to admins. It is adjacent to F-3 but is a separate decision (OQ-5).
5. CSRF protection, session hardening, or changes to rate limiting (including rate limiting
   `/health` and `/readyz`).
6. CSP nonces or hashes to remove `style-src 'unsafe-inline'`, CSP violation reporting
   endpoints, `Permissions-Policy`, `Cross-Origin-Opener-Policy`/`-Embedder-Policy`.
7. A configurable `frame-ancestors` allowlist, for example to embed rIOt in a Home Assistant
   iframe panel (OQ-2).
8. A separate liveness endpoint (`/livez`).
9. An audit of every other secret comparison in the codebase beyond `RegisterDevice` (OQ-8).
10. Changing the retry queue into a general-purpose delivery guarantee for SMTP or Telegram
    beyond what Option A's covered types specify.
11. Changing the default deployment to bind `127.0.0.1` or putting rIOt behind a proxy. That is
    a deployment ops decision.
12. Database schema changes and agent changes (doctor only gains test coverage, FR-041).

---

## 10. Assumptions / Decisions

No human was available for a clarification interview. Each item below is a decision this FRD
made on the operator's behalf using the safest sensible default, for the maintainer's review. If the operator disagrees
with any of them, the FRD changes. The architect must not silently choose otherwise.

| # | Decision | Rationale | Reversal cost |
|---|---|---|---|
| D-01 | pprof is off by default and opt-in only through `RIOT_PPROF_ADDR` on a loopback-only listener. The admin-gated main-router variant (FR-007) is optional and not expected. | Satisfies C-4 and invariant I-1 with the smallest attack surface. | Low. FR-007 can be added later. |
| D-02 | Only literal loopback IPs are accepted for `RIOT_PPROF_ADDR`. Hostnames, including `localhost`, are rejected. | Prevents widening the bind through DNS or hosts-file changes. | Trivial to relax. |
| D-03 | When containerized, pprof is reached with `docker exec` into the container. A Docker port mapping cannot reach the container's loopback, and this is intended. | Security posture. Documented for the operator's memory-profiling workflow. | None. |
| D-04 | The main router returns an explicit 404 for `/debug/pprof*` rather than falling through to the SPA. | Makes the closure observable to scanners and tests (§2.2 item 1). | None. |
| D-05 | The ntfy `topic` is classified as **secret**. | On public ntfy servers the topic name works as a password: anyone who knows it can subscribe. Cost: the UI shows "configured" instead of the topic name. | Low. Move it to non-secret in T-1. |
| D-06 | The Pushover `user_key` is classified as **secret** (along with `api_token`). | Safest default. The key identifies the alert recipient. | Low. |
| D-07 | CSP `style-src` permits `'unsafe-inline'`. `script-src` stays strict (`'self'` only). | xterm.js injects `<style>` elements and runtime-styled components need it. Script containment is where CSP adds most of its value. | Medium. Needs nonces or hashes (out of scope). |
| D-08 | The CSP omits `form-action`. | Browsers enforce `form-action` across redirects, which could block the OIDC redirect to the IdP if any SSO entry point is a form submission. | Low. Can be added once the SSO entry is confirmed non-form. |
| D-09 | `Referrer-Policy: strict-origin-when-cross-origin` (not `no-referrer`). | Under `no-referrer`, browsers serialize `Origin: null` on some same-origin form navigations, which risks breaking any origin checks. The chosen value still strips paths cross-origin. | Trivial. |
| D-10 | HSTS `max-age=31536000` with no `includeSubDomains` and no `preload`. Sent only on HTTPS, judged by the same predicate as the `Secure` cookie. | Homelab sibling subdomains may still be plain HTTP. Preload is irreversible. | Low. |
| D-11 | The registration-key comparison must also not leak the key's length, for example by comparing fixed-length digests. | `subtle.ConstantTimeCompare` returns early on a length mismatch. Digest comparison closes that at negligible cost. | None. |
| D-12 | The redaction sentinel is one fixed string for all fields. "Configured" is signalled by sentinel versus empty, so no new per-field flag is added to the API. | Smallest contract change. The frontend needs only one constant. | Low. |
| D-13 | On `PUT`: sentinel keeps, omitted keeps, `""` clears, a new value replaces. Webhook `headers`: omitted keeps all, present means the full set with per-name sentinel resolution. | Satisfies "editing without retyping must not wipe" while still allowing a deliberate clear. | Low. |
| D-14 | A operator-chosen password equal to the sentinel literal would be read as "keep". This is accepted. | Not a plausible real value (V-001). | None. |
| D-15 | Scrubbing secrets from error text (F-3b, FR-030) is **in scope**. | It leaks the same secrets as F-3 through `notification_log`, the server log viewer and test-send responses. Closing F-3 without it would leave the exposure open. | Could be split into a follow-up story if the architect judges it too large. |
| D-16 | `/health` keeps its exact body shape and only its status code changes, 200 to 503 when degraded. `/readyz` is a new, separate endpoint with its own shape (`migrations` field added). | Backward compatibility (FR-036). The known body consumer (doctor) reads only the status code. | None. |
| D-17 | "Migrations complete" is part of readiness and is represented explicitly, even though today's startup order makes it always true while serving. | The story asks for it "if knowable". It is knowable, and modelling it guards future refactors. | None. |
| D-18 | The DB probe for health is bounded at 2 s and the response at 3 s. | Keeps orchestrator probes (typically 5 s timeouts) from hanging. | Trivial. |
| D-19 | Retry queue: the FRD default and BA recommendation is **Option B (remove the claim and delete `retry_queue.go`)**. The architect makes the final call under FR-042. | The code is memory-bounded but not sound for wiring as-is (§2.2 item 5). Making the README true is the requirement, and removal achieves it with no new failure modes. | Medium. Option A is a contained follow-up. |
| D-20 | Under Option B only `retry_queue.go` (and its dedicated test) is deleted, not the `internal/resilient` package. | The package likely hosts the agent DNS cache (README "DNS Resilience"). | None. |
| D-21 | Security headers apply to agent API responses too. | Browsers ignore them on non-document responses and agents ignore unknown headers, so there is no risk to C-1, and a single global middleware is simpler to verify. | None. |
| D-22 | The `101 Switching Protocols` response of a WebSocket upgrade is exempt from the header requirement. | The WebSocket library writes its own handshake response. Headers on a 101 have no security effect. | None. |
| D-23 | Iframe embedding of the dashboard is denied by default (`frame-ancestors 'none'` plus `X-Frame-Options: DENY`). | Clickjacking protection for an admin console with remote-command capability. | Low. A configurable allowlist is a follow-up (OQ-2). |

---

## 11. Open Questions

Each question carries the answer this FRD assumed (§10). They are non-blocking unless marked.

- **OQ-1: Retry queue, Option A or Option B?** *Assumed:* Option B (D-19). The architect may
  choose A only by meeting FR-045 to FR-052. The operator may override.
- **OQ-2: Does the operator embed the rIOt dashboard in an iframe anywhere (for example a Home
  Assistant panel)?** *Assumed:* no. Framing is denied (D-23). If yes, a follow-up story adds
  a `RIOT_FRAME_ANCESTORS` allowlist. Until then that embed will stop rendering after deploy.
- **OQ-3: Should the ntfy `topic` really be hidden in the UI?** *Assumed:* yes (D-05).
- **OQ-4: Does the production riot-server compose file define a healthcheck against
  `/health`, and is any auto-restart tool (for example autoheal) watching it?** *Assumed:*
  unknown. **Must be checked by the orchestrator before deploy.** With this story `/health`
  returns 503 during a DB outage. A healthcheck plus autoheal would then restart riot-server
  in a loop while riot-db is down. If such a check exists, the orchestrator decides whether to
  keep it on `/health` or move it to `/readyz`. Either way a DB outage will mark the container
  unhealthy, which is the intended signal.
- **OQ-5: Should the registration key also become write-only
  (`GET /api/v1/settings/registration` returns it in cleartext)?** *Assumed:* out of scope for
  this story. Recommend a follow-up.
- **OQ-6: Build the admin-gated pprof variant (FR-007)?** *Assumed:* no (D-01).
- **OQ-7: Add CSP `form-action 'self'`?** *Assumed:* omitted (D-08) until the SSO entry point
  is confirmed not to be a form submission.
- **OQ-8: Audit other request-secret comparisons (bootstrap keys, device keys, enrollment) for
  timing safety?** *Assumed:* out of scope. The architect should list any `==`/`!=` secret
  comparisons found during design as follow-up items.
- **OQ-9: Should the operator rotate notification-service credentials (SMTP password, bot tokens,
  webhook URLs) after deploy?** *Assumed:* the operator's call. The exposure required an admin
  session (F-3) or access to the admin log views (F-3b). pprof (F-1) exposes argv and stacks,
  not memory contents, so it is unlikely to have leaked these secrets.

---

## 12. Dependencies

| Dependency | Nature | Status |
|---|---|---|
| `internal/server/router.go` | Remove the pprof mount (FR-001/002), add the security-headers middleware (FR-008), register `/readyz` before the SPA catch-all (FR-038). | Present. Change required. |
| `internal/server/server.go` | Optional loopback pprof listener lifecycle (FR-004 to FR-006). Readiness migration flag (FR-037). Shutdown wiring. | Present. Change required. |
| `internal/server/config.go` (or wherever env config is parsed) | `RIOT_PPROF_ADDR` parsing and validation (V-002). | Present. Change required. |
| Existing trusted-proxy HTTPS predicate (OIDC-001, `RIOT_TRUSTED_PROXIES`) | HSTS must reuse it (FR-014). | Present. Reuse. |
| `internal/server/handlers/handlers.go` | `Health` (503, injectable probe), new `/readyz` handler, `RegisterDevice` constant-time compare. | Present. Change required. |
| `internal/server/handlers/settings.go` | Redaction on list, create and update responses. Merge on `PUT`. 404 on unknown ID. Scrubbed test-send error. | Present. Change required. |
| `internal/server/notify/` (`dispatcher.go`, backends) | Secret registry location (architect's choice). Error scrubbing in `Dispatch`/`SendToChannel`/`TestChannel`. Option A wiring, if chosen. | Present. Change required. |
| `internal/server/middleware/` | New security-headers middleware. Setup-guard exemption for `/readyz` if needed. | Present. Change required. |
| `internal/resilient/retry_queue.go` | Deleted (Option B) or wired (Option A). | Present. Change required. |
| `internal/agent/doctor.go` `checkServer` | Consumer of `/health` (status code only). Gains test coverage (AC-053). No behavior change. | Present. Test only. |
| Frontend notification-channel editor (component under `web/src/`, exact file to be located by the architect), `web/src/api/settings.ts`, `web/src/api/demo-settings*`, `web/src/types/models.ts` | Sentinel handling, "configured" state, clear control (FR-031/032). | Present. Change required. |
| `README.md`, `CHANGELOG` | FR-043/FR-054 documentation changes. | Present. Change required. |
| Production compose for riot-server | Healthcheck semantics review before deploy (OQ-4). | **Orchestrator to verify.** |
| Release process (tag every code push) | A tagged release is required to ship. Deploy follows the operator's own runbook. | Present. |

### 12.1 Related non-code deliverable (handled separately; not part of this story)

**riot-db backup and restore (ops).** The second half of the story title ("the backup gap") is
delivered as deployment ops work by the orchestrator, not by this code story. It consists of a
pg_dump backup sidecar for `riot-db`, with scheduled dumps, retention and a
storage location off the DB volume, plus a written and tested restore runbook in the operator's private
documentation. Two points from this FRD feed into that work:
1. Notification secrets remain plaintext in Postgres (§9 item 2), so the dumps are sensitive
   and need permissions and storage treated accordingly.
2. `/readyz` (FR-037) is the natural post-restore verification signal.
