package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	dispatchdomain "transport-app/internal/dispatch/domain"
	"transport-app/internal/route/optimizer"
	"transport-app/internal/shared"
)

// PlannerStopInput is one order/stop fed into a planner run.
// Either BookingID (resolved to pickup+dropoff via facility links, migration
// 00159) or explicit Lat/Lng+Address (adhoc) must be provided.
type PlannerStopInput struct {
	BookingID string  `json:"booking_id,omitempty"`
	Type      string  `json:"type,omitempty"`    // pickup|dropoff|waypoint (default dropoff)
	Address   string  `json:"address,omitempty"` // required for adhoc stops
	Lat       float64 `json:"lat,omitempty"`
	Lng       float64 `json:"lng,omitempty"`
	Demand    float64 `json:"demand,omitempty"`
}

// CreateRunCommand starts a planner run (status=draft).
type CreateRunCommand struct {
	TenantID    shared.TenantID
	ActorID     string
	Source      string // "orders" | "adhoc"
	Stops       []PlannerStopInput
	Constraints optimizer.Constraints
}

// PlanCommand solves a run: optimizer → planned_routes/planned_stops + KPI.
type PlanCommand struct {
	TenantID shared.TenantID
	RunID    string
	Provider string // optional optimizer override; empty = config default
}

// PlanKPI is the what-if summary persisted as planner_runs.kpi_json.
type PlanKPI struct {
	TotalKM         float64 `json:"total_km"`
	TotalMin        float64 `json:"total_min"`
	TotalCost       float64 `json:"total_cost"`
	UnassignedCount int     `json:"unassigned_count"`
	VehiclesUsed    int     `json:"vehicles_used"`
}

// PlannerService — spec docs/design/dispatcher-route-planner/03-cto-architecture.md §3.
// P0 scope: create run from orders/adhoc stops → solve → persist plan → KPI.
type PlannerService struct {
	DB       *sql.DB
	Optimize func(provider string) optimizer.Optimizer // provider factory (injectable for tests)
}

func NewPlannerService(db *sql.DB, optimize func(provider string) optimizer.Optimizer) *PlannerService {
	if optimize == nil {
		optimize = optimizer.Get
	}
	return &PlannerService{DB: db, Optimize: optimize}
}

