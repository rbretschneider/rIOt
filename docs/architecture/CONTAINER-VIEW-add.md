# Architecture Decision Document

| Field         | Value                                          |
|---------------|------------------------------------------------|
| Story ID      | CONTAINER-VIEW                                 |
| FRD Reference | docs/requirements/CONTAINER-VIEW-frd.md        |
| Author        | Architect Agent                                |
| Date          | 2026-04-16                                     |
| Status        | FINAL                                          |

---

## 1. Summary

This story fixes three bugs on the container detail page by (1) replacing the `invalidateQueries` call in the `docker_update` WS handler with a targeted `setQueryData` mutation that updates only the affected container's `state` field, (2) adding a reconnect-triggered invalidation of device detail queries using a `useEffect` that detects the `connected` flag transitioning from `false` to `true`, and (3) adding a name-based container fallback lookup with `navigate(..., { replace: true })` in `ContainerDetailPage` when the URL's `short_id` no longer matches any container but a container with the same `name` exists.

---

## 2. Technical Context

### Current State

- **`useDevices.ts`** (line 95-104): The `docker_update` handler calls `queryClient.invalidateQueries({ queryKey: ['device', msg.device_id] })` for significant actions (start, stop, die, create, destroy, pause, unpause). This triggers a full refetch that races with the telemetry WS `setQueryData` merge on lines 76-94. This is the root cause of Bug 1.

- **`WebSocketProvider.tsx`**: Manages the WS connection. Exposes a `connected` boolean via context. On disconnect, it sets `connected = false` and attempts reconnect after 3 seconds. On reconnect, it sets `connected = true`. There is no mechanism to notify subscribers of the *transition* from disconnected to connected. The `useDevices` hook and page-level queries use `connected` to toggle between WS-only and polling fallback, but React Query does not automatically refetch when `refetchInterval` changes from `15_000` to `false`. This is the root cause of Bug 2.

- **`ContainerDetailPage.tsx`**: Finds a container by `short_id` from the URL params. Uses a `lastContainer` ref to prevent flicker during transient cache gaps. Does not import `useNavigate`. Does not attempt name-based fallback when the `short_id` lookup fails. This is the root cause of Bug 3.

### What Exists and Is Unchanged

- The telemetry WS handler in `useDevices.ts` (lines 57-94) that merges telemetry into the `['device', deviceId]` cache via `setQueryData`. This is correct and untouched.
- The fleet list invalidation `queryClient.invalidateQueries({ queryKey: ['devices'] })` on line 104 of `useDevices.ts`. This remains.
- The `WebSocketProvider` itself. No changes to reconnection logic, timing, or message dispatch.
- The server-side WS broadcast logic (`hub.go`). No backend changes.
- The `DockerEvent` type in both Go (`internal/models/events.go`) and TypeScript (`web/src/types/models.ts`). Both already include `container_id`, `container_name`, and `action`.

---

## 3. Architecture Decisions

### AD-001: Replace `invalidateQueries` with targeted `setQueryData` for docker_update on device detail cache

**Decision:** For significant `docker_update` events, apply the event's `action` to the matching container's `state` field in the `['device', deviceId]` cache using `setQueryData`. Do NOT call `invalidateQueries` on the device detail query key. Non-significant events (healthcheck, etc.) are ignored entirely for the device detail cache.

**Rationale:** The FRD (FR-001, BR-001, BR-004) explicitly prohibits refetches on docker_update. A targeted `setQueryData` is preferred over ignoring docker_update entirely because it provides immediate visual feedback for state changes (e.g., a container stopping shows "exited" state within milliseconds instead of waiting up to 60 seconds for the next telemetry push). The telemetry push remains the authoritative full refresh.

**State mapping:** The `docker_update` `action` field maps to container `state` as follows:
- `start` -> `state: "running"`
- `stop` -> `state: "exited"`
- `die` -> `state: "exited"`
- `pause` -> `state: "paused"`
- `unpause` -> `state: "running"`
- `create` -> no state change (container may not be in the list yet; telemetry will add it)
- `destroy` -> no state change (do NOT remove from cache; telemetry will remove it per BR-003)

