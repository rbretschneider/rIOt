-- EXPECT-002: agent self-update records.
--
-- A self-update is one commands row with origin 'agent_self_update', created
-- directly in 'running' by the server (never queued, so it can never be
-- claimed, sent, expired, reverted or cancelled). The shape CHECK makes that,
-- the 10-minute bound and "holds no target keys" database invariants.
--
-- Bounds (AD-013): at most one non-terminal record per device (unique index),
-- at most 6 created per device per rolling 24 h (enforced by the server, counted
-- with idx_commands_self_update_recent), removed with the command retention.
-- There are no existing self-update rows, so the new CHECK validates instantly.

ALTER TABLE commands DROP CONSTRAINT commands_origin_check;
ALTER TABLE commands ADD CONSTRAINT commands_origin_check
  CHECK (origin IN ('user','auto_update','auto_patch','agent_self_update'));

ALTER TABLE commands ADD CONSTRAINT commands_self_update_shape CHECK (
  origin <> 'agent_self_update' OR (
    action = 'agent_update'
    AND status IN ('running','verifying','succeeded','failed','timed_out')
    AND dispatched_at IS NOT NULL
    AND deadline_at IS NOT NULL
    AND deadline_at <= dispatched_at + INTERVAL '10 minutes'
    AND exec_deadline_s <= 600
    AND cardinality(target_keys) = 0));

CREATE UNIQUE INDEX idx_commands_self_update_open ON commands (device_id)
  WHERE origin = 'agent_self_update' AND status IN ('running','verifying');

CREATE INDEX idx_commands_self_update_recent ON commands (device_id, created_at)
  WHERE origin = 'agent_self_update';
