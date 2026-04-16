# Formal Requirements Document

| Field   | Value                                                  |
|---------|--------------------------------------------------------|
| Story ID | CONTAINER-VIEW                                        |
| Title    | Fix container detail page stability: race conditions, stale data, and recreation handling |
| Author   | Business Developer Agent                              |
| Date     | 2026-04-16                                            |
| Status   | FINAL                                                 |

---

## 1. Executive Summary

The container detail page (`/devices/:id/containers/:cid`) suffers from three bugs that cause it to crash, freeze, or display stale data. These bugs are triggered under normal operating conditions for users running many Docker containers (48+). This story fixes all three: (1) a race condition between telemetry WebSocket cache merges and docker_update-triggered refetches, (2) missing data refresh on WebSocket reconnection, and (3) incorrect "container not found" when a container is recreated with a new ID but the same name.

---

## 2. Background & Context

The rIOt dashboard receives real-time container data through two mechanisms:

- **Telemetry WebSocket messages** arrive approximately every 60 seconds per device and contain the full container list (with stripped heavy fields). The frontend merges these into the React Query cache via `setQueryData`.
- **Docker event WebSocket messages (`docker_update`)** fire on container lifecycle events (start, stop, create, destroy, healthcheck, etc.). Currently, "significant" docker events (start, stop, die, create, destroy, pause, unpause) trigger `invalidateQueries` on the device detail query key, causing a full refetch from the server API.

With 48 containers each producing healthcheck events every ~30 seconds, plus telemetry pushes every ~60 seconds, the two cache update mechanisms frequently collide. The `invalidateQueries` refetch can overwrite the WS-merged cache with stale server data, or the WS merge can overwrite the refetch result. Either scenario can cause the container list to momentarily be incomplete, leading to a "container not found" flash on the detail page.

Additionally, when the WebSocket connection drops and reconnects, React Query does not automatically refetch when the `refetchInterval` changes from `15_000` (polling fallback) to `false` (WS active). The page stays frozen on stale data until the next telemetry WS message arrives (up to 60 seconds).

Finally, when a container is recreated via `docker compose up -d`, the old container is destroyed and a new one is created with a different `short_id` but the same `name`. The container detail page URL contains the old `short_id`, which no longer exists in the container list. The page currently shows "container not found" instead of navigating to the replacement container.

---

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Dashboard User | Authenticated user viewing the container detail page | Read access to device detail and container data |
| Telemetry WebSocket | Server-side push of telemetry snapshots every ~60s per device | Pushes telemetry data to all connected dashboard clients |
| Docker Event WebSocket | Server-side push of Docker lifecycle events in real time | Pushes docker_update messages to all connected dashboard clients |
| Device Detail API | REST endpoint returning device info + latest telemetry | Responds to GET requests from authenticated clients |

---

## 4. Functional Requirements

### Bug 1: Race condition between telemetry WS merge and docker_update refetch

- **FR-001**: The system must NOT call `invalidateQueries` or trigger a refetch of the device detail query (`['device', deviceId]`) in response to `docker_update` WebSocket messages.
- **FR-002**: The system must use the telemetry WebSocket push as the single authoritative source for updating the device detail query cache.
- **FR-003**: When a `docker_update` WebSocket message with a "significant" action (start, stop, die, create, destroy, pause, unpause) is received, the system must apply the event data directly to the cached container's state via `setQueryData` rather than triggering a refetch.
- **FR-004**: When a `docker_update` WebSocket message with a non-significant action (e.g., healthcheck) is received, the system must NOT modify the device detail query cache.
- **FR-005**: The fleet list query (`['devices']`) must continue to be invalidated on `docker_update` messages (existing behavior, unchanged).

### Bug 2: WS reconnect does not trigger a refetch

- **FR-006**: When the WebSocket connection transitions from disconnected to connected, the system must invalidate all active device detail queries so they refetch fresh data from the server.
- **FR-007**: The refetch triggered by WS reconnection must complete within 5 seconds of the reconnection event under normal network conditions.

### Bug 3: Container recreation navigates to new container

- **FR-008**: When the container detail page cannot find a container matching the URL's `short_id` in the current container list, the system must attempt to find a container with the same `name` as the last successfully displayed container.
- **FR-009**: When a container with a matching name is found (per FR-008) and that container has a different `short_id` than the URL parameter, the system must navigate the browser to the new container's URL (`/devices/:id/containers/:newCid`), replacing the current history entry.
- **FR-010**: When no container is found by either `short_id` or `name` lookup, and the `lastContainer` ref also has no value, the system must display "Container not found."
- **FR-011**: The system must NOT display "Container not found" while a matching container exists in the container list by either `short_id` or `name`.
- **FR-012**: The name-based fallback lookup (FR-008) must use the container `name` field, not the `riot.name` display label.

