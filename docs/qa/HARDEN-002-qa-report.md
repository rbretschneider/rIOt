# QA Report

**Story ID:** HARDEN-002
**QA Engineer:** QA Agent (Model: Sonnet 5.5)
**Date:** 2026-10-17
**Verdict:** PASS WITH NOTES

One MEDIUM defect (QA-1, pprof port collision with the main port) is documented below. It is opt-in and operator-triggered, and it does not block the ship. It should be fixed before anyone sets `RIOT_PPROF_ADDR` on a Linux host. Every AC is covered, all tests are green, and conditions C1 to C6 are verified by test and by mutation.

---

## Test Run Summary

All numbers were produced independently on this machine (Windows, no `-race`), not copied from the implementation report.

- `go build ./...` clean. `go vet ./...` clean.
- `go test -count=1 -v ./...`: 19 packages `ok`, **874 top-level `--- PASS`, 0 `--- FAIL`, 2 `--- SKIP`**, 687 passing subtests. The 2 skips are the pre-existing platform-gated `TestAC004_SecurityCollector_UsesCounterNotJournalctl` and `TestAC005_SecurityCollector_CounterNotReady_ReportsZero`. 122 of the passing top-level tests are `TestHARDEN002_*`.
  - The implementation report counted 856 top-level tests. QA added 18, which gives 874.
- `cd web && npm run test:run`: **31 files, 460 tests pass** (the implementation had 30 files and 449 tests; QA added 1 file and 11 tests).
- `cd web && npx tsc --noEmit`: clean (exit 0).
- No new Go or npm dependency. `go.mod`, `go.sum`, `package.json`, Dockerfile, compose files, `db/*`, `doctor.go` and `cors.go` are untouched, per ADD §4.1.
- Flaky tests found: **none in this story.** With `-count=3`, the unrelated and pre-existing `internal/server/probes` test `TestExecuteDNS_ResolveLocalhost` fails (`LatencyMs` is 0 once localhost resolution is cached). It passes with `-count=1`. The story did not touch that package, so this is not a regression.
- Suspicious output: the `queryFn` stderr noise in `DeviceDetail.test.tsx` is pre-existing.

### ADD §13 mechanical checks

| Check | Result |
|---|---|
| `grep 'RequestScheme(r) == "https"' internal/` | 1 hit, the body of `IsHTTPS` itself (`clientip.go:175`). The four cookie sites use `IsHTTPS`. |
| `grep RetryQueue --include=*.go .` | 1 hit, the ADD-mandated test name `TestHARDEN002_AC055_RetryQueueRemoved`. No symbol remains. |
| `grep debug/pprof router.go` | only the two explicit 404 routes (`router.go:217-218`) |
| `DefaultServeMux`, `http.ListenAndServe`, `http.Serve(` in non-test code | none. Every `ListenAndServe*` is a method on `s.httpServer`, which has `Handler`. |
| `&http.Server{` literals | 3 (`pprof.go:144`, `server.go:202`, `server.go:410`), all with `Handler:` set |
| Named tests from ADD §8 | 60 of 61 present with the exact name. See Test Quality Finding 1 for the one deviation. |

---

## AC Coverage Audit

I read every test body, not just the names. Each AC was also checked by asking whether the test would fail if the AC were broken. I ran the mutations listed under "Mutation verification".

