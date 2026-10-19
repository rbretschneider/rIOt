# Implementation Report

**Story ID:** UI-FOUNDATION-001
**Engineer:** Senior Dev Agent (Frontend Engineer specialist)
**Date:** 2026-10-17
**Branch:** `claude/exciting-hodgkin-4ab37d` (not tagged, not pushed)

## Detected stack

| Source | Finding |
|---|---|
| `web/package.json` | React 19.2, react-router-dom 7, TanStack Query 5, recharts 3, `@floating-ui/react` (Tooltip only), Tailwind 4.2 through `@tailwindcss/vite`, vitest 4 + happy-dom, Testing Library + user-event. No ESLint. |
| `web/tsconfig.json` | `strict`, `target` ES2020, `types: ["vitest/globals"]`. "Lint" for this story is `npx tsc --noEmit`. |
| `web/vitest.config.ts` | happy-dom, globals, setup file `src/test/setup.ts`. Vitest does not process CSS. |
| `web/vite.config.ts` | Demo build swaps `api/client`, `api/settings` and `contexts/WebSocketProvider` by regex alias when `VITE_DEMO=true`. |
| Backend | Go server and agent exist but are out of scope (NFR-010). No Go file changed. |

Specialists active: Frontend Engineer only. No database, backend or infrastructure work.
No dependency was added or upgraded (`git diff main -- web/package.json web/package-lock.json` is empty).

## Commits

| # | SHA | Subject |
|---|---|---|
| 1 | 2db5f77 | feat: design tokens, z-index scale and chart color constants |
| 2 | 795e2ed | feat: typed ApiError for API clients |
| 3 | f26833f | feat: canonical format module and time-zone preference |
| 4 | e980ba7 | feat: UI primitives (Button, Card, Badge, Modal, ConfirmDialog, EmptyState, Skeleton, QueryState, Toast, RelativeTime, Timestamp) |
| 5 | a6d47c5 | feat: mount toast and time providers; header clock toggle |
| 6 | 96fc988 | refactor: rebuild ConfirmModal and UpdateConfirmModal on Modal |
| 7 | 05779dc | feat: replace native confirm() with ConfirmDialog; migrate settings modals to Modal |
| 8 | c42350f | feat: FleetOverview toasts, honest error states, PatchReviewModal on Modal |
| 9 | 49f55dc | feat: DeviceDetail command toasts, delete dialog, honest device-page states, header polish |
| 10 | 95006d0 | refactor: route timestamps, durations and enum labels through shared modules; chart colors |
| 11 | 4017cc2 | fix: render hostnames as reported; Security stat cards in one row |
| 12 | a1987ce | refactor: remove dead components and container drawer; add static guards |
| 13 | 35d7b62 | feat: demo clients at parity with real clients; demo WebSocket connected |
| 14 | fd95dd2 | test: AgentManagement update-all dialog focuses Cancel (added after the plan, SEC-001) |
| 15 | (this commit) | docs: UI-FOUNDATION-001 implementation report |

