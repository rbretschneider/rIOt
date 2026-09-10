# Formal Requirements Document

- **Story ID:** PART-001
- **Title:** Time-Range Partitioning with Drop-Partition Retention for High-Churn Tables
- **Author:** Business Developer Agent
- **Date:** 2026-09-10
- **Status:** FINAL

---

## 1. Executive Summary

Convert rIOt's six high-churn retention tables — `container_logs`,
`telemetry_snapshots`, `device_logs`, `container_metrics`, `heartbeats`, and
`probe_results` — from ordinary tables purged by `DELETE` into tables
`PARTITION BY RANGE` on their timestamp column, and enforce retention by
**dropping whole partitions** that are older than each table's retention window
instead of deleting rows. A hand-rolled background partition manager in the Go
server pre-creates upcoming partitions so inserts never miss a partition, and
drops aged-out partitions to return disk space directly to the operating system.
The one-time conversion runs automatically on server startup through the normal
embedded-migration path, and — because the production database on the `Hawaii`
box is roughly 34 GB live — the conversion must be a fast, idempotent metadata
swap (rename the existing table, create the partitioned parent, attach the old
table as a bounded "legacy" partition) that never copies existing rows and never
holds a long exclusive lock. The result eliminates the dead-tuple bloat that
grew `container_logs` to 28 GB against ~844 MB of live data, and removes the
continuous autovacuum load caused by `DELETE`-based retention.

## 2. Background & Context

rIOt's retention tables accumulate severe dead-tuple bloat. The retention purge
`DELETE`s old rows continuously, but Postgres's default autovacuum only fires
when roughly 20% of a table is dead (`autovacuum_vacuum_scale_factor = 0.2`). On
large, constantly-churning tables that threshold is rarely reached, so dead
tuples are never reclaimed and the table files grow unbounded. Autovacuum, even
when it does run, never returns space to the OS — it only makes space reusable
inside the table. Shortening retention does not help: on the bloated
`container_logs` table, deleting rows older than 14 days removed only ~200 rows
(~844 MB of real 14-day data) against 28 GB on disk (~97% bloat). The whole
production database shrank from 34 GB to 7.3 GB after a manual
`VACUUM FULL container_logs`.

Migration `000023_autovacuum_tuning` (already shipped) lowered the per-table
autovacuum trigger to a small absolute threshold for these six tables. That is
an interim measure: it bounds future growth but does not shrink files already
bloated, and it leaves the continuous vacuum load in place.

The permanent fix — this story — is to convert the six tables to time-range
partitioning and implement retention by dropping old partitions. Dropping a
partition is instant, returns space to the OS immediately, and generates zero
dead tuples and zero vacuum load. The insert path, the retention job, and any
queries against these tables must all remain correct across the change. Because
this is a schema change to the hottest tables, it warrants its own pipeline
story rather than a drive-by migration (per `docs/ops/postgres-bloat.md`,
Fix 3).

The retention purge lives in `internal/server/server.go` (`runRetention`),
invoked hourly by `retentionWorker`. Migrations are embedded via `go:embed`
(`cmd/riot-server/main.go`) and applied automatically on startup by
`DB.RunMigrations` (`internal/server/db/db.go`) using golang-migrate. Retention
windows come from `internal/server/config.go`: `RIOT_RETENTION_DAYS`
(default 30) governs `telemetry_snapshots`, `device_logs`, and `probe_results`;
`heartbeats`, `container_logs`, and `container_metrics` are purged at a
hardcoded 7 days in `runRetention`. The `events` table (90 days) and other
non-churn tables keep their existing `DELETE` retention and are out of scope.

### User Stories

- **US-1:** As a homelab operator, I want retention to return disk space to the
  operating system when old data ages out, so a log/telemetry table cannot grow
  to tens of gigabytes of dead-tuple bloat and consume my host's disk.
- **US-2:** As a homelab operator running the ~34 GB `Hawaii` production
  database, I want the one-time conversion to be a fast metadata operation that
  cannot hang my server boot, half-convert a table, or take the monitoring
  platform offline while it rewrites 28 GB of data.
