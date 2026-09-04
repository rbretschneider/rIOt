# Architecture Decision Document

| Field         | Value                                 |
|---------------|---------------------------------------|
| Story ID      | OIDC-001                              |
| FRD Reference | docs/requirements/OIDC-001-frd.md     |
| Author        | Architect Agent                       |
| Date          | 2026-09-04                            |
| Revision      | 4                                     |
| Status        | FINAL                                 |

## Revision log

| Rev | Date | Change |
|---|---|---|
| 1 | 2026-09-04 | Initial FINAL. AD-001 – AD-019. |
| 2 | 2026-09-04 | Revised against `docs/security/OIDC-001-security-review.md` (verdict REVISE). **SEC-001** → AD-008 and §6.3: `riot_session` gains scheme-derived `Secure` at both mint sites. **SEC-002** → new **AD-020** (trusted-proxy client identity, `RIOT_TRUSTED_PROXIES`, `chimw.RealIP` removed); AD-013 and §10 rewritten to depend on it; covers the pre-existing `loginLimiter` and `registerLimiter` as well as the new `oidcLimiter`. **SEC-003** → AD-016 (`RecordLogin` reports insert-vs-update via `RETURNING (xmax = 0)`), new **AD-021** (first admission emits a `WARN`), new §4.5 (operator-documentation requirements, `register_oidc_app.py` named). **SEC-004** → AD-005: transaction MAC uses a domain-separated derived key. **SEC-005** → AD-005 and §10 login-CSRF rationale corrected. **SEC-006** → AD-005 records the delegated single-use property + a LIVE QA step. **SEC-007** → AD-002 security floors + `govulncheck` in §13. **SEC-008** → §4.5 redirect-URI registration requirements. **SEC-011** → AD-019 switches to exact path matching. SEC-009/SEC-010/SEC-012/SEC-013/SEC-014 acknowledged in §10 and §12 as out of scope and explicitly not to be fixed inline. New §8.1 maps the security-review conditions to tests. No AC weakened; two FRD amendments recorded in §0. |
| 3 | 2026-09-04 | Documentation-only pass closing re-review conditions **C1**, **C2**, **C3** and **C6** (verdict on rev 2: APPROVED WITH CONDITIONS). §4.5 items 3 and 5 rewritten for the updated `D:\Repos\Hawaii\scripts\register_oidc_app.py`, which now supports `--redirect-uri`, `--launch-url`, and `--group` (creates the group if missing **and** creates the `PolicyBinding`, warning loudly when omitted) and PATCHes an existing provider's `redirect_uris` on re-run; the stale "creates no policy binding" and "adding that flag is a Hawaii-repo task" statements are removed, the runbook now carries the exact one-liner, and the append-not-replace behaviour of the re-run PATCH is recorded. **C6** → AD-020's "single place" claim narrowed to client identity and request scheme, with `handlers/setup.go`'s direct `X-Real-Ip` reads recorded as a known residual carried to the follow-up story. New §14 collects the deferred items. No decision, mechanism, contract, AC, or test mapping changed. |
| 4 | 2026-09-04 | Closes the last two re-review conditions. **C5** → AD-020 step 2 now specifies the all-trusted `X-Forwarded-For` chain: fall back to the immediate peer's `RemoteAddr`. Rationale recorded; the case added to §4.1 and §8.1's `clientip_test.go` rows. **C4** → §4.5 item 6 and the `.env.example` row in §4.4 now lead with the `Secure`-attribute consequence (behind a TLS-terminating proxy with `RIOT_TRUSTED_PROXIES` unset, `RequestScheme` resolves `http` and `riot_session` ships without `Secure`), with rate-limit granularity and audit IPs as the secondary reasons; AD-020's consequences bullet updated to match. All re-review conditions C1–C6 are now closed. No decision reversed, no AC changed. |

---

## 0. FRD review outcome and recorded amendments

No questions for the Business Developer. Three corrections and two flagged amendments.

**Corrections**

1. **FRD §12 is stale.** It records "no IdP is deployed on hawaii yet". authentik 2026.8.1
   is live at `https://auth.rbretschneider.com` on the production host. Per-application
   issuer shape `https://auth.rbretschneider.com/application/o/<slug>/`, RS256, strict
   redirect matching. Automated ACs are still satisfied against a stubbed issuer (§8); a
   live end-to-end run against authentik is now a real, non-blocked QA step (§8, LIVE rows).
2. **FRD §12 "Login throttling policy — Verify".** Verified: `POST /api/v1/auth/login`
   *is* throttled — `internal/server/router.go:73,86` applies
   `middleware.NewRateLimiter(5, 5)` (5/min, burst 5) via `loginLimiter`. NFR-008 therefore
   means "match 5/min burst 5, keyed on IP". The policy is reused unchanged; the *key
   derivation* is fixed by AD-020 (see amendment 2 below).
3. **FRD §2.2 item 1 and A-6.** Confirmed: there is no `/login` route
   (`web/src/App.tsx:302`). Landing target decided in AD-014: `/?sso_error=<code>`.

### Amendment 1 — FR-031 is amended by AD-008 (cookie attributes)

FR-031 requires the response contract of `POST /api/v1/auth/login` and
`POST /api/v1/auth/logout` to be *unchanged*. AD-008 changes two attributes of the
`riot_session` cookie on those responses: `SameSite` `Strict` → `Lax`, and the addition of
`Secure` when the request scheme resolves to `https`.

- The `Lax` change is the sanctioned fix for the FR-036/AC-009 hazard the FRD itself raised
  in §2.2 item 2, and is house standard `app_standards.md` §4 rule 5.
- The `Secure` addition is required by security finding **SEC-001**: `Strict` was, as a side
  effect, suppressing the cookie on attacker-initiated top-level navigations, including
  scheme downgrades to `http://<same-host>/…`. Shipping `Lax` without `Secure` would be a net
  weakening of an existing control. The same house rule pairs the two attributes for exactly
  this reason.

Status codes, bodies, cookie name, value, claims, signing key, lifetime, `Path`, and
`HttpOnly` are unchanged. AC-008 and AC-022 remain satisfiable because password login and SSO
login continue to emit identical cookie attributes to each other — guaranteed by the single
shared helper (AD-008). §9 item 12 is not violated: it forbids changing the session's
lifetime, claims, signing algorithm, and throttling policy, none of which change.

Engineering must treat FR-031 as amended here and not as a blocker.

### Amendment 2 — §9 item 12 and NFR-008 are amended by AD-020 (client-IP derivation)

§9 item 12 places "changing the … throttling policy of the existing password login" out of
scope. AD-020 does **not** change any throttling policy — `loginLimiter` and
`registerLimiter` keep their exact rates and bursts (5/min burst 5, 10/min burst 10). It
changes how the *client IP* used as the bucket key is derived, and it does so for the whole
application at once by replacing the global `chimw.RealIP` middleware.

This is unavoidable: security finding **SEC-002** establishes that `chimw.RealIP` runs with
no trusted-proxy configuration, so in rIOt's documented no-proxy deployment (D-8, NFR-010)
the throttle key is a client-supplied header. NFR-008 and AC-027 specify an IP-keyed
throttle; satisfying them only against a unit test that manipulates `RemoteAddr` would ship a
control that QA signs off and that does nothing in production. The same weakness also voids
AD-013's separate-bucket reasoning (an attacker can empty the operator's password bucket by
spoofing their IP) and makes NFR-009's logged client IP forgeable.

The fix cannot be scoped to the OIDC endpoints alone, because the header rewrite happens in
global middleware before any route is chosen. Fixing it only for `oidcLimiter` would leave a
misleading half-control in place.

FR-008 is not violated: `RIOT_TRUSTED_PROXIES` is optional and defaults to empty (trust
nobody), so an existing `.env` continues to work. AC-030 is unaffected.

**Operational consequence that must reach the operator docs (§4.5):** a deployment behind a
reverse proxy that previously got per-client throttling for free from `chimw.RealIP` will,
after this change, see every client keyed to the proxy's address until
`RIOT_TRUSTED_PROXIES` is set — and, more importantly, will not get the `Secure` attribute on
`riot_session` until it is set. That is the secure default and the reason the variable exists.

---

## 1. Summary

Add a new `internal/server/oidc` package that owns Authorization Code + PKCE login against a
single OIDC issuer, driven entirely by four `RIOT_OIDC_*` environment variables and dormant
when they are absent. Three new browser-facing routes under the existing `/api/v1/auth`
prefix (`/oidc`, `/oidc/start`, `/oidc/callback`) sit on the existing chi router and the
existing `*Handlers` receiver; on a validated callback the handler calls the *same* session
minting helper that `POST /api/v1/auth/login` uses, so an SSO session and a password session
are identical by construction. Identity is federated only: no IdP token is stored, and the
sole persistence is a single additive audit table (`external_identities`) written
best-effort, whose first sighting of an identity raises a persisted `WARN`. The React login
screen gains a TanStack-Query availability probe and a plain anchor button. Two
application-wide security controls are corrected as part of the story because the story
depends on them: the `riot_session` cookie moves to `SameSite=Lax` **with** scheme-derived
`Secure`, and client-IP derivation moves from unconditional header trust to an explicit
trusted-proxy allowlist.

---

## 2. Technical Context

### 2.1 What exists

**Server (Go 1.24, chi v5, pgx v5, slog, golang-migrate)**

- `internal/server/config.go` — `LoadConfig()` reads every `RIOT_*` env var into `*Config`.
  This is the single place env is read; it never aborts boot on bad input, it logs and falls
  back (`slog.Error` on a bad admin password, `slog.Info` on a generated JWT secret).
- `internal/server/server.go` — `Server` struct holds one `*db.XRepo` field per aggregate,
  all constructed in `Start()` after migrations. `SetupComplete atomic.Bool` is the live
  setup flag; `applyTLSAndRestart()` flips it and rebuilds the router via `setupRouter()`.
- `internal/server/router.go` — all routes in one function. `r.Use(chimw.RealIP)` at line 30
  is the first global middleware. Public `/api/v1/auth` group at line 85 already hosts
  `POST /login` (rate-limited), `POST /logout`, `GET /check`. Two rate limiters exist:
  `loginLimiter` (5/min, burst 5) and `registerLimiter` (10/min, burst 10).
  **The frontend catch-all `r.Get("/*")` at line 274 serves `index.html` with HTTP 200 for
  any unmatched path** — including unmatched `/api/...` paths. This is why the OIDC routes
  must always be registered (AD-011).
- `internal/server/handlers/handlers.go` — `HandlerDeps` struct → `New()` → unexported
  fields on `*Handlers`. Every handler is a method on `*Handlers`. `writeJSON` is the JSON
  writer; errors go out as `http.Error(w, `{"error":"..."}`, status)`.
- `internal/server/handlers/auth.go` — `Login` (bcrypt compare → HS256 JWT
  `{sub:"admin", exp:+24h, iat}` → `riot_session` cookie, `Path=/`, `HttpOnly`,
  `SameSite=Strict`, **no `Secure`**, `MaxAge=86400`), `Logout`, `AuthCheck`,
  `ChangePassword`. The cookie construction is inline and duplicated between `Login` and
  `Logout`.
- `internal/server/middleware/setup.go` — `SetupGuard` returns
  `503 {"error":"setup_required"}` for **every** `/api/*` path while setup is incomplete,
  with a hardcoded allowlist (`/api/v1/setup/`, `/health`, `/api/v1/server-cert`,
  `/api/v1/auth/check`).
- `internal/server/middleware/ratelimit.go` — in-memory per-IP token bucket,
  `NewRateLimiter(ratePerMin, burst)`, `allow(ip)`, and one exposed adapter `Middleware()`
  that responds `429 {"error":"too many requests"}`. Keyed on
  `net.SplitHostPort(r.RemoteAddr)` with a fallback to the raw `RemoteAddr`.
- `internal/server/middleware/logger.go` — logs `method`, `path` (no `RawQuery`), `status`,
  `duration`, and `remote` = `r.RemoteAddr`.
- `internal/server/middleware/admin_auth.go` — `AdminAuth(jwtSecret)` validates
  `riot_session`; `CheckWSOrigin` guards both WebSocket upgrades against cross-origin.
- `internal/server/db/` — one file per aggregate, hand-written SQL on `r.db.Pool`, a
  `NewXRepo(db)` constructor, an interface in `interfaces.go`, and a compile-time
  conformance assertion at the bottom of `interfaces.go`. Mocks live in
  `internal/testutil/mocks.go`.
- `internal/server/logstore/` — `DBHandler` persists `WARN` and above to `server_logs`,
  surfaced by `GET /api/v1/settings/logs` in Settings → Logs. `INFO` goes to stdout only.
- `cmd/riot-server/migrations/` — `NNNNNN_name.up.sql` / `.down.sql`, embedded with
  `//go:embed all:migrations`, run automatically at boot. Highest is `000021`.
- Tests: `testify` (`assert`/`require`), `httptest`, hand-written mocks from
  `internal/testutil`. `newTestHandlers(t)` in `auth_handler_test.go` builds a `*Handlers`
  by direct struct literal — new fields must be settable that way.

**Frontend (React 19, Vite 6, TS 5.9, Tailwind 4, TanStack Query 5, react-router-dom 7)**

- `web/src/main.tsx` — `QueryClientProvider` wraps `BrowserRouter` wraps `App`, so
  `useQuery` and `useSearchParams` are both available inside `Login`. Global query defaults:
  `retry: 1`, `staleTime: 10_000`, `refetchOnWindowFocus: false`.
- `web/src/App.tsx:302` — `if (!authenticated) return <Login onLogin={login} />`. The login
  screen has no route of its own and renders at whatever URL the browser is on.
- `web/src/pages/Login.tsx` — password form, "remember me" via `credentialStore`, demo-mode
  auto-login. No SSO affordance.
- `web/src/api/client.ts` — `fetchJSON<T>` + a flat `api` object. In demo builds
  `vite.config.ts` aliases `api/client` → `api/demo-client`, so **every export added to
  `client.ts` must also exist in `demo-client.ts`** or `tsc && vite build:demo` fails.
- **There is no service worker.** No `navigator.serviceWorker` registration, no
  `vite-plugin-pwa`, no `sw.js`, no `workbox`; `web/public/` contains only icons and
  `site.webmanifest`. See AD-018.

### 2.2 What is missing

- No OIDC anything: no package, no routes, no dependency on `go-oidc` or `x/oauth2`.
- No persistence for an external identity, and no migration slot used past `000021`.
- No reusable session-minting helper — the cookie is built inline inside `Login`.
- No `Secure` attribute on `riot_session`, and no scheme resolution outside the OIDC design.
- No trusted-proxy configuration: `chimw.RealIP` rewrites `r.RemoteAddr` from
  `True-Client-IP` / `X-Real-IP` / `X-Forwarded-For` supplied by any client.
- `SetupGuard` would return `503` for the new endpoints, which contradicts AC-020.
- `RateLimiter` has no adapter that fails a browser navigation as a redirect instead of JSON.
- `Handlers` has no access to the live setup flag (it re-queries `adminRepo` in `AuthCheck`).

### 2.3 Reference implementation

