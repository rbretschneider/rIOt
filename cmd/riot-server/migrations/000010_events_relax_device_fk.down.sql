-- Re-add foreign key constraint (may fail if synthetic device_id rows exist)
ALTER TABLE events ADD CONSTRAINT events_device_id_fkey FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE;
