package sql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	paymentdomain "transport-app/internal/payment/domain"
	"transport-app/internal/shared"
)

// TestPaymentRepository_SearchReadModelsDateRange proves the from/to window
// filters on payment_date (stored RFC3339) used by the payments list page
// calendar.
func TestPaymentRepository_SearchReadModelsDateRange(t *testing.T) {
	dbConn := setupPaymentDB(t)
	defer func() { _ = dbConn.Close() }()

	repo, ok := NewPaymentRepository(dbConn).(interface {
		SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, method string, from string, to string, limit int, offset int) ([]paymentdomain.PaymentReadModel, int64, error)
	})
	require.True(t, ok, "payment repo must implement date-range search")

	ctx := context.Background()
	// Timestamps seeded RFC3339 (with "T") exactly as the Go driver writes
	// them — SQLite date() cannot parse these directly, hence substr.
	mk := func(id, payDate, method string) {
		_, err := dbConn.Exec(`INSERT INTO payments
			(id, invoice_id, payment_date, amount, method, tenant_id)
			VALUES (?, 'inv-1', ?, 500.0, ?, '1')`,
			id, payDate, method)
		require.NoError(t, err)
	}
	mk("pay-1", "2026-08-01T08:00:00Z", "upi")
	mk("pay-2", "2026-08-10T08:00:00Z", "cash")
	mk("pay-3", "2026-08-20T08:00:00Z", "bank_transfer")
	mk("pay-4", "2026-09-05T08:00:00Z", "upi")

	// Month window
	rows, total, err := repo.SearchReadModelsDateRange(ctx, "1", "", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	assert.Len(t, rows, 3)
	for _, r := range rows {
		assert.Contains(t, []string{"pay-1", "pay-2", "pay-3"}, r.ID)
	}

	// Single-day window (from == to)
	rows, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "2026-08-10", "2026-08-10", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	assert.Equal(t, "pay-2", rows[0].ID)

	// From-only bound
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "2026-09-01", "", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// To-only bound
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-08-05", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// Method + date combined
	rows, total, err = repo.SearchReadModelsDateRange(ctx, "1", "upi", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	assert.Equal(t, "pay-1", rows[0].ID)
	assert.Equal(t, "INV-0001", rows[0].InvoiceNumber)

	// Tenant isolation: other tenant sees nothing
	_, total, err = repo.SearchReadModelsDateRange(ctx, "2", "", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
}