`D:\Repos\photojournal\apps\server\src\auth\oidc\` (recollect, NestJS). Mirrored here:
lazy discovery cached across logins and cleared on failure (`oidc.service.ts:210-225` →
AD-004); availability/start/callback split with browser-navigation error handling
(`oidc.controller.ts` → AD-010); base64url-JSON httpOnly path-scoped 5-minute transaction
cookie (`oidc-transaction-cookie.ts` → AD-005); pure, unit-tested decision helpers
(`oidc-identity.ts` → AD-007). Deliberate divergences: rIOt has no user table so the
link/provision decision collapses to "validate the claims" (D-4/BR-002), and rIOt has a
server-side secret conveniently at hand so the transaction cookie is authenticated with a
domain-separated key derived from it (AD-005).

---

## 3. Architecture Decisions

### AD-001: A dedicated `internal/server/oidc` package owns the protocol; handlers stay thin

**Decision.** Create `internal/server/oidc` with `config.go`, `service.go`, `transaction.go`,
`identity.go`, `errors.go`. HTTP concerns (routing, cookies on the wire, redirects, logging,
throttling) stay in `internal/server/handlers/oidc.go` as methods on the existing `*Handlers`
receiver.

**Rationale.** Matches the existing per-domain package layout (`ca`, `notify`, `probes`,
`updates`, `events`, `scoring`) and the reference implementation's service/controller split.
It keeps the protocol logic testable against a stub issuer without an HTTP handler, and keeps
the handler file readable as a state machine over the §7.4 error vocabulary.

**Alternatives considered.** (a) Everything in `handlers/oidc.go` — rejected: mixes
protocol with transport, and makes the pure helpers (`SafeReturnPath`, transaction
encode/decode) awkward to unit test. (b) A third-party "sso" middleware library — rejected:
`app_standards.md` §4 rule 3 mandates `coreos/go-oidc` + `x/oauth2` directly.

**Consequences.** One new package to keep in sync with `Config`. The service is constructed
once in `setupRouter()` and lives for the process lifetime, which is what makes the discovery
cache useful.

---

### AD-002: New dependencies `github.com/coreos/go-oidc/v3` and `golang.org/x/oauth2` — approved, with security floors

**Decision.** Add exactly these two modules as direct dependencies. No other new direct
dependency is authorised by this story. **Minimum versions are security floors, not API
floors, and must be recorded in `go.mod`:**

| Module | Minimum | Why |
|---|---|---|
| `golang.org/x/oauth2` | **≥ v0.27.0** | CVE-2025-22868 (unbounded memory parsing malicious JWS in `oauth2/jws`), fixed in v0.27.0. Also provides `GenerateVerifier`, `S256ChallengeOption`, `VerifierOption` (AD-006) |
| `github.com/coreos/go-oidc/v3` | current release | Discovery, JWKS/`RemoteKeySet`, ID-token verification |
| `github.com/go-jose/go-jose/v4` | **≥ v4.0.5**, pinned explicitly in `go.mod` | Transitive via `go-oidc`. CVE-2024-28180 (JWE decompression bomb, v4.0.1) and CVE-2025-27144 (parsing DoS, v4.0.5). Go's MVS will resolve lower if any module asks for it, so add an explicit `require` entry rather than relying on `go get`'s choice |

`govulncheck ./...` must be clean before the story is handed to QA (§13). This binary had no
JOSE parsing stack before this story; adding one warrants a standing check, not a one-time
glance.

**Rationale.** Mandated by D-5, FR-021 ("validation must be performed by the OIDC library,
not by hand-written token parsing") and `app_standards.md` §4 rule 3. The version floors come
from security finding SEC-007 — "take the current release" is a build-time instruction, not a
constraint, and MVS does not honour intent.

**Alternatives considered.** Hand-rolling JWKS + JWT validation on top of the already-present
`golang-jwt/jwt/v5` — rejected outright by FR-021 and by the house rule; token validation is
exactly the code you do not write yourself.

**Consequences.** Engineering runs `go get` for both, adds the explicit `go-jose/v4`
requirement, and commits `go.mod` + `go.sum`. Any missing symbol is a version problem, not a
design problem — do not vendor or fork. Dependency *upgrades* outside this set remain
forbidden by the engineering standards.

---

### AD-003: Configuration read in `LoadConfig`, policy applied in the `oidc` package

**Decision.** `internal/server/config.go` gains five fields and five reads (the fifth,
`TrustedProxies`, belongs to AD-020):

```
OIDCIssuerURL    <- strings.TrimSpace(os.Getenv("RIOT_OIDC_ISSUER_URL"))
OIDCClientID     <- strings.TrimSpace(os.Getenv("RIOT_OIDC_CLIENT_ID"))
OIDCClientSecret <- strings.TrimSpace(os.Getenv("RIOT_OIDC_CLIENT_SECRET"))
OIDCButtonLabel  <- strings.TrimSpace(os.Getenv("RIOT_OIDC_BUTTON_LABEL"))
TrustedProxies   <- strings.TrimSpace(os.Getenv("RIOT_TRUSTED_PROXIES"))
```

`LoadConfig` performs no validation beyond trimming and stores the values verbatim.
`oidc.New(oidc.Options{IssuerURL, ClientID, ClientSecret, ButtonLabel, JWTSecret})` applies
all policy and returns a `*Service` that is either enabled or dormant:

- dormant when any of issuer/clientID/clientSecret is empty after trimming (FR-002);
- dormant when the issuer URL fails `url.Parse`, or its scheme is not `http`/`https`, or its
  host is empty — logged once at boot as
  `slog.Warn("OIDC issuer URL is not a valid absolute http(s) URL — SSO stays dormant", "issuer", raw)` (V-005, FR-006);
- label defaults to `Sign in with SSO` when empty (FR-003);
- label truncated to the first 64 **runes** when longer, logged once at `slog.Warn` (OQ-8).

**Rationale.** Keeps one env-reading site (the codebase's existing invariant) while keeping
validation policy unit-testable inside the package that depends on it. The boot-time warning
satisfies V-005's "logs a configuration warning at boot; must not abort boot".

**Alternatives considered.** Reading `os.Getenv` inside the `oidc` package — rejected: two
places would then own `RIOT_*` and `config_test.go` would no longer cover the feature's
dormancy contract. Runtime configuration through the settings panel — explicitly out of
scope (§9 item 6).

**Consequences.** A-1 holds: changing any `RIOT_OIDC_*` value requires a restart. Neither
`RIOT_OIDC_*` nor `RIOT_TRUSTED_PROXIES` is required, so AC-030 falls out for free.

---

### AD-004: Lazy, once-only discovery cached in the service; failures are never cached

**Decision.** `*Service` holds `mu sync.Mutex`, `provider *oidc.Provider`, and
`httpClient *http.Client` with `Timeout: 10 * time.Second`. `discover(ctx)`:

1. Fast path: return the cached `provider` under the mutex if non-nil.
2. Otherwise build `ctx = oidc.ClientContext(ctx, s.httpClient)` and call
   `oidc.NewProvider(ctx, s.issuerURL)`.
3. On success, store the provider and return it.
4. On failure, leave the field nil and return a `LoadError` — the next attempt retries.

The server must **never** call `discover` during `Start()` or `setupRouter()` (FR-006,
NFR-004). The only callers are `/start` and `/callback`.

**Rationale.** Direct port of `oidc.service.ts:210-225`. A cached provider gives NFR-002
(<2 s redirect) after the first login; not caching failures gives A-7 (a recovered IdP works
on the next attempt without a restart). Constructing nothing at boot gives AC-019 for free —
the IdP is not in any startup path, so `/health`, ingest, WS, and password login cannot be
affected by it.

**Alternatives considered.** (a) Eager discovery at boot with a background retry — rejected:
couples boot to an external service and directly violates FR-006. (b) A TTL cache — rejected:
authentik's metadata and JWKS are effectively static, and `go-oidc`'s `RemoteKeySet` already
re-fetches JWKS on unknown `kid`, so key rotation is handled without expiring the provider.

**Consequences.** A running server pins the discovery document until restart. If an operator
re-creates the authentik application with different endpoints, they restart rIOt — acceptable
for a homelab tool and consistent with A-1.

---

### AD-005: Transaction carried in a domain-separated, HMAC-authenticated, path-scoped 5-minute cookie

*(Amended in rev 2 for SEC-004, SEC-005, SEC-006.)*

**Decision.** Cookie `riot_oidc_tx`. Value = `base64url(JSON(payload)) + "." +
base64url(HMAC_SHA256(txKey, base64url(JSON(payload))))`. Payload:

```go
type Transaction struct {
    State        string `json:"s"`
    Nonce        string `json:"n"`
    CodeVerifier string `json:"v"`
    ReturnPath   string `json:"r"`
    IssuedAt     int64  `json:"t"` // unix seconds
}
```

**Key derivation (SEC-004).** `txKey` is **not** the raw JWT secret. It is derived once, at
`oidc.New` time, as:

```
txKey = HMAC_SHA256(jwtSecret, []byte("riot-oidc-tx-v1"))
```

exposed as `oidc.DeriveTransactionKey(jwtSecret []byte) []byte` so the derivation is
unit-testable and used identically by encode and decode. The raw `jwtSecret` must not be
passed to `Encode`/`DecodeTransaction`.

Attributes: `Path=/api/v1/auth/oidc`, `HttpOnly`, `SameSite=Lax`, `Secure` when the resolved
request scheme is `https` (AD-020), `MaxAge=300`. Cleared on every callback outcome by
re-setting the same name/path/attributes with `MaxAge=-1` and an empty value.

**Verification order on read**, which is a contract because QA asserts on it:

1. Cookie present, else `ErrTransactionMalformed`.
2. Split on the **last** `.`; compute the MAC over the prefix; `hmac.Equal` — a mismatch is
   `ErrTransactionMAC`. **The MAC check runs before any decoding**, so a value that is not a
   transaction at all (for example a `riot_session` JWT) is rejected here, on cryptographic
   grounds, and never on a downstream parse accident.
3. `base64.RawURLEncoding` decode + JSON unmarshal, else `ErrTransactionMalformed`.
4. All five fields non-empty and `IssuedAt > 0`, else `ErrTransactionMalformed`.
5. `now.Unix() - IssuedAt <= 300`, else `ErrTransactionExpired`.

`ErrTransactionMAC` and `ErrTransactionMalformed` both map to browser code `sso_expired` with
log reason `no_transaction`; `ErrTransactionExpired` maps to `sso_expired` with reason
`transaction_expired`. The sentinels are exported solely so tests can assert *which* check
rejected a value; the browser cannot distinguish them.

**Rationale.** Stateless: no server-side store, no restart coordination, no cleanup worker —
matching `oidc-transaction-cookie.ts`. `SameSite=Lax` is mandatory here, not optional: the
callback arrives as a cross-site top-level navigation from authentik, and a `Strict` cookie
would be withheld on exactly that request, making every SSO login fail. Path-scoping keeps
the verifier off every other request in the app.

The MAC exists to prevent **cookie-format confusion**, not to stop attacker-initiated flows.
Without domain separation, `riot_oidc_tx` and `riot_session` would be authenticated by the
same key under the same algorithm, and a `riot_session` JWT split on its last `.` yields
exactly `payload = b64(header).b64(claims)`, `mac = sig` — a MAC that verifies. Rev 1 was
saved only by the payload segment containing a `.` and failing base64 decoding, which is a
knife-edge that dies the moment a lenient decoder or a reordered check appears. Deriving a
separate key removes the collision class outright, independent of encoding or parse order.

**What the MAC does *not* do (SEC-005).** It does not prevent login CSRF. An attacker can
simply call `/api/v1/auth/oidc/start` themselves and receive a genuinely server-signed
transaction of the server's choosing, then plant that cookie on a victim from a same-site
foothold and navigate them to a captured callback URL. The property that makes this
low-impact in rIOt is different and is stated plainly here and in §10: **every rIOt session
is the identical `sub:"admin"` principal (BR-006)**, so "logged into the attacker's session"
and "logged into their own session" are the same session — there is no per-user data to
poison and no account to attribute actions to. The attacker cannot read the resulting session
(same-origin policy). **If rIOt ever gains per-user sessions, this analysis must be redone.**
`Secure` on both cookies (AD-008, AD-020) and the key separation above both materially shrink
this surface.

**Single use (SEC-006).** NFR-006 says the transaction "must be usable only once". This
design does not make consumption server-authoritative — it is stateless by choice. The
property is delegated to two mechanisms, and this is recorded rather than implied: (a) the
cookie is cleared on the response for every callback outcome (§7.3 step 4), and (b) the
authorization code is single-use at the IdP. A copy of the cookie taken before the callback
remains cryptographically valid for the rest of its 300 s window; an attacker holding both
that copy and an unconsumed `code` already has the victim's callback traffic. QA is handed a
named LIVE step (§8.1) confirming that authentik rejects a replayed `code`. Making
consumption server-authoritative would require the server-side store AD-005 rejects, and is
recorded as a follow-up if the threat model changes.

**Alternatives considered.** (a) Server-side transaction map keyed by state — rejected: adds
a mutex, a sweeper, and a restart failure mode; the benefit (server-authoritative single use)
does not carry its weight against the threat model above. (b) Encrypting the payload —
rejected: the verifier and nonce are not secrets from the person holding the browser;
authenticity, not confidentiality, is the property required. (c) HKDF-Expand instead of a
single HMAC for derivation — equivalent here; the single HMAC with a fixed label is fewer
moving parts for one derived key.

**Consequences.** When `RIOT_JWT_SECRET` is unset and no `jwt_secret` row exists, the secret
is regenerated per boot, so a restart mid-flight invalidates in-flight transactions →
`sso_expired`. That is precisely the answer OQ-6 assumes, and it is no worse than the
existing behaviour for `riot_session`.

---

### AD-006: PKCE, state and nonce generated from `crypto/rand`; PKCE via `x/oauth2` helpers

**Decision.** `verifier := oauth2.GenerateVerifier()` (43 chars, 32 random bytes, RFC 7636
compliant). `state` and `nonce` are each 32 bytes from `crypto/rand.Read` rendered with
`base64.RawURLEncoding` (256 bits, ≥ the 128 required by NFR-005); a `rand.Read` error is a
hard failure that returns `sso_unavailable` rather than proceeding with weak values.
Authorization URL: `oauth2Config.AuthCodeURL(state, oidc.Nonce(nonce),
oauth2.S256ChallengeOption(verifier))`. Token exchange:
`oauth2Config.Exchange(ctx, code, oauth2.VerifierOption(verifier))`. Scopes:
`[]string{oidc.ScopeOpenID, "email", "profile"}` (FR-013, A-4).

**Rationale.** The library helpers are the only path that guarantees the challenge and the
verifier agree and are correctly encoded; hand-rolling S256 is a classic source of
`invalid_grant`. Uniform 32-byte randomness for state and nonce is simple to audit.

**Alternatives considered.** `oauth2.SetAuthURLParam("code_challenge", ...)` by hand —
rejected: more code, more room for base64 padding bugs.

**Consequences.** `x/oauth2` must be ≥ v0.27.0 (AD-002), which covers both the helper symbols
and CVE-2025-22868.

---

### AD-007: Pure helpers for the two decisions that carry security weight

**Decision.** `internal/server/oidc/identity.go` exposes two pure functions with no I/O:

- `SafeReturnPath(raw string) string` — returns `raw` only when it begins with exactly one
  `/` (i.e. `strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//")`) **and** contains
  no `\` (a Windows-style backslash is normalised to `/` by some browsers, making
  `/\evil.example` a protocol-relative bypass) and no control characters; otherwise `/`.
  Implements V-004 and the `^/(?!/)` rule.
- `ValidateClaims(c Claims) error` — rejects empty `Issuer` or empty `Subject` with a
  `LoginError{Code: "sso_failed", Reason: "claims_incomplete"}`. Implements V-001.

`Claims` is `{Issuer, Subject string; Email *string; EmailVerified *bool}` — pointers so
"absent" and "present and false/empty" stay distinguishable in the audit row (7.1, A-5).

**Rationale.** Mirrors `oidc-identity.ts` + `oidc-identity.spec.ts`: the decisions that could
produce an open redirect or a session for an anonymous subject are exercised by table tests
with no HTTP or network scaffolding. The `\` and control-character guards are additions over
the reference's `startsWith('/') && !startsWith('//')`, which the security review confirmed
covers the bypasses it tested (SEC-015).

**Alternatives considered.** `url.Parse` + checks on `IsAbs()`/`Host` — rejected: `url.Parse`
accepts and normalises enough oddities that the prefix rule is both simpler and stricter.

**Consequences.** `email_verified` is recorded, never gated on (BR-007) — the pure function
must not consult it.

---

### AD-008: One helper mints `riot_session`; `SameSite=Lax` **with** scheme-derived `Secure`

*(Amended in rev 2 for SEC-001.)*

**Decision.** Two changes to `internal/server/handlers/auth.go`, both scoped to this story:

1. Extract the cookie construction into two unexported helpers used by `Login`, `Logout`,
   and the new callback:
   - `func (h *Handlers) issueSessionCookie(w http.ResponseWriter, r *http.Request, now time.Time) error`
     — builds the HS256 JWT `{sub:"admin", exp:now+24h, iat:now}`, signs with `h.jwtSecret`,
     and sets `riot_session`.
   - `func clearSessionCookie(w http.ResponseWriter, r *http.Request)` — same
     name/path/attributes, empty value, `MaxAge=-1`.
2. Both set: `Path=/`, `HttpOnly`, `SameSite=http.SameSiteLaxMode`, `MaxAge` (`86400` /
   `-1`), and **`Secure: middleware.RequestScheme(r) == "https"`** — the identical scheme
   resolution used for `riot_oidc_tx` and for `redirect_uri` (AD-020).

`Login`, `Logout`, and `OIDCCallback` all call these helpers and construct no cookie of their
own. The helpers take `*http.Request` purely so the scheme can be resolved.

**Rationale.** The helper makes FR-023/AC-008 true *by construction* rather than by three
implementations agreeing — the single highest-value structural decision in this story, and
what makes the `Secure` fix a one-line change instead of a three-site change.

The `Lax` change is the sanctioned fix for the FR-036/AC-009 hazard: the callback's `302` is
the tail of a cross-site redirect chain that originates at authentik, so the browser
evaluates the whole chain as cross-site and withholds a `Strict` cookie on the landing
navigation to `/`. It is the house standard (`app_standards.md` §4 rule 5), and recollect's
session cookies are already `Lax` with a proven callback round-trip.

The `Secure` addition is **not optional and not cosmetic** (SEC-001). `Strict` was doing two
jobs: blocking cross-site mutating requests, and suppressing the cookie on *any*
attacker-initiated top-level navigation. `Lax` keeps the first and drops the second. Cookies
are not port-scoped and are not scheme-scoped without `Secure`, so under `Lax` alone an
attacker page executing `location = 'http://<rIOt-host>/x'` causes the browser to attach the
full 24-hour admin JWT to a cleartext request aimed at any listener the attacker can get onto
port 80 of that host — a common homelab shape. That session is an interactive container shell
on every enrolled device (`GET /ws/terminal/{deviceId}/{containerId}`). rIOt emits no HSTS
header (SEC-010, pre-existing, own story), so `Secure` is the only control available here —
and, per the security review, fixing `Secure` is both necessary and sufficient in the absence
of HSTS.

Deriving `Secure` from the request rather than hardcoding it preserves NFR-010's plain-HTTP
LAN mode: over `http` the attribute is omitted and the cookie works exactly as today. It also
cannot lock anyone out — `Secure` is a restriction on *sending over http*, so a deployment
behind a TLS-terminating proxy where rIOt speaks plain HTTP (and has not configured
`RIOT_TRUSTED_PROXIES`) simply omits the flag and continues to work over https. That
deployment does not *get* the protection until `RIOT_TRUSTED_PROXIES` is set, which is why
§4.5 item 6 leads with exactly that consequence.

Residual CSRF impact of `Strict → Lax`: `Lax` still withholds the cookie from **all**
cross-site `POST`/`PUT`/`DELETE`. Every mutating rIOt endpoint is `POST`, `PUT`, or `DELETE`
(verified across `internal/server/router.go` — there is no state-changing `GET`). `Lax` does
allow the cookie on cross-site top-level `GET` navigations, which reaches only read handlers
plus the markdown download `GET /api/v1/devices/{id}/summary`; a cross-site navigation cannot
read the response. Cross-site WebSocket handshakes are not top-level navigations, so `Lax`
withholds the cookie from them just as `Strict` did, with `middleware.CheckWSOrigin` as
defence in depth (SEC-014). Net CSRF exposure: unchanged.

**Alternatives considered.** (a) Keep `Strict` and land on an intermediate same-site page
that re-navigates to `/` — rejected: adds a page, a redirect, and a flash of the login screen
that AC-009 explicitly forbids. (b) Keep `Strict` and set a second, `Lax` "bootstrap" cookie
— rejected: two session cookies is a security-review liability for zero benefit. (c)
`Secure` unconditionally — rejected: breaks NFR-010's plain-HTTP LAN mode outright.

**Consequences.** FR-031 is amended as recorded in §0 amendment 1. `auth_handler_test.go`
gains `SameSite` and `Secure` assertions at both mint sites. The security-review note in
`docs/security/FLEET-DASH-security-review.md` describing the cookie as `Strict` becomes
stale — the technical writer logs it, this story does not edit that file.

---

### AD-009: `redirect_uri` derived from the request through one shared helper

*(Amended in rev 2: scheme resolution now delegates to AD-020's trust-gated helper.)*

**Decision.** `handlers.requestOrigin(r *http.Request) string` returns
`middleware.RequestScheme(r) + "://" + r.Host`. `Host` is `r.Host` verbatim (including any
port). The callback URI is `requestOrigin(r) + "/api/v1/auth/oidc/callback"`, produced by the
same function on `/start` and on `/callback`, guaranteeing FR-015's byte-for-byte identity.
`X-Forwarded-Host` is **not** consulted, ever. `X-Forwarded-Proto` is consulted only when the
immediate peer is a configured trusted proxy (AD-020).

**Rationale.** D-8/NFR-010: rIOt is reached directly on `:7331` over self-signed TLS, or over
plain HTTP on a LAN, or occasionally behind a proxy that terminates TLS (where `r.TLS` is nil
but the browser used `https`). One function, three call sites (`/start`, `/callback`, and
the cookie `Secure` flag), no configuration beyond the proxy allowlist.

**Security.** `Host` is client-controlled, so a poisoned `Host` header yields a poisoned
`redirect_uri`. This is contained by authentik's strict redirect-URI matching (D-8, A-3): a
`redirect_uri` the operator did not register is rejected by the IdP before any code is
issued, and the code is additionally bound to the `redirect_uri` at exchange time. Even a
disclosed code is unusable without the PKCE verifier (in the victim's `HttpOnly` cookie) and
the client secret. **That containment is an operator configuration** — §4.5 items 4–5 make
`matching_mode: strict` mandatory and prohibit regex/wildcard matching, and give the exact
registration command that produces it. A spoofed `X-Forwarded-Proto` from an untrusted peer
is now ignored outright (AD-020).

**Alternatives considered.** A `RIOT_PUBLIC_URL` env var — rejected: FR-008 forbids any new
required variable, and D-8 settles the derivation. Honouring `X-Forwarded-Host` — rejected:
strictly more spoofable surface for a deployment shape rIOt does not target.

---

### AD-010: Failures leave as redirects; the browser never sees JSON, HTML errors, or 5xx

**Decision.** `/start` and `/callback` have exactly three response shapes: `302` to the IdP,
`302` to `/?sso_error=<code>`, or `404` (dormant / setup incomplete). Every internal failure
is funnelled through `oidc.LoginError{Code, Reason, err}` where `Code` is one of the four
§7.4 values and `Reason` is a fixed log-only token. Full mapping:

| Failure | Browser `sso_error` | Log `reason` |
|---|---|---|
| Discovery failed / issuer unreachable / timeout at `/start` | `sso_unavailable` | `discovery_failed` |
| `crypto/rand` failure building state/nonce | `sso_unavailable` | `rand_failed` |
| Throttled (AD-013) | `sso_failed` | `throttled` |
| Transaction cookie absent, malformed, or MAC invalid | `sso_expired` | `no_transaction` |
| Transaction older than 300 s | `sso_expired` | `transaction_expired` |
| IdP returned `error=access_denied` | `sso_denied` | `idp_error` |
| IdP returned any other `error=` | `sso_failed` | `idp_error` |
| `state` mismatch | `sso_failed` | `state_mismatch` |
| `code` parameter absent | `sso_failed` | `missing_code` |
| Token exchange: transport error / timeout / DNS (not `*oauth2.RetrieveError`) | `sso_unavailable` | `idp_unreachable` |
| Token exchange: `*oauth2.RetrieveError` (OAuth error from the IdP) | `sso_failed` | `token_exchange_rejected` |
| Response carries no `id_token` | `sso_failed` | `missing_id_token` |
| ID token signature / issuer / audience / expiry invalid | `sso_failed` | `token_invalid` |
| `nonce` claim ≠ transaction nonce | `sso_failed` | `nonce_mismatch` |
| Empty `iss` or `sub` after validation | `sso_failed` | `claims_incomplete` |

**Rationale.** FR-017, FR-024, D-9, AC-013, AC-017, AC-018. These endpoints are only ever
reached by a top-level navigation, so any non-redirect response is a dead end in the user's
face. Folding state/nonce/token failures into `sso_failed` is OQ-5's decided answer:
distinguishing them in the URL tells an attacker which check they tripped, while the operator
gets the exact `reason` in the log (NFR-009).

**Alternatives considered.** Rendering a server-side error page — rejected: the frontend is
an embedded SPA with no server-rendered templates, and the login screen must remain usable
(FR-035).

**Consequences.** The fixed `reason` vocabulary above is a contract: QA asserts on it, and
engineering must not invent additional values.

---

### AD-011: Routes are always registered; dormancy is a handler-level 404

**Decision.** Register all three routes unconditionally inside the existing public
`r.Route("/api/v1/auth", ...)` group. `/start` and `/callback` begin with
`if !h.oidc.Enabled() || !h.isSetupComplete() { 404 }`. `oidc.Enabled()` is written with a
nil-receiver guard (`func (s *Service) Enabled() bool { return s != nil && s.enabled }`) so
tests can pass a nil service.

**Rationale.** The frontend catch-all `r.Get("/*")` (router.go:274) serves `index.html` with
**HTTP 200** for any path chi does not match. Conditional registration would therefore make
AC-002 and AC-020 fail with a 200 HTML page instead of a 404. Handler-level gating also lets
the setup check be dynamic — setup completes at runtime and rebuilds the router, but the
config does not.

**Alternatives considered.** Conditional registration plus an explicit `r.NotFound` for
`/api/*` — rejected: changes global 404 behaviour for every endpoint, far outside this
story's blast radius.

---

### AD-012: `Handlers` reads the live setup flag through `*atomic.Bool`, not the database

**Decision.** `HandlerDeps` gains `SetupComplete *atomic.Bool`, wired from `&s.SetupComplete`
in `setupRouter()`. `Handlers` gains the field plus
`func (h *Handlers) isSetupComplete() bool { return h.setupComplete != nil && h.setupComplete.Load() }`.
A nil pointer means "not complete" — tests that need the complete state set it explicitly.

**Rationale.** NFR-001 caps availability at 100 ms p95 with no network I/O; the login screen
calls it on every mount, and a DB round trip per call is avoidable. `SetupGuard` already
takes exactly this pointer, so the pattern is established. The value is authoritative and
live — `applyTLSAndRestart()` stores `true` before rebuilding the router. Nil-safe false
means a mis-wired dependency yields "SSO off", never "SSO on and ungated".

**Alternatives considered.** `h.adminRepo.IsSetupComplete(ctx)` per request (as `AuthCheck`
does) — rejected on the latency budget and because it would make the availability endpoint
fail closed on a DB blip, hiding the SSO button for no good reason.

---

### AD-013: A dedicated rate-limiter instance and a redirecting adapter

*(Amended in rev 2: the key now comes from AD-020. The bucket decision is unchanged.)*

**Decision.** Add `oidcLimiter := middleware.NewRateLimiter(5, 5)` in `setupRouter()` — the
same policy as `loginLimiter` but a **separate bucket** — and apply it to `/oidc/start` and
`/oidc/callback` only. `GET /api/v1/auth/oidc` is **not** throttled. Add one method to
`internal/server/middleware/ratelimit.go`:

`func (rl *RateLimiter) MiddlewareRedirect(location string) func(http.Handler) http.Handler`

— identical keying and bucket logic as `Middleware()`, but on rejection responds
`http.Redirect(w, r, location, http.StatusFound)` instead of `429 JSON`. Wired with
`location = "/?sso_error=sso_failed"`.

Both `Middleware()` and `MiddlewareRedirect()` key on `middleware.ClientIP(r)` (AD-020) —
which, in the default no-proxy deployment, is the TCP peer address and is not derivable from
any request header. Policy is unchanged: 5/min, burst 5.

**Rationale.** NFR-008 requires "at least as strict as password login" — 5/min burst 5 is
exactly that, and reusing the existing token bucket honours "reuse, don't reinvent". The
bucket must be *separate*: sharing `loginLimiter` would let a burst of failed SSO attempts
consume the password-login allowance and lock the operator out of the fallback, directly
contradicting D-9 and AC-017's "a password login submitted immediately afterwards succeeds".
That reasoning only holds once the key is unforgeable — with a spoofable key an attacker
empties both buckets at will (SEC-002 secondary scenario), which is why AD-020 is a
prerequisite of this decision rather than an optional hardening.

Availability is exempt because it is fetched on every login-screen mount by legitimate users
and throttling it would suppress the button; the handler touches no DB and no network, so it
is not a useful amplification target (SEC-012, no action required). The redirecting adapter
keeps AD-010's "never JSON in the browser" invariant intact for a throttled navigation, and
reuses the closed §7.4 vocabulary rather than inventing a fifth code.

**Alternatives considered.** (a) Reuse `loginLimiter` — rejected above. (b) Accept the
existing `429 {"error":"too many requests"}` JSON page — rejected: a raw JSON body in the
browser is exactly the dead end D-9 forbids. (c) A new limiter type keyed on IP + endpoint —
rejected: unnecessary; two instances of the existing type is simpler.

---

### AD-014: Failure lands on `/?sso_error=<code>`; `Login.tsx` clears it via `useSearchParams`

**Decision.** The query parameter is named **`sso_error`**. The landing URL is always the
dashboard root: `/?sso_error=sso_failed|sso_expired|sso_denied|sso_unavailable`. The
frontend owner of the parameter is `web/src/pages/Login.tsx`: on mount it reads
`searchParams.get('sso_error')` into local state, renders the mapped message, and immediately
removes the key with `setSearchParams(next, { replace: true })` from `react-router-dom`.
Unrecognised codes render the generic `sso_failed` message and are still stripped (FR-037).

**Rationale.** A-6 proposes this exact shape; §2.2 rules out `/login`. `useSearchParams` is
already a dependency and performs a `replace` navigation through the router, so the history
entry and React Router's internal location stay in sync — unlike a bare
`window.history.replaceState`, which would desync `BrowserRouter` and can resurrect the
parameter on the next in-app navigation. Clearing in `Login` (not `App`) is correct because
`Login` is the only component that consumes it, and a successful callback redirects to a
clean `/` so an authenticated user can never be left holding the parameter.

**Alternatives considered.** (a) `#sso_error=` fragment — rejected: never sent to the server,
harder to test, and inconsistent with the reference. (b) A one-shot cookie — rejected: more
moving parts than a query parameter for a message the user is about to read once.

**Consequences.** The parameter is visible in the URL for one render. It carries no secret —
only one of four fixed tokens. It is rendered as React text (auto-escaped); the component
must map the code to a message from a lookup table and **must never** render the raw
parameter value.

---

### AD-015: Audit write is best-effort, detached from the request context, and never blocks login

**Decision.** After successful validation and *before* writing the session cookie, the
handler calls `firstSeen, err := h.externalIdentityRepo.RecordLogin(ctx, ident)` with
`ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)`.
An error is logged at
`slog.Error("record external identity", "error", err, "issuer", iss, "subject", sub, "first_seen_unknown", true)`
and execution continues to mint the session and redirect. A nil repo (tests) is skipped.
`firstSeen == true` additionally triggers AD-021's `WARN`.

**Rationale.** OQ-2's decided answer: losing admin access because an audit row would not
write is worse than a missing audit row (FR-028). `context.WithoutCancel` matters because
the browser is mid-navigation and a fast client disconnect would otherwise cancel the write.
Running it synchronously (rather than in a goroutine) keeps AC-010/AC-011 and the AD-021
`WARN` deterministically testable; the write is a single indexed upsert, well inside the
latency budget. When the write fails, first-seen status is genuinely unknown — the error
entry says so explicitly rather than silently omitting the admission warning.

**Alternatives considered.** (a) Fire-and-forget goroutine — rejected: makes the ACs racy to
test for no measurable gain. (b) Abort the login on write failure — rejected by OQ-2.

---

### AD-016: One additive table, upserted on `(issuer, subject)`, reporting insert-vs-update

*(Amended in rev 2 for SEC-003: `RecordLogin` now returns whether the row was newly created.)*

**Decision.** Migration `000022_external_identities` creates one table with a unique
constraint on `(issuer, subject)` (§5). The repository signature is:

```go
RecordLogin(ctx context.Context, ident models.ExternalIdentity) (firstSeen bool, err error)
```

implemented as a single `INSERT ... ON CONFLICT (issuer, subject) DO UPDATE ...
RETURNING (xmax = 0)` — Postgres reports `xmax = 0` for a tuple produced by an insert and
non-zero for one produced by the conflict update, so insert-vs-update is known in the **same
round trip**. The update refreshes `email`, `email_verified`, and `last_login_at` while
leaving `first_login_at` untouched. The timestamp is a parameter
(`models.ExternalIdentity.LoginAt`), never `NOW()` inside the SQL.

**Rationale.** V-002 and AC-011 in one statement, no read-modify-write, no race between two
concurrent logins for the same subject — and `RETURNING (xmax = 0)` preserves that property
while supplying exactly the signal AD-021 needs. NFR-011: additive, single migration, clean
rollback, no existing table touched. The injected clock satisfies the "no time bombs" testing
standard and lets AC-011 assert `T0` preserved / `T1` updated deterministically.

**Alternatives considered.** (a) `SELECT` then `INSERT`/`UPDATE` — rejected: two round trips
and a race. (b) A second query to test existence before the upsert — rejected for the same
reason; `xmax` is free. (c) Storing `email_verified` as `NOT NULL DEFAULT false` — rejected:
it would erase the distinction between "IdP asserted false" and "IdP asserted nothing", which
is exactly what 7.1 asks to record.

**Consequences.** The `bool` in the signature is load-bearing for a security control, not a
convenience. The mock in `internal/testutil` must reproduce the semantics faithfully.

---

### AD-017: No IdP token, in any form, outside the callback stack frame

**Decision.** The `*oauth2.Token` and the raw ID token string are local variables in
`Service.CompleteLogin` and are never returned, logged, stored, or placed in a cookie. The
function's return type is `(oidc.Claims, error)` — a struct with no token field, so the
compiler enforces BR-005/FR-025/AC-023. No refresh token is requested (`offline_access` is
not in the scope set) and none is retained if the IdP volunteers one.

**Rationale.** D-1/BR-005. Making the return type incapable of carrying a token is stronger
than a code review rule.

---

### AD-018: No service worker exists — do not add a navigation bypass

**Decision.** Verified across `web/`: there is no `navigator.serviceWorker.register`, no
`vite-plugin-pwa`/`workbox` dependency, no `sw.js`/`ngsw-config.json`, and `web/public/`
holds only icons plus `site.webmanifest`. **No navigation-bypass fix is required or
permitted by this story.** If a service worker is ever introduced to rIOt, its navigation
handling must exclude `/api/**` (`app_standards.md` §4 rule 9) or the SSO button will
silently serve the cached app shell instead of redirecting.

**Rationale.** The house lesson exists and is real, but applying a fix for a service worker
that does not exist is cargo-cult work that future readers cannot evaluate. Recording the
absence explicitly is the useful artefact.

---

### AD-019: `SetupGuard` allows the three OIDC paths by exact match

*(Amended in rev 2 for SEC-011: exact paths, not a prefix.)*

**Decision.** `internal/server/middleware/setup.go` allows exactly these three paths through
to the handlers while setup is incomplete, alongside the existing `/api/v1/auth/check` entry:

```
/api/v1/auth/oidc
/api/v1/auth/oidc/start
/api/v1/auth/oidc/callback
```

Comparison is `path == …`, not `strings.HasPrefix`. The handlers then produce the AC-020
responses: `200 {"available":false,"label":""}` for availability, `404` for `/start` and
`/callback`.

**Rationale.** Without this entry, the guard returns `503 {"error":"setup_required"}` for all
three endpoints, which fails AC-020's explicit requirement of `{"available": false}` and
`404`. Allowing the paths through is safe because both gated endpoints check
`isSetupComplete()` themselves and refuse to mint a session (AC-020's last clause). The
security review traced the rev-1 prefix form and found **no reachable bypass**
(`/api/v1/auth/oidcanything` and `/api/v1/auth/oidc/../login` both fall to the frontend
catch-all, and `embed.FS.Open` rejects the traversal) — exact matching is adopted anyway
because a prefix allowlist inside an auth-adjacent guard is a shape that ages badly.

**Alternatives considered.** Loosening the guard to `/api/v1/auth/` wholesale — rejected: it
would expose `POST /login` during setup, a behaviour change well outside this story.

---

### AD-020: Client identity comes from the TCP peer unless an explicitly trusted proxy says otherwise

*(New in rev 2 — SEC-002, and a prerequisite for AD-008's `Secure` derivation and AD-009.
Scope claim narrowed in rev 3 — re-review condition C6. All-trusted chain specified in
rev 4 — re-review condition C5.)*

**Decision.** Introduce a single place where **client identity and request scheme** are
derived from forwarding headers, and remove the unconditional derivation that exists today.

1. **New config.** `RIOT_TRUSTED_PROXIES` — a comma-separated list of CIDR blocks (bare IPs
   accepted and treated as `/32` / `/128`). **Default empty = trust nobody.** Read as a raw
   trimmed string by `LoadConfig` (AD-003); parsed by
   `middleware.ParseTrustedProxies(raw string) *TrustedProxies` in `setupRouter()`. An entry
   that fails to parse is **dropped** with `slog.Warn`, never aborting boot; if every entry
   is bad the result is the empty (trust-nobody) set. Fails closed by construction.
2. **New middleware, replacing `chimw.RealIP`.** `middleware.RealIP(tp *TrustedProxies)`
   becomes the first global middleware in `setupRouter()`; `r.Use(chimw.RealIP)` is
   **deleted**. Behaviour:
   - Resolve the TCP peer from `r.RemoteAddr`.
   - If `tp` is nil/empty **or** the peer is not inside any trusted prefix: change nothing.
     `X-Forwarded-For`, `X-Real-IP`, `True-Client-IP`, and `X-Forwarded-Proto` are all
     ignored.
   - If the peer *is* trusted: walk `X-Forwarded-For` right-to-left, skipping entries that
     are themselves trusted, and take the first untrusted address as the client; rewrite
     `r.RemoteAddr` to that bare IP (matching chi's existing convention). Additionally, if
     `X-Forwarded-Proto` is exactly `http` or `https`, record it in the request context.
   - **If every entry in the chain is trusted (or the header is absent/unparseable): keep the
     immediate peer's `RemoteAddr` unchanged.** No entry is promoted to "the client".

**All-trusted chain (C5).** The fallback above is deliberate. An all-trusted chain means the
operator has listed CIDRs broad enough to cover the real clients too — the configuration is
wrong, and the one address rIOt can still actually vouch for is the TCP peer it is talking to.
Falling back to the peer fails closed in the same direction as the trust-nobody default
(clients collapse onto one throttle bucket — over-throttling, never under-throttling), and it
keeps a single invariant across every branch: *the resolved client is either an address an
explicitly trusted proxy vouched for, or the peer itself.* Promoting the leftmost entry
instead would hand the key back to whoever wrote the header, which is the exact failure
SEC-002 exists to close.

3. **Two exported accessors, used everywhere.**
   - `middleware.ClientIP(r *http.Request) string` — `net.SplitHostPort(r.RemoteAddr)` with a
     fallback to the raw value. Used by `RateLimiter.Middleware`,
     `RateLimiter.MiddlewareRedirect`, and the OIDC handlers' audit logging.
   - `middleware.RequestScheme(r *http.Request) string` — the context value set in step 2 if
     present; else `https` when `r.TLS != nil`; else `http`. Used by `requestOrigin` (AD-009),
     the `riot_session` `Secure` flag (AD-008), and the `riot_oidc_tx` `Secure` flag (AD-005).

**Scope of the claim (C6).** "Single place" means *client identity and request scheme* — the
two values this story's controls depend on. It is **not** a claim that no other code in rIOt
reads a forwarding header. One other consumer exists and is deliberately untouched:
`internal/server/handlers/setup.go` reads `r.Header.Get("X-Real-Ip")` directly (lines 123 and
223) to add an IP SAN to the generated self-signed TLS certificate. That value is
`net.ParseIP`-validated, and an attacker-chosen SAN inside rIOt's own certificate is worthless
without the private key, so the impact is genuinely low — but it is a real residual and must
not be read out of this ADD as a guarantee. `setup.go` is listed in §4.5 as explicitly not
changed by this story; the residual is carried to the follow-up story alongside SEC-009 and
SEC-010 (§14). **Do not fix `setup.go` in this story.**

**Rationale.** `chimw.RealIP` rewrites `r.RemoteAddr` from client-supplied headers with no
trust configuration at all. In rIOt's documented deployment — direct exposure on `:7331` with
no reverse proxy (D-8, NFR-010) — that makes the rate-limit key, the access-log `remote`
field, and the NFR-009 SSO audit IP all attacker-chosen strings. The consequences are
concrete and not confined to this story: `loginLimiter` never engages against an attacker
who increments `X-Forwarded-For`, leaving the single shared bcrypt-protected admin password
brute-forceable at CPU speed; and an attacker can deliberately drain *both* the operator's
password and SSO buckets, defeating the D-9 fallback that AD-013's separate-bucket design
exists to preserve.

Rev 1 recorded this as accepted pre-existing risk. That was wrong: this story *introduces*
NFR-008 and AC-027, whose stated property is a per-client-IP throttle. Shipping a control
that passes a unit test manipulating `RemoteAddr` and does nothing in production is worse than
shipping none, because QA signs it off. House standard `app_standards.md` §4 (Hardening,
rule 9) already requires explicit proxy trust "applied before auth and rate limiting so
IP-keyed throttles see real client IPs", and pre-dates this story.

Replacing the global middleware rather than special-casing the OIDC limiter is the only
coherent option: the rewrite happens before any route is selected, so a local fix would leave
`loginLimiter` and `registerLimiter` broken while claiming the property is held. Mutating
`r.RemoteAddr` (rather than threading a new context value through every consumer) keeps
`ratelimit.go`'s keying and `logger.go`'s `remote` field correct with no change to their
logic, and makes the diff a drop-in swap that a reviewer can check in one pass.

**Alternatives considered.** (a) Leave `chimw.RealIP` and key the OIDC limiter on a separate
unforgeable value — rejected: two contradictory notions of "client IP" in one binary, and
the pre-existing password brute-force stays open. (b) Configure a trusted-proxy *count* (hop
limit) instead of CIDRs — rejected: a count is guessable and does not express "only my
nginx". (c) Drop forwarding-header support entirely — rejected: it would break legitimate
reverse-proxy deployments with no opt-out, and the house standard asks for explicit trust,
not no trust. (d) Make `RIOT_TRUSTED_PROXIES` required — rejected: FR-008 forbids any new
required variable; the safe default is the empty set. (e) On an all-trusted chain, promote the
leftmost entry — rejected under C5 above.

**Consequences.**
- Recorded as §0 amendment 2. Throttling *policy* is untouched (§9 item 12 holds).
- **Behaviour change for proxied deployments, in two respects.** Until `RIOT_TRUSTED_PROXIES`
  is set, an operator behind a TLS-terminating proxy gets (i) **no `Secure` attribute on
  `riot_session`** — `X-Forwarded-Proto` is ignored, `r.TLS` is nil, so `RequestScheme`
  resolves `http` — and (ii) every client keyed to the proxy address in the throttle and the
  logs. (i) is the more important of the two and leads the operator guidance in §4.5 item 6
  and `.env.example`. Both fail safe (no protection lost that existed before; over-throttling,
  not under-throttling), and neither can lock anyone out (AD-008).
- NFR-009's logged client IP becomes trustworthy in the default shape, which also makes the
  persisted `WARN` rows from AD-021 meaningful.
- `X-Forwarded-Proto` spoofing (AD-009) is closed as a side effect.

---

### AD-021: The first admission of a new identity is loud

*(New in rev 2 — SEC-003 item 1.)*

**Decision.** When `RecordLogin` reports `firstSeen == true`, the callback emits, in addition
to the ordinary success entry:

```go
slog.Warn("new SSO identity granted admin",
    "issuer", claims.Issuer,
    "subject", claims.Subject,
    "ip", middleware.ClientIP(r))
```

`WARN` is chosen deliberately: `logstore.DBHandler` persists `WARN` and above to
`server_logs`, so the entry survives in the database and is visible in **Settings → Logs**,
where an operator will actually encounter it. `INFO` would go to stdout only and be lost
among request logs. The email claim is **not** included (§12 note 11 never-log list). A repeat
login emits only the ordinary `INFO` success entry.

**Rationale.** BR-002 grants full rIOt admin — which is an interactive container shell on
every enrolled device, plus device-key rotation, bootstrap-key issuance, and fleet-wide bulk
update/patch — to any identity authentik authorizes, and FR-026 forbids an rIOt-side
allowlist. Delegation is the correct model and is not being changed. The problem SEC-003
identifies is the *direction the default fails in*: an authentik application with zero policy
bindings is accessible to every authenticated user, and the implicit-consent flow makes
admission silent. The residual risk is acceptable only if the wrong outcome is loud. This is
the cheapest possible way to make it loud, inside machinery the story already builds. (The
registration script now creates the policy binding when `--group` is passed — §4.5 item 3 —
which raises the floor, but the binding is still an operator action that can be omitted, so
the observability control stands on its own.)

This is a **log, not a rIOt event** — OQ-3's decided answer ("no event for this story") is
respected, and no `events` row, notification, or WebSocket broadcast is created.

**Alternatives considered.** (a) An rIOt-side allowlist — rejected: contradicts FR-026 and
§9 item 2 and would require an FRD revision. (b) A rIOt event + notification — rejected by
OQ-3; a good follow-up story, exactly as the FRD suggests. (c) `INFO` level — rejected: not
persisted, therefore not surfaced, therefore not loud.

**Consequences.** An operator adding a second legitimate admin will see one `WARN` per new
person. That is the intended signal-to-noise ratio: rare, meaningful, and self-explanatory.

---

## 4. Component Changes

### 4.1 Server — new files

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `internal/server/oidc/config.go` | `Options`, `Service` construction, dormancy rules (FR-002), issuer URL validation (V-005), label default + 64-rune truncation (FR-003, OQ-8), `Enabled()`, `ButtonLabel()`, `DeriveTransactionKey` invocation |
| CREATE | `internal/server/oidc/service.go` | Lazy cached discovery (AD-004), `BeginLogin(ctx, redirectURI, returnPath, now) (authURL string, tx Transaction, err error)`, `CompleteLogin(ctx, redirectURI, code string, tx Transaction) (Claims, error)`, 10 s HTTP client (NFR-003), TLS verification left at system defaults (NFR-007) |
| CREATE | `internal/server/oidc/transaction.go` | `Transaction` struct, `DeriveTransactionKey(jwtSecret []byte) []byte` (AD-005), `Encode(txKey []byte) (string, error)`, `DecodeTransaction(raw string, txKey []byte, now time.Time) (Transaction, error)` with MAC-before-decode ordering, exported sentinels `ErrTransactionMAC` / `ErrTransactionMalformed` / `ErrTransactionExpired`, cookie name/path/TTL constants |
| CREATE | `internal/server/oidc/identity.go` | `Claims` struct, `ValidateClaims` (V-001), `SafeReturnPath` (V-004) — pure, no I/O |
| CREATE | `internal/server/oidc/errors.go` | `LoginError{Code, Reason string; Err error}`, the four `Code` constants, the fixed `Reason` constants from AD-010, `Unwrap()` |
| CREATE | `internal/server/oidc/config_test.go` | AC-003, AC-004, V-005, OQ-8 |
| CREATE | `internal/server/oidc/identity_test.go` | AC-026, V-001 table tests |
| CREATE | `internal/server/oidc/transaction_test.go` | AC-015, AC-016, SEC-004 (a `riot_session` JWT presented as `riot_oidc_tx` fails at `ErrTransactionMAC`), key-derivation determinism, expiry boundary |
| CREATE | `internal/server/oidc/service_test.go` | AC-006, AC-008, AC-014, AC-017 against the stub issuer |
| CREATE | `internal/server/oidc/testidp_test.go` | Test-only stub issuer: `httptest.Server` serving `/.well-known/openid-configuration`, `/token`, `/keys`; in-test RSA key; RS256 ID-token minting with controllable `nonce`, `iss`, `aud`, `exp`, `sub`, `email`, `email_verified`; a "hard down" mode that closes immediately |
| CREATE | `internal/server/handlers/oidc.go` | `OIDCAvailability`, `OIDCStart`, `OIDCCallback`; `requestOrigin(r)` (AD-009), `ssoErrorRedirect(w, r, code)`, structured audit logging (NFR-009) and the AD-021 `WARN` |
| CREATE | `internal/server/handlers/oidc_handler_test.go` | AC-001, AC-002, AC-005, AC-006, AC-008, AC-010 – AC-018, AC-020, AC-021, AC-023, AC-024, AC-026, AC-028, SEC-003 |
| CREATE | `internal/server/middleware/clientip.go` | AD-020: `TrustedProxies`, `ParseTrustedProxies`, `RealIP(tp)`, `ClientIP(r)`, `RequestScheme(r)` |
| CREATE | `internal/server/middleware/clientip_test.go` | SEC-002: spoofed `X-Forwarded-For` / `X-Real-IP` / `True-Client-IP` ignored from an untrusted peer; honoured from a trusted peer; rightmost-untrusted selection; **all-trusted chain falls back to the peer (C5)**; absent/unparseable header falls back to the peer; malformed CIDR dropped with the rest of the list still working; `RequestScheme` trust gating |
| CREATE | `internal/server/db/external_identity_repo.go` | `ExternalIdentityRepo`, `NewExternalIdentityRepo(db)`, `RecordLogin(ctx, models.ExternalIdentity) (bool, error)` (AD-016) |
| CREATE | `internal/models/identity.go` | `models.ExternalIdentity` |
| CREATE | `internal/server/middleware/setup_test.go` | AC-020 guard passthrough, exact-path matching (no such file today) |
| CREATE | `cmd/riot-server/migrations/000022_external_identities.up.sql` | §5 DDL |
| CREATE | `cmd/riot-server/migrations/000022_external_identities.down.sql` | `DROP TABLE IF EXISTS external_identities;` |

### 4.2 Server — modified files

| Action | File Path | Change |
|---|---|---|
| MODIFY | `internal/server/config.go` | Add `OIDCIssuerURL`, `OIDCClientID`, `OIDCClientSecret`, `OIDCButtonLabel`, `TrustedProxies` to `Config`; read + `strings.TrimSpace` all five in `LoadConfig` (AD-003, AD-020). No validation here, no logging of the secret |
| MODIFY | `internal/server/config_test.go` | AC-003, AC-030: absent vars leave all fields empty; whitespace-only values trim to empty; a set trio populates verbatim; `RIOT_TRUSTED_PROXIES` absent → empty |
| MODIFY | `internal/server/server.go` | Add `ExternalIdentityRepo *db.ExternalIdentityRepo` to `Server`; construct it in `Start()` alongside the other repos |
| MODIFY | `internal/server/router.go` | **Delete `r.Use(chimw.RealIP)`** and replace with `r.Use(middleware.RealIP(trustedProxies))` where `trustedProxies := middleware.ParseTrustedProxies(s.Config.TrustedProxies)` (AD-020); construct `oidcSvc := oidc.New(...)` from `s.Config` + `s.JWTSecret`; add `OIDC`, `ExternalIdentityRepo`, `SetupComplete: &s.SetupComplete` to `HandlerDeps`; add `oidcLimiter := middleware.NewRateLimiter(5, 5)`; register in the existing `/api/v1/auth` group: `r.Get("/oidc", h.OIDCAvailability)`, `r.With(oidcLimiter.MiddlewareRedirect("/?sso_error=sso_failed")).Get("/oidc/start", h.OIDCStart)`, same wrapper for `.Get("/oidc/callback", h.OIDCCallback)`. Remove the now-unused `chimw` import only if nothing else uses it (`chimw.Recoverer` does — keep the import) |
| MODIFY | `internal/server/handlers/handlers.go` | `HandlerDeps` + `Handlers` gain `OIDC *oidc.Service`, `ExternalIdentityRepo db.ExternalIdentityRepository`, `SetupComplete *atomic.Bool`; wire in `New()`; add `isSetupComplete()` (AD-012) |
| MODIFY | `internal/server/handlers/auth.go` | Extract `issueSessionCookie(w, r, now)` / `clearSessionCookie(w, r)`; `Login` and `Logout` call them; `SameSite` → `http.SameSiteLaxMode`; `Secure: middleware.RequestScheme(r) == "https"` (AD-008) |
| MODIFY | `internal/server/handlers/auth_handler_test.go` | Assert `SameSite == http.SameSiteLaxMode` **and** `Secure` true over `https` / false over `http` on login and logout cookies; assert the SSO-minted cookie and the password-minted cookie share name/Path/HttpOnly/SameSite/Secure/MaxAge for the same request shape and both parse to `sub: "admin"` (AC-008, AC-009, AC-022, SEC-001) |
| MODIFY | `internal/server/middleware/setup.go` | Allow the three exact OIDC paths through the guard (AD-019). **Do not touch the `X-Real-Ip` reads in `handlers/setup.go`** — different file, out of scope (AD-020 scope note) |
| MODIFY | `internal/server/middleware/ratelimit.go` | Add `MiddlewareRedirect(location string)` (AD-013); change `Middleware()` and the new adapter to key on `ClientIP(r)` (AD-020). Do not change `allow()`, `cleanup()`, the constructor, or any rate/burst value |
| MODIFY | `internal/server/middleware/ratelimit_test.go` | AC-027 + SEC-002: `MiddlewareRedirect` throttles by IP after the burst and answers `302` to the given location, not `429`; **a spoofed `X-Forwarded-For` from an untrusted peer does not create a fresh bucket** |
| MODIFY | `internal/server/db/interfaces.go` | Add `ExternalIdentityRepository` interface (returning `(bool, error)`) + `_ ExternalIdentityRepository = (*ExternalIdentityRepo)(nil)` conformance assertion |
| MODIFY | `internal/testutil/mocks.go` | Add `MockExternalIdentityRepo` implementing the upsert semantics in memory (preserves `first_login_at`, overwrites `last_login_at`/`email`/`email_verified`, returns `firstSeen` correctly), with a settable `Err` following the existing mock idiom |
| MODIFY | `go.mod` / `go.sum` | `github.com/coreos/go-oidc/v3`; `golang.org/x/oauth2` ≥ v0.27.0; explicit `github.com/go-jose/go-jose/v4` ≥ v4.0.5 (AD-002) |

### 4.3 Frontend — files

| Action | File Path | Change |
|---|---|---|
| MODIFY | `web/src/api/client.ts` | Add `export interface SSOAvailability { available: boolean; label: string }` and `getSSOAvailability: () => fetchJSON<SSOAvailability>(`${BASE}/auth/oidc`)` |
| MODIFY | `web/src/api/demo-client.ts` | Add a matching `getSSOAvailability` returning `Promise.resolve({ available: false, label: '' })` — required or the demo build fails to typecheck (the `api/client` → `api/demo-client` alias in `vite.config.ts`) |
| MODIFY | `web/src/pages/Login.tsx` | `useQuery(['sso-availability'], api.getSSOAvailability, { retry: false, staleTime: Infinity, enabled: !isDemo })`; render the anchor button only when `data?.available === true`; read/clear `sso_error` via `useSearchParams` and render the mapped message (AD-014); password form untouched and always rendered |
| CREATE | `web/src/pages/Login.test.tsx` | AC-001, AC-005, AC-007, AC-018, AC-025 |

### 4.4 Configuration / infrastructure

| Action | File Path | Change |
|---|---|---|
| MODIFY | `.env.example` | Add a commented block for `RIOT_OIDC_ISSUER_URL`, `RIOT_OIDC_CLIENT_ID`, `RIOT_OIDC_CLIENT_SECRET`, `RIOT_OIDC_BUTTON_LABEL` with the authentik per-application issuer shape as the example value, placeholder secrets only, and a one-line "the IdP decides who gets in — bind the application to a group" warning (BR-003). Add a separate commented `RIOT_TRUSTED_PROXIES` entry whose guidance leads with the security consequence: **default empty means forwarding headers are ignored, so behind a TLS-terminating reverse proxy the session cookie is issued without `Secure` until this is set** (`X-Forwarded-Proto` is ignored, `r.TLS` is nil, the scheme resolves to `http`); secondarily, per-client rate limiting and audit IPs collapse onto the proxy address. Set it to the proxy's CIDR if and only if rIOt sits behind a reverse proxy; leave it unset for direct exposure; never `0.0.0.0/0` (AD-020) |
| MODIFY | `docker-compose.yml` | Add the same five vars as commented lines in the `riot-server` `environment:` block |
| MODIFY | `docker-compose.prod.yml` | Add the five vars as commented `${RIOT_*}` passthrough lines in the `riot-server` `environment:` block |

### 4.5 Operator documentation requirements (A-9 — technical writer deliverable, QA-verified)

*(New in rev 2 — SEC-003 item 2 and SEC-008. Items 3 and 5 rewritten in rev 3 for re-review
conditions C1/C2/C3 against the updated `register_oidc_app.py`; item 6 rewritten in rev 4 for
condition C4. These are requirements on the documentation, not on the code. QA must confirm
the text exists before sign-off; the technical writer owns the wording.)*

The enable-SSO runbook (README + `docs/`) must contain, in the imperative:

1. **The authentik application-to-group binding is a REQUIRED step, not a recommendation.**
   An authentik application with zero policy bindings is accessible to **every authenticated
   authentik user**, and any user who reaches rIOt through SSO receives full admin — which
   includes an interactive container shell on every enrolled device. The binding must exist
   **before** the three `RIOT_OIDC_*` variables are written to `.env`.
2. **A named verification instruction:** sign in with a test account that is *not* in the
   bound group and confirm authentik refuses it (rIOt shows `sso_denied`). Do this before
   enabling SSO for real.
3. **Register the application with the group flag — one command.**
   `D:\Repos\Hawaii\scripts\register_oidc_app.py` is the sanctioned registration path
   (`app_standards.md` §4 OIDC rule 10) and it creates the `PolicyBinding` for you when
   `--group` is passed, creating the group first if it does not exist. The runbook's command
   for rIOt is:

   ```
   python register_oidc_app.py riot \
     --redirect-uri https://10.0.10.11:7331/api/v1/auth/oidc/callback \
     --launch-url  https://10.0.10.11:7331/ \
     --group       "riot admins"
   ```

   Substitute the origin rIOt is actually reached at. `--group` is REQUIRED for rIOt — when
   it is omitted the script prints a loud `WARNING` that an unbound application is granted to
   every authenticated user, and that warning is **not** a substitute for creating the
   binding; it is a prompt to re-run with the flag or add the binding in the admin UI. The
   script emits house-idiom `OIDC_*` keys; rIOt reads `RIOT_OIDC_*`, so the three values must
   be re-prefixed when they are copied into `.env`.
4. **Redirect URIs must use `matching_mode: strict`, with one explicitly enumerated entry per
   scheme + host + port that rIOt is reached by** — for example
   `https://10.0.10.11:7331/api/v1/auth/oidc/callback`. rIOt derives the redirect URI from the
   incoming request (D-8, AD-009), and strict matching at the IdP is the *only* thing
   containing a poisoned `Host` header. The script registers everything it creates as
   `strict`; keep it that way.
5. **Regex and wildcard redirect matching are prohibited**, and getting the redirect URI right
   on the first run avoids the temptation. Without `--redirect-uri` the script defaults to
   `https://<slug>.rbretschneider.com/api/v1/auth/oidc/callback`, which will **not** match
   rIOt's direct-exposure shape on `:7331`. The operator's first SSO attempt then fails with
   an IdP redirect-mismatch error, and the path of least resistance in the authentik UI —
   switching `matching_mode` to `regex` — destroys AD-009's only mitigation and violates
   `app_standards.md` §4 OIDC rule 8. The runbook must pre-empt this and must also record the
   re-run behaviour: re-running the script with `--redirect-uri` **appends** the correct URI
   to an existing provider (it does not replace the list), so after fixing a wrong first run
   the operator must delete the stale entry by hand in the authentik UI to keep item 4's
   "exactly the reachable origins" property.
6. **`RIOT_TRUSTED_PROXIES` — set it when, and only when, rIOt sits behind a reverse proxy,
   and understand that it is what turns `Secure` on.** With the variable unset, rIOt ignores
   `X-Forwarded-Proto`; behind a TLS-terminating proxy that means `r.TLS` is nil, the request
   scheme resolves to `http`, and **`riot_session` is issued without the `Secure` attribute**
   — the browser will then attach the admin session to a cleartext `http://` request to the
   same host, which is exactly the exposure SEC-001 closed for the direct-exposure shape.
   Setting `RIOT_TRUSTED_PROXIES` to the proxy's CIDR restores it. Secondary, but also true:
   until it is set, every client shares one rate-limit bucket and every logged client IP is
   the proxy's, rather than the real client's. Leave it unset for direct exposure on `:7331`
   (where rIOt terminates its own TLS and `Secure` is already set correctly). Never set it to
   `0.0.0.0/0` — that hands the throttle key and the audit IP back to whoever writes the
   header.
7. **The three `RIOT_OIDC_*` values are secrets in env** (SEC-013): `.env` must not be
   committed, and `.env.example` carries placeholders only.

**Explicitly not changed by this story:** `internal/server/handlers/setup.go` (including its
`X-Real-Ip` reads — AD-020 scope note), `middleware/admin_auth.go`, `middleware/cors.go`,
`middleware/wsorigin.go`, `middleware/logger.go`, `db/admin_repo.go`, any migration ≤
`000021`, `web/src/App.tsx`, `web/src/hooks/useAuth.ts`, `web/src/types/models.ts`, the
`/debug/pprof` mount (SEC-009), security headers (SEC-010), and
`docs/security/FLEET-DASH-security-review.md`.

---

## 5. Data Model Changes

### 5.1 New table (additive; nothing existing is altered — NFR-011)

`cmd/riot-server/migrations/000022_external_identities.up.sql`:

```sql
CREATE TABLE IF NOT EXISTS external_identities (
    id             BIGSERIAL PRIMARY KEY,
    issuer         TEXT NOT NULL,
    subject        TEXT NOT NULL,
    email          TEXT,
    email_verified BOOLEAN,
    first_login_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT external_identities_issuer_subject_key UNIQUE (issuer, subject)
);
```

`cmd/riot-server/migrations/000022_external_identities.down.sql`:

```sql
DROP TABLE IF EXISTS external_identities;
```

No additional index: the `UNIQUE (issuer, subject)` constraint's implicit B-tree is the only
access path this table has (AD-016, §11). No foreign key: rIOt has no user table (BR-006),
and A-8 requires the rows to be deletable without affecting logins.

### 5.2 New Go types

`internal/models/identity.go`:

```go
type ExternalIdentity struct {
    Issuer        string
    Subject       string
    Email         *string
    EmailVerified *bool
    LoginAt       time.Time // injected clock — used for both first_login_at and last_login_at
}
```

`internal/server/db/interfaces.go`:

```go
type ExternalIdentityRepository interface {
    // RecordLogin upserts the identity and reports whether this (issuer, subject)
    // had never been seen before. firstSeen drives the AD-021 admission warning and
    // is only meaningful when err == nil.
    RecordLogin(ctx context.Context, ident models.ExternalIdentity) (firstSeen bool, err error)
}
```

Repository SQL (the whole repository — this table is otherwise write-only, A-8):

```sql
INSERT INTO external_identities (issuer, subject, email, email_verified, first_login_at, last_login_at)
VALUES ($1, $2, $3, $4, $5, $5)
ON CONFLICT (issuer, subject) DO UPDATE
SET email          = EXCLUDED.email,
    email_verified = EXCLUDED.email_verified,
    last_login_at  = EXCLUDED.last_login_at
RETURNING (xmax = 0) AS inserted
```

Read with `QueryRow(...).Scan(&firstSeen)`. `xmax = 0` is true for a tuple produced by the
insert and false for one produced by the conflict update — insert-vs-update in the same round
trip, preserving AD-016's no-race property.

### 5.3 Ephemeral structure (never persisted to the database — 7.1)

`internal/server/oidc/transaction.go`:

```go
type Transaction struct {
    State        string `json:"s"`
    Nonce        string `json:"n"`
    CodeVerifier string `json:"v"`
    ReturnPath   string `json:"r"`
    IssuedAt     int64  `json:"t"`
}
```

Cookie `riot_oidc_tx`, value `base64url(JSON) + "." + base64url(HMAC-SHA256(txKey, …))`
where `txKey = HMAC_SHA256(jwtSecret, "riot-oidc-tx-v1")` (AD-005),
`Path=/api/v1/auth/oidc`, `HttpOnly`, `SameSite=Lax`, `Secure` iff
`middleware.RequestScheme(r) == "https"`, `MaxAge=300`.

### 5.4 Modified structures (no schema impact)

| Struct | Before | After |
|---|---|---|
| `server.Config` | …`SetupComplete bool` | + `OIDCIssuerURL`, `OIDCClientID`, `OIDCClientSecret`, `OIDCButtonLabel`, `TrustedProxies string` |
| `handlers.HandlerDeps` / `handlers.Handlers` | …`AdminPasswordHash` | + `OIDC *oidc.Service`, `ExternalIdentityRepo db.ExternalIdentityRepository`, `SetupComplete *atomic.Bool` |
| `server.Server` | …`DeviceProbeRepo` | + `ExternalIdentityRepo *db.ExternalIdentityRepo` |

---

## 6. API / Interface Contract

All three endpoints are `GET`, live under the existing public `/api/v1/auth` group, and
require no `riot_session` cookie (FR-009).

### 6.1 `GET /api/v1/auth/oidc` — availability

- **Request:** no headers, params, or body. Never contacts the IdP (FR-011).
- **Throttling:** none (AD-013).
- **`200 OK`** — the only status this endpoint ever returns:

```json
{ "available": true, "label": "Sign in with authentik" }
```

```json
{ "available": false, "label": "" }
```

`available` is `true` only when SSO is configured **and** `isSetupComplete()` (FR-010,
FR-029). When `available` is `false`, `label` is always the empty string — the configured
label is never disclosed for a dormant or setup-incomplete server. `Content-Type:
application/json` via the existing `writeJSON`. The `label` is env-sourced only and must
never be derived from a request parameter (SEC-012).

### 6.2 `GET /api/v1/auth/oidc/start` — login initiation

- **Query:** `returnUrl` *(optional, string)* — same-origin absolute path; anything else is
  replaced with `/` (FR-016, V-004, AC-026).
- **Throttling:** 5/min, burst 5, per client IP as resolved by AD-020.

| Status | Condition | Headers / body |
|---|---|---|
| `302` | Success | `Location:` the discovered `authorization_endpoint` with `response_type=code`, `client_id`, `redirect_uri`, `scope=openid email profile`, `state`, `nonce`, `code_challenge`, `code_challenge_method=S256`. `Set-Cookie: riot_oidc_tx=…; Path=/api/v1/auth/oidc; HttpOnly; SameSite=Lax; Max-Age=300[; Secure]`. Empty body |
| `302` | Discovery/RNG failure | `Location: /?sso_error=sso_unavailable`. No cookie. Empty body |
| `302` | Throttled | `Location: /?sso_error=sso_failed`. No cookie |
| `404` | SSO dormant, or setup incomplete | `{"error":"not found"}`. No cookie (AC-002) |

`redirect_uri` is exactly `requestOrigin(r) + "/api/v1/auth/oidc/callback"` — e.g.
`https://riot.example:7331/api/v1/auth/oidc/callback` (AC-006).

### 6.3 `GET /api/v1/auth/oidc/callback` — IdP return

- **Query (from the IdP):** `code`, `state`, or `error` + optional `error_description`.
- **Cookie in:** `riot_oidc_tx`.
- **Throttling:** 5/min, burst 5, per client IP as resolved by AD-020.

| Status | Condition | Headers / body |
|---|---|---|
| `302` | Success | `Location: <transaction.ReturnPath>` (`/` unless a safe `returnUrl` was supplied). Two `Set-Cookie` headers: `riot_session=<jwt>; Path=/; HttpOnly; SameSite=Lax; Max-Age=86400[; Secure]` and the `riot_oidc_tx` clear (`Max-Age=-1`). Empty body |
| `302` | Any failure in the AD-010 table | `Location: /?sso_error=<code>`. `riot_oidc_tx` cleared (FR-022, AC-015). No `riot_session`. Empty body |
| `404` | SSO dormant, or setup incomplete | `{"error":"not found"}`. No cookie |

**`Secure` on `riot_session`** *(footnote replaced in rev 2 — SEC-001)*: present whenever
`middleware.RequestScheme(r) == "https"` and omitted otherwise, applied identically here and
in `POST /api/v1/auth/login` / `POST /api/v1/auth/logout` by the single shared helper
(AD-008). This is a deliberate, recorded amendment to FR-031 (§0 amendment 1), not an
incidental change; over plain HTTP the attribute is absent so NFR-010's LAN mode is
unaffected. Behind a TLS-terminating proxy the scheme resolves to `https` — and therefore
`Secure` is set — only once `RIOT_TRUSTED_PROXIES` names that proxy (AD-020, §4.5 item 6).

The response body is **never** JSON, HTML, a stack trace, or a raw IdP error string
(FR-024, AC-013).

### 6.4 Internal Go interfaces

```go
// internal/server/oidc
func New(o Options) *Service
func (s *Service) Enabled() bool          // nil-safe
func (s *Service) ButtonLabel() string    // "" when dormant
func (s *Service) BeginLogin(ctx context.Context, redirectURI, returnPath string, now time.Time) (authURL string, tx Transaction, err error)
func (s *Service) CompleteLogin(ctx context.Context, redirectURI, code string, tx Transaction) (Claims, error)

func SafeReturnPath(raw string) string
func ValidateClaims(c Claims) error

func DeriveTransactionKey(jwtSecret []byte) []byte
func (t Transaction) Encode(txKey []byte) (string, error)
func DecodeTransaction(raw string, txKey []byte, now time.Time) (Transaction, error)

var ErrTransactionMAC, ErrTransactionMalformed, ErrTransactionExpired error

// internal/server/middleware
func ParseTrustedProxies(raw string) *TrustedProxies
func RealIP(tp *TrustedProxies) func(http.Handler) http.Handler
func ClientIP(r *http.Request) string
func RequestScheme(r *http.Request) string
func (rl *RateLimiter) MiddlewareRedirect(location string) func(http.Handler) http.Handler
```

Every error returned by `BeginLogin`/`CompleteLogin`/`DecodeTransaction` is a
`*LoginError` carrying a §7.4 `Code` and an AD-010 `Reason`; the transaction sentinels are
wrapped inside it and reachable with `errors.Is`.

---

## 7. Sequence / Flow

### 7.1 Login screen mount

1. `App` renders `<Login>` because `authenticated === false`.
2. `Login` mounts. TanStack Query issues `GET /api/v1/auth/oidc` (`retry: false`).
3. Handler: `available = h.oidc.Enabled() && h.isSetupComplete()`; `label` = the effective
   label when available, else `""`. `writeJSON(200, …)`. No DB read, no network call.
4. `data.available === true` → render the anchor; anything else (including a rejected query
   or a timeout) → render nothing extra. The password form renders unconditionally in all
   cases (FR-035, AC-025).
5. If `searchParams.has('sso_error')`, map the code to a message, render it, and
   `setSearchParams(withoutSSOError, { replace: true })`.

### 7.2 `/start`

1. Browser performs a top-level navigation to `/api/v1/auth/oidc/start` (anchor click).
2. `middleware.RealIP(tp)` resolves the client identity (AD-020). `SetupGuard` passes the
   path through (AD-019). `oidcLimiter` admits or redirects.
3. Handler: if `!Enabled() || !isSetupComplete()` → `404`, stop.
4. `returnPath := oidc.SafeReturnPath(r.URL.Query().Get("returnUrl"))`.
5. `redirectURI := requestOrigin(r) + "/api/v1/auth/oidc/callback"`.
6. `Service.BeginLogin`: `discover(ctx)` (10 s budget) → on failure return
   `sso_unavailable`/`discovery_failed`; generate `state`, `nonce`, verifier; build the
   authorization URL.
7. On error: `slog.Warn` the attempt with `reason` and IP, then `302 /?sso_error=<code>`, stop.
8. Encode the transaction with `txKey`, set `riot_oidc_tx`, `302` to the authorization URL.

### 7.3 `/callback` — happy path

1. authentik redirects the browser to `…/api/v1/auth/oidc/callback?code=…&state=…`.
2. `RealIP` resolves the client; `SetupGuard` passes; `oidcLimiter` admits.
3. Handler: `404` gate as in 7.2.
4. Read `riot_oidc_tx`, then **immediately clear it on the response** — before any branch, so
   every outcome clears it (FR-022, AC-016).
5. `DecodeTransaction(raw, txKey, now)` → MAC failure, malformed, or expired → `sso_expired`.
6. `r.URL.Query().Get("error") != ""` → `sso_denied` when `access_denied`, else `sso_failed`.
7. `query.state != tx.State` (constant-time compare) → `sso_failed`/`state_mismatch`.
8. `code == ""` → `sso_failed`/`missing_code`.
9. `Service.CompleteLogin`: `discover(ctx)` (cached) → `Exchange` with the verifier and the
   **same** `redirectURI` → extract `id_token` → `provider.Verifier(&oidc.Config{ClientID}).
   Verify(ctx, raw)` (signature, `iss`, `aud`, `exp`) → `idToken.Nonce != tx.Nonce` →
   `nonce_mismatch` → decode `email`/`email_verified` → `ValidateClaims`.
10. `firstSeen, err := h.externalIdentityRepo.RecordLogin(...)` with a 5 s `WithoutCancel`
    context; an error is logged at `slog.Error` and ignored (AD-015).
11. If `firstSeen` → `slog.Warn("new SSO identity granted admin", …)` (AD-021).
12. `h.issueSessionCookie(w, r, time.Now())` — the identical helper `POST /auth/login` uses.
13. `slog.Info("sso login", "outcome", "success", "ip", ip, "issuer", iss, "subject", sub)`.
14. `302` to `tx.ReturnPath`. The browser requests `/`, receives `index.html`, the SPA mounts,
    `useAuth` calls `GET /api/v1/auth/check` — a same-origin request carrying the `Lax`
    `riot_session` — and renders the dashboard authenticated on that first navigation
    (FR-036, AC-009).

### 7.4 `/callback` — failure path

Steps 1–4 as above (the transaction cookie is already cleared). The first failing check
produces a `*LoginError`; the handler emits exactly one structured entry
`slog.Warn("sso login failed", "outcome", "failure", "reason", <reason>, "code", <sso_code>,
"ip", ip)` — with the raw IdP `error`/`error_description` added only for `idp_error`, and
never a token, secret, verifier, or email — and responds `302 /?sso_error=<code>`. The login
screen renders the message and a working password form; a password login submitted
immediately afterwards is unaffected because the SSO throttle bucket is separate (AD-013)
and, after AD-020, cannot be drained by a third party spoofing the operator's IP.

---

## 8. Acceptance Criteria Mapping

`UNIT` = automated Go/vitest test. `LIVE` = manual verification against authentik at
`https://auth.rbretschneider.com`; QA owns these and records them in the QA report.

| AC | Fulfilled By | Test Strategy |
|---|---|---|
| AC-001 | `oidc/config.go` dormancy; `handlers/oidc.go:OIDCAvailability`; `Login.tsx` conditional render | UNIT `oidc/config_test.go` (dormant with no vars); `handlers/oidc_handler_test.go` (`200 {"available":false,"label":""}`); `web/src/pages/Login.test.tsx` (no anchor, password submit still calls `onLogin`) |
| AC-002 | `handlers/oidc.go` 404 gate (AD-011) | UNIT `oidc_handler_test.go`: both routes `404`, `rec.Result().Cookies()` empty |
| AC-003 | `config.go` reads; `oidc.New` all-three rule | UNIT `internal/server/config_test.go` (secret empty → field empty); `oidc/config_test.go` (`Enabled()==false`); `oidc_handler_test.go` (`available:false`, `/start` 404) |
| AC-004 | `oidc/config.go` label default | UNIT `oidc/config_test.go`; `oidc_handler_test.go` (`{"available":true,"label":"Sign in with SSO"}`) |
| AC-005 | `oidc/config.go`; `Login.tsx` | UNIT `oidc_handler_test.go` (custom label echoed); `Login.test.tsx` (button text = label from the mocked query) |
| AC-006 | `Service.BeginLogin`; `handlers.requestOrigin`; `transaction.go` cookie | UNIT `oidc/service_test.go` (parse the returned URL: `response_type`, `code_challenge_method=S256`, non-empty `code_challenge`/`state`/`nonce`, `scope` contains `openid`); `oidc_handler_test.go` with `httptest.NewRequest("GET","https://riot.example:7331/…")` + `req.TLS` set (exact `redirect_uri`, `302`, cookie `HttpOnly`, `Secure`, and `MaxAge<=300`) |
| AC-007 | `Login.tsx` anchor | UNIT `Login.test.tsx`: `element.tagName === 'A'`, `getAttribute('href') === '/api/v1/auth/oidc/start'`, `fetch` spy not called on click |
| AC-008 | `Service.CompleteLogin` + `issueSessionCookie` (AD-008) | UNIT `oidc_handler_test.go` full flow against `testidp_test.go` (`302` to `/`; cookie name/Path/HttpOnly/SameSite/**Secure**/MaxAge equal to `Login`'s for the same request shape; JWT parses with the same secret to `sub:"admin"`); feed the cookie to `h.AuthCheck` and assert `{"authenticated":true,"needs_setup":false}` |
| AC-009 | AD-008 `SameSite=Lax` (+ `Secure`) | UNIT `auth_handler_test.go` + `oidc_handler_test.go` assert `http.SameSiteLaxMode` at both mint sites. LIVE: real browser round trip to authentik lands authenticated with no manual reload |
| AC-010 | `ExternalIdentityRepo.RecordLogin`; AD-016 upsert | UNIT `oidc_handler_test.go` with `MockExternalIdentityRepo`: exactly one record for (I,S), email as asserted, `first_login_at == last_login_at == injected now`, and `firstSeen == true` |
| AC-011 | AD-016 `ON CONFLICT … DO UPDATE` preserving `first_login_at` | UNIT same file, two logins at `T0`/`T1`: one record, `first_login_at == T0`, `last_login_at == T1`, second call reports `firstSeen == false`. Migration-level confirmation is the AC-029 LIVE run |
| AC-012 | No allowlist anywhere in `CompleteLogin` (FR-026) | UNIT `oidc_handler_test.go`: a second, unseen `sub` gets a cookie; that cookie passes `middleware.AdminAuth` guarding a stub handler → `200` |
| AC-013 | Handler step 7.3.7 | UNIT `oidc_handler_test.go`: no `riot_session` cookie, `302` to `/?sso_error=sso_failed`, body empty and `Content-Type` not JSON |
| AC-014 | `CompleteLogin` nonce comparison | UNIT `oidc/service_test.go` (stub IdP mints an ID token with a different `nonce`) + `oidc_handler_test.go` (`sso_failed`, no cookie) |
| AC-015 | `DecodeTransaction` absence/MAC/expiry (AD-005) | UNIT `oidc/transaction_test.go` (absent; tampered MAC → `ErrTransactionMAC`; `IssuedAt` 301 s old → `ErrTransactionExpired`; boundary at exactly 300 s accepted); `oidc_handler_test.go` (`302 sso_expired`, `riot_oidc_tx` cleared with `MaxAge==-1`) |
| AC-016 | Handler step 7.3.4 (clear before branching) | UNIT `oidc_handler_test.go`: success response clears `riot_oidc_tx`; replaying the same URL without the cookie yields `sso_expired` and no session. See also SEC-006 LIVE in §8.1 |
| AC-017 | AD-004 failure-not-cached; AD-010 `sso_unavailable` | UNIT `oidc/service_test.go` (stub issuer closed → error; a second call re-attempts, proving the failure was not cached); `oidc_handler_test.go` (`302 /?sso_error=sso_unavailable`, not 5xx, body not JSON, within the 10 s budget); `Login.test.tsx` (message + working password form). LIVE: stop authentik, click the button, then log in with the password |
| AC-018 | AD-010 `access_denied` mapping | UNIT `oidc_handler_test.go` (`?error=access_denied` → `302 /?sso_error=sso_denied`, no cookie); `Login.test.tsx` (message rendered, password submit works) |
| AC-019 | AD-004 (no IdP call at boot or on any other route) | UNIT: full `go test ./...` green with an unreachable issuer configured in the handler test fixture; `oidc_handler_test.go` asserts `h.Login` still succeeds with SSO configured and the issuer down. LIVE: `/health`, register, heartbeat, telemetry with authentik stopped |
| AC-020 | AD-019 guard entries + `isSetupComplete()` gate | UNIT `middleware/setup_test.go` (the three exact OIDC paths pass the guard while incomplete; `/api/v1/auth/login` and other `/api/` paths still `503`); `oidc_handler_test.go` with `SetupComplete=false` (`available:false`, both routes `404`, no cookie obtainable) |
| AC-021 | The callback never touches `adminRepo` writes | UNIT `oidc_handler_test.go`: after a successful SSO login, `MockAdminRepo.PasswordHash` is byte-identical and `Config["setup_complete"]` is untouched; `h.Login` with the original password still returns `200` |
| AC-022 | `clearSessionCookie` used by `Logout` (AD-008) | UNIT `auth_handler_test.go`: SSO-minted cookie → `Logout` → `MaxAge==-1` with matching `SameSite`/`Secure`; `AuthCheck` with the cleared cookie → `authenticated:false`; the stub IdP records zero requests on the logout path |
| AC-023 | AD-017 (`CompleteLogin` returns `Claims`, which has no token field) | UNIT `oidc_handler_test.go`: no response cookie other than `riot_session` + the `riot_oidc_tx` clear; the recorded `models.ExternalIdentity` has no token-bearing field (compile-time) and no captured value contains the stub's token strings |
| AC-024 | FR-007 discipline: the secret is only read inside `oidc.Service` | UNIT `oidc_handler_test.go`: run all three endpoints with a sentinel secret and assert it appears in no response body or header; capture `slog` into a buffer via a test handler across a success and each failure branch and assert absence. Frontend/bundle grep is a QA step. (`/debug/pprof` was confirmed by the security review not to leak it — SEC-009) |
| AC-025 | `Login.tsx` `retry:false` + `data?.available === true` | UNIT `Login.test.tsx`: mocked `getSSOAvailability` rejects → no anchor, password submit succeeds |
| AC-026 | `oidc.SafeReturnPath` (AD-007) | UNIT `oidc/identity_test.go` table test (`https://evil.example`, `//evil.example`, `/\evil.example`, `evil`, `""`, `/ok?a=b` → only the last is preserved); `oidc_handler_test.go` end-to-end: hostile `returnUrl` → successful callback redirects to `/` |
| AC-027 | AD-013 `oidcLimiter` + `MiddlewareRedirect`, keyed per AD-020 | UNIT `middleware/ratelimit_test.go`: 6 requests from one peer → the 6th is `302` to the configured location; a different peer is unaffected; **a spoofed `X-Forwarded-For` from an untrusted peer does not create a fresh bucket**; the key never comes from a request body or identity claim. Wiring is confirmed by inspection of `router.go` in the QA review |
| AC-028 | NFR-009 structured logging (§7.3/7.4), IP per AD-020 | UNIT `oidc_handler_test.go` with a capturing `slog` handler: exactly one attempt entry per attempt; success carries `outcome=success`, `ip`, `issuer`, `subject`; state mismatch carries `reason=state_mismatch`; IdP-down carries `reason=discovery_failed`; no entry contains the token, secret, verifier, or email; the logged `ip` is the peer address even when `X-Forwarded-For` is present and the peer is untrusted |
| AC-029 | Migration `000022` (§5) | LIVE/QA: `make migrate-up` then `make migrate-down` against a database restored from the previous release; `\d` diff shows only `external_identities` added and then removed, row counts on every pre-existing table unchanged. Not unit-testable — there is no DB test harness in this repo today, and this story does not introduce one |
| AC-030 | AD-003 (no new required var, including `RIOT_TRUSTED_PROXIES`) | UNIT `internal/server/config_test.go` (no `RIOT_OIDC_*` and no `RIOT_TRUSTED_PROXIES` → all fields empty, no error) plus the whole suite staying green. LIVE/QA: start the built image with the previous `.env`, confirm migrations apply and no SSO button appears |

