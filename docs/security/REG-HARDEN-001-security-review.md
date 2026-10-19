# Security Review Report

**Story ID:** REG-HARDEN-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-17
**Verdict:** APPROVED WITH CONDITIONS

No CRITICAL or HIGH findings. The proof decision (ADD AD-001 to AD-005) closes Flaw A
(unauthenticated key minting for an existing device) and the unauthenticated metadata overwrite (Flaw B) at the design level. The FR-013
precedence is encoded correctly. Every fact the decision reads is gathered with read-only I/O
before the first mutating statement. Every I/O error removes a grant and never adds one. The
certificate path reads only handshake-verified chains and fails closed on revocation-list
errors. The clamp and read guard (AD-006 to AD-008) close Flaw E.

There is one new MEDIUM issue (SEC-001). The new-device rejection log writes an
attacker-supplied `device_id` into the persisted server log, and the persisted log is a
COPY-batched JSONB insert. A single NUL byte therefore drops the whole batch. The fix is a small
implementation change, not a design change. The other MEDIUM is the architect's pre-existing F-1
(SEC-002).

**Capability gate answer.** Once this design is implemented to the conditions
below, knowing a device UUID gives **no capability** against that device:

- no key issuance;
- no metadata or status mutation;
- no `DeviceOnline` or other event;
- no WebSocket broadcast;
- no way to deny service to the legitimate agent.

What remains is existence disclosure (SEC-008), which matters little because device UUIDs appear
in dashboard URLs, logs and API responses and must not be treated as credentials, and
rate-limited log rows (SEC-010). The property that knowing a UUID grants no capability is met.
The property that telemetry age is never negative is met for every "latest" read. Any future
feature that exposes telemetry age must still clamp it at zero itself, because rows up to +5
minutes are kept as sent (SEC-011).

Read in full:

- `docs/requirements/REG-HARDEN-001-frd.md` and `docs/architecture/REG-HARDEN-001-add.md`
- the grounding note
- `internal/server/handlers/handlers.go`: `HandlerDeps`/`Handlers`/`New`, `RegisterDevice`,
  `Heartbeat`, `Telemetry`, `RotateKey`, `generateAPIKey`
- `internal/server/handlers/enroll.go`: `Enroll`, `Renew`
- `internal/server/handlers/terminal.go`: `HandleAgentWS`
- `middleware/auth.go`, `mtls_auth.go`, `clientip.go`, `ratelimit.go`, `logger.go`
- `router.go`
- `server.go`: startup, `loadDBConfig`, `applyTLSAndRestart`, `configureTLS`, `addMTLSConfig`,
  `loadOrCreateCA`
- `config.go`
- `db/device_repo.go`, `db/telemetry_repo.go`, `db/ca_repo.go`, `db/admin_repo.go`,
  `db/log_repo.go`
- `logstore` DB handler, `ca/ca.go` `SignCSR`, `auth/keyhash.go`
- `000001_init.up.sql`
- `testutil/mocks.go`: `MockDeviceRepo`, `MockCARepo`
- `internal/agent/register.go`, `agent.go` (startup/enroll/re-register), `http_client.go`
- `scripts/install.sh`, `scripts/uninstall.sh`
- `README.md` registration sections

---

## Threat Model Summary

### Assets

| Asset | Where it lives | Why it matters |
|---|---|---|
| Device API keys | `api_keys` (SHA-256 of 256-bit random value), agent `server.api_key` | Holding a key is holding the device: telemetry, heartbeat (drains pending commands), events, logs, agent WS |
| Device record integrity | `devices` (hostname, tags, hardware profile, status) | Tags drive admin scoping. Status and `DeviceOnline` drive alerting and notifications |
| mTLS identity | rIOt CA (`device_certs`, CA key in DB), agent client cert | On an mTLS server, a qualifying certificate yields an API key via path (b), so the cert alone is equivalent to the device |
| Registration key (RK) | `admin_config.registration_key` | Admits devices. After this story, it also bootstraps a key for zero-key devices only |
| "Latest" telemetry semantics | `telemetry_snapshots`, `heartbeats` ordering | Staleness logic, dashboard summary views, and the fleet UI all trust "latest" |
| Server log integrity | `server_logs` (WARN+ persisted through a COPY batch), error-rate alert worker | Detection and alerting, and the operator's audit trail |

