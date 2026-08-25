package handlers

import (
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
	"transport-app/internal/config"
	"transport-app/internal/events"
	repoSQLite "transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func newInvoiceLineTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_inv_line_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}
	goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestInvoiceHandlers_LineItemGSTFailClosed verifies the tax-resolution fixes:
// an unknown HSN code must be rejected (400), never silently billed at the
// 18% default; a known code must produce the correct CGST/SGST split.
func TestInvoiceHandlers_LineItemGSTFailClosed(t *testing.T) {
	db := newInvoiceLineTestDB(t)
	app := newMaintHandlerApp(t, db, maintAllowAuthSvc{})
	app.Services = service.NewServices(
		repoSQLite.NewRepository(db),
		&config.Config{AppEnv: "testing", CookieSecret: "test-cookie-secret-32-chars-long!"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		events.NewInMemoryBus(),
	)
	app.Invoices = &InvoiceHandlers{App: app}

	_, err := db.Exec(`INSERT INTO customers (id, name, phone, gst) VALUES ('cust-gst', 'GST Buyer', '+91-9000000000', '27ABCDE1234F1Z5')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt-gst', '1', 'Mumbai', 'Pune', 150, 3, 5000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price) VALUES ('bk-gst', '1', 'BK-GST', 'cust-gst', date('now','+1 day'), 'rt-gst', 'truck', 20000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, total, tenant_id) VALUES ('inv-gst', 'INV-GST', 'bk-gst', 'cust-gst', 0, 0, '1')`)
	require.NoError(t, err)
	// hsn_sac_master code 996511 (SAC, 5%) is pre-seeded by migration 00048.
	require.NoError(t, err)

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}).Post("/invoices/{id}/line-items", app.Invoices.AddLineItem)

	form := strings.NewReader("hsn_sac_code=999999&description=Bogus&quantity=1&rate=1000")
	req := withSession(httptest.NewRequest("POST", "/invoices/inv-gst/line-items", form), "user-1", "admin")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, "unknown HSN code must fail closed")
	assert.Contains(t, w.Body.String(), "unknown HSN/SAC")

	form = strings.NewReader("hsn_sac_code=996511&description=Freight&quantity=2&rate=1000")
	req = withSession(httptest.NewRequest("POST", "/invoices/inv-gst/line-items", form), "user-1", "admin")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusSeeOther, w.Code, "known HSN code must succeed")

	var taxable, cgstAmt, sgstAmt, igstAmt float64
	err = db.QueryRow(`SELECT taxable_value, cgst_amount, sgst_amount, igst_amount FROM invoice_line_items WHERE invoice_id = 'inv-gst'`).
		Scan(&taxable, &cgstAmt, &sgstAmt, &igstAmt)
	require.NoError(t, err)
	assert.Equal(t, 2000.0, taxable)
	assert.InDelta(t, 50.0, cgstAmt, 0.001, "intra-state 5% splits into CGST half")
	assert.InDelta(t, 50.0, sgstAmt, 0.001)
	assert.Equal(t, 0.0, igstAmt)

	var invTotal float64
	err = db.QueryRow(`SELECT total FROM invoices WHERE id = 'inv-gst'`).Scan(&invTotal)
	require.NoError(t, err)
	assert.InDelta(t, 2100.0, invTotal, 0.001, "header totals recalculated in same tx")
}

// TestInvoiceHandlers_LineItemsEditorLockState verifies the editor renders
// read-only for locked invoices (IRN set or payment status != pending) and
// keeps the delete form only for editable ones.
func TestInvoiceHandlers_LineItemsEditorLockState(t *testing.T) {
	cases := []struct {
		name         string
		setup        func(t *testing.T, db *sql.DB, invID string)
		wantHint     string // empty → expect unlocked rendering
		wantDisabled bool
	}{
		{
			name: "pending_invoice_shows_delete_form",
			setup: func(t *testing.T, db *sql.DB, invID string) {
				insertGuardHandlerInvoice(t, db, invID)
			},
		},
		{
			name: "einvoiced_invoice_renders_lock_hint",
			setup: func(t *testing.T, db *sql.DB, invID string) {
				insertGuardHandlerInvoice(t, db, invID)
				_, err := db.Exec(`UPDATE invoices SET irn = ? WHERE id = ?`, "irn-"+invID, invID)
				require.NoError(t, err)
			},
			wantHint:     "Locked (e-invoiced)",
			wantDisabled: true,
		},
		{
			name: "paid_invoice_renders_lock_hint",
			setup: func(t *testing.T, db *sql.DB, invID string) {
				insertGuardHandlerInvoice(t, db, invID)
				_, err := db.Exec(`UPDATE invoices SET payment_status = 'paid' WHERE id = ?`, invID)
				require.NoError(t, err)
			},
			wantHint:     "Locked (paid)",
			wantDisabled: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newInvoiceGuardHandlerDB(t)
			app := newInvoiceGuardHandlerApp(t, db)

			invID := "inv-" + tc.name
			tc.setup(t, db, invID)

			r := chi.NewRouter()
			tenantMW := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
					next.ServeHTTP(w, req.WithContext(ctx))
				})
			}
			r.With(tenantMW).Get("/invoices/{id}/line-items", app.Invoices.LineItemsEditor)

			req := withSession(httptest.NewRequest(http.MethodGet, "/invoices/"+invID+"/line-items", nil), "user-1", "admin")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			body := w.Body.String()
			if tc.wantHint == "" {
				assert.Contains(t, body, `data-confirm="Are you sure? This cannot be undone."`, "editable invoice must render the delete form")
				assert.NotContains(t, body, "<fieldset disabled>")
				assert.NotContains(t, body, "Locked (")
				return
			}
			assert.Contains(t, body, tc.wantHint)
			assert.Contains(t, body, "<fieldset disabled>", "add-item form must be disabled when locked")
			assert.NotContains(t, body, `data-confirm="Are you sure? This cannot be undone."`, "locked invoice must not render any delete form")
		})
	}
}
