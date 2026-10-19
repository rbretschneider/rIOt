# DOCKER-UX-001 implementation report

Stack: Go (agent/server), chi, pgx and PostgreSQL 16; React 19, TypeScript, Vite, Tailwind and TanStack Query; Go tests and Vitest/Testing Library. No new dependency was introduced.

## Scope and source decisions

Implemented P0–P4 from the [FRD](../requirements/DOCKER-UX-001-frd.md), [ADD](../architecture/DOCKER-UX-001-add.md), and [security review](../security/DOCKER-UX-001-security-review.md), including their resolved product Q&As. The user confirmed all five phases and parallel implementation across agent, server and dashboard. Existing lifecycle, authentication, command and UI foundations were reused.

| Phase | Delivered behavior |
|---|---|
| P0 | PART-001 merged and renumbered after lifecycle migration 23; partition retention, agent timestamps, byte/rate bounds, continuously refilling persisted device quotas, storage headroom and command-result budgets, bounded Docker service logging, read-only migration fingerprint and upgrade-only command |
| P1 | Docker navigation, fleet table/tiles, URL filters and selection, bounded bulk actions, canonical resource routes/headers, stored log history, capability-aware older-agent behavior, keyboard command palette and canonical recent identities |
| P2 | Explicit bounded live logs, shared follows and isolated/coalesced backfills, history/live identity and gaps, masked inspect, Activity with actor/steps/results, metadata and I/O rates |
| P3 | Fleet/detail stacks, trusted and acknowledged Compose paths, retained Down stacks, safe read-only Compose/env, lifecycle/self-stack protections, scoped update checks and auto-update policies |
| P4 | Opt-in bounded latest inventory, refresh, single-use cleanup previews, exact selected-item removal with current usage checks, per-item outcomes and protected resources |

## Integration and migrations

Lifecycle remains migration 23. The six imported partition migrations occupy 24–29; Docker migrations 30–32 add agent timestamps, storage usage and feature persistence. SQL schema introspection and integration-test introspection are scoped to the current schema. The original branch migration fixtures are retained in testdata to verify reconciliation without losing seeded rows. Already partitioned tables skip legacy autovacuum ALTERs. An original-branch converted database can reapply the renumbered files without changing its conversion record.

Telemetry inventory is separated from historical snapshots. Stored snapshots may omit oversized sections with explicit metadata; readers restore omitted prior sections instead of inventing empty security/update state. Live freshness is maintained even when storage downsamples snapshots. Fleet/host projections avoid transferring complete telemetry or arbitrary container labels.

The integrated GROWTH-001 agent hardening retains corruption recovery/vacuum, typed HTTP backoff and permanent-rejection handling, journal/ring bounds, payload guards and atomic identity persistence. Docker defaults follow this design: 50 stored lines and 16 KiB per container/pass, with a 16 KiB line limit; the offline buffer defaults to 50 MiB and is configurable up to the 64 MiB hard ceiling. Existing hardening tests exercise configurable maxima explicitly where they differ from the new defaults. Stored/live line normalization uses the same effective configured limit.

Log ingestion sanitizes NUL/control content, validates identities/streams, restamps out-of-window timestamps and charges actual masked bytes. Priority classification occurs before masking. Live and stored output share masking fixtures with the frontend. Compose source masking preserves original line layout and comments, resolves bounded aliases/merges, and fails closed on malformed input. File reads use trusted paths and Linux descriptor/no-follow checks.

## Validation

Baseline: the existing Go suite passed; frontend baseline was 66 files / 1,410 tests. Final frontend: 78 files / 1,481 tests passed. Production and static-demo builds passed; the production bundle was regenerated last. The existing bundle-size advisory remains.

Windows and Linux agent, collector and redaction suites passed. Agent/collector additions contain 26 story tests (including Linux-only file/FIFO safety tests); shared redaction has 13 test functions plus table cases. Server feature/storage PostgreSQL checks and lifecycle downgrade checks passed. A real WebSocket test verifies notification and closure at JWT expiry. Final full Go/PostgreSQL and static-check outputs are recorded in the [QA report](../qa/DOCKER-UX-001-qa-report.md).

Browser review covered fleet layout, canonical logs, opt-in Live, retained Down-stack controls, masked read-only Compose, and host-scoped cleanup preview/cancel against the demo. Cross-layer review fixed real-response identity normalization, backfill markers, self-stack confirmations, canonical Activity filters and stack service projections that demo-only testing had missed.

## Operational handoff

Nothing was deployed and no production database was changed. The [upgrade procedure](../ops/docker-upgrade.md) documents verified backup, read-only classification, per-database sign-off for reconciliation, and migration without serving. The local PostgreSQL tests use disposable synthetic schemas. A production database's backup/sign-off evidence cannot be substituted by these tests.

Residual validation limits: destructive Docker operations use deterministic fakes; a browser-to-managed-Docker end-to-end exercise and production-scale soak/low-disk load test were not performed. Stack observation is bounded but uses per-project transactions; batching may reduce overhead for very large fleets. These are validation/performance limits, not placeholders in the five phases.
