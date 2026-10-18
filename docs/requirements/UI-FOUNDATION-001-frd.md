# Formal Requirements Document

**Story ID:** UI-FOUNDATION-001
**Title:** Dashboard design-system foundation — tokens, primitives, toasts, honest error states, unified formatting, demo-mode repair
**Author:** Business Developer Agent
**Date:** 2026-10-17
**Status:** FINAL (clarifications resolved by Clod on Ryan's behalf — see Section 2a; pending Ryan's review)

---

## 1. Executive Summary

The rIOt dashboard performs well but its flow and visuals do not feel
enterprise-grade: colors and spacing are repeated inline, the app uses native
browser `confirm()` / `alert()` dialogs, action results appear as easily missed
timed banners (some failures are silent), a failed fetch is shown as "Add your
first device" or "Device not found", timestamps are a mix of unlabeled local
time and UTC, hostnames are forced to uppercase, and the demo build cannot
render `/dashboard`. This story builds the foundation that fixes these problems
once. It adds a design-token set, a small library of accessible UI primitives
(Button, Card, Badge, Modal, ConfirmDialog, EmptyState, Skeleton, QueryState),
an in-house toast system, a single formatting module with a UTC/Local
preference, and a demo provider with the same surface as the real one. It then
migrates the worst offenders onto that foundation. Visually it consolidates the
existing palette and does not re-skin the app. It adds no new dependencies and
changes no backend code.

## 2. Background & Context

rIOt is a self-hosted fleet-monitoring platform with a Go server and an embedded
React 19 / TypeScript dashboard (Tailwind v4, TanStack Query 5, recharts 3,
vitest + Testing Library). Performance work is complete. Ryan, the product
owner, has stated that the flow and visuals now need to feel enterprise-grade,
and that shared work must become reusable primitives instead of more inline
Tailwind. This story covers the foundation and the migration of the worst
offenders. The device-page split and the app-shell redesign are separate, later
stories.

Current state, verified against the codebase on 2026-10-17. Line numbers refer
to the `main`-derived worktree:

- **No design tokens.** `web/src/index.css` contains only the Tailwind import
  and scrollbar utilities. The app uses 36 distinct `bg|text|border-gray-N`
  classes, and 8 files contain hardcoded hex colors.
- **Native dialogs.** The app has 8 `confirm()` calls and 1 `alert()` (listed in
  FR-060..FR-068).
- **Timed banners and silent failures.** FleetOverview shows the results of bulk
  agent-update and patch-all in 8-second inline banners below the table. Bulk
  agent-update has no error handler, and patch-all swallows its errors.
  DeviceDetail shows command results in a 5-second inline banner that displays
  raw tokens such as `Command sent: os_update (pending)`.
- **Pending-state bug.** `ConfirmModal` is stateless. The FleetOverview caller
  closes it right after calling `mutate()`, so the "Updating..." label never
  appears.
- **Modals.** The app has 14 hand-rolled overlays with mixed z-indexes
  (`z-40`/`z-50`/`z-[60]`). None has `role="dialog"`, a focus trap, or Escape
  handling.
- **Misleading error states.** FleetOverview shows `<SetupGuide inline />` when
  the device fetch fails. DeviceDetail, DeviceContainers and ContainerDetailPage
  show "Device not found" for any failure. `fetchJSON` throws a plain `Error`
  without the HTTP status.
- **Formatters.** `formatBytes` is defined 3 times, and duration/uptime logic is
  defined at least 5 times. The UI has 19 unlabeled local `toLocale*` timestamp
  calls. The header clock is deliberately UTC (commit 64a3e3c).
- **Uppercase hostnames.** At least 8 places force hostnames to uppercase.
- **Dead code.** Sparkline, MiniScore, ContainerGroup, ContainerCard and the
  ContainerDetail drawer default export have no importers.
- **Demo mode is broken.** The demo WebSocket reports `connected:false`. The
  demo API and settings stubs lack about 20 methods, so the `/dashboard` charts
  stay in skeleton state forever, the leaderboard is empty, and the console
  repeatedly logs "No queryFn" errors.

Baseline: `npx tsc --noEmit` is clean, and `npm run test:run` passes 27 files /
**405 tests**. Before screenshots are in
`docs/implementation/UI-FOUNDATION-001-screenshots/before-*.png`.

## 2a. Resolved Clarifications (product decisions)

Ryan was not available for a live interview. Clod resolved every Stage 1
clarifying question on Ryan's behalf. These decisions are binding for this
story and are **not** open questions.

| ID | Decision |
|----|----------|
| D1 | **Timestamps.** There is one app-wide preference, `UTC` or `Local`, and the **default is UTC**. It governs the header clock and every absolute timestamp. Ryan toggles it by clicking the header clock, which is an accessible button. The preference is persisted in localStorage, and any read failure falls back to the default. Every absolute timestamp carries a zone label ("UTC" or the local zone abbreviation). Relative times use `<RelativeTime>`; hovering it, or reading its title, shows the absolute time in the preferred zone. |
| D2 | **Toasts.** Toasts are built in-house with no new dependency. They stack at the bottom right with the newest on top, and at most 4 are visible. Success and info toasts auto-dismiss after 5 s. Error toasts stay until dismissed. Hovering pauses auto-dismiss. Each toast has a close button. Toasts are announced via aria-live: polite for success and info, assertive (`role="alert"`) for error. |
| D3 | **Dark theme only.** The tokens reproduce the existing palette: gray-950 shell, gray-900 cards, gray-800 borders, a blue accent, and green/amber/red/sky status colors. This is a consolidation, not a re-skin. |
| D4 | **ConfirmDialog pending state.** While the async action runs, the confirm button shows a spinner and a pending label, both buttons are disabled, and Escape and backdrop clicks are ignored. On success the dialog closes and a success toast appears. On failure the dialog stays open with an inline error so the user can retry or cancel. No native alert is used. |
| D5 | **Replacements.** All 8 `confirm()` calls become ConfirmDialog, using the danger variant for destructive actions. The device-delete `alert()` is removed (see BR-002 for how the failure is shown). The FleetOverview bulk agent-update and patch-all results, and their silent failures, become toasts. A success toast shows the sent/queued/skipped counts and an error toast shows the message. DeviceDetail command and fetch-logs results become toasts instead of 5 s inline banners. |
| D6 | **Error states.** A failed device-list fetch shows an error panel with Retry and never the SetupGuide. The SetupGuide appears only when the fetch succeeded and returned zero devices. On the device, containers and container-detail pages, HTTP 404 shows "Device not found" with a link back to Fleet. Any other error shows "Couldn't load device", the error message, and a Retry button. |
| D7 | **Hostnames** render exactly as reported. Section headers may stay uppercase. |
| D8 | **Enum labels.** One shared enum-to-label map is used everywhere. The UI never shows a raw snake_case or raw lowercase enum value. Unknown values fall back to a humanized form (underscores become spaces, sentence case). |
| D9 | **Demo mode.** The demo WebSocket reports connected. The demo API and settings stubs implement every method that the real clients expose, so `/dashboard` renders device charts, the leaderboard and activity with no "No queryFn" console errors. |
| D10 | **Cheap polish in scope.** The Security page stat cards fill a single row at desktop width. The DeviceDetail security score gauge has a visible "Security score" caption and an accessible name. The DeviceDetail Delete action is a danger-styled button, visually separated from the Docker/Probes/Terminal links. Misaligned device-page table columns are out of scope. |
| D11 | **Dead code.** Remove Sparkline, MiniScore, ContainerGroup, ContainerCard and the ContainerDetail drawer default export, keeping `ContainerDetailContent`. Remove each one only after verifying it has no importers. Duplicate formatters collapse into `web/src/utils/format.ts`. |
| D12 | **Out of scope:** the device-page split, app-shell/nav redesign, a light theme, a full-repo sweep of gray classes, chart library changes, and any backend/Go change. |
| D13 | **Modal primitive.** It has `role="dialog"` and `aria-modal="true"`, is labelled by its title, traps focus, sets initial focus inside the dialog, closes on Escape (unless pending), restores focus to the opener, and uses a single z-index scale from the tokens. Touched modals migrate to it. At minimum this covers ConfirmModal, UpdateConfirmModal and every modal used by the replaced flows. |
| D14 | **Keyboard.** Every interactive primitive shows a visible `:focus-visible` ring. |

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Fleet operator (Ryan / dashboard user) | An authenticated dashboard user. Today there is a single role. | Can view all pages and perform the existing actions: delete, update, patch, reboot, purge logs, and so on. This story adds no new permission and removes none. |
| Demo visitor | An anonymous user of the static demo build (`VITE_DEMO=true`, GitHub Pages `/rIOt/`). | Can view everything. All actions are simulated and change no real system. |
| rIOt server | The existing REST API and WebSocket. | Unchanged by this story (no backend change). |
| Browser storage | localStorage, which holds the time preference. | Can read and write one preference value. It may be unavailable (private mode, quota, policy). |

## 4. Functional Requirements

Every requirement uses "must" or "must not" and can be verified on its own.
Paths are relative to `web/src/`.

### 4.1 Design tokens

- **FR-001:** `index.css` must define, in a Tailwind v4 `@theme` block, color
  tokens named `surface`, `raised`, `border`, `text`, `text-muted`, `accent`,
  `success`, `warn`, `danger` and `info`, so that they work as utilities (for
  example `bg-surface` and `text-text-muted`).
- **FR-002:** Each color token must reproduce the existing palette value listed
  in Section 7.1. Applying the tokens must not visibly change the hue of any
  surface, border, text or status color.
