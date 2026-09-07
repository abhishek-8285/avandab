package aggregate

import (
	"errors"
	"time"

	"transport-app/internal/shared"
)

type VehicleID string
type VehicleType string
type FuelType string
type VehicleStatus string

const (
	VehicleTypeTruck     VehicleType = "truck"
	VehicleTypeMiniTruck VehicleType = "mini_truck"
	VehicleTypeBus       VehicleType = "bus"
	VehicleTypeVan       VehicleType = "van"
	VehicleTypePickup    VehicleType = "pickup"
	VehicleTypeTempo     VehicleType = "tempo"

	FuelTypeDiesel   FuelType = "diesel"
	FuelTypePetrol   FuelType = "petrol"
	FuelTypeGas      FuelType = "gas"
	FuelTypeElectric FuelType = "electric"
	FuelTypeCNG      FuelType = "cng"

	VehicleAvailable   VehicleStatus = "available"
	VehicleRunning     VehicleStatus = "running"
	VehicleMaintenance VehicleStatus = "maintenance"
	VehicleInactive    VehicleStatus = "inactive"
	VehicleBlocked     VehicleStatus = "blocked"
)

// FleetClass is the SOP fleet-object type (TMS_SOP IE31 screen, p.2).
// NOTE: the Vehicle Master report (p.18) swaps the labels — there "Vehicle
// Type" is ownership and "Vehicle Category" is the fleet class.
type FleetClass string

const (
	FleetClassCV  FleetClass = "CV"
	FleetClassPV  FleetClass = "PV"
	FleetClassFS  FleetClass = "FS"
	FleetClassOFS FleetClass = "OFS"
)

// Ownership is the SOP equipment category (TMS_SOP IE31 screen, p.2).
type Ownership string

const (
	OwnershipOwn      Ownership = "O"
	OwnershipContract Ownership = "C"
	OwnershipFuel     Ownership = "F"
	OwnershipMachine  Ownership = "M"
)

// VehicleProfile holds TMS SOP fleet-object master data: the General,
// Organization, Vehicle Details and Vehicle Technology tabs (pp.3-4
// screenshots) plus the Vehicle Master report columns (p.18).
// Zero value = unregistered (contractual vehicles may leave the
// Organization block empty, per SOP p.2).
type VehicleProfile struct {
	FleetClass          FleetClass `json:"fleet_class"`
	Ownership           Ownership  `json:"ownership"`
	FleetNumber         string     `json:"fleet_number"`
	Description         string     `json:"description"`
	Manufacturer        string     `json:"manufacturer"`
	ManufCountry        string     `json:"manuf_country"`
	Model               string     `json:"model"`
	ConstrYearMonth     string     `json:"constr_year_month"`
	AcquisitionValue    *float64   `json:"acquisition_value"`
	AcquisitionCurrency string     `json:"acquisition_currency"`
	AcquisitionDate     *time.Time `json:"acquisition_date"`
	PurchaseVendor      string     `json:"purchase_vendor"`
	ValidFrom           *time.Time `json:"valid_from"`
	ValidTo             *time.Time `json:"valid_to"`
	FacilityID          string     `json:"facility_id"`
	MaintPlant          string     `json:"maint_plant"`
	PlanningPlant       string     `json:"planning_plant"`
	CompanyCode         string     `json:"company_code"`
	BusinessArea        string     `json:"business_area"`
	CostCenter          string     `json:"cost_center"`
	AssetNo             string     `json:"asset_no"`
	FleetObjectNo       string     `json:"fleet_object_no"`
	ChassisNo           string     `json:"chassis_no"`
	VehicleCategory     string     `json:"vehicle_category"`
	EngineNumber        string     `json:"engine_number"`
	EnginePower         string     `json:"engine_power"`
	EngineCapacity      string     `json:"engine_capacity"`
	CylinderCount       *int64     `json:"cylinder_count"`
	MaxSpeed            *float64   `json:"max_speed"`
	Weight              *float64   `json:"weight"`
	WeightUnit          string     `json:"weight_unit"`
	LoadVolume          *float64   `json:"load_volume"`
	VolumeUnit          string     `json:"volume_unit"`
	SecondaryFuel       string     `json:"secondary_fuel"`
	UsageIndicator      string     `json:"usage_indicator"`
}

// Normalized returns the profile with SOP defaults filled for unset
// classification fields. The repository applies this on every Save so that
// aggregates built without ApplyProfile (tests, legacy paths) persist the
// SOP defaults instead of tripping the DB CHECKs with empty strings.
func (p VehicleProfile) Normalized() VehicleProfile {
	if p.FleetClass == "" {
		p.FleetClass = FleetClassCV
	}
	if p.Ownership == "" {
		p.Ownership = OwnershipOwn
	}
	if p.AcquisitionCurrency == "" {
		p.AcquisitionCurrency = "INR"
	}
	if p.WeightUnit == "" {
		p.WeightUnit = "TO"
	}
	return p
}

// normalizeProfile applies SOP defaults for unset classification fields.
func normalizeProfile(p VehicleProfile) VehicleProfile {
	return p.Normalized()
}