The handler matches by `container_name` (stripped of leading `/`) against the `name` field on `ContainerInfo`. Matching by name rather than ID is intentional: during container recreation, the docker_update for the new container may arrive before telemetry adds it to the list. Matching by name ensures the old entry gets updated if the new one isn't present yet.

**Alternatives Considered:**
- *Ignore docker_update entirely for device detail cache:* Simpler, but delays state feedback by up to 60 seconds. Users expect near-instant status updates when they start/stop containers.
- *Keep invalidateQueries but debounce:* Still a race condition, just less frequent. Rejected.

**Consequences:** Container state changes from docker_update are cosmetic hints, not authoritative. The `state` field may briefly be incorrect if the action-to-state mapping diverges from Docker's actual state model. This is acceptable because telemetry will correct it within 60 seconds.

---

### AD-002: Detect WS reconnection via `useEffect` watching `connected` transition in `useDevices`

**Decision:** Add a `useRef<boolean>` (`prevConnectedRef`) in `useDevices` that tracks the previous value of `connected`. Add a `useEffect` that runs when `connected` changes. When `connected` transitions from `false` to `true` (reconnection), call `queryClient.invalidateQueries({ queryKey: ['device'], exact: false })` to invalidate ALL device detail queries. This is a prefix match -- it invalidates `['device', 'xxx']` for every cached device.

**Rationale:** The reconnection invalidation must happen globally (for all cached device detail queries), not just the one the user is currently viewing. Multiple pages may have device detail queries in the cache. A prefix match on `['device']` with `exact: false` covers all of them. The fleet list query `['devices']` (plural) does NOT match this pattern because React Query's prefix matching requires the array elements to match positionally -- `['device']` does not prefix-match `['devices']`.

Wait -- this is wrong. Let me verify. React Query's `queryKey` matching: `{ queryKey: ['device'] }` with default (non-exact) matching will match any query whose key *starts with* `['device']`. The key `['devices']` starts with the string `'devices'`, not `'device'`, so `['device']` does NOT prefix-match `['devices']`. Confirmed: this is safe.

On initialization (`connected` starts as `false`, then becomes `true` on first connect), `prevConnectedRef` is initialized to `true` to prevent a spurious invalidation on the initial connection. The initial data load is handled by the query's own `queryFn`.

**Alternatives Considered:**
- *Emit a custom event from WebSocketProvider on reconnect:* More explicit, but adds coupling between the provider and consumers. The `connected` state already conveys this information; we just need to detect the edge.
- *Add a `reconnectCount` to the WSContext:* Cleaner edge detection, but requires modifying the provider. The `useRef` approach keeps the change localized to `useDevices`.
- *Invalidate from inside `WebSocketProvider.onopen`:* The provider does not have access to the query client. Would require prop-drilling or a new context dependency. Rejected.

**Consequences:** On reconnect, all cached device detail queries are invalidated. If the user has 10 device detail pages in their browser history (still in cache), all 10 will refetch on reconnect. This is correct behavior -- all of them are stale.

---

### AD-003: Name-based container fallback with URL replacement in ContainerDetailPage

**Decision:** In `ContainerDetailPage`, when the `short_id` lookup fails, perform a secondary lookup by `name` using the `lastContainer.current.name` value. If a container with the same name but a different `short_id` is found, call `navigate(\`/devices/${id}/containers/${newContainer.short_id}\`, { replace: true })`. The `replace: true` prevents a back-button loop to the dead URL.

The lookup sequence is:
1. Find container by `short_id` from URL params (existing behavior)
2. If not found, check if `lastContainer.current` exists and has a `name`
3. If yes, find a container with the same `name` in the container list
4. If found and its `short_id` differs from the URL param, navigate with replace
5. If not found by either method, show "Container not found"

The `lastContainer` ref is kept. It serves two purposes:
- Prevents flicker during transient cache gaps (existing purpose, still valid)
- Provides the `name` for the fallback lookup after a container is recreated

**Alternatives Considered:**
- *Parse the container name from the URL instead of using lastContainer ref:* URLs contain `short_id`, not `name`. We would need to change the URL scheme, which is out of scope.
- *Store container name in a separate ref:* Redundant -- `lastContainer.current.name` already has it.
- *Remove lastContainer ref and rely solely on name-based navigation:* The ref still prevents flicker during the brief window between a cache update dropping the container and the navigation effect firing. Keep it.