- **FR-003:** The token set must define one ordered z-index (layering) scale with
  at least these layers, from lowest to highest: dropdown, modal, tooltip,
  toast. Every primitive in this story must take its z-index from that scale.
- **FR-004:** Chart colors must come from one shared chart-color definition.
  These files must contain no hex color literals: `MetricChart.tsx`,
  `ContainerMetricChart.tsx`, `dashboard/PerDeviceCharts.tsx`,
  `ContainerDetail.tsx`, `pages/Alerts.tsx` and `pages/DeviceDetail.tsx`. The
  xterm theme in `Terminal.tsx` is exempt.
- **FR-005:** Primitives under `components/ui/` must not use raw
  `gray-<N>` palette classes or hex color literals. They must use the tokens
  from FR-001.

### 4.2 Button

- **FR-006:** `components/ui/` must provide a Button with the variants
  `primary`, `secondary`, `ghost` and `danger`, and the sizes `sm` and `md`.
  Each variant and size combination must be visually distinct.
- **FR-007:** Button must render as `type="button"` unless the caller sets
  another type.
- **FR-008:** A disabled Button must not invoke its click handler, and it must
  look disabled.
- **FR-009:** Button must support a pending state. In that state it shows a
  spinner and the caller's pending label, is disabled, and sets
  `aria-busy="true"`.
- **FR-010:** Every interactive primitive must show a visible focus ring on
  `:focus-visible` (D14). This covers Button, the Modal close control, the toast
  close button, the QueryState Retry button and the header clock toggle.

### 4.3 Card, Badge and enum labels

- **FR-011:** `components/ui/` must provide a Card that renders on the `raised`
  surface with the `border` token. When the caller provides a title, Card must
  render it as a heading.
- **FR-012:** One shared enum-to-label map must cover the following categories:
  alert/event severity, command status, event type, and update kind (for
  example `os_update` and `agent_update`).
- **FR-013:** The map must hold an explicit label for every value of each
  category's client-side type union. Labels must use sentence case and keep
  correct acronyms, for example `os_update` → "OS update",
  `agent_update` → "Agent update", `warning` → "Warning" and
  `queued` → "Queued".
- **FR-014:** A value that is not in the map must render in humanized form:
  underscores become spaces, the first letter is capitalized and the rest is
  lowercase. For example, `some_new_value` → "Some new value".
- **FR-015:** A null, undefined or empty enum value must render as "—".
- **FR-016:** `components/ui/` must provide a Badge with the tones `neutral`,
  `accent`, `success`, `warn`, `danger` and `info`. When a Badge is given an
  enum value, it must take its text from the shared map.
- **FR-017:** No user-visible text in the UI may contain a raw severity, command
  status, event type or update kind value. Any place that displays one of these
  values must use the shared map.

### 4.4 Modal

- **FR-018:** `components/ui/` must provide a Modal with `role="dialog"` and
  `aria-modal="true"`, labelled through `aria-labelledby` by its title element.
- **FR-019:** When a Modal opens, keyboard focus must move to an element inside
  it.
- **FR-020:** Focus must stay trapped inside an open Modal. Tab from the last
  focusable element wraps to the first, and Shift+Tab from the first wraps to
  the last.
- **FR-021:** Escape must close the Modal, unless the Modal is in a pending
  state.
- **FR-022:** When the Modal closes, focus must return to the element that had
  focus before it opened, as long as that element is still in the document.
- **FR-023:** A click on the backdrop must close the Modal, unless it is pending
  or the caller has turned backdrop dismissal off. A click inside the dialog
  panel must not close it.
- **FR-024:** When Modals are stacked, Escape must close only the topmost one.
- **FR-025:** These overlays must be migrated to the Modal primitive:
  `ConfirmModal.tsx`, `UpdateConfirmModal.tsx`, FleetOverview's
  `PatchReviewModal`, and any other hand-rolled modal whose file is edited in
  this story. A migrated modal must not contain its own z-index utility
  (`z-40`, `z-50`, `z-[60]`).

### 4.5 ConfirmDialog

- **FR-026:** `components/ui/` must provide a ConfirmDialog built on Modal. It
  takes a title, a message, a confirm label, an optional pending label, a
  variant (`primary` or `danger`), a confirm action that may be asynchronous, an
  optional success-toast message, and a cancel handler.
- **FR-027:** While the confirm action is unsettled, the confirm button must show
  the pending state from FR-009. The default pending label is "Working…", and
  the caller can override it, for example with "Deleting…".
- **FR-028:** While pending, both the confirm and cancel buttons must be
  disabled, and Escape and backdrop clicks must not close the dialog.
- **FR-029:** While pending, further activations of the confirm button must not
  invoke the action again.
- **FR-030:** When the action resolves, the dialog must close. If the caller
  supplied a success message, the system must show it as a success toast.
- **FR-031:** When the action rejects or throws, the dialog must stay open, leave
  the pending state, and show an inline error with `role="alert"`. The error
  contains the error's message, or "Something went wrong. Please try again." if
  the rejection carries no message.
- **FR-032:** After a failure, activating confirm must clear the inline error and
  invoke the action again. Activating cancel must close the dialog without
  invoking the action.
- **FR-033:** The `danger` variant must render its confirm button as a danger
  Button. When a danger ConfirmDialog opens, initial focus must go to the Cancel
  button.
- **FR-034:** The FleetOverview bulk agent-update confirmation must show its
  pending label ("Updating…") for the whole time the update requests are in
  flight.

### 4.6 EmptyState, Skeleton and QueryState

- **FR-035:** `components/ui/` must provide an EmptyState that renders a title,
  an optional description and an optional action Button.
- **FR-036:** `components/ui/` must provide a Skeleton placeholder that is hidden
  from assistive technology (`aria-hidden="true"`).
- **FR-037:** `components/ui/` must provide a `<QueryState>` wrapper. It takes a
  query result (loading flag, error, data, refetch), an emptiness predicate, an
  optional loading placeholder, and empty-state content.
- **FR-038:** When QueryState is loading and has no data, it must render the
  loading placeholder (Skeleton by default) inside a container marked
  `aria-busy="true"` with an accessible "Loading" status.
- **FR-039:** When QueryState has an error and no data, it must render an error
  panel with the error message and a Retry button. Retry must call the query's
  refetch.
- **FR-040:** When the query succeeded and the emptiness predicate returns true,
  QueryState must render the empty-state content.
- **FR-041:** When the query succeeded with non-empty data, QueryState must
  render its children with that data.
- **FR-042:** When a background refetch fails while previously loaded data
  exists, QueryState must keep rendering the children with the existing data and
  must not replace them with the error panel.
- **FR-043:** The error state must take precedence over the empty state. An
  errored query with no data must never render empty-state content.

### 4.7 Toast system

- **FR-044:** The app must provide a toast system that any page or component can
  call. It must be mounted once at the application root in both the regular and
  demo builds.
- **FR-045:** A toast must have one tone (`success`, `info` or `error`), a title,
  and an optional message.
- **FR-046:** Toasts must render in a fixed stack at the bottom right of the
  viewport, with the newest toast on top. The stack must be a region with the
  accessible name "Notifications".
- **FR-047:** No more than 4 toasts may be visible at once. When a fifth toast
  arrives, the oldest visible toast must be dismissed.
- **FR-048:** Success and info toasts must auto-dismiss 5 seconds after they
  appear.
- **FR-049:** Error toasts must not auto-dismiss. They stay until the user
  dismisses them or they are evicted under FR-047.
- **FR-050:** While the pointer is over a toast, or keyboard focus is inside it,
  its auto-dismiss timer must pause. When both end, the timer must resume with
  the remaining time.
- **FR-051:** Every toast must have a close button with the accessible name
  "Dismiss notification", and activating it must remove the toast.
- **FR-052:** Success and info toasts must be announced through a polite live
  region. Error toasts must be announced assertively with `role="alert"`.
- **FR-053:** Toasts must persist across client-side route changes. A toast
  raised just before navigation must still be visible after it.

### 4.8 Native dialog replacement

- **FR-054:** Non-test source under `web/src` must not call `window.confirm`,
  `window.alert`, `window.prompt`, or the bare globals `confirm(`, `alert(` or
  `prompt(`.
- **FR-055:** Existing tests that stub `window.confirm` or `window.alert` must be
  migrated to interact with the ConfirmDialog or toast instead. They must not be
  deleted.
- **FR-056:** Deleting a probe in `pages/DeviceProbes.tsx` must use a danger
  ConfirmDialog whose message names the probe.
- **FR-057:** Deleting a probe in `pages/Probes.tsx` must use a danger
  ConfirmDialog.
- **FR-058:** Deleting a device probe in `pages/Probes.tsx` must use a danger
  ConfirmDialog.
- **FR-059:** "Update all" in `pages/settings/AgentManagement.tsx` must use a
  primary ConfirmDialog whose message states the device count and the version.
  On success, a toast must show the sent and skipped counts.
- **FR-060:** Both rule-delete actions in `pages/settings/AlertRuleSettings.tsx`
  (today at lines 202 and 223) must use a danger ConfirmDialog.
- **FR-061:** "Delete all server logs" in `pages/settings/LogSettings.tsx` must
  use a danger ConfirmDialog whose message states that the action cannot be
  undone.
- **FR-062:** Deleting a channel in `pages/settings/NotificationSettings.tsx`
  must use a danger ConfirmDialog.
