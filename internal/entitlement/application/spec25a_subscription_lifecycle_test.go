package application_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/entitlement/application"
	"transport-app/internal/entitlement/domain"
	entApi "transport-app/internal/entitlement/presentation/api"
	"transport-app/internal/shared"
)

func seedTestSubscription(t *testing.T, db *sql.DB, tenantID shared.TenantID, planID domain.PlanID, status domain.SubscriptionStatus, providerSubID string) *domain.TenantSubscription {
	now := time.Now().UTC()
	start := now.Add(-24 * time.Hour)
	end := now.Add(30 * 24 * time.Hour)

	_, err := db.Exec(`
		INSERT INTO tenant_subscriptions (id, tenant_id, plan_id, status, current_period_start, current_period_end, provider_subscription_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id) DO UPDATE SET
			plan_id = excluded.plan_id,
			status = excluded.status,
			provider_subscription_id = excluded.provider_subscription_id,
			updated_at = CURRENT_TIMESTAMP
	`, "sub_"+string(tenantID), string(tenantID), string(planID), string(status), start.Format(time.RFC3339), end.Format(time.RFC3339), providerSubID, now.Format(time.RFC3339), now.Format(time.RFC3339))
	require.NoError(t, err)

	svc := application.NewService(db)
	sub, err := svc.GetSubscription(context.Background(), tenantID)
	require.NoError(t, err)
	return sub
}

