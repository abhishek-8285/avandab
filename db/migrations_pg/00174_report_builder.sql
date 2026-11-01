-- +goose Up
-- PG port of 00174_report_builder.sql
-- Report Builder & Scheduled Delivery Engine (FleetX Parity P3.4)
-- Dynamic query generation across fleet datasets (Trips, Vehicles, Drivers, Fuel, FASTag,
-- Tyres, Inventory, Freight Audit, Invoices), custom report templates, and automated delivery schedules.

CREATE TABLE IF NOT EXISTS custom_reports (
    id               TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL REFERENCES tenants(id),
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    dataset          TEXT NOT NULL CHECK (dataset IN ('TRIPS', 'VEHICLES', 'DRIVERS', 'FUEL', 'FASTAG', 'TYRES', 'INVENTORY', 'FREIGHT_AUDIT', 'EXPENSES', 'INVOICES')),
    selected_columns TEXT NOT NULL DEFAULT '[]',
    filters          TEXT NOT NULL DEFAULT '[]',
    group_by         TEXT NOT NULL DEFAULT '',
    aggregations     TEXT NOT NULL DEFAULT '[]',
    sort_by          TEXT NOT NULL DEFAULT '',
    sort_direction   TEXT NOT NULL DEFAULT 'DESC' CHECK (sort_direction IN ('ASC', 'DESC')),
    chart_type       TEXT NOT NULL DEFAULT 'TABLE' CHECK (chart_type IN ('TABLE', 'BAR', 'LINE', 'PIE')),
    is_system        BOOLEAN NOT NULL DEFAULT FALSE,
    created_by       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_custom_reports_tenant_dataset
    ON custom_reports(tenant_id, dataset);

CREATE INDEX IF NOT EXISTS idx_custom_reports_tenant_system
    ON custom_reports(tenant_id, is_system);

CREATE TABLE IF NOT EXISTS report_schedules (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    report_id       TEXT NOT NULL REFERENCES custom_reports(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    frequency       TEXT NOT NULL CHECK (frequency IN ('DAILY', 'WEEKLY', 'MONTHLY')),
    cron_expression TEXT NOT NULL DEFAULT '',
    format          TEXT NOT NULL DEFAULT 'CSV' CHECK (format IN ('CSV', 'JSON', 'PDF')),
    channel         TEXT NOT NULL DEFAULT 'EMAIL' CHECK (channel IN ('EMAIL', 'WEBHOOK')),
    recipients      TEXT NOT NULL DEFAULT '[]',
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    last_run_at     TIMESTAMPTZ,
    next_run_at     TIMESTAMPTZ,
    last_status     TEXT NOT NULL DEFAULT '',
    last_error      TEXT NOT NULL DEFAULT '',
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_report_schedules_tenant_active
    ON report_schedules(tenant_id, is_active);

CREATE INDEX IF NOT EXISTS idx_report_schedules_next_run
    ON report_schedules(is_active, next_run_at);

CREATE TABLE IF NOT EXISTS report_executions (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL REFERENCES tenants(id),
    report_id             TEXT NOT NULL REFERENCES custom_reports(id) ON DELETE CASCADE,
    schedule_id           TEXT REFERENCES report_schedules(id) ON DELETE SET NULL,
    format                TEXT NOT NULL DEFAULT 'CSV' CHECK (format IN ('CSV', 'JSON', 'PDF')),
    status                TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'RUNNING', 'COMPLETED', 'FAILED')),
    row_count             INTEGER NOT NULL DEFAULT 0,
    file_size_bytes       INTEGER NOT NULL DEFAULT 0,
    execution_duration_ms INTEGER NOT NULL DEFAULT 0,
    error_message         TEXT NOT NULL DEFAULT '',
    triggered_by          TEXT NOT NULL DEFAULT 'MANUAL' CHECK (triggered_by IN ('MANUAL', 'SCHEDULE')),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at          TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_report_executions_report
    ON report_executions(tenant_id, report_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_report_executions_status
    ON report_executions(tenant_id, status);

-- RBAC Seeding & Grants
INSERT INTO permissions (name, description) VALUES
('reports:write', 'Create, update, and schedule custom fleet reports')
ON CONFLICT (name) DO NOTHING;

-- Grant to Role 1 (Admin)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name = 'reports:write'
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- Grant to Role 2 (Dispatcher / Ops)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name = 'reports:write'
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- Grant to Role 6 (Org Admin)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name = 'reports:write'
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name = 'reports:write');
DELETE FROM permissions WHERE name = 'reports:write';

DROP INDEX IF EXISTS idx_report_executions_status;
DROP INDEX IF EXISTS idx_report_executions_report;
DROP TABLE IF EXISTS report_executions;

DROP INDEX IF EXISTS idx_report_schedules_next_run;
DROP INDEX IF EXISTS idx_report_schedules_tenant_active;
DROP TABLE IF EXISTS report_schedules;

DROP INDEX IF EXISTS idx_custom_reports_tenant_system;
DROP INDEX IF EXISTS idx_custom_reports_tenant_dataset;
DROP TABLE IF EXISTS custom_reports;
