# Documentation Report

**Story ID:** AGENT-HARDEN-001
**Author:** Technical Writer Agent
**Date:** 2026-10-15

---

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | Added a top-level "Agent Auto-Update and Release Signing" section (placed after mTLS Device Authentication, before Single Sign-On). It covers: the five-step verification flow; a table of refusal messages operators will see in `agent_update_failed` events; what the `riot` user can and cannot do as root (the exact two update sudoers lines); how to disable auto-update (`agent.auto_update: false`); an "Upgrading existing agents" subsection (re-run `install.sh`; what it changes; what is true until it is re-run); known limitations (signed rollback, Linux-only, manual key rotation); a maintainer overview of the signing pipeline; and a "Rotating the signing key" runbook. Added a Features bullet. Updated the agent install "This will:" list (verifier download, `root:root` binary). Added the `doctor` "Agent Update" check to Agent Diagnostics. Added a Manual Install note (no verifier or sudoers rule is created). **Corrected the Sudoers Rules table**: removed the two `sh -c` update rows, added the fixed `systemd-run ... riot-agent-apply` row. Expanded the `agent.auto_update` config-reference row. Added a verifier build command to Building from Source. Updated the `GET /api/v1/update/check` row in the API table for `signature_url`. Extended the Releasing section with the signing/verifier build steps and the **pre-release runbook** (below). |
| `README.md` > Releasing > "One-time setup: release signing (required before the next tag)" | **Pre-release runbook (created)** | Five numbered steps: (1) `go run ./cmd/riot-signer genkey`; (2) paste `PUBLIC_KEY` over the `REPLACE_WITH_BASE64_ED25519_PUBLIC_KEY` placeholder in `internal/releasekey/releasekey.go` and commit; (3) set repo secret `AGENT_SIGNING_PRIVATE_KEY`; (4) store the private key offline, with loss and compromise consequences; (5) tag, then confirm the `Sign checksums` and `Verify signature against embedded key` steps pass and `checksums.txt.sig` plus five `riot-agent-apply-*` assets attach. Includes an optional local sign/verify check that mirrors the CI gate. Put in the README (not a separate `docs/` file) because the Releasing section is where a maintainer looks before tagging; it is cross-referenced from the auto-update section. |
| `CHANGELOG.md` | Updated | Added `[AGENT-HARDEN-001]` entries at the top of `[Unreleased]` under `### Added` (signed releases and `riot-signer`; compile-time trust anchor; `riot-agent-apply` verifier; `signature_url`; `doctor` Agent Update check), `### Fixed` (fail-open checksum; arbitrary-root `sh -c *` sudoers primitive), and `### Changed` (two **BREAKING:** entries: fail-closed verification with the operator-visible effect on unsigned releases, and the sudoers/ownership/unit change requiring an installer re-run; installer warn-and-continue when no verifier is published; best-effort downgrade guard). No version number was invented; entries sit in the existing `[Unreleased]` section in the same `- [STORY-ID] ...` style as OIDC-001. |
| `internal/agent/updater.go` | Inline comments | Added a doc-comment paragraph on `applyStaged` explaining a non-obvious behavior: it returns no error in either the scheduled or the staged-fallback case, the caller then reports "completed" and exits, and a verifier refusal surfaces in the journal rather than as an `agent_update_failed` event. No code changed. |
| `scripts/install.sh` | Inline comments | Added a shell comment (outside the unit-file heredoc, so it is not written into the unit) explaining the `ExecStartPre=-+` prefix combination: `+` runs as root despite `User=riot`, `-` ignores failure or a missing verifier. No logic changed; `bash -n` passes on the LF-normalised script. |
| `docs/technical-writing/AGENT-HARDEN-001-docs-report.md` | Created | This report. |

## Artifacts Reviewed, Not Changed

