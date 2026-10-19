-- Bound dead-tuple bloat on the high-churn retention tables.
--
-- The retention purge DELETEs old rows continuously, but Postgres's default
-- autovacuum only fires at autovacuum_vacuum_scale_factor = 0.2 (20% of the
-- table dead). On large, constantly-churning tables that threshold is rarely
-- reached, so dead tuples are never reclaimed and the table files grow
-- unbounded (observed: container_logs at 28 GB with ~844 MB live, ~97% bloat).
--
-- Per-table tuning makes autovacuum fire on a small absolute threshold instead,
-- keeping bloat at a low steady state. This bounds *growth* (space is reused
-- within the table); it does not shrink files already bloated — that needs a
-- one-time VACUUM FULL / pg_repack, done operationally, not in a migration.
--
-- Long term the proper fix is time-range partitioning with drop-partition
-- retention (see docs/ops/postgres-bloat.md); this migration is the low-risk
-- interim that needs no application change.

-- Highest churn (logs, telemetry, heartbeats): fire at ~2% dead or 5000 rows.
DO $$ BEGIN
IF NOT EXISTS (SELECT 1 FROM pg_class WHERE oid='container_logs'::regclass AND relkind='p') THEN
ALTER TABLE container_logs
    SET (autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 5000);
END IF;
IF NOT EXISTS (SELECT 1 FROM pg_class WHERE oid='telemetry_snapshots'::regclass AND relkind='p') THEN
ALTER TABLE telemetry_snapshots
    SET (autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 5000);
END IF;
IF NOT EXISTS (SELECT 1 FROM pg_class WHERE oid='device_logs'::regclass AND relkind='p') THEN
ALTER TABLE device_logs
    SET (autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 5000);
END IF;
IF NOT EXISTS (SELECT 1 FROM pg_class WHERE oid='heartbeats'::regclass AND relkind='p') THEN
ALTER TABLE heartbeats
    SET (autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 5000);
END IF;

-- Moderate churn (per-container metrics, probe results): fire at ~5% or 5000.
IF NOT EXISTS (SELECT 1 FROM pg_class WHERE oid='container_metrics'::regclass AND relkind='p') THEN
ALTER TABLE container_metrics
    SET (autovacuum_vacuum_scale_factor = 0.05, autovacuum_vacuum_threshold = 5000);
END IF;
IF NOT EXISTS (SELECT 1 FROM pg_class WHERE oid='probe_results'::regclass AND relkind='p') THEN
ALTER TABLE probe_results
    SET (autovacuum_vacuum_scale_factor = 0.05, autovacuum_vacuum_threshold = 5000);
END IF;
END $$;
