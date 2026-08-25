package test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
)

func TestSQLiteRepo_DriverCRUD(t *testing.T) {
	db := NewTestDB(t)
	repo := NewTestRepo(t, db)
	ctx := context.Background()

	driver := domain.Driver{
		ID:            domain.DriverID("driver-test-001"),
		DriverID:      "DRV001",
		FirstName:     "John",
		LastName:      "Driver",
		Phone:         "555-1234",
		LicenseNumber: "LIC123",
		Status:        domain.DriverAvailable,
	}

	created, err := repo.CreateDriver(ctx, driver)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	found, err := repo.GetDriverByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "John", found.FirstName)

	err = repo.DeleteDriver(ctx, created.ID)
	require.NoError(t, err)
}

func TestSQLiteRepo_VehicleCRUD(t *testing.T) {
	db := NewTestDB(t)
	repo := NewTestRepo(t, db)
	ctx := context.Background()

	vehicle := domain.Vehicle{
		ID:                 domain.VehicleID("vehicle-test-001"),
		RegistrationNumber: "ABC-1234",
		VehicleNumber:      "V100",
		VehicleType:        domain.VehicleTypeTruck,
		Capacity:           20,
		FuelType:           domain.FuelTypeDiesel,
		Status:             domain.VehicleAvailable,
	}

	created, err := repo.CreateVehicle(ctx, vehicle)
	require.NoError(t, err)

	found, err := repo.GetVehicleByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "V100", found.VehicleNumber)
}

func TestSQLiteRepo_CustomerCRUD(t *testing.T) {
	db := NewTestDB(t)
	repo := NewTestRepo(t, db)
	ctx := ContextWithTestTenant(context.Background())

	customer := domain.Customer{
		Name:  "Test Customer",
		Phone: "555-9999",
	}

	created, err := repo.CreateCustomer(ctx, customer)
	require.NoError(t, err)

	found, err := repo.GetCustomerByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "Test Customer", found.Name)

	count, err := repo.CountCustomers(ctx, "")
	require.NoError(t, err)
	require.True(t, count >= 1)
}

func TestSQLiteRepo_RouteCRUD(t *testing.T) {
	db := NewTestDB(t)
	repo := NewTestRepo(t, db)
	ctx := context.Background()

	route := domain.Route{
		ID:          domain.RouteID("route-test-001"),
		Source:      "Mumbai",
		Destination: "Delhi",
		Distance:    1400,
	}

	created, err := repo.CreateRoute(ctx, route)
	require.NoError(t, err)

	found, err := repo.GetRouteByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "Mumbai", found.Source)
	require.Equal(t, "Delhi", found.Destination)
}

func TestSQLiteRepo_BookingCRUD(t *testing.T) {
	db := NewTestDB(t)
	repo := NewTestRepo(t, db)
	ctx := context.Background()

	customer, _ := repo.CreateCustomer(ctx, domain.Customer{Name: "Cust", Phone: "555"})
	route, _ := repo.CreateRoute(ctx, domain.Route{ID: domain.RouteID("route-test-002"), Source: "A", Destination: "B"})

	booking := domain.Booking{
		ID:            domain.BookingID("booking-test-001"),
		BookingNumber: "BK001",
		CustomerID:    customer.ID,
		RouteID:       route.ID,
		Price:         10000,
		Status:        domain.BookingPending,
	}

	created, err := repo.CreateBooking(ctx, booking)
	require.NoError(t, err)
	require.NotEmpty(t, created.BookingNumber)

	found, err := repo.GetBookingByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, domain.BookingPending, found.Status)
}

func TestSQLiteRepo_TripCRUD(t *testing.T) {
	db := NewTestDB(t)
	repo := NewTestRepo(t, db)
	ctx := context.Background()

	route, _ := repo.CreateRoute(ctx, domain.Route{ID: domain.RouteID("route-test-003"), Source: "A", Destination: "B"})

	trip := domain.Trip{
		ID:      domain.TripID("trip-test-001"),
		RouteID: route.ID,
		Status:  domain.TripDraft,
	}

	created, err := repo.CreateTrip(ctx, trip)
	require.NoError(t, err)

	found, err := repo.GetTripByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, domain.TripDraft, found.Status)

	count, err := repo.CountTrips(ctx, "", string(domain.TripDraft))
	require.NoError(t, err)
	require.True(t, count >= 1)
}

func TestSQLiteRepo_InvoiceAndPayment(t *testing.T) {
	db := NewTestDB(t)
	repo := NewTestRepo(t, db)
	ctx := context.Background()

	customer, _ := repo.CreateCustomer(ctx, domain.Customer{Name: "Cust", Phone: "555"})

	invoice := domain.Invoice{
		ID:            domain.InvoiceID("invoice-test-001"),
		InvoiceNumber: "INV-001",
		CustomerID:    customer.ID,
		Subtotal:      10000,
		Tax:           1800,
		Total:         11800,
		PaymentStatus: domain.PaymentStatusPending,
	}

	createdInv, err := repo.CreateInvoice(ctx, invoice)
	require.NoError(t, err)
	require.Equal(t, "INV-001", createdInv.InvoiceNumber)

	foundInv, err := repo.GetInvoiceByID(ctx, createdInv.ID)
	require.NoError(t, err)
	require.Equal(t, "Cust", foundInv.CustomerName)

	// Create payment
	payment := domain.Payment{
		InvoiceID:   createdInv.ID,
		Amount:      5000,
		Method:      domain.PaymentMethodCash,
		PaymentDate: time.Now(),
	}

	createdPay, err := repo.CreatePayment(ctx, payment)
	require.NoError(t, err)

	foundPay, err := repo.GetPaymentByID(ctx, createdPay.ID)
	require.NoError(t, err)
	require.Equal(t, 5000.0, foundPay.Amount)

	// Check payment sum
	sum, err := repo.SumPaymentsByInvoice(ctx, createdInv.ID)
	require.NoError(t, err)
	require.Equal(t, 5000.0, sum)
}
