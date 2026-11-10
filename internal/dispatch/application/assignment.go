package application

// Driver-vehicle preferred assignment, extracted from
// internal/handlers/assignment.go (handlers-end-state ADR, Phase 2). The
// driver_preferred_vehicles table is the master relationship behind the driver
// "assign vehicle" and vehicle "assign driver" actions: at most one active row
// (unassigned_at IS NULL) per driver and per vehicle, history is kept by
// stamping unassigned_at, and a reassignment closes the conflicting active rows
// before inserting the new one. The legacy SQL, tenant scoping, error mapping
// and best-effort conflict cleanup are preserved verbatim; the handler keeps
// HTTP parsing, validation and rendering.

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"transport-app/internal/shared"
)

// DriverVehicleAssignment is one active preferred driver-vehicle assignment.
type DriverVehicleAssignment struct {
	ID        string
	DriverID  string
	VehicleID string
	IsPrimary bool
}

// AssignmentService owns the driver-vehicle preferred assignment master
// relationship. It is stateless and cheap to construct per request.
type AssignmentService struct {
	db *sql.DB
}

// NewAssignmentService wires the assignment service against the database.
func NewAssignmentService(db *sql.DB) *AssignmentService {
	return &AssignmentService{db: db}
}

// ActiveByDriver returns the active (unassigned_at IS NULL) assignment for a
// driver, or nil when the driver has none. Legacy semantics (see
// getActiveAssignmentByDriver): sql.ErrNoRows maps to (nil, nil); any other
// scan error is returned. A nil store degrades to "no assignment".
func (s *AssignmentService) ActiveByDriver(ctx context.Context, tenantID shared.TenantID, driverID string) (*DriverVehicleAssignment, error) {
	if s.db == nil {
		return nil, nil
	}
	return scanActiveAssignment(s.db.QueryRowContext(ctx,
		`SELECT id, driver_id, vehicle_id, is_primary FROM driver_preferred_vehicles
		 WHERE driver_id = $1 AND tenant_id = $2 AND unassigned_at IS NULL LIMIT 1`,
		driverID, string(tenantID)))
}

// ActiveByVehicle returns the active (unassigned_at IS NULL) assignment for a
// vehicle, or nil when the vehicle has none. Legacy semantics (see
// getActiveAssignmentByVehicle): sql.ErrNoRows maps to (nil, nil); any other
// scan error is returned. A nil store degrades to "no assignment".
func (s *AssignmentService) ActiveByVehicle(ctx context.Context, tenantID shared.TenantID, vehicleID string) (*DriverVehicleAssignment, error) {
	if s.db == nil {
		return nil, nil
	}
	return scanActiveAssignment(s.db.QueryRowContext(ctx,
		`SELECT id, driver_id, vehicle_id, is_primary FROM driver_preferred_vehicles
		 WHERE vehicle_id = $1 AND tenant_id = $2 AND unassigned_at IS NULL LIMIT 1`,
		vehicleID, string(tenantID)))
}

// scanActiveAssignment maps one active-assignment row. The legacy helpers
// translated only sql.ErrNoRows to "no assignment" and returned every other
// error unchanged.
func scanActiveAssignment(row *sql.Row) (*DriverVehicleAssignment, error) {
	var a DriverVehicleAssignment
	var isPrimary int
	if err := row.Scan(&a.ID, &a.DriverID, &a.VehicleID, &isPrimary); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	a.IsPrimary = isPrimary == 1
	return &a, nil
}

// AssignVehicleToDriverCommand is one preferred driver -> vehicle assignment.
type AssignVehicleToDriverCommand struct {
	TenantID   shared.TenantID
	DriverID   string
	VehicleID  string
	AssignedBy string
}

// AssignVehicleToDriver closes any conflicting active assignment for this
// driver and for this vehicle, then inserts the new active assignment (legacy
// helper assignVehicleToDriver, SQL and semantics verbatim). Missing/empty
// driver or vehicle ids return sql.ErrNoRows, which the handler maps to its
// legacy 400 page; conflict-cleanup UPDATE errors stay intentionally ignored
// exactly like the legacy helper.
func (s *AssignmentService) AssignVehicleToDriver(ctx context.Context, cmd AssignVehicleToDriverCommand) error {
	if cmd.DriverID == "" || cmd.VehicleID == "" {
		return sql.ErrNoRows
	}
	if s.db == nil {
		return errors.New("assignment store unavailable")
	}
	now := "datetime('now')"
	// Verify driver and vehicle exist in this tenant (fail fast with clear message).
	var cnt int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM drivers WHERE id = $1 AND tenant_id = $2`, cmd.DriverID, string(cmd.TenantID)).Scan(&cnt); err != nil {
		return err
	}
	if cnt == 0 {
		return sql.ErrNoRows
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vehicles WHERE id = $1 AND tenant_id = $2`, cmd.VehicleID, string(cmd.TenantID)).Scan(&cnt); err != nil {
		return err
	}
	if cnt == 0 {
		return sql.ErrNoRows
	}
	// Close any active assignment for this driver and for this vehicle (1:1 active).
	_, _ = s.db.ExecContext(ctx, `UPDATE driver_preferred_vehicles SET unassigned_at = `+now+`, updated_at = `+now+` WHERE tenant_id = $1 AND driver_id = $2 AND unassigned_at IS NULL`, string(cmd.TenantID), cmd.DriverID)
	_, _ = s.db.ExecContext(ctx, `UPDATE driver_preferred_vehicles SET unassigned_at = `+now+`, updated_at = `+now+` WHERE tenant_id = $1 AND vehicle_id = $2 AND unassigned_at IS NULL`, string(cmd.TenantID), cmd.VehicleID)
	id := "dva-" + uuid.NewString()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO driver_preferred_vehicles (id, tenant_id, driver_id, vehicle_id, is_primary, assigned_by) VALUES ($1,$2,$3,$4,1,$5)`,
		id, string(cmd.TenantID), cmd.DriverID, cmd.VehicleID, cmd.AssignedBy)
	return err
}

// UnassignDriver closes the driver's active assignment, keeping the history row
// (legacy helper unassignDriver, SQL verbatim). A nil store returns an error
// the handler ignores, same as any legacy UPDATE error on this path.
func (s *AssignmentService) UnassignDriver(ctx context.Context, tenantID shared.TenantID, driverID string) error {
	if s.db == nil {
		return errors.New("assignment store unavailable")
	}
	now := "datetime('now')"
	_, err := s.db.ExecContext(ctx, `UPDATE driver_preferred_vehicles SET unassigned_at = `+now+`, updated_at = `+now+` WHERE tenant_id = $1 AND driver_id = $2 AND unassigned_at IS NULL`, string(tenantID), driverID)
	return err
}

// UnassignVehicle closes the vehicle's active assignment, keeping the history
// row (legacy helper unassignVehicle, SQL verbatim). A nil store returns an
// error the handler ignores, same as any legacy UPDATE error on this path.
func (s *AssignmentService) UnassignVehicle(ctx context.Context, tenantID shared.TenantID, vehicleID string) error {
	if s.db == nil {
		return errors.New("assignment store unavailable")
	}
	now := "datetime('now')"
	_, err := s.db.ExecContext(ctx, `UPDATE driver_preferred_vehicles SET unassigned_at = `+now+`, updated_at = `+now+` WHERE tenant_id = $1 AND vehicle_id = $2 AND unassigned_at IS NULL`, string(tenantID), vehicleID)
	return err
}
