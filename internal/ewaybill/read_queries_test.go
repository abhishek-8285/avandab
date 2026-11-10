package ewaybill_test

// Characterization + extraction tests for the E-Way Bill presentation read
// model moved out of internal/handlers/ewaybill.go (handlers-end-state ADR,
// Phase 2): stats math, list ordering/limit/null-coalescing, candidate-trip
// eligibility/ordering and detail event history. The legacy global scope
// (no tenant filter on these presentation reads) is pinned explicitly so a
// future tenant-scoping change is deliberate; the EWB write lifecycle keeps
// its own tenant gate and is covered by TestGeneratePartA_CrossTenantDenied.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/ewaybill"
)

const ewbReadTestSchema = `
CREATE TABLE routes (
    id TEXT PRIMARY KEY,
    source TEXT,
    destination TEXT
);
CREATE TABLE trips (
    id TEXT PRIMARY KEY,
    trip_number TEXT,
    route_id TEXT,
    status TEXT,
    tenant_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE eway_bills (
    id TEXT PRIMARY KEY,
    trip_id TEXT,
    ewb_number TEXT NOT NULL,
    vehicle_number TEXT,
    from_place TEXT,
    to_place TEXT,
    goods_value REAL,
    status TEXT,
    valid_until DATETIME,
    extension_count INTEGER DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE eway_bill_events (
    id TEXT PRIMARY KEY,
    ewb_number TEXT NOT NULL,
    trip_id TEXT,
    event_type TEXT NOT NULL,
    payload TEXT,
    created_by TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

func newEWBReadTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("ewb_read_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(ewbReadTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExecEWBRead(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func TestEWayBillReadQueries_Stats(t *testing.T) {
	db := newEWBReadTestDB(t)
	ctx := context.Background()
	mustExecEWBRead(t, db, `INSERT INTO eway_bills (id, ewb_number, status, vehicle_number, extension_count) VALUES
		('b1', 'EWB-1', 'active', 'MH12AB1234', 0),
		('b2', 'EWB-2', 'active', NULL, 0),
		('b3', 'EWB-3', 'active', '', 2),
		('b4', 'EWB-4', 'cancelled', 'MH12AB1234', 0),
		('b5', 'EWB-5', 'expired', NULL, 0),
		('b6', 'EWB-6', 'part_a', NULL, 0)`)

	stats, err := ewaybill.NewEWayBillReadQueries(db).Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 6, stats.Total)
	assert.Equal(t, 1, stats.Active, "active WITH vehicle")
	assert.Equal(t, 2, stats.PartAOnly, "active with NULL or empty vehicle")
	assert.Equal(t, 1, stats.Extended)
	assert.Equal(t, 1, stats.Cancelled)
	assert.Equal(t, 1, stats.Expired)

	// A failed stats query is returned for logging; the handler still renders
	// zeroed counters instead of failing the page.
	mustExecEWBRead(t, db, `DROP TABLE eway_bills`)
	stats, err = ewaybill.NewEWayBillReadQueries(db).Stats(ctx)
	require.Error(t, err)
	assert.Zero(t, stats.Total)
}

func TestEWayBillReadQueries_Items(t *testing.T) {
	db := newEWBReadTestDB(t)
	ctx := context.Background()
	mustExecEWBRead(t, db, `INSERT INTO trips (id, trip_number, tenant_id) VALUES
		('t-1', 'TRP-001', 'tenant-1'), ('t-2', 'TRP-002', 'tenant-2')`)
	mustExecEWBRead(t, db, `INSERT INTO eway_bills (id, trip_id, ewb_number, vehicle_number, from_place, to_place, goods_value, status, valid_until, extension_count, created_at) VALUES
		('b-old', 't-1', 'EWB-OLD', 'MH12AB1234', 'Mumbai', 'Pune', 60000, 'active', '2026-09-02 10:00:00', 0, '2026-09-01 10:00:00'),
		('b-new', 't-2', 'EWB-NEW', NULL, NULL, NULL, 75000, 'cancelled', '2026-09-03 10:00:00', 1, '2026-09-02 10:00:00')`)

	items := ewaybill.NewEWayBillReadQueries(db).Items(ctx)
	require.Len(t, items, 2)
	assert.Equal(t, "b-new", items[0].ID, "newest created_at first")
	assert.Equal(t, "EWB-NEW", items[0].EwbNumber)
	assert.Equal(t, "TRP-002", items[0].TripNumber, "trip number comes from the LEFT JOIN")
	assert.Empty(t, items[0].VehicleNumber, "NULL vehicle/from/to coalesce to empty strings")
	assert.Empty(t, items[0].FromPlace)
	assert.Empty(t, items[0].ToPlace)
	assert.Equal(t, 75000.0, items[0].GoodsValue)
	assert.Equal(t, "cancelled", items[0].Status)
	assert.Equal(t, 1, items[0].ExtensionCount)
	assert.Equal(t, "b-old", items[1].ID)
	assert.Equal(t, "Mumbai", items[1].FromPlace)

	// FROZEN LEGACY SCOPE: the list has no tenant filter (eway_bills has no
	// tenant_id; the global presentation view is preserved verbatim).
	seen := map[string]bool{}
	for _, it := range items {
		seen[it.TripNumber] = true
	}
	assert.True(t, seen["TRP-001"] && seen["TRP-002"], "legacy global list shows every tenant's rows")

	// Legacy limit is 100 rows.
	mustExecEWBRead(t, db, `DELETE FROM eway_bills`)
	for i := 0; i < 105; i++ {
		mustExecEWBRead(t, db, `INSERT INTO eway_bills (id, trip_id, ewb_number, status, valid_until, goods_value, created_at) VALUES (?, 't-1', ?, 'active', '2026-12-31 00:00:00', 0, ?)`,
			fmt.Sprintf("bulk-%03d", i), fmt.Sprintf("EWB-BULK-%03d", i), fmt.Sprintf("2026-01-01 00:%02d:%02d", i/60, i%60))
	}
	items = ewaybill.NewEWayBillReadQueries(db).Items(ctx)
	assert.Len(t, items, 100)
}

func TestEWayBillReadQueries_CandidateTrips(t *testing.T) {
	db := newEWBReadTestDB(t)
	ctx := context.Background()
	mustExecEWBRead(t, db, `INSERT INTO routes (id, source, destination) VALUES ('r-1', 'Mumbai', 'Pune')`)
	mustExecEWBRead(t, db, `INSERT INTO trips (id, trip_number, route_id, status, tenant_id, created_at) VALUES
		('t-sched', 'TRP-SCHED', 'r-1', 'scheduled', 'tenant-1', '2026-01-01 00:00:00'),
		('t-assign', 'TRP-ASSIGN', 'r-1', 'assigned', 'tenant-2', '2026-01-02 00:00:00'),
		('t-start', 'TRP-START', 'r-1', 'started', 'tenant-1', '2026-01-03 00:00:00'),
		('t-pickup', 'TRP-PICKUP', 'r-1', 'reached_pickup', 'tenant-1', '2026-01-04 00:00:00'),
		('t-transit', 'TRP-TRANSIT', 'r-1', 'in_transit', 'tenant-1', '2026-01-05 00:00:00'),
		('t-done', 'TRP-DONE', 'r-1', 'completed', 'tenant-1', '2026-01-06 00:00:00'),
		('t-cancel', 'TRP-CANCEL', 'r-1', 'cancelled', 'tenant-1', '2026-01-07 00:00:00')`)

	trips := ewaybill.NewEWayBillReadQueries(db).CandidateTrips(ctx)
	require.Len(t, trips, 5, "only the five eligible statuses")
	assert.Equal(t, "t-transit", trips[0].ID, "newest created_at first")
	assert.Equal(t, "TRP-TRANSIT", trips[0].TripNumber)
	assert.Equal(t, "Mumbai", trips[0].Source)
	assert.Equal(t, "Pune", trips[0].Destination)
	assert.Equal(t, "t-sched", trips[4].ID)
	for _, tr := range trips {
		assert.NotEqual(t, "t-done", tr.ID)
		assert.NotEqual(t, "t-cancel", tr.ID)
	}

	// FROZEN LEGACY SCOPE: candidate trips are also listed globally (the
	// cross-tenant dropdown is preserved verbatim, flagged for follow-up).
	assert.Equal(t, "t-assign", trips[3].ID, "tenant-2 trip remains visible in the legacy global dropdown")
}

func TestEWayBillReadQueries_Events(t *testing.T) {
	db := newEWBReadTestDB(t)
	ctx := context.Background()
	mustExecEWBRead(t, db, `INSERT INTO eway_bill_events (id, ewb_number, trip_id, event_type, payload, created_by, created_at) VALUES
		('e2', 'EWB-1', 't-1', 'PART_B_ADDED', NULL, NULL, '2026-01-02 00:00:00'),
		('e1', 'EWB-1', 't-1', 'PART_A_GENERATED', '{"ok":true}', 'u-1', '2026-01-01 00:00:00'),
		('e3', 'EWB-1', NULL, 'EXTENDED', NULL, 'u-2', '2026-01-03 00:00:00'),
		('other', 'EWB-2', NULL, 'CANCELLED', NULL, NULL, '2026-01-04 00:00:00')`)

	events := ewaybill.NewEWayBillReadQueries(db).Events(ctx, "EWB-1")
	require.Len(t, events, 3)
	assert.Equal(t, "e1", events[0].ID, "chronological ASC")
	assert.Equal(t, "PART_A_GENERATED", events[0].EventType)
	assert.Equal(t, `{"ok":true}`, events[0].Payload)
	assert.Equal(t, "u-1", events[0].CreatedBy)
	assert.Equal(t, "e2", events[1].ID)
	assert.Empty(t, events[1].Payload, "NULL payload coalesces to empty")
	assert.Equal(t, "system", events[1].CreatedBy, "NULL actor defaults to system")
	assert.Equal(t, "e3", events[2].ID)
	assert.Empty(t, events[2].TripID, "NULL trip id coalesces to empty")
}

func TestEWayBillReadQueries_NilDBAndDegradation(t *testing.T) {
	ctx := context.Background()
	reads := ewaybill.NewEWayBillReadQueries(nil)
	stats, err := reads.Stats(ctx)
	require.NoError(t, err)
	assert.Zero(t, stats.Total)
	assert.Nil(t, reads.Items(ctx))
	assert.Nil(t, reads.CandidateTrips(ctx))
	assert.Nil(t, reads.Events(ctx, "EWB-1"))

	// Missing-table errors degrade to empty sections like the legacy handler
	// (the EWB page still renders).
	db := newEWBReadTestDB(t)
	mustExecEWBRead(t, db, `DROP TABLE eway_bills`)
	mustExecEWBRead(t, db, `DROP TABLE trips`)
	mustExecEWBRead(t, db, `DROP TABLE eway_bill_events`)
	live := ewaybill.NewEWayBillReadQueries(db)
	assert.Nil(t, live.Items(ctx))
	assert.Nil(t, live.CandidateTrips(ctx))
	assert.Nil(t, live.Events(ctx, "EWB-1"))
}
