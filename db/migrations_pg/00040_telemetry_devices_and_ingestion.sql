-- PG port of 00040_telemetry_devices_and_ingestion.sql | status: PORTABLE | flags: none
-- +goose Up
-- Device registry (GPS resale lifecycle)
CREATE TABLE telemetry_devices (
    id                 TEXT PRIMARY KEY,
    tenant_id          TEXT NOT NULL DEFAULT '1',
    imei               TEXT NOT NULL UNIQUE,
    serial_number      TEXT,
    firmware_version   TEXT,
    sim_number         TEXT,
    iccid              TEXT,
    warranty_until     DATE,
    device_type        TEXT NOT NULL DEFAULT 'hardware'
                       CHECK (device_type IN ('hardware','mobile_app','obd_dongle')),
    status             TEXT NOT NULL DEFAULT 'inventory'
                       CHECK (status IN ('inventory','assigned','active','retired','quarantined')),
    vehicle_id         TEXT,
    customer_id        TEXT,
    activated_at       TIMESTAMPTZ,
    last_seen_at       TIMESTAMPTZ,
    device_secret_hash TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (vehicle_id)  REFERENCES vehicles(id),
    FOREIGN KEY (customer_id) REFERENCES customers(id)
);
CREATE UNIQUE INDEX idx_telemetry_devices_vehicle
    ON telemetry_devices(vehicle_id) WHERE vehicle_id IS NOT NULL;
CREATE INDEX idx_telemetry_devices_status ON telemetry_devices(status);
CREATE INDEX idx_telemetry_devices_tenant ON telemetry_devices(tenant_id, status);
-- Append-only raw frame log (dedup + retention + audit)
CREATE TABLE telemetry_raw_events (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL DEFAULT '1',
    imei            TEXT NOT NULL,
    device_time     TIMESTAMPTZ NOT NULL,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    provider        TEXT NOT NULL DEFAULT 'own',
    provider_msg_id TEXT,
    payload         TEXT NOT NULL,
    FOREIGN KEY (imei) REFERENCES telemetry_devices(imei)
);
CREATE UNIQUE INDEX idx_raw_dedup ON telemetry_raw_events(imei, provider_msg_id)
    WHERE provider_msg_id IS NOT NULL;
CREATE INDEX idx_raw_received ON telemetry_raw_events(received_at);
CREATE INDEX idx_raw_imei_time ON telemetry_raw_events(imei, device_time DESC);
-- Unknown-IMEI quarantine queue (admin resolution UI)
CREATE TABLE device_quarantine (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL DEFAULT '1',
    imei        TEXT NOT NULL,
    source      TEXT NOT NULL DEFAULT 'own' CHECK (source IN ('own','loconav','wheelseye')),
    raw_payload TEXT NOT NULL,
    reason      TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','rejected')),
    resolved_by TEXT,
    resolved_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (resolved_by) REFERENCES users(id)
);
CREATE INDEX idx_quarantine_status ON device_quarantine(status, created_at);
-- Provider polling state (store-and-forward during downtime)
CREATE TABLE provider_poll_state (
    provider             TEXT PRIMARY KEY,
    tenant_id            TEXT NOT NULL DEFAULT '1',
    last_poll_at         TIMESTAMPTZ,
    last_success_at      TIMESTAMPTZ,
    cursor               TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    backoff_until        TIMESTAMPTZ
);
-- RBAC seeds (casbin loads role_permissions -> permissions, see 00012 pattern)
INSERT INTO permissions (name, description) VALUES
('telemetry:read',   'View devices, positions, quarantine queue'),
('telemetry:write',  'Ingest telemetry (API-level, machine routes)'),
('telemetry:update', 'Provision devices: register, assign, activate, retire, resolve quarantine'),
('telemetry:delete', 'Delete devices / quarantine entries') ON CONFLICT DO NOTHING;
-- admin (role 1): everything
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name LIKE 'telemetry:%' ON CONFLICT DO NOTHING;
-- dispatcher (role 2): view + provision, no delete
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('telemetry:read','telemetry:update') ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE name LIKE 'telemetry:%');
DELETE FROM permissions WHERE name LIKE 'telemetry:%';
DROP TABLE IF EXISTS provider_poll_state;
DROP TABLE IF EXISTS device_quarantine;
DROP TABLE IF EXISTS telemetry_raw_events;
DROP TABLE IF EXISTS telemetry_devices;