- **US-3:** As a rIOt developer, I want retention on these tables enforced by
  dropping partitions instead of deleting rows, so the platform stops generating
  the continuous dead-tuple churn and autovacuum load that `DELETE` retention
  causes.
- **US-4:** As a rIOt developer, I want the partition manager to pre-create
  upcoming partitions so a device pushing telemetry never hits a missing
  partition and never fails to insert.
- **US-5:** As a homelab operator, I want the retention windows I already
  configured to be honored unchanged after the migration, so the change is
  invisible to me except that bloat stops.
- **US-6:** As a rIOt developer, I want partition create/drop actions logged and
  failures surfaced, so I can see the retention system working and be alerted if
  it stops.

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| rIOt Server (migration path) | golang-migrate runner invoked on startup; performs the one-time metadata conversion of each table. | Full DDL on the rIOt database; runs automatically at boot before serving traffic. |
| rIOt Server (partition manager) | New background job in the server that pre-creates future partitions and drops aged-out ones. | Creates and drops partitions on the six in-scope tables; reads retention config. |
| rIOt Server (retention worker) | Existing hourly `runRetention` job. | Retains `DELETE` purge for out-of-scope tables only; no longer purges the six in-scope tables. |
| rIOt Agent | Go daemon pushing telemetry that is inserted into the partitioned tables. | Inserts rows only, via the existing ingest path; unaware of partitioning. |
| Operator | Homelab administrator running the server and configuring retention. | Sets `RIOT_RETENTION_DAYS`; observes logs; performs out-of-band bloat reclaim separately. |
| PostgreSQL | The database engine (PostgreSQL 16). | Routes inserts to partitions; enforces partitioned-table constraints. |

## 4. Functional Requirements

### Conversion — Schema (one-time, on-startup migration)

- **FR-001:** Each of the six in-scope tables (`container_logs`,
  `telemetry_snapshots`, `device_logs`, `container_metrics`, `heartbeats`,
  `probe_results`) must be converted to a `PARTITION BY RANGE` table keyed on its
  existing timestamp column. The range key is the `timestamp` column for
  `container_logs`, `telemetry_snapshots`, `device_logs`, `container_metrics`,
  and `heartbeats`; it is the `created_at` column for `probe_results` (that table
  has no `timestamp` column).
- **FR-002:** Each converted table's primary key must be changed to include the
  partition key column, as PostgreSQL requires for a partitioned table (the
  current `id BIGSERIAL` single-column primary key must become a composite key
  such as `(id, <partition-key>)`). The `id` column must remain unique in
  practice via the serial sequence; no application code may rely on a
  single-column `id` primary-key constraint after the change.
- **FR-003:** Every index that exists on each table today must exist on the
  partitioned parent after conversion so it propagates to all partitions:
  `idx_heartbeats_device_time`, `idx_telemetry_device_time`,
  `idx_device_logs_device_ts`, `idx_device_logs_priority`,
  `idx_container_metrics_lookup`, `idx_container_metrics_time`,
  `idx_container_logs_lookup`, `idx_container_logs_time`, and
  `idx_probe_results_probe_time`.
- **FR-004:** Existing outgoing foreign keys must continue to be enforced after
  conversion: `heartbeats`, `telemetry_snapshots`, `device_logs`, and
  `container_metrics` reference `devices(id) ON DELETE CASCADE`, and
  `probe_results` references `probes(id) ON DELETE CASCADE`. `container_logs`
  has no foreign key today and must not gain one.
- **FR-005:** The conversion must be performed as a metadata swap: rename the
  existing populated table to a legacy name, create the new partitioned parent
  in its place, and `ATTACH` the renamed legacy table as a bounded partition
  covering all pre-existing rows. The conversion must not copy or rewrite the
  existing row data.
- **FR-006:** The conversion must be idempotent and resumable: re-running it (a
  normal boot after a successful conversion, or a boot after a crash partway
  through) must converge to the fully converted state without error and without
  duplicating or losing data or partitions.
