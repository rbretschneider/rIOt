# QA report — DOCKER-TABLE-002

Date: 2026-10-20. Reviewed commits `4df7eb2` and corrective `09ec5d5`.

**Verdict: PASS.** The Name-only preference defect found during independent review is fixed and regression-tested. No unresolved blocking finding remains within this frontend scope. No production deployment or mutation was performed.

## Review and independent validation

Read the repository engineering pipeline and paired FRD, ADD, security review and implementation report. Reviewed the committed Name/marker rendering, typed verdict reuse, URL-state fallback, Stack comparator and generic DataTable sizing correction, together with the new and existing focused tests. QA began against committed clean trees; no implementation files were edited by QA.

Independently ran the final focused suite:

```text
npm run test:run -- src/pages/docker/DockerPages.test.tsx src/utils/dockerUrlState.test.ts src/components/docker/DataTable.test.tsx

src/utils/dockerUrlState.test.ts: 7 passed
src/components/docker/DataTable.test.tsx: 6 passed
src/pages/docker/DockerPages.test.tsx: 37 passed
Test Files: 3 passed
Tests: 50 passed
Duration: 5.11s
```

Independent `npx tsc --noEmit` exited zero with no diagnostics. The initial independent run on `4df7eb2` also passed its 47 focused tests and type check, but code review identified the missing preference edge below. Implementation evidence records a green 1,541-test baseline, final full 1,559-test suite and production TypeScript/Vite build. QA did not redundantly rerun the entire unchanged frontend suite. There is no lint script in package.json; no separate lint result is claimed.

## Finding and correction

The first implementation removed Name's explicit grow metadata but left the shared last-visible-column grow fallback active. Saved `['name']`, empty or unknown-only preferences legitimately produce a Name-only table, making Name expand again. Independent QA reported this against AC-001 before issuing a verdict.

Commit `09ec5d5` adds regressions for all three restored preference cases and fully pinned generic tables. Fully sized pinned-only tables now use their summed width; the implicit last-column grow fallback excludes pinned columns. Other tables retain their current width behavior. No filler columns, synthetic selectable rows or altered grid counts were introduced. The new regressions and all focused tests pass independently.

## Acceptance coverage

| Criterion | Result and evidence |
|---|---|
| AC-001 | PASS. Default/restored Name width and 44px pinned selection offset are tested. Corrected Name-only/empty/unknown preferences preserve Name184px and two semantic columns. Actual wide, portrait and Name-only screenshots were visually inspected; Name remains compact, markers fit, and rows remain visually stable. Implementation's browser measurements report Name184px/rows44px at wide and portrait sizes, no document-width overflow at1200px, and Name-only table228.5px including borders. QA inspected those screenshots independently but did not independently remeasure their browser geometry. |
| AC-002 | PASS. Long-name/title/canonical-route test, persisted Image-hidden preference, marker visibility and single-marker behavior after enabling Image pass. The nonshrinking marker is a sibling of the truncated link and has accessible name/title `Update available`. Screenshots show readable markers with Image hidden. |
| AC-003 | PASS. Nine verdict/legacy cases cover available, pinned, failed check, up-to-date, false/null/missing and authoritative override. Stopped availability remains informational. Marker is a plain span without a handler; action eligibility and confirmations are unchanged. |
| AC-004 | PASS. Missing/invalid sorts default Stack ascending; explicit sort/direction matrix passes. Stack ordering uses ascending host/name and final row-key tie-breakers within either primary direction. Standalone rows stay adjacent; explicit Name sort remains authoritative. No grouping rows are created. |
| AC-005 | PASS. Existing saved-column/view, URL/filter, canonical Name/Logs route, selection, virtual keyboard navigation and mutation/self-host confirmation regressions pass in the focused suite. Shared-table correction preserves pinned offsets and semantic column counts. |

## Visual evidence and limits

Inspected actual saved images `bin/docker_table_002/wide.png`, `portrait.png` and `name_only.png` with the image viewer. Wide and portrait tables show compact identities, readable inline updates, aligned metrics and adjacent Stack ordering. The Name-only image shows a narrow two-column table with the update badges retained, rather than a stretched identity area. The screenshots use simulated demo data; they do not establish production telemetry freshness or exercise a live mutation.

The existing title retains full container/image identity when the link truncates; keyboard/canonical link behavior is unchanged. Explicit verdict precedence comes from the existing shared helper, without new runtime fields, API calls, dependencies or privileges. No authentication/action boundary change was found. Existing Vite chunk-size warning is unrelated and nonblocking.

Technical writing and normal PR review may proceed against `09ec5d5`.