### Threat Actors

1. **Unauthenticated network caller who knows device UUIDs.** UUIDs appear in dashboard URLs,
   logs and API responses and must not be treated as credentials. This is the primary actor.
2. **Compromised fleet device.** Holds its own key and/or certificate, and tries to act on
   another device (BR-004).
3. **Registration-key holder.** Trusted to admit devices, but must not be able to hijack keyed
   devices (BR-003).
4. **Automated abuse.** Floods of registration requests from rotating source addresses, aimed
   at log and DB amplification.
5. **Insider/admin.** Out of scope. Admin already has Rotate key.

### Attack Surface Introduced

- The registration endpoint now **parses the body before authentication** (PO-6), bounded by a
  new 1 MiB cap.
- New credential reads on a public endpoint: `LookupAPIKey`, `ListRevokedSerials`, and
  `HasAPIKey`.
- A new warn-level, **DB-persisted** log line per rejected registration. It includes a
  caller-supplied `device_id`.
- A new in-memory per-device clamp-log map, keyed by authenticated device IDs.
- An `ingestClock` test seam (unexported).
- A SQL fragment spliced into 8 read queries (constant, no user data).

---

## Findings

### CRITICAL

None.

---

### HIGH

None.

---

### MEDIUM

#### SEC-001: Attacker-supplied `device_id` in the new-device rejection log can drop entire batches of persisted server logs (NUL → JSONB COPY failure)

**Severity:** MEDIUM
**Domain:** Data exposure / log integrity / detection evasion (new attack surface)
**Location:** ADD AD-004 step 6 and §4 `logRegReject(..., truncate(suppliedDeviceID, 64), ...)`
(class `new-device-regkey-mismatch`). Interacts with `internal/server/logstore` `DBHandler.flush`
(buffers ≤100 entries, flushes every 1 s, `_ = h.repo.Insert(...)`) and `db/log_repo.go` `Insert`
(one `CopyFrom` per batch; `attrs` is `JSONB`; `000002_server_logs.up.sql:6`).

**Description.**

- The default slog handler persists every WARN+ entry to `server_logs`. It serializes attrs with
  `json.Marshal`, which encodes U+0000 as `\u0000`, and writes the whole buffered batch in one
  `COPY`.
- PostgreSQL `jsonb` rejects `\u0000` ("unsupported Unicode escape sequence"). One bad entry
  therefore aborts the COPY, and the handler discards the error and the batch.
- On a server with a registration key configured, a request whose `device_id` does not resolve
  reaches the new-device branch. ADD AD-004 step 6 then logs the supplied `device_id`.
- A `device_id` containing NUL never resolves: Postgres rejects NUL in a `text` parameter, so
  `FindByDeviceUUID` errors and the request falls through to the new-device branch. That makes
  this path reliably reachable.
- Truncating to 64 bytes does not help, because the NUL can be byte 0.
- Re-registration rejection classes are not affected. Their `device_id` exactly equals an
  existing DB ID.

**Attack Scenario.**

1. The attacker sends `POST /api/v1/devices/register` with body
   `{"device_id":"\u0000x","hostname":"a"}`, no credential, and no `X-rIOt-Key`. The server has
   a registration key configured.
2. The handler returns the uniform 401 and logs `device registration rejected` with
   `device_id="\x00x"`.
3. Within 1 s, the DB handler's COPY for that batch fails. Every other WARN/ERROR entry
   persisted in the same second is lost too, for example a real `store telemetry` error or a
   DB-pool cascade.
4. The attacker repeats about once a second. The register limiter allows a burst of 10, then
   1 request per 6 s per IP, so about 6 source addresses (trivial with IPv6) sustain it. The
   Server Logs page then goes blank, and the server-error-rate alert worker
   (`CountSince("ERROR")` over `server_logs`) never fires during a real incident. Stdout
   (`docker logs`) still has the entries, so this is suppression, not erasure.

**Required Resolution.**

