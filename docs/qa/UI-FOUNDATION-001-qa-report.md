# QA Report

**Story ID:** UI-FOUNDATION-001
**Title:** UI foundation: design tokens, shared primitives, honest states, canonical time/format
**QA Engineer:** QA Agent
**Date:** 2026-10-18
**Verdict:** PASS WITH NOTES

---

## Test Run Summary

Frontend-only story (`web/` plus `docs/`). No Go file changed (verified, see AC-145), so `go test` was not part of this run.

| Check | Result |
|-------|--------|
| `npx tsc --noEmit` (AC-140) | exit 0, no output (run before and after QA additions) |
| `npm run test:run` as delivered (AC-141) | 56 files, **1070 passed**, 0 failed, 0 skipped (baseline 405) |
| `npm run test:run` after QA additions | 57 files, **1153 passed**, 0 failed, 0 skipped (3 consecutive full runs, identical) |
| Intentional known-defect tests | 2 `it.fails` (QA-D1, QA-D2), counted in the 1153 as passing |
| `npx vite build` (production) (AC-144) | OK, 826 modules, entry chunk 1,457,700 B raw |
| `VITE_DEMO=true npx vite build` (AC-144) | OK (built to a scratch directory, not into `web/dist-demo`) |
| SEC-008 grep of `web/dist` for `proxmox-01` | **0 files** |
| SEC-008 grep of `web/dist` for `Demo mode: certificate not regenerated` | **0 files** |
| SEC-008 positive control (demo build, `proxmox-01`) | 1 file, so the markers are real |
| Bundle budget (AC-143) | `gzip -6` of the entry chunk is 400,706 B. Baseline 394,204 B is taken from the impl report (QA did not rebuild `main`). Delta +6.5 KB against a 20 KB limit |
| AC-139 `git diff main...HEAD -- web/package.json web/package-lock.json` | empty |
| AC-145 `git diff --name-only main...HEAD` | 128 paths, every one under `web/` or `docs/` |
| Compiled CSS | `z-(--z-modal)`, `z-(--z-toast)`, `z-(--z-tooltip)`, `wrap-anywhere`, `motion-safe`, `animate-toast-in` and the five `--z-*` variables are all present in the production CSS |

Flaky tests: none observed. The `ECONNREFUSED ::1:3000` stderr noise is pre-existing (impl report note) and unrelated.

Visual ACs (006, 052, 124, 133, 135, 138) and the AC-134 console walkthrough were treated as verified by Clod's screenshots and walkthrough in the impl report, as instructed. See the AC-119 and AC-134 notes below for what that evidence does and does not show.

## AC Coverage Audit

Test locations are `[UIF AC-xxx]` describe titles under `web/src/`. "QA" marks coverage added in this review.

