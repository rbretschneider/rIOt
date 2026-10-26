-- EXPECT-001 rollback. Only the new state is lost: after a down then up, the
-- escalation markers of commands that finished in the last hour are gone, so
-- those commands can escalate a second time.
DROP TABLE IF EXISTS device_powered_off;
DROP TABLE IF EXISTS command_escalations;
DROP INDEX IF EXISTS idx_events_command;
DROP INDEX IF EXISTS idx_commands_window_pending;
ALTER TABLE commands DROP COLUMN IF EXISTS window_outcome, DROP COLUMN IF EXISTS window_closed_at;
DELETE FROM admin_config WHERE key = 'expect_restart_series';
