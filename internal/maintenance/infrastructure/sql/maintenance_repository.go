package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/maintenance/domain"
	"transport-app/internal/shared"
)

// MaintenanceRepository handles raw SQL persistence for schedules, records, and DTC events (Spec 04 §6, §3).
type MaintenanceRepository struct {
	db *sql.DB
}

// NewMaintenanceRepository creates a new SQLite MaintenanceRepository instance.
func NewMaintenanceRepository(db *sql.DB) *MaintenanceRepository {
	return &MaintenanceRepository{db: db}
}

// ListActiveSchedules returns active schedules, optionally filtered by vehicleID.
func (r *MaintenanceRepository) ListActiveSchedules(ctx context.Context, vehicleID string) ([]domain.Schedule, error) {
	return r.listSchedules(ctx, vehicleID, "", true)
}

func (r *MaintenanceRepository) ListActiveSchedulesForTenant(ctx context.Context, vehicleID, tenantID string) ([]domain.Schedule, error) {
	return r.listSchedules(ctx, vehicleID, tenantID, true)
}

func (r *MaintenanceRepository) listSchedules(ctx context.Context, vehicleID, tenantID string, activeOnly bool) ([]domain.Schedule, error) {
	query := `SELECT s.id, s.vehicle_id, s.service_type, s.interval_km, s.interval_days,
	                 s.last_done_km, s.last_done_at, s.due_km, s.due_at, s.active, s.created_at, s.updated_at
	          FROM maintenance_schedules s`
	var args []interface{}
	clauses := []string{}
	if activeOnly {
		clauses = append(clauses, "s.active = 1")
	}
	if vehicleID != "" {
		clauses = append(clauses, "s.vehicle_id = ?")
		args = append(args, vehicleID)
	}
	if tenantID != "" {
		clauses = append(clauses, "s.vehicle_id IN (SELECT id FROM vehicles WHERE tenant_id = ?)")
		args = append(args, tenantID)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY s.created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.Schedule
	for rows.Next() {
		var s domain.Schedule
		var intKM, lastKM, dueKM sql.NullFloat64
		var intDays sql.NullInt64
		var lastDoneAt, dueAt sql.NullTime
		var lastDoneAtStr, dueAtStr sql.NullString
		if err := rows.Scan(
			&s.ID, &s.VehicleID, &s.ServiceType, &intKM, &intDays,
			&lastKM, &lastDoneAt, &dueKM, &dueAt, &s.Active, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			// Fallback string parse for SQLite timestamp columns
			continue
		}
		if intKM.Valid {
			s.IntervalKM = &intKM.Float64
		}
		if intDays.Valid {
			d := int(intDays.Int64)
			s.IntervalDays = &d
		}
		if lastKM.Valid {
			s.LastDoneKM = &lastKM.Float64
		}
		if lastDoneAt.Valid {
			t := lastDoneAt.Time.UTC()
			s.LastDoneAt = &t
		} else if lastDoneAtStr.Valid && lastDoneAtStr.String != "" {
			if t, err := time.Parse("2006-01-02 15:04:05", lastDoneAtStr.String); err == nil {
				tUTC := t.UTC()
				s.LastDoneAt = &tUTC
			}
		}
		if dueKM.Valid {
			s.DueKM = &dueKM.Float64
		}
		if dueAt.Valid {
			t := dueAt.Time.UTC()
			s.DueAt = &t
		} else if dueAtStr.Valid && dueAtStr.String != "" {
			if t, err := time.Parse("2006-01-02 15:04:05", dueAtStr.String); err == nil {
				tUTC := t.UTC()
				s.DueAt = &tUTC
			}
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// ListSchedules returns all schedules (active and inactive).
func (r *MaintenanceRepository) ListSchedules(ctx context.Context, vehicleID string) ([]domain.Schedule, error) {
	return r.listSchedules(ctx, vehicleID, "", false)
}

func (r *MaintenanceRepository) ListSchedulesForTenant(ctx context.Context, vehicleID, tenantID string) ([]domain.Schedule, error) {
	return r.listSchedules(ctx, vehicleID, tenantID, false)
}

// GetLatestOdometer returns the max odometer from snapshots, falling back to vehicles.odometer / current_mileage.
func (r *MaintenanceRepository) GetLatestOdometer(ctx context.Context, vehicleID string) (float64, error) {
	var snapOdo sql.NullFloat64
	err := r.db.QueryRowContext(ctx, `
		SELECT MAX(odometer) FROM telemetry_snapshots
		WHERE vehicle_id = ? AND odometer IS NOT NULL`, vehicleID).Scan(&snapOdo)
	if err == nil && snapOdo.Valid && snapOdo.Float64 > 0 {
		return snapOdo.Float64, nil
	}

	var vehOdo sql.NullFloat64
	err = r.db.QueryRowContext(ctx, `
		SELECT COALESCE(odometer, current_mileage, 0) FROM vehicles
		WHERE id = ?`, vehicleID).Scan(&vehOdo)
	if err == nil && vehOdo.Valid {
		return vehOdo.Float64, nil
	}
	return 0, nil
}

// SetMaintenanceDue sets the DATE on vehicles.maintenance_due (DATE semantics from 00042).
func (r *MaintenanceRepository) SetMaintenanceDue(ctx context.Context, vehicleID string, dueDate time.Time) error {
	dateStr := dueDate.UTC().Format("2006-01-02")
	_, err := r.db.ExecContext(ctx, `
		UPDATE vehicles
		SET maintenance_due = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND maintenance_due IS NULL`, dateStr, vehicleID)
	return err
}

// ClearMaintenanceDue clears vehicles.maintenance_due to NULL and resets overrides (Spec 04 §6).
func (r *MaintenanceRepository) ClearMaintenanceDue(ctx context.Context, vehicleID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE vehicles
		SET maintenance_due = NULL,
		    maintenance_override_by = NULL,
		    maintenance_override_at = NULL,
		    maintenance_override_reason = NULL,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, vehicleID)
	return err
}

// IsMaintenanceBlocked checks if a vehicle is blocked for maintenance (Spec 04 §6, §12).
// Returns (blocked bool, reason string, err error).
// If an admin override is active, blocked is false.
func (r *MaintenanceRepository) IsMaintenanceBlocked(ctx context.Context, vehicleID string) (bool, string, error) {
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

	var hasCriticalDTC bool
	var dtcCode string
	err = r.db.QueryRowContext(ctx, `
		SELECT dtc_code FROM dtc_events
		WHERE vehicle_id = ? AND severity = 'critical' AND resolved_at IS NULL
		ORDER BY occurred_at DESC LIMIT 1`, vehicleID).Scan(&dtcCode)
	if err == nil && dtcCode != "" {
		hasCriticalDTC = true
	}

	isDue := due.Valid && due.String != ""
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

// InsertDtcEvent inserts a DTC event with minute-granularity storm dedup (Spec 04 §6, §3).
func (r *MaintenanceRepository) InsertDtcEvent(ctx context.Context, evt domain.DtcEvent) (bool, error) {
	truncated := evt.OccurredAt.UTC().Truncate(time.Minute).Format("2006-01-02 15:04:05")
	res, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO dtc_events (id, vehicle_id, trip_id, dtc_code, severity, description, raw_payload, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		evt.ID, evt.VehicleID, evt.TripID, evt.DtcCode, evt.Severity, evt.Description, evt.RawPayload, truncated,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListUnresolvedCriticalDtc returns all unresolved critical DTCs for a vehicle.
func (r *MaintenanceRepository) ListUnresolvedCriticalDtc(ctx context.Context, vehicleID string) ([]domain.DtcEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, vehicle_id, trip_id, dtc_code, severity, description, raw_payload, occurred_at
		FROM dtc_events
		WHERE vehicle_id = ? AND severity = 'critical' AND resolved_at IS NULL
		ORDER BY occurred_at DESC`, vehicleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.DtcEvent
	for rows.Next() {
		var e domain.DtcEvent
		var tripID, desc, raw sql.NullString
		var occ time.Time
		if err := rows.Scan(&e.ID, &e.VehicleID, &tripID, &e.DtcCode, &e.Severity, &desc, &raw, &occ); err != nil {
			continue
		}
		if tripID.Valid {
			e.TripID = &tripID.String
		}
		if desc.Valid {
			e.Description = &desc.String
		}
		if raw.Valid {
			e.RawPayload = &raw.String
		}
		e.OccurredAt = occ.UTC()
		list = append(list, e)
	}
	return list, rows.Err()
}

// ResolveDtcEvent marks a DTC event as resolved.
func (r *MaintenanceRepository) ResolveDtcEvent(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE dtc_events
		SET resolved_at = CURRENT_TIMESTAMP
		WHERE id = ? AND resolved_at IS NULL`, id)
	return err
}

// InsertRecord adds a completed maintenance record and updates the associated schedule.
func (r *MaintenanceRepository) InsertRecord(ctx context.Context, rec domain.Record) error {
	tenantID := string(shared.TenantIDFromContext(ctx))
	if tenantID == "" {
		// Workshop records carry cost/vendor data: attribute to the
		// vehicle's own org, never the bootstrap tenant.
		_ = r.db.QueryRowContext(ctx, `SELECT tenant_id FROM vehicles WHERE id = ?`, rec.VehicleID).Scan(&tenantID)
	}
	if tenantID == "" {
		return errors.New("maintenance: cannot record without tenant")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO maintenance_records (id, vehicle_id, schedule_id, service_type, performed_at, odometer_km, cost, vendor, notes, recorded_by, tenant_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.VehicleID, rec.ScheduleID, rec.ServiceType, rec.PerformedAt.UTC().Format("2006-01-02 15:04:05"),
		rec.OdometerKM, rec.Cost, rec.Vendor, rec.Notes, rec.RecordedBy, tenantID,
	)
	if err != nil {
		return err
	}

	// Update schedule last_done markers if schedule_id or match found
	if rec.ScheduleID != nil && *rec.ScheduleID != "" {
		_, _ = tx.ExecContext(ctx, `
			UPDATE maintenance_schedules
			SET last_done_km = ?, last_done_at = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?`, rec.OdometerKM, rec.PerformedAt.UTC().Format("2006-01-02 15:04:05"), *rec.ScheduleID)
	} else {
		_, _ = tx.ExecContext(ctx, `
			UPDATE maintenance_schedules
			SET last_done_km = ?, last_done_at = ?, updated_at = CURRENT_TIMESTAMP
			WHERE vehicle_id = ? AND service_type = ? AND active = 1`,
			rec.OdometerKM, rec.PerformedAt.UTC().Format("2006-01-02 15:04:05"), rec.VehicleID, rec.ServiceType)
	}

	return tx.Commit()
}

// SaveSchedule creates or updates a maintenance schedule.
func (r *MaintenanceRepository) SaveSchedule(ctx context.Context, s domain.Schedule) error {
	var dueAtStr *string
	if s.DueAt != nil {
		str := s.DueAt.UTC().Format("2006-01-02 15:04:05")
		dueAtStr = &str
	}
	var lastDoneAtStr *string
	if s.LastDoneAt != nil {
		str := s.LastDoneAt.UTC().Format("2006-01-02 15:04:05")
		lastDoneAtStr = &str
	}

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO maintenance_schedules (id, vehicle_id, service_type, interval_km, interval_days, last_done_km, last_done_at, due_km, due_at, active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			service_type = excluded.service_type,
			interval_km = excluded.interval_km,
			interval_days = excluded.interval_days,
			last_done_km = excluded.last_done_km,
			last_done_at = excluded.last_done_at,
			due_km = excluded.due_km,
			due_at = excluded.due_at,
			active = excluded.active,
			updated_at = CURRENT_TIMESTAMP`,
		s.ID, s.VehicleID, s.ServiceType, s.IntervalKM, s.IntervalDays,
		s.LastDoneKM, lastDoneAtStr, s.DueKM, dueAtStr, s.Active,
	)
	return err
}

// OverrideMaintenance applies an admin override to a vehicle's maintenance block.
func (r *MaintenanceRepository) OverrideMaintenance(ctx context.Context, vehicleID, actorID, reason string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE vehicles
		SET maintenance_override_by = ?,
		    maintenance_override_at = CURRENT_TIMESTAMP,
		    maintenance_override_reason = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, actorID, reason, vehicleID)
	return err
}

// GetCriticalDtcCodes reads configured critical DTCs from company_settings or falls back to env/default.
func (r *MaintenanceRepository) GetCriticalDtcCodes(ctx context.Context, fallback string) []string {
	var setting sql.NullString
	_ = r.db.QueryRowContext(ctx, `
		SELECT maintenance_critical_dtcs FROM company_settings LIMIT 1`).Scan(&setting)
	val := fallback
	if setting.Valid && strings.TrimSpace(setting.String) != "" {
		val = setting.String
	}
	if strings.TrimSpace(val) == "" {
		val = "P0A0F,P1602"
	}
	rawParts := strings.Split(val, ",")
	var codes []string
	for _, p := range rawParts {
		trimmed := strings.ToUpper(strings.TrimSpace(p))
		if trimmed != "" {
			codes = append(codes, trimmed)
		}
	}
	return codes
}

// ListDueVehicles returns vehicles with an active maintenance due date.
func (r *MaintenanceRepository) ListDueVehicles(ctx context.Context, tenantID string) ([]map[string]interface{}, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT v.id, v.registration_number, v.vehicle_number, v.vehicle_type,
		       v.maintenance_due, v.maintenance_override_by, v.maintenance_override_at, v.maintenance_override_reason
		FROM vehicles v
		WHERE v.tenant_id = ? AND v.maintenance_due IS NOT NULL
		ORDER BY v.maintenance_due ASC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, reg, num, vtype, due sql.NullString
		var ovBy, ovReason sql.NullString
		var ovAt sql.NullTime
		if err := rows.Scan(&id, &reg, &num, &vtype, &due, &ovBy, &ovAt, &ovReason); err != nil {
			continue
		}
		list = append(list, map[string]interface{}{
			"ID":             id.String,
			"Registration":   reg.String,
			"VehicleNumber":  num.String,
			"VehicleType":    vtype.String,
			"DueDate":        due.String,
			"OverrideBy":     ovBy.String,
			"OverrideReason": ovReason.String,
			"IsOverridden":   ovBy.Valid && ovBy.String != "",
		})
	}
	return list, nil
}

// ListRecords returns completed maintenance records for a vehicle or fleetwide.
// tenantID scopes to one org ("" = all); records carry their own tenant.
func (r *MaintenanceRepository) ListRecords(ctx context.Context, vehicleID string, limit int) ([]domain.Record, error) {
	return r.listRecords(ctx, vehicleID, "", limit)
}

func (r *MaintenanceRepository) ListRecordsForTenant(ctx context.Context, vehicleID, tenantID string, limit int) ([]domain.Record, error) {
	return r.listRecords(ctx, vehicleID, tenantID, limit)
}

func (r *MaintenanceRepository) listRecords(ctx context.Context, vehicleID, tenantID string, limit int) ([]domain.Record, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT id, vehicle_id, schedule_id, service_type, performed_at, odometer_km, cost, vendor, notes, recorded_by, created_at
	          FROM maintenance_records`
	var args []interface{}
	clauses := []string{}
	if vehicleID != "" {
		clauses = append(clauses, "vehicle_id = ?")
		args = append(args, vehicleID)
	}
	if tenantID != "" {
		clauses = append(clauses, "tenant_id = ?")
		args = append(args, tenantID)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY performed_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.Record
	for rows.Next() {
		var rec domain.Record
		var schedID, vendor, notes, recBy sql.NullString
		var odo, cost sql.NullFloat64
		var perf time.Time
		if err := rows.Scan(&rec.ID, &rec.VehicleID, &schedID, &rec.ServiceType, &perf, &odo, &cost, &vendor, &notes, &recBy, &rec.CreatedAt); err != nil {
			continue
		}
		if schedID.Valid {
			rec.ScheduleID = &schedID.String
		}
		if vendor.Valid {
			rec.Vendor = &vendor.String
		}
		if notes.Valid {
			rec.Notes = &notes.String
		}
		if recBy.Valid {
			rec.RecordedBy = &recBy.String
		}
		if odo.Valid {
			rec.OdometerKM = &odo.Float64
		}
		if cost.Valid {
			rec.Cost = &cost.Float64
		}
		rec.PerformedAt = perf.UTC()
		list = append(list, rec)
	}
	return list, rows.Err()
}

// ListDtcEvents returns DTC events for a vehicle or fleetwide.
// tenantID scopes via the vehicle (DTC rows carry no tenant of their own).
func (r *MaintenanceRepository) ListDtcEvents(ctx context.Context, vehicleID string, limit int) ([]domain.DtcEvent, error) {
	return r.listDtcEvents(ctx, vehicleID, "", limit)
}

func (r *MaintenanceRepository) ListDtcEventsForTenant(ctx context.Context, vehicleID, tenantID string, limit int) ([]domain.DtcEvent, error) {
	return r.listDtcEvents(ctx, vehicleID, tenantID, limit)
}

func (r *MaintenanceRepository) listDtcEvents(ctx context.Context, vehicleID, tenantID string, limit int) ([]domain.DtcEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT d.id, d.vehicle_id, d.trip_id, d.dtc_code, d.severity, d.description, d.raw_payload, d.occurred_at, d.resolved_at, d.created_at
	          FROM dtc_events d`
	var args []interface{}
	clauses := []string{}
	if vehicleID != "" {
		clauses = append(clauses, "d.vehicle_id = ?")
		args = append(args, vehicleID)
	}
	if tenantID != "" {
		clauses = append(clauses, "d.vehicle_id IN (SELECT id FROM vehicles WHERE tenant_id = ?)")
		args = append(args, tenantID)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY d.occurred_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.DtcEvent
	for rows.Next() {
		var e domain.DtcEvent
		var tripID, desc, raw sql.NullString
		var occ time.Time
		var resAt sql.NullTime
		if err := rows.Scan(&e.ID, &e.VehicleID, &tripID, &e.DtcCode, &e.Severity, &desc, &raw, &occ, &resAt, &e.CreatedAt); err != nil {
			continue
		}
		if tripID.Valid {
			e.TripID = &tripID.String
		}
		if desc.Valid {
			e.Description = &desc.String
		}
		if raw.Valid {
			e.RawPayload = &raw.String
		}
		if resAt.Valid {
			t := resAt.Time.UTC()
			e.ResolvedAt = &t
		}
		e.OccurredAt = occ.UTC()
		list = append(list, e)
	}
	return list, rows.Err()
}

// ── Work orders (job cards) ─────────────────────────────────────────────
// All reads/writes are tenant-scoped; empty tenant matches nothing.

// CreateWorkOrder opens a job card in 'open' status.
func (r *MaintenanceRepository) CreateWorkOrder(ctx context.Context, w domain.WorkOrder) error {
	if w.TenantID == "" || w.VehicleID == "" || w.Title == "" {
		return errors.New("maintenance: tenant, vehicle and title are required")
	}
	if w.Status == "" {
		w.Status = domain.WorkOrderOpen
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO work_orders (id, tenant_id, vehicle_id, schedule_id, trip_id, title, description,
			assignee, vendor, cost_estimate, cost_actual, status, due_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.TenantID, w.VehicleID, nullStr(w.ScheduleID), nullStr(w.TripID),
		w.Title, w.Description, w.Assignee, w.Vendor,
		nullFloat(w.CostEstimate), nullFloat(w.CostActual), w.Status, nullTime(w.DueAt))
	return err
}

// FindWorkOrder returns one card of the org (nil, nil when absent/foreign).
func (r *MaintenanceRepository) FindWorkOrder(ctx context.Context, tenantID, id string) (*domain.WorkOrder, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, vehicle_id, schedule_id, trip_id, title, description,
			assignee, vendor, cost_estimate, cost_actual, status, due_at, closed_at,
			created_at, updated_at
		FROM work_orders WHERE id = ? AND tenant_id = ?`, id, tenantID)
	w, err := scanWorkOrder(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return w, nil
}

// ListWorkOrders returns the org's cards, optionally filtered by status.
func (r *MaintenanceRepository) ListWorkOrders(ctx context.Context, tenantID, status string, limit int) ([]domain.WorkOrder, error) {
	if tenantID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT id, tenant_id, vehicle_id, schedule_id, trip_id, title, description,
			assignee, vendor, cost_estimate, cost_actual, status, due_at, closed_at,
			created_at, updated_at
		FROM work_orders WHERE tenant_id = ?`
	args := []interface{}{tenantID}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.WorkOrder
	for rows.Next() {
		w, err := scanWorkOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// FindOpenWorkOrder returns the newest non-terminal card for the vehicle,
// optionally pinned to one schedule (nil, nil when the books are clear).
// The worker uses it to open exactly one job card per cause.
func (r *MaintenanceRepository) FindOpenWorkOrder(ctx context.Context, tenantID, vehicleID, scheduleID string) (*domain.WorkOrder, error) {
	if tenantID == "" || vehicleID == "" {
		return nil, nil
	}
	query := `SELECT id, tenant_id, vehicle_id, schedule_id, trip_id, title, description,
			assignee, vendor, cost_estimate, cost_actual, status, due_at, closed_at,
			created_at, updated_at
		FROM work_orders
		WHERE tenant_id = ? AND vehicle_id = ? AND status NOT IN ('done','cancelled')`
	args := []interface{}{tenantID, vehicleID}
	if scheduleID != "" {
		query += " AND schedule_id = ?"
		args = append(args, scheduleID)
	}
	query += " ORDER BY created_at DESC LIMIT 1"
	w, err := scanWorkOrder(r.db.QueryRowContext(ctx, query, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return w, nil
}

// TransitionWorkOrder moves a card along open → assigned → in_progress → done
// (on_hold loops back). Terminal cards (done/cancelled) are immutable;
// closing stamps closed_at. Cross-org transitions affect zero rows.
func (r *MaintenanceRepository) TransitionWorkOrder(ctx context.Context, tenantID, id, toStatus string) error {
	switch toStatus {
	case domain.WorkOrderOpen, domain.WorkOrderAssigned, domain.WorkOrderInProgress,
		domain.WorkOrderOnHold, domain.WorkOrderDone, domain.WorkOrderCancelled:
	default:
		return fmt.Errorf("maintenance: unknown work order status %q", toStatus)
	}
	var res sql.Result
	var err error
	if toStatus == domain.WorkOrderDone || toStatus == domain.WorkOrderCancelled {
		res, err = r.db.ExecContext(ctx, `
			UPDATE work_orders SET status = ?, closed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND tenant_id = ? AND status NOT IN ('done','cancelled')`,
			toStatus, id, tenantID)
	} else {
		res, err = r.db.ExecContext(ctx, `
			UPDATE work_orders SET status = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND tenant_id = ? AND status NOT IN ('done','cancelled')`,
			toStatus, id, tenantID)
	}
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("maintenance: work order not found or already terminal")
	}
	return nil
}

// AssignWorkOrder sets mechanic/vendor and moves open cards to assigned.
func (r *MaintenanceRepository) AssignWorkOrder(ctx context.Context, tenantID, id, assignee, vendor string) error {
	if tenantID == "" || id == "" {
		return errors.New("maintenance: tenant and work order id required")
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE work_orders SET assignee = ?, vendor = ?,
			status = CASE WHEN status = 'open' THEN 'assigned' ELSE status END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND tenant_id = ? AND status NOT IN ('done','cancelled')`,
		assignee, vendor, id, tenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("maintenance: work order not found or already terminal")
	}
	return nil
}

// CompleteWorkOrder closes a card as done AND writes the service record in
// one place, so HTTP, web and agent callers close the books identically.
// The record carries the linked schedule's service type (fallback general),
// the latest odometer (so interval schedules resolve), cost, vendor and the
// actor. Callers re-run resolution afterwards to clear the due flag.
func (r *MaintenanceRepository) CompleteWorkOrder(ctx context.Context, tenantID, id, actor string) (*domain.WorkOrder, error) {
	if tenantID == "" || id == "" {
		return nil, errors.New("maintenance: tenant and work order id required")
	}
	wo, err := r.FindWorkOrder(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if wo == nil {
		return nil, errors.New("maintenance: work order not found")
	}
	if wo.Terminal() {
		return nil, errors.New("maintenance: work order already terminal")
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE work_orders SET status = 'done', closed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND tenant_id = ? AND status NOT IN ('done','cancelled')`,
		id, tenantID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errors.New("maintenance: work order not found or already terminal")
	}
	done, err := r.FindWorkOrder(ctx, tenantID, id)
	if err != nil || done == nil {
		return nil, errors.New("maintenance: work order closed but not readable")
	}

	serviceType := "general"
	if done.ScheduleID != nil && *done.ScheduleID != "" {
		if schedules, err := r.ListSchedulesForTenant(ctx, done.VehicleID, tenantID); err == nil {
			for _, s := range schedules {
				if s.ID == *done.ScheduleID && s.ServiceType != "" {
					serviceType = s.ServiceType
					break
				}
			}
		}
	}
	now := time.Now().UTC()
	rec := domain.Record{
		ID:          uuid.NewString(),
		VehicleID:   done.VehicleID,
		ScheduleID:  done.ScheduleID,
		ServiceType: serviceType,
		PerformedAt: now,
	}
	if odo, err := r.GetLatestOdometer(ctx, done.VehicleID); err == nil && odo > 0 {
		rec.OdometerKM = &odo
	}
	if done.CostActual != nil {
		rec.Cost = done.CostActual
	} else if done.CostEstimate != nil {
		rec.Cost = done.CostEstimate
	}
	if done.Vendor != "" {
		rec.Vendor = &done.Vendor
	}
	notes := fmt.Sprintf("Closed job card: %s", done.Title)
	if done.Description != "" {
		notes += " — " + done.Description
	}
	rec.Notes = &notes
	if actor != "" {
		rec.RecordedBy = &actor
	}
	if err := r.InsertRecord(ctx, rec); err != nil {
		return nil, fmt.Errorf("maintenance: card closed but service record failed: %w", err)
	}
	return done, nil
}

type workOrderScanner interface {
	Scan(dest ...any) error
}

func scanWorkOrder(row workOrderScanner) (*domain.WorkOrder, error) {
	var w domain.WorkOrder
	var schedID, tripID sql.NullString
	var costEst, costAct sql.NullFloat64
	var dueAt, closedAt sql.NullTime
	var createdAt, updatedAt time.Time
	if err := row.Scan(&w.ID, &w.TenantID, &w.VehicleID, &schedID, &tripID,
		&w.Title, &w.Description, &w.Assignee, &w.Vendor, &costEst, &costAct,
		&w.Status, &dueAt, &closedAt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if schedID.Valid {
		w.ScheduleID = &schedID.String
	}
	if tripID.Valid {
		w.TripID = &tripID.String
	}
	if costEst.Valid {
		w.CostEstimate = &costEst.Float64
	}
	if costAct.Valid {
		w.CostActual = &costAct.Float64
	}
	if dueAt.Valid {
		w.DueAt = &dueAt.Time
	}
	if closedAt.Valid {
		w.ClosedAt = &closedAt.Time
	}
	w.CreatedAt = createdAt
	return &w, nil
}

func nullStr(s *string) sql.NullString {
	if s != nil && *s != "" {
		return sql.NullString{String: *s, Valid: true}
	}
	return sql.NullString{}
}

func nullFloat(f *float64) sql.NullFloat64 {
	if f != nil {
		return sql.NullFloat64{Float64: *f, Valid: true}
	}
	return sql.NullFloat64{}
}

func nullTime(t *time.Time) sql.NullTime {
	if t != nil && !t.IsZero() {
		return sql.NullTime{Time: *t, Valid: true}
	}
	return sql.NullTime{}
}
