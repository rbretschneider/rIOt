# Formal Requirements Document

- **Story ID:** PROBE-SPLIT
- **Title:** Unified Probes Page with Server and Device Probe Sections
- **Author:** Business Developer Agent
- **Date:** 2026-03-20
- **Status:** DRAFT

---

## 1. Executive Summary

The Probes page must be restructured to display both server-level probes and device-level probes on a single unified page, organized into two visually distinct sections. Server probes must appear at the top of the page; device probes must appear below. This mirrors the established pattern already used on the Alert Rules page, which separates "Global Alert Rules" from "Device-Specific Alert Rules."

---

## 2. Background & Context

Currently, rIOt has two separate probe systems:

- **Server probes** are configured at the server level, run by the server itself, and are not tied to any specific device. They are managed on the `/probes` page. These support HTTP, Ping (ICMP), and DNS probe types.
- **Device probes** are configured per-device, run by the agent on that device, and are managed on a per-device page at `/devices/:id/probes`. These support Shell Command, HTTP, Port Check, File Check, and Container Exec probe types.

The user currently has no single view that shows all probes across the system. Device probes are hidden behind individual device pages, making it difficult to get a holistic view of all health checks. The Alert Rules page (`/alert-rules`) already implements a two-section pattern (Global Alert Rules and Device-Specific Alert Rules) that users are familiar with. The Probes page must adopt this same pattern.

---

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Authenticated User | A user logged into the rIOt dashboard | Full CRUD on all probes (server and device). Can view, create, edit, delete, enable/disable, and manually run any probe. |

---

## 4. Functional Requirements

### Server Probes Section

**FR-001:** The Probes page (`/probes`) must display a section titled "Server Probes" at the top of the page.

**FR-002:** The Server Probes section must list all server-level probes retrieved from the existing `GET /api/v1/probes` endpoint.

**FR-003:** The Server Probes section must include an "Add Probe" button that opens the existing server probe creation modal (ProbeModal).

**FR-004:** Each server probe in the list must display the same information currently shown on the Probes page: probe name (linking to `/probes/:id`), type, target, interval, enabled/disabled toggle, status indicator (up/down/unknown), success rate, total checks, latest latency, and latest check time.

**FR-005:** Each server probe must have Run, Edit, and Delete action buttons that perform the same operations as the current Probes page.

**FR-006:** When no server probes exist, the Server Probes section must display an empty state message indicating that no server probes are configured.

### Device Probes Section

**FR-007:** The Probes page must display a section titled "Device Probes" below the Server Probes section.

**FR-008:** The Device Probes section must list all device-level probes across all devices.

**FR-009:** Each device probe in the list must display: probe name, probe type, the device hostname it belongs to, enabled/disabled state, interval, status indicator (up/down/unknown based on latest result), success rate, total checks, and latest latency.

**FR-010:** The device hostname displayed for each device probe must be a clickable link that navigates to that device's detail page (`/devices/:id`).

**FR-011:** The Device Probes section must include an "Add Probe" button. When clicked, this button must allow the user to select a target device before creating the probe, or navigate to the device-specific probe creation flow.

**FR-012:** Each device probe must have action buttons for Run, Edit, and Delete that call the appropriate device probe API endpoints (`/api/v1/devices/:deviceId/device-probes/:probeId/...`).

**FR-013:** Each device probe must have an Enable/Disable toggle or button that calls the device probe update API endpoint.

**FR-014:** When no device probes exist across any device, the Device Probes section must display an empty state message indicating that no device probes are configured.

### Layout and Visual Consistency

**FR-015:** The page title must remain "Probes" at the top of the page.

**FR-016:** The visual separation between the Server Probes and Device Probes sections must follow the same pattern used on the Alert Rules page: each section must have its own heading and its own "Add Probe" button, with vertical spacing between the two sections.

**FR-017:** The Server Probes section must appear above the Device Probes section with no option to reorder them.

