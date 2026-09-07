-- PG port of 00077_files_generic_types.sql | status: NEEDS-REVIEW | flags: STRIPPED-PRAGMA | reviewed: YES
-- +goose Up
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
         'trip_pod','expense_receipt','logo','general')),
    uploadable_id   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
INSERT INTO files_new (id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, created_at)
SELECT id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, created_at FROM files;
DROP TABLE files;
ALTER TABLE files_new RENAME TO files;
CREATE INDEX IF NOT EXISTS idx_files_uploadable ON files(uploadable_type, uploadable_id);
-- +goose Down
DROP INDEX IF EXISTS idx_files_uploadable;
-- Remove rows typed by the widened CHECK before restoring the narrower one.
DELETE FROM files WHERE uploadable_type IN ('trip_pod','expense_receipt','logo','general');
CREATE TABLE files_old (
    id              TEXT PRIMARY KEY,
    filename        TEXT NOT NULL,
    original_name   TEXT NOT NULL,
    path            TEXT NOT NULL,
    size            INTEGER NOT NULL,
    mime_type       TEXT NOT NULL,
    uploadable_type TEXT NOT NULL CHECK (uploadable_type IN
        ('driver_license','vehicle_insurance','vehicle_permit','company_logo',
         'vehicle_rc','vehicle_fitness','vehicle_puc')),
    uploadable_id   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
INSERT INTO files_old (id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, created_at)
SELECT id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, created_at FROM files;
DROP TABLE files;
ALTER TABLE files_old RENAME TO files;
