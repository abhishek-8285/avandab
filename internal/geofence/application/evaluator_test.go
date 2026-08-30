package application_test

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"transport-app/internal/events"
	"transport-app/internal/geofence/application"
	"transport-app/internal/geofence/domain"
)

type mockBus struct {
	mu     sync.Mutex
	events []events.Event
}

func (m *mockBus) Publish(ctx context.Context, e events.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}

func (m *mockBus) Subscribe(eventType string, handler events.Handler) func() {
	return func() {}
}

func (m *mockBus) CountType(eventType string) int {
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

func setupEvaluatorDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory: %v", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS geofences (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		name TEXT NOT NULL,
		kind TEXT NOT NULL,
		shape TEXT NOT NULL,
		center_lat REAL,
		center_lng REAL,
		radius_m REAL,
		polygon TEXT,
		route_name TEXT,
		priority INTEGER NOT NULL DEFAULT 0,
		is_active INTEGER NOT NULL DEFAULT 1,
		created_by TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS vehicle_geofences (
		vehicle_id TEXT NOT NULL,
		geofence_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL DEFAULT '1',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (vehicle_id, geofence_id)
	);

	CREATE TABLE IF NOT EXISTS engine_state (
		vehicle_id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		state TEXT NOT NULL DEFAULT 'outside',
		trip_id TEXT,
		geofence_id TEXT,
		zone_kind TEXT,
		zone_entered_at DATETIME,
		confirmed_at DATETIME,
		exit_started_at DATETIME,
		last_fix_at DATETIME,
		last_lat REAL,
		last_lng REAL,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS geofence_events (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		vehicle_id TEXT,
		trip_id TEXT,
		geofence_id TEXT,
		zone_kind TEXT,
		event_type TEXT NOT NULL,
		alert_type TEXT,
		severity TEXT,
		latitude REAL,
		longitude REAL,
		details TEXT,
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

	CREATE TABLE IF NOT EXISTS trips (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		route_id TEXT,
		vehicle_id TEXT,
		driver_id TEXT,
		status TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS routes (
		id TEXT PRIMARY KEY,
		source TEXT NOT NULL,
		destination TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS company_config (
		tenant_id TEXT NOT NULL,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (tenant_id, key)
	);
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}
	return db
}

func TestRealtimeEvaluator_EnterExitAndReplayInvariants(t *testing.T) {
	db := setupEvaluatorDB(t)
	defer db.Close()

	bus := &mockBus{}
	cfgReader := application.NewConfigReader(db)
	evaluator := application.NewRealtimeEvaluator(db, bus, cfgReader, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	tenantID := "tenant_t1"
	zoneID := "zone_pickup_mumbai"

	// 1. Seed Pickup Geofence: Mumbai Hub (Circle 500m radius)
	centerLat := 19.0760
	centerLng := 72.8777
	_, _ = db.Exec(`
		INSERT INTO geofences (id, tenant_id, name, kind, shape, center_lat, center_lng, radius_m, priority, is_active)
		VALUES (?, ?, 'Mumbai Hub Pickup', 'pickup', 'circle', ?, ?, 500.0, 10, 1)`,
		zoneID, tenantID, centerLat, centerLng)

	tripID := "TRIP-GEOFENCE-001"
	_, _ = db.Exec(`
		INSERT INTO trips (id, tenant_id, vehicle_id, driver_id, status)
		VALUES (?, ?, 'V-GEOFENCE-1', 'D-1', 'started')`,
		tripID, tenantID)

	now := time.Now().UTC()

	// 2. Fix 1: Vehicle outside zone (2000m away)
	outsideFix := application.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: "V-GEOFENCE-1",
		TripID:    &tripID,
		Latitude:  19.0950,
		Longitude: 72.8777,
		Speed:     40.0,
		Accuracy:  10.0,
		Timestamp: now,
	}
	evs, err := evaluator.EvaluateFix(context.Background(), outsideFix)
	if err != nil {
		t.Fatalf("evaluate fix outside: %v", err)
	}
	if len(evs) != 0 {
		t.Fatalf("expected 0 events when outside, got %d", len(evs))
	}

	// 3. Fix 2: Vehicle ENTERS geofence (~2000m away reached in 200s, speed ~37 km/h)
	// First fix triggers probe (StateEntering)
	enterProbeFix := application.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: "V-GEOFENCE-1",
		TripID:    &tripID,
		Latitude:  centerLat + 0.0005, // ~55 metres from center
		Longitude: centerLng,
		Speed:     15.0,
		Accuracy:  8.0,
		Timestamp: now.Add(200 * time.Second),
	}
	evs, _ = evaluator.EvaluateFix(context.Background(), enterProbeFix)
	if len(evs) != 0 {
		t.Fatalf("first enter fix is probe (debounce pending), expected 0 events, got %d", len(evs))
	}

	// 4. Fix 3: Vehicle stays inside zone past debounce (60s default) -> generates exactly 1 ENTER event
	enterConfirmFix := application.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: "V-GEOFENCE-1",
		TripID:    &tripID,
		Latitude:  centerLat + 0.0005,
		Longitude: centerLng,
		Speed:     0.0,
		Accuracy:  8.0,
		Timestamp: now.Add(270 * time.Second), // 70s after probe (> 60s debounce)
	}
	evs, _ = evaluator.EvaluateFix(context.Background(), enterConfirmFix)
	if len(evs) != 1 {
		t.Fatalf("expected 1 confirmed ENTER event, got %d", len(evs))
	}
	if evs[0].EventType != domain.EventEntering {
		t.Fatalf("expected event_type 'entering', got '%s'", evs[0].EventType)
	}
	if bus.CountType("geofence.zone_entering") != 1 {
		t.Fatalf("expected 1 bus publication for zone_entering, got %d", bus.CountType("geofence.zone_entering"))
	}

	// 5. Invariant: 5x Telemetry Replay of same fix produces 0 duplicate events/alerts
	for i := 0; i < 5; i++ {
		_, _ = evaluator.EvaluateFix(context.Background(), enterConfirmFix)
	}
	var geofenceEventCount, outboxCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM geofence_events WHERE vehicle_id = ?`, "V-GEOFENCE-1").Scan(&geofenceEventCount)
	_ = db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = ?`, zoneID).Scan(&outboxCount)
	if geofenceEventCount != 1 {
		t.Fatalf("5x replay must produce exactly 1 geofence_event row, got %d", geofenceEventCount)
	}
	if outboxCount != 1 {
		t.Fatalf("5x replay must produce exactly 1 outbox_event row, got %d", outboxCount)
	}
	if bus.CountType("geofence.zone_entering") != 1 {
		t.Fatalf("5x replay must produce exactly 1 bus event, got %d", bus.CountType("geofence.zone_entering"))
	}

	// 6. Invariant: Boundary GPS jitter does not oscillate state
	// Fix near boundary (490m vs 510m with 50m buffer and 30m hysteresis)
	jitterFix := application.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: "V-GEOFENCE-1",
		TripID:    &tripID,
		Latitude:  centerLat + 0.0042, // ~465m from center (near boundary)
		Longitude: centerLng,
		Speed:     10.0,
		Accuracy:  10.0,
		Timestamp: now.Add(290 * time.Second),
	}
	evs, _ = evaluator.EvaluateFix(context.Background(), jitterFix)
	if len(evs) != 0 {
		t.Fatalf("boundary jitter inside hysteresis buffer must not create exit/enter events, got %d", len(evs))
	}

	// 7. Fix 4: Vehicle LEAVES geofence (moves 2500m away in 200s, speed ~45 km/h)
	exitProbeFix := application.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: "V-GEOFENCE-1",
		TripID:    &tripID,
		Latitude:  19.1000, // 2.6 km away
		Longitude: 72.8777,
		Speed:     50.0,
		Accuracy:  10.0,
		Timestamp: now.Add(500 * time.Second),
	}
	evs, _ = evaluator.EvaluateFix(context.Background(), exitProbeFix)
	if len(evs) != 0 {
		t.Fatalf("exit probe must debounce, expected 0 events, got %d", len(evs))
	}

	exitConfirmFix := application.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: "V-GEOFENCE-1",
		TripID:    &tripID,
		Latitude:  19.1000,
		Longitude: 72.8777,
		Speed:     50.0,
		Accuracy:  10.0,
		Timestamp: now.Add(570 * time.Second), // 70s after exit probe (> 60s debounce)
	}
	evs, _ = evaluator.EvaluateFix(context.Background(), exitConfirmFix)
	if len(evs) != 1 {
		t.Fatalf("expected 1 confirmed EXIT event, got %d", len(evs))
	}
	if evs[0].EventType != domain.EventLeaving {
		t.Fatalf("expected event_type 'leaving', got '%s'", evs[0].EventType)
	}
	if bus.CountType("geofence.zone_leaving") != 1 {
		t.Fatalf("expected 1 bus publication for zone_leaving, got %d", bus.CountType("geofence.zone_leaving"))
	}
}

