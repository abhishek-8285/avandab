package test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/integration"
	"transport-app/internal/integration/gstn"
	invoiceapp "transport-app/internal/invoice/application"
	"transport-app/internal/invoice/domain/aggregate"
	invoicesql "transport-app/internal/invoice/infrastructure/persistence/sql"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_einvoice_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)&_pragma=foreign_keys(OFF)")
	require.NoError(t, err)

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../db/migrations"), "goose up failed")
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)

	return db
}

// 1. Test Migrations 00047, 00048, 00049 RoundTrip
func TestGST_Migrations_RoundTrip(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Verify 00047 columns and events table
	var count int
	err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='eway_bill_events'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "eway_bill_events table must exist")

	// Verify 00048 tables and columns
	for _, tbl := range []string{"hsn_sac_master", "invoice_sequences"} {
		err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "table %s must exist", tbl)
	}

	// Verify 00049 tables
	for _, tbl := range []string{"fastag_tags", "fastag_transactions"} {
		err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "table %s must exist", tbl)
	}

	// Verify company_config feature flags
	var flagCount int
	err = db.QueryRow(`SELECT count(*) FROM company_config WHERE key IN ('gst_einvoice_enabled', 'ewaybill_auto_generate', 'fastag_auto_kharcha', 'fastag_merchant_id', 'fastag_provider')`).Scan(&flagCount)
	require.NoError(t, err)
	assert.Equal(t, 5, flagCount, "all 5 Spec 07 company_config keys must be seeded")

	// Verify RBAC permissions
	var permCount int
	err = db.QueryRow(`SELECT count(*) FROM permissions WHERE name IN ('integrations:einvoice', 'integrations:ewaybill', 'integrations:fastag')`).Scan(&permCount)
	require.NoError(t, err)
	assert.Equal(t, 3, permCount, "all 3 permissions must be seeded")

	// Down to 46 (reverses 00049, 00048, 00047)
	require.NoError(t, goose.DownTo(db, "../db/migrations", 46), "goose down to 46 failed")

	for _, tbl := range []string{"eway_bill_events", "hsn_sac_master", "invoice_sequences", "fastag_tags", "fastag_transactions"} {
		err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count, "table %s must be dropped after rollback", tbl)
	}
}

// 2. Test State Code Parsing & Intra/Inter-state Determination
func TestGST_StateCode_And_TaxSplit(t *testing.T) {
	// StateCodeFromGSTIN
	assert.Equal(t, "27", invoiceapp.StateCodeFromGSTIN("27AABCU9603R1ZX"))
	assert.Equal(t, "07", invoiceapp.StateCodeFromGSTIN("07AAACP0000M1Z9"))
	assert.Equal(t, "29", invoiceapp.StateCodeFromGSTIN("29AABCU9603R1ZX"))
	assert.Equal(t, "", invoiceapp.StateCodeFromGSTIN("X"))
	assert.Equal(t, "", invoiceapp.StateCodeFromGSTIN(""))

	// IsIntraState
	assert.True(t, invoiceapp.IsIntraState("27AABCU9603R1ZX", "27XYZAB1234C1Z1"))
	assert.False(t, invoiceapp.IsIntraState("27AABCU9603R1ZX", "07AAACP0000M1Z9"))
	assert.False(t, invoiceapp.IsIntraState("27AABCU9603R1ZX", "29AABCU9603R1ZX"))

	// ComputeLineTax - Intra-state (18% on 1000)
	cgst, sgst, igst := invoiceapp.ComputeLineTax(1000.0, 18.0, true)
	assert.Equal(t, 90.0, cgst)
	assert.Equal(t, 90.0, sgst)
	assert.Equal(t, 0.0, igst)

	// ComputeLineTax - Inter-state (18% on 1000)
	cgst, sgst, igst = invoiceapp.ComputeLineTax(1000.0, 18.0, false)
	assert.Equal(t, 0.0, cgst)
	assert.Equal(t, 0.0, sgst)
	assert.Equal(t, 180.0, igst)

	// ComputeLineTax - 5% freight on 5000 intra-state
	cgst, sgst, igst = invoiceapp.ComputeLineTax(5000.0, 5.0, true)
	assert.Equal(t, 125.0, cgst)
	assert.Equal(t, 125.0, sgst)
	assert.Equal(t, 0.0, igst)

	// ComputeLineTax - 5% freight on 5000 inter-state
	cgst, sgst, igst = invoiceapp.ComputeLineTax(5000.0, 5.0, false)
	assert.Equal(t, 0.0, cgst)
	assert.Equal(t, 0.0, sgst)
	assert.Equal(t, 250.0, igst)
}

