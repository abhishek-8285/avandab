package application

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestTenantOwnershipQueries_FailClosedAndTenantScoped(t *testing.T) {
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

	q := NewTenantOwnershipQueries(db)
	ctx := context.Background()

	ok, err := q.TripOwned(ctx, "trip-a", "tenant-a")
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = q.TripOwned(ctx, "trip-b", "tenant-a")
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = q.EWBOwned(ctx, "EWB-A", "tenant-a")
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = q.EWBOwned(ctx, "EWB-A", "tenant-b")
	require.NoError(t, err)
	require.False(t, ok)

	// Empty tenant fails closed.
	ok, err = q.EWBOwned(ctx, "EWB-A", "")
	require.NoError(t, err)
	require.False(t, ok)
}
