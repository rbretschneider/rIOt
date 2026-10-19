# Architecture Decision Document

| Field         | Value                                       |
|---------------|---------------------------------------------|
| Story ID      | REG-HARDEN-001                              |
| FRD Reference | docs/requirements/REG-HARDEN-001-frd.md     |
| Author        | Architect Agent                             |
| Date          | 2026-10-17                                  |
| Revision      | 1                                           |
| Status        | FINAL (subject to security review)          |

---

## 0. FRD review outcome and recorded decisions

No questions for the Business Developer. The FRD is final. Every AC can be met as written, with
one qualification. AC-026, AC-027 (the "latest" half), AC-032 and AC-033 depend on SQL ordering
and filtering. The codebase has no database test harness (`internal/server/db` has no `_test.go`
files), so the SQL halves of those ACs are verified by QA inspection plus a manual dev-DB check
(Section 8). They are not covered by automated tests. This is a test-strategy limit, not an
infeasible AC.

FRD open questions are answered as follows. None contradicts an FRD assumption.

- **OQ-1 (zero-key window lets a registration-key holder bootstrap a key):** accepted, as the
  FRD assumes. Path (c) is gated by `DeviceRepository.HasAPIKey`, evaluated per request. It does
  not depend on any cached state.
- **OQ-2 (dashboard events for rejections/clamps):** none. Rejections and clamps go to warn logs only.
- **OQ-3 (stored future rows resurface near their date):** accepted residual. The read guard is
  relative to `NOW()`. No rows are rewritten.
- **OQ-4 (timing side channel on UUID existence):** out of scope. Not mitigated.
- **OQ-5 (key accumulation via path (b)):** accepted. The rate limit bounds it, and Rotate key
  collapses the set back to one key.

### Pre-existing findings surfaced during analysis (not changed by this story)

These findings are recorded so they do not get lost. None of them blocks this story.

- **F-1: `RIOT_API_KEY` is never enforced.** `config.go:62` loads `RIOT_API_KEY` into
  `Config.RegistrationKey`, and `loadDBConfig` (`server.go:306-311`) documents "env var overrides
  DB". But `RegisterDevice` only reads `admin_config.registration_key` through
  `adminRepo.GetConfig`. `Config.RegistrationKey` is never read anywhere. An operator who sets
  only the env var, as `README.md:139` describes, runs an **open** registration server. The FRD
  defines "registration key" as the admin-config value, so this story keeps that source. Fixing
  F-1 needs a separate product owner decision (see the hand-off).
- **F-2: `GetConfig` errors are swallowed** (`regKey, _ :=`). A transient DB error reads as "not
  configured". For re-registration this only disables path (c), which is fail-closed. For
  new-device registration it means open registration during the DB error, which is pre-existing
  and unchanged (S-4). In practice `Create` would also fail when the DB is down.
- **F-3: autocert TLS mode never requests client certificates.** `configureTLS` (`server.go:567`)
  returns `m.TLSConfig()` without calling `addMTLSConfig`. So with `TLSDomain` set and mTLS
  enabled, no request ever carries `VerifiedChains`. Path (b) is unavailable there and fails
  closed (FR-012). `MTLSDeviceAuth`-guarded routes are already unusable in that mode.

---

## 1. Summary

`RegisterDevice` is reordered: parse the body (400) first, then resolve the named device. For an
existing device, the handler runs a **pure, table-tested proof decision** before any mutation.
That decision lives in a new file, `handlers/register_auth.go`, and implements the FR-013
precedence over three gathered facts:

- the device that `X-rIOt-Key` resolves to;
- the CN of a *handshake-verified*, unrevoked client certificate (`r.TLS.VerifiedChains`, checked
  against `CARepository.ListRevokedSerials`, fail-closed);
- a constant-time registration-key match, used only together with a new
  `DeviceRepository.HasAPIKey`.

New-device registration keeps today's policy, with a constant-time key check. On ingest, the
`Telemetry` and `Heartbeat` handlers clamp timestamps more than 5 minutes in the future to an
injectable server receive time and log clamps with a bounded, debounced limiter. Every
snapshot/heartbeat read query gains `timestamp <= NOW() + interval '5 minutes'`. The fleet
`formatAgo` moves to `web/src/utils/format.ts` and clamps negative ages to zero. There is no
migration, no new dependency, and no agent change.

---

## 2. Technical Context

### 2.1 What exists

- **`RegisterDevice`** (`internal/server/handlers/handlers.go:155-264`) works as follows. The top
  check (`157-164`) compares `X-rIOt-Key` against the admin-config registration key with plain
  `!=` and returns 401 `{"error":"invalid registration key"}`. Body decode follows (`166-170`).
  The re-registration branch (`176-221`) mutates `existing`, calls `devices.Update`, mints a key
  when the header is empty, writes 200, broadcasts, and emits `DeviceOnline`. It performs **no
  credential check**. The new-device branch is at `223-263`.
- **Route** (`router.go:131`): `POST /api/v1/devices/register`, public, `registerLimiter`
  (10/min, keyed on the RealIP-resolved client).
- **`Handlers` construction happens in `router.go:64-90`** (`setupRouter`), not in `server.go`.
  `setupRouter` runs at startup (`server.go:195`) and again in `applyTLSAndRestart`
  (`server.go:381`), after `loadOrCreateCA`, when the setup wizard enables mTLS. Those are the
  only two construction sites. mTLS state is `s.Config.MTLSEnabled && s.CA != nil`, the same
  predicate `setupRouter` already uses for the enroll routes and `MTLSDeviceAuth`.
- **TLS** (`server.go:610-619`): `addMTLSConfig` sets `ClientCAs = {rIOt CA}` and
  `ClientAuth = VerifyClientCertIfGiven`. Go's TLS server populates
  `ConnectionState.VerifiedChains` only when a presented client certificate verified against
  `ClientCAs` (session resumption restores the verified chains). `PeerCertificates` alone carries
  no proof.
- **`MTLSDeviceAuth`** (`middleware/mtls_auth.go`) uses `PeerCertificates[0]`, CN = device ID,
  and `SerialNumber.Text(16)` against `ListRevokedSerials`. It **fails open** on a list error.
  Serials are stored as `fmt.Sprintf("%x", serial)` (`ca/ca.go:136`), which equals `Text(16)`.
- **`HandlerDeps`/`Handlers`** (`handlers.go:33-131`) have no CA repository today. Tests build
  `&Handlers{...}` literals directly (`heartbeat_compat_test.go:38`, `handlers_test.go:143`).
- **Device repository**: `LookupAPIKey` (`device_repo.go:213`) maps *every* error, including DB
  errors, to `"invalid api key"`. There is no "does the device have keys" method. An index
  `idx_api_keys_device_id` exists (`000001_init.up.sql:30`), and `api_keys` cascades on device
  delete (FRD A-4 confirmed).
- **Telemetry/heartbeat ingest** (`handlers.go:266-409`): the timestamp defaults only when zero,
  using `time.Now().UTC()`. `processTelemetryPostWork` copies `snap.Timestamp` into container
  metrics (`handlers.go:464`). These two handlers are the only `StoreSnapshot`/`StoreHeartbeat`
  call sites.
- **Read queries** (`db/telemetry_repo.go`): `GetLatestSnapshot` (49),
  `GetAllLatestSnapshots` (64), `GetAllLatestSummaries` (89), `GetHistory` (133),
  `GetHeartbeatHistory` (160), `GetFleetHeartbeats` (204), `GetGPUDeviceIDs` (232),
  `GetFleetContainerLeaderboard` (260). No other Go code reads `telemetry_snapshots` or
  `heartbeats`.
- **Clock-injection precedent**: `Handlers.oidcClock func() time.Time` (nil means `time.Now`),
  consumed at `oidc.go:224`, injected by tests (`oidc_handler_test.go:330`).
- **Log-capture precedent**: `captureLogs(t)` in `oidc_handler_test.go:98` (same package,
  reusable).
- **Frontend**: `formatAgo` is a component-local function in `FleetOverview.tsx:142-149`, used at
  `:284` for `last_heartbeat`. It is the **only** relative-age formatter in `web/src`.
  `web/src/utils/format.ts` + `format.test.ts` exist as the home for pure formatters.

