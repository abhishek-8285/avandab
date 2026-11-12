package ewaybill_test

// Tests for the E-Way Bill presentation read model after the 2026-11-11
// security fix: every read is tenant-scoped through the owning trip.
//
// Covered contract: tenant isolation across Stats/Items/CandidateTrips/Events,
// fail-closed empty tenant (no global fallback, no DefaultTenant), orphan EWBs
// (trip deleted/unknown) invisible to every tenant, ordering/limit/coalescing
// and best-effort degradation.

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

// seedEWBReadFixture seeds two tenants (A/B) with one EWB each plus an orphan
// EWB whose trip no longer exists.
func seedEWBReadFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExecEWBRead(t, db, `INSERT INTO trips (id, trip_number, tenant_id) VALUES
		('t-a', 'TRP-A', 'tenant-a'), ('t-b', 'TRP-B', 'tenant-b')`)
	mustExecEWBRead(t, db, `INSERT INTO eway_bills (id, trip_id, ewb_number, vehicle_number, from_place, to_place, goods_value, status, valid_until, extension_count, created_at) VALUES
		('eb-a', 't-a', 'EWB-A', 'MH12A', 'Mumbai', 'Pune', 60000, 'active', '2026-12-31 00:00:00', 0, '2026-09-01 10:00:00'),
		('eb-b', 't-b', 'EWB-B', 'KA01B', 'Delhi', 'Jaipur', 75000, 'cancelled', '2026-12-31 00:00:00', 1, '2026-09-02 10:00:00'),
		('eb-orphan', NULL, 'EWB-ORPHAN', NULL, NULL, NULL, 10, 'active', '2026-12-31 00:00:00', 0, '2026-09-03 10:00:00')`)
	mustExecEWBRead(t, db, `INSERT INTO eway_bill_events (id, ewb_number, trip_id, event_type, payload, created_by, created_at) VALUES
		('ev-a', 'EWB-A', 't-a', 'PART_A_GENERATED', '{"ok":true}', 'u-a', '2026-09-01 11:00:00'),
		('ev-b', 'EWB-B', 't-b', 'CANCELLED', NULL, NULL, '2026-09-02 11:00:00'),
		('ev-orphan', 'EWB-ORPHAN', NULL, 'PART_A_GENERATED', NULL, NULL, '2026-09-03 11:00:00')`)
}

func TestEWayBillReadQueries_StatsTenantScoped(t *testing.T) {
	db := newEWBReadTestDB(t)
	ctx := context.Background()
	seedEWBReadFixture(t, db)
	reads := ewaybill.NewEWayBillReadQueries(db)

	statsA, err := reads.Stats(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, 1, statsA.Total, "tenant A sees only its own EWB")
	assert.Equal(t, 1, statsA.Active)
	assert.Zero(t, statsA.Cancelled)

	statsB, err := reads.Stats(ctx, "tenant-b")
	require.NoError(t, err)
	assert.Equal(t, 1, statsB.Total)
	assert.Equal(t, 1, statsB.Cancelled)
	assert.Zero(t, statsB.Active)

	// Fail closed: empty tenant and unknown tenant see nothing (no global
	// fallback, no DefaultTenant substitution).
	for _, tenant := range []string{"", "tenant-unknown"} {
		stats, err := reads.Stats(ctx, tenant)
		require.NoError(t, err)
		assert.Zero(t, stats.Total, "tenant %q must fail closed", tenant)
	}

	// Failure path still surfaces the query error for logging.
	mustExecEWBRead(t, db, `DROP TABLE eway_bills`)
	_, err = reads.Stats(ctx, "tenant-a")
	require.Error(t, err)
}

func TestEWayBillReadQueries_ItemsTenantScoped(t *testing.T) {
	db := newEWBReadTestDB(t)
	ctx := context.Background()
	seedEWBReadFixture(t, db)
	reads := ewaybill.NewEWayBillReadQueries(db)

	itemsA := reads.Items(ctx, "tenant-a")
	require.Len(t, itemsA, 1)
	assert.Equal(t, "eb-a", itemsA[0].ID)
	assert.Equal(t, "EWB-A", itemsA[0].EwbNumber)
	assert.Equal(t, "TRP-A", itemsA[0].TripNumber)
	assert.Equal(t, "Mumbai", itemsA[0].FromPlace)
	assert.Equal(t, 60000.0, itemsA[0].GoodsValue)

	itemsB := reads.Items(ctx, "tenant-b")
	require.Len(t, itemsB, 1)
	assert.Equal(t, "EWB-B", itemsB[0].EwbNumber)
	assert.Equal(t, "KA01B", itemsB[0].VehicleNumber)

	// Orphan EWB (trip_id NULL) and empty/unknown tenants are invisible.
	assert.Empty(t, reads.Items(ctx, ""))
	assert.Empty(t, reads.Items(ctx, "tenant-unknown"))
	for _, it := range append(itemsA, itemsB...) {
		assert.NotEqual(t, "EWB-ORPHAN", it.EwbNumber, "orphan EWB must never surface")
	}

	// Ordering/limit within the tenant: seed 105 more tenant-a EWBs.
	for i := 0; i < 105; i++ {
		mustExecEWBRead(t, db, `INSERT INTO eway_bills (id, trip_id, ewb_number, status, valid_until, goods_value, created_at) VALUES (?, 't-a', ?, 'active', '2026-12-31 00:00:00', 0, ?)`,
			fmt.Sprintf("bulk-%03d", i), fmt.Sprintf("EWB-BULK-%03d", i), fmt.Sprintf("2026-01-01 00:%02d:%02d", i/60, i%60))
	}
	itemsA = reads.Items(ctx, "tenant-a")
	require.Len(t, itemsA, 100, "tenant-scoped cap is still 100")
	assert.Equal(t, "EWB-A", itemsA[0].EwbNumber, "newest created_at first (seeded 2026-09-01)")
	for _, it := range itemsA {
		assert.NotEqual(t, "EWB-B", it.EwbNumber)
		assert.NotEqual(t, "EWB-ORPHAN", it.EwbNumber)
	}
}