### Data Retrieval

**FR-018:** The page must fetch server probes using the existing `GET /api/v1/probes` endpoint.

**FR-019:** The page must fetch device probes for display in the Device Probes section. If no existing endpoint returns all device probes across all devices, a new endpoint or aggregation approach must be used to retrieve them.

**FR-020:** Both sections must auto-refresh their data on a periodic interval consistent with the current behavior (every 30 seconds).

---

## 5. Non-Functional Requirements

**NFR-001:** [Performance] The Probes page must render within the same performance envelope as the current page. Adding the device probes section must not introduce perceptible delay for fleets of up to 50 devices with up to 20 device probes each (1,000 total device probes).

**NFR-002:** [Consistency] All styling must use the existing Tailwind CSS classes and dark theme (gray-900 backgrounds, gray-800 borders, etc.) consistent with the rest of the dashboard.

**NFR-003:** [Responsiveness] Both sections must be fully functional on mobile viewports, following the existing responsive patterns (flex column on small screens, flex row on larger screens).

**NFR-004:** [Scrollbar] Any scrollable containers must use the `scrollbar-thin` class per project convention.

---

## 6. Business Rules

**BR-001:** Server probes and device probes are fundamentally different entities with different data models, different API endpoints, and different probe type options. They must not be merged into a single data model.

**BR-002:** A server probe has no `device_id` association. A device probe always has a `device_id` association. This distinction determines which section a probe appears in.

**BR-003:** Device probes are executed by the agent on the target device. Server probes are executed by the server. This execution model must not change.

**BR-004:** The existing per-device probe page (`/devices/:id/probes`) must continue to function as-is. The new unified view on the Probes page is an additional aggregated view, not a replacement.

---

## 7. Data Requirements

### Entities Involved

| Entity | Source Endpoint | Key Fields |
|--------|----------------|------------|
| Server Probe (ProbeWithResult) | `GET /api/v1/probes` | id, name, type, enabled, config, interval_seconds, timeout_seconds, latest_result, success_rate, total_checks |
| Device Probe (DeviceProbeWithResult) | `GET /api/v1/devices/:id/device-probes` (per device) or new aggregated endpoint | id, name, device_id, type, enabled, config, assertions, interval_seconds, timeout_seconds, latest_result, success_rate, total_checks |
| Device | `GET /api/v1/devices` | id, hostname (needed to display device names for device probes) |

### Validation Rules

- No new validation rules are introduced. Existing validation for probe creation/editing must remain unchanged.

### State Transitions

No new state transitions. Probe states (enabled/disabled, up/down/unknown) are unchanged.

---

## 8. Acceptance Criteria

**AC-001:** [Maps to FR-001, FR-002, FR-004, FR-005, FR-006]
- Given: The user navigates to the `/probes` page and server probes exist
- When: The page finishes loading
- Then: A section titled "Server Probes" must be visible at the top of the page
- And: All server probes must be listed with their name, type, target, interval, status indicator, success rate, total checks, latest latency, and latest check time
- And: Each probe must have Run, Edit, and Delete action buttons
- And: Each probe name must link to `/probes/:id`

**AC-002:** [Maps to FR-003]
- Given: The user is on the `/probes` page
- When: The user clicks the "Add Probe" button in the Server Probes section
- Then: The server probe creation modal must open with default values

**AC-003:** [Maps to FR-006]
- Given: No server probes are configured
- When: The user navigates to the `/probes` page
- Then: The Server Probes section must display an empty state message

**AC-004:** [Maps to FR-007, FR-008, FR-009, FR-010]
- Given: The user navigates to the `/probes` page and device probes exist on one or more devices
- When: The page finishes loading
- Then: A section titled "Device Probes" must be visible below the Server Probes section
- And: All device probes across all devices must be listed
- And: Each device probe must display its name, type, the associated device hostname, enabled state, interval, status indicator, success rate, total checks, and latest latency
- And: Each device hostname must be a clickable link to `/devices/:id`

