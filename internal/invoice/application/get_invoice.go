package application

import (
	"context"
	"errors"
	"time"

	"transport-app/internal/invoice/domain"
	"transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

// InvoiceResponseDTO represents read model fields.
type InvoiceResponseDTO struct {
	ID              string  `json:"id"`
	InvoiceNumber   string  `json:"invoice_number"`
	BookingID       string  `json:"booking_id"`
	BookingNumber   string  `json:"booking_number"`
	CustomerID      string  `json:"customer_id"`
	CustomerName    string  `json:"customer_name"`
	CustomerCompany string  `json:"customer_company"`
	TripID          *string `json:"trip_id"`
	TripNumber      string  `json:"trip_number"`
	Subtotal        float64 `json:"subtotal"`
	Tax             float64 `json:"tax"`
	Discount        float64 `json:"discount"`
	Total           float64 `json:"total"`
	PaymentStatus   string  `json:"payment_status"`
	CGST            float64 `json:"cgst"`
	SGST            float64 `json:"sgst"`
	IGST            float64 `json:"igst"`
	IRN             string  `json:"irn"`
	IRNAckNo        string  `json:"irn_ack_no"`
	IRNAckDate      string  `json:"irn_ack_date"`
	// IRNCancelledAt is empty when the IRN is active (NULL in DB,
	// migration 00099).
	IRNCancelledAt string    `json:"irn_cancelled_at"`
	SignedQR       string    `json:"signed_qr"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// GetInvoiceQuery query arguments.
type GetInvoiceQuery struct {
	ID       aggregate.InvoiceID
	TenantID shared.TenantID
}

// GetInvoiceUseCase gets details of an invoice.
type GetInvoiceUseCase struct {
	uow ports.UnitOfWork
}

// NewGetInvoiceUseCase constructs a new GetInvoiceUseCase.
func NewGetInvoiceUseCase(uow ports.UnitOfWork) *GetInvoiceUseCase {
	return &GetInvoiceUseCase{uow: uow}
}

// Execute retrieves and maps to the read model DTO.
func (uc *GetInvoiceUseCase) Execute(ctx context.Context, q GetInvoiceQuery) (InvoiceResponseDTO, error) {
	var dto InvoiceResponseDTO
	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Invoices().(domain.InvoiceRepository)
		if !ok {
			return errors.New("failed to retrieve invoice repository")
		}

		inv, err := repo.GetReadModel(txCtx, q.ID, q.TenantID)
		if err != nil {
			return err
		}

		dto = InvoiceResponseDTO{
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
			CGST:            inv.CGST,
			SGST:            inv.SGST,
			IGST:            inv.IGST,
			IRN:             inv.IRN,
			IRNAckNo:        inv.IRNAckNo,
			IRNAckDate:      inv.IRNAckDate,
			IRNCancelledAt:  inv.IRNCancelledAt,
			SignedQR:        inv.SignedQR,
			CreatedAt:       inv.CreatedAt,
			UpdatedAt:       inv.UpdatedAt,
		}
		return nil
	})
	return dto, err
}
