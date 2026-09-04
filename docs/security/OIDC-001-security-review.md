# Security Review Report

**Story ID:** OIDC-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-09-04
**Verdict:** REVISE (ARCHITECT)

Blocking findings: **SEC-001, SEC-002, SEC-003** (all HIGH). SEC-004 also requires an ADD
text change but is cheap. No CRITICAL findings. The OIDC protocol design itself is sound —
the blockers are (1) a session-cookie regression this story introduces, (2) a named NFR that
is unenforceable on the existing middleware stack, and (3) a fail-open authorization default
that rIOt currently has no way to surface.

Documents and code read in full: `docs/requirements/OIDC-001-frd.md`,
`docs/architecture/OIDC-001-add.md`, `internal/server/handlers/auth.go`,
`internal/server/handlers/setup.go`, `internal/server/router.go`,
`internal/server/config.go`, `internal/server/server.go` (setup-state path),
`internal/server/middleware/{admin_auth,ratelimit,setup,wsorigin,cors,logger}.go`,
`go.mod`, `web/src/pages/Login.tsx` (form shape), `D:\Repos\Hawaii\docs\app_standards.md` §4,
`D:\Repos\Hawaii\scripts\register_oidc_app.py`, `D:\Repos\Hawaii\docs\authentik.md`.

---

## Threat Model Summary

### Assets

| Asset | Where it lives after this story | Why it matters |
|---|---|---|
| `riot_session` JWT (`sub:"admin"`, 24 h, HS256) | Browser cookie, `Path=/`, `HttpOnly`, **`SameSite=Lax` after AD-008**, **no `Secure`** | Single credential for total fleet admin |
| `RIOT_JWT_SECRET` | Process memory + `admin_config.jwt_secret` | Forges sessions **and** (new, AD-005) transaction MACs |
| `RIOT_OIDC_CLIENT_SECRET` | Process memory, container env | Back-channel token exchange as the rIOt RP |
| PKCE verifier / `state` / `nonce` | `riot_oidc_tx` cookie, ≤300 s | Binds the authorization response to this browser |
| IdP ID/access token | Callback stack frame only (AD-017) | Not persisted — correct |
| `external_identities` rows | New table `000022` | Audit only; never read for an access decision |
| Fleet control surface reached by any admin session | Existing routes | `GET /ws/terminal/{deviceId}/{containerId}` (interactive shell into containers on every enrolled device), `POST /devices/{id}/rotate-key`, `GET/PUT /settings/registration`, `GET/POST/DELETE /settings/bootstrap-keys`, `POST /fleet/bulk-update`, `POST /fleet/bulk-patch`, `POST /devices/{id}/commands` |

The last row is the reason this review is strict. rIOt's admin session is not "read some
dashboards" — it is remote code execution on every device in the fleet.

### Threat Actors

1. **Unauthenticated network attacker on the same LAN.** rIOt's documented shapes (D-8,
   NFR-010) are direct exposure on `:7331` with a self-signed cert, or plain HTTP on a LAN.
   This actor can ARP/mDNS-spoof, bind ports on neighbouring hosts, and read cleartext.
2. **Unauthenticated remote attacker who can get an admin to load a web page.** Drives
   cross-site navigations; cannot read cross-origin responses.
3. **Authenticated authentik user who is *not* meant to administer rIOt.** The central actor
   for BR-002/BR-003. Under authentik's defaults this actor is authorized by omission.
4. **Automated attacker.** Credential stuffing / brute force against `POST /auth/login` and
   flooding of the new OIDC endpoints.
5. **A compromised device already in the fleet.** rIOt monitors devices that frequently have
   their own web UIs; a compromised one is a same-LAN attacker with a browser-reachable origin.
6. **Supply chain.** Two new direct Go modules and their transitive JOSE stack.

### Attack Surface Introduced

- Three new unauthenticated, browser-facing endpoints: `GET /api/v1/auth/oidc`,
  `/oidc/start`, `/oidc/callback`.
- A new attacker-reachable path through `SetupGuard` (AD-019) while setup is incomplete.
- A new authenticated cookie, `riot_oidc_tx`, carrying a PKCE verifier, MAC'd with the
  session signing key (AD-005) — a second cryptographic use of `RIOT_JWT_SECRET`.
- New server-initiated outbound HTTP to an operator-supplied issuer URL (discovery, JWKS,
  token exchange), triggerable by any unauthenticated client that hits `/start`.
- A new authentication path that produces the *same* full-admin session as the password,
  whose authorization decision is made entirely outside rIOt.
- A change to an existing security control on every session: `SameSite` `Strict` → `Lax`
  (AD-008), which affects every endpoint in the application, not just the new ones.
- A new database table and migration.
- Two new direct dependencies plus `go-jose/v4`.

---

## Findings

### CRITICAL

None.

---

### HIGH

#### SEC-001: `riot_session` is relaxed to `SameSite=Lax` without the compensating `Secure` attribute

**Severity:** HIGH
**Domain:** Session management / transport security
**Location:** ADD AD-008; ADD §6.3 footnote (`* Secure on riot_session follows whatever the
existing Login handler does today — this story does not add a Secure flag`);
`internal/server/handlers/auth.go:46-53, 59-66`

**Description.**
AD-008 changes `riot_session` from `SameSite=Strict` to `SameSite=Lax` and explicitly
declines to add `Secure`. The security analysis in AD-008 evaluates one axis only — CSRF
against rIOt's own mutating endpoints — and correctly concludes that axis is unchanged. It
does not evaluate the other thing `Strict` was doing: suppressing the cookie on
attacker-initiated **top-level navigations**, including navigations that downgrade the scheme.

Cookies are not port-scoped and `riot_session` carries no `Secure` flag, so the browser will
attach it to *any* `http://<same-host>/…` request. Under `SameSite=Strict` a cross-site page
could not trigger such a request. Under `Lax` it can, because a top-level `GET` navigation is
exactly the case `Lax` permits, and `http` vs `https` are different sites under schemeful
same-site.

House standard `app_standards.md` §4 (Authentication, rule 5) pairs these two attributes
deliberately: "`httpOnly`, `SameSite=Lax`, … **`Secure` derived from the actual request
scheme so LAN HTTP still works behind-proxy**." The ADD adopts the first half of the rule and
declines the second. The ADD already implements exactly the required scheme derivation for
`riot_oidc_tx` (AD-005 + AD-009), so the mechanism is present and unused on the cookie that
matters more.

**Attack Scenario.**
1. Operator runs rIOt at `https://192.168.1.50:7331` (self-signed, the documented reference
   shape) and is logged in; their browser holds `riot_session`.
2. The attacker gets any listener onto `192.168.1.50:80` — a sibling container publishing
   `:80` on the same host is the common homelab case — or ARP/mDNS-spoofs the name the
   operator uses.
3. The operator loads any attacker-influenced page: an ad frame, a phishing link, or the web
   UI of a compromised device that rIOt itself monitors.