- **FR-007:** The conversion must complete cleanly on a fresh or empty database:
  when a table has no pre-existing rows, the partitioned parent is created with
  no (or an empty) legacy partition and no error occurs.
- **FR-008:** The conversion must run automatically on server startup through the
  existing embedded-migration path (`go:embed` migrations applied by
  `DB.RunMigrations`), requiring no manual operator step.
- **FR-009:** After conversion, all existing read and write queries against the
  six tables must return the same results and continue to function unchanged
  (inserts, the latest-snapshot and per-device/per-container/per-probe lookups,
  and any range scans must all remain correct through the partitioned parent).

### Partition Management (background job in Go)

- **FR-010:** A background partition manager in the server must pre-create
  upcoming time partitions for each of the six tables ahead of the time they are
  needed, on a periodic schedule and on startup.
- **FR-011:** At all times there must be at least one future partition available
  to receive new rows for each table, so an insert never fails because the target
  partition does not exist.
- **FR-012:** The partition manager must drop each partition whose entire time
  range is older than that table's retention window, and must not drop any
  partition that could still contain rows within the retention window.
- **FR-013:** The attached legacy partition (FR-005) holding pre-existing rows
  must be dropped by the same retention logic once its entire time range is older
  than the table's retention window; until then it must remain queryable and its
  rows retained.
- **FR-014:** The retention window used to drop partitions must be read from the
  existing configuration per table and must equal the window the current
  `DELETE` purge uses: 7 days for `heartbeats`, `container_logs`, and
  `container_metrics`; `RIOT_RETENTION_DAYS` (default 30) for
  `telemetry_snapshots`, `device_logs`, and `probe_results`. No new hardcoded
  retention duration may be introduced.
- **FR-015:** The partition manager must run on server startup and periodically
  thereafter, without requiring a server restart to create new partitions or drop
  aged-out ones.

### Retention Purge — Removal / Non-Duplication

- **FR-016:** The existing `DELETE`-based retention in `runRetention` for the six
  in-scope tables must be removed or replaced so that no row-level `DELETE` purge
  runs against them in addition to drop-partition retention (no double retention).
- **FR-017:** The `DELETE`-based retention for all out-of-scope tables must remain
  unchanged, including `events` (90 days), `notification_log` (90 days), server
  logs (7 days), device probe results, and stale bootstrap keys.

### Backward Compatibility & Downgrade

- **FR-018:** The server must start cleanly and serve traffic on both a fresh
  (empty) database and an already-populated legacy database, with the conversion
  and partition manager both succeeding in each case.
- **FR-019:** The down-migration behavior must be defined and documented. Because
  a partitioned table cannot be trivially reverted to a plain table without a
  data move, the limitation must be documented; the down migration must either
  perform a defined, safe reversal or explicitly document that the conversion is
  not automatically reversible.

### Observability

- **FR-020:** Every partition create and partition drop action (including the
  legacy-partition drop) must be logged via structured `log/slog` with at least
  the table name and the partition identifier or time range.
- **FR-021:** A failure in partition creation or dropping must be logged at error
  level and must not silently stop the partition manager; the manager must
  continue managing other tables and retry on its next cycle.

## 5. Non-Functional Requirements

- **NFR-001:** [Safety — bounded lock, no rewrite] The one-time conversion must
  not perform a full-table data copy or rewrite of any existing table, and must
  not hold an `ACCESS EXCLUSIVE` lock for longer than the brief metadata
  operations (rename, create parent, attach partition, constraint/index
  metadata). It must be safe to run against the ~34 GB / 28 GB-per-table
  production database without taking the platform offline for a data move or
  hanging the boot. The conversion time must be independent of the number of
  existing rows.
- **NFR-002:** [Availability — insert never fails] The pre-creation of future
  partitions must guarantee that, under normal operation (clock advancing at real
  time and the manager running on schedule), an insert can never fail because its
  target partition is missing. This guarantee must survive a server restart and a
  gap between manager runs (sufficient future partitions must exist to cover the
  worst-case interval between runs plus a safety margin).
