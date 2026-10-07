-- +goose Up
-- 00167: audit_logs.tenant_id — per-org scoping for dashboard RecentActivity
-- and /audit-logs. Platform admin (role admin) reads global; every other
-- role reads only its own tenant. Unscoped reads leaked super-admin rows
-- (tenant.create/suspend, foreign logins) onto every org dashboard.

ALTER TABLE audit_logs ADD COLUMN tenant_id TEXT;

-- Backfill from the acting user's org.
UPDATE audit_logs
SET tenant_id = (SELECT tenant_id FROM users WHERE users.id = audit_logs.user_id)
WHERE user_id IS NOT NULL;

-- Platform-level rows (tenants table) stay platform-attributed: visible to
-- the platform admin only, never on an org dashboard.
UPDATE audit_logs SET tenant_id = '1' WHERE table_name = 'tenants';

-- Actor-less trip events (e.g. pod_otp_issued with NULL user) attribute to
-- the trip's org so the owning org keeps its own system trail.
UPDATE audit_logs
SET tenant_id = (SELECT tenant_id FROM trips WHERE trips.id = audit_logs.record_id)
WHERE tenant_id IS NULL AND table_name = 'trips'
  AND record_id IN (SELECT id FROM trips);

-- Anything still unattributed (deleted users, unknown tables) falls back to
-- platform scope: admin-visible instead of leaking nowhere/anywhere.
UPDATE audit_logs SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';

CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant ON audit_logs(tenant_id);

-- +goose StatementBegin
CREATE TRIGGER trg_audit_logs_tenant_fk_insert
BEFORE INSERT ON audit_logs
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for audit_logs.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_audit_logs_tenant_fk_update
BEFORE UPDATE OF tenant_id ON audit_logs
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for audit_logs.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_audit_logs_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_audit_logs_tenant_fk_insert;
DROP INDEX IF EXISTS idx_audit_logs_tenant;
ALTER TABLE audit_logs DROP COLUMN tenant_id;
