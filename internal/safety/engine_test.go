package safety

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"transport-app/internal/fuel"
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
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedFixtures(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO drivers
		(id, driver_id, first_name, last_name, phone, license_number, license_expiry, status)
		VALUES ('d1', 'D-001', 'Ravi', 'Kumar', '9988776655', 'KA-12345', '2028-01-01', 'available')`)
	require.NoError(t, err)
}

type frameSpec struct {
	offset   time.Duration
	speed    float64
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
		}
		var drv interface{}
		if sp.driver != "" {
			drv = sp.driver
		}
		_, err := db.Exec(`INSERT INTO telemetry_snapshots
			(id, vehicle_id, timestamp, speed, ignition, driver_id, trip_id)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("sf-%d", frameSeq), "v1", timeStr(t0.Add(sp.offset)),
			sp.speed, ign, drv, trip)
		require.NoError(t, err)
	}
}

func buildEngine(t *testing.T, db *sql.DB, t0 time.Time) (*Engine, *[]string) {
	t.Helper()
	hookedDrivers := &[]string{}
	e := NewEngine(db, uow.NewSQLUnitOfWork(db), fuel.NewConfigReader(db), slog.New(slog.DiscardHandler))
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

func TestTickSkipsUnknownDriver(t *testing.T) {
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	pre := []frameSpec{{offset: 0, speed: 60}}
	post := []frameSpec{
		{offset: 30 * time.Second, speed: 95},
		{offset: 60 * time.Second, speed: 97},
	}
	rows, _, alerts := runTwoPhase(t, t0, pre, post)
	assert.Empty(t, rows, "no behaviour events without a known driver")
	assert.Zero(t, alerts)
}

func TestTickWarmupDoesNotEmitHistoricalEvents(t *testing.T) {
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	pre := []frameSpec{
		{offset: 0, speed: 60, driver: "d1"},
		{offset: 30 * time.Second, speed: 95, driver: "d1"},
		{offset: 60 * time.Second, speed: 97, driver: "d1"},
	}
	db := newTestDB(t)
	seedFixtures(t, db)
	insertFrames(t, db, t0, pre)
	e, _ := buildEngine(t, db, t0)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		_, err := e.Tick(ctx)
		require.NoError(t, err)
	}
	assert.Empty(t, behaviourRows(t, db), "restart replay must seed state without emitting")
}

func TestTripDriverFallbackAttribution(t *testing.T) {
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	db := newTestDB(t)
	seedFixtures(t, db)
	_, err := db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, driver_id)
		VALUES ('t1', 'TRIP-001', 'r1', '2026-08-19 09:00:00', 'in_transit', 'd1')`)
	require.NoError(t, err)
	insertFrames(t, db, t0, []frameSpec{
		{offset: 0, speed: 60, tripID: "t1"},
	})
	e, _ := buildEngine(t, db, t0)
	ctx := context.Background()
	_, err = e.Tick(ctx)
	require.NoError(t, err)
	insertFrames(t, db, t0, []frameSpec{
		{offset: 30 * time.Second, speed: 95, tripID: "t1"},
		{offset: 60 * time.Second, speed: 97, tripID: "t1"},
	})
	_, err = e.Tick(ctx)
	require.NoError(t, err)
	rows := behaviourRows(t, db)
	require.Len(t, rows, 1)
	assert.Equal(t, "d1", rows[0]["driver_id"], "driver must be attributed via trips.driver_id fallback")
}
