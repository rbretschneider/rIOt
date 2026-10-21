# Security Review — DOCKER-STACK-003

**Date:** 2026-10-20  
**Verdict:** APPROVED for scoped frontend implementation

## Reviewed boundary

Reviewed StacksPage, StackPage, dockerApi.stackAction, useCommandTracker, CommandProgress, stackPrimaryAction and the existing DockerStackAction server handler. Existing Update maps to the existing docker_update command with server-derived Compose project/working directory/config files. Server requires agreed paths and stack_actions capability; it does not require typed host confirmation for Update. This story adds access to that existing action, not a new command/API/privilege.

## Required controls

- Opening/refetching/closing a dialog is read-only. The existing mutation is sent once only after explicit Confirm. No auto retries, effect-driven dispatch or automatic path acknowledgement.
- Fresh selected-target detail and host capability gates precede confirmation; server revalidates current path/capability/authorization. Client never supplies arbitrary Compose paths/options or claim a new server-enforced host confirmation.
- Bind device and project together through selected state, query keys, keyed dialog, tracker scope and API arguments. Same project on another host must never receive stale async actions/progress. Disable/recheck gates during submission and prevent duplicate creates.
- Reuse tracker conflict attachment and command progress; report errors truthfully and refresh queries on terminal result. Closing observation never cancels a running update; any queued cancellation stays an explicit existing control.
- Confirmation accurately warns of pull/up, changed-service recreation, stopped-service start and self-infrastructure connection interruption. Zero update count is not a no-op promise.
- Preserve all existing auth/feature gates, other-action typed confirmations, path acknowledgement, logs and read-only secret masking. Do not fetch Compose/env contents or introduce external navigation to perform this action.
- Escape all paths/names as React text, preserve canonical encoded links and shared accessible Modal behavior. Fixed virtual rows cannot contain growing progress regions.

## Evidence required

AC-001/004: no mutation before explicit confirmation and clear consequences. AC-002/003: discoverability never bypasses gates, including newly changed paths and unsupported agents. AC-005: exact payload, duplicate prevention and conflict adoption. AC-006/007: terminal refresh, durable visible failure, scoped reopen/cross-host isolation, no cancellation on close, keyboard/virtual-row regressions. QA uses mocked/demo commands and must not perform a production stack update.

No unresolved security blocker within the approved ADD. Existing Compose execution remains a powerful authorized server/agent operation; browser confirmation does not sandbox Compose or replace server validation.
