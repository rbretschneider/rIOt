# Formal Requirements Document

| Field   | Value                                      |
|---------|--------------------------------------------|
| Story ID | SYS-EXPORT-001                            |
| Title   | Device System Summary Export               |
| Author  | Business Developer Agent                   |
| Date    | 2026-04-08                                 |
| Status  | FINAL                                      |

---

## 1. Executive Summary

Users need the ability to export a high-level inventory-style summary of a monitored device's hardware and software configuration. The export must be available in two formats: download as a Markdown (.md) file, and copy to the system clipboard. This feature surfaces static/identity information already collected by the agent and does not include time-series telemetry, logs, or granular process-level data.

---

## 2. Background & Context

The rIOt dashboard already displays detailed device pages with telemetry data collected by the agent. However, there is no way to extract a concise, portable summary of a device's hardware and software inventory. Users need this for documentation, support requests, inventory tracking, and quick reference when working outside the dashboard. The data required is already collected by existing agent collectors (system, os_info, cpu, memory, disk, hardware, usb, network, docker, ups) and stored in the server database as part of the telemetry snapshot.

---

## 3. Actors

| Actor              | Description                                                         | Permissions                          |
|--------------------|---------------------------------------------------------------------|--------------------------------------|
| Dashboard User     | Any authenticated user viewing a device's detail page in the dashboard | Read access to device telemetry data |

---

## 4. Functional Requirements

### Data Content

**FR-001:** The system must produce a device summary document containing the following sections, where data is available for the device:

- **System Identity**: hostname, device UUID, board model, serial number, BIOS version, BIOS date, architecture, virtualization type
- **Operating System**: OS name, version, codename, kernel version, kernel architecture, init system, timezone, locale, uptime
- **CPU**: model name, core count, thread count, max frequency (if available)
- **Memory**: total RAM, total swap
- **Storage**: count of physical drives, and for each drive: name, model, serial, type (HDD/SSD/NVMe/SD), size, SMART health status; count of filesystems, and for each filesystem: mount point, device, filesystem type, total size, used/free
- **GPUs**: for each GPU: vendor, model, VRAM, PCI slot, driver
- **Network Interfaces**: for each interface: name, MAC address, IPv4 addresses, IPv6 addresses, state, link speed; plus default gateway, DNS servers, FQDN
- **USB Devices**: for each device: vendor, product, description, vendor ID, product ID, serial, device class, speed
- **Docker** (if available): Docker version, total containers, running/stopped/paused counts, storage driver; container list with name, image, state, status
- **UPS** (if available): name, model, manufacturer, status, battery charge, load

**FR-002:** The system must omit any top-level section entirely when the device has no data for that category (e.g., if no UPS data exists, the UPS section must not appear in the output).

**FR-003:** The system must not include any of the following in the summary: journal/system logs, container logs, per-core CPU usage percentages, real-time CPU/memory usage percentages, process lists, load averages, network byte counters, security audit data, web server/proxy configuration details, cron jobs/timers, PCI device inventory, serial ports, or time-series metrics of any kind.

### Format

**FR-004:** The summary document must be formatted as valid Markdown with clear section headings, using tables or structured lists for multi-item data (drives, interfaces, USB devices, containers).

**FR-005:** The Markdown output must include a header containing the device hostname, the device short ID, and the timestamp (date and time with timezone) at which the summary was generated.

### Export: Download as File

**FR-006:** The system must provide a control on the device detail page that initiates a file download of the summary as a `.md` file.

**FR-007:** The downloaded file must be named using the pattern `{hostname}-summary-{YYYY-MM-DD}.md` where `{hostname}` is the device's hostname and `{YYYY-MM-DD}` is the date the export was generated.

### Export: Copy to Clipboard

**FR-008:** The system must provide a control on the device detail page that copies the full summary Markdown text to the user's system clipboard.

**FR-009:** Upon successful clipboard copy, the system must display a brief visual confirmation to the user (e.g., the button text or icon changes to indicate success) that automatically reverts after no more than 3 seconds.

**FR-010:** If the clipboard copy operation fails (e.g., browser permission denied), the system must display an error indication to the user.

### Data Source

**FR-011:** The summary must be generated from the device's most recent telemetry snapshot stored on the server.

**FR-012:** If no telemetry snapshot exists for the device, the export controls must either be disabled or, when activated, display a message stating that no telemetry data is available.

### Server Endpoint

**FR-013:** The server must expose an API endpoint that accepts a device ID and returns the formatted Markdown summary as a response.

**FR-014:** The endpoint must return an appropriate error response when the requested device does not exist.

**FR-015:** The endpoint must return an appropriate error response when the requested device has no telemetry data available.

---

## 5. Non-Functional Requirements

**NFR-001:** [Performance] The summary generation endpoint must respond within 2 seconds under normal operating conditions (single device, most recent snapshot lookup).

**NFR-002:** [Security] The endpoint must require the same authentication/authorization as other device telemetry endpoints.

**NFR-003:** [Compatibility] The clipboard copy functionality must work in all modern browsers that support the Clipboard API (Chrome, Firefox, Edge, Safari).

**NFR-004:** [Accessibility] Both export controls (download and clipboard) must be keyboard-accessible and have appropriate ARIA labels.

