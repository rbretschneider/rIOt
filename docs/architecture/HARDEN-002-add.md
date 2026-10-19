# Architecture Decision Document

| Field         | Value                                   |
|---------------|-----------------------------------------|
| Story ID      | HARDEN-002                              |
| FRD Reference | docs/requirements/HARDEN-002-frd.md     |
| Author        | Architect Agent                         |
| Date          | 2026-10-17                              |
| Revision      | 2                                       |
| Status        | FINAL                                   |

---

## Amendments (security review)

Source: `docs/security/HARDEN-002-security-review.md`, verdict APPROVE WITH CONDITIONS.
Each condition is folded into the sections listed below. This is an in-place delta and needs
no re-review.

| Rev 2 item | Review ref | What changed | Where |
|---|---|---|---|
| C1 | SEC-001a (HIGH) | Credentials in URL userinfo are now **write-only** in any URL-valued config key. That includes the non-secret ntfy and gotify `server_url`. Responses rewrite the userinfo as `scheme://[rIOt-redacted]@host…`. A PUT that sends that form back keeps the stored userinfo. The sentinel is never persisted in userinfo either. | AD-006, AD-007, §6.5, §8 (AC-019, AC-024, AC-025, AC-029) |
| C2 | SEC-001b (HIGH) | `ScrubSecrets` now also matches net/http's `stripPassword` form (`user:***@`) and the `%q`-escaped interior form used by `url.Error`. The username and password from the userinfo of **any** config URL join the secret set. Tests drive real `http.Client` failures through an erroring `RoundTripper` for telegram, discord, slack, ntfy, gotify and webhook, including a URL with an embedded password. | AD-008, §4, §8 (AC-034, AC-035) |
| C3 | SEC-003 (MED) | Secret inputs opt out of browser credential autofill and save prompts: `autoComplete="new-password"` or `"off"`, password-manager ignore attributes, and a unique non-credential `name`. The SMTP username input gets `autoComplete="off"`. A vitest attribute test and a manual post-deploy check cover it. | AD-009, §8 (AC-037), §13 |
| C4 | SEC-002 (MED) | §10 and the README now state the limits of the write-only design. It does not protect against an actor who can edit channels (destination rebinding), and it does not protect secrets at rest. Destination binding is follow-up F-7. | §10, §12.3, §12.4 |
| C5 | SEC-007 + pprof | New router test over pprof path variants. New automated and REVIEW gate: no `http.Server` with a nil `Handler`, and nothing serves `DefaultServeMux`. Extra parser cases. The port must be canonical decimal, and the bound address is built only from parsed values. | AD-001, AD-002, §4, §8 (AC-001, AC-003, AC-004), §13 |
| C6 | SEC-005 | New CSP Host-injection table: `; , ' * ( ) ! $ & + = %` and a 263-byte host must fall back, and a 262-byte host must be accepted. | AD-004, §8 (AC-012) |
| SEC-008 | SEC-008 (LOW) | **`form-action 'self'` is added to the CSP now.** The SSO entry is an `<a href>` outside the login form (`Login.tsx:172-175`, pinned by `Login.test.tsx:73`). This overrides FR-010's "no form-action" clause and D-08, on orchestrator instruction. F-2 is closed. The AC-011 test assertion is inverted. | §0, AD-004, §6.1, §8 (AC-011), §12.4 |
| C7 | C7 / SEC-004 / P-1 | Deployment preconditions are recorded generically (§12.5). The README HSTS note now says HSTS covers every port and makes certificate errors non-bypassable. P-1 is a follow-up with **elevated** priority and stays out of this story's scope. | §12.3, §12.4, §12.5 |

---

## 0. FRD review outcome and recorded decisions

No human was available. The orchestrator resolved the FRD open questions, and the architect
treats those answers as binding:

- **OQ-1 (retry queue): Option B.**
  - Remove the README claims: the Features bullet at L37 and the "Notification Retry Queue"
    subsection at L996–998.
  - Delete only `internal/resilient/retry_queue.go` and `internal/resilient/retry_queue_test.go`
    (D-20).
  - AC-056 to AC-061 are **N/A**. See AD-012.
- **OQ-2 (iframe):** no known deployment frames the dashboard; dashboards link to it.
  `frame-ancestors 'none'` and `X-Frame-Options: DENY` stand.
- **OQ-3 (ntfy topic):** treated as a secret. D-05 stands.
- **OQ-4 (healthcheck / autoheal):** the shipped Dockerfile has no HEALTHCHECK and the shipped
  compose only health-checks the database. Operators who point a healthcheck or autoheal at
  `/health` should expect a 503 (and possible restarts) while the DB is down; the README says so.
- **OQ-5 (registration key returned in cleartext by GET):** out of scope. Follow-up F-1 (§12.4).
- **OQ-6 (admin-gated pprof): not built.** pprof is reachable only on the env-gated loopback
  listener. FR-007 is not offered, so FR-002 applies unconditionally. AC-008 is **N/A**.
- **OQ-7:** superseded in rev 2.
  - After security review SEC-008, the orchestrator instructed that the CSP include
    `form-action 'self'`. This overrides FR-010's "must not contain a `form-action`
    directive" clause and D-08.
  - D-08's only concern was an SSO entry point that submits a form. The code already resolves
    it: the SSO button is an `<a href="/api/v1/auth/oidc/start">` placed outside the login
    `<form>` (`web/src/pages/Login.tsx:172-175`, pinned by `Login.test.tsx:73`).
  - Every form in the SPA posts same-origin.
- **OQ-8:** out of scope and logged as a follow-up (§12.4). The audit result is in §10.
- **FR-030 (error-text scrubbing):** in scope, kept minimal.
  - One helper, `notify.ScrubSecrets`, runs at one choke point in the dispatcher (AD-008).
  - That covers the log lines, `notification_log.error_msg`, the test-send response and the
    `SendToChannel` return value.
- **Direct TLS:** riot-server can terminate TLS itself (no proxy). The HSTS predicate covers
  `r.TLS != nil` (AD-003). See §12.5 for deployment preconditions.

### Technical conflicts found in the FRD and how this ADD resolves them

| # | Conflict | Resolution |
|---|---|---|
| T-1 | AC-004 requires `127.0.0.1:0` to be **rejected**. AC-005 says "a test may bind `127.0.0.1:0`". | AD-002 splits validation (`ParsePprofAddr`, which rejects port 0) from binding (`listenPprof`, which takes an already-validated address). AC-005 and AC-007 call `listenPprof("127.0.0.1:0", …)` directly. |
| T-2 | AC-010 and AC-017 ask for register (wrong key), heartbeat, auth-check, login and the WebSocket routes on the "main router". `setupRouter` cannot serve those without a live DB, because `Server` holds concrete repo types (`*db.DeviceRepo`, `*db.AdminRepo`, …). A nil `*db.AdminRepo` panics inside `RegisterDevice`, and Recoverer turns that into a 500. | AD-013: the global middleware chain becomes one function, `(*Server).globalMiddleware()`. Both `setupRouter` and a test harness with mock-backed handlers use it. The FRD allows "an equivalent test harness that includes the global middleware". Routes that do not need a DB are tested on the real `setupRouter`. |
| T-3 | AC-016 needs the OIDC stub IdP. It lives in `handlers` test files, which package `server` cannot import. | AC-016 is tested in package `handlers`. The existing OIDC routes are wrapped in the same `middleware.RealIP` and `middleware.SecurityHeaders`, using the existing `newStubIDP` and `newOIDCTestSetup` helpers. |
| T-4 | FR-023 ("resolve against the stored channel") does not say what happens when the channel `type` changes. FR-024 and AC-029(b) cover only the sentinel case. | AD-007: **a type change inherits nothing.** The stored config is treated as empty for the merge. This matches the editor, which already resets `config` to `{}` on a type change. |
| T-5 | V-005 says no stored config value may equal the sentinel. FR-023 only defines the merge for secret keys. | AD-007 adds a final sweep over all values. Rev 2 extends the same guarantee to URL userinfo (C1). |
| T-6 | AC-062 cites "existing Go tests covering `/api/v1/devices/register`". None exist. | AC-041 to AC-043 add them. AC-062 is satisfied by the existing agent-route suites passing unmodified. |
| T-7 | Test-name collision: package `handlers` already has `TestAC001_…` to `TestAC023_…` from OIDC-001. | AD-014: new Go tests are named `TestHARDEN002_AC0NN_<Desc>`. Tests for a security-review condition add the condition id, as in `TestHARDEN002_AC019_C1_…`. Vitest titles start with `[HARDEN-002][AC-0NN]`. |

### Out-of-scope finding (flagged, not fixed)

- `RIOT_API_KEY` populates `Config.RegistrationKey` (`config.go:62`).
- `RegisterDevice`, however, reads only the DB key `admin_config.registration_key`
  (`handlers.go:157`).
- README L139 says "if set, agents must present this key to register". If only the env var is
  set, registration is open.
- FR-034 freezes `RegisterDevice` behavior for this story, so this is follow-up **F-3**
  (§12.4).

---

## 1. Summary

This story makes six minimal, independently testable changes. There is no new dependency, no
migration and no agent API change.

1. **pprof.** The unconditional pprof mount comes off the main chi router and becomes an
   explicit 404. An opt-in pprof listener, which accepts only a literal loopback IP and
   canonical port, is controlled by `RIOT_PPROF_ADDR`.
2. **Security headers.** A global middleware that does not wrap the ResponseWriter is placed
   right after `RealIP`. It sets:
   - `Content-Security-Policy`, now including `form-action 'self'`
   - `X-Frame-Options`
   - `X-Content-Type-Options: nosniff`
   - `Referrer-Policy`
   - HSTS when `middleware.IsHTTPS(r)` is true. The same predicate now sets the session
     cookie's `Secure` flag.
3. **Notification secrets.** A secret-key registry in `internal/server/notify/secrets.go` is
   the single source of truth for:
   - redacting every channel response, including credentials in URL userinfo in any config
     URL
   - write-only merge on PUT
   - scrubbing error text at the dispatcher's send choke point. The scrubber matches the exact
     serializations net/http emits.

   A small `SecretInput` component teaches the React editor the sentinel contract and opts the
   secret fields out of browser autofill.
4. **Registration key.** Compared in constant time over SHA-256 digests.
5. **Health.** `/health` returns 503 when degraded, and a new `/readyz` endpoint is added. An
   injectable, time-bounded `HealthHandler` serves both.
6. **Retry queue.** The dead code and its README claim are removed (Option B).

---

## 2. Technical Context

### 2.1 What exists (verified at `a7b9f9e`)

**Router (`internal/server/router.go`)**
- The global chain is `middleware.RealIP(trustedProxies)` → `middleware.Logger` →
  `chimw.Recoverer` → `middleware.CORS` → `middleware.SetupGuard` (L37–44).
- `r.Mount("/debug/pprof", pprofHandler())` is unauthenticated, and its comment is false
  (L203–205). `pprofHandler()` is at L14–25.
- `/health` is wired as `h.Health(s.DB)` (L104).
- `serveFrontend` (L308–324) answers every unmatched GET with `index.html` and 200, through
  `http.FileServer`.

**Server (`internal/server/server.go`)**
- `Start` runs migrations before building the router (L88–93).
- The listener binds `:<port>` (L198) and shuts down with a 10 s deadline (L252).
- `applyTLSAndRestart` rebuilds the router through `setupRouter` (L381).
- Both `http.Server` literals set `Handler` (L197, L398).
- The tree has no `http.ListenAndServe`, `http.Serve` or `DefaultServeMux` usage (grep).

**Config (`internal/server/config.go`)**
- `LoadConfig` parses env before `slog` is configured. Validation that must log therefore
  belongs in `Start`.

**Middleware**
- `clientip.go` has `RequestScheme(r)`. It returns a trusted `X-Forwarded-Proto`, else
  `https` when `r.TLS != nil`, else `http`.
- The cookie `Secure` flag is `middleware.RequestScheme(r) == "https"` at
  `handlers/auth.go:44,60` and `handlers/oidc.go:102,119`.
- `setup.go` allows `/health` explicitly.
- `logger.go` wraps the writer and forwards `Hijack`.
- `cors.go` answers every `OPTIONS` with 204 itself.

