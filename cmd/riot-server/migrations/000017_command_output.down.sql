DROP TABLE IF EXISTS command_output;
ALTER TABLE commands DROP COLUMN IF EXISTS duration_ms;
ALTER TABLE commands DROP COLUMN IF EXISTS exit_code;
