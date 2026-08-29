package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	invoiceapp "transport-app/internal/invoice/application"
	"transport-app/internal/shared"
)

// TestInvoicePDF_SellerFromCompanySettings — Spec: invoice PDF v2 must
// take the seller block from company_settings; the legacy hardcoded
// "Apex Transport Ltd" name is banned. Also asserts the composed data
// picks up GST split + line items + IRN/EWB extras when present.
func TestInvoicePDF_SellerFromCompanySettings(t *testing.T) {
	db := newInvoiceLineTestDB(t)
	app := newMaintHandlerApp(t, db, maintAllowAuthSvc{})
	app.Invoices = &InvoiceHandlers{App: app}
	h := app.Invoices
	h.init()

	_, err := db.Exec(`UPDATE company_settings SET company_name='Devi Transport Co',
		address='22 Goods Yard, Nashik', gst_number='27AABCU9603R1ZX', state_code='27',
		phone='9700000000', email='accounts@devi.example' WHERE id = 1`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers (id, name, phone, gst, address) VALUES ('cust-pdf', 'Bharat Steels', '+91-9000000001', '29AABCB1234C1Z7', '4 Steel Zone, Bengaluru')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt-pdf', '1', 'Nashik', 'Bengaluru', 900, 16, 45000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price) VALUES ('bk-pdf', '1', 'BK-PDF', 'cust-pdf', date('now','+1 day'), 'rt-pdf', 'truck', 45000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, tax, total, tenant_id, cgst, sgst, igst, irn)
		VALUES ('inv-pdf', 'INV-PDF-1', 'bk-pdf', 'cust-pdf', 45000, 0, 47250, '1', 0, 0, 2250, 'irn-test-payload')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoice_line_items (id, tenant_id, invoice_id, line_type, description, quantity, unit_price, amount,
		hsn_sac_code, unit, rate, taxable_value, cgst_rate, sgst_rate, igst_rate,
		cgst_amount, sgst_amount, igst_amount, total)
		VALUES ('li-pdf-1', '1', 'inv-pdf', 'freight', 'FTL Nashik to Bengaluru', 1, 45000, 45000,
		'996511', 'TRIP', 45000, 45000, 0, 0, 5, 0, 0, 2250, 47250)`)
	require.NoError(t, err)

	// Tenant ctx: mirrors auth middleware — invoice reads are tenant-scoped
	// and the repo seam fails closed without it (see sqlite.tenantIDFromCtx).
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)
	dto, err := h.getUC.Execute(ctx, invoiceapp.GetInvoiceQuery{ID: "inv-pdf", TenantID: shared.DefaultTenant})
	require.NoError(t, err)

	data, err := h.buildInvoicePDFData(ctx, &dto, 0, 47250)
	require.NoError(t, err)

	assert.Equal(t, "Devi Transport Co", data.Company.Name, "seller name must come from company_settings")
	assert.NotContains(t, data.Company.Name, "Apex", "hardcoded legacy seller name leaked")
	assert.Equal(t, "27AABCU9603R1ZX", data.Company.GSTIN)
	assert.Equal(t, "Bharat Steels", data.Customer.Name)
	assert.Equal(t, "29", data.Customer.StateCode, "buyer state from GST prefix")
	assert.False(t, data.IntraState, "27 vs 29 is inter-state supply")
	assert.True(t, data.GSTBreakdown, "line items carry HSN + rates")
	assert.InDelta(t, 2250.0, data.IGST, 0.001)
	assert.Len(t, data.Items, 1)
	assert.Equal(t, "irn-test-payload", data.IRN)
	assert.Empty(t, data.SignedQR, "non-image signed_qr payload must be dropped")

	// Full render smoke through the handler.
	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			tctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
			next.ServeHTTP(w, req.WithContext(tctx))
		})
	}).Get("/invoices/{id}/pdf", h.DownloadPDF)

	req := withSession(httptest.NewRequest(http.MethodGet, "/invoices/inv-pdf/pdf", nil), "user-1", "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Disposition"), "INV-PDF-1.pdf")
	assert.True(t, bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF-")), "response body must be a PDF")
}

func TestValidGSTINFormat(t *testing.T) {
	cases := []struct {
		gstin string
		want  bool
		why   string
	}{
		{"27AABCU9603R1ZX", true, "classic GSTN doc example (company)"},
		{"29AABCB1234C1Z7", true, "valid company GSTIN, different state"},
		{"07AAACP0000M1Z9", true, "valid proprietor GSTIN"},
		{"07KUKPS5477RDAF", false, "real-world bad value: idx12 'D' not digit, idx13 'A' not Z"},
		{"27PQRSX5678K1Z2", false, "entity letter idx5 'S' not in P/F/C/H/A/T/B/L/J/G"},
		{"27AABCU9603R1Z", false, "too short"},
		{"27AABCU9603R1ZX1", false, "too long"},
		{"X7AABCU9603R1ZX", false, "state code not numeric"},
		{"27AABCU9603R1Z7", true, "alnum check digit"},
		{"", false, "empty"},
	}
	for _, tc := range cases {
		got := validGSTINFormat(tc.gstin)
		if got != tc.want {
			t.Errorf("validGSTINFormat(%q) = %v, want %v (%s)", tc.gstin, got, tc.want, tc.why)
		}
	}
}
