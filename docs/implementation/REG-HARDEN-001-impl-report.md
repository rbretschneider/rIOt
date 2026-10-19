# Implementation Report

- **Story ID:** REG-HARDEN-001
- **Title:** Device-proof re-registration (no key minting by UUID) + future-dated telemetry clamp
- **Engineer:** Senior Dev Agent
- **Date:** 2026-10-18
- **Stack:** Go module (chi/pgx server, testify tests; `go vet` as the Go linter) plus a React 18 / Vite / TypeScript web UI tested with vitest. Detected from `go.mod`, `web/package.json`, `web/vitest.config.ts`. The web package has no `lint` script, so `npx tsc --noEmit` is used as its static check.
- **Origin:** an internal security review of device registration.

---

## Pre-Implementation Baseline

The FRD, ADD (FINAL), and security review (APPROVED WITH CONDITIONS, C1 to C9) were read in full before any code was written. No blockers were found, so no `REG-HARDEN-001-blockers.md` exists. Nothing was pushed, tagged, or deployed.

| Check (before any change) | Result |
|---|---|
| `go build ./...` | green |
| `go test ./...` (no `-race`: needs CGO, unavailable on Windows) | all packages `ok`; 757 top-level tests, 1217 including subtests |
| `go vet ./...` | clean (no output) |
| `cd web && npm run test:run` | 27 files, **405** tests passed (after `npm ci` in the fresh worktree; `package.json` and the lockfile are unchanged) |
| Web lint | no `lint` script exists; `npx tsc --noEmit` clean |

**Environment notes.**
- The working tree is CRLF (`core.autocrlf=true`; the index is LF). `gofmt -l` therefore lists about 125 files on this machine, which is pre-existing. With CRs stripped, `handlers.go`, `router.go`, `mocks.go` and `interfaces.go` each have one pre-existing alignment diff, identical to the baseline commit, and none touch the lines this story added. All new files are `gofmt` clean.
- Edits to existing files preserved CRLF. New files were written LF and are normalised by git on commit.

---

## Completed Components

