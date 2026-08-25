package application

import (
	"context"
	"errors"

	"transport-app/internal/invoice/domain"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

type ListInvoicesQuery struct {
	TenantID shared.TenantID
	Page     int
	Limit    int
	Search   string
	Status   string
	DateFrom string // YYYY-MM-DD inclusive, empty = unbounded
	DateTo   string // YYYY-MM-DD inclusive, empty = unbounded
}

// dateRangeInvoiceRepo is implemented by invoice repositories that support
// invoice-date window filtering. Asserted optionally so existing repository
// implementations/mocks keep compiling unchanged.
type dateRangeInvoiceRepo interface {
	SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]domain.InvoiceReadModel, int64, error)
}

func hasDateRange(from, to string) bool {
	return from != "" || to != ""
}

type ListInvoicesResponse struct {
	Invoices []InvoiceResponseDTO
	Total    int64
}

type ListInvoicesUseCase struct {
	uow ports.UnitOfWork
}

func NewListInvoicesUseCase(uow ports.UnitOfWork) *ListInvoicesUseCase {
	return &ListInvoicesUseCase{uow: uow}
}

func (uc *ListInvoicesUseCase) Execute(ctx context.Context, q ListInvoicesQuery) (ListInvoicesResponse, error) {
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	offset := (q.Page - 1) * q.Limit

	var res ListInvoicesResponse

	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Invoices().(domain.InvoiceRepository)
		if !ok {
			return errors.New("failed to retrieve invoice repository")
		}

		var rows []domain.InvoiceReadModel
		var total int64
		var err error

		dateRepo, dateOK := repo.(dateRangeInvoiceRepo)
		if hasDateRange(q.DateFrom, q.DateTo) && dateOK {
			rows, total, err = dateRepo.SearchReadModelsDateRange(txCtx, q.TenantID, q.Search, q.Status, q.DateFrom, q.DateTo, q.Limit, offset)
		} else {
			rows, total, err = repo.SearchReadModels(txCtx, q.TenantID, q.Search, q.Status, q.Limit, offset)
		}
		if err != nil {
			return err
		}

		dtos := make([]InvoiceResponseDTO, len(rows))
		for i, inv := range rows {
			dtos[i] = InvoiceResponseDTO{
				ID:              inv.ID,
				InvoiceNumber:   inv.InvoiceNumber,
				BookingID:       inv.BookingID,
				BookingNumber:   inv.BookingNumber,
				CustomerID:      inv.CustomerID,
				CustomerName:    inv.CustomerName,
				CustomerCompany: inv.CustomerCompany,
				TripID:          inv.TripID,
				TripNumber:      inv.TripNumber,
				Subtotal:        inv.Subtotal,
				Tax:             inv.Tax,
				Discount:        inv.Discount,
				Total:           inv.Total,
				PaymentStatus:   inv.PaymentStatus,
				CreatedAt:       inv.CreatedAt,
				UpdatedAt:       inv.UpdatedAt,
			}
		}

		res = ListInvoicesResponse{
			Invoices: dtos,
			Total:    total,
		}
		return nil
	})

	return res, err
}
