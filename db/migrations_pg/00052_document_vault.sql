-- PG port of 00052_document_vault.sql | status: PORTABLE | flags: none
-- +goose Up
-- Feature 3: document vault.
-- Note: vehicles.puc_expiry was already added in 00046 (Spec 05).

-- Driver documents
CREATE TABLE IF NOT EXISTS driver_documents (
    id            TEXT PRIMARY KEY,
    driver_id     TEXT NOT NULL,
    doc_type      TEXT NOT NULL CHECK (doc_type IN ('aadhaar','pan','dl','bank_proof','medical','other')),
    file_url      TEXT NOT NULL,
    expiry_date   DATE,
    status        TEXT NOT NULL DEFAULT 'pending_review' CHECK (status IN ('pending_review','verified','rejected')),
    verified_by   TEXT,
    verified_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (driver_id) REFERENCES drivers(id)
);
CREATE INDEX IF NOT EXISTS idx_drvdocs_driver ON driver_documents(driver_id);
-- Vehicle documents
CREATE TABLE IF NOT EXISTS vehicle_documents (
    id            TEXT PRIMARY KEY,
    vehicle_id    TEXT NOT NULL,
    doc_type      TEXT NOT NULL CHECK (doc_type IN ('rc','insurance','puc','fitness','permit','others')),
    file_url      TEXT NOT NULL,
    expiry_date   DATE,
    status        TEXT NOT NULL DEFAULT 'pending_review' CHECK (status IN ('pending_review','verified','rejected')),
    verified_by   TEXT,
    verified_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id)
);
CREATE INDEX IF NOT EXISTS idx_vehdocs_vehicle ON vehicle_documents(vehicle_id);
-- RBAC resources/permissions for the new surfaces.
INSERT INTO permissions (name, description) VALUES
('documents:upload', 'Upload driver/vehicle docs'),
('documents:read', 'View documents'),
('compliance:read', 'View compliance dashboard') ON CONFLICT DO NOTHING;
-- Assign to admin role (role id 1 per 00012 pattern)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions
WHERE name IN ('documents:upload','documents:read','compliance:read') ON CONFLICT DO NOTHING;
-- Assign read-only to dispatcher (role id 2)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions
WHERE name IN ('documents:read','compliance:read') ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
(SELECT id FROM permissions WHERE name IN ('documents:upload','documents:read','compliance:read'));
DELETE FROM permissions WHERE name IN ('documents:upload','documents:read','compliance:read');
DROP TABLE IF EXISTS vehicle_documents;
DROP TABLE IF EXISTS driver_documents;
