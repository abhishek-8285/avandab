package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/events"
	"transport-app/internal/handlers"
	"transport-app/internal/shared"
)

// TestDriverSOS_RejectsForeignTripAndVehicle: client-supplied IDs are copied
// verbatim into the alert payload today, so any authenticated caller can
// attribute an emergency to another tenant's vehicle.
func TestDriverSOS_RejectsForeignTripAndVehicle(t *testing.T) {
	db, app, _, _, _ := setupSOSTestEnvironment(t)
	defer db.Close()
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")

	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-2-sos', 'Other SOS', 'other-sos-tenant')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, tenant_id, first_name, last_name, phone, license_number, license_expiry, pan, status)
		VALUES ('drv-sos-a', 'drv-sos-a', 'tenant-1', 'Own', 'Driver', '9811122234', 'DL-SOS-A', '2029-01-01', 'ABCDE9998F', 'available')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, vehicle_number, tenant_id, registration_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status)
		VALUES ('veh-sos-a', 'DL01SOS09', 'tenant-1', 'DL01SOS09', 'truck', 10000, '2028-01-01', '2028-01-01', '2028-01-01', 'running')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, vehicle_number, tenant_id, registration_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status)
		VALUES ('veh-sos-b', 'DL01SOS99', 'tenant-2-sos', 'DL01SOS99', 'truck', 10000, '2028-01-01', '2028-01-01', '2028-01-01', 'running')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, tenant_id, driver_id, vehicle_id, route_id, departure_time, status)
		VALUES ('trip-sos-b', 'TRIP-SOS-B', 'tenant-2-sos', 'drv-sos-b', 'veh-sos-b', 'route-1', datetime('now'), 'in_transit')`)
	require.NoError(t, err)

	body, err := json.Marshal(handlers.SOSRequest{
		TripID: "trip-sos-b", VehicleID: "veh-sos-b",
		Latitude: 28.5, Longitude: 77.3, TriggeredAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sos", bytes.NewReader(body))
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Driver-ID", "drv-sos-a")
	rec := httptest.NewRecorder()
	app.SOS.TriggerSOS(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code, "foreign trip/vehicle must be rejected")
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE event_type = ?`, events.SOSEvent).Scan(&n))
	assert.Zero(t, n, "no SOS event may be emitted for foreign IDs")
}
