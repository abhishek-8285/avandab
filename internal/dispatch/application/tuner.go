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

// TuneOp is one manual edit operation on a planned run (spec §3 tuner.go:
// reorder, move stop between routes, merge/split routes, unassign).
type TuneOp struct {
	// Op is one of: "reorder", "move", "unassign", "merge", "split".
	Op string `json:"op"`
	// StopID targets a single stop (reorder/move/unassign).
	StopID string `json:"stop_id,omitempty"`
	// RouteID targets a route (move destination, unassign scope, split).
	RouteID string `json:"route_id,omitempty"`
	// ToRouteID is the move destination route.
	ToRouteID string `json:"to_route_id,omitempty"`
	// Seq is the 1-based target position (reorder/move).
	Seq int `json:"seq,omitempty"`
	// SplitPart is the 1-based position at which route RouteID splits into a
	// second route on the same vehicle (split). 1 <= SplitPart < len(stops).
	SplitPart int `json:"split_part,omitempty"`
}

// TunerService — spec §3: manual edit ops; every op recomputes the what-if KPI
// (pure function over the mutable plan) and rewrites stop sequences, leg
// distances and durations from coordinates.
type TunerService struct {
	DB *sql.DB
}

func NewTunerService(db *sql.DB) *TunerService {
	return &TunerService{DB: db}
}

// Apply executes the operation, resequences affected routes, recomputes KPI
// and persists everything in one transaction. Same run → same ops → same KPI.
func (s *TunerService) Apply(ctx context.Context, tenantID shared.TenantID, runID string, op TuneOp) (*PlanKPI, error) {
	if tenantID == "" {
		return nil, dispatchdomain.ErrTenantRequired
	}
	if err := s.guardRun(ctx, tenantID, runID); err != nil {
		return nil, err
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Lock the run row against concurrent tunes (last-write-wins by design).
	if _, err := tx.ExecContext(ctx,
		`UPDATE planner_runs SET kpi_json = kpi_json WHERE id = ? AND tenant_id = ?`,
		runID, string(tenantID)); err != nil {
		return nil, fmt.Errorf("lock run: %w", err)
	}

	switch op.Op {
	case "reorder":
		err = s.reorder(ctx, tx, tenantID, runID, op)
	case "move":
		err = s.move(ctx, tx, tenantID, runID, op)
	case "unassign":
		err = s.unassign(ctx, tx, tenantID, runID, op)
	case "merge":
		err = s.merge(ctx, tx, tenantID, runID)
	case "split":
		err = s.split(ctx, tx, tenantID, runID, op)
	default:
		err = fmt.Errorf("%w: unknown op %q", dispatchdomain.ErrInvalidOp, op.Op)
	}
	if err != nil {
		return nil, err
	}

	kpi, err := s.resequenceAndKPI(ctx, tx, tenantID, runID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tune: %w", err)
	}
	return kpi, nil
}

// guardRun verifies tenant scope and that the run is still editable.
func (s *TunerService) guardRun(ctx context.Context, tenantID shared.TenantID, runID string) error {
	var status string
	err := s.DB.QueryRowContext(ctx,
		`SELECT status FROM planner_runs WHERE id=? AND tenant_id=?`,
		runID, string(tenantID)).Scan(&status)
	if err == sql.ErrNoRows {
		return dispatchdomain.ErrRunNotFound
	}
	if err != nil {
		return fmt.Errorf("load run: %w", err)
	}
	if status == "dispatched" || status == "done" {
		return dispatchdomain.ErrRunCommitted
	}
	return nil
}

// reorder moves StopID to 1-based position Seq within its route.
func (s *TunerService) reorder(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, runID string, op TuneOp) error {
	if op.StopID == "" || op.Seq < 1 {
		return fmt.Errorf("%w: reorder needs stop_id and seq>=1", dispatchdomain.ErrInvalidOp)
	}
	var routeID string
	err := tx.QueryRowContext(ctx,
		`SELECT route_id FROM planned_stops WHERE id=? AND tenant_id=? AND run_id=?`,
		op.StopID, string(tenantID), runID).Scan(&routeID)
	if err == sql.ErrNoRows {
		return dispatchdomain.ErrStopNotFound
	}
	if err != nil {
		return fmt.Errorf("load stop: %w", err)
	}
	if routeID == "" {
		return fmt.Errorf("%w: cannot reorder an unassigned stop", dispatchdomain.ErrInvalidOp)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE planned_stops SET seq = -seq WHERE id = ? AND tenant_id = ?`,
		op.StopID, string(tenantID)); err != nil {
		return err
	}
	// Close the gap: stops after the old position shift down by one.
	if _, err := tx.ExecContext(ctx,
		`UPDATE planned_stops SET seq = seq - 1
		 WHERE tenant_id=? AND route_id=? AND seq > (SELECT -seq FROM planned_stops WHERE id=?)
		   AND seq > 0`, // second seq>0 keeps the negated row out
		string(tenantID), routeID, op.StopID); err != nil {
		return err
	}
	// Open the gap at the new position.
	if _, err := tx.ExecContext(ctx,
		`UPDATE planned_stops SET seq = seq + 1
		 WHERE tenant_id=? AND route_id=? AND seq >= ? AND seq > 0 AND id != ?`,
		string(tenantID), routeID, op.Seq, op.StopID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE planned_stops SET seq = ? WHERE id = ? AND tenant_id = ?`,
		op.Seq, op.StopID, string(tenantID))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return dispatchdomain.ErrStopNotFound
	}
	return nil
}

