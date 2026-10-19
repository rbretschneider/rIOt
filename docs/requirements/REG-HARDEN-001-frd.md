# Formal Requirements Document

**Story ID:** REG-HARDEN-001
**Title:** Device-proof re-registration (no key minting by UUID) + future-dated telemetry clamp
**Author:** Business Developer Agent
**Date:** 2026-10-17
**Status:** DRAFT

---

## 1. Executive Summary

`POST /api/v1/devices/register` is a public endpoint. Today, when no registration key is
configured, it hands a freshly generated, valid API key for an **existing** device to any
caller who knows that device's UUID, and the device's old keys stay valid. The same endpoint
also lets any caller overwrite an existing device's hostname, tags, hardware profile, and
status. Device UUIDs must not be treated as credentials: they appear in dashboard URLs, logs and
API responses. Separately, agent-supplied
snapshot timestamps are stored as sent, so a single future-dated snapshot stays the device's
"latest" snapshot indefinitely and the UI can show a negative age.

This story closes all of that. Re-registering an existing device (any change to its record
and any API key issued for it) succeeds only when the caller proves it is that device. The
three accepted proofs are: an API key already bound to the device; a valid, unrevoked mTLS
client certificate for the device; or the registration key, accepted only while the device
has no API key at all. Every other request is rejected and changes nothing. The story also
fixes an existing bug: when a registration key is configured, an agent re-registering with
its own API key is rejected. New-device registration is unchanged.

On the telemetry side, snapshot and heartbeat timestamps more than **5 minutes** ahead of the
server's receive time are clamped to the receive time. A read-side guard stops future-dated
rows that are already stored from being treated as "latest". The UI never shows a negative
age. The story needs no agent change and no database migration. Any future feature that exposes device UUIDs or telemetry age outside the authenticated API must ship after this story and clamp ages at >= 0.

---

## 2. Background & Context

**Origin.** Found during an internal security review of device registration.

**Current registration behavior** (`internal/server/handlers/handlers.go`, `RegisterDevice`
~L155–264; route is public and protected only by `registerLimiter`, 10/min per client IP,
`router.go` ~L131):

1. If a registration key is configured (`admin config "registration_key"`), the header
   `X-rIOt-Key` must equal it (plain string inequality, not constant-time). Otherwise the
   endpoint returns 401. This check runs **before** anything else.
2. If the body names a `device_id` that `FindByDeviceUUID` resolves, the handler performs
   **no credential check at all**. It overwrites `Hostname`, `Arch`, `AgentVersion`,
   `HardwareProfile`, `Tags` (when supplied), and sets `Status = online`, then calls
   `devices.Update`, broadcasts over WebSocket, and emits `DeviceOnline` if the device was
   not already online.
   - If `X-rIOt-Key` is empty, it generates and stores a **new** API key for that device and
     returns it. Existing keys are untouched.
   - If `X-rIOt-Key` is non-empty (any string, never validated), it returns `api_key: ""`.
3. Otherwise (no `device_id`, or unknown UUID), it creates a new device with a
   server-generated UUID and API key and returns 201.

**Defects this story closes.**

| ID | Defect | Condition |
|---|---|---|
| Flaw A | Unauthenticated API key minting for an existing device | Registration key not configured + known UUID + no `X-rIOt-Key` → fresh valid key; old keys stay valid. |
| Flaw B | Unauthenticated overwrite of device metadata/status | Registration key not configured + known UUID + any `X-rIOt-Key` → hostname/tags/hardware profile/status overwritten, WS broadcast, possible `DeviceOnline` event. |
| Bug C | Legit periodic re-registration rejected | Registration key configured + agent sends its own device API key → 401. `install.sh` writes the registration key into `server.api_key`, and `register.go` replaces it with the issued device key, so every agent hits this after its first registration. Agent startup and periodic re-registration (`agent.go` ~L164, ~L322) log and continue, so the failure is silent. |
| Bug D | mTLS-enrolled device on a registration-key server never gets an API key | Registration key configured + mTLS-enrolled device (enrollment creates no API key) sends the registration key → header is non-empty → handler returns `api_key: ""`. |
| Flaw E | Future-dated snapshot is "latest" forever | `Telemetry` (~L332) and `Heartbeat` (~L266) store the agent-supplied timestamp as-is (defaulted only when zero). Every "latest snapshot" read orders by `timestamp DESC`, so a far-future row wins forever. It also escapes retention (`timestamp < now − N days`) and makes the UI render a negative age (`web/src/pages/FleetOverview.tsx` `formatAgo`). |

**What is already safe.** `devices.last_telemetry` and `devices.last_heartbeat` are set from
server `NOW()` (`device_repo.go` ~L115), so device-level online/offline age does not depend on
agent clocks. The agent's offline buffer replays **past**-dated snapshots on reconnect, which
is legitimate. Only future dates are a problem.

**Existing credential surfaces this story reuses (no new ones).**

- Device API keys: `LookupAPIKey(key) → deviceID`, `StoreAPIKey`, `DeleteAPIKeysByDevice`
  (`internal/server/db/interfaces.go`). `DeviceAuth` middleware returns 401 for a
  missing/invalid key and 403 when the key's device does not match the URL `{id}`.
- mTLS: when `MTLSEnabled` and a CA exists, the server's TLS listener uses
  `VerifyClientCertIfGiven` with the rIOt CA, so any peer certificate present on the
  connection has already been verified against that CA during the handshake. Identity is the
  certificate CN (= device ID). Revocation is checked against `CARepository.ListRevokedSerials`
  (`middleware/mtls_auth.go`).
- Admin recovery: `POST /api/v1/devices/{id}/rotate-key` (admin-authenticated, already exposed
  in the UI) deletes all keys for the device and returns one new key (`handlers.go` ~L1154).
- Agent: sends `device_id` from config or the ID file (default `/var/lib/riot/device-id`), sends
  `X-rIOt-Key` = config `server.api_key`, presents the client certificate when configured, and
  persists the returned `api_key` only when it is non-empty. Re-running `install.sh` without
  `--bootstrap-key` keeps the existing `agent.yaml` and ID file (so the AGENT-HARDEN-001
  "re-run install.sh per device" step is not disrupted by this story).

### 2.1 Settled decisions (inputs, not open for redesign)

