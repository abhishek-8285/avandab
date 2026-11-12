package application

// VehicleOptionQueries serves the tenant vehicle dropdown shared by the
// video-telematics and cold-chain configuration modals, extracted from
// internal/handlers/{scorecard,fuel_issues}.go (handlers-end-state ADR,
// Phase 2).

import (
	"context"
	"database/sql"
)

// VehicleOption is one selectable vehicle.
type VehicleOption struct {
	ID                 string
	VehicleNumber      string
	RegistrationNumber string
}

// VehicleOptions returns the tenant's vehicles ordered by vehicle_number.
// Best-effort like the legacy inline query: a query/scan failure leaves the
// dropdown empty instead of failing the page.
func VehicleOptions(ctx context.Context, db *sql.DB, tenantID string) []VehicleOption {
	if db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id, vehicle_number, registration_number
		FROM vehicles WHERE tenant_id = $1 ORDER BY vehicle_number ASC`, tenantID)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var out []VehicleOption
	for rows.Next() {
		var v VehicleOption
		if err := rows.Scan(&v.ID, &v.VehicleNumber, &v.RegistrationNumber); err == nil {
			out = append(out, v)
		}
	}
	return out
}
