package domain_test

import (
	"testing"
	"time"

	"transport-app/internal/entitlement/domain"
	"transport-app/internal/shared"
)

func TestTenantUsageMeter_RemainingCapacity(t *testing.T) {
	tests := []struct {
		name     string
		meter    domain.TenantUsageMeter
		expected int
	}{
		{
			name: "full capacity remaining",
			meter: domain.TenantUsageMeter{
				MaxQuantity:      100,
				UsedQuantity:     0,
				ReservedQuantity: 0,
			},
			expected: 100,
		},
		{
			name: "partially used and reserved",
			meter: domain.TenantUsageMeter{
				MaxQuantity:      100,
				UsedQuantity:     40,
				ReservedQuantity: 25,
			},
			expected: 35,
		},
		{
			name: "exactly exhausted",
			meter: domain.TenantUsageMeter{
				MaxQuantity:      50,
				UsedQuantity:     30,
				ReservedQuantity: 20,
			},
			expected: 0,
		},
		{
			name: "over quota returns zero never negative",
			meter: domain.TenantUsageMeter{
				MaxQuantity:      50,
				UsedQuantity:     40,
				ReservedQuantity: 20,
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.meter.RemainingCapacity()
			if got != tt.expected {
				t.Fatalf("RemainingCapacity() = %d, expected %d", got, tt.expected)
			}
		})
	}
}

func TestDomainModels_Instantiations(t *testing.T) {
	now := time.Now()
	tenantID := shared.TenantID("tenant-test-123")

	plan := domain.Plan{
		ID:              domain.PlanEnterprise,
		Name:            "Enterprise Tier",
		Description:     "Dedicated scale tier",
		MonthlyPriceINR: 49999,
		Features: map[domain.FeatureKey]bool{
			domain.FeatureAPIAccess:    true,
			domain.FeatureDedicatedSLA: true,
		},
		Quotas: map[domain.QuotaKey]int{
			domain.QuotaMaxVehicles:      500,
			domain.QuotaMaxTripsPerMonth: 10000,
		},
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if plan.ID != domain.PlanEnterprise || !plan.Features[domain.FeatureAPIAccess] {
		t.Fatalf("plan fields not initialized correctly")
	}

	sub := domain.TenantSubscription{
		ID:                 "sub-1",
		TenantID:           tenantID,
		PlanID:             domain.PlanScale,
		Status:             domain.SubActive,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(0, 1, 0),
	}

	if sub.TenantID != tenantID || sub.Status != domain.SubActive {
		t.Fatalf("subscription fields not initialized correctly")
	}

	event := domain.TenantUsageEvent{
		ID:               "evt-1",
		TenantID:         tenantID,
		QuotaKey:         domain.QuotaSMSAlertsPerMonth,
		Quantity:         1,
		IdempotencyKey:   "idem-1",
		SourceEntityType: "booking",
		SourceEntityID:   "bk-1",
		Timestamp:        now,
	}

	if event.Quantity != 1 || event.QuotaKey != domain.QuotaSMSAlertsPerMonth {
		t.Fatalf("usage event fields mismatch")
	}
}
