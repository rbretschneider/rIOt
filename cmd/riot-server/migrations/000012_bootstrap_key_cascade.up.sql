-- Fix: bootstrap_keys.used_by_device had no ON DELETE action, defaulting to
-- RESTRICT which blocked device deletion when a bootstrap key referenced it.
ALTER TABLE bootstrap_keys DROP CONSTRAINT IF EXISTS bootstrap_keys_used_by_device_fkey;
ALTER TABLE bootstrap_keys ADD CONSTRAINT bootstrap_keys_used_by_device_fkey
    FOREIGN KEY (used_by_device) REFERENCES devices(id) ON DELETE SET NULL;
