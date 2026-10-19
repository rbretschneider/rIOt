-- PART-001 file 2/5: pre-build the composite (id, <key>) unique index that
-- will back the partitioned parent's primary key at ATTACH time (file
-- 000027), so PostgreSQL ADOPTS this already-built index instead of
-- rebuilding one under the swap's ACCESS EXCLUSIVE lock (AD-004).
--
-- CREATE UNIQUE INDEX (non-CONCURRENTLY) takes SHARE — it blocks writers but
-- not readers — and is O(rows). This is intentionally NOT CONCURRENTLY:
-- CONCURRENTLY cannot run inside golang-migrate's implicit per-file
-- transaction, and it is unnecessary here because migrations run before the
-- server serves traffic (server.go: RunMigrations precedes router/worker
-- startup) — a SHARE lock during a pre-serving boot blocks nothing (see
-- docs/architecture/PART-001-add.md AD-002, §11 rolling-deploy note).
--
-- Guarded: skip entirely if the table has already been converted to a
-- partitioned parent (in which case "<t>" no longer refers to the
-- pre-swap heap and this index has already been built and renamed by file
-- 000027). In normal operation golang-migrate's version tracking makes this
-- guard unreachable in practice, but it keeps the file safe to re-run.

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'heartbeats') THEN
        RETURN;
    END IF;
    CREATE UNIQUE INDEX IF NOT EXISTS heartbeats_pk_new ON heartbeats (id, timestamp);
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'telemetry_snapshots') THEN
        RETURN;
    END IF;
    CREATE UNIQUE INDEX IF NOT EXISTS telemetry_snapshots_pk_new ON telemetry_snapshots (id, timestamp);
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'device_logs') THEN
        RETURN;
    END IF;
    CREATE UNIQUE INDEX IF NOT EXISTS device_logs_pk_new ON device_logs (id, timestamp);
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'container_metrics') THEN
        RETURN;
    END IF;
    CREATE UNIQUE INDEX IF NOT EXISTS container_metrics_pk_new ON container_metrics (id, timestamp);
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'container_logs') THEN
        RETURN;
    END IF;
    CREATE UNIQUE INDEX IF NOT EXISTS container_logs_pk_new ON container_logs (id, timestamp);
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_partitioned_table pt
               JOIN pg_class c ON c.oid = pt.partrelid
               WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'probe_results') THEN
        RETURN;
    END IF;
    CREATE UNIQUE INDEX IF NOT EXISTS probe_results_pk_new ON probe_results (id, created_at);
END $$;
