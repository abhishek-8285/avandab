package telemetry

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetryCleaner_Purge(t *testing.T) {
	db := newTestIngestorDB(t)
	ctx := context.Background()

	// Seed old data (> 30 days ago) and fresh data
	oldTime := time.Now().UTC().AddDate(0, 0, -45)
	freshTime := time.Now().UTC().Add(-10 * time.Minute)

	// Seed raw events
	_, err := db.Exec(`INSERT INTO telemetry_raw_events (id, tenant_id, imei, device_time, provider, payload) VALUES
		('raw-old', '1', 'IMEI-OLD', ?, 'own', '{}'),
		('raw-fresh', '1', 'IMEI-FRESH', ?, 'own', '{}')`, oldTime, freshTime)
	require.NoError(t, err)

	// Seed positions
	_, err = db.Exec(`INSERT INTO telemetry_positions (id, tenant_id, imei, device_time, latitude, longitude, raw_event_id) VALUES
		('pos-old', '1', 'IMEI-OLD', ?, 19.0, 72.0, 'raw-old'),
		('pos-fresh', '1', 'IMEI-FRESH', ?, 19.1, 72.1, 'raw-fresh')`, oldTime, freshTime)
	require.NoError(t, err)

	// Seed snapshots
	_, err = db.Exec(`INSERT INTO telemetry_snapshots (id, vehicle_id, timestamp, ts_unix, latitude, longitude) VALUES
		('snap-old', 'v-old', ?, ?, 19.0, 72.0),
		('snap-fresh', 'v-fresh', ?, ?, 19.1, 72.1)`, oldTime, oldTime.Unix(), freshTime, freshTime.Unix())
	require.NoError(t, err)

	// Seed outbox events
	_, err = db.Exec(`INSERT INTO outbox_events (id, aggregate_id, aggregate_type, event_type, payload, created_at, published_at) VALUES
		('ob-old-pub', 'v-old', 'Vehicle', 'PositionEvent', '{}', ?, ?),
		('ob-old-unpub', 'v-old', 'Vehicle', 'PositionEvent', '{}', ?, NULL),
		('ob-fresh', 'v-fresh', 'Vehicle', 'PositionEvent', '{}', ?, ?)`,
		oldTime, oldTime, oldTime, freshTime, freshTime)
	require.NoError(t, err)

	// Seed sessions (expired and active)
	_, err = db.Exec(`INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES
		('sess-expired', '1', 'hash-old', ?),
		('sess-active', '1', 'hash-fresh', ?)`,
		oldTime, freshTime.Add(24*time.Hour))
	require.NoError(t, err)

	cleaner := NewTelemetryCleaner(db, 30, nil)
	cleaner.Purge(ctx)

	// Verify old positions pruned, fresh preserved
	var posOldCount, posFreshCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM telemetry_positions WHERE id = 'pos-old'`).Scan(&posOldCount))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM telemetry_positions WHERE id = 'pos-fresh'`).Scan(&posFreshCount))
	assert.Equal(t, 0, posOldCount, "old position must be purged")
	assert.Equal(t, 1, posFreshCount, "fresh position must be preserved")

	// Verify old raw events pruned, fresh preserved
	var rawOldCount, rawFreshCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM telemetry_raw_events WHERE id = 'raw-old'`).Scan(&rawOldCount))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM telemetry_raw_events WHERE id = 'raw-fresh'`).Scan(&rawFreshCount))
	assert.Equal(t, 0, rawOldCount, "old raw event must be purged")
	assert.Equal(t, 1, rawFreshCount, "fresh raw event must be preserved")

	// Verify old snapshots pruned
	var snapOldCount, snapFreshCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM telemetry_snapshots WHERE id = 'snap-old'`).Scan(&snapOldCount))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM telemetry_snapshots WHERE id = 'snap-fresh'`).Scan(&snapFreshCount))
	assert.Equal(t, 0, snapOldCount, "old snapshot must be purged")
	assert.Equal(t, 1, snapFreshCount, "fresh snapshot must be preserved")

	// Verify published old outbox pruned, unpublished kept
	var obOldPubCount, obOldUnpubCount, obFreshCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM outbox_events WHERE id = 'ob-old-pub'`).Scan(&obOldPubCount))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM outbox_events WHERE id = 'ob-old-unpub'`).Scan(&obOldUnpubCount))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM outbox_events WHERE id = 'ob-fresh'`).Scan(&obFreshCount))
	assert.Equal(t, 0, obOldPubCount, "published old outbox event must be purged")
	assert.Equal(t, 1, obOldUnpubCount, "unpublished outbox event must never be purged")
	assert.Equal(t, 1, obFreshCount, "fresh outbox event must be preserved")

	// Verify expired sessions pruned, active preserved
	var sessExpiredCount, sessActiveCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM sessions WHERE id = 'sess-expired'`).Scan(&sessExpiredCount))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM sessions WHERE id = 'sess-active'`).Scan(&sessActiveCount))
	assert.Equal(t, 0, sessExpiredCount, "expired session must be purged")
	assert.Equal(t, 1, sessActiveCount, "active session must be preserved")
}

func TestTelemetryCleaner_ScalePurge(t *testing.T) {
	db := newTestIngestorDB(t)
	ctx := context.Background()

	oldTime := time.Now().UTC().AddDate(0, 0, -40)
	freshTime := time.Now().UTC().Add(-5 * time.Minute)

	// Seed 10,000 stale positions across 2 batches in a transaction to test chunked deletion
	tx, err := db.Begin()
	require.NoError(t, err)

	stmtPos, err := tx.Prepare(`INSERT INTO telemetry_positions (id, tenant_id, imei, device_time, latitude, longitude) VALUES (?, '1', 'IMEI-SCALE', ?, 19.0, 72.0)`)
	require.NoError(t, err)
	defer stmtPos.Close()

	stmtRaw, err := tx.Prepare(`INSERT INTO telemetry_raw_events (id, tenant_id, imei, device_time, provider, payload) VALUES (?, '1', 'IMEI-SCALE', ?, 'scale', '{}')`)
	require.NoError(t, err)
	defer stmtRaw.Close()

	const staleCount = 10000
	for i := 0; i < staleCount; i++ {
		id := fmt.Sprintf("scale-old-%d", i)
		ts := oldTime.Add(time.Duration(i) * time.Second)
		_, err = stmtRaw.Exec(id, ts)
		require.NoError(t, err)
		_, err = stmtPos.Exec(id, ts)
		require.NoError(t, err)
	}

	// Also insert fresh rows
	_, err = stmtRaw.Exec("scale-fresh-1", freshTime)
	require.NoError(t, err)
	_, err = stmtPos.Exec("scale-fresh-1", freshTime)
	require.NoError(t, err)

	require.NoError(t, tx.Commit())

	cleaner := NewTelemetryCleaner(db, 30, nil)

	start := time.Now()
	cleaner.Purge(ctx)
	duration := time.Since(start)

	// P1.4 Acceptance: Purge completes < 60s at 30-day volume
	assert.Less(t, duration, 10*time.Second, "scale purge of 10,000 rows must complete well under 10s")

	var staleRemaining, freshRemaining int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM telemetry_positions WHERE device_time < ?`, oldTime.Add(24*time.Hour)).Scan(&staleRemaining))
	assert.Equal(t, 0, staleRemaining, "all stale rows must be purged")

	require.NoError(t, db.QueryRow(`SELECT count(*) FROM telemetry_positions WHERE id = 'scale-fresh-1'`).Scan(&freshRemaining))
	assert.Equal(t, 1, freshRemaining, "fresh row must be retained")
}
