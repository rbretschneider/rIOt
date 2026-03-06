CREATE TABLE server_logs (
    id         BIGSERIAL PRIMARY KEY,
    timestamp  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    level      TEXT NOT NULL,
    message    TEXT NOT NULL,
    attrs      JSONB,
    source     TEXT DEFAULT ''
);

CREATE INDEX idx_server_logs_ts ON server_logs (timestamp DESC);
CREATE INDEX idx_server_logs_level ON server_logs (level);
