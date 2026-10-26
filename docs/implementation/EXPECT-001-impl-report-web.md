# Implementation Report (web, WP-7)

- **Story ID:** EXPECT-001
- **Package:** WP-7 (web) only. No Go file touched.
- **Engineer:** Senior Dev Agent (frontend)
- **Date:** 2026-10-26
- **Branch:** feat/expected-activity-001
- **Detected stack:** React 19 + TypeScript (strict), Tailwind 4, Vite, TanStack Query 5, react-router 7, Vitest 4 with Testing Library and happy-dom. No lint script exists in `web/package.json`; the typecheck is `npx tsc --noEmit`.

## Commits

| SHA | Message |
|---|---|
| 5cfe205 | [EXPECT-001] feat: activity types, client, hook, WS handling and label rules |
| 1d58d4f | [EXPECT-001] feat: show activity labels on the fleet and device pages |
| 59aae4e | [EXPECT-001] fix: refetch activity when an update arrives before the first fetch |
| 93c06f5 | [EXPECT-001] test: cover activity labels, counts, live updates, device link and safety scan |
| (this report and the null-slice hardening are in the final commit) | |

## Completed Components

| File | Action | Notes |
|---|---|---|
| `web/src/types/models.ts` | MODIFY | `ActivityKind`, `ActivityPhase`, `ActivityEscalationReason`, `ActivityEscalation`, `DeviceActivity`, `ActivityResponse` (ADD 6.1) |
| `web/src/api/client.ts` | MODIFY | `api.getActivity()` -> `GET /api/v1/activity` |
| `web/src/hooks/useActivity.ts` | CREATE | Query `['activity']`; refetch after a WS reconnect; 30 s poll only while the WS is down |
| `web/src/hooks/useDevices.ts` | MODIFY | `activity_update` handling (patches `['activity']`; invalidates it if the first fetch has not landed) |
| `web/src/utils/activity.ts` | CREATE | `activityLabel`, `isExpectedOffline`, `activityLink`, `applyActivityUpdate`, `indexActivities`, `showsActorLine`, `isEscalated` |
| `web/src/components/ActivityLabel.tsx` | CREATE | Label, actor phrase, `started <RelativeTime>`, command link (route link with `encodeURIComponent`, or a modal for host and agent commands). Modal body uses `scrollbar-thin`. No raw HTML |
| `web/src/components/ui/labels.ts` | MODIFY | `expect_backstop_skipped` "Expected-activity check skipped", `expect_notifications_held` "Escalation notifications held" |
| `web/src/pages/FleetOverview.tsx` | MODIFY | Label under the hostname, hollow sky dot for expected offline, "Expected offline" card, Offline count excludes expected offline |
| `web/src/pages/DeviceDetail.tsx` | MODIFY | Header block with label, actor, start time and link |
| `web/src/components/docker/CommandProgress.tsx` | MODIFY | `showActivityLink?: boolean` (default true) |
| `web/src/api/demo-client.ts`, `web/src/api/demo-data.ts` | MODIFY | `getActivity` (required by the demo/real api parity test); one demo docker activity |
| `web/src/components/ui/labels.test.ts` | MODIFY | Event type count 49 -> 51 (required by the two new event types) |

Not changed: PatchBadge, RebootClassBadge, the bulk "Update Agents" button and their tests (AC-043).

## Test Summary

