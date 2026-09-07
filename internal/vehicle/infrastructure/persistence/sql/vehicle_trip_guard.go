package sql

import (
	"context"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/shared"
	"transport-app/internal/vehicle/domain/aggregate"
)

// Trip-conflict guard for deletes (same rule as the legacy store's
// CheckVehicleConflict): a vehicle with live trips cannot be removed.

// TripConflictChecker counts trips that block a vehicle delete.
type TripConflictChecker interface {
	CountActiveTripsForVehicle(ctx context.Context, tenantID shared.TenantID, vehicleID string) (int64, error)
}

// VehicleDeleter removes a vehicle row and flushes its outbox events.
type VehicleDeleter interface {
	DeleteVehicleRecord(ctx context.Context, id string, tenantID string) error
	FlushVehicleEvents(ctx context.Context, v *aggregate.VehicleAggregate) error
}

// DeleteVehicleRecord deletes the vehicle row (tenant-scoped).
func (r *vehicleRepository) DeleteVehicleRecord(ctx context.Context, id string, tenantID string) error {
	return r.Q(ctx).DeleteVehicle(ctx, db.DeleteVehicleParams{
		ID:       id,
		TenantID: tenantID,
	})
}

// FlushVehicleEvents persists pending aggregate events to the outbox.
func (r *vehicleRepository) FlushVehicleEvents(ctx context.Context, v *aggregate.VehicleAggregate) error {
	if err := r.outbox.SaveEvents(ctx, string(v.ID), "Vehicle", v.Events()); err != nil {
		return err
	}
	v.ClearEvents()
	return nil
}

// CountActiveTripsForVehicle returns the number of trips referencing the
// vehicle that are not in a terminal state.
func (r *vehicleRepository) CountActiveTripsForVehicle(ctx context.Context, tenantID shared.TenantID, vehicleID string) (int64, error) {
	var count int64
	err := r.dbConn.QueryRowContext(ctx, `
SELECT COUNT(*) FROM trips
WHERE vehicle_id = ? AND tenant_id = ?
  AND status NOT IN ('completed', 'cancelled')`,
		vehicleID, string(tenantID)).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
