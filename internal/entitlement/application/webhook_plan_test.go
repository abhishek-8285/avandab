package application_test

import (
	"bytes"
	"context"
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
)

// Paid activation carrying a plan reference upgrades both status and plan.
func TestWebhook_PlanUpgradeOnActivation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	ctx := context.Background()
	providerSubID := "sub_rzp_plan_up"
	seedTestSubscription(t, db, "tenant-a", domain.PlanStarter, domain.SubTrial, providerSubID)

	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	require.NoError(t, svc.ProcessSubscriptionWebhook(ctx, application.WebhookEventPayload{
		EventID:                "evt_plan_up_001",
		EventType:              "subscription.activated",
		Provider:               "RAZORPAY",
		ProviderSubscriptionID: providerSubID,
		PlanID:                 "GROWTH",
		PayloadJSON:            `{}`,
		EventTimestamp:         t0,
		PeriodStart:            t0,
		PeriodEnd:              t0.Add(30 * 24 * time.Hour),
	}))

	sub, err := svc.GetSubscription(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, domain.SubActive, sub.Status)
	assert.Equal(t, domain.PlanGrowth, sub.PlanID)
}

// Unknown plan references never touch the plan (status still applies).
func TestWebhook_UnknownPlanIgnored(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	ctx := context.Background()
	providerSubID := "sub_rzp_plan_unknown"
	seedTestSubscription(t, db, "tenant-a", domain.PlanStarter, domain.SubTrial, providerSubID)

	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	require.NoError(t, svc.ProcessSubscriptionWebhook(ctx, application.WebhookEventPayload{
		EventID:                "evt_plan_unk_001",
		EventType:              "subscription.activated",
		Provider:               "RAZORPAY",
		ProviderSubscriptionID: providerSubID,
		PlanID:                 "MOONSHOT",
		PayloadJSON:            `{}`,
		EventTimestamp:         t0,
	}))

	sub, err := svc.GetSubscription(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, domain.SubActive, sub.Status)
	assert.Equal(t, domain.PlanStarter, sub.PlanID)
}

// End-to-end HTTP: notes[plan_id] at checkout flows to the subscription row.
func TestWebhook_HTTP_PlanRefFromNotes(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	providerSubID := "sub_rzp_http_plan"
	seedTestSubscription(t, db, "tenant-a", domain.PlanStarter, domain.SubTrial, providerSubID)

	handler := entApi.NewWebhookHandler(svc, "")
	now := time.Now()
	bodyJSON := fmt.Sprintf(`{
		"event_id": "evt_http_plan_001",
		"event": "subscription.activated",
		"created_at": %d,
		"payload": {"subscription": {"entity": {
			"id": "%s",
			"current_start": %d,
			"current_end": %d,
			"notes": {"plan_id": "GROWTH"}
		}}}
	}`, now.Unix(), providerSubID, now.Unix(), now.Add(30*24*time.Hour).Unix())

	req := httptest.NewRequest("POST", "/api/v1/billing/webhooks/razorpay", bytes.NewBufferString(bodyJSON))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	sub, err := svc.GetSubscription(context.Background(), "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, domain.SubActive, sub.Status)
	assert.Equal(t, domain.PlanGrowth, sub.PlanID)
}