### 2.2 What is missing / must change

1. A credential-proof decision for re-registration (FR-001 to FR-016), evaluated before any
   mutation.
2. A CA repository handle on `Handlers` (path b).
3. `DeviceRepository.HasAPIKey` (path c).
4. Constant-time registration-key comparison without colliding with HARDEN-002's
   `registrationKeyMatches`.
5. Ingest clamp, debounced clamp log, injectable ingest clock.
6. Read-side guard on all eight read queries.
7. Negative-age guard in the UI formatter.

---

## 3. Architecture Decisions

### AD-001: Proof logic in a new file as a pure decision function

**Decision:** Create `internal/server/handlers/register_auth.go` with these pieces:

- `decideReRegistration(namedDeviceID string, c regCredentials) regDecision`: **pure**, no I/O,
  no clock. It encodes FR-013 exactly.
- `(h *Handlers) collectRegCredentials(ctx, r *http.Request, namedDeviceID, presentedKey, regKey string) regCredentials`:
  does all the I/O. It calls `LookupAPIKey`, `qualifyingCertDeviceID`, `regKeyEqual`, and
  `HasAPIKey`.
- `qualifyingCertDeviceID(ctx, cs *tls.ConnectionState, caRepo db.CARepository) (deviceID string, state certState)`.
- `regKeyEqual(provided, configured string) bool`.
- Response/log helpers `writeRegReject(w, status int)` and
  `logRegReject(r *http.Request, suppliedDeviceID, class string, status int)`.
- The fixed bodies and rejection-class constants.

`RegisterDevice` changes only by a few lines: one call to collect, one to decide, an early
return on rejection, the mint condition, and the new-device key check.

**Rationale:**

- It keeps the `RegisterDevice` diff small, because HARDEN-002 (`claude/youthful-austin-8d4743`)
  edits the same function and adds `registrationKeyMatches` directly above it.
- FR-013 becomes testable as a plain table over the V-01 to V-11 rows without HTTP plumbing.
- Separating "gather facts" from "decide" makes the no-side-effect property (FR-002) structural:
  the decision runs before the first mutating statement.

**Alternatives Considered:**

- *Inline the logic in `RegisterDevice`*: rejected. The diff and conflict surface grow, and the
  precedence can only be tested through HTTP.
- *A middleware on the register route*: rejected. The proof depends on the parsed body
  (`device_id`) and on whether the device exists, and middleware would have to parse and re-buffer
  the body.

**Consequences:**

- `decideReRegistration` must never be given a hook to call the DB. `HasAPIKey` is resolved
  eagerly in `collectRegCredentials`, and only when `RegKeyMatch` is true, so the query runs only
  when a registration key was presented.
- **HARDEN-002 merge note:** `regKeyEqual` is deliberately *not* named `registrationKeyMatches`.
  Whichever branch lands second:
  1. deletes the old top-of-handler key block (this story removes it; HARDEN-002 rewrote it), and
  2. replaces `regKeyEqual` with HARDEN-002's `registrationKeyMatches`, which has the same
     semantics (sha256 of both values plus `subtle.ConstantTimeCompare`), and deletes
     `regKeyEqual` and its test, or renames that test to target the surviving helper.

### AD-002: Exact FR-013 precedence and the gathered facts

**Decision:** `regCredentials` carries these fields:

| Field | Type | Meaning |
|---|---|---|
| `KeyPresented` | bool | `X-rIOt-Key` header non-empty |
| `KeyDeviceID` | string | Device the presented key resolves to via `LookupAPIKey`; `""` when absent, unknown, or on lookup error |
| `CertDeviceID` | string | CN of a *qualifying* certificate; `""` otherwise |
| `CertState` | `certState` | `certAbsent`, `certQualifying`, `certUnverified`, `certEmptyCN`, `certRevoked`, `certRevocationUnavailable` (used for the log class only) |
| `RegKeyMatch` | bool | Registration key configured **and** `regKeyEqual(presented, regKey)` |
| `DeviceKeyless` | bool | `HasAPIKey` returned `(false, nil)`; computed only when `RegKeyMatch` |
| `KeylessCheckFailed` | bool | `HasAPIKey` returned an error |

`regDecision{Path regPath; MintKey bool; RejectClass string}` with
`regPath ∈ {regReject401, regReject403, regPathOwnKey, regPathCert, regPathRegKey}`.

Precedence (first match wins). `N` is `existing.ID`, the canonical DB ID, not the raw body string:

1. `KeyDeviceID != "" && KeyDeviceID != N` → 403, class `wrong-device-key` (FR-009).
2. `CertDeviceID != "" && CertDeviceID != N` → 403, class `wrong-device-cert` (FR-010).
3. `KeyDeviceID == N` → `regPathOwnKey`, `MintKey=false` (FR-003, V-03, V-05).
4. `CertDeviceID == N` → `regPathCert`, `MintKey=true` (FR-004/005, V-04, V-10 garbage key).
5. `RegKeyMatch && DeviceKeyless` → `regPathRegKey`, `MintKey=true` (FR-006, V-06).
6. Otherwise 401. The class is chosen in this order:
   - `RegKeyMatch && KeylessCheckFailed` → `regkey-keycheck-error`
   - `RegKeyMatch` → `regkey-device-has-keys` (FR-007, V-07)
   - `CertState` is revoked, revocation-unavailable, empty-CN, or unverified → `cert-revoked` /
     `cert-revocation-unavailable` / `cert-empty-cn` / `cert-unverified` (FR-012, V-11)
   - `KeyPresented` → `unknown-key` (FR-011, V-10)
   - else `no-credential` (FR-008, V-08/V-09)

**Rationale:**

- Rules 1 and 2 both precede rule 3, so a mixed-identity request is 403 even when the other
  credential would succeed (PO-1).
- An unresolved key never blocks rule 4 (FR-011).
- The registration key is only consulted after both identity credentials fail (FR-017: the
  registration key check is never applied to path a or b).
- `LookupAPIKey` errors collapse to "unresolved". Because the repo cannot tell not-found from a
  DB error, a DB error can only *remove* a grant. It can never add one (fail closed).

**Alternatives Considered:**

- *Skip `LookupAPIKey` when the header equals the registration key*: rejected. It saves nothing
  meaningful and adds a branch.
- *Checking the key before the cert for 403 purposes only*: equivalent to the chosen order. Rules
  1 and 2 are independent.

**Consequences:** The rejection classes are a closed, documented set (Section 9). The new-device
branch adds one more class, `new-device-regkey-mismatch` (AD-004).

### AD-003: Qualifying certificate = `VerifiedChains`, CN, unrevoked, mTLS wired

**Decision:** `qualifyingCertDeviceID` returns `("", certAbsent)` when any of these hold:

- `caRepo == nil` (mTLS not active for this router)
- `cs == nil` (plain HTTP or a TLS-terminating proxy)
- `len(cs.PeerCertificates) == 0`

It returns `("", certUnverified)` when certificates are present but
`len(cs.VerifiedChains) == 0 || len(cs.VerifiedChains[0]) == 0`. Otherwise it takes
`leaf := cs.VerifiedChains[0][0]` and continues:

- Empty `leaf.Subject.CommonName` → `certEmptyCN`.
- `leaf.SerialNumber == nil` → `certUnverified`.
- `caRepo.ListRevokedSerials(ctx)` error → `certRevocationUnavailable` (**fail closed**, PO-3).
- Serial `leaf.SerialNumber.Text(16)` present in the list → `certRevoked`.
- Else → `(CN, certQualifying)`.

`ListRevokedSerials` is called only when a verified chain with a CN exists.

**Wiring:** add `CARepo db.CARepository` to `HandlerDeps` and `caRepo db.CARepository` to
`Handlers`, assigned in `New`. In `router.go setupRouter`, set `CARepo` **only** when
`s.Config.MTLSEnabled && s.CA != nil` (same predicate as lines 48/133/150). Otherwise leave the
field as an untyped `nil` interface. Because `setupRouter` is the single construction site and is
re-invoked by `applyTLSAndRestart` after the CA loads, the runtime-enable path is covered with no
extra code.

**Rationale:**

