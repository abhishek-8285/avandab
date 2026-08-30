package deviation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"transport-app/internal/events"
	"transport-app/internal/fuel"
	"transport-app/internal/shared"
)

// TelemetryPoint represents a GPS telemetry point streamed into the deviation engine.
type TelemetryPoint struct {
	TripID    string    `json:"trip_id"`
	VehicleID string    `json:"vehicle_id"`
	DriverID  string    `json:"driver_id"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Speed     float64   `json:"speed"`
	Accuracy  float64   `json:"accuracy"`
	Timestamp time.Time `json:"timestamp"`
	TenantID  string    `json:"tenant_id,omitempty"`
}

// Engine processes live GPS telemetry against active trips to detect sustained route deviations.
type Engine struct {
	db           *sql.DB
	events       events.EventBus
	configReader *fuel.ConfigReader
	logger       *slog.Logger

	mu       sync.RWMutex
	trackers map[string]*TripDeviationTracker
}

// NewEngine constructs a new GPS Deviation Engine instance.
func NewEngine(db *sql.DB, bus events.EventBus, reader *fuel.ConfigReader, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		db:           db,
		events:       bus,
		configReader: reader,
		logger:       logger,
		trackers:     make(map[string]*TripDeviationTracker),
	}
}

type tripMetadata struct {
	ID        string
	TenantID  string
	RouteID   string
	VehicleID string
	DriverID  string
	Status    string
}

// ProcessTelemetry evaluates a single telemetry point for potential route deviation.
func (e *Engine) ProcessTelemetry(ctx context.Context, pt TelemetryPoint) (DeviationState, float64, error) {
	if pt.TripID == "" && pt.VehicleID == "" {
		return StateOnRoute, 0, nil
	}

	// 1. Resolve and validate active trip
	trip, err := e.resolveActiveTrip(ctx, pt.TripID, pt.VehicleID)
	if err != nil || trip == nil {
		return StateOnRoute, 0, nil // No active trip associated
	}

	// Completed / Delivered / Cancelled trip guard (Spec 03 §P3C)
	if trip.Status == "completed" || trip.Status == "delivered" || trip.Status == "cancelled" {
		return StateOnRoute, 0, nil
	}

	// 2. Driver & Vehicle binding verification
	if pt.VehicleID != "" && trip.VehicleID != "" && pt.VehicleID != trip.VehicleID {
		e.logger.Debug("telemetry vehicle mismatch with trip", "pt_vehicle", pt.VehicleID, "trip_vehicle", trip.VehicleID)
		return StateOnRoute, 0, nil
	}
	if pt.DriverID != "" && trip.DriverID != "" && pt.DriverID != trip.DriverID {
		e.logger.Debug("telemetry driver mismatch with trip", "pt_driver", pt.DriverID, "trip_driver", trip.DriverID)
		return StateOnRoute, 0, nil
	}

	tenantID := pt.TenantID
	if tenantID == "" {
		tenantID = trip.TenantID
	}
	if tenantID == "" {
		tenantID = string(shared.TenantIDFromContext(ctx))
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	// 3. Load tenant-specific policy
	policy, err := LoadDeviationPolicy(ctx, tenantID, e.configReader)
	if err != nil {
		policy = DefaultDeviationPolicy(tenantID)
	}

	// 4. Load planned route corridor
	corridor, err := LoadRouteCorridor(ctx, e.db, trip.RouteID)
	if err != nil || corridor == nil || len(corridor.Waypoints) < 2 {
		return StateOnRoute, 0, nil // Cannot compute deviation without route geometry
	}

	if pt.Timestamp.IsZero() {
		pt.Timestamp = time.Now().UTC()
	}

	// 5. Get or initialize state tracker
	e.mu.Lock()
	tracker, exists := e.trackers[trip.ID]
	if !exists {
		tracker = NewTripDeviationTracker(trip.ID, trip.VehicleID, trip.DriverID)
		e.trackers[trip.ID] = tracker
	}
	e.mu.Unlock()

	// 6. Step state machine
	distM, state, shouldAlert := tracker.Step(policy, corridor, pt.Latitude, pt.Longitude, pt.Speed, pt.Accuracy, pt.Timestamp)

	// 7. If sustained deviation threshold reached, persist and dispatch event
	if shouldAlert {
		alertID := fmt.Sprintf("dev_%s_%s_%d", trip.ID, trip.VehicleID, pt.Timestamp.Unix())
		distKM := distM / 1000.0
		details := fmt.Sprintf("Vehicle #%s deviating by %.2f km from planned route (%s → %s)",
			trip.VehicleID, distKM, corridor.Source, corridor.Dest)

		// A. Persist into telemetry_alerts idempotently
		resAlert, _ := e.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO telemetry_alerts (id, trip_id, vehicle_id, driver_id, alert_type, severity, details, latitude, longitude, resolved, created_at)
			VALUES (?, ?, ?, ?, 'gps_deviation', 'critical', ?, ?, ?, 0, ?)
		`, alertID, trip.ID, trip.VehicleID, trip.DriverID, details, pt.Latitude, pt.Longitude, pt.Timestamp.Format("2006-01-02 15:04:05"))

		rowsAlert := int64(0)
		if resAlert != nil {
			rowsAlert, _ = resAlert.RowsAffected()
		}

		// B. Persist into outbox_events idempotently
		outboxPayload := map[string]interface{}{
			"alert_id":     alertID,
			"trip_id":      trip.ID,
			"vehicle_id":   trip.VehicleID,
			"driver_id":    trip.DriverID,
			"tenant_id":    tenantID,
			"alert_type":   "gps_deviation",
			"severity":     "critical",
			"details":      details,
			"latitude":     pt.Latitude,
			"longitude":    pt.Longitude,
			"deviation_m":  distM,
			"deviation_km": distKM,
			"occurred_at":  pt.Timestamp,
			"route_id":     trip.RouteID,
			"source":       corridor.Source,
			"destination":  corridor.Dest,
		}
		payloadBytes, _ := json.Marshal(outboxPayload)

		resOutbox, _ := e.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO outbox_events (id, aggregate_id, aggregate_type, event_type, payload, created_at)
			VALUES (?, ?, 'trip', ?, ?, datetime('now'))
		`, "ob_"+alertID, trip.ID, events.GPSDeviationAlert, string(payloadBytes))

		rowsOutbox := int64(0)
		if resOutbox != nil {
			rowsOutbox, _ = resOutbox.RowsAffected()
		}

		// C. Publish to Event Bus if this is the first delivery
		if (rowsAlert > 0 || rowsOutbox > 0) && e.events != nil {
			e.events.Publish(ctx, events.Event{
				Type:    events.GPSDeviationAlert,
				Payload: outboxPayload,
			})
			e.events.Publish(ctx, events.Event{
				Type: "AlertEvent",
				Payload: map[string]interface{}{
					"source":      "telemetry",
					"alert_type":  "gps_deviation",
					"severity":    "critical",
					"title":       "GPS Route Deviation Alert",
					"details":     details,
					"vehicle_id":  trip.VehicleID,
					"driver_id":   trip.DriverID,
					"trip_id":     trip.ID,
					"tenant_id":   tenantID,
					"latitude":    pt.Latitude,
					"longitude":   pt.Longitude,
					"occurred_at": pt.Timestamp,
				},
			})
		}
	}

	return state, distM, nil
}

func (e *Engine) resolveActiveTrip(ctx context.Context, tripID, vehicleID string) (*tripMetadata, error) {
	var t tripMetadata
	if tripID != "" {
		err := e.db.QueryRowContext(ctx, `
			SELECT id, tenant_id, route_id, vehicle_id, driver_id, status
			FROM trips WHERE id = ? LIMIT 1`, tripID).
			Scan(&t.ID, &t.TenantID, &t.RouteID, &t.VehicleID, &t.DriverID, &t.Status)
		if err == nil {
			return &t, nil
		}
	}

	if vehicleID != "" {
		err := e.db.QueryRowContext(ctx, `
			SELECT id, tenant_id, route_id, vehicle_id, driver_id, status
			FROM trips
			WHERE vehicle_id = ? AND status IN ('assigned', 'started', 'reached_pickup', 'in_transit')
			ORDER BY created_at DESC LIMIT 1`, vehicleID).
			Scan(&t.ID, &t.TenantID, &t.RouteID, &t.VehicleID, &t.DriverID, &t.Status)
		if err == nil {
			return &t, nil
		}
	}

	return nil, nil
}

// ResetTripTracker resets in-memory state for a trip (e.g. on trip completion).
func (e *Engine) ResetTripTracker(tripID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.trackers, tripID)
}

// GetTrackerState returns current state of a trip's tracker.
func (e *Engine) GetTrackerState(tripID string) (DeviationState, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	t, ok := e.trackers[tripID]
	if !ok {
		return StateOnRoute, false
	}
	return t.State, true
}
