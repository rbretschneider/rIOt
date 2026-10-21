# Documentation report — DOCKER-TABLE-006

Date: 2026-10-21. Technical writing follows independent QA **PASS** against `5411f79`.

## Inputs and scope

Read the complete FRD, ADD, security review, implementation report and QA report. Reviewed both changed source/test files and cross-checked the unchanged CPU/Memory rendering and column allocation. No implementation, tests, upstream pipeline documents, Hawaii files or production state changed at this stage.

## Changes

- `README.md`: updated the current Containers table paragraph from compact Name to pinned Name receiving remaining space, equal fixed 160px CPU/Memory columns and graph tracks, distinct existing scales and internal narrow-screen scrolling. Retained update badges, sorting, saved preferences, identity/tooltips and action/freshness caveats.
- `CHANGELOG.md`: one Unreleased Changed entry explicitly supersedes the earlier compact-Name preference, including Name-only preferences, and records fixed 64px Open plus preserved behavior.
- This report records the documentation handoff.

Historical DOCKER-TABLE-002 documents and changelog entry remain unchanged. Its generic pinned-only/no-grow table behavior is still valid; this change uses explicit Name growth locally. Documentation claims no metric normalization, new API/query, preference migration, shared-component change or deployment.

## Verification

Compared documented widths with ContainersPage metadata: CPU160, Memory160, Open64, declared Name184 minimum and only Name marked grow/pinned. Existing metric renderers preserve CPU percentage and configured-limit memory calculations. `git diff --check` passed for these documentation files.

No application tests were rerun for this documentation-only stage. QA independently passed 62 focused tests and TypeScript; implementation evidence records the full 1,584-test suite and production build. Actual implementation browser measurements report equal 136px CPU/Memory tracks at wide and portrait widths and 44px rows. QA inspected saved screenshots without independently repeating geometry measurements; documentation does not broaden that evidence.

## Stale content found

The prior compact-Name changelog entry and DOCKER-TABLE-002 pipeline documents intentionally describe the earlier decision. They were preserved as historical evidence; the new entry explicitly states the superseding preference. No unrelated documentation was rewritten.

## Completion

Documentation is ready for normal PR review. No live action or production mutation was performed.
