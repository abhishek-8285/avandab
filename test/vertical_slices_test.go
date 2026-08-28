package test

import (
	"context"
	"testing"
	"time"
	"transport-app/internal/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bookingApp "transport-app/internal/booking/application"
	invoiceApp "transport-app/internal/invoice/application"
	paymentApp "transport-app/internal/payment/application"
	paymentAggregate "transport-app/internal/payment/domain/aggregate"
	tripApp "transport-app/internal/trip/application"

	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
)

// ─────────────────────────────────────────────────────────────────────────────
// Sprint 1: Booking
// ─────────────────────────────────────────────────────────────────────────────

func TestSprint1_CreateBooking(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	svc := NewTestServices(t, db)
	route, err := svc.Routes.CreateRoute(ctx, "Mumbai", "Delhi", 1400, 24, 15000, "")
	require.NoError(t, err)
	customer, err := svc.Customers.CreateCustomer(ctx, "TestCo", "TC", "555-0001", "tc@example.com", "", "", "")
	require.NoError(t, err)

	createUC := bookingApp.NewCreateBookingUseCase(sqlUoW, idGen, realClock)
	bID, err := createUC.Execute(ctx, bookingApp.CreateBookingCommand{
		TenantID:    "1",
		CustomerID:  string(customer.ID),
		RouteID:     string(route.ID),
		PickupDate:  time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		VehicleType: "Truck",
		Passengers:  2,
		Price:       15000,
		Notes:       "fragile",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, bID)
}

func TestSprint1_ConfirmAndCancelBooking(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	svc := NewTestServices(t, db)
	route, _ := svc.Routes.CreateRoute(ctx, "A", "B", 100, 2, 5000, "")
	customer, err := svc.Customers.CreateCustomer(ctx, "Confirm Co", "CC", "555-0002", "cc@example.com", "", "", "")
	require.NoError(t, err)

	createUC := bookingApp.NewCreateBookingUseCase(sqlUoW, idGen, realClock)
	confirmUC := bookingApp.NewConfirmBookingUseCase(sqlUoW, realClock)
	cancelUC := bookingApp.NewCancelBookingUseCase(sqlUoW, realClock)
	getUC := bookingApp.NewGetBookingUseCase(sqlUoW)

	bookingID, err := createUC.Execute(ctx, bookingApp.CreateBookingCommand{
		TenantID:    "1",
		CustomerID:  string(customer.ID),
		RouteID:     string(route.ID),
		PickupDate:  time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		VehicleType: "Van",
		Passengers:  1,
		Price:       5000,
	})
	require.NoError(t, err)

	// Confirm
	require.NoError(t, confirmUC.Execute(ctx, bookingApp.ConfirmBookingCommand{
		BookingID: bookingID,
		TenantID:  "1",
	}))

	res, err := getUC.Execute(ctx, bookingApp.GetBookingQuery{BookingID: bookingID, TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "confirmed", res.Status)

	// Cancel a confirmed booking
	require.NoError(t, cancelUC.Execute(ctx, bookingApp.CancelBookingCommand{
		BookingID: bookingID,
		TenantID:  "1",
	}))

	res, err = getUC.Execute(ctx, bookingApp.GetBookingQuery{BookingID: bookingID, TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "cancelled", res.Status)
}

func TestSprint1_ListBookings(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	svc := NewTestServices(t, db)
	route, _ := svc.Routes.CreateRoute(ctx, "X", "Y", 200, 3, 8000, "")
	customer, err := svc.Customers.CreateCustomer(ctx, "List Co", "LC", "555-0003", "lc@example.com", "", "", "")
	require.NoError(t, err)

	createUC := bookingApp.NewCreateBookingUseCase(sqlUoW, idGen, realClock)
	listUC := bookingApp.NewListBookingsUseCase(sqlUoW)

	for i := 0; i < 3; i++ {
		_, err := createUC.Execute(ctx, bookingApp.CreateBookingCommand{
			TenantID:    "1",
			CustomerID:  string(customer.ID),
			RouteID:     string(route.ID),
			PickupDate:  time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			VehicleType: "Bus",
			Passengers:  10,
			Price:       8000,
		})
		require.NoError(t, err)
	}

	res, err := listUC.Execute(ctx, bookingApp.ListBookingsQuery{TenantID: "1", Page: 1, Limit: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, res.Total, int64(3))
}

func TestSprint1_UpdateBooking(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	svc := NewTestServices(t, db)
	route, _ := svc.Routes.CreateRoute(ctx, "A", "B", 100, 2, 5000, "")
	customer, err := svc.Customers.CreateCustomer(ctx, "Update Co", "UC", "555-0004", "uc@example.com", "", "", "")
	require.NoError(t, err)

	createUC := bookingApp.NewCreateBookingUseCase(sqlUoW, idGen, realClock)
	updateUC := bookingApp.NewUpdateBookingUseCase(sqlUoW)
	getUC := bookingApp.NewGetBookingUseCase(sqlUoW)

	bookingID, err := createUC.Execute(ctx, bookingApp.CreateBookingCommand{
		TenantID:    "1",
		CustomerID:  string(customer.ID),
		RouteID:     string(route.ID),
		PickupDate:  time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		VehicleType: "Truck",
		Passengers:  2,
		Price:       5000,
	})
	require.NoError(t, err)

	err = updateUC.Execute(ctx, bookingApp.UpdateBookingCommand{
		BookingID:   bookingID,
		TenantID:    "1",
		CustomerID:  string(customer.ID),
		RouteID:     string(route.ID),
		PickupDate:  time.Now().Add(48 * time.Hour).Format(time.RFC3339),
		VehicleType: "Van",
		Passengers:  4,
		Price:       6000,
	})
	require.NoError(t, err)

	res, err := getUC.Execute(ctx, bookingApp.GetBookingQuery{BookingID: bookingID, TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "Van", res.VehicleType)
	assert.Equal(t, int64(4), res.Passengers)
}

func TestSprint1_CompleteBooking(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	svc := NewTestServices(t, db)
	route, _ := svc.Routes.CreateRoute(ctx, "A", "B", 100, 2, 5000, "")
	customer, err := svc.Customers.CreateCustomer(ctx, "Complete Co", "CC", "555-0005", "cc@example.com", "", "", "")
	require.NoError(t, err)

	createUC := bookingApp.NewCreateBookingUseCase(sqlUoW, idGen, realClock)
	confirmUC := bookingApp.NewConfirmBookingUseCase(sqlUoW, realClock)
	completeUC := bookingApp.NewCompleteBookingUseCase(sqlUoW, realClock)
	getUC := bookingApp.NewGetBookingUseCase(sqlUoW)

	bookingID, err := createUC.Execute(ctx, bookingApp.CreateBookingCommand{
		TenantID:    "1",
		CustomerID:  string(customer.ID),
		RouteID:     string(route.ID),
		PickupDate:  time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		VehicleType: "Truck",
		Passengers:  2,
		Price:       5000,
	})
	require.NoError(t, err)

	// Cannot complete a pending booking
	err = completeUC.Execute(ctx, bookingApp.CompleteBookingCommand{
		BookingID: bookingID,
		TenantID:  "1",
	})
	require.Error(t, err)

	// Confirm first
	require.NoError(t, confirmUC.Execute(ctx, bookingApp.ConfirmBookingCommand{
		BookingID: bookingID,
		TenantID:  "1",
	}))

	// Now complete
	require.NoError(t, completeUC.Execute(ctx, bookingApp.CompleteBookingCommand{
		BookingID: bookingID,
		TenantID:  "1",
	}))

	res, err := getUC.Execute(ctx, bookingApp.GetBookingQuery{BookingID: bookingID, TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "completed", res.Status)
}

// ─────────────────────────────────────────────────────────────────────────────
// Sprint 2: Trip
// ─────────────────────────────────────────────────────────────────────────────

func TestSprint2_CreateTripAndLifecycle(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	svc := NewTestServices(t, db)
	route, _ := svc.Routes.CreateRoute(ctx, "Mumbai", "Pune", 150, 3, 3000, "")
	driver, _ := svc.Drivers.CreateDriver(ctx, "Ali", "Khan", "111", "", "", "LIC999", "2027-01-01", 3, nil, nil, nil)
	vehicle, err := svc.Vehicles.CreateVehicle(ctx, "MH-01-XX-1234", "V200", "truck", 15, "diesel", "2027-01-01", "2027-01-01", "2027-01-01", "0")
	require.NoError(t, err)

	createUC := tripApp.NewCreateTripUseCase(sqlUoW, idGen, realClock)
	assignDriverUC := tripApp.NewAssignDriverUseCase(sqlUoW, realClock)
	assignVehicleUC := tripApp.NewAssignVehicleUseCase(sqlUoW, realClock)
	startUC := tripApp.NewStartTripUseCase(sqlUoW, realClock)
	reachPickupUC := tripApp.NewReachPickupUseCase(sqlUoW, realClock)
	startTransitUC := tripApp.NewStartTransitUseCase(sqlUoW, realClock)
	deliverUC := tripApp.NewDeliverUseCase(sqlUoW, realClock)
	completeUC := tripApp.NewCompleteTripUseCase(sqlUoW, realClock)
	getUC := tripApp.NewGetTripUseCase(sqlUoW)

	tripID, err := createUC.Execute(ctx, tripApp.CreateTripCommand{
		TenantID:      "1",
		RouteID:       string(route.ID),
		DepartureTime: time.Now().Add(1 * time.Hour),
		Remarks:       "test trip",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, tripID)

	require.NoError(t, assignDriverUC.Execute(ctx, tripApp.AssignDriverCommand{
		TripID:   tripID,
		DriverID: string(driver.ID),
		TenantID: "1",
	}))

	require.NoError(t, assignVehicleUC.Execute(ctx, tripApp.AssignVehicleCommand{
		TripID:    tripID,
		VehicleID: string(vehicle.ID),
		TenantID:  "1",
	}))

	require.NoError(t, startUC.Execute(ctx, tripApp.StartTripCommand{TripID: tripID, TenantID: "1"}))

	trip, err := getUC.Execute(ctx, tripApp.GetTripQuery{TripID: tripID, TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "started", trip.Status)
	assert.NotNil(t, trip.StartedAt)

	// Execution workflow: started -> reached_pickup -> in_transit -> delivered -> completed
	require.NoError(t, reachPickupUC.Execute(ctx, tripApp.ReachPickupCommand{TripID: tripID, TenantID: "1"}))
	trip, err = getUC.Execute(ctx, tripApp.GetTripQuery{TripID: tripID, TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "reached_pickup", trip.Status)
	assert.NotNil(t, trip.ReachedPickupAt)

	require.NoError(t, startTransitUC.Execute(ctx, tripApp.StartTransitCommand{TripID: tripID, TenantID: "1"}))
	trip, err = getUC.Execute(ctx, tripApp.GetTripQuery{TripID: tripID, TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "in_transit", trip.Status)
	assert.NotNil(t, trip.InTransitAt)

	require.NoError(t, deliverUC.Execute(ctx, tripApp.DeliverCommand{TripID: tripID, TenantID: "1"}))
	trip, err = getUC.Execute(ctx, tripApp.GetTripQuery{TripID: tripID, TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "delivered", trip.Status)
	assert.NotNil(t, trip.DeliveredAt)

	require.NoError(t, completeUC.Execute(ctx, tripApp.CompleteTripCommand{TripID: tripID, TenantID: "1"}))

	trip, err = getUC.Execute(ctx, tripApp.GetTripQuery{TripID: tripID, TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "completed", trip.Status)
	assert.NotNil(t, trip.CompletedAt)
}

func TestSprint2_CancelTrip(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	svc := NewTestServices(t, db)
	route, _ := svc.Routes.CreateRoute(ctx, "C", "D", 100, 2, 2000, "")

	createUC := tripApp.NewCreateTripUseCase(sqlUoW, idGen, realClock)
	cancelUC := tripApp.NewCancelTripUseCase(sqlUoW, realClock)
	getUC := tripApp.NewGetTripUseCase(sqlUoW)

	tripID, err := createUC.Execute(ctx, tripApp.CreateTripCommand{
		TenantID:      "1",
		RouteID:       string(route.ID),
		DepartureTime: time.Now().Add(2 * time.Hour),
	})
	require.NoError(t, err)

	require.NoError(t, cancelUC.Execute(ctx, tripApp.CancelTripCommand{TripID: tripID, TenantID: "1"}))

	trip, err := getUC.Execute(ctx, tripApp.GetTripQuery{TripID: tripID, TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "cancelled", trip.Status)
}

func TestSprint2_ScheduleTrip(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	svc := NewTestServices(t, db)
	route, _ := svc.Routes.CreateRoute(ctx, "E", "F", 120, 3, 2500, "")

	createUC := tripApp.NewCreateTripUseCase(sqlUoW, idGen, realClock)
	scheduleUC := tripApp.NewScheduleTripUseCase(sqlUoW, realClock)
	getUC := tripApp.NewGetTripUseCase(sqlUoW)

	tripID, err := createUC.Execute(ctx, tripApp.CreateTripCommand{
		TenantID:      "1",
		RouteID:       string(route.ID),
		DepartureTime: time.Now().Add(3 * time.Hour),
	})
	require.NoError(t, err)

	// Cannot start a draft trip; schedule first
	require.NoError(t, scheduleUC.Execute(ctx, tripApp.ScheduleTripCommand{
		TripID:   tripID,
		TenantID: "1",
	}))

	trip, err := getUC.Execute(ctx, tripApp.GetTripQuery{TripID: tripID, TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "scheduled", trip.Status)
}

func TestSprint2_TripExecutionTransitionErrors(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	svc := NewTestServices(t, db)
	route, _ := svc.Routes.CreateRoute(ctx, "G", "H", 80, 1, 1500, "")
	driver, _ := svc.Drivers.CreateDriver(ctx, "Bob", "Smith", "777", "", "", "LIC777", "2027-01-01", 5, nil, nil, nil)

	createUC := tripApp.NewCreateTripUseCase(sqlUoW, idGen, realClock)
	assignDriverUC := tripApp.NewAssignDriverUseCase(sqlUoW, realClock)
	startUC := tripApp.NewStartTripUseCase(sqlUoW, realClock)
	reachPickupUC := tripApp.NewReachPickupUseCase(sqlUoW, realClock)
	startTransitUC := tripApp.NewStartTransitUseCase(sqlUoW, realClock)
	deliverUC := tripApp.NewDeliverUseCase(sqlUoW, realClock)
	completeUC := tripApp.NewCompleteTripUseCase(sqlUoW, realClock)

	tripID, err := createUC.Execute(ctx, tripApp.CreateTripCommand{
		TenantID:      "1",
		RouteID:       string(route.ID),
		DepartureTime: time.Now().Add(1 * time.Hour),
	})
	require.NoError(t, err)

	// Can't reach pickup from draft
	err = reachPickupUC.Execute(ctx, tripApp.ReachPickupCommand{TripID: tripID, TenantID: "1"})
	require.Error(t, err)

	// Can't start transit from draft
	err = startTransitUC.Execute(ctx, tripApp.StartTransitCommand{TripID: tripID, TenantID: "1"})
	require.Error(t, err)

	// Can't deliver from draft
	err = deliverUC.Execute(ctx, tripApp.DeliverCommand{TripID: tripID, TenantID: "1"})
	require.Error(t, err)

	// Can't complete from draft
	err = completeUC.Execute(ctx, tripApp.CompleteTripCommand{TripID: tripID, TenantID: "1"})
	require.Error(t, err)

	// Assign driver → start trip
	require.NoError(t, assignDriverUC.Execute(ctx, tripApp.AssignDriverCommand{
		TripID:   tripID,
		DriverID: string(driver.ID),
		TenantID: "1",
	}))
	require.NoError(t, startUC.Execute(ctx, tripApp.StartTripCommand{TripID: tripID, TenantID: "1"}))

	// Can't complete from started directly — must go through full chain
	err = completeUC.Execute(ctx, tripApp.CompleteTripCommand{TripID: tripID, TenantID: "1"})
	require.Error(t, err)

	// Can't start transit from started — must reach pickup first
	err = startTransitUC.Execute(ctx, tripApp.StartTransitCommand{TripID: tripID, TenantID: "1"})
	require.Error(t, err)
}

func TestSprint3_GenerateAndGetInvoice(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	svc := NewTestServices(t, db)
	customer, err := svc.Customers.CreateCustomer(ctx, "Acme Corp", "Acme", "555-9000", "acme@example.com", "", "", "")
	require.NoError(t, err)

	generateUC := invoiceApp.NewGenerateInvoiceUseCase(sqlUoW, idGen, realClock)
	getUC := invoiceApp.NewGetInvoiceUseCase(sqlUoW)

	invID, err := generateUC.Execute(ctx, invoiceApp.GenerateInvoiceCommand{
		TenantID:   "1",
		BookingID:  "bk-001",
		CustomerID: string(customer.ID),
		Subtotal:   10000,
		Tax:        1800,
		Discount:   0,
		Total:      11800,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, invID)

	inv, err := getUC.Execute(ctx, invoiceApp.GetInvoiceQuery{ID: invID, TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "bk-001", inv.BookingID)
	assert.InDelta(t, 11800.0, inv.Total, 0.01)
	assert.Equal(t, "pending", inv.PaymentStatus)
}

func TestSprint3_GenerateInvoice_Idempotent(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	svc := NewTestServices(t, db)
	customer, err := svc.Customers.CreateCustomer(ctx, "Beta Ltd", "Beta", "555-1111", "beta@example.com", "", "", "")
	require.NoError(t, err)

	generateUC := invoiceApp.NewGenerateInvoiceUseCase(sqlUoW, idGen, realClock)

	cmd := invoiceApp.GenerateInvoiceCommand{
		TenantID:   "1",
		BookingID:  "bk-idem",
		CustomerID: string(customer.ID),
		Subtotal:   5000,
		Tax:        900,
		Total:      5900,
	}

	id1, err := generateUC.Execute(ctx, cmd)
	require.NoError(t, err)

	id2, err := generateUC.Execute(ctx, cmd)
	require.NoError(t, err)

	// Same booking → same invoice returned (idempotent)
	assert.Equal(t, id1, id2)
}

func TestSprint3_GenerateInvoice_InvalidInput(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	generateUC := invoiceApp.NewGenerateInvoiceUseCase(sqlUoW, idGen, realClock)

	// Missing booking ID
	_, err := generateUC.Execute(ctx, invoiceApp.GenerateInvoiceCommand{
		TenantID:   "1",
		CustomerID: "cust-1",
		Total:      100,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "booking ID is required")

	// Negative total
	_, err = generateUC.Execute(ctx, invoiceApp.GenerateInvoiceCommand{
		TenantID:   "1",
		BookingID:  "bk-1",
		CustomerID: "cust-1",
		Total:      -100,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "total cannot be negative")
}

// ─────────────────────────────────────────────────────────────────────────────
// Sprint 4: Payment
// ─────────────────────────────────────────────────────────────────────────────

func TestSprint4_RecordPaymentAndGet(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	// First generate an invoice to have a valid invoice ID
	genUC := invoiceApp.NewGenerateInvoiceUseCase(sqlUoW, idGen, realClock)
	invID, err := genUC.Execute(ctx, invoiceApp.GenerateInvoiceCommand{
		TenantID:   "1",
		BookingID:  "bk-pay",
		CustomerID: "cust-99",
		Subtotal:   8000,
		Tax:        1440,
		Total:      9440,
	})
	require.NoError(t, err)

	recordUC := paymentApp.NewRecordPaymentUseCase(sqlUoW, idGen, realClock)
	getUC := paymentApp.NewGetPaymentUseCase(sqlUoW)

	payID, err := recordUC.Execute(ctx, paymentApp.RecordPaymentCommand{
		TenantID:    "1",
		InvoiceID:   string(invID),
		PaymentDate: time.Now(),
		Amount:      9440,
		Method:      paymentAggregate.PaymentMethodCash,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, payID)

	pay, err := getUC.Execute(ctx, paymentApp.GetPaymentQuery{ID: payID, TenantID: "1"})
	require.NoError(t, err)
	assert.InDelta(t, 9440.0, pay.Amount, 0.01)
	assert.Equal(t, "cash", pay.Method)
}

func TestSprint4_RecordPayment_InvalidAmount(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	recordUC := paymentApp.NewRecordPaymentUseCase(sqlUoW, idGen, realClock)

	_, err := recordUC.Execute(ctx, paymentApp.RecordPaymentCommand{
		TenantID:  "1",
		InvoiceID: "inv-x",
		Amount:    -100,
		Method:    paymentAggregate.PaymentMethodCash,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "amount must be greater than zero")
}