- **NFR-003:** [Idempotency & resumability] The conversion and all
  partition-management operations must be idempotent and safe to repeat: a crash
  partway through conversion, or a re-run on an already-converted table, must
  never half-convert, duplicate, or lose data, and must converge to the correct
  state on the next boot or cycle.
- **NFR-004:** [Performance] Retention must be enforced by O(1) partition drops
  that return space to the OS immediately and generate zero dead tuples and zero
  vacuum load. Retention must not perform a table scan or row-level delete on the
  in-scope tables.
- **NFR-005:** [Observability] Partition create, drop, and failure events must be
  emitted as structured `slog` records suitable for operator review and alerting.
- **NFR-006:** [Reliability] A failure managing one table's partitions must not
  crash the server, block the ingest path, or stop partition management for the
  other five tables.

## 6. Business Rules

- **BR-001:** Partition management must be hand-rolled in Go. `pg_partman` and any
  other new PostgreSQL extension or new external dependency is prohibited for this
  story.
- **BR-002:** Retention for the six in-scope tables must be enforced by dropping
  whole partitions, not by `DELETE`.
- **BR-003:** The conversion must happen automatically on server startup via the
  normal migration path and must be a fast, idempotent metadata swap that is safe
  against the large populated production database (a production boot must not hang
  or half-convert).
- **BR-004:** Pre-existing (legacy) rows live in the attached legacy partition and
  age out under normal retention — they are dropped once the whole legacy
  partition is older than the retention window; new rows route into fresh time
  partitions.
- **BR-005:** Retention durations are unchanged by this story. Each table keeps
  the exact effective window it has today, sourced from existing configuration
  (7 days for `heartbeats`/`container_logs`/`container_metrics`;
  `RIOT_RETENTION_DAYS` for `telemetry_snapshots`/`device_logs`/`probe_results`).
- **BR-006:** The `events` table and all other non-retention tables are out of
  scope and keep their existing `DELETE` retention.
- **BR-007:** Reclaiming the pre-existing bloat (the one-time
  `VACUUM FULL` / `pg_repack` already documented in `docs/ops/postgres-bloat.md`)
  is a separate operational task and is not part of this story.
- **BR-008:** Because a partition can only be dropped once its entire time range
  is older than the retention window, effective retention is "at least the
  configured window": up to one additional partition interval of data may be
  retained beyond the exact window. This is an accepted behavioral change from
  row-exact `DELETE` retention.

## 7. Data Requirements

### Entities Involved

The six in-scope tables and their current shape (source of truth for the
conversion):

| Table | Range key column | Current PK | Indexes (must be preserved) | Outgoing FK | Retention window | Window source |
|-------|------------------|-----------|-----------------------------|-------------|------------------|---------------|
| `heartbeats` | `timestamp` | `id BIGSERIAL` | `idx_heartbeats_device_time (device_id, timestamp DESC)` | `device_id → devices(id) ON DELETE CASCADE` | 7 days | hardcoded 7 in `runRetention` |
| `telemetry_snapshots` | `timestamp` | `id BIGSERIAL` | `idx_telemetry_device_time (device_id, timestamp DESC)` | `device_id → devices(id) ON DELETE CASCADE` | `RIOT_RETENTION_DAYS` (default 30) | `Config.RetentionDays` |
| `device_logs` | `timestamp` | `id BIGSERIAL` | `idx_device_logs_device_ts (device_id, timestamp DESC)`, `idx_device_logs_priority (priority)` | `device_id → devices(id) ON DELETE CASCADE` | `RIOT_RETENTION_DAYS` (default 30) | `Config.RetentionDays` |
| `container_metrics` | `timestamp` | `id BIGSERIAL` | `idx_container_metrics_lookup (device_id, container_name, timestamp DESC)`, `idx_container_metrics_time (timestamp)` | `device_id → devices(id) ON DELETE CASCADE` | 7 days | hardcoded 7 in `runRetention` |
| `container_logs` | `timestamp` | `id BIGSERIAL` | `idx_container_logs_lookup (device_id, container_id, timestamp DESC)`, `idx_container_logs_time (timestamp)` | none | 7 days | hardcoded 7 in `runRetention` |
| `probe_results` | `created_at` | `id BIGSERIAL` | `idx_probe_results_probe_time (probe_id, created_at DESC)` | `probe_id → probes(id) ON DELETE CASCADE` | `RIOT_RETENTION_DAYS` (default 30) | `Config.RetentionDays` |

