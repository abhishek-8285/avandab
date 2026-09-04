package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
)

// Mutating device-registry calls without a resolved tenant must be blocked
// instead of writing into tenant "1".
func TestTelemetryDevices_BulkRegister_NoTenantBlocked(t *testing.T) {
	h := &TelemetryDeviceHandlers{}
	body := strings.NewReader(`[{"imei":"123456789012345"}]`)
	req := httptest.NewRequest(http.MethodPost, "/telemetry/devices/bulk", body)
	ctx := context.WithValue(context.Background(), auth.ContextUser, &auth.SessionData{UserID: "admin-1", Role: "admin"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.BulkRegister(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTelemetryDevices_Assign_NoTenantBlocked(t *testing.T) {
	h := &TelemetryDeviceHandlers{}
	form := strings.NewReader("vehicle_id=veh-1")
	req := httptest.NewRequest(http.MethodPost, "/telemetry/devices/359876543210987/assign", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("imei", "359876543210987")
	ctx := context.WithValue(context.Background(), auth.ContextUser, &auth.SessionData{UserID: "admin-1", Role: "admin"})
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.Assign(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// Creating a user without a resolved tenant must fail instead of dropping
// the new user into tenant "1".
func TestUsers_Create_NoTenantBlocked(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, &mockAuthSvc{}, false)
	h := &UserHandlers{App: app}

	form := "email=notenant@example.com&name=NoTenant&phone=9999999999&password=Test1234&role_id=4&status=active"
	req := httptest.NewRequest(http.MethodPost, "/users/new", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Admin session but deliberately NO tenant in context.
	ctx := context.WithValue(context.Background(), auth.ContextUser, &auth.SessionData{UserID: "admin-1", Role: "admin"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM users WHERE email = 'notenant@example.com'`).Scan(&count))
	assert.Equal(t, 0, count)
}
