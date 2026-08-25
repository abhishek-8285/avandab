package application

import (
	"context"
	"errors"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/vehicle/domain"
)

type ListVehiclesQuery struct {
	TenantID shared.TenantID
	Page     int
	Limit    int
	Search   string
	Status   string
	DateFrom string // YYYY-MM-DD inclusive, empty = unbounded (created_at)
	DateTo   string // YYYY-MM-DD inclusive, empty = unbounded (created_at)
}

// dateRangeVehicleRepo is implemented by vehicle repositories that support
// created_at window filtering. Asserted optionally so existing repository
// implementations/mocks keep compiling unchanged.
type dateRangeVehicleRepo interface {
	SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]domain.VehicleReadModel, int64, error)
}

func hasDateRange(from, to string) bool {
	return from != "" || to != ""
}

type ListVehiclesResponse struct {
	Vehicles []VehicleResponseDTO
	Total    int64
}

type ListVehiclesUseCase struct {
	uow ports.UnitOfWork
}

func NewListVehiclesUseCase(uow ports.UnitOfWork) *ListVehiclesUseCase {
	return &ListVehiclesUseCase{uow: uow}
}

func (uc *ListVehiclesUseCase) Execute(ctx context.Context, q ListVehiclesQuery) (ListVehiclesResponse, error) {
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	offset := (q.Page - 1) * q.Limit

	var res ListVehiclesResponse

	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Vehicles().(domain.VehicleRepository)
		if !ok {
			return errors.New("failed to retrieve vehicle repository")
		}

		var rows []domain.VehicleReadModel
		var total int64
		var err error

		dateRepo, dateOK := repo.(dateRangeVehicleRepo)
		useDates := hasDateRange(q.DateFrom, q.DateTo) && dateOK

		if useDates {
			rows, total, err = dateRepo.SearchReadModelsDateRange(txCtx, q.TenantID, q.Search, q.Status, q.DateFrom, q.DateTo, q.Limit, offset)
		} else {
			rows, total, err = repo.SearchReadModels(txCtx, q.TenantID, q.Search, q.Status, q.Limit, offset)
		}
		if err != nil {
			return err
		}

		dtos := make([]VehicleResponseDTO, len(rows))
		for i, v := range rows {
			dtos[i] = VehicleResponseDTO{
				ID:                 v.ID,
				RegistrationNumber: v.RegistrationNumber,
				VehicleNumber:      v.VehicleNumber,
				VehicleType:        v.VehicleType,
				Capacity:           v.Capacity,
				FuelType:           v.FuelType,
				InsuranceExpiry:    v.InsuranceExpiry,
				FitnessExpiry:      v.FitnessExpiry,
				PermitExpiry:       v.PermitExpiry,
				Status:             v.Status,
				CurrentMileage:     v.CurrentMileage,
				CreatedAt:          v.CreatedAt,
				UpdatedAt:          v.UpdatedAt,
			}
		}

		res = ListVehiclesResponse{
			Vehicles: dtos,
			Total:    total,
		}
		return nil
	})

	return res, err
}
