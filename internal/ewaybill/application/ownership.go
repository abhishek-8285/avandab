package application

// Tenant ownership lookups extracted from internal/handlers/ewaybill.go so the
// handlers package carries no raw SQL. eway_bills has no tenant column, so EWB
// ownership resolves through the owning trip. Empty ids fail closed.

import (
	"context"
	"database/sql"
)

// TenantOwnershipQueries answers trip/EWB ownership questions for a tenant.
// Stateless and cheap to construct per request.
type TenantOwnershipQueries struct {
	db *sql.DB
}

// NewTenantOwnershipQueries wires the ownership lookups against the database.
func NewTenantOwnershipQueries(db *sql.DB) *TenantOwnershipQueries {
	return &TenantOwnershipQueries{db: db}
}

// TripOwned reports whether tripID belongs to tenantID.
func (q *TenantOwnershipQueries) TripOwned(ctx context.Context, tripID, tenantID string) (bool, error) {
	if q.db == nil || tripID == "" || tenantID == "" {
		return false, nil
	}
	var n int
	if err := q.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM trips WHERE id = $1 AND tenant_id = $2`, tripID, tenantID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// EWBOwned resolves EWB ownership through its trip.
func (q *TenantOwnershipQueries) EWBOwned(ctx context.Context, ewbNumber, tenantID string) (bool, error) {
	if q.db == nil || ewbNumber == "" || tenantID == "" {
		return false, nil
	}
	var n int
	if err := q.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM eway_bills e
		JOIN trips t ON t.id = e.trip_id
		WHERE e.ewb_number = $1 AND t.tenant_id = $2`, ewbNumber, tenantID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}
