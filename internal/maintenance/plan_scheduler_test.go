package maintenance

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/maintenance/domain"
	maintsql "transport-app/internal/maintenance/infrastructure/sql"
)

func setupTestDB(t *testing.T) *sql.DB {
	dbPath := filepath.Join(t.TempDir(), "maint_test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	schema := `
	CREATE TABLE tenants (id TEXT PRIMARY KEY, name TEXT);
	INSERT INTO tenants (id, name) VALUES ('tenant-1', 'Org 1'), ('tenant-2', 'Org 2');

	CREATE TABLE vehicles (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		registration_number TEXT NOT NULL,
		vehicle_number TEXT NOT NULL,
		vehicle_type TEXT NOT NULL,
		capacity REAL NOT NULL DEFAULT 1000,
		status TEXT NOT NULL DEFAULT 'available',
		maintenance_due DATETIME,
		maintenance_override_by TEXT,
		maintenance_override_at DATETIME,
		maintenance_override_reason TEXT
	);

	CREATE TABLE vehicle_measuring_points (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		vehicle_id TEXT NOT NULL REFERENCES vehicles(id),
		category TEXT NOT NULL DEFAULT 'M',
		kind TEXT NOT NULL,
		meas_position TEXT NOT NULL DEFAULT 'DISTANCE',
		unit TEXT NOT NULL DEFAULT 'KM',
		decimal_places INTEGER NOT NULL DEFAULT 0,
		annual_estimate REAL,
		count_backwards INTEGER NOT NULL DEFAULT 0,
		is_counter INTEGER NOT NULL DEFAULT 1,
		description TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE vehicle_measurements (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		point_id TEXT NOT NULL REFERENCES vehicle_measuring_points(id),
		doc_number TEXT,
		counter_reading REAL NOT NULL,
		difference_reading REAL,
		total_counter_reading REAL,
		measured_at DATETIME,
		read_by TEXT,
		remarks TEXT,
		recorded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		recorded_by TEXT
	);

	CREATE TABLE maintenance_schedules (
		id TEXT PRIMARY KEY,
		vehicle_id TEXT NOT NULL,
		service_type TEXT NOT NULL,
		interval_km REAL,
		interval_days INTEGER,
		last_done_km REAL,
		last_done_at DATETIME,
		due_km REAL,
		due_at DATETIME,
		active INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE maintenance_records (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		vehicle_id TEXT NOT NULL,
		schedule_id TEXT,
		service_type TEXT NOT NULL,
		performed_at DATETIME NOT NULL,
		odometer_km REAL,
		cost REAL,
		vendor TEXT,
		notes TEXT,
		recorded_by TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE maintenance_plans (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		plan_number TEXT NOT NULL,
		vehicle_id TEXT NOT NULL REFERENCES vehicles(id),
		measuring_point_id TEXT REFERENCES vehicle_measuring_points(id),
		service_type TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		cycle_interval_km REAL,
		cycle_interval_days INTEGER,
		call_horizon_percent REAL NOT NULL DEFAULT 100.0,
		last_scheduled_km REAL,
		last_scheduled_date DATETIME,
		next_due_km REAL,
		next_due_date DATETIME,
		status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(tenant_id, plan_number)
	);

	CREATE TABLE work_orders (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		vehicle_id TEXT NOT NULL,
		schedule_id TEXT,
		trip_id TEXT,
		plan_id TEXT REFERENCES maintenance_plans(id),
		due_km REAL,
		title TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		assignee TEXT NOT NULL DEFAULT '',
		vendor TEXT NOT NULL DEFAULT '',
		cost_estimate REAL,
		cost_actual REAL,
		status TEXT NOT NULL DEFAULT 'open',
		due_at DATETIME,
		closed_at DATETIME,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err = db.Exec(schema)
	require.NoError(t, err)
	return db
}

func TestPlanScheduler_CalculateProjection_AnnualEstimate(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	intervalKM := 10000.0
	targetKM := 20000.0
	plan := domain.MaintenancePlan{
		ID:                 "mp-1",
		TenantID:           "tenant-1",
		PlanNumber:         "MP-1001",
		VehicleID:          "veh-1",
		ServiceType:        "oil_change",
		CycleIntervalKM:    &intervalKM,
		NextDueKM:          &targetKM,
		CallHorizonPercent: 90.0,
		CreatedAt:          now,
	}

	// Annual estimate = 36,500 km/year -> 100 km/day
	annualEstimate := 36500.0
	currentOdo := 10000.0

	// 1. Initial evaluation: target KM = 20,000 km. Remaining = 10,000 km -> 100 days.
	dueKM, dueDate, needsCall := CalculateProjection(plan, currentOdo, annualEstimate, now)
	require.NotNil(t, dueKM)
	require.NotNil(t, dueDate)
	assert.Equal(t, 20000.0, *dueKM)
	expectedDueDate := now.AddDate(0, 0, 100)
	assert.Equal(t, expectedDueDate.Unix(), dueDate.Unix())
	assert.False(t, needsCall, "at 10,000 km, call threshold 19,000 km not yet reached")

	// 2. Advance odometer to 19,100 km (crosses 90% call horizon of 19,000 km)
	dueKM2, dueDate2, needsCall2 := CalculateProjection(plan, 19100.0, annualEstimate, now)
	require.NotNil(t, dueKM2)
	require.NotNil(t, dueDate2)
	assert.Equal(t, 20000.0, *dueKM2)
	assert.True(t, needsCall2, "at 19,100 km, call horizon 90% (19,000 km) reached")

	// 3. Odometer surpasses due threshold (20,500 km >= 20,000 km)
	dueKM3, dueDate3, needsCall3 := CalculateProjection(plan, 20500.0, annualEstimate, now)
	assert.Equal(t, 20000.0, *dueKM3)
	assert.Equal(t, now.Unix(), dueDate3.Unix(), "surpassed target KM projects immediate due date")
	assert.True(t, needsCall3)
}

func TestPlanScheduler_CalculateProjection_CalendarDays(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	intervalDays := 90
	plan := domain.MaintenancePlan{
		ID:                 "mp-cal",
		TenantID:           "tenant-1",
		PlanNumber:         "MP-CAL-01",
		VehicleID:          "veh-1",
		ServiceType:        "general",
		CycleIntervalDays:  &intervalDays,
		CallHorizonPercent: 100.0,
		CreatedAt:          now,
	}

	dueKM, dueDate, needsCall := CalculateProjection(plan, 5000.0, 0, now)
	assert.Nil(t, dueKM)
	require.NotNil(t, dueDate)
	expectedDue := now.AddDate(0, 0, 90)
	assert.Equal(t, expectedDue.Unix(), dueDate.Unix())
	assert.False(t, needsCall)

	// Call horizon 90% after 82 days
	asOf82 := now.AddDate(0, 0, 82)
	plan.CallHorizonPercent = 90.0
	_, _, needsCall82 := CalculateProjection(plan, 5000.0, 0, asOf82)
	assert.True(t, needsCall82, "after 82 days with 90% horizon, call triggered")
}

func TestPlanScheduler_EvaluatePlan_WorkOrderLifecycle(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	repo := maintsql.NewMaintenanceRepository(db)
	scheduler := NewPlanScheduler(db, repo)

	// Seed vehicle
	_, err := db.Exec(`
		INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type)
		VALUES ('veh-1', 'tenant-1', 'KA01TR9999', 'TR-9999', 'truck')
	`)
	require.NoError(t, err)

	// Seed measuring point with annual_estimate = 73,000 km/yr (200 km/day)
	_, err = db.Exec(`
		INSERT INTO vehicle_measuring_points (id, tenant_id, vehicle_id, kind, annual_estimate)
		VALUES ('mp-1', 'tenant-1', 'veh-1', 'ODO', 73000.0)
	`)
	require.NoError(t, err)

	// Seed measurement (odometer = 19,200 km)
	_, err = db.Exec(`
		INSERT INTO vehicle_measurements (id, tenant_id, point_id, counter_reading)
		VALUES ('meas-1', 'tenant-1', 'mp-1', 19200.0)
	`)
	require.NoError(t, err)

	mpID := "mp-1"
	intervalKM := 20000.0
	nextDueKM := 20000.0
	plan := domain.MaintenancePlan{
		ID:                 uuid.NewString(),
		TenantID:           "tenant-1",
		PlanNumber:         "MP-TRUCK-01",
		VehicleID:          "veh-1",
		MeasuringPointID:   &mpID,
		ServiceType:        "oil_change",
		Description:        "Heavy Duty Oil Change",
		CycleIntervalKM:    &intervalKM,
		NextDueKM:          &nextDueKM,
		CallHorizonPercent: 90.0, // 90% of 20,000 = 18,000 km threshold
		Status:             "active",
	}

	err = repo.CreateMaintenancePlan(ctx, plan)
	require.NoError(t, err)

	now := time.Now().UTC()

	// 1. Evaluate: 19,200 km >= 18,000 km call threshold -> triggers WorkOrder
	wo, err := scheduler.EvaluatePlan(ctx, "tenant-1", plan.ID, now)
	require.NoError(t, err)
	require.NotNil(t, wo)
	assert.Equal(t, domain.WorkOrderOpen, wo.Status)
	assert.Equal(t, "tenant-1", wo.TenantID)
	assert.Equal(t, "veh-1", wo.VehicleID)
	require.NotNil(t, wo.PlanID)
	assert.Equal(t, plan.ID, *wo.PlanID)
	assert.Contains(t, wo.Title, "IP41: oil_change - MP-TRUCK-01")

	// 2. Idempotent: repeated evaluation does not open duplicate work orders while one is active
	woRepeat, err := scheduler.EvaluatePlan(ctx, "tenant-1", plan.ID, now)
	require.NoError(t, err)
	assert.Nil(t, woRepeat, "subsequent evaluation before next cycle threshold must not trigger new call")

	workOrders, err := repo.ListWorkOrders(ctx, "tenant-1", "", 10)
	require.NoError(t, err)
	assert.Len(t, workOrders, 1)

	// 3. Multi-tenant isolation: Tenant 2 cannot evaluate or see Tenant 1's plan
	_, err = scheduler.EvaluatePlan(ctx, "tenant-2", plan.ID, now)
	require.Error(t, err, "foreign tenant must not access plan")
}
