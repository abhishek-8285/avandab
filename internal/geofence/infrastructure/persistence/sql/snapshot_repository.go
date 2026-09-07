package sql

import (
	"context"
	"database/sql"
	"fmt"

	"transport-app/internal/geofence/domain"
	"transport-app/internal/repository"
)

// SnapshotRepository implements domain.FixRepository over
// telemetry_snapshots, using engine_state.last_fix_at as the per-vehicle
// watermark (Spec 02 §4).
type SnapshotRepository struct {
	db *sql.DB
}

// NewSnapshotRepository constructs a SnapshotRepository.
func NewSnapshotRepository(db *sql.DB) *SnapshotRepository {
	return &SnapshotRepository{db: db}
}

func (r *SnapshotRepository) dbFromContext(ctx context.Context) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return r.db
}

// LoadNewFixes returns fixes newer than each vehicle's consumed watermark,
// oldest first, limited to `limit` rows. Vehicles with no engine_state row
// return all their snapshots.
func (r *SnapshotRepository) LoadNewFixes(ctx context.Context, limit int) ([]domain.Fix, error) {
	rows, err := r.dbFromContext(ctx).QueryContext(ctx,
		`SELECT s.vehicle_id, s.trip_id, s.timestamp, s.latitude, s.longitude, s.speed
		 FROM telemetry_snapshots s
		 LEFT JOIN engine_state e ON e.vehicle_id = s.vehicle_id
		 WHERE (e.last_fix_at IS NULL OR s.timestamp > e.last_fix_at)
		   AND s.latitude IS NOT NULL AND s.longitude IS NOT NULL
		 ORDER BY s.timestamp ASC
		 LIMIT $1`,
		limit)
	if err != nil {
		return nil, fmt.Errorf("load new fixes: %w", err)
	}
	defer rows.Close()

	var fixes []domain.Fix
	for rows.Next() {
		var f domain.Fix
		var tripID sql.NullString
		if err := rows.Scan(&f.VehicleID, &tripID, &f.Timestamp, &f.Latitude, &f.Longitude, &f.Speed); err != nil {
			return nil, err
		}
		if tripID.Valid {
			f.TripID = &tripID.String
		}
		fixes = append(fixes, f)
	}
	return fixes, rows.Err()
}
