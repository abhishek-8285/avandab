package safety

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"transport-app/internal/fuel"
	"transport-app/internal/shared"
	"transport-app/internal/shared/uow"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_safety_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
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

func seedFixtures(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug, status)
		VALUES ('tenant-1', 'Tenant 1', 'slug-safety-t1', 'active')
		ON CONFLICT(id) DO UPDATE SET name='Tenant 1'`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug, status)
		VALUES ('tenant-2', 'Tenant 2', 'slug-safety-t2', 'active')
		ON CONFLICT(id) DO UPDATE SET name='Tenant 2'`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers
		(id, driver_id, tenant_id, first_name, last_name, phone, license_number, license_expiry, status)
		VALUES ('d1', 'D-001', 'tenant-1', 'Ravi', 'Kumar', '9988776655', 'KA-12345', '2028-01-01', 'available')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id)
		VALUES ('v1', 'KA01AB1234', 'KA-01-AB-1234', 'truck', 5000, 'diesel', '2030-01-01', '2030-01-01', '2030-01-01', 'available', 'tenant-1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips
		(id, trip_number, route_id, departure_time, status, driver_id, vehicle_id, tenant_id)
		VALUES ('t1', 'TRIP-001', 'r1', '2026-08-19 09:00:00', 'in_transit', 'd1', 'v1', 'tenant-1')`)
	require.NoError(t, err)
}

type frameSpec struct {
	offset   time.Duration
	speed    float64
	lat      float64
	lng      float64
	ignition *bool
	driver   string
	tripID   string
}

var frameSeq int

func insertFrames(t *testing.T, db *sql.DB, t0 time.Time, specs []frameSpec) {
	t.Helper()
	for _, sp := range specs {
		frameSeq++
		var ign interface{}
		if sp.ignition != nil {
			if *sp.ignition {
				ign = 1
			} else {
				ign = 0
			}
		}
		var trip interface{}
		if sp.tripID != "" {
			trip = sp.tripID
		} else if sp.driver != "" {
			trip = "t1"
		}
		_, err := db.Exec(`INSERT INTO telemetry_snapshots
			(id, vehicle_id, timestamp, speed, latitude, longitude, ignition, trip_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("sf-%d", frameSeq), "v1", timeStr(t0.Add(sp.offset)),
			sp.speed, sp.lat, sp.lng, ign, trip)
		require.NoError(t, err)
	}
}

func buildEngine(t *testing.T, db *sql.DB, t0 time.Time) (*Engine, *[]string) {
	t.Helper()
	hookedDrivers := &[]string{}
	e := NewEngine(db, uow.NewSQLUnitOfWork(db), fuel.NewConfigReader(db), slog.New(slog.DiscardHandler))
	e.tenantID = shared.TenantID("tenant-1")
	e.WithLocation(time.UTC)
	e.now = func() time.Time { return t0.Add(-time.Minute) }
	e.WithBehaviourHook(func(_ context.Context, driverID string) {
		*hookedDrivers = append(*hookedDrivers, driverID)
	})
	return e, hookedDrivers
}

func behaviourRows(t *testing.T, db *sql.DB) []map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT event_type, severity, weight, metadata, driver_id
		FROM driver_behaviour_events ORDER BY occurred_at ASC`)
	require.NoError(t, err)
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var evt, sev, meta, drv string
		var w float64
		require.NoError(t, rows.Scan(&evt, &sev, &w, &meta, &drv))
		out = append(out, map[string]string{
			"event_type": evt, "severity": sev,
			"weight": fmt.Sprintf("%v", w), "metadata": meta, "driver_id": drv,
		})
	}
	require.NoError(t, rows.Err())
	return out
}

func countOutboxSafetyAlerts(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM outbox_events
		WHERE aggregate_type = 'Vehicle' AND event_type = 'AlertEvent' AND payload LIKE '%"SAFETY"%'`).
		Scan(&n)
	require.NoError(t, err)
	return n
}

func runTwoPhase(t *testing.T, t0 time.Time, pre, post []frameSpec) ([]map[string]string, *[]string, int) {
	t.Helper()
	db := newTestDB(t)
	seedFixtures(t, db)
	insertFrames(t, db, t0, pre)
	e, hooked := buildEngine(t, db, t0)
	ctx := context.Background()
	n1, err := e.Tick(ctx)
	require.NoError(t, err)
	insertFrames(t, db, t0, post)
	n2, err := e.Tick(ctx)
	require.NoError(t, err)
	t.Logf("phase counts: warmup=%d processed=%d", n1, n2)
	return behaviourRows(t, db), hooked, countOutboxSafetyAlerts(t, db)
}

func TestTickSpeedingEndToEnd(t *testing.T) {
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	pre := []frameSpec{{offset: 0, speed: 60, driver: "d1"}}
	post := []frameSpec{
		{offset: 30 * time.Second, speed: 95, driver: "d1"},
		{offset: 60 * time.Second, speed: 97, driver: "d1"},
		{offset: 90 * time.Second, speed: 60, driver: "d1"},
	}
	rows, hooked, alerts := runTwoPhase(t, t0, pre, post)
	require.Len(t, rows, 1, "exactly one speeding event expected: %v", rows)
	assert.Equal(t, EventSpeeding, rows[0]["event_type"])
	assert.Equal(t, "medium", rows[0]["severity"])
	assert.Equal(t, "d1", rows[0]["driver_id"])
	var meta map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(rows[0]["metadata"]), &meta))
	assert.InDelta(t, 97.0, meta["speed_kmh"], 0.01)
	assert.Contains(t, *hooked, "d1", "behaviour hook must fire for scorecard recompute")
	assert.Equal(t, 1, alerts, "one SAFETY alert must reach the outbox")
}

