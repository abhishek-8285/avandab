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
	"transport-app/internal/shared"
)

// TestDriverUpdateMyStatus_TenantScoped: the driver lookup and UPDATE are keyed
// only by user id today, so a user mapped to a driver row in another org can
// change that org's driver status.
func TestDriverUpdateMyStatus_TenantScoped(t *testing.T) {
	dbConn, app, _ := setupMobileAPITestEnv(t)
	defer dbConn.Close()

	_, err := dbConn.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-self-x', 'Self X', 'tenant-self-x')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status)
		VALUES ('u-self-x', 'selfx@example.com', 'hash', 'Self X', 5, 'active')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, tenant_id)
		VALUES ('d-self-x', 'DRV-SELF-X', 'Self', 'X', '+919900000099', 'selfx@example.com', 'DL-SELF-X', '2030-01-01', 'available', 'tenant-self-x')`)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
			ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "u-self-x"})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Post("/api/v1/drivers/me/status", app.Drivers.UpdateMyStatus)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/drivers/me/status", strings.NewReader(`{"status":"leave"}`)))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	var status string
	require.NoError(t, dbConn.QueryRow(`SELECT status FROM drivers WHERE id = 'd-self-x'`).Scan(&status))
	assert.Equal(t, "available", status)
}
