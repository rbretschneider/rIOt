ALTER TABLE events
    DROP COLUMN IF EXISTS container_id,
    DROP COLUMN IF EXISTS container_name;
