package sql

import (
	"database/sql"
	"time"

	"transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/shared"
)

// SQLBookingModel represents the raw persistence model for SQL database.
type SQLBookingModel struct {
	ID             string
	BookingNumber  string
	CustomerID     string
	PickupDate     time.Time
	RouteID        string
	VehicleType    string
	Passengers     int64
	CargoWeight    sql.NullFloat64
	Price          float64
	Notes          sql.NullString
	Status         string
	TenantID       string
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	IdempotencyKey sql.NullString
}

// MapToAggregate converts a SQL persistence model to the domain BookingAggregate.
func MapToAggregate(m SQLBookingModel) *aggregate.BookingAggregate {
	var cargoWeight *float64
	if m.CargoWeight.Valid {
		val := m.CargoWeight.Float64
		cargoWeight = &val
	}

	var notes string
	if m.Notes.Valid {
		notes = m.Notes.String
	}

	priceMoney := shared.FloatToMoney(m.Price, "INR")

	return &aggregate.BookingAggregate{
		ID:             aggregate.BookingID(m.ID),
		TenantID:       shared.TenantID(m.TenantID),
		BookingNumber:  m.BookingNumber,
		CustomerID:     m.CustomerID,
		RouteID:        m.RouteID,
		PickupDate:     m.PickupDate,
		VehicleType:    m.VehicleType,
		Passengers:     m.Passengers,
		CargoWeight:    cargoWeight,
		Price:          priceMoney,
		Notes:          notes,
		Status:         aggregate.BookingStatus(m.Status),
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
		Version:        m.Version,
		IdempotencyKey: m.IdempotencyKey.String,
	}
}

// MapToPersistence converts the domain BookingAggregate to the SQL persistence model.
func MapToPersistence(agg *aggregate.BookingAggregate) SQLBookingModel {
	var cargoWeight sql.NullFloat64
	if agg.CargoWeight != nil {
		cargoWeight = sql.NullFloat64{Float64: *agg.CargoWeight, Valid: true}
	}

	var notes sql.NullString
	if agg.Notes != "" {
		notes = sql.NullString{String: agg.Notes, Valid: true}
	}

	var idempotencyKey sql.NullString
	if agg.IdempotencyKey != "" {
		idempotencyKey = sql.NullString{String: agg.IdempotencyKey, Valid: true}
	}

	return SQLBookingModel{
		ID:             string(agg.ID),
		BookingNumber:  agg.BookingNumber,
		CustomerID:     agg.CustomerID,
		RouteID:        agg.RouteID,
		PickupDate:     agg.PickupDate,
		VehicleType:    agg.VehicleType,
		Passengers:     agg.Passengers,
		CargoWeight:    cargoWeight,
		Price:          agg.Price.MoneyToFloat(),
		Notes:          notes,
		Status:         string(agg.Status),
		TenantID:       string(agg.TenantID),
		Version:        agg.Version,
		CreatedAt:      agg.CreatedAt,
		UpdatedAt:      agg.UpdatedAt,
		IdempotencyKey: idempotencyKey,
	}
}
