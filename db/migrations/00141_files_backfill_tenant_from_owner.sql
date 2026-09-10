-- +goose Up
-- 00141 - files CHECK widening + tenant data backfill from owner entity.
-- Two related repairs for files (00077/00140):
--
-- (A) CHECK widening: 00077's uploadable_type CHECK never included
-- 'driver_issue', but internal/handlers/drivers.go uploads with that kind -
-- every driver-issue photo upload failed with a CHECK violation. Table
-- rebuild widens the CHECK (additive; no data change). The rebuild carries
-- 00140's tenant_id column and recreates its index + FK triggers.
--
-- (B) Data backfill: 00140 stamped every pre-existing row DEFAULT '1'.
-- Correct for security, wrong for data: orgs '2'+ lost read access to their
-- own historical uploads. Backfill derives the owner tenant from the scoping
-- entity each file is keyed to:
--   driver_issue    -> drivers.tenant_id  (drivers.id = files.uploadable_id)
--   trip_pod        -> trips.tenant_id    (trips.id = files.uploadable_id)
--   expense_receipt -> trips.tenant_id    (web + mobile upload paths key
--                                         expense receipts to the trip)
-- Unknown uploadable types and dangling references stay at '1' (bootstrap) -
-- honest, never guessed.
--
-- Down is a documented no-op (00136 convention): reversing the CHECK
-- relaxation would strand existing driver_issue rows, and un-stamping
-- tenant_id would re-blind non-bootstrap orgs - never silently reverse a
-- security fix.

-- (A) rebuild with widened CHECK (preserves all columns incl 00140 tenant_id)
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
    created_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO files_new (id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, tenant_id, created_at)
SELECT id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, tenant_id, created_at FROM files;
DROP TABLE files;
ALTER TABLE files_new RENAME TO files;

-- recreate 00140 index + FK triggers (dropped with the old table)
CREATE INDEX IF NOT EXISTS idx_files_tenant_uploadable ON files(tenant_id, uploadable_type, uploadable_id);

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

-- (B) tenant backfill from owner entity
UPDATE files SET tenant_id = (SELECT d.tenant_id FROM drivers d WHERE d.id = files.uploadable_id)
WHERE tenant_id = '1' AND uploadable_type = 'driver_issue'
  AND EXISTS (SELECT 1 FROM drivers d WHERE d.id = files.uploadable_id AND d.tenant_id != '1');

UPDATE files SET tenant_id = (SELECT t.tenant_id FROM trips t WHERE t.id = files.uploadable_id)
WHERE tenant_id = '1' AND uploadable_type IN ('trip_pod', 'expense_receipt')
  AND EXISTS (SELECT 1 FROM trips t WHERE t.id = files.uploadable_id AND t.tenant_id != '1');

-- +goose Down
-- Documented no-op (00136 convention): reversing the CHECK relaxation would
-- strand existing driver_issue rows, and un-stamping tenant_id would re-blind
-- non-bootstrap orgs' historical uploads - never silently reverse a security
-- fix.
SELECT 1;
