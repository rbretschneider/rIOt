-- EXPECT-008 rollback. The reinstall history is deleted (the command_escalations
-- markers of those rows cascade). Events keep a dangling command_id, and the
-- agent_reinstall_* events remain as plain warnings.
DELETE FROM commands WHERE origin = 'installer';
DROP INDEX IF EXISTS idx_commands_reinstall_recent;
DROP INDEX IF EXISTS idx_commands_reinstall_open;
ALTER TABLE commands DROP CONSTRAINT IF EXISTS commands_reinstall_shape;
ALTER TABLE commands DROP CONSTRAINT commands_origin_check;
ALTER TABLE commands ADD CONSTRAINT commands_origin_check
  CHECK (origin IN ('user','auto_update','auto_patch','agent_self_update'));