// move transfers StopID from its route to ToRouteID at position Seq.
// Moving to route_id ” (empty) unassigns the stop. The destination route
// must belong to the same run.
func (s *TunerService) move(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, runID string, op TuneOp) error {
	if op.StopID == "" {
		return fmt.Errorf("%w: move needs stop_id", dispatchdomain.ErrInvalidOp)
	}
	var fromRoute string
	err := tx.QueryRowContext(ctx,
		`SELECT route_id FROM planned_stops WHERE id=? AND tenant_id=? AND run_id=?`,
		op.StopID, string(tenantID), runID).Scan(&fromRoute)
	if err == sql.ErrNoRows {
		return dispatchdomain.ErrStopNotFound
	}
	if err != nil {
		return fmt.Errorf("load stop: %w", err)
	}
	if fromRoute == "" {
		return fmt.Errorf("%w: stop is already unassigned", dispatchdomain.ErrInvalidOp)
	}
	if op.ToRouteID == "" {
		return s.unassign(ctx, tx, tenantID, runID, TuneOp{Op: "unassign", StopID: op.StopID})
	}
	// Destination route must be part of this run.
	var destExists string
	err = tx.QueryRowContext(ctx,
		`SELECT 'ok' FROM planned_routes WHERE id=? AND tenant_id=? AND run_id=?`,
		op.ToRouteID, string(tenantID), runID).Scan(&destExists)
	if err == sql.ErrNoRows {
		return dispatchdomain.ErrRouteNotFound
	}
	if err != nil {
		return fmt.Errorf("load dest route: %w", err)
	}

	// Remove from source (compact later during resequence).
	if _, err := tx.ExecContext(ctx,
		`UPDATE planned_stops SET route_id='', status='unassigned', planned_eta=NULL, planned_duration_min=NULL
		 WHERE id=? AND tenant_id=?`, op.StopID, string(tenantID)); err != nil {
		return err
	}
	// Append to destination tail; resequenceAndKPI normalises seq.
	if _, err := tx.ExecContext(ctx,
		`UPDATE planned_stops SET route_id=?, status='pending', seq=(SELECT COALESCE(MAX(seq),0)+1 FROM planned_stops WHERE route_id=? AND tenant_id=?)
		 WHERE id=? AND tenant_id=?`,
		op.ToRouteID, op.ToRouteID, string(tenantID), op.StopID, string(tenantID)); err != nil {
		return err
	}
	return nil
}

// unassign detaches one stop (StopID) or all stops of a route (RouteID empty
// StopID) back to the unassigned pool.
func (s *TunerService) unassign(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, runID string, op TuneOp) error {
	if op.StopID != "" {
		res, err := tx.ExecContext(ctx,
			`UPDATE planned_stops SET route_id='', status='unassigned', seq=0, planned_eta=NULL, planned_duration_min=NULL
			 WHERE id=? AND tenant_id=? AND run_id=? AND route_id != ''`,
			op.StopID, string(tenantID), runID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return dispatchdomain.ErrStopNotFound
		}
		return nil
	}
	if op.RouteID != "" {
		var cnt int
		err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM planned_routes WHERE id=? AND tenant_id=? AND run_id=?`,
			op.RouteID, string(tenantID), runID).Scan(&cnt)
		if err != nil {
			return err
		}
		if cnt == 0 {
			return dispatchdomain.ErrRouteNotFound
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE planned_stops SET route_id='', status='unassigned', seq=0, planned_eta=NULL, planned_duration_min=NULL
			 WHERE tenant_id=? AND route_id=?`,
			string(tenantID), op.RouteID)
		return err
	}
	return fmt.Errorf("%w: unassign needs stop_id or route_id", dispatchdomain.ErrInvalidOp)
}

