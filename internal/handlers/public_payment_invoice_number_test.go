package handlers

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
)

// TestLoadPublicPayData_PublicLookupRequiresInvoiceID guards audit finding M11
// (2026-11-12): the public payment page accepted either the random invoice ID
// or the sequential invoice number (`INV/2026-27/0001`). Sequential numbers are
// trivially enumerable, so anyone could walk /pay/<number> and harvest customer
// billing identities. Public lookups must accept only the UUID ID.
func TestLoadPublicPayData_PublicLookupRequiresInvoiceID(t *testing.T) {
	db := newPaymentTestDB(t)
	defer db.Close()

	_, err := db.Exec(`INSERT INTO customers (id, name, phone, email, type, status, tenant_id)
		VALUES ('cust-m11', 'Ramesh Kumar', '+919876543210', 'ramesh@example.com',
		        'individual', 'active', '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings
		(id, booking_number, customer_id, tenant_id, pickup_date, route_id, vehicle_type, passengers, price)
		VALUES ('bk-m11', 'BK-M11', 'cust-m11', '1', datetime('now'), 'route-m11', 'sedan', 1, 1000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices
		(id, invoice_number, booking_id, customer_id, subtotal, total, payment_status, status, tenant_id, created_at)
		VALUES ('inv-m11-id', 'INV/2026-27/0001', 'bk-m11', 'cust-m11', 1000, 1180,
		        'partially_paid', 'active', '1', datetime('now'))`)
	require.NoError(t, err)

	store := sqlite.NewRepository(db)
	svc := service.NewServices(store, &config.Config{}, nil, events.NewInMemoryBus())
	h := &PaymentHandlers{App: &App{DB: db, Services: svc}}

	_, err = h.loadPublicPayData(context.Background(), "INV/2026-27/0001")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("public pay must not resolve sequential invoice numbers, got %v", err)
	}

	data, err := h.loadPublicPayData(context.Background(), "inv-m11-id")
	require.NoError(t, err)
	assert.Equal(t, "inv-m11-id", data.Invoice.ID)
	assert.Equal(t, "INV/2026-27/0001", data.Invoice.InvoiceNumber)
}
