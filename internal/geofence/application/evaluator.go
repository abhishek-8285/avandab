package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"transport-app/internal/events"
	"transport-app/internal/geofence/domain"
	sqlrepo "transport-app/internal/geofence/infrastructure/persistence/sql"
	"transport-app/internal/shared"
)

// TelemetryFix represents a live GPS fix submitted to the Realtime Geofence Evaluator.
type TelemetryFix struct {
	TenantID  string    `json:"tenant_id,omitempty"`
	VehicleID string    `json:"vehicle_id"`
	TripID    *string   `json:"trip_id,omitempty"`
	DriverID  *string   `json:"driver_id,omitempty"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Speed     float64   `json:"speed"`
	Accuracy  float64   `json:"accuracy"`
	Timestamp time.Time `json:"timestamp"`
}

// EvaluatorConfig holds thresholds and hysteresis parameters for geofence evaluation.
type EvaluatorConfig struct {
	Debounce            time.Duration
	BufferMetres        float64
	HysteresisMetres    float64
	MaxAccuracyMeters   float64
	MaxFeasibleSpeedKmh float64
}

// DefaultEvaluatorConfig provides standard production defaults.
func DefaultEvaluatorConfig() EvaluatorConfig {
	return EvaluatorConfig{
		Debounce:            DefaultDwellDebounce,
		BufferMetres:        DefaultBufferMetres,
		HysteresisMetres:    DefaultHysteresisMetres,
		MaxAccuracyMeters:   50.0,
		MaxFeasibleSpeedKmh: 180.0,
	}
}

// LoadEvaluatorConfig reads tenant-specific geofence configs from company_config.
func LoadEvaluatorConfig(ctx context.Context, tenantID string, r *ConfigReader) EvaluatorConfig {
	cfg := DefaultEvaluatorConfig()
	if r == nil {
		return cfg
	}

	if d, err := r.GetDurationSeconds(ctx, tenantID, ConfigDwellDebounceSeconds, cfg.Debounce); err == nil && d > 0 {
		cfg.Debounce = d
	}
	if b, err := r.GetFloat(ctx, tenantID, ConfigBufferMetres, cfg.BufferMetres); err == nil && b > 0 {
		cfg.BufferMetres = b
	}
	if h, err := r.GetFloat(ctx, tenantID, ConfigHysteresisMetres, cfg.HysteresisMetres); err == nil && h > 0 {
		cfg.HysteresisMetres = h
	}
	return cfg
}

// RealtimeEvaluator evaluates incoming telemetry streams against active geofences in real time.
type RealtimeEvaluator struct {
	db           *sql.DB
	bus          events.EventBus
	configReader *ConfigReader
	logger       *slog.Logger
	stateRepo    *sqlrepo.EngineStateRepository
}

// NewRealtimeEvaluator constructs a new RealtimeEvaluator instance.
func NewRealtimeEvaluator(db *sql.DB, bus events.EventBus, reader *ConfigReader, logger *slog.Logger) *RealtimeEvaluator {
	if logger == nil {
		logger = slog.Default()
	}
	return &RealtimeEvaluator{
		db:           db,
		bus:          bus,
		configReader: reader,
		logger:       logger,
		stateRepo:    sqlrepo.NewEngineStateRepository(db),
	}
}

// EvaluatedEvent represents a geofence transition or breach event.
type EvaluatedEvent struct {
	EventID    string
	TenantID   string
	VehicleID  string
	TripID     string
	GeofenceID string
	ZoneName   string
	ZoneKind   string
	EventType  string
	Severity   string
	Latitude   float64
	Longitude  float64
	Details    string
	OccurredAt time.Time
}

// EvaluateFix evaluates a single telemetry fix against applicable geofences.
func (e *RealtimeEvaluator) EvaluateFix(ctx context.Context, fix TelemetryFix) ([]EvaluatedEvent, error) {
	if fix.VehicleID == "" {
		return nil, nil
	}

	tenantID := fix.TenantID
	if tenantID == "" {
		tenantID = string(shared.TenantIDFromContext(ctx))
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	cfg := LoadEvaluatorConfig(ctx, tenantID, e.configReader)

	// 1. False positive guard: GPS Accuracy Check
	if fix.Accuracy > 0 && cfg.MaxAccuracyMeters > 0 && fix.Accuracy > cfg.MaxAccuracyMeters {
		e.logger.Debug("geofence fix discarded: low accuracy", "accuracy", fix.Accuracy, "vehicle", fix.VehicleID)
		return nil, nil
	}

	if fix.Timestamp.IsZero() {
		fix.Timestamp = time.Now().UTC()
	}

	// 2. Load active trip context and status guard
	tripStatus := ""
	routeSource := ""
	routeDest := ""
	if fix.TripID != nil && *fix.TripID != "" {
		var status, rSrc, rDst sql.NullString
		_ = e.db.QueryRowContext(ctx, `
			SELECT t.status, COALESCE(r.source, ''), COALESCE(r.destination, '')
			FROM trips t
			LEFT JOIN routes r ON r.id = t.route_id
			WHERE t.id = ? AND t.tenant_id = ?`, *fix.TripID, tenantID).
			Scan(&status, &rSrc, &rDst)
		if status.Valid {
			tripStatus = status.String
			routeSource = rSrc.String
			routeDest = rDst.String
		}
	}

	// 3. Load active geofences for this vehicle & tenant
	zones, err := e.loadApplicableZones(ctx, tenantID, fix.VehicleID, fix.TripID, tripStatus, routeSource, routeDest)
	if err != nil {
		return nil, fmt.Errorf("load geofences: %w", err)
	}

	// 4. Load persisted engine state
	var currentState domain.EngineState
	sPtr, err := e.stateRepo.GetByVehicle(ctx, tenantID, fix.VehicleID)
	if err != nil || sPtr == nil {
		currentState = domain.EngineState{
			VehicleID: fix.VehicleID,
			TenantID:  tenantID,
			State:     domain.StateOutside,
		}
	} else {
		currentState = *sPtr
	}

	// 5. False positive guard: Teleportation Jump Check
	if currentState.LastLat != 0 && currentState.LastLng != 0 && !currentState.LastFixAt.IsZero() {
		dtSec := fix.Timestamp.Sub(currentState.LastFixAt).Seconds()
		if dtSec > 0 {
			distM := domain.Haversine(currentState.LastLat, currentState.LastLng, fix.Latitude, fix.Longitude)
			speedKmh := (distM / 1000.0) / (dtSec / 3600.0)
			if cfg.MaxFeasibleSpeedKmh > 0 && speedKmh > cfg.MaxFeasibleSpeedKmh {
				e.logger.Debug("geofence fix discarded: teleportation jump", "speedKmh", speedKmh, "vehicle", fix.VehicleID)
				return nil, nil // Discard corrupted fix without corrupting state
			}
		}
	}

	// 6. Run DwellEngine evaluation with configured hysteresis & debounce
	engine := NewDwellEngine(EngineConfig{
		Debounce:         cfg.Debounce,
		BufferMetres:     cfg.BufferMetres,
		HysteresisMetres: cfg.HysteresisMetres,
	})

	domainFix := domain.Fix{
		VehicleID: fix.VehicleID,
		TripID:    fix.TripID,
		Timestamp: fix.Timestamp,
		Latitude:  fix.Latitude,
		Longitude: fix.Longitude,
		Speed:     fix.Speed,
	}

	nextState, zoneEvents := engine.Evaluate(currentState, domainFix, zones)

	// 7. Persist state and events with deterministic IDs & Outbox dispatch
	var emittedEvents []EvaluatedEvent
	tripIDStr := ""
	if fix.TripID != nil {
		tripIDStr = *fix.TripID
	}

	// Update engine_state via repository
	_ = e.stateRepo.Upsert(ctx, nextState)

	for _, zev := range zoneEvents {
		eventID := fmt.Sprintf("geo_%s_%s_%s_%d", fix.VehicleID, zev.Zone.ID, zev.EventType, zev.At.Unix())
		sev := SeverityFor(zev.Zone.Kind)

		evaluated := EvaluatedEvent{
			EventID:    eventID,
			TenantID:   tenantID,
			VehicleID:  fix.VehicleID,
			TripID:     tripIDStr,
			GeofenceID: zev.Zone.ID,
			ZoneName:   zev.Zone.Name,
			ZoneKind:   zev.Zone.Kind,
			EventType:  zev.EventType,
			Severity:   sev,
			Latitude:   fix.Latitude,
			Longitude:  fix.Longitude,
			Details:    zev.Details,
			OccurredAt: zev.At,
		}

		// A. Insert into geofence_events with INSERT OR IGNORE
		resEvent, _ := e.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO geofence_events (id, tenant_id, vehicle_id, trip_id, geofence_id, zone_kind, event_type, alert_type, severity, latitude, longitude, details, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'geofence', ?, ?, ?, ?, ?)
		`, eventID, tenantID, fix.VehicleID, tripIDStr, zev.Zone.ID, zev.Zone.Kind, zev.EventType, sev, fix.Latitude, fix.Longitude, zev.Details, zev.At.Format("2006-01-02 15:04:05"))

		rowsEvent := int64(0)
		if resEvent != nil {
			rowsEvent, _ = resEvent.RowsAffected()
		}

		// B. Insert into outbox_events with INSERT OR IGNORE
		outboxPayload := map[string]interface{}{
			"event_id":    eventID,
			"tenant_id":   tenantID,
			"vehicle_id":  fix.VehicleID,
			"trip_id":     tripIDStr,
			"geofence_id": zev.Zone.ID,
			"zone_name":   zev.Zone.Name,
			"zone_kind":   zev.Zone.Kind,
			"event_type":  zev.EventType,
			"severity":    sev,
			"latitude":    fix.Latitude,
			"longitude":   fix.Longitude,
			"details":     zev.Details,
			"occurred_at": zev.At,
		}
		payloadBytes, _ := json.Marshal(outboxPayload)

		canonicalEventType := "geofence.zone_" + zev.EventType
		if zev.EventType == domain.EventBreach {
			canonicalEventType = events.GeofenceZoneBreach
		}

		resOutbox, _ := e.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO outbox_events (id, aggregate_id, aggregate_type, event_type, payload, created_at)
			VALUES (?, ?, 'geofence', ?, ?, datetime('now'))
		`, "ob_"+eventID, zev.Zone.ID, canonicalEventType, string(payloadBytes))

		rowsOutbox := int64(0)
		if resOutbox != nil {
			rowsOutbox, _ = resOutbox.RowsAffected()
		}

		// C. Publish to Event Bus if newly inserted
		if (rowsEvent > 0 || rowsOutbox > 0) && e.bus != nil {
			e.bus.Publish(ctx, events.Event{
				Type:    canonicalEventType,
				Payload: outboxPayload,
			})
			if zev.EventType == domain.EventBreach {
				e.bus.Publish(ctx, events.Event{
					Type: "AlertEvent",
					Payload: map[string]interface{}{
						"source":      "geofence",
						"alert_type":  "geofence_breach",
						"severity":    sev,
						"title":       "Geofence Zone Breach",
						"details":     fmt.Sprintf("Vehicle #%s breached restricted zone: %s", fix.VehicleID, zev.Zone.Name),
						"vehicle_id":  fix.VehicleID,
						"trip_id":     tripIDStr,
						"tenant_id":   tenantID,
						"latitude":    fix.Latitude,
						"longitude":   fix.Longitude,
						"occurred_at": zev.At,
					},
				})
			}
		}

		// D. If stop geofence arrival, transition trip_stops status to arrived
		if strings.HasPrefix(zev.Zone.ID, "stop_geo_") && (zev.EventType == domain.EventEntering || zev.EventType == domain.EventInside) {
			stopID := strings.TrimPrefix(zev.Zone.ID, "stop_geo_")
			_, _ = e.db.ExecContext(ctx, `
				UPDATE trip_stops
				SET status = 'arrived',
				    actual_arrival = COALESCE(actual_arrival, ?),
				    updated_at = datetime('now')
				WHERE id = ? AND tenant_id = ? AND status IN ('pending', 'en_route')
			`, zev.At.Format("2006-01-02 15:04:05"), stopID, tenantID)

			if (rowsEvent > 0 || rowsOutbox > 0) && e.bus != nil {
				e.bus.Publish(ctx, events.Event{
					Type: "trip.stop_arrived",
					Payload: map[string]interface{}{
						"trip_id":     tripIDStr,
						"stop_id":     stopID,
						"tenant_id":   tenantID,
						"occurred_at": zev.At,
					},
				})
			}
		}

		emittedEvents = append(emittedEvents, evaluated)
	}

	return emittedEvents, nil
}

func (e *RealtimeEvaluator) loadApplicableZones(ctx context.Context, tenantID, vehicleID string, tripID *string, tripStatus, routeSource, routeDest string) ([]domain.Geofence, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT id, tenant_id, name, kind, shape, center_lat, center_lng, radius_m, polygon, COALESCE(route_name, ''), priority, is_active
		FROM geofences
		WHERE tenant_id = ? AND is_active = 1
		ORDER BY priority DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var zones []domain.Geofence
	for rows.Next() {
		var z domain.Geofence
		var cLat, cLng, rM sql.NullFloat64
		var polyJSON sql.NullString
		var isActive int

		if err := rows.Scan(&z.ID, &z.TenantID, &z.Name, &z.Kind, &z.Shape, &cLat, &cLng, &rM, &polyJSON, &z.RouteName, &z.Priority, &isActive); err != nil {
			continue
		}
		z.IsActive = isActive == 1
		if cLat.Valid {
			z.CenterLat = cLat.Float64
		}
		if cLng.Valid {
			z.CenterLng = cLng.Float64
		}
		if rM.Valid {
			z.RadiusM = rM.Float64
		}
		if polyJSON.Valid && polyJSON.String != "" {
			z.Polygon, _ = domain.PolygonFromJSON(polyJSON.String)
		}

		// Filter zone applicability
		if (z.Kind == domain.KindPickup || z.Kind == domain.KindDrop) && (tripID == nil || *tripID == "") {
			continue
		}
		// If trip completed/delivered/cancelled, pickup/drop zones are not evaluated
		if (z.Kind == domain.KindPickup || z.Kind == domain.KindDrop) && (tripStatus == "completed" || tripStatus == "delivered" || tripStatus == "cancelled") {
			continue
		}

		if z.RouteName != "" {
			rn := strings.ToLower(strings.TrimSpace(z.RouteName))
			if rn != strings.ToLower(strings.TrimSpace(routeSource)) && rn != strings.ToLower(strings.TrimSpace(routeDest)) {
				continue
			}
		}

		zones = append(zones, z)
	}

	// Dynamic multi-stop geofencing: Load current active stop for active trip
	if tripID != nil && *tripID != "" && tripStatus != "completed" && tripStatus != "delivered" && tripStatus != "cancelled" {
		var stopID, stopType, locName, status string
		var lat, lng, radius sql.NullFloat64
		var seq int
		err := e.db.QueryRowContext(ctx, `
			SELECT id, stop_sequence, stop_type, location_name, latitude, longitude, geofence_radius_m, status
			FROM trip_stops
			WHERE trip_id = ? AND tenant_id = ? AND status IN ('pending', 'en_route', 'arrived', 'servicing')
			ORDER BY stop_sequence ASC
			LIMIT 1
		`, *tripID, tenantID).Scan(&stopID, &seq, &stopType, &locName, &lat, &lng, &radius, &status)
		if err == nil && lat.Valid && lng.Valid && lat.Float64 != 0 && lng.Float64 != 0 {
			radVal := 100.0
			if radius.Valid && radius.Float64 > 0 {
				radVal = radius.Float64
			}
			kind := domain.KindDrop
			if stopType == "pickup" {
				kind = domain.KindPickup
			}
			zones = append(zones, domain.Geofence{
				ID:        fmt.Sprintf("stop_geo_%s", stopID),
				TenantID:  tenantID,
				Name:      locName,
				Kind:      kind,
				Shape:     domain.ShapeCircle,
				CenterLat: lat.Float64,
				CenterLng: lng.Float64,
				RadiusM:   radVal,
				Priority:  100,
				IsActive:  true,
			})
		}
	}

	return zones, nil
}
