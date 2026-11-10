package application

// Read-model queries behind the invoice view, PDF and line-item editor pages,
// extracted from internal/handlers/invoices.go (handlers-end-state ADR,
// Phase 2). The legacy SQL, tenant scoping and best-effort error semantics are
// preserved; handlers keep HTTP, template rendering and PDF composition.

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"transport-app/internal/shared"
	"transport-app/internal/shared/gstin"
	"transport-app/internal/validate"
)

// InvoiceDocumentQueries is the read-model facade for invoice document
// rendering. It is stateless and cheap to construct per request.
type InvoiceDocumentQueries struct {
	db *sql.DB
}

// NewInvoiceDocumentQueries wires the document read model over the app DB.
func NewInvoiceDocumentQueries(db *sql.DB) *InvoiceDocumentQueries {
	return &InvoiceDocumentQueries{db: db}
}

// InvoiceDocumentKey identifies a tenant-scoped invoice document.
type InvoiceDocumentKey struct {
	InvoiceID string
	TenantID  shared.TenantID
}

// DueDate returns the raw invoices.due_date text ("" when NULL or absent).
// The legacy view path ignored a lookup failure.
func (q *InvoiceDocumentQueries) DueDate(ctx context.Context, key InvoiceDocumentKey) (string, error) {
	var d sql.NullString
	if err := q.db.QueryRowContext(ctx, `SELECT COALESCE(CAST(due_date AS TEXT), '') FROM invoices WHERE id = $1 AND tenant_id = $2`, key.InvoiceID, string(key.TenantID)).Scan(&d); err != nil {
		return "", err
	}
	return d.String, nil
}

// Extras returns the raw due_date and ewb_number columns (not surfaced on the
// invoice read-model DTO). A lookup failure returns the error; the legacy PDF
// path degraded to blank labels.
func (q *InvoiceDocumentQueries) Extras(ctx context.Context, key InvoiceDocumentKey) (dueDate, ewb string, err error) {
	var d, e sql.NullString
	err = q.db.QueryRowContext(ctx, `
		SELECT COALESCE(CAST(due_date AS TEXT), ''), COALESCE(ewb_number, '')
		 FROM invoices WHERE id = $1 AND tenant_id = $2`, key.InvoiceID, string(key.TenantID)).Scan(&d, &e)
	if err != nil {
		return "", "", err
	}
	return d.String, e.String, nil
}

// CompanyProfile mirrors the tenant's company_settings row for the seller
// block on invoices and PDFs.
type CompanyProfile struct {
	Name      string
	Address   string
	GSTNumber string
	PanNumber string
	StateCode string
	Phone     string
	Email     string
}

// TaxTier classifies the seller identity: 1 GST registered, 2 PAN only,
// 3 unregistered micro transporter.
func (p CompanyProfile) TaxTier() int {
	if p.GSTNumber != "" && gstin.Valid(p.GSTNumber) {
		return 1
	}
	if p.PanNumber != "" && validate.ValidPAN(p.PanNumber) {
		return 2
	}
	return 3
}

// LegalTitle is the invoice title mandated by the seller's tax tier.
func (p CompanyProfile) LegalTitle() string {
	switch p.TaxTier() {
	case 1:
		return "TAX INVOICE"
	case 2:
		return "BILL OF SUPPLY / FREIGHT BILL"
	default:
		return "CONSIGNMENT FREIGHT BILL"
	}
}

// tenantProfileColumns selects the seller-block columns shared by the
// per-tenant profile table and the global singleton (same column order).
const tenantProfileColumns = `COALESCE(company_name, ''), COALESCE(address, ''),
       COALESCE(gst_number, ''), COALESCE(pan_number, ''), COALESCE(state_code, '27'),
       COALESCE(phone, ''), COALESCE(email, '')`

func finalizeCompanyProfile(p CompanyProfile) CompanyProfile {
	if p.StateCode == "" {
		p.StateCode = "27"
	}
	if p.GSTNumber != "" && len(p.GSTNumber) == 15 && p.PanNumber == "" {
		p.PanNumber = p.GSTNumber[2:12]
	}
	return p
}

// CompanyProfile reads the tenant's profile for the seller block. The global
// singleton is the fallback for the bootstrap tenant and tenant-less contexts
// only; rowless org tenants render blank (placeholder) instead of another
// org's identity. Name is empty when unset — the renderer prints a placeholder
// rather than any hardcoded company name.
func (q *InvoiceDocumentQueries) CompanyProfile(ctx context.Context, tenantID shared.TenantID) CompanyProfile {
	tid := string(tenantID)
	if tid != "" && tid != string(shared.DefaultTenant) { //nolint:tenant-default // bootstrap-vs-org identity branching, not a data fallback
		var p CompanyProfile
		err := q.db.QueryRowContext(ctx, `
			SELECT `+tenantProfileColumns+`
			FROM tenant_company_profiles WHERE tenant_id = ?`, tid).
			Scan(&p.Name, &p.Address, &p.GSTNumber, &p.PanNumber, &p.StateCode, &p.Phone, &p.Email)
		if err == nil {
			return finalizeCompanyProfile(p)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Warn("tenant company profile unavailable for invoice PDF", "error", err)
		}
		return finalizeCompanyProfile(CompanyProfile{})
	}
	var p CompanyProfile
	err := q.db.QueryRowContext(ctx,
		`SELECT `+tenantProfileColumns+` FROM company_settings WHERE id = 1`).
		Scan(&p.Name, &p.Address, &p.GSTNumber, &p.PanNumber, &p.StateCode, &p.Phone, &p.Email)
	if err != nil {
		slog.Warn("company_settings unavailable for invoice PDF", "error", err)
	}
	return finalizeCompanyProfile(p)
}

