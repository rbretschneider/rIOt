# Security Review Report

**Story ID:** AGENT-HARDEN-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-15
**Verdict:** REVISE (ARCHITECT)

Blocking finding: **SEC-001 (HIGH)**. The ed25519-over-`checksums.txt` scheme itself is
correct and, against the *server / MITM* threat, genuinely closes the hole: the trust anchor is
compiled in, the server cannot substitute a key, the verification order is sound, and every
enumerated failure branch fails closed. But the story's *other* stated goal — "a compromise of
the `riot` user is no longer a path to root" (NFR-003, BR-003) — is not met. The privileged
apply step (AD-005 helper + AD-006) performs **no verification of its own**: it copies a
`riot`-writable staging file into the system binary path as root, and the `riot` user holds a
standing sudo rule to invoke it directly. All of the ed25519/SHA-256 verification lives in the
unprivileged, bypassable agent, over a mutable file. That re-introduces a root-write primitive
for the exact actor this story exists to contain.

Documents and code read in full: `docs/requirements/AGENT-HARDEN-001-frd.md`,
`docs/architecture/AGENT-HARDEN-001-add.md`, `internal/agent/updater.go`,
`internal/agent/http_client.go` (update path), `internal/server/updates/checker.go`
(`AgentUpdateInfo`), `scripts/install.sh` (sudoers block 424-493, systemd unit 495-520),
`.github/workflows/release.yml`, and the OIDC-001 review for format. `internal/agent/doctor.go`
patterns cross-referenced from the ADD (AD-007) and FRD.

---

## Threat Model Summary

### Assets

| Asset | Where it lives after this story | Why it matters |
|---|---|---|
| The code that runs on every enrolled agent | `/usr/local/bin/riot-agent` (root-owned `0755`), executed as `User=riot` | Auto-update replaces this fleet-wide; the whole story exists to control what can land here |
| ed25519 **private** signing key | GitHub Actions secret `AGENT_SIGNING_PRIVATE_KEY` only | Forges a release the entire fleet will install as a verified update |
| ed25519 **public** key (trust anchor) | Compiled-in `var releasePublicKeyBase64` (AD-002), `-ldflags -X`-overridable | Sole authenticity root; must be unspoofable and fail-closed when absent/garbage |
| The `riot` → root sudo boundary | `/etc/sudoers.d/riot-agent` + the root-owned apply helper | The story's second promise: a `riot` compromise must NOT reach root |
| The staging artifact | `/var/lib/riot/riot-agent.update` — **riot-writable**, consumed by a **root** helper | The privileged/unprivileged trust seam; the subject of SEC-001 |

### Threat Actors

1. **Forged / compromised rIOt server, or a network MITM** on the download / checksum /
   signature fetches. Controls `assets`, `checksum_url`, `signature_url`, `update_available`,
   and `latest_version`, and the bytes returned for each. Wants a chosen binary executed on the
   fleet. **This is the threat the ed25519 design targets.**
2. **Local `riot` user, or a process that has compromised the agent to `riot`.** Holds the
   update sudoers rule and write access to the staging path. Wants arbitrary root. **This is the
   threat the sudoers/helper rework targets — and the one SEC-001 shows is not fully closed.**
3. **Compromised GitHub release channel / CI.** Wants to leak the signing key or publish an
   unsigned/mis-signed release.
4. **Attacker replaying a genuine, older, validly-signed release** (downgrade). Covered by
   SEC-004.

### Attack Surface Introduced

- A new root-owned, root-executed helper script at a fixed path, whose only variable input is
  the content of a `riot`-writable file (AD-005).
- A standing `riot ALL=(root) NOPASSWD` rule to launch that helper via `systemd-run` at any
  time, independent of any update cycle (AD-005 §4).
- Two new agent-initiated HTTP fetches of server-advertised URLs whose bodies are read into
  memory (`checksums.txt`, `checksums.txt.sig`) (AD-004).
- A new build-time tool `cmd/riot-signer` (`genkey`/`sign`) and a new CI step that handles the
  signing secret (AD-009).
- A new compiled-in trust anchor whose value must be kept in correspondence with the CI secret
  (AD-002).
- One additive `signature_url` field on the update-info contract (AD-003).

---

## Findings

### CRITICAL