4. That page executes `location = 'http://192.168.1.50/x'`.
5. The browser performs a top-level `GET`. `SameSite=Lax` permits the cookie. No `Secure`
   flag suppresses it on the `http` scheme. The full 24-hour admin JWT is transmitted in
   cleartext to the attacker's listener.
6. The attacker replays the cookie and now holds `GET /ws/terminal/{deviceId}/{containerId}`
   — an interactive shell into containers on every enrolled device.

Step 4 is the step `SameSite=Strict` used to block. This story removes that block and adds
nothing in its place.

**Required Resolution.**
Amend **AD-008 and the §6.3 footnote**. Property that must hold: *the `riot_session` cookie
must carry `Secure` whenever the request scheme resolves to `https`, and must omit it
otherwise so NFR-010's plain-HTTP LAN mode keeps working.* The scheme resolution must be the
same one AD-009 already specifies (`X-Forwarded-Proto` when exactly `http`/`https`, else
`r.TLS != nil`, else `http`), applied identically at both mint sites so AC-008's
"byte-identical attributes" assertion still holds. §9 item 12 is not violated: it forbids
changing the session's lifetime, claims, signing algorithm and throttling policy — this story
is already changing cookie attributes, and it may not ship a net weakening of them.

If the architect instead wishes to keep `Strict`, AD-008 must be replaced with a design that
solves FR-036/AC-009 some other way; but the alternatives AD-008 already rejected are
reasonable rejections, so amending the attribute set is the expected path.

**Blocks:** Implementation.

---

#### SEC-002: The IP throttle key is fully attacker-controlled, so NFR-008 / AC-027 cannot hold in the target deployment

**Severity:** HIGH
**Domain:** Rate limiting / abuse prevention
**Location:** ADD AD-013 and ADD §10 row "Rate-limit key spoofing";
`internal/server/router.go:30` (`r.Use(chimw.RealIP)`);
`internal/server/middleware/ratelimit.go:75-89`

**Description.**
`chimw.RealIP` runs as global middleware and overwrites `r.RemoteAddr` from `True-Client-IP`,
`X-Real-IP`, or the first entry of `X-Forwarded-For`, with **no trusted-proxy
configuration**. `RateLimiter.Middleware()` then keys its bucket on that value. In rIOt's
documented deployment shape — reached directly on `:7331` with no reverse proxy (D-8) — those
headers are supplied by the attacker, not by a proxy.

The ADD acknowledges this in §10 and declines to act, categorising it as pre-existing and
out of scope. That categorisation is wrong on two counts. First, this story *creates* a new
security requirement (NFR-008) and a new acceptance criterion (AC-027) whose stated property
is "throttled per client IP"; AD-013 satisfies that requirement only against a unit test that
manipulates `RemoteAddr` directly. Shipping a control that passes its test and fails in
production is worse than shipping no control, because QA will sign it off. Second, house
standard `app_standards.md` §4 (Hardening, rule 9) is explicit and pre-dates this story:
"Explicit proxy trust … limited to private ranges with a hop limit — **applied before auth
and rate limiting so IP-keyed throttles see real client IPs**."

**Attack Scenario.**

*Primary (bypass):*
1. Attacker scripts `POST /api/v1/auth/login` with `{"password":"<guess>"}` and header
   `X-Forwarded-For: 10.0.<i>.<j>`, incrementing per request.
2. Every request lands in a fresh bucket. `loginLimiter` (5/min) never engages.
3. bcrypt is the only remaining cost. The single shared admin password is brute-forceable at
   whatever rate the CPU allows, indefinitely. Success is total fleet compromise.
4. The identical technique voids `oidcLimiter` and `registerLimiter`.

*Secondary (targeted denial, newly relevant to this story):*
1. Attacker sends 5 requests to `/api/v1/auth/oidc/start` with
   `X-Forwarded-For: <operator's IP>`, and 5 to `/api/v1/auth/login` with the same header.
2. Both of the operator's buckets are now empty. The operator's SSO attempts redirect to
   `/?sso_error=sso_failed` and their password attempts return `429`.
3. AD-013's separate-bucket reasoning — which exists precisely so a password fallback stays
   available (D-9, AC-017) — is defeated, because the attacker can empty both buckets at will.

*Tertiary (audit integrity):* NFR-009 requires each SSO attempt to log the client IP. The
logged value is `r.RemoteAddr` post-`RealIP`, i.e. the attacker's chosen string. Every SSO
audit entry, including the persisted `WARN` rows visible in Settings → Logs, records a
forgeable IP.

**Required Resolution.**
Amend **AD-013** (and the corresponding ADD §10 row, which currently records this as accepted
risk). Property that must hold: *in the default deployment shape — no reverse proxy — the
value used as the rate-limit key and as the logged client IP must be the actual TCP peer
address and must not be derivable from any request header. Forwarded-header values may be
honoured only when the immediate peer is an explicitly configured trusted source.*

This is achievable without violating §9 item 12: the throttle **policy** (5/min, burst 5,
per IP) is unchanged; only the derivation of "IP" changes. FR-008 is not violated either —
any trusted-proxy setting must be optional with a safe default, not required.

If the architect concludes this genuinely cannot be scoped into OIDC-001, then AD-013 must be
amended to state plainly that NFR-008 and AC-027 are **not met in the default deployment**,
the FRD must be routed back so NFR-008 can be re-scoped, and a follow-up story must be
raised. What is not acceptable is shipping AC-027 as satisfied.

**Blocks:** Implementation.

---

#### SEC-003: Delegated authorization fails open by default at the IdP, and rIOt has no way to surface it

**Severity:** HIGH
**Domain:** Authorization / access control
**Location:** FRD D-4, BR-002, BR-003, FR-026, §9 item 13; ADD §10 row "Authorization
delegated to the IdP"; ADD AD-016 / §5.2 `ExternalIdentityRepository`

**Description.**
BR-002 grants full rIOt admin to any identity authentik authorizes for the application, and
§9 item 13 states rIOt will not verify that the operator restricted it. Delegating
authorization to an IdP is the correct model and I am not asking for it to be replaced. The
finding is about the **direction the default fails in**, and about rIOt being blind to the
consequence.

Two concrete facts make this more than theoretical:

1. **In authentik, an application with zero policy bindings is accessible to every
   authenticated user.** The insecure configuration is the state you reach by doing nothing.
2. **The house registration runbook produces exactly that state.**
   `D:\Repos\Hawaii\scripts\register_oidc_app.py` — the tool `app_standards.md` §4 OIDC rule 10
   mandates for registering apps — calls `/providers/oauth2/` and `/core/applications/`
   (lines 103-138) and creates **no `PolicyBinding` at all**. It also selects
   `default-provider-authorization-implicit-consent` (line 109), so an authorized user is
   admitted with no consent interstitial and no visible signal.

