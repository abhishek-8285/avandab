package sql

import (
	"context"
	"database/sql"
	"errors"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/booking/domain"
	"transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/shared/outbox"
)

type bookingRepository struct {
	dbConn *sql.DB
	q      *db.Queries
	outbox *outbox.OutboxWriter
}

// NewBookingRepository creates a SQLite-backed implementation of BookingRepository.
func NewBookingRepository(dbConn *sql.DB) domain.BookingRepository {
	return &bookingRepository{
		dbConn: dbConn,
		q:      db.New(dbConn),
		outbox: outbox.NewOutboxWriter(dbConn),
	}
}

// Q retrieves queries, using a transaction context if active.
func (r *bookingRepository) Q(ctx context.Context) *db.Queries {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return r.q.WithTx(tx)
	}
	return r.q
}

func (r *bookingRepository) Save(ctx context.Context, b *aggregate.BookingAggregate) error {
	exists, err := r.Exists(ctx, b.ID, b.TenantID)
	if err != nil {
		return err
	}

	p := MapToPersistence(b)

	if exists {
		_, err = r.Q(ctx).UpdateBooking(ctx, db.UpdateBookingParams{
			BookingNumber: p.BookingNumber,
			CustomerID:    p.CustomerID,
			PickupDate:    p.PickupDate,
			RouteID:       p.RouteID,
			VehicleType:   p.VehicleType,
			Passengers:    p.Passengers,
			CargoWeight:   p.CargoWeight,
			Price:         p.Price,
			Notes:         p.Notes,
			Status:        p.Status,
			ID:            p.ID,
			TenantID:      p.TenantID,
			Version:       p.Version,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("concurrency conflict: booking modified by another process")
			}
			return err
		}
		b.Version++
	} else {
		_, err = r.Q(ctx).CreateBooking(ctx, db.CreateBookingParams{
			ID:             p.ID,
			BookingNumber:  p.BookingNumber,
			CustomerID:     p.CustomerID,
			PickupDate:     p.PickupDate,
			RouteID:        p.RouteID,
			VehicleType:    p.VehicleType,
			Passengers:     p.Passengers,
			CargoWeight:    p.CargoWeight,
			Price:          p.Price,
			Notes:          p.Notes,
			Status:         p.Status,
			TenantID:       p.TenantID,
			IdempotencyKey: p.IdempotencyKey,
		})
		if err != nil {
			return err
		}
		b.Version = 1
	}

	err = r.outbox.SaveEvents(ctx, string(b.ID), "Booking", b.Events())
	if err != nil {
		return err
	}
	b.ClearEvents()
	return nil
}

