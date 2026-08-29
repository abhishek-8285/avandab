package sqlite

import (
	"context"

	"transport-app/internal/domain"
	"transport-app/internal/repository"

	db "transport-app/db/generated/sqlite"
)

// PaymentRepository implementation

func (r *SQLRepository) CreatePayment(ctx context.Context, payment domain.Payment) (domain.Payment, error) {
	created, err := r.Q(ctx).CreatePayment(ctx, db.CreatePaymentParams{
		ID:          string(payment.ID),
		InvoiceID:   string(payment.InvoiceID),
		PaymentDate: payment.PaymentDate,
		Amount:      payment.Amount,
		Method:      string(payment.Method),
		Reference:   nullString(payment.Reference),
		Remarks:     nullString(payment.Remarks),
		TenantID:    tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Payment{}, err
	}
	v := db.Payment{
		ID:          created.ID,
		InvoiceID:   created.InvoiceID,
		PaymentDate: created.PaymentDate,
		Amount:      created.Amount,
		Method:      created.Method,
		Reference:   created.Reference,
		Remarks:     created.Remarks,
		CreatedAt:   created.CreatedAt,
		UpdatedAt:   created.UpdatedAt,
		TenantID:    created.TenantID,
	}
	return toDomainPayment(v), nil
}

func (r *SQLRepository) GetPaymentByID(ctx context.Context, id domain.PaymentID) (repository.PaymentWithInvoice, error) {
	row, err := r.Q(ctx).GetPaymentByID(ctx, db.GetPaymentByIDParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return repository.PaymentWithInvoice{}, err
	}
	return paymentRowToWithInvoice(
		row.ID, row.InvoiceID, row.PaymentDate, row.Amount, row.Method,
		row.Reference, row.Remarks, row.CreatedAt, row.UpdatedAt,
		row.InvoiceNumber, row.InvoiceTotal, row.InvoicePaymentStatus,
	), nil
}

func (r *SQLRepository) DeletePayment(ctx context.Context, id domain.PaymentID) error {
	return r.Q(ctx).DeletePayment(ctx, db.DeletePaymentParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
}

func (r *SQLRepository) GetPaymentsByInvoice(ctx context.Context, invoiceID domain.InvoiceID) ([]domain.Payment, error) {
	rows, err := r.Q(ctx).GetPaymentsByInvoice(ctx, db.GetPaymentsByInvoiceParams{
		InvoiceID: string(invoiceID),
		TenantID:  tenantIDFromCtx(ctx),
	})
	if err != nil {
		return nil, err
	}
	result := make([]domain.Payment, len(rows))
	for i, row := range rows {
		result[i] = domain.Payment{
			ID:          domain.PaymentID(row.ID),
			InvoiceID:   domain.InvoiceID(row.InvoiceID),
			PaymentDate: row.PaymentDate,
			Amount:      row.Amount,
			Method:      domain.PaymentMethod(row.Method),
			Reference:   fromNullString(row.Reference),
			Remarks:     fromNullString(row.Remarks),
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
		}
	}
	return result, nil
}

func (r *SQLRepository) SumPaymentsByInvoice(ctx context.Context, invoiceID domain.InvoiceID) (float64, error) {
	return r.Q(ctx).SumPaymentsByInvoice(ctx, db.SumPaymentsByInvoiceParams{
		InvoiceID: string(invoiceID),
		TenantID:  tenantIDFromCtx(ctx),
	})
}

func (r *SQLRepository) SearchPayments(ctx context.Context, method string, limit, offset int) ([]repository.PaymentWithInvoice, error) {
	rows, err := r.Q(ctx).SearchPayments(ctx, db.SearchPaymentsParams{
		TenantID: tenantIDFromCtx(ctx),
		Column2:  method,
		Method:   method,
		Limit:    int64(limit),
		Offset:   int64(offset),
	})
	if err != nil {
		return nil, err
	}
	result := make([]repository.PaymentWithInvoice, len(rows))
	for i, row := range rows {
		result[i] = paymentRowToWithInvoice(
			row.ID, row.InvoiceID, row.PaymentDate, row.Amount, row.Method,
			row.Reference, row.Remarks, row.CreatedAt, row.UpdatedAt,
			row.InvoiceNumber, row.InvoiceTotal, row.InvoicePaymentStatus,
		)
	}
	return result, nil
}

func (r *SQLRepository) CountPayments(ctx context.Context, method string) (int64, error) {
	count, err := r.Q(ctx).CountPayments(ctx, db.CountPaymentsParams{
		TenantID: tenantIDFromCtx(ctx),
		Column2:  method,
		Method:   method,
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *SQLRepository) GetTotalRevenue(ctx context.Context) (float64, error) {
	rev, err := r.Q(ctx).GetTotalRevenue(ctx, tenantIDFromCtx(ctx))
	if err != nil {
		return 0, err
	}
	return rev, nil
}

func (r *SQLRepository) GetMonthlyRevenue(ctx context.Context) ([]repository.MonthlyRevenue, error) {
	rows, err := r.Q(ctx).GetMonthlyRevenue(ctx, tenantIDFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	result := make([]repository.MonthlyRevenue, len(rows))
	for i, row := range rows {
		result[i] = repository.MonthlyRevenue{
			Month: row.Month,
			Total: row.Total,
		}
	}
	return result, nil
}

func (r *SQLRepository) GetRevenueByDay(ctx context.Context) ([]repository.RevenueByDay, error) {
	rows, err := r.Q(ctx).GetRevenueByDay(ctx, tenantIDFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	result := make([]repository.RevenueByDay, len(rows))
	for i, row := range rows {
		result[i] = repository.RevenueByDay{
			Day:   row.Day,
			Total: row.Total,
		}
	}
	return result, nil
}

func (r *SQLRepository) GetPaymentsByCustomer(ctx context.Context, customerID domain.CustomerID, limit, offset int) ([]repository.PaymentWithInvoice, error) {
	rows, err := r.Q(ctx).GetPaymentsByCustomer(ctx, db.GetPaymentsByCustomerParams{
		ID:       string(customerID),
		TenantID: tenantIDFromCtx(ctx),
		Limit:    int64(limit),
		Offset:   int64(offset),
	})
	if err != nil {
		return nil, err
	}
	result := make([]repository.PaymentWithInvoice, len(rows))
	for i, row := range rows {
		result[i] = repository.PaymentWithInvoice{
			Payment: domain.Payment{
				ID:          domain.PaymentID(row.ID),
				InvoiceID:   domain.InvoiceID(row.InvoiceID),
				PaymentDate: row.PaymentDate,
				Amount:      row.Amount,
				Method:      domain.PaymentMethod(row.Method),
				Reference:   fromNullString(row.Reference),
				Remarks:     fromNullString(row.Remarks),
				CreatedAt:   row.CreatedAt,
				UpdatedAt:   row.UpdatedAt,
			},
			InvoiceNumber: row.InvoiceNumber,
			CustomerName:  &row.CustomerName,
		}
	}
	return result, nil
}

func (r *SQLRepository) CountPaymentsByCustomer(ctx context.Context, customerID domain.CustomerID) (int64, error) {
	return r.Q(ctx).CountPaymentsByCustomer(ctx, db.CountPaymentsByCustomerParams{
		ID:       string(customerID),
		TenantID: tenantIDFromCtx(ctx),
	})
}