So the documented, sanctioned procedure for turning on rIOt SSO yields: every authentik user
silently obtains full rIOt admin — which is an interactive shell on every device in the fleet
(`GET /ws/terminal/{deviceId}/{containerId}`), plus device key rotation, bootstrap-key
issuance, and fleet-wide bulk update/patch. There is no rIOt-side allowlist (FR-026, by
design), no approval step, no local record consulted (A-8), and, as the design stands, no
alert — the only trace is an `slog.Info` success line (NFR-009) among thousands of request
logs, and the persisted-log store only retains `WARN` and above (ADD §9).

Is the failure mode acceptable for a homelab fleet tool with terminal access to devices?
**As designed, no — not because delegation is wrong, but because the failure is silent.** A
homelab operator will not notice a second admin. The residual risk becomes acceptable when
the wrong outcome is (a) hard to reach by accident and (b) loud when it happens.

**Attack Scenario.**
1. Operator runs `register_oidc_app.py riot`, pastes the three variables into `.env`, and
   restarts. SSO works on their first try, so nothing looks wrong.
2. Any other authentik principal — a family member's account, a service account, an account
   created through a self-service enrollment flow — browses to `https://riot.host:7331`.
3. They click "Sign in with SSO". authentik authenticates them, finds no policy binding,
   authorizes them, and (implicit consent) redirects straight back.
4. rIOt validates the ID token correctly, finds no reason to refuse (FR-026), writes an audit
   row, and mints a `sub:"admin"` session.
5. They open Devices → any device → Containers → terminal, and have a root-capable shell on
   fleet hardware. Total elapsed operator awareness: zero.

Variant: the same drive-by can be triggered *without any click* by an attacker page that
navigates the victim to `/api/v1/auth/oidc/start`, because the implicit-consent flow requires
no interaction when an authentik session already exists (see SEC-005).

**Required Resolution.**
Do **not** add an rIOt-side allowlist — that would contradict FR-026 and §9 item 2 and would
require an FRD revision. Two changes, both inside this story's existing machinery:

1. **Make first admission loud.** Property that must hold: *the first successful login of a
   previously unseen `(issuer, subject)` must emit a distinct, `WARN`-or-above structured log
   entry stating that a new external identity was granted admin, carrying issuer and subject,
   so it is persisted by `logstore.DBHandler` and visible in Settings → Logs.* This requires
   `RecordLogin` to report whether the row was an insert or an update, which is an amendment
   to the `ExternalIdentityRepository` signature in **ADD §5.2 and AD-016** — the
   `INSERT … ON CONFLICT DO UPDATE` can return that fact in the same round trip, so AD-016's
   "no read-modify-write, no race" rationale survives intact. Note this is a *log*, not an
   event, so OQ-3's "no rIOt event for this story" answer is respected.