**AC-005:** [Maps to FR-012, FR-013]
- Given: Device probes are listed in the Device Probes section
- When: The user clicks Run, Edit, Delete, or Enable/Disable on a device probe
- Then: The corresponding API call must be made to the correct device-specific endpoint
- And: The probe list must refresh to reflect the change

**AC-006:** [Maps to FR-014]
- Given: No device probes are configured on any device
- When: The user navigates to the `/probes` page
- Then: The Device Probes section must display an empty state message

**AC-007:** [Maps to FR-016, FR-017]
- Given: Both server probes and device probes exist
- When: The user views the `/probes` page
- Then: The Server Probes section must appear above the Device Probes section
- And: Each section must have its own heading and its own "Add Probe" button
- And: The visual pattern must match the Alert Rules page layout (section heading with button on the right, content below, vertical spacing between sections)

**AC-008:** [Maps to FR-020]
- Given: The user is on the `/probes` page
- When: 30 seconds elapse without user interaction
- Then: Both the server probes and device probes data must be re-fetched and the display updated

**AC-009:** [Maps to BR-004]
- Given: Device probes are shown on the unified `/probes` page
- When: The user navigates to `/devices/:id/probes` for any device
- Then: The per-device probes page must continue to function exactly as before, showing only that device's probes with full CRUD operations

---

## 9. Out of Scope

- Changing the server probe or device probe data models
- Changing any existing API endpoints or their response shapes
- Merging server probes and device probes into a single probe type
- Adding filtering, sorting, or search to the Probes page (may be a future story)
- Adding pagination to the Probes page (may be a future story if probe counts grow large)
- Modifying the ProbeDetail page (`/probes/:id`)
- Modifying the per-device probes page (`/devices/:id/probes`)
- Adding a device probe detail view analogous to `/probes/:id` for server probes

---

## 10. Assumptions

- **A-001:** The Alert Rules page pattern (two sections with separate headings and tables) is the correct visual pattern to follow. The user's reference to "similar to how alerts show GLOBAL and DEVICE specific" refers to the `AlertRuleSettings` component.
- **A-002:** An API endpoint to retrieve all device probes across all devices may not currently exist. If it does not, one must be created, or the frontend must aggregate data by fetching the device list and then each device's probes. The architecture decision on which approach to use is deferred to the architect.
- **A-003:** The existing ProbeModal and DeviceProbeModal components can be reused on the unified page without modification (or with minimal adaptation to support device selection for new device probes).
- **A-004:** The "Add Probe" button in the Device Probes section requires a way to select which device the probe will be created for. The exact UX for this (a device picker in the modal, a dropdown, or navigation to the device's probe page) is deferred to the architect.

---

## 11. Open Questions

- **OQ-001:** Does a backend endpoint currently exist that returns all device probes across all devices in a single call? If not, should one be created, or should the frontend aggregate by calling each device's probe endpoint?
- **OQ-002:** For the "Add Probe" button in the Device Probes section, what is the preferred UX for selecting the target device? Options include: (a) adding a device selector dropdown to the DeviceProbeModal, (b) showing a device picker first, then the probe modal, or (c) linking to the existing per-device probes page.
- **OQ-003:** Should device probes on this page support inline result history expansion (as they do on the per-device page), or should they link to a detail view instead?

---

## 12. Dependencies

- **DEP-001:** Existing `GET /api/v1/probes` endpoint (server probes)
- **DEP-002:** Existing `GET /api/v1/devices` endpoint (to resolve device hostnames for device probes)
- **DEP-003:** Existing `GET /api/v1/devices/:id/device-probes` endpoint (device probes per device)
- **DEP-004:** Existing ProbeModal component (server probe create/edit)
- **DEP-005:** Existing DeviceProbeModal component (device probe create/edit)
- **DEP-006:** Existing AlertRuleSettings component (reference pattern for two-section layout)
