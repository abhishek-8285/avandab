package coldchain

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// ConfigureProfile sets up or updates reefer specifications and multi-zone temperature bands.
func (s *Service) ConfigureProfile(ctx context.Context, tenantID, vehicleID string, in ConfigureReeferProfileInput) (*ReeferProfile, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	profile := &ReeferProfile{
		VehicleID:          vehicleID,
		TenantID:           tenantID,
		ReeferMakeModel:    in.ReeferMakeModel,
		NumZones:           in.NumZones,
		Zone1Type:          in.Zone1Type,
		Zone1TargetTempC:   in.Zone1TargetTempC,
		Zone1MinTempC:      in.Zone1MinTempC,
		Zone1MaxTempC:      in.Zone1MaxTempC,
		Zone2Type:          in.Zone2Type,
		Zone2TargetTempC:   in.Zone2TargetTempC,
		Zone2MinTempC:      in.Zone2MinTempC,
		Zone2MaxTempC:      in.Zone2MaxTempC,
		HumidityMonitoring: in.HumidityMonitoring,
		TargetHumidityRH:   in.TargetHumidityRH,
		DefrostMode:        in.DefrostMode,
	}

	if err := s.repo.UpsertProfile(ctx, profile); err != nil {
		return nil, err
	}

	return s.repo.GetProfile(ctx, tenantID, vehicleID)
}

// GetProfile fetches the reefer profile for a vehicle.
func (s *Service) GetProfile(ctx context.Context, tenantID, vehicleID string) (*ReeferProfile, error) {
	return s.repo.GetProfile(ctx, tenantID, vehicleID)
}

