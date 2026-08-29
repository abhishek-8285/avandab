package test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/config"
	"transport-app/internal/eta"
	"transport-app/internal/events"
	"transport-app/internal/handlers"
	"transport-app/internal/middleware"
	"transport-app/internal/realtime"
	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/uow"
	"transport-app/internal/telemetry"
	tripapp "transport-app/internal/trip/application"
	"transport-app/internal/trip/domain/aggregate"
)

func newPhase3DB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_p3_val_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// 1. Map & Tracking (3A) Checkpoint:
// Marker states: running, stopped, no_signal, maintenance_due (override priority)
func TestPhase3_3A_MarkerStatesAndPriority(t *testing.T) {
	db := newPhase3DB(t)

	// Seed vehicles with various states
	// V1: running (speed 60, recent)
	// V2: stopped (speed 0, recent)
	// V3: no_signal (stale > 15m)
	// V4: maintenance_due + speed 80 (maintenance_due MUST override running)
	_, err := db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, maintenance_due, tenant_id)
		VALUES
		('v-run', 'MH-01-RUN', 'RUN', 'truck', 10, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), NULL, '1'),
		('v-stop', 'MH-01-STP', 'STP', 'truck', 10, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), NULL, '1'),
		('v-stale', 'MH-01-STL', 'STL', 'truck', 10, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), NULL, '1'),
		('v-maint', 'MH-01-MNT', 'MNT', 'truck', 10, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), date('now'), '1')`)
	require.NoError(t, err)

	recentTs := time.Now().UTC().Format("2006-01-02 15:04:05")
	staleTs := time.Now().UTC().Add(-20 * time.Minute).Format("2006-01-02 15:04:05")

	_, err = db.Exec(`INSERT INTO telemetry_snapshots
		(id, vehicle_id, timestamp, latitude, longitude, speed, odometer, ignition)
		VALUES
		('s-1', 'v-run', ?, 18.5, 73.8, 60.0, 1000.0, 1),
		('s-2', 'v-stop', ?, 18.5, 73.8, 0.0, 1000.0, 0),
		('s-3', 'v-stale', ?, 18.5, 73.8, 50.0, 1000.0, 1),
		('s-4', 'v-maint', ?, 18.5, 73.8, 80.0, 1000.0, 1)`,
		recentTs, recentTs, staleTs, recentTs)
	require.NoError(t, err)

	liveStore := telemetry.NewLiveStore(db, 15*time.Minute)
	vehicles, err := liveStore.Live(shared.ContextWithTenantID(context.Background(), "1"), "1", "", time.Now())
	require.NoError(t, err)
	require.Len(t, vehicles, 4)

	statusMap := make(map[string]string)
	for _, v := range vehicles {
		statusMap[v.VehicleID] = v.Status
	}

	assert.Equal(t, "running", statusMap["v-run"])
	assert.Equal(t, "stopped", statusMap["v-stop"])
	assert.Equal(t, "no_signal", statusMap["v-stale"])
	assert.Equal(t, "maintenance_due", statusMap["v-maint"], "maintenance_due must override running speed 80")
}

// 2. SSE Hub & Streaming (3B) Checkpoint:
// SSE Event framing and filter support
func TestPhase3_3B_SSEStreamingAndFilter(t *testing.T) {
	hub := realtime.NewHub(15, nil)
	ctx, cancel := context.WithCancel(shared.ContextWithTenantID(context.Background(), "1"))
	defer cancel()

	// Filter for trip-123
	filterFn := func(e events.Event) bool {
		if p, ok := e.Payload.(map[string]interface{}); ok {
			return p["trip_id"] == "trip-123"
		}
		return false
	}

	ch, unsub := hub.Subscribe(ctx, filterFn)
	defer unsub()

	// Publish non-matching event
	hub.Publish(ctx, events.Event{
		Type:    "telemetry.snapshot",
		Payload: map[string]interface{}{"trip_id": "trip-other", "speed": 40.0},
	})

	// Publish matching event
	hub.Publish(ctx, events.Event{
		Type:    "telemetry.snapshot",
		Payload: map[string]interface{}{"trip_id": "trip-123", "speed": 55.0},
	})

	select {
	case frame := <-ch:
		frameStr := string(frame)
		assert.Contains(t, frameStr, "event: telemetry\n")
		assert.Contains(t, frameStr, `"trip_id":"trip-123"`)
		assert.Contains(t, frameStr, `"speed":55`)
	case <-time.After(1 * time.Second):
		t.Fatal("expected matching SSE frame")
	}
}

// 3. Share Links (3C) Checkpoint:
// Creation -> Hashed token stored -> View authenticated
func TestPhase3_3C_ShareLinkFlow(t *testing.T) {
	db := newPhase3DB(t)
	rawToken := "share-phase3-token-abc"
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	_, err := db.Exec(`INSERT INTO trips
		(id, trip_number, booking_id, route_id, status, departure_time, arrival_time, tenant_id)
		VALUES ('trip-p3', 'TRIP-P3', 'b-1', 'r-1', 'started', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO share_links
		(id, trip_id, token_hash, created_by, created_at, expires_at)
		VALUES ('share-p3', 'trip-p3', ?, 'user-1', CURRENT_TIMESTAMP, datetime('now', '+24 hours'))`,
		tokenHash)
	require.NoError(t, err)

	tmpl := template.New("share_public.html")
	_, err = tmpl.Parse(`<html><body>{{.TripNumber}}</body></html>`)
	require.NoError(t, err)

	app := &handlers.App{
		DB: db,
		Config: &config.Config{
			LiveMap: config.LiveMapConfig{ShareLinkTTLHours: 24, ShareLinkMaxTTLHours: 168},
		},
		Templates: tmpl,
	}
	app.Share = handlers.NewShareHandlers(app, db)

	r := chi.NewRouter()
	r.Get("/share/{token}", app.Share.ViewShare)
	r.Get("/share/{token}/data", app.Share.ShareData)

	// GET /share/{token}
	req := httptest.NewRequest("GET", "/share/"+rawToken, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "TRIP-P3")

	// GET /share/{token}/data
	reqData := httptest.NewRequest("GET", "/share/"+rawToken+"/data", nil)
	rrData := httptest.NewRecorder()
	r.ServeHTTP(rrData, reqData)
	assert.Equal(t, http.StatusOK, rrData.Code)
}

// 4. Hybrid ETA (3D) Checkpoint:
// 0.7/0.3 blend, ±15 min window, monotonic guard
func TestPhase3_3D_HybridETACalculator(t *testing.T) {
	db := newPhase3DB(t)

	depTime := time.Now().UTC().Add(-1 * time.Hour)
	arrTime := time.Now().UTC().Add(3 * time.Hour)

	_, err := db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('r-eta', 'Mumbai', 'Pune', 200.0, 4.0, 5000.0, '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
		VALUES ('v-eta', 'MH-01-ETA', 'ETA', 'truck', 10, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO trips
		(id, trip_number, booking_id, route_id, vehicle_id, status, departure_time, arrival_time, tenant_id)
		VALUES ('trip-eta', 'TRIP-ETA', 'b-1', 'r-eta', 'v-eta', 'started', ?, ?, '1')`,
		depTime, arrTime)
	require.NoError(t, err)

	// Seed 4 snapshots with 50 km/h speed
	for i := 0; i < 4; i++ {
		ts := time.Now().UTC().Add(-time.Duration(4-i) * time.Minute)
		_, err = db.Exec(`INSERT INTO telemetry_snapshots
			(id, vehicle_id, trip_id, timestamp, latitude, longitude, speed, odometer, ignition)
			VALUES (?, 'v-eta', 'trip-eta', ?, 18.5, 73.8, 50.0, ?, 1)`,
			fmt.Sprintf("snap-eta-%d", i), ts, float64(50+i*2))
		require.NoError(t, err)
	}

	etaSvc := eta.NewEtaService(db, 15, 30, 5)
	res, err := etaSvc.Calculate(shared.ContextWithTenantID(context.Background(), "1"), "trip-eta")
	require.NoError(t, err)

	assert.Equal(t, "hybrid", res.Method)
	// Window must be exactly 30 minutes
	diff := res.EtaMax.Sub(res.EtaMin)
	assert.Equal(t, 30*time.Minute, diff, "window must be ±15 min (30 min span)")
}

