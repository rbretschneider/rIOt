# Implementation Report (web half)

**Story ID:** TIME-12H-001
**Engineer:** Senior Dev Agent (web / dashboard)
**Date:** 2026-10-27
**Scope:** every web file in ADD Section 4, plus the web tests, the static guards and the formatter docs. The Go half is a separate report.

## Detected stack

| Item | Value |
|------|-------|
| Language / framework | TypeScript 5.9, React 19.2, react-router-dom 7, TanStack Query 5, Tailwind CSS 4 |
| Build | Vite 6 (`npm run build` = `tsc && vite build`; demo build with `VITE_DEMO=true`) |
| Test runner | Vitest 4 with happy-dom, Testing Library (`@testing-library/react` 16, `user-event` 14) |
| Positioning | `@floating-ui/react` (already a dependency; same middleware as `PatchBadge`) |
| Linter | None. `web/package.json` has no `lint` script and there is no ESLint config, so `tsc --noEmit` is the static check |
| Dependencies | `web/package.json` and the lockfile are unchanged (AC-073) |

## Completed components

| File | Action | Notes |
|------|--------|-------|
| `web/src/utils/format.ts` | MODIFY | `Server` zone, required `hourCycle`, `clock` precision, `acceptServerZone` (the only `as AcceptedZone`), `AcceptedZone` brand, `ZONE_NAME_PATTERN`, `formatDateOnly`, `localZoneName`, `formatterCacheSize`; 12-hour text derived from the `h23` hour; formatter cache capped at 12 with oldest-first eviction. `formatUtc`, `formatRelative` unchanged |
| `web/src/utils/cron.ts` | MODIFY | `parseSystemdCalendar(calendar, hourCycle = '24h')`, `clockText`, first-letter-only lower-casing, hour > 23 keeps 24-hour text, `formatRunDate` default opts gain `hourCycle: '24h'` |
| `web/src/contexts/TimePreferenceProvider.tsx` | MODIFY | Zone (`Server`/`UTC`/`Local`) and hour cycle; keys `riot-time-zone` and `riot-hour-cycle`; the cycle key marks post-story values (legacy `UTC` becomes `Server`, legacy `Local` stays); zone key written before the cycle key and the cycle is not written if the zone write throws; `serverZone` state, `setServerZone(raw: unknown)`, `serverZone` test-seam prop, memoised `localZoneName`; `toggleZone` removed; no-provider default stays UTC / 24h |
| `web/src/components/HeaderClock.tsx` | MODIFY | Button with `aria-expanded`/`aria-controls` and the FR-046 name; non-modal panel with native radio groups "Time zone" and "Clock"; Escape (focus returns to the button), outside `pointerdown`, blur out of the root and second activation close it; selecting never closes; focus moves to the checked zone radio on open; floating-ui `fixed` strategy with `flip`/`shift`/`size`; `overflow-y-auto scrollbar-thin`, `max-w-[calc(100vw-16px)]`, tokens only |
| `web/src/components/ServerZoneSync.tsx` | CREATE | `useQuery(['time-display'])` with `staleTime`/`gcTime` `Infinity`, `retry: 1`; pushes the raw value into `setServerZone` |
| `web/src/App.tsx` | MODIFY | `<ServerZoneSync />` next to `<GlobalWSHandler />`, in the authenticated branch only |
| `web/src/components/ActivityLabel.tsx`, `AgentUpdatingVersion.tsx` | MODIFY | Clock times via `useTimeFormatters().timestamp(iso, 'clock')`; `formatChartTick` no longer imported |
| `web/src/pages/DeviceDetail.tsx` | MODIFY | `parseSystemdCalendar(timer.calendar, zoneOpts.hourCycle)`; download filename date from `formatDateOnly(now(), { ...zoneOpts, zone: 'Server' })` (no `new Date()`) |
| `web/src/api/client.ts` | MODIFY | `TimeDisplayInfo`, `api.getTimeDisplay` (`GET /api/v1/server/time-display`) |
| `web/src/api/demo-client.ts` | MODIFY | `getTimeDisplay` returns `America/New_York` |
| `web/src/api/demo-data.ts` | MODIFY | patch badge `as_of` = `2026-10-18 10:00 PM EDT (just now)` |
| `web/src/components/ui/README.md` | MODIFY | 12-hour table, `clock`, `formatDateOnly`, `acceptServerZone` and the brand rule, preference paragraph (three zones, hour cycle, two keys, legacy mapping, panel), new guard rows, `renderWithProviders` options |
| `web/src/test/renderWithProviders.tsx` | MODIFY (not in ADD Section 4, see Deviations) | `timeZone.serverZone` option; omitted `timeZone.storage` pins UTC / 24-hour |
| `docs/implementation/TIME-12H-001-screenshots/after-header-time-panel.png` | CREATE | AC-031, captured from `VITE_DEMO=true` build served by `vite preview` and driven with headless Chrome |

