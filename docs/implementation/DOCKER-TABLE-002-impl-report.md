# DOCKER-TABLE-002 implementation report

Stack: React19, TypeScript5.9, Vite6, Tailwind4, Vitest4/Testing Library.
No dependency, server, agent or production deployment changes.

## Changes

- Keep mandatory pinned Name184px; Open absorbs available desktop space using
  the existing DataTable sizing contract. Pinned-only tables retain their summed
  widths and never implicitly grow a pinned last column.
- Move Update available marker from optional Image into a nonshrinking sibling
  of the truncated identity link, preserving the full identity/image tooltip.
- Reuse authoritative update verdict precedence, including legacy boolean
  fallback. Stopped containers retain informational availability; actions and
  eligibility are unchanged. Align existing DockerRow verdict annotation with
  the shared UpdateStatus union; no casts/new API fields.
- Default missing/invalid sort to Stack ascending; within a stack use hostname,
  container name and row key. Explicit sort URLs/preferences remain authoritative.

## Acceptance and validation

AC001 sizing/pinned restoration, AC002 hidden-Image visibility/no duplicates,
AC003 nine verdict cases plus stopped availability, AC004 fallback/direction/
stable host/name ordering/explicit Name sort, AC005 saved columns/routes/actions
are covered by named tests and existing regressions.

Baseline85files/1541tests passed before code changes. Focused3files/47tests pass;
full85files/1556tests pass. Production npm run build passes (TypeScript plus
Vite). package.json has no lint script; git diff --check passes. Full command
output is retained in DOCKER-TABLE-002-validation.txt. Sandbox worker startup
initially prevented test initialization; the same baseline passed outside the
sandbox. Initial new tests incorrectly assumed stopped update eligibility and
unambiguous Name/Stack link labels; tests corrected to preserve existing behavior.
Initial typecheck found the wider pre-existing string verdict annotation;
architect/security approved correction to existing UpdateStatus before the fix.

Actual Chromium demo preview inspected at2560x1392 and1200x1600. Name measured
184px and rows44px; Stack ascending selected, four update badges visible with
Image hidden. At1200px document width1200, table1140, no table horizontal
overflow and marker inside Name bounds. Wide and portrait screenshots retained
in ignored bin/docker_table_002/wide.png and portrait.png. Demo uses simulated
data; no production action invoked. Browser viewport override reset afterward.

## Independent-review correction

QA found saved Name-only, empty or unknown-only column choices still triggered
DataTable's last-column grow fallback. Added failing AC001 regressions before
the fix; all four relevant assertions failed on the prior implementation.
Architect/security approved constraining fully sized, pinned-only tables to
their summed width and preventing implicit pinned-column growth. No filler
columns, selectable rows, grid-count or virtualization changes.

Final focused50tests and full1559tests pass; production build passes again.
Actual browser Name-only measurement: Name184px, table228.5px including borders,
two semantic columns including selection, four update markers. Screenshot:
bin/docker_table_002/name_only.png. Default preferences restored afterward.

Vite retains its existing large-chunk warning. No separate lint command exists.
Ready for independent QA against the corrective commit.
