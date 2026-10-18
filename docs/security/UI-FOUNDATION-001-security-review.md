# Security Review Report
**Story ID:** UI-FOUNDATION-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-17
**Verdict:** APPROVED WITH CONDITIONS

---

## Threat Model Summary

This is a frontend-only story. No endpoint, request shape, credential, server
handler or dependency changes (FRD NFR-001/NFR-010, ADD Section 4 "Explicitly
unchanged"). The review is scoped to the browser-side risks the story actually
introduces or reshapes. No backend threats are assumed.

### Assets
- **Integrity of operator consent for destructive and fleet-wide actions:**
  device delete (including remote agent **uninstall** for online devices),
  reboot, shutdown, OS patch, agent update, bulk agent update (fleet-wide),
  patch-all (fleet-wide), probe/rule/channel delete, and server-log purge. The
  dashboard's confirmation step is the only human-in-the-loop control on these
  actions, so it is the primary asset in this story.
- **Truthfulness of action feedback:** the operator must learn whether a
  destructive request succeeded or failed (BR-002, BR-003). Silent failure of
  a reboot or delete is an operational-integrity risk.
- **The dashboard origin's DOM:** it must not execute markup from
  server-supplied or agent-supplied strings (hostnames, `sendCommand` error
  bodies, `statusText`, event messages).
- **Client auth behavior:** the existing 401 to `window.location.reload()` path
  (FR-080) must not change.
- **Production bundle hygiene:** demo stubs (fake "always-succeeds" clients,
  fixture data) must never be reachable in the production build.
- **One new client-persisted value:** `riot-time-zone` in localStorage. It is
  not sensitive, but it is untrusted input to rendering code.

### Threat Actors
- **Authenticated operator (Ryan) making mistakes:** the realistic actor for
  the confirmation-integrity findings. Misclicks, key auto-repeat and flapping
  device status are accidents, not attacks, but they lead to the same
  destructive outcomes.
- **Compromised or spoofing agent:** it controls its own reported `hostname`,
  and it can influence event messages and command-error text that the dashboard
  renders in toasts, dialog messages and error panels.
- **Anyone with write access to the browser profile** (another local user, an
  extension, a prior XSS): this actor can tamper with localStorage.
- **Anonymous demo visitor:** uses the public GitHub Pages build
  (`VITE_DEMO=true`). This actor must not be able to cause real requests.
- **Cross-site framer (clickjacking):** see SEC-009. The existing session
  cookie posture neutralizes this actor. No change in this story.

### Attack Surface Introduced
- New render sinks for externally influenced text: toast title and message,
  the ConfirmDialog inline error, ErrorPanel, EmptyState, and ConfirmDialog
  messages that interpolate hostnames.
- New modal infrastructure: a portal to `document.body`, a module-level stack
  registry, a keyboard focus trap, initial-focus placement, a backdrop-dismiss
  rule and a pending lock. This now gates every destructive action in the
  migrated flows.
- One new localStorage read and write (`riot-time-zone`).
- A typed `ApiError` and a new device-query retry policy
  (`retryUnlessNotFound`).
- Demo clients: about 21 new stub methods, plus a type and key-set parity
  contract with the real clients.
- **No new network surface**, no new secret and no new dependency.

---

## Findings

### CRITICAL
None.

### HIGH
None.

### MEDIUM

#### SEC-001: Primary-variant ConfirmDialog puts initial focus on Confirm, so the gesture that opens a fleet-wide action can also confirm it
**Severity:** MEDIUM
**Domain:** Business Logic Security (confirmation integrity / UI redress)
**Location:** ADD AD-004 (ConfirmDialog initial focus), Section 3a **DEV-11**,
Flow 7.2 (FleetOverview bulk agent update), FR-059 (AgentManagement "Update
all"), FR-074 / Flow 7.4 (DeviceDetail Patch and Update Agent, both primary).
Also PatchReviewModal (Flow 7.3), whose initial focus is not specified.
**Description:**
DEV-11 sends initial focus to the **Confirm** button for every `primary`
ConfirmDialog. Today's `ConfirmModal` (`web/src/components/ConfirmModal.tsx`)
sets no focus at all, so focus stays on the trigger and the problem described
below does not exist yet. The new focus rule introduces it. The `primary`
variant is not low-risk here. BR-001 classes updates and patches as
"non-destructive", but the primary dialogs in this story include the two
highest-blast-radius operations in the product: a **fleet-wide agent update**
(FleetOverview, AgentManagement) and **OS package upgrades** (DeviceDetail
Patch, `mode: full`). The in-flight ref guard (FR-029) blocks a second
invocation *within* a dialog. It does nothing against the opening gesture
itself confirming the dialog.
**Attack Scenario (accidental, but concrete):**
1. Ryan tabs to FleetOverview "Update all" and presses Enter, or holds Enter a
   moment too long. On a `<button>`, a repeating Enter keydown fires repeated
   `click` events in Chromium and Firefox.
2. The first click opens the ConfirmDialog. Its layout effect moves focus to
   **Confirm** before the next auto-repeat keydown.
3. The next repeated keydown activates Confirm. `bulkUpdateAgents` dispatches
   to every outdated agent in the fleet without Ryan ever reading the dialog.
   The same sequence applies to a quick double Enter, and to DeviceDetail
   "Patch", which runs a full OS upgrade on a production host.
**Required Resolution:**
The property that must hold: **a ConfirmDialog (and PatchReviewModal) must not
be confirmable by the same keyboard or pointer gesture that opened it,
including key auto-repeat and double activation.** At minimum, for every
confirmation that dispatches to more than one device (FleetOverview bulk
update, AgentManagement update-all, PatchReviewModal patch-all) and for
OS-modifying single-device commands (DeviceDetail Patch), initial focus must
not be on the confirm control. The architect amends DEV-11 (and states
PatchReviewModal's initial focus target explicitly). QA must verify the
property with a held or repeated Enter test against at least the FleetOverview
bulk-update dialog.
**Blocks:** Does not block implementation. Must be resolved before QA sign-off.
Because it is a one-line DEV-11 change, it should be folded in during
implementation.

#### SEC-002: The device-delete dialog's stated consequence and the `uninstall` flag actually sent come from different snapshots
**Severity:** MEDIUM
**Domain:** Business Logic Security (TOCTOU between consent and action)
**Location:** `web/src/pages/DeviceDetail.tsx:146-147` (`mutationFn: () =>
api.deleteDevice(id!, data?.device.status === 'online')`),
`DeviceDetail.tsx:552-565` (message chosen from the live `device.status`),
ADD Flow 7.4 (Delete: `onConfirm` is `deleteMutation.mutateAsync()`), FR-031/FR-032
(retry after failure).
**Description:**
The delete flow has two different meanings. For an online device it **remotely
uninstalls the agent** (`?uninstall=true`). For an offline device it only
removes the record. The dialog message is rendered from the *current* render's
`device.status`. The `uninstall` flag is recomputed from the *current* query
data at the moment `mutationFn` runs, and it is recomputed again on every
retry. Device status updates live (WebSocket invalidation plus refetch), so the
text Ryan read and the flag that is sent can diverge. The issue is
pre-existing, but this story widens the window. The dialog now stays open
through Pending and through an Error, then Retry cycle (FR-031/FR-032), and the
ADD wires the delete straight to `mutateAsync()` with no snapshot.
**Attack Scenario:**
1. Device `nas-01` is offline. Ryan opens Delete. The message reads "The device
   is offline, you may need to manually uninstall the agent." He intends to
   drop only the record.
2. The first attempt fails (HTTP 500), and the dialog shows the inline error.
   Meanwhile `nas-01` reconnects. A flapping link is enough, as is a
   compromised agent that times its reconnection deliberately.
3. Ryan clicks Delete again to retry. `mutationFn` now reads `status ===
   'online'` and sends `?uninstall=true`. The agent is remotely uninstalled,
   which Ryan never agreed to. In the reverse case, a device that goes offline
   mid-dialog is deleted without the uninstall Ryan was told would happen,
   which leaves an installed agent behind.
   The message text can also flip under the cursor while the dialog is open,
   so the consequence Ryan read is not the one he confirms.
**Required Resolution:**
The property that must hold: **the destructive scope sent to the server
(`uninstall` true or false) must be exactly the scope stated in the dialog the
user confirmed. It is fixed once when the dialog opens, and it must not change
through Pending, Error or Retry.** If the scope would differ from what the
dialog states, the user must re-confirm against the new statement rather than
having the request silently change meaning. The architect states this
explicitly in Flow 7.4. QA must verify it with a test that flips
`device.status` between open and confirm, and between failure and retry, and
asserts the `deleteDevice` second argument matches the message shown.
**Blocks:** Does not block implementation. Must be resolved before QA sign-off.

---

### LOW / INFORMATIONAL

#### SEC-003: A failure that settles after the ConfirmDialog has unmounted is reported nowhere
**Severity:** LOW
**Domain:** Business Logic Security (truthfulness of destructive-action feedback)
**Location:** ADD AD-004 ("State setters are guarded by a mounted ref because
the parent may unmount it"), Flow 7.1 step 4, BR-002.
**Description:**
The pending lock blocks Escape, the backdrop and Cancel. It does not block
browser-level navigation: Back, Alt+Left, a hash change in the demo, or the
page swapping itself for `DeviceQueryFallback` when a background refetch
returns 404. In every case the dialog unmounts while the request is still in
flight. Per the ADD, a rejection after unmount is swallowed by the mounted-ref
guard, and only the inline error exists to report it. BR-002 ("each action
failure is reported to the user exactly once") is then violated.
**Attack Scenario:**
Ryan confirms Reboot on `hawaii`, then presses Alt+Left while "Working…" is
showing. The command request fails. No inline error renders, because the dialog
is gone, and no toast is raised. Ryan believes the reboot was dispatched.
**Required Resolution:**
A rejection that settles after the dialog has unmounted must still be reported
exactly once, as an error toast. The ToastProvider sits above `<Routes>`, so
this is possible without architectural change. QA must verify by unmounting a
pending dialog and then rejecting the action.
**Blocks:** Does not block implementation.

#### SEC-004: Un-migrated `z-[60]` overlays sit above the new modal layer (50)
**Severity:** LOW
**Domain:** UI redress / layering
**Location:** ADD AD-001 (z-scale: modal 50), `components/SecurityScoreModal.tsx:187`
and `components/SetupGuide.tsx:271` (both `z-[60]`, deferred by DEV-02 and
Section 12.7).
**Description:**
`ConfirmModal` and `UpdateConfirmModal` currently sit at `z-[60]` and move down
to the new modal layer (50). The two remaining legacy `z-[60]` overlays then
paint **above** any new Modal or ConfirmDialog. If a ConfirmDialog is ever
opened while one of them is open, the dialog renders underneath, and the
legacy overlay's buttons stay clickable on top of the confirmation backdrop.
That is a misclick vector on a destructive dialog. Clod verified that no such
path exists today. `SecurityScoreModal`'s FixButton uses its own inline
two-step confirm (`SecurityScoreModal.tsx:66-98`) and does not open a Modal, and
SetupGuide opens no confirmation.
**Attack Scenario:**
A future change has SecurityScoreModal's remediation open a ConfirmDialog. The
dialog renders beneath the z-60 overlay, and Ryan clicks a visible "Fix" control
believing he is interacting with the confirmation.
**Required Resolution:**
No new Modal or ConfirmDialog may be opened from within an un-migrated
`z-[60]` overlay in this story. QA must confirm this. The deferred migration of
those two overlays must bring them onto the token scale.
**Blocks:** Does not block implementation.

#### SEC-005: Externally influenced text in toasts, dialogs and error panels must stay text-only and must not displace dialog controls
**Severity:** INFORMATIONAL
**Domain:** Input Validation / XSS / content spoofing
**Location:** ADD Section 10 "Injection", AD-006 (Toast), AD-004 (inline
error), AD-005 (`errorMessage`), Section 6.2 (ConfirmDialog `message:
ReactNode`, toast `title`/`message: string`), FR-066 and FR-075 (hostname in
toasts).
**Description:**
The new sinks render strings that a compromised agent or upstream can
influence. Examples are agent-reported `hostname`, the `sendCommand` /
`getDeviceSummary` `body.error` text and `statusText`. A repo-wide check found
no `dangerouslySetInnerHTML` or `innerHTML` assignment in non-test `web/src`
(the only matches are a test assertion in `Probes.test.tsx:251-253` and a test
read in `ContainerLeaderboard.test.tsx:196`). The ADD commits to React text
rendering, and the toast API is typed `string`, so markup injection is not
viable as designed. `getDeviceSummary` markdown is only downloaded as a Blob or
copied to the clipboard. It is never rendered. The residual risks are
non-executing:
(a) A future contributor could pass a server string into a raw-HTML sink.
(b) A compromised agent can set a very long or unbroken hostname. That text
then appears inside the **danger** delete, reboot and shutdown dialog messages
and the toasts, and it could push the Cancel and Confirm footer or the toast
"Dismiss notification" button out of view or reach.
(c) Text such as `web-01" removed. Press Delete to cancel` reads as spoofed
instructions. Quoting is the only mitigation, and it is acceptable at this
trust level.
**Attack Scenario:**
A compromised agent reports a 5 KB unbroken hostname. When Ryan opens Reboot
for that device, the message overflows the `max-w-sm` panel and the footer
buttons are no longer visible. The focus trap still allows Tab to reach Cancel,
but a pointer user cannot find it.
**Required Resolution (QA / engineering verification conditions):**
1. Add a static guard to `test/static-guards.test.ts` that forbids
   `dangerouslySetInnerHTML` and `.innerHTML =` in non-test `web/src`. This
   makes the existing convention enforced rather than implicit.
2. Toast title and message, the ConfirmDialog message and inline error, and the
   ErrorPanel message must wrap long unbroken strings. The ConfirmDialog footer
   (Cancel and Confirm) and the toast Dismiss button must stay visible and
   reachable whatever the message length. QA verifies this with a
   multi-kilobyte unbroken hostname.
**Blocks:** Does not block implementation.

#### SEC-006: The time-zone preference read is a whitelist and must never reach `Intl` as a zone identifier
**Severity:** INFORMATIONAL
**Domain:** Input Validation (client storage)
**Location:** ADD AD-007 (`riot-time-zone`, "anything other than exactly
`'Local'` resolves to `'UTC'`"), Section 5, Section 6.4 (`localTimeZone` prop).
**Description:**
The design is correct. The read is in try/catch, the value is an exact-match
whitelist, the write is in try/catch with failures swallowed, and the stored
value only selects between two enum branches. The one way this could go wrong
is if the stored string, rather than the enum, were ever passed as
`Intl.DateTimeFormat({ timeZone })`. An invalid zone throws `RangeError`, which
would crash every Timestamp render, a persistent self-DoS from a tampered
storage entry. The `localTimeZone` prop is a test seam that production never
sets (DEV-05). The value is not sensitive, so tampering gives no privilege.
**Attack Scenario:**
Someone with profile access sets `riot-time-zone` to `America/Nowhere`. With the
whitelist as designed, the result is UTC and nothing else happens. Without it,
every page that shows a timestamp throws on render until storage is cleared.
**Required Resolution (QA verification conditions):**
QA verifies that `"banana"`, `"America/Nowhere"`, `"local"` (wrong case), JSON,
a 100 KB string, and a throwing `getItem` all resolve to UTC with no thrown
error. QA also verifies that the stored value never flows into an `Intl` option
and that `localTimeZone` is not wired from storage, URL or any runtime input in
production.
**Blocks:** Does not block implementation.

#### SEC-007: `ApiError` must leave the 401 path and error disclosure exactly as they are
**Severity:** INFORMATIONAL
**Domain:** Authentication (client) / Information Leakage
**Location:** ADD AD-005, Section 6.1 thrown-error table; `web/src/api/client.ts`
and `web/src/api/settings.ts` (every `res.status === 401` branch).
**Description:**
The design keeps every 401 branch byte-identical. Each one still calls
`window.location.reload()` and throws a plain `Error('Unauthorized')`, not an
`ApiError`. As a result, `isNotFound` is false for 401, and a 401 can never
render "Device not found". Several client mutations have **no** 401 branch
today: probe and device-probe create, update, delete and run
(`client.ts:140-172`, `client.ts:325-362`). After this change they raise
`ApiError(401, 'HTTP 401')`. That is behavior-equivalent to today's `Error('HTTP
401')`, and FR-080 requires that these methods neither gain nor lose a reload as
a side effect. `errorMessage()` surfaces only `.message`, never a response body
or stack, and no new server text is displayed beyond what is shown today.
**Attack Scenario:**
None as designed. A regression that converted a 401 branch into `ApiError`
would drop the reload-to-login and leave a stale authenticated-looking UI that
surfaces "HTTP 401" errors.
**Required Resolution (QA verification conditions):**
AC-089 must assert both the reload and the plain `Error('Unauthorized')`
(`not.toBeInstanceOf(ApiError)`) for `fetchJSON` and settings `mutate`. QA
reviews the diff of `client.ts` and `settings.ts` to confirm that every 401
block is untouched and that no other method's 401 handling changed.
**Blocks:** Does not block implementation.

#### SEC-008: Demo stubs must remain unreachable from the production bundle
**Severity:** INFORMATIONAL
**Domain:** Infrastructure & Configuration (build isolation)
**Location:** `web/vite.config.ts` (alias only when `VITE_DEMO === 'true'`;
unchanged per ADD Section 4), ADD AD-010, AC-131, AC-144.
**Description:**
The alias block exists only when `isDemo` is true. Clod verified this in
`vite.config.ts`. `demo-data` is imported only by `demo-client.ts` and
`demo-settings.ts`, and no non-test, non-demo module imports `api/demo-*`.
`demo-client.ts` uses `import type` from `./client`, which is erased. The new
shared modules (`api/errors.ts`, `hooks/featureCatalog.ts`) contain no demo
logic. The risk is a future import such as a page importing
`api/demo-data` for a fixture. That would ship fake "always succeeds" stubs in
production, or worse, let a code path resolve against the demo client, so that
destructive actions appear to succeed while doing nothing.
`demo-parity.test.ts` deliberately imports both client sets, which is
acceptable because it is test-only. Separately, `hooks/useAuth.ts` has
pre-existing demo auth gated by the compile-time constant `import.meta.env.VITE_DEMO`.
The server enforces authentication regardless, so this is out of scope here.
**Attack Scenario:**
A later change imports `demo-data` from a page. The production bundle then
contains a demo code path, and with any mis-wiring a delete "succeeds" against
an in-memory stub while the real device stays.
**Required Resolution (QA / engineering verification conditions):**
1. Add a static guard: no non-test module outside `api/demo-*.ts` and
   `contexts/DemoWebSocketProvider.tsx` may import `demo-client`,
   `demo-settings`, `demo-data` or `DemoWebSocketProvider`.
2. After `npm run build` (production), grep `dist/assets` for demo-only fixture
   markers, for example `proxmox-01` and `Demo mode: certificate not
   regenerated`. Record that the result is empty in the impl report.
3. AC-131 (demo mutating methods never call `fetch`) must cover every
   mutating demo method, including the new ones.
**Blocks:** Does not block implementation.

#### SEC-009: Clickjacking posture (framing) is unchanged and already neutralized by the cookie policy
**Severity:** INFORMATIONAL
**Domain:** Infrastructure & Configuration
**Location:** `internal/server/handlers/auth.go:43,59`, `oidc.go:101,118`
(`riot_session` is `SameSite=Lax`). There is no `X-Frame-Options` or
`frame-ancestors` in `internal/server`.
**Description:**
The new danger dialogs make a classic framed-clickjacking attack worth
checking: a hostile page iframes the dashboard and overlays a decoy over Delete
or Reboot. The server sends no anti-framing header. However, the session cookie
is `SameSite=Lax`, so a cross-site iframe loads the dashboard
**unauthenticated**. It renders the login screen, and the destructive controls
are unreachable. Within the dashboard itself, the full-viewport modal backdrop
blocks pointer access to background controls, and the toast layer that sits
above it (z 80) has no action buttons, only Dismiss. The modal layer therefore
introduces no in-page redress vector beyond SEC-001 and SEC-004.
**Attack Scenario:**
None viable today, because the framed session is unauthenticated.
**Required Resolution:**
No action in this story (frontend-only, no backend change). Adding
`frame-ancestors 'none'` / `X-Frame-Options: DENY` is a reasonable future
server-hardening item and is recorded for awareness only.
**Blocks:** Does not block implementation.

---

## Positive Observations
- **The pending lock is designed correctly.** Both buttons are disabled,
  Escape and the backdrop are ignored, the close X is hidden or disabled, and a
  synchronous `useRef` in-flight guard blocks re-entry before React re-renders
  (FR-028/FR-029, AD-004). This closes the existing bug where
  `mutate(); setShowUpdateConfirm(false)` hid the in-flight state and allowed
  reopening and re-dispatch.
- **Danger dialogs focus Cancel** (FR-033), so a stray Enter on a delete,
  reboot or shutdown dialog is harmless.
- **The backdrop dismisses only when both `mousedown` and `click` hit the
  backdrop.** A drag that ends outside the panel cannot dismiss, or
  mis-dismiss, a dialog.
- **The stack-aware Escape** (a module registry acting only on the top modal)
  avoids listener-order races across portals.
- **Every destructive and fleet-wide flow keeps an explicit confirmation:**
  device delete with uninstall wording, reboot, shutdown, bulk agent update,
  patch-all (PatchReviewModal with a pending confirm), AgentManagement
  update-all, and every probe, rule, channel and log-purge delete. The native
  `confirm()` calls are replaced with equal or stronger confirmation, and none
  is removed. A static guard (AC-063) prevents regressions.
- **Error feedback persists.** Error toasts never auto-dismiss (BR-003), and
  bulk-update and patch-all failures are no longer silent. This is a real
  integrity improvement over the swallowed `catch {}` at
  `FleetOverview.tsx:83-91`.
- **`ApiError` lives in a non-aliased module** with the 401 branches explicitly
  frozen. This keeps auth behavior stable in both builds and under partial
  `vi.mock` factories.
- **Demo parity is enforced by types plus a key-set test**, and demo mutations
  are asserted never to call `fetch` (AC-131). The public demo cannot issue
  requests.
- **Text-only rendering** with a typed `string` toast API, and no new
  dependency. This eliminates supply-chain risk for a story that adds a focus
  trap and toast system, two areas where headless libraries are common.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.** There are no CRITICAL and no HIGH findings. The
ADD's security design is sound, and Section 10 addresses the right concerns.
There are two MEDIUM findings. Both have narrow, non-architectural resolutions
(one DEV-11 sentence, and one snapshot rule in Flow 7.4). They should be folded
in during implementation, and they must be verified before QA sign-off.

Conditions for engineering, with QA verification:
1. **SEC-001:** For bulk and fleet-wide confirmations (FleetOverview bulk
   update, AgentManagement update-all, PatchReviewModal) and DeviceDetail
   Patch, the confirm control must not be activatable by the opening gesture,
   and initial focus must not be on Confirm. Amend DEV-11, and add a held or
   repeated Enter test.
2. **SEC-002:** The device-delete `uninstall` scope is fixed when the dialog
   opens. It must equal the consequence stated in the message, and it must not
   change across Pending, Error or Retry. Add a status-flip test.
3. **SEC-003:** A rejection that settles after the dialog unmounts is still
   reported once, as an error toast.
4. **SEC-004:** No Modal or ConfirmDialog opens from a legacy `z-[60]` overlay.
5. **SEC-005:** A static guard bans `dangerouslySetInnerHTML` and
   `.innerHTML =`. Long unbroken hostnames and error text wrap, and the dialog
   footer and toast Dismiss button stay reachable.
6. **SEC-006:** A tampered `riot-time-zone` resolves to UTC without throwing,
   and it never reaches `Intl` as a zone.
7. **SEC-007:** The 401 branches are byte-identical and still throw a plain
   `Error('Unauthorized')`. AC-089 asserts that it is not an `ApiError`.
8. **SEC-008:** A static guard on demo-module imports, plus a production
   `dist` grep for demo fixture markers, recorded in the impl report.

SEC-009 is awareness only and needs no action in this story.

**Routing:** Invoke `senior_dev`, passing this security report alongside the
ADD. The architect may optionally amend DEV-11 and Flow 7.4 to match
conditions 1 and 2. The amendment does not block the start of implementation.
