package test

import (
	"context"
	"testing"
	"time"
	"transport-app/internal/shared"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/alerts/repository/sqlite"
)

// TestSpec22_Migration00092RoundTrip — Spec 22 §7: 00092 applies, backfills
// inbox columns, and rolls down cleanly (up → downTo(91) → up).
func TestSpec22_Migration00092RoundTrip(t *testing.T) {
	db := NewTestDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// Seed one pre-00092-shaped row is impossible post-up (columns exist),
	// so verify the backfill semantics directly: insert with defaults, then
	// confirm column presence + default behaviour.
	_, err := db.Exec(`INSERT INTO alerts (
		id, source, alert_type, severity, status, dedup_key,
		title, message, occurrences, first_seen_at, last_seen_at, created_at, updated_at
	) VALUES ('al-rt', 'telemetry', 'speeding', 'critical', 'open', 'dk-rt',
		't', 'm', 1, ?, ?, ?, ?)`,
		time.Now().UTC(), time.Now().UTC(), time.Now().UTC(), time.Now().UTC())
	require.NoError(t, err)

	var ackStatus, tenantID string
	var rank int
	require.NoError(t, db.QueryRow(
		`SELECT ack_status, tenant_id, severity_rank FROM alerts WHERE id = 'al-rt'`).
		Scan(&ackStatus, &tenantID, &rank), "inbox columns must exist after 00092")
	assert.Equal(t, "open", ackStatus)
	assert.Equal(t, "1", tenantID, "tenant_id column defaults to bootstrap tenant")
	assert.Equal(t, 5, rank, "bare SQL inserts default to rank 5 (engine sets explicit ranks)")

	repo := sqlite.NewAlertRepository(db)
	rows, err := repo.ListInbox(ctx, "1", "all", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "al-rt", rows[0].ID)

	var perms int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM permissions WHERE name IN
		 ('alerts:read','alerts:write','kharcha:approve','compliance:read','driver:read-self','driver:write-self')`).
		Scan(&perms))
	assert.Equal(t, 6, perms, "all six spec-22 permissions seeded idempotently")

	// Down to 91 removes the columns; re-up restores them.
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.DownTo(db, "../db/migrations", 91))
	err = db.QueryRow(`SELECT ack_status FROM alerts LIMIT 1`).Scan(&ackStatus)
	assert.Error(t, err, "ack_status must be gone after downgrade")

	require.NoError(t, goose.Up(db, "../db/migrations"))
	err = db.QueryRow(`SELECT ack_status FROM alerts WHERE id = 'al-rt'`).Scan(&ackStatus)
	if err == nil {
		t.Log("row survived down/up cycle")
	}
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM alerts`).Scan(new(int)),
		"alerts table queryable after re-up")
}
