package application

// Bookings kanban board read model, extracted from internal/handlers/
// bookings.go (handlers-end-state ADR, Phase 2; Spec 22 §2.4). The handler
// keeps lane mapping, JSON/HTML rendering and the preview UX; this file keeps
// the tenant-scoped board query.

import (
	"context"
	"database/sql"
)

// BoardRow is one booking row joined with its active trip/customer details.
type BoardRow struct {
	ID       string
	Number   string
	Customer string
	Status   string
	Freight  float64
	Pickup   sql.NullTime
	Vehicle  string
	Driver   string
}

// BoardQueries reads the kanban board.
type BoardQueries struct {
	db *sql.DB
}

// NewBoardQueries wires the board read model against the database.
func NewBoardQueries(db *sql.DB) *BoardQueries {
	return &BoardQueries{db: db}
}

// Rows returns the tenant's 200 newest bookings with their active trip's
// vehicle/driver display fields. Query/scan/rows errors surface to the
// handler (legacy httpx.Error 500 path).
func (q *BoardQueries) Rows(ctx context.Context, tenantID string) ([]BoardRow, error) {
	if q == nil || q.db == nil {
		return nil, nil
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT b.id, b.booking_number, COALESCE(c.name,''), b.status,
		       COALESCE(b.price,0), b.pickup_date,
		       COALESCE(v.vehicle_number,''),
		       COALESCE(d.first_name || ' ' || d.last_name, '')
		FROM bookings b
		LEFT JOIN customers c ON c.id = b.customer_id
		LEFT JOIN trips t ON t.booking_id = b.id
		     AND t.status IN ('assigned','started','reached_pickup','in_transit')
		LEFT JOIN vehicles v ON v.id = t.vehicle_id
		LEFT JOIN drivers d ON d.id = t.driver_id
		WHERE b.tenant_id = $1
		ORDER BY b.created_at DESC LIMIT 200`, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []BoardRow
	for rows.Next() {
		var r BoardRow
		if err := rows.Scan(&r.ID, &r.Number, &r.Customer, &r.Status, &r.Freight, &r.Pickup, &r.Vehicle, &r.Driver); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
