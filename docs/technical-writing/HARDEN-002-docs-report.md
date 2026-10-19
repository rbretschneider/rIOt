# Documentation Report

**Story ID:** HARDEN-002
**Author:** Technical Writer Agent
**Date:** 2026-10-18

---

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` > Server Environment Variables | Updated | `RIOT_PPROF_ADDR` row now states the loopback `ip:port` rule and that the port must differ from `RIOT_PORT`. |
| `README.md` > Profiling (pprof) | Rewritten | Rules table (host, port, port collision), what the server logs on rejection and success, a note that the compose files do not forward the variable, `docker exec` recipes for a heap profile and a 30-second CPU profile (BusyBox `wget`), the direct-host `curl` form, and security notes (unauthenticated, host-network exposure). Replaces the engineer's three-paragraph minimal section. |
| `README.md` > Security Headers | Created (moved and expanded) | The engineer's "Security headers" paragraph sat as a `####` heading under the reverse-proxy subsection. It is now a top-level section after Single Sign-On with a header table, a CSP directive table, the rule for the reflected `Host`, the no-iframe consequence, and an HSTS subsection (trigger rule, "every port", no click-through on certificate errors, IP hosts exempt, no server-side reset, no setting to disable). The reverse-proxy section keeps a one-line pointer. |
| `README.md` > Notification Channels | Rewritten | New "Secrets are write-only" subsection: per-channel redaction table, URL userinfo redaction, editor behaviour (Configured (unchanged), Clear, Undo), API contract for `PUT` (omit or placeholder keeps, value replaces, empty string clears, webhook header merge, type change inherits nothing, unknown id returns `404`), the "cannot copy a channel by reading it" consequence, test-send behaviour, and the threat-model limits (not against anyone who can edit channels; not at rest). |
| `README.md` > API | Updated | `/health` row (200 ok, 503 degraded, about 2 s bound), new `/readyz` row (200 ready, 503 not_ready, not blocked by setup), and the notification-channel CRUD row (write-only secrets, `PUT` unknown id returns `404`). |
| `README.md` > Features | Verified | The "automatic retry queue" claim and the "Notification Retry Queue" section were already removed by the engineer. The AC-054 test passes and no `retry queue`, `retried automatically` or `automatic retry` text remains. |
| `CHANGELOG.md` | Updated | HARDEN-002 entries rewritten under `[Unreleased]`. Security: pprof removed from the main listener (BREAKING), security headers (BREAKING for iframe embedding), write-only secrets (BREAKING for API clients that read secrets), constant-time registration key. Removed: retry queue. Added: `RIOT_PPROF_ADDR` and `/readyz`. Changed: `/health` returns `503` when degraded (BREAKING for health checkers), `PUT` unknown channel returns `404`. The engineer's separate `### Changed` heading was merged into the existing one, so this story adds no duplicate heading. |
| `.env.example` | Updated | `RIOT_PPROF_ADDR` comment now says the port must differ from `RIOT_PORT` and that the compose files do not forward the variable. |
| `internal/server/server.go` | Inline comment | Added the "why" for stopping pprof first and only logging its shutdown error (a slow profile download must not block the main shutdown). |

Other changed files already carried accurate "why" comments referencing the ADD (`pprof.go`, `security_headers.go`, `health.go`, `secrets.go`, `router.go`, `handlers.go`, `settings.go`, `dispatcher.go`, `clientip.go`, `SecretInput.tsx`). No comment was restating code, so none was deleted.

### Verification performed

- `go test -count=1 ./internal/server/... -run 'AC064|AC054|AC055'` passes. The AC-064 test checks ten README terms, six CHANGELOG topics and the `.env.example` entry.
- Full `go test ./...` and `cd web && npm run test:run` results are in the commit hand-off message.
- Every behaviour documented was read from the code: `pprof.go` (`ParsePprofAddr`, `startPprof`), `security_headers.go`, `clientip.go` (`IsHTTPS`), `health.go`, `secrets.go` (`RedactChannel`, `MergeSecrets`), `settings.go`, `SecretInput.tsx`, `doctor.go`.
- The `docker exec ... wget` commands were not run against a live container. They rely on BusyBox `wget` in the `alpine:3.19` runtime stage of the `Dockerfile`, which is a documented property of the image, not a tested path.

