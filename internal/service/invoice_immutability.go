package service

import (
	"context"
	"database/sql"
	"fmt"

	"transport-app/internal/domain"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
)

// GST invoice immutability guards. Once an invoice carries an IRN
// (e-invoice registered with the GSTN) it is legally immutable: any
// correction must flow through credit/debit notes. Invoices with recorded
// payments must also never be hard-deleted — the payments table cascades,
// so deletion would silently destroy payment history.

// invoiceIRN reads the IRN for an invoice straight from the invoices table.
// The legacy read model (InvoiceWithJoins) does not surface e-invoicing
// columns, so this uses the raw DB handle exposed by the SQLite store.
// Returns "" when the store has no DB access or the invoice has no IRN.
func (s *InvoiceService) invoiceIRN(ctx context.Context, id domain.InvoiceID) string {
	dbGetter, ok := s.store.(repository.DBGetter)
	if !ok || dbGetter == nil || dbGetter.DB() == nil {
		return ""
	}
	var irn sql.NullString
	err := dbGetter.DB().QueryRowContext(ctx,
		`SELECT irn FROM invoices WHERE id = ? AND tenant_id = ?`,
		string(id), string(shared.TenantIDFromContext(ctx))).Scan(&irn)
	if err != nil || !irn.Valid {
		return ""
	}
	return irn.String
}

// ensureInvoiceNotLocked blocks destructive operations on invoices that are
// e-invoiced or already carry payments.
func (s *InvoiceService) ensureInvoiceNotLocked(ctx context.Context, id domain.InvoiceID) error {
	if s.invoiceIRN(ctx, id) != "" {
		return domain.ErrInvoiceEInvoiced
	}
	payments, err := s.store.GetPaymentsByInvoice(ctx, id)
	if err != nil {
		return fmt.Errorf("check payments for invoice: %w", err)
	}
	if len(payments) > 0 {
		return domain.ErrInvoiceHasPayments
	}
	return nil
}

// EnsureLineItemsEditable verifies line items of an invoice may still be
// added/edited/deleted: the invoice must have no IRN and its payment status
// must still be pending (no money moved). Handlers map the returned domain
// errors to HTTP 409.
func (s *InvoiceService) EnsureLineItemsEditable(ctx context.Context, id domain.InvoiceID) error {
	if err := s.ensureInvoiceNotLocked(ctx, id); err != nil {
		return err
	}
	invoice, err := s.store.GetInvoiceByID(ctx, id)
	if err != nil {
		return domain.ErrInvoiceNotFound
	}
	if invoice.PaymentStatus != domain.PaymentStatusPending {
		return domain.ErrInvoiceHasPayments
	}
	return nil
}
