-- PART-001 file 1/5: partition bookkeeping table + per-table derived cutoff +
-- NOT VALID bound check.
--
-- The legacy partition's upper bound ("cutoff") is DERIVED FROM THE DATA
-- (strictly greater than the table's own max(<key>)), not a fixed offset from
-- wall-clock now. This guarantees, by construction, that file 000026's
-- VALIDATE CONSTRAINT (which scans for any row with <key> >= cutoff) cannot
-- fail — see docs/architecture/PART-001-add.md AD-003.
--
-- This file only takes ACCESS EXCLUSIVE on the small partition_conversion
-- catalog row and on each table's constraint list (O(1) metadata); the
-- max(<key>) read itself is a single aggregate scan but holds no exclusive
-- lock while it runs.
--
-- Idempotent and self-healing (NFR-003): a table already converted
-- (pg_partitioned_table has an entry for it) is left untouched — its
-- persisted cutoff is the historical record the legacy partition was
-- actually attached under and must not be disturbed. A table not yet
-- converted (fresh install, or a dirty pre-swap re-run after `migrate force`)
-- always recomputes cutoff from the CURRENT live max(<key>) and re-adds the
-- NOT VALID check to match — this is what fixes the "sticky stale cutoff"
-- recovery bug from the pre-review ADD (SEC-PART-001-001).

CREATE TABLE IF NOT EXISTS partition_conversion (
    table_name  TEXT PRIMARY KEY,
    key_column  TEXT NOT NULL,
    cutoff      TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- heartbeats (key: timestamp)
DO $$
DECLARE v_cutoff timestamptz;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relname = 'heartbeats') THEN
        RETURN;
    END IF;

    SELECT date_trunc('day', GREATEST(now(), (SELECT max(timestamp) FROM heartbeats)))
           + INTERVAL '1 day'
      INTO v_cutoff;

    INSERT INTO partition_conversion(table_name, key_column, cutoff)
         VALUES ('heartbeats', 'timestamp', v_cutoff)
    ON CONFLICT (table_name) DO UPDATE
         SET cutoff = EXCLUDED.cutoff, key_column = EXCLUDED.key_column;

    ALTER TABLE heartbeats DROP CONSTRAINT IF EXISTS heartbeats_pbound;
    EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (%I < %L) NOT VALID',
                   'heartbeats', 'heartbeats_pbound', 'timestamp', v_cutoff);
END $$;

-- telemetry_snapshots (key: timestamp)
DO $$
DECLARE v_cutoff timestamptz;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relname = 'telemetry_snapshots') THEN
        RETURN;
    END IF;

    SELECT date_trunc('day', GREATEST(now(), (SELECT max(timestamp) FROM telemetry_snapshots)))
           + INTERVAL '1 day'
      INTO v_cutoff;

    INSERT INTO partition_conversion(table_name, key_column, cutoff)
         VALUES ('telemetry_snapshots', 'timestamp', v_cutoff)
    ON CONFLICT (table_name) DO UPDATE
         SET cutoff = EXCLUDED.cutoff, key_column = EXCLUDED.key_column;

    ALTER TABLE telemetry_snapshots DROP CONSTRAINT IF EXISTS telemetry_snapshots_pbound;
    EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (%I < %L) NOT VALID',
                   'telemetry_snapshots', 'telemetry_snapshots_pbound', 'timestamp', v_cutoff);
END $$;

-- device_logs (key: timestamp)
DO $$
DECLARE v_cutoff timestamptz;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relname = 'device_logs') THEN
        RETURN;
    END IF;

    SELECT date_trunc('day', GREATEST(now(), (SELECT max(timestamp) FROM device_logs)))
           + INTERVAL '1 day'
      INTO v_cutoff;

    INSERT INTO partition_conversion(table_name, key_column, cutoff)
         VALUES ('device_logs', 'timestamp', v_cutoff)
    ON CONFLICT (table_name) DO UPDATE
         SET cutoff = EXCLUDED.cutoff, key_column = EXCLUDED.key_column;

    ALTER TABLE device_logs DROP CONSTRAINT IF EXISTS device_logs_pbound;
    EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (%I < %L) NOT VALID',
                   'device_logs', 'device_logs_pbound', 'timestamp', v_cutoff);
END $$;

-- container_metrics (key: timestamp)
DO $$
DECLARE v_cutoff timestamptz;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relname = 'container_metrics') THEN
        RETURN;
    END IF;

    SELECT date_trunc('day', GREATEST(now(), (SELECT max(timestamp) FROM container_metrics)))
           + INTERVAL '1 day'
      INTO v_cutoff;

    INSERT INTO partition_conversion(table_name, key_column, cutoff)
         VALUES ('container_metrics', 'timestamp', v_cutoff)
    ON CONFLICT (table_name) DO UPDATE
         SET cutoff = EXCLUDED.cutoff, key_column = EXCLUDED.key_column;

    ALTER TABLE container_metrics DROP CONSTRAINT IF EXISTS container_metrics_pbound;
    EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (%I < %L) NOT VALID',
                   'container_metrics', 'container_metrics_pbound', 'timestamp', v_cutoff);
END $$;

-- container_logs (key: timestamp)
DO $$
DECLARE v_cutoff timestamptz;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relname = 'container_logs') THEN
        RETURN;
    END IF;

    SELECT date_trunc('day', GREATEST(now(), (SELECT max(timestamp) FROM container_logs)))
           + INTERVAL '1 day'
      INTO v_cutoff;

    INSERT INTO partition_conversion(table_name, key_column, cutoff)
         VALUES ('container_logs', 'timestamp', v_cutoff)
    ON CONFLICT (table_name) DO UPDATE
         SET cutoff = EXCLUDED.cutoff, key_column = EXCLUDED.key_column;

    ALTER TABLE container_logs DROP CONSTRAINT IF EXISTS container_logs_pbound;
    EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (%I < %L) NOT VALID',
                   'container_logs', 'container_logs_pbound', 'timestamp', v_cutoff);
END $$;

-- probe_results (key: created_at — no timestamp column)
DO $$
DECLARE v_cutoff timestamptz;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relname = 'probe_results') THEN
        RETURN;
    END IF;

    SELECT date_trunc('day', GREATEST(now(), (SELECT max(created_at) FROM probe_results)))
           + INTERVAL '1 day'
      INTO v_cutoff;

    INSERT INTO partition_conversion(table_name, key_column, cutoff)
         VALUES ('probe_results', 'created_at', v_cutoff)
    ON CONFLICT (table_name) DO UPDATE
         SET cutoff = EXCLUDED.cutoff, key_column = EXCLUDED.key_column;

    ALTER TABLE probe_results DROP CONSTRAINT IF EXISTS probe_results_pbound;
    EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (%I < %L) NOT VALID',
                   'probe_results', 'probe_results_pbound', 'created_at', v_cutoff);
END $$;
