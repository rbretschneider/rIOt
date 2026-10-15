# Implementation Report

- **Story ID:** AGENT-HARDEN-001
- **Title:** Cryptographically-verified, fail-closed agent auto-update + least-privilege update sudoers rule
- **Engineer:** Senior Dev Agent
- **Date:** 2026-10-15
- **Stack:** Go 1.24 module (toolchain go1.26.0 locally; CI pins 1.24) — agent daemon, chi/pgx server, stdlib `crypto/ed25519`; testify for tests; bash installer; GitHub Actions release workflow. No frontend change.

---

## Pre-Implementation Baseline

`go build ./...` and `go test ./...` were green on the worktree before any change (all packages `ok`). ADD Revision 2 (FINAL) and the security re-review (APPROVE WITH CONDITIONS, C1–C6) were read in full first. No blockers were raised; no `AGENT-HARDEN-001-blockers.md` exists.

**Environment notes.**
- The `standards` skill is not invocable as a tool in this environment; its `STANDARDS.md` was read directly and applied (braced control flow, named constants, injected dependencies, one-concern functions, error context via `%w`, AC-named tests). The standards file has no Go-specific section, so existing repo patterns (testify, table tests, `collectors.CommandRunner`) were followed.
- Not committed, not tagged, not pushed. `agent-heal-dpkg` / PR #1 untouched.
- Working tree is CRLF on Windows (`core.autocrlf=true`, index is LF). New files were written LF; git normalises on commit.

---

## Completed Components

### New files

| File | Action | Notes |
|---|---|---|
| `internal/releasekey/releasekey.go` | CREATE | `var PublicKeyBase64` (placeholder, see Notes), `PublicKey()`, `ParsePublicKey`, plus the one shared implementation of the AD-001 encoding contract: `VerifySignature`, `SignChecksums`, `ExpectedChecksum` (exact-basename), `GenerateKeyPair`. Sentinel errors name each failure class. |
| `internal/releasekey/releasekey_test.go` | CREATE | Key decode fail-closed table, signature table, exact-basename table. |
| `cmd/riot-signer/main.go` | CREATE | `genkey`, `sign <in> <out>` (key only from `AGENT_SIGNING_PRIVATE_KEY`), `verify <in> <sig>` (against `releasekey.PublicKey()`); never prints key-derived data. |
| `cmd/riot-signer/main_test.go` | CREATE | Round trip, tamper/mismatch/bad-committed-key failures, no key echo, usage. |
| `cmd/riot-agent-apply/main.go` | CREATE | `//go:build linux`. Privileged verifier: `verifyAndInstall` (O_NOFOLLOW + O_NONBLOCK open, fstat regular-file, size caps, read-once into memory, ed25519 verify, exact-basename hash, hash of the in-memory bytes, write the same bytes to a hardcoded target via O_EXCL+O_NOFOLLOW temp + fsync + chmod 0755 + atomic rename, then remove staging). Apply mode (`sleep 3 → stop → verify/install → start`, always restarts the service) and `--boot` mode (never blocks startup). Only accepted args: none or `--boot`. |
| `cmd/riot-agent-apply/main_other.go` | CREATE | `//go:build !linux` stub so `go build ./...` stays green on Windows/macOS. |
| `cmd/riot-agent-apply/main_test.go` | CREATE | `//go:build linux`. SEC-001/C1/C2/C3/AC-001/005/006/007/011/NFR-005 coverage (see table). |
| `internal/agent/updater_verify_test.go` | CREATE | httptest-driven pipeline tests (refusal table, locators, transport errors, valid path argv, staging, fallback, key-substitution, `fetchBytes` cap, exact-basename, downgrade guard, default-on/dev/disabled, event emission, end-to-end unsigned/signed cycles). |

### Modified files

