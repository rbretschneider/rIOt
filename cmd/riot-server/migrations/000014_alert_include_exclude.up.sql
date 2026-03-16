ALTER TABLE alert_rules ADD COLUMN include_devices TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_rules ADD COLUMN exclude_devices TEXT NOT NULL DEFAULT '';

-- Migrate existing device_filter values into include_devices
UPDATE alert_rules SET include_devices = device_filter WHERE device_filter != '';

ALTER TABLE alert_rules DROP COLUMN device_filter;