**Consequences:** The name-based fallback only works if the user has viewed the container at least once (so `lastContainer.current` is populated). If the user navigates directly to a URL with a stale `short_id` (e.g., from a bookmark), `lastContainer.current` will be null, and "Container not found" will display. This is acceptable -- the FRD only requires handling the recreation scenario while the user is actively viewing the container.

---

### AD-004: Navigation via `useEffect` rather than inline render logic

**Decision:** The name-based navigation (AD-003) must be triggered via a `useEffect`, not inline during render. React Router's `navigate()` must not be called during the render phase. The effect watches the container list and the URL param, and fires the navigation when the conditions in AD-003 are met.

**Rationale:** Calling `navigate()` during render causes React warnings and can lead to infinite render loops. The `useEffect` approach is the standard React pattern for imperative side effects triggered by state changes.

**Consequences:** There is a single render frame where the component sees no matching container before the effect fires and navigates. The `lastContainer` ref covers this frame -- the component renders the stale data from `lastContainer.current` for one frame, then the effect navigates to the new URL, which triggers a re-render with the correct container.

---

## 4. Component Changes

| Action | File Path | Purpose |
|--------|-----------|---------|
| MODIFY | `web/src/hooks/useDevices.ts` | (1) Replace `invalidateQueries` with targeted `setQueryData` for docker_update on device detail cache. (2) Add reconnection detection via `prevConnectedRef` and `useEffect` that invalidates device detail queries on reconnect. |
| MODIFY | `web/src/pages/ContainerDetailPage.tsx` | Add `useNavigate` import, name-based container fallback lookup, `useEffect` for navigation on container recreation. |
| CREATE | `web/src/hooks/useDevices.test.ts` | Tests for docker_update handler and reconnection invalidation logic. |
| CREATE | `web/src/pages/ContainerDetailPage.test.tsx` | Tests for container recreation navigation and "container not found" display logic. |

---

## 5. Data Model Changes

None. No schema changes, no migrations, no new data structures. All changes are in frontend cache management and UI logic.

---

## 6. API / Interface Contract

No new or modified APIs. All changes are client-side. The existing server endpoints and WS message formats are unchanged.

---

## 7. Sequence / Flow

### Flow 1: docker_update for a significant action (e.g., "stop")

1. Server broadcasts `{ type: "docker_update", device_id: "xxx", data: { container_id: "abc", container_name: "my-app", action: "stop" } }` via WS.
2. `useDevices` `handleWS` receives the message.
3. Handler checks: `action` is "stop", which is in the significant list.
4. Handler calls `queryClient.setQueryData(['device', 'xxx'], ...)`:
   - Reads `old.latest_telemetry.data.docker.containers`
   - Finds the container where `name === "my-app"` (stripping leading `/` from `container_name` if present)
   - Sets that container's `state` to `"exited"`
   - Returns the updated cache entry
5. Handler calls `queryClient.invalidateQueries({ queryKey: ['devices'] })` (unchanged fleet list invalidation).
6. The container detail page re-renders with `state: "exited"`.

### Flow 2: docker_update for a non-significant action (e.g., "health_status")

1. Server broadcasts `{ type: "docker_update", device_id: "xxx", data: { container_id: "abc", container_name: "my-app", action: "health_status" } }`.
2. `useDevices` `handleWS` receives the message.
3. Handler checks: `action` is not in the significant list.
4. Handler calls `queryClient.invalidateQueries({ queryKey: ['devices'] })` (fleet list only).
5. No change to `['device', 'xxx']` cache. No HTTP request to the device detail API.

### Flow 3: WS disconnects and reconnects