| File | Action | Notes |
|---|---|---|
| `internal/agent/updater.go` | MODIFY | `performUpdate(ctx, downloadURL, checksumURL, signatureURL, suffix)` rewritten as the AD-004 fail-closed ordered pipeline (`updatePipeline.run`): download+stream hash → both locators required → `fetchBytes` (1 MiB `LimitReader`, HTTP 200 only, oversize = error) for checksums and sig → pre-flight `ed25519` verify → exact-basename parse → hash compare → stage binary + checksums + sig → `sudo systemctl reset-failed` then `sudo systemd-run --unit=riot-agent-update /usr/local/lib/riot/riot-agent-apply`. **Both `sh -c` strategies and `fetchExpectedChecksum` are deleted.** Fallback leaves the verified triple for the unit's `ExecStartPre --boot`. AD-010 downgrade guard (`isNewerVersion`) with the limit documented in a code comment. |
| `internal/agent/http_client.go` | MODIFY | `SignatureURL` on `UpdateCheckResponse`. |
| `internal/agent/commands.go` | MODIFY | Manual-update call site passes `resp.SignatureURL`; AD-010 guard added on that path (see Deviations). |
| `internal/agent/doctor.go` | MODIFY | "Agent Update" section: `sudo -n -l <systemd-run> --unit=riot-agent-update /usr/local/lib/riot/riot-agent-apply` probe + verifier presence/mode check; called on Linux when auto-update is effectively enabled. |
| `internal/server/updates/checker.go` | MODIFY | `SignatureURL` on `UpdateInfo`, populated from the `checksums.txt.sig` asset; verifier assets excluded from agent-binary selection (see Deviations #3). |
| `internal/server/updates/checker_test.go` | MODIFY | SignatureURL assertions, unsigned-release case, verifier-asset-ordering regression. |
| `internal/agent/doctor_test.go` | MODIFY | AC-012/AC-013 probe and report tests (see Deviations #5). |
| `scripts/install.sh` | MODIFY | Downloads `riot-agent-apply-<suffix>` to `/usr/local/lib/riot/riot-agent-apply` (`root:root 0755`, atomic via `.new` + `mv`); `$RIOT_BIN` now `root:root` (`chown riot:riot` removed); sudoers: both `sh -c` rules deleted, replaced by exactly `riot ALL=(root) NOPASSWD: <systemd-run> --unit=riot-agent-update /usr/local/lib/riot/riot-agent-apply`; `reset-failed` and every package-manager/reboot/smartctl/nginx rule untouched; unit `ExecStartPre=-+/usr/local/lib/riot/riot-agent-apply --boot`; `visudo -cf` gate kept; now-unused `SH_PATH` removed. |
| `.github/workflows/release.yml` | MODIFY | Builds `riot-agent-apply-<suffix>` for the five Linux targets with `-X main.targetAssetName=riot-agent-<suffix>` plus a grep gate proving the `-X` took effect (C5b); `Sign checksums` step (secret via `env:` only); `Verify signature against embedded key` gate; `dist/checksums.txt.sig` added to `files:`. |

---

## Deviations from the ADD

None weakens an AC, NFR, or security condition. All are recorded for architect review.

1. **`internal/releasekey` exports more than `PublicKeyBase64`/`PublicKey()`** (`ParsePublicKey`, `VerifySignature`, `SignChecksums`, `ExpectedChecksum`, `GenerateKeyPair`, sentinel errors). Reason: AD-001 requires the agent pre-flight, the verifier, and the signer to share one encoding contract; implementing it three times is a DRY/drift risk on the security-critical path. The ADD-named agent helpers (`parseExpectedChecksum`, `verifyReleaseSignature`) are kept as thin wrappers over it. The package is still a stdlib-only leaf.
2. **Dependency injection in `updater.go`** (standards: DI required for testing). `verifyReleaseSignature` is a method on a new `updatePipeline` struct (injected key source, staging paths, command runner) rather than a bare function with the embedded key hard-wired; `checkAndUpdate` now delegates to `runUpdateCheck(ctx, apply, restart)` so tests can drive the cycle without `os.Exit`. Production wiring uses `releasekey.PublicKey`, the ADD's constant paths, and `Agent.runner` (existing test seam) or `exec`. `performUpdate` keeps the ADD signature.
3. **`internal/server/updates/checker.go` asset selection fix (not in ADD).** The existing rule `Contains(name, suffix) && !HasSuffix(".txt")` also matches the new `riot-agent-apply-linux-amd64` asset (it contains the suffix). Depending on GitHub's asset order, the server could advertise the *verifier* binary as the agent download, which the pre-flight would refuse (hash mismatch) and break all updates. Added `isAgentBinaryAsset` which excludes the `riot-agent-apply-` prefix; regression-tested in both asset orders.
4. **`internal/agent/commands.go` is not in ADD §4.2** but is a required call site (signature change). It also gets the AD-010 guard on the manual path so a server cannot use the manual trigger to bypass it. Refusal there returns a command error, consistent with its neighbours.
5. **`internal/agent/doctor_test.go` modified** (not listed in §4.2) to hold the AC-012/AC-013 tests next to the existing doctor tests.
6. **`targetAssetName` default is `""`, not `"riot-agent-linux-amd64"`.** A verifier built without the `-X` then matches no line and fails closed with an explicit error (C5b wording), instead of silently expecting the amd64 asset on another architecture.
7. **`maxBinarySize` = 128 MiB** (ADD said "e.g. 256 MiB"). The linux/amd64 agent is ~20 MiB; 128 MiB leaves ~6x headroom while bounding memory on small devices (C3). Metadata cap is 1 MiB as specified.
8. **Verifier reads the two small signed files first, verifies the signature, then reads the binary.** Still strictly read-once with no re-read after verification (C1); it just avoids loading a large binary for an unsigned/garbage release.
9. **Extra hardening beyond the ADD:** staged inputs are opened with `O_NONBLOCK` so a planted FIFO cannot hang the root verifier (tested); `ExpectedChecksum` rejects duplicate entries (FRD V-002 "exactly one") and non-SHA-256 digests; the release workflow greps each verifier binary for its baked asset name.
10. **C4 handled, not just documented:** `releaseAssetName` appends `.exe` for `windows-*` suffixes so exact-basename matching works for the Windows asset. Auto-update still only completes on Linux (the verifier and staging paths are Linux-only); non-Linux agents are not a regression.
11. **`release.yml` `files:`** — did not add a redundant explicit `dist/riot-agent-apply-*` line. `dist/riot-agent-*` already matches those assets (ADD AD-009 note) and a duplicate glob risks double-upload. A YAML comment above the step records this. Add the line if the architect prefers it explicit.
12. **`install.sh`:** a missing `riot-agent-apply-<suffix>` asset (an old pinned `--version`, or a pre-signing release) is a WARN-and-continue, not a hard failure, so pinned installs of old versions still work; auto-update simply stays unavailable. The duplicated curl/wget block became a `download_file` function (reused for the verifier).
13. **Test seam in production struct:** `installer.betweenVerifyAndWrite` (nil in production) exists solely so the C1 test can mutate staging between verification and write deterministically.
14. **Minor:** the 30 s / 6 h literals in `updateCheckLoop` became named constants.
15. **Functions longer than the 15-line guideline:** `verifyAndInstall` (linear guard sequence, kept in one place deliberately so the C1 ordering is auditable) and the pre-existing `runUpdateCheck` body.

---

## Test Summary

### AC / condition coverage

`Win` = runs in `go test ./...` on this Windows box. `Linux-gated` = `//go:build linux`, compiled with `GOOS=linux` and executed under WSL (Ubuntu 18.04); CI (ubuntu, `-race`) runs it natively.

| AC / Finding | Test File | Tests | Status |
|---|---|---|---|
| AC-001 tampered binary refused, no sudo, staging removed | `internal/agent/updater_verify_test.go` (Win); `cmd/riot-agent-apply/main_test.go` (Linux-gated) | `TestAC001_AC006_PerformUpdate_RefusalsFailClosed/tampered…`; `TestAC001_VerifyAndInstall_TamperedBinaryIsRefused` | PASS |
| AC-002 unsigned release refused | `updater_verify_test.go`; `checker_test.go` | `TestAC002_AC004_PerformUpdate_MissingLocatorsAreRefused`; `TestAC002_UpdateCycle_PassesSignatureURLToPerformUpdate`; `TestAC002_UpdateCycle_UnsignedReleaseEndToEndIsASafeNoOp`; `TestAC002_AgentUpdateInfo_*` (2) | PASS |
| AC-003 missing/non-200 signature | `updater_verify_test.go` | refusal table (404/403 sig), `TestAC003_AC004_PerformUpdate_TransportErrorsAreRefused` | PASS |
| AC-004 missing/non-200 checksums, no warn-then-install | `updater_verify_test.go` | refusal table (404/500), missing `checksum_url`, transport error | PASS |
| AC-005 valid sig + hash installs via fixed verifier | `updater_verify_test.go`; `main_test.go` (Linux-gated) | `TestAC005_AC010_PerformUpdate_ValidReleaseInvokesOnlyTheFixedVerifier`; `…StagesTheSignedMaterial…`; `…ResetFailedFailureIsIgnored`; `TestAC005_UpdateCycle_*` (2); `TestAC005_VerifyAndInstall_ValidSignatureAndMatchingHashInstalls`; `TestAC005_RunApply_StopsInstallsAndStartsService`; `TestAC005_AgentUpdateInfo_VerifierAssetIsNeverServedAsTheAgentBinary` | PASS |
| AC-006 altered checksums / wrong key refused | `updater_verify_test.go`; `main_test.go`; `releasekey_test.go` | refusal table (bad base64, empty, wrong length, zeroed sig, altered, other key); `TestAC006_VerifyAndInstall_SignatureProblemsAreRefused`; `TestAC006_VerifySignature_FailClosedTable` | PASS |
| AC-007 no key substitution / bad embedded key fails closed | `updater_verify_test.go`; `main_test.go`; `releasekey_test.go` | `TestAC007_PerformUpdate_SelfConsistentAttackerReleaseIsRefused`; `TestAC007_NewUpdatePipeline_UsesOnlyTheCompiledInReleaseKey`; empty/garbage/placeholder key rows; `TestAC007_VerifyAndInstall_UnusableEmbeddedKeyFailsClosed`; `TestAC007_PublicKey_*` (2) | PASS |
| AC-008 / AC-009 / AC-014 sudoers & installer | `scripts/install.sh` | `bash -n` + diff review (MANUAL on a host per ADD §8); verifier path/target constants: `TestAC009_ProductionInstaller_TargetIsHardcoded`, `TestV005_*` (2) | MANUAL (UNIT part PASS) |
| AC-010 only fixed `systemd-run <verifier>`, no `sh -c` | `updater_verify_test.go` | argv equality + no `sh`/`-c`/`*` in any arg | PASS |
| AC-011 staged-file restart fallback via `--boot` | `updater_verify_test.go`; `main_test.go` | `TestAC011_PerformUpdate_VerifierUnavailableLeavesVerifiedStagingForBoot`; `TestAC011_RunBoot_*` (2) | PASS (end-to-end restart is MANUAL) |
| AC-012 doctor present | `doctor_test.go` | `TestAC012_UpdatePrivilegeProbe_TargetsTheFixedVerifierRule`; `TestAC012_ReportUpdatePrivilege_PresentReportsPass` | PASS |
| AC-013 doctor missing + guidance | `doctor_test.go` | `TestAC013_ReportUpdatePrivilege_*` (3) | PASS |
| AC-015 dev builds never update | `updater_verify_test.go` | `TestAC015_UpdateCycle_DevBuildNeverChecksOrUpdates` | PASS |
| AC-016 default-on / explicit false | `updater_verify_test.go` | `TestAC016_UpdateCycle_AutoUpdateDefaultOnAndExplicitFalseSuppresses` (3 rows); `TestAC016_RunUpdateCheck_ServerSaysUpToDateDoesNothing` | PASS |
| AC-017 sign + verify round trip, no key in output | `cmd/riot-signer/main_test.go`; `releasekey_test.go` | `TestAC017_*` (6 signer tests + 2 releasekey tests) | PASS |
| AC-018 build/tests green | whole repo | see Test Run Output | PASS |
| AC-019 refusal leaves agent operational, exactly one failed event | `updater_verify_test.go` | `TestAC019_UpdateCycle_RefusalEmitsExactlyOneFailedEventAndNeverRestarts` | PASS |
| AC-020 live binary never written pre-verification | `main_test.go` | every refusal test asserts target bytes unchanged and no temp debris (`assertRefused`) | PASS |
| SEC-001 root-write / symlink-read primitive closed | `main_test.go` | `TestSEC001_VerifyAndInstall_ArbitraryUnsignedBytesAreRefused` (5 rows); `TestSEC001_RunApply_RefusedUpdateStillRestoresServiceAndCleansStaging`; `TestSEC001_RunMode_*` (2) | PASS |
| SEC-002 size caps | `updater_verify_test.go`; `main_test.go` | `TestSEC002_FetchBytes_EnforcesStatusAndSizeCap`; oversized checksums/sig rows; `TestC3_*` | PASS |
| SEC-003 CI key-correspondence gate | `cmd/riot-signer/main_test.go` | `TestSEC003_Verify_FailsOnMismatch` (4 rows); `TestSEC003_Verify_FailsOnUnusableCommittedKey` | PASS (workflow step MANUAL in CI) |
| SEC-004 downgrade guard | `updater_verify_test.go` | `TestSEC004_IsNewerVersion_FailsClosed` (15 rows); `TestSEC004_UpdateCycle_NonNewerAdvertisedVersionIsIgnoredSilently` | PASS |
| SEC-005 exact basename | `releasekey_test.go`; `updater_verify_test.go`; `main_test.go` | `TestSEC005_*` (3) | PASS |
| SEC-006 authoritative hash over installed bytes | `main_test.go` | `requireHashMatch` runs over the in-memory slice later written; C1 test proves the write uses it | PASS |
| **C1** read-once / verify-in-memory / write-same-bytes | `main_test.go` | `TestC1_VerifyAndInstall_PostVerificationStagingChangeCannotAlterInstalledBytes` (overwrite, symlink-swap, delete, checksums swap — installed bytes unchanged in all) | PASS |
| **C2** O_NOFOLLOW + S_ISREG on every input, hardcoded target | `main_test.go` | `TestC2_…NonRegularOrSymlinkedInputsAreRefused` (3 inputs × 4 replacements); `TestC2_…FIFOInputIsRefusedWithoutBlocking`; `TestAC009_ProductionInstaller_TargetIsHardcoded`; `TestSEC001_RunMode_ArgumentsCannotRedirectTargetOrKey` | PASS |
| **C3** bounded binary read | `main_test.go` | `TestC3_VerifyAndInstall_OversizedInputsAreRefused` (3); `…BinaryAtExactLimitIsAccepted`; `TestC3_Limits_*` | PASS |
| **C4** non-Linux asset names | `updater_verify_test.go` | `TestC4_ReleaseAssetName_MatchesPublishedAssets`; windows row in `TestSEC005_ParseExpectedChecksum_ExactAssetNames` | PASS |
| **C5** `-X` targets resolve / committed key real | `release.yml` | grep gate + `riot-signer verify` gate (verified locally that the gate passes with `-X` and fails without, for amd64/armv7/386) | CI-enforced; key is a placeholder (see Notes) |
| **C6** vestigial `ReadWritePaths=…${RIOT_BIN}` | `install.sh` | left untouched, as the review allows | n/a |

Mutation spot-check: with the agent's signature check stubbed out, the AC-006/AC-007 tests failed (10 rows), confirming they detect the regression; restored afterwards.

### Test Run Output

```
go build ./...                       -> OK (windows/amd64)
go vet ./...                         -> OK
GOOS=linux GOARCH=amd64 go build ./...                    -> OK
GOOS=linux GOARCH=amd64 go vet ./...                      -> OK (includes the linux-gated test file)
GOOS=linux GOARCH={386,arm64,arm(GOARM=7),arm(GOARM=6)} go build ./cmd/riot-agent-apply ./cmd/riot-agent -> OK
GOOS=darwin GOARCH=arm64 go build ./...                   -> OK
tr -d '\r' < scripts/install.sh | bash -n                 -> OK

go test -count=1 ./...   (Windows, no -race: needs CGO)
?     cmd/riot-agent                   [no test files]
?     cmd/riot-agent-apply             [no test files]   (build-tagged linux; ran under WSL below)
?     cmd/riot-server                  [no test files]
ok    cmd/riot-signer
ok    internal/agent
ok    internal/agent/collectors
ok    internal/models
ok    internal/releasekey
ok    internal/resilient
ok    internal/server
ok    internal/server/auth
ok    internal/server/ca
ok    internal/server/events
ok    internal/server/handlers
ok    internal/server/middleware
ok    internal/server/notify
ok    internal/server/oidc
ok    internal/server/probes
ok    internal/server/scoring
ok    internal/server/summary
ok    internal/server/updates
ok    internal/server/websocket
(verbose: 1155 "--- PASS" lines incl. subtests, 0 FAIL, 2 SKIP — both SKIPs are pre-existing Linux-only security collector tests)

Linux-native run under WSL (Ubuntu 18.04), compiled with GOOS=linux go test -c:
cmd/riot-agent-apply   PASS  (21 top-level tests / 37 subtests, 0 failures)
internal/agent         PASS  (73 top-level / 87 subtests, 0 failures)
internal/releasekey    PASS
cmd/riot-signer        PASS
internal/server/updates PASS
```

New tests added by this story: `internal/releasekey` 6 top-level / 30 subtests; `cmd/riot-signer` 7 / 7; `cmd/riot-agent-apply` 21 / 37 (Linux-gated); `internal/agent` updater_verify_test.go + 5 doctor tests; `internal/server/updates` +3.

---

## Notes for QA

**Blocking-to-ship items that are not code (maintainer actions).**
- `internal/releasekey.PublicKeyBase64` is still the **placeholder** `REPLACE_WITH_BASE64_ED25519_PUBLIC_KEY`. The design permits this (every verification fails closed). Before the first signed tag the maintainer must: run `go run ./cmd/riot-signer genkey`, commit the `PUBLIC_KEY=` value into `releasekey.go`, and store the `PRIVATE_KEY=` value as the GitHub Actions secret `AGENT_SIGNING_PRIVATE_KEY`. Until then the release workflow's `Verify signature against embedded key` step (and the `Sign` step, with no secret) fails the release by design — that is the C5/SEC-003 gate working.
- No new environment variables for the server/agent. The only new configuration is the CI secret above.

**What to probe.**
- **C2 MANUAL on a re-installed host, as a `riot` shell:** (i) stage arbitrary unsigned bytes and run the sudo rule — nothing may install; (ii) make `/var/lib/riot/riot-agent.update` a symlink — must be refused; (iii) `stat` shows `/usr/local/bin/riot-agent` and `/usr/local/lib/riot/riot-agent-apply` are `root:root` and not riot-writable; (iv) confirm `systemctl status riot-agent` shows `User=riot` running a root-owned binary correctly. The Go tests prove the logic but not the real sudoers/systemd interaction.
- **`sudo -l` for `riot` (AC-008):** the update rules are exactly the fixed `systemd-run … riot-agent-apply` line and `reset-failed`. The pre-existing PATCH-GATE `apt-mark hold *` / `unhold *` rules still contain a `*`; FR-020/D-6 forbid touching them, so scope the AC-008 "no wildcard" check to *update-related* rules.
- **`ExecStartPre=-+…`** prefix combination (`-` ignore failure, `+` full privileges) — confirm on a real systemd that a host with no verifier still starts, and that a staged valid triple is applied on restart with the sudo rule removed (AC-011).
- **Race worth a look:** after `systemd-run` succeeds the agent exits (`restartSelf`) and systemd restarts it after `RestartSec=5`, while the verifier (3 s delay) may still be working. Both paths verify and rename atomically with the same bytes, and the loser harmlessly fails/cleans, but it is the one concurrency interaction worth watching in a live run.
- **Existing hosts** need an `install.sh` re-run to get the verifier, root-owned binary, tightened rule and new `ExecStartPre`; until then `doctor` reports the privilege missing and updates fall back to staged-for-restart (which no-ops without a verifier). Key rotation likewise needs a fleet-wide re-run (the verifier is not auto-updated).
- **Transition (BR-005):** until the first signed release is published, enabled non-dev agents refuse each 6 h cycle with one `agent_update_failed` event and an error log. Expected.
- **Not covered by this change:** `Makefile` `build-agent-all` does not build `riot-agent-apply` (it is not in ADD §4 and is not used by the release); README/CHANGELOG operator notes belong to the technical writer; the downgrade residual (SEC-004) stands until a signed version signal exists.
- Linux-gated verifier tests were executed under WSL here, not on a CI runner; confirm they are green on the ubuntu `-race` job.
