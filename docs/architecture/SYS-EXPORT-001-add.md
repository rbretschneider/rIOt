# Architecture Decision Document

| Field          | Value                                          |
|----------------|------------------------------------------------|
| Story ID       | SYS-EXPORT-001                                 |
| FRD Reference  | docs/requirements/SYS-EXPORT-001-frd.md        |
| Author         | Architect Agent                                |
| Date           | 2026-04-08                                     |
| Status         | FINAL                                          |

---

## 1. Summary

Add a server-side Markdown summary generator that renders a device's latest telemetry snapshot into a portable inventory document. Expose it via `GET /api/v1/devices/{id}/summary` returning `text/markdown`. On the frontend, add "Download Summary" and "Copy to Clipboard" buttons to the device detail page header that consume this endpoint.

---

## 2. Technical Context

**Existing infrastructure that this feature leverages:**

- `TelemetryRepository.GetLatestSnapshot(ctx, deviceID)` already fetches the most recent `FullTelemetryData` snapshot for a device. No new DB queries are needed.
- `DeviceRepository.GetByID(ctx, id)` provides the `Device` struct with hostname, short ID, and hardware profile.
- The `Handlers` struct already holds references to both `devices` and `telemetry` repositories.
- Routes under `/api/v1/devices/{id}/` use `middleware.AdminAuth(s.JWTSecret)` for authentication.
- The frontend `DeviceDetail.tsx` page already queries the device and its latest telemetry via `api.getDevice(id)`.
- The API client (`web/src/api/client.ts`) uses `fetchJSON<T>()` for JSON endpoints but also supports raw `fetch()` calls for non-JSON responses (see `deleteDevice`, `sendCommand` patterns).

**What is missing:**

- A Markdown rendering/template function that converts `*models.Device` + `*models.FullTelemetryData` into a formatted Markdown string.
- A handler that serves this Markdown as `text/markdown; charset=utf-8`.
- An API client function to fetch the summary as text.
- Frontend UI controls for download and clipboard copy.

---

## 3. Architecture Decisions

### AD-001: Server-side Markdown generation using Go templates

**Decision:** Generate the Markdown document on the server using Go's `text/template` package in a dedicated `internal/server/summary` package.

**Rationale:** The FRD (A-004) explicitly requires server-side formatting. Go's `text/template` provides clean separation of template structure from data logic, conditional section inclusion (for FR-002 -- omit empty sections), and is part of the standard library. A dedicated package keeps the rendering logic isolated and independently testable without HTTP concerns.

**Alternatives Considered:**
- `fmt.Sprintf` / string concatenation: brittle, hard to maintain, poor readability for a document with 10+ sections.
- `strings.Builder` with helper functions: functional but mixes data logic with formatting; harder to test the template independently.
- External Markdown library (goldmark, etc.): unnecessary -- we are _generating_ Markdown, not parsing/rendering it.

**Consequences:** The template file is embedded as a Go string constant within the package. Changes to the Markdown format require modifying the Go source. This is acceptable because the format is tightly coupled to the data model.

---

### AD-002: Single GET endpoint returning text/markdown

**Decision:** `GET /api/v1/devices/{id}/summary` returns `Content-Type: text/markdown; charset=utf-8` with the rendered Markdown body. No format negotiation (Accept header or query param).

**Rationale:** The FRD specifies only Markdown output (other formats are explicitly out of scope in Section 9). Adding content negotiation adds complexity for a single-format endpoint. The `Content-Type: text/markdown` header lets the browser and frontend handle it correctly. A `Content-Disposition: attachment; filename=...` header is included to support direct-download use cases (though the frontend will handle download programmatically).

**Alternatives Considered:**
- JSON wrapper `{"markdown": "..."}`: adds an unnecessary encoding/decoding layer. The frontend needs the raw text for both clipboard and download.
- Query param `?format=md`: over-engineering for a single format; out-of-scope formats can be added later if needed.

**Consequences:** If future formats are needed (PDF, JSON), a query param can be added at that time. For now, the endpoint is simple and purpose-built.

---