The ADD plan listed 14 commits (the 14th being this report). One extra small test commit (#14) was inserted before it.
Every commit left `npx tsc --noEmit` and `npm run test:run` green.

## Completed components (files changed)

112 files changed, 8,370 insertions, 1,214 deletions, all under `web/` (plus this report under `docs/`).
CHANGELOG.md and README.md are untouched (technical writer owns them, DEV-18).

**Created (source)**
`theme/tokens.ts`, `theme/chartColors.ts`, `api/errors.ts`, `contexts/TimePreferenceProvider.tsx`, `hooks/useMinuteTick.ts`, `hooks/featureCatalog.ts`,
`components/HeaderClock.tsx`, `components/DeviceQueryFallback.tsx`,
`components/ui/` (`index.ts`, `focusRing.ts`, `Button.tsx`, `Card.tsx`, `labels.ts`, `Badge.tsx`, `Modal.tsx`, `ConfirmDialog.tsx`, `EmptyState.tsx`, `Skeleton.tsx`, `QueryState.tsx`, `Toast.tsx`, `RelativeTime.tsx`, `Timestamp.tsx`),
`test/renderWithProviders.tsx`.

**Modified (source)**
`index.css` (tokens, z-scale, toast keyframes), `main.tsx`, `App.tsx`, `api/client.ts`, `api/settings.ts`, `api/demo-client.ts`, `api/demo-settings.ts`, `api/demo-data.ts`,
`contexts/DemoWebSocketProvider.tsx`, `hooks/useFeatures.ts`, `utils/format.ts`, `utils/rebootEta.ts`, `utils/docker.ts`, `utils/cron.ts`,
`components/`: `ConfirmModal`, `UpdateConfirmModal`, `ActivityLog`, `ContainerDetail`, `MetricChart`, `ContainerMetricChart`, `Tooltip`, `SeverityBadge`, `SecurityScoreGauge`, `SecurityScoreModal`, `CreateAlertDialog`,
`pages/`: `FleetOverview`, `DeviceDetail`, `DeviceContainers`, `ContainerDetailPage`, `DeviceProbes`, `Probes`, `ProbeDetail`, `Alerts`, `Security`, `LearnMore`, `dashboard/ActivityRiver`, `dashboard/PerDeviceCharts`, `settings/AgentManagement`, `settings/AlertRuleSettings`, `settings/LogSettings`, `settings/NotificationSettings`, `settings/CertificateSettings`.

**Deleted**
`components/Sparkline.tsx`, `components/MiniScore.tsx`, `components/ContainerGroup.tsx`, `components/ContainerCard.tsx`, the default-export drawer in `components/ContainerDetail.tsx`, `formatContainerUptime` in `utils/docker.ts`.

**Tests created**
`theme/tokens.test.ts`, `api/errors.test.ts`*, `api/demo-parity.test.ts`, `contexts/TimePreferenceProvider.test.tsx`, `contexts/DemoWebSocketProvider.test.tsx`, `hooks/useMinuteTick.test.tsx`*,
`components/HeaderClock.test.tsx`, `components/UpdateConfirmModal.test.tsx`, one test file per primitive under `components/ui/`,
`pages/Alerts.test.tsx`, `pages/DeviceContainers.test.tsx`, `pages/DeviceDetail.flows.test.tsx`, `pages/DeviceProbes.test.tsx`, `pages/FleetOverview.states.test.tsx`,
`pages/settings/CertificateSettings.test.tsx`, `pages/settings/LogSettings.test.tsx`, `pages/settings/NotificationSettings.test.tsx`, `test/static-guards.test.ts`.
(*) not listed in ADD Section 4; added for helpers that deserve direct coverage (see Deviations).

**Tests extended**
`api/client.test.ts`, `utils/format.test.ts`, `utils/rebootEta.test.ts`, `utils/cron.test.ts`, `components/ConfirmModal.test.tsx`, `components/ActivityLog.test.tsx`,
`pages/FleetOverview.test.tsx`, `pages/DeviceDetail.test.tsx` (harness only), `pages/ContainerDetailPage.test.tsx`, `pages/Probes.test.tsx`, `pages/Security.test.tsx`,
`pages/dashboard/PerDeviceCharts.test.tsx`, `pages/settings/AgentManagement.test.tsx`, `pages/settings/AlertRuleSettings.test.tsx`.

## Security conditions (override the ADD where they conflict)

| Condition | How it is met | Tests |
|---|---|---|
| SEC-001 opening gesture can never confirm | `ConfirmDialog` never autofocuses Confirm: initial focus is **Cancel for danger and primary** (overrides DEV-11). `ConfirmModal`, `UpdateConfirmModal` and `PatchReviewModal` pass `initialFocusRef` to Cancel. Modal's default fallback focuses the first body control, then the footer (Cancel precedes Confirm), never the close X. | `ConfirmDialog.test` (`[UIF SEC-001]`: no Confirm focus for either variant, held `{Enter>4/}` after keyboard open, repeat keydown, double click); `FleetOverview.test` (`[UIF SEC-001]`: held Enter on "Update Agents", double click, Cancel focused; PatchReviewModal Cancel focused); `DeviceDetail.flows.test` (Patch, Update Agent focus, held Enter on Patch); `AgentManagement.test` (Cancel focused); `ConfirmModal.test` and `UpdateConfirmModal.test`. Mutation check: moving focus to Confirm fails 2 FleetOverview SEC-001 tests. |
| SEC-002 delete scope fixed at open | `DeviceDetail` stores `{ uninstall }` in `deleteScope` when Delete is clicked. Message and `api.deleteDevice(id, uninstall)` both read that snapshot. `deleteMutation` takes `uninstall` as its variable and no longer reads live status. | `DeviceDetail.flows.test` `[UIF SEC-002]`: online->offline flip before confirm, offline->online flip before confirm, flip between a failed attempt and the retry (both calls send `false`), scope re-read on the next opening. |
| SEC-003 late rejection reported once | `ConfirmDialog` tracks a mounted ref. If the action rejects after unmount it raises one `toast.error('Action failed', '<title>: <message>')`. While mounted it shows the inline error only (never both). | `ConfirmDialog.test` `[UIF SEC-003]` (unmount while pending then reject -> exactly one `role=alert` toast; inline failure raises no toast; success after unmount still toasts but skips `onSuccess`/`onClose`). |
| SEC-004 no Modal from a z-[60] overlay | Nothing opens a Modal or ConfirmDialog from `SecurityScoreModal` or `SetupGuide`. `SecurityScoreModal` is unchanged except for the severity label text. | Review. |
| SEC-005 text-only sinks, long text wraps | Static guard bans `dangerouslySetInnerHTML`, `.innerHTML =`, `.outerHTML =` and `insertAdjacentHTML` in non-test source. Toast text uses `wrap-anywhere`; Modal/ConfirmDialog/EmptyState/ErrorPanel text uses `break-words`/`wrap-anywhere`; the Modal footer sits outside the scrolling body and the panel has `max-h-[calc(100vh-2rem)]`; toast viewport is `w-80 max-w-[calc(100vw-2rem)]` with the Dismiss button `shrink-0`. | `static-guards.test` (`[UIF SEC-005]`), `[UIF SEC-005]` describes in `Modal`, `ConfirmDialog`, `Toast`, `EmptyState`, `QueryState` tests (5,000-character unbroken strings; markup in a toast renders as text). Visual confirmation of a multi-kilobyte hostname is left to Clod. |
| SEC-006 tampered `riot-time-zone` | Only the exact string `Local` selects Local. Storage value never reaches `Intl`. `localTimeZone` is a prop only tests set. | `TimePreferenceProvider.test` `[UIF SEC-006]` ("banana", "America/Nowhere", "local", JSON, quoted JSON, 100 KB string, empty string, throwing `getItem`; `localTimeZone` stays unset), `HeaderClock.test` (AC-116 plus a stored IANA name). |
| SEC-007 401 branches untouched | Only `throw new Error(\`HTTP ...\`)` lines were rewritten (sed on the exact non-401 patterns). `git diff` of `client.ts`/`settings.ts` contains no line mentioning `Unauthorized`. | `client.test` `[UIF AC-089]`: fetchJSON, sendCommand, settings fetchJSON and settings mutate reload and throw a plain `Error('Unauthorized')` that is `not.toBeInstanceOf(ApiError)`. |
| SEC-008 demo isolation | Static guard on demo imports; production `dist` grep below. | `static-guards.test` (`[UIF SEC-008]`, with a positive control for the pattern); `demo-parity.test` `[UIF AC-131]` covers every non-read method of `api`, `settingsApi` and `authApi` and a coverage test fails if a new mutator is added without an entry. |

### SEC-008 production build grep (recorded, per the security review)

Command, from `web/`, after `npm run build` (output in `web/dist`, gitignored):

```
grep -rl "proxmox-01" dist                                    -> (empty)   0 files
grep -rl "Demo mode: certificate not regenerated" dist        -> (empty)   0 files
```

Positive control on the demo build (`VITE_DEMO=true`): `proxmox-01` appears in 1 file and the regenerate message in 1 file, so the markers are real and the production result is meaningful.

## Dead-code verification (AD-012)

Before deletion:

```
$ grep -rnE "from ['\"][./]+(.*/)?(Sparkline|MiniScore|ContainerGroup|ContainerCard)['\"]" web/src
web/src/components/ContainerGroup.tsx:7:import ContainerCard from './ContainerCard'      <- ContainerGroup is itself deleted
$ grep -rnE "import ContainerDetail\b|import ContainerDetail from|default as ContainerDetail" web/src
(no output)
$ grep -rn formatContainerUptime web/src
web/src/components/ContainerCard.tsx:2:   import { ..., formatContainerUptime, ... }    <- deleted file
web/src/utils/docker.ts:144:             export function formatContainerUptime       <- the definition being removed
```

(`ContainerDetail.tsx` had already moved to `formatDuration` in commit 10.)
After deletion: `npx tsc --noEmit` is clean and `static-guards.test.ts` `[UIF AC-120]`/`[UIF AC-121]` pass.
`utils/docker.ts` `groupContainers` and the `ContainerGroup` interface lost their only consumer and were left in place (deferred, ADD AD-012).

## Test summary (AC coverage)

Describe titles carry `[UIF AC-0xx]`. Paths are relative to `web/src/`.

| AC | Test file(s) | Status |
|---|---|---|
| 001, 002, 003, 007 | `theme/tokens.test.ts` | PASS |
| 004, 005, 031, 063, 064, 108, 110, 120, 121 (export shape) | `test/static-guards.test.ts` | PASS |
| 006 | screenshot `after-fleet.png` | MANUAL (Clod) |
| 008, 009, 010, 011, 012 (Button) | `components/ui/Button.test.tsx` | PASS |
| 012 (Modal close, toast close, Retry, header clock) | `Modal.test.tsx`, `Toast.test.tsx`, `QueryState.test.tsx`, `components/HeaderClock.test.tsx` | PASS |
| 013 | `components/ui/Card.test.tsx` | PASS |
| 014, 015, 016, 017, 018, 019 | `components/ui/labels.test.ts` (014 also `AlertRuleSettings.test`, 017 also `ActivityLog.test`) | PASS |
| 020 | `components/ui/Badge.test.tsx` | PASS |
| 021 | `pages/Alerts.test.tsx` | PASS |
| 022, 023, 024, 025, 026, 027, 028 | `components/ui/Modal.test.tsx` | PASS |
| 029 | `ConfirmDialog.test.tsx` (stack of ConfirmDialog over Modal), `Modal.test.tsx` | PASS |
| 030 | `components/ConfirmModal.test.tsx`, `components/UpdateConfirmModal.test.tsx`, `pages/FleetOverview.test.tsx`, plus `ActivityLog`, `AlertRuleSettings`, `NotificationSettings` | PASS |
| 032 to 041 | `components/ui/ConfirmDialog.test.tsx` | PASS |
| 042 | `pages/FleetOverview.test.tsx` | PASS |
| 043 | `components/ui/EmptyState.test.tsx` | PASS |
| 044 | `components/ui/Skeleton.test.tsx` | PASS |
| 045 to 050 | `components/ui/QueryState.test.tsx` | PASS |
| 051, 053 to 062 | `components/ui/Toast.test.tsx` | PASS |
| 052 | screenshot `after-toast-stack.png` | MANUAL (Clod) |
| 065 | `pages/DeviceProbes.test.tsx` | PASS |
| 066, 067 | `pages/Probes.test.tsx` | PASS |
| 068 | `pages/settings/AgentManagement.test.tsx` | PASS |
| 069, 070 | `pages/settings/AlertRuleSettings.test.tsx` | PASS |
| 071 | `pages/settings/LogSettings.test.tsx` | PASS |
| 072 | `pages/settings/NotificationSettings.test.tsx` | PASS |
| 073, 074, 081 to 087 | `pages/DeviceDetail.flows.test.tsx` | PASS |
| 075 to 080 | `pages/FleetOverview.test.tsx` | PASS |
| 088, 089 | `api/client.test.ts` (+ `api/errors.test.ts`) | PASS |
| 090 to 093 | `pages/FleetOverview.states.test.tsx` | PASS |
| 094, 095, 098, 100 | `pages/DeviceDetail.flows.test.tsx` | PASS |
| 096, 097 | `pages/DeviceContainers.test.tsx`, `pages/ContainerDetailPage.test.tsx` | PASS |
| 099 | `pages/ContainerDetailPage.test.tsx` | PASS |
| 101 to 106, 118 | `utils/format.test.ts` | PASS |
| 107 | `utils/rebootEta.test.ts` | PASS |
| 109 | `ActivityLog.test.tsx`, `pages/Alerts.test.tsx`, `pages/settings/CertificateSettings.test.tsx` (+ `Timestamp.test.tsx`, `cron.test.ts`) | PASS |
| 111, 112 | `components/ui/RelativeTime.test.tsx` (+ `hooks/useMinuteTick.test.tsx`) | PASS |
| 113, 114, 115, 116 | `components/HeaderClock.test.tsx`; 116 also `contexts/TimePreferenceProvider.test.tsx` | PASS |
| 117 | `contexts/TimePreferenceProvider.test.tsx`, `components/ui/Timestamp.test.tsx` | PASS |
| 119 | screenshot `after-device-detail-chart-tooltip.png` | MANUAL (Clod) |
| 121 (render) | `pages/ContainerDetailPage.test.tsx` | PASS |
| 122 | `pages/DeviceDetail.flows.test.tsx` | PASS |
| 123 | `FleetOverview.test`, `Security.test`, `Alerts.test`, `DeviceContainers.test` (both places), `DeviceProbes.test`, `ContainerDetailPage.test`, plus `PerDeviceCharts.test` | PASS |
| 124 | screenshots `after-fleet.png`, `after-device-detail-header.png` | MANUAL (Clod) |
| 125 | `pages/Security.test.tsx` | PASS |
| 126 | `contexts/DemoWebSocketProvider.test.tsx` | PASS |
| 127, 128, 129, 130, 131, 132 | `api/demo-parity.test.ts` plus `tsc --noEmit` | PASS |
| 133 | screenshot `after-dashboard-demo.png` | MANUAL (Clod) |
| 134 | demo console walkthrough | **NOT RUN** (needs a browser; Clod) |
| 135 | `pages/Security.test.tsx` (grid classes); screenshot `after-security.png` | PASS (classes) / MANUAL (visual) |
| 136, 137 | `pages/DeviceDetail.flows.test.tsx` | PASS |
| 138 | screenshot `after-device-detail-header.png` | MANUAL (Clod) |
| 139 | `git diff main -- web/package.json web/package-lock.json` is empty | PASS |
| 140 | `npx tsc --noEmit` exit 0 | PASS |
| 141 | 1,070 tests pass (baseline 405) | PASS |
| 142 | `Skeleton.test.tsx`, `Toast.test.tsx` | PASS |
| 143 | bundle size below | PASS |
| 144 | both builds succeed (below) | PASS |
| 145 | `git diff --name-only a500346 HEAD`: every path under `web/` or `docs/` | PASS |

Test counts: baseline 27 files / 405 tests; now **56 files / 1,070 tests**, all passing.

### Bundle size (AC-143 / NFR-007)

Main entry chunk (`dist/assets/index-*.js`), `gzip -6`, baseline built from the branch before any JS change (commit 1 only added CSS):

| Build | raw | gzip |
|---|---|---|
| main (baseline) | 1,445,161 B | 394,204 B |
| branch, production | 1,458,181 B | 400,765 B (**+6,561 B, +6.4 KB**, limit 20 KB) |
| branch, demo | 1,495,764 B | (about 412.5 kB by Vite) |

CSS: 62,746 B raw / 11,770 B gzip before, 68,579 B / 12,617 B after.

### Test run output

**New and changed tests only** (`npx vitest run` on the 43 test files changed since the docs commit):

```
 ✓ src/api/errors.test.ts (13 tests) 8ms
 ✓ src/test/static-guards.test.ts (52 tests) 26ms
 ✓ src/pages/DeviceDetail.flows.test.tsx (34 tests) 5454ms

 Test Files  43 passed (43)
      Tests  901 passed (901)
   Start at  23:10:13
   Duration  7.72s (transform 6.80s, setup 10.00s, import 14.06s, tests 29.77s, environment 24.78s)
```

**Full suite** (`npm run test:run`):

```
 ✓ src/test/static-guards.test.ts (52 tests) 28ms

 Test Files  56 passed (56)
      Tests  1070 passed (1070)
   Start at  23:10:26
   Duration  8.34s (transform 7.34s, setup 13.11s, import 15.31s, tests 34.49s, environment 32.87s)
```

**Linter** (no ESLint in the repo; `npx tsc --noEmit`):

```
$ npx tsc --noEmit
(no output)   exit code 0
```

**Production build** (`npm run build`, which is `tsc && vite build`):

```
vite v6.4.1 building for production...
✓ 826 modules transformed.
dist/index.html                     0.89 kB │ gzip:   0.45 kB
dist/assets/index-DzmoyDLa.css     68.58 kB │ gzip:  12.76 kB
dist/assets/index-Dv7c8NfW.js   1,458.18 kB │ gzip: 402.29 kB
(!) Some chunks are larger than 500 kB after minification.   [pre-existing warning]
✓ built in 4.35s
```

**Demo build** (`npx tsc --noEmit && VITE_DEMO=true npx vite build`, POSIX env syntax works in Git Bash; `npm run build:demo` itself was not changed):

```
✓ 827 modules transformed.
dist-demo/assets/index-DzmoyDLa.css     68.58 kB │ gzip:  12.76 kB
dist-demo/assets/index-CVvDiw-D.js   1,495.76 kB │ gzip: 412.52 kB
✓ built in 5.04s
```

Note: running the suite prints `ECONNREFUSED ::1:3000` noise from `SetupGuide`/`useFeatures` network calls in older test files. The same noise appears when running `FleetOverview.test.tsx` on the pre-change code (110 matching lines), so it is not new.

## Deviations from ADD

Items 1 to 3 are conflicts the task instructions resolved; the rest need architect awareness.

1. **SEC-001 supersedes DEV-11.** No ConfirmDialog autofocuses Confirm (primary included). `ConfirmModal`, `UpdateConfirmModal` and PatchReviewModal also focus Cancel. Modal's default initial focus is the first control in the body, then the footer, then the close X, then the panel (ADD said "first focusable", which would have been the close X).
2. **SEC-002** resolved in the page (`deleteScope` state) instead of ADD Flow 7.4's `mutateAsync()` with live status.
3. **SEC-003:** besides the error toast, a *success* that lands after the dialog unmounted still raises the success toast but skips `onSuccess`/`onClose` (so a delete confirmed and then abandoned does not navigate the user). The security review only specified the rejection case.
4. **`tokens.test.ts` reads `index.css` through `node:fs`**, with `/// <reference types="node" />` and the vitest working directory (`src/index.css`), not through `index.css?raw`. Under this vitest config a CSS `?raw` import resolves to an empty string (verified with a probe: `?raw`, `?inline` and `import.meta.glob` all return length 0). `@types/node` is already installed (transitive); `vitest.config.ts` and `tsconfig.json` are untouched.
5. **Two extra test files not in ADD Section 4:** `api/errors.test.ts` and `hooks/useMinuteTick.test.tsx`.
6. **`DeviceDetail` "Update Sent" label.** ADD Flow 7.4 says the command mutation has no timers. The old `isSuccess`-gated label would have stayed "Update Sent" and the button disabled until reload after a failed agent update, so it now uses a small `agentUpdateSent` state cleared by an effect-managed 5 s timer (with cleanup). No banner or mutation timer remains.
7. **Existing test assertions changed beyond ADD 12.5:** `ContainerDetailPage.test.tsx` used `'Container not found.'` (trailing period); the EmptyState title is "Container not found" per the FRD, so the five assertions now use the new text. `Probes.test.tsx` stub removal and `ActivityLog.test.tsx`/`ConfirmModal.test.tsx` migrations are as listed in ADD 12.5. No test was deleted.
8. **Visible palette/format changes** worth a look: `UpdateConfirmModal` confirm and the PatchReviewModal confirm are now the primary (blue) Button instead of amber and cyan; the neutral Badge uses `text-muted` at 20 % instead of `gray-500`; header clock is 24-hour (A-005); ActivityRiver shows `HH:mm:ss UTC` instead of `HH:mm`; AgentManagement and AlertRuleSettings durations read `10m` / `1h 0m` instead of `10 min` / `1 hr`; DeviceDetail uptime under a day drops the leading `0h` (A-010).
9. **Demo data tightened to satisfy the typed clients:** `getServerUpdate` adds the merged-interface fields (`types/models.ts` declares two `UpdateInfo` interfaces that merge), `getSecurityOverview`/`getSecurityDevices` gain `certs_expiring_soon`, `total_certs`, `pending_security_count`, `unattended_upgrades` (so the demo Security page now shows its fifth "Certs Expiring" card), and `getHeartbeatHistory` returns oldest-first with deterministic variation instead of a flat newest-first line.
10. **`DeviceQueryFallback`** also shows "Device not found" when the query settled with neither data nor an error (for example a disabled query with no route id). ADD 6.5 only described the 404 case.
11. **`renderWithProviders`** sets `retryDelay: 0` so device pages (which set their own `retry` policy, `retryUnlessNotFound`) settle at once in tests. `ContainerDetailPage.test.tsx` keeps its own client, so its non-404 test waits for the 1 s default backoff (`timeout: 3000`).
12. Migrated JSX bodies (FleetOverview table card, Alert/Notification edit modals, PatchReviewModal list) were **not re-indented** to avoid a diff that hides the real change.
13. Working-tree line endings: this Windows checkout has `core.autocrlf=true`, so some edited files are LF and some CRLF on disk. Git normalizes on commit; no commit contains a line-ending-only change.

## Known gaps / not done

- **Visual verification:** done by Clod. See "Visual verification (Clod)" below. The only screenshot not captured is the AC-119 chart-tooltip hover, which unit tests cover.
- **AC-134 demo console walkthrough:** Clod covered Fleet, Dashboard, Device detail and Security, with no console warnings or errors (see below). The Settings tabs were not visited; QA's `demo-parity.test.ts` checks that all 46 demo readers resolve. The manual keyboard pass from the ADD DoD was not run.
- Anything in ADD Section 12.7 stays deferred: un-migrated modals (SecurityScoreModal, CreateAlertDialog, DeviceProbeModal, ProbeModal, SetupGuide), `Section.tsx` and the wider gray-class sweep, the ContainerDetail container-command banner (DEV-15), OQ-001 silent failures, probe-type/container-state/firewall labels, `text-[9px|10px|11px]`, orphaned `groupContainers`, body scroll lock, cross-tab preference sync, SEC-004's migration of the two `z-[60]` overlays onto the token scale.
- `utils/docker.ts` `groupContainers` and its `ContainerGroup` type are now unused.

## Notes for QA

- **Time zone tests are deterministic:** they inject `localTimeZone: 'America/Los_Angeles'` through `TimePreferenceProvider` (DEV-05). Do not rely on `process.env.TZ` on Windows.
- **Harness:** `test/renderWithProviders.tsx` wraps QueryClient (`retry:false`, `retryDelay:0`), MemoryRouter, TimePreferenceProvider (no storage) and ToastProvider; options `route`, `path`, `routes`, `timeZone`, `toast`, `showLocation`.
- **Native `window.alert` in happy-dom:** it does not exist. `DeviceDetail.flows.test` defines a no-op with `Object.defineProperty` so a `vi.spyOn(window,'alert')` can assert `not.toHaveBeenCalled()` (allowed by DEV-03); the static guard does not match `defineProperty`.
- **Things worth probing by hand:** hold Enter on "Update Agents (n)", "Review Patches" and a device "Patch" button; open Delete on an online device, take it offline, confirm; navigate Back while a Reboot dialog is pending; a 5 KB unbroken hostname through Reboot, delete and a toast; the header clock toggle across a page with charts (tooltip label shows the zone); `localStorage['riot-time-zone']` set to garbage; reduced-motion on (Skeleton does not pulse, toast does not slide).
- **Mutation checks already done:** the held-Enter test fails if Confirm is given initial focus; the static guards fail when a hex literal, `confirm(`, `innerHTML =` or a `demo-data` import is planted.

## Visual verification (Clod)

Clod checked the demo build (`web-demo`, port 5179) at 1440×900 with Playwright on 2026-10-18. All screenshots are in `docs/implementation/UI-FOUNDATION-001-screenshots/`.

| Screenshot | Before | After | Result |
|---|---|---|---|
| Fleet (AC-006, AC-124) | `before-fleet.png` | `after-fleet.png` | Hostnames render as reported (`backup-server`, not `BACKUP-SERVER`). "Last seen" uses `<RelativeTime>`. |
| Toast stack (AC-052) | none (8 s inline banner below the table) | `after-toast-stack.png` | Fleet → Update Agents → Update all: a success toast at bottom-right ("Agent update dispatched", sent and skipped counts), with a Dismiss button. |
| Confirm dialog (SEC-001) | none (native `confirm()`) | `after-confirm-dialog.png` | Device → Delete opens `role="dialog"` "Delete Device", focus starts on **Cancel**, and Escape closes it. |
| Device detail header (AC-124, AC-138) | `before-device-top.png` | `after-device-detail-header.png` | The hostname keeps its case. The gauge has a "SECURITY SCORE" caption. Delete is a danger button in the right-hand cluster, behind a divider, no longer next to Docker · Probes · Terminal. |
| Dashboard, demo (AC-133) | `before-dashboard.png` | `after-dashboard-demo.png` | Per-device charts, leaderboard and activity are populated. The "WebSocket disconnected" banner is gone. |
| Security (AC-135) | `before-security.png` | `after-security.png` | All six stat cards fit one row. See fix 1 below. |
| Device detail chart tooltip (AC-119) | n/a | not captured | Hover screenshots of recharts tooltips are unreliable in headless Playwright. The zone-labelled tooltip formatter is covered by unit tests (Section 8 mapping). |

**AC-134 console walkthrough:** Clod visited Fleet, Dashboard, Device detail and Security in demo mode. `browser_console_messages` at warning level returned nothing: no "No queryFn" errors and no React warnings. Before this story, every page logged `[["feature-toggles"]]: No queryFn` and the Dashboard also logged `[["fleet-containers"]]` errors.

**Fixes Clod made during visual verification** (commit `[UI-FOUNDATION-001] fix: visual-verification follow-ups`):
1. **Security stat cards still orphaned.** The tightened demo data adds a "Certs Expiring" card, and SELinux/AppArmor sat in a separate 2-column grid, so "AppArmor Enabled" still wrapped onto a half-width row of its own. The MAC-module cards now render as `StatCard`s inside the single stat grid (`xl:grid-flow-col auto-cols-fr`), so every card shares one row.
2. **Activity feed times had no date.** `ActivityRiver` rendered `Timestamp precision="time"`, so a 3-day-old "Device went offline" read `03:14:02 UTC` like a current event. It now uses `<RelativeTime>` ("3d ago"), with the absolute, zone-labelled time in the hover title.
3. **Not a defect:** the text the accessibility tree showed at the end of `<body>` ("249K", then an ISO timestamp) is recharts' own `#recharts_measurement_span`. That span is `aria-hidden` and sits at `top:-20000px`, so nothing is visible.

After the fixes: `npx tsc --noEmit` exits 0, and `npm run test:run` reports 56 files / 1070 tests passed.

## Release notes (for the technical writer, DEV-18)

- New in-house UI primitives: Button, Card, Badge, Modal (focus trap, Escape, focus restore), ConfirmDialog (pending state, inline errors), EmptyState, Skeleton, QueryState, toasts, RelativeTime and Timestamp. Design tokens and a single z-index scale were added to `index.css`.
- Native `confirm()`/`alert()` dialogs are gone. Destructive confirmations are danger dialogs that stay open with a spinner while they run, show failures inline, and never put initial focus on the confirm button.
- Fleet bulk agent update and patch-all, and device commands, report through toasts; failures are no longer silent. Failed fetches show an error panel with Retry instead of "Add your first device" or "Device not found".
- All timestamps carry a zone label. Click the header clock to switch between UTC and local time (stored in `localStorage` as `riot-time-zone`; default UTC). The clock is now 24-hour.
- Hostnames render as reported. Enum values (severity, command status, command action, event type) render through one label map.
- Demo build: the dashboard renders charts, leaderboard and activity; the demo clients are type-checked against the real ones.
- Removed dead components (Sparkline, MiniScore, ContainerGroup, ContainerCard) and the unused container slide-out drawer.
- Device Delete is a danger button, separated from the Docker/Probes/Terminal navigation; the security score gauge has a visible caption and an accessible name; the Security page stat cards fill one row from 1280 px.
