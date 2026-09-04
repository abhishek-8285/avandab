package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Scoped reopen touches only the given org: a neighbour org's expired
// snoozes stay snoozed even when the global sweep would have reopened them.
func TestReopenExpiredSnoozesForTenant_Scoped(t *testing.T) {
	db := newInboxTestDB(t)
	repo := NewAlertRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	insertAlert(t, db, "al-q-1", "tenant-a", 1, now)
	insertAlert(t, db, "al-q-2", "tenant-b", 1, now)

	past := now.Add(-time.Hour)
	_, err := db.Exec(`UPDATE alerts SET ack_status = 'snoozed', snoozed_until = ? WHERE id IN ('al-q-1','al-q-2')`, past)
	require.NoError(t, err)

	n, err := repo.ReopenExpiredSnoozesForTenant(ctx, "tenant-a", now)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	var aStatus, bStatus string
	require.NoError(t, db.QueryRow(`SELECT ack_status FROM alerts WHERE id = 'al-q-1'`).Scan(&aStatus))
	require.NoError(t, db.QueryRow(`SELECT ack_status FROM alerts WHERE id = 'al-q-2'`).Scan(&bStatus))
	assert.Equal(t, "open", aStatus)
	assert.Equal(t, "snoozed", bStatus)

	_, err = repo.ReopenExpiredSnoozesForTenant(ctx, "", now)
	require.Error(t, err, "empty tenant must be refused")
}
