-- PG port of 00180_razorpay_order_binding.sql
-- +goose Up
CREATE TABLE IF NOT EXISTS razorpay_orders (
    order_id     TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL,
    invoice_id   TEXT NOT NULL,
    amount_paise BIGINT NOT NULL,
    currency     TEXT NOT NULL DEFAULT 'INR',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_razorpay_orders_invoice
    ON razorpay_orders (tenant_id, invoice_id);

-- +goose Down
DROP TABLE IF EXISTS razorpay_orders;
