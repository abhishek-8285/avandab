package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"transport-app/internal/geofence/domain"
	"transport-app/internal/repository"
)

// EventLogRepository implements domain.EventLogRepository.
type EventLogRepository struct {
	db *sql.DB
}

// NewEventLogRepository constructs an EventLogRepository.
func NewEventLogRepository(db *sql.DB) *EventLogRepository {
	return &EventLogRepository{db: db}
}

func (r *EventLogRepository) dbFromContext(ctx context.Context) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return r.db
}

// InsertEvent logs a zone transition or breach alert.
func (r *EventLogRepository) InsertEvent(ctx context.Context, e domain.GeofenceEvent) error {
	db := r.dbFromContext(ctx)
	_, err := db.ExecContext(ctx,
		`INSERT INTO geofence_events
		 (id, tenant_id, vehicle_id, trip_id, geofence_id, zone_kind,
		  event_type, alert_type, severity, latitude, longitude, details, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		e.ID, e.TenantID, ptrOrNil(e.VehicleID), ptrOrNil(e.TripID), ptrOrNil(e.GeofenceID),
		ptrOrNil(e.ZoneKind), e.EventType, ptrOrNil(e.AlertType), ptrOrNil(e.Severity),
		nullFloatPtr(e.Latitude), nullFloatPtr(e.Longitude), ptrOrNil(e.Details), e.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert geofence event: %w", err)
	}
	return nil
}

// OpenDetention starts a pickup/drop dwell window (status=open).
func (r *EventLogRepository) OpenDetention(ctx context.Context, d domain.Detention) error {
	db := r.dbFromContext(ctx)
	_, err := db.ExecContext(ctx,
		`INSERT INTO trip_detentions
		 (id, tenant_id, trip_id, vehicle_id, geofence_id, zone_kind,
		  entered_at, dwell_seconds, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 0, $8)`,
		d.ID, d.TenantID, d.TripID, ptrOrNil(d.VehicleID), ptrOrNil(d.GeofenceID),
		d.ZoneKind, d.EnteredAt, domain.DetentionOpen)
	if err != nil {
		return fmt.Errorf("open detention: %w", err)
	}
	return nil
}

// FindOpenDetention returns the open detention window for a trip/zone kind.
func (r *EventLogRepository) FindOpenDetention(ctx context.Context, tenantID, tripID, zoneKind string) (*domain.Detention, error) {
	row := r.dbFromContext(ctx).QueryRowContext(ctx,
		`SELECT id, tenant_id, trip_id, vehicle_id, geofence_id, zone_kind,
		        entered_at, exited_at, dwell_seconds, status
		 FROM trip_detentions
		 WHERE tenant_id = $1 AND trip_id = $2 AND zone_kind = $3 AND status = $4
		 ORDER BY entered_at DESC LIMIT 1`,
		tenantID, tripID, zoneKind, domain.DetentionOpen)
	var d domain.Detention
	var vehicleID, geofenceID sql.NullString
	var exited sql.NullTime
	if err := row.Scan(
		&d.ID, &d.TenantID, &d.TripID, &vehicleID, &geofenceID, &d.ZoneKind,
		&d.EnteredAt, &exited, &d.DwellSeconds, &d.Status,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find open detention: %w", err)
	}
	d.VehicleID = nullStringToPtr(vehicleID)
	d.GeofenceID = nullStringToPtr(geofenceID)
	if exited.Valid {
		t := exited.Time
		d.ExitedAt = &t
	}
	return &d, nil
}

// CloseDetention closes an open dwell window and records the dwell duration,
// free window, billable seconds, rate and amount (Spec 02 §5/§6).
func (r *EventLogRepository) CloseDetention(ctx context.Context, id string, exitedAt time.Time, dwellSeconds, freeSeconds int64, ratePerHour, amount float64) error {
	db := r.dbFromContext(ctx)
	billable := dwellSeconds - freeSeconds
	if billable < 0 {
		billable = 0
	}
	_, err := db.ExecContext(ctx,
		`UPDATE trip_detentions
		 SET status = $1, exited_at = $2, dwell_seconds = $3, free_seconds = $4,
		     billable_seconds = $5, rate_per_hour = $6, amount = $7, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $8 AND status = $9`,
		domain.DetentionClosed, exitedAt, dwellSeconds, freeSeconds,
		billable, ratePerHour, amount, id, domain.DetentionOpen)
	if err != nil {
		return fmt.Errorf("close detention: %w", err)
	}
	return nil
}

// Find returns a detention by ID (tenant-scoped).
func (r *EventLogRepository) Find(ctx context.Context, tenantID, id string) (*domain.Detention, error) {
	row := r.dbFromContext(ctx).QueryRowContext(ctx,
		`SELECT d.id, d.tenant_id, d.trip_id, d.vehicle_id, d.geofence_id, d.zone_kind,
		        COALESCE(g.name, ''), d.entered_at, d.exited_at, d.dwell_seconds,
		        d.free_seconds, d.billable_seconds, d.rate_per_hour, d.amount, d.status
		 FROM trip_detentions d
		 LEFT JOIN geofences g ON g.id = d.geofence_id
		 WHERE d.id = $1 AND d.tenant_id = $2`,
		id, tenantID)
	d, err := scanDetention(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("find detention: %w", err)
	}
	return d, nil
}

// ListClosedForTrip returns closed, billable detentions that are not yet
// attached to an invoice (ref_id absent from invoice_line_items).
func (r *EventLogRepository) ListClosedForTrip(ctx context.Context, tenantID, tripID string) ([]domain.Detention, error) {
	rows, err := r.dbFromContext(ctx).QueryContext(ctx,
		`SELECT d.id, d.tenant_id, d.trip_id, d.vehicle_id, d.geofence_id, d.zone_kind,
		        COALESCE(g.name, ''), d.entered_at, d.exited_at, d.dwell_seconds,
		        d.free_seconds, d.billable_seconds, d.rate_per_hour, d.amount, d.status
		 FROM trip_detentions d
		 LEFT JOIN geofences g ON g.id = d.geofence_id
		 WHERE d.tenant_id = $1 AND d.trip_id = $2 AND d.status = $3
		   AND d.amount > 0
		   AND NOT EXISTS (
		     SELECT 1 FROM invoice_line_items ili
		     WHERE ili.ref_id = d.id AND ili.invoice_id IS NOT NULL
		   )
		 ORDER BY d.entered_at ASC`,
		tenantID, tripID, domain.DetentionClosed)
	if err != nil {
		return nil, fmt.Errorf("list closed detentions: %w", err)
	}
	defer rows.Close()
	var out []domain.Detention
	for rows.Next() {
		d, err := scanDetention(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// MarkAttached moves a detention to attached (invoiced line exists).
func (r *EventLogRepository) MarkAttached(ctx context.Context, id string) error {
	db := r.dbFromContext(ctx)
	_, err := db.ExecContext(ctx,
		`UPDATE trip_detentions SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`,
		domain.DetentionAttached, id)
	if err != nil {
		return fmt.Errorf("mark detention attached: %w", err)
	}
	return nil
}

// Waive zeroes the amount and marks the detention waived.
func (r *EventLogRepository) Waive(ctx context.Context, id string) error {
	db := r.dbFromContext(ctx)
	_, err := db.ExecContext(ctx,
		`UPDATE trip_detentions SET status = $1, amount = 0, updated_at = CURRENT_TIMESTAMP WHERE id = $2`,
		domain.DetentionWaived, id)
	if err != nil {
		return fmt.Errorf("waive detention: %w", err)
	}
	return nil
}

// scanRow is the minimal query-row interface shared by scanDetention callers.
type scanRow interface {
	Scan(dest ...any) error
}

// scanDetention maps a trip_detentions row (joined with geofences.name).
func scanDetention(row scanRow) (*domain.Detention, error) {
	var d domain.Detention
	var vehicleID, geofenceID sql.NullString
	var exited sql.NullTime
	if err := row.Scan(
		&d.ID, &d.TenantID, &d.TripID, &vehicleID, &geofenceID, &d.ZoneKind,
		&d.ZoneName, &d.EnteredAt, &exited, &d.DwellSeconds,
		&d.FreeSeconds, &d.BillableSeconds, &d.RatePerHour, &d.Amount, &d.Status,
	); err != nil {
		return nil, err
	}
	d.VehicleID = nullStringToPtr(vehicleID)
	d.GeofenceID = nullStringToPtr(geofenceID)
	if exited.Valid {
		t := exited.Time
		d.ExitedAt = &t
	}
	return &d, nil
}

// ptrOrNil converts a *string to a nullable SQL parameter.
func ptrOrNil(p *string) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

// timeOrNil converts a *time.Time to a nullable SQL parameter.
func timeOrNil(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return *t
}
