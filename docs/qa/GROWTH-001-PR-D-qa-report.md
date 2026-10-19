# QA Report

**Story ID:** GROWTH-001, PR-D (origin control; DUX D-30, D-72, AD-030, SEC-032)
**QA Engineer:** QA Agent
**Date:** 2026-10-19
**Verdict:** PASS WITH NOTES

---

## Test Run Summary

Run on Windows, without `-race`, on the branch with the QA tests added.

- `go build ./...` and `go vet ./...`: clean.
- `go test ./...`: every package passes, 0 failing, 0 skipped. The two QA files add 13 test functions on top of the implementation's tests.
- `cd web && npm ci && npm run test:run`: 66 files, 1409 tests, all passing. `web/` is unchanged from `main`, so this is a regression run only.
- Coverage: not measured (no coverage gate in the repository).
- Flaky tests: none seen. The QA tests that use a socket were run more than once and gave the same result.
- Mutation checks, all caught by tests (each reverted afterwards):
  - `AdminAuthSameOrigin` replaced by `AdminAuth` in `router.go`: the router walk and the through-router body test fail.
  - `SameOrigin` removed from the `/api/v1/auth` route: the same two tests fail.
  - `r.ContentLength == 0` loosened to `<= 0` in the middleware (which would let chunked bodies skip the JSON check): the chunked-body test and `TestGROWTH_D72_TextPlainBodyRejected` fail.

## AC Coverage Audit

| AC / condition | Status | Tests covering it | Gap description |
|---|---|---|---|
| D-72: cross-origin POST/PUT/PATCH/DELETE refused with 403 `origin_refused` before the handler runs | COVERED | `TestGROWTH_D72_CrossOriginStateChangeRejected` (each method, no Origin, `null`, cross-site, same-site, `none`, port and scheme mismatch); the handler-not-called assertion is in `assertRefused` | none |
| D-72: same-origin, `Sec-Fetch-Site: same-origin`, allowed origin accepted; GET/HEAD/OPTIONS untouched | COVERED | `TestGROWTH_D72_SameOriginAccepted`; QA `TestGROWTH_D72_MethodMatrix`, `TestGROWTH_D72_SafeMethodsAndWebSocketUpgradeAreNotOriginRefused` (through the real router, mTLS off and on) | none |
| D-72: body must be JSON, `charset` allowed, bodyless needs no type | COVERED | `TestGROWTH_D72_TextPlainBodyRejected`; QA `TestGROWTH_D72_JSONWithCharsetAccepted`, `TestGROWTH_D72_ZeroLengthBodyOverTheWire`, `TestGROWTH_D72_NonJSONBodiesRefusedThroughRouter` | The implementation's version of the chunked case set `ContentLength = -1` by hand; QA added real-wire tests (below) |
| D-72: log bound (one line per address per minute, capped table, no header values) | COVERED | `TestGROWTH_D72_RefusalLogThrottle` (injected clock, cap, sweep, no header values); QA `TestGROWTH_D72_RefusalLogThrottleConcurrent`, `TestGROWTH_D72_RefusalBodyDoesNotEchoInput` | none |
| SEC-032 item 1: check is on the `AdminAuth` chain, not on the shared device route | COVERED | `TestGROWTH_SEC032_AdminAuthSameOriginRunsOriginBeforeAuth`, `TestGROWTH_D72_SharedDeviceRouteShape`; mutation check above | none |
| SEC-032 item 2: router walk identifies cookie auth by chain, asserts every state-changing route | COVERED | `TestGROWTH_D72_EveryCookieStateChangingRouteHasOriginCheck` (mTLS off and on; a new unclassified state-changing route fails the test) | Named `...EveryCookieStateChangingRoute...` rather than the `...EveryAdminAuth...` name in the ADD (note 1) |
| SEC-032 item 3: agent routes unaffected (seven device POSTs, register, enroll, renew, `/ws/agent`, `/update/check`) | COVERED | `TestGROWTH_D72_AgentRoutesUnaffected` (mTLS off and on, hostile and plain headers, real chain with stub endpoint, plus the real router for the 401 from the device-key check); QA `TestGROWTH_D72_AgentWebSocketUpgradeUnaffected` | none |
| SEC-032 item 4: media type parsed, `application/json; charset=utf-8` accepted | COVERED | `TestGROWTH_D72_TextPlainBodyRejected` (subtests); QA `TestGROWTH_D72_JSONWithCharsetAccepted` (the ADD name) | none |
| SEC-032 item 5: behind a proxy, `Sec-Fetch-Site: same-origin` accepted with rewritten `Host`; 403 without it; README tells operators to set `RIOT_ALLOWED_ORIGINS` | COVERED | `TestGROWTH_SEC032_BehindProxy`; QA `TestGROWTH_D72_BehindProxySecFetchSiteAccepted` (the ADD name, over a real socket), `TestGROWTH_D72_UntrustedForwardedProtoDoesNotChangeOrigin`; README section "Origin check on state-changing requests" | none |
| SEC-037: PR-D carries no story documents | COVERED (inspection) | See "SEC-037" below | none |

