# Architecture Decision Document

- **Story ID:** PART-001
- **FRD Reference:** `docs/requirements/PART-001-frd.md`
- **Author:** Architect Agent
- **Date:** 2026-09-10
- **Status:** DRAFT (revised after security review `docs/security/PART-001-security-review.md`, verdict REVISE)

> **Revision note (security pass).** This revision resolves the two HIGH findings
> and all MEDIUM/LOW findings from the PART-001 security review:
> - **SEC-PART-001-001** — `cutoff` is now **derived from the data**
>   (`> max(existing key)`, day-aligned), not a fixed `now+2d`; the persisted-cutoff
>   recovery bug is fixed by a self-healing recompute in `000024`; the
>   VALIDATE-cannot-fail rationale is stated (AD-001, AD-003, §5, §9, §12).
> - **SEC-PART-001-002** — NFR-002 is now a **genuine guarantee** via a near-empty
>   **forward DEFAULT catch partition** (AD-008), so no future-dated row can fail a
>   `CopyFrom`/staged-`INSERT` batch (AD-001, AD-005, AD-008, §5, §9, §11).
> - **SEC-PART-001-003** — the planner starts create-ahead **at `cutoff`** and
>   overlap/DEFAULT-capture errors are classified **benign (WARN, not ERROR)**, so a
>   clean conversion cannot trip `newServerErrorAlertWorker` (AD-005, §5, §9).
> - **SEC-PART-001-004** — the O(1) swap is a **hard, tested DoD gate**: exact index
>   definitions per table are specified and a no-relfilenode-change / lock-capture
>   test gates the swap (AD-004, §5, §8 AC-006/AC-019, §11, §13).
> - **SEC-PART-001-005** — the manager exposes a **health surface** (last run, last
>   success, last error, forward coverage, DEFAULT row counts) and escalates a
>   persistent stall loudly (AD-005, §8 AC-020, §9).
> - **SEC-PART-001-006** — dynamic DDL uses `format('%I', …)` for identifiers (§10, §12).
> - **SEC-PART-001-007** — the `000023` autovacuum reloptions are deliberately **not**
>   carried onto forward partitions (decision documented, AD-009, §11).
>
> The Section 4 file list is **unchanged** — every mitigation lands inside files
> already listed (the DEFAULT partition and the reworked `cutoff` live in
> `000027`/`000024`; the health surface and benign-error classification live in
> `manager.go`; the planner change lives in `plan.go`). No new file, no new
> dependency, and **no ingest-path repo change** (the DEFAULT catch is chosen over
> ingest-time clamping precisely to keep the insert path untouched).

---

## 1. Summary

Convert the six high-churn retention tables to `PARTITION BY RANGE` on their
timestamp column and replace their `DELETE` purge with whole-partition drops.
The one-time conversion is a **metadata swap** — rename the populated table to
`<t>_legacy`, create a partitioned parent in its place, and `ATTACH` the legacy
table as a **bounded** partition `[MINVALUE, cutoff)` where `cutoff` is derived
from the table's own `max(key)` so the bound is provably satisfiable — split
across **five sequential golang-migrate SQL files** so that the two unavoidable
O(rows) steps (the composite-PK index build and the partition-bound validation
scan) run under **non-blocking** locks and the `ACCESS EXCLUSIVE` swap itself is
O(1) metadata (NFR-001). Each parent additionally carries a **near-empty forward
DEFAULT catch partition** so an out-of-range (e.g. clock-skewed) row can never
fail an insert batch (NFR-002, genuine). A new hand-rolled Go partition manager
(`internal/server/partition/`) pre-creates daily partitions from `cutoff` forward
to three days ahead and drops partitions — including the legacy partition — once
their entire range is older than each table's retention window; the six `DELETE`
calls are removed from `runRetention`. No `pg_partman`, no new extension, no new
dependency, no ingest-path code change.

---

## 2. Technical Context

Current state on `main`, verified against the real code:

- **Migration path.** `cmd/riot-server/main.go` embeds `//go:embed all:migrations`;
  `internal/server/server.go:88-93` calls `DB.RunMigrations` (`internal/server/db/db.go:50-64`)
  **before** the repos are constructed, before any worker starts, and before the
  router/`http.Server` is built (`server.go:195+`) — i.e. **before any traffic is
  served and before any insert can occur**. This ordering is load-bearing for two
  guarantees in this ADD: (a) the conversion's O(rows) prep steps block nothing the
  platform serves, and (b) **no concurrent writer can race the `cutoff`
  compute → `VALIDATE` window** (AD-003). `RunMigrations` uses golang-migrate over
  an `iofs` source and runs `m.Up()`. Highest existing migration is
  `000023_autovacuum_tuning` (the shipped interim bloat fix). Next number is **000024**.
- **golang-migrate transaction behavior (critical).** The postgres driver sends
  each migration **file** to the server as one `Exec`. A multi-statement file is
  therefore executed as a **single implicit transaction** — every lock a
  statement takes is held until the file commits. `CREATE INDEX CONCURRENTLY`
  and other non-transactional DDL cannot run this way. This is the constraint
  that forces the conversion to be **split across multiple files** (AD-002).
- **The six tables** (`000001_init`, `000003_device_logs`, `000006_container_metrics`,
  `000015_container_logs`). All have `id BIGSERIAL PRIMARY KEY` and a **NOT NULL**
  range-key column (`timestamp`, or `created_at` for `probe_results`). Indexes,
  FKs and windows are exactly as tabulated in FRD §7. `device_logs` additionally
  carries a **UNIQUE** dedup index `idx_device_logs_dedup (device_id, timestamp,
  priority, unit, md5(message))` from `000005` — its `INSERT … ON CONFLICT`
  dedup path (`device_log_repo.go:72-77`) depends on it.
- **Agent-supplied timestamps (drives SEC-001/002).** `device_logs.timestamp`,
  `container_logs.timestamp`, `container_metrics.timestamp` are copied straight
  from the agent payload, **not** `DEFAULT now()` server stamps
  (`container_metric_repo.go:27` copies `m.Timestamp`; `device_log_repo.go:61`
  copies `e.Timestamp`; `container_log_repo.go:41` copies `e.Timestamp`). A device
  with a bad clock can therefore write rows dated arbitrarily in the future. The
  conversion bound (`cutoff`) and the forward-partition window must both tolerate
  this. This is why `cutoff` is derived from `max(key)` (AD-003) and why a forward
  DEFAULT catch exists (AD-008).
- **Ingest batch atomicity (drives SEC-002).** `container_logs`/`container_metrics`
  ingest via a single `pgx.CopyFrom` (`container_log_repo.go:43`,
  `container_metric_repo.go:29`); `device_logs` ingests via `CopyFrom` into a
  per-connection `TEMP` stage then **one** `INSERT … SELECT … ON CONFLICT`
  (`device_log_repo.go:63-77`). In all three, a **single** out-of-range row fails
  the **entire batch**, not one row — so "insert never fails on a missing
  partition" (NFR-002) must be a hard guarantee, not a probabilistic one.
- **Retention.** `internal/server/server.go:445-524` `runRetention` runs hourly
  (`retentionWorker`, ticker `1h`, started at `server.go:182`). It issues **eleven**
  purge calls; **six** target in-scope tables and must be removed:
  `TelemetryRepo.PurgeHeartbeats` (7d), `TelemetryRepo.PurgeSnapshots`
  (`RetentionDays`), `ProbeRepo.PurgeResults` (`RetentionDays`),
  `ContainerLogRepo.Purge` (7d), `ContainerMetricRepo.Purge` (7d),
  `DeviceLogRepo.Purge` (`RetentionDays`). The other five stay: `EventRepo.Purge`
  (90d), `NotifyRepo.PurgeNotificationLog` (90d), `LogRepo.Purge` (7d, server
  logs), **`DeviceProbeRepo.PurgeResults`** (`RetentionDays` — a *different*
  table from `probe_results`, out of scope), `CARepo.PurgeStaleBootstrapKeys`.
- **Server-error-rate alert worker (drives SEC-003).** `server.go:189` starts
  `newServerErrorAlertWorker`, which "notifies the user when the server itself
  starts logging too many errors in a short window." A predictable post-upgrade
  burst of `slog.Error` from the partition manager would page the operator on a
  clean conversion; the manager must therefore not log benign, expected conditions
  at ERROR (AD-005).
- **Config.** `internal/server/config.go` `Config.RetentionDays` (env
  `RIOT_RETENTION_DAYS`, default 30). The 7-day windows are hardcoded literals
  in `runRetention`. No new config is introduced (FR-014).
- **Insert/read paths (verified — no change required).** `heartbeats`,
  `telemetry_snapshots` use plain `INSERT`; `probe_results` uses `INSERT … RETURNING id`;
  `container_logs`/`container_metrics` use `pgx.CopyFrom`; `device_logs` uses a
  temp-stage + `INSERT … SELECT … ON CONFLICT`. **All of these work unchanged on
  a partitioned parent** (PG 11+ routes `INSERT`, `COPY`, `RETURNING`, and
  `ON CONFLICT` whose arbiter index includes the partition key). Reads (`List`,
  `GetLatestSnapshot`, `GetHistory`) query the parent and are transparently
  partition-pruned. This is why the composite-PK change touches **schema only**.
- **Server struct / wiring.** `Server` (`server.go:32-68`) holds `*db.DB`, all
  repos, and background workers started from `Start()`. A new partition manager
  is wired here.

### What is missing

- No partitioning; all six tables are plain heaps purged by `DELETE`.
- No partition manager, no create-ahead, no drop-partition retention.
- No composite primary keys (partitioning requires the key in every unique/PK).

---

## 3. Architecture Decisions

### AD-001: Bounded `NOT VALID` → `VALIDATE` → `ATTACH` legacy partition, plus a forward DEFAULT catch — not the legacy blob in a DEFAULT

