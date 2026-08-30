-- +goose Up
-- Migration 00112: Trip Stops & Multi-Leg Execution (Phase 8 Priority 5A)

CREATE TABLE IF NOT EXISTS trip_stops (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL,
    trip_id               TEXT NOT NULL,
    stop_sequence         INTEGER NOT NULL,
    stop_type             TEXT NOT NULL CHECK(stop_type IN ('pickup', 'drop', 'waypoint', 'hub_transit')),
    location_name         TEXT NOT NULL,
    address               TEXT NOT NULL,
    latitude              REAL,
    longitude             REAL,
    geofence_radius_m     REAL NOT NULL DEFAULT 100.0,
    consignee_name        TEXT,
    consignee_phone       TEXT,
    consignee_email       TEXT,
    planned_arrival       DATETIME,
    actual_arrival        DATETIME,
    actual_departure      DATETIME,
    status                TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'en_route', 'arrived', 'servicing', 'completed', 'skipped', 'failed')),
    otp_required          INTEGER NOT NULL DEFAULT 1,
    otp_code              TEXT,
    otp_expires_at        DATETIME,
    otp_verified_at       DATETIME,
    pod_required          INTEGER NOT NULL DEFAULT 1,
    pod_url               TEXT,
    pod_signature_url     TEXT,
    pod_verified_at       DATETIME,
    pod_notes             TEXT,
    failure_reason        TEXT,
    created_at            DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at            DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    FOREIGN KEY (trip_id) REFERENCES trips(id) ON DELETE CASCADE,
    UNIQUE (trip_id, stop_sequence)
);

CREATE INDEX IF NOT EXISTS idx_trip_stops_trip_seq
ON trip_stops(tenant_id, trip_id, stop_sequence);

CREATE TABLE IF NOT EXISTS stop_pod_attachments (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    stop_id    TEXT NOT NULL,
    trip_id    TEXT NOT NULL,
    file_url   TEXT NOT NULL,
    file_type  TEXT NOT NULL DEFAULT 'photo' CHECK(file_type IN ('photo', 'signature', 'document', 'invoice_copy')),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    FOREIGN KEY (stop_id) REFERENCES trip_stops(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_stop_pod_attachments_stop
ON stop_pod_attachments(tenant_id, stop_id);

-- +goose Down
DROP INDEX IF EXISTS idx_stop_pod_attachments_stop;
DROP TABLE IF EXISTS stop_pod_attachments;
DROP INDEX IF EXISTS idx_trip_stops_trip_seq;
DROP TABLE IF EXISTS trip_stops;