## Test Quality Findings

- The implementation tests are behavioural: they assert on status, body, content type and on whether the guarded handler ran. They do not reach into private state, except the throttle tests, which read the throttle table (`last`) to prove the bound. That is acceptable because the cap is the property under test and the table is the only observable.
- The walk test classifies routes by the middleware chain, with the closure name of each middleware, so it follows the real router rather than a copy of the route list. A route that is neither cookie-authenticated, nor under `/auth` or `/setup`, nor in the agent list, fails the walk. Mutation checks confirmed it fails when the mount is wrong.
- The agent-route test replays the real middleware chain around a stub endpoint, because the real handlers need a database. This is a fair substitute for the question asked (does any origin check sit in front of these routes?), and the device-key POSTs are also sent to the real router.
- Isolation: no shared mutable state between tests. The throttle tests use an injected clock.
- Gap found and filled: the implementation never sent a chunked request over a real connection. A hand-built `httptest` request cannot prove how `net/http` fills `ContentLength` and `Body` for `Transfer-Encoding: chunked`, or for `Content-Length: 0` together with chunked. The QA tests do this over a socket (below).

## Adversarial Findings

All are covered by a test, and none is a defect.

- **Chunked bypass.** A chunked request has `ContentLength == -1` and a real body, so it takes the JSON check. Confirmed over a real socket for `text/plain`, no type, form, multipart, and `Content-Length: 0` sent together with `Transfer-Encoding: chunked` (net/http lets chunked win). A chunked `application/json` body passes. `TestGROWTH_D72_ChunkedBodyCannotBypassJSONCheck`.
- **Explicit `Content-Length: 0` with `text/plain`** is allowed; it is a genuinely empty body, and the same request with a two-byte body is refused. `TestGROWTH_D72_ZeroLengthBodyOverTheWire`.
- **Multipart and form bodies** are refused on admin, login and setup routes through the whole router, even with a correct `Origin`. `TestGROWTH_D72_NonJSONBodiesRefusedThroughRouter`.
- **Origin spelling.** Trailing slash, appended path, explicit default port, userinfo, longer host, subdomain, wrong port, empty and whitespace-only values are all refused. Upper-case scheme and host are accepted (a case-insensitive origin compare, which is correct). `TestGROWTH_D72_OriginSpellingVariants`.
- **Scheme behind a proxy.** A client that is not a trusted proxy cannot make an `https` Origin match by sending `X-Forwarded-Proto: https`. `TestGROWTH_D72_UntrustedForwardedProtoDoesNotChangeOrigin`.
- **HEAD, OPTIONS and WebSocket upgrades.** HEAD and OPTIONS on every state-changing admin path (with a foreign Origin) are not answered by the origin check, so CORS preflight is unaffected. GET upgrades on `/ws` and `/ws/terminal/...` from a foreign Origin get 401 from `AdminAuth` and are not answered by the origin check (the WebSocket origin check is separate and unchanged). The agent socket is untouched.
- **Login lock-out.** The origin check runs before the login rate limiter, so 20 cross-origin login attempts from one address all answer `origin_refused` and do not consume the login allowance. `TestGROWTH_D72_RefusedLoginDoesNotConsumeLoginRateLimit`.
- **Response body.** The refusal body is a fixed string and never echoes input. `TestGROWTH_D72_RefusalBodyDoesNotEchoInput`.
- **mTLS on.** Walk and agent tests run with the CA configured, so the enroll and renew routes and the client-certificate chain are included. The origin check is not reachable from them.
- **Log bound, concurrent use.** Eight goroutines hitting the throttle table with twice as many addresses as the cap never exceed the cap, and no address is admitted twice inside the window. `TestGROWTH_D72_RefusalLogThrottleConcurrent` (run without `-race` on this host).

## Tests Added by QA

