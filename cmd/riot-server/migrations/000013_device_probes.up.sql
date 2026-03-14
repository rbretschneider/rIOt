CREATE TABLE IF NOT EXISTS device_probes (
    id               BIGSERIAL PRIMARY KEY,
    name             TEXT NOT NULL,
    device_id        TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    type             TEXT NOT NULL,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    config           JSONB NOT NULL DEFAULT '{}',
    assertions       JSONB NOT NULL DEFAULT '[]',
    interval_seconds INTEGER NOT NULL DEFAULT 60,
    timeout_seconds  INTEGER NOT NULL DEFAULT 10,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS device_probe_results (
    id                BIGSERIAL PRIMARY KEY,
    probe_id          BIGINT NOT NULL REFERENCES device_probes(id) ON DELETE CASCADE,
    device_id         TEXT NOT NULL,
    success           BOOLEAN NOT NULL,
    latency_ms        DOUBLE PRECISION,
    output            JSONB NOT NULL DEFAULT '{}',
    failed_assertions JSONB,
    error_msg         TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_device_probes_device ON device_probes(device_id);
CREATE INDEX IF NOT EXISTS idx_device_probe_results_probe ON device_probe_results(probe_id);
CREATE INDEX IF NOT EXISTS idx_device_probe_results_created ON device_probe_results(created_at);
