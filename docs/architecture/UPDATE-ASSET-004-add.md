# Architecture — UPDATE-ASSET-004

**Date:** 2026-10-20  
**Stack:** Go 1.24, standard-library string matching, existing testify tests. No dependencies/schema/API changes.

## 1. Root cause and minimal fix

`internal/server/updates/checker.go:isAgentBinaryAsset` currently uses `strings.Contains(name, suffix)` and excludes only `.txt` and `riot-agent-apply-`. Maintenance binaries share the suffix and can overwrite `UpdateInfo.Assets[suffix]` during release iteration. A growing helper denylist would repeat the bug for the next artifact family.

Construct one expected basename: `riot-agent-` + suffix, plus `.exe` only when the suffix begins `windows-`; return exact case-sensitive equality with the GitHub asset Name. Do not clean paths, strip extensions, accept archives, compare substring, infer binary kind from order or add a helper fallback. Preserve the existing `isAgentBinaryAsset(name, suffix)` signature and its call site. Remove the obsolete verifier-only prefix constant/comment if no longer used; retain the strings import because existing version/suffix logic still uses it.

## 2. Component changes

| File | Change |
|---|---|
| `internal/server/updates/checker.go` | Replace broad matcher with exact canonical basename, updating its explanatory comment. All other update-feed behavior unchanged. |
| `internal/server/updates/checker_test.go` | AC-001–004 exact-platform matcher matrix, real naming families/order regression, helper/decoy-only absence and metadata preservation. Keep existing verifier/metadata/version/backoff tests. |

Read-only reference: `.github/workflows/release.yml` constructs the same agent basenames and emits apply/maintenance helpers; it needs no change. Agent updater and root verifier remain unchanged. No frontend, agent, server route, release workflow, manifest, trust key or storage changes.

## 3. Compatibility and absence semantics

Test current release platforms linux-amd64, linux-386, linux-arm64, linux-armv7, linux-armv6, darwin-amd64, darwin-arm64 and windows-amd64, with Windows .exe. Keep assetSuffix behavior including GOARM zero; do not add an unrelated platform allowlist or validation rule. Other caller suffixes simply require their exact canonical agent name.

When no exact asset exists, `Assets` remains the existing empty map/requested key absent. Preserve version-derived UpdateAvail even if that asset is missing; callers already distinguish a newer release from a usable binary locator. Preserve exact checksum/signature locator collection and metadata regardless of helper/agent ordering. Duplicate identical GitHub asset names are outside this hotfix's ordering guarantee; do not add a new duplicate policy.

## 4. Security and tests

Exact naming prevents cross-kind confusion but does not authenticate download URLs or bytes. Agents still verify repository-pinned signatures and exact-name checksums, and root apply re-verifies before installation. Do not relax those checks to accept the incorrectly selected maintenance payload. Signed metadata may contain several valid executable families; selecting the correct family and verifying its bytes are separate necessary steps.

AC-001 tests eight-platform positives and extension negatives. AC-002 uses the same mixed release entries in multiple orders and asserts only canonical agent URLs. AC-003 exercises helper-only plus decoys (prefixes, suffixes, wrong arch, archive, `.txt`, `.sig`, `.exe` mismatch, path/case variants) without live GitHub calls. AC-004 asserts current/latest/update/release/published/checksum/signature fields and retains existing absent/unsigned/dev/downgrade tests. AC-005 reuses the existing signed-agent verification suite as regression evidence; no verifier changes are authorized.

Run baseline before code, focused update-checker tests, full `go test ./...`, Linux `-race` and `go vet ./...` using the existing project environments. Tests use in-memory GitHubRelease fixtures, not live downloads or production updates. Implementation report records exact cases and unchanged trust boundary; independent QA follows implementation, then technical writing records the narrow hotfix.