// 3. Test Deterministic IRN Computation
func TestGST_DeterministicIRN(t *testing.T) {
	inv1 := gstn.InvoiceView{
		InvoiceID:      "inv-123",
		InvoiceNumber:  "INV/2026/0001",
		InvoiceDate:    "2026-08-18",
		SupplierGSTIN:  "27AABCU9603R1ZX",
		RecipientGSTIN: "07AAACP0000M1Z9",
		TotalValue:     1180.0,
		LineItems: []gstn.LineItemView{
			{HSNSACCode: "996511", Description: "Freight", Quantity: 1, Rate: 1000.0, TaxableValue: 1000.0, IGSTRate: 18.0, IGSTAmount: 180.0, Total: 1180.0},
		},
	}

	irn1 := gstn.ComputeIRN(inv1)
	assert.Len(t, irn1, 64, "IRN must be 64-char SHA-256 hex string")

	// Repeat with same data -> exact same IRN
	irn2 := gstn.ComputeIRN(inv1)
	assert.Equal(t, irn1, irn2, "IRN must be deterministic for identical input")

	// Line items reordered -> same IRN due to canonical sorting
	invReordered := inv1
	invReordered.LineItems = []gstn.LineItemView{
		{HSNSACCode: "997113", Description: "Packaging", Quantity: 2, Rate: 50.0, TaxableValue: 100.0, Total: 118.0},
		{HSNSACCode: "996511", Description: "Freight", Quantity: 1, Rate: 1000.0, TaxableValue: 1000.0, Total: 1180.0},
	}
	invReordered2 := inv1
	invReordered2.LineItems = []gstn.LineItemView{
		{HSNSACCode: "996511", Description: "Freight", Quantity: 1, Rate: 1000.0, TaxableValue: 1000.0, Total: 1180.0},
		{HSNSACCode: "997113", Description: "Packaging", Quantity: 2, Rate: 50.0, TaxableValue: 100.0, Total: 118.0},
	}
	assert.Equal(t, gstn.ComputeIRN(invReordered), gstn.ComputeIRN(invReordered2), "IRN must be invariant to line item input order")

	// Changed invoice number -> different IRN
	invDiffNo := inv1
	invDiffNo.InvoiceNumber = "INV/2026/0002"
	assert.NotEqual(t, irn1, gstn.ComputeIRN(invDiffNo))
}

// 4. Test Invoice Aggregate with Tax Split & Totals
func TestGST_InvoiceAggregate_TaxSplit(t *testing.T) {
	now := time.Now()
	inv := aggregate.NewInvoiceAggregate(
		"inv-test-1",
		"1",
		"INV-100",
		"book-100",
		"cust-100",
		nil,
		1000.0,
		180.0,
		0.0,
		1180.0,
		aggregate.PaymentStatusPending,
		now,
	)

	hsn := "996511"
	unit := "KGS"
	inv.AddLineItem(aggregate.LineItem{
		ID:           "li-1",
		TenantID:     "1",
		InvoiceID:    "inv-test-1",
		LineType:     aggregate.LineTypeFreight,
		HSNSACCode:   &hsn,
		Description:  "Goods transport by road",
		Unit:         &unit,
		Quantity:     1,
		UnitPrice:    1000.0,
		Rate:         1000.0,
		TaxableValue: 1000.0,
		CgstRate:     9.0,
		SgstRate:     9.0,
		IgstRate:     0.0,
		CgstAmount:   90.0,
		SgstAmount:   90.0,
		IgstAmount:   0.0,
		Amount:       1000.0,
		Total:        1180.0,
	})

	assert.Equal(t, 1000.0, inv.Subtotal)
	assert.Equal(t, 90.0, inv.Cgst)
	assert.Equal(t, 90.0, inv.Sgst)
	assert.Equal(t, 0.0, inv.Igst)
	assert.Equal(t, 180.0, inv.Tax)
	assert.Equal(t, 1180.0, inv.Total)

	// Set IRN fields
	irn := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	ackNo := "ACK123456"
	ackDate := "2026-08-19 12:00:00"
	signedQR := "data:image/png;base64,mockqr"
	ewb := "EWB31100001"

	inv.IRN = &irn
	inv.IRNAckNo = &ackNo
	inv.IRNAckDate = &ackDate
	inv.SignedQR = &signedQR
	inv.EwbNumber = &ewb

	assert.Equal(t, irn, *inv.IRN)
	assert.Equal(t, ackNo, *inv.IRNAckNo)
	assert.Equal(t, ackDate, *inv.IRNAckDate)
	assert.Equal(t, signedQR, *inv.SignedQR)
	assert.Equal(t, ewb, *inv.EwbNumber)
}

