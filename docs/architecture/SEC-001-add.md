# Architecture Decision Document

- **Story ID:** SEC-001
- **FRD Reference:** docs/requirements/SEC-001-frd.md
- **Author:** Architect Agent
- **Date:** 2026-03-22
- **Status:** FINAL

---

## 1. Summary

This story moves the security score column from the Fleet Overview page to the Security page, extends the backend `/api/v1/security/devices` endpoint to include update and certificate telemetry fields, and enriches the Security page with fleet-wide overview cards (average score, expiring certs) plus new per-device columns (security updates count, auto-updates status). The frontend gains client-side sorting on the Security page table. No new backend endpoints are created; existing endpoints are extended.

---

## 2. Technical Context

**Current state:**

- The Security page (`web/src/pages/Security.tsx`) displays overview cards (failed logins, logged-in users, firewall active, reporting count) and a per-device table with columns: Device, Firewall, SELinux, AppArmor, Failed Logins, Logged In, Open Ports. No sorting, no security scores.
- The Fleet Overview page (`web/src/pages/FleetOverview.tsx`) has a `MiniScore` component (lines 511-539) that fetches per-device security scores via `GET /api/v1/devices/{id}/security-score` and renders a radial indicator. Clicking opens `SecurityScoreModal`. This is gated behind `isEnabled('security_score')`.
- Helper functions `miniScoreColor` and `miniStrokeColor` are defined locally in `FleetOverview.tsx` (lines 491-509).
- The backend handler `SecurityDevices` in `internal/server/handlers/security.go` returns a `deviceSecurityInfo` struct with basic security fields. It already calls `GetAllLatestSnapshots()` which returns full telemetry including `Updates` and `WebServers` data -- these fields are currently ignored.
- The backend handler `SecurityOverview` similarly iterates over all snapshots but only extracts security telemetry.
- The `SecurityScoreModal` component at `web/src/components/SecurityScoreModal.tsx` is a standalone component that receives a `SecurityScoreResult` and renders the full breakdown. It is used unmodified.
- The `gradeColor` function is duplicated: once in `SecurityScoreModal.tsx` (line 47) and conceptually as `miniScoreColor` in `FleetOverview.tsx` (line 491). Both produce the same A-F color mapping.

**What needs to change:**

1. Backend: extend `deviceSecurityInfo` struct and `securityOverview` struct to include update/cert fields
2. Frontend: remove score column from Fleet Overview, add score column and new columns to Security page, add overview cards, add sorting
3. Extract shared color/score utilities so both pages can use them

---

## 3. Architecture Decisions

### AD-001: Extend existing `/api/v1/security/devices` endpoint rather than creating new endpoints

**Decision:** Add `pending_security_count` (int), `unattended_upgrades` (*bool, nullable), and `certs_expiring_soon` (int) fields to the existing `deviceSecurityInfo` response struct. The handler already fetches full telemetry snapshots via `GetAllLatestSnapshots()`, so the data is available -- it just needs to be extracted.

**Rationale:** The handler already has the full telemetry snapshot in memory. Extracting three additional fields from `snap.Data.Updates` and `snap.Data.WebServers` requires zero additional database queries. Creating new endpoints would add unnecessary API surface and force the frontend to make additional requests.

**Alternatives Considered:**
- Separate endpoint for update/cert data: Rejected -- would require an additional frontend fetch and the data is already available in the same snapshot.
- Frontend fetching from telemetry endpoint per device: Rejected by the FRD explicitly (Section 7: "without requiring the frontend to make additional per-device telemetry API calls").

**Consequences:** The security devices endpoint response grows by three fields. Existing consumers (the current Security page) will simply ignore the new fields until the frontend is updated.

---

### AD-002: Extend `/api/v1/security/overview` with fleet-wide cert expiry count

