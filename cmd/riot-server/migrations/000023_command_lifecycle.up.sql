-- CMD-LIFECYCLE-001: canonical command lifecycle, TTL/deadlines, target keys,
-- and auto-update failure backoff.
--
-- Legacy statuses are rewritten in place (pending->queued, sent->dispatched,
-- success->succeeded, error->failed). The server also maps legacy values at
-- read time, so a row written by an older server during a rollback window is
-- still readable.

-- 1. Columns on commands. expires_at and exec_deadline_s are backfilled below
--    and made NOT NULL afterwards.
ALTER TABLE commands
    ADD COLUMN expires_at      TIMESTAMPTZ,
    ADD COLUMN exec_deadline_s INTEGER,
    ADD COLUMN deadline_at     TIMESTAMPTZ,
    ADD COLUMN dispatched_at   TIMESTAMPTZ,
    ADD COLUMN acknowledged_at TIMESTAMPTZ,
    ADD COLUMN started_at      TIMESTAMPTZ,
    ADD COLUMN verifying_at    TIMESTAMPTZ,
    ADD COLUMN finished_at     TIMESTAMPTZ,
    ADD COLUMN step            TEXT NOT NULL DEFAULT '',
    ADD COLUMN progress        SMALLINT,
    ADD COLUMN failure_step    TEXT NOT NULL DEFAULT '',
    ADD COLUMN stderr_tail     TEXT NOT NULL DEFAULT '',
    ADD COLUMN origin          TEXT NOT NULL DEFAULT 'user',
    ADD COLUMN policy_id       INTEGER REFERENCES auto_update_policies(id) ON DELETE SET NULL,
    ADD COLUMN target_keys     TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN pre_lifecycle   BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN send_ambiguous  BOOLEAN NOT NULL DEFAULT false;

-- SEC-010: every row that exists now is a pre-lifecycle row. The default is
-- flipped immediately, so rows inserted from here on are not (and the table is
-- not rewritten).
ALTER TABLE commands ALTER COLUMN pre_lifecycle SET DEFAULT false;

ALTER TABLE commands
    ADD CONSTRAINT commands_origin_check CHECK (origin IN ('user', 'auto_update', 'auto_patch')),
    ADD CONSTRAINT commands_progress_check CHECK (progress IS NULL OR (progress >= 0 AND progress <= 100));

-- 2. Execution deadline per action class (seconds). docker_bulk_update is
--    min(35m * N + 5m, 6h) where N is the number of container_ids.
UPDATE commands SET exec_deadline_s = CASE action
    WHEN 'reboot' THEN 600
    WHEN 'shutdown' THEN 600
    WHEN 'os_update' THEN 2400
    WHEN 'enable_auto_updates' THEN 2400
    WHEN 'docker_update' THEN 2100
    WHEN 'docker_bulk_update' THEN LEAST(
        2100 * GREATEST(COALESCE(
            CASE WHEN jsonb_typeof(params->'container_ids') = 'array'
                 THEN jsonb_array_length(params->'container_ids') END, 1), 1) + 300,
        21600)
    ELSE 900
END;

-- 3. Queued TTL per action class.
UPDATE commands SET expires_at = created_at + CASE action
    WHEN 'reboot' THEN INTERVAL '15 minutes'
    WHEN 'shutdown' THEN INTERVAL '15 minutes'
    WHEN 'os_update' THEN INTERVAL '30 minutes'
    WHEN 'enable_auto_updates' THEN INTERVAL '30 minutes'
    WHEN 'docker_update' THEN INTERVAL '60 minutes'
    WHEN 'docker_bulk_update' THEN INTERVAL '60 minutes'
    WHEN 'agent_update' THEN INTERVAL '60 minutes'
    ELSE INTERVAL '30 minutes'
END;

-- 4. Rows that were sent get a dispatch time and a deadline derived from their
--    last update. Historical rows whose result was lost are therefore overdue
--    and are relabelled timed_out by the first sweep (D-010).
UPDATE commands
   SET dispatched_at = updated_at,
       deadline_at   = updated_at + (exec_deadline_s * INTERVAL '1 second')
 WHERE status = 'sent';

-- 5. Terminal rows get their finish time from the last update.
UPDATE commands SET finished_at = updated_at WHERE status IN ('success', 'error');

-- 6. Best-effort origin from the result_msg prefixes the old dispatchers wrote.
UPDATE commands SET origin = 'auto_update' WHERE result_msg LIKE 'auto-update%';
UPDATE commands SET origin = 'auto_patch'  WHERE result_msg LIKE 'auto-patch%';

-- 7. Best-effort target keys for rows that are still active.
UPDATE commands SET target_keys = ARRAY['host']
 WHERE status IN ('pending', 'queued', 'sent')
   AND action IN ('reboot', 'shutdown', 'os_update', 'enable_auto_updates', 'agent_update');

UPDATE commands SET target_keys = ARRAY[CASE
        WHEN COALESCE(params->>'compose_work_dir', '') <> ''
            THEN 'docker:compose:' || rtrim(params->>'compose_work_dir', '/')
        WHEN COALESCE(params->>'container', '') <> ''
            THEN 'docker:container:' || ltrim(params->>'container', '/')
        WHEN COALESCE(params->>'container_id', '') <> ''
            THEN 'docker:id:' || (params->>'container_id')
    END]
 WHERE status IN ('pending', 'queued', 'sent')
   AND action = 'docker_update'
   AND (COALESCE(params->>'compose_work_dir', '') <> ''
        OR COALESCE(params->>'container', '') <> ''
        OR COALESCE(params->>'container_id', '') <> '');

-- 8. Map the legacy status vocabulary to the canonical one.
UPDATE commands SET status = 'queued'     WHERE status = 'pending';
UPDATE commands SET status = 'dispatched' WHERE status = 'sent';
UPDATE commands SET status = 'succeeded'  WHERE status = 'success';
UPDATE commands SET status = 'failed'     WHERE status = 'error';

ALTER TABLE commands ALTER COLUMN expires_at SET NOT NULL;
ALTER TABLE commands ALTER COLUMN exec_deadline_s SET NOT NULL;

-- 9. Partial indexes for the three hot paths. The status lists keep the legacy
--    aliases so the planner can still use them for rows written by an older
--    server during a rollback window.
CREATE INDEX idx_commands_active_device ON commands (device_id, created_at)
    WHERE status IN ('queued', 'pending', 'dispatched', 'sent', 'acknowledged', 'running', 'verifying');
CREATE INDEX idx_commands_queued_expiry ON commands (expires_at)
    WHERE status IN ('queued', 'pending');
CREATE INDEX idx_commands_inflight_deadline ON commands (deadline_at)
    WHERE status IN ('dispatched', 'sent', 'acknowledged', 'running', 'verifying');

-- 10. Auto-update failure backoff (AD-007).
ALTER TABLE auto_update_policies
    ADD COLUMN consecutive_failures INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN last_failure_at      TIMESTAMPTZ;

-- 11. FR-008: every container-level policy has only ever failed (the agent
--     required container_id while the server sent a name), so its last-triggered
--     time came from a failed run. Clear it. Stack policies are unchanged.
UPDATE auto_update_policies SET last_triggered_at = NULL WHERE is_stack = false;
