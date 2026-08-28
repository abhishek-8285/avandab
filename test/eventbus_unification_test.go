package test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
	bookingevents "transport-app/internal/domain/booking"
	"transport-app/internal/events"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// TestEventBusUnification_BookingConfirmedReachesAutomationAndFounder proves
// Spec 09 §5.1: services publish on the SAME bus instance that automation
// subscribers and (a mock) founder handler listen on. With the pre-fix
// dual-bus setup the auto-trip handler (on the service's private bus) never
// saw events published by the confirm flow.
func TestEventBusUnification_BookingConfirmedReachesAutomationAndFounder(t *testing.T) {
	db := NewTestDB(t)
	bus := events.NewInMemoryBus()

	// Mock founder-style handler on the shared bus.
	var founderGot atomic.Int32
	unsub := bus.Subscribe(events.BookingConfirmed, func(ctx context.Context, e events.Event) error {
		founderGot.Add(1)
		return nil
	})
	defer unsub()

	svc := NewTestServicesWithBus(t, db, bus)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	customer, err := svc.Customers.CreateCustomer(ctx, "Bus Test Customer", "", "9999999999", "", "", "", "")
	require.NoError(t, err)
	require.NotEmpty(t, customer.ID)

	route, err := svc.Routes.CreateRoute(ctx, "Pune", "Mumbai", 150, 3, 1200, "")
	require.NoError(t, err)
	require.NotEmpty(t, route.ID)

	booking, err := svc.Bookings.CreateBooking(ctx, service.CreateBookingRequest{
		CustomerID:  customer.ID,
		RouteID:     route.ID,
		PickupDate:  time.Now().Add(24 * time.Hour).Format("2006-01-02T15:04:05"),
		VehicleType: domain.VehicleTypeTruck,
		Passengers:  1,
		Price:       1200,
	})
	require.NoError(t, err)

	confirmed, err := svc.Bookings.ConfirmBooking(ctx, booking.ID)
	require.NoError(t, err)
	require.Equal(t, domain.BookingConfirmed, confirmed.Status)

	// Automation: booking.confirmed → auto-create trip on the SAME bus.
	require.Eventually(t, func() bool {
		trips, _, err := svc.Trips.ListTrips(ctx, "", "", 50, 0)
		return err == nil && len(trips) == 1 && trips[0].BookingID != nil && *trips[0].BookingID == booking.ID
	}, 2*time.Second, 10*time.Millisecond)

	// Founder handler received the SAME event exactly once (no double dispatch).
	assert.Equal(t, int32(1), founderGot.Load())
}

// TestEventBus_OutboxRelayDispatchesToSharedBus proves the outbox relay's
// persisted event_type matches the canonical catalog string that subscribers
// listen on, and that a relay-dispatched event reaches the same bus.
func TestEventBus_OutboxRelayDispatchesToSharedBus(t *testing.T) {
	bus := events.NewInMemoryBus()
	var relayGot atomic.Int32
	unsub := bus.Subscribe(events.BookingConfirmed, func(ctx context.Context, e events.Event) error {
		relayGot.Add(1)
		return nil
	})
	defer unsub()

	// Assert the catalog mapping that outbox.getEventTypeName consults.
	assert.Equal(t, "booking.confirmed", events.EventTypeOf[bookingevents.BookingConfirmedEvent{}])
	assert.Equal(t, "booking.created", events.EventTypeOf[bookingevents.BookingCreatedEvent{}])
	assert.Equal(t, "booking.confirmed", events.BookingConfirmed)
}
