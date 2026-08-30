package aggregate

import (
	"errors"
	"fmt"
	"time"

	"transport-app/internal/shared"
)

type TripID string
type TripStatus string

const (
	TripDraft         TripStatus = "draft"
	TripScheduled     TripStatus = "scheduled"
	TripAssigned      TripStatus = "assigned"
	TripStarted       TripStatus = "started"
	TripReachedPickup TripStatus = "reached_pickup"
	TripInTransit     TripStatus = "in_transit"
	TripDelivered     TripStatus = "delivered"
	TripCompleted     TripStatus = "completed"
	TripCancelled     TripStatus = "cancelled"
)

type StopType string

const (
	StopTypePickup     StopType = "pickup"
	StopTypeDrop       StopType = "drop"
	StopTypeWaypoint   StopType = "waypoint"
	StopTypeHubTransit StopType = "hub_transit"
)

type StopStatus string

const (
	StopStatusPending   StopStatus = "pending"
	StopStatusEnRoute   StopStatus = "en_route"
	StopStatusArrived   StopStatus = "arrived"
	StopStatusServicing StopStatus = "servicing"
	StopStatusCompleted StopStatus = "completed"
	StopStatusSkipped   StopStatus = "skipped"
	StopStatusFailed    StopStatus = "failed"
)