## Test summary

Baseline on this worktree before any change: 116 files, 2034 tests, all green. After: 125 files, 2282 tests, all green (+9 files, +248 tests). No test was deleted, skipped or loosened.

| AC | Test file | Tests | Status |
|----|-----------|-------|--------|
| AC-001 to AC-015 (formats, midnight/noon, day boundary, DST, ticks, invalid input, relative) | `web/src/utils/format.time12h.test.ts` | 72 (whole file) | PASS |
| AC-005, AC-007, AC-013 (24-hour UIF cases, pinned) | `web/src/utils/format.test.ts` (`[UIF AC-104] [TIME-12H-001 AC-005]`, `[UIF AC-105] [TIME-12H-001 AC-007]`, `[UIF AC-118] [TIME-12H-001 AC-013]`) | 73 (whole file) | PASS |
| AC-013 (chart wiring) | `web/src/components/chartTimeWiring.test.tsx` | 10 (6 new) | PASS |
| AC-015 (component half), AC-016 | `web/src/components/ui/Timestamp.time12h.test.tsx` | 9 | PASS |
| AC-017 (provider), AC-027, AC-028, AC-029, AC-072 | `web/src/contexts/TimePreferenceProvider.time12h.test.tsx` | 26 | PASS |
| AC-018 (shape and Intl acceptance), AC-020 (Intl spy), AC-071 (cache bound) | `web/src/utils/format.time12h.test.ts` | included above | PASS |
| AC-017 (option name), AC-021 to AC-026, AC-030, NFR-008 | `web/src/components/HeaderClock.time12h.test.tsx` | 25 | PASS |
| AC-018 (API path), AC-019, NFR-006 (404, `200 text/html`, network error) | `web/src/components/ServerZoneSync.test.tsx` | 17 | PASS |
| AC-032, AC-033, AC-039 | `web/src/pages/time12h.callsites.test.tsx` | 9 | PASS |
| AC-034, AC-035 | `web/src/components/ActivityLabel.time12h.test.tsx` | 9 | PASS |
| AC-036 (rendered), AC-058 (browser filename) | `web/src/pages/DeviceDetail.time12h.test.tsx` | 8 | PASS |
| AC-036 (`formatRunDate`), AC-037, AC-038, FR-064 | `web/src/utils/cron.test.ts` (additive describes) | 70 (43 new) | PASS |
| AC-040, AC-041 | `web/src/api/demo-time.test.ts` | 4 | PASS |
| AC-020 guard, SEC-003 guard (C-3), AC-036 guard, FR-047, FR-062, BR-005 | `web/src/test/static-guards.test.ts` (additive describes, with self-tests) | 70 (18 new) | PASS |
| AC-031 | screenshot `docs/implementation/TIME-12H-001-screenshots/after-header-time-panel.png` | n/a | captured |
| AC-072 | provider test above | | PASS |
| AC-073 | `git diff -- web/package.json web/package-lock.json` is empty | n/a | PASS |

Existing tests updated deliberately (AD-013), strings unchanged unless noted:
- `utils/format.test.ts`, `utils/cron.test.ts` l.72: `hourCycle: '24h'` added to the zone constants.
- `contexts/TimePreferenceProvider.test.tsx`: seeds `UTC` + `24h`, switches with `setZone`; `[UIF AC-103]` and `[UIF SEC-006]` now expect Server / 12-hour (`2026-10-17 2:03:22 PM UTC` with no server zone); the real-`localStorage` block expects a legacy `Local` to restore as Local 12-hour and a switch to write both keys.
- `components/HeaderClock.test.tsx`: queries `/Time display/`; seeds `UTC` + `24h`; the zone is chosen through the panel radios; the fallback cases expect `2:03:22 PM UTC`.
- `components/ui/Timestamp.test.tsx`, `components/ui/RelativeTime.test.tsx`, `components/chartTimeWiring.test.tsx`: seeded storage as in AD-013.
- `pages/FleetOverview.activity.test.tsx`, `pages/DeviceDetail.activity.test.tsx`: activity labels and titles gain ` UTC` (DEC-9).
- Every `[UIF AC-104]`, `[UIF AC-105]`, `[UIF AC-113]` to `[UIF AC-118]` describe still exists.

