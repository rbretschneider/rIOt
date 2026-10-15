# Formal Requirements Document

**Story ID:** AGENT-HARDEN-001
**Title:** Cryptographically-verified, fail-closed agent auto-update + least-privilege update sudoers rule
**Author:** Business Developer Agent
**Date:** 2026-10-15
**Status:** DRAFT

---

## 1. Executive Summary

rIOt agents auto-update themselves to new releases and then replace their own binary as
root. Today that trust chain is broken in two places. First, the update is not
cryptographically verified: the agent fetches a SHA-256 from a server-supplied
`checksum_url`, but a checksum fetch failure only logs a warning and installs anyway, and an
empty `checksum_url` skips verification entirely — the binary and its checksum come from the
same server trust source, so a malicious or compromised server controls what every agent
runs as root. Second, the installer grants the `riot` user an arbitrary-root primitive: the
sudoers line `systemd-run --unit=riot-agent-update sh -c *` lets the agent run *any* command
as root. This story closes both holes. Release artifacts become signed with an ed25519
project key whose public half is compiled into the agent; the agent verifies that signature
before installing and **fails closed** on any verification failure. The `sh -c *` wildcard is
replaced by a fixed, argument-locked root-owned helper script invoked through wildcard-free
sudoers rules, so `sudo -l` for the `riot` user exposes no path to arbitrary root execution.
Auto-update remains default-on, but with verification now mandatory it safely refuses to
update from any release that is not signed and verified.

---

## 2. Background & Context

**Current state.** Each agent runs `updateCheckLoop` every 6 hours
(`internal/agent/updater.go`). When the server reports an update is available, the agent
downloads the platform binary to a staging path and calls `performUpdate`. Checksum handling
is advisory: at `updater.go` ~lines 154–163 a checksum is verified *only if* `checksumURL` is
non-empty, a fetch error merely logs `"could not verify checksum"` and proceeds to install,
and only an explicit hash *mismatch* aborts. The binary is then installed as root via one of
three strategies, the first of which shells out through
`sudo systemd-run --unit=riot-agent-update sh -c <script>`. The release workflow
(`.github/workflows/release.yml`) cross-compiles the binaries and emits a plain
`checksums.txt` with no signature. The server
(`internal/server/updates/checker.go`, `AgentUpdateInfo`) hands the agent an `Assets` map and
a `ChecksumURL` built from GitHub release assets. Auto-update defaults **on**:
`config.go:71` treats `Agent.AutoUpdate == nil` as enabled.

**Problem 1 — auto-update is not cryptographically verified and fails open.** Both the
binary and the checksum originate from the server trust source, so neither proves publisher
authenticity, and every non-mismatch failure path installs anyway. A forged or compromised
server (or a MITM on either fetch) can make the whole fleet execute attacker-chosen code as
root.

**Problem 2 — the installer grants an arbitrary-root primitive.** The sudoers drop-in written
by `scripts/install.sh` (~lines 437–482) contains
`riot ALL=(root) NOPASSWD: ${SYSTEMD_RUN_PATH} --unit=riot-agent-update sh -c *` and
`riot ALL=(root) NOPASSWD: ${SH_PATH} -c mv -f ...`. The trailing `sh -c *` wildcard means
the rule authorizes *any* shell command as root; a compromise of the unprivileged `riot` user
is therefore a full root compromise regardless of the update path.

**Problem 3 — default-on policy.** Auto-update being default-on is only acceptable once the
update is verified. This story keeps it default-on but makes it refuse, safely and visibly,
to update from any release that is not signed and verified — which means agents will not
auto-update until a signed release exists.

**Why now.** The two holes compound: an unverified auto-update delivers code into an
arbitrary-root sudoers primitive. Signing the release and locking the sudoers rule together
reduce "forge one server response" from fleet-wide root to a safe no-op.

### 2.1 Settled decisions (inputs, not open for redesign)

These were decided before this FRD and are recorded here so the requirements below reference
them rather than re-litigate them.