| # | Decision |
|---|---|
| S-1 | Re-registration of an existing device succeeds only through one of three proofs: (a) an API key bound to that device, (b) a valid, unrevoked mTLS client certificate for that device, (c) the configured registration key, and only while the device has **no** API key. |
| S-2 | Path (a) issues no new key. Path (b) issues a new key only when the caller did not present a valid key for this device. Path (c) issues a key. |
| S-3 | A key bound to a **different** device is answered with 403. Every other rejection is 401. |
| S-4 | New-device registration stays as it is today: open when no registration key is configured (documented policy), and requires the registration key when one is configured. The registration key comparison becomes constant-time. |
| S-5 | Recovery for an agent that has lost its key while its device still has keys is either the existing admin **Rotate key** action, or deleting the agent's ID file so it registers as a new device. No new endpoint and no agent change. |
| S-6 | Future-dated telemetry/heartbeat timestamps are **clamped** to server receive time, not rejected. Past timestamps are stored as sent. |
| S-7 | No new dependencies, no agent changes, no UI redesign (only a minimal negative-age guard). The enroll endpoint is not changed. Closing open new-device registration by default is out of scope. |

### 2.2 Overlap with in-flight work

1. **HARDEN-002** (branch `claude/youthful-austin-8d4743`, commit `6d39261`, unmerged) adds a
   constant-time `registrationKeyMatches(provided, configured)` helper just above
   `RegisterDevice` and uses it in the top-level registration key check. This story must not
   declare a second symbol with that name. The two branches will conflict textually in
   `RegisterDevice`, and whichever lands second resolves the conflict. If HARDEN-002 lands
   first, this story reuses its helper to satisfy FR-021.
2. **PART-001** (`part-001-partitioning`, unpushed) partitions telemetry tables by
   `timestamp`. This story therefore requires **no schema migration and no row rewrite or
   deletion** (FR-038). Clamping on write (FR-031/FR-032) also means future far-future rows
   will no longer need a partition to land in, which helps PART-001.

---

## 3. Actors

| Actor | Description | Permissions |
|---|---|---|
| Registered agent (key-based) | `riot-agent` on a device that holds an API key bound to its own device record. | May re-register its **own** device and update its metadata via path (a). Receives no new key. |
| Registered agent (mTLS-enrolled) | `riot-agent` on a device enrolled via bootstrap key. It holds a client certificate with CN = its device ID and, at first, no API key. | May re-register its own device via path (b) and obtain an API key when it does not present a valid one. |
| New / unregistered agent | `riot-agent` with no device ID, or with a device ID the server does not know. | Creates a new device: freely when no registration key is configured; only with the registration key when one is configured. Cannot claim an existing device. |
| Registration-key holder | Anyone holding the server's configured registration key (normally the operator, via `install.sh --key`). | May admit new devices. May bootstrap an API key for an existing device **only** while that device has zero API keys (path c). May not mint keys for, or modify, a device that already has keys. |
| Admin (dashboard) | Authenticated dashboard administrator. | May rotate a device's key (`POST /api/v1/devices/{id}/rotate-key`), which is the supported recovery path. May delete devices. |
| rIOt server | Go server serving the registration, telemetry, and heartbeat endpoints and the dashboard. | Evaluates device credentials. Clamps future timestamps on ingest. Excludes future rows on read. |
| Unauthenticated caller / attacker (threat model, not a user) | Any network client that knows or guesses a device UUID (URLs, logs, API responses). | Must be unable to obtain an API key for, or change any field of, an existing device. May still create new devices when no registration key is configured (accepted, out-of-scope policy). |
| Compromised fleet device (threat model) | A legitimate agent's key or certificate in hostile hands. | Must be unable to use its credential to re-register, or mint keys for, any **other** device (403). |

---

## 4. Functional Requirements

Terms used below:

- **Named device**: the existing device whose ID matches the request's `device_id`.
- **Re-registration**: a registration request whose `device_id` resolves to a named device.
- **Qualifying certificate**: a client certificate that (i) was presented on the TLS
  connection to the rIOt server and verified against the rIOt CA during that server's own
  handshake, (ii) has a non-empty CN, and (iii) has a serial number that is confirmed absent
  from the revoked-serials list.

### 4.1 Re-registration requires device proof

- **FR-001:** When a registration request is a re-registration, the system must authorize it
  only if exactly one of the paths in FR-003, FR-004, or FR-006 applies under the precedence
  of FR-013. Otherwise it must reject the request.
- **FR-002:** A rejected re-registration must have no side effects. The system must not modify
  any field of the named device record (including hostname, arch, agent version, hardware
  profile, tags, and status), must not create or store any API key, must not broadcast a
  device update over WebSocket, and must not emit a `DeviceOnline` event or any other event.
- **FR-003 (path a — own API key):** When `X-rIOt-Key` resolves to an API key bound to the
  named device, the system must accept the re-registration. It must update the named device's
  hostname, arch, agent version, hardware profile, and status (online), and its tags when the
  request supplies tags. It must respond 200 with an empty `api_key` and must not create any
  new API key.
- **FR-004 (path b — mTLS certificate):** When the request presents a qualifying certificate
  whose CN equals the named device's ID, and `X-rIOt-Key` does not resolve to a key bound to
  the named device, the system must accept the re-registration. It must apply the same
  metadata update as FR-003, generate and store exactly one new API key bound to the named
  device, and return that key in `api_key` with status 200.
- **FR-005:** A key issued under FR-004 must be added to the named device's keys. Any existing
  API keys of the named device must remain valid; path (b) must not revoke them.
- **FR-006 (path c — registration key, keyless device only):** When a registration key is
  configured, `X-rIOt-Key` matches it (per FR-021), and the named device currently has **zero**
  API keys, the system must accept the re-registration. It must apply the same metadata update
  as FR-003, generate and store exactly one new API key bound to the named device, and return
  it with status 200.
- **FR-007:** When the named device has one or more API keys, the registration key must not
  authorize its re-registration. Such a request, with no other qualifying credential, must be
  rejected with 401 under FR-002.
- **FR-008:** When no registration key is configured, a re-registration that presents no
  credential satisfying FR-003 or FR-004 must be rejected with 401 under FR-002. This closes
  Flaw A.
- **FR-009:** When `X-rIOt-Key` resolves to an API key bound to a device **other than** the
  named device, the system must reject the request with 403 under FR-002, regardless of any
  other credential presented.
- **FR-010:** When the request presents a qualifying certificate whose CN identifies a device
  **other than** the named device, the system must reject the request with 403 under FR-002,
  regardless of any other credential presented.