| AC ID | Status | Tests Covering It | Gap Description |
|-------|--------|-------------------|-----------------|
| AC-001 | COVERED | `theme/tokens.test.ts` | none |
| AC-002 | COVERED | `theme/tokens.test.ts`; QA: FRD 7.1 palette pinned literally | The original test compared `index.css` with `TOKEN_PALETTE`, which lives in source, so both could drift together. QA added a literal pin. `success` = emerald-500 and `info` = blue-500 are the documented ADD DEV-01 hue-family deviations from green-500 and sky-500 |
| AC-003, 004, 005, 007 | COVERED | `theme/tokens.test.ts`, `test/static-guards.test.ts` | none |
| AC-006 | COVERED (visual) | `after-fleet.png` | per Clod's evidence |
| AC-008 to AC-013 | COVERED | `ui/Button.test`, `ui/Card.test`, `Modal.test`, `Toast.test`, `QueryState.test`, `HeaderClock.test` | AC-012 verified on all five named controls |
| AC-014 to AC-021 | COVERED | `ui/labels.test.ts`, `ui/Badge.test`, `pages/Alerts.test`, `AlertRuleSettings.test`, `ActivityLog.test` | Event types cross-checked against Go: no constant missing from the TS list |
| AC-022 to AC-031 | COVERED | `ui/Modal.test`, `ConfirmDialog.test`, `ConfirmModal.test`, `UpdateConfirmModal.test`, `FleetOverview.test`, `static-guards` | none |
| AC-032, 033, 034 | COVERED | `ui/ConfirmDialog.test`; QA: Escape from `body`, panel and busy button | none |
| AC-035 | COVERED (was PARTIAL) | `ui/ConfirmDialog.test`; QA: two activations in one `act()` | **False coverage found.** All 3 original double-click tests stay green with the `inFlight` guard deleted, because the Confirm button is already disabled when the second click lands. Mutation check: with `if (inFlight.current) return` removed, only the 2 QA tests fail. The guard is now actually tested |
| AC-036 to AC-041 | COVERED | `ui/ConfirmDialog.test` | none |
| AC-042 | COVERED | `pages/FleetOverview.test` | none |
| AC-043 to AC-050 | COVERED | `EmptyState`, `Skeleton`, `QueryState` tests | none |
| AC-051 | COVERED | `ui/Toast.test`; QA: toast raised over an open dialog | QA verifies the toast sits outside the dialog, focus stays on Cancel, Escape closes only the dialog, and Tab stays trapped |
| AC-052 | COVERED (visual) | `after-toast-stack.png` | per Clod's evidence |
| AC-053 to AC-062 | COVERED | `ui/Toast.test` | 4,999 ms and 5,000 ms boundary, pause and resume accounting, eviction, live-region roles all asserted |
| AC-063, 064 | COVERED | `static-guards` | Pattern has positive controls. Does not match `globalThis.confirm(` or `self.confirm(` (low, see Notes) |
| AC-065 to AC-072 | COVERED | `DeviceProbes`, `Probes`, `AgentManagement`, `AlertRuleSettings`, `LogSettings`, `NotificationSettings` tests | none |
| AC-073, 074 | COVERED | `pages/DeviceDetail.flows.test` | none |
| AC-075 | COVERED | `pages/FleetOverview.test` | Text-based, sound |
| AC-076 to AC-080 | COVERED | `pages/FleetOverview.test` | AC-078 satisfied as written. See defect QA-D1 for the unguarded double activation |
| AC-081 | COVERED (was PARTIAL) | `pages/DeviceDetail.flows.test`; QA: exactly-once | The original error-banner check matches the old banner's colour classes (`bg-red-900/30`), so a banner restyled with tokens would pass. QA added a text-based exactly-once assertion for Reboot and Delete failures |
| AC-082 to AC-087 | COVERED | `pages/DeviceDetail.flows.test` | none |
| AC-088, 089 | COVERED | `api/client.test`, `api/errors.test` | `git diff` of `client.ts` and `settings.ts` contains no changed line mentioning 401 or `Unauthorized` |
| AC-090 to AC-093 | COVERED | `pages/FleetOverview.states.test`; QA: paused fetch, failed refetch | The tests run the real `useDevices`. QA added the offline-paused first fetch and the failed background refetch: neither shows the SetupGuide |
| AC-094, 095, 098, 100 | COVERED | `pages/DeviceDetail.flows.test` | See defect QA-D2 (paused query shows "Device not found") |
| AC-096, 097, 099 | COVERED | `pages/DeviceContainers.test`, `ContainerDetailPage.test` | Same shared fallback component as QA-D2 |
| AC-101 to AC-110 | COVERED | `utils/format.test`, `rebootEta.test`, `cron.test`, `ActivityLog.test`, `Alerts.test`, `CertificateSettings.test`, `static-guards` | AC-110 guard is stricter than the AC: it bans every `toLocale*String(` in source |
| AC-111 to AC-117 | COVERED | `RelativeTime.test`, `useMinuteTick.test`, `HeaderClock.test`, `TimePreferenceProvider.test` | none |
| AC-116 | COVERED | `HeaderClock.test`, `TimePreferenceProvider.test`; QA: real `window.localStorage` path | All original tests injected a fake storage, so the production default path (`safeLocalStorage()`) was untested. QA covers tampered values, a throwing `localStorage` getter (blocked storage), and a throwing real `getItem` |
| AC-118 | COVERED (was PARTIAL) | `utils/format.test`; QA: `components/chartTimeWiring.test.tsx` | Only the formatters were tested, not that `MetricChart` and `ContainerMetricChart` pass them to recharts. QA stubs recharts and asserts the real `tickFormatter` and `labelFormatter` props under UTC and Local |
| AC-119 | PARTIAL | QA wiring tests above | The hover screenshot was not captured (impl report says recharts hover is unreliable headless). The tooltip label wiring is now unit-tested; the on-screen rendering is not evidenced |
| AC-120, 121, 122, 123, 125 | COVERED | `static-guards`, `ContainerDetailPage.test`, page tests, `Security.test` | none |
| AC-124 | COVERED (visual) | `after-fleet.png`, `after-device-detail-header.png` | per Clod's evidence |
| AC-126 to AC-132 | COVERED | `contexts/DemoWebSocketProvider.test`, `api/demo-parity.test` | AC-131 enumerates every non-read method and fails if a new mutator has no entry |
| AC-133 | COVERED (visual) | `after-dashboard-demo.png` | per Clod's evidence |
| AC-134 | PARTIAL (evidence) | Clod's walkthrough; QA: every demo reader resolves to a defined value | The walkthrough visited Fleet, Dashboard, one DeviceDetail and Security. The AC also names "each Settings tab", which was not walked. QA added an automated check that all 46 demo readers (`api` and `settingsApi`) resolve to something other than `undefined` (or a typed 404). That is the failure mode TanStack Query logs. All pass |
| AC-135 | COVERED | `pages/Security.test` (grid classes), `after-security.png` | none |
| AC-136, 137 | COVERED | `pages/DeviceDetail.flows.test` | none |
| AC-138 | COVERED (visual) | `after-device-detail-header.png` | per Clod's evidence |
| AC-139, 140, 141, 143, 144, 145 | COVERED | Re-run by QA, see Test Run Summary | AC-143 baseline taken from the impl report |
| AC-142 | COVERED | `Skeleton.test`, `Toast.test` | none |

