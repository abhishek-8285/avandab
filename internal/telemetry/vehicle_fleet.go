package telemetry

// EV-dashboard fleet queries, extracted from
// internal/handlers/telemetry_devices.go (handlers-end-state ADR, Phase 2).
// The EV profile/session/energy logic stays in the untouched EV service; this
// file only moves the two tenant-scoped vehicles-table statements the EV web
// handler runs around it.

import (
	"context"
	"database/sql"
)

// FleetVehicleOption is one fleet vehicle row offered in the EV configuration
// modal dropdown.
type FleetVehicleOption struct {
	ID                 string
	VehicleNumber      string
	RegistrationNumber string
	FuelType           string
}

// FleetVehicles returns the tenant's vehicles ordered by vehicle_number for
// the EV configuration dropdown. Best-effort like the legacy inline query: a
// query/scan failure leaves the list empty instead of failing the page.
func FleetVehicles(ctx context.Context, db *sql.DB, tenantID string) []FleetVehicleOption {
	if db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id, vehicle_number, registration_number, fuel_type
		FROM vehicles WHERE tenant_id = $1 ORDER BY vehicle_number ASC`, tenantID)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var vehicles []FleetVehicleOption
	for rows.Next() {
		var v FleetVehicleOption
		if err := rows.Scan(&v.ID, &v.VehicleNumber, &v.RegistrationNumber, &v.FuelType); err == nil {
			vehicles = append(vehicles, v)
		}
	}
	return vehicles
}

// MarkVehicleElectric flips the tenant vehicle's fuel_type to electric after a
// successful EV profile write. Contractually best-effort: the legacy handler
// ignored the error because the profile write had already succeeded.
func MarkVehicleElectric(ctx context.Context, db *sql.DB, tenantID, vehicleID string) error {
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx, `UPDATE vehicles SET fuel_type = 'electric' WHERE tenant_id = $1 AND id = $2`, tenantID, vehicleID)
	return err
}
