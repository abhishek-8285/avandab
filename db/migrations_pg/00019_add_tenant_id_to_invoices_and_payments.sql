-- PG port of 00019_add_tenant_id_to_invoices_and_payments.sql | status: PORTABLE | flags: none
-- +goose Up
ALTER TABLE invoices ADD COLUMN tenant_id TEXT DEFAULT '1' NOT NULL;
ALTER TABLE payments ADD COLUMN tenant_id TEXT DEFAULT '1' NOT NULL;
-- +goose Down
