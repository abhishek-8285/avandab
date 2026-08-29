package test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"transport-app/internal/domain"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func TestDriverVehicleService_Validations(t *testing.T) {
	db := NewTestDB(t)
	services := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// --- DRIVER TESTS ---

	futureExpiry := time.Now().Add(365 * 24 * time.Hour).Format("2006-01-02")
	pastExpiry := time.Now().Add(-24 * time.Hour).Format("2006-01-02")

	// 1. Create a driver with an expired license (should fail)
	_, err := services.Drivers.CreateDriver(
		ctx,
		"John",
		"Doe",
		"555-1001",
		"john@doe.com",
		"123 St",
		"LIC-EXP-1",
		pastExpiry,
		5,
		nil,
		nil,
		nil,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "driver license has already expired")

	// 2. Create a driver successfully
	d1, err := services.Drivers.CreateDriver(
		ctx,
		"John",
		"Doe",
		"555-1001",
		"john@doe.com",
		"123 St",
		"LIC-OK-1",
		futureExpiry,
		5,
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.NotEmpty(t, d1.ID)

	// 3. Create another driver with the same phone (should fail)
	_, err = services.Drivers.CreateDriver(
		ctx,
		"Jane",
		"Doe",
		"555-1001",
		"jane@doe.com",
		"123 St",
		"LIC-OK-2",
		futureExpiry,
		3,
		nil,
		nil,
		nil,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "phone number 555-1001 is already registered")

	// --- VEHICLE TESTS ---

	// 1. Create a vehicle successfully
	v1, err := services.Vehicles.CreateVehicle(
		ctx,
		"MH-12-AB-1234",
		"TRK-001",
		domain.VehicleTypeTruck,
		15,
		domain.FuelTypeDiesel,
		futureExpiry,
		futureExpiry,
		futureExpiry,
		"10000.5",
	)
	require.NoError(t, err)

	// 2. Create another vehicle with the same registration (should fail)
	_, err = services.Vehicles.CreateVehicle(
		ctx,
		"MH-12-AB-1234",
		"TRK-002",
		domain.VehicleTypeTruck,
		15,
		domain.FuelTypeDiesel,
		futureExpiry,
		futureExpiry,
		futureExpiry,
		"",
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "vehicle with registration number MH-12-AB-1234 already exists")

	// 3. Deletion validation: create a trip and assign vehicle
	cust, err := services.Customers.CreateCustomer(ctx, "Test Customer", "Test Corp", "555-0000", "", "", "", "")
	require.NoError(t, err)

	route, err := services.Routes.CreateRoute(ctx, "Source", "Destination", 100, 2.5, 5000, "")
	require.NoError(t, err)

	booking, err := services.Bookings.CreateBooking(ctx, service.CreateBookingRequest{
		CustomerID:  cust.ID,
		RouteID:     route.ID,
		PickupDate:  futureExpiry,
		VehicleType: domain.VehicleTypeTruck,
		Passengers:  1,
		Price:       6000,
		Notes:       "fast",
	})
	require.NoError(t, err)

	trip, err := services.Trips.CreateTrip(ctx, service.CreateTripRequest{
		BookingID:     &booking.ID,
		RouteID:       route.ID,
		DepartureTime: futureExpiry,
		Remarks:       "remark",
	})
	require.NoError(t, err)

	// Assign vehicle to trip
	_, err = services.Trips.AssignVehicle(ctx, trip.ID, v1.ID)
	require.NoError(t, err)

	// Move trip from draft to scheduled status so it triggers the conflict check
	_, err = services.Trips.ScheduleTrip(ctx, trip.ID)
	require.NoError(t, err)

	// Try to delete the vehicle (should fail because it's assigned to an active trip)
	err = services.Vehicles.DeleteVehicle(ctx, v1.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot delete vehicle because it is assigned to trip")
}