- FR-012 requires that "verified by the rIOt server's own handshake" is distinguishable from
  "present". `VerifiedChains` is the only field Go populates exclusively after successful
  verification against `ClientCAs`, which is exactly `{rIOt CA}`.
- Ignoring headers entirely (only `r.TLS` is read) satisfies the forwarded-identity clause.
- A nil `caRepo` when mTLS is off means a stray verified chain could never confer identity on a
  non-mTLS server.

**Alternatives Considered:**

- *`PeerCertificates` like `MTLSDeviceAuth`*: rejected. That only holds under the
  `addMTLSConfig` invariant and gives no defense if `ClientAuth` is ever set without `ClientCAs`
  (Go would then verify against system roots).
- *Also pass the CA certificate and compare chain roots*: rejected for now. It adds a dependency
  for a case that cannot occur while `addMTLSConfig` is the only `ClientAuth` writer, and public
  CAs do not issue UUID CNs. This is recorded as residual in Section 10.
- *Reusing `MTLSDeviceAuth`*: rejected. It fails open on revocation-list errors, rejects requests
  with no certificate, and requires the device to exist.

**Consequences:**

- On servers where mTLS is enabled but the listener does not request certificates (plain HTTP
  after setup, or autocert, see F-3), path (b) is unavailable. Enrolled devices on a
  registration-key server still obtain a key via path (c) (FR-018).
- **Typed-nil gotcha:** assign `s.CARepo` into the interface only inside the `if`. Never assign a
  possibly-nil `*db.CARepo` unconditionally.

### AD-004: Handler ordering; new-device gate; uniform bodies; body cap

**Decision:** The new `RegisterDevice` order:

1. `regKey, _ := h.adminRepo.GetConfig(ctx, "registration_key")`. This is unchanged and read once.
2. `r.Body = http.MaxBytesReader(w, r.Body, maxRegistrationBodyBytes)`, with
   `const maxRegistrationBodyBytes = 1 << 20` declared in `register_auth.go`.
3. Decode the body. On error, return 400 with the existing `{"error":"invalid request body"}`.
   This happens before any credential evaluation (FR-023, PO-6).
4. `presented := r.Header.Get("X-rIOt-Key")`.
5. If `reg.DeviceID != ""` and `FindByDeviceUUID` returns a device:
   - collect credentials, then decide;
   - on reject: `logRegReject`, `writeRegReject`, return. **No statement before this point
     mutates `existing` or calls any repo writer, the hub, or `eventGen`.**
   - on accept: run the existing metadata-update block unchanged. Then, if `dec.MintKey`,
     generate and store a key using the existing mint block with its condition changed from
     `agentKey == ""` to `dec.MintKey`. The 200 response, broadcast, and `DeviceOnline` logic are
     unchanged.
6. New device: if `regKey != "" && !regKeyEqual(presented, regKey)`, log (class
   `new-device-regkey-mismatch`), write the uniform 401, and return. Otherwise the existing
   creation path is unchanged (201).

The fixed bodies, written via `http.Error` exactly like the rest of the file:

- 401: `{"error":"unauthorized"}` (const `regBodyUnauthorized`)
- 403: `{"error":"forbidden"}` (const `regBodyForbidden`)

**Rationale:**

- Steps 5 and 6 together implement FR-017 (the old top-of-handler key gate is gone) and FR-022
  (a device key on an unknown UUID never reaches `LookupAPIKey`).
- One 401 body constant makes AC-014 byte-identity hold by construction.
- The 1 MiB cap exists because PO-6 moves decoding *ahead* of authentication. Without it, an
  unauthenticated caller could make the server parse arbitrarily large bodies on a registration-key
  server where today the request is refused before decode. The real payload (hostname, arch,
  version, tags, hardware profile) is a few KB.

**Alternatives Considered:**

- *Keeping a top-of-handler key gate that exempts own-key callers*: rejected. It needs the
  device lookup before the gate anyway, and it duplicates precedence.
- *A JSON 401 with a reason code*: rejected by FR-014.

**Consequences:**

- The old `"invalid registration key"` text disappears (PO-5). The agent only prints the body
  (`http_client.go:147-149`).
- When no registration key is configured, unknown-UUID requests still return 201, so UUID
  existence is distinguishable (accepted, NFR-004).
- A caller holding a *valid fleet key* can distinguish existing UUIDs (403) from unknown ones
  (401 on a registration-key server). This is outside NFR-004's "unauthenticated" scope and is
  recorded in Section 10.

### AD-005: `DeviceRepository.HasAPIKey`

**Decision:** Add `HasAPIKey(ctx context.Context, deviceID string) (bool, error)` to
`DeviceRepository`. Implement it in `DeviceRepo` as a single
`SELECT EXISTS(SELECT 1 FROM api_keys WHERE device_id=$1)`, served by `idx_api_keys_device_id`.
Implement it in `MockDeviceRepo` by scanning `APIKeys` values under `RLock`. The mock honors `Err`
and a new targeted field `HasAPIKeyErr error` so tests can fail only this call.

**Rationale:** Path (c) needs exactly a boolean. `EXISTS` stops at the first row. A count or list
method would invite misuse.

**Alternatives Considered:**

- *`CountAPIKeys`*: rejected; more than needed.
- *Inferring keylessness from `LookupAPIKey`*: impossible, because keys are hashed.

**Consequences:**

- `DeviceRepo` and `MockDeviceRepo` are the only two implementers (verified), so the interface
  change compiles cleanly once both are updated.
- An error → `KeylessCheckFailed` → 401 (fail closed).

### AD-006: Ingest clamp with injectable clock

**Decision:** Create `internal/server/handlers/ingest_clamp.go` containing:

- `clampFutureTimestamp(ts, now time.Time) (time.Time, bool)`:
  - zero `ts` → `(now, false)` (FR-033 default);
  - `ts.After(now.Add(db.FutureSkewAllowance))` → `(now, true)`;
  - else `(ts, false)`.

  The boundary is inclusive: exactly `now+5m` is kept (PO-8).
- `(h *Handlers) ingestNow() time.Time`: `h.ingestClock()` if it is non-nil, else `time.Now()`,
  always `.UTC()`.
- New `Handlers` field `ingestClock func() time.Time`, documented like `oidcClock` (nil means
  production `time.Now`).
- `(h *Handlers) noteClamp(deviceID, kind string, sent, received time.Time)`, with kinds
  `"snapshot"` / `"heartbeat"` as consts.

In `Heartbeat` and `Telemetry`, immediately after a successful decode, take
`received := h.ingestNow()` and replace the zero-default block with the clamp call. If the clamp
fired, call `noteClamp(deviceID, kind, original, received)`. In `Telemetry` this happens **before**
the size check, `StoreSnapshot`, and the `processTelemetryPostWork` goroutine, so container
metrics derived from the snapshot carry the same, clamped, timestamp.

**Rationale:**

- One function serves both handlers (FR-030).
- The `oidcClock` pattern is the established, minimal injection seam. It also works with the
  struct-literal `&Handlers{}` used throughout the tests.
- Clamping (not rejecting) satisfies FR-034/NFR-007 because the response path is untouched.

**Alternatives Considered:**

- *Clamping in the repo layer*: rejected. It cannot log per device and kind without leaking
  handler concerns.
- *A package-level clock var*: rejected. It is global mutable test state.

**Consequences:**

- Container metrics inherit the clamped snapshot time. This is a consequence of clamping the
  snapshot, not new validation of the container-metric path (FRD §9 item 7 stays out of scope).
- `devices.last_telemetry` and `last_heartbeat` remain DB `NOW()` (FR-040, untouched).

### AD-007: Bounded, debounced clamp logging

**Decision:** `clampLogLimiter`, in `ingest_clamp.go`, has these fields:

- `mu sync.Mutex`
- `entries map[clampKey]*clampEntry`, where `clampKey{deviceID, kind string}` and
  `clampEntry{lastLogged time.Time; suppressed int}`
- `lastSweep time.Time`

Its method `observe(deviceID, kind string, now time.Time) (emit bool, clampsSinceLast int)` works
like this:

- No entry, or `now.Sub(e.lastLogged) >= clampLogWindow` (`const clampLogWindow = time.Hour`):
  emit, with `clampsSinceLast = e.suppressed + 1` (1 for a new entry). Then reset `suppressed`
  to 0 and set `lastLogged = now`.
