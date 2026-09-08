package application

import (
	"context"
	"errors"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/trip/domain"
	"transport-app/internal/trip/domain/aggregate"
)

// TripResponseDTO represents the read model of a trip returned to handlers.
type TripResponseDTO struct {
	ID                        string     `json:"id"`
	TripNumber                string     `json:"trip_number"`
	BookingID                 *string    `json:"booking_id"`
	DriverID                  *string    `json:"driver_id"`
	DriverDisplayID           string     `json:"driver_display_id"`
	DriverFirstName           string     `json:"driver_first_name"`
	DriverLastName            string     `json:"driver_last_name"`
	VehicleID                 *string    `json:"vehicle_id"`
	VehicleRegistrationNumber string     `json:"vehicle_registration_number"`
	VehicleNumber             string     `json:"vehicle_number"`
	RouteID                   string     `json:"route_id"`
	RouteSource               string     `json:"route_source"`
	RouteDestination          string     `json:"route_destination"`
	DepartureTime             time.Time  `json:"departure_time"`
	ArrivalTime               *time.Time `json:"arrival_time"`
	Status                    string     `json:"status"`
	Remarks                   string     `json:"remarks"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
	StartedAt                 *time.Time `json:"started_at"`
	ReachedPickupAt           *time.Time `json:"reached_pickup_at"`
	InTransitAt               *time.Time `json:"in_transit_at"`
	DeliveredAt               *time.Time `json:"delivered_at"`
	CompletedAt               *time.Time `json:"completed_at"`
	CloseOdometer             *float64   `json:"close_odometer"`
}

// VehicleRegistration returns VehicleRegistrationNumber for template compatibility.
func (t TripResponseDTO) VehicleRegistration() string {
	return t.VehicleRegistrationNumber
}

// GetTripQuery parameters.
type GetTripQuery struct {
	TripID   aggregate.TripID
	TenantID shared.TenantID
}

// GetTripUseCase retrieves a trip aggregate read model.
type GetTripUseCase struct {
	uow ports.UnitOfWork
}

// NewGetTripUseCase creates a new GetTripUseCase.
func NewGetTripUseCase(uow ports.UnitOfWork) *GetTripUseCase {
	return &GetTripUseCase{uow: uow}
}

// Execute retrieves a trip by ID and returns its read model DTO.
func (uc *GetTripUseCase) Execute(ctx context.Context, query GetTripQuery) (TripResponseDTO, error) {
	var dto TripResponseDTO
	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Trips().(domain.TripRepository)
		if !ok {
			return errors.New("failed to retrieve trip repository")
		}

		t, err := repo.GetReadModel(txCtx, query.TripID, query.TenantID)
		if err != nil {
			return err
		}

		dto = TripResponseDTO{
			ID:                        t.ID,
			TripNumber:                t.TripNumber,
			BookingID:                 t.BookingID,
			DriverID:                  t.DriverID,
			DriverDisplayID:           t.DriverDisplayID,
			DriverFirstName:           t.DriverFirstName,
			DriverLastName:            t.DriverLastName,
			VehicleID:                 t.VehicleID,
			VehicleRegistrationNumber: t.VehicleRegistrationNumber,
			VehicleNumber:             t.VehicleNumber,
			RouteID:                   t.RouteID,
			RouteSource:               t.RouteSource,
			RouteDestination:          t.RouteDestination,
			DepartureTime:             t.DepartureTime,
			ArrivalTime:               t.ArrivalTime,
			Status:                    t.Status,
			Remarks:                   t.Remarks,
			CreatedAt:                 t.CreatedAt,
			UpdatedAt:                 t.UpdatedAt,
			StartedAt:                 t.StartedAt,
			ReachedPickupAt:           t.ReachedPickupAt,
			InTransitAt:               t.InTransitAt,
			DeliveredAt:               t.DeliveredAt,
			CompletedAt:               t.CompletedAt,
			CloseOdometer:             t.CloseOdometer,
		}
		return nil
	})
	return dto, err
}
