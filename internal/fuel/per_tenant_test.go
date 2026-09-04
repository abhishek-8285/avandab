package fuel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// One org disabling fuel_audit stops only its own vehicles: the other's
// anomalies are still detected, and unknown vehicles are never processed
// as another org.
func TestEngine_PerTenantSweep(t *testing.T) {
	db := newTestDB(t)
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-A','A','a'),('tenant-B','B','b')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type,
		 insurance_expiry, fitness_expiry, permit_expiry, status, tank_capacity_litres, fuel_sensor_fitted, tenant_id)
		VALUES ('va','RA','RA','truck',2000,'diesel','2027-01-01','2027-01-01','2027-01-01','available',100,1,'tenant-A'),
		       ('vb','RB','RB','truck',2000,'diesel','2027-01-01','2027-01-01','2027-01-01','available',100,1,'tenant-B')`)
	require.NoError(t, err)

	t0 := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	mkSnaps := func(id, vid string) []snapshotRow {
		return []snapshotRow{
			{id: id + "-1", vehicleID: vid, ts: t0, speed: 0, fuelLevel: 40, odometer: 1000},
			{id: id + "-2", vehicleID: vid, ts: t0.Add(1 * time.Second), speed: 0, fuelLevel: 65, odometer: 1000},
			{id: id + "-3", vehicleID: vid, ts: t0.Add(2 * time.Second), speed: 0, fuelLevel: 65, odometer: 1000},
		}
	}
	snaps := append(mkSnaps("a", "va"), mkSnaps("b", "vb")...)
	snaps = append(snaps, snapshotRow{id: "g-1", vehicleID: "ghost", ts: t0, speed: 0, fuelLevel: 10, odometer: 5})
	insertSnapshots(t, db, snaps)

	e := buildEngine(t, db, t0)
	e.WithFeatureGate(func(tenantID string) bool { return tenantID == "tenant-A" })

	handled, err := e.Tick(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, handled, "only tenant-A snapshots processed")

	var aCount, bCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM fuel_events WHERE vehicle_id = 'va'`).Scan(&aCount))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM fuel_events WHERE vehicle_id = 'vb'`).Scan(&bCount))
	assert.Equal(t, 1, aCount, "tenant-A refill detected")
	assert.Equal(t, 0, bCount, "gated-off tenant-B untouched")
}