**Handlers**
- `Health(database *db.DB)` returns 200 on every path (`handlers.go:140`).
- `RegisterDevice` compares `apiKey != regKey` (`handlers.go:160`) and on mismatch writes
  `http.Error(w, '{"error":"invalid registration key"}', 401)`.
- `writeJSON` sets `Content-Type: application/json`.

**Settings (`handlers/settings.go`)**
- List, Create and Update serialize `config` verbatim.
- Update never loads the stored row. A missing ID is a silent no-op that returns 200.
- `TestNotificationChannel` returns `err.Error()`, and maps any `GetChannel` error to 404
  `{"error":"channel not found"}`.

**Notify (`internal/server/notify/`)**
- `Dispatcher` has a `backends` factory map. `Dispatch`, `SendToChannel` and `TestChannel`
  pass `err.Error()` raw into `NotificationLog.ErrorMsg` and `slog.Error`.
- `server_error_alert_worker.go:103` logs the error returned by `SendToChannel`.
- Config keys each backend reads:

  | Backend | Keys read |
  |---|---|
  | smtp | `host`, `port` (float64), `username`, `password`, `from`, `to`, `starttls` (bool) |
  | ntfy | `server_url`, `topic`, `token`, `priority`, `priority_map` (map) |
  | telegram | `bot_token` (goes in the URL path), `chat_id` |
  | discord, slack | `webhook_url` |
  | pushover | `api_token`, `user_key` |
  | gotify | `server_url`, `app_token` (goes raw in the query string) |
  | webhook | `url`, `headers` (`map[string]interface{}`) |

- Go's `http.Client` errors are `*url.Error`, formatted with `"%s %q: %s"` over
  `stripPassword(req.URL)`. `stripPassword` rewrites the password as `***`. Go also turns URL
  userinfo into a Basic `Authorization` header.

**Model**
- `models.NotificationChannel{ID, Name, Type, Enabled, Config map[string]interface{}, CreatedAt, UpdatedAt}`.

**Test infrastructure**
- `testutil.MockNotifyRepo.GetChannel` returns a pointer into its slice, so maps are shared.
  It returns `fmt.Errorf("not found")` for a missing ID.
- `CreateChannel` and `UpdateChannel` store shallow copies.
- There is a log-capture helper pattern: `captureLogs` in `oidc_handler_test.go`.

**Frontend**
- `web/index.html` has no inline script or style.
- The Vite 6 + `@vitejs/plugin-react` production build emits external `/assets/*` files only.
  Assets under 4 KB may be inlined as `data:` URIs.
- xterm.js 6 injects `<style>`. Recharts uses SVG style attributes.
- There is no `eval`, no Web Workers, and no external origin is loaded.
- WebSockets are built as `${ws:|wss:}//${window.location.host}/ws` and `…/ws/terminal/…`.
- `blob:` is used only as an `<a download>` target, which is a navigation.
- The login form sets `autoComplete="on"` (`Login.tsx:115`), so browsers save admin
  credentials for this origin.
- The SSO entry is an anchor outside the form (`Login.tsx:172-175`).

**Editor (`web/src/pages/settings/NotificationSettings.tsx`)**
- Secret fields are plain `value={(config.x as string) || ''}` inputs.
- Changing the type resets `config` to `{}`.
- List rows print the topic and the URL values.
- There is no headers editor, and the editor is not wrapped in a `<form>`.
- There is no existing test file for this page.

**Demo**
- In demo builds only, a Vite alias swaps `web/src/api/demo-settings.ts` in for
  `api/settings`.
- The mock channels in `demo-data.ts` include an ntfy channel with a plaintext topic and a
  stray `url` key.

**Agent doctor**
- `checkServer` (`internal/agent/doctor.go:426`) prints `Health endpoint returned 200`
  (pass) or `Health endpoint returned %d` (warn). Its output is captured with
  `captureStdout`.

**Deployment files**
- The `Dockerfile` has no `HEALTHCHECK`. The runtime image is `alpine:3.19`, which includes
  BusyBox `wget`.

**Retry queue**
- `internal/resilient/retry_queue.go` and its test are self-contained. `dns_cache.go` and
  `http_client.go` are independent of them.

### 2.2 What is missing

- An explicit 404 for `/debug/pprof*` and a loopback-only listener.
- All security response headers.
- A secret registry, redaction (including URL userinfo), merge, scrubbing, and a
  sentinel-aware editor that is safe from browser autofill.
- Constant-time key comparison.
- An injectable, bounded health probe; 503 semantics; `/readyz`; a migrations flag.
- Deterministic `Content-Type` for embedded assets. Go's built-in MIME table lacks `.ico`,
  `.woff2`, `.woff`, `.ttf` and `.webmanifest`, the Windows registry can override `.js`, and
  content sniffing turns `.webmanifest` into `text/plain`.

---

## 3. Architecture Decisions

### AD-001: Take pprof off the main router, return an explicit 404, serve it only on a loopback listener, no admin variant

**Decision**
- Delete `r.Mount("/debug/pprof", pprofHandler())`, its comment, and the `net/http/pprof`
  import from `router.go`.
- Register `r.Handle("/debug/pprof", pprofNotFound)` and
  `r.Handle("/debug/pprof/*", pprofNotFound)` on the main router, before `serveFrontend`.
  `pprofNotFound` writes `http.Error(w, '{"error":"not found"}', 404)`.
- Move `pprofHandler()` unchanged into the new `internal/server/pprof.go`. It is served only
  from a separate `*http.Server` bound to the parsed `RIOT_PPROF_ADDR` (AD-002).
- FR-007 (the admin-gated variant) is not built (OQ-6).
- **C5(a):** non-canonical path variants are not 404, because chi does not clean paths. They
  fall through to the SPA and return `index.html`. That is safe, because the main mux has no
  pprof handler. AC-001 adds a variant test asserting that no pprof content is ever returned,
  whatever the status code.

**Rationale**
- Removing the mount alone would fall through to the SPA catch-all, which returns 200 with
  HTML. The explicit route makes the closure visible to scanners and tests.
- A separate listener makes invariant I-1 true by construction.

**Alternatives considered**
- Admin-gated pprof: rejected by OQ-6.
- A `/debug/` prefix 404: too broad.
- A loopback `RemoteAddr` check on the main router: fragile, because `RealIP` rewrites
  `RemoteAddr` and the Docker bridge gateway is not loopback.
- Installing `chimw.CleanPath` so variants also 404: rejected. It changes routing for every
  route and adds no security.

**Consequences**
- Operator notes that say "pprof at /debug/pprof" become stale. The README documents
  `docker exec riot-server wget -qO- http://127.0.0.1:6060/debug/pprof/heap > heap.pb.gz`.
- The `net/http/pprof` import still registers handlers on `http.DefaultServeMux`. That is
  harmless only while nothing serves `DefaultServeMux`, so it is now pinned by an automated
  invariant test and a REVIEW gate (C5(b), §13).

### AD-002: Parse `RIOT_PPROF_ADDR` purely, bind separately, build the bind address only from parsed values, never let pprof abort startup

**Decision**
- `Config.PprofAddr string` is set to `strings.TrimSpace(os.Getenv("RIOT_PPROF_ADDR"))`.
  `LoadConfig` does no validation.
- `internal/server/pprof.go` defines `ParsePprofAddr(raw string) (addr string, err error)` as
  a pure function:
  - `""` returns `("", nil)`, which means disabled.
  - Otherwise `net.SplitHostPort(raw)` splits it into `host` and `portStr`.
  - **Host rules:**
    - It must parse with `netip.ParseAddr`, so hostnames, `127.1` and `0177.0.0.1` are
      rejected.
    - It must have no zone: `a.Zone() == ""`.
    - It must be either `a.Is4() && a.IsLoopback()` (127.0.0.0/8) or
      `a == netip.IPv6Loopback()`.
    - Do **not** simplify this to `a.IsLoopback()`. `IsLoopback()` unmaps 4-in-6, so it
      would accept `[::ffff:127.0.0.1]`, and the documented behavior is to reject it.
  - **Port rules (C5(c)):**
    - `portStr` must be canonical decimal: 1–5 ASCII digits, first digit `1`–`9`, no sign,
      no leading zero, no whitespace.
    - It is converted with `strconv.Atoi` and must be at most 65535.
    - So `+6060`, `06060`, `0` and `70000` are rejected.
  - **Bind address (C5(d)):** `addr = net.JoinHostPort(a.String(), strconv.Itoa(port))`,
    built only from the parsed values and never from the raw string.
  - Every rejection error names its reason: `missing port`, `host must be a literal IP`,
    `host has a zone`, `host is not loopback`, `port not canonical decimal`,
    `port out of range` or `malformed`.
- `listenPprof(addr string, log *slog.Logger) (*pprofServer, error)`:
  - Calls `net.Listen("tcp", addr)` synchronously.
  - Builds `&http.Server{Handler: newPprofMux(), ReadHeaderTimeout: 10 * time.Second}` with no
    `WriteTimeout`, because `/profile` and `/trace` stream for up to 30 s. `Handler` must
    never be nil.
  - Starts `go srv.Serve(ln)`. If `Serve` returns anything other than `http.ErrServerClosed`,
    it logs at ERROR.
  - Logs `log.Warn("pprof profiling enabled on loopback listener", "addr", ln.Addr().String())`.
- `startPprof(raw string, log *slog.Logger) *pprofServer`:
  - Calls `ParsePprofAddr(raw)`, then `listenPprof(addr, log)` with only the parsed `addr`.
  - Returns nil, with no log, when disabled.
  - Returns nil after an ERROR log when the value is rejected
    (`"rejected RIOT_PPROF_ADDR; pprof disabled"` with `value` and `reason`) or the bind
    fails (`"pprof listener bind failed; pprof disabled"` with `addr` and `error`).
  - Never panics and never returns an error.
- `(*pprofServer).Addr()` and `(*pprofServer).Shutdown(ctx)` are safe on a nil receiver.
- `Server` gains an unexported `pprof *pprofServer` field.
- In `Start`, `s.pprof = startPprof(s.Config.PprofAddr, slog.Default())` runs after
  `s.httpServer` is constructed. The shutdown block calls `s.pprof.Shutdown(shutdownCtx)`
  before `s.httpServer.Shutdown`. Errors there are logged at WARN, not returned.

**Rationale**
- A pure parser lets AC-002 to AC-004 run as table tests.
- The parse/bind split resolves conflict T-1.
- Literal-IP-only parsing implements NFR-003.
- Canonical ports and binding from parsed values remove any textual gap between what was
  validated and what is bound (SEC-007).

**Alternatives considered**
- Validate in `LoadConfig`: `slog` is not configured yet there.
- Accept `localhost`: rejected by D-02.

**Consequences**
- Misconfiguration is visible only in logs, which is intended.
- Under `network_mode: host` the listener would bind the host's loopback (§10).

### AD-003: One security-headers middleware, placed second, that does not wrap the ResponseWriter; one HTTPS predicate

**Decision**
- The new `internal/server/middleware/security_headers.go` exports
  `SecurityHeaders() func(http.Handler) http.Handler`.
- Before calling `next`, it sets these on `w.Header()` using `Set`:
  - `Content-Security-Policy: <ContentSecurityPolicy(r.Host)>` (AD-004)
  - `X-Frame-Options: DENY`
  - `X-Content-Type-Options: nosniff`
  - `Referrer-Policy: strict-origin-when-cross-origin`
  - `Strict-Transport-Security: max-age=31536000`, **only if** `IsHTTPS(r)`
- A new `IsHTTPS(r *http.Request) bool { return RequestScheme(r) == "https" }` goes in
  `middleware/clientip.go`.
- The four cookie sites switch to `middleware.IsHTTPS(r)`.
- Chain order in `globalMiddleware()` (AD-013): `RealIP` → **`SecurityHeaders`** → `Logger`
  → `Recoverer` → `CORS` → `SetupGuard`.

**Rationale**
- HSTS depends on the trusted-proxy scheme that `RealIP` records, so the middleware must run
  after `RealIP`. `RealIP` never writes a response.
- Every downstream response class therefore carries the headers: CORS 204, SetupGuard 503,
  Admin/Device auth 401/403, Recoverer 500, OIDC redirects, `FileServer` responses, and the
  pprof 404.
