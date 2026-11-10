package kpi

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

func newKPITestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("kpi_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE bookings (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, status TEXT NOT NULL, price REAL NOT NULL DEFAULT 0, created_at DATETIME NOT NULL);
		CREATE TABLE trips (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, status TEXT NOT NULL, created_at DATETIME NOT NULL);
		CREATE TABLE vehicles (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, status TEXT NOT NULL);
		CREATE TABLE drivers (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, status TEXT NOT NULL);
		CREATE TABLE payments (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, amount REAL NOT NULL, payment_date DATETIME NOT NULL);
		CREATE TABLE invoices (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, payment_status TEXT NOT NULL, total REAL NOT NULL, paid_amount REAL NOT NULL)`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestKPIReadModels_TenantScopedValues(t *testing.T) {
	db := newKPITestDB(t)
	ctx := context.Background()
	now := time.Now().Format("2006-01-02 15:04:05")
	old := time.Now().AddDate(0, -2, 0).Format("2006-01-02 15:04:05")

	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	// tenant-1
	exec(`INSERT INTO bookings VALUES ('b1','tenant-1','pending',500,?)`, now)
	exec(`INSERT INTO bookings VALUES ('b2','tenant-1','confirmed',1000,?)`, now)
	exec(`INSERT INTO bookings VALUES ('b3','tenant-1','confirmed',1000,?)`, now)
	exec(`INSERT INTO bookings VALUES ('b4','tenant-1','completed',900,?)`, old)
	exec(`INSERT INTO bookings VALUES ('b5','tenant-2','pending',999,?)`, now)
	exec(`INSERT INTO trips VALUES ('t1','tenant-1','assigned',?)`, now)
	exec(`INSERT INTO trips VALUES ('t2','tenant-1','in_transit',?)`, now)
	exec(`INSERT INTO trips VALUES ('t3','tenant-1','completed',?)`, old)
	exec(`INSERT INTO trips VALUES ('t4','tenant-2','draft',?)`, now)
	exec(`INSERT INTO vehicles VALUES ('v1','tenant-1','available'),('v2','tenant-1','available'),('v3','tenant-1','running'),('v4','tenant-2','maintenance')`)
	exec(`INSERT INTO drivers VALUES ('d1','tenant-1','available'),('d2','tenant-1','on_trip'),('d3','tenant-1','inactive'),('d4','tenant-1','leave'),('d5','tenant-2','available')`)
	exec(`INSERT INTO payments VALUES ('p1','tenant-1',100,?),('p2','tenant-1',200,?),('p3','tenant-2',900,?)`, now, now, now)
	exec(`INSERT INTO invoices VALUES ('i1','tenant-1','paid',1000,1000),('i2','tenant-1','pending',500,0),('i3','tenant-1','partially_paid',400,100),('i4','tenant-2','pending',700,0)`)

	svc := New(db)

	bookings := svc.Bookings(ctx, "tenant-1")
	require.Len(t, bookings, 4)
	assert.Equal(t, "4", bookings[0].Value, "total excludes tenant-2")
	assert.Equal(t, "1", bookings[1].Value)
	assert.Equal(t, "2", bookings[2].Value)
	assert.Equal(t, "1", bookings[3].Value)
	assert.Contains(t, bookings[0].Sub, "3 new this month")
	assert.Contains(t, bookings[0].Sub, "2.5k")

	assert.Equal(t, "1", svc.Bookings(ctx, "tenant-2")[0].Value, "tenant-2 sees only its own booking")

	trips := svc.Trips(ctx, "tenant-1")
	require.Len(t, trips, 4)
	assert.Equal(t, "3", trips[0].Value)
	assert.Equal(t, "0", trips[1].Value, "draft")
	assert.Equal(t, "2", trips[2].Value, "active = assigned + in_transit")
	assert.Equal(t, "1", trips[3].Value, "completed")

	vehicles := svc.Vehicles(ctx, "tenant-1")
	require.Len(t, vehicles, 4)
	assert.Equal(t, "3", vehicles[0].Value)
	assert.Equal(t, "1", vehicles[1].Value)
	assert.Equal(t, "2", vehicles[2].Value)
	assert.Equal(t, "0", vehicles[3].Value)

	drivers := svc.Drivers(ctx, "tenant-1")
	require.Len(t, drivers, 4)
	assert.Equal(t, "4", drivers[0].Value)
	assert.Equal(t, "2", drivers[3].Value, "inactive + leave")

	payments := svc.Payments(ctx, "tenant-1")
	require.Len(t, payments, 2)
	assert.Equal(t, "₹300", payments[0].Value)
	assert.Equal(t, "2 payments", payments[0].Sub)
	assert.Equal(t, "₹300", payments[1].Value)

	invoices := svc.Invoices(ctx, "tenant-1")
	require.Len(t, invoices, 3)
	assert.Equal(t, "3", invoices[0].Value)
	assert.Equal(t, "₹1.9k billed", invoices[0].Sub)
	assert.Equal(t, "1", invoices[1].Value)
	assert.Equal(t, "₹800", invoices[2].Value, "outstanding = 500 + (400-100)")
	assert.Equal(t, "2 unpaid", invoices[2].Sub)
}

func TestKPIReadModels_NilDBIsSafe(t *testing.T) {
	svc := New(nil)
	ctx := context.Background()
	bookings := svc.Bookings(ctx, "tenant-1")
	require.NotEmpty(t, bookings)
	assert.Equal(t, "0", bookings[0].Value, "nil DB renders zero cards, never panics")
	assert.NotEmpty(t, svc.Trips(ctx, "tenant-1"))
	assert.NotEmpty(t, svc.Payments(ctx, "tenant-1"))
}
