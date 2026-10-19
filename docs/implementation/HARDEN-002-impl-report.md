# HARDEN-002 — Implementation Report

**Story:** HARDEN-002 — Close live exposures (code-side): pprof lockdown, security headers, write-only notification secrets, constant-time registration key, honest health/readiness, removal of the false retry-queue claim.
**Engineer:** senior_dev (Model: Sonnet 5.5). **Date:** 2026-10-17.
**Branch:** `claude/youthful-austin-8d4743` (worktree `D:\Repos\rIOt\.claude\worktrees\youthful-austin-8d4743`).
**Inputs:** FRD, ADD revision 2 (binding, includes security amendments C1–C7 and SEC-008), security review (APPROVE WITH CONDITIONS).

## Detected stack
- Go 1.26 (module `github.com/DesyncTheThird/rIOt`), chi v5, pgx v5, gorilla/websocket, testify.
- React 18 + TypeScript + Tailwind (Vite), vitest 4 + Testing Library, TanStack Query.
- Active specialists: Backend Engineer, Frontend Engineer, Infrastructure (`.env.example` only).
- Platform: Windows. `-race` was not run (needs CGO, per project convention); run it on Linux CI.

## Result
- Baseline confirmed green before any change: `go test ./...` 19 packages ok, `npm run test:run` 27 files / 405 tests.
- After: `go test ./...`, `go vet ./...`, `go build ./...`, `npm run test:run` and `tsc --noEmit` are all green. Working tree clean, everything committed. Nothing pushed, tagged or deployed.
- No new Go or npm dependency, no migration, no agent API change.
- Every in-scope AC in ADD §8 has a test with the exact name the ADD gives. AC-008 and AC-056 to AC-061 are N/A (OQ-6, OQ-1). AC-062 is satisfied by the existing agent-route suites passing unmodified.

## Test counts
| | Before | After | Delta |
|---|---|---|---|
| Go top-level tests (`--- PASS` at column 0) | 757 | 856 | +99 (+104 new `HARDEN002` tests, −5 deleted retry-queue tests) |
| Go tests including subtests | 1217 | 1499 | +282 |
| Vitest files | 27 | 30 | +3 |
| Vitest tests | 405 | 449 | +44 |

## Commits (oldest first)
All prefixed `[HARDEN-002]` and ending with the `Co-Authored-By: Claude Sonnet 5.5` trailer.

1. `cf2db43` feat: security headers middleware and shared IsHTTPS predicate
2. `7fb27c7` feat: global middleware chain with security headers and static asset Content-Type map
3. `e278efb` fix: take pprof off the main router and add the opt-in loopback listener
4. `48e8947` test: doctor server check handles a 503 from /health
5. `221e64d` feat: /health returns 503 when degraded and add /readyz
6. `6d39261` fix: compare the registration key in constant time
7. `918857a` feat: notification secret registry with redact, write-only merge and scrub helpers
8. `2bd5187` fix: scrub secrets from notification send errors at the dispatcher choke point
9. `bc85e5e` feat: write-only notification secrets in the settings API
10. `299596d` test: security headers on every response class and WebSocket handshakes through the middleware chain
11. `9b4069a` test: OIDC redirects are unaffected by the security headers
12. `6172254` fix: remove the dead notification retry queue and its README claims
13. `9c8b5f1` feat: SecretInput and sentinel helpers for write-only secrets
14. `f2497a7` feat: notification editor handles write-only secrets
15. `0f035f1` fix: demo client returns redacted notification secrets
16. `4856eaf` config: document RIOT_PPROF_ADDR in .env.example
17. `c9e06fc` test: README and CHANGELOG document the hardening changes
18. (this report) docs: implementation report

