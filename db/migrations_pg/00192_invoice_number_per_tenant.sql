-- PG port of 00182_invoice_number_per_tenant.sql
-- +goose Up
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_invoice_number_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_invoices_tenant_number ON invoices(tenant_id, invoice_number);

-- +goose Down
DROP INDEX IF EXISTS idx_invoices_tenant_number;
ALTER TABLE invoices ADD CONSTRAINT invoices_invoice_number_key UNIQUE (invoice_number);
