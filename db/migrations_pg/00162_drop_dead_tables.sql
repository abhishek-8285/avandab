-- +goose Up
-- 00162: drop dead tables — PG port of 00162_drop_dead_tables.sql
-- alert_sources KEPT: live alert_rules.source FK parent.
-- Down recreates empty shells (data unrecoverable); keeps roundtrips working.

DROP TABLE IF EXISTS i18n_keys;
DROP TABLE IF EXISTS notifications_preferences;
DROP TABLE IF EXISTS revoked_refresh_tokens;
DROP TABLE IF EXISTS provider_poll_state;
DROP TABLE IF EXISTS route_constraints;
DROP TABLE IF EXISTS offline_sync_log;
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS telemetry_events;

-- +goose Down
-- Schema-only rollback: recreates empty shells so down/up roundtrips keep
-- later migrations working. Data is NOT restored (unrecoverable by design).
CREATE TABLE IF NOT EXISTS i18n_keys (
    key TEXT PRIMARY KEY,
    en TEXT NOT NULL,
    hi TEXT NOT NULL DEFAULT '',
    ta TEXT NOT NULL DEFAULT '',
    te TEXT NOT NULL DEFAULT '',
    kn TEXT NOT NULL DEFAULT '',
    mr TEXT NOT NULL DEFAULT '',
    gu TEXT NOT NULL DEFAULT ''
);
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
CREATE TABLE IF NOT EXISTS revoked_refresh_tokens (
    token_hash TEXT PRIMARY KEY,
    revoked_at TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    user_id TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_revoked_refresh_user ON revoked_refresh_tokens(user_id);
CREATE TABLE provider_poll_state (
    provider             TEXT PRIMARY KEY,
    tenant_id            TEXT NOT NULL DEFAULT '1',
    last_poll_at         TIMESTAMPTZ,
    last_success_at      TIMESTAMPTZ,
    cursor               TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    backoff_until        TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS route_constraints (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES route_optimization_jobs(id) ON DELETE CASCADE,
    constraint_type TEXT NOT NULL CHECK (constraint_type IN ('time_window','capacity','terrain','driver_hours','skill')),
    constraint_json TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
CREATE INDEX IF NOT EXISTS idx_route_constraints_job ON route_constraints(job_id);
CREATE TABLE IF NOT EXISTS offline_sync_log (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '1',
    user_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    payload TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TEXT NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
CREATE TABLE IF NOT EXISTS telemetry_events (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    client_event_id TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
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
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_audit_events_entity ON audit_events(tenant_id, entity_type, entity_id);
