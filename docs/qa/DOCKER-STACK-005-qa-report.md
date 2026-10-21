# QA report — DOCKER-STACK-005

Date: 2026-10-20. Reviewed clean committed revision `2022052` on `codex/stack-update-visibility`.

**Verdict: PASS.** No blocking defect found in this presentation-only change. Positive update counts become conspicuous without changing target selection, confirmation or command dispatch. No production action was performed.

## Review and independent checks

Read the repository pipeline and paired FRD, ADD, security review, implementation report and validation evidence. Reviewed the complete StacksPage/test diff. Only local positive-count rendering/classes and their regressions changed; shared Button/DataTable, dialog, routes, API/types and backend remain unchanged.

Independent checks, all exit zero:

```text
npm run test:run -- src/pages/docker/DockerPages.test.tsx src/components/docker/StackUpdateDialog.test.tsx
StackUpdateDialog.test.tsx: 13 passed
DockerPages.test.tsx: 47 passed
Test Files: 2 passed
Tests: 60 passed
Duration: 5.71s

npx tsc --noEmit
No diagnostics.
```

Implementation evidence records a green 1,578-test baseline, final full 1,582-test suite and production build. QA did not repeat the entire suite for this narrow presentation diff. No separate lint script exists; existing build chunk-size warning is unrelated.

## Acceptance coverage

| Criterion | Result and evidence |
|---|---|
| AC-001 | PASS. Tests cover counts1and15, exact `Update (N)` text, explanatory singular/plural title and warning pill. The positive action uses local amber/dark-text overrides of existing shared Button styles. Actual screenshots show readable positive labels and pills. |
| AC-002 | PASS. Zero and negative test cases preserve neutral secondary `Update` and numeric count, without warning title/style or a freshness claim. The predicate is only `updates > 0`; no count/filter/data transformation is added. |
| AC-003 | PASS. Positive-count clicks preserve the scoped project/host accessible label and open the correct existing dialog without dispatch. Existing zero-count collection context and selected-target tests pass, together with all13 fresh-gate/duplicate/conflict/progress dialog regressions. The onClick and dialog behavior are unchanged. |
| AC-004 | PASS within the recorded browser scope. Independently inspected actual wide, portrait and focused-button screenshots. Positive controls stay compact, identity cells remain unchanged, warning counts remain on the action itself and the shared focus ring is visible. Existing fixed project/action widths, button height and DataTable rows are unchanged. Implementation's browser measurements report button24px/row44px/action112px, no page overflow at1200px, and9.79:1 positive foreground/background contrast. |

## Visual evidence and limits

Inspected `bin/stack_visibility_005/portrait.png`, `wide.png` and `focus.png` with the image viewer. Amber buttons clearly distinguish known updates from neutral zero-count stacks; count text conveys the difference independently of color. The focus image shows the retained ring around the action. Portrait overflow stays inside the existing table and project identities remain pinned/readable.

QA inspected saved pixels rather than independently repeating browser geometry or color measurements. The actual demo screenshots contain single-digit counts. Two-digit15is covered by component tests; the implementation reports a single-digit rendered width71.91px plus an independently measured extra-digit width4.84px, estimating76.75px within88px action-cell content. This is a fit estimate, not a claimed browser rendering of a15-count fixture. No arbitrary-large-count layout guarantee is established.

No new mutation, automatic update, capability/path bypass, dependency, global theme or shared-component change was found. Existing server counts remain informational rather than a freshness or authorization guarantee. QA made no implementation edits and issued no production update. Technical writing and normal PR review may proceed against `2022052`.