Zero ACs are MISSING. Three were PARTIAL and are now COVERED (035, 081, 118). Two remain PARTIAL because of missing visual or manual evidence, not missing tests (119, 134).

## Security Conditions (SEC-001 to SEC-008)

| Condition | Verdict | Evidence |
|-----------|---------|----------|
| SEC-001 opening gesture cannot confirm | MET | `ConfirmDialog.test` (both variants never focus Confirm, held `{Enter>4/}`, repeat keydown, double click), `FleetOverview.test` (held Enter on "Update Agents", double click, Cancel focused, PatchReviewModal Cancel focused), `DeviceDetail.flows.test` (Patch, Update Agent, held Enter on Patch), `AgentManagement.test`. The held-Enter requirement is met against the FleetOverview bulk-update dialog. Related gap that SEC-001 does not cover: QA-D1 |
| SEC-002 delete scope fixed at open | MET | 4 tests: online to offline before confirm, offline to online before confirm, flip between failure and retry (both calls send `false`), scope re-read on the next opening. Each asserts the `deleteDevice` second argument against the message shown |
| SEC-003 late rejection reported once | MET | `ConfirmDialog.test` (unmount while pending, then reject: exactly one `role=alert` toast; inline failure raises no toast). QA added the same property on the real DeviceDetail page: Reboot pending, route changes, request rejects, exactly one error toast |
| SEC-004 no Modal opened from a `z-[60]` overlay | MET | Review: `SecurityScoreModal.tsx` imports only `enumLabel` from `./ui`; `SetupGuide.tsx` imports nothing from `./ui`. Neither opens a Modal or ConfirmDialog. No automated guard (the security review asked for confirmation only) |
| SEC-005 text-only sinks, long text wraps | MET (class level) | Static guard bans `dangerouslySetInnerHTML`, `.innerHTML =`, `.outerHTML =`, `insertAdjacentHTML`, with a positive control. 5,000-character unbroken strings are tested in Modal, ConfirmDialog, Toast, EmptyState, QueryState: wrap classes present, footer outside the scrolling body, markup renders as text. `wrap-anywhere` and `break-words` are present in the compiled CSS. happy-dom has no layout engine and no multi-kilobyte-hostname screenshot exists, so footer reachability is asserted structurally, not pixel-verified |
| SEC-006 tampered `riot-time-zone` | MET | `banana`, `America/Nowhere`, `local`, JSON, quoted JSON, 100 KB string, empty string, throwing `getItem` all resolve to UTC without throwing. QA repeated the cases on the real `window.localStorage` path plus a throwing `localStorage` getter. `localTimeZone` is referenced in production only by `TimePreferenceProvider` and `utils/format.ts` and is never wired from storage, URL or other input. The stored value is compared to the literal `'Local'` and never reaches `Intl` |
| SEC-007 401 path untouched | MET | `[UIF AC-089]` asserts reload and plain `Error('Unauthorized')` with `not.toBeInstanceOf(ApiError)` for `fetchJSON`, `sendCommand`, settings `fetchJSON` and settings `mutate`. QA diff review: the `git diff` of `client.ts` and `settings.ts` has no changed line containing `401` or `Unauthorized`; only `throw new Error(...)` lines became `throw new ApiError(...)` |
| SEC-008 demo isolation | MET | Static guard with positive control. Production `dist` grep: 0 and 0. Demo build positive control: 1 file. AC-131 covers every mutating demo method and fails on a new unmapped mutator |

