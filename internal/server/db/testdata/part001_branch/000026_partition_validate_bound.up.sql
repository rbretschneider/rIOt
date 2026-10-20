-- PART-001 file 3/5: validate the NOT VALID bound check added in file
-- 000024. Runs under SHARE UPDATE EXCLUSIVE (non-blocking — reads and writes
-- both proceed), and is O(rows): it scans the table once to confirm no row
-- has <key> >= cutoff.
--
-- This CANNOT fail: cutoff was derived in file 000024 as strictly greater
-- than max(<key>) as observed at that time (AD-003), and RunMigrations runs
-- before Server.Start serves traffic (server.go: migrations precede the
-- router/worker startup) — no concurrent writer can insert a row between the
-- file 000024 compute and this validate in the single-instance target
-- (documented rolling-deploy caveat: docs/architecture/PART-001-add.md §11,
-- residual R5 of the security re-review — a rolling deploy where an old
-- instance is still writing could theoretically race this window; recovery
-- is `migrate force` + reboot, which self-heals via file 000024's
-- recompute).
--
-- Guarded: skip if the table has already been converted (the constraint no
-- longer exists under this name once the parent takes over "<t>").

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relname = 'heartbeats') THEN
        RETURN;
    END IF;
    ALTER TABLE heartbeats VALIDATE CONSTRAINT heartbeats_pbound;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relname = 'telemetry_snapshots') THEN
        RETURN;
    END IF;
    ALTER TABLE telemetry_snapshots VALIDATE CONSTRAINT telemetry_snapshots_pbound;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relname = 'device_logs') THEN
        RETURN;
    END IF;
    ALTER TABLE device_logs VALIDATE CONSTRAINT device_logs_pbound;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relname = 'container_metrics') THEN
        RETURN;
    END IF;
    ALTER TABLE container_metrics VALIDATE CONSTRAINT container_metrics_pbound;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relname = 'container_logs') THEN
        RETURN;
    END IF;
    ALTER TABLE container_logs VALIDATE CONSTRAINT container_logs_pbound;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relname = 'probe_results') THEN
        RETURN;
    END IF;
    ALTER TABLE probe_results VALIDATE CONSTRAINT probe_results_pbound;
END $$;
