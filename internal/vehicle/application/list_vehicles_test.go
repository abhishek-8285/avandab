package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/vehicle/domain"
)

func TestListVehiclesUseCase_DefaultPagination(t *testing.T) {
	repo := &mockVehicleRepo{
		searchResult: []domain.VehicleReadModel{},
		searchTotal:  0,
	}
	uow := &mockUoW{provider: repo}
	uc := NewListVehiclesUseCase(uow)

	_, err := uc.Execute(context.Background(), ListVehiclesQuery{
		TenantID: "t1",
		Page:     0,
		Limit:    0,
		Search:   "",
		Status:   "",
	})
	require.NoError(t, err)
	assert.Equal(t, 10, repo.capturedLimit)
	assert.Equal(t, 0, repo.capturedOffset)

	// Page 2, limit 5 => offset 5
	_, err = uc.Execute(context.Background(), ListVehiclesQuery{
		TenantID: "t1",
		Page:     2,
		Limit:    5,
	})
	require.NoError(t, err)
	assert.Equal(t, 5, repo.capturedLimit)
	assert.Equal(t, 5, repo.capturedOffset)

	// Page 3, limit 10 => offset 20
	_, err = uc.Execute(context.Background(), ListVehiclesQuery{
		TenantID: "t1",
		Page:     3,
		Limit:    10,
	})
	require.NoError(t, err)
	assert.Equal(t, 10, repo.capturedLimit)
	assert.Equal(t, 20, repo.capturedOffset)
}

func TestListVehiclesUseCase_SuccessMapping(t *testing.T) {
	now := time.Now()
	mileage := 100.0
	rm1 := domain.VehicleReadModel{
		ID:                 "veh-1",
		RegistrationNumber: "REG1",
		VehicleNumber:      "VN1",
		VehicleType:        "truck",
		Capacity:           1000,
		FuelType:           "diesel",
		InsuranceExpiry:    now,
		FitnessExpiry:      now,
		PermitExpiry:       now,
		Status:             "available",
		CurrentMileage:     &mileage,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	rm2 := domain.VehicleReadModel{
		ID:                 "veh-2",
		RegistrationNumber: "REG2",
		VehicleNumber:      "VN2",
		VehicleType:        "bus",
		Capacity:           40,
		FuelType:           "cng",
		InsuranceExpiry:    now,
		FitnessExpiry:      now,
		PermitExpiry:       now,
		Status:             "maintenance",
		CurrentMileage:     nil,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	repo := &mockVehicleRepo{
		searchResult: []domain.VehicleReadModel{rm1, rm2},
		searchTotal:  2,
	}
	uow := &mockUoW{provider: repo}
	uc := NewListVehiclesUseCase(uow)

	res, err := uc.Execute(context.Background(), ListVehiclesQuery{
		TenantID: "t1",
		Page:     1,
		Limit:    10,
		Search:   "",
		Status:   "",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), res.Total)
	require.Len(t, res.Vehicles, 2)
	assert.Equal(t, "veh-1", res.Vehicles[0].ID)
	assert.Equal(t, "REG1", res.Vehicles[0].RegistrationNumber)
	require.NotNil(t, res.Vehicles[0].CurrentMileage)
	assert.InDelta(t, 100.0, *res.Vehicles[0].CurrentMileage, 0.001)
	assert.Equal(t, "veh-2", res.Vehicles[1].ID)
	assert.Nil(t, res.Vehicles[1].CurrentMileage)
	assert.Equal(t, "maintenance", res.Vehicles[1].Status)
}

func TestListVehiclesUseCase_RepoError(t *testing.T) {
	repo := &mockVehicleRepo{searchErr: errors.New("search failed")}
	uow := &mockUoW{provider: repo}
	uc := NewListVehiclesUseCase(uow)

	_, err := uc.Execute(context.Background(), ListVehiclesQuery{TenantID: "t1", Page: 1, Limit: 10})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "search failed")
}

func TestListVehiclesUseCase_TypeAssertionFailure(t *testing.T) {
	uow := &mockUoW{provider: "bad"}
	uc := NewListVehiclesUseCase(uow)
	_, err := uc.Execute(context.Background(), ListVehiclesQuery{TenantID: "t1", Page: 1, Limit: 10})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to retrieve vehicle repository")
}

func TestListVehiclesUseCase_UoWError(t *testing.T) {
	uow := &mockUoW{execErr: errors.New("uow error")}
	uc := NewListVehiclesUseCase(uow)
	_, err := uc.Execute(context.Background(), ListVehiclesQuery{TenantID: "t1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uow error")
}

func TestListVehiclesUseCase_EmptyResult(t *testing.T) {
	repo := &mockVehicleRepo{
		searchResult: []domain.VehicleReadModel{},
		searchTotal:  0,
	}
	uow := &mockUoW{provider: repo}
	uc := NewListVehiclesUseCase(uow)

	res, err := uc.Execute(context.Background(), ListVehiclesQuery{TenantID: "t1", Page: 1, Limit: 10, Search: "nonexistent"})
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.Total)
	assert.Len(t, res.Vehicles, 0)
}
