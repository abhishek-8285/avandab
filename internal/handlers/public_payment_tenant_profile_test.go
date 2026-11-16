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

// TestLoadPublicPayData_TenantCompanyProfile guards audit finding H2
// (2026-11-12): GET /pay/{invoiceId} is public and used to render
// company_settings id=1 — the PLATFORM-global singleton — onto every tenant's
// invoice. That leaked the platform's (or another org's) name, GSTIN, address
// and contact details to anyone holding an invoice link. Each tenant must
// render its own tenant_company_profiles row; tenant '1' keeps the global
// bootstrap row for backwards compatibility.
func TestLoadPublicPayData_TenantCompanyProfile(t *testing.T) {
	db := newPaymentTestDB(t)
	defer db.Close()

	_, err := db.Exec(`INSERT INTO tenants (id, name, slug, status)
		VALUES ('tenant-h2', 'H2 Tenant', 'h2-tenant', 'active')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO company_settings
		(id, company_name, gst_number, address, state_code, currency, timezone)
		VALUES (1, 'Platform Global Co', '27GLOBAL0000A1Z5', 'Global HQ, Mumbai', '27', 'INR', 'Asia/Kolkata')
		ON CONFLICT(id) DO UPDATE SET
			company_name = 'Platform Global Co',
			gst_number = '27GLOBAL0000A1Z5',
			address = 'Global HQ, Mumbai',
			state_code = '27'`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO tenant_company_profiles
		(tenant_id, company_name, gst_number, address, phone, email, state_code)
		VALUES ('tenant-h2', 'Acme Freight', '29ACME0000A1Z5', 'Acme House, Bengaluru',
		        '+91 9111111111', 'billing@acme.example', '29')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO customers (id, name, phone, email, type, status, tenant_id)
		VALUES ('cust-h2', 'Ramesh Kumar', '+919876543210', 'ramesh@example.com',
		        'individual', 'active', 'tenant-h2')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings
		(id, booking_number, customer_id, tenant_id, pickup_date, route_id, vehicle_type, passengers, price)
		VALUES ('bk-h2', 'BK-H2', 'cust-h2', 'tenant-h2', datetime('now'), 'route-h2', 'sedan', 1, 1000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices
		(id, invoice_number, booking_id, customer_id, subtotal, total, payment_status, status, tenant_id, created_at)
		VALUES ('inv-h2', 'INV-H2-001', 'bk-h2', 'cust-h2', 1000, 1180, 'partially_paid', 'active',
		        'tenant-h2', datetime('now'))`)
	require.NoError(t, err)

	store := sqlite.NewRepository(db)
	svc := service.NewServices(store, &config.Config{}, nil, events.NewInMemoryBus())
	h := &PaymentHandlers{App: &App{DB: db, Services: svc}}

	data, err := h.loadPublicPayData(context.Background(), "inv-h2")
	require.NoError(t, err)

	assert.Equal(t, "Acme Freight", data.Company.Name,
		"invoice must render the invoice tenant's company name")
	assert.Equal(t, "29ACME0000A1Z5", data.Company.GSTIN,
		"invoice must render the invoice tenant's GSTIN")
	assert.Equal(t, "Acme House, Bengaluru", data.Company.Address)
	assert.Equal(t, "29", data.Company.StateCode)
	assert.NotEqual(t, "Platform Global Co", data.Company.Name,
		"platform-global company_settings must not leak onto another tenant's invoice")
	assert.NotEqual(t, "27GLOBAL0000A1Z5", data.Company.GSTIN)
	assert.NotContains(t, data.Title, "Platform Global Co")
}