func TestSpec25A_SubscriptionWebhook_FullLifecycle_ReplayProtection(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	ctx := context.Background()

	providerSubID := "sub_rzp_test_12345"
	seedTestSubscription(t, db, "tenant-a", domain.PlanGrowth, domain.SubTrial, providerSubID)

	t.Run("1. subscription.activated 5x replay -> exact 1 transition", func(t *testing.T) {
		t0 := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
		for i := 0; i < 5; i++ {
			err := svc.ProcessSubscriptionWebhook(ctx, application.WebhookEventPayload{
				EventID:                "evt_act_001",
				EventType:              "subscription.activated",
				Provider:               "RAZORPAY",
				ProviderSubscriptionID: providerSubID,
				PayloadJSON:            `{"event":"subscription.activated"}`,
				EventTimestamp:         t0,
			})
			require.NoError(t, err)
		}

		sub, err := svc.GetSubscription(ctx, "tenant-a")
		require.NoError(t, err)
		assert.Equal(t, domain.SubActive, sub.Status)

		// Exactly 1 webhook event recorded
		var count int
		_ = db.QueryRow(`SELECT count(*) FROM subscription_webhook_events WHERE event_id = 'evt_act_001'`).Scan(&count)
		assert.Equal(t, 1, count, "5x replay must record exact 1 event row")
	})

	t.Run("2. payment.captured 5x replay -> exact 1 transition & period update", func(t *testing.T) {
		t1 := time.Date(2026, 8, 30, 10, 5, 0, 0, time.UTC)
		newStart := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
		newEnd := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

		for i := 0; i < 5; i++ {
			err := svc.ProcessSubscriptionWebhook(ctx, application.WebhookEventPayload{
				EventID:                "evt_pay_002",
				EventType:              "payment.captured",
				Provider:               "RAZORPAY",
				ProviderSubscriptionID: providerSubID,
				PayloadJSON:            `{"event":"payment.captured"}`,
				EventTimestamp:         t1,
				PeriodStart:            newStart,
				PeriodEnd:              newEnd,
			})
			require.NoError(t, err)
		}

		sub, err := svc.GetSubscription(ctx, "tenant-a")
		require.NoError(t, err)
		assert.Equal(t, domain.SubActive, sub.Status)
		assert.Equal(t, newStart.UTC(), sub.CurrentPeriodStart.UTC())
		assert.Equal(t, newEnd.UTC(), sub.CurrentPeriodEnd.UTC())
	})

	t.Run("3. payment.failed -> transitions to PAST_DUE (7-day grace period)", func(t *testing.T) {
		t2 := time.Date(2026, 8, 30, 10, 10, 0, 0, time.UTC)
		err := svc.ProcessSubscriptionWebhook(ctx, application.WebhookEventPayload{
			EventID:                "evt_fail_003",
			EventType:              "payment.failed",
			Provider:               "RAZORPAY",
			ProviderSubscriptionID: providerSubID,
			PayloadJSON:            `{"event":"payment.failed"}`,
			EventTimestamp:         t2,
		})
		require.NoError(t, err)

		sub, err := svc.GetSubscription(ctx, "tenant-a")
		require.NoError(t, err)
		assert.Equal(t, domain.SubPastDue, sub.Status)

		// PAST_DUE allows grace operations
		allowed, err := svc.CanExecuteOperation(ctx, "tenant-a", domain.OpCreateBooking, "")
		require.NoError(t, err)
		assert.True(t, allowed, "PAST_DUE allows operations during grace period")
	})

	t.Run("4. subscription.paused -> transitions to READ_ONLY", func(t *testing.T) {
		t3 := time.Date(2026, 8, 30, 10, 15, 0, 0, time.UTC)
		err := svc.ProcessSubscriptionWebhook(ctx, application.WebhookEventPayload{
			EventID:                "evt_pause_004",
			EventType:              "subscription.paused",
			Provider:               "RAZORPAY",
			ProviderSubscriptionID: providerSubID,
			PayloadJSON:            `{"event":"subscription.paused"}`,
			EventTimestamp:         t3,
		})
		require.NoError(t, err)

		sub, err := svc.GetSubscription(ctx, "tenant-a")
		require.NoError(t, err)
		assert.Equal(t, domain.SubReadOnly, sub.Status)

		// Ingress blocked, in-flight allowed
		ingressAllowed, _ := svc.CanExecuteOperation(ctx, "tenant-a", domain.OpCreateBooking, "")
		assert.False(t, ingressAllowed)

		inFlightAllowed, _ := svc.CanExecuteOperation(ctx, "tenant-a", domain.OpCompleteTrip, "trip_1")
		assert.True(t, inFlightAllowed)
	})

	t.Run("5. subscription.resumed -> transitions back to ACTIVE", func(t *testing.T) {
		t4 := time.Date(2026, 8, 30, 10, 20, 0, 0, time.UTC)
		err := svc.ProcessSubscriptionWebhook(ctx, application.WebhookEventPayload{
			EventID:                "evt_resume_005",
			EventType:              "subscription.resumed",
			Provider:               "RAZORPAY",
			ProviderSubscriptionID: providerSubID,
			PayloadJSON:            `{"event":"subscription.resumed"}`,
			EventTimestamp:         t4,
		})
		require.NoError(t, err)

		sub, err := svc.GetSubscription(ctx, "tenant-a")
		require.NoError(t, err)
		assert.Equal(t, domain.SubActive, sub.Status)
	})

	t.Run("6. subscription.cancelled -> transitions to READ_ONLY", func(t *testing.T) {
		t5 := time.Date(2026, 8, 30, 10, 25, 0, 0, time.UTC)
		err := svc.ProcessSubscriptionWebhook(ctx, application.WebhookEventPayload{
			EventID:                "evt_cancel_006",
			EventType:              "subscription.cancelled",
			Provider:               "RAZORPAY",
			ProviderSubscriptionID: providerSubID,
			PayloadJSON:            `{"event":"subscription.cancelled"}`,
			EventTimestamp:         t5,
		})
		require.NoError(t, err)

		sub, err := svc.GetSubscription(ctx, "tenant-a")
		require.NoError(t, err)
		assert.Equal(t, domain.SubReadOnly, sub.Status)
	})
}

