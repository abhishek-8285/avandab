package converters

import (
	"database/sql"
	"time"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/shared"
	"transport-app/internal/vehicle/domain"
	"transport-app/internal/vehicle/domain/aggregate"
)

// ToDomain converts db.Vehicle to *aggregate.VehicleAggregate.
func ToDomain(v db.Vehicle) *aggregate.VehicleAggregate {
	agg := aggregate.NewVehicleAggregate(
		aggregate.VehicleID(v.ID),
		shared.TenantID(v.TenantID),
		v.RegistrationNumber,
		v.VehicleNumber,
		aggregate.VehicleType(v.VehicleType),
		v.Capacity,
		aggregate.FuelType(v.FuelType),
		v.InsuranceExpiry,
		v.FitnessExpiry,
		v.PermitExpiry,
		aggregate.VehicleStatus(v.Status),
		getFloat64Pointer(v.CurrentMileage),
		v.CreatedAt,
	)
	agg.Profile = ProfileFromDB(v)
	return agg
}

// ProfileFromDB maps the SOP fleet-object columns of db.Vehicle onto
// aggregate.VehicleProfile. Rows written before 00126 (or by the legacy
// stack) carry zero values, which normalize to the SOP defaults.
func ProfileFromDB(v db.Vehicle) aggregate.VehicleProfile {
	p := aggregate.VehicleProfile{
		FleetClass:          aggregate.FleetClass(v.FleetClass),
		Ownership:           aggregate.Ownership(v.Ownership),
		FleetNumber:         v.FleetNumber.String,
		Description:         v.Description.String,
		Manufacturer:        v.Manufacturer.String,
		ManufCountry:        v.ManufCountry.String,
		Model:               v.Model.String,
		ConstrYearMonth:     v.ConstrYearMonth.String,
		AcquisitionValue:    getFloat64Pointer(v.AcquisitionValue),
		AcquisitionCurrency: v.AcquisitionCurrency,
		AcquisitionDate:     getTimePointer(v.AcquisitionDate),
		PurchaseVendor:      v.PurchaseVendor.String,
		ValidFrom:           getTimePointer(v.ValidFrom),
		ValidTo:             getTimePointer(v.ValidTo),
		FacilityID:          v.FacilityID.String,
		MaintPlant:          v.MaintPlant.String,
		PlanningPlant:       v.PlanningPlant.String,
		CompanyCode:         v.CompanyCode.String,
		BusinessArea:        v.BusinessArea.String,
		CostCenter:          v.CostCenter.String,
		AssetNo:             v.AssetNo.String,
		FleetObjectNo:       v.FleetObjectNo.String,
		ChassisNo:           v.ChassisNo.String,
		VehicleCategory:     v.VehicleCategory.String,
		EngineNumber:        v.EngineNumber.String,
		EnginePower:         v.EnginePower.String,
		EngineCapacity:      v.EngineCapacity.String,
		CylinderCount:       getInt64Pointer(v.CylinderCount),
		MaxSpeed:            getFloat64Pointer(v.MaxSpeed),
		Weight:              getFloat64Pointer(v.Weight),
		WeightUnit:          v.WeightUnit,
		LoadVolume:          getFloat64Pointer(v.LoadVolume),
		VolumeUnit:          v.VolumeUnit.String,
		SecondaryFuel:       v.SecondaryFuel.String,
		UsageIndicator:      v.UsageIndicator.String,
	}
	if p.FleetClass == "" {
		p.FleetClass = aggregate.FleetClassCV
	}
	if p.Ownership == "" {
		p.Ownership = aggregate.OwnershipOwn
	}
	if p.AcquisitionCurrency == "" {
		p.AcquisitionCurrency = "INR"
	}
	if p.WeightUnit == "" {
		p.WeightUnit = "TO"
	}
	return p
}

// ToReadModel converts db.Vehicle to domain.VehicleReadModel.
func ToReadModel(v db.Vehicle) domain.VehicleReadModel {
	return domain.VehicleReadModel{
		ID:                 v.ID,
		RegistrationNumber: v.RegistrationNumber,
		VehicleNumber:      v.VehicleNumber,
		VehicleType:        v.VehicleType,
		Capacity:           v.Capacity,
		FuelType:           v.FuelType,
		InsuranceExpiry:    v.InsuranceExpiry,
		FitnessExpiry:      v.FitnessExpiry,
		PermitExpiry:       v.PermitExpiry,
		Status:             v.Status,
		CurrentMileage:     getFloat64Pointer(v.CurrentMileage),
		Profile:            ProfileFromDB(v),
		CreatedAt:          v.CreatedAt,
		UpdatedAt:          v.UpdatedAt,
	}
}

func getFloat64Pointer(nf sql.NullFloat64) *float64 {
	if nf.Valid {
		return &nf.Float64
	}
	return nil
}

func getInt64Pointer(ni sql.NullInt64) *int64 {
	if ni.Valid {
		return &ni.Int64
	}
	return nil
}

func getTimePointer(nt sql.NullTime) *time.Time {
	if nt.Valid {
		return &nt.Time
	}
	return nil
}

// NullString maps "" to NULL so unset SOP text fields stay NULL (and the
// Vehicle Master report can distinguish "not registered" from empty).
func NullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// NullFloat64 maps nil to NULL.
func NullFloat64(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

// NullInt64 maps nil to NULL.
func NullInt64(i *int64) sql.NullInt64 {
	if i == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *i, Valid: true}
}

// NullTime maps nil / zero to NULL.
func NullTime(t *time.Time) sql.NullTime {
	if t == nil || t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