## Completed components (ADD §4)
| File | Action | Notes |
|---|---|---|
| `internal/server/pprof.go` | CREATE | `pprofHandler` (moved), `newPprofMux`, `pprofNotFound`, `ParsePprofAddr`, `listenPprof`, `startPprof`, nil-safe `pprofServer`. Bind address is rebuilt from parsed values only; `[::ffff:127.0.0.1]` rejected (no `IsLoopback()` shortcut for IPv6). |
| `internal/server/pprof_test.go` | CREATE | AC-002, AC-003 (+C5), AC-004 (+C5, 23-row table), bind failure, AC-005, AC-007. |
| `internal/server/router.go` | MODIFY | pprof import/function/mount removed; `/debug/pprof` and `/debug/pprof/*` explicit 404; `globalMiddleware()` (RealIP, SecurityHeaders, Logger, Recoverer, CORS, SetupGuard); `HealthHandler` wiring (typed-nil guard); `staticContentTypes` map in `serveFrontend`. |
| `internal/server/router_test.go` | CREATE | Harness (a) real `setupRouter`, harness (b) production chain over mock handlers. AC-001 (+C5 variants), AC-006, AC-009, AC-010, AC-013, AC-014, AC-015, AC-017, AC-018, AC-050. |
| `internal/server/invariants_test.go` | CREATE | C5(b) scan: no `DefaultServeMux`/`ListenAndServe`/`Serve` use, every `http.Server` literal sets `Handler`. Plus a self-test of the scanner. |
| `internal/server/server.go` | MODIFY | `pprof` field, `MigrationsComplete atomic.Bool` (set after the migrations block), `startPprof` call, `s.pprof.Shutdown` before `httpServer.Shutdown` (error logged at WARN). |
| `internal/server/config.go` / `config_test.go` | MODIFY | `PprofAddr` from trimmed `RIOT_PPROF_ADDR`; no validation in `LoadConfig`. |
| `internal/server/docs_test.go` | CREATE | AC-054, AC-055, AC-064. |
| `internal/server/middleware/security_headers.go` | CREATE | `SecurityHeaders`, `ContentSecurityPolicy`, `validCSPHost` (byte loop, 1–262 bytes). CSP includes `form-action 'self'` (SEC-008). Never wraps the writer. |
| `internal/server/middleware/security_headers_test.go` | CREATE | AC-009, AC-011, AC-012 (+C6 byte table, 263/262-byte boundary), AC-013, AC-014, AC-015, AC-017 (Hijacker/Flusher survive). |
| `internal/server/middleware/clientip.go` | MODIFY | `IsHTTPS(r)`. |
| `internal/server/middleware/setup.go` / `setup_test.go` | MODIFY | `/readyz` allowed through the guard; test. |
| `internal/server/handlers/auth.go`, `oidc.go` | MODIFY | Four cookie `Secure` sites use `middleware.IsHTTPS(r)`. |
| `internal/server/handlers/health.go` | CREATE | `DBHealthChecker`, `DefaultHealthProbeTimeout` (2 s), `HealthHandler.Health/Readyz`, fixed body structs, buffered-channel probe. |
| `internal/server/handlers/health_test.go` | CREATE | AC-045 to AC-049, AC-051 (checker honouring and ignoring its context), AC-052. |
| `internal/server/handlers/handlers.go` | MODIFY | `Health` deleted; `registrationKeyMatches` (SHA-256 digests + `subtle.ConstantTimeCompare`) used in `RegisterDevice`. |
| `internal/server/handlers/register_key_test.go` | CREATE | AC-041 to AC-044 (+ source scan for `crypto/subtle`). |
| `internal/server/handlers/settings.go` | MODIFY | List/Create/Update redaction; Update loads stored channel (404 + no write if missing), merges secrets. |
| `internal/server/handlers/settings_secrets_test.go` | CREATE | AC-019 to AC-034 including the C1 tests and the C2 test-send table. |
| `internal/server/handlers/security_headers_oidc_test.go` | CREATE | AC-016. |
| `internal/server/notify/secrets.go` | CREATE | `RedactedSecret`, registry, `SecretKeys`, `splitURLUserinfo`, `RedactChannel`, `MergeSecrets`, `ScrubSecrets`, `scrubError`. |
| `internal/server/notify/secrets_test.go` | CREATE | Pure tables for redaction, merge, sweep, userinfo split, scrub (stdlib `***` and `%q` forms), plus sentinel parity with the frontend file. |
| `internal/server/notify/scrub_client_test.go` | CREATE | C2: real `http.Client` failures via an erroring `RoundTripper`, 6 backends × `Dispatch`, `SendToChannel`, `TestChannel`, with a precondition proving the unscrubbed error really leaks. |
| `internal/server/notify/dispatcher.go` / `dispatcher_test.go` | MODIFY | `scrubError` at the three `backend.Send` sites; AC-035 baseline. |
| `internal/testutil/mocks.go` | MODIFY | `MockNotifyRepo.UpdateChannelCalls`. |
| `internal/resilient/retry_queue.go`, `retry_queue_test.go` | DELETE | Option B. `dns_cache.go` and `http_client.go` untouched. |
| `internal/agent/doctor_test.go` | MODIFY | AC-053 (test only). |
| `web/src/utils/secrets.ts` (+ test) | CREATE | `REDACTED_SECRET`, `SECRET_KEYS`, `isRedacted`, `isSecretSet`, `redactChannelConfig`. |
| `web/src/components/SecretInput.tsx` | CREATE | Sentinel-aware input, Clear/Undo, full C3 autofill opt-out attribute set. |
| `web/src/pages/settings/NotificationSettings.tsx` (+ test) | MODIFY / CREATE | `original` state, `configured()`, `SecretInput` for every T-1 secret, autofill opt-out on SMTP username and ntfy/gotify `server_url`, list rows that never show secrets. |
| `web/src/api/demo-data.ts`, `demo-settings.ts` (+ test) | MODIFY / CREATE | Mock ntfy channel uses `server_url` and a sentinel topic; create/update echo `redactChannelConfig`. |
| `README.md`, `CHANGELOG.md` | MODIFY | See Deviations item 5. |
| `.env.example` | MODIFY | `RIOT_PPROF_ADDR` entry (requested in the task). |