- **FR-063:** In every flow from FR-056..FR-062, cancelling must not call the
  underlying API.
- **FR-064:** In every flow from FR-056..FR-062, a successful action must show a
  success toast that names what happened, for example "Probe deleted" or
  "Channel deleted".
- **FR-065:** Deleting a device from DeviceDetail must use a danger
  ConfirmDialog that stays pending until the delete request settles. A failure
  must be shown as the dialog's inline error (FR-031), not as a native alert.
- **FR-066:** A successful device delete must navigate to the Fleet page and show
  a success toast that names the deleted hostname.

### 4.9 Inline banner replacement (FleetOverview and DeviceDetail)

- **FR-067:** FleetOverview must not render the inline patch-result or
  agent-update-result banners, and must not use the 8-second banner timers.
- **FR-068:** A successful bulk agent update must close the confirmation and show
  a success toast with the sent count and, when it is not zero, the skipped
  count.
- **FR-069:** A failed bulk agent update must not be silent. The ConfirmDialog
  stays open and shows the error inline (FR-031, BR-002).
- **FR-070:** The PatchReviewModal "patch all" confirm control must show a
  pending state while the dispatch is in flight.
- **FR-071:** A successful patch-all must close the PatchReviewModal and show a
  success toast with the sent, queued, skipped and total counts.
- **FR-072:** A failed patch-all must not be silent. The system must show an
  error toast with the error message, and the PatchReviewModal must stay open.
- **FR-073:** DeviceDetail must not render the inline command-result or
  fetch-logs result banners.
- **FR-074:** The reboot, shutdown, OS patch and agent update commands in
  DeviceDetail must use ConfirmDialog. Reboot and shutdown use the danger
  variant; patch and agent update use the primary variant. Each dialog stays
  pending until the command request settles.
- **FR-075:** A successful DeviceDetail command must show a success toast with
  the humanized command label, the humanized resulting status and the device
  hostname, for example "OS update command sent to web-01 — Pending". The toast
  must not contain raw enum tokens.
- **FR-076:** A failed command launched from a ConfirmDialog must be shown as
  that dialog's inline error (FR-031).
- **FR-077:** A command launched outside a ConfirmDialog, such as from
  SecurityScoreModal, must show a success toast on success and an error toast
  with the message on failure.
- **FR-078:** A fetch-logs request must show a success toast when it is accepted
  and an error toast with the message when it fails.

### 4.10 Honest error states

- **FR-079:** The API client must raise a typed error that carries the numeric
  HTTP status for non-2xx responses. The error message must keep its current
  format, `HTTP <status>: <statusText>`.
- **FR-080:** The API client's handling of 401 responses must not change.
- **FR-081:** When the device-list fetch has failed and no device data exists,
  FleetOverview must render an error panel titled "Couldn't load devices" with
  the error message and a Retry button. It must not render the SetupGuide.
- **FR-082:** FleetOverview must render the inline SetupGuide only when the
  device-list fetch succeeded, returned zero devices, and no search filter is
  active.
- **FR-083:** While the first device-list fetch is loading, FleetOverview must
  render a loading placeholder and must not render the SetupGuide.
- **FR-084:** When the device fetch fails with HTTP 404, DeviceDetail,
  DeviceContainers and ContainerDetailPage must each render "Device not found"
  and a link back to the Fleet page.
- **FR-085:** When the device fetch fails with any non-404 error, including
  network errors that have no HTTP status, those three pages must each render
  "Couldn't load device", the error message, and a Retry button that refetches.
- **FR-086:** When ContainerDetailPage loads its device but the requested
  container is not part of it, the page must render "Container not found" and a
  link back to that device's containers page.
- **FR-087:** While the first device fetch is loading, DeviceDetail,
  DeviceContainers and ContainerDetailPage must render a Skeleton placeholder
  instead of the text "Loading...".

### 4.11 Formatting module and time display

- **FR-088:** `utils/format.ts` must be the only module that defines formatters
  for bytes, byte rates, durations, percentages, counts, relative times and
  absolute timestamps. It must keep the existing exports `formatBytesPerSec`,
  `formatBytesPerSecAxis`, `formatPercent` and `formatCount` with unchanged
  output.
- **FR-089:** `formatBytes` must produce exactly the output of today's
  `utils/docker.ts` `formatBytes`: base 1024; `N B` below 1 KB; one decimal with
  KB, MB, GB or TB above that. Non-finite or negative input must render as
  "0 B".
- **FR-090:** The duration formatter must take seconds, which may be fractional,
  and render as follows:

  | Input range | Output |
  |-------------|--------|
  | below 1 s | `Nms` (rounded) |
  | below 60 s | `Ns` |
  | below 1 h | `Nm` |
  | below 1 d | `Hh Mm` |
  | otherwise | `Dd Hh` |

  Negative or non-finite input must render as "—". All duration and uptime
  displays, including container uptime, must use it.
- **FR-091:** The relative-time formatter must render a past instant as follows:

  | Time ago | Output |
  |----------|--------|
  | under 60 s | "just now" |
  | under 60 min | `Nm ago` |
  | under 24 h | `Nh ago` |
  | otherwise | `Nd ago` |

  A future instant must render as `in Nm`, `in Nh` or `in Nd` using the same
  thresholds.
- **FR-092:** The absolute-timestamp formatter must render
  `YYYY-MM-DD HH:mm:ss <ZONE>` in 24-hour time, using the preferred zone. It
  must also provide a minute-precision form, `YYYY-MM-DD HH:mm <ZONE>`, and a
  time-only form, `HH:mm:ss <ZONE>`. `<ZONE>` is "UTC" for the UTC preference,
  or the browser's short time-zone name (for example "PDT") for the Local
  preference.
- **FR-093:** An unparseable or missing timestamp must render as "—" and must
  never render as "Invalid Date".
- **FR-094:** `utils/rebootEta.ts` `formatUtc` must keep its exact current output
  (`YYYY-MM-DD HH:MM UTC`) and must stay UTC whatever the preference (BR-007).
  It must be implemented through, or re-exported from, the canonical module.
- **FR-095:** Each of the 19 timestamp call sites listed in Section 7.4 must
  render through the canonical absolute-timestamp formatter, or through
  `<RelativeTime>` where the site shows elapsed time.
- **FR-096:** These duplicate local formatter definitions must be removed, and
  their call sites must use `utils/format.ts`:
  - `formatBytes` in `MetricChart.tsx` (replaced by `formatBytesPerSec`) and in
    `pages/DeviceDetail.tsx`
  - `formatUptime` in `pages/DeviceDetail.tsx`
  - `formatDuration` in `components/ActivityLog.tsx` and in
    `pages/settings/AgentManagement.tsx`
  - the seconds-to-d/h/m helper in `pages/settings/AlertRuleSettings.tsx`
  - the inline "Xm ago" logic in `pages/FleetOverview.tsx`

  `utils/docker.ts` may re-export `formatBytes` from `utils/format.ts` but must
  not define its own.
- **FR-097:** The app must provide a `<RelativeTime>` component. It renders a
  `<time>` element whose `dateTime` attribute is the ISO-8601 instant, whose
  text is the relative time (FR-091), and whose `title` is the absolute
  timestamp in the preferred zone (FR-092).
- **FR-098:** `<RelativeTime>` must refresh its relative text at least once every
  60 seconds while it is mounted.
- **FR-099:** The app must hold one time-zone preference with the values `UTC`
  and `Local`. The default is `UTC`.
- **FR-100:** The header clock must display the current time with the time-only
  form from FR-092 in the preferred zone, updated every second.
- **FR-101:** The header clock must be a button. Its accessible name must state
  the current zone and the action, for example "Time zone: UTC. Switch to local
  time". Activating it must toggle the preference.
- **FR-102:** The preference must be stored in localStorage and restored on the
  next app load.
- **FR-103:** If localStorage is unavailable, throws, or holds an unrecognized
  value, the preference must fall back to `UTC` without an error being shown or
  thrown. If writing fails, the toggled value must still apply for the current
  session.
- **FR-104:** Changing the preference must update every rendered absolute
  timestamp and every `<RelativeTime>` title right away, without a page reload.
- **FR-105:** Time-axis ticks on charts must render in the preferred zone. Each
  chart must show the zone label at least once, either in its hover tooltip or
  in an axis caption. Every chart hover-tooltip timestamp must carry the zone
  label.

### 4.12 Dead code

- **FR-106:** `components/Sparkline.tsx`, `components/MiniScore.tsx`,
  `components/ContainerGroup.tsx` and `components/ContainerCard.tsx` must be
  deleted. Before each deletion, a check must confirm that the file has no
  importers other than files deleted in this story.
- **FR-107:** The default-export drawer `ContainerDetail` in
  `components/ContainerDetail.tsx` must be removed. The named export
  `ContainerDetailContent` and the helpers it uses must stay, and
  `pages/ContainerDetailPage.tsx` must keep working.

### 4.13 Hostnames

- **FR-108:** Hostnames must render exactly as reported, with no forced change of
  case, in at least these places: `DeviceDetail.tsx` (h1), `Alerts.tsx`,
  `FleetOverview.tsx`, `Security.tsx`, `DeviceContainers.tsx` (both places),
  `DeviceProbes.tsx` and `ContainerDetailPage.tsx`.
- **FR-109:** Section-header labels that are not hostnames, for example
  "Per-Device Security", may keep their uppercase styling and must not be
  changed by this story.

### 4.14 Demo mode

- **FR-110:** In the demo build, the WebSocket provider must report the
  connection as connected. The Dashboard must not show the "Live updates paused"
  notice.
