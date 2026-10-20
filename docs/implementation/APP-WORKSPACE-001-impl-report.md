# Implementation report — APP-WORKSPACE-001

Date: 2026-10-20. Base: `7b3123f`. Branch: `codex/unified-application-workspace`.

## Outcome and stack

Implemented the approved FRD/ADD after security approval. React 19.2, TypeScript 5.9 strict, React Router 7, TanStack Query 5, Tailwind 4.2, Vite 6.4 and Vitest 4/happy-dom remain unchanged. No new dependency, API, agent, database or deployment configuration changes.

AppShell owns one grouped application navigation, a compact utility bar and one main content landmark. Desktop navigation is persistent at 1280px; below that width it becomes an in-flow disclosure. Selecting a route or pressing Escape closes it. The skip link focuses content without corrupting HashRouter's location. Authentication guards and the authenticated feature-query condition precede every shell/global-query mount.

PageHeader provides title, description, type, actions and contextual metadata slots. Devices, Overview, device detail/probes/terminal, monitoring pages, Docker and settings use it. Account/search/clock/unread notifications remain available on mobile. Neutral gray surfaces and existing thin scrollbars now apply globally; chart and status colors are preserved. DockerLayout is only its existing feature gate and outlet, without another sidebar or navigation-only host query.

Docker host listings and options derive copies filtered by `docker_available === true`. Offline known Docker hosts remain eligible. Devices and the shared host cache remain complete. No Docker hosts, no search matches and request failure have separate states. Existing stored/live logs, confirmations, scoped resource URLs and return-state validation remain intact.

The only additional shared component change beyond the named page files is DeviceQueryFallback's “Back to Devices” label, with corresponding legacy tests. This is a necessary consequence of the canonical Devices terminology. CommandPalette's placeholder uses “device”; its queries, resource types and terminal shortcut exemptions are unchanged.

## Verification

Baseline before implementation: 80 files / 1,499 tests passed. Final full frontend suite: **85 files / 1,538 tests passed**. Focused final shell/auth/settings subset: 4 files / 33 tests passed; specialists also passed Fleet/device, monitoring and Docker suites. TypeScript `npx tsc --noEmit`, production `npm run build`, and demo Vite build with `VITE_DEMO=true` passed. Full captured focused/full/type/build output is in [validation output](APP-WORKSPACE-001-validation.txt).

Sandboxed Vite initially could not rename its temporary cache; validation used normal runtime permissions. New auth assertions initially selected both the Alerts navigation and utility links; corrected to assert both explicitly. TypeScript caught an unsupported Testing Library option; removed it. Legacy fallback tests were updated to the Devices label. Final tests pass. Existing fixture/query/act warnings and Vite's existing large-chunk advisory remain; these are not new runtime failures.

| Acceptance | Evidence |
|---|---|
| AC-001–003 | AppShell's 19 route/disclosure/skip-link tests; Docker boundary tests. Single current app-navigation item for representative routes; Docker-disabled links absent. |
| AC-004–005 | App.auth tests exercise pending, setup and anonymous OIDC without protected requests/reloads; authenticated utilities remain mounted. Existing palette/terminal guards retained; browser search/account checks below. |
| AC-006–007 | PageHeader slot/title tests and new representative Devices/device/monitoring/settings action/context tests. Existing mutation and confirmation regressions pass. |
| AC-008 | Online/offline Docker and non-Docker fixtures cover Hosts, all Docker host filters, unchanged cache and complete Devices. Empty and query-error cases tested separately. |
| AC-009 | DockerPages/workspace, container/stack/log/action and encoded-link regressions pass. Terminal remains an explicitly opened route. |
| AC-010 | Actual browser checks at 390×844, 1080×1760, 1280×900 and 1920×1080; global neutral backgrounds, semantic status/chart colors and thin scrollbars visible. |
| AC-011 | Full test suite, type checking, production and demo builds passed. No package/lock, backend or migration diff. |

## Browser evidence and limits

Used the existing local simulated-data preview at `http://127.0.0.1:5177/rIOt/`. Verified Devices, Overview, Notifications and Docker Hosts/Containers. At 390px the closed navigation is absent from the accessibility tree, utilities are visible, opening/selection closes the menu and content remains usable. Search opens its existing modal and Account exposes Change Password/Logout. At 1080px the Docker table's scroll/client widths both measured 1020px, so no horizontal table scroll was needed. At 1280px navigation is persistent and document width remains 1280px. At 1920px Overview charts retain their colors and space. Representative pages render one h1.

Screenshots retained in the task's visualization directory: `app_workspace_devices.png`, `app_workspace_overview.png`, `app_workspace_mobile.png`. These are demo evidence, not a claim of production deployment or live destructive-action testing. Existing automated action/log tests provide the interaction evidence. Production remains v2.71.1; this story requires normal review/release and any specific infrastructure approval before rollout.

## Handoff

Implementation and checks are complete. Independent QA should review the committed source, upstream documents and validation evidence; technical writing follows QA PASS/PASS WITH NOTES.