// CreateRun validates stops (resolving booking→facility coords), inserts the
// draft run and its stops as unassigned planned_stops.
func (s *PlannerService) CreateRun(ctx context.Context, cmd CreateRunCommand) (string, error) {
	if cmd.TenantID == "" {
		return "", dispatchdomain.ErrTenantRequired
	}
	if len(cmd.Stops) == 0 {
		return "", dispatchdomain.ErrNoStops
	}
	source := cmd.Source
	if source != "orders" && source != "adhoc" {
		source = "orders"
	}

	runID := uuid.NewString()
	type stopRow struct {
		bookingID, stopType, address string
		lat, lng, demand             float64
	}
	stops := make([]stopRow, 0, len(cmd.Stops))

	for i, in := range cmd.Stops {
		st := stopRow{bookingID: in.BookingID, stopType: in.Type, demand: in.Demand}
		switch st.stopType {
		case "pickup", "dropoff", "waypoint":
		case "":
			st.stopType = "dropoff"
		default:
			return "", dispatchdomain.ErrInvalidStopType
		}
		if in.BookingID != "" {
			// Resolve coordinates via booking → facility links (00159).
			var addr string
			var lat, lng float64
			err := s.DB.QueryRowContext(ctx, `
				SELECT CASE WHEN st.stop_type = 'pickup'
					THEN COALESCE(pf.name, '') ELSE COALESCE(df.name, '') END,
				       CASE WHEN st.stop_type = 'pickup'
					THEN COALESCE(pf.latitude, 0) ELSE COALESCE(df.latitude, 0) END,
				       CASE WHEN st.stop_type = 'pickup'
					THEN COALESCE(pf.longitude, 0) ELSE COALESCE(df.longitude, 0) END
				FROM (SELECT ? AS stop_type) st
				JOIN bookings b ON b.id = ? AND b.tenant_id = ?
				LEFT JOIN facilities pf ON pf.id = b.pickup_facility_id
				LEFT JOIN facilities df ON df.id = b.drop_facility_id`,
				st.stopType, in.BookingID, string(cmd.TenantID)).Scan(&addr, &lat, &lng)
			if err != nil || lat == 0 && lng == 0 {
				return "", fmt.Errorf("%w: %s", dispatchdomain.ErrBookingUnknown, in.BookingID)
			}
			st.address, st.lat, st.lng = addr, lat, lng
		} else {
			if in.Address == "" || in.Lat == 0 && in.Lng == 0 {
				return "", fmt.Errorf("%w: stop %d needs address and lat/lng", dispatchdomain.ErrBookingUnknown, i)
			}
			st.address, st.lat, st.lng = in.Address, in.Lat, in.Lng
		}
		stops = append(stops, st)
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO planner_runs (id, tenant_id, status, source, created_by)
		VALUES (?, ?, 'draft', ?, ?)`,
		runID, string(cmd.TenantID), source, cmd.ActorID); err != nil {
		return "", fmt.Errorf("insert planner_run: %w", err)
	}

	for i, st := range stops {
		if _, err := tx.ExecContext(ctx, `
		INSERT INTO planned_stops
			(id, tenant_id, run_id, route_id, seq, booking_id, source_type, address, lat, lng, demand, status)
		VALUES (?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?, 'unassigned')`,
			uuid.NewString(), string(cmd.TenantID), runID, i+1, nullIfEmpty(st.bookingID),
			st.stopType, st.address, st.lat, st.lng, st.demand); err != nil {
			return "", fmt.Errorf("insert planned_stop: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit run: %w", err)
	}
	return runID, nil
}

// Plan solves the run via the optimizer and persists planned_routes +
// planned_stops. Deterministic: same input → same plan (optimizer contract).
func (s *PlannerService) Plan(ctx context.Context, cmd PlanCommand) (*PlanKPI, error) {
	if cmd.TenantID == "" {
		return nil, dispatchdomain.ErrTenantRequired
	}

	run, err := s.loadRun(ctx, cmd.TenantID, cmd.RunID)
	if err != nil {
		return nil, err
	}
	if run.status == "dispatched" || run.status == "done" {
		return nil, dispatchdomain.ErrRunCommitted
	}

	// Fleet: tenant vehicles with coordinates (depot = facility or fallback city centre).
	vehicles, err := s.loadVehicles(ctx, cmd.TenantID)
	if err != nil {
		return nil, err
	}
	if len(vehicles) == 0 {
		return nil, dispatchdomain.ErrNoVehicles
	}

	// All stops of the run become shipments (re-solve re-plans the whole run).
	type stopRow struct {
		id, bookingID, address string
		lat, lng, demand       float64
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, COALESCE(booking_id,''), address, lat, lng, COALESCE(demand,0)
		FROM planned_stops
		WHERE tenant_id = ? AND run_id = ?
		ORDER BY seq ASC`, string(cmd.TenantID), cmd.RunID)
	if err != nil {
		return nil, fmt.Errorf("load stops: %w", err)
	}
	defer func() { _ = rows.Close() }()
	stopRows := []stopRow{}
	for rows.Next() {
		var r stopRow
		if err := rows.Scan(&r.id, &r.bookingID, &r.address, &r.lat, &r.lng, &r.demand); err != nil {
			return nil, fmt.Errorf("scan stop: %w", err)
		}
		stopRows = append(stopRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(stopRows) == 0 {
		return nil, dispatchdomain.ErrNoStops
	}

	shipments := make([]optimizer.Shipment, len(stopRows))
	for i, r := range stopRows {
		shipments[i] = optimizer.Shipment{
			ID: r.id, Latitude: r.lat, Longitude: r.lng, Demand: r.demand,
		}
	}
	in := optimizer.OptimizationInput{
		Shipments:   shipments,
		Vehicles:    vehicles,
		Constraints: run.constraints,
	}
	opt := s.Optimize(cmd.Provider)
	out, err := opt.Solve(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("solve: %w", err)
	}

	// Persist plan atomically: wipe previous plan, write routes + stops.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`UPDATE planned_stops SET route_id='', status='unassigned' WHERE tenant_id=? AND run_id=? AND route_id != ''`,
		string(cmd.TenantID), cmd.RunID); err != nil {
		return nil, fmt.Errorf("unassign old stops: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM planned_routes WHERE tenant_id=? AND run_id=?`,
		string(cmd.TenantID), cmd.RunID); err != nil {
		return nil, fmt.Errorf("clear old routes: %w", err)
	}

	assigned := map[string]bool{}
	usedVehicles := 0
	for seq, route := range out.Routes {
		if len(route.Legs) == 0 {
			continue
		}
		usedVehicles++
		routeID := uuid.NewString()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO planned_routes (id, tenant_id, run_id, seq, vehicle_id)
			VALUES (?, ?, ?, ?, ?)`,
			routeID, string(cmd.TenantID), cmd.RunID, seq+1, route.VehicleID); err != nil {
			return nil, fmt.Errorf("insert planned_route: %w", err)
		}
		for _, leg := range route.Legs {
			assigned[leg.ShipmentID] = true
			plannedEta := run.plannedStart.Add(time.Duration(leg.DurationMin) * time.Minute)
			if _, err := tx.ExecContext(ctx, `
				UPDATE planned_stops SET route_id=?, seq=?, status='pending',
					planned_eta=?, planned_duration_min=?
				WHERE id=? AND tenant_id=?`,
				routeID, leg.Sequence, plannedEta, leg.DurationMin, leg.ShipmentID, string(cmd.TenantID)); err != nil {
				return nil, fmt.Errorf("assign stop: %w", err)
			}
		}
	}

	kpi := PlanKPI{
		TotalKM:         out.TotalKM,
		TotalMin:        0,
		TotalCost:       out.TotalCost,
		UnassignedCount: len(stopRows) - len(assigned),
		VehiclesUsed:    usedVehicles,
	}
	for _, route := range out.Routes {
		kpi.TotalMin += route.TotalMin
	}
	kpiJSON, _ := json.Marshal(kpi)
	if _, err := tx.ExecContext(ctx,
		`UPDATE planner_runs SET status='planned', job_id=NULL, kpi_json=? WHERE id=? AND tenant_id=?`,
		string(kpiJSON), cmd.RunID, string(cmd.TenantID)); err != nil {
		return nil, fmt.Errorf("update run: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit plan: %w", err)
	}
	return &kpi, nil
}

type runRow struct {
	id, status, source, kpiJSON string
	constraints                 optimizer.Constraints
	plannedStart                time.Time
}

func (s *PlannerService) loadRun(ctx context.Context, tenantID shared.TenantID, runID string) (*runRow, error) {
	var r runRow
	var kpi sql.NullString
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, status, source, COALESCE(kpi_json,'') FROM planner_runs WHERE id=? AND tenant_id=?`,
		runID, string(tenantID)).Scan(&r.id, &r.status, &r.source, &kpi)
	if err == sql.ErrNoRows {
		return nil, dispatchdomain.ErrRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load run: %w", err)
	}
	r.kpiJSON = kpi.String
	r.plannedStart = time.Now().UTC()
	return &r, nil
}

// loadVehicles returns tenant fleet with depot coords. Depot resolution:
// first facility with coords (tenant-scoped), else vehicle start defaults.
func (s *PlannerService) loadVehicles(ctx context.Context, tenantID shared.TenantID) ([]optimizer.Vehicle, error) {
	depotLat, depotLng, ok := s.depot(ctx, tenantID)
	vehicles := []optimizer.Vehicle{}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, COALESCE(capacity,0) FROM vehicles
		WHERE tenant_id = ? AND status != 'blocked'
		ORDER BY id ASC LIMIT 50`, string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("load vehicles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var cap float64
		if err := rows.Scan(&id, &cap); err != nil {
			return nil, err
		}
		v := optimizer.Vehicle{ID: id, Capacity: cap}
		if ok {
			v.StartLat, v.StartLng = depotLat, depotLng
		}
		vehicles = append(vehicles, v)
	}
	// Fleet without depot: optimizer needs start coords; default all vehicles
	// to first stop's location is wrong — reject instead (halt rule).
	if !ok {
		return nil, dispatchdomain.ErrNoVehicles
	}
	return vehicles, nil
}

// depot picks the tenant's primary facility with coordinates.
func (s *PlannerService) depot(ctx context.Context, tenantID shared.TenantID) (float64, float64, bool) {
	var lat, lng float64
	err := s.DB.QueryRowContext(ctx, `
		SELECT latitude, longitude FROM facilities
		WHERE tenant_id = ? AND latitude IS NOT NULL AND latitude != 0
		ORDER BY created_at ASC LIMIT 1`, string(tenantID)).Scan(&lat, &lng)
	if err != nil {
		return 0, 0, false
	}
	return lat, lng, true
}

// GetRun returns run + routes + stops for the detail view.
func (s *PlannerService) GetRun(ctx context.Context, tenantID shared.TenantID, runID string) (*RunDetail, error) {
	run, err := s.loadRun(ctx, tenantID, runID)
	if err != nil {
		return nil, err
	}
	detail := &RunDetail{ID: run.id, Status: run.status, Source: run.source}
	if run.kpiJSON != "" {
		_ = json.Unmarshal([]byte(run.kpiJSON), &detail.KPI)
	}

	detail.Stops = []StopView{}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, COALESCE(booking_id,''), source_type, address, lat, lng,
		       COALESCE(demand,0), status, seq, planned_eta
		FROM planned_stops WHERE tenant_id=? AND run_id=? AND route_id=''
		ORDER BY seq ASC`, string(tenantID), runID)
	if err != nil {
		return nil, fmt.Errorf("load run stops: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var v StopView
		var eta sql.NullTime
		if err := rows.Scan(&v.ID, &v.BookingID, &v.Type, &v.Address, &v.Lat, &v.Lng,
			&v.Demand, &v.Status, &v.Seq, &eta); err != nil {
			return nil, err
		}
		if eta.Valid {
			t := eta.Time
			v.PlannedETA = &t
		}
		detail.Stops = append(detail.Stops, v)
	}

	detail.Routes = []RouteView{}
	routes, qerr := s.DB.QueryContext(ctx, `
		SELECT id, COALESCE(vehicle_id,''), seq FROM planned_routes
		WHERE tenant_id=? AND run_id=? ORDER BY seq ASC`, string(tenantID), runID)
	if qerr != nil {
		return nil, fmt.Errorf("load routes: %w", qerr)
	}
	defer func() { _ = routes.Close() }()
	routeByID := map[string]*RouteView{}
	for routes.Next() {
		var id, vehID string
		var seq int
		if err := routes.Scan(&id, &vehID, &seq); err != nil {
			return nil, err
		}
		routeByID[id] = &RouteView{ID: id, VehicleID: vehID, Seq: seq, Stops: []StopView{}}
		detail.Routes = append(detail.Routes, *routeByID[id])
	}

	routeStops, err := s.DB.QueryContext(ctx, `
		SELECT id, COALESCE(booking_id,''), source_type, address, lat, lng,
		       COALESCE(demand,0), status, seq, route_id, planned_eta
		FROM planned_stops WHERE tenant_id=? AND run_id=? AND route_id != '' ORDER BY seq ASC`,
		string(tenantID), runID)
	if err != nil {
		return nil, fmt.Errorf("load route stops: %w", err)
	}
	defer func() { _ = routeStops.Close() }()
	// Re-point to map entries so appended stops land in the actual route.
	byID := map[string]*RouteView{}
	for i := range detail.Routes {
		byID[detail.Routes[i].ID] = &detail.Routes[i]
	}
	for routeStops.Next() {
		var v StopView
		var routeID string
		var eta sql.NullTime
		if err := routeStops.Scan(&v.ID, &v.BookingID, &v.Type, &v.Address, &v.Lat, &v.Lng,
			&v.Demand, &v.Status, &v.Seq, &routeID, &eta); err != nil {
			return nil, err
		}
		if eta.Valid {
			t := eta.Time
			v.PlannedETA = &t
		}
		if rv, ok := byID[routeID]; ok {
			rv.Stops = append(rv.Stops, v)
		}
	}
	return detail, nil
}

// ListRuns returns recent runs for the board.
func (s *PlannerService) ListRuns(ctx context.Context, tenantID shared.TenantID, limit int) ([]RunSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, status, source, COALESCE(kpi_json,''), created_at
		FROM planner_runs WHERE tenant_id=? ORDER BY created_at DESC LIMIT ?`,
		string(tenantID), limit)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	runs := []RunSummary{}
	for rows.Next() {
		var r RunSummary
		var kpi sql.NullString
		if err := rows.Scan(&r.ID, &r.Status, &r.Source, &kpi, &r.CreatedAt); err != nil {
			return nil, err
		}
		if kpi.String != "" {
			_ = json.Unmarshal([]byte(kpi.String), &r.KPI)
		}
		runs = append(runs, r)
	}
	return runs, nil
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
