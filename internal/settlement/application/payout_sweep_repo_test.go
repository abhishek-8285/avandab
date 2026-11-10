package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	settleSQL "transport-app/internal/settlement/infrastructure/persistence/sql"
)

func TestSettlementRepository_ListPayoutsAwaitingCompletion(t *testing.T) {
	db := setupSettlementTestDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	old := now.Add(-25 * time.Hour)
	fresh := now.Add(-1 * time.Hour)

	insert := func(id, status string, initiatedAt time.Time) {
		t.Helper()
		_, err := db.Exec(`
			INSERT INTO payout_instructions
				(id, tenant_id, driver_id, payout_account_id, amount, currency, idempotency_key,
				 status, initiated_at, created_at, updated_at)
			VALUES (?, 'tenant-1', 'drv-settle-1', 'acc-1', 500, 'INR', ?, ?, ?, ?, ?)`,
			id, "idem-"+id, status, initiatedAt, initiatedAt, initiatedAt)
		require.NoError(t, err)
	}
	insert("pout-old-initiated", "initiated", old.Add(-time.Hour))
	insert("pout-old-processing", "processing", old)
	insert("pout-old-paid", "paid", old)
	insert("pout-fresh-processing", "processing", fresh)

	got, err := repo.ListPayoutsAwaitingCompletion(ctx, now.Add(-24*time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, got, 2, "only non-terminal payouts older than the cutoff")
	assert.Equal(t, "pout-old-initiated", got[0].ID, "oldest first")
	assert.Equal(t, "pout-old-processing", got[1].ID)
	for _, p := range got {
		assert.Equal(t, "tenant-1", p.TenantID)
		assert.Equal(t, "drv-settle-1", p.DriverID)
	}
}
