-- EXPECT-008: installer reinstall records.
--
-- A reinstall record is one commands row with origin 'installer', created
-- directly in 'running' by the server when an installer's authenticated notice
-- is accepted (never queued, so it can never be claimed, sent, expired, reverted
-- or cancelled). The shape CHECK makes that, the 10-minute bound, "no params"
-- and "holds no target keys" database invariants.
--
-- Bounds (AD-012): at most one running record per device (unique index), at most
-- 6 created per device per rolling 24 h (enforced by the server, counted with
-- idx_commands_reinstall_recent), removed with the command retention.
-- There are no existing installer rows, so the new CHECK validates instantly.

ALTER TABLE commands DROP CONSTRAINT commands_origin_check;
ALTER TABLE commands ADD CONSTRAINT commands_origin_check
  CHECK (origin IN ('user','auto_update','auto_patch','agent_self_update','installer'));

ALTER TABLE commands ADD CONSTRAINT commands_reinstall_shape CHECK (
  origin <> 'installer' OR (
    action = 'agent_update'
    AND status IN ('running','succeeded','timed_out')
    AND dispatched_at IS NOT NULL
    AND deadline_at IS NOT NULL
    AND deadline_at <= dispatched_at + INTERVAL '10 minutes'
    AND exec_deadline_s <= 600
    AND cardinality(target_keys) = 0
    AND params = '{}'::jsonb));

CREATE UNIQUE INDEX idx_commands_reinstall_open ON commands (device_id)
  WHERE origin = 'installer' AND status = 'running';

CREATE INDEX idx_commands_reinstall_recent ON commands (device_id, created_at)
  WHERE origin = 'installer';
