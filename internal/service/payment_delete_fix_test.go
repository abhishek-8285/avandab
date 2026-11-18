package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
	"transport-app/internal/shared"
)

func TestDeletePayment_CompensatingLedgerAndRecalc(t *testing.T) {
	dbConn, svcs := openMigratedDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))

	_, err := dbConn.Exec(`INSERT INTO customers (id, name, phone) VALUES ('cus-del', 'Delete Customer', '+919999999999')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO invoices
		(id, invoice_number, booking_id, customer_id, subtotal, total, tenant_id)
		VALUES ('inv-del', 'INV-DEL', 'bk-del', 'cus-del', 100.0, 100.0, 'tenant-1')`)
	require.NoError(t, err)

	p1, err := svcs.Payments.RecordPayment(ctx, domain.InvoiceID("inv-del"), 60.0, domain.PaymentMethodCash, "r1", "", "")
	require.NoError(t, err)
	p2, err := svcs.Payments.RecordPayment(ctx, domain.InvoiceID("inv-del"), 40.0, domain.PaymentMethodCash, "r2", "", "")
	require.NoError(t, err)

	var status string
	require.NoError(t, dbConn.QueryRow(`SELECT payment_status FROM invoices WHERE id = 'inv-del'`).Scan(&status))
	require.Equal(t, "paid", status)

	require.NoError(t, svcs.Payments.DeletePayment(ctx, p2.ID))

	// Compensating ledger entry: the deleted payment's credit is reversed.
	var direction, refID string
	var amountMinor int64
	require.NoError(t, dbConn.QueryRow(`
		SELECT direction, ref_id, amount_minor FROM money_ledger
		WHERE ref_table = 'payments' AND direction = 'debit' AND ref_id = ?`, string(p2.ID)).
		Scan(&direction, &refID, &amountMinor))
	assert.Equal(t, "debit", direction)
	assert.Equal(t, int64(4000), amountMinor)

	// Invoice status recalculated after the delete.
	require.NoError(t, dbConn.QueryRow(`SELECT payment_status FROM invoices WHERE id = 'inv-del'`).Scan(&status))
	assert.Equal(t, "partially_paid", status, "invoice status must be recalculated after delete")
	_ = p1
}

func TestDeletePayment_RecalcErrorIsReturned(t *testing.T) {
	dbConn, svcs := openMigratedDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))

	_, err := dbConn.Exec(`INSERT INTO customers (id, name, phone) VALUES ('cus-del2', 'Delete Customer 2', '+919999999999')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO invoices
		(id, invoice_number, booking_id, customer_id, subtotal, total, tenant_id)
		VALUES ('inv-del2', 'INV-DEL2', 'bk-del2', 'cus-del2', 100.0, 100.0, 'tenant-1')`)
	require.NoError(t, err)
	p, err := svcs.Payments.RecordPayment(ctx, domain.InvoiceID("inv-del2"), 40.0, domain.PaymentMethodCash, "r3", "", "")
	require.NoError(t, err)

	_, err = dbConn.Exec(`CREATE TRIGGER fail_inv_status BEFORE UPDATE ON invoices
		BEGIN SELECT RAISE(ABORT, 'forced recalc failure'); END`)
	require.NoError(t, err)

	err = svcs.Payments.DeletePayment(ctx, p.ID)
	assert.Error(t, err, "invoice recalculation failure must be returned, not swallowed")
}
