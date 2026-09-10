-- PART-001 file 5/5 down: drop the foreign key constraints added in file
-- 000027 for the five FK'd tables. Guarded with IF EXISTS so this is safe
-- to run regardless of whether the swap has already been reverted (in which
-- case DROP TABLE in 000027's down already removed the constraint along
-- with the parent).

ALTER TABLE IF EXISTS heartbeats DROP CONSTRAINT IF EXISTS heartbeats_fk;
ALTER TABLE IF EXISTS telemetry_snapshots DROP CONSTRAINT IF EXISTS telemetry_snapshots_fk;
ALTER TABLE IF EXISTS device_logs DROP CONSTRAINT IF EXISTS device_logs_fk;
ALTER TABLE IF EXISTS container_metrics DROP CONSTRAINT IF EXISTS container_metrics_fk;
ALTER TABLE IF EXISTS probe_results DROP CONSTRAINT IF EXISTS probe_results_fk;
