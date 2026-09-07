-- PG port of 00045_alerts_pipeline.sql | status: NEEDS-REVIEW | flags: HAS-DQUOTE | reviewed: YES
-- +goose Up
CREATE TABLE IF NOT EXISTS alert_sources (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    is_active   INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS alert_rules (
    id              TEXT PRIMARY KEY,
    source          TEXT NOT NULL REFERENCES alert_sources(name),
    alert_type      TEXT NOT NULL,
    name            TEXT NOT NULL,
    severity        TEXT NOT NULL DEFAULT 'warning'
                    CHECK (severity IN ('info','warning','critical','blocker')),
    threshold       REAL,
    threshold_unit  TEXT,
    dedup_key_expr  TEXT NOT NULL DEFAULT 'source:alert_type:entity_id',
    cooldown_seconds INTEGER NOT NULL DEFAULT 300,
    storm_window_seconds INTEGER NOT NULL DEFAULT 60,
    storm_batch_min   INTEGER NOT NULL DEFAULT 3,
    channel_routing TEXT NOT NULL DEFAULT 'in_app',
    escalation_schedule TEXT,
    is_active       INTEGER NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS rule_overrides (
    id          TEXT PRIMARY KEY,
    rule_id     TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    entity_id   TEXT,
    severity    TEXT,
    threshold   REAL,
    cooldown_seconds INTEGER,
    channels    TEXT,
    is_active   INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS alerts (
    id              TEXT PRIMARY KEY,
    rule_id         TEXT REFERENCES alert_rules(id),
    source          TEXT NOT NULL,
    alert_type      TEXT NOT NULL,
    severity        TEXT NOT NULL DEFAULT 'warning',
    status          TEXT NOT NULL DEFAULT 'open'
                    CHECK (status IN ('open','acknowledged','resolved','escalated','closed')),
    dedup_key       TEXT NOT NULL,
    entity_type     TEXT,
    entity_id       TEXT,
    user_id         TEXT,
    title           TEXT NOT NULL,
    message         TEXT NOT NULL,
    occurrences     INTEGER NOT NULL DEFAULT 1,
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    next_escalation_at TIMESTAMPTZ,
    escalation_step INTEGER NOT NULL DEFAULT 0,
    latitude        REAL,
    longitude       REAL,
    metadata        TEXT,
    acked_by        TEXT,
    acked_at        TIMESTAMPTZ,
    resolved_by     TEXT,
    resolved_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS idx_alerts_status_seen  ON alerts(status, last_seen_at);
CREATE INDEX IF NOT EXISTS idx_alerts_dedup        ON alerts(dedup_key, status);
CREATE INDEX IF NOT EXISTS idx_alerts_entity       ON alerts(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_alerts_user_status  ON alerts(user_id, status);
CREATE TABLE IF NOT EXISTS notifications_preferences (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel    TEXT NOT NULL CHECK (channel IN ('in_app','email','whatsapp','sms','telegram')),
    enabled    INTEGER NOT NULL DEFAULT 1,
    min_severity TEXT NOT NULL DEFAULT 'warning'
                 CHECK (min_severity IN ('info','warning','critical','blocker')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, channel)
);
-- Seed sources
INSERT INTO alert_sources (id, name, description) VALUES
 ('src_telemetry','telemetry','Raw telemetry exception stream'),
 ('src_geofence','geofence','Geofence boundary/usage events'),
 ('src_fuel','fuel','Fuel refill/theft/siphon events'),
 ('src_compliance','compliance','Document expiry and dispatch blocks'),
 ('src_ewaybill','ewaybill','E-way bill lifecycle failures'),
 ('src_sos','sos','Emergency SOS events') ON CONFLICT DO NOTHING;
-- Seed default rules
INSERT INTO alert_rules (id, source, alert_type, name, severity, threshold, threshold_unit, cooldown_seconds, storm_window_seconds, storm_batch_min, channel_routing) VALUES
 ('rule_speeding', 'telemetry', 'speeding', 'Over Speeding Alert', 'warning', 80.0, 'km/h', 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_gps_dev', 'telemetry', 'gps_deviation', 'Route GPS Deviation', 'warning', 5.0, 'km', 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_temp_breach', 'telemetry', 'temp_breach', 'Cargo Temperature Breach', 'critical', 25.0, 'C', 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_night_drive', 'telemetry', 'night_driving', 'Night Driving Violation', 'warning', NULL, NULL, 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_restricted_zone', 'telemetry', 'restricted_zone', 'Restricted Zone Violation', 'critical', NULL, NULL, 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_unauth_move', 'telemetry', 'unauthorized_movement', 'Unauthorized Movement', 'critical', NULL, NULL, 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_off_hours', 'telemetry', 'off_hours_use', 'Off-Hours Vehicle Use', 'warning', NULL, NULL, 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_refill', 'fuel', 'refill', 'Fuel Refill Detected', 'info', 20.0, 'litres', 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_theft_susp', 'fuel', 'theft_suspicion', 'Fuel Theft Suspicion', 'critical', 15.0, 'litres', 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_abnormal_drain', 'fuel', 'abnormal_drain', 'Abnormal Fuel Drain', 'warning', 10.0, 'litres', 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_siphon', 'fuel', 'siphon_confirmed', 'Confirmed Fuel Siphoning', 'critical', 25.0, 'litres', 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_odo_rollback', 'telemetry', 'odometer_rollback', 'Odometer Tampering / Rollback', 'critical', NULL, NULL, 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_geofence_breach', 'geofence', 'geofence_breach', 'Geofence Perimeter Breach', 'warning', NULL, NULL, 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_sos', 'sos', 'emergency_sos', 'Driver Emergency SOS', 'critical', NULL, NULL, 60, 30, 2, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_compliance', 'compliance', 'compliance_blocked', 'Dispatch Compliance Blocked', 'blocker', NULL, NULL, 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}'),
 ('rule_ewb_expiry', 'ewaybill', 'ewaybill_expiring', 'E-Way Bill Expiring Soon', 'warning', 4.0, 'hours', 300, 60, 3, '{"in_app":true,"telegram":["critical","blocker"]}') ON CONFLICT DO NOTHING;
-- RBAC seeds for alerts (Spec 05 §11)
INSERT INTO permissions (name, description) VALUES
 ('alerts:read', 'View operational alerts'),
 ('alerts:update', 'Ack/resolve operational alerts') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('alerts:read','alerts:update') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('alerts:read','alerts:update') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 4, id FROM permissions WHERE name IN ('alerts:read') ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
 (SELECT id FROM permissions WHERE name IN ('alerts:read','alerts:update'));
DELETE FROM permissions WHERE name IN ('alerts:read','alerts:update');
DROP TABLE IF EXISTS notifications_preferences;
DROP TABLE IF EXISTS alerts;
DROP TABLE IF EXISTS rule_overrides;
DROP TABLE IF EXISTS alert_rules;
DROP TABLE IF EXISTS alert_sources;
