-- PG port of 00009_payments.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE payments (
    id          TEXT PRIMARY KEY,
    invoice_id  TEXT NOT NULL,
    payment_date TIMESTAMPTZ NOT NULL,
    amount      REAL NOT NULL,
    method      TEXT NOT NULL CHECK (method IN ('cash', 'upi', 'bank_transfer', 'cheque')),
    reference   TEXT,
    remarks     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE
);
CREATE INDEX idx_payments_invoice ON payments(invoice_id);
-- +goose Down
DROP TABLE IF EXISTS payments;