// IngestTelemetry ingests a live reefer telemetry frame, checks set-point drift and thermal bands,
// and automatically manages excursion incidents.
func (s *Service) IngestTelemetry(ctx context.Context, tenantID string, in IngestReeferTelemetryInput) (*ReeferTelemetry, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if in.Timestamp != nil && !in.Timestamp.IsZero() {
		now = in.Timestamp.UTC()
	}

	profile, err := s.repo.GetProfile(ctx, tenantID, in.VehicleID)
	if err != nil {
		return nil, err
	}

	hasActiveExcursion := false

	if profile != nil {
		activeExc, err := s.repo.GetActiveExcursion(ctx, tenantID, in.VehicleID, 1)
		if err != nil {
			return nil, err
		}

		isOverTemp := in.Zone1TempC > profile.Zone1MaxTempC
		isUnderTemp := in.Zone1TempC < profile.Zone1MinTempC

		if isOverTemp {
			hasActiveExcursion = true
			severity := SeverityWarning
			if in.Zone1TempC >= profile.Zone1MaxTempC+3.0 {
				severity = SeverityCritical
			}

			if activeExc == nil {
				// Open new OVER_TEMP excursion
				newExc := &TemperatureExcursion{
					ID:             uuid.NewString(),
					TenantID:       tenantID,
					VehicleID:      in.VehicleID,
					TripID:         in.TripID,
					ZoneNumber:     1,
					ExcursionType:  ExcursionOverTemp,
					Severity:       severity,
					StartTime:      now,
					PeakTempC:      in.Zone1TempC,
					ThresholdTempC: profile.Zone1MaxTempC,
					SetpointC:      in.Zone1SetpointC,
					Status:         ExcursionOpen,
				}
				_ = s.repo.CreateExcursion(ctx, newExc)
			} else {
				// Update peak temperature and duration
				if in.Zone1TempC > activeExc.PeakTempC {
					activeExc.PeakTempC = in.Zone1TempC
				}
				activeExc.DurationSeconds = int(now.Sub(activeExc.StartTime).Seconds())
				_ = s.repo.UpdateExcursion(ctx, activeExc)
			}
		} else if isUnderTemp {
			hasActiveExcursion = true
			severity := SeverityWarning
			if in.Zone1TempC <= profile.Zone1MinTempC-3.0 {
				severity = SeverityCritical
			}

			if activeExc == nil {
				newExc := &TemperatureExcursion{
					ID:             uuid.NewString(),
					TenantID:       tenantID,
					VehicleID:      in.VehicleID,
					TripID:         in.TripID,
					ZoneNumber:     1,
					ExcursionType:  ExcursionUnderTemp,
					Severity:       severity,
					StartTime:      now,
					PeakTempC:      in.Zone1TempC,
					ThresholdTempC: profile.Zone1MinTempC,
					SetpointC:      in.Zone1SetpointC,
					Status:         ExcursionOpen,
				}
				_ = s.repo.CreateExcursion(ctx, newExc)
			} else {
				if in.Zone1TempC < activeExc.PeakTempC {
					activeExc.PeakTempC = in.Zone1TempC
				}
				activeExc.DurationSeconds = int(now.Sub(activeExc.StartTime).Seconds())
				_ = s.repo.UpdateExcursion(ctx, activeExc)
			}
		} else if activeExc != nil {
			// Temperature normalized: resolve active thermal excursion
			activeExc.EndTime = &now
			activeExc.DurationSeconds = int(now.Sub(activeExc.StartTime).Seconds())
			activeExc.Status = ExcursionResolved
			activeExc.ResolutionNote = "Thermal reading restored within permissible operational band"
			activeExc.ResolvedBy = "telemetry-engine"
			_ = s.repo.UpdateExcursion(ctx, activeExc)
		}
	}

	// Check prolonged door open (> 180 seconds)
	if in.DoorStatus == DoorOpen && in.DoorOpenDurationSec >= 180 {
		hasActiveExcursion = true
	}

	telem := &ReeferTelemetry{
		VehicleID:           in.VehicleID,
		TenantID:            tenantID,
		ReeferPowerStatus:   in.ReeferPowerStatus,
		ReeferMode:          in.ReeferMode,
		Zone1TempC:          in.Zone1TempC,
		Zone1SetpointC:      in.Zone1SetpointC,
		Zone2TempC:          in.Zone2TempC,
		Zone2SetpointC:      in.Zone2SetpointC,
		HumidityRH:          in.HumidityRH,
		DoorStatus:          in.DoorStatus,
		DoorOpenDurationSec: in.DoorOpenDurationSec,
		ReeferFuelLevelPct:  in.ReeferFuelLevelPct,
		ReeferBatteryV:      in.ReeferBatteryV,
		HasActiveExcursion:  hasActiveExcursion,
		LastSyncAt:          now,
	}

	if err := s.repo.UpsertTelemetry(ctx, telem); err != nil {
		return nil, err
	}

	return telem, nil
}

// GetLatestTelemetry returns the latest reefer status for a vehicle.
func (s *Service) GetLatestTelemetry(ctx context.Context, tenantID, vehicleID string) (*ReeferTelemetry, error) {
	return s.repo.GetLatestTelemetry(ctx, tenantID, vehicleID)
}

// ListReeferVehicles retrieves all reefer-equipped vehicles with real-time zone metrics.
func (s *Service) ListReeferVehicles(ctx context.Context, tenantID string) ([]ColdChainVehicleStatus, error) {
	return s.repo.ListReeferVehicles(ctx, tenantID)
}

// ListExcursions returns temperature excursion incidents.
func (s *Service) ListExcursions(ctx context.Context, tenantID, vehicleID, status string, limit, offset int) ([]TemperatureExcursion, int, error) {
	return s.repo.ListExcursions(ctx, tenantID, vehicleID, status, limit, offset)
}

// AcknowledgeExcursion flags an open excursion as reviewed by the operator.
func (s *Service) AcknowledgeExcursion(ctx context.Context, tenantID, excursionID, acknowledgedBy string) (*TemperatureExcursion, error) {
	exc, err := s.repo.GetExcursion(ctx, tenantID, excursionID)
	if err != nil {
		return nil, err
	}
	if exc == nil {
		return nil, errors.New("excursion not found")
	}

	exc.Status = ExcursionAcknowledged
	exc.ResolvedBy = acknowledgedBy
	if err := s.repo.UpdateExcursion(ctx, exc); err != nil {
		return nil, err
	}
	return exc, nil
}