| # | Decision |
|---|---|
| D-1 | Signing is raw ed25519 (Go stdlib `crypto/ed25519`), no external signing service. The public key is non-secret and compiled into the agent as a source constant, optionally overridable at build time via `-ldflags -X` for rotation. The private key is held only as a GitHub Actions secret. |
| D-2 | What is signed is the release's `checksums.txt` → a detached `checksums.txt.sig` (ed25519 over the raw bytes of `checksums.txt`). One signature covers all per-platform binaries. The agent verifies `checksums.txt` against its embedded public key, then verifies the downloaded binary's SHA-256 against the entry in the now-trusted `checksums.txt`. Both links — publisher→checksums via ed25519, checksums→binary via SHA-256 — are rooted in a compiled-in public key the server does not control. |
| D-3 | Fail-closed everywhere. No signature, no checksum, or any fetch/parse/verify failure means the update is refused and the current binary keeps running; an agent update-failed event plus an error log are emitted. |
| D-4 | Least-privilege swap: a root-owned helper script at a fixed path (e.g. `/usr/local/lib/riot/riot-agent-apply-update`), installed `0755 root:root` by `install.sh`, performing exactly: stop `riot-agent`, copy staging→target, chmod, remove staging, start `riot-agent`. Only fixed sudoers rules (the direct helper invocation and, if retained, a `systemd-run --unit=riot-agent-update <helper>` form with the helper path fixed) — NO `sh -c`, NO wildcard. The staged-file `ExecStartPre` restart-time apply in the service unit is retained as a non-sudo fallback. |
| D-5 | Dev builds (`version == "dev"`) never auto-update (unchanged). |
| D-6 | Scope is limited to agent update and update-privilege. This story must not touch branch `agent-heal-dpkg` / PR #1, and must not weaken the existing package-manager, reboot, or smartctl sudoers rules. |

### 2.2 Two implementation realities carried from intake

1. **Three install strategies exist, not one.** `performUpdate` tries `systemd-run`, then a
   direct `sh -c` `mv`/`cp`, then a staged-file apply on next restart. Strategies 1 and 2 both
   currently rely on `sh -c`. The tightened privilege model (D-4) must cover whichever
   strategies are retained, and the staged-file `ExecStartPre` fallback (strategy 3) must keep
   working without any sudo rule.
2. **`doctor` probes privileges with `sudo -n -l <cmd>` / `sudo -n <cmd>`.** The existing
   `checkHoldEnforcement` and smartctl checks in `internal/agent/doctor.go` show the house
   pattern: probe the exact sudoers-authorized invocation and tell the operator to re-run the
   installer when it is missing. The update-privilege probe must follow the same pattern
   against the *new* fixed rule, not the old wildcard.

---

## 3. Actors

| Actor | Description | Permissions |
|---|---|---|
| Release pipeline (GitHub Actions) | Builds agent binaries, generates `checksums.txt`, and signs it with the ed25519 private key held as a repository secret. | Reads the signing private key (CI secret only); publishes `checksums.txt` and `checksums.txt.sig` as release assets. Never exposes the private key in logs or artifacts. |
| rIOt server | Polls GitHub Releases and tells agents what the latest version is and where to download assets. | Advertises asset URLs including `checksums.txt` and `checksums.txt.sig`. Is explicitly **not trusted** to assert binary authenticity. |
| rIOt agent | The unprivileged `riot` process on each device that auto-updates and installs the new binary as root via the fixed helper. | Downloads assets; verifies the signature against its compiled-in public key; invokes only the fixed, argument-locked update helper through sudo. Holds no arbitrary-root capability. |
| Update helper script | Root-owned script at a fixed path installed by `install.sh`. | Runs as root to stop the service, swap the binary, and restart the service — nothing else. |
| Operator | Owner of the self-hosted deployment who runs `install.sh` and may run `riot-agent doctor`. | Installs/updates the sudoers drop-in and helper; can disable auto-update via config; verifies privilege state via `doctor`. |
| Attacker (threat model, not a user) | A forged/compromised server or network MITM on the update fetch. | Must be unable to cause any agent to install or execute attacker-chosen code. This is the capability this story removes. |

---

## 4. Functional Requirements

### 4.1 Release signing (pipeline)

- **FR-001:** The release workflow must generate `checksums.txt` exactly as today (a
  SHA-256-per-asset manifest) and must additionally produce a detached signature file named
  `checksums.txt.sig` that is a raw ed25519 signature over the exact bytes of `checksums.txt`.
- **FR-002:** The release workflow must publish both `checksums.txt` and `checksums.txt.sig`
  as assets of the GitHub Release alongside the agent binaries.
- **FR-003:** The signing private key must be read only from a GitHub Actions secret and must
  not appear in build logs, workflow output, the release notes, or any published asset.
