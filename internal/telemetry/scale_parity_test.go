package telemetry

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTelemetryScale_IndexQueryPlans verifies FleetX Parity P1.2:
// Retention purge subqueries and outbox pruning use index range scans,
// not full-table scans.
func TestTelemetryScale_IndexQueryPlans(t *testing.T) {
	db := newTestIngestorDB(t)

	// 1. Positions retention query plan
	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT id FROM telemetry_positions WHERE device_time < '2026-01-01' LIMIT 1000`)
	require.NoError(t, err)
	defer rows.Close()
	var posPlan string
	for rows.Next() {
		var a, b, c int
		var detail string
		require.NoError(t, rows.Scan(&a, &b, &c, &detail))
		posPlan += detail + " "
	}
	assert.Contains(t, posPlan, "idx_positions_device_time", "telemetry_positions retention must use idx_positions_device_time")
	assert.NotContains(t, strings.ToUpper(posPlan), "SCAN TELEMETRY_POSITIONS", "telemetry_positions retention must not do full table scan")

	// 2. Raw events retention query plan
	rowsRaw, err := db.Query(`EXPLAIN QUERY PLAN SELECT id FROM telemetry_raw_events WHERE device_time < '2026-01-01' LIMIT 1000`)
	require.NoError(t, err)
	defer rowsRaw.Close()
	var rawPlan string
	for rowsRaw.Next() {
		var a, b, c int
		var detail string
		require.NoError(t, rowsRaw.Scan(&a, &b, &c, &detail))
		rawPlan += detail + " "
	}
	assert.Contains(t, rawPlan, "idx_raw_device_time", "telemetry_raw_events retention must use idx_raw_device_time")

	// 3. Snapshots retention query plan
	rowsSnap, err := db.Query(`EXPLAIN QUERY PLAN SELECT id FROM telemetry_snapshots WHERE timestamp < '2026-01-01' LIMIT 1000`)
	require.NoError(t, err)
	defer rowsSnap.Close()
	var snapPlan string
	for rowsSnap.Next() {
		var a, b, c int
		var detail string
		require.NoError(t, rowsSnap.Scan(&a, &b, &c, &detail))
		snapPlan += detail + " "
	}
	assert.Contains(t, snapPlan, "idx_snapshots_timestamp", "telemetry_snapshots retention must use idx_snapshots_timestamp")

	// 4. Outbox published retention query plan
	rowsOutbox, err := db.Query(`EXPLAIN QUERY PLAN SELECT id FROM outbox_events WHERE published_at IS NOT NULL AND created_at < '2026-01-01' LIMIT 1000`)
	require.NoError(t, err)
	defer rowsOutbox.Close()
	var outboxPlan string
	for rowsOutbox.Next() {
		var a, b, c int
		var detail string
		require.NoError(t, rowsOutbox.Scan(&a, &b, &c, &detail))
		outboxPlan += detail + " "
	}
	assert.Contains(t, outboxPlan, "idx_outbox_published_created_at", "outbox retention must use partial index idx_outbox_published_created_at")
}

// TestTelemetryScale_LiveQueryReadsVLP verifies FleetX Parity P1.5:
// Live store reads vehicle_latest_position and does not scan telemetry_snapshots.
func TestTelemetryScale_LiveQueryReadsVLP(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicleReg(t, db, "v-vlp-scale", "REG-VLP")

	now := time.Now().UTC()
	// Insert directly into vehicle_latest_position with NO snapshot
	_, err := db.Exec(`INSERT INTO vehicle_latest_position
		(vehicle_id, tenant_id, device_time, latitude, longitude, speed, fuel_level, odometer)
		VALUES ('v-vlp-scale', '1', ?, 19.123, 72.456, 52.0, 75.0, 15000.0)`,
		now.Add(-2*time.Minute).Format("2006-01-02 15:04:05"))
	require.NoError(t, err)

	store := NewLiveStore(db, 15*time.Minute)
	vehicles, err := store.Live(context.Background(), "1", "", now)
	require.NoError(t, err)
	require.Len(t, vehicles, 1)

	v := vehicles[0]
	assert.Equal(t, "v-vlp-scale", v.VehicleID)
	assert.InDelta(t, 19.123, v.Lat, 0.0001)
	assert.InDelta(t, 72.456, v.Lng, 0.0001)
	assert.InDelta(t, 52.0, v.Speed, 0.0001)
	assert.Equal(t, MarkerStateRunning, v.Status)
}
