package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
)

const attentionTestSchema = `
CREATE TABLE bookings (id TEXT PRIMARY KEY, status TEXT NOT NULL, tenant_id TEXT NOT NULL);
CREATE TABLE trips (id TEXT PRIMARY KEY, booking_id TEXT, tenant_id TEXT NOT NULL);
CREATE TABLE vehicles (id TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT 'available', maintenance_due DATE, tenant_id TEXT NOT NULL);
CREATE TABLE work_orders (id TEXT PRIMARY KEY, status TEXT NOT NULL, tenant_id TEXT NOT NULL);
CREATE TABLE alerts (id TEXT PRIMARY KEY, status TEXT NOT NULL, tenant_id TEXT NOT NULL);
CREATE TABLE dtc_events (id TEXT PRIMARY KEY, vehicle_id TEXT NOT NULL, resolved_at DATETIME);
CREATE TABLE eway_bills (id TEXT PRIMARY KEY, trip_id TEXT, ewb_number TEXT NOT NULL, status TEXT NOT NULL, valid_until DATETIME NOT NULL);
CREATE TABLE driver_expenses (id TEXT PRIMARY KEY, status TEXT, tenant_id TEXT NOT NULL);
CREATE TABLE fastag_tags (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, tag_id TEXT UNIQUE NOT NULL, balance REAL NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'ACTIVE');
`

// TestAttentionCounts proves every exception-strip count: positives counted,
// terminal/irrelevant rows excluded, other tenants excluded.
func TestAttentionCounts(t *testing.T) {
	dbConn, err := sql.Open("sqlite", "file:attention_counts?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })
	_, err = dbConn.Exec(attentionTestSchema)
	require.NoError(t, err)

	exec := func(q string, args ...any) {
		t.Helper()
		_, err := dbConn.Exec(q, args...)
		require.NoError(t, err)
	}

	// Unassigned bookings: pending w/o trip counts; confirmed w/ trip,
	// completed, and other-tenant rows do not.
	exec(`INSERT INTO bookings VALUES ('b1','pending','1'),('b2','confirmed','1'),('b3','completed','1'),('b4','pending','2')`)
	exec(`INSERT INTO trips (id, booking_id, tenant_id) VALUES ('t0','b2','1')`)

	// Maintenance: one due, one clean, one other-tenant due, one garage unit.
	exec(`INSERT INTO vehicles VALUES ('v1','available','2026-08-01','1'),('v2','available',NULL,'1'),('v3','available','2026-08-01','2'),('v4','maintenance',NULL,'1')`)

	// Work orders: open/on_hold count; done/cancelled/other-tenant do not.
	exec(`INSERT INTO work_orders VALUES ('w1','open','1'),('w2','on_hold','1'),('w3','done','1'),('w4','cancelled','1'),('w5','open','2')`)

	// Alerts: open + escalated count; acknowledged/resolved/other-tenant do not.
	exec(`INSERT INTO alerts VALUES ('a1','open','1'),('a2','escalated','1'),('a3','acknowledged','1'),('a4','resolved','1'),('a5','open','2')`)

	// DTCs: unresolved on own vehicle counts; resolved or foreign-vehicle do not.
	exec(`INSERT INTO dtc_events VALUES ('d1','v1',NULL),('d2','v1','2026-08-01 00:00:00'),('d3','v3',NULL)`)

	// EWBs: active expiring within 8h counts; far-future, expired-status,
	// and foreign-trip rows do not.
	soon := time.Now().Add(2 * time.Hour).Format("2006-01-02 15:04:05")
	far := time.Now().Add(72 * time.Hour).Format("2006-01-02 15:04:05")
	exec(`INSERT INTO trips (id, booking_id, tenant_id) VALUES ('te1','b2','1'),('te2','b4','2')`)
	exec(`INSERT INTO eway_bills VALUES ('e1','te1','EWB1','active',?), ('e2','te1','EWB2','active',?), ('e3','te1','EWB3','expired',?), ('e4','te2','EWB4','active',?)`, soon, far, soon, soon)

	// Kharcha: pending (incl. NULL status) counts; approved/other-tenant do not.
	exec(`INSERT INTO driver_expenses VALUES ('k1','pending','1'),('k2',NULL,'1'),('k3','approved','1'),('k4','pending','2')`)

	// FASTag: active below threshold counts; rich/blocked/foreign do not.
	exec(`INSERT INTO fastag_tags VALUES ('f1','1','TAG1',100,'ACTIVE'),('f2','1','TAG2',5000,'ACTIVE'),('f3','1','TAG3',50,'BLOCKED'),('f4','2','TAG4',10,'ACTIVE')`)

	repo := NewRepository(dbConn)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	cases := []struct {
		name string
		got  func(context.Context) (int64, error)
		want int64
	}{
		{"unassigned", repo.CountUnassignedBookings, 1},
		{"due", repo.CountMaintenanceDue, 1},
		{"openWO", repo.CountOpenWorkOrders, 2},
		{"garage", repo.CountGarageVehicles, 1},
		{"alerts", repo.CountOpenAlerts, 2},
		{"dtc", repo.CountActiveDTCs, 1},
		{"ewb", repo.CountExpiringEwaybills, 1},
		{"kharcha", repo.CountPendingKharcha, 2},
	}
	for _, tc := range cases {
		n, err := tc.got(ctx)
		require.NoError(t, err, tc.name)
		assert.EqualValues(t, tc.want, n, tc.name)
	}
	n, err := repo.CountLowFastag(ctx, 500.0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "low fastag")
}
