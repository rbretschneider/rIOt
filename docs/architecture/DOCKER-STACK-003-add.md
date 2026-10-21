# Architecture — DOCKER-STACK-003

**Date:** 2026-10-20  
**Stack:** React 19/strict TypeScript 5.9, React Router 7, TanStack Query 5, Tailwind 4, Vitest 4/Testing Library. Existing shared Modal, useCommandTracker and CommandProgress. No dependencies/backend changes.

## 1. Design

Add one compact Update action column to StacksPage. Store the selected device/project pair as local UI state and mount one `StackUpdateDialog` outside DataTable, keyed by that pair. The dialog owns fresh selected-resource reads and command tracking. Do not mount a query/tracker for every row. On StackPage add `update` to the secondary action set when it is not the primary, preserving the existing generic confirmation and command presentation. Apply the same accurate Update consequences and capability/path checks there; avoid refactoring unrelated actions or the primary-action helper.

## 2. Component changes

| File | Change |
|---|---|
| `web/src/pages/docker/StacksPage.tsx` | Fixed-width, nonsortable row Update action and single selected-target dialog outside virtual grid; preserve filters/route/links/rows. |
| `web/src/components/docker/StackUpdateDialog.tsx` | New focused dialog with fresh detail/hosts gates, target identity, confirmation, tracker/error/progress and terminal refresh. Suggested props: `deviceId: string`, `project: string`, `onClose: () => void`. Parent conditionally mounts it for one selected target, with a device/project key. |
| `web/src/pages/docker/StackPage.tsx` | Always-discoverable Update secondary action when not primary; existing modal Update copy, current gate recheck on submit and terminal query refresh. Keep all other confirmations/actions unchanged. |
| `web/src/pages/docker/DockerPages.test.tsx` | AC-001/002/003/004/005/006/007 page regressions including no dispatch on open, detail visibility, exact target/payload, state gates and existing Down confirmation. |
| `web/src/components/docker/StackUpdateDialog.test.tsx` | Focused AC-003/004/005/006/007 asynchronous gate, duplicate/conflict/progress, refresh, close/reopen and cross-host isolation tests if page tests become unwieldy. |

Existing `api/docker.ts`, hooks/useCommandTracker.ts, CommandProgress.tsx, DataTable.tsx and utils/dockerActions.ts are reused without behavioral changes. No changes to server/agent/shared models, route definitions, feature/auth shell, global styles or release config.

## 3. Data and gates

On dialog mount use existing query keys `['docker-stack', deviceId, project]` and `['docker-hosts']`, with mount refetch/staleTime zero so cached collection/detail state cannot alone enable Confirm. Disable dispatch until both fresh reads succeed and are not fetching. Resolve capability from the exact selected host and require `stack_actions_v1`; require selected stack `path_state === 'agreed'` and a nonempty `agreed_working_dir`. Display current config files but do not invent or edit paths. A read error/404/unsupported/unagreed state renders a clear nonmutating explanation and canonical stack detail link for existing remediation.

Track by selected device plus `composeTargetKey(agreed_working_dir)` once the valid detail target exists. Existing active-command lookup/push/polling handles adoption and exclusion. Unknown/stale collection update counts are presentation only; zero/down/partial state is not an Update availability gate. Do not fetch Compose file/env content to perform an update. The server remains authoritative for path changes/capability/authorization at dispatch time; handle refusal visibly.

StackPage keeps its current detail polling and host query, applies its existing allowed gate plus nonempty agreed directory to Update, and rechecks the current allowed/busy state inside the submit handler. Update's new visibility must not imply permission to dispatch when the gate fails. Its confirmation remains the existing generic modal, with new Update consequence text and existing self warning; other actions retain their existing required typed values.

## 4. Mutation, tracking and lifecycle

The only new send is `tracker.track(() => dockerApi.stackAction(deviceId, project, {action:'update'}))`. The selected device/project is captured from the mounted target, never inferred from visible text/row index or replaced by a later selection. Check the current gates in the handler and use a local synchronous submission guard plus disabled pending/busy controls to prevent rapid duplicate creation. No effect, query callback or modal-open callback sends a mutation. No automatic retry. Do not include `confirm`, `confirm_host`, work directory, compose filenames, shell text or package/image options for this existing Update API.

Read the tracker command directly for collection progress, including conflict attachment; `track` returns undefined for conflicts/errors, so returned-command-only logic would lose the existing command. Render existing `CommandProgress` and tracker errors in the modal body, not the row. Avoid new timers and keep nonterminal commands tracked. Prevent dismissal during the create request; after acceptance Close may dismiss observation without cancelling the job. Reopening adopts an active command through the existing target tracker. Existing queued-only cancellation offered by CommandProgress remains unchanged and explicit.

On terminal callback invalidate `['docker-stack', deviceId, project]`, `['docker-stacks']`, and `['docker-containers']`. Existing CommandProgress invokes terminal once per command instance; avoid repeated invalidation through render effects or callback identity churn. Detail progress should use the same refresh set as well as clearing its local active state. A terminal failure/error remains visible while open; retry is an explicit new Confirm after terminal, never automatic or driven by a refetch. Target-keyed mounting discards stale UI state when switching stacks, while React Query scopes async reads by device/project. No persistence format or URL rewrite is introduced.

## 5. Confirmation and accessibility

Use shared Modal focus trapping/restoration and named controls. Row button accessible name includes project (and host when needed), e.g. `Update app on host-one`; visible text may remain Update. The confirmation title names the stack and the body shows host, agreed directory/files and: Update pulls images and runs Compose up; changed services may be recreated and stopped services may start. This is not merely an update check, and zero known updates is not a guarantee of no change. Preserve the existing rIOt infrastructure connection-interruption warning for `self_role`. Do not claim a new server-required typed-host confirmation for Update; server currently requires host confirmation only for down/redeploy/restart self actions.

Keep the action cell single-line inside the fixed 44px virtual row. No inline row progress, extra nested virtual rows, dropdown that requires row expansion or layout/theme redesign. Dialog content can scroll within the existing modal at portrait widths; long paths wrap/truncate with full values discoverable. Paths/project/hostname remain escaped React text, and links use existing canonical encoding.

## 6. Security and verification

No privilege expansion; existing authenticated Docker route and server dispatch enforce authorization/capabilities/paths. UI consent and gates improve usability but are not a security boundary. Never fetch raw environment/Compose secrets or accept client-selected executable paths. A conflict attaches to the existing scoped command; errors cannot trigger automatic resubmission. Closing an observer never cancels a running update.

Tests map to FRD AC-001–007. Use mocked API/command/WebSocket data and fake timers where existing tracking requires time; no network/production mutation. Test query refresh/permission/path races, conflict attach, repeated click, retained failure, correct same-project/different-host target, detail Up+Update/down and primary Update uniqueness, and existing typed Down confirmation. Run focused tests, full frontend suite, `npx tsc --noEmit`, production build and browser wide/portrait visual/keyboard QA; package.json has no lint script. Record implementation evidence, then independent QA and technical writing before merge.
