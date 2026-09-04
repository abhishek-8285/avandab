package application

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bookingApp "transport-app/internal/booking/application"
	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	id "transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
	tripagg "transport-app/internal/trip/domain/aggregate"
	tripsql "transport-app/internal/trip/infrastructure/persistence/sql"
)

func TestCreateTrip_RejectsUnknownBooking(t *testing.T) {
	db := newTripTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()

	_, _ = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt_guard_1', 'tenant-1', 'Delhi', 'Jaipur', 280, 5, 12000)`)

	uc := NewCreateTripUseCase(unitOfWork, idGen, clk)
	bookingID := "bk-does-not-exist"
	_, err := uc.Execute(context.Background(), CreateTripCommand{
		TenantID:      shared.TenantID("tenant-1"),
		BookingID:     &bookingID,
		RouteID:       "rt_guard_1",
		DepartureTime: time.Now().Add(2 * time.Hour),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// CreateTrip must enforce single-active-trip per booking and allow a
// replacement once the previous trip reaches a terminal state.
func TestCreateTrip_SingleActiveTripPerBooking(t *testing.T) {
	db := newTripTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	ctx := context.Background()
	tenantID := shared.TenantID("tenant-1")

	_, err := db.Exec(`INSERT INTO customers (id, tenant_id, name, email, phone) VALUES ('cust_guard_1', 'tenant-1', 'Guard Buyer', 'guard@example.com', '9999999999')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt_guard_2', 'tenant-1', 'Delhi', 'Jaipur', 280, 5, 12000)`)
	require.NoError(t, err)

	bookingUC := bookingApp.NewCreateBookingUseCase(unitOfWork, idGen, clk)
	bookingID, err := bookingUC.Execute(ctx, bookingApp.CreateBookingCommand{
		TenantID:    tenantID,
		CustomerID:  "cust_guard_1",
		RouteID:     "rt_guard_2",
		PickupDate:  time.Now().Add(24 * time.Hour).Format("2006-01-02"),
		VehicleType: "truck",
		Passengers:  1,
		Price:       12000,
	})
	require.NoError(t, err)
	bookingIDStr := string(bookingID)

	createUC := NewCreateTripUseCase(unitOfWork, idGen, clk)
	tripID1, err := createUC.Execute(ctx, CreateTripCommand{
		TenantID:      tenantID,
		BookingID:     &bookingIDStr,
		RouteID:       "rt_guard_2",
		DepartureTime: time.Now().Add(2 * time.Hour),
	})
	require.NoError(t, err)
	require.NotEmpty(t, tripID1)

	// Second active trip for the same booking must be rejected.
	_, err = createUC.Execute(ctx, CreateTripCommand{
		TenantID:      tenantID,
		BookingID:     &bookingIDStr,
		RouteID:       "rt_guard_2",
		DepartureTime: time.Now().Add(3 * time.Hour),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already has active trip")

	// FindByBookingID roundtrip proves the linkage.
	repo := tripsql.NewTripRepository(db)
	found, err := repo.FindByBookingID(ctx, bookingIDStr, tenantID)
	require.NoError(t, err)
	assert.Equal(t, string(tripID1), string(found.ID))

	// After the first trip is cancelled, a replacement trip is allowed.
	cancelUC := NewCancelTripUseCase(unitOfWork, clk)
	require.NoError(t, cancelUC.Execute(ctx, CancelTripCommand{TripID: tripID1, TenantID: tenantID}))

	tripID2, err := createUC.Execute(ctx, CreateTripCommand{
		TenantID:      tenantID,
		BookingID:     &bookingIDStr,
		RouteID:       "rt_guard_2",
		DepartureTime: time.Now().Add(4 * time.Hour),
	})
	require.NoError(t, err)
	require.NotEmpty(t, tripID2)
}

// StartTrip must require a driver; starting an unassigned trip hides a
// dispatch failure and breaks downstream compliance checks.
func TestStartTrip_RequiresDriver(t *testing.T) {
	db := newTripTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	ctx := context.Background()
	tenantID := shared.TenantID("tenant-1")

	_, err := db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt_guard_3', 'tenant-1', 'Delhi', 'Jaipur', 280, 5, 12000)`)
	require.NoError(t, err)
	seedTestDriver(t, db, "drv_guard_start")

	createUC := NewCreateTripUseCase(unitOfWork, idGen, clk)
	tripID, err := createUC.Execute(ctx, CreateTripCommand{
		TenantID:      tenantID,
		RouteID:       "rt_guard_3",
		DepartureTime: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)

	startUC := NewStartTripUseCase(unitOfWork, clk)
	err = startUC.Execute(ctx, StartTripCommand{TripID: tripID, TenantID: tenantID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "driver must be assigned")

	assignUC := NewAssignDriverUseCase(unitOfWork, clk)
	require.NoError(t, assignUC.Execute(ctx, AssignDriverCommand{
		TripID:   tripID,
		DriverID: "drv_guard_start",
		TenantID: tenantID,
	}))
	require.NoError(t, startUC.Execute(ctx, StartTripCommand{TripID: tripID, TenantID: tenantID}))

	repo := tripsql.NewTripRepository(db)
	got, err := repo.Find(ctx, tripID, tenantID)
	require.NoError(t, err)
	assert.Equal(t, tripagg.TripStarted, got.Status)
}
