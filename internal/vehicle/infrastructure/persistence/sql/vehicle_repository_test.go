package sql

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/vehicle/domain/aggregate"
)

const vehicleTestSchema = `
CREATE TABLE vehicles (
    id TEXT PRIMARY KEY,
    registration_number TEXT NOT NULL UNIQUE,
    vehicle_number TEXT NOT NULL,
    vehicle_type TEXT NOT NULL CHECK (vehicle_type IN ('truck', 'mini_truck', 'bus', 'van', 'pickup', 'tempo')),
    capacity INTEGER NOT NULL,
    fuel_type TEXT NOT NULL CHECK (fuel_type IN ('diesel', 'petrol', 'gas', 'electric', 'cng')),
    insurance_expiry DATETIME NOT NULL,
    fitness_expiry DATETIME NOT NULL,
    permit_expiry DATETIME NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('available', 'running', 'maintenance', 'inactive')),
    current_mileage REAL,
    tenant_id TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE outbox_events (
    id TEXT PRIMARY KEY,
    aggregate_id TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,
    published_at DATETIME
);
`

func setupVehicleTestDB(t *testing.T) *sql.DB {
	t.Helper()
	safeName := strings.ReplaceAll(t.Name(), "/", "_")
	safeName = strings.ReplaceAll(safeName, " ", "_")
	safeName = strings.ReplaceAll(safeName, "-", "_")
	dsn := "file:" + safeName + "?mode=memory&cache=shared&_pragma=journal_mode(WAL)"
	dbConn, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })
	_, err = dbConn.Exec(vehicleTestSchema)
	require.NoError(t, err)
	return dbConn
}

func newTestVehicleAgg(id, tenantID, reg, vehNum string, vehType aggregate.VehicleType, capacity int64, fuel aggregate.FuelType, status aggregate.VehicleStatus, mileage *float64, now time.Time) *aggregate.VehicleAggregate {
	return aggregate.NewVehicleAggregate(
		aggregate.VehicleID(id),
		shared.TenantID(tenantID),
		reg, vehNum, vehType, capacity, fuel,
		now.Add(365*24*time.Hour),
		now.Add(365*24*time.Hour),
		now.Add(365*24*time.Hour),
		status, mileage, now,
	)
}

// ---------------------------------------------------------------------------
// Save / Find
// ---------------------------------------------------------------------------

func TestVehicleRepository_Save_CreateAndFind(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	mileage := 12345.67
	agg := newTestVehicleAgg("veh-1", "tenant-1", "MH01AB1234", "VN-001", aggregate.VehicleTypeTruck, 10000, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, &mileage, now)
	require.Len(t, agg.Events(), 1)

	require.NoError(t, repo.Save(ctx, agg))
	// Events cleared
	assert.Len(t, agg.Events(), 0)

	found, err := repo.Find(ctx, "veh-1", "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, "MH01AB1234", found.RegistrationNumber)
	assert.Equal(t, "VN-001", found.VehicleNumber)
	assert.Equal(t, aggregate.VehicleTypeTruck, found.VehicleType)
	assert.Equal(t, int64(10000), found.Capacity)
	assert.Equal(t, aggregate.FuelTypeDiesel, found.FuelType)
	assert.Equal(t, aggregate.VehicleAvailable, found.Status)
	require.NotNil(t, found.CurrentMileage)
	assert.InDelta(t, 12345.67, *found.CurrentMileage, 0.001)
	assert.Equal(t, shared.TenantID("tenant-1"), found.TenantID)

	// outbox should have one event
	var outboxCount int
	err = dbConn.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = ?`, "veh-1").Scan(&outboxCount)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, outboxCount, 1)
}

func TestVehicleRepository_Save_CreateNilMileage(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()

	agg := newTestVehicleAgg("veh-nil", "tenant-1", "MH02CD5678", "VN-002", aggregate.VehicleTypeBus, 40, aggregate.FuelTypeCNG, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	found, err := repo.Find(ctx, "veh-nil", "tenant-1")
	require.NoError(t, err)
	assert.Nil(t, found.CurrentMileage)

	rm, err := repo.GetReadModel(ctx, "veh-nil", "tenant-1")
	require.NoError(t, err)
	assert.Nil(t, rm.CurrentMileage)
}

func TestVehicleRepository_Save_UpdateSuccess(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	agg := newTestVehicleAgg("veh-1", "tenant-1", "MH01AB1234", "VN-001", aggregate.VehicleTypeTruck, 5000, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	// Load and update
	found, err := repo.Find(ctx, "veh-1", "tenant-1")
	require.NoError(t, err)
	// Need to ensure found has no pending events (ToDomain creates 1, but we may want to clear)
	found.ClearEvents()

	newMileage := 9999.0
	newInsurance := now.Add(500 * 24 * time.Hour)
	err = found.UpdateDetails("MH01AB9999", "VN-999", aggregate.VehicleTypeVan, 2000, aggregate.FuelTypePetrol, newInsurance, newInsurance, newInsurance, aggregate.VehicleMaintenance, &newMileage, now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, found.Events(), 1)

	require.NoError(t, repo.Save(ctx, found))
	assert.Len(t, found.Events(), 0)

	updated, err := repo.Find(ctx, "veh-1", "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, "MH01AB9999", updated.RegistrationNumber)
	assert.Equal(t, "VN-999", updated.VehicleNumber)
	assert.Equal(t, aggregate.VehicleTypeVan, updated.VehicleType)
	assert.Equal(t, int64(2000), updated.Capacity)
	assert.Equal(t, aggregate.FuelTypePetrol, updated.FuelType)
	assert.Equal(t, aggregate.VehicleMaintenance, updated.Status)
	require.NotNil(t, updated.CurrentMileage)
	assert.InDelta(t, 9999.0, *updated.CurrentMileage, 0.001)

	// outbox should have at least 2 events (create + update)
	var count int
	err = dbConn.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = ?`, "veh-1").Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 2)
}

