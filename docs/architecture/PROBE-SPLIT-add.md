# Architecture Decision Document

- **Story ID:** PROBE-SPLIT
- **FRD Reference:** docs/requirements/PROBE-SPLIT-frd.md
- **Author:** Architect Agent
- **Date:** 2026-03-20
- **Status:** FINAL

---

## 1. Summary

The Probes page (`/probes`) will be restructured into a two-section layout matching the AlertRuleSettings pattern: a "Server Probes" section at the top (using existing data) and a "Device Probes" section below (using a new backend endpoint). A new `GET /api/v1/device-probes` endpoint will be created to return all device probes across all devices in a single call, enriched with device hostname. The frontend Probes page will be rewritten to render both sections with table-based layouts, reusing existing modals and API mutation functions.

---

## 2. Technical Context

### What exists
- **Server Probes page** (`web/src/pages/Probes.tsx`): card-based layout listing all server probes from `GET /api/v1/probes`. Has Add/Edit/Delete/Run/Toggle functionality using `ProbeModal`.
- **Device Probes page** (`web/src/pages/DeviceProbes.tsx`): per-device page at `/devices/:id/probes` with card-based layout. Has full CRUD, run, toggle, and inline result history. Uses `DeviceProbeModal`.
- **AlertRuleSettings** (`web/src/pages/settings/AlertRuleSettings.tsx`): two-section pattern with "Global Alert Rules" and "Device-Specific Alert Rules", each with their own heading, Add button, and table. This is the reference pattern.
- **Backend**: Device probes are scoped per-device via `GET /api/v1/devices/{id}/device-probes`. There is no endpoint that returns all device probes across all devices.
- **Repository**: `DeviceProbeRepo.List(ctx, deviceID)` only queries by device_id. No `ListAll` method exists.
- **Interface**: `DeviceProbeRepository` in `internal/server/db/interfaces.go` has no `ListAll` method.
- **Mock**: `MockDeviceProbeRepo` in `internal/testutil/mocks.go` mirrors the interface.
- **Frontend types**: `DeviceProbeWithResult` in `web/src/types/models.ts` does not include a `device_hostname` field.
- **API client**: `web/src/api/client.ts` has per-device probe methods but no all-device-probes method.

### What's missing
- A backend method to query all device probes regardless of device_id.
- A handler to serve `GET /api/v1/device-probes`.
- A frontend type that includes device hostname alongside device probe data.
- Frontend code to render both sections on the `/probes` page.

### What needs to change
- `DeviceProbeRepo` needs a `ListAll` method.
- `DeviceProbeRepository` interface needs `ListAll` added.
- `MockDeviceProbeRepo` needs `ListAll` added.
- A new handler `ListAllDeviceProbes` must be added.
- The router must register the new endpoint.
- The frontend Probes page must be rewritten.
- The frontend API client needs a new function.
- The frontend types need a new enriched type.

---

## 3. Architecture Decisions

### AD-001: New backend endpoint for all device probes (OQ-001)

**Decision:** Create a new `GET /api/v1/device-probes` endpoint that returns all device probes across all devices in a single response, with each probe enriched with `device_hostname`.

**Rationale:** The alternative (frontend aggregation) would require fetching the device list, then making N requests for each device's probes. For 50 devices, that's 51 HTTP requests on every page load and every 30-second refresh. A single backend endpoint is O(1) requests, simpler frontend code, and better performance. The backend already has access to both the device repo and device probe repo, so the join is trivial.

**Alternatives Considered:**
- *Frontend aggregation (fetch devices, then N+1 calls)*: Rejected. Poor performance, complex frontend orchestration, race conditions on partial failures, and 51 requests every 30 seconds for a 50-device fleet.
- *SQL JOIN in a single query*: Considered but rejected in favor of application-level enrichment (fetch all probes, fetch all devices, merge in Go). This is consistent with how `ListDeviceProbes` already enriches probes with latest_result and success_rate in the handler. A SQL JOIN would require a new scan function and deviates from existing patterns.

