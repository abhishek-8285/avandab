package notifications

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEmailPool_UsageMatchesDB is a timezone-agnostic regression test for the
// quota-rollover read path. getQuotaUsed compares the DB's UTC date columns
// (date('now')) against Go-side dates; using local time there silently reset
// every counter to 0 whenever the local date drifted ahead of UTC
// (e.g. 00:00–05:30 IST), permanently disabling quota enforcement.
// The invariant: right after sends, GetUsage() must agree with the raw
// persisted counters — the rollover reset may only fire for rows that are
// genuinely from a previous day, which never happens inside a fresh test DB.
func TestEmailPool_UsageMatchesDB(t *testing.T) {
	db := newPoolTestDB(t)
	pool := NewEmailPool(PoolConfig{
		Strategy: "priority",
		Providers: []ProviderSpec{
			{Name: "brevo", Host: "smtp-relay.brevo.com", Port: "587", From: "billing@avandab.com", DailyQuota: 10, MonthlyQuota: 9000, Priority: 1},
			{Name: "resend", Host: "smtp.resend.com", Port: "587", From: "billing@avandab.com", DailyQuota: 10, MonthlyQuota: 3000, Priority: 2},
		},
	}, db, nil)
	for _, e := range pool.providers {
		e.sender = &fakePoolSender{name: e.spec.Name}
	}

	ctx := context.Background()
	require.NoError(t, pool.Send(ctx, "a@b.c", "s1", "b1"))
	require.NoError(t, pool.Send(ctx, "a@b.c", "s2", "b2"))

	usage := pool.GetUsage()
	for _, u := range usage {
		var du, mu int
		err := db.QueryRowContext(ctx,
			`SELECT daily_used, monthly_used FROM email_provider_counters WHERE provider = ?`, u.Name).
			Scan(&du, &mu)
		require.NoError(t, err, "counter row for %s must exist", u.Name)
		require.Equal(t, du, u.DailyUsed,
			"%s: GetUsage().DailyUsed must match persisted counter (TZ rollover bug)", u.Name)
		require.Equal(t, mu, u.MonthlyUsed,
			"%s: GetUsage().MonthlyUsed must match persisted counter (TZ rollover bug)", u.Name)
	}

	brevo := usage[0]
	require.Equal(t, 2, brevo.DailyUsed)
	require.False(t, brevo.Exhausted)
}
