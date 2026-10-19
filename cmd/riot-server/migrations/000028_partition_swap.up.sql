-- PART-001 file 4/5: the metadata swap. Per table: rename the populated heap
-- to <t>_legacy, create the partitioned parent in its place with the
-- composite PRIMARY KEY (id, <key>), attach <t>_legacy as a bounded
-- partition [MINVALUE, cutoff), attach a near-empty forward DEFAULT catch
-- partition, recreate every FR-003 index on the parent with the EXACT legacy
-- definition (so PostgreSQL adopts the pre-built child index instead of
-- rebuilding — AD-004), add outgoing FKs NOT VALID, and pre-create the
-- initial forward daily partitions from cutoffDay through today+3.
--
-- Everything in this file runs inside ONE implicit transaction (golang-migrate
-- sends the whole file as a single Exec). Every operation here is O(1)
-- metadata (ACCESS EXCLUSIVE only on the rename/create/attach/index-adopt
-- steps, never a rewrite) because the O(rows) prep (composite index build,
-- bound validation) already completed in files 000025/000026 — see
-- docs/architecture/PART-001-add.md AD-002, AD-004, §5, §11.
--
-- IMPORTANT — two implementation details required to make AD-004's "adopt,
-- don't rebuild" claim actually hold, verified empirically against a live
-- PostgreSQL 16 (not spelled out verbatim in the ADD's illustrative SQL):
--
--  1. The legacy table still carries its ORIGINAL single-column PRIMARY KEY
--     (<t>_pkey on id alone) after the rename — renaming a table does not
--     rename or drop its constraints, and PostgreSQL disallows two PRIMARY
--     KEY constraints on one table. The original PK must be dropped and the
--     pre-built composite unique index converted into the table's new
--     PRIMARY KEY via `ADD CONSTRAINT ... PRIMARY KEY USING INDEX` *before*
--     ATTACH — this is what makes ATTACH adopt that exact index as the
--     partition's local PK-backing index instead of building a new one
--     (confirmed via pg_relation_filenode: identical before/after with this
--     step; a plain composite index left unconverted is silently ignored
--     and PostgreSQL builds a brand-new local PK index under the swap's
--     ACCESS EXCLUSIVE, which is precisely the NFR-001 violation the split
--     migration exists to avoid).
--  2. Index/relation names are unique per SCHEMA in PostgreSQL, not per
--     table. The legacy child keeps its original lookup-index names (e.g.
--     `idx_container_logs_lookup`) after the rename, so the PARENT-level
--     `CREATE INDEX` for that exact name cannot be issued while the child
--     still holds it — `CREATE INDEX idx_x ON <parent> (...)` would fail
--     with "relation already exists". The legacy child's original indexes
--     are therefore renamed out of the way (`<t>_legacy_<suffix>`) BEFORE
--     the parent-level indexes are created with the canonical FR-003 names.
--     Adoption matches by index DEFINITION (columns/expression/opclass), not
--     by name, so the (now differently-named) legacy child index is still
--     linked as the parent index's partition — verified via pg_inherits and
--     an unchanged relfilenode. This is a cosmetic rename only: the legacy
--     partition's queries are unaffected by its index's name.
--
-- Both steps are necessary implementation detail to satisfy FR-002/AD-004's
-- intent, not a design change; see the PART-001 implementation report
-- "Deviations" section.
--
-- Guarded: skip entirely if the table has already been converted
-- (pg_partitioned_table), so this file converges to a no-op on any re-run
-- once it has succeeded once (NFR-003, FR-006, FR-007).

-- =====================================================================
-- heartbeats (key: timestamp, FK: device_id -> devices(id) ON DELETE CASCADE)
-- =====================================================================
DO $$
DECLARE
    v_cutoff timestamptz;
    v_day    date;
    v_today  date := (now() AT TIME ZONE 'UTC')::date;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'heartbeats') THEN
        RETURN;
    END IF;

    SELECT cutoff INTO v_cutoff FROM partition_conversion WHERE table_name = 'heartbeats';

    ALTER TABLE heartbeats RENAME TO heartbeats_legacy;

    ALTER INDEX heartbeats_pk_new RENAME TO heartbeats_legacy_pk;
    ALTER TABLE heartbeats_legacy DROP CONSTRAINT heartbeats_pkey;
    ALTER TABLE heartbeats_legacy ADD CONSTRAINT heartbeats_legacy_pk PRIMARY KEY USING INDEX heartbeats_legacy_pk;
    ALTER INDEX idx_heartbeats_device_time RENAME TO heartbeats_legacy_device_time;

    CREATE TABLE heartbeats (LIKE heartbeats_legacy INCLUDING DEFAULTS INCLUDING GENERATED,
        PRIMARY KEY (id, timestamp))
      PARTITION BY RANGE (timestamp);

    ALTER SEQUENCE heartbeats_id_seq OWNED BY heartbeats.id;
    ALTER TABLE heartbeats ALTER COLUMN id SET DEFAULT nextval('heartbeats_id_seq');

    EXECUTE format('ALTER TABLE %I ATTACH PARTITION %I FOR VALUES FROM (MINVALUE) TO (%L)',
                   'heartbeats', 'heartbeats_legacy', v_cutoff);

    CREATE TABLE heartbeats_default PARTITION OF heartbeats DEFAULT;

    CREATE INDEX idx_heartbeats_device_time ON heartbeats (device_id, timestamp DESC);

    -- Foreign key added in file 000028, not here: PostgreSQL 16 does not
    -- support adding a NOT VALID foreign key on a partitioned table (lifted
    -- in PG 17). Adding it here (validating) would run its O(rows)
    -- validation scan while this transaction still holds the RENAME's
    -- ACCESS EXCLUSIVE lock from above — exactly the NFR-001 violation the
    -- file split exists to avoid. See 000028 and the implementation report.

    FOR v_day IN SELECT generate_series(date_trunc('day', v_cutoff)::date, v_today + 3, '1 day')::date LOOP
        EXECUTE format('CREATE TABLE IF NOT EXISTS %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
                       'heartbeats_p' || to_char(v_day, 'YYYYMMDD'), 'heartbeats', v_day, v_day + 1);
    END LOOP;
