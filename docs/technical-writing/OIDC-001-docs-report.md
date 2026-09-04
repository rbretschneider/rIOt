# Documentation Report

**Story ID:** OIDC-001
**Author:** Technical Writer Agent
**Date:** 2026-09-04

---

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | Added a "Single Sign-On (OIDC)" section (placed between mTLS Device Authentication and DNS Resilience) covering: what the feature does and does not do; the delegated-authorization model with the required-group-binding warning stated first; the enable-SSO runbook in order (bind group → register app with `--group`/`--redirect-uri`/`--launch-url` → strict redirect matching → verify with a non-member test account → set env vars); the four `RIOT_OIDC_*` variables; `RIOT_TRUSTED_PROXIES` with the `Secure`-cookie consequence stated before the rate-limit/audit-IP consequence; and the first-seen-identity `WARN` in Settings → Logs. Added a "Optional SSO login" bullet to the Features list. Added the three `RIOT_OIDC_*`/`RIOT_TRUSTED_PROXIES` rows to the Server Environment Variables table (linking to the new section). Added the three new endpoints (`GET /api/v1/auth/oidc`, `/oidc/start`, `/oidc/callback`) to the Public API table. |
| `CHANGELOG.md` | Created new `[Unreleased]` entries | Added `[OIDC-001]` entries under `### Added` (feature description, endpoints, `external_identities` audit table + delegated-authorization model, first-seen `WARN`, `RIOT_TRUSTED_PROXIES`), a new `### Fixed` entry (the pre-existing spoofable-`X-Forwarded-For` rate-limit bypass that this story's client-IP rework closes), and `### Changed` entries (the `riot_session` `SameSite=Lax`+scheme-derived `Secure` change, and the client-IP-derivation change with its explicit operator-facing consequence for reverse-proxy deployments). `CHANGELOG.md` already existed at the repo root (Keep a Changelog format) — no file creation was needed. |

## Artifacts Reviewed, Not Changed

| File | Reason |
|------|--------|
| `internal/server/handlers/oidc.go` | Task instructions explicitly scoped this file (and `.env.example`) to the senior dev's concurrent one-line `context.WithoutCancel` fix, with instructions not to collide with its content beyond reading it. Read in full — the fix is already landed (the `recordExternalIdentity` doc comment now correctly describes `context.WithoutCancel(reqCtx)`, matching AD-015 literally and closing the QA report's disclosed-deviation note). Not edited. See "Findings not fixed" below for one comment gap in this file that could not be addressed as a result. |
| `.env.example` | Same concurrent-edit exclusion. Read in full — the `RIOT_TRUSTED_PROXIES` block already states the `Secure`-attribute consequence **before** the rate-limit/audit-IP consequence, matching security-review condition C4 and QA's requested reorder. Already fixed; left untouched per instructions. |
| `internal/server/oidc/*.go` (`config.go`, `service.go`, `transaction.go`, `identity.go`, `errors.go`) | Read in full for the inline-comment audit. All "why" comments called out by the ADD and security review (transaction MAC-before-decode ordering in `transaction.go`, key derivation in `DeriveTransactionKey`, the peer-fallback branch in `clientip.go`) are already present, correctly attributed to AD/SEC/FR numbers, and read as intended. No additions or deletions needed. |
| `internal/server/middleware/clientip.go`, `ratelimit.go`, `setup.go` | Same — the C5 all-trusted-XFF-chain fallback comment in `rightmostUntrusted`, the `MiddlewareRedirect` rationale, and the exact-path-match rationale in `SetupGuard` are all present and correct. No changes needed. |
| `internal/server/handlers/auth.go`, `internal/server/db/external_identity_repo.go`, `internal/server/router.go`, `internal/server/config.go` | Read in full. Cookie-helper, `RETURNING (xmax = 0)` upsert, and router-wiring comments are all present with correct AD references. No changes needed. |
| `web/src/pages/Login.tsx` | Read in full. The `sso_error` lookup table, the `useSearchParams`-vs-`history.replaceState` rationale, and the plain-anchor-not-fetch comment are all present. No changes needed. |
| `docker-compose.yml`, `docker-compose.prod.yml` | Read in full. Both already carry the commented `RIOT_OIDC_*`/`RIOT_TRUSTED_PROXIES` blocks with the "bind the group first" warning. No changes needed. |
| `CONTRIBUTING.md` | Does not exist in this repository (confirmed via glob) and this story introduces no new build step, test runner, migration workflow, or contribution convention that isn't already covered by the existing `make test` / `make migrate-up` documentation in the README and `CLAUDE.md`. Not created — consistent with the FLEET-DASH technical-writer pass, which made the same call for the same reason. |
| `docs/` reference documentation | The repository has no `docs/api.md` or equivalent — the README's API tables are the sole source of truth for endpoint reference (confirmed by glob of `docs/*.md`, which contains only pipeline artifacts per story). The three OIDC endpoints were added there instead of a separate file, per ADD §4.5's fallback instruction and the FLEET-DASH precedent. |

