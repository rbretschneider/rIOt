# Documentation report — UPDATE-ASSET-004

Date: 2026-10-20. Technical writing follows independent QA **PASS** against `1adf7ce`.

## Inputs and scope

Read the complete FRD, ADD, security review, implementation report and QA report. Inspected the exact matcher and its update-feed call site, AC-labelled tests and existing README signing guidance. No implementation, tests, upstream pipeline documents, Hawaii files or production state changed at this stage.

## Changes

- `CHANGELOG.md`: one Unreleased fix entry explains exact canonical agent filename selection, exclusion of maintenance/apply helpers and sidecars regardless of ordering, no helper fallback when absent, and preserved signed verification.
- `README.md`: updated only step one of How an update is verified to describe exact platform asset selection and missing-binary behavior. It retains the distinction between a locator and authentication.
- This report records the documentation handoff.

No new API, platform, dependency, schema, release artifact or trust key is claimed. Filename matching selects the executable family; existing agent signature/checksum and privileged-helper verification still authenticate installed bytes. A newer release can retain version-derived availability without a usable platform locator, as before.

## Verification

Compared documented canonical names with `isAgentBinaryAsset`: exact `riot-agent-` plus platform suffix, adding lowercase `.exe` only for Windows. Confirmed the call site preserves checksum/signature locators and metadata. `git diff --check` passed for these documentation files. Application tests were not rerun for this documentation-only stage; independent QA passed the checker/release-key and signed-agent verification suites. Linux race and integration checks remain CI gates, not claims made by this report.

## Stale content found

Existing README maintenance guidance still uses pre-release wording despite the implementation report's reference to published v2.74.0. Updating release/migration/rollout status is outside this narrow matcher hotfix and was not silently bundled into it.

## Completion

Documentation is ready for normal PR/release review. No deployment, agent update or release-asset replacement was performed by this stage.