**Decision:** Add `certs_expiring_soon` (int) and `total_certs` (int) fields to the `securityOverview` response struct. The overview handler already iterates all snapshots; it will additionally count certificates with `DaysLeft <= 30` from `snap.Data.WebServers`.

**Rationale:** The Certificates summary card (FR-017/FR-018/FR-019) needs a fleet-wide count. Computing this server-side in the existing overview endpoint avoids the frontend needing to iterate all device data to compute a fleet aggregate. The handler already iterates all snapshots, so adding cert counting is a trivial extension.

**Alternatives Considered:**
- Client-side computation from the devices list: Viable but inconsistent with how other overview stats are computed (server-side). The overview endpoint is the canonical source for fleet aggregates.

**Consequences:** Two new fields on the overview response. The fleet average score will still be computed client-side (per AD-003).

---

### AD-003: Compute fleet average score client-side

**Decision:** The fleet average security score (FR-009) will be computed client-side from the individual per-device scores fetched for the table. The Security page will fetch security scores for all devices using `Promise.allSettled` with the existing `GET /api/v1/devices/{id}/security-score` endpoint, then compute the arithmetic mean of successful responses.

**Rationale:** The security score computation is non-trivial (it calls `scoring.Score()` which evaluates multiple categories against the full telemetry data plus device-level config like `auto_patch`). Computing a fleet-wide score server-side would require either: (a) a new endpoint that duplicates the scoring loop for all devices, or (b) storing scores in the database. Both add significant complexity for a display-only metric. The FRD (BR-002) explicitly states: "The fleet average score must be computed client-side from the individual device scores that are fetched."

**Alternatives Considered:**
- New backend endpoint `GET /api/v1/security/fleet-score`: Would be cleaner but is more work and BR-002 mandates client-side computation.
- Adding score to the `deviceSecurityInfo` response: Would require the backend to run the scoring engine for every device in the list endpoint, which is expensive and couples the list endpoint to the scoring engine.

**Consequences:** The Security page will make N+2 API calls (1 overview + 1 devices list + N score fetches). For the NFR-001 target of 50 devices, this is 52 concurrent requests. React Query's built-in request deduplication and the browser's HTTP/2 multiplexing make this feasible. The score queries use `staleTime: 5 * 60_000` (5 minutes) matching the existing pattern in FleetOverview.

---

### AD-004: Extract shared score color utilities to a dedicated module

**Decision:** Create `web/src/utils/security.ts` containing the `gradeColor`, `miniScoreColor`, `miniStrokeColor`, and `gradeFromScore` functions. Both `FleetOverview.tsx` (until the score column is removed) and `Security.tsx` will import from this shared module. The `MiniScore` component will be relocated to `web/src/components/MiniScore.tsx` for reuse on the Security page.

**Rationale:** The color mapping functions are currently duplicated between `FleetOverview.tsx` and `SecurityScoreModal.tsx`. Extracting them prevents drift and enables reuse. The `MiniScore` component is needed on the Security page and should not be copied.

**Alternatives Considered:**
- Inline the functions in Security.tsx: Would create a third copy of the same logic. Rejected.
- Put in SecurityScoreModal.tsx: That component is a full modal, not the right place for utility functions.

**Consequences:** FleetOverview.tsx imports shrink (removed local functions). SecurityScoreModal.tsx can optionally be updated to import `gradeColor` from the shared module for consistency, but this is not required for this story (the FRD says the modal is not modified).

---

### AD-005: Use `unattended_upgrades` as nullable pointer (`*bool`) in the backend response

**Decision:** The `unattended_upgrades` field in `deviceSecurityInfo` will be `*bool` (JSON: `true`, `false`, or `null`). When the device has no update telemetry (`snap.Data.Updates == nil`), the field will be `null`. When update telemetry exists, the field reflects `Updates.UnattendedUpgrades`.

**Rationale:** The frontend must distinguish between "disabled" (false) and "no data" (null/missing) per FR-016. A plain `bool` defaults to `false` in Go, making it impossible to distinguish "explicitly disabled" from "no telemetry data."