Not touched (ADD §4.1): `Dockerfile`, compose files, `internal/agent/doctor.go`, `internal/server/db/*`, migrations, `web/src/api/settings.ts`, `web/src/types/models.ts`, `web/src/pages/Login.tsx`, `middleware/logger.go`, `cors.go`, `admin_auth.go`, `auth.go`.

## AC → test mapping
Go tests are `TestHARDEN002_<id>_…` and vitest titles start `[HARDEN-002][AC-0NN]`.

| AC | Tests (file) |
|---|---|
| 001 | `AC001_PprofNotServedOnMainRouter`, `AC001_C5_PprofPathVariantsServeNoProfile` (router_test) |
| 002 | `AC002_PprofDisabledByDefault` (pprof_test); `AC002_LoadConfig_PprofAddrDefaultsEmpty` (config_test) |
| 003 | `AC003_LoopbackAddrsAccepted`, `AC003_C5_BindAddrBuiltFromParsedValues` (pprof_test) |
| 004 | `AC004_NonLoopbackAddrsRejected`, `AC004_BindFailureDoesNotAbort` (pprof_test) |
| 005 | `AC005_LoopbackListenerServesPprof` (pprof_test) |
| 006 | `AC006_LoopbackEnabledMainRouterStill404` (router_test) |
| 007 | `AC007_PprofEnableLoggedAndShutdownCloses` (pprof_test) |
| 008 | N/A (OQ-6) |
| 009 | `AC009_BaselineHeadersOnSPA` (router_test); `AC009_BaselineHeadersSetBeforeHandler` (middleware) |
| 010 | `AC010_HeadersOnEveryResponseClass` (12 subtests across both harnesses), `AC010_CORSHeadersUnchangedByHeaderMiddleware` (router_test) |
| 011 | `AC011_CSPDirectiveContent` (middleware; asserts `form-action 'self'` present) |
| 012 | `AC012_CSPPermitsSameHostWebSockets`, `AC012_C6_HostInjectionFallsBackToSelf` (middleware) |
| 013 | `AC013_HSTSOnDirectTLS` (middleware, router_test) |
| 014 | `AC014_HSTSBehindTrustedProxyMatchesSecureCookie` (router_test, includes login cookie `Secure`); `AC014_HSTSBehindTrustedProxyMatchesIsHTTPS` (middleware) |
| 015 | `AC015_NoHSTSOnPlainHTTPOrUntrustedPeer` (middleware, router_test) |
| 016 | `AC016_OIDCRedirectsUnaffectedByHeaders`, `AC016_OIDCFailureRedirectCarriesHeaders` (security_headers_oidc_test) |
| 017 | `AC017_WebSocketUpgradesThroughMiddleware` (router_test, real 101 on `/ws` and `/ws/agent` plus message exchange); `AC017_SecurityHeadersDoNotWrapWriter` (middleware) |
| 018 | `AC018_StaticAssetContentTypes` (router_test) |
| 019 | `AC019_SMTPPasswordRedactedOnList`, `AC019_C1_URLUserinfoRedactedOnList` (settings_secrets_test); pure `AC019_C1_Pure_URLUserinfoRedacted` |
| 020 | `AC020_EveryRegistryTypeRedacted` (settings_secrets_test); pure `AC020_Pure_RedactEveryRegistryType` |
| 021 | `AC021_WebhookURLAndHeaderValuesRedacted` (+ pure, + non-map headers) |
| 022 | `AC022_UnsetSecretsNotSentinel` (+ pure) |
| 023 | `AC023_UnknownTypeFullyRedacted` (+ pure) |
| 024 | `AC024_CreateStoresCleartextReturnsRedacted`, `AC024_C1_CreateAndUpdateResponsesRedactUserinfo` |
| 025 | `AC025_PutSentinelKeepsSecret`, `AC025_C1_NameOnlyPutPreservesUserinfo` (+ pure) |
| 026 | `AC026_PutOmittedKeepsSecret` (+ pure) |
| 027 | `AC027_PutNewValueReplaces` (+ pure) |
| 028 | `AC028_PutEmptyClears` (+ pure) |
| 029 | `AC029_SentinelNeverPersisted` (a, b, c), `AC029_C1_UserinfoSentinelNeverPersisted` (a, b, c) (+ pure) |
| 030 | `AC030_WebhookHeaderMerge` (+ pure) |
| 031 | `AC031_NonSecretKeysReplaced` (+ pure) |
| 032 | `AC032_PutUnknownChannel404` (`UpdateChannelCalls == 0`), `AC032_PutRepositoryErrorOnLookupIs404WithoutWrite` |
| 033 | `AC033_TestSendUsesStoredSecretAfterRedactedEdit` |
| 034 | `AC034_TestSendErrorScrubbed`, `AC034_C2_TestSendRealClientErrorsScrubbed` (6 backends); pure `AC034_Pure_ScrubErrorBehaviour` |
| 035 | `AC035_DispatchFailureLogsScrubbed` (dispatcher_test), `AC035_C2_RealClientErrorsScrubbed` (scrub_client_test), `C2_Pure_ScrubSecretsMatchesStdlibForms` (secrets_test) |
| 036 | VT `[AC-036] saving after a name-only edit submits password as the sentinel, never ""` (+ revert-on-erase, + userinfo round trip) |
| 037 | VT `[AC-037] redacted bot_token shows "Configured (unchanged)", is not editable text, and typing NEWTOKEN submits it`; `[AC-037][C3] secret inputs opt out of browser credential autofill` (smtp, ntfy, telegram, pushover, gotify) |
| 038 | VT `[AC-038] Clear on ntfy token submits token ""` (+ Undo, + SMTP Clear, + no Clear elsewhere) |
| 039 | VT `[AC-039] Test calls testNotificationChannel with the id only` |
| 040 | VT `[AC-040] demo channels expose only sentinel or empty secrets` (+ create/update echo) |
| 041–044 | `AC041_CorrectKeyRegisters`, `AC042_WrongKeysRejectedIdentically`, `AC043_OpenRegistrationUnchanged`, `AC044_RegistrationKeyMatchesTruthTable`, `AC044_UsesConstantTimePrimitive` (register_key_test) |
| 045–049, 051, 052 | `AC045_HealthOK`, `AC046_HealthDegradedReturns503`, `AC047_ReadyzReady`, `AC048_ReadyzDBDown503`, `AC049_ReadyzMigrationsIncomplete503`, `AC051_HungProbeBounded`, `AC052_HealthBodiesLeakNothing` (health_test) |
| 050 | `AC050_HealthEndpointsPublicAndSetupIndependent` (router_test); `AC050_SetupGuard_ReadyzPassesInSetupMode` (setup_test) |
| 053 | `AC053_DoctorServerCheckHandles503` (doctor_test) |
| 054, 055, 064 | `AC054_ReadmeHasNoRetryClaim`, `AC055_RetryQueueRemoved`, `AC064_DocsUpdated` (docs_test) |
| 056–061 | N/A (OQ-1) |
| 062 | Existing agent-route suites pass unmodified |
| 063 | See the test runs below |
| C5(b) | `C5_NoNilHandlerOrDefaultServeMux` (invariants_test) |