- **FR-004:** The ed25519 public key embedded in the agent must correspond to the private key
  used by the release workflow, such that a `checksums.txt.sig` produced by the workflow
  verifies against the agent's compiled-in public key.

### 4.2 Embedded trust anchor (agent)

- **FR-005:** The agent binary must carry the ed25519 **public** key as a compiled-in value
  (a source constant), overridable at build time via `-ldflags -X` for key rotation, and must
  never read the verification public key from the server, the config file, the network, or any
  path the `riot` user can write.
- **FR-006:** The agent must not treat the server-supplied `checksum_url` (or any
  server-supplied value) as a trust anchor; server-supplied URLs may be used only to *locate*
  assets, never to decide authenticity.

### 4.3 Fail-closed verification (agent)

- **FR-007:** Before installing any downloaded binary, the agent must (a) download
  `checksums.txt` and `checksums.txt.sig`, (b) verify the ed25519 signature of
  `checksums.txt` against its embedded public key, and (c) verify that the SHA-256 of the
  downloaded binary equals the entry for this platform in the verified `checksums.txt`.
- **FR-008:** The agent must refuse to install and must leave the current binary running
  (fail closed) on **every** of the following conditions: `checksums.txt.sig` is absent or
  returns a non-200 status; `checksums.txt` is absent or returns a non-200 status; the
  signature does not verify against the embedded public key; `checksums.txt` is unparseable or
  contains no entry for this platform; the downloaded binary's SHA-256 does not match the
  verified entry; or any network, I/O, or timeout error occurs while fetching the signature or
  checksum file.
- **FR-009:** On any refusal under FR-008 the agent must emit an agent update-failed event
  (`EventAgentUpdateFailed`) and an error-level log entry whose message identifies the failure
  class (for example missing signature, signature invalid, checksum mismatch), and must not
  emit an update-completed event.
- **FR-010:** On a refusal under FR-008 the agent must remove its staging file and must not
  invoke the update helper or any `sudo` command for that update attempt.
- **FR-011:** When FR-007 succeeds in full (valid signature over `checksums.txt` and a
  matching binary hash), the agent must proceed to install the new binary and, on a successful
  swap, emit the update-completed event (`EventAgentUpdateCompleted`) as today.
- **FR-012:** The legacy advisory checksum path at `updater.go` ~lines 154–163 must be removed
  or converted so that a checksum fetch failure, a missing checksum, or an unparseable
  checksum is a refusal (fail closed), not a warning-then-install, as interim defense-in-depth
  regardless of signing.
- **FR-013:** The agent must verify the signature and hash of the staged binary *before* it is
  moved into the live binary path; a binary that fails verification must never be copied over
  the running executable by any strategy.

### 4.4 Least-privilege install helper (installer + agent)

- **FR-014:** `install.sh` must install a root-owned helper script at a fixed absolute path
  (per D-4), with ownership `root:root` and mode `0755`, whose behaviour is exactly: stop
  `riot-agent`, copy the staged binary to the target path, chmod the target to `0755`, remove
  the staged file, start `riot-agent` — and nothing else.
- **FR-015:** `install.sh` must remove from the sudoers drop-in the rule
  `${SYSTEMD_RUN_PATH} --unit=riot-agent-update sh -c *` and the rule `${SH_PATH} -c mv -f ...`,
  and must not write any sudoers rule for the `riot` user that contains `sh -c`, a shell
  invocation, or a `*` wildcard in the command or its arguments as a means of applying an
  update.
- **FR-016:** The sudoers drop-in must authorize the update exclusively through fixed,
  fully-argument-specified rules: the direct invocation of the helper at its fixed path and,
  if the `systemd-run` strategy is retained, `${SYSTEMD_RUN_PATH} --unit=riot-agent-update
  <fixed-helper-path>` with the helper path fixed and no trailing wildcard. The existing
  `systemctl reset-failed riot-agent-update` rule may be retained unchanged.
- **FR-017:** After `install.sh` completes on Linux, `sudo -l` for the `riot` user must list
  no rule that permits executing an arbitrary command, an arbitrary shell, or `sh -c` as root
  for update purposes; every listed command must be a fixed binary with fixed arguments.
- **FR-018:** The agent's install strategies in `performUpdate` must be reconciled with the
  tightened rule: whichever strategies are retained must invoke only the fixed helper (directly
  or via the fixed `systemd-run` form), and must not construct or execute an `sh -c` script as
  root.