END $$;

-- =====================================================================
-- telemetry_snapshots (key: timestamp, FK: device_id -> devices(id) ON DELETE CASCADE)
-- =====================================================================
DO $$
DECLARE
    v_cutoff timestamptz;
    v_day    date;
    v_today  date := (now() AT TIME ZONE 'UTC')::date;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'telemetry_snapshots') THEN
        RETURN;
    END IF;

    SELECT cutoff INTO v_cutoff FROM partition_conversion WHERE table_name = 'telemetry_snapshots';

    ALTER TABLE telemetry_snapshots RENAME TO telemetry_snapshots_legacy;

    ALTER INDEX telemetry_snapshots_pk_new RENAME TO telemetry_snapshots_legacy_pk;
    ALTER TABLE telemetry_snapshots_legacy DROP CONSTRAINT telemetry_snapshots_pkey;
    ALTER TABLE telemetry_snapshots_legacy ADD CONSTRAINT telemetry_snapshots_legacy_pk PRIMARY KEY USING INDEX telemetry_snapshots_legacy_pk;
    ALTER INDEX idx_telemetry_device_time RENAME TO telemetry_snapshots_legacy_device_time;

    CREATE TABLE telemetry_snapshots (LIKE telemetry_snapshots_legacy INCLUDING DEFAULTS INCLUDING GENERATED,
        PRIMARY KEY (id, timestamp))
      PARTITION BY RANGE (timestamp);

    ALTER SEQUENCE telemetry_snapshots_id_seq OWNED BY telemetry_snapshots.id;
    ALTER TABLE telemetry_snapshots ALTER COLUMN id SET DEFAULT nextval('telemetry_snapshots_id_seq');

    EXECUTE format('ALTER TABLE %I ATTACH PARTITION %I FOR VALUES FROM (MINVALUE) TO (%L)',
                   'telemetry_snapshots', 'telemetry_snapshots_legacy', v_cutoff);

    CREATE TABLE telemetry_snapshots_default PARTITION OF telemetry_snapshots DEFAULT;

    CREATE INDEX idx_telemetry_device_time ON telemetry_snapshots (device_id, timestamp DESC);

    -- Foreign key added in file 000028 — see the heartbeats block above for
    -- why (PG16 cannot add a NOT VALID FK on a partitioned table).

    FOR v_day IN SELECT generate_series(date_trunc('day', v_cutoff)::date, v_today + 3, '1 day')::date LOOP
        EXECUTE format('CREATE TABLE IF NOT EXISTS %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
                       'telemetry_snapshots_p' || to_char(v_day, 'YYYYMMDD'), 'telemetry_snapshots', v_day, v_day + 1);
    END LOOP;
END $$;

