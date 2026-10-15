# Architecture Decision Document

| Field         | Value                                        |
|---------------|----------------------------------------------|
| Story ID      | AGENT-HARDEN-001                             |
| FRD Reference | docs/requirements/AGENT-HARDEN-001-frd.md    |
| Author        | Architect Agent                              |
| Date          | 2026-10-15                                   |
| Revision      | 2                                            |
| Status        | FINAL                                        |

## Revision log

| Rev | Date | Change |
|---|---|---|
| 1 | 2026-10-15 | Initial FINAL. AD-001 – AD-009. Records the settled design (FRD D-1..D-6) as buildable decisions: ed25519 signing of `checksums.txt`, embedded public key, fail-closed verification rewrite of `performUpdate`, fixed root-owned apply helper + wildcard-free sudoers, `doctor` probe, default-on policy, and the release-workflow signing step. |
| 2 | 2026-10-15 | Revised against `docs/security/AGENT-HARDEN-001-security-review.md` (verdict REVISE). **SEC-001 (HIGH)** → **AD-005 and AD-006 rewritten**: the shell apply helper is replaced by a dedicated root-owned **compiled Go verifier** `cmd/riot-agent-apply` that embeds the same public key and performs signature+hash verification over the exact in-memory bytes it installs (O_NOFOLLOW reads, regular-file check, hardcoded target), so a `riot` compromise invoking it directly cannot install unsigned bytes and cannot read root files via a symlinked source; the ExecStartPre fallback and `RIOT_BIN` ownership are hardened to match. The embedded key moves to a shared package `internal/releasekey` (AD-002) so agent and verifier share one trust anchor. **SEC-002 (MED)** → AD-004: `checksums.txt`/`.sig` fetches and the verifier's file reads are size-capped (1 MiB) and refuse oversized bodies. **SEC-003 (MED)** → AD-009: a CI gate verifies the produced `.sig` against the committed embedded key and fails the build on mismatch / non-32-byte constant (`riot-signer verify`). **SEC-004 (MED)** → new **AD-010** (best-effort agent-side downgrade guard) and §10 rewritten to state the residual accurately (version is unsigned) with a recorded follow-up for a signed version signal. **SEC-005 (LOW)** → AD-004/verifier match the exact asset basename, not a substring. **SEC-006 (INFO)** → made explicit: the authoritative hash is computed by the verifier over the installed-file bytes, not the download stream. AD-001/AD-003, the fail-closed verification order, and the default-on policy (AD-008) are unchanged (security approved). |

---

## 0. FRD review outcome and recorded decisions

No questions for the Business Developer. The FRD is internally consistent and every AC is
technically fulfillable as written. The FRD's open questions (§11) are answered here as
architecture decisions; the architect is not choosing against any assumed answer.

- **OQ-1 (keep `systemd-run` or collapse to direct helper?)** → **Keep `systemd-run`, in the
  fixed wildcard-free form, as the *only* privileged update path.** See AD-005/AD-006: the
  detached transient unit is required because systemd kills the whole `riot-agent.service`
  cgroup (default `KillMode=control-group`) when the privileged step runs `systemctl stop
  riot-agent`, so the swap must run outside that cgroup. The direct-invocation sudoers rule is
  not added.
- **OQ-2 (sign `checksums.txt` vs per-binary)** → `checksums.txt` only (D-2, AD-001).
- **OQ-3 (rate-limit refusal emission)** → once per attempt is accepted (FR-009); no
  cross-cycle de-duplication is built.
- **OQ-4 (raw vs encoded embedded key)** → base64 (std encoding) of the raw 32-byte ed25519
  public key, held in a package-level `var` so `-ldflags -X` can override it (AD-002).
- **OQ-5 (anti-rollback)** → out of scope as a hard requirement (NFR-009, §9 item 1). Per the
  security review (SEC-004) a **best-effort** agent-side guard is added now (AD-010) and a
  signed version signal is raised as a follow-up; §10 states the residual accurately.

### Security review outcome (rev 2)

The security researcher approved the cryptographic design (ed25519-over-`checksums.txt`,
compiled-in fail-closed trust anchor, strictly-ordered verification) against the server/MITM
threat, and **blocked on SEC-001 (HIGH)**: in rev 1 the *privileged* apply step verified
nothing — it copied a `riot`-writable staging file as root, and `riot` held a standing sudo rule
to invoke it directly, so a `riot` compromise could (a) make root install arbitrary bytes into a
system binary and (b) read arbitrary root-owned files via a symlinked staging source. This
revision moves the authenticity check **onto the privileged side of the boundary** (AD-005/
AD-006) and folds in SEC-002..006. No AC is weakened; FRD D-4's "helper script" is amended to "a
compiled verifier binary" (recorded below).

**FRD amendment (D-4 helper form).** FRD D-4 describes the privileged apply component as a
root-owned *shell script* that performs stop→copy→chmod→rm→start. AD-005 amends its *form* to a
root-owned *compiled Go binary* that performs verify-then-stop→write→start, because a shell `cp`
cannot establish authenticity of the bytes it copies (SEC-001). The D-4 *properties* —
root-owned, `0755`, fixed absolute path, no `sh -c`, no wildcard, only fixed sudoers rules, and
the retained no-sudo restart fallback — are all preserved and strengthened. Engineering must
treat D-4 as amended here and not as a blocker.

**One design note, not a blocker (A-1 / server contract).** The agent locates
`checksums.txt.sig` through a new server-advertised `signature_url`, mirroring `checksum_url`
(AD-003). Trust derives only from the embedded key, so a wrong/missing URL is a safe refusal. A
*new* agent talking to an *old* server (no `signature_url`) refuses to update — the correct
fail-closed behaviour (FR-008, BR-005). Intended; noted so QA does not read it as a regression.

---

## 1. Summary

Release artifacts gain a detached ed25519 signature over `checksums.txt`
(`checksums.txt.sig`), produced in CI by a tiny in-repo signing tool reading the private key
from a GitHub Actions secret, and the CI run fails unless that signature verifies against the
public key compiled into the shipped binaries. Both the agent and a **new, dedicated,
root-owned compiled verifier** (`cmd/riot-agent-apply`) embed that public key from one shared
package. The agent's `performUpdate` becomes a strictly-ordered, size-capped, fail-closed
pipeline that stages the binary plus the signed `checksums.txt`/`.sig`; the **privileged
verifier** then re-verifies the signature and re-hashes the exact staged bytes in memory,
refusing symlinked or unverified input, and only then writes those same bytes to the hardcoded
system binary path and restarts the service. The arbitrary-root `sudo … sh -c *` primitive is
replaced by a single wildcard-free `systemd-run <verifier>` sudoers rule, with a matching
`doctor` probe and the (now verifier-gated) `ExecStartPre` restart fallback retained.
Auto-update stays default-on because authenticity is now enforced on the privileged side and
fails closed.

---

## 2. Technical Context

### 2.1 What exists

**Agent (Go 1.24, single `internal/agent` package + collectors)**

- `internal/agent/updater.go` — `updateCheckLoop` (30 s initial delay, then 6 h ticker) →
  `checkAndUpdate` → `performUpdate`. `checkAndUpdate` gates on `version == "dev"` (line 45) and
  on `config.Agent.AutoUpdate` (line 49). `performUpdate` (line 106) downloads to
  `stagingPath = /var/lib/riot/riot-agent.update` while streaming a SHA-256 over the **download
  stream** (`io.MultiWriter(stagingFile, hash)`, lines 144–151), then the advisory checksum
  block (154–163) fails *open*. Install uses `sudo systemd-run … sh -c`, `sudo sh -c`, and the
  staged-file restart. `fetchExpectedChecksum` (line 223) GETs and substring-scans the manifest.
- `internal/agent/http_client.go` — `UpdateCheckResponse` (line 194), `ChecksumURL` (line 201).
  Unknown JSON fields are ignored.
- `internal/agent/config.go` — `AgentConfig.AutoUpdate *bool` (line 71); `nil` ⇒ enabled.
- `internal/agent/doctor.go` — `checkHoldEnforcement` (263) and the smartctl check (185–190)
  establish the `sudo -n`/`sudo -n -l` probe + "re-run installer" pattern; `section/pass/warn/
  fail` helpers exist.
- `internal/agent/collectors/holds.go:146` — `VerifyPrivileges` = `sudo -n -l <cmd> <args…>`.

**Server (Go, `internal/server`)**

- `internal/server/updates/checker.go` — `UpdateInfo` (32), `ChecksumURL` (39).
  `AgentUpdateInfo` (168) populates `Assets[suffix]` and `ChecksumURL` from the `checksums.txt`
  asset, only when `UpdateAvail`. `isNewer`/`LatestVersion` are computed **server-side**
  (184) and trusted by the agent.
