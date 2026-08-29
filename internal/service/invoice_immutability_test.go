package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/config"
	"transport-app/internal/domain"
	"transport-app/internal/events"
	repoSQLite "transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func newInvoiceGuardTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_inv_guard_%d", time.Now().UnixNano())
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
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newInvoiceGuardServices(t *testing.T, db *sql.DB) *service.Services {
	t.Helper()
	cfg := &config.Config{AppEnv: "testing"}
	return service.NewServices(repoSQLite.NewRepository(db), cfg,
		slog.New(slog.NewTextHandler(io.Discard, nil)), events.NewInMemoryBus())
}

func insertGuardInvoiceFixture(t *testing.T, db *sql.DB, id string) {
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
}

// TestInvoiceService_DeleteInvoice_ImmutabilityGuard — GST law: e-invoiced
// invoices and invoices with payments must never be hard-deleted; drafts
// with no payments may be.
func TestInvoiceService_DeleteInvoice_ImmutabilityGuard(t *testing.T) {
	cases := []struct {
		name     string
		withIRN  bool
		withPay  bool
		wantErr  error
		wantGone bool
	}{
		{name: "blocked_einvoiced_draft", withIRN: true, wantErr: domain.ErrInvoiceEInvoiced},
		{name: "blocked_with_payments", withPay: true, wantErr: domain.ErrInvoiceHasPayments},
		{name: "allowed_draft_no_payments", wantGone: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newInvoiceGuardTestDB(t)
			svc := newInvoiceGuardServices(t, db)
			ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)

			id := "inv-" + tc.name
			insertGuardInvoiceFixture(t, db, id)
			if tc.withIRN {
				_, err := db.Exec(`UPDATE invoices SET irn = ? WHERE id = ?`, "irn-"+id, id)
				require.NoError(t, err)
			}
			if tc.withPay {
				_, err := db.Exec(`INSERT INTO payments (id, tenant_id, invoice_id, payment_date, amount, method)
					VALUES ('pay-`+tc.name+`', '1', ?, datetime('now'), 500.0, 'upi')`, id)
				require.NoError(t, err)
			}

			err := svc.Invoices.DeleteInvoice(ctx, domain.InvoiceID(id))
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				var count int
				require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE id = ?`, id).Scan(&count))
				assert.Equal(t, 1, count, "invoice must survive a blocked delete")
				if tc.withPay {
					require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM payments WHERE invoice_id = ?`, id).Scan(&count))
					assert.Equal(t, 1, count, "payment history must survive a blocked delete")
				}
				return
			}
			require.NoError(t, err)
			var count int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE id = ?`, id).Scan(&count))
			assert.Equal(t, 0, count, "draft invoice without payments must be deletable")
		})
	}
}

// TestInvoiceService_UpdateInvoice_EInvoicedLocksFinancials — with an IRN
// present, subtotal/tax/discount/total changes are rejected; a pure payment
// status advance stays allowed.
func TestInvoiceService_UpdateInvoice_EInvoicedLocksFinancials(t *testing.T) {
	db := newInvoiceGuardTestDB(t)
	svc := newInvoiceGuardServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)

	insertGuardInvoiceFixture(t, db, "inv-einv-upd")
	_, err := db.Exec(`UPDATE invoices SET irn = 'irn-einv-upd' WHERE id = 'inv-einv-upd'`)
	require.NoError(t, err)

	// Financial change → blocked.
	_, err = svc.Invoices.UpdateInvoice(ctx, "inv-einv-upd", "bk-inv_einv_upd", "cust-inv_einv_upd",
		nil, 1200, 216, 0, 1416, domain.PaymentStatusPending)
	require.ErrorIs(t, err, domain.ErrInvoiceEInvoiced)

	var total float64
	require.NoError(t, db.QueryRow(`SELECT total FROM invoices WHERE id = 'inv-einv-upd'`).Scan(&total))
	assert.Equal(t, 1180.0, total, "financial fields must be untouched after blocked update")

	// Same financials, only payment status advances → allowed.
	updated, err := svc.Invoices.UpdateInvoice(ctx, "inv-einv-upd", "bk-inv_einv_upd", "cust-inv_einv_upd",
		nil, 1000, 180, 0, 1180, domain.PaymentStatusPaid)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentStatusPaid, updated.PaymentStatus)
}

// TestInvoiceService_EnsureLineItemsEditable — line items are locked once
// the invoice is e-invoiced or money has moved (non-pending status).
func TestInvoiceService_EnsureLineItemsEditable(t *testing.T) {
	cases := []struct {
		name    string
		irn     bool
		payStat string
		wantErr error
	}{
		{name: "blocked_irn", irn: true, wantErr: domain.ErrInvoiceEInvoiced},
		{name: "blocked_paid_status", payStat: "paid", wantErr: domain.ErrInvoiceHasPayments},
		{name: "allowed_pending_no_irn", payStat: "pending"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newInvoiceGuardTestDB(t)
			svc := newInvoiceGuardServices(t, db)
			ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)

			id := "inv-li-" + tc.name
			insertGuardInvoiceFixture(t, db, id)
			if tc.irn {
				_, err := db.Exec(`UPDATE invoices SET irn = ? WHERE id = ?`, "irn-"+id, id)
				require.NoError(t, err)
			}
			if tc.payStat != "" && tc.payStat != "pending" {
				_, err := db.Exec(`UPDATE invoices SET payment_status = ? WHERE id = ?`, tc.payStat, id)
				require.NoError(t, err)
			}

			err := svc.Invoices.EnsureLineItemsEditable(ctx, domain.InvoiceID(id))
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}
