package eta

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newEtaTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_eta_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedTripAndRoute(t *testing.T, db *sql.DB, tripID, vehicleID, status string, distance, estHours float64) {
	t.Helper()
	_, _ = db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, maintenance_due)
		VALUES (?, 'MH-12-AB-1234', 'MH-12-AB-1234', 'truck', 10, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 0)`,
		vehicleID)

	_, _ = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES (?, 'Mumbai', 'Pune', ?, ?, 5000.0)`, "r-"+tripID, distance, estHours)

	now := time.Now().UTC()
	started := now.Add(-1 * time.Hour)
	arrival := now.Add(2 * time.Hour)

	_, err := db.Exec(`INSERT INTO trips
		(id, trip_number, route_id, vehicle_id, departure_time, started_at, arrival_time, status, tenant_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, '1')`,
		tripID, "TRP-"+tripID, "r-"+tripID, vehicleID, started.Format(time.RFC3339), started.Format(time.RFC3339), arrival.Format(time.RFC3339), status)
	require.NoError(t, err)
}

func TestEtaService_FreshnessGate(t *testing.T) {
	db := newEtaTestDB(t)
	svc := NewEtaService(db, 15, 30, 5)
	ctx := context.Background()

	seedTripAndRoute(t, db, "trip-fresh", "veh-fresh", "in_transit", 100.0, 2.0)

	// 1. No telemetry snapshots -> scheduled fallback
	resNoTele, err := svc.Calculate(ctx, "trip-fresh")
	require.NoError(t, err)
	assert.Equal(t, "scheduled", resNoTele.Method)
	assert.Equal(t, "low", resNoTele.Confidence)

	// Verify fallback audit log written
	var auditCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE action = 'eta_fallback' AND record_id = 'trip-fresh'").Scan(&auditCount)
	assert.GreaterOrEqual(t, auditCount, 1)

	// 2. Stale telemetry (20 min old, staleMin = 15) -> scheduled fallback
	staleTime := time.Now().UTC().Add(-20 * time.Minute)
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots
		(id, trip_id, vehicle_id, latitude, longitude, speed, odometer, timestamp)
		VALUES ('snap-stale', 'trip-fresh', 'veh-fresh', 19.0, 72.8, 50.0, 1000.0, ?)`, staleTime)

	resStale, err := svc.Calculate(ctx, "trip-fresh")
	require.NoError(t, err)
	assert.Equal(t, "scheduled", resStale.Method)

	// 3. Fresh telemetry (2 min old, 4 speed samples) -> hybrid
	freshTime := time.Now().UTC().Add(-2 * time.Minute)
	for i := 1; i <= 4; i++ {
		ts := freshTime.Add(time.Duration(i*10) * time.Second)
		_, _ = db.Exec(`INSERT INTO telemetry_snapshots
			(id, trip_id, vehicle_id, latitude, longitude, speed, odometer, timestamp)
			VALUES (?, 'trip-fresh', 'veh-fresh', 19.1, 72.9, 60.0, ?, ?)`,
			fmt.Sprintf("snap-fresh-%d", i), 1000.0+float64(i*5), ts)
	}

	resFresh, err := svc.Calculate(ctx, "trip-fresh")
	require.NoError(t, err)
	assert.Equal(t, "hybrid", resFresh.Method)
	assert.Equal(t, 30*time.Minute, resFresh.EtaMax.Sub(resFresh.EtaMin))
}

func TestEtaService_RollingAvgSpeed(t *testing.T) {
	db := newEtaTestDB(t)
	svc := NewEtaService(db, 15, 30, 5)
	ctx := context.Background()

	seedTripAndRoute(t, db, "trip-speed", "veh-speed", "in_transit", 150.0, 3.0)

	// 1. Only 2 samples (< 3 required) -> scheduled fallback
	now := time.Now().UTC()
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, timestamp)
		VALUES ('snap-s1', 'trip-speed', 'veh-speed', 19.0, 72.8, 40.0, ?)`, now.Add(-3*time.Minute))
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, timestamp)
		VALUES ('snap-s2', 'trip-speed', 'veh-speed', 19.0, 72.8, 60.0, ?)`, now.Add(-1*time.Minute))

	resLowSamples, err := svc.Calculate(ctx, "trip-speed")
	require.NoError(t, err)
	assert.Equal(t, "scheduled", resLowSamples.Method)

	// 2. Add 3rd, 4th, 5th samples -> Valid average speed: (40+60+50+50+50)/5 = 50.0 km/h
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, timestamp)
		VALUES ('snap-s3', 'trip-speed', 'veh-speed', 19.0, 72.8, 50.0, ?)`, now.Add(-40*time.Second))
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, timestamp)
		VALUES ('snap-s4', 'trip-speed', 'veh-speed', 19.0, 72.8, 50.0, ?)`, now.Add(-30*time.Second))
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, timestamp)
		VALUES ('snap-s5', 'trip-speed', 'veh-speed', 19.0, 72.8, 50.0, ?)`, now.Add(-10*time.Second))

	resValid, err := svc.Calculate(ctx, "trip-speed")
	require.NoError(t, err)
	assert.InDelta(t, 50.0, resValid.AvgSpeed, 0.5)
	assert.Equal(t, "high", resValid.Confidence)
}

