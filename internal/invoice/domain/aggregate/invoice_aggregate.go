package aggregate

import (
	"errors"
	"math"
	"time"

	"transport-app/internal/shared"
)

type InvoiceID string
type PaymentStatus string
type InvoiceStatus string

// invoiceCurrency is the single currency for minor-unit math below (Indian
// logistics billing; shared.Money needs a currency to Add/compare).
const invoiceCurrency = "INR"

const (
	PaymentStatusPending       PaymentStatus = "pending"
	PaymentStatusPaid          PaymentStatus = "paid"
	PaymentStatusPartiallyPaid PaymentStatus = "partially_paid"
)

const (
	InvoiceStatusDraft       InvoiceStatus = "draft"
	InvoiceStatusIssued      InvoiceStatus = "issued"
	InvoiceStatusOutstanding InvoiceStatus = "outstanding"
	InvoiceStatusPaid        InvoiceStatus = "paid"
	InvoiceStatusCancelled   InvoiceStatus = "cancelled"
)

// LineType constants for invoice line items (Spec 02 DDL CHECK constraint).
const (
	LineTypeFreight     = "freight"
	LineTypeDetention   = "detention"
	LineTypeAccessorial = "accessorial"
)

// LineItem is a single invoice line (freight, detention or accessorial).
type LineItem struct {
	ID           string
	TenantID     shared.TenantID
	InvoiceID    InvoiceID
	TripID       *string
	LineType     string
	HSNSACCode   *string
	Description  string
	Unit         *string
	Quantity     float64
	UnitPrice    float64
	Rate         float64
	TaxableValue float64
	CgstRate     float64
	SgstRate     float64
	IgstRate     float64
	CgstAmount   float64
	SgstAmount   float64
	IgstAmount   float64
	Amount       float64
	Total        float64
	RefID        *string // e.g. trip_detentions.id for detention lines
}

// InvoiceAggregate is the aggregate root representing a billing invoice.
type InvoiceAggregate struct {
	ID            InvoiceID
	TenantID      shared.TenantID
	InvoiceNumber string
	BookingID     string
	CustomerID    string
	TripID        *string
	Subtotal      float64
	Tax           float64
	Cgst          float64
	Sgst          float64
	Igst          float64
	Discount      float64
	Total         float64
	PaymentStatus PaymentStatus
	PaidAmount    float64
	Status        InvoiceStatus
	DueDate       *time.Time
	FinancialYear string
	CreditBalance float64
	Remarks       string
	IRN           *string
	IRNAckNo      *string
	IRNAckDate    *string
	SignedQR      *string
	EwbNumber     *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Version       int64
	LineItems     []LineItem
	events        []any
}

