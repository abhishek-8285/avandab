package application

import (
	"context"
	"errors"
	"fmt"
	"math"

	"transport-app/internal/sustainability"
)

// ESGUsecase coordinates carbon emission calculation, auditing, snapshotting, and BRSR reporting.
type ESGUsecase struct {
	repo sustainability.ESGRepository
}

// NewESGUsecase creates a new ESGUsecase.
func NewESGUsecase(repo sustainability.ESGRepository) *ESGUsecase {
	return &ESGUsecase{repo: repo}
}

// CalculateTripESG computes and persists carbon metrics for a specific trip.
func (uc *ESGUsecase) CalculateTripESG(ctx context.Context, tenantID, tripID string) (*sustainability.TripESGMetrics, error) {
	td, err := uc.repo.GetTripDetails(ctx, tenantID, tripID)
	if err != nil {
		return nil, err
	}

	norm := sustainability.DeriveEmissionNorm(td.FuelType, td.VehicleType)
	co2eKG, co2ePerTKM, meth := sustainability.CalculateEmissions(td.DistanceKM, td.PayloadTonnes, td.FuelConsumedLitres, norm)

	metrics := sustainability.TripESGMetrics{
		TenantID:           tenantID,
		TripID:             tripID,
		DistanceKM:         td.DistanceKM,
		PayloadTonnes:      td.PayloadTonnes,
		FuelConsumedLitres: td.FuelConsumedLitres,
		CO2eKG:             math.Round(co2eKG*100) / 100,
		CO2ePerTKM:         math.Round(co2ePerTKM*10000) / 10000,
		EmissionNorm:       norm,
		Methodology:        meth,
	}

	return uc.repo.RecordTripESG(ctx, metrics)
}

// GetTripCarbonCertificate delivers an audited carbon certificate for a consignment/trip.
func (uc *ESGUsecase) GetTripCarbonCertificate(ctx context.Context, tenantID, tripID string) (*sustainability.TripCarbonCertificate, error) {
	m, err := uc.repo.GetTripESG(ctx, tenantID, tripID)
	if err != nil || m == nil {
		// Not calculated yet; calculate on demand
		m, err = uc.CalculateTripESG(ctx, tenantID, tripID)
		if err != nil {
			return nil, err
		}
	}

	td, err := uc.repo.GetTripDetails(ctx, tenantID, tripID)
	if err != nil {
		return nil, err
	}

	cert := &sustainability.TripCarbonCertificate{
		TripID:             m.TripID,
		TripNumber:         td.TripNumber,
		TenantID:           m.TenantID,
		VehicleNumber:      td.VehicleNumber,
		VehicleType:        td.VehicleType,
		DistanceKM:         m.DistanceKM,
		PayloadTonnes:      m.PayloadTonnes,
		CargoTKM:           math.Round(m.DistanceKM*m.PayloadTonnes*100) / 100,
		FuelConsumedLitres: m.FuelConsumedLitres,
		CO2eKG:             m.CO2eKG,
		CO2ePerTKM:         m.CO2ePerTKM,
		EmissionNorm:       m.EmissionNorm,
		Methodology:        m.Methodology,
		CertifiedAt:        m.CreatedAt,
	}

	return cert, nil
}

