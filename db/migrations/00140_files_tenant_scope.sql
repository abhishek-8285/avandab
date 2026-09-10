-- +goose Up
-- 00140: files.tenant_id — close the cross-tenant file-read hole.
-- The files table (00077) carried no tenant column, so any user holding
-- files:read (or a viewer role, which gets every %:read) could fetch another
-- org's driver licence / RC / POD / receipt by UUID. Existing rows backfill to
-- the default tenant '1' (same convention as 00065); new rows carry the
-- uploader's tenant. Reads/writes are tenant-scoped in db/query/files.sql.
ALTER TABLE files ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '1';

-- Covers the scoped entity listing (GetFilesByUploadable); the leftmost tenant
-- column also serves any bare tenant lookup. GetFileByID rides the PK.
CREATE INDEX IF NOT EXISTS idx_files_tenant_uploadable ON files(tenant_id, uploadable_type, uploadable_id);

-- Trigger-based FK enforcement per 00103 convention (SQLite cannot add a FK
-- via ALTER TABLE ADD COLUMN; no table rebuild for a security patch).
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_files_tenant_fk_insert
BEFORE INSERT ON files
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for files.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_files_tenant_fk_update
BEFORE UPDATE OF tenant_id ON files
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for files.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_files_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_files_tenant_fk_insert;
DROP INDEX IF EXISTS idx_files_tenant_uploadable;
ALTER TABLE files DROP COLUMN tenant_id;
