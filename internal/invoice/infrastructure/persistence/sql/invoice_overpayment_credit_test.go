package sql

// Red-green regression for invoice overpayment + float-money defects
// (aggregate ApplyPayment float add, exact-zero compare, capped paid with
// in-memory-only CreditBalance; repo reload forcing credit to 0; reversal
// subtracting the full original from capped paid). Billing spec
// docs/05-BILLING-GST-AND-SETTLEMENTS.md §§2/4: Outstanding = Total - Paid,
// Outstanding == 0 -> Paid. Uses in-memory SQLite only, no migrations.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/shared"
)

func newOverpayInvoice(t *testing.T, repo interface {
	Save(ctx context.Context, inv *aggregate.InvoiceAggregate) error
}, ctx context.Context, id, number string, total float64) *aggregate.InvoiceAggregate {
	t.Helper()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	inv := aggregate.NewInvoiceAggregate(
		aggregate.InvoiceID(id),
		shared.TenantID("1"),
		number,
		"bk-1",
		"cust-1",
		nil,
		total, 0, 0, total,
		aggregate.PaymentStatusPending,
		now,
	)
	require.NoError(t, repo.Save(ctx, inv))
	return inv
}

// Credit must survive Save -> Find. Pre-fix: repo reload forces 0.
func TestOverpaymentCreditSurvivesSaveReload(t *testing.T) {
	dbConn := setupInvoiceDB(t)
	defer func() { _ = dbConn.Close() }()
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()

	inv := newOverpayInvoice(t, repo, ctx, "inv-overpay-1", "INV-OVER-001", 1000)
	require.NoError(t, inv.ApplyPayment(1200, time.Now()))
	require.NoError(t, repo.Save(ctx, inv))

	found, err := repo.Find(ctx, "inv-overpay-1", shared.TenantID("1"))
	require.NoError(t, err)
	assert.Equal(t, 1200.0, found.PaidAmount, "gross paid persists so credit is derivable")
	assert.Equal(t, 200.0, found.CreditBalance, "overpayment excess survives reload")
	assert.Equal(t, 0.0, found.OutstandingBalance())
	assert.Equal(t, aggregate.PaymentStatusPaid, found.PaymentStatus)
}

// Reversing the overpayment must balance to zero. Pre-fix: reversal
// subtracts the full 1200 from capped paid 1000 and lands at -200.
func TestOverpaymentReversalBalances(t *testing.T) {
	dbConn := setupInvoiceDB(t)
	defer func() { _ = dbConn.Close() }()
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()

	inv := newOverpayInvoice(t, repo, ctx, "inv-overpay-2", "INV-OVER-002", 1000)
	require.NoError(t, inv.ApplyPayment(1200, time.Now()))
	require.NoError(t, repo.Save(ctx, inv))

	found, err := repo.Find(ctx, "inv-overpay-2", shared.TenantID("1"))
	require.NoError(t, err)
	// Same call reverse_payment.go makes: ApplyPayment(-original.Amount).
	require.NoError(t, found.ApplyPayment(-1200, time.Now()))
	require.NoError(t, repo.Save(ctx, found))

	balanced, err := repo.Find(ctx, "inv-overpay-2", shared.TenantID("1"))
	require.NoError(t, err)
	assert.Equal(t, 0.0, balanced.PaidAmount)
	assert.Equal(t, 0.0, balanced.CreditBalance)
	assert.Equal(t, 1000.0, balanced.OutstandingBalance())
	assert.NotEqual(t, aggregate.PaymentStatusPaid, balanced.PaymentStatus)
}

// Fractional payments must hit exact zero via minor units. Pre-fix float
// dust either false-triggers overpay (0.1+0.2 > 0.3) or misses zero.
func TestFractionalPaymentsReachExactZero(t *testing.T) {
	dbConn := setupInvoiceDB(t)
	defer func() { _ = dbConn.Close() }()
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()

	t.Run("tenth_plus_fifth", func(t *testing.T) {
		inv := newOverpayInvoice(t, repo, ctx, "inv-frac-1", "INV-FRAC-001", 0.3)
		require.NoError(t, inv.ApplyPayment(0.1, time.Now()))
		require.NoError(t, inv.ApplyPayment(0.2, time.Now()))
		assert.Equal(t, 0.0, inv.CreditBalance, "float dust must not fake an overpayment")
		assert.Equal(t, 0.0, inv.OutstandingBalance())
		assert.Equal(t, aggregate.PaymentStatusPaid, inv.PaymentStatus)
		require.NoError(t, repo.Save(ctx, inv))
		found, err := repo.Find(ctx, "inv-frac-1", shared.TenantID("1"))
		require.NoError(t, err)
		assert.Equal(t, aggregate.PaymentStatusPaid, found.PaymentStatus)
		assert.Equal(t, 0.0, found.OutstandingBalance())
	})

	t.Run("ten_dimes", func(t *testing.T) {
		inv := newOverpayInvoice(t, repo, ctx, "inv-frac-2", "INV-FRAC-002", 1.0)
		for range 10 {
			require.NoError(t, inv.ApplyPayment(0.1, time.Now()))
		}
		assert.Equal(t, 0.0, inv.OutstandingBalance())
		assert.Equal(t, aggregate.PaymentStatusPaid, inv.PaymentStatus)
	})
}
