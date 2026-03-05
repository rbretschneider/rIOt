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
    id          BIGSERIAL PRIMARY KEY,
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,
    severity    TEXT NOT NULL DEFAULT 'info',
    message     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_events_device_time ON events(device_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);

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
