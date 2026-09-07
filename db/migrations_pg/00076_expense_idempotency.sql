-- PG port of 00076_expense_idempotency.sql | status: PORTABLE | flags: none
-- +goose Up
-- Add idempotency_key to driver_expenses for offline sync duplicate prevention (Spec 21.1 Seam 2)
ALTER TABLE driver_expenses ADD COLUMN idempotency_key TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_driver_expenses_idempotency ON driver_expenses(idempotency_key) WHERE idempotency_key IS NOT NULL AND idempotency_key != '';
-- +goose Down
DROP INDEX IF EXISTS idx_driver_expenses_idempotency;
ALTER TABLE driver_expenses DROP COLUMN idempotency_key;
