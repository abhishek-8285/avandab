package application

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// Platform tenant-administration characterization (handlers-end-state ADR,
// Phase 2). The handler HTTP characterization suites stay in
// internal/handlers/tenants_test.go; these cases pin the moved SQL semantics:
// per-tenant subscription/quota/count reads, the slug conflict probe, the
// best-effort commercial writes (plan upsert with provider-link preservation,
// trial extension, override upsert), lifecycle session purge scoping and the
// nil-store degradation.

func newTenantAdminTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_tenant_admin_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	cwd, _ := os.Getwd()
	migDir := ""
	for _, cand := range []string{"../../../db/migrations", "../../db/migrations", "../db/migrations", "db/migrations"} {
		if _, statErr := os.Stat(cand); statErr == nil {
			migDir = cand
			break
		}
	}
	require.NotEmpty(t, migDir, "db/migrations not found from %s", cwd)
	goose.SetLogger(goose.NopLogger())
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migDir))
	// Production migrations remove the 00103 test-only tenants (00107); the
	// console tests seed the tenants they exercise, like the handler tests do.
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
		('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b')`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedTenantSubscription(t *testing.T, db *sql.DB, id, tenantID, planID, status, periodEnd, trialEnd string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO tenant_subscriptions (id, tenant_id, plan_id, status, current_period_start, current_period_end, trial_end)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, planID, status, "2026-01-01T00:00:00Z", periodEnd, trialEnd)
	require.NoError(t, err)
}

func seedTenantUser(t *testing.T, db *sql.DB, id, email, tenantID string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id)
		VALUES (?, ?, 'x', 'User', 2, 'active', ?)`, id, email, tenantID)
	require.NoError(t, err)
}

