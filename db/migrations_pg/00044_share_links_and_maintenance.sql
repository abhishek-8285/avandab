-- PG port of 00044_share_links_and_maintenance.sql | status: PORTABLE | flags: none
-- +goose Up
-- ── Share links (bind to TRIP, not vehicle) ──────────────────────────
CREATE TABLE share_links (
    id                 TEXT PRIMARY KEY,
    trip_id            TEXT NOT NULL,
    token_hash         TEXT NOT NULL UNIQUE,          -- SHA-256 of raw token
    pin_hash           TEXT,                          -- NULL = no PIN; SHA-256(pin||salt)
    pin_salt           TEXT,
    created_by         TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at         TIMESTAMPTZ NOT NULL,             -- sliding: refreshed on valid view, capped at max_ttl
    last_viewed_at     TIMESTAMPTZ,
    view_count         INTEGER NOT NULL DEFAULT 0,
    failed_pin_attempts INTEGER NOT NULL DEFAULT 0,
    locked_until       TIMESTAMPTZ,                      -- 15 min lock after 5 failed PIN attempts
    revoked_at         TIMESTAMPTZ,                      -- NULL = active
    FOREIGN KEY (trip_id) REFERENCES trips(id),
    FOREIGN KEY (created_by) REFERENCES users(id)
);
CREATE INDEX idx_share_links_trip   ON share_links(trip_id);
CREATE INDEX idx_share_links_expires ON share_links(expires_at);
-- ── Preventive maintenance ───────────────────────────────────────────
CREATE TABLE maintenance_schedules (
    id            TEXT PRIMARY KEY,
    vehicle_id    TEXT NOT NULL,
    service_type  TEXT NOT NULL CHECK (service_type IN
                  ('oil_change','brake','tyre','battery','engine','fitness','insurance','permit','general')),
    interval_km   REAL,                               -- due every N km
    interval_days INTEGER,                            -- or every N days
    last_done_km  REAL,
    last_done_at  TIMESTAMPTZ,
    due_km        REAL,                               -- absolute odometer threshold
    due_at        TIMESTAMPTZ,                           -- absolute date threshold
    active        INTEGER NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id)
);
CREATE INDEX idx_maint_sched_vehicle ON maintenance_schedules(vehicle_id, active);
CREATE TABLE maintenance_records (
    id            TEXT PRIMARY KEY,
    vehicle_id    TEXT NOT NULL,
    schedule_id   TEXT,
    service_type  TEXT NOT NULL,
    performed_at  TIMESTAMPTZ NOT NULL,
    odometer_km   REAL,
    cost          REAL,
    vendor        TEXT,
    notes         TEXT,
    recorded_by   TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (schedule_id) REFERENCES maintenance_schedules(id)
);
CREATE INDEX idx_maint_rec_vehicle ON maintenance_records(vehicle_id, performed_at);
CREATE TABLE dtc_events (
    id          TEXT PRIMARY KEY,
    vehicle_id  TEXT NOT NULL,
    trip_id     TEXT,
    dtc_code    TEXT NOT NULL,
    severity    TEXT NOT NULL CHECK (severity IN ('info','warning','critical')),
    description TEXT,
    raw_payload TEXT,                                 -- JSON as received
    occurred_at TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ,                             -- set by maintenance record or admin
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (trip_id) REFERENCES trips(id)
);
CREATE INDEX idx_dtc_vehicle ON dtc_events(vehicle_id, occurred_at);
-- DTC storm dedupe: one row per (vehicle, code, 1-minute bucket)
CREATE UNIQUE INDEX idx_dtc_dedupe ON dtc_events(vehicle_id, dtc_code, occurred_at);
-- ── Company defaults ─────────────────────────────────────────────────
ALTER TABLE company_settings ADD COLUMN maintenance_default_interval_km REAL;
ALTER TABLE company_settings ADD COLUMN maintenance_default_interval_days INTEGER;
ALTER TABLE company_settings ADD COLUMN maintenance_critical_dtcs TEXT;
-- comma-separated codes

-- ── Admin override (flag itself lives in 00042) ──────────────────────
ALTER TABLE vehicles ADD COLUMN maintenance_override_by TEXT;
ALTER TABLE vehicles ADD COLUMN maintenance_override_at TIMESTAMPTZ;
ALTER TABLE vehicles ADD COLUMN maintenance_override_reason TEXT;
-- ── RBAC seeds (re-seed explicitly; 00012's blanket insert does not re-run) ──
INSERT INTO permissions (name, description) VALUES
('shares:create',  'Create trip share links'),
('shares:read',    'View share links'),
('shares:revoke',  'Revoke share links'),
('maintenance:read',   'Read maintenance data'),
('maintenance:create', 'Record maintenance work'),
('maintenance:update', 'Update schedules / override blocks') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions
WHERE name IN ('shares:create','shares:read','shares:revoke',
               'maintenance:read','maintenance:create','maintenance:update') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions
WHERE name IN ('shares:create','shares:read','shares:revoke','maintenance:read') ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE name IN ('shares:create','shares:read','shares:revoke',
   'maintenance:read','maintenance:create','maintenance:update'));
DELETE FROM permissions WHERE name IN ('shares:create','shares:read','shares:revoke',
   'maintenance:read','maintenance:create','maintenance:update');
DROP TABLE IF EXISTS dtc_events;
DROP TABLE IF EXISTS maintenance_records;
DROP TABLE IF EXISTS maintenance_schedules;
DROP TABLE IF EXISTS share_links;
ALTER TABLE company_settings DROP COLUMN maintenance_default_interval_km;
ALTER TABLE company_settings DROP COLUMN maintenance_default_interval_days;
ALTER TABLE company_settings DROP COLUMN maintenance_critical_dtcs;
ALTER TABLE vehicles DROP COLUMN maintenance_override_by;
ALTER TABLE vehicles DROP COLUMN maintenance_override_at;
ALTER TABLE vehicles DROP COLUMN maintenance_override_reason;