- Otherwise `suppressed++`, no emit.
- Opportunistic sweep, at most once per `clampLogWindow` (checked inside `observe`): delete
  entries with `suppressed == 0 && now.Sub(lastLogged) >= clampLogWindow`, and any entry with
  `now.Sub(lastLogged) >= 24h`.
- The method is **nil-receiver safe**: a nil limiter returns `(true, 1)`, so tests that build
  `&Handlers{}` without it still work.

`New()` sets `clampLog: newClampLogLimiter()`. On emit, `noteClamp` logs:

`slog.Warn("future-dated timestamp clamped", "device_id", id, "kind", kind, "skew", (sent-received).String(), "skew_seconds", int64, "clamps_since_last", n)`

**Rationale:**

- FR-035 and PO-9 call for at most one entry per device and kind per rolling hour.
- The entry carries the skew and the number of clamps since the previous entry, including the
  current one.
- Keys come from URL device IDs that already passed `DeviceAuth`, so the map is bounded by
  devices × 2 (NFR-009). The sweep removes entries for deleted or long-quiet devices so the map
  does not keep stale keys.
- The state is lost on restart, which is accepted.

**Alternatives Considered:**

- *A global rate limiter*: rejected. It is not per device and kind, so one skewed device would
  mask another.
- *An LRU cap*: rejected. The map is already bounded by authenticated devices, and the sweep is
  simpler.

**Consequences:** When the sweep drops a pending suppressed count (older than 24h with no new
clamp), that count is never reported. This is acceptable because it describes stale history.

### AD-008: Read-side guard on all eight read queries via one SQL constant (deviation recorded)

**Decision:** In `db/telemetry_repo.go`, add:

- `const FutureSkewAllowance = 5 * time.Minute`, exported and used by the handlers' clamp, so
  there is one Go source of truth;
- `const notFutureDatedSQL = "timestamp <= NOW() + interval '5 minutes'"`.

Splice the fragment into all eight read queries:

| Query | Where the predicate goes |
|---|---|
| `GetLatestSnapshot` | `WHERE device_id=$1 AND <guard>` |
| `GetAllLatestSnapshots` | `WHERE <guard>` before `ORDER BY` |
| `GetAllLatestSummaries` | `WHERE <guard>` inside the `DISTINCT ON` subquery |
| `GetGPUDeviceIDs` | `WHERE <guard>` inside the `DISTINCT ON` subquery |
| `GetFleetContainerLeaderboard` | `WHERE <guard>` inside the `DISTINCT ON` subquery (before the outer `WHERE data ? 'docker'`) |
| `GetHistory` | `AND <guard>` |
| `GetHeartbeatHistory` | `AND <guard>` |
| `GetFleetHeartbeats` | `AND <guard>` |

In the three subquery cases the guard must be inside the subquery, before `DISTINCT ON`
selection. Placed outside, it would drop the device instead of falling back to its newest valid
row. Repository signatures are unchanged, so interfaces and mocks are not touched. A pure unit test
asserts that the SQL literal and the Go constant agree.

**Deviation from orchestrator guidance (keep the guard to latest-selection only):** the guard
**also** covers the three history/window reads. The reasons:

1. FR-037 and AC-033 are final FRD requirements (PO-10). Leaving them out would ship a failing AC.
2. The blast radius on legitimate data is provably zero. After this story every newly written row
   has `timestamp ≤ R + 5m`, where `R` is the receive time. At read time `Q ≥ R`, so
   `Q + 5m ≥ R + 5m`, and no clamped or as-sent row is ever excluded. The guard only ever hides
   pre-existing far-future rows.
3. The fleet dashboard window (`FleetHeartbeats`, `since = now − ≤60m`, `until = now`) keeps
   exactly today's semantics, including rows up to 5 minutes ahead.
4. On the heartbeat queries the predicate turns an open-ended `timestamp >= $since` into a
   bounded range, which is no worse for the planner.

**Rationale for using `NOW()` in SQL rather than a Go-bound parameter:** it avoids changing eight
repository signatures, `TelemetryRepository`, `MockTelemetryRepo`, and every caller. In the
reference docker-compose deployment the app and Postgres share a host clock, so the clocks agree. Any cross-host skew is absorbed
by the same 5-minute tolerance. `NOW()` is stable within a statement, so under PART-001
partitioning it allows runtime pruning of future partitions.

**Alternatives Considered:**

- *A Go parameter (`$n`)*: rejected; signature churn (above).
- *Migration, index, or row cleanup*: forbidden (FR-038, NFR-012).
- *Latest-only guard*: rejected; it fails AC-033.

**Consequences:**

- A device whose *only* rows are future-dated temporarily has no "latest" snapshot until it posts
  again. Its next post is clamped and becomes latest.
- OQ-3: a stored future row reappears once real time reaches its date minus 5 minutes (accepted).
- These SQL changes cannot be unit-tested in this repo (no DB harness). They are covered by
  inspection and a manual dev-DB check (Section 8).

### AD-009: Frontend: extract `formatAgo` to `utils/format.ts` and clamp

**Decision:** Move the function to `web/src/utils/format.ts` as
`export function formatAgo(ts?: string, now: number = Date.now()): string`. The body is identical
except `const diff = Math.max(0, now - new Date(ts).getTime())`. `FleetOverview.tsx` deletes the
local function and imports `formatAgo` from `../utils/format`. The single call site at `:284` is
unchanged.

**Rationale:**

- A component-local closure is not unit-testable without rendering the full page.
- `format.ts` with `format.test.ts` is the existing home for pure formatters.
- The injectable `now` keeps the test deterministic.
- The zero-age text is `"0s ago"`, which is what the formatter already produces for `diff = 0`
  (FR-039, PO-11).

**Alternatives Considered:** *Clamp in place, no extraction*: rejected. AC-035 would then need a
full-page render test with mocked queries for a one-line change.

**Consequences:** Invalid-date behavior (`NaN`) is unchanged: `Math.max(0, NaN)` is `NaN`, the same
output as today. That keeps the change minimal (FRD §9 item 12).

---

## 4. Component Changes

| Action | File Path | Purpose |
|--------|-----------|---------|
| CREATE | `internal/server/handlers/register_auth.go` | Proof decision (pure), credential collection, qualifying-cert check, `regKeyEqual`, fixed 401/403 bodies, reject logging, body-size const |
| MODIFY | `internal/server/handlers/handlers.go` | `HandlerDeps.CARepo`; `Handlers.caRepo`, `ingestClock`, `clampLog`; `New` wiring; `RegisterDevice` reorder (AD-004); `Heartbeat`/`Telemetry` clamp calls |
| CREATE | `internal/server/handlers/ingest_clamp.go` | `clampFutureTimestamp`, `ingestNow`, `noteClamp`, `clampLogLimiter` |
| MODIFY | `internal/server/router.go` | Pass `CARepo` into `HandlerDeps` only when `s.Config.MTLSEnabled && s.CA != nil` |
| MODIFY | `internal/server/db/interfaces.go` | Add `HasAPIKey(ctx, deviceID) (bool, error)` to `DeviceRepository` |
| MODIFY | `internal/server/db/device_repo.go` | Implement `HasAPIKey` (`SELECT EXISTS`) |
| MODIFY | `internal/server/db/telemetry_repo.go` | `FutureSkewAllowance`, `notFutureDatedSQL`; guard spliced into 8 read queries |
| CREATE | `internal/server/db/telemetry_repo_guard_test.go` | Pure test: SQL interval literal matches `FutureSkewAllowance` |
| MODIFY | `internal/testutil/mocks.go` | `MockDeviceRepo.HasAPIKey` + `HasAPIKeyErr` field |
| CREATE | `internal/server/handlers/register_auth_test.go` | Decision table, `regKeyEqual`, `qualifyingCertDeviceID` unit tests |
| CREATE | `internal/server/handlers/register_device_test.go` | `RegisterDevice` handler tests (AC-001 to AC-022, except AC-018) |
| CREATE | `internal/server/handlers/ingest_clamp_test.go` | Clamp + limiter unit tests; Telemetry/Heartbeat handler clamp tests |
| MODIFY | `web/src/utils/format.ts` | Add exported `formatAgo(ts?, now?)` with negative clamp |
| MODIFY | `web/src/utils/format.test.ts` | `formatAgo` tests incl. AC-035 |
| MODIFY | `web/src/pages/FleetOverview.tsx` | Remove local `formatAgo`; import from `../utils/format` |
| MODIFY (tech writer) | `README.md` | Registration/auth section (≈ lines 30, 184-194, 342): re-registration proof rules; new "Recovering an agent rejected at re-registration" subsection (FR-027, both alternatives, `server.api_key`, `/var/lib/riot/device-id`, mTLS self-recovery) |
| MODIFY (tech writer) | `CHANGELOG.md` | Behavior change: keyless re-registration of an existing device is rejected (401), registration-key-only re-registration of a keyed device rejected, Bug C/D fixed, future timestamps clamped; pointer to README recovery (FR-028) |