| AC ID | Status | Tests Covering It | Gap Description |
|-------|--------|-------------------|-----------------|
| AC-001 | ✅ COVERED | `router_test.go` `AC001_PprofNotServedOnMainRouter` (5 paths x GET, plus POST/PUT/DELETE), `AC001_C5_PprofPathVariantsServeNoProfile` (6 variants, `RawPath` set explicitly) | Asserts 404, no pprof markers and no SPA marker. |
| AC-002 | ✅ COVERED | `pprof_test.go` `AC002_PprofDisabledByDefault`; `config_test.go` `AC002_LoadConfig_PprofAddrDefaultsEmpty` | Also asserts nothing is logged and nil `Shutdown` is a no-op. |
| AC-003 | ✅ COVERED | `AC003_LoopbackAddrsAccepted`, `AC003_C5_BindAddrBuiltFromParsedValues` | Asserts the WARN record carries the exact bound address. |
| AC-004 | ✅ COVERED | `AC004_NonLoopbackAddrsRejected` (23 rows, including all six C5(c) rows), `AC004_BindFailureDoesNotAbort` | Pins reason string, log level and logged value per row. **See QA-1 for the reverse collision.** |
| AC-005 | ✅ COVERED | `AC005_LoopbackListenerServesPprof` | Real HTTP to the listener; asserts profile text. |
| AC-006 | ✅ COVERED | `router_test.go` `AC006_LoopbackEnabledMainRouterStill404` | First proves the loopback listener is serving, so the main-router 404 is meaningful. |
| AC-007 | ✅ COVERED | `AC007_PprofEnableLoggedAndShutdownCloses`; QA `AC007_ShutdownWithStreamingProfileReleasesPort`, `AC007_ShutdownIsIdempotent` | QA closed the gap on a streaming `/profile` request and a double `Shutdown`. |
| AC-008 | N/A | none | OQ-6 |
| AC-009 | ✅ COVERED | `AC009_BaselineHeadersOnSPA` (real router), `AC009_BaselineHeadersSetBeforeHandler` | |
| AC-010 | ✅ COVERED | `AC010_HeadersOnEveryResponseClass` (12 subtests over two harnesses), `AC010_CORSHeadersUnchangedByHeaderMiddleware` | Includes the CORS 204, the recovered panic 500, the 401s, the 503s and the pprof 404. Exact bodies are asserted, so they prove no behaviour change. |
| AC-011 | ✅ COVERED | `AC011_CSPDirectiveContent` | Asserts exact source lists, `form-action 'self'`, and no `unsafe-inline`, `unsafe-eval` or `upgrade-insecure-requests` in script policy. |
| AC-012 | ✅ COVERED | `AC012_CSPPermitsSameHostWebSockets`, `AC012_C6_HostInjectionFallsBackToSelf`; QA `AC012_CSPHostShapesThroughRouter` | C6 is verified (see below). QA added `<server>:7331`, `[::1]:7331`, a bare hostname, mixed case, a zoned IPv6 literal and three injection attempts through the real router. |
| AC-013 | ✅ COVERED | `middleware` and `router_test` `AC013_HSTSOnDirectTLS` | Exact `max-age=31536000`, no `includeSubDomains`, no `preload`. |
| AC-014 | ✅ COVERED | `router_test` `AC014_HSTSBehindTrustedProxyMatchesSecureCookie` (3 subtests), `middleware` `AC014_..MatchesIsHTTPS` | Asserts HSTS and the `Secure` cookie flag move together. |
| AC-015 | ✅ COVERED | `AC015_NoHSTSOnPlainHTTPOrUntrustedPeer` (middleware, 3 subtests, and router_test) | Includes a trusted peer saying `http` over TLS. |
| AC-016 | ✅ COVERED | `security_headers_oidc_test.go` `AC016_OIDCRedirectsUnaffectedByHeaders`, `AC016_OIDCFailureRedirectCarriesHeaders` | |
| AC-017 | ✅ COVERED | `AC017_WebSocketUpgradesThroughMiddleware` (real 101 on `/ws` and `/ws/agent`, with a hub broadcast and a ping/pong exchange), `AC017_SecurityHeadersDoNotWrapWriter` | The handshake is real, not mocked. |
| AC-018 | ✅ COVERED (gap closed by QA) | `router_test` `AC018_StaticAssetContentTypes`; QA `AC018_StaticContentTypesAllExtensions` | The shipped test missed `.svg`, `.woff`, `.ttf`, `.png`, `.webp`, `.json`, `.map`, `.mjs` and the missing-asset SPA fallback. QA added them (13 subtests). |
| AC-019 | ✅ COVERED | `AC019_SMTPPasswordRedactedOnList`, `AC019_C1_URLUserinfoRedactedOnList` (+ pure); QA `AC019_ListNeverMutatesStoredSecrets` | The mock repository shares maps. QA proved a repeated list does not corrupt stored state. |
| AC-020 | ✅ COVERED | `AC020_EveryRegistryTypeRedacted` (8 types, 11 canaries scanned in the raw body) | |
| AC-021 | ✅ COVERED | `AC021_WebhookURLAndHeaderValuesRedacted` (+ pure, + non-map headers) | |
| AC-022 | ✅ COVERED | `AC022_UnsetSecretsNotSentinel` (absent and `""`) | |
| AC-023 | ✅ COVERED | `AC023_UnknownTypeFullyRedacted` | |
| AC-024 | ✅ COVERED | `AC024_CreateStoresCleartextReturnsRedacted`, `AC024_C1_CreateAndUpdateResponsesRedactUserinfo`; QA header test | The repository receives cleartext and the response is redacted. |
| AC-025 | ✅ COVERED | `AC025_PutSentinelKeepsSecret`, `AC025_C1_NameOnlyPutPreservesUserinfo` | The C1 test feeds back exactly what the list returned. |
| AC-026 | ✅ COVERED | `AC026_PutOmittedKeepsSecret` | |
| AC-027 | ✅ COVERED | `AC027_PutNewValueReplaces` | |
| AC-028 | ✅ COVERED | `AC028_PutEmptyClears` | |
| AC-029 | ✅ COVERED | `AC029_SentinelNeverPersisted` (a/b/c), `AC029_C1_UserinfoSentinelNeverPersisted` (a/b/c); QA `AC029_SentinelForNeverSetFieldEveryType` (9 types), `AC029_TypeChangeInheritsNothing` (3 scenarios) | QA widened the never-set and type-change cases to every type. |
| AC-030 | ✅ COVERED | `AC030_WebhookHeaderMerge` (a/b); QA `AC030_WebhookHeaderEdgeCases` | QA added null headers, empty map, a never-stored name with the sentinel, non-map headers, and the create response. |
| AC-031 | ✅ COVERED | `AC031_NonSecretKeysReplaced` | |
| AC-032 | ✅ COVERED | `AC032_PutUnknownChannel404`, `AC032_PutRepositoryErrorOnLookupIs404WithoutWrite`; QA `AC032_UnknownAndMalformedIDs`, `AC032_MalformedBodyNeverWrites` | `UpdateChannelCalls == 0` is asserted. QA added ids `0`, `-1`, `2`, int64 max (404) and a non-numeric or overflowing id (400). |
| AC-033 | ✅ COVERED | `AC033_TestSendUsesStoredSecretAfterRedactedEdit` | A capturing transport proves the stored URL is the one sent. |
| AC-034 | ✅ COVERED | `AC034_TestSendErrorScrubbed`, `AC034_C2_TestSendRealClientErrorsScrubbed` (6 backends); QA `C2_*` (see below) | |
| AC-035 | ✅ COVERED | `dispatcher_test` `AC035_DispatchFailureLogsScrubbed`, `scrub_client_test` `AC035_C2_RealClientErrorsScrubbed` (6 backends x Dispatch, SendToChannel and TestChannel, plus a precondition subtest proving the raw error really leaks), `C2_Pure_ScrubSecretsMatchesStdlibForms` | Checks the return value, `ErrorMsg` and captured slog. |
| AC-036 | ✅ COVERED | VT `[AC-036]` x3; QA VT `[AC-036]` x10 | QA added name-only edits for all 8 types. |
| AC-037 | ✅ COVERED | VT `[AC-037]` x5 plus the C3 attribute tests; QA VT type-change test | |
| AC-038 | ✅ COVERED | VT `[AC-038]` Clear/Undo/SMTP Clear/no Clear elsewhere | |
| AC-039 | ✅ COVERED | VT `[AC-039] Test calls testNotificationChannel with the id only` | |
| AC-040 | ✅ COVERED | VT `[AC-040]` (`demo-settings.test.ts`, `secrets.test.ts`) | |
| AC-041 | ✅ COVERED | `AC041_CorrectKeyRegisters` | |
| AC-042 | ✅ COVERED | `AC042_WrongKeysRejectedIdentically` (6 shapes) | Byte-identical 401 body; nothing registered. |
| AC-043 | ✅ COVERED | `AC043_OpenRegistrationUnchanged` | No key configured: open, and an irrelevant header is ignored. |
| AC-044 | ✅ COVERED | `AC044_RegistrationKeyMatchesTruthTable`, `AC044_UsesConstantTimePrimitive` (source scan) | |
| AC-045 | ✅ COVERED | `AC045_HealthOK` | |
| AC-046 | ✅ COVERED | `AC046_HealthDegradedReturns503`, `AC046_HealthNilDBCountsAsDown` | |
| AC-047 | ✅ COVERED | `AC047_ReadyzReady` | |
| AC-048 | ✅ COVERED | `AC048_ReadyzDBDown503` | |
| AC-049 | ✅ COVERED | `AC049_ReadyzMigrationsIncomplete503` (false, nil flag, and `/health` ignoring the flag) | |
| AC-050 | ✅ COVERED | `router_test` `AC050_HealthEndpointsPublicAndSetupIndependent` (setup true and false), `setup_test` `AC050_SetupGuard_ReadyzPassesInSetupMode` | |
| AC-051 | ✅ COVERED (gap closed by QA) | `AC051_HungProbeBounded` (50 ms override, checker honouring and ignoring its context), `AC051_DefaultProbeTimeoutIsTwoSeconds`; QA `AC051_RealDefaultTimeoutBoundsHungPing`, `AC051_CancelledRequestContextReturnsPromptly` | The shipped test used a 50 ms override and a deadline-only check. QA added the real 2 s bound end to end on both endpoints (4.0 s), and a client disconnect. |
| AC-052 | ✅ COVERED | `AC052_HealthBodiesLeakNothing` | |
| AC-053 | ✅ COVERED | `doctor_test` `AC053_DoctorServerCheckHandles503` | |
| AC-054 | ✅ COVERED | `docs_test` `AC054_ReadmeHasNoRetryClaim` | |
| AC-055 | ✅ COVERED | `AC055_RetryQueueRemoved` | |
| AC-056 to AC-061 | N/A | none | OQ-1 |
| AC-062 | ✅ COVERED | existing agent-route suites, unmodified | `git diff` shows no edits to existing agent tests. The only addition is the new doctor test. |
| AC-063 | ✅ COVERED | full suites green (above) | |
| AC-064 | ✅ COVERED | `docs_test` `AC064_DocsUpdated` | I read the README and CHANGELOG text. It carries the C4 limits ("not against anyone who can edit channels", "not secrets at rest") and the C7 HSTS caveat ("every port", non-bypassable cert errors). |
| C5(b) | ✅ COVERED | `invariants_test` `C5_NoNilHandlerOrDefaultServeMux` + `C5_ScanDetectsViolations` (scanner self-test) | I confirmed the same scan by hand (table above). |