func TestVehicleRepository_Save_UpdateMileageNilToValueAndBack(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()

	agg := newTestVehicleAgg("veh-1", "t1", "REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	found, err := repo.Find(ctx, "veh-1", "t1")
	require.NoError(t, err)
	found.ClearEvents()
	mileage := 555.0
	require.NoError(t, found.UpdateDetails("REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, now, now, now, aggregate.VehicleAvailable, &mileage, now))
	require.NoError(t, repo.Save(ctx, found))

	found2, err := repo.Find(ctx, "veh-1", "t1")
	require.NoError(t, err)
	require.NotNil(t, found2.CurrentMileage)
	assert.InDelta(t, 555.0, *found2.CurrentMileage, 0.001)

	found2.ClearEvents()
	require.NoError(t, found2.UpdateDetails("REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, now, now, now, aggregate.VehicleAvailable, nil, now))
	require.NoError(t, repo.Save(ctx, found2))

	found3, err := repo.Find(ctx, "veh-1", "t1")
	require.NoError(t, err)
	assert.Nil(t, found3.CurrentMileage)
}

func TestVehicleRepository_Save_ErrorClosedDB_Create(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newTestVehicleAgg("veh-1", "t1", "REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	_ = dbConn.Close()
	err := repo.Save(ctx, agg)
	require.Error(t, err)
}

func TestVehicleRepository_Save_ErrorClosedDB_Update(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newTestVehicleAgg("veh-1", "t1", "REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	_ = dbConn.Close()
	// Need to update; Find will also fail, but Save's first GetVehicleByID will fail with closed DB not ErrNoRows, so it returns error
	agg2 := newTestVehicleAgg("veh-1", "t1", "REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	// Ensure agg2 will try to update (existing) path, but DB closed => GetVehicleByID error not ErrNoRows => Save returns error
	// However Save checks GetVehicleByID to decide create vs update; with closed DB, it will hit else return err before create/update branching
	// To reach update error path we need Get succeeds then Update fails, but with closed DB Get fails earlier.
	// So we test the early error
	err := repo.Save(ctx, agg2)
	require.Error(t, err)
}

func TestVehicleRepository_Save_UpdateErrorClosedDB_AfterFind(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newTestVehicleAgg("veh-1", "t1", "REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	found, err := repo.Find(ctx, "veh-1", "t1")
	require.NoError(t, err)
	found.ClearEvents()
	require.NoError(t, found.UpdateDetails("REG2", "VN2", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, now, now, now, aggregate.VehicleAvailable, nil, now))
	_ = dbConn.Close()
	err = repo.Save(ctx, found)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Find
// ---------------------------------------------------------------------------

func TestVehicleRepository_Find_Success(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newTestVehicleAgg("veh-1", "t1", "REG-A", "VN-A", aggregate.VehicleTypePickup, 5, aggregate.FuelTypePetrol, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	found, err := repo.Find(ctx, "veh-1", "t1")
	require.NoError(t, err)
	assert.Equal(t, "REG-A", found.RegistrationNumber)
	assert.Equal(t, "t1", string(found.TenantID))
}

func TestVehicleRepository_Find_NotFound(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	_, err := repo.Find(ctx, "nonexistent", "t1")
	require.Error(t, err)
	assert.True(t, err == sql.ErrNoRows || strings.Contains(err.Error(), "no rows"))
}

func TestVehicleRepository_Find_TenantIsolation(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newTestVehicleAgg("veh-1", "tenant-1", "REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	_, err := repo.Find(ctx, "veh-1", "tenant-2")
	require.Error(t, err)

	found, err := repo.Find(ctx, "veh-1", "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, "REG1", found.RegistrationNumber)
}

func TestVehicleRepository_Find_ErrorClosedDB(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newTestVehicleAgg("veh-1", "t1", "REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))
	_ = dbConn.Close()
	_, err := repo.Find(ctx, "veh-1", "t1")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// GetReadModel
// ---------------------------------------------------------------------------

func TestVehicleRepository_GetReadModel_Success(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	mileage := 777.0
	agg := newTestVehicleAgg("veh-1", "t1", "REG-GET", "VN-GET", aggregate.VehicleTypeTempo, 3000, aggregate.FuelTypeGas, aggregate.VehicleRunning, &mileage, now)
	require.NoError(t, repo.Save(ctx, agg))

	rm, err := repo.GetReadModel(ctx, "veh-1", "t1")
	require.NoError(t, err)
	assert.Equal(t, "veh-1", rm.ID)
	assert.Equal(t, "REG-GET", rm.RegistrationNumber)
	assert.Equal(t, "VN-GET", rm.VehicleNumber)
	assert.Equal(t, string(aggregate.VehicleTypeTempo), rm.VehicleType)
	assert.Equal(t, int64(3000), rm.Capacity)
	assert.Equal(t, string(aggregate.FuelTypeGas), rm.FuelType)
	assert.Equal(t, string(aggregate.VehicleRunning), rm.Status)
	require.NotNil(t, rm.CurrentMileage)
	assert.InDelta(t, 777.0, *rm.CurrentMileage, 0.001)
	assert.False(t, rm.CreatedAt.IsZero())
	assert.False(t, rm.UpdatedAt.IsZero())
}

func TestVehicleRepository_GetReadModel_NilMileage(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newTestVehicleAgg("veh-nil", "t1", "REG-NIL", "VN-NIL", aggregate.VehicleTypeVan, 10, aggregate.FuelTypePetrol, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))
	rm, err := repo.GetReadModel(ctx, "veh-nil", "t1")
	require.NoError(t, err)
	assert.Nil(t, rm.CurrentMileage)
}

func TestVehicleRepository_GetReadModel_NotFound(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	_, err := repo.GetReadModel(ctx, "nope", "t1")
	require.Error(t, err)
}

func TestVehicleRepository_GetReadModel_TenantIsolation(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newTestVehicleAgg("veh-1", "tenant-1", "REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))
	_, err := repo.GetReadModel(ctx, "veh-1", "tenant-2")
	require.Error(t, err)
	found, err := repo.GetReadModel(ctx, "veh-1", "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, "REG1", found.RegistrationNumber)
}

func TestVehicleRepository_GetReadModel_ErrorClosedDB(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newTestVehicleAgg("veh-1", "t1", "REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))
	_ = dbConn.Close()
	_, err := repo.GetReadModel(ctx, "veh-1", "t1")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// SearchReadModels
// ---------------------------------------------------------------------------

func TestVehicleRepository_SearchReadModels_Pagination(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	// Create 3 vehicles for tenant 1 with staggered created_at via direct inserts to control ordering
	// Use repo.Save but ensure created_at ordering: sleep or use different now values
	agg1 := newTestVehicleAgg("veh-1", "t1", "REG-001", "VN-001", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now.Add(-3*time.Hour))
	agg2 := newTestVehicleAgg("veh-2", "t1", "REG-002", "VN-002", aggregate.VehicleTypeBus, 40, aggregate.FuelTypeCNG, aggregate.VehicleAvailable, nil, now.Add(-2*time.Hour))
	agg3 := newTestVehicleAgg("veh-3", "t1", "REG-003", "VN-003", aggregate.VehicleTypeVan, 5, aggregate.FuelTypePetrol, aggregate.VehicleMaintenance, nil, now.Add(-1*time.Hour))
	aggOther := newTestVehicleAgg("veh-4", "t2", "REG-OTHER", "VN-OTHER", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)

	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))
	require.NoError(t, repo.Save(ctx, agg3))
	require.NoError(t, repo.Save(ctx, aggOther))

	// Search uses ORDER BY created_at DESC, so veh-3 should be first (most recent among t1)
	models, total, err := repo.SearchReadModels(ctx, "t1", "", "", 2, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, models, 2)

	models, total, err = repo.SearchReadModels(ctx, "t1", "", "", 2, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, models, 1)

	models, total, err = repo.SearchReadModels(ctx, "t1", "", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, models, 3)

	models, total, err = repo.SearchReadModels(ctx, "t2", "", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, models, 1)
	assert.Equal(t, "veh-4", models[0].ID)

	models, total, err = repo.SearchReadModels(ctx, "t1", "", "", 10, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, models, 0)
}

func TestVehicleRepository_SearchReadModels_FilterByQuery(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()

	agg1 := newTestVehicleAgg("veh-1", "t1", "MH01AB1234", "VN-001", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	agg2 := newTestVehicleAgg("veh-2", "t1", "MH02CD5678", "VN-002", aggregate.VehicleTypeBus, 40, aggregate.FuelTypeCNG, aggregate.VehicleAvailable, nil, now)
	agg3 := newTestVehicleAgg("veh-3", "t1", "KA05EF9012", "VN-003", aggregate.VehicleTypeVan, 5, aggregate.FuelTypePetrol, aggregate.VehicleAvailable, nil, now)

	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))
	require.NoError(t, repo.Save(ctx, agg3))

	// Search by registration fragment
	models, total, err := repo.SearchReadModels(ctx, "t1", "MH01AB", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	assert.Equal(t, "veh-1", models[0].ID)

	// Search by partial registration "MH" matches 2
	models, total, err = repo.SearchReadModels(ctx, "t1", "MH", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, models, 2)

	// Search by vehicle_number
	models, total, err = repo.SearchReadModels(ctx, "t1", "VN-002", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "veh-2", models[0].ID)

	// Search by vehicle_type
	models, total, err = repo.SearchReadModels(ctx, "t1", "truck", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "veh-1", models[0].ID)

	models, total, err = repo.SearchReadModels(ctx, "t1", "bus", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "veh-2", models[0].ID)

	// Non-matching
	models, total, err = repo.SearchReadModels(ctx, "t1", "NONEXISTENTXYZ", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Len(t, models, 0)

	// Empty query should match all
	models, total, err = repo.SearchReadModels(ctx, "t1", "", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, models, 3)
}

func TestVehicleRepository_SearchReadModels_FilterByStatus(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()

	agg1 := newTestVehicleAgg("veh-1", "t1", "REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	agg2 := newTestVehicleAgg("veh-2", "t1", "REG2", "VN2", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleRunning, nil, now)
	agg3 := newTestVehicleAgg("veh-3", "t1", "REG3", "VN3", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleMaintenance, nil, now)

	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))
	require.NoError(t, repo.Save(ctx, agg3))

	models, total, err := repo.SearchReadModels(ctx, "t1", "", "available", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	assert.Equal(t, "available", models[0].Status)

	models, total, err = repo.SearchReadModels(ctx, "t1", "", "running", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "veh-2", models[0].ID)

	models, total, err = repo.SearchReadModels(ctx, "t1", "", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, models, 3)

	// Combined query + status
	models, total, err = repo.SearchReadModels(ctx, "t1", "REG", "available", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, models, 1)

	models, total, err = repo.SearchReadModels(ctx, "t1", "", "inactive", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Len(t, models, 0)
}

func TestVehicleRepository_SearchReadModels_MileageMapping(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	mileage := 123.45
	agg1 := newTestVehicleAgg("veh-1", "t1", "REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, &mileage, now)
	agg2 := newTestVehicleAgg("veh-2", "t1", "REG2", "VN2", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))

	models, total, err := repo.SearchReadModels(ctx, "t1", "", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, models, 2)
	// Find specific
	var foundWith, foundNil bool
	for _, m := range models {
		if m.ID == "veh-1" {
			require.NotNil(t, m.CurrentMileage)
			assert.InDelta(t, 123.45, *m.CurrentMileage, 0.001)
			foundWith = true
		}
		if m.ID == "veh-2" {
			assert.Nil(t, m.CurrentMileage)
			foundNil = true
		}
	}
	assert.True(t, foundWith)
	assert.True(t, foundNil)
}

func TestVehicleRepository_SearchReadModels_ErrorClosedDB(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	_ = dbConn.Close()
	_, _, err := repo.SearchReadModels(ctx, "t1", "", "", 10, 0)
	require.Error(t, err)
}

func TestVehicleRepository_SearchReadModels_TenantIsolationWithQuery(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg1 := newTestVehicleAgg("veh-1", "tenant-1", "SHARED-REG-T1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	agg2 := newTestVehicleAgg("veh-2", "tenant-2", "SHARED-REG-T2", "VN2", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))

	models, total, err := repo.SearchReadModels(ctx, "tenant-1", "SHARED-REG", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "veh-1", models[0].ID)

	models, total, err = repo.SearchReadModels(ctx, "tenant-2", "SHARED-REG", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "veh-2", models[0].ID)
}

func TestVehicleRepository_Q_WithTx(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repoI := NewVehicleRepository(dbConn)
	repo, ok := repoI.(*vehicleRepository)
	require.True(t, ok)
	ctx := context.Background()
	now := time.Now()
	agg := newTestVehicleAgg("veh-tx", "t1", "REG-TX", "VN-TX", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	tx, err := dbConn.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	txCtx := repository.WithTxInContext(ctx, tx)

	// Q should return tx-bound queries when transaction present
	qTx := repo.Q(txCtx)
	require.NotNil(t, qTx)
	qNoTx := repo.Q(ctx)
	require.NotNil(t, qNoTx)
	assert.NotEqual(t, qTx, qNoTx)

	// Ensure Find works with tx context
	found, err := repo.Find(txCtx, "veh-tx", "t1")
	require.NoError(t, err)
	assert.Equal(t, "REG-TX", found.RegistrationNumber)

	// Save inside transaction should use tx-bound queries
	mileage := 123.0
	found.ClearEvents()
	require.NoError(t, found.UpdateDetails("REG-TX2", "VN-TX2", aggregate.VehicleTypeBus, 20, aggregate.FuelTypeCNG, now, now, now, aggregate.VehicleAvailable, &mileage, now))
	require.NoError(t, repo.Save(txCtx, found))

	// Rollback should revert update
	_ = tx.Rollback()
	// After rollback, original should still be visible
	found2, err := repo.Find(ctx, "veh-tx", "t1")
	require.NoError(t, err)
	assert.Equal(t, "REG-TX", found2.RegistrationNumber)

	// Now test commit path
	tx2, err := dbConn.Begin()
	require.NoError(t, err)
	txCtx2 := repository.WithTxInContext(ctx, tx2)
	found3, err := repo.Find(txCtx2, "veh-tx", "t1")
	require.NoError(t, err)
	found3.ClearEvents()
	require.NoError(t, found3.UpdateDetails("REG-TX3", "VN-TX3", aggregate.VehicleTypeVan, 30, aggregate.FuelTypePetrol, now, now, now, aggregate.VehicleRunning, nil, now))
	require.NoError(t, repo.Save(txCtx2, found3))
	require.NoError(t, tx2.Commit())

	found4, err := repo.Find(ctx, "veh-tx", "t1")
	require.NoError(t, err)
	assert.Equal(t, "REG-TX3", found4.RegistrationNumber)
}

func TestVehicleRepository_Save_CreateError_DuplicateOrInvalid(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()

	// Create first vehicle
	agg := newTestVehicleAgg("veh-1", "t1", "REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	// Try to create second vehicle with same ID but invalid vehicle_type should fail on create path
	// Use non-existent ID with invalid type
	badAgg := newTestVehicleAgg("veh-bad", "t1", "REG-BAD", "VN-BAD", aggregate.VehicleType("invalid_type_xyz"), 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	err := repo.Save(ctx, badAgg)
	require.Error(t, err)
}

func TestVehicleRepository_Save_UpdateError_InvalidType(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()

	agg := newTestVehicleAgg("veh-1", "t1", "REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	found, err := repo.Find(ctx, "veh-1", "t1")
	require.NoError(t, err)
	found.ClearEvents()
	// Set invalid vehicle type to trigger CHECK constraint failure on update
	found.VehicleType = aggregate.VehicleType("invalid")
	found.UpdatedAt = now
	// Manually add event to ensure outbox would be attempted, but update should fail before outbox
	found.ClearEvents()
	// Use UpdateDetails with invalid type via direct field manipulation plus Save
	// Since UpdateDetails doesn't validate vehicleType, we set it directly
	err = repo.Save(ctx, found)
	require.Error(t, err)
}

func TestVehicleRepository_Save_OutboxError(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn)
	ctx := context.Background()
	now := time.Now()

	agg := newTestVehicleAgg("veh-1", "t1", "REG1", "VN1", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	// Drop outbox table to cause outbox SaveEvents to fail
	_, err := dbConn.Exec(`DROP TABLE outbox_events`)
	require.NoError(t, err)

	found, err := repo.Find(ctx, "veh-1", "t1")
	require.NoError(t, err)
	found.ClearEvents()
	require.NoError(t, found.UpdateDetails("REG2", "VN2", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, now, now, now, aggregate.VehicleAvailable, nil, now))
	err = repo.Save(ctx, found)
	require.Error(t, err)
}