- Any caller-supplied string that this story writes to a log must be unable to contain NUL or
  other C0 control characters. For example, log `device_id` only when it is syntactically a
  UUID, and otherwise log a fixed placeholder plus its length. The architect or senior_dev
  chooses the mechanism, but the property must hold for every `logRegReject` call site.
- The AC-020 test must include a rejected request whose `device_id` contains `\u0000` and other
  control bytes. It must assert that the captured log line contains none of them.
- Systemic, as a follow-up and not blocking this story: the logstore persistence path should
  not let one entry's content drop a whole batch. Either sanitize attrs before marshaling, or
  fall back to per-row inserts on COPY failure. Other WARN+ call sites that log request-derived
  strings inherit the same weakness.

**Blocks:** QA sign-off (condition C1).

---

#### SEC-002: Pre-existing F-1: `RIOT_API_KEY` is documented as gating registration but is never enforced

**Severity:** MEDIUM (pre-existing; it was effectively CRITICAL in combination with Flaw A
before this story)
**Domain:** Configuration / authentication
**Location:** `internal/server/config.go:62-63` (loads into `Config.RegistrationKey`, which
nothing reads); `server.go:306-311` ("env var overrides DB", dead); `handlers.go:157` (reads only
`admin_config`); `README.md:139` ("if set, agents must present this key to register").

**Description.** An operator who sets only `RIOT_API_KEY`, as the README instructs, believes
registration is closed but actually runs an **open** server.

- **Before this story**, F-1 combined with Flaw A gave that operator unauthenticated key minting
  for every existing device, while the operator believed a gate was in place.
- **After this story**, NFR-001 holds whatever the registration-key source, so F-1 no longer
  touches existing devices. Its remaining impact is open new-device creation on a server the
  operator believes is gated.

A rogue device can:

- create fleet entries;
- post its own events, docker events, logs, and probe results, producing alert and notification
  noise and storage growth;
- appear in admin views.

It cannot reach other devices' data, commands, or keys. F-1 is independent of UUID knowledge, so
it does not affect the UUID-capability property above.

**Attack Scenario.**

1. An operator sets `RIOT_API_KEY=…` in the compose env and leaves Settings › Registration empty.
2. An attacker on the LAN or VPN runs `install.sh https://server:7331` without `--key`, or POSTs
   `/register` with no `device_id`.
3. The server returns 201 and a key. The attacker then posts crafted events and logs that
   trigger notifications to the operator, or keeps creating devices.

**Required Resolution.**

- **Agree with the architect's plan to fix enforcement in a follow-up story.** Enforcing the env key
  changes behavior for env-configured operators:
  - new installs without `--key` start failing;
  - path (c) starts honoring the env value;
  - the Settings UI shows a value that differs from the effective one.

  Those changes need a product owner decision and their own FRD.
- **Condition for this story (doc-only, no behavior change):** the README update the technical
  writer is already making must not claim that `RIOT_API_KEY` gates registration. It must state
  that only the Settings › Registration value (`admin_config.registration_key`) is enforced
  (condition C8).
- **Follow-up scope recommendation:**
  - enforce, or remove, the env key;
  - log a startup WARN when `RIOT_API_KEY` is set but is not the effective gate;
  - schedule soon.
- **Operator note:** operators who set only `RIOT_API_KEY` in the compose environment while
  `admin_config.registration_key` is empty should know that registration is open today,
  whatever they believe.

**Blocks:** Nothing in this story beyond the README correction.

---

### LOW / INFORMATIONAL

#### SEC-003: On mTLS servers, path (a) accepts the API key alone, so a stolen key can still re-register after the device's certificate is revoked

**Severity:** LOW
**Domain:** Authentication consistency
**Location:** ADD AD-002 rule 3; FRD S-1/S-2; compare `router.go` device route stack
(`MTLSDeviceAuth` + `DeviceAuth`).

**Description.** On an mTLS server, device routes require **both** cert and key. Registration
path (a) requires only the key. An attacker holding device D's key but no valid cert, for
example after the admin revoked D's certificate because D was compromised, can still re-register
D. That overwrites hostname, tags, and hardware profile, sets status online, broadcasts, and
emits `DeviceOnline`. No new key is issued, and device routes stay closed. This follows directly
from settled decision S-1, so it is not a defect of the ADD.

**Attack Scenario.**

