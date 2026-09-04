package handlers

import (
	"context"
	"encoding/json"
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

// TestSelectedDashboard_AttentionStrip seeds one row per backlog and proves
// GetDashboardData counts each exactly once plus the exception strip renders
// with honest drill-down links (and no agent card without an approval svc).
func TestSelectedDashboard_AttentionStrip(t *testing.T) {
	db := newDashboardSelectedDB(t)
	_, err := db.Exec(`INSERT OR REPLACE INTO company_settings (id, company_name, currency, timezone, address, phone, email) VALUES (1, 'TestCo', 'INR', 'Asia/Kolkata', '123 Logistics St', '+91 9876543210', 'ops@test.co')`)
	require.NoError(t, err)
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := db.Exec(q, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO customers (id, name, phone, tenant_id) VALUES ('cust-att', 'Att Customer', '9000000002', '1')`)
	exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES ('route-att', 'Delhi', 'Jaipur', 280, 6, 5000, '1')`)
	exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, passengers, price, status, tenant_id)
		VALUES ('bk-att', 'BK-ATT', 'cust-att', '2026-08-10T08:00:00Z', 'route-att', 'truck', 1, 1500, 'pending', '1')`)
	exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status, maintenance_due, tenant_id)
		VALUES ('v-att', 'DL-ATT-1', 'V-ATT-1', 'truck', 5000, '2027-01-01', '2027-01-01', '2027-01-01', 'maintenance', '2026-08-01', '1')`)
	exec(`INSERT INTO work_orders (id, tenant_id, vehicle_id, title, status) VALUES ('wo-att', '1', 'v-att', 'Att brake job', 'open')`)
	exec(`INSERT INTO alerts (id, source, alert_type, status, dedup_key, tenant_id, title, message) VALUES ('al-att', 'test', 'test_type', 'open', 'dedup-att', '1', 'Att alert', 'attention')`)
	exec(`INSERT INTO dtc_events (id, vehicle_id, dtc_code, severity, occurred_at) VALUES ('d-att', 'v-att', 'P0300', 'critical', '2026-08-10T08:00:00Z')`)
	exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id, version)
		VALUES ('tr-att', 'TR-ATT', 'route-att', '2026-08-10T08:00:00Z', 'completed', '1', 1)`)
	soon := time.Now().Add(2 * time.Hour).Format("2006-01-02 15:04:05")
	exec(`INSERT INTO eway_bills (id, trip_id, ewb_number, generation_date, valid_until, status) VALUES ('e-att', 'tr-att', 'EWB-ATT', '2026-08-10 08:00:00', ?, 'active')`, soon)
	exec(`INSERT INTO driver_expenses (id, expense_type, amount, tenant_id) VALUES ('k-att', 'fuel', 500, '1')`)
	exec(`INSERT INTO fastag_tags (id, tenant_id, tag_id, balance, status) VALUES ('f-att', '1', 'TAG-ATT', 100, 'ACTIVE')`)

	cfg := &config.Config{
		AppEnv:       "testing",
		CookieSecret: "test-secret-32",
		Experiment:   config.ExperimentConfig{Rollout: 100, ForceVariant: "B"},
	}
	app := newDashboardSelectedApp(t, db, cfg, &mockAuthSvc{})

	data, err := app.Services.Dashboard.GetDashboardData(shared.ContextWithTenantID(context.Background(), "1"))
	require.NoError(t, err)
	assert.EqualValues(t, 1, data.Attention.UnassignedBookings, "BK-ATT has no trip")
	assert.EqualValues(t, 1, data.Attention.MaintenanceDue)
	assert.EqualValues(t, 1, data.Attention.OpenWorkOrders)
	assert.EqualValues(t, 1, data.Attention.GarageVehicles)
	assert.EqualValues(t, 1, data.Attention.OpenAlerts)
	assert.EqualValues(t, 1, data.Attention.ActiveDTCs)
	assert.EqualValues(t, 1, data.Attention.ExpiringEwaybills)
	assert.EqualValues(t, 1, data.Attention.PendingKharcha)
	assert.EqualValues(t, 1, data.Attention.LowFastag)
	assert.EqualValues(t, 9, data.Attention.Total())

	req := httptest.NewRequest(http.MethodGet, "/dashboard?variant=B", nil)
	ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID("1"))
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "user-1", Role: "admin"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	app.Dashboard.Index(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Needs attention")
	assert.Contains(t, body, "/bookings?status=unassigned")
	assert.Contains(t, body, "/alerts?status=open")
	assert.Contains(t, body, "/kharcha/pending")
	assert.Contains(t, body, "/vehicles?status=maintenance")
	assert.Contains(t, body, "/maintenance/dtc")
	assert.NotContains(t, body, "/agent-actions", "no approval service wired in test app")
}

// TestSelectedDashboard_TablesFragment proves GET /dashboard/tables serves
// the same row partials as the page render, keyed by container id, with
// badge counts — the live board swaps these without a reload.
func TestSelectedDashboard_TablesFragment(t *testing.T) {
	db := newDashboardSelectedDB(t)
	_, err := db.Exec(`INSERT OR REPLACE INTO company_settings (id, company_name, currency, timezone, address, phone, email) VALUES (1, 'TestCo', 'INR', 'Asia/Kolkata', '123 Logistics St', '+91 9876543210', 'ops@test.co')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES ('route-tbl', 'Delhi', 'Jaipur', 280, 6, 5000, '1')`)
	require.NoError(t, err)
	today := time.Now().Format("2006-01-02")
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id, version)
		VALUES ('tr-tbl', 'TR-TBL-1', 'route-tbl', ?, 'assigned', '1', 1)`, today+" 06:00:00")
	require.NoError(t, err)

	cfg := &config.Config{
		AppEnv:       "testing",
		CookieSecret: "test-secret-32",
		Experiment:   config.ExperimentConfig{Rollout: 100, ForceVariant: "B"},
	}
	app := newDashboardSelectedApp(t, db, cfg, &mockAuthSvc{})

	req := httptest.NewRequest(http.MethodGet, "/dashboard/tables", nil)
	ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID("1"))
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "user-1", Role: "admin"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	app.Dashboard.Tables(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var payload struct {
		Regions map[string]string      `json:"regions"`
		Badges  map[string]interface{} `json:"badges"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	for _, id := range []string{"upcoming-tbody", "upcoming-mobile", "tbl-bookings", "tbl-payments", "feed-activity", "feed-overdue", "feed-idle", "feed-pending"} {
		assert.Contains(t, payload.Regions, id, "fragment for "+id)
	}
	assert.Contains(t, payload.Regions["upcoming-tbody"], "TR-TBL-1")
	assert.Contains(t, payload.Regions["upcoming-mobile"], "TR-TBL-1")
	assert.EqualValues(t, 1, payload.Badges["badge-upcoming"])
}

// TestSelectedDashboard_FastagThresholdOverlay proves the low-FASTag count
// honors the per-tenant company_config override with safe fallbacks. Each
// case uses its own tenant: GetDashboardData caches per tenant (3s TTL),
// so fresh tenants keep the test fast and deterministic.
func TestSelectedDashboard_FastagThresholdOverlay(t *testing.T) {
	db := newDashboardSelectedDB(t)
	cfg := &config.Config{
		AppEnv:       "testing",
		CookieSecret: "test-secret-32",
		Experiment:   config.ExperimentConfig{Rollout: 100, ForceVariant: "B"},
	}
	app := newDashboardSelectedApp(t, db, cfg, &mockAuthSvc{})
	require.NotNil(t, app.Services.TenantConfigs, "overlay reader must be wired")

	setup := func(tenant, configValue string) {
		t.Helper()
		_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES (?, ?, ?)`, tenant, "Tenant "+tenant, "t"+tenant)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO fastag_tags (id, tenant_id, tag_id, balance, status) VALUES (?, ?, ?, 400, 'ACTIVE')`,
			"f-thr-"+tenant, tenant, "TAG-THR-"+tenant)
		require.NoError(t, err)
		if configValue != "" {
			_, err = db.Exec(`INSERT INTO company_config (tenant_id, key, value) VALUES (?, 'fastag.low_balance_threshold', ?)`,
				tenant, configValue)
			require.NoError(t, err)
		}
	}
	attention := func(tenant string) int64 {
		t.Helper()
		data, err := app.Services.Dashboard.GetDashboardData(shared.ContextWithTenantID(context.Background(), shared.TenantID(tenant)))
		require.NoError(t, err)
		return data.Attention.LowFastag
	}

	setup("21", "")
	assert.EqualValues(t, 1, attention("21"), "default 500 flags balance 400")

	setup("22", "300")
	assert.EqualValues(t, 0, attention("22"), "override 300 clears balance 400")

	setup("23", "abc")
	assert.EqualValues(t, 1, attention("23"), "unparseable falls back to default")

	setup("24", "-5")
	assert.EqualValues(t, 1, attention("24"), "non-positive falls back to default")
}

// TestMaintenanceDTC_RendersWithoutFullIndexKeys proves /maintenance/dtc
// renders even though it shares maintenance_index.html with the full index
// page (missing DueVehicles/Schedules keys 500'd it via len-on-nil).
func TestMaintenanceDTC_RendersWithoutFullIndexKeys(t *testing.T) {
	db := newMaintHandlerTestDB(t)
	app := newMaintHandlerApp(t, db, maintAllowAuthSvc{})
	insertMaintTestVehicle(t, db, "veh-dtc", "REG-DTC")
	_, err := db.Exec(`INSERT INTO dtc_events (id, vehicle_id, dtc_code, severity, occurred_at) VALUES ('d-dtc', 'veh-dtc', 'P0300', 'critical', '2026-08-10T08:00:00Z')`)
	require.NoError(t, err)

	w := woWebRequest(t, app.Maintenance.ListDTC, "GET", "/maintenance/dtc?vehicle_id=veh-dtc", "1", "")
	require.Equal(t, http.StatusOK, w.Code, "DTC page must render with partial keys")
	assert.Contains(t, w.Body.String(), "P0300")
}
