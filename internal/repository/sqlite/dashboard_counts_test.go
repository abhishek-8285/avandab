package sqlite

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
)

// TestDashboardCountChips proves the COUNT(*) twins used by dashboard chips:
// statuses counted exactly, other tenants excluded, zero when empty.
func TestDashboardCountChips(t *testing.T) {
	dbConn, err := sql.Open("sqlite", "file:dashboard_counts?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })
	_, err = dbConn.Exec(`
CREATE TABLE vehicles (id TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT 'available', tenant_id TEXT NOT NULL);
CREATE TABLE drivers (id TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT 'available', tenant_id TEXT NOT NULL);
CREATE TABLE invoices (id TEXT PRIMARY KEY, payment_status TEXT NOT NULL, tenant_id TEXT NOT NULL);
`)
	require.NoError(t, err)

	exec := func(q string, args ...any) {
		t.Helper()
		_, err := dbConn.Exec(q, args...)
		require.NoError(t, err)
	}

	// Vehicles: 2 available (tenant 1), plus in-trip/garage/foreign rows that must not count.
	exec(`INSERT INTO vehicles VALUES ('v1','available','1'),('v2','available','1'),('v3','in_trip','1'),('v4','maintenance','1'),('v5','available','2')`)
	// Drivers: 1 available (tenant 1), plus on_trip/foreign.
	exec(`INSERT INTO drivers VALUES ('d1','available','1'),('d2','on_trip','1'),('d3','available','2')`)
	// Invoices: pending+partially_paid count; paid/refunded/foreign do not.
	exec(`INSERT INTO invoices VALUES ('i1','pending','1'),('i2','partially_paid','1'),('i3','paid','1'),('i4','refunded','1'),('i5','pending','2')`)

	repo := NewRepository(dbConn)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	n, err := repo.CountAvailableVehicles(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 2, n, "available vehicles (tenant 1 only)")

	n, err = repo.CountAvailableDrivers(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "available drivers (tenant 1 only)")

	n, err = repo.CountPendingInvoices(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 2, n, "pending+partially_paid invoices (tenant 1 only)")

	// Other tenant sees only its own rows.
	ctx2 := shared.ContextWithTenantID(context.Background(), "2")
	n, err = repo.CountAvailableVehicles(ctx2)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "tenant 2 vehicles")
	n, err = repo.CountPendingInvoices(ctx2)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "tenant 2 pending invoices")

	// Empty tenant: zero counts, no errors.
	ctx3 := shared.ContextWithTenantID(context.Background(), "9")
	n, err = repo.CountAvailableVehicles(ctx3)
	require.NoError(t, err)
	assert.EqualValues(t, 0, n, "empty tenant vehicles")
}