func seedTenantSession(t *testing.T, db *sql.DB, id, userID string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO sessions (id, user_id, token_hash, expires_at)
		VALUES (?, ?, ?, datetime('now', '+1 day'))`, id, userID, "hash_"+id)
	require.NoError(t, err)
}

func TestTenantAdminQueries_SubscriptionAndQuotaAreTenantScoped(t *testing.T) {
	db := newTenantAdminTestDB(t)
	seedTenantSubscription(t, db, "sub_tenant-1", "tenant-1", "GROWTH", "ACTIVE", "2026-12-01T00:00:00Z", "")
	seedTenantSubscription(t, db, "sub_tenant-2", "tenant-2", "STARTER", "TRIAL", "2026-12-01T00:00:00Z", "2026-11-20T00:00:00Z")
	_, err := db.Exec(`INSERT INTO tenant_usage_meters (id, tenant_id, quota_key, period_start, period_end, used_quantity, max_quantity)
		VALUES ('meter-1', 'tenant-1', 'max_trips_per_month', '2026-11-01T00:00:00Z', '2026-12-01T00:00:00Z', 200, 250)`)
	require.NoError(t, err)
	ctx := context.Background()
	queries := NewTenantAdminQueries(db)

	sub, err := queries.Subscription(ctx, "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, "GROWTH", sub.PlanID)
	assert.Equal(t, "ACTIVE", sub.Status)
	assert.Equal(t, 1999.00, sub.MonthlyPrice)
	assert.Equal(t, "2026-12-01T00:00:00Z", sub.PeriodEnd)
	assert.Empty(t, sub.TrialEnd, "NULL trial_end must stay empty")

	sub2, err := queries.Subscription(ctx, "tenant-2")
	require.NoError(t, err)
	assert.Equal(t, "STARTER", sub2.PlanID)
	assert.Equal(t, "TRIAL", sub2.Status)
	assert.Equal(t, 0.00, sub2.MonthlyPrice)
	assert.Equal(t, "2026-11-20T00:00:00Z", sub2.TrialEnd)

	// A tenant without a subscription row is an error (legacy caller defaults
	// the item to STARTER/TRIAL and counts it as a trial).
	_, err = queries.Subscription(ctx, "tenant-9")
	require.Error(t, err)

	quota, err := queries.TripQuota(ctx, "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, 200, quota.Used)
	assert.Equal(t, 250, quota.Max)
	_, err = queries.TripQuota(ctx, "tenant-2")
	require.Error(t, err)
}

func TestTenantAdminQueries_UserCountsGrouped(t *testing.T) {
	db := newTenantAdminTestDB(t)
	seedTenantUser(t, db, "u1", "u1@test", "tenant-1")
	seedTenantUser(t, db, "u2", "u2@test", "tenant-1")
	seedTenantUser(t, db, "u3", "u3@test", "tenant-2")
	ctx := context.Background()

	counts, err := NewTenantAdminQueries(db).UserCounts(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 2, counts["tenant-1"])
	assert.EqualValues(t, 1, counts["tenant-2"])
	_, hasDefault := counts["1"]
	assert.False(t, hasDefault, "tenants without users must not appear")
}

func TestTenantAdminQueries_TenantSlugExists(t *testing.T) {
	db := newTenantAdminTestDB(t)
	ctx := context.Background()
	queries := NewTenantAdminQueries(db)

	// The migration seeds tenant '1' with slug 'default'.
	exists, err := queries.TenantSlugExists(ctx, "1")
	require.NoError(t, err)
	assert.True(t, exists, "probe must match the tenant id column")
	exists, err = queries.TenantSlugExists(ctx, "default")
	require.NoError(t, err)
	assert.True(t, exists, "probe must match the slug column")
	exists, err = queries.TenantSlugExists(ctx, "acme")
	require.NoError(t, err)
	assert.False(t, exists)

	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('acme', 'Acme', 'acme')`)
	require.NoError(t, err)
	exists, err = queries.TenantSlugExists(ctx, "acme")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestTenantAdminService_UpdatePlanUpsertsAndPreservesProviderLink(t *testing.T) {
	db := newTenantAdminTestDB(t)
	ctx := context.Background()
	svc := NewTenantAdminService(db)

	require.NoError(t, svc.UpdatePlan(ctx, UpdatePlanCommand{TenantID: "tenant-1", PlanID: "GROWTH", ProviderSubscriptionID: "rzp_link_1"}))
	var id, planID, status, provider, periodStart, periodEnd string
	require.NoError(t, db.QueryRow(`SELECT id, plan_id, status, COALESCE(provider_subscription_id, ''), current_period_start, current_period_end
		FROM tenant_subscriptions WHERE tenant_id = 'tenant-1'`).Scan(&id, &planID, &status, &provider, &periodStart, &periodEnd))
	assert.Equal(t, "sub_tenant-1", id, "legacy subscription id shape")
	assert.Equal(t, "GROWTH", planID)
	assert.Equal(t, "ACTIVE", status)
	assert.Equal(t, "rzp_link_1", provider)
	start, err := time.Parse(time.RFC3339, periodStart)
	require.NoError(t, err)
	end, err := time.Parse(time.RFC3339, periodEnd)
	require.NoError(t, err)
	assert.Equal(t, 30*24*time.Hour, end.Sub(start), "legacy 30-day billing period")

	// A plan-only change must keep the Razorpay link (COALESCE/NULLIF).
	require.NoError(t, svc.UpdatePlan(ctx, UpdatePlanCommand{TenantID: "tenant-1", PlanID: "SCALE"}))
	require.NoError(t, db.QueryRow(`SELECT plan_id, status, provider_subscription_id FROM tenant_subscriptions WHERE tenant_id = 'tenant-1'`).Scan(&planID, &status, &provider))
	assert.Equal(t, "SCALE", planID)
	assert.Equal(t, "ACTIVE", status)
	assert.Equal(t, "rzp_link_1", provider, "empty provider id must preserve the existing link")

	// Another tenant is untouched (tenant scoping).
	var otherRows int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM tenant_subscriptions WHERE tenant_id = 'tenant-2'`).Scan(&otherRows))
	assert.Zero(t, otherRows)
}

func TestTenantAdminService_ExtendTrialScoped(t *testing.T) {
	db := newTenantAdminTestDB(t)
	seedTenantSubscription(t, db, "sub_tenant-1", "tenant-1", "STARTER", "PAST_DUE", "2026-12-01T00:00:00Z", "2026-11-01T00:00:00Z")
	seedTenantSubscription(t, db, "sub_tenant-2", "tenant-2", "STARTER", "PAST_DUE", "2026-12-01T00:00:00Z", "2026-11-01T00:00:00Z")
	ctx := context.Background()

	require.NoError(t, NewTenantAdminService(db).ExtendTrial(ctx, "tenant-1"))

	var status, trialEnd string
	require.NoError(t, db.QueryRow(`SELECT status, trial_end FROM tenant_subscriptions WHERE tenant_id = 'tenant-1'`).Scan(&status, &trialEnd))
	assert.Equal(t, "TRIAL", status)
	parsed, err := time.Parse(time.RFC3339, trialEnd)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().UTC().Add(14*24*time.Hour), parsed, 5*time.Minute, "legacy 14-day extension")

	var otherStatus, otherTrialEnd string
	require.NoError(t, db.QueryRow(`SELECT status, trial_end FROM tenant_subscriptions WHERE tenant_id = 'tenant-2'`).Scan(&otherStatus, &otherTrialEnd))
	assert.Equal(t, "PAST_DUE", otherStatus, "another tenant must not be extended")
	assert.Equal(t, "2026-11-01T00:00:00Z", otherTrialEnd)
}

func TestTenantAdminService_SetOverrideUpsertsPerTenantKey(t *testing.T) {
	db := newTenantAdminTestDB(t)
	ctx := context.Background()
	svc := NewTenantAdminService(db)

	require.NoError(t, svc.SetOverride(ctx, SetOverrideCommand{TenantID: "tenant-1", EntitlementType: "FEATURE", KeyName: "max_vehicles", OverrideValue: "100", Reason: "contract v1"}))
	var id, val, reason string
	require.NoError(t, db.QueryRow(`SELECT id, override_value, reason FROM tenant_entitlement_overrides WHERE tenant_id = 'tenant-1' AND entitlement_type = 'FEATURE' AND key_name = 'max_vehicles'`).Scan(&id, &val, &reason))
	assert.Equal(t, "ovr_tenant-1_max_vehicles", id, "legacy override id shape")
	assert.Equal(t, "100", val)
	assert.Equal(t, "contract v1", reason)

	// Same (tenant, type, key) upserts in place.
	require.NoError(t, svc.SetOverride(ctx, SetOverrideCommand{TenantID: "tenant-1", EntitlementType: "FEATURE", KeyName: "max_vehicles", OverrideValue: "200", Reason: "contract v2"}))
	var rows int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM tenant_entitlement_overrides WHERE tenant_id = 'tenant-1'`).Scan(&rows))
	assert.Equal(t, 1, rows)
	require.NoError(t, db.QueryRow(`SELECT override_value, reason FROM tenant_entitlement_overrides WHERE tenant_id = 'tenant-1'`).Scan(&val, &reason))
	assert.Equal(t, "200", val)
	assert.Equal(t, "contract v2", reason)

	// Another tenant keeps its own override row (isolation).
	require.NoError(t, svc.SetOverride(ctx, SetOverrideCommand{TenantID: "tenant-2", EntitlementType: "FEATURE", KeyName: "max_vehicles", OverrideValue: "5", Reason: "trial"}))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM tenant_entitlement_overrides WHERE tenant_id = 'tenant-2'`).Scan(&rows))
	assert.Equal(t, 1, rows)
	require.NoError(t, db.QueryRow(`SELECT override_value FROM tenant_entitlement_overrides WHERE tenant_id = 'tenant-2'`).Scan(&val))
	assert.Equal(t, "5", val)
	require.NoError(t, db.QueryRow(`SELECT override_value FROM tenant_entitlement_overrides WHERE tenant_id = 'tenant-1'`).Scan(&val))
	assert.Equal(t, "200", val, "tenant-1 override must survive the tenant-2 write")
}

func TestTenantAdminService_PurgeTenantSessionsScoped(t *testing.T) {
	db := newTenantAdminTestDB(t)
	seedTenantUser(t, db, "u1", "u1@test", "tenant-1")
	seedTenantUser(t, db, "u2", "u2@test", "tenant-1")
	seedTenantUser(t, db, "u3", "u3@test", "tenant-2")
	seedTenantSession(t, db, "s1", "u1")
	seedTenantSession(t, db, "s2", "u2")
	seedTenantSession(t, db, "s3", "u3")

	purged, err := NewTenantAdminService(db).PurgeTenantSessions(context.Background(), "tenant-1")
	require.NoError(t, err)
	assert.EqualValues(t, 2, purged)

	var remaining int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id IN ('u1','u2')`).Scan(&remaining))
	assert.Zero(t, remaining)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = 'u3'`).Scan(&remaining))
	assert.Equal(t, 1, remaining, "another tenant's sessions must survive")
}

func TestTenantAdmin_NilStoreDegrades(t *testing.T) {
	ctx := context.Background()
	queries := NewTenantAdminQueries(nil)
	_, err := queries.Subscription(ctx, "tenant-1")
	require.Error(t, err)
	_, err = queries.TripQuota(ctx, "tenant-1")
	require.Error(t, err)
	_, err = queries.UserCounts(ctx)
	require.Error(t, err)
	_, err = queries.TenantSlugExists(ctx, "acme")
	require.Error(t, err)

	svc := NewTenantAdminService(nil)
	require.Error(t, svc.UpdatePlan(ctx, UpdatePlanCommand{TenantID: "tenant-1", PlanID: "GROWTH"}))
	require.Error(t, svc.ExtendTrial(ctx, "tenant-1"))
	require.Error(t, svc.SetOverride(ctx, SetOverrideCommand{TenantID: "tenant-1", EntitlementType: "FEATURE", KeyName: "k", OverrideValue: "v"}))
	_, err = svc.PurgeTenantSessions(ctx, "tenant-1")
	require.Error(t, err)
}
