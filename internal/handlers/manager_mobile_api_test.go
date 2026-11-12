package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

func TestManagerMobileAPI_DashboardSummary(t *testing.T) {
	db := newDashboardSelectedDB(t)
	app := newDashboardSelectedApp(t, db, nil, nil)

	req := httptest.NewRequest("GET", "/api/v1/manager/dashboard", nil)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("1"))
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	app.Dashboard.SummaryAPI(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Contains(t, resp, "todays_trips_count")
	assert.Contains(t, resp, "active_trips_count")
	assert.Contains(t, resp, "available_vehicles_count")
	assert.Contains(t, resp, "attention")
}

func TestManagerMobileAPI_KharchaApprovalFlow(t *testing.T) {
	db := newDashboardSelectedDB(t)
	app := newDashboardSelectedApp(t, db, nil, nil)
	app.Kharcha = &KharchaHandlers{App: app}

	// Seed test customer, route, trip, driver, and pending expenses
	_, err := db.Exec(`INSERT INTO customers (id, name, phone, tenant_id) VALUES ('cust-m', 'Manager Cust', '9111111111', '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES ('route-m', 'Mumbai', 'Pune', 150, 4, 3000, '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, status, tenant_id) VALUES ('drv-m', 'DRV-M', 'Driver', 'M', '9222222222', 'available', '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, route_id, driver_id, departure_time, status, tenant_id, version)
		VALUES ('trip-m', 'TRIP-M-1', 'route-m', 'drv-m', '2026-08-10 08:00:00', 'in_transit', '1', 1)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO driver_expenses (id, trip_id, driver_id, category, expense_type, amount, status, tenant_id)
		VALUES ('exp-m-1', 'trip-m', 'drv-m', 'fuel', 'fuel', 1200.50, 'pending', '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO driver_expenses (id, trip_id, driver_id, category, expense_type, amount, status, tenant_id)
		VALUES ('exp-m-2', 'trip-m', 'drv-m', 'toll', 'toll', 350.00, 'pending', '1')`)
	require.NoError(t, err)

	// 1. PendingAPI: GET /api/v1/kharcha/pending
	{
		req := httptest.NewRequest("GET", "/api/v1/kharcha/pending", nil)
		ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("1"))
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		app.Kharcha.PendingAPI(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var pending []map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &pending)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(pending), 2)
	}

	// 2. ApproveAPI: POST /api/v1/kharcha/exp-m-1/approve
	{
		req := httptest.NewRequest("POST", "/api/v1/kharcha/exp-m-1/approve", nil)
		ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("1"))
		ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "mgr-user-1", Role: "manager"})

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "exp-m-1")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		app.Kharcha.ApproveAPI(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "approved", resp["status"])
		assert.Equal(t, "exp-m-1", resp["expense_id"])

		// Verify database state
		var status string
		err = db.QueryRow(`SELECT status FROM driver_expenses WHERE id = 'exp-m-1'`).Scan(&status)
		require.NoError(t, err)
		assert.Equal(t, "approved", status)
	}

	// 3. RejectAPI: POST /api/v1/kharcha/exp-m-2/reject
	{
		bodyJSON := []byte(`{"reason":"Missing toll receipt photo"}`)
		req := httptest.NewRequest("POST", "/api/v1/kharcha/exp-m-2/reject", bytes.NewReader(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("1"))
		ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "mgr-user-1", Role: "manager"})

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "exp-m-2")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		app.Kharcha.RejectAPI(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "rejected", resp["status"])
		assert.Equal(t, "exp-m-2", resp["expense_id"])
		assert.Equal(t, "Missing toll receipt photo", resp["reason"])

		// Verify database state
		var status string
		err = db.QueryRow(`SELECT status FROM driver_expenses WHERE id = 'exp-m-2'`).Scan(&status)
		require.NoError(t, err)
		assert.Equal(t, "rejected", status)
	}
}
