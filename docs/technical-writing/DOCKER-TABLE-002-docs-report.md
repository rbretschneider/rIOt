# Documentation report — DOCKER-TABLE-002

Date: 2026-10-20. Technical writing follows independent QA **PASS** against corrective commit `09ec5d5`.

## Inputs

Read the complete FRD, ADD including pinned-only sizing amendment, security review, implementation report and QA report. Cross-checked Name/marker rendering, URL fallback and scoped implementation contracts. No code, tests, upstream pipeline documents or production state changed at this stage.

## Changes

- `README.md`: one focused paragraph in Docker Container Management documents compact pinned Name, update availability with Image hidden, full-identity tooltip, Stack ascending fallback, per-host/name ordering, Standalone placement and preserved explicit sorts/preferences. It explains that the marker is informational and absence does not establish freshness.
- `CHANGELOG.md`: one Unreleased fix entry records the same behavior and corrected Name-only preference case. Historical entries remain unchanged.
- This report records the final documentation handoff.

No new API, action eligibility, confirmation flow, grouping controls, dependency or deployment is claimed.

## Verification

Compared documented behavior with the implemented marker/verdict helper usage, URL parsing and approved sort/sizing contract. Confirmed only one README paragraph and one Unreleased entry were added. `git diff --check` passed for these documentation files. This documentation-only stage does not rerun application tests; QA independently passed 50 focused tests and TypeScript checks, and the implementation report records the full 1,559-test suite, build and real-browser evidence.

## Stale content found

The older DOCKER-UX-001 changelog entry says spare width goes to container names. It describes prior behavior and was intentionally preserved; the new DOCKER-TABLE-002 entry documents the replacement. No unrelated documentation was rewritten.

## Completion

Documentation is ready for normal PR review. No production deployment or live mutation was performed.
