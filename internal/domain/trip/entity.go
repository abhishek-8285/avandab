package trip

import (
	"fmt"
	"time"

	"transport-app/internal/domain/types"
)

// Trip represents a scheduled transport trip.
type Trip struct {
	ID                    types.TripID
	TripNumber            string
	BookingID             *types.BookingID
	DriverID              *types.DriverID
	VehicleID             *types.VehicleID
	RouteID               types.RouteID
	DepartureTime         time.Time
	ArrivalTime           *time.Time
	Status                TripStatus
	EWayBillRef           *string
	PODURL                *string
	FinalSettlementAmount float64
	Remarks               *string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// TripStatus represents the lifecycle status of a trip.
type TripStatus string

const (
	TripDraft     TripStatus = "draft"
	TripScheduled TripStatus = "scheduled"
	TripAssigned  TripStatus = "assigned"
	TripStarted   TripStatus = "started"
	TripInTransit TripStatus = "in_transit"
	TripDelivered TripStatus = "delivered"
	TripCompleted TripStatus = "completed"
	TripCancelled TripStatus = "cancelled"
)

// ActiveTripStatuses are statuses that indicate a trip is currently in progress.
var ActiveTripStatuses = []TripStatus{
	TripScheduled,
	TripAssigned,
	TripStarted,
	TripInTransit,
	TripDelivered,
}

// CanSchedule validates that a trip can be scheduled (must be in draft status).
func (t Trip) CanSchedule() error {
	if t.Status != TripDraft {
		return fmt.Errorf("only draft trips can be scheduled; current status: %s", t.Status)
	}
	return nil
}

// CanStart validates that a trip can be started (must be assigned).
func (t Trip) CanStart() error {
	if t.Status != TripAssigned {
		return fmt.Errorf("only assigned trips can be started; current status: %s", t.Status)
	}
	return nil
}

// CanDeliver validates that e-POD can be captured and trip delivered.
func (t Trip) CanDeliver() error {
	if t.Status != TripStarted && t.Status != TripInTransit {
		return fmt.Errorf("only active/started/in_transit trips can be marked delivered; current status: %s", t.Status)
	}
	return nil
}

// CanComplete validates that a trip can be completed (only from delivered).
// Must match TripAggregate.Complete and TripWorkflow.CanComplete.
func (t Trip) CanComplete() error {
	if t.Status != TripDelivered {
		return fmt.Errorf("only delivered trips can be completed; current status: %s", t.Status)
	}
	return nil
}

// CanCancel validates that a trip can be cancelled (must not be completed).
func (t Trip) CanCancel() error {
	if t.Status == TripCompleted {
		return ErrCompletedTripImmutable
	}
	return nil
}