### 8.1 Security-review conditions (from `docs/security/OIDC-001-security-review.md`)

QA must verify these in addition to the ACs. They are listed separately because they are
review conditions rather than FRD acceptance criteria; none of them weakens an AC.

| Finding | Property to verify | Where |
|---|---|---|
| SEC-001 | `riot_session` carries `Secure` over `https` and omits it over plain `http`, identically at both mint sites | UNIT `auth_handler_test.go`, `oidc_handler_test.go` (two request fixtures: one with `req.TLS` set, one without) |
| SEC-002 / C5 | The throttle key and the logged client IP cannot be influenced by any request header when the peer is untrusted; forwarded headers are honoured only from a configured trusted peer; **an all-trusted `X-Forwarded-For` chain falls back to the immediate peer** (AD-020) | UNIT `middleware/clientip_test.go` (all four headers, trusted and untrusted peers, rightmost-untrusted walk, all-trusted chain → peer, absent/unparseable header → peer, malformed CIDR handling) + `middleware/ratelimit_test.go` (spoofed `X-Forwarded-For` does not open a fresh bucket) |
| SEC-003 | A first-ever `(issuer, subject)` login produces a `WARN`-or-above structured entry naming issuer and subject; a repeat login does not | UNIT `oidc_handler_test.go` with a capturing `slog` handler, both first and repeat login. QA additionally confirms the entry appears in Settings → Logs on the LIVE run |
| SEC-004 | A `riot_session` JWT presented as the `riot_oidc_tx` cookie is rejected **at the MAC check** (`errors.Is(err, oidc.ErrTransactionMAC)`), not at a later decode step; `DeriveTransactionKey` output ≠ the raw JWT secret | UNIT `oidc/transaction_test.go` |
| SEC-005 | ADD text states the correct residual-risk argument (single fixed admin principal), not the incorrect "attacker cannot obtain a signed transaction" claim | Document review — AD-005 and §10 |
| SEC-006 | Replaying a consumed authorization `code` against `/callback` yields `sso_failed`/`sso_expired` and no session | **LIVE** against authentik (capture a callback URL, complete it, then replay it) |
| SEC-007 | `go.mod` resolves `golang.org/x/oauth2` ≥ v0.27.0 and `github.com/go-jose/go-jose/v4` ≥ v4.0.5; `govulncheck ./...` clean | QA runs `go list -m all` + `govulncheck ./...` |
| SEC-008 | Operator documentation states: `matching_mode: strict`, one redirect entry per scheme/host/port, regex and wildcard matching prohibited, the `--redirect-uri` flag used on the first run, and that a re-run appends rather than replaces | Document review against §4.5 items 4–5 |
| SEC-003 (doc) | Operator documentation states the authentik group binding is REQUIRED, gives the `--group` registration one-liner, and requires verification with a non-member test account before SSO is enabled | Document review against §4.5 items 1–3 |
| C4 | Operator documentation and `.env.example` state that, behind a TLS-terminating proxy with `RIOT_TRUSTED_PROXIES` unset, `riot_session` is issued **without** `Secure`, and that setting the variable is what turns it on — stated ahead of the rate-limit and audit-IP consequences | Document review against §4.5 item 6 and the `.env.example` row in §4.4 |
| SEC-011 | `SetupGuard` matches the three OIDC paths exactly; `/api/v1/auth/oidcanything` does not pass the guard | UNIT `middleware/setup_test.go` |
| SEC-012 | The availability `label` is never sourced from a request parameter | Code review of `handlers/oidc.go` |

