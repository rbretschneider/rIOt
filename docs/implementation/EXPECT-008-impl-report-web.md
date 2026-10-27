# Implementation Report (Part W: web dashboard)

- **Story ID:** EXPECT-008
- **Part:** W (ADD Section 4.3). Parts G (Go) and I (installer) are reported separately.
- **Engineer:** Senior Dev Agent (Frontend)
- **Date:** 2026-10-27
- **Detected stack:** React 19 + TypeScript (strict, `tsc --noEmit`), Vite, Tailwind CSS, TanStack Query, react-router 7, vitest 4 with Testing Library. No ESLint or other linter is configured in `web/package.json`; `tsc --noEmit` is the static check.

## Completed Components

| File | Action | Notes |
|---|---|---|
| `web/src/types/models.ts` | MODIFY | `CommandOrigin` adds `'installer'`; `DeviceActivity.origin?: CommandOrigin \| ''` (optional so older servers and fixtures typecheck) |
| `web/src/utils/reinstall.ts` | CREATE | `isReinstall`, `isReinstallActivity`, `REINSTALL_ACTION_LABEL`, `REINSTALL_ACTOR_PHRASE`, `REINSTALL_ACTIVITY_LABEL`, `reinstallOutcomeLabel`, `reinstallOutcomeTone` |
| `web/src/utils/activity.ts` | MODIFY | `Reinstalling agent`; escalated label `Not back 10m after Agent reinstall`; `isAgentUpdating` is false for origin `installer` (fleet keeps the one-line `ActivityLabel`, PR #44) |
| `web/src/components/SelfUpdateDetails.tsx` | MODIFY | Reinstall variant: label, actor, outcome wording; same test IDs; no controls; `scrollbar-thin` unchanged |
| `web/src/components/ActivityLog.tsx` | MODIFY | History row label, actor phrase and outcome badge; read-only modal |
| `web/src/components/docker/CommandProgress.tsx` | MODIFY | Routes reinstall rows to the record view |
| `web/src/components/ui/labels.ts` | MODIFY | `agent_reinstall_unconfirmed`, `agent_reinstall_paused` added to `EVENT_TYPES` and `LABELS.eventType`. No origin label map exists, so none was added |
| `web/src/utils/reinstall.test.ts` | CREATE | WR |
| `web/src/utils/reinstall.safety.test.ts` | CREATE | WZ8 (AC-026) |
| `web/src/utils/activity.test.ts` | MODIFY | Additive `EXPECT-008 [AC-0xx]` cases (WA) |
| `web/src/components/SelfUpdateDetails.reinstall.test.tsx` | CREATE | WS8 (also covers ActivityLog row/modal and CommandProgress routing) |
| `web/src/pages/FleetOverview.reinstall.test.tsx` | CREATE | WF8 |
| `web/src/pages/DeviceDetail.reinstall.test.tsx` | CREATE | WD8 |
| `web/src/components/ui/labels.test.ts` | MODIFY | Event type count 54 to 56 (see Deviations) |

## Test Summary (AC to test)

| AC | Test file | Tests | Status |
|---|---|---|---|
| AC-012 (not-back label names the reinstall) | `utils/activity.test.ts` `EXPECT-008 [AC-012] not-back label names the reinstall`; `pages/FleetOverview.reinstall.test.tsx` `EXPECT-008 [AC-012] ...`; `pages/DeviceDetail.reinstall.test.tsx` `EXPECT-008 [AC-012] ...` | 4 + 1 + 1 | PASS |
| AC-022 (label, count, history, read-only record) | `utils/activity.test.ts` `EXPECT-008 [AC-022] reinstall label, actor line, count and link` (incl. "label clears on activity_update null"); `utils/reinstall.test.ts` `EXPECT-008 [AC-022] isReinstall and outcome labels`; `components/SelfUpdateDetails.reinstall.test.tsx` (two `[AC-022]` describes for the record view and history, one for the link); `pages/FleetOverview.reinstall.test.tsx` (`[AC-022]` fleet row; live follow); `pages/DeviceDetail.reinstall.test.tsx` `[AC-022]` header and record | see per-file counts below | PASS |
| AC-025 (EXPECT-001/002 unchanged) | `utils/activity.test.ts` `EXPECT-008 [AC-025] other agent updates keep their wording`; `pages/FleetOverview.reinstall.test.tsx` `EXPECT-008 [AC-025]`; all existing EXPECT-001/002 web tests run unmodified | 3 + 1 | PASS |
| AC-026 (nothing spawns, no power/agent execution) | `utils/reinstall.safety.test.ts` `EXPECT-008 [AC-026] ...` | 43 (7 sources and the 5 EXPECT-008 test files, each scanned) | PASS |

The other ACs (Go and installer) have no web test in the ADD map. The "fleet and device page show Reinstalling agent" part of AC-008 is covered by the AC-022 web tests.

New web tests: 103 test cases in 6 files (reinstall.test.ts 13, activity.test.ts +16, SelfUpdateDetails.reinstall.test.tsx 14, FleetOverview.reinstall.test.tsx 10, DeviceDetail.reinstall.test.tsx 7, reinstall.safety.test.ts 43), plus 2 label-map rows picked up by the existing `it.each` in `labels.test.ts`. Full web suite: 121 files, 2141 tests, all passing.

### Test Run Output

`cd web && npx tsc --noEmit`: exit 0, no output.

`cd web && npm run test:run` (tail):

```
 Test Files  121 passed (121)
      Tests  2141 passed (2141)
   Duration  24.18s
```

Linter: none configured for `web/` (no `lint` script, no ESLint config). `tsc --noEmit` clean.

## Deviations from ADD

1. `web/src/components/ui/labels.test.ts` was modified though not listed in Section 4.3: its "knows all 54 server event types" count test fails once two event types are added. The count became 56. Unavoidable consequence of the specified `labels.ts` change.
2. `DeviceActivity.origin` is typed optional (`origin?:`) as the ADD Section 4.3 states, so a server without this story still typechecks. No other deviation. No "Rev 2 delta for Part G/W" subsection existed in the ADD when this work finished.

## Notes for QA

- Fleet row: a reinstall shows the compact one-line `ActivityLabel` (spinner plus "Reinstalling agent"); actor phrase and start time are in the hover title. The Version cell keeps the installed version (no spinner) because a reinstall has no target version.
- The device page `View command` link opens the same read-only modal used for self-updates (`data-testid="self-update-details"`, `self-update-outcome`); outcome wording: running "In progress", succeeded "Succeeded", timed_out "Not confirmed".
- The web relies on the server setting `origin: "installer"` on the activity (ADD 6.3). Activities from an older server (no `origin`) render with the pre-existing wording.
- The `reinstall.safety.test.ts` console output lists the scanned files for the QA coverage check against `git diff --stat -- web/`.
- Not verified in a browser preview (no dev server run); behaviour is covered by the Testing Library page tests.
