package coldchain

import (
	"errors"
	"math"
	"strings"
	"time"
)

// Reefer zone commodity classifications.
const (
	ZoneTypeFrozen    = "FROZEN"  // -18°C to -25°C (Ice cream, meat, seafood)
	ZoneTypeChilled   = "CHILLED" // 2°C to 8°C (Vaccines, pharma, biologics, dairy)
	ZoneTypeAmbient   = "AMBIENT" // 15°C to 25°C (Room temp pharma, chocolate)
	ZoneTypeMultiTemp = "MULTI_TEMP"
)

// Reefer operating power status.
const (
	PowerStatusOn      = "ON"
	PowerStatusOff     = "OFF"
	PowerStatusStandby = "STANDBY"
	PowerStatusFault   = "FAULT"
)

// Reefer thermodynamic modes.
const (
	ModeCooling = "COOLING"
	ModeHeating = "HEATING"
	ModeDefrost = "DEFROST"
	ModeIdle    = "IDLE"
	ModeOff     = "OFF"
)

// Cargo door reed-switch status.
const (
	DoorClosed = "CLOSED"
	DoorOpen   = "OPEN"
)

// Excursion classifications.
const (
	ExcursionOverTemp          = "OVER_TEMP"
	ExcursionUnderTemp         = "UNDER_TEMP"
	ExcursionDoorProlongedOpen = "DOOR_PROLONGED_OPEN"
	ExcursionSensorFault       = "SENSOR_FAULT"
)

// Incident severity levels.
const (
	SeverityWarning  = "WARNING"
	SeverityCritical = "CRITICAL"
)

// Excursion resolution lifecycle.
const (
	ExcursionOpen         = "OPEN"
	ExcursionAcknowledged = "ACKNOWLEDGED"
	ExcursionResolved     = "RESOLVED"
)

// HACCP / GDP regulatory audit status.
const (
	ComplianceCompliant = "COMPLIANT"
	ComplianceBreached  = "BREACHED"
)

