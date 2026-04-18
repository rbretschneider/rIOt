-- Allow alert rules to scope to specific containers (include) or exclude
-- specific containers (e.g. decluttarr, which dies constantly from API timeouts).
ALTER TABLE alert_rules
    ADD COLUMN IF NOT EXISTS include_containers TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS exclude_containers TEXT NOT NULL DEFAULT '';
