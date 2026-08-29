package sqlite

import (
	"context"
	"database/sql"
	"time"

	"transport-app/internal/domain"
	"transport-app/internal/repository"

	db "transport-app/db/generated/sqlite"
)

// TripRepository implementation

func (r *SQLRepository) CreateTrip(ctx context.Context, trip domain.Trip) (domain.Trip, error) {
	var bookingID, driverID, vehicleID sql.NullString
	if trip.BookingID != nil {
		bookingID = sql.NullString{String: string(*trip.BookingID), Valid: true}
	}
	if trip.DriverID != nil {
		driverID = sql.NullString{String: string(*trip.DriverID), Valid: true}
	}
	if trip.VehicleID != nil {
		vehicleID = sql.NullString{String: string(*trip.VehicleID), Valid: true}
	}

	created, err := r.Q(ctx).CreateTrip(ctx, db.CreateTripParams{
		ID:            string(trip.ID),
		TripNumber:    trip.TripNumber,
		BookingID:     bookingID,
		DriverID:      driverID,
		VehicleID:     vehicleID,
		RouteID:       string(trip.RouteID),
		DepartureTime: trip.DepartureTime,
		ArrivalTime:   nullTime(trip.ArrivalTime),
		Status:        string(trip.Status),
		Remarks:       nullString(trip.Remarks),
		TenantID:      tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Trip{}, err
	}
	t := db.Trip{
		ID:            created.ID,
		TripNumber:    created.TripNumber,
		BookingID:     created.BookingID,
		DriverID:      created.DriverID,
		VehicleID:     created.VehicleID,
		RouteID:       created.RouteID,
		DepartureTime: created.DepartureTime,
		ArrivalTime:   created.ArrivalTime,
		Status:        created.Status,
		Remarks:       created.Remarks,
		CreatedAt:     created.CreatedAt,
		UpdatedAt:     created.UpdatedAt,
	}
	return toDomainTrip(t), nil
}

func (r *SQLRepository) GetTripByID(ctx context.Context, id domain.TripID) (repository.TripWithJoins, error) {
	row, err := r.Q(ctx).GetTripByID(ctx, db.GetTripByIDParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return repository.TripWithJoins{}, err
	}
	return tripRowToWithJoins(
		row.ID, row.TripNumber, row.BookingID, row.DriverID, row.VehicleID,
		row.RouteID, row.DepartureTime, row.ArrivalTime, row.Status, row.Remarks,
		row.CreatedAt, row.UpdatedAt,
		row.DriverDisplayID, row.DriverFirstName, row.DriverLastName,
		row.VehicleRegistrationNumber, row.VehicleNumber,
		row.RouteSource, row.RouteDestination,
	), nil
}

func (r *SQLRepository) GetTripByNumber(ctx context.Context, number string) (repository.TripWithJoins, error) {
	row, err := r.Q(ctx).GetTripByNumber(ctx, db.GetTripByNumberParams{
		TripNumber: number,
		TenantID:   tenantIDFromCtx(ctx),
	})
	if err != nil {
		return repository.TripWithJoins{}, err
	}
	return tripRowToWithJoins(
		row.ID, row.TripNumber, row.BookingID, row.DriverID, row.VehicleID,
		row.RouteID, row.DepartureTime, row.ArrivalTime, row.Status, row.Remarks,
		row.CreatedAt, row.UpdatedAt,
		row.DriverDisplayID, row.DriverFirstName, row.DriverLastName,
		row.VehicleRegistrationNumber, row.VehicleNumber,
		row.RouteSource, row.RouteDestination,
	), nil
}

func (r *SQLRepository) GetTripByBookingID(ctx context.Context, bookingID domain.BookingID) (repository.TripWithJoins, error) {
	row, err := r.Q(ctx).GetTripByBookingID(ctx, db.GetTripByBookingIDParams{
		BookingID: sql.NullString{String: string(bookingID), Valid: true},
		TenantID:  tenantIDFromCtx(ctx),
	})
	if err != nil {
		return repository.TripWithJoins{}, err
	}
	return tripRowToWithJoins(
		row.ID, row.TripNumber, row.BookingID, row.DriverID, row.VehicleID,
		row.RouteID, row.DepartureTime, row.ArrivalTime, row.Status, row.Remarks,
		row.CreatedAt, row.UpdatedAt,
		row.DriverDisplayID, row.DriverFirstName, row.DriverLastName,
		row.VehicleRegistrationNumber, row.VehicleNumber,
		row.RouteSource, row.RouteDestination,
	), nil
}

func (r *SQLRepository) UpdateTrip(ctx context.Context, trip domain.Trip) (domain.Trip, error) {
	current, err := r.Q(ctx).GetTripByID(ctx, db.GetTripByIDParams{
		ID:       string(trip.ID),
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Trip{}, err
	}

	var bookingID, driverID, vehicleID sql.NullString
	if trip.BookingID != nil {
		bookingID = sql.NullString{String: string(*trip.BookingID), Valid: true}
	}
	if trip.DriverID != nil {
		driverID = sql.NullString{String: string(*trip.DriverID), Valid: true}
	}
	if trip.VehicleID != nil {
		vehicleID = sql.NullString{String: string(*trip.VehicleID), Valid: true}
	}

	updated, err := r.Q(ctx).UpdateTrip(ctx, db.UpdateTripParams{
		TripNumber:    trip.TripNumber,
		BookingID:     bookingID,
		DriverID:      driverID,
		VehicleID:     vehicleID,
		RouteID:       string(trip.RouteID),
		DepartureTime: trip.DepartureTime,
		ArrivalTime:   nullTime(trip.ArrivalTime),
		Status:        string(trip.Status),
		Remarks:       nullString(trip.Remarks),
		ID:            string(trip.ID),
		TenantID:      tenantIDFromCtx(ctx),
		Version:       current.Version,
	})
	if err != nil {
		return domain.Trip{}, err
	}
	t := db.Trip{
		ID:            updated.ID,
		TripNumber:    updated.TripNumber,
		BookingID:     updated.BookingID,
		DriverID:      updated.DriverID,
		VehicleID:     updated.VehicleID,
		RouteID:       updated.RouteID,
		DepartureTime: updated.DepartureTime,
		ArrivalTime:   updated.ArrivalTime,
		Status:        updated.Status,
		Remarks:       updated.Remarks,
		CreatedAt:     updated.CreatedAt,
		UpdatedAt:     updated.UpdatedAt,
	}
	return toDomainTrip(t), nil
}

func (r *SQLRepository) UpdateTripStatus(ctx context.Context, id domain.TripID, status domain.TripStatus) (domain.Trip, error) {
	current, err := r.Q(ctx).GetTripByID(ctx, db.GetTripByIDParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Trip{}, err
	}
	updated, err := r.Q(ctx).UpdateTripStatus(ctx, db.UpdateTripStatusParams{
		Status:   string(status),
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
		Version:  current.Version,
	})
	if err != nil {
		return domain.Trip{}, err
	}
	t := db.Trip{
		ID:            updated.ID,
		TripNumber:    updated.TripNumber,
		BookingID:     updated.BookingID,
		DriverID:      updated.DriverID,
		VehicleID:     updated.VehicleID,
		RouteID:       updated.RouteID,
		DepartureTime: updated.DepartureTime,
		ArrivalTime:   updated.ArrivalTime,
		Status:        updated.Status,
		Remarks:       updated.Remarks,
		CreatedAt:     updated.CreatedAt,
		UpdatedAt:     updated.UpdatedAt,
	}
	return toDomainTrip(t), nil
}

func (r *SQLRepository) AssignDriver(ctx context.Context, tripID domain.TripID, driverID domain.DriverID) (domain.Trip, error) {
	current, err := r.Q(ctx).GetTripByID(ctx, db.GetTripByIDParams{
		ID:       string(tripID),
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Trip{}, err
	}
	updated, err := r.Q(ctx).AssignDriverToTrip(ctx, db.AssignDriverToTripParams{
		DriverID: sql.NullString{String: string(driverID), Valid: true},
		ID:       string(tripID),
		TenantID: tenantIDFromCtx(ctx),
		Version:  current.Version,
	})
	if err != nil {
		return domain.Trip{}, err
	}
	t := db.Trip{
		ID:            updated.ID,
		TripNumber:    updated.TripNumber,
		BookingID:     updated.BookingID,
		DriverID:      updated.DriverID,
		VehicleID:     updated.VehicleID,
		RouteID:       updated.RouteID,
		DepartureTime: updated.DepartureTime,
		ArrivalTime:   updated.ArrivalTime,
		Status:        updated.Status,
		Remarks:       updated.Remarks,
		CreatedAt:     updated.CreatedAt,
		UpdatedAt:     updated.UpdatedAt,
	}
	return toDomainTrip(t), nil
}

func (r *SQLRepository) AssignVehicle(ctx context.Context, tripID domain.TripID, vehicleID domain.VehicleID) (domain.Trip, error) {
	current, err := r.Q(ctx).GetTripByID(ctx, db.GetTripByIDParams{
		ID:       string(tripID),
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Trip{}, err
	}
	updated, err := r.Q(ctx).AssignVehicleToTrip(ctx, db.AssignVehicleToTripParams{
		VehicleID: sql.NullString{String: string(vehicleID), Valid: true},
		ID:        string(tripID),
		TenantID:  tenantIDFromCtx(ctx),
		Version:   current.Version,
	})
	if err != nil {
		return domain.Trip{}, err
	}
	t := db.Trip{
		ID:            updated.ID,
		TripNumber:    updated.TripNumber,
		BookingID:     updated.BookingID,
		DriverID:      updated.DriverID,
		VehicleID:     updated.VehicleID,
		RouteID:       updated.RouteID,
		DepartureTime: updated.DepartureTime,
		ArrivalTime:   updated.ArrivalTime,
		Status:        updated.Status,
		Remarks:       updated.Remarks,
		CreatedAt:     updated.CreatedAt,
		UpdatedAt:     updated.UpdatedAt,
	}
	return toDomainTrip(t), nil
}

func (r *SQLRepository) DeleteTrip(ctx context.Context, id domain.TripID) error {
	return r.Q(ctx).DeleteTrip(ctx, db.DeleteTripParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
}

func (r *SQLRepository) SearchTrips(ctx context.Context, query string, status string, limit, offset int) ([]repository.TripWithJoins, error) {
	rows, err := r.Q(ctx).SearchTrips(ctx, db.SearchTripsParams{
		TenantID: tenantIDFromCtx(ctx),
		Query:    query,
		Status:   status,
		Limit:    int64(limit),
		Offset:   int64(offset),
	})
	if err != nil {
		return nil, err
	}
	result := make([]repository.TripWithJoins, len(rows))
	for i, row := range rows {
		result[i] = tripRowToWithJoins(
			row.ID, row.TripNumber, row.BookingID, row.DriverID, row.VehicleID,
			row.RouteID, row.DepartureTime, row.ArrivalTime, row.Status, row.Remarks,
			row.CreatedAt, row.UpdatedAt,
			row.DriverDisplayID, row.DriverFirstName, row.DriverLastName,
			row.VehicleRegistrationNumber, row.VehicleNumber,
			row.RouteSource, row.RouteDestination,
		)
	}
	return result, nil
}

func (r *SQLRepository) CountTrips(ctx context.Context, query string, status string) (int64, error) {
	count, err := r.Q(ctx).CountTrips(ctx, db.CountTripsParams{
		TenantID: tenantIDFromCtx(ctx),
		Query:    query,
		Status:   status,
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *SQLRepository) CheckVehicleConflict(ctx context.Context, vehicleID domain.VehicleID, excludeTripID *domain.TripID) ([]domain.Trip, error) {
	excludeID := ""
	if excludeTripID != nil {
		excludeID = string(*excludeTripID)
	}

	rows, err := r.Q(ctx).CheckVehicleConflict(ctx, db.CheckVehicleConflictParams{
		VehicleID: sql.NullString{String: string(vehicleID), Valid: true},
		TenantID:  tenantIDFromCtx(ctx),
		Column3:   excludeID,
		ID:        excludeID,
	})
	if err != nil {
		return nil, err
	}
	result := make([]domain.Trip, len(rows))
	for i, row := range rows {
		t := db.Trip{
			ID:            row.ID,
			TripNumber:    row.TripNumber,
			Status:        row.Status,
			DepartureTime: row.DepartureTime,
		}
		result[i] = toDomainTrip(t)
	}
	return result, nil
}

func (r *SQLRepository) CheckDriverConflict(ctx context.Context, driverID domain.DriverID, excludeTripID *domain.TripID) ([]domain.Trip, error) {
	excludeID := ""
	if excludeTripID != nil {
		excludeID = string(*excludeTripID)
	}

	rows, err := r.Q(ctx).CheckDriverConflict(ctx, db.CheckDriverConflictParams{
		DriverID: sql.NullString{String: string(driverID), Valid: true},
		TenantID: tenantIDFromCtx(ctx),
		Column3:  excludeID,
		ID:       excludeID,
	})
	if err != nil {
		return nil, err
	}
	result := make([]domain.Trip, len(rows))
	for i, row := range rows {
		t := db.Trip{
			ID:            row.ID,
			TripNumber:    row.TripNumber,
			Status:        row.Status,
			DepartureTime: row.DepartureTime,
		}
		result[i] = toDomainTrip(t)
	}
	return result, nil
}

func (r *SQLRepository) GetTripsByDate(ctx context.Context, date string) ([]repository.TripWithJoins, error) {
	t, _ := time.Parse("2006-01-02", date)
	rows, err := r.Q(ctx).GetTripsByDate(ctx, db.GetTripsByDateParams{
		DepartureTime: t,
		TenantID:      tenantIDFromCtx(ctx),
	})
	if err != nil {
		return nil, err
	}
	result := make([]repository.TripWithJoins, len(rows))
	for i, row := range rows {
		result[i] = tripRowToWithJoins(
			row.ID, row.TripNumber, row.BookingID, row.DriverID, row.VehicleID,
			row.RouteID, row.DepartureTime, row.ArrivalTime, row.Status, row.Remarks,
			row.CreatedAt, row.UpdatedAt,
			row.DriverDisplayID, row.DriverFirstName, row.DriverLastName,
			row.VehicleRegistrationNumber, row.VehicleNumber,
			row.RouteSource, row.RouteDestination,
		)
	}
	return result, nil
}

func (r *SQLRepository) CountTripsByStatusForDate(ctx context.Context, date string) (map[domain.TripStatus]int64, error) {
	t, _ := time.Parse("2006-01-02", date)
	rows, err := r.Q(ctx).CountTripsByStatus(ctx, db.CountTripsByStatusParams{
		DepartureTime: t,
		TenantID:      tenantIDFromCtx(ctx),
	})
	if err != nil {
		return nil, err
	}
	result := make(map[domain.TripStatus]int64)
	for _, row := range rows {
		result[domain.TripStatus(row.Status)] = row.Count
	}
	return result, nil
}

func (r *SQLRepository) GetOverdueTrips(ctx context.Context) ([]repository.TripWithJoins, error) {
	rows, err := r.Q(ctx).GetOverdueTrips(ctx, tenantIDFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	result := make([]repository.TripWithJoins, len(rows))
	for i, row := range rows {
		result[i] = tripRowToWithJoins(
			row.ID, row.TripNumber, row.BookingID, row.DriverID, row.VehicleID,
			row.RouteID, row.DepartureTime, row.ArrivalTime, row.Status, row.Remarks,
			row.CreatedAt, row.UpdatedAt,
			row.DriverDisplayID, row.DriverFirstName, row.DriverLastName,
			row.VehicleRegistrationNumber, row.VehicleNumber,
			row.RouteSource, row.RouteDestination,
		)
	}
	return result, nil
}