| File | Reason |
|------|--------|
| `internal/releasekey/releasekey.go`, `cmd/riot-signer/main.go`, `cmd/riot-agent-apply/main.go`, `internal/agent/doctor.go`, `internal/server/updates/checker.go`, `.github/workflows/release.yml` | Read in full. The security-critical paths (read-once/verify-in-memory/write-same-bytes, `O_NOFOLLOW` plus regular-file checks, hardcoded target, `--boot` never blocking startup, the AD-010 downgrade-guard limits, `isAgentBinaryAsset`, the workflow signing/verify steps) already carry accurate "why" comments tied to AD/SEC/C numbers. No restating comments found. No additions needed. |
| `.env.example` | No new server or agent environment variables. The only new configuration is the CI secret `AGENT_SIGNING_PRIVATE_KEY`, documented in the README. |
| `CONTRIBUTING.md` | Does not exist in this repository (same call as OIDC-001 and FLEET-DASH). The story adds no new contributor workflow beyond the maintainer runbook, which lives in the README Releasing section. |
| OpenAPI / GraphQL schema | None exist. The README API table is the endpoint reference; its `GET /api/v1/update/check` row was updated (see above) for the new `signature_url` field. |

---

## Operator-Facing Changes (summary)

1. **Updates are signed and verified, fail-closed.** Hardened agents install a release only if `checksums.txt.sig` verifies against the public key compiled into the agent and the binary's SHA-256 matches the signed entry. Everything else is a refusal that leaves the current version running and raises `agent_update_failed`.
2. **Releases published without a signature are refused**, including every release before this change. One error event per 6-hour cycle. Dev builds still never update.
3. **No arbitrary-root sudo rule for update.** The `sh -c *` and `sh -c mv` rules are gone. The one update rule runs the root-owned verifier `/usr/local/lib/riot/riot-agent-apply` with fixed arguments.
4. **The installed agent binary and the verifier are now `root:root`.** The service unit's `ExecStartPre` now runs the verifier with `--boot`.
5. **Existing hosts must re-run `install.sh`.** Until they do, the previous sudoers rules and unit stay in place and the hardening is not in effect on that host. `riot-agent doctor` reports the gap.
6. **Auto-update stays on by default.** `agent.auto_update: false` disables it (including the dashboard-triggered update).
7. **The maintainer must complete one-time key setup before the next tag** (README > Releasing).

---

## Maintainer Checklist (carried forward from the QA report's Manual / Deferred items)

Do these in order. Items 1 to 3 gate the release; items 4 to 8 are verification the QA engineer could not run from a Windows dev box.

- [ ] **1. Generate the keypair and commit the public key.** `go run ./cmd/riot-signer genkey`; paste `PUBLIC_KEY` over the placeholder in `internal/releasekey/releasekey.go`; commit. (Required. Until then every tag push fails the `Sign` and `Verify` steps by design.)
- [ ] **2. Set the secret.** Repository secret `AGENT_SIGNING_PRIVATE_KEY` = the `PRIVATE_KEY` value.
- [ ] **3. Store the private key offline.** Loss means no future signed releases. Leak means attackers can sign malicious updates. Rotation steps are in the README.
- [ ] **4. Real-tag run (AC-017).** Tag per the project release rule (this is a code change, so it must be tagged). Confirm `checksums.txt`, `checksums.txt.sig`, and all five `riot-agent-apply-*` assets attach, the `Verify signature against embedded key` step passes, and the private key appears in no log or asset.
- [ ] **5. Confirm the Linux-gated verifier tests are green on the ubuntu `-race` CI job** (`cmd/riot-agent-apply`). QA ran them natively under WSL as uid 1000, without `-race`.
- [ ] **6. `riot`-shell privilege exercise on one re-installed host (C2 / SEC-001).** (i) Stage arbitrary unsigned bytes and run the sudo rule: nothing installs. (ii) Make `/var/lib/riot/riot-agent.update` a symlink: refused. (iii) `stat` shows `/usr/local/bin/riot-agent` and `/usr/local/lib/riot/riot-agent-apply` are `root:root` and not `riot`-writable. (iv) `sudo -l` for `riot` shows no `sh -c`, shell, or wildcard **update** rule (AC-008; the `apt-mark hold *` / `unhold *` rules legitimately remain), and `visudo -cf /etc/sudoers.d/riot-agent` passes.
- [ ] **7. systemd behavior.** On a real unit: `ExecStartPre=-+... --boot` lets a host with no verifier still start; a staged valid triple is applied on restart with the sudo rule removed (AC-011); `riot-agent doctor` reports present and missing correctly (AC-012, AC-013); watch the restart-versus-3-second-delay race noted by QA (after `systemd-run` succeeds, the agent exits and restarts after `RestartSec=5` while the verifier may still be working).
- [ ] **8. Re-install the fleet.** Re-run `install.sh` on every Linux host (verifier, root-owned binary, tightened rule, new unit). Re-run `riot-agent doctor` on each and confirm the Agent Update section passes.
- [ ] **9. (Follow-up backlog, not a release blocker.)** Open a story for a signed version signal to close the signed-downgrade residual (NFR-009 / SEC-004).