func TestSpec25A_OutOfOrderWebhookProtection(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	ctx := context.Background()

	providerSubID := "sub_rzp_ooo_999"
	seedTestSubscription(t, db, "tenant-a", domain.PlanGrowth, domain.SubTrial, providerSubID)

	// Newer event arrives first: subscription.activated at 12:00
	tNewer := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	err := svc.ProcessSubscriptionWebhook(ctx, application.WebhookEventPayload{
		EventID:                "evt_newer_act",
		EventType:              "subscription.activated",
		Provider:               "RAZORPAY",
		ProviderSubscriptionID: providerSubID,
		PayloadJSON:            `{"event":"subscription.activated"}`,
		EventTimestamp:         tNewer,
	})
	require.NoError(t, err)

	sub, err := svc.GetSubscription(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, domain.SubActive, sub.Status)

	// Older event arrives late: payment.failed at 11:00
	tOlder := time.Date(2026, 8, 30, 11, 0, 0, 0, time.UTC)
	err = svc.ProcessSubscriptionWebhook(ctx, application.WebhookEventPayload{
		EventID:                "evt_older_fail",
		EventType:              "payment.failed",
		Provider:               "RAZORPAY",
		ProviderSubscriptionID: providerSubID,
		PayloadJSON:            `{"event":"payment.failed"}`,
		EventTimestamp:         tOlder,
	})
	require.NoError(t, err)

	// State MUST remain ACTIVE (older failure event cannot overwrite newer active status)
	subAfter, err := svc.GetSubscription(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, domain.SubActive, subAfter.Status, "Older out-of-order event must not downgrade subscription status")

	// Verify older event recorded as IGNORED_OUT_OF_ORDER
	var oooStatus string
	_ = db.QueryRow(`SELECT status FROM subscription_webhook_events WHERE event_id = 'evt_older_fail'`).Scan(&oooStatus)
	assert.Equal(t, "IGNORED_OUT_OF_ORDER", oooStatus)
}

func TestSpec25A_CrossTenantWebhookIsolation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	ctx := context.Background()

	subA := "sub_rzp_tenant_a"
	subB := "sub_rzp_tenant_b"
	seedTestSubscription(t, db, "tenant-a", domain.PlanGrowth, domain.SubActive, subA)
	seedTestSubscription(t, db, "tenant-b", domain.PlanGrowth, domain.SubActive, subB)

	// Cancellation webhook for Tenant A
	err := svc.ProcessSubscriptionWebhook(ctx, application.WebhookEventPayload{
		EventID:                "evt_cancel_a",
		EventType:              "subscription.cancelled",
		Provider:               "RAZORPAY",
		ProviderSubscriptionID: subA,
		PayloadJSON:            `{"event":"subscription.cancelled"}`,
		EventTimestamp:         time.Now().UTC(),
	})
	require.NoError(t, err)

	// Tenant A is now READ_ONLY
	stateA, err := svc.GetSubscription(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, domain.SubReadOnly, stateA.Status)

	// Tenant B remains ACTIVE
	stateB, err := svc.GetSubscription(ctx, "tenant-b")
	require.NoError(t, err)
	assert.Equal(t, domain.SubActive, stateB.Status, "Tenant B subscription must not be affected by Tenant A webhook")
}

func TestSpec25A_HTTPWebhookHandler_SignatureVerification(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	providerSubID := "sub_rzp_http_test"
	seedTestSubscription(t, db, "tenant-a", domain.PlanGrowth, domain.SubTrial, providerSubID)

	secret := "test_webhook_secret_xyz123"
	handler := entApi.NewWebhookHandler(svc, secret)

	bodyJSON := fmt.Sprintf(`{
		"event_id": "evt_http_001",
		"event": "subscription.activated",
		"created_at": %d,
		"payload": {
			"subscription": {
				"entity": {
					"id": "%s",
					"current_start": %d,
					"current_end": %d
				}
			}
		}
	}`, time.Now().Unix(), providerSubID, time.Now().Unix(), time.Now().Add(30*24*time.Hour).Unix())

	// Compute valid HMAC
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(bodyJSON))
	validSig := hex.EncodeToString(mac.Sum(nil))

	t.Run("Invalid signature rejected 401", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/billing/webhooks/razorpay", bytes.NewBufferString(bodyJSON))
		req.Header.Set("X-Razorpay-Signature", "invalid_signature")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("Valid signature accepted 200 and processes subscription", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/billing/webhooks/razorpay", bytes.NewBufferString(bodyJSON))
		req.Header.Set("X-Razorpay-Signature", validSig)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		sub, err := svc.GetSubscription(context.Background(), "tenant-a")
		require.NoError(t, err)
		assert.Equal(t, domain.SubActive, sub.Status)
	})
}
