# Architecture Decision Document

**Story:** APP-WORKSPACE-001 — Unified application workspace  
**FRD:** `docs/requirements/APP-WORKSPACE-001-frd.md`  
**Date:** 2026-10-20  
**Status:** Ready for security review; implementation contract

## 1. Context and detected stack

Read root `CLAUDE.md`, `web/package.json`, `web/tsconfig.json`, `web/vite.config.ts`, `web/vitest.config.ts`, `go.mod`, current App/routes, Docker shell, settings layout and UI-FOUNDATION-001 ADD. The frontend is React 19.2, TypeScript 5.9 strict/ES2020, React Router 7, TanStack Query 5, Tailwind 4.2, Vite 6.4, Recharts 3, Floating UI and xterm. Tests use Vitest 4, Testing Library/user-event and happy-dom. Go 1.24/chi/pgx serves embedded assets; it is unchanged. No root JavaScript package or ESLint script exists. TypeScript checking is the relevant lint check. Demo Vite aliases API/settings/WebSocket providers and uses the existing base path/router arrangement.

The current App has a crowded horizontal primary nav; Docker independently supplies a sidebar and scoped neutral palette. Extending that model by adding more nested navigation would preserve the inconsistency. Instead lift the grouped navigation into an authenticated AppShell and make DockerLayout only a feature gate and outlet.

## 2. Decisions

1. Keep all routes and API clients. Rename the visible Fleet collection to Devices while retaining `/`; do not add a competing `/devices` collection or change deep links.
2. One application navigation owns collection selection; collection/resource tabs remain local content navigation. Docker links move into the app sidebar; settings may retain subsection tabs.
3. AppShell is presentation and navigation only. App retains auth ownership and authenticated provider/global query lifecycles. No shell is mounted before confirmed authentication.
4. Promote the existing neutral Docker gray palette to the application surface scope; keep semantic aliases and chart/severity hues intact. Avoid mass numeric-color replacement unrelated to neutral surfaces.
5. Share one small PageHeader primitive rather than per-page header implementations. Keep domain-specific status badges, links and controls in caller slots.
6. Docker host availability filtering is frontend-only and strictly `docker_available === true`; offline known hosts remain. Do not filter Devices or mutate shared query data.

## 3. Shared interfaces and layout

### PageHeader: exact public contract

Create `web/src/components/ui/PageHeader.tsx` and export the named component and props from `web/src/components/ui/index.ts`:

```tsx
import type { ReactNode } from 'react'

export interface PageHeaderProps {
  title: ReactNode
  description?: ReactNode
  eyebrow?: ReactNode
  actions?: ReactNode
  children?: ReactNode
}

export function PageHeader(props: PageHeaderProps): React.JSX.Element
```

`children` is the optional metadata/status/relationship row. `title` renders in one h1. `eyebrow` is a small muted resource type, not a second heading. Description is muted body copy. Actions render alongside the title block on wide layouts and wrap below as needed. Metadata renders below title/description. The outer header uses a restrained bottom border and padding; descendants use min-width zero/wrapping and bounded controls. Optional slots are omitted when undefined. The component does not fetch, navigate, add breadcrumbs, infer actions, hold state or use raw HTML injection. Callers preserve accessible names and disabled/pending states of controls. No additional props are required by this story.

### AppShell

Create `web/src/components/layout/AppShell.tsx`; root integrator owns it and App wiring. A small explicit slot contract avoids importing App-local query components:

```tsx
interface AppShellProps {
  children: ReactNode
  utilities?: ReactNode
  brand?: ReactNode
  banners?: ReactNode
  dockerEnabled: boolean
}
```

The named exported AppShell renders desktop sidebar, compact topbar, banners and a single `<main id="main-content">`. `utilities` is supplied by App with the existing search button, HeaderClock, AlertsBell and UserMenu. `brand` supplies the existing logo/version content. The shell provides a skip link, primary nav landmark, accessible active state and mobile toggle. Its only state is mobile navigation visibility. It closes on pathname changes. Use existing icon patterns/components or inline decorative SVG, not a new icon dependency. If brand is omitted, use a simple rIOt link. All topbar controls remain keyboard reachable.