---

## 5. Non-Functional Requirements

- **NFR-001** [Performance]: The total number of HTTP API requests to the device detail endpoint must not increase as a result of these changes. Docker healthcheck events (~1 per container per 30s, so ~96/min for 48 containers) must not trigger any API requests.
- **NFR-002** [Performance]: The container detail page must update displayed CPU, memory, status, and uptime data within 1 second of receiving a telemetry WebSocket message.
- **NFR-003** [Memory]: The changes must not introduce additional polling intervals, duplicate data caches, or persistent data structures beyond the existing `lastContainer` ref.
- **NFR-004** [Responsiveness]: After WS reconnection, fresh data must be visible on the container detail page within 5 seconds.
- **NFR-005** [Stability]: The container detail page must not flash "container not found" during normal operation (continuous WS connection, no container removal).

---

## 6. Business Rules

- **BR-001**: The telemetry WebSocket push is the authoritative source of truth for the device detail cache. All other update mechanisms must yield to it.
- **BR-002**: A container is considered "the same container" across a recreation event if and only if it has the same `name` field value on the same device.
- **BR-003**: A container is considered "genuinely removed" only when a telemetry WebSocket push arrives that does not include the container by either `short_id` or `name`.
- **BR-004**: Direct cache mutation via `setQueryData` is preferred over `invalidateQueries` + refetch for the device detail query key to avoid race conditions.

---

## 7. Data Requirements

### Entities Involved

| Entity | Key Fields | Role |
|--------|-----------|------|
| `ContainerInfo` | `short_id`, `name`, `state`, `status`, `cpu_percent`, `mem_usage`, `mem_limit` | Container data displayed on the detail page |
| `DeviceDetailResponse` | `device`, `latest_telemetry` | Cache entry for the device detail query |
| `DockerEvent` (WS) | `container_id`, `container_name`, `action` | Docker lifecycle event received via WS |
| `WSMessage` | `type`, `device_id`, `data` | WebSocket message envelope |

### State Transitions: Container Detail Page

| Current State | Event | Next State |
|---------------|-------|------------|
| Displaying container (by short_id) | Telemetry WS arrives with container present | Displaying container (updated data) |
| Displaying container (by short_id) | Telemetry WS arrives without container, but name match exists with new short_id | Navigate to new container URL |
| Displaying container (by short_id) | Telemetry WS arrives without container, no name match | Display "Container not found" |
| Displaying container (by short_id) | docker_update (significant action) for this container | Update container state field in cache |
| Displaying container (by short_id) | docker_update (healthcheck) | No change |
| Displaying container (by short_id) | WS disconnects | Switch to polling fallback (15s) |
| Polling fallback | WS reconnects | Invalidate + refetch, then resume WS-only updates |
| Displaying "Container not found" | Telemetry WS arrives with name match | Navigate to matching container URL |

### Validation Rules

- The `name` field on `ContainerInfo` is always present and non-empty (Docker guarantees this).
- The `short_id` field on `ContainerInfo` is always present and non-empty.
- The `action` field on `DockerEvent` is always present.

---

## 8. Acceptance Criteria

### AC-001: Container detail page does not flash "container not found" during normal telemetry WS updates
**Maps to:** FR-001, FR-002, FR-004

```
Given: The user is viewing a container detail page for a running container
When: A telemetry WebSocket message arrives containing that container's data
Then: The page continues to display the container's detail
And: The page does not momentarily display "Container not found"
```

### AC-002: Container detail page does not flash "container not found" during Docker healthcheck events
**Maps to:** FR-001, FR-004

```
Given: The user is viewing a container detail page for a running container
When: A docker_update WebSocket message with action "health_status" arrives for that container
Then: The page continues to display the container's detail
And: No HTTP request is made to the device detail API endpoint
And: The page does not momentarily display "Container not found"
```

### AC-003: Container detail page updates CPU, memory, status, and uptime on each telemetry WS push
**Maps to:** FR-002

```
Given: The user is viewing a container detail page
When: A telemetry WebSocket message arrives with updated container metrics
Then: The displayed CPU percentage reflects the new value
And: The displayed memory usage reflects the new value
And: The displayed container status reflects the new value
And: The displayed uptime reflects the new value
And: These updates occur within 1 second of message receipt
```

