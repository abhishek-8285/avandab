package application

// Characterization + extraction tests for the invoice money/operation paths
// moved out of internal/handlers/invoices.go (handlers-end-state ADR,
// Phase 2): tenant isolation, GST split math/rounding, transaction-scoped
// total recalculation, IRN duplicate/status guards and IRN persistence.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	gstn "transport-app/internal/integration/gstn"
	"transport-app/internal/shared"
)

func newInvoiceWriteTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("invoice_write_%s_%d", t.Name(), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE invoices (
			id TEXT PRIMARY KEY, tenant_id TEXT, invoice_number TEXT, booking_id TEXT,
			customer_id TEXT, status TEXT, payment_status TEXT, irn TEXT, irn_ack_no TEXT,
			irn_ack_date TEXT, signed_qr TEXT, created_at DATETIME, due_date DATETIME,
			ewb_number TEXT, subtotal REAL DEFAULT 0, tax REAL DEFAULT 0, cgst REAL DEFAULT 0,
			sgst REAL DEFAULT 0, igst REAL DEFAULT 0, discount REAL DEFAULT 0, total REAL DEFAULT 0,
			updated_at DATETIME);
		CREATE TABLE invoice_line_items (
			id TEXT PRIMARY KEY, tenant_id TEXT, invoice_id TEXT, line_type TEXT,
			description TEXT, quantity REAL, unit_price REAL, amount REAL,
			hsn_sac_code TEXT, unit TEXT, rate REAL, taxable_value REAL,
			cgst_rate REAL, sgst_rate REAL, igst_rate REAL,
			cgst_amount REAL, sgst_amount REAL, igst_amount REAL, total REAL,
			created_at DATETIME);
		CREATE TABLE customers (id TEXT PRIMARY KEY, tenant_id TEXT, name TEXT, gst TEXT, address TEXT);
		CREATE TABLE hsn_sac_master (code TEXT PRIMARY KEY, description TEXT, type TEXT, rate REAL, active INTEGER);
		CREATE TABLE tenant_company_profiles (
			tenant_id TEXT PRIMARY KEY, company_name TEXT, address TEXT, gst_number TEXT,
			pan_number TEXT, state_code TEXT, phone TEXT, email TEXT);
		CREATE TABLE company_settings (
			id INTEGER PRIMARY KEY, company_name TEXT, address TEXT, gst_number TEXT,
			pan_number TEXT, state_code TEXT, phone TEXT, email TEXT);
	`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func seedInvoiceForWrite(t *testing.T, db *sql.DB, id, tenantID, customerID string, total float64) {
	t.Helper()
	mustExec(t, db, `INSERT INTO invoices (id, tenant_id, invoice_number, customer_id, status, subtotal, total)
		VALUES (?, ?, ?, ?, 'pending', 0, ?)`, id, tenantID, "INV-"+id, customerID, total)
}

func seedGSTCompanies(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExec(t, db, `INSERT INTO company_settings (id, company_name, gst_number, pan_number, state_code)
		VALUES (1, 'Bootstrap Carrier', '27AABCU9603R1ZX', 'AABCU9603R', '27')`)
	mustExec(t, db, `INSERT INTO tenant_company_profiles (tenant_id, company_name, gst_number, state_code)
		VALUES ('2', 'Tenant Two Carriers', '29AABCT1234T1Z2', '29')`)
	mustExec(t, db, `INSERT INTO tenant_company_profiles (tenant_id, company_name, pan_number, state_code)
		VALUES ('4', 'Tier Two Carriers', 'BKZPK9876L', '27')`)
	mustExec(t, db, `INSERT INTO hsn_sac_master (code, description, type, rate, active)
		VALUES ('996511', 'Road freight', 'SAC', 5, 1)`)
	mustExec(t, db, `INSERT INTO hsn_sac_master (code, description, type, rate, active)
		VALUES ('996519', 'Old freight', 'SAC', 12, 0)`)
}

func TestInvoiceDocumentQueries_TenantScopingAndProfileFallback(t *testing.T) {
	db := newInvoiceWriteTestDB(t)
	seedGSTCompanies(t, db)
	ctx := context.Background()

	mustExec(t, db, `INSERT INTO invoices (id, tenant_id, invoice_number, customer_id, due_date, ewb_number)
		VALUES ('inv-doc-1', '1', 'INV-DOC-1', 'cust-doc-1', '2026-12-31 00:00:00', 'EWB-77')`)
	mustExec(t, db, `INSERT INTO invoices (id, tenant_id, invoice_number, customer_id)
		VALUES ('inv-doc-2', '2', 'INV-DOC-2', 'cust-doc-2')`)
	mustExec(t, db, `INSERT INTO customers (id, tenant_id, name, gst, address)
		VALUES ('cust-doc-1', '1', 'Doc Buyer', '27ABCDE1234F1Z5', 'Pune')`)
	mustExec(t, db, `INSERT INTO customers (id, tenant_id, name, gst, address)
		VALUES ('cust-doc-2', '2', 'Other Buyer', '29ABCDE1234F1Z1', 'Bengaluru')`)
	mustExec(t, db, `INSERT INTO invoice_line_items (id, tenant_id, invoice_id, description, quantity, rate, taxable_value, total, created_at)
		VALUES ('li-doc-2', '1', 'inv-doc-1', 'second', 1, 100, 100, 100, '2026-01-02 00:00:00')`)
	mustExec(t, db, `INSERT INTO invoice_line_items (id, tenant_id, invoice_id, description, quantity, rate, taxable_value, total, created_at)
		VALUES ('li-doc-1', '1', 'inv-doc-1', 'first', 1, 200, 200, 200, '2026-01-01 00:00:00')`)

	q := NewInvoiceDocumentQueries(db)

	due, err := q.DueDate(ctx, InvoiceDocumentKey{InvoiceID: "inv-doc-1", TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "2026-12-31 00:00:00", due)

	_, err = q.DueDate(ctx, InvoiceDocumentKey{InvoiceID: "inv-doc-1", TenantID: "2"})
	assert.ErrorIs(t, err, sql.ErrNoRows, "cross-tenant due-date read must not find the invoice")

	dueRaw, ewb, err := q.Extras(ctx, InvoiceDocumentKey{InvoiceID: "inv-doc-1", TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "2026-12-31 00:00:00", dueRaw)
	assert.Equal(t, "EWB-77", ewb)

	_, _, err = q.Extras(ctx, InvoiceDocumentKey{InvoiceID: "inv-doc-1", TenantID: "2"})
	assert.ErrorIs(t, err, sql.ErrNoRows)

	// Bootstrap tenant falls back to the global singleton.
	bootstrap := q.CompanyProfile(ctx, shared.DefaultTenant)
	assert.Equal(t, "Bootstrap Carrier", bootstrap.Name)
	assert.Equal(t, "27AABCU9603R1ZX", bootstrap.GSTNumber)
	assert.Equal(t, "AABCU9603R", bootstrap.PanNumber, "PAN derived from GSTIN")
	assert.Equal(t, "TAX INVOICE", bootstrap.LegalTitle())

	// Org tenant uses its own row.
	orgTwo := q.CompanyProfile(ctx, "2")
	assert.Equal(t, "Tenant Two Carriers", orgTwo.Name)
	assert.Equal(t, "29AABCT1234T1Z2", orgTwo.GSTNumber)
	assert.Empty(t, q.CompanyProfile(ctx, "3").Name, "rowless org tenant must render blank, never another org's identity")
	assert.Equal(t, "CONSIGNMENT FREIGHT BILL", q.CompanyProfile(ctx, "3").LegalTitle())
	assert.Equal(t, "BILL OF SUPPLY / FREIGHT BILL", q.CompanyProfile(ctx, "4").LegalTitle(), "PAN-only seller is tier 2")
}

func TestInvoiceDocumentQueries_CustomerAndLineItemsScoped(t *testing.T) {
	db := newInvoiceWriteTestDB(t)
	seedGSTCompanies(t, db)
	ctx := context.Background()
	mustExec(t, db, `INSERT INTO customers (id, tenant_id, name, gst, address)
		VALUES ('cust-scope', '1', 'Scoped Buyer', '27ABCDE1234F1Z5', 'Pune')`)
	mustExec(t, db, `INSERT INTO invoice_line_items (id, tenant_id, invoice_id, description, quantity, rate, taxable_value, total, created_at)
		VALUES ('li-scope-2', '1', 'inv-scope', 'second', 1, 100, 100, 100, '2026-01-02 00:00:00')`)
	mustExec(t, db, `INSERT INTO invoice_line_items (id, tenant_id, invoice_id, description, quantity, rate, taxable_value, total, created_at)
		VALUES ('li-scope-1', '1', 'inv-scope', 'first', 1, 200, 200, 200, '2026-01-01 00:00:00')`)
	mustExec(t, db, `INSERT INTO invoice_line_items (id, tenant_id, invoice_id, description, quantity, rate, taxable_value, total, created_at)
		VALUES ('li-other-tenant', '2', 'inv-scope', 'foreign', 1, 900, 900, 900, '2026-01-01 00:00:00')`)
	q := NewInvoiceDocumentQueries(db)

	name, gstNumber, address, err := q.CustomerBillTo(ctx, "1", "cust-scope")
	require.NoError(t, err)
	assert.Equal(t, "Scoped Buyer", name)
	assert.Equal(t, "27ABCDE1234F1Z5", gstNumber)
	assert.Equal(t, "Pune", address)

	_, _, _, err = q.CustomerBillTo(ctx, "2", "cust-scope")
	assert.ErrorIs(t, err, sql.ErrNoRows)

	items, err := q.InvoiceLineItems(ctx, InvoiceDocumentKey{InvoiceID: "inv-scope", TenantID: "1"})
	require.NoError(t, err)
	require.Len(t, items, 2, "tenant scoping must exclude the tenant-2 line")
	assert.Equal(t, "first", items[0].Description)
	assert.Equal(t, "second", items[1].Description)

	foreign, err := q.InvoiceLineItems(ctx, InvoiceDocumentKey{InvoiceID: "inv-scope", TenantID: "2"})
	require.NoError(t, err)
	require.Len(t, foreign, 1)
	assert.Equal(t, "foreign", foreign[0].Description)

	active, err := q.ActiveHSNRecords(ctx)
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, "996511", active[0].Code)

	searched, err := q.SearchHSNRecords(ctx, "freight")
	require.NoError(t, err)
	require.Len(t, searched, 1)
	assert.Equal(t, "996511", searched[0].Code)
}

func TestLineItemService_AddGSTMoneyMath(t *testing.T) {
	cases := []struct {
		name      string
		custGST   string
		qty       float64
		rate      float64
		hsn       string
		wantCGST  float64
		wantSGST  float64
		wantIGST  float64
		wantTotal float64
	}{
		{name: "intra_state_splits_cgst_sgst", custGST: "27ABCDE1234F1Z5", qty: 2, rate: 1000, hsn: "996511", wantCGST: 50, wantSGST: 50, wantTotal: 2100},
		{name: "inter_state_uses_igst", custGST: "29ABCDE1234F1Z1", qty: 2, rate: 1000, hsn: "996511", wantIGST: 100, wantTotal: 2100},
		{name: "sub_paise_tax_rounds_to_paise", custGST: "27ABCDE1234F1Z5", qty: 10, rate: 9.9995, hsn: "996511", wantCGST: 2.5, wantSGST: 2.5, wantTotal: 104.995},
		{name: "no_hsn_defaults_to_18_percent", custGST: "27ABCDE1234F1Z5", qty: 1, rate: 1000, wantCGST: 90, wantSGST: 90, wantTotal: 1180},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newInvoiceWriteTestDB(t)
			seedGSTCompanies(t, db)
			mustExec(t, db, `INSERT INTO customers (id, tenant_id, name, gst) VALUES ('cust-calc', '1', 'Calc Buyer', ?)`, tc.custGST)
			seedInvoiceForWrite(t, db, "inv-calc", "1", "cust-calc", 0)

			svc := NewLineItemService(db)
			err := svc.AddLineItem(context.Background(), AddLineItemCommand{
				TenantID: "1", InvoiceID: "inv-calc", HSNSACCode: tc.hsn,
				Description: "Freight", Unit: "NOS", Quantity: tc.qty, Rate: tc.rate,
			})
			require.NoError(t, err)

			var taxable, cgst, sgst, igst, total float64
			require.NoError(t, db.QueryRow(`SELECT taxable_value, cgst_amount, sgst_amount, igst_amount, total
				FROM invoice_line_items WHERE invoice_id = 'inv-calc'`).Scan(&taxable, &cgst, &sgst, &igst, &total))
			assert.InDelta(t, tc.qty*tc.rate, taxable, 1e-9)
			assert.InDelta(t, tc.wantCGST, cgst, 1e-9)
			assert.InDelta(t, tc.wantSGST, sgst, 1e-9)
			assert.InDelta(t, tc.wantIGST, igst, 1e-9)
			assert.InDelta(t, tc.wantTotal, total, 1e-9, "line total must use the rounded tax")

			var hSubtotal, hTax, hCGST, hSGST, hIGST, hTotal float64
			require.NoError(t, db.QueryRow(`SELECT subtotal, tax, cgst, sgst, igst, total FROM invoices WHERE id = 'inv-calc'`).
				Scan(&hSubtotal, &hTax, &hCGST, &hSGST, &hIGST, &hTotal))
			assert.InDelta(t, taxable, hSubtotal, 1e-9)
			assert.InDelta(t, tc.wantCGST+tc.wantSGST+tc.wantIGST, hTax, 1e-9)
			assert.InDelta(t, tc.wantCGST, hCGST, 1e-9)
			assert.InDelta(t, tc.wantSGST, hSGST, 1e-9)
			assert.InDelta(t, tc.wantIGST, hIGST, 1e-9)
			assert.InDelta(t, tc.wantTotal, hTotal, 1e-9, "header totals recalculated in the same tx")
		})
	}
}

func TestLineItemService_AddFailsClosedOnUnknownHSNAndWrongTenant(t *testing.T) {
	db := newInvoiceWriteTestDB(t)
	seedGSTCompanies(t, db)
	mustExec(t, db, `INSERT INTO customers (id, tenant_id, name, gst) VALUES ('cust-close', '1', 'Close Buyer', '27ABCDE1234F1Z5')`)
	seedInvoiceForWrite(t, db, "inv-close", "1", "cust-close", 0)
	seedInvoiceForWrite(t, db, "inv-close-other", "2", "cust-close", 0)
	svc := NewLineItemService(db)
	ctx := context.Background()

	err := svc.AddLineItem(ctx, AddLineItemCommand{
		TenantID: "1", InvoiceID: "inv-close", HSNSACCode: "999999", Description: "Bogus", Unit: "NOS", Quantity: 1, Rate: 1000,
	})
	require.ErrorIs(t, err, ErrUnknownHSN)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoice_line_items WHERE invoice_id = 'inv-close'`).Scan(&count))
	assert.Zero(t, count, "fail-closed HSN error must not persist a line")

	err = svc.AddLineItem(ctx, AddLineItemCommand{
		TenantID: "1", InvoiceID: "inv-close-other", HSNSACCode: "996511", Description: "Cross tenant", Unit: "NOS", Quantity: 1, Rate: 1000,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lookup invoice tax context", "wrong-tenant invoice must be invisible")
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoice_line_items`).Scan(&count))
	assert.Zero(t, count, "cross-tenant write must not insert a line")
}

func TestLineItemService_EditDeleteRecalcAndTenantScope(t *testing.T) {
	db := newInvoiceWriteTestDB(t)
	seedGSTCompanies(t, db)
	mustExec(t, db, `INSERT INTO customers (id, tenant_id, name, gst) VALUES ('cust-edit', '1', 'Edit Buyer', '27ABCDE1234F1Z5')`)
	seedInvoiceForWrite(t, db, "inv-edit", "1", "cust-edit", 0)
	svc := NewLineItemService(db)
	ctx := context.Background()

	require.NoError(t, svc.AddLineItem(ctx, AddLineItemCommand{TenantID: "1", InvoiceID: "inv-edit", HSNSACCode: "996511", Description: "First", Unit: "NOS", Quantity: 1, Rate: 1000}))
	require.NoError(t, svc.AddLineItem(ctx, AddLineItemCommand{TenantID: "1", InvoiceID: "inv-edit", HSNSACCode: "996511", Description: "Second", Unit: "NOS", Quantity: 1, Rate: 500}))

	var lineID string
	require.NoError(t, db.QueryRow(`SELECT id FROM invoice_line_items WHERE invoice_id = 'inv-edit' AND description = 'First'`).Scan(&lineID))

	require.NoError(t, svc.EditLineItem(ctx, EditLineItemCommand{
		TenantID: "1", InvoiceID: "inv-edit", LineID: lineID,
		HSNSACCode: "996511", Description: "First edited", Unit: "NOS", Quantity: 2, Rate: 1000,
	}))
	var taxable, cgst float64
	require.NoError(t, db.QueryRow(`SELECT taxable_value, cgst_amount FROM invoice_line_items WHERE id = ?`, lineID).Scan(&taxable, &cgst))
	assert.InDelta(t, 2000.0, taxable, 1e-9)
	assert.InDelta(t, 50.0, cgst, 1e-9)

	// Cross-tenant edit resolves the invoice tax context fail-closed.
	otherLine := lineID
	err := svc.EditLineItem(ctx, EditLineItemCommand{
		TenantID: "2", InvoiceID: "inv-edit", LineID: otherLine,
		HSNSACCode: "996511", Description: "Hijacked", Unit: "NOS", Quantity: 9, Rate: 9,
	})
	require.Error(t, err)
	var unchanged string
	require.NoError(t, db.QueryRow(`SELECT description FROM invoice_line_items WHERE id = ?`, lineID).Scan(&unchanged))
	assert.Equal(t, "First edited", unchanged, "cross-tenant edit must not mutate the line")

	// Cross-tenant delete touches nothing and leaves headers untouched.
	var headerBefore float64
	require.NoError(t, db.QueryRow(`SELECT total FROM invoices WHERE id = 'inv-edit'`).Scan(&headerBefore))
	require.NoError(t, svc.DeleteLineItem(ctx, DeleteLineItemCommand{TenantID: "2", InvoiceID: "inv-edit", LineID: lineID}))
	var stillThere int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoice_line_items WHERE id = ?`, lineID).Scan(&stillThere))
	assert.Equal(t, 1, stillThere, "cross-tenant delete must not remove the line")
	var headerAfter float64
	require.NoError(t, db.QueryRow(`SELECT total FROM invoices WHERE id = 'inv-edit'`).Scan(&headerAfter))
	assert.InDelta(t, headerBefore, headerAfter, 1e-9, "cross-tenant delete must not rewrite header totals")

	// Tenant-scoped delete recalculates the header in the same tx.
	require.NoError(t, svc.DeleteLineItem(ctx, DeleteLineItemCommand{TenantID: "1", InvoiceID: "inv-edit", LineID: lineID}))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoice_line_items WHERE id = ?`, lineID).Scan(&stillThere))
	assert.Zero(t, stillThere)
	var subtotal, total float64
	require.NoError(t, db.QueryRow(`SELECT subtotal, total FROM invoices WHERE id = 'inv-edit'`).Scan(&subtotal, &total))
	assert.InDelta(t, 500.0, subtotal, 1e-9)
	assert.InDelta(t, 525.0, total, 1e-9)
}

type fakeIRNGenerator struct {
	calls int
	got   gstn.InvoiceView
	res   *gstn.IRNResponse
	err   error
}

func (g *fakeIRNGenerator) GenerateIRN(_ context.Context, inv gstn.InvoiceView) (*gstn.IRNResponse, error) {
	g.calls++
	g.got = inv
	return g.res, g.err
}

func seedIRNInvoice(t *testing.T, db *sql.DB, id, tenantID, status string, withLines bool) {
	t.Helper()
	custID := "cust-" + id
	mustExec(t, db, `INSERT INTO customers (id, tenant_id, name, gst) VALUES (?, ?, 'IRN Buyer', '29ABCDE1234F1Z1')`, custID, tenantID)
	mustExec(t, db, `INSERT INTO invoices (id, tenant_id, invoice_number, customer_id, status, created_at, subtotal, cgst, sgst, total)
		VALUES (?, ?, 'INV-IRN-1', ?, ?, '2026-11-10 10:00:00', 1000, 90, 90, 1180)`, id, tenantID, custID, status)
	if withLines {
		mustExec(t, db, `INSERT INTO invoice_line_items (id, tenant_id, invoice_id, description, quantity, rate, taxable_value, cgst_amount, sgst_amount, total)
			VALUES ('li-irn-1', ?, ?, 'Freight line', 1, 1000, 1000, 90, 90, 1180)`, tenantID, id)
	}
}

func TestGenerateIRNUseCase_GuardsTenantAndPersistence(t *testing.T) {
	db := newInvoiceWriteTestDB(t)
	seedGSTCompanies(t, db)
	seedIRNInvoice(t, db, "inv-irn", "1", "pending", true)
	ctx := context.Background()
	uc := NewGenerateIRNUseCase(db)

	gen := &fakeIRNGenerator{res: &gstn.IRNResponse{
		InvoiceID: "inv-irn", IRN: "IRN-64", AckNo: "ACK-1", AckDate: "2026-11-10", SignedQR: "qr-data",
	}}
	res, err := uc.Execute(ctx, GenerateIRNCommand{TenantID: "1", InvoiceID: "inv-irn", Generator: gen})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "IRN-64", res.IRN)
	require.Equal(t, 1, gen.calls)
	assert.Equal(t, "INV-IRN-1", gen.got.InvoiceNumber)
	assert.Equal(t, "2026-11-10", gen.got.InvoiceDate)
	assert.Equal(t, "27AABCU9603R1ZX", gen.got.SupplierGSTIN, "bootstrap tenant uses company_settings GSTIN")
	assert.Equal(t, "29ABCDE1234F1Z1", gen.got.RecipientGSTIN)
	assert.InDelta(t, 1180.0, gen.got.TotalValue, 1e-9)
	require.Len(t, gen.got.LineItems, 1)
	assert.Equal(t, "Freight line", gen.got.LineItems[0].Description)

	var irn, ackNo, signedQR string
	require.NoError(t, db.QueryRow(`SELECT irn, irn_ack_no, signed_qr FROM invoices WHERE id = 'inv-irn'`).Scan(&irn, &ackNo, &signedQR))
	assert.Equal(t, "IRN-64", irn)
	assert.Equal(t, "ACK-1", ackNo)
	assert.Equal(t, "qr-data", signedQR)

	// Duplicate IRN guard: no second GSTN call, no overwrite.
	dup := &fakeIRNGenerator{res: &gstn.IRNResponse{IRN: "IRN-OTHER"}}
	_, err = uc.Execute(ctx, GenerateIRNCommand{TenantID: "1", InvoiceID: "inv-irn", Generator: dup})
	require.ErrorIs(t, err, ErrIRNAlreadyGenerated)
	assert.Zero(t, dup.calls)

	// Cross-tenant: invoice is invisible to another tenant.
	_, err = uc.Execute(ctx, GenerateIRNCommand{TenantID: "2", InvoiceID: "inv-irn", Generator: gen})
	assert.ErrorIs(t, err, sql.ErrNoRows)

	// Paid / partially paid status guard (Spec 07 §5.3).
	seedIRNInvoice(t, db, "inv-paid", "1", "paid", false)
	_, err = uc.Execute(ctx, GenerateIRNCommand{TenantID: "1", InvoiceID: "inv-paid", Generator: gen})
	require.ErrorIs(t, err, ErrIRNPaymentStatus)
	seedIRNInvoice(t, db, "inv-partial", "1", "partially_paid", false)
	_, err = uc.Execute(ctx, GenerateIRNCommand{TenantID: "1", InvoiceID: "inv-partial", Generator: gen})
	require.ErrorIs(t, err, ErrIRNPaymentStatus)

	// Empty line items fall back to the legacy single freight line.
	seedIRNInvoice(t, db, "inv-fallback", "1", "pending", false)
	fb := &fakeIRNGenerator{res: &gstn.IRNResponse{IRN: "IRN-FB"}}
	_, err = uc.Execute(ctx, GenerateIRNCommand{TenantID: "1", InvoiceID: "inv-fallback", Generator: fb})
	require.NoError(t, err)
	require.Len(t, fb.got.LineItems, 1)
	assert.Equal(t, "996511", fb.got.LineItems[0].HSNSACCode)
	assert.Equal(t, "Freight Services", fb.got.LineItems[0].Description)
	assert.InDelta(t, 1180.0, fb.got.LineItems[0].Rate, 1e-9)

	// GSTN failure propagates without persisting anything.
	seedIRNInvoice(t, db, "inv-fail", "1", "pending", false)
	fail := &fakeIRNGenerator{err: errors.New("gsp down")}
	_, err = uc.Execute(ctx, GenerateIRNCommand{TenantID: "1", InvoiceID: "inv-fail", Generator: fail})
	require.ErrorContains(t, err, "gsp down")
	var failedIRN sql.NullString
	require.NoError(t, db.QueryRow(`SELECT irn FROM invoices WHERE id = 'inv-fail'`).Scan(&failedIRN))
	assert.False(t, failedIRN.Valid, "failed GSTN call must not persist an IRN")
}

func TestGenerateIRNUseCase_TenantProfileGSTINWins(t *testing.T) {
	db := newInvoiceWriteTestDB(t)
	seedGSTCompanies(t, db)
	seedIRNInvoice(t, db, "inv-irn-org", "2", "pending", false)
	gen := &fakeIRNGenerator{res: &gstn.IRNResponse{IRN: "IRN-ORG"}}
	_, err := NewGenerateIRNUseCase(db).Execute(context.Background(), GenerateIRNCommand{TenantID: "2", InvoiceID: "inv-irn-org", Generator: gen})
	require.NoError(t, err)
	assert.Equal(t, "29AABCT1234T1Z2", gen.got.SupplierGSTIN, "tenant profile GSTIN wins over the global singleton")
}
