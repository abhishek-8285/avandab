package application

// GST e-invoice (IRN) generation use-case, extracted from
// internal/handlers/invoices.go (handlers-end-state ADR, Phase 2). The
// invoice/customer/tax-context reads, the paid/partially-paid and duplicate-IRN
// guards and the IRN persistence are preserved exactly; the handler keeps the
// GSTN client wiring, HTTP statuses and JSON/HTML rendering.

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	gstn "transport-app/internal/integration/gstn"
	"transport-app/internal/shared"
)

// ErrIRNPaymentStatus rejects IRN generation for paid / partially paid
// invoices (Spec 07 §5.3).
var ErrIRNPaymentStatus = errors.New("Cannot generate IRN for paid/partially paid invoices")

// ErrIRNAlreadyGenerated rejects a duplicate IRN for an already e-invoiced
// invoice.
var ErrIRNAlreadyGenerated = errors.New("IRN already generated for this invoice")

// IRNQueryError reports a database failure while loading invoice data for IRN
// generation (as opposed to the not-found case, which returns sql.ErrNoRows).
type IRNQueryError struct{ Err error }

// Error implements error.
func (e *IRNQueryError) Error() string { return "load invoice for IRN: " + e.Err.Error() }

// Unwrap exposes the underlying driver error.
func (e *IRNQueryError) Unwrap() error { return e.Err }

// IRNStoreError reports the failure to persist the generated IRN.
type IRNStoreError struct{ Err error }

// Error implements error.
func (e *IRNStoreError) Error() string { return "store IRN: " + e.Err.Error() }

// Unwrap exposes the underlying driver error.
func (e *IRNStoreError) Unwrap() error { return e.Err }

// IRNGenerator generates an IRN from canonical invoice data (implemented by
// the GSTN/GSP client).
type IRNGenerator interface {
	GenerateIRN(ctx context.Context, inv gstn.InvoiceView) (*gstn.IRNResponse, error)
}

// GenerateIRNCommand is the input of the IRN generation use-case.
type GenerateIRNCommand struct {
	TenantID  shared.TenantID
	InvoiceID string
	Generator IRNGenerator
}

// GenerateIRNUseCase orchestrates e-invoice generation.
type GenerateIRNUseCase struct {
	db *sql.DB
}

// NewGenerateIRNUseCase wires the IRN use-case over the app DB.
func NewGenerateIRNUseCase(db *sql.DB) *GenerateIRNUseCase {
	return &GenerateIRNUseCase{db: db}
}

