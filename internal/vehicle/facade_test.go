package vehicle

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
	"transport-app/internal/vehicle/application"
	"transport-app/internal/vehicle/domain/aggregate"
)

const facadeTestSchema = `
CREATE TABLE vehicles (
    id TEXT PRIMARY KEY,
    registration_number TEXT NOT NULL,
    vehicle_number TEXT NOT NULL,
    vehicle_type TEXT NOT NULL,
    capacity INTEGER NOT NULL,
    fuel_type TEXT NOT NULL,
    insurance_expiry DATETIME NOT NULL,
    fitness_expiry DATETIME NOT NULL,
    permit_expiry DATETIME NOT NULL,
    status TEXT NOT NULL,
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

func setupFacadeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	safeName := strings.ReplaceAll(t.Name(), "/", "_")
	safeName = strings.ReplaceAll(safeName, " ", "_")
	dsn := "file:" + safeName + "?mode=memory&cache=shared&_pragma=journal_mode(WAL)"
	dbConn, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })
	_, err = dbConn.Exec(facadeTestSchema)
	require.NoError(t, err)
	return dbConn
}

func TestVehicleFacade_CreateAndUpdate(t *testing.T) {
	dbConn := setupFacadeTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(dbConn)
	idGen := id.NewUUIDGenerator()
	clk := clock.NewRealClock()

	createUC := application.NewCreateVehicleUseCase(unitOfWork, idGen, clk)
	updateUC := application.NewUpdateVehicleUseCase(unitOfWork, clk)

	facade := NewVehicleFacade(createUC, updateUC)
	ctx := context.Background()

	now := time.Now()
	mileage := 100.0

	// Create via facade
	cmd := CreateVehicleCommand{
		TenantID:           shared.TenantID("tenant-1"),
		RegistrationNumber: "MH01AB1234",
		VehicleNumber:      "VN-001",
		VehicleType:        aggregate.VehicleTypeTruck,
		Capacity:           5000,
		FuelType:           aggregate.FuelTypeDiesel,
		InsuranceExpiry:    now.Add(365 * 24 * time.Hour),
		FitnessExpiry:      now.Add(365 * 24 * time.Hour),
		PermitExpiry:       now.Add(365 * 24 * time.Hour),
		CurrentMileage:     &mileage,
	}
	vid, err := facade.CreateVehicle(ctx, cmd)
	require.NoError(t, err)
	assert.NotEmpty(t, vid)

	// Verify via list use case
	listUC := application.NewListVehiclesUseCase(unitOfWork)
	res, err := listUC.Execute(ctx, application.ListVehiclesQuery{TenantID: "tenant-1", Page: 1, Limit: 10})
	require.NoError(t, err)
	require.Len(t, res.Vehicles, 1)
	assert.Equal(t, string(vid), res.Vehicles[0].ID)

	// Update via facade
	newMileage := 200.0
	updateCmd := UpdateVehicleCommand{
		ID:                 vid,
		TenantID:           shared.TenantID("tenant-1"),
		RegistrationNumber: "MH01AB9999",
		VehicleNumber:      "VN-999",
		VehicleType:        aggregate.VehicleTypeBus,
		Capacity:           40,
		FuelType:           aggregate.FuelTypeCNG,
		InsuranceExpiry:    now.Add(200 * 24 * time.Hour),
		FitnessExpiry:      now.Add(200 * 24 * time.Hour),
		PermitExpiry:       now.Add(200 * 24 * time.Hour),
		Status:             aggregate.VehicleMaintenance,
		CurrentMileage:     &newMileage,
	}
	err = facade.UpdateVehicle(ctx, updateCmd)
	require.NoError(t, err)

	getUC := application.NewGetVehicleUseCase(unitOfWork)
	dto, err := getUC.Execute(ctx, application.GetVehicleQuery{ID: vid, TenantID: "tenant-1"})
	require.NoError(t, err)
	assert.Equal(t, "MH01AB9999", dto.RegistrationNumber)
	assert.Equal(t, "VN-999", dto.VehicleNumber)
	assert.Equal(t, string(aggregate.VehicleMaintenance), dto.Status)
	require.NotNil(t, dto.CurrentMileage)
	assert.InDelta(t, 200.0, *dto.CurrentMileage, 0.001)
}

func TestVehicleFacade_CreateValidationError(t *testing.T) {
	dbConn := setupFacadeTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(dbConn)
	idGen := id.NewUUIDGenerator()
	clk := clock.NewRealClock()

	createUC := application.NewCreateVehicleUseCase(unitOfWork, idGen, clk)
	updateUC := application.NewUpdateVehicleUseCase(unitOfWork, clk)
	facade := NewVehicleFacade(createUC, updateUC)

	// Empty registration should fail
	_, err := facade.CreateVehicle(context.Background(), CreateVehicleCommand{
		TenantID:           "t1",
		RegistrationNumber: "",
		VehicleNumber:      "VN1",
		VehicleType:        aggregate.VehicleTypeTruck,
		Capacity:           10,
		FuelType:           aggregate.FuelTypeDiesel,
		InsuranceExpiry:    time.Now(),
		FitnessExpiry:      time.Now(),
		PermitExpiry:       time.Now(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "registration number and vehicle number are required")
}

func TestVehicleFacade_UpdateValidationError(t *testing.T) {
	dbConn := setupFacadeTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(dbConn)
	idGen := id.NewUUIDGenerator()
	clk := clock.NewRealClock()

	createUC := application.NewCreateVehicleUseCase(unitOfWork, idGen, clk)
	updateUC := application.NewUpdateVehicleUseCase(unitOfWork, clk)
	facade := NewVehicleFacade(createUC, updateUC)

	ctx := context.Background()
	now := time.Now()
	vid, err := facade.CreateVehicle(ctx, CreateVehicleCommand{
		TenantID:           "t1",
		RegistrationNumber: "REG1",
		VehicleNumber:      "VN1",
		VehicleType:        aggregate.VehicleTypeTruck,
		Capacity:           10,
		FuelType:           aggregate.FuelTypeDiesel,
		InsuranceExpiry:    now,
		FitnessExpiry:      now,
		PermitExpiry:       now,
	})
	require.NoError(t, err)

	// Update with empty vehicle number should fail
	err = facade.UpdateVehicle(ctx, UpdateVehicleCommand{
		ID:                 vid,
		TenantID:           "t1",
		RegistrationNumber: "REG2",
		VehicleNumber:      "",
		VehicleType:        aggregate.VehicleTypeTruck,
		Capacity:           10,
		FuelType:           aggregate.FuelTypeDiesel,
		InsuranceExpiry:    now,
		FitnessExpiry:      now,
		PermitExpiry:       now,
		Status:             aggregate.VehicleAvailable,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "registration and vehicle number are required")
}
