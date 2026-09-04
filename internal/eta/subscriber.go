package eta

import (
	"context"
	"log/slog"
	"time"

	tripevents "transport-app/internal/domain/trip"
	"transport-app/internal/events"
)

// SubscribeTripEvents wires the ETA history recorder to the event bus.
// Handles both typed payload (in-memory bus) and map payload (outbox relay JSON rehydration).
func (s *EtaService) SubscribeTripEvents(bus events.EventBus, logger *slog.Logger) {
	if bus == nil || s == nil || s.db == nil {
		if logger != nil {
			logger.Warn("eta: subscriber not wired (nil bus or db)")
		}
		return
	}
	bus.Subscribe(events.TripCompleted, func(ctx context.Context, e events.Event) error {
		var tripID string
		var tenantID string

		switch p := e.Payload.(type) {
		case tripevents.TripCompletedEvent:
			tripID = string(p.TripID)
		case *tripevents.TripCompletedEvent:
			if p != nil {
				tripID = string(p.TripID)
			}
		case map[string]interface{}:
			if v, ok := p["TripID"].(string); ok {
				tripID = v
			} else if v, ok := p["trip_id"].(string); ok {
				tripID = v
			} else if v, ok := p["TripID"].(interface{}); ok {
				_ = v
			}
			if tid, ok := p["TenantID"].(string); ok && tid != "" {
				tenantID = tid
			}
		default:
			// Try generic map via type assertion fallback
			if m, ok := e.Payload.(map[string]string); ok {
				tripID = m["TripID"]
				if tripID == "" {
					tripID = m["trip_id"]
				}
			}
		}

		if tripID == "" {
			if logger != nil {
				logger.Error("eta: TripCompletedEvent missing TripID", "payload_type", stringifyType(e.Payload))
			}
			return nil
		}

		// Resolve tenant if not in payload
		if tenantID == "" {
			tenantID = resolveTenant(ctx, s, tripID)
		}
		if tenantID == "" {
			// Fail closed: never poison another org's ETA history.
			// The trip row itself carries the tenant, so this only fires
			// for deleted/unknown trips.
			if logger != nil {
				logger.Warn("eta: skipped (tenant unknown)", "trip_id", tripID)
			}
			return nil
		}

		segments, err := s.extractSegments(ctx, tripID)
		if err != nil {
			if logger != nil {
				logger.Error("eta: segment extraction failed", "trip_id", tripID, "error", err)
			}
			return nil // do not fail event pipeline
		}
		if len(segments) == 0 {
			if logger != nil {
				logger.Info("eta: no segments for trip (no in-transit points)", "trip_id", tripID)
			}
			return nil
		}

		now := time.Now().UTC()
		trafficTag := deriveTrafficTag(now)

		recorded := 0
		for _, seg := range segments {
			if err := s.RecordHistory(ctx, tenantID, tripID, seg.Start, seg.End, seg.DurationMinutes, trafficTag); err != nil {
				if logger != nil {
					logger.Error("eta: record failed", "trip_id", tripID, "segment", seg.Start+"->"+seg.End, "error", err)
				}
				continue
			}
			recorded++
		}
		if logger != nil {
			logger.Info("eta: history recorded", "trip_id", tripID, "segments", recorded, "traffic_tag", trafficTag, "tenant_id", tenantID)
		}
		return nil
	})
}

func resolveTenant(ctx context.Context, s *EtaService, tripID string) string {
	var tid string
	_ = s.db.QueryRowContext(ctx, `SELECT tenant_id FROM trips WHERE id=?`, tripID).Scan(&tid)
	return tid
}

func stringifyType(v any) string {
	if v == nil {
		return "<nil>"
	}
	return stringify(v)
}

func stringify(v any) string {
	// minimal type name without importing reflect heavy
	switch v.(type) {
	case tripevents.TripCompletedEvent:
		return "tripevents.TripCompletedEvent"
	case *tripevents.TripCompletedEvent:
		return "*tripevents.TripCompletedEvent"
	case map[string]interface{}:
		return "map[string]interface{}"
	default:
		return "unknown"
	}
}

// RunCleanupCron runs daily cleanup of eta_history older than 90 days.
func (s *EtaService) RunCleanupCron(ctx context.Context, interval time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// Run once shortly after start (staggered to avoid startup thundering herd)
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Minute):
	}
	if _, err := s.CleanupOldHistory(ctx); err != nil && logger != nil {
		logger.Error("eta: cleanup failed", "error", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.CleanupOldHistory(ctx); err != nil && logger != nil {
				logger.Error("eta: cleanup failed", "error", err)
			} else if logger != nil {
				logger.Info("eta: cleanup ok")
			}
		}
	}
}

// RunAggregationCron rolls up old history into monthly table daily.
func (s *EtaService) RunAggregationCron(ctx context.Context, interval time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	select {
	case <-ctx.Done():
		return
	case <-time.After(3 * time.Minute):
	}
	if err := s.AggregateMonthly(ctx); err != nil && logger != nil {
		logger.Error("eta: aggregation failed", "error", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.AggregateMonthly(ctx); err != nil && logger != nil {
				logger.Error("eta: aggregation failed", "error", err)
			} else if logger != nil {
				logger.Info("eta: aggregation ok")
			}
		}
	}
}
