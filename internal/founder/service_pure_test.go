package founder

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"transport-app/internal/events"
	"transport-app/internal/founder/customer_health"
	"transport-app/internal/founder/digest"
)

func TestParseAlertPayload(t *testing.T) {
	valid := map[string]interface{}{"company_name": "Acme", "plan": "Pro", "mrr": "100"}

	tests := []struct {
		name    string
		payload any
		keys    []string
		want    map[string]string
		wantOK  bool
	}{
		{"valid full map", valid, []string{"company_name", "plan", "mrr"},
			map[string]string{"company_name": "Acme", "plan": "Pro", "mrr": "100"}, true},
		{"missing key", map[string]interface{}{"company_name": "Acme"}, []string{"company_name", "plan"},
			nil, false},
		{"wrong type", map[string]interface{}{"company_name": 123, "plan": "Pro"}, []string{"company_name", "plan"},
			nil, false},
		{"non-map payload", "not-a-map", []string{"company_name"},
			nil, false},
		{"nil payload", nil, []string{"company_name"},
			nil, false},
		{"nil map", map[string]interface{}(nil), []string{"company_name"},
			nil, false},
		{"present-but-empty is still a string", map[string]interface{}{"title": ""}, []string{"title"},
			map[string]string{"title": ""}, true},
		{"extra keys ignored", valid, []string{"plan"},
			map[string]string{"plan": "Pro"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseAlertPayload(tt.payload, tt.keys...)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNeedsChurnAlert_Boundary(t *testing.T) {
	tests := []struct {
		score int
		want  bool
	}{
		{0, true},
		{39, true},
		{40, false},
		{41, false},
		{100, false},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, needsChurnAlert(tt.score), "score %d", tt.score)
	}
}

// The calculator only yields multiples of 5, so 39/41 are pinned on
// needsChurnAlert above; here the exact reachable boundary (40 vs 35)
// is pinned through EvaluateCustomerHealth.
func TestEvaluateCustomerHealth_ScoreBoundary(t *testing.T) {
	frozen := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	// 100-30-15-15 = 40 → no alert.
	at40 := customer_health.CustomerHealthFactors{
		BookingsCount30d: 0, TripsCompleted30d: 0, IsTrial: true, DaysUntilTrialExpiry: 1,
	}
	// 100-20-30-15 = 35 → alert.
	below40 := customer_health.CustomerHealthFactors{
		LastLoginDays: 8, BookingsCount30d: 0, TripsCompleted30d: 0,
	}

	mock := &MockNotifier{}
	svc := NewFounderService(mock, &seqID{}, fixedClock{t: frozen})

	res := svc.EvaluateCustomerHealth("comp_40", "Acme", at40)
	require.Equal(t, 40, res.Score)
	require.Empty(t, mock.SentEvents, "score 40 must not alert")

	res = svc.EvaluateCustomerHealth("comp_35", "Acme", below40)
	require.Equal(t, 35, res.Score)
	require.Len(t, mock.SentEvents, 1, "score 35 must alert")
}

func TestFounderService_NilNotifier(t *testing.T) {
	frozen := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	svc := NewFounderService(nil, &seqID{}, fixedClock{t: frozen})

	// Churn-risk factors with no notifier: no panic, result still returned.
	res := svc.EvaluateCustomerHealth("comp_1", "Acme", customer_health.CustomerHealthFactors{LastLoginDays: 15})
	require.Less(t, res.Score, 40)

	// Healthy factors with no notifier: same path, no alert attempted.
	res = svc.EvaluateCustomerHealth("comp_2", "Acme", customer_health.CustomerHealthFactors{
		BookingsCount30d: 5, TripsCompleted30d: 5,
	})
	require.GreaterOrEqual(t, res.Score, 40)

	require.NoError(t, svc.SendDailyDigest(digest.DailyDigestReport{}))
}

// Malformed payloads keep the historical lenient behavior: empty strings,
// alert still sent, no panic.
func TestRegisterEventHandlers_MalformedPayloads(t *testing.T) {
	frozen := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	mock := &MockNotifier{}
	svc := NewFounderService(mock, &seqID{}, fixedClock{t: frozen})
	bus := events.NewInMemoryBus()
	svc.RegisterEventHandlers(bus)

	bus.Publish(context.Background(), events.Event{Type: "customer.activated", Payload: "not-a-map"})
	bus.Publish(context.Background(), events.Event{Type: "system.critical_failure", Payload: map[string]interface{}{}})
	bus.Publish(context.Background(), events.Event{
		Type:    "customer.onboarding_completed",
		Payload: map[string]interface{}{"company_name": 123, "activation_time": "now"},
	})

	require.Len(t, mock.SentEvents, 3)
	require.Equal(t, "", mock.SentEvents[0].Metadata["company"])
	require.Equal(t, "", mock.SentEvents[1].Title)
	require.Equal(t, "", mock.SentEvents[2].Metadata["company"])
}
