-- +goose Up
-- 00108 Driver lifecycle refactor: decoupled compliance, temporal vehicle assignments,
-- telemetry sessions, auditable payout accounts, vehicle compliance documents, and audit logs.

CREATE TABLE IF NOT EXISTS driver_licenses (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    driver_id TEXT NOT NULL,
    license_number TEXT NOT NULL,
    issuing_authority TEXT,
    issued_on DATE,
    expires_on DATE NOT NULL,
    is_current INTEGER NOT NULL DEFAULT 1,
    superseded_at DATETIME,
    verification_status TEXT NOT NULL DEFAULT 'unverified' 
        CHECK (verification_status IN ('unverified', 'pending', 'verified', 'rejected', 'expired')),
    verified_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_driver_licenses_lookup ON driver_licenses(tenant_id, license_number);
CREATE INDEX IF NOT EXISTS idx_driver_licenses_driver ON driver_licenses(tenant_id, driver_id, is_current);

CREATE TABLE IF NOT EXISTS driver_license_classes (
    id TEXT PRIMARY KEY,
    license_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    class_code TEXT NOT NULL,
    valid_from DATE,
    valid_until DATE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(license_id, class_code)
);
CREATE INDEX IF NOT EXISTS idx_dlc_tenant ON driver_license_classes(tenant_id, license_id);

CREATE TABLE IF NOT EXISTS driver_identities (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    driver_id TEXT NOT NULL,
    identity_type TEXT NOT NULL CHECK (identity_type IN ('aadhaar', 'pan', 'voter_id')),
    masked_value TEXT NOT NULL,
    encrypted_value TEXT NOT NULL,
    lookup_hash TEXT NOT NULL,
    vault_reference TEXT,
    verification_status TEXT NOT NULL DEFAULT 'unverified'
        CHECK (verification_status IN ('unverified', 'pending', 'verified', 'rejected')),
    verified_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tenant_id, identity_type, lookup_hash)
);
CREATE INDEX IF NOT EXISTS idx_driver_identities_driver ON driver_identities(tenant_id, driver_id);