---

## Inline Comment Assessment

Every new/modified file named in ADD §4.1–§4.2 was read in full. The engineering team's comment discipline on this story is unusually strong — nearly every non-obvious decision already carries a comment referencing the correct AD/FR/SEC/AC number, and none of the comments found restate what the code obviously does. Specific items the task asked me to verify:

- **Transaction MAC ordering (SEC-004).** `internal/server/oidc/transaction.go`, `DecodeTransaction`'s doc comment states the five-step verification order as a numbered contract and explains *why* the MAC check runs before any decoding (cross-protocol confusion with a `riot_session` JWT). Present and correct — no addition needed.
- **Peer-fallback branch (C5).** `internal/server/middleware/clientip.go`, `rightmostUntrusted`'s doc comment explains the all-trusted-chain fallback and why the conservative answer (leave `RemoteAddr` as the TCP peer) was chosen over promoting an untrusted-by-assumption entry. Present and correct — no addition needed.
- **WARN log-level choice (AD-021).** **Not commented at its point of use.** The `slog.Warn("new SSO identity granted admin", ...)` call in `internal/server/handlers/oidc.go`'s `OIDCCallback` has no comment explaining *why* `WARN` (rather than `INFO`) was chosen — the reason (only `WARN`-and-above is persisted by `logstore.DBHandler` and surfaced in Settings → Logs, so `INFO` would be invisible) is recorded in ADD AD-021 but not restated in code. This is exactly the kind of business-rule comment this pass would normally add. It was not added because `oidc.go` is one of the two files the task explicitly excluded from edits (the senior dev's concurrent `context.WithoutCancel` fix lives in this same file). Logged below as a finding for the next writer or engineer who touches this file.

No comments were deleted — none found restating obvious code, none were dated developer notes, and no commented-out code blocks were found in any reviewed file.

---

## Stale Content Found (Not Fixed)

Items found outside the scope of this story that need a documentation pass:

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `docs/security/FLEET-DASH-security-review.md` line 17 | Describes `riot_session` as `HttpOnly` + `SameSite=Strict`. As of this story, the cookie is `SameSite=Lax` with a scheme-derived `Secure` attribute (AD-008). The OIDC-001 ADD itself flags this exact staleness in its §0 amendment 1 consequences note and explicitly says "this story does not edit that file." | Correct in a future documentation-only pass touching FLEET-DASH's artifacts, or accept as historical record of the review's own point in time — either is defensible, but a reader of that file today will be misled about the current cookie attributes. |
| `README.md` line 18 | `> **README last updated for v2.38.0**` — this version marker predates OIDC-001 and was already flagged as stale by the FLEET-DASH technical-writer pass (which recommended updating it at the next release tag or removing it). Still not updated; no release has been tagged since. | Update at the next `git tag` per the Releasing section's process, or remove the marker entirely and rely on `git log`/CHANGELOG for currency — this recommendation carries forward unchanged from the prior pass. |
| `internal/server/handlers/oidc.go` — `OIDCCallback`'s `slog.Warn("new SSO identity granted admin", ...)` call | No in-code comment states why `WARN` (not `INFO`) was chosen for the first-admission log line, even though every other non-obvious decision in this story's new files carries one. The rationale exists in ADD AD-021 but is not restated at the call site. | Add a short comment (e.g. "`WARN` specifically — `logstore.DBHandler` persists `WARN`-and-above to `server_logs`, so this is what makes the first admission actually visible in Settings → Logs; `INFO` would be silent (AD-021)") the next time this file is touched for an unrelated change. Not added in this pass because the file was explicitly excluded to avoid colliding with the senior dev's concurrent `context.WithoutCancel` fix. |
| `Makefile` `migrate-up` / `migrate-down` targets | Noted independently by both the implementation report and the QA report: these targets reference a `-migrate-up`/`-migrate-down` CLI flag and a `migrate` build tag that do not exist anywhere in `cmd/riot-server/*.go`. Migrations actually apply automatically on every server boot via `Server.Start()`. This is a pre-existing gap unrelated to OIDC-001. | Fix the `Makefile` targets (or document that migrations are boot-automatic and the targets are aspirational/unused) in a dedicated small story — this is outside OIDC-001's scope and was correctly not touched by engineering or QA. |

