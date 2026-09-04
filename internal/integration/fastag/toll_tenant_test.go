package fastag

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func tollTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:fastag_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE trips (id TEXT PRIMARY KEY, tenant_id TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE fastag_tags (id TEXT PRIMARY KEY, tag_id TEXT, tenant_id TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE fastag_transactions (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, tag_id TEXT, vehicle_number TEXT,
		trip_id TEXT, plaza_id TEXT, plaza_name TEXT, amount REAL, txn_timestamp DATETIME,
		status TEXT, source TEXT, reconciled INTEGER)`)
	require.NoError(t, err)
	return db
}

// Toll money without an attributable tenant must be refused, never filed
// under the bootstrap tenant.
func TestDeductToll_RequiresTenant(t *testing.T) {
	db := tollTestDB(t)
	c := &clientImpl{cfg: Config{Enabled: true}, db: db}

	_, err := c.DeductToll(context.Background(), DeductTollRequest{
		VehicleNumber: "DL-01-AB-1234", Amount: 95, PlazaName: "Gharaunda",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without tenant")

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM fastag_transactions`).Scan(&count))
	assert.Equal(t, 0, count)
}

// Tenant resolves from the trip row when context carries none.
func TestDeductToll_TenantFromTrip(t *testing.T) {
	db := tollTestDB(t)
	_, err := db.Exec(`INSERT INTO trips (id, tenant_id) VALUES ('trip-toll-1', 'tenant-T')`)
	require.NoError(t, err)
	c := &clientImpl{cfg: Config{Enabled: true}, db: db}

	_, err = c.DeductToll(context.Background(), DeductTollRequest{
		VehicleNumber: "DL-01-AB-1234", Amount: 95, PlazaName: "Gharaunda", TripID: "trip-toll-1",
	})
	require.NoError(t, err)

	var tenant string
	require.NoError(t, db.QueryRow(`SELECT tenant_id FROM fastag_transactions LIMIT 1`).Scan(&tenant))
	assert.Equal(t, "tenant-T", tenant)
}