// Execute loads the invoice/tax context, enforces the guard rules, generates
// the IRN through the GSTN client and persists the IRN fields. It returns
// sql.ErrNoRows when the tenant-scoped invoice does not exist.
func (uc *GenerateIRNUseCase) Execute(ctx context.Context, cmd GenerateIRNCommand) (*gstn.IRNResponse, error) {
	if cmd.Generator == nil {
		return nil, errors.New("IRN generator is required")
	}

	// 1. Fetch invoice and status.
	var invNum, custID, invStatus, existingIRN, invDate sql.NullString
	var totalVal, cgstVal, sgstVal, igstVal float64
	err := uc.db.QueryRowContext(ctx, `
		SELECT invoice_number, customer_id, status, irn, substr(CAST(created_at AS TEXT), 1, 10), total, cgst, sgst, igst
		FROM invoices
		WHERE id = $1 AND tenant_id = $2
	`, cmd.InvoiceID, string(cmd.TenantID)).Scan(&invNum, &custID, &invStatus, &existingIRN, &invDate, &totalVal, &cgstVal, &sgstVal, &igstVal)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, &IRNQueryError{Err: err}
	}

	// 2. Block on paid / partially paid invoices per Spec 07 §5.3.
	statusStr := strings.ToLower(invStatus.String)
	if statusStr == "paid" || statusStr == "partially_paid" {
		return nil, ErrIRNPaymentStatus
	}

	// 3. Block if IRN already generated.
	if existingIRN.Valid && existingIRN.String != "" {
		return nil, ErrIRNAlreadyGenerated
	}

	// 4. Fetch customer & company GSTIN (tenant profile first; the global
	// singleton is the fallback for the bootstrap tenant and tenant-less
	// contexts only, never for rowless org tenants).
	var custGST string
	var compGST sql.NullString
	_ = uc.db.QueryRowContext(ctx, `
		SELECT COALESCE(c.gst, ''), COALESCE(
			(SELECT gst_number FROM tenant_company_profiles WHERE tenant_id = $1),
			CASE WHEN $2 IN ('', '1') THEN (SELECT gst_number FROM company_settings WHERE id = 1) END)
		FROM customers c
		WHERE c.id = $3 AND c.tenant_id = $4
	`, string(cmd.TenantID), string(cmd.TenantID), custID.String, string(cmd.TenantID)).Scan(&custGST, &compGST)

	supplierGST := "27AABCU9603R1ZX"
	if compGST.Valid && compGST.String != "" {
		supplierGST = compGST.String
	}

	// 5. Fetch line items (a query failure leaves the fallback line below).
	lineViews := uc.lineItemsForIRN(ctx, cmd.TenantID, cmd.InvoiceID)

	// Fallback single line item if none entered.
	if len(lineViews) == 0 {
		lineViews = append(lineViews, gstn.LineItemView{
			HSNSACCode:   "996511",
			Description:  "Freight Services",
			Unit:         "NOS",
			Quantity:     1,
			Rate:         totalVal,
			TaxableValue: totalVal,
			Total:        totalVal,
		})
	}

	invView := gstn.InvoiceView{
		InvoiceID:      cmd.InvoiceID,
		InvoiceNumber:  invNum.String,
		InvoiceDate:    invDate.String,
		SupplierGSTIN:  supplierGST,
		RecipientGSTIN: custGST,
		TotalValue:     totalVal,
		CGST:           cgstVal,
		SGST:           sgstVal,
		IGST:           igstVal,
		LineItems:      lineViews,
	}

	res, err := cmd.Generator.GenerateIRN(ctx, invView)
	if err != nil {
		return nil, err
	}

	// 6. Update invoices row.
	_, err = uc.db.ExecContext(ctx, `
		UPDATE invoices
		SET irn = $1, irn_ack_no = $2, irn_ack_date = $3, signed_qr = $4, updated_at = CURRENT_TIMESTAMP
		WHERE id = $5 AND tenant_id = $6
	`, res.IRN, res.AckNo, res.AckDate, res.SignedQR, cmd.InvoiceID, string(cmd.TenantID))
	if err != nil {
		return nil, &IRNStoreError{Err: err}
	}

	return res, nil
}

// lineItemsForIRN loads the invoice lines for the e-invoice payload. A query
// failure returns nil so the caller falls back to the single freight line,
// matching the legacy handler semantics.
func (uc *GenerateIRNUseCase) lineItemsForIRN(ctx context.Context, tenantID shared.TenantID, invoiceID string) []gstn.LineItemView {
	rows, err := uc.db.QueryContext(ctx, `
		SELECT COALESCE(hsn_sac_code, '996511'), description, COALESCE(unit, 'NOS'),
		       quantity, COALESCE(rate, unit_price), COALESCE(taxable_value, amount),
		       COALESCE(cgst_rate, 0), COALESCE(sgst_rate, 0), COALESCE(igst_rate, 0),
		       COALESCE(cgst_amount, 0), COALESCE(sgst_amount, 0), COALESCE(igst_amount, 0),
		       COALESCE(total, amount)
		FROM invoice_line_items
		WHERE invoice_id = $1 AND tenant_id = $2
	`, invoiceID, string(tenantID))
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var lineViews []gstn.LineItemView
	for rows.Next() {
		var lv gstn.LineItemView
		if err := rows.Scan(
			&lv.HSNSACCode, &lv.Description, &lv.Unit,
			&lv.Quantity, &lv.Rate, &lv.TaxableValue,
			&lv.CGSTRate, &lv.SGSTRate, &lv.IGSTRate,
			&lv.CGSTAmount, &lv.SGSTAmount, &lv.IGSTAmount,
			&lv.Total,
		); err == nil {
			lineViews = append(lineViews, lv)
		}
	}
	return lineViews
}
