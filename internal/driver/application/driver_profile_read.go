package application

// Read-model queries behind the driver view page, extracted from
// internal/handlers/drivers.go (handlers-end-state ADR, Phase 2). The legacy
// SQL, tenant scoping and best-effort error semantics are preserved: the
// handler renders whatever was collected and never fails the page on a query
// error.

import (
	"context"
	"database/sql"

	"transport-app/internal/shared"
)

// DriverProfileQueries is the read model behind the driver view page. It is
// stateless and cheap to construct per request.
type DriverProfileQueries struct {
	db *sql.DB
}

// NewDriverProfileQueries wires the read model against the database. A nil db
// degrades to an empty, renderable view.
func NewDriverProfileQueries(db *sql.DB) *DriverProfileQueries {
	return &DriverProfileQueries{db: db}
}

// DriverProfileQuery selects the driver whose view-page data is needed.
type DriverProfileQuery struct {
	DriverID string
	TenantID shared.TenantID
}

// DriverBehaviourEvent is one recent driver_behaviour_events row. The legacy
// page read the metadata column into Description and always rendered the
// event as open (Resolved false); both quirks are preserved.
type DriverBehaviourEvent struct {
	EventType   string
	Severity    string
	CreatedAt   sql.NullTime
	Description sql.NullString
	Resolved    bool
}

// DriverRecentTrip is one recent trips row for the driver view.
type DriverRecentTrip struct {
	ID         string
	TripNumber string
	Status     string
	CreatedAt  sql.NullTime
}

// DriverAssignedVehicle is the driver's active preferred vehicle plus its
// display fields (the legacy handler map keys are preserved as struct fields).
type DriverAssignedVehicle struct {
	ID                 string
	RegistrationNumber string
	VehicleNumber      string
	VehicleType        string
	IsPrimary          bool
}

// DriverVehicleOption is one selectable vehicle in the assignment dropdown.
type DriverVehicleOption struct {
	ID                 string
	RegistrationNumber string
	VehicleNumber      string
}

// DriverProfileView carries every section the driver view template renders.
type DriverProfileView struct {
	Score           sql.NullFloat64
	Tier            sql.NullString
	BehaviourEvents []DriverBehaviourEvent
	RecentTrips     []DriverRecentTrip
	AssignedVehicle *DriverAssignedVehicle
	AssignVehicles  []DriverVehicleOption
}

// Load returns the driver view-page data. Every legacy query was best-effort:
// a query error left its section empty instead of failing the page, and
// per-row scan failures were skipped. Those semantics are preserved.
func (q *DriverProfileQueries) Load(ctx context.Context, query DriverProfileQuery) DriverProfileView {
	view := DriverProfileView{}
	if q.db == nil {
		return view
	}

	// Scorecard tier + score. The legacy query is keyed by driver id only:
	// the tenant check already happened in the tenant-scoped GetDriver read.
	_ = q.db.QueryRowContext(ctx,
		`SELECT score, tier FROM drivers WHERE id = $1`, query.DriverID).Scan(&view.Score, &view.Tier)

	// Fuel-fraud and, later, driving behaviour events (newest first, limit 5).
	if rows, err := q.db.QueryContext(ctx, `
		SELECT event_type, severity, created_at, metadata
		FROM driver_behaviour_events WHERE driver_id = $1
		ORDER BY created_at DESC LIMIT 5`, query.DriverID); err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			event := DriverBehaviourEvent{Resolved: false}
			if rows.Scan(&event.EventType, &event.Severity, &event.CreatedAt, &event.Description) == nil {
				view.BehaviourEvents = append(view.BehaviourEvents, event)
			}
		}
	}

	// Recent trips + latest settlement summary.
	if rows, err := q.db.QueryContext(ctx, `
		SELECT id, trip_number, status, created_at FROM trips
		WHERE driver_id = $1 ORDER BY created_at DESC LIMIT 5`, query.DriverID); err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var trip DriverRecentTrip
			if rows.Scan(&trip.ID, &trip.TripNumber, &trip.Status, &trip.CreatedAt) == nil {
				view.RecentTrips = append(view.RecentTrips, trip)
			}
		}
	}

	tenantID := string(query.TenantID)
	// Preferred vehicle (master assignment) + vehicle details for display.
	view.AssignedVehicle = q.assignedVehicle(ctx, query.DriverID, tenantID)
	// Vehicles for the "Change" dropdown (tenant-scoped, ordered by plate).
	view.AssignVehicles = q.assignableVehicles(ctx, tenantID)
	return view
}

// assignedVehicle mirrors the legacy getActiveAssignmentByDriver helper plus
// the vehicle-details lookup. The helper returned nil on sql.ErrNoRows and
// the caller ignored every other error (empty section).
func (q *DriverProfileQueries) assignedVehicle(ctx context.Context, driverID, tenantID string) *DriverAssignedVehicle {
	var assignmentID, assignmentDriverID, vehicleID string
	var isPrimary int
	err := q.db.QueryRowContext(ctx,
		`SELECT id, driver_id, vehicle_id, is_primary FROM driver_preferred_vehicles
		 WHERE driver_id = $1 AND tenant_id = $2 AND unassigned_at IS NULL LIMIT 1`,
		driverID, tenantID).
		Scan(&assignmentID, &assignmentDriverID, &vehicleID, &isPrimary)
	if err != nil {
		return nil
	}
	vehicle := &DriverAssignedVehicle{ID: vehicleID, IsPrimary: isPrimary == 1}
	_ = q.db.QueryRowContext(ctx,
		`SELECT registration_number, vehicle_number, vehicle_type FROM vehicles WHERE id = $1 AND tenant_id = $2`,
		vehicleID, tenantID).
		Scan(&vehicle.RegistrationNumber, &vehicle.VehicleNumber, &vehicle.VehicleType)
	return vehicle
}

// assignableVehicles returns the tenant vehicles offered in the dropdown,
// ordered by registration number (legacy limit 100, scan failures skipped).
func (q *DriverProfileQueries) assignableVehicles(ctx context.Context, tenantID string) []DriverVehicleOption {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, registration_number, vehicle_number FROM vehicles WHERE tenant_id = $1 ORDER BY registration_number LIMIT 100`,
		tenantID)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var vehicles []DriverVehicleOption
	for rows.Next() {
		var vehicle DriverVehicleOption
		if rows.Scan(&vehicle.ID, &vehicle.RegistrationNumber, &vehicle.VehicleNumber) == nil {
			vehicles = append(vehicles, vehicle)
		}
	}
	return vehicles
}
