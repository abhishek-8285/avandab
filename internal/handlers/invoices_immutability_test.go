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

func newInvoiceGuardHandlerDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_inv_guard_h_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}
	goose.SetLogger(goose.NopLogger())
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newInvoiceGuardHandlerApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		_ = os.Chdir("../..")
	}
	tmpl, err := parseTemplates(maintAllowAuthSvc{})
	require.NoError(t, err)
	cfg := &config.Config{AppEnv: "testing", CookieSecret: "test-cookie-secret-32-chars-long!"}
	services := service.NewServices(repoSQLite.NewRepository(db), cfg,
		slog.New(slog.NewTextHandler(io.Discard, nil)), events.NewInMemoryBus())
	app := &App{
		DB:        db,
		Config:    cfg,
		Services:  services,
		Templates: tmpl,
		AuthSrv:   maintAllowAuthSvc{},
	}
	app.Invoices = &InvoiceHandlers{App: app}
	return app
}

func insertGuardHandlerInvoice(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	suffix := strings.ReplaceAll(id, "-", "_")
	_, err := db.Exec(`INSERT INTO customers (id, name, phone) VALUES (?, 'Guard Buyer', '+91-9000000000')`, "cust-"+suffix)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare)
		VALUES (?, '1', 'Mumbai', 'Pune', 150, 3, 5000)`, "rt-"+suffix)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price)
		VALUES (?, '1', ?, ?, date('now','+1 day'), ?, 'truck', 20000)`,
		"bk-"+suffix, "BK-"+strings.ToUpper(suffix), "cust-"+suffix, "rt-"+suffix)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, tax, total, payment_status, tenant_id)
		VALUES (?, ?, ?, ?, 1000, 180, 1180, 'pending', '1')`,
		id, "INV-"+strings.ToUpper(suffix), "bk-"+suffix, "cust-"+suffix)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO invoice_line_items (id, tenant_id, invoice_id, line_type, description, quantity, unit_price, amount,
		hsn_sac_code, unit, rate, taxable_value, cgst_rate, sgst_rate, igst_rate,
		cgst_amount, sgst_amount, igst_amount, total)
		VALUES ('li-`+suffix+`', '1', ?, 'freight', 'Freight leg', 1, 1180, 1000,
		'996511', 'NOS', 1180, 1000, 9, 9, 0, 90, 90, 0, 1180)`, id)
	require.NoError(t, err)
}

// TestInvoiceHandlers_ImmutabilityGuards — server-side GST immutability:
// e-invoiced invoices and invoices carrying payments must answer HTTP 409
// on delete / line-item mutation; clean drafts still delete.
func TestInvoiceHandlers_ImmutabilityGuards(t *testing.T) {
	cases := []struct {
		name        string
		setup       func(t *testing.T, db *sql.DB, invID string)
		path        string
		wantCode    int
		wantBody    string // empty → no body assertion
		wantDeleted bool   // invoice row must be gone afterwards
	}{
		{
			name: "delete_blocked_einvoiced",
			setup: func(t *testing.T, db *sql.DB, invID string) {
				insertGuardHandlerInvoice(t, db, invID)
				_, err := db.Exec(`UPDATE invoices SET irn = ? WHERE id = ?`, "irn-"+invID, invID)
				require.NoError(t, err)
			},
			path:     "/invoices/%s/delete",
			wantCode: http.StatusConflict,
			wantBody: "invoice is e-invoiced; issue credit note instead",
		},
		{
			name: "delete_blocked_with_payments",
			setup: func(t *testing.T, db *sql.DB, invID string) {
				insertGuardHandlerInvoice(t, db, invID)
				_, err := db.Exec(`INSERT INTO payments (id, tenant_id, invoice_id, payment_date, amount, method)
					VALUES (?, '1', ?, datetime('now'), 500.0, 'upi')`, "pay-"+invID, invID)
				require.NoError(t, err)
			},
			path:     "/invoices/%s/delete",
			wantCode: http.StatusConflict,
			wantBody: "invoice has payments recorded; cannot delete",
		},
		{
			name: "delete_allowed_draft",
			setup: func(t *testing.T, db *sql.DB, invID string) {
				insertGuardHandlerInvoice(t, db, invID)
			},
			path:        "/invoices/%s/delete",
			wantCode:    http.StatusSeeOther,
			wantDeleted: true,
		},
		{
			name: "line_item_delete_blocked_einvoiced",
			setup: func(t *testing.T, db *sql.DB, invID string) {
				insertGuardHandlerInvoice(t, db, invID)
				_, err := db.Exec(`UPDATE invoices SET irn = ? WHERE id = ?`, "irn-"+invID, invID)
				require.NoError(t, err)
			},
			path:     "/invoices/%s/line-items/li-x/delete",
			wantCode: http.StatusConflict,
			wantBody: "invoice is e-invoiced; issue credit note instead",
		},
		{
			name: "line_item_delete_blocked_paid",
			setup: func(t *testing.T, db *sql.DB, invID string) {
				insertGuardHandlerInvoice(t, db, invID)
				_, err := db.Exec(`UPDATE invoices SET payment_status = 'paid' WHERE id = ?`, invID)
				require.NoError(t, err)
			},
			path:     "/invoices/%s/line-items/li-x/delete",
			wantCode: http.StatusConflict,
			wantBody: "invoice has payments recorded; cannot delete",
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
			r.With(tenantMW).Post("/invoices/{id}/line-items/{lineId}/delete", app.Invoices.DeleteLineItem)
			r.With(tenantMW).Post("/invoices/{id}/delete", app.Invoices.Delete)

			req := withSession(httptest.NewRequest(http.MethodPost, fmt.Sprintf(tc.path, invID), nil), "user-1", "admin")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code, tc.name)
			if tc.wantBody != "" {
				assert.Contains(t, w.Body.String(), tc.wantBody)
			}

			var count int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE id = ?`, invID).Scan(&count))
			if tc.wantDeleted {
				assert.Equal(t, 0, count, "draft invoice must be deleted")
			} else {
				assert.Equal(t, 1, count, "invoice must survive a blocked delete")
			}
		})
	}
}