// ValidateProfile rejects values the DB CHECKs would reject, so callers get
// domain errors instead of SQLite constraint failures.
func ValidateProfile(p VehicleProfile) error {
	switch normalizeProfile(p).FleetClass {
	case FleetClassCV, FleetClassPV, FleetClassFS, FleetClassOFS:
	default:
		return errors.New("invalid fleet class (want CV, PV, FS or OFS)")
	}
	switch normalizeProfile(p).Ownership {
	case OwnershipOwn, OwnershipContract, OwnershipFuel, OwnershipMachine:
	default:
		return errors.New("invalid ownership (want O, C, F or M)")
	}
	switch normalizeProfile(p).WeightUnit {
	case "TO", "KG":
	default:
		return errors.New("invalid weight unit (want TO or KG)")
	}
	return nil
}

// VehicleAggregate is the aggregate root representing a vehicle.
type VehicleAggregate struct {
	ID                 VehicleID
	TenantID           shared.TenantID
	RegistrationNumber string
	VehicleNumber      string
	VehicleType        VehicleType
	Capacity           int64
	FuelType           FuelType
	InsuranceExpiry    time.Time
	FitnessExpiry      time.Time
	PermitExpiry       time.Time
	Status             VehicleStatus
	CurrentMileage     *float64
	Profile            VehicleProfile
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Version            int64
	events             []any
}

// NewVehicleAggregate constructs a new VehicleAggregate and records a created event.
func NewVehicleAggregate(
	id VehicleID,
	tenantID shared.TenantID,
	registrationNumber string,
	vehicleNumber string,
	vehicleType VehicleType,
	capacity int64,
	fuelType FuelType,
	insuranceExpiry time.Time,
	fitnessExpiry time.Time,
	permitExpiry time.Time,
	status VehicleStatus,
	currentMileage *float64,
	now time.Time,
) *VehicleAggregate {
	v := &VehicleAggregate{
		ID:                 id,
		TenantID:           tenantID,
		RegistrationNumber: registrationNumber,
		VehicleNumber:      vehicleNumber,
		VehicleType:        vehicleType,
		Capacity:           capacity,
		FuelType:           fuelType,
		InsuranceExpiry:    insuranceExpiry,
		FitnessExpiry:      fitnessExpiry,
		PermitExpiry:       permitExpiry,
		Status:             status,
		CurrentMileage:     currentMileage,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	v.events = append(v.events, VehicleCreatedEvent{
		ID:                 id,
		TenantID:           tenantID,
		RegistrationNumber: registrationNumber,
		VehicleNumber:      vehicleNumber,
		CreatedAt:          now,
	})

	return v
}

// Events returns recorded domain events.
func (a *VehicleAggregate) Events() []any {
	return a.events
}

// ClearEvents clears the recorded events list.
func (a *VehicleAggregate) ClearEvents() {
	a.events = nil
}

// UpdateDetails updates vehicle properties and records an updated event.
func (a *VehicleAggregate) UpdateDetails(
	registrationNumber string,
	vehicleNumber string,
	vehicleType VehicleType,
	capacity int64,
	fuelType FuelType,
	insuranceExpiry time.Time,
	fitnessExpiry time.Time,
	permitExpiry time.Time,
	status VehicleStatus,
	currentMileage *float64,
	now time.Time,
) error {
	if registrationNumber == "" || vehicleNumber == "" {
		return errors.New("registration and vehicle number are required")
	}

	a.RegistrationNumber = registrationNumber
	a.VehicleNumber = vehicleNumber
	a.VehicleType = vehicleType
	a.Capacity = capacity
	a.FuelType = fuelType
	a.InsuranceExpiry = insuranceExpiry
	a.FitnessExpiry = fitnessExpiry
	a.PermitExpiry = permitExpiry
	a.Status = status
	a.CurrentMileage = currentMileage
	a.UpdatedAt = now

	a.events = append(a.events, VehicleUpdatedEvent{
		ID:                 a.ID,
		TenantID:           a.TenantID,
		Status:             status,
		RegistrationNumber: registrationNumber,
		UpdatedAt:          now,
	})

	return nil
}

// ApplyProfile sets the SOP fleet-object master data. It validates the
// classification fields (fleet class / ownership / weight unit) and fills
// SOP defaults for unset values. It records no event of its own: callers
// always invoke it alongside NewVehicleAggregate / UpdateDetails, whose
// created/updated events already cover the mutation.
func (a *VehicleAggregate) ApplyProfile(p VehicleProfile, now time.Time) error {
	if err := ValidateProfile(p); err != nil {
		return err
	}

	a.Profile = normalizeProfile(p)
	a.UpdatedAt = now

	return nil
}

// VehicleCreatedEvent emitted when a vehicle is registered.
type VehicleCreatedEvent struct {
	ID                 VehicleID
	TenantID           shared.TenantID
	RegistrationNumber string
	VehicleNumber      string
	CreatedAt          time.Time
}

// VehicleUpdatedEvent emitted when vehicle details change.
type VehicleUpdatedEvent struct {
	ID                 VehicleID
	TenantID           shared.TenantID
	Status             VehicleStatus
	RegistrationNumber string
	UpdatedAt          time.Time
}

// VehicleDeletedEvent emitted when a vehicle is removed from the registry.
type VehicleDeletedEvent struct {
	ID                 VehicleID
	TenantID           shared.TenantID
	RegistrationNumber string
	DeletedAt          time.Time
}

// RecordDeletion appends the deletion event. Callers persist via Delete.
func (a *VehicleAggregate) RecordDeletion(now time.Time) {
	a.events = append(a.events, VehicleDeletedEvent{
		ID:                 a.ID,
		TenantID:           a.TenantID,
		RegistrationNumber: a.RegistrationNumber,
		DeletedAt:          now,
	})
}
