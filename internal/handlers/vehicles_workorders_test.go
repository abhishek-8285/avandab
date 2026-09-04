package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
	"transport-app/internal/shared"
)

// Vehicle page shows the org's open job cards with links, hides done cards
// and other orgs' cards.
func TestSelectedVehicles_ViewOpenJobCards(t *testing.T) {
	db := newVehiclesSelectedDB(t)
	app := newVehiclesSelectedApp(t, db, &mockAuthSvc{})
	future := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	v, err := app.Services.Vehicles.CreateVehicle(shared.ContextWithTenantID(context.Background(), shared.DefaultTenant),
		"MHWO0001", "V-WO", domain.VehicleTypeTruck, 5000, domain.FuelTypeDiesel, future, future, future, "")
	require.NoError(t, err)
	vid := string(v.ID)

	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-W','W','w')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO work_orders (id, tenant_id, vehicle_id, title, status) VALUES
		('wo-v-open','1',?,'Vehicle brake job','in_progress'),
		('wo-v-done','1',?,'Old oil change','done'),
		('wo-v-foreign','tenant-W',?,'Foreign card','open')`, vid, vid, vid)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Route("/vehicles", app.Vehicles.Routes)

	req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/"+vid, nil), "1", "user-1", "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Open Job Cards")
	assert.Contains(t, body, "Vehicle brake job")
	assert.Contains(t, body, "/maintenance/work-orders/wo-v-open")
	assert.Contains(t, body, "New job card")
	assert.NotContains(t, body, "Old oil change", "done cards stay off the vehicle page")
	assert.NotContains(t, body, "Foreign card", "other org cards never leak")

	// Other org sees the vehicle page section empty (or 404 when the vehicle itself is foreign).
	req = withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/"+vid, nil), "tenant-W", "user-1", "admin")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		assert.NotContains(t, w.Body.String(), "Vehicle brake job")
	}
}
