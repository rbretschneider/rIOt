# DOCKER-STACK-005 implementation report

Stack: React19, TypeScript5.9, Tailwind4, TanStack Query5, existing shared
Button/DataTable/dialog and Vitest/Testing Library. No dependencies added.

Stacks with updates >0 now have an amber Update (N) action and singular/plural
Updates-cell pill. A title explains the available container update count. Other
counts retain neutral Update and their numeric count; no freshness claim added.
Exact host/project accessible labels, identity links, row widths, dialog opening,
fresh gates and mutation/confirmation behavior are unchanged. Warning classes
use existing semantic tokens with local important overrides of shared defaults;
shared Button and global styles are unchanged.

## Validation and acceptance

AC001: tests cover1and15, warning button/count/title and singular/plural pill.
AC002: tests cover0and-1 neutral presentation without Up-to-date claims.
AC003: highlighted controls open exact existing dialog without dispatch;
existing filters/target isolation/dialog fresh gates and confirmation tests pass.
AC004: actual demo Chromium wide1920x1080 and portrait1200x1600 inspected.
Measured button24px/row44px/action112px and no page horizontal overflow at1200.
Measured single-digit action71.91px with88px available cell content; DOM Range
extra-digit width4.84px gives a two-digit estimate76.75px (15 is covered in the
component test, not a modified browser fixture). Positive computed background
oklch(0.769 0.188 70.08), black text; conversion gives9.79:1 contrast. Keyboard
Tab reaches the positive action with focus-visible true and existing2px ring.
Screenshots retained ignored bin/stack_visibility_005/{portrait,wide,focus}.png.
Viewport override reset. Browser performed observation/keyboard only; no update
command dispatched. Demo progress fixtures are irrelevant to this presentation.

Baseline full86files/1578tests passed before edits; focused2files/60tests and
final full86files/1582tests pass. Production build including TypeScript passes;
existing chunk-size warning remains. No lint script exists. git diff --check
passes. Full output retained in adjacent DOCKER-STACK-005-validation.txt.
No backend change, so Go tests were not rerun locally; normal PR CI remains.

Ready for independent QA on committed clean implementation.