func TestEtaService_OdometerDelta(t *testing.T) {
	db := newEtaTestDB(t)
	svc := NewEtaService(db, 15, 30, 5)
	ctx := context.Background()

	seedTripAndRoute(t, db, "trip-odo", "veh-odo", "in_transit", 100.0, 2.0)

	// Route distance = 100 km.
	// Odometer at start = 1000.0 km
	// Odometer current = 1040.0 km
	// Distance travelled = 40 km -> Remaining = 60 km
	now := time.Now().UTC()
	started := now.Add(-1 * time.Hour)

	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, odometer, timestamp)
		VALUES ('snap-o1', 'trip-odo', 'veh-odo', 19.0, 72.8, 50.0, 1000.0, ?)`, started)
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, odometer, timestamp)
		VALUES ('snap-o2', 'trip-odo', 'veh-odo', 19.0, 72.8, 50.0, 1010.0, ?)`, now.Add(-10*time.Minute))
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, odometer, timestamp)
		VALUES ('snap-o3', 'trip-odo', 'veh-odo', 19.0, 72.8, 50.0, 1025.0, ?)`, now.Add(-5*time.Minute))
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, odometer, timestamp)
		VALUES ('snap-o4', 'trip-odo', 'veh-odo', 19.0, 72.8, 50.0, 1040.0, ?)`, now.Add(-1*time.Minute))

	res, err := svc.Calculate(ctx, "trip-odo")
	require.NoError(t, err)
	assert.InDelta(t, 60.0, res.RemainingKM, 0.1)
}

func TestEtaService_HybridBlend(t *testing.T) {
	db := newEtaTestDB(t)
	svc := NewEtaService(db, 15, 30, 5)
	ctx := context.Background()

	// Route: 100 km, 2.0 hours estimated (scheduled speed = 50 km/h)
	seedTripAndRoute(t, db, "trip-blend", "veh-blend", "in_transit", 100.0, 2.0)

	now := time.Now().UTC()
	started := now.Add(-1 * time.Hour)

	// Remaining distance = 50 km (1050 - 1000)
	// AvgSpeed = 100 km/h
	// etaTelemetry = 50 km / 100 km/h = 0.5 hours
	// etaScheduled = 2.0 hours * (50 km / 100 km) = 1.0 hours
	// etaHybrid = 0.7 * 0.5 + 0.3 * 1.0 = 0.35 + 0.30 = 0.65 hours = 39 minutes
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, odometer, timestamp)
		VALUES ('snap-b1', 'trip-blend', 'veh-blend', 19.0, 72.8, 100.0, 1000.0, ?)`, started)
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, odometer, timestamp)
		VALUES ('snap-b2', 'trip-blend', 'veh-blend', 19.0, 72.8, 100.0, 1010.0, ?)`, now.Add(-10*time.Minute))
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, odometer, timestamp)
		VALUES ('snap-b3', 'trip-blend', 'veh-blend', 19.0, 72.8, 100.0, 1030.0, ?)`, now.Add(-5*time.Minute))
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, odometer, timestamp)
		VALUES ('snap-b4', 'trip-blend', 'veh-blend', 19.0, 72.8, 100.0, 1050.0, ?)`, now.Add(-1*time.Minute))

	res, err := svc.Calculate(ctx, "trip-blend")
	require.NoError(t, err)
	assert.Equal(t, "hybrid", res.Method)

	diffMin := res.ArrivalAt.Sub(now).Minutes()
	assert.InDelta(t, 39.0, diffMin, 2.0)
	assert.Equal(t, 30*time.Minute, res.EtaMax.Sub(res.EtaMin))
}