1. WS `onclose` fires. `WebSocketProvider` sets `connected = false`.
2. `useDevices` re-renders. `connected` is now `false`.
3. The `useEffect` in `useDevices` runs. `prevConnectedRef.current` is `true`, `connected` is `false`. This is a disconnect, not a reconnect. Update `prevConnectedRef.current = false`. No invalidation.
4. Page-level queries (e.g., `ContainerDetailPage`) detect `wsConnected === false` and switch `refetchInterval` to `15_000`.
5. After ~3 seconds, WS reconnects. `WebSocketProvider` sets `connected = true`.
6. `useDevices` re-renders. `connected` is now `true`.
7. The `useEffect` runs. `prevConnectedRef.current` is `false`, `connected` is `true`. This is a reconnect. Call `queryClient.invalidateQueries({ queryKey: ['device'], exact: false })`. Update `prevConnectedRef.current = true`.
8. All active device detail queries refetch. Fresh data arrives from the server.
9. Page-level queries detect `wsConnected === true` and switch `refetchInterval` to `false`.

### Flow 4: Container recreation (same name, new ID)

1. User is viewing `/devices/xxx/containers/abc123` (container name "my-app", short_id "abc123").
2. `lastContainer.current` holds the `ContainerInfo` for "my-app" with `short_id: "abc123"`.
3. Container is recreated on the Docker host. Old container destroyed, new one created with `short_id: "def456"` and `name: "my-app"`.
4. A telemetry WS push arrives. The `setQueryData` merge updates the `['device', 'xxx']` cache. The container list now contains "my-app" with `short_id: "def456"`. The old "abc123" is gone.
5. `ContainerDetailPage` re-renders. `cid` from URL params is still "abc123".
6. `containers.find(c => c.short_id === cid)` returns `undefined`.
7. `lastContainer.current` still holds the old container with `name: "my-app"`.
8. The component renders using `lastContainer.current` (no flicker).
9. The `useEffect` fires. It detects: `found` is `undefined`, `lastContainer.current.name` is "my-app". It searches containers for `name === "my-app"` and finds `short_id: "def456"`.
10. `navigate(\`/devices/xxx/containers/def456\`, { replace: true })` is called.
11. The URL updates. `cid` is now "def456". `containers.find(c => c.short_id === 'def456')` succeeds. `lastContainer.current` is updated to the new container. Page displays the new container.

---

## 8. Acceptance Criteria Mapping

| AC ID | Fulfilled By | Test Strategy |
|-------|-------------|---------------|
| AC-001 | `useDevices.ts`: Remove `invalidateQueries` for `['device', deviceId]` in docker_update handler; replace with `setQueryData` for significant actions only | Unit: Verify that after a docker_update with action "stop", the device detail cache is updated via `setQueryData` and `invalidateQueries` is NOT called on the device detail key |
| AC-002 | `useDevices.ts`: Non-significant actions (healthcheck) skip the device detail cache entirely | Unit: Verify that after a docker_update with action "health_status", neither `setQueryData` nor `invalidateQueries` is called on the device detail key |
| AC-003 | `useDevices.ts`: Telemetry WS handler (existing, unchanged) merges data into cache via `setQueryData` | Unit: Verify telemetry WS message updates container CPU/memory/status in the device detail cache |
| AC-004 | `useDevices.ts`: `useEffect` with `prevConnectedRef` detects reconnect and calls `invalidateQueries({ queryKey: ['device'], exact: false })` | Unit: Simulate `connected` transitioning from `false` to `true` and verify `invalidateQueries` is called with the correct query key |
| AC-005 | `useDevices.ts`: Same reconnect handler as AC-004; page-level `refetchInterval` toggles to `false` when `wsConnected` is `true` (existing behavior) | Unit: Verify that after reconnect invalidation, the query refetches, and subsequent telemetry WS messages update the cache without polling |
| AC-006 | `ContainerDetailPage.tsx`: Name-based fallback lookup in `useEffect`, `navigate(..., { replace: true })` | Unit: Render with a container list where the URL's `short_id` is absent but a container with the same name exists with a different `short_id`; verify `navigate` is called with the new URL and `replace: true` |
| AC-007 | `ContainerDetailPage.tsx`: When neither `short_id` nor `name` match and `lastContainer.current` is null, render "Container not found" | Unit: Render with empty container list and no lastContainer; verify "Container not found" text is displayed |
| AC-008 | `useDevices.ts`: docker_update handler does not call `invalidateQueries` on device detail key for any action (significant or non-significant) | Unit: Verify for every action type that `invalidateQueries` is never called with `['device', deviceId]` |

