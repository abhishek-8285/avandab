package trip

import (
	"time"

	"transport-app/internal/domain/types"
	"transport-app/internal/shared"
)

// TripCreatedEvent is emitted when a new trip is created.
type TripCreatedEvent struct {
	TripID        types.TripID
	TenantID      shared.TenantID
	TripNumber    string
	RouteID       types.RouteID
	DriverID      *types.DriverID
	VehicleID     *types.VehicleID
	DepartureTime time.Time
	OccurredAt    time.Time
}

// TripScheduledEvent is emitted when a trip is scheduled.
type TripScheduledEvent struct {
	TripID     types.TripID
	Status     TripStatus
	OccurredAt time.Time
}

// TripAssignedEvent is emitted when a driver or vehicle is assigned to a trip.
type TripAssignedEvent struct {
	TripID     types.TripID
	DriverID   *types.DriverID
	VehicleID  *types.VehicleID
	OccurredAt time.Time
}

// TripStartedEvent is emitted when a trip is started.
type TripStartedEvent struct {
	TripID     types.TripID
	TenantID   shared.TenantID
	StartedAt  time.Time
	OccurredAt time.Time
}

// TripCompletedEvent is emitted when a trip is completed.
type TripCompletedEvent struct {
	TripID      types.TripID
	TenantID    shared.TenantID
	CompletedAt time.Time
	OccurredAt  time.Time
}

// TripCancelledEvent is emitted when a trip is cancelled.
type TripCancelledEvent struct {
	TripID      types.TripID
	CancelledAt time.Time
	OccurredAt  time.Time
}

// TripDeletedEvent is emitted when a trip is deleted.
type TripDeletedEvent struct {
	TripID     types.TripID
	OccurredAt time.Time
}
