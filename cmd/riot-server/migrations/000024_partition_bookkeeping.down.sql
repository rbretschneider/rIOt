-- PART-001 file 1/5 down: drop the NOT VALID bound checks and the bookkeeping
-- table. Safe regardless of how far the conversion progressed: if the swap
-- (000027) was reverted first, <t> has already been renamed back from
-- <t>_legacy and still carries the check constraint (renaming a table does
-- not rename its constraints); if the swap never happened, <t> is still the
-- plain table carrying the constraint directly.

ALTER TABLE heartbeats DROP CONSTRAINT IF EXISTS heartbeats_pbound;
ALTER TABLE telemetry_snapshots DROP CONSTRAINT IF EXISTS telemetry_snapshots_pbound;
ALTER TABLE device_logs DROP CONSTRAINT IF EXISTS device_logs_pbound;
ALTER TABLE container_metrics DROP CONSTRAINT IF EXISTS container_metrics_pbound;
ALTER TABLE container_logs DROP CONSTRAINT IF EXISTS container_logs_pbound;
ALTER TABLE probe_results DROP CONSTRAINT IF EXISTS probe_results_pbound;

DROP TABLE IF EXISTS partition_conversion;
