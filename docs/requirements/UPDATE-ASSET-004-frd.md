# Formal Requirements — UPDATE-ASSET-004

**Date:** 2026-10-20  
**Scope:** Server update-feed agent asset selection regression

## 1. Problem and outcome

After publishing v2.74.0, agent updates failed checksum verification. The server matcher accepts any release asset containing the requested platform suffix except text files and the known apply-helper prefix. A later `riot-maintenance-<suffix>` therefore overwrites the actual agent URL. Root's release-manifest investigation identified the downloaded hashes as maintenance binaries, not agent binaries. Agent checksum/signature refusal is working and must remain unchanged.

Return only the exact canonical agent release basename for the requested suffix. Helpers and lookalikes must never be advertised as agent downloads, regardless of release asset order.

## 2. Requirements

- FR-001: Match exactly `riot-agent-<suffix>` on non-Windows platforms and exactly `riot-agent-<suffix>.exe` for Windows suffixes. Do not use substring/prefix/extension-denylist matching or normalize an asset path into a valid basename.
- FR-002: Preserve existing suffix generation, including ARM v6/v7 and GOARM zero behavior. Preserve supported release platforms, version comparison/dev behavior, unknown-release handling and update metadata/API shape.
- FR-003: Never select apply helpers, maintenance helpers, archives, sidecars, prefixed/suffixed decoys, path-like names or wrong-platform binaries. A helper-only/decoy-only release leaves the requested asset key absent; it does not substitute a helper or fabricate success.
- FR-004: Preserve exact checksums.txt and checksums.txt.sig locators, release URL/time and version metadata. Asset locators remain untrusted input; agent and privileged-helper checksum/signature validation stay intact. No release artifact replacement, trust-key change, agent verification bypass or new dependency.

## 3. Acceptance criteria

| ID | Required evidence |
|---|---|
| AC-001 | Table-driven exact agent selection for all eight released platforms: linux amd64/386/arm64/armv7/armv6, darwin amd64/arm64 and windows amd64. Windows requires .exe; non-Windows does not accept .exe. Existing suffix tests remain green. |
| AC-002 | Mixed agent/apply/maintenance/sidecar assets in forward, reverse and representative shuffled orders always advertise the correct agent URL and no helper URL. Include maintenance after the agent to reproduce the regression. |
| AC-003 | Helper-only and decoy-only release fixtures advertise no requested agent asset; direct matcher negatives cover wrong platform, added prefix/suffix, archives, text/signature files, path components and case/extension variants. No fallback/helper substitution. |
| AC-004 | Metadata/version semantics remain unchanged: current/latest, update availability, release URL/published time, checksum and signature locators, unsigned/missing metadata, no-release/dev/current/newer-agent cases. All existing checker tests pass. |
| AC-005 | Existing signed agent download/verification regression suite remains green; no code change weakens agent or root-helper verification. No actual production update or remote release modification is needed for testing. |

## 4. Delivery

Follow CLAUDE pipeline: approved architecture/security, green Go baseline, AC-labelled matcher/checker regressions, focused/full Go tests, Linux race and vet as appropriate, independent QA then technical writing. This is a server-only hotfix with unchanged API and artifacts. Deployment/release remains a separately coordinated step.