---

## 9. Error Handling

- **Browser-facing:** exactly the AD-010 table. Four codes, four messages, one generic
  fallback for an unrecognised code (FR-037). No 5xx is reachable from `/start` or
  `/callback` — every internal error is mapped before the response is written.
- **HTTP status discipline:** `302` for every outcome except dormancy/setup-incomplete
  (`404 {"error":"not found"}`) and throttling (`302`, per AD-013). `GET /api/v1/auth/oidc`
  returns `200` unconditionally; it has no error path because it reads only process-local
  state.
- **Error shape (server-internal):** `*oidc.LoginError{Code, Reason, Err}`. `Code` is
  browser-visible; `Reason` is log-only; `Err` is the wrapped cause and is never rendered.
  `errors.As`/`errors.Is` are the only ways handlers and tests inspect it; a non-`LoginError`
  reaching the handler is logged at `slog.Error` with the full value (it is a bug) and mapped
  to `sso_failed`.
- **Logging levels:**
  - `slog.Info` — successful SSO login (one entry per attempt, NFR-009).
  - `slog.Warn` — every SSO failure; **and the AD-021 first-admission entry**; and dropped
    `RIOT_TRUSTED_PROXIES` entries at boot; and the AD-003 config warnings.
  - `slog.Error` — audit-write failure, and any unexpected non-`LoginError`.