// TripStop represents an individual stop/leg within a multi-stop transport trip.
type TripStop struct {
	ID              string
	TenantID        shared.TenantID
	TripID          TripID
	StopSequence    int
	StopType        StopType
	LocationName    string
	Address         string
	Latitude        *float64
	Longitude       *float64
	GeofenceRadiusM float64
	ConsigneeName   string
	ConsigneePhone  string
	ConsigneeEmail  string
	PlannedArrival  *time.Time
	ActualArrival   *time.Time
	ActualDeparture *time.Time
	Status          StopStatus
	OTPRequired     bool
	OTPCode         string
	OTPExpiresAt    *time.Time
	OTPVerifiedAt   *time.Time
	PODRequired     bool
	PODURL          string
	PODSignatureURL string
	PODVerifiedAt   *time.Time
	PODNotes        string
	FailureReason   string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TripAggregate represents the consistency boundary for a single transport Trip.
type TripAggregate struct {
	ID            TripID
	TenantID      shared.TenantID
	TripNumber    string
	BookingID     *string
	DriverID      *string
	VehicleID     *string
	RouteID       string
	DepartureTime time.Time
	ArrivalTime   *time.Time
	Status        TripStatus
	Remarks       string

	// Timeline timestamps
	StartedAt       *time.Time
	ReachedPickupAt *time.Time
	InTransitAt     *time.Time
	DeliveredAt     *time.Time
	CompletedAt     *time.Time

	// Multi-Stop legs
	Stops []TripStop

	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int64

	events []any
}

// NewTripAggregate creates a new TripAggregate in 'draft' status.
func NewTripAggregate(
	id TripID,
	tenantID shared.TenantID,
	tripNumber string,
	bookingID *string,
	routeID string,
	departureTime time.Time,
	remarks string,
	now time.Time,
) *TripAggregate {
	t := &TripAggregate{
		ID:            id,
		TenantID:      tenantID,
		TripNumber:    tripNumber,
		BookingID:     bookingID,
		RouteID:       routeID,
		DepartureTime: departureTime,
		Status:        TripDraft,
		Remarks:       remarks,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	t.RecordEvent(TripCreatedEvent{
		TripID:     id,
		TenantID:   tenantID,
		TripNumber: tripNumber,
		OccurredAt: now,
	})

	return t
}

// AddStop appends a stop to the trip.
func (t *TripAggregate) AddStop(stop TripStop) {
	if stop.StopSequence == 0 {
		stop.StopSequence = len(t.Stops) + 1
	}
	if stop.GeofenceRadiusM <= 0 {
		stop.GeofenceRadiusM = 100.0
	}
	if stop.Status == "" {
		stop.Status = StopStatusPending
	}
	t.Stops = append(t.Stops, stop)
}

// AllStopsCompleted returns true if every stop in the trip is completed (or skipped).
func (t *TripAggregate) AllStopsCompleted() bool {
	if len(t.Stops) == 0 {
		return true
	}
	for _, s := range t.Stops {
		if s.Status != StopStatusCompleted && s.Status != StopStatusSkipped {
			return false
		}
		// If POD is required, ensure it is verified
		if s.PODRequired && (s.PODURL == "" && s.PODVerifiedAt == nil) {
			return false
		}
	}
	return true
}

// ReachStop records arrival at a specific stop.
func (t *TripAggregate) ReachStop(stopID string, now time.Time) error {
	for i := range t.Stops {
		if t.Stops[i].ID != stopID {
			continue
		}
		// Enforce sequence: cannot reach stop i if previous non-skipped stop is not completed
		for j := 0; j < i; j++ {
			if t.Stops[j].Status != StopStatusCompleted && t.Stops[j].Status != StopStatusSkipped {
				return fmt.Errorf("cannot reach stop %d (#%s); previous stop %d is not completed", t.Stops[i].StopSequence, stopID, t.Stops[j].StopSequence)
			}
		}
		t.Stops[i].Status = StopStatusArrived
		t.Stops[i].ActualArrival = &now
		t.Stops[i].UpdatedAt = now
		t.UpdatedAt = now

		t.RecordEvent(TripStopArrivedEvent{
			TripID:       t.ID,
			TenantID:     t.TenantID,
			StopID:       stopID,
			StopSequence: t.Stops[i].StopSequence,
			OccurredAt:   now,
		})
		return nil
	}
	return errors.New("stop not found")
}

// VerifyStopOTP verifies consignee delivery OTP for a stop.
func (t *TripAggregate) VerifyStopOTP(stopID string, otp string, now time.Time) error {
	for i := range t.Stops {
		if t.Stops[i].ID != stopID {
			continue
		}
		if !t.Stops[i].OTPRequired {
			return nil
		}
		if t.Stops[i].OTPCode != "" && t.Stops[i].OTPCode != otp {
			return errors.New("invalid stop OTP")
		}
		if t.Stops[i].OTPExpiresAt != nil && t.Stops[i].OTPExpiresAt.Before(now) {
			return errors.New("stop OTP expired")
		}
		t.Stops[i].OTPVerifiedAt = &now
		t.Stops[i].UpdatedAt = now
		t.UpdatedAt = now
		return nil
	}
	return errors.New("stop not found")
}

// SubmitStopPOD uploads and verifies proof of delivery for a specific stop.
func (t *TripAggregate) SubmitStopPOD(stopID string, podURL, signatureURL, notes string, now time.Time) error {
	for i := range t.Stops {
		if t.Stops[i].ID != stopID {
			continue
		}
		if podURL == "" && signatureURL == "" {
			return errors.New("pod photo or signature url is required")
		}
		t.Stops[i].PODURL = podURL
		t.Stops[i].PODSignatureURL = signatureURL
		t.Stops[i].PODNotes = notes
		t.Stops[i].PODVerifiedAt = &now
		t.Stops[i].UpdatedAt = now
		t.UpdatedAt = now

		t.RecordEvent(TripStopPODVerifiedEvent{
			TripID:       t.ID,
			TenantID:     t.TenantID,
			StopID:       stopID,
			StopSequence: t.Stops[i].StopSequence,
			PODURL:       podURL,
			OccurredAt:   now,
		})
		return nil
	}
	return errors.New("stop not found")
}

// CompleteStop marks a stop as completed after verifying POD/OTP prerequisites.
func (t *TripAggregate) CompleteStop(stopID string, now time.Time) error {
	for i := range t.Stops {
		if t.Stops[i].ID != stopID {
			continue
		}
		if t.Stops[i].OTPRequired && t.Stops[i].OTPVerifiedAt == nil {
			return errors.New("cannot complete stop: OTP verification required")
		}
		if t.Stops[i].PODRequired && (t.Stops[i].PODURL == "" && t.Stops[i].PODVerifiedAt == nil) {
			return errors.New("cannot complete stop: POD submission required")
		}
		t.Stops[i].Status = StopStatusCompleted
		t.Stops[i].ActualDeparture = &now
		t.Stops[i].UpdatedAt = now
		t.UpdatedAt = now

		t.RecordEvent(TripStopCompletedEvent{
			TripID:       t.ID,
			TenantID:     t.TenantID,
			StopID:       stopID,
			StopSequence: t.Stops[i].StopSequence,
			OccurredAt:   now,
		})
		return nil
	}
	return errors.New("stop not found")
}

// Schedule updates status to scheduled.
func (t *TripAggregate) Schedule(now time.Time) error {
	if t.Status != TripDraft {
		return errors.New("only draft trips can be scheduled")
	}
	t.Status = TripScheduled
	t.UpdatedAt = now
	t.RecordEvent(TripScheduledEvent{
		TripID:     t.ID,
		TenantID:   t.TenantID,
		OccurredAt: now,
	})
	return nil
}

// AssignDriver associates a driver.
func (t *TripAggregate) AssignDriver(driverID string, now time.Time) error {
	if t.Status == TripCompleted || t.Status == TripCancelled {
		return errors.New("cannot assign driver to completed or cancelled trip")
	}
	t.DriverID = &driverID
	t.Status = TripAssigned
	t.UpdatedAt = now
	t.RecordEvent(TripAssignedEvent{
		TripID:     t.ID,
		TenantID:   t.TenantID,
		DriverID:   driverID,
		OccurredAt: now,
	})
	return nil
}

// AssignVehicle associates a vehicle.
func (t *TripAggregate) AssignVehicle(vehicleID string, now time.Time) error {
	if t.DriverID == nil || *t.DriverID == "" {
		return errors.New("driver must be assigned before vehicle")
	}
	if t.Status == TripCompleted || t.Status == TripCancelled {
		return errors.New("cannot assign vehicle to completed or cancelled trip")
	}
	t.VehicleID = &vehicleID
	t.UpdatedAt = now
	driverID := ""
	if t.DriverID != nil {
		driverID = *t.DriverID
	}
	t.RecordEvent(TripAssignedEvent{
		TripID:     t.ID,
		TenantID:   t.TenantID,
		DriverID:   driverID,
		VehicleID:  vehicleID,
		OccurredAt: now,
	})
	return nil
}

// Start moves status to started.
func (t *TripAggregate) Start(now time.Time) error {
	if t.Status != TripAssigned && t.Status != TripScheduled {
		return errors.New("trip must be scheduled or assigned to start")
	}
	t.Status = TripStarted
	t.StartedAt = &now
	t.UpdatedAt = now
	t.RecordEvent(TripStartedEvent{
		TripID:     t.ID,
		TenantID:   t.TenantID,
		OccurredAt: now,
	})
	return nil
}

// ReachPickup moves status to reached_pickup.
func (t *TripAggregate) ReachPickup(now time.Time) error {
	if t.Status != TripStarted {
		return errors.New("trip must be started before reaching pickup")
	}
	t.Status = TripReachedPickup
	t.ReachedPickupAt = &now
	t.UpdatedAt = now
	t.RecordEvent(TripReachedPickupEvent{
		TripID:     t.ID,
		TenantID:   t.TenantID,
		OccurredAt: now,
	})
	return nil
}

// StartTransit moves status to in_transit.
func (t *TripAggregate) StartTransit(now time.Time) error {
	if t.Status != TripReachedPickup {
		return errors.New("trip must have reached pickup before going in transit")
	}
	t.Status = TripInTransit
	t.InTransitAt = &now
	t.UpdatedAt = now
	t.RecordEvent(TripInTransitEvent{
		TripID:     t.ID,
		TenantID:   t.TenantID,
		OccurredAt: now,
	})
	return nil
}

// Deliver moves status to delivered.
func (t *TripAggregate) Deliver(now time.Time) error {
	if t.Status != TripInTransit && t.Status != TripReachedPickup {
		return errors.New("trip must be in transit or reached pickup before being delivered")
	}
	// Multi-stop check: if stops are defined, all stops must be completed before overall trip delivery
	if len(t.Stops) > 0 && !t.AllStopsCompleted() {
		return errors.New("cannot deliver trip: incomplete stops remain")
	}
	t.Status = TripDelivered
	t.DeliveredAt = &now
	t.ArrivalTime = &now
	t.UpdatedAt = now
	t.RecordEvent(TripDeliveredEvent{
		TripID:     t.ID,
		TenantID:   t.TenantID,
		OccurredAt: now,
	})
	return nil
}

// Complete completes the trip execution.
func (t *TripAggregate) Complete(now time.Time) error {
	if t.Status == TripCompleted {
		return nil
	}
	if t.Status != TripDelivered {
		return errors.New("only delivered trips can be completed")
	}
	// Multi-stop check: cannot complete trip if any stop is incomplete
	if len(t.Stops) > 0 && !t.AllStopsCompleted() {
		return errors.New("cannot complete trip: incomplete stops remain")
	}
	t.Status = TripCompleted
	t.CompletedAt = &now
	t.UpdatedAt = now
	t.RecordEvent(TripCompletedEvent{
		TripID:     t.ID,
		TenantID:   t.TenantID,
		OccurredAt: now,
	})
	return nil
}

// Cancel cancels the trip.
func (t *TripAggregate) Cancel(now time.Time) error {
	if t.Status == TripCompleted {
		return errors.New("completed trips cannot be cancelled")
	}
	t.Status = TripCancelled
	t.UpdatedAt = now
	t.RecordEvent(TripCancelledEvent{
		TripID:     t.ID,
		TenantID:   t.TenantID,
		OccurredAt: now,
	})
	return nil
}

// Events returns collected events.
func (t *TripAggregate) Events() []any {
	return t.events
}

// ClearEvents clears the events.
func (t *TripAggregate) ClearEvents() {
	t.events = nil
}

// RecordEvent records a domain event.
func (t *TripAggregate) RecordEvent(event any) {
	t.events = append(t.events, event)
}

// Event Definitions
type TripCreatedEvent struct {
	TripID     TripID
	TenantID   shared.TenantID
	TripNumber string
	OccurredAt time.Time
}

type TripScheduledEvent struct {
	TripID     TripID
	TenantID   shared.TenantID
	OccurredAt time.Time
}

type TripAssignedEvent struct {
	TripID     TripID
	TenantID   shared.TenantID
	DriverID   string
	VehicleID  string
	OccurredAt time.Time
}

type TripStartedEvent struct {
	TripID     TripID
	TenantID   shared.TenantID
	OccurredAt time.Time
}

type TripReachedPickupEvent struct {
	TripID     TripID
	TenantID   shared.TenantID
	OccurredAt time.Time
}

type TripInTransitEvent struct {
	TripID     TripID
	TenantID   shared.TenantID
	OccurredAt time.Time
}

type TripDeliveredEvent struct {
	TripID     TripID
	TenantID   shared.TenantID
	OccurredAt time.Time
}

type TripCompletedEvent struct {
	TripID     TripID
	TenantID   shared.TenantID
	OccurredAt time.Time
}

type TripCancelledEvent struct {
	TripID     TripID
	TenantID   shared.TenantID
	OccurredAt time.Time
}

type TripStopArrivedEvent struct {
	TripID       TripID
	TenantID     shared.TenantID
	StopID       string
	StopSequence int
	OccurredAt   time.Time
}

type TripStopCompletedEvent struct {
	TripID       TripID
	TenantID     shared.TenantID
	StopID       string
	StopSequence int
	OccurredAt   time.Time
}

type TripStopPODVerifiedEvent struct {
	TripID       TripID
	TenantID     shared.TenantID
	StopID       string
	StopSequence int
	PODURL       string
	OccurredAt   time.Time
}
