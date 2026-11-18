-- +goose Up
-- 00180: bind a Razorpay order to the invoice it was created for.
-- /verify previously checked only order|payment HMAC, so any valid order pair
-- could settle an arbitrary invoice. The binding row makes that impossible.
CREATE TABLE IF NOT EXISTS razorpay_orders (
    order_id     TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL,
    invoice_id   TEXT NOT NULL,
    amount_paise INTEGER NOT NULL,
    currency     TEXT NOT NULL DEFAULT 'INR',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_razorpay_orders_invoice
    ON razorpay_orders (tenant_id, invoice_id);

-- +goose Down
DROP TABLE IF EXISTS razorpay_orders;
