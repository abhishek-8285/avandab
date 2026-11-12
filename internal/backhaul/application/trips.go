package application

// Backhaul matching-page trip selector, extracted from internal/handlers/
// backhaul.go (handlers-end-state ADR, Phase 2). The matching engine stays in
// the untouched service.BackhaulService; this file keeps the trip list query.

import (
	"context"
	"database/sql"
)

// TripOption is one trip available for backhaul matching.
type TripOption struct {
	ID         string
	TripNumber string
	Status     string
}

// RecentTrips returns the tenant's 50 newest trips in the matching statuses.
// Best-effort like the legacy inline query: a query/scan failure leaves the
// selector empty instead of failing the page.
func RecentTrips(ctx context.Context, db *sql.DB, tenantID string) []TripOption {
	if db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, trip_number, status
		FROM trips
		WHERE tenant_id = $1 AND status IN ('scheduled', 'assigned', 'started', 'completed')
		ORDER BY created_at DESC
		LIMIT 50`, tenantID)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var out []TripOption
	for rows.Next() {
		var t TripOption
		if err := rows.Scan(&t.ID, &t.TripNumber, &t.Status); err == nil {
			out = append(out, t)
		}
	}
	return out
}