// 5. Test Invoice Persistence Roundtrip with GST Line Items & IRN
func TestGST_InvoiceRepository_PersistenceRoundtrip(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Insert prerequisite customer, route and booking
	_, err := db.Exec(`INSERT INTO customers (id, name, email, phone, gst) VALUES ('cust-1', 'Acme Corp', 'acme@example.com', '9876543210', '07AAACP0000M1Z9')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare) VALUES ('route-1', 'Mumbai', 'Delhi', 1450, 24, 5000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('book-1', '1', 'BKG-001', 'cust-1', 'route-1', '2026-08-20', 'TRUCK', 'confirmed', 5000.0)`)
	require.NoError(t, err)

	repo := invoicesql.NewInvoiceRepository(db)
	ctx := context.Background()

	now := time.Now()
	inv := aggregate.NewInvoiceAggregate(
		"inv-roundtrip-1",
		"1",
		"INV-2026-001",
		"book-1",
		"cust-1",
		nil,
		5000.0,
		900.0,
		0.0,
		5900.0,
		aggregate.PaymentStatusPending,
		now,
	)

	hsn := "996511"
	unit := "TRIP"
	inv.AddLineItem(aggregate.LineItem{
		ID:           "li-rt-1",
		TenantID:     "1",
		InvoiceID:    "inv-roundtrip-1",
		LineType:     aggregate.LineTypeFreight,
		HSNSACCode:   &hsn,
		Description:  "Freight delivery Mumbai to Delhi",
		Unit:         &unit,
		Quantity:     1,
		UnitPrice:    5000.0,
		Rate:         5000.0,
		TaxableValue: 5000.0,
		CgstRate:     0.0,
		SgstRate:     0.0,
		IgstRate:     18.0,
		CgstAmount:   0.0,
		SgstAmount:   0.0,
		IgstAmount:   900.0,
		Amount:       5000.0,
		Total:        5900.0,
	})

	irn := "4b7b996fb92427ae41e4649b934ca495991b7852b855e3b0c44298fc1c149afb"
	ackNo := "ACK998877"
	ackDate := "2026-08-19 14:32:00"
	signedQR := "data:image/png;base64,mockqr_test"
	ewbNo := "311009988776"

	inv.IRN = &irn
	inv.IRNAckNo = &ackNo
	inv.IRNAckDate = &ackDate
	inv.SignedQR = &signedQR
	inv.EwbNumber = &ewbNo

	require.NoError(t, repo.Save(ctx, inv), "saving invoice must succeed")

	// Read back and verify
	loaded, err := repo.Find(ctx, "inv-roundtrip-1", "1")
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, "INV-2026-001", loaded.InvoiceNumber)
	assert.Equal(t, 5000.0, loaded.Subtotal)
	assert.Equal(t, 900.0, loaded.Igst)
	assert.Equal(t, 0.0, loaded.Cgst)
	assert.Equal(t, 0.0, loaded.Sgst)
	assert.Equal(t, 900.0, loaded.Tax)
	assert.Equal(t, 5900.0, loaded.Total)
	require.NotNil(t, loaded.IRN)
	assert.Equal(t, irn, *loaded.IRN)
	require.NotNil(t, loaded.IRNAckNo)
	assert.Equal(t, ackNo, *loaded.IRNAckNo)
	require.NotNil(t, loaded.SignedQR)
	assert.Equal(t, signedQR, *loaded.SignedQR)
	require.NotNil(t, loaded.EwbNumber)
	assert.Equal(t, ewbNo, *loaded.EwbNumber)

	require.Len(t, loaded.LineItems, 1)
	assert.Equal(t, "Freight delivery Mumbai to Delhi", loaded.LineItems[0].Description)
	assert.Equal(t, 900.0, loaded.LineItems[0].IgstAmount)
}

