package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"transport-app/internal/domain"

	db "transport-app/db/generated/sqlite"
)

// VehicleRepository implementation

func (r *SQLRepository) CreateVehicle(ctx context.Context, vehicle domain.Vehicle) (domain.Vehicle, error) {
	created, err := r.Q(ctx).CreateVehicle(ctx, db.CreateVehicleParams{
		ID:                 string(vehicle.ID),
		RegistrationNumber: vehicle.RegistrationNumber,
		VehicleNumber:      vehicle.VehicleNumber,
		VehicleType:        string(vehicle.VehicleType),
		Capacity:           vehicle.Capacity,
		FuelType:           string(vehicle.FuelType),
		InsuranceExpiry:    vehicle.InsuranceExpiry,
		FitnessExpiry:      vehicle.FitnessExpiry,
		PermitExpiry:       vehicle.PermitExpiry,
		Status:             string(vehicle.Status),
		CurrentMileage:     nullFloat(vehicle.CurrentMileage),
		TenantID:           tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Vehicle{}, err
	}
	v := db.Vehicle{
		ID:                 created.ID,
		RegistrationNumber: created.RegistrationNumber,
		VehicleNumber:      created.VehicleNumber,
		VehicleType:        created.VehicleType,
		Capacity:           created.Capacity,
		FuelType:           created.FuelType,
		InsuranceExpiry:    created.InsuranceExpiry,
		FitnessExpiry:      created.FitnessExpiry,
		PermitExpiry:       created.PermitExpiry,
		Status:             created.Status,
		CurrentMileage:     created.CurrentMileage,
		CreatedAt:          created.CreatedAt,
		UpdatedAt:          created.UpdatedAt,
	}
	if vehicle.PUCExpiry != nil {
		_, _ = r.exec(ctx, `UPDATE vehicles SET puc_expiry = ? WHERE id = ?`, vehicle.PUCExpiry.Format("2006-01-02"), string(vehicle.ID))
	}
	dom := toDomainVehicle(v)
	dom.PUCExpiry = vehicle.PUCExpiry
	return dom, nil
}

func (r *SQLRepository) GetVehicleByID(ctx context.Context, id domain.VehicleID) (domain.Vehicle, error) {
	row, err := r.Q(ctx).GetVehicleByID(ctx, db.GetVehicleByIDParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Vehicle{}, err
	}
	v := db.Vehicle{
		ID:                 row.ID,
		RegistrationNumber: row.RegistrationNumber,
		VehicleNumber:      row.VehicleNumber,
		VehicleType:        row.VehicleType,
		Capacity:           row.Capacity,
		FuelType:           row.FuelType,
		InsuranceExpiry:    row.InsuranceExpiry,
		FitnessExpiry:      row.FitnessExpiry,
		PermitExpiry:       row.PermitExpiry,
		Status:             row.Status,
		CurrentMileage:     row.CurrentMileage,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
	dom := toDomainVehicle(v)
	var puc sql.NullString
	_ = r.queryRow(ctx, `SELECT puc_expiry FROM vehicles WHERE id = ?`, string(id)).Scan(&puc)
	if puc.Valid && puc.String != "" {
		if t, err := time.Parse("2006-01-02", puc.String); err == nil {
			dom.PUCExpiry = &t
		} else if t, err := time.Parse(time.RFC3339, puc.String); err == nil {
			dom.PUCExpiry = &t
		}
	}
	return dom, nil
}

func (r *SQLRepository) GetVehicleByRegistration(ctx context.Context, regNum string) (domain.Vehicle, error) {
	row, err := r.Q(ctx).GetVehicleByRegistration(ctx, db.GetVehicleByRegistrationParams{
		RegistrationNumber: regNum,
		TenantID:           tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Vehicle{}, err
	}
	v := db.Vehicle{
		ID:                 row.ID,
		RegistrationNumber: row.RegistrationNumber,
		VehicleNumber:      row.VehicleNumber,
		VehicleType:        row.VehicleType,
		Capacity:           row.Capacity,
		FuelType:           row.FuelType,
		InsuranceExpiry:    row.InsuranceExpiry,
		FitnessExpiry:      row.FitnessExpiry,
		PermitExpiry:       row.PermitExpiry,
		Status:             row.Status,
		CurrentMileage:     row.CurrentMileage,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
	dom := toDomainVehicle(v)
	var puc sql.NullString
	_ = r.queryRow(ctx, `SELECT puc_expiry FROM vehicles WHERE id = ?`, row.ID).Scan(&puc)
	if puc.Valid && puc.String != "" {
		if t, err := time.Parse("2006-01-02", puc.String); err == nil {
			dom.PUCExpiry = &t
		} else if t, err := time.Parse(time.RFC3339, puc.String); err == nil {
			dom.PUCExpiry = &t
		}
	}
	return dom, nil
}

func (r *SQLRepository) UpdateVehicle(ctx context.Context, vehicle domain.Vehicle) (domain.Vehicle, error) {
	updated, err := r.Q(ctx).UpdateVehicle(ctx, db.UpdateVehicleParams{
		RegistrationNumber: vehicle.RegistrationNumber,
		VehicleNumber:      vehicle.VehicleNumber,
		VehicleType:        string(vehicle.VehicleType),
		Capacity:           vehicle.Capacity,
		FuelType:           string(vehicle.FuelType),
		InsuranceExpiry:    vehicle.InsuranceExpiry,
		FitnessExpiry:      vehicle.FitnessExpiry,
		PermitExpiry:       vehicle.PermitExpiry,
		Status:             string(vehicle.Status),
		CurrentMileage:     nullFloat(vehicle.CurrentMileage),
		ID:                 string(vehicle.ID),
		TenantID:           tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Vehicle{}, err
	}
	v := db.Vehicle{
		ID:                 updated.ID,
		RegistrationNumber: updated.RegistrationNumber,
		VehicleNumber:      updated.VehicleNumber,
		VehicleType:        updated.VehicleType,
		Capacity:           updated.Capacity,
		FuelType:           updated.FuelType,
		InsuranceExpiry:    updated.InsuranceExpiry,
		FitnessExpiry:      updated.FitnessExpiry,
		PermitExpiry:       updated.PermitExpiry,
		Status:             updated.Status,
		CurrentMileage:     updated.CurrentMileage,
		CreatedAt:          updated.CreatedAt,
		UpdatedAt:          updated.UpdatedAt,
	}
	if vehicle.PUCExpiry != nil {
		_, _ = r.exec(ctx, `UPDATE vehicles SET puc_expiry = ? WHERE id = ?`, vehicle.PUCExpiry.Format("2006-01-02"), string(vehicle.ID))
	}
	dom := toDomainVehicle(v)
	dom.PUCExpiry = vehicle.PUCExpiry
	return dom, nil
}

func (r *SQLRepository) DeleteVehicle(ctx context.Context, id domain.VehicleID) error {
	return r.Q(ctx).DeleteVehicle(ctx, db.DeleteVehicleParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
}

func (r *SQLRepository) SearchVehicles(ctx context.Context, query string, status string, limit, offset int) ([]domain.Vehicle, error) {
	rows, err := r.Q(ctx).SearchVehicles(ctx, db.SearchVehiclesParams{
		TenantID: tenantIDFromCtx(ctx),
		Column2:  sql.NullString{String: query, Valid: true},
		Column3:  sql.NullString{String: query, Valid: true},
		Column4:  sql.NullString{String: query, Valid: true},
		Column5:  status,
		Status:   status,
		Limit:    int64(limit),
		Offset:   int64(offset),
	})
	if err != nil {
		return nil, err
	}
	result := make([]domain.Vehicle, len(rows))
	for i, row := range rows {
		v := db.Vehicle{
			ID:                 row.ID,
			RegistrationNumber: row.RegistrationNumber,
			VehicleNumber:      row.VehicleNumber,
			VehicleType:        row.VehicleType,
			Capacity:           row.Capacity,
			FuelType:           row.FuelType,
			InsuranceExpiry:    row.InsuranceExpiry,
			FitnessExpiry:      row.FitnessExpiry,
			PermitExpiry:       row.PermitExpiry,
			Status:             row.Status,
			CurrentMileage:     row.CurrentMileage,
			CreatedAt:          row.CreatedAt,
			UpdatedAt:          row.UpdatedAt,
		}
		result[i] = toDomainVehicle(v)
	}
	return result, nil
}

func (r *SQLRepository) CountVehicles(ctx context.Context, query string, status string) (int64, error) {
	count, err := r.Q(ctx).CountVehicles(ctx, db.CountVehiclesParams{
		TenantID: tenantIDFromCtx(ctx),
		Column2:  sql.NullString{String: query, Valid: true},
		Column3:  sql.NullString{String: query, Valid: true},
		Column4:  sql.NullString{String: query, Valid: true},
		Column5:  status,
		Status:   status,
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *SQLRepository) GetAvailableVehicles(ctx context.Context) ([]domain.Vehicle, error) {
	rows, err := r.Q(ctx).GetAvailableVehicles(ctx, tenantIDFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	result := make([]domain.Vehicle, len(rows))
	for i, row := range rows {
		v := db.Vehicle{
			ID:                 row.ID,
			RegistrationNumber: row.RegistrationNumber,
			VehicleNumber:      row.VehicleNumber,
			VehicleType:        row.VehicleType,
			Capacity:           row.Capacity,
			FuelType:           row.FuelType,
			InsuranceExpiry:    row.InsuranceExpiry,
			FitnessExpiry:      row.FitnessExpiry,
			PermitExpiry:       row.PermitExpiry,
			Status:             row.Status,
			CurrentMileage:     row.CurrentMileage,
			CreatedAt:          row.CreatedAt,
			UpdatedAt:          row.UpdatedAt,
		}
		result[i] = toDomainVehicle(v)
	}
	return result, nil
}

func (r *SQLRepository) GetIdleVehicles(ctx context.Context) ([]domain.Vehicle, error) {
	rows, err := r.Q(ctx).GetIdleVehicles(ctx, tenantIDFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	result := make([]domain.Vehicle, len(rows))
	for i, row := range rows {
		v := db.Vehicle{
			ID:                 row.ID,
			RegistrationNumber: row.RegistrationNumber,
			VehicleNumber:      row.VehicleNumber,
			VehicleType:        row.VehicleType,
			Capacity:           row.Capacity,
			FuelType:           row.FuelType,
			InsuranceExpiry:    row.InsuranceExpiry,
			FitnessExpiry:      row.FitnessExpiry,
			PermitExpiry:       row.PermitExpiry,
			Status:             row.Status,
			CurrentMileage:     row.CurrentMileage,
			CreatedAt:          row.CreatedAt,
			UpdatedAt:          row.UpdatedAt,
		}
		result[i] = toDomainVehicle(v)
	}
	return result, nil
}

// IsMaintenanceBlocked checks if a vehicle is blocked for maintenance (Spec 04 §6, §12).
func (r *SQLRepository) IsMaintenanceBlocked(ctx context.Context, vehicleID string) (bool, string, error) {
	var due sql.NullString
	var overrideBy, overrideReason sql.NullString
	var overrideAt sql.NullTime

	err := r.db.QueryRowContext(ctx, `
		SELECT maintenance_due, maintenance_override_by, maintenance_override_at, maintenance_override_reason
		FROM vehicles
		WHERE id = ?`, vehicleID).Scan(&due, &overrideBy, &overrideAt, &overrideReason)
	if err != nil {
		return false, "", err
	}

	var dtcCode string
	_ = r.db.QueryRowContext(ctx, `
		SELECT dtc_code FROM dtc_events
		WHERE vehicle_id = ? AND severity = 'critical' AND resolved_at IS NULL
		ORDER BY occurred_at DESC LIMIT 1`, vehicleID).Scan(&dtcCode)

	isDue := due.Valid && due.String != ""
	hasCriticalDTC := dtcCode != ""
	if !isDue && !hasCriticalDTC {
		return false, "", nil
	}

	// Active override check
	if overrideBy.Valid && overrideBy.String != "" && overrideAt.Valid {
		return false, "", nil // Override lifts block
	}

	if isDue {
		return true, fmt.Sprintf("vehicle %s is blocked for maintenance (due since: %s); override requires maintenance:update permission", vehicleID, due.String), nil
	}
	return true, fmt.Sprintf("vehicle %s is blocked for maintenance (unresolved critical DTC %s); override requires maintenance:update permission", vehicleID, dtcCode), nil
}
