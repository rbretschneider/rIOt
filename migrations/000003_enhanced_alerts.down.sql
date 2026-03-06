-- Revert enhanced alerting changes
DROP INDEX IF EXISTS idx_events_unack;
ALTER TABLE events DROP COLUMN IF EXISTS acknowledged_at;

DROP TABLE IF EXISTS notification_log;
DROP TABLE IF EXISTS alert_rules;

-- Recreate original alert_rules
CREATE TABLE alert_rules (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    metric          TEXT NOT NULL,
    operator        TEXT NOT NULL DEFAULT '>',
    threshold       DOUBLE PRECISION NOT NULL DEFAULT 0,
    severity        TEXT NOT NULL DEFAULT 'warning',
    device_filter   TEXT NOT NULL DEFAULT '',
    cooldown_seconds INTEGER NOT NULL DEFAULT 900,
    notify          BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Recreate original notification_log
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
