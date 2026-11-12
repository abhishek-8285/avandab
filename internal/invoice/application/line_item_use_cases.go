package application

// Invoice line-item write use-cases, extracted from
// internal/handlers/invoices.go (handlers-end-state ADR, Phase 2). The
// transaction boundary (insert/update/delete + header recalculation in ONE
// tx), GST split math, rounding and tenant scoping are preserved exactly;
// handlers keep form parsing, validation defaults and HTTP mapping.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"transport-app/internal/shared"
)

// ErrUnknownHSN reports an HSN/SAC code that is absent from the tax master.
// The line-item path FAILS CLOSED on it (400) — never a silent default that
// could misfile taxes on a legal invoice.
var ErrUnknownHSN = errors.New("unknown HSN/SAC code")

// LineItemStage identifies which step of a line-item write failed so the HTTP
// adapter can preserve the legacy status/message mapping.
type LineItemStage string

const (
	LineItemStageTxBegin     LineItemStage = "tx_begin"
	LineItemStageInsert      LineItemStage = "insert"
	LineItemStageUpdate      LineItemStage = "update"
	LineItemStageDelete      LineItemStage = "delete"
	LineItemStageRecalculate LineItemStage = "recalculate"
	LineItemStageCommit      LineItemStage = "commit"
)

// LineItemWriteError reports the failed step of a line-item write.
type LineItemWriteError struct {
	Stage LineItemStage
	Err   error
}

// Error implements error.
func (e *LineItemWriteError) Error() string {
	if e.Err == nil {
		return "invoice line item write failed at " + string(e.Stage)
	}
	return "invoice line item write failed at " + string(e.Stage) + ": " + e.Err.Error()
}

// Unwrap exposes the underlying driver error to errors.Is/As callers.
func (e *LineItemWriteError) Unwrap() error { return e.Err }

// LineItemService owns the invoice line-item write paths.
type LineItemService struct {
	db *sql.DB
}

// NewLineItemService wires the line-item use-cases over the app DB.
func NewLineItemService(db *sql.DB) *LineItemService {
	return &LineItemService{db: db}
}

// AddLineItemCommand creates one freight line on an editable invoice.
type AddLineItemCommand struct {
	TenantID    shared.TenantID
	InvoiceID   string
	HSNSACCode  string
	Description string
	Unit        string
	Quantity    float64
	Rate        float64
}

// AddLineItem resolves the GST split, inserts the line and recalculates the
// invoice header totals in the same transaction.
func (s *LineItemService) AddLineItem(ctx context.Context, cmd AddLineItemCommand) error {
	gstRate, cgstRate, sgstRate, igstRate, err := s.resolveLineGST(ctx, cmd.TenantID, cmd.InvoiceID, cmd.HSNSACCode)
	if err != nil {
		return err
	}

	// Single rounding site for persisted line tax: stored values must equal the
	// totals the PDF renderer sums back out of these columns.
	taxable := cmd.Quantity * cmd.Rate
	cgstAmt, sgstAmt, igstAmt := ComputeLineTax(taxable, gstRate, cgstRate > 0 || sgstRate > 0)
	lineTotal := taxable + cgstAmt + sgstAmt + igstAmt

	lineID := uuid.NewString()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return &LineItemWriteError{Stage: LineItemStageTxBegin, Err: err}
	}
	defer func() { _ = tx.Rollback() }() //nolint:errcheck // no-op after commit

	_, err = tx.ExecContext(ctx, `
		INSERT INTO invoice_line_items (
			id, tenant_id, invoice_id, line_type, description, quantity, unit_price, amount,
			hsn_sac_code, unit, rate, taxable_value, cgst_rate, sgst_rate, igst_rate,
			cgst_amount, sgst_amount, igst_amount, total, created_at
		) VALUES ($1, $2, $3, 'freight', $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, CURRENT_TIMESTAMP)
	`, lineID, string(cmd.TenantID), cmd.InvoiceID, cmd.Description, cmd.Quantity, cmd.Rate, taxable,
		cmd.HSNSACCode, cmd.Unit, cmd.Rate, taxable, cgstRate, sgstRate, igstRate,
		cgstAmt, sgstAmt, igstAmt, lineTotal)
	if err != nil {
		return &LineItemWriteError{Stage: LineItemStageInsert, Err: err}
	}

	if !recalculateInvoiceTotalsTx(ctx, tx, cmd.TenantID, cmd.InvoiceID) {
		return &LineItemWriteError{Stage: LineItemStageRecalculate}
	}
	if err := tx.Commit(); err != nil {
		return &LineItemWriteError{Stage: LineItemStageCommit, Err: err}
	}
	return nil
}

// EditLineItemCommand rewrites one existing freight line.
type EditLineItemCommand struct {
	TenantID    shared.TenantID
	InvoiceID   string
	LineID      string
	HSNSACCode  string
	Description string
	Unit        string
	Quantity    float64
	Rate        float64
}

