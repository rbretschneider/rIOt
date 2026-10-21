# QA report — DOCKER-TABLE-006

Date: 2026-10-21. Reviewed clean committed revision `5411f79` on `codex/container-metric-widths`.

**Verdict: PASS.** No blocking defect found. Equal fixed metric columns and explicit Name growth use the existing table contract without changing metric calculations, preferences or interactions. The new growth requirement deliberately supersedes the earlier compact-Name preference; this is not a regression against the current requirements.

## Review and independent checks

Read the repository pipeline, FRD, ADD, security review, implementation report and validation evidence. Reviewed both changed code/test files and the existing shared table allocation. The implementation changes only CPU metadata64→160 and grow assignment Open→Name. Memory160, Name's declared184 allocation/pinning, Open64 and all renderers remain unchanged. Shared DataTable is untouched; its pinned-only/no-grow behavior remains valid for other callers.

Independent execution, all exit zero:

```text
npm run test:run -- src/pages/docker/DockerPages.test.tsx src/components/docker/DataTable.test.tsx src/utils/dockerUrlState.test.ts
dockerUrlState.test.ts: 7 passed
DataTable.test.tsx: 6 passed
DockerPages.test.tsx: 49 passed
Test Files: 3 passed
Tests: 62 passed
Duration: 6.03s

npx tsc --noEmit
No diagnostics.

git diff --check
No diagnostics.
```

Implementation evidence records the green 1,582-test baseline, final full 1,584-test suite and passing production build. QA did not repeat the complete suite for this two-line metadata change. There is no separate lint script; existing Vite chunk-size warning remains nonblocking.

## Acceptance coverage

| Criterion | Result and evidence |
|---|---|
| AC-001 | PASS. Default and custom-column tests establish fixed CPU160/Memory160/Open64 and Name as the only unspecified grow width. Rendered CPU2%/Memory50% fills retain their distinct existing calculations. Review confirms stopped/no-limit rules and identical cell padding are unchanged. Actual portrait screenshot shows aligned full-width tracks; implementation browser measurements report equal160px cells/136px tracks at wide and portrait sizes. |
| AC-002 | PASS. Default/restored column tests retain pinned Name44px offset, existing declared widths and no preference migration. Name-only, empty and unknown-only saved choices now intentionally use full table width, preserve minimum228 including selection, and retain mandatory Name/badges. The existing explicit-grow table branch applies naturally without changing generic pinned-only tests. |
| AC-003 | PASS. Existing long identity/title, nonshrinking available marker and authoritative verdict regressions pass. Stack default/explicit sorts, saved columns/view and canonical Name/Logs routes remain green. No identity, marker or navigation implementation changed. |
| AC-004 | PASS within recorded browser scope. Independent visual inspection of actual wide and portrait screenshots confirms flexible identity space, stable rows, aligned metric geometry and readable badges. Implementation measured rows44px, metrics160px, Open64px, wide Name884px and portrait Name396px. Narrow900px evidence records table928px inside840px viewport with Name184px and document900px; actual Name-only controls yielded table840px/Name795.5px with all four badges. Existing virtual keyboard/selection and action/confirmation regressions pass independently. |

## Visual evidence, boundary and limits

Inspected `bin/table_widths_006/wide.png` and `portrait.png` with the image viewer. The portrait table shows equal CPU/Memory tracks and Name receiving spare space while the Open column stays compact. QA inspected saved pixels; it did not independently repeat the browser geometry measurements or narrow/Name-only interaction sequence reported by implementation. Those values are explicitly implementation browser evidence, supported by the metadata/table-contract regressions.

Equal geometry does not make CPU percentage and configured-limit memory usage equivalent scales. No value normalization, new freshness claim, query/API, dependency, global style, shared component, auth gate or mutation path changed. Tests preserve exact targets, confirmations and resource URLs. No production action was needed or performed, and QA made no implementation edits.

Technical writing and normal PR review may proceed against `5411f79`.
