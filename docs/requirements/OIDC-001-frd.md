# Formal Requirements Document

**Story ID:** OIDC-001
**Title:** Optional "Sign in with authentik" (OIDC) login for the rIOt admin dashboard
**Author:** Business Developer Agent
**Date:** 2026-09-04
**Status:** DRAFT

---

## 1. Executive Summary

rIOt's admin dashboard is protected by a single shared admin password. Operators who run
an identity provider (authentik) want their household/homelab admins to sign in with that
IdP instead of circulating a shared secret. This story adds an **optional** OIDC login
button to the rIOt login screen. The IdP answers exactly one question — "who is this?" —
and on a successful callback rIOt mints its own existing `riot_session` cookie, identical
to a password login. The feature is entirely dormant until three environment variables are
set, password login is never removed or weakened, and an unreachable or misbehaving IdP
degrades to password login rather than locking the operator out.

---

## 2. Background & Context

**Current state.** rIOt is a single-admin application. There is no user table. `POST
/api/v1/auth/login` compares a submitted password against one bcrypt hash held in the
database and issues a `riot_session` JWT cookie (`sub: "admin"`, 24h). A first-run setup
wizard establishes that password; until it completes, `GET /api/v1/auth/check` reports
`needs_setup: true` and the dashboard renders the setup screen instead of the login screen.

**Problem.** A shared password cannot be attributed to a person, cannot be revoked for one
person, and cannot carry MFA. Homelab operators already run an IdP for exactly this reason.

