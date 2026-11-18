package application_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/driver/application"
)

// Realistic trips schema for the driver command API: it keeps the production
// CHECK constraint. The older services_test fixture omits it, which is why the
// en_route_pickup/arrived_pickup writes looked green while failing in SQLite.
const tripStatusSchema = `
CREATE TABLE tenants (id TEXT PRIMARY KEY, name TEXT NOT NULL, slug TEXT NOT NULL);
CREATE TABLE drivers (
    id TEXT PRIMARY KEY,
    driver_id TEXT NOT NULL UNIQUE,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    phone TEXT NOT NULL,
    email TEXT,
    license_number TEXT,
    license_expiry DATE,
    status TEXT NOT NULL DEFAULT 'available',
    notes TEXT,
    tenant_id TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE driver_commands (
    command_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    driver_id TEXT NOT NULL,
    command_type TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('processed', 'failed')),
    response_payload TEXT,
    executed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE trips (
    id TEXT PRIMARY KEY,
    trip_number TEXT NOT NULL UNIQUE,
    booking_id TEXT,
    driver_id TEXT,
    vehicle_id TEXT,
    route_id TEXT NOT NULL,
    departure_time DATETIME NOT NULL,
    arrival_time DATETIME,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'scheduled', 'assigned', 'started', 'reached_pickup', 'in_transit', 'delivered', 'completed', 'cancelled')),
    remarks TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    tenant_id TEXT NOT NULL DEFAULT '1',
    version INTEGER NOT NULL DEFAULT 1,
    started_at DATETIME,
    reached_pickup_at DATETIME,
    in_transit_at DATETIME,
    delivered_at DATETIME,
    completed_at DATETIME
);
`

func setupTripStatusDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(tripStatusSchema)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-1', 'Fleet One', 'fleet-one')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, tenant_id)
		VALUES ('drv-1', 'DRV-1', 'Ravi', 'Kumar', '9876543210', 'tenant-1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, tenant_id, driver_id, route_id, departure_time, status)
		VALUES ('trip-check', 'TRIP-CHECK-1', 'tenant-1', 'drv-1', 'route-1', datetime('now'), 'assigned')`)
	require.NoError(t, err)
	return db
}

// The driver command lifecycle must write statuses the database accepts, and
// must never reopen a terminal trip.
func TestDriverCommands_WriteCanonicalTripStatuses(t *testing.T) {
	db := setupTripStatusDB(t)
	defer db.Close()

	svc := application.NewDriverAppService(db)
	ctx := context.Background()

	steps := []struct {
		cmd       string
		wantState string
	}{
		{"START_TRIP", "started"},
		{"ARRIVE_PICKUP", "reached_pickup"},
		{"START_LOADING", "reached_pickup"},
		{"START_DELIVERY", "in_transit"},
		{"COMPLETE_TRIP", "completed"},
	}

	for _, step := range steps {
		resp, err := svc.ProcessDriverCommand(ctx, "tenant-1", "drv-1", application.DriverCommandRequest{
			CommandID: "cmd-" + step.cmd,
			Type:      step.cmd,
			Payload:   map[string]interface{}{"trip_id": "trip-check"},
		})
		require.NoError(t, err, "command %s must succeed", step.cmd)
		assert.Equal(t, step.wantState, resp.TripState, "command %s state", step.cmd)

		var stored string
		require.NoError(t, db.QueryRow(`SELECT status FROM trips WHERE id = 'trip-check'`).Scan(&stored))
		assert.Equal(t, step.wantState, stored, "command %s must persist a valid status", step.cmd)
	}

	// A completed trip must not be cancellable or restartable.
	_, err := svc.ProcessDriverCommand(ctx, "tenant-1", "drv-1", application.DriverCommandRequest{
		CommandID: "cmd-cancel-terminal",
		Type:      "CANCEL_TRIP",
		Payload:   map[string]interface{}{"trip_id": "trip-check"},
	})
	assert.Error(t, err, "terminal trip must not be reopened")
}
