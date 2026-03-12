-- Allow events with device_id values that don't reference the devices table
-- (e.g. probe events use "probe:<id>" as a synthetic device_id).
ALTER TABLE events DROP CONSTRAINT IF EXISTS events_device_id_fkey;