// 5. Preventive Maintenance (3E) Checkpoint:
// Dispatch blocker on maintenance_due + Override RBAC & Audit
func TestPhase3_3E_DispatchBlockerAndOverride(t *testing.T) {
	db := newPhase3DB(t)

	_, err := db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, maintenance_due, tenant_id)
		VALUES ('v-blocked', 'MH-01-BLK', 'BLK', 'truck', 10, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), date('now'), 'tenant-1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO drivers
		(id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES ('d-1', 'DRV-1', 'Rajesh', 'Kumar', '9999999999', 'LIC-1', date('now','+1 year'), 'available', 'tenant-1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO trips
		(id, trip_number, booking_id, route_id, status, departure_time, arrival_time, tenant_id)
		VALUES ('trip-blk', 'TRIP-BLK', 'b-1', 'r-1', 'scheduled', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'tenant-1')`)
	require.NoError(t, err)

	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()

	// Assign driver first
	assignDriverUC := tripapp.NewAssignDriverUseCase(unitOfWork, clk)
	err = assignDriverUC.Execute(shared.ContextWithTenantID(context.Background(), "1"), tripapp.AssignDriverCommand{
		TripID:   aggregate.TripID("trip-blk"),
		DriverID: "d-1",
		TenantID: shared.TenantID("tenant-1"),
	})
	require.NoError(t, err)

	assignVehUC := tripapp.NewAssignVehicleUseCase(unitOfWork, clk)

	// Driver assigned, now try to assign maintenance-due vehicle without override
	err = assignVehUC.Execute(shared.ContextWithTenantID(context.Background(), "1"), tripapp.AssignVehicleCommand{
		TripID:              aggregate.TripID("trip-blk"),
		VehicleID:           "v-blocked",
		TenantID:            shared.TenantID("tenant-1"),
		OverrideMaintenance: false,
	})
	assert.Error(t, err, "assigning maintenance_due vehicle without override must fail")
	assert.Contains(t, err.Error(), "maintenance")

	// Now assign with override
	err = assignVehUC.Execute(shared.ContextWithTenantID(context.Background(), "1"), tripapp.AssignVehicleCommand{
		TripID:              aggregate.TripID("trip-blk"),
		VehicleID:           "v-blocked",
		TenantID:            shared.TenantID("tenant-1"),
		OverrideMaintenance: true,
		OverrideReason:      "Emergency medical shipment override",
	})
	require.NoError(t, err)

	// Verify vehicle assigned on trip
	var assignedVeh sql.NullString
	err = db.QueryRow("SELECT vehicle_id FROM trips WHERE id = 'trip-blk'").Scan(&assignedVeh)
	require.NoError(t, err)
	assert.Equal(t, "v-blocked", assignedVeh.String)

	// Verify audit log recorded
	var auditCount int
	err = db.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE action = 'assign_vehicle_override'").Scan(&auditCount)
	require.NoError(t, err)
	assert.Equal(t, 1, auditCount, "override must record audit_log")
}

// 6. CSP Middleware (3F-i) Checkpoint:
// Enabled vs Disabled
func TestPhase3_3F_CSPMiddleware(t *testing.T) {
	hEnabled := middleware.ContentSecurityPolicy(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	hDisabled := middleware.ContentSecurityPolicy(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/tracking", nil)

	rr1 := httptest.NewRecorder()
	hEnabled.ServeHTTP(rr1, req)
	csp1 := rr1.Header().Get("Content-Security-Policy")
	assert.Contains(t, csp1, "script-src 'self' 'unsafe-inline'")
	assert.Contains(t, csp1, "img-src 'self' data: https://mt1.google.com https://tile.openstreetmap.org")
	assert.Contains(t, csp1, "connect-src 'self' https://nominatim.openstreetmap.org")

	rr2 := httptest.NewRecorder()
	hDisabled.ServeHTTP(rr2, req)
	csp2 := rr2.Header().Get("Content-Security-Policy")
	assert.Empty(t, csp2)
}
