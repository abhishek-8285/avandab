package deviation_test

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"transport-app/internal/deviation"
	"transport-app/internal/events"
	"transport-app/internal/fuel"
	geodomain "transport-app/internal/geofence/domain"
)

type mockEventBus struct {
	mu     sync.Mutex
	events []events.Event
}

func (m *mockEventBus) Publish(ctx context.Context, e events.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}

func (m *mockEventBus) Subscribe(eventType string, handler events.Handler) func() {
	return func() {}
}

func (m *mockEventBus) CountType(eventType string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	cnt := 0
	for _, e := range m.events {
		if e.Type == eventType {
			cnt++
		}
	}
	return cnt
}

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory: %v", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS routes (
		id TEXT PRIMARY KEY,
		source TEXT NOT NULL,
		destination TEXT NOT NULL,
		distance REAL NOT NULL,
		estimated_hours REAL NOT NULL
	);

	CREATE TABLE IF NOT EXISTS route_locations (
		route_id TEXT PRIMARY KEY,
		source_lat REAL NOT NULL,
		source_lng REAL NOT NULL,
		source_name TEXT,
		dest_lat REAL NOT NULL,
		dest_lng REAL NOT NULL,
		dest_name TEXT
	);

	CREATE TABLE IF NOT EXISTS trips (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		route_id TEXT NOT NULL,
		vehicle_id TEXT NOT NULL,
		driver_id TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS telemetry_alerts (
		id TEXT PRIMARY KEY,
		trip_id TEXT,
		vehicle_id TEXT,
		driver_id TEXT,
		alert_type TEXT NOT NULL,
		severity TEXT NOT NULL,
		details TEXT NOT NULL,
		latitude REAL,
		longitude REAL,
		resolved INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS outbox_events (
		id TEXT PRIMARY KEY,
		aggregate_id TEXT NOT NULL,
		aggregate_type TEXT NOT NULL,
		event_type TEXT NOT NULL,
		payload TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS company_config (
		tenant_id TEXT NOT NULL,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		PRIMARY KEY (tenant_id, key)
	);
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}
	return db
}

func seedRouteAndTrip(t *testing.T, db *sql.DB, tripID, status, tenantID string) {
	routeID := "route_mumbai_pune"
	// Mumbai (19.0760, 72.8777) to Pune (18.5204, 73.8567)
	_, _ = db.Exec(`
		INSERT OR REPLACE INTO routes (id, source, destination, distance, estimated_hours)
		VALUES (?, 'Mumbai', 'Pune', 150.0, 3.5)`, routeID)

	_, _ = db.Exec(`
		INSERT OR REPLACE INTO route_locations (route_id, source_lat, source_lng, dest_lat, dest_lng)
		VALUES (?, 19.0760, 72.8777, 18.5204, 73.8567)`, routeID)

	_, _ = db.Exec(`
		INSERT OR REPLACE INTO trips (id, tenant_id, route_id, vehicle_id, driver_id, status)
		VALUES (?, ?, ?, 'V-001', 'D-001', ?)`, tripID, tenantID, routeID, status)
}

func TestGPSDeviation_StateTransitionsAndReplayInvariants(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	bus := &mockEventBus{}
	engine := deviation.NewEngine(db, bus, nil, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	tripID := "TRIP-DEV-001"
	seedRouteAndTrip(t, db, tripID, "in_transit", "tenant_alpha")

	now := time.Now().UTC()

	// 1. Point ON ROUTE (Midpoint between Mumbai and Pune: ~18.7982, 73.3672)
	state, distM, err := engine.ProcessTelemetry(context.Background(), deviation.TelemetryPoint{
		TripID:    tripID,
		VehicleID: "V-001",
		DriverID:  "D-001",
		Latitude:  18.7982,
		Longitude: 73.3672,
		Speed:     60.0,
		Accuracy:  10.0,
		Timestamp: now,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != deviation.StateOnRoute {
		t.Fatalf("expected ON_ROUTE, got %s (dist: %.1fm)", state, distM)
	}
	if bus.CountType(events.GPSDeviationAlert) != 0 {
		t.Fatalf("expected 0 alerts for on_route point")
	}

	// 2. Invariant: Teleportation jump filter rejects impossible GPS anomalies (> 180 km/h)
	state, _, err = engine.ProcessTelemetry(context.Background(), deviation.TelemetryPoint{
		TripID:    tripID,
		VehicleID: "V-001",
		DriverID:  "D-001",
		Latitude:  21.0000, // 250 km jump in 2 seconds
		Longitude: 73.3672,
		Speed:     60.0,
		Accuracy:  10.0,
		Timestamp: now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != deviation.StateOnRoute {
		t.Fatalf("teleportation anomaly must be rejected, got %s", state)
	}

	// 3. Invariant: First valid off-route reading transitions to DEVIATING but does NOT immediately alert (requires sustained duration)
	// Shift 6.5 km off-corridor (approx 0.06 deg lat = ~6.6 km) over 400 seconds (speed ~60 km/h)
	deviatedTime1 := now.Add(400 * time.Second)
	state, distM, err = engine.ProcessTelemetry(context.Background(), deviation.TelemetryPoint{
		TripID:    tripID,
		VehicleID: "V-001",
		DriverID:  "D-001",
		Latitude:  18.8582, // ~6.6 km off-route
		Longitude: 73.3672,
		Speed:     50.0,
		Accuracy:  10.0,
		Timestamp: deviatedTime1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != deviation.StateDeviating {
		t.Fatalf("expected DEVIATING state on first off-route fix, got %s (dist: %.1fm)", state, distM)
	}
	if bus.CountType(events.GPSDeviationAlert) != 0 {
		t.Fatalf("single off-route fix must not trigger immediate alert (got %d)", bus.CountType(events.GPSDeviationAlert))
	}

	// 4. Invariant: Poor accuracy GPS point is ignored and does not advance deviation
	state, _, err = engine.ProcessTelemetry(context.Background(), deviation.TelemetryPoint{
		TripID:    tripID,
		VehicleID: "V-001",
		DriverID:  "D-001",
		Latitude:  18.8582,
		Longitude: 73.3672,
		Speed:     50.0,
		Accuracy:  120.0, // High noise (> 50m)
		Timestamp: deviatedTime1.Add(30 * time.Second),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != deviation.StateDeviating {
		t.Fatalf("poor accuracy point must not change state from DEVIATING, got %s", state)
	}
	if bus.CountType(events.GPSDeviationAlert) != 0 {
		t.Fatalf("poor accuracy point must not trigger alert")
	}

	// 5. Invariant: Sustained deviation (> 60s & >= 2 valid fixes) triggers ALERTED and generates exactly 1 event
	deviatedPointSustained := deviation.TelemetryPoint{
		TripID:    tripID,
		VehicleID: "V-001",
		DriverID:  "D-001",
		Latitude:  18.8600,
		Longitude: 73.3672,
		Speed:     50.0,
		Accuracy:  8.0,
		Timestamp: deviatedTime1.Add(75 * time.Second), // > 60s sustained
	}
	state, distM, err = engine.ProcessTelemetry(context.Background(), deviatedPointSustained)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != deviation.StateAlerted {
		t.Fatalf("expected ALERTED after sustained deviation, got %s (dist: %.1fm)", state, distM)
	}
	if bus.CountType(events.GPSDeviationAlert) != 1 {
		t.Fatalf("expected exactly 1 GPSDeviationAlert, got %d", bus.CountType(events.GPSDeviationAlert))
	}

	// 6. Invariant: 5x Telemetry Replay generates 0 duplicate alerts / events
	for i := 0; i < 5; i++ {
		st, _, err := engine.ProcessTelemetry(context.Background(), deviatedPointSustained)
		if err != nil {
			t.Fatalf("replay iteration %d failed: %v", i, err)
		}
		if st != deviation.StateAlerted {
			t.Fatalf("replay state should remain ALERTED, got %s", st)
		}
	}

	// Verify database rows and bus count
	var alertCount, outboxCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM telemetry_alerts WHERE trip_id = ?`, tripID).Scan(&alertCount)
	_ = db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = ?`, tripID).Scan(&outboxCount)

	if alertCount != 1 {
		t.Fatalf("expected exactly 1 row in telemetry_alerts, got %d", alertCount)
	}
	if outboxCount != 1 {
		t.Fatalf("expected exactly 1 row in outbox_events, got %d", outboxCount)
	}
	if bus.CountType(events.GPSDeviationAlert) != 1 {
		t.Fatalf("expected exactly 1 event published across 5x replay, got %d", bus.CountType(events.GPSDeviationAlert))
	}

	// 7. Invariant: Returning to route closes deviation
	returnPoint := deviation.TelemetryPoint{
		TripID:    tripID,
		VehicleID: "V-001",
		DriverID:  "D-001",
		Latitude:  18.7982,
		Longitude: 73.3672,
		Speed:     60.0,
		Accuracy:  10.0,
		Timestamp: deviatedTime1.Add(600 * time.Second),
	}
	state, _, err = engine.ProcessTelemetry(context.Background(), returnPoint)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != deviation.StateReturnedToRoute && state != deviation.StateOnRoute {
		t.Fatalf("expected RETURNED_TO_ROUTE or ON_ROUTE upon returning, got %s", state)
	}
}

func TestGPSDeviation_GuardsAndBindings(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	bus := &mockEventBus{}
	engine := deviation.NewEngine(db, bus, nil, nil)

	tripID := "TRIP-GUARD-001"
	seedRouteAndTrip(t, db, tripID, "completed", "tenant_beta") // Completed trip

	now := time.Now().UTC()

	// 1. Invariant: Completed trip cannot generate deviation alerts
	state, _, err := engine.ProcessTelemetry(context.Background(), deviation.TelemetryPoint{
		TripID:    tripID,
		VehicleID: "V-001",
		DriverID:  "D-001",
		Latitude:  19.5000, // Massive deviation
		Longitude: 73.3672,
		Speed:     60.0,
		Accuracy:  10.0,
		Timestamp: now,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != deviation.StateOnRoute {
		t.Fatalf("completed trip must remain unaffected, got state: %s", state)
	}
	if bus.CountType(events.GPSDeviationAlert) != 0 {
		t.Fatalf("completed trip must not emit alerts")
	}

	// 2. Invariant: Telemetry from wrong driver or vehicle is discarded
	activeTripID := "TRIP-GUARD-002"
	seedRouteAndTrip(t, db, activeTripID, "in_transit", "tenant_beta")

	state, _, err = engine.ProcessTelemetry(context.Background(), deviation.TelemetryPoint{
		TripID:    activeTripID,
		VehicleID: "V-999-IMPOSTER",
		DriverID:  "D-001",
		Latitude:  19.5000,
		Longitude: 73.3672,
		Speed:     60.0,
		Accuracy:  10.0,
		Timestamp: now,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != deviation.StateOnRoute {
		t.Fatalf("mismatched vehicle state should not change, got %s", state)
	}
	if bus.CountType(events.GPSDeviationAlert) != 0 {
		t.Fatalf("mismatched vehicle must not generate alerts")
	}
}

func TestGPSDeviation_TenantPolicyOverrides(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	reader := fuel.NewConfigReader(db)
	bus := &mockEventBus{}
	engine := deviation.NewEngine(db, bus, reader, nil)

	tenantID := "tenant_strict"
	tripID := "TRIP-STRICT-001"
	seedRouteAndTrip(t, db, tripID, "in_transit", tenantID)

	// Set strict tenant threshold: 2.0 km deviation (2000m) and 20s sustained
	_, _ = db.Exec(`
		INSERT INTO company_config (tenant_id, key, value)
		VALUES (?, 'deviation.max_distance_meters', '2000.0')`, tenantID)
	_, _ = db.Exec(`
		INSERT INTO company_config (tenant_id, key, value)
		VALUES (?, 'deviation.sustained_duration_seconds', '20.0')`, tenantID)

	now := time.Now().UTC()

	// Initial on-route point
	_, _, _ = engine.ProcessTelemetry(context.Background(), deviation.TelemetryPoint{
		TripID:    tripID,
		TenantID:  tenantID,
		VehicleID: "V-001",
		DriverID:  "D-001",
		Latitude:  18.7982,
		Longitude: 73.3672,
		Speed:     50.0,
		Accuracy:  10.0,
		Timestamp: now,
	})

	// Deviated by ~2.5 km (0.023 deg lat) - below default 5km, but ABOVE strict 2km
	t1 := now.Add(200 * time.Second)
	state1, distM1, _ := engine.ProcessTelemetry(context.Background(), deviation.TelemetryPoint{
		TripID:    tripID,
		TenantID:  tenantID,
		VehicleID: "V-001",
		DriverID:  "D-001",
		Latitude:  18.8212, // ~2.5 km
		Longitude: 73.3672,
		Speed:     50.0,
		Accuracy:  10.0,
		Timestamp: t1,
	})
	if state1 != deviation.StateDeviating {
		t.Fatalf("expected DEVIATING with strict tenant policy, got %s (dist: %.1fm)", state1, distM1)
	}

	// Sustained for 25s (> 20s policy)
	state2, distM2, _ := engine.ProcessTelemetry(context.Background(), deviation.TelemetryPoint{
		TripID:    tripID,
		TenantID:  tenantID,
		VehicleID: "V-001",
		DriverID:  "D-001",
		Latitude:  18.8215,
		Longitude: 73.3672,
		Speed:     50.0,
		Accuracy:  10.0,
		Timestamp: t1.Add(25 * time.Second),
	})
	if state2 != deviation.StateAlerted {
		t.Fatalf("expected ALERTED with strict tenant policy, got %s (dist: %.1fm)", state2, distM2)
	}
	if bus.CountType(events.GPSDeviationAlert) != 1 {
		t.Fatalf("expected 1 alert under strict tenant policy, got %d", bus.CountType(events.GPSDeviationAlert))
	}
}

func TestGPSDeviation_CorridorGeometry(t *testing.T) {
	// Mumbai to Pune segment
	corridor := &deviation.RouteCorridor{
		RouteID:    "r1",
		Source:     "Mumbai",
		Dest:       "Pune",
		DistanceKM: 150.0,
		Waypoints: []geodomain.Point{
			{Lat: 19.0760, Lng: 72.8777}, // Mumbai
			{Lat: 18.5204, Lng: 73.8567}, // Pune
		},
	}

	// Test midpoint on the line
	midLat := (19.0760 + 18.5204) / 2
	midLng := (72.8777 + 73.8567) / 2
	distM := corridor.DistanceToPoint(midLat, midLng)
	if distM > 50.0 {
		t.Fatalf("expected point on segment to have near-zero distance, got %.2f metres", distM)
	}

	// Test point shifted 10 km perpendicularly
	// 0.1 deg lat is ~11.1 km
	distDeviated := corridor.DistanceToPoint(midLat+0.1, midLng)
	if distDeviated < 8000.0 || distDeviated > 15000.0 {
		t.Fatalf("expected ~11km distance, got %.2f metres", distDeviated)
	}
}
