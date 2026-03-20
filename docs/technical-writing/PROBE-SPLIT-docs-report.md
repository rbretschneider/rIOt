# Documentation Report

**Story ID:** PROBE-SPLIT
**Author:** Technical Writer Agent
**Date:** 2026-03-20

---

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | Updated `Uptime probes` feature description to describe the unified page with both server and device probe sections. Expanded the API table to include the new `GET /api/v1/device-probes` endpoint and the full set of device probe endpoints that were previously undocumented. |
| `internal/server/handlers/device_probes.go` | Inline comment | Replaced a "what" comment on the hostname lookup map with a "why" comment explaining that Go's zero-value map miss is intentional — it produces an empty string for orphaned probes, and the frontend handles the fallback to `device_id` for display. |

---

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` line 12 | The stability disclaimer ("relatively stable at version 2.24", "prior to v2.24 I recommend starting over") is stale — the project is well past v2.38 and this note has not been updated since early development. It will confuse new users. | Remove or rewrite as a current stability statement in a dedicated pass. |
| `README.md` | No CHANGELOG.md exists at the project root. The project is at v2.38 with a documented release workflow and a multi-story pipeline. A changelog would give adopters and contributors visibility into what changed between releases. | Create `CHANGELOG.md` with entries backfilled from git tags and GitHub Release notes. Treat as a separate task. |
| `README.md` API table (Dashboard section) | The device probe CRUD endpoints (`GET/POST/PUT/DELETE /api/v1/devices/:id/device-probes`) were not listed anywhere in the API reference prior to this story, even though those endpoints have existed since before PROBE-SPLIT. They were only added to the table as part of this story's documentation pass. Their absence was pre-existing stale omission. The endpoints themselves are not new — only their documentation is. | No further action needed. The omission has been corrected as part of this story. |

---

## Accuracy Flags

No discrepancies found between the ADD, implementation report, QA report, and the actual code.

One implementation deviation was documented (DEV-001: `editingDeviceId` useRef pattern in `Probes.tsx`) — the code comment at `Probes.tsx` line 37–39 accurately describes this pattern and its reason. The ADD described the intended outcome; the implementation detail is within the ADD's stated constraint that `DeviceProbeModal.tsx` must not be modified.

The QA report notes that `GET /api/v1/device-probes` is registered at `router.go` line 233 inside the admin-auth group. This was verified against the actual router source.

---

## Notes for Future Writers

**Two probe types, one page:** The Probes page (`/probes`) now hosts two distinct tables. Server probes and device probes are different entities with different API endpoints, different probe type options, and different execution models. When documenting any probe feature in the future, always clarify which type is affected. Do not write "probes" without qualifying "server" or "device" unless the statement genuinely applies to both.

**Device probe creation flow:** Creating a new device probe from the unified Probes page navigates the user to the per-device probes page (`/devices/:id/probes`). It does not open an inline modal. This is intentional (AD-003). Any user-facing documentation about "adding a device probe" must describe this two-step flow: select a device from the dropdown, then use the Add Probe button on the device's probes page.

**`GET /api/v1/device-probes` vs `GET /api/v1/devices/:id/device-probes`:** The new top-level endpoint returns all device probes across all devices enriched with `device_hostname`. The per-device endpoint returns only that device's probes without the hostname field. The response shapes are intentionally different (`DeviceProbeWithResultEnriched` vs `DeviceProbeWithResult`). Do not document them as interchangeable.

**Query key `all-device-probes`:** The frontend uses `['all-device-probes']` as the react-query cache key for the new endpoint, distinct from `['device-probes', id]` used by the per-device page. Mutations from the unified page invalidate only `['all-device-probes']`. If a future story adds server-side push invalidation or cross-key coordination, this distinction will matter.

**No inline result history on the unified page:** Device probes on the unified Probes page do not have a History button. This is by design (AD-004). The per-device page (`/devices/:id/probes`) retains full result history. Do not document the unified page as having this capability.