Desktop uses a fixed or sticky 216–240px sidebar, a 48–56px utility bar and `minmax(0, 1fr)` content. At the chosen Tailwind desktop breakpoint the sidebar becomes persistent; below it navigation collapses and content occupies full width. The collapsed panel must not remain keyboard focusable while closed. A disclosure in document flow is acceptable and avoids creating another modal/focus trap. If an overlay drawer is used, use the existing Modal primitive and its focus behavior. Keep all long tables/logs locally scrollable and retain the portrait fixes from v2.71.1. Content can use a generous shared max-width, while forms/cards retain their local readable widths.

## 4. Component changes and ownership

| Owner | Files / responsibility |
|---|---|
| Root integration | `web/src/App.tsx`; create `components/layout/AppShell.tsx`, `components/ui/PageHeader.tsx`; export in `components/ui/index.ts`; global surfaces in `index.css`; shared component/auth tests. Reuse App utility components and preserve authenticated WebSocketProvider, GlobalWSHandler, CommandPalette, update/demo banners and ScrollToTop. |
| Fleet/device engineer | `pages/FleetOverview.tsx`, `pages/Dashboard.tsx`, `pages/DeviceDetail.tsx`, `pages/DeviceProbes.tsx`, `pages/DeviceTerminal.tsx` and relevant colocated tests. Import PageHeader from `components/ui`; visible collection title Devices; preserve controls/data/tabs. Dashboard subcomponents change only where necessary for consistent surfaces. |
| Monitoring engineer | `pages/Probes.tsx`, `pages/ProbeDetail.tsx`, `pages/Alerts.tsx`, `pages/Security.tsx`, `pages/LearnMore.tsx` and relevant tests. Preserve all probe mutations, findings/acknowledgements and guidance. |
| Root settings integration | `pages/SettingsLayout.tsx`, all six `pages/settings/*Settings.tsx` plus `AgentManagement.tsx`, their tests and `AlertRuleSettings.tsx`. Settings layout supplies subsection navigation; each content page supplies one PageHeader. Remove the layout h1 if it duplicates the subsection header; use eyebrow Settings. |
| Root Docker integration | `pages/docker/DockerLayout.tsx`, `HostsPage.tsx`, `ContainersPage.tsx`, `StacksPage.tsx`, `StackPage.tsx`, `InventoryPage.tsx`, `ActivityPage.tsx`, `ContainerPage.tsx`; associated tests. Remove independent sidebar, use shared header for top-level/detail content, filter Docker host options/counts without losing offline hosts. Embedded Activity must not introduce another page h1; retain its current scoped form. |
| Root integration if needed | `components/CommandPalette.tsx` for Devices terminology and Docker host visibility only; `components/ui/README.md`, `theme/tokens.ts` and token tests if global token definitions genuinely change. Preserve API alias patterns. |

Agents must not edit shared shell/header/global CSS concurrently; root creates those contracts first. No implementation files are changed by the upstream author. Any additional component changes must be necessary consequences of the listed parent page changes and documented in the implementation report.

## 5. Routes, active matching and data boundaries

| Group / item | Destination | Active behavior |
|---|---|---|
| Infrastructure / Devices | `/` | exact `/` or `/devices/` subtree |
| Infrastructure / Overview | `/dashboard` | exact route |
| Docker / Containers | `/docker/containers` | exact collection and `/docker/containers/` details |
| Docker / Stacks | `/docker/stacks` | exact collection and `/docker/stacks/` details |
| Docker / Hosts, Images, Volumes, Networks | corresponding existing `/docker/*` | exact destination, with segment-boundary descendants if added already by routing |
| Observe / Probes | `/probes` | exact collection or `/probes/` detail; device probes still select Devices |
| Observe / Alerts | `/alerts` | exact route |
| Administration / Alert Rules | `/alert-rules` | exact route; do not accidentally match Alerts |
| Observe / Security | `/security` | exact route and `/learn/` guidance |
| Observe / Activity | `/docker/activity` | exact route; Docker feature-gated |
| Administration / Agents | `/settings/agents` | exact route |
| Administration / Settings | `/settings` | exact root or `/settings/` descendants except `/settings/agents` |

