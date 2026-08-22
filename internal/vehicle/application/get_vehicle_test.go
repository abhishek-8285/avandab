package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
	"transport-app/internal/vehicle/domain"
	"transport-app/internal/vehicle/domain/aggregate"
)

func TestGetVehicleUseCase_Success(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	mileage := 1234.5
	rm := domain.VehicleReadModel{
		ID:                 "veh-1",
		RegistrationNumber: "MH01AB1234",
		VehicleNumber:      "VN-001",
		VehicleType:        string(aggregate.VehicleTypeTruck),
		Capacity:           10000,
		FuelType:           string(aggregate.FuelTypeDiesel),
		InsuranceExpiry:    now.Add(365 * 24 * time.Hour),
		FitnessExpiry:      now.Add(365 * 24 * time.Hour),
		PermitExpiry:       now.Add(365 * 24 * time.Hour),
		Status:             string(aggregate.VehicleAvailable),
		CurrentMileage:     &mileage,
		CreatedAt:          now,
		UpdatedAt:          now.Add(time.Hour),
	}
	repo := &mockVehicleRepo{
		getReadModelResult: rm,
	}
	uow := &mockUoW{provider: repo}
	uc := NewGetVehicleUseCase(uow)

	q := GetVehicleQuery{
		ID:       aggregate.VehicleID("veh-1"),
		TenantID: shared.TenantID("tenant-1"),
	}
	dto, err := uc.Execute(context.Background(), q)
	require.NoError(t, err)
	assert.Equal(t, "veh-1", dto.ID)
	assert.Equal(t, "MH01AB1234", dto.RegistrationNumber)
	assert.Equal(t, "VN-001", dto.VehicleNumber)
	assert.Equal(t, string(aggregate.VehicleTypeTruck), dto.VehicleType)
	assert.Equal(t, int64(10000), dto.Capacity)
	assert.Equal(t, string(aggregate.FuelTypeDiesel), dto.FuelType)
	assert.Equal(t, string(aggregate.VehicleAvailable), dto.Status)
	require.NotNil(t, dto.CurrentMileage)
	assert.InDelta(t, 1234.5, *dto.CurrentMileage, 0.001)
	assert.Equal(t, now, dto.CreatedAt)
	assert.Equal(t, now.Add(time.Hour), dto.UpdatedAt)
}

func TestGetVehicleUseCase_NilMileage(t *testing.T) {
	now := time.Now()
	rm := domain.VehicleReadModel{
		ID:                 "veh-nil",
		RegistrationNumber: "REG",
		VehicleNumber:      "VN",
		VehicleType:        "van",
		Capacity:           5,
		FuelType:           "petrol",
		InsuranceExpiry:    now,
		FitnessExpiry:      now,
		PermitExpiry:       now,
		Status:             "available",
		CurrentMileage:     nil,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	repo := &mockVehicleRepo{getReadModelResult: rm}
	uow := &mockUoW{provider: repo}
	uc := NewGetVehicleUseCase(uow)

	dto, err := uc.Execute(context.Background(), GetVehicleQuery{ID: "veh-nil", TenantID: "t1"})
	require.NoError(t, err)
	assert.Nil(t, dto.CurrentMileage)
	assert.Equal(t, "veh-nil", dto.ID)
}

func TestGetVehicleUseCase_RepoError(t *testing.T) {
	repo := &mockVehicleRepo{getReadModelErr: errors.New("not found")}
	uow := &mockUoW{provider: repo}
	uc := NewGetVehicleUseCase(uow)

	_, err := uc.Execute(context.Background(), GetVehicleQuery{ID: "nonexistent", TenantID: "t1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetVehicleUseCase_TypeAssertionFailure(t *testing.T) {
	uow := &mockUoW{provider: "bad"}
	uc := NewGetVehicleUseCase(uow)
	_, err := uc.Execute(context.Background(), GetVehicleQuery{ID: "veh-1", TenantID: "t1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to retrieve vehicle repository")
}

func TestGetVehicleUseCase_UoWError(t *testing.T) {
	uow := &mockUoW{execErr: errors.New("uow error")}
	uc := NewGetVehicleUseCase(uow)
	_, err := uc.Execute(context.Background(), GetVehicleQuery{ID: "veh-1", TenantID: "t1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uow error")
}
