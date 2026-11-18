package application

// SQL-backed helpers for invoice line-item totals, relocated from
// internal/handlers/invoices.go (handlers-end-state ADR). The transaction
// boundary, GST split math and tenant scoping are unchanged.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"transport-app/internal/shared"
)

// InvoiceDBTX is satisfied by both *sql.DB and *sql.Tx.
type InvoiceDBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ResolveLineGST determines the GST rate and intra/inter-state split for a
// new or edited invoice line item. It FAILS CLOSED: an unknown HSN code or
// an unreadable tax configuration is an error — never a silent default that
// could misfile taxes on a legal invoice. Every lookup is tenant-scoped.
func ResolveLineGST(ctx context.Context, q InvoiceDBTX, tenantID shared.TenantID, invoiceID, hsnCode string) (gstRate, cgstRate, sgstRate, igstRate float64, err error) {
	gstRate = 18.0 // default only when no HSN code supplied
	if hsnCode != "" {
		switch err := q.QueryRowContext(ctx, `SELECT rate FROM hsn_sac_master WHERE code = $1`, hsnCode).Scan(&gstRate); {
		case errors.Is(err, sql.ErrNoRows):
			return 0, 0, 0, 0, fmt.Errorf("%w %q", ErrUnknownHSN, hsnCode)
		case err != nil:
			return 0, 0, 0, 0, fmt.Errorf("lookup HSN rate: %w", err)
		}
	}

	var custGST string
	var compState sql.NullString
	err = q.QueryRowContext(ctx, `
		SELECT COALESCE(c.gst, ''), COALESCE(
			(SELECT state_code FROM tenant_company_profiles WHERE tenant_id = inv.tenant_id),
			CASE WHEN inv.tenant_id IS NULL OR inv.tenant_id IN ('', '1')
				THEN (SELECT state_code FROM company_settings WHERE id = 1) END, '27')
		FROM invoices inv
		JOIN customers c ON inv.customer_id = c.id
		WHERE inv.id = $1 AND inv.tenant_id = $2
	`, invoiceID, string(tenantID)).Scan(&custGST, &compState)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("lookup invoice tax context: %w", err)
	}

	supplierState := "27"
	if compState.Valid && compState.String != "" {
		supplierState = compState.String
	}
	custState := ""
	if len(custGST) >= 2 {
		custState = custGST[:2]
	}
	if custState == "" || custState == supplierState {
		cgstRate = gstRate / 2.0
		sgstRate = gstRate / 2.0
	} else {
		igstRate = gstRate
	}
	return gstRate, cgstRate, sgstRate, igstRate, nil
}

// RecalculateInvoiceTotals re-aggregates line items and updates the invoice
// header inside the caller's transaction. Every statement is tenant-scoped.
// Legacy lines store taxable_value/total = 0, so those fall back to amount;
// the header discount is subtracted from the recomputed total. Returns false
// when the recalculation failed (tx should be rolled back).
func RecalculateInvoiceTotals(ctx context.Context, q InvoiceDBTX, tenantID shared.TenantID, invoiceID string) bool {
	var sumTaxable, sumCGST, sumSGST, sumIGST, sumTotal, discount float64
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(COALESCE(NULLIF(taxable_value, 0), amount)), 0),
		       COALESCE(SUM(cgst_amount), 0),
		       COALESCE(SUM(sgst_amount), 0), COALESCE(SUM(igst_amount), 0),
		       COALESCE(SUM(COALESCE(NULLIF(total, 0), amount + cgst_amount + sgst_amount + igst_amount)), 0),
		       COALESCE((SELECT discount FROM invoices WHERE id = $1 AND tenant_id = $2), 0)
		FROM invoice_line_items
		WHERE invoice_id = $1 AND tenant_id = $2
	`, invoiceID, string(tenantID)).Scan(&sumTaxable, &sumCGST, &sumSGST, &sumIGST, &sumTotal, &discount)
	if err != nil {
		return false
	}

	sumTotal -= discount
	totalTax := sumCGST + sumSGST + sumIGST
	_, err = q.ExecContext(ctx, `
		UPDATE invoices
		SET subtotal = $1, tax = $2, cgst = $3, sgst = $4, igst = $5, total = $6, updated_at = CURRENT_TIMESTAMP
		WHERE id = $7 AND tenant_id = $8
	`, sumTaxable, totalTax, sumCGST, sumSGST, sumIGST, sumTotal, invoiceID, string(tenantID))
	return err == nil
}
