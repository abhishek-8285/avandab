package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/alerts/channels"
	"transport-app/internal/alerts/domain"
	"transport-app/internal/events"
	"transport-app/internal/shared"
)

// HandleSOS processes an emergency SOSEvent (Spec 05 §8).
func (e *Engine) HandleSOS(ctx context.Context, ev events.Event) error {
	var payload map[string]interface{}
	if m, ok := ev.Payload.(map[string]interface{}); ok {
		payload = m
	} else {
		b, err := json.Marshal(ev.Payload)
		if err == nil {
			_ = json.Unmarshal(b, &payload)
		}
	}
	if payload == nil {
		return nil
	}

	vehicleID := ""
	if v, ok := payload["VehicleID"].(string); ok && v != "" {
		vehicleID = v
	} else if v, ok := payload["vehicle_id"].(string); ok {
		vehicleID = v
	}

	driverID := ""
	if d, ok := payload["DriverID"].(string); ok && d != "" {
		driverID = d
	} else if d, ok := payload["driver_id"].(string); ok && d != "" {
		driverID = d
	}

	// Route the emergency to the right org: context, then payload. Never the
	// bootstrap tenant — a misrouted SOS both leaks location and misses
	// response. Unroutable SOS fails loudly so it gets fixed, not filed away.
	tenantID := string(shared.TenantIDFromContext(ctx))
	if tenantID == "" {
		if t, ok := payload["TenantID"].(string); ok && t != "" {
			tenantID = t
		} else if t, ok := payload["tenant_id"].(string); ok && t != "" {
			tenantID = t
		}
	}

	var latPtr, lngPtr *float64
	if lat, ok := payload["Latitude"].(float64); ok {
		latPtr = &lat
	} else if lat, ok := payload["latitude"].(float64); ok {
		latPtr = &lat
	}
	if lng, ok := payload["Longitude"].(float64); ok {
		lngPtr = &lng
	} else if lng, ok := payload["longitude"].(float64); ok {
		lngPtr = &lng
	}

	now := e.clock.Now()

	// 1. Dedup key: "sos:<vehicle_id>" with 60s cooldown
	dedupKey := fmt.Sprintf("sos:%s", vehicleID)
	if existing, err := e.repo.FindOpenByDedupKey(ctx, dedupKey); err == nil && existing != nil {
		if now.Sub(existing.LastSeenAt) < 60*time.Second {
			_ = e.repo.IncrementOccurrences(ctx, existing.ID, now)
			return nil
		}
	}

	// 2. Create canonical alert (source='sos', severity='blocker')
	latStr := "unknown"
	lngStr := "unknown"
	if latPtr != nil && lngPtr != nil {
		latStr = fmt.Sprintf("%.6f", *latPtr)
		lngStr = fmt.Sprintf("%.6f", *lngPtr)
	}

	entityType := "vehicle"
	alert := &domain.Alert{
		ID:             uuid.NewString(),
		Source:         "sos",
		AlertType:      "sos",
		Severity:       domain.SeverityBlocker,
		DedupKey:       dedupKey,
		EntityType:     &entityType,
		EntityID:       &vehicleID,
		Title:          "🚨 SOS — Emergency from vehicle " + vehicleID,
		Message:        fmt.Sprintf("SOS triggered at (%s, %s). Driver: %s. Immediate response required.", latStr, lngStr, driverID),
		Latitude:       latPtr,
		Longitude:      lngPtr,
		Status:         domain.StatusOpen,
		EscalationStep: 0,
		FirstSeenAt:    now,
		LastSeenAt:     now,
		Occurrences:    1,
		CreatedAt:      now,
		TenantID:       tenantID,
	}

	if tenantID == "" {
		e.logger.Error("SOS alert unroutable (tenant unknown)", "vehicle_id", vehicleID)
		return fmt.Errorf("sos: cannot persist alert without tenant")
	}

	// Escalation: 10 min schedule for SOS
	nextAt := now.Add(10 * time.Minute)
	alert.NextEscalationAt = &nextAt

	if err := e.repo.CreateAlert(ctx, alert); err != nil {
		e.logger.Error("failed to create SOS alert", "error", err)
		return err
	}

	// 3. Fan out: in_app and telegram
	msg := channels.Message{
		AlertID:  alert.ID,
		Title:    alert.Title,
		Body:     alert.Message,
		Severity: domain.SeverityBlocker,
		Meta: map[string]any{
			"alert_type":  alert.AlertType,
			"source":      alert.Source,
			"entity_type": "vehicle",
			"entity_id":   vehicleID,
			"driver_id":   driverID,
		},
	}

	if inApp, ok := e.channels["in_app"]; ok && inApp != nil {
		_ = inApp.Send(ctx, msg)
	}
	if tg, ok := e.channels["telegram"]; ok && tg != nil {
		_ = tg.Send(ctx, msg)
	}

	return nil
}
