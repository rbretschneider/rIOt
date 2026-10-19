-- Restore the cluster-default autovacuum behavior on these tables.
ALTER TABLE container_logs
    RESET (autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold);
ALTER TABLE telemetry_snapshots
    RESET (autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold);
ALTER TABLE device_logs
    RESET (autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold);
ALTER TABLE heartbeats
    RESET (autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold);
ALTER TABLE container_metrics
    RESET (autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold);
ALTER TABLE probe_results
    RESET (autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold);
