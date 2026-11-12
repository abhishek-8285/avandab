package application

// Expense OCR fields read, extracted from internal/handlers/kharcha_verify.go
// (handlers-end-state ADR, Phase 2). The OCR/verification state machine stays
// in the untouched service.KharchaVerifyService; this only fetches the fields
// the endpoint echoes back.

import (
	"context"
	"database/sql"
	"errors"
)

// ExpenseOCRFields returns the persisted OCR amount/confidence for one
// tenant-scoped expense. NULL columns come back as nil (JSON null); a missing
// row is intentionally not an error - the legacy handler tolerated
// sql.ErrNoRows and echoed nulls with 200. A real query failure is returned.
func ExpenseOCRFields(ctx context.Context, db *sql.DB, tenantID, expenseID string) (amount, confidence any, err error) {
	if db == nil {
		return nil, nil, nil
	}
	err = db.QueryRowContext(ctx,
		`SELECT ocr_amount, ocr_confidence FROM driver_expenses WHERE id = $1 AND tenant_id = $2`, expenseID, tenantID).
		Scan(&amount, &confidence)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return amount, confidence, nil
}