### Validation Rules

- The range key column per table is exactly as listed above; `probe_results`
  partitions on `created_at`, all others on `timestamp`. No table may be
  partitioned on the wrong column.
- Every partition's range must be a contiguous, non-overlapping half-open
  interval on the key column so every insertable timestamp maps to exactly one
  partition.
- The primary key of each converted table must contain the range key column
  (FR-002).
- A partition is eligible to drop only when its entire range upper bound is
  strictly older than `now - retention_window` for that table (FR-012, FR-013).

### State Transitions (per partition lifecycle)

| From | Event | To | Action |
|------|-------|----|--------|
| (none) | Manager pre-creates ahead of need | Future partition | `CREATE` partition covering an upcoming interval |
| Future partition | Wall clock enters its range | Active partition | Receives new inserts |
| Active partition | Wall clock passes its range | Historical partition | No new inserts; still queried and retained |
| Historical partition | Entire range older than retention window | Dropped | `DROP` partition; space returned to OS |
| Legacy partition (attached at conversion) | Entire range older than retention window | Dropped | `DROP` legacy partition (BR-004) |

### Conversion state (per table, one-time)

| From | Event | To | Action |
|------|-------|----|--------|
| Plain table (populated or empty) | Migration runs | Converting | Rename existing table to legacy name |
| Converting | Metadata swap | Partitioned | Create partitioned parent; attach legacy as bounded partition; recreate PK/indexes/FKs |
| Partitioned | Migration re-runs (reboot/crash-resume) | Partitioned | No-op / resume to converged state (idempotent, FR-006) |

## 8. Acceptance Criteria

### AC-001: Each table is range-partitioned on its timestamp column [Maps to FR-001]
- **Given** a database that has completed the PART-001 conversion
- **When** the schema of each of the six in-scope tables is inspected
- **Then** each table is a `PARTITION BY RANGE` table
- **And** it is partitioned on `timestamp` for `heartbeats`,
  `telemetry_snapshots`, `device_logs`, `container_metrics`, and
  `container_logs`

### AC-002: probe_results is partitioned on created_at [Maps to FR-001]
- **Given** the converted database
- **When** the `probe_results` table schema is inspected
- **Then** it is a `PARTITION BY RANGE` table partitioned on `created_at`
- **And** it is not partitioned on any `timestamp` column (it has none)

### AC-003: Primary key includes the partition key [Maps to FR-002]
- **Given** any of the six converted tables
- **When** its primary key is inspected
- **Then** the primary key includes the table's range key column
- **And** the `id` serial column still yields unique values on insert

### AC-004: All pre-existing indexes are preserved [Maps to FR-003]
- **Given** the converted tables
- **When** their indexes are inspected
- **Then** every index that existed before conversion exists on the partitioned
  parent (and therefore on its partitions)
- **And** the per-device / per-container / per-probe lookup indexes and the
  `device_logs` priority index are present

### AC-005: Foreign keys are preserved and container_logs stays FK-free [Maps to FR-004]
- **Given** the converted tables
- **When** their foreign keys are inspected
- **Then** `heartbeats`, `telemetry_snapshots`, `device_logs`, and
  `container_metrics` still enforce `device_id → devices(id) ON DELETE CASCADE`,
  and `probe_results` still enforces `probe_id → probes(id) ON DELETE CASCADE`
- **And** `container_logs` has no foreign key

### AC-006: Conversion is a metadata swap with no row copy on a large table [Maps to FR-005, NFR-001]
- **Given** a table populated with a large volume of pre-existing rows
  (representative of the 28 GB `container_logs` case)
- **When** the conversion migration runs
- **Then** the existing rows are not copied or rewritten
- **And** the pre-existing rows are present in an attached legacy partition after
  conversion
- **And** the migration does not hold an `ACCESS EXCLUSIVE` lock for a duration
  that scales with the number of rows