1. D's key leaks, and the admin revokes D's certificate.
2. The attacker POSTs `/register` with `device_id=D`, `X-rIOt-Key: K_D`, and new tags.
3. The server returns 200. D's tags and status are attacker-chosen until the next legitimate
   re-registration. `DeviceOnline` fires and masks the device's real state until offline
   detection flips it back.

**Required Resolution.** None for this story. Recorded for the product owner as a possible FRD follow-up:
"when mTLS is active, path (a) also requires a qualifying certificate for N". QA should note the
behavior so it is not mistaken for a regression.

---

#### SEC-004: Pre-existing systemic: `MTLSDeviceAuth` fails open on revocation-list errors (including `/renew`) and does not bind CN to the device

**Severity:** LOW (pre-existing)
**Domain:** Authentication / revocation
**Location:** `middleware/mtls_auth.go:30-39` (`if err == nil { …check… }`), guarding `/renew`,
`/ws/agent`, and all device routes on mTLS servers. CN is never compared with the URL `{id}` or
the key's device.

**Description.**

- If `ListRevokedSerials` errors, a revoked certificate passes `MTLSDeviceAuth`.
- On `/renew`, that yields a **fresh, unrevoked** certificate for the revoked device. Under this
  story, that new certificate then qualifies for path (b) and mints a key. The chain is
  slightly more valuable than before, although a revoked device holding its old key could
  already combine it with a renewed cert on device routes.
- Separately, cert A + key B is accepted on device B's routes, while this story's registration
  path correctly returns 403 for the same mix (PO-1).

The ADD rightly does not reuse this middleware.

**Attack Scenario.** Device D's certificate is revoked. During DB pool saturation, the
`ListRevokedSerials` query times out while `GetByID`/`StoreCert` succeed. D's agent calls
`/api/v1/renew` and receives a new certificate. It then re-registers with no key, path (b) mints
a key, and D has full access again.

**Required Resolution.** Follow-up, not blocking:

- `MTLSDeviceAuth` must fail closed on revocation-list errors, consistent with PO-3.
- When an API key is also required, the cert CN must equal the key's device / URL `{id}`.

---

#### SEC-005: Pre-existing F-2: `GetConfig("registration_key")` errors are read as "registration open"

**Severity:** LOW (pre-existing)
**Domain:** Fail-open configuration read
**Location:** `handlers.go:157` (`regKey, _ :=`); `db/admin_repo.go:34-42` returns the error for
both "no row" and real DB errors.

**Description.** A transient error on the `admin_config` read makes an RK server behave as open
for new-device registration during that request. For re-registration, the ADD shows it only
disables path (c), which is fail-closed. In practice `Create` usually fails too, so the window
is narrow.

**Attack Scenario.** Under induced DB saturation, the attacker floods new-device registrations.
Any request whose `GetConfig` times out while `Create`/`StoreAPIKey` succeed yields 201 and a key
on an RK server.

**Required Resolution.** Follow-up, together with SEC-002. A real DB error, as opposed to "not
configured", must fail closed (401/503) on the new-device path. This needs a not-found sentinel
from the repo, which is outside this story's surface. Not a condition.

---

#### SEC-006: Pre-existing F-3: autocert TLS never requests client certificates

**Severity:** INFORMATIONAL (pre-existing; fails closed)
**Domain:** Configuration
**Location:** `server.go:556-569` (`configureTLS` returns `m.TLSConfig()` without
`addMTLSConfig`).

**Description.** With `TLSDomain` set and mTLS enabled:

- no connection ever carries a client certificate;
- path (b) yields `certAbsent`;
- every `MTLSDeviceAuth`-guarded route returns 401.

The result is non-functional, not insecure: nothing is granted without a cert. There is no
attacker scenario, so this is not rated above informational.

**Required Resolution.** Follow-up: refuse to start, or WARN loudly, when mTLS is enabled in
autocert mode, or wire `addMTLSConfig` into the autocert config. Not a condition.

---

#### SEC-007: The read guard uses the DB clock (`NOW()`) while the write clamp uses the app clock

**Severity:** INFORMATIONAL
**Domain:** Correctness / availability
**Location:** ADD AD-008 (`timestamp <= NOW() + interval '5 minutes'`) versus AD-006
(`ingestNow()` in Go).