- **FR-111:** The demo API client must implement every method exposed by the real
  API client. A missing method must cause a type-check failure.
- **FR-112:** The demo settings client must implement every method exposed by the
  real settings client. A missing method must cause a type-check failure.
- **FR-113:** Demo `getFleetHeartbeats` must return a non-empty, time-ordered
  heartbeat series for each demo device, derived from the existing per-device
  history generators.
- **FR-114:** Demo `getFleetContainers` must return container data consistent
  with the demo devices, so the Dashboard leaderboard is populated.
- **FR-115:** Demo `getFeatureToggles` must return a feature-toggle set with all
  features enabled.
- **FR-116:** Demo mutating methods must resolve successfully with plausible
  payloads (for example counts for bulk operations) and must not touch any
  network endpoint.
- **FR-117:** The demo API client must reject with a 404 typed error (FR-079)
  when asked for a device ID that does not exist in the demo data.
- **FR-118:** With the demo build open on `/dashboard`, the device charts,
  leaderboard and activity sections must render populated content. They must not
  stay in a skeleton state.

### 4.15 Demo-walkthrough polish

- **FR-119:** At viewport widths of 1280 px and above, the Security page stat
  cards must sit in a single row with no card wrapping alone onto a second row.
- **FR-120:** The DeviceDetail security score gauge must show the visible caption
  "Security score". Its accessible name must include the score and the grade,
  for example "Security score 72, grade C".
- **FR-121:** The DeviceDetail Delete action must be a danger-variant Button.
- **FR-122:** The DeviceDetail Docker, Probes and Terminal links must be grouped
  in a navigation landmark labelled "Device sections". The Delete button must
  sit outside that landmark and be visually separated from it.

## 5. Non-Functional Requirements

- **NFR-001 [Dependencies]:** The `dependencies` and `devDependencies` in
  `web/package.json` must be identical to `main`. No new npm package may be
  added.
- **NFR-002 [Type safety]:** `npx tsc --noEmit` must report zero errors.
- **NFR-003 [Test suite]:** `npm run test:run` must pass with zero failures, and
  the total test count must be at least 405. A test may be removed only when its
  subject is deleted under FR-106/FR-107. Every new primitive must have a
  co-located test file.
- **NFR-004 [Accessibility]:** Text set in the `text` and `text-muted` tokens
  must have a contrast ratio of at least 4.5:1 on both the `surface` and
  `raised` tokens (WCAG 2.1 AA).
- **NFR-005 [Accessibility]:** Every primitive must be operable with the keyboard
  alone. Dialogs and toasts must meet FR-018..FR-024 and FR-051..FR-052.
- **NFR-006 [Accessibility]:** When `prefers-reduced-motion: reduce` is set, the
  Skeleton pulse and the toast enter/exit motion must be turned off. The spinner
  may keep rotating.
- **NFR-007 [Performance]:** The gzip size of the production main entry chunk
  must not grow by more than 20 KB compared with `main`.
- **NFR-008 [Performance]:** The header clock's one-second tick must not cause
  re-renders outside the clock itself. Only a change of preference may re-render
  timestamp consumers.
- **NFR-009 [Compatibility]:** Both the regular build and the demo build
  (`VITE_DEMO=true`) must compile and start.
- **NFR-010 [Scope]:** No file outside `web/` and `docs/` may change. No Go,
  server or agent code may change.

## 6. Business Rules

- **BR-001:** An action that deletes, purges, uninstalls, reboots or shuts down
  must be confirmed with the `danger` variant. Non-destructive confirmations
  (updates and patches) must use `primary`.
- **BR-002:** Each action failure is reported to the user exactly once. If the
  action was started from a ConfirmDialog, the failure appears as that dialog's
  inline error. Otherwise it appears as an error toast. *(This reconciles D4 and
  D5. See A-001.)*
- **BR-003:** Error feedback never auto-dismisses.
- **BR-004:** The SetupGuide ("add your first device") is shown only when the
  fleet has been positively confirmed empty. A fetch failure is never treated as
  an empty fleet.
- **BR-005:** No absolute timestamp is shown without a zone label.
- **BR-006:** The time-zone preference defaults to UTC.
- **BR-007:** Maintenance-window times, including the REBOOT-ETA-001 tooltip, are
  always shown in UTC whatever the preference, because they are configured and
  stored in UTC (REBOOT-ETA-001 BR-003). Because they carry the "UTC" label,
  they are never ambiguous.
- **BR-008:** Hostnames are identifiers and are shown exactly as reported.
- **BR-009:** Raw enum values are never shown to the user.
- **BR-010:** Native browser dialogs are not used anywhere in the dashboard.

## 7. Data Requirements

### 7.1 Design tokens (default values)

| Token | Tailwind palette reference (current usage) |
|-------|---------------------------------------------|
| surface | gray-950 (app shell) |
| raised | gray-900 (cards, panels, dialogs) |
| border | gray-800 |
| text | gray-100 |
| text-muted | gray-400 |
| accent | blue-600 (with blue-500 hover) |
| success | green-500 |
| warn | amber-500 |
| danger | red-600 (with red-500 hover) |
| info | sky-500 |

The architect may adjust a shade by one step to match the dominant existing
usage, but must meet FR-002 and NFR-004.

Layering order (FR-003), lowest to highest: base < dropdown < modal < tooltip <
toast.

### 7.2 Entities

| Entity | Fields | Rules |
|--------|--------|-------|
| TimePreference | `zone: 'UTC' \| 'Local'` | Default `UTC`. Persisted in localStorage, and any unrecognized or unreadable value means `UTC` (FR-103). Applies app-wide. |
| Toast | id, tone (`success` \| `info` \| `error`), title (non-empty), message (optional), created-at | Visible at most 4. Lifetime of 5 s for success and info; error has none. |
| ApiError | status (number), message (`HTTP <status>: <statusText>`) | Additive. Existing `instanceof Error` checks and message text keep working. |
| Enum label map | category → (raw value → label) | Categories: severity, command status, event type, update kind. Fallback per FR-014 and FR-015. |

### 7.3 State transitions

**ConfirmDialog**

| From | Event | To |
|------|-------|----|
| Closed | Opened by the user | Idle |
| Idle | Confirm | Pending |
| Idle | Cancel / Escape / backdrop | Closed |
| Pending | Action resolves | Closed (plus a success toast if a message was given) |
| Pending | Action rejects | Error |
| Pending | Cancel / Escape / backdrop | *(ignored, stays Pending)* |
| Error | Confirm (retry) | Pending (inline error cleared) |
| Error | Cancel / Escape / backdrop | Closed |

**Toast**

| From | Event | To |
|------|-------|----|
| — | Raised | Visible (timer running for success/info) |
| Visible | Pointer enters / focus enters | Paused |
| Paused | Pointer and focus both leave | Visible (remaining time) |
| Visible | Timer elapses (success/info only) | Dismissed |
| Visible / Paused | Close button | Dismissed |
| Visible / Paused | Fifth toast raised while this is the oldest | Dismissed |

**Query view (QueryState, FleetOverview, device pages)**

| Condition | Rendered |
|-----------|----------|
| Loading, no data | Skeleton / loading placeholder |
| Error, no data, status 404 (device pages) | "Device not found" with a link to Fleet |
| Error, no data, other | Error panel with message and Retry |
| Error, stale data present | Existing data |
| Success, empty (Fleet, no search) | SetupGuide |
| Success, non-empty | Content |

### 7.4 Timestamp call sites to migrate (FR-095)

- `ActivityLog.tsx`: lines 125 and 216
- `ContainerDetail.tsx`: lines 389 and 623
- `ContainerMetricChart.tsx`: line 64
- `MetricChart.tsx`: line 106
- `Alerts.tsx`: line 124
- `dashboard/ActivityRiver.tsx`: line 120
- `DeviceDetail.tsx`: lines 1465, 1666 and 1703
- `DeviceProbes.tsx`: line 214
- `ProbeDetail.tsx`: line 127
- `Probes.tsx`: line 199
- `CertificateSettings.tsx`: lines 114, 230 and 231
- `LogSettings.tsx`: line 105
- `utils/cron.ts`: line 36 (`formatRunDate`)

Number formatting through `Number.prototype.toLocaleString` is not a timestamp
and is not covered.

### 7.5 Validation rules

- The toast title must be non-empty.
- The ConfirmDialog title and confirm label must be non-empty.
- Formatters must accept non-finite, negative, null or unparseable input without
  throwing, and must return the fallbacks defined in FR-089, FR-090 and FR-093.

## 8. Acceptance Criteria

Each AC names how it is verified:
- **[vitest]** means a vitest / Testing Library test, which may be a static
  source-scan test that reads files with Node `fs`.
- **[screenshot: name]** means a named after-screenshot taken from the demo
  build (`http://localhost:5179/rIOt/#/…`) and saved to
  `docs/implementation/UI-FOUNDATION-001-screenshots/`, compared with its
  `before-*` counterpart where one exists.
- **[demo-check]** means a manual walkthrough of the demo build with the browser
  console observed.
- **[build]** means a command-line build or type-check result.

### 8.1 Tokens

**AC-001 — Tokens defined [FR-001]** [vitest]
Given `web/src/index.css`
When its contents are read
Then an `@theme` block defines the color tokens surface, raised, border, text, text-muted, accent, success, warn, danger and info.

**AC-002 — Token values match the existing palette [FR-002]** [vitest]
Given the token definitions
When each value is compared with the Section 7.1 table
Then every token resolves to the listed palette value, or to a documented one-step adjustment.

