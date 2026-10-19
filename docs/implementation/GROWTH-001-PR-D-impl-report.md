# Implementation Report: GROWTH-001 PR-D (origin control)

Story ID: GROWTH-001, PR-D (DUX D-30, D-72, AD-030, SEC-032)
Engineer: Senior Dev Agent
Date: 2026-10-19
Stack: Go 1.24 (chi v5, testify). Backend only; the dashboard is unchanged.

## Completed components

| File | Action | Notes |
|---|---|---|
| `internal/server/middleware/sameorigin.go` | Created | `SameOrigin(allowed)` and `AdminAuthSameOrigin(jwtSecret, allowed)` |
| `internal/server/middleware/sameorigin_test.go` | Created | Unit tests, including SEC-032 items 4 and 5 and the log throttle |
| `internal/server/router.go` | Modified | `adminAuth` defined once as `AdminAuthSameOrigin`; `SameOrigin` on `/api/v1/auth` and `/api/v1/setup` |
| `internal/server/router_origin_test.go` | Created | Router walk, agent-route list, shared-route shape |
| `README.md`, `CHANGELOG.md` | Modified | Scripts must send `Origin` and JSON; proxy settings |

Behaviour:

- Applies to `POST`, `PUT`, `PATCH`, `DELETE` only.
- Accepted when `Sec-Fetch-Site` is `same-origin`, or `Origin` equals `RequestScheme(r) + "://" + r.Host` or an `AllowedOrigins` entry (case-insensitive compare, like a normal origin match). `Origin: null` never matches.
- Otherwise 403 `{"error":"origin_refused"}`, before the next handler.
- With a body (`ContentLength != 0`, including chunked), `mime.ParseMediaType` must give `application/json`; `; charset=utf-8` is accepted. Bodyless requests need no `Content-Type`.
- Refusal log: one `slog.Warn` per client IP per minute (client IP from `ClientIP`, method only), table capped at 4096 entries. When the table is full of live entries a new address is not logged and not stored. Header values are never logged.
- SEC-032 item 1: the check is part of the `adminAuth` chain (origin check, then `AdminAuth`), used by every `r.With(adminAuth)` route and by the admin group. Nothing is attached to the shared `/api/v1/devices/{id}` route, so the device-key POSTs on it are untouched. An unauthenticated cross-origin request now gets 403 rather than 401 (origin check runs first).

## Test summary

| AC / condition | Test file | Tests | Status |
|---|---|---|---|
| D-72 cross-origin rejected | `middleware/sameorigin_test.go` | `TestGROWTH_D72_CrossOriginStateChangeRejected` | pass |
| D-72 same-origin accepted (GET, AllowedOrigins, Sec-Fetch-Site) | `middleware/sameorigin_test.go` | `TestGROWTH_D72_SameOriginAccepted` | pass |
| D-72 text/plain body rejected; JSON with charset accepted (SEC-032 item 4) | `middleware/sameorigin_test.go` | `TestGROWTH_D72_TextPlainBodyRejected` | pass |
| SEC-032 item 5, behind a proxy | `middleware/sameorigin_test.go` | `TestGROWTH_SEC032_BehindProxy` | pass |
| Refusal log throttle and table bound (injected clock) | `middleware/sameorigin_test.go` | `TestGROWTH_D72_RefusalLogThrottle` | pass |
| SEC-032 item 1, AdminAuth chain order | `middleware/sameorigin_test.go` | `TestGROWTH_SEC032_AdminAuthSameOriginRunsOriginBeforeAuth` | pass |
| D-72 router walk, SEC-032 item 2 | `router_origin_test.go` | `TestGROWTH_D72_EveryCookieStateChangingRouteHasOriginCheck` (mTLS off and on) | pass |
| D-72 agent routes, SEC-032 item 3 | `router_origin_test.go` | `TestGROWTH_D72_AgentRoutesUnaffected` (mTLS off and on) | pass |
| SEC-032 item 1, shared route shape | `router_origin_test.go` | `TestGROWTH_D72_SharedDeviceRouteShape` | pass |

Router walk: it uses `chi.Walk` on the real `setupRouter`. A route is cookie-authenticated when a middleware named `AdminAuth*` is in its chain, not by its group. For every state-changing route it requires one of four classes: cookie-authenticated or under `/api/v1/auth` or `/api/v1/setup` (must answer 403 `origin_refused` to a request with no headers, and to a cross-site one); a listed agent route; the explicit-404 pprof paths; anything else fails the test, so a new state-changing route must be classified on purpose. Mutation check: replacing `AdminAuthSameOrigin` with `AdminAuth` in the router makes the walk fail.

Agent routes: the device-key POSTs, register, enroll, renew, `/ws/agent` and `/update/check` are replayed through their real middleware chain (global and per-route) with a stub endpoint, once with no headers and once with a hostile `Origin`, `Sec-Fetch-Site` and `text/plain`. None answers 403 or `origin_refused`. The device-key POSTs are also sent to the real router and must answer 401 from the device-key check. The stub endpoint is used because the handlers behind these routes need a database; the tests never call them.

