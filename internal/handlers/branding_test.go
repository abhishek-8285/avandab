package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

func TestTenantBrand_Resolution(t *testing.T) {
	db := newCustomersSelectedDB(t)
	defer db.Close()
	app := newCustomersSelectedApp(t, db, &mockAuthSvc{})

	// 1. Context without tenant -> falls back to company_settings (id=1)
	bPlatform := app.tenantBrand(context.Background())
	assert.Equal(t, "Transport Company", bPlatform.Name)

	// 2. When company_settings is empty -> falls back to product default "Avandab"
	_, err := db.Exec(`UPDATE company_settings SET company_name = '', logo_path = '' WHERE id = 1`)
	require.NoError(t, err)
	bDefault := app.tenantBrand(context.Background())
	assert.Equal(t, "Avandab", bDefault.Name)
	assert.Empty(t, bDefault.Logo)

	// 3. Tenant with tenant_company_profiles entry -> returns tenant's custom brand
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-custom-brand', 'Custom Transporter', 'custom-transporter')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tenant_company_profiles (tenant_id, company_name, logo_path) VALUES ('tenant-custom-brand', 'Apex Express Logistics', '/static/logos/apex.png')`)
	require.NoError(t, err)

	ctxTenantCustom := shared.ContextWithTenantID(context.Background(), "tenant-custom-brand")
	bCustom := app.tenantBrand(ctxTenantCustom)
	assert.Equal(t, "Apex Express Logistics", bCustom.Name)
	assert.Equal(t, "/static/logos/apex.png", bCustom.Logo)
}

func TestCustomerPortal_WhiteLabelRendering(t *testing.T) {
	db := newCustomersSelectedDB(t)
	defer db.Close()
	app := newCustomersSelectedApp(t, db, &mockAuthSvc{})

	tenantID := "tenant-wl-shipper"
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES (?, 'Super Cargo', 'super-cargo')`, tenantID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO tenant_company_profiles (tenant_id, company_name, logo_path) VALUES (?, 'Super Cargo Transporters', '/static/logos/supercargo.svg')`, tenantID)
	require.NoError(t, err)

	// Seed customer, user, customer_user, route, booking, trip
	_, err = db.Exec(`INSERT INTO customers (id, tenant_id, name, phone) VALUES ('cust-wl-1', ?, 'Shipper Corp', '9988776655')`, tenantID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, email, password_hash, name, phone, role_id, status) VALUES ('u-wl-shipper', 'shipper@corp.com', 'hash', 'Shipper Rep', '9988776655', 2, 'active')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customer_users (id, customer_id, user_id) VALUES ('cu-wl-1', 'cust-wl-1', 'u-wl-shipper')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt-wl-1', 'Delhi', 'Jaipur', 280, 5.5, 18000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, passengers, price, status, tenant_id) VALUES ('b-wl-1', 'BK-WL-100', 'cust-wl-1', '2026-10-30', 'rt-wl-1', 'truck', 0, 18000, 'confirmed', ?)`, tenantID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, booking_id, route_id, departure_time, status, tenant_id) VALUES ('t-wl-1', 'TRP-WL-500', 'b-wl-1', 'rt-wl-1', '2026-10-30', 'in_transit', ?)`, tenantID)
	require.NoError(t, err)

	portal := NewCustomerPortalHandlers(app)

	// 1. Test /customer/bookings
	reqBookings := httptest.NewRequest(http.MethodGet, "/customer/bookings", nil)
	reqBookings = reqBookings.WithContext(shared.ContextWithTenantID(reqBookings.Context(), shared.TenantID(tenantID)))
	reqBookings = reqBookings.WithContext(context.WithValue(reqBookings.Context(), auth.ContextUser, &auth.SessionData{UserID: "u-wl-shipper", Role: "customer"}))

	rrBookings := httptest.NewRecorder()
	portal.ListMyBookings(rrBookings, reqBookings)
	assert.Equal(t, http.StatusOK, rrBookings.Code)
	bodyBookings := rrBookings.Body.String()
	assert.Contains(t, bodyBookings, "Super Cargo Transporters")
	assert.Contains(t, bodyBookings, "/static/logos/supercargo.svg")
	assert.Contains(t, bodyBookings, "BK-WL-100")

	// 2. Test /customer/invoices
	reqInvoices := httptest.NewRequest(http.MethodGet, "/customer/invoices", nil)
	reqInvoices = reqInvoices.WithContext(shared.ContextWithTenantID(reqInvoices.Context(), shared.TenantID(tenantID)))
	reqInvoices = reqInvoices.WithContext(context.WithValue(reqInvoices.Context(), auth.ContextUser, &auth.SessionData{UserID: "u-wl-shipper", Role: "customer"}))

	rrInvoices := httptest.NewRecorder()
	portal.ListMyInvoices(rrInvoices, reqInvoices)
	assert.Equal(t, http.StatusOK, rrInvoices.Code)
	bodyInvoices := rrInvoices.Body.String()
	assert.Contains(t, bodyInvoices, "Super Cargo Transporters")
	assert.Contains(t, bodyInvoices, "/static/logos/supercargo.svg")

	// 3. Test /customer/tracking/{tripId}
	reqTracking := httptest.NewRequest(http.MethodGet, "/customer/tracking/t-wl-1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("trip_id", "t-wl-1")
	reqTracking = reqTracking.WithContext(context.WithValue(reqTracking.Context(), chi.RouteCtxKey, rctx))
	reqTracking = reqTracking.WithContext(shared.ContextWithTenantID(reqTracking.Context(), shared.TenantID(tenantID)))
	reqTracking = reqTracking.WithContext(context.WithValue(reqTracking.Context(), auth.ContextUser, &auth.SessionData{UserID: "u-wl-shipper", Role: "customer"}))

	rrTracking := httptest.NewRecorder()
	portal.Tracking(rrTracking, reqTracking)
	assert.Equal(t, http.StatusOK, rrTracking.Code)
	bodyTracking := rrTracking.Body.String()
	assert.Contains(t, bodyTracking, "Super Cargo Transporters")
	assert.Contains(t, bodyTracking, "/static/logos/supercargo.svg")
	assert.Contains(t, bodyTracking, "TRP-WL-500")
}
