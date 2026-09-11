package application

import (
	"context"

	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// FindBackhaulMatchesQuery parameters.
type FindBackhaulMatchesQuery struct {
	TripID   string
	TenantID shared.TenantID
	RadiusKm float64
}

// CreateBackhaulOfferCommand parameters.
type CreateBackhaulOfferCommand struct {
	TripID      string
	TenantID    shared.TenantID
	BookingID   string
	OfferedRate float64
}

// BackhaulUseCase coordinates return-corridor matching and dispatch offers.
type BackhaulUseCase struct {
	service *service.BackhaulService
}

// NewBackhaulUseCase creates a new BackhaulUseCase.
func NewBackhaulUseCase(svc *service.BackhaulService) *BackhaulUseCase {
	return &BackhaulUseCase{service: svc}
}

// FindMatches returns backhaul candidates for a completing trip.
func (uc *BackhaulUseCase) FindMatches(ctx context.Context, q FindBackhaulMatchesQuery) ([]service.BackhaulMatchDTO, error) {
	return uc.service.FindBackhaulMatches(ctx, string(q.TenantID), q.TripID, q.RadiusKm)
}

// CreateOffer dispatches an offer for a driver on a completing trip.
func (uc *BackhaulUseCase) CreateOffer(ctx context.Context, cmd CreateBackhaulOfferCommand) (*service.BackhaulOfferResponse, error) {
	return uc.service.CreateBackhaulOffer(ctx, string(cmd.TenantID), service.BackhaulOfferRequest{
		TripID:      cmd.TripID,
		BookingID:   cmd.BookingID,
		OfferedRate: cmd.OfferedRate,
	})
}
