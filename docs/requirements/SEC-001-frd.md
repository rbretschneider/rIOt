# Formal Requirements Document

- **Story ID:** SEC-001
- **Title:** Enhance Security Page with Score Column and Additional Security Insights
- **Author:** Business Developer Agent
- **Date:** 2026-03-22
- **Status:** DRAFT

---

## 1. Executive Summary

The Security page in the rIOt dashboard currently displays basic per-device security telemetry (firewall status, SELinux/AppArmor, failed logins, logged-in users, open ports) but lacks the security score that already exists on the Fleet Overview page. This story moves the security score column from the Fleet Overview table to the Security page table and enriches the Security page with additional security-relevant data that the system already collects, making it the central hub for security visibility across the homelab fleet.

---

## 2. Background & Context

The rIOt platform already computes a per-device security score (0-100 with letter grade A-F) based on multiple categories: access control, patching, network, Docker, and system. This score is currently displayed as a mini radial indicator in the Fleet Overview table and expands into a detailed modal showing category breakdowns and individual findings with severity levels.

The current Security page shows only raw telemetry fields (firewall, MAC modules, failed logins, open ports) without the computed score, making it less useful than the Fleet Overview for security assessment. The user has identified this as a gap: the Security page should be the primary place for security posture visibility, not the Fleet Overview.

Additionally, the system already collects data that is security-relevant but not surfaced on the Security page: pending security updates, SSL/TLS certificate expiry from web servers, unattended upgrades status, and Docker container privilege information. Surfacing these on the Security page provides a more complete security picture without requiring new data collection.

---

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Dashboard User | Authenticated user viewing the rIOt web dashboard | Read access to all security data; ability to click into score details |

---

## 4. Functional Requirements

### Score Column Migration

- **FR-001:** The system must remove the security score column from the Fleet Overview page table.
- **FR-002:** The system must remove the `MiniScore` component rendering and the `SecurityScoreModal` trigger from the Fleet Overview page.
- **FR-003:** The system must remove the feature flag gate (`isEnabled('security_score')`) from the Fleet Overview page for the score column (the feature flag may still be used on the Security page if appropriate).
- **FR-004:** The Security page per-device table must display a "Score" column showing the security score for each device.
- **FR-005:** The Score column on the Security page must display the score as a numeric value (0-100) with a letter grade, color-coded by grade (A=emerald, B=blue, C=amber, D=orange, F=red), consistent with the existing `MiniScore` visual treatment from the Fleet Overview.
- **FR-006:** The Score column must be the first data column in the Security page table (immediately after the Device name column).
- **FR-007:** Clicking a device's security score on the Security page must open the existing `SecurityScoreModal` showing the full category breakdown and individual findings.
- **FR-008:** When a device has no security score data available, the Score column must display a dash ("-") in muted styling.

### Overview Cards Enhancement

- **FR-009:** The Security page must display a fleet-wide average security score in the overview cards section, showing the mean score across all devices that have score data.
- **FR-010:** The fleet average score card must be color-coded using the same grade color scheme as individual scores.

### Pending Security Updates Column

- **FR-011:** The Security page per-device table must display a "Security Updates" column showing the count of pending security-classified package updates for each device.
- **FR-012:** When a device has one or more pending security updates, the count must be displayed in red/amber coloring to indicate urgency.
- **FR-013:** When a device has zero pending security updates or no update data, the column must display "0" in muted styling or a dash if no update data is available.

### Unattended Upgrades Column

- **FR-014:** The Security page per-device table must display an "Auto-Updates" column showing whether unattended/automatic OS security updates are enabled for each device.
- **FR-015:** The Auto-Updates column must display "Enabled" in green when unattended upgrades are active, and "Disabled" in amber/red when not active.
- **FR-016:** When no update telemetry is available for a device, the column must display a dash in muted styling.

### SSL Certificate Expiry

- **FR-017:** The Security page must display a "Certificates" summary card in the overview section showing the count of SSL/TLS certificates expiring within 30 days across all devices.
- **FR-018:** When one or more certificates are expiring within 30 days, the summary card must use amber/red coloring.
- **FR-019:** When no certificate data exists across the fleet, the Certificates summary card must not be displayed.

### Table Sorting