### AC-007: Conversion is idempotent and resumable [Maps to FR-006, NFR-003]
- **Given** a database where the conversion has already completed
- **When** the server restarts and the migration path runs again
- **Then** the conversion is a no-op and produces no error
- **And** re-running after a simulated crash partway through converges to the
  fully converted state without duplicating or losing data or partitions

### AC-008: Fresh/empty install converts cleanly [Maps to FR-007, FR-018]
- **Given** a fresh, empty database with no pre-existing rows in the six tables
- **When** the server starts and runs the conversion
- **Then** every in-scope table is created as a partitioned table with no error
- **And** the server starts and serves traffic normally

### AC-009: Existing queries continue to work unchanged [Maps to FR-009]
- **Given** the converted database receiving live telemetry
- **When** the existing ingest inserts and the existing read queries
  (latest-snapshot, per-device/per-container/per-probe lookups, range scans) run
- **Then** each returns the same results it would have before conversion
- **And** no query fails due to the tables now being partitioned

### AC-010: Future partitions are pre-created so inserts never miss [Maps to FR-010, FR-011, NFR-002]
- **Given** the partition manager has run
- **When** the current wall-clock time and a safety margin ahead are considered
- **Then** at least one future partition exists to receive new rows for every
  in-scope table
- **And** an insert with a current timestamp routes into an existing partition
  and does not fail
- **And** the future-partition coverage survives a manager restart and the
  interval between manager runs

### AC-011: Aged-out partitions are dropped, returning space [Maps to FR-012, NFR-004]
- **Given** a table with a partition whose entire range is older than that
  table's retention window
- **When** the partition manager runs
- **Then** that partition is dropped
- **And** no partition that could still contain rows within the retention window
  is dropped
- **And** retention performs no row-level `DELETE` or table scan on the table

### AC-012: The legacy partition ages out under normal retention [Maps to FR-013, BR-004]
- **Given** a converted table whose attached legacy partition's entire range is
  now older than the retention window
- **When** the partition manager runs
- **Then** the legacy partition is dropped
- **And** while any of the legacy partition's range is still within the window,
  the legacy partition is retained and remains queryable

### AC-013: Retention windows are read from existing config, per table [Maps to FR-014, BR-005]
- **Given** the running server with a configured `RIOT_RETENTION_DAYS`
- **When** the partition manager decides which partitions to drop
- **Then** it uses 7 days for `heartbeats`, `container_logs`, and
  `container_metrics`
- **And** it uses `RIOT_RETENTION_DAYS` for `telemetry_snapshots`,
  `device_logs`, and `probe_results`
- **And** no new hardcoded retention duration is introduced

### AC-014: Old DELETE retention is removed for the six tables [Maps to FR-016, BR-002]
- **Given** the running server after this story
- **When** the hourly retention worker executes
- **Then** it performs no row-level `DELETE` purge against any of the six
  in-scope tables
- **And** retention for those tables is enforced only by dropping partitions
  (no double retention)

### AC-015: Out-of-scope tables keep DELETE retention [Maps to FR-017, BR-006]
- **Given** the running server after this story
- **When** the hourly retention worker executes
- **Then** `events` (90 days), `notification_log` (90 days), server logs
  (7 days), device probe results, and stale bootstrap keys are still purged by
  their existing `DELETE`-based retention
- **And** none of those tables are partitioned by this story

### AC-016: Server starts on a populated legacy production-style database [Maps to FR-008, FR-018, BR-003]
- **Given** an already-populated legacy database resembling the ~34 GB
  production box
- **When** the server boots and the migration path runs the conversion
- **Then** the boot completes without hanging and the conversion succeeds as a
  metadata swap
- **And** the server proceeds to serve traffic

### AC-017: Partition create/drop/failure are observable [Maps to FR-020, FR-021, NFR-005, NFR-006]
- **Given** the partition manager running
- **When** it creates a partition, drops a partition, or a create/drop fails
- **Then** it emits a structured `slog` record identifying the table and the
  partition (or time range) for creates and drops