- **FR-019:** The staged-file restart-time apply (the service unit `ExecStartPre` fallback,
  strategy 3) must be retained and must continue to apply a previously verified staged binary
  on the next service restart without requiring any sudo rule.
- **FR-020:** `install.sh` must not modify, remove, or weaken the existing package-manager
  (`apt-get`, `apt-mark`, `dnf`, `install`, `rm`), `systemctl reboot`, or `smartctl` sudoers
  rules while making these changes (D-6).

### 4.5 Privilege diagnostics (doctor)

- **FR-021:** `riot-agent doctor` must, on Linux, probe for the presence of the new
  update-apply sudoers rule using the same `sudo -n`-style non-interactive probe pattern used
  by the existing privilege checks, and must report the privilege as present or missing.
- **FR-022:** When the update-apply privilege is missing, `doctor` must emit a failure line and
  instruct the operator to re-run the install script to rewrite `/etc/sudoers.d/riot-agent`,
  consistent with the existing hold-enforcement and smartctl guidance.
- **FR-023:** The `doctor` update-privilege probe must target the new fixed helper rule and
  must not report success on the basis of the removed `sh -c` wildcard rule.

### 4.6 Default-on policy under mandatory verification

- **FR-024:** Auto-update must remain enabled by default (`Agent.AutoUpdate == nil` continues
  to mean enabled); this story must not change that default.
- **FR-025:** Dev builds (`version == "dev"`) must never auto-update (unchanged, D-5).
- **FR-026:** When an available release cannot be verified under FR-007/FR-008 (for example a
  release that predates signing and therefore has no `checksums.txt.sig`), the agent must
  perform a safe no-op — it must not install, must keep the current binary running, and must
  emit the update-failed event and an error log once per attempt (FR-009) — rather than
  erroring destructively or disabling itself.
- **FR-027:** An operator who sets `Agent.AutoUpdate` to false must continue to suppress
  auto-update exactly as today; the verification changes must not alter the meaning of that
  config value.

---

## 5. Non-Functional Requirements

- **NFR-001 [Security]:** The verification public key must be a compile-time trust anchor the
  server cannot influence; the only supported way to change it is rebuilding the agent (source
  constant or `-ldflags -X` override). No runtime configuration, environment variable, or
  server response may substitute an alternative verification key.
- **NFR-002 [Security]:** Signature verification must use the Go standard library
  `crypto/ed25519` over the raw bytes of `checksums.txt`; no custom or hand-rolled signature
  primitive may be introduced.
- **NFR-003 [Security]:** After installation of the sudoers drop-in, the `riot` user must have
  no sudo-authorized path to execute an arbitrary command as root; this is a hard invariant
  verifiable by inspecting `sudo -l` (FR-017).
- **NFR-004 [Security]:** The update helper script must be root-owned and not writable by the
  `riot` user (mode `0755`, owner `root:root`), so the fixed sudoers rule cannot be subverted
  by rewriting the helper it invokes.
- **NFR-005 [Availability]:** A refused update (any FR-008 condition) must leave the agent
  fully operational on its current version — telemetry, heartbeat, and all collectors must
  continue unaffected; a verification failure must never crash, wedge, or disable the agent.
- **NFR-006 [Audit]:** Every update attempt must produce structured log output distinguishing
  at least: no update available, update verified and applied, and update refused (with the
  refusal class). Refusals must be logged at error level and surfaced as an update-failed
  event.
- **NFR-007 [Performance]:** Fetching and verifying `checksums.txt` and `checksums.txt.sig`
  must each be bounded by a request timeout no longer than the existing checksum fetch timeout
  (30 s), after which the attempt fails closed per FR-008.
- **NFR-008 [Compatibility]:** The signing and verification changes must not alter the agent's
  behaviour when auto-update is disabled, when no update is available, or for dev builds, and
  must not change the server's `UpdateInfo` contract beyond, at most, advertising the
  `checksums.txt.sig` asset location.
- **NFR-009 [Residual risk — signed downgrade/rollback]:** This story authenticates the
  *publisher* and the *binary-to-checksum binding*, but it does not establish version
  monotonicity. A signed, authentic older release could in principle be served to an agent (a
  rollback/downgrade attack) and would pass verification because it is genuinely signed.
  Mitigating rollback (for example refusing to install a version lower than the running one,
  or anti-rollback version pinning) is explicitly out of scope for this story and recorded as a
  known residual risk (see §9).

---

## 6. Business Rules

