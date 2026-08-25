package application

import (
	"context"
	"errors"

	"transport-app/internal/booking/domain"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

// ListBookingsQuery holds pagination, status, and search filters.
type ListBookingsQuery struct {
	TenantID shared.TenantID
	Page     int
	Limit    int
	Search   string
	Status   string
	DateFrom string // YYYY-MM-DD inclusive on pickup_date, empty = unbounded
	DateTo   string // YYYY-MM-DD inclusive on pickup_date, empty = unbounded
}

// dateRangeBookingRepo is implemented by booking repositories supporting
// pickup-date window filtering. Asserted optionally so existing repository
// implementations/mocks keep compiling unchanged.
type dateRangeBookingRepo interface {
	SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]domain.BookingReadModel, int64, error)
}

// ListBookingsResponse represents the paginated result.
type ListBookingsResponse struct {
	Bookings []BookingResponseDTO
	Total    int64
}

// ListBookingsUseCase orchestrates the paginated search query execution.
type ListBookingsUseCase struct {
	uow ports.UnitOfWork
}

// NewListBookingsUseCase creates a new ListBookingsUseCase.
func NewListBookingsUseCase(uow ports.UnitOfWork) *ListBookingsUseCase {
	return &ListBookingsUseCase{uow: uow}
}

// Execute performs retrieval of paginated booking DTOs.
func (uc *ListBookingsUseCase) Execute(ctx context.Context, q ListBookingsQuery) (ListBookingsResponse, error) {
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	offset := (q.Page - 1) * q.Limit

	var res ListBookingsResponse

	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Bookings().(domain.BookingRepository)
		if !ok {
			return errors.New("failed to retrieve booking repository")
		}

		readModels, total, err := func() ([]domain.BookingReadModel, int64, error) {
			if q.DateFrom != "" || q.DateTo != "" {
				if dateRepo, ok := repo.(dateRangeBookingRepo); ok {
					return dateRepo.SearchReadModelsDateRange(txCtx, q.TenantID, q.Search, q.Status, q.DateFrom, q.DateTo, q.Limit, offset)
				}
			}
			return repo.SearchReadModels(txCtx, q.TenantID, q.Search, q.Status, q.Limit, offset)
		}()
		if err != nil {
			return err
		}

		dtos := make([]BookingResponseDTO, len(readModels))
		for i, b := range readModels {
			dtos[i] = BookingResponseDTO{
				ID:               b.ID,
				BookingNumber:    b.BookingNumber,
				CustomerID:       b.CustomerID,
				CustomerName:     b.CustomerName,
				CustomerCompany:  b.CustomerCompany,
				RouteID:          b.RouteID,
				RouteSource:      b.RouteSource,
				RouteDestination: b.RouteDestination,
				PickupDate:       b.PickupDate,
				VehicleType:      b.VehicleType,
				Passengers:       b.Passengers,
				CargoWeight:      b.CargoWeight,
				Price:            b.Price,
				Notes:            b.Notes,
				Status:           b.Status,
				CreatedAt:        b.CreatedAt,
				UpdatedAt:        b.UpdatedAt,
			}
		}

		res = ListBookingsResponse{
			Bookings: dtos,
			Total:    total,
		}
		return nil
	})

	return res, err
}