## Test runs

### 1. New tests only
`go test -count=1 ./... -run HARDEN002` (104 top-level tests, 287 including subtests; packages with none report "no tests to run"):
```
ok  	github.com/DesyncTheThird/rIOt/internal/agent	1.982s
ok  	github.com/DesyncTheThird/rIOt/internal/server	2.345s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	1.955s
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	0.795s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	1.313s
(all other packages: ok ... [no tests to run])
```
New vitest files (`npx vitest run src/utils/secrets.test.ts src/api/demo-settings.test.ts src/pages/settings/NotificationSettings.test.tsx`):
```
 ✓ src/utils/secrets.test.ts (20 tests) 6ms
 ✓ src/api/demo-settings.test.ts (3 tests) 829ms
 ✓ src/pages/settings/NotificationSettings.test.tsx (21 tests) 885ms
 Test Files  3 passed (3)
      Tests  44 passed (44)
```

### 2. Full `go test ./...`
```
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-server	[no test files]
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer	1.125s
ok  	github.com/DesyncTheThird/rIOt/internal/agent	2.592s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	4.974s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.404s
ok  	github.com/DesyncTheThird/rIOt/internal/releasekey	0.867s
ok  	github.com/DesyncTheThird/rIOt/internal/resilient	1.300s
ok  	github.com/DesyncTheThird/rIOt/internal/server	2.559s
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	0.774s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	0.815s
?   	github.com/DesyncTheThird/rIOt/internal/server/db	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	0.874s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	3.831s
?   	github.com/DesyncTheThird/rIOt/internal/server/logstore	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	0.966s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	1.253s
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc	2.171s
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	1.349s
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	0.718s
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary	1.161s
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	0.526s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	0.725s
?   	github.com/DesyncTheThird/rIOt/internal/testutil	[no test files]
```

