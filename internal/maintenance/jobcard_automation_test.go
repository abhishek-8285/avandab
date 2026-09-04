package maintenance

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/maintenance/domain"
	maintsql "transport-app/internal/maintenance/infrastructure/sql"
)

// A due schedule opens exactly one schedule-linked job card; repeat sweeps
// dedupe and the card carries the org.
func TestWorker_DueOpensOneJobCard(t *testing.T) {
	db := newMaintTestDB(t)
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-J','J','j')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
		VALUES ('vj','RJ','RJ','truck',15,date('now','+1 year'),date('now','+1 year'),date('now','+1 year'),'tenant-J')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO maintenance_schedules (id, vehicle_id, service_type, due_at, active)
		VALUES ('sched-j','vj','oil_change',datetime('now','-1 day'),1)`)
	require.NoError(t, err)

	bus := newMockBus()
	worker := NewWorker(db, bus, nil, 15, "")
	worker.EvaluateSchedules(context.Background())
	worker.EvaluateSchedules(context.Background())

	repo := maintsql.NewMaintenanceRepository(db)
	open, err := repo.FindOpenWorkOrder(context.Background(), "tenant-J", "vj", "sched-j")
	require.NoError(t, err)
	require.NotNil(t, open, "due schedule must open a job card")
	assert.Equal(t, "open", open.Status)
	require.NotNil(t, open.ScheduleID)
	assert.Equal(t, "sched-j", *open.ScheduleID)
	assert.Contains(t, open.Title, "oil_change")

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM work_orders WHERE tenant_id = 'tenant-J'`).Scan(&count))
	assert.Equal(t, 1, count, "repeat sweeps must not open duplicates")
}

// A critical DTC opens a schedule-less card; the storm dedupes on it and
// unknown-org vehicles get nothing.
func TestWorker_CriticalDTCOpensJobCard(t *testing.T) {
	db := newMaintTestDB(t)
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-K','K','k')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
		VALUES ('vk','RK','RK','truck',15,date('now','+1 year'),date('now','+1 year'),date('now','+1 year'),'tenant-K')`)
	require.NoError(t, err)

	bus := newMockBus()
	worker := NewWorker(db, bus, nil, 15, "P0A0F,P1602")

	payload := map[string]interface{}{
		"vehicle_id":  "vk",
		"dtc_code":    "P0A0F",
		"severity":    "critical",
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
	}
	require.NoError(t, worker.HandleDtcEvent(context.Background(), payload))
	// Same-code storm: deduped DTC insert means no second markVehicleDue.
	payload["occurred_at"] = time.Now().UTC().Add(time.Second).Format(time.RFC3339)
	require.NoError(t, worker.HandleDtcEvent(context.Background(), payload))

	repo := maintsql.NewMaintenanceRepository(db)
	open, err := repo.FindOpenWorkOrder(context.Background(), "tenant-K", "vk", "")
	require.NoError(t, err)
	require.NotNil(t, open, "critical DTC must open a job card")
	assert.Nil(t, open.ScheduleID)
	assert.Contains(t, open.Title, "engine")

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM work_orders WHERE tenant_id = 'tenant-K'`).Scan(&count))
	assert.Equal(t, 1, count)

	// Unknown vehicle (no org) → due flag skipped upstream; direct call is a no-op.
	worker.ensureJobCard(context.Background(), "", "ghost", "", "engine", "x", time.Now().UTC())
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM work_orders`).Scan(&count))
	assert.Equal(t, 1, count, "unknown org must not gain cards")
}

// Closing the card lets the next sweep open a fresh one (books close, then re-detect).
func TestWorker_ClosedCardReopens(t *testing.T) {
	db := newMaintTestDB(t)
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-L','L','l')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
		VALUES ('vl','RL','RL','truck',15,date('now','+1 year'),date('now','+1 year'),date('now','+1 year'),'tenant-L')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO maintenance_schedules (id, vehicle_id, service_type, due_at, active)
		VALUES ('sched-l','vl','brake',datetime('now','-1 day'),1)`)
	require.NoError(t, err)

	bus := newMockBus()
	worker := NewWorker(db, bus, nil, 15, "")
	ctx := context.Background()
	worker.EvaluateSchedules(ctx)

	repo := maintsql.NewMaintenanceRepository(db)
	open, err := repo.FindOpenWorkOrder(ctx, "tenant-L", "vl", "sched-l")
	require.NoError(t, err)
	require.NotNil(t, open)
	require.NoError(t, repo.TransitionWorkOrder(ctx, "tenant-L", open.ID, domain.WorkOrderDone))

	worker.EvaluateSchedules(ctx)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM work_orders WHERE tenant_id = 'tenant-L'`).Scan(&count))
	assert.Equal(t, 2, count, "still-due vehicle re-opens after close")
}
