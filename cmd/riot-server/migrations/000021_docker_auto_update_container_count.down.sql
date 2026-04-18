ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS docker_group_count INT NOT NULL DEFAULT 0;
ALTER TABLE devices DROP COLUMN IF EXISTS docker_auto_update_container_count;
