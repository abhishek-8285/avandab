-- +goose Up
-- PG port of 00178_video_telematics.sql
-- Video Telematics & AI Dashcam Safety (FleetX Parity P3 3.12)
-- Dual-facing dashcam binding, AI DMS (driver monitoring: drowsiness, distraction, phone, smoking),
-- ADAS (forward collision, lane departure, tailgating), live video stream brokering, and safety coaching.

CREATE TABLE IF NOT EXISTS vehicle_dashcam_profiles (
    vehicle_id                TEXT PRIMARY KEY REFERENCES vehicles(id) ON DELETE CASCADE,
    tenant_id                 TEXT NOT NULL REFERENCES tenants(id),
    camera_make_model         TEXT NOT NULL DEFAULT 'FleetX AI Dashcam Pro',
    camera_serial             TEXT NOT NULL,
    dual_facing               BOOLEAN NOT NULL DEFAULT TRUE,
    adas_enabled              BOOLEAN NOT NULL DEFAULT TRUE,
    dms_enabled               BOOLEAN NOT NULL DEFAULT TRUE,
    live_stream_supported     BOOLEAN NOT NULL DEFAULT TRUE,
    firmware_version          TEXT NOT NULL DEFAULT 'v2.4.0-edge-ai',
    status                    TEXT NOT NULL DEFAULT 'ONLINE' CHECK (status IN ('ONLINE', 'OFFLINE', 'STANDBY', 'FAULT')),
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dashcam_profiles_tenant_status
    ON vehicle_dashcam_profiles(tenant_id, status);

CREATE TABLE IF NOT EXISTS video_safety_events (
    id                        TEXT PRIMARY KEY,
    tenant_id                 TEXT NOT NULL REFERENCES tenants(id),
    vehicle_id                TEXT NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
    driver_id                 TEXT REFERENCES drivers(id) ON DELETE SET NULL,
    trip_id                   TEXT REFERENCES trips(id) ON DELETE SET NULL,
    category                  TEXT NOT NULL CHECK (category IN ('DMS', 'ADAS', 'MANUAL_SOS')),
    event_type                TEXT NOT NULL CHECK (event_type IN (
        'DROWSINESS', 'CELLPHONE_DISTRACTION', 'SEATBELT_UNBUCKLED', 'SMOKING', 'YAWNING', 'FACE_MISMATCH', 'CAMERA_OBSTRUCTION',
        'FORWARD_COLLISION_WARNING', 'LANE_DEPARTURE_WARNING', 'TAILGATING', 'PEDESTRIAN_COLLISION_WARNING', 'HARSH_CORNERING', 'HARSH_BRAKING', 'PANIC_SOS'
    )),
    severity                  TEXT NOT NULL DEFAULT 'MEDIUM' CHECK (severity IN ('LOW', 'MEDIUM', 'HIGH', 'CRITICAL')),
    event_time                TIMESTAMPTZ NOT NULL,
    speed_kmh                 REAL NOT NULL DEFAULT 0.0,
    latitude                  REAL,
    longitude                 REAL,
    location_name             TEXT NOT NULL DEFAULT '',
    media_clip_url            TEXT NOT NULL DEFAULT '',
    thumbnail_url             TEXT NOT NULL DEFAULT '',
    duration_seconds          INTEGER NOT NULL DEFAULT 15,
    review_status             TEXT NOT NULL DEFAULT 'PENDING_REVIEW' CHECK (review_status IN ('PENDING_REVIEW', 'COACHED', 'DISMISSED', 'ESCALATED')),
    coaching_note             TEXT NOT NULL DEFAULT '',
    reviewed_by               TEXT NOT NULL DEFAULT '',
    reviewed_at               TIMESTAMPTZ,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_video_events_tenant_veh_time
    ON video_safety_events(tenant_id, vehicle_id, event_time);

CREATE INDEX IF NOT EXISTS idx_video_events_tenant_review_sev
    ON video_safety_events(tenant_id, review_status, severity);

CREATE INDEX IF NOT EXISTS idx_video_events_tenant_driver
    ON video_safety_events(tenant_id, driver_id);

CREATE TABLE IF NOT EXISTS video_livestream_sessions (
    id                        TEXT PRIMARY KEY,
    tenant_id                 TEXT NOT NULL REFERENCES tenants(id),
    vehicle_id                TEXT NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
    channel                   TEXT NOT NULL DEFAULT 'ROAD_FACING' CHECK (channel IN ('ROAD_FACING', 'CABIN_FACING', 'CARGO_FACING')),
    stream_protocol           TEXT NOT NULL DEFAULT 'WEBRTC' CHECK (stream_protocol IN ('WEBRTC', 'HLS', 'RTSP')),
    stream_url                TEXT NOT NULL,
    session_token             TEXT NOT NULL,
    status                    TEXT NOT NULL DEFAULT 'INITIATED' CHECK (status IN ('INITIATED', 'ACTIVE', 'ENDED', 'FAILED')),
    started_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at                  TIMESTAMPTZ,
    requested_by              TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_video_livestream_tenant_veh
    ON video_livestream_sessions(tenant_id, vehicle_id, status);

CREATE TABLE IF NOT EXISTS driver_safety_scorecards (
    id                        TEXT PRIMARY KEY,
    tenant_id                 TEXT NOT NULL REFERENCES tenants(id),
    driver_id                 TEXT NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    period_start              TIMESTAMPTZ NOT NULL,
    period_end                TIMESTAMPTZ NOT NULL,
    distance_km               REAL NOT NULL DEFAULT 0.0,
    safety_score              REAL NOT NULL DEFAULT 100.0,
    adas_event_count          INTEGER NOT NULL DEFAULT 0,
    dms_event_count          INTEGER NOT NULL DEFAULT 0,
    critical_event_count      INTEGER NOT NULL DEFAULT 0,
    coached_event_count       INTEGER NOT NULL DEFAULT 0,
    risk_tier                 TEXT NOT NULL DEFAULT 'LOW_RISK' CHECK (risk_tier IN ('LOW_RISK', 'MODERATE_RISK', 'HIGH_RISK', 'CRITICAL_RISK')),
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_safety_scorecard_tenant_driver
    ON driver_safety_scorecards(tenant_id, driver_id, period_start);

-- RBAC Seeding & Grants
INSERT INTO permissions (name, description) VALUES
('video:read', 'View dashcam fleet live streams, safety incident video clips, and driver safety scorecards'),
('video:write', 'Configure dashcams, initiate live stream sessions, and coach drivers on safety events')
ON CONFLICT (name) DO NOTHING;

-- Grant to Role 1 (Admin)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('video:read', 'video:write')
ON CONFLICT DO NOTHING;

-- Grant to Role 2 (Dispatcher / Ops)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('video:read', 'video:write')
ON CONFLICT DO NOTHING;

-- Grant to Role 3 (Maintenance / Safety)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 3, id FROM permissions WHERE name IN ('video:read', 'video:write')
ON CONFLICT DO NOTHING;

-- Grant to Role 6 (Org Admin)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name IN ('video:read', 'video:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name IN ('video:read', 'video:write'));
DELETE FROM permissions WHERE name IN ('video:read', 'video:write');

DROP INDEX IF EXISTS idx_safety_scorecard_tenant_driver;
DROP TABLE IF EXISTS driver_safety_scorecards;

DROP INDEX IF EXISTS idx_video_livestream_tenant_veh;
DROP TABLE IF EXISTS video_livestream_sessions;

DROP INDEX IF EXISTS idx_video_events_tenant_driver;
DROP INDEX IF EXISTS idx_video_events_tenant_review_sev;
DROP INDEX IF EXISTS idx_video_events_tenant_veh_time;
DROP TABLE IF EXISTS video_safety_events;

DROP INDEX IF EXISTS idx_dashcam_profiles_tenant_status;
DROP TABLE IF EXISTS vehicle_dashcam_profiles;
