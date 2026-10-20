# rIOt UI primitives and design tokens

This guide is for developers building or changing screens in the rIOt web dashboard (`web/`). It covers the shared building blocks introduced by UI-FOUNDATION-001: design tokens, a z-index scale, UI primitives, toasts, honest loading and error states, and the one place where times, durations, and enum labels are formatted.

**The rule:** new UI uses these primitives and tokens. Do not copy a primitive's Tailwind classes into a page, and do not write a new modal, button, badge, or timestamp format by hand. If a primitive cannot do what a screen needs, extend the primitive.

Import everything from the barrel file:

```tsx
import { Button, Card, Badge, Modal, ConfirmDialog, EmptyState, Skeleton, QueryState, useToast } from '../components/ui'
```

`Timestamp`, `RelativeTime`, `ToastProvider`, `ErrorPanel`, `Spinner`, `focusRing`, `enumLabel`, and the label arrays are exported there too.

## Contents

- [Design tokens](#design-tokens)
- [z-index scale](#z-index-scale)
- [Primitives](#primitives)
- [Toasts](#toasts)
- [ConfirmDialog](#confirmdialog)
- [Loading and error states: QueryState](#loading-and-error-states-querystate)
- [Time, durations, and bytes: utils/format.ts](#time-durations-and-bytes-utilsformatts)
- [Enum labels: labels.ts](#enum-labels-labelsts)
- [Chart colors](#chart-colors)
- [Typed API errors](#typed-api-errors)
- [Rules enforced by tests](#rules-enforced-by-tests)
- [Testing a screen](#testing-a-screen)

## Design tokens

### Scrolling and data tables

Thin scrollbars are the dashboard default, applied globally in `web/src/index.css` using theme tokens and both standard and WebKit rules. New scrolling areas inherit this style; do not add page-specific scrollbar colors or widths. Keep the existing `scrollbar-hide` exception for navigation strips only.

Wide tables must scroll inside their own container, not widen the page. Give dense tables explicit column widths, truncate long values with the full text available, keep identifying columns visible while scrolling, and right-align numerical values with tabular numerals. Optional columns should have a discoverable chooser. Shared Docker `DataTable` supports widths, pinned columns and numerical alignment, and virtualizes rows for larger fleets.

Tokens are declared once, in the `@theme inline` block of `web/src/index.css`. Each one aliases an existing Tailwind palette color, so the token name is the only thing a component references. Tailwind v4 turns each `--color-<name>` into the utilities `bg-<name>`, `text-<name>`, `border-<name>`, `ring-<name>`, and so on, including opacity forms such as `bg-danger-hover/20`.

| Token | Utility examples | Aliases | Use for |
|-------|------------------|---------|---------|
| `surface` | `bg-surface` | gray-950 | Page background |
| `raised` | `bg-raised` | gray-900 | Cards, modals, toasts |
| `raised-2` | `bg-raised-2` | gray-800 | Hover fill, secondary buttons, skeletons |
| `border` | `border-border` | gray-800 | Hairline borders |
| `border-strong` | `border-border-strong` | gray-700 | Dialog and control borders |
| `text` | `text-text` | gray-100 | Primary text |
| `text-muted` | `text-text-muted` | gray-400 | Secondary text |
| `accent` | `bg-accent` | blue-600 | Primary button, focus ring |
| `accent-hover` | `hover:bg-accent-hover` | blue-500 | Primary button hover |
| `link` | `text-link` | blue-400 | Links and accent text |
| `success`, `success-fg` | `bg-success`, `text-success-fg` | emerald-500, emerald-400 | Success fill and text |
| `warn`, `warn-fg` | `bg-warn`, `text-warn-fg` | amber-500, amber-400 | Warning fill and text |
| `danger`, `danger-hover`, `danger-fg` | `bg-danger`, `text-danger-fg` | red-600, red-500, red-400 | Danger button, error fill and text |
| `info`, `info-fg` | `bg-info`, `text-info-fg` | blue-500, blue-400 | Informational fill and text |

Notes:

- The `-fg` tokens are for text on the dark surfaces. Tests check their contrast ratio.
- `web/src/theme/tokens.ts` mirrors the table for tests. `web/src/theme/tokens.test.ts` fails if `index.css` and the mirror drift apart. Change the palette in both files.
- Existing pages still use raw palette classes such as `bg-gray-900`. Migrate a screen to tokens when you change it. Do not mix both styles in a new component.
- `web/src/index.css` also defines `animate-toast-in`, used by toasts. Toast motion is wrapped in `motion-safe:`, and Skeleton uses `motion-reduce:animate-none`.
- Use the `scrollbar-thin` class on every scrollable element.

## z-index scale

One scale, in `:root` in `web/src/index.css`. Use the variable, never a number:

| Layer | Variable | Value | Tailwind class |
|-------|----------|-------|----------------|
| base | `--z-base` | 0 | |
| dropdown | `--z-dropdown` | 40 | `z-(--z-dropdown)` |
| modal | `--z-modal` | 50 | `z-(--z-modal)` |
| tooltip | `--z-tooltip` | 70 | `z-(--z-tooltip)` |
| toast | `--z-toast` | 80 | `z-(--z-toast)` |

Toasts sit above modals so an error is never hidden behind a dialog. Tooltips sit above modals. `Z_INDEX` in `web/src/theme/tokens.ts` mirrors the values for tests. `SecurityScoreModal` and `SetupGuide` still use a literal `z-[60]`. Move them to the scale when you touch them.

## Primitives

All primitives live in `web/src/components/ui/`. Props are documented in the source with TypeScript types and short comments. This section states each one's purpose and minimal usage.

### Button

Every clickable action that is not a link. Variants `primary`, `secondary` (default), `ghost`, `danger`. Sizes `sm`, `md` (default). The `type` defaults to `"button"`, so a Button inside a form never submits it by accident. Pass `type="submit"` to submit.

```tsx
<Button variant="primary" onClick={save}>Save</Button>
<Button variant="danger" size="sm" pending={isPending} pendingLabel="Deleting…" onClick={remove}>Delete</Button>
```

`pending` shows a spinner, swaps the label for `pendingLabel` (or keeps the children), disables the button, and sets `aria-busy`. A Button accepts a `ref`.

### Card

A bordered, raised panel. With `title` or `actions` it gets a ruled header. `titleAs` chooses `h2` (default) or `h3`. `padding="none"` removes body padding for tables.

```tsx
<Card title="Containers" actions={<Button size="sm">Refresh</Button>}>…</Card>
```

### Badge

A small status pill. Give it a tone and children, or give it an enum category and value and let the label map supply the text and tone.

```tsx
<Badge tone="warn">Update available</Badge>
<Badge category="severity" value={event.severity} />
```

Tones: `neutral`, `accent`, `success`, `warn`, `danger`, `info`.

### Modal

The only way to build a dialog. It renders into a portal on `document.body`, gives the panel `role="dialog"` and `aria-modal="true"` named by its title, and:

- traps Tab focus inside the panel;
- closes on Escape, but only the topmost modal reacts when dialogs are stacked;
- closes on a backdrop click only when the press and the release both land on the backdrop;
- returns focus to the element that opened it;
- ignores Escape, the backdrop, and the close button while `pending` is true.

Initial focus goes to `initialFocusRef` if given, otherwise the first control in the body, then the first control in the footer, then the close button, then the panel.

```tsx
<Modal open={open} onClose={() => setOpen(false)} title="Edit rule" size="lg" footer={<Button variant="primary" onClick={save}>Save</Button>}>
  …form fields…
</Modal>
```

Sizes `sm`, `md` (default), `lg`, `xl`. Do not set `z-index` on a modal. For an action that needs confirmation, use [ConfirmDialog](#confirmdialog) rather than a hand-built Modal.

### EmptyState

A centered "nothing here" panel with a title, optional description, icon, and one optional action. Use it only for a genuine empty result, never for a failed load. See [QueryState](#loading-and-error-states-querystate).

```tsx
<EmptyState title="No alert rules" description="Create a rule to be notified." action={{ label: 'Create rule', onClick: openCreate }} />
```

### Skeleton

A pulsing loading placeholder. It is hidden from assistive technology, so wrap it in a status region. QueryState does this for you. `lines={3}` renders stacked bars.

### focusRing

The shared focus-visible ring class string. Add it to any custom interactive element you build so keyboard focus looks the same everywhere.

## Toasts

`ToastProvider` is mounted once, in `web/src/main.tsx`. Call `useToast()` anywhere below it. The hook throws if the provider is missing.

```tsx
const toast = useToast()
toast.success('Agent update dispatched', '3 sent, 1 skipped')
toast.error('Reboot failed', errorMessage(err))
toast.info('Update check started')
```

Behavior:

- Success and info toasts dismiss after 5 seconds. Hovering over a toast or focusing inside it pauses the timer and resumes it with the time left.
- Error toasts never auto-dismiss. They use `role="alert"`. The other tones use a polite live region.
- At most four toasts show. Raising a fifth removes the oldest.
- Each toast has a Dismiss button.

Use a toast for the outcome of an action the user started. Use [QueryState](#loading-and-error-states-querystate) for a failed load. Do not show an error both inline and as a toast.

## ConfirmDialog

Use `ConfirmDialog` for every destructive or fleet-wide action. Never call `window.confirm()`, `alert()`, or `prompt()`. A static guard test fails if they appear.

```tsx
<ConfirmDialog
  open={confirming}
  title="Delete device?"
  message={`This removes ${hostname} and its history.`}
  confirmLabel="Delete"
  pendingLabel="Deleting…"
  variant="danger"
  onConfirm={() => api.deleteDevice(id)}
  onSuccess={() => navigate('/')}
  successToast="Device deleted"
  onClose={() => setConfirming(false)}
/>
```

### Pending and error contract

The dialog moves through these states:

| State | What the user sees | What the code does |
|-------|--------------------|--------------------|
| Idle | Cancel and Confirm | Waiting |
| Pending | Spinner and `pendingLabel` on Confirm. Both buttons disabled | Escape, backdrop clicks, and repeat activations are ignored |
| Success | Dialog closes | Raises `successToast` if set, then calls `onSuccess(result)`, then `onClose()` |
| Error | Dialog stays open with an inline `role="alert"` message from `errorMessage(error)` | Focus returns to Cancel. The user can retry or cancel |

Rules for callers:

- `onConfirm` returns a promise. **Reject (throw) to signal failure.** Do not catch the error and show your own toast: the dialog shows it inline.
- Do not close the dialog yourself from `onConfirm`. The dialog calls `onClose` after success.
- A synchronous `useRef` guard inside the dialog blocks a second activation in the same frame, before React re-renders the disabled button. A hand-built confirm step needs the same guard.
- `successToast` takes a string, a `{ title, message, tone }` object, or a function of the result.
- If the dialog unmounts while the action runs (the user navigated away), a failure raises one error toast, and a success raises the success toast but skips `onSuccess` and `onClose`. A confirmed delete does not navigate a user who already left the page.

### Never autofocus Confirm

Initial focus is always **Cancel**, for every variant including `primary`. The keypress or click that opened the dialog, including a held Enter with key auto-repeat, must never also confirm the action. Do not pass `initialFocusRef` pointing at a confirm button, and do not add `autoFocus` to one. `ConfirmDialog.test.tsx` fails if Confirm receives initial focus.

## Loading and error states: QueryState

`QueryState` renders one of three views for a TanStack Query result, so a failed load can never look like an empty list:

1. **Data** (including stale data after a failed background refetch): `children(data)`. If `isEmpty(data)` is true, renders `empty` instead.
2. **Error** with no data: an `ErrorPanel` with the error message and a Retry button that calls `refetch`.
3. **Loading**: `loading`, or a three-line Skeleton, inside a `role="status"` region.

```tsx
const query = useQuery({ queryKey: ['rules'], queryFn: api.getRules })

<QueryState
  query={query}
  isEmpty={rules => rules.length === 0}
  empty={<EmptyState title="No alert rules" />}
>
  {rules => <RuleTable rules={rules} />}
</QueryState>
```

Rules:

- `isEmpty` runs only when data exists. An error is never "empty".
- "Add your first device" and "Device not found" must not appear for a failed fetch. Show "Device not found" only for an HTTP 404 (`isNotFound(error)`).
- Pass `isPending` to fallback logic, not `isLoading`. A fetch paused because the browser is offline has `isLoading === false` but `isPending === true`. `components/DeviceQueryFallback.tsx` shows the example.
- Use `ErrorPanel` directly when you need the panel outside QueryState. Use `renderError` on QueryState for a custom error view, for example a 404 view.

## Time, durations, and bytes: utils/format.ts

`web/src/utils/format.ts` is the only module that formats bytes, durations, percentages, relative times, and timestamps. Never define a `formatBytes`, `formatUptime`, or `formatDuration` elsewhere, and never call `toLocaleString`, `toLocaleDateString`, or `toLocaleTimeString` on a date.

| Function | Output |
|----------|--------|
| `formatBytes(n)` | `1.5 MB`. Invalid input gives `0 B` |
| `formatDuration(seconds)` | `350ms`, `42s`, `10m`, `1h 5m`, `1d 3h`. Missing or negative gives `—` |
| `formatBytesPerSec`, `formatBytesPerSecAxis`, `formatPercent`, `formatCount` | Rates, chart ticks, percentages, integers |
| `formatRelative(value, nowMs)` | `just now`, `5m ago`, `in 3h`. The clock is passed in |
| `formatTimestamp(value, zoneOpts, precision)` | `2026-10-17 14:03:22 UTC`. 24-hour, always zone-labelled |
| `formatChartTick`, `formatChartTooltip` | `HH:mm` axis tick; full timestamp for tooltips |
| `formatUtc(iso)` | `YYYY-MM-DD HH:MM UTC`, always UTC. Used by the reboot ETA and maintenance windows |
| `parseInstant(value)` | `Date` or `null` for an ISO string, epoch milliseconds, or `Date` |

`precision` is `second` (default), `minute`, or `time` (time of day only). Missing or unparseable input renders `—`. The pure functions take the clock and zone as arguments, so tests are deterministic.

### In components

Do not call the pure functions with a hand-picked zone. Use the components and hooks, which read the user's UTC/local preference:

```tsx
<Timestamp value={event.created_at} />                 // 2026-10-17 14:03:22 UTC
<Timestamp value={rule.updated_at} precision="minute" />
<RelativeTime value={device.last_seen} />              // "5m ago", absolute time in the hover title
```

- `Timestamp` and `RelativeTime` render a `<time>` element, or the `fallback` (default `—`) for a missing value.
- `RelativeTime` refreshes every 30 seconds through `useMinuteTick`, which runs one shared timer for all instances.
- For strings built in code (a chart tooltip, a table export), use `useTimeFormatters()`, which returns `timestamp`, `chartTick`, and `chartTooltip` bound to the current preference.

### The preference

`TimePreferenceProvider` (mounted in `main.tsx`) holds the UTC/local choice. The header clock (`components/HeaderClock.tsx`) toggles it. The choice is stored in `localStorage` under `riot-time-zone`. The default is UTC. Only the exact string `Local` selects local time, so a tampered value is never used as a time zone identifier. If storage is unavailable the choice lasts for the session. Read it with `useTimePreference()`.

## Enum labels: labels.ts

Raw enum values (`os_update`, `critical`, `container_oom_killed`) must never reach the screen. `web/src/components/ui/labels.ts` holds the one label map for four categories: `severity`, `commandStatus`, `commandAction`, and `eventType`.

```tsx
enumLabel('commandAction', 'os_update')   // "OS update"
enumLabel('severity', null)               // "—"
enumLabel('eventType', 'new_thing')       // "New thing" (unknown values are humanized)
```

To add an enum value:

1. Add it to the array (`SEVERITIES`, `COMMAND_STATUSES`, `COMMAND_ACTIONS`, or `EVENT_TYPES`) and to the matching `Record` in `LABELS`. A value present in the array but missing from the record fails `tsc`.
2. Keep the list in step with the Go constants it mirrors (`internal/models/events.go`, `internal/models/probes.go`, `internal/agent/commands.go`). `labels.test.ts` checks that every value has a label and pins the count of event types, so add the new value there too.
3. If the value needs a non-neutral badge color, add it to `toneFor`.

Unknown values from a newer server still render, humanized, instead of raw.

## Chart colors

Recharts needs literal color strings, so charts import `chartColors` from `web/src/theme/chartColors.ts` instead of writing hex values. A palette change touches that file and the aliases in `index.css`. The terminal's xterm theme is the only exempt hex user.

## Typed API errors

`web/src/api/errors.ts` exports `ApiError` (carries the HTTP `status`), `isNotFound(error)`, `errorMessage(error)` (safe text for any thrown value, never throws), and `retryUnlessNotFound` (a query `retry` function that never retries a 404). Import them from this module, not from `api/client`: the demo build swaps `api/client` for a stand-in, and tests mock it with partial factories.

## Rules enforced by tests

`web/src/test/static-guards.test.ts` reads every source file as text and fails when a convention is broken. Each guard has a positive control that proves its pattern still matches.

| Guard | Fails when |
|-------|------------|
| AC-004 | A hex color literal appears in the chart components and the other guarded files. Use `chartColors` or a token |
| AC-005 | A file in `components/ui` uses a raw `gray-N` class, a hex color, or a numeric `z-` utility |
| AC-031 | A migrated modal or the tooltip uses an ad-hoc `z-40`, `z-50`, `z-[60]`, or `z-[100]` |
| AC-063 | Non-test code calls `confirm()`, `alert()`, `prompt()`, or their `window.` forms |
| AC-064 | A test stubs `window.confirm`, `alert`, or `prompt` |
| AC-108 | `formatBytes`, `formatUptime`, or `formatDuration` is defined outside `utils/format.ts` |
| AC-110 | Code calls `toLocaleString`, `toLocaleDateString`, or `toLocaleTimeString` |
| AC-120, AC-121 | A removed component (`Sparkline`, `MiniScore`, `ContainerGroup`, `ContainerCard`) or the container drawer default export returns |
| SEC-005 | Code uses `dangerouslySetInnerHTML`, `innerHTML =`, or `insertAdjacentHTML`. Render API text as React text only |
| SEC-008 | Anything outside the demo modules imports `demo-client`, `demo-settings`, `demo-data`, or `DemoWebSocketProvider` |

Other tests worth knowing: `theme/tokens.test.ts` (palette, contrast, `index.css` in step with the mirror) and `api/demo-parity.test.ts` plus the type checker (the demo clients implement every method of the real clients).

Run the checks from `web/`:

```bash
npx tsc --noEmit
npm run test:run
npx vitest run src/test/static-guards.test.ts
```

## Testing a screen

`web/src/test/renderWithProviders.tsx` wraps a component in a QueryClient (no retries), a router, the time preference, and the toast provider. Use it instead of a bare `render`.

```tsx
const { queryClient } = renderWithProviders(<MyPage />, {
  route: '/devices/abc',
  path: '/devices/:id',
  timeZone: { localTimeZone: 'America/Los_Angeles' },
})
```

Options: `route`, `path`, `routes`, `queryClient`, `timeZone` (`localTimeZone`, `storage`, `now`), `toast` (`now`), `showLocation`. Inject `localTimeZone` to test local time. Do not rely on the `TZ` environment variable, which does not work on Windows. Do not stub native dialogs in tests.