---

## 9. Error Handling

| Failure Mode | Handling | HTTP/UI Behavior |
|-------------|----------|-----------------|
| `setQueryData` callback receives `old` as `undefined` (no cached device detail) | Return `undefined` unchanged; the `if (!old) return old` guard (existing pattern) handles this | No visible effect -- the cache simply isn't updated |
| `container_name` in docker_update has leading `/` (Docker sometimes includes it) | Strip leading `/` before matching: `const name = evt.container_name.replace(/^\//, '')` | Correct match against `ContainerInfo.name` which never has a leading `/` |
| Container not found by name in `setQueryData` (e.g., new container not yet in cache) | Return the cache unchanged; telemetry will add the container within 60 seconds | No visible effect |
| `lastContainer.current` is null when name-based fallback is attempted | Skip the fallback; fall through to "Container not found" | Display "Container not found" with "Back to Containers" link |
| Navigation to new container URL fails (route does not exist) | Cannot happen -- the route pattern `/devices/:id/containers/:cid` is a wildcard that accepts any `cid` | N/A |

---

## 10. Security Considerations

No security implications. All changes are client-side cache management and navigation. No new data is exposed. No new API calls are introduced. The WS message format is unchanged.

---

## 11. Performance Considerations

- **Eliminated API calls:** The `invalidateQueries` call for `['device', deviceId]` on significant docker_update events is removed. For a device with 48 containers, this eliminates up to ~96 unnecessary refetches per minute (container start/stop events during deployments).
- **Retained fleet list invalidation:** The `invalidateQueries({ queryKey: ['devices'] })` call remains for docker_update events. This is acceptable because the fleet list endpoint is lightweight and the invalidation is debounced by React Query's default behavior.
- **Reconnect invalidation scope:** On reconnect, all cached device detail queries are invalidated. This is bounded by the number of device detail pages the user has visited (typically 1-3). React Query's `staleTime` and automatic garbage collection prevent unbounded growth.
- **No new polling intervals, timers, or persistent data structures.**
- **No new indexes or database queries.**

---

## 12. Implementation Notes for Engineers

### useDevices.ts changes

1. **Import `useEffect` and `useRef`** (add to existing import from `'react'`; `useCallback` is already imported, add `useEffect, useRef`).

2. **docker_update handler** (replace lines 95-104):

   The new handler shape:
   ```
   else if (msg.type === 'docker_update' && msg.device_id) {
     const evt = msg.data as DockerEvent
     const significant = ['start', 'stop', 'die', 'create', 'destroy', 'pause', 'unpause']
     if (evt?.action && significant.includes(evt.action)) {
       // Map action to container state
       const stateMap: Record<string, string> = {
         start: 'running',
         stop: 'exited',
         die: 'exited',
         pause: 'paused',
         unpause: 'running',
       }
       const newState = stateMap[evt.action]
       if (newState) {
         const name = evt.container_name?.replace(/^\//, '')
         queryClient.setQueryData(['device', msg.device_id], (old: any) => {
           if (!old?.latest_telemetry?.data?.docker?.containers) return old
           const containers = old.latest_telemetry.data.docker.containers as ContainerInfo[]
           const idx = containers.findIndex((c: ContainerInfo) => c.name === name)
           if (idx < 0) return old
           const updatedContainers = [...containers]
           updatedContainers[idx] = { ...updatedContainers[idx], state: newState }
           return {
             ...old,
             latest_telemetry: {
               ...old.latest_telemetry,
               data: {
                 ...old.latest_telemetry.data,
                 docker: {
                   ...old.latest_telemetry.data.docker,
                   containers: updatedContainers,
                 },
               },
             },
           }
         })
       }
     }
     // Fleet list invalidation remains for all docker_update events
     queryClient.invalidateQueries({ queryKey: ['devices'] })
   }
   ```

   Import `DockerEvent` from `'../types/models'` (add to existing import) and `ContainerInfo` (add to existing import).