**AC-003 — Layering order [FR-003]** [vitest]
Given the z-index scale tokens
When their numeric values are compared
Then dropdown < modal < tooltip < toast.

**AC-004 — No hex in chart and touched files [FR-004]** [vitest]
Given the six files listed in FR-004
When each is scanned for `#[0-9a-fA-F]{3,8}` color literals
Then none is found
And `Terminal.tsx` is excluded from the scan.

**AC-005 — Primitives are token-only [FR-005]** [vitest]
Given every file under `web/src/components/ui/`
When it is scanned for `gray-\d` classes and hex color literals
Then none is found.

**AC-006 — Visual parity of the palette [FR-002]** [screenshot: after-fleet.png]
Given the demo Fleet page before and after the change
When the screenshots are compared side by side
Then the shell, card, border and status colors are the same hue.

**AC-007 — Text contrast [NFR-004]** [vitest]
Given the hex values behind the text, text-muted, surface and raised tokens
When the WCAG contrast ratio is computed for each text/background pair
Then every ratio is at least 4.5:1.

### 8.2 Button

**AC-008 — Variants and sizes [FR-006]** [vitest]
Given Button rendered once for each variant (primary, secondary, ghost, danger) and each size (sm, md)
When the eight renders are inspected
Then each exposes its variant and size in a test-assertable way
And no two variant/size combinations share an identical class set.

**AC-009 — Default type is button [FR-007]** [vitest]
Given a Button rendered with no `type` inside a form
When it is clicked
Then the form is not submitted
And the element has `type="button"`.

**AC-010 — Disabled blocks clicks [FR-008]** [vitest]
Given a disabled Button with a click spy
When it is clicked
Then the spy is not called.

**AC-011 — Pending state [FR-009]** [vitest]
Given a Button in the pending state with the pending label "Deleting…"
When it renders
Then it shows a spinner and the text "Deleting…"
And it is disabled
And it has `aria-busy="true"`.

**AC-012 — Focus-visible ring [FR-010, NFR-005]** [vitest]
Given Button, the Modal close control, the toast close button, the QueryState Retry button and the header clock toggle
When their classes are inspected
Then each carries a focus-visible ring style.

### 8.3 Card, Badge and enum labels

**AC-013 — Card [FR-011]** [vitest]
Given a Card with the title "Hardware"
When it renders
Then "Hardware" is exposed as a heading
And the container uses the raised and border tokens.

**AC-014 — Severity labels [FR-012, FR-013]** [vitest]
Given every value of the severity type union
When it is passed through the label map
Then each returns an explicit sentence-case label (for example `warning` → "Warning")
And no label equals its raw value when the raw value is lowercase or contains an underscore.

**AC-015 — Command status labels [FR-012, FR-013]** [vitest]
Given every value of the command-status type union
When it is passed through the label map
Then each returns an explicit sentence-case label (for example `queued` → "Queued").

**AC-016 — Event type labels [FR-012, FR-013]** [vitest]
Given every value of the event-type type union
When it is passed through the label map
Then each returns an explicit label that contains no underscore.

**AC-017 — Update kind labels [FR-013]** [vitest]
Given the update kinds `os_update` and `agent_update`
When they are passed through the label map
Then they return "OS update" and "Agent update".

**AC-018 — Unknown value fallback [FR-014]** [vitest]
Given the unmapped value `some_new_value`
When it is passed through the label map
Then it returns "Some new value".

**AC-019 — Empty value fallback [FR-015]** [vitest]
Given null, undefined and ""
When each is passed through the label map
Then each returns "—".

**AC-020 — Badge uses the map [FR-016]** [vitest]
Given a Badge given the severity value `critical`
When it renders
Then its text is the mapped label, not `critical`
And it uses the danger tone.

**AC-021 — Alerts page shows mapped labels [FR-017]** [vitest]
Given the Alerts page rendered with an event whose severity is `warning` and whose type is a snake_case value
When the page renders
Then the text "Warning" and the mapped event-type label appear
And neither raw value appears as visible text.

### 8.4 Modal

**AC-022 — Dialog semantics [FR-018]** [vitest]
Given an open Modal titled "Edit rule"
When the accessibility tree is queried
Then `getByRole('dialog', { name: 'Edit rule' })` finds it
And it has `aria-modal="true"`.

**AC-023 — Initial focus inside [FR-019]** [vitest]
Given a Modal is opened from a button
When it mounts
Then `document.activeElement` is inside the dialog.

**AC-024 — Tab wraps forward [FR-020]** [vitest]
Given an open Modal with focus on its last focusable element
When the user presses Tab
Then focus moves to the dialog's first focusable element.

**AC-025 — Shift+Tab wraps backward [FR-020]** [vitest]
Given an open Modal with focus on its first focusable element
When the user presses Shift+Tab
Then focus moves to the dialog's last focusable element.

**AC-026 — Escape closes [FR-021]** [vitest]
Given an open, non-pending Modal
When the user presses Escape
Then the close handler is called once.

**AC-027 — Focus restored [FR-022]** [vitest]
Given a Modal opened by clicking the button "Open"
When the Modal closes
Then focus returns to "Open".

**AC-028 — Backdrop closes, panel does not [FR-023]** [vitest]
Given an open Modal
When the user clicks inside the panel
Then the close handler is not called
And when the user clicks the backdrop
Then the close handler is called once.

**AC-029 — Stacked Escape [FR-024]** [vitest]
Given a ConfirmDialog opened on top of another open Modal
When the user presses Escape
Then only the ConfirmDialog closes
And the underlying Modal stays open.

**AC-030 — Migrated modals [FR-025]** [vitest]
Given ConfirmModal, UpdateConfirmModal and PatchReviewModal each rendered open
When the accessibility tree is queried
Then each exposes `role="dialog"` named by its title.

**AC-031 — No ad-hoc z-index in migrated modals [FR-025, FR-003]** [vitest]
Given the source of every modal migrated in this story
When it is scanned
Then it contains no `z-40`, `z-50` or `z-[60]` utility.

### 8.5 ConfirmDialog

**AC-032 — Pending visuals [FR-027]** [vitest]
Given a ConfirmDialog whose action returns an unresolved promise
When the user clicks confirm
Then the confirm button shows a spinner and the pending label
And both the confirm and cancel buttons are disabled.

**AC-033 — Escape ignored while pending [FR-028]** [vitest]
Given a pending ConfirmDialog
When the user presses Escape
Then the dialog stays open.

**AC-034 — Backdrop ignored while pending [FR-028]** [vitest]
Given a pending ConfirmDialog
When the user clicks the backdrop
Then the dialog stays open.

**AC-035 — No double invoke [FR-029]** [vitest]
Given a ConfirmDialog with an action spy that returns an unresolved promise
When the user clicks confirm twice
Then the spy has been called exactly once.

**AC-036 — Success closes with a toast [FR-030]** [vitest]
Given a ConfirmDialog with the success message "Probe deleted" and an action that resolves
When the user confirms and the promise resolves
Then the dialog is removed
And a success toast "Probe deleted" is visible.

**AC-037 — Failure stays open with an inline error [FR-031]** [vitest]
Given a ConfirmDialog whose action rejects with `new Error('HTTP 500: Internal Server Error')`
When the user confirms
Then the dialog stays open
And an element with `role="alert"` inside it contains "HTTP 500: Internal Server Error"
And the buttons are enabled again.

**AC-038 — Message-less rejection [FR-031]** [vitest]
Given a ConfirmDialog whose action rejects with a value that has no message
When the user confirms
Then the inline error reads "Something went wrong. Please try again."

**AC-039 — Retry after failure [FR-032]** [vitest]
Given a ConfirmDialog showing an inline error
When the user clicks confirm again
Then the inline error is cleared
And the action is invoked a second time.

**AC-040 — Cancel after failure [FR-032]** [vitest]
Given a ConfirmDialog showing an inline error
When the user clicks cancel
Then the dialog closes
And the action is not invoked again.

**AC-041 — Danger variant and initial focus [FR-033]** [vitest]
Given a danger ConfirmDialog
When it opens
Then the confirm button is a danger-variant Button
And `document.activeElement` is the Cancel button.

**AC-042 — Bulk update "Updating…" is visible [FR-034]** [vitest]
Given FleetOverview with outdated agents and a bulk-update API that returns an unresolved promise
When the user opens the update confirmation and confirms
Then the dialog stays open showing "Updating…" until the promise settles.

### 8.6 EmptyState, Skeleton and QueryState

**AC-043 — EmptyState [FR-035]** [vitest]
Given an EmptyState with a title, a description and an action labelled "Add probe"
When the user clicks "Add probe"
Then the action handler is called
And the title and description are visible.

**AC-044 — Skeleton hidden from assistive technology [FR-036]** [vitest]
Given a rendered Skeleton
When it is inspected
Then it has `aria-hidden="true"`.

**AC-045 — QueryState loading [FR-038]** [vitest]
Given QueryState with `isLoading: true` and no data
When it renders
Then a container with `aria-busy="true"` and an accessible "Loading" status is present
And the children are not rendered.

**AC-046 — QueryState error with Retry [FR-039]** [vitest]
Given QueryState with an error "HTTP 502: Bad Gateway", no data, and a refetch spy
When the user clicks Retry
Then the message "HTTP 502: Bad Gateway" is visible
And the refetch spy is called once.

