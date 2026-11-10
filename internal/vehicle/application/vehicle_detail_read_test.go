package application

// Focused tests for the vehicle view-page read model extracted from
// internal/handlers/vehicles.go (handlers-end-state ADR, Phase 2): tenant
// isolation of every section, telemetry frame binding, best-effort empty
// sections and the legacy map key truthiness the template relies on.

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const vehicleDetailTestSchema = `
CREATE TABLE vehicles (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    registration_number TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'available',
    blocked_reason TEXT,
    maintenance_due DATE,
    maintenance_override_by TEXT,
    maintenance_override_at DATETIME,
    maintenance_override_reason TEXT
);
CREATE TABLE vehicle_latest_position (
    vehicle_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    speed REAL NOT NULL,
    device_time DATETIME
);
CREATE TABLE trips (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    vehicle_id TEXT NOT NULL,
    trip_number TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at DATETIME
);
CREATE TABLE work_orders (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    vehicle_id TEXT NOT NULL,
    title TEXT NOT NULL,
    status TEXT NOT NULL,
    assignee TEXT,
    created_at DATETIME
);
CREATE TABLE drivers (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    phone TEXT,
    driver_id TEXT
);
CREATE TABLE driver_preferred_vehicles (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    driver_id TEXT NOT NULL,
    vehicle_id TEXT NOT NULL,
    is_primary INTEGER NOT NULL DEFAULT 1,
    unassigned_at DATETIME
);
CREATE TABLE vehicle_measuring_points (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    vehicle_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    meas_position TEXT NOT NULL DEFAULT 'DISTANCE',
    unit TEXT NOT NULL DEFAULT 'KM',
    description TEXT NOT NULL DEFAULT '',
    annual_estimate REAL,
    created_at DATETIME
);
CREATE TABLE vehicle_measurements (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    point_id TEXT NOT NULL,
    counter_reading REAL NOT NULL,
    difference_reading REAL,
    measured_at DATETIME,
    read_by TEXT,
    recorded_at DATETIME
);
CREATE TABLE vehicle_commands (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    vehicle_id TEXT NOT NULL,
    command_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    issued_by TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

func openVehicleDetailTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", "-", "_").Replace(t.Name())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(vehicleDetailTestSchema)
	require.NoError(t, err)
	return db
}

func mustExecVehicleDetail(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func TestVehicleDetailQueries_TenantIsolation(t *testing.T) {
	db := openVehicleDetailTestDB(t)
	q := NewVehicleDetailQueries(db)
	ctx := context.Background()

	mustExecVehicleDetail(t, db, `INSERT INTO vehicles (id, tenant_id, registration_number, status, maintenance_due, maintenance_override_by, maintenance_override_at, maintenance_override_reason) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"v-a", "tenant-a", "MH01AB1234", "available", "2026-12-01", "sup-a", "2026-11-01T09:00:00Z", "due for service")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicles (id, tenant_id, registration_number, status) VALUES (?, ?, ?, ?)`,
		"v-b", "tenant-b", "MH02CD5678", "available")

	// The same vehicle id exists under two tenants: each tenant sees its own
	// telemetry frame and never the other tenant frame.
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_latest_position (vehicle_id, tenant_id, latitude, longitude, speed, device_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"v-a", "tenant-a", 19.076, 72.877, 45.5, "2026-11-10T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_latest_position (vehicle_id, tenant_id, latitude, longitude, speed, device_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"v-a", "tenant-b", 1.1, 2.2, 3.3, "2026-11-10T11:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_latest_position (vehicle_id, tenant_id, latitude, longitude, speed, device_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"v-b", "tenant-b", 3.3, 4.4, 5.5, "2026-11-10T12:00:00Z")

	mustExecVehicleDetail(t, db, `INSERT INTO trips (id, tenant_id, vehicle_id, trip_number, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"t-a1", "tenant-a", "v-a", "TR-A1", "completed", "2026-11-01T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO trips (id, tenant_id, vehicle_id, trip_number, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"t-a2", "tenant-a", "v-a", "TR-A2", "scheduled", "2026-11-02T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO trips (id, tenant_id, vehicle_id, trip_number, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"t-b1", "tenant-b", "v-a", "TR-B1", "running", "2026-11-03T10:00:00Z")

	mustExecVehicleDetail(t, db, `INSERT INTO work_orders (id, tenant_id, vehicle_id, title, status, assignee, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"wo-a1", "tenant-a", "v-a", "Brake job", "open", "tech-1", "2026-11-01T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO work_orders (id, tenant_id, vehicle_id, title, status, assignee, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"wo-a2", "tenant-a", "v-a", "Old oil change", "done", "tech-1", "2026-11-02T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO work_orders (id, tenant_id, vehicle_id, title, status, assignee, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"wo-b1", "tenant-b", "v-a", "Foreign card", "open", "tech-2", "2026-11-03T10:00:00Z")

	mustExecVehicleDetail(t, db, `INSERT INTO drivers (id, tenant_id, first_name, last_name, phone, driver_id) VALUES (?, ?, ?, ?, ?, ?)`,
		"d-a", "tenant-a", "Asha", "Rao", "9000000001", "DRV-A")
	mustExecVehicleDetail(t, db, `INSERT INTO drivers (id, tenant_id, first_name, last_name, phone, driver_id) VALUES (?, ?, ?, ?, ?, ?)`,
		"d-b", "tenant-b", "Bilal", "Khan", "9000000002", "DRV-B")
	// One active and one closed assignment for the same vehicle: the closed
	// row must never surface (unassigned_at IS NULL filter).
	mustExecVehicleDetail(t, db, `INSERT INTO driver_preferred_vehicles (id, tenant_id, driver_id, vehicle_id, is_primary, unassigned_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"a-1", "tenant-a", "d-a", "v-a", 1, nil)
	mustExecVehicleDetail(t, db, `INSERT INTO driver_preferred_vehicles (id, tenant_id, driver_id, vehicle_id, is_primary, unassigned_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"a-old", "tenant-a", "d-a", "v-a", 0, "2026-01-01T00:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO driver_preferred_vehicles (id, tenant_id, driver_id, vehicle_id, is_primary, unassigned_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"b-1", "tenant-b", "d-b", "v-a", 1, nil)

	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_measuring_points (id, tenant_id, vehicle_id, kind, meas_position, unit, description, annual_estimate, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"p-a", "tenant-a", "v-a", "ODO", "DISTANCE", "KM", "ODO point", 50000.0, "2026-11-01T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_measuring_points (id, tenant_id, vehicle_id, kind, meas_position, unit, description, annual_estimate, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"p-a2", "tenant-a", "v-a", "FUEL_TOPUP", "FUELTOPUP", "L", "", nil, "2026-11-02T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_measuring_points (id, tenant_id, vehicle_id, kind, meas_position, unit, description, annual_estimate, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"p-b", "tenant-b", "v-a", "ODO", "DISTANCE", "KM", "foreign point", 100.0, "2026-11-03T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_measurements (id, tenant_id, point_id, counter_reading, difference_reading, measured_at, read_by, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"m-a1", "tenant-a", "p-a", 1200.0, nil, nil, nil, "2026-11-01T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_measurements (id, tenant_id, point_id, counter_reading, difference_reading, measured_at, read_by, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"m-a2", "tenant-a", "p-a", 1300.0, 100.0, "2026-11-02T10:00:00Z", "TCS795488", "2026-11-02T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_measurements (id, tenant_id, point_id, counter_reading, difference_reading, measured_at, read_by, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"m-b", "tenant-b", "p-b", 50.0, nil, nil, nil, "2026-11-03T10:00:00Z")

	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_commands (id, tenant_id, vehicle_id, command_type, status, issued_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"c-a1", "tenant-a", "v-a", "E_STOP", "acknowledged", "op-a", "2026-11-01T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_commands (id, tenant_id, vehicle_id, command_type, status, issued_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"c-a2", "tenant-a", "v-a", "RESUME_MISSION", "acknowledged", "op-a", "2026-11-02T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_commands (id, tenant_id, vehicle_id, command_type, status, issued_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"c-b1", "tenant-b", "v-a", "E_STOP", "acknowledged", "op-b", "2026-11-03T10:00:00Z")

	view := q.Load(ctx, "tenant-a", "v-a")
	assert.True(t, view.Maintenance.IsMaintenanceDue)
	// DATE columns scan back through the sqlite driver as RFC3339 text; the
	// legacy handler rendered that same scan value, so the shape is preserved.
	assert.Equal(t, "2026-12-01T00:00:00Z", view.Maintenance.MaintenanceDue)
	assert.Equal(t, "sup-a", view.Maintenance.MaintenanceOverrideBy)
	assert.True(t, view.Maintenance.IsMaintenanceOverridden)
	assert.Equal(t, "due for service", view.Maintenance.MaintenanceOverrideReason)

	require.True(t, view.LastPosition.Has)
	assert.InDelta(t, 19.076, view.LastPosition.Lat, 0.0001)
	assert.InDelta(t, 72.877, view.LastPosition.Lng, 0.0001)
	assert.InDelta(t, 45.5, view.LastPosition.Speed, 0.0001)

	require.Len(t, view.RecentTrips, 2)
	assert.Equal(t, "TR-A2", view.RecentTrips[0].TripNumber)
	assert.Equal(t, "TR-A1", view.RecentTrips[1].TripNumber)
	assert.True(t, view.RecentTrips[0].CreatedAt.Valid)

	require.Len(t, view.OpenWorkOrders, 1)
	assert.Equal(t, "wo-a1", view.OpenWorkOrders[0].ID)
	assert.Equal(t, "Brake job", view.OpenWorkOrders[0].Title)

	require.NotNil(t, view.AssignedDriver)
	assert.Equal(t, "d-a", view.AssignedDriver.ID)
	assert.Equal(t, "Asha", view.AssignedDriver.FirstName)
	assert.Equal(t, "Rao", view.AssignedDriver.LastName)
	assert.Equal(t, "9000000001", view.AssignedDriver.Phone)
	assert.Equal(t, "DRV-A", view.AssignedDriver.DriverID)
	assert.True(t, view.AssignedDriver.IsPrimary)
	require.Len(t, view.AssignDrivers, 1)
	assert.Equal(t, "d-a", view.AssignDrivers[0].ID)

	require.Len(t, view.MeasuringPoints, 2)
	assert.Equal(t, "p-a", view.MeasuringPoints[0]["ID"])
	assert.Equal(t, "ODO", view.MeasuringPoints[0]["Kind"])
	assert.Equal(t, 50000.0, view.MeasuringPoints[0]["AnnualEstimate"])
	assert.Equal(t, "p-a2", view.MeasuringPoints[1]["ID"])
	assert.NotContains(t, view.MeasuringPoints[1], "AnnualEstimate")

	require.Len(t, view.RecentMeasurements, 2)
	assert.Equal(t, "m-a2", view.RecentMeasurements[0]["ID"])
	assert.Equal(t, 1300.0, view.RecentMeasurements[0]["Counter"])
	assert.Equal(t, 100.0, view.RecentMeasurements[0]["Difference"])
	assert.Equal(t, "KM", view.RecentMeasurements[0]["Unit"])
	assert.Equal(t, "TCS795488", view.RecentMeasurements[0]["ReadBy"])
	assert.NotContains(t, view.RecentMeasurements[1], "Difference", "NULL difference keeps the legacy map-absent shape")

	require.Len(t, view.RecentCommands, 2)
	assert.Equal(t, "c-a2", view.RecentCommands[0]["ID"])
	assert.Equal(t, "RESUME_MISSION", view.RecentCommands[0]["CommandType"])
	assert.Equal(t, "acknowledged", view.RecentCommands[0]["Status"])
	assert.Equal(t, "op-a", view.RecentCommands[0]["IssuedBy"])
	_, hasCreatedAt := view.RecentCommands[0]["CreatedAt"]
	assert.True(t, hasCreatedAt)

	// The same vehicle id under tenant-b renders only tenant-b rows.
	other := q.Load(ctx, "tenant-b", "v-a")
	require.True(t, other.LastPosition.Has)
	assert.InDelta(t, 1.1, other.LastPosition.Lat, 0.0001)
	require.Len(t, other.RecentTrips, 1)
	assert.Equal(t, "TR-B1", other.RecentTrips[0].TripNumber)
	require.Len(t, other.OpenWorkOrders, 1)
	assert.Equal(t, "wo-b1", other.OpenWorkOrders[0].ID)
	require.NotNil(t, other.AssignedDriver)
	assert.Equal(t, "d-b", other.AssignedDriver.ID)
	require.Len(t, other.AssignDrivers, 1)
	assert.Equal(t, "d-b", other.AssignDrivers[0].ID)
	require.Len(t, other.MeasuringPoints, 1)
	assert.Equal(t, "p-b", other.MeasuringPoints[0]["ID"])
	require.Len(t, other.RecentMeasurements, 1)
	assert.Equal(t, "m-b", other.RecentMeasurements[0]["ID"])
	require.Len(t, other.RecentCommands, 1)
	assert.Equal(t, "c-b1", other.RecentCommands[0]["ID"])

	// A tenant with no rows for the vehicle renders an empty, safe page.
	foreign := q.Load(ctx, "tenant-c", "v-a")
	assert.False(t, foreign.LastPosition.Has)
	assert.False(t, foreign.Maintenance.IsMaintenanceDue)
	assert.Empty(t, foreign.RecentTrips)
	assert.Empty(t, foreign.OpenWorkOrders)
	assert.Nil(t, foreign.AssignedDriver)
	assert.Empty(t, foreign.AssignDrivers)
	assert.Empty(t, foreign.MeasuringPoints)
	assert.Empty(t, foreign.RecentMeasurements)
	assert.Empty(t, foreign.RecentCommands)
}

func TestVehicleDetailQueries_LastPositionBindsVehicleAndTenant(t *testing.T) {
	db := openVehicleDetailTestDB(t)
	q := NewVehicleDetailQueries(db)
	ctx := context.Background()

	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_latest_position (vehicle_id, tenant_id, latitude, longitude, speed, device_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"v1", "tenant-a", 19.0, 72.0, 40.0, "2026-11-10T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_latest_position (vehicle_id, tenant_id, latitude, longitude, speed, device_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"v1", "tenant-b", 28.0, 77.0, 50.0, "2026-11-10T11:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_latest_position (vehicle_id, tenant_id, latitude, longitude, speed, device_time) VALUES (?, ?, ?, ?, ?, ?)`,
		"v2", "tenant-a", 13.0, 80.0, 30.0, nil)

	a := q.Load(ctx, "tenant-a", "v1")
	require.True(t, a.LastPosition.Has)
	assert.InDelta(t, 19.0, a.LastPosition.Lat, 0.0001)

	b := q.Load(ctx, "tenant-b", "v1")
	require.True(t, b.LastPosition.Has)
	assert.InDelta(t, 28.0, b.LastPosition.Lat, 0.0001)

	missing := q.Load(ctx, "tenant-c", "v1")
	assert.False(t, missing.LastPosition.Has, "a frame never crosses the tenant boundary")

	noDeviceTime := q.Load(ctx, "tenant-a", "v2")
	require.True(t, noDeviceTime.LastPosition.Has)
	assert.Empty(t, noDeviceTime.LastPosition.At)
}

func TestVehicleDetailQueries_EmptySectionsAreRenderable(t *testing.T) {
	db := openVehicleDetailTestDB(t)
	q := NewVehicleDetailQueries(db)
	ctx := context.Background()

	view := q.Load(ctx, "tenant-a", "missing-vehicle")
	assert.Equal(t, VehicleMaintenanceStatus{}, view.Maintenance)
	assert.False(t, view.LastPosition.Has)
	assert.Empty(t, view.RecentTrips)
	assert.Empty(t, view.OpenWorkOrders)
	assert.Nil(t, view.AssignedDriver)
	assert.Empty(t, view.AssignDrivers)
	assert.Empty(t, view.MeasuringPoints)
	assert.Empty(t, view.RecentMeasurements)
	assert.Empty(t, view.RecentCommands)

	// A closed assignment is not an active driver (legacy unassigned_at filter).
	mustExecVehicleDetail(t, db, `INSERT INTO vehicles (id, tenant_id, registration_number, status) VALUES (?, ?, ?, ?)`,
		"v-closed", "tenant-a", "MH03EF9012", "available")
	mustExecVehicleDetail(t, db, `INSERT INTO drivers (id, tenant_id, first_name, last_name, phone, driver_id) VALUES (?, ?, ?, ?, ?, ?)`,
		"d-closed", "tenant-a", "Closed", "Driver", "9000000003", "DRV-C")
	mustExecVehicleDetail(t, db, `INSERT INTO driver_preferred_vehicles (id, tenant_id, driver_id, vehicle_id, is_primary, unassigned_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"a-closed", "tenant-a", "d-closed", "v-closed", 1, "2026-01-01T00:00:00Z")
	closed := q.Load(ctx, "tenant-a", "v-closed")
	assert.Nil(t, closed.AssignedDriver)
}

func TestVehicleDetailQueries_MeasurementMapKeysMatchLegacyShape(t *testing.T) {
	db := openVehicleDetailTestDB(t)
	q := NewVehicleDetailQueries(db)
	ctx := context.Background()

	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_measuring_points (id, tenant_id, vehicle_id, kind, meas_position, unit, description, annual_estimate, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"p-null", "tenant-a", "v1", "PUMP", "PUMP", "L", "", nil, "2026-11-01T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_measuring_points (id, tenant_id, vehicle_id, kind, meas_position, unit, description, annual_estimate, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"p-zero", "tenant-a", "v1", "ODO", "DISTANCE", "KM", "", 0.0, "2026-11-02T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_measurements (id, tenant_id, point_id, counter_reading, difference_reading, measured_at, read_by, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"m-null", "tenant-a", "p-null", 10.0, nil, nil, nil, "2026-11-01T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_measurements (id, tenant_id, point_id, counter_reading, difference_reading, measured_at, read_by, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"m-full", "tenant-a", "p-null", 25.0, 15.5, "2026-11-02T10:00:00Z", "reader-1", "2026-11-02T10:00:00Z")
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_commands (id, tenant_id, vehicle_id, command_type, status, issued_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"c-no-time", "tenant-a", "v1", "HOLD_POSITION", "acknowledged", "op", nil)
	mustExecVehicleDetail(t, db, `INSERT INTO vehicle_commands (id, tenant_id, vehicle_id, command_type, status, issued_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"c-time", "tenant-a", "v1", "HOLD_POSITION", "acknowledged", "op", "2026-11-01T10:00:00Z")

	view := q.Load(ctx, "tenant-a", "v1")
	require.Len(t, view.MeasuringPoints, 2)
	assert.NotContains(t, view.MeasuringPoints[0], "AnnualEstimate", "NULL annual estimate stays absent")
	assert.Equal(t, 0.0, view.MeasuringPoints[1]["AnnualEstimate"], "explicit zero stays present")

	require.Len(t, view.RecentMeasurements, 2)
	full := view.RecentMeasurements[0]
	assert.Equal(t, 15.5, full["Difference"])
	_, hasMeasuredAt := full["MeasuredAt"]
	assert.True(t, hasMeasuredAt)
	assert.Equal(t, "reader-1", full["ReadBy"])
	null := view.RecentMeasurements[1]
	assert.NotContains(t, null, "Difference")
	assert.NotContains(t, null, "MeasuredAt")
	assert.Equal(t, "", null["ReadBy"], "read_by is always present, matching the legacy map")

	require.Len(t, view.RecentCommands, 2)
	assert.Contains(t, view.RecentCommands[0], "CreatedAt")
	assert.NotContains(t, view.RecentCommands[1], "CreatedAt", "NULL created_at keeps the legacy map-absent shape")
}

func TestVehicleDetailQueries_NilDBDegradesToEmptyView(t *testing.T) {
	q := NewVehicleDetailQueries(nil)
	view := q.Load(context.Background(), "tenant-a", "v1")
	assert.False(t, view.LastPosition.Has)
	assert.Nil(t, view.AssignedDriver)
	assert.Empty(t, view.MeasuringPoints)
	assert.Equal(t, VehicleMaintenanceStatus{}, q.Maintenance(context.Background(), "tenant-a", "v1"))
}