// NewInvoiceAggregate constructs a new InvoiceAggregate and registers a created event.
func NewInvoiceAggregate(
	id InvoiceID,
	tenantID shared.TenantID,
	invoiceNumber string,
	bookingID string,
	customerID string,
	tripID *string,
	subtotal float64,
	tax float64,
	discount float64,
	total float64,
	status PaymentStatus,
	now time.Time,
) *InvoiceAggregate {
	inv := &InvoiceAggregate{
		ID:            id,
		TenantID:      tenantID,
		InvoiceNumber: invoiceNumber,
		BookingID:     bookingID,
		CustomerID:    customerID,
		TripID:        tripID,
		Subtotal:      subtotal,
		Tax:           tax,
		Discount:      discount,
		Total:         total,
		PaymentStatus: status,
		Status:        InvoiceStatusOutstanding,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	inv.events = append(inv.events, InvoiceGeneratedEvent{
		ID:            id,
		TenantID:      tenantID,
		InvoiceNumber: invoiceNumber,
		Total:         total,
		CreatedAt:     now,
	})

	return inv
}

// RehydrateInvoiceAggregate reconstructs an invoice from persistence without emitting events.
func RehydrateInvoiceAggregate(
	id InvoiceID, tenantID shared.TenantID, invoiceNumber string,
	bookingID, customerID string, tripID *string,
	subtotal, tax, discount, total float64,
	paymentStatus PaymentStatus, invoiceStatus InvoiceStatus,
	paidAmount, _ float64,
	dueDate *time.Time, financialYear, remarks string,
	createdAt, updatedAt time.Time, version int64,
) *InvoiceAggregate {
	// No credit_balance column exists: credit is derived from gross paid
	// (PaidAmount is never capped) in minor units, so save/reload round-trips.
	var creditBalance float64
	if over := shared.FloatToMoney(paidAmount, invoiceCurrency).Amount - shared.FloatToMoney(total, invoiceCurrency).Amount; over > 0 {
		creditBalance = shared.Money{Amount: over, Currency: invoiceCurrency}.MoneyToFloat()
	}
	return &InvoiceAggregate{
		ID:            id,
		TenantID:      tenantID,
		InvoiceNumber: invoiceNumber,
		BookingID:     bookingID,
		CustomerID:    customerID,
		TripID:        tripID,
		Subtotal:      subtotal,
		Tax:           tax,
		Discount:      discount,
		Total:         total,
		PaymentStatus: paymentStatus,
		PaidAmount:    paidAmount,
		Status:        invoiceStatus,
		DueDate:       dueDate,
		FinancialYear: financialYear,
		CreditBalance: creditBalance,
		Remarks:       remarks,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		Version:       version,
	}
}

// Events returns recorded domain events.
func (a *InvoiceAggregate) Events() []any {
	return a.events
}

// ClearEvents clears the recorded events list.
func (a *InvoiceAggregate) ClearEvents() {
	a.events = nil
}

// AddLineItem appends a line item and recomputes Subtotal (sum of all line
// amounts) and Total (Subtotal + Tax - Discount). Spec 02 §6.
func (a *InvoiceAggregate) AddLineItem(item LineItem) {
	item.Amount = RoundMoney(item.Amount)
	a.LineItems = append(a.LineItems, item)
	a.RecomputeTotals()
}

// RecomputeTotals re-derives Subtotal, CGST, SGST, IGST, Tax, and Total from the line items.
// It is a no-op when the invoice has no line items (flat booking pricing remains).
func (a *InvoiceAggregate) RecomputeTotals() {
	if len(a.LineItems) == 0 {
		return
	}
	var subtotal, cgst, sgst, igst float64
	hasLineTaxes := false
	for _, li := range a.LineItems {
		subtotal += li.Amount
		if li.CgstAmount > 0 || li.SgstAmount > 0 || li.IgstAmount > 0 {
			hasLineTaxes = true
			cgst += li.CgstAmount
			sgst += li.SgstAmount
			igst += li.IgstAmount
		}
	}
	a.Subtotal = RoundMoney(subtotal)
	if hasLineTaxes {
		a.Cgst = RoundMoney(cgst)
		a.Sgst = RoundMoney(sgst)
		a.Igst = RoundMoney(igst)
		a.Tax = RoundMoney(a.Cgst + a.Sgst + a.Igst)
	}
	a.Total = RoundMoney(a.Subtotal + a.Tax - a.Discount)
}

// RoundMoney rounds to 2 decimal places (minor-unit precision).
func RoundMoney(v float64) float64 {
	return math.Round(v*100) / 100
}

// UpdatePaymentStatus updates the status of the invoice and records a payment received event.
func (a *InvoiceAggregate) UpdatePaymentStatus(status PaymentStatus, now time.Time) error {
	a.PaymentStatus = status
	a.UpdatedAt = now

	a.events = append(a.events, InvoicePaymentUpdatedEvent{
		ID:            a.ID,
		TenantID:      a.TenantID,
		PaymentStatus: status,
		UpdatedAt:     now,
	})

	return nil
}

// OutstandingBalance returns max(Total - PaidAmount, 0) in minor units, so
// float dust never shows a phantom paise due and overpay reads 0 (Spec 05 §2).
func (a *InvoiceAggregate) OutstandingBalance() float64 {
	outstanding := shared.FloatToMoney(a.Total, invoiceCurrency).Amount -
		shared.FloatToMoney(a.PaidAmount, invoiceCurrency).Amount
	if outstanding <= 0 {
		return 0
	}
	return shared.Money{Amount: outstanding, Currency: invoiceCurrency}.MoneyToFloat()
}

// ApplyPayment records a payment against this invoice, updates PaidAmount and PaymentStatus.
// PaidAmount is gross (never capped) so CreditBalance = max(Paid - Total, 0)
// derives from the persisted column — no new column needed. All compares run
// in minor units via shared.Money, so 0.1+0.2 hits exact zero.
// If payment exceeds outstanding, the excess is recorded in CreditBalance.
// Returns error if invoice is cancelled.
func (a *InvoiceAggregate) ApplyPayment(amount float64, now time.Time) error {
	if a.Status == InvoiceStatusCancelled {
		return errors.New("cannot apply payment to cancelled invoice")
	}

	newPaid, err := shared.FloatToMoney(a.PaidAmount, invoiceCurrency).
		Add(shared.FloatToMoney(amount, invoiceCurrency))
	if err != nil {
		return err
	}
	if newPaid.Amount < 0 {
		newPaid.Amount = 0 // a reversal can never drive paid below zero
	}
	a.PaidAmount = newPaid.MoneyToFloat()
	outstanding := shared.FloatToMoney(a.Total, invoiceCurrency).Amount - newPaid.Amount

	switch {
	case outstanding < 0:
		a.CreditBalance = shared.Money{Amount: -outstanding, Currency: invoiceCurrency}.MoneyToFloat()
		a.PaymentStatus = PaymentStatusPaid
		a.Status = InvoiceStatusPaid
	case outstanding == 0:
		a.CreditBalance = 0
		a.PaymentStatus = PaymentStatusPaid
		a.Status = InvoiceStatusPaid
	default:
		a.CreditBalance = 0
		if newPaid.Amount > 0 {
			a.PaymentStatus = PaymentStatusPartiallyPaid
		} else {
			a.PaymentStatus = PaymentStatusPending
		}
		if a.Status == InvoiceStatusPaid {
			a.Status = InvoiceStatusOutstanding // reversal reopens a paid invoice
		}
	}

	a.UpdatedAt = now

	a.events = append(a.events, InvoicePaymentAppliedEvent{
		ID:            a.ID,
		TenantID:      a.TenantID,
		Amount:        amount,
		PaidAmount:    a.PaidAmount,
		PaymentStatus: a.PaymentStatus,
		OccurredAt:    now,
	})

	return nil
}

// MarkIssued transitions invoice to issued status with a due date.
// Returns error if invoice is not in draft status.
func (a *InvoiceAggregate) MarkIssued(dueDate time.Time, now time.Time) error {
	if a.Status != InvoiceStatusDraft {
		return errors.New("only draft invoices can be issued")
	}

	a.Status = InvoiceStatusIssued
	a.DueDate = &dueDate
	a.UpdatedAt = now

	a.events = append(a.events, InvoiceIssuedEvent{
		ID:         a.ID,
		TenantID:   a.TenantID,
		DueDate:    dueDate,
		OccurredAt: now,
	})

	return nil
}

// Void transitions invoice to cancelled status.
// Returns error if invoice is paid or already cancelled.
func (a *InvoiceAggregate) Void(now time.Time) error {
	if a.Status == InvoiceStatusPaid {
		return errors.New("paid invoices cannot be voided")
	}
	if a.Status == InvoiceStatusCancelled {
		return errors.New("invoice already cancelled")
	}

	a.Status = InvoiceStatusCancelled
	a.UpdatedAt = now

	a.events = append(a.events, InvoiceVoidedEvent{
		ID:         a.ID,
		TenantID:   a.TenantID,
		OccurredAt: now,
	})

	return nil
}

// ValidateInvoiceNumber returns error if invoice number is empty.
func ValidateInvoiceNumber(number string) error {
	if number == "" {
		return errors.New("invoice number is required")
	}
	return nil
}

// InvoiceGeneratedEvent emitted when an invoice is generated.
type InvoiceGeneratedEvent struct {
	ID            InvoiceID
	TenantID      shared.TenantID
	InvoiceNumber string
	Total         float64
	CreatedAt     time.Time
}

// InvoicePaymentUpdatedEvent emitted when an invoice payment status changes.
type InvoicePaymentUpdatedEvent struct {
	ID            InvoiceID
	TenantID      shared.TenantID
	PaymentStatus PaymentStatus
	UpdatedAt     time.Time
}

// InvoicePaymentAppliedEvent emitted when a payment is applied to an invoice.
type InvoicePaymentAppliedEvent struct {
	ID            InvoiceID
	TenantID      shared.TenantID
	Amount        float64
	PaidAmount    float64
	PaymentStatus PaymentStatus
	OccurredAt    time.Time
}

// InvoiceIssuedEvent emitted when an invoice transitions to issued status.
type InvoiceIssuedEvent struct {
	ID         InvoiceID
	TenantID   shared.TenantID
	DueDate    time.Time
	OccurredAt time.Time
}

// InvoiceVoidedEvent emitted when an invoice is voided.
type InvoiceVoidedEvent struct {
	ID         InvoiceID
	TenantID   shared.TenantID
	OccurredAt time.Time
}
