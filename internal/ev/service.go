package ev

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
)

// Electricity tariff constants for India (INR per kWh) used for automated session costing.
const (
	TariffACPerKWh = 8.50  // AC slow / destination charging
	TariffDCPerKWh = 14.50 // DC fast charging highway corridor
)

// Standard Indian commercial diesel vs grid carbon displacement factor (kg CO2 per km).
const (
	CO2SavingsPerKM = 0.55
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// ConfigureProfile sets or updates battery & drivetrain parameters for an electric vehicle.
func (s *Service) ConfigureProfile(ctx context.Context, tenantID, vehicleID string, in ConfigureEVProfileInput) (*EVProfile, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	profile := &EVProfile{
		VehicleID:             vehicleID,
		TenantID:              tenantID,
		BatteryCapacityKWh:    in.BatteryCapacityKWh,
		MaxRangeKM:            in.MaxRangeKM,
		ChargingStandard:      in.ChargingStandard,
		MinSafeSoC:            in.MinSafeSoC,
		NominalEfficiencyWhKM: in.NominalEfficiencyWhKM,
	}

	if err := s.repo.UpsertProfile(ctx, profile); err != nil {
		return nil, err
	}

	return s.repo.GetProfile(ctx, tenantID, vehicleID)
}

// GetProfile retrieves an EV profile by vehicle ID.
func (s *Service) GetProfile(ctx context.Context, tenantID, vehicleID string) (*EVProfile, error) {
	return s.repo.GetProfile(ctx, tenantID, vehicleID)
}

// IngestTelemetry ingests a live CAN or OEM battery telemetry frame, maintains latest status,
// and automatically manages charging sessions.
func (s *Service) IngestTelemetry(ctx context.Context, tenantID string, in IngestEVTelemetryInput) (*EVTelemetry, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if in.Timestamp != nil && !in.Timestamp.IsZero() {
		now = in.Timestamp.UTC()
	}

	// Fetch vehicle profile if configured
	profile, err := s.repo.GetProfile(ctx, tenantID, in.VehicleID)
	if err != nil {
		return nil, err
	}

	// 1. Calculate remaining range if not directly supplied by vehicle CAN
	if in.RemainingRangeKM <= 0 {
		if profile != nil && profile.MaxRangeKM > 0 {
			in.RemainingRangeKM = math.Round((in.SoCPercent/100.0)*profile.MaxRangeKM*10) / 10
		} else if profile != nil && profile.NominalEfficiencyWhKM > 0 && profile.BatteryCapacityKWh > 0 {
			usableKWh := (in.SoCPercent / 100.0) * profile.BatteryCapacityKWh
			in.RemainingRangeKM = math.Round((usableKWh*1000.0/profile.NominalEfficiencyWhKM)*10) / 10
		}
	}

	// 2. Calculate charging power if electrical metrics are provided
	if in.ChargingPowerKW <= 0 && in.VoltageV > 0 && in.CurrentDrawA > 0 {
		in.ChargingPowerKW = math.Round((in.VoltageV*in.CurrentDrawA/1000.0)*100) / 100
	}

	// 3. Automated charging session lifecycle management
	activeSession, err := s.repo.GetActiveChargingSession(ctx, tenantID, in.VehicleID)
	if err != nil {
		return nil, err
	}

	isChargingStatus := in.ChargingStatus == ChargingStatusCharging || in.ChargingStatus == ChargingStatusFastCharging

	if isChargingStatus {
		chargerType := ChargerTypeACSlow
		if in.ChargingStatus == ChargingStatusFastCharging {
			chargerType = ChargerTypeDCFast
		}

		if activeSession == nil {
			// Trigger new charging session
			newSession := &ChargingSession{
				ID:             uuid.NewString(),
				TenantID:       tenantID,
				VehicleID:      in.VehicleID,
				StartTime:      now,
				StartSoC:       in.SoCPercent,
				EnergyAddedKWh: 0.0,
				PeakPowerKW:    in.ChargingPowerKW,
				ChargerType:    chargerType,
				EstimatedCost:  0.0,
				Status:         SessionStatusInProgress,
			}
			_ = s.repo.CreateChargingSession(ctx, newSession)
		} else {
			// Update ongoing session
			changed := false
			if in.ChargingPowerKW > activeSession.PeakPowerKW {
				activeSession.PeakPowerKW = in.ChargingPowerKW
				changed = true
			}
			if in.SoCPercent > activeSession.StartSoC && profile != nil {
				addedKWh := math.Round(((in.SoCPercent-activeSession.StartSoC)/100.0)*profile.BatteryCapacityKWh*100) / 100
				if addedKWh != activeSession.EnergyAddedKWh {
					activeSession.EnergyAddedKWh = addedKWh
					changed = true
				}
			}
			if chargerType == ChargerTypeDCFast && activeSession.ChargerType != ChargerTypeDCFast {
				activeSession.ChargerType = ChargerTypeDCFast
				changed = true
			}
			if changed {
				_ = s.repo.UpdateChargingSession(ctx, activeSession)
			}
		}
	} else if activeSession != nil {
		// Vehicle unplugged / completed charging: close session
		activeSession.EndTime = &now
		activeSession.EndSoC = &in.SoCPercent

		if in.SoCPercent > activeSession.StartSoC && profile != nil {
			activeSession.EnergyAddedKWh = math.Round(((in.SoCPercent-activeSession.StartSoC)/100.0)*profile.BatteryCapacityKWh*100) / 100
		}

		tariff := TariffACPerKWh
		if activeSession.ChargerType == ChargerTypeDCFast {
			tariff = TariffDCPerKWh
		}
		activeSession.EstimatedCost = math.Round(activeSession.EnergyAddedKWh*tariff*100) / 100
		activeSession.Status = SessionStatusCompleted

		_ = s.repo.UpdateChargingSession(ctx, activeSession)
	}

	// 4. Upsert latest telemetry
	telem := &EVTelemetry{
		VehicleID:        in.VehicleID,
		TenantID:         tenantID,
		SoCPercent:       in.SoCPercent,
		SoHPercent:       in.SoHPercent,
		BatteryTempC:     in.BatteryTempC,
		RemainingRangeKM: in.RemainingRangeKM,
		ChargingStatus:   in.ChargingStatus,
		ChargingPowerKW:  in.ChargingPowerKW,
		CurrentDrawA:     in.CurrentDrawA,
		VoltageV:         in.VoltageV,
		LastSyncAt:       now,
	}

	if err := s.repo.UpsertTelemetry(ctx, telem); err != nil {
		return nil, err
	}

	return telem, nil
}

// GetLatestTelemetry returns current battery and range metrics for a vehicle.
func (s *Service) GetLatestTelemetry(ctx context.Context, tenantID, vehicleID string) (*EVTelemetry, error) {
	return s.repo.GetLatestTelemetry(ctx, tenantID, vehicleID)
}

// ListEVVehicles returns all electric vehicles with their profile, battery telemetry, and session info.
func (s *Service) ListEVVehicles(ctx context.Context, tenantID string) ([]EVVehicleStatus, error) {
	vehicles, err := s.repo.ListEVVehicles(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	for i := range vehicles {
		sess, err := s.repo.GetActiveChargingSession(ctx, tenantID, vehicles[i].VehicleID)
		if err == nil && sess != nil {
			vehicles[i].ActiveSession = sess
		}
	}

	return vehicles, nil
}

// ListChargingSessions returns past charging cycles for a vehicle or across the tenant fleet.
func (s *Service) ListChargingSessions(ctx context.Context, tenantID, vehicleID string, limit, offset int) ([]ChargingSession, int, error) {
	return s.repo.ListChargingSessions(ctx, tenantID, vehicleID, limit, offset)
}

// RecordEnergySummary logs energy consumption and carbon offset for an EV trip.
func (s *Service) RecordEnergySummary(ctx context.Context, tenantID string, in RecordEnergySummaryInput) (*EVEnergySummary, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	profile, err := s.repo.GetProfile(ctx, tenantID, in.VehicleID)
	if err != nil {
		return nil, err
	}

	energyKWh := in.EnergyConsumedKWh
	if energyKWh <= 0 && profile != nil && profile.NominalEfficiencyWhKM > 0 {
		energyKWh = math.Round((in.DistanceKM*profile.NominalEfficiencyWhKM/1000.0)*100) / 100
	}

	efficiencyWhKM := 0.0
	if in.DistanceKM > 0 && energyKWh > 0 {
		efficiencyWhKM = math.Round((energyKWh*1000.0/in.DistanceKM)*10) / 10
	} else if profile != nil {
		efficiencyWhKM = profile.NominalEfficiencyWhKM
	}

	co2Avoided := math.Round(in.DistanceKM*CO2SavingsPerKM*100) / 100

	summary := &EVEnergySummary{
		ID:                uuid.NewString(),
		TenantID:          tenantID,
		VehicleID:         in.VehicleID,
		TripID:            in.TripID,
		DistanceKM:        in.DistanceKM,
		EnergyConsumedKWh: energyKWh,
		EfficiencyWhKM:    efficiencyWhKM,
		CO2AvoidedKG:      co2Avoided,
		RecordedAt:        time.Now().UTC(),
	}

	if err := s.repo.RecordEnergySummary(ctx, summary); err != nil {
		return nil, err
	}

	return summary, nil
}

// GetFleetMetrics returns holistic battery health, charging status, and carbon offset analytics.
func (s *Service) GetFleetMetrics(ctx context.Context, tenantID string) (*EVFleetMetrics, error) {
	return s.repo.GetFleetMetrics(ctx, tenantID)
}

// ListRecentEnergySummaries returns recent trip energy efficiency entries.
func (s *Service) ListRecentEnergySummaries(ctx context.Context, tenantID string, limit int) ([]EVEnergySummary, error) {
	return s.repo.ListRecentEnergySummaries(ctx, tenantID, limit)
}