**AC-047 — QueryState empty [FR-040]** [vitest]
Given QueryState with successful data `[]` and an emptiness predicate that returns true
When it renders
Then the empty-state content is rendered
And the children are not.

**AC-048 — QueryState data [FR-041]** [vitest]
Given QueryState with successful non-empty data
When it renders
Then the children are rendered with that data.

**AC-049 — Stale data survives a background error [FR-042]** [vitest]
Given QueryState with existing data and an error from a background refetch
When it renders
Then the children are rendered with the existing data
And the error panel is not.

**AC-050 — Error beats empty [FR-043]** [vitest]
Given QueryState with an error, no data, and an emptiness predicate that would return true
When it renders
Then the error panel is rendered
And the empty-state content is not.

### 8.7 Toasts

**AC-051 — Region and placement [FR-044, FR-046]** [vitest]
Given the app root with the toast system mounted
When a success toast is raised
Then it appears inside `getByRole('region', { name: 'Notifications' })`.

**AC-052 — Placement on screen [FR-046]** [screenshot: after-toast-stack.png]
Given three toasts raised in the demo build
When the screen is captured
Then the stack sits at the bottom right with the newest on top.

**AC-053 — Success auto-dismiss at 5 s [FR-048]** [vitest]
Given fake timers and a success toast raised at t=0
When time advances to 4,999 ms
Then the toast is visible
And when time advances to 5,000 ms
Then the toast is gone.

**AC-054 — Info auto-dismiss at 5 s [FR-048]** [vitest]
Given fake timers and an info toast
When time advances 5,000 ms
Then the toast is gone.

**AC-055 — Error persists [FR-049, BR-003]** [vitest]
Given fake timers and an error toast
When time advances 60,000 ms
Then the toast is still visible.

**AC-056 — Close button [FR-051]** [vitest]
Given any visible toast
When the user activates the button named "Dismiss notification"
Then the toast is removed.

**AC-057 — Hover pauses and resumes [FR-050]** [vitest]
Given fake timers and a success toast at t=0
When the pointer enters it at 2,000 ms and time advances 10,000 ms
Then the toast is still visible
And when the pointer leaves and time advances 2,999 ms
Then it is still visible
And at 3,000 ms after leaving
Then it is gone.

**AC-058 — Focus pauses [FR-050]** [vitest]
Given fake timers and a success toast
When its close button receives focus and time advances 10,000 ms
Then the toast is still visible.

**AC-059 — Newest on top [FR-046]** [vitest]
Given toasts "A" then "B" are raised
When the stack is read in DOM visual order
Then "B" comes before "A".

**AC-060 — Maximum of 4 visible [FR-047]** [vitest]
Given toasts 1 to 4 are visible
When toast 5 is raised
Then toasts 2, 3, 4 and 5 are visible
And toast 1 is gone.

**AC-061 — Live-region politeness [FR-052]** [vitest]
Given one success toast and one error toast
When their containers are inspected
Then the success toast is in an `aria-live="polite"` region
And the error toast has `role="alert"`.

**AC-062 — Survives navigation [FR-053]** [vitest]
Given a toast raised on route `/devices/x`
When the router navigates to `/`
Then the toast is still visible.

### 8.8 Native dialog replacement

**AC-063 — No native dialogs in source [FR-054, BR-010]** [vitest]
Given every non-test `.ts`/`.tsx` file under `web/src`
When it is scanned for `window.confirm`, `window.alert`, `window.prompt` and bare `confirm(` / `alert(` / `prompt(` calls
Then none is found.

**AC-064 — No stubbed native dialogs in tests [FR-055]** [vitest]
Given every `*.test.ts(x)` under `web/src`
When it is scanned
Then no test stubs or spies on `window.confirm` or `window.alert`
And the test count is not lower than before migration, except for tests removed under FR-106/FR-107.

**AC-065 — DeviceProbes delete [FR-056, FR-063]** [vitest]
Given DeviceProbes with a probe named "nas-ping"
When the user clicks that probe's delete control
Then a danger ConfirmDialog is shown whose message contains "nas-ping"
And when the user cancels
Then the delete API is not called.

**AC-066 — Probes delete probe [FR-057, FR-064]** [vitest]
Given the Probes page with one probe
When the user deletes it and confirms
Then the delete API is called once
And a success toast is shown.

**AC-067 — Probes delete device probe [FR-058, FR-063]** [vitest]
Given the Probes page with one device probe
When the user deletes it
Then a danger ConfirmDialog appears
And cancelling it does not call the API.

**AC-068 — AgentManagement update all [FR-059]** [vitest]
Given AgentManagement with 3 devices on v2.60.0
When the user triggers "update all" for that version
Then a primary ConfirmDialog states "3" and "v2.60.0"
And on confirm with the API resolving `{sent: 2, skipped: 1}`, a success toast shows 2 sent and 1 skipped.

**AC-069 — AlertRuleSettings delete, first site [FR-060]** [vitest]
Given AlertRuleSettings with a rule in the first delete location (today line 202)
When the user clicks delete
Then a danger ConfirmDialog appears.

**AC-070 — AlertRuleSettings delete, second site [FR-060]** [vitest]
Given AlertRuleSettings with a rule in the second delete location (today line 223)
When the user clicks delete
Then a danger ConfirmDialog appears.

**AC-071 — LogSettings purge [FR-061]** [vitest]
Given LogSettings
When the user clicks "Delete all server logs"
Then a danger ConfirmDialog appears whose message contains "cannot be undone".

**AC-072 — NotificationSettings delete channel [FR-062, FR-064]** [vitest]
Given NotificationSettings with one channel
When the user deletes it and confirms with the API resolving
Then a success toast naming the channel deletion is shown.

**AC-073 — Device delete failure is inline [FR-065, BR-002]** [vitest]
Given DeviceDetail with a `window.alert` spy and `deleteDevice` rejecting with `HTTP 500: Internal Server Error`
When the user clicks Delete and confirms
Then the dialog stays open with an inline `role="alert"` error containing that message
And the `window.alert` spy is not called.

**AC-074 — Device delete success [FR-066]** [vitest]
Given DeviceDetail for hostname "web-01" with `deleteDevice` resolving
When the user deletes and confirms
Then the router is at the Fleet route
And a success toast containing "web-01" is visible.

### 8.9 Inline banner replacement

**AC-075 — No Fleet banners [FR-067]** [vitest]
Given FleetOverview after a successful bulk update and a successful patch-all
When the page body (outside the toast region) is inspected
Then no inline "Agent update dispatched" or "Patch commands dispatched" banner exists.

**AC-076 — Bulk update success toast [FR-068]** [vitest]
Given bulk update resolving to `{sent: 4, skipped: 1}`
When the user confirms
Then the dialog closes
And a success toast shows 4 sent and 1 skipped.

**AC-077 — Bulk update failure is not silent [FR-069]** [vitest]
Given bulk update rejecting with `HTTP 503: Service Unavailable`
When the user confirms
Then the ConfirmDialog stays open with an inline error containing that message.

**AC-078 — Patch-all pending [FR-070]** [vitest]
Given PatchReviewModal and `bulkPatchDevices` returning an unresolved promise
When the user clicks the patch-all confirm control
Then that control is disabled and shows a pending label.

**AC-079 — Patch-all success toast [FR-071]** [vitest]
Given `bulkPatchDevices` resolving to `{sent: 3, queued: 1, skipped: 2, total: 6}`
When the user confirms patch-all
Then PatchReviewModal closes
And a success toast shows 3 sent, 1 queued, 2 skipped and 6 total.

**AC-080 — Patch-all failure toast [FR-072]** [vitest]
Given `bulkPatchDevices` rejecting with `HTTP 500: Internal Server Error`
When the user confirms patch-all
Then an error toast containing that message is visible
And PatchReviewModal is still open.

**AC-081 — No DeviceDetail banners [FR-073]** [vitest]
Given DeviceDetail after a successful command and a failed command
When the page body (outside dialogs and the toast region) is inspected
Then no inline "Command sent:" banner or command-error banner exists.

**AC-082 — Command confirmation variants [FR-074, BR-001]** [vitest]
Given an online DeviceDetail
When the user opens Reboot, Shutdown, Patch and Update Agent in turn
Then Reboot and Shutdown show a danger ConfirmDialog
And Patch and Update Agent show a primary ConfirmDialog.

**AC-083 — Command success toast is humanized [FR-075, FR-017]** [vitest]
Given DeviceDetail for "web-01" and `sendCommand` resolving `{action: 'os_update', status: 'pending'}`
When the user confirms Patch
Then a success toast contains "OS update", "Pending" and "web-01"
And no visible text contains "os_update".

**AC-084 — Command failure is inline [FR-076]** [vitest]
Given `sendCommand` rejecting with `HTTP 409: Conflict`
When the user confirms Reboot
Then the ConfirmDialog stays open with an inline error containing "HTTP 409: Conflict".

**AC-085 — Commands outside a dialog use toasts [FR-077]** [vitest]
Given SecurityScoreModal invoking a remediation command
When the command resolves
Then a success toast appears
And when a second command rejects
Then an error toast with the message appears.

**AC-086 — Fetch-logs success [FR-078]** [vitest]
Given the fetch-logs request resolving
When the user requests logs
Then a success toast is shown.

**AC-087 — Fetch-logs failure [FR-078]** [vitest]
Given the fetch-logs request rejecting with `HTTP 504: Gateway Timeout`
When the user requests logs
Then an error toast containing that message is shown.

### 8.10 Honest error states

