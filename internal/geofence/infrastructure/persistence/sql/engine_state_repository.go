package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"transport-app/internal/geofence/domain"
	"transport-app/internal/repository"
)

// ErrNoEngineState is returned when no state row exists for a vehicle.
var ErrNoEngineState = errors.New("engine state not found")

// EngineStateRepository implements domain.EngineStateRepository.
type EngineStateRepository struct {
	db *sql.DB
}

// NewEngineStateRepository constructs an EngineStateRepository.
func NewEngineStateRepository(db *sql.DB) *EngineStateRepository {
	return &EngineStateRepository{db: db}
}

func (r *EngineStateRepository) dbFromContext(ctx context.Context) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return r.db
}

// GetByVehicle loads the dwell state row for a vehicle. Returns
// ErrNoEngineState when no row exists (vehicle not yet seen).
func (r *EngineStateRepository) GetByVehicle(ctx context.Context, tenantID, vehicleID string) (*domain.EngineState, error) {
	row := r.dbFromContext(ctx).QueryRowContext(ctx,
		`SELECT vehicle_id, tenant_id, state, trip_id, geofence_id, zone_kind,
		        zone_entered_at, confirmed_at, exit_started_at,
		        last_fix_at, last_lat, last_lng, updated_at
		 FROM engine_state WHERE vehicle_id = $1 AND tenant_id = $2`,
		vehicleID, tenantID)
	var s domain.EngineState
	var tripID, geofenceID, zoneKind sql.NullString
	var zoneEnteredAt, confirmedAt, exitStartedAt sql.NullTime
	var lastLat, lastLng sql.NullFloat64
	if err := row.Scan(
		&s.VehicleID, &s.TenantID, &s.State, &tripID, &geofenceID, &zoneKind,
		&zoneEnteredAt, &confirmedAt, &exitStartedAt,
		&s.LastFixAt, &lastLat, &lastLng, &s.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoEngineState
		}
		return nil, fmt.Errorf("engine state lookup: %w", err)
	}
	s.TripID = nullStringToPtr(tripID)
	s.GeofenceID = nullStringToPtr(geofenceID)
	s.ZoneKind = nullStringToPtr(zoneKind)
	if zoneEnteredAt.Valid {
		t := zoneEnteredAt.Time
		s.ZoneEnteredAt = &t
	}
	if confirmedAt.Valid {
		t := confirmedAt.Time
		s.ConfirmedAt = &t
	}
	if exitStartedAt.Valid {
		t := exitStartedAt.Time
		s.ExitStartedAt = &t
	}
	s.LastLat = lastLat.Float64
	s.LastLng = lastLng.Float64
	return &s, nil
}

// Upsert persists the dwell state machine row (insert or replace).
func (r *EngineStateRepository) Upsert(ctx context.Context, s domain.EngineState) error {
	db := r.dbFromContext(ctx)
	_, err := db.ExecContext(ctx,
		`INSERT INTO engine_state
		 (vehicle_id, tenant_id, state, trip_id, geofence_id, zone_kind,
		  zone_entered_at, confirmed_at, exit_started_at,
		  last_fix_at, last_lat, last_lng, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, CURRENT_TIMESTAMP)
		 ON CONFLICT (vehicle_id) DO UPDATE SET
		   state = excluded.state,
		   trip_id = excluded.trip_id,
		   geofence_id = excluded.geofence_id,
		   zone_kind = excluded.zone_kind,
		   zone_entered_at = excluded.zone_entered_at,
		   confirmed_at = excluded.confirmed_at,
		   exit_started_at = excluded.exit_started_at,
		   last_fix_at = excluded.last_fix_at,
		   last_lat = excluded.last_lat,
		   last_lng = excluded.last_lng,
		   updated_at = CURRENT_TIMESTAMP`,
		s.VehicleID, s.TenantID, s.State,
		ptrOrNil(s.TripID), ptrOrNil(s.GeofenceID), ptrOrNil(s.ZoneKind),
		timeOrNil(s.ZoneEnteredAt), timeOrNil(s.ConfirmedAt), timeOrNil(s.ExitStartedAt),
		s.LastFixAt, s.LastLat, s.LastLng)
	if err != nil {
		return fmt.Errorf("upsert engine state: %w", err)
	}
	return nil
}
