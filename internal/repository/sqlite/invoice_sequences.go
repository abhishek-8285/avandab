package sqlite

import (
	"context"
	"fmt"
	"time"
)

// nextInvoiceNumberSQL atomically allocates the next GST invoice sequence
// number for a (financial_year, tenant_id) pair — the invoice_sequences table
// from migration 00048. The single upsert + RETURNING statement is race-safe:
// SQLite serialises writers, so concurrent callers can never observe the same
// last_number.
//
// Plain SQL, no sqlc regen — matches the established raw-SQL pattern used by
// the invoice persistence layer ("plain SQL read, no sqlc regen").
const nextInvoiceNumberSQL = `
INSERT INTO invoice_sequences (financial_year, tenant_id, last_number, prefix)
VALUES (?, ?, 1, ?)
ON CONFLICT(financial_year, tenant_id)
DO UPDATE SET last_number = invoice_sequences.last_number + 1
RETURNING last_number
`

// FinancialYear returns the Indian financial year label (April–March) covering
// t, e.g. "2026-27". GST invoice sequences restart every financial year.
func FinancialYear(t time.Time) string {
	y := t.Year()
	if t.Month() >= time.April {
		return fmt.Sprintf("%04d-%02d", y, (y+1)%100)
	}
	return fmt.Sprintf("%04d-%02d", y-1, y%100)
}

// NextInvoiceNumber atomically increments the per-financial-year counter for
// tenantID and returns the formatted number "{prefix}/{FY}/{seq:04d}", e.g.
// "INV/2026-27/0001". For the default "INV" prefix the result is exactly 16
// characters, inside the GST ≤16-character invoice-number limit.
func (r *SQLRepository) NextInvoiceNumber(ctx context.Context, tenantID string, prefix string) (string, error) {
	if tenantID == "" {
		return "", fmt.Errorf("invoice sequence requires a tenant id")
	}
	if prefix == "" {
		prefix = "INV"
	}
	fy := FinancialYear(time.Now())

	var seq int64
	if err := r.queryRow(ctx, nextInvoiceNumberSQL, fy, tenantID, prefix).Scan(&seq); err != nil {
		return "", fmt.Errorf("allocate invoice sequence (fy=%s tenant=%s): %w", fy, tenantID, err)
	}
	return fmt.Sprintf("%s/%s/%04d", prefix, fy, seq), nil
}