**AC-088 — Typed error carries status [FR-079]** [vitest]
Given the fetch layer receives HTTP 404 "Not Found"
When the client call rejects
Then the error is `instanceof Error`
And its `status` is 404
And its message is exactly "HTTP 404: Not Found".

**AC-089 — 401 unchanged [FR-080]** [vitest]
Given the fetch layer receives HTTP 401
When a client call is made
Then the existing 401 behavior (reload) still happens.

**AC-090 — Fleet fetch error shows the error panel [FR-081, BR-004]** [vitest]
Given `/devices` rejects with `HTTP 500: Internal Server Error`
When FleetOverview renders
Then "Couldn't load devices", the message and a Retry button are visible
And no SetupGuide content is rendered.

**AC-091 — Fleet Retry recovers [FR-081]** [vitest]
Given the Fleet error panel is shown
When the user clicks Retry and the refetch resolves with two devices
Then both device rows are rendered.

**AC-092 — Fleet empty shows SetupGuide [FR-082]** [vitest]
Given `/devices` resolves to `[]` and the search box is empty
When FleetOverview renders
Then the inline SetupGuide is rendered.

**AC-093 — Fleet loading shows no SetupGuide [FR-083]** [vitest]
Given `/devices` has not resolved yet
When FleetOverview renders
Then a loading placeholder is shown
And no SetupGuide content is rendered.

**AC-094 — DeviceDetail 404 [FR-084]** [vitest]
Given the device fetch rejects with status 404
When DeviceDetail renders
Then "Device not found" is visible
And a link to the Fleet route is present.

**AC-095 — DeviceDetail non-404 [FR-085]** [vitest]
Given the device fetch rejects with status 500
When DeviceDetail renders
Then "Couldn't load device", the error message and Retry are visible
And "Device not found" is not.

**AC-096 — Containers pages 404 [FR-084]** [vitest]
Given the device fetch rejects with status 404
When DeviceContainers and ContainerDetailPage each render
Then each shows "Device not found" and a link to Fleet.

**AC-097 — Containers pages non-404 [FR-085]** [vitest]
Given the device fetch rejects with status 500
When DeviceContainers and ContainerDetailPage each render
Then each shows "Couldn't load device", the message and Retry.

**AC-098 — Network error is non-404 [FR-085]** [vitest]
Given the device fetch rejects with `TypeError('Failed to fetch')`
When DeviceDetail renders
Then "Couldn't load device" and "Failed to fetch" are visible.

**AC-099 — Container missing in a loaded device [FR-086]** [vitest]
Given the device loads but has no container with the route's container ID
When ContainerDetailPage renders
Then "Container not found" and a link to that device's containers page are visible.

**AC-100 — Device pages loading skeleton [FR-087]** [vitest]
Given the device fetch has not resolved yet
When DeviceDetail renders
Then a Skeleton is present
And the text "Loading..." is absent.

### 8.11 Formatting and time

**AC-101 — formatBytes [FR-089]** [vitest]
Given the inputs 0, 1023, 1024, 1536, 1048576, 1073741824, 1099511627776, -5 and NaN
When each is passed to formatBytes
Then the outputs are "0 B", "1023 B", "1.0 KB", "1.5 KB", "1.0 MB", "1.0 GB", "1.0 TB", "0 B" and "0 B".

**AC-102 — Duration [FR-090]** [vitest]
Given the inputs 0.35, 42, 600, 3900, 97200, -1 and NaN seconds
When each is passed to the duration formatter
Then the outputs are "350ms", "42s", "10m", "1h 5m", "1d 3h", "—" and "—".

**AC-103 — Relative time [FR-091]** [vitest]
Given a fixed "now"
When instants 30 s, 5 min, 3 h and 2 d in the past and 10 min in the future are formatted
Then the outputs are "just now", "5m ago", "3h ago", "2d ago" and "in 10m".

**AC-104 — Absolute timestamp, UTC [FR-092]** [vitest]
Given the instant `2026-10-17T14:03:22Z` and the UTC preference
When it is formatted
Then the output is "2026-10-17 14:03:22 UTC".

**AC-105 — Absolute timestamp, Local [FR-092]** [vitest]
Given the process time zone is America/Los_Angeles, the instant `2026-10-17T14:03:22Z`, and the Local preference
When it is formatted
Then the output is "2026-10-17 07:03:22 PDT".

**AC-106 — Invalid timestamp [FR-093]** [vitest]
Given the inputs `"not-a-date"`, `undefined` and `""`
When each is formatted
Then each returns "—"
And none returns "Invalid Date".

**AC-107 — Reboot-ETA stays UTC [FR-094, BR-007]** [vitest]
Given the Local preference active in America/Los_Angeles
When `formatUtc('2026-10-17T14:03:00Z')` is called
Then it returns "2026-10-17 14:03 UTC"
And all existing `rebootEta` tests pass unchanged.

**AC-108 — No duplicate formatters [FR-088, FR-096]** [vitest]
Given every non-test source file under `web/src` other than `utils/format.ts`
When it is scanned for function definitions named `formatBytes`, `formatUptime` or `formatDuration`
Then none is found.

**AC-109 — Call sites carry a zone label [FR-095, BR-005]** [vitest]
Given ActivityLog, Alerts and CertificateSettings rendered with fixed timestamps and the default preference
When the rendered timestamps are read
Then each ends with "UTC".

**AC-110 — Call-site inventory complete [FR-095]** [vitest]
Given the 19 call sites in Section 7.4
When each is scanned
Then none passes a Date to `toLocaleString`, `toLocaleDateString` or `toLocaleTimeString`.

**AC-111 — RelativeTime markup [FR-097]** [vitest]
Given `<RelativeTime>` for an instant 5 minutes ago with the UTC preference
When it renders
Then a `<time>` element has the ISO `dateTime`, the text "5m ago", and a `title` ending in "UTC".

**AC-112 — RelativeTime refresh [FR-098]** [vitest]
Given fake timers and `<RelativeTime>` showing "just now"
When time advances 120 s
Then the text reads "2m ago".

**AC-113 — Header clock default [FR-099, FR-100, BR-006]** [vitest]
Given empty localStorage
When the app header renders
Then the clock text ends with "UTC" in `HH:mm:ss UTC` form.

**AC-114 — Header clock toggle [FR-101]** [vitest]
Given the header clock in UTC with the process time zone America/Los_Angeles
When the user activates the button whose accessible name contains "Time zone: UTC"
Then the clock text ends with "PDT"
And the button's accessible name now references local time.

**AC-115 — Preference persists [FR-102]** [vitest]
Given the user toggled to Local
When the app is unmounted and mounted again
Then the clock shows the local zone.

**AC-116 — Storage failure fallback [FR-103]** [vitest]
Given localStorage `getItem` throws, and separately holds the value "banana"
When the app mounts in each case
Then the clock shows UTC
And no error is thrown.

**AC-117 — Live propagation [FR-104]** [vitest]
Given a page showing an absolute timestamp and a `<RelativeTime>`, with the UTC preference
When the preference is toggled to Local
Then both the absolute timestamp and the `<RelativeTime>` title show the local zone label without a remount.

**AC-118 — Chart time labels [FR-105]** [vitest]
Given the chart tick and tooltip label formatters with the Local preference
When they format `2026-10-17T14:03:22Z` in America/Los_Angeles
Then the tick shows local time-of-day
And the tooltip label ends with "PDT".

**AC-119 — Chart zone label visible [FR-105]** [screenshot: after-device-detail-chart-tooltip.png]
Given a device metric chart in the demo build
When a data point is hovered
Then the tooltip timestamp shows a zone label.

### 8.12 Dead code

**AC-120 — Dead components removed [FR-106]** [vitest]
Given the repository
When the existence of Sparkline.tsx, MiniScore.tsx, ContainerGroup.tsx and ContainerCard.tsx under `web/src/components/` is checked
Then none exists
And no source file imports any of them.

**AC-121 — Drawer removed, content kept [FR-107]** [vitest]
Given `components/ContainerDetail.tsx`
When its exports are inspected
Then it has no default export
And `ContainerDetailContent` is still exported
And ContainerDetailPage renders a container's details.

### 8.13 Hostnames

**AC-122 — DeviceDetail hostname case [FR-108, BR-008]** [vitest]
Given a device with hostname "Web-01.lan"
When DeviceDetail renders
Then the h1 text is exactly "Web-01.lan"
And the h1 has no `uppercase` class.

**AC-123 — Other hostname sites [FR-108]** [vitest]
Given a device with hostname "Web-01.lan"
When FleetOverview, Security, Alerts, DeviceContainers, DeviceProbes and ContainerDetailPage each render it
Then the element showing the hostname has no `uppercase` class in any of them.

**AC-124 — Hostname case visual [FR-108]** [screenshot: after-fleet.png, after-device-detail-header.png]
Given demo devices
When the Fleet and DeviceDetail pages are captured
Then hostnames appear in their reported case.

**AC-125 — Section headers untouched [FR-109]** [vitest]
Given the Security page
When the "Per-Device Security" header renders
Then it keeps its uppercase styling.

### 8.14 Demo mode

**AC-126 — Demo WebSocket connected [FR-110]** [vitest]
Given the demo WebSocket provider
When `useWebSocket()` is read
Then `connected` is `true`.

**AC-127 — Demo API parity [FR-111]** [vitest + build]
Given the real API client object and the demo API client object
When their method-name sets are compared
Then they are equal
And `tsc --noEmit` fails if a method is removed from the demo client.

