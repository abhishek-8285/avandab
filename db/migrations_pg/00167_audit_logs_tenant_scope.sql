-- PG port of 00167_audit_logs_tenant_scope.sql | status: PORTABLE | flags: none
-- +goose Up
-- SQL in this section is executed when the migration is applied.

ALTER TABLE audit_logs ADD COLUMN tenant_id TEXT;

UPDATE audit_logs
SET tenant_id = (SELECT tenant_id FROM users WHERE users.id = audit_logs.user_id)
WHERE user_id IS NOT NULL;

UPDATE audit_logs SET tenant_id = '1' WHERE table_name = 'tenants';

UPDATE audit_logs
SET tenant_id = (SELECT tenant_id FROM trips WHERE trips.id = audit_logs.record_id)
WHERE tenant_id IS NULL AND table_name = 'trips'
  AND record_id IN (SELECT id FROM trips);

UPDATE audit_logs SET tenant_id = '1' WHERE tenant_id IS NULL OR tenant_id = '';

CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant ON audit_logs(tenant_id);

DROP TRIGGER IF EXISTS trg_audit_logs_tenant_fk_insert ON audit_logs;
CREATE TRIGGER trg_audit_logs_tenant_fk_insert BEFORE INSERT ON audit_logs
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

DROP TRIGGER IF EXISTS trg_audit_logs_tenant_fk_update ON audit_logs;
CREATE TRIGGER trg_audit_logs_tenant_fk_update BEFORE UPDATE OF tenant_id ON audit_logs
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

-- +goose Down
DROP TRIGGER IF EXISTS trg_audit_logs_tenant_fk_update ON audit_logs;
DROP TRIGGER IF EXISTS trg_audit_logs_tenant_fk_insert ON audit_logs;
DROP INDEX IF EXISTS idx_audit_logs_tenant;
ALTER TABLE audit_logs DROP COLUMN tenant_id;