### Security conditions

| Cond. | Status | Verification |
|---|---|---|
| **C1** URL userinfo redaction and write-only merge | ✅ VERIFIED | The list, create and update responses contain no `S3cret`, `Pw2`, `alice:` or `bob:`. `server_url` becomes `https://[rIOt-redacted]@host`. A name-only PUT that echoes the list preserves the stored credential. The sentinel userinfo is never persisted on update-with-none-stored, create, or type change. Mutation M3 (userinfo not redacted) fails 6 tests. Mutation M5 and M11 (merge disabled) fail 13. |
| **C2** scrubbing through a real `http.Client` | ✅ VERIFIED | Six backends x (Dispatch, SendToChannel, TestChannel) check the return value, `ErrorMsg` and slog, with a precondition proving the leak is real. The `user:***@` and `%q` forms are in `ScrubSecrets`. QA added special-character userinfo, awkward topics, a `parse "..."` error, remote body echo and a query-string credential. Mutations M1 (no scrub), M8 (no `stripPassword` variant), M9 (no `%q` variant) and M10 (dispatcher not scrubbed) are each caught. |
| **C3** autofill opt-out | ✅ VERIFIED by test, manual check pending | The attribute assertions cover smtp, ntfy, telegram, pushover and gotify: `new-password`, `data-1p-ignore`, `data-lpignore`, `data-bwignore`, `data-form-type`, the `riot-secret-` name, and no `<form>` ancestor. Mutation (autoComplete set to `on`) fails 5 tests. The Chrome saved-login check is manual and remains an orchestrator post-deploy item. |
| **C4** README and ADD text | ✅ VERIFIED | README and CHANGELOG read; limits are stated. |
| **C5** pprof invariants | ✅ VERIFIED (a, b, c, d) | (a) six variants return no profile content. (b) the scan and my manual grep agree. (c) all six rows are pinned with reasons. (d) the bound address equals `net.JoinHostPort(parsedIP, parsedPort)`. `[::ffff:127.0.0.1]` is rejected because the code uses `a.Is4() && a.IsLoopback()` and not `a.IsLoopback()`. |
| **C6** CSP Host injection | ✅ VERIFIED | The byte table covers `;` `,` `'` `*` `(` `)` `!` `$` `&` `+` `=` `%`, space, `"` `\` `/` `<`, empty, a zoned IPv6 literal, 263 bytes (falls back) and 262 bytes (accepted). Every case yields exactly `connect-src 'self'`. Mutations M2 and M2b (validation weakened) are caught. |
| **C7** `RIOT_TRUSTED_PROXIES` unset, HSTS caveat | ⏳ DEPLOY-TIME | The README caveat is present. The pre-deploy check on the deployed server belongs to the orchestrator. |

### Mutation verification (implementation temporarily broken, then restored with `git checkout`; `git status` confirmed clean)

| Mutation | Result |
|---|---|
| `scrubError` returns the unscrubbed text | 8 top-level failures |
| `stripPassword` variant removed | 3 failures |
| `%q` variant removed | 2 failures |
| Dispatch `Send` not wrapped by `scrubError` | 2 failures |
| Userinfo redaction disabled | 6 failures |
| Sentinel no longer keeps the stored value | 13 failures |
| `MergeSecrets` not called on PUT | 13 failures |
| PUT 404 branch removed | 3 failures |
| CSP host validation weakened | 1 failure each, 2 variants |
| HSTS always on | 4 failures |
| `/health` degraded returns 200 | 5 failures |
| Frontend: erased secret submits `""` | 1 failure |
| Frontend: autofill attributes removed | 5 failures |

---

## Test Quality Findings

1. **Test-name deviation from ADD §8 (LOW).** ADD §8 names `TestHARDEN002_C2_ScrubSecretsMatchesStdlibForms`. The implemented test is `TestHARDEN002_C2_Pure_ScrubSecretsMatchesStdlibForms` (`secrets_test.go`). The behaviour asserted is exactly what the ADD describes. Only the name differs, and the implementation report acknowledges the `Pure` spelling. Not worth a re-spin. The 60 other named tests match exactly.
2. **Shipped AC-018 and AC-051 had boundary gaps (closed by QA).** The AC-018 test omitted `.svg` and the other fonts and images. The AC-051 test only used a 50 ms override. Both are now covered.
3. **Test isolation (INFO).** `scrub_client_test.go`, `dispatcher_test.go` and one QA test swap `slog.Default()` and must not use `t.Parallel()`. The implementation report notes this. No test depends on execution order, and the new packages pass with `-count=3`.
4. **Port race in `AC003_C5_BindAddrBuiltFromParsedValues` (INFO).** It picks a free port, closes it, then rebinds. The race window is tiny, and it did not flake in 3 runs.
5. **Behaviour tests, not implementation tests.** The shipped tests assert on responses, stored state, headers and bytes. The only source scans are the intended ones (`AC044_UsesConstantTimePrimitive`, `AC055`, `AC064`, `C5`).

---

## Adversarial Findings

| # | Severity | Finding | Test written? |
|---|---|---|---|
| **QA-1** | **MEDIUM** | **pprof listener can take the main port and crash the server on Linux.** `server.go:222` starts pprof before the main listener binds. The main listener binds later, in a goroutine at `server.go:227-253`. If an operator sets `RIOT_PPROF_ADDR=127.0.0.1:7331`, the same port as `RIOT_PORT`, pprof binds `127.0.0.1:7331` first. The main `ListenAndServe(":7331")` then fails with `EADDRINUSE` and runs `os.Exit(1)` (`server.go:243` and `:253`). I reproduced this in a `golang:1.24` container: a wildcard `net.Listen(":P")` after a `127.0.0.1:P` listen returns `bind: address already in use`. On Windows the same sequence succeeds, so the local test suite cannot see it. Impact: a crash loop under `restart: unless-stopped` until the variable is unset, and the error line names the main listener, not pprof. It is opt-in, default off, and an obvious operator mistake, so it is not an exposure. It does contradict the intent of FR-005, FR-006 and AC-004 that a pprof misconfiguration must never keep the main server from starting. The ADD-mandated ordering (AD-002) created the hazard. | No. A correct red test cannot exist before the fix API is chosen, and the OS-level collision is Linux-only. Repro is above. |
| QA-2 | LOW | A webhook PUT that submits `headers` as the **sentinel string** (a non-map) while the stored headers are a map is swept to `""`, so the stored header map is wiped silently. The UI never sends this. The server only returns a sentinel string for headers when the stored value is already non-map. No secret leaks and the sentinel is never persisted. | Partly. QA `AC030_WebhookHeaderEdgeCases` asserts no sentinel persists. |
| QA-3 | INFO | A bracketed IPv6 Host (`[::1]:7331`) produces `ws://[::1]:7331` in `connect-src`. The CSP source grammar has no IPv6 host form, so browsers may drop that source with a console warning. `'self'` still covers same-origin requests, so there is no breakage and no widening. | Yes, `AC012_CSPHostShapesThroughRouter`. |
| QA-4 | INFO | A `GetChannel` error of any kind on PUT, including a real DB outage, returns 404 and not 500. This matches ADD §9 and ADD AC-032 and fails closed with no write. It is only operationally misleading. | Existing. |
| QA-5 | INFO | `/readyz` `migrations` is effectively always `true` while serving. The HTTP listener only starts after the migration block, so the flag cannot be false for a served request. The behaviour is correct and matches the spec, and the flag is a harmless future-proofing hook. | n/a |
| QA-6 | INFO | `RegisterDevice` still discards the `GetConfig` error, so a DB error reads as "no key" and registration fails open. This is pre-existing P-2 and out of scope for this story. | n/a |
| QA-7 | INFO | Destination rebinding by an actor who can edit channels (a host change with a kept userinfo credential) remains. The ADD documents it as follow-up F-7, and the README states the limit. | n/a |
| QA-8 | INFO | Probed with no finding: untrusted `X-Forwarded-Proto` spoof (HSTS and `Secure` stay off); CSP directive injection through `Host`; `//debug/pprof`, `/debug%2Fpprof` and dot-segment variants (no profile content); malformed or overflowing channel ids; double `Shutdown`; a listener left bound after a deadline-expired shutdown (released by the `Close` fallback); a cancelled client context on `/health`. | Yes (see below). |