- Not wrapping the writer keeps `Hijacker` and `Flusher` intact.
- The WebSocket 101 is exempt by construction (D-22).
- `http.Error` overrides only `Content-Type` and `X-Content-Type-Options`.

**Alternatives considered**
- Setting headers after `next` through a wrapping writer: breaks `Hijack`.
- Placing the middleware first: HSTS would ignore trusted proxies.
- Per-route headers: cannot be verified as complete.

**Consequences**
- Agent responses also carry the headers (D-21), which is harmless (C-1).
- With direct TLS, HSTS is sent; browsers ignore it when the server is reached by IP literal (§12.5).
- Behind a trusted proxy, HSTS appears exactly when the session cookie is `Secure`.

### AD-004: CSP content, `form-action 'self'`, and explicit same-host `ws:`/`wss:` sources from a validated `Host`

**Decision**

`ContentSecurityPolicy(host string) string` returns exactly this, in this order:

```
default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'<WS>; form-action 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'
```

- **`<WS>`:**
  - It is ` ws://<host> wss://<host>` when `host` passes `validCSPHost`.
  - `validCSPHost` requires a length of 1–262 bytes, with every byte in `[A-Za-z0-9.\-:\[\]]`.
    It is a byte loop, not a regexp.
  - Otherwise `<WS>` is empty, so the directive is exactly `connect-src 'self'`.
- **Performance:** the static prefix and suffix are package-level constants, so each request
  does one string concatenation.
- **`form-action 'self'` (SEC-008, rev 2):** this deliberately overrides FR-010's
  `form-action` clause and D-08 (§0).
- **No `upgrade-insecure-requests`.** That part of FR-010 stands, because the setup wizard
  runs over plain HTTP.
- **No `worker-src`, `manifest-src` or `media-src`.** They fall back to the defaults.

**Rationale**
- **WebSockets:** CSP3 `'self'` matches same-host `ws:`/`wss:`, but older WebKit and Chromium
  do not. So both are listed explicitly (FR-009).
- **Why not bare `ws:`/`wss:`:** a bare scheme would allow WebSockets to any host, which opens
  an exfiltration channel.
- **Host reflection grants nothing beyond `'self'`:** it is the document's own origin. That
  holds under DNS rebinding too.
- **The byte set blocks injection.** It excludes every policy-altering byte that Go's Host
  grammar accepts: `;` (new directive), `,` (second policy), whitespace (new source), `'`
  (keyword), `*` (wildcard), and `( ) ! $ & + = %`.
- **`form-action 'self'`** closes form-hijack exfiltration. It is safe because every form
  posts same-origin, and the SSO entry is a link, not a form submission.
- **`style-src 'unsafe-inline'`** covers xterm.js and Recharts (D-07).
- **`data:`** covers Vite-inlined assets.
- **No external origins** are loaded.
- **`blob:` downloads** are navigations, so they need no source.

**Alternatives considered**
- `connect-src 'self'` only: risks breaking the terminal on older browsers.
- Bare `ws:`/`wss:`: too broad.
- Nonces or hashes: out of scope.

**Consequences**
- The CSP string varies per request with the Host. There is no shared cache, so this is
  harmless.
- **If the SSO entry ever becomes a cross-origin form POST, `form-action 'self'` will block
  it.** `Login.test.tsx:73` pins the anchor.
- Any future third-party origin must extend this function. AC-011 catches relaxations.

### AD-005: Deterministic `Content-Type` for embedded static assets

**Decision**
- In `serveFrontend`, before `fileServer.ServeHTTP`, set `Content-Type` from a package-level
  map keyed by `path.Ext` of the resolved file. When the request falls back to the SPA, the
  resolved file is `index.html`.

  | Extension | Content-Type |
  |---|---|
  | `.html` | `text/html; charset=utf-8` |
  | `.js`, `.mjs` | `text/javascript; charset=utf-8` |
  | `.css` | `text/css; charset=utf-8` |
  | `.json`, `.map` | `application/json` |
  | `.webmanifest` | `application/manifest+json` |
  | `.ico` | `image/x-icon` |
  | `.png` | `image/png` |
  | `.svg` | `image/svg+xml` |
  | `.jpg`, `.jpeg` | `image/jpeg` |
  | `.webp` | `image/webp` |
  | `.woff2` | `font/woff2` |
  | `.woff` | `font/woff` |
  | `.ttf` | `font/ttf` |
  | `.txt` | `text/plain; charset=utf-8` |

- Unknown extensions are left to `FileServer`.

**Rationale**
- `nosniff` makes browsers reject scripts and styles with the wrong MIME type.
- Go's MIME detection depends on the OS: the Windows registry and Alpine's missing
  `mime.types` both change results.

**Alternatives considered**
- `mime.AddExtensionType` in `init()`: changes global state for every package.

**Consequences**
- Existing routes are unaffected.

### AD-006: Secret-key registry, URL-userinfo classification and sentinel live in `notify`

**Decision**

The new `internal/server/notify/secrets.go` exports:

- `const RedactedSecret = "[rIOt-redacted]"`. It is fixed, ASCII, not a plausible credential,
  and not an absolute URL (V-001).
- An unexported registry, exactly matching T-1:

  | Type | Secret keys |
  |---|---|
  | `smtp` | `password` |
  | `ntfy` | `token`, `topic` |
  | `telegram` | `bot_token` |
  | `discord` | `webhook_url` |
  | `slack` | `webhook_url` |
  | `pushover` | `api_token`, `user_key` |
  | `gotify` | `app_token` |
  | `webhook` | `url`, plus every value in `headers` |

- `SecretKeys(channelType) (keys []string, known bool)`. An unknown type returns
  `(nil, false)`, which means every key is secret.
- `RedactChannel`, `MergeSecrets` and `ScrubSecrets` (AD-007, AD-008).
- **URL-userinfo classification (C1, rev 2):**
  - The userinfo component of any top-level string config value that has a URL authority is
    a secret, whatever its key and whether or not the registry lists the key.
  - Examples: ntfy and gotify `server_url`, or any URL an unknown type stores.
  - This amends the FRD's T-1/FR-022 "returned as-is" to read "as-is except embedded
    credentials".
  - Userinfo is detected by an unexported pure string function,
    `splitURLUserinfo(v string) (prefix, userinfo, rest string, ok bool)`:
    - `ok` requires `v` to start with a valid scheme (an ASCII letter, then letters, digits,
      `+`, `-` or `.`) followed by `://`.
    - The authority is the text after `://` up to the first `/`, `?` or `#`.
    - `userinfo` is the authority text before its **last** `@`. It is `""` when there is no
      `@`.
    - `prefix` is `scheme://`, and `rest` is everything after that `@`.
    - It deliberately does **not** call `url.Parse`. The redacted form
      `https://[rIOt-redacted]@host` is not a parseable URL (brackets), yet it must still be
      recognized on PUT.

**Rationale**
- `notify` owns the channel types and the backends.
- The dispatcher needs the same rules for scrubbing (NFR-010).
- Go's client sends userinfo as Basic auth, so userinfo is a working credential. An operator
  with a basic-auth-protected ntfy or gotify will put it in `server_url` (SEC-001).

**Alternatives considered**
- Classifying every `server_url` as wholly secret: it hides the host, which the operator needs
  to see, and it breaks FR-022 more than necessary.
- A per-field "configured" flag: rejected by D-12.

**Consequences**
- The frontend keeps a UX-only mirror (AD-009).
- A Go test pins the sentinel literal in `web/src/utils/secrets.ts`.

### AD-007: Pure redaction and merge over copies; a type change inherits nothing; userinfo is write-only; final sentinel sweep

**Decision**

All functions here are pure, never mutate their inputs, and always build new maps.
- `nonEmpty(v)` means `v != nil`, and for strings `v != ""`.
- `deepCopyConfig` recurses into nested `map[string]interface{}`.

**`RedactChannel(ch) models.NotificationChannel`:**
1. Copy the struct. `Config = deepCopyConfig(ch.Config)`, and a nil config becomes an empty
   map.
2. For each secret key that is present (registry keys for a known type, every key for an
   unknown type): write `RedactedSecret` when `nonEmpty`, else `""`.
3. For webhook `headers`:
   - When it is a `map[string]interface{}`: keep the names, and set each value to the
     sentinel or `""` by the same rule.
   - Any other non-nil type is replaced wholesale by the sentinel (fail closed).
4. **Userinfo (C1):** for every remaining top-level string value, if `splitURLUserinfo`
   returns `ok` with a non-empty `userinfo`, rewrite it as `prefix + RedactedSecret + "@" + rest`.
   For example, `https://alice:S3cret@ntfy.home/x` becomes
   `https://[rIOt-redacted]@ntfy.home/x`. A username-only userinfo is redacted the same way.
5. Non-secret values without userinfo, and `id`, `name`, `type`, `enabled` and the
   timestamps, are unchanged.

**`MergeSecrets(stored *models.NotificationChannel, submitted models.NotificationChannel) map[string]interface{}`:**
1. `out := deepCopyConfig(submitted.Config)`.
2. `inherit := stored != nil && stored.Type == submitted.Type`.
3. `prev := stored.Config` if `inherit`, else an empty map. A type change inherits nothing
   (T-4), and that applies to userinfo too.
4. Secret key set: the registry keys for a known type, or for an unknown type the union of the
   keys in `prev` and `submitted.Config`.
5. For each secret key `k`, with `sv, present := submitted.Config[k]`:
   - not present → `out[k] = prev[k]` if `nonEmpty(prev[k])`, else leave it absent
   - `sv == RedactedSecret` → `out[k] = prev[k]` if `nonEmpty`, else `""`
   - otherwise → `out[k] = sv`
6. For webhook `headers`:
   - absent or `nil` → copy `prev["headers"]` if `inherit`
   - a map → the full set of names. A sentinel value resolves to the previous value for that
     name if `nonEmpty`, else `""`. Other values are taken as submitted. Names not submitted
     are dropped.
   - any other type → stored as submitted
7. **Userinfo merge (C1).** For each top-level non-secret key `k` whose submitted value `sv` is
   a string, take `p, ui, rest, ok := splitURLUserinfo(sv)`. When `ok && ui == RedactedSecret`:
   - Set `out[k] = p + pui + "@" + rest` if all of these hold: `inherit` is true, `prev[k]` is
     a string, `splitURLUserinfo(prev[k])` yields a `pui` that is non-empty and not the
     sentinel.
   - Otherwise set `out[k] = p + rest`, which drops the userinfo.
   - Any other submitted userinfo is taken as typed: a new credential replaces the old one,
     and no `@` means no credential.
   - This keeps the stored credential even when the host part was edited. That is a
     destination-rebinding residual (SEC-002, §10, F-7).
8. **Final sweep (V-005, T-5, C1).** For every string at the top level of `out`, and one level
   down in nested `map[string]interface{}`:
   - a value equal to `RedactedSecret` becomes `""`
   - a value whose `splitURLUserinfo` userinfo equals `RedactedSecret` becomes `prefix + rest`

   Arrays and deeper nesting are not swept (SEC-009). Nothing reads them as secrets.
9. Non-secret keys keep full-replacement semantics (FR-026).

**Create** uses `MergeSecrets(nil, submitted)`. With no stored channel, a sentinel userinfo is
stripped, and a sentinel secret becomes `""`.

**Handler wiring (`settings.go`)**
- **List:** apply `RedactChannel` to each channel. A nil slice becomes `[]`.
- **Create:**
  1. Validate as today.
  2. `ch.Config = notify.MergeSecrets(nil, ch)`.
  3. `CreateChannel(&ch)`.
  4. Respond `201` with `notify.RedactChannel(ch)`.
- **Update:**
  1. Parse the ID (400 on failure) and decode the body (400 on failure).
  2. `stored, err := GetChannel(id)`. If `err != nil || stored == nil`, respond **404**
     `{"error":"channel not found"}` with no write (FR-027).
  3. `ch.ID = id`. A nil config becomes an empty map.
  4. `ch.Config = notify.MergeSecrets(stored, ch)`.
  5. `UpdateChannel(&ch)` (500 on failure).
  6. Respond `200` with `notify.RedactChannel(ch)`.