func TestRealtimeEvaluator_RestrictedZoneBreachAndGuards(t *testing.T) {
	db := setupEvaluatorDB(t)
	defer db.Close()

	bus := &mockBus{}
	cfgReader := application.NewConfigReader(db)
	evaluator := application.NewRealtimeEvaluator(db, bus, cfgReader, nil)

	tenantID := "tenant_security"
	restrictedZoneID := "zone_restricted_007"

	// 1. Seed Restricted Zone
	centerLat := 28.6139
	centerLng := 77.2090
	_, _ = db.Exec(`
		INSERT INTO geofences (id, tenant_id, name, kind, shape, center_lat, center_lng, radius_m, priority, is_active)
		VALUES (?, ?, 'High Security Zone', 'restricted', 'circle', ?, ?, 300.0, 100, 1)`,
		restrictedZoneID, tenantID, centerLat, centerLng)

	now := time.Now().UTC()

	// 2. Invariant: Teleportation jump fix is ignored
	_, _ = evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: "V-TELEPORT-TEST",
		Latitude:  28.0000,
		Longitude: 77.2090,
		Speed:     60.0,
		Accuracy:  10.0,
		Timestamp: now,
	})
	teleportFix := application.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: "V-TELEPORT-TEST",
		Latitude:  35.0000, // 700 km jump in 2s
		Longitude: 77.2090,
		Speed:     60.0,
		Accuracy:  10.0,
		Timestamp: now.Add(2 * time.Second),
	}
	evs, _ := evaluator.EvaluateFix(context.Background(), teleportFix)
	if len(evs) != 0 {
		t.Fatalf("teleportation anomaly must be rejected")
	}

	// 3. Invariant: Poor accuracy fix is ignored
	poorAccuracyFix := application.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: "V-RESTRICTED-1",
		Latitude:  centerLat,
		Longitude: centerLng,
		Speed:     20.0,
		Accuracy:  150.0, // Poor accuracy (> 50m)
		Timestamp: now.Add(10 * time.Second),
	}
	evs, _ = evaluator.EvaluateFix(context.Background(), poorAccuracyFix)
	if len(evs) != 0 {
		t.Fatalf("poor accuracy fix must be rejected")
	}

	// 4. Invariant: Entering restricted zone produces BREACH event and AlertEvent
	// Probe
	_, _ = evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: "V-RESTRICTED-1",
		Latitude:  centerLat,
		Longitude: centerLng,
		Speed:     20.0,
		Accuracy:  10.0,
		Timestamp: now.Add(20 * time.Second),
	})

	// Confirm after debounce (60s)
	evs, err := evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
		TenantID:  tenantID,
		VehicleID: "V-RESTRICTED-1",
		Latitude:  centerLat,
		Longitude: centerLng,
		Speed:     20.0,
		Accuracy:  10.0,
		Timestamp: now.Add(90 * time.Second),
	})
	if err != nil {
		t.Fatalf("evaluate breach: %v", err)
	}

	hasBreach := false
	for _, e := range evs {
		if e.EventType == domain.EventBreach {
			hasBreach = true
		}
	}
	if !hasBreach {
		t.Fatalf("expected restricted zone entry to emit EventBreach")
	}

	if bus.CountType(events.GeofenceZoneBreach) != 1 {
		t.Fatalf("expected exactly 1 GeofenceZoneBreach event, got %d", bus.CountType(events.GeofenceZoneBreach))
	}
	if bus.CountType("AlertEvent") != 1 {
		t.Fatalf("expected exactly 1 AlertEvent dispatched to Alert Engine, got %d", bus.CountType("AlertEvent"))
	}
}