## Test Quality Findings

1. **AC-035 false coverage (fixed).** The three original "no double invoke" tests pass with the synchronous `inFlight` ref guard deleted. They only prove the Confirm button is disabled once React has re-rendered. Mutation-verified. The guard is the SEC-001/FR-029 safeguard and was effectively untested. Two QA tests now fail without it.
2. **AC-081 brittle assertion (strengthened).** The "no error banner" check filters by the old banner's Tailwind colour classes. It would pass for a banner built from the new tokens. Added a text-based exactly-once assertion (also covers BR-002 for Delete).
3. **AC-002 mirror test (strengthened).** The expected palette lives in `theme/tokens.ts`, the same module family as the CSS it checks. Added a literal pin of FRD 7.1.
4. **AC-116 and SEC-006 tested only against injected storage (strengthened).** Production uses `window.localStorage` through `safeLocalStorage()`. Now covered, including a throwing getter.
5. **AC-118 wiring gap (fixed).** Formatter correctness was tested; the chart components' use of the formatters was not.
6. **AC-134 evidence gap.** Settings tabs were not walked. Mitigated by the new reader test; a quick manual pass of the Settings tabs in the demo is still advisable.
7. **Isolation.** No inter-test ordering dependence found. Three full runs were identical. `onlineManager` is restored in `afterEach` in the tests that flip it.
8. **Mocking hygiene.** Page tests mock `../api/client` with partial factories, but the demo-parity suite pins the real and demo method sets together and `tsc` checks the demo client against `typeof api`. No mock drift risk found.
9. **Positive controls.** Every static guard regex has a positive control or was mutation-checked (impl report), which is good practice.

## Adversarial Findings

### QA-D1 (Medium): PatchReviewModal patch-all has no synchronous double-submit guard

`web/src/pages/FleetOverview.tsx:72` (`patchAllMutation`) and `:356` (`onConfirm={() => patchAllMutation.mutate()}`). Patch-all is the highest-blast-radius action in the product (OS upgrade on every device). `ConfirmDialog` blocks re-entry with a synchronous ref; PatchReviewModal is a plain `Modal` and depends on `isPending` reaching React to disable the button. TanStack Query delivers that state through a timer, so two activations queued before it lands (a busy main thread while a large fleet renders, a scripted or very fast double click) both call `bulkPatchDevices`. Reproduced: two clicks inside one `act()` give `bulkPatchDevices` called **2 times**.

Not an FRD violation (AC-078 only requires disabled plus pending label, and FR-029 is scoped to ConfirmDialog), and a human double-click arrives after the re-render, so practical likelihood is low. Test written: `FleetOverview.test.tsx`, `[UIF AC-078] QA: patch-all double activation dispatches once`, as `it.fails` so the suite stays green. Fix: add the same `useRef` in-flight guard (or reuse `ConfirmDialog` with a `children` body), then change `it.fails` to `it`.

### QA-D2 (Medium): device pages say "Device not found" when the query is paused offline

