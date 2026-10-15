# QA Report

**Story ID:** AGENT-HARDEN-001
**QA Engineer:** QA Agent
**Date:** 2026-10-15
**Verdict:** PASS WITH NOTES

---

## Test Run Summary

All numbers were reproduced independently on this machine (Windows 11, Go toolchain go1.26; CI pins 1.24), not copied from the implementation report.

- **Build / vet (Windows):** `go build ./...` clean. `go vet ./...` clean.
- **Build / vet (Linux target):** `GOOS=linux GOARCH=amd64 go build ./...` clean. `GOOS=linux GOARCH=amd64 go vet ./...` clean (this includes the `//go:build linux` verifier test file).
- **Go test (Windows, no `-race`; needs CGO):** `go test -count=1 ./...` gives 19 packages `ok`, 0 failing. `-v` count: **1160 `--- PASS`, 0 `--- FAIL`, 2 `--- SKIP`**. The two skips are the pre-existing Linux-only security collector tests (`TestAC004/005_SecurityCollector_*`), unrelated to this story. The baseline before QA additions was 1155 PASS, which matches the implementation report; QA added 5 (2 top-level, 3 subtests).
- **Flaky tests:** none. The full suite was run twice back to back with identical results (1160 / 0 / 2 both times).
- **Linux-gated verifier tests (`cmd/riot-agent-apply/main_test.go`):** these report `[no test files]` on Windows, as expected. QA cross-compiled them with `GOOS=linux GOARCH=amd64 go test -c ./cmd/riot-agent-apply` (compiles clean) and, because WSL was available (Ubuntu 18.04, running as a non-root uid 1000), **executed the binary natively: 21 top-level + 37 subtests PASS, 0 FAIL, exit 0** (rerun after QA's changes, and again under three separate mutation builds, see below). This is NOT a substitute for the ubuntu CI job with `-race`; that run remains to be confirmed (see Manual / Deferred).
- **Coverage percentage:** not measured (no coverage tool in the house flow for this repo).
- **Suspicious output:** none. The `ECONNREFUSED` noise seen in other OIDC-001 runs does not apply here (Go only).

### Mutation probes (security-critical paths)

Each probe temporarily weakened production code, was run against the relevant tests, then **reverted byte-for-byte** (backups taken first; SHA-256 of `releasekey.go`, `cmd/riot-agent-apply/main.go`, `updater.go` verified identical to the originals after the last probe).

| # | Mutation | Target | Result | Tests that went RED |
|---|----------|--------|--------|---------------------|
| A | `ed25519.Verify` made to always pass (`if false && !ed25519.Verify(...)` in `releasekey.VerifySignature`) | AC-006 / AC-007 / SEC-003, **all three consumers** (agent pre-flight, privileged verifier, CI gate) | CAUGHT in every consumer | Agent: `TestAC001_AC006_PerformUpdate_RefusalsFailClosed/{AC-006 checksums altered after signing, AC-006 signed by a key other than the embedded one}`, `TestAC007_PerformUpdate_SelfConsistentAttackerReleaseIsRefused`. Verifier (WSL): `TestSEC001_VerifyAndInstall_ArbitraryUnsignedBytesAreRefused/attacker_self-signed...`, `TestAC006_VerifyAndInstall_SignatureProblemsAreRefused/{checksums altered..., signature from a different key}`, `TestNFR005_RunBoot_BadStagedUpdateNeverBlocksStartup`. Signer: `TestSEC003_Verify_FailsOnMismatch/{tampered signature, tampered checksums, committed key differs from the signing key}` |
| B | Dropped `O_NOFOLLOW`/`O_NONBLOCK` from the open flags **and** the `IsRegular()` fstat check in `openRegularNoFollow` | C2 / SEC-001 symlink arbitrary-read, FIFO hang | CAUGHT | `TestC2_VerifyAndInstall_NonRegularOrSymlinkedInputsAreRefused/{binary,checksums,signature}/symlink_to_the_genuine_file_elsewhere`, `TestC2_VerifyAndInstall_FIFOInputIsRefusedWithoutBlocking` |
| C | Verifier re-reads the staging binary from disk after verification and writes that instead of the verified in-memory bytes (the exact "tidy-up" the security review warned about) | C1 read-once / verify-in-memory / write-same-bytes | CAUGHT | `TestC1_VerifyAndInstall_PostVerificationStagingChangeCannotAlterInstalledBytes/{staging file overwritten in place, ...replaced by a symlink to a secret, ...deleted}` |

Observation on probe B: the "symlink to a secret" and "directory" replacement rows are refused even without `O_NOFOLLOW` (the secret fails the hash, a directory fails the read), so only the "symlink to the genuine file" row and the FIFO row are the true discriminators. They exist and fire, so the control is genuinely tested. The `IsRegular()` check on its own is behaviourally redundant with the hash check for non-FIFO cases; it is defence in depth, not a gap.

---

## AC Coverage Audit

Legend: PASS = tests that would fail if the AC were broken (confirmed by reading bodies, and for the critical ones by mutation). DEFERRED-to-CI = needs the real GitHub Actions run. MANUAL = needs a real Linux/systemd host (the ADD's own section 8 marks these MANUAL).

| AC ID | Status | Tests / Evidence | Gap Description |
|-------|--------|------------------|-----------------|
| AC-001 | PASS | `updater_verify_test.go`: `TestAC001_AC006_PerformUpdate_RefusalsFailClosed/AC-001 tampered binary` (asserts error class "checksum mismatch", **zero sudo/runner calls**, all three staging files removed); `main_test.go`: `TestAC001_VerifyAndInstall_TamperedBinaryIsRefused` (target bytes unchanged, no temp debris). **QA added** `TestAC001_AC019_UpdateCycle_TamperedBinaryEndToEndIsRefusedWithoutPrivilege` (updater_verify_test.go:813) to prove the full chain through the real cycle (exactly 1 failed event, 0 completed, 0 restart, 0 sudo, no staging) | The existing tampered test stopped at `performUpdate` and the cycle tests stub `performUpdate` out, so the "event + no privilege" chain was only proven by composition. Gap closed by QA |
| AC-002 | PASS | `TestAC002_AC004_PerformUpdate_MissingLocatorsAreRefused` (no `signature_url`); `TestAC002_UpdateCycle_UnsignedReleaseEndToEndIsASafeNoOp` (1 failed event, no restart, no sudo, no staging); `checker_test.go:TestAC002_AgentUpdateInfo_UnsignedReleaseHasNoSignatureURL`, `...AdvertisesSignatureURL` | "Release publishes `checksums.txt` but no `.sig`" is modelled as empty `signature_url`, which is exactly how the server advertises it |
| AC-003 | PASS | Refusal table rows: sig 404, sig 403; `TestAC003_AC004_PerformUpdate_TransportErrorsAreRefused/AC-003 signature host unreachable`; all assert error class, no sudo, staging removed | A hung (never-answering) server is not tested; bounded by the 30 s `http.Client.Timeout` and `TestNFR007_FetchTimeout_...` asserts the constant. Acceptable |
| AC-004 | PASS | Refusal table rows: checksums 404 / 500 **with a valid .sig present**; missing `checksum_url`; unreachable host. Legacy advisory block is gone: `fetchExpectedChecksum` no longer exists (grep clean) and `fetchSignedChecksums` returns an error on every failure | — |
| AC-005 | PASS (+ MANUAL e2e) | `TestAC005_AC010_PerformUpdate_ValidReleaseInvokesOnlyTheFixedVerifier` (exact argv equality of both sudo calls); `TestAC005_PerformUpdate_StagesTheSignedMaterialForTheVerifier`; `TestAC005_UpdateCycle_SignedReleaseEndToEndAppliesAndRestarts`; verifier: `TestAC005_VerifyAndInstall_ValidSignatureAndMatchingHashInstalls` (bytes + mode 0755 + no temp left), `TestAC005_RunApply_StopsInstallsAndStartsService` (stop, then start, with the 3 s delay) | Real swap/restart on systemd is MANUAL |
| AC-006 | PASS | Agent refusal table: malformed base64, empty, 63-byte, zeroed 64-byte (ed25519 verify false), altered-after-signing, other-key. Verifier: `TestAC006_VerifyAndInstall_SignatureProblemsAreRefused` (5 rows). `releasekey_test.go:TestAC006_VerifySignature_FailClosedTable`. **Mutation probe A confirms these fail when Verify is neutered** | — |
| AC-007 | PASS | `TestAC007_PerformUpdate_SelfConsistentAttackerReleaseIsRefused` (server controls binary + checksums + sig + URLs, only the embedded key decides); `TestAC007_NewUpdatePipeline_UsesOnlyTheCompiledInReleaseKey` (key comes from `releasekey.PublicKeyBase64` only); empty / garbage / placeholder key rows in agent and verifier (`TestAC007_VerifyAndInstall_UnusableEmbeddedKeyFailsClosed`, 4 rows incl. nil-key-nil-error); `releasekey_test.go:TestAC007_PublicKey_*`. Code review: `releasekey.PublicKey()` reads only the package `var`; grep confirms no config/env/network read of a verification key | — |
| AC-008 | PASS (static) / MANUAL (`sudo -l` on a host) | Static: see "Sudoers Static Verification" below. `sh -c *` rule and `sh -c mv` rule are gone; only update rule is the fixed `systemd-run ... riot-agent-apply` line; `apt-mark hold *`/`unhold *` legitimately remain (FR-020) | `sudo -l` on a real re-installed host is MANUAL |
| AC-009 | PASS (static) / MANUAL (`stat`) | `install.sh` creates `/usr/local/lib/riot` `root:root 0755`, installs verifier `chown root:root`, `chmod 0755`, atomic `.new` + `mv`. Verifier code review: actions are stop, verify, write temp + `fsync` + `chmod 0755` + rename, remove staging, start. Form amended per ADD AD-005 (compiled binary, not shell script) | `stat` on a host is MANUAL |
| AC-010 | PASS | Argv equality in `...ValidReleaseInvokesOnlyTheFixedVerifier` plus per-arg assertions: no `sh`, no `-c`, no `*`. Grep: no `sh -c` anywhere in the update path (`device_probes.go` shell use is the unrelated remote-command feature) | — |
| AC-011 | PASS (unit) / MANUAL (restart e2e) | `TestAC011_PerformUpdate_VerifierUnavailableLeavesVerifiedStagingForBoot` (sudo failure is not an update failure, staged triple kept, no shell); verifier `TestAC011_RunBoot_NoStagedUpdateIsANoOp`, `...AppliesAValidStagedUpdateWithoutServiceControl`, `TestNFR005_RunBoot_BadStagedUpdateNeverBlocksStartup` (exit 0, staging removed). `ExecStartPre=-+/usr/local/lib/riot/riot-agent-apply --boot` confirmed in `install.sh:544` | Real `ExecStartPre` `-+` semantics on systemd are MANUAL |
| AC-012 | PASS | `doctor_test.go:TestAC012_UpdatePrivilegeProbe_TargetsTheFixedVerifierRule` (probe argv built from the same `updateUnit`/`applyBinPath` constants the agent invokes), `TestAC012_ReportUpdatePrivilege_PresentReportsPass` | Real `sudo -n -l` on a host MANUAL |
| AC-013 | PASS | `TestAC013_ReportUpdatePrivilege_MissingRuleFailsWithReinstallGuidance`, `..._MissingHelperFails`, `..._WrongHelperModeWarns` | — |
| AC-014 | PASS (static) / MANUAL (host diff) | `git diff -U0 scripts/install.sh` shows exactly 2 NOPASSWD lines removed and 1 added. apt/dnf/`apt-mark`/`install`/`rm`/`reboot`/`reset-failed`/nginx/smartctl rules are byte-identical | — |
| AC-015 | PASS | `TestAC015_UpdateCycle_DevBuildNeverChecksOrUpdates` (zero server hits, zero apply calls) | — |
| AC-016 | PASS | `TestAC016_UpdateCycle_AutoUpdateDefaultOnAndExplicitFalseSuppresses` (nil / true / false rows); `config.go` is unmodified (git status) | — |
| AC-017 | PASS (unit) / DEFERRED-to-CI (real run) | `cmd/riot-signer/main_test.go` (7 tests): round trip, no key echo, bad key source, tamper/mismatch/bad-committed-key, genkey, usage. QA ran the real tool end to end (see Release Workflow). `release.yml` has `Sign checksums` (secret via step `env:` only), `Verify signature against embedded key`, and `dist/checksums.txt.sig` in `files:` | Real tag run and "key in no log/asset" are CI/MANUAL. **Placeholder key makes a tag push FAIL by design until the maintainer commits the real key** |
| AC-018 | PASS | Windows build/vet/test and Linux build/vet green; Linux-gated tests executed under WSL | `-race` on the ubuntu runner is DEFERRED-to-CI |
| AC-019 | PASS | `TestAC019_UpdateCycle_RefusalEmitsExactlyOneFailedEventAndNeverRestarts`; every refusal row asserts staging removed; QA's end-to-end test proves it through the real pipeline. Pre-flight failures call `discardStaging` via `defer` | — |
| AC-020 | PASS | Verifier: every refusal test calls `assertRefused` which asserts the live target bytes are unchanged **and** no temp file debris. Agent never opens the live binary path at all (grep of `updater.go`: no `os.Executable`, no write to the install path). Mutation C confirms the "same bytes" property | — |

---

## Security Review Conditions

| Cond | Status | Evidence |
|------|--------|----------|
| C1 read-once / verify-in-memory / write-same-bytes | PASS | `verifyAndInstall` reads checksums, sig, then binary once each into memory and passes the verified slice to `writeTargetAtomically`; no path is re-opened after verification. `TestC1_..._PostVerificationStagingChangeCannotAlterInstalledBytes` mutates staging (overwrite, symlink-swap, delete, checksums swap) via the test seam between verify and write and asserts installed bytes are unchanged. **Mutation C made it RED.** The seam `betweenVerifyAndWrite` is nil in production |
| C2 `O_NOFOLLOW` + `S_ISREG` on every input; hardcoded target | PASS (unit) / MANUAL (riot-shell) | `openRegularNoFollow` used for all three inputs (3 inputs x 4 replacements tested, plus FIFO). Temp file opened `O_EXCL\|O_NOFOLLOW`, `fstat` regular check, atomic rename. `targetBin` is a `const`; `runMode` accepts only no args or `--boot` (`TestSEC001_RunMode_ArgumentsCannotRedirectTargetOrKey`, 5 rows). **Mutation B made it RED.** The riot-shell privilege exercise (stage unsigned bytes + invoke the real sudo rule; symlinked `riot-agent.update`; `stat` ownership) is MANUAL |
| C3 bounded binary read | PASS | `TestC3_..._OversizedInputsAreRefused` (binary, checksums, sig; asserts the error names the limit), `...BinaryAtExactLimitIsAccepted` (boundary), `TestC3_Limits_*` (cap 128 MiB, metadata 1 MiB). Both an `info.Size()` pre-check and a `LimitReader(limit+1)` guard exist (TOCTOU growth) |
| C4 non-Linux asset names | PASS | `releaseAssetName` appends `.exe` for `windows-*`; `TestC4_ReleaseAssetName_MatchesPublishedAssets`, windows row in `TestSEC005_ParseExpectedChecksum_ExactAssetNames`. Windows/macOS agents still cannot complete an update (Linux-only verifier), which is not a regression |
| C5 `-X` targets resolve; committed key real | DEFERRED-to-CI / MANUAL | Workflow greps each built verifier for its baked `riot-agent-<suffix>` string (fails the build if the `-X` did not take; QA confirmed the literal does not occur elsewhere in the verifier source, so the gate is meaningful). The committed key is still the **placeholder** `REPLACE_WITH_BASE64_ED25519_PUBLIC_KEY` (`releasekey.go:31`) |
| C6 vestigial `ReadWritePaths=...${RIOT_BIN}` | n/a | Left untouched, which the review allowed (`install.sh:553`). Cosmetic only |

Also verified: SEC-002 (agent `TestSEC002_FetchBytes_EnforcesStatusAndSizeCap` at exact cap, cap+1, empty, non-200; oversized checksums/sig rows; verifier oversize rows), SEC-003 (signer `verify` fails on placeholder, mismatched key, tampered input; QA ran it live, see below), SEC-004 (`TestSEC004_IsNewerVersion_FailsClosed` 15 rows, cycle test, plus QA's manual-path test), SEC-005 (exact basename in `releasekey`, agent, and verifier; duplicate entries rejected as ambiguous), SEC-006 (hash is over the in-memory bytes that are written).

---

## Sudoers Static Verification (no Linux host available)

`bash -n` passes on `scripts/install.sh` after stripping CR (working tree is CRLF under `core.autocrlf=true`; the index is LF, so this is a Windows checkout artefact, not a script defect; first line is `#!/usr/bin/env bash`).

| Check | Result | Evidence |
|-------|--------|----------|
| (a) `sh -c *` update wildcard rule gone | PASS | Removed in `git diff`; `grep -nE 'sh -c\|SH_PATH'` over the LF-normalised script finds only a pre-existing comment at line 499 ("No sh -c, no tee...") |
| (b) `${SH_PATH} -c mv ...` rule gone | PASS | Removed; `SH_PATH` variable itself deleted, no remaining reference |
| (c) only update rule is the fixed `systemd-run --unit=riot-agent-update /usr/local/lib/riot/riot-agent-apply` | PASS | `install.sh:511`, plus the unchanged `reset-failed riot-agent-update` at 512. No `*` in either |
| (d) `RIOT_BIN` and verifier `root:root` | PASS | `chown root:root "$RIOT_BIN"` (line 246; `chown riot:riot` removed); `/usr/local/lib/riot` dir and the verifier `.new` file are `chown root:root`, `chmod 0755` |
| (e) other rules untouched | PASS | The only NOPASSWD lines in the diff are the 2 removed + 1 added. apt/dnf/`apt-mark hold *`+`unhold *`/`install`/`rm`/`reboot`/nginx/smartctl are unchanged, so the only wildcards left in the drop-in are the PATCH-GATE `apt-mark hold *`/`unhold *`. **AC-008's "no wildcard" must be read as scoped to update-related rules** (FR-020 forbids touching those) |
| (f) `ExecStartPre` runs the verifier `--boot` | PASS | `install.sh:544` `ExecStartPre=-+${RIOT_APPLY_BIN} --boot` (replaces the raw `cp`) |
| `visudo -cf` gate retained | PASS | Not in the diff, so unchanged |

V-005 byte-for-byte path match: `/usr/local/lib/riot/riot-agent-apply`, `riot-agent-update`, and the three `/var/lib/riot/riot-agent.update*` staging paths are identical in `install.sh`, `updater.go`, `doctor.go` (via the same constants), and `cmd/riot-agent-apply/main.go` (pinned by `TestV005_*` in both packages).

## Release Workflow Static + Live Verification

- **Verifier matrix:** five Linux targets build `riot-agent-apply-<suffix>` with `-X main.targetAssetName=riot-agent-<suffix>` plus the grep gate. Darwin/Windows get no verifier (correct).
- **Signing step:** present, secret only via `env:` of the one step; `riot-signer` never prints key-derived data (`TestAC017_Sign_NeverPrintsKeyMaterial`).
- **CI correspondence gate:** present, runs `go run ./cmd/riot-signer verify` using the committed key (no `-X`), so it checks the key the shipped binaries embed. QA ran the real tool locally:
  - `sign` with no `AGENT_SIGNING_PRIVATE_KEY`: exit 1 ("is not set").
  - `sign` with a generated key then `verify` against the **committed placeholder**: exit 1 (`committed release public key is unusable`), i.e. **a tag push today fails this gate**.
  - `verify` with a matching key via `-X`: exit 0. With a different key: exit 1. Produced `.sig` is 88 bytes, single line, no newline (base64 of 64 bytes), as AD-001 requires.
- **Assets:** `dist/checksums.txt.sig` is listed explicitly; the verifier binaries ride the existing `dist/riot-agent-*` glob (impl deviation 11; a duplicate explicit line would risk a double upload, so this is acceptable). `sha256sum *` runs before signing and covers the verifier binaries, so they are covered by the signature.
- **Flagged, not a defect:** the placeholder key means the first tag push will fail at `Sign` (no secret) and `Verify` (placeholder) until the maintainer commits the real key and sets `AGENT_SIGNING_PRIVATE_KEY`. This is the intended SEC-003/C5 gate working, and is a **required pre-release manual step**.

## Server Bonus Fix Spot-Check: `isAgentBinaryAsset`

Real defect found and fixed by the implementer: the old rule `Contains(name, suffix) && !HasSuffix(".txt")` also matched the new `riot-agent-apply-linux-amd64` asset, so depending on GitHub's asset order the server could have advertised the *verifier* as the agent download. `checker_test.go:TestAC005_AgentUpdateInfo_VerifierAssetIsNeverServedAsTheAgentBinary` exercises **both asset orderings** (verifier first, agent first) and asserts the agent URL wins in each. It would fail if the `HasPrefix(verifierAssetPrefix)` exclusion were removed (verifier-first ordering). `checksums.txt.sig` cannot match the binary rule (no platform suffix). No other consumer of `release.Assets` exists (grep clean). PASS.

---

## Test Quality Findings

1. **Behaviour, not implementation (good).** Refusal tests assert observable outcomes: error class, runner call log (zero sudo), staging files on disk, target file bytes, no temp debris, emitted events. The `recordingRunner` asserts exact argv, so mock drift in the sudo contract would be caught by `TestV005_*` and the argv-equality test.
2. **Fail-closed coverage is thorough.** Every refusal listed in the task is genuinely asserted: tampered binary, missing sig URL, missing checksum URL, non-200 (both fetches, several codes), transport error, malformed/short/empty/zeroed signature, wrong key, altered checksums, suffix not found, substring-only entry, duplicate entry, empty/garbage/placeholder/nil key, oversized bodies at cap+1 (and exact cap accepted), verifier read-once, symlink / directory / dangling / FIFO inputs, oversized staging, hardcoded target, rejected argv.
3. **Test seam in production struct** (`installer.betweenVerifyAndWrite`): nil in production and necessary for a deterministic C1 test. Acceptable; documented by the implementer.
4. **Minor gaps (non-blocking, documentation-only):**
   - No test for a *hung* server (relies on `http.Client.Timeout`). The constant is asserted, the behaviour is stdlib.
   - Verifier success path under a *non-root* uid only (WSL ran as uid 1000). Nothing in the tests depends on root, but `chown`/ownership behaviour of the real install is MANUAL.
   - `handleTriggerUpdate` (manual `trigger_update` command path) is not unit-testable end to end because it calls `performUpdate` directly (not injected). QA covered the guard portion (below). The `performUpdate` call there only differs by the threaded `SignatureURL`, which is compile-enforced.
5. **Isolation:** no shared mutable state. `TestAC007_NewUpdatePipeline_UsesOnlyTheCompiledInReleaseKey` mutates the package `var releasekey.PublicKeyBase64` but restores it in `t.Cleanup`; no `t.Parallel()` in the file, so no cross-test leakage. Two full runs were identical.

## Adversarial Findings

- **Direct invocation by a compromised `riot` of the standing sudo rule:** with unsigned / attacker-signed / unsigned-manifest / planted-symlink / FIFO / oversized inputs the verifier refuses and the live binary is untouched (unit-proven; real-sudo exercise MANUAL).
- **Post-verification swap of staging:** cannot change installed bytes (C1, mutation-proven).
- **Between-file swap** (swap the binary after the checksums were verified): safe, because the binary is read afterwards and hashed against the already-verified entry; no trust is carried across a re-read.
- **Property injection via `systemd-run`:** sudo matches the argv exactly with no wildcard, so extra `--property`/`-p ExecStartPost=` is not authorised. Static only; no host to confirm.
- **Server-supplied key/URL substitution:** server controls only locators; a self-consistent attacker release is refused (AC-007).
- **Pre-signing release / old server:** `signature_url` empty => refusal, no destructive effect, agent keeps running (BR-005). Intended.
- **Rollback (documented residual, not a defect):** a genuinely signed older release can still be served by a server that lies about `latest_version`; the AD-010 guard only stops an honest-but-old server. Stated accurately in the ADD (NFR-009 / SEC-004). Follow-up story for a signed version signal is recorded in the ADD, not yet in a backlog item.
- **Local `riot` can bounce the agent** by repeatedly invoking the rule (stop, refuse, start). Accepted by the security review (riot can already kill its own process).
- **Race worth watching live (from impl report):** after `systemd-run` succeeds the agent exits and systemd restarts it after `RestartSec=5`, while the verifier's 3 s delay + stop may still be in flight. Both paths verify and rename atomically with identical bytes, so the loser harmlessly fails. Not reproducible on Windows; MANUAL observation.
- **No new vulnerability or critical finding discovered.**

## Tests Added by QA

| File | Lines | Covers |
|------|-------|--------|
| `internal/agent/updater_verify_test.go` | 806-837, `TestAC001_AC019_UpdateCycle_TamperedBinaryEndToEndIsRefusedWithoutPrivilege` | AC-001 + AC-019 end to end: signed release, tampered binary, real pipeline + real cycle => exactly 1 `EventAgentUpdateFailed`, 0 completed, 0 restart, 0 sudo/runner calls, staging removed. Passes green |
| `internal/agent/updater_verify_test.go` | 839-857, `TestSEC004_TriggerUpdate_NonNewerAdvertisedVersionIsRefusedBeforeAnyDownload` (3 subtests: equal, older, garbage) | SEC-004 / AD-010 on the **manual** `handleTriggerUpdate` path in `commands.go`, which the implementer changed but left untested: asserts `"error"`/"refusing update" and that no `EventAgentUpdateStarted` is emitted (no download begins). Passes green |

Both are tagged `Added by QA Engineer`, follow the file's naming convention, and are gofmt-clean. No implementation code was changed. All three mutation probes were reverted (hash-verified).

## Deviations from ADD

All implementer-recorded deviations were reviewed; none weakens an AC, NFR, or security condition.

1. `releasekey` exports the shared encoding contract (`VerifySignature`, `SignChecksums`, `ExpectedChecksum`, `GenerateKeyPair`, sentinels) beyond the ADD's two symbols. Beneficial: one implementation across agent, verifier, and signer prevents drift.
2. Dependency injection (`updatePipeline`, `runUpdateCheck`) rather than the ADD's bare functions. Improves testability; the `performUpdate` signature is preserved.
3. `checker.go` `isAgentBinaryAsset` fix is outside the ADD but fixes a real order-dependent bug (verified above).
4. `commands.go` (not in ADD 4.2) is a required call site, and also gets the AD-010 guard.
5. `targetAssetName` defaults to `""` (fail closed) instead of the ADD's `riot-agent-linux-amd64`. Safer.
6. `maxBinarySize` 128 MiB vs "e.g. 256 MiB". Within C3's intent; ~6x headroom over the ~20 MiB agent.
7. `install.sh` treats a missing `riot-agent-apply-<suffix>` asset as WARN-and-continue so pinned old-version installs still work. Consequence: the sudoers rule can reference a verifier that does not exist; `doctor` reports it missing (AC-013) and updates fall back to a no-op `--boot`. Acceptable and documented.
8. `release.yml` does not add a separate `dist/riot-agent-apply-*` line (glob already matches). Acceptable.
9. Extra hardening beyond the ADD (`O_NONBLOCK`, duplicate-entry rejection, non-SHA-256 digest rejection, workflow `-X` grep gate) is all test-covered.

## Deviations from FRD

None found. D-4's "shell script helper" is amended to a compiled verifier by ADD AD-005 (security-approved, recorded). FR-015 ("must not write any sudoers rule ... containing a `*` wildcard ... as a means of applying an update") is satisfied; the retained `apt-mark hold *`/`unhold *` are package-manager rules protected by FR-020/D-6, not update rules.

---

## Manual / Deferred Items (NOT code defects)

These cannot be verified from a Windows dev box and are expected hand-offs, not a reason to fail.

1. **Commit the real release public key.** Run `go run ./cmd/riot-signer genkey`, put `PUBLIC_KEY=` into `internal/releasekey/releasekey.go` (currently the placeholder), and store `PRIVATE_KEY=` as the GitHub Actions secret `AGENT_SIGNING_PRIVATE_KEY`. Until then a tag push fails the `Sign`/`Verify` steps by design. **Required pre-release step.**
2. **Real-tag release run:** confirm `checksums.txt`, `checksums.txt.sig`, and all five `riot-agent-apply-*` assets are attached, the `Verify signature against embedded key` gate passes, and the private key appears in no log/asset (AC-017).
3. **Confirm the Linux-gated verifier tests are green on the ubuntu `-race` CI job.** QA executed them natively under WSL (21 + 37 subtests, uid 1000, no `-race`).
4. **riot-shell privilege exercise on a re-installed host (C2 / SEC-001):** (i) stage arbitrary unsigned bytes and run the sudo rule: nothing installs; (ii) symlink `/var/lib/riot/riot-agent.update`: refused; (iii) `stat` shows `/usr/local/bin/riot-agent` and `/usr/local/lib/riot/riot-agent-apply` are `root:root` and not riot-writable; (iv) `sudo -l` for `riot` shows no `sh -c`/shell/wildcard update rule (AC-008); `visudo -cf` passes.
5. **systemd behaviour:** `ExecStartPre=-+... --boot` (the `-` and `+` prefix combination) on a real unit: a host with no verifier still starts; a staged valid triple is applied on restart with the sudo rule removed (AC-011); `doctor` reports present/missing correctly on a real host (AC-012/013); the restart-versus-3 s-delay race noted above.
6. **Existing hosts must re-run `install.sh`** (verifier, root-owned binary, tightened rule, new `ExecStartPre`); key rotation likewise needs a fleet-wide re-run. Operator-facing README/CHANGELOG wording (BR-005/A-5) belongs to the technical writer.
7. **Per project release rule:** this is a code change, so the merge must be tagged to cut a release, and that first signed release is what re-enables fleet auto-update.

## Verdict Rationale

**PASS WITH NOTES.**

- **Zero MISSING ACs.** All 20 ACs map to tests that would fail if the AC broke; the three load-bearing security properties (signature enforcement in all three consumers, O_NOFOLLOW/regular-file input handling, read-once verify-then-write-same-bytes) were each proven by a mutation probe that turned specific tests RED, then reverted. QA closed two small coverage gaps itself (end-to-end tampered binary, manual-path downgrade guard).
- **All tests green:** Windows 1160 PASS / 0 FAIL / 2 pre-existing SKIP (stable across two runs); Linux build and vet green; Linux-gated verifier tests 21 + 37 PASS under WSL.
- **No FRD deviation, no critical or high adversarial finding.** SEC-001..006 and conditions C1-C4 are satisfied in code and tests; C5 and the live privilege checks are deferred to CI / a real host by the ADD's own design.
- **Notes (non-blocking):** (1) the release public key is still the placeholder, so the first tag push fails by design until the maintainer commits the real key and sets the secret; (2) the Linux-gated verifier tests have been run on WSL but not yet on the CI runner with `-race`; (3) the MANUAL host exercises above (sudo -l, riot-shell, systemd `ExecStartPre`, doctor on a host) remain outstanding and are required before relying on the privilege boundary in production; (4) the signed-downgrade residual (NFR-009) remains open by design pending a signed version signal.

## Action Required (if FAIL)

Not applicable. Verdict is PASS WITH NOTES. Recommended non-blocking follow-ups:

1. Maintainer: generate the keypair, commit the public key into `internal/releasekey/releasekey.go`, set `AGENT_SIGNING_PRIVATE_KEY`, then tag.
2. Confirm the ubuntu `-race` CI job is green for `cmd/riot-agent-apply`.
3. Run the MANUAL host exercises (item 4 and 5 above) on one re-installed device before enabling the update for the fleet.
4. Open a backlog story for a signed version signal (closes the NFR-009 / SEC-004 downgrade residual).