func TestRealtimeEvaluator_CompletedTripAndTenantIsolation(t *testing.T) {
	db := setupEvaluatorDB(t)
	defer db.Close()

	bus := &mockBus{}
	cfgReader := application.NewConfigReader(db)
	evaluator := application.NewRealtimeEvaluator(db, bus, cfgReader, nil)

	// Seed completed trip in tenant A
	completedTripID := "TRIP-COMPLETED-99"
	_, _ = db.Exec(`
		INSERT INTO trips (id, tenant_id, vehicle_id, driver_id, status)
		VALUES (?, 'tenant_A', 'V-COMPLETED-1', 'D-1', 'completed')`, completedTripID)

	// Seed pickup geofence in tenant A
	centerLat := 19.0760
	centerLng := 72.8777
	_, _ = db.Exec(`
		INSERT INTO geofences (id, tenant_id, name, kind, shape, center_lat, center_lng, radius_m, priority, is_active)
		VALUES ('zone_pickup_A', 'tenant_A', 'Tenant A Pickup', 'pickup', 'circle', ?, ?, 500.0, 10, 1)`,
		centerLat, centerLng)

	now := time.Now().UTC()

	// 1. Invariant: Completed trip cannot generate pickup/drop geofence alerts
	evs, _ := evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
		TenantID:  "tenant_A",
		VehicleID: "V-COMPLETED-1",
		TripID:    &completedTripID,
		Latitude:  centerLat,
		Longitude: centerLng,
		Speed:     10.0,
		Accuracy:  10.0,
		Timestamp: now,
	})
	if len(evs) != 0 {
		t.Fatalf("completed trip must not generate pickup geofence events, got %d", len(evs))
	}

	// 2. Invariant: Tenant isolation — vehicle in tenant B cannot trigger tenant A's geofences
	evs, _ = evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
		TenantID:  "tenant_B",
		VehicleID: "V-TENANT-B-1",
		Latitude:  centerLat,
		Longitude: centerLng,
		Speed:     10.0,
		Accuracy:  10.0,
		Timestamp: now,
	})
	if len(evs) != 0 {
		t.Fatalf("cross-tenant geofence trigger must be rejected, got %d events", len(evs))
	}
}
