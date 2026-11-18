package sql

import (
	"context"
	stdSQL "database/sql"
	"fmt"

	"transport-app/internal/payment/orderstore"
)

// RazorpayOrderSQLStore persists the order -> invoice binding that makes
// /verify reject an order created for a different invoice or tenant.
type RazorpayOrderSQLStore struct {
	db *stdSQL.DB
}

func NewRazorpayOrderSQLStore(db *stdSQL.DB) *RazorpayOrderSQLStore {
	return &RazorpayOrderSQLStore{db: db}
}

func (s *RazorpayOrderSQLStore) SaveOrder(ctx context.Context, tenantID, invoiceID, orderID string, amountPaise int64, currency string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("razorpay order store: database unavailable")
	}
	if currency == "" {
		currency = "INR"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO razorpay_orders (order_id, tenant_id, invoice_id, amount_paise, currency)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (order_id) DO NOTHING`,
		orderID, tenantID, invoiceID, amountPaise, currency)
	if err != nil {
		return fmt.Errorf("save razorpay order binding: %w", err)
	}
	return nil
}

func (s *RazorpayOrderSQLStore) GetOrder(ctx context.Context, orderID string) (orderstore.Binding, error) {
	var b orderstore.Binding
	if s == nil || s.db == nil {
		return b, fmt.Errorf("razorpay order store: database unavailable")
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, invoice_id, amount_paise
		FROM razorpay_orders
		WHERE order_id = $1`, orderID).
		Scan(&b.TenantID, &b.InvoiceID, &b.AmountPaise)
	if err != nil {
		return b, err
	}
	return b, nil
}