### AD-003: Frontend download via Blob URL

**Decision:** The frontend fetches the summary text from the API, creates a `Blob` with type `text/markdown`, generates an object URL, and triggers a download via a dynamically created `<a>` element with the `download` attribute set to the filename pattern from FR-007.

**Rationale:** This is the standard browser pattern for programmatic file downloads. It avoids navigating away from the page and gives full control over the filename. The same fetched text is reusable for the clipboard copy operation.

**Alternatives Considered:**
- Direct `<a href="/api/v1/devices/{id}/summary" download>`: would work for download but does not support clipboard copy from the same fetch, and requires the browser to handle auth cookies for a navigation request (which works with same-origin cookies but is less explicit).
- Streaming the response: unnecessary for a document that will be a few KB at most.

**Consequences:** Two separate button actions (download and copy) can share the same fetched text, reducing API calls when the user does both.

---

### AD-004: Clipboard API with graceful fallback

**Decision:** Use `navigator.clipboard.writeText()` for clipboard copy. On success, show a brief "Copied!" confirmation that reverts after 2 seconds. On failure (permission denied, API unavailable), show an error toast/indicator.

**Rationale:** The Clipboard API is supported in all modern browsers (NFR-003). The 2-second revert is within the 3-second maximum specified by FR-009. No fallback to `document.execCommand('copy')` is needed -- the FRD scopes to modern browsers only.

**Alternatives Considered:**
- `document.execCommand('copy')`: deprecated, requires creating a textarea element. Unnecessary given the browser scope.
- Third-party clipboard library: adds a dependency for a single API call.

**Consequences:** The feature will not work in very old browsers, which is acceptable per NFR-003.

---

### AD-005: Export buttons placement in device detail header

**Decision:** Add "Download Summary" and "Copy to Clipboard" buttons in the right-side action area of the device detail header, alongside existing action buttons (Update Agent, Patch, Reboot, Shutdown). The buttons are always visible but disabled when `latest_telemetry` is null (FR-012). They use the same styling as existing header action buttons (small, bordered, gray/cyan tones).

**Rationale:** Placing export controls with existing device actions keeps the UI consistent and discoverable. No new UI components or modals are needed. Disabling (rather than hiding) when no telemetry exists provides clear affordance per FR-012.

**Alternatives Considered:**
- Separate toolbar or dropdown menu: over-engineering for two buttons.
- Hide buttons when no telemetry: less discoverable; user would not know the feature exists for new devices.

**Consequences:** The header action area may become crowded on small screens. The existing responsive layout (`flex-wrap`, `gap-2`) handles this adequately.

---

### AD-006: Summary data struct for template rendering

**Decision:** Create a `SummaryData` struct in the `internal/server/summary` package that flattens/combines fields from `models.Device` and `models.FullTelemetryData` into a template-friendly shape. The `Render` function accepts `*models.Device` and `*models.TelemetrySnapshot` and returns the Markdown string.

**Rationale:** The template needs data from two sources (device metadata + telemetry). A purpose-built struct keeps the template logic clean and avoids passing raw model types that contain excluded data (FR-003). The struct acts as a filter, ensuring only inventory-appropriate data reaches the template.

**Alternatives Considered:**
- Pass raw models directly to template: would expose excluded fields (logs, processes, security data) to the template layer, requiring the template to know what to skip. Violates separation of concerns.
- Map/interface{} data: loses type safety and makes testing harder.

**Consequences:** Adding new sections to the summary requires updating both the `SummaryData` struct and the template.

---

## 4. Component Changes

