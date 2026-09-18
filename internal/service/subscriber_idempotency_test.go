package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
	bookingevents "transport-app/internal/domain/booking"
	tripevents "transport-app/internal/domain/trip"
	"transport-app/internal/events"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func TestSubscriber_Idempotency_DuplicateDelivery(t *testing.T) {
	dbConn, svcs, bus := setupComplianceTestDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))
	customer, err := svcs.Customers.CreateCustomer(ctx, "Idem Customer", "", "9999988771", "", "", "", "")
	require.NoError(t, err)
	route, err := svcs.Routes.CreateRoute(ctx, "Delhi", "Jaipur", 250, 5, 5000, "")
	require.NoError(t, err)
	driver, err := svcs.Drivers.CreateDriver(ctx, "Ramesh", "Kumar", "9999988772", "", "", "DL-IDEM-001", "2029-01-01", 5, nil, nil, nil)
	require.NoError(t, err)
	_, err = svcs.Vehicles.CreateVehicle(ctx, "DL01IDEM01", "DL01IDEM01", domain.VehicleTypeTruck, 10000, domain.FuelTypeDiesel, "2028-01-01", "2028-01-01", "2028-01-01", "0")
	require.NoError(t, err)
	pickup := time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02T15:04:05")
	booking, err := svcs.Bookings.CreateBooking(ctx, service.CreateBookingRequest{
		CustomerID:  customer.ID,
		RouteID:     route.ID,
		PickupDate:  pickup,
		VehicleType: domain.VehicleTypeTruck,
		Passengers:  1,
		Price:       5000,
	})
	require.NoError(t, err)
	_, err = svcs.Bookings.ConfirmBooking(ctx, booking.ID)
	require.NoError(t, err)
	var tripCount int
	require.NoError(t, dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM trips WHERE booking_id = ?`, string(booking.ID)).Scan(&tripCount))
	require.Equal(t, 1, tripCount)
	now := time.Now().UTC()
	require.NoError(t, bus.Publish(ctx, events.Event{
		Type: events.BookingConfirmed,
		Payload: bookingevents.BookingConfirmedEvent{
			TenantID:    shared.TenantID("tenant-1"),
			BookingID:   booking.ID,
			ConfirmedAt: now,
			OccurredAt:  now,
		},
	}))
	require.NoError(t, dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM trips WHERE booking_id = ?`, string(booking.ID)).Scan(&tripCount))
	require.Equal(t, 1, tripCount)
	booking2, err := svcs.Bookings.CreateBooking(ctx, service.CreateBookingRequest{
		CustomerID:  customer.ID,
		RouteID:     route.ID,
		PickupDate:  pickup,
		VehicleType: domain.VehicleTypeTruck,
		Passengers:  1,
		Price:       5000,
	})
	require.NoError(t, err)
	_, err = svcs.Bookings.ConfirmBooking(ctx, booking2.ID)
	require.NoError(t, err)
	var tripID2 string
	require.NoError(t, dbConn.QueryRowContext(ctx, `SELECT id FROM trips WHERE booking_id = ? ORDER BY created_at DESC LIMIT 1`, string(booking2.ID)).Scan(&tripID2))
	require.NotEmpty(t, tripID2)
	completedAt := time.Now().UTC()
	require.NoError(t, bus.Publish(ctx, events.Event{
		Type: events.TripCompleted,
		Payload: tripevents.TripCompletedEvent{
			TripID:      domain.TripID(tripID2),
			TenantID:    shared.TenantID("tenant-1"),
			CompletedAt: completedAt,
			OccurredAt:  completedAt,
		},
	}))
	var invCount int
	require.NoError(t, dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoices WHERE trip_id = ?`, tripID2).Scan(&invCount))
	require.Equal(t, 1, invCount)
	require.NoError(t, bus.Publish(ctx, events.Event{
		Type: events.TripCompleted,
		Payload: tripevents.TripCompletedEvent{
			TripID:      domain.TripID(tripID2),
			TenantID:    shared.TenantID("tenant-1"),
			CompletedAt: completedAt,
			OccurredAt:  completedAt,
		},
	}))
	require.NoError(t, dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoices WHERE trip_id = ?`, tripID2).Scan(&invCount))
	require.Equal(t, 1, invCount)
	booking3, err := svcs.Bookings.CreateBooking(ctx, service.CreateBookingRequest{
		CustomerID:  customer.ID,
		RouteID:     route.ID,
		PickupDate:  pickup,
		VehicleType: domain.VehicleTypeTruck,
		Passengers:  1,
		Price:       5000,
	})
	require.NoError(t, err)
	_, err = svcs.Bookings.ConfirmBooking(ctx, booking3.ID)
	require.NoError(t, err)
	var tripID3 string
	require.NoError(t, dbConn.QueryRowContext(ctx, `SELECT id FROM trips WHERE booking_id = ? ORDER BY created_at DESC LIMIT 1`, string(booking3.ID)).Scan(&tripID3))
	require.NotEmpty(t, tripID3)
	_, err = dbConn.ExecContext(ctx, `UPDATE trips SET driver_id = ? WHERE id = ?`, string(driver.ID), tripID3)
	require.NoError(t, err)
	deliveredAt := time.Now().UTC()
	payload := map[string]interface{}{
		"trip_id":      domain.TripID(tripID3),
		"tenant_id":    string(shared.TenantID("tenant-1")),
		"booking_id":   booking3.ID,
		"driver_id":    driver.ID,
		"pod_url":      "https://vault.test/epod.jpg",
		"delivered_at": deliveredAt,
		"occurred_at":  deliveredAt,
	}
	_ = bus.Publish(ctx, events.Event{Type: events.TripDelivered, Payload: payload})
	var invCount3 int
	require.NoError(t, dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoices WHERE trip_id = ?`, tripID3).Scan(&invCount3))
	require.Equal(t, 1, invCount3)
	var settleCount int
	require.NoError(t, dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM driver_settlements WHERE trip_id = ?`, tripID3).Scan(&settleCount))
	require.Equal(t, 1, settleCount)
	var ledgerCount int
	require.NoError(t, dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM driver_ledger_entries WHERE trip_id = ?`, tripID3).Scan(&ledgerCount))
	require.Greater(t, ledgerCount, 0)
	require.NoError(t, bus.Publish(ctx, events.Event{Type: events.TripDelivered, Payload: payload}))
	require.NoError(t, dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoices WHERE trip_id = ?`, tripID3).Scan(&invCount3))
	require.Equal(t, 1, invCount3)
	require.NoError(t, dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM driver_settlements WHERE trip_id = ?`, tripID3).Scan(&settleCount))
	require.Equal(t, 1, settleCount)
	var ledgerAfter int
	require.NoError(t, dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM driver_ledger_entries WHERE trip_id = ?`, tripID3).Scan(&ledgerAfter))
	require.Equal(t, ledgerCount, ledgerAfter)
}
