-- +goose Up
-- Spec 22 S8 — kharcha verification states + OCR extraction columns.
ALTER TABLE driver_expenses ADD COLUMN verification_state TEXT NOT NULL DEFAULT 'manual'
  CHECK (verification_state IN ('manual','auto_verified','flagged'));
ALTER TABLE driver_expenses ADD COLUMN flag_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE driver_expenses ADD COLUMN ocr_amount REAL;
ALTER TABLE driver_expenses ADD COLUMN ocr_confidence REAL;
CREATE INDEX idx_driver_expenses_verify
  ON driver_expenses(verification_state, created_at DESC);
-- NOTE: tenant_id is NOT in this index — that column arrives in 00095,
-- after this migration. The flagged-queue query filters on
-- verification_state + status here and applies tenant_id as a residual
-- predicate (Spec 22 §3 DDL adjusted to real column availability).

-- +goose Down
DROP INDEX IF EXISTS idx_driver_expenses_verify;
ALTER TABLE driver_expenses DROP COLUMN ocr_confidence;
ALTER TABLE driver_expenses DROP COLUMN ocr_amount;
ALTER TABLE driver_expenses DROP COLUMN flag_reason;
ALTER TABLE driver_expenses DROP COLUMN verification_state;
