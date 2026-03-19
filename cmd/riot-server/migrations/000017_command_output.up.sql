-- Add duration and exit code to commands table
ALTER TABLE commands ADD COLUMN duration_ms BIGINT;
ALTER TABLE commands ADD COLUMN exit_code INT;

-- Create command_output table for captured stdout/stderr
CREATE TABLE command_output (
  id         BIGSERIAL PRIMARY KEY,
  command_id TEXT NOT NULL REFERENCES commands(id) ON DELETE CASCADE,
  stream     TEXT NOT NULL CHECK (stream IN ('stdout','stderr','combined')),
  content    TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_command_output_command_id ON command_output(command_id);