func TestEWayBillReadQueries_CandidateTripsTenantScoped(t *testing.T) {
	db := newEWBReadTestDB(t)
	ctx := context.Background()
	mustExecEWBRead(t, db, `INSERT INTO routes (id, source, destination) VALUES ('r-1', 'Mumbai', 'Pune')`)
	mustExecEWBRead(t, db, `INSERT INTO trips (id, trip_number, route_id, status, tenant_id, created_at) VALUES
		('t-a1', 'TRP-A1', 'r-1', 'scheduled', 'tenant-a', '2026-01-01 00:00:00'),
		('t-a2', 'TRP-A2', 'r-1', 'in_transit', 'tenant-a', '2026-01-02 00:00:00'),
		('t-b1', 'TRP-B1', 'r-1', 'started', 'tenant-b', '2026-01-03 00:00:00'),
		('t-a-done', 'TRP-ADONE', 'r-1', 'completed', 'tenant-a', '2026-01-04 00:00:00')`)

	reads := ewaybill.NewEWayBillReadQueries(db)
	tripsA := reads.CandidateTrips(ctx, "tenant-a")
	require.Len(t, tripsA, 2, "only tenant-a's eligible trips")
	assert.Equal(t, "t-a2", tripsA[0].ID, "newest first")
	assert.Equal(t, "t-a1", tripsA[1].ID)
	assert.Equal(t, "Mumbai", tripsA[0].Source)

	tripsB := reads.CandidateTrips(ctx, "tenant-b")
	require.Len(t, tripsB, 1)
	assert.Equal(t, "t-b1", tripsB[0].ID)

	assert.Empty(t, reads.CandidateTrips(ctx, ""))
	assert.Empty(t, reads.CandidateTrips(ctx, "tenant-unknown"))
}

func TestEWayBillReadQueries_EventsTenantScoped(t *testing.T) {
	db := newEWBReadTestDB(t)
	ctx := context.Background()
	seedEWBReadFixture(t, db)
	reads := ewaybill.NewEWayBillReadQueries(db)

	eventsA := reads.Events(ctx, "EWB-A", "tenant-a")
	require.Len(t, eventsA, 1)
	assert.Equal(t, "ev-a", eventsA[0].ID)
	assert.Equal(t, "PART_A_GENERATED", eventsA[0].EventType)
	assert.Equal(t, `{"ok":true}`, eventsA[0].Payload)
	assert.Equal(t, "u-a", eventsA[0].CreatedBy)

	// Cross-tenant: A cannot read B's event history, and B cannot read A's.
	assert.Empty(t, reads.Events(ctx, "EWB-B", "tenant-a"))
	assert.Empty(t, reads.Events(ctx, "EWB-A", "tenant-b"))

	// B's own history is visible with NULL payload/actor defaults.
	eventsB := reads.Events(ctx, "EWB-B", "tenant-b")
	require.Len(t, eventsB, 1)
	assert.Empty(t, eventsB[0].Payload)
	assert.Equal(t, "system", eventsB[0].CreatedBy)

	// Orphan EWB events are invisible to every tenant.
	assert.Empty(t, reads.Events(ctx, "EWB-ORPHAN", "tenant-a"))
	assert.Empty(t, reads.Events(ctx, "EWB-ORPHAN", "tenant-b"))

	// Fail closed on empty tenant.
	assert.Empty(t, reads.Events(ctx, "EWB-A", ""))
}

func TestEWayBillReadQueries_NilDBAndDegradation(t *testing.T) {
	ctx := context.Background()
	reads := ewaybill.NewEWayBillReadQueries(nil)
	stats, err := reads.Stats(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Zero(t, stats.Total)
	assert.Nil(t, reads.Items(ctx, "tenant-a"))
	assert.Nil(t, reads.CandidateTrips(ctx, "tenant-a"))
	assert.Nil(t, reads.Events(ctx, "EWB-1", "tenant-a"))

	// Missing-table errors degrade to empty sections (the EWB page renders).
	db := newEWBReadTestDB(t)
	mustExecEWBRead(t, db, `DROP TABLE eway_bills`)
	mustExecEWBRead(t, db, `DROP TABLE trips`)
	mustExecEWBRead(t, db, `DROP TABLE eway_bill_events`)
	live := ewaybill.NewEWayBillReadQueries(db)
	assert.Nil(t, live.Items(ctx, "tenant-a"))
	assert.Nil(t, live.CandidateTrips(ctx, "tenant-a"))
	assert.Nil(t, live.Events(ctx, "EWB-1", "tenant-a"))
}
