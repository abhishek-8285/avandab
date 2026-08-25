package sql

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/shared"
)

// setupIRNCancelDB builds the package fixture schema, whose invoices table
// now carries migration 00099's irn_cancelled_at column.
func setupIRNCancelDB(t *testing.T) *sql.DB {
	t.Helper()
	safeName := "irn_cancel_" + strings.NewReplacer("/", "_", " ", "_", "-", "_", "#", "_").Replace(t.Name())
	dsn := "file:" + safeName + "?mode=memory&cache=shared&_pragma=journal_mode(WAL)"
	dbConn, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })
	_, err = dbConn.Exec(invoiceSchema)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO customers (id, name, company) VALUES ('cust-1', 'Acme', 'Acme Corp')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO bookings (id, booking_number) VALUES ('bk-1', 'BK-0001')`)
	require.NoError(t, err)
	return dbConn
}

// TestInvoiceRepository_GetReadModel_IRNCancelledAt — migration 00099's
// irn_cancelled_at must flow through GetReadModel: NULL → "", set → value.
func TestInvoiceRepository_GetReadModel_IRNCancelledAt(t *testing.T) {
	db := setupIRNCancelDB(t)
	repo := NewInvoiceRepository(db)
	ctx := context.Background()

	agg := newInvoiceAggWithTrip("inv-irn-1", "t1", "INV-IRN-1", "bk-1", "cust-1", nil,
		1000, 0, 0, 1000, aggregate.PaymentStatusPending, time.Now())
	require.NoError(t, repo.Save(ctx, agg))

	rm, err := repo.GetReadModel(ctx, "inv-irn-1", "t1")
	require.NoError(t, err)
	assert.Empty(t, rm.IRNCancelledAt, "NULL irn_cancelled_at must map to empty string")

	cancelledAt := "2026-08-21 10:30:00"
	_, err = db.Exec(`UPDATE invoices SET irn = ?, irn_cancelled_at = ? WHERE id = ?`,
		"irn-cancel-test", cancelledAt, "inv-irn-1")
	require.NoError(t, err)

	rm, err = repo.GetReadModel(ctx, "inv-irn-1", "t1")
	require.NoError(t, err)
	// SQLite TIMESTAMP affinity round-trips through the driver in its own
	// layout (e.g. 2026-08-21T10:30:00Z); assert on the stored instant.
	assert.True(t, strings.HasPrefix(rm.IRNCancelledAt, "2026-08-21"),
		"got %q", rm.IRNCancelledAt)

	// Tenant isolation still holds with the extra column in play.
	_, err = repo.GetReadModel(ctx, "inv-irn-1", shared.TenantID("t2"))
	require.Error(t, err)
}
