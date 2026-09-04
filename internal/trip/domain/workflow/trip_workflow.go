package workflow

import (
	"context"

	bookingApp "transport-app/internal/booking/application"
	bookingAggregate "transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/shared"
	tripApp "transport-app/internal/trip/application"
	tripAggregate "transport-app/internal/trip/domain/aggregate"
)

// TripWorkflow orchestrates the trip lifecycle, coordinating with
// the booking module when trips complete.
type TripWorkflow struct {
	scheduleUC        *tripApp.ScheduleTripUseCase
	startUC           *tripApp.StartTripUseCase
	completeUC        *tripApp.CompleteTripUseCase
	cancelUC          *tripApp.CancelTripUseCase
	completeBookingUC *bookingApp.CompleteBookingUseCase
}

// NewTripWorkflow creates a new TripWorkflow.
func NewTripWorkflow(
	scheduleUC *tripApp.ScheduleTripUseCase,
	startUC *tripApp.StartTripUseCase,
	completeUC *tripApp.CompleteTripUseCase,
	cancelUC *tripApp.CancelTripUseCase,
	completeBookingUC *bookingApp.CompleteBookingUseCase,
) *TripWorkflow {
	return &TripWorkflow{
		scheduleUC:        scheduleUC,
		startUC:           startUC,
		completeUC:        completeUC,
		cancelUC:          cancelUC,
		completeBookingUC: completeBookingUC,
	}
}

// Schedule schedules a draft trip.
func (w *TripWorkflow) Schedule(ctx context.Context, tripID tripAggregate.TripID, tenantID shared.TenantID) error {
	return w.scheduleUC.Execute(ctx, tripApp.ScheduleTripCommand{
		TripID:   tripID,
		TenantID: tenantID,
	})
}

// Start starts a scheduled/assigned trip.
func (w *TripWorkflow) Start(ctx context.Context, tripID tripAggregate.TripID, tenantID shared.TenantID) error {
	return w.startUC.Execute(ctx, tripApp.StartTripCommand{
		TripID:   tripID,
		TenantID: tenantID,
	})
}

// Complete completes a started trip and triggers booking completion.
func (w *TripWorkflow) Complete(ctx context.Context, tripID tripAggregate.TripID, tenantID shared.TenantID) error {
	return w.completeUC.Execute(ctx, tripApp.CompleteTripCommand{
		TripID:   tripID,
		TenantID: tenantID,
	})
}

// Cancel cancels a trip.
func (w *TripWorkflow) Cancel(ctx context.Context, tripID tripAggregate.TripID, tenantID shared.TenantID) error {
	return w.cancelUC.Execute(ctx, tripApp.CancelTripCommand{
		TripID:   tripID,
		TenantID: tenantID,
	})
}

// CanComplete returns whether the trip is eligible for completion.
// Must match TripAggregate.Complete: only delivered trips can be completed.
func (w *TripWorkflow) CanComplete(status tripAggregate.TripStatus) bool {
	return status == tripAggregate.TripDelivered
}

// CanCancel returns whether the trip is eligible for cancellation.
func (w *TripWorkflow) CanCancel(status tripAggregate.TripStatus) bool {
	return status != tripAggregate.TripCompleted
}

// CompleteBookingForTrip completes the associated booking after a trip completes.
// This enables cross-module coordination without tight coupling.
func (w *TripWorkflow) CompleteBookingForTrip(ctx context.Context, bookingID string, tenantID shared.TenantID) error {
	if w.completeBookingUC == nil {
		return nil
	}
	return w.completeBookingUC.Execute(ctx, bookingApp.CompleteBookingCommand{
		BookingID: bookingAggregate.BookingID(bookingID),
		TenantID:  tenantID,
	})
}