Use route-segment-aware matching, `aria-current="page"` on the active item, and a visible active treatment. Query strings do not change the owning collection. Existing `/docker` and settings index/legacy redirects remain. The notification bell is a utility link, not another primary navigation current marker. No feature catalog semantics are broadened: Docker hides via `docker`; current section toggles continue to govern their existing content. Do not newly hide the whole Security route based on a device-detail-only feature.

Docker uses `dockerApi.hosts` / `['docker-hosts']` as before. Compute filtered copies at render time, or use a small pure local helper; never rewrite the shared cache. Apply availability filtering before empty-state detection, counts and all Docker host option rendering. Inventory already has a separate per-host result shape; derive its selectable IDs from the Docker-host query when needed while preserving existing loading/error behavior. Do not require a host to be online or currently return containers. Query failures remain errors, not empty arrays passed as successful data. The fleet `api.getDevices` result remains complete.

## 6. Authentication, security and compatibility

App must call `useFeatures(authenticated && !loading && !needsSetup)` before early returns, then mount AppShell and authenticated global components only after those checks. AppShell must not independently query protected endpoints. Preserve existing logout, cookie auth, OIDC and setup behavior. Keep command palette open-only resource queries and terminal keyboard exemptions. Do not expose secrets in new metadata slots, change URL escaping, introduce external redirects from router state, or weaken infrastructure confirmation dialogs. Existing safeUrl, masked compose/inspect and log redaction remain unchanged. Keep collection-return validation and canonical host/name resource links.

No schema, agent, API, environment, retention, image/deployment configuration, dependency or build-tool changes. Existing feature-disabled deep links retain their current disabled behavior. Demo aliases and base paths continue to resolve. Prior DOCKER-UX-001 requirements for logs, read-only Compose and capability handling and UI-FOUNDATION accessibility/error conventions remain binding.

## 7. Performance and failure behavior

This is composition and styling, not new polling. Reuse existing utility queries and cache keys; avoid mounting an extra hosts/device query solely to paint the global navigation. AppShell must not block rendering on a fleet/hosts roundtrip. Existing content retains its query loading/error/empty boundaries and refresh intervals. Keep table virtualization/local scroll, terminal sizing and chart responsive parents. Avoid a global overflow rule that masks unreachable controls; verify long names, large actions, tables and mobile menus at actual viewport widths.

## 8. Acceptance and verification mapping

| FRD AC | Required coverage |
|---|---|
| AC-001–003 | AppShell component/router tests for groups, Docker feature flag, unique route ownership and mobile aria-expanded/route-close. Browser desktop + 390px portrait screenshots and keyboard check. |
| AC-004 | Extend/re-run `App.auth.test.tsx`: pending/login/setup no protected requests, OIDC remains rendered and stable; authenticated route gets shell. |
| AC-005 | Utility controls and existing palette/terminal tests. Account/alert destinations remain existing routes. |
| AC-006 | PageHeader component tests for required title/optional slots and one h1; visual wrap check for long titles/actions. |
| AC-007 | Existing fleet/device/dashboard/probes/alerts/security/settings interaction tests plus representative PageHeader assertions. Scoped/embedded views must avoid duplicate h1. |
| AC-008 | Docker host/workspace and filter tests with online available, offline available and online unavailable fixtures; device collection unchanged. Test all-non-Docker empty state. |
| AC-009 | Existing DockerPages, workspace, LogView and action/confirmation tests; legacy/direct resource routes; saved filters. |
| AC-010 | Source/token consistency tests as applicable; browser visual verification because happy-dom does not resolve Tailwind layout/colors. |
| AC-011 | New focused tests, complete `npm run test:run`, `npx tsc --noEmit`, `npm run build`, demo build with VITE_DEMO=true; document outputs and any pre-existing baseline failures. No dependency/migration diff. |

Use mandatory `[APP-WORKSPACE AC-xxx]` test names for new coverage, mock network, isolate browser storage and inject clocks where needed. The root integrator ran the pre-change full frontend baseline: 80 files / 1,499 tests passed. Any subsequent pre-existing failure routes through CLAUDE.md blocker protocol. No Go checks are newly required for unchanged Go code beyond existing CI. Security review precedes development; QA follows complete implementation and the implementation report; technical writing follows QA PASS/PASS WITH NOTES. Reports live under the standard story-specific `docs/security`, `docs/implementation`, `docs/qa` and `docs/technical-writing` paths.

