-- Replace docker_group_count (distinct compose stacks) with a count of
-- containers actually covered by an enabled auto-update policy. The fleet
-- badge is more useful as "containers auto-updating / total containers" than
-- "stacks auto-updating / total stacks".
ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS docker_auto_update_container_count INT NOT NULL DEFAULT 0;
ALTER TABLE devices DROP COLUMN IF EXISTS docker_group_count;
