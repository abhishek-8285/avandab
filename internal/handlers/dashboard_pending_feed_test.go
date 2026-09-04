package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/shared"
)

// TestSelectedDashboard_PendingInvoicesFeed proves the Pending Payments card
// lists actually-unpaid invoices (pending + partially_paid), links each row
// to its invoice, drills down to the combined filter, and exposes no dead
// invoice-creation buttons.
func TestSelectedDashboard_PendingInvoicesFeed(t *testing.T) {
	db := newDashboardSelectedDB(t)
	_, err := db.Exec(`INSERT OR REPLACE INTO company_settings (id, company_name, currency, timezone, address, phone, email) VALUES (1, 'TestCo', 'INR', 'Asia/Kolkata', '123 Logistics St', '+91 9876543210', 'ops@test.co')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers (id, name, phone, tenant_id) VALUES ('cust-dash', 'Dash Customer', '9000000001', '1')`)
	require.NoError(t, err)
	mkInvoice := func(id, num, status string) {
		_, err := db.Exec(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, total, payment_status, tenant_id, created_at, updated_at)
			VALUES (?, ?, 'bk-dash', 'cust-dash', 100.0, 118.0, ?, '1', '2026-08-10T08:00:00Z', '2026-08-10T08:00:00Z')`,
			id, num, status)
		require.NoError(t, err)
	}
	mkInvoice("inv-dash-pend", "INV-DASH-PEND", "pending")
	mkInvoice("inv-dash-part", "INV-DASH-PART", "partially_paid")
	mkInvoice("inv-dash-paid", "INV-DASH-PAID", "paid")

	cfg := &config.Config{
		AppEnv:       "testing",
		CookieSecret: "test-secret-32",
		Experiment:   config.ExperimentConfig{Rollout: 100, ForceVariant: "B"},
	}
	app := newDashboardSelectedApp(t, db, cfg, &mockAuthSvc{})

	req := httptest.NewRequest(http.MethodGet, "/dashboard?variant=B", nil)
	ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID("1"))
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "user-1", Role: "admin"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	app.Dashboard.Index(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()

	// Feed shows unpaid invoices with per-row links...
	assert.Contains(t, body, "INV-DASH-PEND")
	assert.Contains(t, body, "INV-DASH-PART")
	assert.Contains(t, body, "/invoices/inv-dash-pend")
	assert.Contains(t, body, "partially_paid")
	// ...and never the paid one (no payments seeded, so any occurrence is the feed's fault).
	assert.NotContains(t, body, "INV-DASH-PAID")
	// Drill-down covers the same set the KPI counts.
	assert.Contains(t, body, "/invoices?status=open")
	// Dead invoice-creation route must not be linked anywhere.
	assert.NotContains(t, body, "/invoices/new")
	// Zero cancelled trips → Clear badge + honest sub-line, no static 0.0%.
	assert.Contains(t, body, "Clear")
	assert.NotContains(t, body, "0.0% of today")
	assert.NotContains(t, body, "Needs review")
}

// TestSelectedDashboard_UpcomingTripRows proves the Upcoming Trips table
// renders populated rows: driver/vehicle joins are nullable (*string) and the
// initials avatar slices them, which 500'd the whole page before nullString
// wrapping (latent until the date-binding fix let rows through).
func TestSelectedDashboard_UpcomingTripRows(t *testing.T) {
	db := newDashboardSelectedDB(t)
	_, err := db.Exec(`INSERT OR REPLACE INTO company_settings (id, company_name, currency, timezone, address, phone, email) VALUES (1, 'TestCo', 'INR', 'Asia/Kolkata', '123 Logistics St', '+91 9876543210', 'ops@test.co')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES ('route-dash', 'Delhi', 'Jaipur', 280, 6, 5000, '1')`)
	require.NoError(t, err)
	today := time.Now().Format("2006-01-02")
	// Unassigned trip: NULL driver/vehicle exercises the nil-pointer path.
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id, version)
		VALUES ('tr-dash', 'TR-DASH-1', 'route-dash', ?, 'assigned', '1', 1)`, today+" 06:00:00")
	require.NoError(t, err)

	cfg := &config.Config{
		AppEnv:       "testing",
		CookieSecret: "test-secret-32",
		Experiment:   config.ExperimentConfig{Rollout: 100, ForceVariant: "B"},
	}
	app := newDashboardSelectedApp(t, db, cfg, &mockAuthSvc{})

	for _, variant := range []string{"B", "A"} {
		req := httptest.NewRequest(http.MethodGet, "/dashboard?variant="+variant, nil)
		ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID("1"))
		ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "user-1", Role: "admin"})
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		app.Dashboard.Index(w, req)
		require.Equal(t, http.StatusOK, w.Code, "variant %s must render with upcoming rows", variant)
		assert.Contains(t, w.Body.String(), "TR-DASH-1", "variant %s shows today's trip", variant)
	}
}
