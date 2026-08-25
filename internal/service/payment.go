package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"transport-app/internal/domain"
	paymentevents "transport-app/internal/domain/payment"
	"transport-app/internal/events"
	"transport-app/internal/repository"
)

// PaymentService handles payment processing and balance tracking.
type PaymentService struct {
	baseService
}

// RecordPayment records a payment against an invoice.
func (s *PaymentService) RecordPayment(ctx context.Context, invoiceID domain.InvoiceID, amount float64, method domain.PaymentMethod, reference, remarks string, paymentDate string) (domain.Payment, error) {
	if amount <= 0 {
		return domain.Payment{}, fmt.Errorf("payment amount must be greater than zero")
	}

	payDate, err := parseDateTime(paymentDate)
	if err != nil {
		payDate = time.Now()
	}
	_ = err

	payment := domain.Payment{
		ID:          domain.PaymentID(generateID()),
		InvoiceID:   invoiceID,
		PaymentDate: payDate,
		Amount:      amount,
		Method:      method,
		Reference:   strPtr(reference),
		Remarks:     strPtr(remarks),
	}

	// Validate invoice and outstanding balance atomically with the insert so
	// concurrent payments cannot both pass the duplicate/balance check.
	var created domain.Payment
	insertPayment := func(ctx context.Context) error {
		invoice, err := s.store.GetInvoiceByID(ctx, invoiceID)
		if err != nil {
			return domain.ErrInvoiceNotFound
		}

		paid, err := s.store.SumPaymentsByInvoice(ctx, invoiceID)
		if err != nil {
			return err
		}

		totalPaisa := int64(math.Round(invoice.Total * 100))
		paidPaisa := int64(math.Round(paid * 100))
		amountPaisa := int64(math.Round(amount * 100))
		if paidPaisa+amountPaisa > totalPaisa+1 {
			return fmt.Errorf("payment exceeds invoice outstanding balance")
		}

		created, err = s.store.CreatePayment(ctx, payment)
		return err
	}

	if s.txManager != nil {
		err = s.txManager.WithTransaction(ctx, insertPayment)
	} else {
		err = insertPayment(ctx)
	}
	if err != nil {
		return domain.Payment{}, err
	}

	// Update invoice payment status based on total payments
	if err := s.updateInvoicePaymentStatus(ctx, invoiceID); err != nil {
		s.log.Warn("failed to update invoice payment status", "invoice_id", invoiceID, "error", err)
	}

	s.log.Info("payment recorded", "payment_id", created.ID, "invoice_id", invoiceID, "amount", amount)
	s.logAudit(ctx, nil, "create", "payments", string(created.ID), nil, nil)
	s.events.Publish(ctx, events.Event{
		Type: events.PaymentRecorded,
		Payload: paymentevents.PaymentRecorded{
			PaymentID:  created.ID,
			InvoiceID:  invoiceID,
			Amount:     amount,
			Method:     created.Method,
			OccurredAt: time.Now(),
		},
	})

	// Internal money ledger (migration 00097): a received payment is a
	// credit. The ledger is audit infrastructure — a failure here must
	// never fail the payment itself (same contract as logAudit above).
	if err := NewMoneyLedgerService(s.store, s.log).AppendEntry(ctx, LedgerEntry{
		TxnType:     "payment_recorded",
		RefTable:    "payments",
		RefID:       string(created.ID),
		Direction:   "credit",
		AmountMinor: ToMinor(amount),
		Memo:        fmt.Sprintf("payment against invoice %s", invoiceID),
	}); err != nil {
		s.log.Warn("money ledger append failed; payment stands",
			"payment_id", created.ID, "error", err)
	}
	return created, nil
}

// updateInvoicePaymentStatus recalculates payment status for an invoice.
func (s *PaymentService) updateInvoicePaymentStatus(ctx context.Context, invoiceID domain.InvoiceID) error {
	invoice, err := s.store.GetInvoiceByID(ctx, invoiceID)
	if err != nil {
		return err
	}

	paid, err := s.store.SumPaymentsByInvoice(ctx, invoiceID)
	if err != nil {
		return err
	}

	var status domain.PaymentStatus
	if paid >= invoice.Total {
		status = domain.PaymentStatusPaid
	} else if paid > 0 {
		status = domain.PaymentStatusPartiallyPaid
	} else {
		status = domain.PaymentStatusPending
	}

	_, err = s.store.UpdateInvoicePaymentStatus(ctx, invoiceID, status)
	return err
}

// GetPayment retrieves a payment by ID.
func (s *PaymentService) GetPayment(ctx context.Context, id domain.PaymentID) (repository.PaymentWithInvoice, error) {
	return s.store.GetPaymentByID(ctx, id)
}

// ListPayments retrieves payments with search and pagination.
func (s *PaymentService) ListPayments(ctx context.Context, method string, limit, offset int) ([]repository.PaymentWithInvoice, int64, error) {
	payments, err := s.store.SearchPayments(ctx, method, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.store.CountPayments(ctx, method)
	if err != nil {
		return nil, 0, err
	}
	return payments, total, nil
}

// DeletePayment deletes a payment and updates the invoice status.
func (s *PaymentService) DeletePayment(ctx context.Context, id domain.PaymentID) error {
	// Get payment to find invoice ID
	payment, err := s.store.GetPaymentByID(ctx, id)
	if err != nil {
		return domain.ErrPaymentNotFound
	}

	if err := s.store.DeletePayment(ctx, id); err != nil {
		return err
	}

	// Recalculate invoice payment status
	invoiceID := domain.InvoiceID(payment.InvoiceID)
	_ = s.updateInvoicePaymentStatus(ctx, invoiceID)

	s.log.Info("payment deleted", "payment_id", id)
	return nil
}

// GetTotalRevenue returns total revenue from all payments.
func (s *PaymentService) GetTotalRevenue(ctx context.Context) (float64, error) {
	return s.store.GetTotalRevenue(ctx)
}

// GetMonthlyRevenue returns monthly revenue data.
func (s *PaymentService) GetMonthlyRevenue(ctx context.Context) ([]repository.MonthlyRevenue, error) {
	return s.store.GetMonthlyRevenue(ctx)
}

// GetCustomerPayments returns payments for a specific customer.
func (s *PaymentService) GetCustomerPayments(ctx context.Context, customerID domain.CustomerID, limit, offset int) ([]repository.PaymentWithInvoice, int64, error) {
	payments, err := s.store.GetPaymentsByCustomer(ctx, customerID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.store.CountPaymentsByCustomer(ctx, customerID)
	if err != nil {
		return nil, 0, err
	}
	return payments, total, nil
}