No other files. `server.go`, `middleware/*`, `enroll.go`, the agent, `install.sh`, and migrations
are **not** touched.

### Per-file specifics

**`handlers/register_auth.go`** (new). Contracts only:

- `const maxRegistrationBodyBytes = 1 << 20`
- `const regBodyUnauthorized = `{"error":"unauthorized"}``, `const regBodyForbidden = `{"error":"forbidden"}``
- Reject-class string consts: `no-credential`, `unknown-key`, `wrong-device-key`,
  `wrong-device-cert`, `regkey-device-has-keys`, `regkey-keycheck-error`, `cert-revoked`,
  `cert-revocation-unavailable`, `cert-empty-cn`, `cert-unverified`,
  `new-device-regkey-mismatch`.
- `type regPath int`, `type certState int`, `type regCredentials struct{...}`,
  `type regDecision struct{...}` (fields per AD-002).
- `func decideReRegistration(namedDeviceID string, c regCredentials) regDecision` (pure).
- `func (h *Handlers) collectRegCredentials(ctx context.Context, r *http.Request, namedDeviceID, presentedKey, regKey string) regCredentials`
  - calls `h.devices.LookupAPIKey` only when `presentedKey != ""`;
  - always calls `qualifyingCertDeviceID(ctx, r.TLS, h.caRepo)`;
  - sets `RegKeyMatch = regKey != "" && regKeyEqual(presentedKey, regKey)`;
  - only if `RegKeyMatch`, calls `h.devices.HasAPIKey(ctx, namedDeviceID)`.
- `func qualifyingCertDeviceID(ctx context.Context, cs *tls.ConnectionState, caRepo db.CARepository) (string, certState)` (AD-003).
- `func regKeyEqual(provided, configured string) bool`: `configured == ""` → false. Else
  `sha256.Sum256` both and `subtle.ConstantTimeCompare(p[:], c[:]) == 1`. Doc comment:
  "collapse into HARDEN-002 `registrationKeyMatches` on merge".
- `func writeRegReject(w http.ResponseWriter, status int)`: `http.Error` with the fixed body for
  401 or 403.
- `func logRegReject(r *http.Request, suppliedDeviceID, class string, status int)`:
  `slog.Warn("device registration rejected", "device_id", truncate(suppliedDeviceID, 64), "client", middleware.ClientIP(r), "class", class, "status", status)`.
  It **never** receives the header value or registration key as an argument.

**`handlers/handlers.go`**:

- `HandlerDeps`: add `CARepo db.CARepository` with the comment "REG-HARDEN-001: nil unless mTLS
  is active; nil ⇒ certificate proof unavailable".
- `Handlers`: add `caRepo db.CARepository`, `ingestClock func() time.Time` (doc like `oidcClock`),
  and `clampLog *clampLogLimiter`.
- `New`: `caRepo: deps.CARepo`, `clampLog: newClampLogLimiter()`.
- `RegisterDevice`: delete lines 156-164. Insert the `MaxBytesReader` line before decode. After
  `FindByDeviceUUID` succeeds, insert collect, decide, and the reject early-return before line
  179. Change the line 199-200 mint condition to `dec.MintKey`, removing the `agentKey` variable.
  Insert the new-device registration-key check before line 224. Nothing else changes.
- `Heartbeat`: replace lines 275-277 with `received := h.ingestNow()`, then the clamp and
  `noteClamp`.
- `Telemetry`: replace lines 341-343 the same way (kind `snapshot`).

**`router.go`**: before `handlers.New(...)`, declare `var regCARepo db.CARepository` and set
`regCARepo = s.CARepo` inside `if s.Config.MTLSEnabled && s.CA != nil`. Pass
`CARepo: regCARepo`. Add the `db` import if it is not already present.

**`db/telemetry_repo.go`**: add the two consts with comments referencing REG-HARDEN-001
FR-030/036/037. Splice the fragment by string concatenation into the eight query literals per
AD-008. The `time` import already exists.

**`db/device_repo.go`**: `HasAPIKey` beside `DeleteAPIKeysByDevice`, wrapping DB errors with
`fmt.Errorf("has api key: %w", err)`.

**`testutil/mocks.go`**: add `HasAPIKeyErr error` to `MockDeviceRepo` and implement `HasAPIKey`.
Return `m.Err` if set, then `HasAPIKeyErr` if set, else whether any `APIKeys` value equals
`deviceID`.

---

## 5. Data Model Changes

**None at the schema level.** No migration, no DDL, no `UPDATE`/`DELETE` on any telemetry or
heartbeat row (FR-038, NFR-012).

Code-level additions:

- `DeviceRepository` interface: `+ HasAPIKey(ctx context.Context, deviceID string) (bool, error)`.
- `HandlerDeps`: `+ CARepo db.CARepository`.
- `Handlers`: `+ caRepo`, `+ ingestClock`, `+ clampLog`.
- In-memory only: `clampLogLimiter.entries` (bounded, AD-007).

---

## 6. API / Interface Contract

### `POST /api/v1/devices/register` (route, rate limit, and success shapes unchanged)

**Request:** headers `Content-Type: application/json` and optional `X-rIOt-Key: <string>`.
Optional TLS client certificate. The body is unchanged (`models.DeviceRegistration`: `hostname`,
`arch`, `agent_version?`, `tags?`, `device_id?`, `hardware_profile`). Maximum body size is 1 MiB.

| Status | When | Body |
|---|---|---|
| 201 | New device (V-20, V-21) | `{"device_id":string,"short_id":string,"api_key":string(non-empty)}` (unchanged) |
| 200 | Re-registration via path (a) | same shape, `api_key: ""` |
| 200 | Re-registration via path (b) or (c) | same shape, `api_key` non-empty, newly stored |
| 400 | Body unparseable or over 1 MiB | `{"error":"invalid request body"}` (unchanged text) |
| 401 | Any rejection in FR-013 rule 5, or new-device registration-key mismatch | `{"error":"unauthorized"}`, byte-identical for every cause |
| 403 | Wrong-device key or certificate (rules 1 and 2) | `{"error":"forbidden"}` |
| 429 | Rate limit (unchanged middleware) | unchanged |
| 500 | `Update` / `Create` / `StoreAPIKey` failure after authorization | unchanged texts (`failed to update device`, `failed to store API key`, `failed to create device`, `failed to store api key`) |

401/403 bodies are written with `http.Error`, as elsewhere in the file. They contain no device
ID, short ID, hostname, key material, or reason.

### `POST /api/v1/devices/{id}/telemetry` and `/heartbeat`

Request and response contracts are unchanged. A timestamp more than `R + 5m` is stored as `R`. The
response is identical whether or not the timestamp was clamped.

### Read endpoints

Response shapes are unchanged. Rows with `timestamp > NOW() + 5m` are omitted from every
snapshot/heartbeat read listed in AD-008.

### Internal Go contracts (new)

```
DeviceRepository.HasAPIKey(ctx context.Context, deviceID string) (bool, error)
decideReRegistration(namedDeviceID string, c regCredentials) regDecision
qualifyingCertDeviceID(ctx context.Context, cs *tls.ConnectionState, caRepo db.CARepository) (string, certState)
regKeyEqual(provided, configured string) bool
clampFutureTimestamp(ts, now time.Time) (time.Time, bool)
(*clampLogLimiter).observe(deviceID, kind string, now time.Time) (emit bool, clampsSinceLast int)
db.FutureSkewAllowance = 5 * time.Minute
```

