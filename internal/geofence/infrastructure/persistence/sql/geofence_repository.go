// Package sql provides raw-SQL persistence for the geofence engine (Spec 02).
// All methods honour repository.TxFromContext so multi-write operations
// participate in the caller's UnitOfWork transaction (no 1E deadlock trap).
package sql

import (
	"context"
	"database/sql"
	"fmt"

	"transport-app/internal/geofence/domain"
	"transport-app/internal/repository"
)

// GeofenceRepository implements domain.GeofenceRepository.
type GeofenceRepository struct {
	db *sql.DB
}

// NewGeofenceRepository constructs a GeofenceRepository.
func NewGeofenceRepository(db *sql.DB) *GeofenceRepository {
	return &GeofenceRepository{db: db}
}

func (r *GeofenceRepository) dbFromContext(ctx context.Context) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return r.db
}

const geofenceCols = `id, tenant_id, name, kind, shape, center_lat, center_lng,
	radius_m, polygon, route_name, priority, is_active, created_by, created_at, updated_at`

// ListActiveByTenant returns all active geofences for a tenant, highest
// priority first.
func (r *GeofenceRepository) ListActiveByTenant(ctx context.Context, tenantID string) ([]domain.Geofence, error) {
	rows, err := r.dbFromContext(ctx).QueryContext(ctx,
		`SELECT `+geofenceCols+`
		 FROM geofences
		 WHERE tenant_id = ? AND is_active = 1
		 ORDER BY priority DESC, name`,
		tenantID)
	if err != nil {
		return nil, fmt.Errorf("list active geofences: %w", err)
	}
	defer rows.Close()
	return scanGeofences(rows)
}

// ListBoundForVehicle returns active geofences explicitly bound to a vehicle
// via vehicle_geofences.
func (r *GeofenceRepository) ListBoundForVehicle(ctx context.Context, tenantID, vehicleID string) ([]domain.Geofence, error) {
	rows, err := r.dbFromContext(ctx).QueryContext(ctx,
		`SELECT g.id, g.tenant_id, g.name, g.kind, g.shape, g.center_lat, g.center_lng,
		        g.radius_m, g.polygon, g.route_name, g.priority, g.is_active,
		        g.created_by, g.created_at, g.updated_at
		 FROM geofences g
		 JOIN vehicle_geofences vg ON vg.geofence_id = g.id
		 WHERE vg.vehicle_id = $1 AND g.tenant_id = $2 AND g.is_active = 1
		 ORDER BY g.priority DESC, g.name`,
		vehicleID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list bound geofences: %w", err)
	}
	defer rows.Close()
	return scanGeofences(rows)
}

