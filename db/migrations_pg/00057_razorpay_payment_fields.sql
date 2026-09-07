-- PG port of 00057_razorpay_payment_fields.sql | status: PORTABLE | flags: none
-- +goose Up
ALTER TABLE payments ADD COLUMN razorpay_order_id   TEXT;
ALTER TABLE payments ADD COLUMN razorpay_payment_id TEXT;
ALTER TABLE payments ADD COLUMN razorpay_signature  TEXT;
ALTER TABLE payments ADD COLUMN webhook_event_id    TEXT;
CREATE UNIQUE INDEX idx_payments_webhook_event
    ON payments(tenant_id, webhook_event_id)
    WHERE webhook_event_id IS NOT NULL;
CREATE UNIQUE INDEX idx_payments_razorpay_payment
    ON payments(tenant_id, razorpay_payment_id)
    WHERE razorpay_payment_id IS NOT NULL;
-- +goose Down
DROP INDEX IF EXISTS idx_payments_webhook_event;
DROP INDEX IF EXISTS idx_payments_razorpay_payment;
ALTER TABLE payments DROP COLUMN razorpay_order_id;
ALTER TABLE payments DROP COLUMN razorpay_payment_id;
ALTER TABLE payments DROP COLUMN razorpay_signature;
ALTER TABLE payments DROP COLUMN webhook_event_id;
