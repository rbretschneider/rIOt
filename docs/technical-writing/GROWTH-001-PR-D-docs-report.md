# Documentation Report
**Story ID:** GROWTH-001, PR-D (origin control)
**Author:** Technical Writer Agent
**Date:** 2026-10-19

## Audience

- Developers and operators: anyone who runs rIOt, scripts the admin API, or puts rIOt behind a TLS-terminating reverse proxy.
- End users of the dashboard: no documentation impact. The dashboard sends `Origin` or `Sec-Fetch-Site` on every state-changing request, so it needs no change.

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| README.md | Updated | "Origin check on state-changing requests" section rewritten for a new reader: who is affected, the accepted headers, a working script example, proxy setup as two ordered steps, the exact-match format for `RIOT_ALLOWED_ORIGINS`, and the 403-instead-of-401 change. |
| README.md | Updated | `RIOT_ALLOWED_ORIGINS` row in the environment variable table now states the `scheme://host[:port]`, no-trailing-slash format. |
| README.md | Fixed | The "Recovering an agent that lost its API key" curl example called the login and rotate-key endpoints with no `Origin` header. Both requests would now get 403. Both now send `Origin`. |
| CHANGELOG.md | Updated | The Unreleased "Security" entry now states who must act (scripts, proxy operators), the exact-match rule for `RIOT_ALLOWED_ORIGINS`, and the 403-instead-of-401 change. |
| internal/server/middleware/sameorigin.go | Inline comments | Added the reasons behind four non-obvious choices: `Sec-Fetch-Site` is checked first (proxies rewrite `Host`), a missing or `null` Origin is refused, the JSON requirement closes the no-preflight form and `text/plain` route, and chunked bodies have `ContentLength` -1. Also why the refusal body is fixed, why header values are never logged, and why the log is throttled. |
| internal/server/router.go | Inline comments | The `adminAuth` comment now explains why agent routes are excluded and why an unauthenticated cross-origin write gets 403. Added a comment on why `/api/v1/setup` and `/api/v1/auth` take the origin check alone. |

Comments only; no logic changed. `go build ./...`, `go vet ./internal/server/...` and `go test ./internal/server/ ./internal/server/middleware/` pass after the edits.

## Verification

- Every claim in the README section was checked against `sameorigin.go`, `router.go` and `config.go`: accepted headers, the 403 body, the JSON rule (including bodyless and chunked requests), the exact, case-insensitive `Origin` compare, and the one-line-per-address-per-minute log.
- The README anchor `#behind-a-reverse-proxy-riot_trusted_proxies` matches the heading "Behind a reverse proxy: `RIOT_TRUSTED_PROXIES`".
- The curl examples were not run against a live server. They follow the behaviour pinned by the middleware tests (an `Origin` equal to the request origin plus a JSON body passes).
- The added text contains no real host names, LAN addresses, local paths or personal names. Examples use `riot.example.com` and `<server>` placeholders.
- SEC-037: this branch adds no FRD, ADD or security review. The README, CHANGELOG and comments refer to review items by label only.

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| README.md, environment variable table, `RIOT_TRUSTED_PROXIES` row | Points to the "Single Sign-On" section for details. The proxy guidance lives in the subsection "Behind a reverse proxy", which sits under that heading. | Link directly to `#behind-a-reverse-proxy-riot_trusted_proxies`. |

## Accuracy Flags

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| The ADD says to mount the check "on the admin group"; the code attaches `AdminAuthSameOrigin` to each cookie-authenticated route and group, and attaches nothing to the shared `/api/v1/devices/{id}` route. | `internal/server/router.go` | Documented what the code does. |
| The ADD says a non-`same-origin` `Sec-Fetch-Site` is refused. The code accepts it when `Origin` matches the request origin or `RIOT_ALLOWED_ORIGINS`, which keeps cross-origin deployments listed there working. | `internal/server/middleware/sameorigin.go` | Documented what the code does. |
| The ADD lists a `chain.go` and `middleware.Chain(...)`. The code has `AdminAuthSameOrigin` in `sameorigin.go`. | `internal/server/middleware/` | Documented what the code does. |

## Notes for Future Writers

- The working tree checks out with CRLF line endings on Windows; Git stores LF. Edit tools that match multi-line strings can fail on CRLF. A small script that splices by heading was used for the README section.
- Origin-check text lives in one README section. The environment variable table, the API intro and the key-recovery example link to it. Keep it the single source.
- Any new README `curl` example that writes through the cookie-authenticated API needs an `Origin` header.