### Test run output

New TIME-12H-001 files only (run together):

```
 Test Files  9 passed (9)   [format.time12h 72, TimePreferenceProvider.time12h 26, HeaderClock.time12h 25,
                             ServerZoneSync 17, Timestamp.time12h 9, callsites 9, ActivityLabel.time12h 9,
                             DeviceDetail.time12h 8, demo-time 4]
      Tests  179 passed (179)
```

Full web suite (`cd web && npm run test:run`):

```
 Test Files  125 passed (125)
      Tests  2282 passed (2282)
```

Type check (`cd web && npx tsc --noEmit`): exit 0, no output.

Builds (output redirected to a scratch directory so the worktree stays clean):
- `npx vite build`: built in 5.30 s, exit 0 (after `tsc --noEmit` passed; `npm run build` is `tsc && vite build`).
- `VITE_DEMO=true npx vite build`: built in 5.13 s, exit 0.

Lint: not applicable. There is no lint script or ESLint configuration in `web/`.

## Deviations from the ADD

| # | Deviation | Reason |
|---|-----------|--------|
| D-1 | `web/src/test/renderWithProviders.tsx` is modified although it is not in ADD Section 4. Omitted `timeZone.storage` now pins UTC / 24-hour through a read-only storage; `storage: null` gives the real defaults; `timeZone.serverZone` was added | The helper mounts a real provider with `storage: null`. Under the new default (Server, 12-hour) every page test that renders through it and asserts `... 14:03:22 UTC` (Alerts, CertificateSettings, SelfUpdateDetails, ActivityLog, FleetOverview, DeviceDetail.activity) would break. AD-013 says those tests stay unchanged and treats them as "without a provider", which is not how the helper works. Pinning in one place keeps them unchanged and explicit, and it is what the new call-site tests override. Architect review requested |
| D-2 | The SEC-003 guard ignores test-support modules (`src/test/*`, keyed as `./x` in the glob) for the `localTimeZone` checks | `renderWithProviders.tsx` passes `localTimeZone` as a JSX prop to drive the provider's test seam, which is its purpose. The guard still scans every other production file, and the AcceptedZone cast check has no exemption |
| D-3 | The FRD 7.5 monthly row prints `Monthly on day 1 at midnight` for `*-*-01 00:00:00`, but the unchanged code and the pre-existing test give `day 01` | The day text is echoed verbatim and AC-038 requires the pre-existing tests to pass unchanged. The table-driven AC-037/AC-038 tests use `*-*-1 00:00:00` for that row. `*-*-01` stays covered by the pre-existing case |
| D-4 | The panel root is `role="group"` with `aria-label="Time display"` and `tabIndex={-1}` | The `tabIndex` makes a click on the panel background move focus inside the root, so the blur rule (Tab out closes) does not close the panel on a background click |
| D-5 | `dateTimeFormat` falls back to a UTC formatter if `Intl` rejects a zone | Only reachable through the Local test seam (every API zone is accepted first). It keeps "the formatter never throws" (FR-035) true without a try in callers. Not in the ADD text, harmless |
| D-6 | `ServerZoneSync` stores `data?.display_zone` in a local variable and effects on that value rather than on `data` | Equivalent to the ADD, and the effect no longer re-runs when React Query returns a new `data` object with the same zone |

No ARCH-QUESTION blockers. No blockers file was needed.

## Notes for QA

- AC-031: the screenshot shows the demo build with the panel open, Server (America/New_York) and 12-hour selected, and the clock reading 12-hour time with `EDT`. The demo's "Local" option reads `Local (America/New_York)` because the capture machine's browser zone is New York.
- The static guards key files in `src/test/` as `./name`, not `test/name` (a property of `import.meta.glob` relative to the guard file). The SEC-003 guard's `productionSources` filter excludes both spellings.
- `formatRunDate` and `parseSystemdCalendar` keep default arguments (UTC / 24-hour) so the pre-existing tests stay valid. The AC-036 guard keeps production callers from relying on them. The guard's argument counter is itself self-tested.
- The web docs test on the Go side (AC-069) reads `web/src/components/ui/README.md`. It documents `12-hour`, `24-hour`, `clock`, `1:05 PM EDT`, `formatDateOnly` and `acceptServerZone`.
- The header clock changes width at 9:59:59 to 10:00:00 and at 12:59:59 to 1:00:00 (FRD A-006). No fixed width was added.
- Safari does not focus a button on click, so clicking the clock while the panel is open can close and reopen it. Chrome and Firefox behave as designed. This is the same behaviour as the existing `Popover`.