**Decision.** Attach the renamed legacy table as an explicitly **bounded**
partition `FOR VALUES FROM (MINVALUE) TO (<cutoff>)`. To make the `ATTACH` a
metadata-only operation (no scan under `ACCESS EXCLUSIVE`), a matching
`CHECK (<key> < <cutoff>)` constraint is added `NOT VALID` and then `VALIDATE`d
on the still-live original table **before** the swap. When `ATTACH` runs, the
constraint is already `VALID`, so PostgreSQL proves the partition bound is
satisfied from the catalog and **skips the row scan**. **Separately**, each parent
carries a **near-empty forward DEFAULT catch partition** (`<t>_default`, AD-008)
so any row not covered by a bounded partition still has a home.

**Rationale / the two DEFAULT cases are different (resolves SEC-002's critique of
the prior blanket rejection).** The prior ADD rejected *all* DEFAULT partitions by
conflating two unrelated costs:

1. **The 28 GB *legacy* blob in a DEFAULT — correctly rejected.** A non-empty
   DEFAULT is rescanned every time a new bounded partition is attached/created, so
   putting the legacy blob in a DEFAULT would rescan 28 GB on *every daily create,
   forever*. The legacy data therefore stays a **bounded** partition
   `[MINVALUE, cutoff)`, which is **never** scanned on a subsequent partition
   create. This half of the original rationale stands.
2. **A near-empty *forward* DEFAULT catch — now adopted (AD-008).** The scan PG
   performs on the DEFAULT when creating a new daily partition is O(rows-in-DEFAULT).
   Because this DEFAULT only ever holds the rare future/gap row (clock-skewed
   devices), that scan is negligible — nothing like the legacy blob. It buys a
   genuine NFR-002 guarantee for a trivial steady-state cost. See AD-008 and §11
   for the create-interaction analysis.

Both partitions coexist on the same parent: legacy owns `(-∞, cutoff)`, daily
partitions own `[cutoff, furthest_created)`, and the DEFAULT catches
`[furthest_created, +∞)` and any gap.

**Honest NFR-001 reading.** The `ACCESS EXCLUSIVE` operations (rename, create
parent, `ATTACH`, create empty DEFAULT) are all O(1) metadata and satisfy "no long
`ACCESS EXCLUSIVE` lock / no rewrite / never offline for a data move." The
**one-time** validation scan and the composite-PK index build are O(rows), but they
run under `SHARE UPDATE EXCLUSIVE` / `SHARE` (never `ACCESS EXCLUSIVE`), perform
**no table rewrite**, and — because they execute during the boot migration before
the server serves — block nothing the platform depends on. FRD assumption A-002
("conversion time independent of table size") holds for the *swap*; it is imprecise
for the *validation/index-build prep*, which is O(rows)-but-non-blocking. This is
called out rather than papered over. It is still vastly cheaper than the
`VACUUM FULL` rewrite it replaces (a read pass + index build vs. a full heap copy).

**Alternatives considered.** Legacy blob in a DEFAULT (rejected: recurring 28 GB
rescans); `ATTACH` without a pre-validated `CHECK` (rejected: forces a scan under
`ACCESS EXCLUSIVE` = the NFR-001 violation); ingest-time timestamp clamping instead
of a DEFAULT (viable per the security review, but rejected here because it mutates
agent data and touches the `CopyFrom` hot path — the DEFAULT keeps the insert path
byte-for-byte unchanged, AD-008).

---

### AD-002: Conversion is five sequential migration files, not one

**Decision.** Because golang-migrate runs each file as one implicit transaction
and holds every lock until the file commits, a single-file conversion would hold
the `ACCESS EXCLUSIVE` lock taken by `RENAME`/`ADD CONSTRAINT` for the *entire*
duration of the O(rows) validation scan in the same transaction — an
`ACCESS EXCLUSIVE` lock scaling with row count, which is precisely what NFR-001
forbids. The conversion is therefore split so the O(rows) steps commit in their
own transactions under weak locks **before** the brief-`ACCESS EXCLUSIVE` swap:

| # | File | Work | Heaviest lock | Cost |
|---|------|------|---------------|------|
| 1 | `000024_partition_bookkeeping` | create `partition_conversion`; **derive** `cutoff` from `max(key)` and persist (self-healing); `ADD CONSTRAINT <t>_pbound CHECK (<key> < <cutoff>) NOT VALID` | `ACCESS EXCLUSIVE` (catalog only) | O(1)\* |
| 2 | `000025_partition_pk_index` | `CREATE UNIQUE INDEX <t>_pk_new ON <t> (id, <key>)` | `SHARE` (blocks writes, not reads) | O(rows) |
| 3 | `000026_partition_validate_bound` | `VALIDATE CONSTRAINT <t>_pbound` | `SHARE UPDATE EXCLUSIVE` (non-blocking) | O(rows) |
| 4 | `000027_partition_swap` | rename → create parent → attach legacy bounded → **attach forward DEFAULT** → adopt indexes → initial forward partitions → `ADD FK … NOT VALID` | `ACCESS EXCLUSIVE` (metadata only) | O(1) |
| 5 | `000028_partition_validate_fk` | `VALIDATE CONSTRAINT <t>_fk` | `SHARE UPDATE EXCLUSIVE` (non-blocking) | O(rows) |

\* File 1 runs one cheap `SELECT max(<key>)` per table to derive `cutoff`. On an
indexed/normal heap this is a fast aggregate (an index-backed or single seq-agg
scan); it is not a rewrite and takes only the catalog `ACCESS EXCLUSIVE` for the
`ADD CONSTRAINT`, released at file commit. It is O(1) in the NFR-001 sense (no
rewrite, no long AEL); the `max()` read itself is O(rows) but trivial relative to
files 2–3 and holds no exclusive lock during the read.

Files 2, 3 and 5 are O(rows) but never take `ACCESS EXCLUSIVE`. File 4 is the
only `ACCESS EXCLUSIVE` step and is O(1) because every scan-triggering condition
(bound validity, PK/lookup index existence — AD-004) was satisfied in files 1–3.
`CREATE UNIQUE INDEX` is intentionally **non-`CONCURRENTLY`**: `CONCURRENTLY`
cannot run inside golang-migrate's implicit transaction, and it is unnecessary
here because migrations run before the server serves traffic — a `SHARE` lock
(reads allowed, writes blocked) during a pre-serving boot blocks nothing. This is
documented as the deliberate trade (single-instance homelab; a concurrent
old-instance writer during a rolling deploy would merely queue for the build
duration — see §11).

**Rationale.** Keeps the whole conversion inside the mandated embedded-migration
path (FR-008, D-001) while honoring NFR-001's `ACCESS EXCLUSIVE` bound. No new
Go migration framework, no source-driver change.

**Alternatives considered.** Single monolithic migration file (rejected:
`ACCESS EXCLUSIVE` held for the scan). A Go-coded conversion invoked after
`RunMigrations` with `CONCURRENTLY` (rejected: departs from D-001's "new
migration in `cmd/riot-server/migrations/`"; the split-file SQL approach meets
every NFR without leaving the migration path).

---

### AD-003: `cutoff` is **derived from the data** (`> max(existing key)`), persisted, and self-healing across a dirty re-run

**Decision (resolves SEC-PART-001-001).** File 1 creates:

