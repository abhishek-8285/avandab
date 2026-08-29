package service

import (
	"context"
	"time"

	"transport-app/internal/domain"
	invoiceevents "transport-app/internal/domain/invoice"
	"transport-app/internal/events"
	"transport-app/internal/repository"
)

// InvoiceService handles invoice generation and management.
type InvoiceService struct {
	baseService
}

// GenerateInvoiceFromTrip creates an invoice for a completed trip.
func (s *InvoiceService) GenerateInvoiceFromTrip(ctx context.Context, tripID domain.TripID) (domain.Invoice, error) {
	trip, err := s.store.GetTripByID(ctx, tripID)
	if err != nil {
		return domain.Invoice{}, domain.ErrTripNotFound
	}

	// Check if invoice already exists for this trip
	if _, err := s.store.GetInvoiceByTripID(ctx, tripID); err == nil {
		return domain.Invoice{}, domain.ErrDuplicateInvoice
	}

	// Get the booking to find customer
	var bookingID domain.BookingID
	if trip.BookingID != nil {
		bookingID = *trip.BookingID
	}

	booking, err := s.store.GetBookingByID(ctx, bookingID)
	if err != nil {
		return domain.Invoice{}, domain.ErrBookingNotFound
	}

	// Calculate invoice amounts
	subtotal := booking.Price
	var tax float64
	var discount float64

	// company_settings remains the GLOBAL default writer; billing.* tenant
	// rows overlay it per tenant (Spec 24 §Business logic overlay).
	settings, _ := s.store.GetCompanySettings(ctx)
	gstEnabled := s.overlayBool(ctx, ConfigKeyGSTEnabled, settings.GSTEnabled)
	gstRate := s.overlayFloat(ctx, ConfigKeyGSTRate, settings.GSTRate)
	if gstEnabled {
		tax = subtotal * (gstRate / 100.0)
	}

	total := subtotal + tax - discount

	invoice := domain.Invoice{
		ID:            domain.InvoiceID(generateID()),
		InvoiceNumber: s.generateInvoiceNumber(ctx),
		BookingID:     bookingID,
		CustomerID:    booking.CustomerID,
		TripID:        &tripID,
		Subtotal:      subtotal,
		Tax:           tax,
		Discount:      discount,
		Total:         total,
		PaymentStatus: domain.PaymentStatusPending,
	}

	created, err := s.store.CreateInvoice(ctx, invoice)
	if err != nil {
		return domain.Invoice{}, err
	}

	s.log.Info("invoice generated", "invoice_id", created.ID, "invoice_number", created.InvoiceNumber)
	s.logAudit(ctx, nil, "create", "invoices", string(created.ID), nil, nil)
	s.events.Publish(ctx, events.Event{
		Type: events.InvoiceGenerated,
		Payload: invoiceevents.InvoiceGenerated{
			InvoiceID:     created.ID,
			InvoiceNumber: created.InvoiceNumber,
			TripID:        tripID,
			Total:         created.Total,
			OccurredAt:    time.Now(),
		},
	})
	return created, nil
}

// GetInvoice retrieves an invoice by ID.
func (s *InvoiceService) GetInvoice(ctx context.Context, id domain.InvoiceID) (repository.InvoiceWithJoins, error) {
	return s.store.GetInvoiceByID(ctx, id)
}

// GetInvoiceByNumber retrieves an invoice by its number.
func (s *InvoiceService) GetInvoiceByNumber(ctx context.Context, number string) (repository.InvoiceWithJoins, error) {
	return s.store.GetInvoiceByNumber(ctx, number)
}

// GetInvoiceByBooking retrieves the invoice generated for a booking.
func (s *InvoiceService) GetInvoiceByBooking(ctx context.Context, bookingID domain.BookingID) (repository.InvoiceWithJoins, error) {
	return s.store.GetInvoiceByBookingID(ctx, bookingID)
}

// ListInvoicesByCustomer returns the most recent invoices for a customer.
func (s *InvoiceService) ListInvoicesByCustomer(ctx context.Context, customerID domain.CustomerID, limit int) ([]repository.InvoiceWithJoins, error) {
	if limit <= 0 {
		limit = 5
	}
	return s.store.ListInvoicesByCustomer(ctx, customerID, limit)
}

// ListInvoices retrieves invoices with search and pagination.
func (s *InvoiceService) ListInvoices(ctx context.Context, query, status string, limit, offset int) ([]repository.InvoiceWithJoins, int64, error) {
	invoices, err := s.store.SearchInvoices(ctx, query, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.store.CountInvoices(ctx, query, status)
	if err != nil {
		return nil, 0, err
	}
	return invoices, total, nil
}

// GetPendingInvoices returns all invoices with pending or partially paid status.
func (s *InvoiceService) GetPendingInvoices(ctx context.Context) ([]repository.InvoiceWithJoins, error) {
	return s.store.GetPendingInvoices(ctx)
}

// UpdateInvoice updates an existing invoice.
func (s *InvoiceService) UpdateInvoice(ctx context.Context, id domain.InvoiceID, bookingID domain.BookingID, customerID domain.CustomerID, tripID *domain.TripID, subtotal, tax, discount, total float64, paymentStatus domain.PaymentStatus) (domain.Invoice, error) {
	invoice, err := s.store.GetInvoiceByID(ctx, id)
	if err != nil {
		return domain.Invoice{}, domain.ErrInvoiceNotFound
	}

	// GST immutability: once e-invoiced, core financial fields are locked;
	// corrections go through credit/debit notes. Payment status may still
	// advance (payments legitimately settle the invoice).
	if s.invoiceIRN(ctx, id) != "" {
		if invoice.Subtotal != subtotal || invoice.Tax != tax ||
			invoice.Discount != discount || invoice.Total != total {
			return domain.Invoice{}, domain.ErrInvoiceEInvoiced
		}
	}

	invoice.BookingID = bookingID
	invoice.CustomerID = customerID
	invoice.TripID = tripID
	invoice.Subtotal = subtotal
	invoice.Tax = tax
	invoice.Discount = discount
	invoice.Total = total
	invoice.PaymentStatus = paymentStatus

	return s.store.UpdateInvoice(ctx, invoice.Invoice)
}

// DeleteInvoice deletes an invoice.
func (s *InvoiceService) DeleteInvoice(ctx context.Context, id domain.InvoiceID) error {
	// GST immutability guard: e-invoiced or paid invoices must not be
	// hard-deleted (payments cascade with the invoice row).
	if err := s.ensureInvoiceNotLocked(ctx, id); err != nil {
		return err
	}
	if err := s.store.DeleteInvoice(ctx, id); err != nil {
		return err
	}
	s.log.Info("invoice deleted", "invoice_id", id)
	return nil
}

// GetPaymentsForInvoice returns all payments for an invoice.
func (s *InvoiceService) GetPaymentsForInvoice(ctx context.Context, invoiceID domain.InvoiceID) ([]domain.Payment, error) {
	return s.store.GetPaymentsByInvoice(ctx, invoiceID)
}

// GetBalance returns the outstanding balance for an invoice.
func (s *InvoiceService) GetBalance(ctx context.Context, invoiceID domain.InvoiceID) (float64, error) {
	invoice, err := s.store.GetInvoiceByID(ctx, invoiceID)
	if err != nil {
		return 0, domain.ErrInvoiceNotFound
	}

	paid, err := s.store.SumPaymentsByInvoice(ctx, invoiceID)
	if err != nil {
		return 0, err
	}

	balance := invoice.Total - paid
	if balance < 0 {
		balance = 0
	}
	return balance, nil
}
