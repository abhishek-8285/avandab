package invoice

import "errors"

var (
	ErrInvoiceNotFound  = errors.New("invoice not found")
	ErrDuplicateInvoice = errors.New("invoice already exists for this trip")

	// GST immutability: once an invoice is e-invoiced (IRN issued) or has
	// payments recorded, it cannot be mutated or deleted — corrections go
	// through credit/debit notes.
	ErrInvoiceEInvoiced   = errors.New("invoice is e-invoiced; issue credit note instead")
	ErrInvoiceHasPayments = errors.New("invoice has payments recorded; cannot delete")
)