// Insert persists a geofence definition (used by seeding/tests; UI create is
// Sub-task 1G).
func (r *GeofenceRepository) Insert(ctx context.Context, g domain.Geofence) error {
	db := r.dbFromContext(ctx)
	polygon := ""
	if len(g.Polygon) > 0 {
		raw, err := domain.PolygonJSON(g.Polygon)
		if err != nil {
			return err
		}
		polygon = raw
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO geofences
		 (id, tenant_id, name, kind, shape, center_lat, center_lng, radius_m,
		  polygon, route_name, priority, is_active, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		g.ID, g.TenantID, g.Name, g.Kind, g.Shape,
		nullIfZero(g.CenterLat), nullIfZero(g.CenterLng), nullIfZero(g.RadiusM),
		polygonOrNull(polygon), g.RouteName, g.Priority, boolToInt(g.IsActive), g.CreatedBy)
	if err != nil {
		return fmt.Errorf("insert geofence: %w", err)
	}
	return nil
}

// Create persists a new geofence definition (Spec 02 §8 CRUD).
func (r *GeofenceRepository) Create(ctx context.Context, g domain.Geofence) error {
	return r.Insert(ctx, g)
}

// Update rewrites an existing geofence definition (Spec 02 §8 CRUD).
func (r *GeofenceRepository) Update(ctx context.Context, g domain.Geofence) error {
	db := r.dbFromContext(ctx)
	polygon := ""
	if len(g.Polygon) > 0 {
		raw, err := domain.PolygonJSON(g.Polygon)
		if err != nil {
			return err
		}
		polygon = raw
	}
	_, err := db.ExecContext(ctx,
		`UPDATE geofences
		 SET name = $1, kind = $2, shape = $3, center_lat = $4, center_lng = $5,
		     radius_m = $6, polygon = $7, route_name = $8, priority = $9,
		     is_active = $10, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $11 AND tenant_id = $12`,
		g.Name, g.Kind, g.Shape,
		nullIfZero(g.CenterLat), nullIfZero(g.CenterLng), nullIfZero(g.RadiusM),
		polygonOrNull(polygon), g.RouteName, g.Priority,
		boolToInt(g.IsActive), g.ID, g.TenantID)
	if err != nil {
		return fmt.Errorf("update geofence: %w", err)
	}
	return nil
}

// SoftDelete deactivates a geofence (is_active=0), stopping worker evaluation.
func (r *GeofenceRepository) SoftDelete(ctx context.Context, tenantID, id string) error {
	db := r.dbFromContext(ctx)
	_, err := db.ExecContext(ctx,
		`UPDATE geofences SET is_active = 0, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1 AND tenant_id = $2`,
		id, tenantID)
	if err != nil {
		return fmt.Errorf("soft delete geofence: %w", err)
	}
	return nil
}

// ListAll returns all geofences (active and inactive) for the admin list.
func (r *GeofenceRepository) ListAll(ctx context.Context, tenantID string) ([]domain.Geofence, error) {
	rows, err := r.dbFromContext(ctx).QueryContext(ctx,
		`SELECT `+geofenceCols+`
		 FROM geofences
		 WHERE tenant_id = ?
		 ORDER BY is_active DESC, priority DESC, name`,
		tenantID)
	if err != nil {
		return nil, fmt.Errorf("list all geofences: %w", err)
	}
	defer rows.Close()
	return scanGeofences(rows)
}

// Find returns one geofence by ID (tenant-scoped).
func (r *GeofenceRepository) Find(ctx context.Context, tenantID, id string) (*domain.Geofence, error) {
	row := r.dbFromContext(ctx).QueryRowContext(ctx,
		`SELECT `+geofenceCols+`
		 FROM geofences
		 WHERE id = ? AND tenant_id = ?`,
		id, tenantID)
	var g domain.Geofence
	var centerLat, centerLng, radius sql.NullFloat64
	var polygon, routeName, createdBy sql.NullString
	var isActive int
	if err := row.Scan(
		&g.ID, &g.TenantID, &g.Name, &g.Kind, &g.Shape,
		&centerLat, &centerLng, &radius, &polygon, &routeName,
		&g.Priority, &isActive, &createdBy, &g.CreatedAt, &g.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("find geofence: %w", err)
	}
	g.CenterLat = centerLat.Float64
	g.CenterLng = centerLng.Float64
	g.RadiusM = radius.Float64
	g.RouteName = routeName.String
	if createdBy.Valid {
		g.CreatedBy = &createdBy.String
	}
	g.IsActive = isActive == 1
	if polygon.Valid && polygon.String != "" {
		pts, err := domain.PolygonFromJSON(polygon.String)
		if err != nil {
			return nil, fmt.Errorf("geofence %s: %w", g.ID, err)
		}
		g.Polygon = pts
	}
	return &g, nil
}

func scanGeofences(rows *sql.Rows) ([]domain.Geofence, error) {
	var out []domain.Geofence
	for rows.Next() {
		var g domain.Geofence
		var centerLat, centerLng, radius sql.NullFloat64
		var polygon, routeName, createdBy sql.NullString
		var isActive int
		if err := rows.Scan(
			&g.ID, &g.TenantID, &g.Name, &g.Kind, &g.Shape,
			&centerLat, &centerLng, &radius, &polygon, &routeName,
			&g.Priority, &isActive, &createdBy, &g.CreatedAt, &g.UpdatedAt,
		); err != nil {
			return nil, err
		}
		g.CenterLat = centerLat.Float64
		g.CenterLng = centerLng.Float64
		g.RadiusM = radius.Float64
		g.RouteName = routeName.String
		if createdBy.Valid {
			g.CreatedBy = &createdBy.String
		}
		g.IsActive = isActive == 1
		if polygon.Valid && polygon.String != "" {
			pts, err := domain.PolygonFromJSON(polygon.String)
			if err != nil {
				return nil, fmt.Errorf("geofence %s: %w", g.ID, err)
			}
			g.Polygon = pts
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