- **FR-011:** An `X-rIOt-Key` value that resolves to no device and does not match the
  configured registration key must grant nothing. When no other path applies, the request must
  be rejected with 401. Such a value must not block an otherwise satisfied path (b).
- **FR-012:** A certificate that is revoked, has an empty CN, or was not verified by the rIOt
  server's own TLS handshake (including any certificate identity conveyed by a forwarded
  header) must confer no device identity. The same applies to any certificate whose revocation
  status cannot be confirmed because the revoked-serials list could not be read: the request is
  evaluated as if no certificate was presented (fail closed).
- **FR-013:** Credential evaluation for a re-registration must apply this precedence, and stop
  at the first rule that applies:
  1. Any presented credential (API key or qualifying certificate) bound to a different device
     → 403 (FR-009, FR-010).
  2. `X-rIOt-Key` bound to the named device → accept via path (a) (FR-003).
  3. Qualifying certificate for the named device → accept via path (b) (FR-004).
  4. Registration key configured, presented, and the named device has zero API keys → accept
     via path (c) (FR-006).
  5. Otherwise → 401 (FR-007, FR-008, FR-011).
- **FR-014:** Every 401 response from the registration endpoint must have the identical status
  code and identical response body, whatever the rejection reason and whether or not the named
  device exists. Every 403 response must have one fixed body. No 401 or 403 body may contain
  the device ID, short ID, hostname, any key material, or the specific rejection reason.
- **FR-015:** When a registration key is configured, a caller who presents neither a valid
  device credential nor the registration key must receive the identical 401 response
  (FR-014) whether the `device_id` it names exists or not.
- **FR-016:** Each rejected re-registration must produce one server log entry at warn level
  containing the `device_id` as supplied, the client address, and the rejection class (for
  example `no-credential`, `wrong-device-key`, `wrong-device-cert`, `regkey-device-has-keys`,
  `unknown-key`). The log entry must never contain the presented `X-rIOt-Key` value, the
  configured registration key, or any issued API key.

### 4.2 Legit periodic re-registration (Bug C, Bug D)

- **FR-017:** The registration key check must not be applied to a re-registration authorized
  by path (a) or path (b). An agent presenting its own device API key must be able to
  re-register successfully whether or not a registration key is configured.
- **FR-018:** An mTLS-enrolled device that has zero API keys and presents the configured
  registration key must receive an API key, either via path (b) when it also presents a
  qualifying certificate for itself, or via path (c) otherwise.

### 4.3 New-device registration (unchanged policy)

- **FR-019:** When a registration request has no `device_id`, or has a `device_id` that
  matches no existing device, and no registration key is configured, the system must create a
  new device with a server-generated ID and API key and respond 201, exactly as today.
- **FR-020:** When such a request arrives while a registration key is configured, the system
  must create the new device (201) only if `X-rIOt-Key` matches the registration key (per
  FR-021). Otherwise it must reject with the 401 of FR-014 and create nothing.
- **FR-021:** Every comparison of a presented value against the configured registration key
  must take time independent of where the first differing character is and of the length of
  the presented value (constant-time comparison).
- **FR-022:** A device API key presented with a `device_id` that matches no existing device
  must not be treated as the registration key. Such a request must be handled solely under
  FR-019 or FR-020.
- **FR-023:** A registration request whose body cannot be parsed must be rejected with 400
  before any credential evaluation, with no side effects (as FR-002), and the 400 body must not
  reveal whether any device exists.
- **FR-024:** The registration endpoint's existing rate limit (10 requests/minute per client
  IP) must remain in force and unchanged.

### 4.4 Recovery path (no new endpoint)

- **FR-025:** The system must not add any new endpoint, flag, or self-service mechanism that
  lets an agent without a valid device credential reclaim an existing device that has API keys.
- **FR-026:** The admin **Rotate key** action (`POST /api/v1/devices/{id}/rotate-key`) must
  keep its current behavior: delete all API keys of the device and return exactly one new key.
  An agent whose `server.api_key` is then set to that key must re-register successfully via
  path (a).
- **FR-027:** The operator documentation (README) must state the recovery procedure for an
  agent whose device still has keys but whose own key is lost or wrong, as two alternatives:
  (1) an admin uses **Rotate key** on the device in the dashboard, pastes the returned key
  into `server.api_key` in `/etc/riot/agent.yaml`, and restarts `riot-agent`; or (2) the
  operator deletes the agent's device ID file (default `/var/lib/riot/device-id`) and restarts
  `riot-agent`, which then registers as a **new** device (this needs the registration key in
  `server.api_key` when the server has one). The old device record remains until an admin
  deletes it. The documentation must also state that mTLS-enrolled agents with a valid
  certificate recover automatically via path (b).
- **FR-028:** The CHANGELOG must record the behavior change: an agent that re-registers
  without a valid credential for its existing device no longer silently receives a new key and
  is rejected instead, with a pointer to the FR-027 recovery procedure.

### 4.5 Agent compatibility

- **FR-029:** All legitimate flows must work with the currently released agent, with no agent
  code change: first install with a registration key; first install against an open server;
  periodic and startup re-registration with the agent's own key, with or without a
  registration key configured; and first API key acquisition by an mTLS-enrolled agent.

### 4.6 Telemetry and heartbeat timestamps

- **FR-030:** The future-skew allowance must be **5 minutes**. It is a single server-wide value
  that applies to both snapshots and heartbeats.
- **FR-031:** When a telemetry snapshot's timestamp is later than the server receive time plus
  the skew allowance, the system must store the snapshot with the server receive time as its
  timestamp.
- **FR-032:** When a heartbeat's timestamp is later than the server receive time plus the skew
  allowance, the system must store the heartbeat with the server receive time as its timestamp.
- **FR-033:** Snapshot and heartbeat timestamps at or earlier than the server receive time plus
  the skew allowance, including any past timestamp, must be stored exactly as sent. A zero or
  missing timestamp must continue to default to the server receive time.
- **FR-034:** A clamped snapshot or heartbeat must be accepted with the same success status and
  response body as an unclamped one. Clamping must never cause a rejection or error response.
- **FR-035:** Clamping must be observable through a server log entry at warn level that names
  the device ID, the kind (snapshot or heartbeat), the observed skew (sent timestamp minus
  receive time), and the number of clamps for that device and kind since the previous entry.
  The system must emit at most one such entry per device per kind per rolling 1-hour window.
