CREATE TABLE IF NOT EXISTS container_logs (
    id          BIGSERIAL PRIMARY KEY,
    device_id   TEXT NOT NULL,
    container_id   TEXT NOT NULL,
    container_name TEXT NOT NULL,
    timestamp   TIMESTAMPTZ NOT NULL,
    stream      TEXT NOT NULL DEFAULT 'stdout',
    line        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_container_logs_lookup ON container_logs(device_id, container_id, timestamp DESC);
CREATE INDEX idx_container_logs_time ON container_logs(timestamp);