// ResolveExcursion closes out a temperature excursion with corrective action documentation.
func (s *Service) ResolveExcursion(ctx context.Context, tenantID, excursionID string, in ResolveExcursionInput) (*TemperatureExcursion, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	exc, err := s.repo.GetExcursion(ctx, tenantID, excursionID)
	if err != nil {
		return nil, err
	}
	if exc == nil {
		return nil, errors.New("excursion not found")
	}

	now := time.Now().UTC()
	exc.EndTime = &now
	exc.DurationSeconds = int(now.Sub(exc.StartTime).Seconds())
	exc.Status = ExcursionResolved
	exc.ResolutionNote = in.ResolutionNote
	exc.ResolvedBy = in.ResolvedBy

	if err := s.repo.UpdateExcursion(ctx, exc); err != nil {
		return nil, err
	}
	return exc, nil
}

// CertifyTripCompliance calculates Mean Kinetic Temperature (MKT), evaluates regulatory limits,
// and stamps a HACCP / GDP pharma cold-chain certificate for a trip.
func (s *Service) CertifyTripCompliance(ctx context.Context, tenantID string, in CertifyTripComplianceInput, readings []float64, excursionDurationSec, doorOpenCount int, startTime, endTime time.Time) (*ReeferTripSummary, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	profile, err := s.repo.GetProfile(ctx, tenantID, in.VehicleID)
	if err != nil {
		return nil, err
	}

	minTemp := 999.0
	maxTemp := -999.0
	for _, t := range readings {
		if t < minTemp {
			minTemp = t
		}
		if t > maxTemp {
			maxTemp = t
		}
	}
	if len(readings) == 0 {
		minTemp = 0.0
		maxTemp = 0.0
	}

	mkt := CalculateMKT(readings)

	complianceStatus := ComplianceCompliant
	if profile != nil {
		// GDP Pharma standard: if MKT exceeds upper/lower target, or cumulative excursion > 15 mins (900s)
		if mkt > profile.Zone1MaxTempC || mkt < profile.Zone1MinTempC || excursionDurationSec > 900 {
			complianceStatus = ComplianceBreached
		}
	}

	now := time.Now().UTC()
	summary := &ReeferTripSummary{
		ID:                        uuid.NewString(),
		TenantID:                  tenantID,
		TripID:                    in.TripID,
		VehicleID:                 in.VehicleID,
		StartTime:                 startTime,
		EndTime:                   endTime,
		Zone1MinTempC:             math.Round(minTemp*10) / 10,
		Zone1MaxTempC:             math.Round(maxTemp*10) / 10,
		Zone1MeanKineticTempC:     mkt,
		TotalExcursionDurationSec: excursionDurationSec,
		DoorOpenCount:             doorOpenCount,
		ComplianceStatus:          complianceStatus,
		CertifiedAt:               &now,
		CertifiedBy:               in.CertifiedBy,
		CreatedAt:                 now,
	}

	if err := s.repo.CreateTripSummary(ctx, summary); err != nil {
		return nil, err
	}

	return summary, nil
}

// GetFleetMetrics returns holistic cold chain compliance and active cooling analytics.
func (s *Service) GetFleetMetrics(ctx context.Context, tenantID string) (*ColdChainFleetMetrics, error) {
	return s.repo.GetFleetMetrics(ctx, tenantID)
}

// ListTripSummaries retrieves recent cold-chain compliance certificates.
func (s *Service) ListTripSummaries(ctx context.Context, tenantID string, limit, offset int) ([]ReeferTripSummary, int, error) {
	return s.repo.ListTripSummaries(ctx, tenantID, limit, offset)
}