-- =====================================================================
-- device_logs (key: timestamp, FK: device_id -> devices(id) ON DELETE CASCADE,
-- UNIQUE dedup index idx_device_logs_dedup required by the ON CONFLICT insert
-- path — device_log_repo.go)
-- =====================================================================
DO $$
DECLARE
    v_cutoff timestamptz;
    v_day    date;
    v_today  date := (now() AT TIME ZONE 'UTC')::date;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'device_logs') THEN
        RETURN;
    END IF;

    SELECT cutoff INTO v_cutoff FROM partition_conversion WHERE table_name = 'device_logs';

    ALTER TABLE device_logs RENAME TO device_logs_legacy;

    ALTER INDEX device_logs_pk_new RENAME TO device_logs_legacy_pk;
    ALTER TABLE device_logs_legacy DROP CONSTRAINT device_logs_pkey;
    ALTER TABLE device_logs_legacy ADD CONSTRAINT device_logs_legacy_pk PRIMARY KEY USING INDEX device_logs_legacy_pk;
    ALTER INDEX idx_device_logs_device_ts RENAME TO device_logs_legacy_device_ts;
    ALTER INDEX idx_device_logs_priority RENAME TO device_logs_legacy_priority;
    ALTER INDEX idx_device_logs_dedup RENAME TO device_logs_legacy_dedup;

    CREATE TABLE device_logs (LIKE device_logs_legacy INCLUDING DEFAULTS INCLUDING GENERATED,
        PRIMARY KEY (id, timestamp))
      PARTITION BY RANGE (timestamp);

    ALTER SEQUENCE device_logs_id_seq OWNED BY device_logs.id;
    ALTER TABLE device_logs ALTER COLUMN id SET DEFAULT nextval('device_logs_id_seq');

    EXECUTE format('ALTER TABLE %I ATTACH PARTITION %I FOR VALUES FROM (MINVALUE) TO (%L)',
                   'device_logs', 'device_logs_legacy', v_cutoff);

    CREATE TABLE device_logs_default PARTITION OF device_logs DEFAULT;

    CREATE INDEX idx_device_logs_device_ts ON device_logs (device_id, timestamp DESC);
    CREATE INDEX idx_device_logs_priority ON device_logs (priority);
    CREATE UNIQUE INDEX idx_device_logs_dedup
        ON device_logs (device_id, timestamp, priority, unit, md5(message));

    -- Foreign key added in file 000028 — see the heartbeats block above for
    -- why (PG16 cannot add a NOT VALID FK on a partitioned table).

    FOR v_day IN SELECT generate_series(date_trunc('day', v_cutoff)::date, v_today + 3, '1 day')::date LOOP
        EXECUTE format('CREATE TABLE IF NOT EXISTS %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
                       'device_logs_p' || to_char(v_day, 'YYYYMMDD'), 'device_logs', v_day, v_day + 1);
    END LOOP;
END $$;

-- =====================================================================
-- container_metrics (key: timestamp, FK: device_id -> devices(id) ON DELETE CASCADE)
-- =====================================================================
DO $$
DECLARE
    v_cutoff timestamptz;
    v_day    date;
    v_today  date := (now() AT TIME ZONE 'UTC')::date;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'container_metrics') THEN
        RETURN;
    END IF;

    SELECT cutoff INTO v_cutoff FROM partition_conversion WHERE table_name = 'container_metrics';

    ALTER TABLE container_metrics RENAME TO container_metrics_legacy;

    ALTER INDEX container_metrics_pk_new RENAME TO container_metrics_legacy_pk;
    ALTER TABLE container_metrics_legacy DROP CONSTRAINT container_metrics_pkey;
    ALTER TABLE container_metrics_legacy ADD CONSTRAINT container_metrics_legacy_pk PRIMARY KEY USING INDEX container_metrics_legacy_pk;
    ALTER INDEX idx_container_metrics_lookup RENAME TO container_metrics_legacy_lookup;
    ALTER INDEX idx_container_metrics_time RENAME TO container_metrics_legacy_time;

    CREATE TABLE container_metrics (LIKE container_metrics_legacy INCLUDING DEFAULTS INCLUDING GENERATED,
        PRIMARY KEY (id, timestamp))
      PARTITION BY RANGE (timestamp);

    ALTER SEQUENCE container_metrics_id_seq OWNED BY container_metrics.id;
    ALTER TABLE container_metrics ALTER COLUMN id SET DEFAULT nextval('container_metrics_id_seq');

    EXECUTE format('ALTER TABLE %I ATTACH PARTITION %I FOR VALUES FROM (MINVALUE) TO (%L)',
                   'container_metrics', 'container_metrics_legacy', v_cutoff);

    CREATE TABLE container_metrics_default PARTITION OF container_metrics DEFAULT;

    CREATE INDEX idx_container_metrics_lookup ON container_metrics (device_id, container_name, timestamp DESC);
    CREATE INDEX idx_container_metrics_time ON container_metrics (timestamp);

    -- Foreign key added in file 000028 — see the heartbeats block above for
    -- why (PG16 cannot add a NOT VALID FK on a partitioned table).

    FOR v_day IN SELECT generate_series(date_trunc('day', v_cutoff)::date, v_today + 3, '1 day')::date LOOP
        EXECUTE format('CREATE TABLE IF NOT EXISTS %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
                       'container_metrics_p' || to_char(v_day, 'YYYYMMDD'), 'container_metrics', v_day, v_day + 1);
    END LOOP;
END $$;