None. (SEC-001 was weighed for CRITICAL — see its severity note; it is rated HIGH because the
swapped binary executes as `User=riot`, not root, so root code execution is *conditional*
rather than automatic.)

---

### HIGH

#### SEC-001: The privileged apply helper verifies nothing; it installs a `riot`-controlled file, so a `riot` compromise still reaches a root-write primitive

**Severity:** HIGH
**Domain:** Authorization / privilege boundary / TOCTOU
**Location:** ADD AD-005 (helper contents, sudoers rule) and AD-006 (apply strategies); FRD D-4;
`scripts/install.sh` staging path `${RIOT_DATA_DIR}/riot-agent.update` (riot-writable) vs. the
root helper that `cp`s it; `internal/agent/updater.go:144-151` (hash is computed over the
download **stream**, not over the file the helper later reads)

**Description.**
The design splits verification and installation across a trust boundary and puts the
verification on the wrong side of it.

- **Verification runs as `riot`.** AD-004 performs the ed25519 + SHA-256 checks inside
  `performUpdate`, in the agent process, which runs as `User=riot` (install.sh:510). The hash
  is computed over the bytes *as they stream off the network* (`io.MultiWriter(stagingFile,
  hash)`), not over a re-read of the staged file.
- **Installation runs as root and re-checks nothing.** The AD-005 helper does exactly
  `sleep 3 → systemctl stop → cp ${RIOT_DATA_DIR}/riot-agent.update ${RIOT_BIN} → chmod 755 →
  rm → start`. It copies whatever is at the staging path *at `cp` time*. It does not verify a
  signature, a hash, or anything else.
- **The staging path is `riot`-writable** (it is under `RIOT_DATA_DIR`, owned by `riot` — it
  must be, because the agent downloads to it).
- **`riot` can invoke the helper directly.** AD-005 §4 grants `riot ALL=(root) NOPASSWD:
  systemd-run --unit=riot-agent-update /usr/local/lib/riot/riot-agent-apply-update`. Nothing
  about that rule is tied to an update cycle — `riot` can run it on demand.

So the verification the whole story is built on is performed by the unprivileged, fully
bypassable party, over a file that the same party can rewrite before (or after) the check. The
`sleep 3` in the helper even widens the window, but the window is irrelevant: the attacker does
not need to race anything, because it can stage its payload *first* and then invoke the helper.

**Attack Scenario.**
Actor: a process that has reached `riot` (the exact actor NFR-003/BR-003 promise to contain).
1. Write an attacker-chosen binary to `/var/lib/riot/riot-agent.update`.
2. Run `sudo systemd-run --unit=riot-agent-update
   /usr/local/lib/riot/riot-agent-apply-update` (the standing rule; no update, no server, no
   verification involved).
3. The root helper copies the attacker's bytes over `/usr/local/bin/riot-agent` — a root-owned
   system path — `chmod 755`s it, and restarts the service onto it.

Consequences:
- **Root-write-to-system-binary primitive.** An unprivileged user causes root to place
  arbitrary attacker content at a root-owned path and mark it executable. The swapped binary
  runs as `riot` (so this is not *automatic* root RCE — hence HIGH not CRITICAL), but
  AC-020/FR-013 ("the live binary is never overwritten by unverified bytes") is violated as a
  guarantee, and NFR-003/BR-003 ("`riot` holds no path to arbitrary root action") is false: the
  helper *is* a root action whose payload `riot` controls.
- **Arbitrary root file *read* via a symlinked source.** `riot` owns `/var/lib/riot`, so it can
  make `riot-agent.update` a symlink to e.g. `/etc/shadow` or a root-only private key. The root
  `cp` follows it and writes the secret's contents into world-readable `/usr/local/bin/riot-agent`
  (`0755`), which `riot` then reads. Combined with the write primitive, a `riot` compromise
  reaches root-level confidentiality and, if the operator ever runs the binary under `sudo`
  (e.g. `sudo riot-agent doctor`) or if the unit is ever changed to `User=root`, full root
  code execution.
- **The ed25519 work is bypassed entirely** in this path — the attacker never goes through
  `performUpdate`.

This is a design-level flaw: it is inherent to AD-005's helper (which verifies nothing) + the
riot-writable staging contract (FRD D-4) + AD-006 delegating trust to the agent. It is not an
implementation bug a senior_dev can paper over.