- **FR-020:** The Security page per-device table must support column sorting by clicking column headers, at minimum for: Score, Failed Logins, and Security Updates columns.
- **FR-021:** The default sort order for the Security page table must be by Score ascending (worst scores first), so that the most concerning devices appear at the top.

---

## 5. Non-Functional Requirements

- **NFR-001:** [Performance] The Security page must load and render within 2 seconds for a fleet of up to 50 devices. Security scores are fetched per-device; the page must handle parallel requests efficiently.
- **NFR-002:** [Consistency] All color coding, grade labels, and score display formats must be identical to the existing `MiniScore` and `SecurityScoreModal` components' visual treatment.
- **NFR-003:** [Responsiveness] The Security page table must remain horizontally scrollable on small screens, consistent with the current implementation's `min-w-[640px]` pattern.

---

## 6. Business Rules

- **BR-001:** The security score is computed server-side per device based on telemetry data. The Security page must use the same API endpoint (`/api/v1/devices/{id}/security-score`) that the Fleet Overview currently uses.
- **BR-002:** The fleet average score (FR-009) must be computed client-side from the individual device scores that are fetched. It must only include devices that returned valid score data (not devices with no score).
- **BR-003:** The "Security Updates" count (FR-011) must reflect the `pending_security_count` field from the device's update telemetry, not the total pending update count.
- **BR-004:** Certificate expiry threshold for the summary card (FR-017) is 30 days. Certificates with `days_left <= 30` are considered "expiring soon."

---

## 7. Data Requirements

### Entities Involved

| Entity | Source | Fields Used |
|--------|--------|-------------|
| SecurityScoreResult | `GET /api/v1/devices/{id}/security-score` | `overall_score`, `grade`, `categories`, `max_score` |
| SecurityDeviceData | `GET /api/v1/security/devices` | `device_id`, `hostname`, `firewall_status`, `selinux`, `apparmor`, `failed_logins_24h`, `logged_in_users`, `open_ports` |
| SecurityOverview | `GET /api/v1/security/overview` | All existing fields |
| UpdateInfo (telemetry) | Embedded in `FullTelemetryData.updates` | `pending_security_count`, `unattended_upgrades` |
| ProxyCert (telemetry) | Embedded in `FullTelemetryData.web_servers.servers[].certs` | `days_left`, `subject` |

### New Data Needed from Backend

The current `/api/v1/security/devices` endpoint returns basic security telemetry. To support the new columns, the endpoint response must be extended to also include:

- `pending_security_count` (integer): count of pending security updates
- `unattended_upgrades` (boolean): whether automatic updates are enabled
- `certs_expiring_soon` (integer): count of SSL certificates expiring within 30 days

Alternatively, these fields may be sourced from existing telemetry endpoints if the architect determines that is more appropriate. The requirement is that the Security page has access to this data per device without requiring the frontend to make additional per-device telemetry API calls beyond the security score.

### Validation Rules

- Security scores are integers 0-100.
- Grades are single uppercase letters: A, B, C, D, F.
- `pending_security_count` is a non-negative integer.
- `unattended_upgrades` is a boolean.
- `days_left` on certificates is an integer (can be negative for expired certs).

### State Transitions

No new state transitions are introduced by this story.

---

## 8. Acceptance Criteria

**AC-001: Score column removed from Fleet Overview** [Maps to FR-001, FR-002, FR-003]
- Given: A user navigates to the Fleet Overview page
- When: The page renders the device table
- Then: No "Score" column is present in the table
- And: No `MiniScore` component is rendered for any device
- And: The `SecurityScoreModal` is not triggerable from the Fleet Overview page

**AC-002: Score column displayed on Security page** [Maps to FR-004, FR-005, FR-006]
- Given: A user navigates to the Security page with devices reporting security data
- When: The per-device table renders
- Then: A "Score" column is visible as the first data column after the Device name
- And: Each device's score is displayed as a numeric value with color coding matching the grade (A=emerald, B=blue, C=amber, D=orange, F=red)

**AC-003: Score modal opens from Security page** [Maps to FR-007]
- Given: A user is on the Security page and a device has a security score
- When: The user clicks the device's score value
- Then: The `SecurityScoreModal` opens showing the full category breakdown and findings for that device

