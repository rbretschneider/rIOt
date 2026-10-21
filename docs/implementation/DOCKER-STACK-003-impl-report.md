# DOCKER-STACK-003 implementation report

Stack: React19/TypeScript5.9, TanStack Query5, Tailwind4, Vite6,
Vitest4/Testing Library. No new dependency, API, agent or server change.

## Implementation

Stacks collection now has a compact Update action beside Host. One selected
device/project-keyed dialog outside the virtual table loads fresh detail/host
data, checks agreed/nonempty paths and capability, and sends the existing
stackAction update only on explicit confirmation. It uses the existing tracker
and command progress for busy state, conflicts, failures, explicit retries and
active-command adoption. A synchronous guard prevents duplicate creation.
Terminal results refresh the stack detail, collection and container queries.
Closing an accepted command's observer does not cancel it.

Detail now exposes exactly one Update even at zero known updates or when down/
partially running. Existing primary actions and typed confirmations remain.
Update submit rechecks gates/busy state with a synchronous guard. Both
confirmations explain pull/up, changed-service recreation, stopped-service
start, exact Compose context and existing self-infrastructure warning.

## Validation

Baseline1559tests passed before edits. Focused page/dialog55tests passed,
then final full86files/1578tests passed including19 new cases. TypeScript and
production build pass. No lint script exists; git diff --check passes. Full
baseline/focused/final-suite/build output is retained in the adjacent
DOCKER-STACK-003-validation.txt. Initial test selectors used an unsupported
Testing Library exact option and duplicated Close labels; corrected selectors
and explicit dismiss-button label before final validation. No baseline failure.

AC001/007: in-place collection filters, close/no mutation, same project on
two hosts with late old response and exact host-scoped dispatch/progress.
AC002: detail Update uniqueness for running+available, running+zero, partial,
and down. AC003: delayed fresh reads over cached agreed paths, pending/conflict/
empty directory, missing capability and stale-cache refetch errors. AC004:
host/project/path/consequences, self warning, close/no dispatch. AC005: exact
payload, repeated-click guard, existing-command conflict adoption. AC006:
retained failure, dispatch refusal/no retry, once-only terminal invalidation,
active adoption and no cancellation on observation close. Existing Down/self
host, paths, logs, filtering and other action regressions remain green.

Actual local demo Chromium checked1920x1080 and1200x1600. Update remains near
identity; a24px button preserves44px rows (initial default button measured47px
rows and was tightened). At1200px document width1200, dialog448px, confirmation
fits; stack table retains internal scrolling. Screenshots are retained in
ignored bin/stack_update_003/list_wide.png, confirm_portrait.png and
progress_portrait.png. Simulated commands only; no production update was run.
The pre-existing dockerDemo command fixture returns a generic succeeded
container-restart record for stack mutations; screenshot progress therefore
does not establish real Compose output/action labels. Mocked tests verify the
real exact update request and scoped command ID. This demo limitation was not
silently changed outside the approved frontend controls scope. Browser viewport
override reset after visual checks.

Existing Vite chunk-size warning remains nonblocking. Ready for independent QA
against the committed implementation; no production deployment authorized here.