### Frontend

`export function formatAgo(ts?: string, now: number = Date.now()): string` in
`web/src/utils/format.ts`.

---

## 7. Sequence / Flow

### 7.1 Registration

1. The rate limiter admits the request (unchanged).
2. Read `regKey` from admin config. On error, treat it as `""`.
3. Cap the body at 1 MiB and decode. On failure, return 400. No repo, hub, or event calls have
   happened.
4. `presented := X-rIOt-Key`.
5. If `device_id` is non-empty and `FindByDeviceUUID` finds `existing`:
   1. `creds := collectRegCredentials(ctx, r, existing.ID, presented, regKey)`. This performs
      only reads: `LookupAPIKey`, `ListRevokedSerials`, and `HasAPIKey` when the registration key
      matched.
   2. `dec := decideReRegistration(existing.ID, creds)`.
   3. If `dec` is a reject: `logRegReject(...)`, then `writeRegReject(w, 401|403)`, then return.
   4. Otherwise apply the metadata to `existing` and call `devices.Update` (500 on error).
   5. If `dec.MintKey`: `generateAPIKey`, then `StoreAPIKey` (500 on error), then log at info
      "generated API key for device" with `device_id` and `path`. Never log the key.
   6. Write 200 `{device_id, short_id, api_key}`, broadcast the device update, and emit
      `DeviceOnline` if the device was not online (unchanged).
6. Else (new device): if `regKey != ""` and `!regKeyEqual(presented, regKey)`, log and return 401.
   Otherwise run the existing creation path (201).

### 7.2 Ingest (heartbeat or telemetry)

1. `DeviceAuth` (and `MTLSDeviceAuth` when enabled) as today.
2. Decode (400 unchanged). `received := h.ingestNow()`.
3. `ts, clamped := clampFutureTimestamp(sent, received)`. Assign `ts`. If `clamped`, call
   `noteClamp`, which calls `observe` and logs if it emits.
4. Continue unchanged: size guard, store, update device time, broadcast, respond.

### 7.3 Read

Each query evaluates `timestamp <= NOW() + interval '5 minutes'` before `DISTINCT ON`, `ORDER BY`,
or `LIMIT` selection.

---

## 8. Acceptance Criteria Mapping

Test files:

- `RD` = `internal/server/handlers/register_device_test.go`
- `RA` = `internal/server/handlers/register_auth_test.go`
- `IC` = `internal/server/handlers/ingest_clamp_test.go`
- `DG` = `internal/server/db/telemetry_repo_guard_test.go`
- `FT` = `web/src/utils/format.test.ts`

**Harness conventions:**

- Handler tests build `&Handlers{devices, telemetry, adminRepo, caRepo, hub, eventGen}` from
  `testutil` mocks, following `newHeartbeatTestHandlers`. A helper `newRegisterTestHandlers(t)`
  returns the handlers plus the mock device, admin, CA, and event repos.
- **Rejection tests set `hub: nil` and `eventGen: nil`.** Any broadcast or event emission on a
  rejection path then panics and fails the test, which proves FR-002's "no broadcast, no event"
  without WebSocket plumbing.
- "Unchanged" is asserted by deep-comparing `devRepo.Devices[id]` and a copy of `devRepo.APIKeys`
  taken before the request.
- mTLS requests set `req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}, VerifiedChains: [][]*x509.Certificate{{leaf}}}`,
  where `leaf` is an `x509.Certificate` literal with `Subject.CommonName` and
  `SerialNumber: big.NewInt(n)`, mirroring `makeSelfSignedCert` in `mtls_auth_test.go`. Revoked
  serials go into `MockCARepo.Certs` as `{SerialNumber: fmt.Sprintf("%x", n), Revoked: true}`.
- The registration key is set via `adminRepo.Config["registration_key"]`.