**Description.** Let δ be the app clock minus the DB clock.

- When δ > 0, a freshly stored row with timestamp up to `R_app + 5m` stays hidden for up to δ.
- When δ > 5 minutes, every freshly clamped row (`timestamp = R_app`) is hidden for δ − 5m, so
  devices show stale "latest" data.
- When δ < 0, the guard is only looser, which is harmless.

In the reference docker-compose deployment the app and DB share the host kernel clock, so δ ≈ 0. The ADD's statement that skew
"is absorbed by the 5-minute tolerance" holds for |δ| < 5 minutes. No attacker controls δ.

**Required Resolution.** None. The deployment assumption (app and DB clocks synchronized well
within 5 minutes) should appear in the ADD or the operator notes.

---

#### SEC-008: Residual UUID-existence oracles

**Severity:** INFORMATIONAL (accepted: NFR-004, OQ-4, ADD §10)
**Domain:** Information disclosure

**Description.**

1. **Open server:** a known UUID returns 401, and an unknown UUID returns 201 with a new device
   (accepted).
2. **RK server timing:** with a non-empty `X-rIOt-Key`, a known UUID costs an extra
   `LookupAPIKey` round trip. FR-022 skips that lookup on the new-device path, so the attacker
   can amplify the difference at will.
3. **Fleet-key holder:** an existing UUID returns 403, and an unknown one returns 401.

UUIDs are random v4 values (122 bits), so these oracles cannot be used for enumeration. They
only confirm a UUID the attacker already holds, and device UUIDs must not be treated as credentials anyway.
No grant follows from existence.

**Required Resolution.** None.

---

#### SEC-009: `HasAPIKey` → `StoreAPIKey` is not atomic (path (c) TOCTOU), and Rotate key can leave a device with zero keys

**Severity:** INFORMATIONAL
**Domain:** Business logic / race

**Description.**

- Two concurrent path (c) requests for a zero-key device can both see `HasAPIKey=false` and
  both mint a key. A path (c) request can also race the legitimate agent's path (b) first-key
  fetch, so both get keys.
- Both cases need the registration key. They grant nothing beyond what OQ-1 already accepts:
  an RK holder can bootstrap a key for a zero-key device by going first.
- No unauthenticated actor can trigger the race, and it cannot add a key to a device that had
  keys **before** both requests began.
- Separately, `RotateKey` runs `DeleteAPIKeysByDevice` then `StoreAPIKey` without a
  transaction. If the store fails, the device is left with zero keys, which opens path (c) to RK
  holders until the admin retries.

**Required Resolution.** None for this story. After deploy, existing Bug D victims (enrolled on
RK servers, never keyed) stay claimable by RK holders until their agent next re-registers
(startup, or every 24h). Restarting those agents after deploy closes the window.

---

#### SEC-010: Unauthenticated, rate-limited, persisted warn rows; rate-limiter keying

**Severity:** INFORMATIONAL
**Domain:** Abuse / resource use

**Description.**

- Each rejection writes one WARN row to `server_logs`. That row is new unauthenticated DB write
  amplification, capped by `registerLimiter`: a burst of 10, then 1 request per 6 s per
  `ClientIP`.
- The limiter keys on the full IPv6 /128, and its map grows until the 5-minute sweep (both
  pre-existing).
- If `RIOT_TRUSTED_PROXIES` is misconfigured so that every client resolves to the proxy, one
  caller can exhaust the shared registration bucket. That blocks legitimate re-registration,
  which agents tolerate (log and continue) and which is not UUID-dependent.
- There is deliberately no per-device lockout. That is correct: a per-device counter would turn
  UUID knowledge into a DoS of the legitimate agent.

**Required Resolution.** None.

---

#### SEC-011: Agent-controlled timestamps after the clamp; cross-story notes

**Severity:** INFORMATIONAL
**Domain:** Data integrity

**Description.**

- **Within-window poisoning:** a device can still date a snapshot `R + 5m`, keeping it "latest"
  over its own genuine posts for up to 5 minutes, and repeat. This affects only that device's
  own record, and the poster already holds that device's credential and controls its content,
  so it does not matter.