---

## Accuracy Flags

Discrepancies between the pipeline documents and what the code actually does, found while reading for this pass:

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| None found beyond what QA's own report already disclosed (the `oidcClock` field addition, the duplicated test-only stub IdP, and the now-resolved `context.Background()` vs `context.WithoutCancel(r.Context())` deviation). All three were independently re-verified against the current code during this pass and match the QA report's description exactly — the `context.WithoutCancel` deviation QA flagged is no longer present; the code and its comment now read `context.WithoutCancel(reqCtx)`. | `internal/server/handlers/oidc.go:212-220` | Not re-documented here as a new discrepancy — it is a QA-flagged item that has since been resolved by the senior dev's concurrent fix, confirmed by direct reading. |

---

## Notes for Future Writers

**README section placement.** The "Single Sign-On (OIDC)" section was placed between "mTLS Device Authentication" and "DNS Resilience" — both are security/authentication-adjacent major features with their own `##` section and subsections, matching the existing pattern (see FLEET-DASH's prior note on section placement, which this pass followed).

**Delegated-authorization warnings lead, not trail.** Per the ADD's explicit ordering requirement (security-review condition C4, and the analogous ordering the ADD demands for the group-binding requirement in §4.5), every place this feature's access-control model is explained states "any IdP-authorized identity gets admin" and "the group binding is your access control" *before* any mechanical setup instruction. Future writers extending this section should preserve that ordering — the security review treats ordering itself as a correctness property here, not just style.

**API table is still the only endpoint reference.** No `docs/api.md` exists in this repository; the README's `### Public` / `### Agent` / `### Dashboard` tables remain the sole source of truth for endpoint documentation, as FLEET-DASH's docs report already established. The three new OIDC endpoints were added to `### Public` (not `### Dashboard`) because none of them require the `riot_session` cookie — `/oidc` and `/oidc/start` are reachable pre-authentication by design, and `/oidc/callback` is the unauthenticated return leg from the IdP.

**Two files were intentionally left untouched in this pass.** `internal/server/handlers/oidc.go` and `.env.example` were excluded from all edits per the task's explicit instruction to avoid colliding with the senior dev's concurrent one-line fixes (a `context.WithoutCancel` correction and a `.env.example` comment reorder). Both were confirmed already correct on inspection. If a future pass needs to touch either file, the WARN-level comment gap noted above is the one outstanding item in `oidc.go`.

**CHANGELOG entry order.** Existing `[Unreleased]` blocks are not strictly chronological by FRD date (e.g. `FLEET-DASH`, dated 2026-04-27, precedes `PATCH-GATE`, dated 2026-08-25, in the file). The `[OIDC-001]` entries were placed at the very top of `### Added` (and a new `### Fixed` section was introduced before the existing `### Changed` section) since OIDC-001 (2026-09-04) is the most recent story to date — consistent with newest-entries-at-top, which is the pattern each prior story's technical-writer pass appears to have followed even though the block ordering below it doesn't strictly preserve global chronology.