- **BR-001:** The server is not a trust anchor for agent code. Authenticity of an update is
  established solely by the agent's compiled-in public key verifying the release signature.
- **BR-002:** No update may be installed unless it is both signed (publisher authenticated via
  ed25519) and hash-matched (binary bound to the verified `checksums.txt`). Any gap in that
  chain is a refusal.
- **BR-003:** The `riot` user must never hold a general-purpose root capability. Root actions
  available to the agent are limited to the fixed, enumerated update/fleet operations; update
  specifically is limited to the fixed helper.
- **BR-004:** Auto-update default-on is permitted only because verification is mandatory and
  fail-closed. The two are a single policy: the default must not be loosened without the
  verification guarantee, and the verification guarantee must not be weakened while the default
  is on.
- **BR-005:** Releases produced before signing exists are not installable by the hardened
  agent. Operators must cut a signed release for auto-update to resume; this is the intended,
  accepted behaviour change.
- **BR-006:** This story must not alter any sudoers rule or branch outside the update/
  update-privilege scope (D-6).

---

## 7. Data Requirements

### 7.1 Entities / artifacts

**Release assets (pipeline output).**

| Artifact | New? | Notes |
|---|---|---|
| `riot-agent-<os>-<arch>` binaries | no | Unchanged set and naming. |
| `checksums.txt` | no | SHA-256-per-asset manifest, unchanged format (`<hash>  <filename>`). |
| `checksums.txt.sig` | **yes** | Detached raw ed25519 signature over the exact bytes of `checksums.txt`. |

**Embedded agent constant.**

| Value | New? | Notes |
|---|---|---|
| ed25519 public key | **yes** | Compiled-in source constant; `-ldflags -X` overridable for rotation; never read from server/config/disk. |

**Install-time artifacts.**

| Artifact | New? | Notes |
|---|---|---|
| Update helper script | **yes** | Fixed absolute path, `root:root`, `0755`; does only stop→copy→chmod→rm→start. |
| `/etc/sudoers.d/riot-agent` update rules | changed | Wildcard `sh -c` rules removed; fixed helper rule(s) added. Other rules untouched. |

**Existing entities touched:** `UpdateInfo` (`internal/server/updates/checker.go`) may gain,
at most, the advertised location of `checksums.txt.sig`; no other server data contract
changes. No database schema change.

### 7.2 Validation rules

| Rule | Constraint |
|---|---|
| V-001 | `checksums.txt.sig` must verify as a valid ed25519 signature over the received bytes of `checksums.txt` under the embedded public key; failure ⇒ refuse (FR-008). |
| V-002 | The verified `checksums.txt` must contain exactly one matching entry for the running platform suffix; zero or unparseable ⇒ refuse (FR-008). |
| V-003 | The SHA-256 of the staged binary must equal the verified entry byte-for-byte (hex, case-insensitive) before any swap; mismatch ⇒ refuse (FR-008, FR-013). |
| V-004 | The signature/checksum fetches must each return HTTP 200; any non-200 or transport error ⇒ refuse (FR-008). |
| V-005 | The helper path referenced by the sudoers rule and invoked by the agent must be a single fixed absolute path with no wildcard component and must match byte-for-byte between `install.sh`, the agent invocation, and the `doctor` probe. |

### 7.3 State transitions

**Update attempt (per cycle, `version != "dev"`, auto-update enabled)**

| From | Event | To | Observable outcome |
|---|---|---|---|
| Idle | Server reports no update available | Idle | Debug log "up to date"; no event |
| Idle | Update available; binary downloaded; signature + hash verify | Applying | Update helper invoked via fixed rule; `EventAgentUpdateCompleted`; agent restarts |
| Idle | Update available; signature/checksum fetch non-200 or error | Refused | Staging removed; `EventAgentUpdateFailed`; error log; current binary keeps running |
| Idle | Update available; signature invalid over `checksums.txt` | Refused | As above, failure class "signature invalid" |
| Idle | Update available; `checksums.txt` unparseable / no entry | Refused | As above, failure class "checksum unavailable" |
| Idle | Update available; binary hash ≠ verified entry | Refused | As above, failure class "checksum mismatch"; binary never swapped |
| Idle | Update available but release has no `checksums.txt.sig` (pre-signing release) | Refused (safe no-op) | `EventAgentUpdateFailed`; error log; agent stays on current version (FR-026) |

No transition reaches **Applying** without passing both V-001 and V-003.

