-- EXPECT-001: expected-activity windows.
--
-- Bounds (AD-012): command_escalations holds at most 22 rows per command and is
-- removed with its command (ON DELETE CASCADE); device_powered_off holds at most
-- one row per device and is removed with the device; the two commands columns
-- live and die with their row.

ALTER TABLE commands
  ADD COLUMN window_closed_at TIMESTAMPTZ,
  ADD COLUMN window_outcome  TEXT NOT NULL DEFAULT '';

-- X-12: terminal commands that finished more than an hour before the upgrade
-- are lapsed here, so the first start does not escalate for old history.
UPDATE commands
   SET window_closed_at = COALESCE(finished_at, updated_at), window_outcome = 'lapsed'
 WHERE status IN ('succeeded','failed','timed_out','cancelled','expired','success','error')
   AND COALESCE(finished_at, updated_at) < now() - INTERVAL '1 hour';

-- The list below must equal models.ExpectActions() (a unit test compares them).
CREATE INDEX idx_commands_window_pending ON commands (device_id, created_at)
  WHERE window_closed_at IS NULL
    AND action IN ('reboot','shutdown','os_update','agent_update',
                   'docker_update','docker_bulk_update','docker_restart','docker_stop',
                   'docker_stack_redeploy','docker_stack_restart','docker_stack_up','docker_stack_down');

CREATE INDEX idx_events_command ON events (command_id, created_at) WHERE command_id <> '';

CREATE TABLE command_escalations (
  command_id  TEXT        NOT NULL REFERENCES commands(id) ON DELETE CASCADE,
  target      TEXT        NOT NULL,   -- 'device' | 'summary' | 'container:<32 hex of sha256(key name)>'
  device_id   TEXT        NOT NULL,
  event_id    BIGINT,
  created_at  TIMESTAMPTZ NOT NULL,
  cleared_at  TIMESTAMPTZ,            -- device target only
  PRIMARY KEY (command_id, target),
  CONSTRAINT command_escalations_target_len CHECK (octet_length(target) <= 64)
);
CREATE INDEX idx_command_escalations_device_open ON command_escalations (device_id, created_at DESC)
  WHERE target = 'device' AND cleared_at IS NULL;

CREATE TABLE device_powered_off (
  device_id     TEXT        PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
  command_id    TEXT        NOT NULL,
  action_label  TEXT        NOT NULL,
  actor_phrase  TEXT        NOT NULL,
  shutdown_at   TIMESTAMPTZ NOT NULL,
  offline_at    TIMESTAMPTZ NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
