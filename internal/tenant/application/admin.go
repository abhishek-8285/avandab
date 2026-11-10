package application

// Platform tenant-administration surface, extracted from
// internal/handlers/tenants.go (handlers-end-state ADR, Phase 2). This is the
// first package of the new tenant slice (there was no internal/tenant* domain
// package before; tenants.go held the whole super-admin console): the platform
// reads (commercial subscription, trip quota meter, grouped user counts, slug
// conflict probe) and the best-effort tenant-admin writes (plan upsert, trial
// extension, entitlement override upsert, suspended-tenant session purge).
//
// The legacy SQL, tenant scoping and best-effort error semantics are preserved
// verbatim. The handler keeps RBAC gating, form validation, the bootstrap-tenant
// suspension guard, audit logging and all rendering; SQL errors that the legacy
// handler swallowed stay swallowed by the caller (the errors are returned only
// for tests and observability).

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// errStoreUnavailable mirrors the legacy handlers' nil-App.DB degradation: the
// console skipped these queries entirely when no database was wired.
var errStoreUnavailable = errors.New("tenant admin store unavailable")

// TenantSubscription is one tenant's commercial subscription joined with its
// plan, as rendered on the platform commercial list.
type TenantSubscription struct {
	PlanID       string
	Status       string
	MonthlyPrice float64
	PeriodEnd    string
	TrialEnd     string
}

// TripQuota is the latest max_trips_per_month usage meter for one tenant.
type TripQuota struct {
	Used int
	Max  int
}

// TenantAdminQueries is the read-model facade for the platform tenant console.
// It is stateless and cheap to construct per request.
type TenantAdminQueries struct {
	db *sql.DB
}

// NewTenantAdminQueries wires the read model against the database.
func NewTenantAdminQueries(db *sql.DB) *TenantAdminQueries {
	return &TenantAdminQueries{db: db}
}

// Subscription returns the tenant's subscription + plan pricing. Legacy
// semantics (List): any lookup/scan error means "no subscription row", so the
// caller keeps the STARTER/TRIAL defaults and counts the tenant as a trial.
func (q *TenantAdminQueries) Subscription(ctx context.Context, tenantID string) (TenantSubscription, error) {
	if q.db == nil {
		return TenantSubscription{}, errStoreUnavailable
	}
	var sub TenantSubscription
	var periodEnd, trialEnd sql.NullString
	err := q.db.QueryRowContext(ctx, `
				SELECT ts.plan_id, ts.status, sp.monthly_price_inr, ts.current_period_end, ts.trial_end
				FROM tenant_subscriptions ts
				JOIN subscription_plans sp ON sp.id = ts.plan_id
				WHERE ts.tenant_id = $1
			`, tenantID).Scan(&sub.PlanID, &sub.Status, &sub.MonthlyPrice, &periodEnd, &trialEnd)
	if err != nil {
		return TenantSubscription{}, err
	}
	if periodEnd.Valid {
		sub.PeriodEnd = periodEnd.String
	}
	if trialEnd.Valid {
		sub.TrialEnd = trialEnd.String
	}
	return sub, nil
}

// TripQuota returns the tenant's latest max_trips_per_month usage meter. Legacy
// semantics (List): a lookup/scan error means "no meter" and the quota section
// is left blank; the caller only renders quotas when Max > 0.
func (q *TenantAdminQueries) TripQuota(ctx context.Context, tenantID string) (TripQuota, error) {
	if q.db == nil {
		return TripQuota{}, errStoreUnavailable
	}
	var quota TripQuota
	err := q.db.QueryRowContext(ctx, `
				SELECT used_quantity, max_quantity
				FROM tenant_usage_meters
				WHERE tenant_id = $1 AND quota_key = 'max_trips_per_month'
				ORDER BY updated_at DESC LIMIT 1
			`, tenantID).Scan(&quota.Used, &quota.Max)
	if err != nil {
		return TripQuota{}, err
	}
	return quota, nil
}

// UserCounts returns per-tenant user totals from one grouped query (legacy
// applyUserCounts). Counts are cosmetic: unreadable rows are skipped, and a
// query/rows error is returned for the caller to log and degrade to zeros.
func (q *TenantAdminQueries) UserCounts(ctx context.Context) (map[string]int64, error) {
	counts := map[string]int64{}
	if q.db == nil {
		return counts, errStoreUnavailable
	}
	rows, err := q.db.QueryContext(ctx, `SELECT tenant_id, COUNT(*) FROM users GROUP BY tenant_id`)
	if err != nil {
		return counts, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var tenantID string
		var n int64
		if err := rows.Scan(&tenantID, &n); err != nil {
			continue
		}
		counts[tenantID] = n
	}
	if err := rows.Err(); err != nil {
		return counts, err
	}
	return counts, nil
}