**Install privilege (observed via `doctor` / `sudo -l`)**

| State | Meaning |
|---|---|
| Present | Fixed helper rule is authorized; no arbitrary-exec rule present. `doctor` reports present. |
| Missing | Fixed helper rule absent (installer not re-run). `doctor` reports missing and advises re-running `install.sh`. |

---

## 8. Acceptance Criteria

**AC-001 — Tampered binary is refused** *(FR-007, FR-008, FR-013)*
- **Given** auto-update is enabled, a non-dev agent, and a valid signed `checksums.txt`/
  `checksums.txt.sig` for a release
- **When** the downloaded binary's bytes are altered so its SHA-256 no longer matches the
  verified `checksums.txt` entry
- **Then** the agent does not swap the binary and keeps running the current version
- **And** it emits `EventAgentUpdateFailed` and an error log identifying a checksum mismatch
- **And** no `sudo` command and no update helper is invoked for that attempt.

**AC-002 — Unsigned release is refused** *(FR-007, FR-008, FR-026)*
- **Given** a release that publishes `checksums.txt` but no `checksums.txt.sig`
- **When** the agent attempts to update to it
- **Then** the agent refuses, keeps the current binary running, and emits an update-failed
  event plus an error log
- **And** the agent remains fully operational on its current version.

**AC-003 — Missing or non-200 signature fetch is refused** *(FR-008, V-004)*
- **Given** `checksums.txt.sig` returns a 404 (or any non-200, or a transport error)
- **When** the agent attempts the update
- **Then** the agent fails closed, removes its staging file, and emits an update-failed event
  and error log.

**AC-004 — Missing or non-200 checksum fetch is refused** *(FR-008, FR-012, V-004)*
- **Given** `checksums.txt` returns a non-200 status or a transport/timeout error
- **When** the agent attempts the update
- **Then** the agent fails closed and does not install, even though a `checksums.txt.sig` was
  present
- **And** no warning-then-install path remains (the legacy advisory path is gone).

**AC-005 — Valid signature and matching hash installs** *(FR-007, FR-011)*
- **Given** a release whose `checksums.txt.sig` verifies against the agent's embedded public
  key and whose binary SHA-256 matches the verified `checksums.txt` entry
- **When** the agent runs an update cycle with auto-update enabled on a non-dev build
- **Then** the agent installs the new binary via the fixed helper, emits
  `EventAgentUpdateCompleted`, and restarts onto the new version.

**AC-006 — Valid signature over the wrong/altered checksums is refused** *(FR-007, FR-008, V-001)*
- **Given** a `checksums.txt.sig` that is a valid ed25519 signature but whose `checksums.txt`
  bytes have been altered after signing (so the signature no longer verifies over the received
  bytes), or a signature produced by a key other than the embedded one
- **When** the agent attempts the update
- **Then** the signature verification fails and the agent refuses to install, emitting an
  update-failed event and error log.

**AC-007 — No embedded-key substitution** *(FR-005, FR-006, NFR-001)*
- **Given** a server that advertises an alternate `checksum_url` or attempts to supply its own
  verification key
- **When** the agent verifies a release
- **Then** verification uses only the compiled-in public key and no server-supplied value can
  change which key authenticates the release.

**AC-008 — `sudo -l` contains no arbitrary-exec rule** *(FR-015, FR-017, NFR-003)*
- **Given** a Linux host where `install.sh` has completed and installed the sudoers drop-in
- **When** `sudo -l` is inspected for the `riot` user
- **Then** no rule permits running `sh -c`, an arbitrary shell, or any wildcard command as root
- **And** every update-related rule names the fixed helper path with fixed arguments.

**AC-009 — Fixed helper performs only the swap** *(FR-014, NFR-004)*
- **Given** the helper script installed by `install.sh`
- **When** its path, ownership, and mode are inspected
- **Then** it is owned `root:root` with mode `0755` at the fixed absolute path
- **And** its actions are limited to stop `riot-agent`, copy staging→target, chmod target,
  remove staging, start `riot-agent`.

**AC-010 — Agent invokes only the fixed helper** *(FR-018)*
- **Given** a verified update ready to apply
- **When** the agent applies it
- **Then** the root invocation is the fixed helper (directly or via the fixed
  `systemd-run --unit=riot-agent-update <helper>` form) and no `sudo sh -c <script>` is
  executed.

