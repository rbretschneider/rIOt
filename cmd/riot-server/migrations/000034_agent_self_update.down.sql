-- EXPECT-002 rollback. The self-update history is deleted (the command_escalations
-- markers of those rows cascade). Events keep a dangling command_id, and the
-- agent_self_update_* events remain as plain warnings.
DELETE FROM commands WHERE origin = 'agent_self_update';
DROP INDEX IF EXISTS idx_commands_self_update_recent;
DROP INDEX IF EXISTS idx_commands_self_update_open;
ALTER TABLE commands DROP CONSTRAINT IF EXISTS commands_self_update_shape;
ALTER TABLE commands DROP CONSTRAINT commands_origin_check;
ALTER TABLE commands ADD CONSTRAINT commands_origin_check
  CHECK (origin IN ('user','auto_update','auto_patch'));