- **FR-036:** Every read path that selects a device's latest snapshot must exclude snapshots
  whose stored timestamp is later than the read time plus the skew allowance, so a future-dated
  snapshot can never be returned as, or used as, any device's latest snapshot. This covers
  per-device latest, fleet-wide latest, latest summaries, GPU-device detection, and the
  container leaderboard.
- **FR-037:** Snapshot history reads and heartbeat window/history reads must also exclude rows
  whose stored timestamp is later than the read time plus the skew allowance.
- **FR-038:** This story must not add a schema migration and must not delete or rewrite any
  existing snapshot or heartbeat row. Already-stored future-dated rows are handled only by
  FR-036 and FR-037.
- **FR-039:** Wherever the web UI renders a relative age ("… ago") for a telemetry or heartbeat
  timestamp, a timestamp later than the browser's current time must render as zero age (the
  same text the formatter produces for an age of 0 seconds), never as a negative value. No
  other UI change is in scope.
- **FR-040:** Device-level `last_telemetry` and `last_heartbeat` must continue to be recorded
  from server time (unchanged).

### 4.7 Verification

- **FR-041:** Automated tests must exist for each AC in §8 that the server or frontend test
  suites can check. Each test name must include the AC ID it covers (for example
  `TestRegisterDevice_AC004_NoKeyNoRegKey_Rejected`). At minimum, tests must cover: existing
  key; wrong-device key; no key with no registration key; registration key against a device
  with no keys; registration key against a device with keys; mTLS-enrolled device; future-dated
  snapshot; past-dated snapshot; and periodic re-registration while a registration key is
  configured.

---

## 5. Non-Functional Requirements

- **NFR-001 [Security]:** Knowing a device UUID alone must never be enough to obtain an API key
  for that device or to change any field of its record. This invariant must hold whether or not
  a registration key is configured.
- **NFR-002 [Security]:** No log entry, response body, or event produced by the registration,
  telemetry, or heartbeat endpoints may contain a presented `X-rIOt-Key`, the configured
  registration key, or an API key. The only exception is the `api_key` field of a successful
  registration response.
- **NFR-003 [Security]:** Registration key comparison must be constant-time (FR-021) and must
  use the Go standard library's constant-time comparison facilities. No hand-rolled primitive.
- **NFR-004 [Security — disclosure]:** When a registration key is configured, an
  unauthenticated caller must not be able to tell from status code or response body whether a
  UUID exists. When no registration key is configured, the only distinguishable outcome is 201
  (unknown UUID → new device) versus 401 (known UUID). This is inherent to the documented open
  registration policy and is accepted. Timing side channels from the device lookup are not
  addressed by this story (see OQ-4).
- **NFR-005 [Compatibility]:** The registration response contract must not change. The JSON
  shape (`device_id`, `short_id`, `api_key`) stays the same, 201 still means a new device, and
  200 still means a re-registration. Only the 401/403 bodies change (FR-014). The telemetry and
  heartbeat response contracts must not change.
- **NFR-006 [Compatibility]:** No agent code change. No change to `install.sh`, the enroll
  endpoint, or the rotate-key endpoint contract.
- **NFR-007 [Availability]:** Clamping must never cause telemetry or heartbeat ingestion to
  fail, retry, or drop data (FR-034). An agent with a skewed clock must keep delivering, and
  its offline buffer must drain normally.
- **NFR-008 [Performance]:** Re-registration credential evaluation must add no outbound
  network call and no external dependency. The read-side guard (FR-036, FR-037) must not
  measurably change the latency of the latest-snapshot, fleet, or heartbeat endpoints compared
  with today, as measured by existing endpoint timings on the reference deployment.
- **NFR-009 [Resource bounds]:** The state used to debounce clamp logging (FR-035) must be
  bounded by the number of devices times the two kinds and must not grow with the number of
  snapshots. Losing it on server restart is acceptable.
- **NFR-010 [Dependencies]:** No new Go module or npm package may be added.
- **NFR-011 [Sequencing]:** Any future feature that exposes device UUIDs or telemetry age outside the authenticated API must ship after this story and clamp ages at >= 0.
- **NFR-012 [Data safety]:** No existing row in any table may be deleted or rewritten by this
  story (FR-038), consistent with the in-flight PART-001 partitioning work.

---

## 6. Business Rules

- **BR-001:** Only that device itself (proven by its own API key or its own certificate) or an
  admin may change an existing device's record or credentials.
- **BR-002:** Device UUIDs are identifiers, not secrets. No authorization decision may rest on
  knowledge of a UUID.
- **BR-003:** The registration key admits devices to the fleet. It does not prove a device's
  identity. It admits new devices and may bootstrap a first API key only for a device that has
  none.
- **BR-004:** A credential that belongs to one device must never authorize action on another
  device (403).
- **BR-005:** Open new-device registration, when no registration key is configured, remains
  the documented policy and is not changed by this story.
- **BR-006:** Recovering from lost device keys is an admin action (Rotate key) or a
  re-registration as a new device. A device that already has keys cannot be reclaimed through
  self-service.
- **BR-007:** For ordering and "latest" selection, server time is authoritative. Agent
  timestamps are trusted for the past and up to 5 minutes into the future, and are clamped
  beyond that.
- **BR-008:** Telemetry is never discarded because of a timestamp. Clamp, don't reject.

---

## 7. Data Requirements

### 7.1 Entities involved

| Entity | Touched how | Notes |
|---|---|---|
| Device record (`devices`) | Updated only by an accepted re-registration (FR-003/004/006). Never updated by a rejected one (FR-002). | `last_telemetry` / `last_heartbeat` stay server-time (FR-040). |
| Device API keys | Read (resolve key → device; whether the device has zero keys). Created by path (b)/(c) and new-device registration. Deleted only by admin rotate (unchanged). | Path (c) depends on knowing "device has zero keys". Today's repository interface exposes no such check (see §12). |
| `registration_key` (admin config) | Read only. | Empty value = not configured. |
| Device certificates / revoked serials (CA repository) | Read only (revocation check). | Enroll endpoint unchanged. |
| Telemetry snapshots (`telemetry_snapshots`) | Timestamp clamped on insert (FR-031). Future rows excluded on read (FR-036/037). | No migration, no row rewrite (FR-038). |
| Heartbeats (`heartbeats`) | Timestamp clamped on insert (FR-032). Future rows excluded on read (FR-037). | Same. |

### 7.2 Validation rules — re-registration decision table

