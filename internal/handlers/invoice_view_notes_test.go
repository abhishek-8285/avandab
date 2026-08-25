package handlers

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/config"
	"transport-app/internal/events"
	repoSQLite "transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// invoiceViewTestEnv builds a migrated in-memory DB, a real service stack
// (including Notes), and an InvoiceHandlers wired to a tenant-scoped router
// exposing View + ViewByNumber.
func invoiceViewTestEnv(t *testing.T) (*chi.Mux, *InvoiceHandlers, *sql.DB) {
	t.Helper()
	db := newInvoiceLineTestDB(t)
	app := newMaintHandlerApp(t, db, maintAllowAuthSvc{})
	app.Services = service.NewServices(
		repoSQLite.NewRepository(db),
		&config.Config{AppEnv: "testing", CookieSecret: "test-cookie-secret-32-chars-long!"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		events.NewInMemoryBus(),
	)
	app.Invoices = &InvoiceHandlers{App: app}
	h := app.Invoices
	h.init()

	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		gr.Get("/invoices/{id}", h.View)
		gr.Get("/invoices/number/{number}", h.ViewByNumber)
	})
	return r, h, db
}

func getView(t *testing.T, r *chi.Mux, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := withSession(httptest.NewRequest(http.MethodGet, path, nil), "user-1", "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func seedInvoiceViewInvoices(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO customers (id, name, phone) VALUES ('cust-view', 'View Buyer', '+91-9000000000')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('rt-view', '1', 'Mumbai', 'Pune', 150, 3, 5000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price)
		VALUES ('bk-view', '1', 'BK-VIEW', 'cust-view', date('now','+1 day'), 'rt-view', 'truck', 5000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, total, tenant_id)
		VALUES ('inv-view', 'INV-VIEW-1', 'bk-view', 'cust-view', 5000, 5000, '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, total, tenant_id)
		VALUES ('inv-view2', 'INV-VIEW-2', 'bk-view', 'cust-view', 5000, 5000, '1')`)
	require.NoError(t, err)
}

// TestInvoiceView_PassesNotesAndLocked verifies the view payload contract:
// Locked is false for a bare invoice, flips true once a payment exists
// (destructive affordances replaced by the locked hint), and issued credit/
// debit notes render in the history table.
func TestInvoiceView_PassesNotesAndLocked(t *testing.T) {
	r, _, db := invoiceViewTestEnv(t)
	seedInvoiceViewInvoices(t, db)

	w := getView(t, r, "/invoices/inv-view")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := w.Body.String()

	assert.Contains(t, body, "Issue Credit Note", "note form must always be reachable")
	assert.Contains(t, body, "/invoices/inv-view/credit-note")
	assert.Contains(t, body, "/invoices/inv-view/debit-note")
	assert.Contains(t, body, "Manage Line Items", "unlocked invoice keeps edit entry point")
	assert.NotContains(t, body, "Locked — corrections", "bare invoice is not locked")

	_, err := db.Exec(`INSERT INTO payments (id, invoice_id, payment_date, amount, method)
		VALUES ('pay-view-1', 'inv-view', datetime('now'), 5000, 'cash')`)
	require.NoError(t, err)

	w = getView(t, r, "/invoices/inv-view")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body = w.Body.String()

	assert.Contains(t, body, "Locked — corrections", "payment presence must lock the UI")
	assert.NotContains(t, body, "Manage Line Items", "locked invoice hides line-item editor link")

	_, err = db.Exec(`INSERT INTO credit_debit_notes
		(id, tenant_id, note_number, note_type, invoice_id, reason, taxable_value, igst, cgst, sgst, total)
		VALUES ('cn-view-1', '1', 'CN/2026/0001', 'credit', 'inv-view', 'rate correction after e-invoice', 1000, 180, 0, 0, 1180)`)
	require.NoError(t, err)

	w = getView(t, r, "/invoices/inv-view")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body = w.Body.String()
	assert.Contains(t, body, "CN/2026/0001", "issued note must appear in history table")
	assert.Contains(t, body, "rate correction after e-invoice")
	assert.Contains(t, body, ">Credit<")
}

// TestInvoiceViewByNumber_FullDTOWithIRNCancel — ViewByNumber previously fed
// the template an InvoiceWithJoins with no IRN fields at all; it now resolves
// the ID and loads the full DTO so IRN state, cancellation and lock flags all
// render on the by-number route too.
func TestInvoiceViewByNumber_FullDTOWithIRNCancel(t *testing.T) {
	r, _, db := invoiceViewTestEnv(t)
	seedInvoiceViewInvoices(t, db)

	_, err := db.Exec(`UPDATE invoices SET irn = '64-char-irn-test-value-000000000000000000000000000000',
		irn_ack_no = 'ACK-9001', irn_ack_date = '2026-08-20', irn_cancelled_at = datetime('now')
		WHERE id = 'inv-view2'`)
	require.NoError(t, err)

	w := getView(t, r, "/invoices/number/INV-VIEW-2")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := w.Body.String()

	assert.Contains(t, body, "IRN Cancelled", "cancelled IRN badge must render")
	assert.Contains(t, body, "ACK-9001")
	assert.Contains(t, body, "Cancelled At")
	assert.Contains(t, body, "Locked — corrections", "IRN presence locks the UI even without payments")
}
