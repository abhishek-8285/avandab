-- 004: GPS provider-parity columns (server migration 00117 counterpart).
-- speed (m/s as reported by the platform), heading (degrees), motion
-- (derived server-side flag: 1 = moving, 0 = parked), battery_level
-- (phone battery percent 0-100 from expo-battery, nullable).
-- satellites omitted: expo-location does not expose satellite count.
ALTER TABLE offline_gps_logs ADD COLUMN speed REAL;
ALTER TABLE offline_gps_logs ADD COLUMN heading REAL;
ALTER TABLE offline_gps_logs ADD COLUMN motion INTEGER;
ALTER TABLE offline_gps_logs ADD COLUMN battery_level REAL;

-- down:
-- ALTER TABLE offline_gps_logs DROP COLUMN battery_level;
-- ALTER TABLE offline_gps_logs DROP COLUMN motion;
-- ALTER TABLE offline_gps_logs DROP COLUMN heading;
-- ALTER TABLE offline_gps_logs DROP COLUMN speed;