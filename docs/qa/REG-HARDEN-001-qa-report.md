# QA Report

**Story ID:** REG-HARDEN-001
**QA Engineer:** QA Agent
**Date:** 2026-10-18
**Verdict:** PASS WITH NOTES

Every number below was reproduced independently on this machine (Windows 11, Go go1.26.0, Node 24, Docker 26). CI pins Go 1.24 on Linux with `-race`; that run is not reproduced here.

---

## Test Run Summary

| Check | Result |
|---|---|
| `go build ./...` | clean |
| `go vet ./...` | clean |
| `go test -count=1 ./...` (no `-race`, needs CGO) | 20 packages `ok`, 0 failing |
| `go test -count=1 -v ./...`, baseline (before QA additions) | 808 top-level PASS / 0 FAIL / 2 SKIP; 1414 including subtests |
| `go test -count=1 -v ./...`, after QA additions, run twice | **821 top-level PASS / 0 FAIL / 4 SKIP; 1446 including subtests**, identical both runs |
| `cd web && npm run test:run` | 27 files, **411 passed**, 0 failed |
| `cd web && npx tsc --noEmit` | clean |
| New Go module / npm package | none (`go.mod`, `go.sum`, `package.json`, lockfile have an empty diff) |
| Schema migration | none (no `.sql` in the diff) |

- **Skips (4):** 2 are the pre-existing Linux-only security collector tests (`TestAC004/005_SecurityCollector_*`). 2 are QA's disposable-database tests (`TestTelemetryRepo_PGDisposable_*`, `TestDeviceRepo_PGDisposable_*`), which skip unless `RIOT_QA_DISPOSABLE_DB_URL` is set (see C9). They were executed separately, see below.
- **Flaky tests:** none. The full suite was run twice back to back with identical counts.
- **Suspicious output:** none. One observation: a registration test with a ~1 MiB hostname makes the pre-existing `device registered` INFO line enormous (see Adversarial Findings, ADV-3).
- **Coverage percentage:** not measured (no coverage tool in the house flow).
- **`-race`:** not run (Windows, no CGO). QA added a concurrent test of the clamp-log limiter that would crash the process on an unsynchronised map write even without the race detector. The CI `-race` job remains to be confirmed.

---

## Mutation Probes