**AC-004: No-score device handling** [Maps to FR-008]
- Given: A device exists but has no security score data available
- When: The Security page table renders
- Then: The Score column for that device displays "-" in muted gray styling

**AC-005: Fleet average score card** [Maps to FR-009, FR-010]
- Given: At least one device has a computed security score
- When: The Security page overview cards render
- Then: A card labeled "Fleet Score" (or similar) displays the mean score rounded to the nearest integer
- And: The card is color-coded by the grade of the average score

**AC-006: Security updates column** [Maps to FR-011, FR-012, FR-013]
- Given: A user navigates to the Security page
- When: The per-device table renders
- Then: A "Sec. Updates" column is visible showing pending security update counts
- And: Devices with 1 or more pending security updates display the count in amber/red
- And: Devices with 0 or no data display "0" in muted styling or "-"

**AC-007: Auto-updates column** [Maps to FR-014, FR-015, FR-016]
- Given: A user navigates to the Security page
- When: The per-device table renders
- Then: An "Auto-Updates" column shows whether unattended upgrades are enabled
- And: "Enabled" is displayed in green, "Disabled" in amber
- And: Devices with no update telemetry show "-"

**AC-008: Certificate expiry summary card** [Maps to FR-017, FR-018, FR-019]
- Given: One or more devices report web server certificate data with certificates expiring within 30 days
- When: The Security page overview section renders
- Then: A "Certs Expiring" summary card is displayed with the count of certificates where `days_left <= 30`
- And: The card uses amber or red coloring

**AC-009: Certificate card hidden when no data** [Maps to FR-019]
- Given: No devices report web server certificate data
- When: The Security page overview section renders
- Then: No "Certs Expiring" summary card is displayed

**AC-010: Table sorting** [Maps to FR-020, FR-021]
- Given: A user is on the Security page with multiple devices
- When: The page first loads
- Then: The table is sorted by Score ascending (lowest/worst scores at top)
- And: Clicking the Score column header toggles between ascending and descending sort
- And: Clicking Failed Logins or Sec. Updates headers sorts by those columns

---

## 9. Out of Scope

- Adding new security checks or findings to the security score computation (backend scoring logic is unchanged).
- Modifying the `SecurityScoreModal` component's internal layout or content.
- Adding remediation actions directly on the Security page (existing modal-based remediation via `SecurityScoreModal` is preserved).
- Per-device certificate detail listing on the Security page (only fleet-level summary count in overview card).
- Historical security score trending or graphs.
- New backend API endpoints for certificate aggregation (the implementation may use existing telemetry data or extend the existing security endpoints; this is an architectural decision).

---

## 10. Assumptions

- **A-001:** The `security_score` feature flag currently gating the Score column on Fleet Overview will no longer gate the column on the Security page. The Security page always shows scores if the data exists. The architect may decide whether to retain the feature flag or remove it entirely.
- **A-002:** The `/api/v1/security/devices` endpoint can be extended to include `pending_security_count`, `unattended_upgrades`, and `certs_expiring_soon` fields without breaking existing consumers.
- **A-003:** The fleet average score is a simple arithmetic mean. No weighting by device importance or status is applied.
- **A-004:** The `MiniScore` component and related helper functions (`miniScoreColor`, `miniStrokeColor`) currently defined in `FleetOverview.tsx` will be relocated or shared so they can be used on the Security page. This is an architectural decision.

---

## 11. Open Questions

None. The requirements are grounded in existing data models and capabilities.

---

## 12. Dependencies

| Dependency | Description |
|------------|-------------|
| `SecurityScoreModal` component | Existing component at `web/src/components/SecurityScoreModal.tsx`; reused without modification |
| `GET /api/v1/devices/{id}/security-score` | Existing endpoint; no changes required |
| `GET /api/v1/security/devices` | Existing endpoint; must be extended to include additional fields (pending security count, auto-updates status, cert expiry count) |
| `GET /api/v1/security/overview` | Existing endpoint; may need extension for fleet average score and cert expiry count |
| Security telemetry collector | Agent-side `security` collector must be enabled on devices for score data to be available |
| Updates telemetry collector | Agent-side `updates` collector must be enabled for security update counts and auto-update status |
| Webservers telemetry collector | Agent-side `webservers` collector must be enabled for certificate expiry data |