- **And** a failure is logged at error level, the manager continues managing the
  other tables, and it retries on its next cycle rather than stopping silently

### AC-018: Down-migration limitation is defined [Maps to FR-019]
- **Given** the PART-001 migration
- **When** its down migration is examined
- **Then** the down migration either performs a defined, safe reversal or
  explicitly documents that the conversion is not automatically reversible
- **And** the non-trivial nature of reverting a partitioned table to a plain
  table is documented

## 9. Out of Scope

- Partitioning the `events` table or any table other than the six named
  high-churn retention tables.
- Using `pg_partman` or introducing any new PostgreSQL extension or third-party
  dependency.
- Changing any retention **duration** (the windows are preserved exactly).
- Any online, zero-lock conversion approach beyond the metadata-swap described
  here.
- Reclaiming the pre-existing bloat (the one-time `VACUUM FULL` / `pg_repack`
  operational task in `docs/ops/postgres-bloat.md`).
- Relaxing or removing the interim autovacuum tuning from migration `000023`
  (may be revisited later, not in this story).
- Changing the ingest API, telemetry model, or agent behavior.
- Migrating away from `BIGSERIAL` `id` semantics beyond the composite-key change
  required by partitioning.

## 10. Assumptions

- **A-001:** The production PostgreSQL version (16) supports range partitioning,
  `ATTACH PARTITION`, partitioned-table primary keys that include the partition
  key, outgoing foreign keys from partitioned tables, and indexes defined on the
  partitioned parent that propagate to partitions.
- **A-002:** Attaching the renamed legacy table as a bounded partition is a
  metadata operation that does not rewrite its rows, so conversion time is
  independent of table size (basis for NFR-001).
- **A-003:** Partition granularity (e.g., daily vs. weekly), the exact naming
  scheme for partitions and the legacy partition, the pre-creation lead time and
  manager run cadence, the legacy partition's boundary selection, and the
  bookkeeping for idempotency/resumability are architect decisions; this FRD
  constrains behavior, not implementation naming.
- **A-004:** The single `RIOT_RETENTION_DAYS` value is the correct source for the
  three configurable tables, and the 7-day windows for
  `heartbeats`/`container_logs`/`container_metrics` are correct as they exist in
  `runRetention` today.
- **A-005:** Accepting "at least the configured window" retention (up to one
  extra partition interval of data, BR-008) is acceptable to the operator.
- **A-006:** The `container_logs` table intentionally has no foreign key today,
  and this story preserves that (it must not add one, FR-004).

## 11. Open Questions

None. The solution shape — hand-rolled Go partition management (no `pg_partman`),
automatic on-startup conversion via a fast idempotent metadata swap, and legacy
rows aging out in an attached legacy partition — was decided with the user and is
final. Remaining choices are delegated to the architect per Assumption A-003.

## 12. Dependencies

- **D-001:** Embedded-migration path — `go:embed` migrations in
  `cmd/riot-server/main.go` applied by `DB.RunMigrations`
  (`internal/server/db/db.go`) via golang-migrate on startup. The conversion is a
  new migration in `cmd/riot-server/migrations/`.
- **D-002:** Existing retention worker — `retentionWorker` / `runRetention`
  (`internal/server/server.go`), which must be amended to stop purging the six
  in-scope tables while retaining purge for the out-of-scope tables.
- **D-003:** Retention configuration — `Config.RetentionDays` /
  `RIOT_RETENTION_DAYS` (`internal/server/config.go`) and the hardcoded 7-day
  windows in `runRetention`.
- **D-004:** The repositories that read/write the six tables (`TelemetryRepo`,
  `ContainerLogRepo`, `ContainerMetricRepo`, `DeviceLogRepo`, `ProbeRepo`) whose
  queries must remain correct against the partitioned tables (FR-009).
- **D-005:** PostgreSQL 16 (companion container) with the partitioning features in
  A-001. No new extension is permitted (BR-001).
- **D-006:** The interim autovacuum-tuning migration `000023_autovacuum_tuning`
  and the problem write-up `docs/ops/postgres-bloat.md`, which this story
  supersedes as the permanent fix.