---

## Tests Added by QA

Commit `ec48b86`. All are marked "Added by QA Engineer" and are green.

| File | Tests | Covers |
|---|---|---|
| `internal/server/notify/qa_scrub_test.go` | 6 (`SpecialCharUserinfoScrubbed`, `AwkwardNtfyTopicScrubbed` x4, `CreateRequestParseErrorScrubbed`, `RemoteBodyEchoScrubbed` x2, `QueryStringCredentialScrubbed`, `ScrubbedErrorHasNoUnwrapChain`) | AC-034, AC-035, C2 |
| `internal/server/handlers/qa_settings_secrets_test.go` | 8 (`AC029_SentinelForNeverSetFieldEveryType` x9 types, `AC029_TypeChangeInheritsNothing` x3, `AC030_WebhookHeaderEdgeCases` x5, `AC032_UnknownAndMalformedIDs`, `AC032_MalformedBodyNeverWrites`, `AC019_ListNeverMutatesStoredSecrets`, `AC051_RealDefaultTimeoutBoundsHungPing`, `AC051_CancelledRequestContextReturnsPromptly`) | AC-019, AC-029, AC-030, AC-032, AC-051, C1 |
| `internal/server/router_qa_test.go` | 4 (`AC012_CSPHostShapesThroughRouter` x8, `AC018_StaticContentTypesAllExtensions` x14, `AC007_ShutdownWithStreamingProfileReleasesPort`, `AC007_ShutdownIsIdempotent`) | AC-012, AC-018, AC-007, C6 |
| `web/src/pages/settings/NotificationSettings.qa.test.tsx` | 11 vitest (name-only edit for 8 types, webhook header round trip, unset optional secret, type change) | AC-036, AC-037, C1 |

