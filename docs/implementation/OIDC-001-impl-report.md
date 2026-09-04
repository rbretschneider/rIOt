# Implementation Report

- **Story ID:** OIDC-001
- **Title:** Optional "Sign in with authentik" (OIDC) login for the rIOt admin dashboard
- **Engineer:** Senior Dev Agent
- **Date:** 2026-09-04
- **Stack:** Go 1.24 (chi v5, pgx v5, golang-migrate, slog) + React 19 + TypeScript 5.9 + Vite 6 + TanStack Query 5 + react-router-dom 7 + Vitest

---

## Pre-Implementation Baseline

Both suites were confirmed green before any code was touched:

- `go test ./...`: all packages `ok` (no pre-existing failures).
- `cd web && npm run test:run`: 25 test files, 353 tests passing.

No blockers were raised. The ADD (Revision 2, FINAL) and the security review's Re-review of Revision 2 (verdict **APPROVED WITH CONDITIONS**) were both read in full before writing code, including §0's two recorded FRD amendments and conditions C1–C6.

**Branch note.** The working tree's checked-out branch at task start (`AGENT-PRIV`) was itself several unrelated, unmerged commits ahead of `main` (a previous story's completed-but-unmerged branch). All OIDC-001 work was implemented and is committed on a new `OIDC-001` branch created from `main`, not from `AGENT-PRIV`, so this story's history does not entangle unrelated in-flight work. `AGENT-PRIV` was left exactly as found. Because of this, the baseline/final test counts below are `main`'s numbers (347 frontend tests baseline → 357 with this story's 10 new tests), not the `AGENT-PRIV`-branch numbers seen transiently during development (353 → 363) — both baselines were independently confirmed green before their respective code changes.

---

## Completed Components

### Server — new files

| File | Action | Notes |
|---|---|---|
| `internal/server/oidc/config.go` | CREATE | `Options`, `Service` construction, dormancy rules (FR-002), issuer URL validation (V-005), label default + 64-rune truncation (FR-003/OQ-8), `Enabled()`, `ButtonLabel()`, `TransactionKey()` |
| `internal/server/oidc/service.go` | CREATE | Lazy cached discovery (AD-004), `BeginLogin`, `CompleteLogin`, 10s HTTP client (NFR-003) |
| `internal/server/oidc/transaction.go` | CREATE | `Transaction`, `DeriveTransactionKey`, `Encode`/`DecodeTransaction` with MAC-before-decode ordering, exported sentinels |
| `internal/server/oidc/identity.go` | CREATE | `Claims`, `ValidateClaims` (V-001), `SafeReturnPath` (V-004) — pure, no I/O |
| `internal/server/oidc/errors.go` | CREATE | `LoginError`, the four `Code*` constants, the fixed `Reason*` constants |
| `internal/server/oidc/config_test.go` | CREATE | AC-003, AC-004, V-005, OQ-8 |
| `internal/server/oidc/identity_test.go` | CREATE | AC-026, V-001 table tests |
| `internal/server/oidc/transaction_test.go` | CREATE | AC-015, AC-016, SEC-004, key-derivation determinism, expiry boundary |
| `internal/server/oidc/service_test.go` | CREATE | AC-006, AC-008, AC-014, AC-017, FR-021 signature/audience/expiry, V-001 |
| `internal/server/oidc/testidp_test.go` | CREATE | Test-only stub issuer (`httptest.Server`, in-test RSA key, RS256 minting) |
| `internal/server/handlers/oidc.go` | CREATE | `OIDCAvailability`, `OIDCStart`, `OIDCCallback`, `requestOrigin`, `ssoErrorRedirect`, audit logging, AD-021 WARN |
| `internal/server/handlers/oidc_handler_test.go` | CREATE | AC-001, AC-002, AC-005, AC-006, AC-008–AC-018, AC-020, AC-021, AC-023, AC-024, AC-026, AC-028, SEC-003 |
| `internal/server/handlers/oidc_stub_idp_test.go` | CREATE | Test-only stub issuer duplicated into the `handlers` package (see Deviations) |
| `internal/server/middleware/clientip.go` | CREATE | AD-020: `TrustedProxies`, `ParseTrustedProxies`, `RealIP`, `ClientIP`, `RequestScheme` |
| `internal/server/middleware/clientip_test.go` | CREATE | SEC-002 spoofing/trust cases, rightmost-untrusted walk, malformed CIDR, C5 all-trusted fallback |
| `internal/server/db/external_identity_repo.go` | CREATE | `ExternalIdentityRepo`, `RecordLogin` via `RETURNING (xmax = 0)` |
| `internal/models/identity.go` | CREATE | `models.ExternalIdentity` |
| `internal/server/middleware/setup_test.go` | CREATE | AC-020 guard passthrough, SEC-011 exact-path matching |
| `cmd/riot-server/migrations/000022_external_identities.up.sql` | CREATE | §5 DDL |
| `cmd/riot-server/migrations/000022_external_identities.down.sql` | CREATE | `DROP TABLE IF EXISTS external_identities;` |

### Server — modified files

| File | Action | Notes |
|---|---|---|
| `internal/server/config.go` | MODIFY | Added `OIDCIssuerURL`, `OIDCClientID`, `OIDCClientSecret`, `OIDCButtonLabel`, `TrustedProxies`; read + trimmed in `LoadConfig` |
| `internal/server/config_test.go` | MODIFY | AC-003, AC-030, whitespace trimming, `RIOT_TRUSTED_PROXIES` absence |
| `internal/server/server.go` | MODIFY | Added `ExternalIdentityRepo *db.ExternalIdentityRepo`, constructed in `Start()` |
| `internal/server/router.go` | MODIFY | Replaced `chimw.RealIP` with `middleware.RealIP(trustedProxies)`; constructed `oidcSvc`; wired `OIDC`/`ExternalIdentityRepo`/`SetupComplete` into `HandlerDeps`; added `oidcLimiter`; registered the three OIDC routes |
| `internal/server/handlers/handlers.go` | MODIFY | `HandlerDeps`/`Handlers` gained `OIDC`, `ExternalIdentityRepo`, `SetupComplete`, `oidcClock` (see Deviations); added `isSetupComplete()` |
| `internal/server/handlers/auth.go` | MODIFY | Extracted `issueSessionCookie`/`clearSessionCookie`; `Login`/`Logout` call them; `SameSite` → `Lax`; `Secure` derived from `middleware.RequestScheme` |
| `internal/server/handlers/auth_handler_test.go` | MODIFY | `SameSite`/`Secure` assertions at both mint sites (AC-008, AC-009, AC-022, SEC-001) |
| `internal/server/middleware/setup.go` | MODIFY | Exact-path allowlist for the three OIDC endpoints (AD-019, SEC-011) |
| `internal/server/middleware/ratelimit.go` | MODIFY | Added `MiddlewareRedirect`; `Middleware`/`MiddlewareRedirect` key on `ClientIP(r)` |
| `internal/server/middleware/ratelimit_test.go` | MODIFY | AC-027, SEC-002 spoofed-XFF-does-not-open-fresh-bucket |
| `internal/server/db/interfaces.go` | MODIFY | Added `ExternalIdentityRepository` interface + conformance assertion |
| `internal/testutil/mocks.go` | MODIFY | Added `MockExternalIdentityRepo` reproducing the upsert semantics |
| `go.mod` / `go.sum` | MODIFY | `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2`, `github.com/go-jose/go-jose/v4` (see Deviations for exact versions and rationale) |

### Frontend

| File | Action | Notes |
|---|---|---|
| `web/src/api/client.ts` | MODIFY | Added `SSOAvailability` interface and `getSSOAvailability` |
| `web/src/api/demo-client.ts` | MODIFY | Added matching `getSSOAvailability` stub (demo-alias hazard) |
| `web/src/pages/Login.tsx` | MODIFY | Availability `useQuery`, anchor button, `sso_error` read/strip via `useSearchParams` |
| `web/src/pages/Login.test.tsx` | CREATE | AC-001, AC-005, AC-007, AC-018, AC-025 |

### Configuration / infrastructure

| File | Action | Notes |
|---|---|---|
| `.env.example` | MODIFY | Commented `RIOT_OIDC_*` block + `RIOT_TRUSTED_PROXIES`, including the Secure-flag consequence (condition C4) |
| `docker-compose.yml` | MODIFY | Same five vars as commented lines |
| `docker-compose.prod.yml` | MODIFY | Same five vars as commented `${RIOT_*}` passthroughs |

---

## Deviations from the ADD

None of these weaken any AC, NFR, or security control. All are recorded here per the blocker/deviation protocol.

1. **`go-oidc/v3` pinned to `v3.17.0`, not the newest release (`v3.21.0`).** `go-oidc/v3` v3.18.0 and later declare `go 1.25.0` in their own `go.mod`, which forces this repo's `go` directive from `1.24.0` to `1.25.0` — a project-wide toolchain-version change with no ADD authorization, and a mismatch with `.github/workflows/ci.yml`'s pinned `go-version: "1.24"` and `CLAUDE.md`'s stated Go 1.24 conventions. `v3.17.0` is the newest release that still declares `go 1.24.0`. AD-002's stated floors are **security floors**, not "must be the newest tag": `golang.org/x/oauth2` resolves to `v0.30.0` (≥ the v0.27.0 floor) and `github.com/go-jose/go-jose/v4` is pinned to `v4.1.4` (≥ the v4.0.5 floor, and additionally clear of the newly-published GO-2026-4945 go-jose panic advisory, which affects v4.1.3 — see the govulncheck section below). No functionality is missing at v3.17.0; every API surface AD-006 specifies (`Provider`, `RemoteKeySet`-backed `Verifier`, `Nonce`, discovery) is present and used unchanged.
2. **`internal/server/handlers/handlers.go` gained one additional unexported field, `oidcClock func() time.Time`**, beyond the three (`OIDC`, `ExternalIdentityRepo`, `SetupComplete`) the ADD's §5.4 table lists. AD-016's rationale explicitly states "the injected clock... lets AC-011 assert T0 preserved / T1 updated deterministically," but the ADD does not name the injection point. Per the repo's testing standard ("no time bombs: inject clocks, do not use `Date.now()` directly in testable code"), `recordExternalIdentity` (in `handlers/oidc.go`, a file already in the ADD's file list) reads `h.oidcClock` when set and falls back to `time.Now` otherwise — nil-safe, so no other test or production code path is affected. This is what makes `TestAC010_FirstLogin_WritesAuditRow` and `TestAC011_RepeatLogin_UpdatesNotDuplicates` deterministic rather than relying on real-clock timing.
3. **`internal/server/handlers/oidc_stub_idp_test.go` was created**, not named in ADD §4.1. The ADD names `internal/server/oidc/testidp_test.go` as the stub issuer for the `oidc` package's own tests, and separately requires `handlers/oidc_handler_test.go` to exercise the full `/start` → `/callback` round trip against a stub issuer (AC-008, AC-010–AC-018, AC-023, AC-024, AC-026, AC-028, SEC-003). Go test files in one package are not importable from another package's tests, so a second, `handlers`-package-local stub server (mirroring the `oidc` package's design exactly: in-test RSA key, discovery/token/JWKS endpoints, controllable per-code ID-token claims) was required to satisfy that mapping. It is test-only, adds no production dependency, and is deleted along with the rest of the test binary — recorded here as a scope note rather than a functional deviation.
4. **Security-review condition C5** (all-XFF-entries-trusted must fall back to the TCP peer, not an unspecified value) was implemented exactly as directed in `middleware.rightmostUntrusted` and has a named test, `TestC5_AllTrustedXFFChain_FallsBackToPeer`, in `internal/server/middleware/clientip_test.go`.
5. **Security-review condition C4** (document the `Secure`-flag consequence of leaving `RIOT_TRUSTED_PROXIES` unset behind a TLS-terminating proxy) is reflected in `.env.example`'s `RIOT_TRUSTED_PROXIES` comment block.
6. **Conditions C1, C2, C3, C6** are documentation/Hawaii-repo items the task explicitly scoped to the technical writer / architect, not to this implementation pass, and were not touched here.
7. **Corrected post-QA:** `recordExternalIdentity`'s audit-write context originally used `context.WithoutCancel(context.Background())` instead of AD-015's literal `context.WithoutCancel(r.Context())`. Functionally near-identical (both survive the request's cancellation and carry the same 5s timeout), but the request-derived form additionally carries the request's values (trace/log context), and using `context.Background()` instead was an undisclosed deviation from the ADD's exact wording. Fixed to take the request context; no test assertions changed as a result (no test depended on the specific parent context).

No other deviations. Every file in ADD §4 was created or modified exactly as specified; no file outside §4 was touched. `/debug/pprof`, security headers, `handlers/setup.go`'s `X-Real-Ip` reads, `middleware/admin_auth.go`, `middleware/cors.go`, `middleware/wsorigin.go`, `middleware/logger.go`, `db/admin_repo.go`, any migration ≤ `000021`, `web/src/App.tsx`, `web/src/hooks/useAuth.ts`, and `web/src/types/models.ts` are all untouched, matching ADD §4.5's explicit "not changed by this story" list.

---

## Test Summary

### AC / Security-condition coverage

| AC / Finding | Test(s) | Status |
|---|---|---|
| AC-001 | `oidc_handler_test.go:TestAC001_OIDCAvailability_DormantByDefault`; `Login.test.tsx:[AC-001]` | PASS |
| AC-002 | `oidc_handler_test.go:TestAC002_OIDCStartAndCallback_404WhenDormant` | PASS |
| AC-003 | `config_test.go:TestAC003_LoadConfig_PartialOIDCConfig_FieldsPopulatedIndependently`; `oidc/config_test.go:TestAC003_New_PartialConfig_Dormant`; `oidc_handler_test.go:TestAC003_PartialConfig_DormantAndStartNotFound` | PASS |
| AC-004 | `oidc/config_test.go:TestAC004_New_AllConfigured_DefaultLabel`; `oidc_handler_test.go:TestAC004_OIDCAvailability_ConfiguredDefaultLabel` | PASS |
| AC-005 | `oidc_handler_test.go:TestAC005_OIDCAvailability_CustomLabel`; `Login.test.tsx:[AC-005]` | PASS |
| AC-006 | `oidc/service_test.go:TestAC006_BeginLogin_AuthURLShapeAndPKCE`; `oidc_handler_test.go:TestAC006_OIDCStart_RedirectsWithPKCEStateNonce` | PASS |
| AC-007 | `Login.test.tsx:[AC-007]` | PASS |
| AC-008 | `oidc/service_test.go:TestAC008_CompleteLogin_Success`; `oidc_handler_test.go:TestAC008_SuccessfulLogin_MintsStandardSessionCookie` | PASS |
| AC-009 | `auth_handler_test.go:TestAC009_Login_SameSiteLax_NoSecureOverHTTP`; assertions inside `TestAC008_SuccessfulLogin_MintsStandardSessionCookie` | PASS |
| AC-010 | `oidc_handler_test.go:TestAC010_FirstLogin_WritesAuditRow` | PASS |
| AC-011 | `oidc_handler_test.go:TestAC011_RepeatLogin_UpdatesNotDuplicates` | PASS |
| AC-012 | `oidc_handler_test.go:TestAC012_UnseenIdentity_BecomesAdmin` | PASS |
| AC-013 | `oidc_handler_test.go:TestAC013_StateMismatch_Rejected` | PASS |
| AC-014 | `oidc/service_test.go:TestAC014_CompleteLogin_NonceMismatch`; `oidc_handler_test.go:TestAC014_NonceMismatch_Rejected` | PASS |
| AC-015 | `oidc/transaction_test.go:TestAC015_*` (4 tests); `oidc_handler_test.go:TestAC015_MissingTransaction_Rejected` | PASS |
| AC-016 | `oidc/transaction_test.go:TestAC016_DecodeTransaction_ClearedValueNeverDecodes`; `oidc_handler_test.go:TestAC016_TransactionCookieCleared_ReplayFails` | PASS |
| AC-017 | `oidc/service_test.go:TestAC017_*` (2 tests); `oidc_handler_test.go:TestAC017_StartDegradesWhenIdPDown` | PASS |
| AC-018 | `oidc_handler_test.go:TestAC018_IdPAccessDenied_LandsOnLoginScreen`; `Login.test.tsx:[AC-018]` | PASS |
| AC-019 | `oidc_handler_test.go:TestAC019_IdPDown_PasswordLoginUnaffected`; full `go test ./...` green with SSO configured against a closed stub issuer | PASS |
| AC-020 | `setup_test.go:TestAC020_*` (2 tests); `oidc_handler_test.go:TestAC020_SetupIncomplete_OIDCSuppressed` | PASS |
| AC-021 | `oidc_handler_test.go:TestAC021_SSOLogin_DoesNotTouchPasswordOrSetupFlag` | PASS |
| AC-022 | `auth_handler_test.go:TestAC022_Logout_ClearsWithMatchingAttributes` | PASS |
| AC-023 | `oidc_handler_test.go:TestAC023_NoIdPTokensPersisted` | PASS |
| AC-024 | `oidc_handler_test.go:TestAC024_ClientSecretNeverLeaks`; frontend/bundle grep (see below) | PASS |
| AC-025 | `Login.test.tsx:[AC-025]` | PASS |
| AC-026 | `oidc/identity_test.go:TestAC026_SafeReturnPath`; `oidc_handler_test.go:TestAC026_OpenRedirectRefused` | PASS |
| AC-027 | `ratelimit_test.go:TestAC027_*` (2 tests) | PASS |
| AC-028 | `oidc_handler_test.go:TestAC028_EveryAttemptIsAuditable` | PASS |
| AC-029 | Not unit-testable (no DB harness in this repo, as the ADD records) — **verified manually against a real PostgreSQL 16 container** (see "Manual migration verification" below); LIVE/QA still owns the formal sign-off | VERIFIED (manual) |
| AC-030 | `config_test.go:TestAC030_LoadConfig_NoOIDCVars_AllFieldsEmpty`; live boot smoke test (see below); full suite green | PASS |
| SEC-001 | `auth_handler_test.go:TestSEC001_Login_SecureOverHTTPS` | PASS |
| SEC-002 | `clientip_test.go` (7 tests); `ratelimit_test.go:TestSEC002_Middleware_SpoofedXFF_DoesNotOpenFreshBucket` | PASS |
| SEC-003 | `oidc_handler_test.go:TestSEC003_FirstAdmission_EmitsWarnRepeatDoesNot` | PASS |
| SEC-004 | `oidc/transaction_test.go:TestSEC004_*` (2 tests) | PASS |
| SEC-005 | Document review only (ADD text) — no test required | N/A |
| SEC-006 | LIVE-only (replay a consumed authorization code against a real IdP) — not reproducible against the stub issuer, which does not implement single-use codes; handed to QA per ADD §8.1 | LIVE (QA) |
| SEC-007 | `go list -m all` + `govulncheck ./...` (see below) | PASS |
| SEC-008 | Document review (§4.5) — technical writer/QA scope | N/A |
| SEC-009 / SEC-010 | Explicitly not touched, per ADD §12 note 18 | N/A (deferred by design) |
| SEC-011 | `setup_test.go:TestSEC011_SetupGuard_ExactMatchNotPrefix` | PASS |
| SEC-012 | Code review of `handlers/oidc.go`: `label` is sourced only from `h.oidc.ButtonLabel()`, never from `r.URL.Query()` or any request field | PASS (review) |
| C4 | `.env.example` `RIOT_TRUSTED_PROXIES` comment | Documented |
| C5 | `clientip_test.go:TestC5_AllTrustedXFFChain_FallsBackToPeer` | PASS |

### Go test run — full suite

```
go build ./...           → clean
go vet ./...              → clean (this repo's CI "lint" job)
go test ./...
ok  github.com/DesyncTheThird/rIOt/internal/agent               1.410s
ok  github.com/DesyncTheThird/rIOt/internal/agent/collectors    1.593s
ok  github.com/DesyncTheThird/rIOt/internal/models              0.392s
ok  github.com/DesyncTheThird/rIOt/internal/resilient           (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server              (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/auth         (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/ca           (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/events       0.632s
ok  github.com/DesyncTheThird/rIOt/internal/server/handlers     2.475s
ok  github.com/DesyncTheThird/rIOt/internal/server/middleware   (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/notify       (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/oidc         (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/probes       (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/scoring      0.522s
ok  github.com/DesyncTheThird/rIOt/internal/server/summary      0.631s
ok  github.com/DesyncTheThird/rIOt/internal/server/updates      (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/websocket    (cached)
```
666 individual `--- PASS` results, 0 `--- FAIL` (verbose run, `OIDC-001` branch off `main`). No `-race` on this Windows dev box per `CLAUDE.md`; CI runs `go test -race -count=1 ./...` on Linux against the same code. (`internal/privileges` does not exist on `main`/`OIDC-001` — it belongs to the unrelated, unmerged `AGENT-PRIV` branch and is correctly absent here; see the Branch note above.)

### Frontend test run — full suite

```
cd web && npm run test:run
 Test Files  26 passed (26)
      Tests  357 passed (357)
```
(347 pre-existing on `main` + 10 new in `Login.test.tsx`.) The `ECONNREFUSED :3000` stack traces interleaved in the output are pre-existing, expected console noise from an unrelated mocked-network test elsewhere in the suite (present in the baseline run too), not failures.

### Typecheck / build

```
cd web && npx tsc --noEmit         → clean
cd web && npm run build            → succeeds (dist/index-*.js, 396 KB gzip)
cd web && VITE_DEMO=true vite build → succeeds (dist/index-*.js, 405 KB gzip)
```
`npm run build:demo` itself fails on this Windows/Git-Bash shell because the script's inline `VITE_DEMO=true vite build` Unix env-var syntax is not understood by `cmd.exe`'s script runner — this is a pre-existing shell-portability issue in `package.json`, not something introduced by this story (confirmed by running the equivalent `VITE_DEMO=true npx vite build` directly, which succeeds). CI runs `npx tsc --noEmit` + `npm run test:run` on Ubuntu, where the script works natively.

### `go vet` / linting

`go vet ./...` is this repo's linter of record (see `.github/workflows/ci.yml`'s `lint` job) — clean, no new findings. No `golangci-lint` config exists in the repo.

### govulncheck

```
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```
Result: 23 vulnerabilities reported as "affected" (i.e. actually reachable, not just imported), spanning `golang.org/x/net`, `golang.org/x/text`, `github.com/jackc/pgx/v5`, `github.com/docker/docker`, and the Go standard library as shipped by the locally-installed `go1.26.0` toolchain. **None involve `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2`, or `github.com/go-jose/go-jose/v4`** — the three dependencies this story adds. All 23 are pre-existing (the modules were already direct/indirect dependencies before this story; the stdlib findings are an artifact of this workstation's `go1.26.0` toolchain vs. the CI-pinned `go1.24`) and are out of scope for OIDC-001 per the engineering standards ("Do not upgrade existing dependencies as part of a feature story"). One newly-published advisory, **GO-2026-4945** (a `go-jose` panic in JWE decryption, affecting `go-jose/go-jose/v4` v4.1.3), was caught during this check and fixed by bumping the explicit `go-jose` pin from v4.1.3 to **v4.1.4** — re-running govulncheck confirmed the fix. `go list -m all` confirms `golang.org/x/oauth2 v0.30.0` (≥ v0.27.0 floor) and `github.com/go-jose/go-jose/v4 v4.1.4` (≥ v4.0.5 floor).

### Manual migration verification (AC-029)

The repo has no automated DB migration test harness (confirmed, matching the ADD's note). Verified by hand against a disposable PostgreSQL 16 container:

1. Started `postgres:16-alpine` in Docker, pointed `RIOT_DB_URL` at it, booted the real `riot-server` binary with `RIOT_ADMIN_PASSWORD` set and no `RIOT_OIDC_*`/`RIOT_TRUSTED_PROXIES` vars — server started cleanly, ran migrations 000001–000022 in one pass (`schema_migrations.version = 22, dirty = false`), no OIDC-related warnings logged (dormant, as expected).
2. `\d external_identities` matched the ADD §5.1 schema exactly (columns, types, nullability, the `UNIQUE (issuer, subject)` constraint, no extra index).
3. Applied `000022_external_identities.down.sql` directly: table count in `information_schema.tables` went from 26 → 25 (exactly one table removed), `external_identities` confirmed gone, `admin_config` (a pre-existing table) confirmed unchanged (`admin_password_hash`, `setup_complete` keys intact).
4. Container torn down after verification.

This exercises the real migration SQL against real PostgreSQL; QA's LIVE step (a full `make migrate-up`/`make migrate-down` cycle against a database populated by the previous release, per the ADD's exact wording) still stands as the formal sign-off gate. Note: the `Makefile`'s `migrate-up`/`migrate-down` targets reference a `-migrate-up`/`-migrate-down` CLI flag and a `migrate` build tag that do not exist anywhere in `cmd/riot-server/*.go` — this is a pre-existing gap in the Makefile unrelated to this story (the server always applies pending migrations automatically on boot via `Server.Start()` → `DB.RunMigrations`, which is the path actually exercised above and in production). Flagged here for QA/operator awareness, not fixed inline (out of scope).

---

## Notes for QA

- **Live authentik run.** All ACs above are satisfied against the in-repo stub issuer per the ADD's "automated ACs are still satisfiable against a stubbed issuer" instruction. The LIVE rows the ADD and security review name (AC-006 through AC-018 end-to-end, SEC-006's replayed-code test, SEC-003's Settings → Logs visibility check) still require a real run against `https://auth.rbretschneider.com` and are not something this pass could exercise.
- **SEC-006 (replay a consumed code) cannot be tested against the stub issuer** as built — the stub always honours whatever code was issued to it (single-use enforcement is authentik's job, not something rIOt or the stub emulates). This is a genuine LIVE-only gap, not an oversight; the design correctly delegates single-use enforcement to the IdP (AD-005).
- **Trusted-proxy behaviour change.** Any existing deployment behind a reverse proxy will, after this upgrade, have every client's rate-limit bucket and audit IP collapse onto the proxy's address until `RIOT_TRUSTED_PROXIES` is set. This is the intended, secure-by-default behaviour (AD-020) and is documented in `.env.example`, but it is worth confirming operators are told about it explicitly in the runbook (condition C4/C6, technical-writer scope).
- **Windows dev-box quirks encountered** (informational, not defects): `npm run build:demo`'s inline env-var syntax doesn't execute under this Git-Bash/cmd.exe combination; work around with `VITE_DEMO=true npx vite build` directly. `docker cp` path translation misbehaves under Git-Bash; piping SQL via `docker exec -i psql` avoided it during the manual migration check.
- **`go.mod`'s `go` directive is unchanged at `1.24.0`** despite adding `go-oidc/v3` — see Deviations item 1 for why `v3.17.0` was chosen over the newest tag, and why that's still fully compliant with AD-002's stated security floors.
- Existing `docs/security/FLEET-DASH-security-review.md`'s note describing `riot_session` as `Strict` is now stale (as the ADD's AD-008 consequences section already anticipated) — this story does not edit that file; flagged for the technical writer per the ADD.