`web/src/components/DeviceQueryFallback.tsx:19`: `isNotFound(error) || (!isLoading && !error)`. When the browser is offline, TanStack Query pauses the fetch: no request is made, `error` is null and `isLoading` is false (`isLoading` is `isFetching && isPending`). The fallback then renders "Device not found. This device no longer exists, or the link is wrong." Reproduced on DeviceDetail: `api.getDevice` called 0 times, "Device not found" shown. DeviceContainers and ContainerDetailPage use the same component. FleetOverview is not affected (QA test confirms it keeps its loading skeleton and never shows the SetupGuide).

FR-084 reserves "Device not found" for HTTP 404, and BR-004 sets the same honesty principle for the fleet. The page recovers by itself when the connection returns, which is why this is Medium and not a FAIL. Test written: `DeviceDetail.flows.test.tsx`, `[UIF AC-095] QA: an offline browser is not "Device not found"`, as `it.fails`. Fix: treat a pending query that is not fetching as loading, for example pass `isPending` (or `fetchStatus`) and show the Skeleton or an "offline" error panel, keeping "not found" for the 404 and the missing-route-parameter cases.

### Documentation-only findings (no test)

- **Toasts and modals (a11y).** A toast sits outside the `aria-modal` dialog, and the focus trap keeps Tab inside the dialog. While any modal is open, a keyboard user cannot reach a toast's Dismiss button, and a screen reader that honours `aria-modal` may not announce a toast raised meanwhile. Relevant case: the patch-all failure toast (AC-080) is raised while PatchReviewModal stays open. Pointer users are unaffected (toast layer is above the modal and the region is `pointer-events-none` except toasts). Behaviour is by design per the FRD; consider an inline error in PatchReviewModal in a follow-up.
- **Empty `statusText` over HTTP/2.** Messages are built as `HTTP ${status}: ${statusText}` (FR-079 pins the format). Browsers report `statusText === ''` on HTTP/2, which a TLS reverse proxy usually negotiates, so users will see `HTTP 500: ` with a trailing colon. Cosmetic; the format predates this story but the new error panels make it prominent.
- **Retry feedback.** After a failed device or fleet load, clicking Retry gives no pending indicator: the query status stays `error` with `isLoading` false until it settles. Double clicks are harmless.
- **Bulk agent update partial failure** (`FleetOverview.tsx:57-66`). The mutation loops over versions. If version 1 is dispatched and version 2 throws, the dialog shows only the error and Retry re-sends version 1. No count of already-dispatched agents is shown.
- **`onSuccess` that throws** inside `ConfirmDialog` (`:100`) becomes an unhandled rejection in `void handleConfirm()`, leaving the dialog open with no message. No current caller throws; hypothetical.
- **Patch-detail fetch failure** renders "No pending updates on any device." (`handleOpenPatchReview`). This is the known OQ-001 item, out of scope. Confirm is disabled in that state, so no harm.
- **`formatBytes(1048575)`** gives `1024.0 KB` (rounding at the unit boundary). FR-089 pins the existing output, so by design.
- **AC-063 guard** does not match `globalThis.confirm(` or `self.confirm(`. Low; nothing uses them.
- **Working tree line endings.** `core.autocrlf=true` on this checkout: git reports LF to CRLF warnings on every touched file. Harmless, noted for awareness.

Probed and found sound: ConfirmDialog double-submit through the disabled button and through the ref, Escape while pending (document, panel, button, after a settled failure), backdrop drag handling, stacked Escape, focus restore when the opener is gone, toast eviction with errors, tampered time-zone storage (incl. 100 KB values), delete scope flips, 401 branches, 404 vs 500 vs `TypeError` on device pages, empty fleet vs failed fetch vs failed refetch with stale data vs offline-paused fetch, hostnames as text, long unbroken text, demo mutations never calling `fetch`.

## Tests Added by QA

Commit `0b32bb6`. 83 new tests (1070 to 1153): 81 plain, 2 `it.fails` documenting QA-D1 and QA-D2.