// ReeferProfile stores static configuration, multi-zone boundaries, and defrost policy.
type ReeferProfile struct {
	VehicleID          string    `json:"vehicle_id"`
	TenantID           string    `json:"tenant_id"`
	ReeferMakeModel    string    `json:"reefer_make_model"`
	NumZones           int       `json:"num_zones"`
	Zone1Type          string    `json:"zone1_type"`
	Zone1TargetTempC   float64   `json:"zone1_target_temp_c"`
	Zone1MinTempC      float64   `json:"zone1_min_temp_c"`
	Zone1MaxTempC      float64   `json:"zone1_max_temp_c"`
	Zone2Type          *string   `json:"zone2_type,omitempty"`
	Zone2TargetTempC   *float64  `json:"zone2_target_temp_c,omitempty"`
	Zone2MinTempC      *float64  `json:"zone2_min_temp_c,omitempty"`
	Zone2MaxTempC      *float64  `json:"zone2_max_temp_c,omitempty"`
	HumidityMonitoring bool      `json:"humidity_monitoring"`
	TargetHumidityRH   *float64  `json:"target_humidity_rh,omitempty"`
	DefrostMode        string    `json:"defrost_mode"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// ReeferTelemetry captures real-time multi-zone probe readings and door integrity.
type ReeferTelemetry struct {
	VehicleID           string    `json:"vehicle_id"`
	TenantID            string    `json:"tenant_id"`
	ReeferPowerStatus   string    `json:"reefer_power_status"`
	ReeferMode          string    `json:"reefer_mode"`
	Zone1TempC          float64   `json:"zone1_temp_c"`
	Zone1SetpointC      float64   `json:"zone1_setpoint_c"`
	Zone2TempC          *float64  `json:"zone2_temp_c,omitempty"`
	Zone2SetpointC      *float64  `json:"zone2_setpoint_c,omitempty"`
	HumidityRH          *float64  `json:"humidity_rh,omitempty"`
	DoorStatus          string    `json:"door_status"`
	DoorOpenDurationSec int       `json:"door_open_duration_sec"`
	ReeferFuelLevelPct  float64   `json:"reefer_fuel_level_pct"`
	ReeferBatteryV      float64   `json:"reefer_battery_v"`
	HasActiveExcursion  bool      `json:"has_active_excursion"`
	LastSyncAt          time.Time `json:"last_sync_at"`
}

// TemperatureExcursion tracks out-of-spec thermal excursions and prolonged door open alarms.
type TemperatureExcursion struct {
	ID              string     `json:"id"`
	TenantID        string     `json:"tenant_id"`
	VehicleID       string     `json:"vehicle_id"`
	VehicleNumber   *string    `json:"vehicle_number,omitempty"`
	TripID          *string    `json:"trip_id,omitempty"`
	ZoneNumber      int        `json:"zone_number"`
	ExcursionType   string     `json:"excursion_type"`
	Severity        string     `json:"severity"`
	StartTime       time.Time  `json:"start_time"`
	EndTime         *time.Time `json:"end_time,omitempty"`
	DurationSeconds int        `json:"duration_seconds"`
	PeakTempC       float64    `json:"peak_temp_c"`
	ThresholdTempC  float64    `json:"threshold_temp_c"`
	SetpointC       float64    `json:"setpoint_c"`
	Status          string     `json:"status"`
	ResolutionNote  string     `json:"resolution_note"`
	ResolvedBy      string     `json:"resolved_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ReeferTripSummary documents trip-level cold chain performance and GDP pharma audit MKT.
type ReeferTripSummary struct {
	ID                        string     `json:"id"`
	TenantID                  string     `json:"tenant_id"`
	TripID                    string     `json:"trip_id"`
	VehicleID                 string     `json:"vehicle_id"`
	StartTime                 time.Time  `json:"start_time"`
	EndTime                   time.Time  `json:"end_time"`
	Zone1MinTempC             float64    `json:"zone1_min_temp_c"`
	Zone1MaxTempC             float64    `json:"zone1_max_temp_c"`
	Zone1MeanKineticTempC     float64    `json:"zone1_mean_kinetic_temp_c"`
	TotalExcursionDurationSec int        `json:"total_excursion_duration_sec"`
	DoorOpenCount             int        `json:"door_open_count"`
	ComplianceStatus          string     `json:"compliance_status"`
	CertifiedAt               *time.Time `json:"certified_at,omitempty"`
	CertifiedBy               string     `json:"certified_by"`
	CreatedAt                 time.Time  `json:"created_at"`
}

// ColdChainVehicleStatus combines vehicle metadata, reefer configuration, and telemetry for the UI.
type ColdChainVehicleStatus struct {
	VehicleID             string           `json:"vehicle_id"`
	RegistrationNumber    string           `json:"registration_number"`
	VehicleNumber         string           `json:"vehicle_number"`
	VehicleType           string           `json:"vehicle_type"`
	Status                string           `json:"status"`
	Profile               *ReeferProfile   `json:"profile,omitempty"`
	Telemetry             *ReeferTelemetry `json:"telemetry,omitempty"`
	ActiveExcursionsCount int              `json:"active_excursions_count"`
	IsInExcursion         bool             `json:"is_in_excursion"`
	IsDoorOpen            bool             `json:"is_door_open"`
}

// ColdChainFleetMetrics summarizes fleet-wide cold chain health and regulatory metrics.
type ColdChainFleetMetrics struct {
	TotalReefers           int     `json:"total_reefers"`
	ActiveCoolingCount     int     `json:"active_cooling_count"`
	InExcursionCount       int     `json:"in_excursion_count"`
	OpenExcursionsCount    int     `json:"open_excursions_count"`
	DoorOpenAlerts         int     `json:"door_open_alerts"`
	FleetComplianceRatePct float64 `json:"fleet_compliance_rate_pct"`
	AvgZone1TempC          float64 `json:"avg_zone1_temp_c"`
}

// ConfigureReeferProfileInput configures thermal limits and reefer specifications.
type ConfigureReeferProfileInput struct {
	ReeferMakeModel    string   `json:"reefer_make_model"`
	NumZones           int      `json:"num_zones"`
	Zone1Type          string   `json:"zone1_type"`
	Zone1TargetTempC   float64  `json:"zone1_target_temp_c"`
	Zone1MinTempC      float64  `json:"zone1_min_temp_c"`
	Zone1MaxTempC      float64  `json:"zone1_max_temp_c"`
	Zone2Type          *string  `json:"zone2_type,omitempty"`
	Zone2TargetTempC   *float64 `json:"zone2_target_temp_c,omitempty"`
	Zone2MinTempC      *float64 `json:"zone2_min_temp_c,omitempty"`
	Zone2MaxTempC      *float64 `json:"zone2_max_temp_c,omitempty"`
	HumidityMonitoring bool     `json:"humidity_monitoring"`
	TargetHumidityRH   *float64 `json:"target_humidity_rh,omitempty"`
	DefrostMode        string   `json:"defrost_mode"`
}

func (in *ConfigureReeferProfileInput) Validate() error {
	if in.NumZones < 1 || in.NumZones > 3 {
		in.NumZones = 1
	}
	if in.Zone1MinTempC > in.Zone1MaxTempC {
		return errors.New("zone1_min_temp_c cannot be greater than zone1_max_temp_c")
	}
	if in.Zone1Type == "" {
		in.Zone1Type = ZoneTypeChilled
	}
	if in.ReeferMakeModel == "" {
		in.ReeferMakeModel = "Carrier Transicold"
	}
	if in.DefrostMode == "" {
		in.DefrostMode = "AUTO"
	}
	if in.Zone2Type != nil && in.Zone2MinTempC != nil && in.Zone2MaxTempC != nil {
		if *in.Zone2MinTempC > *in.Zone2MaxTempC {
			return errors.New("zone2_min_temp_c cannot be greater than zone2_max_temp_c")
		}
	}
	return nil
}

// IngestReeferTelemetryInput accepts real-time reefer controller and sensor frames.
type IngestReeferTelemetryInput struct {
	VehicleID           string     `json:"vehicle_id"`
	TripID              *string    `json:"trip_id,omitempty"`
	ReeferPowerStatus   string     `json:"reefer_power_status"`
	ReeferMode          string     `json:"reefer_mode"`
	Zone1TempC          float64    `json:"zone1_temp_c"`
	Zone1SetpointC      float64    `json:"zone1_setpoint_c"`
	Zone2TempC          *float64   `json:"zone2_temp_c,omitempty"`
	Zone2SetpointC      *float64   `json:"zone2_setpoint_c,omitempty"`
	HumidityRH          *float64   `json:"humidity_rh,omitempty"`
	DoorStatus          string     `json:"door_status"`
	DoorOpenDurationSec int        `json:"door_open_duration_sec"`
	ReeferFuelLevelPct  float64    `json:"reefer_fuel_level_pct"`
	ReeferBatteryV      float64    `json:"reefer_battery_v"`
	Timestamp           *time.Time `json:"timestamp,omitempty"`
}

func (in *IngestReeferTelemetryInput) Validate() error {
	if strings.TrimSpace(in.VehicleID) == "" {
		return errors.New("vehicle_id is required")
	}
	status := strings.ToUpper(strings.TrimSpace(in.ReeferPowerStatus))
	if status == "" {
		status = PowerStatusOn
	}
	in.ReeferPowerStatus = status

	mode := strings.ToUpper(strings.TrimSpace(in.ReeferMode))
	if mode == "" {
		mode = ModeCooling
	}
	in.ReeferMode = mode

	door := strings.ToUpper(strings.TrimSpace(in.DoorStatus))
	if door == "" {
		door = DoorClosed
	}
	in.DoorStatus = door

	if in.ReeferFuelLevelPct <= 0 {
		in.ReeferFuelLevelPct = 100.0
	}
	if in.ReeferBatteryV <= 0 {
		in.ReeferBatteryV = 12.6
	}
	return nil
}

// ResolveExcursionInput records corrective actions for temperature breaches.
type ResolveExcursionInput struct {
	ResolutionNote string `json:"resolution_note"`
	ResolvedBy     string `json:"resolved_by"`
}

func (in *ResolveExcursionInput) Validate() error {
	if strings.TrimSpace(in.ResolutionNote) == "" {
		return errors.New("resolution_note is required")
	}
	return nil
}

// CertifyTripComplianceInput generates HACCP GDP regulatory seal for a completed trip.
type CertifyTripComplianceInput struct {
	TripID      string `json:"trip_id"`
	VehicleID   string `json:"vehicle_id"`
	CertifiedBy string `json:"certified_by"`
}

func (in *CertifyTripComplianceInput) Validate() error {
	if strings.TrimSpace(in.TripID) == "" {
		return errors.New("trip_id is required")
	}
	if strings.TrimSpace(in.VehicleID) == "" {
		return errors.New("vehicle_id is required")
	}
	return nil
}

// CalculateMKT computes Mean Kinetic Temperature in Celsius based on Arrhenius equation (USP <1150>).
// Constant deltaH / R = 10,000 K (approx 83.144 kJ/mol activation energy).
func CalculateMKT(tempsC []float64) float64 {
	if len(tempsC) == 0 {
		return 0.0
	}
	const dHR = 10000.0 // deltaH / R in Kelvin
	var sumExp float64
	for _, t := range tempsC {
		kelvin := t + 273.15
		if kelvin <= 0 {
			kelvin = 1.0 // Guard against division by zero
		}
		sumExp += math.Exp(-dHR / kelvin)
	}
	meanExp := sumExp / float64(len(tempsC))
	if meanExp <= 0 {
		return 0.0
	}
	mktKelvin := dHR / (-math.Log(meanExp))
	return math.Round((mktKelvin-273.15)*100) / 100
}
