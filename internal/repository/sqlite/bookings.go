package sqlite

import (
	"context"
	"database/sql"

	"transport-app/internal/domain"
	"transport-app/internal/repository"

	db "transport-app/db/generated/sqlite"
)

// BookingRepository implementation

func (r *SQLRepository) CreateBooking(ctx context.Context, booking domain.Booking) (domain.Booking, error) {
	row, err := r.Q(ctx).CreateBooking(ctx, db.CreateBookingParams{
		ID:            string(booking.ID),
		BookingNumber: booking.BookingNumber,
		CustomerID:    string(booking.CustomerID),
		PickupDate:    booking.PickupDate,
		RouteID:       string(booking.RouteID),
		VehicleType:   string(booking.VehicleType),
		Passengers:    booking.Passengers,
		CargoWeight:   nullFloat(booking.CargoWeight),
		Price:         booking.Price,
		Notes:         nullString(booking.Notes),
		Status:        string(booking.Status),
		TenantID:      tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Booking{}, err
	}
	b := db.Booking{
		ID:            row.ID,
		BookingNumber: row.BookingNumber,
		CustomerID:    row.CustomerID,
		PickupDate:    row.PickupDate,
		RouteID:       row.RouteID,
		VehicleType:   row.VehicleType,
		Passengers:    row.Passengers,
		CargoWeight:   row.CargoWeight,
		Price:         row.Price,
		Notes:         row.Notes,
		Status:        row.Status,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	return toDomainBooking(b), nil
}

func (r *SQLRepository) GetBookingByID(ctx context.Context, id domain.BookingID) (repository.BookingWithJoins, error) {
	row, err := r.Q(ctx).GetBookingByID(ctx, db.GetBookingByIDParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return repository.BookingWithJoins{}, err
	}
	b := db.Booking{
		ID:            row.ID,
		BookingNumber: row.BookingNumber,
		CustomerID:    row.CustomerID,
		PickupDate:    row.PickupDate,
		RouteID:       row.RouteID,
		VehicleType:   row.VehicleType,
		Passengers:    row.Passengers,
		CargoWeight:   row.CargoWeight,
		Price:         row.Price,
		Notes:         row.Notes,
		Status:        row.Status,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	return repository.BookingWithJoins{
		Booking:          toDomainBooking(b),
		CustomerName:     row.CustomerName,
		CustomerCompany:  fromNullString(row.CustomerCompany),
		RouteSource:      row.RouteSource,
		RouteDestination: row.RouteDestination,
	}, nil
}

func (r *SQLRepository) GetBookingByNumber(ctx context.Context, number string) (repository.BookingWithJoins, error) {
	row, err := r.Q(ctx).GetBookingByNumber(ctx, db.GetBookingByNumberParams{
		BookingNumber: number,
		TenantID:      tenantIDFromCtx(ctx),
	})
	if err != nil {
		return repository.BookingWithJoins{}, err
	}
	b := db.Booking{
		ID:            row.ID,
		BookingNumber: row.BookingNumber,
		CustomerID:    row.CustomerID,
		PickupDate:    row.PickupDate,
		RouteID:       row.RouteID,
		VehicleType:   row.VehicleType,
		Passengers:    row.Passengers,
		CargoWeight:   row.CargoWeight,
		Price:         row.Price,
		Notes:         row.Notes,
		Status:        row.Status,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	return repository.BookingWithJoins{
		Booking:          toDomainBooking(b),
		CustomerName:     row.CustomerName,
		CustomerCompany:  fromNullString(row.CustomerCompany),
		RouteSource:      row.RouteSource,
		RouteDestination: row.RouteDestination,
	}, nil
}

func (r *SQLRepository) UpdateBooking(ctx context.Context, booking domain.Booking) (domain.Booking, error) {
	current, err := r.Q(ctx).GetBookingByID(ctx, db.GetBookingByIDParams{
		ID:       string(booking.ID),
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Booking{}, err
	}

	updated, err := r.Q(ctx).UpdateBooking(ctx, db.UpdateBookingParams{
		BookingNumber: booking.BookingNumber,
		CustomerID:    string(booking.CustomerID),
		PickupDate:    booking.PickupDate,
		RouteID:       string(booking.RouteID),
		VehicleType:   string(booking.VehicleType),
		Passengers:    booking.Passengers,
		CargoWeight:   nullFloat(booking.CargoWeight),
		Price:         booking.Price,
		Notes:         nullString(booking.Notes),
		Status:        string(booking.Status),
		ID:            string(booking.ID),
		TenantID:      tenantIDFromCtx(ctx),
		Version:       current.Version,
	})
	if err != nil {
		return domain.Booking{}, err
	}
	b := db.Booking{
		ID:            updated.ID,
		BookingNumber: updated.BookingNumber,
		CustomerID:    updated.CustomerID,
		PickupDate:    updated.PickupDate,
		RouteID:       updated.RouteID,
		VehicleType:   updated.VehicleType,
		Passengers:    updated.Passengers,
		CargoWeight:   updated.CargoWeight,
		Price:         updated.Price,
		Notes:         updated.Notes,
		Status:        updated.Status,
		CreatedAt:     updated.CreatedAt,
		UpdatedAt:     updated.UpdatedAt,
	}
	return toDomainBooking(b), nil
}

func (r *SQLRepository) UpdateBookingStatus(ctx context.Context, id domain.BookingID, status domain.BookingStatus) (domain.Booking, error) {
	current, err := r.Q(ctx).GetBookingByID(ctx, db.GetBookingByIDParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Booking{}, err
	}
	updated, err := r.Q(ctx).UpdateBookingStatus(ctx, db.UpdateBookingStatusParams{
		Status:   string(status),
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
		Version:  current.Version,
	})
	if err != nil {
		return domain.Booking{}, err
	}
	b := db.Booking{
		ID:            updated.ID,
		BookingNumber: updated.BookingNumber,
		CustomerID:    updated.CustomerID,
		PickupDate:    updated.PickupDate,
		RouteID:       updated.RouteID,
		VehicleType:   updated.VehicleType,
		Passengers:    updated.Passengers,
		CargoWeight:   updated.CargoWeight,
		Price:         updated.Price,
		Notes:         updated.Notes,
		Status:        updated.Status,
		CreatedAt:     updated.CreatedAt,
		UpdatedAt:     updated.UpdatedAt,
	}
	return toDomainBooking(b), nil
}

func (r *SQLRepository) DeleteBooking(ctx context.Context, id domain.BookingID) error {
	return r.Q(ctx).DeleteBooking(ctx, db.DeleteBookingParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
}

func (r *SQLRepository) SearchBookings(ctx context.Context, query string, status string, limit, offset int) ([]repository.BookingWithJoins, error) {
	rows, err := r.Q(ctx).SearchBookings(ctx, db.SearchBookingsParams{
		TenantID: tenantIDFromCtx(ctx),
		Column2:  sql.NullString{String: query, Valid: true},
		Column3:  sql.NullString{String: query, Valid: true},
		Column4:  sql.NullString{String: query, Valid: true},
		Column5:  status,
		Status:   status,
		Limit:    int64(limit),
		Offset:   int64(offset),
	})
	if err != nil {
		return nil, err
	}
	result := make([]repository.BookingWithJoins, len(rows))
	for i, row := range rows {
		b := db.Booking{
			ID:            row.ID,
			BookingNumber: row.BookingNumber,
			CustomerID:    row.CustomerID,
			PickupDate:    row.PickupDate,
			RouteID:       row.RouteID,
			VehicleType:   row.VehicleType,
			Passengers:    row.Passengers,
			CargoWeight:   row.CargoWeight,
			Price:         row.Price,
			Notes:         row.Notes,
			Status:        row.Status,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
		}
		result[i] = repository.BookingWithJoins{
			Booking:          toDomainBooking(b),
			CustomerName:     row.CustomerName,
			CustomerCompany:  fromNullString(row.CustomerCompany),
			RouteSource:      row.RouteSource,
			RouteDestination: row.RouteDestination,
		}
	}
	return result, nil
}

func (r *SQLRepository) ListBookingsByCustomer(ctx context.Context, customerID domain.CustomerID, limit int) ([]repository.BookingWithJoins, error) {
	rows, err := r.Q(ctx).ListBookingsByCustomer(ctx, db.ListBookingsByCustomerParams{
		TenantID:   tenantIDFromCtx(ctx),
		CustomerID: string(customerID),
		Limit:      int64(limit),
	})
	if err != nil {
		return nil, err
	}
	result := make([]repository.BookingWithJoins, len(rows))
	for i, row := range rows {
		b := db.Booking{
			ID:            row.ID,
			BookingNumber: row.BookingNumber,
			CustomerID:    row.CustomerID,
			PickupDate:    row.PickupDate,
			RouteID:       row.RouteID,
			VehicleType:   row.VehicleType,
			Passengers:    row.Passengers,
			CargoWeight:   row.CargoWeight,
			Price:         row.Price,
			Notes:         row.Notes,
			Status:        row.Status,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
		}
		result[i] = repository.BookingWithJoins{
			Booking:          toDomainBooking(b),
			CustomerName:     row.CustomerName,
			CustomerCompany:  fromNullString(row.CustomerCompany),
			RouteSource:      row.RouteSource,
			RouteDestination: row.RouteDestination,
		}
	}
	return result, nil
}

func (r *SQLRepository) CountBookings(ctx context.Context, query string, status string) (int64, error) {
	count, err := r.Q(ctx).CountBookings(ctx, db.CountBookingsParams{
		TenantID: tenantIDFromCtx(ctx),
		Column2:  sql.NullString{String: query, Valid: true},
		Column3:  sql.NullString{String: query, Valid: true},
		Column4:  sql.NullString{String: query, Valid: true},
		Column5:  status,
		Status:   status,
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *SQLRepository) CountBookingsByDay(ctx context.Context) ([]repository.BookingsByDay, error) {
	rows, err := r.Q(ctx).CountBookingsByDay(ctx, tenantIDFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	result := make([]repository.BookingsByDay, len(rows))
	for i, row := range rows {
		result[i] = repository.BookingsByDay{
			Day:   row.Day,
			Count: row.Count,
		}
	}
	return result, nil
}