| Action | File Path | Purpose |
|--------|-----------|---------|
| CREATE | `internal/server/summary/summary.go` | `SummaryData` struct, `Render(*models.Device, *models.TelemetrySnapshot) (string, error)` function, Markdown template |
| CREATE | `internal/server/summary/summary_test.go` | Unit tests for Markdown rendering (all ACs related to content) |
| MODIFY | `internal/server/handlers/handlers.go` | Add `DeviceSummary(w, r)` handler method |
| CREATE | `internal/server/handlers/summary_handler_test.go` | Unit tests for the HTTP handler (status codes, content type, error cases) |
| MODIFY | `internal/server/router.go` | Register `GET /api/v1/devices/{id}/summary` under admin auth |
| MODIFY | `web/src/api/client.ts` | Add `getDeviceSummary(id: string): Promise<string>` method |
| MODIFY | `web/src/pages/DeviceDetail.tsx` | Add Download Summary and Copy to Clipboard buttons |
| MODIFY | `web/src/pages/DeviceDetail.test.tsx` | Add tests for button rendering, disabled state, click behavior |

---

## 5. Data Model Changes

**None.** This feature is entirely read-only and uses existing data access patterns. No migrations required.

---

## 6. API / Interface Contract

### `GET /api/v1/devices/{id}/summary`

**Authentication:** Admin JWT cookie (same as all `/api/v1/devices/{id}/*` admin endpoints)

**Path Parameters:**
| Parameter | Type   | Description |
|-----------|--------|-------------|
| `id`      | string | Device UUID |

**Request Headers:**
| Header   | Value                | Required |
|----------|----------------------|----------|
| `Cookie` | `riot_token=<JWT>`   | Yes      |

**Success Response: `200 OK`**
```
Content-Type: text/markdown; charset=utf-8
Content-Disposition: attachment; filename="{hostname}-summary-{YYYY-MM-DD}.md"

# Device Summary: {hostname}

**Device ID:** {short_id}
**Generated:** {timestamp with timezone}

...rendered Markdown content...
```

**Error Responses:**

| Status | Condition | Body |
|--------|-----------|------|
| `401 Unauthorized` | Missing or invalid JWT | (handled by auth middleware) |
| `404 Not Found` | Device ID does not exist | `{"error":"device not found"}` |
| `404 Not Found` | Device exists but has no telemetry | `{"error":"no telemetry data available"}` |
| `500 Internal Server Error` | Template rendering failure | `{"error":"failed to generate summary"}` |

**Design note on 404 for missing telemetry:** Using 404 (rather than 204 or 422) because the resource being requested is "the summary" -- if no telemetry exists, the summary does not exist. This is consistent with how `GetLatestSnapshot` returns an error for missing data. The two 404 cases are distinguished by the error message string, which the frontend uses to show the appropriate user message.

---

## 7. Sequence / Flow

### Download Summary Flow

1. User clicks "Download Summary" button on `DeviceDetail` page.
2. Frontend calls `api.getDeviceSummary(deviceId)` which performs `GET /api/v1/devices/{id}/summary` with credentials.
3. Handler extracts `id` from URL via `chi.URLParam(r, "id")`.
4. Handler calls `h.devices.GetByID(ctx, id)`.
5. If device not found, return 404 with `{"error":"device not found"}`.
6. Handler calls `h.telemetry.GetLatestSnapshot(ctx, id)`.
7. If no snapshot, return 404 with `{"error":"no telemetry data available"}`.
8. Handler calls `summary.Render(device, snapshot)`.
9. If render error, log with `slog.Error` and return 500.
10. Handler sets `Content-Type: text/markdown; charset=utf-8` and `Content-Disposition: attachment; filename={hostname}-summary-{YYYY-MM-DD}.md`.
11. Handler writes Markdown string to response body with 200 status.
12. Frontend receives text, creates `Blob("text/markdown")`, generates object URL, creates hidden `<a download="{filename}">`, triggers click, revokes URL.

### Copy to Clipboard Flow

1. User clicks "Copy to Clipboard" button.
2. Frontend calls same `api.getDeviceSummary(deviceId)`.
3. On success, calls `navigator.clipboard.writeText(text)`.
4. On clipboard success, button text changes to "Copied!" with a green color shift, reverts after 2 seconds.
5. On clipboard failure, button text changes to "Copy Failed" with a red color shift, reverts after 2 seconds.
6. On API fetch failure, show error state on the button.

---

## 8. Acceptance Criteria Mapping

