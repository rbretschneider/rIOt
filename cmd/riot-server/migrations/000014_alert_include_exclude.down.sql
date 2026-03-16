ALTER TABLE alert_rules ADD COLUMN device_filter TEXT NOT NULL DEFAULT '';

UPDATE alert_rules SET device_filter = include_devices WHERE include_devices != '';

ALTER TABLE alert_rules DROP COLUMN include_devices;
ALTER TABLE alert_rules DROP COLUMN exclude_devices;
