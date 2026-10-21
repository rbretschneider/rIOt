# Formal Requirements — DOCKER-STACK-003

**Date:** 2026-10-20  
**Scope:** Stack Update actions in collection and detail, existing frontend/API only

## 1. Outcome

Operators can update a Compose stack directly from Stacks without navigating away. Update remains discoverable on stack detail even when no newer-image verdict exists or the stack is down/partially running. The operation is the existing Compose update action; this story adds no server or agent capability.

## 2. Requirements

- FR-001: Every collection row exposes a compact accessible Update action. It opens an in-place confirmation dialog for the exact device/project; opening, fetching or closing never dispatches an update. The collection route, search/host filters and scroll position remain intact.
- FR-002: Stack detail exposes exactly one Update action: preserve its primary placement when selected by `stackPrimaryAction`, otherwise show it as secondary alongside the existing actions. Zero known updates, down state and partial service state do not hide Update. Existing capability/path/active-command safety gates still apply.
- FR-003: Before confirmation can dispatch, load fresh selected-stack detail and host capabilities. Require successful fresh reads, agreed Compose paths, a nonempty agreed working directory and `stack_actions_v1`. Missing data, loading, errors, missing capability or unagreed paths fail closed with an explanation/detail link, without silently acknowledging paths.
- FR-004: Confirmation identifies project, host, agreed working directory and config files; explains that Update pulls images and runs Compose up, may recreate changed services and start stopped services. Preserve the rIOt infrastructure connection-interruption warning. This is explicit UI consent, not a new typed-host/server confirmation rule for Update.
- FR-005: Dispatch only the existing `dockerApi.stackAction(deviceId, project, {action:'update'})` after confirmation. Prevent duplicate submission while starting/nonterminal. Reuse existing command tracking/progress and attach a returned conflict's existing command instead of retrying or issuing another command.
- FR-006: Keep collection progress/error presentation in the selected-stack modal outside the virtual table. Preserve fixed 44px rows and keyboard focus behavior; no row-expanding progress, per-row trackers or per-row detail fetches. Closing an accepted job's dialog must not cancel or restart it; reopening can adopt the existing command.
- FR-007: Persist progress/failure visibility according to existing tracker semantics while the dialog is open; do not clear an active job on a fixed timer. On terminal state refresh relevant stack detail, Stacks collection and Containers data, including failures. Allow retry only after explicit user confirmation once the tracked command is terminal.
- FR-008: Preserve other stack actions, typed Down/self-host confirmations, path acknowledgement, read-only Compose/env, auto-update settings, logs/activity, canonical resource URLs, auth/feature gating and server authorization. No new dependency, backend endpoint, parameter, schema, permission or live production update.

## 3. Acceptance criteria

| ID | Required evidence |
|---|---|
| AC-001 | Collection row Update opens the correct stack dialog in place; zero/down/partial stacks remain eligible to open it. Clicking/opening/cancelling sends no mutation, retains URL/filter/scroll context and does not navigate to detail. |
| AC-002 | Detail shows exactly one Update for running+updates, running+zero updates, partial and down cases; existing primary Up/Restart and other actions remain present. |
| AC-003 | Confirm stays disabled until fresh detail and host queries succeed and gates pass. Collection's stale agreed row cannot bypass newly pending_ack/conflicted paths, missing capability, query errors or empty directory. Explain the reason; no automatic path acknowledgement. |
| AC-004 | Confirmation shows selected project/host/paths and pull/up/recreate/start-stopped consequences, plus self-infrastructure warning when applicable. Cancel/close/Enter without selecting Confirm never updates. Existing Down typed confirmation regression remains green. |
| AC-005 | Confirm sends one exact update request with correct device/project, never client-selected paths/options. Rapid repeated confirmation cannot duplicate starting/active work. A 409 attaches to existing command and displays progress without a second create. |
| AC-006 | Pending/active/terminal/error progress renders outside grid rows; terminal callback invalidates the relevant detail/list/container queries once per completed command. Failure stays visible and retry requires new explicit confirmation. Closing/reopening adopts an active target without dispatching or cancelling it. |
| AC-007 | Same project on different hosts stays isolated; switching/closing targets cannot send a stale target or apply prior async results to a different stack. Modal uses accessible name/focus restoration and collection rows remain 44px. Canonical stack/log routes, filters and existing other-action guards pass regressions. |

## 4. Gates

Read approved ADD/security before implementation. Establish green frontend baseline; add AC-labelled focused tests; run full frontend tests, TypeScript and production build. Browser QA checks collection row/button fit, confirmation context, progress placement and keyboard behavior at wide and portrait widths with mocked/demo commands only. Do not perform a real production stack update to verify this story.
