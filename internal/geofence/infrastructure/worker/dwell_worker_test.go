package worker

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"transport-app/internal/events"
	"transport-app/internal/geofence/application"
	"transport-app/internal/geofence/domain"
	sqlrepo "transport-app/internal/geofence/infrastructure/persistence/sql"
	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/outbox"
	"transport-app/internal/shared/uow"
	tripApp "transport-app/internal/trip/application"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// mockFixRepo is the test double for domain.FixRepository. It mirrors the
// real repository's watermark semantics: fixes newer than
// engine_state.last_fix_at per vehicle.
type mockFixRepo struct {
	fixes []domain.Fix
	db    *sql.DB
}

func (m *mockFixRepo) LoadNewFixes(ctx context.Context, limit int) ([]domain.Fix, error) {
	var out []domain.Fix
	for _, f := range m.fixes {
		var last sql.NullTime
		_ = m.db.QueryRowContext(ctx,
			`SELECT last_fix_at FROM engine_state WHERE vehicle_id = ?`, f.VehicleID).Scan(&last)
		if !last.Valid || f.Timestamp.After(last.Time) {
			out = append(out, f)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// newTestDB creates an in-memory SQLite DB with all migrations applied.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_geofence_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../../../db/migrations"))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedFixtures inserts a vehicle, route, trip and geofence used by the tests.
func seedFixtures(t *testing.T, db *sql.DB, gf domain.Geofence) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type,
		 insurance_expiry, fitness_expiry, permit_expiry, status)
		VALUES ('v1', 'KA01AB1234', 'KA01AB1234', 'truck', 2000, 'diesel',
		        '2027-01-01', '2027-01-01', '2027-01-01', 'available')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('r1', 'Bengaluru', 'Mysuru', 145, 3, 25000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status)
		VALUES ('t1', 'TRIP-001', 'r1', '2026-08-19 09:00:00', 'in_transit')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO geofences
		(id, tenant_id, name, kind, shape, center_lat, center_lng, radius_m, priority, is_active)
		VALUES (?, '1', ?, ?, 'circle', ?, ?, ?, 10, 1)`,
		gf.ID, gf.Name, gf.Kind, gf.CenterLat, gf.CenterLng, gf.RadiusM)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO company_config (tenant_id, key, value) VALUES
		('1', 'geofence.dwell_debounce_seconds', '2'),
		('1', 'geofence.buffer_metres', '20'),
		('1', 'geofence.hysteresis_metres', '25')`)
	require.NoError(t, err)
}

// buildWorker wires a DwellWorker over the real DB with a mocked FixRepository.
func buildWorker(t *testing.T, db *sql.DB, fixes []domain.Fix) (*DwellWorker, *events.InMemoryBus) {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	bus := events.NewInMemoryBus()
	w := &DwellWorker{
		uow:       uow.NewSQLUnitOfWork(db),
		db:        db,
		config:    application.NewConfigReader(db),
		bus:       bus,
		outbox:    outbox.NewOutboxWriter(db),
		idGen:     id.NewUUIDGenerator(),
		log:       logger,
		tenantID:  "1",
		fixes:     &mockFixRepo{fixes: fixes, db: db},
		geofences: sqlrepo.NewGeofenceRepository(db),
		states:    sqlrepo.NewEngineStateRepository(db),
		logs:      sqlrepo.NewEventLogRepository(db),
	}
	return w, bus
}

// northOf returns a coordinate `metres` north of the given centre.
func northOf(lat, lng, metres float64) (float64, float64) {
	return lat + metres/111320.0, lng
}

