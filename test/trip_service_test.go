package test

import (
	"context"
	"testing"
	"transport-app/internal/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
	"transport-app/internal/service"
)

func TestTripService_CreateTrip(t *testing.T) {
	db := NewTestDB(t)
	svc := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// Create prerequisites
	driver, err := svc.Drivers.CreateDriver(ctx,
		"John", "Driver", "555-1234", "", "", "LIC123", "2026-12-31", 5, nil, nil, nil)
	require.NoError(t, err)

	vehicle, err := svc.Vehicles.CreateVehicle(ctx,
		"ABC-1234", "V100", domain.VehicleTypeTruck, 20, domain.FuelTypeDiesel, "2027-01-01", "2027-01-01", "2027-01-01", "0")
	require.NoError(t, err)

	route, err := svc.Routes.CreateRoute(ctx, "Mumbai", "Delhi", 1400, 24, 15000, "")
	require.NoError(t, err)

	// Create trip
	trip, err := svc.Trips.CreateTrip(ctx, service.CreateTripRequest{
		RouteID:       route.ID,
		DepartureTime: "2026-08-10T08:00:00",
	})
	require.NoError(t, err)
	assert.Equal(t, domain.TripDraft, trip.Status)

	// Assign driver
	_, err = svc.Trips.AssignDriver(ctx, trip.ID, driver.ID)
	require.NoError(t, err)

	// Assign vehicle
	_, err = svc.Trips.AssignVehicle(ctx, trip.ID, vehicle.ID)
	require.NoError(t, err)

	// Verify assignments
	tripUpdated, err := svc.Trips.GetTrip(ctx, trip.ID)
	require.NoError(t, err)
	assert.Equal(t, driver.ID, *tripUpdated.DriverID)
	assert.Equal(t, vehicle.ID, *tripUpdated.VehicleID)
}

func TestTripService_AssignDriver_BusyDriver(t *testing.T) {
	db := NewTestDB(t)
	svc := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	driver, err := svc.Drivers.CreateDriver(ctx,
		"John", "Driver", "555-1234", "", "", "LIC123", "2026-12-31", 5, nil, nil, nil)
	require.NoError(t, err)

	route, _ := svc.Routes.CreateRoute(ctx, "A", "B", 100, 2, 5000, "")

	trip1, _ := svc.Trips.CreateTrip(ctx, service.CreateTripRequest{RouteID: route.ID, DepartureTime: "2026-08-10T08:00:00"})
	trip2, _ := svc.Trips.CreateTrip(ctx, service.CreateTripRequest{RouteID: route.ID, DepartureTime: "2026-08-10T08:00:00"})

	// Assign to first trip
	_, err = svc.Trips.AssignDriver(ctx, trip1.ID, driver.ID)
	require.NoError(t, err)

	// Assign trip1 to scheduled so driver is busy
	_, _ = svc.Trips.ScheduleTrip(ctx, trip1.ID)

	// Should fail: driver already busy
	_, err = svc.Trips.AssignDriver(ctx, trip2.ID, driver.ID)
	assert.Error(t, err)
}

func TestTripService_Cancel_Then_Complete(t *testing.T) {
	db := NewTestDB(t)
	svc := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	route, _ := svc.Routes.CreateRoute(ctx, "A", "B", 100, 2, 5000, "")
	trip, _ := svc.Trips.CreateTrip(ctx, service.CreateTripRequest{RouteID: route.ID, DepartureTime: "2026-08-10T08:00:00"})

	// Cancel trip
	_, err := svc.Trips.CancelTrip(ctx, trip.ID)
	require.NoError(t, err)

	// Cancelled trips are immutable - can't complete
	_, err = svc.Trips.CompleteTrip(ctx, trip.ID)
	assert.Error(t, err)
}

func TestTripService_CompletedTrip_Immutable(t *testing.T) {
	db := NewTestDB(t)
	svc := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	route, _ := svc.Routes.CreateRoute(ctx, "A", "B", 100, 2, 5000, "")
	driver, _ := svc.Drivers.CreateDriver(ctx, "John", "Driver", "555-1234", "", "", "LIC123", "2026-12-31", 5, nil, nil, nil)
	trip, _ := svc.Trips.CreateTrip(ctx, service.CreateTripRequest{RouteID: route.ID, DepartureTime: "2026-08-10T08:00:00"})

	// Go through full lifecycle: draft → scheduled → assigned → started → completed
	_, err := svc.Trips.ScheduleTrip(ctx, trip.ID)
	require.NoError(t, err)

	_, err = svc.Trips.AssignDriver(ctx, trip.ID, driver.ID)
	require.NoError(t, err)

	_, err = svc.Trips.StartTrip(ctx, trip.ID)
	require.NoError(t, err)

	// Complete the trip
	_, err = svc.Trips.CompleteTrip(ctx, trip.ID)
	require.NoError(t, err)

	// Can't cancel completed trip
	_, err = svc.Trips.CancelTrip(ctx, trip.ID)
	assert.Error(t, err)
}

