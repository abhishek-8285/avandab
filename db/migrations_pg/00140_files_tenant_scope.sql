-- PG port of 00140_files_tenant_scope.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00140: files.tenant_id — close the cross-tenant file-read hole (see sqlite).
ALTER TABLE files ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '1';
CREATE INDEX IF NOT EXISTS idx_files_tenant_uploadable ON files(tenant_id, uploadable_type, uploadable_id);

DROP TRIGGER IF EXISTS trg_files_tenant_fk_insert ON files;
CREATE TRIGGER trg_files_tenant_fk_insert BEFORE INSERT ON files
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_files_tenant_fk_update ON files;
CREATE TRIGGER trg_files_tenant_fk_update BEFORE UPDATE OF tenant_id ON files
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

-- +goose Down
DROP TRIGGER IF EXISTS trg_files_tenant_fk_update ON files;
DROP TRIGGER IF EXISTS trg_files_tenant_fk_insert ON files;
DROP INDEX IF EXISTS idx_files_tenant_uploadable;
ALTER TABLE files DROP COLUMN tenant_id;