**AC-011 — Staged-file restart fallback still works without sudo** *(FR-019)*
- **Given** a verified staged binary and a host where the direct/`systemd-run` apply did not
  run
- **When** the `riot-agent` service is restarted
- **Then** the service unit's `ExecStartPre` applies the staged binary with no sudoers rule
  required, and the agent comes up on the new version.

**AC-012 — doctor reports the update privilege present** *(FR-021, FR-023)*
- **Given** a host where `install.sh` installed the new fixed helper rule
- **When** `riot-agent doctor` runs on Linux
- **Then** it reports the update-apply sudo privilege as present, probing the fixed helper rule
  (not the removed wildcard).

**AC-013 — doctor reports the update privilege missing** *(FR-022)*
- **Given** a host whose sudoers drop-in lacks the new fixed helper rule
- **When** `riot-agent doctor` runs on Linux
- **Then** it reports the update-apply privilege as missing with a failure line
- **And** it advises re-running the install script to rewrite `/etc/sudoers.d/riot-agent`.

**AC-014 — Existing privileged rules are untouched** *(FR-020, D-6)*
- **Given** a host with package-manager, `systemctl reboot`, and `smartctl` sudoers rules from
  a prior install
- **When** `install.sh` is re-run with this story's changes
- **Then** those rules remain present and unchanged, and only the update rules are rewritten.

**AC-015 — Dev builds never auto-update** *(FR-025, D-5)*
- **Given** an agent reporting `version == "dev"`
- **When** an update cycle runs
- **Then** no download, verification, or install is attempted, regardless of signing.

**AC-016 — Default-on preserved; disable still works** *(FR-024, FR-027)*
- **Given** a config where `Agent.AutoUpdate` is nil
- **When** the agent evaluates whether to auto-update
- **Then** auto-update is treated as enabled
- **And** when `Agent.AutoUpdate` is explicitly false, auto-update is suppressed exactly as
  before.

**AC-017 — Release workflow signs and publishes the signature** *(FR-001, FR-002, FR-003)*
- **Given** a tagged release build in GitHub Actions
- **When** the release job completes
- **Then** both `checksums.txt` and `checksums.txt.sig` are attached to the GitHub Release
- **And** `checksums.txt.sig` verifies as an ed25519 signature over `checksums.txt` under the
  key whose public half the agent embeds
- **And** the signing private key appears in no log, output, or published asset.

**AC-018 — Build and tests are green** *(cross-cutting)*
- **Given** the implemented changes
- **When** `go build ./...` and `go test ./...` are run
- **Then** both succeed with no failures, including updated `updater.go`, `checker.go`,
  `doctor.go`, and their tests.

**AC-019 — Refusal leaves the agent fully operational** *(FR-009, FR-010, NFR-005)*
- **Given** any FR-008 refusal condition during an update cycle
- **When** the cycle completes
- **Then** the staging file is removed, exactly one update-failed event and error log are
  emitted, and telemetry, heartbeat, and collectors continue running unaffected on the current
  version.

**AC-020 — Binary never swapped before verification** *(FR-013)*
- **Given** a staged binary that fails signature or hash verification
- **When** the update attempt is processed
- **Then** the live binary at the executable path is never overwritten by any install strategy.

---

## 9. Out of Scope

1. **Anti-rollback / version monotonicity.** A signed, authentic *older* release would pass
   verification; refusing downgrades or pinning minimum versions is not part of this story
   (recorded as residual risk, NFR-009).
2. Changing the auto-update cadence, the 6-hour interval, or the startup delay.
3. Migrating to an external signing service, HSM, Sigstore/cosign, TUF, or any key-management
   system beyond a single ed25519 keypair with the private half in a CI secret.
4. Key-rotation automation, key revocation lists, or multi-key trust (the `-ldflags -X`
   override is the only rotation mechanism in scope).
5. Server-side verification or re-signing of releases; the server remains untrusted for
   authenticity by design.
6. Any change to the `agent-heal-dpkg` branch / PR #1 (D-6).
7. Weakening, consolidating, or re-deriving the package-manager, reboot, or smartctl sudoers
   rules (D-6).
8. Signing the server Docker image or the `install.sh` script itself.
9. TLS/transport hardening of the download path (verification is end-to-end via the signature,
   independent of transport).
10. A dashboard/UI surface for update verification status or signed-release enforcement.
11. Database schema changes.

---

## 10. Assumptions

