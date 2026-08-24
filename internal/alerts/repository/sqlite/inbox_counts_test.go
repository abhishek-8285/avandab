package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInboxCounts — Spec 22 §2.2 open_alerts/critical counts use the same
// visibility rule as ListInbox status=open: future-snoozed hidden,
// expired-snoozed visible, acked excluded; rank-1 rows count as critical.
func TestInboxCounts(t *testing.T) {
	db := newInboxTestDB(t)
	repo := NewAlertRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	open, critical, err := repo.InboxCounts(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, 0, open)
	assert.Equal(t, 0, critical)

	insertAlert(t, db, "a1", "1", 1, now.Add(-3*time.Hour))
	insertAlert(t, db, "a2", "1", 4, now.Add(-2*time.Hour))
	insertAlert(t, db, "a3", "1", 2, now.Add(-1*time.Hour))
	_, err = db.Exec(`UPDATE alerts SET ack_status='snoozed', snoozed_until=? WHERE id='a3'`, now.Add(time.Hour))
	require.NoError(t, err)
	insertAlert(t, db, "a4", "1", 5, now.Add(-4*time.Hour))
	_, err = db.Exec(`UPDATE alerts SET ack_status='snoozed', snoozed_until=? WHERE id='a4'`, now.Add(-time.Minute))
	require.NoError(t, err)
	insertAlert(t, db, "a5", "1", 1, now.Add(-5*time.Hour))
	_, err = db.Exec(`UPDATE alerts SET ack_status='acked', status='acknowledged' WHERE id='a5'`)
	require.NoError(t, err)
	// Tenant 2: isolated.
	insertAlert(t, db, "b1", "2", 1, now.Add(-time.Hour))

	open, critical, err = repo.InboxCounts(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, 3, open, "open + expired-snooze visible; future snooze and acked hidden")
	assert.Equal(t, 1, critical, "only rank-1 rows count as critical")

	open, _, err = repo.InboxCounts(ctx, "2")
	require.NoError(t, err)
	assert.Equal(t, 1, open, "tenant isolation: tenant 2 sees only its own alert")
}