| AC | Test file | Tests | Status |
|---|---|---|---|
| AC-024 | `web/src/pages/FleetOverview.activity.test.tsx` (`[AC-024] fleet agent update shows what is happening`); `web/src/utils/activity.test.ts` (`[AC-024] indexing the response`, label cases) | 3 + 2 | PASS |
| AC-028 (web) | `FleetOverview.activity.test.tsx` (`[AC-028] a powered-off device on the fleet page`); `activity.test.ts` | 2 | PASS |
| AC-038 | `activity.test.ts` (`[AC-038] label vocabulary`, `[AC-038] the actor line`) | see per-file counts | PASS |
| AC-039 | `FleetOverview.activity.test.tsx` (`[AC-039] offline and expected-offline counts`); `activity.test.ts` (`[AC-039] expected offline versus offline`) | see per-file counts | PASS |
| AC-040 | `useDevices.test.ts` (`[AC-040] activity_update keeps the activity cache live`); `useActivity.test.tsx`; `FleetOverview.activity.test.tsx` (`[AC-040] the fleet page updates live`); `activity.test.ts` (`applying an activity_update`) | see per-file counts | PASS |
| AC-041 (web part) | `activity.test.ts` (`[AC-041] the escalated state`); escalated styling in the Fleet and DeviceDetail tests. The 24 h and online clearing is server-side (`TestEX_AC041_*`); the web renders whatever the server sends | 1 | PASS |
| AC-042 | `web/src/pages/DeviceDetail.activity.test.tsx` (`[AC-042] header shows label, actor, start and link`); `CommandProgress.activity.test.tsx`; `activity.test.ts` (`[AC-042] the link to the command`) | see per-file counts | PASS |
| AC-043 | Existing patch-badge, bulk-update and fleet tests run unmodified and pass; `FleetOverview.activity.test.tsx` (`[AC-043] devices without an activity are displayed as before`, 2 tests) | 2 | PASS |
| AC-047 (web) | `web/src/utils/activity.safety.test.ts` (`[AC-047] new web tests spawn nothing`, `[AC-047] the new web files use no raw HTML sink`) | 16 | PASS |

The AC-047 web scan set is computed from the sources and printed on each run. Current set: `utils/activity.ts`, `hooks/useActivity.ts`, `components/ActivityLabel.tsx`, and the test files `components/docker/CommandProgress.activity.test.tsx`, `hooks/useActivity.test.tsx`, `hooks/useDevices.test.ts`, `pages/DeviceDetail.activity.test.tsx`, `pages/FleetOverview.activity.test.tsx`, `utils/activity.test.ts`.

### Test Run Output

Typecheck (`cd web && npx tsc --noEmit`): exit 0, no output.

Lint: the project has no lint script (`web/package.json` scripts: dev, build, build:demo, preview, test, test:run, test:coverage). `tsc` (strict) is the static check.

Full suite (`cd web && npx vitest run`), baseline before this story: 95 files / 1707 tests.

```
 Test Files  101 passed (101)
      Tests  1813 passed (1813)
   Duration  19.80s
```

Per new or changed file: `FleetOverview.activity.test.tsx` 13, `DeviceDetail.activity.test.tsx` 7, `useActivity.test.tsx` 5, `useDevices.test.ts` 41 (was 35), `CommandProgress.activity.test.tsx` 2, `activity.test.ts` 53, `activity.safety.test.ts` 16, `labels.test.ts` 109 (was 107).

## API / WS contract coded against (ADD 6.1, 6.4)

`GET /api/v1/activity` (admin session), 200:

```
{"generated_at": RFC3339, "newest_agent_version": string, "activities": [DeviceActivity]}
```

`DeviceActivity`: `device_id`, `kind` (host | agent | docker | powered_off | escalated), `phase` (queued | in_progress | return | settling | powered_off | escalated), `action`, `command_id`, `actor_phrase`, `started_at`, `back_by` (RFC3339 | null), `waiting_dpkg_lock` (bool), `progress` (int | null), `agent_from_version`, `target`, `docker_actions` (int), `covers_offline` (bool), `device_online` (bool), `escalation` (null | `{reason: failed|timed_out|no_result|not_back, bound: string, at: RFC3339}`), `powered_off_at` (RFC3339 | null).

WS: `{"type":"activity_update","device_id":<id>,"data":DeviceActivity|null}`. `data: null` clears the device's entry.

Behavior the web depends on, which the Go server must honour:

