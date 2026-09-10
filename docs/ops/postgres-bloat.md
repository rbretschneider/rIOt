# Postgres bloat on high-churn retention tables

**Status:** interim fix shipped (migration `000023_autovacuum_tuning`); one-time
reclaim + long-term partitioning still open.

## Problem

rIOt's retention tables accumulate severe dead-tuple bloat. The retention purge
`DELETE`s old rows continuously, but Postgres's default autovacuum only fires
when roughly 20% of a table is dead (`autovacuum_vacuum_scale_factor = 0.2`). On
large, constantly-churning tables that threshold is rarely reached, so dead
tuples are never reclaimed and the table files grow unbounded. (Autovacuum, when
it does run, also never returns space to the OS — it only makes space reusable
inside the table.)

**This is not a retention-days problem.** Shortening retention does not help: a
`DELETE` of rows older than 14 days on the bloated table removed only ~200 rows
(≈844 MB of actual 14-day log data). The bloat is purely a vacuum/maintenance
concern.

## Evidence (Hawaii, 2026-09-10)

- `container_logs`: **28 GB** on disk with only **~844 MB** live data (~97% bloat).
- Whole DB **34 GB → 7.3 GB** after a manual `VACUUM FULL container_logs`.

## Affected tables

`container_logs`, `telemetry_snapshots`, `device_logs`, `container_metrics`,
`heartbeats`, `probe_results` — the continuously-inserted, retention-purged
tables.

## Fix 1 — per-table autovacuum tuning (shipped)

Migration `cmd/riot-server/migrations/000023_autovacuum_tuning.{up,down}.sql`
lowers the autovacuum trigger so it fires on a small absolute threshold instead
of 20% of the (large) table:

| Table | scale_factor | threshold |
|---|---|---|
| `container_logs` | 0.02 | 5000 |
| `telemetry_snapshots` | 0.02 | 5000 |
| `device_logs` | 0.02 | 5000 |
| `heartbeats` | 0.02 | 5000 |
| `container_metrics` | 0.05 | 5000 |
| `probe_results` | 0.05 | 5000 |

This keeps bloat at a low steady state going forward. It requires no application
or retention-days change and runs automatically on server startup like every
other migration. **It bounds growth; it does not shrink files already bloated.**

## Fix 2 — one-time reclaim of existing bloat (operational, pending)

Autovacuum will not shrink files that are already bloated. Reclaiming the
existing waste is a one-time operational step, not a migration:

- **`VACUUM FULL <table>`** — simplest, but takes an **ACCESS EXCLUSIVE lock**
  for the duration (table unavailable). Run in a maintenance window.
- **`pg_repack`** — reclaims with no long-held exclusive lock; preferred for
  live tables. Requires the `pg_repack` extension installed.

Done so far: `container_logs` (via `VACUUM FULL`). Still wants a reclaim:
`telemetry_snapshots` (~4.3 GB). Any Hawaii-side execution is deferred and
should follow `D:\Repos\Hawaii` runbooks + a maintenance window.

## Fix 3 — time-partitioning (proper long-term solution)

Convert the retention tables to time-partitioned tables
(`PARTITION BY RANGE (timestamp)`, daily or weekly) and implement retention by
**dropping old partitions** instead of `DELETE`:

- Dropping a partition is instant, returns space to the OS immediately, and
  generates zero bloat and zero vacuum load.
- `pg_partman` can automate partition creation + retention.

This eliminates the problem permanently and would let Fix 1's tuning be relaxed.
It is a schema change to the hottest tables (insert paths, retention job, and
queries all need to be partition-aware), so it warrants its own story through
the engineering pipeline rather than a drive-by migration.

## TL;DR

Default autovacuum is too lazy for these log/telemetry tables → tens of GB of
bloat. **Interim (shipped):** per-table autovacuum tuning. **Operational:** a
one-time `VACUUM FULL`/`pg_repack` to reclaim existing bloat. **Long-term:**
time-partition + drop-partition retention. No app or retention-days change is
needed for the interim fix.
