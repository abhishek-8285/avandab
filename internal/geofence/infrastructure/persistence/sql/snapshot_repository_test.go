package sql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func newSnapTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_snap_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(db, "../../../../../db/migrations"))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// ” is the unattributed sentinel every reader filters out. LoadNewFixes must
// hand the dwell worker nil, not &"": zoneApplies and applyTransitions gate on
// tripID == nil, so a pointer to "" runs pickup/drop evaluation against a trip
// that does not exist and the auto-transitions silently never match.
func TestLoadNewFixes_EmptyTripIDIsNil(t *testing.T) {
	db := newSnapTestDB(t)
	_, err := db.Exec(`INSERT INTO telemetry_snapshots
		(id, vehicle_id, trip_id, timestamp, ts_unix, latitude, longitude, speed)
		VALUES ('s-empty', 'v1', '', '2026-09-01 10:00:00', 1788246000, 19.0, 72.0, 10)`)
	require.NoError(t, err)

	fixes, err := NewSnapshotRepository(db).LoadNewFixes(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, fixes, 1, "unattributed fix must still be delivered to the dwell engine")
	require.Nil(t, fixes[0].TripID, "empty trip_id must surface as nil, not a pointer to \"\"")
}

// A real trip id still arrives as a non-nil pointer, so the fix must not
// over-correct and break attribution.
func TestLoadNewFixes_PopulatedTripIDIsNotNil(t *testing.T) {
	db := newSnapTestDB(t)
	_, err := db.Exec(`INSERT INTO telemetry_snapshots
		(id, vehicle_id, trip_id, timestamp, ts_unix, latitude, longitude, speed)
		VALUES ('s-full', 'v1', 't-real', '2026-09-01 10:00:00', 1788246000, 19.0, 72.0, 10)`)
	require.NoError(t, err)

	fixes, err := NewSnapshotRepository(db).LoadNewFixes(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, fixes, 1)
	require.NotNil(t, fixes[0].TripID)
	require.Equal(t, "t-real", *fixes[0].TripID)
}

// An unbound frame stores vehicle_id NULL (FK columns never take the ”
// sentinel). The dwell engine is keyed by vehicle, so such a row is not a fix —
// but it used to abort the entire sweep on the NULL->string Scan error, so the
// worker logged "dwell worker sweep failed" every tick and no vehicle was ever
// evaluated again.
func TestLoadNewFixes_SkipsUnboundSnapshotsWithoutFailing(t *testing.T) {
	db := newSnapTestDB(t)
	repo := NewSnapshotRepository(db)

	_, err := db.Exec(`INSERT INTO telemetry_snapshots
		(id, vehicle_id, trip_id, timestamp, ts_unix, latitude, longitude, speed)
		VALUES ('snap-unbound', NULL, NULL, datetime('now'), unixepoch(), 19.07, 72.87, 30)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO telemetry_snapshots
		(id, vehicle_id, trip_id, timestamp, ts_unix, latitude, longitude, speed)
		VALUES ('snap-bound', 'veh-ok', NULL, datetime('now'), unixepoch(), 19.08, 72.88, 25)`)
	require.NoError(t, err)

	fixes, err := repo.LoadNewFixes(context.Background(), 50)

	require.NoError(t, err, "a NULL vehicle_id snapshot must not fail the whole sweep")
	require.Len(t, fixes, 1, "only the bound snapshot is a fix")
	assert.Equal(t, "veh-ok", fixes[0].VehicleID)
}
