package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"transport-app/internal/shared"
)

// A long trip has more than 5000 telemetry points. LoadFuelRollup used a
// single history fetch (limit 5000, ordered oldest-first), so it computed
// distance/duration/KMPL from the earliest slice only and silently dropped
// the rest of the trip.
func TestLoadFuelRollup_LongTrip_CoversAllPoints(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestRoute(t, db, "r1")
	insertTestVehicleReg(t, db, "v1", "REG-LONG")
	_, err := db.Exec(`INSERT INTO trips
		(id, trip_number, route_id, departure_time, status, tenant_id, started_at, completed_at)
		VALUES ('t-long', 'TRIP-long', 'r1', '2026-01-01 00:00:00', 'completed', '1',
		        '2026-01-01 00:00:00', '2026-01-01 03:00:00')`)
	require.NoError(t, err)

	// 6000 positions spread over 3h; odometer climbs 1000 -> 7000 (1 km each).
	// Constant lat/lng makes haversine 0, so computeSummary uses odometer delta.
	_, err = db.Exec(`
		INSERT INTO telemetry_positions
			(id, tenant_id, imei, device_time, latitude, longitude, speed, odometer, trip_id, vehicle_id)
		WITH RECURSIVE seq(i) AS (SELECT 0 UNION ALL SELECT i + 1 FROM seq WHERE i < 5999)
		SELECT 'p-' || i, '1', 'IMEI-LONG',
		       datetime('2026-01-01 00:00:00', '+' || (i * 10800.0 / 5999) || ' seconds'),
		       19.0, 72.8, 50.0, 1000.0 + i, 't-long', 'v1'
		FROM seq`)
	require.NoError(t, err)

	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE trip_id = 't-long'`).Scan(&n))
	require.Equal(t, 6000, n)

	ctx := shared.ContextWithTenantID(context.Background(), "1")
	roll, err := LoadFuelRollup(ctx, db, "t-long")
	require.NoError(t, err)
	require.Equal(t, 6000, roll.Points, "rollup must cover all points, not only the oldest 5000")
	require.InDelta(t, 6000.0, roll.DistanceKM, 1.0, "distance must span the full odometer delta")
	require.InDelta(t, 10800.0, float64(roll.DurationSeconds), 2.0, "duration must span the full 3h")
}