```sql
CREATE TABLE IF NOT EXISTS partition_conversion (
    table_name  TEXT PRIMARY KEY,
    key_column  TEXT NOT NULL,
    cutoff      TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

For each in-scope table **not yet partitioned**, `cutoff` is computed as

```sql
cutoff = date_trunc('day', GREATEST(now(), (SELECT max(<key>) FROM <t>))) + INTERVAL '1 day'
```

and persisted. This makes the FRD §7 invariant **`cutoff > max(existing <key>)`**
hold **by construction**, day-aligned into the future:

- `GREATEST(now(), max(<key>))` picks the later of wall-clock now and the newest
  row. `date_trunc('day', …)` floors it to that day's 00:00Z; `+ INTERVAL '1 day'`
  advances to the **start of the next day**, which is strictly greater than any
  timestamp within (or before) that day — including a `max(<key>)` that sits
  exactly on a day boundary (`date_trunc` is a no-op there, `+1 day` still exceeds it).
- **Empty / fresh-install table** (FR-007): `max(<key>)` is `NULL`; PostgreSQL
  `GREATEST` ignores `NULL` arguments, so `GREATEST(now(), NULL) = now()` and
  `cutoff` falls back to the now-based value. No special-case branch needed.
- **Future-skewed rows** (the SEC-001 failure driver): a device clock set months
  ahead simply raises `max(<key>)`, so `cutoff` lands one day past that row. The
  legacy partition's bound stretches to cover it and **`VALIDATE` cannot fail**.

**Why `VALIDATE` cannot fail (state explicitly).** `VALIDATE CONSTRAINT <t>_pbound`
in file 3 scans the legacy heap for any row with `<key> >= cutoff`. Because
`cutoff` was derived in file 1 as `> max(<key>)`, and **`RunMigrations` runs before
`Server.Start` serves traffic (`server.go:88-93` precedes the router/worker startup
at `server.go:182-195`)**, **no new row can be inserted between the compute in file 1
and the validate in file 3** — there is no serving path and no ingest handler yet.
The maximum key the validate can observe is exactly the one file 1 measured; the
bound is therefore provably satisfiable and `VALIDATE` is a formality. (Even the
day-boundary case is covered: `+1 day` is strictly greater.)

**Self-healing persist — fixes the sticky-cutoff recovery bug.** The prior ADD used
`INSERT … ON CONFLICT DO NOTHING`, which pinned a stale/bad `cutoff` across a
re-run and made the documented recovery ineffective. File 1 is now a guarded `DO`
block per table:

1. **If the table is already partitioned** (`pg_partitioned_table`) → **no-op**.
   The legacy partition's bound is already baked; the persisted row is now only the
   manager's historical record and must not be disturbed.
2. **Otherwise** (fresh install *or* a dirty pre-swap re-run) → recompute `cutoff`
   from the **current live** `max(<key>)`, `INSERT … ON CONFLICT (table_name) DO
   UPDATE SET cutoff = EXCLUDED.cutoff, key_column = EXCLUDED.key_column`
   (**not** `DO NOTHING`), then `DROP CONSTRAINT IF EXISTS <t>_pbound` and re-`ADD`
   it `NOT VALID` with the freshly-computed literal via `format('… CHECK (%I < %L) NOT VALID', key, cutoff)`.

Because golang-migrate only re-executes the file at the **dirty version**, file 1
re-runs only if it (or a prior boot's file 1) rolled back — in which case the
`DO NOTHING` bug could otherwise strand a stale row. The `DO UPDATE` + constraint
re-add guarantees a pre-swap re-run always recomputes from live data and always
leaves the `_pbound` literal matching the persisted `cutoff` that file 3 validates
and file 4 attaches under. There is no operator SQL literal to hand-edit any more.

**Consequence.** The bookkeeping table is small and permanent; it is the single
source of truth for the legacy bound and doubles as the idempotency ledger.

**Alternatives considered.** Fixed `now + 2 days` (rejected: SEC-001 — not derived
from data, fails `VALIDATE` on a single future-dated row, and the recovery was
broken). Computing `cutoff` only in file 4 at attach time (rejected: file 1's
`CHECK` and file 3's `VALIDATE` need the identical value; persisting it in file 1
keeps all three phases consistent while the self-healing `DO UPDATE` still lets a
dirty re-run refresh it).

---

### AD-004: Composite primary key `(id, <key>)`; parent-defined indexes adopt the legacy indexes byte-for-byte

**Decision.** Each parent is created with `PRIMARY KEY (id, <key>)` (FR-002).
`id` keeps its `BIGSERIAL` sequence default, so it remains unique-in-practice on
insert (AC-003); no code depends on a single-column PK constraint (verified §2).
The parent's PK adopts the unique index pre-built in file 2 (`<t>_pk_new` on
`(id, <key>)`, both columns NOT NULL) at `ATTACH` time — **no rebuild**. Every
FR-003 lookup index is created on the **parent** after attach; because an
**exactly-matching** index already exists on the legacy partition (it carried over
with the rename), PG marks the existing child index as the parent index's child
instead of rebuilding it. The `device_logs` dedup **UNIQUE** index is recreated on
the parent identically — its key already includes the partition column `timestamp`,
satisfying the "unique keys must contain the partition key" rule, so the
`ON CONFLICT` dedup path keeps working (FR-009).

**Exact index definitions to match (SEC-PART-001-004 — hard requirement, not a
note).** Adoption is O(1) **only if** the parent index definition byte-matches the
legacy child (column order, `DESC`, opclass, expression). A mismatch forces an
O(rows) rebuild **under the RENAME's held `ACCESS EXCLUSIVE` lock in the same
transaction** — the exact NFR-001 violation the split exists to prevent. The parent
indexes therefore **must** be created with these definitions (from FRD §7):

| Table | Parent index DDL (must byte-match the legacy child) |
|-------|------------------------------------------------------|
| `heartbeats` | `CREATE INDEX idx_heartbeats_device_time ON heartbeats (device_id, timestamp DESC)` |
| `telemetry_snapshots` | `CREATE INDEX idx_telemetry_device_time ON telemetry_snapshots (device_id, timestamp DESC)` |
| `device_logs` | `CREATE INDEX idx_device_logs_device_ts ON device_logs (device_id, timestamp DESC)`; `CREATE INDEX idx_device_logs_priority ON device_logs (priority)`; `CREATE UNIQUE INDEX idx_device_logs_dedup ON device_logs (device_id, timestamp, priority, unit, md5(message))` |
| `container_metrics` | `CREATE INDEX idx_container_metrics_lookup ON container_metrics (device_id, container_name, timestamp DESC)`; `CREATE INDEX idx_container_metrics_time ON container_metrics (timestamp)` |
| `container_logs` | `CREATE INDEX idx_container_logs_lookup ON container_logs (device_id, container_id, timestamp DESC)`; `CREATE INDEX idx_container_logs_time ON container_logs (timestamp)` |
| `probe_results` | `CREATE INDEX idx_probe_results_probe_time ON probe_results (probe_id, created_at DESC)` |

This is enforced as a **hard DoD/test gate** (§8 AC-019, §13): after `000027` a test
captures each legacy child index's `relfilenode` (and the legacy heap's) before and
after the swap and asserts **none changed** (no rebuild/rewrite), and captures
`pg_locks` to assert the `ACCESS EXCLUSIVE` hold time is **row-count-independent**
(compare a tiny fixture against a large one). If any relfilenode changes, the swap
is not O(1) and the gate fails the build.

**FK handling.** Outgoing FKs (`device_id → devices`, `probe_id → probes`, both
`ON DELETE CASCADE`) are defined on the parent in file 4 as `NOT VALID` (O(1)
catalog) and `VALIDATE`d in file 5 (non-blocking). `container_logs` gets **no**
FK (FR-004, A-006). A `NOT VALID` FK still enforces on all new rows immediately;
the legacy rows were already valid under the identical pre-conversion FK, so the
validate is a formality that runs off the `ACCESS EXCLUSIVE` path.

**Sequence continuity.** `RENAME` does not rename an owned sequence, so
`<t>_id_seq` survives. File 4 re-points it: `ALTER SEQUENCE <t>_id_seq OWNED BY <t>.id`
and sets the parent column default to `nextval('<t>_id_seq')`, so `id` generation
continues monotonically across the swap.

---

### AD-005: Hand-rolled Go partition manager in `internal/server/partition/`, daily partitions, cutoff-aware create-ahead, benign-error tolerance, health surface

**Decision.** New package `internal/server/partition` with a **pure** planning
layer (`plan.go`, unit-tested with no DB) and a thin DDL executor + scheduler
(`manager.go`). No `pg_partman`, no new dependency (BR-001).

- **Granularity: daily**, uniform across all six tables. Justification: the 7-day
  windows demand sub-weekly granularity — weekly partitions would retain up to
  ~2 weeks on a 7-day table (BR-008 allows one extra interval), nearly doubling
  it. Daily bounds over-retention to ≤1 day (≤14 % on a 7-day table, ≤3 % on a
  30-day table). Partition counts stay in the low hundreds total, well within PG's
  comfortable range for a homelab. One interval simplifies the manager to a single
  code path.
- **Naming:** `<table>_p<YYYYMMDD>` where the date is the partition's UTC start
  day (its range is `[day 00:00Z, day+1 00:00Z)`). Legacy partition:
  `<table>_legacy`. Forward catch: `<table>_default`. The date encodes the range,
  so drop-eligibility is computed from the name without reading `relpartbound`.
- **Create-ahead starts at `cutoff` (resolves SEC-PART-001-003).** The planner
  emits day-starts in `[max(now - behindDays, cutoffDay) .. now + aheadDays]` where
  `cutoffDay = date_trunc('day', cutoff)` (already day-aligned, AD-003). It **never
  emits a day whose entire range is below `cutoff`**, so no `CREATE … PARTITION OF`
  ever overlaps the legacy partition `[MINVALUE, cutoff)`. This eliminates the
  ~18-ERROR-per-cycle overlap storm the prior design produced for the first ~2 days
  after conversion, which could otherwise trip `newServerErrorAlertWorker`
  (`server.go:189`). The manager reads `cutoff` per table from
  `partition_conversion`. `behindDays = 1`, `aheadDays = 3`.
- **NFR-002 guarantee.** The forward DEFAULT catch (AD-008) means **every**
  insertable row already has a home before the manager runs; create-ahead is a
  *retention-granularity optimization* (giving days their own droppable partition),
  not a correctness prerequisite. At startup `EnsureAll` still runs **synchronously
  before the server serves traffic** so the common case gets fine-grained
  partitions immediately. `CREATE TABLE IF NOT EXISTS … PARTITION OF` makes creation
  idempotent.
- **Benign-error tolerance (resolves SEC-PART-001-003 second half).** Two create
  errors are **expected and benign** and must be classified as success/skip and
  logged at **WARN or below, never ERROR**, so they cannot trip the error-rate
  alert worker:
  1. duplicate-object (`SQLSTATE 42P07`) — partition already exists (covered by
     `IF NOT EXISTS`; treat any residual as success).
  2. the DEFAULT-capture error (`SQLSTATE 23514`, check_violation: *"updated
     partition constraint for default partition would be violated by some row"*) and
     the range-overlap error (`SQLSTATE 42P17`, invalid_object_definition) — raised
     when a rare skewed row already sits in the DEFAULT for the day being created.
     These are logged at **WARN** with the table + day + a hint ("a device clock may
     be skewed; rows are safely held in the DEFAULT partition"), and the manager
     moves on. Detection is via `errors.As(err, *pgconn.PgError)` on the code.
  Any **other** error is a genuine failure: logged at **ERROR**, per-table isolated,
  retried next cycle (FR-021, NFR-006).
- **Drop:** a partition whose **upper bound** `≤ now - window(table)` is dropped
  (`DROP TABLE`). The legacy partition is treated identically — its upper bound is
  `cutoff` (read from `partition_conversion`), so it ages out once
  `cutoff ≤ now - window` (FR-013, BR-004). **The DEFAULT partition is never
  dropped** (it has no finite upper bound; dropping it would break NFR-002). Windows
  come from config: `Config.RetentionDays` for
  `telemetry_snapshots`/`device_logs`/`probe_results`, the constant `7` for
  `heartbeats`/`container_logs`/`container_metrics` (FR-014; no new hardcoded
  retention value).
- **Scheduling:** `EnsureAll` at startup (sync), then a `1h` ticker loop
  (`Run(ctx)`) doing ensure + drop per table. A failure on one table logs at
  ERROR and continues to the next; the loop never dies (FR-021, NFR-006).
- **Observability & health surface (resolves SEC-PART-001-005).** Every create and
  drop emits `slog` at INFO with `table` and `partition` (name / range); every
  genuine failure at ERROR with the error (FR-020, NFR-005). In addition the manager
  maintains a mutex-protected **health snapshot** exposed via `Health()`:
  `LastRunAt`, `LastSuccessAt`, `LastError`, and per-table `FurthestFuturePartition`
  and `DefaultRowCount`. On a **persistent** stall the manager escalates loudly
  rather than only logging each cycle:
  - If the furthest future partition for any table is within **1 day** of `now`
    (forward coverage collapsing), it logs **WARN** ("forward partition coverage low").
  - If `EnsureAll`/`DropAged` has failed for **N consecutive cycles** (N=3), it logs
    a single escalated **ERROR** ("partition manager stalled") — deliberately routed
    through the DB log handler (`server.go:114-129`) so the operator sees it even
    without tailing stdout.
  - A non-zero `DefaultRowCount` is surfaced (WARN) so an operator can spot a
    clock-skewed device whose rows are piling into the DEFAULT (see AD-008).
  The `Health()` snapshot is a plain struct held on the `Server` so a future status
  endpoint can read it; adding an HTTP endpoint is out of scope for this story.

**Rationale.** A pure planner (date math + name parsing) is the only part with
interesting logic and is fully unit-testable without Postgres; the executor is a
mechanical `for`-loop of `CREATE`/`DROP`. Living under `internal/server` gives it
the pool and config without an import cycle.

**Alternatives considered.** `pg_partman` (prohibited, BR-001). A separate
goroutine per table (rejected: six tickers for no gain). Weekly partitions
(rejected: over-retention on 7-day tables). Reconciling DEFAULT rows into their day
partition via detach/move/reattach on every create (rejected: that takes
`ACCESS EXCLUSIVE` on a hot serving parent — worse than leaving rare skewed rows in
the near-empty DEFAULT and surfacing them via health; see AD-008).

---

### AD-006: Retention cutover — delete exactly six purge calls

**Decision.** Remove the six in-scope `DELETE` purge calls from `runRetention`
and delete their now-orphaned repo/interface/mock methods (no dead code). The
five out-of-scope purges (events, notification log, server logs,
**device probe results**, bootstrap keys) are untouched (FR-016, FR-017, AC-014,
AC-015). Drop-partition retention (AD-005) is the sole retention for the six
tables — no double retention, no gap: the manager runs at startup and hourly,
same cadence class as the old purge.

**Rationale.** Leaving the `DELETE` calls in would reintroduce the exact
dead-tuple churn this story eliminates (NFR-004). Deleting the orphaned
`PurgeHeartbeats`/`PurgeSnapshots`/etc. methods keeps the repo surface honest.

---

### AD-007: Downgrade is a documented, best-effort, lossy reversal

**Decision.** A partitioned table cannot be reverted to a plain heap without
moving rows, so full automatic reversibility is **not** provided (FR-019 permits
this explicitly). The `.down.sql` files undo prep cleanly (drop the bookkeeping
table, the `_pbound`/`_fk` constraints, the `<t>_pk_new` index) and, for the swap
(`000027.down.sql`), perform a **best-effort data-preserving reversal**: `DETACH`
the legacy partition, drop the (near-empty, freshly-created) forward partitions,
**drop the DEFAULT partition**, drop the partitioned parent, then rename
`<t>_legacy` back to `<t>` and restore its original single-column PK. This is safe
and lossless when run **soon after** conversion (forward + DEFAULT partitions still
empty). A prominent comment documents that any rows written into the forward daily
partitions **or the DEFAULT** after conversion are **discarded** by the down
migration, and that a production downgrade with meaningful post-conversion data
requires a manual data-merge runbook. `down` is intended for dev/test rollback, not
routine production use.

---

### AD-008: Forward DEFAULT catch partition guarantees NFR-002 (new)

**Decision (resolves SEC-PART-001-002).** Each partitioned parent carries exactly
one **DEFAULT** partition, `<t>_default`, created empty in file `000027`
immediately after the legacy attach. It catches any row whose key is not covered by
a bounded partition — in steady state, only rows dated **beyond the furthest
pre-created daily partition** (i.e. a device clock skewed past the 3-day forward
margin) or in a transient gap. Because such a row lands in the DEFAULT rather than
hitting a missing partition, **the `CopyFrom` / staged `INSERT … SELECT` batch
never fails** — NFR-002 becomes a genuine guarantee independent of agent clock skew,
not the prior "guaranteed only if no clock is >3 days fast" (which §2 confirms is
false for the target environment).

**Create-interaction analysis (required by the review).** When the manager creates
a new daily bounded partition `[D, D+1)`, PostgreSQL scans the DEFAULT to prove no
DEFAULT row belongs in `[D, D+1)`:

- **DEFAULT empty or holds only rows outside `[D, D+1)`** (the overwhelmingly common
  case — DEFAULT only ever holds far-future skewed rows) → the scan is O(near-zero)
  and the `CREATE` succeeds. This is the crucial difference from putting the 28 GB
  legacy blob in a DEFAULT (AD-001): the *forward* DEFAULT stays near-empty, so its
  per-create scan cost is trivial and acceptable in the hourly loop.
- **DEFAULT holds a row dated within `[D, D+1)`** (skew beyond the 3-day margin at
  the time the row was written) → `CREATE` raises `SQLSTATE 23514`. The manager
  classifies this as **benign** (AD-005), logs **WARN**, and leaves that day without
  a dedicated partition. Inserts for that day continue to land in the DEFAULT and
  **still succeed** — availability is unaffected.

**How DEFAULT rows are handled (they do not auto-migrate — documented).**
- Rows in the DEFAULT are **not** automatically moved into a daily partition later,
  and the manager does **not** perform a detach/move/reattach reconciliation (that
  would take `ACCESS EXCLUSIVE` on a live serving parent — rejected in AD-005).
- Because the DEFAULT is **never dropped** (AD-005), rows in it are **not subject to
  drop-partition retention**. In the pathological case (a permanently clock-skewed
  device) its rows accumulate there. Volume is expected to be negligible (only
  clock-broken devices), and it is **surfaced**: the manager reports
  `DefaultRowCount` per table in its health snapshot and logs WARN when non-zero
  (AD-005), so the operator can fix the offending device's clock. A manual cleanup
  (`DELETE FROM <t>_default WHERE <key> < now() - window`) is documented as an
  optional operator remediation; it is not automated in this story.
- This is an accepted, documented trade: NFR-002 (never lose a batch) is prioritized
  over perfectly-granular retention of anomalous, clock-broken rows.

**Runaway prevention still holds.** Forward daily partitions are bounded by the
**server** clock (`PartitionsToCreate(now, …)`), so a year-2099 device cannot make
the manager pre-create thousands of partitions; its rows simply sit in the DEFAULT.
The DEFAULT converts the prior "such a device's inserts have nowhere to go" hazard
into "such a device's inserts land safely in the catch partition and are surfaced."

**Alternatives considered.** Ingest-time clamp/validate of absurd future timestamps
(review option c) — rejected: mutates agent data and touches the `CopyFrom` hot path
(`container_*_repo.go`, `device_log_repo.go`), whereas the DEFAULT keeps the insert
path unchanged. On-demand partition creation in the insert error path (review option
b) — rejected: reintroduces dynamic DDL on the hot path and risks the runaway the
server-clock bound otherwise prevents.

---

### AD-009: Autovacuum reloptions are not carried onto forward partitions (SEC-PART-001-007)

**Decision.** `CREATE TABLE … (LIKE <t>_legacy INCLUDING DEFAULTS INCLUDING
GENERATED)` does **not** copy `000023`'s per-table autovacuum reloptions
(`autovacuum_vacuum_scale_factor`/`threshold`). We deliberately **do not** re-apply
them to the new parent or the daily partitions. Rationale: the parent stores no
rows; daily partitions are short-lived and dropped whole (drop-partition retention
generates **zero** dead tuples — the entire point of the story, NFR-004), so the
interim autovacuum tuning is simply no longer needed forward. The legacy partition
keeps its `000023` reloptions (they travel with the renamed relation). This keeps
the migration minimal and avoids re-encoding tuning that the new model obsoletes.
Documented so the technical writer does not claim the `000023` tuning still applies
to new partitions.

---

## 4. Component Changes

*(Unchanged from the pre-review file list — every security mitigation lands inside
files already listed. No new file, no new dependency, no ingest-path repo change.)*

| Action | File Path | Purpose |
|--------|-----------|---------|
| CREATE | `cmd/riot-server/migrations/000024_partition_bookkeeping.up.sql` | `partition_conversion` table; **derive `cutoff` from `max(key)`** and persist self-healing (`DO UPDATE` guarded by not-yet-partitioned); `DROP … IF EXISTS` + re-`ADD … CHECK (%I < %L) NOT VALID` per table (AD-002 #1, AD-003, AD-008 n/a). |
| CREATE | `cmd/riot-server/migrations/000024_partition_bookkeeping.down.sql` | Drop the bound checks; drop `partition_conversion` (AD-007). |
| CREATE | `cmd/riot-server/migrations/000025_partition_pk_index.up.sql` | `CREATE UNIQUE INDEX <t>_pk_new ON <t>(id,<key>)` per table (AD-002 #2, AD-004). |
| CREATE | `cmd/riot-server/migrations/000025_partition_pk_index.down.sql` | Drop `<t>_pk_new` (AD-007). |
| CREATE | `cmd/riot-server/migrations/000026_partition_validate_bound.up.sql` | `VALIDATE CONSTRAINT <t>_pbound` per table (AD-002 #3). |
| CREATE | `cmd/riot-server/migrations/000026_partition_validate_bound.down.sql` | No-op (cannot un-validate); documented comment. |
| CREATE | `cmd/riot-server/migrations/000027_partition_swap.up.sql` | Metadata swap: rename→legacy, create partitioned parent w/ composite PK, re-point sequence, `ATTACH` legacy bounded, **`ATTACH … DEFAULT` catch partition** (AD-008), create parent lookup+dedup indexes to the **exact** legacy defs (adopt, AD-004), `ADD FK … NOT VALID`, create initial forward partitions from `cutoff` (AD-001, AD-002 #4, AD-004, AD-008). |
| CREATE | `cmd/riot-server/migrations/000027_partition_swap.down.sql` | Best-effort lossy reversal incl. dropping the DEFAULT (AD-007). |
| CREATE | `cmd/riot-server/migrations/000028_partition_validate_fk.up.sql` | `VALIDATE CONSTRAINT <t>_fk` for the five FK'd tables (AD-002 #5, AD-004). |
| CREATE | `cmd/riot-server/migrations/000028_partition_validate_fk.down.sql` | Drop the FK constraints (AD-007). |
| CREATE | `internal/server/partition/plan.go` | Pure planning: `PartitionName`, `ParsePartitionDay`, **`PartitionsToCreate(now, cutoff, ahead, behind)`** (cutoff-aware), `PartitionsToDrop`, `TableSpec`, `PartitionInfo`, `Health` (AD-005). |
| CREATE | `internal/server/partition/plan_test.go` | Unit tests for the pure planner incl. **cutoff-start / no-legacy-overlap** and future-skew reasoning (AC-010, AC-011, AC-012, AC-013, AC-021). |
| CREATE | `internal/server/partition/manager.go` | `Manager` (pool + specs + slog + health); `EnsureAll`, `DropAged`, `Run(ctx)`; idempotent `CREATE`/`DROP` with **benign-error classification** (`%I` identifiers), **health snapshot + escalation**, DEFAULT-row counting (AD-005, AD-008). |
| CREATE | `internal/server/partition/manager_test.go` | Integration tests against live PG (AC-001..AC-006, AC-008..AC-012, AC-019, AC-020, AC-021). |
| MODIFY | `internal/server/server.go` | Build `partition.Manager` from `Config` after repos; call `EnsureAll` synchronously post-migration before serving; `go s.PartitionManager.Run(ctx)`; hold the manager for its `Health()` snapshot; **remove** the six in-scope purge calls from `runRetention` (AD-005, AD-006). |
| MODIFY | `internal/server/db/telemetry_repo.go` | Remove orphaned `PurgeHeartbeats`, `PurgeSnapshots` (AD-006). |
| MODIFY | `internal/server/db/probe_repo.go` | Remove orphaned `PurgeResults` (probe_results) (AD-006). |
| MODIFY | `internal/server/db/container_log_repo.go` | Remove orphaned `Purge` (AD-006). **Insert path untouched.** |
| MODIFY | `internal/server/db/container_metric_repo.go` | Remove orphaned `Purge` (AD-006). **Insert path untouched.** |
| MODIFY | `internal/server/db/device_log_repo.go` | Remove orphaned `Purge` (AD-006). **Insert path untouched.** |
| MODIFY | `internal/server/db/interfaces.go` | Remove the six removed methods from the five repo interfaces (AD-006). |
| MODIFY | `internal/testutil/mocks.go` | Remove the six corresponding mock methods (AD-006). |
| MODIFY | `internal/server/server_test.go` (or existing retention test) | Update retention tests: assert the six tables are no longer purged, the five out-of-scope purges still run (AC-014, AC-015). |

> Deliberately **not** changed: every insert/read repo query (partition-transparent,
> §2), `internal/server/config.go` (no new env var, FR-014), the agent, the ingest
> API, migrations `000001`–`000023` (never edited once shipped). `DeviceProbeRepo.PurgeResults`
> is **kept** (out of scope — different table).

---

## 5. Data Model Changes

### Per-table conversion (before → after)

For each table `<t>` with range key `<key>` (`timestamp`, except `probe_results` =
`created_at`):

**Before:** plain heap, `id BIGSERIAL PRIMARY KEY`, its FR-003 indexes, its FR-004
FK (except `container_logs`).

**After:**
```
<t>                      -- PARTITION BY RANGE (<key>), PRIMARY KEY (id, <key>)
├─ <t>_legacy            -- FOR VALUES FROM (MINVALUE) TO (<cutoff>)   [all pre-existing rows]
├─ <t>_pYYYYMMDD         -- FOR VALUES FROM (day) TO (day+1)           [cutoffDay .. today+3, managed]
├─ …
└─ <t>_default           -- DEFAULT                                    [near-empty forward catch, AD-008]
```
Column definitions, defaults, and NOT NULL are copied verbatim. The `id`
sequence is preserved and re-owned by the parent (AD-004). Every FR-003 index
exists on the parent (propagated to all partitions) with the exact legacy
definition; `device_logs` keeps its UNIQUE dedup index. FR-004 FKs exist on the
parent (`container_logs` FK-free). Autovacuum reloptions are **not** re-applied to
new partitions (AD-009).

### New bookkeeping table

`partition_conversion (table_name PK, key_column, cutoff, created_at)` — one row
per converted table; the single source of truth for the legacy bound. `cutoff` is
derived from `max(<key>)` (AD-003).

### Partition naming / bounds

- Legacy: `<t>_legacy`, range `[MINVALUE, cutoff)`.
- Daily: `<t>_p<YYYYMMDD>`, range `[YYYYMMDD 00:00:00Z, +1 day)`, only for days
  `≥ cutoffDay` (no legacy overlap, AD-005).
- Forward catch: `<t>_default`, DEFAULT (everything above the furthest daily
  partition and any gap).
- The bounded partitions are contiguous, non-overlapping, half-open; the DEFAULT
  catches the remainder — every insertable timestamp maps to exactly one partition
  (FRD §7 validation rule, now with a guaranteed catch).

### Full conversion SQL sequence with per-step lock analysis

Illustrated for `container_logs` (28 GB case); the same shape applies to all six,
each guarded by an idempotent `DO` block that skips if the table is already
partitioned (`pg_partitioned_table`) or the phase's target already holds.

```
-- FILE 000024 (one implicit txn; O(1) locks; one cheap max() read per table)
--  Guarded DO block per table:
DO $$
DECLARE v_cutoff timestamptz;
BEGIN
  -- Already converted? leave the historical row untouched.
  IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
             JOIN pg_class c ON c.oid = pt.partrelid
             WHERE c.relname = 'container_logs') THEN
    RETURN;
  END IF;
  -- Derive cutoff strictly > max(existing key), day-aligned; empty-table -> now-based.
  SELECT date_trunc('day', GREATEST(now(), (SELECT max(timestamp) FROM container_logs)))
         + INTERVAL '1 day'
    INTO v_cutoff;
  -- Self-healing persist (fixes the sticky-cutoff bug): recompute on a dirty pre-swap re-run.
  INSERT INTO partition_conversion(table_name, key_column, cutoff)
       VALUES ('container_logs', 'timestamp', v_cutoff)
  ON CONFLICT (table_name) DO UPDATE
       SET cutoff = EXCLUDED.cutoff, key_column = EXCLUDED.key_column;
  -- Ensure the NOT VALID bound literal matches the persisted cutoff.
  ALTER TABLE container_logs DROP CONSTRAINT IF EXISTS container_logs_pbound;
  EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (%I < %L) NOT VALID',
                 'container_logs', 'container_logs_pbound', 'timestamp', v_cutoff);