Not given a new test: the "registration key compare when no key is configured" probe. Shipped `AC043_OpenRegistrationUnchanged` already asserts it, including an arbitrary header sent when no key is configured.

---

## Deviations from ADD

Impl-report deviations checked:

1. **Single-pass `strings.NewReplacer` in `ScrubSecrets`.** Correct. Variants are sorted longest-first and `Replacer` honours argument order at a position, so longer matches win. Replaced text is never rescanned, so a short secret cannot corrupt the sentinel. Pinned by `C2_Pure_ScrubDoesNotCorruptOnShortOrOverlappingSecrets`. Accepted.
2. **`pprofServer.Shutdown` falls back to `Close()` on error.** Correct, and QA proved it with a streaming 10 s profile. Shutdown returned in 0.7 s and the port was released. Accepted.
3. **`deepCopyConfig` also deep-copies `[]interface{}`.** Harmless and safer. Accepted.
4. **`ParsePprofAddr` reason mapping for edge inputs.** All reasons are in the ADD's list (line 314). Accepted.
5. **README and CHANGELOG written by the engineer.** Accurate, and the AC-064 terms and C4/C7 text are present. Accepted. The technical writer may still polish.
6. **ADD §13 greps are not literally empty.** Explained and verified above. Accepted.
7. **`MockNotifyRepo.UpdateChannelCalls` increments before the `Err` check.** The stricter reading of "UpdateChannel was not called". Accepted.