| AC ID | Fulfilled By | Test Strategy |
|-------|-------------|---------------|
| AC-001 | `summary.Render()` for content + header; `DeviceSummary` handler for Content-Disposition filename; `DeviceDetail.tsx` download logic | Unit: `summary_test.go` verifies header fields; `summary_handler_test.go` verifies filename header; Frontend: `DeviceDetail.test.tsx` verifies download button triggers fetch |
| AC-002 | `DeviceDetail.tsx` clipboard logic with `navigator.clipboard.writeText` | Frontend: `DeviceDetail.test.tsx` mocks clipboard API, verifies confirmation text appears |
| AC-003 | `summary.Render()` section generation for all data categories | Unit: `summary_test.go` with full telemetry fixture, verify all section headings present |
| AC-004 | `summary.Render()` conditional section logic (nil checks) | Unit: `summary_test.go` with partial telemetry (no Docker, no UPS), verify sections absent |
| AC-005 | `SummaryData` struct excludes logs, processes, security, etc.; template only renders included fields | Unit: `summary_test.go` with full telemetry including excluded data, verify none appears in output |
| AC-006 | `DeviceDetail.tsx` disabled state when `latest_telemetry` is null | Frontend: `DeviceDetail.test.tsx` with null telemetry, verify buttons are disabled |
| AC-007 | `DeviceDetail.tsx` clipboard error handling | Frontend: `DeviceDetail.test.tsx` mock clipboard rejection, verify error indication |
| AC-008 | `DeviceSummary` handler 404 for missing device | Unit: `summary_handler_test.go` with mock returning not-found error |
| AC-009 | `DeviceSummary` handler 404 for missing telemetry | Unit: `summary_handler_test.go` with device present but no snapshots |
| AC-010 | `TelemetryRepository.GetLatestSnapshot` returns most recent; `summary.Render` uses that snapshot's data | Unit: `summary_test.go` documents this; `summary_handler_test.go` verifies GetLatestSnapshot is called |

---

## 9. Error Handling

| Failure Mode | HTTP Status | Error Message | Logging |
|-------------|-------------|---------------|---------|
| Device ID not found | 404 | `{"error":"device not found"}` | None (normal flow) |
| Device exists, no telemetry | 404 | `{"error":"no telemetry data available"}` | None (normal flow) |
| Template rendering fails | 500 | `{"error":"failed to generate summary"}` | `slog.Error("render device summary", "device_id", id, "error", err.Error())` |
| Database error on GetByID | 404 | `{"error":"device not found"}` | None (matches existing GetDevice pattern which does not distinguish DB errors from not-found) |
| Database error on GetLatestSnapshot | 404 | `{"error":"no telemetry data available"}` | None (matches existing pattern) |
| Frontend fetch fails (network) | N/A | Button shows error state | Console error via standard fetch rejection |
| Clipboard write denied | N/A | Button shows "Copy Failed" for 2s | None |

---

## 10. Security Considerations

**Authentication:** The endpoint sits behind `middleware.AdminAuth(s.JWTSecret)`, identical to all other device admin endpoints. No additional auth is needed.

**Data exposure:** The summary intentionally includes serial numbers and device UUIDs (BR-002 classifies these as inventory data). The summary explicitly excludes:
- API keys (never present in telemetry data)
- Environment variables from containers (present in `ContainerInfo.Env` but not rendered)
- Passwords or tokens of any kind
- Container labels that might contain secrets (not rendered)

**Injection:** The Markdown is generated server-side from structured data, not user input. Template escaping is not a concern because Markdown is plain text. However, hostname and other string fields from agent telemetry could theoretically contain malicious content. Since the output is plain text Markdown (not rendered HTML by the server), this is not an XSS vector. The frontend downloads or copies the text without rendering it as HTML.

**Rate limiting:** No additional rate limiting beyond the existing middleware. The endpoint performs two DB reads (device + snapshot) and CPU-only rendering, making it lightweight.

---

## 11. Performance Considerations

**Response time:** The endpoint performs exactly two database queries (`GetByID` + `GetLatestSnapshot`), both indexed lookups by primary key/device ID. Template rendering is CPU-only on a small data structure. NFR-001's 2-second requirement will be met easily -- expected response time is under 50ms.

