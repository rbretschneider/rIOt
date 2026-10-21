# QA report — UPDATE-ASSET-004

Date: 2026-10-20. Reviewed clean committed revision `1adf7ce` on `codex/agent-asset-selection-fix`.

**Verdict: PASS.** No blocking defect found in this server-only hotfix. Exact executable-family matching fixes helper substitution without weakening signed download verification. Normal CI/release checks remain required; this verdict does not perform or authorize a production update.

## Independent review and execution

Read the repository pipeline, FRD, ADD, security review, implementation report and retained validation evidence. Inspected the matcher, its call site and tests, release-workflow filenames, suffix/version handling and existing signed-agent verification regressions. The only implementation change is `isAgentBinaryAsset` and its obsolete denylist constant/comment removal; agent/root-helper verification, API shape, trust keys, release artifacts and workflow are unchanged.

Independent Windows Go checks, all exit zero:

```text
go test -count=1 ./internal/server/updates ./internal/releasekey
ok github.com/DesyncTheThird/rIOt/internal/server/updates 0.689s
ok github.com/DesyncTheThird/rIOt/internal/releasekey     0.668s

go test -count=1 -run 'UpdatePipeline|UpdateVerify|Signed|Checksum|Signature|Verifier' ./internal/agent
ok github.com/DesyncTheThird/rIOt/internal/agent 2.275s

go test -count=1 -run 'PerformUpdate|TamperedBinaryEndToEnd|SelfConsistentAttackerRelease|CompiledInReleaseKey|ExactAssetNames' ./internal/agent
ok github.com/DesyncTheThird/rIOt/internal/agent 2.257s

git diff --check
No diagnostics.
```

Implementation evidence records a passing pre-change full Go baseline, regression failures against the broad matcher, then passing focused/full Go suites and vet. QA did not repeat the entire suite for this narrow change. Local Windows execution does not establish Linux race/root-helper or PostgreSQL integration results; those remain normal CI gates. All new release fixtures are in-memory, with no production downloads or mutations.

## Acceptance coverage

| Criterion | Result and evidence |
|---|---|
| AC-001 | PASS. Eight-platform checker matrix covers Linux amd64/386/arm64/armv7/armv6, Darwin amd64/arm64 and Windows amd64. Exact filenames agree with the release workflow. Windows requires lowercase `.exe`; non-Windows `.exe` is rejected. Existing suffix tests, including GOARM zero, pass unchanged. |
| AC-002 | PASS. Each platform is checked against forward, reverse and representative shuffled agent/apply/maintenance/sidecar orders. Maintenance after agent reproduces the former overwrite condition; all orders now advertise only the exact agent URL. |
| AC-003 | PASS. Direct matcher negatives reject helpers, wrong architecture, prefix/suffix, archives, text/signature/checksum sidecars, path components, case, whitespace and extension lookalikes. Helper-only/decoy-only/mixed missing-agent feeds leave Assets empty, with no helper fallback or path normalization. |
| AC-004 | PASS. Metadata assertions preserve current/latest version, version-derived availability, release URL/time and exact checksum/signature locators with and without an agent binary. Existing no-release, unsigned, dev, downgrade, verifier and backoff tests pass. A newer version may still be advertised without a usable platform locator; this intentionally preserves existing semantics. |
| AC-005 | PASS. Independent signed-release and agent regression checks cover valid signed staging, missing locators, transport failures, tampered payloads, attacker keys, exact-name checksums and fixed-verifier dispatch. Code review confirms no agent/root verification or trust-anchor changes. |

## Limits and handoff

Canonical filename selection identifies the intended executable family; it is not authentication of a URL or payload. Existing signature/checksum verification remains necessary and authoritative. Duplicate identical asset names retain prior behavior and are outside the stated ordering guarantee. No new platform allowlist, dependency, endpoint, schema or frontend behavior was introduced.

QA made no implementation edits and performed no production agent update, container recreation, SSH action or release-asset replacement. Technical writing and normal PR review may proceed against `1adf7ce`.
