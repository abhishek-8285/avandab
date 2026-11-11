package application

// Focused tests for the extracted bookings board read model (handlers-end-
// state ADR, Phase 2): tenant isolation and active-trip join semantics.

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

const sweepBoardSchema = `
CREATE TABLE bookings (id TEXT PRIMARY KEY, tenant_id TEXT, booking_number TEXT, customer_id TEXT,
  status TEXT, price REAL, pickup_date DATETIME, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE customers (id TEXT PRIMARY KEY, name TEXT);
CREATE TABLE trips (id TEXT PRIMARY KEY, booking_id TEXT, status TEXT, vehicle_id TEXT, driver_id TEXT);
CREATE TABLE vehicles (id TEXT PRIMARY KEY, vehicle_number TEXT);
CREATE TABLE drivers (id TEXT PRIMARY KEY, first_name TEXT, last_name TEXT);
`

func TestSweepBoardRows_TenantScopedAndActiveTripJoin(t *testing.T) {
	name := fmt.Sprintf("sweep_board_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(sweepBoardSchema)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers (id, name) VALUES ('c-1', 'Acme')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, vehicle_number) VALUES ('v-1', 'VAN-1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, first_name, last_name) VALUES ('d-1', 'Asha', 'Rao')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, status, price, pickup_date, created_at) VALUES
		('b-a1', 'tenant-a', 'BK-A1', 'c-1', 'confirmed', 1200, '2026-10-01 09:00:00', '2026-10-01 00:00:00'),
		('b-a2', 'tenant-a', 'BK-A2', 'c-1', 'pending', 900, NULL, '2026-10-02 00:00:00'),
		('b-b1', 'tenant-b', 'BK-B1', 'c-1', 'pending', 500, NULL, '2026-10-03 00:00:00')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, booking_id, status, vehicle_id, driver_id) VALUES ('t-1', 'b-a1', 'in_transit', 'v-1', 'd-1')`)
	require.NoError(t, err)

	rows, err := NewBoardQueries(db).Rows(context.Background(), "tenant-a")
	require.NoError(t, err)
	require.Len(t, rows, 2, "only tenant-a's bookings")
	assert.Equal(t, "b-a2", rows[0].ID, "newest created_at first")
	assert.Equal(t, "b-a1", rows[1].ID)
	assert.Equal(t, "Acme", rows[1].Customer)
	assert.Equal(t, 1200.0, rows[1].Freight)
	require.True(t, rows[1].Pickup.Valid)
	assert.Equal(t, "VAN-1", rows[1].Vehicle, "active trip joins the vehicle")
	assert.Equal(t, "Asha Rao", rows[1].Driver)
	assert.Equal(t, "pending", rows[0].Status)
	assert.False(t, rows[0].Pickup.Valid)

	rows, err = NewBoardQueries(nil).Rows(context.Background(), "tenant-a")
	require.NoError(t, err)
	assert.Nil(t, rows)
}