Other deviations found by QA:
- ADD AD-002 requires pprof to start after `s.httpServer` is constructed and before the main listener binds. That ordering is the cause of QA-1. The implementation follows the ADD.
- Test name `C2_Pure_ScrubSecretsMatchesStdlibForms` differs from ADD §8 (Finding 1).

## Deviations from FRD

None that violate an AC. QA-1 conflicts with the intent of FR-006 ("must not prevent the main server from starting"), though the FRD's literal wording covers only a pprof bind failure. Treated as a note, not a fail.

---

## Verdict Rationale

**PASS WITH NOTES.**

- Every in-scope AC has a test that would fail if the AC were violated. I confirmed this by mutating 13 behaviours in the implementation and watching the tests fail.
- All tests are green: Go 874 passing, 0 failing, 2 pre-existing skips; vitest 460 passing; `go vet`, `go build` and `tsc --noEmit` clean.
- Security conditions C1 to C6 are verified. C3's manual browser check and C7's pre-deploy check remain with the orchestrator.
- QA found and closed gaps in AC-007, AC-012, AC-018, AC-029, AC-030, AC-032, AC-051 and C2 by adding 18 Go top-level tests and 11 vitest tests.
- QA-1 is a MEDIUM defect. It does not break any AC or expose anything, but it can crash the server on Linux when an operator mis-sets the opt-in pprof address. It should be fixed before `RIOT_PPROF_ADDR` is ever used in production.