END $$;                                               -- ACCESS EXCLUSIVE (catalog), released at COMMIT

-- FILE 000025 (own txn)
CREATE UNIQUE INDEX container_logs_pk_new
  ON container_logs (id, timestamp);                   -- SHARE (reads ok, writes wait), O(rows)

-- FILE 000026 (own txn)
ALTER TABLE container_logs
  VALIDATE CONSTRAINT container_logs_pbound;           -- SHARE UPDATE EXCLUSIVE, non-blocking, O(rows)
                                                       -- CANNOT fail: cutoff > max(key), no concurrent writer (pre-serving)

-- FILE 000027 (own txn; all O(1) metadata)
ALTER TABLE container_logs RENAME TO container_logs_legacy;          -- AEL, ms
ALTER INDEX container_logs_pk_new RENAME TO container_logs_legacy_pk;
CREATE TABLE container_logs (LIKE container_logs_legacy
    INCLUDING DEFAULTS INCLUDING GENERATED,
    PRIMARY KEY (id, timestamp))
  PARTITION BY RANGE (timestamp);                                    -- AEL on new rel, ms
ALTER SEQUENCE container_logs_id_seq OWNED BY container_logs.id;
ALTER TABLE container_logs ALTER COLUMN id SET DEFAULT nextval('container_logs_id_seq');
ALTER TABLE container_logs
  ATTACH PARTITION container_logs_legacy
  FOR VALUES FROM (MINVALUE) TO ('<cutoff>');   -- AEL brief: CHECK VALID + PK index exists => metadata only, NO scan