- `logstore.DBHandler` persists `WARN`+ to `server_logs`, so SSO failures and first
  admissions become visible in Settings → Logs. This is deliberate for AD-021 and is why the
  `reason` vocabulary is fixed and why no token, secret, verifier, or email may ever enter a
  log field.
- **Frontend:** the availability query failing is not an error state — it is "no button"
  (FR-035). No toast, no console noise, no retry storm (`retry: false`).
- **Audit-write failure:** logged with `first_seen_unknown: true`, ignored, login proceeds
  (FR-028, AD-015).
- **Malformed trusted-proxy config:** the bad entry is dropped with a `WARN`; boot continues;
  the resulting set is smaller, i.e. fails closed (AD-020).

---

## 10. Security Considerations

| Vector | Mitigation |
|---|---|
| **Session cookie exfiltration over cleartext** | `riot_session` carries `Secure` whenever the request scheme resolves to `https` (AD-008), so an attacker-initiated top-level navigation to `http://<rIOt-host>/…` cannot carry the admin JWT. Omitted over plain `http` so NFR-010's LAN mode still works. rIOt emits no HSTS (SEC-010, pre-existing, own story) — `Secure` is therefore the only control on this path, which is why it is mandatory here. Behind a TLS-terminating proxy the protection engages only once `RIOT_TRUSTED_PROXIES` names the proxy (AD-020, §4.5 item 6) |
| **CSRF against rIOt's own API after `Strict → Lax`** | `Lax` still blocks cross-site `POST`/`PUT`/`DELETE`, which is every mutating endpoint in `router.go`. Cross-site top-level `GET` navigations reach read-only handlers whose responses the attacker cannot read. Cross-site WebSocket handshakes are not top-level navigations, so `Lax` withholds the cookie exactly as `Strict` did; `middleware.CheckWSOrigin` is defence in depth (SEC-014). Full analysis in AD-008 |
| **Authorization-response injection (state/nonce/PKCE)** | `state` (256 bits, `crypto/rand`) compared constant-time; `nonce` bound into the ID token and compared; PKCE S256 binds the code to this browser |
| **Login CSRF** *(rationale corrected, SEC-005)* | **Not** closed by the transaction MAC: any client can call `/start` and obtain a genuinely server-signed transaction, then plant it on a victim from a same-site foothold and navigate them to a captured callback. The property that makes this low-impact is that **every rIOt session is the identical `sub:"admin"` principal (BR-006)** — there is no per-user data to poison and no account to attribute actions to, and the attacker cannot read the resulting session (same-origin policy). `Secure` (AD-008) and the derived MAC key (AD-005) both shrink the surface. **This analysis must be redone if rIOt ever gains per-user sessions** |
| **Cross-protocol MAC confusion** | The transaction MAC key is `HMAC(jwtSecret, "riot-oidc-tx-v1")`, cryptographically separated from the session signing key, and the MAC is verified *before* any decoding (AD-005). A `riot_session` JWT submitted as `riot_oidc_tx` fails on cryptographic grounds, not on a base64 accident (SEC-004) |
| **Open redirect via `returnUrl`** | `oidc.SafeReturnPath` — single leading `/`, no `//`, no `\`, no control characters; anything else becomes `/` (AC-026). The only other `Location` values the handlers can emit are the discovered authorization endpoint and the four fixed `/?sso_error=` URLs (SEC-015 reviewed, no findings) |
| **`redirect_uri` poisoning via `Host`** | Contained by authentik's **strict** redirect-URI matching: an unregistered value is rejected before a code exists, and the code is bound to `redirect_uri` at exchange. A disclosed code is unusable without the PKCE verifier and the client secret. This containment is an *operator configuration*, so §4.5 items 4–5 make `matching_mode: strict`, the `--redirect-uri` first-run flag, and the prohibition on regex/wildcard matching hard documentation requirements (SEC-008). `X-Forwarded-Host` is never consulted; `X-Forwarded-Proto` is consulted only from a trusted peer (AD-020) |
| **Token theft / replay** | No IdP token is persisted, returned, or logged (AD-017). The authorization code is single-use at the IdP; the transaction cookie is cleared on every outcome, so a replayed callback URL finds no transaction (AC-016). Server-authoritative single use is *not* implemented — the property is delegated, recorded in AD-005, and verified LIVE (SEC-006) |
| **Session fixation / privilege confusion** | The callback mints a brand-new session through the same helper as password login; there is no session to fix and no role to escalate to (BR-006) |
| **Client-secret disclosure** | The secret is read once in `LoadConfig`, held in `oidc.Service`, and used only as the `oauth2.Config.ClientSecret` on the back-channel token request. It is never in a response, a log field, an error message, or the frontend bundle (FR-007, AC-024). The security review confirmed `/debug/pprof` does not leak it (SEC-009) |
| **TLS downgrade to the IdP** | The service uses a plain `&http.Client{Timeout: 10s}` with the default transport and the system trust store. `InsecureSkipVerify` must not appear anywhere in this story, and no env var may make it configurable (NFR-007) |
| **Brute force / attempt flooding** | 5/min, burst 5, per IP on `/start` and `/callback`, in a bucket separate from password login so an attacker cannot exhaust the operator's password allowance (AD-013, AC-027) |
| **Rate-limit key and audit-IP forgery** *(closed, SEC-002)* | `chimw.RealIP` is removed. `middleware.RealIP(tp)` honours `X-Forwarded-For` / `X-Real-IP` / `True-Client-IP` / `X-Forwarded-Proto` **only** when the TCP peer is inside a `RIOT_TRUSTED_PROXIES` prefix; the default is trust-nobody, and an all-trusted chain falls back to the peer (AD-020, C5). This also closes the pre-existing bypass of `loginLimiter` (unbounded bcrypt brute force of the shared admin password) and `registerLimiter`, and makes NFR-009's logged IP trustworthy. Known residual: `handlers/setup.go` still reads `X-Real-Ip` for a certificate SAN — low impact, out of scope, carried to the follow-up story (AD-020 scope note, §14) |
| **Fail-open delegated authorization** *(surfaced, SEC-003)* | BR-002/BR-003: any identity authentik authorizes becomes rIOt admin, and rIOt performs no local check (FR-026, by design). An authentik application with no policy binding admits every authenticated user. rIOt cannot verify the binding (§9 item 13), so the residual risk is managed two ways: the first admission of any new `(issuer, subject)` emits a persisted `WARN` visible in Settings → Logs (AD-021), and the binding — created by `register_oidc_app.py --group` — plus a non-member verification are REQUIRED runbook steps (§4.5 items 1–3) |
| **Denial of service via the availability endpoint** | Answers from process-local state only — no DB, no network, no allocation beyond the response (NFR-001). Deliberately unthrottled (AD-013); disclosing "SSO is configured" and the button label is inherent to rendering the button pre-authentication (SEC-012, no action) |
| **Log injection through claims** | `issuer` and `subject` are written as structured `slog` attributes (JSON-encoded), not interpolated into a message string; `email` is **not** logged |
| **Supply chain** | Version floors with CVE justification in AD-002; `govulncheck ./...` clean is a Definition-of-Done item (SEC-007) |
| **Secrets in container env** | `RIOT_OIDC_CLIENT_SECRET` is delivered as an env var, readable via `docker inspect` and `/proc/<pid>/environ`. This matches rIOt's existing handling of `RIOT_JWT_SECRET` and `RIOT_ADMIN_PASSWORD`, so no new class of exposure is introduced (SEC-013, informational). `.env.example` carries placeholders only |

**Out of scope, recorded, must not be fixed inline:** `/debug/pprof` mounted unauthenticated
on the application port (SEC-009), the absence of `Strict-Transport-Security` /
`X-Frame-Options` / `X-Content-Type-Options` / CSP (SEC-010), and `handlers/setup.go`'s direct
`X-Real-Ip` reads (AD-020 scope note, C6). All three are pre-existing, all three warrant the
same follow-up story (§14), and §4.2 touches `router.go` and `middleware/setup.go` — an
engineer will be tempted, and must not (§12 note 18).

---

## 11. Performance Considerations

- **`GET /api/v1/auth/oidc` (NFR-001, ≤100 ms p95):** two atomic/boolean reads and a
  ~40-byte JSON body. No DB query (AD-012), no network (FR-011), no lock beyond the
  `atomic.Bool`. Not rate-limited, so a mounting login screen is never delayed.
- **`/start` (NFR-002, ≤2 s warm):** with a cached provider the path is RNG + string
  building + two HMACs (transaction MAC; the key derivation itself is done once at startup) —
  sub-millisecond. Cold, it costs one discovery fetch bounded by the 10 s client timeout. The
  discovery mutex is held only around the cache read/populate, so concurrent first logins
  serialise once and then run free (AD-004).
- **`/callback`:** one token POST plus, at most, one JWKS fetch (thereafter served from
  `go-oidc`'s in-memory `RemoteKeySet`), then one indexed upsert. All outbound calls share
  the 10 s-timeout client (NFR-003).
- **`middleware.RealIP` (AD-020):** on the untrusted path it is one `SplitHostPort` plus at
  most a handful of `netip.Prefix.Contains` calls against a slice that is empty by default —
  strictly cheaper than `chimw.RealIP`'s three header lookups. The trusted path adds one
  `X-Forwarded-For` split and one `Contains` per chain entry. Runs once per request,
  allocation-free in the default case.
- **Timeouts:** `&http.Client{Timeout: 10 * time.Second}` on every IdP call. Note the
  server's `ReadTimeout: 15s` and absent `WriteTimeout` (server.go:198) leave ample headroom.
- **Indexing:** `UNIQUE (issuer, subject)` is the sole index and the sole access path; the
  upsert is a single index probe and `RETURNING (xmax = 0)` adds no extra round trip
  (AD-016). No additional index is authorised (this is the "no indexes not called out in
  Section 11" gate for the database engineer).
- **Table growth:** one row per distinct IdP subject, forever — a homelab's worth is single
  digits. It is deliberately excluded from `runRetention` (A-8: an audit trail that
  self-deletes is not an audit trail).
- **Rate-limiter memory:** the existing sweeper drops visitors idle for 10 minutes; a second
  `RateLimiter` instance adds one map and one goroutine — negligible.
- **Log volume:** AD-021's `WARN` fires once per distinct identity, not per login, so
  `server_logs` growth is bounded by the number of people, not the number of sign-ins.
- **Frontend:** `staleTime: Infinity` + `retry: false` on the availability query means at
  most one request per page load, and none in demo builds (`enabled: !isDemo`).

---

## 12. Implementation Notes for Engineers

1. **Copy the issuer URL verbatim, trailing slash included.** authentik's per-application
   issuer is `https://auth.rbretschneider.com/application/o/<slug>/`. `go-oidc` compares the
   discovery document's `issuer` against the configured string exactly; a dropped trailing
   slash fails at `/start` with `sso_unavailable`. Do **not** reach for
   `oidc.InsecureIssuerURLContext` to paper over a mismatch — log both values in the
   `discovery_failed` warning so the operator can see what to fix.
