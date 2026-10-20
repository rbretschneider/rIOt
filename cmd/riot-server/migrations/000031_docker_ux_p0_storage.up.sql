CREATE INDEX IF NOT EXISTS idx_container_logs_name ON container_logs(device_id, container_name, timestamp DESC);
CREATE TABLE IF NOT EXISTS device_storage_usage (
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    state JSONB NOT NULL,
    PRIMARY KEY (device_id,day)
);
