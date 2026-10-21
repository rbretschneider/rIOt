# Security Review — UPDATE-ASSET-004

**Date:** 2026-10-20  
**Verdict:** APPROVED for scoped server matcher hotfix

## Reviewed evidence

Read CLAUDE pipeline, checker.go/checker_test.go, Go module and release workflow naming. The current matcher admits any suffix-containing asset except text and apply helpers, so maintenance helpers can replace the agent locator based on order. Root's supplied release-manifest comparison identifies the observed mismatches as maintenance payloads. Existing agent verification rejects them; that refusal is correct and is not part of the defect.

## Approved change and invariants

- Match only exact canonical agent basename, with Windows .exe and no path/case/extension normalization. This is an allowlist by executable family, not an expanding helper denylist.
- Helper-only/decoy-only feeds expose no agent asset key. Never fall back to a helper, advertise helper URLs as agents or bypass an absent/mismatched binary.
- Preserve signed checksum/signature locators, release/version semantics, API shape and suffix generation. No runtime dependency or privilege change.
- Do not modify agent checksum/signature enforcement, root signed apply, keys, manifests, release binaries or URL trust semantics. A correctly named locator still requires full existing byte verification; filename matching is not authentication.
- Tests remain in-memory/no network. No production update, release artifact replacement or verification weakening is authorized by this implementation task.

## Verification gate

AC-001: eight platform/executable-extension matrix. AC-002: mixed helper/agent order cannot affect selection. AC-003: helper-only and representative decoys never advertised. AC-004: metadata/version and existing checker regressions. AC-005: existing signed-agent verification tests remain green. No unresolved security blocker in this minimal ADD; release repository/URL compromise remains subject to existing pinned signature verification.
