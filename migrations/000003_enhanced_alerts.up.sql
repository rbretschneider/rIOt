-- Enhanced alerting: drop and recreate alert_rules with new columns
-- Also add acknowledgement support to events

-- Drop dependent tables first
DROP TABLE IF EXISTS notification_log;
DROP TABLE IF EXISTS alert_rules;

CREATE TABLE alert_rules (
    id               BIGSERIAL PRIMARY KEY,
    name             TEXT NOT NULL,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    metric           TEXT NOT NULL,
    operator         TEXT NOT NULL DEFAULT '>',
    threshold        DOUBLE PRECISION NOT NULL DEFAULT 0,
    target_name      TEXT NOT NULL DEFAULT '',
    target_state     TEXT NOT NULL DEFAULT '',
    severity         TEXT NOT NULL DEFAULT 'warning',
    device_filter    TEXT NOT NULL DEFAULT '',
    cooldown_seconds INTEGER NOT NULL DEFAULT 900,
    notify           BOOLEAN NOT NULL DEFAULT true,
    template_id      TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Recreate notification_log with FK to new alert_rules
CREATE TABLE notification_log (
    id              BIGSERIAL PRIMARY KEY,
    channel_id      BIGINT REFERENCES notification_channels(id) ON DELETE SET NULL,
    event_id        BIGINT REFERENCES events(id) ON DELETE SET NULL,
    alert_rule_id   BIGINT REFERENCES alert_rules(id) ON DELETE SET NULL,
    status          TEXT NOT NULL DEFAULT 'sent',
    error_msg       TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notification_log_created ON notification_log(created_at DESC);

-- Event acknowledgement support
ALTER TABLE events ADD COLUMN IF NOT EXISTS acknowledged_at TIMESTAMPTZ;
CREATE INDEX idx_events_unack ON events(acknowledged_at) WHERE acknowledged_at IS NULL;
