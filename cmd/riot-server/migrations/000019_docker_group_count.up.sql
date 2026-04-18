-- Track distinct compose stacks per device so the fleet page can show
-- auto-update coverage as X/Y (stacks with auto-update / total stacks).
ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS docker_group_count INT NOT NULL DEFAULT 0;