// TenantSlugExists reports whether a tenant id or slug already uses the value.
// Provisioned orgs use the slug as their tenant id, so one probe covers both
// the primary key and the UNIQUE(slug) index (legacy Create SQL verbatim).
func (q *TenantAdminQueries) TenantSlugExists(ctx context.Context, slug string) (bool, error) {
	if q.db == nil {
		return false, errStoreUnavailable
	}
	var existing int
	if err := q.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM tenants WHERE id = $1 OR slug = $2`, slug, slug).Scan(&existing); err != nil {
		return false, err
	}
	return existing > 0, nil
}

// TenantAdminService applies the platform console's best-effort commercial
// writes. The legacy handlers ignored every SQL error on these paths (the
// operator was always audited and redirected), so callers may ignore the
// returned errors; they exist for tests and observability.
type TenantAdminService struct {
	db *sql.DB
}

// NewTenantAdminService wires the write side against the database.
func NewTenantAdminService(db *sql.DB) *TenantAdminService {
	return &TenantAdminService{db: db}
}

// UpdatePlanCommand is one direct plan change from Platform Admin.
type UpdatePlanCommand struct {
	TenantID               string
	PlanID                 string
	ProviderSubscriptionID string
}

// UpdatePlan upserts the tenant subscription as ACTIVE with a fresh 30-day
// period (legacy plan_update SQL verbatim). An empty provider_subscription_id
// keeps the previously linked Razorpay subscription (COALESCE/NULLIF), so
// billing webhooks still resolve paid events after a plan-only change.
func (s *TenantAdminService) UpdatePlan(ctx context.Context, cmd UpdatePlanCommand) error {
	if s.db == nil {
		return errStoreUnavailable
	}
	now := time.Now().UTC()
	end := now.Add(30 * 24 * time.Hour)
	_, err := s.db.ExecContext(ctx, `
			INSERT INTO tenant_subscriptions (id, tenant_id, plan_id, status, current_period_start, current_period_end, provider_subscription_id, created_at, updated_at)
			VALUES ($1, $2, $3, 'ACTIVE', $4, $5, $6, $7, $8)
			ON CONFLICT (tenant_id) DO UPDATE SET
				plan_id = excluded.plan_id,
				status = 'ACTIVE',
				current_period_start = excluded.current_period_start,
				current_period_end = excluded.current_period_end,
				provider_subscription_id = COALESCE(NULLIF(excluded.provider_subscription_id, ''), tenant_subscriptions.provider_subscription_id),
				updated_at = CURRENT_TIMESTAMP
		`, "sub_"+cmd.TenantID, cmd.TenantID, cmd.PlanID, now.Format(time.RFC3339), end.Format(time.RFC3339), cmd.ProviderSubscriptionID, now.Format(time.RFC3339), now.Format(time.RFC3339))
	return err
}

// ExtendTrial grants 14 more trial days and flips the subscription back to
// TRIAL (legacy extend_trial SQL verbatim, tenant-scoped by tenant_id).
func (s *TenantAdminService) ExtendTrial(ctx context.Context, tenantID string) error {
	if s.db == nil {
		return errStoreUnavailable
	}
	now := time.Now().UTC()
	newTrial := now.Add(14 * 24 * time.Hour)
	_, err := s.db.ExecContext(ctx, `
			UPDATE tenant_subscriptions
			SET trial_end = $1, status = 'TRIAL', updated_at = CURRENT_TIMESTAMP
			WHERE tenant_id = $2
		`, newTrial.Format(time.RFC3339), tenantID)
	return err
}

// SetOverrideCommand is one privileged feature/quota override from Platform Admin.
type SetOverrideCommand struct {
	TenantID        string
	EntitlementType string
	KeyName         string
	OverrideValue   string
	Reason          string
}

// SetOverride upserts the tenant entitlement override, keyed by
// (tenant_id, entitlement_type, key_name) (legacy override SQL verbatim).
func (s *TenantAdminService) SetOverride(ctx context.Context, cmd SetOverrideCommand) error {
	if s.db == nil {
		return errStoreUnavailable
	}
	recID := "ovr_" + cmd.TenantID + "_" + cmd.KeyName
	_, err := s.db.ExecContext(ctx, `
			INSERT INTO tenant_entitlement_overrides (id, tenant_id, entitlement_type, key_name, override_value, reason)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (tenant_id, entitlement_type, key_name) DO UPDATE SET
				override_value = excluded.override_value,
				reason = excluded.reason
		`, recID, cmd.TenantID, cmd.EntitlementType, cmd.KeyName, cmd.OverrideValue, cmd.Reason)
	return err
}

// PurgeTenantSessions deletes every stored session belonging to the tenant's
// users (legacy suspend SQL verbatim: users are matched by tenant_id, sessions
// by user_id). Returns the number of purged sessions; a RowsAffected error is
// treated as zero (legacy only logged positive purge counts).
func (s *TenantAdminService) PurgeTenantSessions(ctx context.Context, tenantID string) (int64, error) {
	if s.db == nil {
		return 0, errStoreUnavailable
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE tenant_id = $1)`, tenantID)
	if err != nil {
		return 0, err
	}
	n, nerr := res.RowsAffected()
	if nerr != nil {
		return 0, nil
	}
	return n, nil
}
