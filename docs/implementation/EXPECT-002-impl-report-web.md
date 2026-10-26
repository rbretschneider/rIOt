# EXPECT-002 Implementation Report: Web

- **Story:** EXPECT-002 (agent self-update as expected activity)
- **ADD:** `docs/architecture/EXPECT-002-add.md` revision 2
- **Stack:** React 18 + TypeScript, Vite, Tailwind CSS, vitest + Testing Library (`web/`)
- **Scope:** files under `web/` only. The Go side is reported in `EXPECT-002-impl-report.md`.

## Commits

| Commit | Change |
|---|---|
| `8ffdfb8` | Self-update types, events client and labels that carry versions. `types/models.ts`: origin `agent_self_update`, `DeviceActivity.agent_to_version`, `CommandEventsResponse`, optional `command_id` and `command_attr` on `Event`. `api/client.ts` and `demo-client.ts`: `getCommandEvents`. `demo-data.ts`: `agent_to_version: ''`. `components/ui/labels.ts`: the three `agent_self_update_*` event labels. New `utils/selfUpdate.ts`. `utils/activity.ts`: "Updating agent <from> → <to>" and the escalated labels with versions. Additive tests in `activity.test.ts` and a new `selfUpdate.test.ts` |
| `a9bdfae` | Read-only self-update record in the command history. New `components/SelfUpdateDetails.tsx` and its tests. `CommandProgress.tsx` and `ActivityLog.tsx` render it only for the self-update origin. The history row shows the versioned label, "by agent auto-update" and an outcome badge. The modal has no controls |
| `4b4dba4` | `pages/FleetOverview.selfupdate.test.tsx`, `pages/DeviceDetail.selfupdate.test.tsx` |
| `d1ee067` | `utils/selfUpdate.safety.test.ts` (the C-1 / AC-029 web scan) |

## AC to test mapping (web)

| AC | Describe / test |
|---|---|
| AC-001, AC-008, AC-016, AC-020 | `EXPECT-002 [AC-0xx]` cases in `utils/activity.test.ts` and `utils/selfUpdate.test.ts`, including "not-back label names versions" and "label clears on activity_update null" |
| AC-023 | `pages/FleetOverview.selfupdate.test.tsx` ("fleet label, actor, start and expected-offline count"); `pages/DeviceDetail.selfupdate.test.tsx` ("header link opens the read-only record"); `components/SelfUpdateDetails.test.tsx` ("details show versions, outcome, server events, the omitted count, and no controls", plus the ActivityLog history-row and modal cases) |
| AC-029 | `utils/selfUpdate.safety.test.ts`. It prints its scanned file list, and that list includes the three new test files, `selfUpdate.ts` and `SelfUpdateDetails.tsx` |

## Results

- `cd web && npx tsc --noEmit -p .`: clean.
- `npm run test:run`: 114 files, 2015 tests passed. Baseline was 109 files, 1873 tests.
- No lint script exists in `web/package.json`, so no linter was run.

## Deviations from the ADD

1. **`DeviceActivity.agent_to_version` is optional (`?: string`).** The existing EXPECT-001 fixtures build full `DeviceActivity` literals and would fail `tsc` on a required field. Optional also keeps an older server working. Runtime behavior matches the ADD.
2. **`components/ui/labels.test.ts` is modified.** It asserted exactly 51 event types; the three new types make 54. EXPECT-001 made the same change when it added its types.
3. **No separate history test file.** `SelfUpdateDetails.test.tsx` also holds the ActivityLog history-row and modal cases.
4. **Two extra helpers in `selfUpdate.ts`:** `selfUpdateOutcomeTone` and `SELF_UPDATE_ACTOR_PHRASE`, beyond the three the ADD lists.
5. **Web safety scan scope.**
   - It bans `child_process`, `spawn(`, `exec(`, `execSync`, `process.kill`, `systemctl`, `poweroff`, `shutdown -`, `reboot -`, `riot-agent` exec/run/`--`, and raw HTML sinks.
   - It does not ban the Go-side literals `dpkg`, `apt` and `systemd-run`, because `activity.ts` legitimately contains "waiting for dpkg lock".
   - It scans the five ADD sources plus `models.ts`, `client.ts`, `demo-client.ts`, `labels.ts` and `labels.test.ts`.
   - `demo-data.ts` is excluded because it holds sample install-script text that names `systemctl`.
   - The existing EXPECT-001 activity scan picks up any test that contains the text `utils/activity`. The new scan therefore assembles that path at runtime, so the old scan stays unchanged and green.

## Notes for QA

- **Version re-validation.** Versions are re-validated against the AD-011 grammar. A hostile version, or one that is equal to the other after stripping `v`, falls back to plain "Updating agent" / "Agent update".
- **Events endpoint.** `getCommandEvents(deviceId, commandId)` encodes both ids. A Go `null` slice is treated as empty. If the call fails, the record still renders, with an inline error.
- **Rendering.** Scrolling lists use `scrollbar-thin`. The code has no `any`, no `dangerouslySetInnerHTML`, and renders every server string as React text.