## Action Required (follow-up, non-blocking)

1. **QA-1, `internal/server/server.go:222` (`startPprof`) and `internal/server/pprof.go` (`startPprof`/`ParsePprofAddr`).** Make a pprof address that uses the main listener's port unable to stop the main server. Either of these works:
   - Reject, with an ERROR log, any `RIOT_PPROF_ADDR` whose port equals `s.Config.Port`.
   - Start pprof only after the main listener is bound, so a collision falls on the pprof side and is logged as "pprof listener bind failed; pprof disabled".
   Add a Linux-valid test, for example by occupying `127.0.0.1:P` with `startPprof` and asserting the main bind on `:P` is never attempted, or by asserting the rejection log. Mention the same-port rule in the README Profiling section.
2. **QA-2 (optional).** Treat a sentinel-valued non-map `headers` as "keep stored" in `MergeSecrets`/`mergeWebhookHeaders` (`notify/secrets.go`) instead of letting the final sweep blank it.
3. **Orchestrator, pre-deploy (C7).** Confirm `RIOT_TRUSTED_PROXIES` is unset on the deployed server.
4. **Orchestrator, post-deploy.** Run the ADD §13 curl and devtools CSP checks, and the C3 manual Chrome saved-login check. Also confirm the production `npm run build` bundle contains only external `/assets/*` scripts (CSP `script-src 'self'`).