### 3. `go vet ./...`
```
(no output)
go vet exit code: 0
```

### 4. `cd web && npm run test:run`
```
 Test Files  30 passed (30)
      Tests  449 passed (449)
```

### 5. `cd web && npx tsc --noEmit`
```
(no output)
tsc --noEmit exit code: 0
```
There is no separate lint script in `web/package.json`; `npm run build` is `tsc && vite build`, and `tsc` is the type check run here.

### 6. `go build ./...`
```
(no output)
go build exit code: 0
```

## Deviations from the ADD
None changes the design. Items 1 to 4 are small implementation choices the architect may want to glance at.

1. **`ScrubSecrets` uses a single-pass `strings.NewReplacer`** (variants ordered longest first, so longer matches win ties) instead of sequential `strings.ReplaceAll` calls (AD-008 step 4). The set of strings replaced is identical. The difference: replacement text is never rescanned, so a very short secret that happens to occur inside `[rIOt-redacted]` (for example a password of `redacted`) cannot corrupt an earlier replacement. Pinned by `C2_Pure_ScrubDoesNotCorruptOnShortOrOverlappingSecrets`.
2. **`pprofServer.Shutdown` falls back to `srv.Close()`** when the graceful shutdown returns an error (for example the 10 s deadline expires while a 30 s `/profile` is streaming), so the address is never left bound. AD-002 only says "Shutdown(ctx)".
3. **`deepCopyConfig` also deep-copies `[]interface{}` values**, not just nested maps, so a response can never alias a stored slice. Behaviour is otherwise as specified.
4. **`ParsePprofAddr` reason strings for edge inputs.** `garbage` and `127.0.0.1:` report `missing port`; `127.0.0.1:0` reports `port not canonical decimal` (first digit rule), while `70000` reports `port out of range`. All are in the ADD's reason list; the mapping of inputs to reasons is pinned in `AC004_NonLoopbackAddrsRejected`.
5. **README and CHANGELOG prose.** The task said to leave prose to the technical writer except where an AC test needs text. AC-064 needs ten specific README terms and a CHANGELOG entry per behaviour change, so Clod wrote minimal accurate sections rather than the full §12.3 text: an env-table row and a "Profiling (pprof)" subsection, a "Security headers" paragraph in the reverse-proxy section (HSTS `every port` caveat included), a "Secrets are write-only" paragraph (with the `edit channels` limit), and the `/health` and `/readyz` API rows. The CHANGELOG has `[HARDEN-002]` bullets under new `Security`, `Changed` and `Removed` headings. The technical writer should still polish and extend these.
6. **ADD §13 greps.**
   - `grep -rn 'RetryQueue' --include=*.go .` is not empty: the ADD-mandated test name `TestHARDEN002_AC055_RetryQueueRemoved` contains the word. The test's own source scan skips `docs_test.go` and finds no symbol anywhere else.
   - `grep -rn 'RequestScheme(r) == "https"' internal/` matches one line: the body of `IsHTTPS` itself in `clientip.go`. The four cookie sites no longer contain it.
