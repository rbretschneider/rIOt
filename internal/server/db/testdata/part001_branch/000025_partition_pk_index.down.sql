-- PART-001 file 2/5 down: drop the composite PK-backing index. If the swap
-- (000027) was reverted first, its down migration already renamed
-- <t>_legacy_pk back to <t>_pk_new, so this always resolves to the right
-- name regardless of how far the conversion progressed.

DROP INDEX IF EXISTS heartbeats_pk_new;
DROP INDEX IF EXISTS telemetry_snapshots_pk_new;
DROP INDEX IF EXISTS device_logs_pk_new;
DROP INDEX IF EXISTS container_metrics_pk_new;
DROP INDEX IF EXISTS container_logs_pk_new;
DROP INDEX IF EXISTS probe_results_pk_new;