3. **Reconnection detection** (add after the `handleWS` callback, before the `useWebSocket` call):

   ```
   const prevConnectedRef = useRef(true)
   ```

   Note: initialized to `true` to prevent spurious invalidation on initial connect.

   After the `const { connected } = useWebSocket(handleWS)` line, add:

   ```
   useEffect(() => {
     if (connected && !prevConnectedRef.current) {
       queryClient.invalidateQueries({ queryKey: ['device'], exact: false })
     }
     prevConnectedRef.current = connected
   }, [connected, queryClient])
   ```

   The `prevConnectedRef` must be declared inside the component body but outside the `handleWS` callback (it is not a dependency of the callback).

### ContainerDetailPage.tsx changes

1. **Add imports:** `useNavigate` from `'react-router-dom'`, `useEffect` from `'react'`.

2. **Add `navigate`:** `const navigate = useNavigate()` near the top of the component.

3. **Add name-based fallback lookup and navigation effect:**

   After the existing `const container = found ?? lastContainer.current` line, add:

   ```
   useEffect(() => {
     if (found || !lastContainer.current || !containers.length) return
     const match = containers.find(c => c.name === lastContainer.current!.name)
     if (match && match.short_id !== cid) {
       navigate(`/devices/${id}/containers/${match.short_id}`, { replace: true })
     }
   }, [containers, found, cid, id, navigate])
   ```

   **Important:** The dependency array includes `containers`. Because the telemetry WS handler creates a new array reference on every update, this effect will re-evaluate on every telemetry push. This is correct -- we need to re-check on every cache update. The early return (`if (found)`) makes the common case (container exists) a no-op.

4. **Keep the `lastContainer` ref and the existing `found ?? lastContainer.current` fallback.** Do not remove it.

### Testing patterns

- Tests are co-located (e.g., `useDevices.test.ts` next to `useDevices.ts`).
- Use vitest (`describe`, `it`, `expect`, `vi`).
- Use `@testing-library/react` for rendering.
- Use `MemoryRouter` + `Routes` + `Route` for routing context.
- Use `QueryClientProvider` with `retry: false` for React Query context.
- Mock `../api/client` and `../hooks/useDevices` as needed.
- Mock `../contexts/WebSocketProvider` or `../hooks/useWebSocket` to control `connected` state.
- For `useDevices.test.ts`, test the `handleWS` callback by extracting it or by simulating WS messages through the mocked WebSocket provider.

### Gotchas

- **`container_name` leading slash:** Docker event `container_name` sometimes includes a leading `/`. The Go `DockerEvent` struct's `ContainerName` field receives whatever the Docker API sends. The `ContainerInfo.name` field in telemetry never has a leading `/` (the agent strips it). The handler MUST strip the leading `/` from `container_name` before matching.

- **`create` and `destroy` actions:** These are in the significant list but do NOT map to a state change. `create` means a container was created but not started -- it may not be in the cache yet. `destroy` means it was removed, but per BR-003, we do not remove containers from cache on docker events; we wait for telemetry confirmation. The `stateMap` lookup returns `undefined` for these actions, and the `if (newState)` guard skips the `setQueryData` call. The fleet list invalidation still fires.

- **`exact: false` on reconnect invalidation:** The call is `queryClient.invalidateQueries({ queryKey: ['device'], exact: false })`. The `exact: false` is the default, but being explicit improves readability. This matches `['device', 'xxx']` for any `xxx` but does NOT match `['devices']` (the fleet list key). Verify this in tests.

- **React Query `setQueryData` immutability:** Always return new objects/arrays, never mutate in place. The implementation notes above follow this pattern with spread operators.

---

## 13. Definition of Done

- [ ] All component changes in Section 4 implemented
- [ ] `invalidateQueries` for `['device', deviceId]` removed from docker_update handler
- [ ] `setQueryData` with state mapping added for significant docker_update actions
- [ ] Reconnection detection (`prevConnectedRef` + `useEffect`) added to `useDevices`
- [ ] Name-based container fallback with `navigate(..., { replace: true })` added to `ContainerDetailPage`
- [ ] All AC mappings in Section 8 have corresponding tests with AC references
- [ ] Full test suite is green (`cd web && npm run test:run`)
- [ ] No new linting errors introduced
- [ ] No `console.log` or debug statements left in code
- [ ] Fleet list invalidation (`['devices']`) on docker_update is preserved (regression check)