"Key→this" means `X-rIOt-Key` resolves to a key bound to the named device. "Key→other" means
it resolves to a different device. "Cert→this/other" means a qualifying certificate (§4
definitions) for the named device or for another device. "RK" means `X-rIOt-Key` matches the
configured registration key.

| # | Credentials presented | Reg key configured? | Named device has keys? | Result | Key issued? |
|---|---|---|---|---|---|
| V-01 | Key→other (any cert) | any | any | 403, no side effects | no |
| V-02 | Cert→other (any key) | any | any | 403, no side effects | no |
| V-03 | Key→this | any | yes | 200, metadata updated | **no** |
| V-04 | Cert→this, no key / unknown key / RK | any | any | 200, metadata updated | **yes** |
| V-05 | Cert→this + Key→this | any | yes | 200 (path a), metadata updated | **no** |
| V-06 | RK, no qualifying cert | yes | **no** (zero keys) | 200, metadata updated | **yes** |
| V-07 | RK, no qualifying cert | yes | yes | 401, no side effects | no |
| V-08 | nothing | no | any | 401, no side effects | no |
| V-09 | nothing | yes | any | 401, no side effects | no |
| V-10 | unknown key (not RK), no qualifying cert | any | any | 401, no side effects | no |
| V-11 | revoked / unverifiable cert only | any | any | 401, no side effects | no |
| V-12 | unparseable body | any | n/a | 400, no side effects | no |

New-device requests (no `device_id`, or unknown `device_id`):

| # | Reg key configured? | `X-rIOt-Key` | Result |
|---|---|---|---|
| V-20 | no | anything | 201 new device + key (unchanged) |
| V-21 | yes | matches RK (constant-time) | 201 new device + key (unchanged) |
| V-22 | yes | anything else (including a valid device key) | 401 (FR-014 body), nothing created |

### 7.3 Validation rules — timestamps

Let `R` = server receive time and `A` = skew allowance (5 minutes).

| # | Sent timestamp `T` | Stored timestamp | Logged? |
|---|---|---|---|
| V-30 | zero / missing | `R` | no |
| V-31 | `T ≤ R + A` (including any past `T`) | `T` (as sent) | no |
| V-32 | `T > R + A` | `R` (clamped) | yes, debounced: ≤ 1 per device per kind per hour (FR-035) |

Read-side: for read time `Q`, rows with stored timestamp `> Q + A` are excluded from every
latest/history/window read of snapshots and heartbeats (FR-036, FR-037).

### 7.4 State transitions — device credential state

| From | Event | To | Observable outcome |
|---|---|---|---|
| (none) | New-device registration accepted (V-20/V-21) | Has keys | 201 + key |
| (none) | Enrollment via bootstrap key (unchanged) | Zero keys, has cert | No API key |
| Zero keys | Re-registration via path (b) or (c) | Has keys | 200 + key |
| Has keys | Re-registration via path (a) | Has keys (unchanged set) | 200, `api_key: ""` |
| Has keys | Re-registration via path (b) without a valid own key | Has keys (+1) | 200 + key; prior keys remain valid (FR-005) |
| Has keys | Admin **Rotate key** | Has keys (exactly 1, new) | All prior keys invalid |
| Has keys | Registration-key-only re-registration | Has keys (unchanged) | 401, no side effects |
| any | Unauthenticated / wrong-device re-registration | unchanged | 401 / 403, no side effects |

---

## 8. Acceptance Criteria

