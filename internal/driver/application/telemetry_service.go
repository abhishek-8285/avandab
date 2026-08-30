package application

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/driver/domain"
)

type IngestionStatus string

const (
	StatusAccepted           IngestionStatus = "ACCEPTED"
	StatusDuplicate          IngestionStatus = "DUPLICATE"
	StatusStale              IngestionStatus = "STALE"
	StatusInvalidSession     IngestionStatus = "INVALID_SESSION"
	StatusInvalidCoordinates IngestionStatus = "INVALID_COORDINATES"
	StatusUnauthorized       IngestionStatus = "UNAUTHORIZED"
)

type TelemetryIngestRequest struct {
	SessionID       string  `json:"session_id"`
	InstallationID  string  `json:"installation_id"`
	ClientEventID   string  `json:"client_event_id"`
	OccurredAt      string  `json:"occurred_at"`
	Latitude        float64 `json:"latitude"`
	Longitude       float64 `json:"longitude"`
	AccuracyMeters  float64 `json:"accuracy_meters"`
	SpeedKmph       float64 `json:"speed_kmph"`
	HeadingDegrees  float64 `json:"heading_degrees"`
	BatteryLevelPct int     `json:"battery_level_pct"`
	BatteryState    string  `json:"battery_state"`
	TripID          *string `json:"trip_id,omitempty"`
}

type TelemetryIngestResponse struct {
	Status        IngestionStatus `json:"status"`
	Message       string          `json:"message"`
	ClientEventID string          `json:"client_event_id"`
	EventID       string          `json:"event_id,omitempty"`
	VehicleID     string          `json:"vehicle_id,omitempty"`
}

func (s *DriverAppService) StartTelemetrySession(ctx context.Context, tenantID, driverID, installationID, appVersion, osVersion string) (*domain.TelemetrySessionRecord, error) {
	// Find active vehicle assignment for driver
	asg, err := s.repo.GetActiveAssignmentForDriver(ctx, tenantID, driverID)
	if err != nil {
		return nil, err
	}
	var vehicleID *string
	if asg != nil {
		vehicleID = &asg.VehicleID
	}

	sessionID := uuid.NewString()
	sess := domain.TelemetrySessionRecord{
		ID:             sessionID,
		TenantID:       tenantID,
		InstallationID: installationID,
		DriverID:       driverID,
		VehicleID:      vehicleID,
		SessionType:    "on_duty",
		Status:         "active",
		StartReason:    "APP_AVAILABLE",
		StartedAt:      time.Now(),
	}

	if err := s.repo.StartSession(ctx, tenantID, sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *DriverAppService) EndTelemetrySession(ctx context.Context, tenantID, driverID, sessionID string) error {
	return s.repo.EndSession(ctx, tenantID, sessionID, "DRIVER_LOGOUT", time.Now())
}

func (s *DriverAppService) IngestTelemetryEvent(ctx context.Context, tenantID, driverID string, req TelemetryIngestRequest) (TelemetryIngestResponse, error) {
	// 1. Validate coordinates (-90 to 90 lat, -180 to 180 lng, not 0,0 unless specifically allowed)
	if math.IsNaN(req.Latitude) || math.IsNaN(req.Longitude) ||
		req.Latitude < -90 || req.Latitude > 90 ||
		req.Longitude < -180 || req.Longitude > 180 ||
		(req.Latitude == 0 && req.Longitude == 0) {
		return TelemetryIngestResponse{
			Status:        StatusInvalidCoordinates,
			Message:       "Coordinates out of physical range or uncalibrated zero point",
			ClientEventID: req.ClientEventID,
		}, nil
	}

	// 2. Parse occurred_at timestamp
	occurredAt, err := time.Parse(time.RFC3339, req.OccurredAt)
	if err != nil {
		occurredAt = time.Now()
	}

	// Reject frames older than 7 days (STALE)
	if time.Since(occurredAt) > 7*24*time.Hour {
		return TelemetryIngestResponse{
			Status:        StatusStale,
			Message:       "Telemetry timestamp is older than retention window",
			ClientEventID: req.ClientEventID,
		}, nil
	}

	// 3. Resolve active session & vehicle
	var vehicleID string
	ex := s.exec(ctx)
	err = ex.QueryRowContext(ctx, `
		SELECT COALESCE(vehicle_id, '') FROM telemetry_sessions
		WHERE tenant_id = ? AND id = ? AND driver_id = ? AND status = 'active'`,
		tenantID, req.SessionID, driverID).Scan(&vehicleID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Auto-provision fallback session if none active
			asg, _ := s.repo.GetActiveAssignmentForDriver(ctx, tenantID, driverID)
			var vID *string
			if asg != nil {
				vID = &asg.VehicleID
				vehicleID = asg.VehicleID
			}
			newSess := domain.TelemetrySessionRecord{
				ID:             req.SessionID,
				TenantID:       tenantID,
				DriverID:       driverID,
				VehicleID:      vID,
				InstallationID: req.InstallationID,
				SessionType:    "on_duty",
				Status:         "active",
				StartReason:    "AUTO_FALLBACK",
				StartedAt:      time.Now(),
			}
			_ = s.repo.StartSession(ctx, tenantID, newSess)
		} else {
			return TelemetryIngestResponse{
				Status:        StatusInvalidSession,
				Message:       "Failed querying active telemetry session: " + err.Error(),
				ClientEventID: req.ClientEventID,
			}, nil
		}
	}

	// 4. Save Event (idempotent ON CONFLICT DO NOTHING)
	eventID := uuid.NewString()
	accuracy := req.AccuracyMeters
	heading := req.HeadingDegrees
	event := domain.TelemetryEventRecord{
		ID:            eventID,
		TenantID:      tenantID,
		SessionID:     req.SessionID,
		ClientEventID: req.ClientEventID,
		OccurredAt:    occurredAt,
		ReceivedAt:    time.Now(),
		Latitude:      req.Latitude,
		Longitude:     req.Longitude,
		Speed:         req.SpeedKmph,
		Accuracy:      &accuracy,
		Heading:       &heading,
	}

	if err := s.repo.IngestEvent(ctx, tenantID, event); err != nil {
		return TelemetryIngestResponse{
			Status:        StatusInvalidSession,
			Message:       fmt.Sprintf("Failed recording event: %v", err),
			ClientEventID: req.ClientEventID,
		}, err
	}

	// 5. Update latest position projection if vehicle bound
	if vehicleID != "" {
		pos := domain.VehicleLatestPositionRecord{
			TenantID:   tenantID,
			VehicleID:  vehicleID,
			SessionID:  req.SessionID,
			DriverID:   driverID,
			Latitude:   req.Latitude,
			Longitude:  req.Longitude,
			Accuracy:   &accuracy,
			Speed:      req.SpeedKmph,
			Heading:    &heading,
			OccurredAt: occurredAt,
			ReceivedAt: time.Now(),
			Source:     "mobile_driver_app",
		}
		_ = s.repo.UpsertLatestPosition(ctx, tenantID, pos)
	}

	return TelemetryIngestResponse{
		Status:        StatusAccepted,
		Message:       "Event ingested successfully",
		ClientEventID: req.ClientEventID,
		EventID:       eventID,
		VehicleID:     vehicleID,
	}, nil
}
