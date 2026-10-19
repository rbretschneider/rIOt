# Architecture Decision Document

**Story ID:** UI-FOUNDATION-001
**FRD Reference:** docs/requirements/UI-FOUNDATION-001-frd.md
**Author:** Architect Agent
**Date:** 2026-10-17
**Status:** FINAL (pending security review and Ryan's review)

---

## 0. Stage 1 outcome (FRD review)

No blocking business questions. The orchestrator accepted assumptions A-001..A-015
as written. Every technical ambiguity, conflict and infeasibility found in the FRD
is resolved by the architect in **Section 3a (FRD deviations and conflict
resolutions)**. Engineers must follow Section 3a where it differs from the FRD
wording; QA must test against the resolved wording.

## 1. Summary

The story adds a token layer to `web/src/index.css` (`@theme inline` aliases onto the
existing Tailwind palette, plus a CSS-variable z-index scale), a TypeScript
`chartColors` constant for recharts, a set of in-house primitives under
`web/src/components/ui/` (Button, Card, Badge with a shared enum label map, Modal
with focus trap and stack-aware Escape, ConfirmDialog with an async pending state,
EmptyState, Skeleton, QueryState, a Toast provider, RelativeTime and Timestamp). It
also adds a UTC/Local time-preference context with an injected clock and a typed
`ApiError`. The flows named in the FRD then move onto these primitives (native
dialogs, timed banners, misleading error states, timestamps, hostnames). The demo
clients are typed against the real clients, so a missing method fails `tsc`. No
dependency, backend or build-config file changes.

## 2. Technical Context

**Stack (verified):** React 19.2, react-router-dom 7 (HashRouter in demo, BrowserRouter
otherwise; `main.tsx`), TanStack Query 5 (default `retry: 1`, `staleTime: 10s`),
Tailwind v4.2 via `@tailwindcss/vite`. There is no `tailwind.config`. `index.css`
holds only `@import "tailwindcss"` and scrollbar utilities. The stack also includes
recharts 3, `@floating-ui/react` (only in `components/Tooltip.tsx`), vitest 4,
happy-dom, Testing Library and user-event. `tsconfig.json` is strict, has `target`
ES2020 and `types: ["vitest/globals"]`, and `src/vite-env.d.ts` references
`vite/client` (so `?raw` imports and `import.meta.glob` are typed). There is no
ESLint, so "lint" means `npx tsc --noEmit`. `vitest.config.ts` does not load the
Tailwind plugin, so tests never process CSS. Assertions must target class names or
`data-*` attributes, never computed styles.

**Conventions found:**
- Repository-style API objects: `api` (`api/client.ts`), `settingsApi`
  (`api/settings.ts`), `authApi`. Each is a plain object literal of arrow functions.
  Errors are `throw new Error(...)` with message `HTTP <status>: <statusText>`
  (fetchJSON) or `HTTP <status>` (mutations). `sendCommand` and `getDeviceSummary`
  use the server `error` body. A 401 calls `window.location.reload()` and then
  throws `Error('Unauthorized')`.
- Demo build: `vite.config.ts` aliases, by regex, `*/api/client` → `api/demo-client`,
  `*/api/settings` → `api/demo-settings` and `*/contexts/WebSocketProvider` →
  `contexts/DemoWebSocketProvider`. Any module whose specifier ends in those paths
  is swapped. **Consequence:** anything new that pages import from `api/client` must
  also exist in `demo-client`. This is why `ApiError` lives in a new, never-aliased
  module `api/errors.ts` (AD-005).
- Tests are co-located `*.test.ts(x)`. Each page test builds its own
  `renderWithProviders` (QueryClient with `retry:false` plus MemoryRouter) and mocks
  `../api/client` with `vi.mock` factories that list only the methods the page uses.
  Describe titles carry `[AC-0xx]` tags from earlier stories, so this story uses
  `[UIF AC-0xx]` to avoid collisions.
- `hooks/useDashboardTick.ts` keeps one `setInterval` per consumer, which makes it
  unsuitable as a shared ticker (AD-007).
- `utils/rebootEta.ts` `formatUtc(iso): string | null` (UTC getters, minute precision)
  is tested by `rebootEta.test.ts` and `FleetOverview.test.tsx`.

**Verified current state relevant to the story** (line numbers are from `main`):
- Native dialogs: `DeviceDetail.tsx:154` alert; confirm at `DeviceProbes.tsx:168`,
  `Probes.tsx:217`, `Probes.tsx:378`, `AgentManagement.tsx:74`,
  `AlertRuleSettings.tsx:202` and `:223`, `LogSettings.tsx:78`, and
  `NotificationSettings.tsx:173`. One test stubs it: `Probes.test.tsx:287`
  (`vi.stubGlobal('confirm', …)`).
- Timed banners: `FleetOverview.tsx:56-91` (8 s, bulk update has no onError, patch-all
  swallows errors), `FleetOverview.tsx:353-365` (render), `DeviceDetail.tsx:123-138`
  (5 s resets), `:503-513` and `:1639-1644` (render). `AgentManagement.tsx:22-31,101-110`
  has a third 8 s result block.
- Misleading states: `FleetOverview.tsx:347`, `DeviceDetail.tsx:165-166`,
  `DeviceContainers.tsx:29-30` and `ContainerDetailPage.tsx:44-45`.
- Hand-rolled overlays: as listed in the grounding brief. In addition,
  `ActivityLog.tsx:186` already sets `role="dialog"` on the **backdrop** and has its
  own Escape listener. Its test clicks the dialog element to close it
  (`ActivityLog.test.tsx:231`).
- Raw enum displays: `ActivityLog.tsx:130,198` (`cmd.status`),
  `ActivityLog.tsx:15-20` (local `formatAction`, title case), `Alerts.tsx:126`
  (`SeverityBadge` prints the raw value), `Alerts.tsx:151` (`type.replace(/_/g,' ')`),
  `DeviceDetail.tsx:506` (`os_update (pending)`), `DeviceDetail.tsx:958` and
  `ContainerDetail.tsx:344` (`rule.severity`), `ContainerDetail.tsx:92`
  (`Command sent (pending)`), `AlertRuleSettings.tsx:555` and its severity
  `<option>`s, the `CreateAlertDialog.tsx:206` severity `<option>`s,
  `SecurityScoreModal.tsx:42` and `LearnMore.tsx:1197` (finding severity).
- Hex colors: `MetricChart.tsx` (axis and tooltip), `ContainerMetricChart.tsx`,
  `PerDeviceCharts.tsx` (series, grid, legend, tooltip),
  `ContainerDetail.tsx:302,304` (series colors passed as props),
  `DeviceDetail.tsx:701-707` (series colors passed as props) and `Alerts.tsx:120`
  (inline left border). `Terminal.tsx` is exempt.
- Hostname `uppercase`: `DeviceDetail.tsx:222`, `Alerts.tsx:129`,
  `FleetOverview.tsx:241`, `Security.tsx:248`, `DeviceContainers.tsx:40,59`,
  `DeviceProbes.tsx:82`, `ContainerDetailPage.tsx:67`, and also
  `dashboard/PerDeviceCharts.tsx:116`, which the FRD did not list. It is fixed too.
- Demo gaps (verified by diffing object keys): `demo-client` lacks 14 of the 53 `api`
  methods (`getContainerLogs`, `updateDeviceLocation`, `setAutoPatch`,
  `bulkDockerUpdate`, `getAllDeviceProbes`, `getDeviceProbes`, `createDeviceProbe`,
  `updateDeviceProbe`, `deleteDeviceProbe`, `runDeviceProbe`,
  `getDeviceProbeResults`, `getDeviceSummary`, `getFleetHeartbeats`,
  `getFleetContainers`). `demo-settings` lacks 7 of 32 (`getTLSSANs`, `saveTLSSANs`,
  `regenerateTLS`, `getFeatureToggles`, `saveFeatureToggles`, `getAutomationConfig`,
  `saveAutomationConfig`). The demo `updateDeviceTags` returns `void` but the real
  method returns `{tags}`, which is a latent type mismatch that the new typing will
  surface. `demo.getDeviceDetail(id)!` returns `null` for unknown IDs.

## 3. Architecture Decisions

### AD-001: Token layer as `@theme inline` aliases onto the existing palette

**Decision:** `index.css` gains an `@theme inline { … }` block in which every color
token is a `var()` alias of an existing Tailwind palette variable. Tailwind generates
utilities named after the token (`bg-surface`, `text-text-muted`, `border-border` and
so on). The exact names and values are:

| Token (`--color-*`) | Value | Purpose / typical utility |
|---|---|---|
| `surface` | `var(--color-gray-950)` | app shell, `bg-surface` |
| `raised` | `var(--color-gray-900)` | cards, panels, dialogs, toasts, `bg-raised` |
| `raised-2` | `var(--color-gray-800)` | secondary-button fill, hover rows, skeleton bars |
| `border` | `var(--color-gray-800)` | default card border, `border-border` |
| `border-strong` | `var(--color-gray-700)` | dialog, toast and input outline |
| `text` | `var(--color-gray-100)` | primary text, `text-text` |
| `text-muted` | `var(--color-gray-400)` | secondary text, `text-text-muted` (A-013) |
| `accent` | `var(--color-blue-600)` | primary button fill, focus ring |
| `accent-hover` | `var(--color-blue-500)` | primary hover |
| `link` | `var(--color-blue-400)` | link / accent text on dark |
| `success` | `var(--color-emerald-500)` | success fills, tinted backgrounds |
| `success-fg` | `var(--color-emerald-400)` | success text and icons |
| `warn` | `var(--color-amber-500)` | warn fills |
| `warn-fg` | `var(--color-amber-400)` | warn text |
| `danger` | `var(--color-red-600)` | danger button fill |
| `danger-hover` | `var(--color-red-500)` | danger hover |
| `danger-fg` | `var(--color-red-400)` | danger text, inline errors |
| `info` | `var(--color-blue-500)` | info fills |
| `info-fg` | `var(--color-blue-400)` | info text |

The z-index scale is declared as **plain custom properties on `:root`**, not inside
`@theme`, and primitives consume it through the Tailwind v4 variable shorthand
`z-(--z-modal)`:
`--z-base: 0; --z-dropdown: 40; --z-modal: 50; --z-tooltip: 70; --z-toast: 80;`

Motion: `@theme` defines `--animate-toast-in` with a matching `@keyframes toast-in`
(150 ms, translate-y plus fade). It is used only behind `motion-safe:`.

A TS mirror `web/src/theme/tokens.ts` exports `TOKEN_PALETTE` (token → palette name,
for example `surface: 'gray-950'`), `TOKEN_HEX` (token → sRGB hex of that palette
step, used only by the contrast test) and `Z_INDEX` (the five numbers). The test
asserts that `index.css` and `tokens.ts` agree.

**Rationale:** Aliasing with `var()` reproduces the current palette exactly. That
satisfies FR-002 by construction, because the OKLCH values are Tailwind's own and
nothing is copied or approximated. `@theme inline` is Tailwind's documented pattern
for referencing other theme variables, and it makes the utility emit the referenced
variable directly, so unused-variable pruning cannot drop the source. The z-scale
sits outside `@theme` because Tailwind v4 has no z-index theme namespace and the
`(--var)` shorthand is supported from 4.0. The base step is 50 so it equals the
legacy `z-50` overlays. Tooltip (70) and toast (80) sit above every legacy value
(40/50/60), so un-migrated overlays such as `SecurityScoreModal` at `z-[60]` can
never cover a toast or tooltip.

**Alternatives considered:** (1) Hard-coding hex values from the Tailwind v3 palette
was rejected because v4 colors are OKLCH and copying v3 hex would shift hues
slightly, violating FR-002. (2) Using the FRD defaults `green-500` / `sky-500` for
success/info was rejected because the codebase uses `emerald` and `blue` for those
roles (more than 130 `emerald` occurrences against 6 `green`). Switching would
visibly change hue (see Section 3a, DEV-01). (3) Single-shade status tokens were
rejected because text on dark surfaces uses the -400 shades while fills use -500/-600,
so a single token cannot serve both without a visual change. Hence the `*-fg`
companions.

**Consequences:** Utility names such as `text-text` and `border-border` are mandated by
FR-001 and are slightly redundant. A z-value used anywhere must be one of the five
variables. Chart colors cannot read these tokens (recharts takes literal strings), so
AD-002 adds a parallel constant.

### AD-002: `chartColors` TypeScript constant for recharts

**Decision:** Create `web/src/theme/chartColors.ts` exporting one frozen object:
- `series`: `cpu` `#3b82f6` (blue-500), `mem` `#8b5cf6` (violet-500, DeviceDetail),
  `memAlt` `#10b981` (emerald-500, PerDeviceCharts RAM), `diskIo` `#f59e0b` (amber-500),
  `diskRead` `#34d399` (emerald-400), `diskWrite` `#fb923c` (orange-400), `load`
  `#a855f7` (purple-500), `netRx` `#06b6d4` (cyan-500), `netTx` `#f97316` (orange-500)
- `axisText` `#6b7280`, `axisLine` `#374151`, `grid` `#1f2937`
- `tooltipBg` `#1f2937` (MetricChart / ContainerMetricChart), `tooltipBgDense`
  `#111827` (PerDeviceCharts), `tooltipBorder` `#374151`
- `legendText` `#9ca3af`, `legendTextHidden` `#4b5563`

Every hex literal in `MetricChart.tsx`, `ContainerMetricChart.tsx`,
`dashboard/PerDeviceCharts.tsx`, `ContainerDetail.tsx` and `DeviceDetail.tsx` is
replaced by a reference to this object. Each value is the exact literal currently in
use, so nothing changes visually. `Alerts.tsx`'s inline left border becomes token
classes (`border-l-[3px] border-l-danger-fg` / `border-l-warn-fg`), because #f87171 is
red-400 and #fbbf24 is amber-400. `Terminal.tsx` is untouched.

**Rationale:** FR-004 requires one shared chart-color source. recharts SVG attributes
and `contentStyle` objects need literal colors, and `var()` support in SVG
presentation attributes is inconsistent across engines. The file lives under
`theme/`, not `components/ui/`, so it does not trip the AC-005 hex scan.

**Alternatives considered:** CSS `var(--color-…)` strings passed to recharts were
rejected (unreliable in `stop-color`/`stroke` attributes, and `getComputedStyle` at
render time is impure and untestable in happy-dom).

**Consequences:** The palette is defined twice, once in CSS aliases and once in chart
hex. Comments in `chartColors.ts` name the palette step for each value. A theme change
must touch both files. The light theme is out of scope (D12).

### AD-003: Primitive library under `web/src/components/ui/`

**Decision:** Each primitive is one file plus a co-located test, re-exported from
`components/ui/index.ts`. No primitive file contains a `gray-<N>` class or a hex
literal (AC-005). The public contracts are in Section 6.2. Key decisions:

- **Button**: `variant` primary|secondary|ghost|danger (default `secondary`), `size`
  sm|md (default `md`). It renders `data-variant` and `data-size` attributes, which is
  how AC-008 is asserted. `type` defaults to `"button"`. `pending` implies `disabled`
  plus `aria-busy="true"` and shows an inline `Spinner` (exported from `Button.tsx`)
  with `pendingLabel` text. React 19 `ref` is accepted as a normal prop.
- **Card**: `<section>` with `bg-raised border border-border rounded-lg`. An optional
  `title` renders a heading (`titleAs` h2|h3, default h2). An optional `actions` slot
  sits in the header.
- **Badge**: tones neutral|accent|success|warn|danger|info, rendered as `data-tone`.
  Two call forms: children with an explicit `tone`, or `category` + `value`. The
  second form takes its text from `enumLabel()` and, unless overridden, its tone from
  `toneFor()`.
- **labels.ts**: the single enum label map. Its contents are in AD-008.
- **Modal**, **ConfirmDialog**: see AD-004.
- **EmptyState**: title, optional description, optional `action {label,onClick}`
  (rendered as a secondary Button), and optional `children` for custom content such as
  a back link.
- **Skeleton**: `aria-hidden="true"`, `animate-pulse motion-reduce:animate-none`,
  `bg-raised-2`. An optional `lines` prop renders stacked bars.
- **QueryState**: render-prop wrapper. It exports `ErrorPanel` (title, message,
  Retry). Precedence: data present → children (or `empty` when `isEmpty(data)`),
  which keeps stale data visible on a background error (FR-042). Error without data →
  error panel, or `renderError`. Otherwise → loading container `role="status"`
  `aria-busy="true"` `aria-label="Loading"`, holding the placeholder (default:
  three-line Skeleton). Because emptiness is evaluated only when data exists, error
  always beats empty (FR-043).
- **Toast**: `ToastProvider` plus `useToast()`, see AD-006.
- **RelativeTime** and **Timestamp**: see AD-007.
- **focusRing.ts**: exports one class string,
  `focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-surface`.
  It is used by Button, the Modal close button, the toast close button, the
  ErrorPanel Retry (a Button) and HeaderClock. AC-012 asserts the substring
  `focus-visible:ring-2`.

**Rationale:** Small, explicit APIs that cover today's call sites and nothing
speculative. `data-*` attributes give stable test hooks that do not depend on class
strings, which also removes the `className.toContain('red')` fragility that existing
tests have.

**Alternatives considered:** Headless libraries (Radix, react-aria) were rejected
because NFR-001 allows no new dependencies. A single mega-component with variants for
modal and drawer was rejected because the drawer is dead code.

**Consequences:** `components/Section.tsx` (gray Section card) is **not** rebuilt on
Card in this story, to avoid a visual sweep of DeviceDetail (deferred, Section 12.7).

### AD-004: In-house Modal: portal, stack registry, focus trap, focus restore

**Decision:**
- Modal renders through `createPortal(…, document.body)` when `open` is true. Its
  structure is a backdrop `div[data-testid="modal-backdrop"]`
  (`fixed inset-0 z-(--z-modal) bg-black/60 flex items-center justify-center p-4`)
  containing the panel `div[role="dialog"][aria-modal="true"][aria-labelledby=<titleId>][tabIndex=-1]`.
  `titleId` comes from `useId()`. The title renders as an `h2`.
- **Stack registry**: a module-level array in `Modal.tsx`. Each mounted, open Modal
  pushes a unique token in a mount effect and removes it in cleanup, which is
  symmetric under StrictMode double effects. Each Modal adds one `keydown` listener on
  `document` and acts only if it is the top of the stack. On Escape it calls
  `onClose()` unless `pending`. On Tab it applies the trap. A non-top modal ignores
  all keys, so Escape closes only the topmost modal (FR-024 / AC-029). Because the
  portal appends later modals later in `body` with an equal z-index, a ConfirmDialog
  opened over another Modal paints on top with no extra z math.
- **Focus trap**: on Tab or Shift+Tab keydown in the top modal, it collects focusables
  in the panel with the selector
  `a[href],button:not([disabled]),input:not([disabled]):not([type=hidden]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])`.
  It excludes elements inside `[hidden]` or `[aria-hidden="true"]` and does no layout
  checks, because happy-dom has no layout. If focus is on the last element (or outside
  the panel) and Tab is pressed, it calls `preventDefault` and focuses the first.
  Shift+Tab on the first element focuses the last. With zero focusables, it focuses the
  panel.
- **Initial focus**: `initialFocusRef` if provided, else the first focusable, else the
  panel. This runs in a layout effect after mount.
- **Focus restore**: it captures `document.activeElement` when the modal opens. On
  unmount, it refocuses that element if `isConnected` (FR-022). After a device-delete
  navigation the opener is gone and nothing is focused, which is expected.
- **Backdrop**: the modal closes only when both `mousedown` and `click` targets are
  the backdrop itself. This prevents a text-selection drag that ends outside the panel
  from closing it. It is ignored when `pending` or `dismissOnBackdrop={false}`.
- The close "X" button (`aria-label` = `closeLabel`, default `"Close"`) is hidden when
  `showCloseButton={false}` and disabled while `pending`.
- **ConfirmDialog** is built on Modal (`size="sm"`, `showCloseButton={false}`). Its
  internal body component mounts only while `open`, so every opening starts in the
  Idle state. States follow FRD Section 7.3: Idle → Pending → (Closed with optional
  toast | Error). A `useRef` in-flight guard blocks re-entry before React re-renders
  (FR-029). Initial focus goes to **Cancel for `danger`** (FR-033 / A-002) and to
  **Confirm for `primary`** (DEV-11). The inline error is a
  `div[role="alert"]` with `text-danger-fg` and the text `errorMessage(e)` (AD-005).
  After resolve, ConfirmDialog raises the success toast (if configured), calls
  `onSuccess(result)` and then `onClose()`. State setters are guarded by a mounted ref
  because the parent may unmount it (navigate on delete).

**Rationale:** The requirements (stacking, pending lock, focus restore) are small
enough to own. A shared registry is the simplest correct answer to "only the topmost
closes". Portals avoid `overflow`/`transform` ancestors clipping fixed overlays inside
cards.

**Alternatives considered:** Native `<dialog>` with `showModal()` was rejected because
happy-dom support is incomplete, its Escape/cancel semantics are hard to pin while
`pending`, and it has inconsistent `::backdrop` click handling. `inert` on siblings was
rejected because the key-handler trap is enough and testable in happy-dom.
Per-instance `stopPropagation` for Escape was rejected because listener order is not
guaranteed across portals.

**Consequences:** Every test that clicks the backdrop must target
`getByTestId('modal-backdrop')`, since `role="dialog"` is now the panel. Body scroll
lock is not implemented and nothing requires it. While a modal is open, the toast
close button cannot be reached with Tab because of the trap. It stays clickable. This
is acceptable because error toasts persist.

### AD-005: Additive typed `ApiError` in a non-aliased module

**Decision:** Create `web/src/api/errors.ts` exporting `class ApiError extends Error`
with `readonly status: number` and `name = 'ApiError'`. It also exports
`isApiError(e)`, `isNotFound(e)` (`ApiError` with status 404),
`errorMessage(e, fallback = 'Something went wrong. Please try again.')` (returns
`e.message` for any non-empty `Error`, a non-empty string as-is, otherwise
`fallback`), and `retryUnlessNotFound(failureCount, error)` (true when
`failureCount < 1 && !isNotFound(error)`, matching the global `retry: 1` but never
retrying a 404).
- `client.ts` and `settings.ts` throw `ApiError` for every non-2xx, non-401 response
  and keep each existing message byte for byte: `HTTP <status>: <statusText>` in
  fetchJSON and settings `mutate`, `HTTP <status>` in the client mutations, and the
  server `error` text for `sendCommand`/`getDeviceSummary`. The **401 branches stay
  exactly as they are**, including the plain `Error('Unauthorized')` (FR-080).
- `client.ts` re-exports `ApiError` and adds `export type ApiClient = typeof api` and
  `export type AuthClient = typeof authApi`. `settings.ts` adds
  `export type SettingsClient = typeof settingsApi`.
- Pages import `ApiError`, `isNotFound` and `errorMessage` from `../api/errors`,
  **never from `../api/client`**.
- Device queries on DeviceDetail, DeviceContainers and ContainerDetailPage set
  `retry: retryUnlessNotFound`.

**Rationale:** A subclass keeps `instanceof Error`, `.message` and every existing
`toThrow('HTTP 500')` assertion valid (AC-088). A separate module is required because
(a) the demo alias would otherwise swap the class out, and (b) ten existing test files
`vi.mock('../api/client', () => ({ api: … }))` with factories that would not contain
`ApiError`, which would make the import `undefined` at runtime.

**Alternatives considered:** Adding a `status` property to a plain `Error` was
rejected because it is untyped and not narrowable. Putting `ApiError` in `client.ts`
only was rejected for the alias and mock reasons above. Mapping 404 to `null` data was
rejected because it is lossy and conflicts with `useQuery` error semantics.

**Consequences:** A `TypeError('Failed to fetch')` network error has no status and is
treated as non-404 (FR-085 / AC-098). `demo-client` re-exports `ApiError` so module
export parity holds (AD-010).

### AD-006: In-house toast system

**Decision:** `components/ui/Toast.tsx` exports `ToastProvider` and `useToast()`.
- State is an array of toasts, newest first, each `{ id, tone, title, message?,
  createdAt }`. IDs come from a monotonically increasing counter (no randomness, no
  `Date.now()`).
- `show()` prepends the new toast. If the array already holds `max` (4) toasts, the
  oldest (last) is dropped (FR-047 / A-003). There is no hidden queue.
- Each `ToastItem` owns its own timer. Success and info toasts run
  `setTimeout(remaining)` where `remaining` starts at `duration` (5000). Error toasts
  never schedule a timer (FR-049).
- Pause and resume: `onMouseEnter`/`onMouseLeave` and React `onFocus`/`onBlur` (which
  bubble) set `hovered` and `focusWithin` flags. On the transition to paused, the item
  clears the timer and subtracts the elapsed time `now() - startedAt` from
  `remaining`. On the transition to running (both flags false) it restarts the timer
  with `remaining` (FR-050 / A-004). `now` comes from the provider prop, defaulting to
  `() => Date.now()` at the composition root.
- Viewport: the region is always mounted, including when empty:
  `div[role="region"][aria-label="Notifications"]`
  `fixed bottom-4 right-4 z-(--z-toast) w-80 max-w-[calc(100vw-2rem)] pointer-events-none`
  wraps an `ol[aria-live="polite"]` laid out as `flex-col gap-2`. Items are
  `pointer-events-auto` `li` elements. Success and info items carry no extra role,
  so they are announced through the polite ancestor. Error items carry `role="alert"`
  (assertive) (FR-052). DOM order is newest first, which is visual top-to-bottom
  (FR-046 / AC-059).
- Item visuals: `bg-raised border border-border-strong border-l-4` plus a tone border
  (`border-l-success` / `border-l-info` / `border-l-danger`), an icon, the title
  (`font-medium text-text`), the message (`text-text-muted`) and a close button with
  `aria-label="Dismiss notification"` and `focusRing`. The enter animation is
  `motion-safe:animate-toast-in`. There is no exit animation; dismissed items unmount
  immediately, so NFR-006 holds trivially.
- `useToast()` **throws** when no provider is mounted (DEV-16). It returns a stable,
  memoized API object.
- Mounting: `main.tsx` wraps `<App/>` once, inside the Router and QueryClientProvider:
  `<TimePreferenceProvider><ToastProvider><App/></ToastProvider></TimePreferenceProvider>`.
  `main.tsx` is shared by both builds (FR-044). The provider sits above `<Routes>`, so
  toasts survive navigation (FR-053).

**Rationale:** FR-044..FR-053 are fully specified and small. Per-item timers isolate
pause logic. A pre-existing polite list is the reliable live-region pattern, because
regions inserted together with their content are often not announced.

**Alternatives considered:** A single global timer loop was rejected because pause per
item becomes awkward. Separate polite and assertive containers were rejected because
they would break the single newest-on-top order. Silent no-op without a provider was
rejected because it would hide wiring mistakes.

**Consequences:** Test harnesses that render a component calling `useToast` (directly
or through ConfirmDialog) must include `ToastProvider` (Section 12.4 lists them).

### AD-007: Time preference context, injected clock, one shared relative ticker

**Decision:**
- `contexts/TimePreferenceProvider.tsx` exports `TimePreferenceProvider`,
  `useTimePreference()` and `useTimeFormatters()`.
  - Provider props: `storage` (default `safeLocalStorage()`, which returns
    `window.localStorage` or `null` inside try/catch), `localTimeZone` (IANA override
    used by tests; `undefined` means the runtime zone) and `now` (default
    `() => Date.now()`).
  - Storage key `riot-time-zone`, values `'UTC' | 'Local'`. The initial read is
    wrapped in try/catch, and anything other than exactly `'Local'` resolves to
    `'UTC'` (FR-103). Writes are try/catch and ignored on failure; the in-memory
    state still changes.
  - The context value
    `{ zone, setZone, toggleZone, zoneOpts: { zone, localTimeZone }, now }` is
    memoized on `[zone, localTimeZone, now]` and **changes only when the preference
    changes** (NFR-008).
  - The default context value (no provider) is UTC with `now = () => Date.now()`, so
    the many untouched test files that render timestamp components need no new
    wrapper.
  - `useTimeFormatters()` returns memoized `{ timestamp(value, precision?),
    chartTick(ts), chartTooltip(ts) }` bound to `zoneOpts`.
- **Pure formatters** in `utils/format.ts` take the zone and the clock as arguments and
  never call `Date.now()` (house rule "no time bombs"). See Section 6.3.
  - Absolute formatting uses a cached `Intl.DateTimeFormat('en-US', { timeZone,
    year:'numeric', month:'2-digit', day:'2-digit', hour:'2-digit', minute:'2-digit',
    second:'2-digit', hourCycle:'h23', timeZoneName:'short' })` per resolved zone key
    (`'UTC'`, the IANA override or `'<runtime>'`), assembled from `formatToParts`.
  - For the UTC preference the label is the literal `UTC`. For Local it is the
    `timeZoneName` part, for example `PDT`, or `GMT+2` in some zones (A-006).
- **Header clock**: a new `components/HeaderClock.tsx` replaces `App.tsx` `UtcClock`.
  It owns its own 1 s `setInterval` and `useState(now())`, so only the clock
  re-renders each second (NFR-008). It renders a `button[type=button]` with
  `focusRing` and a `<time>` showing `formatTimestamp(now, zoneOpts, 'time')`
  (`HH:mm:ss UTC`). Its `aria-label` and `title` are
  `Time zone: UTC. Switch to local time` or
  `Time zone: local (<ABBR>). Switch to UTC`. Activating it calls `toggleZone()`.
  The clock changes from 12-hour to 24-hour format (A-005).
- **One relative ticker**: `hooks/useMinuteTick.ts` is a module-level external store
  read with `useSyncExternalStore`. A single `setInterval(30_000)` starts on the first
  subscriber and is cleared when the last one leaves. It returns a tick counter.
  `RelativeTime` subscribes and re-renders at most every 30 s (FR-098 requires at
  least every 60 s).
- `utils/rebootEta.ts` `formatUtc` **moves verbatim** into `utils/format.ts`
  (signature `(iso: string) => string | null`, minute precision, always UTC).
  `rebootEta.ts` re-exports it (`export { formatUtc } from './format'`) and keeps
  importing it for `rebootEtaTooltip`. Reboot-ETA and maintenance-window strings
  ignore the preference (BR-007 / A-007).

**Rationale:** Context plus a memoized value gives instant, app-wide propagation
without a reload (FR-104), and the clock's own state keeps the per-second tick local.
`useDashboardTick` was evaluated and **not reused**: it creates one interval and one
state per consumer, so a 50-row table would run 50 intervals. A shared external store
gives exactly one timer. Passing `localTimeZone` explicitly makes the Los Angeles ACs
deterministic on Windows, where mutating `process.env.TZ` at runtime is unreliable
(DEV-05).

**Alternatives considered:** Putting the 1 s clock in context was rejected (it would
re-render every consumer each second, violating NFR-008). Reading
`localStorage` during render was rejected (impure, and it throws in some browsers).
A `dayjs`/`date-fns-tz` style library was rejected under NFR-001.

**Consequences:** The `formatRunDate` signature gains an optional third argument
(`zoneOpts`, default UTC). Existing callers and tests keep working. Every display of
`Date#toLocale*String` is removed from non-test source, and the static guard enforces
this repo-wide (AC-110 is strengthened).

### AD-008: Enum label map: categories, contents, display sites

**Decision:** `components/ui/labels.ts` exports runtime `as const` arrays (the source
of each type union), the label records, and `enumLabel`, `humanize`, `toneFor` and the
types `LabelCategory`, `Severity`, `CommandStatus`, `CommandAction` and `EventType`.
The record types are `Record<Union, string>`, so a value missing from the map fails
`tsc`. Two compile-time assertions tie the map to `types/models.ts`: `Event['severity']`
and `SecurityFinding['severity']` must be assignable to `Severity`, and
`Command['status']` must equal `CommandStatus`.

| Category | Values → label |
|---|---|
| `severity` | info→"Info", warning→"Warning", critical→"Critical", pass→"Pass" |
| `commandStatus` | pending→"Pending", sent→"Sent", queued→"Queued", success→"Success", error→"Error" |
| `commandAction` (FRD "update kind", DEV-13) | os_update→"OS update", agent_update→"Agent update", agent_uninstall→"Agent uninstall", reboot→"Reboot", shutdown→"Shutdown", fetch_logs→"Fetch logs", enable_auto_updates→"Enable auto-updates", run_device_probe→"Run device probe", docker_start→"Container start", docker_stop→"Container stop", docker_restart→"Container restart", docker_update→"Container update", docker_bulk_update→"Bulk container update", docker_check_updates→"Container update check" |
| `eventType` (49 values from `internal/models/events.go` and `probes.go`, DEV-12) | device_online→"Device online", device_offline→"Device offline", disk_high→"Disk usage high", mem_high→"Memory usage high", update_available→"Update available", container_started→"Container started", container_stopped→"Container stopped", container_died→"Container died", container_oom_killed→"Container OOM-killed", container_created→"Container created", container_destroyed→"Container destroyed", container_paused→"Container paused", container_unpaused→"Container unpaused", container_update_started→"Container update started", container_update_completed→"Container update completed", container_update_failed→"Container update failed", service_stopped→"Service stopped", service_failed→"Service failed", process_missing→"Process missing", nic_down→"NIC down", command_sent→"Command sent", command_completed→"Command completed", agent_update_available→"Agent update available", agent_update_started→"Agent update started", agent_update_completed→"Agent update completed", agent_update_failed→"Agent update failed", log_errors→"Log errors", container_high_cpu→"Container CPU high", container_high_mem→"Container memory high", container_cpu_over_limit→"Container CPU over limit", ups_on_battery→"UPS on battery", ups_low_battery→"UPS battery low", ups_restored→"UPS power restored", cert_expiring→"Certificate expiring", cert_expired→"Certificate expired", usb_disconnected→"USB device disconnected", disk_smart_failing→"Disk SMART failing", disk_smart_temp→"Disk temperature high", gpu_temp→"GPU temperature high", gpu_metric→"GPU metric alert", nginx_5xx_high→"Nginx 5xx rate high", nginx_4xx_high→"Nginx 4xx rate high", nginx_request_high→"Nginx request rate high", auth_failure→"Authentication failure", detector_initialized→"Detector initialized", reboot_required→"Reboot required", probe_down→"Probe down", probe_recovered→"Probe recovered", tls_expiring→"TLS certificate expiring" |

- `enumLabel(category, value)`: null, undefined or `''` → `"—"`. A mapped value → its
  label. An unknown value → `humanize(value)`: replace `_` with space, lower-case
  everything, upper-case the first character (`some_new_value` → "Some new value").
  Demo-only event types such as `cpu_high` take this path.
- `toneFor(category, value)`: severity critical→danger, warning→warn, info→info,
  pass→success. commandStatus success→success, error→danger, sent→warn, queued→warn,
  pending→neutral. Everything else → neutral.

**Display sites migrated in this story:** `ActivityLog.tsx` (table and modal
status badges, action column and action filter `<option>` text; the local
`formatAction` is deleted), `Alerts.tsx` (severity via `SeverityBadge`, which becomes
a Badge wrapper; the type column uses `enumLabel('eventType', e.type)`),
`ActivityRiver.tsx` (severity label from the map instead of the local `label`),
`DeviceDetail.tsx` (alert-rule severity badge at :958; command toasts),
`ContainerDetail.tsx` (alert-rule severity at :344; the
`Command sent (<status>)` banner text uses `enumLabel('commandStatus', …)`),
`AlertRuleSettings.tsx` (RulesTable severity badge and the severity `<option>` text),
`CreateAlertDialog.tsx` (severity `<option>` text only), and
`SecurityScoreModal.tsx`/`LearnMore.tsx` (finding severity text only).

**Explicitly not mapped (deferred, Section 12.7):** probe types (`Probes.tsx`,
`DeviceProbes.tsx`; tests assert the raw `shell`), container states and health
(`ContainerStatusBadge`, `statusColor`), firewall, SELinux and AppArmor strings on
`Security.tsx`, server log levels (already upper-case constants), device status
(`StatusBadge` already capitalizes), and notification log status.

**Rationale:** FR-012 names four categories. Event type is a plain `string` on the
client, so an explicit union built from the Go source is the only way to make FR-013
"every value of the union" meaningful and machine-checked.

**Consequences:** A new server event type renders in humanized form until someone adds
it to `EVENT_TYPES`. That fallback is the intended behavior.

### AD-009: Honest error and empty states

**Decision:**
- **FleetOverview** wraps the device table in `QueryState` using the full `useDevices()`
  result (`isLoading`, `error`, `data`, `refetch`; `useDevices` already spreads the
  query). It passes `errorTitle="Couldn't load devices"`, a loading placeholder of five
  Skeleton rows inside the Card shell, and
  `isEmpty={d => d.length === 0 && !search}` with `empty={<SetupGuide inline/>}`. The
  standalone `{!search && filtered.length===0 && !isLoading && <SetupGuide inline/>}`
  is deleted. The "No devices match your search" row stays inside the table. An empty
  fleet now shows the guide **in place of** the empty table.
- **Device pages** (DeviceDetail, DeviceContainers, ContainerDetailPage) replace their
  `isLoading`/`!data` early returns with `if (!data) return <DeviceQueryFallback
  isLoading error onRetry={refetch} />`. The new `components/DeviceQueryFallback.tsx`
  renders:
  - 404 → `EmptyState` titled "Device not found" with a `Link to="/"` reading
    "Back to Fleet".
  - Any other error, including a network `TypeError` → `ErrorPanel` titled
    "Couldn't load device" with the message and Retry.
  - Otherwise → a page Skeleton (`role="status" aria-busy="true"`
    `aria-label="Loading"`, no "Loading..." text).
- **ContainerDetailPage**: when the device loaded but the container is absent (and no
  previously seen container exists), it renders `EmptyState` titled
  "Container not found" with a link to `/devices/:id/containers`. The existing
  `lastContainer` ref behavior stays.

**Rationale:** A single component for the three device pages keeps the 404 versus
other-error logic identical (FR-084/085). FleetOverview uses the generic QueryState
because its empty branch is ordinary.

### AD-010: Demo parity enforced by types and a key-set test

**Decision:**
- `demo-client.ts` declares `export const api: ApiClient = { … }` and
  `export const authApi: AuthClient = { … }`. `demo-settings.ts` declares
  `export const settingsApi: SettingsClient = { … }`. With a type annotation on an
  object literal, `tsc` reports any **missing** method (FR-111/112, AC-127 "build"
  half) and any **extra** method (excess-property check). The types are imported with
  `import type` from `./client` and `./settings`. Type-only imports are erased, and
  the alias regex requires `/api/client`, so the demo bundle never pulls in the real
  modules.
- `demo-client.ts` re-exports `ApiError` from `./errors`, so the value-export key sets
  of the two modules match (`api`, `authApi`, `ApiError`).
- New demo methods. All are inert: they use only `delay()` and in-memory data and
  never call `fetch`.
  - `getFleetHeartbeats(window)` → `{ since, until, devices, devices_with_gpu: [] }`.
    `devices[id]` holds, for **every** demo device, `demo.getDeviceHistory(id)`
    reversed into ascending time order and mapped to `FleetHeartbeat`. `data` comes
    from the same mapping as the existing `getHeartbeatHistory`, plus
    `net_rx_bytes_sec`/`net_tx_bytes_sec`/`disk_io_percent` derived
    deterministically from the snapshot index. That mapping moves into a shared
    helper `demo.toHeartbeatData(snapshot)` in `demo-data.ts` (FR-113).
  - `getFleetContainers()` → `demo.getFleetContainers()`, a new generator: the
    `getContainers()` rows attributed to the first demo device (`proxmox-01`) as
    `FleetContainerRow` with `stack = riot.group ?? ''` (FR-114).
  - `getDevice(id)` and `getContainerDetail(id, cid)` → `Promise.reject(new
    ApiError(404, 'HTTP 404: Not Found'))` when the record is absent (FR-117).
  - `getDeviceSummary(id)` → a short markdown string built from the device detail.
  - `getContainerLogs` → ten synthetic `ContainerLogEntry` lines.
  - Device probes: `demo.getDeviceProbes()` (new generator: two probes on
    `proxmox-01`, a shell probe and a port probe, with results). It backs
    `getAllDeviceProbes` (enriched with `device_hostname`), `getDeviceProbes(id)`
    (filtered) and `getDeviceProbeResults`. Create, update, delete and run resolve
    with plausible payloads.
  - `bulkDockerUpdate` → a `Command` (`status: 'queued'`). `setAutoPatch(id, e)` →
    `{ auto_patch: e }`. `updateDeviceLocation(id, l)` → `{ location: l }`. The
    existing `updateDeviceTags(id, t)` is fixed to return `{ tags: t }`.
  - Settings: `getFeatureToggles` → every key of `FEATURES` set to `true`.
    `saveFeatureToggles(t)` → `t`. `getAutomationConfig` → a plausible
    `AutomationConfig` (os_patch `window` 03:00-05:00 daily, docker_update
    `anytime`, stagger 600). `saveAutomationConfig(c)` → `c`. `getTLSSANs` →
    `{ sans: [] }`. `saveTLSSANs(s)` → `{ ok: true, sans: s }`. `regenerateTLS` →
    `{ ok: true, message: 'Demo mode: certificate not regenerated' }`.
- `FEATURES` moves from `hooks/useFeatures.ts` into a new `hooks/featureCatalog.ts`.
  `useFeatures.ts` re-exports `FEATURES` and `FeatureKey` unchanged. This avoids a
  demo-settings → useFeatures → api/settings (aliased back to demo-settings) import
  cycle.
- `DemoWebSocketProvider.useWebSocket` returns `{ connected: true }` (FR-110).
- **Parity test** (`api/demo-parity.test.ts`): it imports both real modules and both
  demo modules directly (vitest has no alias) and asserts that the sorted
  `Object.keys(api)`, `Object.keys(settingsApi)` and `Object.keys(authApi)` are equal,
  and that `Object.keys(await import(...))` value exports are equal per pair.
  Importing the real modules is side-effect-free (they only define functions).

**Rationale:** Types catch drift at compile time, which covers both builds via the
`build` script's `tsc`. The runtime key test covers a method added to the real client
under a `// @ts-expect-error` or an `any` cast.

**Consequences:** Some existing demo return shapes must be tightened to satisfy the
real return types (`updateDeviceTags`, `getDevice`, `getContainerDetail`). The
engineer fixes every `tsc` error the annotation surfaces and must not use `as any` to
silence them, except on parameters, which are already typed `any` today.

### AD-011: Migration scope (exhaustive)

**Decision:** Exactly these files migrate in this story. Everything else is deferred
(Section 12.7).

| Area | Files | What changes |
|---|---|---|
| Native dialogs → ConfirmDialog (8 confirm + 1 alert) | DeviceProbes, Probes (×2), AgentManagement, AlertRuleSettings (×2), LogSettings, NotificationSettings, DeviceDetail (delete) | ConfirmDialog with `danger` for deletes and `primary` for update-all; success toasts; the delete `alert()` is removed |
| Modals → Modal primitive | `ConfirmModal.tsx`, `UpdateConfirmModal.tsx`, FleetOverview `PatchReviewModal`, `ActivityLog` `CommandDetailModal`, AlertRuleSettings edit modal (:228) and `TemplatePicker` (:621), NotificationSettings edit modal (:222), App.tsx `UpdateBanner` modal (:200) | Wrap in `<Modal>`; delete own backdrop, z-index and Escape handling. `ContainerDetail` drawer is deleted |
| Banners → toasts | FleetOverview (bulk update, patch-all), DeviceDetail (commands, fetch logs, SecurityScoreModal remediation), AgentManagement (update-all result block) | Remove banner state and timers |
| Honest states | FleetOverview, DeviceDetail, DeviceContainers, ContainerDetailPage | AD-009 |
| Header polish | DeviceDetail header, SecurityScoreGauge, Security top grid | FR-119..FR-122 |
| Formatting and time | The 19 sites in FRD 7.4 + App clock + FleetOverview "ago" + Probes last-check + container uptime | Section 6.3 |
| Enum labels | AD-008 site list | AD-008 |
| Hostnames | 9 sites (FRD list + PerDeviceCharts) | Remove `uppercase` from the hostname element only |
| Chart colors | MetricChart, ContainerMetricChart, PerDeviceCharts, ContainerDetail, DeviceDetail, Alerts | AD-002 |
| Dead code | Sparkline, MiniScore, ContainerGroup, ContainerCard, ContainerDetail default export, `docker.ts` `formatContainerUptime` | AD-012 |
| Tooltip layer | `Tooltip.tsx` | `z-[100]` → `z-(--z-tooltip)` (FR-003) |

**Rationale:** This set fulfils every FR. The modal list resolves the FR-025 versus
Out-of-Scope contradiction with one rule (DEV-02).

### AD-012: Dead-code removal with a verification method

**Decision:** Remove `components/Sparkline.tsx`, `components/MiniScore.tsx`,
`components/ContainerGroup.tsx`, `components/ContainerCard.tsx`, the default-export
`ContainerDetail` drawer in `components/ContainerDetail.tsx` (lines 139-176: the
`Props` interface and the default export; `ContainerDetailContent` and every tab
helper stay) and `formatContainerUptime` in `utils/docker.ts` (its only live caller,
ContainerDetail, moves to `formatDuration`).

**Verification (must be recorded in the impl report):**
1. Before each deletion, a ripgrep for the import specifier, for example
   `rg -n "from ['\"][./]+(.*/)?(Sparkline|MiniScore|ContainerGroup|ContainerCard)['\"]" web/src`,
   returns only files that are themselves being deleted (`ContainerCard` is imported
   only by `ContainerGroup`).
2. For the drawer: `rg -n "import ContainerDetail\b|import ContainerDetail from|default as ContainerDetail" web/src`
   returns nothing.
3. For `formatContainerUptime`: `rg -n formatContainerUptime web/src` returns only
   the ContainerDetail call being migrated.
4. After deletion, `npx tsc --noEmit` is clean and the static guard test (AC-120/121)
   passes.

`utils/docker.ts` `groupContainers` and the `ContainerGroup` interface lose their only
consumer. They are **left in place** (D11 does not list them), and their removal is
deferred.

### AD-013: Test strategy and harness

**Decision:**
- One co-located test file per primitive (NFR-003), plus a static-guard suite
  `web/src/test/static-guards.test.ts`. The suite uses
  `import.meta.glob('../**/*.{ts,tsx}', { query: '?raw', import: 'default', eager: true })`
  to read sources. No `node:fs` is needed, which matters because tsconfig limits
  `types`. It excludes its own file from scans.
- Token tests read `../index.css?raw`.
- A shared helper `web/src/test/renderWithProviders.tsx` exports `TestProviders` and
  `renderWithProviders(ui, { route?, routes?, queryClient?, timeZone?:
  {localTimeZone?, storage?, now?}, toast?: {now?} })`. It wraps in QueryClient
  (`retry:false`), MemoryRouter, TimePreferenceProvider and ToastProvider. New page
  tests use it. Existing harnesses that must gain ToastProvider are updated in place
  (Section 12.4).
- Fake timers: use `vi.useFakeTimers()` (which also fakes `Date`) and `fireEvent` for
  timer-sensitive toast and relative-time tests. Where user-event is needed under fake
  timers, use `userEvent.setup({ advanceTimers: vi.advanceTimersByTime })`.
- The Los Angeles ACs (105, 114, 118) inject `localTimeZone: 'America/Los_Angeles'`
  through the provider or formatter options (DEV-05).
- Describe titles: `[UIF AC-0xx] <summary>`, one describe per AC or per small AC group
  as listed in Section 8.

**Consequences:** About 30 new test files and a net gain of well over 150 tests.
AC-141 (405 or more) is satisfied by a wide margin.

### AD-014: Commit plan

One logical change per commit, message format `[UI-FOUNDATION-001] <type>: <summary>`,
and each commit must leave `npx tsc --noEmit` and `npm run test:run` green. **No tag,
no push** (story instruction; docs-only exemption is irrelevant).

1. `feat: design tokens, z-index scale and chart color constants` (index.css,
   theme/tokens.ts, theme/chartColors.ts, theme/tokens.test.ts)
2. `feat: typed ApiError for API clients` (api/errors.ts, client.ts, settings.ts,
   client.test.ts)
3. `feat: canonical format module and time-zone preference` (utils/format.ts and its
   test, rebootEta.ts and its test, cron.ts, docker.ts, contexts/TimePreferenceProvider
   and its test, hooks/useMinuteTick.ts)
4. `feat: UI primitives (Button, Card, Badge, Modal, ConfirmDialog, EmptyState,
   Skeleton, QueryState, Toast, RelativeTime, Timestamp)` (components/ui/**,
   test/renderWithProviders.tsx)
5. `feat: mount toast and time providers; header clock toggle` (main.tsx, App.tsx,
   HeaderClock and its test)
6. `refactor: rebuild ConfirmModal and UpdateConfirmModal on Modal` (plus their tests)
7. `feat: replace native confirm() and alert() with ConfirmDialog` (DeviceProbes,
   Probes, AgentManagement, AlertRuleSettings, LogSettings, NotificationSettings, their
   modal migrations, and tests)
8. `feat: FleetOverview toasts, honest error states, PatchReviewModal on Modal`
   (plus tests)
9. `feat: DeviceDetail command toasts, delete dialog, honest device-page states,
   header polish` (DeviceDetail, DeviceQueryFallback, DeviceContainers,
   ContainerDetailPage, SecurityScoreGauge, and tests)
10. `refactor: route timestamps, durations and enum labels through shared modules;
    chart colors` (remaining 7.4 sites, AD-008 sites, charts, Tooltip.tsx, and tests)
11. `fix: render hostnames as reported; Security stat cards in one row` (plus tests)
12. `refactor: remove dead components and container drawer` (deletions,
    static-guards.test.ts)
13. `feat: demo clients at parity with real clients; demo WebSocket connected`
    (demo-*, featureCatalog, useFeatures, DemoWebSocketProvider, and tests)
14. `docs: UI-FOUNDATION-001 implementation report and after screenshots`

## 3a. FRD deviations and conflict resolutions (architect-owned)

| ID | FRD item | Resolution |
|---|---|---|
| DEV-01 | 7.1 success = green-500, info = sky-500 | Use **emerald-500/-400** and **blue-500/-400**. FR-002 ("must not visibly change hue") outranks the table defaults, and the existing status colors are emerald and blue. The FRD allows one-step shade adjustments. A hue-family substitution is a deviation, recorded here. `danger`/`accent`/`warn` follow the table, plus `-fg` text companions. |
| DEV-02 | FR-025 ("any modal whose file is edited") vs. Section 9 Out of Scope (App.tsx, ActivityLog, CreateAlertDialog, SecurityScoreModal … not migrated) | Rule: a hand-rolled modal migrates when its file gets a **structural edit** in this story. A file edited **only** for an FR-017 label substitution or an FR-108 class removal (CreateAlertDialog, SecurityScoreModal, LearnMore) does not trigger migration. App.tsx (new clock and provider wiring) and ActivityLog (formatter, label and timestamp rework inside the modal) get structural edits, so their modals migrate. The Out-of-Scope list wrongly assumed those files were untouched. AC-031 scans exactly the AD-011 modal list. |
| DEV-03 | AC-064 (no test spies on `window.alert`) vs. AC-073 (test has a `window.alert` spy) | AC-064's scan forbids **stubbing that drives flows**: `stubGlobal('confirm'|'alert'|'prompt')`, `window.(confirm\|alert\|prompt) =` and `spyOn(window, 'confirm'\|'prompt')`. A `vi.spyOn(window, 'alert')` used only for a `.not.toHaveBeenCalled()` assertion (AC-073) is permitted. |
| DEV-04 | AC-071 clicks "Delete all server logs" | The trigger button keeps its visible label **"Clear"**. The ConfirmDialog title is **"Delete all server logs"** and its message contains "cannot be undone". The test clicks "Clear". |
| DEV-05 | AC-105/114/118 "process time zone is America/Los_Angeles" | Tests inject `localTimeZone: 'America/Los_Angeles'` through `TimePreferenceProvider` or the formatter options. Runtime `process.env.TZ` changes are unreliable on Windows. Production passes nothing, which means the browser zone. |
| DEV-06 | FR-090 fallback "—" vs. ActivityLog's existing `-` for a null duration (asserted by `ActivityLog.test.tsx:173`) | `formatDuration` returns "—" for null, undefined, NaN and negative input. The ActivityLog call site keeps `'-'` for `duration_ms == null` to match the adjacent Exit Code `-`. |
| DEV-07 | FR-092 has no date-only form; cert expiry was date-only | Cert expiry sites (DeviceDetail :1465, CertificateSettings :114) use the minute form `YYYY-MM-DD HH:mm ZONE`. |
| DEV-08 | FR-091 future instant under 60 s unspecified | `|diff| < 60 s` gives "just now" in both directions. |
| DEV-09 | FR-105 for PerDeviceCharts, which shows no time at all today | Enable a tooltip label through `chartTooltip(ts)` so the chart shows the zone label. The X axis stays hidden. |
| DEV-10 | FR-059 adds a toast; AgentManagement also has an 8 s inline result block | Remove the inline block and its timer to avoid double reporting (BR-002 spirit). |
| DEV-11 | Initial focus for a primary ConfirmDialog unspecified | Focus goes to the Confirm button. Danger goes to Cancel (FR-033). |
| DEV-12 | FR-013 "every value of the event-type client-side union", but `Event.type` is `string` | Create `EVENT_TYPES` (49 values from Go `internal/models/events.go` and `probes.go`) as the union. `Event.type` stays `string`, and unknown values humanize. |
| DEV-13 | "Update kind" category | Implemented as `commandAction`, which covers all 14 agent command actions dispatched by `internal/agent/commands.go`, `os_update` and `agent_update` among them. |
| DEV-14 | Container uptime for a future `created` was "just now" | It now follows FR-090: negative gives "—". |
| DEV-15 | ContainerDetail container-command banner (`Command sent (pending)`) | Kept as a banner, but the status renders through `enumLabel` (FR-017). Converting it to toasts and ConfirmDialog is deferred, because D5 names only FleetOverview and DeviceDetail. |
| DEV-16 | Toast hook outside a provider unspecified | `useToast()` throws. Harnesses are updated (Section 12.4). |
| DEV-17 | None (additive) | Device queries skip retry on 404 (`retryUnlessNotFound`), so "Device not found" appears without a second request. |
| DEV-18 | NFR-010 (only `web/` and `docs/`) | **CHANGELOG.md (repo root) is not edited.** Release notes go into the impl report. |

No FRD AC is infeasible after these resolutions.

## 4. Component Changes

Paths are relative to the repo root.

| Action | File Path | Purpose |
|---|---|---|
| MODIFY | `web/src/index.css` | `@theme inline` color tokens (AD-001), `--animate-toast-in` + `@keyframes toast-in`, `:root` z-scale variables. Keep existing scrollbar rules |
| CREATE | `web/src/theme/tokens.ts` | `TOKEN_PALETTE`, `TOKEN_HEX`, `Z_INDEX` mirrors for tests |
| CREATE | `web/src/theme/chartColors.ts` | Shared recharts color constant (AD-002) |
| CREATE | `web/src/theme/tokens.test.ts` | AC-001, 002, 003, 007 |
| CREATE | `web/src/api/errors.ts` | `ApiError`, `isApiError`, `isNotFound`, `errorMessage`, `retryUnlessNotFound` |
| MODIFY | `web/src/api/client.ts` | Throw `ApiError` (messages unchanged, 401 unchanged); re-export `ApiError`; export `ApiClient`, `AuthClient` types |
| MODIFY | `web/src/api/client.test.ts` | Add AC-088 (status 404 + exact message + instanceof Error) and an AC-089 assertion; keep existing tests |
| MODIFY | `web/src/api/settings.ts` | `fetchJSON`/`mutate` throw `ApiError` (same messages); export `SettingsClient` type |
| MODIFY | `web/src/api/demo-client.ts` | `api: ApiClient`, `authApi: AuthClient`; add 14 methods; 404 rejections; fix `updateDeviceTags`; re-export `ApiError` |
| MODIFY | `web/src/api/demo-settings.ts` | `settingsApi: SettingsClient`; add 7 methods |
| MODIFY | `web/src/api/demo-data.ts` | `toHeartbeatData`, `getFleetHeartbeats`, `getFleetContainers`, `getDeviceProbes` (+ results), `getAutomationConfig` generators |
| CREATE | `web/src/api/demo-parity.test.ts` | AC-127, 128, 129, 130, 131, 132 |
| MODIFY | `web/src/contexts/DemoWebSocketProvider.tsx` | `connected: true` |
| CREATE | `web/src/contexts/DemoWebSocketProvider.test.tsx` | AC-126 |
| CREATE | `web/src/contexts/TimePreferenceProvider.tsx` | Provider, `useTimePreference`, `useTimeFormatters`, `safeLocalStorage` |
| CREATE | `web/src/contexts/TimePreferenceProvider.test.tsx` | AC-116 (provider level), AC-117 |
| CREATE | `web/src/hooks/useMinuteTick.ts` | Single shared 30 s ticker (useSyncExternalStore) |
| CREATE | `web/src/hooks/featureCatalog.ts` | `FEATURES` + `FeatureKey` moved here |
| MODIFY | `web/src/hooks/useFeatures.ts` | Import and re-export `FEATURES`/`FeatureKey` from `featureCatalog` |
| MODIFY | `web/src/utils/format.ts` | Add `formatBytes`, `formatDuration`, `formatRelative`, `formatTimestamp`, `formatChartTick`, `formatChartTooltip`, `formatUtc` (moved), `parseInstant`, types `TimeZonePref`/`ZoneOpts`/`TimestampPrecision`; keep 4 existing exports unchanged |
| MODIFY | `web/src/utils/format.test.ts` | AC-101..106, AC-118; keep existing tests |
| MODIFY | `web/src/utils/rebootEta.ts` | Remove local `formatUtc`/`pad2`; `export { formatUtc } from './format'` and import it |
| MODIFY | `web/src/utils/rebootEta.test.ts` | Add AC-107 case (Local preference has no effect); existing tests untouched |
| MODIFY | `web/src/utils/docker.ts` | Delete `formatBytes` body and `formatContainerUptime`; `export { formatBytes } from './format'` |
| MODIFY | `web/src/utils/cron.ts` | `formatRunDate(value, fallback='-', opts: ZoneOpts = {zone:'UTC'})` → `formatTimestamp(d, opts, 'minute')` |
| CREATE | `web/src/components/ui/index.ts` | Barrel |
| CREATE | `web/src/components/ui/focusRing.ts` | Shared focus-visible ring class string |
| CREATE | `web/src/components/ui/Button.tsx` | Button + `Spinner` |
| CREATE | `web/src/components/ui/Button.test.tsx` | AC-008..011, AC-012 (Button) |
| CREATE | `web/src/components/ui/Card.tsx` | Card |
| CREATE | `web/src/components/ui/Card.test.tsx` | AC-013 |
| CREATE | `web/src/components/ui/labels.ts` | Enum arrays, label records, `enumLabel`, `humanize`, `toneFor`, types (AD-008) |
| CREATE | `web/src/components/ui/labels.test.ts` | AC-014..019 |
| CREATE | `web/src/components/ui/Badge.tsx` | Badge |
| CREATE | `web/src/components/ui/Badge.test.tsx` | AC-020 |
| CREATE | `web/src/components/ui/Modal.tsx` | Modal + stack registry + focus trap |
| CREATE | `web/src/components/ui/Modal.test.tsx` | AC-012 (close), AC-022..028 |
| CREATE | `web/src/components/ui/ConfirmDialog.tsx` | ConfirmDialog |
| CREATE | `web/src/components/ui/ConfirmDialog.test.tsx` | AC-029, AC-032..041 |
| CREATE | `web/src/components/ui/EmptyState.tsx` | EmptyState |
| CREATE | `web/src/components/ui/EmptyState.test.tsx` | AC-043 |
| CREATE | `web/src/components/ui/Skeleton.tsx` | Skeleton |
| CREATE | `web/src/components/ui/Skeleton.test.tsx` | AC-044, AC-142 (Skeleton) |
| CREATE | `web/src/components/ui/QueryState.tsx` | QueryState + `ErrorPanel` |
| CREATE | `web/src/components/ui/QueryState.test.tsx` | AC-012 (Retry), AC-045..050 |
| CREATE | `web/src/components/ui/Toast.tsx` | `ToastProvider`, `useToast`, viewport, item |
| CREATE | `web/src/components/ui/Toast.test.tsx` | AC-012 (close), AC-051, AC-053..062, AC-142 (toast) |
| CREATE | `web/src/components/ui/RelativeTime.tsx` | RelativeTime |
| CREATE | `web/src/components/ui/RelativeTime.test.tsx` | AC-111, AC-112 |
| CREATE | `web/src/components/ui/Timestamp.tsx` | Timestamp |
| CREATE | `web/src/components/ui/Timestamp.test.tsx` | Precision forms, "—" fallback, title/dateTime (supports AC-117) |
| CREATE | `web/src/components/HeaderClock.tsx` | Header clock toggle (AD-007) |
| CREATE | `web/src/components/HeaderClock.test.tsx` | AC-012 (clock), AC-113, 114, 115, 116 |
| CREATE | `web/src/components/DeviceQueryFallback.tsx` | Shared device-page loading/404/error view (AD-009) |
| CREATE | `web/src/test/renderWithProviders.tsx` | `TestProviders` / `renderWithProviders` helper |
| CREATE | `web/src/test/static-guards.test.ts` | AC-004, 005, 031, 063, 064, 108, 110, 120, 121 (export shape) |
| MODIFY | `web/src/main.tsx` | Wrap `<App/>` in `TimePreferenceProvider` > `ToastProvider` |
| MODIFY | `web/src/App.tsx` | Replace `UtcClock` with `<HeaderClock/>`; `UpdateBanner` modal → `<Modal>` |
| MODIFY | `web/src/components/ConfirmModal.tsx` | Same props; rendered with `Modal` + `Button`; no z-index |
| MODIFY | `web/src/components/ConfirmModal.test.tsx` | Variant assertions → `data-variant`; add dialog-name test (AC-030) |
| MODIFY | `web/src/components/UpdateConfirmModal.tsx` | Same props; rendered with `Modal` + `Button` |
| CREATE | `web/src/components/UpdateConfirmModal.test.tsx` | AC-030 (dialog named by title) |
| MODIFY | `web/src/components/ActivityLog.tsx` | Modal migration; labels for status/action; `Timestamp`; `formatDuration`; delete local `formatAction`/`formatDuration`/`statusColor` (Badge tones) |
| MODIFY | `web/src/components/ActivityLog.test.tsx` | Migrate label, tone and backdrop assertions; add AC-109 (ActivityLog) |
| MODIFY | `web/src/components/ContainerDetail.tsx` | Delete drawer default export + `Props`; `chartColors`; `Timestamp` (:389, :623 time); uptime via `formatDuration`; severity Badge (:344); banner status label |
| MODIFY | `web/src/components/MetricChart.tsx` | Delete local `formatBytes` → `formatBytesPerSec`; `chartColors`; `useTimeFormatters` tick/tooltip |
| MODIFY | `web/src/components/ContainerMetricChart.tsx` | `chartColors`; `useTimeFormatters` tick/tooltip |
| MODIFY | `web/src/components/Tooltip.tsx` | `z-[100]` → `z-(--z-tooltip)` |
| MODIFY | `web/src/components/SeverityBadge.tsx` | Becomes `<Badge category="severity" value={severity}/>` |
| MODIFY | `web/src/components/SecurityScoreGauge.tsx` | Visible "Security score" caption; button `aria-label="Security score <n>, grade <g>. Open details"` |
| MODIFY | `web/src/components/SecurityScoreModal.tsx` | Label-only: finding severity text via `enumLabel('severity', …)` (no modal migration, DEV-02) |
| MODIFY | `web/src/components/CreateAlertDialog.tsx` | Label-only: severity `<option>` text via `enumLabel` (no modal migration, DEV-02) |
| MODIFY | `web/src/pages/LearnMore.tsx` | Label-only: finding severity text via `enumLabel` (:1197) |
| MODIFY | `web/src/pages/FleetOverview.tsx` | QueryState (AD-009); bulk-update ConfirmDialog + toasts; patch-all mutation + toasts; PatchReviewModal on Modal with pending Button; remove banners/timers/`formatAgo`; `RelativeTime`; hostname case |
| MODIFY | `web/src/pages/FleetOverview.test.tsx` | Harness + ToastProvider; `useDevices` mock → `vi.fn`; api mock + `getAgentVersions`/`bulkUpdateAgents`/`bulkPatchDevices`; new AC tests |
| CREATE | `web/src/pages/FleetOverview.states.test.tsx` | AC-090..093 (real `useDevices`, mocked api and WS) |
| MODIFY | `web/src/pages/DeviceDetail.tsx` | DeviceQueryFallback; command ConfirmDialogs + toasts; delete ConfirmDialog (remove `alert`); remediation and fetch-logs toasts; remove banners/5 s resets; header nav landmark + danger Delete; gauge caption wrapper; `Timestamp` (:1465 minute, :1666, :1703); `formatDuration` (uptime, UPS runtime); delete local `formatUptime`/`formatBytes` (NicTable uses `formatBytes` from utils/format); `chartColors`; severity Badge (:958); `formatRunDate(…, zoneOpts)`; hostname case |
| MODIFY | `web/src/pages/DeviceDetail.test.tsx` | Harness gains ToastProvider (no assertion changes) |
| CREATE | `web/src/pages/DeviceDetail.flows.test.tsx` | AC-073, 074, 081..087, 094, 095, 098, 100, 122, 136, 137 |
| MODIFY | `web/src/pages/DeviceContainers.tsx` | DeviceQueryFallback; `retryUnlessNotFound`; hostname case (×2) |
| CREATE | `web/src/pages/DeviceContainers.test.tsx` | AC-096, 097, 123 (both places) |
| MODIFY | `web/src/pages/ContainerDetailPage.tsx` | DeviceQueryFallback; "Container not found" EmptyState; hostname case |
| MODIFY | `web/src/pages/ContainerDetailPage.test.tsx` | Add AC-096, 097, 099, 121 (render), 123 |
| MODIFY | `web/src/pages/DeviceProbes.tsx` | Delete ConfirmDialog + toast; `Timestamp` (:214); hostname case |
| CREATE | `web/src/pages/DeviceProbes.test.tsx` | AC-065, 123 |
| MODIFY | `web/src/pages/Probes.tsx` | Two delete ConfirmDialogs + toasts; last-check `RelativeTime` (:199) |
| MODIFY | `web/src/pages/Probes.test.tsx` | Harness + ToastProvider; migrate the stubbed-confirm test; add AC-066, 067 |
| MODIFY | `web/src/pages/ProbeDetail.tsx` | `Timestamp` (:127) |
| MODIFY | `web/src/pages/Alerts.tsx` | `Timestamp` (:124); event type label; border tokens (no hex); hostname case |
| CREATE | `web/src/pages/Alerts.test.tsx` | AC-021, 109 (Alerts), 123 (Alerts) |
| MODIFY | `web/src/pages/Security.tsx` | Top stat grid one row at xl; hostname case; StatCard markup unchanged |
| MODIFY | `web/src/pages/Security.test.tsx` | Add AC-123 (Security), AC-125 |
| MODIFY | `web/src/pages/dashboard/ActivityRiver.tsx` | `Timestamp precision="time"` (:120); severity label from map |
| MODIFY | `web/src/pages/dashboard/PerDeviceCharts.tsx` | `chartColors`; tooltip time label (DEV-09); hostname case (:116) |
| MODIFY | `web/src/pages/settings/AgentManagement.tsx` | Update-all ConfirmDialog + toast; remove result block/timer; delete local `formatDuration` → `utils/format` |
| MODIFY | `web/src/pages/settings/AgentManagement.test.tsx` | Harness + ToastProvider; add AC-068 |
| MODIFY | `web/src/pages/settings/AlertRuleSettings.tsx` | Shared delete ConfirmDialog for both tables + toast; edit modal + TemplatePicker → Modal; severity Badge + option labels; delete `formatCooldown` → `formatDuration` |
| MODIFY | `web/src/pages/settings/AlertRuleSettings.test.tsx` | Harness + ToastProvider; add AC-069, 070 |
| MODIFY | `web/src/pages/settings/LogSettings.tsx` | Clear-logs ConfirmDialog + toast; `Timestamp` (:105) |
| CREATE | `web/src/pages/settings/LogSettings.test.tsx` | AC-071 |
| MODIFY | `web/src/pages/settings/NotificationSettings.tsx` | Delete-channel ConfirmDialog + toast; edit modal → Modal |
| CREATE | `web/src/pages/settings/NotificationSettings.test.tsx` | AC-072 |
| MODIFY | `web/src/pages/settings/CertificateSettings.tsx` | `Timestamp` (:114 minute, :230, :231) |
| CREATE | `web/src/pages/settings/CertificateSettings.test.tsx` | AC-109 (CertificateSettings) |
| DELETE | `web/src/components/Sparkline.tsx` | Dead (AD-012) |
| DELETE | `web/src/components/MiniScore.tsx` | Dead |
| DELETE | `web/src/components/ContainerGroup.tsx` | Dead |
| DELETE | `web/src/components/ContainerCard.tsx` | Dead (only imported by ContainerGroup) |
| CREATE | `docs/implementation/UI-FOUNDATION-001-impl-report.md` | Engineering report incl. dead-code grep output, bundle sizes, release notes (DEV-18) |
| CREATE | `docs/implementation/UI-FOUNDATION-001-screenshots/after-fleet.png`, `after-toast-stack.png`, `after-device-detail-chart-tooltip.png`, `after-device-detail-header.png`, `after-dashboard-demo.png`, `after-security.png` | Screenshot ACs (006, 052, 119, 124, 133, 135, 138) |

**Explicitly unchanged:** `web/package.json`, `web/package-lock.json`,
`web/vite.config.ts`, `web/vitest.config.ts`, `web/tsconfig.json`,
`web/src/components/Terminal.tsx`, `Section.tsx`, `SetupGuide.tsx`,
`DeviceProbeModal.tsx`, `ProbeModal.tsx`, `ComposeStackSection.tsx`,
`NetworkClusterGroup.tsx` (they consume the migrated `UpdateConfirmModal` through its
unchanged props), `CHANGELOG.md`, and every file outside `web/` and `docs/`.

## 5. Data Model Changes

No backend schema, migration or API contract change.

**Client-persisted data (new):**

| Key | Store | Values | Rules |
|---|---|---|---|
| `riot-time-zone` | `localStorage` | `"UTC"` \| `"Local"` | Missing, unreadable or any other value means `UTC`. Write failures are swallowed. Not synced across tabs (out of scope). |

**New client types (TS only):**
- `ApiError { name: 'ApiError'; status: number; message: string }` extends `Error`.
- `TimeZonePref = 'UTC' | 'Local'`; `ZoneOpts = { zone: TimeZonePref; localTimeZone?: string }`;
  `TimestampPrecision = 'second' | 'minute' | 'time'`.
- `Toast { id: number; tone: 'success' | 'info' | 'error'; title: string; message?: string; createdAt: number }`.
- `Severity`, `CommandStatus`, `CommandAction`, `EventType` unions (AD-008), derived
  from `as const` arrays in `labels.ts`.
- `ApiClient`, `AuthClient` (`typeof api` / `typeof authApi`) and `SettingsClient`
  (`typeof settingsApi`).

`types/models.ts` is **unchanged**. `Event.type` and `Command.action` stay `string`.

## 6. API / Interface Contract

No HTTP endpoint is added or changed. The contracts below are TypeScript module
interfaces, as signatures only.

### 6.1 `api/errors.ts`

- `class ApiError extends Error { readonly status: number; constructor(status: number, message: string) }`
- `isApiError(e: unknown): e is ApiError`
- `isNotFound(e: unknown): boolean`
- `errorMessage(e: unknown, fallback?: string): string` (default fallback
  `"Something went wrong. Please try again."`)
- `retryUnlessNotFound(failureCount: number, error: unknown): boolean`

Thrown-error contract of `api`/`settingsApi`:

| Response | Thrown |
|---|---|
| 2xx | (resolves) |
| 401 | unchanged: `window.location.reload()` then `Error('Unauthorized')` |
| other non-2xx, fetchJSON / settings mutate | `ApiError(status, "HTTP <status>: <statusText>")` |
| other non-2xx, client mutations | `ApiError(status, "HTTP <status>")` |
| other non-2xx, sendCommand / getDeviceSummary | `ApiError(status, body.error \|\| "HTTP <status>")` |
| network failure | native `TypeError` (unchanged) |

### 6.2 `components/ui` (props; `?` = optional, defaults in brackets)

- `Button(props: ButtonHTMLAttributes & { variant?: 'primary'|'secondary'|'ghost'|'danger' ['secondary']; size?: 'sm'|'md' ['md']; pending?: boolean; pendingLabel?: string [children]; ref?: Ref<HTMLButtonElement> })`
  Renders `data-variant`, `data-size`, `type` ['button'], `disabled = disabled || pending`, `aria-busy` when pending.
- `Spinner(props: { className?: string })`: `aria-hidden`, `animate-spin`.
- `Card(props: HTMLAttributes<HTMLElement> & { title?: ReactNode; titleAs?: 'h2'|'h3' ['h2']; actions?: ReactNode; padding?: 'none'|'md' ['md'] })`
- `Badge(props: { tone?: BadgeTone; children: ReactNode; className?: string } | { category: LabelCategory; value: string|null|undefined; tone?: BadgeTone; className?: string })`, renders `data-tone`.
- `labels.ts`: `SEVERITIES`, `COMMAND_STATUSES`, `COMMAND_ACTIONS`, `EVENT_TYPES` (readonly arrays); `LABELS`; `enumLabel(category: LabelCategory, value: string|null|undefined): string`; `humanize(value: string): string`; `toneFor(category: LabelCategory, value: string|null|undefined): BadgeTone`.
- `Modal(props: { open: boolean; onClose: () => void; title: ReactNode; description?: ReactNode; children?: ReactNode; footer?: ReactNode; size?: 'sm'|'md'|'lg'|'xl' ['md'; max-w-sm/md/lg/3xl]; pending?: boolean; dismissOnBackdrop?: boolean [true]; showCloseButton?: boolean [true]; closeLabel?: string ['Close']; initialFocusRef?: RefObject<HTMLElement|null>; panelClassName?: string; bodyClassName?: string })`
- `ConfirmDialog<T>(props: { open: boolean; title: string; message: ReactNode; confirmLabel: string; pendingLabel?: string ['Working…']; cancelLabel?: string ['Cancel']; variant?: 'primary'|'danger' ['primary']; onConfirm: () => T | Promise<T>; onClose: () => void; onSuccess?: (result: T) => void; successToast?: string | ToastInput | ((result: T) => string | ToastInput); children?: ReactNode })`
  `onClose` is called on Cancel, Escape and backdrop (each ignored while pending) and after success.
- `EmptyState(props: { title: string; description?: ReactNode; action?: { label: string; onClick: () => void }; icon?: ReactNode; children?: ReactNode })`
- `Skeleton(props: { className?: string; lines?: number [1] })`
- `QueryState<T>(props: { query: { isLoading?: boolean; error?: unknown; data: T | undefined; refetch?: () => unknown }; isEmpty?: (data: T) => boolean; empty?: ReactNode; loading?: ReactNode; errorTitle?: string ["Couldn't load data"]; renderError?: (error: unknown, retry: () => void) => ReactNode; children: (data: T) => ReactNode })`
- `ErrorPanel(props: { title?: string ["Couldn't load data"]; error: unknown; onRetry?: () => void })`: `role="alert"`, message via `errorMessage`, Retry Button (secondary) when `onRetry`.
- `ToastProvider(props: { children: ReactNode; duration?: number [5000]; max?: number [4]; now?: () => number [() => Date.now()] })`
- `useToast(): { show(t: ToastInput & { tone: ToastTone }): number; success(title: string, message?: string): number; info(title: string, message?: string): number; error(title: string, message?: string): number; dismiss(id: number): void }`. `ToastInput = { tone?: ToastTone; title: string; message?: string }`. An empty title falls back to a per-tone default ("Done", "Notice", "Something went wrong"). It throws without a provider.
- `RelativeTime(props: { value: string|number|Date|null|undefined; fallback?: string ['—']; className?: string })`: `<time dateTime={ISO} title={formatTimestamp(…)}>{formatRelative(…)}</time>`; invalid gives `<span>{fallback}</span>`.
- `Timestamp(props: { value: string|number|Date|null|undefined; precision?: TimestampPrecision ['second']; fallback?: string ['—']; className?: string })`: `<time dateTime={ISO}>{formatTimestamp(…)}</time>`.
- `focusRing: string`.

### 6.3 `utils/format.ts` (pure; never reads the clock or the environment)

- Existing, **unchanged output**: `formatBytesPerSec`, `formatBytesPerSecAxis`, `formatPercent`, `formatCount`.
- `formatBytes(bytes: number): string`: base 1024, `"N B"` below 1024, one decimal KB/MB/GB/TB; non-finite or negative gives `"0 B"`.
- `formatDuration(seconds: number | null | undefined): string`: below 1 → `"<round(ms)>ms"`; below 60 → `"<floor>s"`; below 3600 → `"<floor(m)>m"`; below 86400 → `"<h>h <m>m"`; else `"<d>d <h>h"`; null, undefined, NaN, ±Infinity or negative give `"—"`.
- `formatRelative(value: string|number|Date|null|undefined, nowMs: number): string`: `|Δ| < 60 s` → `"just now"`; past → `"Nm ago"`/`"Nh ago"`/`"Nd ago"` (floor; thresholds 60 min, 24 h); future → `"in Nm"`/`"in Nh"`/`"in Nd"`; invalid → `"—"`.
- `formatTimestamp(value: string|number|Date|null|undefined, opts: ZoneOpts, precision: TimestampPrecision = 'second'): string`: second `YYYY-MM-DD HH:mm:ss ZONE`, minute `YYYY-MM-DD HH:mm ZONE`, time `HH:mm:ss ZONE`. 24 h (`h23`). ZONE is the literal `UTC` for UTC, otherwise the `en-US` short zone name of `opts.localTimeZone` or the runtime zone. Invalid gives `"—"`, never `"Invalid Date"`.
- `formatChartTick(ts: number, opts: ZoneOpts): string` gives `HH:mm` in the zone (no label, A-008). `formatChartTooltip(ts: number, opts: ZoneOpts): string` equals `formatTimestamp(ts, opts, 'second')`.
- `formatUtc(iso: string): string | null`: moved verbatim from `rebootEta.ts` (`YYYY-MM-DD HH:MM UTC`, null if unparseable).
- `parseInstant(value): Date | null`: ISO string, epoch ms number or Date. Strings of digits only are **not** interpreted here, because `cron.ts parseTimestamp` keeps that job.

### 6.4 `contexts/TimePreferenceProvider.tsx`

- `TimePreferenceProvider(props: { children: ReactNode; storage?: Pick<Storage,'getItem'|'setItem'> | null; localTimeZone?: string; now?: () => number })`
- `useTimePreference(): { zone: TimeZonePref; setZone(z: TimeZonePref): void; toggleZone(): void; zoneOpts: ZoneOpts; now: () => number }`
- `useTimeFormatters(): { timestamp(value, precision?): string; chartTick(ts: number): string; chartTooltip(ts: number): string }`
- `safeLocalStorage(): Storage | null`
- `hooks/useMinuteTick.ts`: `useMinuteTick(): number`

### 6.5 `components/DeviceQueryFallback.tsx`

`DeviceQueryFallback(props: { isLoading: boolean; error: unknown; onRetry: () => void })`
renders the 404 view (EmptyState "Device not found" plus "Back to Fleet" link to
`/`), the error view (ErrorPanel "Couldn't load device") or the loading Skeleton.

## 7. Sequence / Flow

**7.1 ConfirmDialog (generic, also used by every FR-056..FR-062/065/074 flow)**
1. The user clicks a trigger. The page sets `target` state and the dialog opens in
   Idle. Focus goes to Cancel (danger) or Confirm (primary).
2. Cancel, Escape or the backdrop calls `onClose()`. The page clears `target` and no
   API call is made (FR-063). Focus returns to the trigger.
3. Confirm: if the in-flight ref is set, it returns (FR-029). Otherwise it sets the
   ref, clears any error and enters Pending. Both buttons disable, the confirm button
   shows Spinner and `pendingLabel`, and Escape and the backdrop are ignored.
4. It awaits `onConfirm()`, typically `mutation.mutateAsync(args)`.
   - Resolve: the success toast is raised from `successToast` (if given), then
     `onSuccess(result)`, then `onClose()`.
   - Reject: if still mounted, it leaves Pending and shows `role="alert"` with
     `errorMessage(e)`. The dialog stays open (FR-031). Retry returns to step 3 and
     Cancel to step 2.

**7.2 FleetOverview bulk agent update (FR-034, FR-068, FR-069)**
`onConfirm` is `bulkUpdateMutation.mutateAsync()`, which loops `api.bulkUpdateAgents`
over the outdated versions sequentially and sums `{sent, skipped}`. The dialog shows
"Updating…" until the whole loop settles. On success it raises the toast
"Agent update dispatched" with message `"<sent> sent"` plus `", <skipped> skipped (offline)"`
when skipped > 0, and the mutation `onSuccess` invalidates `['agent-versions']`. On
failure the inline error appears and **no toast is raised** (BR-002). The confirm label
is the fixed "Update all"; the `isPending ? … : …` label hack is removed.

**7.3 FleetOverview patch-all (FR-070..FR-072)**
`patchAllMutation = useMutation(() => api.bulkPatchDevices('full'))`. PatchReviewModal
receives `pending={patchAllMutation.isPending}`, passes `pending` to `Modal` and renders
its confirm as `Button pending pendingLabel="Dispatching…"`. On success it raises the
toast "Patch commands dispatched" with
`"<sent> sent, <queued> queued, <skipped> skipped (<total> total)"`, closes the modal,
clears the detail and invalidates `['patch-status']`. On error it raises the error
toast "Couldn't dispatch patches" with the message, and the modal stays open (FR-072).
The detail-fetch `catch { setPatchDetail(null) }` is unchanged (OQ-001, deferred).

**7.4 DeviceDetail commands (FR-074..FR-078)**
- Reboot and Shutdown (danger) and Patch and Update Agent (primary) open one
  ConfirmDialog keyed by `confirmAction`. `onConfirm` is
  `commandMutation.mutateAsync({action, params})`, with params `{mode:'full'}` for
  `os_update`. `successToast` is
  `(cmd) => ({ title: "<enumLabel('commandAction', action)> command sent to <hostname> — <enumLabel('commandStatus', cmd.status)>" })`.
  It uses the **requested** action, not `cmd.action`. The mutation has no
  `onSuccess`/`onError` timers.
- SecurityScoreModal remediation: `onRunCommand(action, params)` calls
  `remediationMutation.mutate`. Its `onSuccess` raises the same success toast format
  and its `onError` calls `toast.error('Command failed', errorMessage(e))` (FR-077).
- Fetch logs: `onSuccess` raises `toast.success('Log fetch requested', 'Logs will refresh in a few seconds.')`
  and keeps the existing 3 s `invalidateQueries` (a data refresh, not a banner).
  `onError` raises `toast.error("Couldn't fetch logs", errorMessage(e))`.
- Delete: danger ConfirmDialog, `pendingLabel="Deleting…"`, `onConfirm` is
  `deleteMutation.mutateAsync()`, `successToast` is `Device "<hostname>" deleted`,
  `onSuccess` calls `navigate('/')`. The mutation's `onSuccess` keeps invalidating
  `['devices']`. The `onError`/`alert`/`console.error` handler is removed (FR-065).

**7.5 Device page resolution (FR-084..FR-087)**
`useQuery(['device', id], { retry: retryUnlessNotFound, … })`. If data exists, the
page renders (stale data survives a background error). If there is no data and
`isNotFound(error)`, it shows "Device not found" and a Fleet link. If there is no data
and any other error, it shows "Couldn't load device", the message and Retry
(`refetch`). Otherwise it shows the Skeleton.

**7.6 Time preference toggle**
HeaderClock click calls `toggleZone()`. The provider state changes, the storage write
is attempted inside try/catch, and the memoized context value changes. Every
`Timestamp` and `RelativeTime`, plus the chart formatters, re-render with the new
`zoneOpts` (FR-104). The 1 s clock interval re-renders only HeaderClock.

**7.7 Demo dashboard**
DemoWebSocketProvider reports connected, so DisconnectedBanner is hidden.
`getFleetHeartbeats` returns ascending series for all eight devices, which fills
`useFleetMetrics`, and PerDeviceCharts renders. `getFleetContainers` fills the
leaderboard. `getFeatureToggles` resolves, which removes the "No queryFn" console
floods.

## 8. Acceptance Criteria Mapping

Test file paths are relative to `web/src/`. Describe titles use the prefix `[UIF AC-0xx]`.

| AC | Fulfilled By | Test Strategy (file › describe) |
|---|---|---|
| AC-001 | `index.css` `@theme inline` tokens | `theme/tokens.test.ts` › `[UIF AC-001] color tokens defined in @theme` |
| AC-002 | Token aliases = `TOKEN_PALETTE` (DEV-01 documented) | `theme/tokens.test.ts` › `[UIF AC-002] token values alias the existing palette` |
| AC-003 | `:root` z-scale | `theme/tokens.test.ts` › `[UIF AC-003] z-index layering order` |
| AC-004 | `chartColors` + Alerts border tokens | `test/static-guards.test.ts` › `[UIF AC-004] no hex literals in chart and touched files` |
| AC-005 | Token-only primitives | `test/static-guards.test.ts` › `[UIF AC-005] components/ui uses tokens only` |
| AC-006 | AD-001 aliases | Screenshot `after-fleet.png` vs `before-fleet*.png` (manual) |
| AC-007 | `TOKEN_HEX` contrast (text, text-muted on surface, raised) | `theme/tokens.test.ts` › `[UIF AC-007] text tokens meet WCAG AA 4.5:1` |
| AC-008 | Button variants/sizes + `data-*` | `components/ui/Button.test.tsx` › `[UIF AC-008] variants and sizes are distinct` |
| AC-009 | `type="button"` default | `Button.test.tsx` › `[UIF AC-009] default type does not submit forms` |
| AC-010 | native `disabled` | `Button.test.tsx` › `[UIF AC-010] disabled blocks clicks` |
| AC-011 | `pending` + Spinner + `aria-busy` | `Button.test.tsx` › `[UIF AC-011] pending state` |
| AC-012 | `focusRing` on Button, Modal close, toast close, Retry, HeaderClock | `Button.test.tsx`, `Modal.test.tsx`, `Toast.test.tsx`, `QueryState.test.tsx`, `components/HeaderClock.test.tsx` › `[UIF AC-012] focus-visible ring` (one `it` per file) |
| AC-013 | Card heading + `bg-raised border-border` | `components/ui/Card.test.tsx` › `[UIF AC-013] card title is a heading on raised surface` |
| AC-014 | `LABELS.severity` | `components/ui/labels.test.ts` › `[UIF AC-014] severity labels` |
| AC-015 | `LABELS.commandStatus` | `labels.test.ts` › `[UIF AC-015] command status labels` |
| AC-016 | `LABELS.eventType` over `EVENT_TYPES` | `labels.test.ts` › `[UIF AC-016] event type labels have no underscores` |
| AC-017 | `LABELS.commandAction` | `labels.test.ts` › `[UIF AC-017] update kind labels` |
| AC-018 | `humanize` fallback | `labels.test.ts` › `[UIF AC-018] unknown value humanized` |
| AC-019 | empty → "—" | `labels.test.ts` › `[UIF AC-019] empty value fallback` |
| AC-020 | Badge `category`/`value` + `toneFor` | `components/ui/Badge.test.tsx` › `[UIF AC-020] badge uses the shared map and tone` |
| AC-021 | Alerts SeverityBadge + event type label | `pages/Alerts.test.tsx` › `[UIF AC-021] alerts show mapped labels` |
| AC-022 | Modal `role=dialog`, `aria-modal`, `aria-labelledby` | `components/ui/Modal.test.tsx` › `[UIF AC-022] dialog semantics` |
| AC-023 | initial focus effect | `Modal.test.tsx` › `[UIF AC-023] initial focus inside` |
| AC-024 | Tab trap | `Modal.test.tsx` › `[UIF AC-024] Tab wraps forward` |
| AC-025 | Shift+Tab trap | `Modal.test.tsx` › `[UIF AC-025] Shift+Tab wraps backward` |
| AC-026 | Escape handler (top, not pending) | `Modal.test.tsx` › `[UIF AC-026] Escape closes` |
| AC-027 | focus restore | `Modal.test.tsx` › `[UIF AC-027] focus restored to opener` |
| AC-028 | backdrop handler | `Modal.test.tsx` › `[UIF AC-028] backdrop closes, panel does not` |
| AC-029 | stack registry | `components/ui/ConfirmDialog.test.tsx` › `[UIF AC-029] stacked Escape closes only the top dialog` |
| AC-030 | ConfirmModal, UpdateConfirmModal, PatchReviewModal on Modal | `components/ConfirmModal.test.tsx` › `[UIF AC-030] ConfirmModal is a named dialog`; `components/UpdateConfirmModal.test.tsx` › `[UIF AC-030] UpdateConfirmModal is a named dialog`; `pages/FleetOverview.test.tsx` › `[UIF AC-030] PatchReviewModal is a named dialog` |
| AC-031 | AD-011 modal list | `test/static-guards.test.ts` › `[UIF AC-031] migrated modals carry no ad-hoc z-index` |
| AC-032 | ConfirmDialog Pending | `ConfirmDialog.test.tsx` › `[UIF AC-032] pending visuals` |
| AC-033 | `pending` → Modal ignores Escape | `ConfirmDialog.test.tsx` › `[UIF AC-033] Escape ignored while pending` |
| AC-034 | `pending` → backdrop ignored | `ConfirmDialog.test.tsx` › `[UIF AC-034] backdrop ignored while pending` |
| AC-035 | in-flight ref | `ConfirmDialog.test.tsx` › `[UIF AC-035] no double invoke` |
| AC-036 | resolve → toast → onClose | `ConfirmDialog.test.tsx` › `[UIF AC-036] success closes with a toast` |
| AC-037 | reject → inline `role=alert` | `ConfirmDialog.test.tsx` › `[UIF AC-037] failure stays open with inline error` |
| AC-038 | `errorMessage` fallback | `ConfirmDialog.test.tsx` › `[UIF AC-038] message-less rejection` |
| AC-039 | Error → Pending | `ConfirmDialog.test.tsx` › `[UIF AC-039] retry after failure` |
| AC-040 | Error → Closed | `ConfirmDialog.test.tsx` › `[UIF AC-040] cancel after failure` |
| AC-041 | danger Button + Cancel focus | `ConfirmDialog.test.tsx` › `[UIF AC-041] danger variant and initial focus` |
| AC-042 | Flow 7.2 | `pages/FleetOverview.test.tsx` › `[UIF AC-042] bulk update shows Updating… while in flight` |
| AC-043 | EmptyState | `components/ui/EmptyState.test.tsx` › `[UIF AC-043] title, description and action` |
| AC-044 | Skeleton `aria-hidden` | `components/ui/Skeleton.test.tsx` › `[UIF AC-044] hidden from assistive technology` |
| AC-045 | QueryState loading branch | `components/ui/QueryState.test.tsx` › `[UIF AC-045] loading` |
| AC-046 | ErrorPanel + Retry → refetch | `QueryState.test.tsx` › `[UIF AC-046] error with retry` |
| AC-047 | `isEmpty` → `empty` | `QueryState.test.tsx` › `[UIF AC-047] empty` |
| AC-048 | children(data) | `QueryState.test.tsx` › `[UIF AC-048] data` |
| AC-049 | data-first precedence | `QueryState.test.tsx` › `[UIF AC-049] stale data survives background error` |
| AC-050 | error before empty | `QueryState.test.tsx` › `[UIF AC-050] error beats empty` |
| AC-051 | viewport region | `components/ui/Toast.test.tsx` › `[UIF AC-051] toast renders in the Notifications region` |
| AC-052 | viewport placement | Screenshot `after-toast-stack.png` (manual, demo) |
| AC-053 | 5000 ms timer | `Toast.test.tsx` › `[UIF AC-053] success auto-dismisses at 5 s` |
| AC-054 | 5000 ms timer | `Toast.test.tsx` › `[UIF AC-054] info auto-dismisses at 5 s` |
| AC-055 | no timer for error | `Toast.test.tsx` › `[UIF AC-055] error persists` |
| AC-056 | close button | `Toast.test.tsx` › `[UIF AC-056] Dismiss notification removes toast` |
| AC-057 | pause/resume remaining | `Toast.test.tsx` › `[UIF AC-057] hover pauses and resumes with remaining time` |
| AC-058 | focus pause | `Toast.test.tsx` › `[UIF AC-058] focus pauses` |
| AC-059 | newest-first order | `Toast.test.tsx` › `[UIF AC-059] newest on top` |
| AC-060 | max 4 eviction | `Toast.test.tsx` › `[UIF AC-060] at most four visible` |
| AC-061 | polite `ol`, error `role=alert` | `Toast.test.tsx` › `[UIF AC-061] live-region politeness` |
| AC-062 | provider above Routes | `Toast.test.tsx` › `[UIF AC-062] toast survives navigation` (MemoryRouter with two routes) |
| AC-063 | all confirm/alert replaced | `test/static-guards.test.ts` › `[UIF AC-063] no native dialogs in source` |
| AC-064 | test migration (DEV-03) | `test/static-guards.test.ts` › `[UIF AC-064] tests do not stub native dialogs`; count confirmed by AC-141 |
| AC-065 | DeviceProbes ConfirmDialog | `pages/DeviceProbes.test.tsx` › `[UIF AC-065] delete probe confirm names probe; cancel skips API` |
| AC-066 | Probes server delete | `pages/Probes.test.tsx` › `[UIF AC-066] delete server probe confirms and toasts` |
| AC-067 | Probes device-probe delete | `Probes.test.tsx` › `[UIF AC-067] delete device probe confirm; cancel skips API` |
| AC-068 | AgentManagement update-all | `pages/settings/AgentManagement.test.tsx` › `[UIF AC-068] update all confirm and counts toast` |
| AC-069 | AlertRuleSettings global table delete | `pages/settings/AlertRuleSettings.test.tsx` › `[UIF AC-069] delete rule (global table) uses danger dialog` |
| AC-070 | AlertRuleSettings granular table delete | `AlertRuleSettings.test.tsx` › `[UIF AC-070] delete rule (granular table) uses danger dialog` |
| AC-071 | LogSettings clear (DEV-04) | `pages/settings/LogSettings.test.tsx` › `[UIF AC-071] clear logs danger dialog says cannot be undone` |
| AC-072 | NotificationSettings delete | `pages/settings/NotificationSettings.test.tsx` › `[UIF AC-072] delete channel toasts Channel deleted` |
| AC-073 | Flow 7.4 delete failure | `pages/DeviceDetail.flows.test.tsx` › `[UIF AC-073] delete failure inline, no alert` |
| AC-074 | Flow 7.4 delete success | `DeviceDetail.flows.test.tsx` › `[UIF AC-074] delete success navigates and toasts hostname` |
| AC-075 | banners removed | `pages/FleetOverview.test.tsx` › `[UIF AC-075] no inline result banners` |
| AC-076 | Flow 7.2 success | `FleetOverview.test.tsx` › `[UIF AC-076] bulk update success toast counts` |
| AC-077 | Flow 7.2 failure | `FleetOverview.test.tsx` › `[UIF AC-077] bulk update failure inline` |
| AC-078 | Flow 7.3 pending | `FleetOverview.test.tsx` › `[UIF AC-078] patch-all pending control` |
| AC-079 | Flow 7.3 success | `FleetOverview.test.tsx` › `[UIF AC-079] patch-all success toast` |
| AC-080 | Flow 7.3 failure | `FleetOverview.test.tsx` › `[UIF AC-080] patch-all failure toast, modal stays open` |
| AC-081 | banners removed | `DeviceDetail.flows.test.tsx` › `[UIF AC-081] no inline command banners` |
| AC-082 | variant per action | `DeviceDetail.flows.test.tsx` › `[UIF AC-082] command dialog variants` |
| AC-083 | humanized toast | `DeviceDetail.flows.test.tsx` › `[UIF AC-083] command toast is humanized` |
| AC-084 | inline command error | `DeviceDetail.flows.test.tsx` › `[UIF AC-084] command failure inline` |
| AC-085 | remediationMutation toasts | `DeviceDetail.flows.test.tsx` › `[UIF AC-085] remediation commands use toasts` |
| AC-086 | fetch-logs success toast | `DeviceDetail.flows.test.tsx` › `[UIF AC-086] fetch logs success toast` |
| AC-087 | fetch-logs error toast | `DeviceDetail.flows.test.tsx` › `[UIF AC-087] fetch logs failure toast` |
| AC-088 | `ApiError` in fetchJSON | `api/client.test.ts` › `[UIF AC-088] typed error carries status` |
| AC-089 | 401 branch untouched | `api/client.test.ts` › `[UIF AC-089] 401 still reloads` (the existing 401 test stays as well) |
| AC-090 | FleetOverview QueryState error | `pages/FleetOverview.states.test.tsx` › `[UIF AC-090] fetch error shows error panel, not SetupGuide` |
| AC-091 | Retry → refetch | `FleetOverview.states.test.tsx` › `[UIF AC-091] retry recovers` |
| AC-092 | empty → SetupGuide | `FleetOverview.states.test.tsx` › `[UIF AC-092] empty fleet shows SetupGuide` |
| AC-093 | loading branch | `FleetOverview.states.test.tsx` › `[UIF AC-093] loading shows placeholder, no SetupGuide` |
| AC-094 | DeviceQueryFallback 404 | `DeviceDetail.flows.test.tsx` › `[UIF AC-094] 404 shows Device not found` |
| AC-095 | DeviceQueryFallback error | `DeviceDetail.flows.test.tsx` › `[UIF AC-095] non-404 shows Couldn't load device` |
| AC-096 | Fallback on container pages | `pages/DeviceContainers.test.tsx` and `pages/ContainerDetailPage.test.tsx` › `[UIF AC-096] 404 shows Device not found` |
| AC-097 | Fallback on container pages | `DeviceContainers.test.tsx` and `ContainerDetailPage.test.tsx` › `[UIF AC-097] non-404 shows error and retry` |
| AC-098 | TypeError is non-404 | `DeviceDetail.flows.test.tsx` › `[UIF AC-098] network error treated as non-404` |
| AC-099 | Container not found EmptyState | `ContainerDetailPage.test.tsx` › `[UIF AC-099] missing container in loaded device` |
| AC-100 | Fallback Skeleton | `DeviceDetail.flows.test.tsx` › `[UIF AC-100] loading skeleton, no Loading... text` |
| AC-101 | `formatBytes` | `utils/format.test.ts` › `[UIF AC-101] formatBytes` |
| AC-102 | `formatDuration` | `format.test.ts` › `[UIF AC-102] formatDuration` |
| AC-103 | `formatRelative` | `format.test.ts` › `[UIF AC-103] formatRelative` |
| AC-104 | `formatTimestamp` UTC | `format.test.ts` › `[UIF AC-104] absolute timestamp UTC` |
| AC-105 | `formatTimestamp` Local (DEV-05) | `format.test.ts` › `[UIF AC-105] absolute timestamp Local (America/Los_Angeles)` |
| AC-106 | invalid → "—" | `format.test.ts` › `[UIF AC-106] invalid timestamp` |
| AC-107 | `formatUtc` moved, re-exported | `utils/rebootEta.test.ts` › `[UIF AC-107] reboot ETA stays UTC under Local preference` (plus the unchanged existing tests) |
| AC-108 | duplicate formatters deleted | `test/static-guards.test.ts` › `[UIF AC-108] no duplicate formatter definitions` |
| AC-109 | Timestamp at call sites | `components/ActivityLog.test.tsx`, `pages/Alerts.test.tsx`, `pages/settings/CertificateSettings.test.tsx` › `[UIF AC-109] timestamps carry UTC label` |
| AC-110 | 19 sites migrated | `test/static-guards.test.ts` › `[UIF AC-110] no Date toLocale* calls in source` (all non-test `web/src` files, no exclusions; stronger than the 19) |
| AC-111 | RelativeTime markup | `components/ui/RelativeTime.test.tsx` › `[UIF AC-111] markup` |
| AC-112 | `useMinuteTick` | `RelativeTime.test.tsx` › `[UIF AC-112] refreshes over time` |
| AC-113 | HeaderClock default | `components/HeaderClock.test.tsx` › `[UIF AC-113] defaults to UTC HH:mm:ss` |
| AC-114 | toggleZone | `HeaderClock.test.tsx` › `[UIF AC-114] toggle switches to local` |
| AC-115 | storage persistence | `HeaderClock.test.tsx` › `[UIF AC-115] preference persists across remount` |
| AC-116 | safe storage read | `HeaderClock.test.tsx` › `[UIF AC-116] storage failure falls back to UTC`; `contexts/TimePreferenceProvider.test.tsx` › `[UIF AC-116] write failure keeps session value` |
| AC-117 | memoized context propagation | `contexts/TimePreferenceProvider.test.tsx` › `[UIF AC-117] toggle updates Timestamp and RelativeTime title without remount` |
| AC-118 | `formatChartTick`/`formatChartTooltip` | `utils/format.test.ts` › `[UIF AC-118] chart tick and tooltip formatters` |
| AC-119 | chart tooltips via `useTimeFormatters` | Screenshot `after-device-detail-chart-tooltip.png` (manual) |
| AC-120 | AD-012 deletions | `test/static-guards.test.ts` › `[UIF AC-120] dead components removed and unimported` |
| AC-121 | drawer removed, content kept | `test/static-guards.test.ts` › `[UIF AC-121] ContainerDetail has no default export`; `pages/ContainerDetailPage.test.tsx` › `[UIF AC-121] container details still render` |
| AC-122 | h1 without `uppercase` | `DeviceDetail.flows.test.tsx` › `[UIF AC-122] hostname case preserved in h1` |
| AC-123 | hostname sites | `FleetOverview.test.tsx`, `Security.test.tsx`, `Alerts.test.tsx`, `DeviceContainers.test.tsx`, `DeviceProbes.test.tsx`, `ContainerDetailPage.test.tsx` › `[UIF AC-123] hostname not uppercased` |
| AC-124 | hostname sites | Screenshots `after-fleet.png`, `after-device-detail-header.png` (manual) |
| AC-125 | Security header untouched | `pages/Security.test.tsx` › `[UIF AC-125] Per-Device Security header keeps uppercase` |
| AC-126 | Demo WS connected | `contexts/DemoWebSocketProvider.test.tsx` › `[UIF AC-126] demo WebSocket reports connected` |
| AC-127 | `api: ApiClient` + key test | `api/demo-parity.test.ts` › `[UIF AC-127] demo api parity`; `tsc --noEmit` (build) |
| AC-128 | `settingsApi: SettingsClient` + key test | `demo-parity.test.ts` › `[UIF AC-128] demo settings parity`; `tsc --noEmit` |
| AC-129 | `getFleetHeartbeats` generator | `demo-parity.test.ts` › `[UIF AC-129] demo heartbeats per device ascending` |
| AC-130 | `getFleetContainers`, `getFeatureToggles` | `demo-parity.test.ts` › `[UIF AC-130] demo containers and toggles` |
| AC-131 | inert demo mutations | `demo-parity.test.ts` › `[UIF AC-131] demo mutations never fetch` (spy `globalThis.fetch`; `Promise.all` of every mutating method) |
| AC-132 | demo 404 `ApiError` | `demo-parity.test.ts` › `[UIF AC-132] unknown demo device is 404` |
| AC-133 | AD-010 | Screenshot `after-dashboard-demo.png` (manual, 5 s after load) |
| AC-134 | AD-010 | Demo walkthrough with console (manual; recorded in impl report) |
| AC-135 | Security grid `xl:grid-cols-none xl:grid-flow-col xl:auto-cols-fr` | Screenshot `after-security.png` at 1440 px (manual) |
| AC-136 | SecurityScoreGauge caption + `aria-label` | `DeviceDetail.flows.test.tsx` › `[UIF AC-136] gauge caption and accessible name` |
| AC-137 | `nav[aria-label="Device sections"]`, danger Button outside | `DeviceDetail.flows.test.tsx` › `[UIF AC-137] Delete is a separated danger button` |
| AC-138 | header layout | Screenshot `after-device-detail-header.png` (manual) |
| AC-139 | no dependency edits | Build: `git diff main -- web/package.json` shows no `dependencies`/`devDependencies` change |
| AC-140 | — | Build: `cd web && npx tsc --noEmit` exits 0 |
| AC-141 | — | Build: `cd web && npm run test:run`, 0 failures, 405 or more passing |
| AC-142 | `motion-reduce:animate-none` / `motion-safe:` only | `Skeleton.test.tsx` and `Toast.test.tsx` › `[UIF AC-142] reduced motion` |
| AC-143 | — | Build: `npm run build` on `main` and branch; compare gzip of `dist/assets/index-*.js` (≤ +20 KB); record in impl report |
| AC-144 | — | Build: `npm run build` and `npm run build:demo` both succeed. On Windows, run demo as `$env:VITE_DEMO='true'; npx tsc; npx vite build`, because the npm script uses POSIX env syntax |
| AC-145 | Section 4 scope | Build: `git diff --name-only main...HEAD`, every path under `web/` or `docs/` |

## 9. Error Handling

| Failure | Technical handling | User sees | Logging |
|---|---|---|---|
| Device list fetch fails, no cached data | QueryState error branch | ErrorPanel "Couldn't load devices", message, Retry | none (react-query) |
| Device fetch 404 | `isNotFound` in DeviceQueryFallback; no retry | "Device not found" + Back to Fleet | none |
| Device fetch other or network error | DeviceQueryFallback error branch; one retry (global policy) | "Couldn't load device", message, Retry | none |
| Background refetch fails with data cached | data-first precedence | existing content, no flicker (A-009) | none |
| ConfirmDialog action rejects | caught in dialog; `errorMessage(e)` | inline `role="alert"` in dialog; retry or cancel; **no toast** (BR-002) | none |
| Patch-all rejects | mutation `onError` | error toast (persistent), modal stays open | none |
| Remediation or fetch-logs command rejects | mutation `onError` | error toast (persistent) | none |
| Rejection without a message | `errorMessage` fallback | "Something went wrong. Please try again." | none |
| 401 anywhere | unchanged reload path | page reloads to login | unchanged |
| localStorage read throws or is invalid | try/catch, whitelist | UTC, no error | none |
| localStorage write throws | swallowed | toggle still applies for the session | none |
| Unparseable timestamp | `formatTimestamp`/`formatRelative` → "—" | "—" | none |
| Unknown enum value | `humanize` | humanized label | none |
| `useToast` outside provider | throws `Error('useToast must be used within ToastProvider')` | developer error, caught in tests | console (React) |
| Demo unknown device | `ApiError(404)` | "Device not found" | none |

Error toasts never auto-dismiss (BR-003). The removed `console.error` in device delete
is intentional: the inline error is the single report.

## 10. Security Considerations

- **Injection:** every toast title and message, inline error and ErrorPanel message is
  rendered as React text, never `dangerouslySetInnerHTML`. Server-supplied strings
  (`sendCommand` `error` body, `statusText`) are therefore inert. Enum labels come
  from a static map, and humanized fallbacks are plain text.
- **Destructive-action safety:** ConfirmDialog's in-flight guard prevents a double
  reboot, shutdown or delete from double clicks (FR-029). Danger dialogs focus Cancel,
  so an accidental Enter does nothing destructive (A-002). The pending lock prevents
  dismissing the dialog while a destructive request is in flight, which could otherwise
  mislead the user about the outcome.
- **Storage:** the only new persisted value is a whitelisted enum. Nothing sensitive is
  stored, and reads tolerate tampering ("banana" resolves to UTC).
- **Error disclosure:** `ApiError` carries only status and the existing message. No
  response bodies or stacks are surfaced beyond what is shown today.
- **Auth:** 401 handling is byte-identical (FR-080). No new endpoints and no new
  credentials.
- **Demo build:** demo mutations never call `fetch` (AC-131), so the public GitHub
  Pages demo cannot be used to issue requests.
- **Supply chain:** no new dependencies (NFR-001).
- **Focus trap:** the trap is keyboard-only and does not block screen-reader virtual
  cursors outside the dialog. `aria-modal="true"` signals modality. Acceptable for
  WCAG at this scope.

## 11. Performance Considerations

- `Intl.DateTimeFormat` instances are cached per zone key in `utils/format.ts`
  (construction is the expensive part). Formatting a cell is a `formatToParts` call
  plus string assembly.
- One relative-time interval app-wide (`useMinuteTick`) instead of one per row.
- The header clock's 1 s state is local to HeaderClock (NFR-008). The time-preference
  context value is memoized and changes only on toggle.
- Toast timers are per item, at most four, and torn down on unmount.
- `useMinuteTick` re-renders only `RelativeTime` consumers, every 30 s.
- Bundle: primitives and context are small, and four dead components plus a drawer are
  removed. The estimated main-chunk gzip delta is under +8 KB, well inside the
  NFR-007 limit of 20 KB. It is measured in the impl report.
- No new network requests. Demo data generators are synchronous and in-memory.
- Device queries no longer retry 404s, which saves one request per missing device.

## 12. Implementation Notes for Engineers

1. **Import rule:** pages import `ApiError`/`isNotFound`/`errorMessage` from
   `../api/errors`. Importing them from `../api/client` breaks every test that mocks
   `../api/client` with a partial factory.
2. **Tailwind v4 syntax:** use `z-(--z-modal)` (parentheses, not brackets) and
   `border-l-[3px]`. Do not add `tailwind.config.*`. Check that `bg-danger/15`
   opacity modifiers render in the dev build, since they compile to `color-mix` with
   aliased vars.
3. **No `Date.now()` / `new Date()` without arguments in testable code.** The defaults
   `now = () => Date.now()` live only in `ToastProvider` and
   `TimePreferenceProvider` default props (composition root). `formatRelative` takes
   `nowMs`. `demo-data.ts` is exempt (demo fixture generation).
4. **Harnesses needing ToastProvider** (they render `useToast` directly or through
   ConfirmDialog): `FleetOverview.test.tsx`, `DeviceDetail.test.tsx`,
   `Probes.test.tsx`, `AgentManagement.test.tsx` and `AlertRuleSettings.test.tsx`.
   Wrap their existing `renderWithProviders` with `<ToastProvider>` or switch them to
   the shared helper. Do not change their existing assertions except as listed in item
   5.
5. **Existing tests that must be migrated (not deleted):**
   - `components/ConfirmModal.test.tsx`: "applies danger variant by default" and
     "applies primary variant when specified". Change `className.toContain('red'|'blue')`
     to `toHaveAttribute('data-variant','danger'|'primary')`.
   - `components/ActivityLog.test.tsx`:
     - "renders table with command data": change 'OS Update' → 'OS update' and
       'Docker Restart' → 'Container restart'.
     - "shows status badges with correct styling": change the text
       'success'/'error'/'pending' → 'Success'/'Error'/'Pending', and the class
       assertions → `data-tone` success/danger/neutral.
     - "highlights non-zero exit code in red": change the 'Docker Restart' lookup →
       'Container restart'. The exit-code `text-red-400` stays, so the 'red'
       assertion is unchanged.
     - "closes modal when backdrop is clicked": click
       `screen.getByTestId('modal-backdrop')`.
   - `pages/Probes.test.tsx` "Delete button calls api.deleteDeviceProbe…": drop the
     `vi.stubGlobal('confirm', …)`/`unstubAllGlobals`. After clicking the row Delete,
     click `within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' })`.
   - `pages/FleetOverview.test.tsx`: the `useDevices` mock becomes a `vi.fn` with the
     same default return. Add `getAgentVersions: vi.fn().mockResolvedValue([])`,
     `bulkUpdateAgents` and `bulkPatchDevices` to the api mock. The defaults preserve
     today's behavior for the existing tests.
   - No other existing test asserts native dialogs, banner text, uppercase hostnames
     or `Loading...` presence. `ContainerDetailPage.test.tsx:287,299` assert
     `Loading...` is **absent**, which stays true.
6. **Keep these unchanged so existing assertions stay valid:** the DeviceDetail
   "Update Agent" and "Patch" button classes (`DeviceDetail.test.tsx:79` asserts
   `amber`), Security `StatCard` markup and colors (`Security.test.tsx:136,206,257,307`),
   PatchReviewModal badge text `reboot required` and `<n> reboot-class` (REBOOT-ETA
   tests), `rebootEtaTooltip` strings, and the ActivityLog `'-'` for null duration and
   exit code.
7. **Deferred (explicitly out of this story):**
   - Un-migrated modals: SecurityScoreModal, CreateAlertDialog, DeviceProbeModal,
     ProbeModal, SetupGuide modal.
   - `components/Section.tsx` and every other gray-class sweep.
   - The ContainerDetail container-command flow (still ConfirmModal plus a banner),
     DeviceContainers' "Check queued" 5 s reset, and ComposeStackSection and
     NetworkClusterGroup inline states.
   - OQ-001 silent failures (location, tags, auto-patch, patch-review detail fetch).
   - Labels for probe types, container states, firewall/SELinux/AppArmor values,
     server log levels and notification log status.
   - AgentManagement local-time maintenance-window inputs (pre-existing; BR-007
     tension noted for a follow-up).
   - The `text-[9px|10px|11px]` normalization.
   - `utils/docker.ts` `groupContainers`/`ContainerGroup` type (orphaned).
   - The undefined `animate-river-enter` keyframes in ActivityRiver (pre-existing).
   - Body scroll lock.
   - Cross-tab preference sync.
8. **Modal migration recipe:** delete the outer `fixed inset-0 … z-*` div and the
   inner `stopPropagation` panel. Pass `title`, move the header close button into
   Modal's built-in close, move the footer buttons into `footer`, and use
   `panelClassName` for `max-h-[85vh] flex flex-col` layouts (PatchReviewModal).
   Delete any local Escape `useEffect` (ActivityLog). Keep `aria-label="Close"` as the
   default close label, which `ActivityLog.test.tsx:211` relies on.
9. **DeviceDetail header:** wrap the Docker, Probes and Terminal links in
   `<nav aria-label="Device sections">`. Move Delete into the right-hand action
   cluster as `<Button variant="danger" size="sm">Delete</Button>`, rendered for every
   device status, after a `border-l border-border pl-3` divider. SecurityScoreGauge
   renders its own "Security score" caption under the ring.
10. **Security grid:** `grid grid-cols-2 sm:grid-cols-3 xl:grid-cols-none xl:grid-flow-col xl:auto-cols-fr gap-4`
    gives one row at ≥1280 px for 4 or 5 cards.
11. **Static-guard regexes** (in `static-guards.test.ts`):
    - Hex: `/#[0-9a-fA-F]{3,8}\b/`, ignoring `&#` HTML entities and the SVG `url(#…)`
      gradient references used by recharts (`` `url(#${…})` ``). Strip
      `url\(#[^)]*\)` before scanning.
    - Gray: `/\bgray-\d/`.
    - Native dialogs: `/\b(window\.)?(confirm|alert|prompt)\s*\(/` on non-test source,
      after stripping `//` and `/* */` comments. Exclude identifiers such as
      `onConfirm(`, `setConfirm…(` and `.confirm(` via a `(?<![\w.])` look-behind.
    - Formatter definitions:
      `/(function\s+|const\s+)(formatBytes|formatUptime|formatDuration)\b/` outside
      `utils/format.ts`.
    - Locale dates: `/\.toLocale(Date|Time)?String\(/` across non-test source. There is
      no number `toLocaleString` usage today (verified).
    - Default export of ContainerDetail: `/export\s+default\b/` must not match
      `components/ContainerDetail.tsx`.
12. **Windows:** run `go`-free commands only. Use `npx vitest run` without `-race`. The
    `build:demo` npm script uses `VITE_DEMO=true …` POSIX syntax. Use the PowerShell
    form in AC-144 or the `.claude/launch.json` "web-demo" config for screenshots, and
    do not change the script.
13. **Demo typing:** after annotating `api: ApiClient`, fix each `tsc` error by making
    the demo return the real shape. `getDevice` must return
    `Promise<DeviceDetailResponse>` and reject with `ApiError(404)`. Do not widen
    `ApiClient`.
14. **SetupGuide inline:** it renders only through QueryState `empty`. Its "+ Add
    Device" modal path (`showGuide`) is unchanged.

## 13. Definition of Done

- [ ] All Section 4 component changes are implemented, with no file outside `web/` or
      `docs/` touched (AC-145) and `CHANGELOG.md` untouched (DEV-18).
- [ ] Every AC row in Section 8 marked as a test has a test with the listed file and
      `[UIF AC-0xx]` describe.
- [ ] Every existing test listed in Section 12.5 is migrated, none is deleted, and only
      tests whose subjects are deleted under FR-106/FR-107 may be removed (none exist
      today).
- [ ] `cd web && npx tsc --noEmit` exits 0 (AC-140). There is no `any` in new
      primitives and no `as any` silencing demo parity errors.
- [ ] `cd web && npm run test:run`: 0 failures, 405 or more tests (AC-141).
- [ ] Regular and demo production builds both succeed (AC-144), and the gzip delta of
      the main entry chunk is ≤ 20 KB and recorded (AC-143).
- [ ] `web/package.json` dependencies and devDependencies are identical to `main`
      (AC-139).
- [ ] The dead-code verification greps (AD-012) are pasted into the impl report.
- [ ] The six `after-*.png` screenshots are saved and compared with their `before-*`
      counterparts in the impl report (AC-006, 052, 119, 124, 133, 135, 138).
- [ ] The demo console walkthrough (AC-134) is recorded: Dashboard, Fleet, one
      DeviceDetail, Security and every Settings tab, with no "No queryFn" and no
      unhandled rejection.
- [ ] Manual keyboard pass: Tab and Shift+Tab trap, Escape, focus restore and the
      focus-visible ring on every primitive in the demo build.
- [ ] No migration is needed, and none was added (frontend-only story).
- [ ] Commits follow AD-014. **No tag, no push.**
