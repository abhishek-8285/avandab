package sql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	invoicedomain "transport-app/internal/invoice/domain"
	"transport-app/internal/shared"
)

// TestInvoiceRepository_SearchOutstandingFilter proves the "open"
// pseudo-status (dashboard Pending Payments drill-down →
// /invoices?status=open) returns pending + partially_paid and nothing else.
func TestInvoiceRepository_SearchOutstandingFilter(t *testing.T) {
	dbConn := setupInvoiceDB(t)
	defer func() { _ = dbConn.Close() }()

	repo := NewInvoiceRepository(dbConn)

	ctx := context.Background()
	mk := func(id, num, tenant, payStatus string) {
		_, err := dbConn.Exec(`INSERT INTO invoices
			(id, invoice_number, booking_id, customer_id, subtotal, total, payment_status, tenant_id, created_at, updated_at)
			VALUES (?, ?, 'bk-1', 'cust-1', 100.0, 118.0, ?, ?, '2026-08-10T08:00:00Z', '2026-08-10T08:00:00Z')`,
			id, num, payStatus, tenant)
		require.NoError(t, err)
	}
	mk("inv-pend", "INV-PEND", "1", "pending")
	mk("inv-part", "INV-PART", "1", "partially_paid")
	mk("inv-paid", "INV-PAID", "1", "paid")
	mk("inv-other", "INV-OTHER", "2", "pending")

	rows, total, err := repo.SearchReadModels(ctx, "1", "", "open", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total, "open = pending + partially_paid")
	require.Len(t, rows, 2)
	for _, r := range rows {
		assert.Contains(t, []string{"INV-PEND", "INV-PART"}, r.InvoiceNumber)
	}

	// Exact statuses keep working.
	_, total, err = repo.SearchReadModels(ctx, "1", "", "pending", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	_, total, err = repo.SearchReadModels(ctx, "1", "", "paid", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// Date-range variant honors "open" with and without a window.
	dateRepo, ok := repo.(interface {
		SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]invoicedomain.InvoiceReadModel, int64, error)
	})
	require.True(t, ok)
	_, total, err = dateRepo.SearchReadModelsDateRange(ctx, "1", "", "open", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	_, total, err = dateRepo.SearchReadModelsDateRange(ctx, "1", "", "open", "2026-09-01", "", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total, "window excludes August invoices")
}
