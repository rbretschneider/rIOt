# Security Review Report
**Story ID:** EXPECT-008
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-27
**Verdict:** APPROVED WITH CONDITIONS

Inputs:
- `docs/requirements/EXPECT-008-frd.md` (revision 1, commit 05144a7)
- `docs/architecture/EXPECT-008-add.md` (revision 1, commit 5e7cc31)
- Inherited reviews: `docs/security/EXPECT-001-security-review.md` (C-1 to C-8),
  `docs/security/EXPECT-002-security-review.md` (C-1 to C-6)
- Code at 5e7cc31:
  - `scripts/install.sh` (whole file; the reinstall path l.482-1200, `config_facts` l.199-388, `download_file`)
  - `scripts/test_install_maintenance.py` (fixture stubs and how the installer is invoked)
  - `internal/server/router.go`, `internal/server/middleware/auth.go`, `internal/server/ingest/limits.go`
  - `internal/server/handlers/handlers.go` (`ReceiveAgentEvent`, `generateAPIKey`), `agent_event_self_update.go`,
    `commands.go` (`authenticatedDeviceID`), `fleet.go` (`BulkUpdateAgents`)
  - `internal/server/commands/self_update.go`, `internal/server/db/self_update_repo.go`, `command_repo.go`
    (`buildTransitionSQL`, `ListOverdue`)
  - `internal/server/expect/selfupdate.go`; `internal/models/expect.go` (`ExpectPolicyFor`, `ActorPhrase`,
    `DeviceIsLive`)
  - `cmd/riot-server/migrations/000033_*`, `000034_*`
  - `internal/agent/agent.go` (`trustServerCert`, client selection), `http_client.go` (`buildCertPool`,
    `NewHTTPClient`, `NewHTTPClientWithTLS`), `config.go` (`ServerConfig`, `AgentConfig`)

Two installer assumptions were checked empirically in a disposable `debian:bookworm-slim` container on Clod's
workstation (curl 7.88.1, OpenSSL 3.0). No production system was touched. The results are summarised in SEC-001 and
SEC-002; the raw probe details are in Ryan's private review notes, not in this public repository.

---

## Threat Model Summary

### Assets
- **The device key.** For the first time, `install.sh` (root) sends a device's `X-rIOt-Key` over the network. Until
  now an installer re-run only contacted GitHub. The key authenticates every agent endpoint of that device: holding it
  allows faking heartbeats, telemetry, events and command reports for that device.
- **Alerting integrity.** An accepted notice turns `device_offline` into an `info` event for up to 10 minutes. Any
  path that opens, extends or chains a window, or ends one without its backstop, hides an outage.
- **Installer integrity on the device.** The notice runs as root inside the installer, between the last validation
  step and `systemctl stop`. It must never abort, hang, truncate or redirect the rest of the run, and it must not give
  the unprivileged `riot` user any new lever over root.
- **Operator command integrity.** Reinstall records live in `commands` next to operator, auto-update, auto-patch and
  self-update rows.
- **Dashboard truth and Ryan's attention.** The "Reinstalling agent" label and the "Expected offline" count replace
  "offline" for up to 10 minutes. The story adds at most one escalation push per record.
