-- PG port of 00037_payments_razorpay_method.sql | status: NEEDS-REVIEW | flags: STRIPPED-PRAGMA | reviewed: YES
-- +goose Up
ALTER TABLE payments RENAME TO payments_old;
CREATE TABLE payments (
    id              TEXT PRIMARY KEY,
    invoice_id      TEXT NOT NULL,
    payment_date    TIMESTAMPTZ NOT NULL,
    amount          REAL NOT NULL,
    method          TEXT NOT NULL CHECK (method IN ('cash', 'upi', 'bank_transfer', 'cheque', 'razorpay')),
    reference       TEXT,
    remarks         TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    tenant_id       TEXT DEFAULT '1' NOT NULL,
    idempotency_key TEXT,
    FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE
);
INSERT INTO payments (id, invoice_id, payment_date, amount, method, reference, remarks, created_at, updated_at, tenant_id, idempotency_key)
    SELECT id, invoice_id, payment_date, amount, method, reference, remarks, created_at, updated_at, tenant_id, idempotency_key
    FROM payments_old;
DROP TABLE payments_old;
CREATE INDEX idx_payments_invoice ON payments(invoice_id);
CREATE UNIQUE INDEX idx_payments_idempotency ON payments(tenant_id, idempotency_key);
-- +goose Down
ALTER TABLE payments RENAME TO payments_old;
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
    tenant_id   TEXT DEFAULT '1' NOT NULL,
    idempotency_key TEXT,
    FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE
);
INSERT INTO payments (id, invoice_id, payment_date, amount, method, reference, remarks, created_at, updated_at, tenant_id, idempotency_key)
    SELECT id, invoice_id, payment_date, amount, method, reference, remarks, created_at, updated_at, tenant_id, idempotency_key
    FROM payments_old;
DROP TABLE payments_old;
CREATE INDEX idx_payments_invoice ON payments(invoice_id);
CREATE UNIQUE INDEX idx_payments_idempotency ON payments(tenant_id, idempotency_key);
