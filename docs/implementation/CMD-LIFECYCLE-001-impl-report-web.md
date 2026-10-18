# Implementation Report: WP-WEB

**Story ID:** CMD-LIFECYCLE-001
**Work package:** WP-WEB (ADD §4.3, `web/` only)
**Engineer:** Senior Dev Agent (frontend)
**Date:** 2026-10-17
**ADD:** docs/architecture/CMD-LIFECYCLE-001-add.md (rev 2). Contract: frozen §6.1, §6.2, §6.5, §6.7; design AD-018, AD-019.

## Detected stack

React 19.2 + TypeScript 5.9 (strict), Vite 6, Tailwind 4, TanStack Query 5, react-router 7, vitest 4 with
happy-dom, Testing Library 16 (`@testing-library/react`, `user-event`, `jest-dom`). No new npm dependency.
`web/package.json` and `web/package-lock.json` are untouched. There is no ESLint config and no `lint` script
in `web/package.json`; the static check is `tsc --noEmit` (also run as part of `npm run build`).

## Commits (ADD §13.2 items 22 to 26)

| # | Commit | Subject |
|---|---|---|
| 22 | 00c8c4d | `[CMD-LIFECYCLE-001] feat: command lifecycle types, API client and pure helpers` |
| 23 | d57d2c5 | `[CMD-LIFECYCLE-001] fix: drop stale live telemetry using server timestamps` |
| 24 | c501e4b | `[CMD-LIFECYCLE-001] feat: useCommandTracker and exited suppression during tracked updates` |
| 25 | 2891ab9 | `[CMD-LIFECYCLE-001] feat: track updates by command ID in ContainerDetail and ComposeStackSection` |
| 26 | a04e2b8 | `[CMD-LIFECYCLE-001] fix: canonical command labels in history and update-reason titles` |

Every commit staged and committed explicit `web/` paths only. No tag, no push.

## Completed components

| File | Action | Notes |
|---|---|---|
| `web/src/types/models.ts` | MODIFY | `CommandState`, `LegacyCommandStatus`, `CommandOrigin`; `Command` gains the ADD §6.1 fields (all optional so a pre-lifecycle server still typechecks); `CommandUpdate`, `BulkConflict`, `BulkDockerUpdateResult`, `CommandConflictBody`; `ContainerInfo.image_id/update_status/update_reason`; `WSMessage.collected_at/received_at` |
| `web/src/api/client.ts` | MODIFY | Additive and grouped: `CommandConflictError` + `throwIfCommandConflict` after `SSOAvailability`; `getCommand`, `getActiveCommands`, `cancelCommand` after `getCommandOutput`; 409 handling inside `sendCommand` and `bulkDockerUpdate` (now returns `BulkDockerUpdateResult`); `conflicts?: number` on the fleet bulk return types. One new `import type` line, no existing line reformatted |
| `web/src/api/client.test.ts` | MODIFY | AC-111 and the three new endpoints |
| `web/src/api/demo-client.ts` | MODIFY | `getCommand`, `getActiveCommands`, `cancelCommand`, `bulkDockerUpdate` stubs; re-exports `CommandConflictError` |
| `web/src/api/demo-data.ts` | MODIFY | Canonical statuses plus one failed auto-update row |
| `web/src/utils/commandLifecycle.ts` | CREATE | `normalizeStatus`, `isTerminal`, step and command labels, history chip labels, filter allowlist, target-key helpers, `applyCommandUpdate`, `applyActiveUpdate`, `upsertActiveCommand`, `updateIndicatorTitle`, `hasUpdateAvailable`, `updateStatusNote`, `shouldSuppressExited`, `isCommandConflictError` |
| `web/src/utils/commandLifecycle.test.ts` | CREATE | AC-062, 111, 112/113 (predicate), 114, 115, target keys |
| `web/src/hooks/telemetryMerge.ts` | CREATE | Pure merge: drops snapshots at or before the last `collected_at`, uses server `received_at`/`collected_at`, never reads a clock |
| `web/src/hooks/telemetryMerge.test.ts` | CREATE | AC-080, 081, 082 |
| `web/src/hooks/useCommandTracker.ts` | CREATE | Tracks by command id: mount fetch, WS `command_update`, 5s re-fetch loop, 5s "Updated" display, sticky terminal states, 409 attach, Cancel, Dismiss. No fixed timer ends tracking |
| `web/src/hooks/useCommandTracker.test.tsx` | CREATE | AC-108, 109, 110, 111, 114, 116, 117 |
| `web/src/hooks/useDevices.ts` | MODIFY | Merge via `mergeTelemetry`; `last_heartbeat` from `received_at`; maintains `['commands-active', deviceId]` from `command_update`; suppresses the stop/die to `exited` mapping while a tracked update for that container is in flight |
| `web/src/hooks/useDevices.test.ts` | MODIFY | AC-080/081/082 at hook level, AC-112, AC-113 |
| `web/src/components/ContainerDetail.tsx` | MODIFY | Tracker, status banner (Cancel/Dismiss), disabled controls, `docker_update` sends `container` (name) and `container_id`, reason title on the Update button, "Pinned by digest" / "Update check failed" status |
| `web/src/components/ContainerDetail.test.tsx` | CREATE | AC-062, 108, 109, 110, 111, 112 (integration with real `useDevices`), 114, 116, `[SEC]` |
| `web/src/components/ComposeStackSection.tsx` | MODIFY | One tracker for the stack's compose key, covering stack and network-group (bulk) updates; status line; skipped-target note for bulk conflicts |
| `web/src/components/ComposeStackSection.test.tsx` | CREATE | AC-108, 109, 110, 111, Cancel/Dismiss, `[SEC]` |
| `web/src/components/CompactContainerTile.tsx` | MODIFY | Indicator `title` states the reason; none for `check_failed`/`pinned` |
| `web/src/components/CompactContainerTile.test.tsx` | MODIFY | AC-062 |
| `web/src/components/ActivityLog.tsx` | MODIFY | Canonical labels for canonical and legacy rows, filter built from the ten allowlisted values, refresh on `command_update` (and legacy `command_result`) |
| `web/src/components/ActivityLog.test.tsx` | MODIFY | AC-115, `[SEC]`; the existing badge test moved to canonical labels |