// merge folds every other route of the run into the seq-lowest route,
// preserving per-route order (route A stops then route B stops, ...).
func (s *TunerService) merge(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, runID string) error {
	var keepID string
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM planned_routes WHERE tenant_id=? AND run_id=? ORDER BY seq ASC LIMIT 1`,
		string(tenantID), runID).Scan(&keepID)
	if err == sql.ErrNoRows {
		return dispatchdomain.ErrRouteNotFound
	}
	if err != nil {
		return fmt.Errorf("find keep route: %w", err)
	}
	// Append all other routes' stops onto the keeper, per-route order kept.
	_, err = tx.ExecContext(ctx, `
		UPDATE planned_stops SET route_id=?, status='pending', planned_eta=NULL, planned_duration_min=NULL,
			seq = seq + (SELECT COALESCE((SELECT COUNT(*) FROM planned_stops WHERE route_id = ?), 0)) + 1000
		 WHERE tenant_id=? AND run_id=? AND route_id != '' AND route_id != ?`,
		keepID, keepID, string(tenantID), runID, keepID)
	if err != nil {
		return err
	}
	// Empty routes disappear.
	_, err = tx.ExecContext(ctx,
		`DELETE FROM planned_routes WHERE tenant_id=? AND run_id=? AND id != ?`,
		string(tenantID), runID, keepID)
	return err
}

// split cuts route RouteID at SplitPart: stops from that position onward move
// to a new route row for the same vehicle.
func (s *TunerService) split(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, runID string, op TuneOp) error {
	if op.RouteID == "" || op.SplitPart < 1 {
		return fmt.Errorf("%w: split needs route_id and split_part>=1", dispatchdomain.ErrInvalidOp)
	}
	var vehicleID sql.NullString
	var stopCount int
	err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(vehicle_id,''), (SELECT COUNT(*) FROM planned_stops WHERE route_id=? AND tenant_id=?)
		 FROM planned_routes WHERE id=? AND tenant_id=? AND run_id=?`,
		op.RouteID, string(tenantID), op.RouteID, string(tenantID), runID).Scan(&vehicleID, &stopCount)
	if err == sql.ErrNoRows {
		return dispatchdomain.ErrRouteNotFound
	}
	if err != nil {
		return fmt.Errorf("load route: %w", err)
	}
	if op.SplitPart >= stopCount {
		return fmt.Errorf("%w: split_part must be before the last stop", dispatchdomain.ErrInvalidSeq)
	}

	newID := newRouteID()
	maxSeq, err := s.maxRouteSeq(ctx, tx, tenantID, runID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO planned_routes (id, tenant_id, run_id, seq, vehicle_id)
		 VALUES (?, ?, ?, ?, ?)`,
		newID, string(tenantID), runID, maxSeq+1, vehicleID); err != nil {
		return err
	}
	// Tail stops (seq >= SplitPart) hop to the new route; resequence normalises.
	_, err = tx.ExecContext(ctx,
		`UPDATE planned_stops SET route_id=?, status='pending', planned_eta=NULL, planned_duration_min=NULL
		 WHERE tenant_id=? AND route_id=? AND seq >= ?`,
		newID, string(tenantID), op.RouteID, op.SplitPart)
	return err
}

func newRouteID() string {
	return uuid.NewString()
}

func (s *TunerService) maxRouteSeq(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, runID string) (int, error) {
	var maxSeq sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM planned_routes WHERE tenant_id=? AND run_id=?`,
		string(tenantID), runID).Scan(&maxSeq)
	if err != nil {
		return 0, err
	}
	return int(maxSeq.Int64), nil
}

// routeStopRow is one stop of a route, for KPI recompute.
type routeStopRow struct {
	id            string
	lat, lng, dem float64
}

