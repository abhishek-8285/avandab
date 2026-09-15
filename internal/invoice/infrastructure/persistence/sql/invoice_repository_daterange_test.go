package sql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	invoicedomain "transport-app/internal/invoice/domain"
	"transport-app/internal/shared"
)

// TestInvoiceRepository_SearchReadModelsDateRange proves the from/to window
// filters on the invoice date (created_at, stored RFC3339) used by the
// invoices list page calendar.
func TestInvoiceRepository_SearchReadModelsDateRange(t *testing.T) {
	dbConn := setupInvoiceDB(t)
	defer func() { _ = dbConn.Close() }()

	repo, ok := NewInvoiceRepository(dbConn).(interface {
		SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]invoicedomain.InvoiceReadModel, int64, error)
	})
	require.True(t, ok, "invoice repo must implement date-range search")

	ctx := context.Background()
	// Timestamps seeded RFC3339 (with "T") exactly as the Go driver writes
	// them — SQLite date() cannot parse these directly, hence substr.
	mk := func(id, num, createdAt, payStatus string) {
		_, err := dbConn.Exec(`INSERT INTO invoices
			(id, invoice_number, booking_id, customer_id, subtotal, total, payment_status, created_at, updated_at)
			VALUES (?, ?, 'bk-1', 'cust-1', 100.0, 118.0, ?, ?, ?)`,
			id, num, payStatus, createdAt, createdAt)
		require.NoError(t, err)
	}
	mk("inv-1", "INV-AUG01", "2026-08-01T08:00:00Z", "pending")
	mk("inv-2", "INV-AUG10", "2026-08-10T08:00:00Z", "paid")
	mk("inv-3", "INV-AUG20", "2026-08-20T08:00:00Z", "partially_paid")
	mk("inv-4", "INV-SEP05", "2026-09-05T08:00:00Z", "pending")
	// IST-day-boundary row: 2026-08-09T18:45:00Z = 00:15 IST Aug 10.
	mk("inv-5", "INV-AUG10-IST", "2026-08-09T18:45:00Z", "pending")

	// Month window
	rows, total, err := repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	assert.Len(t, rows, 4)
	for _, r := range rows {
		assert.Contains(t, []string{"INV-AUG01", "INV-AUG10", "INV-AUG20", "INV-AUG10-IST"}, r.InvoiceNumber)
	}

	// Single-day window (from == to) — the boundary row belongs to Aug 10.
	rows, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-08-10", "2026-08-10", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, rows, 2)
	nums := []string{rows[0].InvoiceNumber, rows[1].InvoiceNumber}
	assert.Contains(t, nums, "INV-AUG10")
	assert.Contains(t, nums, "INV-AUG10-IST")

	// From-only bound
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-09-01", "", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// To-only bound
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "", "2026-08-05", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// Status + date combined: pending rows are now INV-AUG01, INV-SEP05(excluded:
	// September) and the "pending" IST-boundary row INV-AUG10-IST.
	rows, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "pending", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, rows, 2)
	nums = []string{rows[0].InvoiceNumber, rows[1].InvoiceNumber}
	assert.Contains(t, nums, "INV-AUG01")
	assert.Contains(t, nums, "INV-AUG10-IST")

	// Search query still applies within the window
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "AUG20", "", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// Tenant isolation: other tenant sees nothing
	_, total, err = repo.SearchReadModelsDateRange(ctx, "2", "", "", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
}