`web/src/pages/ContainerDetailPage.test.tsx` and `web/src/pages/DeviceDetail.test.tsx` are listed in §4.3 as
"if needed". They already mock `ContainerDetail` wholesale and do not exercise the new API methods, so no
change was needed and none was made.

## AC to test table

Vitest names start with `[AC-xxx]` or `[SEC]`.

| AC | Test file(s) | Tests (describe block) | Status |
|---|---|---|---|
| AC-062 | `utils/commandLifecycle.test.ts`, `components/CompactContainerTile.test.tsx`, `components/ContainerDetail.test.tsx` | `[AC-062] update indicator title states the reason...` (9), `[AC-062] CompactContainerTile update indicator` (6), `[AC-062] the Update button title states why...` (3) | PASS |
| AC-080 | `hooks/telemetryMerge.test.ts`, `hooks/useDevices.test.ts` | `[AC-080] an older live snapshot is discarded` (3); `[AC-080] [AC-081] stale live telemetry is dropped` (3) | PASS |
| AC-081 | same | `[AC-081] a message with the same collected_at is discarded` (2), plus the hook-level block above | PASS |
| AC-082 | same | `[AC-082] last-seen comes from received_at, never the browser clock` (3, browser clock skewed +1h); `[AC-082] last-seen is the server received_at...` (3) | PASS |
| AC-108 | `hooks/useCommandTracker.test.tsx`, `components/ContainerDetail.test.tsx`, `components/ComposeStackSection.test.tsx` | `[AC-108] ... no fixed timer` / `follows ... by command id` (3 + 3 + 3). Covers each state and step label, still tracking after 5s and 30s | PASS |
| AC-109 | same three | `[AC-109] controls ... disabled while ... non-terminal` (1 + 1 + 2) | PASS |
| AC-110 | same three | `[AC-110] on mount/remount ... current step` (6 + 1 + 2) | PASS |
| AC-111 | `api/client.test.ts`, `hooks/useCommandTracker.test.tsx`, `components/ContainerDetail.test.tsx`, `components/ComposeStackSection.test.tsx`, `utils/commandLifecycle.test.ts` | `[AC-111] a 409 conflict carries/attaches...` (4 + 3 + 1 + 2), plus conflict detection (2) | PASS |
| AC-112 | `hooks/useDevices.test.ts`, `components/ContainerDetail.test.tsx`, `utils/commandLifecycle.test.ts` | `[AC-112] stop and die during a tracked update...` (9 hook-level incl. compose, stale, terminal; 3 integration with the real `useDevices`; 9 predicate cases shared with AC-113) | PASS |
| AC-113 | `hooks/useDevices.test.ts`, `components/ContainerDetail.test.tsx` | `[AC-113] with no tracked command a stop event still shows exited` (2), plus the integration case inside the `[AC-112]` block of ContainerDetail | PASS |
| AC-114 | `utils/commandLifecycle.test.ts`, `hooks/useCommandTracker.test.tsx`, `components/ContainerDetail.test.tsx` | `[AC-114] every state has its frozen label` (table-driven, 20), `[AC-114] terminal states...` (10 + 6); Updated returns to the normal view at exactly 5s (fake timers) | PASS |
| AC-115 | `components/ActivityLog.test.tsx`, `utils/commandLifecycle.test.ts` | `[AC-115] command history shows canonical labels, including for legacy rows` (7), `[AC-115] legacy history rows show canonical labels` (5) | PASS |
| AC-116 | `hooks/useCommandTracker.test.tsx`, `components/ContainerDetail.test.tsx`, `components/ComposeStackSection.test.tsx` | `[AC-116] Cancel ... only while ... queued` (4 + 2), plus Cancel/Dismiss cases in the stack suite | PASS |
| AC-117 | `hooks/useCommandTracker.test.tsx` | `[AC-117] ... re-fetched, and again every 5s` (6): nothing at 4999ms, one fetch at 5000ms, then every 5s; a push restarts the wait; a failed poll keeps polling | PASS |
| C-11 `[SEC]` | `ContainerDetail.test.tsx`, `ComposeStackSection.test.tsx`, `ActivityLog.test.tsx` (plus label and tracker unit cases) | `[SEC] agent failure text renders as text` (1 each): `result_msg` = `<img src=x onerror=alert(1)>` is present as literal text and no `<img>` element exists | PASS |