- **Bounded growth on the device and the server** (Ryan's standing rule).

### Threat Actors
- **Compromised `riot` user on the device.** `/etc/riot` and `agent.yaml` are `riot`-owned and writable at notice
  time (install.sh l.1114 and l.1168 hand them to `riot`; the agent is running). This actor controls every input the
  notice reads: URL, key, ID, trust settings, the certificate and key files, and their timing (symlink swaps). It
  already holds the device key. What matters is whether it can make **root** do anything it could not do itself.
- **Holder of a stolen device key** (from a backup, or sniffed from an `http://` deployment), on or off the device.
  It can send notices like the installer, within the abuse budget.
- **Network attacker on the path between device and server.** Against `https://` it must defeat the trust anchor the
  agent uses. Against `http://` it already sees the key in every heartbeat.
- **Honest operator with an unlucky outage.** A real failure during a reinstall (accepted NFR-010 residual).
- **The server itself** (or anything answering at the configured URL). It controls the response the installer reads.
- **Environment.** `curl | sudo bash` piping, root's environment variables, bookworm-era tool versions, server
  restarts and DB errors, clock steps.
- **Future developer (regression).** Tests that run real power, package or agent actions (NFR-001); refactors that let
  a generic command path touch a reinstall row.

### Attack Surface Introduced
- A root-run network client in `install.sh` that reads a `riot`-writable config and up to three `riot`-writable trust
  files, and sends the device key.
- A new accepted type, `agent_reinstall_started`, on the existing device-key route `POST /api/v1/devices/{id}/events`,
  which opens an alert-suppression window without any other proof.
- A new origin `installer`, a restricted actor `ActorReinstall`, a shape CHECK and two partial indexes (migration
  000035), plus one predicate added to an EXPECT-002 query.
- `DeviceActivity.origin` and two server-authored event types.
- No new agent endpoint, agent privilege, dependency or sudoers change. One CI change (`curl` and `gawk` in the
  fixture image).

---

## Findings

There are no CRITICAL findings. There is one HIGH finding with a non-architectural resolution.

**What holds** (details in INFO-1 to INFO-4):
- Cross-device isolation holds by construction. A key for another `{id}` is rejected (403) before the handler runs.
- The abuse budget is as the FRD states. Overlapping or staggered windows cannot extend a single outage past the
  first covering bound.
- The EXPECT-002 attribution change is a narrowing and is required.
- The curl-config injection argument in AD-014/AD-016 is sound for the characters it forbids.
- Every server-side failure path fails toward alerting.

**What does not hold yet:**
- The installer's TLS trust is **wider** than the agent's in the pinned and CA-file configurations (SEC-001).
- The response-size bound the ADD states is not enforced by the curl version on bookworm and DietPi (SEC-002).
- Under the documented `curl … | sudo bash` invocation, the notice's I/O is not isolated from the script stream
  (SEC-003).
- The device-key validation rejects every real key (SEC-005). It fails safe, but the feature would never work, and
  the tests can pass anyway.

### HIGH

#### SEC-001: With a pinned certificate or a CA file, the installer also trusts the system CA store
**Severity:** HIGH
**Domain:** Cryptography / TLS trust parity (FR-003, BR-006, NFR-006)
**Location:** ADD AD-015 rows "`server.crt` present" and "`ca_cert_file` set"; AD-016 `$TLS_ARGS`; AC-005 tests in
Section 8

**Description:**
- The agent's trust in these two configurations is exactly one anchor. With `/etc/riot/server.crt` present,
  `trustServerCert` sets `CACertFile` to it, and `buildCertPool` builds a pool from it alone. With a CA file, the pool
  is that file alone. Go never adds system roots to a non-nil `RootCAs` pool.
- The ADD asserts that the installer's CA-file option makes "a server presenting any other certificate fail the
  handshake". That is not true for the curl shipped on bookworm (7.88.1, OpenSSL backend).
- Verified in a disposable container: a CA-file option naming an unrelated self-signed certificate **still accepted a
  public site whose certificate chains to the system store**. Only when the default CA directory was also overridden
  did the handshake fail.
- So in the default homelab configuration (TOFU-pinned self-signed server), the installer trusts the pinned
  certificate **plus every publicly trusted CA**, while the agent trusts only the pinned certificate. That is precisely
  the downgrade FR-003 forbids: the key can be sent to a server the agent would refuse.
- The planned test `test_EX8_AC005_pinned_certificate_mismatch_no_key_sent` cannot detect this. Its listener presents
  a test certificate that is not in the system store, so it fails for the right result but the wrong reason.

**Attack Scenario:**
1. Ryan's devices reach the server under a name for which an attacker can obtain, or already holds, a certificate
   from any publicly trusted CA (mis-issuance, a compromised CA, or a name the attacker controls through DNS or a
   reused domain).
2. The attacker has an on-path position between a device and the server (LAN ARP or DNS spoofing, a compromised
   router).
3. Ryan re-runs the installer. The agent, pinned to the self-signed certificate, would reject the attacker's
   endpoint. The installer accepts it and sends the device key.
4. With the key, the attacker can impersonate the device to the real server: fake heartbeats that mask a real outage,
   fake telemetry and events, and command reports. With mTLS off, nothing else is required.

For a server addressed by a private IP or a private name, step 1 is hard. That is why this is HIGH and not CRITICAL.
It still breaks a stated MUST, on every stock install, and the planned test would hide it.

**Required Resolution (properties):**
- (a) In every AD-015 row that names a trust file (`server.crt` snapshot or CA-file snapshot), the set of anchors the
  installer's TLS client accepts must be **exactly** the snapshot. The system CA bundle, the default CA directory and
  any environment-supplied CA locations must all be excluded. This must hold on every curl the installer can run on
  (bookworm 7.88 and trixie 8.x at minimum), whatever its compile-time defaults.
- (b) AD-015 and AD-016 state property (a) explicitly, together with the reason (compile-time default CA directory).
- (c) AC-005 gains a test that fails under today's design:
  - the fixture installs a test CA into the container's **system** trust store;
  - the listener presents a certificate for `127.0.0.1` issued by that CA;
  - `server.crt` (and, in a second subtest, `ca.crt`) is a different certificate;
  - expected: zero HTTP requests reach the listener, and the line reads "TLS verification failed".
  - A positive control in the "no trust file" row shows the same listener **is** reachable through the system store,
    which proves the test can tell the two cases apart.
- (d) The "system roots" row (no trust file, verification on) stays as designed: curl's default store is the same
  Debian store Go reads.

**Blocks:** Implementation of Part I until the ADD states (a) to (c). Condition C-1. Parts G and W are not affected.

---

### MEDIUM

#### SEC-002: The response-size bound is not enforced on bookworm, and the response lands in `/tmp`
**Severity:** MEDIUM
**Domain:** Resource exhaustion / bounded growth (Ryan's standing rule, NFR-005)
**Location:** ADD AD-016 (`--max-filesize 4096 --output "$NS/response"`), AD-012 "Installer, on the device" row

**Description:**
- The ADD relies on curl's max-filesize option to cap the response at 4096 bytes.
- On curl 7.88.1 (bookworm, and DietPi on bookworm) that option only applies when the size is announced up front.
  Verified in a disposable container: a response without a declared length was written in full, 12.5 MiB in that
  probe, despite the 4096-byte limit.
- The only remaining bound is time (≤ about 7 s) multiplied by link speed. On a gigabit LAN that is hundreds of MiB.
- `$STAGE` is under `/tmp`, and DietPi mounts `/tmp` as tmpfs, so the response would consume RAM on a Raspberry Pi.
  That memory is held until the notice deletes it a few seconds later, but the peak is unbounded by bytes.
- The AD-012 statement "body (≤ 4096 bytes)" is therefore false on the main target platform.

**Attack Scenario:**
1. Whoever answers at the configured URL controls the response. That is the verified server on `https://`, or any
   on-path party on `http://`.
2. During an installer re-run, it answers the notice with an endless body of undeclared length.
3. For up to about 7 s the installer writes it into tmpfs. On a small Pi this can push the host into memory pressure
   or OOM kills during the reinstall, a moment when Ryan expects disruption and is least likely to notice.

**Required Resolution (properties):**
- (a) The number of response bytes the installer stores or reads must be bounded by a byte count (≤ 4096 is enough
  for the disposition token). This must hold whatever the transfer encoding and whatever curl version runs.
- (b) AD-012 states that bound and how it holds on curl 7.88.
- (c) Installer test: the listener streams an oversize body of undeclared length. Expected: at no point does the
  stored response exceed the bound; the line reads "unexpected server response"; the run completes with today's exit
  status.

**Blocks:** No (fix before QA sign-off). Condition C-2.

---

#### SEC-003: Under `curl … | sudo bash`, the notice's I/O is not isolated from the script stream, and the fixture never runs that way
**Severity:** MEDIUM
**Domain:** Business logic / installer integrity (FR-005, NFR-006)
**Location:** ADD AD-013, AD-016; Section 8 (I tests); `scripts/test_install_maintenance.py` (always runs
`bash install.sh`, never a piped script)

**Description:**
- The documented install command pipes the script into `bash -s`. With a piped script, bash reads its input one byte
  at a time, and every child process inherits that pipe as stdin.
- Any command in the notice block that reads stdin, other than curl reading its own config pipe, consumes the
  **rest of the installer**. Bash then reaches end of input and exits with the last status, which is 0.
- Verified locally with a three-line piped script: the lines after a stdin reader never ran, and the exit status was
  0.
- Consequences: "Server notified" is printed, then the run ends silently. The agent is never stopped or replaced, and
  the installer exits 0. The record ends "not confirmed" with a warning. That violates FR-005 ("never change any later
  step"), and Ryan gets a false success.
- The current design does not read stdin anywhere by intent. But nothing in the ADD states the property, and no test
  would catch a regression (a `read`, a `cat` or an awk call without a file operand, added later).
- Related: the subshell keeps `set -u`, and bash's own diagnostics (unbound variable, integer-expression errors) go to
  the installer's stderr. That breaks the "exactly one line" rule (FR-006). In the worst case it echoes a value, if a
  future edit runs a test or arithmetic on a config-derived string.

**Attack Scenario:**
This is mainly a robustness defect with a security edge. A compromised `riot` user cannot inject a stdin reader, but
it can choose config values that drive the notice down rarely tested branches. If any such branch reads stdin or
errors out, a re-run Ryan believes succeeded leaves the old, possibly tampered, agent binary running, with a
"notified" line on screen.

**Required Resolution (properties):**
- (a) No command in the notice block may read the installer's stdin. curl's stdin is only the config pipe; every other
  command reads from an explicit file or from nothing. AD-016 states this.
- (b) Everything the notice subshell writes to stderr, including bash's own diagnostics, is discarded. Its only
  output is the one fixed FR-006 line on stdout. AD-016 states this.
- (c) Installer test: one EX8 run with the notice accepted, executed as a **piped** script (the script fed to
  `bash -s` on stdin), asserting:
  - `systemctl stop`, the binary installs and `systemctl start` all appear in the trace;
  - the final "rIOt installed" line is printed;
  - the exit status equals today's.
- (d) Every "not notified" path of the AD-016 table, including a forced internal error such as an unset variable,
  prints exactly one line, and nothing on stderr.

**Blocks:** No (fix before QA sign-off). Condition C-3.

---

### LOW / INFORMATIONAL

#### SEC-004: Root's environment can widen curl's trust or make it write TLS secrets
**Severity:** LOW
**Domain:** Secrets / configuration (NFR-003, FR-003)
**Location:** ADD AD-016 step 2 (unsets four variables)

**Description:**
- AD-016 unsets `CURL_CA_BUNDLE`, `CURL_HOME`, `SSL_CERT_FILE` and `SSL_CERT_DIR`, and uses `-q` and a no-proxy
  option. Other inherited variables still change curl's behaviour.
- The one that matters for NFR-003 is the TLS key-log variable that OpenSSL-based curl honours. It writes the session
  secrets of the notice handshake to a file, and with a packet capture those secrets reveal the device key.
- Developers often set it for Wireshark. `sudo -E`, or a root shell that inherited it, passes it through. The Go agent
  does not honour it, so this is also a parity gap.
- The OpenSSL configuration variable can likewise change trust behaviour.
- The bash validation regexes run in the caller's locale. With the C locale their character classes are exactly
  ASCII. In other locales, ranges can admit non-ASCII bytes. No injection was found from this, because the forbidden
  quote, backslash and whitespace stay excluded, but the result depends on the host's locale.

**Attack Scenario:**
Ryan, debugging TLS on a device, has exported a key-log path in his shell and runs the installer through `sudo -E`.
The notice handshake's secrets are written to that file. Anyone who later reads the file and holds a capture of the
LAN traffic recovers the device key. No attacker action on the device is needed.

**Required Resolution (properties):**
- (a) curl in the notice runs with an **allowlisted** environment: a fixed `PATH` and a C locale, nothing inherited.
  The bash parsing and validation inside the notice subshell run under a C locale.
- (b) AD-016 states this in place of the four-variable list.
- (c) Installer test: the installer runs with the key-log variable and an alternative CA-bundle variable set in its
  environment. Expected: no key-log file is created; the trust result equals the run without them.

**Blocks:** No. Condition C-4.

---

#### SEC-005: Key validation rejects every real device key (fails safe, but the feature never works, and tests can still pass)
**Severity:** LOW (functional; fail-safe direction)
**Domain:** Input validation
**Location:** ADD AD-014 (key regex and the awk plain-scalar character set); AC-006 fixture key

**Description:**
- Every device key is `riot_` followed by 64 hex digits (`generateAPIKey`, also used by rotation). It contains an
  underscore.
- AD-014's key pattern and the awk plain-scalar character set both omit `_`. So on every real device, the notice
  takes the "agent.yaml not readable" path, prints "not notified", and re-runs page exactly as today.
- That is safe, but it is a silent no-op. AC-001 and AC-006 can still pass if the fixture writes a key in some other
  format, for example a random alphanumeric marker.

**Attack Scenario:**
None for an attacker. The risk is a false sense of protection: the story ships, Ryan sees "not notified" on every
device, and the cause is not obvious.

**Required Resolution (properties):**
- (a) Validation accepts the server's real device-key format and should be no broader than it (that format exactly is
  the tightest choice). Quote, backslash, whitespace, CR and LF stay forbidden, so the curl-config argument of AD-016
  still holds. The awk scalar reader accepts the same characters.
- (b) Every EX8 fixture key, including the AC-006 marker, uses the production format, with the marker carried in the
  hex part.

**Blocks:** No. Condition C-5.

---

#### SEC-006: R-1 (symlink race on the trust files) is real but narrow; its impact statement and R-3 need correcting
**Severity:** LOW
**Domain:** Privilege boundary (root reading `riot`-controlled paths)
**Location:** ADD AD-014 `notice_snapshot`, AD-015, Residuals R-1 and R-3

**Description:**
- `notice_snapshot` checks "regular file, not a symlink, one link" and then opens the path. `/etc/riot` is
  `riot`-owned, so a compromised `riot` can swap the path between the check and the open.
- Impact, assessed:
  - CA anchors are only parsed locally.
  - A client certificate is public material.
  - A root-readable PEM private key could be used by root to sign the CertificateVerify of **one** TLS handshake,
    toward a server `riot` chose. That is not a chosen-message signing oracle, because the transcript includes
    curl's random. No practical relay was found against TLS 1.3 or TLS 1.2 with extended master secret.
  - No file content leaves the host.
  - The same race on `agent.yaml` yields nothing usable: a swapped-in root file would have to parse as a complete,
    valid config, and its key and URL would come from the same file.
- So R-1 is acceptable as a residual. But the race can be closed cheaply: verify the inode **after** opening, not
  before.
- R-3 understates what a SIGKILLed run leaves behind. `$STAGE/notice` can also hold the snapshots of the mTLS client
  key and certificate (root, 0700), not only the config.

**Attack Scenario:**
A compromised `riot` user, timing a swap of `client.key` during Ryan's re-run, makes root complete one TLS
client authentication with a different root-readable key, toward an endpoint `riot` chose. No practical gain beyond
what `riot` already has was identified.

**Required Resolution:**
- (a) Either close the race (the bytes snapshotted must come from the inode that passed the regular-file, no-symlink
  and one-link checks, verified against the opened file and not only the path), **or** keep R-1 with the impact
  stated as above.
- (b) R-3 lists the client-key and certificate snapshots.

**Blocks:** No. Condition C-6.

---

#### INFO-1: Cross-device isolation and who can open a window
- `POST /api/v1/devices/{id}/events` sits behind `DeviceAuth`:
  - missing key → 401;
  - unknown or revoked key → 401;
  - a key whose device differs from `{id}` → 403;
  - when mTLS is on, `MTLSDeviceAuth` runs first;
  - the ingest bucket (per device) follows.
- AD-002 re-checks `authenticatedDeviceID(r) == {id}` and answers `unavailable` on a mismatch.
- Every new query and the coverage are scoped by device. The advisory-lock key is per device.
- So only the holder of **that** device's key (and client certificate, with mTLS) can open its window. A notice can
  never affect another device.
- **Replay** is key possession. Over `https://` a capture yields nothing. Over `http://` the key is already in every
  heartbeat, and faking heartbeats is a far stronger capability than a 10-minute window (R-2 stands).
- `ReceiveAgentEvent` validates types against a fixed allowlist. The server-authored types
  `agent_reinstall_unconfirmed` and `agent_reinstall_paused` must stay off it (QA gate, C-7).

#### INFO-2: Abuse budget, verified against the Tracker
- Each window's backstop is evaluated on its own. The `finishSelfUpdate` pattern escalates whenever that window's
  coverage contains the ongoing offline, regardless of which window holds the link.
  - So an outage covered by an EXPECT-002 window and a reinstall window escalates at the **earlier** bound.
  - Staggering notices cannot extend a single outage.
  - DEC-4 (refuse when offline) stops chaining across an outage.
- **Per device:** at most 6 reinstall plus 6 self-update windows per rolling 24 h. Each silent outage is under about
  10 minutes of server-observed offline time; real downtime is up to about 45 s more, because offline time is
  backdated to the last heartbeat plus 45 s. That gives the FRD's < 60 minutes per day from reinstalls and < 120
  minutes combined. Every covered transition is stored as `info`.
- **Stolen key used off the device:** a notice sent shortly after the device died, while the server still shows it
  online, covers that outage because of the 45 s backdating. This is identical in kind to EXPECT-002 and within the
  same budget.
- **Overlapping windows** can produce two escalation pushes for one outage (one marker per record). This errs toward
  alerting and stays under the 5-per-hour ceiling.
- **Cooldown-exempt escalations:** a key holder can turn at most 12 rule-cooldown-limited pushes per day into
  cooldown-exempt ones, still under the per-device ceiling. Negligible.
- The budget needs Ryan's recorded acceptance (EXPECT-002 C-5 carried over, C-8). The ADD's "Open points for Ryan"
  already records it on his behalf.

#### INFO-3: The EXPECT-002 attribution change (AD-017) is correct and required
- Today's FR-004 query in `self_update_repo.go` matches any in-flight `agent_update` row with
  `origin <> 'agent_self_update'` and `dispatched_at` set. A running reinstall row would capture a self-update start,
  and that start would then get no record.
- Adding `origin <> 'installer'` is a pure narrowing. It cannot create extra windows; each budget still counts its own
  origin.
- `noteOutcomeEvent` and `NoteSelfUpdateRegistration` select by `origin = 'agent_self_update'`. So agent
  `agent_update_*` events are never linked to a reinstall row, and EXPECT-002 C-3 holds for reinstall records (only
  server-authored links).
- Mock parity for the new predicate is required (`TestEX8_MockGuardParity`; C-7).
- Reinstall rows have no target keys. So they never become conflict holders for `CreateIfNoConflict`, bulk agent
  updates or the web tracker: a key holder cannot block operator commands.

#### INFO-4: Interaction with operator reboot and shutdown windows
- A notice that arrives while an operator reboot or shutdown is in flight, and before the device drops, takes the
  label and the coverage link (latest D).
- The reboot or shutdown window keeps its own backstop, and EXPECT-001 C-1 powered-off entry does not depend on the
  link.
- Net effect: at most one extra "still offline 10m after Agent reinstall" push for a host Ryan deliberately powered
  off, the same as EXPECT-002 INFO-6. This errs toward alerting.
- A reinstall window can never suppress beyond its own 10-minute bound. It covers only `device_offline`
  (`Containers: none`).

#### INFO-5: Best-effort guarantees (`set -e`, `pipefail`, traps), verified against install.sh
- The call site is `reinstall_notice || true`, inside an `if` body. The function is a `( … )` subshell that turns off
  `-e`, `pipefail` and xtrace first. A subshell does not inherit the `EXIT` trap, so `cleanup` cannot run early.
  Nothing in it can change the outer exit status, apart from the SEC-003 stdin case.
- **Ctrl-C during the notice.** The subshell dies, and the outer `INT` trap exits 130 **before** the stop: the agent
  stays up, and the record ends "not confirmed" with one warning and no push (NFR-010).
  - `timeout` runs curl in its own process group, so an interrupted run can leave curl alive for up to about 8 s. It
    holds the inherited maintenance-lock descriptor during that time. Bounded and harmless. Not inheriting that
    descriptor would be tidier (optional).
- **Time bound:** ≤ 4 × 1 s of snapshots, the "skip below 3 s" budget check, then curl killed at `left` seconds plus
  1 s. That is ≤ 10 s, including the 1 s granularity of `SECONDS`.
- **Key exposure** (NFR-003), checked as designed:
  - the key reaches curl only through the `printf` builtin into a pipe; it is never on any argv and never exported;
  - xtrace is off before the config is read;
  - the only file holding the key is the existing kind of root-only 0700 snapshot;
  - here-strings are not needed. On the bash versions the installer supports (bookworm and later; older releases
    fail the existing sudo ≥ 1.9.10 gate), small here-documents use pipes anyway.
  - SEC-004 closes the remaining environment-driven leak.
- **Curl-config injection** (AD-014/AD-016): sound for the forbidden characters. With quote, backslash, whitespace, CR
  and LF excluded from the key, no second config directive can be formed. The URL and ID are argv values, not
  options, and are character-restricted. `-q` stops `.curlrc`. Redirects are not followed.
- **The URL is `riot`-controlled.** root's request carries a fixed body to a `riot`-chosen host. `riot` can already
  send the same request itself, so this grants nothing new, apart from egress firewall rules keyed on uid, which do
  not apply here.

#### INFO-6: Mixed rollout states
- **New installer, old server:** `ReceiveAgentEvent`'s `default` branch answers 400 "unsupported event type" before
  anything is stored (verified, handlers.go l.1197). The installer prints "not notified" and pages as today.
- **Old installer, new server:** no request, no record.
- **Server image rolled back without migrating down:** the old binary would treat `installer` rows as ordinary
  `agent_update` commands, with its generic policy, its sweeper and no origin guard. Section 14 already requires
  migrating down to 34 first. Keep that order in the CHANGELOG.
- The installer is live from `main` at merge. Until the deploy that is harmless, as stated.

#### INFO-7: Bounded growth
- **Server:** the bounds of AD-012 are verified:
  - ≤ 1 open row (unique index);
  - ≤ 6 per 24 h, counted in the locked transaction;
  - ≤ 540 per device over 90 days;
  - ≤ 3 markers per record;
  - ≤ 18 events per record, each offline event replacing today's warning one for one;
  - ≤ 1 cap event per device per 24 h;
  - refused or rejected notices store nothing;
  - Debug logs on the hot path.
- **Device:** the bounds hold once SEC-002 is fixed and R-3 is corrected (SEC-006).

#### INFO-8: One ADD statement to correct
- AD-015 and Section 2.1 say the agent uses no proxy ("custom `Transport`").
- In the "no trust file, verification on" case the agent uses Go's default client. That client honours proxy
  environment variables, so the agent would use a proxy if one were configured for its unit; no unit does today.
- The installer's no-proxy choice is the safe direction (a direct, verified connection, or "not notified"), so no
  change is needed beyond correcting the text.

#### INFO-9: Inherited conditions
- **EXPECT-001 C-1 to C-8** and **EXPECT-002 C-1 to C-6** are mapped in the ADD's Security addendum.
- The mapping is correct, with these notes:
  - EXPECT-002 C-2's server-edge guard is the appended `origin <> 'installer'` clause on every non-reinstall actor;
  - C-3 holds as explained in INFO-3;
  - C-5 (budget acceptance) appears in "Open points for Ryan" and must also appear in the README residuals (FR-030,
    AC-027).
- No inherited condition is weakened by this design.

---

## Positive Observations
- **The record reuses the commands-row pattern.** Durability, markers, retention exemption, history and the backstop
  come from EXPECT-001/002 unchanged. The bound is a database CHECK, and `params = '{}'` keeps untrusted text out of
  the row.
- **Nothing from the notice body is trusted or displayed.** Identity comes from `DeviceAuth` alone, and the notice is
  never stored as an event.
- **Old servers fail closed.** Reusing the event route turns a version mismatch into a clean 400 instead of a false
  200 from the SPA catch-all.
- **The installer treats `agent.yaml` as hostile input.** It uses a strict, mawk-safe reader with whole-file reject,
  bash-side validation, and a restricted set of trust-file paths.
- **The key never touches argv.** It goes through a builtin into a stdin config line, with xtrace off, and is
  validated against config-injection characters before use.
- **Every skipped configuration fails toward alerting with a visible reason**, including `tls_verify: false`, which
  the agent itself would accept.
- **The notice sits after every abort point that leaves the agent running**, and the ADD verified that no prompt sits
  between stop and start.
- **The EXPECT-002 attribution interaction was found by the architect**, not left for QA.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.** There are no CRITICAL findings. The one HIGH finding (SEC-001) has a clear,
non-architectural resolution: tighten the installer's trust-anchor property, and add a test that can actually observe
it. It does not change the mechanism, the server design or the FRD.

- The server side is sound: isolation, the budget, chaining, fail-open paths and bounds all hold.
- The installer side needs:
  - an exact trust anchor (SEC-001);
  - a byte bound on the response (SEC-002);
  - I/O isolation under the documented piped invocation (SEC-003);
  - a clean environment (SEC-004);
  - the real key format (SEC-005).

The architect folds C-1 to C-6 into ADD revision 2. The senior_dev implements them, and QA verifies them together
with C-7. Part I implementation must not start before C-1 is in the ADD; Parts G and W may proceed.

### Conditions
1. **C-1 (SEC-001, HIGH). AD-015, AD-016, Section 8 row AC-005:**
   - In the `server.crt` and CA-file rows, the installer's accepted anchor set is exactly the snapshot. The system
     bundle, the default CA directory and environment-supplied CA locations are excluded, on every supported curl.
   - State the property and its reason.
   - Add the system-store test: a listener certificate from a CA in the container's system trust store, a different
     `server.crt` (and, separately, `ca.crt`), zero requests, the "TLS verification failed" line, plus the positive
     control in the no-trust-file row.
2. **C-2 (SEC-002). AD-016, AD-012, Section 8:**
   - The stored or read response is bounded by a byte count (≤ 4096), whatever the transfer encoding and the curl
     version.
   - Correct the AD-012 bound text.
   - Add the oversize undeclared-length body test.
3. **C-3 (SEC-003). AD-013, AD-016, Section 8:**
   - No notice command reads the installer's stdin (curl reads only its config pipe).
   - All notice stderr, including bash diagnostics, is discarded; exactly one fixed stdout line.
   - Add a piped-script EX8 run (accepted notice; stop, installs, start, the final line and the exit status asserted)
     and a forced-internal-error case (one line, empty stderr).
4. **C-4 (SEC-004). AD-016 step 2:**
   - curl runs with an allowlisted environment (fixed `PATH`, C locale).
   - Parsing and validation run under a C locale.
   - Add a test with the TLS key-log and CA-bundle variables set: no key-log file, unchanged trust result.
5. **C-5 (SEC-005). AD-014, Section 8:**
   - Key validation and the awk scalar charset accept the real `riot_` plus 64-hex format, no broader than needed,
     still excluding quote, backslash and whitespace.
   - All EX8 fixture keys, including the AC-006 marker, use the production format.
6. **C-6 (SEC-006). AD-014, Residuals:**
   - Either the snapshot is verified against the opened inode, or R-1 is restated with the impact in SEC-006.
   - R-3 also lists the client key and certificate snapshots.
7. **C-7 (QA gate, in addition to the ADD's Section 13 and the inherited EXPECT-002 C-6):**
   - `agent_reinstall_unconfirmed` and `agent_reinstall_paused` posted on the agent route get 400 and store nothing.
   - `agent_reinstall_started` never creates an `events` row on any disposition.
   - The AD-017 predicate is mirrored in `MockCommandRepo` and covered by `TestEX8_MockGuardParity`.
   - A notice whose key belongs to another device gets 403 with no row, also with mTLS on.
   - C-1 to C-5 tests pass on both bookworm-slim and trixie-slim.
8. **C-8 (EXPECT-002 C-5 carried over):**
   - Ryan's acceptance of the budget (< 60 minutes per day from reinstalls, < 120 minutes combined, each silent
     outage under about 10 minutes and stored as `info`) stays in the ADD's open points.
   - The README residuals state it in those terms.

### Residual risks accepted (on Ryan's behalf, for his review in the PR)
- A holder of a device's key can make up to 6 reinstall-window outages per day silent (12 with EXPECT-002), each
  under about 10 minutes, same device only, each stored as `info`. Each escalates if it lasts past the bound.
- On `http://` deployments the notice sends the key in clear, as every heartbeat already does (R-2).
- A compromised `riot` user can make the installer print a misleading notice line, or abort after the stop. Both err
  toward alerting (a page at the bound at worst).
- R-1, as restated in SEC-006, if the race is not closed.
- Overlapping windows can cause one extra escalation push per outage, or one extra "not back" push after a deliberate
  shutdown (INFO-2, INFO-4).
