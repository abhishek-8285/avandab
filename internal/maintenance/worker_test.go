package maintenance

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/events"
	"transport-app/internal/maintenance/domain"
	maintsql "transport-app/internal/maintenance/infrastructure/sql"
)

type mockBus struct {
	mu        sync.Mutex
	events    []events.Event
	listeners map[string][]events.Handler
}

func newMockBus() *mockBus {
	return &mockBus{
		listeners: make(map[string][]events.Handler),
	}
}

func (b *mockBus) Publish(ctx context.Context, evt events.Event) error {
	b.mu.Lock()
	b.events = append(b.events, evt)
	handlers := append([]events.Handler{}, b.listeners[evt.Type]...)
	b.mu.Unlock()

	for _, h := range handlers {
		_ = h(ctx, evt)
	}
	return nil
}

func (b *mockBus) Subscribe(topic string, handler events.Handler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners[topic] = append(b.listeners[topic], handler)
	return func() {}
}

func (b *mockBus) GetEvents(eventType string) []events.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []events.Event
	for _, e := range b.events {
		if eventType == "" || e.Type == eventType {
			out = append(out, e)
		}
	}
	return out
}

func newMaintTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_maint_%d", time.Now().UnixNano())
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

func insertTestVehicle(t *testing.T, db *sql.DB, id, reg string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry)
		VALUES (?, ?, ?, 'truck', 15, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'))`,
		id, reg, "MH-01-"+reg)
	require.NoError(t, err)
}

func TestWorker_ScheduleEvaluation_OdometerDue(t *testing.T) {
	db := newMaintTestDB(t)
	bus := newMockBus()
	insertTestVehicle(t, db, "veh-odo", "REG-ODO")

	// Seed schedule with due_km = 100
	dueKM := 100.0
	repo := maintsql.NewMaintenanceRepository(db)
	err := repo.SaveSchedule(context.Background(), domain.Schedule{
		ID:          "sched-1",
		VehicleID:   "veh-odo",
		ServiceType: "oil_change",
		DueKM:       &dueKM,
		Active:      true,
	})
	require.NoError(t, err)

	// Vehicle has telemetry odometer = 150 (> 100)
	_, err = db.Exec(`INSERT INTO telemetry_snapshots
		(id, vehicle_id, timestamp, latitude, longitude, speed, odometer)
		VALUES ('snap-1', 'veh-odo', CURRENT_TIMESTAMP, 19.0, 72.8, 50.0, 150.0)`)
	require.NoError(t, err)

	worker := NewWorker(db, bus, nil, 15, "P0A0F,P1602")
	worker.EvaluateSchedules(context.Background())

	// Assert vehicles.maintenance_due is set to today's date
	var maintDue sql.NullString
	err = db.QueryRow("SELECT maintenance_due FROM vehicles WHERE id = 'veh-odo'").Scan(&maintDue)
	require.NoError(t, err)
	assert.True(t, maintDue.Valid)
	assert.Contains(t, maintDue.String, time.Now().UTC().Format("2006-01-02"))

	// Assert event published
	dueEvents := bus.GetEvents("maintenance.due")
	require.Len(t, dueEvents, 1)
	payload := dueEvents[0].Payload.(map[string]interface{})
	assert.Equal(t, "veh-odo", payload["vehicle_id"])
	assert.Equal(t, "oil_change", payload["service_type"])
}

func TestWorker_ScheduleEvaluation_DateDue(t *testing.T) {
	db := newMaintTestDB(t)
	bus := newMockBus()
	insertTestVehicle(t, db, "veh-date", "REG-DATE")

	// Seed schedule with due_at = yesterday
	yesterday := time.Now().UTC().Add(-24 * time.Hour)
	repo := maintsql.NewMaintenanceRepository(db)
	err := repo.SaveSchedule(context.Background(), domain.Schedule{
		ID:          "sched-date",
		VehicleID:   "veh-date",
		ServiceType: "brake",
		DueAt:       &yesterday,
		Active:      true,
	})
	require.NoError(t, err)

	worker := NewWorker(db, bus, nil, 15, "P0A0F,P1602")
	worker.EvaluateSchedules(context.Background())

	var maintDue sql.NullString
	err = db.QueryRow("SELECT maintenance_due FROM vehicles WHERE id = 'veh-date'").Scan(&maintDue)
	require.NoError(t, err)
	assert.True(t, maintDue.Valid)
	assert.Contains(t, maintDue.String, time.Now().UTC().Format("2006-01-02"))

	dueEvents := bus.GetEvents("maintenance.due")
	require.Len(t, dueEvents, 1)
}

