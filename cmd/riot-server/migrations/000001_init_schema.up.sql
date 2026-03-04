-- Devices table: registry of all monitored devices
CREATE TABLE IF NOT EXISTS devices (
    id          TEXT PRIMARY KEY,
    short_id    TEXT NOT NULL,
    hostname    TEXT NOT NULL,
    arch        TEXT NOT NULL DEFAULT '',
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

-- API keys table: per-device authentication
CREATE TABLE IF NOT EXISTS api_keys (
    key         TEXT PRIMARY KEY,
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
