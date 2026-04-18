ALTER TABLE alert_rules
    DROP COLUMN IF EXISTS include_containers,
    DROP COLUMN IF EXISTS exclude_containers;