- **Test-send:** unchanged. It sends the stored, unredacted channel (FR-029), and its error is
  scrubbed (AD-008).

**Rationale**
- Pure functions satisfy NFR-009.
- Copy-on-write prevents a response from aliasing stored maps.
- The type-change rule stops a stored secret being reused for a different backend.
- The userinfo rule satisfies C1 with no new API field. The host stays visible, and an
  untouched `server_url` round-trips through the editor unchanged.

**Alternatives considered**
- Redacting in the repository layer: the dispatcher needs cleartext.
- Inheriting across a type change: surprising for the operator.
- Masking only the password inside userinfo: the username can be a token too.

**Consequences**
- An unknown-type key can be dropped only by sending `""`.
- D-14 is accepted: a real secret equal to the sentinel cannot be stored.
- The ntfy and gotify `server_url` fields show `https://[rIOt-redacted]@host` when a credential
  is stored (AD-009, README).

### AD-008: Scrub error text at the dispatcher's send choke point, matching net/http's real serializations (FR-030, C2)

**Decision**

`ScrubSecrets(text string, ch models.NotificationChannel) string`:

1. **Collect the secret values:**
   - every `nonEmpty` string secret: registry keys, webhook header values, and every string
     value for an unknown type
   - **and (C2)**, for every top-level string config value (any key) where
     `splitURLUserinfo` gives a non-empty `userinfo`: the raw `userinfo`, plus its username
     and password. These come from splitting at the first `:`. Each is also included in its
     `url.PathUnescape` form when that unescapes cleanly.
2. **Build the variants of each value `v`:**
   - `v`, `url.PathEscape(v)` and `url.QueryEscape(v)`.
   - If `url.Parse(v)` succeeds with a non-empty `Scheme`, also add:
     - `u.String()`
     - `u.Redacted()`
     - **the net/http `stripPassword` form**. When a password is set, it is
       `strings.Replace(u.String(), u.User.String()+"@", u.User.Username()+":***@", 1)`;
       otherwise it is `u.String()`. This copies net/http's implementation exactly.
3. **(C2) Quoted forms:** for every variant `w` from step 2, also add
   `q := strconv.Quote(w); q[1:len(q)-1]`, the `%q` interior that `url.Error` emits, when it
   differs from `w`.
4. Drop empty variants, de-duplicate, sort longest first, and apply
   `strings.ReplaceAll(text, w, RedactedSecret)` for each.

The unexported `scrubError(err, ch) error` returns `nil` for `nil`, and otherwise
`errors.New(ScrubSecrets(err.Error(), ch))`. It has no `Unwrap`.

`dispatcher.go` applies `scrubError` immediately to the result of `backend.Send` in
`Dispatch`, `SendToChannel` and `TestChannel`. As a result:
- `ErrorMsg`, the `slog.Error` attribute, the `SendToChannel` return value (logged by the
  server-error-alert worker) and the test-send response all see only scrubbed text.
- The `UnsupportedChannelError` paths are unchanged.