CREATE TABLE container_logs_default
  PARTITION OF container_logs DEFAULT;                               -- empty forward catch (AD-008), instant
-- Parent lookup indexes: EXACT legacy defs so PG ADOPTS the child index (no rebuild under AEL, AD-004)
CREATE INDEX idx_container_logs_lookup
  ON container_logs (device_id, container_id, timestamp DESC);
CREATE INDEX idx_container_logs_time ON container_logs (timestamp);
-- (container_logs has no FK)
-- initial forward partitions, starting at cutoffDay (never below cutoff), through today+3:
CREATE TABLE IF NOT EXISTS container_logs_p<cutoffday>
  PARTITION OF container_logs FOR VALUES FROM ('<cutoffday>') TO ('<cutoffday+1>');
-- … through today+3.

-- FILE 000028 (own txn) — for FK'd tables only (not container_logs)
ALTER TABLE <t> VALIDATE CONSTRAINT <t>_fk;            -- SHARE UPDATE EXCLUSIVE, non-blocking
```

**Lock summary:** `ACCESS EXCLUSIVE` is taken only for O(1) catalog operations
(files 1 and 4). The two O(rows) operations (files 2, 3) and the FK validate
(file 5) never take `ACCESS EXCLUSIVE`, never rewrite the heap, and run
pre-serving. Creating the empty DEFAULT and the initial forward partitions in file
4 scans only the (empty at conversion) DEFAULT — trivial. NFR-001 satisfied.

**Why `VALIDATE` (file 000026) cannot fail.** `cutoff` was derived in file 1 as
`> max(<key>)` (AD-003), and migrations run **before** `Server.Start` serves
traffic (`server.go:88-93` precedes `server.go:182-195`), so no writer can add a
row between the compute and the validate. The maximum key the scan can observe is
the one file 1 measured; the bound is provably satisfiable. This is the structural
reason the prior SEC-001 boot-failure path is closed.

**Idempotency / resumability (NFR-003, FR-006, FR-007).** Each phase is atomic
(one implicit txn) so a crash rolls it back entirely — no half state. Each phase's
`DO` block is guarded (skip if already partitioned / constraint already valid /
index already exists), so re-execution converges. File 1 additionally **recomputes
and re-persists** `cutoff` on any pre-swap re-run (`DO UPDATE` + constraint re-add),
so a dirty re-run never inherits a stale bound. On a **fresh/empty** DB the legacy
table is empty: `max(<key>)` is NULL → `cutoff = now`-based, the check validates
trivially, the unique index builds instantly, and the legacy + DEFAULT partitions
attach empty — no special case (FR-007, AC-008).

**Dirty-state recovery (WORKING procedure, resolves SEC-001 compounding defect).**
golang-migrate marks the schema **dirty** at the failing version and rolls that
file back. Recovery is:
1. `migrate force <last-good-version>` (e.g. the version before the failing file).
2. Reboot the server; `RunMigrations` re-runs from that version.

Because `cutoff` is **data-derived and self-healing** (file 1 recomputes from the
live `max(<key>)` on the re-run via `DO UPDATE`, and re-adds the `_pbound` literal to
match), the operator does **not** need to hand-edit any SQL literal and does **not**
need a manual `UPDATE partition_conversion`. The prior broken remedy ("bump the
`+2 days` literal and re-run", which the sticky `DO NOTHING` ignored) is gone
because there is no fixed offset any more. This is validated by AC-007/AC-022.

---

## 6. API / Interface Contract

No HTTP API changes. This story is schema + background-job only; the ingest and
read endpoints are byte-for-byte unchanged (§2).

**Internal Go contract — `internal/server/partition`:**

```go
// TableSpec is the immutable description of one partitioned table.
type TableSpec struct {
    Name       string        // e.g. "container_logs"
    KeyColumn  string        // "timestamp" | "created_at"
    Retention  time.Duration // 7*24h or RetentionDays*24h
}