**Caching:** Not needed. The summary is generated on-demand from the latest snapshot, which changes on every telemetry push (typically every 60 seconds). Caching would add complexity for minimal benefit.

**Payload size:** A fully populated summary (all sections present, many containers/disks/interfaces) is estimated at 5-15 KB of Markdown text. No compression or streaming is warranted.

**No new indexes required.** Both queries use existing indexed paths.

---

## 12. Implementation Notes for Engineers

### Go Backend

1. **Package location:** Create `internal/server/summary/` with a single file `summary.go`. The package exports one function: `Render(device *models.Device, snapshot *models.TelemetrySnapshot) (string, error)`.

2. **Template structure:** Define the Markdown template as a `const` string in `summary.go`. Use `text/template` (not `html/template` -- we are generating plain text, and HTML escaping would corrupt Markdown). Parse the template once in an `init()` function or package-level `var` using `template.Must()`.

3. **SummaryData struct:** Build an internal (unexported) struct that the template receives. Populate it from the device and snapshot. Include boolean flags like `HasDocker`, `HasUPS`, `HasGPUs`, `HasUSB` that the template uses for conditional section rendering.

4. **Formatting helpers:** Register template functions for:
   - `formatBytes(mb int64) string` -- converts MB to human-readable (e.g., "16,384 MB (16 GB)")
   - `formatFreq(mhz *float64) string` -- converts MHz to GHz if >= 1000
   - `formatSpeed(mbps *int) string` -- formats link speed
   - `formatUptime(seconds uint64) string` -- converts to "Xd Yh Zm"

5. **GPU data source:** Per FRD assumption A-005, use `Hardware.GPUs` (static `[]GPUInfo` from the hardware collector) for GPU inventory data, NOT `GPUTelemetry` (runtime metrics). The summary is an inventory document, not a metrics report.

6. **Handler pattern:** Follow the exact pattern of `GetDevice` -- extract ID from chi URL param, call repos, handle errors with `http.Error` and JSON error bodies. The only difference is the response content type.

7. **Filename generation:** In the handler, compute the filename as `fmt.Sprintf("%s-summary-%s.md", device.Hostname, time.Now().UTC().Format("2006-01-02"))`. Sanitize the hostname to remove characters invalid in filenames (replace non-alphanumeric except hyphens/underscores with underscores).

8. **BlockDevice serial field:** Note that `DiskInfo.BlockDevices` (from the `disk` collector) does NOT have a serial field. `HardwareInfo.DiskDrives` (from the `hardware` collector) has `Serial`. The FRD requests serial numbers for drives. Use `HardwareInfo.DiskDrives` for the Storage/Physical Drives section, and `DiskInfo.Filesystems` for the Filesystems section.

### Frontend

9. **API client method:** Add `getDeviceSummary` to the `api` object. This must use raw `fetch()` (not `fetchJSON`) since the response is `text/markdown`, not JSON. Pattern:
   ```
   getDeviceSummary: async (id: string): Promise<string> => {
     const res = await fetch(`${BASE}/devices/${id}/summary`, { credentials: 'same-origin' })
     if (res.status === 401) { window.location.reload(); throw new Error('Unauthorized') }
     if (!res.ok) {
       const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
       throw new Error(err.error || `HTTP ${res.status}`)
     }
     return res.text()
   }
   ```

10. **Button placement:** Add buttons inside the `<div className="flex gap-2">` block in the device header's right section (around line 208 of `DeviceDetail.tsx`). However, these buttons should be visible even when the device is offline (the summary uses stored data, not live data). Therefore, place them OUTSIDE the `{device.status === 'online' && (...)}` conditional, but still within the right-side flex container. Place them before the StatusBadge for visual grouping with other action controls.

11. **Button styling:** Use the same pattern as existing header buttons: `px-3 py-1.5 text-xs rounded-md border transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed`. Use `text-gray-400 hover:text-gray-300 border-gray-600/50 hover:border-gray-500/50` for a neutral style that does not compete with the warning/danger colors of Update/Reboot/Shutdown.