- **Past timestamps:** these can never become "latest", and retention purges them (accepted by
  BR-007/BR-008).
- **Other ingest paths:** device logs, container logs, container metrics, and probe results
  stay unclamped (FRD §9 item 7). Each is limited to the poster's own device.

Cross-story notes:

- **Future telemetry-age features:** rows up to +5 minutes are stored as sent, so any age or
  staleness computation outside the clamped paths must clamp age at ≥ 0 itself. Telemetry age
  must never be negative. This story fixes only `formatAgo`.
- **PART-001:** far-past timestamps are not clamped. Partitioning needs a default or
  catch-all partition, or inserts with ancient timestamps will 500 and loop in the agent's
  offline buffer.

**Required Resolution.** None for this story. The notes are carried to the owners of future
telemetry-age features and of PART-001.

---

## Probes performed (no finding unless referenced)

**Precedence and identity**

| Probe | Result |
|---|---|
| RK + another device's UUID → mint for a keyed device? | No. Rule 5 needs `DeviceKeyless`, computed by `HasAPIKey(existing.ID)` per request. A `HasAPIKey` error means 401. |
| Cert A + key B naming A, B, or C | Rule 1 or rule 2 → 403 in every case. Matches PO-1 and AC-012. |
| Key → this + revoked/unverified cert | Path (a), as FR-012 intends (the cert is ignored). See SEC-003 for the mTLS nuance. |
| Garbage key + cert → this | Path (b) mints (FR-011). A `LookupAPIKey` DB error is treated as "unresolved" and can only lead to path (b) (cert proof) or 401. |
| Header = RK and also a device key? | `LookupAPIKey` runs first; rules 1 and 3 win. Harmless. |
| UUID normalization | `FindByDeviceUUID` is an exact `text` match, and N = `existing.ID`. Case, whitespace, or NUL variants miss and go to the new-device path, where the server mints a fresh UUID and ignores the supplied one. |
| Mass assignment | The accepted path writes only hostname, arch, version, hardware profile, tags, and server-set status. |

**TLS**

| Probe | Result |
|---|---|
| `VerifyClientCertIfGiven` | A presented cert that fails verification aborts the handshake, so `certUnverified` is defense in depth. |
| `VerifiedChains` vs `PeerCertificates` | `VerifiedChains[0][0]` is the leaf. Session resumption restores chains from server-encrypted tickets, and revocation is checked per request, so resumption bypasses nothing. |
| Expired certs | Rejected at handshake or resumption. |
| Client-auth EKU | `SignCSR` forces CN = device ID with ClientAuth EKU. `Renew` signs with the mTLS-derived ID, not the CSR subject. |
| Reverse proxy / TLS termination | `r.TLS` is nil or carries no peer certs → `certAbsent`. No header is ever consulted. |
| Autocert | See SEC-006. |
| Runtime mTLS enable | `applyTLSAndRestart` → `setupRouter` rebuilds `HandlerDeps` under the same predicate. The HTTP-only branch swaps `Handler`, but `r.TLS` is nil there. |
| Nil `CARepo` | Yields `certAbsent`, and the ADD flags the typed-nil hazard. |

**Abuse, disclosure, and logging**

| Probe | Result |
|---|---|
| 1 MiB cap | Adequate. The real payload is a few KB. It also bounds the previously unbounded new-device body on open servers (an improvement). |
| Uniform 401 | One body constant and identical headers (`http.Error`). No pre-decision 500 differs by existence, because collection errors fold into states. |
| Logs | The header value never reaches the log helper, and `middleware.Logger` logs no headers. The minted-key info log carries only `device_id` and `path`. The clamp log uses the authenticated URL ID. See SEC-001 for `device_id` content. |
| Constant time | sha256 of both values + `subtle.ConstantTimeCompare` over 32 bytes. Returning early when `configured == ""` leaks nothing that is not already observable. |

**Clamp and read guard**