2. **`x/oauth2` PKCE helpers only.** `oauth2.GenerateVerifier()`,
   `oauth2.S256ChallengeOption(v)` on `AuthCodeURL`, `oauth2.VerifierOption(v)` on
   `Exchange`. Do not hand-build `code_challenge`.
3. **`redirect_uri` must come from one function.** Call `requestOrigin(r)` in both handlers.
   A copy-pasted second implementation is how `invalid_grant` bugs are born (FR-015).
4. **Clear `riot_oidc_tx` before you branch** in the callback (7.3 step 4) — not in each
   error path. That is what makes FR-022/AC-016 unconditional.
5. **Verify the transaction MAC before decoding anything** (AD-005 step 2 before step 3).
   QA asserts that a `riot_session` JWT fed to `riot_oidc_tx` fails with
   `ErrTransactionMAC`. Split on the **last** `.`; do not add a dot-count precheck ahead of
   the MAC, and do not use a lenient base64 decoder.
6. **Never pass the raw `jwtSecret` to transaction encode/decode.** Use
   `oidc.DeriveTransactionKey` once at service construction and store the result.
7. **Constant-time comparisons** for `state` and the transaction MAC:
   `subtle.ConstantTimeCompare` / `hmac.Equal`. Not `==`.
8. **`context.WithoutCancel`** on the audit write (Go 1.21+, available on 1.24). Using
   `r.Context()` directly loses rows whenever the browser follows the redirect quickly.
