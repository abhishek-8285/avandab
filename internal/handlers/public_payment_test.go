package handlers

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
)

// TestPublicPay_MasksCustomerPII guards audit finding M2 (2026-09-17):
// /pay/{invoiceId} is intentionally public (a customer settles an invoice
// from a link), so the handler must never render the customer's raw phone
// number or email address. Both are masked at the handler boundary.
//
// Red-proven: FAILs against the pre-fix handler that copied custPhone /
// custEmail straight from the DB row into PublicInvoiceView.
func TestPublicPay_MasksCustomerPII(t *testing.T) {
	db := newPaymentTestDB(t)
	defer db.Close()

	const rawPhone = "+919876543210"
	const rawEmail = "ramesh@example.com"
	_, err := db.Exec(`INSERT INTO customers (id, name, phone, email, type, status, tenant_id)
		VALUES ('cust-m2', 'Ramesh Kumar', ?, ?, 'individual', 'active', '1')`, rawPhone, rawEmail)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO bookings (id, booking_number, customer_id, tenant_id, pickup_date, route_id, vehicle_type, passengers, price)
		VALUES ('bk-m2', 'BK-M2', 'cust-m2', '1', datetime('now'), 'route-m2', 'sedan', 1, 1000)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO invoices
		(id, invoice_number, booking_id, customer_id, subtotal, total, payment_status, status, tenant_id, created_at)
		VALUES ('inv-m2', 'INV-M2-001', 'bk-m2', 'cust-m2', 1000, 1180, 'partially_paid', 'active', '1', datetime('now'))`)
	require.NoError(t, err)

	store := sqlite.NewRepository(db)
	svc := service.NewServices(store, &config.Config{}, nil, events.NewInMemoryBus())
	app := &App{DB: db, Services: svc}
	h := &PaymentHandlers{App: app}

	data, err := h.loadPublicPayData(context.Background(), "inv-m2")
	require.NoError(t, err)

	assert.Contains(t, data.Invoice.CustomerPhone, "••••", "customer phone must be masked on the public pay page")
	assert.NotContains(t, data.Invoice.CustomerPhone, rawPhone, "raw phone must not appear")
	assert.Contains(t, data.Invoice.CustomerEmail, "@example.com", "domain is safe to show")
	assert.NotContains(t, data.Invoice.CustomerEmail, rawEmail, "raw email must not appear")
}

// newPaymentTestDB runs the full migration set on a fresh on-disk SQLite
// database. File-based (not in-memory) because loadPublicPayData reaches the
// invoices table through the services layer's own connection; shared-cache
// in-memory mode gives each connection a private view and the row seeded
// from the test handle would be invisible to the handler.
func newPaymentTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := "test_publicpay_mask_" + t.Name() + ".db"
	if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
		require.NoError(t, err)
	}
	db, err := sql.Open("sqlite", "file:"+name+"?mode=rwc&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)")
	require.NoError(t, err)

	migrationsDir := "../../db/migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		for _, cand := range []string{"db/migrations", "../db/migrations", "../../db/migrations"} {
			if _, err := os.Stat(cand); err == nil {
				migrationsDir = cand
				break
			}
		}
	}
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = os.Remove(name) })
	return db
}