**Consequences:** Adds one new endpoint to the API surface. The endpoint does not break any existing endpoints. The response shape is a superset of `DeviceProbeWithResult` with an added `device_hostname` field.

---

### AD-002: Response shape for the new endpoint

**Decision:** The new endpoint will return `[]DeviceProbeWithResultEnriched` where `DeviceProbeWithResultEnriched` embeds `DeviceProbeWithResult` and adds `DeviceHostname string`. This avoids modifying the existing `DeviceProbeWithResult` struct (which would affect the per-device endpoint's response shape, violating the "no existing endpoint changes" constraint).

**Rationale:** The FRD explicitly states existing API response shapes must not change (Out of Scope). A new struct keeps the per-device endpoint clean while providing hostname data for the unified page.

**Alternatives Considered:**
- *Add `DeviceHostname` to `DeviceProbeWithResult`*: Would add an always-empty field to the per-device endpoint responses. Violates FRD out-of-scope constraint.
- *Return device_id only, resolve hostname on frontend*: Would require the frontend to also fetch the device list and do a client-side join. Adds complexity for no benefit since the server already has device data.

**Consequences:** New Go struct `DeviceProbeWithResultEnriched` in models. New TypeScript interface on frontend.

---

### AD-003: UX for adding device probes from unified page (OQ-002)

**Decision:** The "Add Probe" button in the Device Probes section will open a device picker dropdown first, and upon device selection, navigate to that device's probes page (`/devices/:id/probes`) where the user can use the existing `DeviceProbeModal` creation flow.

**Rationale:** Embedding a device selector into the `DeviceProbeModal` would require modifying a shared component used by the per-device page, introducing conditional logic and risking regressions. A two-step flow (pick device, then navigate) is simple, avoids modal modifications, and the per-device page already has a polished creation UX. This is the lowest-risk approach.

**Alternatives Considered:**
- *Add device selector to DeviceProbeModal*: Higher risk, modifies a shared component, requires the modal to conditionally show/hide the device picker based on context. Rejected.
- *Show device picker first, then open DeviceProbeModal inline on the probes page*: Would require the Probes page to manage device-scoped mutations (create, which requires device_id in the URL). Adds significant complexity for marginal UX improvement. Rejected.

**Consequences:** The "Add Probe" button in the Device Probes section becomes a dropdown that lists devices. Clicking a device navigates away. This is slightly more friction than inline creation but avoids component modifications and is consistent with the principle that device probes are managed per-device.

---

### AD-004: No inline result history on unified page (OQ-003)

**Decision:** Device probes on the unified page will NOT support inline result history expansion. The per-device page (`/devices/:id/probes`) retains that functionality.

**Rationale:** Inline result history on the unified page would require fetching results for a specific probe, but the results endpoint is scoped to a device: `GET /api/v1/devices/{id}/device-probes/{pid}/results`. While technically possible, it adds complexity and the unified page is intended as an overview, not a deep-dive tool. Users who want result history can click through to the device or device probes page.

**Alternatives Considered:**
- *Add inline result history*: Adds UI complexity, additional API calls, and the unified page already has a lot of data density with two sections. Rejected for v1.
- *Link probe name to a device probe detail view*: No such view exists (out of scope per FRD). Rejected.

**Consequences:** Device probe rows on the unified page will not have a "History" button. The device hostname link provides navigation to the device, and from there the user can access probes.

---

### AD-005: Table-based layout for both sections

**Decision:** Both Server Probes and Device Probes sections will use table-based layouts (HTML `<table>`) inside a card container, matching the `RulesTable` pattern from `AlertRuleSettings.tsx`.

**Rationale:** The current Probes page uses card-based layout (grid of `ProbeCard` components). The FRD requires matching the AlertRuleSettings pattern, which uses tables. Tables are more information-dense and better suited for the overview nature of a unified page. The `RulesTable` component in AlertRuleSettings provides a proven pattern.

**Alternatives Considered:**
- *Keep card layout*: Inconsistent with AlertRuleSettings pattern. Rejected per FRD FR-016.
- *Mixed (cards for server, table for device)*: Inconsistent visual language on one page. Rejected.

**Consequences:** The Probes page will look different from its current card-based layout. This is the intended behavior per the FRD.

---

### AD-006: Device picker implementation for "Add Probe" button

**Decision:** Implement the device picker as a dropdown menu that appears on click, listing all devices by hostname. Each device entry navigates to `/devices/:id/probes` with no additional query params (the user uses the existing "Add Probe" button on that page).

**Rationale:** A simple dropdown is consistent with UI patterns used elsewhere in the dashboard (e.g., the "Create from Template" flow on AlertRules). It avoids introducing a modal for device selection.

**Alternatives Considered:**
- *Full modal with device list*: Over-engineered for a simple selection. Rejected.
- *Direct navigation without dropdown (just one button that goes to a device list)*: No clear UX target. Rejected.

**Consequences:** Requires fetching the device list (`GET /api/v1/devices`) for the dropdown. This data is already cached by react-query in most dashboard contexts, so no performance concern.

---

## 4. Component Changes

| Action | File Path | Purpose |
|--------|-----------|---------|
| MODIFY | `internal/models/device_probes.go` | Add `DeviceProbeWithResultEnriched` struct with `DeviceHostname` field |
| MODIFY | `internal/server/db/interfaces.go` | Add `ListAll(ctx context.Context) ([]models.DeviceProbe, error)` to `DeviceProbeRepository` |
| MODIFY | `internal/server/db/device_probe_repo.go` | Implement `ListAll` method (SELECT all device probes, no device_id filter) |
| MODIFY | `internal/server/handlers/device_probes.go` | Add `ListAllDeviceProbes` handler |
| MODIFY | `internal/server/router.go` | Register `GET /api/v1/device-probes` route |
| MODIFY | `internal/testutil/mocks.go` | Add `ListAll` method to `MockDeviceProbeRepo` |
| MODIFY | `web/src/pages/Probes.tsx` | Rewrite to two-section layout with Server Probes and Device Probes tables |
| MODIFY | `web/src/api/client.ts` | Add `getAllDeviceProbes` function |
| MODIFY | `web/src/types/models.ts` | Add `DeviceProbeWithResultEnriched` interface |

---

## 5. Data Model Changes

### New Go struct (no database migration required)

```
// DeviceProbeWithResultEnriched extends DeviceProbeWithResult with the device hostname
// for use in the all-device-probes list endpoint.
type DeviceProbeWithResultEnriched struct {
    DeviceProbeWithResult
    DeviceHostname string `json:"device_hostname"`
}
```

### New TypeScript interface

```
export interface DeviceProbeWithResultEnriched extends DeviceProbeWithResult {
    device_hostname: string
}
```

### New repository method

`ListAll(ctx context.Context) ([]models.DeviceProbe, error)` -- identical query to `List` but without the `WHERE device_id=$1` clause. Returns all device probes ordered by `id`.

### No database migration

No schema changes. The new `ListAll` method queries the existing `device_probes` table with no new columns or indexes.

---

## 6. API / Interface Contract

### New Endpoint: `GET /api/v1/device-probes`

**Authentication:** Admin JWT (cookie auth via `AdminAuth` middleware)

**Request:**
- Method: `GET`
- Path: `/api/v1/device-probes`
- Headers: JWT cookie (existing auth)
- Query params: none
- Body: none

**Response 200 OK:**
```json
[
  {
    "id": 1,
    "name": "Check nginx",
    "device_id": "abc-123",
    "type": "shell",
    "enabled": true,
    "config": { "command": "curl -s localhost" },
    "assertions": [],
    "interval_seconds": 60,
    "timeout_seconds": 10,
    "created_at": "2026-03-01T00:00:00Z",
    "updated_at": "2026-03-01T00:00:00Z",
    "latest_result": {
      "id": 42,
      "probe_id": 1,
      "device_id": "abc-123",
      "success": true,
      "latency_ms": 12.5,
      "output": {},
      "created_at": "2026-03-20T10:00:00Z"
    },
    "success_rate": 0.98,
    "total_checks": 100,
    "device_hostname": "web-server-01"
  }
]
```

**Response 500 Internal Server Error:**
```json
{"error": "failed to list all device probes"}
```

**Implementation notes:** The handler will:
1. Call `deviceProbeRepo.ListAll(ctx)` to get all probes.
2. Call `devices.List(ctx)` to get all devices.
3. Build a `deviceID -> hostname` map.
4. For each probe, enrich with latest_result, success_rate, total_checks (same pattern as `ListDeviceProbes`), and device_hostname from the map.
5. Return the enriched array.

---

## 7. Sequence / Flow

### Page load flow

1. User navigates to `/probes`.
2. React renders the new `Probes` component.
3. Two parallel react-query fetches fire:
   - `GET /api/v1/probes` (server probes, existing)
   - `GET /api/v1/device-probes` (all device probes, new)
4. Both have `refetchInterval: 30_000` for auto-refresh.
5. Server Probes section renders with server probe data.
6. Device Probes section renders with device probe data, showing device_hostname per row.

### Server probe CRUD flow (unchanged)

Same as current: Add/Edit opens `ProbeModal`, mutations use existing `api.createProbe`/`api.updateProbe`/`api.deleteProbe`/`api.runProbe`. On mutation success, invalidate `['probes']` query key.

### Device probe CRUD flow (from unified page)

- **Run:** Calls `api.runDeviceProbe(deviceId, probeId)`. On success, invalidate `['all-device-probes']`.
- **Edit:** Opens `DeviceProbeModal` with the probe data. Save calls `api.updateDeviceProbe(deviceId, probeId, data)`. On success, invalidate `['all-device-probes']`.
- **Delete:** Calls `api.deleteDeviceProbe(deviceId, probeId)`. On success, invalidate `['all-device-probes']`.
- **Toggle:** Calls `api.updateDeviceProbe(deviceId, probeId, { ...probe, enabled: !probe.enabled })`. On success, invalidate `['all-device-probes']`.
- **Add:** Opens device picker dropdown. User clicks a device. Navigates to `/devices/:id/probes`.

### Backend `GET /api/v1/device-probes` flow

1. Handler receives request.
2. Calls `deviceProbeRepo.ListAll(ctx)` -- returns `[]DeviceProbe`.
3. Calls `devices.List(ctx)` -- returns `[]Device`.
4. Builds `map[string]string` of device_id to hostname.
5. For each probe, calls `deviceProbeRepo.LatestResult(ctx, probe.ID)` and `deviceProbeRepo.SuccessRate(ctx, probe.ID)`.
6. Assembles `[]DeviceProbeWithResultEnriched`.
7. Returns JSON response.

---

## 8. Acceptance Criteria Mapping

| AC ID | Fulfilled By | Test Strategy |
|-------|-------------|---------------|
| AC-001 | `Probes.tsx`: Server Probes section renders from `useQuery(['probes'], api.getProbes)`. Table rows show name, type, target, interval, status, success rate, total checks, latency, check time. Name links to `/probes/:id`. Run/Edit/Delete buttons per row. | Frontend unit: render with mock server probes, assert section heading, table columns, link hrefs, button presence. |
| AC-002 | `Probes.tsx`: "Add Probe" button in Server Probes section header opens `ProbeModal` with `emptyProbe`. | Frontend unit: click Add Probe, assert ProbeModal renders. |
| AC-003 | `Probes.tsx`: Server Probes section shows empty state when `probes` array is empty. | Frontend unit: render with empty probes, assert empty message. |
| AC-004 | `Probes.tsx`: Device Probes section renders from `useQuery(['all-device-probes'], api.getAllDeviceProbes)`. Table rows show name, type, device hostname (linked to `/devices/:id`), enabled state, interval, status, success rate, total checks, latency. `ListAllDeviceProbes` handler enriches with device_hostname. | Backend unit: handler returns enriched data with hostname. Frontend unit: render with mock device probes, assert section heading, hostname links, all columns. |
| AC-005 | `Probes.tsx`: Run/Edit/Delete/Toggle buttons call `api.runDeviceProbe`, `api.updateDeviceProbe`, `api.deleteDeviceProbe` with correct `device_id` and `probe.id`. Mutations invalidate `['all-device-probes']`. | Frontend unit: click each action button, assert correct API call with device_id param. Backend unit: existing device probe handler tests cover the API calls themselves. |
| AC-006 | `Probes.tsx`: Device Probes section shows empty state when `allDeviceProbes` array is empty. | Frontend unit: render with empty device probes, assert empty message. |
| AC-007 | `Probes.tsx`: Page structure has Server Probes section first, Device Probes section second. Each section has its own heading (`<h2>`) and "Add Probe" button, with `mt-8` spacing between sections (matching AlertRuleSettings pattern). | Frontend unit: assert DOM order of sections, heading text, button text, spacing classes. |
| AC-008 | `Probes.tsx`: Both queries use `refetchInterval: 30_000`. | Frontend unit: assert query configuration includes refetchInterval. |
| AC-009 | No changes to `DeviceProbes.tsx`. | Verify file is unmodified (no component changes listed for this file). |

---

## 9. Error Handling

| Failure Mode | HTTP Status | Error Shape | Logging |
|-------------|-------------|-------------|---------|
| `ListAll` DB query fails | 500 | `{"error": "failed to list all device probes"}` | `slog.Error("list all device probes", "error", err)` |
| `devices.List` fails during enrichment | 500 | `{"error": "failed to list all device probes"}` | `slog.Error("list devices for device probes", "error", err)` |
| `LatestResult` fails for a probe | N/A (non-fatal) | Probe returned without latest_result | No log (matches existing `ListDeviceProbes` pattern which silently skips) |
| `SuccessRate` fails for a probe | N/A (non-fatal) | Probe returned without success_rate | No log (matches existing pattern) |
| Frontend fetch fails | N/A | react-query handles retry. Section shows loading state. | No additional handling needed. |

---

## 10. Security Considerations

- **Authentication:** The new endpoint `GET /api/v1/device-probes` is behind `AdminAuth` middleware (JWT cookie), consistent with all other admin-facing endpoints. No new auth surface.
- **Authorization:** Single-user system. The authenticated admin can see all device probes. No additional authorization logic needed.
- **Data exposure:** The endpoint returns device_hostname and probe config (which may contain commands, URLs, etc.). This is the same data already available via per-device endpoints. No new data exposure.
- **Input validation:** The endpoint is read-only (GET). No user input to validate.

---

## 11. Performance Considerations

### Backend

- **Query pattern for `ListAll`:** Single query `SELECT ... FROM device_probes ORDER BY id`. No WHERE clause, so it does a sequential scan. For the target scale (1,000 probes across 50 devices), this is negligible.
- **N+1 for enrichment:** The handler calls `LatestResult` and `SuccessRate` per probe (same pattern as existing `ListDeviceProbes`). For 1,000 probes, this is 2,000 additional queries. Each is a simple indexed lookup on `probe_id` with `LIMIT 1` or an aggregate. At homelab scale this is acceptable (sub-100ms total). If this becomes a bottleneck in the future, the enrichment can be batched into two bulk queries, but that optimization is not needed now.
- **Device list fetch:** `devices.List(ctx)` is called once per request. For 50 devices, negligible.
- **No new indexes required:** The `device_probe_results` table already has indexes on `probe_id` for the `LatestResult` and `SuccessRate` queries (these are used by the existing per-device endpoint).

### Frontend

- **Two parallel fetches:** Server probes and device probes fetch in parallel. No waterfall.
- **30-second refresh:** Both queries refresh every 30 seconds, consistent with current behavior.
- **Rendering 1,000 rows:** Standard HTML table. No virtualization needed at this scale. If probe counts grow significantly, pagination can be added in a future story (explicitly out of scope per FRD).
- **react-query caching:** The device list query (used by the device picker dropdown) uses `staleTime: 60_000` consistent with AlertRuleSettings usage.

---

## 12. Implementation Notes for Engineers

### Backend

1. **`ListAll` method:** Model it exactly on the existing `List` method but remove the `WHERE device_id=$1` clause. Use the same `scanDeviceProbes` helper.

2. **`ListAllDeviceProbes` handler:** Follow the exact enrichment pattern from `ListDeviceProbes` (lines 16-39 of `device_probes.go`). Additionally call `h.devices.List(ctx)` to build the hostname map, and return `[]models.DeviceProbeWithResultEnriched`.

3. **Router registration:** The new route `GET /api/v1/device-probes` goes in the admin-authed group (the `r.Group` block starting around line 152 of `router.go`), NOT inside the `/api/v1/devices/{id}` route group. It is a top-level resource.

4. **Handler needs device repo access:** The `Handlers` struct already has `devices db.DeviceRepository`. Use `h.devices.List(ctx)` in the handler.

5. **Mock `ListAll`:** Return all probes in `m.Probes` regardless of device_id (no filter).

### Frontend

6. **Query key:** Use `['all-device-probes']` for the new endpoint to avoid conflicts with the per-device `['device-probes', id]` keys.

7. **Mutations on device probes from unified page:** Each device probe row has `device_id` in the data. Use that for all mutation API calls. After mutation success, invalidate `['all-device-probes']`. Do NOT also invalidate `['device-probes', deviceId]` -- react-query will handle stale data naturally when the user navigates to a per-device page.

8. **DeviceProbeModal reuse:** The existing `DeviceProbeModal` does not need a `deviceId` prop -- it operates on form data only. The save mutation on the Probes page will use `api.updateDeviceProbe(probe.device_id, probe.id, data)`. For the unified page, `DeviceProbeModal` is used only for editing (not creating, since creation navigates to the device page).

9. **Device picker dropdown:** Use a simple `useState<boolean>` to toggle the dropdown, and `useQuery(['devices'], api.getDevices, { staleTime: 60_000 })` for the device list. Render as an absolutely-positioned div below the "Add Probe" button. Use `useRef` + click-outside handler to close it. Style consistently with existing dropdowns in the codebase.

10. **Table structure for Server Probes:** Columns: Enabled (toggle), Status (dot), Name (link to `/probes/:id`), Type, Target, Interval, Success Rate, Checks, Latency, Last Check, Actions (Run, Edit, Delete).

11. **Table structure for Device Probes:** Columns: Enabled (toggle), Status (dot), Name, Type, Device (link to `/devices/:id`), Interval, Success Rate, Checks, Latency, Actions (Run, Edit, Delete).

12. **`scrollbar-thin`:** Apply to both table wrapper `<div>` elements per project convention.

13. **`getTarget` helper:** Import from `ProbeModal` (already exported) for displaying the server probe target in the table.

---

## 13. Definition of Done

- [ ] All component changes in Section 4 implemented
- [ ] `GET /api/v1/device-probes` returns enriched device probes with hostname
- [ ] `ListAll` method added to interface, repo, and mock
- [ ] Server Probes section renders with all columns, actions, empty state
- [ ] Device Probes section renders with all columns, actions, empty state, device hostname links
- [ ] "Add Probe" in Server Probes section opens ProbeModal
- [ ] "Add Probe" in Device Probes section shows device picker, navigates to device probes page
- [ ] Both sections auto-refresh every 30 seconds
- [ ] Edit/Delete/Run/Toggle on device probes call correct device-scoped API endpoints
- [ ] DeviceProbes page (`/devices/:id/probes`) is completely unchanged
- [ ] All AC mappings in Section 8 have corresponding tests with AC references
- [ ] No new linting errors introduced
- [ ] `go test ./...` passes
- [ ] `cd web && npm run test:run` passes
- [ ] `scrollbar-thin` class applied to all scrollable containers
