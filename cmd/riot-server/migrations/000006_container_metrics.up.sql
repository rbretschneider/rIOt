CREATE TABLE IF NOT EXISTS container_metrics (
    id              BIGSERIAL PRIMARY KEY,
    device_id       TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    container_name  TEXT NOT NULL,
    container_id    TEXT NOT NULL DEFAULT '',
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cpu_percent     DOUBLE PRECISION NOT NULL DEFAULT 0,
    mem_usage       BIGINT NOT NULL DEFAULT 0,
    mem_limit       BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_container_metrics_lookup ON container_metrics(device_id, container_name, timestamp DESC);
CREATE INDEX idx_container_metrics_time ON container_metrics(timestamp);