12. **State management:** Use local `useState` for button loading/feedback states. No need for React Query mutations since this is a one-shot fetch-and-act pattern. Track three states per button: idle, loading, success/error.

13. **Accessibility (NFR-004):** Add `aria-label="Download device summary as Markdown file"` and `aria-label="Copy device summary to clipboard"` to the buttons. Both must be focusable and activatable via keyboard (native `<button>` elements satisfy this).

14. **Download filename:** The frontend should extract the filename from the `Content-Disposition` header if present, falling back to constructing it client-side from `device.hostname` and the current date.

---

## 13. Markdown Template Specification

The rendered Markdown must follow this structure (sections present only when data exists):

```
# Device Summary: {hostname}

**Device ID:** {short_id}
**Generated:** {YYYY-MM-DD HH:MM:SS TZ}

---

## System Identity

| Field | Value |
|-------|-------|
| Hostname | ... |
| Device UUID | ... |
| Board Model | ... |
| Serial Number | ... |
| BIOS Version | ... |
| BIOS Date | ... |
| Architecture | ... |
| Virtualization | ... |

## Operating System

| Field | Value |
|-------|-------|
| OS | {name} {version} |
| Codename | ... |
| Kernel | {kernel} ({kernel_arch}) |
| Init System | ... |
| Timezone | ... |
| Locale | ... |
| Uptime | ... |

## CPU

| Field | Value |
|-------|-------|
| Model | ... |
| Cores | ... |
| Threads | ... |
| Max Frequency | ... |

## Memory

| Field | Value |
|-------|-------|
| Total RAM | ... |
| Total Swap | ... |

## Storage

### Physical Drives ({count})

| Name | Model | Serial | Type | Size | SMART Health |
|------|-------|--------|------|------|-------------|
| ... | ... | ... | ... | ... | ... |

### Filesystems ({count})

| Mount Point | Device | Type | Total | Used | Free |
|-------------|--------|------|-------|------|------|
| ... | ... | ... | ... | ... | ... |

## GPUs

| Vendor | Model | VRAM | PCI Slot | Driver |
|--------|-------|------|----------|--------|
| ... | ... | ... | ... | ... |

## Network Interfaces

| Name | MAC | IPv4 | IPv6 | State | Speed |
|------|-----|------|------|-------|-------|
| ... | ... | ... | ... | ... | ... |

**Default Gateway:** ...
**DNS Servers:** ...
**FQDN:** ...

## USB Devices

| Vendor | Product | Description | VID:PID | Serial | Class | Speed |
|--------|---------|-------------|---------|--------|-------|-------|
| ... | ... | ... | ... | ... | ... | ... |

## Docker

| Field | Value |
|-------|-------|
| Version | ... |
| Total Containers | ... |
| Running | ... |
| Stopped | ... |
| Paused | ... |
| Storage Driver | ... |

### Containers

| Name | Image | State | Status |
|------|-------|-------|--------|
| ... | ... | ... | ... |

## UPS

| Field | Value |
|-------|-------|
| Name | ... |
| Model | ... |
| Manufacturer | ... |
| Status | ... |
| Battery Charge | ... |
| Load | ... |
```

---

## 14. Definition of Done

- [ ] All component changes in Section 4 implemented
- [ ] All AC mappings in Section 8 have corresponding tests with AC reference in test name
- [ ] `go test ./internal/server/summary/...` passes
- [ ] `go test ./internal/server/handlers/...` passes
- [ ] `cd web && npm run test:run` passes
- [ ] `go vet ./...` produces no new warnings
- [ ] No new linting errors introduced
- [ ] No `console.log` debug statements left in frontend code
- [ ] No `fmt.Println` debug statements left in Go code
- [ ] Endpoint returns valid Markdown for a fully populated device
- [ ] Endpoint returns valid Markdown for a minimally populated device (system + OS only)
- [ ] Endpoint omits sections cleanly when data is nil
- [ ] Buttons are keyboard-accessible with ARIA labels
- [ ] Buttons are disabled when `latest_telemetry` is null
