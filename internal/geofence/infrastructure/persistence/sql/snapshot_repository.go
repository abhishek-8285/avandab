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
		   -- An unbound frame stores vehicle_id NULL (FK columns never take the
		   -- '' sentinel). The dwell engine is keyed by vehicle — engine_state,
		   -- zone config and the tenant lookup all need one — so such a row is
		   -- not a fix. Selecting it aborted the whole sweep on the NULL→string
		   -- Scan error, every tick, forever.
		   AND s.vehicle_id IS NOT NULL AND s.vehicle_id != ''
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
		// '' is the unattributed sentinel every reader filters out
		// (IS NOT NULL AND != ''). Surface it as nil, or the dwell worker's
		// tripID == nil gate never holds and pickup/drop zones run against
		// an empty trip id.
		if tripID.Valid && tripID.String != "" {
			f.TripID = &tripID.String
		}
		fixes = append(fixes, f)
	}
	return fixes, rows.Err()
}
