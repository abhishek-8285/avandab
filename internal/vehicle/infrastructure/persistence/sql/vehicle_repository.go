package sql

import (
	"context"
	"database/sql"
	"errors"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/shared/outbox"
	"transport-app/internal/vehicle/domain"
	"transport-app/internal/vehicle/domain/aggregate"
	"transport-app/internal/vehicle/infrastructure/persistence/sql/converters"
)

type vehicleRepository struct {
	dbConn *sql.DB
	q      *db.Queries
	outbox *outbox.OutboxWriter
}

// NewVehicleRepository creates a SQLite-backed implementation of VehicleRepository.
func NewVehicleRepository(dbConn *sql.DB) domain.VehicleRepository {
	return &vehicleRepository{
		dbConn: dbConn,
		q:      db.New(dbConn),
		outbox: outbox.NewOutboxWriter(dbConn),
	}
}

func (r *vehicleRepository) Q(ctx context.Context) *db.Queries {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return r.q.WithTx(tx)
	}
	return r.q
}

func (r *vehicleRepository) Save(ctx context.Context, v *aggregate.VehicleAggregate) error {
	p := v.Profile.Normalized()

	_, err := r.Q(ctx).GetVehicleByID(ctx, db.GetVehicleByIDParams{
		ID:       string(v.ID),
		TenantID: string(v.TenantID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_, err = r.Q(ctx).CreateVehicle(ctx, db.CreateVehicleParams{
				ID:                  string(v.ID),
				RegistrationNumber:  v.RegistrationNumber,
				VehicleNumber:       v.VehicleNumber,
				VehicleType:         string(v.VehicleType),
				Capacity:            v.Capacity,
				FuelType:            string(v.FuelType),
				InsuranceExpiry:     converters.NullTime(&v.InsuranceExpiry),
				FitnessExpiry:       converters.NullTime(&v.FitnessExpiry),
				PermitExpiry:        converters.NullTime(&v.PermitExpiry),
				Status:              string(v.Status),
				CurrentMileage:      converters.NullFloat64(v.CurrentMileage),
				Blocked:             converters.BoolToInt64(v.Blocked),
				BlockedReason:       converters.NullString(v.BlockedReason),
				RcExpiry:            converters.NullTime(v.RCExpiry),
				Odometer:            v.Odometer,
				PucExpiry:           converters.NullTime(v.PUCExpiry),
				TenantID:            string(v.TenantID),
				FleetClass:          string(p.FleetClass),
				Ownership:           string(p.Ownership),
				FleetNumber:         converters.NullString(p.FleetNumber),
				Description:         converters.NullString(p.Description),
				Manufacturer:        converters.NullString(p.Manufacturer),
				ManufCountry:        converters.NullString(p.ManufCountry),
				Model:               converters.NullString(p.Model),
				ConstrYearMonth:     converters.NullString(p.ConstrYearMonth),
				AcquisitionValue:    converters.NullFloat64(p.AcquisitionValue),
				AcquisitionCurrency: p.AcquisitionCurrency,
				AcquisitionDate:     converters.NullTime(p.AcquisitionDate),
				PurchaseVendor:      converters.NullString(p.PurchaseVendor),
				ValidFrom:           converters.NullTime(p.ValidFrom),
				ValidTo:             converters.NullTime(p.ValidTo),
				FacilityID:          converters.NullString(p.FacilityID),
				MaintPlant:          converters.NullString(p.MaintPlant),
				PlanningPlant:       converters.NullString(p.PlanningPlant),
				CompanyCode:         converters.NullString(p.CompanyCode),
				BusinessArea:        converters.NullString(p.BusinessArea),
				CostCenter:          converters.NullString(p.CostCenter),
				AssetNo:             converters.NullString(p.AssetNo),
				FleetObjectNo:       converters.NullString(p.FleetObjectNo),
				ChassisNo:           converters.NullString(p.ChassisNo),
				VehicleCategory:     converters.NullString(p.VehicleCategory),
				EngineNumber:        converters.NullString(p.EngineNumber),
				EnginePower:         converters.NullString(p.EnginePower),
				EngineCapacity:      converters.NullString(p.EngineCapacity),
				CylinderCount:       converters.NullInt64(p.CylinderCount),
				MaxSpeed:            converters.NullFloat64(p.MaxSpeed),
				Weight:              converters.NullFloat64(p.Weight),
				WeightUnit:          p.WeightUnit,
				LoadVolume:          converters.NullFloat64(p.LoadVolume),
				VolumeUnit:          converters.NullString(p.VolumeUnit),
				SecondaryFuel:       converters.NullString(p.SecondaryFuel),
				UsageIndicator:      converters.NullString(p.UsageIndicator),
				StandardKmpl:        converters.NullFloat64(v.StandardKmpl),
			})
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		_, err = r.Q(ctx).UpdateVehicle(ctx, db.UpdateVehicleParams{
			RegistrationNumber:  v.RegistrationNumber,
			VehicleNumber:       v.VehicleNumber,
			VehicleType:         string(v.VehicleType),
			Capacity:            v.Capacity,
			FuelType:            string(v.FuelType),
			InsuranceExpiry:     converters.NullTime(&v.InsuranceExpiry),
			FitnessExpiry:       converters.NullTime(&v.FitnessExpiry),
			PermitExpiry:        converters.NullTime(&v.PermitExpiry),
			Status:              string(v.Status),
			CurrentMileage:      converters.NullFloat64(v.CurrentMileage),
			Blocked:             converters.BoolToInt64(v.Blocked),
			BlockedReason:       converters.NullString(v.BlockedReason),
			RcExpiry:            converters.NullTime(v.RCExpiry),
			Odometer:            v.Odometer,
			PucExpiry:           converters.NullTime(v.PUCExpiry),
			FleetClass:          string(p.FleetClass),
			Ownership:           string(p.Ownership),
			FleetNumber:         converters.NullString(p.FleetNumber),
			Description:         converters.NullString(p.Description),
			Manufacturer:        converters.NullString(p.Manufacturer),
			ManufCountry:        converters.NullString(p.ManufCountry),
			Model:               converters.NullString(p.Model),
			ConstrYearMonth:     converters.NullString(p.ConstrYearMonth),
			AcquisitionValue:    converters.NullFloat64(p.AcquisitionValue),
			AcquisitionCurrency: p.AcquisitionCurrency,
			AcquisitionDate:     converters.NullTime(p.AcquisitionDate),
			PurchaseVendor:      converters.NullString(p.PurchaseVendor),
			ValidFrom:           converters.NullTime(p.ValidFrom),
			ValidTo:             converters.NullTime(p.ValidTo),
			FacilityID:          converters.NullString(p.FacilityID),
			MaintPlant:          converters.NullString(p.MaintPlant),
			PlanningPlant:       converters.NullString(p.PlanningPlant),
			CompanyCode:         converters.NullString(p.CompanyCode),
			BusinessArea:        converters.NullString(p.BusinessArea),
			CostCenter:          converters.NullString(p.CostCenter),
			AssetNo:             converters.NullString(p.AssetNo),
			FleetObjectNo:       converters.NullString(p.FleetObjectNo),
			ChassisNo:           converters.NullString(p.ChassisNo),
			VehicleCategory:     converters.NullString(p.VehicleCategory),
			EngineNumber:        converters.NullString(p.EngineNumber),
			EnginePower:         converters.NullString(p.EnginePower),
			EngineCapacity:      converters.NullString(p.EngineCapacity),
			CylinderCount:       converters.NullInt64(p.CylinderCount),
			MaxSpeed:            converters.NullFloat64(p.MaxSpeed),
			Weight:              converters.NullFloat64(p.Weight),
			WeightUnit:          p.WeightUnit,
			LoadVolume:          converters.NullFloat64(p.LoadVolume),
			VolumeUnit:          converters.NullString(p.VolumeUnit),
			SecondaryFuel:       converters.NullString(p.SecondaryFuel),
			UsageIndicator:      converters.NullString(p.UsageIndicator),
			StandardKmpl:        converters.NullFloat64(v.StandardKmpl),
			ID:                  string(v.ID),
			TenantID:            string(v.TenantID),
		})
		if err != nil {
			return err
		}
	}

	err = r.outbox.SaveEvents(ctx, string(v.ID), "Vehicle", v.Events())
	if err != nil {
		return err
	}
	v.ClearEvents()
	return nil
}

func (r *vehicleRepository) Find(ctx context.Context, id aggregate.VehicleID, tenantID shared.TenantID) (*aggregate.VehicleAggregate, error) {
	row, err := r.Q(ctx).GetVehicleByID(ctx, db.GetVehicleByIDParams{
		ID:       string(id),
		TenantID: string(tenantID),
	})
	if err != nil {
		return nil, err
	}
	return converters.ToDomain(dbVehicleFromGetRow(row)), nil
}

func (r *vehicleRepository) GetReadModel(ctx context.Context, id aggregate.VehicleID, tenantID shared.TenantID) (domain.VehicleReadModel, error) {
	row, err := r.Q(ctx).GetVehicleByID(ctx, db.GetVehicleByIDParams{
		ID:       string(id),
		TenantID: string(tenantID),
	})
	if err != nil {
		return domain.VehicleReadModel{}, err
	}
	return converters.ToReadModel(dbVehicleFromGetRow(row)), nil
}

func (r *vehicleRepository) SearchReadModels(ctx context.Context, tenantID shared.TenantID, query string, status string, limit int, offset int) ([]domain.VehicleReadModel, int64, error) {
	return r.searchFiltered(ctx, tenantID, query, status, "", "", limit, offset)
}

// searchFiltered is the shared implementation behind SearchReadModels and
// SearchReadModelsFiltered (SOP fleet_class / ownership filters, spec §6).
func (r *vehicleRepository) searchFiltered(ctx context.Context, tenantID shared.TenantID, query string, status string, fleetClass string, ownership string, limit int, offset int) ([]domain.VehicleReadModel, int64, error) {
	rows, err := r.Q(ctx).SearchVehicles(ctx, db.SearchVehiclesParams{
		TenantID:      string(tenantID),
		Search:        query,
		StatusAll:     status,
		Status:        status,
		FleetClassAll: fleetClass,
		FleetClass:    fleetClass,
		OwnershipAll:  ownership,
		Ownership:     ownership,
		Limit:         int64(limit),
		Offset:        int64(offset),
	})
	if err != nil {
		return nil, 0, err
	}

	total, err := r.Q(ctx).CountVehicles(ctx, db.CountVehiclesParams{
		TenantID:      string(tenantID),
		Search:        query,
		StatusAll:     status,
		Status:        status,
		FleetClassAll: fleetClass,
		FleetClass:    fleetClass,
		OwnershipAll:  ownership,
		Ownership:     ownership,
	})
	if err != nil {
		return nil, 0, err
	}

	readModels := make([]domain.VehicleReadModel, len(rows))
	for i, row := range rows {
		readModels[i] = converters.ToReadModel(dbVehicleFromSearchRow(row))
	}

	return readModels, total, nil
}

// dbVehicleFromGetRow maps a GetVehicleByID row onto db.Vehicle for the
// shared converters.
func dbVehicleFromGetRow(row db.GetVehicleByIDRow) db.Vehicle {
	return db.Vehicle{
		ID:                  row.ID,
		RegistrationNumber:  row.RegistrationNumber,
		VehicleNumber:       row.VehicleNumber,
		VehicleType:         row.VehicleType,
		Capacity:            row.Capacity,
		FuelType:            row.FuelType,
		InsuranceExpiry:     row.InsuranceExpiry,
		FitnessExpiry:       row.FitnessExpiry,
		PermitExpiry:        row.PermitExpiry,
		Status:              row.Status,
		CurrentMileage:      row.CurrentMileage,
		Blocked:             row.Blocked,
		BlockedReason:       row.BlockedReason,
		RcExpiry:            row.RcExpiry,
		Odometer:            row.Odometer,
		PucExpiry:           row.PucExpiry,
		TenantID:            row.TenantID,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		FleetClass:          row.FleetClass,
		Ownership:           row.Ownership,
		FleetNumber:         row.FleetNumber,
		Description:         row.Description,
		Manufacturer:        row.Manufacturer,
		ManufCountry:        row.ManufCountry,
		Model:               row.Model,
		ConstrYearMonth:     row.ConstrYearMonth,
		AcquisitionValue:    row.AcquisitionValue,
		AcquisitionCurrency: row.AcquisitionCurrency,
		AcquisitionDate:     row.AcquisitionDate,
		PurchaseVendor:      row.PurchaseVendor,
		ValidFrom:           row.ValidFrom,
		ValidTo:             row.ValidTo,
		FacilityID:          row.FacilityID,
		MaintPlant:          row.MaintPlant,
		PlanningPlant:       row.PlanningPlant,
		CompanyCode:         row.CompanyCode,
		BusinessArea:        row.BusinessArea,
		CostCenter:          row.CostCenter,
		AssetNo:             row.AssetNo,
		FleetObjectNo:       row.FleetObjectNo,
		ChassisNo:           row.ChassisNo,
		VehicleCategory:     row.VehicleCategory,
		EngineNumber:        row.EngineNumber,
		EnginePower:         row.EnginePower,
		EngineCapacity:      row.EngineCapacity,
		CylinderCount:       row.CylinderCount,
		MaxSpeed:            row.MaxSpeed,
		Weight:              row.Weight,
		WeightUnit:          row.WeightUnit,
		LoadVolume:          row.LoadVolume,
		VolumeUnit:          row.VolumeUnit,
		SecondaryFuel:       row.SecondaryFuel,
		UsageIndicator:      row.UsageIndicator,
		StandardKmpl:        row.StandardKmpl,
	}
}

// dbVehicleFromSearchRow maps a SearchVehicles row onto db.Vehicle.
func dbVehicleFromSearchRow(row db.SearchVehiclesRow) db.Vehicle {
	return db.Vehicle{
		ID:                  row.ID,
		RegistrationNumber:  row.RegistrationNumber,
		VehicleNumber:       row.VehicleNumber,
		VehicleType:         row.VehicleType,
		Capacity:            row.Capacity,
		FuelType:            row.FuelType,
		InsuranceExpiry:     row.InsuranceExpiry,
		FitnessExpiry:       row.FitnessExpiry,
		PermitExpiry:        row.PermitExpiry,
		Status:              row.Status,
		CurrentMileage:      row.CurrentMileage,
		Blocked:             row.Blocked,
		BlockedReason:       row.BlockedReason,
		RcExpiry:            row.RcExpiry,
		Odometer:            row.Odometer,
		PucExpiry:           row.PucExpiry,
		TenantID:            row.TenantID,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		FleetClass:          row.FleetClass,
		Ownership:           row.Ownership,
		FleetNumber:         row.FleetNumber,
		Description:         row.Description,
		Manufacturer:        row.Manufacturer,
		ManufCountry:        row.ManufCountry,
		Model:               row.Model,
		ConstrYearMonth:     row.ConstrYearMonth,
		AcquisitionValue:    row.AcquisitionValue,
		AcquisitionCurrency: row.AcquisitionCurrency,
		AcquisitionDate:     row.AcquisitionDate,
		PurchaseVendor:      row.PurchaseVendor,
		ValidFrom:           row.ValidFrom,
		ValidTo:             row.ValidTo,
		FacilityID:          row.FacilityID,
		MaintPlant:          row.MaintPlant,
		PlanningPlant:       row.PlanningPlant,
		CompanyCode:         row.CompanyCode,
		BusinessArea:        row.BusinessArea,
		CostCenter:          row.CostCenter,
		AssetNo:             row.AssetNo,
		FleetObjectNo:       row.FleetObjectNo,
		ChassisNo:           row.ChassisNo,
		VehicleCategory:     row.VehicleCategory,
		EngineNumber:        row.EngineNumber,
		EnginePower:         row.EnginePower,
		EngineCapacity:      row.EngineCapacity,
		CylinderCount:       row.CylinderCount,
		MaxSpeed:            row.MaxSpeed,
		Weight:              row.Weight,
		WeightUnit:          row.WeightUnit,
		LoadVolume:          row.LoadVolume,
		VolumeUnit:          row.VolumeUnit,
		SecondaryFuel:       row.SecondaryFuel,
		UsageIndicator:      row.UsageIndicator,
		StandardKmpl:        row.StandardKmpl,
	}
}