func TestDwellWorker_SweepCrossesCircleBoundary(t *testing.T) {
	db := newTestDB(t)
	seedFixtures(t, db, domain.Geofence{
		ID: "z1", TenantID: string(shared.DefaultTenant), Name: "pickup-zone", Kind: domain.KindPickup,
		Shape: domain.ShapeCircle, CenterLat: 12.97, CenterLng: 77.59, RadiusM: 100,
	})

	t0 := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	latFar, lngFar := northOf(12.97, 77.59, 1000)
	latIn, lngIn := northOf(12.97, 77.59, 50)
	tripID := "t1"

	// 10 fixes: 2 far, 3 inside (debounce=2s), 3 outside, 2 far.
	var fixes []domain.Fix
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	fixes = append(fixes,
		domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(1), Latitude: latFar, Longitude: lngFar},
		domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(2), Latitude: latFar, Longitude: lngFar},
		domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(3), Latitude: latIn, Longitude: lngIn},
		domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(4), Latitude: latIn, Longitude: lngIn},
		domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(5), Latitude: latIn, Longitude: lngIn},
		domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(6), Latitude: latFar, Longitude: lngFar},
		domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(7), Latitude: latFar, Longitude: lngFar},
		domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(8), Latitude: latFar, Longitude: lngFar},
		domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(9), Latitude: latFar, Longitude: lngFar},
		domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(10), Latitude: latFar, Longitude: lngFar},
	)

	w, _ := buildWorker(t, db, fixes)
	handled, err := w.Tick(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 10, handled)

	// Engine state ends outside with the last fix watermark.
	st, err := sqlrepo.NewEngineStateRepository(db).GetByVehicle(context.Background(), "1", "v1")
	require.NoError(t, err)
	assert.Equal(t, domain.StateOutside, st.State)
	assert.Equal(t, at(10), st.LastFixAt)
	assert.Equal(t, latFar, st.LastLat)

	// Zone events: one entering + one leaving.
	var eventsCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM geofence_events WHERE vehicle_id = 'v1'`).Scan(&eventsCount))
	assert.Equal(t, 2, eventsCount)

	var enteringEvent, leavingEvent string
	require.NoError(t, db.QueryRow(`SELECT event_type FROM geofence_events WHERE event_type = 'entering'`).Scan(&enteringEvent))
	require.NoError(t, db.QueryRow(`SELECT event_type FROM geofence_events WHERE event_type = 'leaving'`).Scan(&leavingEvent))
	assert.Equal(t, domain.EventEntering, enteringEvent)
	assert.Equal(t, domain.EventLeaving, leavingEvent)

	// Detention window opened at confirmation and closed on exit.
	var detStatus, detZoneKind string
	var dwell int64
	var enteredAt time.Time
	require.NoError(t, db.QueryRow(
		`SELECT status, zone_kind, dwell_seconds, entered_at FROM trip_detentions WHERE trip_id = 't1'`).
		Scan(&detStatus, &detZoneKind, &dwell, &enteredAt))
	assert.Equal(t, domain.DetentionClosed, detStatus)
	assert.Equal(t, domain.KindPickup, detZoneKind)
	assert.Equal(t, int64(3), dwell) // exit confirmed at t8, entered t5
	assert.Equal(t, at(5), enteredAt)

	// Watermark advanced: a second sweep must process nothing new.
	handled2, err := w.Tick(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, handled2)
}

func TestDwellWorker_RestrictedBreachEmitsOutboxAlert(t *testing.T) {
	db := newTestDB(t)
	seedFixtures(t, db, domain.Geofence{
		ID: "zr", TenantID: string(shared.DefaultTenant), Name: "restricted-yard", Kind: domain.KindRestricted,
		Shape: domain.ShapeCircle, CenterLat: 12.97, CenterLng: 77.59, RadiusM: 100,
	})

	t0 := time.Date(2026, 8, 19, 11, 0, 0, 0, time.UTC)
	latIn, lngIn := northOf(12.97, 77.59, 30)

	var fixes []domain.Fix
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	for i := 0; i < 3; i++ {
		fixes = append(fixes, domain.Fix{
			VehicleID: "v1", Timestamp: at(i + 1), Latitude: latIn, Longitude: lngIn,
		})
	}

	w, bus := buildWorker(t, db, fixes)
	alerts := make(chan events.Event, 1)
	bus.Subscribe("geofence.zone_breach", func(ctx context.Context, e events.Event) error {
		alerts <- e
		return nil
	})
	handled, err := w.Tick(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, handled)

	// geofence_events: entering + breach.
	var breachCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM geofence_events WHERE event_type = 'breach'`).Scan(&breachCount))
	assert.Equal(t, 1, breachCount)

	// Outbox row in the same transaction.
	var outboxCount int
	var outboxType string
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM outbox_events WHERE event_type = 'geofence.zone_breach'`).Scan(&outboxCount))
	assert.Equal(t, 1, outboxCount)
	require.NoError(t, db.QueryRow(
		`SELECT event_type FROM outbox_events LIMIT 1`).Scan(&outboxType))
	assert.Equal(t, "geofence.zone_breach", outboxType)

	// Bus received the alert (post-commit publication path).
	select {
	case e := <-alerts:
		assert.Equal(t, "geofence.zone_breach", e.Type)
		alert, ok := e.Payload.(application.GeofenceAlertEvent)
		require.True(t, ok)
		assert.Equal(t, "zr", alert.GeofenceID)
		assert.Equal(t, application.SeverityMedium, alert.Severity)
	case <-time.After(2 * time.Second):
		t.Fatal("bus did not receive the zone breach alert")
	}
}

// setTripStatus mutates the seeded trip status directly (statuses beyond the
// 00007 CHECK are allowed after migration 00026 rebuilt the table).
func setTripStatus(t *testing.T, db *sql.DB, status string) {
	t.Helper()
	_, err := db.Exec(`UPDATE trips SET status = ? WHERE id = 't1'`, status)
	require.NoError(t, err)
}

// setConfig upserts a single geofence config key.
func setConfig(t *testing.T, db *sql.DB, key, value string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO company_config (tenant_id, key, value) VALUES ('1', ?, ?)
		ON CONFLICT(tenant_id, key) DO UPDATE SET value = excluded.value`, key, value)
	require.NoError(t, err)
}

