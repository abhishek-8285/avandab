package application_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/entitlement/application"
	"transport-app/internal/entitlement/domain"
)

func TestLivePricing_ListAndUpdate(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	svc := application.NewService(db)
	ctx := context.Background()

	plans, err := svc.ListPlans(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, plans, "seeded catalog must list")
	var growth *domain.Plan
	for i := range plans {
		if plans[i].ID == domain.PlanGrowth {
			growth = &plans[i]
		}
	}
	require.NotNil(t, growth, "GROWTH plan must exist")
	assert.Equal(t, 1999.0, growth.MonthlyPriceINR)
	assert.NotEmpty(t, growth.Features, "plan features must parse")

	require.NoError(t, svc.UpdatePlanPrice(ctx, domain.PlanGrowth, 2499.0))
	plans, err = svc.ListPlans(ctx)
	require.NoError(t, err)
	for i := range plans {
		if plans[i].ID == domain.PlanGrowth {
			assert.Equal(t, 2499.0, plans[i].MonthlyPriceINR)
		}
	}

	assert.Error(t, svc.UpdatePlanPrice(ctx, domain.PlanGrowth, -1), "negative price rejected")
	assert.ErrorIs(t, svc.UpdatePlanPrice(ctx, domain.PlanID("NOPE"), 10), domain.ErrPlanNotFound)
}
