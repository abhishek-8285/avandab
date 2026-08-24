package service

import (
	"context"
	"fmt"
	"time"

	"transport-app/internal/domain"
	bookingevents "transport-app/internal/domain/booking"
	"transport-app/internal/events"
	"transport-app/internal/repository"
)

// BookingService handles booking management and workflow.
type BookingService struct {
	baseService
}

// CreateBookingRequest contains the fields needed to create a booking.
type CreateBookingRequest struct {
	CustomerID  domain.CustomerID
	RouteID     domain.RouteID
	PickupDate  string
	VehicleType domain.VehicleType
	Passengers  int64
	CargoWeight *float64
	Price       float64
	Notes       string
}

// CreateBooking creates a new booking.
func (s *BookingService) CreateBooking(ctx context.Context, req CreateBookingRequest) (domain.Booking, error) {
	if req.CustomerID == "" {
		return domain.Booking{}, fmt.Errorf("customer is required")
	}
	if req.RouteID == "" {
		return domain.Booking{}, fmt.Errorf("route is required")
	}
	if req.Passengers < 1 && req.CargoWeight == nil {
		return domain.Booking{}, fmt.Errorf("passengers or cargo weight is required")
	}

	if _, err := s.store.GetCustomerByID(ctx, req.CustomerID); err != nil {
		return domain.Booking{}, domain.ErrCustomerNotFound
	}

	if _, err := s.store.GetRouteByID(ctx, req.RouteID); err != nil {
		return domain.Booking{}, domain.ErrRouteNotFound
	}

	pickupDate, err := parseDateTime(req.PickupDate)
	if err != nil {
		return domain.Booking{}, fmt.Errorf("invalid pickup date: must be YYYY-MM-DD or YYYY-MM-DDTHH:MM:SS")
	}

	if req.Price <= 0 {
		route, _ := s.store.GetRouteByID(ctx, req.RouteID)
		req.Price = route.StandardFare
	}

	booking := domain.Booking{
		ID:            domain.BookingID(generateID()),
		BookingNumber: s.generateBookingNumber(ctx),
		CustomerID:    req.CustomerID,
		PickupDate:    pickupDate,
		RouteID:       req.RouteID,
		VehicleType:   req.VehicleType,
		Passengers:    req.Passengers,
		CargoWeight:   req.CargoWeight,
		Price:         req.Price,
		Notes:         strPtr(req.Notes),
		Status:        domain.BookingPending,
	}

	created, err := s.store.CreateBooking(ctx, booking)
	if err != nil {
		return domain.Booking{}, err
	}

	s.log.Info("booking created", "booking_id", created.ID, "booking_number", created.BookingNumber)
	s.logAudit(ctx, nil, "create", "bookings", string(created.ID), nil, nil)
	s.events.Publish(ctx, events.Event{
		Type: events.BookingCreated,
		Payload: bookingevents.BookingCreatedEvent{
			BookingID:     created.ID,
			BookingNumber: created.BookingNumber,
			CustomerID:    created.CustomerID,
			RouteID:       created.RouteID,
			PickupDate:    created.PickupDate,
			OccurredAt:    time.Now(),
		},
	})
	return created, nil
}

// GetBooking retrieves a booking by ID.
func (s *BookingService) GetBooking(ctx context.Context, id domain.BookingID) (repository.BookingWithJoins, error) {
	return s.store.GetBookingByID(ctx, id)
}

// GetBookingByNumber retrieves a booking by its number.
func (s *BookingService) GetBookingByNumber(ctx context.Context, number string) (repository.BookingWithJoins, error) {
	return s.store.GetBookingByNumber(ctx, number)
}