**AC-001 — Own API key re-registers, no new key (open server)** *(FR-003, FR-013)*
- **Given** no registration key is configured and an existing device D with API key K
- **When** a request re-registers D (`device_id` = D) with `X-rIOt-Key: K` and a changed hostname
- **Then** the response is 200 with `api_key` empty
- **And** D's hostname is updated and D's status is online
- **And** no new API key exists for D (K is still D's only key).

**AC-002 — Periodic re-registration works when a registration key is configured** *(FR-003, FR-017, FR-029)*
- **Given** a registration key RK is configured and existing device D has API key K
- **When** the agent re-registers D with `X-rIOt-Key: K`
- **Then** the response is 200 with `api_key` empty, D's metadata is updated, and no new key is created
- **And** the request is not rejected for failing to present RK (Bug C regression).

**AC-003 — Key bound to a different device is 403 with no side effects** *(FR-009, FR-002, FR-014)*
- **Given** existing devices D1 and D2, where K2 is bound to D2
- **When** a request re-registers D1 with `X-rIOt-Key: K2` and altered hostname/tags
- **Then** the response is 403 with the fixed 403 body, which contains neither D1's nor D2's ID or hostname
- **And** D1's record is byte-for-byte unchanged, no key is created, no WebSocket device update is broadcast, and no `DeviceOnline` event is emitted.

**AC-004 — No key, no registration key: Flaw A closed** *(FR-001, FR-002, FR-008)*
- **Given** no registration key is configured and an existing, currently offline device D that has API keys
- **When** an unauthenticated request re-registers D with no `X-rIOt-Key` header
- **Then** the response is 401 and contains no `api_key`
- **And** no API key exists for D other than the ones it had before
- **And** D's record is unchanged (still offline), no WebSocket broadcast occurs, and no `DeviceOnline` event is emitted.

**AC-005 — Garbage key cannot overwrite metadata (Flaw B closed)** *(FR-011, FR-002)*
- **Given** no registration key is configured and existing device D
- **When** a request re-registers D with `X-rIOt-Key: garbage` and a different hostname, tags, and hardware profile
- **Then** the response is 401 and D's hostname, tags, hardware profile, and status are unchanged.

**AC-006 — Registration key bootstraps a key for a keyless device** *(FR-006, FR-018)*
- **Given** registration key RK is configured and existing device D has zero API keys (for example, created by mTLS enrollment)
- **When** a request with no client certificate re-registers D with `X-rIOt-Key: RK`
- **Then** the response is 200 with a non-empty `api_key`
- **And** that key subsequently authenticates D's telemetry endpoint.

**AC-007 — Registration key cannot mint a key for a device that has keys** *(FR-007, FR-002)*
- **Given** registration key RK is configured and existing device D has one or more API keys
- **When** a request with no client certificate re-registers D with `X-rIOt-Key: RK`
- **Then** the response is 401 with no `api_key`, D's record is unchanged, and D's key set is unchanged.

**AC-008 — mTLS-enrolled device obtains its first key via certificate** *(FR-004, FR-018, FR-029)*
- **Given** mTLS is enabled and device D was created by enrollment (zero API keys), and the agent presents a qualifying certificate with CN = D
- **When** it re-registers D with no `X-rIOt-Key` (or with RK, or with an unknown value), with or without a registration key configured
- **Then** the response is 200 with a non-empty `api_key` bound to D
- **And** D's metadata is updated.

**AC-009 — Certificate plus own key issues no new key** *(FR-003, FR-013)*
- **Given** device D has API key K and the agent presents a qualifying certificate with CN = D
- **When** it re-registers D with `X-rIOt-Key: K`
- **Then** the response is 200 with `api_key` empty and no new key is created.

**AC-010 — Certificate path keeps prior keys valid** *(FR-004, FR-005, FR-011)*
- **Given** device D has API key K, and the agent presents a qualifying certificate with CN = D
- **When** it re-registers D with `X-rIOt-Key: garbage`
- **Then** the response is 200 with a new non-empty key K'
- **And** both K and K' authenticate D afterwards.

**AC-011 — Revoked certificate confers nothing** *(FR-012)*
- **Given** device D's certificate serial is on the revoked list, and the request presents that certificate with no other valid credential
- **When** it re-registers D
- **Then** the response is 401 with no side effects.

**AC-012 — Certificate for a different device is 403** *(FR-010, FR-002)*
- **Given** devices D1 and D2, and a qualifying certificate with CN = D2
- **When** a request presenting it re-registers D1 (with or without any `X-rIOt-Key`)
- **Then** the response is 403 with the fixed 403 body and D1 is unchanged.

**AC-013 — Revocation list unavailable fails closed** *(FR-012)*
- **Given** the revoked-serials list cannot be read and the request presents an otherwise qualifying certificate for D with no other valid credential
- **When** it re-registers D
- **Then** the response is 401, no key is issued, and D is unchanged.

**AC-014 — Uniform 401 body; no UUID existence disclosure with a registration key** *(FR-014, FR-015, NFR-004)*
- **Given** a registration key is configured
- **When** an unauthenticated caller sends (i) a re-registration for an existing UUID with no credential, (ii) one with a garbage key, (iii) a registration key against a device with keys, and (iv) a registration for an unknown UUID with a wrong key
- **Then** the responses to (i), (ii), and (iv) are byte-identical in status and body
- **And** the response to (iii) is identical to them as well
- **And** no 401 body contains a device ID, hostname, key, or rejection reason.

**AC-015 — Open new-device registration unchanged** *(FR-019)*
- **Given** no registration key is configured
- **When** a request with no `device_id`, and separately one with an unknown `device_id`, is sent with no `X-rIOt-Key`
- **Then** each response is 201 with a new server-generated `device_id` and a non-empty `api_key`.

**AC-016 — New-device registration with a registration key configured** *(FR-020, FR-021)*
- **Given** registration key RK is configured
- **When** a new-device request presents RK
- **Then** the response is 201 with a new device and key
- **And** when a new-device request presents a missing or different value, the response is the FR-014 401 and no device is created.

**AC-017 — Device key is not a registration key for unknown UUIDs** *(FR-022)*
- **Given** registration key RK is configured and K is bound to existing device D
- **When** a request with an unknown `device_id` presents `X-rIOt-Key: K`
- **Then** the response is 401 and no device is created.

**AC-018 — Constant-time registration key comparison** *(FR-021, NFR-003)*
- **Given** the implementation of every registration key comparison
- **When** it is inspected
- **Then** each comparison uses a Go standard-library constant-time primitive over equal-length inputs (for example, over fixed-length digests), with no early-exit string comparison against the registration key anywhere in the registration path.

**AC-019 — Malformed body is 400 with no side effects** *(FR-023)*
- **Given** any server configuration
- **When** a registration request has an unparseable body
- **Then** the response is 400, no device is created or modified, and no key is created.

**AC-020 — Rejections are logged without secrets** *(FR-016, NFR-002)*
- **Given** rejected re-registrations of each class in FR-016
- **When** the server logs are inspected
- **Then** each rejection produced exactly one warn-level entry containing the supplied `device_id`, client address, and rejection class
- **And** no log entry contains the presented key value, the registration key, or any API key.

**AC-021 — Recovery via admin Rotate key** *(FR-025, FR-026)*
- **Given** device D has keys and an agent configured for D with no valid key is rejected with 401
- **When** an admin calls Rotate key for D and sets the returned key as the agent's `server.api_key`
- **Then** the agent's next re-registration of D returns 200 via path (a), and no previously issued key for D still authenticates.

**AC-022 — Recovery via deleting the device ID file** *(FR-027, FR-019, FR-020)*
- **Given** an agent rejected as in AC-021
- **When** its device ID file is deleted and it registers again (with RK in `server.api_key` if the server has a registration key)
- **Then** it is registered as a new device with a new `device_id` and key (201)
- **And** the original device record still exists and is unchanged.

**AC-023 — Documentation states the recovery path and behavior change** *(FR-027, FR-028)*
- **Given** the README and CHANGELOG at the release that contains this story
- **When** they are read
- **Then** the README describes both recovery alternatives of FR-027, including the `server.api_key` field, the `/var/lib/riot/device-id` default path, and the fact that mTLS agents self-recover
- **And** the CHANGELOG records that keyless re-registration of an existing device is now rejected.

**AC-024 — Current agent works unmodified** *(FR-029, NFR-006)*
- **Given** the currently released agent binary with no code changes
- **When** it performs a first install with RK, a first install against an open server, a startup and a periodic re-registration with its own key (with and without RK configured), and a first key acquisition after mTLS enrollment
- **Then** each flow succeeds with the outcomes of AC-015/AC-016, AC-001/AC-002, and AC-008 respectively.

**AC-025 — Future-dated snapshot is clamped and accepted** *(FR-030, FR-031, FR-034)*
- **Given** a device posts a snapshot whose timestamp is 1 day after server receive time R
- **When** the server ingests it
- **Then** the response is the normal success response
- **And** the stored snapshot's timestamp equals R (within the ingest request's own duration).

**AC-026 — Future-dated snapshot never becomes latest** *(FR-031, FR-036)*
- **Given** a device posts a snapshot dated 1 year ahead, followed by a correctly dated snapshot S
- **When** the device's latest snapshot is requested
- **Then** S is returned, and the 1-year-ahead content never appears as latest at any later time.