- `internal/server/handlers/handlers.go:948` — `AgentUpdateCheck` returns `AgentUpdateInfo`
  verbatim via `writeJSON`; any field added to `UpdateInfo` is advertised for free.

**Pipeline**

- `.github/workflows/release.yml` — `agent-binaries` builds the agent matrix into `dist/`
  (`-ldflags "-s -w -X main.version=…"`, lines 98–123), copies `install.sh`, runs
  `sha256sum * > checksums.txt` (line 130), then `softprops/action-gh-release@v2` with `files:` =
  `dist/riot-agent-*`, `dist/install.sh`, `dist/checksums.txt`. No signing.

**Installer**

- `scripts/install.sh` — path vars (83–86): `RIOT_USER="riot"`, `RIOT_DATA_DIR="/var/lib/riot"`,
  `RIOT_BIN="/usr/local/bin/riot-agent"`. Platform `SUFFIX` resolved from `uname -m` (128–149);
  agent downloaded from the release `${BINARY_NAME}=riot-agent-${SUFFIX}` to `$RIOT_BIN` (221–231)
  and then **`chown riot:riot "$RIOT_BIN"` (line 232–233)** — i.e. today the installed agent
  binary is `riot`-owned (relevant to AD-005). `RIOT_DATA_DIR` is `riot`-owned (212), so the
  staging path is `riot`-writable. Sudoers drop-in (437–482) with the two offending rules at
  lines 472 (`${SH_PATH} -c mv …`) and 473 (`${SYSTEMD_RUN_PATH} … sh -c *`); line 474 is
  `reset-failed`. The systemd unit (498–520) has `ExecStartPre=+/bin/sh -c 'test -f …update &&
  cp -f …'` (506 — an unverified root copy of a riot-writable file) and
  `ReadWritePaths=${RIOT_DATA_DIR} ${RIOT_CONFIG_DIR} ${RIOT_BIN}` (515); `User=riot` (510).

### 2.2 What is missing

- No signing, signing secret, or `checksums.txt.sig` asset; no CI correspondence check.
- No compiled-in trust anchor and no ed25519 verification anywhere.
- No `signature_url` on either contract.
- **No privileged component that verifies what it installs** — the swap (sudo helper *and*
  `ExecStartPre`) copies riot-writable bytes as root unverified (SEC-001).
- No size bound on the (not-yet-existing) checksum/signature reads.
- No `doctor` probe for the update-apply privilege.
- No agent-side downgrade guard.

---

## 3. Architecture Decisions

### AD-001: Signing scheme — raw ed25519 over `checksums.txt`, detached `.sig`, base64 single line

*(Unchanged in rev 2; security-approved.)*

**Context.** D-1/D-2/NFR-002 fix raw ed25519 via Go stdlib `crypto/ed25519` over the bytes of
`checksums.txt`, one signature covering all platforms. The only open representational choice is
the on-disk format, which must be identical on the producing (CI) and consuming (agent +
verifier) sides.

**Decision.** CI produces `checksums.txt.sig` = **`base64.StdEncoding` of the raw 64-byte
ed25519 signature**, single line, no trailing newline, computed over the **exact byte stream of
`checksums.txt`** (`ed25519.Sign(priv, checksumsBytes)`). Every consumer (agent pre-flight and
the privileged verifier) `strings.TrimSpace`es the `.sig`, `base64.StdEncoding.DecodeString`s
it, requires exactly `ed25519.SignatureSize` (64) bytes — else refuse — and calls
`ed25519.Verify(pub, checksumsBytes, sig)`.

**Rationale.** Base64 on a single line is safe through bash/CI/HTTP; signing the raw received
bytes makes V-001/AC-006 a byte-for-byte property.

**Alternatives considered.** Raw binary `.sig` (fragile across transports); base64url
(unnecessary); signing a canonicalized manifest (adds a canonicalization contract for no gain).

**Consequences.** The encoding is a contract shared by `cmd/riot-signer` (AD-009), the agent
pre-flight (AD-004), and the verifier (AD-005); all three use `base64.StdEncoding` over the raw
`checksums.txt` bytes. Decoded-length checking means a mangled `.sig` fails closed.

---

### AD-002: Embedded public key lives in a shared `internal/releasekey` package; private key is a CI secret

*(Rev 2: moved the key from `internal/agent` to a standalone shared package so both the agent
and the privileged verifier embed the identical trust anchor.)*

**Context.** FR-004/FR-005/NFR-001: non-secret compile-time trust anchor the server cannot
influence, `-ldflags -X`-overridable (§9 item 4); private key only in a CI secret (FR-003);
A-2 keypair generated out of band. Rev 1 placed the key in `internal/agent`, but the new
privileged verifier (AD-005) is a separate `main` package that must embed the **same** key;
importing all of `internal/agent` into it is wrong.

**Decision.**

1. **Shared key package.** New `internal/releasekey/releasekey.go`:
   ```go
   package releasekey
   // PublicKeyBase64 is the base64 (std encoding) of the 32-byte ed25519 public key
   // that authenticates release checksums. Non-secret. Overridable at build time for
   // rotation; never read from server, config, network, or disk.
   var PublicKeyBase64 = "<base64 of the 32-byte ed25519 public key>"
   // PublicKey decodes PublicKeyBase64, requiring exactly ed25519.PublicKeySize (32)
   // bytes; a bad/empty/placeholder value returns an error so callers fail CLOSED.
   func PublicKey() (ed25519.PublicKey, error)
   ```
   Imported by `internal/agent` (pre-flight), `cmd/riot-agent-apply` (verifier), and
   `cmd/riot-signer verify` (CI gate). Because it is a `var`, it is overridable at build time via:
   ```
   -ldflags "-X github.com/DesyncTheThird/rIOt/internal/releasekey.PublicKeyBase64=<base64pub>"
   ```
   (composes with `-X main.version=…`). A malformed/empty/placeholder key makes **every**
   verification refuse (fail closed), never accept.

2. **Private key.** GitHub Actions secret **`AGENT_SIGNING_PRIVATE_KEY`** = base64 of the
   64-byte `ed25519.PrivateKey`. Read only by the CI signing step (AD-009); never embedded,
   never logged.

3. **Keypair generation (one-time, A-2).** `cmd/riot-signer genkey` → `ed25519.GenerateKey`,
   printing the base64 public key (paste into `PublicKeyBase64`) and base64 private key (paste
   into the secret) on labelled stdout lines only.

4. **Rotation story (automation out of scope, §9 item 4).** Run `genkey`; cut **one**
   transitional release still signed with the *current* private key but whose binaries embed the
   *new* public key; the fleet updates onto it using their currently-embedded (old) key. **Both
   the agent and the verifier must embed the new key — the verifier only updates on a deliberate
   `install.sh` re-run (AD-005 §6), so key rotation is a deliberate fleet re-install, not an
   auto-update.** After the fleet has the new key embedded in *both* binaries, swap
   `AGENT_SIGNING_PRIVATE_KEY` to the new private key; subsequent releases sign with it and
   verify against the now-embedded new key.

**Rationale.** A standalone leaf package is the only clean way for two `main`s plus the agent to
share one `-X`-overridable anchor. Decode+length-check at load makes the anchor authenticate-or-
refuse. One Go tool for keygen/sign/verify avoids openssl-ed25519 footguns and keeps encodings
identical across producer and both consumers.

**Alternatives considered.** (a) Key in `internal/agent`, verifier imports it — rejected: drags
the whole agent into a privileged `main`. (b) `const` — rejected: `-X` cannot write a const.
(c) `go:embed` a key file — rejected: not `-X`-overridable.

**Consequences.** Maintainer runs `genkey` once, commits the public key into `releasekey.go`,
and sets the secret before the first signed release (FRD §12, blocking). The `-X` ldflags target
is `…/internal/releasekey.PublicKeyBase64` in all build paths.

---

### AD-003: `signature_url` added to both update-info contracts; server advertises, never asserts trust

*(Unchanged in rev 2; security-approved.)*

**Context.** The agent must *locate* `checksums.txt.sig`. FR-006/BR-001: the server locates
assets but is not a trust anchor. A-1: reuse the existing advertisement mechanism.

**Decision.** Add one field to each struct, mirroring `ChecksumURL`:

- `internal/server/updates/checker.go`, `UpdateInfo`:
  `SignatureURL string json:"signature_url,omitempty"`, populated inside `AgentUpdateInfo`'s
  existing `UpdateAvail` asset loop:
  ```go
  } else if asset.Name == "checksums.txt.sig" {
      info.SignatureURL = asset.BrowserDownloadURL
  }
  ```
  (The existing binary-asset test `Contains(name, suffix) && !HasSuffix(name, ".txt")` does not
  match `checksums.txt.sig` — the `.sig` name contains no platform suffix.)
- `internal/agent/http_client.go`, `UpdateCheckResponse`:
  `SignatureURL string json:"signature_url,omitempty"`.

