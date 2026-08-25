-- +goose Up
-- 00101 customer state_code for GST e-invoice place_of_supply (derived from GSTIN first 2 digits)
-- Settings already has company_settings.state_code; customer lacked it.
ALTER TABLE customers ADD COLUMN state_code TEXT;

-- Backfill from existing GSTINs where valid (15 chars, first 2 digits are state code 01-38)
UPDATE customers SET state_code = substr(gst,1,2) WHERE gst IS NOT NULL AND length(gst)=15 AND substr(gst,1,2) GLOB '[0-9][0-9]';

CREATE INDEX IF NOT EXISTS idx_customers_state_code ON customers(state_code);

-- +goose Down
DROP INDEX IF EXISTS idx_customers_state_code;
ALTER TABLE customers DROP COLUMN state_code;
