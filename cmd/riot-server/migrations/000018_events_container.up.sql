-- Attach container context to events so the dashboard can link alerts
-- to the specific container (not just the device).
ALTER TABLE events
    ADD COLUMN container_id   TEXT NOT NULL DEFAULT '',
    ADD COLUMN container_name TEXT NOT NULL DEFAULT '';
