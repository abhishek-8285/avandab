package application

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
	"transport-app/internal/vehicle/domain/aggregate"
)

type fakeDeleteRepo struct {
	mockVehicleRepo
	activeTrips int64
	guardErr    error
	deleted     []string
}

func (f *fakeDeleteRepo) CountActiveTripsForVehicle(ctx context.Context, tenantID shared.TenantID, vehicleID string) (int64, error) {
	if f.guardErr != nil {
		return 0, f.guardErr
	}
	return f.activeTrips, nil
}

func (f *fakeDeleteRepo) DeleteVehicleRecord(ctx context.Context, id string, tenantID string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeDeleteRepo) FlushVehicleEvents(ctx context.Context, v *aggregate.VehicleAggregate) error {
	v.ClearEvents()
	return nil
}

func deleteTestAgg() *aggregate.VehicleAggregate {
	now := time.Now()
	return aggregate.NewVehicleAggregate("v1", "t1", "REG", "VN",
		aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, now, now, now,
		aggregate.VehicleAvailable, nil, now)
}

func TestDeleteVehicle_Success(t *testing.T) {
	repo := &fakeDeleteRepo{}
	repo.findResult = deleteTestAgg()
	uc := NewDeleteVehicleUseCase(&mockUoW{provider: repo}, &mockClock{now: time.Now()})

	require.NoError(t, uc.Execute(context.Background(), "v1", "t1"))
	require.Equal(t, []string{"v1"}, repo.deleted)
}

func TestDeleteVehicle_BlockedByLiveTrips(t *testing.T) {
	repo := &fakeDeleteRepo{activeTrips: 2}
	repo.findResult = deleteTestAgg()
	uc := NewDeleteVehicleUseCase(&mockUoW{provider: repo}, &mockClock{now: time.Now()})

	err := uc.Execute(context.Background(), "v1", "t1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "live trip")
	assert.Empty(t, repo.deleted)
}

func TestDeleteVehicle_MissingTripsTableTolerated(t *testing.T) {
	repo := &fakeDeleteRepo{guardErr: sql.ErrNoRows}
	// Simulate minimal schemas: surface as "no such table".
	repo.guardErr = errNoSuchTable{}
	repo.findResult = deleteTestAgg()
	uc := NewDeleteVehicleUseCase(&mockUoW{provider: repo}, &mockClock{now: time.Now()})

	require.NoError(t, uc.Execute(context.Background(), "v1", "t1"))
	require.Equal(t, []string{"v1"}, repo.deleted)
}

type errNoSuchTable struct{}

func (errNoSuchTable) Error() string { return "SQL logic error: no such table: trips (1)" }

func TestDeleteVehicle_NotFound(t *testing.T) {
	repo := &fakeDeleteRepo{}
	repo.mockVehicleRepo.findErr = sql.ErrNoRows
	uc := NewDeleteVehicleUseCase(&mockUoW{provider: repo}, &mockClock{now: time.Now()})

	err := uc.Execute(context.Background(), "nope", "t1")
	require.ErrorIs(t, err, sql.ErrNoRows)
	assert.Empty(t, repo.deleted)
}