2. **Make the binding a mandatory, verified step, not a warning in prose.** Property that
   must hold: *the operator documentation (A-9) must present the authentik
   application-to-group binding as a REQUIRED step in the enable-SSO runbook, with an explicit
   verification instruction ("confirm the application denies a test account that is not in the
   group") performed before the three variables are written to `.env`.* Because
   `register_oidc_app.py` is the sanctioned registration path and creates no binding, this
   must be called out against that script by name. Whether the script itself gains a
   `--group` flag is a Hawaii-repo concern outside this story, but the gap must be recorded.

**Blocks:** Implementation (for item 1; item 2 is a technical-writer deliverable that QA must
confirm exists).

---

### MEDIUM

#### SEC-004: The transaction MAC reuses the session signing key with no domain separation

**Severity:** MEDIUM
**Domain:** Cryptography
**Location:** ADD AD-005; ADD §5.3; ADD §6.4 `DecodeTransaction(raw, secret, now)`

**Description.**
AD-005 authenticates `riot_oidc_tx` with `HMAC_SHA256(jwtSecret, base64url(JSON(payload)))` —
the *same* key that signs `riot_session` as an HS256 JWT, which is also
`HMAC_SHA256(jwtSecret, …)`. Two distinct message formats are authenticated with one key and
one algorithm. The only thing preventing a token from one context validating in the other is
an encoding accident.

Concretely: AD-005 says to "split on the last `.`". A `riot_session` JWT is
`b64(header).b64(claims).b64(sig)`. Splitting that on the last `.` yields
payload = `b64(header).b64(claims)` and MAC = `sig`. And `sig` *is* precisely
`HMAC_SHA256(jwtSecret, "b64(header).b64(claims)")`. **`hmac.Equal` returns true.** The
forgery is stopped one step later only because the payload segment contains a `.`, which is
outside the base64url alphabet, so `base64.RawURLEncoding.DecodeString` errors.

That is a knife-edge. It survives today's spec and fails the moment an implementer reaches
for a lenient decoder, changes the split to "first `.`", or reorders the checks. A MAC that
verifies across two protocol contexts is a defect regardless of whether the next line
happens to catch it.

**Attack Scenario.**
An attacker holding any `riot_session` JWT signed with the current secret — their own
legitimately issued session, or an expired one they retained — submits it as the value of
`riot_oidc_tx` on `/callback`. The authenticity check, the one control AD-005 exists to
provide, passes. Under the ADD as written the request then dies in base64 decoding and
reports `sso_expired`; under any implementation drift it does not, and the attacker controls
`State`, `Nonce`, `CodeVerifier` and `ReturnPath` — i.e. full login-CSRF with an
attacker-chosen post-login landing path.

**Required Resolution.**
Amend **AD-005**. Property that must hold: *the transaction MAC key must be
cryptographically separated from the session signing key — derived from `RIOT_JWT_SECRET`
with a fixed, distinct context label — such that no value authenticated in one context can
verify in the other, independent of encoding or parse order.* Standard HKDF-Expand or
`HMAC(jwtSecret, "riot-oidc-tx-v1")` both satisfy it; the choice is the architect's. AD-005's
existing rationale (stateless, no sweeper, authenticity-not-confidentiality) is unaffected.

---

#### SEC-005: ADD §10's login-CSRF rationale is incorrect; the residual risk should be stated accurately

**Severity:** MEDIUM
**Domain:** Business logic / CSRF
**Location:** ADD §10 row "Login CSRF / authorization-response injection"; AD-005 rationale

**Description.**
AD-005 and §10 both claim the HMAC means "an attacker who can plant cookies cannot inject a
transaction of their own choosing". That is not correct. The attacker does not need to forge
a transaction — they can request `/api/v1/auth/oidc/start` themselves and receive a
genuinely server-signed `riot_oidc_tx` whose `State`, `Nonce`, `CodeVerifier` and
`ReturnPath` are all values the server chose *for the attacker's session*. The HMAC raises
the bar from "craft a cookie" to "make one HTTP request first". It does not close the class.

The real reason login CSRF is low-impact here is different, and the ADD does not say it:
every rIOt session is the identical `sub:"admin"` claim, so "the victim is logged into the
attacker's session" and "the victim is logged into their own session" are the same session.
There is no per-user data to poison and no account to attribute actions to. That is a sound
argument — it should be the one on the page.

**Attack Scenario.**
1. Attacker calls `/api/v1/auth/oidc/start`, capturing the `riot_oidc_tx` cookie and the
   authorization URL.
2. Attacker completes the IdP flow themselves and captures `?code=…&state=…` at the callback
   without following it.
3. From a same-site foothold — a sibling host under the same registrable domain, or a
   cleartext-HTTP injection point in the LAN deployment (NFR-010) — the attacker sets
   `riot_oidc_tx` on the victim's browser for `Path=/api/v1/auth/oidc`.
4. Attacker navigates the victim to the captured callback URL. `SameSite=Lax` on
   `riot_oidc_tx` permits the cookie on that top-level `GET`. rIOt validates everything
   correctly and mints a session in the victim's browser.
5. The victim's browser now holds an admin session it did not ask for, landing on an
   attacker-chosen same-origin path (`tx.ReturnPath`).

A cheaper variant needs no cookie planting at all: because the house registration script uses
the implicit-consent flow, an attacker page that simply navigates a victim with a live
authentik session to `/api/v1/auth/oidc/start` will silently walk them through the entire
flow and deposit an admin session — no clicks, no consent screen. This is the same primitive
as SEC-003's variant.

The attacker cannot read the resulting session (same-origin policy), so this is a MEDIUM, not
a HIGH. It becomes serious only in combination with a script foothold on the rIOt origin.

**Required Resolution.**
Correct the rationale text in **AD-005 and ADD §10** so the residual risk is recorded
accurately: state that a server-signed transaction is obtainable by any client, that the
mitigating property is rIOt's single fixed identity (BR-006), and that the exposure would
have to be re-evaluated if rIOt ever gains per-user sessions. No mechanism change is
required. Note that SEC-001's `Secure` flag and SEC-004's key separation both materially
shrink this surface.

---

#### SEC-006: NFR-006's "usable only once" is not implemented server-side

**Severity:** MEDIUM
**Domain:** Business logic / replay
**Location:** FRD NFR-006; ADD AD-005; ADD §7.3 step 4; AC-016

**Description.**
NFR-006 states the transaction cookie "must be usable only once". AD-005's design is
stateless by choice, so nothing on the server records that a transaction has been consumed.
Single use is achieved indirectly by two other things: the cookie is cleared on the response
(§7.3 step 4), and the authorization code is single-use at the IdP. Neither is a property
rIOt enforces. A copy of the cookie taken before the callback remains cryptographically valid
for the remainder of its 300 s window.

This is a low-exploitability gap — an attacker holding a copy of both the transaction cookie
and an unconsumed `code` already has the victim's callback traffic — but it is a stated NFR
that the architecture does not implement, and AC-016's wording ("replaying the same callback
URL a second time does not produce a session") will pass its unit test for the wrong reason.

**Attack Scenario.**
An attacker who observes the callback request (cleartext LAN deployment per NFR-010, or a
malicious browser extension) captures `riot_oidc_tx` and `?code=&state=`. If they reach the
callback before the victim's browser does, they exchange the code and receive the admin
session; the victim then gets `sso_failed` and retries, seeing nothing more than a transient
glitch. rIOt provides no defence of its own here — only the IdP's single-use code semantics
decide who wins.

**Required Resolution.**
Either (a) amend AD-005 with a mechanism that makes consumption server-authoritative, or
(b) amend AD-005 to record explicitly that NFR-006's single-use property is delegated to
cookie clearing plus the IdP's single-use authorization code, and hand QA a named LIVE
verification step against authentik confirming that replaying a consumed `code` is rejected.
Option (b) is defensible for this threat model; what is not defensible is leaving the NFR
looking implemented when it is not.

---

#### SEC-007: AD-002's dependency version floor admits versions with known CVEs

**Severity:** MEDIUM
**Domain:** Dependency / supply chain
**Location:** ADD AD-002 ("v0.10.0+; take the current release"); ADD §4.2 `go.mod` row;
`go.mod`

**Description.**
AD-002 authorises `github.com/coreos/go-oidc/v3` and `golang.org/x/oauth2`, setting the
`x/oauth2` floor at v0.10.0 on the basis of API symbols, not security. It also permits
`go-jose/v4` transitively without a floor. "Take the current release" is a build-time
instruction, not a constraint recorded in `go.mod`, and Go's MVS will happily resolve a lower
version if another module requires one.

Known issues below those floors:

| Module | Advisory | Fixed in |
|---|---|---|
| `golang.org/x/oauth2` | CVE-2025-22868 — unbounded memory consumption parsing malicious JWS in `oauth2/jws` | v0.27.0 |
| `github.com/go-jose/go-jose/v4` | CVE-2024-28180 — JWE decompression bomb | v4.0.1 |
| `github.com/go-jose/go-jose/v4` | CVE-2025-27144 — DoS in parsing | v4.0.5 |

`go.mod` currently has no OIDC/JOSE stack at all, so the resolved versions are entirely
determined by what engineering runs `go get` against, guided only by "v0.10.0+".

**Attack Scenario.**
With a vulnerable `go-jose`, an attacker who can influence what rIOt parses as an ID token —
which in the malicious-IdP or DNS-hijack case they can, since discovery endpoints come from a
URL the attacker may control if they compromise the operator's issuer — sends a compressed
JWE that expands to gigabytes, exhausting server memory. The rIOt server is the monitoring
plane for the entire fleet; taking it down is a useful precursor to attacking the fleet
unobserved.

**Required Resolution.**
Amend **AD-002** to state security floors, not just API floors: `golang.org/x/oauth2` ≥
v0.27.0, and a `go-jose/v4` ≥ v4.0.5 floor pinned in `go.mod` (add it as an explicit
requirement if MVS resolves lower). Add `govulncheck ./...` clean as a Definition-of-Done item
in ADD §13 — the story adds a JOSE parsing stack to a previously JOSE-free binary and that
warrants a standing check, not a one-time glance.

---

#### SEC-008: The `Host`-derived `redirect_uri` mitigation depends on strict IdP matching, which the house runbook will not produce for rIOt

**Severity:** MEDIUM
**Domain:** Redirect handling / configuration
**Location:** ADD AD-009 "Security" paragraph; ADD §10 row "`redirect_uri` poisoning";
FRD D-8, A-3; `D:\Repos\Hawaii\scripts\register_oidc_app.py:119, 152`

**Description.**
AD-009 accepts that `r.Host` is client-controlled and that a poisoned `Host` yields a
poisoned `redirect_uri`. Its sole stated containment is authentik's strict redirect matching.
That containment is real — and the analysis is otherwise correct: with strict matching a
poisoned URI is rejected before a code exists, and even a leaked code is useless without the
PKCE verifier (in the victim's `HttpOnly` cookie) and the client secret. AD-009's refusal to
honour `X-Forwarded-Host` is the right call.

The problem is that the mitigation is an *operator configuration* that the house tooling will
not produce for this app. `register_oidc_app.py` registers exactly one redirect URI:
`https://<slug>.rbretschneider.com/api/v1/auth/oidc/callback` (line 152), with
`matching_mode: strict` (line 119). rIOt, per D-8, is reached at `https://<host>:7331` and
derives its redirect URI from the request. The two will never match. The operator's first
SSO attempt will fail with an IdP redirect-mismatch error, and the path of least resistance
in authentik's UI is to switch `matching_mode` to `regex` and write something permissive.
The moment they do, AD-009's only mitigation is gone, and `app_standards.md` §4 OIDC rule 8
("Fixed, exact-match redirect URI … no wildcard redirects") is violated.

**Attack Scenario.**
1. Operator hits the mismatch and sets the authentik redirect URI to a regex such as
   `https://.*/api/v1/auth/oidc/callback` to cover both the proxied name and `:7331`.
2. Attacker sends `GET /api/v1/auth/oidc/start` with `Host: attacker.example`.
3. rIOt builds `redirect_uri = https://attacker.example/api/v1/auth/oidc/callback` and
   redirects the browser to authentik, which now accepts it.
4. A victim who follows that link authenticates and is redirected to the attacker's server
   with a valid `code` and `state`.
5. The attacker cannot complete the exchange — PKCE verifier and client secret are both out
   of reach — so this stops at code disclosure plus a phishing-grade redirect that *originates
   from the operator's own trusted IdP domain*. That last property is the real value to the
   attacker.

**Required Resolution.**
No code change. Amend the operator documentation requirement in **A-9 / ADD §4.4** so it
mandates: `matching_mode: strict` in authentik, one explicitly enumerated entry per
scheme+host+port that rIOt is reached by, and an explicit prohibition on regex or wildcard
matching. Record in the runbook that `register_oidc_app.py`'s single generated redirect URI
does not fit rIOt's direct-exposure shape and must be supplemented by hand. QA should verify
the documentation states this before sign-off.

---

#### SEC-009: `/debug/pprof` is mounted unauthenticated on the application port

**Severity:** MEDIUM
**Domain:** Information disclosure / DoS (pre-existing)
**Location:** `internal/server/router.go:169`; `internal/server/middleware/setup.go:40`

**Description.**
`r.Mount("/debug/pprof", pprofHandler())` sits outside every auth group, and `SetupGuard`
only blocks paths prefixed `/api/` or `/ws`, so pprof is also reachable before setup
completes. The in-code comment ("no auth so operators can curl from the host") assumes a
loopback binding that does not exist — the mount is on the same listener as the dashboard.

I checked whether this leaks the new `RIOT_OIDC_CLIENT_SECRET` and it does **not**: Go's heap
profile is a sampled allocation profile of stack traces and sizes, not a memory dump, and no
registered profile emits string contents. **AC-024 is unaffected.** The exposure is
unauthenticated CPU/execution profiling and goroutine-stack disclosure.

**Attack Scenario.**
An unauthenticated client requests `GET /debug/pprof/profile?seconds=600` repeatedly. Each
request pins a profiling worker and inflates CPU on the box that is the monitoring plane for
the whole fleet. `GET /debug/pprof/goroutine?debug=2` discloses the full internal goroutine
topology — worker names, connected-agent counts, internal package structure — which is
reconnaissance for the endpoints above.

**Required Resolution.**
Out of scope for OIDC-001 and explicitly *not* required to unblock it. Raise as its own
story. Property that should hold: *pprof must either sit behind `AdminAuth`, or bind to a
separate loopback-only listener, or be gated behind an off-by-default env flag.* Logged here
because the story is what caused the surface to be enumerated, and because §4.2 already
touches `router.go` and `setup.go` — an engineer may be tempted to fix it inline, which they
must not do without an ADD entry.

---

#### SEC-010: No transport-security headers on rIOt's own responses

**Severity:** MEDIUM
**Domain:** Infrastructure / configuration
**Location:** whole-application; no `Strict-Transport-Security`, `X-Frame-Options`,
`X-Content-Type-Options`, or CSP is emitted by any rIOt middleware (verified by grep — the
only matches in the repo are in the *agent's* scanner, which flags other people's servers for
lacking them)

**Description.**
`app_standards.md` §4 (Hardening, rule 13) requires security headers in-app with CSP/HSTS
delegated to nginx. rIOt in its documented shape has no nginx in front of it and emits none
of them itself. This is pre-existing and I am not asking this story to fix it, but it is
directly load-bearing for SEC-001: an `Strict-Transport-Security` header would cause the
victim's browser to upgrade the attacker's `http://<host>/x` navigation to `https` before it
was ever sent, neutralising that exfiltration path even without the `Secure` flag. The
absence of both controls at once is what makes SEC-001 a HIGH rather than a LOW.

Note that rIOt's own `scoring/engine.go:363` docks devices in the fleet for exactly this.

**Attack Scenario.** See SEC-001 step 4; HSTS is the control that would have blocked it
independently.

**Required Resolution.** Own story. Record the interaction in the SEC-001 resolution so the
architect understands that fixing `Secure` is necessary and, absent HSTS, also sufficient.

---

### LOW / INFORMATIONAL

#### SEC-011: `SetupGuard`'s new allowlist entry is a prefix match, not a path match

**Severity:** LOW
**Domain:** Access control
**Location:** ADD AD-019 (`strings.HasPrefix(path, "/api/v1/auth/oidc")`)

AD-019 admits any path beginning `/api/v1/auth/oidc`, which includes
`/api/v1/auth/oidcanything` and `/api/v1/auth/oidc/../login`. I traced both: the first falls
to the frontend catch-all and returns `index.html`, which is already publicly served; the
second does not match chi's literal routing tree and also lands on the catch-all, where
`embed.FS.Open` rejects the `..` and serves `index.html`. **No bypass is reachable** — the
setup wizard cannot be skipped and `POST /api/v1/auth/login` stays blocked during setup.
Recommend exact matching on the three known paths anyway, since a prefix allowlist in an
auth-adjacent guard is a shape that ages badly.

#### SEC-012: Availability endpoint is unauthenticated and unthrottled

**Severity:** LOW
**Domain:** Information disclosure
**Location:** ADD §6.1, AD-013 (throttle exemption)

`GET /api/v1/auth/oidc` discloses, to anyone, whether SSO is configured and the operator's
button label. Both are inherently public (the button is rendered pre-authentication), the
handler touches no DB or network, and AD-013's reasoning for exempting it from throttling is
sound. The `label` is env-sourced and truncated to 64 runes (AD-003) and is rendered as React
text, so it is not an injection vector. Confirm during QA that the label is never sourced
from a request parameter. No action required.

#### SEC-013: The OIDC client secret is delivered as a plain container environment variable

**Severity:** LOW / INFORMATIONAL
**Domain:** Secrets management
**Location:** ADD §4.4 (`docker-compose.prod.yml` `${RIOT_OIDC_*}` passthrough)

An env-var secret is readable via `docker inspect`, `/proc/<pid>/environ`, and any crash
dump that captures the environment. This matches rIOt's existing handling of
`RIOT_JWT_SECRET` and `RIOT_ADMIN_PASSWORD`, so the story introduces no new *class* of
exposure and consistency has real value. Recorded for awareness. `.env.example` must carry a
placeholder only — ADD §4.4 already says so.

#### SEC-014: `CheckWSOrigin` returns true when `Origin` is absent

**Severity:** LOW / INFORMATIONAL
**Domain:** CSRF (pre-existing)
**Location:** `internal/server/middleware/wsorigin.go:12-14`

AD-008 leans on `CheckWSOrigin` as the mitigation that keeps the WebSocket upgrades safe
across the `Strict → Lax` change. That reliance is sound *and* the WS analysis is
independently correct: cross-site WebSocket handshakes are not top-level navigations, so
`SameSite=Lax` withholds `riot_session` from them just as `Strict` did. `CheckWSOrigin` is
therefore defence in depth, not the load-bearing control. Its `Origin == ""` passthrough is
not reachable from a browser (browsers always send `Origin` on WS handshakes) and a
non-browser client still needs a valid session cookie. No action.

#### SEC-015: Return-path and error-parameter handling — reviewed, no findings

**Severity:** INFORMATIONAL

Recorded so the analysis is not repeated. `SafeReturnPath` (AD-007) is stricter than the
reference implementation and correctly covers the bypasses I tested: `//evil.example`,
`/\evil.example`, `https://evil.example`, and control characters. Percent-encoded variants
(`/%2f%2fevil.example`) are not decoded by browsers before origin resolution and remain
same-origin. Header injection via `returnUrl` is doubly covered — by AD-007's control-character
rule and by Go's `Header.Write`, which folds `\r`/`\n` to spaces. Go's HTTP server also
rejects malformed `Host` headers before they reach the handler, so `requestOrigin(r)` cannot
be made to emit a split header. `sso_error` is one of four fixed tokens rendered from a
lookup table (AD-014, implementation note 12), and the dashboard contains no
`dangerouslySetInnerHTML` anywhere.

---

## Positive Observations

These are genuinely well done and should survive the revision unchanged.

1. **AD-017 makes the token-retention rule a compiler-enforced invariant.** `CompleteLogin`
   returns a `Claims` struct with no token field, so BR-005/FR-025/AC-023 cannot regress
   through inattention. This is the right way to encode a security rule.
2. **AD-002 / FR-021 refuse hand-rolled token validation.** Issuer, audience, signature and
   expiry go through `go-oidc`'s verifier, with JWKS fetch and `kid`-triggered rotation
   handled by `RemoteKeySet`. The explicit instruction (§12 note 1) *not* to reach for
   `oidc.InsecureIssuerURLContext` when the trailing-slash issuer mismatch bites is exactly
   the guidance that prevents the classic "fix" that disables issuer validation.
3. **AD-006 gets the randomness right and fails closed.** 256-bit `crypto/rand` `state` and
   `nonce`, `oauth2.GenerateVerifier()` rather than a hand-built S256 challenge, and a
   `rand.Read` error treated as a hard failure rather than proceeding with weak values. The
   last part is the one people get wrong.
4. **AD-010's closed error vocabulary is a deliberate anti-oracle.** Folding state mismatch,
   nonce mismatch and token-validation failure into a single browser-visible `sso_failed`
   while keeping the precise `reason` server-side denies the attacker feedback on which check
   they tripped. The fixed `reason` list being declared a contract QA asserts on is good
   discipline.
5. **AD-013's separate rate-limit bucket.** Recognising that sharing `loginLimiter` would let
   SSO flooding consume the operator's password-login allowance — and thereby destroy the
   D-9 fallback — is a subtle, correct piece of reasoning. (SEC-002 is about the key, not the
   bucket; the bucket decision is right.)
6. **AD-011 and AD-012 fail closed on nil.** `Enabled()` is nil-safe false and
   `isSetupComplete()` is nil-safe false, so a mis-wired dependency yields "SSO off", never
   "SSO on and ungated". Combined with AD-019, I could find no path to a session before setup
   completes.
7. **AD-004 keeps the IdP out of every startup path.** No discovery at boot, failures never
   cached. NFR-004/AC-019 fall out structurally rather than by testing, and a recovered IdP
   works without a restart.
8. **AD-009's explicit refusal to honour `X-Forwarded-Host`**, and its constraint of
   `X-Forwarded-Proto` to exactly two literal values, are both correct and correctly
   justified.
9. **The access logger records `r.URL.Path` only** (`middleware/logger.go:38`), not
   `RawQuery` — so the authorization `code` never reaches the request log. That is luck the
   story inherits rather than a decision it made, but §12 note 7's explicit never-log list
   turns it into an intentional property.
10. **AD-007's `\` and control-character guards** exceed the reference implementation's
    `startsWith('/') && !startsWith('//')`. Treating AC-026 as a security AC rather than a
    validation AC was the right instinct.
11. **AD-008's decision to extract one shared cookie-minting helper.** Making
    "SSO session ≡ password session" true by construction rather than by two implementations
    agreeing is the single best structural decision in this story. It is also what makes
    SEC-001 a one-line fix instead of a two-site fix.

---

## Verdict Rationale

**REVISE (ARCHITECT).**

There is no CRITICAL finding, and the OIDC protocol design — PKCE, state, nonce, library-based
ID-token validation, no token persistence, closed error vocabulary — is correct and in several
places better than the reference implementation. I would have approved it on those grounds.

Implementation is blocked on three HIGH findings, each of which names a specific ADD decision:

| Finding | ADD decision to change | Why it blocks |
|---|---|---|
| SEC-001 | **AD-008** and the **§6.3 `Secure` footnote** | The story ships a *net weakening* of an existing control on every session in the application. `Strict` was suppressing attacker-initiated top-level navigations; `Lax` permits them; and with no `Secure` flag and no HSTS, one of those navigations exfiltrates the admin JWT in cleartext. The house standard pairs `Lax` with scheme-derived `Secure` for exactly this reason, and the ADD already implements that derivation for `riot_oidc_tx`. |
| SEC-002 | **AD-013** and the **§10 "Rate-limit key spoofing" row** | NFR-008 and AC-027 specify an IP-keyed throttle, and `chimw.RealIP` makes that key attacker-supplied in rIOt's documented no-proxy deployment. As designed, the control passes its unit test and does nothing in production — while the same weakness leaves the shared admin password brute-forceable without limit. A security control that will be signed off as working must actually work. |
| SEC-003 | **AD-016 / §5.2 `ExternalIdentityRepository`**, plus the **A-9 documentation scope** | Delegating authorization to authentik is the right model, but the sanctioned registration path (`register_oidc_app.py`) creates no policy binding, authentik grants unbound applications to all authenticated users, and the implicit-consent flow makes admission silent. The result is a fleet-wide remote-shell grant that no one observes. The fix is not an allowlist — FR-026 stands and no FRD revision is needed — it is making first admission loud and making the binding a verified runbook step. |

SEC-004 additionally requires an AD-005 amendment (key domain separation) and should be
folded into the same revision; it is cheap and removes a cross-protocol MAC collision that is
currently prevented only by a base64 decoding accident.

SEC-005, SEC-006 and SEC-008 are ADD *text* corrections and documentation requirements rather
than mechanism changes — the architect should address them in the same pass so the ADD's
stated security rationale matches what the design actually provides.

SEC-007 must be reflected in AD-002 and ADD §13 before `go get` is run.

SEC-009 and SEC-010 are pre-existing, out of scope for OIDC-001, and must **not** be fixed
inline by engineering. They are recorded here because SEC-010 is what elevates SEC-001 from
theoretical to practical, and both warrant their own stories.

**Routing:** back to `architect` with this report. Do **not** invoke `senior_dev`.

On re-review I expect to move to APPROVED or APPROVED WITH CONDITIONS quickly — none of the
blocking findings requires redesigning the feature, and SEC-003 is the only one that touches
an interface signature.

### Conditions the QA engineer must verify on re-review

Carry these forward whatever the revised ADD says:

- `riot_session` carries `Secure` over `https` and omits it over plain `http`, identically at
  both mint sites (SEC-001, AC-008's byte-identical-attributes assertion).
- The throttle key and the logged client IP cannot be influenced by any request header in a
  no-proxy deployment (SEC-002) — test with a spoofed `X-Forwarded-For`, not just a
  manipulated `RemoteAddr`.
- A first-ever `(issuer, subject)` login produces a persisted `WARN`-or-above entry visible
  in Settings → Logs; a repeat login does not (SEC-003).
- A `riot_session` JWT submitted as `riot_oidc_tx` is rejected at the MAC check, not at a
  later decode step (SEC-004).
- LIVE against authentik: replaying a consumed authorization `code` yields `sso_failed`/
  `sso_expired` and no session (SEC-006).
- `go.mod` resolves `golang.org/x/oauth2` ≥ v0.27.0 and `go-jose/v4` ≥ v4.0.5;
  `govulncheck ./...` is clean (SEC-007).
- Operator documentation states, in the imperative: the authentik group binding is REQUIRED
  and must be verified with a non-member test account before SSO is enabled; redirect URIs
  must use `matching_mode: strict` with one entry per scheme/host/port; regex and wildcard
  matching are prohibited (SEC-003, SEC-008).
- Sentinel-secret sweep per ADD §13 — unchanged by this review; `/debug/pprof` does not leak
  it (SEC-009).

---
---

# Re-review of Revision 2

**Date:** 2026-09-04
**Scope:** ADD Revision 2 (`docs/architecture/OIDC-001-add.md`, revision log at top, new §8.1)
against the findings above. The original findings are unchanged and remain the record of what
was wrong with Revision 1.
**Verdict:** **APPROVED WITH CONDITIONS**

All three blocking findings are resolved at the mechanism level, not papered over. Two of the
resolutions are better than what I specified. No CRITICAL or HIGH findings remain. Six
conditions follow, all documentation-accuracy or test-coverage items; none blocks
`senior_dev` from starting.

Two facts postdate the original report and are incorporated below:
`D:\Repos\Hawaii\scripts\register_oidc_app.py` has gained `--group`, `--redirect-uri`, and
`--launch-url`; the ADD's statements that the script creates no binding and lacks a
redirect-URI override are stale.

---

## Disposition of the original findings

| Finding | Rev 2 resolution | Status |
|---|---|---|
| **SEC-001** (HIGH) | AD-008 + §6.3: both mint sites set `Secure: middleware.RequestScheme(r) == "https"` via the shared helper. §0 amendment 1 records the FR-031 amendment and why. AD-008's rationale states the attack, names the `:80`/no-HSTS precondition, and explains why deriving from the request cannot lock anyone out. §13 has a matching DoD checkbox; §8.1 names two request fixtures. | **RESOLVED** |
| **SEC-002** (HIGH) | New **AD-020**. `chimw.RealIP` is deleted and replaced with `middleware.RealIP(tp)`; `RIOT_TRUSTED_PROXIES` defaults to empty = trust nobody; malformed entries are dropped with a `WARN` rather than aborting boot; the XFF walk is rightmost-untrusted; `ClientIP` and `RequestScheme` are the two exported accessors and every consumer uses them. §0 amendment 2 correctly establishes that throttling *policy* is untouched, so §9 item 12 holds, and that `RIOT_TRUSTED_PROXIES` being optional keeps FR-008 intact. | **RESOLVED** |
| **SEC-003** (HIGH) | AD-016 `RecordLogin` returns `(firstSeen bool, err error)` via `RETURNING (xmax = 0)` — one round trip, no added race, exactly the signal needed. New **AD-021** emits `slog.Warn("new SSO identity granted admin", ...)` on first sight, at `WARN` specifically so `logstore.DBHandler` persists it into Settings → Logs. New §4.5 makes the group binding and its verification hard documentation requirements. AD-015 handles the "write failed, first-seen unknown" case explicitly rather than silently omitting the warning. | **RESOLVED** — see condition C1 |
| **SEC-004** (MED) | AD-005: `txKey = HMAC_SHA256(jwtSecret, "riot-oidc-tx-v1")`, exposed as a testable `DeriveTransactionKey`, with the raw secret forbidden at the `Encode`/`Decode` boundary. | **RESOLVED, exceeded** |
| **SEC-005** (MED) | AD-005 and §10 both replaced. The corrected text states the residual-risk argument I asked for (single fixed `sub:"admin"` principal) and adds a standing trigger: "this analysis must be redone if rIOt ever gains per-user sessions". | **RESOLVED** |
| **SEC-006** (MED) | AD-005 records the delegation explicitly — cookie clearing plus IdP single-use code — states that a copy stays cryptographically valid for the window, and hands QA a named LIVE replay step (§8.1). Option (b) from my resolution, correctly executed. | **RESOLVED** |
| **SEC-007** (MED) | AD-002 is now a floors table with CVE justification per row, including an explicit `go-jose/v4 >= v4.0.5` `require` entry rather than trusting MVS. `govulncheck ./...` is a §13 checkbox. | **RESOLVED** |
| **SEC-008** (MED) | §4.5 items 4–5 make `matching_mode: strict`, per-origin enumeration, and the regex/wildcard prohibition hard requirements, and AD-009's security paragraph now says out loud that the containment is operator configuration. | **RESOLVED** — text needs correcting, conditions C2/C3 |
| **SEC-009 / SEC-010** | Recorded in §10 "out of scope, recorded, must not be fixed inline", listed in §4.5's not-changed list, and reinforced by §12 note 18 and a §13 checkbox. Anticipating that an engineer touching `router.go` will be tempted, and pre-emptively forbidding it, is the right handling. | **CORRECTLY DEFERRED** |
| **SEC-011** | AD-019 switched to exact-path matching on the three routes, while recording that the rev-1 prefix had no reachable bypass. Adopted for the right reason. | **RESOLVED** |
| **SEC-012 / SEC-013 / SEC-014 / SEC-015** | Each acknowledged in §10 or the relevant AD with "no action required" and the reasoning preserved. SEC-012 additionally gets a §8.1 code-review target (label never sourced from a request parameter). | **CORRECTLY DEFERRED** |

Two resolutions are better than specified. AD-005's verification order makes "the MAC check
runs before any decoding" a stated contract with exported `ErrTransactionMAC` /
`ErrTransactionMalformed` sentinels so QA can assert *which* check rejected a value — that
removes the class rather than the instance. AD-020 goes further than the OIDC endpoints and
closes the pre-existing unbounded bcrypt brute force against the shared admin password, which
was the most serious *systemic* consequence of SEC-002 and which rev 1 had accepted.

§8.1 is exactly the artefact this needed: every condition maps to a named test file or a named
document-review target, and the LIVE-only rows are marked as such.

---

## Conditions

Hand these to `senior_dev` and QA verbatim. None blocks implementation start. C1–C3 must be
resolved before the **technical writer** runs; C4–C6 before QA sign-off.

**C1 — Correct §4.5 item 3: the registration script now creates the binding.**
`register_oidc_app.py` has gained `--group`, which creates the authentik group if missing,
creates the `PolicyBinding` (`{"target": app_pk, "group": group_pk, "order": 0, "enabled": True}`),
and prints a loud `WARNING` when the flag is omitted. §4.5 item 3 currently states the script
"creates no policy binding" and that a `--group` flag "is a Hawaii-repo task, out of scope" —
both stale. If the technical writer transcribes it as written, the runbook will send operators
to the admin UI for work a single flag now does, and will never mention the flag. Replace item
3 with: pass `--group <group>` on the registration run; the binding is REQUIRED; the script's
omitted-flag warning is not a substitute for creating it. Item 1 (binding is required) and item
2 (verify with a non-member test account) stay exactly as written — they are the substance and
they remain correct.

**C2 — Correct §4.5 item 5: `--redirect-uri` exists.**
The script now accepts `--redirect-uri` (and `--launch-url`), and its help text already carries
rIOt's own shape as the example plus "Always strict-matched; never use regex matching instead."
§4.5 item 5's "adding that flag is a Hawaii-repo task" is stale. The prohibition on regex and
wildcard matching, the per-origin enumeration requirement, and the explanation of *why* the
operator will be tempted are all still correct and must survive the edit.

**C3 — Add to §4.5 item 5: the script does not update an existing provider's redirect URIs.**
New, found while re-reading the current script. `ensure_provider` (lines 91–94) returns the
existing provider unmodified when one matching the name is found — it never applies
`redirect_uri` on a re-run. So an operator who runs the script once without `--redirect-uri`
and then re-runs it *with* the flag gets no change: the wrong URI silently persists, SSO keeps
failing with a redirect mismatch, and the temptation to switch to regex matching — precisely
what item 5 exists to prevent — comes straight back. `--group` does not have this problem
(`ensure_group_binding` runs on every invocation). The runbook must say: pass `--redirect-uri`
on the **first** run, or correct `redirect_uris` by hand in the authentik UI, and re-running
the script will not fix it.

**C4 — Add the `Secure`-flag consequence to §4.5 item 6 and `.env.example`.**
`RIOT_TRUSTED_PROXIES` guidance is currently justified only by rate-limit granularity and audit
IPs. It has a third consequence that matters more: behind a TLS-terminating proxy with the
variable unset, `RequestScheme` ignores `X-Forwarded-Proto` and finds `r.TLS == nil`, resolves
`http`, and **`riot_session` ships without `Secure`** — the SEC-001 exposure, in the deployment
shape where nginx most commonly also listens on `:80`. AD-020's trust-nobody default is the
right default and AD-008 is right that this cannot lock anyone out, so this is a documentation
condition, not a design change: proxied operators must be told that setting
`RIOT_TRUSTED_PROXIES` is what turns `Secure` on. rIOt's primary direct-exposure shape is fully
protected either way.

**C5 — Pin the all-trusted `X-Forwarded-For` chain in `middleware/clientip_test.go`.**
AD-020 specifies walking XFF right-to-left "skipping entries that are themselves trusted, and
take the first untrusted address as the client", but does not say what happens when *every*
entry in the chain is trusted. That is an unspecified branch in an auth-adjacent code path that
decides a rate-limit key. §8.1 already names this file for the rightmost-untrusted walk and
malformed-CIDR handling; add a case for the all-trusted chain and let the test fix the
behaviour. Either answer (leftmost entry, or fall back to the peer) is defensible; an
unspecified one is not.

**C6 — Soften AD-020's "single place" claim and record the residual.**
AD-020 says it introduces "a single place where every decision about trusting a forwarding
header is made". That is not quite true after the change: `internal/server/handlers/setup.go`
reads `r.Header.Get("X-Real-Ip")` directly at lines 123 and 223 to add an IP SAN to the
generated self-signed TLS certificate, and §4.5 lists `setup.go` as explicitly not changed —
correctly, it is out of scope. Impact is genuinely low: the value is `net.ParseIP`-validated,
and an attacker-chosen SAN in rIOt's own certificate is worthless without the private key. But
the claim as written will be read later as a guarantee. Narrow it to "client identity and
request scheme", note the `setup.go` consumer as a known residual, and carry it into the
follow-up story alongside SEC-009 and SEC-010. **Do not fix `setup.go` in this story.**

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.** No CRITICAL findings. No HIGH findings remain: SEC-001 is closed
by AD-008's scheme-derived `Secure`, SEC-002 by AD-020's trust-gated client identity, and
SEC-003 by AD-016's `firstSeen` signal plus AD-021's persisted `WARN` and §4.5's runbook
requirements. Every MEDIUM is either resolved or explicitly and correctly deferred with its
reasoning preserved.

The six conditions are documentation accuracy (C1–C4, C6) and one test-coverage gap (C5). None
of them describes a vulnerability in the design; C1–C3 exist because the Hawaii tooling moved
after the ADD was written, and shipping a runbook that describes tooling as it was a day ago
would waste operator time and, in C3's case, walk them into the regex-matching trap the ADD
correctly identified.

I want to record one thing for the follow-up backlog rather than as a condition: the
authorization model is now *observable* (AD-021) but still *unenforced* by rIOt (FR-026, by
design). AD-021 is the right answer for this story and I am satisfied with it. If rIOt later
gains per-user sessions, three things must be revisited together — AD-005's login-CSRF
analysis (which now says so itself), BR-002's single-role grant, and AD-021's
one-WARN-per-person signal-to-noise assumption.

**Routing:** invoke `senior_dev`, passing this report alongside the ADD. QA must verify §8.1
plus conditions C4–C6; the technical writer must receive C1–C3 before writing the runbook.