7. **`MockNotifyRepo.UpdateChannelCalls` increments before the `Err` check**, so a failed update still counts as a call. That is the stricter reading of "UpdateChannel was not called".

No blockers; `docs/architecture/HARDEN-002-blockers.md` was not needed. No ARCH-QUESTION ambiguity required a guess.

## Notes for QA
- **Pre-deploy (orchestrator, C7):** confirm `RIOT_TRUSTED_PROXIES` is still unset on the deployed server.
- **C3 manual check (cannot be automated):** in Chrome with a saved rIOt login, open each masked channel type (smtp, ntfy, telegram, pushover, gotify) and confirm nothing is prefilled and no save-password prompt appears after saving. The vitest attribute test only proves the opt-out attributes are present.
- **Post-deploy checks** are in ADD §13 (curl for 404 on pprof and the headers, `/health` and `/readyz` 200, devtools CSP violations on login, device page, terminal, settings and charts, an agent still heartbeating under mTLS).
- **CSP assumptions to probe in a real browser:** the production Vite build was not produced here. `script-src 'self'` relies on the build emitting only external `/assets/*` scripts, and `style-src 'unsafe-inline'` covers xterm.js and Recharts. `npm run build` plus a browser pass will confirm.
- **Stored-secret edge cases to probe:** edit a channel's host while keeping a userinfo URL (the credential is kept; this is the documented destination-rebinding residual, follow-up F-7); change a channel's type in the editor (secrets must not carry over); open an ntfy channel whose `server_url` shows `https://[rIOt-redacted]@host` and save without touching it.
- **Test isolation:** tests that swap `slog.Default()` (`scrub_client_test.go`, `dispatcher_test.go`) must not be run with `t.Parallel()`. The pprof listener tests bind `127.0.0.1` ephemeral ports; `AC003_C5_BindAddrBuiltFromParsedValues` picks a free port and rebinds it, which has a tiny race window if the port is stolen in between.
- **Windows line endings:** the repo uses `core.autocrlf=true`, so `gofmt -l` lists many pre-existing files in the working tree. Files created in this story are LF and gofmt-clean.
- **Not changed, still open (ADD §12.4 follow-ups):** F-1 registration key readable in cleartext, F-3 `RIOT_API_KEY` never enforced (README L139 claim), F-7 destination binding, F-8 re-registration key minting (elevated), F-10 health probe cost, F-11 registry-versus-backend key test, F-12 remote error body size.

## Post-QA fixes (orchestrator)

QA verdict PASS WITH NOTES raised two defects; both fixed before the technical writer ran:

- **QA-1 (MEDIUM), `5aeba58`:** `startPprof` now takes the main port and rejects a `RIOT_PPROF_ADDR` whose port equals it (reason `port collides with the main server port`). pprof binds before the main listener, and on Linux a `127.0.0.1:P` listener makes the main wildcard bind of `:P` fail with `EADDRINUSE` and exit the server. New test `TestHARDEN002_AC004_QA1_PprofPortEqualToMainPortRejected`.
- **QA-2 (LOW), `4868eb9`:** a webhook PUT that sends the bare sentinel string as `headers` is treated as "unchanged" (keeps the stored headers) instead of wiping them. The QA test `a non-map headers value never persists the sentinel` now also asserts the stored headers survive.

`go build ./...`, `go vet ./...` and `go test ./internal/server/...` are green after both fixes.