**Alternatives Considered:**
- String field ("enabled"/"disabled"/""): More verbose, less type-safe. Rejected.
- Separate `has_update_data` boolean: Over-engineered. Rejected.

**Consequences:** Frontend TypeScript type for this field becomes `boolean | null`. The frontend checks `=== null` for the "no data" case.

---

### AD-006: Client-side table sorting with three sortable columns

**Decision:** Implement client-side sorting on the Security page table using React state, matching the pattern already used in `FleetOverview.tsx` (lines 104-141). Sortable columns: Score, Failed Logins, Security Updates. Default sort: Score ascending (lowest first, per FR-021). Other columns remain non-sortable.

**Rationale:** The device list is small (target: up to 50 devices per NFR-001). Server-side sorting would require API changes for no benefit. The FleetOverview already uses this exact pattern, so the implementation is consistent.

**Alternatives Considered:**
- Server-side sorting: Unnecessary for small data sets, adds API complexity.
- All columns sortable: Over-scoped; the FRD only requires Score, Failed Logins, and Security Updates.

**Consequences:** The sorting implementation follows the existing `SortHeader` / `toggleSort` pattern from FleetOverview.

---

### AD-007: Remove the `security_score` feature flag gate from Fleet Overview

**Decision:** Remove the `isEnabled('security_score')` conditional rendering of the Score column header and `MiniScore` cell from `FleetOverview.tsx`. The feature flag definition in `useFeatures.ts` will be updated: the `security_score` key's description will be changed to reflect that it now controls the Security page score display (and device detail, per the existing description). The Security page will NOT gate on this feature flag (per A-001 in the FRD).

**Rationale:** The FRD (FR-001, FR-002, FR-003) explicitly requires removing the score column from Fleet Overview. A-001 states the Security page always shows scores if data exists.

**Alternatives Considered:**
- Keep the feature flag and just move the gated section: The FRD says to remove it from Fleet Overview entirely. The flag can remain in the system for other uses (device detail page) but should not gate anything on Fleet Overview.

**Consequences:** The Score column disappears from Fleet Overview regardless of feature flag state. The `security_score` feature flag continues to control the score gauge on the device detail page (its other documented use).

---

## 4. Component Changes

| Action | File Path | Purpose |
|--------|-----------|---------|
| MODIFY | `internal/server/handlers/security.go` | Add `pending_security_count`, `unattended_upgrades`, `certs_expiring_soon` to `deviceSecurityInfo` struct; add `certs_expiring_soon`, `total_certs` to `securityOverview` struct; update both handler functions to populate new fields from existing telemetry |
| CREATE | `web/src/utils/security.ts` | Shared score color utilities: `gradeColor()`, `gradeFromScore()`, `scoreColor()` (bg variant) |
| CREATE | `web/src/components/MiniScore.tsx` | Extracted `MiniScore` component from FleetOverview, plus `miniScoreColor` and `miniStrokeColor` helpers |
| MODIFY | `web/src/pages/Security.tsx` | Add Score column with MiniScore + modal, add Sec. Updates column, add Auto-Updates column, add fleet average score card, add certs expiring card, add client-side sorting, update table colSpan |
| MODIFY | `web/src/pages/FleetOverview.tsx` | Remove `MiniScore` component definition, remove `miniScoreColor`/`miniStrokeColor` functions, remove score column header and cell, remove `SecurityScoreModal` import and `scoreModal` state, remove `isEnabled('security_score')` gate for score column. Remove unused imports (`SecurityScoreResult` type, `SecurityScoreModal` component). |
| MODIFY | `web/src/api/client.ts` | Update `getSecurityDevices` return type to include new fields; update `getSecurityOverview` return type to include new fields |
| MODIFY | `web/src/hooks/useFeatures.ts` | Update `security_score` feature description to remove "fleet dashboard" reference since score is no longer on Fleet Overview |

