package founder

import (
	"context"
	"fmt"

	"transport-app/internal/events"
	"transport-app/internal/founder/alerts"
	"transport-app/internal/founder/customer_health"
	"transport-app/internal/founder/digest"
	"transport-app/internal/shared/ports"
)

type FounderService struct {
	notifier Notifier
	idGen    ports.IDGenerator
	clock    ports.Clock
}

type Notifier interface {
	SendAlert(event alerts.AlertEvent) error
}

func NewFounderService(notifier Notifier, idGen ports.IDGenerator, clock ports.Clock) *FounderService {
	return &FounderService{
		notifier: notifier,
		idGen:    idGen,
		clock:    clock,
	}
}

// newID mints rev_/sys_/act_/churn_/digest_ ids from the injected IDGenerator —
// never from time.Now().UnixNano(), which repeats inside one clock tick on
// coarse clocks (Windows ~0.5ms steps).
func (s *FounderService) newID(prefix string) string {
	return prefix + "_" + s.idGen.GenerateUUID()
}

// parseAlertPayload extracts required string fields from an event payload.
// ok=false when payload is not a map or any key is missing/wrong-typed.
// Callers index the result directly: indexing a nil map yields "" per key,
// preserving the historical lenient behavior (empty strings, still alert).
func parseAlertPayload(payload any, keys ...string) (map[string]string, bool) {
	m, ok := payload.(map[string]interface{})
	if !ok || m == nil {
		return nil, false
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		v, ok := m[k].(string)
		if !ok {
			return nil, false
		}
		out[k] = v
	}
	return out, true
}

// needsChurnAlert pins the Score<40 boundary in one testable place.
func needsChurnAlert(score int) bool { return score < 40 }

// RegisterEventHandlers subscribes the founder alert service to relevant domain events on the event bus
func (s *FounderService) RegisterEventHandlers(bus events.EventBus) {
	if bus == nil {
		return
	}

	// 1. Paid Customer / Revenue
	bus.Subscribe("customer.activated", func(ctx context.Context, e events.Event) error {
		fields, _ := parseAlertPayload(e.Payload, "company_name", "plan", "mrr")
		companyName := fields["company_name"]
		plan := fields["plan"]
		mrr := fields["mrr"]

		return s.notifier.SendAlert(alerts.AlertEvent{
			ID:       s.newID("rev"),
			Category: alerts.CategoryRevenue,
			Priority: alerts.PriorityHigh,
			Title:    "New Business Customer",
			Metadata: map[string]interface{}{
				"company": companyName,
				"plan":    plan,
				"mrr":     mrr,
			},
			Timestamp: s.clock.Now(),
		})
	})

	// 2. Critical System Outage
	bus.Subscribe("system.critical_failure", func(ctx context.Context, e events.Event) error {
		fields, _ := parseAlertPayload(e.Payload, "title", "summary")
		title := fields["title"]
		summary := fields["summary"]

		return s.notifier.SendAlert(alerts.AlertEvent{
			ID:        s.newID("sys"),
			Category:  alerts.CategorySystem,
			Priority:  alerts.PriorityCritical,
			Title:     title,
			Summary:   summary,
			Timestamp: s.clock.Now(),
		})
	})

	// 3. Customer Activation
	bus.Subscribe("customer.onboarding_completed", func(ctx context.Context, e events.Event) error {
		fields, _ := parseAlertPayload(e.Payload, "company_name", "activation_time")
		companyName := fields["company_name"]
		activationTime := fields["activation_time"]

		return s.notifier.SendAlert(alerts.AlertEvent{
			ID:       s.newID("act"),
			Category: alerts.CategoryActivation,
			Priority: alerts.PriorityMedium,
			Title:    "New Activated Customer",
			Metadata: map[string]interface{}{
				"company":         companyName,
				"activation_time": activationTime,
			},
			Timestamp: s.clock.Now(),
		})
	})
}

// EvaluateCustomerHealth calculates health score and sends a churn risk alert if critical
func (s *FounderService) EvaluateCustomerHealth(companyID, companyName string, factors customer_health.CustomerHealthFactors) customer_health.CustomerHealthResult {
	result := customer_health.CalculateHealthScore(companyID, companyName, factors)

	if needsChurnAlert(result.Score) && s.notifier != nil {
		reasonStr := ""
		if len(result.Reasons) > 0 {
			reasonStr = result.Reasons[0]
		}
		_ = s.notifier.SendAlert(alerts.AlertEvent{
			ID:       fmt.Sprintf("churn_%s_%s", companyID, s.idGen.GenerateUUID()),
			Category: alerts.CategoryChurnRisk,
			Priority: alerts.PriorityHigh,
			Title:    "Churn Risk",
			Metadata: map[string]interface{}{
				"company": companyName,
				"score":   fmt.Sprintf("%d", result.Score),
				"reason":  reasonStr,
				"action":  result.SuggestedAction,
			},
			Timestamp: s.clock.Now(),
		})
	}

	return result
}

// SendDailyDigest compiles and dispatches executive daily report to Telegram
func (s *FounderService) SendDailyDigest(report digest.DailyDigestReport) error {
	if s.notifier == nil {
		return nil
	}
	msg := digest.FormatDailyDigest(report)
	return s.notifier.SendAlert(alerts.AlertEvent{
		ID:        s.newID("digest"),
		Category:  alerts.CategoryProductUsage,
		Priority:  alerts.PriorityLow,
		Title:     "Daily Report",
		Summary:   msg,
		Timestamp: s.clock.Now(),
	})
}
