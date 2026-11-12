package application

// Focused test for the agent dashboard snapshot (Phase 3): card math +
// tenant isolation.

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

func TestAgentDashboardQueries_Snapshot(t *testing.T) {
	name := fmt.Sprintf("agent_dash_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`
		CREATE TABLE trips (id TEXT PRIMARY KEY, tenant_id TEXT, departure_time DATETIME, status TEXT);
		CREATE TABLE vehicles (id TEXT PRIMARY KEY, tenant_id TEXT, status TEXT);
		CREATE TABLE drivers (id TEXT PRIMARY KEY, tenant_id TEXT, status TEXT);
		CREATE TABLE invoices (id TEXT PRIMARY KEY, tenant_id TEXT, payment_status TEXT);
		CREATE TABLE payments (id TEXT PRIMARY KEY, tenant_id TEXT, payment_date DATETIME, amount REAL)`)
	require.NoError(t, err)
	today := time.Now().Format("2006-01-02")
	_, err = db.Exec(`INSERT INTO trips (id, tenant_id, departure_time, status) VALUES
		('t1','tenant-a',?,'completed'), ('t2','tenant-a',?,'in_transit'), ('t3','tenant-a',?,'cancelled'), ('t4','tenant-b',?,'completed')`,
		today+" 08:00:00", today+" 09:00:00", today+" 10:00:00", today+" 11:00:00")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, status) VALUES ('v1','tenant-a','available'),('v2','tenant-a','on_trip'),('v3','tenant-b','available')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, tenant_id, status) VALUES ('d1','tenant-a','available'),('d2','tenant-b','available')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices (id, tenant_id, payment_status) VALUES ('i1','tenant-a','pending'),('i2','tenant-a','paid'),('i3','tenant-b','pending')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO payments (id, tenant_id, payment_date, amount) VALUES ('p1','tenant-a',?,100),('p2','tenant-b',?,999)`,
		time.Now().Format("2006-01-02")+" 10:00:00", time.Now().Format("2006-01-02")+" 10:00:00")
	require.NoError(t, err)

	snap, err := NewAgentDashboardQueries(db).Snapshot(context.Background(), shared.TenantID("tenant-a"))
	require.NoError(t, err)
	assert.Equal(t, int64(3), snap.TodayTripsCount)
	assert.Equal(t, int64(1), snap.CompletedTrips)
	assert.Equal(t, int64(1), snap.CancelledTrips)
	assert.Equal(t, int64(1), snap.ActiveTrips)
	assert.Equal(t, int64(1), snap.AvailableVehicles)
	assert.Equal(t, int64(1), snap.AvailableDrivers)
	assert.Equal(t, int64(1), snap.PendingPayments)
	assert.Equal(t, 100.0, snap.MonthlyRevenue)

	other, err := NewAgentDashboardQueries(db).Snapshot(context.Background(), shared.TenantID("tenant-b"))
	require.NoError(t, err)
	assert.Equal(t, int64(1), other.TodayTripsCount)
	assert.Equal(t, 999.0, other.MonthlyRevenue)
}