- **A-1:** The GitHub release assets `checksums.txt` and `checksums.txt.sig` are reachable by
  the agent via the same asset-URL mechanism the server already advertises; locating
  `checksums.txt.sig` may reuse the existing `checksum_url` convention (by name), while trust
  still derives only from the embedded key.
- **A-2:** The ed25519 keypair is generated out of band by the maintainer; the private key is
  placed in the repository's Actions secrets before the first signed release, and the matching
  public key is committed as the agent's embedded constant.
- **A-3:** The fixed helper path (illustrated as `/usr/local/lib/riot/riot-agent-apply-update`
  in D-4) is the architect's to finalize, but once chosen it must match byte-for-byte across
  `install.sh`, the agent invocation, the sudoers rule, and the `doctor` probe (V-005).
- **A-4:** Existing deployments must re-run `install.sh` to obtain the new helper and tightened
  sudoers rule; until they do, `doctor` reports the privilege missing (AC-013) and, if the old
  wildcard rule is gone, the agent falls back to the staged-file restart apply (FR-019).
- **A-5:** The accepted behaviour change (agents on auto-update will not update until a signed
  release exists, BR-005) is communicated to operators in `README.md`/`CHANGELOG`; the
  technical writer owns the wording.
- **A-6:** The per-platform matching in `checksums.txt` keeps the existing suffix convention
  (`<os>-<arch>` / `<os>-armv<n>`) so the agent can select its line deterministically.

---

## 11. Open Questions

Each carries the answer assumed by this FRD. If the maintainer disagrees, the FRD changes; the
architect should not silently choose differently.

- **OQ-1 — Keep the `systemd-run` strategy at all, or collapse to the direct helper plus the
  restart fallback?** *Assumed:* the architect may retain `systemd-run` only in the fixed,
  wildcard-free form of FR-016; collapsing to the direct helper is acceptable and preferable if
  it simplifies the sudoers surface.
- **OQ-2 — Should the signature be over `checksums.txt` (per D-2) or over each binary
  individually?** *Assumed:* over `checksums.txt` only (D-2) — one signature covers all
  platforms; per-binary signing is out of scope.
- **OQ-3 — Should a refused update be rate-limited in its event/log emission to once per
  release rather than once per 6-hour cycle?** *Assumed:* once per attempt is acceptable
  (FR-009); de-duplication across cycles is a nice-to-have, not required.
- **OQ-4 — Should the embedded public key be a raw 32-byte value or a PEM/base64 constant?**
  *Assumed:* an architect's representation choice; any encoding is fine provided it is a
  compile-time constant overridable via `-ldflags -X` (FR-005).
- **OQ-5 — Should anti-rollback be at least partially addressed now (refuse installing a lower
  version than running)?** *Assumed:* No — explicitly out of scope and tracked as residual
  risk (NFR-009, §9 item 1); revisit as a follow-up story if the threat model warrants.

---

## 12. Dependencies

| Dependency | Nature | Status |
|---|---|---|
| ed25519 keypair + GitHub Actions secret | Required to produce signed releases (FR-001–FR-004). The public key must be embedded before the hardened agent ships. | **Blocking for signed releases.** Maintainer provisions the key. |
| `.github/workflows/release.yml` | Must be extended to sign `checksums.txt` and publish `checksums.txt.sig`. | Present; change required. |
| `internal/agent/updater.go` (`performUpdate`, `fetchExpectedChecksum`) | Carries the fail-closed verification and the helper-only install (FR-007–FR-013, FR-018). | Present; change required. |
| `scripts/install.sh` sudoers drop-in (~lines 437–482) + new helper script | Replaces the `sh -c *` wildcard with the fixed helper and rule (FR-014–FR-020). | Present; change required. |
| `internal/agent/doctor.go` privilege probes | Must gain an update-privilege probe against the new fixed rule (FR-021–FR-023). | Present; change required. |
| `internal/server/updates/checker.go` (`AgentUpdateInfo`/`UpdateInfo`) | May advertise the `checksums.txt.sig` asset location; must not become a trust anchor. | Present; minor change possible. |
| `crypto/ed25519` (Go stdlib) | Signature verification primitive (D-1, NFR-002). | Stdlib; no new module. |
| Operator documentation (`README.md`, `CHANGELOG`) | Records the accepted behaviour change (BR-005, A-5). | Technical writer, end of pipeline. |
| Release process (`memory/release-process.md`) | A code change here requires a tagged release; the *first* signed release is what re-enables fleet auto-update. | Present. |