| AC | Fulfilled By | Test (Go / vitest) |
|----|--------------|--------------------|
| AC-001 | AD-002 rule 3; mint gated on `dec.MintKey` | RD `TestRegisterDevice_AC001_OwnKeyOpenServer_NoNewKey`: 200, `api_key==""`, hostname updated, status online, `APIKeys` has exactly K |
| AC-002 | AD-004 (top gate removed) + rule 3 | RD `TestRegisterDevice_AC002_OwnKeyWithRegKeyConfigured_Accepted` (Bug C regression) |
| AC-003 | Rule 1; reject before mutation | RD `TestRegisterDevice_AC003_WrongDeviceKey_403NoSideEffects`: body == `regBodyForbidden`+"\n", contains neither ID nor hostname; D1 and the key set unchanged; nil hub/eventGen |
| AC-004 | Rule 6 `no-credential`; reject before mutation | RD `TestRegisterDevice_AC004_NoKeyNoRegKey_Rejected`: offline D stays offline; no `api_key` field value; key set unchanged; nil hub/eventGen |
| AC-005 | Rule 6 `unknown-key` | RD `TestRegisterDevice_AC005_GarbageKey_NoMetadataOverwrite` |
| AC-006 | Rule 5 + `HasAPIKey` | RD `TestRegisterDevice_AC006_RegKeyKeylessDevice_IssuesKey`: 200, non-empty key; then the key passes `middleware.DeviceAuth(devRepo)` wrapping `h.Telemetry` on `/api/v1/devices/{id}/telemetry` → 200 |
| AC-007 | Rule 6 `regkey-device-has-keys`; `HasAPIKey` error ⇒ fail closed | RD `TestRegisterDevice_AC007_RegKeyDeviceHasKeys_401`; RD `TestRegisterDevice_AC007_HasAPIKeyError_FailsClosed` (`HasAPIKeyErr` set) |
| AC-008 | AD-003 + rule 4 | RD `TestRegisterDevice_AC008_MTLSEnrolledDevice_CertIssuesKey`, subtests `[AC-008] no key`, `[AC-008] regkey presented`, `[AC-008] unknown value`, each with and without a registration key configured |
| AC-009 | Rule 3 precedes rule 4 | RD `TestRegisterDevice_AC009_CertPlusOwnKey_NoNewKey` |
| AC-010 | Rule 4; no key deletion | RD `TestRegisterDevice_AC010_CertPath_KeepsPriorKeys`: K and K' both resolve to D via `LookupAPIKey` |
| AC-011 | AD-003 `certRevoked` / `certUnverified` / `certEmptyCN` / caRepo nil | RD `TestRegisterDevice_AC011_RevokedCert_401`; RA `TestQualifyingCert_AC011_NoIdentityCases`, subtests: revoked, empty CN, `PeerCertificates` without `VerifiedChains`, nil `TLS`, nil `caRepo` with a verified chain |
| AC-012 | Rule 2 | RD `TestRegisterDevice_AC012_CertForOtherDevice_403`, subtests with no key, a garbage key, and D1's own key (PO-1: still 403) |
| AC-013 | AD-003 `certRevocationUnavailable` | RD `TestRegisterDevice_AC013_RevocationListError_FailsClosed` (`MockCARepo.Err` set) |
| AC-014 | Single `regBodyUnauthorized` const for all 401s | RD `TestRegisterDevice_AC014_Uniform401Body`: requests (i) to (iv) produce identical status and body bytes; the body contains no ID, hostname, key, or class string |
| AC-015 | New-device path unchanged | RD `TestRegisterDevice_AC015_OpenNewDeviceRegistration_201` (no `device_id`; unknown `device_id`) |
| AC-016 | AD-004 step 6 | RD `TestRegisterDevice_AC016_NewDeviceRegKey` (RK → 201; missing or wrong → 401, `Devices` size unchanged) |
| AC-017 | AD-004 step 6 (no `LookupAPIKey` on the new-device path) | RD `TestRegisterDevice_AC017_DeviceKeyNotRegKeyForUnknownUUID` |
| AC-018 | `regKeyEqual` (sha256 + `subtle.ConstantTimeCompare`); no other comparison against the registration key | RA `TestRegKeyEqual_AC018_ConstantTimeCompare` (equal; differs in the first and last byte; different lengths; empty configured → false). **Inspection:** `grep` for `regKey` shows no `==`/`!=` against it |
| AC-019 | AD-004 step 3 | RD `TestRegisterDevice_AC019_MalformedBody_400NoSideEffects` (malformed JSON; >1 MiB body), with and without a registration key; nil hub/eventGen |
| AC-020 | `logRegReject` | RD `TestRegisterDevice_AC020_RejectionLogsWithoutSecrets`: `captureLogs(t)`; one request per class (`no-credential`, `unknown-key`, `wrong-device-key`, `wrong-device-cert`, `regkey-device-has-keys`); exactly one warn line each with `device_id`, `client`, `class`; the buffer never contains the presented key, the registration key, or any stored key |
| AC-021 | `RotateKey` unchanged + rule 3 | RD `TestRegisterDevice_AC021_RotateKeyThenOwnKeyReRegisters`: a rejected agent, then `h.RotateKey` via a chi route, then re-registration with the new key → 200 `api_key==""`; the old key no longer resolves |
| AC-022 | New-device path | RD `TestRegisterDevice_AC022_NoDeviceID_RegistersAsNewDevice`: 201 with a new ID; the original device unchanged; with a registration key the request needs RK |
| AC-023 | README + CHANGELOG (tech writer) | **Doc review** (QA): both FR-027 alternatives, `server.api_key`, `/var/lib/riot/device-id`, mTLS self-recovery; CHANGELOG entry |
| AC-024 | All of the above; no agent diff | RD `TestRegisterDevice_AC024_ReleasedAgentRequestShapes` (table that replays the exact header/body combinations `internal/agent/register.go` sends for each flow). **QA manual:** released agent binary against a dev server for all five flows |
| AC-025 | AD-006 | IC `TestTelemetry_AC025_FutureSnapshotClampedAndAccepted`: `ingestClock` fixed at R; timestamp R+24h; 200 `{"status":"ok"}`; stored `Timestamp.Equal(R)` |
| AC-026 | Clamp on write (AD-006) + guard (AD-008) | IC `TestTelemetry_AC026_FutureSnapshotNeverLatest`: post R1+1y at clock R1, then S at R2>R1; stored timestamps R1 and T_S > R1, so `ORDER BY timestamp DESC` selects S. **SQL half:** QA manual (Section 8.1) |
| AC-027 | `clampFutureTimestamp` pass-through | IC `TestTelemetry_AC027_PastSnapshotStoredAsSent`: T0−2h stored exactly; the T0 row has the max timestamp. **SQL ordering:** inspection |
| AC-028 | Inclusive boundary | IC `TestClampFutureTimestamp_AC028_Boundaries` (R+4m kept; R+5m kept; R+5m+1ns → R; R+6m → R; zero → R); IC `TestTelemetry_AC028_WithinAllowanceKept` (handler) |
| AC-029 | AD-006 in `Heartbeat` | IC `TestHeartbeat_AC029_FutureHeartbeatClamped` |
| AC-030 | Pass-through for past heartbeats; shared constant | IC `TestHeartbeat_AC030_PastHeartbeatStoredAsSent`; DG `TestFutureSkewSQL_AC030_MatchesAllowance` |
| AC-031 | AD-007 | IC `TestClampLogLimiter_AC031_DebouncePerDeviceKind` (pure: 100 observes per kind in 1h → 1 emit each; at +1h the next emits with `clampsSinceLast==100`; kinds and devices independent); IC `TestHeartbeat_AC031_ClampLogDebounced` (handler + `captureLogs`, 100 future heartbeats → 1 line; advance clock 1h → 1 more line with `clamps_since_last`); IC `TestClampLogLimiter_NFR009_SweepBounded` |
| AC-032 | AD-008 guard in 5 latest queries | **QA manual dev-DB** (Section 8.1) + code inspection that each of the 5 queries places the guard inside the `DISTINCT ON` subquery or `WHERE` |
| AC-033 | AD-008 guard in 3 history/window queries | **QA manual dev-DB** (Section 8.1) + inspection |
| AC-034 | No migration; no UPDATE/DELETE added | **Inspection:** `git diff --stat` shows nothing under `cmd/riot-server/migrations/`; no new `UPDATE`/`DELETE` statements in `telemetry_repo.go` |
| AC-035 | AD-009 | FT `describe('formatAgo')`: `it('[AC-035] renders a future timestamp as zero age with no minus sign')` (`formatAgo(future, now) === '0s ago'`, no `-`), plus regression cases: `undefined → 'Never'`, 30s, 5m, 3h, 2d |
| AC-036 | `router.go:131` unchanged | Existing `internal/server/middleware/ratelimit_test.go`; **inspection** that the register route line and `NewRateLimiter(10, 10)` are untouched |
| AC-037 | All of the above | CI: `go build ./...`, `go test ./...`, `cd web && npm run test:run`; `go.mod`/`package.json` diff empty |

Pure decision table: RA `TestDecideReRegistration_AC003_AC012_DecisionTable`. Its subtests are
named after FRD rows, for example `"[AC-003] V-01 key->other => 403"`,
`"[AC-012] V-02 cert->other => 403"`, `"[AC-001] V-03 key->this => own-key, no mint"`,
`"[AC-008] V-04 cert->this, no key => cert, mint"`, `"[AC-009] V-05 cert+key->this => own-key"`,
`"[AC-006] V-06 RK keyless => regkey, mint"`, `"[AC-007] V-07 RK has keys => 401"`,
`"[AC-004] V-08 nothing open => 401 no-credential"`, `"[AC-014] V-09 nothing RK => 401"`,
`"[AC-005] V-10 unknown key => 401 unknown-key"`, `"[AC-011] V-11 revoked only => 401"`,
`"[AC-012] PO-1 key->this + cert->other => 403"`, `"[AC-003] PO-1 key->other + cert->this => 403"`,
`"[AC-007] RK + keycheck error => 401"`. Each row asserts `Path`, `MintKey`, and `RejectClass`.

### 8.1 QA manual dev-DB procedure (AC-026 SQL half, AC-032, AC-033)

Run this on a **disposable dev database only**, never on a production deployment (FR-038 / no-delete
rule).

1. Insert one snapshot row with `timestamp = NOW() + interval '1 year'` and one with
   `NOW() - interval '1 hour'` for device D. Insert heartbeat rows at `NOW() + interval '1 day'`
   and `NOW() - interval '5 minutes'`.
2. Call each endpoint and confirm the past-dated row is used and the future-dated row is absent:
   device detail (latest), fleet overview / patch-status (summaries), `/api/v1/fleet/heartbeats`
   (window and GPU list), `/api/v1/fleet/containers` (leaderboard), `/api/v1/devices/{id}/history`,
   and `/api/v1/devices/{id}/heartbeats`.

---

## 9. Error Handling

| Failure mode | Behavior | Status | Log |
|---|---|---|---|
| Body unparseable / > 1 MiB | No credential evaluation, no side effects | 400 `invalid request body` | none (unchanged) |
| `GetConfig(registration_key)` error | Treated as "not configured" (unchanged). On re-registration this only removes path (c) | n/a | none (unchanged; F-2) |
| `FindByDeviceUUID` error | Treated as unknown and goes to the new-device path (unchanged) | 201/401 | unchanged |
| `LookupAPIKey` error (incl. DB) | Key unresolved, grants nothing | per precedence | class `unknown-key` if no other path |
| `ListRevokedSerials` error | Certificate confers nothing (fail closed) | per precedence | class `cert-revocation-unavailable` |
| `HasAPIKey` error | Path (c) unavailable (fail closed) | 401 | class `regkey-keycheck-error` |
| Wrong-device key or certificate | No side effects | 403 fixed body | warn, `wrong-device-key` / `wrong-device-cert` |
| Any other unauthorized re-registration | No side effects | 401 fixed body | warn with class (AD-002 rule 6) |
| New-device registration-key mismatch | Nothing created | 401 fixed body | warn, `new-device-regkey-mismatch` |
| `Update` / `StoreAPIKey` / `Create` failure after authorization | Unchanged | 500 (unchanged texts) | error (unchanged) |
| Future timestamp | Clamped, accepted | unchanged 200 | warn, debounced (AD-007) |