---

## Inline Comment Assessment

The engineering and QA work left the security-critical code unusually well commented. Two genuine gaps were filled (see "Artifacts Updated"). No comments were deleted: none restated the code, none were dated notes, and no commented-out code was found in the changed files.

---

## Stale Content Found (Not Fixed)

Items found outside the scope of this story that need a documentation pass:

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` line 18 | `> **README last updated for v2.38.0**` marker predates OIDC-001 and this story. Carried forward unchanged from the OIDC-001 and FLEET-DASH reports. | Update at the next release tag or remove the marker. |
| `README.md` Sudoers Rules table, `sudo systemctl poweroff` row | `scripts/install.sh` writes no `poweroff` rule (`grep poweroff` finds nothing), and the row's command carries a `sudo` prefix unlike every other row. The documented rule does not match the installer. | Determine how remote shutdown gains privilege, then correct or delete the row in an isolated pass. |
| `README.md` Sudoers Rules intro | States "no shell access, no wildcard escalation", but the `apt-mark hold *` / `unhold *` rules contain a wildcard (argument-validated agent-side). Narrowly true for updates now; still imprecise for the file as a whole. | Reword to "no shell access" and describe the `apt-mark` wildcard explicitly. |
| `scripts/uninstall.sh` | Does not remove `/usr/local/lib/riot/` (new verifier) and has never removed `/etc/sudoers.d/riot-agent`. After uninstall a root-owned verifier and a sudoers drop-in referencing it remain. Not a regression in privilege (the `riot` user is also not removed here), but it leaves residue. | Add removal of both in a small installer-maintenance story. Not fixed: shell logic is out of scope for a docs-only pass. |
| `Makefile` `build-agent-all` | Does not build `riot-agent-apply` (also noted by the implementation report). The README now documents the manual build command. | Add a Makefile target in a dedicated story. |
| `README.md` line 12 | "relatively stable at version 2.24" banner is old. | Review wording at next release. |
| `docs/technical-writing/` | OIDC-001 report's two carried-forward items (FLEET-DASH security review cookie text, `Makefile` `migrate-up`) are unchanged and still open. | See the OIDC-001 report. |

---

## Accuracy Flags

Discrepancies between the pipeline documents and what the code does, found while reading:

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| **ADD AD-002 section 4 key-rotation procedure cannot work with the as-built CI gate.** The ADD says to cut a "transitional release" signed with the old private key whose binaries embed the new public key. The release workflow's `riot-signer verify` step checks the signature against the key committed in the same commit (no `-X` override). A release signed by the old key with the new key committed fails that gate. | `docs/architecture/AGENT-HARDEN-001-add.md` AD-002 section 4 vs `.github/workflows/release.yml` ("Verify signature against embedded key") | README "Rotating the signing key" documents the procedure that does work: change the committed public key and the secret together, tag, then re-run the installer on every host. Hosts still embedding the old key refuse the new release with `signature invalid` until re-installed. **The maintainer should decide whether to keep this or change the workflow to support a transitional release.** |
| ADD and implementation report say that on a host that has not re-run the installer, updates "fall back to staged-for-restart, which no-ops without a verifier". That is true only after the new unit is installed. A host that has not re-run `install.sh` still has the **old** unit, whose `ExecStartPre` is an unverified root `cp` of the staged file, and the old `sh -c *` sudoers rules. The new agent's pre-flight check still gates what it stages, but the old privileged apply path remains. | `docs/architecture/AGENT-HARDEN-001-add.md` AD-005 Consequences; `docs/implementation/AGENT-HARDEN-001-impl-report.md` Notes for QA; old unit text in `git show HEAD:scripts/install.sh` line 506 | README and CHANGELOG state only the verified fact: until the installer is re-run, the host keeps its previous sudoers rules and unit, so the hardening is not in effect. Neither document claims the fallback no-ops. |
| FRD Section 2.2 and ADD "Transition (BR-005)" imply fleets stop updating until the first signed release. In practice an agent built before this change keeps using its old, unverified path and will update to the first signed release normally. Only hardened agents (built from this change) refuse unsigned releases, and the first hardened agent is itself a signed release. | FRD BR-005 / A-5; ADD AD-008 | README and CHANGELOG describe the refusal of unsigned releases and the need to re-run the installer. They do not say the fleet "will not auto-update until a signed release ships", because older agents will. **The requester's brief used that phrasing; it is accurate only for hardened agents.** |
| FRD D-4 describes the helper as a shell script `riot-agent-apply-update`; the built component is a compiled Go verifier `riot-agent-apply`. This is the documented, security-approved ADD amendment, not an error. | FRD D-4 vs ADD AD-005 | Documented as the compiled verifier. |

---

## Notes for Future Writers

**Where the signing documentation lives.** There is no separate signing document. The operator-facing material is the README section "Agent Auto-Update and Release Signing". The maintainer runbook is in the README under Releasing ("One-time setup: release signing"). If key rotation or a signed version signal ships, update both.

**Do not describe the sudoers rule as "locked down" without the ownership caveat.** The rule is safe only because the verifier and `/usr/local/bin/riot-agent` are `root:root` and the verifier authenticates the bytes it installs. A future story that makes either file `riot`-writable breaks the model. State both properties together.

**Two statements must stay precise.** (1) The hardening takes effect per host only after the installer is re-run. (2) Hardened agents refuse unsigned releases; older agents do not. Prior drafts blurred both.

**Rollback residual.** Anything that says "protected against downgrade" is wrong. The AD-010 guard only stops a server that honestly advertises an older version.

**CHANGELOG order.** New entries go at the top of each subsection. Block order below the top is not strictly chronological (see the OIDC-001 report).

**Line endings.** `README.md`, `CHANGELOG.md`, and `scripts/install.sh` are CRLF in this Windows checkout (`core.autocrlf=true`); the index is LF. Edits preserved the existing endings. New Go files written by engineering are LF; git normalises on commit.

**Verified commands.** `go run ./cmd/riot-signer genkey`, `sign`, and `verify` were run locally with a throwaway key (discarded). Observed: `genkey` prints `PUBLIC_KEY=` (44 characters) and `PRIVATE_KEY=` (88 characters); `sign` writes an 88-byte single-line signature; `verify` exits 1 with `committed release public key is unusable` against the placeholder, and exits 0 with `OK: ... verifies against the committed release public key` against a matching key. `go build ./...` and `go vet ./internal/agent/` pass after the comment edits. The installer was checked with `bash -n`. Steps that need GitHub (setting the secret, a real tag run) or a Linux/systemd host were not run and are in the maintainer checklist.