// loadRouteStops reads all stops of one route (fully drained, rows closed).
func loadRouteStops(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, routeID string) ([]routeStopRow, error) {
	legs, err := tx.QueryContext(ctx,
		`SELECT id, lat, lng, COALESCE(demand,0) FROM planned_stops WHERE tenant_id=? AND route_id=? ORDER BY seq ASC`,
		string(tenantID), routeID)
	if err != nil {
		return nil, fmt.Errorf("load route stops: %w", err)
	}
	defer func() { _ = legs.Close() }()
	var rows []routeStopRow
	for legs.Next() {
		var l routeStopRow
		if err := legs.Scan(&l.id, &l.lat, &l.lng, &l.dem); err != nil {
			return nil, err
		}
		rows = append(rows, l)
	}
	return rows, legs.Err()
}

// resequenceAndKPI normalises seq 1..N per route (and the unassigned pool),
// recomputes leg distances/durations + per-stop ETAs from coordinates, then
// rebuilds the run KPI. This is the "what-if" pure-function part of the tuner.
func (s *TunerService) resequenceAndKPI(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, runID string) (*PlanKPI, error) {
	depotLat, depotLng, ok := s.depotCoords(ctx, tenantID)
	if !ok {
		return nil, dispatchdomain.ErrNoVehicles
	}

	kpi := &PlanKPI{}

	// Routes in seq order.
	routes, err := tx.QueryContext(ctx,
		`SELECT id, COALESCE(vehicle_id,'') FROM planned_routes WHERE tenant_id=? AND run_id=? ORDER BY seq ASC`,
		string(tenantID), runID)
	if err != nil {
		return nil, fmt.Errorf("load routes: %w", err)
	}
	defer func() { _ = routes.Close() }()
	type rrow struct{ id, vehicleID string }
	var rrows []rrow
	for routes.Next() {
		var r rrow
		if err := routes.Scan(&r.id, &r.vehicleID); err != nil {
			return nil, err
		}
		rrows = append(rrows, r)
	}
	if err := routes.Err(); err != nil {
		return nil, err
	}
	_ = routes.Close()

	for _, r := range rrows {
		legsRows, err := loadRouteStops(ctx, tx, tenantID, r.id)
		if err != nil {
			return nil, err
		}
		if len(legsRows) == 0 {
			// Empty route after moves — drop it.
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM planned_routes WHERE id=? AND tenant_id=?`, r.id, string(tenantID)); err != nil {
				return nil, err
			}
			continue
		}
		kpi.VehiclesUsed++

		// Recompute distances/durations/seq/ETA from real coordinates.
		curLat, curLng := depotLat, depotLng
		for i, l := range legsRows {
			d := optimizer.HaversineKM(curLat, curLng, l.lat, l.lng)
			dur := d / optimizer.DefaultSpeedKMH * 60.0
			kpi.TotalKM += d
			kpi.TotalMin += dur
			kpi.TotalCost += d + dur
			// ETA computed in Go: sqlite datetime() has no PG equivalent and
			// both drivers bind time.Time (sqlite DATETIME, pg TIMESTAMPTZ).
			eta := time.Now().Add(time.Duration(kpi.TotalMin * float64(time.Minute)))
			if _, err := tx.ExecContext(ctx,
				`UPDATE planned_stops SET seq=?, planned_duration_min=?, planned_eta=?
			 WHERE id=? AND tenant_id=?`,
				i+1, dur, eta, l.id, string(tenantID)); err != nil {
				return nil, fmt.Errorf("reseq stop: %w", err)
			}
			curLat, curLng = l.lat, l.lng
		}
	}

	// Unassigned count.
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM planned_stops WHERE tenant_id=? AND run_id=? AND route_id=''`,
		string(tenantID), runID).Scan(&kpi.UnassignedCount); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE planner_runs SET status='planned', kpi_json = ?
		 WHERE id=? AND tenant_id=?`,
		mustJSON(kpi), runID, string(tenantID)); err != nil {
		return nil, fmt.Errorf("update kpi: %w", err)
	}
	return kpi, nil
}

// depotCoords mirrors PlannerService.depot — first facility with coordinates.
func (s *TunerService) depotCoords(ctx context.Context, tenantID shared.TenantID) (float64, float64, bool) {
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

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
