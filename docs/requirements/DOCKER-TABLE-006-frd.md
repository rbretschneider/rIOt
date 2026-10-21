# Formal Requirements — DOCKER-TABLE-006

**Date:** 2026-10-21  
**Scope:** Containers table metric widths and flexible Name column

## Outcome and requirements

The user explicitly changes the previous compact-Name preference: CPU and Memory should have equal physical widths so their graph tracks align, and Name should receive the remaining table space. This supersedes DOCKER-TABLE-002's compact-Name/no-growth requirement only; its update badges, sorting, preferences and navigation remain intact.

- FR-001: CPU and Memory each have a fixed 160px column, with identical cell padding so running-container graph tracks have the same physical width. Preserve CPU's percent-of-100 fill and Memory's configured-limit fill; equal geometry does not mean equal units or scales. Stopped/no-limit behavior is unchanged.
- FR-002: Name is the only grow column, with the existing declared 184px minimum allocation, pinned identity, truncation/title and nonshrinking Update badge. Open/Logs returns to fixed 64px. All other widths stay unchanged.
- FR-003: Name consumes available space with default columns, restored custom columns and Name-only preferences. Empty/unknown-only stored choices still resolve to mandatory Name and let it grow. Preserve saved choices without migration/reset.
- FR-004: On narrow/portrait widths the sum of minimum allocations remains authoritative: use internal horizontal scrolling, keep fixed 44px virtual rows, and do not squeeze metric columns or create page-wide overflow. Thin scrollbars and keyboard focus remain unchanged.
- FR-005: No graph semantics, status/freshness claim, API/query, action/confirmation, sort/filter/selection, canonical Name/Logs route, auth/feature gate, dependency, global theme or storage-format change.

## Acceptance criteria

| ID | Required evidence |
|---|---|
| AC-001 | CPU and Memory column metadata is fixed160 each; with both running-container bars present, real-browser cell and track measurements are equal at wide and portrait widths. Existing fill percentages/limit/no-limit semantics are unchanged. |
| AC-002 | Default and saved column sets allocate desktop slack to pinned Name only; Open stays64 and other explicit widths remain fixed. Name-only, empty and unknown-only preferences fill the available workspace rather than remaining184. The table retains the declared184 minimum allocation plus selection44 where applicable. |
| AC-003 | Long identity still truncates with full title and visible Update badge. Existing positive/negative verdict tests, default Stack/explicit sorts, saved columns/view and canonical Name/Logs navigation pass. |
| AC-004 | Browser wide/portrait checks preserve 44px rows, fixed160 metric widths, equal tracks, thin contained horizontal scroll and no new document overflow. Selection, frozen metric sorting, resource actions/confirmations and feature/auth regression suites remain green. |

## Gates

Approved ADD/security, green baseline, AC-labelled focused/full frontend tests, TypeScript and build, then independent wide/portrait browser QA and technical writing. No production action is needed.
