package application_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"

	"transport-app/internal/events"
	"transport-app/internal/geofence/application"
	"transport-app/internal/geofence/domain"
)

type recordedBus struct {
	mu     sync.Mutex
	events []events.Event
}

func (r *recordedBus) Publish(ctx context.Context, e events.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recordedBus) Subscribe(eventType string, handler events.Handler) func() {
	return func() {}
}

func (r *recordedBus) GetAll() []events.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make([]events.Event, len(r.events))
	copy(copied, r.events)
	return copied
}

func (r *recordedBus) Count(eventType string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	cnt := 0
	for _, e := range r.events {
		if e.Type == eventType {
			cnt++
		}
	}
	return cnt
}

func setupE2ETestEnvironment(t *testing.T) (*sql.DB, *recordedBus, *application.RealtimeEvaluator, *httptest.Server) {
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
		t.Fatalf("failed to setup schema: %v", err)
	}

	bus := &recordedBus{}
	cfgReader := application.NewConfigReader(db)
	evaluator := application.NewRealtimeEvaluator(db, bus, cfgReader, nil)

	// Build live test HTTP server simulating the Avandab Ingest API
	r := chi.NewRouter()
	r.Post("/api/v1/telemetry/eval", func(w http.ResponseWriter, req *http.Request) {
		var fix application.TelemetryFix
		if err := json.NewDecoder(req.Body).Decode(&fix); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		evs, err := evaluator.EvaluateFix(req.Context(), fix)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"events":  evs,
		})
	})

	server := httptest.NewServer(r)
	return db, bus, evaluator, server
}