// buildTransitioner wires a worker with the real ReachPickup/StartTransit use
// cases over the same DB, mirroring cmd/server/main.go.
func buildTransitioner(t *testing.T, db *sql.DB, fixes []domain.Fix) *DwellWorker {
	t.Helper()
	w, _ := buildWorker(t, db, fixes)
	uowImpl := uow.NewSQLUnitOfWork(db)
	w.WithTripTransitions(
		tripApp.NewReachPickupUseCase(uowImpl, clock.NewRealClock()),
		tripApp.NewStartTransitUseCase(uowImpl, clock.NewRealClock()),
	)
	return w
}

func TestDwellWorker_AutoReachPickupOnPickupEntering(t *testing.T) {
	db := newTestDB(t)
	seedFixtures(t, db, domain.Geofence{
		ID: "z1", TenantID: string(shared.DefaultTenant), Name: "pickup-zone", Kind: domain.KindPickup,
		Shape: domain.ShapeCircle, CenterLat: 12.97, CenterLng: 77.59, RadiusM: 100,
	})
	setTripStatus(t, db, "started")
	setConfig(t, db, application.ConfigAutoReachPickup, "1")

	t0 := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	latIn, lngIn := northOf(12.97, 77.59, 30)
	tripID := "t1"
	var fixes []domain.Fix
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	for i := 0; i < 3; i++ {
		fixes = append(fixes, domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(i + 1), Latitude: latIn, Longitude: lngIn})
	}

	w := buildTransitioner(t, db, fixes)
	handled, err := w.Tick(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, handled)

	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM trips WHERE id = 't1'`).Scan(&status))
	assert.Equal(t, "reached_pickup", status)

	// Audit trail written by the transition use case.
	var auditCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM audit_logs WHERE action = 'reach_pickup'`).Scan(&auditCount))
	assert.Equal(t, 1, auditCount)
}

func TestDwellWorker_AutoStartTransitOnDropEntering(t *testing.T) {
	db := newTestDB(t)
	seedFixtures(t, db, domain.Geofence{
		ID: "z1", TenantID: string(shared.DefaultTenant), Name: "drop-zone", Kind: domain.KindDrop,
		Shape: domain.ShapeCircle, CenterLat: 12.97, CenterLng: 77.59, RadiusM: 100,
	})
	setTripStatus(t, db, "reached_pickup")
	setConfig(t, db, application.ConfigAutoStartTransit, "1")

	t0 := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	latIn, lngIn := northOf(12.97, 77.59, 30)
	tripID := "t1"
	var fixes []domain.Fix
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	for i := 0; i < 3; i++ {
		fixes = append(fixes, domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(i + 1), Latitude: latIn, Longitude: lngIn})
	}

	w := buildTransitioner(t, db, fixes)
	handled, err := w.Tick(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, handled)

	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM trips WHERE id = 't1'`).Scan(&status))
	assert.Equal(t, "in_transit", status)
}

func TestDwellWorker_RouteNameFallbackGatesPickupTransition(t *testing.T) {
	db := newTestDB(t)
	seedFixtures(t, db, domain.Geofence{
		ID: "z1", TenantID: string(shared.DefaultTenant), Name: "pickup-zone", Kind: domain.KindPickup,
		Shape: domain.ShapeCircle, CenterLat: 12.97, CenterLng: 77.59, RadiusM: 100,
		RouteName: "bengaluru", // lowercase vs route source "Bengaluru"
	})
	setTripStatus(t, db, "started")
	setConfig(t, db, application.ConfigAutoReachPickup, "1")

	t0 := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	latIn, lngIn := northOf(12.97, 77.59, 30)
	tripID := "t1"
	var fixes []domain.Fix
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	for i := 0; i < 3; i++ {
		fixes = append(fixes, domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(i + 1), Latitude: latIn, Longitude: lngIn})
	}

	w := buildTransitioner(t, db, fixes)
	_, err := w.Tick(context.Background())
	require.NoError(t, err)

	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM trips WHERE id = 't1'`).Scan(&status))
	assert.Equal(t, "reached_pickup", status, "route-name LOWER/TRIM fallback must match the trip route")

	// A zone pinned to a different route must never trigger the transition.
	setTripStatus(t, db, "started")
	_, err = db.Exec(`UPDATE geofences SET route_name = 'delhi', is_active = 1 WHERE id = 'z1'`)
	require.NoError(t, err)
	_, err = w.Tick(context.Background())
	require.NoError(t, err)
	require.NoError(t, db.QueryRow(`SELECT status FROM trips WHERE id = 't1'`).Scan(&status))
	assert.Equal(t, "started", status, "pickup zone on a different route must not transition")
}

