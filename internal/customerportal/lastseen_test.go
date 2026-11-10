package customerportal

// Characterization + extraction tests for the shipper "last seen" telemetry
// fix moved out of internal/handlers/customer_portal.go (handlers-end-state
// ADR, Phase 2). The ordering must use ts_unix (epoch), never the mixed-format
// `timestamp` text column.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLastSeenFix_OrdersByEpochNotTimestampText(t *testing.T) {
	db := newPortalTestDB(t)
	mustExecPortal(t, db, `INSERT INTO telemetry_snapshots (id, vehicle_id, timestamp, latitude, longitude, speed, ts_unix) VALUES
		('fx-old-rfc3339', 'veh-lastseen', '2026-06-01T10:00:00Z', 11.0, 75.0, 10, CAST(strftime('%s', '2026-06-01 10:00:00') AS INTEGER)),
		('fx-new-gstring', 'veh-lastseen', '2026-06-01 15:00:00 +0000 UTC', 12.0, 76.0, 20, CAST(strftime('%s', '2026-06-01 15:00:00') AS INTEGER))`)

	lat, lng, ts, err := LastSeenFix(context.Background(), db, "veh-lastseen")
	require.NoError(t, err)
	require.NotNil(t, lat)
	require.NotNil(t, lng)
	assert.InDelta(t, 12.0, *lat, 1e-9, "must return the newest epoch fix, not the byte-sort winner")
	assert.InDelta(t, 76.0, *lng, 1e-9)
	assert.Equal(t, "2026-06-01T15:00:00Z", ts)
}

func TestLastSeenFix_NoRowsIsNotAnError(t *testing.T) {
	db := newPortalTestDB(t)
	lat, lng, ts, err := LastSeenFix(context.Background(), db, "veh-never-reported")
	require.NoError(t, err)
	assert.Nil(t, lat)
	assert.Nil(t, lng)
	assert.Empty(t, ts)
}

func TestLastSeenFix_SurfacesQueryFailure(t *testing.T) {
	db := newPortalTestDB(t)
	mustExecPortal(t, db, `DROP TABLE telemetry_snapshots`)
	_, _, _, err := LastSeenFix(context.Background(), db, "veh-lastseen")
	require.Error(t, err, "a failing query must not be reported as an empty result")
}

func TestLastSeenFix_NullCoordinatesAndNilDB(t *testing.T) {
	db := newPortalTestDB(t)
	// A row with NULL coordinates is excluded by the legacy predicate.
	mustExecPortal(t, db, `INSERT INTO telemetry_snapshots (id, vehicle_id, timestamp, latitude, longitude, ts_unix) VALUES
		('null-fix', 'veh-null', '2026-06-01 10:00:00', NULL, NULL, 100)`)
	lat, lng, ts, err := LastSeenFix(context.Background(), db, "veh-null")
	require.NoError(t, err)
	assert.Nil(t, lat)
	assert.Nil(t, lng)
	assert.Empty(t, ts)

	lat, lng, ts, err = LastSeenFix(context.Background(), nil, "veh")
	require.NoError(t, err)
	assert.Nil(t, lat)
	assert.Nil(t, lng)
	assert.Empty(t, ts)
}

func TestParseTimeFlex_Formats(t *testing.T) {
	assert.True(t, ParseTimeFlex("2026-06-01T10:00:00Z").Equal(ParseTimeFlex("2026-06-01 10:00:00")))
	assert.True(t, ParseTimeFlex("not-a-timestamp").IsZero())
	assert.True(t, ParseTimeFlex("").IsZero())
}
