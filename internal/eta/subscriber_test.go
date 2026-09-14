package eta

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	tripevents "transport-app/internal/domain/trip"
	"transport-app/internal/domain/types"
	"transport-app/internal/events"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
)

func newTestEtaDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_eta_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSubscriber_RecordsHistoryOnTripCompleted(t *testing.T) {
	db := newTestEtaDB(t)
	// Seed trip + telemetry_positions simulating a completed run
	tripID := "trip-eta-1"
	_, err := db.Exec(`INSERT INTO trips (id, tenant_id, status, route_id, departure_time, created_at, updated_at, trip_number, vehicle_id)
		VALUES (?, '1', 'completed', 'route-1', datetime('now','-2 hours'), datetime('now'), datetime('now'), 'TRP-ETA-1', 'veh-eta-1')`, tripID)
	require.NoError(t, err)

	// 4 positions: stopped -> moving (45) -> moving (50) -> stopped
	now := time.Now().UTC()
	for i, p := range []struct {
		lat, lng float64
		speed    float64
		dt       time.Time
	}{
		{18.5204, 73.8567, 0, now.Add(-120 * time.Minute)},
		{18.5300, 73.8600, 45, now.Add(-110 * time.Minute)},
		{18.5500, 73.8800, 50, now.Add(-100 * time.Minute)},
		{18.5700, 73.9000, 0, now.Add(-90 * time.Minute)},
	} {
		_, err := db.Exec(`INSERT INTO telemetry_positions (id, tenant_id, trip_id, vehicle_id, imei, device_time, received_at, latitude, longitude, speed, provider)
			VALUES (?, '1', ?, 'veh-eta-1', 'IMEI-ETA', ?, datetime('now'), ?, ?, ?, 'own')`,
			fmt.Sprintf("pos-%d-%s", i, tripID), tripID, p.dt.Format("2006-01-02 15:04:05"), p.lat, p.lng, p.speed)
		require.NoError(t, err)
	}

	svc := NewEtaService(db, 15, 30, 5, id.NewUUIDGenerator(), clock.NewRealClock())
	bus := events.NewInMemoryBus()
	svc.SubscribeTripEvents(bus, nil)

	// Publish TripCompletedEvent (typed)
	bus.Publish(context.Background(), events.Event{
		Type: events.TripCompleted,
		Payload: tripevents.TripCompletedEvent{
			TripID:      types.TripID(tripID),
			CompletedAt: now,
			OccurredAt:  now,
		},
	})

	var cnt int
	err = db.QueryRow(`SELECT count(*) FROM eta_history WHERE trip_id=?`, tripID).Scan(&cnt)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, cnt, 1, "eta_history should have at least one segment")

	var segStart, segEnd string
	var mins int
	var tag string
	err = db.QueryRow(`SELECT segment_start, segment_end, actual_minutes, traffic_tag FROM eta_history WHERE trip_id=? LIMIT 1`, tripID).Scan(&segStart, &segEnd, &mins, &tag)
	require.NoError(t, err)
	assert.NotEmpty(t, segStart)
	assert.NotEmpty(t, segEnd)
	assert.GreaterOrEqual(t, mins, 2)
	assert.NotEmpty(t, tag)
}

func TestSubscriber_NoSegmentsNoRows(t *testing.T) {
	db := newTestEtaDB(t)
	tripID := "trip-eta-empty"
	_, err := db.Exec(`INSERT INTO trips (id, tenant_id, status, route_id, departure_time, created_at, updated_at, trip_number) VALUES (?, '1', 'completed', 'route-1', datetime('now'), datetime('now'), datetime('now'), 'TRP-ETA-2')`, tripID)
	require.NoError(t, err)

	svc := NewEtaService(db, 15, 30, 5, id.NewUUIDGenerator(), clock.NewRealClock())
	bus := events.NewInMemoryBus()
	svc.SubscribeTripEvents(bus, nil)
	bus.Publish(context.Background(), events.Event{
		Type: events.TripCompleted,
		Payload: tripevents.TripCompletedEvent{
			TripID:      types.TripID(tripID),
			CompletedAt: time.Now(),
			OccurredAt:  time.Now(),
		},
	})
	var cnt int
	_ = db.QueryRow(`SELECT count(*) FROM eta_history WHERE trip_id=?`, tripID).Scan(&cnt)
	assert.Equal(t, 0, cnt)
}

func TestDeriveTrafficTag(t *testing.T) {
	assert.Equal(t, "monsoon", deriveTrafficTag(time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)))
	assert.Equal(t, "high", deriveTrafficTag(time.Date(2026, 12, 15, 9, 0, 0, 0, time.UTC)))
	assert.Equal(t, "low", deriveTrafficTag(time.Date(2026, 12, 15, 23, 0, 0, 0, time.UTC)))
	assert.Equal(t, "medium", deriveTrafficTag(time.Date(2026, 12, 15, 14, 0, 0, 0, time.UTC)))
}

func TestGeohash6(t *testing.T) {
	h := geohash6(19.0760, 72.8777)
	assert.Len(t, h, 6)
	assert.Equal(t, h, geohash6(19.0760, 72.8777))
	assert.NotEqual(t, h, geohash6(19.2183, 72.9781))
}

func TestHistory_CleanupAndAggregation(t *testing.T) {
	db := newTestEtaDB(t)
	svc := NewEtaService(db, 15, 30, 5, id.NewUUIDGenerator(), clock.NewRealClock())
	// Insert old row >90 days
	_, err := db.Exec(`INSERT INTO eta_history (id, tenant_id, trip_id, segment_start, segment_end, actual_minutes, traffic_tag, created_at) VALUES ('old-1','1','trip-x','abc','def',10,'low', datetime('now','-100 days'))`)
	require.NoError(t, err)
	n, err := svc.CleanupOldHistory(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	var cnt int
	_ = db.QueryRow(`SELECT count(*) FROM eta_history WHERE id='old-1'`).Scan(&cnt)
	assert.Equal(t, 0, cnt)

	// Aggregation path: insert a row just over threshold then aggregate (should move to monthly)
	_, _ = db.Exec(`INSERT INTO eta_history (id, tenant_id, trip_id, segment_start, segment_end, actual_minutes, traffic_tag, created_at) VALUES ('old-2','1','trip-y','segA','segB',20,'medium', datetime('now','-91 days'))`)
	require.NoError(t, svc.AggregateMonthly(context.Background()))
	_ = db.QueryRow(`SELECT count(*) FROM eta_history_monthly WHERE segment_start='segA'`).Scan(&cnt)
	assert.GreaterOrEqual(t, cnt, 1)
}