| Probe | Result |
|---|---|
| `ingestClock` | Unexported and never set by `New`. No request path can reach it. |
| Debounce map | Keyed only by IDs that passed `DeviceAuth` (URL `{id}` == key's device), so bounded by devices × 2 and swept. |
| Inclusive boundary | `ts.After(now+5m)` and SQL `<=` agree. |
| Read-guard placement | Inside every `DISTINCT ON` subquery, which is correct. Only `telemetry_repo.go` touches these tables. Only `Heartbeat`/`Telemetry` call the store methods. |

**Legitimate flows**

| Flow | Result |
|---|---|
| Fresh install with RK | V-21 → 201. |
| Fresh install, open server | V-20 → 201. |
| Periodic or startup re-registration with own key, with or without RK | Path (a). Fixes Bug C. |
| mTLS enroll → register | `agent.go` rebuilds the client with the new cert before `register` → path (b). Fixes Bug D. With RK and no verified cert → path (c). |
| mTLS server device routes | The minted key plus the cert satisfy both middlewares. |
| AGENT-HARDEN-001 `install.sh` re-run | `agent.yaml` and the ID file are preserved, even with `--key` → path (a). `uninstall.sh --keep-config` keeps `agent.device_id` and `api_key` in `agent.yaml`. |
| Agent still holding an old RK after RK rotation | Keyless + cert → path (b). Keyed → 401. It was already broken before, silently. |
| **Behavior change** | A partially wiped agent (`agent.yaml` deleted, ID file kept) re-installed against an **open** server used to be silently re-keyed through Flaw A. It is now rejected with 401. This is intended (FR-028) and must stay prominent in the CHANGELOG and recovery docs. |

---

## Positive Observations

- **Gather and decide are separate, and the decision is pure.** "No side effects on reject"
  holds by construction, and the nil-hub/nil-eventGen rejection tests make any violation panic.
- **Every credential I/O error removes a grant and never adds one:** `LookupAPIKey` error,
  `HasAPIKey` error, revocation-list error, and `GetConfig` error on re-registration.
- **Certificate identity comes only from `VerifiedChains`**, with revocation fail-closed. This
  is stricter than the existing middleware, and the ADD says why it is not reused.
- **Identity is compared against the canonical `existing.ID`**, not the raw body string.
- **The registration key is consulted last** and only for zero-key devices. That puts BR-003
  ("RK admits, does not identify") into code.
- **One 401 constant and one 403 constant**, so AC-014 byte-identity holds by construction.
- **No per-device lockout or counter.** UUID knowledge cannot be turned into a DoS of the
  legitimate agent.
- **The 1 MiB cap was added because PO-6 moved parsing ahead of authentication.** The ADD
  noticed the new surface it was creating.
- **Clamp, don't reject.** The response path is untouched (NFR-007). The clamp runs before the
  detached goroutine copies `snap`, and the log map is bounded by authenticated IDs.
- **The read guard is placed inside the `DISTINCT ON` subqueries.** Devices fall back to their
  newest valid row instead of disappearing.
- **The ADD surfaced F-1/F-2/F-3 honestly** instead of silently fixing or hiding them.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.**

- There are no CRITICAL or HIGH findings.
- The design ensures that knowing a UUID grants no capability, and that telemetry age is never
  negative for every "latest" read.
- SEC-001 is MEDIUM and has a local, non-architectural fix: restrict the content of the logged
  `device_id`.
- SEC-002 (F-1) is pre-existing. The agreed plan is a follow-up story, with a doc-only
  correction in this story.

None of the conditions needs re-architecture. Route to `senior_dev` with this report alongside
the ADD. QA verifies C1 to C9. C10 is a sequencing condition for future features.

### Conditions

**C1 (SEC-001): Sanitize caller-supplied strings in rejection logs.** Every `logRegReject` call
site must log a `device_id` that cannot contain NUL or C0 control characters, for example a
UUID-shape check with a placeholder plus length otherwise. Extend the AC-020 test with a
`device_id` containing `\u0000` and `\n`, and assert that neither appears in the captured line.

**C2: Never log secrets, across all classes and the success paths.** Extend AC-020 beyond the
five listed classes so that it covers all 11 rejection classes, the path (b) and path (c)
**success** cases, and the new-device 201 case. In every case, assert that the log buffer
contains neither the presented `X-rIOt-Key`, the configured registration key, nor any minted
key. Keep the ADD's grep checks: no `slog` call receives the header value, and no `==`/`!=`
against the registration key remains.

**C3: No side effect before the decision, for every reject.** Rejection tests (nil hub and
eventGen, plus deep-compare of `Devices` and `APIKeys`) must cover every 401 and 403 class:
`wrong-device-key`, `wrong-device-cert`, `regkey-device-has-keys`, `regkey-keycheck-error`,
`cert-revoked`, `cert-revocation-unavailable`, `cert-empty-cn`, `cert-unverified`,
`unknown-key`, `no-credential`, and `new-device-regkey-mismatch` (nothing created). Code review
must confirm that nothing mutates `existing` and no repo writer, hub, or `eventGen` call precedes
the `decideReRegistration` accept.

**C4: Certificate identity property.**

- `register_auth.go` derives identity only from `r.TLS.VerifiedChains[0][0]`.
- It reads no request header other than `X-rIOt-Key`.
- It treats a `ListRevokedSerials` error as no identity (AC-013).
- It does not call or reuse `MTLSDeviceAuth`.

The AC-011 subtests (`PeerCertificates` without `VerifiedChains`, nil `TLS`, nil `caRepo` with a
verified chain) are mandatory.

**C5: `CARepo` wiring.**

- `router.go` assigns `s.CARepo` into the `db.CARepository` value **only** inside
  `if s.Config.MTLSEnabled && s.CA != nil`, so the value stays an untyped nil otherwise.
- `setupRouter` remains the single `handlers.New` construction site, so the
  `applyTLSAndRestart` rebuild is covered.
- QA confirms both by code review.

**C6: HARDEN-002 merge discipline.** Whichever branch lands second must:

- delete the top-of-handler registration-key gate, whether this branch's version or
  HARDEN-002's rewritten one;
- collapse `regKeyEqual` into `registrationKeyMatches`;
- re-run AC-002 (Bug C regression), AC-014, AC-017, and AC-018 plus the grep check on the
  merged result.

If the old gate survives, it silently reintroduces Bug C.

**C7: The precedence table is complete.** The pure decision-table test must include every row
listed in ADD §8, including both PO-1 mixed rows and the RK + keycheck-error row. Each row
asserts `Path`, `MintKey`, and `RejectClass`.

**C8 (SEC-002, doc-only): Correct the README's `RIOT_API_KEY` claim.** `README.md:139` (and any
registration text the technical writer touches) must stop saying that the env var gates
registration. It must say that only Settings › Registration is enforced, pending the F-1
follow-up. This is in addition to the FR-027/FR-028 recovery and CHANGELOG text, which must call
out the open-server, partially wiped agent behavior change listed under Probes.

