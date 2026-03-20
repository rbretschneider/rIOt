# Implementation Report

- **Story ID:** PROBE-SPLIT
- **Engineer:** Senior Dev Agent
- **Date:** 2026-03-20

---

## Detected Stack

- **Backend:** Go 1.23, chi v5 router, pgx v5 DB driver, log/slog
- **Frontend:** React 18 + TypeScript, Vite, Tailwind CSS, @tanstack/react-query, vitest + @testing-library/react
- **Test runners:** `go test ./...` (Go), `cd web && npm run test:run` (frontend)

---

## Completed Components

| File | Action | Notes |
|------|--------|-------|
| `internal/models/device_probes.go` | MODIFIED | Added `DeviceProbeWithResultEnriched` struct embedding `DeviceProbeWithResult` + `DeviceHostname string` |
| `internal/server/db/interfaces.go` | MODIFIED | Added `ListAll(ctx context.Context) ([]models.DeviceProbe, error)` to `DeviceProbeRepository` interface |
| `internal/server/db/device_probe_repo.go` | MODIFIED | Implemented `ListAll` method: same SELECT as `List` but without `WHERE device_id=$1` clause, ordered by id |
| `internal/server/handlers/device_probes.go` | MODIFIED | Added `ListAllDeviceProbes` handler following enrichment pattern from `ListDeviceProbes`, plus device hostname map |
| `internal/server/router.go` | MODIFIED | Registered `GET /api/v1/device-probes` in admin-auth group |
| `internal/testutil/mocks.go` | MODIFIED | Added `ListAll` method to `MockDeviceProbeRepo` returning all probes without device_id filter |
| `web/src/types/models.ts` | MODIFIED | Added `DeviceProbeWithResultEnriched` interface extending `DeviceProbeWithResult` with `device_hostname: string` |
| `web/src/api/client.ts` | MODIFIED | Added `getAllDeviceProbes()` function, imported `DeviceProbeWithResultEnriched` type |
| `web/src/pages/Probes.tsx` | MODIFIED | Rewrote to two-section table layout (Server Probes + Device Probes) matching AlertRuleSettings pattern |

### New test files

| File | Notes |
|------|-------|
| `internal/server/handlers/device_probes_handler_test.go` | 7 Go tests for `ListAllDeviceProbes` handler |
| `web/src/pages/Probes.test.tsx` | 26 frontend tests covering all 9 ACs |

---

## Test Summary

### AC ID to Test Mapping

| AC ID | Test File | Tests | Status |
|-------|-----------|-------|--------|
| AC-001 | `web/src/pages/Probes.test.tsx` | `[AC-001]` describe block (6 tests) | PASS |
| AC-002 | `web/src/pages/Probes.test.tsx` | `[AC-002]` describe block (1 test) | PASS |
| AC-003 | `web/src/pages/Probes.test.tsx` | `[AC-003]` describe block (1 test) | PASS |
| AC-004 | `web/src/pages/Probes.test.tsx`, `device_probes_handler_test.go` | `[AC-004]` describe blocks (7 frontend + 7 backend tests) | PASS |
| AC-005 | `web/src/pages/Probes.test.tsx` | `[AC-005]` describe block (2 tests) | PASS |
| AC-006 | `web/src/pages/Probes.test.tsx` | `[AC-006]` describe block (1 test) | PASS |
| AC-007 | `web/src/pages/Probes.test.tsx` | `[AC-007]` describe block (5 tests) | PASS |
| AC-008 | `web/src/pages/Probes.test.tsx` | `[AC-008]` describe block (2 tests) | PASS |
| AC-009 | `web/src/pages/Probes.test.tsx` | `[AC-009]` describe block (1 test) | PASS |

### Test Run Output

**Go tests (`go test ./...`):**

```
ok  github.com/DesyncTheThird/rIOt/internal/agent
ok  github.com/DesyncTheThird/rIOt/internal/agent/collectors
ok  github.com/DesyncTheThird/rIOt/internal/resilient
ok  github.com/DesyncTheThird/rIOt/internal/server
ok  github.com/DesyncTheThird/rIOt/internal/server/auth
ok  github.com/DesyncTheThird/rIOt/internal/server/ca
ok  github.com/DesyncTheThird/rIOt/internal/server/events
ok  github.com/DesyncTheThird/rIOt/internal/server/handlers
ok  github.com/DesyncTheThird/rIOt/internal/server/middleware
ok  github.com/DesyncTheThird/rIOt/internal/server/notify
ok  github.com/DesyncTheThird/rIOt/internal/server/probes
ok  github.com/DesyncTheThird/rIOt/internal/server/scoring
ok  github.com/DesyncTheThird/rIOt/internal/server/updates
ok  github.com/DesyncTheThird/rIOt/internal/server/websocket
```

**Frontend tests (`cd web && npm run test:run`):**

```
Test Files  9 passed (9)
     Tests  91 passed (91)
  Start at  11:24:08
  Duration  2.50s
```

**`go vet ./...`:** No output (clean)

**`tsc --noEmit`:** No output (clean)

---

## Deviations from ADD

### DEV-001: `DeviceProbeForm` does not carry `device_id`

**ADD Section 12, note 8** states: "The save mutation on the Probes page will use `api.updateDeviceProbe(probe.device_id, probe.id, data)`". However, `DeviceProbeForm` (defined in `DeviceProbeModal.tsx`, which must not be modified) does not have a `device_id` field.

**Resolution:** A `useRef<string>` (`editingDeviceId`) stores the `device_id` alongside the modal state. When the Edit button is clicked, both `editingDeviceId.current` and `editingDevice` state are updated together. The save mutation reads `editingDeviceId.current`. This is a conservative, non-breaking approach that avoids any modification to `DeviceProbeModal.tsx`.

**Impact:** Zero functional impact. The correct `device_id` is always used for the mutation. No architect review required — this is an implementation detail that falls within the scope of the Add "no modal modifications" constraint.

---

## Notes for QA

### Security verification (per security review SEC-003)

The frontend renders `device_hostname` and `probe.name` as React JSX text nodes: `{probe.device_hostname}` and `{probe.name}`. No `dangerouslySetInnerHTML` is used anywhere in `Probes.tsx`. The test `[AC-004]` includes an explicit assertion verifying this: it checks that the hostname link's `textContent` equals the raw hostname string without any HTML interpretation.

### Route location in router.go

The new `GET /api/v1/device-probes` route is registered inside the `r.Group(func(r chi.Router) { r.Use(adminAuth) ... })` block, alongside the existing probe routes. It is NOT inside the `/api/v1/devices/{id}` route group. QA should verify this by checking that the endpoint returns 401 without a valid session cookie.

### DeviceProbes.tsx unchanged

The file `web/src/pages/DeviceProbes.tsx` was not modified. The AC-009 test verifies the module still exports a default function component.

### Device probe type column

The device probe type is rendered from `probe.type` (lowercase value, e.g. `"shell"`, `"http"`, `"port"`) with the Tailwind `uppercase` CSS class applied visually. This means the DOM text content is lowercase but renders uppercase in the UI. QA should visually confirm this on the rendered page.

### Empty state messages

- Server Probes empty state: "No server probes configured. Click \"Add Probe\" to start monitoring."
- Device Probes empty state: "No device probes configured. Click \"Add Probe\" to create one on a device."

### Add Probe (Device Probes section)

The "Add Probe" button in the Device Probes section opens a dropdown listing all devices by hostname. Clicking a device navigates to `/devices/:id/probes`. This is by design (AD-003) — probe creation is handled by the per-device page, not inline on the unified page.
