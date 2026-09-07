-- PG port of 00043_fuel_audit_scorecard.sql | status: PORTABLE | flags: none
-- +goose Up
-- FUEL AUDIT + DRIVER SCORECARD (owned by fuel spec; depends on 00042 geofence columns
-- tank_capacity_litres / fuel_sensor_fitted on vehicles — reference only, no changes here)

-- 1. fuel_events — durable per-vehicle fuel anomaly/refill record
CREATE TABLE IF NOT EXISTS fuel_events (
    id              TEXT PRIMARY KEY,
    vehicle_id      TEXT NOT NULL,
    trip_id         TEXT,
    driver_id       TEXT,
    event_type      TEXT NOT NULL CHECK (event_type IN
                      ('refill_detected','drain_theft_suspected','abnormal_drain',
                       'siphon_confirmed','odometer_rollback')),
    fuel_level_before REAL,
    fuel_level_after  REAL,
    odometer_before   REAL,
    odometer_after    REAL,
    estimated_litres  REAL,
    confidence        REAL,
    details           TEXT,
    occurred_at       TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (trip_id) REFERENCES trips(id),
    FOREIGN KEY (driver_id) REFERENCES drivers(id)
);
CREATE INDEX IF NOT EXISTS idx_fuel_events_vehicle_time ON fuel_events(vehicle_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_fuel_events_trip ON fuel_events(trip_id);
-- 2. fuel_claim_audits — per-claim audit trail (one row per claim, re-audit upserts)
CREATE TABLE IF NOT EXISTS fuel_claim_audits (
    id                  TEXT PRIMARY KEY,
    expense_id          TEXT NOT NULL UNIQUE,
    trip_id             TEXT,
    vehicle_id          TEXT,
    driver_id           TEXT,
    litres_claimed      REAL,
    litres_expected_level REAL,
    litres_expected_odo REAL,
    level_delta_pct     REAL,
    odometer_delta_km   REAL,
    tank_capacity_litres REAL,
    kmpl_used           REAL,
    variance_litres     REAL,
    variance_pct        REAL,
    result              TEXT NOT NULL DEFAULT 'needs_review'
                          CHECK (result IN ('needs_review','passed','failed')),
    checks              TEXT NOT NULL DEFAULT '[]',
    reviewed_by         TEXT REFERENCES users(id),
    reviewed_at         TIMESTAMPTZ,
    review_note         TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (expense_id) REFERENCES driver_expenses(id),
    FOREIGN KEY (trip_id) REFERENCES trips(id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (driver_id) REFERENCES drivers(id)
);
CREATE INDEX IF NOT EXISTS idx_fuel_claim_audits_result ON fuel_claim_audits(result);
-- 3. driver_behaviour_events — weighted scorecard inputs
CREATE TABLE IF NOT EXISTS driver_behaviour_events (
    id          TEXT PRIMARY KEY,
    driver_id   TEXT NOT NULL,
    trip_id     TEXT,
    vehicle_id  TEXT,
    event_type  TEXT NOT NULL CHECK (event_type IN
                  ('speeding','harsh_braking','harsh_accel','idling','night_driving',
                   'fuel_theft_suspicion','odometer_rollback')),
    severity    TEXT NOT NULL DEFAULT 'medium' CHECK (severity IN ('low','medium','high')),
    weight      REAL NOT NULL DEFAULT 1.0,
    metadata    TEXT NOT NULL DEFAULT '{}',
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (driver_id) REFERENCES drivers(id),
    FOREIGN KEY (trip_id) REFERENCES trips(id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id)
);
CREATE INDEX IF NOT EXISTS idx_dbe_driver_time ON driver_behaviour_events(driver_id, occurred_at);
-- 4. driver_scores — 30-day rolling score history
CREATE TABLE IF NOT EXISTS driver_scores (
    id           TEXT PRIMARY KEY,
    driver_id    TEXT NOT NULL,
    score        REAL NOT NULL,
    tier         TEXT NOT NULL CHECK (tier IN ('A','B','C')),
    period_start TIMESTAMPTZ NOT NULL,
    period_end   TIMESTAMPTZ NOT NULL,
    event_counts TEXT NOT NULL DEFAULT '{}',
    computed_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (driver_id) REFERENCES drivers(id)
);
CREATE INDEX IF NOT EXISTS idx_driver_scores_driver ON driver_scores(driver_id, period_end DESC);
-- 5. company_config — fuel + scorecard knobs (plain SQL read, no sqlc regen).
-- OWNERSHIP: the table is CREATED ONCE by spec 02 (geofence) at migration 00042.
-- Per 00-migration-ownership-index.md, do NOT CREATE it here. This spec only
-- seeds rows (item 10 below) and adds no columns to it.
-- NOTE: canonical 00042 schema is (tenant_id, key, value, updated_at) — no
-- description column; seeds below use that schema (spec §5 override).

-- 6. drivers — current score/tier (denormalized)
ALTER TABLE drivers ADD COLUMN score REAL;
ALTER TABLE drivers ADD COLUMN tier TEXT;
-- 7. driver_settlements — performance bonus (written by settlement computation)
ALTER TABLE driver_settlements ADD COLUMN performance_bonus REAL NOT NULL DEFAULT 0.0;
-- 8. driver_expenses — audit columns
ALTER TABLE driver_expenses ADD COLUMN audit_status TEXT NOT NULL DEFAULT 'pending'
    CHECK (audit_status IN ('pending','needs_review','passed','failed'));
ALTER TABLE driver_expenses ADD COLUMN fuel_litres REAL;
-- 9. RBAC seeds — fuel:* and scorecard:*
INSERT INTO permissions (name, description) VALUES
('fuel:read',       'View fuel audit queue and reports'),
('fuel:update',     'Review fuel audit claims'),
('scorecard:read',  'View driver scorecard and leaderboard') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name LIKE 'fuel:%' OR name LIKE 'scorecard:%' ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.role_id, p.id
FROM (SELECT 2 AS role_id UNION ALL SELECT 3) r
JOIN permissions p ON p.name IN ('fuel:read','scorecard:read') ON CONFLICT DO NOTHING;
-- 10. company_config seed defaults (full key list in §9)
INSERT INTO company_config (tenant_id, key, value) VALUES
('1', 'fuel.median_window', '7'),
('1', 'fuel.spike_deviation_pct', '25'),
('1', 'fuel.noise_floor_pct', '1.5'),
('1', 'fuel.refill_threshold_litres', '20'),
('1', 'fuel.theft_drop_threshold_litres', '10'),
('1', 'fuel.siphon_drop_threshold_litres', '15'),
('1', 'fuel.siphon_stop_minutes', '20'),
('1', 'fuel.stop_speed_kmh', '5'),
('1', 'fuel.odometer_tolerance_km', '1'),
('1', 'fuel.abnormal_drain_l_per_km', '0.6'),
('1', 'fuel.abnormal_drain_margin_pct', '30'),
('1', 'fuel.level_unit', 'percent'),
('1', 'fuel.tank_capacity_default', '0'),
('1', 'fuel.kmpl_default', '4.0'),
('1', 'fuel.claim_tolerance_pct', '20'),
('1', 'fuel.claim_crosscheck_margin_pct', '25'),
('1', 'fuel.audit_enforce', 'false'),
('1', 'fuel.tick_interval_seconds', '30'),
('1', 'fuel.gap_tolerance_minutes', '30'),
('1', 'scorecard.window_days', '30'),
('1', 'scorecard.tier_a', '85'),
('1', 'scorecard.tier_b', '70'),
('1', 'scorecard.min_events', '3'),
('1', 'scorecard.fraud_cap', '69'),
('1', 'scorecard.weight.speeding', '8'),
('1', 'scorecard.weight.harsh_braking', '6'),
('1', 'scorecard.weight.harsh_accel', '6'),
('1', 'scorecard.weight.idling', '3'),
('1', 'scorecard.weight.night_driving', '2'),
('1', 'scorecard.weight.fuel_theft_suspicion', '25'),
('1', 'scorecard.weight.odometer_rollback', '20'),
('1', 'scorecard.bonus_a_pct', '5'),
('1', 'scorecard.bonus_b_pct', '2'),
('1', 'scorecard.bonus_c_pct', '0') ON CONFLICT DO NOTHING;
-- 11. Engine access path: per-vehicle replay + incremental poll both scan
--     by (vehicle_id, timestamp) — Spec 03 §13.12 VERIFY item, decided at
--     implementation: the engine's hot queries demand this index.
CREATE INDEX IF NOT EXISTS idx_telemetry_snapshots_vehicle_time
    ON telemetry_snapshots(vehicle_id, timestamp);
-- +goose Down
DROP INDEX IF EXISTS idx_telemetry_snapshots_vehicle_time;
DROP INDEX IF EXISTS idx_dbe_driver_time;
DROP INDEX IF EXISTS idx_driver_scores_driver;
DROP INDEX IF EXISTS idx_fuel_claim_audits_result;
DROP INDEX IF EXISTS idx_fuel_events_trip;
DROP INDEX IF EXISTS idx_fuel_events_vehicle_time;
DROP TABLE IF EXISTS driver_scores;
DROP TABLE IF EXISTS driver_behaviour_events;
DROP TABLE IF EXISTS fuel_claim_audits;
DROP TABLE IF EXISTS fuel_events;
-- (company_config DROP intentionally omitted: owned/created by spec 02 @00042)
ALTER TABLE drivers DROP COLUMN score;
ALTER TABLE drivers DROP COLUMN tier;
ALTER TABLE driver_settlements DROP COLUMN performance_bonus;
ALTER TABLE driver_expenses DROP COLUMN audit_status;
ALTER TABLE driver_expenses DROP COLUMN fuel_litres;
DELETE FROM permissions WHERE name IN ('fuel:read','fuel:update','scorecard:read');