Note for QA: `useDevices.test.ts` and `CompactContainerTile.test.tsx` also contain `[AC-001]` to `[AC-008]` and
`[AC-022]` tests from earlier stories. AC ids are story-scoped; those are not CMD-LIFECYCLE-001 ACs.

QA grep for C-11: `dangerouslySetInnerHTML` appears in `web/src` only inside a comment/test name in
`pages/Probes.test.tsx` (pre-existing, asserts its absence). No agent string is bound to `href`, `src`, or a
`title` containing markup; `title` values come from `updateIndicatorTitle`, which uses server enums only.

## Test run output

### Typecheck (`cd web && npx tsc --noEmit`)

```
tsc exit code: 0
```

(No diagnostics.) `npx vite build` and `VITE_DEMO=true npx vite build` (output to a scratch directory) both
succeed, so the demo alias with the re-exported `CommandConflictError` bundles.

### New and changed test files

```
 RUN  v4.0.18 <worktree>/web

 ✓ src/hooks/telemetryMerge.test.ts (13 tests) 8ms
 ✓ src/api/client.test.ts (17 tests) 41ms
 ✓ src/utils/commandLifecycle.test.ts (64 tests) 13ms
 ✓ src/components/CompactContainerTile.test.tsx (9 tests) 39ms
 ✓ src/hooks/useDevices.test.ts (34 tests) 60ms
 ✓ src/components/ComposeStackSection.test.tsx (13 tests) 240ms
 ✓ src/hooks/useCommandTracker.test.tsx (35 tests) 321ms
 ✓ src/components/ContainerDetail.test.tsx (21 tests) 597ms
 ✓ src/components/ActivityLog.test.tsx (17 tests) 1153ms

 Test Files  9 passed (9)
      Tests  223 passed (223)
```

### Full suite (`cd web && npm run test:run`)

Baseline before this work: 27 files, 405 tests passing. After:

```
 ✓ src/pages/dashboard/ContainerLeaderboard.test.tsx (16 tests) 148ms
 ✓ src/hooks/useFleetMetrics.test.ts (4 tests) 423ms
 ✓ src/pages/Login.test.tsx (12 tests) 299ms
 ✓ src/hooks/useCommandTracker.test.tsx (35 tests) 376ms
 ✓ src/pages/ContainerDetailPage.test.tsx (10 tests) 413ms
 ✓ src/pages/settings/AgentManagement.test.tsx (3 tests) 443ms
 ✓ src/pages/settings/AlertRuleSettings.test.tsx (7 tests) 706ms
 ✓ src/components/ComposeStackSection.test.tsx (13 tests) 594ms
 ✓ src/pages/dashboard/Dashboard.test.tsx (16 tests) 631ms
 ✓ src/pages/FleetOverview.test.tsx (22 tests) 1067ms
 ✓ src/pages/Security.test.tsx (23 tests) 1488ms
 ✓ src/components/ActivityLog.test.tsx (17 tests) 1597ms
 ✓ src/pages/dashboard/ActivityRiver.test.tsx (21 tests) 228ms
 ✓ src/components/RebootClassBadge.test.tsx (4 tests) 34ms
 ✓ src/utils/cron.test.ts (24 tests) 41ms
 ✓ src/hooks/useDevices.test.ts (34 tests) 117ms
 ✓ src/components/ConfirmModal.test.tsx (6 tests) 206ms
 ✓ src/pages/dashboard/PerDeviceCharts.test.tsx (14 tests) 188ms
 ✓ src/components/GaugeBar.test.tsx (5 tests) 46ms
 ✓ src/api/client.test.ts (17 tests) 27ms
 ✓ src/components/ContainerDetail.test.tsx (21 tests) 1548ms
 ✓ src/pages/Probes.test.tsx (29 tests) 2167ms
 ✓ src/components/CompactContainerTile.test.tsx (9 tests) 50ms
 ✓ src/components/StatusBadge.test.tsx (3 tests) 34ms
 ✓ src/utils/commandLifecycle.test.ts (64 tests) 20ms
 ✓ src/utils/filesystem.test.ts (39 tests) 11ms
 ✓ src/utils/format.test.ts (13 tests) 7ms
 ✓ src/hooks/telemetryMerge.test.ts (13 tests) 10ms
 ✓ src/utils/rebootEta.test.ts (30 tests) 8ms
 ✓ src/utils/security.test.ts (18 tests) 7ms
 ✓ src/utils/eventCategory.test.ts (11 tests) 8ms
 ✓ src/pages/DeviceDetail.test.tsx (37 tests) 4215ms

 Test Files  32 passed (32)
      Tests  590 passed (590)
```

(+185 tests, +5 files, no regression. The `stderr` noise about `queryFn` and "Maximum update depth" in
`DeviceDetail.test.tsx` and `useFleetMetrics.test.ts` is pre-existing and unchanged.)

Mutation check performed during development: with the stop/die suppression in `useDevices.ts` disabled, 6
tests (4 in `useDevices.test.ts`, 2 integration tests in `ContainerDetail.test.tsx`) failed. The change was
reverted; the committed file is the correct one.

### Lint

No lint script or ESLint config exists in `web/`. `tsc --noEmit` is the only static check and it is clean.
New code contains no `any`, no `console.*`, no `dangerouslySetInnerHTML`, and no `Date.now()` or `new Date()`
in `telemetryMerge.ts`, `useCommandTracker.ts` or `commandLifecycle.ts`. (`useDevices.ts` keeps one
pre-existing `(old: any)` in the `docker_update` cache updater; the telemetry and heartbeat updaters I
rewrote are now typed.)

## Deviations from the ADD

1. **Hook entry point is `track(send)`, not `start`.** AD-019 says "`start` tracks the command on 201". The hook
   interface is not frozen. `track(send)` takes the request as a callback so one tracker serves
   `sendCommand` and `bulkDockerUpdate` (a network-group update runs against the same compose key). Behavior is
   as specified: 201 tracks the new id, 409 attaches to the existing id.
2. **Non-update actions are tracked too.** In ContainerDetail, Check Updates, Restart, Stop and Start are tracked by
   command id (ADD 6.7: "Running… <step>" and "Done"). They carry no target key, so they are not re-adopted
   after a remount; only keyed commands are (FR-092).
3. **`Command` lifecycle fields are optional in the TypeScript type** (ADD 6.1 lists them as always present). This
   keeps the dashboard compiling and working against an old server (NFR-001), and keeps existing fixtures valid.
4. **Terminal states other than `succeeded` are all sticky** (`failed`, `timed_out`, `expired`, `cancelled`), with
   a Dismiss button. FR-095 and AD-019 specify this only for `failed`; the others have no auto-clear rule in the
   FRD, and clearing them silently would hide an outcome the operator needs to see.
5. **ContainerDetail control labels.** While a command is tracked the buttons stay labelled and disabled; the
   "..." label is shown only while the create request itself is in flight. The old code showed "..." for the
   whole mutation. The tracked state is carried by the status banner instead.
6. **ActivityLog colours.** `queued` is now grey (the pre-lifecycle `pending` colour); the old amber `queued` was
   a legacy-only value that now maps onto the same state as `pending`. In-flight states are amber/blue and
   `timed_out` is orange. Cosmetic.