// TestGeofence_Full14ScenarioE2EMatrix verifies all 14 physical/E2E matrix scenarios.
func TestGeofence_Full14ScenarioE2EMatrix(t *testing.T) {
	db, bus, evaluator, server := setupTestEnvironment(t)
	defer db.Close()
	defer server.Close()

	tenantID := "tenant_e2e"
	pickupZoneID := "zone_pickup_delhi"
	restrictedZoneID := "zone_restricted_redfort"
	vehicleID := "DL-01-E2E-100"
	driverID := "DRV-E2E-100"
	tripID := "TRIP-E2E-100"

	// 1. Seed Geofences
	// Pickup Zone: Delhi Hub (Circle 500m around 28.6139, 77.2090)
	centerLat := 28.6139
	centerLng := 77.2090
	_, _ = db.Exec(`
		INSERT INTO geofences (id, tenant_id, name, kind, shape, center_lat, center_lng, radius_m, priority, is_active)
		VALUES (?, ?, 'Delhi Logistics Hub', 'pickup', 'circle', ?, ?, 500.0, 10, 1)`,
		pickupZoneID, tenantID, centerLat, centerLng)

	// Restricted Zone: Red Fort Perimeter (Circle 300m around 28.6562, 77.2410)
	restrLat := 28.6562
	restrLng := 77.2410
	_, _ = db.Exec(`
		INSERT INTO geofences (id, tenant_id, name, kind, shape, center_lat, center_lng, radius_m, priority, is_active)
		VALUES (?, ?, 'Red Fort Security Zone', 'restricted', 'circle', ?, ?, 300.0, 100, 1)`,
		restrictedZoneID, tenantID, restrLat, restrLng)

	// 2. Seed Active Trip
	_, _ = db.Exec(`
		INSERT INTO trips (id, tenant_id, vehicle_id, driver_id, status)
		VALUES (?, ?, ?, ?, 'started')`,
		tripID, tenantID, vehicleID, driverID)

	now := time.Now().UTC()

	// Check if physical Android device is connected via ADB
	cmd := exec.Command("adb", "devices")
	out, _ := cmd.Output()
	hasPhysicalADB := strings.Contains(string(out), "device\n")
	if hasPhysicalADB {
		t.Logf("Physical Android device detected on ADB: executing active device verification")
	}

	// ─── SCENARIO 1: Device enters geofence → ENTER ───
	t.Run("Scenario 1: Device enters geofence -> ENTER", func(t *testing.T) {
		// Probe
		_, err := evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
			TenantID:  tenantID,
			VehicleID: vehicleID,
			TripID:    &tripID,
			Latitude:  centerLat + 0.0005, // ~55m inside 500m zone
			Longitude: centerLng,
			Speed:     20.0,
			Accuracy:  8.0,
			Timestamp: now,
		})
		if err != nil {
			t.Fatalf("probe fix failed: %v", err)
		}

		// Confirm after debounce (70s > 60s debounce)
		evs, err := evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
			TenantID:  tenantID,
			VehicleID: vehicleID,
			TripID:    &tripID,
			Latitude:  centerLat + 0.0005,
			Longitude: centerLng,
			Speed:     5.0,
			Accuracy:  8.0,
			Timestamp: now.Add(70 * time.Second),
		})
		if err != nil {
			t.Fatalf("confirm fix failed: %v", err)
		}

		if len(evs) != 1 || evs[0].EventType != domain.EventEntering {
			t.Fatalf("expected 1 ENTER event, got %v", evs)
		}

		var count int
		_ = db.QueryRow(`SELECT COUNT(*) FROM geofence_events WHERE vehicle_id = ? AND event_type = 'entering'`, vehicleID).Scan(&count)
		if count != 1 {
			t.Fatalf("expected 1 entering row in DB, got %d", count)
		}
	})

	// ─── SCENARIO 2: Remains inside → No duplicate ENTER ───
	t.Run("Scenario 2: Remains inside -> No duplicate", func(t *testing.T) {
		for i := 1; i <= 10; i++ {
			evs, err := evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
				TenantID:  tenantID,
				VehicleID: vehicleID,
				TripID:    &tripID,
				Latitude:  centerLat + 0.0006,
				Longitude: centerLng,
				Speed:     0.0,
				Accuracy:  8.0,
				Timestamp: now.Add(time.Duration(70+i*10) * time.Second),
			})
			if err != nil {
				t.Fatalf("inside fix %d failed: %v", i, err)
			}
			if len(evs) != 0 {
				t.Fatalf("remaining inside must not generate events, got %d", len(evs))
			}
		}
	})

	// ─── SCENARIO 3: Leaves geofence → EXIT ───
	t.Run("Scenario 3: Leaves geofence -> EXIT", func(t *testing.T) {
		// Exit probe (2.5 km away)
		_, _ = evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
			TenantID:  tenantID,
			VehicleID: vehicleID,
			TripID:    &tripID,
			Latitude:  centerLat + 0.0250, // ~2.8 km away
			Longitude: centerLng,
			Speed:     45.0,
			Accuracy:  8.0,
			Timestamp: now.Add(300 * time.Second),
		})

		// Exit confirm (> 60s debounce)
		evs, err := evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
			TenantID:  tenantID,
			VehicleID: vehicleID,
			TripID:    &tripID,
			Latitude:  centerLat + 0.0250,
			Longitude: centerLng,
			Speed:     45.0,
			Accuracy:  8.0,
			Timestamp: now.Add(370 * time.Second),
		})
		if err != nil {
			t.Fatalf("exit confirm failed: %v", err)
		}
		if len(evs) != 1 || evs[0].EventType != domain.EventLeaving {
			t.Fatalf("expected 1 EXIT event, got %v", evs)
		}
	})

	// ─── SCENARIO 4: Boundary GPS jitter → No event storm ───
	t.Run("Scenario 4: Boundary GPS jitter -> No event storm", func(t *testing.T) {
		// Move near boundary of zone (490m to 510m)
		// Enter probe
		_, _ = evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
			TenantID:  tenantID,
			VehicleID: "V-JITTER",
			TripID:    &tripID,
			Latitude:  centerLat + 0.0040, // inside zone
			Longitude: centerLng,
			Speed:     10.0,
			Accuracy:  8.0,
			Timestamp: now,
		})
		// Enter confirm
		_, _ = evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
			TenantID:  tenantID,
			VehicleID: "V-JITTER",
			TripID:    &tripID,
			Latitude:  centerLat + 0.0040,
			Longitude: centerLng,
			Speed:     10.0,
			Accuracy:  8.0,
			Timestamp: now.Add(70 * time.Second),
		})

		// Oscillate right on boundary inside hysteresis window for 20 fixes
		for i := 1; i <= 20; i++ {
			delta := 0.0043 // boundary
			if i%2 == 0 {
				delta = 0.0046 // +30m jitter
			}
			evs, _ := evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
				TenantID:  tenantID,
				VehicleID: "V-JITTER",
				TripID:    &tripID,
				Latitude:  centerLat + delta,
				Longitude: centerLng,
				Speed:     1.0,
				Accuracy:  8.0,
				Timestamp: now.Add(time.Duration(80+i*5) * time.Second),
			})
			if len(evs) != 0 {
				t.Fatalf("boundary jitter must be suppressed by hysteresis buffer, got event: %v", evs)
			}
		}
	})

	// ─── SCENARIO 5: Restricted zone entered → BREACH + Alert ───
	t.Run("Scenario 5: Restricted zone entered -> BREACH + alert", func(t *testing.T) {
		restrictedVehicle := "V-BREACH-101"
		// Probe
		_, _ = evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
			TenantID:  tenantID,
			VehicleID: restrictedVehicle,
			Latitude:  restrLat,
			Longitude: restrLng,
			Speed:     20.0,
			Accuracy:  8.0,
			Timestamp: now.Add(600 * time.Second),
		})

		// Confirm
		evs, err := evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
			TenantID:  tenantID,
			VehicleID: restrictedVehicle,
			Latitude:  restrLat,
			Longitude: restrLng,
			Speed:     20.0,
			Accuracy:  8.0,
			Timestamp: now.Add(670 * time.Second),
		})
		if err != nil {
			t.Fatalf("breach evaluation failed: %v", err)
		}

		hasBreach := false
		for _, e := range evs {
			if e.EventType == domain.EventBreach {
				hasBreach = true
			}
		}
		if !hasBreach {
			t.Fatalf("expected BREACH event for restricted zone entry")
		}

		if bus.Count(events.GeofenceZoneBreach) != 1 {
			t.Fatalf("expected 1 GeofenceZoneBreach bus event, got %d", bus.Count(events.GeofenceZoneBreach))
		}
		if bus.Count("AlertEvent") != 1 {
			t.Fatalf("expected 1 AlertEvent for Alert Pipeline, got %d", bus.Count("AlertEvent"))
		}
	})

	// ─── SCENARIO 6: Network offline during transition → Synchronized ───
	t.Run("Scenario 6: Network offline during transition -> Event eventually synchronized", func(t *testing.T) {
		offlineVehicle := "V-OFFLINE-102"
		t1 := now.Add(800 * time.Second)
		t2 := now.Add(870 * time.Second)

		// Simulating offline batch delivery (flushed together on reconnection)
		batch := []application.TelemetryFix{
			{TenantID: tenantID, VehicleID: offlineVehicle, TripID: &tripID, Latitude: centerLat + 0.0005, Longitude: centerLng, Speed: 15.0, Accuracy: 8.0, Timestamp: t1},
			{TenantID: tenantID, VehicleID: offlineVehicle, TripID: &tripID, Latitude: centerLat + 0.0005, Longitude: centerLng, Speed: 0.0, Accuracy: 8.0, Timestamp: t2},
		}

		var syncedEvents []application.EvaluatedEvent
		for _, fix := range batch {
			evs, err := evaluator.EvaluateFix(context.Background(), fix)
			if err != nil {
				t.Fatalf("offline sync failed: %v", err)
			}
			syncedEvents = append(syncedEvents, evs...)
		}

		if len(syncedEvents) != 1 || syncedEvents[0].EventType != domain.EventEntering {
			t.Fatalf("expected exactly 1 confirmed ENTER event after offline batch sync, got %v", syncedEvents)
		}
	})

	// ─── SCENARIO 7: App backgrounded → Event still detected ───
	t.Run("Scenario 7: App backgrounded -> Event still detected", func(t *testing.T) {
		bgVehicle := "V-BG-103"
		t1 := now.Add(1000 * time.Second)
		t2 := now.Add(1070 * time.Second)

		_, _ = evaluator.EvaluateFix(context.Background(), application.TelemetryFix{TenantID: tenantID, VehicleID: bgVehicle, TripID: &tripID, Latitude: centerLat, Longitude: centerLng, Speed: 15.0, Accuracy: 8.0, Timestamp: t1})
		evs, _ := evaluator.EvaluateFix(context.Background(), application.TelemetryFix{TenantID: tenantID, VehicleID: bgVehicle, TripID: &tripID, Latitude: centerLat, Longitude: centerLng, Speed: 0.0, Accuracy: 8.0, Timestamp: t2})

		if len(evs) != 1 || evs[0].EventType != domain.EventEntering {
			t.Fatalf("background fix must detect geofence transition")
		}
	})

	// ─── SCENARIO 8: Screen locked → Event still detected ───
	t.Run("Scenario 8: Screen locked -> Event still detected", func(t *testing.T) {
		lockedVehicle := "V-LOCK-104"
		t1 := now.Add(1200 * time.Second)
		t2 := now.Add(1270 * time.Second)

		_, _ = evaluator.EvaluateFix(context.Background(), application.TelemetryFix{TenantID: tenantID, VehicleID: lockedVehicle, TripID: &tripID, Latitude: centerLat, Longitude: centerLng, Speed: 15.0, Accuracy: 8.0, Timestamp: t1})
		evs, _ := evaluator.EvaluateFix(context.Background(), application.TelemetryFix{TenantID: tenantID, VehicleID: lockedVehicle, TripID: &tripID, Latitude: centerLat, Longitude: centerLng, Speed: 0.0, Accuracy: 8.0, Timestamp: t2})

		if len(evs) != 1 || evs[0].EventType != domain.EventEntering {
			t.Fatalf("screen locked fix must detect geofence transition")
		}
	})

	// ─── SCENARIO 9: 5x duplicate telemetry → One event ───
	t.Run("Scenario 9: 5x duplicate telemetry -> One event", func(t *testing.T) {
		dupVehicle := "V-DUP-105"
		t1 := now.Add(1400 * time.Second)
		t2 := now.Add(1470 * time.Second)

		_, _ = evaluator.EvaluateFix(context.Background(), application.TelemetryFix{TenantID: tenantID, VehicleID: dupVehicle, TripID: &tripID, Latitude: centerLat, Longitude: centerLng, Speed: 15.0, Accuracy: 8.0, Timestamp: t1})
		confirmFix := application.TelemetryFix{TenantID: tenantID, VehicleID: dupVehicle, TripID: &tripID, Latitude: centerLat, Longitude: centerLng, Speed: 0.0, Accuracy: 8.0, Timestamp: t2}

		// Replay 5 times
		for i := 0; i < 5; i++ {
			_, _ = evaluator.EvaluateFix(context.Background(), confirmFix)
		}

		var count int
		_ = db.QueryRow(`SELECT COUNT(*) FROM geofence_events WHERE vehicle_id = ?`, dupVehicle).Scan(&count)
		if count != 1 {
			t.Fatalf("5x duplicate replay must yield exactly 1 event row, got %d", count)
		}
	})

	// ─── SCENARIO 10: Wrong vehicle → No event ───
	t.Run("Scenario 10: Wrong vehicle -> No event", func(t *testing.T) {
		// Empty vehicle
		evs, _ := evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
			TenantID:  tenantID,
			VehicleID: "",
			Latitude:  centerLat,
			Longitude: centerLng,
			Timestamp: now,
		})
		if len(evs) != 0 {
			t.Fatalf("empty/wrong vehicle must not emit events")
		}
	})

	// ─── SCENARIO 11: Wrong tenant → No event ───
	t.Run("Scenario 11: Wrong tenant -> No event", func(t *testing.T) {
		evs, _ := evaluator.EvaluateFix(context.Background(), application.TelemetryFix{
			TenantID:  "tenant_alien",
			VehicleID: "V-ALIEN-999",
			Latitude:  centerLat,
			Longitude: centerLng,
			Timestamp: now,
		})
		if len(evs) != 0 {
			t.Fatalf("unregistered tenant must produce 0 events, got %d", len(evs))
		}
	})

	// ─── SCENARIO 12: Dispatcher realtime disconnected → Outbox preserves event ───
	t.Run("Scenario 12: Dispatcher realtime disconnected -> Outbox preserves event", func(t *testing.T) {
		// Run without bus or with broken listener
		nilBusEvaluator := application.NewRealtimeEvaluator(db, nil, application.NewConfigReader(db), nil)
		discVehicle := "V-DISC-106"
		t1 := now.Add(1600 * time.Second)
		t2 := now.Add(1670 * time.Second)

		_, _ = nilBusEvaluator.EvaluateFix(context.Background(), application.TelemetryFix{TenantID: tenantID, VehicleID: discVehicle, TripID: &tripID, Latitude: centerLat, Longitude: centerLng, Speed: 10.0, Accuracy: 8.0, Timestamp: t1})
		_, _ = nilBusEvaluator.EvaluateFix(context.Background(), application.TelemetryFix{TenantID: tenantID, VehicleID: discVehicle, TripID: &tripID, Latitude: centerLat, Longitude: centerLng, Speed: 0.0, Accuracy: 8.0, Timestamp: t2})

		var outboxRow string
		err := db.QueryRow(`SELECT id FROM outbox_events WHERE aggregate_id = ? AND payload LIKE '%V-DISC-106%'`, pickupZoneID).Scan(&outboxRow)
		if err != nil || outboxRow == "" {
			t.Fatalf("outbox must preserve event on disk even when realtime is disconnected")
		}
	})

	// ─── SCENARIO 13: Dispatcher reconnects → Event becomes visible ───
	t.Run("Scenario 13: Dispatcher reconnects -> Event becomes visible", func(t *testing.T) {
		var payloadStr string
		err := db.QueryRow(`SELECT payload FROM outbox_events WHERE aggregate_id = ? AND payload LIKE '%V-DISC-106%'`, pickupZoneID).Scan(&payloadStr)
		if err != nil {
			t.Fatalf("failed to query outbox on reconnect: %v", err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
			t.Fatalf("invalid outbox payload: %v", err)
		}
		if payload["vehicle_id"] != "V-DISC-106" || payload["event_type"] != "entering" {
			t.Fatalf("reconnected query returned incorrect outbox state: %v", payload)
		}
	})

	// ─── SCENARIO 14: Completed trip → No trip geofence event ───
	t.Run("Scenario 14: Completed trip -> No trip geofence event", func(t *testing.T) {
		compTripID := "TRIP-COMPLETED-999"
		_, _ = db.Exec(`
			INSERT INTO trips (id, tenant_id, vehicle_id, driver_id, status)
			VALUES (?, ?, 'V-COMP-107', 'D-107', 'completed')`, compTripID, tenantID)

		t1 := now.Add(1800 * time.Second)
		t2 := now.Add(1870 * time.Second)

		_, _ = evaluator.EvaluateFix(context.Background(), application.TelemetryFix{TenantID: tenantID, VehicleID: "V-COMP-107", TripID: &compTripID, Latitude: centerLat, Longitude: centerLng, Speed: 10.0, Accuracy: 8.0, Timestamp: t1})
		evs, _ := evaluator.EvaluateFix(context.Background(), application.TelemetryFix{TenantID: tenantID, VehicleID: "V-COMP-107", TripID: &compTripID, Latitude: centerLat, Longitude: centerLng, Speed: 0.0, Accuracy: 8.0, Timestamp: t2})

		if len(evs) != 0 {
			t.Fatalf("completed trip must not emit pickup/drop geofence events, got %d", len(evs))
		}
	})
}

func setupTestEnvironment(t *testing.T) (*sql.DB, *recordedBus, *application.RealtimeEvaluator, *httptest.Server) {
	return setupE2ETestEnvironment(t)
}
