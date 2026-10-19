# Postgres bloat on high-churn retention tables

**Status:** long-term fix shipped (PART-001, time-partitioning + drop-partition
retention); one-time reclaim of pre-existing bloat still open.

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

## Fix 1 — per-table autovacuum tuning (shipped, now interim/superseded)

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

**Superseded by Fix 3 for these six tables.** Since PART-001 (below), all six
tables are range-partitioned and retained by dropping whole day-partitions
instead of `DELETE`. A dropped partition generates zero dead tuples — there is
nothing for autovacuum to reclaim on the forward (post-conversion) data, so this
tuning is moot for it. These reloptions are **not** carried forward onto the new
partitioned parent or its daily partitions; they are left in place deliberately
because the **legacy partition** (the pre-conversion data, still attached as a
bounded partition until it fully ages out under retention) keeps them, since it
is the renamed original table and its reloptions travel with it. No migration
change is needed — the tuning simply stops doing anything once a table's legacy
partition is eventually dropped.

## Fix 2 — one-time reclaim of existing bloat (operational, still pending for some tables)

Neither autovacuum nor PART-001's partitioning (Fix 3, below) shrinks files that
were **already** bloated before the conversion — that bloat lives on inside the
**legacy partition** (the renamed original table, attached as a bounded
partition covering all pre-conversion rows) until the legacy partition's entire
date range ages past the retention window and it is dropped whole. Until then,
reclaiming existing waste is still a one-time operational step, not a migration:

- **`VACUUM FULL <table>`** — simplest, but takes an **ACCESS EXCLUSIVE lock**
  for the duration (table unavailable). Run in a maintenance window. After
  PART-001, run this against `<table>_legacy`, not `<table>` (the partitioned
  parent holds no rows itself).
- **`pg_repack`** — reclaims with no long-held exclusive lock; preferred for
  live tables. Requires the `pg_repack` extension installed.

Done so far: `container_logs` (via `VACUUM FULL`, pre-PART-001). Still wants a
reclaim: `telemetry_snapshots` (~4.3 GB). Any Hawaii-side execution is deferred
and should follow `D:\Repos\Hawaii` runbooks + a maintenance window. Once a
table's legacy partition ages out and is dropped under normal retention, its
pre-existing bloat is reclaimed automatically and this step becomes moot for
that table.

## Fix 3 — time-partitioning (shipped, PART-001)

The six retention tables (`container_logs`, `telemetry_snapshots`,
`device_logs`, `container_metrics`, `heartbeats`, `probe_results`) are
range-partitioned by day (`PARTITION BY RANGE` on `timestamp`, or `created_at`
for `probe_results`), and retention is enforced by **dropping** each day's
partition once it ages past the table's retention window, instead of `DELETE`.
This is the mechanism now live:

- **Conversion.** On first boot after upgrading, each table is converted
  automatically via `cmd/riot-server/migrations/000024`–`000028`, before the
  server starts serving traffic. The conversion is a metadata swap (rename +
  attach), not a data copy or rewrite — safe to run against a large production
  database without a maintenance window, though it does briefly hold an
  exclusive lock per table during the swap itself.
- **Retention.** A hand-rolled Go partition manager
  (`internal/server/partition/`) pre-creates daily partitions three days ahead
  and drops any partition — including the legacy partition once it fully ages
  out — whose entire date range is older than the table's retention window.
  Dropping a partition is instant, returns space to the OS immediately, and
  generates zero dead tuples and zero vacuum load, eliminating the problem this
  document describes at the source. No `pg_partman` or other extension is used
  — this is hand-rolled per rIOt's dependency policy.
- **Clock-skew safety.** Every partitioned table also has a near-empty forward
  `DEFAULT` catch partition, so a device with a skewed clock pushing a
  future-dated row cannot fail an insert batch — the row lands in the catch
  partition instead, and a persistently non-zero count there is surfaced via a
  `WARN` log and the manager's health snapshot.
- **What it does not do.** Partitioning does not retroactively shrink bloat
  that already existed before conversion (Fix 2, above) — that bloat rides
  along in the legacy partition until it ages out — and does not change any
  retention duration (`RIOT_RETENTION_DAYS` and the hardcoded 7-day windows are
  unchanged).

See [`README.md#partitioned-tables-and-drop-based-retention`](../../README.md#partitioned-tables-and-drop-based-retention)
for the operator-facing summary (boot-time pause, one-way conversion).

## TL;DR

Default autovacuum was too lazy for these log/telemetry tables → tens of GB of
bloat. **Interim (shipped, now superseded for these six tables):** per-table
autovacuum tuning. **Long-term (shipped, PART-001):** the six tables are
range-partitioned and retained by dropping whole day-partitions instead of
`DELETE` — this is now the live mechanism, requires no app or retention-days
change, and eliminates the bloat problem going forward. **Still operational and
pending:** a one-time `VACUUM FULL`/`pg_repack` to reclaim bloat that already
existed before conversion (rides along in each table's legacy partition until
it ages out on its own).