The rejection log message is `device registration rejected`, with fields `device_id`
(as supplied, truncated to 64 characters), `client` (`middleware.ClientIP`), `class`, and
`status`. It never logs the `X-rIOt-Key` value, the configured registration key, or any issued key
(NFR-002). The existing info log `generated API key for enrolled device` keeps logging only
`device_id`.

---

## 10. Security Considerations

| Vector | Mitigation |
|---|---|
| UUID-only key minting (Flaw A) / metadata overwrite (Flaw B) | Rule 6 rejects with no side effects; the decision runs before any write (AD-004). NFR-001 holds with or without a registration key. |
| Cross-device credential reuse | Rules 1 and 2 return 403 and take precedence over everything (PO-1, BR-004). |
| Registration-key holder hijacking a keyed device | Path (c) requires `HasAPIKey == false` per request; an error fails closed. |
| Forged certificate identity via proxy header | Only `r.TLS.VerifiedChains` is read; no header is consulted. |
| Certificate accepted without handshake verification | `PeerCertificates` alone yields `certUnverified`. |
| Revoked certificate / revocation list unavailable | Both yield no identity (stricter than `MTLSDeviceAuth`, PO-3). |
| Timing oracle on the registration key | sha256 both values + `subtle.ConstantTimeCompare` over fixed 32-byte digests (FR-021, NFR-003). |
| UUID existence disclosure (registration key configured) | One 401 body for all causes, including unknown UUIDs (FR-014/015). Residual: timing (OQ-4), and callers holding a valid fleet key can tell existing UUIDs apart via 403 (out of NFR-004 scope). |
| Unauthenticated large-body parsing (introduced by PO-6) | 1 MiB `MaxBytesReader` plus the unchanged 10/min/IP limiter. |
| Log injection / secret leakage | slog JSON handler escapes values; `device_id` is truncated to 64; no secret ever reaches the log helper's arguments (AC-020 test asserts this). |
| Clock-skew log flooding / memory | Debounced per device and kind; the map is bounded by authenticated devices × 2 and swept (NFR-009). |
| Key accumulation through path (b) | Accepted (OQ-5); rate-limited; Rotate key resets. |
| Zero-key window | Accepted (OQ-1); this is the intended bootstrap path. |
| Residual: `ClientAuth` set without `ClientCAs` in future code | `VerifiedChains` would then verify against system roots. Not possible today (`addMTLSConfig` is the only writer and sets both). Public CAs do not issue UUID CNs. Revisit if TLS configuration changes. |
| Residual: F-1 (`RIOT_API_KEY` not enforced) | Out of scope; flagged to the product owner. |

---

## 11. Performance Considerations

- **Re-registration:** at most three indexed lookups:
  - `LookupAPIKey` (PK on `key_hash`);
  - `ListRevokedSerials`, only when a verified certificate is present (small table, revoked
    rows only);
  - `HasAPIKey`, only when the registration key matched (`idx_api_keys_device_id`, `EXISTS`).

  There are no outbound calls (NFR-008). Volume is capped by the limiter.
- **Ingest:** one time comparison per request. Clamp logging takes a mutex and does a map lookup
  only when a clamp actually fires.
- **Read guard:**
  - `GetLatestSnapshot`/`GetHistory`: the predicate is a range bound on
    `idx_telemetry_device_time (device_id, timestamp DESC)`, so the plan shape is unchanged.
  - `DISTINCT ON` queries: they already scan per device; the added filter is a cheap comparison
    applied before sorting.
  - Heartbeat queries: `>= since AND <= NOW()+5m` is a bounded range, no worse than today.
  - Under PART-001, `NOW()` is a stable expression, so it allows runtime pruning of future
    partitions.
  - NFR-008 latency parity is verified by QA by comparing endpoint timings from the request log
    (`middleware.Logger`) before and after on the reference deployment.
- **No new index**, no migration.

---

## 12. Implementation Notes for Engineers

- **Never mutate `existing` or call `Update`, `StoreAPIKey`, the hub, or `eventGen` before
  `decideReRegistration` returns an accept.** The nil-hub rejection tests enforce this.
- Compare against `existing.ID` (the DB value), not `reg.DeviceID`.
- `X-rIOt-Key` must never be passed to `slog` or any error message. `logRegReject` takes no key
  argument by design.
- **Typed nil:** in `router.go`, assign `s.CARepo` into the `db.CARepository` variable only inside
  the mTLS `if`.
- Use `cs.VerifiedChains[0][0]` as the leaf. Do not use `PeerCertificates[0]` for identity. Serial
  format is `SerialNumber.Text(16)`, matching enrollment's `%x`.
- Place `http.MaxBytesReader` **before** `json.NewDecoder(r.Body)`.
- In `Telemetry`, clamp **before** the size check, `StoreSnapshot`, and the
  `processTelemetryPostWork` goroutine, because the goroutine takes `snap` by value.
- Handler telemetry tests must post minimal payloads (`{"timestamp":..., "data":{}}`) so the
  detached goroutine stays on nil-safe branches (no docker, no updates), following
  `TestTelemetry_AC024_OldAgentCompat`. Put the 100-iteration debounce handler test on
  `Heartbeat`, which is synchronous, not on `Telemetry`.
- `http.Error` appends `\n` to bodies. Compare bodies across requests (AC-014), or against
  `regBodyUnauthorized+"\n"`.
- Keep the SQL guard **inside** the `DISTINCT ON` subqueries (AD-008 table). Placing it in the
  outer `WHERE` silently drops devices.
- Reuse `captureLogs(t)` from `oidc_handler_test.go` (same package). Do not run these tests with
  `t.Parallel()`, because `captureLogs` swaps the global logger.
- **HARDEN-002 merge:** see AD-001 consequences. Do not add a symbol named
  `registrationKeyMatches` in this branch.
- **PART-001:** nothing in this story adds DDL. If PART-001 lands first, the query literals move
  but the guard fragment is unchanged.
- Windows: run `go test ./...` without `-race` (CGO). Linux CI runs `-race`. `MockDeviceRepo` is
  mutex-protected; keep the new `HasAPIKey` under `RLock`.
- No new collector, so the agent collector whitelist rule (MEMORY.md) does not apply. No agent
  change at all.
- Release: this is a code change, so it needs a tagged release. Per NFR-011, any future
  feature that exposes device UUIDs or telemetry age outside the authenticated API must ship after
  it and clamp ages at >= 0.

---

## 13. Definition of Done

- [ ] All Section 4 component changes implemented; no file outside Section 4 modified.
- [ ] `RegisterDevice` evaluates proof before any mutation; the old top-of-handler key gate is
      removed; the new-device key check uses `regKeyEqual`.
- [ ] `HandlerDeps.CARepo` is wired in `router.go` only under `MTLSEnabled && CA != nil`, and is
      therefore covered on both startup and `applyTLSAndRestart`.
- [ ] `DeviceRepository.HasAPIKey` is implemented in `DeviceRepo` and `MockDeviceRepo`.
- [ ] `Telemetry` and `Heartbeat` clamp via `clampFutureTimestamp` with `ingestNow`; debounced
      limiter in place.
- [ ] Guard fragment present in all 8 read queries (inside subqueries where applicable).
- [ ] `formatAgo` extracted to `web/src/utils/format.ts` with the negative clamp; `FleetOverview`
      imports it.
- [ ] Every Section 8 automated test exists, with the AC ID in its name or subtest name; the
      FRD's minimum list (AC-001/002/003/004/006/007/008/025/026/027) is covered.
- [ ] `go build ./...`, `go test ./...` (Windows, no `-race`), and `cd web && npm run test:run` are
      green.
- [ ] No new Go module or npm package (`go.mod`, `go.sum`, `package.json`, lockfile unchanged).
- [ ] No migration file added; no `UPDATE`/`DELETE` on telemetry or heartbeat tables added.
- [ ] No new lint errors; `gofmt` clean.
- [ ] Grep check: no `==`/`!=` comparison against the registration key remains in `handlers/`.
- [ ] Grep check: no `slog` call in the registration path receives the `X-rIOt-Key` value.
- [ ] Hand-off to the technical writer for the README recovery section and CHANGELOG (FR-027,
      FR-028); QA has the Section 8.1 manual procedure and the AC-024 released-agent run.