7. **`client.ts`:** one extra `import type` line was added below the first import rather than editing the existing
   long import line, and `conflicts?: number` was added to the two fleet bulk return types (ADD §6.2: "adds
   `conflicts: <int>`"). `bulkDockerUpdate` also returns the richer `BulkDockerUpdateResult`.
8. **`demo-client.ts`** also gained a `bulkDockerUpdate` stub (it was missing, and bulk update now needs a
   conflict-aware path) and re-exports `CommandConflictError` from `./client`.
9. **Duck-typed conflict detection.** The tracker detects a 409 through `isCommandConflictError` (name plus
   `existingCommandId`) rather than `instanceof`, so it keeps working when tests or the demo build replace the
   API module. `CommandConflictError` itself lives in `client.ts` as specified.
10. **`ContainerGroup.tsx` not changed.** It still has the old 5s-reset mutation and `update_available` check, but
    it is not imported anywhere in `web/src` (dead component) and is not in §4.3.

No file outside `web/` was touched, apart from this report. No file in `web/` outside ADD §4.3 was modified.

## Contract assumptions the server must honour

These are the places the dashboard relies on behavior beyond the literal field list in ADD §6.

1. **WS envelope timestamps are top-level.** `collected_at` and `received_at` sit next to `type`/`device_id`/`data`
   on `telemetry` messages, and `received_at` on `heartbeat` messages (ADD §6.5, AD-018). If either is absent the
   dashboard keeps the stored value (old-server behavior). RFC3339 with nanoseconds is fine (JS keeps ms).
2. **`GET /devices/{id}/commands?active=true` (no `target_key`) returns every non-terminal command of the device**
   (queued through verifying, canonical status) with `target_keys` populated. The dashboard filters by key on
   the client and shares one fetch per device (`['commands-active', deviceId]`). `docker_bulk_update` rows must
   carry the union of their targets' keys, and `docker_update` rows the compose or container key, or the exited
   suppression and the remount adoption cannot match.
3. **`command_update` payload** is the ADD §6.5 shape. `updated_at` must be non-decreasing per command (the
   dashboard ignores an older one); `message` is the row's `result_msg`; the first `queued` transition may be
   broadcast, but is not required (the 201 body seeds the tracker). Its `device_id` is read from `data.device_id`
   with the envelope `device_id` as fallback.
4. **409 on create** must carry `code: "conflict"` and a non-empty `existing_command_id` (both single and
   all-conflict bulk). `existing_status` is optional; when present it is used for the immediate label before the
   `GET /commands/{id}` hydrate. A 409 without `code: "conflict"` is shown as a normal error.
5. **Bulk 201** returns the Command with `conflicts` as an array (or absent); a non-empty array is shown as
   "N target(s) skipped".
6. **`GET /devices/{id}/commands/{commandId}`** returns the Command with canonical `status` and is the
   source for the 5s re-fetch. A non-2xx or a network error is treated as transient and polling continues.
7. **`POST …/cancel`** returns 200 with the cancelled Command, or 409 with an `error` string, which is shown to
   the operator before the state is re-read.
8. **History filter:** the dashboard sends only the ten canonical status values in `status=`; the server's 400
   `invalid_filter` is therefore never expected from the UI.
9. **Agent text** (`result_msg`, `step`, `failure_step`) may contain anything; it is rendered as text. `step` is
   shown raw in "Failed at <step>"; an unknown step renders as "Updating… Working" while running.

No conflict was found between the models contract and ADD §6; field names and enum values match
`internal/models/commands.go` (`CommandUpdate`, `CommandConflictResponse`, `BulkConflict`, `Command` JSON tags
checked against the committed file).

## Notes for QA

- Fake timers drive every timing case (5s "Updated", 5s re-fetch). Under fake timers React must re-arm the
  re-fetch timer between steps, so tests advance in 5s steps inside `act`.
- To exercise the live UI manually: start an update on a container, then `docker stop` it by hand. The tile and
  detail header must keep `running` and show "Updating…" until the command is terminal.
- Cases worth probing against a real server: refresh the page mid-update (adoption by target key), start the
  same update from two tabs (409 attach), stop the server's WS (the 5s re-fetch should keep the label moving),
  cancel racing a heartbeat claim (409 message, then "Sent to agent"), and an old server (legacy `sent`/`success`
  rows and missing `collected_at`).
- Windows working-tree files are CRLF (git `autocrlf`); new files committed from here normalize to LF in the
  index. Git prints "LF will be replaced by CRLF" warnings; they are harmless.
