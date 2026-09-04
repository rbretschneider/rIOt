# QA Report

**Story ID:** OIDC-001
**QA Engineer:** QA Agent
**Date:** 2026-09-04
**Verdict:** PASS WITH NOTES

---

## Test Run Summary

All numbers below were reproduced independently on this machine, not copied from the implementation report.

- **Go:** `go test -count=1 ./...` → all 17 tested packages `ok`, 0 failures. `go test -v ./...` → **666 `--- PASS`, 0 `--- FAIL`, 2 `--- SKIP`** (the two skips are pre-existing, platform-gated agent-collector tests — `TestAC005_SecurityCollector_CounterNotReady_ReportsZero` / `TestAC004_SecurityCollector_UsesCounterNotJournalctl` — unrelated to OIDC-001, present on `main` before this story). Matches the implementation report's count exactly.
- `go build ./...` → clean. `go vet ./...` → clean (this repo's CI lint job).
- **Frontend:** `vitest run` (invoked directly via `node`, `npm`'s shell wrapper is broken on this Windows/Git-Bash combination — see Environment Notes) → **26 test files, 357 tests passing** before any QA changes, **359 passing** after QA added 2 tests to `Login.test.tsx` (12 tests in that file, up from 10).
- `npx tsc --noEmit` → clean.
- `npm run build` (via direct `node`/`vite` invocation) → succeeds, 396 KB gzip.
- `VITE_DEMO=true` build (via direct `node`/`vite` invocation) → succeeds, 405 KB gzip. Confirmed `getSSOAvailability` exists in both `api/client.ts` and `api/demo-client.ts`; the demo build does not fail to typecheck.
- `govulncheck ./...` → 23 "affected" vulnerabilities, independently re-run and confirmed: **zero involve `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2`, or `github.com/go-jose/go-jose/v4`** — all 23 are pre-existing stdlib (this workstation's go1.26 toolchain vs. CI's pinned go1.24) and pre-existing third-party deps (`x/net`, `x/text`, `pgx/v5`, `docker/docker`). `go list -m all` confirms `golang.org/x/oauth2 v0.30.0` (≥ v0.27.0 floor) and `github.com/go-jose/go-jose/v4 v4.1.4` (≥ v4.0.5 floor). `go.mod`'s `go` directive is unchanged at `1.24.0`.
- **AC-029 (migration, additive + reversible):** independently re-verified against a disposable `postgres:16-alpine` container (not just trusting the implementation report's own manual run) — built and booted the real `riot-server` binary with no OIDC vars, confirmed clean boot, `schema_migrations.version = 22, dirty = false`, `external_identities` schema matches ADD §5.1 exactly (columns, nullability, the `UNIQUE (issuer, subject)` constraint, no extra index), table count 25 → 26. Applied `000022...down.sql` directly: table count returned to 25, `external_identities` gone, `admin_config` (`admin_password_hash`, `setup_complete`) unchanged. Matches the implementation report's own findings.
- **Flaky tests:** none found. Ran the full Go suite twice (cached and `-count=1`) and the frontend suite twice; results were stable both times.
- **Suspicious output:** the `ECONNREFUSED :3000` stack traces in the frontend run are pre-existing noise from an unrelated mocked-network test (`DeviceDetail.test.tsx`), present on `main` before this story — not a regression.

---

## AC Coverage Audit

Every AC-001 through AC-030 has at least one test named with its AC reference, mechanically confirmed via `grep -rn "^func Test(AC|SEC|C4|C5)"` across the Go tree and `\[AC-\d+\]` across `web/src`. Test bodies for the security-critical items were read in full (not just their names) — see Test Quality Findings below for what that reading surfaced.

| AC ID | Status | Tests Covering It | Gap Description |
|-------|--------|-------------------|-----------------|
| AC-001 | ✅ COVERED | `oidc_handler_test.go:TestAC001_OIDCAvailability_DormantByDefault`; `Login.test.tsx:[AC-001]` (2 tests) | — |
| AC-002 | ✅ COVERED | `oidc_handler_test.go:TestAC002_OIDCStartAndCallback_404WhenDormant` | — |
| AC-003 | ✅ COVERED | `config_test.go:TestAC003_LoadConfig_PartialOIDCConfig_FieldsPopulatedIndependently`; `oidc/config_test.go:TestAC003_New_PartialConfig_Dormant`; `oidc_handler_test.go:TestAC003_PartialConfig_DormantAndStartNotFound` | — |
| AC-004 | ✅ COVERED | `oidc/config_test.go:TestAC004_New_AllConfigured_DefaultLabel`; `oidc_handler_test.go:TestAC004_OIDCAvailability_ConfiguredDefaultLabel` | — |
| AC-005 | ✅ COVERED | `oidc_handler_test.go:TestAC005_OIDCAvailability_CustomLabel`; `Login.test.tsx:[AC-005]` | — |
| AC-006 | ✅ COVERED | `oidc/service_test.go:TestAC006_BeginLogin_AuthURLShapeAndPKCE`; `oidc_handler_test.go:TestAC006_OIDCStart_RedirectsWithPKCEStateNonce` | Both assert `response_type`, `code_challenge_method=S256`, non-empty `code_challenge`/`state`/`nonce`, `scope` contains `openid`, exact `redirect_uri`, and the transaction cookie's `HttpOnly`/`Secure`/`MaxAge≤300`/`Path` — full AC text covered |
| AC-007 | ✅ COVERED | `Login.test.tsx:[AC-007]` (2 tests) | Asserts `tagName==='A'`, exact `href`, and that a click issues no `fetch`/XHR |
| AC-008 | ✅ COVERED | `oidc/service_test.go:TestAC008_CompleteLogin_Success`; `oidc_handler_test.go:TestAC008_SuccessfulLogin_MintsStandardSessionCookie` | Verified the handler test parses the JWT with the shared secret, asserts `sub:"admin"`, and feeds the cookie through `AuthCheck` for `{"authenticated":true,"needs_setup":false}` — this is real, not a shape-only check |
| AC-009 | ✅ COVERED | `auth_handler_test.go:TestAC009_Login_SameSiteLax_NoSecureOverHTTP`; assertions inside `TestAC008_SuccessfulLogin_MintsStandardSessionCookie` | The "no manual reload" behavior itself is a browser property that a Go unit test cannot observe directly; the ADD's own §8 mapping for AC-009 marks the full round-trip as a LIVE step. The unit tests correctly verify the mechanism (`SameSite=Lax`, scheme-derived `Secure`) that FR-036 depends on. **LIVE verification against real authentik still required before production sign-off** (see Deviations/Notes) |
| AC-010 | ✅ COVERED | `oidc_handler_test.go:TestAC010_FirstLogin_WritesAuditRow` | Asserts record count, email, and that `first_login_at == last_login_at == injected now` via an injected clock — no time bombs |
| AC-011 | ✅ COVERED | `oidc_handler_test.go:TestAC011_RepeatLogin_UpdatesNotDuplicates` | Two logins at injected `T0`/`T1`; asserts exactly one record, `first_login_at` stays `T0`, `last_login_at` becomes `T1` |
| AC-012 | ✅ COVERED | `oidc_handler_test.go:TestAC012_UnseenIdentity_BecomesAdmin` | Drives a real `middleware.AdminAuth`-guarded stub handler with the minted cookie — genuinely proves no allowlist gate exists, not just that a cookie was set |
| AC-013 | ✅ COVERED | `oidc_handler_test.go:TestAC013_StateMismatch_Rejected` | — |
| AC-014 | ✅ COVERED | `oidc/service_test.go:TestAC014_CompleteLogin_NonceMismatch`; `oidc_handler_test.go:TestAC014_NonceMismatch_Rejected` | — |
| AC-015 | ✅ COVERED | `oidc/transaction_test.go:TestAC015_DecodeTransaction_Absent/ExpiredJustOverBoundary/ExactBoundaryAccepted/OneSecondBeforeBoundary`; `oidc_handler_test.go:TestAC015_MissingTransaction_Rejected` | Boundary is tested at exactly 300s (accepted), 299s (accepted), and 301s (rejected) — a real boundary test, not just "some time later" |
| AC-016 | ✅ COVERED | `oidc/transaction_test.go:TestAC016_DecodeTransaction_ClearedValueNeverDecodes`; `oidc_handler_test.go:TestAC016_TransactionCookieCleared_ReplayFails` | The handler test drives a real `/start` → `/callback` → replay `/callback` sequence and asserts the replay gets `sso_expired` with no session — this is a genuine replay test, not a shape check |
| AC-017 | ✅ COVERED (gap closed by QA) | `oidc/service_test.go:TestAC017_BeginLogin_IdPDown_UnavailableAndNotCached`, `TestAC017_BeginLogin_RepeatedFailureDoesNotPanicOrCache`; `oidc_handler_test.go:TestAC017_StartDegradesWhenIdPDown`; **`Login.test.tsx:[AC-017]` (added by QA)** | The ADD's own §8 AC-017 row names `Login.test.tsx` for "message + working password form", but no such test existed — every existing `sso_error` test in `Login.test.tsx` used `sso_denied` or an unrecognised code. A broken/renamed `sso_unavailable` entry in the frontend's error-message lookup table would have passed every existing test. QA added 2 tests exercising `sso_error=sso_unavailable` specifically. See Tests Added by QA |
| AC-018 | ✅ COVERED | `oidc_handler_test.go:TestAC018_IdPAccessDenied_LandsOnLoginScreen`; `Login.test.tsx:[AC-018]` (3 tests) | — |
| AC-019 | ✅ COVERED | `oidc_handler_test.go:TestAC019_IdPDown_PasswordLoginUnaffected`; full `go test ./...` green with SSO configured against a closed stub issuer | `/health`, agent registration, heartbeat, telemetry, and the dashboard pages are not separately unit-tested against an IdP-down fixture, but none of those handlers touch `oidc.Service` at all (confirmed by reading `router.go`'s wiring) — an IdP outage cannot reach them structurally. The ADD's own mapping correctly delegates that breadth to a LIVE step |
| AC-020 | ✅ COVERED | `setup_test.go:TestAC020_SetupGuard_OIDCPathsPassThrough`, `TestAC020_SetupGuard_OtherAPIPathsStillBlocked`; `oidc_handler_test.go:TestAC020_SetupIncomplete_OIDCSuppressed` | — |
| AC-021 | ✅ COVERED | `oidc_handler_test.go:TestAC021_SSOLogin_DoesNotTouchPasswordOrSetupFlag` | Asserts byte-identical password hash and unchanged setup flag after a real SSO login, then re-confirms the original password still logs in |
| AC-022 | ✅ COVERED | `auth_handler_test.go:TestAC022_Logout_ClearsWithMatchingAttributes` | — |
| AC-023 | ✅ COVERED | `oidc_handler_test.go:TestAC023_NoIdPTokensPersisted` | Asserts exactly 2 cookies on the response (`riot_session` + the `riot_oidc_tx` clear), that no cookie value contains the stub's access-token string, and that the persisted record has no token field (compiler-enforced by `Claims`'s shape, per AD-017) |
| AC-024 | ✅ COVERED | `oidc_handler_test.go:TestAC024_ClientSecretNeverLeaks`; QA-independent frontend/bundle grep (see below) | QA independently ran `grep -rl "client_secret\|CLIENT_SECRET" web/dist/` against a freshly built bundle — zero matches |
| AC-025 | ✅ COVERED | `Login.test.tsx:[AC-025]` (2 tests) | — |
| AC-026 | ✅ COVERED | `oidc/identity_test.go:TestAC026_SafeReturnPath` (9 table cases incl. backslash bypass, control chars, CR/LF injection); `oidc_handler_test.go:TestAC026_OpenRedirectRefused` | — |
| AC-027 | ✅ COVERED | `ratelimit_test.go:TestAC027_MiddlewareRedirect_ThrottlesToLocation`, `TestAC027_MiddlewareRedirect_DifferentIPUnaffected`; `TestSEC002_Middleware_SpoofedXFF_DoesNotOpenFreshBucket` | The spoofed-XFF test chains the real `RealIP` middleware in front of `Middleware()` with no trusted proxies configured — the production default — so it exercises the actual header path, not just `ClientIP` in isolation. This is exactly the test the security review's SEC-002 condition demanded ("test with a spoofed X-Forwarded-For, not just a manipulated RemoteAddr") |
| AC-028 | ✅ COVERED | `oidc_handler_test.go:TestAC028_EveryAttemptIsAuditable` | Drives one success, one state-mismatch, and one IdP-down attempt through a captured `slog` buffer and asserts each has exactly one structured entry with the right fields, and that the client secret/access-token/email never appear in any log line |
| AC-029 | ✅ VERIFIED (LIVE/manual, as the ADD itself specifies) | Independently re-verified by QA against a disposable PostgreSQL 16 container (see Test Run Summary) — not unit-testable per the ADD's own note ("no DB test harness in this repo") | — |
| AC-030 | ✅ COVERED | `config_test.go:TestAC030_LoadConfig_NoOIDCVars_AllFieldsEmpty`; full suite green with no `RIOT_OIDC_*`/`RIOT_TRUSTED_PROXIES` set; independently confirmed via the live boot in the AC-029 verification (no OIDC warnings logged, dormant) | — |

**Security-review conditions (ADD §8.1):** all verified. SEC-001 (`Secure` scheme-derived, both mint sites) — `auth_handler_test.go`, `oidc_handler_test.go`. SEC-002/C5 (unforgeable client IP, all-trusted-chain peer fallback) — `clientip_test.go` (7 tests) + `ratelimit_test.go` — read in full, see Test Quality Findings. SEC-003 (loud first admission) — `oidc_handler_test.go:TestSEC003_FirstAdmission_EmitsWarnRepeatDoesNot`. SEC-004 (MAC domain separation) — `transaction_test.go`, read in full, see below. SEC-005 — document review, correct. SEC-006 — LIVE-only, correctly deferred (the stub IdP has no single-use code semantics to test against; delegated to the real IdP by design). SEC-007 — independently re-run, confirmed. SEC-008/C1-C3 — `D:\Repos\Hawaii\scripts\register_oidc_app.py` changes are outside this repo and outside this story's engineering scope (ADD explicitly scopes C1-C3 to the technical writer/architect); not re-verified here as they are not OIDC-001 deliverables. SEC-011 — `setup_test.go:TestSEC011_SetupGuard_ExactMatchNotPrefix`. SEC-012 — code review of `handlers/oidc.go` confirms `label` is sourced only from `h.oidc.ButtonLabel()`. **C4 — text is present but ordering does not match the condition's explicit requirement; see Deviations from ADD.**

---

## Test Quality Findings

Overall the test suite is unusually strong for this kind of story — it is testing observable behavior (redirects, cookies, response bodies, log entries, DB state) rather than implementation internals, and several tests go beyond what the ADD strictly required (e.g. `identity_test.go`'s CR/LF-injection and backslash-bypass cases, `service_test.go`'s dedicated signature/audience/expiry rejection tests for FR-021).

**Transaction MAC ordering (SEC-004) — read in full, confirmed correct.** `DecodeTransaction` in `internal/server/oidc/transaction.go` splits on the *last* `.`, computes and compares the MAC (`hmac.Equal`) **before** any base64/JSON decode, exactly as AD-005 mandates. `TestSEC004_RiotSessionJWTAsTransaction_FailsAtMACCheck` mints a real HS256 `riot_session`-shaped JWT with the *same underlying secret material*, submits it as `riot_oidc_tx`, and asserts `errors.Is(err, ErrTransactionMAC)` — this is the exact cross-protocol-confusion attack the security review's SEC-004 finding described, and the test proves the fix at the correct step, not just that *some* error occurred. `DeriveTransactionKey` is confirmed deterministic, secret-dependent, and not equal to the raw JWT secret.

**C5 all-trusted-XFF-chain peer fallback — read in full, confirmed correct and correctly tested.** `middleware.rightmostUntrusted` returns `""` when every entry in the chain is trusted; `RealIP` treats an empty return as "leave `r.RemoteAddr` untouched" — i.e. the client resolves to the immediate peer, never an unspecified value and never a promoted trusted hop. `TestC5_AllTrustedXFFChain_FallsBackToPeer` constructs exactly this scenario (two trusted CIDRs, an XFF chain where both entries fall inside them) and asserts the resolved client is the TCP peer, not either chain entry. This matches AD-020 step 2's C5 amendment and §12 note 14 verbatim.

**SEC-002 forgery test is a real integration, not a unit-in-isolation test.** `TestSEC002_Middleware_SpoofedXFF_DoesNotOpenFreshBucket` chains `RealIP(tp)` (with `tp` = trust-nobody, the production default) in front of `RateLimiter.Middleware()` — the same arrangement `router.go` wires — and proves a spoofed `X-Forwarded-For` on a second request from the same TCP peer does not open a fresh bucket. This is the test the security review explicitly asked QA to insist on ("test with a spoofed X-Forwarded-For, not just a manipulated RemoteAddr") and it is present and correct.

**AC-023 (no IdP tokens persisted) is a genuine negative test, not a false-coverage placeholder.** It counts the exact cookie set (`== 2`, not `>= 2`), string-searches every cookie value for the stub's known access-token literal, and relies on `Claims`'s struct shape (no token field) being a compile-time guarantee — consistent with AD-017's stated design.

**Minor gap found and closed by QA: AC-017's frontend message coverage.** The ADD's own §8 AC-017 row names `Login.test.tsx` as covering "message + working password form" for the `sso_unavailable` case, but as delivered, `Login.test.tsx` only exercised `sso_denied` and an unrecognised code — never `sso_unavailable` itself. Because the error-message table in `Login.tsx` is a flat `Record<string,string>` lookup, a typo or accidental removal of the `sso_unavailable` key specifically would have passed every pre-existing test (the "if I broke the code so this AC fails, would the tests catch it?" question answers "no" for that one specific code). QA added two tests to close this — see Tests Added by QA. Severity is low (the underlying mapping is simple and shared code, not per-code logic), but the gap was real by the story's own mechanical-coverage standard.

**Test isolation.** No shared mutable state observed across Go test files; `t.Setenv` is used correctly for config tests (auto-restored). `captureLogs` swaps `slog.Default()` and restores it via `t.Cleanup`, so log-capturing tests do not leak into others even though none of them call `t.Parallel()`. Frontend tests reset `localStorage` and mock state in `beforeEach`. No flakiness observed across repeated runs.

**Behavior vs. implementation.** Tests assert on cookies, HTTP status/Location headers, JSON response bodies, DB-mock state, and structured log fields — all observable behavior. No test reaches into unexported implementation details beyond what's necessary to drive a scenario (e.g., constructing a `*Handlers` via struct literal is the established pattern in this codebase, not new brittleness introduced by this story).

---

## Adversarial Findings

- **Dormant-mode 404 vs. SPA catch-all 200 trap:** verified structurally, not just by test. The three OIDC routes are registered directly on the chi router inside `r.Route("/api/v1/auth", ...)` (AD-011), so they are matched by chi *before* the bottom-of-file `r.Get("/*")` SPA catch-all is ever consulted, regardless of dormancy state. A request to `/api/v1/auth/oidc/start` while dormant gets the handler's explicit `404`, never the SPA's `200 index.html`. No test or code path found that could route these three paths to the catch-all. No finding.
- **Setup-wizard gating (SSO cannot bypass needs-setup):** `SetupGuard`'s three-path allowlist is exact-match (not prefix, per SEC-011's fix), and even if a request reaches the handler, `OIDCStart`/`OIDCCallback` independently re-check `isSetupComplete()` and 404 regardless. Two independent gates, both nil-safe-false on mis-wiring (AD-011/AD-012). Tested at both layers (`setup_test.go` for the middleware, `oidc_handler_test.go` for the handler-level gate). No finding.
- **Malformed/expired transaction cookies:** exercised at every layer — absent, no `.` separator, tampered MAC byte, wrong key, valid-MAC-but-non-base64 payload, valid-payload-but-missing-field, and the exact 300s boundary (299s/300s/301s). No finding.
- **Callback with `error=` from the IdP:** `access_denied` → `sso_denied`; any other value → `sso_failed`; both tested, both clear the transaction cookie and refuse a session, and (AC-018) the password form is proven to still work afterward. No finding.
- **Demo build alias:** independently rebuilt with `VITE_DEMO=true` — succeeds, and `demo-client.ts`'s `getSSOAvailability` stub returns `{available:false,label:''}`, consistent with the demo build never showing an SSO button. No finding.
- **Open redirect / header injection via `returnUrl`:** `SafeReturnPath` rejects `//evil.example`, `https://evil.example`, `/\evil.example` (backslash bypass), NUL and other control characters, and CR/LF (header-injection) sequences — exceeding the reference implementation and directly tested. No finding.
- **Login-CSRF / transaction cookie replanting (SEC-005 residual):** confirmed the ADD's own documented residual is accurate and unchanged by the implementation — a genuinely server-signed transaction is obtainable by any client via `/start`; the design does not close this class, it relies on rIOt's single fixed `sub:"admin"` identity making "someone else's session" and "your own session" equivalent. No new finding beyond what SEC-005 already records; verified the implementation matches that documented (accepted) risk rather than silently drifting from it.
- **Documentation-only C4 ordering defect (new finding, not previously flagged):** see Deviations from ADD below.

---

## Tests Added by QA

| File | Lines | Covers |
|------|-------|--------|
| `web/src/pages/Login.test.tsx` | New `describe('[AC-017] IdP down: start degrades to the login screen', ...)` block, 2 `it(...)` cases, inserted between the existing `[AC-007]` and `[AC-018]` blocks | AC-017 — asserts the `sso_unavailable` message ("The identity provider could not be reached.") renders from `?sso_error=sso_unavailable`, and that the password form still accepts and submits a valid password afterward. Both pass green; full suite reproduced at 359/359 after the addition (357 baseline + 2 new) |

Both tests are tagged `Added by QA Engineer` per instructions and follow the existing file's `describe('[AC-0NN] ...')` / `it(...)` naming convention exactly.

---

## Deviations from ADD

1. **C4's required ordering is not honored in `.env.example`.** Condition C4 and ADD §8.1's row for it are explicit: the `Secure`-attribute consequence of leaving `RIOT_TRUSTED_PROXIES` unset behind a TLS-terminating proxy must be "stated ahead of the rate-limit and audit-IP consequences." The shipped `.env.example` text reads: "...otherwise per-client rate limiting and audit IPs will all collapse onto the proxy's own address, **and** the riot_session cookie will never receive the Secure attribute even over https" — the rate-limit/audit-IP consequence is stated *first*, and the `Secure` consequence is appended as an afterthought, the reverse of what C4 requires. The underlying control itself (scheme-derived `Secure`, gated by `RIOT_TRUSTED_PROXIES`) is implemented and tested correctly (see AC coverage above) — this is a documentation wording/ordering defect only, not a functional or security-control defect. Low severity; should be corrected (reorder the sentence, lead with `Secure`) before or during the technical-writer pass, since the technical writer's runbook work is downstream of this text and would otherwise propagate the same ordering into the README.
2. **Undisclosed minor deviation: `recordExternalIdentity`'s detached context.** AD-015 specifies `ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)`. The implementation (`handlers/oidc.go:recordExternalIdentity`) instead uses `context.WithoutCancel(context.Background())`. Functionally equivalent in this codebase today — no request-scoped context values are consumed anywhere in the `db` package or by `RecordLogin` — but it is a real deviation from the literal ADD text that was not listed in the implementation report's "Deviations from the ADD" section (which lists four other deviations but not this one). Not a blocking finding; flagged so it doesn't silently drift further if this code path is ever touched again.
3. **All other deviations claimed by the implementation report were independently verified and are accurate as described:** the `go-oidc/v3` v3.17.0 pin (confirmed newest release still declaring `go 1.24.0`; confirmed `x/oauth2`/`go-jose` floors are still met and clear of GO-2026-4945); the `oidcClock` field addition (confirmed used only by `recordExternalIdentity`, nil-safe, and is exactly what makes AC-010/AC-011 deterministic); the duplicated `oidc_stub_idp_test.go` (confirmed test-only, necessitated by Go's package-test-file visibility rules, not importable across `oidc`/`handlers` packages); and the C5/C4 items already reflected in code and `.env.example` respectively (modulo the ordering defect above).

---

## Deviations from FRD

None found. Every FR, NFR, BR, and AC read against the implementation was satisfied:

- Dormancy (FR-002 through FR-008), the availability endpoint's local-only, network-free answer (FR-009/FR-011), PKCE + state + nonce (FR-012/FR-013), the transaction cookie's httpOnly/short-lived/single-use-by-clearing design (FR-014, NFR-006), the byte-identical `redirect_uri` derivation (FR-015), the safe return-path validation (FR-016, V-004), the closed browser-facing error vocabulary with no 5xx/JSON/stack trace ever reaching the browser (FR-017, FR-024), the transaction/state/nonce/token rejection paths (FR-018 through FR-021), unconditional transaction-cookie clearing (FR-022), the shared session-minting helper (FR-023), no token persistence (FR-025, BR-005), no allowlist (FR-026, BR-002), the upsert-with-first-seen audit record (FR-027, V-002), best-effort audit writes that never block login (FR-028, OQ-2), setup-wizard gating (FR-029, BR-009), unchanged password/logout/change-password/check contracts modulo the two recorded FRD amendments (FR-031, §0 amendments 1 and 2), SSO-established sessions logging out identically (FR-032), and the full frontend contract (FR-033 through FR-038) were all read against their implementing code and/or their test bodies and hold.
- BR-006 (no user table, no roles) — confirmed; `external_identities` is a pure audit table with no FK into any access-control path.
- BR-007 (`email_verified` recorded, never gated) — confirmed in `ValidateClaims` and by a dedicated test.
- BR-008 (no single-logout) — confirmed; `Logout` makes no IdP call, asserted by `TestAC022_Logout_ClearsWithMatchingAttributes`'s stub-request-count assertion described in the implementation report and consistent with the code (no reference to the OIDC service in `Logout`).
- NFR-007 (TLS verification never disabled) — confirmed by grep: no `InsecureSkipVerify` or `oidc.InsecureIssuerURLContext` anywhere in the OIDC code path (the pre-existing `InsecureSkipVerify` uses in `internal/agent/*` and `internal/server/probes/http.go` are unrelated, pre-existing, and untouched by this story's diff).
- NFR-011 (additive, reversible migration) — independently verified live (see Test Run Summary).

---

## Verdict Rationale

**PASS WITH NOTES.**

- **Zero ❌ MISSING ACs.** All 30 ACs have named tests that genuinely exercise the AC (read in full for the security-critical ones), and the one real gap found (AC-017's frontend message coverage) was closed by QA with a passing test rather than merely reported.
- **All tests green**, reproduced independently: Go 666/666 (2 unrelated pre-existing skips), frontend 359/359 (357 baseline + 2 added by QA), `go vet` clean, `tsc --noEmit` clean, both frontend builds (regular and demo) succeed, `govulncheck` confirmed clean of the three new dependencies.
- **No implementation deviation violates the FRD.** Every FR/NFR/BR/AC checked against the code holds.
- **Minor, non-blocking quality issues exist**, which is what keeps this from a plain PASS: (1) the `.env.example` text for `RIOT_TRUSTED_PROXIES` does not honor security-review condition C4's explicit ordering requirement (content present, sequence reversed) — a documentation fix, not a code fix, but one that should happen before the technical writer's runbook work builds on it; (2) one undisclosed minor deviation from AD-015's literal text (`context.Background()` vs. `context.WithoutCancel(r.Context())`) that is functionally inert today but wasn't listed in the implementation report's deviations section; (3) AC-009 and the broader AC-006–AC-018 range, plus SEC-006, remain genuinely LIVE-only per the ADD's own design (no authentik instance was available in this QA pass either) and still require a real end-to-end run against `https://auth.rbretschneider.com` before this ships to a production deployment that will actually exercise SSO.

None of these three notes describes a vulnerability, a failing test, or an FRD violation — they are documentation-accuracy and disclosure items plus a pre-planned LIVE verification step that no automated QA pass (mine included) can substitute for.

## Action Required (if FAIL)

Not applicable — verdict is PASS WITH NOTES. For completeness, recommended (non-blocking) follow-ups before/alongside the technical-writer pass:

1. `D:\Repos\rIOt\.env.example`, `RIOT_TRUSTED_PROXIES` comment block (currently lines 24-30): reorder so the `Secure`-attribute consequence is stated before the rate-limit/audit-IP consequence, per security-review condition C4.
2. `D:\Repos\rIOt\internal\server\handlers\oidc.go`, `recordExternalIdentity`: either change to `context.WithoutCancel(r.Context())` to match AD-015 literally, or add one line to a future implementation-report addendum noting the `context.Background()` choice as an intentional, disclosed deviation. Either is acceptable; leaving it undisclosed is the only wrong answer.
3. Before enabling SSO against a real deployment: run the LIVE steps the ADD and this report both name — the full authentik round trip for AC-006 through AC-018/AC-009, the SEC-006 replayed-authorization-code check, and the SEC-003 Settings → Logs visibility check.
