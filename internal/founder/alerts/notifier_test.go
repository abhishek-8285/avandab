package alerts_test

import (
	"strings"
	"testing"
	"time"

	"transport-app/internal/founder/alerts"
)

func TestTelegramFormatter_Format(t *testing.T) {
	formatter := alerts.NewTelegramFormatter()

	t.Run("Revenue Event", func(t *testing.T) {
		ev := alerts.AlertEvent{
			ID:       "rev-1",
			Category: alerts.CategoryRevenue,
			Priority: alerts.PriorityHigh,
			Title:    "New Subscription",
			Metadata: map[string]interface{}{
				"company": "Acme Logistics",
				"plan":    "Enterprise",
				"mrr":     "₹50,000",
			},
			Timestamp: time.Now(),
		}
		got := formatter.Format(ev)
		if !strings.Contains(got, "💰") || !strings.Contains(got, "Acme Logistics") || !strings.Contains(got, "₹50,000") {
			t.Fatalf("unexpected formatted revenue output: %s", got)
		}
	})

	t.Run("Churn Risk Event", func(t *testing.T) {
		ev := alerts.AlertEvent{
			ID:       "churn-1",
			Category: alerts.CategoryChurnRisk,
			Priority: alerts.PriorityHigh,
			Title:    "Tenant Inactive",
			Metadata: map[string]interface{}{
				"company": "FastFreight",
				"score":   "30",
				"reason":  "No bookings this week",
				"action":  "Check in via CSM",
			},
			Timestamp: time.Now(),
		}
		got := formatter.Format(ev)
		if !strings.Contains(got, "⚠️") || !strings.Contains(got, "FastFreight") || !strings.Contains(got, "30%") {
			t.Fatalf("unexpected formatted churn output: %s", got)
		}
	})

	t.Run("Activation Event", func(t *testing.T) {
		ev := alerts.AlertEvent{
			ID:       "act-1",
			Category: alerts.CategoryActivation,
			Priority: alerts.PriorityMedium,
			Title:    "Tenant Fully Activated",
			Metadata: map[string]interface{}{
				"company":         "Metro Trans",
				"activation_time": "2 hours",
			},
			Timestamp: time.Now(),
		}
		got := formatter.Format(ev)
		if !strings.Contains(got, "🎉") || !strings.Contains(got, "Metro Trans") || !strings.Contains(got, "2 hours") {
			t.Fatalf("unexpected formatted activation output: %s", got)
		}
	})

	t.Run("System Event", func(t *testing.T) {
		ev := alerts.AlertEvent{
			ID:        "sys-1",
			Category:  alerts.CategorySystem,
			Priority:  alerts.PriorityCritical,
			Title:     "Database High Latency",
			Summary:   "p99 latency exceeded 500ms",
			Timestamp: time.Now(),
		}
		got := formatter.Format(ev)
		if !strings.Contains(got, "🚨") || !strings.Contains(got, "CRITICAL SYSTEM ALERT") || !strings.Contains(got, "Database High Latency") {
			t.Fatalf("unexpected formatted system output: %s", got)
		}
	})

	t.Run("Generic Event", func(t *testing.T) {
		ev := alerts.AlertEvent{
			ID:        "gen-1",
			Category:  alerts.CategoryProductUsage,
			Priority:  alerts.PriorityLow,
			Title:     "Report Downloaded",
			Summary:   "Quarterly audit downloaded",
			Timestamp: time.Now(),
		}
		got := formatter.Format(ev)
		if !strings.Contains(got, "🔔") || !strings.Contains(got, "Report Downloaded") {
			t.Fatalf("unexpected formatted generic output: %s", got)
		}
	})
}
