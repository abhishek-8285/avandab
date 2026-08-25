package application

import (
	"context"
	"errors"

	"transport-app/internal/driver/domain"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

type ListDriversQuery struct {
	TenantID shared.TenantID
	Page     int
	Limit    int
	Search   string
	Status   string
	DateFrom string // YYYY-MM-DD inclusive, empty = unbounded (created_at)
	DateTo   string // YYYY-MM-DD inclusive, empty = unbounded (created_at)
}

// dateRangeDriverRepo is implemented by driver repositories that support
// created_at window filtering. Asserted optionally so existing repository
// implementations/mocks keep compiling unchanged.
type dateRangeDriverRepo interface {
	SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]domain.DriverReadModel, int64, error)
}

func hasDateRange(from, to string) bool {
	return from != "" || to != ""
}

type ListDriversResponse struct {
	Drivers []DriverResponseDTO
	Total   int64
}

type ListDriversUseCase struct {
	uow ports.UnitOfWork
}

func NewListDriversUseCase(uow ports.UnitOfWork) *ListDriversUseCase {
	return &ListDriversUseCase{uow: uow}
}

func (uc *ListDriversUseCase) Execute(ctx context.Context, q ListDriversQuery) (ListDriversResponse, error) {
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	offset := (q.Page - 1) * q.Limit

	var res ListDriversResponse

	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Drivers().(domain.DriverRepository)
		if !ok {
			return errors.New("failed to retrieve driver repository")
		}

		var rows []domain.DriverReadModel
		var total int64
		var err error

		dateRepo, dateOK := repo.(dateRangeDriverRepo)
		useDates := hasDateRange(q.DateFrom, q.DateTo) && dateOK

		if useDates {
			rows, total, err = dateRepo.SearchReadModelsDateRange(txCtx, q.TenantID, q.Search, q.Status, q.DateFrom, q.DateTo, q.Limit, offset)
		} else {
			rows, total, err = repo.SearchReadModels(txCtx, q.TenantID, q.Search, q.Status, q.Limit, offset)
		}
		if err != nil {
			return err
		}

		dtos := make([]DriverResponseDTO, len(rows))
		for i, d := range rows {
			dtos[i] = DriverResponseDTO{
				ID:                    d.ID,
				DriverDisplayID:       d.DriverDisplayID,
				FirstName:             d.FirstName,
				LastName:              d.LastName,
				Phone:                 d.Phone,
				Email:                 d.Email,
				Address:               d.Address,
				LicenseNumber:         d.LicenseNumber,
				LicenseExpiry:         d.LicenseExpiry,
				ExperienceYears:       d.ExperienceYears,
				Status:                d.Status,
				EmergencyContactName:  d.EmergencyContactName,
				EmergencyContactPhone: d.EmergencyContactPhone,
				Notes:                 d.Notes,
				CreatedAt:             d.CreatedAt,
				UpdatedAt:             d.UpdatedAt,
			}
		}

		res = ListDriversResponse{
			Drivers: dtos,
			Total:   total,
		}
		return nil
	})

	return res, err
}