-- =====================================================================
-- container_logs (key: timestamp, NO foreign key — must stay FK-free, FR-004/A-006)
-- =====================================================================
DO $$
DECLARE
    v_cutoff timestamptz;
    v_day    date;
    v_today  date := (now() AT TIME ZONE 'UTC')::date;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'container_logs') THEN
        RETURN;
    END IF;

    SELECT cutoff INTO v_cutoff FROM partition_conversion WHERE table_name = 'container_logs';

    ALTER TABLE container_logs RENAME TO container_logs_legacy;

    ALTER INDEX container_logs_pk_new RENAME TO container_logs_legacy_pk;
    ALTER TABLE container_logs_legacy DROP CONSTRAINT container_logs_pkey;
    ALTER TABLE container_logs_legacy ADD CONSTRAINT container_logs_legacy_pk PRIMARY KEY USING INDEX container_logs_legacy_pk;
    ALTER INDEX idx_container_logs_lookup RENAME TO container_logs_legacy_lookup;
    ALTER INDEX idx_container_logs_time RENAME TO container_logs_legacy_time;

    CREATE TABLE container_logs (LIKE container_logs_legacy INCLUDING DEFAULTS INCLUDING GENERATED,
        PRIMARY KEY (id, timestamp))
      PARTITION BY RANGE (timestamp);

    ALTER SEQUENCE container_logs_id_seq OWNED BY container_logs.id;
    ALTER TABLE container_logs ALTER COLUMN id SET DEFAULT nextval('container_logs_id_seq');

    EXECUTE format('ALTER TABLE %I ATTACH PARTITION %I FOR VALUES FROM (MINVALUE) TO (%L)',
                   'container_logs', 'container_logs_legacy', v_cutoff);

    CREATE TABLE container_logs_default PARTITION OF container_logs DEFAULT;

    CREATE INDEX idx_container_logs_lookup ON container_logs (device_id, container_id, timestamp DESC);
    CREATE INDEX idx_container_logs_time ON container_logs (timestamp);
    -- container_logs has no foreign key today and must not gain one (FR-004, A-006).

    FOR v_day IN SELECT generate_series(date_trunc('day', v_cutoff)::date, v_today + 3, '1 day')::date LOOP
        EXECUTE format('CREATE TABLE IF NOT EXISTS %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
                       'container_logs_p' || to_char(v_day, 'YYYYMMDD'), 'container_logs', v_day, v_day + 1);
    END LOOP;
END $$;

-- =====================================================================
-- probe_results (key: created_at, FK: probe_id -> probes(id) ON DELETE CASCADE)
-- =====================================================================
DO $$
DECLARE
    v_cutoff timestamptz;
    v_day    date;
    v_today  date := (now() AT TIME ZONE 'UTC')::date;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'probe_results') THEN
        RETURN;
    END IF;

    SELECT cutoff INTO v_cutoff FROM partition_conversion WHERE table_name = 'probe_results';

    ALTER TABLE probe_results RENAME TO probe_results_legacy;

    ALTER INDEX probe_results_pk_new RENAME TO probe_results_legacy_pk;
    ALTER TABLE probe_results_legacy DROP CONSTRAINT probe_results_pkey;
    ALTER TABLE probe_results_legacy ADD CONSTRAINT probe_results_legacy_pk PRIMARY KEY USING INDEX probe_results_legacy_pk;
    ALTER INDEX idx_probe_results_probe_time RENAME TO probe_results_legacy_probe_time;

    CREATE TABLE probe_results (LIKE probe_results_legacy INCLUDING DEFAULTS INCLUDING GENERATED,
        PRIMARY KEY (id, created_at))
      PARTITION BY RANGE (created_at);

    ALTER SEQUENCE probe_results_id_seq OWNED BY probe_results.id;
    ALTER TABLE probe_results ALTER COLUMN id SET DEFAULT nextval('probe_results_id_seq');

    EXECUTE format('ALTER TABLE %I ATTACH PARTITION %I FOR VALUES FROM (MINVALUE) TO (%L)',
                   'probe_results', 'probe_results_legacy', v_cutoff);

    CREATE TABLE probe_results_default PARTITION OF probe_results DEFAULT;

    CREATE INDEX idx_probe_results_probe_time ON probe_results (probe_id, created_at DESC);

    -- Foreign key added in file 000028 — see the heartbeats block above for
    -- why (PG16 cannot add a NOT VALID FK on a partitioned table).

    FOR v_day IN SELECT generate_series(date_trunc('day', v_cutoff)::date, v_today + 3, '1 day')::date LOOP
        EXECUTE format('CREATE TABLE IF NOT EXISTS %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
                       'probe_results_p' || to_char(v_day, 'YYYYMMDD'), 'probe_results', v_day, v_day + 1);
    END LOOP;
END $$;