---

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` Server Environment Variables, `RIOT_API_KEY` row | The row says that if set, agents must present the key to register. The server copies the variable into `Config.RegistrationKey`, but `RegisterDevice` reads only the database setting, so the variable never gates registration (ADD follow-up F-3). | Fix the code or correct the row, in a story that owns the registration behaviour. Not changed here because the FRD froze `RegisterDevice` behaviour. |
| `README.md` "Docker Compose" > `.env.example` code block | The block is a stale copy of the file (three variables, no OIDC, trusted-proxy or pprof entries). It duplicates `.env.example` and will drift again. | Replace the block with a link to `.env.example`. |
| `README.md` line 12 | Stability disclaimer refers to v2.24. Flagged in earlier reports. | Replace with a current stability statement. |
| `README.md` line 18 | The marker "README last updated for v2.38.0" is far behind the current release. | Update on every README change, or remove it. |
| `README.md` Building from Source | Says "Go 1.24+ and Node.js 20+". `go.mod` and CI use Go 1.24; CI and the release workflow use Node 22 while the `Dockerfile` builds with Node 20. | Pin the exact versions used by CI and the `Dockerfile`, as the README template requires. |
| `README.md` | No dedicated Requirements or Running Tests section with single-test and coverage commands (only a Testing section with `make` targets). | Add the sections in a dedicated documentation pass. |
| `CHANGELOG.md` | `### Changed` appears twice under `[Unreleased]`. The duplicate pre-dates this story. | Merge the two `### Changed` blocks in a dedicated pass. |
| `docker-compose.yml`, `docker-compose.prod.yml` | `RIOT_PPROF_ADDR` is not listed, unlike `RIOT_TRUSTED_PROXIES`, which has a commented example. Users must edit the environment list by hand (documented in the README). | Add a commented example line to both files (config change, not made by the writer). |
| Project memory note "Memory optimization lessons" (maintainer notes, outside this repo) | Says "pprof at /debug/pprof". That path now returns `404` on the main port. | Update the note: pprof is off by default. When enabled with `RIOT_PPROF_ADDR`, the ops path is `docker exec riot-server wget -qO- http://127.0.0.1:<port>/debug/pprof/...`. |
| `README.md` Single Sign-On step 2 | Named a maintainer-specific tooling set and a host name. Fixed here under the public-repository rule: the sentence now says "a script that registers OIDC applications". The `register_oidc_app.py` example itself is private tooling that is not in this repository. | Replace the example with generic identity-provider instructions in a dedicated pass. |
| `docs/implementation/HARDEN-002-impl-report.md`, `docs/qa/HARDEN-002-qa-report.md`, and other HARDEN-002 documents under `docs/` | They contain maintainer deployment specifics (a host name and live-environment checks). The repository is public. The orchestrator owns these files and was editing them during this pass, so the writer did not touch them. | Confirm the orchestrator's scrub covers them before pushing. |

---

## Accuracy Flags

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| The engineer's CHANGELOG said `/health` and `/readyz` answer "within about 3 seconds". The code bounds each probe at `DefaultHealthProbeTimeout` = 2 seconds. | `internal/server/handlers/health.go` | "about 2 seconds" in README and CHANGELOG. |
| The `.env.example` comment implied the variable works from `.env`. The compose files pass through only the variables listed in their `environment:` blocks, and `RIOT_PPROF_ADDR` is not listed. | `docker-compose.yml`, `docker-compose.prod.yml`, `.env.example` | README, CHANGELOG and `.env.example` tell the user to add the variable to the `riot-server` `environment:` list. |
| The README previously said the pprof listener rejects "non-canonical ports" only. After QA-1 it also rejects a port equal to `RIOT_PORT` (reason `port collides with the main server port`). This is a post-QA fix, so the original ADD design does not include it. | `internal/server/pprof.go` `startPprof` | Documented as a rule in the pprof table and in the CHANGELOG. |
| QA-2 fix: a webhook `PUT` that sends the bare placeholder as `headers` keeps the stored headers. The ADD says only that a missing or null `headers` keeps them. | `internal/server/notify/secrets.go` `mergeWebhookHeaders` | Documented in code only. The README says the `headers` object you send is the complete set. |
| `riot-agent doctor` treats a `503` from `/health` as a warning (`Health endpoint returned 503`), not a pass. | `internal/agent/doctor.go` | CHANGELOG states this. |
| QA-3: a bracketed IPv6 `Host` produces `ws://[::1]:7331` in `connect-src`, which some browsers may drop with a console warning. `'self'` still covers same-origin requests. | `internal/server/middleware/security_headers.go` | README lists the allowed host characters (including `[` and `]`). The warning is not documented because it has no user-visible effect. |
| QA-4: any `GetChannel` error on a channel `PUT`, including a database outage, returns `404`, not `500`. | `internal/server/handlers/settings.go` | CHANGELOG states it. |

---

## Notes for Future Writers

**Public repository.** The repository is public. Describe behaviour generically. Use `192.0.2.x` (documentation range) or `riot.example.com` in examples, and never record anything about a specific deployment's configuration or state.

**`README.md` is long and anchor-linked.** CHANGELOG entries link to README anchors (`#profiling-pprof`, `#security-headers`, `#notification-channels`). If you rename these headings, update the CHANGELOG links.

**A test pins README and CHANGELOG wording.** `internal/server/docs_test.go` fails if the README drops any of: `RIOT_PPROF_ADDR`, `docker exec`, `/readyz`, `503`, `write-only`, `frame-ancestors`, `form-action`, `Strict-Transport-Security`, `every port`, `edit channels`. It also fails if the README contains `retry queue`, `retried automatically` or `automatic retry`, or if the CHANGELOG lacks `[HARDEN-002]` and any of `pprof`, `security headers`, `write-only`, `constant time`, `/readyz`, `retry queue`. Re-run `go test ./internal/server/... -run AC064` after every README edit.

**The placeholder text is a contract.** `[rIOt-redacted]` is defined in `internal/server/notify/secrets.go` and mirrored in `web/src/utils/secrets.ts`. A test keeps the two equal. Keep the README spelling identical.

**Secrets registry.** The list of redacted keys in the README table mirrors `secretKeys` in `secrets.go`. When a new channel type or secret key is added, update the registry, the README table, and the frontend `SECRET_KEYS`.

**Windows line endings.** The repository uses `core.autocrlf=true`. Edit with tools that preserve LF in the working tree, and do not rewrite whole files, or the diff shows every line changed.
