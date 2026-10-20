-- PART-001 file 5/5: add the outgoing foreign keys for the five FK'd tables
-- (container_logs has no FK — FR-004).
--
-- DEVIATION FROM THE ORIGINAL TWO-PHASE (NOT VALID + VALIDATE) DESIGN,
-- REQUIRED BY A POSTGRESQL 16 LIMITATION (verified empirically): PG16
-- rejects `ALTER TABLE <partitioned_table> ADD CONSTRAINT ... FOREIGN KEY
-- ... NOT VALID` outright —
--   "cannot add NOT VALID foreign key on partitioned table ... This feature
--   is not yet supported on partitioned tables."
-- (This restriction was lifted in PostgreSQL 17; the target here is PG16,
-- confirmed by A-001/D-005.) The originally-specified split (add NOT VALID
-- in file 000027, VALIDATE here in 000028) is therefore impossible for FK
-- constraints on the parent.
--
-- This file instead performs a single, PG16-compatible, VALIDATING
-- `ADD CONSTRAINT ... FOREIGN KEY` in its OWN transaction — critically, in a
-- SEPARATE file/transaction from the swap (000027), so the FK's validation
-- scan does NOT run while the swap's RENAME-induced ACCESS EXCLUSIVE lock is
-- still held (that would have been the actual NFR-001 violation). A
-- validating `ADD CONSTRAINT ... FOREIGN KEY` on its own takes
-- `SHARE ROW EXCLUSIVE` (blocks writes, allows reads) — never
-- `ACCESS EXCLUSIVE` — for the scan duration, exactly analogous in character
-- to file 000025's `SHARE` lock composite-index build. Since migrations run
-- pre-serving (before the server accepts traffic), this blocks nothing the
-- platform depends on, for the same reason files 000025/000026 are safe.
-- See the PART-001 implementation report "Deviations" section for the full
-- analysis.
--
-- Guarded: skip a table whose FK constraint already exists (idempotent;
-- converges to a no-op on any re-run once it has succeeded once).

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid
                    WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'heartbeats' AND con.conname = 'heartbeats_fk') THEN
        ALTER TABLE heartbeats ADD CONSTRAINT heartbeats_fk
            FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid
                    WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'telemetry_snapshots' AND con.conname = 'telemetry_snapshots_fk') THEN
        ALTER TABLE telemetry_snapshots ADD CONSTRAINT telemetry_snapshots_fk
            FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid
                    WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'device_logs' AND con.conname = 'device_logs_fk') THEN
        ALTER TABLE device_logs ADD CONSTRAINT device_logs_fk
            FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid
                    WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'container_metrics' AND con.conname = 'container_metrics_fk') THEN
        ALTER TABLE container_metrics ADD CONSTRAINT container_metrics_fk
            FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid
                    WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = 'probe_results' AND con.conname = 'probe_results_fk') THEN
        ALTER TABLE probe_results ADD CONSTRAINT probe_results_fk
            FOREIGN KEY (probe_id) REFERENCES probes(id) ON DELETE CASCADE;
    END IF;
END $$;
