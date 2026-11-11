package ewaybill

// Read model behind the E-Way Bill list/detail pages, extracted from
// internal/handlers/ewaybill.go (handlers-end-state ADR, Phase 2). The
// lifecycle/validation/provider logic stays in EWayBillService; this file
// only owns the four presentation queries.
//
// TENANT SCOPING (security fix, 2026-11-11): these presentation reads are
// tenant-scoped through the owning trip (`trips.tenant_id`). eway_bills has no
// tenant_id column of its own, so every query joins its trip and filters on
// the caller's tenant. The previous behavior - a global list/stats/events
// view for any operator holding ewaybill:read - was a pre-existing
// cross-tenant data exposure and is intentionally NOT preserved.
//
// Fail-closed contract: an empty tenant id returns empty results (zeroed
// stats, nil lists) - there is no global fallback and no DefaultTenant
// substitution. Orphan EWBs whose trip was deleted/unknown are invisible to
// every tenant (inner join), and event history is only returned for an EWB
// whose trip belongs to the calling tenant.
//
// The EWB write lifecycle keeps its own tenant gate inside EWayBillService
// (TestGeneratePartA_CrossTenantDenied).

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

// Stats aggregates the tenant's list-page counters. A failed query is
// returned for the caller to log while the page renders zeroed stats; an
// empty tenant fails closed with zeroed stats.
func (q *EWayBillReadQueries) Stats(ctx context.Context, tenantID string) (EWBStats, error) {
	var stats EWBStats
	if q.db == nil || tenantID == "" {
		return stats, nil
	}
	err := q.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN eb.status='active' AND (eb.vehicle_number IS NOT NULL AND eb.vehicle_number != '') THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN eb.status='active' AND (eb.vehicle_number IS NULL OR eb.vehicle_number = '') THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN eb.extension_count > 0 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN eb.status='cancelled' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN eb.status='expired' THEN 1 ELSE 0 END), 0)
		FROM eway_bills eb
		JOIN trips t ON t.id = eb.trip_id AND t.tenant_id = $1
	`, tenantID).Scan(&stats.Total, &stats.Active, &stats.PartAOnly, &stats.Extended, &stats.Cancelled, &stats.Expired)
	return stats, err
}

// Items returns the tenant's 100 newest EWB rows (orphan EWBs without a
// tenant-owned trip are excluded by the inner join). A query error renders an
// empty table instead of failing the page, and per-row scan failures are
// skipped; an empty tenant fails closed with no rows.
func (q *EWayBillReadQueries) Items(ctx context.Context, tenantID string) []EWBListItem {
	if q.db == nil || tenantID == "" {
		return nil
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT e.id, e.trip_id, COALESCE(t.trip_number, ''), e.ewb_number,
		       COALESCE(e.vehicle_number, ''), COALESCE(e.from_place, ''), COALESCE(e.to_place, ''),
		       e.goods_value, e.status, e.valid_until, e.extension_count, e.created_at
		FROM eway_bills e
		JOIN trips t ON t.id = e.trip_id AND t.tenant_id = $1
		ORDER BY e.created_at DESC LIMIT 100
	`, tenantID)
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

// CandidateTrips returns the tenant's 50 newest trips eligible for manual EWB
// creation. Query errors render an empty dropdown; an empty tenant fails
// closed with no trips.
func (q *EWayBillReadQueries) CandidateTrips(ctx context.Context, tenantID string) []TripOption {
	if q.db == nil || tenantID == "" {
		return nil
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT t.id, t.trip_number, r.source, r.destination
		FROM trips t
		JOIN routes r ON t.route_id = r.id
		WHERE t.tenant_id = $1 AND t.status IN ('scheduled', 'assigned', 'started', 'reached_pickup', 'in_transit')
		ORDER BY t.created_at DESC LIMIT 50
	`, tenantID)
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

// Events returns the tenant's EWB lifecycle history in chronological order.
// The event table has no tenant column, so ownership is proven through
// eway_bills -> trips.tenant_id; events for an EWB outside the caller's
// tenant (or whose trip is unknown) are invisible. Query errors render an
// empty history; payload/creator NULLs default like the legacy handler.
func (q *EWayBillReadQueries) Events(ctx context.Context, ewbNumber, tenantID string) []EWBEventRecord {
	if q.db == nil || tenantID == "" {
		return nil
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT ev.id, ev.ewb_number, COALESCE(ev.trip_id, ''), ev.event_type, COALESCE(ev.payload, ''), COALESCE(ev.created_by, 'system'), ev.created_at
		FROM eway_bill_events ev
		JOIN eway_bills eb ON eb.ewb_number = ev.ewb_number
		JOIN trips t ON t.id = eb.trip_id
		WHERE ev.ewb_number = $1 AND t.tenant_id = $2
		ORDER BY ev.created_at ASC
	`, ewbNumber, tenantID)
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