---

## 5. Data Model Changes

### Backend response: `deviceSecurityInfo` (no database change)

**Before:**
```go
type deviceSecurityInfo struct {
    DeviceID        string `json:"device_id"`
    Hostname        string `json:"hostname"`
    Status          string `json:"status"`
    SELinux         string `json:"selinux"`
    AppArmor        string `json:"apparmor"`
    FirewallStatus  string `json:"firewall_status"`
    FailedLogins24h int    `json:"failed_logins_24h"`
    LoggedInUsers   int    `json:"logged_in_users"`
    OpenPorts       []int  `json:"open_ports"`
}
```

**After:**
```go
type deviceSecurityInfo struct {
    DeviceID             string `json:"device_id"`
    Hostname             string `json:"hostname"`
    Status               string `json:"status"`
    SELinux              string `json:"selinux"`
    AppArmor             string `json:"apparmor"`
    FirewallStatus       string `json:"firewall_status"`
    FailedLogins24h      int    `json:"failed_logins_24h"`
    LoggedInUsers        int    `json:"logged_in_users"`
    OpenPorts            []int  `json:"open_ports"`
    PendingSecurityCount int    `json:"pending_security_count"`
    UnattendedUpgrades   *bool  `json:"unattended_upgrades"`
    CertsExpiringSoon    int    `json:"certs_expiring_soon"`
}
```

### Backend response: `securityOverview` (no database change)

**Before:**
```go
type securityOverview struct {
    TotalDevices      int `json:"total_devices"`
    DevicesReporting  int `json:"devices_reporting"`
    TotalFailedLogins int `json:"total_failed_logins"`
    TotalLoggedIn     int `json:"total_logged_in"`
    FirewallActive    int `json:"firewall_active"`
    FirewallInactive  int `json:"firewall_inactive"`
    SELinuxEnforcing  int `json:"selinux_enforcing"`
    AppArmorEnabled   int `json:"apparmor_enabled"`
}
```

**After:**
```go
type securityOverview struct {
    TotalDevices      int `json:"total_devices"`
    DevicesReporting  int `json:"devices_reporting"`
    TotalFailedLogins int `json:"total_failed_logins"`
    TotalLoggedIn     int `json:"total_logged_in"`
    FirewallActive    int `json:"firewall_active"`
    FirewallInactive  int `json:"firewall_inactive"`
    SELinuxEnforcing  int `json:"selinux_enforcing"`
    AppArmorEnabled   int `json:"apparmor_enabled"`
    CertsExpiringSoon int `json:"certs_expiring_soon"`
    TotalCerts        int `json:"total_certs"`
}
```

### Frontend TypeScript types (inline in `api/client.ts`)

Update `getSecurityDevices` return type to add:
```typescript
pending_security_count: number
unattended_upgrades: boolean | null
certs_expiring_soon: number
```

Update `getSecurityOverview` return type to add:
```typescript
certs_expiring_soon: number
total_certs: number
```

### No database migrations

All new data is derived from existing telemetry snapshots stored as JSONB. No schema changes are required.

---

## 6. API / Interface Contract

### `GET /api/v1/security/devices` (MODIFIED)

**Response: 200 OK**
```json
[
  {
    "device_id": "abc-123",
    "hostname": "server1",
    "status": "online",
    "selinux": "",
    "apparmor": "enabled",
    "firewall_status": "active",
    "failed_logins_24h": 3,
    "logged_in_users": 1,
    "open_ports": [22, 80, 443],
    "pending_security_count": 5,
    "unattended_upgrades": false,
    "certs_expiring_soon": 1
  }
]
```

Field semantics for new fields:
- `pending_security_count`: `0` when updates telemetry exists but no security updates pending; `0` when no updates telemetry exists (frontend uses presence of `unattended_upgrades` to distinguish "no data" vs "zero updates")
- `unattended_upgrades`: `true`/`false` when update telemetry exists; `null` when no update telemetry available
- `certs_expiring_soon`: count of certs with `days_left <= 30` from web server telemetry; `0` if no web server data

