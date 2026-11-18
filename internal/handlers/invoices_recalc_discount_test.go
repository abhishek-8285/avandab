package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

// TestRecalculateInvoiceTotals_PreservesLegacyLinesAndDiscount covers the two
// ways a line-item edit used to destroy invoice money: legacy auto-generated
// lines store taxable_value/total = 0, and the header discount was never
// subtracted from the recomputed total.
func TestRecalculateInvoiceTotals_PreservesLegacyLinesAndDiscount(t *testing.T) {
	db := newInvoiceLineTestDB(t)
	h := &InvoiceHandlers{App: &App{DB: db}}

	_, err := db.Exec(`INSERT INTO customers (id, name, phone, gst) VALUES ('cust-recalc', 'Recalc Buyer', '+91-9000000002', '27ABCDE1234F1Z5')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt-recalc', '1', 'Mumbai', 'Pune', 150, 3, 5000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price) VALUES ('bk-recalc', '1', 'BK-REC', 'cust-recalc', date('now','+1 day'), 'rt-recalc', 'truck', 80000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, tax, discount, total, tenant_id)
		VALUES ('inv-recalc', 'INV-REC', 'bk-recalc', 'cust-recalc', 80500, 14490, 1000, 93990, '1')`)
	require.NoError(t, err)
	// Legacy freight line: only amount + tax components were persisted.
	_, err = db.Exec(`INSERT INTO invoice_line_items (id, tenant_id, invoice_id, line_type, description, amount, taxable_value, cgst_amount, sgst_amount, total)
		VALUES ('li-legacy', '1', 'inv-recalc', 'freight', 'Freight', 80000, 0, 7200, 7200, 0)`)
	require.NoError(t, err)
	// Newly added line: full money columns populated.
	_, err = db.Exec(`INSERT INTO invoice_line_items (id, tenant_id, invoice_id, line_type, description, amount, taxable_value, cgst_amount, sgst_amount, total)
		VALUES ('li-new', '1', 'inv-recalc', 'detention', 'Detention', 500, 500, 45, 45, 590)`)
	require.NoError(t, err)

	require.True(t, h.recalculateInvoiceTotalsTx(context.Background(), db, shared.DefaultTenant, "inv-recalc"))

	var subtotal, tax, discount, total float64
	err = db.QueryRow(`SELECT subtotal, tax, discount, total FROM invoices WHERE id = 'inv-recalc'`).Scan(&subtotal, &tax, &discount, &total)
	require.NoError(t, err)
	assert.InDelta(t, 80500, subtotal, 0.001)
	assert.InDelta(t, 14490, tax, 0.001)
	assert.InDelta(t, 1000, discount, 0.001, "discount must remain untouched")
	assert.InDelta(t, 93990, total, 0.001, "legacy line money + new line - discount")
}