// GenerateSnapshot aggregates trips for a defined calendar window into an immutable periodic snapshot.
func (uc *ESGUsecase) GenerateSnapshot(ctx context.Context, tenantID string, req sustainability.GenerateSnapshotRequest, createdBy string) (*sustainability.ESGEmissionSnapshot, error) {
	if req.PeriodStart == "" || req.PeriodEnd == "" {
		return nil, errors.New("period_start and period_end are required (YYYY-MM-DD)")
	}

	agg, err := uc.repo.AggregatePeriodTrips(ctx, tenantID, req.PeriodStart, req.PeriodEnd)
	if err != nil {
		return nil, err
	}

	snap := sustainability.ESGEmissionSnapshot{
		TenantID:        tenantID,
		PeriodStart:     req.PeriodStart,
		PeriodEnd:       req.PeriodEnd,
		TotalTrips:      agg.TotalTrips,
		TotalDistanceKM: math.Round(agg.TotalDistanceKM*100) / 100,
		TotalCargoTKM:   math.Round(agg.TotalCargoTKM*100) / 100,
		TotalFuelLitres: math.Round(agg.TotalFuelLitres*100) / 100,
		TotalCO2eKG:     math.Round(agg.TotalCO2eKG*100) / 100,
		AvgCO2ePerTKM:   math.Round(agg.AvgCO2ePerTKM*10000) / 10000,
		EVDistanceKM:    math.Round(agg.EVDistanceKM*100) / 100,
		BS6DistanceKM:   math.Round(agg.BS6DistanceKM*100) / 100,
		BS4DistanceKM:   math.Round(agg.BS4DistanceKM*100) / 100,
		CreatedBy:       createdBy,
	}

	return uc.repo.SaveSnapshot(ctx, snap)
}

// ListSnapshots retrieves existing periodic snapshots for a tenant.
func (uc *ESGUsecase) ListSnapshots(ctx context.Context, tenantID string) ([]sustainability.ESGEmissionSnapshot, error) {
	return uc.repo.ListSnapshots(ctx, tenantID)
}

// GetBRSRReport compiles the SEBI BRSR Principle 6 carbon report from periodic snapshots.
func (uc *ESGUsecase) GetBRSRReport(ctx context.Context, tenantID, year string) (*sustainability.BRSRPrinciple6Report, error) {
	snapshots, err := uc.repo.ListSnapshots(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	var totalDistance, totalCargoTKM, totalCO2eKG, greenDistance float64
	var filtered []sustainability.ESGEmissionSnapshot

	for _, s := range snapshots {
		if year != "" && (len(s.PeriodStart) < 4 || s.PeriodStart[:4] != year) {
			continue
		}
		totalDistance += s.TotalDistanceKM
		totalCargoTKM += s.TotalCargoTKM
		totalCO2eKG += s.TotalCO2eKG
		greenDistance += s.EVDistanceKM
		filtered = append(filtered, s)
	}

	repPeriod := year
	if repPeriod == "" {
		repPeriod = "All Time"
	}

	// Scope 1: Direct transport fuel emissions
	scope1Tonne := totalCO2eKG / 1000.0
	// Scope 3 Category 4 (Upstream / subcontracted transport)
	scope3TKM := (totalCO2eKG * 0.20) / 1000.0

	var intensityPerKM, intensityPerTKM float64
	if totalDistance > 0 {
		intensityPerKM = totalCO2eKG / totalDistance
	}
	if totalCargoTKM > 0 {
		intensityPerTKM = totalCO2eKG / totalCargoTKM
	}

	var greenShare float64
	if totalDistance > 0 {
		greenShare = (greenDistance / totalDistance) * 100.0
	}

	return &sustainability.BRSRPrinciple6Report{
		TenantID:             tenantID,
		ReportingPeriod:      fmt.Sprintf("FY %s", repPeriod),
		Scope1EmissionsTonne: math.Round(scope1Tonne*100) / 100,
		Scope3Category4Tonne: math.Round(scope3TKM*100) / 100,
		TotalEmissionsTonne:  math.Round((scope1Tonne+scope3TKM)*100) / 100,
		CarbonIntensityPerKM: math.Round(intensityPerKM*100) / 100,
		CarbonIntensityTKM:   math.Round(intensityPerTKM*10000) / 10000,
		TotalDistanceKM:      math.Round(totalDistance*100) / 100,
		TotalCargoTKM:        math.Round(totalCargoTKM*100) / 100,
		GreenFleetSharePct:   math.Round(greenShare*100) / 100,
		Snapshots:            filtered,
	}, nil
}
