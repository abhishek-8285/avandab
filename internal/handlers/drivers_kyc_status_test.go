package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

// TestDriverStatusTransitionsStampDeactivationForKYC proves the KYC retention
// clock starts when a driver goes inactive and resets on reactivation.
func TestDriverStatusTransitionsStampDeactivationForKYC(t *testing.T) {
	app := newRegisterTestApp(t)
	h := &DriverHandlers{App: app}
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "drv-kyc-clock"})

	_, err := app.DB.ExecContext(ctx, `
		INSERT INTO drivers (id, driver_id, first_name, last_name, phone, status, tenant_id)
		VALUES ('drv-kyc-clock', 'DRVKYC', 'Clock', 'Driver', '9000000002', 'available', '1')`)
	require.NoError(t, err)

	setStatus := func(status string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/drivers/me/status",
			strings.NewReader(`{"status":"`+status+`"}`)).WithContext(ctx)
		w := httptest.NewRecorder()
		h.UpdateMyStatus(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	setStatus("inactive")
	var deactivated sql.NullTime
	require.NoError(t, app.DB.QueryRow(`SELECT deactivated_at FROM drivers WHERE id = 'drv-kyc-clock'`).Scan(&deactivated))
	assert.True(t, deactivated.Valid, "inactive must stamp deactivated_at")

	setStatus("available")
	require.NoError(t, app.DB.QueryRow(`SELECT deactivated_at FROM drivers WHERE id = 'drv-kyc-clock'`).Scan(&deactivated))
	assert.False(t, deactivated.Valid, "reactivation must clear deactivated_at")
}
