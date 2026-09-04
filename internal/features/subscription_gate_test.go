package features

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedSubscription(t *testing.T, reg *Registry, tenant, status, trialEnd, periodEnd string) {
	t.Helper()
	_, err := reg.db.Exec(`CREATE TABLE IF NOT EXISTS tenant_subscriptions (
		tenant_id TEXT PRIMARY KEY, plan_id TEXT, status TEXT,
		current_period_start DATETIME, current_period_end DATETIME, trial_end DATETIME)`)
	require.NoError(t, err)
	_, err = reg.db.Exec(`INSERT OR REPLACE INTO tenant_subscriptions
		(tenant_id, plan_id, status, current_period_start, current_period_end, trial_end)
		VALUES (?, 'STARTER', ?, '2026-01-01 00:00:00', ?, ?)`,
		tenant, status, periodEnd, trialEnd)
	require.NoError(t, err)
	reg.invalidate(tenant)
}

// Lapsed subscriptions lose add-ons; core keeps working.
func TestSubscriptionGate_BlocksAddons(t *testing.T) {
	ctx := context.Background()

	pastDue := testRegistry(t, nil)
	seedSubscription(t, pastDue, "t-past", "PAST_DUE", "", "2026-08-29 00:00:00")
	assert.False(t, pastDue.Enabled(ctx, "t-past", "fastag"), "PAST_DUE loses addons")
	assert.True(t, pastDue.Enabled(ctx, "t-past", "ewaybill"), "PAST_DUE keeps core")

	expiredTrial := testRegistry(t, nil)
	seedSubscription(t, expiredTrial, "t-exp", "TRIAL", "2026-08-01 00:00:00", "2026-08-01 00:00:00")
	assert.False(t, expiredTrial.Enabled(ctx, "t-exp", "fastag"), "expired trial loses addons")

	liveTrial := testRegistry(t, nil)
	seedSubscription(t, liveTrial, "t-trial", "TRIAL", "2099-01-01 00:00:00", "2099-01-01 00:00:00")
	assert.False(t, liveTrial.Enabled(ctx, "t-trial", "fastag"), "trial addon still needs grant")
	require.NoError(t, liveTrial.Set(ctx, "t-trial", "fastag", true, "admin"))
	assert.True(t, liveTrial.Enabled(ctx, "t-trial", "fastag"), "live trial + grant works")

	active := testRegistry(t, nil)
	seedSubscription(t, active, "t-act", "ACTIVE", "", "2099-01-01 00:00:00")
	assert.False(t, active.Enabled(ctx, "t-act", "fastag"), "active addon still needs grant")

	closed := testRegistry(t, nil)
	seedSubscription(t, closed, "t-closed", "ACCOUNT_CLOSED", "", "")
	assert.False(t, closed.Enabled(ctx, "t-closed", "fastag"), "closed loses addons")
	assert.True(t, closed.Enabled(ctx, "t-closed", "ewaybill"), "closed keeps core")
}

// Explicit admin grants survive billing state; orgs without any
// subscription row keep legacy behaviour (never locked out).
func TestSubscriptionGate_ExplicitGrantAndLegacy(t *testing.T) {
	ctx := context.Background()

	grant := testRegistry(t, nil)
	seedSubscription(t, grant, "t-past-grant", "PAST_DUE", "", "2026-08-29 00:00:00")
	require.NoError(t, grant.Set(ctx, "t-past-grant", "fastag", true, "admin"))
	assert.True(t, grant.Enabled(ctx, "t-past-grant", "fastag"), "manual grant wins over PAST_DUE")

	legacy := testRegistry(t, nil)
	assert.True(t, legacy.Enabled(ctx, "t-legacy", "telemetry"), "no subscription row keeps env defaults")
}
