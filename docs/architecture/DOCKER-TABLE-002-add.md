# Architecture — DOCKER-TABLE-002

**Date:** 2026-10-20  
**Stack:** React 19, strict TypeScript 5.9, React Router 7 URL state, TanStack Query 5, Tailwind 4, Vite 6, Vitest 4/Testing Library. No dependency changes.

## 1. Minimal design

Retain the existing `ContainersPage` and generic virtual `DataTable`. Fix column metadata, add an informational marker to mandatory Name, and change URL-state fallback/comparator. Do not introduce a new table engine, page layout, resource model, grouping rows, API request or update mutation.

## 2. Column allocation and identity

`ContainersPage` currently sets `grow: key === 'name'`; `DataTable` deliberately ignores width for grow columns. Remove Name's grow assignment, retaining width 184 and pinned true. Prefer Open (`links`) as the explicit grow column when visible, so its utility cell absorbs desktop slack. If users hide Open, retain DataTable's existing last-visible-column slack fallback. All explicit width minima, selection width 44, table min-width sum and pinned offsets stay valid. No shared DataTable behavioral change is needed unless a focused regression proves the existing contract insufficient.

Render Name contents in a constrained single-row flex container: identity link `min-w-0`/truncate, compact update marker `shrink-0`. Keep full existing identity/image title, canonical `containerPath`, collection router state, icon/name/self-role display and keyboard activation. The marker uses neutral/warn theme tokens, visible compact text or existing icon plus an accessible name `Update available`; title provides the same explanation. It is not a button/link and has no event handler. This keeps 44px virtual rows stable and prevents truncation from swallowing availability. Remove the old optional Image badge; Image retains its own full-image title.

## 3. Verdict semantics and data

Reuse `hasUpdateAvailable` from `utils/commandLifecycle.ts` with the existing `DockerRow.update_status` and inherited `update_available`. That helper lets explicit verdict override legacy boolean and does not invent freshness from null/false. Fleet rows do not expose `update_reason`, so do not fetch per-device details or claim a registry/pulled reason that is absent. A plain `Update available` title is sufficient; an absent marker is not labelled `Up to date`. Keep `applicable`, bulk eligibility, Updates filter and Check for Updates behavior unchanged.

## 4. Component changes

| File | Change |
|---|---|
| `web/src/pages/docker/ContainersPage.tsx` | Compact Name sizing/flex marker; move Image badge; Stack comparator tie-breakers. |
| `web/src/utils/dockerUrlState.ts` | Fallback sort `stack` (asc) when omitted/invalid; keep valid explicit sorts/directions. |
| `web/src/types/docker.ts` | Import existing `UpdateStatus` from models and narrow existing optional `DockerRow.update_status` from string to that union, allowing reuse of the typed verdict helper without a cast. No field or wire change. |
| `web/src/pages/docker/DockerPages.test.tsx` | AC-001/002/003/004/005 integration regressions with verdicts, hidden Image, preference restoration, row ordering/routes. |
| `web/src/utils/dockerUrlState.test.ts` | AC-004 fallback and explicit-sort matrix; update obsolete name-fallback expectation. |
| `web/src/components/docker/DataTable.test.tsx` | AC-001 column metadata/pinned offset regression only if useful; implementation need not change. |

No modifications to server/agent, shared model definitions, shared auth shell, tiles implementation or global theme. The narrow DockerRow annotation correction above is permitted; no new verdict or helper semantics are introduced. Existing table tests and Docker detail/log tests remain regression evidence.

## 5. Ordering and state compatibility

`parseDockerState` is used only by ContainersPage. Fallback changes from Name asc to Stack asc; do not write an unsolicited sort parameter on mount or reset persisted columns/view. For Stack sort, compare stack names (empty represents displayed `Standalone`), then hostname, then container_name, then `rowKey` for collision-safe total order. Stack primary comparison follows asc/desc; the host/name identity tie-breakers stay ascending, yielding readable order inside each stack in either direction. Empty stack sorts first ascending and last descending. Duplicate stack names across hosts remain distinguishable through host column and deterministic host ordering. Other explicit sorts retain their current comparison plus rowKey tie-breaker. No synthetic rows are added, and selection keys/frozen metric-sort behavior remain untouched.

## 6. Security/accessibility

All names remain React text; no HTML injection, remote icon fetching or new external links. Resource path encoding, safe published-port URLs, feature/auth gates and mutation confirmations are unchanged. Availability is data, never authorization. The marker has a text-equivalent accessible name and is not color-only or hover-only. It must not obscure focus rings or increase row height. Storage parsing remains defensive and session fallback remains functional when browser storage is unavailable.

## 7. Verification and pipeline

Map FRD AC-001–005 to named tests. Run existing baseline before edits, focused Docker pages/URL/DataTable tests, full `npm run test:run`, `npx tsc --noEmit` and production build. Actual browser QA at wide desktop and portrait width must confirm Name bounding, nonclipped marker, internal horizontal scroll and preserved Name/Logs navigation. Report existing lint availability honestly; package.json defines no lint script. Security review precedes implementation, then independent QA and technical writing; no production deployment is part of this story.
