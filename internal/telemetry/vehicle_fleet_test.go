package telemetry

// Characterization + extraction tests for the EV-dashboard fleet queries moved
// out of internal/handlers/telemetry_devices.go (handlers-end-state ADR,
// Phase 2): tenant-scoped vehicle dropdown ordering and the best-effort
// electric fuel-type transition.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const vehicleFleetTestSchema = `
CREATE TABLE vehicles (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    vehicle_number TEXT NOT NULL,
    registration_number TEXT,
    fuel_type TEXT NOT NULL DEFAULT 'diesel'
);
`

func newVehicleFleetTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("ev_fleet_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(vehicleFleetTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExecFleet(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func TestFleetVehicles_TenantScopedOrdering(t *testing.T) {
	db := newVehicleFleetTestDB(t)
	mustExecFleet(t, db, `INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, fuel_type) VALUES
		('v-b', 'tenant-1', 'VN-B', 'MH12B', 'diesel'),
		('v-a', 'tenant-1', 'VN-A', 'MH12A', 'electric'),
		('v-t2', 'tenant-2', 'VN-0', 'KA01', 'diesel')`)

	vehicles := FleetVehicles(context.Background(), db, "tenant-1")
	require.Len(t, vehicles, 2)
	assert.Equal(t, "v-a", vehicles[0].ID, "ordered by vehicle_number ASC")
	assert.Equal(t, "VN-A", vehicles[0].VehicleNumber)
	assert.Equal(t, "MH12A", vehicles[0].RegistrationNumber)
	assert.Equal(t, "electric", vehicles[0].FuelType)
	assert.Equal(t, "v-b", vehicles[1].ID)

	assert.Nil(t, FleetVehicles(context.Background(), db, "tenant-unknown"))
	assert.Nil(t, FleetVehicles(context.Background(), nil, "tenant-1"))

	// Best-effort: a dead table renders an empty dropdown.
	mustExecFleet(t, db, `DROP TABLE vehicles`)
	assert.Nil(t, FleetVehicles(context.Background(), db, "tenant-1"))
}

func TestMarkVehicleElectric_TenantScoped(t *testing.T) {
	db := newVehicleFleetTestDB(t)
	ctx := context.Background()
	mustExecFleet(t, db, `INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, fuel_type) VALUES
		('v-1', 'tenant-1', 'VN-1', 'MH12A', 'diesel'),
		('v-2', 'tenant-2', 'VN-2', 'KA01', 'diesel')`)

	require.NoError(t, MarkVehicleElectric(ctx, db, "tenant-1", "v-1"))
	readFuel := func(id string) string {
		t.Helper()
		var fuel string
		require.NoError(t, db.QueryRow(`SELECT fuel_type FROM vehicles WHERE id = ?`, id).Scan(&fuel))
		return fuel
	}
	assert.Equal(t, "electric", readFuel("v-1"))
	assert.Equal(t, "diesel", readFuel("v-2"), "other tenants are untouched")

	// Tenant scoping: a tenant-1 call cannot flip a tenant-2 vehicle id.
	require.NoError(t, MarkVehicleElectric(ctx, db, "tenant-1", "v-2"))
	assert.Equal(t, "diesel", readFuel("v-2"))

	// Unknown vehicle + nil DB are no-ops (best-effort contract).
	require.NoError(t, MarkVehicleElectric(ctx, db, "tenant-1", "v-missing"))
	require.NoError(t, MarkVehicleElectric(ctx, nil, "tenant-1", "v-1"))
}