// ListBookings retrieves bookings with search and pagination.
func (s *BookingService) ListBookings(ctx context.Context, query, status string, limit, offset int) ([]repository.BookingWithJoins, int64, error) {
	bookings, err := s.store.SearchBookings(ctx, query, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.store.CountBookings(ctx, query, status)
	if err != nil {
		return nil, 0, err
	}
	return bookings, total, nil
}

// ListBookingsByCustomer returns the most recent bookings for a customer.
func (s *BookingService) ListBookingsByCustomer(ctx context.Context, customerID domain.CustomerID, limit int) ([]repository.BookingWithJoins, error) {
	if limit <= 0 {
		limit = 5
	}
	return s.store.ListBookingsByCustomer(ctx, customerID, limit)
}

// UpdateBooking updates an existing booking.
func (s *BookingService) UpdateBooking(ctx context.Context, id domain.BookingID, req CreateBookingRequest, notes string) (domain.Booking, error) {
	b, err := s.store.GetBookingByID(ctx, id)
	if err != nil {
		return domain.Booking{}, domain.ErrBookingNotFound
	}

	if b.Status == domain.BookingCompleted || b.Status == domain.BookingCancelled {
		return domain.Booking{}, domain.ErrTripImmutable
	}

	if req.CustomerID != "" {
		if _, err := s.store.GetCustomerByID(ctx, req.CustomerID); err != nil {
			return domain.Booking{}, domain.ErrCustomerNotFound
		}
		b.CustomerID = req.CustomerID
	}

	if req.RouteID != "" {
		if _, err := s.store.GetRouteByID(ctx, req.RouteID); err != nil {
			return domain.Booking{}, domain.ErrRouteNotFound
		}
		b.RouteID = req.RouteID
	}

	pickup, err := parseDateTime(req.PickupDate)
	if err == nil {
		b.PickupDate = pickup
	}
	b.VehicleType = req.VehicleType
	b.Passengers = req.Passengers
	b.CargoWeight = req.CargoWeight
	b.Price = req.Price
	b.Notes = strPtr(notes)

	s.logAudit(ctx, nil, "update", "bookings", string(id), nil, nil)
	return s.store.UpdateBooking(ctx, b.Booking)
}

// ConfirmBooking confirms a booking (sets status to confirmed).
func (s *BookingService) ConfirmBooking(ctx context.Context, id domain.BookingID) (domain.Booking, error) {
	b, err := s.store.GetBookingByID(ctx, id)
	if err != nil {
		return domain.Booking{}, domain.ErrBookingNotFound
	}

	if err := b.CanConfirm(); err != nil {
		return domain.Booking{}, err
	}

	updated, err := s.store.UpdateBookingStatus(ctx, id, domain.BookingConfirmed)
	if err != nil {
		return domain.Booking{}, err
	}

	s.log.Info("booking confirmed", "booking_id", id)
	s.logAudit(ctx, nil, "approve", "bookings", string(id), nil, nil)
	s.events.Publish(ctx, events.Event{
		Type: events.BookingConfirmed,
		Payload: bookingevents.BookingConfirmedEvent{
			BookingID:   id,
			ConfirmedAt: time.Now(),
			OccurredAt:  time.Now(),
		},
	})
	return updated, nil
}

// CancelBooking cancels a booking.
func (s *BookingService) CancelBooking(ctx context.Context, id domain.BookingID) (domain.Booking, error) {
	b, err := s.store.GetBookingByID(ctx, id)
	if err != nil {
		return domain.Booking{}, domain.ErrBookingNotFound
	}

	if err := b.CanCancel(); err != nil {
		return domain.Booking{}, err
	}

	updated, err := s.store.UpdateBookingStatus(ctx, id, domain.BookingCancelled)
	if err != nil {
		return domain.Booking{}, err
	}

	s.log.Info("booking cancelled", "booking_id", id)
	s.logAudit(ctx, nil, "cancel", "bookings", string(id), nil, nil)
	return updated, nil
}

// CompleteBooking marks a booking as completed.
func (s *BookingService) CompleteBooking(ctx context.Context, id domain.BookingID) (domain.Booking, error) {
	b, err := s.store.GetBookingByID(ctx, id)
	if err != nil {
		return domain.Booking{}, domain.ErrBookingNotFound
	}

	if b.Status == domain.BookingCancelled {
		return domain.Booking{}, domain.ErrCancelledTripImmutable
	}

	updated, err := s.store.UpdateBookingStatus(ctx, id, domain.BookingCompleted)
	if err != nil {
		return domain.Booking{}, err
	}

	s.log.Info("booking completed", "booking_id", id)
	s.logAudit(ctx, nil, "complete", "bookings", string(id), nil, nil)
	return updated, nil
}

// DeleteBooking deletes a booking.
func (s *BookingService) DeleteBooking(ctx context.Context, id domain.BookingID) error {
	b, err := s.store.GetBookingByID(ctx, id)
	if err != nil {
		return domain.ErrBookingNotFound
	}

	if err := b.CanDelete(); err != nil {
		return err
	}

	s.logAudit(ctx, nil, "delete", "bookings", string(id), nil, nil)
	return s.store.DeleteBooking(ctx, id)
}

// parseDateTime parses a date/datetime string.
func parseDateTime(s string) (time.Time, error) {
	layouts := []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	var t time.Time
	var err error
	for _, layout := range layouts {
		t, err = time.Parse(layout, s)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, err
}
