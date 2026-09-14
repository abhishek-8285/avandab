package founder

import (
	"context"
	"testing"

	"transport-app/internal/events"
	"transport-app/internal/founder/alerts"
	"transport-app/internal/founder/customer_health"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
)

type MockNotifier struct {
	SentEvents []alerts.AlertEvent
}

func (m *MockNotifier) SendAlert(event alerts.AlertEvent) error {
	m.SentEvents = append(m.SentEvents, event)
	return nil
}

func TestFounderServiceEventsAndHealth(t *testing.T) {
	mock := &MockNotifier{}
	svc := NewFounderService(mock, id.NewUUIDGenerator(), clock.NewRealClock())
	bus := events.NewInMemoryBus()

	svc.RegisterEventHandlers(bus)

	// Publish activated event
	bus.Publish(context.Background(), events.Event{
		Type: "customer.activated",
		Payload: map[string]interface{}{
			"company_name": "ABC Logistics",
			"plan":         "Business",
			"mrr":          "₹4,999/month",
		},
	})

	if len(mock.SentEvents) != 1 {
		t.Fatalf("Expected 1 sent alert event, got %d", len(mock.SentEvents))
	}

	if mock.SentEvents[0].Category != alerts.CategoryRevenue {
		t.Errorf("Expected category REVENUE, got %s", mock.SentEvents[0].Category)
	}

	// Test health evaluation triggering churn risk alert
	svc.EvaluateCustomerHealth("comp_1", "ABC Logistics", customer_health.CustomerHealthFactors{
		LastLoginDays: 15,
	})

	if len(mock.SentEvents) != 2 {
		t.Fatalf("Expected 2 total sent alert events after churn evaluation, got %d", len(mock.SentEvents))
	}

	if mock.SentEvents[1].Category != alerts.CategoryChurnRisk {
		t.Errorf("Expected category CHURN_RISK, got %s", mock.SentEvents[1].Category)
	}
}