**Response: 500 Internal Server Error**
```json
{ "error": "failed to get telemetry" }
```

### `GET /api/v1/security/overview` (MODIFIED)

**Response: 200 OK**
```json
{
  "total_devices": 10,
  "devices_reporting": 8,
  "total_failed_logins": 15,
  "total_logged_in": 3,
  "firewall_active": 7,
  "firewall_inactive": 1,
  "selinux_enforcing": 2,
  "apparmor_enabled": 5,
  "certs_expiring_soon": 3,
  "total_certs": 12
}
```

### `GET /api/v1/devices/{id}/security-score` (UNCHANGED)

No changes to this endpoint. Continues to return `SecurityScore` struct.

---

## 7. Sequence / Flow

### Security Page Load Flow

1. User navigates to `/security`
2. Security page component mounts
3. Two queries fire in parallel (existing, via React Query):
   a. `GET /api/v1/security/overview` -> overview data (now includes `certs_expiring_soon`, `total_certs`)
   b. `GET /api/v1/security/devices` -> device list (now includes `pending_security_count`, `unattended_upgrades`, `certs_expiring_soon`)
4. Once device list returns, for each device, fire `GET /api/v1/devices/{device_id}/security-score` (using React Query with `staleTime: 5 * 60_000` and `Promise.allSettled` pattern)
5. As individual score queries resolve, update score display per device row
6. Once all scores have resolved (or settled), compute fleet average from successful results
7. Render overview cards: Fleet Score (with average), Failed Logins, Logged-in Users, Firewall Active, Reporting, Certs Expiring (conditional)
8. Render per-device table sorted by Score ascending (worst first)
9. User clicks a device's score -> `SecurityScoreModal` opens with that device's `SecurityScoreResult`
10. User clicks a column header -> client-side re-sort of the table

### Score Column Removal from Fleet Overview

1. On the Fleet Overview page, the Score column header, `MiniScore` cell, and `SecurityScoreModal` rendering are removed
2. The `isEnabled('security_score')` gate for the Score column is removed
3. The `MiniScore` component definition, `miniScoreColor`, and `miniStrokeColor` are removed from FleetOverview.tsx (now in separate files)
4. The `scoreModal` state and `SecurityScoreModal` import are removed from FleetOverview.tsx

---

## 8. Acceptance Criteria Mapping