### AC-004: After WS disconnect and reconnect, the container detail page refetches fresh data within 5 seconds
**Maps to:** FR-006, FR-007

```
Given: The user is viewing a container detail page
And: The WebSocket connection is currently disconnected (polling fallback active)
When: The WebSocket connection is re-established
Then: The system invalidates the device detail query
And: Fresh data is fetched from the server API
And: The page displays the fresh data within 5 seconds of reconnection
```

### AC-005: After WS disconnect and reconnect, the container detail page resumes live updates
**Maps to:** FR-006

```
Given: The user is viewing a container detail page
And: The WebSocket has just reconnected after a disconnection
When: The next telemetry WebSocket message arrives
Then: The page updates with the new telemetry data
And: No polling requests are made to the server API
```

### AC-006: When a container is recreated (same name, new ID), the container detail page navigates to the new container automatically
**Maps to:** FR-008, FR-009, FR-012

```
Given: The user is viewing a container detail page at URL /devices/:id/containers/:oldCid
And: The container has name "my-app"
When: The container is recreated (destroyed and created with a new short_id but the same name "my-app")
And: A telemetry WebSocket message arrives reflecting the new container list
Then: The page navigates to /devices/:id/containers/:newCid (where newCid is the new container's short_id)
And: The navigation replaces the current history entry (no back-button loop)
And: The page displays the new container's details
```

### AC-007: When a container is genuinely removed, the page shows "container not found" after telemetry confirms removal
**Maps to:** FR-010, FR-011

```
Given: The user is viewing a container detail page for container "my-app"
When: The container is removed (not recreated) from the Docker host
And: A telemetry WebSocket message arrives that does not contain any container with the same short_id or name
Then: The page displays "Container not found"
And: A "Back to Containers" link is displayed
```

### AC-008: No increase in API requests to the server from docker healthcheck events
**Maps to:** FR-001, FR-004, NFR-001

```
Given: A device with 48 containers, each producing healthcheck events every ~30 seconds
And: The user is viewing any page in the dashboard
When: Docker healthcheck events arrive via WebSocket
Then: No HTTP requests are made to the device detail API endpoint as a result of these events
And: No HTTP requests are made to the device detail API endpoint as a result of significant docker events (start, stop, die, create, destroy, pause, unpause)
```

---

## 9. Out of Scope

- Changes to the server-side WebSocket broadcast logic or telemetry stripping behavior.
- Changes to the telemetry push interval (remains ~60 seconds).
- Changes to the Docker event detection or filtering on the agent side.
- Changes to the fleet overview page's handling of docker_update events (fleet list invalidation remains).
- Adding container-level WebSocket subscriptions or per-container API endpoints.
- Handling containers that share the same `name` on the same device (Docker does not allow this).
- Changes to the container list page (`DeviceContainers`); this story covers only the container detail page, though the cache changes in `useDevices` affect both.
- Backend API changes.
- Database schema changes.

---

## 10. Assumptions

- **A-001**: The telemetry WebSocket push always includes the complete container list for the device (all running and stopped containers), even when Docker data is "stripped" for bandwidth. Specifically, the `docker.containers` array is always present and complete in telemetry WS messages.
- **A-002**: Container names are unique per device (enforced by Docker).
- **A-003**: The `docker_update` WebSocket message `data` field always includes `container_name` and `action` fields, as defined by the `DockerEvent` type.
- **A-004**: The WebSocket reconnection delay is approximately 3 seconds (current implementation), and a refetch triggered on reconnection will complete within 2 additional seconds under normal conditions, totaling under 5 seconds.
- **A-005**: The `lastContainer` ref pattern already in use on the container detail page is sufficient to prevent flicker during the brief window between a cache update that removes the container and the subsequent name-based navigation.

---

## 11. Open Questions

None. All three bugs have well-defined root causes, fix directions, and acceptance criteria.

---

## 12. Dependencies

| Dependency | Description |
|------------|-------------|
| WebSocket Provider (`WebSocketProvider`) | The `connected` state must accurately reflect the WebSocket connection status. The current implementation already tracks this. |
| React Query cache | All three fixes operate on the React Query cache for the `['device', deviceId]` query key. No changes to the query key structure are needed. |
| React Router `useNavigate` | Required for the container recreation navigation (Bug 3). Already available in the routing context. |
| `DockerEvent` type | The `docker_update` WS message data must conform to the `DockerEvent` interface, which includes `container_name` and `action`. Already defined in the codebase. |