1. At most one entry per device in `activities`, and the WS payload's `device_id` must equal the envelope `device_id` (a mismatch is ignored).
2. `covers_offline` must be true for a host or agent activity whose window currently covers an offline transition, and false for `queued`, `docker`, `escalated`. The web counts a device as "expected offline" only when the device-list status is `offline` AND (`kind == powered_off` OR (`kind` host/agent AND `covers_offline`)). Escalated devices stay in the Offline count.
3. `powered_off` entries must send `powered_off_at`; the label falls back to `started_at` if it is missing. `actor_phrase` is shown verbatim (e.g. `by ryan`).
4. For `agent_update` the label needs `agent_from_version` (version at command creation) and `newest_agent_version`; either missing gives "Updating agent".
5. `escalation.bound` is rendered verbatim in "Not back <bound> after <Action>" (send e.g. `"15m"`).
6. `command_id` must be non-empty for the "View command" link to render. Docker actions (kind `docker`, or any action starting with `docker_`) link to `/docker/activity?host=<device_id>&command=<command_id>`; host and agent commands open a modal using the existing `GET /devices/{id}/commands/{cid}` (`api.getCommand`).
7. An empty list may be sent as `null` (Go nil slice); the web tolerates it. The endpoint returning 503 or any error leaves the pages on the plain status display.
8. After a WS reconnect the web refetches `GET /activity`, so the endpoint must return current state, not only deltas.

## Deviations from the ADD

| # | Deviation | Rationale |
|---|---|---|
| 1 | Times in labels ("back by", "Powered off ... at") use the app's UTC/Local preference via `formatChartTick`, not "the browser locale" (AD-014). | The repo has a time-zone preference (FR-103) and a static guard (UIF AC-110) that forbids `toLocale*Time` calls. The default is UTC. The server-side message text still uses server local time. Architect to confirm. |
| 2 | `web/src/components/ui/labels.test.ts` modified (49 -> 51 event types). | Not in ADD Section 4, but adding the two event types the ADD names breaks that count assertion. |
| 3 | `web/src/api/demo-client.ts` and `demo-data.ts` modified. | Not in Section 4. The demo-parity test requires the demo client to expose exactly the real client's methods, so `getActivity` is needed. The demo returns one in-flight docker update. |
| 4 | The "Expected offline" card is shown only when its count is above 0 (the grid grows from 5 to 6 columns). | Keeps today's layout unchanged when nothing is expected offline (FR-042, AC-043). AC-039 is met whenever a device is expected offline. |
| 5 | `activity_update` arriving before the first fetch has landed invalidates `['activity']` instead of being dropped. | Closes a race where a fetch started before the message could overwrite it. No new endpoint or contract change. |
| 6 | The label is placed under the hostname in the Device cell; the dot title is "Offline (expected)" for expected offline. | The fleet table has no status-text column (status is a dot only), so there is nothing to replace. |
| 7 | New test files not listed in Section 4: `hooks/useActivity.test.tsx`, `components/docker/CommandProgress.activity.test.tsx`. `hooks/useDevices.test.ts` was extended as the ADD says. | Cover the reconnect refetch and the `showActivityLink` flag. |

No new dependency. No Go, agent or `install.sh` change.

## Notes for QA

- Escalated and powered-off label texts are covered in `activity.test.ts`; the fleet and device pages are covered with a mocked api and a mocked WebSocket. Nothing is spawned or executed (AC-047 web scan prints its file list in the vitest output).
- The AC-040 "within 5 s" requirement is met on the web side by applying the WS message immediately (no polling needed while the WS is connected). The server-side latency is `TestEX_AC040_*`.
- When the WS is down, `['activity']` polls every 30 s (same fallback as the device list). When `/api/v1/activity` fails, pages show the plain status and counts as before.
- To view it by eye the server needs the Go endpoint; the demo build (`build:demo`) shows one docker-update label on the first demo device.
- The vitest run prints some stderr from `AlertRuleSettings.test.tsx` (a file this story does not touch); all its tests pass.
