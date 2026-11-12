package application

// Vehicle view-page read model, extracted from internal/handlers/vehicles.go
// (handlers-end-state ADR, Phase 2). The legacy SQL, tenant scoping, ordering,
// row limits and best-effort error semantics are preserved verbatim: the view
// page renders whatever was collected and a query error leaves its section
// empty instead of failing the page. The optional measurement fields stay maps
// so the existing vehicle_view.html truthiness checks (AnnualEstimate,
// Difference) render exactly as before.

import (
	"context"
	"database/sql"

	"transport-app/internal/shared"
)

// VehicleDetailQueries is the read model behind the vehicle view/edit pages.
// It is stateless and cheap to construct per request.
type VehicleDetailQueries struct {
	db *sql.DB
}

// NewVehicleDetailQueries wires the read model against the database. A nil db
// degrades to an empty, renderable view.
func NewVehicleDetailQueries(db *sql.DB) *VehicleDetailQueries {
	return &VehicleDetailQueries{db: db}
}

// VehicleMaintenanceStatus is the maintenance block shared by the view and
// edit pages (legacy SQL: the four maintenance columns of the vehicle row).
type VehicleMaintenanceStatus struct {
	MaintenanceDue            string
	MaintenanceOverrideBy     string
	MaintenanceOverrideReason string
	IsMaintenanceDue          bool
	IsMaintenanceOverridden   bool
}

// VehicleLastPosition is the latest telemetry frame mirror for one vehicle.
// A missing row renders as Has=false.
type VehicleLastPosition struct {
	Has   bool
	Lat   float64
	Lng   float64
	Speed float64
	At    string
}

// VehicleRecentTrip is one row of the "recent trips" section.
type VehicleRecentTrip struct {
	ID         string
	TripNumber string
	Status     string
	CreatedAt  sql.NullTime
}

// VehicleOpenWorkOrder is one open job card for a vehicle.
type VehicleOpenWorkOrder struct {
	ID        string
	Title     string
	Status    string
	Assignee  string
	CreatedAt sql.NullTime
}

// VehicleAssignedDriver is the active preferred driver rendered on the view
// page. nil means no active assignment.
type VehicleAssignedDriver struct {
	ID        string
	FirstName string
	LastName  string
	Phone     string
	DriverID  string
	IsPrimary bool
}

// VehicleDriverOption is one selectable driver in the assignment dropdown.
type VehicleDriverOption struct {
	ID        string
	FirstName string
	LastName  string
	DriverID  string
}

// VehicleDetailView carries every section the vehicle view template renders.
type VehicleDetailView struct {
	Maintenance        VehicleMaintenanceStatus
	LastPosition       VehicleLastPosition
	RecentTrips        []VehicleRecentTrip
	OpenWorkOrders     []VehicleOpenWorkOrder
	AssignedDriver     *VehicleAssignedDriver
	AssignDrivers      []VehicleDriverOption
	MeasuringPoints    []map[string]interface{}
	RecentMeasurements []map[string]interface{}
	RecentCommands     []map[string]interface{}
}

// Maintenance loads only the maintenance block (the edit page needs nothing
// else). A scan error leaves every field zero, matching the legacy best-effort
// read.
func (q *VehicleDetailQueries) Maintenance(ctx context.Context, tenantID shared.TenantID, vehicleID string) VehicleMaintenanceStatus {
	if q.db == nil {
		return VehicleMaintenanceStatus{}
	}
	var due, overrideBy, overrideReason sql.NullString
	var overrideAt sql.NullTime
	_ = q.db.QueryRowContext(ctx, `
		SELECT maintenance_due, maintenance_override_by, maintenance_override_at, maintenance_override_reason
		FROM vehicles WHERE id = $1 AND tenant_id = $2`, vehicleID, string(tenantID)).
		Scan(&due, &overrideBy, &overrideAt, &overrideReason)
	return newMaintenanceStatus(due, overrideBy, overrideReason)
}

func newMaintenanceStatus(due, overrideBy, overrideReason sql.NullString) VehicleMaintenanceStatus {
	return VehicleMaintenanceStatus{
		MaintenanceDue:            due.String,
		MaintenanceOverrideBy:     overrideBy.String,
		MaintenanceOverrideReason: overrideReason.String,
		IsMaintenanceDue:          due.Valid && due.String != "",
		IsMaintenanceOverridden:   overrideBy.Valid && overrideBy.String != "",
	}
}

