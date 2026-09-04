package converters

import (
	"database/sql"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/trip/domain/aggregate"
)

type SQLTripModel struct {
	ID              string         `json:"id"`
	TripNumber      string         `json:"trip_number"`
	BookingID       sql.NullString `json:"booking_id"`
	DriverID        sql.NullString `json:"driver_id"`
	VehicleID       sql.NullString `json:"vehicle_id"`
	RouteID         string         `json:"route_id"`
	DepartureTime   time.Time      `json:"departure_time"`
	ArrivalTime     sql.NullTime   `json:"arrival_time"`
	Status          string         `json:"status"`
	Remarks         sql.NullString `json:"remarks"`
	TenantID        string         `json:"tenant_id"`
	Version         int64          `json:"version"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	StartedAt       sql.NullTime   `json:"started_at"`
	ReachedPickupAt sql.NullTime   `json:"reached_pickup_at"`
	InTransitAt     sql.NullTime   `json:"in_transit_at"`
	DeliveredAt     sql.NullTime   `json:"delivered_at"`
	CompletedAt     sql.NullTime   `json:"completed_at"`
	IdempotencyKey  sql.NullString `json:"idempotency_key"`
}

func MapToAggregate(m SQLTripModel) *aggregate.TripAggregate {
	var bookingID *string
	if m.BookingID.Valid {
		val := m.BookingID.String
		bookingID = &val
	}

	var driverID *string
	if m.DriverID.Valid {
		val := m.DriverID.String
		driverID = &val
	}

	var vehicleID *string
	if m.VehicleID.Valid {
		val := m.VehicleID.String
		vehicleID = &val
	}

	var arrivalTime *time.Time
	if m.ArrivalTime.Valid {
		val := m.ArrivalTime.Time
		arrivalTime = &val
	}

	var remarks string
	if m.Remarks.Valid {
		remarks = m.Remarks.String
	}

	var startedAt *time.Time
	if m.StartedAt.Valid {
		val := m.StartedAt.Time
		startedAt = &val
	}

	var reachedPickupAt *time.Time
	if m.ReachedPickupAt.Valid {
		val := m.ReachedPickupAt.Time
		reachedPickupAt = &val
	}

	var inTransitAt *time.Time
	if m.InTransitAt.Valid {
		val := m.InTransitAt.Time
		inTransitAt = &val
	}

	var deliveredAt *time.Time
	if m.DeliveredAt.Valid {
		val := m.DeliveredAt.Time
		deliveredAt = &val
	}

	var completedAt *time.Time
	if m.CompletedAt.Valid {
		val := m.CompletedAt.Time
		completedAt = &val
	}

	return &aggregate.TripAggregate{
		ID:              aggregate.TripID(m.ID),
		TenantID:        shared.TenantID(m.TenantID),
		TripNumber:      m.TripNumber,
		BookingID:       bookingID,
		DriverID:        driverID,
		VehicleID:       vehicleID,
		RouteID:         m.RouteID,
		DepartureTime:   m.DepartureTime,
		ArrivalTime:     arrivalTime,
		Status:          aggregate.TripStatus(m.Status),
		Remarks:         remarks,
		StartedAt:       startedAt,
		ReachedPickupAt: reachedPickupAt,
		InTransitAt:     inTransitAt,
		DeliveredAt:     deliveredAt,
		CompletedAt:     completedAt,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
		Version:         m.Version,
		IdempotencyKey:  m.IdempotencyKey.String,
	}
}

func MapToPersistence(agg *aggregate.TripAggregate) SQLTripModel {
	var bookingID sql.NullString
	if agg.BookingID != nil {
		bookingID = sql.NullString{String: *agg.BookingID, Valid: true}
	}

	var driverID sql.NullString
	if agg.DriverID != nil {
		driverID = sql.NullString{String: *agg.DriverID, Valid: true}
	}

	var vehicleID sql.NullString
	if agg.VehicleID != nil {
		vehicleID = sql.NullString{String: *agg.VehicleID, Valid: true}
	}

	var arrivalTime sql.NullTime
	if agg.ArrivalTime != nil {
		arrivalTime = sql.NullTime{Time: *agg.ArrivalTime, Valid: true}
	}

	var remarks sql.NullString
	if agg.Remarks != "" {
		remarks = sql.NullString{String: agg.Remarks, Valid: true}
	}

	var startedAt sql.NullTime
	if agg.StartedAt != nil {
		startedAt = sql.NullTime{Time: *agg.StartedAt, Valid: true}
	}

	var reachedPickupAt sql.NullTime
	if agg.ReachedPickupAt != nil {
		reachedPickupAt = sql.NullTime{Time: *agg.ReachedPickupAt, Valid: true}
	}

	var inTransitAt sql.NullTime
	if agg.InTransitAt != nil {
		inTransitAt = sql.NullTime{Time: *agg.InTransitAt, Valid: true}
	}

	var deliveredAt sql.NullTime
	if agg.DeliveredAt != nil {
		deliveredAt = sql.NullTime{Time: *agg.DeliveredAt, Valid: true}
	}

	var completedAt sql.NullTime
	if agg.CompletedAt != nil {
		completedAt = sql.NullTime{Time: *agg.CompletedAt, Valid: true}
	}

	var idempotencyKey sql.NullString
	if agg.IdempotencyKey != "" {
		idempotencyKey = sql.NullString{String: agg.IdempotencyKey, Valid: true}
	}

	return SQLTripModel{
		ID:              string(agg.ID),
		TripNumber:      agg.TripNumber,
		BookingID:       bookingID,
		DriverID:        driverID,
		VehicleID:       vehicleID,
		RouteID:         agg.RouteID,
		DepartureTime:   agg.DepartureTime,
		ArrivalTime:     arrivalTime,
		Status:          string(agg.Status),
		Remarks:         remarks,
		TenantID:        string(agg.TenantID),
		Version:         agg.Version,
		CreatedAt:       agg.CreatedAt,
		UpdatedAt:       agg.UpdatedAt,
		StartedAt:       startedAt,
		ReachedPickupAt: reachedPickupAt,
		InTransitAt:     inTransitAt,
		DeliveredAt:     deliveredAt,
		CompletedAt:     completedAt,
		IdempotencyKey:  idempotencyKey,
	}
}
