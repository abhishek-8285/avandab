package ev

import (
	"errors"
	"strings"
	"time"
)

// Charging standards supported for commercial EV fleet.
const (
	ChargingStandardCCS2    = "CCS2"
	ChargingStandardCHAdeMO = "CHAdeMO"
	ChargingStandardGBT     = "GB/T"
	ChargingStandardType2   = "TYPE2"
)

// Charging status enum values matching migration 00176 check constraints.
const (
	ChargingStatusDischarging  = "DISCHARGING"
	ChargingStatusCharging     = "CHARGING"
	ChargingStatusFastCharging = "FAST_CHARGING"
	ChargingStatusComplete     = "COMPLETE"
	ChargingStatusNotCharging  = "NOT_CHARGING"
)

// Charger types.
const (
	ChargerTypeACSlow = "AC_SLOW"
	ChargerTypeDCFast = "DC_FAST"
)

// Charging session status.
const (
	SessionStatusInProgress  = "IN_PROGRESS"
	SessionStatusCompleted   = "COMPLETED"
	SessionStatusInterrupted = "INTERRUPTED"
)

// EVProfile represents static battery and powertrain specifications for an electric vehicle.
type EVProfile struct {
	VehicleID             string    `json:"vehicle_id"`
	TenantID              string    `json:"tenant_id"`
	BatteryCapacityKWh    float64   `json:"battery_capacity_kwh"`
	MaxRangeKM            float64   `json:"max_range_km"`
	ChargingStandard      string    `json:"charging_standard"`
	MinSafeSoC            float64   `json:"min_safe_soc"`
	NominalEfficiencyWhKM float64   `json:"nominal_efficiency_wh_km"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// EVTelemetry represents the latest live battery, charge, and electrical status of an EV.
type EVTelemetry struct {
	VehicleID        string    `json:"vehicle_id"`
	TenantID         string    `json:"tenant_id"`
	SoCPercent       float64   `json:"soc_percent"`
	SoHPercent       float64   `json:"soh_percent"`
	BatteryTempC     *float64  `json:"battery_temp_c,omitempty"`
	RemainingRangeKM float64   `json:"remaining_range_km"`
	ChargingStatus   string    `json:"charging_status"`
	ChargingPowerKW  float64   `json:"charging_power_kw"`
	CurrentDrawA     float64   `json:"current_draw_a"`
	VoltageV         float64   `json:"voltage_v"`
	LastSyncAt       time.Time `json:"last_sync_at"`
}

// ChargingSession records an individual EV battery replenishment cycle.
type ChargingSession struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	VehicleID      string     `json:"vehicle_id"`
	FacilityID     *string    `json:"facility_id,omitempty"`
	FacilityName   *string    `json:"facility_name,omitempty"`
	VehicleNumber  *string    `json:"vehicle_number,omitempty"`
	StartTime      time.Time  `json:"start_time"`
	EndTime        *time.Time `json:"end_time,omitempty"`
	StartSoC       float64    `json:"start_soc"`
	EndSoC         *float64   `json:"end_soc,omitempty"`
	EnergyAddedKWh float64    `json:"energy_added_kwh"`
	PeakPowerKW    float64    `json:"peak_power_kw"`
	ChargerType    string     `json:"charger_type"`
	EstimatedCost  float64    `json:"estimated_cost"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// EVEnergySummary aggregates distance, energy consumed, efficiency, and carbon offset for trips.
type EVEnergySummary struct {
	ID                string    `json:"id"`
	TenantID          string    `json:"tenant_id"`
	VehicleID         string    `json:"vehicle_id"`
	TripID            *string   `json:"trip_id,omitempty"`
	DistanceKM        float64   `json:"distance_km"`
	EnergyConsumedKWh float64   `json:"energy_consumed_kwh"`
	EfficiencyWhKM    float64   `json:"efficiency_wh_km"`
	CO2AvoidedKG      float64   `json:"co2_avoided_kg"`
	RecordedAt        time.Time `json:"recorded_at"`
}

// EVVehicleStatus aggregates vehicle metadata, EV profile, and live telemetry for display.
type EVVehicleStatus struct {
	VehicleID          string           `json:"vehicle_id"`
	RegistrationNumber string           `json:"registration_number"`
	VehicleNumber      string           `json:"vehicle_number"`
	VehicleType        string           `json:"vehicle_type"`
	FuelType           string           `json:"fuel_type"`
	Status             string           `json:"status"`
	Profile            *EVProfile       `json:"profile,omitempty"`
	Telemetry          *EVTelemetry     `json:"telemetry,omitempty"`
	ActiveSession      *ChargingSession `json:"active_session,omitempty"`
	IsLowBattery       bool             `json:"is_low_battery"`
}

// EVFleetMetrics holds top-level fleet sustainability and battery health analytics.
type EVFleetMetrics struct {
	TotalEVs             int     `json:"total_evs"`
	ActiveChargingCount  int     `json:"active_charging_count"`
	FleetAvgSoC          float64 `json:"fleet_avg_soc"`
	FleetAvgSoH          float64 `json:"fleet_avg_soh"`
	LowBatteryAlerts     int     `json:"low_battery_alerts"`
	TotalGreenDistanceKM float64 `json:"total_green_distance_km"`
	TotalEnergyKWh       float64 `json:"total_energy_kwh"`
	TotalCO2AvoidedKG    float64 `json:"total_co2_avoided_kg"`
	AvgEfficiencyWhKM    float64 `json:"avg_efficiency_wh_km"`
}

// ConfigureEVProfileInput is used to register or update EV battery specs.
type ConfigureEVProfileInput struct {
	BatteryCapacityKWh    float64 `json:"battery_capacity_kwh"`
	MaxRangeKM            float64 `json:"max_range_km"`
	ChargingStandard      string  `json:"charging_standard"`
	MinSafeSoC            float64 `json:"min_safe_soc"`
	NominalEfficiencyWhKM float64 `json:"nominal_efficiency_wh_km"`
}

func (in *ConfigureEVProfileInput) Validate() error {
	if in.BatteryCapacityKWh <= 0 {
		return errors.New("battery_capacity_kwh must be greater than 0")
	}
	if in.MaxRangeKM <= 0 {
		return errors.New("max_range_km must be greater than 0")
	}
	if in.MinSafeSoC < 0 || in.MinSafeSoC > 100 {
		return errors.New("min_safe_soc must be between 0 and 100")
	}
	if in.NominalEfficiencyWhKM <= 0 {
		in.NominalEfficiencyWhKM = 140.0 // Default 140 Wh/km
	}
	if in.ChargingStandard == "" {
		in.ChargingStandard = ChargingStandardCCS2
	}
	return nil
}

// IngestEVTelemetryInput receives real-time CAN/OEM/tracker telemetry frames.
type IngestEVTelemetryInput struct {
	VehicleID        string     `json:"vehicle_id"`
	SoCPercent       float64    `json:"soc_percent"`
	SoHPercent       float64    `json:"soh_percent"`
	BatteryTempC     *float64   `json:"battery_temp_c,omitempty"`
	RemainingRangeKM float64    `json:"remaining_range_km"`
	ChargingStatus   string     `json:"charging_status"`
	ChargingPowerKW  float64    `json:"charging_power_kw"`
	CurrentDrawA     float64    `json:"current_draw_a"`
	VoltageV         float64    `json:"voltage_v"`
	Timestamp        *time.Time `json:"timestamp,omitempty"`
}

func (in *IngestEVTelemetryInput) Validate() error {
	if strings.TrimSpace(in.VehicleID) == "" {
		return errors.New("vehicle_id is required")
	}
	if in.SoCPercent < 0 || in.SoCPercent > 100 {
		return errors.New("soc_percent must be between 0 and 100")
	}
	// Measured battery health is required — a missing reading must be refused,
	// never defaulted to 100.0 (P0 "stop fake data"; migration 00176's schema
	// default is deferred debt, see plan row 3.10).
	if in.SoHPercent <= 0 || in.SoHPercent > 100 {
		return errors.New("soh_percent is required from the CAN/OEM feed (measured battery health, 0-100)")
	}
	status := strings.ToUpper(strings.TrimSpace(in.ChargingStatus))
	switch status {
	case ChargingStatusDischarging, ChargingStatusCharging, ChargingStatusFastCharging, ChargingStatusComplete, ChargingStatusNotCharging:
		in.ChargingStatus = status
	case "":
		in.ChargingStatus = ChargingStatusDischarging
	default:
		return errors.New("invalid charging_status: " + in.ChargingStatus)
	}
	return nil
}

// RecordEnergySummaryInput creates an energy consumption and carbon offset record.
type RecordEnergySummaryInput struct {
	VehicleID         string  `json:"vehicle_id"`
	TripID            *string `json:"trip_id,omitempty"`
	DistanceKM        float64 `json:"distance_km"`
	EnergyConsumedKWh float64 `json:"energy_consumed_kwh"`
}

func (in *RecordEnergySummaryInput) Validate() error {
	if strings.TrimSpace(in.VehicleID) == "" {
		return errors.New("vehicle_id is required")
	}
	if in.DistanceKM < 0 {
		return errors.New("distance_km cannot be negative")
	}
	return nil
}
