package telemetry

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"transport-app/internal/eta"
)

// insertTestRoute inserts a route row (FK target for trips).
func insertTestRoute(t *testing.T, db *sql.DB, rid string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES (?, 'src', 'dst', 100, 2, 1000)`, rid)
	require.NoError(t, err)
}

// insertTestTrip inserts a minimal trip row for FK compliance.
func insertTestTrip(t *testing.T, db *sql.DB, tripID string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status)
		VALUES (?, ?, 'r1', datetime('now'), 'in_transit')`, tripID, "TRIP-"+tripID)
	require.NoError(t, err)
}

// insertTestVehicleReg inserts a vehicle row with a caller-chosen
// registration number (the shared helper hardcodes REG-001, which collides
// across vehicles within one test DB).
func insertTestVehicleReg(t *testing.T, db *sql.DB, vid, reg string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry)
		VALUES (?, ?, ?, ?, ?, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'))`,
		vid, reg, "MH-01-"+reg, "truck", 15)
	require.NoError(t, err)
}

func insertLiveSnapshot(t *testing.T, db *sql.DB, id, tripID, vehicleID string, ts time.Time, speed float64) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO telemetry_snapshots
		(id, trip_id, vehicle_id, timestamp, latitude, longitude, speed, fuel_level, odometer)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, tripID, vehicleID, ts.UTC().Format("2006-01-02 15:04:05"),
		19.07, 72.87, speed, 60.0, 10000.0)
	require.NoError(t, err)
}

func TestLiveStore_States(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicleReg(t, db, "v1", "REG-1")
	insertTestVehicleReg(t, db, "v2", "REG-2")
	insertTestVehicleReg(t, db, "v3", "REG-3")

	now := time.Now().UTC()
	insertLiveSnapshot(t, db, "s1", "", "v1", now.Add(-2*time.Minute), 45.0)  // running
	insertLiveSnapshot(t, db, "s2", "", "v2", now.Add(-2*time.Minute), 0.0)   // stopped
	insertLiveSnapshot(t, db, "s3", "", "v3", now.Add(-20*time.Minute), 10.0) // stale → no_signal

	store := NewLiveStore(db, 15*time.Minute)
	vehicles, err := store.Live(context.Background(), "1", "", now)
	require.NoError(t, err)
	require.Len(t, vehicles, 3)

	byID := map[string]LiveVehicle{}
	for _, v := range vehicles {
		byID[v.VehicleID] = v
	}
	assert.Equal(t, MarkerStateRunning, byID["v1"].Status)
	assert.Equal(t, MarkerStateStopped, byID["v2"].Status)
	assert.Equal(t, MarkerStateNoSignal, byID["v3"].Status)
}

func TestLiveStore_LatestPerVehicle(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicleReg(t, db, "v1", "REG-1")

	now := time.Now().UTC()
	insertLiveSnapshot(t, db, "old", "", "v1", now.Add(-10*time.Minute), 5.0)
	insertLiveSnapshot(t, db, "new", "", "v1", now.Add(-1*time.Minute), 60.0)

	store := NewLiveStore(db, 15*time.Minute)
	vehicles, err := store.Live(context.Background(), "1", "", now)
	require.NoError(t, err)
	require.Len(t, vehicles, 1)
	// The latest row (speed 60) must win over the older one (speed 5).
	assert.Equal(t, 60.0, vehicles[0].Speed)
}

func TestLiveStore_MaintenanceDueOverrides(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicleReg(t, db, "v1", "REG-1")
	_, err := db.Exec(`UPDATE vehicles SET maintenance_due = 1 WHERE id = 'v1'`)
	require.NoError(t, err)

	now := time.Now().UTC()
	insertLiveSnapshot(t, db, "s1", "", "v1", now.Add(-1*time.Minute), 40.0)

	store := NewLiveStore(db, 15*time.Minute)
	vehicles, err := store.Live(context.Background(), "1", "", now)
	require.NoError(t, err)
	require.Len(t, vehicles, 1)
	assert.Equal(t, MarkerStateMaintenanceDue, vehicles[0].Status)
}

// insertTestDriver inserts a minimal driver row and returns its id.
func insertTestDriver(t *testing.T, db *sql.DB, id, first, last, phone string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry)
		VALUES (?, ?, ?, ?, ?, 'DL-TEST', date('now','+1 year'))`, id, "D-"+id, first, last, phone)
	require.NoError(t, err)
}

func TestLiveStore_DriverInfoJoined(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestRoute(t, db, "r1")
	insertTestDriver(t, db, "d1", "Ramesh", "Kumar", "+91-98200-00000")
	_, err := db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, driver_id)
		VALUES ('t1', 'TRIP-t1', 'r1', datetime('now'), 'in_transit', 'd1')`)
	require.NoError(t, err)

	insertTestVehicleReg(t, db, "v1", "REG-1")
	insertTestVehicleReg(t, db, "v2", "REG-2") // unassigned trip ⇒ no driver

	now := time.Now().UTC()
	insertLiveSnapshot(t, db, "s1", "t1", "v1", now.Add(-1*time.Minute), 30.0)
	insertLiveSnapshot(t, db, "s2", "", "v2", now.Add(-1*time.Minute), 0.0)

	store := NewLiveStore(db, 15*time.Minute)
	vehicles, err := store.Live(context.Background(), "1", "", now)
	require.NoError(t, err)
	require.Len(t, vehicles, 2)

	byID := map[string]LiveVehicle{}
	for _, v := range vehicles {
		byID[v.VehicleID] = v
	}
	assert.Equal(t, "Ramesh Kumar", byID["v1"].DriverName)
	assert.Equal(t, "+91-98200-00000", byID["v1"].DriverPhone)
	assert.Empty(t, byID["v2"].DriverName)
	assert.Empty(t, byID["v2"].DriverPhone)
}