| File | Tests | Covers |
|------|-------|--------|
| `web/src/components/ui/ConfirmDialog.test.tsx` | 7 | AC-035 (ref guard under double activation, guard released after failure), AC-033 (Escape from body, panel, busy button; Escape works after a settled failure), AC-051 and AC-055 (toast over an open dialog: stays outside, focus unchanged, Escape closes only the dialog, Tab trapped) |
| `web/src/contexts/TimePreferenceProvider.test.tsx` | 8 | AC-116, SEC-006 on the real `window.localStorage` path (5 tampered values, Local restore and write-back, throwing getter, throwing `getItem`) |
| `web/src/pages/DeviceDetail.flows.test.tsx` | 4 | AC-084, AC-073, BR-002 (failure shown exactly once for Reboot and Delete), SEC-003 on the real page (route change while pending, then reject: one error toast), AC-095 offline pause (`it.fails`, QA-D2) |
| `web/src/pages/FleetOverview.states.test.tsx` | 2 | AC-092, AC-093, BR-004 (offline-paused first fetch is not the SetupGuide; failed background refetch keeps stale rows) |
| `web/src/pages/FleetOverview.test.tsx` | 1 | AC-078 double activation of patch-all (`it.fails`, QA-D1) |
| `web/src/theme/tokens.test.ts` | 10 | AC-002 (FRD 7.1 palette pinned literally) |
| `web/src/components/chartTimeWiring.test.tsx` (new) | 4 | AC-118 and AC-119 wiring (MetricChart and ContainerMetricChart hand the zone-aware formatters to recharts, UTC and Local) |
| `web/src/api/demo-parity.test.ts` | 47 | AC-134 automated half (every demo reader resolves to a defined value or a typed 404) |

All carry `// Added by QA Engineer` and an AC reference. No production file was changed. The only production-file edit during the review was a temporary mutation of `ConfirmDialog.tsx` to prove AC-035 false coverage, restored byte for byte from a backup (the file does not appear in `git status`).

## Deviations from ADD

- DEV-11 (ADD: Confirm focus for primary dialogs) is superseded by SEC-001: every dialog focuses Cancel. Expected, and recorded in the impl report.
- ADD Flow 7.4 (`mutateAsync()` with live status for delete) is superseded by the `deleteScope` snapshot (SEC-002). Expected.
- The impl report lists the other deviations (items 3 to 13). QA found no unrecorded deviation. Item 10 (`DeviceQueryFallback` also shows "not found" for a settled query with neither data nor error) is the origin of QA-D2: the condition `!isLoading && !error` is true for a paused query as well as a settled one.

## Deviations from FRD

- None that fail an AC. AC-002 relies on the documented ADD DEV-01 hue-family deviation (success emerald, info blue).
- QA-D2 is in tension with FR-084 ("Device not found" for a 404) in one environment (browser offline). QA-D1 is in tension with the intent of FR-029 for a fleet-wide action, though FR-029 names only ConfirmDialog.

## Verdict Rationale

**PASS WITH NOTES.** All 145 ACs are covered or evidenced; none are MISSING. tsc is clean, all 1153 tests are green and stable across three runs, both builds compile, the production bundle contains no demo markers, and all eight SEC conditions are met with tests that were checked to fail when the property is broken (AC-035 was found to lack that property and was fixed). The two Medium defects (QA-D1, QA-D2) are real, reproduced and captured as `it.fails` tests, but neither breaks an AC as written, neither is exploitable beyond a low-probability duplicate dispatch or a misleading message that clears when the connection returns, and each has a small fix. They should be fixed before tagging. They are not a reason to block the review pipeline.

Non-blocking follow-ups: walk the demo Settings tabs once by hand (AC-134), capture the chart tooltip screenshot (AC-119), and decide on the toast-vs-modal accessibility note.

## Action Required

None required for the verdict. Recommended before the release tag, in priority order:

1. `web/src/pages/FleetOverview.tsx:72` and `:356`: add a synchronous in-flight guard to the patch-all confirm (same `useRef` pattern as `ConfirmDialog.tsx:70-74`), then change the `it.fails` in `FleetOverview.test.tsx` (`QA-D1`) to `it`.
2. `web/src/components/DeviceQueryFallback.tsx:19`: do not report "Device not found" for a paused or pending query. Pass `isPending` (or `fetchStatus`) from the three device pages and show the skeleton or an offline error panel. Keep "not found" for a 404 and for a missing route parameter. Then change the `it.fails` in `DeviceDetail.flows.test.tsx` (`QA-D2`) to `it`.
3. Optional: trim the trailing colon in `HTTP <status>: ` when `statusText` is empty (needs an FRD wording decision for FR-079 and AC-088).