func TestGST_HTTP_Endpoints(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Seed customer, booking, and invoice
	_, err := db.Exec(`INSERT INTO customers (id, name, email, phone, gst) VALUES ('cust-http', 'Delhi Logistics', 'dl@example.com', '9123456789', '07AAACP0000M1Z9')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare) VALUES ('route-http', 'Mumbai', 'Delhi', 1450, 24, 10000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('book-http', '1', 'BKG-HTTP-1', 'cust-http', 'route-http', '2026-08-20', 'TRUCK', 'confirmed', 10000.0)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices (id, tenant_id, invoice_number, booking_id, customer_id, subtotal, tax, discount, total, payment_status, status, created_at, updated_at) VALUES ('inv-http-1', '1', 'INV-HTTP-1', 'book-http', 'cust-http', 10000.0, 1800.0, 0.0, 11800.0, 'pending', 'outstanding', datetime('now'), datetime('now'))`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoice_line_items (id, tenant_id, invoice_id, line_type, hsn_sac_code, description, quantity, unit_price, rate, taxable_value, igst_rate, igst_amount, amount, total) VALUES ('li-http-1', '1', 'inv-http-1', 'freight', '996511', 'Highway Freight', 1, 10000.0, 10000.0, 10000.0, 18.0, 1800.0, 10000.0, 11800.0)`)
	require.NoError(t, err)

	cfg := integration.Config{
		GSTN: gstn.Config{
			Enabled: true,
			UseMock: true,
		},
	}
	h := integration.NewHandler(cfg, &stubAuthSvc{}, db)

	r := chi.NewRouter()
	r.Use(authInjectMiddleware)
	h.Register(r)

	// A) POST /api/v1/integrations/gstn/einvoice/irn - 404 for unknown invoice
	{
		body, _ := json.Marshal(map[string]string{"invoice_id": "non-existent"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/gstn/einvoice/irn", bytes.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	}

	// B) POST /api/v1/integrations/gstn/einvoice/irn - 201 Success
	var generatedIRN string
	{
		body, _ := json.Marshal(map[string]string{"invoice_id": "inv-http-1"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/gstn/einvoice/irn", bytes.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)

		var resp gstn.IRNResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "inv-http-1", resp.InvoiceID)
		assert.Len(t, resp.IRN, 64)
		assert.NotEmpty(t, resp.AckNo)
		assert.NotEmpty(t, resp.SignedQR)
		generatedIRN = resp.IRN

		// Verify persisted in DB
		var dbIRN string
		err = db.QueryRow(`SELECT irn FROM invoices WHERE id='inv-http-1'`).Scan(&dbIRN)
		require.NoError(t, err)
		assert.Equal(t, generatedIRN, dbIRN)
	}

	// C) POST /api/v1/integrations/gstn/einvoice/irn - 409 Conflict when IRN already exists
	{
		body, _ := json.Marshal(map[string]string{"invoice_id": "inv-http-1"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/gstn/einvoice/irn", bytes.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusConflict, w.Code)
	}

	// D) POST /api/v1/integrations/gstn/einvoice/push - 200 Success
	{
		body, _ := json.Marshal(map[string]string{"invoice_id": "inv-http-1", "irn": generatedIRN})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/gstn/einvoice/push", bytes.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp gstn.PushResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp.Pushed)
		assert.Equal(t, generatedIRN, resp.IRN)
	}
}
