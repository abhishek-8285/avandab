-- PG port of 00058_pnl_ops_experiments_founder.sql | status: PORTABLE | flags: none
-- +goose Up
-- Spec 16 §3.1: PNL daily snapshot, ops alerts, A/B experiments, founder signals + audit.
-- NOTE: tenant_id is free-form TEXT — no tenants table FK (migration index rule).

-- PNL daily snapshot per tenant
CREATE TABLE IF NOT EXISTS pnl_daily (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL DEFAULT '1',
    snapshot_date   DATE NOT NULL,
    revenue         REAL NOT NULL DEFAULT 0.0,
    expenses        REAL NOT NULL DEFAULT 0.0,
    fuel_costs      REAL NOT NULL DEFAULT 0.0,
    driver_payouts  REAL NOT NULL DEFAULT 0.0,
    maintenance     REAL NOT NULL DEFAULT 0.0,
    toll_costs      REAL NOT NULL DEFAULT 0.0,
    tds_deducted    REAL NOT NULL DEFAULT 0.0,
    net_profit      REAL NOT NULL DEFAULT 0.0,
    trip_count      INTEGER NOT NULL DEFAULT 0,
    vehicle_count   INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, snapshot_date)
);
CREATE INDEX IF NOT EXISTS idx_pnl_daily_tenant_date ON pnl_daily(tenant_id, snapshot_date);
-- Operational alerts (distinct from telemetry/alerting pipeline alerts — Spec 16 §4)
CREATE TABLE IF NOT EXISTS ops_alerts (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL DEFAULT '1',
    alert_type      TEXT NOT NULL CHECK (alert_type IN (
        'vehicle_breakdown','driver_absence','route_disruption','payment_delay',
        'compliance_breach','fuel_theft_confirmed','settlement_dispute','system_error')),
    severity        TEXT NOT NULL DEFAULT 'medium' CHECK (severity IN ('low','medium','high','critical')),
    title           TEXT NOT NULL,
    description     TEXT,
    entity_type     TEXT,  -- 'vehicle' | 'driver' | 'trip' | 'invoice' | null
    entity_id       TEXT,
    status          TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','acknowledged','resolved','dismissed')),
    acknowledged_by TEXT,
    acknowledged_at TIMESTAMPTZ,
    resolved_by     TEXT,
    resolved_at     TIMESTAMPTZ,
    resolution_note TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_ops_alerts_status ON ops_alerts(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_ops_alerts_type   ON ops_alerts(tenant_id, alert_type);
-- A/B experiments (Spec 16 §5)
CREATE TABLE IF NOT EXISTS experiments_spec16 (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL DEFAULT '1',
    name            TEXT NOT NULL,
    description     TEXT,
    variant_a       TEXT NOT NULL DEFAULT 'control',
    variant_b       TEXT NOT NULL DEFAULT 'treatment',
    traffic_split   REAL NOT NULL DEFAULT 50.0,  -- % assigned to variant_b
    status          TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','running','paused','completed','archived')),
    start_date      DATE,
    end_date        DATE,
    metric_name     TEXT,  -- primary metric to measure
    created_by      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_experiments_spec16_status ON experiments_spec16(tenant_id, status);
-- Experiment assignments (which variant a user/driver/vehicle/customer is in)
CREATE TABLE IF NOT EXISTS experiment_assignments (
    id              TEXT PRIMARY KEY,
    experiment_id   TEXT NOT NULL,
    tenant_id       TEXT NOT NULL DEFAULT '1',
    subject_type    TEXT NOT NULL CHECK (subject_type IN ('user','driver','vehicle','customer')),
    subject_id      TEXT NOT NULL,
    variant         TEXT NOT NULL CHECK (variant IN ('a','b')),
    assigned_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (experiment_id, subject_type, subject_id),
    FOREIGN KEY (experiment_id) REFERENCES experiments_spec16(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_exp_assignments_subject ON experiment_assignments(tenant_id, subject_type, subject_id);
-- Founder signals (key business metrics — Spec 16 §6)
CREATE TABLE IF NOT EXISTS founder_signals (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL DEFAULT '1',
    signal_type     TEXT NOT NULL CHECK (signal_type IN (
        'revenue_milestone','customer_churn_risk','driver_churn_risk',
        'vehicle_utilization','fuel_efficiency_trend','compliance_score',
        'settlement_dispute_spike','cash_flow_alert')),
    signal_value    REAL NOT NULL,
    threshold_value REAL,
    direction       TEXT NOT NULL CHECK (direction IN ('above','below','crossed')),
    metadata        TEXT NOT NULL DEFAULT '{}',  -- JSON payload
    acknowledged    INTEGER NOT NULL DEFAULT 0,
    acknowledged_by TEXT,
    acknowledged_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_founder_signals_type ON founder_signals(tenant_id, signal_type, created_at);
-- Founder audit trail (Spec 16 §7)
CREATE TABLE IF NOT EXISTS founder_audit (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL DEFAULT '1',
    actor_id        TEXT NOT NULL,
    actor_role      TEXT NOT NULL,
    action          TEXT NOT NULL,
    resource_type   TEXT NOT NULL,
    resource_id     TEXT NOT NULL,
    details         TEXT NOT NULL DEFAULT '{}',  -- JSON payload
    ip_address      TEXT,
    user_agent      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_founder_audit_actor    ON founder_audit(tenant_id, actor_id, created_at);
CREATE INDEX IF NOT EXISTS idx_founder_audit_resource ON founder_audit(tenant_id, resource_type, resource_id);
-- RBAC permissions for Spec 16 (Ops Alerts, PNL)
INSERT INTO permissions (name, description) VALUES
('ops_alerts:read', 'View operational alerts'),
('ops_alerts:update', 'Acknowledge/resolve/dismiss operational alerts'),
('pnl:read', 'View PNL snapshots and metrics'),
('pnl:write', 'Generate PNL snapshots') ON CONFLICT DO NOTHING;
-- Assign all to admin role (role id 1 per 00012 pattern)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions
WHERE name IN ('ops_alerts:read','ops_alerts:update','pnl:read','pnl:write') ON CONFLICT DO NOTHING;
-- Assign read/update to dispatcher role (role id 2)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions
WHERE name IN ('ops_alerts:read','ops_alerts:update','pnl:read') ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
(SELECT id FROM permissions WHERE name IN ('ops_alerts:read','ops_alerts:update','pnl:read','pnl:write'));
DELETE FROM permissions WHERE name IN ('ops_alerts:read','ops_alerts:update','pnl:read','pnl:write');
DROP TABLE IF EXISTS founder_audit;
DROP TABLE IF EXISTS founder_signals;
DROP TABLE IF EXISTS experiment_assignments;
DROP TABLE IF EXISTS experiments_spec16;
DROP TABLE IF EXISTS ops_alerts;
DROP TABLE IF EXISTS pnl_daily;
