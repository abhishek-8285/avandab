package application

import (
	"context"
	"fmt"
	"testing"
	"time"

	"database/sql"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	id "transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
	tripApp "transport-app/internal/trip/application"
	tripAgg "transport-app/internal/trip/domain/aggregate"
	tripSQL "transport-app/internal/trip/infrastructure/persistence/sql"
)

func newCascadeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:cascade_%d?mode=memory&cache=shared&_pragma=journal_mode(WAL)", time.Now().UnixNano()))
	require.NoError(t, err)
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../../db/migrations"))
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-1','Test Tenant 1','tenant-1')`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedCascadeLane(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO customers (id, tenant_id, name, email, phone) VALUES ('cust_cas_1', 'tenant-1', 'Cascade Buyer', 'cas@example.com', '9999999999')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt_cas_1', 'tenant-1', 'Delhi', 'Jaipur', 280, 5, 12000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES ('drv_cas_1', 'DRV-CAS-1', 'Rajesh', 'Kumar', '9876543210', 'DL-12345', date('now','+1 year'), 'available', 'tenant-1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
		VALUES ('veh_cas_1', 'REG-CAS-1', 'MH-01-CAS-1', 'truck', 15, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 'tenant-1')`)
	require.NoError(t, err)
}

// Cancelling a booking with an active (assigned) trip cancels both in one transaction.
func TestCancelBooking_CascadesToActiveTrip(t *testing.T) {
	db := newCascadeTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	ctx := context.Background()
	tenantID := shared.TenantID("tenant-1")
	seedCascadeLane(t, db)

	bookingUC := NewCreateBookingUseCase(unitOfWork, idGen, clk)
	bookingID, err := bookingUC.Execute(ctx, CreateBookingCommand{
		TenantID: tenantID, CustomerID: "cust_cas_1", RouteID: "rt_cas_1",
		PickupDate:  time.Now().Add(24 * time.Hour).Format("2006-01-02"),
		VehicleType: "truck", Passengers: 1, Price: 12000,
	})
	require.NoError(t, err)
	bookingIDStr := string(bookingID)

	tripCreateUC := tripApp.NewCreateTripUseCase(unitOfWork, idGen, clk)
	tripID, err := tripCreateUC.Execute(ctx, tripApp.CreateTripCommand{
		TenantID: tenantID, BookingID: &bookingIDStr, RouteID: "rt_cas_1",
		DepartureTime: time.Now().Add(2 * time.Hour),
	})
	require.NoError(t, err)
	require.NoError(t, tripApp.NewAssignDriverUseCase(unitOfWork, clk).Execute(ctx, tripApp.AssignDriverCommand{
		TripID: tripID, DriverID: "drv_cas_1", TenantID: tenantID,
	}))

	require.NoError(t, NewCancelBookingUseCase(unitOfWork, clk).Execute(ctx, CancelBookingCommand{
		BookingID: bookingID, TenantID: tenantID,
	}))

	tripRepo := tripSQL.NewTripRepository(db)
	got, err := tripRepo.Find(ctx, tripID, tenantID)
	require.NoError(t, err)
	assert.Equal(t, tripAgg.TripCancelled, got.Status)
}

// Cancelling a booking with no trip succeeds (no orphan handling needed).
func TestCancelBooking_NoTripSucceeds(t *testing.T) {
	db := newCascadeTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	ctx := context.Background()
	tenantID := shared.TenantID("tenant-1")
	seedCascadeLane(t, db)

	bookingUC := NewCreateBookingUseCase(unitOfWork, idGen, clk)
	bookingID, err := bookingUC.Execute(ctx, CreateBookingCommand{
		TenantID: tenantID, CustomerID: "cust_cas_1", RouteID: "rt_cas_1",
		PickupDate:  time.Now().Add(24 * time.Hour).Format("2006-01-02"),
		VehicleType: "truck", Passengers: 1, Price: 12000,
	})
	require.NoError(t, err)

	require.NoError(t, NewCancelBookingUseCase(unitOfWork, clk).Execute(ctx, CancelBookingCommand{
		BookingID: bookingID, TenantID: tenantID,
	}))

	got, err := NewGetBookingUseCase(unitOfWork).Execute(ctx, GetBookingQuery{BookingID: bookingID, TenantID: tenantID})
	require.NoError(t, err)
	assert.Equal(t, string(aggregate.BookingCancelled), got.Status)
}

// A completed trip is left untouched when its booking is cancelled.
func TestCancelBooking_CompletedTripUntouched(t *testing.T) {
	db := newCascadeTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	ctx := context.Background()
	tenantID := shared.TenantID("tenant-1")
	seedCascadeLane(t, db)

	bookingUC := NewCreateBookingUseCase(unitOfWork, idGen, clk)
	bookingID, err := bookingUC.Execute(ctx, CreateBookingCommand{
		TenantID: tenantID, CustomerID: "cust_cas_1", RouteID: "rt_cas_1",
		PickupDate:  time.Now().Add(24 * time.Hour).Format("2006-01-02"),
		VehicleType: "truck", Passengers: 1, Price: 12000,
	})
	require.NoError(t, err)
	require.NoError(t, NewConfirmBookingUseCase(unitOfWork, clk).Execute(ctx, ConfirmBookingCommand{
		BookingID: bookingID, TenantID: tenantID,
	}))
	bookingIDStr := string(bookingID)

	tripCreateUC := tripApp.NewCreateTripUseCase(unitOfWork, idGen, clk)
	tripID, err := tripCreateUC.Execute(ctx, tripApp.CreateTripCommand{
		TenantID: tenantID, BookingID: &bookingIDStr, RouteID: "rt_cas_1",
		DepartureTime: time.Now().Add(-6 * time.Hour),
	})
	require.NoError(t, err)
	require.NoError(t, tripApp.NewAssignDriverUseCase(unitOfWork, clk).Execute(ctx, tripApp.AssignDriverCommand{
		TripID: tripID, DriverID: "drv_cas_1", TenantID: tenantID,
	}))
	require.NoError(t, tripApp.NewAssignVehicleUseCase(unitOfWork, clk).Execute(ctx, tripApp.AssignVehicleCommand{
		TripID: tripID, VehicleID: "veh_cas_1", TenantID: tenantID,
	}))
	require.NoError(t, tripApp.NewStartTripUseCase(unitOfWork, clk).Execute(ctx, tripApp.StartTripCommand{TripID: tripID, TenantID: tenantID}))
	require.NoError(t, tripApp.NewReachPickupUseCase(unitOfWork, clk).Execute(ctx, tripApp.ReachPickupCommand{TripID: tripID, TenantID: tenantID}))
	require.NoError(t, tripApp.NewStartTransitUseCase(unitOfWork, clk).Execute(ctx, tripApp.StartTransitCommand{TripID: tripID, TenantID: tenantID}))
	require.NoError(t, tripApp.NewDeliverUseCase(unitOfWork, clk).Execute(ctx, tripApp.DeliverCommand{TripID: tripID, TenantID: tenantID}))
	require.NoError(t, tripApp.NewCompleteTripUseCase(unitOfWork, clk).Execute(ctx, tripApp.CompleteTripCommand{TripID: tripID, TenantID: tenantID}))

	require.NoError(t, NewCancelBookingUseCase(unitOfWork, clk).Execute(ctx, CancelBookingCommand{
		BookingID: bookingID, TenantID: tenantID,
	}))

	tripRepo := tripSQL.NewTripRepository(db)
	got, err := tripRepo.Find(ctx, tripID, tenantID)
	require.NoError(t, err)
	assert.Equal(t, tripAgg.TripCompleted, got.Status)
}