func TestTickIdlingEndToEnd(t *testing.T) {
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	on := true
	pre := []frameSpec{{offset: 0, speed: 40, ignition: &on, driver: "d1"}}
	post := []frameSpec{{offset: 30 * time.Second, speed: 0, ignition: &on, driver: "d1"}}
	for i := 2; i <= 11; i++ {
		post = append(post, frameSpec{offset: time.Duration(i) * 30 * time.Second, speed: 0.5, ignition: &on, driver: "d1"})
	}
	rows, _, alerts := runTwoPhase(t, t0, pre, post)
	require.Len(t, rows, 1, "one idling event after 5+ minutes stationary: %v", rows)
	assert.Equal(t, EventIdling, rows[0]["event_type"])
	assert.Equal(t, "low", rows[0]["severity"])
	assert.Equal(t, 1, alerts)
}

func TestTickHarshBrakingAndAcceleration(t *testing.T) {
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	db := newTestDB(t)
	seedFixtures(t, db)
	e, hooked := buildEngine(t, db, t0)
	ctx := context.Background()

	policy := DefaultSafetyPolicy("tenant-1")
	frames := []snapshot{
		{id: "s1", vehicleID: "v1", driverID: "d1", ts: t0, speed: 20},
		{id: "s2", vehicleID: "v1", driverID: "d1", ts: t0.Add(3 * time.Second), speed: 55}, // +11.66 km/h/s -> harsh accel
		{id: "s3", vehicleID: "v1", driverID: "d1", ts: t0.Add(6 * time.Second), speed: 55},
		{id: "s4", vehicleID: "v1", driverID: "d1", ts: t0.Add(9 * time.Second), speed: 20}, // -11.66 km/h/s -> harsh brake
	}

	n, err := e.ProcessSnapshotsDirect(ctx, policy, "v1", frames)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	rows := behaviourRows(t, db)
	require.Len(t, rows, 2)
	assert.Equal(t, EventHarshAccel, rows[0]["event_type"])
	assert.Equal(t, EventHarshBraking, rows[1]["event_type"])
	assert.Len(t, *hooked, 2)
}

func TestTickNightDriving(t *testing.T) {
	t0 := time.Date(2026, 8, 20, 23, 30, 0, 0, time.UTC)
	db := newTestDB(t)
	seedFixtures(t, db)
	e, hooked := buildEngine(t, db, t0)
	ctx := context.Background()

	policy := DefaultSafetyPolicy("tenant-1")
	frames := []snapshot{
		{id: "s1", vehicleID: "v1", driverID: "d1", ts: t0, speed: 45},
	}

	n, err := e.ProcessSnapshotsDirect(ctx, policy, "v1", frames)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	rows := behaviourRows(t, db)
	require.Len(t, rows, 1)
	assert.Equal(t, EventNightDriving, rows[0]["event_type"])
	assert.Equal(t, "medium", rows[0]["severity"])
	assert.Contains(t, *hooked, "d1")
}

func TestTelemetryReplay5x_ZeroDriftZeroDuplicates(t *testing.T) {
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	db := newTestDB(t)
	seedFixtures(t, db)
	e, hooked := buildEngine(t, db, t0)
	ctx := context.Background()

	policy := DefaultSafetyPolicy("tenant-1")
	frames := []snapshot{
		{id: "s1", vehicleID: "v1", driverID: "d1", ts: t0, speed: 20},
		{id: "s2", vehicleID: "v1", driverID: "d1", ts: t0.Add(3 * time.Second), speed: 55}, // harsh accel
	}

	// First execution
	n1, err := e.ProcessSnapshotsDirect(ctx, policy, "v1", frames)
	require.NoError(t, err)
	assert.Equal(t, 1, n1)
	assert.Equal(t, 1, countOutboxSafetyAlerts(t, db))
	assert.Equal(t, 1, len(*hooked))

	// Replays 2..5 (simulate telemetry retry / reprocessing)
	for i := 2; i <= 5; i++ {
		// Reset in-memory state to simulate fresh restart / replay over same DB
		e.state = make(map[string]*vehicleState)
		nReplay, err := e.ProcessSnapshotsDirect(ctx, policy, "v1", frames)
		require.NoError(t, err)
		assert.Equal(t, 0, nReplay, "replay %d must produce 0 new safety events", i)
	}

	// Assert final invariants
	rows := behaviourRows(t, db)
	assert.Len(t, rows, 1, "exactly 1 behaviour event must exist in DB after 5x replay")
	assert.Equal(t, 1, countOutboxSafetyAlerts(t, db), "exactly 1 alert must exist in outbox after 5x replay")
	assert.Equal(t, 1, len(*hooked), "downstream scorecard hook must fire exactly once")
}

func TestTenantSpecificSafetyPolicy(t *testing.T) {
	db := newTestDB(t)
	seedFixtures(t, db)

	// Set tenant-2 strict speed limit: 50 km/h (default is 80)
	_, err := db.Exec(`INSERT INTO company_config (tenant_id, key, value) VALUES ('tenant-2', 'safety.speed_limit_kmh', '50.0')`)
	require.NoError(t, err)

	cfgReader := fuel.NewConfigReader(db)
	policyT1, err := LoadSafetyPolicy(context.Background(), "tenant-1", cfgReader)
	require.NoError(t, err)
	assert.Equal(t, 80.0, policyT1.SpeedLimitKmh)

	policyT2, err := LoadSafetyPolicy(context.Background(), "tenant-2", cfgReader)
	require.NoError(t, err)
	assert.Equal(t, 50.0, policyT2.SpeedLimitKmh)
}
