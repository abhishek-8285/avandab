-- +goose Up
-- 00099 IRN cancellation (GST e-invoice): records when an invoice's IRN was
-- cancelled on the GSTN portal (allowed within 24h of generation).
ALTER TABLE invoices ADD COLUMN irn_cancelled_at TIMESTAMP;

-- +goose Down
ALTER TABLE invoices DROP COLUMN irn_cancelled_at;