func (r *bookingRepository) Find(ctx context.Context, id aggregate.BookingID, tenantID shared.TenantID) (*aggregate.BookingAggregate, error) {
	row, err := r.Q(ctx).GetBookingByID(ctx, db.GetBookingByIDParams{
		ID:       string(id),
		TenantID: string(tenantID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("booking not found")
		}
		return nil, err
	}

	m := SQLBookingModel{
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
		TenantID:      row.TenantID,
		Version:       row.Version,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	return MapToAggregate(m), nil
}

func (r *bookingRepository) FindByNumber(ctx context.Context, number string, tenantID shared.TenantID) (*aggregate.BookingAggregate, error) {
	row, err := r.Q(ctx).GetBookingByNumber(ctx, db.GetBookingByNumberParams{
		BookingNumber: number,
		TenantID:      string(tenantID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("booking not found")
		}
		return nil, err
	}

	m := SQLBookingModel{
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
		TenantID:      row.TenantID,
		Version:       row.Version,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	return MapToAggregate(m), nil
}

func (r *bookingRepository) FindByIdempotencyKey(ctx context.Context, key string, tenantID shared.TenantID) (*aggregate.BookingAggregate, error) {
	if key == "" {
		return nil, errors.New("booking not found")
	}
	row, err := r.Q(ctx).GetBookingByIdempotencyKey(ctx, db.GetBookingByIdempotencyKeyParams{
		IdempotencyKey: sql.NullString{String: key, Valid: true},
		TenantID:       string(tenantID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("booking not found")
		}
		return nil, err
	}

	m := SQLBookingModel{
		ID:             row.ID,
		BookingNumber:  row.BookingNumber,
		CustomerID:     row.CustomerID,
		PickupDate:     row.PickupDate,
		RouteID:        row.RouteID,
		VehicleType:    row.VehicleType,
		Passengers:     row.Passengers,
		CargoWeight:    row.CargoWeight,
		Price:          row.Price,
		Notes:          row.Notes,
		Status:         row.Status,
		TenantID:       row.TenantID,
		Version:        row.Version,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		IdempotencyKey: sql.NullString{String: key, Valid: true},
	}
	return MapToAggregate(m), nil
}

func (r *bookingRepository) Exists(ctx context.Context, id aggregate.BookingID, tenantID shared.TenantID) (bool, error) {
	_, err := r.Q(ctx).GetBookingByID(ctx, db.GetBookingByIDParams{
		ID:       string(id),
		TenantID: string(tenantID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *bookingRepository) GetReadModel(ctx context.Context, id aggregate.BookingID, tenantID shared.TenantID) (domain.BookingReadModel, error) {
	row, err := r.Q(ctx).GetBookingByID(ctx, db.GetBookingByIDParams{
		ID:       string(id),
		TenantID: string(tenantID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.BookingReadModel{}, errors.New("booking not found")
		}
		return domain.BookingReadModel{}, err
	}

	var cargoWeight *float64
	if row.CargoWeight.Valid {
		val := row.CargoWeight.Float64
		cargoWeight = &val
	}

	var notes string
	if row.Notes.Valid {
		notes = row.Notes.String
	}

	var customerCompany string
	if row.CustomerCompany.Valid {
		customerCompany = row.CustomerCompany.String
	}

	return domain.BookingReadModel{
		ID:               row.ID,
		BookingNumber:    row.BookingNumber,
		CustomerID:       row.CustomerID,
		CustomerName:     row.CustomerName,
		CustomerCompany:  customerCompany,
		RouteID:          row.RouteID,
		RouteSource:      row.RouteSource,
		RouteDestination: row.RouteDestination,
		PickupDate:       row.PickupDate,
		VehicleType:      row.VehicleType,
		Passengers:       row.Passengers,
		CargoWeight:      cargoWeight,
		Price:            row.Price,
		Notes:            notes,
		Status:           row.Status,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}, nil
}

func (r *bookingRepository) SearchReadModels(ctx context.Context, tenantID shared.TenantID, query string, status string, limit int, offset int) ([]domain.BookingReadModel, int64, error) {
	// "unassigned" spans two booking states plus a NOT EXISTS trip check;
	// the sqlc exact-match query can't express it.
	if status == StatusFilterUnassigned {
		return r.searchUnassignedBookings(ctx, tenantID, query, "", "", limit, offset)
	}
	rows, err := r.Q(ctx).SearchBookings(ctx, db.SearchBookingsParams{
		TenantID: string(tenantID),
		Column2:  sql.NullString{String: query, Valid: true},
		Column3:  sql.NullString{String: query, Valid: true},
		Column4:  sql.NullString{String: query, Valid: true},
		Column5:  status,
		Status:   status,
		Limit:    int64(limit),
		Offset:   int64(offset),
	})
	if err != nil {
		return nil, 0, err
	}

	count, err := r.Q(ctx).CountBookings(ctx, db.CountBookingsParams{
		TenantID: string(tenantID),
		Column2:  sql.NullString{String: query, Valid: true},
		Column3:  sql.NullString{String: query, Valid: true},
		Column4:  sql.NullString{String: query, Valid: true},
		Column5:  status,
		Status:   status,
	})
	if err != nil {
		return nil, 0, err
	}

	readModels := make([]domain.BookingReadModel, len(rows))
	for i, row := range rows {
		var cargoWeight *float64
		if row.CargoWeight.Valid {
			val := row.CargoWeight.Float64
			cargoWeight = &val
		}

		var notes string
		if row.Notes.Valid {
			notes = row.Notes.String
		}

		var customerCompany string
		if row.CustomerCompany.Valid {
			customerCompany = row.CustomerCompany.String
		}

		readModels[i] = domain.BookingReadModel{
			ID:               row.ID,
			BookingNumber:    row.BookingNumber,
			CustomerID:       row.CustomerID,
			CustomerName:     row.CustomerName,
			CustomerCompany:  customerCompany,
			RouteID:          row.RouteID,
			RouteSource:      row.RouteSource,
			RouteDestination: row.RouteDestination,
			PickupDate:       row.PickupDate,
			VehicleType:      row.VehicleType,
			Passengers:       row.Passengers,
			CargoWeight:      cargoWeight,
			Price:            row.Price,
			Notes:            notes,
			Status:           row.Status,
			CreatedAt:        row.CreatedAt,
			UpdatedAt:        row.UpdatedAt,
		}
	}

	return readModels, count, nil
}

func (r *bookingRepository) Delete(ctx context.Context, id aggregate.BookingID, tenantID shared.TenantID) error {
	err := r.Q(ctx).DeleteBooking(ctx, db.DeleteBookingParams{
		ID:       string(id),
		TenantID: string(tenantID),
	})
	return err
}