### Test run output

`go build ./...` and `go vet ./...`: no output (clean).

`go test -v -run TestGROWTH ./internal/server/middleware/ ./internal/server/`:

```
--- PASS: TestGROWTH_D72_CrossOriginStateChangeRejected (0.00s)
--- PASS: TestGROWTH_D72_SameOriginAccepted (0.00s)
--- PASS: TestGROWTH_D72_TextPlainBodyRejected (0.00s)
--- PASS: TestGROWTH_SEC032_BehindProxy (0.00s)
--- PASS: TestGROWTH_D72_RefusalLogThrottle (0.04s)
--- PASS: TestGROWTH_SEC032_AdminAuthSameOriginRunsOriginBeforeAuth (0.00s)
PASS
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	0.691s
--- PASS: TestGROWTH_D72_EveryCookieStateChangingRouteHasOriginCheck (0.00s)
--- PASS: TestGROWTH_D72_AgentRoutesUnaffected (0.00s)
--- PASS: TestGROWTH_D72_SharedDeviceRouteShape (0.00s)
PASS
ok  	github.com/DesyncTheThird/rIOt/internal/server	1.509s
```

`go test ./...` (Windows, no `-race`):

```
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-server	[no test files]
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer
ok  	github.com/DesyncTheThird/rIOt/internal/agent
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors
ok  	github.com/DesyncTheThird/rIOt/internal/models
ok  	github.com/DesyncTheThird/rIOt/internal/releasekey
ok  	github.com/DesyncTheThird/rIOt/internal/resilient
ok  	github.com/DesyncTheThird/rIOt/internal/server
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands
ok  	github.com/DesyncTheThird/rIOt/internal/server/db
ok  	github.com/DesyncTheThird/rIOt/internal/server/events
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers
?   	github.com/DesyncTheThird/rIOt/internal/server/logstore	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket
?   	github.com/DesyncTheThird/rIOt/internal/testutil	[no test files]
```

The suite was green before the change (no regressions in existing tests). The repository has no separate linter configuration; `go vet` and `gofmt` are clean for the new files.

## Deviations from the ADD

1. **`Sec-Fetch-Site` that is not `same-origin`.** AD-030 says a request is "rejected ... when `Sec-Fetch-Site` has any other value". A strict reading would also refuse a request whose `Origin` is in `RIOT_ALLOWED_ORIGINS` (which browsers mark `cross-site` or `same-site`) and so break the existing CORS `AllowedOrigins` feature. Agreed reading: a non-`same-origin` `Sec-Fetch-Site` is refused unless `Origin` matches the request origin or an `AllowedOrigins` entry. With no `Origin` match, any other value is refused as specified.
2. **Mount point.** AD-030 and ADD section 4.0 say "mount on the admin group". SEC-032 requires the AdminAuth chain instead, so `adminAuth` is `AdminAuthSameOrigin` for both the group and the per-device routes. `router.go` also loses its two duplicate `adminAuth :=` definitions (one definition now, before the setup routes).
3. **Unauthenticated cross-origin requests get 403, not 401**, because the origin check runs before `AdminAuth`. Intended by SEC-032 item 1.

No files outside ADD section 4.0 were changed. `internal/agent/docker_update.go` and `internal/server/handlers/auto_update.go` are untouched. No new dependencies, no new environment variables (`RIOT_ALLOWED_ORIGINS` and `RIOT_TRUSTED_PROXIES` already exist; the README now says they also affect this check).

## Notes for QA

- **Dashboard.** Every request in `web/src/api/client.ts`, `settings.ts`, `useAuth.ts` and `Setup.tsx` that has a body sets `Content-Type: application/json`; bodyless writes (delete, acknowledge, logout, run, cancel) rely on the browser's `Origin` or `Sec-Fetch-Site`. Smoke test: log in, change a setting, delete a probe, acknowledge an event, log out.
- **Scripts.** `curl -X POST .../api/v1/auth/login -d '{"password":"..."}'` with no `Origin` now returns 403. With `-H 'Origin: http://<host>' -H 'Content-Type: application/json'` it works. A body with `Content-Type: text/plain` or no `Content-Type` is refused even with a correct `Origin`.
- **Proxy.** Behind a proxy that rewrites `Host`, dashboard writes work via `Sec-Fetch-Site: same-origin`. A script through the same proxy needs `RIOT_ALLOWED_ORIGINS` set to the public origin.
- **Agents.** Heartbeat, telemetry, events, logs, probe results, command reports, register, enroll and renew take no `Origin` header and must keep working; the walk test pins this, but a real agent smoke test with and without mTLS is worth doing.
- **Log volume.** Expect at most one `origin refused` warning per client address per minute.
- **Edge case.** A request with `Content-Length: 0` and a `text/plain` type is treated as bodyless and allowed (it has no body to misinterpret).
