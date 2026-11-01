-- +goose Up
-- 00174: Report Builder & Scheduled Delivery Engine (FleetX Parity P3.4)
-- Dynamic query generation across fleet datasets (Trips, Vehicles, Drivers, Fuel, FASTag,
-- Tyres, Inventory, Freight Audit, Invoices), custom report templates, and automated delivery schedules.

CREATE TABLE IF NOT EXISTS custom_reports (
    id               TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL,
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
    is_system        INTEGER NOT NULL DEFAULT 0 CHECK (is_system IN (0, 1)),
    created_by       TEXT NOT NULL DEFAULT '',
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX IF NOT EXISTS idx_custom_reports_tenant_dataset
    ON custom_reports(tenant_id, dataset);

CREATE INDEX IF NOT EXISTS idx_custom_reports_tenant_system
    ON custom_reports(tenant_id, is_system);

CREATE TABLE IF NOT EXISTS report_schedules (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    report_id       TEXT NOT NULL,
    name            TEXT NOT NULL,
    frequency       TEXT NOT NULL CHECK (frequency IN ('DAILY', 'WEEKLY', 'MONTHLY')),
    cron_expression TEXT NOT NULL DEFAULT '',
    format          TEXT NOT NULL DEFAULT 'CSV' CHECK (format IN ('CSV', 'JSON', 'PDF')),
    channel         TEXT NOT NULL DEFAULT 'EMAIL' CHECK (channel IN ('EMAIL', 'WEBHOOK')),
    recipients      TEXT NOT NULL DEFAULT '[]',
    is_active       INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    last_run_at     DATETIME,
    next_run_at     DATETIME,
    last_status     TEXT NOT NULL DEFAULT '',
    last_error      TEXT NOT NULL DEFAULT '',
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (report_id) REFERENCES custom_reports(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_report_schedules_tenant_active
    ON report_schedules(tenant_id, is_active);

CREATE INDEX IF NOT EXISTS idx_report_schedules_next_run
    ON report_schedules(is_active, next_run_at);

CREATE TABLE IF NOT EXISTS report_executions (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL,
    report_id             TEXT NOT NULL,
    schedule_id           TEXT,
    format                TEXT NOT NULL DEFAULT 'CSV' CHECK (format IN ('CSV', 'JSON', 'PDF')),
    status                TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'RUNNING', 'COMPLETED', 'FAILED')),
    row_count             INTEGER NOT NULL DEFAULT 0,
    file_size_bytes       INTEGER NOT NULL DEFAULT 0,
    execution_duration_ms INTEGER NOT NULL DEFAULT 0,
    error_message         TEXT NOT NULL DEFAULT '',
    triggered_by          TEXT NOT NULL DEFAULT 'MANUAL' CHECK (triggered_by IN ('MANUAL', 'SCHEDULE')),
    created_at            DATETIME NOT NULL DEFAULT (datetime('now')),
    completed_at          DATETIME,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (report_id) REFERENCES custom_reports(id) ON DELETE CASCADE,
    FOREIGN KEY (schedule_id) REFERENCES report_schedules(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_report_executions_report
    ON report_executions(tenant_id, report_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_report_executions_status
    ON report_executions(tenant_id, status);

-- Tenant FK Triggers
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_custom_reports_tenant_fk_insert
BEFORE INSERT ON custom_reports
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for custom_reports.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_custom_reports_tenant_fk_update
BEFORE UPDATE OF tenant_id ON custom_reports
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for custom_reports.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_report_schedules_tenant_fk_insert
BEFORE INSERT ON report_schedules
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for report_schedules.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_report_schedules_tenant_fk_update
BEFORE UPDATE OF tenant_id ON report_schedules
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for report_schedules.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_report_executions_tenant_fk_insert
BEFORE INSERT ON report_executions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for report_executions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_report_executions_tenant_fk_update
BEFORE UPDATE OF tenant_id ON report_executions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for report_executions.tenant_id') END;
END;
-- +goose StatementEnd

-- RBAC Seeding & Grants
INSERT OR IGNORE INTO permissions (name, description) VALUES
('reports:write', 'Create, update, and schedule custom fleet reports');

-- Grant to Role 1 (Admin)
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name = 'reports:write';

-- Grant to Role 2 (Dispatcher / Ops)
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name = 'reports:write';

-- Grant to Role 6 (Org Admin)
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name = 'reports:write';

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name = 'reports:write');
DELETE FROM permissions WHERE name = 'reports:write';

DROP TRIGGER IF EXISTS trg_report_executions_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_report_executions_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_report_schedules_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_report_schedules_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_custom_reports_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_custom_reports_tenant_fk_insert;

DROP INDEX IF EXISTS idx_report_executions_status;
DROP INDEX IF EXISTS idx_report_executions_report;
DROP TABLE IF EXISTS report_executions;

DROP INDEX IF EXISTS idx_report_schedules_next_run;
DROP INDEX IF EXISTS idx_report_schedules_tenant_active;
DROP TABLE IF EXISTS report_schedules;

DROP INDEX IF EXISTS idx_custom_reports_tenant_system;
DROP INDEX IF EXISTS idx_custom_reports_tenant_dataset;
DROP TABLE IF EXISTS custom_reports;