9. **`RETURNING (xmax = 0)`** is how `RecordLogin` reports first-seen. Do not add a second
   query, and do not infer it from a row count.
10. **The AD-021 entry is `slog.Warn`, deliberately.** `INFO` is not persisted by
    `logstore.DBHandler` and would not reach Settings → Logs, which is the entire point of the
    control. Do not "tidy" it down to `Info`.
11. **Never log** `tokens`, `id_token`, `code`, `code_verifier`, `client_secret`, or `email`.
    Log `issuer`, `subject`, `ip`, `outcome`, `reason`, and — only for `idp_error` — the IdP's
    `error`/`error_description`.
12. **`middleware.RealIP` replaces `chimw.RealIP`; delete the old `r.Use` line.** Leaving both
    in place re-opens SEC-002 in whichever order they run. Keep the `chimw` import — 
    `chimw.Recoverer` still uses it.
13. **Everything that needs a client IP calls `middleware.ClientIP(r)`**, and everything that
    needs a scheme calls `middleware.RequestScheme(r)`. No handler parses `r.RemoteAddr` or
    reads `X-Forwarded-*` directly. (The pre-existing `X-Real-Ip` reads in
    `handlers/setup.go` are the one known exception and are out of scope — note 18.)
14. **In `RealIP`, every branch that cannot identify a client returns the peer.** All-trusted
    chain, absent header, unparseable entry: keep `r.RemoteAddr` as it arrived (AD-020, C5).
    Never promote the leftmost `X-Forwarded-For` entry as a last resort — that hands the key
    back to whoever wrote the header.
