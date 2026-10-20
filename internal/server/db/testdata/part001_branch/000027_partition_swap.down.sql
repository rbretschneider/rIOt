-- PART-001 file 4/5 down: best-effort, DATA-LOSING reversal (FR-019/AD-007).
--
-- This is NOT a full, lossless downgrade. It is intended for dev/test
-- rollback shortly after conversion, not routine production use:
--
--   * The pre-existing (legacy) rows ARE preserved: the legacy partition is
--     detached before the parent is dropped, then renamed back to its
--     original table name with its original single-column primary key and
--     original index names restored.
--   * Any rows written into the forward daily partitions OR the DEFAULT
--     catch partition AFTER conversion are DISCARDED — dropping the
--     partitioned parent drops every partition still attached to it
--     (forward daily partitions and the DEFAULT), and that data is not
--     migrated anywhere.
--
-- A production downgrade with meaningful post-conversion data requires a
-- manual data-merge runbook; this migration does not attempt one.
--
-- Guarded: skip entirely if the table is not currently partitioned (nothing
-- to revert — either the up migration never ran, or a prior down already
-- reverted it).

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_partitioned_table pt
                    JOIN pg_class c ON c.oid = pt.partrelid
                    WHERE c.relname = 'heartbeats') THEN
        RETURN;
    END IF;

    ALTER TABLE heartbeats DETACH PARTITION heartbeats_legacy;
    -- Re-point the owned sequence at the surviving legacy relation BEFORE
    -- dropping the parent — an "OWNED BY" sequence is dropped along with
    -- its owning column's table, which would otherwise destroy
    -- heartbeats_id_seq when the parent is dropped below.
    ALTER SEQUENCE heartbeats_id_seq OWNED BY heartbeats_legacy.id;
    DROP TABLE heartbeats; -- drops the parent and every still-attached partition (forward daily + DEFAULT)

    -- Drop the composite PK constraint (also drops its backing index) and
    -- restore the original single-column PK.
    ALTER TABLE heartbeats_legacy DROP CONSTRAINT heartbeats_legacy_pk;
    ALTER TABLE heartbeats_legacy RENAME TO heartbeats;
    ALTER TABLE heartbeats ADD CONSTRAINT heartbeats_pkey PRIMARY KEY (id);

    ALTER INDEX heartbeats_legacy_device_time RENAME TO idx_heartbeats_device_time;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_partitioned_table pt
                    JOIN pg_class c ON c.oid = pt.partrelid
                    WHERE c.relname = 'telemetry_snapshots') THEN
        RETURN;
    END IF;

    ALTER TABLE telemetry_snapshots DETACH PARTITION telemetry_snapshots_legacy;
    ALTER SEQUENCE telemetry_snapshots_id_seq OWNED BY telemetry_snapshots_legacy.id;
    DROP TABLE telemetry_snapshots;

    ALTER TABLE telemetry_snapshots_legacy DROP CONSTRAINT telemetry_snapshots_legacy_pk;
    ALTER TABLE telemetry_snapshots_legacy RENAME TO telemetry_snapshots;
    ALTER TABLE telemetry_snapshots ADD CONSTRAINT telemetry_snapshots_pkey PRIMARY KEY (id);

    ALTER INDEX telemetry_snapshots_legacy_device_time RENAME TO idx_telemetry_device_time;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_partitioned_table pt
                    JOIN pg_class c ON c.oid = pt.partrelid
                    WHERE c.relname = 'device_logs') THEN
        RETURN;
    END IF;

    ALTER TABLE device_logs DETACH PARTITION device_logs_legacy;
    ALTER SEQUENCE device_logs_id_seq OWNED BY device_logs_legacy.id;
    DROP TABLE device_logs;

    ALTER TABLE device_logs_legacy DROP CONSTRAINT device_logs_legacy_pk;
    ALTER TABLE device_logs_legacy RENAME TO device_logs;
    ALTER TABLE device_logs ADD CONSTRAINT device_logs_pkey PRIMARY KEY (id);

    ALTER INDEX device_logs_legacy_device_ts RENAME TO idx_device_logs_device_ts;
    ALTER INDEX device_logs_legacy_priority RENAME TO idx_device_logs_priority;
    ALTER INDEX device_logs_legacy_dedup RENAME TO idx_device_logs_dedup;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_partitioned_table pt
                    JOIN pg_class c ON c.oid = pt.partrelid
                    WHERE c.relname = 'container_metrics') THEN
        RETURN;
    END IF;

    ALTER TABLE container_metrics DETACH PARTITION container_metrics_legacy;
    ALTER SEQUENCE container_metrics_id_seq OWNED BY container_metrics_legacy.id;
    DROP TABLE container_metrics;

    ALTER TABLE container_metrics_legacy DROP CONSTRAINT container_metrics_legacy_pk;
    ALTER TABLE container_metrics_legacy RENAME TO container_metrics;
    ALTER TABLE container_metrics ADD CONSTRAINT container_metrics_pkey PRIMARY KEY (id);

    ALTER INDEX container_metrics_legacy_lookup RENAME TO idx_container_metrics_lookup;
    ALTER INDEX container_metrics_legacy_time RENAME TO idx_container_metrics_time;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_partitioned_table pt
                    JOIN pg_class c ON c.oid = pt.partrelid
                    WHERE c.relname = 'container_logs') THEN
        RETURN;
    END IF;

    ALTER TABLE container_logs DETACH PARTITION container_logs_legacy;
    ALTER SEQUENCE container_logs_id_seq OWNED BY container_logs_legacy.id;
    DROP TABLE container_logs;

    ALTER TABLE container_logs_legacy DROP CONSTRAINT container_logs_legacy_pk;
    ALTER TABLE container_logs_legacy RENAME TO container_logs;
    ALTER TABLE container_logs ADD CONSTRAINT container_logs_pkey PRIMARY KEY (id);

    ALTER INDEX container_logs_legacy_lookup RENAME TO idx_container_logs_lookup;
    ALTER INDEX container_logs_legacy_time RENAME TO idx_container_logs_time;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_partitioned_table pt
                    JOIN pg_class c ON c.oid = pt.partrelid
                    WHERE c.relname = 'probe_results') THEN
        RETURN;
    END IF;

    ALTER TABLE probe_results DETACH PARTITION probe_results_legacy;
    ALTER SEQUENCE probe_results_id_seq OWNED BY probe_results_legacy.id;
    DROP TABLE probe_results;

    ALTER TABLE probe_results_legacy DROP CONSTRAINT probe_results_legacy_pk;
    ALTER TABLE probe_results_legacy RENAME TO probe_results;
    ALTER TABLE probe_results ADD CONSTRAINT probe_results_pkey PRIMARY KEY (id);

    ALTER INDEX probe_results_legacy_probe_time RENAME TO idx_probe_results_probe_time;
END $$;
