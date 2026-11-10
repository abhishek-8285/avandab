package ewaybill

// Read model behind the E-Way Bill list/detail pages, extracted from
// internal/handlers/ewaybill.go (handlers-end-state ADR, Phase 2). The
// lifecycle/validation/provider logic stays in EWayBillService; this file
// only moves the four presentation queries.
//
// LEGACY SCOPE PRESERVED EXACTLY: these presentation reads are global -
// eway_bills has no tenant_id column and the former handler never joined
// trips.tenant_id, so the list/stats/candidate-trips/event-history views show
// rows from every tenant to any authenticated operator with ewaybill:read.
// This is frozen behavior for the migration (the handler characterization
// tests rely on the page shapes); it is flagged for a follow-up security
// increment rather than silently changed here. The EWB write lifecycle keeps
// its own tenant gate inside EWayBillService (TestGeneratePartA_CrossTenantDenied).

import (
	"context"
	"database/sql"
	"time"
)

// EWBStats is the aggregate strip shown on the E-Way Bill list page.
type EWBStats struct {
	Total     int
	Active    int
	PartAOnly int
	Extended  int
	Cancelled int
	Expired   int
}

// EWBListItem is one row of the E-Way Bill list/partial table.
type EWBListItem struct {
	ID             string
	TripID         string
	TripNumber     string
	EwbNumber      string
	VehicleNumber  string
	FromPlace      string
	ToPlace        string
	GoodsValue     float64
	Status         string
	ValidUntil     time.Time
	ExtensionCount int
	CreatedAt      time.Time
}

// TripOption is one candidate trip in the standalone EWB create modal.
type TripOption struct {
	ID          string
	TripNumber  string
	Source      string
	Destination string
}

// EWBEventRecord is one lifecycle event on the E-Way Bill detail page.
type EWBEventRecord struct {
	ID        string    `json:"id"`
	EwbNumber string    `json:"ewb_number"`
	TripID    string    `json:"trip_id"`
	EventType string    `json:"event_type"`
	Payload   string    `json:"payload"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// EWayBillReadQueries is the read-model facade for the E-Way Bill pages. It
// is stateless and cheap to construct per request.
type EWayBillReadQueries struct {
	db *sql.DB
}

// NewEWayBillReadQueries wires the read model against the database.
func NewEWayBillReadQueries(db *sql.DB) *EWayBillReadQueries {
	return &EWayBillReadQueries{db: db}
}

// Stats aggregates the list-page counters. The legacy handler logged (and
// rendered zeroed stats for) a failed query, so the error is returned for the
// caller to log without failing the page.
func (q *EWayBillReadQueries) Stats(ctx context.Context) (EWBStats, error) {
	var stats EWBStats
	if q.db == nil {
		return stats, nil
	}
	err := q.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN status='active' AND (vehicle_number IS NOT NULL AND vehicle_number != '') THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN status='active' AND (vehicle_number IS NULL OR vehicle_number = '') THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN extension_count > 0 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN status='cancelled' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN status='expired' THEN 1 ELSE 0 END), 0)
		FROM eway_bills
	`).Scan(&stats.Total, &stats.Active, &stats.PartAOnly, &stats.Extended, &stats.Cancelled, &stats.Expired)
	return stats, err
}

// Items returns the 100 newest EWB rows (legacy global ordering/limit). A
// query error renders an empty table instead of failing the page, and per-row
// scan failures are skipped - both preserved from the handler.
func (q *EWayBillReadQueries) Items(ctx context.Context) []EWBListItem {
	if q.db == nil {
		return nil
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT e.id, e.trip_id, COALESCE(t.trip_number, ''), e.ewb_number,
		       COALESCE(e.vehicle_number, ''), COALESCE(e.from_place, ''), COALESCE(e.to_place, ''),
		       e.goods_value, e.status, e.valid_until, e.extension_count, e.created_at
		FROM eway_bills e
		LEFT JOIN trips t ON e.trip_id = t.id
		ORDER BY e.created_at DESC LIMIT 100
	`)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var items []EWBListItem
	for rows.Next() {
		var it EWBListItem
		if err := rows.Scan(
			&it.ID, &it.TripID, &it.TripNumber, &it.EwbNumber,
			&it.VehicleNumber, &it.FromPlace, &it.ToPlace,
			&it.GoodsValue, &it.Status, &it.ValidUntil, &it.ExtensionCount, &it.CreatedAt,
		); err == nil {
			items = append(items, it)
		}
	}
	return items
}

// CandidateTrips returns the 50 newest trips eligible for manual EWB creation
// (legacy global scope: no tenant filter, preserved). Query errors render an
// empty dropdown.
func (q *EWayBillReadQueries) CandidateTrips(ctx context.Context) []TripOption {
	if q.db == nil {
		return nil
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT t.id, t.trip_number, r.source, r.destination
		FROM trips t
		JOIN routes r ON t.route_id = r.id
		WHERE t.status IN ('scheduled', 'assigned', 'started', 'reached_pickup', 'in_transit')
		ORDER BY t.created_at DESC LIMIT 50
	`)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var trips []TripOption
	for rows.Next() {
		var to TripOption
		if err := rows.Scan(&to.ID, &to.TripNumber, &to.Source, &to.Destination); err == nil {
			trips = append(trips, to)
		}
	}
	return trips
}

// Events returns the EWB lifecycle history in chronological order. Query
// errors render an empty history; payload/creator NULLs default like the
// legacy handler (COALESCE 'system' for the actor).
func (q *EWayBillReadQueries) Events(ctx context.Context, ewbNumber string) []EWBEventRecord {
	if q.db == nil {
		return nil
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, ewb_number, COALESCE(trip_id, ''), event_type, COALESCE(payload, ''), COALESCE(created_by, 'system'), created_at
		FROM eway_bill_events
		WHERE ewb_number = $1
		ORDER BY created_at ASC
	`, ewbNumber)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var events []EWBEventRecord
	for rows.Next() {
		var ev EWBEventRecord
		if err := rows.Scan(&ev.ID, &ev.EwbNumber, &ev.TripID, &ev.EventType, &ev.Payload, &ev.CreatedBy, &ev.CreatedAt); err == nil {
			events = append(events, ev)
		}
	}
	return events
}
