package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"transport-app/internal/shared"
)

// TestLoadFuelRollup_Math proves per-trip rollup: playback distance over
// fuel in (refills + claims), plus speed extremes and anomaly counts.
func TestLoadFuelRollup_Math(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestRoute(t, db, "r1")
	insertTestVehicleReg(t, db, "v1", "REG-1")
	_, err := db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id, started_at)
		VALUES ('t1', 'TRIP-t1', 'r1', datetime('now'), 'in_transit', '1', datetime('now','-1 hour'))`)
	require.NoError(t, err)

	now := time.Now().UTC()
	// ~11.1 km apart (0.1° latitude), 30 min window, max speed 60.
	insertLiveSnapshotWithTrip(t, db, "s1", "t1", "v1", now.Add(-30*time.Minute), 40.0, 19.0, 72.8)
	insertLiveSnapshotWithTrip(t, db, "s2", "t1", "v1", now, 60.0, 19.1, 72.8)

	_, err = db.Exec(`INSERT INTO fuel_events (id, vehicle_id, trip_id, event_type, estimated_litres, occurred_at)
		VALUES ('fe1', 'v1', 't1', 'refill_detected', 20, datetime('now'))`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO fuel_events (id, vehicle_id, trip_id, event_type, estimated_litres, occurred_at)
		VALUES ('fe2', 'v1', 't1', 'abnormal_drain', 2, datetime('now'))`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO fuel_claim_audits (id, expense_id, trip_id, vehicle_id, litres_claimed)
		VALUES ('ca1', 'exp1', 't1', 'v1', 10)`)
	require.NoError(t, err)

	ctx := shared.ContextWithTenantID(context.Background(), "1")
	roll, err := LoadFuelRollup(ctx, db, "t1")
	require.NoError(t, err)
	require.InDelta(t, 11.1, roll.DistanceKM, 0.5)
	require.Equal(t, int64(1800), roll.DurationSeconds)
	require.InDelta(t, 60.0, roll.MaxSpeedKMH, 0.001)
	require.InDelta(t, 30.0, roll.FuelLitres, 0.001) // 20 refill + 10 claim; drain excluded
	require.NotNil(t, roll.KMPL)
	require.InDelta(t, 11.1/30.0, *roll.KMPL, 0.05)
	require.Equal(t, 1, roll.RefillCount)
	require.Equal(t, 1, roll.ClaimCount)
	require.Equal(t, 1, roll.AnomalyCount)
	require.NotNil(t, roll.StartedAt)
}

// TestLoadFuelRollup_TenantIsolation proves cross-tenant trips stay invisible.
func TestLoadFuelRollup_TenantIsolation(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestRoute(t, db, "r1")
	_, err := db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id)
		VALUES ('t9', 'TRIP-t9', 'r1', datetime('now'), 'in_transit', '2')`)
	require.NoError(t, err)

	ctx := shared.ContextWithTenantID(context.Background(), "1")
	_, err = LoadFuelRollup(ctx, db, "t9")
	require.Error(t, err)
}

// TestLoadFuelRollup_NoFuelNilKMPL proves KMPL stays nil (not zero/div-zero)
// when a trip burned distance with no recorded fuel.
func TestLoadFuelRollup_NoFuelNilKMPL(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestRoute(t, db, "r1")
	insertTestVehicleReg(t, db, "v1", "REG-1")
	_, err := db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id)
		VALUES ('t2', 'TRIP-t2', 'r1', datetime('now'), 'in_transit', '1')`)
	require.NoError(t, err)
	now := time.Now().UTC()
	insertLiveSnapshotWithTrip(t, db, "s1", "t2", "v1", now.Add(-10*time.Minute), 50.0, 19.0, 72.8)
	insertLiveSnapshotWithTrip(t, db, "s2", "t2", "v1", now, 50.0, 19.05, 72.8)

	ctx := shared.ContextWithTenantID(context.Background(), "1")
	roll, err := LoadFuelRollup(ctx, db, "t2")
	require.NoError(t, err)
	require.Greater(t, roll.DistanceKM, 0.0)
	require.Equal(t, 0.0, roll.FuelLitres)
	require.Nil(t, roll.KMPL)
}