func TestEtaService_MonotonicGuard(t *testing.T) {
	db := newEtaTestDB(t)
	// guardMaxRegress = 5 minutes
	svc := NewEtaService(db, 15, 30, 5)
	ctx := context.Background()

	tripID := "trip-guard"
	seedTripAndRoute(t, db, tripID, "veh-guard", "in_transit", 100.0, 2.0)

	baseArrival := time.Now().UTC().Add(60 * time.Minute)
	// Seed lastETA
	svc.lastETA[tripID] = baseArrival

	// 1. Monotonic jump backward by 20 minutes (e.g. vehicle suddenly speeds up)
	// Should be clamped to at most 5 minutes earlier than baseArrival: baseArrival - 5m = 55m
	jumpArrival := baseArrival.Add(-20 * time.Minute)
	clamped := svc.applyMonotonicGuard(ctx, tripID, jumpArrival)

	expectedClamped := baseArrival.Add(-5 * time.Minute)
	assert.Equal(t, expectedClamped.Unix(), clamped.Unix())

	// Verify audit log written for clamp
	var auditCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE action = 'eta_guard' AND record_id = 'trip-guard'").Scan(&auditCount)
	assert.Equal(t, 1, auditCount)

	// 2. ETA worsens by 15 minutes -> Allowed without clamping
	worseArrival := clamped.Add(15 * time.Minute)
	unclamped := svc.applyMonotonicGuard(ctx, tripID, worseArrival)
	assert.Equal(t, worseArrival.Unix(), unclamped.Unix())
}

func TestEtaService_ScheduledFallback_And_InactivePhases(t *testing.T) {
	db := newEtaTestDB(t)
	svc := NewEtaService(db, 15, 30, 5)
	ctx := context.Background()

	// 1. Inactive phase (e.g. draft, completed, cancelled) -> Returns error
	seedTripAndRoute(t, db, "trip-draft", "veh-draft", "draft", 100.0, 2.0)
	_, err := svc.Calculate(ctx, "trip-draft")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not active")

	seedTripAndRoute(t, db, "trip-comp", "veh-comp", "completed", 100.0, 2.0)
	_, err = svc.Calculate(ctx, "trip-comp")
	assert.Error(t, err)

	// 2. Active phase without telemetry -> scheduled fallback
	seedTripAndRoute(t, db, "trip-started-no-tele", "veh-started", "started", 100.0, 2.0)
	res, err := svc.Calculate(ctx, "trip-started-no-tele")
	require.NoError(t, err)
	assert.Equal(t, "scheduled", res.Method)
	assert.Equal(t, 30*time.Minute, res.EtaMax.Sub(res.EtaMin))
}

func TestLoadTrip_HaversineFallbackWhenDistanceMissing(t *testing.T) {
	db := newEtaTestDB(t)
	seedTripAndRoute(t, db, "trp-geo", "veh-geo", "in_transit", 0, 0)
	_, err := db.Exec(`INSERT INTO route_locations
		(route_id, source_lat, source_lng, dest_lat, dest_lng)
		VALUES ('r-trp-geo', 12.9716, 77.5946, 18.5204, 73.8567)`)
	require.NoError(t, err)

	svc := NewEtaService(db, 15, 30, 5)
	td, err := svc.loadTrip(context.Background(), "trp-geo")
	require.NoError(t, err)

	expect := haversineKm(12.9716, 77.5946, 18.5204, 73.8567) * roadFactor
	assert.InDelta(t, expect, td.RouteDistance, 0.001)
	assert.Greater(t, td.RouteDistance, float64(800), "BLR→Pune road estimate should exceed 800 km")
}

func TestLoadTrip_ManualDistanceWinsOverFallback(t *testing.T) {
	db := newEtaTestDB(t)
	seedTripAndRoute(t, db, "trp-manual", "veh-manual", "in_transit", 500, 9)
	_, err := db.Exec(`INSERT INTO route_locations
		(route_id, source_lat, source_lng, dest_lat, dest_lng)
		VALUES ('r-trp-manual', 12.9716, 77.5946, 18.5204, 73.8567)`)
	require.NoError(t, err)

	svc := NewEtaService(db, 15, 30, 5)
	td, err := svc.loadTrip(context.Background(), "trp-manual")
	require.NoError(t, err)
	assert.Equal(t, float64(500), td.RouteDistance)
}
