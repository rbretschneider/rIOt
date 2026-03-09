-- Remove pre-existing duplicate log entries (keep the lowest id).
DELETE FROM device_logs a
  USING device_logs b
 WHERE a.device_id  = b.device_id
   AND a.timestamp  = b.timestamp
   AND a.priority   = b.priority
   AND a.unit       = b.unit
   AND md5(a.message) = md5(b.message)
   AND a.id > b.id;

-- Prevent future duplicates from overlapping telemetry windows.
CREATE UNIQUE INDEX idx_device_logs_dedup
    ON device_logs (device_id, timestamp, priority, unit, md5(message));