Each probe temporarily broke production code, ran the relevant package tests, then restored the file byte for byte. `git status` was clean (apart from QA's new files) after every probe. 24 probes, all caught.

| # | Mutation | Failing tests | Result |
|---|---|---|---|
| M1 | Remove the reject early-return in `RegisterDevice` (`handlers.go`) | 9 (`AC003`, `AC004`, `AC005`, `AC007` x2, `AC011` x3 ...) | CAUGHT |
| M2 | `regKeyEqual` always true (`|| true`) | 16 (`AC018` x4, `AC014`, `AC016` x4, `AC017`, `C3`, `AC020` ...) | CAUGHT |
| M3 | Swap rule order: own-key accept before wrong-device-cert 403 | 4 (decision table PO-1 row, `AC012_CertForOtherDevice` "D1's own key") | CAUGHT |
| M4 | Drop the read guard from `GetLatestSnapshot` | 2 (`GuardPlacement`) static; plus 11 in the real-database run (PG1) | CAUGHT |
| M4b | Move the `GetGPUDeviceIDs` guard from the subquery to the outer `WHERE` | 2 static (`guard precedes DISTINCT ON ordering`); 2 in the real-database run (PG2, the fallback behaviour) | CAUGHT |
| M4c | SQL interval `5 minutes` -> `10 minutes` | 1 (`TestFutureSkewSQL_AC030_MatchesAllowance`); 4 in the real-database run (PG3) | CAUGHT |
| M6 | Clamp boundary widened by 1 minute | 5 (`AC028_Boundaries` x3, telemetry and heartbeat `AC028`) | CAUGHT |
| M6b | Clamp boundary made exclusive (`!Before`) | 2 (`R+5m exactly is kept`) | CAUGHT |
| M7 | Take identity from `PeerCertificates` when `VerifiedChains` is empty | 9 (`QualifyingCert_AC011` x3, `AC011_UnverifiedCertAndHeader`, `C3`, `AC020`) | CAUGHT |
| M8 | `DeviceKeyless` set regardless of `HasAPIKey` result | 8 (`CollectRegCredentials_Facts`, `AC007`, `AC014`, `C3`, `AC020`) | CAUGHT |
| M9 | Path (a) (own key) mints a key | 13 (`AC001`, `AC002`, `AC009`, `AC021`, `AC024` x2 ...) | CAUGHT |
| M10 | Heartbeat clamp removed | 3 (`AC029`, `AC028`, `AC031`) | CAUGHT |
| M11 | `loggableDeviceID` length cap removed | 4 (`C1_NoControlCharacters`, `AC020 C1 overlong`) | CAUGHT |
| M12 | New-device registration-key check removed | 12 (`AC014`, `AC016` x4, `AC017`, `C3`, `AC020` x4) | CAUGHT |
| M13 | Telemetry (snapshot) clamp removed | 4 (`AC025`, `AC026`, `AC028`, missing-timestamp default) | CAUGHT |
| M14 | Drop the `DeviceKeyless` requirement from the path (c) accept rule | 13 (decision table V-07 rows, `AC007` x2, `AC014`, `C3` ...) | CAUGHT |
| M15 | Remove `Math.max(0, ...)` from web `formatAgo` | 2 vitest cases (`[AC-035]` future and far-future) | CAUGHT |
| M16 | `HasAPIKey` error treated as keyless (fail open) | 7 | CAUGHT |
| M17 | `ListRevokedSerials` error ignored (fail open) | 6 (`AC013` x2, `C3`, `AC020`) | CAUGHT |
| M18 | An unresolved `X-rIOt-Key` blocks the certificate path | 11 (`AC008` x3 variants, `AC010`, decision table) | CAUGHT |
| M19 | Remove the `MaxBytesReader` body cap | 3 (`AC019 over 1 MiB`) | CAUGHT |
| PG1 | Guard replaced by `timestamp IS NOT NULL` (real PostgreSQL) | 11 | CAUGHT |
| PG2 | GPU guard moved to the outer `WHERE` (real PostgreSQL) | 2 | CAUGHT |
| PG3 | Interval widened to 10 minutes (real PostgreSQL) | 4 | CAUGHT |

Observation: the static `GuardPlacement` test pins text and ordering; it cannot prove the SQL behaves correctly. The behaviour is proven only by the real-database run (C9). With the gated test left in the repo, that run can be repeated on any disposable local database.

---

## AC Coverage Audit

Legend: COVERED = tests that would fail if the AC were broken (bodies read; mutation-confirmed where marked). DOC = documentation item owned by the technical writer. MANUAL = needs a released binary or a real host.

| AC ID | Status | Tests Covering It | Gap Description |
|-------|--------|-------------------|-----------------|
| AC-001 | COVERED | `register_device_test.go` `TestRegisterDevice_AC001_OwnKeyOpenServer_NoNewKey`; decision-table row; M9 | — |
| AC-002 | COVERED | `TestRegisterDevice_AC002_OwnKeyWithRegKeyConfigured_Accepted`; QA `TestRegisterDevice_QA_AC024_RealAgentClient_BugC_EndToEnd` (the real agent HTTP client against the real handler) | — |
| AC-003 | COVERED | `TestRegisterDevice_AC003_WrongDeviceKey_403NoSideEffects` (fixed body, no IDs or hostnames, nil hub/eventGen, deep compare); QA `QA_AC003_WrongDeviceKeyWithNamedDeviceCert_403`; M1, M3 | — |
| AC-004 | COVERED | `TestRegisterDevice_AC004_NoKeyNoRegKey_Rejected`; `C3_EveryRejectClass_NoSideEffects`; M1 | — |
| AC-005 | COVERED | `TestRegisterDevice_AC005_GarbageKey_NoMetadataOverwrite` (hostname, tags, hardware profile, status) | — |
| AC-006 | COVERED | `TestRegisterDevice_AC006_RegKeyKeylessDevice_IssuesKey` (issued key then authenticates telemetry through the real `DeviceAuth`); QA `QA_AC007_RegKeyBootstrapIsOneShot` | — |
| AC-007 | COVERED | `AC007_RegKeyDeviceHasKeys_401`, `AC007_HasAPIKeyError_FailsClosed`; QA `QA_AC007_RegKeyBootstrapIsOneShot` (replay after the first mint is 401); M8, M14, M16 | — |
| AC-008 | COVERED | `AC008_MTLSEnrolledDevice_CertIssuesKey` (3 key variants x with/without registration key); QA `QA_AC008_CertPlusRegKey_KeyedDevice_AddsKey` | — |
| AC-009 | COVERED | `AC009_CertPlusOwnKey_NoNewKey` | — |
| AC-010 | COVERED | `AC010_CertPath_KeepsPriorKeys` (both keys authenticate afterwards); M18 | — |
| AC-011 | COVERED | `AC011_RevokedCert_401`, `AC011_UnverifiedCertAndHeader_401` (4 subtests: no `VerifiedChains`, forwarded headers, nil `caRepo`, empty CN), `TestQualifyingCert_AC011_NoIdentityCases`, `TestCollectRegCredentials_AC011_IgnoresForwardedCertHeaders`; QA `QA_AC011_MTLSDisabled_CertIgnored_OwnKeyStillWorks`; M7 | — |
| AC-012 | COVERED | `AC012_CertForOtherDevice_403` (no key, garbage key, D1's own key); M3 | — |
| AC-013 | COVERED | `AC013_RevocationListError_FailsClosed`, `TestQualifyingCert_AC013_*`; M17 | — |
| AC-014 | COVERED | `AC014_Uniform401Body` (6 cases byte-identical in status, body and headers); QA `QA_AC014_ExistingVsUnknown_IdenticalWithTLSStateAndLongKey` (8 KiB header) | — |
| AC-015 | COVERED | `AC015_OpenNewDeviceRegistration_201`; QA `QA_AC015_HostileDeviceID_OpenServer` (NUL, newline, trailing space, upper-cased ID, 100 KiB, SQL metacharacters, unicode: all create a new device, victim untouched) | — |
| AC-016 | COVERED | `AC016_NewDeviceRegKey` (includes a prefix-of-key case); M2, M12 | — |
| AC-017 | COVERED | `AC017_DeviceKeyNotRegKeyForUnknownUUID`; M12 | — |
| AC-018 | COVERED | `TestRegKeyEqual_AC018_ConstantTimeCompare` (first-byte, last-byte, length, empty-configured rows); code inspection: `regKeyEqual` = sha256 of both sides + `subtle.ConstantTimeCompare` over 32 bytes; grep shows no `==`/`!=` of a presented value against the registration key (the two `regKey != ""` hits are the "is it configured" test); M2 | Timing itself is not measurable in a unit test; the primitive is inspected |
| AC-019 | COVERED | `AC019_MalformedBody_400NoSideEffects` (4 bodies x with/without registration key, including empty and over 1 MiB); QA `QA_AC019_BodySizeBoundary` (exactly 1 MiB accepted, 1 MiB + 1 byte is 400); M19 | — |
| AC-020 | COVERED | `AC020_RejectionLogsWithoutSecrets` (11 classes + 2 C1 cases: one WARN line, `device_id`, `client`, `class`, `status`, no secret); `AC020_SuccessPathsLogNoSecrets` (paths a, b, c, new-device 201); QA `QA_AC020_HostileDeviceID_RegKeyServer_OneCleanLogLine` (7 hostile IDs, each exactly one physical line) | — |
| AC-021 | COVERED | `AC021_RotateKeyThenOwnKeyReRegisters` (real `RotateKey` handler; old key no longer resolves) | — |
| AC-022 | COVERED | `AC022_NoDeviceID_RegistersAsNewDevice` (with/without registration key; original record unchanged) | — |
| AC-023 | DOC (pending) | none (documentation) | README recovery section and CHANGELOG entry are not in this branch. See Deviations from FRD and Action Required |
| AC-024 | COVERED (automated) / MANUAL (released binary) | `AC024_ReleasedAgentRequestShapes` (7 flows); QA `QA_AC024_RealAgentClient_BugC_EndToEnd` drives the real, unmodified `agent.HTTPClient.Register` over HTTP against the real handler: first install with registration key (201), periodic re-registration with own key while a registration key is configured (200, empty key), registration-key-only and empty-key reclaim of a keyed device (rejected, device unchanged) | A run of the actual released agent binary against a dev server was not performed (needs the install flow, a service account and a writable ID file path) |
| AC-025 | COVERED | `TestTelemetry_AC025_FutureSnapshotClampedAndAccepted`, `..._ClampedResponseIdenticalToUnclamped`, `..._MissingTimestampDefaultsToReceiveTime`; QA `QA_AC025_ExtremeFutureClampedWithoutOverflow` (year 9999); M13 | — |
| AC-026 | COVERED | `TestTelemetry_AC026_FutureSnapshotNeverLatest` (write half); QA real-database `PGDisposable_AC032_AC033_C9` (read half) | — |
| AC-027 | COVERED | `TestTelemetry_AC027_PastSnapshotStoredAsSent`; QA `QA_AC027_FarPastKept_ZeroDefaults` (year 2000) | — |
| AC-028 | COVERED | `TestClampFutureTimestamp_AC028_Boundaries` (R+4m, R+5m, R+5m+1ns, R+6m), telemetry and heartbeat `AC028` tests; QA `QA_AC028_ExactAllowanceBoundary` (handler level, both endpoints, exactly +5m kept and +5m+1ns clamped); M6, M6b | — |
| AC-029 | COVERED | `TestHeartbeat_AC029_FutureHeartbeatClamped`; M10 | — |
| AC-030 | COVERED | `TestHeartbeat_AC030_PastHeartbeatStoredAsSent`; QA far-past heartbeat | — |
| AC-031 | COVERED | `TestClampLogLimiter_AC031_*`, `TestClampLogLimiter_NFR009_SweepBounded`, `TestHeartbeat_AC031_ClampLogDebounced` (100 clamps -> 1 entry; next entry after the window reports 100); QA `ClampLogLimiter_QA_AC031_ConcurrentObserveEmitsOncePerWindow` (16 goroutines, exactly one emit, no lost count) | Snapshot-kind debounce is proven at the limiter level, heartbeat kind at handler level; both go through the same `noteClamp` |
| AC-032 | COVERED | Static `TestReadQueries_AC032_AC033_GuardPlacement` (8 queries); **real PostgreSQL**: QA `TestTelemetryRepo_PGDisposable_AC032_AC033_C9` (per-device latest, fleet-wide latest, summaries, GPU list, container leaderboard, all use the older valid row; +6m excluded, +4m included); PG1, PG2, PG3 | — |
| AC-033 | COVERED | Static test as above; **real PostgreSQL**: `GetHistory`, `GetHeartbeatHistory`, `GetFleetHeartbeats` exclude +1y / +1d rows | — |
| AC-034 | COVERED | `TestTelemetryRepo_AC034_NoRowRewrite`; `git diff` shows no `.sql` file; real-database check that all six far-future rows still exist after the guarded reads | — |
| AC-035 | COVERED | `format.test.ts` `[AC-035]` x3; M15 (removing `Math.max` fails the future and far-future cases) | Only `formatAgo` was changed, as FR-039 allows; QA grep found no other relative-age formatter for telemetry/heartbeat timestamps in the diff scope |
| AC-036 | COVERED (inspection) | `router.go` diff touches only the `CARepo` wiring; `registerLimiter := middleware.NewRateLimiter(10, 10)` (line 109) and its route line (143) are untouched; existing `ratelimit_test.go` covers the limiter | No new test, none needed |
| AC-037 | COVERED | Build, vet, tests green (above); tests named for AC-001, 002, 003, 004, 006, 007, 008, 025, 026, 027 all exist; no new dependency | — |

**Mechanical result:** 36 of 37 ACs have an AC-ID-named test (or an explicit inspection or manual item). AC-023 is a documentation item and is pending.

---

## Test Quality Findings

1. **Behaviour, not implementation (good).** Rejection tests assert status, fixed body, device record, full key set and event count, with a nil hub and nil event generator so any broadcast or event panics. The decision is a pure function with a 28-row table asserting `Path`, `MintKey` and `RejectClass` on every row.
2. **Mutation resistance is high.** 24 of 24 mutations were caught, including the subtle ones (rule order, fail-open on a revocation or key-check error, an unresolved key blocking the certificate path, inclusive versus exclusive boundary).
3. **Boundary conditions.** The exact `+5m` and `+5m+1ns` boundary is tested in the pure function; before QA it was not tested through the handlers (only +4m / +6m). QA added handler-level boundary tests for both endpoints.
4. **Mock fidelity gap, closed by QA.** `MockDeviceRepo` stores raw keys and does not hash, and does not fail on a NUL in `FindByDeviceUUID`. The real-database test covers the hashed `StoreAPIKey` / `LookupAPIKey` / `HasAPIKey` / `DeleteAPIKeysByDevice` round trip and confirms the security review's premise that a NUL byte in `device_id` errors in PostgreSQL (so it falls to the new-device path, where C1 sanitises the log).
5. **Minor (non-blocking):** `TestClampLogLimiter_AC031_DebouncePerDeviceKind` ends with `_ = n` (an unused value, no assertion on the final call); the surrounding asserts are sufficient. `captureLogs` swaps the global slog logger, so logging tests must not use `t.Parallel()`; none do.
6. **Isolation.** No shared mutable state across tests; each builds its own fixture. QA's new tests follow the same pattern.
7. **Static placement test is a stand-in.** `GuardPlacement` reads source text. It is useful as a tripwire, but only the database run proves behaviour. The QA integration test is now in the repo (gated), so the stand-in is no longer the only line of defence for anyone with a local database.

---

## Adversarial Findings

| ID | Probe | Outcome | Test written |
|---|---|---|---|
| ADV-1 | Zero timestamp; exactly +5m; +5m+1ns; far past (year 2000); far future (year 9999) on both endpoints | Zero defaults to receive time; +5m kept; +5m+1ns clamped; far past stored as sent; year 9999 clamped without duration overflow or panic | Yes |
| ADV-2 | `device_id` with NUL, newline, CR, ESC, trailing space/newline, upper-cased UUID, 100 KiB string, SQL metacharacters, right-to-left override, on open and registration-key servers | Open server: always a fresh server-generated ID, victim untouched. Registration-key server: uniform 401, exactly one physical log line, no control characters, no echoed content (`[invalid]` placeholder). Confirmed against real PostgreSQL that NUL errors (never resolves) | Yes |
| ADV-3 | Body of exactly 1 MiB with a ~1 MiB hostname | Accepted (at the cap); 1 MiB + 1 is 400. **Informational, pre-existing:** the existing `device registered` INFO line logs the hostname verbatim, so an unauthenticated caller on an open server can emit a roughly 1 MiB log line per request (rate-limited to 10/min per IP). Before this story the body, and therefore the line, was unbounded, so this is an improvement. INFO is not persisted to the database log table, only to stdout | Documented only |
| ADV-4 | Key for device A plus certificate for device A, naming A | Path (a): 200, no new key | Existing AC009 |
| ADV-5 | Registration key configured and the agent sends its own key (the periodic re-registration bug) | 200 via path (a); verified with the real agent client | Yes |
| ADV-6 | Registration key plus a device that already has keys | 401, no side effects; after a first successful bootstrap, a replay is also 401 (one-shot) | Yes |
| ADV-7 | Key for D2, certificate for the named device D1 | 403 (wrong-device credential wins, PO-1) at handler level | Yes |
| ADV-8 | Certificate for D1 plus registration key on a device that already has keys | Path (b): one extra key, prior keys remain valid | Yes |
| ADV-9 | mTLS disabled (nil `caRepo`) with a client certificate present, including one naming another device | Certificate ignored; own-key re-registration still 200; certificate-only is 401 | Yes |
| ADV-10 | Empty body, malformed JSON, plain text, over 1 MiB, with and without a registration key | 400 with the fixed body, no side effects | Existing AC019 + boundary test |
| ADV-11 | Concurrent clamp-log observers | Exactly one emit per device and kind per window; no lost suppressed count | Yes |
| ADV-12 | Race: two path (c) requests on a keyless device, path (c) racing path (b) | Not testable without a transactional harness. The security review records this as SEC-009 (both need the registration key and grant nothing beyond OQ-1) | Documented only |
| ADV-13 | SEC-003: on an mTLS server, path (a) accepts the key alone, so a stolen key can still re-register after the device's certificate is revoked | Confirmed by reading the code and the decision table (V-03 has no certificate requirement). It follows from settled decision S-1; not a regression and not a defect of this story | Documented only |

No exploitable defect was found. Knowing a device UUID grants no capability against that device in any probe.

---

## Security Conditions (security review C1 to C10)

| Cond | Status | Evidence |
|---|---|---|
| C1 sanitise logged `device_id` | **Met** | `loggableDeviceID` allowlist `[A-Za-z0-9._-]` up to 64 characters, else `[invalid]`; used by `logRegReject` (every call site) and the clamp log. Tests: `TestLoggableDeviceID_C1_NoControlCharacters` (11 hostile shapes), `AC020 C1` cases, QA 7-case hostile test asserting a single physical line. M11 |
| C2 never log secrets, all classes and success paths | **Met** | `AC020_RejectionLogsWithoutSecrets` covers all 11 classes; `AC020_SuccessPathsLogNoSecrets` covers paths a, b, c and new-device 201; each asserts no presented key, no registration key, no stored or minted key. Grep: `X-rIOt-Key` read once (`presentedKey`), passed only to `collectRegCredentials` and `regKeyEqual`; `logRegReject` takes no key argument; no `==`/`!=` against the registration key |
| C3 no side effect before the decision, every reject | **Met** | `TestRegisterDevice_C3_EveryRejectClass_NoSideEffects` runs all 11 classes with nil hub and nil event generator and a deep compare of devices, keys and events. Code review: nothing mutates `existing`, and no repo writer, hub or event call precedes the `decideReRegistration` accept (`handlers.go` lines ~198-215). M1 |
| C4 certificate identity property | **Met** | `qualifyingCertDeviceID` reads only `r.TLS.VerifiedChains[0][0]`; no header other than `X-rIOt-Key` is read; a `ListRevokedSerials` error yields no identity; `MTLSDeviceAuth` is not used. The AC-011 subtests are present. M7, M17 |
| C5 `CARepo` wiring | **Met (code review)** | `router.go` lines 65-73: `regCARepo` is a `db.CARepository` interface variable assigned `s.CARepo` (type `*db.CARepo`) only inside `if s.Config.MTLSEnabled && s.CA != nil`, so it is an untyped nil otherwise. The predicate is identical to `addMTLSConfig` (`server.go`), the only place `ClientAuth`/`ClientCAs` are set. `handlers.New` has exactly one non-test construction site (`router.go:75`), so the `applyTLSAndRestart` rebuild is covered |
| C6 HARDEN-002 merge discipline | **Met on this branch / open at merge time** | `registrationKeyMatches` is not declared here; the top-of-handler gate is deleted (otherwise Bug C returns, and AC-002 plus the real-client test would fail); `regKeyEqual` carries `TODO(HARDEN-002)` (QA fixed the missing story ID, commit `901ccd5`). Whichever branch lands second must still collapse `regKeyEqual`, delete any surviving old gate, and re-run AC-002, AC-014, AC-017, AC-018 and the grep |
| C7 complete decision table | **Met** | 28 rows: V-01 to V-11, both PO-1 mixed rows, RK + keycheck error, and extra precedence rows; every row asserts `Path`, `MintKey`, `RejectClass`. M3, M9, M14, M18 |
| C8 README must not claim `RIOT_API_KEY` gates registration | **NOT MET (doc-only, pending technical writer)** | `README.md:139` still reads "Registration key override (if set, agents must present this key to register ...)". No recovery section, no CHANGELOG entry. Release gate, see Action Required |
| C9 read-guard verification on a disposable database | **Met (executed)** | See next section |
| C10 sequencing for future features | n/a | Applies to future features: ship only after this story is deployed, and clamp age at >= 0 |

### C9 detail (ADD 8.1 plus the two additions)

Run against a throwaway `postgres:16` container (unique name, `--rm`, published on `127.0.0.1:55499` only, stopped and removed afterwards; confirmed gone). The repository migrations from `cmd/riot-server/migrations` were applied with the repository's own `RunMigrations`. The test refuses any non-loopback host and runs only when `RIOT_QA_DISPOSABLE_DB_URL` is set, so `go test ./...` skips it.

- ADD 8.1 step 1: device D has snapshots at `NOW() + 1 year` and `NOW() - 1 hour`, heartbeats at `NOW() + 1 day` and `NOW() - 5 minutes`.
- ADD 8.1 step 2: every repository read behind the listed endpoints was called (the HTTP layer is covered by the mock-based handler tests, and these repository methods are exactly what the handlers call). `GetLatestSnapshot`, `GetAllLatestSnapshots`, `GetAllLatestSummaries`, `GetGPUDeviceIDs`, `GetFleetContainerLeaderboard`, `GetHistory`, `GetHeartbeatHistory` and `GetFleetHeartbeats` each use the past row and never the future row.
- Allowance boundary through real SQL: a device with rows at +6 minutes, +4 minutes and -1 hour returns the +4 minute row as latest.
- **C9(a):** a device whose only rows are future-dated drops out of per-device latest (no row), fleet-wide latest and summaries. After one clamped post (a snapshot stored at the receive time, which is what the handler produces) it reappears with that row as latest.
- **C9(b):** the guard is inside the subquery in `GetAllLatestSummaries`, `GetGPUDeviceIDs` and `GetFleetContainerLeaderboard`. Proved behaviourally, not only textually: devices with a valid older row (GPU present in the older row, container list in the older row) remain in the views with the older row's content, and a GPU that exists only in a future-dated row is not counted. PG2 shows an outer `WHERE` fails this test.
- FR-038 check: all six far-future rows still exist after all the reads (nothing deleted or rewritten).
- Also verified against the real database: `HasAPIKey` true/false/other-device/unknown-device/after-delete, and `HasAPIKey` / `FindByDeviceUUID` on hostile IDs (NUL errors, trailing space, upper case and SQL metacharacters never resolve).

---

## Tests Added by QA

| File | Lines | Covers |
|------|-------|--------|
| `internal/server/handlers/reg_harden_qa_test.go` | whole file (13 top-level tests, 32 subtests) | AC-003, AC-007, AC-008, AC-011, AC-014, AC-015, AC-019 (boundary), AC-020/C1 (hostile IDs), AC-024 (real agent client, Bug C end to end), AC-025, AC-027, AC-028 (handler-level boundary), AC-031 (concurrency) |
| `internal/server/db/telemetry_repo_pg_qa_test.go` | whole file (2 top-level tests, gated) | AC-026 (read half), AC-032, AC-033, AC-034, C9 (a) and (b), `HasAPIKey`, hostile IDs against PostgreSQL. Skips unless `RIOT_QA_DISPOSABLE_DB_URL` is set; refuses non-loopback hosts |

Each test carries `// Added by QA Engineer` and an AC reference. QA also fixed one trivial house-standard nit in production code (comment only): `register_auth.go` `TODO(merge)` -> `TODO(HARDEN-002)` (commit `901ccd5`). No implementation behaviour was changed.

---

## Deviations from ADD

None that weaken an AC or a security condition. Confirmed against the implementation report's list:

1. Logged `device_id` uses an allowlist plus `[invalid]` placeholder and a `device_id_len` field instead of the ADD's `truncate(..., 64)`. This is required by C1 and is a strictly safer superset.
2. `regPathName` and a `path` field on the existing "generated API key for enrolled device" info log: harmless, carries a proof label only.
3. The `db` guard test is stricter than the ADD's literal-versus-constant check. Fine.
4. Limiter sweep ordering (records the current observation before sweeping). Behaviour matches AD-007; the sweep tests pass.

## Deviations from FRD

1. **FR-027 / FR-028 / AC-023 (documentation) are not in this branch.** The README recovery procedure (both alternatives, `server.api_key`, `/var/lib/riot/device-id`, mTLS self-recovery) and the CHANGELOG behaviour-change entry do not exist yet. The implementation report and ADD assign these to the technical writer at the end of the pipeline, so this is a pipeline-sequence item rather than an implementation defect, but the story is not shippable until they land (see Action Required 1 and 2).
2. **Security condition C8** (correct the README `RIOT_API_KEY` claim) is the same documentation item and is also open.

No other FRD deviation was found. All code-testable FRs (FR-001 to FR-026, FR-029 to FR-041) are implemented and covered.

---

## Verdict Rationale

**PASS WITH NOTES.** All tests are green (821 Go top-level tests, 1446 including subtests, 0 failing; 411 web tests; build, vet and `tsc` clean). Every code-testable AC has a test whose name carries its AC ID, and 24 of 24 mutation probes (including the highest-risk ones requested: reject early-return, rule order, `regKeyEqual`, the read guard, `Math.max`) were caught. The disposable-database check (C9) was executed and passed, including the two additions. Security conditions C1 to C5 and C7 are met; C6 is met on this branch and is a merge-time obligation; C9 is met; C10 is future-facing. No critical or high finding, and no exploitable defect, was found.

It is not a clean PASS because the documentation deliverables (AC-023, FR-027, FR-028, C8) are still outstanding, the actual released agent binary was not run (AC-024 manual half), and `-race` was not run locally.

## Action Required

Not a FAIL, so nothing blocks re-review of the code. Before this story is tagged or released:

1. **Technical writer:** add the README recovery section (FR-027 / AC-023): Rotate key then paste into `server.api_key` in `/etc/riot/agent.yaml` and restart `riot-agent`; or delete `/var/lib/riot/device-id` and restart (registers as a new device, needs the registration key in `server.api_key` when the server has one; the old record remains until an admin deletes it); mTLS agents with a valid certificate recover automatically.
2. **Technical writer:** add the CHANGELOG entry (FR-028) and call out the open-server, partially wiped agent behaviour change (an agent whose ID file survived but whose key was lost used to be silently re-keyed and now gets a 401).
3. **Technical writer (C8):** change `README.md:139` so it no longer says `RIOT_API_KEY` gates registration; state that only the Settings > Registration value is enforced until the follow-up story lands.
4. **Release:** code change, so it needs a tagged release; after deploy, confirm the running image digest contains the tag (C10 depends on this). Restart any existing mTLS-enrolled agents that never received a key so the SEC-009 window closes.
5. **Merge:** if HARDEN-002 lands first or second, follow C6 (collapse `regKeyEqual` into `registrationKeyMatches`, delete any surviving old top-of-handler gate, re-run AC-002, AC-014, AC-017, AC-018 and the grep).
6. **CI:** confirm the Linux `-race` job is green for the new `clampLogLimiter` and the mock `HasAPIKey`.
7. **Manual (optional but recommended):** run the released agent binary against a development server for the five AC-024 flows.

Follow-ups recorded by the security review (not blocking): F-1 enforcement of `RIOT_API_KEY` (SEC-002), fail-closed `GetConfig` (SEC-005), autocert and mTLS (SEC-006), `MTLSDeviceAuth` fail-closed (SEC-004), SEC-003 (path (a) on mTLS servers), logstore batch resilience, atomic `RotateKey`. QA adds one: cap or truncate the hostname in the existing `device registered` INFO log (ADV-3).
