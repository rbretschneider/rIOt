-- CMD-LIFECYCLE-001 down: return the commands table to the vocabulary the
-- previous server release understands. The FR-008 reset of last_triggered_at
-- cannot be undone.

-- SEC-009: the previous release has no queue TTL, so a queued power action
-- would be delivered whenever the agent next heartbeats, possibly days later.
-- Fail every queued reboot/shutdown (and every queued os_update that may reboot)
-- regardless of expiry.
UPDATE commands
   SET status = 'error',
       result_msg = 'not delivered: power action dropped on downgrade',
       updated_at = NOW()
 WHERE status IN ('queued', 'pending')
   AND (action IN ('reboot', 'shutdown')
        OR (action = 'os_update' AND params->'include_reboot_class' = 'true'::jsonb));

-- Rows that already outlived their TTL must not become deliverable again.
UPDATE commands
   SET status = 'error',
       result_msg = 'expired: not delivered within TTL',
       updated_at = NOW()
 WHERE status IN ('queued', 'pending')
   AND expires_at <= NOW();

-- Canonical values back to legacy ones. cancelled and expired become error,
-- never queued: a row the operator cancelled or that expired must stay dead.
UPDATE commands SET status = 'sent'    WHERE status IN ('dispatched', 'acknowledged', 'running', 'verifying');
UPDATE commands SET status = 'success' WHERE status = 'succeeded';
UPDATE commands SET status = 'error'   WHERE status IN ('failed', 'timed_out', 'cancelled', 'expired');

DROP INDEX IF EXISTS idx_commands_inflight_deadline;
DROP INDEX IF EXISTS idx_commands_queued_expiry;
DROP INDEX IF EXISTS idx_commands_active_device;

ALTER TABLE commands
    DROP CONSTRAINT IF EXISTS commands_progress_check,
    DROP CONSTRAINT IF EXISTS commands_origin_check,
    DROP COLUMN IF EXISTS send_ambiguous,
    DROP COLUMN IF EXISTS pre_lifecycle,
    DROP COLUMN IF EXISTS target_keys,
    DROP COLUMN IF EXISTS policy_id,
    DROP COLUMN IF EXISTS origin,
    DROP COLUMN IF EXISTS stderr_tail,
    DROP COLUMN IF EXISTS failure_step,
    DROP COLUMN IF EXISTS progress,
    DROP COLUMN IF EXISTS step,
    DROP COLUMN IF EXISTS finished_at,
    DROP COLUMN IF EXISTS verifying_at,
    DROP COLUMN IF EXISTS started_at,
    DROP COLUMN IF EXISTS acknowledged_at,
    DROP COLUMN IF EXISTS dispatched_at,
    DROP COLUMN IF EXISTS deadline_at,
    DROP COLUMN IF EXISTS exec_deadline_s,
    DROP COLUMN IF EXISTS expires_at;

ALTER TABLE auto_update_policies
    DROP COLUMN IF EXISTS last_failure_at,
    DROP COLUMN IF EXISTS consecutive_failures;