15. **`newTestHandlers(t)` builds `*Handlers` as a struct literal.** New fields must be
    settable that way; give `oidc` and `setupComplete` sane nil behaviour (`Enabled()` is
    nil-safe false; `isSetupComplete()` is nil-safe false) so existing tests keep compiling
    untouched.
16. **Demo build will break** unless `getSSOAvailability` is added to *both* `api/client.ts`
    and `api/demo-client.ts` — `vite.config.ts` aliases one to the other. Run
    `npm run build:demo` before you call the frontend done.
17. **Use `useSearchParams`, not `window.history.replaceState`**, to strip `sso_error`
    (AD-014) — the app is inside `BrowserRouter` and a raw `replaceState` desyncs it. The SSO
    anchor goes **outside** the `<form>` element (the existing form has
    `action="/api/v1/auth/login" method="POST"` for password-manager compatibility; a link
    inside it invites accidental submit behaviour) — render a divider then the anchor. Render
    the mapped message, never the raw `sso_error` value (FR-037).
18. **Do not fix SEC-009 (`/debug/pprof`), SEC-010 (security headers), or `handlers/setup.go`'s
    `X-Real-Ip` reads inline.** All three are pre-existing, all three are out of scope, and
    §4.2 has you editing `router.go` and `middleware/setup.go` right next to them. Touching
    them without an ADD entry is a scope violation that will bounce at QA. They are carried
    together in §14.
19. **Do not add a service worker or any `/api/**` navigation bypass** (AD-018).
20. **Do not touch `loginLimiter`'s or `registerLimiter`'s rate/burst values, the
    `riot_session` lifetime, its claims, or its signing algorithm** (§9 item 12). The only
    changes to existing security controls authorised by this story are: `riot_session`
    `SameSite`/`Secure` (AD-008) and client-IP derivation (AD-020).
21. **Stub-issuer tests must not hit the network.** `internal/server/oidc/testidp_test.go`
    is an `httptest.Server` with an in-test RSA key; every OIDC test points the service at
    its URL. `go test ./...` must pass with no outbound connectivity.
22. **Migration number is `000022`.** Verify nothing else has claimed it before committing;
    never edit a migration ≤ `000021`.
23. **Commit style:** `OIDC-001 feat: …`, `OIDC-001 migration: …`, `OIDC-001 test: …`, one
    logical change per commit.

---

## 13. Definition of Done

- [ ] Every file in §4 created or modified exactly as specified; nothing outside §4 touched
      (in particular: `/debug/pprof`, security headers, and `handlers/setup.go` untouched —
      §12 note 18).
- [ ] `github.com/coreos/go-oidc/v3` added; `golang.org/x/oauth2` ≥ **v0.27.0**;
      `github.com/go-jose/go-jose/v4` ≥ **v4.0.5** pinned explicitly; `go.mod`/`go.sum`
      committed; no other new direct dependency; no unrelated upgrades.
- [ ] `govulncheck ./...` clean.
- [ ] Every AC in §8 has a test named with its AC reference
      (`TestAC013_StateMismatchIsRejected`, `describe('[AC-007] …')`), except AC-029 and the
      LIVE rows, which are handed to QA as named manual steps.
- [ ] Every row in §8.1 has either a named test or a named document-review target.
- [ ] `go test ./...` green (and `make test-go` with `-race` on Linux/CI).
- [ ] `cd web && npm run test:run` green; `npm run build` and `npm run build:demo` both
      succeed.
- [ ] `go vet ./...` clean; no new lint findings; no `fmt.Println`, no leftover TODOs without
      a story ID.
- [ ] `make migrate-up` then `make migrate-down` run cleanly against a database populated by
      the previous release; `external_identities` appears and disappears; every pre-existing
      table, column, and row is unchanged (AC-029).
- [ ] Server boots with **no** `RIOT_OIDC_*` and **no** `RIOT_TRUSTED_PROXIES` set: no
      warning, no SSO button, every existing behaviour identical (AC-030).
- [ ] Server boots with a **malformed** `RIOT_OIDC_ISSUER_URL`: boot succeeds, one `WARN`,
      SSO dormant (V-005). Server boots with a **malformed** `RIOT_TRUSTED_PROXIES` entry:
      boot succeeds, one `WARN`, the bad entry dropped (AD-020).
- [ ] Server boots with all three OIDC variables set and the issuer host unreachable: boot
      succeeds, no outbound call is made during startup, `/health` and password login work
      (FR-006, AC-019).
- [ ] Sentinel-secret sweep: the configured `RIOT_OIDC_CLIENT_SECRET` value appears in no log
      line at any level, no `/api/v1/auth/oidc*` response, and no file under `web/dist`
      (AC-024).
- [ ] `riot_session` is `SameSite=Lax` at both mint sites, carries `Secure` over `https`,
      omits it over plain `http`, and is otherwise identical between password login and SSO
      callback in name, `Path`, `HttpOnly`, `MaxAge`, and claims (AD-008, AC-008, SEC-001).
- [ ] With no `RIOT_TRUSTED_PROXIES` configured, a request carrying
      `X-Forwarded-For: <arbitrary>` is throttled and logged against the TCP peer address,
      not the header (AD-020, SEC-002).
- [ ] With `RIOT_TRUSTED_PROXIES` set and **every** `X-Forwarded-For` entry inside a trusted
      prefix, the resolved client is the immediate peer (AD-020, C5).
- [ ] A first-ever `(issuer, subject)` SSO login emits a `WARN` entry; a repeat login does
      not (AD-021, SEC-003).
- [ ] A `riot_session` JWT submitted as `riot_oidc_tx` is rejected with
      `ErrTransactionMAC` (AD-005, SEC-004).
- [ ] `.env.example`, `docker-compose.yml`, and `docker-compose.prod.yml` document the five
      variables as commented examples with no real secret, and the `RIOT_TRUSTED_PROXIES`
      guidance leads with the `Secure`-attribute consequence (C4).
- [ ] The §4.5 operator-documentation requirements are handed to the technical writer as
      named deliverables (QA verifies the text exists before sign-off).
- [ ] Implementation report written at `docs/implementation/OIDC-001-impl-report.md`; no
      uncommitted changes.

---

## 14. Deferred to a follow-up story

Recorded here so they are not rediscovered, and so no engineer fixes them inside OIDC-001.
All three are pre-existing and none is introduced by this story.

| Item | Source | Note |
|---|---|---|
| `/debug/pprof` is mounted unauthenticated on the application port and is reachable before setup completes | SEC-009 | Should sit behind `AdminAuth`, bind to a loopback-only listener, or be gated behind an off-by-default env flag. Confirmed not to leak the OIDC client secret |
| No `Strict-Transport-Security`, `X-Frame-Options`, `X-Content-Type-Options`, or CSP on rIOt's own responses | SEC-010 | HSTS would independently have blocked the SEC-001 exfiltration path; its absence is what made SEC-001 a HIGH. `app_standards.md` §4 (Hardening, rule 13) |
| `handlers/setup.go` reads `X-Real-Ip` directly (lines 123, 223) for a certificate IP SAN | SEC-002 / re-review C6 | Low impact — `net.ParseIP`-validated, and an attacker-chosen SAN is worthless without the private key. Should route through `middleware.ClientIP` for consistency once AD-020 lands |
| Per-user sessions, if ever introduced | AD-005 / §10, security review closing note | Three things must be revisited together: AD-005's login-CSRF analysis, BR-002's single-role grant, and AD-021's one-WARN-per-person signal-to-noise assumption |
