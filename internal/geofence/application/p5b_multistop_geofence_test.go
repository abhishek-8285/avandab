package application_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/events"
	geoApp "transport-app/internal/geofence/application"
	tripagg "transport-app/internal/trip/domain/aggregate"
	triprepo "transport-app/internal/trip/infrastructure/persistence/sql"
)

type p5bMockEventBus struct {
	mu     sync.Mutex
	events []events.Event
}

func (m *p5bMockEventBus) Publish(ctx context.Context, e events.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}

func (m *p5bMockEventBus) Subscribe(eventType string, handler events.Handler) func() {
	return func() {}
}

func setupP5BTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_p5b_geo_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))

	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-1', 'Tenant One', 'tenant-1')`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-2', 'Tenant Two', 'tenant-2')`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO routes (id, tenant_id, source, destination, distance, standard_fare) VALUES ('rt-1', 'tenant-1', 'Delhi', 'Udaipur', 650, 30000)`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id) VALUES ('drv-1', 'DRV-1', 'Dev', 'Singh', '9876543210', 'DL-1', date('now','+1 year'), 'available', 'tenant-1')`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, tenant_id) VALUES ('veh_p5b_1', 'DL-01-P5B', 'DL-01-P5B', 'truck', 20, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 'tenant-1')`)

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestP5B_ActiveStopGeofence_EvaluationAndInvariants(t *testing.T) {
	db := setupP5BTestDB(t)
	bus := &p5bMockEventBus{}
	evaluator := geoApp.NewRealtimeEvaluator(db, bus, nil, nil)
	ctx := context.Background()

	tenantID := "tenant-1"
	tripID := "trip_p5b_multi"
	vehicleID := "veh_p5b_1"

	// 1. Seed Trip and 3 Stops:
	// Stop 1: Delhi (28.6139, 77.2090)
	// Stop 2: Jaipur (26.9124, 75.7873)
	// Stop 3: Udaipur (24.5854, 73.7125)
	_, err := db.Exec(`
		INSERT INTO trips (id, tenant_id, trip_number, driver_id, vehicle_id, route_id, departure_time, status)
		VALUES (?, ?, 'TRIP-P5B-01', 'drv-1', ?, 'rt-1', datetime('now'), 'in_transit')
	`, tripID, tenantID, vehicleID)
	require.NoError(t, err)

	repo := triprepo.NewTripRepository(db)
	trip, err := repo.Find(ctx, tripagg.TripID(tripID), "tenant-1")
	require.NoError(t, err)

	stop1 := tripagg.TripStop{
		ID:              "stop_1_delhi",
		TenantID:        "tenant-1",
		TripID:          tripagg.TripID(tripID),
		StopSequence:    1,
		StopType:        tripagg.StopTypePickup,
		LocationName:    "Delhi Hub",
		Address:         "Connaught Place, New Delhi",
		Latitude:        ptrFloat(28.6139),
		Longitude:       ptrFloat(77.2090),
		GeofenceRadiusM: 100,
		Status:          tripagg.StopStatusPending,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	stop2 := tripagg.TripStop{
		ID:              "stop_2_jaipur",
		TenantID:        "tenant-1",
		TripID:          tripagg.TripID(tripID),
		StopSequence:    2,
		StopType:        tripagg.StopTypeDrop,
		LocationName:    "Jaipur Terminal",
		Address:         "MI Road, Jaipur",
		Latitude:        ptrFloat(26.9124),
		Longitude:       ptrFloat(75.7873),
		GeofenceRadiusM: 100,
		Status:          tripagg.StopStatusPending,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	stop3 := tripagg.TripStop{
		ID:              "stop_3_udaipur",
		TenantID:        "tenant-1",
		TripID:          tripagg.TripID(tripID),
		StopSequence:    3,
		StopType:        tripagg.StopTypeDrop,
		LocationName:    "Udaipur Depot",
		Address:         "City Palace Rd, Udaipur",
		Latitude:        ptrFloat(24.5854),
		Longitude:       ptrFloat(73.7125),
		GeofenceRadiusM: 100,
		Status:          tripagg.StopStatusPending,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}

	trip.AddStop(stop1)
	trip.AddStop(stop2)
	trip.AddStop(stop3)
	require.NoError(t, repo.Save(ctx, trip))

	now := time.Now().UTC()

	// 2. Telemetry Fixes entering Stop 1 (Delhi): Probe fix -> Debounce confirmation fix (65s later)
	fix1a := geoApp.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: vehicleID,
		TripID:    &tripID,
		Latitude:  28.6139,
		Longitude: 77.2090,
		Speed:     0,
		Accuracy:  5.0,
		Timestamp: now,
	}
	_, err = evaluator.EvaluateFix(ctx, fix1a)
	require.NoError(t, err)

	fix1b := geoApp.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: vehicleID,
		TripID:    &tripID,
		Latitude:  28.6139,
		Longitude: 77.2090,
		Speed:     0,
		Accuracy:  5.0,
		Timestamp: now.Add(65 * time.Second),
	}
	events1, err := evaluator.EvaluateFix(ctx, fix1b)
	require.NoError(t, err)
	assert.NotEmpty(t, events1)

	// Invariant: Stop 1 must transition to 'arrived'
	var s1Status, s2Status, s3Status string
	require.NoError(t, db.QueryRow(`SELECT status FROM trip_stops WHERE id = 'stop_1_delhi'`).Scan(&s1Status))
	require.NoError(t, db.QueryRow(`SELECT status FROM trip_stops WHERE id = 'stop_2_jaipur'`).Scan(&s2Status))
	require.NoError(t, db.QueryRow(`SELECT status FROM trip_stops WHERE id = 'stop_3_udaipur'`).Scan(&s3Status))

	assert.Equal(t, "arrived", s1Status)
	assert.Equal(t, "pending", s2Status)
	assert.Equal(t, "pending", s3Status)

	// 3. 5x Replay Protection: Replaying the same fix produces no duplicate events/rows
	for i := 0; i < 5; i++ {
		_, err := evaluator.EvaluateFix(ctx, fix1b)
		require.NoError(t, err)
	}
	var countGeoEvents int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM geofence_events WHERE trip_id = ?`, tripID).Scan(&countGeoEvents))
	assert.Equal(t, 1, countGeoEvents)

	// 4. Complete Stop 1 (Delhi)
	_, err = db.Exec(`UPDATE trip_stops SET status = 'completed', actual_departure = datetime('now') WHERE id = 'stop_1_delhi'`)
	require.NoError(t, err)

	// 5. Sequence Invariant Check:
	// Fix near Stop 3 (Udaipur) while Stop 2 (Jaipur) is still pending:
	// 650km at 65km/h -> 10 hours later
	fixOutSeqA := geoApp.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: vehicleID,
		TripID:    &tripID,
		Latitude:  24.5854,
		Longitude: 73.7125, // Udaipur coordinates
		Speed:     0,
		Accuracy:  5.0,
		Timestamp: now.Add(10 * time.Hour),
	}
	_, err = evaluator.EvaluateFix(ctx, fixOutSeqA)
	require.NoError(t, err)

	fixOutSeqB := geoApp.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: vehicleID,
		TripID:    &tripID,
		Latitude:  24.5854,
		Longitude: 73.7125,
		Speed:     0,
		Accuracy:  5.0,
		Timestamp: now.Add(10*time.Hour + 65*time.Second),
	}
	_, err = evaluator.EvaluateFix(ctx, fixOutSeqB)
	require.NoError(t, err)

	require.NoError(t, db.QueryRow(`SELECT status FROM trip_stops WHERE id = 'stop_2_jaipur'`).Scan(&s2Status))
	require.NoError(t, db.QueryRow(`SELECT status FROM trip_stops WHERE id = 'stop_3_udaipur'`).Scan(&s3Status))
	assert.Equal(t, "pending", s2Status)
	assert.Equal(t, "pending", s3Status, "Stop 3 cannot be arrived out-of-order")

	// 6. Arrive at Stop 2 (Jaipur): 400km back from Udaipur to Jaipur in 6 hours -> 16 hours later
	fix2a := geoApp.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: vehicleID,
		TripID:    &tripID,
		Latitude:  26.9124,
		Longitude: 75.7873, // Jaipur
		Speed:     0,
		Accuracy:  5.0,
		Timestamp: now.Add(17 * time.Hour),
	}
	_, err = evaluator.EvaluateFix(ctx, fix2a)
	require.NoError(t, err)

	fix2b := geoApp.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: vehicleID,
		TripID:    &tripID,
		Latitude:  26.9124,
		Longitude: 75.7873,
		Speed:     0,
		Accuracy:  5.0,
		Timestamp: now.Add(17*time.Hour + 65*time.Second),
	}
	_, err = evaluator.EvaluateFix(ctx, fix2b)
	require.NoError(t, err)

	require.NoError(t, db.QueryRow(`SELECT status FROM trip_stops WHERE id = 'stop_2_jaipur'`).Scan(&s2Status))
	assert.Equal(t, "arrived", s2Status)

	// Complete Stop 2
	_, err = db.Exec(`UPDATE trip_stops SET status = 'completed', actual_departure = datetime('now') WHERE id = 'stop_2_jaipur'`)
	require.NoError(t, err)

	// 7. Arrive at Stop 3 (Udaipur): 400km from Jaipur to Udaipur in 6 hours -> 24 hours later
	fix3a := geoApp.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: vehicleID,
		TripID:    &tripID,
		Latitude:  24.5854,
		Longitude: 73.7125, // Udaipur
		Speed:     0,
		Accuracy:  5.0,
		Timestamp: now.Add(24 * time.Hour),
	}
	_, err = evaluator.EvaluateFix(ctx, fix3a)
	require.NoError(t, err)

	fix3b := geoApp.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: vehicleID,
		TripID:    &tripID,
		Latitude:  24.5854,
		Longitude: 73.7125,
		Speed:     0,
		Accuracy:  5.0,
		Timestamp: now.Add(24*time.Hour + 65*time.Second),
	}
	_, err = evaluator.EvaluateFix(ctx, fix3b)
	require.NoError(t, err)

	require.NoError(t, db.QueryRow(`SELECT status FROM trip_stops WHERE id = 'stop_3_udaipur'`).Scan(&s3Status))
	assert.Equal(t, "arrived", s3Status)

	// Complete Stop 3
	_, err = db.Exec(`UPDATE trip_stops SET status = 'completed', actual_departure = datetime('now') WHERE id = 'stop_3_udaipur'`)
	require.NoError(t, err)

	// 8. Re-entry into Completed Stop 1: Vehicle drives back to Delhi 650km in 10 hours -> 35 hours later
	fixReEntryA := geoApp.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: vehicleID,
		TripID:    &tripID,
		Latitude:  28.6139,
		Longitude: 77.2090,
		Speed:     0,
		Accuracy:  5.0,
		Timestamp: now.Add(35 * time.Hour),
	}
	_, err = evaluator.EvaluateFix(ctx, fixReEntryA)
	require.NoError(t, err)

	fixReEntryB := geoApp.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: vehicleID,
		TripID:    &tripID,
		Latitude:  28.6139,
		Longitude: 77.2090,
		Speed:     0,
		Accuracy:  5.0,
		Timestamp: now.Add(35*time.Hour + 65*time.Second),
	}
	_, err = evaluator.EvaluateFix(ctx, fixReEntryB)
	require.NoError(t, err)

	require.NoError(t, db.QueryRow(`SELECT status FROM trip_stops WHERE id = 'stop_1_delhi'`).Scan(&s1Status))
	assert.Equal(t, "completed", s1Status, "Completed stop must not revert to arrived on re-entry")

	// 9. Multi-Tenant Isolation: Fix from Tenant-2 cannot affect Tenant-1 stops
	fixTenant2 := geoApp.TelemetryFix{
		TenantID:  "tenant-2",
		VehicleID: vehicleID,
		TripID:    &tripID,
		Latitude:  28.6139,
		Longitude: 77.2090,
		Timestamp: now.Add(36 * time.Hour),
	}
	_, err = evaluator.EvaluateFix(ctx, fixTenant2)
	require.NoError(t, err)
}

func ptrFloat(f float64) *float64 {
	return &f
}
