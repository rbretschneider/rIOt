# Architecture — DOCKER-TABLE-006

**Date:** 2026-10-21  
**Stack:** React19, strict TypeScript5.9, Tailwind4, existing DataTable, Vitest/Testing Library. No dependencies.

## Minimal implementation

Change only ContainersPage column metadata: CPU64 →160, Memory stays160, `grow: key === 'name'` instead of Links. Name remains declared width184/pinned, Links remains64, all other widths/order unchanged. Do not alter graph renderers, percentage calculation, limit policy, cell padding, queries or row height.

Existing DataTable already handles explicit grow columns: their `<col>` width is unset while fixed siblings retain width, table min-width remains the sum of declared allocations plus selection44, and pinned-only exact-width mode applies only when pinned columns have no grow. Thus explicit Name growth naturally makes Name-only tables fill the viewport, while the generic pinned-only/no-grow behavior from DOCKER-TABLE-002 remains correct for other callers. No shared DataTable change is expected or authorized without an architecture amendment.

At minimum table width, Name receives its declared184 allocation; at larger workspace widths it receives all slack. Narrow viewports scroll internally because fixed siblings and the declared minimum sum are retained. CPU/Memory each160 with matching px3 cell padding gives equal physical graph tracks (136px before any browser rounding), but CPU still represents its existing percent scale and Memory its configured-limit ratio. Missing/stopped/no-limit bars remain unchanged. Do not relabel these as interchangeable units or normalize values together.

## Files

| File | Change |
|---|---|
| `web/src/pages/docker/ContainersPage.tsx` | Set CPU160, Memory160 and Name-only grow; retain pinned identity, badge, sort and links. |
| `web/src/pages/docker/DockerPages.test.tsx` | Replace obsolete compact-Name assertions with DOCKER-TABLE-006 AC-001/002 metadata/grow/min-width assertions for default, restored, Name-only/empty/unknown preferences; retain AC-003/004 behavior regressions. |

Read-only reference: components/docker/DataTable.tsx and its existing generic pinned-only tests; these are not obsolete and need not change. No shared components/styles, types, API/server/agent, routes or saved preference schema changes. Historical DOCKER-TABLE-002 docs remain historical; this story explicitly supersedes only its Name growth decision.

## Verification and security

AC-001 tests fixed metric widths and unchanged rendered fill values; actual browser measurements establish equal track geometry. AC-002 tests unset grow-Name col width alongside fixed siblings/min-width sum, restoring preferences without writes, and Name-only table fill rather than exact228 width. AC-003 reuses badge/verdict, Stack sort/explicit sort and canonical logs/resource-link tests. AC-004 requires real wide and portrait browser checks of cell/track/Name dimensions, 44px row height and contained overflow; existing DataTable keyboard/selection and action/feature/auth tests remain green.

Run baseline, focused Docker pages/URL/DataTable tests, full frontend suite, `npx tsc --noEmit` and production build. No lint script is defined. Independent QA validates actual CSS allocation, then technical writing records changed sizing preference. No production update or mutation is needed for this presentation change.