**C9: Read-guard verification.** QA runs ADD §8.1 on a disposable dev DB, never on a production deployment, and
additionally confirms both of the following:

- (a) a device whose **only** rows are future-dated drops out of the latest views and then
  reappears after one clamped post;
- (b) the guard is inside the subquery in `GetAllLatestSummaries`, `GetGPUDeviceIDs`, and
  `GetFleetContainerLeaderboard`. An outer `WHERE` would silently drop devices.

**C10: Sequencing for future features (not for senior_dev).**

- Any future feature that exposes device UUIDs or telemetry age outside the authenticated API
  may proceed only after this story is **deployed**: the running image digest must include the
  tagged release (NFR-011), not just the merge.
- Such features must clamp age and staleness at ≥ 0 themselves (SEC-011).

### Follow-ups recommended (not conditions)

- **F-1 enforcement (SEC-002).** Needs a product owner decision and its own FRD. It should include a
  startup WARN when `RIOT_API_KEY` is set but is not the effective gate.
- **F-2 (SEC-005):** fail closed on a real DB error.
- **F-3 (SEC-006):** refuse to start, or warn, when mTLS is enabled with autocert.
- **`MTLSDeviceAuth` (SEC-004):** fail closed on revocation-list errors, and bind CN to the
  key's device / URL `{id}`.
- **SEC-003:** decide whether path (a) should also require a qualifying certificate on mTLS
  servers.
- **Logstore:** survive per-entry content failures (systemic part of SEC-001).
- **Hygiene:** make `RotateKey` delete-and-store atomic (SEC-009).