**Rationale.** NFR-008 caps the contract change at "advertising the `.sig` location"; this is
exactly that. Additive `omitempty` fields are back/forward compatible: a new agent seeing an
empty `signature_url` from an old server refuses (correct fail-closed outcome).

**Alternatives considered.** Deriving the `.sig` URL agent-side by string-appending `.sig`
(bakes GitHub URL shape into the agent); a new endpoint (unnecessary surface).

**Consequences.** `checker_test.go` gains a `SignatureURL` assertion mirroring `ChecksumURL`.

---

### AD-004: Agent `performUpdate` — ordered, size-capped, fail-closed pipeline that stages the signed material (pre-flight only; not the security boundary)

*(Rev 2: added SEC-002 size caps and SEC-005 exact-match; the agent now also stages
`checksums.txt`/`.sig` for the privileged verifier; made SEC-006 explicit — the agent-side hash
over the download stream is a pre-flight convenience, and the authoritative hash is the
verifier's re-read of the installed bytes.)*

**Context.** FR-007 – FR-013, V-001 – V-004, ACs 001–006/019/020. The advisory warn-then-install
path and the "verify only if `checksumURL != ""`" behaviour must become mandatory and
fail-closed. Per SEC-001 the agent runs as `riot` and is therefore **not** the trust boundary;
its verification is a cheap early-out and a precondition for staging. The authoritative check is
AD-005.

**Decision.** `checkAndUpdate` passes the new URL and gains the AD-010 downgrade guard, then
calls `a.performUpdate(ctx, downloadURL, resp.ChecksumURL, resp.SignatureURL, suffix)`. The
signature becomes:

```go
func (a *Agent) performUpdate(ctx context.Context, downloadURL, checksumURL, signatureURL, suffix string) error
```

Order (every "refuse" returns a non-nil `error`, which `checkAndUpdate` maps to `slog.Error` +
`EventAgentUpdateFailed` and a continuing agent; the `defer` removes staging while `staged ==
false`):

1. Open `stagingPath`, stream-download the binary computing SHA-256 (`io.MultiWriter`). Non-200
   or I/O error → refuse. `actualSum := hex(hash)`. *(This stream hash is pre-flight only —
   SEC-006: the authoritative hash is the verifier's re-read, AD-005.)*
2. `if checksumURL == "" || signatureURL == "" { return … }` — deletes the optional-checksum
   path (FR-012) and covers unsigned/old releases (AC-002/FR-026).
3. `checksumsBytes, err := fetchBytes(ctx, checksumURL, maxMetaFileSize)` — HTTP 200 required;
   non-200/transport/timeout/**oversized** → refuse (V-004, AC-004, SEC-002).
4. `sigBytes, err := fetchBytes(ctx, signatureURL, maxMetaFileSize)` — HTTP 200 / size required
   → refuse otherwise (V-004, AC-003, SEC-002).
5. `verifyReleaseSignature(checksumsBytes, sigBytes)` against the embedded key → refuse on
   failure (V-001, AC-006).
6. `expectedSum, err := parseExpectedChecksum(checksumsBytes, suffix)` — refuse if unparseable
   or no entry (V-002).
7. `if !strings.EqualFold(expectedSum, actualSum) { return … }` — refuse on mismatch (V-003,
   AC-001).
8. **Only now** persist the signed material next to the binary for the privileged verifier:
   write `checksumsBytes` → `stagingChecksumsPath` and `sigBytes` → `stagingSigPath` (both under
   `RIOT_DATA_DIR`), then hand off to AD-006. The live binary is untouched by the agent in all
   paths.

New/changed helpers in `updater.go`:

```go
const (
    stagingPath          = "/var/lib/riot/riot-agent.update"            // unchanged
    stagingChecksumsPath = "/var/lib/riot/riot-agent.update.checksums"  // new
    stagingSigPath       = "/var/lib/riot/riot-agent.update.sig"        // new
    maxMetaFileSize      = 1 << 20 // 1 MiB cap for checksums.txt / .sig (SEC-002)
)

// fetchBytes GETs url (30s timeout), requires HTTP 200, and returns up to `limit`
// bytes; a body exceeding `limit` is an error (fail closed). Uses io.LimitReader(body,
// limit+1) and rejects when the read length > limit.
func fetchBytes(ctx context.Context, url string, limit int64) ([]byte, error)

// verifyReleaseSignature checks the ed25519 signature (base64, single line, trimmed,
// length==64) over the exact bytes of checksums.txt against releasekey.PublicKey().
func verifyReleaseSignature(checksumsBytes, sigB64 []byte) error

// parseExpectedChecksum scans verified checksums.txt bytes for the line whose filename
// FIELD EQUALS the exact asset basename for `suffix` ("riot-agent-"+suffix), not a
// substring (SEC-005). Operates only on already-verified bytes.
func parseExpectedChecksum(checksumsData []byte, suffix string) (string, error)
```

`fetchExpectedChecksum` (fetch+parse) is **removed**; the scanner logic moves into
`parseExpectedChecksum`, which now compares `parts[1] == "riot-agent-"+suffix` (exact, SEC-005)
instead of `strings.Contains`. `loadReleasePublicKey` is replaced by `releasekey.PublicKey()`
(AD-002).

**Rationale.** A linear guard pipeline makes "fail closed on every condition" true by
construction and maps one guard per AC. Size caps close the SEC-002 memory-exhaustion vector on
the common "server claims an update" path. Exact-field matching removes the SEC-005 fragility
now that the parse runs over verified bytes. Staging the signed material (not just the binary)
is what lets the privileged verifier be authoritative without re-fetching from the untrusted
server.

**Alternatives considered.** Dropping agent-side verification entirely and letting only the
verifier check — workable, but the pre-flight avoids invoking the root path (and emits a precise
failure event) on an obviously bad download, and is cheap; it is kept strictly as an early-out.

**Consequences.** `crypto/ed25519`, `encoding/base64`, `io`, and `internal/releasekey` join the
imports. The agent writes three staged files; the verifier consumes and removes them (AD-005).

---

### AD-005: Privileged install is a dedicated root-owned **compiled verifier** that authenticates the exact bytes it writes

*(Rev 2: full rewrite — replaces the rev-1 shell helper. Resolves SEC-001; incorporates SEC-005/
SEC-006.)*

**Context.** SEC-001 (HIGH): the rev-1 shell helper copied a `riot`-writable staging file as
root and verified nothing, and `riot` could invoke it directly — a root-write primitive and a
symlink-based arbitrary-root-file-read for the exact actor NFR-003/BR-003 promise to contain. The
privileged step must establish authenticity of the exact bytes it installs, over
`riot`-unspoofable inputs, and must assume the caller verified nothing.

**Decision.**

1. **A new compiled binary `cmd/riot-agent-apply` (the "verifier")**, **not** the installed agent
   binary (which is `riot`-adjacent). Linux-only (systemd auto-update is Linux-only). It embeds
   the same trust anchor via `internal/releasekey` (AD-002) and the expected install target and
   asset name as build-time constants:
   ```go
   // cmd/riot-agent-apply/main.go
   const targetBin = "/usr/local/bin/riot-agent" // HARDCODED; never an argument
   var targetAssetName = "riot-agent-linux-amd64" // -X per platform in release.yml (SEC-005)
   ```
   Fixed install path: **`/usr/local/lib/riot/riot-agent-apply`**, owner `root:root`, mode
   `0755`, downloaded+installed by `install.sh` (§6). In Go the agent/doctor reference it via:
   ```go
   // updater.go
   const applyBinPath = "/usr/local/lib/riot/riot-agent-apply"
   const updateUnit   = "riot-agent-update"
   ```

2. **`verifyAndInstall()` — the authoritative check, over in-memory bytes (SEC-001/SEC-006).**
   a. Open `stagingPath` with `O_RDONLY|O_NOFOLLOW`; `fstat` and require a **regular file** (not
      a symlink/dir/device) — else refuse (defeats the symlinked-source arbitrary-read). Read the
      bytes **once into memory** (bounded by a generous `maxBinarySize`, e.g. 256 MiB, to avoid
      OOM on a `riot`-planted huge file).
   b. Open `stagingChecksumsPath` and `stagingSigPath` the same way (`O_NOFOLLOW`, regular file),
      each read through `io.LimitReader` ≤ `maxMetaFileSize` (1 MiB; SEC-002) → refuse if over.
   c. `pub, err := releasekey.PublicKey()` (fail closed on a bad embedded key);
      `ed25519.Verify(pub, checksumsBytes, decode(sigBytes))` → refuse on failure.
   d. `expected := exact-match line` for `targetAssetName` in `checksumsBytes` (field equality,
      SEC-005) → refuse if absent/unparseable.
   e. `sha256(binaryBytes) == expected` (EqualFold) → refuse on mismatch. **This is the
      authoritative hash — over the in-memory bytes that will be written, not a download
      stream.**
   f. Write **those same in-memory bytes** to a temp file in `/usr/local/bin` (root-owned dir)
      opened `O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW`, `fsync`, `chmod 0755`, then `rename` over
      `targetBin` (atomic; verified-bytes ≡ installed-bytes). Remove the three staging files.

   Because every refusal path returns before (f), a `riot` caller that stages arbitrary bytes and
   invokes the verifier directly gets a refusal (it cannot produce a validly-signed
   `checksums.txt`/`.sig` pair for its bytes without the private key), and a symlinked source is
   rejected at (a). The worst a `riot` caller can do is install a **genuinely-signed** (possibly
   older) real release — the downgrade residual (SEC-004/AD-010), not arbitrary code.

3. **Two modes (self-stop handling, OQ-1).**
   - **apply mode (default, invoked by the agent via `systemd-run`):** `sleep 3` →
     `systemctl stop riot-agent` → `verifyAndInstall()` → `systemctl start riot-agent`. On any
     `verifyAndInstall` failure it still runs `systemctl start riot-agent` (restore service),
     logs, removes staging, and exits non-zero. Running under the detached `systemd-run` scope
     means stopping `riot-agent` does not kill the verifier (its own cgroup).
   - **`--boot` mode (invoked by the unit's `ExecStartPre`):** if `stagingPath` is absent, exit
     0 (nothing to do). Otherwise `verifyAndInstall()` with **no** stop/start (the service is
     already starting). On failure, remove staging, log, and **exit 0** so a bad staged update
     never blocks the current agent from starting (NFR-005, availability).

   The `--boot` flag is used only by the root `ExecStartPre` inside the unit (not via sudo), so
   it needs no sudoers entry; the sudo path invokes the verifier with **no arguments**.

4. **Sudoers rewrite (`install.sh`, replacing lines 472–474).**

   **Before:**
   ```
   riot ALL=(root) NOPASSWD: ${SH_PATH} -c mv -f ${RIOT_BIN} ${RIOT_BIN}.old && cp ${RIOT_DATA_DIR}/riot-agent.update ${RIOT_BIN} && chmod 755 ${RIOT_BIN} && rm -f ${RIOT_BIN}.old
   riot ALL=(root) NOPASSWD: ${SYSTEMD_RUN_PATH} --unit=riot-agent-update sh -c *
   riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} reset-failed riot-agent-update
   ```

   **After:**
   ```
   riot ALL=(root) NOPASSWD: ${SYSTEMD_RUN_PATH} --unit=riot-agent-update /usr/local/lib/riot/riot-agent-apply
   riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} reset-failed riot-agent-update
   ```

   Both `sh -c` rules deleted; `reset-failed` retained. No rule contains `sh -c`, a shell, or a
   `*` wildcard (FR-015/FR-017/AC-008). Because sudo matches argv exactly with no wildcard, no
   `systemd-run` property-injection (`-p ExecStartPost=…`, extra `--property`) is reachable. The
   package-manager/`systemctl reboot`/`smartctl`/`nginx` rules are untouched (FR-020/AC-014/D-6).

5. **Install-target ownership hardened (SEC-001).** `install.sh` installs the agent binary
   `root:root` (removing the current `chown riot:riot "$RIOT_BIN"` at line 232–233). Combined
   with the root-owned `/usr/local/bin` directory, `riot` can no longer overwrite the installed
   agent directly, so **every** install must pass through the verifier. The agent never writes
   `RIOT_BIN` (all strategies were already root-side), so this breaks nothing.

6. **Verifier install + update story.** `install.sh` downloads the per-platform
   `riot-agent-apply-${SUFFIX}` release asset (mirroring the agent download, 221–231) to
   `/usr/local/lib/riot/riot-agent-apply`, `chown root:root`, `chmod 0755` (and `mkdir -p
   /usr/local/lib/riot`). **The verifier is NOT auto-updated** — only the agent binary is
   swapped. A stale verifier is acceptable as long as the embedded public key is unchanged (it
   verifies all releases signed by the same key). Key rotation therefore requires a deliberate
   `install.sh` re-run to re-download a verifier embedding the new key (AD-002 §4); until then a
   verifier with the old key would refuse releases signed by the new key, which is a visible
   fail-closed stop, not a bypass.

7. **Unit file.** `ExecStartPre` changes from the raw `cp` to the verifier (closing the SEC-001
   hole in the restart path too):
   ```
   ExecStartPre=-+/usr/local/lib/riot/riot-agent-apply --boot
   ```
   The `+` runs it with full privileges (root) regardless of `User=riot`; the `-` ignores
   failure/absence so a partially-upgraded host (no verifier yet) still starts. `ReadWritePaths`
   stays; `RIOT_BIN` is now root-owned but the agent never writes it.

**Rationale.** A compiled verifier with a compiled-in key is the only way to make authenticity a
property of the *privileged* side (SEC-001's required resolution). Reading once into memory and
writing those same bytes removes TOCTOU (SEC-006); `O_NOFOLLOW` + regular-file check removes the
symlink read; the hardcoded target removes target-redirection; root ownership of binary and
`/usr/local/bin` removes `riot`'s direct-overwrite path. `systemd-run` is retained (fixed form)
for the cgroup self-stop reason (OQ-1).

**Alternatives considered.** (a) Pass the verified expected SHA-256 to a shell helper via a
root-only channel — rejected: still a shell doing privileged `cp`, and the "root-only channel"
is awkward versus just embedding the key. (b) Keep the shell helper but `chattr +i` the staging
file after agent verify — rejected: `riot` owns the dir, racey, and doesn't address direct
invocation. (c) Make the installed agent binary itself the verifier (`riot-agent apply`) —
rejected explicitly by the review: the installed agent is overwrite-adjacent to `riot`; the
verifier must be a separate root-owned binary.

**Consequences.** Existing hosts must re-run `install.sh` to get the verifier, the tightened
rule, root-owned binary, and the new `ExecStartPre` (A-4); until then `doctor` reports the
privilege missing (AC-013) and the agent cannot complete a swap (it refuses at the sudo step and
keeps running). `cmd/riot-agent-apply` is cross-compiled per Linux platform and published
(AD-009). The `visudo -cf` validation gate is retained.

---

### AD-006: `updater.go` apply path — stage the signed material, invoke the detached verifier; verifier-gated restart fallback

*(Rev 2: rewritten to drive the AD-005 verifier; removed all `sh -c` script construction.)*

**Context.** FR-018, §2.2 item 1, AC-010/AC-011. The three rev-1 strategies (two `sh -c`) are
replaced.

**Decision.** After AD-004 step 8 (binary + `checksums.txt` + `.sig` staged), `performUpdate`
applies with exactly two paths, in order:

1. **Primary — detached verifier via `systemd-run`:**
   ```go
   _ = exec.Command("sudo", "systemctl", "reset-failed", updateUnit).Run()
   cmd := exec.CommandContext(ctx, "sudo", "systemd-run",
       "--unit="+updateUnit, applyBinPath)
   if out, err := cmd.CombinedOutput(); err == nil {
       staged = true // keep the three staged files for the transient unit
       return nil
   }
   // else: slog.Debug and fall through
   ```
   No script string; the only argument after `--unit=riot-agent-update` is the fixed
   `applyBinPath`, matching the sudoers rule byte-for-byte. The verifier re-verifies
   authoritatively (AD-005) — the agent's pre-flight is not trusted by it.

2. **Fallback — verifier-gated restart apply (no sudo):** leave the three verified staged files
   in place (`staged = true`) and `slog.Warn` that the update applies on next restart (re-run
   `install.sh`). On the next service start the unit's `ExecStartPre=-+… --boot` runs the
   verifier, which re-verifies and installs (FR-019/AC-011). Because the fallback now goes through
   the same verifier, it is no longer an unverified root copy (SEC-001).

The rev-1 strategy 2 (`sudo sh -c "mv … && cp …"`) is **removed entirely** (FR-015/FR-018), and
the raw-`cp` `ExecStartPre` is replaced by the verifier (AD-005 §7).

**Rationale.** Two paths cover the two deployment states (installer re-run → verifier present →
immediate swap; not re-run → staged apply on next restart), both authoritative, zero `sh -c`.
`reset-failed` before `systemd-run` clears any prior failed transient unit (why its rule is
kept).

**Alternatives considered.** A direct in-place `cp` fallback — rejected: unverified root copy and
EBUSY on the running binary.

**Consequences.** `os.Executable()` is no longer needed in `performUpdate` (target is baked into
the verifier). The staging paths are shared constants (AD-004) matching the verifier's and the
unit's expectations (V-005).

---

### AD-007: `doctor` probes the exact `systemd-run <verifier>` rule

*(Rev 2: probe path updated to the verifier binary.)*

**Context.** FR-021 – FR-023, AC-012/013. House `sudo -n -l` pattern (holds.go:146), targeting
the new fixed rule.

**Decision.** Add `checkUpdatePrivilege()` to `doctor.go`, called on Linux when auto-update is
effectively enabled (`runtime.GOOS == "linux"` and `cfg.Agent.AutoUpdate == nil ||
*cfg.Agent.AutoUpdate`):

```go
func checkUpdatePrivilege() {
    section("Agent Update")
    systemdRun, _ := exec.LookPath("systemd-run")
    if systemdRun == "" { systemdRun = "/usr/bin/systemd-run" }
    cmd := exec.Command("sudo", "-n", "-l",
        systemdRun, "--unit="+updateUnit, applyBinPath) // applyBinPath = /usr/local/lib/riot/riot-agent-apply
    if _, err := cmd.CombinedOutput(); err != nil {
        fail("update-apply sudo rule missing — auto-update cannot swap the binary")
        warn("  Re-run the install script (curl … | sudo bash) to rewrite /etc/sudoers.d/riot-agent and install the apply helper")
    } else {
        pass("update-apply sudo rule present (%s --unit=%s %s)", systemdRun, updateUnit, applyBinPath)
    }
    // Secondary: the verifier binary must exist and be root-owned.
    if fi, err := os.Stat(applyBinPath); err != nil {
        fail("apply helper missing at %s — re-run the install script", applyBinPath)
    } else if fi.Mode().Perm() != 0o755 {
        warn("apply helper at %s is not mode 0755", applyBinPath)
    } else {
        pass("apply helper present (%s)", applyBinPath)
    }
}
```

`updateUnit`/`applyBinPath` are the same Go constants the agent invokes, so the probe cannot
drift from the real invocation (V-005). The removed `sh -c *` rule is never referenced
(FR-023/AC-012).

**Rationale.** `sudo -n -l <cmd> <args>` is the non-destructive "would sudo allow exactly this?"
check the house pattern prescribes. Adding a presence/mode check for the verifier binary catches
the "re-ran only part way" state.

**Alternatives considered.** Reusing `HoldManager.VerifyPrivileges` (wrong domain); grepping the
sudoers file (not what sudo evaluates).

**Consequences.** One new `section("Agent Update")`; diagnostics only.

---

### AD-008: Auto-update stays default-on; `config.go` unchanged

*(Unchanged in rev 2; security-approved.)*

**Context.** FR-024 – FR-027, BR-004, AC-016, D-5.

**Decision.** No change to `internal/agent/config.go`: `AutoUpdate *bool`, `nil ⇒ enabled`. No
change to the `version == "dev"` short-circuit (AC-015) or explicit-`false` suppression
(FR-027/AC-016). The default is safe because authenticity is now enforced on the privileged side
and fails closed (AD-005): an unsigned/unverifiable release is a safe no-op.

**Rationale.** BR-004 ties the default to the guarantee; with privileged fail-closed
verification, default-on costs a benign refusal per cycle on unsigned releases and gains
zero-touch updates once a signed release ships.

**Alternatives considered.** Flip default to off — forbidden by FR-024; safety comes from
verification, not the default.

**Consequences (transition, BR-005).** Until the first signed release (built by the updated
pipeline *and* applied by a verifier/agent embedding the matching key), auto-update-enabled
non-dev agents attempt and refuse each cycle, emitting one `EventAgentUpdateFailed` + error log
per attempt. Intended; communicated to operators (A-5).

---

### AD-009: Release workflow cross-compiles the verifier, signs `checksums.txt`, and CI-verifies the signature against the embedded key

*(Rev 2: added the `riot-agent-apply` matrix build and the SEC-003 correspondence gate.)*

**Context.** FR-001 – FR-004, AC-017, SEC-003. CI must build+publish the verifier, sign, and
**fail the release** unless the produced signature verifies against the key the binaries embed.

**Decision.** `cmd/riot-signer` carries three subcommands sharing the AD-001/AD-002 encodings:
- `genkey` (AD-002);
- `sign <in> <out>` — reads `AGENT_SIGNING_PRIVATE_KEY` (base64, 64-byte priv), writes
  `base64.StdEncoding(ed25519.Sign(priv, readFile(in)))` single-line to `<out>`; non-zero exit on
  missing/malformed key; prints nothing derived from the key;
- **`verify <checksums> <sig>` (SEC-003)** — imports `internal/releasekey`, calls
  `releasekey.PublicKey()` (non-zero exit if the committed constant is not a valid 32-byte key),
  and `ed25519.Verify`; non-zero exit on mismatch.

`.github/workflows/release.yml`, `agent-binaries` job:

1. **Build the verifier matrix** in the existing `build()` loop (Linux platforms only), with the
   version ldflag; bake the per-platform asset name:
   ```sh
   # inside the build script, for each linux os/arch/arm:
   env GOOS="$os" GOARCH="$arch" ${arm:+GOARM="$arm"} \
     go build -ldflags "${LDFLAGS} -X main.targetAssetName=riot-agent-${suffix}" \
     -o "dist/riot-agent-apply-${suffix}" ./cmd/riot-agent-apply
   ```
   (`LDFLAGS` already carries `-X main.version`; the public key uses the committed source default
   in `internal/releasekey`, exactly like the agent, so no `-X …PublicKeyBase64` override at
   release time.)
2. `Generate checksums` (`sha256sum * > checksums.txt`) now also covers the
   `riot-agent-apply-*` binaries — unchanged command.
3. **Sign** (secret via `env:` only, never echoed):
   ```yaml
   - name: Sign checksums
     env:
       AGENT_SIGNING_PRIVATE_KEY: ${{ secrets.AGENT_SIGNING_PRIVATE_KEY }}
     run: go run ./cmd/riot-signer sign dist/checksums.txt dist/checksums.txt.sig
   ```
4. **Verify against the embedded key (SEC-003 gate) — fails the release on mismatch:**
   ```yaml
   - name: Verify signature against embedded key
     run: go run ./cmd/riot-signer verify dist/checksums.txt dist/checksums.txt.sig
   ```
5. Publish the new assets:
   ```yaml
       files: |
         dist/riot-agent-*            # now also matches riot-agent-apply-*
         dist/install.sh
         dist/checksums.txt
         dist/checksums.txt.sig       # NEW
   ```

**Rationale.** A Go tool reusing the agent/verifier encodings is the most testable signer and
avoids openssl-ed25519 friction. The `verify` gate turns the dangerous silent fleet-brick
(committed key ≠ CI key, or placeholder key) into a loud build failure, enforcing FR-004 at the
point it matters (SEC-003). Signing right after `sha256sum` guarantees the `.sig` is over exactly
the published bytes.

**Alternatives considered.** openssl `pkeyutl` (awkward ed25519/PEM, divergent encoding); a
third-party signing action (§9 item 3 forbids external signing services).

**Consequences.** `cmd/riot-signer` gets unit tests (`sign`/`verify` round-trip; `verify` fails on
a tampered `.sig` and on a bad embedded constant). `AGENT_SIGNING_PRIVATE_KEY` must exist before
the first signed tag (FRD §12, blocking). FR-003/AC-017 (no key in logs/assets) holds — the key
lives only in the step env and the tool's memory.

---

### AD-010: Best-effort agent-side downgrade guard (SEC-004)

*(New in rev 2 — SEC-004.)*

**Context.** SEC-004 (MED): a genuinely-signed *older* release passes AD-004/AD-005 (it is
authentic and self-consistent). `UpdateAvail`/`LatestVersion` are server-supplied and the only
agent-side gate today is "did the server set `UpdateAvail`." `checksums.txt` filenames carry **no
version**, so the binary has no in-band signed version attestation — an agent-side check can
reject a server that *honestly* advertises an older version but **not** one that advertises a
high `LatestVersion` while serving old signed bytes.

**Decision.** In `checkAndUpdate`, before `performUpdate`, add a local, best-effort guard:
refuse (debug log, no event) when `resp.LatestVersion` is **not strictly newer** than
`a.version`, using a small local semver comparison equivalent to `updates.isNewer` (the agent
must not call the server package; a ~15-line local `isNewerVersion(latest, current)` on the
`major.minor.patch` triple, dev → never). This defeats a server that advertises an older
`LatestVersion`.

Record in §10 that this is **best-effort and bypassable**: because version is absent from the
signed material, a MITM/forged server can claim a high `LatestVersion` while serving an older
signed binary, and the agent cannot cryptographically learn the real version of the bytes.
Robust anti-rollback requires a **signed** version signal and is a follow-up (below).

**Rationale.** Cheap defense-in-depth that closes the honest-but-old vector at zero crypto cost,
without over-claiming. Keeps full anti-rollback out of scope (OQ-5 "No") while improving on rev 1.

**Alternatives considered.** Doing nothing (rev 1) — rejected per SEC-004's non-blocking
condition. Blocking the story on full anti-rollback — rejected: explicitly maintainer-deferred.

**Consequences (follow-up, recorded).** A separate story should put a signed version into the
release — e.g. version in the `checksums.txt` filenames (`riot-agent-<ver>-<suffix>`) or a signed
manifest line — so the verifier can enforce monotonicity cryptographically. Until then the
downgrade residual stands (§10).

---

## 4. Component Changes

### 4.1 New files

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `internal/releasekey/releasekey.go` | Shared `var PublicKeyBase64` (compiled-in ed25519 public key, `-ldflags -X`-overridable) + `PublicKey()` decode-and-length-check (AD-002); imported by agent, verifier, and signer-verify |
| CREATE | `internal/releasekey/releasekey_test.go` | `PublicKey()` fails closed on empty/placeholder/non-32-byte; decodes a valid key |
| CREATE | `cmd/riot-agent-apply/main.go` | Root-owned privileged verifier: `verifyAndInstall()` (O_NOFOLLOW, regular-file, in-memory read→ed25519-verify→exact-basename hash→atomic write to hardcoded `targetBin`), apply mode + `--boot` mode (AD-005); embeds `internal/releasekey`; `-X main.targetAssetName` per platform |
| CREATE | `cmd/riot-agent-apply/main_test.go` | SEC-001 table tests: arbitrary unsigned bytes → refuse (no write); symlinked staging source → refuse; tampered checksums/sig → refuse; valid triple → installs to a temp target; `--boot` no-staging → no-op; `--boot` bad staging → staging removed, exit 0 |
| CREATE | `cmd/riot-signer/main.go` | `genkey`, `sign <in> <out>` (AD-009), `verify <in> <sig>` (SEC-003 gate, imports `internal/releasekey`) |
| CREATE | `cmd/riot-signer/main_test.go` | `sign`→`verify` round-trip against a generated key; `verify` fails on tampered `.sig` and on a bad embedded constant (AC-017, SEC-003) |
| CREATE | `internal/agent/updater_verify_test.go` | AC-001/002/003/004/005/006/019/020 against an `httptest` server (binary + `checksums.txt` + `.sig`, with tampering and oversized-body cases, SEC-002); `verifyReleaseSignature`, `parseExpectedChecksum` exact-match (SEC-005), `fetchBytes` size cap |

### 4.2 Modified files

| Action | File Path | Change |
|---|---|---|
| MODIFY | `internal/agent/updater.go` | Add `applyBinPath`/`updateUnit`/`stagingChecksumsPath`/`stagingSigPath`/`maxMetaFileSize` consts; thread `signatureURL`; rewrite `performUpdate` as the ordered fail-closed pipeline that also stages `checksums.txt`/`.sig` (AD-004); add `fetchBytes(limit)` (size cap), `verifyReleaseSignature`, `parseExpectedChecksum` (exact basename); **remove** `fetchExpectedChecksum` and the advisory block; drive the AD-006 verifier via `systemd-run`, deleting both `sh -c` strategies; add the AD-010 downgrade guard + local `isNewerVersion`; import `crypto/ed25519`, `encoding/base64`, `io`, `internal/releasekey` |
| MODIFY | `internal/agent/http_client.go` | Add `SignatureURL string json:"signature_url,omitempty"` to `UpdateCheckResponse` (AD-003) |
| MODIFY | `internal/server/updates/checker.go` | Add `SignatureURL` to `UpdateInfo`; populate from the `checksums.txt.sig` asset (AD-003) |
| MODIFY | `internal/server/updates/checker_test.go` | Assert `SignatureURL` from a `checksums.txt.sig` asset, mirroring the `ChecksumURL` test |
| MODIFY | `internal/agent/doctor.go` | Add `checkUpdatePrivilege()` (probe `systemd-run --unit=riot-agent-update <applyBinPath>` + verifier presence/mode) and its gated call (AD-007) |
| MODIFY | `scripts/install.sh` | Download+install `riot-agent-apply-${SUFFIX}` to `/usr/local/lib/riot/riot-agent-apply` `root:root 0755` (`mkdir -p /usr/local/lib/riot`); install `$RIOT_BIN` **`root:root`** (remove the `chown riot:riot "$RIOT_BIN"`); replace sudoers lines 472–473 with the single fixed `systemd-run` rule, keep 474; change the unit `ExecStartPre` to `-+/usr/local/lib/riot/riot-agent-apply --boot`; leave all other rules/unit directives untouched (AD-005/AD-006) |
| MODIFY | `.github/workflows/release.yml` | Cross-compile `riot-agent-apply-<suffix>` (Linux matrix, `-X main.targetAssetName=riot-agent-<suffix>`); add the `Sign checksums` step (secret via `env:`); add the `Verify signature against embedded key` gate (SEC-003); add `dist/checksums.txt.sig` to `files:` (the `dist/riot-agent-*` glob already matches `riot-agent-apply-*`) (AD-009) |

### 4.3 Deliberately NOT changed

`internal/agent/config.go` (default-on, AD-008); the unit's `ReadWritePaths`/`ProtectSystem`
logic (AD-005 §7 only swaps the `ExecStartPre` command and `RIOT_BIN` ownership); the
package-manager/`systemctl reboot`/`smartctl`/`nginx` sudoers rules (FR-020/D-6); the
`agent-heal-dpkg` branch / PR #1 (D-6); any database schema (§9 item 11); the 6-hour cadence and
30 s startup delay (§9 item 2).

---

## 5. Data Model Changes

No database schema change (FRD §7, §9 item 11). Contract changes: the two additive `omitempty`
`signature_url` fields (AD-003). New artifacts:

| Artifact | New? | Shape |
|---|---|---|
| `checksums.txt` | no | `"<sha256-hex>  <filename>"` per line, now also listing the `riot-agent-apply-*` binaries |
| `checksums.txt.sig` | **yes** | Single line: `base64.StdEncoding(raw 64-byte ed25519 signature over the exact bytes of checksums.txt)` (AD-001) |
| `riot-agent-apply-<suffix>` | **yes** | Compiled Linux verifier binaries (AD-005), one per Linux platform, covered by `checksums.txt`/`.sig` |
| embedded `releasekey.PublicKeyBase64` | **yes** | base64 (std) of the 32-byte ed25519 public key, compiled into agent + verifier, `-ldflags -X`-overridable (AD-002) |
| staged `riot-agent.update.checksums` / `.sig` | **yes** | The agent-fetched, verified `checksums.txt`/`.sig` bytes, staged under `RIOT_DATA_DIR` for the privileged verifier (AD-004 step 8) |
| `/usr/local/lib/riot/riot-agent-apply` | **yes** | Root-owned `0755` installed verifier (AD-005 §6) |

---

## 6. API / Interface Contract

**`GET /api/v1/update/check?version=&os=&arch=&arm=`** (agent-facing; unchanged route/handler).
Response body is `UpdateInfo` JSON; the only change is one added field:

```jsonc
{
  "current_version": "2.7.0",
  "latest_version": "2.8.0",
  "update_available": true,
  "assets": { "linux-amd64": "https://…/riot-agent-linux-amd64" },
  "checksum_url": "https://…/checksums.txt",
  "signature_url": "https://…/checksums.txt.sig"   // NEW, omitempty
}
```

`signature_url` present only when `update_available` and a `checksums.txt.sig` asset exists.
Absent/empty ⇒ refuse (AD-004 step 2). It is a *locator*; authenticity is decided only by the
embedded key (FR-006/BR-001).

**`cmd/riot-signer`** (build-time CLI, not shipped): `genkey`; `sign <in> <out>` (reads
`AGENT_SIGNING_PRIVATE_KEY`); `verify <in> <sig>` (against `internal/releasekey.PublicKeyBase64`).

**`/usr/local/lib/riot/riot-agent-apply`** (root-owned, installed): no-arg apply mode
(stop→verify→write→start, invoked via `sudo systemd-run --unit=riot-agent-update <path>`);
`--boot` mode (verify→write, invoked by the unit `ExecStartPre`). No argument affects the trust
anchor or the install target (both compiled-in).

---

## 7. Sequence / Flow

**Successful update (non-dev, enabled, signed release, verifier present):**

1. `checkAndUpdate` → `CheckForUpdate`; server returns `update_available:true` + `assets`,
   `checksum_url`, `signature_url`. AD-010 guard: `latest_version` strictly newer → continue.
2. Emit `EventAgentUpdateStarted`. `performUpdate`: download binary to `stagingPath`, pre-flight
   SHA-256.
3. Both URLs non-empty → `fetchBytes(checksum_url, 1 MiB)` + `fetchBytes(signature_url, 1 MiB)`
   (200, size-capped).
4. Pre-flight `ed25519.Verify` + exact-basename hash match → ok (else refuse, no root path).
5. Stage `checksums.txt`→`…update.checksums`, `.sig`→`…update.sig`.
6. `sudo systemctl reset-failed riot-agent-update`; `sudo systemd-run --unit=riot-agent-update
   /usr/local/lib/riot/riot-agent-apply`; keep staged files.
7. **Verifier (root, detached scope):** `sleep 3` → `systemctl stop riot-agent` →
   `verifyAndInstall` (O_NOFOLLOW reads of the three staged files into memory → `ed25519.Verify`
   over checksums → exact-basename expected hash → sha256 of in-memory binary matches → atomic
   write to `/usr/local/bin/riot-agent` → chmod 0755 → remove staged files) → `systemctl start
   riot-agent`.
8. Agent emits `EventAgentUpdateCompleted`, `restartSelf()`; the service comes up on the new
   binary.

**Refusal (any FR-008 condition, agent pre-flight):** steps 3–4 `return error` →
`checkAndUpdate` logs `slog.Error` + `EventAgentUpdateFailed`; `defer` removes staging; no `sudo`
runs; agent keeps serving.

**Refusal (privileged verifier):** a `riot`-planted/unsigned/symlinked input fails
`verifyAndInstall` → the verifier restores the service (`systemctl start`), removes staging,
exits non-zero; `/usr/local/bin/riot-agent` is never written.

**Fallback (verifier present, `systemd-run` unavailable, or installer not re-run):** staged
files kept; on next service start `ExecStartPre=-+… --boot` runs the verifier, which re-verifies
and installs (or, on failure, removes staging and lets the current agent start — never blocks
boot).

---

## 8. Acceptance Criteria Mapping

`UNIT` = automated Go test. `MANUAL` = verified on a Linux host / in CI by QA.

| AC | Fulfilled By | Test Strategy |
|---|---|---|
| AC-001 | AD-004 steps 6–7 pre-flight + AD-005 (d)(e) authoritative | UNIT `updater_verify_test.go` (tampered binary → refuse, no sudo, staging removed, event) + `cmd/riot-agent-apply/main_test.go` (hash mismatch → no write) |
| AC-002 | AD-004 step 2 (`signature_url` empty → refuse); AD-003 | UNIT: empty `signature_url` → refuse; agent stays on version |
| AC-003 | AD-004 step 4 (`fetchBytes` non-200/transport) | UNIT: `.sig` 404 / reset → refuse, staging removed |
| AC-004 | AD-004 step 3 + removal of advisory block (FR-012) | UNIT: `checksums.txt` non-200/timeout → refuse even with a valid `.sig`; no warn-then-install |
| AC-005 | AD-004 pre-flight pass + AD-006 primary + AD-005 authoritative | UNIT: `systemd-run` argv asserted; `verifyAndInstall` writes a temp target from a valid triple. MANUAL: real host end-to-end restart |
| AC-006 | AD-004 step 5 + AD-005 (c) | UNIT: altered `checksums.txt` / wrong-key `.sig` → refuse in both the agent and the verifier |
| AC-007 | AD-002 (key only from `releasekey`); FR-006 | UNIT: server-supplied URLs never feed key selection; verify takes only the embedded key, in both agent and verifier |
| AC-008 | AD-005 §4 sudoers rewrite | MANUAL: `sudo -l` shows no `sh -c`/shell/wildcard; `visudo -cf` passes |
| AC-009 | AD-005 §1/§6 verifier at fixed path, `root:root 0755`; §2 five fixed actions | MANUAL: `stat` the verifier; code review of `verifyAndInstall` (stop/verify/write/chmod/rm/start, nothing else) |
| AC-010 | AD-006 (only `systemd-run <verifier>`; no `sh -c`) | UNIT: the only `sudo` argv is `systemd-run --unit=riot-agent-update <applyBinPath>` (+`reset-failed`); grep for `sh -c` absent |
| AC-011 | AD-005 §7 / AD-006 fallback (verifier `--boot`) | MANUAL: stage a verified triple, restart service → comes up on new version with no sudoers rule |
| AC-012 | AD-007 present-path | UNIT: probe argv equals the AD-005 rule. MANUAL: `doctor` on a re-installed host reports present |
| AC-013 | AD-007 missing-path | MANUAL: remove the rule/verifier → `doctor` reports missing + "re-run install script" |
| AC-014 | AD-005 §4 (other rules untouched) | MANUAL: diff `/etc/sudoers.d/riot-agent` — only the two update rules change |
| AC-015 | AD-008 (`dev` short-circuit) | UNIT: dev build returns before any download/verify |
| AC-016 | AD-008 (`AutoUpdate` semantics) | UNIT: `nil` → enabled; explicit `false` → suppressed |
| AC-017 | AD-009 (`sign`+`verify`) + AD-001/AD-002 | UNIT `cmd/riot-signer/main_test.go` (`verify` passes for the matching key, fails on tamper/bad constant). MANUAL: release attaches both files; key in no log/asset |
| AC-018 | all of the above | MANUAL/CI: `go build ./...` + `go test ./...` green incl. new tool, verifier, releasekey, tests |
| AC-019 | AD-004 refusal path (defer-remove + single event/log); AutoUpdate loop continues | UNIT: each refusal removes staging, emits one failed event; loop unaffected |
| AC-020 | AD-005 (d)(e)(f): verify in memory, then write those bytes; live binary never written pre-verify | UNIT `cmd/riot-agent-apply/main_test.go`: on sig/hash failure the target file is never created/modified |
| SEC-001 | AD-005 (compiled verifier, O_NOFOLLOW, regular-file, in-memory verify-then-write, hardcoded target, root-owned binary + target) | UNIT `cmd/riot-agent-apply/main_test.go` (arbitrary bytes → refuse; symlink source → refuse; direct-invocation cannot install unsigned bytes). MANUAL: as a `riot` shell, stage arbitrary bytes + invoke the rule → no install; `stat` shows `RIOT_BIN` root-owned |
| SEC-002 | AD-004 `fetchBytes(limit)` + AD-005 `io.LimitReader` | UNIT: an oversized `checksums.txt`/`.sig` body → refuse (agent and verifier) |
| SEC-003 | AD-009 `riot-signer verify` CI gate | UNIT `cmd/riot-signer/main_test.go`; CI step fails on mismatch / bad constant |
| SEC-004 | AD-010 agent-side guard + §10 accurate residual | UNIT: server advertising an older `latest_version` → refuse; §10 states the unsigned-version limitation |
| SEC-005 | AD-004 / AD-005 exact-basename match | UNIT `parseExpectedChecksum` and the verifier reject a substring-only line, select `riot-agent-<suffix>` exactly |
| SEC-006 | AD-005 (e): authoritative hash over in-memory installed bytes | UNIT: verifier re-hashes its in-memory bytes (not a stream digest); documented in §7 step 7 |

---

## 9. Error Handling

- **Agent verification failures** return an `error` from `performUpdate`; `checkAndUpdate`
  (updater.go:90–95) logs `slog.Error` + emits `EventAgentUpdateFailed`, then `return`s — the
  agent keeps running (FR-009/NFR-005/NFR-006). Messages name the class (missing URL, oversized
  body, signature invalid, checksum fetch failed, checksum mismatch, no entry).
- **No partial install:** a refusal never sets `staged = true`; `defer` removes the binary
  staging file and no `sudo` runs (FR-010/AC-019). (The agent removes only `stagingPath` on
  pre-flight failure; the `.checksums`/`.sig` files are written only *after* pre-flight passes, at
  step 8, and are cleaned by the verifier.)
- **Privileged verifier failures** (apply mode): restore the service (`systemctl start`), remove
  all staged files, exit non-zero. (`--boot` mode): remove staged files, exit 0 so the current
  agent still starts (NFR-005).
- **Per-fetch timeout + size cap:** `fetchBytes` uses a 30 s timeout and a 1 MiB
  `io.LimitReader`; a timeout or oversize is a refusal (NFR-007/SEC-002).
- **Malformed embedded key:** `releasekey.PublicKey()` errors (bad base64 / wrong length) →
  verification refuses in agent, verifier, and the CI `verify` gate (SEC-003).
- **CI signing/verify failure:** `riot-signer sign`/`verify` exit non-zero, failing the release
  rather than publishing an unsigned or mis-keyed release (FR-001/FR-003/SEC-003).

---

## 10. Security Considerations

- **Trust anchor is compile-time (FR-005/NFR-001):** `releasekey.PublicKeyBase64` is a `var`
  overridable only at build time; decode+length-checked so a tampered/empty/placeholder value
  fails closed. Shared by the agent and the privileged verifier (AD-002).
- **End-to-end authenticity enforced on the privileged side (SEC-001):** the verifier re-verifies
  the ed25519 signature and re-hashes the exact in-memory bytes it writes, over `O_NOFOLLOW`
  regular-file inputs, to a hardcoded root-owned target. A `riot` compromise invoking the standing
  sudo rule directly cannot install unsigned bytes (no private key to sign its `checksums.txt`),
  cannot read root files via a symlinked source (regular-file check), and cannot redirect the
  target (hardcoded). `RIOT_BIN` and `/usr/local/bin` are root-owned, so `riot` has no direct
  overwrite path either. NFR-003/BR-003/FR-013/AC-020 hold.
- **No arbitrary-root primitive (FR-017/NFR-003/NFR-004):** the `sh -c *` rule is gone; the one
  update rule names a fixed binary with no arguments and no wildcard; argv-exact matching
  precludes `systemd-run` property injection.
- **Self-stop integrity (AD-005 §3):** the swap runs in a detached `systemd-run` scope, so
  stopping `riot-agent` cannot kill it mid-write.
- **DoS hardening (SEC-002):** the two in-memory metadata reads are size-capped on both the agent
  and the verifier; the binary download is bounded by the staging write and a verifier read cap.
- **CI key correspondence (SEC-003):** the release fails unless the produced `.sig` verifies
  against the committed embedded key, preventing a silent fleet-wide update outage from a
  key/secret drift.
- **Residual — signed downgrade/rollback (NFR-009/SEC-004/§9 item 1):** version is **absent from
  the signed material** (`checksums.txt` filenames carry no version). The AD-010 agent-side guard
  rejects a server that *honestly* advertises a non-newer `latest_version`, but **cannot** stop a
  MITM/forged server that advertises a high `latest_version` while serving an older, genuinely
  signed binary — the agent cannot cryptographically learn the real version of the bytes. Robust
  anti-rollback requires a signed version signal (version in filenames or a signed manifest line)
  and is recorded as a follow-up (AD-010). Blast radius: downgrade to any previously-released,
  still-signed, known-weaker agent. Accepted residual for this story.
- **Residual — transitional window (BR-005/AD-008):** until the first signed release built by the
  new pipeline *and* applied by a verifier embedding the matching key, agents refuse all updates —
  a safe fail-closed availability trade.

---

## 11. Performance Considerations

- Two extra small, size-capped HTTP GETs per *update-available* cycle (≤1 MiB each, ≤30 s each);
  no new work on the common "up to date" path.
- `ed25519.Verify` over a few-KB manifest is sub-millisecond, done twice (agent pre-flight +
  verifier) — negligible.
- The verifier reads the staged binary once into memory (bounded by `maxBinarySize`); write is a
  single `fsync`+`rename`.
- No new indexing, rate limiting, or DB access; the server change is one string field in the
  existing asset loop.

---

## 12. Implementation Notes for Engineers

- `releasekey.PublicKeyBase64` **must be a `var`, not a `const`**; the `-X` target is
  `github.com/DesyncTheThird/rIOt/internal/releasekey.PublicKeyBase64`. Agent and verifier both
  import this package so they embed the identical key.
- Sign/verify over the **raw bytes** of `checksums.txt`; never re-serialize. The verifier's
  authoritative hash is over the **in-memory staged binary bytes it is about to write**, not a
  stream digest (SEC-006).
- The verifier must `open(O_NOFOLLOW)` and `fstat`-check `mode&S_IFMT == S_IFREG` for *every*
  staged input and for the target temp file; follow the write with `fsync`+atomic `rename` inside
  the root-owned `/usr/local/bin`.
- `targetBin` is a hardcoded `const` and `targetAssetName` is a build-time `-X` value — **neither
  is ever taken from argv or the environment**. The only CLI flag is `--boot`, used solely by the
  unit's `ExecStartPre` (no sudoers entry).
- Match the exact asset basename (`parts[1] == "riot-agent-"+suffix`), not `strings.Contains`
  (SEC-005), in both `parseExpectedChecksum` and the verifier.
- `fetchBytes` and the verifier's metadata reads use `io.LimitReader` with the 1 MiB cap and
  refuse on overflow (SEC-002).
- `applyBinPath`, `updateUnit`, `stagingPath`, `stagingChecksumsPath`, `stagingSigPath`, the paths
  baked into the verifier, the sudoers rule, and the unit `ExecStartPre` must match byte-for-byte
  (V-005). The Go consts are the source of truth.
- `install.sh`: `mkdir -p /usr/local/lib/riot`; download `riot-agent-apply-${SUFFIX}` the same way
  as the agent; `chown root:root` + `chmod 0755` the verifier; install `$RIOT_BIN` `root:root`
  (delete the `chown riot:riot "$RIOT_BIN"`); combine the `ExecStartPre` prefixes exactly as
  `-+` (ignore-failure + full-privilege). Keep the `visudo -cf` gate.
- Remove `fetchExpectedChecksum` and the old `sh -c` strategies entirely — no dead paths
  (FR-015/FR-018).
- `cmd/riot-signer` must print nothing derived from the private key; read it only from the env.
- The verifier is **not** auto-updated; a stale verifier with the same key is fine. Key rotation
  requires a deliberate `install.sh` re-run across the fleet (AD-002 §4, AD-005 §6) — document
  this for operators.
- Do not touch `agent-heal-dpkg` / PR #1 or any non-update sudoers rule (D-6).
- Per MEMORY.md: this is a code change, so the merge must be tagged to trigger a release — and
  that first signed release re-enables fleet auto-update.

---

## 13. Definition of Done

- [ ] Every file in §4 created/modified exactly as specified; nothing in §4.3 touched.
- [ ] `internal/releasekey` present; `PublicKeyBase64` is a `var`; `PublicKey()` length-checks to
      32 bytes and fails closed on bad/placeholder input; agent and verifier both import it.
- [ ] `cmd/riot-agent-apply` verifier implemented: `O_NOFOLLOW` + regular-file checks on all
      staged inputs and the target; in-memory verify-then-write; hardcoded `targetBin`;
      exact-basename match; apply + `--boot` modes; restores the service on apply-mode failure;
      `--boot` never blocks start. Unit-tested for SEC-001/AC-020 (arbitrary bytes, symlink
      source, tamper → no write).
- [ ] `performUpdate` follows the AD-004 order, size-caps both metadata fetches (SEC-002), stages
      the signed `checksums.txt`/`.sig`, matches the exact basename (SEC-005), and invokes only
      `systemd-run <verifier>`; the advisory block, `fetchExpectedChecksum`, and all `sh -c`
      strategies are gone.
- [ ] AD-010 downgrade guard present (refuses a non-newer `latest_version`); §10 states the
      unsigned-version limitation.
- [ ] `signature_url` added to both contracts; server populates it; `checker_test.go` asserts it.
- [ ] `install.sh` downloads+installs the verifier `root:root 0755` at the fixed path; installs
      `$RIOT_BIN` `root:root`; the sudoers drop-in has the single fixed `systemd-run` rule +
      `reset-failed`, no `sh -c`/shell/wildcard; other rules unchanged; `ExecStartPre` is
      `-+/usr/local/lib/riot/riot-agent-apply --boot`; `visudo -cf` passes.
- [ ] `doctor` shows an "Agent Update" section probing the exact `systemd-run` rule and checking
      the verifier presence/mode, advising a re-install when missing.
- [ ] `cmd/riot-signer genkey`/`sign`/`verify` implemented and unit-tested; `release.yml`
      cross-compiles `riot-agent-apply-*`, signs after `sha256sum`, runs the `verify` gate
      (SEC-003), and publishes `checksums.txt.sig`; `AGENT_SIGNING_PRIVATE_KEY` provisioned.
- [ ] `go build ./...` and `go test ./...` green (`-race` on Linux/CI; Windows skips `-race`);
      `go vet ./...` clean.
- [ ] MANUAL on a Linux host: re-run `install.sh`; `sudo -l` shows no arbitrary-exec rule
      (AC-008); as a `riot` shell, staging arbitrary bytes + invoking the rule yields no install
      and a symlinked source is refused (SEC-001); `RIOT_BIN` and the verifier are root-owned
      (AC-009); `doctor` reports present (AC-012); a signed release applies end-to-end (AC-005)
      and the `--boot` fallback works with the rule removed (AC-011).
- [ ] MANUAL in CI: a tagged build attaches `checksums.txt`, `checksums.txt.sig`, and
      `riot-agent-apply-*`; the `verify` gate passes; the private key appears in no log/asset
      (AC-017/SEC-003).
- [ ] Operator-facing note (BR-005/A-5) handed to the technical writer: agents will not
      auto-update until the first signed release ships; existing hosts must re-run `install.sh`
      (to get the verifier, root-owned binary, tightened rule, and new `ExecStartPre`); key
      rotation requires a fleet-wide `install.sh` re-run.
- [ ] Merge is tagged so a release is cut (MEMORY.md release rule).