CREATE TABLE IF NOT EXISTS driver_compliance_documents (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    driver_id TEXT NOT NULL,
    document_type TEXT NOT NULL CHECK (document_type IN ('dl_front', 'dl_back', 'aadhaar_front', 'aadhaar_back', 'pan_card', 'profile_photo', 'bank_passbook', 'police_clearance')),
    storage_key TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    file_size_bytes INTEGER NOT NULL,
    document_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted', 'processing', 'verified', 'rejected', 'expiring_soon', 'expired')),
    rejection_reason TEXT,
    verified_by TEXT,
    verified_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_driver_compliance_documents_driver ON driver_compliance_documents(tenant_id, driver_id, document_type);

CREATE TABLE IF NOT EXISTS driver_payout_accounts (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    driver_id TEXT NOT NULL,
    account_holder_name TEXT NOT NULL,
    account_number_encrypted TEXT NOT NULL,
    account_number_masked TEXT NOT NULL,
    ifsc_code TEXT NOT NULL,
    bank_name TEXT NOT NULL,
    is_primary INTEGER NOT NULL DEFAULT 1,
    verification_status TEXT NOT NULL DEFAULT 'unverified'
        CHECK (verification_status IN ('unverified', 'penny_drop_pending', 'verified', 'rejected')),
    verification_reference TEXT,
    verified_by TEXT,
    verified_at DATETIME,
    rejected_reason TEXT,
    valid_from DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    valid_until DATETIME,
    hold_payouts INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_driver_payout_driver ON driver_payout_accounts(tenant_id, driver_id);

CREATE TABLE IF NOT EXISTS vehicle_ownership (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    vehicle_id TEXT NOT NULL,
    owner_party_type TEXT NOT NULL CHECK (owner_party_type IN ('driver', 'company', 'transporter_partner', 'leasing_company')),
    owner_party_id TEXT NOT NULL,
    valid_from DATE NOT NULL,
    valid_until DATE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_vehicle_ownership_vehicle ON vehicle_ownership(tenant_id, vehicle_id);

CREATE TABLE IF NOT EXISTS vehicle_claims (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    driver_id TEXT NOT NULL,
    registration_number TEXT NOT NULL,
    rc_document_id TEXT,
    status TEXT NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted', 'under_review', 'approved', 'rejected', 'disputed')),
    reviewed_by TEXT,
    reviewed_at DATETIME,
    rejection_reason TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_vehicle_claims_driver ON vehicle_claims(tenant_id, driver_id);

CREATE TABLE IF NOT EXISTS driver_vehicle_assignments (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    driver_id TEXT NOT NULL,
    vehicle_id TEXT NOT NULL,
    assignment_type TEXT NOT NULL CHECK (assignment_type IN ('company_assigned', 'owner_operator_claim', 'temporary_relief')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'ended', 'rejected')),
    started_at DATETIME,
    ended_at DATETIME,
    assigned_by TEXT,
    accepted_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_dva_tenant_driver ON driver_vehicle_assignments(tenant_id, driver_id, status);
CREATE INDEX IF NOT EXISTS idx_dva_tenant_vehicle ON driver_vehicle_assignments(tenant_id, vehicle_id, status);

CREATE TABLE IF NOT EXISTS vehicle_compliance_documents (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    vehicle_id TEXT NOT NULL,
    document_type TEXT NOT NULL CHECK (document_type IN ('rc', 'insurance', 'fitness', 'permit', 'puc', 'road_tax')),
    document_number TEXT NOT NULL,
    storage_key TEXT,
    issued_on DATE,
    expires_on DATE NOT NULL,
    is_current INTEGER NOT NULL DEFAULT 1,
    superseded_at DATETIME,
    verification_status TEXT NOT NULL DEFAULT 'unverified'
        CHECK (verification_status IN ('unverified', 'pending', 'verified', 'rejected', 'expired')),
    verified_by TEXT,
    verified_at DATETIME,
    rejection_reason TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_vcd_vehicle ON vehicle_compliance_documents(tenant_id, vehicle_id, document_type, is_current);

CREATE TABLE IF NOT EXISTS telemetry_installations (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    app_installation_id TEXT NOT NULL UNIQUE,
    platform TEXT NOT NULL CHECK (platform IN ('android', 'ios', 'hardware_gps', 'obd_dongle')),
    app_version TEXT NOT NULL,
    device_model TEXT NOT NULL,
    os_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_telemetry_installations_tenant ON telemetry_installations(tenant_id, app_installation_id);

CREATE TABLE IF NOT EXISTS telemetry_sessions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    driver_id TEXT NOT NULL,
    vehicle_id TEXT,
    trip_id TEXT,
    session_type TEXT NOT NULL CHECK (session_type IN ('on_duty', 'trip_active', 'relief_standby')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'closed', 'interrupted')),
    start_reason TEXT NOT NULL CHECK (start_reason IN ('APP_AVAILABLE', 'TRIP_STARTED', 'MANUAL_START', 'SYSTEM_RECOVERY')),
    end_reason TEXT CHECK (end_reason IN ('TRIP_COMPLETED', 'DRIVER_UNAVAILABLE', 'LOGOUT', 'DEVICE_REVOKED', 'PERMISSION_REVOKED', 'SYSTEM_TIMEOUT')),
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME,
    total_distance_km REAL NOT NULL DEFAULT 0.0,
    positions_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_telemetry_sessions_active ON telemetry_sessions(tenant_id, status, vehicle_id);

CREATE TABLE IF NOT EXISTS telemetry_events (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    client_event_id TEXT NOT NULL,
    occurred_at DATETIME NOT NULL,
    received_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    speed REAL NOT NULL DEFAULT 0.0,
    accuracy REAL,
    heading REAL,
    altitude REAL,
    raw_payload TEXT,
    UNIQUE(tenant_id, session_id, client_event_id)
);
CREATE INDEX IF NOT EXISTS idx_telemetry_events_time ON telemetry_events(session_id, occurred_at DESC);

CREATE TABLE IF NOT EXISTS driver_vehicle_latest_positions (
    tenant_id TEXT NOT NULL,
    vehicle_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    driver_id TEXT NOT NULL,
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    accuracy REAL,
    speed REAL NOT NULL DEFAULT 0.0,
    heading REAL,
    occurred_at DATETIME NOT NULL,
    received_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    source TEXT NOT NULL DEFAULT 'mobile_session',
    PRIMARY KEY (tenant_id, vehicle_id)
);

CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    actor_user_id TEXT,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    action TEXT NOT NULL,
    old_state TEXT,
    new_state TEXT,
    reason TEXT,
    request_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_audit_events_entity ON audit_events(tenant_id, entity_type, entity_id);

CREATE TABLE IF NOT EXISTS verification_attempts (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    provider_reference TEXT,
    status TEXT NOT NULL CHECK (status IN ('initiated', 'success', 'failed', 'timeout')),
    requested_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    failure_code TEXT,
    failure_reason TEXT
);
CREATE INDEX IF NOT EXISTS idx_verification_attempts_entity ON verification_attempts(tenant_id, entity_type, entity_id);

CREATE TABLE IF NOT EXISTS driver_onboarding (
    driver_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    current_step TEXT NOT NULL DEFAULT 'profile' CHECK (current_step IN ('profile', 'ownership_choice', 'vehicle_binding', 'kyc_documents', 'bank_details', 'pending_approval', 'completed')),
    identity_status TEXT NOT NULL DEFAULT 'pending',
    license_status TEXT NOT NULL DEFAULT 'pending',
    vehicle_status TEXT NOT NULL DEFAULT 'pending',
    bank_status TEXT NOT NULL DEFAULT 'pending',
    overall_status TEXT NOT NULL DEFAULT 'in_progress' CHECK (overall_status IN ('in_progress', 'submitted', 'approved', 'rejected')),
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_driver_onboarding_status ON driver_onboarding(tenant_id, overall_status);

-- +goose Down
DROP TABLE IF EXISTS driver_onboarding;
DROP TABLE IF EXISTS verification_attempts;
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS driver_vehicle_latest_positions;
DROP TABLE IF EXISTS telemetry_events;
DROP TABLE IF EXISTS telemetry_sessions;
DROP TABLE IF EXISTS telemetry_installations;
DROP TABLE IF EXISTS vehicle_compliance_documents;
DROP TABLE IF EXISTS driver_vehicle_assignments;
DROP TABLE IF EXISTS vehicle_claims;
DROP TABLE IF EXISTS vehicle_ownership;
DROP TABLE IF EXISTS driver_payout_accounts;
DROP TABLE IF EXISTS driver_compliance_documents;
DROP TABLE IF EXISTS driver_identities;
DROP TABLE IF EXISTS driver_license_classes;
DROP TABLE IF EXISTS driver_licenses;