**Required Resolution (property, not mechanism).**
Amend **AD-005/AD-006 (and FRD D-4's helper definition if needed)** so that *the component that
performs the privileged install establishes the authenticity of the exact bytes it installs,
immediately before and atomically with installing them, and cannot be satisfied by an
unprivileged caller's prior check over a mutable file.* Any of these satisfy it; the choice is
the architect's:
- the root helper re-verifies the staged file against the **embedded public key** (and the
  signed checksum) over the exact bytes it is about to copy, refusing otherwise; or
- the verified expected SHA-256 is handed to the helper through a channel `riot` cannot forge,
  and the helper re-hashes the staged file and refuses on mismatch; or
- the artifact is placed, by a root step, into a location `riot` cannot modify between
  verification and copy, such that verified-bytes ≡ installed-bytes is provable.

Additionally: the helper must refuse a staging **source that is a symlink** (open `O_NOFOLLOW`,
require a regular file), and the resolution must explicitly account for the fact that `riot` can
invoke the helper directly, so the helper may not assume the agent verified anything.

**Blocks:** Implementation.

---

### MEDIUM

#### SEC-002: The new `checksums.txt` / `checksums.txt.sig` fetches read server-controlled bodies into memory with no size bound

**Severity:** MEDIUM
**Domain:** Denial of service / resource exhaustion (new attack surface)
**Location:** ADD AD-004 `fetchBytes` ("GETs url with a 30s timeout, requires HTTP 200, and
returns the body"); the `.sig` length check runs *after* the full body is read and base64
decoded

**Description.**
NFR-007 bounds these fetches by *time* (30 s) but not by *size*. `fetchBytes` returns "the body"
— i.e. an unbounded `io.ReadAll` — and the `.sig` is length-checked to 64 bytes only *after* it
has been fully read and base64-decoded. A forged/MITM server (threat actor 1) that advertises
`signature_url`/`checksum_url` can stream a multi-gigabyte body within the 30 s window and
exhaust the agent's memory. The agent is the monitoring/telemetry process on the device; OOM-ing
it is a useful precursor to acting on the device unobserved, and it is reachable on the
*common* "up to date most of the time" fleet because the fetch happens whenever the server
merely *claims* `update_available`.

**Required Resolution.**
Amend **AD-004** so each of the two text fetches reads through an `io.LimitReader` with a tight
cap (the `.sig` is ~88 base64 bytes; `checksums.txt` is a few KB — low-kilobyte caps are
generous), and a body exceeding the cap is a refusal (fail closed, consistent with FR-008). The
binary download already streams to disk so it is size-bounded by the staging write; these two
in-memory reads are the gap.

---

#### SEC-003: Nothing verifies the embedded public key corresponds to the CI signing key, or is even a valid key

**Severity:** MEDIUM
**Domain:** Cryptography / CI key handling / availability
**Location:** ADD AD-002 (`releasePublicKeyBase64` source default; placeholder
`"<base64 of the 32-byte ed25519 public key>"`); AD-009 (CI `sign` step);
`.github/workflows/release.yml` build step (`LDFLAGS` sets only `-X main.version`, so released
binaries embed the **source default** key)

**Description.**
The released binaries get their trust anchor from the committed source default (the release
workflow does not pass a `-X ...releasePublicKeyBase64` override). Two failure modes have no
guard:
1. **Mismatch.** If the committed public key and the `AGENT_SIGNING_PRIVATE_KEY` secret ever
   drift apart (rotation done in the wrong order per AD-002 §4, a fat-fingered secret, a
   regenerated keypair), CI will happily sign with one key and ship binaries embedding the
   other. Every verification then fails and the **entire fleet silently refuses all
   auto-updates** — including the security update meant to fix it. Per SEC-001/#2 this fails
   *closed* (good, not a bypass), but it is a self-inflicted fleet-wide availability outage with
   no signal until devices stop updating.
2. **Placeholder/garbage.** If the source default is left as the placeholder string, same
   outcome.

Nothing in CI catches either before publishing, and FR-004/AC-017 ("the `.sig` verifies against
the key whose public half the agent embeds") is asserted only by the signer's self-test against
its *own* generated key, not against the committed constant.

**Required Resolution.**
Add to **AD-009 / ADD §13** a CI gate that, after signing, verifies the freshly produced
`checksums.txt.sig` against the **public key compiled into (or committed as)
`releasePublicKeyBase64`**, and fails the release on mismatch or on a non-decodable/non-32-byte
constant. This turns the dangerous silent-brick into a loud build failure and enforces FR-004 at
the point it matters (SEC-003).

---

#### SEC-004: Signed-downgrade residual is real and only partially closable, because version is absent from the signed material

**Severity:** MEDIUM
**Domain:** Business logic / replay (residual risk assessment requested)
**Location:** FRD NFR-009, §9 item 1, OQ-5; ADD §10 "signed downgrade/rollback"; the agent
trusts server-supplied `UpdateAvail` / `LatestVersion` (`updater.go:70-77`;
`checker.go:184` computes `isNewer` **server-side**)

**Description.**
A genuinely signed *older* release passes every check in AD-004 (it is authentically signed and
its binary matches its own `checksums.txt`). The agent currently installs whatever the server
says is "available": `UpdateAvail` and `LatestVersion` are both server-supplied, and the
agent-side gate is only "did the server set `UpdateAvail`." A malicious/MITM server (actor 1)
therefore replays an older signed release — e.g. a pre-hardening agent, or any release with a
later-patched agent-side vulnerability — and the fleet rolls back onto it. The ADD records this
as accepted residual (NFR-009), and the maintainer answered OQ-5 "No."

Assessment as requested: the residual is genuine and its blast radius is "downgrade the whole
fleet to any previously-released, still-signed, known-weaker agent." Crucially, **`checksums.txt`
filenames carry no version** (`riot-agent-linux-amd64`, not `...-v2.8.0-...`), so the binary has
**no in-band, signed version attestation**. That means even an agent-side "refuse non-newer"
check is only partially effective: it defeats a server that *honestly* advertises an older
`LatestVersion`, but not one that advertises a high `LatestVersion` while serving an old signed
binary — the agent cannot cryptographically learn the real version of the bytes it is about to
run.

**Required Resolution.**
I am **not** making full anti-rollback a blocker — it is explicitly out of scope and
maintainer-deferred. But record accurately, and consider as a (non-blocking) condition:
1. Cheap defense-in-depth now: have the **agent** (not the server) refuse to "update" to a
   `LatestVersion` that is not strictly newer than its running version, using the existing
   `isNewer` logic locally. State in AD/§10 that this is best-effort and bypassable by a server
   that lies about the version, because version is unsigned.
2. Raise a follow-up to put a **signed** version signal into the release (e.g. version in the
   `checksums.txt` filenames, or a signed manifest line), without which rollback cannot be
   robustly closed. The ADD §10 text should say this, rather than implying the residual is
   merely "someone replays an old release."

---

### LOW / INFORMATIONAL

#### SEC-005: `parseExpectedChecksum` matches the platform by substring, not by exact filename

**Severity:** LOW
**Domain:** Input parsing / content confusion
**Location:** ADD AD-004 `parseExpectedChecksum` (inherits `updater.go:240`
`strings.Contains(parts[1], suffix)` and returns the **first** matching line)

Cross-*release* confusion is not reachable: the ed25519 signature binds the publisher to a
specific `checksums.txt`, and that file's SHA-256 lines bind it to that release's exact binaries,
so a server cannot pair release A's binary with release B's checksums (the hash won't match) —
the only unbound dimension is *which* release, which is SEC-004, not a parsing bug. Within a
file, today's suffixes are unambiguous (`linux-amd64`, `linux-arm64`, `linux-armv7`,
`linux-armv6` — none is a substring of another). The weakness is fragility, not a current
exploit: `strings.Contains` + first-match means a future asset whose name merely *contains* the
suffix (a `.deb`, a `-fips`, a debug build) could shadow the real line. Recommend matching the
exact basename `riot-agent-<suffix>[.exe]` rather than a substring, now that the parse runs over
already-verified bytes. No bypass today.

#### SEC-006: The verified hash is taken over the download stream, not a re-read of the staged file

**Severity:** INFORMATIONAL
**Domain:** TOCTOU (folds into SEC-001)
**Location:** `updater.go:144-151`; ADD AD-004 step 1

Against the server/MITM threat this is fine (stream bytes ≡ staged bytes at write time). It is
recorded only because it is the same mutable file the root helper later re-reads: whichever
resolution SEC-001 adopts, the authoritative verification must be over *the bytes installed*,
which means re-hashing the staged file (safely) at the privileged step, not reusing the
stream-time digest.

---

## Positive Observations

Genuinely well done; these should survive the revision.

1. **The ed25519 scheme correctly roots trust in the compiled-in key and removes the server
   from the trust path (FR-005/FR-006/NFR-001).** `releasePublicKeyBase64` is a `var` so
   `-ldflags -X` can rotate it, `loadReleasePublicKey` decodes-and-length-checks so a tampered/
   empty/placeholder key fails *closed* rather than accepting, and `SignatureURL`/`ChecksumURL`
   are used only to *locate* bytes that are then verified against the embedded key. AC-007 holds.
2. **The verification order is correct and fail-closed-by-construction (AD-004).** Download+hash
   → require *both* URLs present (killing the old `checksumURL != ""` skip-and-install) → fetch
   checksums → fetch sig → ed25519-verify over the raw bytes → parse → compare hash → *only
   then* apply. Every enumerated branch (missing URL, non-200, truncated/malformed base64 sig,
   wrong sig length, `Verify` false, no suffix entry, hash mismatch, bad embedded key) returns
   an error that `checkAndUpdate` turns into a refusal + event + log, with the `defer` removing
   staging. Signing the *raw received bytes* (not a re-serialized form) makes AC-006 real. The
   live binary is untouched through steps 1-7 — correct for the server threat (SEC-001 is a
   separate, local-actor break of the *privileged* side).
3. **The sudoers rewrite removes the arbitrary-exec primitive.** The `sh -c *` and `sh -c
   mv…` rules are gone; the one update rule names a fixed helper with no arguments and no
   wildcard. Because sudo matches argv exactly with no wildcard, **no `systemd-run` property/
   argument injection** (`-p ExecStartPost=…`, extra `--property`) is reachable through the
   rule, and the package-manager/reboot/smartctl/nginx rules are left untouched (FR-020/D-6).
   The `systemd-run` self-stop analysis (AD-005 §3) is correct and the fixed form is the right
   call. The residual risk is SEC-001 — *what the helper does*, not *what the rule permits*.
4. **CI secret handling is sound.** The private key is a repository secret passed only via
   `env:` on the single signing step, never written to disk (the signer reads it from the
   environment), never interpolated into a `run:` string, and the signer prints nothing derived
   from it. Signing runs on the tag-triggered release workflow only. FR-003/AC-017 holds for
   leakage (SEC-003 is about key *correspondence*, not leakage).
5. **`cmd/riot-signer` sharing one audited code path for keygen, signing, and (implicitly) the
   agent's encodings** removes the openssl-ed25519 footguns and keeps the byte encodings
   identical across producer and consumer.

---

## Verdict Rationale

**REVISE (ARCHITECT).**

The cryptographic half of this story is right. Against a forged/compromised server or a network
MITM — the primary threat — the ed25519-over-`checksums.txt` design, with a compiled-in,
fail-closed trust anchor and a strictly-ordered verification pipeline, actually closes the hole:
the server cannot substitute a key, cannot cause skip-and-install, and cannot get a chosen
binary past verification. I would have approved that half.

Implementation is blocked on **SEC-001**, which is a design flaw in the *privilege* half:

| Finding | ADD/FRD decision to change | Why it blocks |
|---|---|---|
| SEC-001 (HIGH) | **AD-005 helper contents, AD-006 apply path, FRD D-4** | Verification is delegated to the unprivileged, bypassable agent over a `riot`-writable file; the root helper verifies nothing and `riot` can invoke it directly. The story's second promise — "a `riot` compromise is no longer a path to root" (NFR-003/BR-003) — is false: `riot` can make root install arbitrary bytes into a system binary and, via a symlinked source, read arbitrary root-owned files. FR-013/AC-020 are defeated as guarantees. |

SEC-002, SEC-003, and SEC-004 should be folded into the same revision — SEC-002 (unbounded body
read) and SEC-003 (no embedded-key/CI-key correspondence check) are small, concrete ADD
amendments; SEC-004 asks the ADD to state the downgrade residual accurately and to consider a
cheap agent-side guard, and to record that robust anti-rollback needs a *signed* version signal
(follow-up). SEC-005 and SEC-006 are hardening/accuracy notes to carry into the fix.

**Routing:** back to `architect` with this report. Do **not** invoke `senior_dev`. On re-review
I expect to reach APPROVED WITH CONDITIONS quickly — SEC-001 does not require redesigning the
feature, only moving the authenticity check to the privileged side of the boundary (or making
the staged artifact immutable to `riot` before install).

### Conditions the QA engineer must verify on re-review

Carry these forward whatever the revised ADD says:

- The privileged apply step verifies the exact bytes it installs against the embedded key/signed
  hash (or the staged artifact is provably unmodifiable by `riot` between verify and copy); a
  `riot` user that stages arbitrary bytes and invokes the helper directly cannot get them
  installed, and a symlinked staging source is refused (SEC-001).
- `checksums.txt` and `checksums.txt.sig` fetches are size-capped and refuse an oversized body
  (SEC-002).
- CI fails the release if the produced `.sig` does not verify against the embedded/committed
  public key, or if the embedded constant is not a valid 32-byte key (SEC-003).
- §10 states the downgrade residual accurately (version is unsigned; any agent-side check is
  best-effort); if the cheap "refuse non-newer target, agent-side" guard is adopted, it rejects
  a server advertising an older `LatestVersion` (SEC-004).
- Platform-line selection matches the exact binary basename, not a substring (SEC-005).
- Unchanged-from-ADD invariants still hold: `sudo -l` shows no `sh -c`/shell/wildcard update
  rule (AC-008); the helper is `root:root 0755` at the fixed path with its five fixed actions
  (AC-009); other sudoers rules untouched (AC-014); `doctor` probes the fixed rule (AC-012/013);
  dev builds never update (AC-015); default-on preserved (AC-016).

---
---

# Re-review (rev 2)

**Date:** 2026-10-15
**Scope:** ADD Revision 2 (`docs/architecture/AGENT-HARDEN-001-add.md`, revision log row 2 —
AD-002/AD-004/AD-005/AD-006/AD-007/AD-009 rewritten, new AD-010) against the findings above. The
rev-1 findings are unchanged and remain the record of what was wrong with rev 1.
**Verdict:** **APPROVE WITH CONDITIONS**

The blocking finding (SEC-001) is resolved at the mechanism level, not papered over, and the way
it is resolved is the right one: the authenticity check moves onto the *privileged* side of the
boundary via a dedicated, root-owned, **compiled** verifier (`cmd/riot-agent-apply`) that embeds
the same trust anchor and re-verifies the exact bytes it installs. Every MEDIUM/LOW is resolved
or accurately recorded. No CRITICAL or HIGH finding remains. The conditions below are
implementation-fidelity and QA-verification items; none blocks `senior_dev` from starting.

Code re-confirmed against the design: `scripts/install.sh:232-233` is indeed today's
`chown riot:riot "$RIOT_BIN"` that AD-005 §5 removes; `install.sh:510` is `User=riot`;
`install.sh:506` is the raw-`cp` `ExecStartPre` that AD-005 §7 replaces; `RIOT_DATA_DIR` (the
staging dir) is `riot`-owned, but `/var/lib` and `/usr/local/{bin,lib}` are root-owned.

## Disposition of the original findings

| Finding | Rev 2 resolution | Status |
|---|---|---|
| **SEC-001** (HIGH) | New compiled verifier `cmd/riot-agent-apply` (AD-005). (a) Verifier is `root:root 0755` in root-owned `/usr/local/lib/riot`, and `RIOT_BIN` is now installed `root:root` in root-owned `/usr/local/bin` (§5) — `riot` can overwrite neither the executed helper nor the install target. (b) Staged inputs opened `O_RDONLY\|O_NOFOLLOW` with an `fstat` regular-file check (§2a-b); `/var/lib` being root-owned means `riot` cannot swap the `riot` dir component, so only the `O_NOFOLLOW`-protected leaf is `riot`-controlled; the target is a compiled-in `const`, written via `O_EXCL\|O_NOFOLLOW` temp + atomic `rename` in a root-owned dir. (c) Bytes read once into memory, verified in memory, and the *same* bytes written (§2a/e/f) — no read-verify-then-write-different-bytes gap, and the authoritative hash is over the installed bytes (resolves SEC-006). (d) A direct `riot` invocation of the standing `systemd-run <verifier>` rule must present a key-valid `checksums.txt`/`.sig`; without the private key, unsigned bytes refuse before any write (§2, §10). (e) Agent runs `User=riot`, never writes `RIOT_BIN`, `0755` keeps it executable — no breakage. The former symlink arbitrary-root-**read** is gone because bytes are written only *after* verification, so even a hardlinked root file is read into memory and refused, never disclosed. | **RESOLVED** |
| **SEC-002** (MED) | AD-004 `fetchBytes(ctx, url, limit)` and the verifier's metadata reads use `io.LimitReader` at a 1 MiB cap with overflow rejection; the verifier bounds the in-memory binary read with `maxBinarySize` (§2a, §9). | **RESOLVED** — see C3 |
| **SEC-003** (MED) | AD-009 adds a `riot-signer verify <checksums> <sig>` CI step that imports the committed `internal/releasekey`, fails on a non-32-byte/placeholder constant via `releasekey.PublicKey()`, and `ed25519.Verify`s the produced signature against it — failing the release on key/secret drift. `go run` with no `-X` means it checks the same key the shipped binaries embed. | **RESOLVED** |
| **SEC-004** (MED) | New AD-010 best-effort agent-side guard (`isNewerVersion`, refuse non-newer `latest_version`); §10 states plainly that version is absent from the signed material, the guard is bypassable by a server that lies about the version, and robust anti-rollback needs a signed version signal — recorded as an explicit follow-up. Exactly the accurate framing requested. | **RESOLVED** |
| **SEC-005** (LOW) | AD-004/AD-005 match the exact asset basename (`parts[1] == "riot-agent-"+suffix`), not a substring, in both the agent pre-flight and the verifier. | **RESOLVED** — see C4 |
| **SEC-006** (INFO) | Made explicit (AD-004 step 1 note, AD-005 §2e, §7 step 7, §12): the stream digest is pre-flight only; the authoritative hash is the verifier's re-hash of the in-memory bytes it writes. | **RESOLVED** |

Two design choices are better than I specified. Putting the trust anchor in a standalone leaf
package `internal/releasekey` shared by the agent, the verifier, and the CI `verify` gate
(AD-002) means one anchor can never drift between producer and consumers. And keeping the
**two-mode** verifier (`apply` via `systemd-run`, `--boot` via `ExecStartPre -+`) means the
restart-fallback path — previously an *unverified* root `cp` (install.sh:506) and a second,
unflagged instance of SEC-001 — is now gated by the same authoritative verifier. The rev-1
`ExecStartPre` hole is closed at the same time rather than left behind.

## New surface introduced by rev 2 — reviewed

- **The verifier is a new root-executed binary.** Its only runtime inputs are the three
  `riot`-staged files (each authenticated) plus the compiled-in key and target; its only CLI flag
  is `--boot`; no argv/env influences the trust anchor or the install path. `riot` cannot modify
  it (root-owned). It cannot be turned on itself: `targetAssetName` is the *agent* asset and
  `targetBin` is the *agent* path, so it cannot be coerced into installing the verifier (or any
  other signed artifact) over the agent, nor into replacing the verifier. No new hole.
- **Verifier is not auto-updated** (AD-005 §6); a verifier bug or key rotation needs an
  `install.sh` re-run. Accepted and documented; a stale verifier meeting a new-key release fails
  closed (visible refusal), not a bypass.
- **Local `riot` can bounce the agent** by repeatedly invoking the `systemd-run` rule
  (stop → failed verify → start). Not a new capability: the agent runs as `riot`, so `riot` can
  already signal/kill its own process and `Restart=always` behaves identically. Not a finding.

## Conditions

Hand these to `senior_dev` and QA. None blocks implementation start; all are
implementation-fidelity or verification items. C1-C2 are the load-bearing security properties
the whole fix rests on — QA must assert them explicitly, not assume them.

**C1 — The verifier must read-once / verify-in-memory / write-the-same-bytes, with no re-read of
the staged file.** The entire SEC-001 fix depends on the bytes hashed in AD-005 §2e being the
exact bytes written in §2f. An implementer who "tidies" this by re-opening the staging path to
copy it after verifying (or by streaming the copy) silently reintroduces the TOCTOU. QA needs a
test proving a post-verification change to the staging file cannot affect the installed bytes
(the verifier holds the content in memory / an open fd, not a path it re-resolves).

**C2 — `O_NOFOLLOW` + `fstat` `S_ISREG` on *every* staged input and on the target temp file, and
the write confined to the hardcoded root-owned target.** QA MANUAL on a re-installed host, as a
`riot` shell: (i) staging arbitrary unsigned bytes + invoking the sudo rule installs nothing;
(ii) a symlinked `riot-agent.update` is refused; (iii) `stat` shows both
`/usr/local/bin/riot-agent` and `/usr/local/lib/riot/riot-agent-apply` are `root:root` and not
`riot`-writable; (iv) code review confirms `targetBin`/`targetAssetName` are compiled-in and
never taken from argv/env.

**C3 — Bound the in-memory binary read and keep it sane on small devices.** AD-005 §2a reads the
staged binary fully into memory (required by C1). Confirm `maxBinarySize` is enforced (a
`riot`-planted huge staging file must refuse, not OOM) and that the normal ~15-30 MiB agent fits
comfortably on the smallest supported target (e.g. a low-RAM Pi). The cap is specified; QA
verifies the oversized-file refusal.

**C4 — Exact-basename match must account for the non-Linux asset names.** `parts[1] ==
"riot-agent-"+suffix` is correct for the Linux verifier and Linux agents, but the Windows asset
is `riot-agent-windows-amd64.exe`, so a Windows agent's pre-flight will never match and will
refuse every update (`strings.Contains` used to match it). The verifier is Linux-only and this
path never reached systemd auto-update on Windows/macOS anyway, so impact is nil — but the
senior_dev should either handle the platform extension in `parseExpectedChecksum` or explicitly
confirm/document that non-Linux agents do not auto-update, so QA does not log it as a regression.

**C5 — Verify the `-X` targets resolve and the committed key is real before the first signed
tag.** The embedded key uses the `releasekey` source default in every build path (no release-time
`-X` for the key), and `targetAssetName` is set per-platform via `-X main.targetAssetName`. QA/CI
must confirm (a) the committed `PublicKeyBase64` is the real 32-byte key, not the placeholder
(the SEC-003 `verify` gate enforces this once implemented), and (b) each
`riot-agent-apply-<suffix>` is built with its matching `targetAssetName`. A missing/empty
`targetAssetName` fails closed (no matching line), so this is availability, not a bypass.

**C6 — Cosmetic: `ReadWritePaths=…${RIOT_BIN}` is now vestigial.** With `RIOT_BIN` root-owned,
the `riot` service cannot write it regardless of the sandbox allowance; leaving it in
`ReadWritePaths` is harmless (DAC still denies) but slightly misleading. Optional cleanup; do not
expand it. Not a security issue.

## Verdict Rationale

**APPROVE WITH CONDITIONS.** No CRITICAL or HIGH findings remain. SEC-001 is closed by moving
authenticity to a root-owned compiled verifier that reads-once / verifies-in-memory /
writes-the-same-bytes to a hardcoded root-owned target over `O_NOFOLLOW` regular-file inputs,
with the install target and the verifier both made root-owned so `riot` has no direct-overwrite
or target-redirection path — the `riot` → root boundary this story exists to establish now
actually holds, including against a `riot` caller invoking the privileged rule directly, and the
old symlink arbitrary-read is eliminated because nothing is written before verification passes.
SEC-002/003/005/006 are resolved at the mechanism level and SEC-004's residual is now stated
accurately with a recorded follow-up for a signed version signal. The ed25519/server-MITM design
(AD-001/AD-003/AD-004 order, AD-008) that I approved in rev 1 is unchanged.

The six conditions are implementation-fidelity (C1-C4), build/CI hygiene (C5), and one cosmetic
note (C6). C1 and C2 are the properties the entire fix rests on and must be proven by tests and
by the MANUAL `riot`-shell exercise, not assumed. One item for the follow-up backlog rather than
a condition: the downgrade residual (SEC-004) remains open by design until a **signed** version
signal exists; AD-010's guard is the right interim and the ADD now says so.

**Routing:** invoke `senior_dev`, passing this report alongside the ADD (rev 2). QA must verify
C1-C5 (C1/C2 are security-critical) plus the AC mapping in ADD §8; the technical writer must
carry the BR-005/A-5 operator note **and** the new "key rotation requires a fleet-wide
`install.sh` re-run" and "existing hosts must re-run `install.sh` to get the verifier +
root-owned binary + new `ExecStartPre`" statements.
