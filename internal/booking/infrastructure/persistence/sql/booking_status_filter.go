package sql

import (
	"context"

	bookingdomain "transport-app/internal/booking/domain"
	"transport-app/internal/shared"
)

// StatusFilterUnassigned is the bookings list pseudo-status for
// confirmed/pending bookings with no trip yet. The dashboard exception
// strip links here (e.g. /bookings?status=unassigned).
//
// Raw SQL keeps the sqlc query set and existing mocks untouched (same
// coexistence pattern as the daterange search variants). Column shape
// mirrors bookingSearchSelect so scanBookingReadModels stays the single
// scanner.
const StatusFilterUnassigned = "unassigned"

const unassignedBookingSelect = `
SELECT b.id, b.booking_number, b.customer_id, b.pickup_date, b.route_id, b.vehicle_type,
    b.passengers, b.cargo_weight, b.price, b.notes, b.status, b.created_at, b.updated_at,
    c.name AS customer_name, c.company AS customer_company, r.source AS route_source, r.destination AS route_destination
FROM bookings b
JOIN customers c ON b.customer_id = c.id
JOIN routes r ON b.route_id = r.id
WHERE b.tenant_id = ?
  AND (b.booking_number LIKE '%' || ? || '%' OR c.name LIKE '%' || ? || '%' OR c.company LIKE '%' || ? || '%')
  AND b.status IN ('pending', 'confirmed')
  AND NOT EXISTS (SELECT 1 FROM trips t WHERE t.booking_id = b.id)`

const unassignedBookingCount = `
SELECT COUNT(*)
FROM bookings b
JOIN customers c ON b.customer_id = c.id
JOIN routes r ON b.route_id = r.id
WHERE b.tenant_id = ?
  AND (b.booking_number LIKE '%' || ? || '%' OR c.name LIKE '%' || ? || '%' OR c.company LIKE '%' || ? || '%')
  AND b.status IN ('pending', 'confirmed')
  AND NOT EXISTS (SELECT 1 FROM trips t WHERE t.booking_id = b.id)`

// searchUnassignedBookings lists bookings awaiting dispatch, optionally
// bounded by a pickup-date window (from/to empty = unbounded).
func (r *bookingRepository) searchUnassignedBookings(ctx context.Context, tenantID shared.TenantID, query, from, to string, limit, offset int) ([]bookingdomain.BookingReadModel, int64, error) {
	selectSQL := unassignedBookingSelect
	countSQL := unassignedBookingCount
	args := []any{string(tenantID), query, query, query}
	if from != "" || to != "" {
		selectSQL += bookingDateClause
		countSQL += bookingDateClause
		args = append(args, from, from, to, to)
	}

	rows, err := r.dbConn.QueryContext(ctx, selectSQL+`
ORDER BY b.pickup_date DESC
LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	readModels, err := scanBookingReadModels(rows)
	if err != nil {
		return nil, 0, err
	}

	var count int64
	if err := r.dbConn.QueryRowContext(ctx, countSQL, args...).Scan(&count); err != nil {
		return nil, 0, err
	}

	return readModels, count, nil
}