| AC ID | Fulfilled By | Test Strategy |
|-------|-------------|---------------|
| AC-001 | `FleetOverview.tsx`: Remove score column header (`<th>Score</th>`), remove `<MiniScore>` cell, remove `SecurityScoreModal` rendering, remove `isEnabled('security_score')` gate, remove `scoreModal` state | Unit: render FleetOverview, assert no Score column header exists, assert no MiniScore component rendered |
| AC-002 | `Security.tsx`: Add Score column as first data column after Device, render score with `gradeColor()` color coding; `MiniScore.tsx`: extracted component with radial indicator | Unit: render Security page with mock device+score data, assert Score column exists at correct position, assert color classes match grade |
| AC-003 | `Security.tsx`: `onClick` on score value calls `setScoreModal()`, renders `SecurityScoreModal` when `scoreModal` state is set | Unit: render Security page, simulate click on score, assert SecurityScoreModal is rendered with correct props |
| AC-004 | `Security.tsx`: When score query returns no data (404/error), display `"-"` with `text-gray-600` class | Unit: render with device that has no score data, assert dash rendered with muted styling |
| AC-005 | `Security.tsx`: Compute mean of all resolved scores, display in Fleet Score overview card with `gradeColor()` styling | Unit: provide mock scores for 3 devices, assert card shows correct average and color |
| AC-006 | `Security.tsx`: Render "Sec. Updates" column from `pending_security_count` field, color amber/red when > 0, muted when 0; `handlers/security.go`: populate field from `snap.Data.Updates.PendingSecurityCount` | Backend unit: test handler returns `pending_security_count` from telemetry; Frontend unit: render with various counts, assert correct colors |
| AC-007 | `Security.tsx`: Render "Auto-Updates" column from `unattended_upgrades` field, green for true, amber for false, dash for null; `handlers/security.go`: populate as `*bool` from `snap.Data.Updates.UnattendedUpgrades` | Backend unit: test handler returns correct `*bool` values; Frontend unit: render with true/false/null, assert correct text and colors |
| AC-008 | `Security.tsx`: Render "Certs Expiring" card when `overview.certs_expiring_soon > 0`, with amber/red color; `handlers/security.go`: count certs with `DaysLeft <= 30` in overview handler | Backend unit: test overview handler counts expiring certs; Frontend unit: render with expiring certs, assert card visible with correct count and color |
| AC-009 | `Security.tsx`: Do not render "Certs Expiring" card when `overview.total_certs === 0` (no cert data across fleet) | Frontend unit: render with `total_certs: 0`, assert no Certs Expiring card |
| AC-010 | `Security.tsx`: Default `sortKey` state to `'score'`, default `sortDir` to `'asc'`; `SortHeader` component on Score, Failed Logins, Sec. Updates columns | Frontend unit: render with multiple devices, assert initial order is score ascending; simulate header click, assert order changes |

---

## 9. Error Handling

| Failure Mode | Handling | HTTP Status | Logging |
|-------------|----------|-------------|---------|
| `GetAllLatestSnapshots` fails in SecurityDevices handler | Return error JSON | 500 | Existing slog error in handler |
| `GetAllLatestSnapshots` fails in SecurityOverview handler | Return error JSON | 500 | Existing slog error in handler |
| Individual device score fetch fails (404 or 500) | Frontend: score displays as "-", device excluded from fleet average | N/A (client-side) | None (expected for devices without telemetry) |
| Device has no update telemetry (`snap.Data.Updates == nil`) | `pending_security_count` = 0, `unattended_upgrades` = null | 200 | None |
| Device has no web server telemetry (`snap.Data.WebServers == nil`) | `certs_expiring_soon` = 0 | 200 | None |

---

## 10. Security Considerations

No new security attack vectors are introduced by this story:

- No new endpoints are created; existing endpoints are extended with additional read-only fields derived from existing telemetry
- No new authentication or authorization requirements; the security endpoints use the same admin auth middleware as before
- No user input is accepted by the modified endpoints; they are GET-only
- The score data, update counts, and cert expiry info are all derived from telemetry that the device agent already pushes; no new data collection is introduced

---

## 11. Performance Considerations

- **Backend:** The `SecurityDevices` and `SecurityOverview` handlers already call `GetAllLatestSnapshots()` and iterate all results. Adding field extraction from `Updates` and `WebServers` is O(1) per device -- negligible overhead.
- **Frontend:** The Security page will fire N parallel score requests (one per device). For 50 devices (NFR-001 target), this is 50 small GET requests. React Query's `staleTime: 5 * 60_000` ensures scores are cached for 5 minutes. The browser's HTTP/2 connection multiplexing handles concurrent requests efficiently.
- **No new indexes required:** No database schema changes are being made.
- **Sorting is client-side** on a list of at most ~50 devices -- negligible computation.

---

## 12. Implementation Notes for Engineers

### Backend (`internal/server/handlers/security.go`)

1. The `SecurityDevices` handler already has `snap` (type `models.TelemetrySnapshot`) in the loop at line 94. Access `snap.Data.Updates` and `snap.Data.WebServers` to populate the new fields.

2. For `pending_security_count`: if `snap.Data.Updates != nil`, use `snap.Data.Updates.PendingSecurityCount`. Otherwise, leave as zero (Go default).

