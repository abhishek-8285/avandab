package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shipper's "last seen" panel must show the NEWEST fix. telemetry_snapshots
// `timestamp` TEXT mixes RFC3339 and Go's time.String() layout, so ordering on
// it sorts bytes — 'T' (0x54) > ' ' (0x20) puts the OLDER RFC3339 row on top.
// Ratchet: fails on `ORDER BY timestamp DESC`, passes on `ORDER BY ts_unix DESC`.
func TestLastSeenFix_OrdersByEpochNotTimestampText(t *testing.T) {
	app, _ := setupZMOTMReportsTestApp(t)

	_, err := app.DB.Exec(`
		INSERT INTO telemetry_snapshots (id, vehicle_id, timestamp, latitude, longitude, speed, ts_unix)
		VALUES
		  ('fx-old-rfc3339', 'veh-lastseen', '2026-06-01T10:00:00Z',           11.0, 75.0, 10,
		   CAST(strftime('%s', '2026-06-01 10:00:00') AS INTEGER)),
		  ('fx-new-gstring', 'veh-lastseen', '2026-06-01 15:00:00 +0000 UTC',  12.0, 76.0, 20,
		   CAST(strftime('%s', '2026-06-01 15:00:00') AS INTEGER))`)
	require.NoError(t, err)

	lat, lng, ts, err := lastSeenFix(context.Background(), app.DB, "veh-lastseen")
	require.NoError(t, err)
	require.NotNil(t, lat, "expected a fix")
	require.NotNil(t, lng)
	assert.InDelta(t, 12.0, *lat, 1e-9, "must return the 15:00 fix, not the row that wins a byte sort")
	assert.InDelta(t, 76.0, *lng, 1e-9)
	assert.NotEmpty(t, ts)
}

// A vehicle that never reported is not an error — the portal renders "no data".
func TestLastSeenFix_NoRowsIsNotAnError(t *testing.T) {
	app, _ := setupZMOTMReportsTestApp(t)

	lat, lng, ts, err := lastSeenFix(context.Background(), app.DB, "veh-never-reported")
	require.NoError(t, err)
	assert.Nil(t, lat)
	assert.Nil(t, lng)
	assert.Empty(t, ts)
}

// A dead table must surface to the caller so the failure is logged instead of
// rendering as "no tracking data yet". Ratchet: fails if the error is swallowed.
func TestLastSeenFix_SurfacesQueryFailure(t *testing.T) {
	app, _ := setupZMOTMReportsTestApp(t)

	_, err := app.DB.Exec(`DROP TABLE telemetry_snapshots`)
	require.NoError(t, err)

	_, _, _, err = lastSeenFix(context.Background(), app.DB, "veh-lastseen")
	require.Error(t, err, "a failing query must not be reported as an empty result")
}
