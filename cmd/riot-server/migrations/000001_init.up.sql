-- rIOt: consolidated schema (all tables)

-- Devices table: registry of all monitored devices
CREATE TABLE IF NOT EXISTS devices (
    id          TEXT PRIMARY KEY,
    short_id    TEXT NOT NULL,
    hostname    TEXT NOT NULL,
    arch        TEXT NOT NULL DEFAULT '',
    agent_version TEXT NOT NULL DEFAULT '',
    primary_ip  TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'offline',
    tags        JSONB NOT NULL DEFAULT '[]',
    hardware_profile JSONB,
    last_heartbeat  TIMESTAMPTZ,
    last_telemetry  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status);
CREATE INDEX IF NOT EXISTS idx_devices_hostname ON devices(hostname);

-- API keys table: per-device authentication (hashed)
CREATE TABLE IF NOT EXISTS api_keys (
    key_hash    TEXT PRIMARY KEY,
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_api_keys_device_id ON api_keys(device_id);

-- Heartbeats table: lightweight pings
CREATE TABLE IF NOT EXISTS heartbeats (
    id          BIGSERIAL PRIMARY KEY,
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    timestamp   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    data        JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_heartbeats_device_time ON heartbeats(device_id, timestamp DESC);

-- Telemetry snapshots: full telemetry JSONB blobs
CREATE TABLE IF NOT EXISTS telemetry_snapshots (
    id          BIGSERIAL PRIMARY KEY,
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    timestamp   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    data        JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_telemetry_device_time ON telemetry_snapshots(device_id, timestamp DESC);

-- Events table: device events and alerts
CREATE TABLE IF NOT EXISTS events (
    id              BIGSERIAL PRIMARY KEY,
    device_id       TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    type            TEXT NOT NULL,
    severity        TEXT NOT NULL DEFAULT 'info',
    message         TEXT NOT NULL DEFAULT '',
    acknowledged_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_events_device_time ON events(device_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);
CREATE INDEX IF NOT EXISTS idx_events_unack ON events(acknowledged_at) WHERE acknowledged_at IS NULL;

-- Admin config table: key-value store for server configuration
CREATE TABLE IF NOT EXISTS admin_config (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Terminal sessions table: audit log for terminal access
CREATE TABLE IF NOT EXISTS terminal_sessions (
    id           BIGSERIAL PRIMARY KEY,
    device_id    TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    container_id TEXT NOT NULL,
    session_id   TEXT NOT NULL,
    remote_addr  TEXT NOT NULL DEFAULT '',
    started_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_terminal_sessions_device ON terminal_sessions(device_id, started_at DESC);

-- Alert rules: configurable threshold-based alerting (enhanced)
CREATE TABLE IF NOT EXISTS alert_rules (
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

-- Notification channels: ntfy, webhook, etc.
CREATE TABLE IF NOT EXISTS notification_channels (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    type            TEXT NOT NULL,
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
    status          TEXT NOT NULL DEFAULT 'sent',
    error_msg       TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notification_log_created ON notification_log(created_at DESC);

-- Commands: remote command execution tracking
CREATE TABLE IF NOT EXISTS commands (
    id              TEXT PRIMARY KEY,
    device_id       TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    action          TEXT NOT NULL,
    params          JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending',
    result_msg      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_commands_device ON commands(device_id, created_at DESC);

-- Probes: synthetic monitoring checks
CREATE TABLE IF NOT EXISTS probes (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    type            TEXT NOT NULL,
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

-- mTLS: CA storage
CREATE TABLE IF NOT EXISTS ca_config (
    id          INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    ca_cert_pem TEXT NOT NULL,
    ca_key_pem  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- mTLS: device certificates
CREATE TABLE IF NOT EXISTS device_certs (
    id            BIGSERIAL PRIMARY KEY,
    device_id     TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    serial_number TEXT NOT NULL UNIQUE,
    cert_pem      TEXT NOT NULL,
    not_before    TIMESTAMPTZ NOT NULL,
    not_after     TIMESTAMPTZ NOT NULL,
    revoked       BOOLEAN NOT NULL DEFAULT false,
    revoked_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- mTLS: bootstrap keys for initial enrollment
CREATE TABLE IF NOT EXISTS bootstrap_keys (
    key_hash       TEXT PRIMARY KEY,
    label          TEXT NOT NULL DEFAULT '',
    used           BOOLEAN NOT NULL DEFAULT false,
    used_by_device TEXT REFERENCES devices(id),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at     TIMESTAMPTZ NOT NULL
);
