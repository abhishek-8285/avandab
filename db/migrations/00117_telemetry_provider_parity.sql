-- +goose Up
-- 00117: Telemetry provider parity (Spec 17 §5) — columns every mainstream
-- telematics provider (Traccar Position model, Teltonika Codec 8 AVL,
-- Samsara vehicle stats) carries and our pipeline needs for guards/alerts:
--   satellites        GNSS fix quality (Teltonika GNSS element, Traccar attr)
--   battery_level     device battery (mobile_app drain alerts; SOS already proves value)
--   external_voltage  hardwired tracker power (Teltonika IO-67) — unplug = tamper
--   gsm_signal        network triage: "no frames" vs "frames but bad network"
--   motion            parked-vehicle dedup (Traccar motion attr)
--   valid             GPS fix validity — invalid fixes stored for audit but
--                     NEVER overwrite vehicle_latest_position (stops parking drift)
--   fix_time          Traccar deviceTime/fixTime split (stale-fix detection)
-- Deliberately NOT added: altitude (zero consumers today — Traccar-only habit;
-- raw payload JSON already preserves it via telemetry_raw_events.payload).
-- All nullable/defaulted: zero backfill, old providers unaffected.

ALTER TABLE telemetry_positions ADD COLUMN satellites INTEGER;
ALTER TABLE telemetry_positions ADD COLUMN battery_level REAL;
ALTER TABLE telemetry_positions ADD COLUMN external_voltage REAL;
ALTER TABLE telemetry_positions ADD COLUMN gsm_signal INTEGER;
ALTER TABLE telemetry_positions ADD COLUMN motion INTEGER;
ALTER TABLE telemetry_positions ADD COLUMN valid INTEGER NOT NULL DEFAULT 1;
ALTER TABLE telemetry_positions ADD COLUMN fix_time DATETIME;

-- vehicle_latest_position mirrors the same set: the upsert writes one frame
-- per row, partial mirroring would force readers to join history for fields
-- history has but latest doesn't.
ALTER TABLE vehicle_latest_position ADD COLUMN satellites INTEGER;
ALTER TABLE vehicle_latest_position ADD COLUMN battery_level REAL;
ALTER TABLE vehicle_latest_position ADD COLUMN external_voltage REAL;
ALTER TABLE vehicle_latest_position ADD COLUMN gsm_signal INTEGER;
ALTER TABLE vehicle_latest_position ADD COLUMN motion INTEGER;
ALTER TABLE vehicle_latest_position ADD COLUMN valid INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE vehicle_latest_position DROP COLUMN valid;
ALTER TABLE vehicle_latest_position DROP COLUMN motion;
ALTER TABLE vehicle_latest_position DROP COLUMN gsm_signal;
ALTER TABLE vehicle_latest_position DROP COLUMN external_voltage;
ALTER TABLE vehicle_latest_position DROP COLUMN battery_level;
ALTER TABLE vehicle_latest_position DROP COLUMN satellites;
ALTER TABLE telemetry_positions DROP COLUMN fix_time;
ALTER TABLE telemetry_positions DROP COLUMN valid;
ALTER TABLE telemetry_positions DROP COLUMN motion;
ALTER TABLE telemetry_positions DROP COLUMN gsm_signal;
ALTER TABLE telemetry_positions DROP COLUMN external_voltage;
ALTER TABLE telemetry_positions DROP COLUMN battery_level;
ALTER TABLE telemetry_positions DROP COLUMN satellites;