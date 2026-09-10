-- PG port of 00141_files_backfill_tenant_from_owner.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00141 - files CHECK widening + tenant data backfill from owner entity.
-- (A) CHECK widening
CREATE TABLE files_new (
    id              TEXT PRIMARY KEY,
    filename        TEXT NOT NULL,
    original_name   TEXT NOT NULL,
    path            TEXT NOT NULL,
    size            INTEGER NOT NULL,
    mime_type       TEXT NOT NULL,
    uploadable_type TEXT NOT NULL CHECK (uploadable_type IN
        ('driver_license','vehicle_insurance','vehicle_permit','company_logo',
         'vehicle_rc','vehicle_fitness','vehicle_puc',
         'trip_pod','expense_receipt','logo','general','driver_issue')),
    uploadable_id   TEXT,
    tenant_id       TEXT NOT NULL DEFAULT '1',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
INSERT INTO files_new (id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, tenant_id, created_at)
SELECT id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, tenant_id, created_at FROM files;
DROP TABLE files;
ALTER TABLE files_new RENAME TO files;

CREATE INDEX IF NOT EXISTS idx_files_tenant_uploadable ON files(tenant_id, uploadable_type, uploadable_id);

DROP TRIGGER IF EXISTS trg_files_tenant_fk_insert ON files;
CREATE TRIGGER trg_files_tenant_fk_insert BEFORE INSERT ON files
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_files_tenant_fk_update ON files;
CREATE TRIGGER trg_files_tenant_fk_update BEFORE UPDATE OF tenant_id ON files
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

-- (B) tenant backfill from owner entity
UPDATE files SET tenant_id = (SELECT d.tenant_id FROM drivers d WHERE d.id = files.uploadable_id)
WHERE tenant_id = '1' AND uploadable_type = 'driver_issue'
  AND EXISTS (SELECT 1 FROM drivers d WHERE d.id = files.uploadable_id AND d.tenant_id != '1');

UPDATE files SET tenant_id = (SELECT t.tenant_id FROM trips t WHERE t.id = files.uploadable_id)
WHERE tenant_id = '1' AND uploadable_type IN ('trip_pod', 'expense_receipt')
  AND EXISTS (SELECT 1 FROM trips t WHERE t.id = files.uploadable_id AND t.tenant_id != '1');

-- +goose Down
SELECT 1;
