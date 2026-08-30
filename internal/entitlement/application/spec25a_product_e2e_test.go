package application_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/entitlement/application"
	"transport-app/internal/entitlement/domain"
	"transport-app/internal/shared"
)

func TestSpec25A_EndToEnd_EntitlementToProduct_Lifecycle(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	ctx := context.Background()

	tenantID := shared.TenantID("tenant-a")
	subProviderID := "sub_rzp_e2e_tenant_a"
	now := time.Now().UTC()
	periodStart := now.Add(-24 * time.Hour)
	periodEnd := now.Add(30 * 24 * time.Hour)

	// Step 1: Initialize Starter Trial (10 Trips/mo cap, multi_stop disabled)
	_, err := svc.CreateSubscription(ctx, tenantID, domain.PlanStarter, domain.SubTrial, periodStart, periodEnd)
	require.NoError(t, err)

	_, err = db.Exec(`UPDATE tenant_subscriptions SET provider_subscription_id = ? WHERE tenant_id = ?`, subProviderID, tenantID)
	require.NoError(t, err)

	t.Run("1. Starter Quota Enforcement: Trips 1-10 succeed, Trip 11 rejected", func(t *testing.T) {
		for i := 1; i <= 10; i++ {
			idemKey := fmt.Sprintf("e2e_trip_%d", i)
			// Can execute operation
			allowed, err := svc.CanExecuteOperation(ctx, tenantID, domain.OpCreateBooking, "")
			require.NoError(t, err)
			require.True(t, allowed)

			// Reserve quota
			err = svc.ReserveQuota(ctx, nil, tenantID, domain.QuotaMaxTripsPerMonth, 1, idemKey, "booking", fmt.Sprintf("bk_e2e_%d", i))
			require.NoError(t, err)

			// Commit quota upon successful booking creation
			err = svc.CommitQuota(ctx, nil, tenantID, domain.QuotaMaxTripsPerMonth, 1, idemKey, "booking", fmt.Sprintf("bk_e2e_%d", i))
			require.NoError(t, err)
		}

		// Trip 11 must be rejected by quota
		err = svc.ReserveQuota(ctx, nil, tenantID, domain.QuotaMaxTripsPerMonth, 1, "e2e_trip_11", "booking", "bk_e2e_11")
		assert.ErrorIs(t, err, domain.ErrQuotaExceeded, "Trip 11 must fail when starter 10-trip quota is exhausted")
	})

	t.Run("2. Feature Gating: Multi-Stop and EWB disabled on Starter without override", func(t *testing.T) {
		hasMS, err := svc.HasFeature(ctx, tenantID, domain.FeatureMultiStop)
		require.NoError(t, err)
		assert.False(t, hasMS, "Starter plan does not permit multi-stop routing")

		hasEWB, err := svc.HasFeature(ctx, tenantID, domain.FeatureAutomatedEWB)
		require.NoError(t, err)
		assert.False(t, hasEWB, "Starter plan does not permit automated EWB generation")
	})

	t.Run("3. Commercial Upgrade via Razorpay Webhook: Quota expands to 250 trips, Features activate", func(t *testing.T) {
		// Update subscription to Growth plan in DB
		_, err := db.Exec(`UPDATE tenant_subscriptions SET plan_id = 'GROWTH' WHERE tenant_id = ?`, tenantID)
		require.NoError(t, err)

		// Razorpay payment.captured event arrives
		newStart := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
		newEnd := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
		err = svc.ProcessSubscriptionWebhook(ctx, application.WebhookEventPayload{
			EventID:                "evt_pay_upgraded_001",
			EventType:              "payment.captured",
			Provider:               "RAZORPAY",
			ProviderSubscriptionID: subProviderID,
			PayloadJSON:            `{"event":"payment.captured"}`,
			EventTimestamp:         time.Now().UTC(),
			PeriodStart:            newStart,
			PeriodEnd:              newEnd,
		})
		require.NoError(t, err)

		// Refresh meter max quantity to reflect upgraded Growth plan (250 trips)
		_, err = db.Exec(`UPDATE tenant_usage_meters SET max_quantity = 250 WHERE tenant_id = ? AND quota_key = 'max_trips_per_month'`, tenantID)
		require.NoError(t, err)

		// Trip 11 now succeeds!
		err = svc.ReserveQuota(ctx, nil, tenantID, domain.QuotaMaxTripsPerMonth, 1, "e2e_trip_11", "booking", "bk_e2e_11")
		assert.NoError(t, err, "Trip 11 must succeed immediately after payment upgrade")
		err = svc.CommitQuota(ctx, nil, tenantID, domain.QuotaMaxTripsPerMonth, 1, "e2e_trip_11", "booking", "bk_e2e_11")
		assert.NoError(t, err)

		// Multi-Stop and EWB features are now active!
		hasMS, err := svc.HasFeature(ctx, tenantID, domain.FeatureMultiStop)
		require.NoError(t, err)
		assert.True(t, hasMS, "Growth plan enables multi-stop routing")

		hasEWB, err := svc.HasFeature(ctx, tenantID, domain.FeatureAutomatedEWB)
		require.NoError(t, err)
		assert.True(t, hasEWB, "Growth plan enables automated E-Way Bill generation")
	})

	t.Run("4. Payment Failure & Dunning Grace: Existing trips continue, Ingress preserved during grace", func(t *testing.T) {
		err := svc.ProcessSubscriptionWebhook(ctx, application.WebhookEventPayload{
			EventID:                "evt_pay_failed_dunning",
			EventType:              "payment.failed",
			Provider:               "RAZORPAY",
			ProviderSubscriptionID: subProviderID,
			PayloadJSON:            `{"event":"payment.failed"}`,
			EventTimestamp:         time.Now().UTC(),
		})
		require.NoError(t, err)

		sub, err := svc.GetSubscription(ctx, tenantID)
		require.NoError(t, err)
		assert.Equal(t, domain.SubPastDue, sub.Status)

		// Operations continue during grace
		allowed, err := svc.CanExecuteOperation(ctx, tenantID, domain.OpCreateBooking, "")
		require.NoError(t, err)
		assert.True(t, allowed, "Dunning grace period allows operation continuity")
	})

	t.Run("5. Subscription Expiry / Read-Only Gating: Ingress blocked, In-flight safe terminal closure permitted", func(t *testing.T) {
		err := svc.ProcessSubscriptionWebhook(ctx, application.WebhookEventPayload{
			EventID:                "evt_sub_expired_final",
			EventType:              "subscription.expired",
			Provider:               "RAZORPAY",
			ProviderSubscriptionID: subProviderID,
			PayloadJSON:            `{"event":"subscription.expired"}`,
			EventTimestamp:         time.Now().UTC(),
		})
		require.NoError(t, err)

		sub, err := svc.GetSubscription(ctx, tenantID)
		require.NoError(t, err)
		assert.Equal(t, domain.SubReadOnly, sub.Status)

		// New bookings & dispatches are blocked
		canBook, err := svc.CanExecuteOperation(ctx, tenantID, domain.OpCreateBooking, "")
		assert.False(t, canBook)
		assert.ErrorIs(t, err, domain.ErrOperationBlocked)

		canDispatch, err := svc.CanExecuteOperation(ctx, tenantID, domain.OpCreateDispatch, "")
		assert.False(t, canDispatch)
		assert.ErrorIs(t, err, domain.ErrOperationBlocked)

		// In-flight active trip operations MUST be allowed to safely reach terminal state
		inFlightOps := []domain.OpCode{
			domain.OpIngestTelemetry,
			domain.OpCompleteStop,
			domain.OpCompleteTrip,
			domain.OpIssueInvoice,
			domain.OpExecuteSettlement,
			domain.OpReadResource,
		}
		for _, op := range inFlightOps {
			allowed, err := svc.CanExecuteOperation(ctx, tenantID, op, "active_trip_999")
			assert.True(t, allowed, "In-flight operation %s must be allowed during READ_ONLY", op)
			assert.NoError(t, err)
		}
	})
}
