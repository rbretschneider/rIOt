# DOCKER-TABLE-006 implementation report

Stack: React19, TypeScript5.9, existing DataTable, Tailwind4, Vitest/Testing
Library. No dependency, API, storage or global-component change.

Changed only ContainersPage metadata: CPU64 to160, Memory stays160, and Name
is the grow column instead of Open. Name keeps its declared184 minimum and
pinned identity; Open stays64. Existing DataTable already supports pinned grow,
so no shared component edit is needed. CPU and Memory fill scales/labels,
update badges, sorting/filtering, routes and confirmations are unchanged.

## Validation and acceptance

AC001/002: default/custom restored columns have fixed160 CPU/Memory and64Open,
with only Name width unspecified. Existing Name-only/empty/unknown preferences
now fill the workspace, keeping minimum228 including selection. Tests replace
obsolete TABLE-002 compact-Name assertions; its generic pinned/no-grow shared
DataTable tests remain unchanged. CPU2%/Memory50% fill regressions pass.
AC003: existing long-name/badge/verdict/sort/link and saved-choice tests pass.
AC004: actual local demo Chromium measured wide1920x1080: table1628, Name884,
CPU160/Memory160, both tracks136, Open64, rows44. Portrait1200x1600: table1140,
Name396, same metric/track/Open/row sizes; document width1200. Narrow900x1200:
table928 within840px viewport, Name184, metrics160; document remains900 and
overflow is contained in the table. Name-only via actual Columns controls:
table840, Name795.5 plus selection, all4 known update badges retained. Restored
all default choices afterward and reset viewport. No production command ran.
Screenshots ignored bin/table_widths_006/wide.png and portrait.png inspected.

Baseline86files/1582tests passed before code. Focused3files/62tests and final
full86files/1584tests pass. Production build including TypeScript passes; existing
chunk-size warning remains. No lint script exists. git diff --check passes.
Full output retained in adjacent DOCKER-TABLE-006-validation.txt. No backend
change, so Go tests were not rerun locally; normal PR CI remains.

Ready for independent QA on committed clean implementation.