// EditLineItem resolves the GST split, updates the line and recalculates the
// invoice header totals in the same transaction.
func (s *LineItemService) EditLineItem(ctx context.Context, cmd EditLineItemCommand) error {
	gstRate, cgstRate, sgstRate, igstRate, err := s.resolveLineGST(ctx, cmd.TenantID, cmd.InvoiceID, cmd.HSNSACCode)
	if err != nil {
		return err
	}

	// Single rounding site for persisted line tax: stored values must equal the
	// totals the PDF renderer sums back out of these columns.
	taxable := cmd.Quantity * cmd.Rate
	cgstAmt, sgstAmt, igstAmt := ComputeLineTax(taxable, gstRate, cgstRate > 0 || sgstRate > 0)
	lineTotal := taxable + cgstAmt + sgstAmt + igstAmt

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return &LineItemWriteError{Stage: LineItemStageTxBegin, Err: err}
	}
	defer func() { _ = tx.Rollback() }() //nolint:errcheck // no-op after commit

	_, err = tx.ExecContext(ctx, `
		UPDATE invoice_line_items
		SET hsn_sac_code = $1, description = $2, unit = $3, quantity = $4, rate = $5, unit_price = $6,
		    taxable_value = $7, amount = $8, cgst_rate = $9, sgst_rate = $10, igst_rate = $11,
		    cgst_amount = $12, sgst_amount = $13, igst_amount = $14, total = $15
		WHERE id = $16 AND invoice_id = $17 AND tenant_id = $18
	`, cmd.HSNSACCode, cmd.Description, cmd.Unit, cmd.Quantity, cmd.Rate, cmd.Rate, taxable, taxable,
		cgstRate, sgstRate, igstRate, cgstAmt, sgstAmt, igstAmt, lineTotal,
		cmd.LineID, cmd.InvoiceID, string(cmd.TenantID))
	if err != nil {
		return &LineItemWriteError{Stage: LineItemStageUpdate, Err: err}
	}

	if !recalculateInvoiceTotalsTx(ctx, tx, cmd.TenantID, cmd.InvoiceID) {
		return &LineItemWriteError{Stage: LineItemStageRecalculate}
	}
	if err := tx.Commit(); err != nil {
		return &LineItemWriteError{Stage: LineItemStageCommit, Err: err}
	}
	return nil
}

// DeleteLineItemCommand removes one existing freight line.
type DeleteLineItemCommand struct {
	TenantID  shared.TenantID
	InvoiceID string
	LineID    string
}

// DeleteLineItem deletes the line and recalculates the invoice header totals
// in the same transaction.
func (s *LineItemService) DeleteLineItem(ctx context.Context, cmd DeleteLineItemCommand) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return &LineItemWriteError{Stage: LineItemStageTxBegin, Err: err}
	}
	defer func() { _ = tx.Rollback() }() //nolint:errcheck // no-op after commit

	_, err = tx.ExecContext(ctx, `DELETE FROM invoice_line_items WHERE id = $1 AND invoice_id = $2 AND tenant_id = $3`, cmd.LineID, cmd.InvoiceID, string(cmd.TenantID))
	if err != nil {
		return &LineItemWriteError{Stage: LineItemStageDelete, Err: err}
	}

	if !recalculateInvoiceTotalsTx(ctx, tx, cmd.TenantID, cmd.InvoiceID) {
		return &LineItemWriteError{Stage: LineItemStageRecalculate}
	}
	if err := tx.Commit(); err != nil {
		return &LineItemWriteError{Stage: LineItemStageCommit, Err: err}
	}
	return nil
}

// lineItemDBTx is satisfied by both *sql.DB and *sql.Tx.
type lineItemDBTx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// resolveLineGST determines the GST rate and intra/inter-state split for a
// new or edited invoice line item. It FAILS CLOSED: an unknown HSN code or an
// unreadable tax configuration is an error — never a silent default that could
// misfile taxes on a legal invoice. Every invoice lookup is tenant-scoped.
func (s *LineItemService) resolveLineGST(ctx context.Context, tenantID shared.TenantID, invoiceID, hsnCode string) (gstRate, cgstRate, sgstRate, igstRate float64, err error) {
	gstRate = 18.0 // default only when no HSN code supplied
	if hsnCode != "" {
		switch err := s.db.QueryRowContext(ctx, `SELECT rate FROM hsn_sac_master WHERE code = $1`, hsnCode).Scan(&gstRate); {
		case errors.Is(err, sql.ErrNoRows):
			return 0, 0, 0, 0, fmt.Errorf("%w %q", ErrUnknownHSN, hsnCode)
		case err != nil:
			return 0, 0, 0, 0, fmt.Errorf("lookup HSN rate: %w", err)
		}
	}

	var custGST string
	var compState sql.NullString
	err = s.db.QueryRowContext(ctx, `
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

// recalculateInvoiceTotalsTx re-aggregates line items and updates the invoice
// header inside the caller's transaction. Every statement is tenant-scoped.
// Returns false when the recalculation failed (tx should be rolled back).
func recalculateInvoiceTotalsTx(ctx context.Context, q lineItemDBTx, tenantID shared.TenantID, invoiceID string) bool {
	var sumTaxable, sumCGST, sumSGST, sumIGST, sumTotal float64
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(taxable_value), 0), COALESCE(SUM(cgst_amount), 0),
		       COALESCE(SUM(sgst_amount), 0), COALESCE(SUM(igst_amount), 0),
		       COALESCE(SUM(total), 0)
		FROM invoice_line_items
		WHERE invoice_id = $1 AND tenant_id = $2
	`, invoiceID, string(tenantID)).Scan(&sumTaxable, &sumCGST, &sumSGST, &sumIGST, &sumTotal)
	if err != nil {
		return false
	}

	totalTax := sumCGST + sumSGST + sumIGST
	_, err = q.ExecContext(ctx, `
		UPDATE invoices
		SET subtotal = $1, tax = $2, cgst = $3, sgst = $4, igst = $5, total = $6, updated_at = CURRENT_TIMESTAMP
		WHERE id = $7 AND tenant_id = $8
	`, sumTaxable, totalTax, sumCGST, sumSGST, sumIGST, sumTotal, invoiceID, string(tenantID))
	return err == nil
}
