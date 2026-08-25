package sql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"transport-app/internal/shared"
	tripdomain "transport-app/internal/trip/domain"
)

// Date-range search variants (optional interface asserted by ListTripsUseCase
// when from/to filters are present). Keeps the core TripRepository interface
// and its mocks untouched.

const tripDateClause = `
  AND (? = '' OR date(substr(t.departure_time,1,10)) >= date(?))
  AND (? = '' OR date(substr(t.departure_time,1,10)) <= date(?))`

func (r *tripRepository) SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]tripdomain.TripReadModel, int64, error) {
	qPattern := "%" + query + "%"

	querySQL := `
SELECT t.id, t.trip_number, t.booking_id, t.driver_id, t.vehicle_id, t.route_id,
    t.departure_time, t.arrival_time, t.status, t.remarks, t.created_at, t.updated_at,
    t.started_at, t.reached_pickup_at, t.in_transit_at, t.delivered_at, t.completed_at,
    COALESCE(d.driver_id, '') AS driver_display_id,
    COALESCE(d.first_name, '') AS driver_first_name,
    COALESCE(d.last_name, '') AS driver_last_name,
    COALESCE(v.registration_number, '') AS vehicle_registration_number,
    COALESCE(v.vehicle_number, '') AS vehicle_number,
    COALESCE(r.source, '') AS route_source,
    COALESCE(r.destination, '') AS route_destination
FROM trips t
LEFT JOIN drivers d ON t.driver_id = d.id
LEFT JOIN vehicles v ON t.vehicle_id = v.id
LEFT JOIN routes r ON t.route_id = r.id
WHERE t.tenant_id = ?
  AND (? = '' OR t.trip_number LIKE ? OR d.first_name LIKE ? OR d.last_name LIKE ? OR v.registration_number LIKE ? OR r.source LIKE ? OR r.destination LIKE ?)
  AND (? = '' OR t.status = ?)` + tripDateClause + `
ORDER BY t.departure_time DESC
LIMIT ? OFFSET ?`

	rows, err := r.dbConn.QueryContext(ctx, querySQL,
		string(tenantID),
		query, qPattern, qPattern, qPattern, qPattern, qPattern, qPattern,
		status, status,
		from, from, to, to,
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	readModels, err := scanTripReadModels(rows)
	if err != nil {
		return nil, 0, err
	}

	countSQL := `
SELECT COUNT(*)
FROM trips t
LEFT JOIN drivers d ON t.driver_id = d.id
LEFT JOIN vehicles v ON t.vehicle_id = v.id
LEFT JOIN routes r ON t.route_id = r.id
WHERE t.tenant_id = ?
  AND (? = '' OR t.trip_number LIKE ? OR d.first_name LIKE ? OR d.last_name LIKE ? OR v.registration_number LIKE ? OR r.source LIKE ? OR r.destination LIKE ?)
  AND (? = '' OR t.status = ?)` + tripDateClause

	var count int64
	err = r.dbConn.QueryRowContext(ctx, countSQL,
		string(tenantID),
		query, qPattern, qPattern, qPattern, qPattern, qPattern, qPattern,
		status, status,
		from, from, to, to,
	).Scan(&count)
	if err != nil {
		return nil, 0, err
	}

	return readModels, count, nil
}

func (r *tripRepository) SearchReadModelsByDriverDateRange(ctx context.Context, tenantID shared.TenantID, driverIDs []string, query string, status string, from string, to string, limit int, offset int) ([]tripdomain.TripReadModel, int64, error) {
	if len(driverIDs) == 0 {
		return nil, 0, sql.ErrNoRows
	}

	allDriverIDs := make(map[string]struct{})
	for _, id := range driverIDs {
		if id == "" {
			continue
		}
		var dID, dCode string
		err := r.dbConn.QueryRowContext(ctx, `
			SELECT id, driver_id FROM drivers
			WHERE tenant_id = ? AND (id = ? OR driver_id = ? OR email = (SELECT email FROM users WHERE id = ?))
			LIMIT 1
		`, string(tenantID), id, id, id).Scan(&dID, &dCode)
		if err == nil {
			if dID != "" {
				allDriverIDs[dID] = struct{}{}
			}
			if dCode != "" {
				allDriverIDs[dCode] = struct{}{}
			}
		}
	}

	var resolvedIDs []string
	for id := range allDriverIDs {
		resolvedIDs = append(resolvedIDs, id)
	}
	if len(resolvedIDs) == 0 {
		return nil, 0, sql.ErrNoRows
	}

	qPattern := "%" + query + "%"

	placeholders := make([]string, len(resolvedIDs))
	for i := range resolvedIDs {
		placeholders[i] = "?"
	}
	driverClause := fmt.Sprintf("t.driver_id IN (%s)", strings.Join(placeholders, ","))

	querySQL := fmt.Sprintf(`
SELECT t.id, t.trip_number, t.booking_id, t.driver_id, t.vehicle_id, t.route_id,
    t.departure_time, t.arrival_time, t.status, t.remarks, t.created_at, t.updated_at,
    t.started_at, t.reached_pickup_at, t.in_transit_at, t.delivered_at, t.completed_at,
    COALESCE(d.driver_id, '') AS driver_display_id,
    COALESCE(d.first_name, '') AS driver_first_name,
    COALESCE(d.last_name, '') AS driver_last_name,
    COALESCE(v.registration_number, '') AS vehicle_registration_number,
    COALESCE(v.vehicle_number, '') AS vehicle_number,
    COALESCE(r.source, '') AS route_source,
    COALESCE(r.destination, '') AS route_destination
FROM trips t
LEFT JOIN drivers d ON t.driver_id = d.id
LEFT JOIN vehicles v ON t.vehicle_id = v.id
LEFT JOIN routes r ON t.route_id = r.id
WHERE t.tenant_id = ?
  AND %s
  AND (? = '' OR t.trip_number LIKE ? OR d.first_name LIKE ? OR d.last_name LIKE ? OR v.registration_number LIKE ? OR r.source LIKE ? OR r.destination LIKE ?)
  AND (? = '' OR t.status = ?)`+tripDateClause+`
ORDER BY t.departure_time DESC
LIMIT ? OFFSET ?`, driverClause)

	args := []interface{}{string(tenantID)}
	for _, id := range resolvedIDs {
		args = append(args, id)
	}
	args = append(args, query, qPattern, qPattern, qPattern, qPattern, qPattern, qPattern, status, status, from, from, to, to, limit, offset)

	rows, err := r.dbConn.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	readModels, err := scanTripReadModels(rows)
	if err != nil {
		return nil, 0, err
	}

	countSQL := fmt.Sprintf(`
SELECT COUNT(*)
FROM trips t
LEFT JOIN drivers d ON t.driver_id = d.id
LEFT JOIN vehicles v ON t.vehicle_id = v.id
LEFT JOIN routes r ON t.route_id = r.id
WHERE t.tenant_id = ?
  AND %s
  AND (? = '' OR t.trip_number LIKE ? OR d.first_name LIKE ? OR d.last_name LIKE ? OR v.registration_number LIKE ? OR r.source LIKE ? OR r.destination LIKE ?)
  AND (? = '' OR t.status = ?)`+tripDateClause, driverClause)

	countArgs := []interface{}{string(tenantID)}
	for _, id := range resolvedIDs {
		countArgs = append(countArgs, id)
	}
	countArgs = append(countArgs, query, qPattern, qPattern, qPattern, qPattern, qPattern, qPattern, status, status, from, from, to, to)

	var count int64
	err = r.dbConn.QueryRowContext(ctx, countSQL, countArgs...).Scan(&count)
	if err != nil {
		return nil, 0, err
	}

	return readModels, count, nil
}

func scanTripReadModels(rows *sql.Rows) ([]tripdomain.TripReadModel, error) {
	var readModels []tripdomain.TripReadModel
	for rows.Next() {
		var m tripdomain.TripReadModel
		var bookingID, driverID, vehicleID sql.NullString
		var arrivalTime, startedAt, reachedPickupAt, inTransitAt, deliveredAt, completedAt sql.NullTime
		var remarks sql.NullString

		if err := rows.Scan(
			&m.ID, &m.TripNumber, &bookingID, &driverID, &vehicleID, &m.RouteID,
			&m.DepartureTime, &arrivalTime, &m.Status, &remarks, &m.CreatedAt, &m.UpdatedAt,
			&startedAt, &reachedPickupAt, &inTransitAt, &deliveredAt, &completedAt,
			&m.DriverDisplayID, &m.DriverFirstName, &m.DriverLastName,
			&m.VehicleRegistrationNumber, &m.VehicleNumber,
			&m.RouteSource, &m.RouteDestination,
		); err != nil {
			return nil, err
		}

		if bookingID.Valid {
			m.BookingID = &bookingID.String
		}
		if driverID.Valid {
			m.DriverID = &driverID.String
		}
		if vehicleID.Valid {
			m.VehicleID = &vehicleID.String
		}
		if arrivalTime.Valid {
			m.ArrivalTime = &arrivalTime.Time
		}
		if remarks.Valid {
			m.Remarks = remarks.String
		}
		if startedAt.Valid {
			m.StartedAt = &startedAt.Time
		}
		if reachedPickupAt.Valid {
			m.ReachedPickupAt = &reachedPickupAt.Time
		}
		if inTransitAt.Valid {
			m.InTransitAt = &inTransitAt.Time
		}
		if deliveredAt.Valid {
			m.DeliveredAt = &deliveredAt.Time
		}
		if completedAt.Valid {
			m.CompletedAt = &completedAt.Time
		}

		readModels = append(readModels, m)
	}
	return readModels, nil
}