// CustomerBillTo returns the buyer identity for the Bill To block. Lookup
// failures return the error; the legacy PDF path degraded to blank labels.
func (q *InvoiceDocumentQueries) CustomerBillTo(ctx context.Context, tenantID shared.TenantID, customerID string) (name, gstNumber, address string, err error) {
	err = q.db.QueryRowContext(ctx, `
		SELECT name, COALESCE(gst, ''), COALESCE(address, '')
		FROM customers WHERE id = $1 AND tenant_id = $2`, customerID, string(tenantID)).
		Scan(&name, &gstNumber, &address)
	if err != nil {
		return "", "", "", err
	}
	return name, gstNumber, address, nil
}

// InvoiceLineItem is one tenant-scoped invoice_line_items row with the
// per-line GST data needed by the document renderer.
type InvoiceLineItem struct {
	ID           string
	InvoiceID    string
	HSNSACCode   string
	Description  string
	Unit         string
	Quantity     float64
	Rate         float64
	TaxableValue float64
	CGSTRate     float64
	SGSTRate     float64
	IGSTRate     float64
	CGSTAmount   float64
	SGSTAmount   float64
	IGSTAmount   float64
	Total        float64
}

// InvoiceLineItems returns the itemized rows with per-line GST data ordered by
// creation time. A query failure returns the error; the legacy PDF path
// degraded to no line items.
func (q *InvoiceDocumentQueries) InvoiceLineItems(ctx context.Context, key InvoiceDocumentKey) ([]InvoiceLineItem, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, invoice_id, COALESCE(hsn_sac_code, ''), description, COALESCE(unit, 'NOS'),
		       quantity, COALESCE(rate, unit_price), COALESCE(taxable_value, amount),
		       COALESCE(cgst_rate, 0), COALESCE(sgst_rate, 0), COALESCE(igst_rate, 0),
		       COALESCE(cgst_amount, 0), COALESCE(sgst_amount, 0), COALESCE(igst_amount, 0),
		       COALESCE(total, amount)
		FROM invoice_line_items
		WHERE invoice_id = $1 AND tenant_id = $2
		ORDER BY created_at ASC
	`, key.InvoiceID, string(key.TenantID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items []InvoiceLineItem
	for rows.Next() {
		var it InvoiceLineItem
		if err := rows.Scan(
			&it.ID, &it.InvoiceID, &it.HSNSACCode, &it.Description, &it.Unit,
			&it.Quantity, &it.Rate, &it.TaxableValue,
			&it.CGSTRate, &it.SGSTRate, &it.IGSTRate,
			&it.CGSTAmount, &it.SGSTAmount, &it.IGSTAmount,
			&it.Total,
		); err == nil {
			items = append(items, it)
		}
	}
	return items, nil
}

// HSNRecord holds one hsn_sac_master row for the editor datalist and search.
type HSNRecord struct {
	Code        string
	Description string
	Type        string
	Rate        float64
}

// ActiveHSNRecords lists active HSN/SAC masters ordered by code. Row scan
// failures are skipped, matching the legacy editor datalist.
func (q *InvoiceDocumentQueries) ActiveHSNRecords(ctx context.Context) ([]HSNRecord, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT code, description, type, rate FROM hsn_sac_master WHERE active = 1 ORDER BY code ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var records []HSNRecord
	for rows.Next() {
		var rec HSNRecord
		if err := rows.Scan(&rec.Code, &rec.Description, &rec.Type, &rec.Rate); err == nil {
			records = append(records, rec)
		}
	}
	return records, nil
}

// SearchHSNRecords searches active HSN/SAC masters by code or description,
// capped at 10 rows ordered by code.
func (q *InvoiceDocumentQueries) SearchHSNRecords(ctx context.Context, query string) ([]HSNRecord, error) {
	pattern := "%" + query + "%"
	rows, err := q.db.QueryContext(ctx, `
		SELECT code, description, type, rate
		FROM hsn_sac_master
		WHERE active = 1 AND (code LIKE $1 OR description LIKE $2)
		ORDER BY code ASC LIMIT 10
	`, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var results []HSNRecord
	for rows.Next() {
		var rec HSNRecord
		if err := rows.Scan(&rec.Code, &rec.Description, &rec.Type, &rec.Rate); err == nil {
			results = append(results, rec)
		}
	}
	return results, nil
}
