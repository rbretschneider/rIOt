# QA report — DOCKER-STACK-003

Date: 2026-10-20. Reviewed clean committed revision `ea0c581` on `codex/stack-update-controls`.

**Verdict: PASS.** No blocking implementation defect found within the approved frontend scope. Collection updates remain explicit, target-scoped and gated; detail Update remains discoverable without bypassing existing safety controls. No live stack update or production mutation was performed.

## Independent review and checks

Read the repository engineering pipeline, FRD, ADD, security review, implementation report and validation evidence. Reviewed the new dialog, collection/detail integration, tests and reused command tracker, command progress, Modal and DataTable behavior. QA made no implementation edits.

Independent commands, all exit zero:

```text
npm run test:run -- src/pages/docker/DockerPages.test.tsx src/components/docker/StackUpdateDialog.test.tsx
StackUpdateDialog.test.tsx: 13 passed
DockerPages.test.tsx: 43 passed
Test Files: 2 passed
Tests: 56 passed
Duration: 6.13s

npm run test:run -- src/components/ui/Modal.test.tsx src/components/docker/DataTable.test.tsx
Modal.test.tsx: 35 passed
DataTable.test.tsx: 6 passed
Test Files: 2 passed
Tests: 41 passed
Duration: 1.90s

npx tsc --noEmit
No diagnostics.
```

Implementation evidence records the pre-change 1,559-test baseline, final 1,578-test frontend suite and passing production TypeScript/Vite build. QA used independent focused and shared-accessibility checks rather than repeating the complete suite. The current independent page/dialog count is **56**, correcting the implementation report's focused count of 55. There is no lint script; no separate lint result is claimed.

## Acceptance coverage

| Criterion | QA result and evidence |
|---|---|
| AC-001 | PASS. Collection Update selects the device/project in local state and opens one dialog outside the table. The in-place/filter regression passes; open/query/close paths do not send mutations. Zero/down/partial availability is not used as a row-button gate. Existing collection route/query/virtual viewport stays mounted. |
| AC-002 | PASS. Detail matrix proves exactly one Update for running with/without updates, partial and down states. Update is secondary when another primary action applies; Up and existing other actions remain. Capability/path/current busy controls still disable dispatch. |
| AC-003 | PASS. Dialog refetches selected detail and host capabilities on mount; both must succeed and stop fetching. Correct device/project, agreed paths, nonempty directory and selected-host capability are required. Cached-agreed/delayed changed paths, conflict, empty directory, missing capability and cached-data refetch errors fail closed in tests. No path acknowledgement is sent. |
| AC-004 | PASS. Named modal includes host/project, agreed directory/files, pull/up/recreation/stopped-service consequences, canonical detail link and self-infrastructure warning. The only create call is behind the explicit Confirm handler; opening/closing/reads cannot update. Modal has no implicit form submission and initially focuses a nonmutating body link. Existing typed Down/self-host regression passes. |
| AC-005 | PASS. Exact API/payload and rapid repeated-click test proves one create for the selected device/project, without client-selected paths/options. A synchronous submission ref guards the create interval; tracker busy state blocks accepted active work. Conflict test attaches the existing command and shows its progress without creating again. Detail submit also rechecks update gates/busy state. |
| AC-006 | PASS. One selected dialog owns tracking/progress outside grid rows. No growing per-row progress or automatic retry/timer is added. Terminal callback invalidates selected detail, collection and containers; once-per-command failure refresh is tested. Dispatch errors/failures remain visible, retry requires another explicit action, active work is adopted, and observation Close neither sends nor cancels a command. Existing tracker success-display timeout is retained; active/failure state is not cleared on that timer. |
| AC-007 | PASS. Keyed device/project mounting and scoped queries/API/tracker prevent late prior-host reads from replacing the current target. Same-project/different-host late-response test dispatches and observes only host two. Reused Modal focus trap/restoration/pending protection and DataTable virtual keyboard suites pass independently. Canonical links, filters, path acknowledgement, logs and other-action confirmations remain covered by existing page regressions. |

## Visual, accessibility and security review

Independently inspected the saved actual screenshots `bin/stack_update_003/list_wide.png` and `confirm_portrait.png`. The collection action fits beside identity without expanding rows. The portrait confirmation stays readable, identifies exact project/host and Compose context, and separates Close from Confirm. The table retains internal horizontal scrolling. Implementation's browser measurements report 44px rows and a 448px dialog at a 1,200px viewport; QA inspected pixels but did not independently repeat those geometry measurements.

Shared Modal supplies a named dialog, focus trapping/restoration and pending dismissal guards; independent 35-test coverage passes. The action cell uses a compact single-line button and progress lives in the modal body. No effect/query callback triggers updates. Paths and names remain escaped React text, with canonical encoded resource links. Existing authenticated API/server capability/path validation remains authoritative; this adds no backend command, permission, dependency or schema.

The local demo's pre-existing stack-command fixture returns a generic container-restart progress record. Its progress screenshot is therefore not evidence of real Compose output/action labels. Exact update dispatch, target identity, conflict/progress attachment and terminal refresh are instead established by mocked API/command tests and code review. No production update was used as QA. Existing Vite chunk-size warning is unrelated and nonblocking.

Technical writing and normal PR review may proceed against `ea0c581`.