**Why now.** House app standards (`Hawaii\docs\app_standards.md` §4, "OIDC / SSO — baseline
support, off until configured") make OIDC a baseline feature of every in-house app, and a
live reference implementation exists in `photojournal\apps\server\src\auth\oidc\`.

### 2.1 Settled decisions (inputs, not open for redesign)

These were decided before this FRD and are recorded here so requirements below can reference
them rather than re-litigate them.

| # | Decision |
|---|---|
| D-1 | OIDC federates **identity only**. rIOt mints its own `riot_session`. No IdP tokens are stored. |
| D-2 | Password login and the first-run setup wizard remain, untouched and never removable. |
| D-3 | Dormant unless configured: `RIOT_OIDC_ISSUER_URL`, `RIOT_OIDC_CLIENT_ID`, `RIOT_OIDC_CLIENT_SECRET`, optional `RIOT_OIDC_BUTTON_LABEL` (default `Sign in with SSO`). |
| D-4 | Single-admin app: **any** identity the IdP authenticates and authorizes for this application receives the admin session. Access control is delegated to the IdP's application-to-group bindings. |
| D-5 | Authorization Code + PKCE (S256) with `state` and `nonce`; endpoints from issuer discovery. |
| D-6 | Routes under the existing `/api/v1` prefix: `GET /api/v1/auth/oidc`, `GET /api/v1/auth/oidc/start`, `GET /api/v1/auth/oidc/callback`. |
| D-7 | The SSO button is a full-page navigation (plain anchor), never an XHR. |
| D-8 | authentik uses a per-application issuer (`https://auth.example/application/o/<slug>/`) and strict redirect-URI matching; rIOt is typically reached directly at `https://<host>:7331` and terminates its own TLS, so the redirect URI is derived from the incoming request's scheme and host. |
| D-9 | Failed SSO must never dead-end — the login screen always remains usable with the password. |

### 2.2 Two rIOt-specific constraints discovered during intake

1. **There is no `/login` route.** `web/src/App.tsx` renders `<Login>` conditionally when
   `authenticated === false`; the login screen has no URL of its own. The reference
   implementation's `redirect('/login?ssoError=...')` therefore has no valid analogue here.
   The failure landing target must be a URL that *renders* the login screen (see FR-024).
2. **The existing `riot_session` cookie is `SameSite=Strict`.** A successful callback is
   the tail of a cross-site redirect chain originating at the IdP, so a Strict cookie can be
   withheld on the very next navigation, producing "logged in but bounced back to the login
   screen; works after a manual refresh." FR-036 makes the required end-state observable
   without prescribing the mechanism.

---

## 3. Actors

| Actor | Description | Permissions |
|---|---|---|
| Operator | Owner of the self-hosted rIOt server; edits `.env` and restarts the container. | Sets/clears the OIDC env vars; already holds the admin password. |
| Dashboard admin | A household/homelab person signing in through a browser. | After a successful SSO login, full rIOt admin — identical to a password login. No lesser tier exists. |
| Identity provider (authentik) | External OIDC provider. Authenticates the person and decides whether they are authorized for the rIOt application. | Asserts identity claims (`iss`, `sub`, `email`, `email_verified`). Grants nothing directly inside rIOt. |
| rIOt server | OIDC relying party. | Validates the IdP response, mints `riot_session`, records the external identity. |
| rIOt agent | Devices pushing telemetry with `X-rIOt-Key` / mTLS. | Unaffected. Out of scope. |

---

## 4. Functional Requirements

### 4.1 Configuration and dormancy

- **FR-001:** The server must read `RIOT_OIDC_ISSUER_URL`, `RIOT_OIDC_CLIENT_ID`,
  `RIOT_OIDC_CLIENT_SECRET`, and `RIOT_OIDC_BUTTON_LABEL` from the environment at startup.
- **FR-002:** The server must treat SSO as **configured** only when `RIOT_OIDC_ISSUER_URL`,
  `RIOT_OIDC_CLIENT_ID`, and `RIOT_OIDC_CLIENT_SECRET` are all non-empty after trimming
  surrounding whitespace; if any one is empty or absent, SSO must be **dormant**.
- **FR-003:** When SSO is configured and `RIOT_OIDC_BUTTON_LABEL` is empty or absent, the
  server must use the label `Sign in with SSO`.
- **FR-004:** When SSO is dormant, `GET /api/v1/auth/oidc/start` and
  `GET /api/v1/auth/oidc/callback` must respond `404`.
- **FR-005:** When SSO is dormant, `GET /api/v1/auth/oidc` must respond `200` with
  `{"available": false, "label": ""}`. (It must not 404 — the frontend needs a definitive
  negative answer to suppress the button.)
- **FR-006:** The server must start successfully and serve all existing functionality when
  SSO is dormant, when the issuer URL is malformed, and when the IdP is unreachable. The
  server must not contact the IdP during startup.
- **FR-007:** The server must not write the value of `RIOT_OIDC_CLIENT_SECRET` to any log,
  API response, error message, or frontend asset.
- **FR-008:** Existing deployments must continue to operate with no change to their `.env`;
  no new environment variable may become required by this story.

### 4.2 Availability endpoint

- **FR-009:** `GET /api/v1/auth/oidc` must be reachable without a `riot_session` cookie.
- **FR-010:** When SSO is configured and setup is complete, `GET /api/v1/auth/oidc` must
  respond `200` with `{"available": true, "label": "<effective label>"}`.
- **FR-011:** `GET /api/v1/auth/oidc` must answer from local configuration only and must not
  make any network request to the IdP.

### 4.3 Login initiation (`/start`)

- **FR-012:** When SSO is configured, `GET /api/v1/auth/oidc/start` must respond `302` to the
  authorization endpoint obtained from the issuer's OIDC discovery document.
- **FR-013:** The authorization request must use response type `code` with a PKCE code
  challenge computed with method `S256`, and must include a `state` value, a `nonce` value,
  and the scopes `openid email profile`.
- **FR-014:** The server must retain the `state`, `nonce`, PKCE code verifier, and intended
  post-login return path for the duration of the round trip in a cookie that is `httpOnly`,
  expires no more than 5 minutes after being set, and is present on the browser's return
  navigation from the IdP.
- **FR-015:** The `redirect_uri` sent to the IdP must be the incoming request's scheme and
  host concatenated with `/api/v1/auth/oidc/callback`, and must be byte-for-byte identical
  on `/start` and on the token exchange at `/callback`.
- **FR-016:** The post-login return path must be a same-origin absolute path beginning with a
  single `/`; any other supplied value must be replaced with `/`.
- **FR-017:** If discovery or authorization-URL construction fails, `/start` must respond
  `302` to the login landing URL with an error code (FR-024) and must not respond with a 5xx
  status or a JSON error body.

### 4.4 Callback (`/callback`)

- **FR-018:** The callback must reject the request when the transaction cookie is absent or
  expired, and must not mint a session.
- **FR-019:** The callback must reject the request when the `state` parameter does not exactly
  match the `state` held in the transaction, and must not mint a session.
- **FR-020:** The callback must reject the request when the ID token's `nonce` claim does not
  exactly match the `nonce` held in the transaction, and must not mint a session.
- **FR-021:** The callback must reject the request when ID token signature, issuer, audience,
  or expiry validation fails, and must not mint a session. Validation must be performed by the
  OIDC library, not by hand-written token parsing.
- **FR-022:** The callback must clear the transaction cookie on every outcome — success,
  failure, and rejection.
- **FR-023:** On successful validation, the server must issue a `riot_session` cookie whose
  claims, signing key, lifetime, and cookie attributes are the same as those issued by
  `POST /api/v1/auth/login`, and must respond `302` to the dashboard root (`/`).
- **FR-024:** On any failure, the callback must respond `302` to a URL that renders the rIOt
  login screen, carrying a machine-readable error code from the fixed set defined in §7.4. It
  must not return a JSON body, an HTML error page, a stack trace, or a raw IdP error to the
  browser.
- **FR-025:** The server must not persist the IdP's ID token, access token, or refresh token,
  and must not retain them beyond the callback request.
- **FR-026:** The server must not require the authenticated identity to appear in any local
  allowlist; every identity that completes validation must receive the admin session (D-4).
- **FR-027:** The server must record the external identity keyed by (`issuer`, `subject`) —
  creating the record with a first-login timestamp on first sight, and updating the
  last-login timestamp and email on every subsequent successful login.
- **FR-028:** A failure to write or update the external-identity record must be logged at
  error level and must not prevent the session from being issued.

### 4.5 Interaction with setup and password login

- **FR-029:** While first-run setup is incomplete, `GET /api/v1/auth/oidc/start` and
  `GET /api/v1/auth/oidc/callback` must respond `404`, and `GET /api/v1/auth/oidc` must
  report `{"available": false}` regardless of the OIDC environment variables.
- **FR-030:** An SSO login must not create, set, or modify the admin password hash, and must
  not mark first-run setup complete.
- **FR-031:** The behaviour and response contract of `POST /api/v1/auth/login`,
  `POST /api/v1/auth/logout`, `POST /api/v1/auth/change-password`, and
  `GET /api/v1/auth/check` must be unchanged by this story.
- **FR-032:** `POST /api/v1/auth/logout` must clear a session that was established by SSO
  exactly as it clears one established by password.

### 4.6 Dashboard (React)

- **FR-033:** The login screen must request `GET /api/v1/auth/oidc` when it mounts and must
  render the SSO button only when the response is `{"available": true}`.
- **FR-034:** The SSO button must be a plain anchor performing a full-page navigation to
  `/api/v1/auth/oidc/start`; it must not be triggered via `fetch`/`XMLHttpRequest`.
- **FR-035:** The login screen must render the password form and accept a password login
  whenever it is shown — when SSO is dormant, when the availability request fails or times
  out, and when an SSO error code is present in the URL.
- **FR-036:** After a successful callback, the browser must arrive at the dashboard in an
  authenticated state without the user performing a manual reload or a second navigation.
- **FR-037:** When the login screen loads with an SSO error code in the URL, it must display a
  human-readable message for that code, must display a generic failure message for an
  unrecognised code, and must remove the error code from the visible URL so a subsequent
  refresh does not re-display it.
- **FR-038:** The dashboard must not display, request, or embed the OIDC client secret or the
  issuer's client credentials.

---

## 5. Non-Functional Requirements

- **NFR-001 [Performance]:** `GET /api/v1/auth/oidc` must respond within 100 ms at p95 on the
  reference deployment and must perform no outbound network I/O.
- **NFR-002 [Performance]:** `GET /api/v1/auth/oidc/start` must emit its redirect within 2 s
  when discovery metadata is already cached.
- **NFR-003 [Availability]:** Every outbound request to the IdP (discovery, token exchange,
  JWKS fetch) must be bounded by a timeout of no more than 10 s, after which the attempt fails
  to the login screen per FR-017/FR-024.
- **NFR-004 [Availability]:** With the IdP unreachable, the following must continue to
  function unchanged: password login, the setup wizard, `/health`, agent registration,
  heartbeat and telemetry ingest, the WebSocket feed, and all dashboard pages.
- **NFR-005 [Security]:** `state`, `nonce`, and the PKCE code verifier must each be generated
  from a cryptographically secure random source with at least 128 bits of entropy; the code
  verifier must be 43–128 characters as required by RFC 7636.
- **NFR-006 [Security]:** The transaction cookie must be `httpOnly`, must carry `Secure` when
  the request scheme is `https`, must have a max age of at most 300 s, and must be usable only
  once.
- **NFR-007 [Security]:** rIOt must verify the IdP's TLS certificate against the system trust
  store on all outbound IdP calls; certificate verification must not be disabled or made
  configurable by this story.
- **NFR-008 [Security]:** `/api/v1/auth/oidc/start` and `/api/v1/auth/oidc/callback` must be
  rate limited per client IP with a policy at least as strict as the one applied to password
  login, and must be keyed on IP rather than on any identity claim.
- **NFR-009 [Audit]:** Every SSO attempt must produce one structured server log entry
  recording outcome (success or the specific failure reason), client IP, and — on success —
  issuer and subject. Log entries must not contain tokens, the client secret, or the PKCE
  verifier.
- **NFR-010 [Compatibility]:** The feature must work when rIOt terminates its own TLS on port
  7331 with a self-signed certificate and when rIOt is served over plain HTTP on a LAN.
- **NFR-011 [Data]:** The schema change must be a single additive migration with a working
  rollback, and must not alter or drop any existing table or column.

---

## 6. Business Rules

- **BR-001:** The IdP establishes identity only. It never owns, extends, or terminates a rIOt
  session. Session lifetime is governed solely by rIOt's existing 24-hour `riot_session`.
- **BR-002:** Any identity the IdP authenticates and authorizes for the rIOt application
  receives full admin access. rIOt performs no additional authorization check.
- **BR-003:** Restricting who may sign in is the operator's responsibility, exercised through
  the IdP's application-to-group bindings. This must be stated explicitly in the operator
  documentation.
- **BR-004:** Password login must remain available at all times. This story provides no
  mechanism, flag, or setting to disable it.
- **BR-005:** No IdP-issued token may be persisted to the database, to disk, or to a cookie.
- **BR-006:** rIOt remains a single-admin application. This story introduces no user table, no
  roles, and no per-person permissions.
- **BR-007:** `email_verified` is recorded for audit but must not gate access. rIOt has no
  local account to protect against email reassignment, so the house rule about
  email-based account matching (`app_standards.md` §4 OIDC rule 5) does not apply here —
  identity is keyed on (`issuer`, `subject`) and grants a fixed, single role.
- **BR-008:** Signing out of the IdP does not sign the user out of rIOt, and signing out of
  rIOt does not sign the user out of the IdP. Single-logout is not part of this story.
- **BR-009:** Configuring SSO is not a substitute for completing first-run setup. Setup always
  runs first on a fresh install.

---

## 7. Data Requirements

### 7.1 Entities

**External identity (new, persistent).** One row per distinct IdP subject that has
successfully signed in. Purely an audit record — nothing in the login path reads it to make
an access decision.

| Attribute | Required | Notes |
|---|---|---|
| Identifier | yes | Surrogate key. |
| Issuer | yes | The `iss` claim as asserted in the validated ID token. |
| Subject | yes | The `sub` claim. |
| Email | no | The `email` claim; null when the IdP asserts none. |
| Email verified | no | The `email_verified` claim as asserted; recorded, never gated on (BR-007). |
| First login at | yes | Set once, on the first successful login for this (issuer, subject). |
| Last login at | yes | Updated on every successful login. |

**Login transaction (new, ephemeral — never persisted to the database).** `state`, `nonce`,
PKCE code verifier, return path. Lifetime ≤ 5 minutes, single use.

**Existing entities touched:** none. The admin password hash, setup-complete flag, JWT secret,
devices, events, and settings are all unchanged.

### 7.2 Validation rules

| Rule | Constraint |
|---|---|
| V-001 | Issuer and subject must both be non-empty; a validated token missing either is a `sso_failed` rejection. |
| V-002 | (Issuer, subject) must be unique — a repeat login updates the existing row rather than inserting a second. |
| V-003 | Email is stored as asserted; no normalisation is used for matching, because no matching occurs. |
| V-004 | Return path must match `^/(?!/)` — a single leading slash, not a protocol-relative `//host`. |
| V-005 | Issuer URL configuration must parse as an absolute `http`/`https` URL; a malformed value leaves SSO dormant and logs a configuration warning at boot (it must not abort boot). |

### 7.3 State transitions

**Feature state**

| From | Event | To |
|---|---|---|
| Dormant | All three env vars set and setup complete | Available |
| Available | Discovery or IdP call fails at attempt time | Available (degraded — button still shown, attempts fail to the login screen) |
| Available | Any of the three env vars cleared, server restarted | Dormant |
| Any | Setup incomplete | Suppressed (behaves as Dormant) |

**Login attempt**

| From | Event | To | Observable outcome |
|---|---|---|---|
| Idle | `GET /start` succeeds | Pending (transaction cookie set) | 302 to IdP |
| Idle | `GET /start`, discovery fails | Failed | 302 to login screen, `sso_unavailable` |
| Pending | Callback, all validations pass | Authenticated | `riot_session` set, 302 to `/`, identity row written |
| Pending | Callback, transaction missing/expired | Failed | 302 to login screen, `sso_expired` |
| Pending | Callback, `state`/`nonce`/token validation fails | Failed | 302 to login screen, `sso_failed` |
| Pending | Callback carries IdP `error=access_denied` | Failed | 302 to login screen, `sso_denied` |
| Pending | Callback, IdP unreachable during token exchange | Failed | 302 to login screen, `sso_unavailable` |

No transition out of Failed exists other than starting a new attempt or logging in with the
password.

### 7.4 Error code vocabulary

| Code | Meaning shown to the user (wording is the writer's, meaning is fixed) |
|---|---|
| `sso_failed` | Sign-in could not be completed. Details are in the server log. |
| `sso_expired` | The sign-in attempt timed out. Try again. |
| `sso_denied` | The identity provider refused the sign-in for this account. |
| `sso_unavailable` | The identity provider could not be reached. |

State mismatch, nonce mismatch, and token-validation failures all surface as `sso_failed` to
the browser; the specific reason is recorded in the server log only (NFR-009).

---

## 8. Acceptance Criteria

**AC-001 — Dormant by default: no button** *(FR-002, FR-005, FR-033)*
- **Given** a rIOt server with setup complete and none of the `RIOT_OIDC_*` variables set
- **When** an unauthenticated user loads the dashboard
- **Then** `GET /api/v1/auth/oidc` responds `200` with `{"available": false, "label": ""}`
- **And** the login screen shows no SSO button
- **And** the password form is present and accepts a valid password.

**AC-002 — Dormant by default: endpoints 404** *(FR-004)*
- **Given** SSO is dormant
- **When** `GET /api/v1/auth/oidc/start` and `GET /api/v1/auth/oidc/callback` are requested
- **Then** each responds `404`
- **And** no cookie is set on either response.

**AC-003 — Partial configuration stays dormant** *(FR-002)*
- **Given** `RIOT_OIDC_ISSUER_URL` and `RIOT_OIDC_CLIENT_ID` are set but
  `RIOT_OIDC_CLIENT_SECRET` is empty
- **When** the server starts and `GET /api/v1/auth/oidc` is requested
- **Then** the server starts successfully
- **And** the response is `{"available": false, "label": ""}`
- **And** `/start` responds `404`.

**AC-004 — Configured: availability and default label** *(FR-003, FR-010)*
- **Given** all three `RIOT_OIDC_*` variables are set and `RIOT_OIDC_BUTTON_LABEL` is unset
- **When** `GET /api/v1/auth/oidc` is requested
- **Then** the response is `{"available": true, "label": "Sign in with SSO"}`.

**AC-005 — Custom button label** *(FR-003, FR-010, FR-033)*
- **Given** SSO is configured with `RIOT_OIDC_BUTTON_LABEL="Sign in with authentik"`
- **When** an unauthenticated user loads the login screen
- **Then** a button labelled "Sign in with authentik" is displayed.

**AC-006 — Start redirects to the IdP with PKCE, state and nonce** *(FR-012, FR-013, FR-015)*
- **Given** SSO is configured and discovery succeeds
- **When** `GET /api/v1/auth/oidc/start` is requested over `https` at host `riot.example:7331`
- **Then** the response is `302` to the IdP's discovered authorization endpoint
- **And** the location carries `response_type=code`, `code_challenge_method=S256`, a
  `code_challenge`, a `state`, a `nonce`, and `scope` containing `openid`
- **And** `redirect_uri` is exactly `https://riot.example:7331/api/v1/auth/oidc/callback`
- **And** the response sets an `httpOnly` transaction cookie with a max age ≤ 300 s.

**AC-007 — SSO button is a full-page navigation** *(FR-034)*
- **Given** the login screen is displayed with SSO available
- **When** the SSO control is inspected
- **Then** it is an anchor element whose href is `/api/v1/auth/oidc/start`
- **And** activating it performs a top-level navigation, issuing no `fetch`/XHR request.

**AC-008 — Successful login mints the standard session cookie** *(FR-021, FR-023)*
- **Given** a pending transaction created by `/start`
- **When** the IdP returns to `/api/v1/auth/oidc/callback` with a valid code and matching
  `state`, and the ID token validates with the matching `nonce`
- **Then** the response is `302` to `/`
- **And** a `riot_session` cookie is set whose name, claims (`sub: "admin"`), signing key,
  lifetime, and cookie attributes are identical to those issued by `POST /api/v1/auth/login`
- **And** a subsequent `GET /api/v1/auth/check` presenting that cookie returns
  `{"authenticated": true, "needs_setup": false}`.

**AC-009 — Landing on the dashboard requires no manual reload** *(FR-036)*
- **Given** a browser completing a successful SSO round trip from the IdP
- **When** the callback's redirect is followed
- **Then** the dashboard renders in the authenticated state on that navigation
- **And** the login screen is not shown at any point after the callback.

**AC-010 — Identity audit row written on first login** *(FR-027)*
- **Given** an IdP identity with issuer `I` and subject `S` that has never signed in
- **When** that identity completes a successful SSO login
- **Then** exactly one external-identity record exists for (`I`, `S`)
- **And** it carries the asserted email, a first-login timestamp, and a last-login timestamp
  equal to the time of this login.

**AC-011 — Repeat login updates rather than duplicates** *(FR-027, V-002)*
- **Given** an external-identity record already exists for (`I`, `S`) with first-login time `T0`
- **When** the same identity signs in again at `T1`
- **Then** still exactly one record exists for (`I`, `S`)
- **And** its first-login timestamp is still `T0`
- **And** its last-login timestamp is `T1`.

**AC-012 — Any IdP-authorized identity becomes admin** *(FR-026, BR-002)*
- **Given** SSO is configured and a second, previously unseen identity is authorized for the
  application by the IdP
- **When** that identity completes a successful SSO login
- **Then** a `riot_session` cookie is issued
- **And** an admin-only endpoint such as `GET /api/v1/devices` returns `200` with that cookie
- **And** no local allowlist, approval step, or invitation was required.

**AC-013 — State mismatch is rejected** *(FR-019, FR-024)*
- **Given** a pending transaction whose `state` is `A`
- **When** the callback is requested with `state=B`
- **Then** no `riot_session` cookie is set
- **And** the response is `302` to the login landing URL carrying `sso_failed`
- **And** the response body is not JSON.

**AC-014 — Nonce mismatch is rejected** *(FR-020, FR-024)*
- **Given** a pending transaction whose `nonce` is `N1`
- **When** the callback validates an ID token whose `nonce` claim is `N2`
- **Then** no `riot_session` cookie is set
- **And** the response is `302` to the login landing URL carrying `sso_failed`.

**AC-015 — Missing or expired transaction is rejected** *(FR-018, FR-022, FR-024)*
- **Given** no transaction cookie is present (never set, or older than its 5-minute lifetime)
- **When** the callback is requested with an otherwise well-formed code and state
- **Then** no `riot_session` cookie is set
- **And** the response is `302` to the login landing URL carrying `sso_expired`
- **And** the transaction cookie is cleared on the response.

**AC-016 — Transaction cookie is cleared on success too** *(FR-022)*
- **Given** a successful SSO login
- **When** the callback responds
- **Then** the response clears the transaction cookie
- **And** replaying the same callback URL a second time does not produce a session.

**AC-017 — IdP down: start degrades to the login screen** *(FR-017, FR-024, NFR-003, NFR-004)*
- **Given** SSO is configured and the issuer host is unreachable
- **When** `GET /api/v1/auth/oidc/start` is requested
- **Then** the response is `302` to the login landing URL carrying `sso_unavailable` within
  the configured timeout
- **And** the response status is not 5xx and the body is not JSON
- **And** the login screen renders with a readable message and a working password form
- **And** a password login submitted immediately afterwards succeeds.

**AC-018 — IdP error response lands back on the login screen** *(FR-024, D-9)*
- **Given** a pending transaction
- **When** the IdP redirects to the callback with `error=access_denied` instead of a code
- **Then** no `riot_session` cookie is set
- **And** the response is `302` to the login landing URL carrying `sso_denied`
- **And** the password form on the resulting page accepts a valid password and signs the user
  in.

**AC-019 — IdP down does not affect the rest of the server** *(NFR-004)*
- **Given** SSO is configured and the issuer host is unreachable
- **When** `/health`, agent registration, heartbeat, telemetry ingest, and
  `POST /api/v1/auth/login` are exercised
- **Then** every one of them behaves exactly as it does with SSO dormant.

**AC-020 — Fresh install runs the setup wizard first** *(FR-029, FR-030, BR-009)*
- **Given** a server with an empty database, all three `RIOT_OIDC_*` variables set
- **When** a user loads the dashboard
- **Then** `GET /api/v1/auth/check` reports `needs_setup: true` and the setup wizard is shown
- **And** `GET /api/v1/auth/oidc` reports `{"available": false}`
- **And** `GET /api/v1/auth/oidc/start` responds `404`
- **And** no `riot_session` cookie can be obtained through any OIDC endpoint until setup
  completes.

**AC-021 — SSO does not complete setup or set a password** *(FR-030)*
- **Given** setup is complete and an admin password exists
- **When** an SSO login succeeds
- **Then** the stored admin password hash is unchanged
- **And** the setup-complete flag is unchanged
- **And** `POST /api/v1/auth/login` with the original password still succeeds.

**AC-022 — Logout clears an SSO-established session** *(FR-032)*
- **Given** a session established through SSO
- **When** `POST /api/v1/auth/logout` is called
- **Then** the `riot_session` cookie is cleared
- **And** a subsequent `GET /api/v1/auth/check` returns `{"authenticated": false}`
- **And** no request is made to the IdP.

**AC-023 — No IdP tokens are persisted** *(FR-025, BR-005)*
- **Given** a successful SSO login
- **When** the database and all cookies set on the client are inspected
- **Then** no IdP ID token, access token, or refresh token is present in any of them.

**AC-024 — Client secret never leaks** *(FR-007, FR-038)*
- **Given** SSO is configured with a known client secret value
- **When** server logs at all levels, every `/api/v1/auth/oidc*` response, and the served
  frontend bundle are searched for that value
- **Then** it appears in none of them.

**AC-025 — Availability failure does not break the login screen** *(FR-035)*
- **Given** `GET /api/v1/auth/oidc` fails or times out
- **When** the login screen renders
- **Then** no SSO button is shown
- **And** the password form is present and a valid password signs the user in.

**AC-026 — Open redirect is refused** *(FR-016, V-004)*
- **Given** SSO is configured
- **When** `GET /api/v1/auth/oidc/start?returnUrl=https://evil.example` is requested, and
  again with `returnUrl=//evil.example`
- **Then** in both cases the post-login destination recorded in the transaction is `/`
- **And** a subsequent successful callback redirects to `/`, never to `evil.example`.

**AC-027 — SSO attempts are rate limited by IP** *(NFR-008)*
- **Given** SSO is configured
- **When** a single client IP issues callback requests beyond the password-login attempt
  allowance within the throttle window
- **Then** further attempts from that IP are throttled
- **And** the throttle key is the client IP, not any identity claim.

**AC-028 — Every attempt is auditable in the log** *(NFR-009)*
- **Given** one successful login, one state-mismatch rejection, and one IdP-unreachable
  attempt
- **When** the server log is inspected
- **Then** each attempt has one structured entry with its outcome and client IP
- **And** the successful entry records issuer and subject
- **And** no entry contains a token, the client secret, or the PKCE verifier.

**AC-029 — Migration is additive and reversible** *(NFR-011)*
- **Given** a database populated by the previous release
- **When** the migration is applied and then rolled back
- **Then** the external-identity table is created and then removed
- **And** every pre-existing table, column, and row is unchanged after both operations.

**AC-030 — Upgrade requires no configuration change** *(FR-008)*
- **Given** an existing deployment whose `.env` contains no `RIOT_OIDC_*` variables
- **When** the server is upgraded to the release containing this story and started
- **Then** it starts successfully, migrations apply, and the dashboard behaves exactly as
  before, with no SSO button.

---

## 9. Out of Scope

1. Multi-user support, a user table, roles, or per-person permissions inside rIOt.
2. Mapping IdP `groups` claims to rIOt roles or feature access.
3. Disabling password login, whether by env var, admin setting, or otherwise.
4. RP-initiated logout, back-channel logout, and session synchronisation with the IdP.
5. Refreshing or reusing IdP tokens after the callback.
6. Configuring OIDC from the admin settings panel — configuration is env-only in this story.
7. Support for more than one IdP simultaneously.
8. Account-linking or "link to existing account" flows (there is no local account to link).
9. SSO for rIOt agents, device registration, or any `X-rIOt-Key` / mTLS path.
10. Automated registration of the rIOt application inside authentik.
11. Deploying authentik itself.
12. Changing the `riot_session` lifetime, claims, signing algorithm, or throttling policy of
    the existing password login.
13. Enforcing that the operator has actually restricted the application in the IdP — rIOt
    cannot verify this and will not try (BR-003).

---

## 10. Assumptions

- **A-1:** Configuration is read once at startup; changing an `RIOT_OIDC_*` value requires a
  restart. No hot reload is expected.
- **A-2:** The operator registers the rIOt application in authentik by hand and pastes the
  resulting client id/secret and per-application issuer URL into `.env`.
- **A-3:** The operator registers the exact redirect URI at the IdP for each scheme/host that
  rIOt is reached by. rIOt derives it from the request (D-8); mismatches surface as an IdP
  error and land on the login screen per AC-018.
- **A-4:** The `openid email profile` scope set is sufficient. No custom claims are requested.
- **A-5:** A missing or unverified `email` claim does not block login; it is recorded as
  asserted (BR-007).
- **A-6:** The failure landing target is the dashboard root with an error query parameter
  (for example `/?sso_error=<code>`), because no `/login` route exists (§2.2). The exact
  parameter name is an architect's decision; the required behaviour is FR-024 and FR-037.
- **A-7:** Discovery metadata may be cached in memory after a first successful fetch, and a
  failed fetch is not cached, so a recovered IdP works on the next attempt without a restart.
- **A-8:** The external-identity table is never read to make an access decision, so it may be
  emptied by an operator without affecting logins.
- **A-9:** `docs/` and `README.md` gain an operator section covering the three env vars, the
  authentik application setup, and the "the IdP decides who gets in" warning (BR-003). The
  technical writer owns the wording.

---

## 11. Open Questions

Each carries the answer assumed by this FRD. If the operator disagrees, the FRD changes; the
architect should not silently choose differently.

- **OQ-1 — Should `GET /api/v1/auth/oidc` 404 when dormant, matching the "endpoints return
  404" phrasing?** *Assumed:* No — availability returns `200 {"available": false}` (FR-005)
  so the frontend gets a definitive answer; only `/start` and `/callback` return 404.
- **OQ-2 — Should a failed identity-audit write abort the login?** *Assumed:* No. The session
  is issued and the failure is logged at error level (FR-028); losing admin access over an
  audit-row write is worse than a missing audit row.
- **OQ-3 — Should SSO logins raise a rIOt event (visible in the Alerts/events feed) as well as
  a log line?** *Assumed:* No for this story — structured log only (NFR-009). An informational
  event for "new external identity seen for the first time" is a good follow-up.
- **OQ-4 — Should the external-identity record be visible in the dashboard (for example under
  Settings > General)?** *Assumed:* No. Audit rows are inspected in the database or logs for
  now; a Settings view is a follow-up story.
- **OQ-5 — Should `state`/`nonce` mismatch surface a distinct browser-visible error code
  rather than folding into `sso_failed`?** *Assumed:* No — fold into `sso_failed`, log the
  precise reason server-side (§7.4).
- **OQ-6 — Should the transaction cookie survive a rIOt restart mid-flight?** *Assumed:* Not
  required. A restart during the ≤5-minute window fails the attempt to `sso_expired`, and the
  user retries.
- **OQ-7 — Is a 5-minute transaction lifetime right for an IdP that may prompt for MFA?**
  *Assumed:* Yes, matching the reference implementation. If authentik's MFA prompts regularly
  exceed it, raise to 10 minutes — a configuration-free constant either way.
- **OQ-8 — Should `RIOT_OIDC_BUTTON_LABEL` be length-limited?** *Assumed:* Yes — truncate or
  reject beyond a sane bound (for example 64 characters) so the login screen cannot be
  disfigured; this is a display detail, not a security control.

---

## 12. Dependencies

| Dependency | Nature | Status |
|---|---|---|
| An authentik instance | Required to exercise AC-006 through AC-018 end to end. Per `app_standards.md` §4, no IdP is deployed on hawaii yet. | **Blocking for live verification only.** Automated ACs must be satisfiable against a stubbed/mock issuer; a live authentik run is a separate verification step. |
| `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2` | New Go dependencies mandated by D-5 and `app_standards.md` §4 rule 3. | New to `go.mod`. |
| Existing session machinery (`internal/server/handlers` login/JWT path, `riot_session` cookie) | The callback must reuse it verbatim (FR-023). | Present. |
| First-run setup state (`adminRepo.IsSetupComplete`) | Gates FR-029. | Present. |
| golang-migrate migration runner (`cmd/riot-server/migrations/`) | Carries the external-identity table (NFR-011). | Present. |
| React login screen (`web/src/pages/Login.tsx`) and `useAuth` | Host the SSO button and the error message (FR-033 – FR-037). | Present. |
| Login throttling policy | NFR-008 requires parity with password login; if no IP throttle exists on `POST /api/v1/auth/login` today, one must be introduced for the SSO endpoints at minimum. | **Verify** — the current `Login` handler shows no throttle. Flagged for the architect. |
| Operator documentation (`README.md`, `docs/`) | A-9. | Technical writer, end of pipeline. |
