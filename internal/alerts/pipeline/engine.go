package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/alerts/channels"
	"transport-app/internal/alerts/domain"
	"transport-app/internal/alerts/repository"
	"transport-app/internal/events"
	"transport-app/internal/shared"
)

// Clock provides time for deterministic testing.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// Engine is the core alert ingestion, deduplication, and storm-batching engine (Spec 05 §1, §4).
type Engine struct {
	repo     repository.AlertRepository
	channels map[string]channels.Provider
	logger   *slog.Logger
	clock    Clock
}

// NewEngine creates a new alert pipeline Engine.
func NewEngine(repo repository.AlertRepository, channelMap map[string]channels.Provider, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		repo:     repo,
		channels: channelMap,
		logger:   logger,
		clock:    realClock{},
	}
}

// SetClock allows injecting a custom clock for tests.
func (e *Engine) SetClock(c Clock) {
	e.clock = c
}

// SetChannels updates the active channel providers.
func (e *Engine) SetChannels(channelMap map[string]channels.Provider) {
	e.channels = channelMap
}

// IngestEvent represents a normalized event passed to the pipeline.
type IngestEvent struct {
	Source       string                 `json:"source"`
	AlertType    string                 `json:"alert_type"`
	Severity     string                 `json:"severity"`
	Title        string                 `json:"title"`
	Message      string                 `json:"message"`
	EntityType   string                 `json:"entity_type"`
	EntityID     string                 `json:"entity_id"`
	UserID       string                 `json:"user_id,omitempty"`
	TenantID     string                 `json:"tenant_id,omitempty"`
	SeverityRank *int                   `json:"severity_rank,omitempty"`
	MoneyAtRisk  *float64               `json:"money_at_risk,omitempty"`
	Latitude     *float64               `json:"latitude,omitempty"`
	Longitude    *float64               `json:"longitude,omitempty"`
	Value        *float64               `json:"value,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// ProcessEvent processes an incoming event from the event bus.
func (e *Engine) ProcessEvent(ctx context.Context, ev events.Event) error {
	if ev.Type == "SOSEvent" || ev.Type == "telemetry.sos" || ev.Type == events.SOSEvent {
		return e.HandleSOS(ctx, ev)
	}
	ingestEv, err := e.normalizeEvent(ev)
	if err != nil {
		e.logger.Debug("skipping non-alert event in pipeline", "type", ev.Type, "error", err)
		return nil
	}
	return e.Ingest(ctx, ingestEv)
}

// Ingest handles a normalized IngestEvent through rule matching, dedup, and alert creation.
func (e *Engine) Ingest(ctx context.Context, ev IngestEvent) error {
	now := e.clock.Now()

	// 1. Resolve source and rule
	if ev.Source == "" {
		ev.Source = domain.SourceTelemetry
	}

	rule, override, err := e.repo.GetActiveRuleForType(ctx, ev.Source, ev.AlertType, ev.EntityID)
	if err != nil {
		e.logger.Error("failed to look up alert rule", "source", ev.Source, "type", ev.AlertType, "error", err)
	}

	// 2. Determine severity and cooldown
	severity := ev.Severity
	if severity == "" {
		if rule != nil {
			severity = rule.Severity
		} else {
			severity = domain.SeverityWarning
		}
	}
	cooldownSec := 300
	if rule != nil {
		cooldownSec = rule.CooldownSeconds
	}
	if override != nil {
		if override.Severity != nil && *override.Severity != "" {
			severity = *override.Severity
		}
		if override.CooldownSeconds != nil && *override.CooldownSeconds > 0 {
			cooldownSec = *override.CooldownSeconds
		}
	}

	// 3. Compute dedup_key
	dedupKey := fmt.Sprintf("%s:%s:%s", ev.Source, ev.AlertType, ev.EntityID)
	if ev.EntityID == "" {
		dedupKey = fmt.Sprintf("%s:%s:global", ev.Source, ev.AlertType)
	}

	// 4. Check for existing open/acknowledged alert with same dedup_key
	existing, err := e.repo.FindOpenByDedupKey(ctx, dedupKey)
	if err != nil {
		e.logger.Error("error checking open alert dedup", "key", dedupKey, "error", err)
	}

	if existing != nil {
		// Calculate time since last seen
		timeSinceLast := now.Sub(existing.LastSeenAt)
		cooldownDuration := time.Duration(cooldownSec) * time.Second

		if timeSinceLast < cooldownDuration {
			// Inside cooldown window: Increment occurrences and update last_seen_at
			e.logger.Info("alert deduplicated (inside cooldown)",
				"alert_id", existing.ID,
				"dedup_key", dedupKey,
				"occurrences", existing.Occurrences+1,
			)
			return e.repo.IncrementOccurrences(ctx, existing.ID, now)
		}
	}

	// 5. Create new canonical alert
	var ruleIDPtr *string
	var nextEscalationAt *time.Time

	if rule != nil {
		ruleIDPtr = &rule.ID
		if rule.EscalationSchedule != nil && *rule.EscalationSchedule != "" {
			var schedule []EscalationStep
			if err := json.Unmarshal([]byte(*rule.EscalationSchedule), &schedule); err == nil && len(schedule) > 0 {
				at := now.Add(time.Duration(schedule[0].AfterSeconds) * time.Second)
				nextEscalationAt = &at
			}
		}
	}

	var entityTypePtr *string
	if ev.EntityType != "" {
		entityTypePtr = &ev.EntityType
	}
	var entityIDPtr *string
	if ev.EntityID != "" {
		entityIDPtr = &ev.EntityID
	}
	var userIDPtr *string
	if ev.UserID != "" {
		userIDPtr = &ev.UserID
	}

	title := ev.Title
	if title == "" {
		if rule != nil {
			title = rule.Name
		} else {
			title = fmt.Sprintf("Alert: %s", ev.AlertType)
		}
	}

	message := ev.Message
	if message == "" {
		message = title
	}

	metaJSON := "{}"
	if ev.Metadata != nil {
		if b, err := json.Marshal(ev.Metadata); err == nil {
			metaJSON = string(b)
		}
	}

	alert := &domain.Alert{
		ID:               uuid.NewString(),
		RuleID:           ruleIDPtr,
		Source:           ev.Source,
		AlertType:        ev.AlertType,
		Severity:         severity,
		Status:           domain.StatusOpen,
		DedupKey:         dedupKey,
		TenantID:         e.resolveTenantID(ctx, ev),
		AckStatus:        domain.AckStatusOpen,
		SeverityRank:     resolveSeverityRank(ev, severity),
		MoneyAtRisk:      resolveMoneyAtRisk(ev),
		EntityType:       entityTypePtr,
		EntityID:         entityIDPtr,
		UserID:           userIDPtr,
		Title:            title,
		Message:          message,
		Occurrences:      1,
		FirstSeenAt:      now,
		LastSeenAt:       now,
		NextEscalationAt: nextEscalationAt,
		EscalationStep:   0,
		Latitude:         ev.Latitude,
		Longitude:        ev.Longitude,
		Metadata:         metaJSON,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if alert.TenantID == "" {
		e.logger.Warn("skipped unattributable alert (tenant unknown)",
			"type", alert.AlertType, "source", alert.Source, "dedup_key", dedupKey)
		return nil
	}

	if err := e.repo.CreateAlert(ctx, alert); err != nil {
		e.logger.Error("failed to create canonical alert", "dedup_key", dedupKey, "error", err)
		return err
	}

	e.logger.Info("created canonical alert",
		"id", alert.ID,
		"type", alert.AlertType,
		"source", alert.Source,
		"severity", alert.Severity,
		"dedup_key", dedupKey,
	)

	// 6. Dispatch to active channels
	e.dispatchToChannels(ctx, alert, rule, override)

	return nil
}

func (e *Engine) dispatchToChannels(ctx context.Context, alert *domain.Alert, rule *domain.Rule, override *domain.RuleOverride) {
	if len(e.channels) == 0 {
		return
	}

	routingJSON := `{"in_app":true,"telegram":["critical","blocker"]}`
	if rule != nil && rule.ChannelRouting != "" {
		routingJSON = rule.ChannelRouting
	}
	if override != nil && override.Channels != nil && *override.Channels != "" {
		routingJSON = *override.Channels
	}

	var routingMap map[string]interface{}
	if err := json.Unmarshal([]byte(routingJSON), &routingMap); err != nil {
		routingMap = map[string]interface{}{routingJSON: true}
	}

	var entityType, entityID string
	if alert.EntityType != nil {
		entityType = *alert.EntityType
	}
	if alert.EntityID != nil {
		entityID = *alert.EntityID
	}
	var userID string
	if alert.UserID != nil {
		userID = *alert.UserID
	}

	msg := channels.Message{
		AlertID:  alert.ID,
		Title:    alert.Title,
		Body:     alert.Message,
		Severity: alert.Severity,
		UserID:   userID,
		Meta: map[string]any{
			"alert_type":  alert.AlertType,
			"source":      alert.Source,
			"entity_type": entityType,
			"entity_id":   entityID,
		},
	}

	for chName, chConfig := range routingMap {
		provider, ok := e.channels[chName]
		if !ok || provider == nil {
			continue
		}

		shouldSend := false
		switch cfg := chConfig.(type) {
		case bool:
			shouldSend = cfg
		case []interface{}:
			for _, allowedSev := range cfg {
				if s, ok := allowedSev.(string); ok && s == alert.Severity {
					shouldSend = true
					break
				}
			}
		case string:
			shouldSend = cfg == alert.Severity || cfg == "all" || cfg == "true"
		}

		if shouldSend {
			if err := provider.Send(ctx, msg); err != nil {
				e.logger.Warn("failed to send alert to channel", "channel", chName, "alert_id", alert.ID, "error", err)
			}
		}
	}
}

func (e *Engine) normalizeEvent(ev events.Event) (IngestEvent, error) {
	var ingest IngestEvent

	switch ev.Type {
	case "AlertEvent", "telemetry.alert":
		b, err := json.Marshal(ev.Payload)
		if err != nil {
			return ingest, err
		}
		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err != nil {
			return ingest, err
		}

		if src, ok := m["source"].(string); ok && src != "" {
			ingest.Source = src
		} else if cat, ok := m["category"].(string); ok && cat == "fuel" {
			ingest.Source = domain.SourceFuel
		} else {
			ingest.Source = domain.SourceTelemetry
		}

		if at, ok := m["alert_type"].(string); ok {
			ingest.AlertType = at
		} else if at, ok := m["type"].(string); ok {
			ingest.AlertType = at
		}
		if ingest.AlertType == "" {
			ingest.AlertType = legacyAlertType(m)
		}

		if sev, ok := m["severity"].(string); ok {
			ingest.Severity = sev
		} else if sev, ok := m["priority"].(string); ok {
			// Legacy founder-shape producers carry severity as "priority".
			ingest.Severity = sev
		}
		if title, ok := m["title"].(string); ok {
			ingest.Title = title
		}
		if msg, ok := m["details"].(string); ok {
			ingest.Message = msg
		} else if msg, ok := m["message"].(string); ok {
			ingest.Message = msg
		} else if msg, ok := m["summary"].(string); ok {
			ingest.Message = msg
		}

		if vehID, ok := m["vehicle_id"].(string); ok && vehID != "" {
			ingest.EntityType = "vehicle"
			ingest.EntityID = vehID
		} else if tripID, ok := m["trip_id"].(string); ok && tripID != "" {
			ingest.EntityType = "trip"
			ingest.EntityID = tripID
		} else if drvID, ok := m["driver_id"].(string); ok && drvID != "" {
			ingest.EntityType = "driver"
			ingest.EntityID = drvID
		}

		if lat, ok := m["latitude"].(float64); ok {
			ingest.Latitude = &lat
		}
		if lng, ok := m["longitude"].(float64); ok {
			ingest.Longitude = &lng
		}
		ingest.Metadata = m
		return ingest, nil

	case "alert.dtc":
		b, _ := json.Marshal(ev.Payload)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)
		ingest.Source = domain.SourceTelemetry
		ingest.AlertType = "dtc_fault"
		ingest.Severity = domain.SeverityWarning
		if sev, ok := m["severity"].(string); ok && sev != "" {
			ingest.Severity = sev
		}
		if vehID, ok := m["vehicle_id"].(string); ok {
			ingest.EntityType = "vehicle"
			ingest.EntityID = vehID
		}
		if code, ok := m["code"].(string); ok {
			ingest.Title = fmt.Sprintf("DTC Fault: %s", code)
			ingest.Message = fmt.Sprintf("Diagnostic Trouble Code %s detected on vehicle %s", code, ingest.EntityID)
		}
		ingest.Metadata = m
		return ingest, nil

	case "ComplianceBlocked":
		b, _ := json.Marshal(ev.Payload)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)
		ingest.Source = domain.SourceCompliance
		ingest.AlertType = domain.AlertTypeComplianceBlocked
		ingest.Severity = domain.SeverityBlocker
		ingest.Title = "Dispatch Compliance Blocked"
		if reason, ok := m["reason"].(string); ok {
			ingest.Message = reason
		}
		if entityType, ok := m["entity_type"].(string); ok {
			ingest.EntityType = entityType
		}
		if entityID, ok := m["entity_id"].(string); ok {
			ingest.EntityID = entityID
		}
		ingest.Metadata = m
		return ingest, nil

	default:
		return ingest, fmt.Errorf("unsupported event type: %s", ev.Type)
	}
}

// legacyAlertType derives a canonical alert type from founder-shape
// payloads (category + metadata.event_type) that predate the canonical
// alert_type key. Keeps fuel-engine events classifiable instead of
// landing as empty-typed alerts with colliding dedup keys.
func legacyAlertType(m map[string]interface{}) string {
	et, _ := m["event_type"].(string)
	if et == "" {
		if cat, ok := m["category"].(string); ok {
			return cat
		}
		return ""
	}
	switch et {
	case "refill_detected":
		return domain.AlertTypeRefill
	case "drain_theft_suspected":
		return domain.AlertTypeTheftSuspicion
	case "abnormal_drain":
		return domain.AlertTypeAbnormalDrain
	case "siphon_confirmed":
		return domain.AlertTypeSiphonConfirmed
	case "odometer_rollback":
		return domain.AlertTypeOdometerRollback
	default:
		return et
	}
}

// resolveTenantID derives tenant for an alert: context first (bus is
// synchronous, so publisher ctx propagates), then explicit payload key,
// then metadata. Empty means unroutable — callers must skip, never fall
// back to the bootstrap tenant (alerts:read is held by every org's
// viewer/dispatcher, so a misattributed alert leaks across orgs).
func (e *Engine) resolveTenantID(ctx context.Context, ev IngestEvent) string {
	if id := shared.TenantIDFromContext(ctx); id != "" {
		return string(id)
	}
	if ev.TenantID != "" {
		return ev.TenantID
	}
	if ev.Metadata != nil {
		if t, ok := ev.Metadata["tenant_id"].(string); ok && t != "" {
			return t
		}
	}
	return ""
}

// resolveSeverityRank picks the inbox rank: explicit payload rank wins,
// else severity-derived default (Spec 22 §5.1).
func resolveSeverityRank(ev IngestEvent, severity string) int {
	if ev.SeverityRank != nil && *ev.SeverityRank >= domain.RankCritical && *ev.SeverityRank <= domain.RankInfo {
		return *ev.SeverityRank
	}
	if ev.Metadata != nil {
		if r, ok := ev.Metadata["severity_rank"].(float64); ok && r >= domain.RankCritical && r <= domain.RankInfo {
			return int(r)
		}
	}
	return domain.SeverityToRank(severity)
}

// resolveMoneyAtRisk reads the optional money-at-risk estimate from the
// event (field or metadata). Formula-based computation per alert class
// lands with its emitter step (Spec 22 §5.1 constants table).
func resolveMoneyAtRisk(ev IngestEvent) float64 {
	if ev.MoneyAtRisk != nil {
		return *ev.MoneyAtRisk
	}
	if ev.Metadata != nil {
		if m, ok := ev.Metadata["money_at_risk"].(float64); ok && m > 0 {
			return m
		}
	}
	return 0
}
