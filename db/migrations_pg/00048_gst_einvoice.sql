-- PG port of 00048_gst_einvoice.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00048 GST e-invoice: line items, sequences, CGST/SGST/IGST, HSN/SAC master, company state code

-- 1) Company state code (used for intra/inter-state tax determination)
ALTER TABLE company_settings ADD COLUMN state_code TEXT NOT NULL DEFAULT '27';
-- 2) HSN / SAC master
CREATE TABLE IF NOT EXISTS hsn_sac_master (
    code        TEXT PRIMARY KEY,
    description TEXT NOT NULL,
    type        TEXT NOT NULL CHECK (type IN ('HSN','SAC')),
    rate        REAL NOT NULL DEFAULT 0,
    cess_rate   REAL NOT NULL DEFAULT 0,
    active      INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_hsn_sac_type ON hsn_sac_master(type, active);
-- Seed common codes
INSERT INTO hsn_sac_master (code, description, type, rate) VALUES
  ('996511','Goods transport by road','SAC',5.0),
  ('996512','Container freight transport','SAC',12.0),
  ('8708','Parts of motor vehicles','HSN',28.0),
  ('997113','Packaging services','SAC',18.0) ON CONFLICT DO NOTHING;
-- 3) Invoice CGST/SGST/IGST split columns
ALTER TABLE invoices ADD COLUMN cgst REAL NOT NULL DEFAULT 0;
ALTER TABLE invoices ADD COLUMN sgst REAL NOT NULL DEFAULT 0;
ALTER TABLE invoices ADD COLUMN igst REAL NOT NULL DEFAULT 0;
ALTER TABLE invoices ADD COLUMN irn TEXT;
ALTER TABLE invoices ADD COLUMN irn_ack_no TEXT;
ALTER TABLE invoices ADD COLUMN irn_ack_date TEXT;
ALTER TABLE invoices ADD COLUMN signed_qr TEXT;
ALTER TABLE invoices ADD COLUMN ewb_number TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_invoices_irn ON invoices(irn) WHERE irn IS NOT NULL;
-- 4) Invoice line items extension (extends table created in 00042)
ALTER TABLE invoice_line_items ADD COLUMN hsn_sac_code TEXT REFERENCES hsn_sac_master(code);
ALTER TABLE invoice_line_items ADD COLUMN unit TEXT;
ALTER TABLE invoice_line_items ADD COLUMN rate REAL NOT NULL DEFAULT 0;
ALTER TABLE invoice_line_items ADD COLUMN taxable_value REAL NOT NULL DEFAULT 0;
ALTER TABLE invoice_line_items ADD COLUMN cgst_rate REAL NOT NULL DEFAULT 0;
ALTER TABLE invoice_line_items ADD COLUMN sgst_rate REAL NOT NULL DEFAULT 0;
ALTER TABLE invoice_line_items ADD COLUMN igst_rate REAL NOT NULL DEFAULT 0;
ALTER TABLE invoice_line_items ADD COLUMN cgst_amount REAL NOT NULL DEFAULT 0;
ALTER TABLE invoice_line_items ADD COLUMN sgst_amount REAL NOT NULL DEFAULT 0;
ALTER TABLE invoice_line_items ADD COLUMN igst_amount REAL NOT NULL DEFAULT 0;
ALTER TABLE invoice_line_items ADD COLUMN total REAL NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_line_items_invoice ON invoice_line_items(invoice_id);
CREATE INDEX IF NOT EXISTS idx_line_items_hsn ON invoice_line_items(hsn_sac_code);
-- 5) Per-financial-year e-invoice sequence (NIC document number)
CREATE TABLE IF NOT EXISTS invoice_sequences (
    financial_year TEXT NOT NULL,
    tenant_id      TEXT NOT NULL,
    last_number    INTEGER NOT NULL DEFAULT 0,
    prefix         TEXT NOT NULL DEFAULT 'INV',
    PRIMARY KEY (financial_year, tenant_id)
);
-- 6) RBAC permission rows
INSERT INTO permissions (name, description) VALUES
  ('integrations:einvoice','Generate/push GST e-invoice IRN'),
  ('integrations:ewaybill','Manage e-way bills'),
  ('integrations:fastag','Manage FASTag wallet & reconciliation') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('integrations:einvoice','integrations:ewaybill','integrations:fastag') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('integrations:ewaybill','integrations:fastag') ON CONFLICT DO NOTHING;
-- 7) company_config feature-flag seed rows (canonical table is 00042)
INSERT INTO company_config (tenant_id, key, value) VALUES
  ('1', 'gst_einvoice_enabled', 'false'),
  ('1', 'ewaybill_auto_generate', 'true'),
  ('1', 'fastag_auto_kharcha', 'true') ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM company_config WHERE key IN ('gst_einvoice_enabled','ewaybill_auto_generate','fastag_auto_kharcha');
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name IN ('integrations:einvoice','integrations:ewaybill','integrations:fastag'));
DELETE FROM permissions WHERE name IN ('integrations:einvoice','integrations:ewaybill','integrations:fastag');
DROP TABLE IF EXISTS invoice_sequences;
DROP INDEX IF EXISTS idx_line_items_hsn;
DROP INDEX IF EXISTS idx_line_items_invoice;
ALTER TABLE invoice_line_items DROP COLUMN hsn_sac_code;
ALTER TABLE invoice_line_items DROP COLUMN unit;
ALTER TABLE invoice_line_items DROP COLUMN rate;
ALTER TABLE invoice_line_items DROP COLUMN taxable_value;
ALTER TABLE invoice_line_items DROP COLUMN cgst_rate;
ALTER TABLE invoice_line_items DROP COLUMN sgst_rate;
ALTER TABLE invoice_line_items DROP COLUMN igst_rate;
ALTER TABLE invoice_line_items DROP COLUMN cgst_amount;
ALTER TABLE invoice_line_items DROP COLUMN sgst_amount;
ALTER TABLE invoice_line_items DROP COLUMN igst_amount;
ALTER TABLE invoice_line_items DROP COLUMN total;
DROP INDEX IF EXISTS idx_invoices_irn;
ALTER TABLE invoices DROP COLUMN cgst;
ALTER TABLE invoices DROP COLUMN sgst;
ALTER TABLE invoices DROP COLUMN igst;
ALTER TABLE invoices DROP COLUMN irn;
ALTER TABLE invoices DROP COLUMN irn_ack_no;
ALTER TABLE invoices DROP COLUMN irn_ack_date;
ALTER TABLE invoices DROP COLUMN signed_qr;
ALTER TABLE invoices DROP COLUMN ewb_number;
DROP TABLE IF EXISTS hsn_sac_master;
ALTER TABLE company_settings DROP COLUMN state_code;
