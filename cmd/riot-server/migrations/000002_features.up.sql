-- Alert rules: configurable threshold-based alerting
CREATE TABLE IF NOT EXISTS alert_rules (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    metric          TEXT NOT NULL,           -- mem_percent, disk_percent, updates, container_died, container_oom, device_offline
    operator        TEXT NOT NULL DEFAULT '>', -- >, <, >=, <=, ==, !=
    threshold       DOUBLE PRECISION NOT NULL DEFAULT 0,
    severity        TEXT NOT NULL DEFAULT 'warning',
    device_filter   TEXT NOT NULL DEFAULT '',  -- empty = all devices, or comma-separated device IDs/tags
    cooldown_seconds INTEGER NOT NULL DEFAULT 900,
    notify          BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Notification channels: ntfy, webhook, etc.
CREATE TABLE IF NOT EXISTS notification_channels (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    type            TEXT NOT NULL,            -- ntfy, webhook
    enabled         BOOLEAN NOT NULL DEFAULT true,
    config          JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Notification log: audit trail of sent notifications
CREATE TABLE IF NOT EXISTS notification_log (
    id              BIGSERIAL PRIMARY KEY,
    channel_id      BIGINT REFERENCES notification_channels(id) ON DELETE SET NULL,
    event_id        BIGINT REFERENCES events(id) ON DELETE SET NULL,
    alert_rule_id   BIGINT REFERENCES alert_rules(id) ON DELETE SET NULL,
    status          TEXT NOT NULL DEFAULT 'sent',  -- sent, failed
    error_msg       TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notification_log_created ON notification_log(created_at DESC);

-- Commands: remote command execution tracking
CREATE TABLE IF NOT EXISTS commands (
    id              TEXT PRIMARY KEY,          -- UUID
    device_id       TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    action          TEXT NOT NULL,             -- docker_stop, docker_restart, docker_start, reboot, agent_update
    params          JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending', -- pending, sent, success, error
    result_msg      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_commands_device ON commands(device_id, created_at DESC);

-- Probes: synthetic monitoring checks
CREATE TABLE IF NOT EXISTS probes (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    type            TEXT NOT NULL,             -- ping, dns, http
    enabled         BOOLEAN NOT NULL DEFAULT true,
    config          JSONB NOT NULL DEFAULT '{}',
    interval_seconds INTEGER NOT NULL DEFAULT 60,
    timeout_seconds  INTEGER NOT NULL DEFAULT 10,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Probe results: check execution history
CREATE TABLE IF NOT EXISTS probe_results (
    id              BIGSERIAL PRIMARY KEY,
    probe_id        BIGINT NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
    success         BOOLEAN NOT NULL,
    latency_ms      DOUBLE PRECISION NOT NULL DEFAULT 0,
    status_code     INTEGER,
    error_msg       TEXT NOT NULL DEFAULT '',
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_probe_results_probe_time ON probe_results(probe_id, created_at DESC);
