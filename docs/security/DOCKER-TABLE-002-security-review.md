# Security Review — DOCKER-TABLE-002

**Date:** 2026-10-20  
**Verdict:** APPROVED for implementation of the scoped ADD

**Implementation amendment approved:** Narrow the existing optional frontend `DockerRow.update_status` annotation to imported existing `UpdateStatus` from models. This permits the established typed verdict helper without a cast, adds no field/value/endpoint, and preserves its runtime precedence. `web/src/types/docker.ts` is added to the allowed component list; no helper semantic change is authorized or needed.

## Scope and boundary

Reviewed current ContainersPage, DataTable, URL-state parser, DockerRow/fleet types, update verdict helpers and frontend package/TypeScript configuration. The proposal changes frontend sizing, informational presentation and fallback sort only. It adds no dependency, endpoint, privilege, credential handling, server data field or remote command path.

## Required invariants

- Keep feature/auth routing guards and existing query gating untouched.
- React text escaping and canonical encoded container/stack paths remain authoritative. Marker text must not use raw HTML or untrusted markup.
- Update availability is informational. Reuse authoritative verdict precedence; pinned/check_failed/unknown must not become available from a stale legacy boolean. Do not interpret marker presence as permission or change action/confirmation policy.
- Preserve server action validation, self-role/host confirmations, selected target identity and safe external-port URL handling.
- Persisted column/view values remain untrusted preferences with current defensive parsing. No storage migration or collection URL rewrite is necessary.
- Preserve keyboard focus/virtualization and make availability available to assistive technology without relying on color or hover. Keep marker noninteractive so no new accidental action surface is created.

## Verification

AC-002/003 cover hidden-column availability and misleading verdict negatives; AC-004 covers explicit state preservation; AC-005 covers canonical links, preferences, selection and action regression. AC-001 requires real browser wide/portrait visual QA in addition to DOM tests. No unresolved security blocker within the scoped design. Existing backend authorization and stale telemetry risks are unchanged; this story makes no new freshness guarantee.