**AC-027 — Past-dated snapshot stored as sent** *(FR-033)*
- **Given** a device has a latest snapshot at time T0, then replays a buffered snapshot dated T0 − 2 hours
- **When** the server ingests the replay
- **Then** the replay is stored with timestamp T0 − 2 hours exactly as sent
- **And** the device's latest snapshot is still the one at T0.

**AC-028 — Within-allowance future timestamp kept as sent** *(FR-030, FR-033)*
- **Given** a snapshot timestamped R + 4 minutes and one timestamped R + 6 minutes, where R is the server receive time
- **When** each is ingested
- **Then** the first is stored with R + 4 minutes and the second with R.

**AC-029 — Future-dated heartbeat is clamped** *(FR-032, FR-034)*
- **Given** a device posts a heartbeat dated 1 day ahead
- **When** the server ingests it
- **Then** the response is the normal success response and the stored heartbeat timestamp equals the server receive time.

**AC-030 — Past-dated heartbeat stored as sent** *(FR-033)*
- **Given** a device posts a heartbeat dated 10 minutes in the past
- **When** the server ingests it
- **Then** it is stored with that timestamp unchanged.

**AC-031 — Clamp logging is debounced** *(FR-035, NFR-009)*
- **Given** one device posts 100 future-dated snapshots and 100 future-dated heartbeats within 1 hour
- **When** the server logs are inspected
- **Then** there is at most one clamp warn entry for that device's snapshots and at most one for its heartbeats in that hour
- **And** the next entry after the window reports the number of clamps suppressed since the previous one.

**AC-032 — Pre-existing future rows excluded from every latest-snapshot read** *(FR-036, FR-038)*
- **Given** a snapshot row already stored with a timestamp 1 year ahead, and an older correctly dated row for the same device
- **When** the per-device latest snapshot, fleet-wide latest snapshots, latest summaries, GPU device list, and container leaderboard are each read
- **Then** every one of them uses the correctly dated row and none uses the future-dated row.

**AC-033 — Pre-existing future rows excluded from history and heartbeat windows** *(FR-037)*
- **Given** stored snapshot and heartbeat rows timestamped 1 day ahead
- **When** snapshot history, heartbeat history, and the fleet heartbeat window are read
- **Then** none of the future-dated rows are returned.

**AC-034 — No migration, no row rewrite** *(FR-038, NFR-012)*
- **Given** the change set for this story
- **When** it is reviewed
- **Then** it contains no new schema migration and no statement that deletes or updates existing snapshot or heartbeat rows.

**AC-035 — UI never shows a negative age** *(FR-039)*
- **Given** a telemetry or heartbeat timestamp later than the browser's current time
- **When** the UI renders its relative age
- **Then** the rendered text is the formatter's zero-age text and contains no minus sign.

**AC-036 — Registration rate limit unchanged** *(FR-024)*
- **Given** a single client IP
- **When** it sends 11 registration requests within one minute
- **Then** the 11th is rate-limited exactly as before this story.

**AC-037 — Tests named after ACs; build and tests green** *(FR-041, NFR-010)*
- **Given** the implemented changes
- **When** `go build ./...`, `go test ./...`, and `cd web && npm run test:run` are run
- **Then** all pass
- **And** there are tests whose names reference AC-001, AC-002, AC-003, AC-004, AC-006, AC-007, AC-008, AC-025, AC-026, and AC-027 at minimum
- **And** no new Go module or npm package was added.

---

## 9. Out of Scope

1. Making new-device registration closed by default when no registration key is configured
   (BR-005).
2. Any agent code change, including new recovery prompts, retry/backoff changes, or handling
   of 401/403 from registration.
3. Any change to the enroll endpoint, bootstrap keys, certificate issuance, or certificate
   revocation flows.
4. Any change to `install.sh`.
5. A new endpoint, CLI, or UI flow for key recovery (FR-025). The existing Rotate key action
   is the recovery mechanism.
6. Deleting, rewriting, or migrating existing future-dated snapshot or heartbeat rows (FR-038).
7. Clamping or validating agent-supplied timestamps on other ingest paths (device logs,
   container logs, container metrics, command output, probe results).
8. A dashboard event, notification, or UI indicator for rejected re-registrations or clock
   skew (OQ-2).
9. (removed)
10. Timing side channels in device lookup (OQ-4).
11. Changing the registration rate limit.
12. UI redesign beyond the negative-age guard (FR-039).

---

## 10. Assumptions / Decisions made on the product owner's behalf

The product owner was unavailable. Where the brief did not specify something, the
conservative choice below was taken. Each item can be overturned. If one is, update the FRD
first; the architect must not silently choose differently.

- **PO-1 — Precedence, and a wrong-device credential wins.** If any presented credential (key
  or qualifying certificate) belongs to a different device, the answer is 403 even if another
  credential would have succeeded (FR-009, FR-010, FR-013). Mixed-identity requests indicate
  misconfiguration or abuse.
- **PO-2 — A certificate for a different device is 403, not 401.** The brief's "everything
  else 401" list did not cover this case. 403 matches the key-mismatch semantics and the
  existing `DeviceAuth` convention ("authenticated, wrong device") (FR-010).
- **PO-3 — Certificates fail closed.** A revoked certificate, a certificate with an empty CN,
  a certificate not verified by the rIOt server's own TLS handshake (for example, identity
  passed in a header by a TLS-terminating reverse proxy), or a revocation list that cannot be
  read all confer no identity. This is deliberately stricter than the existing
  `MTLSDeviceAuth` middleware, which skips the revocation check if the list read errors
  (FR-012).
- **PO-4 — Path (b) adds a key and does not revoke old ones.** This matches today's behavior.
  The certificate already proves identity, and revoking keys is the admin's explicit Rotate key
  action (FR-005).
- **PO-5 — Disclosure stance.** All 401s from the endpoint use one identical body, and all
  403s use one fixed body. Neither carries identifiers or reasons. With a registration key
  configured, UUID existence is not disclosed to unauthenticated callers. Without one, the
  201-vs-401 difference is unavoidable under open registration and is accepted (FR-014,
  FR-015, NFR-004). The existing `"invalid registration key"` body text is replaced by the
  generic body. The agent does not parse it.
- **PO-6 — Body parse before credentials.** An unparseable body returns 400 before credential
  evaluation, because the `device_id` is needed to choose the path. Today an unauthenticated
  malformed request returns 401 when a registration key is set. The 400 reveals nothing about
  devices (FR-023).