func TestLiveStore_TripFilter(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestRoute(t, db, "r1")
	insertTestTrip(t, db, "t1")
	insertTestTrip(t, db, "t2")
	insertTestVehicleReg(t, db, "v1", "REG-1")
	insertTestVehicleReg(t, db, "v2", "REG-2")

	now := time.Now().UTC()
	insertLiveSnapshot(t, db, "s1", "t1", "v1", now.Add(-1*time.Minute), 30.0)
	insertLiveSnapshot(t, db, "s2", "t2", "v2", now.Add(-1*time.Minute), 30.0)

	store := NewLiveStore(db, 15*time.Minute)
	vehicles, err := store.Live(context.Background(), "1", "t1", now)
	require.NoError(t, err)
	require.Len(t, vehicles, 1)
	assert.Equal(t, "v1", vehicles[0].VehicleID)
	assert.Equal(t, "t1", vehicles[0].TripID)
}

func TestLiveStore_RouteKMJoined(t *testing.T) {
	db := newTestIngestorDB(t)
	_, err := db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('r1', 'src', 'dst', 420.5, 8, 1000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status)
		VALUES ('t1', 'TRIP-t1', 'r1', datetime('now'), 'in_transit')`)
	require.NoError(t, err)
	insertTestVehicleReg(t, db, "v1", "REG-1")

	now := time.Now().UTC()
	insertLiveSnapshot(t, db, "s1", "t1", "v1", now.Add(-1*time.Minute), 30.0)
	// Unassigned vehicle: no trip ⇒ no route distance.
	insertTestVehicleReg(t, db, "v2", "REG-2")
	insertLiveSnapshot(t, db, "s2", "", "v2", now.Add(-1*time.Minute), 0.0)

	store := NewLiveStore(db, 15*time.Minute)
	vehicles, err := store.Live(context.Background(), "1", "", now)
	require.NoError(t, err)
	require.Len(t, vehicles, 2)

	byID := map[string]LiveVehicle{}
	for _, v := range vehicles {
		byID[v.VehicleID] = v
	}
	require.NotNil(t, byID["v1"].RouteKM)
	assert.InDelta(t, 420.5, *byID["v1"].RouteKM, 0.001)
	assert.Nil(t, byID["v2"].RouteKM)
}

func TestLiveStore_TenantScoping(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicleReg(t, db, "v1", "REG-1")
	_, err := db.Exec(`UPDATE vehicles SET tenant_id = '2' WHERE id = 'v1'`)
	require.NoError(t, err)

	now := time.Now().UTC()
	insertLiveSnapshot(t, db, "s1", "", "v1", now.Add(-1*time.Minute), 30.0)

	store := NewLiveStore(db, 15*time.Minute)
	vehicles, err := store.Live(context.Background(), "1", "", now)
	require.NoError(t, err)
	assert.Empty(t, vehicles)
}

func TestLiveStore_Empty(t *testing.T) {
	db := newTestIngestorDB(t)
	store := NewLiveStore(db, 15*time.Minute)
	vehicles, err := store.Live(context.Background(), "1", "", time.Now().UTC())
	require.NoError(t, err)
	assert.Empty(t, vehicles)
}

func TestLiveStore_WithEtaService(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestRoute(t, db, "r1")
	insertTestTrip(t, db, "t1")
	insertTestVehicleReg(t, db, "v1", "REG-1")

	now := time.Now().UTC()
	started := now.Add(-1 * time.Hour)
	// Seed 4 snapshots to trigger hybrid ETA calculation
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots
		(id, trip_id, vehicle_id, timestamp, latitude, longitude, speed, fuel_level, odometer)
		VALUES ('s-start', 't1', 'v1', ?, 19.0, 72.8, 60.0, 80.0, 1000.0)`, started.Format("2006-01-02 15:04:05"))
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots
		(id, trip_id, vehicle_id, timestamp, latitude, longitude, speed, fuel_level, odometer)
		VALUES ('s-1', 't1', 'v1', ?, 19.1, 72.8, 60.0, 80.0, 1010.0)`, now.Add(-10*time.Minute).Format("2006-01-02 15:04:05"))
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots
		(id, trip_id, vehicle_id, timestamp, latitude, longitude, speed, fuel_level, odometer)
		VALUES ('s-2', 't1', 'v1', ?, 19.2, 72.8, 60.0, 80.0, 1020.0)`, now.Add(-5*time.Minute).Format("2006-01-02 15:04:05"))
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots
		(id, trip_id, vehicle_id, timestamp, latitude, longitude, speed, fuel_level, odometer)
		VALUES ('s-3', 't1', 'v1', ?, 19.3, 72.8, 60.0, 80.0, 1030.0)`, now.Add(-1*time.Minute).Format("2006-01-02 15:04:05"))

	etaSvc := eta.NewEtaService(db, 15, 30, 5)
	store := NewLiveStore(db, 15*time.Minute).WithEtaService(etaSvc)

	vehicles, err := store.Live(context.Background(), "1", "t1", now)
	require.NoError(t, err)
	require.Len(t, vehicles, 1)
	assert.Equal(t, "v1", vehicles[0].VehicleID)
	assert.NotEmpty(t, vehicles[0].EtaMethod)
	assert.NotNil(t, vehicles[0].EtaMin)
	assert.NotNil(t, vehicles[0].EtaMax)
}
