# Formal Requirements — DOCKER-TABLE-002

**Date:** 2026-10-20  
**Scope:** Containers table sizing, update visibility and default Compose ordering

## 1. Outcome

The Containers table currently assigns all spare width to Name despite its nominal 184px size. Its update badge lives only in optional Image, so the default columns hide update availability. The default name ordering also scatters Compose members. Keep the existing table and controls, make Name compact, show known updates beside each container identity, and default to Stack ordering.

## 2. Requirements

- FR-001: Bound Name to its existing 184px column width; it remains pinned with the selection column. Long display names, icons and self-role labels truncate while full identity remains available through the existing title/link. Desktop slack belongs to a non-identity column, not Name. Preserve portrait horizontal scrolling, existing thin scrollbars and fixed 44px virtual rows.
- FR-002: Display a compact, accessible update-available marker within mandatory Name, independent of Image visibility and saved column preferences. It must remain visible when the name truncates. Move the existing Image badge rather than duplicating it. Image remains optional.
- FR-003: Use existing authoritative verdict semantics: `update_status` wins when provided; only `update_available` verdict or legacy boolean true means available. Pinned, failed checks, false and unknown never imply a newer image or a successful check. This presentation does not change action applicability, filtering or confirmations.
- FR-004: Missing or invalid sort defaults to Stack ascending. Explicit valid URL sorts and directions continue to win. Stack sorting keeps Compose members adjacent with deterministic host/name tie-breakers; standalone containers form one clearly labelled group. No collapsible groups, group header rows or new grouping controls.
- FR-005: Preserve existing URLs, search/host/stack/status filters, selection identity, table/tiles preference, saved columns, reset behavior, canonical resource/log links, frozen metric sort, actions and self-host confirmations. No new backend/API/model fields, dependencies or storage migration. The existing frontend `DockerRow.update_status` annotation may be narrowed from broad string to the existing shared `UpdateStatus` union to match authoritative helper input; this changes no wire value or runtime behavior.

## 3. Acceptance criteria

| ID | Evidence |
|---|---|
| AC-001 | Default and restored column sets keep Name 184px/pinned without grow. Wide viewport does not stretch Name. Long identity truncates while full name is discoverable. Portrait viewport retains bounded internal scrolling with no document-width regression. |
| AC-002 | Default table and persisted Image-hidden preference show an accessible update marker for an available container. Marker survives long-name truncation. Showing Image does not duplicate the marker. |
| AC-003 | Available verdict and legacy true show marker; pinned/check_failed/up_to_date override stale true and show no available marker; false/null/missing show none. Marker is informational and cannot trigger a mutation. |
| AC-004 | No sort and unknown sort resolve Stack asc; explicit name/status/host/image/CPU/memory/uptime and Stack desc retain existing behavior. Within equal stack labels, host then container name then row key make ordering deterministic; standalone placement is consistent. |
| AC-005 | Existing saved columns/view, URL filters, selection, canonical Name/Logs routes and action confirmations pass regression tests. Stack sorting preserves 44px rows/virtualization and does not add synthetic selectable rows. |

## 4. Gates

Read ADD and security review before implementation. Establish green frontend baseline, add AC-labelled tests, run focused and full frontend tests plus TypeScript/build checks. QA must inspect actual wide and portrait table layout because DOM-only tests cannot prove pixel allocation. Go/server/production behavior is outside scope.