**AC-128 — Demo settings parity [FR-112]** [vitest + build]
Given the real settings client and the demo settings client
When their method-name sets are compared
Then they are equal.

**AC-129 — Demo heartbeats [FR-113]** [vitest]
Given the demo client
When `getFleetHeartbeats()` resolves
Then every demo device has a non-empty, time-ascending series.

**AC-130 — Demo containers and toggles [FR-114, FR-115]** [vitest]
Given the demo client and demo settings
When `getFleetContainers()` and `getFeatureToggles()` resolve
Then containers exist for at least one demo device
And every feature toggle is enabled.

**AC-131 — Demo mutations are inert [FR-116]** [vitest]
Given a `fetch` spy
When each demo mutating method (for example `bulkDockerUpdate`, `setAutoPatch` or `updateDeviceLocation`) is called
Then each resolves
And the `fetch` spy is never called.

**AC-132 — Demo unknown device is 404 [FR-117]** [vitest]
Given the demo client
When `getDevice('does-not-exist')` is called
Then it rejects with a typed error whose status is 404.

**AC-133 — Demo dashboard renders [FR-110, FR-118]** [screenshot: after-dashboard-demo.png]
Given the demo build at `#/dashboard`
When it has loaded for 5 s
Then the device charts, leaderboard and activity sections show populated content
And no "Live updates paused" notice is visible.

**AC-134 — Demo console clean [FR-111, FR-112, FR-118]** [demo-check]
Given the demo build with the console open
When the user visits Dashboard, Fleet, one DeviceDetail, Security and each Settings tab
Then no "No queryFn" error or unhandled promise rejection is logged.

### 8.15 Polish

**AC-135 — Security stat cards in one row [FR-119]** [screenshot: after-security.png]
Given the demo Security page at a 1440 px viewport width
When it is captured
Then all stat cards sit on one row with no orphan card on a second row.

**AC-136 — Gauge label [FR-120]** [vitest]
Given DeviceDetail with security score 72 and grade C
When it renders
Then the visible text "Security score" is next to the gauge
And `getByRole(…, { name: /Security score 72, grade C/ })` finds the gauge.

**AC-137 — Delete is a separated danger button [FR-121, FR-122]** [vitest]
Given an online DeviceDetail
When the header renders
Then `getByRole('navigation', { name: 'Device sections' })` contains Docker, Probes and Terminal
And the Delete button is not inside that navigation
And Delete is a danger-variant Button.

**AC-138 — Header visual [FR-120, FR-121, FR-122]** [screenshot: after-device-detail-header.png]
Given the demo DeviceDetail
When the header is captured
Then the Delete button is visibly danger-styled and set apart from the navigation links
And the gauge shows its "Security score" caption.

### 8.16 Non-functional

**AC-139 — No new dependencies [NFR-001]** [build]
Given `web/package.json` on this branch and on `main`
When `dependencies` and `devDependencies` are compared
Then they are identical.

**AC-140 — Type-check clean [NFR-002]** [build]
Given the branch
When `npx tsc --noEmit` runs in `web/`
Then it exits 0 with no errors.

**AC-141 — Suite green and at least 405 tests [NFR-003]** [build]
Given the branch
When `npm run test:run` runs
Then it reports zero failures and at least 405 passing tests.

**AC-142 — Reduced motion [NFR-006]** [vitest]
Given the reduced-motion styling is applied
When Skeleton and a toast render
Then neither carries an active pulse or slide animation class under the `motion-reduce` condition.

**AC-143 — Bundle budget [NFR-007]** [build]
Given production builds of `main` and of the branch
When the gzip size of the main entry chunk is compared
Then it has grown by no more than 20 KB.

**AC-144 — Both builds compile [NFR-009]** [build]
Given the branch
When `npm run build` runs with and without `VITE_DEMO=true`
Then both succeed.

**AC-145 — No backend change [NFR-010]** [build]
Given `git diff --name-only main...HEAD`
When it is listed
Then every path is under `web/` or `docs/`.

## 9. Out of Scope

- The device-page split, including misaligned device-page table columns.
- App-shell or navigation redesign.
- A light theme or theme switching.
- A full-repo sweep of every `gray-*` class. Only primitives and touched files
  must use tokens.
- Normalizing the 42 arbitrary `text-[9px|10px|11px]` sizes outside touched
  files.
- Chart library changes. Recharts stays, and only its color and time-label
  sources change.
- Any backend, Go server or agent change.
- Migrating hand-rolled modals whose files are not otherwise edited: App.tsx,
  ActivityLog, CreateAlertDialog, DeviceProbeModal, ProbeModal,
  SecurityScoreModal, SetupGuide, and the AlertRuleSettings/NotificationSettings
  edit modals unless edited.
- Silent failures not named in this FRD: location edit, tag edit, auto-patch
  toggle, and the patch-review detail fetch (see OQ-001).
- Syncing the time preference across browser tabs.
- The xterm theme in `Terminal.tsx`.
- Tagging or pushing a release.

## 10. Assumptions

Clod added these where the D1–D14 decisions did not settle a detail. Ryan or the
architect may override any of them.

- **A-001 (D4 and D5 overlap):** Device delete and bulk agent update are already
  confirmed through a modal, so D4 (inline error, dialog stays open) and D5
  (error toast) both apply to them. BR-002 settles this: failures inside a
  ConfirmDialog appear inline only, and failures elsewhere appear as error
  toasts, so each failure is shown exactly once. The device-delete `alert()` is
  removed and replaced by the inline error. If Ryan wants a toast as well, add
  one to FR-065 and FR-069.
- **A-002:** Initial focus in a danger ConfirmDialog goes to Cancel, so pressing
  Enter does not trigger a destructive action by accident.
- **A-003:** A fifth toast evicts the oldest visible toast, even if that toast is
  an error. There is no hidden queue.
- **A-004:** Auto-dismiss also pauses while keyboard focus is inside a toast (WCAG
  2.2.1), and resumes with the remaining time rather than a fresh 5 s.
- **A-005:** The canonical absolute format is 24-hour ISO-like
  (`YYYY-MM-DD HH:mm:ss ZONE`). The header clock moves from its current 12-hour
  `h:mm:ss AM UTC` to 24-hour `HH:mm:ss UTC`. This is a visible change to the
  header.
- **A-006:** The local zone abbreviation is whatever the browser returns for its
  short time-zone name. In some locales this is an offset such as "GMT+2".
- **A-007:** REBOOT-ETA-001 maintenance-window times stay UTC whatever the
  preference (BR-007), as an exception to D1's "every timestamp" rule, because
  windows are configured in UTC and REBOOT-ETA-001 FR-013 requires UTC.
- **A-008:** Chart axis ticks follow the preference but do not each repeat the
  zone label. The label appears in the chart tooltip and/or caption.
- **A-009:** QueryState keeps showing stale data when a background refetch fails
  (FR-042). This avoids the WebSocket-driven pages flickering to an error panel.
- **A-010:** The canonical `formatBytes` keeps today's `utils/docker.ts` output,
  so existing tests stay green. Canonical durations follow the existing
  `formatContainerUptime` style, and DeviceDetail uptime under a day changes from
  `0h 5m` to `5m`.
- **A-011:** A failed patch-all leaves the PatchReviewModal open so Ryan can retry
  (FR-072), consistent with the D4 retry behavior.
- **A-012:** ContainerDetailPage gains a "Container not found" state (FR-086)
  because it shares the same misleading fallback.
- **A-013:** The token shades in Section 7.1 are defaults inferred from dominant
  usage. `text-muted` is gray-400 rather than gray-500 so it meets the 4.5:1
  contrast in NFR-004 (gray-500 on gray-900 is about 3.9:1).
- **A-014:** Bundle budget: the gzip size of the main entry chunk may grow by at
  most 20 KB. This is a generous ceiling, given that dead code is removed and
  nothing is added as a dependency.
- **A-015:** This is a multi-area foundation story. Clod asked for a single FRD,
  so it is kept as one document, with Sections 4.1–4.15 serving as the
  decomposition. The architect may stage implementation by section.

## 11. Open Questions

None of these blocks implementation.

- **OQ-001:** Other silent mutation failures exist outside this story's named
  call sites: DeviceDetail location edit, tag edit and auto-patch toggle, and the
  patch-review detail fetch (`catch { setPatchDetail(null) }`). Recommendation:
  in a follow-up story, route them through the toast system once it exists.
- **OQ-002:** Whether the time preference should also govern Go-rendered
  artifacts, such as the device Markdown summary. Those are outside the UI and
  outside this story.

## 12. Dependencies

- Tailwind v4 `@theme` support, which is already installed (`@tailwindcss/vite`
  4.2).
- The existing `components/Tooltip.tsx` (@floating-ui/react), so that the
  tooltip layer fits the FR-003 z-index scale.
- The `api/client.ts` `fetchJSON` contract (FR-079). Every page that catches
  errors depends on keeping its message format.
- The REBOOT-ETA-001 `utils/rebootEta.ts` and its tests (FR-094).
- The demo build aliases in `vite.config.ts` (`api/client` → `api/demo-client`,
  `api/settings` → `api/demo-settings`, `WebSocketProvider` →
  `DemoWebSocketProvider`) and the generators in `api/demo-data.ts`.
- The demo launch config `.claude/launch.json` "web-demo" (port 5179) for
  screenshot ACs, and the existing `before-*.png` captures in
  `docs/implementation/UI-FOUNDATION-001-screenshots/`.
- The pipeline order: Architect (ADD), then security review, then engineering.
  No tag and no push for this story.