**Rationale**
- One choke point covers all four leak surfaces.
- The variant set now matches the exact strings Go's client writes:
  - a secret URL's `user:***@host/path/TOKEN` is caught as a whole-URL match
  - a non-secret `server_url`'s username is scrubbed individually
  - the password never appears, because net/http strips it
  - `%q` escaping (for `"`, `\` or non-printable bytes) is covered

**Alternatives considered**
- Scrubbing in each backend: eight call sites to keep in sync.
- Scrubbing in the handler only: misses the log and `ErrorMsg` surfaces.

**Consequences**
- Short secrets, such as a short topic or a username like `riot`, can over-redact error
  text. This is cosmetic and errs on the side of safety.
- Rows written before deploy keep any leaked text until retention removes them (§10).

### AD-009: Frontend sentinel contract: `SecretInput`, a "configured" state, an explicit clear, and no browser autofill

**Decision**

**`web/src/utils/secrets.ts` (new)**
- `export const REDACTED_SECRET = '[rIOt-redacted]'`.
- `SECRET_KEYS`, a mirror of T-1.
- `isRedacted`, `isSecretSet`.
- `redactChannelConfig(type, config)`, a mirror of `RedactChannel`, including headers,
  redact-all for unknown types, and the userinfo rewrite. It is used by the demo client.
- It lives in `utils/`, so both the real and the demo clients can import it (the demo alias
  rewrites only `/api/settings`).

**`web/src/components/SecretInput.tsx` (new)**
- Props:
  `{ label: string; name: string; value: unknown; configured: boolean; onChange: (v: string) => void; placeholder?: string; masked?: boolean; clearable?: boolean }`.
- Display value: `value` only if it is a string other than `REDACTED_SECRET`, otherwise `''`.
  The sentinel is never editable text.
- When `configured && value === REDACTED_SECRET`: show the text **"Configured (unchanged)"**
  and the placeholder "Leave blank to keep the current value".
- When the user types: if the text is `''` and the field is configured, call
  `onChange(REDACTED_SECRET)`. Otherwise call `onChange(t)`.
- When `clearable && configured && value !== ''`: show a **"Clear"** button that calls
  `onChange('')`.
- When `configured && value === ''`: show **"Will be cleared on save"** and an **"Undo"**
  button that calls `onChange(REDACTED_SECRET)`.
- **Autofill opt-out (C3)**, on every rendered `<input>`:

  | Attribute | Value |
  |---|---|
  | `aria-label` | `{label}` |
  | `name` | `riot-secret-${name}`, a unique, non-credential-looking name |
  | `id` | the same value as `name` |
  | `autoComplete` | `"new-password"` when `masked` (type `password`); `"off"` otherwise (type `text`) |
  | `data-1p-ignore` | present |
  | `data-lpignore` | `"true"` |
  | `data-bwignore` | `"true"` |
  | `data-form-type` | `"other"` |
  | `spellCheck` | `{false}` |
  | `autoCapitalize` | `"off"` |
  | `autoCorrect` | `"off"` |

**`NotificationSettings.tsx` edits**
- New state `original: NotificationChannel | null`.
- `configured(key) = !isNew && original?.type === editing.type && isRedacted(original.config[key])`.
- Every T-1 secret field renders through `SecretInput`:

  | Type | Field | masked | clearable |
  |---|---|---|---|
  | smtp | `password` | yes | **yes** |
  | ntfy | `topic` | no | no |
  | ntfy | `token` | yes | **yes** |
  | telegram | `bot_token` | yes | no |
  | discord, slack | `webhook_url` | no | no |
  | pushover | `api_token` | yes | no |
  | pushover | `user_key` | yes | no |
  | gotify | `app_token` | yes | no |
  | webhook | `url` | no | no |

- **C3:** the SMTP `username` input, and the ntfy and gotify `server_url` inputs, get
  `autoComplete="off"` and `data-lpignore="true"`. This breaks the "text field then password
  field" login heuristic.
- The editor must stay **outside any `<form>` element**, as it is today. That reduces browser
  save-password prompts.
- The ntfy and gotify `server_url` inputs show the redacted userinfo form verbatim. The
  operator keeps the credential by leaving `[rIOt-redacted]@` in place, removes it by deleting
  that part, or replaces it by typing `user:pass@`.
- Webhook headers round-trip through the `{...config}` spread.
- The list rows never render a secret or the sentinel:
  - ntfy: `<server_url>/(topic hidden)` or `(no topic)`
  - webhook: `URL configured` or `(no URL)`
  - discord and slack: `Webhook URL configured` or `(no webhook URL)`
  - pushover: `User key configured` or `(not set)`
  - gotify: `server_url`, already userinfo-redacted by the server
- The Test button is unchanged.

**Demo**
- `demo-data.ts`: the ntfy config becomes
  `{ server_url: 'https://ntfy.sh', topic: REDACTED_SECRET }`.
- `demo-settings.ts`: create and update echo
  `config: redactChannelConfig(ch.type, ch.config ?? {})`.

**No change** to `web/src/api/settings.ts` or `web/src/types/models.ts`.

**Rationale**
- Sentinel versus empty is the whole contract (D-12).
- Auto-reverting an erased input removes the accidental-wipe path (BR-003).
- An empty password-type input next to a text input attracts the browser's saved rIOt admin
  login (SEC-003). `new-password` stops credential autofill in Chrome and Firefox, and the
  ignore attributes cover 1Password, LastPass, Bitwarden and Dashlane.

**Alternatives considered**
- Showing the sentinel as masked dots: edits would append to it.
- Clear on every field: lets the operator break required secrets.
- `autoComplete="off"` alone on password fields: Chrome ignores it for credential fields.

**Consequences**
- The ntfy topic is hidden.
- The frontend mirror can drift from Go, but that affects display only.
- Autofill behavior differs between browsers, so a manual post-deploy check remains (§13).

### AD-010: Constant-time registration key comparison over SHA-256 digests

**Decision**
- Add `registrationKeyMatches(provided, configured string) bool` to `handlers.go`:

  ```
  a, b := sha256.Sum256([]byte(provided)), sha256.Sum256([]byte(configured))
  return subtle.ConstantTimeCompare(a[:], b[:]) == 1
  ```

- `RegisterDevice` uses `if !registrationKeyMatches(apiKey, regKey)`.
- Add the `crypto/subtle` import.
- Everything else is byte-identical, including the empty-key bypass and the 401 body.

**Rationale**
- Fixed-length digests remove both the content and the length side channels (D-11, V-003).

**Alternatives considered**
- HMAC: equivalent, but more code.

**Consequences**
- The `http.Error` body and its `text/plain` content type stay the same (FR-034, C-1).
- On a deployment with no key set, this code path is dormant until one is.

### AD-011: `HealthHandler` with an injectable, time-bounded probe and an explicit migrations flag

**Decision**

New file `internal/server/handlers/health.go` with:
- `type DBHealthChecker interface { Healthy(ctx context.Context) bool }`. `*db.DB` already
  satisfies it.
- `DefaultHealthProbeTimeout = 2 * time.Second`.
- `HealthHandler{DB DBHealthChecker; Migrated func() bool; ProbeTimeout time.Duration; Now func() time.Time}`.
  - A nil `DB` means the database counts as down.
  - A nil `Migrated` means migrations count as incomplete.
  - `ProbeTimeout <= 0` means the 2 s default.
  - A nil `Now` means `time.Now`.

**Probe:** `context.WithTimeout`, run the check in a goroutine that writes to a channel of
capacity 1, then `select` on the result or `ctx.Done()`. A timeout counts as `false`.

**`Health` response:** a fixed struct `healthBody{Status, Database, Time}`.

| DB reachable | Status code | `status` |
|---|---|---|
| yes | 200 | `"ok"` |
| no | 503 | `"degraded"` |

**`Readyz` response:** a fixed struct `readyBody{Status, Database, Migrations, Time}`.

| DB reachable and migrations complete | Status code | `status` |
|---|---|---|
| yes | 200 | `"ready"` |
| no | 503 | `"not_ready"` |

**Wiring**
- Delete `(*Handlers).Health`.
- Add `Server.MigrationsComplete atomic.Bool`. Store `true` right after the migrations block,
  or when `MigrationsFS == nil`.
- In the router:
  - `hh := &handlers.HealthHandler{Migrated: s.MigrationsComplete.Load}`
  - `if s.DB != nil { hh.DB = s.DB }`
  - `r.Get("/health", hh.Health)` and `r.Get("/readyz", hh.Readyz)`
- Add `/readyz` to the explicit allow line in `middleware/setup.go`.
- The bodies contain no error text (FR-040).

**Rationale**
- No database is needed in tests (NFR-009).
- The response time is bounded (NFR-005).
- Readiness is modelled explicitly (D-17).

**Alternatives considered**
- Keeping `Health` as a method on `*Handlers`: it drags in every handler dependency.

**Consequences**
- `doctor` warns "returned 503" during an outage. AC-053 covers this.
- Each probe request still costs one DB ping (SEC-006, F-10).

### AD-012: Retry queue, Option B

**Decision**
- Delete `internal/resilient/retry_queue.go` and its test.
- In `README.md`, remove ", and automatic retry queue" from L37.
- Delete the "Notification Retry Queue" subsection at L996–998.
- No other file references the queue.

**Rationale**
- The defects listed in FRD §2.2-5 make wiring the queue in a new feature.
- Removing it makes invariant I-2 true and removes a sink for secrets at rest.

**Alternatives considered**
- Option A: rejected by OQ-1.

**Consequences**
- Failed notifications are not retried, and the README stops claiming that they are.

### AD-013: Router testability through `(*Server).globalMiddleware()`

**Decision**
- `router.go` gains `func (s *Server) globalMiddleware() []func(http.Handler) http.Handler`,
  returning in order:
  - `RealIP(ParseTrustedProxies(s.Config.TrustedProxies))`
  - `SecurityHeaders()`
  - `Logger`
  - `chimw.Recoverer`
  - `CORS(s.Config.AllowedOrigins)`
  - `SetupGuard(&s.SetupComplete)`
- `setupRouter` calls `r.Use(s.globalMiddleware()...)`.
- `router_test.go` uses two harnesses:
  - **(a) The real `setupRouter`.** The server is built with `Config`, an `fstest.MapFS`
    frontend, a `JWTSecret` and nil repos. It exercises only DB-free routes: pprof (canonical
    paths and variants), `/health`, `/readyz`, the SPA and static assets, `OPTIONS`, and
    unauthenticated `/api/v1/devices`.
  - **(b) The middleware harness.** `chi.NewRouter()` plus `globalMiddleware()` plus routes on
    mock-backed `handlers.New(...)`:
    - register
    - heartbeat behind `DeviceAuth(mock)`
    - auth-check
    - login
    - `/ws` behind `AdminAuth`
    - `/ws/agent`
    - a `/panic` route
- AC-016 is tested in package `handlers` (T-3).

**Rationale**
- There is one definition of the middleware chain, so the tests cannot drift from production.

**Consequences**
- No runtime change beyond the inserted middleware.

### AD-014: Test naming

**Decision**
- Go test names follow `TestHARDEN002_AC0NN_<Desc>`, with subtests for tables.
- Security-review additions also carry the condition id: `TestHARDEN002_AC0NN_Cn_<Desc>`, or
  `TestHARDEN002_Cn_<Desc>` when no AC fits.
- Vitest titles start with `[HARDEN-002][AC-0NN]`, adding `[Cn]` where applicable.

**Rationale**
- Avoids the T-7 collisions.
- AC ids and condition ids are both greppable.

---

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `internal/server/pprof.go` | `pprofHandler()` (moved), `newPprofMux`, `ParsePprofAddr`, `listenPprof`, `startPprof`, `pprofServer{Addr,Shutdown}` (AD-001/002) |
| CREATE | `internal/server/pprof_test.go` | AC-002, AC-003 (+C5 canonical), AC-004 (+C5 cases), AC-005, AC-007 |
| MODIFY | `internal/server/router.go` | Remove the pprof import, function, mount and comment. Add `globalMiddleware()`, the pprof 404 routes, `HealthHandler` wiring, and the Content-Type map in `serveFrontend` |
| CREATE | `internal/server/router_test.go` | AC-001 (+C5 variants), AC-006, AC-009, AC-010, AC-013, AC-014, AC-015, AC-017, AC-018, AC-050 |
| CREATE | `internal/server/invariants_test.go` | C5(b): source scan of non-test `*.go` under `cmd/` and `internal/` |
| MODIFY | `internal/server/server.go` | Fields `pprof` and `MigrationsComplete`. Set the flag. `startPprof` call. `s.pprof.Shutdown` call |
| MODIFY | `internal/server/config.go` | `PprofAddr` from `RIOT_PPROF_ADDR` (trimmed) |
| MODIFY | `internal/server/config_test.go` | `PprofAddr` default `""` and trimming |
| CREATE | `internal/server/docs_test.go` | AC-054, AC-055, AC-064 |
| CREATE | `internal/server/middleware/security_headers.go` | `SecurityHeaders()`, `ContentSecurityPolicy(host)`, `validCSPHost` |
| CREATE | `internal/server/middleware/security_headers_test.go` | AC-011, AC-012 (+C6), AC-013, AC-015, `IsHTTPS` parity |
| MODIFY | `internal/server/middleware/clientip.go` | Add `IsHTTPS(r)` |
| MODIFY | `internal/server/middleware/setup.go` | Explicitly allow `/readyz` |
| MODIFY | `internal/server/middleware/setup_test.go` | `/readyz` passes the guard in setup mode |
| MODIFY | `internal/server/handlers/auth.go` | `Secure: middleware.IsHTTPS(r)` (2 sites) |
| MODIFY | `internal/server/handlers/oidc.go` | `Secure: middleware.IsHTTPS(r)` (2 sites) |
| CREATE | `internal/server/handlers/health.go` | `DBHealthChecker`, `DefaultHealthProbeTimeout`, `HealthHandler` |
| CREATE | `internal/server/handlers/health_test.go` | AC-045 to AC-049, AC-051, AC-052 |
| MODIFY | `internal/server/handlers/handlers.go` | Delete `Health`. Add `registrationKeyMatches` and use it in `RegisterDevice` |
| CREATE | `internal/server/handlers/register_key_test.go` | AC-041 to AC-044 |
| MODIFY | `internal/server/handlers/settings.go` | List, Create and Update redaction. Update load, 404 and merge |
| CREATE | `internal/server/handlers/settings_secrets_test.go` | AC-019 to AC-034, including the C1 tests and the C2 test-send table |
| CREATE | `internal/server/handlers/security_headers_oidc_test.go` | AC-016 |
| CREATE | `internal/server/notify/secrets.go` | `RedactedSecret`, registry, `SecretKeys`, `splitURLUserinfo`, `RedactChannel`, `MergeSecrets`, `ScrubSecrets`, `scrubError` |
| CREATE | `internal/server/notify/secrets_test.go` | Pure tables (redaction, merge, sweep, userinfo split, scrub variants including `***` and `%q`). Sentinel parity with the frontend |
| CREATE | `internal/server/notify/scrub_client_test.go` | C2: real `http.Client` errors through an erroring `RoundTripper` across 6 backends × `Dispatch`, `SendToChannel` and `TestChannel` (AC-035) |
| MODIFY | `internal/server/notify/dispatcher.go` | `scrubError` at the three `backend.Send` sites |
| MODIFY | `internal/server/notify/dispatcher_test.go` | AC-035 baseline (mock-channel error and captured slog) |
| MODIFY | `internal/testutil/mocks.go` | `MockNotifyRepo.UpdateChannelCalls int` |
| DELETE | `internal/resilient/retry_queue.go` | Option B |
| DELETE | `internal/resilient/retry_queue_test.go` | Option B |
| MODIFY | `internal/agent/doctor_test.go` | AC-053 (test only) |
| CREATE | `web/src/utils/secrets.ts` | `REDACTED_SECRET`, `SECRET_KEYS`, `isRedacted`, `isSecretSet`, `redactChannelConfig` |
| CREATE | `web/src/utils/secrets.test.ts` | Helper tests, including the userinfo rewrite |
| CREATE | `web/src/components/SecretInput.tsx` | Sentinel-aware secret input with autofill opt-out |
| MODIFY | `web/src/pages/settings/NotificationSettings.tsx` | `original` state, `configured()`, `SecretInput`, autofill opt-out on SMTP `username` and `server_url`, safe list summaries |
| CREATE | `web/src/pages/settings/NotificationSettings.test.tsx` | AC-036, AC-037 (+C3), AC-038, AC-039 |
| MODIFY | `web/src/api/demo-data.ts` | ntfy mock channel: `server_url` and the sentinel topic |
| MODIFY | `web/src/api/demo-settings.ts` | Create and update echo redacted config |
| CREATE | `web/src/api/demo-settings.test.ts` | AC-040 |
| MODIFY | `README.md` | FR-043 removals and FR-054 additions, plus the C4 and SEC-004 text (§12.3) |
| MODIFY | `CHANGELOG.md` | `[HARDEN-002]` entries (§12.3) |

### 4.1 Deliberately NOT changed

| File | Why |
|---|---|
| `Dockerfile`, `docker-compose.yml`, `docker-compose.prod.yml` | A riot-server `HEALTHCHECK` is out of scope. OQ-4 found no consumer. |
| `internal/agent/doctor.go` | It already checks only the status code (FR-041). |
| `internal/server/db/*`, `migrations/*` | No schema change. |
| `internal/resilient/dns_cache.go`, `http_client.go` (and their tests) | D-20 |
| `web/src/api/settings.ts`, `web/src/types/models.ts` | The wire shape is unchanged. |
| `web/src/pages/Login.tsx` | The SSO anchor is already outside the form, and `Login.test.tsx:73` pins it. |
| Agent API handlers and routes | C-1. Only the comparison expression in `RegisterDevice` changes. |
| `middleware/logger.go`, `cors.go`, `admin_auth.go`, `auth.go` | The new middleware wraps none of these. |

---

## 5. Data Model Changes

- No schema change and no migration. `notification_channels.config` stays plaintext JSONB.
- `Config` gains `PprofAddr string`.
- `Server` gains `pprof *pprofServer` (unexported) and `MigrationsComplete atomic.Bool`.
- New handler types: `HealthHandler` and `DBHealthChecker`. `(*Handlers).Health(*db.DB)` is
  removed.
- New constant `notify.RedactedSecret = "[rIOt-redacted]"`, with its frontend twin
  `REDACTED_SECRET`.
- Notification channel config is unchanged at rest. In responses:
  - T-1 secret values become the sentinel when non-empty, or `""`.
  - The userinfo of any top-level config URL becomes `[rIOt-redacted]`.

---

## 6. API / Interface Contract

### 6.1 Headers on every main-listener response (except the WebSocket 101)

| Header | Value |
|---|---|
| `Content-Security-Policy` | `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self' ws://<Host> wss://<Host>; form-action 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'`. When the Host is invalid, the directive becomes `connect-src 'self'`. |
| `X-Frame-Options` | `DENY` |
| `X-Content-Type-Options` | `nosniff` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `Strict-Transport-Security` | `max-age=31536000`, only when `middleware.IsHTTPS(r)` |

Status codes, bodies and all other headers of existing routes are unchanged (FR-017).

### 6.2 `GET /health` (public, not setup-guarded)

| Status | Body (`application/json`) |
|---|---|
| 200 | `{"status":"ok","database":true,"time":"<RFC3339 UTC>"}` |
| 503 | `{"status":"degraded","database":false,"time":"<RFC3339 UTC>"}` |

### 6.3 `GET /readyz` (new, public, not setup-guarded)

| Status | Body |
|---|---|
| 200 | `{"status":"ready","database":true,"migrations":true,"time":"…"}` |
| 503 | `{"status":"not_ready","database":<bool>,"migrations":<bool>,"time":"…"}` |

### 6.4 `/debug/pprof` and `/debug/pprof/*` on the main listener

- Any method returns `404`, `text/plain; charset=utf-8`, with body `{"error":"not found"}`.
- Non-canonical variants (case, doubled slash, dot segment, `%2F`) reach the SPA catch-all,
  never pprof.
- **The loopback listener** exists only when `RIOT_PPROF_ADDR` is a valid loopback
  `ip:port`. It serves the standard pprof endpoints with no auth.

### 6.5 Notification channels (admin JWT, routes unchanged)

| Endpoint | Change |
|---|---|
| `GET …/notification-channels` | T-1 secret keys are `"[rIOt-redacted]"` when set, else `""`. Webhook header values are redacted. Unknown types are fully redacted. **Any top-level config URL with userinfo becomes `scheme://[rIOt-redacted]@host…` (C1).** |
| `POST …/notification-channels` | Secrets are stored as submitted. A submitted sentinel is stored as `""`, and a sentinel userinfo is stripped. Responds 201 with the redacted channel. |
| `PUT …/notification-channels/{id}` | Per secret key: sentinel or omitted → keep; new value → replace; `""` → clear. A sentinel userinfo keeps the stored userinfo. A type change inherits nothing. **Unknown ID → 404 `{"error":"channel not found"}`, with no write.** Responds 200 with the redacted channel. |
| `POST …/notification-channels/{id}/test` | Shape unchanged. `error` is scrubbed of every secret value, every userinfo username and password, and the `***` and `%q` forms. |
| `GET …/notifications/log` | Unchanged. `error_msg` is scrubbed from deploy onward. |

### 6.6 Agent API

Unchanged (C-1).

### 6.7 Configuration

| Variable | Default | Contract |
|---|---|---|
| `RIOT_PPROF_ADDR` | empty (disabled) | `<loopback-ip>:<port>`. The host is IPv4 in `127.0.0.0/8` or `[::1]`, with no zone. The port is canonical decimal 1–65535. Anything else is logged at ERROR and ignored. A bind failure is logged at ERROR. Enabling it is logged at WARN. |

---

## 7. Sequence / Flow

**7.1 Request through the main listener**
1. `RealIP` resolves the client IP and records the scheme, but only from trusted proxies.
2. `SecurityHeaders` sets the headers.
3. `Logger`, then `Recoverer`, then `CORS`. `OPTIONS` requests end here with 204.
4. `SetupGuard`, then the route:
   - health endpoints → `HealthHandler`
   - `/debug/pprof*` → 404
   - API routes → handled as before
   - everything else → `serveFrontend`, with the deterministic Content-Type

**7.2 Startup and shutdown**
1. Startup order:
   1. DB connect
   2. migrations
   3. `MigrationsComplete.Store(true)`
   4. repos and workers
   5. `setupRouter`
   6. `httpServer`
   7. TLS
   8. `startPprof`: nil when disabled, rejected or the bind fails; otherwise the listener is
      serving and a WARN line is logged
   9. serve
2. On a signal:
   1. `s.pprof.Shutdown`
   2. `httpServer.Shutdown`
   3. `DB.Close`

**7.3 Notification channel PUT**
1. Parse and decode the request (400 on failure).
2. `GetChannel` fails → 404.
3. `MergeSecrets`: secret keys, headers, and URL userinfo, then the sweep. The result never
   contains a sentinel.
4. `UpdateChannel`.
5. Respond with `RedactChannel`.

**7.4 Send failure**
1. `backend.Send` returns an error. For HTTP backends this is a `url.Error` carrying
   `"Post \"https://u:***@host/…\": …"`.
2. `scrubError` → `ScrubSecrets`.
3. The scrubbed error goes to `ErrorMsg`, slog, and the return value.

**7.5 Editor save without touching secrets**
1. The page shows "Configured (unchanged)". Autofill is suppressed.
2. The user saves.
3. The payload carries the sentinel and the redacted `server_url`.
4. The server keeps the stored password and userinfo.

---

## 8. Acceptance Criteria Mapping

- `GO` = `go test ./...`
- `VT` = `cd web && npm run test:run`
- `REVIEW` = code review check
- **Bold** test names are new or changed in rev 2.

| AC | Fulfilled By | Test Strategy (test name) |
|---|---|---|
| AC-001 | AD-001 | GO `router_test.go` `TestHARDEN002_AC001_PprofNotServedOnMainRouter`: harness (a). Four canonical paths → 404 with no pprof markers or SPA marker. **GO `TestHARDEN002_AC001_C5_PprofPathVariantsServeNoProfile`**: `/DEBUG/PPROF/`, `//debug/pprof/`, `/debug//pprof/heap`, `/debug/pprof/../debug/pprof/heap`, `/debug%2Fpprof/heap` (set `URL.RawPath` explicitly) → no `goroutine profile`, `heap profile` or `Types of profiles available` in the body, whatever the status. |
| AC-002 | AD-002 | GO `TestHARDEN002_AC002_PprofDisabledByDefault` |
| AC-003 | AD-002 | GO `TestHARDEN002_AC003_LoopbackAddrsAccepted` (`127.0.0.1:6060`, `127.0.0.2:6061`, `[::1]:6060`). **GO `TestHARDEN002_AC003_C5_BindAddrBuiltFromParsedValues`**: the returned addr equals `net.JoinHostPort(canonicalIP, canonicalPort)` for each accepted input. `startPprof("127.0.0.1:<free>")` logs WARN with that exact addr. REVIEW: `startPprof` passes only the parsed addr to `listenPprof`. |
| AC-004 | AD-002 | GO **`TestHARDEN002_AC004_NonLoopbackAddrsRejected`** (table extended). Original cases: `:6060`, `0.0.0.0:6060`, `[::]:6060`, `192.168.1.5:6060`, `localhost:6060`, `127.0.0.1`, `127.0.0.1:0`, `127.0.0.1:70000`, `garbage`. **Added (C5(c)), all rejected:** `[::ffff:127.0.0.1]:6060`, `[::1%lo]:6060`, `127.1:6060`, `0177.0.0.1:6060`, `127.0.0.1:+6060`, `127.0.0.1:06060`. Each rejection has a reason; `startPprof` returns nil and logs an ERROR naming the value. |
| AC-005 | AD-002 | GO `TestHARDEN002_AC005_LoopbackListenerServesPprof` |
| AC-006 | AD-001 + AD-002 | GO `TestHARDEN002_AC006_LoopbackEnabledMainRouterStill404` |
| AC-007 | AD-002 | GO `TestHARDEN002_AC007_PprofEnableLoggedAndShutdownCloses` |
| AC-008 | N/A (OQ-6) | none |
| AC-009 | AD-003 | GO `TestHARDEN002_AC009_BaselineHeadersOnSPA` |
| AC-010 | AD-003 + AD-013 | GO `TestHARDEN002_AC010_HeadersOnEveryResponseClass`. Subtests across harnesses (a) and (b): 401, 204, 503 health, readyz, static, auth-check, register wrong key (`{"error":"invalid registration key"}\n`), heartbeat 200, panic 500. Each asserts the four headers and the pre-story status and body. |
| AC-011 | AD-004 | GO **`TestHARDEN002_AC011_CSPDirectiveContent`** (assertion changed in rev 2). Required directives present. `script-src` has no `unsafe-inline` or `unsafe-eval`. No `upgrade-insecure-requests`. **`form-action 'self'` is present (SEC-008; replaces the FRD's "no form-action" assertion).** |
| AC-012 | AD-004 | GO `TestHARDEN002_AC012_CSPPermitsSameHostWebSockets`: host `rIOt.lan:7331`; `ws://…/ws` and `wss://…/ws/terminal/x/y` match a host-source; `'self'` present. **GO `TestHARDEN002_AC012_C6_HostInjectionFallsBackToSelf`**: a Host containing any one of `;` `,` `'` `*` `(` `)` `!` `$` `&` `+` `=` `%`, a space, an empty Host, or a 263-byte host → the `connect-src` directive equals exactly `connect-src 'self'`. A 262-byte valid host → its ws/wss sources are present (boundary). |
| AC-013 | AD-003 | GO `TestHARDEN002_AC013_HSTSOnDirectTLS` |
| AC-014 | AD-003 | GO `TestHARDEN002_AC014_HSTSBehindTrustedProxyMatchesSecureCookie` |
| AC-015 | AD-003 | GO `TestHARDEN002_AC015_NoHSTSOnPlainHTTPOrUntrustedPeer` |
| AC-016 | AD-003 + T-3 | GO `TestHARDEN002_AC016_OIDCRedirectsUnaffectedByHeaders`. The OIDC start and callback redirect exactly as before, while carrying the headers, including the CSP with `form-action 'self'`. Anchor navigation is not governed by `form-action`. |
| AC-017 | AD-003 | GO `TestHARDEN002_AC017_WebSocketUpgradesThroughMiddleware` |
| AC-018 | AD-005 | GO `TestHARDEN002_AC018_StaticAssetContentTypes` |
| AC-019 | AD-007 | GO `TestHARDEN002_AC019_SMTPPasswordRedactedOnList`. **GO `TestHARDEN002_AC019_C1_URLUserinfoRedactedOnList`**: ntfy `server_url: https://alice:S3cret@ntfy.home` and gotify `server_url: https://bob:Pw2@gotify.home` → the body contains neither `S3cret`, `Pw2`, `alice:` nor `bob:`. `server_url` equals `https://[rIOt-redacted]@ntfy.home`. The host is visible. |
| AC-020 | AD-006/007 | GO `TestHARDEN002_AC020_EveryRegistryTypeRedacted` |
| AC-021 | AD-007 | GO `TestHARDEN002_AC021_WebhookURLAndHeaderValuesRedacted` |
| AC-022 | AD-007 | GO `TestHARDEN002_AC022_UnsetSecretsNotSentinel` |
| AC-023 | AD-007 | GO `TestHARDEN002_AC023_UnknownTypeFullyRedacted` |
| AC-024 | AD-007 | GO `TestHARDEN002_AC024_CreateStoresCleartextReturnsRedacted`. **GO `TestHARDEN002_AC024_C1_CreateAndUpdateResponsesRedactUserinfo`**: the POST and PUT response bodies for an ntfy channel with a userinfo `server_url` do not contain the password. The repository holds the cleartext. |
| AC-025 | AD-007 | GO `TestHARDEN002_AC025_PutSentinelKeepsSecret`. **GO `TestHARDEN002_AC025_C1_NameOnlyPutPreservesUserinfo`**: a stored `server_url: https://alice:S3cret@ntfy.home`, then a PUT changing only `name` that sends `server_url` exactly as listed (`https://[rIOt-redacted]@ntfy.home`) → the stored value is `https://alice:S3cret@ntfy.home`. |
| AC-026 | AD-007 | GO `TestHARDEN002_AC026_PutOmittedKeepsSecret` |
| AC-027 | AD-007 | GO `TestHARDEN002_AC027_PutNewValueReplaces` |
| AC-028 | AD-007 | GO `TestHARDEN002_AC028_PutEmptyClears` |
| AC-029 | AD-007 | GO `TestHARDEN002_AC029_SentinelNeverPersisted` (a), (b), (c). **GO `TestHARDEN002_AC029_C1_UserinfoSentinelNeverPersisted`**: (a) stored `server_url` has no userinfo and the PUT sends the sentinel userinfo → stored as `https://ntfy.home`; (b) POST with the sentinel userinfo → stripped; (c) a type change with the sentinel userinfo → stripped. Every stored string is checked: none equals the sentinel, and no userinfo equals the sentinel. |
| AC-030 | AD-007 | GO `TestHARDEN002_AC030_WebhookHeaderMerge` |
| AC-031 | AD-007 | GO `TestHARDEN002_AC031_NonSecretKeysReplaced` |
| AC-032 | AD-007 | GO `TestHARDEN002_AC032_PutUnknownChannel404` (`UpdateChannelCalls == 0`) |
| AC-033 | AD-007 | GO `TestHARDEN002_AC033_TestSendUsesStoredSecretAfterRedactedEdit` |
| AC-034 | AD-008 | GO `TestHARDEN002_AC034_TestSendErrorScrubbed` (telegram). **GO `TestHARDEN002_AC034_C2_TestSendRealClientErrorsScrubbed`**: a real `notify.NewDispatcher` with `SetHTTPClient(&http.Client{Transport: erroringRoundTripper})`, driven through `POST …/test`. The table and its assertions are listed below this one. |
| AC-035 | AD-008 | GO `dispatcher_test.go` `TestHARDEN002_AC035_DispatchFailureLogsScrubbed` (mock-channel baseline). **GO `scrub_client_test.go` `TestHARDEN002_AC035_C2_RealClientErrorsScrubbed`**: the same 6-backend table as AC-034 C2, run through `Dispatch` (check `notifyRepo.Logs[i].ErrorMsg` and the captured slog buffer), `SendToChannel` (check the return value, `ErrorMsg` and slog) and `TestChannel` (check the return value). Each asserts the absence of the backend's secrets and the presence of `[rIOt-redacted]`. **GO `secrets_test.go` `TestHARDEN002_C2_ScrubSecretsMatchesStdlibForms`**: pure. The `stripPassword` output (`user:***@`), `u.Redacted()`, and the `strconv.Quote` interior of a secret containing `"` and `\` are all replaced. |
| AC-036 | AD-009 | VT `[HARDEN-002][AC-036] saving after a name-only edit submits password as the sentinel, never ""` |
| AC-037 | AD-009 | VT `[HARDEN-002][AC-037] redacted bot_token shows "Configured (unchanged)", is not editable text, and typing NEWTOKEN submits it`. **VT `[HARDEN-002][AC-037][C3] secret inputs opt out of browser credential autofill`** (assertions listed below this table). |
| AC-038 | AD-009 | VT `[HARDEN-002][AC-038] Clear on ntfy token submits token ""` |
| AC-039 | — | VT `[HARDEN-002][AC-039] Test calls testNotificationChannel with the id only` |
| AC-040 | AD-009 | VT `[HARDEN-002][AC-040] demo channels expose only sentinel or empty secrets` |
| AC-041 | AD-010 | GO `TestHARDEN002_AC041_CorrectKeyRegisters` |
| AC-042 | AD-010 | GO `TestHARDEN002_AC042_WrongKeysRejectedIdentically` |
| AC-043 | AD-010 | GO `TestHARDEN002_AC043_OpenRegistrationUnchanged` |
| AC-044 | AD-010 | GO `TestHARDEN002_AC044_RegistrationKeyMatchesTruthTable`. REVIEW: uses `crypto/subtle` over digests, with no `==` on the key strings. |
| AC-045 | AD-011 | GO `TestHARDEN002_AC045_HealthOK` |
| AC-046 | AD-011 | GO `TestHARDEN002_AC046_HealthDegradedReturns503` |
| AC-047 | AD-011 | GO `TestHARDEN002_AC047_ReadyzReady` |
| AC-048 | AD-011 | GO `TestHARDEN002_AC048_ReadyzDBDown503` |
| AC-049 | AD-011 | GO `TestHARDEN002_AC049_ReadyzMigrationsIncomplete503` |
| AC-050 | AD-011 | GO `TestHARDEN002_AC050_HealthEndpointsPublicAndSetupIndependent`, plus the `setup_test.go` case for `/readyz` |
| AC-051 | AD-011 | GO `TestHARDEN002_AC051_HungProbeBounded` |
| AC-052 | AD-011 | GO `TestHARDEN002_AC052_HealthBodiesLeakNothing` |
| AC-053 | existing `checkServer` | GO `TestHARDEN002_AC053_DoctorServerCheckHandles503` |
| AC-054 | AD-012 | GO `TestHARDEN002_AC054_ReadmeHasNoRetryClaim` |
| AC-055 | AD-012 | GO `TestHARDEN002_AC055_RetryQueueRemoved` |
| AC-056 to AC-061 | N/A (OQ-1) | none |
| AC-062 | AD-003/010 | The existing agent-route suites pass unmodified |
| AC-063 | All | `go build ./...`, `go vet ./...`, `go test ./...`, `npm run test:run` |
| AC-064 | §12.3 | GO **`TestHARDEN002_AC064_DocsUpdated`** (checks extended). The README contains `RIOT_PPROF_ADDR`, `docker exec`, `/readyz`, `503`, `write-only`, `frame-ancestors`, `form-action`, `Strict-Transport-Security`, `every port` and `edit channels`. The CHANGELOG contains `[HARDEN-002]`. Plus a REVIEW pass. |
| (C5(b)) | AD-001 | **GO `invariants_test.go` `TestHARDEN002_C5_NoNilHandlerOrDefaultServeMux`**: scans non-test `*.go` under `cmd/` and `internal/`. Asserts no `http.DefaultServeMux`, `http.ListenAndServe(`, `http.ListenAndServeTLS(` or `http.Serve(`. Asserts every `&http.Server{` literal contains `Handler:` before its closing brace. Plus REVIEW (§13). |

**AC-034 C2 table and assertions.** Each row gives the backend config and the strings that
must not appear in the response `error`.

| Backend | Config | Must not appear in `error` |
|---|---|---|
| telegram | `bot_token: 999:TGTOKEN` | `TGTOKEN` |
| discord | `webhook_url: https://u1:DPASS@discord.example/api/webhooks/1/DTOK` | `DTOK`, `DPASS` |
| slack | `webhook_url: https://hooks.slack.example/services/STOK` | `STOK` |
| ntfy | `server_url: https://alice:NPASS@ntfy.home`, `topic: NTOPIC`, `token: NBEARER` | `NPASS`, `NTOPIC`, `alice` |
| gotify | `server_url: https://bob:GPASS@gotify.home`, `app_token: GTOK` | `GPASS`, `GTOK`, `bob` |
| webhook | `url: https://riot:WPASS@n8n.home/webhook/WTOK`, `headers: {Authorization: "Bearer WHDR"}` | `WPASS`, `WTOK` |

For every row, the response `error` must also contain `[rIOt-redacted]`.

**AC-037 C3 assertions.** For the smtp, ntfy, telegram, pushover and gotify editors:
- Every `input[type=password]` has `autocomplete="new-password"`, `data-1p-ignore`,
  `data-lpignore="true"`, `data-bwignore="true"`, `data-form-type="other"`, and a `name`
  starting with `riot-secret-`.
- Every non-masked `SecretInput` and the SMTP `username` have `autocomplete="off"`.
- No editor input is inside a `<form>`.

---

## 9. Error Handling

| Failure | Behavior | Status / shape | Logging |
|---|---|---|---|
| `RIOT_PPROF_ADDR` invalid (including a non-canonical port) | Listener not started; server continues | n/a | ERROR `rejected RIOT_PPROF_ADDR; pprof disabled` with `value` and `reason` |
| pprof bind fails | Listener not started; server continues | n/a | ERROR `pprof listener bind failed; pprof disabled` with `addr` and `error` |
| pprof `Serve` exits unexpectedly | Goroutine exits; main server unaffected | n/a | ERROR `pprof listener stopped` with `error` |
| pprof `Shutdown` error | Ignored; main shutdown proceeds | n/a | WARN `pprof shutdown` with `error` |
| Main-router `/debug/pprof*` | Explicit 404 | `404 {"error":"not found"}` | request log only |
| DB unreachable or probe timeout | `database:false` | `/health` 503 `degraded`; `/readyz` 503 `not_ready` | none per request |
| Host fails CSP validation | Directive becomes `connect-src 'self'` | unchanged | none |
| PUT to an unknown channel ID, or any `GetChannel` error | No write | `404 {"error":"channel not found"}` | none |
| PUT or POST with an invalid body | unchanged | `400 {"error":"invalid request body"}` | unchanged |
| Repo create or update fails | unchanged | `500 {"error":"failed to create/update channel"}` | unchanged |
| Submitted userinfo is the sentinel but nothing is stored to keep | Userinfo stripped | normal 200/201 | none |
| Channel send fails | Scrubbed everywhere | Test-send: `200 {"success":false,"error":"<scrubbed>"}` | `slog.Error("dispatch: send failed", …, "error", <scrubbed>)`; `error_msg = <scrubbed>` |
| Registration key mismatch | unchanged | `401 {"error":"invalid registration key"}` | none |
| Handler panic | Recoverer handles it; headers already set | `500` | unchanged |

---

## 10. Security Considerations

### pprof (F-1)
- The main router has no pprof code path. Path variants reach the SPA, never pprof (C5(a)).
- Nothing serves `DefaultServeMux`, and no `http.Server` has a nil `Handler`. This is pinned
  by an automated test and a REVIEW gate (C5(b)).
- The opt-in listener accepts only a literal loopback IP and a canonical port, and it binds
  only the parsed values.
- In Docker with bridge networking, reaching it needs `docker exec`, which is
  root-equivalent.
- **Residual (SEC-007):** under `network_mode: host`, the listener binds the host's
  loopback. Any local user could then read profiles and hold 30 s profiles open. This is
  accepted because the listener is opt-in and off by default. The README notes it.

### Clickjacking
- `frame-ancestors 'none'` and `X-Frame-Options: DENY`. No framing consumer exists.

### Script-injection containment
- `script-src 'self'`.
- Exfiltration channels are closed:
  - `img-src` and `font-src` are `'self' data:`.
  - `connect-src` allows only `'self'` plus same-host WebSockets.
  - `form-action 'self'` (rev 2) blocks posting a hijacked form off-origin.
  - `object-src 'none'` and `base-uri 'self'`.
- **Residual:** `style-src 'unsafe-inline'` permits in-page CSS redressing only. CSS cannot
  fetch off-origin under this policy.

### Host reflection in the CSP
- The reflected value is equivalent to `'self'`.
- The strict byte set blocks every policy-altering byte, and C6 pins it.
- There is no shared cache, so a spoofed Host cannot poison another user.

### HSTS
- It uses the single `IsHTTPS` predicate, shared with the cookie `Secure` flag.
- Untrusted `X-Forwarded-Proto` is ignored.
- No `includeSubDomains` and no `preload`.
- **SEC-004:** once a browser pins a **hostname**, HSTS applies to every port on that host,
  and certificate errors become non-bypassable for a year. A server reached by IP literal is
  unaffected, because browsers ignore HSTS there. The README states the hostname caveat.

### Notification secrets: write-only, and its limits (C4 / SEC-002)

**What write-only protects against.** Disclosure through:
- API responses, including URL userinfo (C1)
- error text, log lines and `notification_log` (C2)
- browser and devtools caches
- a passive reader of admin responses, which is the FRD F-3 actor

**What it does not protect against:**
1. **An actor who can edit channels.** A holder of an admin session, through a stolen cookie
   or full XSS, can re-point a destination while the sentinel keeps the stored secret, then
   test-send to harvest it. This is destination rebinding. Examples:
   - gotify `server_url` + `app_token`
   - ntfy `server_url` + `token` / `topic`
   - smtp `host` + `password`
   - webhook `url` + `headers`
   - a userinfo credential kept while its host is edited

   Such an actor already has fleet-wide command execution and container terminals. The
   database, which holds these secrets in plaintext, is reachable that way. So this is a
   residual, not a regression. Destination binding is follow-up **F-7**. This ADD, the FRD
   and the README make no claim of protection against an active admin-session holder.
2. **Secrets at rest.** They are plaintext in Postgres and in pg_dump backups (FRD §9 item 2).

**Other residuals:**
- `notification_log` rows (90-day retention) and DB server-log rows (7-day retention)
  written before deploy may still contain leaked tokens.
- Partially echoed or transformed secrets in remote error bodies are not detected, and those
  bodies are stored unbounded (SEC-010, F-12).

### Browser autofill (C3 / SEC-003)
- Secret inputs opt out of credential autofill and saving, and the editor is not a `<form>`.
- Browser heuristics vary, so a manual Chrome check is required after deploy (§13).

### Registration key timing (F-4)
- Constant-time comparison over digests.
- With no key set, registration is open and the comparison is dormant (unchanged FR-034 behavior).

### OQ-8 audit
- No other raw secret `==` or `!=` comparison exists in `internal/server`.
- Device and bootstrap keys are looked up by hash, the admin password uses bcrypt, and JWTs
  are verified by the library.

### Health endpoints
- They return booleans, a status word and a timestamp only.
- Each request costs one DB ping (SEC-006, F-10).

---

## 11. Performance Considerations

- **Security headers:** five `Header().Set` calls and one concatenation per request, with no
  regexp (NFR-004).
- **Health probe:** one goroutine and one buffered channel per request, bounded at 2 s.
- **Redaction:** O(number of keys) per channel, plus one `splitURLUserinfo` string scan per
  top-level string value. Channel lists are tiny.
- **Scrubbing:** runs only on the failure path. Roughly 10 variants per secret.
- **Static assets:** one map lookup.
- No new indexes, caches or rate limits.

---

## 12. Implementation Notes for Engineers

### 12.1 Server / Go

- **Never** serve `http.DefaultServeMux`. `listenPprof` must use `newPprofMux()`. Every
  `http.Server` literal must set `Handler`. `TestHARDEN002_C5_NoNilHandlerOrDefaultServeMux`
  enforces this.
- Register the `/debug/pprof` and `/debug/pprof/*` routes before `serveFrontend`, using
  `r.Handle`.
- `SecurityHeaders` sets headers before `next` and never wraps `w`. It must stay second in
  `globalMiddleware()`.
- Replace all four `RequestScheme(r) == "https"` cookie sites with `middleware.IsHTTPS(r)`.
- Guard the DB with `if s.DB != nil { hh.DB = s.DB }`. Never assign a possibly-nil `*db.DB`
  to the interface field.
- `Health` and `Readyz` must use the fixed body structs.
- `MergeSecrets` and `RedactChannel` must deep-copy their maps. The mock repo shares maps.
- **`splitURLUserinfo` is pure string handling.** Do not route sentinel-bearing values
  through `url.Parse`, because `https://[rIOt-redacted]@h` fails to parse. Use
  `strings.LastIndex` for the `@` within the authority, the same as `net/url` does.
- **The `stripPassword` variant must copy net/http exactly:**
  `strings.Replace(u.String(), u.User.String()+"@", u.User.Username()+":***@", 1)`, and only
  when `u.User.Password()` reports a password set. Do not build it with `url.UserPassword`,
  which would escape `*` as `%2A`.
- For the `%q` variant, take `strconv.Quote(w)` and strip the first and last byte.
- `scrubError` uses `errors.New`. Do not use `%w` and do not add `Unwrap`.
  `UnsupportedChannelError` paths stay unwrapped.
- `UpdateNotificationChannel` must run the 404 check before `UpdateChannel`.
- `ParsePprofAddr` must use the exact host rule `a.Is4() && a.IsLoopback() || a == netip.IPv6Loopback()`
  with `a.Zone() == ""`, and the canonical-port byte check before `strconv.Atoi`. Build the
  address from `a.String()` and `strconv.Itoa(port)`.
- In tests, set `ProbeTimeout` small.
- Tests that capture slog swap `slog.Default()` and must not run in parallel. pprof functions
  take an explicit logger instead.
- The C2 tests use `type rtFunc func(*http.Request) (*http.Response, error)` with a
  `RoundTrip` method that returns `nil, errors.New("dial tcp: connection refused")`. Install
  it with `d.SetHTTPClient(&http.Client{Transport: rtFunc(...)})`. The real client then wraps
  the error in a `*url.Error` with the `***` and `%q` URL form. Never hand-build the error
  string.
- The AC-017 `/ws` test needs a matching `Origin` and a valid `riot_session` JWT.
- `docs_test.go` and `invariants_test.go` resolve paths relative to `internal/server`.
- The sentinel parity check reads `../../../web/src/utils/secrets.ts`.
- No new module. Everything comes from the standard library or the existing `go.mod`.

### 12.2 Frontend

- Import `REDACTED_SECRET` from `web/src/utils/secrets.ts` only.
- `SecretInput` sets `aria-label`, plus the C3 attribute set from AD-009.
- Do not wrap the editor in a `<form>`.
- `configured(key)` is false for new channels and after a type change.
- Do not add a headers editor.
- Follow the vitest mocking pattern from `AlertRuleSettings.test.tsx`.
- Keep `scrollbar-thin` on the modal body.

### 12.3 Documentation (FR-043, FR-054, C4, C7/SEC-004)

**README.md**
- Line 37: drop ", and automatic retry queue".
- Delete the "Notification Retry Queue" subsection at L996–998.
- **Env table:** add `RIOT_PPROF_ADDR`, with default "— (disabled)" and the description
  "Loopback-only pprof listener, for example `127.0.0.1:6060`. Hostnames, non-loopback
  addresses and non-canonical ports are rejected".
- **New "Profiling (pprof)" subsection:**
  - Off by default, loopback literal only.
  - Reach it with `docker exec riot-server wget -qO- http://127.0.0.1:6060/debug/pprof/heap > heap.pb.gz`.
  - A Docker port mapping cannot reach it.
  - `/debug/pprof` on `:7331` always returns 404.
  - Under `network_mode: host` it is reachable from the host's loopback.
- **API table:** `/health` returns 200, or 503 when degraded. Add a `/readyz` row.
- **Notification Channels, new "Secrets are write-only" paragraph:**
  - Which keys are secret (T-1, including the ntfy topic), plus credentials embedded in any
    URL, which show as `https://[rIOt-redacted]@host`.
  - The UI shows "Configured (unchanged)". Leave a field blank to keep the stored value.
  - Clear is available for the ntfy token and the SMTP password.
  - Test-send uses the stored value, and error text is scrubbed.
  - **Limits (C4):** write-only protects against disclosure through responses, logs and
    caches. It does **not** protect against anyone who can edit channels, who could re-point
    a channel's destination and send the stored secret there. It does not protect secrets at
    rest in the database or in backups.
- **"Security headers" paragraph in the reverse-proxy section:**
  - CSP, including `form-action 'self'`. `X-Frame-Options: DENY` and
    `frame-ancestors 'none'` mean the dashboard cannot be embedded in an iframe.
  - `Strict-Transport-Security: max-age=31536000` is sent on direct TLS, and behind a trusted
    proxy that sends `X-Forwarded-Proto: https`. The rule is the same one that decides the
    `Secure` cookie flag.
  - Browsers ignore HSTS for IP-address hosts.
  - **SEC-004:** once a browser pins a hostname, HSTS applies to every port on that host, and
    certificate errors become non-bypassable for a year.

**CHANGELOG.md `[Unreleased]`:** one `[HARDEN-002]` bullet for each of:
- the pprof removal and `RIOT_PPROF_ADDR`
- the security headers, `form-action` and the iframe denial
- write-only notification secrets (including URL userinfo), the PUT 404, and error scrubbing
- the constant-time registration key
- `/health` returning 503, and the new `/readyz`
- removal of the retry-queue claim and its dead code

### 12.4 Follow-ups logged (not in this story)

- **F-1 (OQ-5).** `GET /api/v1/settings/registration` returns the key in cleartext. Make it
  write-only.
- **F-2 (OQ-7). CLOSED in rev 2.** `form-action 'self'` has been added (AD-004, SEC-008).
- **F-3 (security).** `RIOT_API_KEY` sets `Config.RegistrationKey`, but `RegisterDevice`
  reads only the DB key, so the README L139 claim is false. Enforce the env value with
  precedence, and make a key read error fail closed (P-2).
- **F-4 (OQ-8).** Audit complete (§10). Nothing beyond F-3.
- **F-5 (low).** `http.FileServer` lists embedded directories (`GET /assets/`).
- **F-6 (ops).** Optionally clear `notification_log.error_msg` and server-log rows written
  before deploy.
- **F-7 (SEC-002, destination binding).** A stored secret may be sent only to the destination
  it was configured with. A PUT that changes a destination-defining field without
  resubmitting the dependent secrets must be **rejected**. It must not silently keep them,
  and it must not silently wipe them (that would break BR-003). The pairs are:
  - gotify: `server_url` → `app_token` and `server_url` userinfo
  - ntfy: `server_url` → `token`, `topic` and userinfo
  - smtp: `host`/`port` → `password`
  - webhook: `url` host → `headers`
  - any config URL whose host changes while its userinfo is kept
- **F-8 (P-1, ELEVATED priority).** With registration open (no registration key
  configured, the default), `POST /api/v1/devices/register` with an existing device UUID and **no**
  `X-rIOt-Key` header causes two problems:
  - It mints an **additional** API key for that device, through `StoreAPIKey`, which
    INSERTs. The real key keeps working, so nothing alerts.
  - It overwrites the device's hostname, tags and hardware profile.

  Device UUIDs are not secret. With mTLS enabled, the minted key alone cannot pass
  device routes or `/ws/agent`, which also require the client certificate. Impersonation is
  therefore mitigated, but key minting and metadata tampering remain. Required property:
  re-registering an existing device must prove possession of that device's current API key
  or client certificate. **Out of scope for HARDEN-002** (FR-034 freezes `RegisterDevice`).
  **Caution for any interim mitigation:** do not simply set a registration key on an
  existing fleet first. `RegisterDevice` compares `X-rIOt-Key` against the registration key *before* the
  re-registration branch, which also reads `X-rIOt-Key` to decide on key issuance. Confirm
  what released agents send on re-registration, or a fleet-wide 401 could follow.
- **F-9 (P-2).** Registration fails open when reading the key fails (`regKey, _ :=`). Fold
  this into F-3.
- **F-10 (SEC-006).** Unauthenticated `/health` and `/readyz` map one-to-one onto DB pings.
  Consider caching the probe result briefly (for example 1 s) or rate-limiting.
- **F-11 (SEC-009).** A test that enumerates every `ch.Config["…"]` key each backend reads,
  and fails when a key is classified as neither secret nor non-secret.
- **F-12 (SEC-010).** Cap the size of remote error bodies in the `discord` and `telegram`
  errors.

### 12.5 Deployment preconditions

| Precondition | Consequence for this story |
|---|---|
| `RIOT_TRUSTED_PROXIES` is **unset** when there is no reverse proxy | `RealIP` trusts nobody. HSTS and the cookie `Secure` flag are decided by `r.TLS` alone, and `X-Forwarded-*` headers are ignored. **It must stay unset** while there is no proxy. Any value would let the listed peers suppress HSTS and `Secure` and spoof the client IP (C7). |
| riot-server terminates TLS itself (DB-stored or self-signed cert) | Every response carries `Strict-Transport-Security: max-age=31536000`. When the server is reached by IP literal, browsers ignore it. If the server is later reached by a hostname with a trusted certificate, the SEC-004 caveat applies (README). |
| mTLS enabled | Enrollment and renewal routes and the cert settings routes are registered. Device routes and `/ws/agent` require the client certificate plus the API key. The new middleware is global and does not touch these routes. Router test harness (a) runs with mTLS disabled (no CA), which does not affect header coverage. Post-deploy check: at least one agent still heartbeats and its terminal connects. |
| No `registration_key` configured (registration open, the default) | The `registrationKeyMatches` path (AD-010) is dormant until a key is set. The empty-key bypass is unchanged FR-034 behavior. P-1 is elevated to F-8. F-3 (`RIOT_API_KEY` not enforced) also applies. |
| A healthcheck or autoheal pointed at `/health` (OQ-4) | It will see 503 while the DB is down and may restart the container. The shipped Dockerfile and compose do not do this. |
| The dashboard is framed by another site (OQ-2) | Blocked by `frame-ancestors 'none'`; no known deployment does this. |

---

## 13. Definition of Done

- [ ] All Section 4 changes are implemented, including both DELETEs. No file in §4.1 is
      touched.
- [ ] Every in-scope AC and condition in Section 8 has its named test. AC-008 and AC-056 to
      AC-061 are recorded as N/A.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green. Use `-race` on Linux CI;
      skip it on Windows.
- [ ] `cd web && npm run test:run` is green. No new npm or Go dependency.
- [ ] **REVIEW gate (C5(b)):** no `http.Server` in the tree has a nil `Handler`, and nothing
      calls `http.ListenAndServe(…)`, `http.ListenAndServeTLS(…)` or `http.Serve(…)`, or
      references `http.DefaultServeMux`. `TestHARDEN002_C5_NoNilHandlerOrDefaultServeMux`
      passes.
- [ ] `grep -rn 'RequestScheme(r) == "https"' internal/` returns nothing.
- [ ] `grep -rn 'RetryQueue' --include=*.go .` returns nothing.
- [ ] `grep -rn 'debug/pprof' internal/server/router.go` shows only the 404 routes.
- [ ] `ParsePprofAddr` uses no `a.IsLoopback()` shortcut for IPv6, and `startPprof` binds
      only the parsed address.
- [ ] No migration. Existing agent API fixtures are unmodified.
- [ ] README and CHANGELOG are updated per §12.3, including the C4 limits and the SEC-004
      HSTS caveat. `TestHARDEN002_AC054`, `AC055` and `AC064` pass.
- [ ] No new lint or type errors (`go vet`; `npm run build` for `tsc`).
- [ ] **Pre-deploy (orchestrator):** confirm `RIOT_TRUSTED_PROXIES` is still unset on the deployed server
      (§12.5).
- [ ] **Post-deploy (orchestrator, FRD §8.8 plus the review):**
  - `curl -ik https://<server>:7331/debug/pprof/` returns 404.
  - `curl -Ik https://<server>:7331/` shows CSP (with `form-action 'self'`), XFO,
    nosniff, Referrer-Policy and HSTS.
  - `/health` and `/readyz` return 200.
  - Browser devtools show zero CSP violations on login (password and OIDC round trip),
    device page, container terminal, settings and charts.
  - An agent still heartbeats under mTLS.
  - Notification channels show "Configured (unchanged)", and test-send succeeds.
  - **C3 manual check:** in Chrome with a saved rIOt login, open each masked channel type
    (smtp, ntfy, telegram, pushover, gotify). Confirm no field is prefilled and no
    save-password prompt appears after saving.
