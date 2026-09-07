-- PG port of 00024_update_invoices_status.sql | status: PORTABLE | flags: none
-- +goose Up
ALTER TABLE invoices ADD COLUMN paid_amount REAL NOT NULL DEFAULT 0.0;
ALTER TABLE invoices ADD COLUMN status TEXT NOT NULL DEFAULT 'outstanding';
ALTER TABLE invoices ADD COLUMN due_date TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_invoices_status ON invoices(status);
CREATE INDEX IF NOT EXISTS idx_invoices_due_date ON invoices(due_date);
-- +goose Down
-- SQLite doesn't support DROP COLUMN in older versions easily, but migration documents state transition
