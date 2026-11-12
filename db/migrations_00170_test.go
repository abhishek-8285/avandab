package db_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestMigration00170_TelemetryScaleIndexes_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00170_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 169))

	// Verify duplicate index exists prior to 170
	var dupCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_telemetry_snapshots_vehicle_time'`).Scan(&dupCount))
	assert.Equal(t, 1, dupCount, "duplicate index idx_telemetry_snapshots_vehicle_time must exist before v170")

	// Seed a vehicle and snapshot to verify backfill into vehicle_latest_position
	_, err = db.Exec(`INSERT INTO tenants (id, name) VALUES ('tenant-170', 'Tenant 170')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry)
		VALUES ('v-mig-170', 'tenant-170', 'MH-01-AB-1234', 'REG-170', 'truck', 20, date('now', '+1 year'), date('now', '+1 year'), date('now', '+1 year'))`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO telemetry_snapshots (id, vehicle_id, timestamp, latitude, longitude, speed, fuel_level, odometer)
		VALUES ('snap-170-old', 'v-mig-170', '2026-10-28 10:00:00', 19.01, 72.81, 40.0, 50.0, 1000.0)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO telemetry_snapshots (id, vehicle_id, timestamp, latitude, longitude, speed, fuel_level, odometer)
		VALUES ('snap-170-new', 'v-mig-170', '2026-10-28 10:05:00', 19.05, 72.85, 55.0, 48.0, 1005.0)`)
	require.NoError(t, err)

	// Apply migration 00170
	require.NoError(t, goose.UpTo(db, "migrations", 170))

	// 1. Verify duplicate index dropped
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_telemetry_snapshots_vehicle_time'`).Scan(&dupCount))
	assert.Equal(t, 0, dupCount, "duplicate index idx_telemetry_snapshots_vehicle_time must be dropped at v170")

	// 2. Verify new scale indexes exist
	expectedIndexes := []string{
		"idx_positions_device_time",
		"idx_positions_trip_time",
		"idx_raw_device_time",
		"idx_snapshots_timestamp",
		"idx_outbox_published_created_at",
		"idx_vlp_trip",
	}
	for _, idx := range expectedIndexes {
		var idxCount int
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='%s'`, idx)).Scan(&idxCount))
		assert.Equal(t, 1, idxCount, fmt.Sprintf("index %s must exist at v170", idx))
	}

	// 3. Verify backfill populated vehicle_latest_position with the newest snapshot
	var backfilledLat, backfilledSpeed float64
	var backfilledTime string
	err = db.QueryRow(`SELECT latitude, speed, device_time FROM vehicle_latest_position WHERE vehicle_id = 'v-mig-170'`).
		Scan(&backfilledLat, &backfilledSpeed, &backfilledTime)
	require.NoError(t, err, "v-mig-170 must be backfilled into vehicle_latest_position")
	assert.InDelta(t, 19.05, backfilledLat, 0.0001)
	assert.InDelta(t, 55.0, backfilledSpeed, 0.0001)

	// 4. Test Down migration rollback to 169
	require.NoError(t, goose.DownTo(db, "migrations", 169))
	for _, idx := range expectedIndexes {
		var idxCount int
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='%s'`, idx)).Scan(&idxCount))
		assert.Equal(t, 0, idxCount, fmt.Sprintf("rollback must drop index %s", idx))
	}
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_telemetry_snapshots_vehicle_time'`).Scan(&dupCount))
	assert.Equal(t, 1, dupCount, "rollback must recreate idx_telemetry_snapshots_vehicle_time")

	// 5. Re-apply to 170 cleanly
	require.NoError(t, goose.UpTo(db, "migrations", 170))
}
