package sustainability

import (
	"time"
)

// EmissionNorm defines regulatory vehicle emission standards in India.
type EmissionNorm string

const (
	EmissionNormBS3   EmissionNorm = "BS3"
	EmissionNormBS4   EmissionNorm = "BS4"
	EmissionNormBS6   EmissionNorm = "BS6"
	EmissionNormEV    EmissionNorm = "EV"
	EmissionNormCNG   EmissionNorm = "CNG"
	EmissionNormOther EmissionNorm = "OTHER"
)

// EmissionMethodology defines GHG Protocol carbon accounting calculation methods.
type EmissionMethodology string

const (
	MethodologyFuelPrimary      EmissionMethodology = "FUEL_PRIMARY"      // Fuel consumed * emission factor
	MethodologyDistanceActivity EmissionMethodology = "DISTANCE_ACTIVITY" // Tonne-km * norm activity factor
	MethodologyDefaultFactor    EmissionMethodology = "DEFAULT_FACTOR"    // Fleet average fallback
)

// Standard GHG Protocol emission coefficients.
const (
	// Fuel Emission Factors (kg CO2e per Litre / kg)
	FactorDieselKGPerLitre = 2.68 // High-Speed Diesel (HSD)
	FactorPetrolKGPerLitre = 2.31 // Motor Spirit (Petrol)
	FactorCNGKGPerKG       = 2.75 // Compressed Natural Gas

	// Activity Emission Factors (kg CO2e per Tonne-KM)
	FactorBS3KGPerTKM = 0.135 // BS-III Heavy Commercial Vehicles (135 g)
	FactorBS4KGPerTKM = 0.115 // BS-IV Heavy Commercial Vehicles (115 g)
	FactorBS6KGPerTKM = 0.092 // BS-VI Heavy Commercial Vehicles (92 g)
	FactorEVKGPerTKM  = 0.000 // Zero direct tailpipe emissions
)

// TripESGMetrics stores trip-level carbon audit details (Spec 20 §3, B11).
type TripESGMetrics struct {
	ID                 string              `json:"id"`
	TenantID           string              `json:"tenant_id"`
	TripID             string              `json:"trip_id"`
	DistanceKM         float64             `json:"distance_km"`
	PayloadTonnes      float64             `json:"payload_tonnes"`
	FuelConsumedLitres float64             `json:"fuel_consumed_litres"`
	CO2eKG             float64             `json:"co2e_kg"`
	CO2ePerTKM         float64             `json:"co2e_per_tkm"`
	EmissionNorm       EmissionNorm        `json:"emission_norm"`
	Methodology        EmissionMethodology `json:"methodology"`
	CreatedAt          time.Time           `json:"created_at"`
}

// ESGEmissionSnapshot captures periodic carbon totals for Scope 3 / BRSR reporting.
type ESGEmissionSnapshot struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenant_id"`
	PeriodStart     string    `json:"period_start"` // YYYY-MM-DD
	PeriodEnd       string    `json:"period_end"`   // YYYY-MM-DD
	TotalTrips      int       `json:"total_trips"`
	TotalDistanceKM float64   `json:"total_distance_km"`
	TotalCargoTKM   float64   `json:"total_cargo_tkm"`
	TotalFuelLitres float64   `json:"total_fuel_litres"`
	TotalCO2eKG     float64   `json:"total_co2e_kg"`
	AvgCO2ePerTKM   float64   `json:"avg_co2e_per_tkm"`
	EVDistanceKM    float64   `json:"ev_distance_km"`
	BS6DistanceKM   float64   `json:"bs6_distance_km"`
	BS4DistanceKM   float64   `json:"bs4_distance_km"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
}

// TripCarbonCertificate provides a customer-ready carbon audit certificate for a trip.
type TripCarbonCertificate struct {
	TripID             string              `json:"trip_id"`
	TripNumber         string              `json:"trip_number"`
	TenantID           string              `json:"tenant_id"`
	VehicleNumber      string              `json:"vehicle_number"`
	VehicleType        string              `json:"vehicle_type"`
	DistanceKM         float64             `json:"distance_km"`
	PayloadTonnes      float64             `json:"payload_tonnes"`
	CargoTKM           float64             `json:"cargo_tkm"`
	FuelConsumedLitres float64             `json:"fuel_consumed_litres"`
	CO2eKG             float64             `json:"co2e_kg"`
	CO2ePerTKM         float64             `json:"co2e_per_tkm"`
	EmissionNorm       EmissionNorm        `json:"emission_norm"`
	Methodology        EmissionMethodology `json:"methodology"`
	CertifiedAt        time.Time           `json:"certified_at"`
}

// BRSRPrinciple6Report formats carbon data for SEBI BRSR Core compliance.
type BRSRPrinciple6Report struct {
	TenantID             string                `json:"tenant_id"`
	ReportingPeriod      string                `json:"reporting_period"`
	Scope1EmissionsTonne float64               `json:"scope1_emissions_tonne"`
	Scope3Category4Tonne float64               `json:"scope3_cat4_emissions_tonne"`
	TotalEmissionsTonne  float64               `json:"total_emissions_tonne"`
	CarbonIntensityPerKM float64               `json:"carbon_intensity_kg_per_km"`
	CarbonIntensityTKM   float64               `json:"carbon_intensity_kg_per_tkm"`
	TotalDistanceKM      float64               `json:"total_distance_km"`
	TotalCargoTKM        float64               `json:"total_cargo_tkm"`
	GreenFleetSharePct   float64               `json:"green_fleet_share_percentage"`
	Snapshots            []ESGEmissionSnapshot `json:"snapshots"`
}

// GenerateSnapshotRequest captures input parameters for generating a periodic snapshot.
type GenerateSnapshotRequest struct {
	PeriodStart string `json:"period_start"` // YYYY-MM-DD
	PeriodEnd   string `json:"period_end"`   // YYYY-MM-DD
}

// CalculateEmissions computes CO2e emissions using primary or activity methodology.
// Ensures zero-division safe output (no NaN or Inf).
func CalculateEmissions(distanceKM, payloadTonnes, fuelLitres float64, norm EmissionNorm) (co2eKG float64, co2ePerTKM float64, method EmissionMethodology) {
	if distanceKM < 0 {
		distanceKM = 0
	}
	if payloadTonnes < 0 {
		payloadTonnes = 0
	}
	if fuelLitres < 0 {
		fuelLitres = 0
	}

	cargoTKM := distanceKM * payloadTonnes

	// Primary Method: Fuel-based
	if fuelLitres > 0 {
		factor := FactorDieselKGPerLitre
		if norm == EmissionNormCNG {
			factor = FactorCNGKGPerKG
		}
		co2eKG = fuelLitres * factor
		method = MethodologyFuelPrimary
	} else {
		// Secondary Method: Distance-Tonne-Km Activity
		var factor float64
		switch norm {
		case EmissionNormEV:
			factor = FactorEVKGPerTKM
		case EmissionNormBS6:
			factor = FactorBS6KGPerTKM
		case EmissionNormBS4:
			factor = FactorBS4KGPerTKM
		case EmissionNormBS3:
			factor = FactorBS3KGPerTKM
		default:
			factor = FactorBS4KGPerTKM // Conservative baseline
		}

		co2eKG = cargoTKM * factor
		method = MethodologyDistanceActivity
	}

	// Zero-division guard: ensure co2ePerTKM is never NaN or Inf
	if cargoTKM > 0 {
		co2ePerTKM = co2eKG / cargoTKM
	} else {
		co2ePerTKM = 0
	}

	return co2eKG, co2ePerTKM, method
}
