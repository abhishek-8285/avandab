package safety

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Gated-off orgs produce no safety events; gated-on orgs detect normally.
// Unknown vehicles are skipped instead of processed as another org.
func TestTick_PerTenantSweep(t *testing.T) {
	db := newTestDB(t)
	seedFixtures(t, db)
	_, err := db.Exec(`INSERT INTO drivers
		(id, driver_id, tenant_id, first_name, last_name, phone, license_number, license_expiry, status)
		VALUES ('d2', 'D-002', 'tenant-2', 'Balu', 'B', '9988776656', 'KA-54321', '2028-01-01', 'available')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id)
		VALUES ('v2', 'KA02CD5678', 'KA-02-CD-5678', 'truck', 5000, 'diesel', '2030-01-01', '2030-01-01', '2030-01-01', 'available', 'tenant-2')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips
		(id, trip_number, route_id, departure_time, status, driver_id, vehicle_id, tenant_id)
		VALUES ('t2', 'TRIP-002', 'r1', '2026-08-19 09:00:00', 'in_transit', 'd2', 'v2', 'tenant-2')`)
	require.NoError(t, err)

	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	speeding := func(vid, trip, did string, base time.Time) {
		for i, sp := range []float64{60, 95, 97, 60} {
			_, err := db.Exec(`INSERT INTO telemetry_snapshots
				(id, vehicle_id, trip_id, timestamp, speed, latitude, longitude, driver_id)
				VALUES (?, ?, ?, ?, ?, 12.97, 77.59, ?)`,
				fmt.Sprintf("s-%s-%d-%d", vid, base.Unix(), i), vid, trip, timeStr(base.Add(time.Duration(i*30)*time.Second)), sp, did)
			require.NoError(t, err)
		}
	}
	// Warm-up phase for both vehicles.
	speeding("v1", "t1", "d1", t0)
	speeding("v2", "t2", "d2", t0)

	e, _ := buildEngine(t, db, t0)
	e.WithFeatureGate(func(tenantID string) bool { return tenantID == "tenant-1" })

	_, err = e.Tick(context.Background())
	require.NoError(t, err)

	// Detection phase: only tenant-1's new frames must produce events.
	speeding("v1", "t1", "d1", t0.Add(5*time.Minute))
	speeding("v2", "t2", "d2", t0.Add(5*time.Minute))

	handled, err := e.Tick(context.Background())
	require.NoError(t, err)
	assert.Greater(t, handled, 0)

	var v1Count, v2Count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_behaviour_events WHERE vehicle_id = 'v1'`).Scan(&v1Count))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_behaviour_events WHERE vehicle_id = 'v2'`).Scan(&v2Count))
	assert.Equal(t, 1, v1Count, "gated-on org detects")
	assert.Equal(t, 0, v2Count, "gated-off org untouched")
}