3. For `unattended_upgrades`: declare as `*bool` in the struct. If `snap.Data.Updates != nil`, set it to `&snap.Data.Updates.UnattendedUpgrades`. Otherwise, leave as nil (JSON null).

4. For `certs_expiring_soon`: iterate `snap.Data.WebServers.Servers[].Certs[]` and count entries where `DaysLeft <= 30`. Guard against nil `WebServers` and nil `Servers` slices.

5. For the overview handler, apply the same cert counting across all snapshots to populate `CertsExpiringSoon` and `TotalCerts`.

### Frontend

1. **`web/src/utils/security.ts`**: Export these functions:
   - `gradeColor(grade: string): string` -- returns Tailwind text color class (A=emerald, B=blue, C=amber, D=orange, F=red)
   - `gradeFromScore(score: number): string` -- returns letter grade from numeric score. Must match the server-side thresholds exactly from `internal/server/scoring/engine.go`: `>=90` = A, `>=75` = B, `>=60` = C, `>=40` = D, `<40` = F.

2. **`web/src/components/MiniScore.tsx`**: Move the `MiniScore` function component, `miniScoreColor`, and `miniStrokeColor` from `FleetOverview.tsx`. The component takes `{ deviceId: string; onShowModal: (s: SecurityScoreResult) => void }` props and uses `useQuery` to fetch the score.

3. **`web/src/pages/Security.tsx`**: Use individual `useQuery` calls for each device's score, keyed as `['security-score', deviceId]`. This matches the existing pattern in `MiniScore` and ensures React Query deduplicates requests if the same device ID appears in both pages. Use the `enabled` option to only fetch when devices are loaded.

4. **Sorting**: Follow the exact `SortHeader` / `toggleSort` pattern from `FleetOverview.tsx`. The sort key type should be a union of the sortable column identifiers. When sorting by score, devices with no score data should sort to the bottom (treat as Infinity for ascending, -Infinity for descending).

5. **Fleet average score card**: After all score queries settle, filter to only successful results, compute the arithmetic mean, round to nearest integer, derive the grade using `gradeFromScore`, and display with `gradeColor` styling.

6. **Column order in the Security table**: Device | Score | Firewall | SELinux | AppArmor | Sec. Updates | Auto-Updates | Failed Logins | Logged In | Open Ports. This puts the new columns in logical groupings. Update the `min-w` value from `[640px]` to `[900px]` to accommodate the wider table.

7. **Update `colSpan`**: The empty-state row currently uses `colSpan={7}`. Update to match the new column count (10).

8. **Do not modify `SecurityScoreModal.tsx`**: The FRD explicitly states this component is out of scope. Import and use it as-is.

### Grade Thresholds (reference from `internal/server/scoring/engine.go`)

```
>=90 -> A
>=75 -> B
>=60 -> C
>=40 -> D
<40  -> F
```

The frontend `gradeFromScore()` function must use these exact thresholds.

---

## 13. Definition of Done

- [ ] All component changes in Section 4 implemented
- [ ] All AC mappings in Section 8 have corresponding tests with AC references
- [ ] `GET /api/v1/security/devices` returns `pending_security_count`, `unattended_upgrades`, `certs_expiring_soon` fields
- [ ] `GET /api/v1/security/overview` returns `certs_expiring_soon`, `total_certs` fields
- [ ] Score column removed from Fleet Overview page (no feature flag gate, no MiniScore, no SecurityScoreModal)
- [ ] Score column displayed on Security page as first data column after Device
- [ ] Fleet average score card displayed in overview section
- [ ] Certs Expiring card conditionally displayed in overview section
- [ ] Table sorting works on Score, Failed Logins, and Sec. Updates columns
- [ ] Default sort is Score ascending (worst first)
- [ ] No new linting errors introduced (`npm run lint` and `go vet ./...`)
- [ ] Full test suite is green (`make test`)
- [ ] No new dependencies added
