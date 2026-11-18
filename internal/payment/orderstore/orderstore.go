// Package orderstore defines the server-side binding between a Razorpay order
// and the invoice it was created for. It lives outside the application package
// so the SQL implementation can depend on it without an import cycle.
package orderstore

import "context"

// Binding is the invoice a gateway order belongs to.
type Binding struct {
	TenantID    string
	InvoiceID   string
	AmountPaise int64
}

// Store persists and resolves order -> invoice bindings.
type Store interface {
	SaveOrder(ctx context.Context, tenantID, invoiceID, orderID string, amountPaise int64, currency string) error
	GetOrder(ctx context.Context, orderID string) (Binding, error)
}