| File | Action | Notes |
|---|---|---|
| `internal/server/db/interfaces.go` | MODIFY | `DeviceRepository.HasAPIKey(ctx, deviceID) (bool, error)` |
| `internal/server/db/device_repo.go` | MODIFY | `HasAPIKey`: `SELECT EXISTS(SELECT 1 FROM api_keys WHERE device_id=$1)`, error wrapped with `%w` |
| `internal/testutil/mocks.go` | MODIFY | `MockDeviceRepo.HasAPIKey` under `RLock`; honors `Err`, then the new `HasAPIKeyErr` |
| `internal/server/handlers/register_auth.go` | CREATE | Pure `decideReRegistration`; read-only `collectRegCredentials`; fail-closed `qualifyingCertDeviceID` (identity only from `VerifiedChains[0][0]`, no header read); `regKeyEqual` (sha256 plus `subtle.ConstantTimeCompare`); fixed 401/403 bodies; `loggableDeviceID`; `logRegReject`; 1 MiB body cap constant |
| `internal/server/handlers/handlers.go` | MODIFY | `HandlerDeps.CARepo`; `Handlers.caRepo/ingestClock/clampLog`; `New` wiring; `RegisterDevice` reorder (top-of-handler key gate deleted, `MaxBytesReader`, decide before any mutation, mint gated on `dec.MintKey`, constant-time new-device key check); `Heartbeat`/`Telemetry` clamp calls |
| `internal/server/router.go` | MODIFY | `CARepo` passed only inside `if s.Config.MTLSEnabled && s.CA != nil`; otherwise an untyped nil interface (C5) |
| `internal/server/handlers/ingest_clamp.go` | CREATE | `clampFutureTimestamp`, `ingestNow`, `noteClamp`, bounded and swept `clampLogLimiter` (nil-receiver safe) |
| `internal/server/db/telemetry_repo.go` | MODIFY | `FutureSkewAllowance`, `notFutureDatedSQL`; guard spliced into all 8 read queries (inside the `DISTINCT ON` subquery for the 3 subquery queries and `GetAllLatestSnapshots`' `WHERE` before `ORDER BY`) |
| `internal/server/db/telemetry_repo_guard_test.go` | CREATE | SQL literal agrees with the Go constant; guard present and correctly placed in each query; no UPDATE and only the two existing DELETEs (AC-034) |
| `internal/server/handlers/register_auth_test.go` | CREATE | Decision table, `regKeyEqual`, certificate identity cases, credential facts, `loggableDeviceID`, fixed bodies |
| `internal/server/handlers/register_device_test.go` | CREATE | `RegisterDevice` handler tests (AC-001 to AC-022, AC-024; AC-018 lives in `register_auth_test.go`) |
| `internal/server/handlers/ingest_clamp_test.go` | CREATE | Clamp rule, limiter, and Telemetry/Heartbeat handler clamp tests |
| `web/src/utils/format.ts` | MODIFY | `formatAgo(ts?, now = Date.now())`, age clamped at zero |
| `web/src/utils/format.test.ts` | MODIFY | `formatAgo` tests including AC-035 |
| `web/src/pages/FleetOverview.tsx` | MODIFY | Local `formatAgo` removed; imported from `../utils/format` |

Not touched, as the ADD requires: `server.go`, `middleware/*`, `enroll.go`, the agent, `install.sh`, migrations, `go.mod`, `go.sum`, `web/package.json` and the lockfile. `README.md` and `CHANGELOG.md` are the technical writer's items (ADD §4), so they are not part of this change (see the QA notes).

### Commits

| Hash | Subject |
|---|---|
| `a2cb235` | `[REG-HARDEN-001] feat: add DeviceRepository.HasAPIKey` |
| `ebce308` | `[REG-HARDEN-001] feat: re-registration proof decision, cert identity check, CA repo wiring` |
| `66be687` | `[REG-HARDEN-001] fix: require proof of device identity to re-register an existing device` |
| `8829207` | `[REG-HARDEN-001] fix: exclude future-dated snapshots and heartbeats from every read` |
| `40596d1` | `[REG-HARDEN-001] fix: clamp future-dated snapshot and heartbeat timestamps on ingest` |
| `4bf45a4` | `[REG-HARDEN-001] fix: never render a negative relative age in the fleet table` |
| (this report) | `[REG-HARDEN-001] docs: implementation report` |

---

## Security conditions (C1 to C9)

| Condition | How it is met |
|---|---|
| C1 sanitize logged `device_id` | `loggableDeviceID` echoes only a string of at most 64 chars from `[A-Za-z0-9._-]` (which covers UUIDs) and otherwise logs the placeholder `[invalid]`; `device_id_len` is logged alongside. Applies to every `logRegReject` call site and to the clamp log. Tests: `TestLoggableDeviceID_C1_NoControlCharacters` (NUL first, NUL embedded, `\n`, `\r`, ESC, tab, DEL, space, quote, non-ASCII, overlong) and, in `TestRegisterDevice_AC020_RejectionLogsWithoutSecrets`, the cases `C1 device_id with NUL and newline` (asserts no `\u0000`, no echoed content, one physical log line) and `C1 overlong device_id` |
| C2 secrets never logged | `TestRegisterDevice_AC020_RejectionLogsWithoutSecrets` covers all 11 reject classes; `TestRegisterDevice_AC020_SuccessPathsLogNoSecrets` covers path (a), path (b), path (c) and new-device 201. Each asserts the log contains neither the presented key, the registration key, nor any stored or minted key. Grep checks below |
| C3 no side effect on any reject | `TestRegisterDevice_C3_EveryRejectClass_NoSideEffects` runs every class (including `new-device-regkey-mismatch`) with nil hub and nil eventGen, a panic-to-failure wrapper, and a deep compare of `Devices`, `APIKeys` and event count. Mutation check: disabling the reject early-return fails 9 tests |
| C4 certificate identity property | Identity only from `r.TLS.VerifiedChains[0][0]`; no header other than `X-rIOt-Key` is read; a `ListRevokedSerials` error gives no identity; `MTLSDeviceAuth` is not used. AC-011 subtests present: revoked, empty CN, `PeerCertificates` without `VerifiedChains`, nil `TLS`, nil `caRepo` with a verified chain, plus a forwarded-certificate-header test |
| C5 `CARepo` wiring | `router.go` assigns `s.CARepo` into a `db.CARepository` variable only inside `if s.Config.MTLSEnabled && s.CA != nil`. `setupRouter` remains the single `handlers.New` site (so the `applyTLSAndRestart` rebuild is covered). Code-review item for QA |
| C6 HARDEN-002 merge discipline | `registrationKeyMatches` is not declared (grep: only a comment mentions it). `regKeyEqual` carries a `TODO(merge)` comment to collapse into HARDEN-002's helper. The merge steps are for whoever lands second |
| C7 complete decision table | `TestDecideReRegistration_AC003_AC012_DecisionTable` has 28 rows: V-01 to V-11, both PO-1 mixed rows, RK plus keycheck error, and extra precedence rows. Each row asserts `Path`, `MintKey` and `RejectClass` |
| C8, C9 | Documentation and QA items, not implemented here |

### Grep checks (ADD Definition of Done)

```
$ grep -nE "regKey\b.*[!=]= " internal/server/handlers/*.go (non-test)
handlers.go:253:  if regKey != "" && !regKeyEqual(presentedKey, regKey) {
register_auth.go:191:  c.RegKeyMatch = regKey != "" && regKeyEqual(presentedKey, regKey)
```
Both hits are the "is a registration key configured" test against the empty string. No `==` or `!=` against a presented value remains.

`X-rIOt-Key` is read once in `RegisterDevice` (`presentedKey`) and passed only to `collectRegCredentials` and `regKeyEqual`. `logRegReject` and the clamp logger take no key argument.

---

## Test Summary

### AC to test mapping

| AC | Test file | Tests | Status |
|---|---|---|---|
| AC-001 | `register_device_test.go` | `TestRegisterDevice_AC001_OwnKeyOpenServer_NoNewKey` | PASS |
| AC-002 | `register_device_test.go` | `TestRegisterDevice_AC002_OwnKeyWithRegKeyConfigured_Accepted` (Bug C regression) | PASS |
| AC-003 | `register_device_test.go`, `register_auth_test.go` | `TestRegisterDevice_AC003_WrongDeviceKey_403NoSideEffects`; decision-table rows | PASS |
| AC-004 | `register_device_test.go` | `TestRegisterDevice_AC004_NoKeyNoRegKey_Rejected`; `..._C3_EveryRejectClass_NoSideEffects` | PASS |
| AC-005 | `register_device_test.go` | `TestRegisterDevice_AC005_GarbageKey_NoMetadataOverwrite` | PASS |
| AC-006 | `register_device_test.go` | `TestRegisterDevice_AC006_RegKeyKeylessDevice_IssuesKey` (key then authenticates telemetry through `middleware.DeviceAuth`) | PASS |
| AC-007 | `register_device_test.go` | `..._AC007_RegKeyDeviceHasKeys_401`, `..._AC007_HasAPIKeyError_FailsClosed` | PASS |
| AC-008 | `register_device_test.go` | `..._AC008_MTLSEnrolledDevice_CertIssuesKey` (3 key variants x with/without registration key) | PASS |
| AC-009 | `register_device_test.go` | `..._AC009_CertPlusOwnKey_NoNewKey` | PASS |
| AC-010 | `register_device_test.go` | `..._AC010_CertPath_KeepsPriorKeys` | PASS |
| AC-011 | `register_device_test.go`, `register_auth_test.go` | `..._AC011_RevokedCert_401`, `..._AC011_UnverifiedCertAndHeader_401`, `TestQualifyingCert_AC011_NoIdentityCases`, `TestCollectRegCredentials_AC011_IgnoresForwardedCertHeaders` | PASS |
| AC-012 | `register_device_test.go` | `..._AC012_CertForOtherDevice_403` (no key, garbage key, D1's own key) | PASS |
| AC-013 | `register_device_test.go`, `register_auth_test.go` | `..._AC013_RevocationListError_FailsClosed`, `TestQualifyingCert_AC013_RevocationListErrorFailsClosed` | PASS |
| AC-014 | `register_device_test.go` | `..._AC014_Uniform401Body` (6 cases: status, body and headers byte-identical; no ID, hostname, key or class in body) | PASS |
| AC-015 | `register_device_test.go` | `..._AC015_OpenNewDeviceRegistration_201` | PASS |
| AC-016 | `register_device_test.go` | `..._AC016_NewDeviceRegKey` (includes a prefix-of-key case) | PASS |
| AC-017 | `register_device_test.go` | `..._AC017_DeviceKeyNotRegKeyForUnknownUUID` | PASS |
| AC-018 | `register_auth_test.go` | `TestRegKeyEqual_AC018_ConstantTimeCompare`; grep inspection above | PASS |
| AC-019 | `register_device_test.go` | `..._AC019_MalformedBody_400NoSideEffects` (4 bodies incl. over 1 MiB, with and without a registration key) | PASS |
| AC-020 | `register_device_test.go` | `..._AC020_RejectionLogsWithoutSecrets` (11 classes plus 2 C1 cases), `..._AC020_SuccessPathsLogNoSecrets` | PASS |
| AC-021 | `register_device_test.go` | `..._AC021_RotateKeyThenOwnKeyReRegisters` | PASS |
| AC-022 | `register_device_test.go` | `..._AC022_NoDeviceID_RegistersAsNewDevice` (with and without registration key) | PASS |
| AC-023 | none | Documentation AC (README, CHANGELOG): doc review by QA. Not part of the senior-dev change | n/a |
| AC-024 | `register_device_test.go` | `..._AC024_ReleasedAgentRequestShapes` (7 flows replaying the request shape in `internal/agent/register.go`). The released-binary run against a dev server is a QA manual step | PASS (automated half) |
| AC-025 | `ingest_clamp_test.go` | `TestTelemetry_AC025_FutureSnapshotClampedAndAccepted`, `..._ClampedResponseIdenticalToUnclamped`, `..._MissingTimestampDefaultsToReceiveTime` | PASS |
| AC-026 | `ingest_clamp_test.go` | `TestTelemetry_AC026_FutureSnapshotNeverLatest` (write half). SQL half: QA manual | PASS (write half) |
| AC-027 | `ingest_clamp_test.go` | `TestTelemetry_AC027_PastSnapshotStoredAsSent`. SQL ordering: inspection | PASS (write half) |
| AC-028 | `ingest_clamp_test.go` | `TestClampFutureTimestamp_AC028_Boundaries`, `TestTelemetry_AC028_WithinAllowanceKept`, `TestHeartbeat_AC028_WithinAllowanceKept` | PASS |
| AC-029 | `ingest_clamp_test.go` | `TestHeartbeat_AC029_FutureHeartbeatClamped` | PASS |
| AC-030 | `ingest_clamp_test.go`, `telemetry_repo_guard_test.go` | `TestHeartbeat_AC030_PastHeartbeatStoredAsSent`, `TestFutureSkewSQL_AC030_MatchesAllowance` | PASS |
| AC-031 | `ingest_clamp_test.go` | `TestClampLogLimiter_AC031_DebouncePerDeviceKind`, `..._NilReceiverAlwaysEmits`, `TestClampLogLimiter_NFR009_SweepBounded`, `TestHeartbeat_AC031_ClampLogDebounced` (100 clamps then 1 entry, then 1h later an entry reporting `clamps_since_last=100`) | PASS |
| AC-032 | `telemetry_repo_guard_test.go` | `TestReadQueries_AC032_AC033_GuardPlacement`: the guard is present exactly once in each of the 5 latest queries and precedes `ORDER BY` and `) latest` in the `DISTINCT ON` ones. Behavior on real rows: QA manual dev-DB (ADD 8.1) | PASS (structural) |
| AC-033 | `telemetry_repo_guard_test.go` | Same test for the 3 history and window queries. Behavior: QA manual | PASS (structural) |
| AC-034 | `telemetry_repo_guard_test.go` | `TestTelemetryRepo_AC034_NoRowRewrite`; `git diff` shows no migration file | PASS |
| AC-035 | `web/src/utils/format.test.ts` | `[AC-035] renders a future timestamp as zero age with no minus sign` (plus far-future, equal-to-now, and regression cases) | PASS |
| AC-036 | none new | Existing `ratelimit_test.go`. The `registerLimiter` route line and `NewRateLimiter(10, 10)` are untouched (the `router.go` diff only adds the `CARepo` wiring) | n/a (inspection) |
| AC-037 | CI | `go build ./...`, `go test ./...`, `npm run test:run` all green; `go.mod`, `go.sum`, `package.json` and the lockfile have an empty diff | PASS |

### Not covered by an automated test

- **AC-023:** documentation (README recovery section, CHANGELOG). The technical writer owns it; QA reviews it.
- **AC-026, AC-027 (ordering), AC-032, AC-033 (behavior on stored rows):** the `db` package has no database harness (ADD section 0). The structural test above pins the guard text and placement. The behavior check is the ADD 8.1 manual dev-DB procedure plus the C9 additions, run by QA on a disposable database.
- **AC-024 (released binary) and AC-036:** QA manual run and inspection, as the ADD maps them.

### Test counts

| Suite | Before | After | Delta |
|---|---|---|---|
| Go, top-level tests (`go test ./... -v`, lines `^--- `) | 757 | 808 | +51 |
| Go, including subtests | 1217 | 1414 | +197 |
| Go, failures | 0 | 0 | |
| Web (vitest) | 405 in 27 files | 411 in 27 files | +6 |

### Test run output

New Go tests (`go test ./internal/server/handlers ./internal/server/db -run '<new tests>' -v`, top-level lines):

```
--- PASS: TestClampFutureTimestamp_AC028_Boundaries
--- PASS: TestClampLogLimiter_AC031_DebouncePerDeviceKind
--- PASS: TestClampLogLimiter_AC031_NilReceiverAlwaysEmits
--- PASS: TestClampLogLimiter_NFR009_SweepBounded
--- PASS: TestTelemetry_AC025_FutureSnapshotClampedAndAccepted
--- PASS: TestTelemetry_AC025_ClampedResponseIdenticalToUnclamped
--- PASS: TestTelemetry_AC026_FutureSnapshotNeverLatest
--- PASS: TestTelemetry_AC027_PastSnapshotStoredAsSent
--- PASS: TestTelemetry_AC028_WithinAllowanceKept
--- PASS: TestTelemetry_AC025_MissingTimestampDefaultsToReceiveTime
--- PASS: TestHeartbeat_AC029_FutureHeartbeatClamped
--- PASS: TestHeartbeat_AC030_PastHeartbeatStoredAsSent
--- PASS: TestHeartbeat_AC028_WithinAllowanceKept
--- PASS: TestHeartbeat_AC031_ClampLogDebounced
--- PASS: TestDecideReRegistration_AC003_AC012_DecisionTable
--- PASS: TestRegKeyEqual_AC018_ConstantTimeCompare
--- PASS: TestQualifyingCert_AC011_NoIdentityCases
--- PASS: TestQualifyingCert_AC013_RevocationListErrorFailsClosed
--- PASS: TestCollectRegCredentials_AC011_IgnoresForwardedCertHeaders
--- PASS: TestCollectRegCredentials_Facts
--- PASS: TestLoggableDeviceID_C1_NoControlCharacters
--- PASS: TestWriteRegReject_AC014_FixedBodies
--- PASS: TestRegisterDevice_AC001_OwnKeyOpenServer_NoNewKey
--- PASS: TestRegisterDevice_AC002_OwnKeyWithRegKeyConfigured_Accepted
--- PASS: TestRegisterDevice_AC003_WrongDeviceKey_403NoSideEffects
--- PASS: TestRegisterDevice_AC004_NoKeyNoRegKey_Rejected
--- PASS: TestRegisterDevice_AC005_GarbageKey_NoMetadataOverwrite
--- PASS: TestRegisterDevice_AC006_RegKeyKeylessDevice_IssuesKey
--- PASS: TestRegisterDevice_AC007_RegKeyDeviceHasKeys_401
--- PASS: TestRegisterDevice_AC007_HasAPIKeyError_FailsClosed
--- PASS: TestRegisterDevice_AC008_MTLSEnrolledDevice_CertIssuesKey
--- PASS: TestRegisterDevice_AC009_CertPlusOwnKey_NoNewKey
--- PASS: TestRegisterDevice_AC010_CertPath_KeepsPriorKeys
--- PASS: TestRegisterDevice_AC011_RevokedCert_401
--- PASS: TestRegisterDevice_AC011_UnverifiedCertAndHeader_401
--- PASS: TestRegisterDevice_AC012_CertForOtherDevice_403
--- PASS: TestRegisterDevice_AC013_RevocationListError_FailsClosed
--- PASS: TestRegisterDevice_AC014_Uniform401Body
--- PASS: TestRegisterDevice_AC015_OpenNewDeviceRegistration_201
--- PASS: TestRegisterDevice_AC016_NewDeviceRegKey
--- PASS: TestRegisterDevice_AC017_DeviceKeyNotRegKeyForUnknownUUID
--- PASS: TestRegisterDevice_AC019_MalformedBody_400NoSideEffects
--- PASS: TestRegisterDevice_C3_EveryRejectClass_NoSideEffects
--- PASS: TestRegisterDevice_AC020_RejectionLogsWithoutSecrets
--- PASS: TestRegisterDevice_AC020_SuccessPathsLogNoSecrets
--- PASS: TestRegisterDevice_AC021_RotateKeyThenOwnKeyReRegisters
--- PASS: TestRegisterDevice_AC022_NoDeviceID_RegistersAsNewDevice
--- PASS: TestRegisterDevice_AC024_ReleasedAgentRequestShapes
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers
--- PASS: TestFutureSkewSQL_AC030_MatchesAllowance
--- PASS: TestReadQueries_AC032_AC033_GuardPlacement
--- PASS: TestTelemetryRepo_AC034_NoRowRewrite
ok  	github.com/DesyncTheThird/rIOt/internal/server/db
```

Full Go suite (`go test ./...`):

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

Web suite (`cd web && npm run test:run`):

```
 Test Files  27 passed (27)
      Tests  411 passed (411)
```

Linters:

```
$ go build ./...         -> ok
$ go vet ./...           -> no output (clean)
$ cd web && npx tsc --noEmit -> no output (clean)
web `npm run lint`       -> no such script in package.json (not run)
```
No new vet or type errors, and no pre-existing failures were seen. The only pre-existing lint-adjacent item is the `gofmt` CRLF and alignment noise described under the baseline.

Mutation sanity check: with the `RegisterDevice` reject early-return disabled, 9 `RegisterDevice` tests failed; the change was reverted before committing.

---

## Deviations from the ADD

No deviation weakens an AC, NFR, or security condition. Recorded for architect review:

1. **Logged `device_id` mechanism (C1).** The ADD said `truncate(suppliedDeviceID, 64)`. The security review required that NUL and C0 characters be impossible. Implementation: allowlist `[A-Za-z0-9._-]` up to 64 chars, else the placeholder `[invalid]`. This is a superset of the "UUID-shape check" example, so non-UUID legacy device IDs still log verbatim. The log entry also carries a `device_id_len` field (not in ADD section 9's field list) so the original length stays visible when the placeholder is used.
2. **`regPathName` helper and a `path` field on the existing "generated API key for enrolled device" info log** (ADD 7.1 step 5.5 asks for a `path` field). The value is the proof label (`certificate` or `registration-key`), never a credential.
3. **The `db` guard test goes further than the ADD's literal-vs-constant check.** `telemetry_repo_guard_test.go` also reads the source file to assert the guard appears exactly once in each of the 8 queries, sits before `ORDER BY` and `) latest` in the `DISTINCT ON` queries, and that no UPDATE or extra DELETE exists (AC-034). This is a static check and a stand-in for a database harness that does not exist. It is not a replacement for the QA dev-DB procedure.
4. **Extra tests beyond the ADD's names:** `TestRegisterDevice_C3_EveryRejectClass_NoSideEffects`, `TestRegisterDevice_AC011_UnverifiedCertAndHeader_401`, `TestRegisterDevice_AC020_SuccessPathsLogNoSecrets`, `TestCollectRegCredentials_*`, `TestLoggableDeviceID_C1_*`, `TestWriteRegReject_AC014_FixedBodies`, `TestClampLogLimiter_AC031_NilReceiverAlwaysEmits`, `TestTelemetry_AC025_ClampedResponseIdenticalToUnclamped`, `TestTelemetry_AC025_MissingTimestampDefaultsToReceiveTime`, `TestHeartbeat_AC028_WithinAllowanceKept`. These implement conditions C1 to C4 and FR-033/FR-034 directly.
5. **Sweep ordering in `clampLogLimiter.observe`.** The sweep runs after the current observation is recorded (not before), so the current key's pending suppressed count is never discarded by its own sweep. Behavior otherwise matches AD-007.
6. **`go test` on Windows without `-race`.** CI (Linux) runs `-race`; the new `clampLogLimiter` is mutex-guarded and the mock's `HasAPIKey` uses `RLock`, but `-race` was not run locally.

---

## Notes for QA

- **Docs still owed (technical writer):** README recovery section (FR-027, AC-023) and CHANGELOG entry (FR-028). Per C8, the README must not say `RIOT_API_KEY` gates registration: only the Settings registration value (`admin_config.registration_key`) is enforced today. The CHANGELOG should call out the open-server, partially wiped agent behavior change (an agent whose ID file survived but whose key was lost used to be silently re-keyed and is now rejected with 401).
- **C9 / ADD 8.1 manual DB check:** run on a disposable database only. Also confirm (a) a device whose only rows are future-dated drops out of the latest views and returns after one clamped post, and (b) the guard is inside the subquery in `GetAllLatestSummaries`, `GetGPUDeviceIDs` and `GetFleetContainerLeaderboard` (the structural test already pins this, so (b) is a confirmation).
- **AC-024 released-agent run:** the released agent against a dev server for all five flows (first install with registration key, first install open, startup re-register with own key, periodic re-register with registration key configured, first key after mTLS enrollment).
- **C5 code review:** `router.go` is the only `handlers.New` construction site; `CARepo` is assigned only inside the `MTLSEnabled && CA != nil` block.
- **Behavior to expect (not a regression):**
  - Every rejection is a 401 `{"error":"unauthorized"}` or a 403 `{"error":"forbidden"}`. The old text `invalid registration key` is gone.
  - On an mTLS server, path (a) accepts the API key alone (SEC-003 follow-up, by design of S-1).
  - Autocert TLS mode never requests client certificates, so path (b) is unavailable there and fails closed (F-3); the registration-key path (c) still works.
  - Existing Bug D devices (enrolled on a registration-key server, never keyed) remain claimable by a registration-key holder until their agent next re-registers (SEC-009); restarting those agents after deploy closes the window.
- **Release:** this is a code change, so it needs a tagged release. No tag was created here.
- **Test data:** all tests use in-memory mocks; none need a database, network, or fixtures. `captureLogs(t)` swaps the global slog logger, so the logging tests must not use `t.Parallel()`.
