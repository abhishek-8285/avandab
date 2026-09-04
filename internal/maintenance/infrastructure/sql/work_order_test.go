package sql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/maintenance/domain"
)

// Job-card lifecycle with tenant isolation and terminal immutability.
func TestWorkOrders_Lifecycle(t *testing.T) {
	db := maintTenantTestDB(t)
	_, err := db.Exec(`CREATE TABLE work_orders (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, vehicle_id TEXT NOT NULL,
		schedule_id TEXT, trip_id TEXT, title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
		assignee TEXT NOT NULL DEFAULT '', vendor TEXT NOT NULL DEFAULT '',
		cost_estimate REAL, cost_actual REAL,
		status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','assigned','in_progress','on_hold','done','cancelled')),
		due_at DATETIME, closed_at DATETIME,
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		updated_at DATETIME NOT NULL DEFAULT (datetime('now')))`)
	require.NoError(t, err)
	repo := NewMaintenanceRepository(db)
	ctx := context.Background()

	// Validation first.
	require.Error(t, repo.CreateWorkOrder(ctx, domain.WorkOrder{}))

	cost := 4500.0
	require.NoError(t, repo.CreateWorkOrder(ctx, domain.WorkOrder{
		ID: "wo-1", TenantID: "tenant-A", VehicleID: "va",
		Title: "Brake pad replacement", Assignee: "", CostEstimate: &cost,
	}))

	got, err := repo.FindWorkOrder(ctx, "tenant-A", "wo-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "open", got.Status)
	assert.Nil(t, got.ClosedAt)

	// Foreign org sees nothing.
	ghost, err := repo.FindWorkOrder(ctx, "tenant-B", "wo-1")
	require.NoError(t, err)
	assert.Nil(t, ghost)

	// Assign → in_progress → done, closed_at stamped.
	require.NoError(t, repo.AssignWorkOrder(ctx, "tenant-A", "wo-1", "Ramesh", "City Garage"))
	got, _ = repo.FindWorkOrder(ctx, "tenant-A", "wo-1")
	assert.Equal(t, "assigned", got.Status)
	assert.Equal(t, "Ramesh", got.Assignee)

	require.NoError(t, repo.TransitionWorkOrder(ctx, "tenant-A", "wo-1", domain.WorkOrderInProgress))
	require.NoError(t, repo.TransitionWorkOrder(ctx, "tenant-A", "wo-1", domain.WorkOrderDone))
	got, _ = repo.FindWorkOrder(ctx, "tenant-A", "wo-1")
	assert.Equal(t, "done", got.Status)
	assert.NotNil(t, got.ClosedAt)

	// Terminal cards are immutable, even cross-tenant attempts fail quietly.
	require.Error(t, repo.TransitionWorkOrder(ctx, "tenant-A", "wo-1", domain.WorkOrderOpen))
	require.Error(t, repo.AssignWorkOrder(ctx, "tenant-B", "wo-1", "X", "Y"))
	require.Error(t, repo.TransitionWorkOrder(ctx, "tenant-A", "wo-1", "flying"))

	// Status-filtered list.
	open, err := repo.ListWorkOrders(ctx, "tenant-A", "open", 10)
	require.NoError(t, err)
	assert.Empty(t, open)
	done, err := repo.ListWorkOrders(ctx, "tenant-A", "done", 10)
	require.NoError(t, err)
	require.Len(t, done, 1)

	// Empty tenant lists nothing.
	none, err := repo.ListWorkOrders(ctx, "", "", 10)
	require.NoError(t, err)
	assert.Empty(t, none)
}

// FindOpenWorkOrder pins repeat sweeps to one card per cause.
func TestWorkOrders_FindOpen(t *testing.T) {
	db := maintTenantTestDB(t)
	_, err := db.Exec(`CREATE TABLE work_orders (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, vehicle_id TEXT NOT NULL,
		schedule_id TEXT, trip_id TEXT, title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
		assignee TEXT NOT NULL DEFAULT '', vendor TEXT NOT NULL DEFAULT '',
		cost_estimate REAL, cost_actual REAL,
		status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','assigned','in_progress','on_hold','done','cancelled')),
		due_at DATETIME, closed_at DATETIME,
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		updated_at DATETIME NOT NULL DEFAULT (datetime('now')))`)
	require.NoError(t, err)
	repo := NewMaintenanceRepository(db)
	ctx := context.Background()

	// Empty tenant/vehicle find nothing (fail-closed).
	got, err := repo.FindOpenWorkOrder(ctx, "", "v1", "")
	require.NoError(t, err)
	assert.Nil(t, got)

	// Nothing open yet.
	got, err = repo.FindOpenWorkOrder(ctx, "tenant-A", "v1", "")
	require.NoError(t, err)
	assert.Nil(t, got)

	sched := "sched-1"
	require.NoError(t, repo.CreateWorkOrder(ctx, domain.WorkOrder{
		ID: "wo-open", TenantID: "tenant-A", VehicleID: "v1",
		ScheduleID: &sched, Title: "PM due",
	}))

	// Unpinned lookup finds it; foreign schedule does not.
	got, err = repo.FindOpenWorkOrder(ctx, "tenant-A", "v1", "")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "wo-open", got.ID)
	got, err = repo.FindOpenWorkOrder(ctx, "tenant-A", "v1", "sched-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	got, err = repo.FindOpenWorkOrder(ctx, "tenant-A", "v1", "sched-2")
	require.NoError(t, err)
	assert.Nil(t, got)

	// Foreign org sees nothing.
	got, err = repo.FindOpenWorkOrder(ctx, "tenant-B", "v1", "")
	require.NoError(t, err)
	assert.Nil(t, got)

	// Terminal cards drop out of the open set.
	require.NoError(t, repo.TransitionWorkOrder(ctx, "tenant-A", "wo-open", domain.WorkOrderDone))
	got, err = repo.FindOpenWorkOrder(ctx, "tenant-A", "v1", "sched-1")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// CompleteWorkOrder closes the card and writes the service record in one call.
func TestWorkOrders_Complete(t *testing.T) {
	db := maintTenantTestDB(t)
	_, err := db.Exec(`CREATE TABLE work_orders (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, vehicle_id TEXT NOT NULL,
		schedule_id TEXT, trip_id TEXT, title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
		assignee TEXT NOT NULL DEFAULT '', vendor TEXT NOT NULL DEFAULT '',
		cost_estimate REAL, cost_actual REAL,
		status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','assigned','in_progress','on_hold','done','cancelled')),
		due_at DATETIME, closed_at DATETIME,
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		updated_at DATETIME NOT NULL DEFAULT (datetime('now')))`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE maintenance_schedules (
		id TEXT PRIMARY KEY, vehicle_id TEXT NOT NULL, service_type TEXT NOT NULL,
		interval_km REAL, interval_days INTEGER, last_done_km REAL, last_done_at DATETIME,
		due_km REAL, due_at DATETIME, active INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		updated_at DATETIME NOT NULL DEFAULT (datetime('now')))`)
	require.NoError(t, err)
	repo := NewMaintenanceRepository(db)
	ctx := context.Background()

	// Empty tenant / unknown id fail closed.
	_, err = repo.CompleteWorkOrder(ctx, "", "wo-x", "u1")
	require.Error(t, err)
	_, err = repo.CompleteWorkOrder(ctx, "tenant-A", "wo-x", "u1")
	require.Error(t, err)

	sched := "sched-c"
	cost := 1200.0
	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id) VALUES ('v9', 'tenant-A')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO maintenance_schedules (id, vehicle_id, service_type, active) VALUES (?,?,?,1)`,
		sched, "v9", "tyre")
	require.NoError(t, err)
	require.NoError(t, repo.CreateWorkOrder(ctx, domain.WorkOrder{
		ID: "wo-c", TenantID: "tenant-A", VehicleID: "v9",
		ScheduleID: &sched, Title: "Tyre swap", Vendor: "City Garage", CostActual: &cost,
	}))

	done, err := repo.CompleteWorkOrder(ctx, "tenant-A", "wo-c", "u1")
	require.NoError(t, err)
	assert.Equal(t, "done", done.Status)
	assert.NotNil(t, done.ClosedAt)

	var svc, recSched, vendor, by string
	var recCost float64
	err = db.QueryRow(`SELECT service_type, schedule_id, vendor, recorded_by, cost FROM maintenance_records WHERE vehicle_id = 'v9'`).Scan(&svc, &recSched, &vendor, &by, &recCost)
	require.NoError(t, err, "complete must write the service record")
	assert.Equal(t, "tyre", svc)
	assert.Equal(t, sched, recSched)
	assert.Equal(t, "City Garage", vendor)
	assert.Equal(t, "u1", by)
	assert.Equal(t, cost, recCost)

	// Re-completing a terminal card fails.
	_, err = repo.CompleteWorkOrder(ctx, "tenant-A", "wo-c", "u1")
	require.Error(t, err)
}