func TestWorker_DTCIntake_CriticalCode_And_Dedup(t *testing.T) {
	db := newMaintTestDB(t)
	bus := newMockBus()
	insertTestVehicle(t, db, "veh-dtc", "REG-DTC")

	worker := NewWorker(db, bus, nil, 15, "P0A0F,P1602")
	_ = worker

	// Ensure both timestamps fall strictly within the exact same minute
	minuteBase := time.Now().UTC().Truncate(time.Minute)
	t1 := minuteBase.Add(5 * time.Second)
	t2 := minuteBase.Add(35 * time.Second)

	dtcPayload := map[string]interface{}{
		"vehicle_id":  "veh-dtc",
		"dtc_code":    "P0A0F",
		"severity":    "critical",
		"description": "Engine Failed to Start",
		"occurred_at": t1.Format(time.RFC3339),
	}

	// 1. Publish critical DTC event
	bus.Publish(context.Background(), events.Event{
		Type:    "alert.dtc",
		Payload: dtcPayload,
	})

	// Verify dtc_events row inserted
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM dtc_events WHERE vehicle_id = 'veh-dtc'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify vehicles.maintenance_due is set
	var maintDue sql.NullString
	err = db.QueryRow("SELECT maintenance_due FROM vehicles WHERE id = 'veh-dtc'").Scan(&maintDue)
	require.NoError(t, err)
	assert.True(t, maintDue.Valid)

	// 2. Storm Dedup Test: publish same DTC 30 seconds later in the same minute
	dtcPayload2 := map[string]interface{}{
		"vehicle_id":  "veh-dtc",
		"dtc_code":    "P0A0F",
		"severity":    "critical",
		"description": "Engine Failed to Start",
		"occurred_at": t2.Format(time.RFC3339),
	}
	bus.Publish(context.Background(), events.Event{
		Type:    "alert.dtc",
		Payload: dtcPayload2,
	})

	// Count must STILL be 1 (deduped within 1-minute bucket)
	err = db.QueryRow("SELECT COUNT(*) FROM dtc_events WHERE vehicle_id = 'veh-dtc'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestWorker_ResolutionFlow(t *testing.T) {
	db := newMaintTestDB(t)
	bus := newMockBus()
	insertTestVehicle(t, db, "veh-res", "REG-RES")

	// Set vehicle as maintenance due
	repo := maintsql.NewMaintenanceRepository(db)
	err := repo.SetMaintenanceDue(context.Background(), "veh-res", time.Now().UTC())
	require.NoError(t, err)

	// Insert schedule with interval_km = 10000 and last_done_km = 0
	intKM := 10000.0
	zeroKM := 0.0
	err = repo.SaveSchedule(context.Background(), domain.Schedule{
		ID:          "sched-res",
		VehicleID:   "veh-res",
		ServiceType: "general",
		IntervalKM:  &intKM,
		LastDoneKM:  &zeroKM,
		Active:      true,
	})
	require.NoError(t, err)

	worker := NewWorker(db, bus, nil, 15, "P0A0F,P1602")

	// Log completed maintenance record at 1500 km
	recKM := 1500.0
	recCost := 2500.0
	recNotes := "General service complete"
	recVendor := "Workshop A"
	recBy := "user-admin"
	schedID := "sched-res"
	err = repo.InsertRecord(context.Background(), domain.Record{
		ID:          uuid.NewString(),
		VehicleID:   "veh-res",
		ScheduleID:  &schedID,
		ServiceType: "general",
		PerformedAt: time.Now().UTC(),
		OdometerKM:  &recKM,
		Cost:        &recCost,
		Vendor:      &recVendor,
		Notes:       &recNotes,
		RecordedBy:  &recBy,
	})
	require.NoError(t, err)

	// Run resolution check
	worker.EvaluateResolution(context.Background(), "veh-res")

	// Assert maintenance_due is CLEARED (NULL)
	var maintDue sql.NullString
	err = db.QueryRow("SELECT maintenance_due FROM vehicles WHERE id = 'veh-res'").Scan(&maintDue)
	require.NoError(t, err)
	assert.False(t, maintDue.Valid)

	// Assert maintenance.cleared event published
	clearedEvents := bus.GetEvents("maintenance.cleared")
	require.Len(t, clearedEvents, 1)
	payload := clearedEvents[0].Payload.(map[string]interface{})
	assert.Equal(t, "veh-res", payload["vehicle_id"])
}
