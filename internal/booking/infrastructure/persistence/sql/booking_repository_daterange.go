package sql

import (
	"context"
	"database/sql"

	bookingdomain "transport-app/internal/booking/domain"
	"transport-app/internal/shared"
)

// Date-range search variant (optional interface asserted by ListBookingsUseCase
// when from/to pickup-date filters are present). Raw SQL keeps the sqlc
// query set and existing mocks untouched.

const bookingDateClause = `
  AND (? = '' OR date(substr(b.pickup_date,1,10)) >= date(?))
  AND (? = '' OR date(substr(b.pickup_date,1,10)) <= date(?))`

const bookingSearchSelect = `
SELECT b.id, b.booking_number, b.customer_id, b.pickup_date, b.route_id, b.vehicle_type,
    b.passengers, b.cargo_weight, b.price, b.notes, b.status, b.created_at, b.updated_at,
    c.name AS customer_name, c.company AS customer_company, r.source AS route_source, r.destination AS route_destination
FROM bookings b
JOIN customers c ON b.customer_id = c.id
JOIN routes r ON b.route_id = r.id
WHERE b.tenant_id = ?
  AND (b.booking_number LIKE '%' || ? || '%' OR c.name LIKE '%' || ? || '%' OR c.company LIKE '%' || ? || '%')
  AND (? = '' OR b.status = ?)`

const bookingSearchCount = `
SELECT COUNT(*)
FROM bookings b
JOIN customers c ON b.customer_id = c.id
JOIN routes r ON b.route_id = r.id
WHERE b.tenant_id = ?
  AND (b.booking_number LIKE '%' || ? || '%' OR c.name LIKE '%' || ? || '%' OR c.company LIKE '%' || ? || '%')
  AND (? = '' OR b.status = ?)`

func (r *bookingRepository) SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]bookingdomain.BookingReadModel, int64, error) {
	// "unassigned" can't be expressed by the fixed consts below.
	if status == StatusFilterUnassigned {
		return r.searchUnassignedBookings(ctx, tenantID, query, from, to, limit, offset)
	}
	rows, err := r.dbConn.QueryContext(ctx, bookingSearchSelect+bookingDateClause+`
ORDER BY b.pickup_date DESC
LIMIT ? OFFSET ?`,
		string(tenantID), query, query, query, status, status, from, from, to, to, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	readModels, err := scanBookingReadModels(rows)
	if err != nil {
		return nil, 0, err
	}

	var count int64
	if err := r.dbConn.QueryRowContext(ctx, bookingSearchCount+bookingDateClause,
		string(tenantID), query, query, query, status, status, from, from, to, to,
	).Scan(&count); err != nil {
		return nil, 0, err
	}

	return readModels, count, nil
}

func scanBookingReadModels(rows *sql.Rows) ([]bookingdomain.BookingReadModel, error) {
	readModels := make([]bookingdomain.BookingReadModel, 0)
	for rows.Next() {
		var m bookingdomain.BookingReadModel
		var cargoWeight sql.NullFloat64
		var notes, customerCompany sql.NullString

		if err := rows.Scan(
			&m.ID, &m.BookingNumber, &m.CustomerID, &m.PickupDate, &m.RouteID, &m.VehicleType,
			&m.Passengers, &cargoWeight, &m.Price, &notes, &m.Status, &m.CreatedAt, &m.UpdatedAt,
			&m.CustomerName, &customerCompany, &m.RouteSource, &m.RouteDestination,
		); err != nil {
			return nil, err
		}

		if cargoWeight.Valid {
			v := cargoWeight.Float64
			m.CargoWeight = &v
		}
		m.Notes = notes.String
		m.CustomerCompany = customerCompany.String

		readModels = append(readModels, m)
	}
	return readModels, nil
}
