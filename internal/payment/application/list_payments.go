package application

import (
	"context"
	"errors"

	"transport-app/internal/payment/domain"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

type ListPaymentsQuery struct {
	TenantID shared.TenantID
	Page     int
	Limit    int
	Method   string
	DateFrom string // YYYY-MM-DD inclusive, empty = unbounded
	DateTo   string // YYYY-MM-DD inclusive, empty = unbounded
}

// dateRangePaymentRepo is implemented by payment repositories that support
// payment-date window filtering. Asserted optionally so existing repository
// implementations/mocks keep compiling unchanged.
type dateRangePaymentRepo interface {
	SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, method string, from string, to string, limit int, offset int) ([]domain.PaymentReadModel, int64, error)
}

func hasDateRange(from, to string) bool {
	return from != "" || to != ""
}

type ListPaymentsResponse struct {
	Payments []PaymentResponseDTO
	Total    int64
}

type ListPaymentsUseCase struct {
	uow ports.UnitOfWork
}

func NewListPaymentsUseCase(uow ports.UnitOfWork) *ListPaymentsUseCase {
	return &ListPaymentsUseCase{uow: uow}
}

func (uc *ListPaymentsUseCase) Execute(ctx context.Context, q ListPaymentsQuery) (ListPaymentsResponse, error) {
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	offset := (q.Page - 1) * q.Limit

	var res ListPaymentsResponse

	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Payments().(domain.PaymentRepository)
		if !ok {
			return errors.New("failed to retrieve payment repository")
		}

		var rows []domain.PaymentReadModel
		var total int64
		var err error

		dateRepo, dateOK := repo.(dateRangePaymentRepo)
		if hasDateRange(q.DateFrom, q.DateTo) && dateOK {
			rows, total, err = dateRepo.SearchReadModelsDateRange(txCtx, q.TenantID, q.Method, q.DateFrom, q.DateTo, q.Limit, offset)
		} else {
			rows, total, err = repo.SearchReadModels(txCtx, q.TenantID, q.Method, q.Limit, offset)
		}
		if err != nil {
			return err
		}

		dtos := make([]PaymentResponseDTO, len(rows))
		for i, p := range rows {
			dtos[i] = PaymentResponseDTO{
				ID:            p.ID,
				InvoiceID:     p.InvoiceID,
				InvoiceNumber: p.InvoiceNumber,
				PaymentDate:   p.PaymentDate,
				Amount:        p.Amount,
				Method:        p.Method,
				Reference:     p.Reference,
				Remarks:       p.Remarks,
				CreatedAt:     p.CreatedAt,
				UpdatedAt:     p.UpdatedAt,
			}
		}

		res = ListPaymentsResponse{
			Payments: dtos,
			Total:    total,
		}
		return nil
	})

	return res, err
}

type ListPaymentsByInvoiceQuery struct {
	TenantID  shared.TenantID
	InvoiceID string
}

type ListPaymentsByInvoiceUseCase struct {
	uow ports.UnitOfWork
}

func NewListPaymentsByInvoiceUseCase(uow ports.UnitOfWork) *ListPaymentsByInvoiceUseCase {
	return &ListPaymentsByInvoiceUseCase{uow: uow}
}

func (uc *ListPaymentsByInvoiceUseCase) Execute(ctx context.Context, q ListPaymentsByInvoiceQuery) ([]PaymentResponseDTO, error) {
	if q.InvoiceID == "" {
		return nil, errors.New("invoice ID is required")
	}

	var dtos []PaymentResponseDTO

	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Payments().(domain.PaymentRepository)
		if !ok {
			return errors.New("failed to retrieve payment repository")
		}

		rows, err := repo.GetPaymentsByInvoice(txCtx, q.InvoiceID, q.TenantID)
		if err != nil {
			return err
		}

		dtos = make([]PaymentResponseDTO, len(rows))
		for i, p := range rows {
			dtos[i] = PaymentResponseDTO{
				ID:            p.ID,
				InvoiceID:     p.InvoiceID,
				InvoiceNumber: p.InvoiceNumber,
				PaymentDate:   p.PaymentDate,
				Amount:        p.Amount,
				Method:        p.Method,
				Reference:     p.Reference,
				Remarks:       p.Remarks,
				CreatedAt:     p.CreatedAt,
				UpdatedAt:     p.UpdatedAt,
			}
		}

		return nil
	})

	return dtos, err
}
