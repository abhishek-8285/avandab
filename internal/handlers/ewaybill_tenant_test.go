package handlers

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// eway_bills has no tenant column; ownership resolves through trips. A missing
// tenant must never resolve as owned.
func TestEWayBillOwnershipResolvesThroughTripTenant(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE trips (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL);
		CREATE TABLE eway_bills (ewb_number TEXT PRIMARY KEY, trip_id TEXT NOT NULL);
		INSERT INTO trips (id, tenant_id) VALUES ('trip-a', 'tenant-a'), ('trip-b', 'tenant-b');
		INSERT INTO eway_bills (ewb_number, trip_id) VALUES ('EWB-A', 'trip-a'), ('EWB-B', 'trip-b');
	`)
	require.NoError(t, err)

	h := &EWayBillHandlers{App: &App{DB: db}}
	ctx := context.Background()

	ok, err := h.ewbOwned(ctx, "EWB-A", "tenant-a")
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = h.ewbOwned(ctx, "EWB-A", "tenant-b")
	require.NoError(t, err)
	require.False(t, ok, "another tenant's EWB must not resolve as owned")

	ok, err = h.ewbOwned(ctx, "EWB-A", "")
	require.NoError(t, err)
	require.False(t, ok, "empty tenant must fail closed")

	ok, err = h.tripOwned(ctx, "trip-b", "tenant-a")
	require.NoError(t, err)
	require.False(t, ok)
}