func TestDwellWorker_DetentionBilling_FreeSecondsAndRate(t *testing.T) {
	db := newTestDB(t)
	seedFixtures(t, db, domain.Geofence{
		ID: "z1", TenantID: string(shared.DefaultTenant), Name: "pickup-zone", Kind: domain.KindPickup,
		Shape: domain.ShapeCircle, CenterLat: 12.97, CenterLng: 77.59, RadiusM: 100,
	})
	setConfig(t, db, application.ConfigDetentionFreeSeconds, "1800")
	setConfig(t, db, application.ConfigDetentionRatePerHour, "10")

	// Enter confirmed at t0 (3 consecutive inside), exit confirmed at t7200
	// (3 consecutive outside) → dwell = 7200s, free = 1800s → billable 5400s
	// → amount = 5400/3600 × 10 = ₹15.00 exactly.
	t0 := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	latIn, lngIn := northOf(12.97, 77.59, 30)
	latOut, lngOut := northOf(12.97, 77.59, 1000)
	tripID := "t1"
	var fixes []domain.Fix
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	for i := -2; i <= 0; i++ {
		fixes = append(fixes, domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(i), Latitude: latIn, Longitude: lngIn})
	}
	for i := 7198; i <= 7200; i++ {
		fixes = append(fixes, domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: at(i), Latitude: latOut, Longitude: lngOut})
	}

	w, _ := buildWorker(t, db, fixes)
	handled, err := w.Tick(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 6, handled)

	var billable, free, dwell int64
	var rate, amount float64
	var zoneName string
	require.NoError(t, db.QueryRow(
		`SELECT d.dwell_seconds, d.free_seconds, d.billable_seconds, d.rate_per_hour, d.amount, g.name
		 FROM trip_detentions d JOIN geofences g ON g.id = d.geofence_id
		 WHERE d.trip_id = 't1'`).
		Scan(&dwell, &free, &billable, &rate, &amount, &zoneName))
	assert.Equal(t, int64(7200), dwell)
	assert.Equal(t, int64(1800), free)
	assert.Equal(t, int64(5400), billable)
	assert.Equal(t, 10.0, rate)
	assert.Equal(t, 15.00, amount)
	assert.Equal(t, "pickup-zone", zoneName)
}

func TestDwellWorker_RetrySameEventsInsertsOnce(t *testing.T) {
	db := newTestDB(t)
	zone := domain.Geofence{
		ID: "z1", TenantID: "1", Name: "pickup-zone", Kind: domain.KindPickup,
		Shape: domain.ShapeCircle, CenterLat: 12.97, CenterLng: 77.59, RadiusM: 100,
	}
	seedFixtures(t, db, zone)
	w, _ := buildWorker(t, db, nil)

	t0 := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	tripID := "t1"
	fix := domain.Fix{VehicleID: "v1", TripID: &tripID, Timestamp: t0, Latitude: 12.9704, Longitude: 77.59}
	state := domain.EngineState{VehicleID: "v1", TenantID: "1", State: domain.StateInside, LastFixAt: t0}
	events := []application.ZoneEvent{{
		EventType: domain.EventEntering, Zone: zone, Lat: fix.Latitude, Lng: fix.Longitude, At: t0,
	}}

	ctx := context.Background()
	require.NoError(t, w.persist(ctx, state, state, fix, events, "1"))
	// Retry with identical inputs (commit-then-crash / overlapping poll).
	require.NoError(t, w.persist(ctx, state, state, fix, events, "1"))

	var eventCount, detentionCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM geofence_events WHERE vehicle_id = 'v1'`).Scan(&eventCount))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM trip_detentions WHERE trip_id = 't1'`).Scan(&detentionCount))
	assert.Equal(t, 1, eventCount, "replayed entering event must not duplicate")
	assert.Equal(t, 1, detentionCount, "replayed entering event must not reopen detention")
}