| File | Tests | Covers |
|---|---|---|
| `internal/server/middleware/sameorigin_qa_test.go` | `TestGROWTH_D72_ChunkedBodyCannotBypassJSONCheck`, `TestGROWTH_D72_ZeroLengthBodyOverTheWire`, `TestGROWTH_D72_JSONWithCharsetAccepted`, `TestGROWTH_D72_BehindProxySecFetchSiteAccepted`, `TestGROWTH_D72_UntrustedForwardedProtoDoesNotChangeOrigin`, `TestGROWTH_D72_OriginSpellingVariants`, `TestGROWTH_D72_MethodMatrix`, `TestGROWTH_D72_RefusalLogThrottleConcurrent`, `TestGROWTH_D72_RefusalBodyDoesNotEchoInput` | D-72, SEC-032 items 4 and 5 |
| `internal/server/router_origin_qa_test.go` | `TestGROWTH_D72_SafeMethodsAndWebSocketUpgradeAreNotOriginRefused`, `TestGROWTH_D72_AgentWebSocketUpgradeUnaffected`, `TestGROWTH_D72_RefusedLoginDoesNotConsumeLoginRateLimit`, `TestGROWTH_D72_NonJSONBodiesRefusedThroughRouter` | D-72, SEC-032 items 1 to 3 |

Two of these (`...JSONWithCharsetAccepted`, `...BehindProxySecFetchSiteAccepted`) carry the names the ADD gives; the implementation covered the same behaviour under other names.

## Dashboard Compatibility

`web/` is unchanged. Every `fetch` with a state-changing method in `web/src` either sets `Content-Type: application/json` together with its body, or sends no body (delete, acknowledge, cancel, run, logout), in which case the browser supplies `Origin` or `Sec-Fetch-Site`. The shared `mutate` helper in `settings.ts` sets the type only when a body exists. No `FormData`, `sendBeacon` or `XMLHttpRequest` use. Dashboard unit tests pass.

## SEC-037

`git diff main` touches only: `CHANGELOG.md`, `README.md`, the implementation report, `sameorigin.go`, `router.go` and two test files. There is no FRD, ADD or security review in the branch, and no description of other issues. A search of the added lines for host names, LAN addresses, local paths and personal names found none. The only addresses are documentation-range examples (`192.0.2.x`, `10.0.0.0/8`) and `example.com` names in tests, the same convention the repository already uses. The implementation report and this report refer to review items by label only (for example SEC-032) and describe nothing else.

## Deviations from ADD

None that affect behaviour. Recorded for the PR description:

1. **Test names.** The ADD names `TestGROWTH_D72_EveryAdminAuthStateChangingRouteHasOriginCheck`, `TestGROWTH_D72_JSONWithCharsetAccepted` and `TestGROWTH_D72_BehindProxySecFetchSiteAccepted`. The implementation used `TestGROWTH_D72_EveryCookieStateChangingRouteHasOriginCheck`, a subtest, and `TestGROWTH_SEC032_BehindProxy`. QA added the latter two under the ADD names; the walk keeps its own name (it does what the ADD describes).
2. **`chain.go` not created.** The ADD lists a `chain.go` and a `middleware.Chain(...)`. The implementation uses `AdminAuthSameOrigin(secret, allowed)` in `sameorigin.go`, which is the same chain with the origin check first. Behaviour is the same.
3. **Implementation report name.** `GROWTH-001-PR-D-impl-report.md` rather than `GROWTH-001-impl-report-D.md`. Naming only.
4. **`Sec-Fetch-Site` handling** (the implementation report's deviation 1): a non-`same-origin` value is refused unless `Origin` matches the request origin or an allowed origin. QA agrees: a literal reading would break the existing `RIOT_ALLOWED_ORIGINS` cross-origin feature, and a request with no `Origin` match is still refused as specified.
5. **Unauthenticated cross-origin request gets 403 rather than 401.** Intended by SEC-032 item 1 and tested.

## Deviations from FRD

None.

## Notes (non-blocking)

1. `RIOT_ALLOWED_ORIGINS` entries are compared exactly after trimming spaces and ignoring case. An entry with a trailing slash (`https://app.example.com/`) never matches, because browsers send no trailing slash. The existing CORS code compares exactly as well, so this is consistent, but the README could say "no trailing slash".
2. The refusal-log table is bounded at 4096 addresses. When it is full of live entries, a new address is not logged (by design), and each new address then triggers one sweep of the table under the mutex. The cost is bounded by the cap and by how many distinct client addresses an attacker controls. No action needed now.
3. `Origin` is compared case-insensitively here, while the CORS middleware compares exactly. A differently-cased allowed `Origin` passes this check but gets no CORS response headers. This is harmless.
4. A real-agent smoke test, with and without mTLS, against a deployed build is still worth doing before release. The tests cover the middleware chain and the real router, but not a live agent.

## Verdict Rationale

PASS WITH NOTES. Every PR-D acceptance criterion and every SEC-032 condition (items 1 to 5) is covered by a test that fails when the behaviour is broken, as the mutation checks show. All Go and web tests are green. No defect was found, and no implementation file was changed. SEC-037 is satisfied. The notes above are minor and non-blocking.

## Action Required (if FAIL)

None.
