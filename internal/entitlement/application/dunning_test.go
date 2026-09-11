package application_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/entitlement/application"
	"transport-app/internal/entitlement/domain"
	"transport-app/internal/shared"
)

func seedAgedSubscription(t *testing.T, db *sql.DB, tenantID string, status domain.SubscriptionStatus, updatedAgo time.Duration) {
	t.Helper()
	now := time.Now().UTC()
	upd := now.Add(-updatedAgo).Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO tenant_subscriptions (id, tenant_id, plan_id, status, current_period_start, current_period_end, provider_subscription_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "sub_"+tenantID, tenantID, string(domain.PlanGrowth), string(status),
		now.Add(-30*24*time.Hour).Format(time.RFC3339), now.Add(30*24*time.Hour).Format(time.RFC3339),
		"sub_rzp_"+tenantID, upd, upd)
	require.NoError(t, err)
}

func TestSweepDunning_ProgressesStaleDelinquencies(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	svc := application.NewService(db)
	ctx := context.Background()

	seedAgedSubscription(t, db, "dun-pd-old", domain.SubPastDue, 8*24*time.Hour)
	seedAgedSubscription(t, db, "dun-pd-fresh", domain.SubPastDue, 24*time.Hour)
	seedAgedSubscription(t, db, "dun-gr-old", domain.SubGrace, 15*24*time.Hour)
	seedAgedSubscription(t, db, "dun-active", domain.SubActive, 90*24*time.Hour)

	pd, gr, err := svc.SweepDunning(ctx, time.Now().UTC())
	require.NoError(t, err)
	assert.Equal(t, int64(1), pd)
	assert.Equal(t, int64(1), gr)

	statusOf := func(tenant string) domain.SubscriptionStatus {
		sub, err := svc.GetSubscription(ctx, shared.TenantID(tenant))
		require.NoError(t, err)
		return sub.Status
	}
	assert.Equal(t, domain.SubGrace, statusOf("dun-pd-old"))
	assert.Equal(t, domain.SubPastDue, statusOf("dun-pd-fresh"))
	assert.Equal(t, domain.SubReadOnly, statusOf("dun-gr-old"))
	assert.Equal(t, domain.SubActive, statusOf("dun-active"))

	// Second sweep is a no-op (one step per run; fresh states).
	pd, gr, err = svc.SweepDunning(ctx, time.Now().UTC())
	require.NoError(t, err)
	assert.Equal(t, int64(0), pd)
	assert.Equal(t, int64(0), gr)
	assert.Equal(t, domain.SubGrace, statusOf("dun-pd-old"))
}