- **PO-7 — Rejections go to the server log only.** One warn line per rejection, with no
  secrets and no dashboard event. Volume is already capped by the 10/min/IP limiter. A
  reinstalled agent re-registering periodically would otherwise flood the event feed (FR-016,
  OQ-2).
- **PO-8 — Skew allowance is 5 minutes, fixed and not operator-configurable.** The boundary is
  inclusive: exactly R + 5 min is kept. The same value applies to snapshots, heartbeats, and
  the read-side guard (FR-030).
- **PO-9 — Clamp logging is debounced.** At most one warn line per device per kind
  (snapshot/heartbeat) per rolling hour, carrying the observed skew and the suppressed count.
  The state is in-memory and resets on restart (FR-035, NFR-009).
- **PO-10 — Existing future rows get a read-side guard, not a migration.** The guard covers all
  five latest-snapshot reads, and is extended to snapshot history and heartbeat history/window
  reads so a stored future row cannot appear anywhere in the UI. No rows are deleted or
  rewritten, because PART-001 partitioning is in flight and the policy is no-delete (FR-036
  to FR-038).
- **PO-11 — The UI guard renders a future timestamp as zero age.** It is applied in the
  relative-age formatter(s) only, with no layout change (FR-039).
- **PO-12 — Recovery doc specifics.** The documented field is `server.api_key` in
  `/etc/riot/agent.yaml`, and the documented ID file is the default `/var/lib/riot/device-id`.
  Both are confirmed against `scripts/install.sh` (FR-027).
- **A-1 —** On servers with mTLS enabled, the registration endpoint is served by the same TLS
  listener that performs client-certificate verification, so a qualifying certificate is
  available to the registration handler.
- **A-2 —** The released agent always sends `X-rIOt-Key` set to its configured
  `server.api_key` (possibly empty), presents its client certificate whenever one is
  configured, and persists a returned `api_key` only when it is non-empty (verified in
  `internal/agent/register.go`).
- **A-3 —** Re-running `install.sh` without `--bootstrap-key` preserves `agent.yaml` and the
  device ID file (verified), so the pending AGENT-HARDEN-001 per-device reinstall is
  unaffected by this story.
- **A-4 —** Deleting a device through the admin UI also removes its API keys. An agent of a
  deleted device therefore falls into the new-device path (FR-019/FR-020).

---

## 11. Open Questions

Each question lists the answer this FRD assumes.

- **OQ-1 — Window after Rotate key or key deletion.** Rotate key leaves the device with
  exactly one key, so path (c) stays closed. But any state in which a device has zero keys (an
  enrolled device before its first key fetch, or a future admin "revoke all keys" action) lets
  a registration-key holder bootstrap a key for it. *Assumed:* acceptable, because the brief
  explicitly allows path (c), and registration-key holders are trusted to admit devices. This
  is recorded as residual risk.
- **OQ-2 — Should rejected re-registrations or clock-skew clamps raise a dashboard event?**
  *Assumed:* No for this story (PO-7, PO-9). Revisit if the product owner wants visibility into credential
  probing or skewed devices in the UI.
- **OQ-3 — Stored future rows eventually resurface.** A stored row dated D in the future stops
  being excluded once real time reaches D − 5 minutes. At that point it can briefly count as
  latest, showing stale content until a newer real snapshot arrives, and only then can
  retention purge it. *Assumed:* acceptable residual risk; any one-off cleanup is a separate,
  explicitly approved operator action, not part of this story.
- **OQ-4 — Timing side channel on UUID existence.** A known UUID triggers credential lookups
  that an unknown UUID does not, so response timing may differ. *Assumed:* out of scope. The
  rate limit and non-secret UUIDs make this low value to an attacker.
- **OQ-5 — Key accumulation through path (b).** If an mTLS agent cannot persist an issued key
  (for example, a config write fails), each certificate-only re-registration adds another valid
  key. *Assumed:* acceptable. The rate limit bounds it, and Rotate key collapses a device back
  to one key.

---

## 12. Dependencies

| Dependency | Nature | Status |
|---|---|---|
| `internal/server/handlers/handlers.go` `RegisterDevice` | Credential evaluation, precedence, uniform responses, Bug C/D fixes (FR-001 to FR-024). | Present; change required. |
| Device repository (`internal/server/db/interfaces.go` `DeviceRepository`, `internal/testutil/mocks.go`) | Path (c) needs a way to know whether a device has zero API keys. No such method exists today. | Present; additive change likely, and mocks must follow. |
| CA repository (`CARepository.ListRevokedSerials`) and access from the registration handler | Path (b) revocation check. `Handlers`/`HandlerDeps` has no CA repository today (only `EnrollHandler` does). | Present; wiring change required. |
| Server TLS config (`server.go` `addMTLSConfig`, `VerifyClientCertIfGiven`) | Supplies handshake-verified peer certificates (A-1). | Present; no change expected. |
| HARDEN-002 (`claude/youthful-austin-8d4743`, `registrationKeyMatches`) | Constant-time comparison overlap. Do not redeclare the symbol. Expect a textual conflict in `RegisterDevice`. | Unmerged; coordinate merge order. |
| `Telemetry` / `Heartbeat` handlers (`handlers.go` ~L266, ~L332) | Clamping on ingest and debounced logging (FR-030 to FR-035). | Present; change required. |
| `internal/server/db/telemetry_repo.go` | Read-side guard on `GetLatestSnapshot`, `GetAllLatestSnapshots`, `GetAllLatestSummaries`, `GetGPUDeviceIDs`, `GetFleetContainerLeaderboard` (FR-036), and on `GetHistory`, `GetHeartbeatHistory`, `GetFleetHeartbeats` (FR-037). | Present; change required, with no DDL. |
| PART-001 (`part-001-partitioning`, unpushed) | Same table family. This story must stay migration-free (FR-038). | In flight; coordinate. |
| `web/src/pages/FleetOverview.tsx` `formatAgo` (and any other relative-age formatter for telemetry/heartbeat timestamps) | Negative-age guard (FR-039). | Present; minimal change. |
| Admin Rotate key (`POST /api/v1/devices/{id}/rotate-key` + existing UI button) | Recovery path (FR-026). | Present; no change. |
| `README.md`, `CHANGELOG` | Recovery procedure and behavior-change notice (FR-027, FR-028). | Technical writer, end of pipeline. |
| Any future feature exposing device UUIDs or telemetry age outside the authenticated API | Must ship after this story (NFR-011). | Not yet built. |
| Release process | A code change requires a tagged release. | Present. |