// Load returns the vehicle view-page data. Query order and best-effort
// semantics mirror the legacy handler: maintenance, latest telemetry frame,
// recent trips, open job cards, assigned driver + driver details, assignment
// dropdown, then the measuring-point/measurement/command sections.
func (q *VehicleDetailQueries) Load(ctx context.Context, tenantID shared.TenantID, vehicleID string) VehicleDetailView {
	view := VehicleDetailView{
		RecentTrips:        []VehicleRecentTrip{},
		OpenWorkOrders:     []VehicleOpenWorkOrder{},
		AssignDrivers:      []VehicleDriverOption{},
		MeasuringPoints:    []map[string]interface{}{},
		RecentMeasurements: []map[string]interface{}{},
		RecentCommands:     []map[string]interface{}{},
	}
	if q.db == nil {
		return view
	}

	view.Maintenance = q.Maintenance(ctx, tenantID, vehicleID)
	view.LastPosition = q.lastPosition(ctx, tenantID, vehicleID)
	view.RecentTrips = q.recentTrips(ctx, tenantID, vehicleID)
	view.OpenWorkOrders = q.openWorkOrders(ctx, tenantID, vehicleID)
	view.AssignedDriver = q.assignedDriver(ctx, tenantID, vehicleID)
	view.AssignDrivers = q.assignableDrivers(ctx, tenantID)
	view.MeasuringPoints = q.measuringPoints(ctx, tenantID, vehicleID)
	view.RecentMeasurements = q.recentMeasurements(ctx, tenantID, vehicleID)
	view.RecentCommands = q.recentCommands(ctx, tenantID, vehicleID)
	return view
}

// lastPosition mirrors the legacy vehicle_latest_position lookup: the frame is
// bound to BOTH the vehicle id and the tenant, so a cross-tenant frame never
// leaks. Any scan error (including no row) renders Has=false.
func (q *VehicleDetailQueries) lastPosition(ctx context.Context, tenantID shared.TenantID, vehicleID string) VehicleLastPosition {
	out := VehicleLastPosition{}
	var lat, lng, speed float64
	var at sql.NullString
	if err := q.db.QueryRowContext(ctx, `
		SELECT latitude, longitude, speed, device_time
		FROM vehicle_latest_position WHERE vehicle_id = $1 AND tenant_id = $2`, vehicleID, string(tenantID)).
		Scan(&lat, &lng, &speed, &at); err == nil {
		out = VehicleLastPosition{Has: true, Lat: lat, Lng: lng, Speed: speed, At: at.String}
	}
	return out
}

// recentTrips returns the newest 5 trips for the vehicle (tenant-scoped).
func (q *VehicleDetailQueries) recentTrips(ctx context.Context, tenantID shared.TenantID, vehicleID string) []VehicleRecentTrip {
	out := []VehicleRecentTrip{}
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, trip_number, status, created_at FROM trips
		WHERE vehicle_id = $1 AND tenant_id = $2 ORDER BY created_at DESC LIMIT 5`, vehicleID, string(tenantID))
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var t VehicleRecentTrip
		if rows.Scan(&t.ID, &t.TripNumber, &t.Status, &t.CreatedAt) == nil {
			out = append(out, t)
		}
	}
	return out
}

// openWorkOrders returns up to 10 non-terminal job cards for the vehicle.
func (q *VehicleDetailQueries) openWorkOrders(ctx context.Context, tenantID shared.TenantID, vehicleID string) []VehicleOpenWorkOrder {
	out := []VehicleOpenWorkOrder{}
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, title, status, assignee, created_at FROM work_orders
		WHERE vehicle_id = $1 AND tenant_id = $2 AND status NOT IN ('done','cancelled')
		ORDER BY created_at DESC LIMIT 10`, vehicleID, string(tenantID))
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var wo VehicleOpenWorkOrder
		if rows.Scan(&wo.ID, &wo.Title, &wo.Status, &wo.Assignee, &wo.CreatedAt) == nil {
			out = append(out, wo)
		}
	}
	return out
}

// assignedDriver mirrors the legacy getActiveAssignmentByVehicle helper plus
// the driver-details lookup: the assignment is tenant-scoped, and a missing or
// unreadable driver row still renders the assignment with blank fields (the
// legacy handler ignored companion lookup errors).
func (q *VehicleDetailQueries) assignedDriver(ctx context.Context, tenantID shared.TenantID, vehicleID string) *VehicleAssignedDriver {
	var assignmentID, driverID, assignedVehicleID string
	var isPrimary int
	err := q.db.QueryRowContext(ctx, `
		SELECT id, driver_id, vehicle_id, is_primary FROM driver_preferred_vehicles
		WHERE vehicle_id = $1 AND tenant_id = $2 AND unassigned_at IS NULL LIMIT 1`, vehicleID, string(tenantID)).
		Scan(&assignmentID, &driverID, &assignedVehicleID, &isPrimary)
	if err != nil {
		return nil
	}
	out := &VehicleAssignedDriver{ID: driverID, IsPrimary: isPrimary == 1}
	_ = q.db.QueryRowContext(ctx,
		`SELECT first_name, last_name, phone, driver_id FROM drivers WHERE id = $1 AND tenant_id = $2`,
		driverID, string(tenantID)).
		Scan(&out.FirstName, &out.LastName, &out.Phone, &out.DriverID)
	return out
}

