package sql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

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
	query := `SELECT id, vehicle_id, service_type, interval_km, interval_days,
	                 last_done_km, last_done_at, due_km, due_at, active, created_at, updated_at
	          FROM maintenance_schedules
	          WHERE active = 1`
	var args []interface{}
	if vehicleID != "" {
		query += " AND vehicle_id = ?"
		args = append(args, vehicleID)
	}
	query += " ORDER BY created_at DESC"

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
	query := `SELECT id, vehicle_id, service_type, interval_km, interval_days,
	                 last_done_km, last_done_at, due_km, due_at, active, created_at, updated_at
	          FROM maintenance_schedules`
	var args []interface{}
	if vehicleID != "" {
		query += " WHERE vehicle_id = ?"
		args = append(args, vehicleID)
	}
	query += " ORDER BY created_at DESC"

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
		if err := rows.Scan(
			&s.ID, &s.VehicleID, &s.ServiceType, &intKM, &intDays,
			&lastKM, &lastDoneAt, &dueKM, &dueAt, &s.Active, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
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
		}
		if dueKM.Valid {
			s.DueKM = &dueKM.Float64
		}
		if dueAt.Valid {
			t := dueAt.Time.UTC()
			s.DueAt = &t
		}
		list = append(list, s)
	}
	return list, rows.Err()
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
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO maintenance_records (id, vehicle_id, schedule_id, service_type, performed_at, odometer_km, cost, vendor, notes, recorded_by, tenant_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.VehicleID, rec.ScheduleID, rec.ServiceType, rec.PerformedAt.UTC().Format("2006-01-02 15:04:05"),
		rec.OdometerKM, rec.Cost, rec.Vendor, rec.Notes, rec.RecordedBy, tenantFromCtx(ctx),
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
func (r *MaintenanceRepository) ListRecords(ctx context.Context, vehicleID string, limit int) ([]domain.Record, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT id, vehicle_id, schedule_id, service_type, performed_at, odometer_km, cost, vendor, notes, recorded_by, created_at
	          FROM maintenance_records`
	var args []interface{}
	if vehicleID != "" {
		query += " WHERE vehicle_id = ?"
		args = append(args, vehicleID)
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
func (r *MaintenanceRepository) ListDtcEvents(ctx context.Context, vehicleID string, limit int) ([]domain.DtcEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT id, vehicle_id, trip_id, dtc_code, severity, description, raw_payload, occurred_at, resolved_at, created_at
	          FROM dtc_events`
	var args []interface{}
	if vehicleID != "" {
		query += " WHERE vehicle_id = ?"
		args = append(args, vehicleID)
	}
	query += " ORDER BY occurred_at DESC LIMIT ?"
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

// tenantFromCtx derives the acting tenant from the request context,
// defaulting to the bootstrap tenant for worker-initiated writes.
func tenantFromCtx(ctx context.Context) string {
	if id := shared.TenantIDFromContext(ctx); id != "" {
		return string(id)
	}
	return string(shared.DefaultTenant)
}