func TestBookingService_CreateAndConfirm(t *testing.T) {
	db := NewTestDB(t)
	svc := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	customer, err := svc.Customers.CreateCustomer(ctx, "Test Customer", "Acme Corp", "555-0100", "test@acme.com", "27AABCU9603R1ZX", "123 Main St", "")
	require.NoError(t, err)

	route, err := svc.Routes.CreateRoute(ctx, "Mumbai", "Delhi", 1400, 24, 15000, "")
	require.NoError(t, err)

	booking, err := svc.Bookings.CreateBooking(ctx, service.CreateBookingRequest{
		CustomerID:  customer.ID,
		RouteID:     route.ID,
		PickupDate:  "2026-08-10",
		VehicleType: domain.VehicleTypeTruck,
		Passengers:  2,
		Price:       15000,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.BookingPending, booking.Status)

	// Confirm booking
	booking, err = svc.Bookings.ConfirmBooking(ctx, booking.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.BookingConfirmed, booking.Status)
}

func TestInvoiceService_GenerateFromTrip(t *testing.T) {
	db := NewTestDB(t)
	svc := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	customer, _ := svc.Customers.CreateCustomer(ctx, "Test Customer", "", "555-0100", "", "", "", "")
	route, _ := svc.Routes.CreateRoute(ctx, "A", "B", 100, 2, 5000, "")

	booking, _ := svc.Bookings.CreateBooking(ctx, service.CreateBookingRequest{
		CustomerID:  customer.ID,
		RouteID:     route.ID,
		PickupDate:  "2026-08-10",
		VehicleType: domain.VehicleTypeTruck,
		Passengers:  2,
		Price:       10000,
	})
	_, _ = svc.Bookings.ConfirmBooking(ctx, booking.ID)

	trip, _ := svc.Trips.CreateTrip(ctx, service.CreateTripRequest{
		RouteID:       route.ID,
		BookingID:     &booking.ID,
		DepartureTime: "2026-08-10T08:00:00",
	})
	_, _ = svc.Trips.ScheduleTrip(ctx, trip.ID)
	_, _ = svc.Trips.CompleteTrip(ctx, trip.ID)

	invoice, err := svc.Invoices.GenerateInvoiceFromTrip(ctx, trip.ID)
	require.NoError(t, err)
	assert.Contains(t, invoice.InvoiceNumber, "INV")
	assert.Equal(t, 10000.0, invoice.Total)

	balance, err := svc.Invoices.GetBalance(ctx, invoice.ID)
	require.NoError(t, err)
	assert.Equal(t, 10000.0, balance)

	// Record payment
	_, err = svc.Payments.RecordPayment(ctx, invoice.ID, 5000, domain.PaymentMethodCash, "", "", "2026-08-10")
	require.NoError(t, err)

	balance, err = svc.Invoices.GetBalance(ctx, invoice.ID)
	require.NoError(t, err)
	assert.Equal(t, 5000.0, balance)
}

func TestDriverService_ListDrivers(t *testing.T) {
	db := NewTestDB(t)
	svc := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	_, err := svc.Drivers.CreateDriver(ctx, "John", "Driver", "555-1234", "", "", "LIC123", "2026-12-31", 5, nil, nil, nil)
	require.NoError(t, err)

	drivers, total, err := svc.Drivers.ListDrivers(ctx, "", "available", 100, 0)
	require.NoError(t, err)
	assert.True(t, total >= 1)
	assert.Equal(t, 1, len(drivers))
}

func TestVehicleService_ListVehicles(t *testing.T) {
	db := NewTestDB(t)
	svc := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	_, err := svc.Vehicles.CreateVehicle(ctx, "ABC-1234", "V100", domain.VehicleTypeTruck, 20, domain.FuelTypeDiesel, "2027-01-01", "2027-01-01", "2027-01-01", "0")
	require.NoError(t, err)

	vehicles, total, err := svc.Vehicles.ListVehicles(ctx, "", "available", 100, 0)
	require.NoError(t, err)
	assert.True(t, total >= 1)
	assert.Equal(t, 1, len(vehicles))
}

func TestRepository_TypeCheck(t *testing.T) {
	db := NewTestDB(t)
	repo := NewTestRepo(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	var _ service.Store = repo

	r, err := repo.CreateRoute(ctx, domain.Route{Source: "Test", Destination: "End"})
	require.NoError(t, err)
	require.Equal(t, "Test", r.Source)
}
