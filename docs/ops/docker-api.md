# Docker area API

All HTTP endpoints below require the existing administrator authentication. Browser writes use the existing same-origin protection. Device IDs are canonical UUIDs; names and projects are validated independently. Agents advertise additive capability tokens, and the server enforces its own bounds regardless of those claims. Unsupported features return a structured error so older agents remain visible.

| Method and path | Purpose |
|---|---|
| `GET /api/v1/docker/hosts` | Host capability and Docker availability projection |
| `GET /api/v1/fleet/containers` | Existing fleet container projection used by the Docker table |
| `POST /api/v1/docker/bulk-actions` | Bounded lifecycle commands, with per-target acceptance/conflict results |
| `GET /api/v1/devices/{id}/docker/containers/{name}/logs` | Stored, masked history |
| `GET /api/v1/devices/{id}/docker/stacks/{project}/logs` | Stored history across current or retained stack members |
| `GET /api/v1/devices/{id}/docker/containers/{name}/inspect` | Masked, read-only agent inspection |
| `GET /api/v1/docker/stacks` | Fleet stack list, including retained Down stacks |
| `GET /api/v1/docker/stacks/{deviceId}/{project}` | Stack details and agreed/observed paths |
| `POST /api/v1/devices/{id}/docker/stacks/{project}/actions` | Stack lifecycle operation |
| `POST /api/v1/devices/{id}/docker/stacks/{project}/paths/acknowledge` | Explicitly acknowledge observed Compose paths |
| `POST /api/v1/devices/{id}/docker/stacks/{project}/forget` | Forget a retained Down stack |
| `GET /api/v1/devices/{id}/docker/stacks/{project}/compose` | Read-only Compose file index |
| `GET /api/v1/devices/{id}/docker/stacks/{project}/compose/files/{index}` | Masked original Compose content |
| `GET /api/v1/devices/{id}/docker/stacks/{project}/env` | Masked environment file read |
| `GET /api/v1/docker/inventory` | Latest bounded images, volumes and networks inventory |
| `POST /api/v1/devices/{id}/docker/inventory/refresh` | Request an opt-in inventory refresh |
| `POST /api/v1/devices/{id}/docker/prune/preview` | Create a short-lived candidate snapshot |
| `POST /api/v1/devices/{id}/docker/prune/execute` | Consume that preview once and remove selected candidates |
| `GET /api/v1/docker/activity` | Filtered Docker command history |
| `GET /api/v1/docker/activity/commands/{commandId}` | Actor, steps and per-target outcomes |
| `WS /ws/docker/logs/{deviceId}` | Explicit browser live-log session |

Stored history uses `from`, `to`, `before`, and `limit` query parameters. `after` is accepted for forward catch-up, and takes precedence over `from`. `before` is the opaque cursor returned by the preceding response. Replies include effective retention information. Agent-asserted timestamps outside the allowed window are restamped, with the original retained in `agent_ts`. Log text is masked at ingestion and again at output where required; masking covers known patterns and does not guarantee that arbitrary log text contains no sensitive information.

The live socket requires an exact Origin and an unexpired administrator session. The client attaches explicitly, supplies container names and optional history boundaries, and receives session, data, marker and error frames. Server follows are shared by viewers; backfills are coalesced only for matching boundaries. One browser cannot stop another viewer's follow. Queues, frames, throughput, per-host streams and session lifetimes are bounded. An expired session receives `session_expired` and closes.

Cleanup previews do not call an unbounded Docker prune operation. Execution uses the preview token, selected IDs and required confirmations; the agent checks each selected item's current usage again. Changed/in-use items are skipped with per-item results. Inventory collection must be enabled in agent configuration. Built-in networks and the rIOt server/database resources receive additional protection.

See the [FRD](../requirements/DOCKER-UX-001-frd.md) and [architecture](../architecture/DOCKER-UX-001-add.md) for complete request/response contracts and limits, and the [upgrade procedure](docker-upgrade.md) before applying migrations to an existing installation.