---

## 6. Business Rules

**BR-001:** The summary represents a point-in-time snapshot. It must reflect the most recently received telemetry data, not live data.

**BR-002:** Sensitive data handling: serial numbers and device UUIDs are included in the export as they are considered inventory data. No API keys, passwords, or authentication tokens must ever be included.

**BR-003:** The file download and clipboard copy actions are independent. The user may use either or both, in any order.

---

## 7. Data Requirements

### Entities Involved

- **Device** (read-only): provides hostname, short ID, status metadata
- **TelemetrySnapshot** (read-only): provides the latest `FullTelemetryData` payload containing System, OS, CPU, Memory, Disks, Network, Hardware, USB, Docker, UPS, and GPU telemetry sections
- **HardwareProfile** (read-only): provides board model, serial number, BIOS info from the device record

### Validation Rules

- Device ID must correspond to an existing registered device
- At least one telemetry snapshot must exist for the device for the export to succeed

### State Transitions

No state changes occur. This feature is entirely read-only.

---

## 8. Acceptance Criteria

**AC-001:** [Maps to FR-006, FR-007, FR-004, FR-005]
```
Given: The user is on a device detail page for a device with telemetry data
When:  The user activates the "Download Summary" control
Then:  A file download is initiated
And:   The file is named "{hostname}-summary-{YYYY-MM-DD}.md"
And:   The file contains valid Markdown with a header showing hostname, short ID, and generation timestamp
```

**AC-002:** [Maps to FR-008, FR-009]
```
Given: The user is on a device detail page for a device with telemetry data
When:  The user activates the "Copy to Clipboard" control
Then:  The full Markdown summary text is copied to the system clipboard
And:   A visual confirmation is displayed for no more than 3 seconds
```

**AC-003:** [Maps to FR-001, FR-002]
```
Given: A device has telemetry data that includes system, OS, CPU, memory, disk, GPU, network, USB, Docker, and UPS information
When:  The summary is generated
Then:  All listed sections are present in the output with the correct data
```

**AC-004:** [Maps to FR-002]
```
Given: A device has telemetry data that does NOT include Docker or UPS information
When:  The summary is generated
Then:  The Docker and UPS sections are omitted entirely from the output
```

**AC-005:** [Maps to FR-003]
```
Given: A device has telemetry data including logs, process lists, and security data
When:  The summary is generated
Then:  No log entries, process lists, per-core CPU usage, real-time usage percentages, network byte counters, security data, web server configs, cron jobs, PCI devices, or serial ports appear in the output
```

**AC-006:** [Maps to FR-012]
```
Given: The user is on a device detail page for a device with NO telemetry data
When:  The user views the export controls
Then:  The controls are either disabled or, when activated, display a message stating no telemetry data is available
```

**AC-007:** [Maps to FR-010]
```
Given: The user is on a device detail page and the browser denies clipboard access
When:  The user activates the "Copy to Clipboard" control
Then:  An error indication is displayed to the user
```

**AC-008:** [Maps to FR-013, FR-014, FR-015]
```
Given: An API request is made for a device summary
When:  The device ID does not exist
Then:  The endpoint returns an error response indicating the device was not found
```

**AC-009:** [Maps to FR-013, FR-014, FR-015]
```
Given: An API request is made for a device summary
When:  The device exists but has no telemetry data
Then:  The endpoint returns an error response indicating no telemetry data is available
```

**AC-010:** [Maps to FR-011, BR-001]
```
Given: A device has multiple telemetry snapshots over time
When:  The summary is generated
Then:  The output reflects data from the most recent telemetry snapshot only
```

---

## 9. Out of Scope

- Exporting summaries for multiple devices at once (batch export)
- Exporting to formats other than Markdown (PDF, JSON, CSV, HTML)
- Customizing which sections appear in the export
- Scheduling or automating exports
- Exporting time-series or historical data
- Exporting log data, process lists, or security audit information
- Sharing exports directly (email, link generation)
- Agent-side export (this is a server/dashboard feature only)

---

## 10. Assumptions

- **A-001:** The dashboard already has a device detail page where export controls can be added.
- **A-002:** The server already stores and serves the most recent telemetry snapshot per device, and this data is accessible without a new database query pattern.
- **A-003:** The existing authentication mechanism on telemetry endpoints is sufficient for the new export endpoint.
- **A-004:** The Markdown formatting/rendering is done server-side (the server returns pre-formatted Markdown text) rather than having the frontend assemble it from raw JSON.
- **A-005:** GPU information comes from both `HardwareInfo.GPUs` (static inventory from the hardware collector) and `GPUTelemetry` (runtime metrics). The summary uses the static inventory data from `HardwareInfo.GPUs` for model/vendor/VRAM, not runtime utilization metrics.

---

## 11. Open Questions

None. All requirements are sufficiently specified for implementation.

---

## 12. Dependencies

- **Existing telemetry pipeline:** The agent must be collecting and pushing telemetry data for the relevant collectors (system, os_info, cpu, memory, disk, hardware, usb, network, docker, ups).
- **Device detail page:** The frontend device detail page must exist as the mounting point for the new export controls.
- **Server telemetry retrieval:** The server must already support fetching the latest telemetry snapshot for a given device ID.
