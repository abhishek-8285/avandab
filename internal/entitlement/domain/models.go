package domain

import (
	"time"

	"transport-app/internal/shared"
)

// Plan models a commercial tier in the plan catalog.
type Plan struct {
	ID              PlanID              `json:"id"`
	Name            string              `json:"name"`
	Description     string              `json:"description"`
	MonthlyPriceINR float64             `json:"monthly_price_inr"`
	Features        map[FeatureKey]bool `json:"features"`
	Quotas          map[QuotaKey]int    `json:"quotas"`
	IsActive        bool                `json:"is_active"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

// TenantSubscription models a tenant's active commercial subscription.
type TenantSubscription struct {
	ID                     string             `json:"id"`
	TenantID               shared.TenantID    `json:"tenant_id"`
	PlanID                 PlanID             `json:"plan_id"`
	Status                 SubscriptionStatus `json:"status"`
	CurrentPeriodStart     time.Time          `json:"current_period_start"`
	CurrentPeriodEnd       time.Time          `json:"current_period_end"`
	TrialEnd               *time.Time         `json:"trial_end,omitempty"`
	ProviderSubscriptionID string             `json:"provider_subscription_id,omitempty"`
	CreatedAt              time.Time          `json:"created_at"`
	UpdatedAt              time.Time          `json:"updated_at"`
}

// TenantEntitlementOverride models explicit overrides (e.g. pilot overrides).
type TenantEntitlementOverride struct {
	ID              string          `json:"id"`
	TenantID        shared.TenantID `json:"tenant_id"`
	EntitlementType string          `json:"entitlement_type"` // "FEATURE" or "QUOTA"
	KeyName         string          `json:"key_name"`
	OverrideValue   string          `json:"override_value"`
	Reason          string          `json:"reason"`
	ExpiresAt       *time.Time      `json:"expires_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

// TenantUsageMeter tracks aggregated quota consumption for a billing period.
type TenantUsageMeter struct {
	ID               string          `json:"id"`
	TenantID         shared.TenantID `json:"tenant_id"`
	QuotaKey         QuotaKey        `json:"quota_key"`
	PeriodStart      time.Time       `json:"period_start"`
	PeriodEnd        time.Time       `json:"period_end"`
	UsedQuantity     int             `json:"used_quantity"`
	ReservedQuantity int             `json:"reserved_quantity"`
	MaxQuantity      int             `json:"max_quantity"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// RemainingCapacity returns available quota without reservation conflict.
func (m *TenantUsageMeter) RemainingCapacity() int {
	available := m.MaxQuantity - (m.UsedQuantity + m.ReservedQuantity)
	if available < 0 {
		return 0
	}
	return available
}

// TenantUsageEvent models an immutable audit consumption event.
type TenantUsageEvent struct {
	ID               string          `json:"id"`
	TenantID         shared.TenantID `json:"tenant_id"`
	QuotaKey         QuotaKey        `json:"quota_key"`
	Quantity         int             `json:"quantity"`
	IdempotencyKey   string          `json:"idempotency_key"`
	SourceEntityType string          `json:"source_entity_type"`
	SourceEntityID   string          `json:"source_entity_id"`
	Timestamp        time.Time       `json:"timestamp"`
}

// QuotaStatus represents read status of a quota.
type QuotaStatus struct {
	QuotaKey    QuotaKey  `json:"quota_key"`
	Used        int       `json:"used"`
	Reserved    int       `json:"reserved"`
	Max         int       `json:"max"`
	Remaining   int       `json:"remaining"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
}
