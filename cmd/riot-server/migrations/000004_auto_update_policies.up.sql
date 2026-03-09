CREATE TABLE auto_update_policies (
    id SERIAL PRIMARY KEY,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    target TEXT NOT NULL,
    is_stack BOOLEAN NOT NULL DEFAULT false,
    compose_work_dir TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_triggered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(device_id, target)
);
