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
	"transport-app/internal/shared"
)

func tollTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:fastag_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE trips (id TEXT PRIMARY KEY, tenant_id TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE fastag_tags (id TEXT PRIMARY KEY, tag_id TEXT, tenant_id TEXT, vehicle_number TEXT, balance REAL DEFAULT 0)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE fastag_transactions (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, tag_id TEXT, vehicle_number TEXT,
		trip_id TEXT, plaza_id TEXT, plaza_name TEXT, amount REAL, txn_timestamp DATETIME,
		status TEXT, source TEXT, reconciled INTEGER, provider_mode TEXT DEFAULT 'LIVE')`)
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

// A negative (or zero) toll must never be applied: subtracting a negative
// amount credits the tag balance.
func TestDeductToll_RejectsNonPositiveAmount(t *testing.T) {
	db := tollTestDB(t)
	_, err := db.Exec(`INSERT INTO fastag_tags (id, tag_id, tenant_id, vehicle_number, balance) VALUES ('row-1','TAG-1','tenant-A','DL-01-AA-1111',100)`)
	require.NoError(t, err)
	c := &clientImpl{cfg: Config{Enabled: true}, db: db}
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-A")

	_, err = c.DeductToll(ctx, DeductTollRequest{TagID: "TAG-1", VehicleNumber: "DL-01-AA-1111", Amount: -50, PlazaName: "X"})
	require.Error(t, err)

	var balance float64
	require.NoError(t, db.QueryRow(`SELECT balance FROM fastag_tags WHERE tag_id='TAG-1'`).Scan(&balance))
	require.Equal(t, 100.0, balance, "negative toll must not credit the balance")
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM fastag_transactions`).Scan(&count))
	require.Zero(t, count, "rejected toll must not be recorded")
}

// A toll recorded for tenant B must not decrement tenant A's tag balance even
// when the caller supplies A's tag id.
func TestDeductToll_BalanceUpdateIsTenantScoped(t *testing.T) {
	db := tollTestDB(t)
	_, err := db.Exec(`INSERT INTO fastag_tags (id, tag_id, tenant_id, vehicle_number, balance) VALUES ('row-a','TAG-A','tenant-A','DL-01-AA-1111',100)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, tenant_id) VALUES ('trip-b','tenant-B')`)
	require.NoError(t, err)

	c := &clientImpl{cfg: Config{Enabled: true}, db: db}
	_, err = c.DeductToll(context.Background(), DeductTollRequest{
		TagID: "TAG-A", VehicleNumber: "DL-01-AA-1111", Amount: 10, PlazaName: "X",
		TripID: "trip-b",
	})
	require.Error(t, err, "a toll must not use another tenant's tag")

	var balance float64
	require.NoError(t, db.QueryRow(`SELECT balance FROM fastag_tags WHERE tag_id='TAG-A'`).Scan(&balance))
	require.Equal(t, 100.0, balance, "another tenant's tag balance must not be decremented")
}

// Mock deductions must be labelled source=MOCK, never PROVIDER.
func TestDeductToll_MockSource(t *testing.T) {
	db := tollTestDB(t)
	_, err := db.Exec(`INSERT INTO fastag_tags (id, tag_id, tenant_id, vehicle_number, balance) VALUES ('row-m','TAG-M','tenant-M','DL-01-MM-1111',100)`)
	require.NoError(t, err)
	c := &clientImpl{cfg: Config{Enabled: true, UseMock: true}, db: db}
	_, err = c.DeductToll(context.Background(), DeductTollRequest{
		TagID: "TAG-M", VehicleNumber: "DL-01-MM-1111", Amount: 10, PlazaName: "X",
	})
	require.NoError(t, err)
	var mode string
	require.NoError(t, db.QueryRow(`SELECT provider_mode FROM fastag_transactions LIMIT 1`).Scan(&mode))
	require.Equal(t, "MOCK", mode)
}