// Pure planning (no DB, no clock reads except the injected `now`):
func PartitionName(table string, day time.Time) string           // "<t>_pYYYYMMDD"
func ParsePartitionDay(name string) (day time.Time, ok bool)      // inverse; ok=false for legacy/default/non-matching

// Cutoff-aware: never emits a day whose range is fully below cutoff (SEC-003).
func PartitionsToCreate(now, cutoff time.Time, aheadDays, behindDays int) []time.Time

func PartitionsToDrop(now time.Time, spec TableSpec, existing []PartitionInfo) []PartitionInfo

type PartitionInfo struct {
    Name       string
    UpperBound time.Time // parsed from name, or cutoff for the legacy partition
    IsLegacy   bool
    IsDefault  bool      // the forward catch; never droppable (AD-008)
}

// Health snapshot surfaced to the Server (SEC-005).
type Health struct {
    LastRunAt     time.Time
    LastSuccessAt time.Time
    LastError     string
    Tables        map[string]TableHealth // per-table
}
type TableHealth struct {
    FurthestFuturePartition time.Time
    DefaultRowCount         int64
}

// Executor + scheduler:
func NewManager(pool *pgxpool.Pool, specs []TableSpec, log *slog.Logger) *Manager
func (m *Manager) EnsureAll(ctx context.Context) error // create-ahead for all specs; called sync at startup
func (m *Manager) DropAged(ctx context.Context) error  // drop aged partitions incl. legacy; never the DEFAULT
func (m *Manager) Run(ctx context.Context)             // startup EnsureAll + hourly ensure/drop loop
func (m *Manager) Health() Health                      // mutex-protected snapshot
```

`Manager` reads each legacy partition's upper bound (`cutoff`) from
`partition_conversion`; both create-start (`PartitionsToCreate(now, cutoff, …)`) and
legacy drop-eligibility (`PartitionsToDrop`) are decided by the pure planner. Benign
create errors (`42P07`, `23514`, `42P17`) are classified in `manager.go` and logged
at WARN, not ERROR (AD-005). All dynamically-built identifiers use `format('%I', …)`
(SEC-006).

---

## 7. Sequence / Flow

**A. One-time conversion (boot, pre-serving).** `Server.Start` → `DB.RunMigrations`
applies `000024`→`000028` in order (each its own txn). Per table: derive+persist
`cutoff` from `max(key)` + add bound check (O(1)) → build composite unique index
(O(rows), SHARE) → validate bound (O(rows), non-blocking, cannot fail) →
rename+create-parent+attach-legacy+attach-DEFAULT+indexes+FK-NOT-VALID+initial
forward partitions (O(1), brief AEL) → validate FK (O(rows), non-blocking). If
already partitioned, every phase no-ops; if dirty-re-run pre-swap, file 1
recomputes `cutoff`.

**B. Startup partition ensure (post-migration, pre-serving).** `Server.Start`
constructs `partition.Manager` from `Config` and calls `EnsureAll(ctx)`
**synchronously** — creating daily partitions `[cutoffDay .. today+3]` before the
router accepts a single insert. Then `go s.PartitionManager.Run(ctx)`. (The DEFAULT
already guarantees any not-yet-partitioned row inserts, so this is an optimization
not a correctness gate — AD-008.)

**C. Steady-state manager cycle (hourly).** For each spec: `PartitionsToCreate(now,
cutoff, 3, 1)` → `CREATE TABLE IF NOT EXISTS … PARTITION OF` (INFO on create; WARN
+ skip on benign `42P07`/`23514`/`42P17`; ERROR + continue on genuine failure);
list existing partitions → `PartitionsToDrop` → `DROP TABLE` (INFO on drop, incl.
the legacy partition when `cutoff ≤ now-window`; **never the DEFAULT**). Update the
health snapshot (last run/success/error, furthest future partition, DEFAULT row
counts); escalate WARN/ERROR on collapsing coverage or N consecutive failures. Loop
survives all per-table errors (FR-021, NFR-006).

**D. Insert (unchanged).** Agent → ingest handler → repo `INSERT`/`COPY`/staged
`INSERT` into the parent → PG routes to the daily partition for the row's timestamp
(or the legacy partition for `< cutoff`, **or the DEFAULT for anything beyond the
furthest daily partition**). **Never fails for a missing partition** — the DEFAULT
is the guaranteed catch (AD-008).

**E. Retention (hourly, `runRetention`).** Purges only the five out-of-scope
tables; the six in-scope tables are served entirely by C. No row-level `DELETE`,
no scan on the six (NFR-004).

---

## 8. Acceptance Criteria Mapping

Pure planner ACs are unit-testable (`plan_test.go`, injected `now`/`cutoff`, no DB).
Schema/DDL ACs need a **live Postgres** (`manager_test.go` / migration integration,
tagged for the Linux CI job that has PG). The **test type is noted per row**:
**Unit** = pure function, no DB; **Integration** = requires a live Postgres.

| AC ID | Fulfilled By | Test Strategy |
|-------|--------------|---------------|
| AC-001 | `000027` creates parents `PARTITION BY RANGE (timestamp)` for the five timestamp tables (AD-001) | **Integration** — after migrate, `pg_partitioned_table`/`pg_class.relkind='p'` shows each of the five partitioned on `timestamp`. |
| AC-002 | `probe_results` parent `PARTITION BY RANGE (created_at)` (AD-001) | **Integration** — `probe_results` `relkind='p'`, partition key = `created_at`; assert no `timestamp` column. |
| AC-003 | `PRIMARY KEY (id,<key>)` + surviving `id` sequence (AD-004) | **Integration** — `pg_constraint` PK columns = `{id,<key>}`; two inserts yield distinct `id`s. |
| AC-004 | Parent indexes adopt legacy indexes with exact defs (AD-004) | **Integration** — every FR-003 index name present on the parent (incl. `idx_device_logs_priority`, `idx_device_logs_dedup`); `pg_index` shows propagation to partitions. |
| AC-005 | FKs on parent `NOT VALID`→`VALIDATE`; `container_logs` FK-free (AD-004) | **Integration** — `pg_constraint` shows the four `device_id→devices` FKs + `probe_id→probes`, all `convalidated`; `container_logs` has zero FK. |
| AC-006 | Bounded attach with pre-validated CHECK + pre-built PK index; O(1) swap (AD-001, AD-002, AD-004) | **Integration** — seed a large legacy table; rows present via `<t>_legacy`; `pg_locks` capture during `000027` shows `AccessExclusiveLock` durations independent of row count. (Relfilenode/no-rewrite gate is AC-019.) |
| AC-007 | Guarded `DO` blocks + atomic per-file txns + self-healing `cutoff` (AD-002, AD-003, §5) | **Integration** — run migrate twice ⇒ second run no-ops, no error; simulate crash by aborting after file N then re-running from a guarded state ⇒ converges (data/partition counts stable). |
| AC-008 | Fresh-DB path — empty legacy attaches empty, `cutoff` falls back to now-based (AD-001, AD-003, FR-007) | **Integration** — migrate on an empty DB ⇒ six partitioned tables, empty legacy + DEFAULT partitions, server boots and serves. |
| AC-009 | Partition-transparent repos (§2) | **Integration** — run existing repo tests against the partitioned schema: insert, `GetLatestSnapshot`, per-device/container/probe lookups, range scans, and `device_logs` `ON CONFLICT` dedup all pass unchanged. |
| AC-010 | `EnsureAll` sync at startup + cutoff-aware `PartitionsToCreate` 3-day margin (AD-005, NFR-002) | **Unit** (`plan_test.go`) `[AC-010]` — `PartitionsToCreate(now, cutoff, 3, 1)` returns day-starts in `[max(now-1,cutoffDay) .. now+3]`; 23:59Z still yields tomorrow. **Integration** — after `EnsureAll`, inserts at `now` and `now+3d` route to an existing partition; restart ⇒ coverage intact. |
| AC-011 | `PartitionsToDrop` (AD-005, NFR-004) | **Unit** `[AC-011]` — partition whose upper bound `≤ now-window` is returned; one still intersecting the window is not; boundary (upper bound exactly `now-window`) droppable, one second before not; **the DEFAULT is never returned**. **Integration** — `DROP` issued, no `DELETE`/seq-scan (`pg_stat`). |
| AC-012 | Legacy treated as `PartitionInfo{IsLegacy:true, UpperBound:cutoff}` (AD-005, BR-004) | **Unit** `[AC-012]` — legacy dropped iff `cutoff ≤ now-window`; retained while within window. **Integration** — legacy `DROP` occurs at the right cycle; queries hit it before. |
| AC-013 | Windows from `Config.RetentionDays` + const 7 (AD-005, AD-006, FR-014) | **Unit** `[AC-013]` — spec builder maps the three configurable tables to `RetentionDays`, three to `7`; changing `RetentionDays` changes only the former; grep asserts no new hardcoded duration. |
| AC-014 | Six purge calls removed from `runRetention` (AD-006) | **Unit** (`server_test.go`) `[AC-014]` — mock repos: after `runRetention`, the six purge methods are never invoked (removal is compile-enforced + test asserts no DELETE on those tables). |
| AC-015 | Five out-of-scope purges retained (AD-006) | **Unit** `[AC-015]` — `runRetention` still calls `EventRepo.Purge`, `NotifyRepo.PurgeNotificationLog`, `LogRepo.Purge`, `DeviceProbeRepo.PurgeResults`, `CARepo.PurgeStaleBootstrapKeys`; none partitioned. |
| AC-016 | Split-file O(1) AEL swap on a populated DB (AD-001, AD-002) | **Integration** — seed a multi-GB-representative legacy table; run migrate; assert boot completes, no `ACCESS EXCLUSIVE` wait scaling with rows (lock capture), server serves. |
| AC-017 | `slog` in `manager.go` create/drop/failure paths (AD-005) | **Unit** `[AC-017]` — inject a `slog` handler capturing records + a fake executor: create emits INFO w/ `table`+`partition`; drop emits INFO; a **genuine** executor error emits ERROR and the loop continues to the next spec and re-tries next cycle. |
| AC-018 | `.down.sql` reversal + documented limitation, incl. DEFAULT drop (AD-007) | **Integration** — `migrate down` after a fresh conversion restores plain tables with legacy data + single-column PK; **doc/comment review** confirms the non-reversibility + data-loss (incl. DEFAULT rows) caveat is stated. |
| **AC-019** *(new, SEC-004)* | O(1) swap is a **hard gate**: exact index defs adopted, no rebuild/rewrite (AD-004, §5) | **Integration** — capture `relfilenode` of each legacy child index **and** the legacy heap before/after `000027`; assert **unchanged** (no rebuild). Run on both a tiny and a large fixture; assert AEL hold time is row-count-independent. A relfilenode change **fails the build** (DoD gate, §13). |
| **AC-020** *(new, SEC-005)* | Manager health surface + persistent-stall escalation (AD-005) | **Unit** `[AC-020]` — with a fake executor forced to fail, `Health()` reports `LastError`/non-advancing `LastSuccessAt`; after N consecutive failures a single escalated ERROR is emitted; a non-zero DEFAULT count is surfaced (WARN). Furthest-future-partition within 1 day of `now` emits WARN. **Integration** — `Health().Tables[t].DefaultRowCount` reflects a seeded DEFAULT row. |
| **AC-021** *(new, SEC-002/003)* | Forward DEFAULT catch + no legacy-overlap create storm (AD-008, AD-005) | **Unit** `[AC-021a]` — `PartitionsToCreate` emits **no** day fully below `cutoff` (freshly-converted table: no legacy overlap). **Integration** `[AC-021b]` — a `CopyFrom` batch containing a row dated `now+10d` (beyond margin) **succeeds** (row lands in `<t>_default`); the whole batch is inserted; the manager cycle logs the DEFAULT-capture create error at **WARN not ERROR** and does not stop. |
| **AC-022** *(new, SEC-001)* | `cutoff` derived from `max(key)`; `VALIDATE` passes with a future-dated row; empty-table fallback; working dirty recovery (AD-003) | **Integration** `[AC-022a]` — seed a legacy row dated `now+60d`; run migrate; `VALIDATE CONSTRAINT` **passes** and `partition_conversion.cutoff > that row`. `[AC-022b]` — empty table ⇒ `cutoff` day-aligned from `now`. `[AC-022c]` — abort mid-conversion pre-swap, `force`+re-run ⇒ file 1 recomputes `cutoff` from current `max(key)` (no stale value survives), conversion completes. |

---

## 9. Error Handling

| Failure mode | Detection | Response |
|--------------|-----------|----------|
| Migration errors mid-conversion | golang-migrate returns error → `Start` returns error | Boot aborts; schema marked dirty. Recovery: `migrate force <last-good>` + reboot; guarded `DO` blocks resume and file 1 **recomputes `cutoff`** from live `max(key)` (no stale value survives). Atomic per-file txn guarantees no half-converted table (NFR-003). |
| `VALIDATE CONSTRAINT` (file `000026`) | — | **Cannot fail**: `cutoff > max(<key>)` by construction (AD-003) and no concurrent writer exists pre-serving (`server.go:88-93` before serving). A future-dated row raises `max(<key>)` and thus `cutoff`; it does not break the validate. (Previously the top boot-failure risk; now closed.) |
| Insert for a timestamp beyond the furthest daily partition | PG routes to `<t>_default` | **Never fails** — the DEFAULT catches it (AD-008). No missing-partition error reaches the batch. Row is surfaced via `DefaultRowCount` health (WARN) so a skewed device is noticed. |
| Manager create hits DEFAULT-capture / overlap | `pgconn.PgError` code `23514`/`42P17` | **Benign** (AD-005): log **WARN** (hint: device clock skew), skip that day, continue. Inserts for the day still land in the DEFAULT. Does **not** trip `newServerErrorAlertWorker`. |
| Manager create duplicate object | code `42P07` | Treated as success (idempotent `IF NOT EXISTS`). |
| Manager create/drop genuine failure (disk full, perms) | any other error | **ERROR** log with `table`+`partition`; per-table isolation; retried next cycle. After **N=3 consecutive** cycle failures, one escalated ERROR ("partition manager stalled") routed through the DB log handler (AD-005, SEC-005). |
| Forward coverage collapsing | furthest future partition within 1 day of `now` | **WARN** ("forward partition coverage low") + reflected in `Health()`, before ingest could ever be affected. |
| Manager goroutine panic | — | Per-cycle recover + ERROR log; loop isolated from ingest (NFR-006). |
| `partition_conversion` row missing when reading legacy bound | query returns no rows | Manager skips the legacy drop for that table (fail-safe: never drops data it can't bound), logs WARN. |

No new HTTP status codes. Partition logging matches the existing `runRetention`
slog style (INFO on action, WARN on benign/expected, ERROR on genuine failure).

---

## 10. Security Considerations

Low surface — no new endpoint, input, auth path, or external service. Notes:

- **Privilege.** The conversion runs as the existing DB role (already has DDL via
  the migration path). No sudoers/OS-privilege change.
- **No dynamic SQL from user input.** Partition/table identifiers come from a fixed
  in-code spec list and the server wall-clock (`PartitionName`), never from request
  data. As a defense-in-depth convention (SEC-PART-001-006), **all dynamically-built
  identifiers use `format('%I', …)`** and literals use `format('%L', …)` — in both
  the `DO`-block DDL (`000024`, `000027`) and the Go manager — so the pattern stays
  injection-correct even if a spec entry were ever changed. No exploitable path today.
- **`DROP` targets are pattern-gated.** The manager only drops names matching the
  fixed `<t>_p<YYYYMMDD>` pattern or the recognized `<t>_legacy`. It **never** drops
  `<t>_default` (which is also excluded from the drop planner, AD-008) and never a
  table it does not recognize.
- **Data destruction is bounded.** `DROP` (retention) and the down-migration's lossy
  reversal are the only destructive operations; both are gated by the computed
  retention window / explicit `migrate down` and are logged. DEFAULT rows are never
  auto-dropped.

---

## 11. Performance Considerations

- **Retention is O(1).** Dropping a partition is a catalog + file unlink; it returns
  space to the OS immediately, generates zero dead tuples, and imposes zero vacuum
  load (NFR-004). Replaces the continuous `DELETE` churn that drove `container_logs`
  to 28 GB / ~97 % bloat.
- **Conversion cost (one-time, boot).** One cheap `max(<key>)` read per table (file
  1) + two O(rows) read passes (build `<t>_pk_new`, `VALIDATE` the bound) + a
  non-blocking FK validate. On the 28 GB `container_logs` this is sequential reads +
  an index build, minutes-scale, under `SHARE`/`SHARE UPDATE EXCLUSIVE` — **no heap
  rewrite** (contrast `VACUUM FULL`, which rewrote 34 GB→7.3 GB). Runs pre-serving;
  the `ACCESS EXCLUSIVE` swap is sub-second and row-count-independent (NFR-001).
- **O(1)-swap adoption is a hard gate, not an assumption (SEC-004).** File `000027`
  begins with `RENAME` (takes AEL) and the AEL is held until the file commits. The
  swap is O(1) **only** if every parent index byte-matches the legacy child def so PG
  *adopts* rather than *rebuilds* it (a rebuild would be O(rows) under the held AEL —
  the NFR-001 violation). AD-004 specifies the exact defs; AC-019 makes
  "no relfilenode change on any legacy child index or the heap" and "AEL hold time
  row-count-independent" a **build-failing DoD gate** on a populated fixture.
- **DEFAULT partition create-scan cost (SEC-002 analysis).** Creating a daily
  partition scans **only** the DEFAULT to prove no DEFAULT row falls in the new
  range. The forward DEFAULT is **near-empty** (only clock-skewed/gap rows), so this
  scan is negligible in the hourly loop — the key distinction from the rejected
  "legacy blob in a DEFAULT" (which would rescan 28 GB every create). The legacy
  data is a **bounded** partition and is never scanned on a subsequent create.
- **Rolling-deploy note.** File `000025`'s `SHARE` lock blocks *writes* on the legacy
  table for the index-build duration. Because migrations run before the new instance
  serves, this only matters if an **old** instance is still writing during a rolling
  deploy; its inserts would queue for the build duration. Non-issue for the
  single-instance homelab target; documented as the accepted trade for staying inside
  golang-migrate (no `CONCURRENTLY`).
- **Indexing.** No new indexes beyond the composite PK index required by partitioning
  (FR-002); all FR-003 indexes are re-created on the parent with their exact legacy
  defs and **adopt** the already-built legacy child indexes (no rebuild). Per CLAUDE
  "no indexes not called out here" — the only added index is `<t>_pk_new` (the PK),
  which is mandatory.
- **Autovacuum reloptions not carried forward (AD-009).** New partitions use PG
  defaults; immaterial because they hold short-lived data dropped whole (zero dead
  tuples). The legacy partition keeps its `000023` reloptions.
- **Partition count.** Daily × (≤31 for 30-day tables, ≤8 for 7-day tables) + 3-day
  margin + 1 legacy + 1 DEFAULT ≈ low hundreds total across six tables — comfortable
  for PG 16 planning/pruning at homelab scale. Predicated queries benefit from
  partition pruning.
- **Insert path.** Tuple routing on a range-partitioned table is O(log n) in
  partition count — negligible; `COPY`/`INSERT` throughput effectively unchanged. The
  DEFAULT adds no hot-path cost for well-dated rows (they route to a bounded
  partition; the DEFAULT is only touched for out-of-range rows).

---

## 12. Implementation Notes for Engineers

- **`cutoff` is data-derived and read back, never a fixed offset.** File 1 computes
  `date_trunc('day', GREATEST(now(), max(<key>))) + INTERVAL '1 day'` and persists it
  via `ON CONFLICT (table_name) DO UPDATE` (**not** `DO NOTHING`), guarded to skip
  already-partitioned tables. Re-add the `_pbound` CHECK with the freshly-computed
  literal each pre-swap run (`DROP CONSTRAINT IF EXISTS` then `ADD … NOT VALID`). File
  4 reads `cutoff` from `partition_conversion` and bakes it via `format('… TO (%L)', cutoff)`.
- **Adopt, don't rebuild — verify against AD-004's exact table.** After `ATTACH`,
  `CREATE INDEX` on the *parent* with the **exact** legacy definition (column order,
  `DESC`, opclass, `md5(message)` expression). Any mismatch forces a full rebuild
  under the RENAME's AEL. AC-019 gates this with a relfilenode check — write that test
  and run it on a populated fixture before claiming NFR-001.
- **Create the DEFAULT partition** (`CREATE TABLE <t>_default PARTITION OF <t>
  DEFAULT`) in `000027` right after the legacy attach. It must be empty at creation.
- **Sequence:** `ALTER SEQUENCE <t>_id_seq OWNED BY <t>.id` + reset the column default
  after creating the parent, or `id` generation breaks.
- **`device_logs` dedup:** the parent must carry `idx_device_logs_dedup` (UNIQUE,
  includes `timestamp`) or the `ON CONFLICT` insert path fails. Test dedup explicitly
  (AC-009).
- **Manager purity:** all date math + name parsing lives in `plan.go` as pure
  functions taking injected `now`/`cutoff` — **no `time.Now()` in testable code**.
  `manager.go` supplies `time.Now().UTC()` at the edges only.
- **Benign-error classification:** use `errors.As(err, &pgErr)` (`*pgconn.PgError`);
  treat codes `42P07` (duplicate object), `23514` (default-partition would be
  violated), `42P17` (partition would overlap) as **benign** → WARN + skip, never
  ERROR (so `newServerErrorAlertWorker` is not tripped). Verify the exact codes
  against your PG 16 in the integration test; classify by code, not message text.
- **Never drop the DEFAULT.** `PartitionsToDrop` must exclude `IsDefault`; the drop
  executor must refuse any name that is not `<t>_p<YYYYMMDD>` or `<t>_legacy`.
- **Health surface:** hold the `Manager` on `Server` and expose `Health()`; update the
  snapshot every cycle; escalate a single ERROR after N=3 consecutive failures and a
  WARN when forward coverage < 1 day or `DefaultRowCount > 0`.
- **Identifiers via `%I`, literals via `%L`** in all dynamic DDL (DO blocks and Go).
- **UTC everywhere.** Partition day boundaries, `cutoff`, and drop math are all UTC.
- **Do not** re-introduce a `DELETE` fallback for the six tables (NFR-004), do **not**
  touch the ingest `CopyFrom`/staged-`INSERT` paths, and do **not** remove
  `DeviceProbeRepo.PurgeResults` (out of scope).
- **Dirty-schema recovery** (crash mid-conversion): `migrate force <last-good>` then
  reboot — file 1 recomputes `cutoff`, so no manual `UPDATE partition_conversion` is
  needed. Document this for the technical writer.
- **DEFAULT-row remediation (operator doc):** a persistently clock-skewed device
  piles rows into `<t>_default`; surface it via health/WARN. Optional manual cleanup:
  `DELETE FROM <t>_default WHERE <key> < now() - <window>` — not automated.
- **Bloat is not reclaimed by this story** (BR-007): legacy-partition bloat persists
  until the legacy partition ages out and is dropped; the separate
  `VACUUM FULL`/`pg_repack` op in `docs/ops/postgres-bloat.md` is unaffected.
- **No `-race` on Windows** (CGO); `go test ./...` locally, CI runs `-race` on Linux.
  Integration tests requiring PG must be tagged/gated to the CI job with a live Postgres.

---

## 13. Definition of Done

- [ ] All §4 component changes implemented; every §8 AC has a named `[AC-XXX]` test
      passing (unit for the planner + retention wiring + health; integration for
      schema/DDL, gated on the PG-backed CI job).
- [ ] **`cutoff` is derived from `max(<key>)`** (`> max` invariant), day-aligned,
      empty-table fallback to now-based; `VALIDATE` passes with a seeded future-dated
      row — AC-022.
- [ ] **Dirty-state recovery works** without hand-editing SQL or manual
      `UPDATE partition_conversion`: abort mid-conversion pre-swap, `force`+re-run
      recomputes `cutoff` and converges — AC-022c, AC-007.
- [ ] **Forward DEFAULT catch present**; a `CopyFrom`/staged-`INSERT` batch containing
      an out-of-margin future row **succeeds** (row in `<t>_default`); NFR-002 holds
      independent of clock skew — AC-021b.
- [ ] **No post-conversion overlap error storm**: `PartitionsToCreate` emits no
      legacy-overlapping day; the DEFAULT-capture create error logs WARN not ERROR —
      AC-021a, AC-021b (no `newServerErrorAlertWorker` trip).
- [ ] **O(1)-swap hard gate**: after `000027` no legacy child index or heap
      `relfilenode` changes (no rewrite) and AEL hold time is row-count-independent on
      a populated fixture — AC-019, AC-006, AC-016. A relfilenode change fails the build.
- [ ] **Manager health surface**: `Health()` reports last run/success/error, furthest
      future partition, and DEFAULT row counts; persistent stall escalates a single
      ERROR; low coverage / non-zero DEFAULT emit WARN — AC-020.
- [ ] Migrations `000024`–`000028` apply cleanly on **both** a fresh/empty DB and a
      populated legacy-style DB; re-running (reboot) is a no-op; crash-resume converges
      — AC-007, AC-008.
- [ ] All six tables are `PARTITION BY RANGE` on the correct key, with composite PK,
      every FR-003 index (incl. `device_logs` dedup) at the exact legacy def, correct
      FKs (`container_logs` FK-free), and a DEFAULT partition — AC-001..AC-005.
- [ ] Existing repo tests pass unchanged against the partitioned schema (inserts,
      `COPY`, `RETURNING`, `ON CONFLICT`, lookups, range scans) — AC-009.
- [ ] Partition manager: sync `EnsureAll` at startup before serving; hourly loop;
      cutoff-aware 3-day create-ahead; drop-by-upper-bound incl. legacy, **never the
      DEFAULT**; windows from `Config.RetentionDays` + const 7; per-table failure
      isolation; benign-error WARN classification; INFO/WARN/ERROR slog — AC-010..AC-013,
      AC-017, AC-020, AC-021.
- [ ] The six in-scope `DELETE` purges removed from `runRetention` and their orphaned
      repo/interface/mock methods deleted; the five out-of-scope purges untouched;
      **ingest paths untouched** — AC-014, AC-015.
- [ ] `.down.sql` files present; swap-down performs the documented best-effort lossy
      reversal (incl. dropping the DEFAULT); non-reversibility + data-loss caveat
      documented — AC-018.
- [ ] Dynamic DDL uses `%I` for identifiers and `%L` for literals — SEC-006.
- [ ] No new dependency in `go.mod`; no new env var; migrations `000001`–`000023`
      untouched; `pg_partman`/extensions absent (BR-001).
- [ ] `make test` green (Go + frontend); no new lint errors.
- [ ] Rollout notes staged for the technical writer: data-derived `cutoff`,
      dirty-schema recovery (no manual SQL), DEFAULT-catch semantics + DEFAULT-row
      remediation, bloat-not-reclaimed, and the "at least the window" (BR-008)
      behavior change.
