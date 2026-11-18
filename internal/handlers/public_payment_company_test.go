package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
)

// TestPublicPay_CompanyProfileUsesInvoiceTenant: the public pay page is
// served by URL, not session, but the seller identity must still come from the
// invoice's tenant — never the bootstrap company_settings row.
func TestPublicPay_CompanyProfileUsesInvoiceTenant(t *testing.T) {
	db := newPaymentTestDB(t)
	defer db.Close()

	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-org-x', 'Org X', 'tenant-org-x')`)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE company_settings SET company_name='Bootstrap Logs', gst_number='27BOOTSTRAP1', address='Bootstrap Address' WHERE id=1`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tenant_company_profiles
		(tenant_id, company_name, gst_number, address, phone, email, state_code)
		VALUES ('tenant-org-x', 'Org X Freight', '27ORGX0001X1Z5', 'Org X Address, Pune', '9999999999', 'orgx@example.com', '27')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO customers (id, name, phone, email, type, status, tenant_id)
		VALUES ('cust-org-x', 'Org X Customer', '+919876543299', 'cx@example.com', 'individual', 'active', 'tenant-org-x')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, booking_number, customer_id, tenant_id, pickup_date, route_id, vehicle_type, passengers, price)
		VALUES ('bk-org-x', 'BK-ORG-X', 'cust-org-x', 'tenant-org-x', datetime('now'), 'route-org-x', 'truck', 1, 1000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices
		(id, invoice_number, booking_id, customer_id, subtotal, total, payment_status, status, tenant_id, created_at)
		VALUES ('inv-org-x', 'INV-ORG-X', 'bk-org-x', 'cust-org-x', 1000, 1180, 'pending', 'active', 'tenant-org-x', datetime('now'))`)
	require.NoError(t, err)

	h := &PaymentHandlers{App: &App{
		DB:       db,
		Services: service.NewServices(sqlite.NewRepository(db), &config.Config{}, nil, events.NewInMemoryBus()),
	}}
	data, err := h.loadPublicPayData(context.Background(), "inv-org-x")
	require.NoError(t, err)
	assert.Equal(t, "Org X Freight", data.Company.Name)
	assert.Equal(t, "27ORGX0001X1Z5", data.Company.GSTIN)
	assert.Equal(t, "Org X Address, Pune", data.Company.Address)
	assert.NotContains(t, data.Company.Name, "Bootstrap")
}