// assignableDrivers returns the tenant drivers offered in the assignment
// dropdown (legacy ORDER BY first_name LIMIT 100, scan failures skipped).
func (q *VehicleDetailQueries) assignableDrivers(ctx context.Context, tenantID shared.TenantID) []VehicleDriverOption {
	rows, err := q.db.QueryContext(ctx, `SELECT id, first_name, last_name, driver_id FROM drivers WHERE tenant_id = $1 ORDER BY first_name LIMIT 100`, string(tenantID))
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var out []VehicleDriverOption
	for rows.Next() {
		var d VehicleDriverOption
		if rows.Scan(&d.ID, &d.FirstName, &d.LastName, &d.DriverID) == nil {
			out = append(out, d)
		}
	}
	return out
}

// measuringPoints lists the SOP IK01 points registered on a vehicle (legacy
// ORDER BY created_at ASC). The map shape keeps the template truthiness for
// the optional annual estimate.
func (q *VehicleDetailQueries) measuringPoints(ctx context.Context, tenantID shared.TenantID, vehicleID string) []map[string]interface{} {
	type point struct {
		ID, Kind, MeasPosition, Unit, Description string
		AnnualEstimate                            sql.NullFloat64
	}
	out := []map[string]interface{}{}
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, kind, meas_position, unit, description, annual_estimate
		FROM vehicle_measuring_points
		WHERE vehicle_id = $1 AND tenant_id = $2
		ORDER BY created_at ASC`, vehicleID, string(tenantID))
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var p point
		if rows.Scan(&p.ID, &p.Kind, &p.MeasPosition, &p.Unit, &p.Description, &p.AnnualEstimate) == nil {
			m := map[string]interface{}{
				"ID": p.ID, "Kind": p.Kind, "MeasPosition": p.MeasPosition,
				"Unit": p.Unit, "Description": p.Description,
			}
			if p.AnnualEstimate.Valid {
				m["AnnualEstimate"] = p.AnnualEstimate.Float64
			}
			out = append(out, m)
		}
	}
	return out
}

// recentMeasurements lists the latest SOP IK11 documents across a vehicle's
// points (counter story for KMPL-style follow-ups).
func (q *VehicleDetailQueries) recentMeasurements(ctx context.Context, tenantID shared.TenantID, vehicleID string) []map[string]interface{} {
	out := []map[string]interface{}{}
	rows, err := q.db.QueryContext(ctx, `
		SELECT m.id, m.point_id, m.counter_reading, m.difference_reading,
			m.measured_at, m.read_by, p.kind, p.meas_position, p.unit
		FROM vehicle_measurements m
		JOIN vehicle_measuring_points p ON p.id = m.point_id
		WHERE p.vehicle_id = $1 AND m.tenant_id = $2
		ORDER BY m.recorded_at DESC LIMIT 20`, vehicleID, string(tenantID))
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var docID, pointID, kind, pos, unit string
		var counter float64
		var diff sql.NullFloat64
		var mAt sql.NullTime
		var readBy sql.NullString
		if rows.Scan(&docID, &pointID, &counter, &diff, &mAt, &readBy, &kind, &pos, &unit) == nil {
			m := map[string]interface{}{
				"ID": docID, "PointID": pointID, "Counter": counter,
				"Kind": kind, "MeasPosition": pos, "Unit": unit,
				"ReadBy": readBy.String,
			}
			if diff.Valid {
				m["Difference"] = diff.Float64
			}
			if mAt.Valid {
				m["MeasuredAt"] = mAt.Time
			}
			out = append(out, m)
		}
	}
	return out
}

// recentCommands lists the last 5 AV commands issued for the vehicle.
func (q *VehicleDetailQueries) recentCommands(ctx context.Context, tenantID shared.TenantID, vehicleID string) []map[string]interface{} {
	out := []map[string]interface{}{}
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, command_type, status, issued_by, created_at
		FROM vehicle_commands
		WHERE vehicle_id = $1 AND tenant_id = $2
		ORDER BY created_at DESC LIMIT 5`, vehicleID, string(tenantID))
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var commandID, command, status, issuedBy string
		var at sql.NullTime
		if rows.Scan(&commandID, &command, &status, &issuedBy, &at) == nil {
			m := map[string]interface{}{
				"ID": commandID, "CommandType": command, "Status": status, "IssuedBy": issuedBy,
			}
			if at.Valid {
				m["CreatedAt"] = at.Time.Format("15:04:05 02-01-2006")
			}
			out = append(out, m)
		}
	}
	return out
}
