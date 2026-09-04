package application

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	entitlementApp "transport-app/internal/entitlement/application"
	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	id "transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
	tripApp "transport-app/internal/trip/application"
)

// Full plan with quota: booking holds a unit, completion consumes it.
func TestMetering_ReserveCommitFlow(t *testing.T) {
	db := newCascadeTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	ctx := context.Background()
	tenantID := shared.TenantID("tenant-1")
	seedCascadeLane(t, db)
	meter := entitlementApp.NewService(db)

	_, err := db.Exec(`INSERT INTO tenant_subscriptions (id, tenant_id, plan_id, status, current_period_start, current_period_end)
		VALUES ('sub_m_1', 'tenant-1', 'GROWTH', 'ACTIVE', '2026-01-01 00:00:00', '2026-12-31 00:00:00')`)
	require.NoError(t, err)

	bookingUC := NewCreateBookingUseCase(unitOfWork, idGen, clk).WithUsageMeter(meter)
	bookingID, err := bookingUC.Execute(ctx, gateTestCmd(tenantID))
	require.NoError(t, err)

	var reserved, used int
	require.NoError(t, db.QueryRow(`SELECT reserved_quantity, used_quantity FROM tenant_usage_meters WHERE tenant_id='tenant-1' AND quota_key='max_trips_per_month'`).Scan(&reserved, &used))
	assert.Equal(t, 1, reserved)
	assert.Equal(t, 0, used)

	// Run the trip to completion with a metered complete UC.
	bookingIDStr := string(bookingID)
	tripCreateUC := tripApp.NewCreateTripUseCase(unitOfWork, idGen, clk)
	tripID, err := tripCreateUC.Execute(ctx, tripApp.CreateTripCommand{
		TenantID: tenantID, BookingID: &bookingIDStr, RouteID: "rt_cas_1",
		DepartureTime: time.Now().Add(-6 * time.Hour),
	})
	require.NoError(t, err)
	require.NoError(t, tripApp.NewAssignDriverUseCase(unitOfWork, clk).Execute(ctx, tripApp.AssignDriverCommand{TripID: tripID, DriverID: "drv_cas_1", TenantID: tenantID}))
	require.NoError(t, tripApp.NewAssignVehicleUseCase(unitOfWork, clk).Execute(ctx, tripApp.AssignVehicleCommand{TripID: tripID, VehicleID: "veh_cas_1", TenantID: tenantID}))
	require.NoError(t, tripApp.NewStartTripUseCase(unitOfWork, clk).Execute(ctx, tripApp.StartTripCommand{TripID: tripID, TenantID: tenantID}))
	require.NoError(t, tripApp.NewReachPickupUseCase(unitOfWork, clk).Execute(ctx, tripApp.ReachPickupCommand{TripID: tripID, TenantID: tenantID}))
	require.NoError(t, tripApp.NewStartTransitUseCase(unitOfWork, clk).Execute(ctx, tripApp.StartTransitCommand{TripID: tripID, TenantID: tenantID}))
	require.NoError(t, tripApp.NewDeliverUseCase(unitOfWork, clk).Execute(ctx, tripApp.DeliverCommand{TripID: tripID, TenantID: tenantID}))
	require.NoError(t, tripApp.NewCompleteTripUseCase(unitOfWork, clk).WithUsageMeter(meter).Execute(ctx, tripApp.CompleteTripCommand{TripID: tripID, TenantID: tenantID}))

	require.NoError(t, db.QueryRow(`SELECT reserved_quantity, used_quantity FROM tenant_usage_meters WHERE tenant_id='tenant-1' AND quota_key='max_trips_per_month'`).Scan(&reserved, &used))
	assert.Equal(t, 0, reserved)
	assert.Equal(t, 1, used)

}

// Cancel returns the hold without consuming.
func TestMetering_CancelReleases(t *testing.T) {
	db := newCascadeTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	ctx := context.Background()
	tenantID := shared.TenantID("tenant-1")
	seedCascadeLane(t, db)
	meter := entitlementApp.NewService(db)

	_, err := db.Exec(`INSERT INTO tenant_subscriptions (id, tenant_id, plan_id, status, current_period_start, current_period_end)
		VALUES ('sub_m_2', 'tenant-1', 'GROWTH', 'ACTIVE', '2026-01-01 00:00:00', '2026-12-31 00:00:00')`)
	require.NoError(t, err)

	bookingUC := NewCreateBookingUseCase(unitOfWork, idGen, clk).WithUsageMeter(meter)
	bookingID, err := bookingUC.Execute(ctx, gateTestCmd(tenantID))
	require.NoError(t, err)

	require.NoError(t, NewCancelBookingUseCase(unitOfWork, clk).WithUsageMeter(meter).Execute(ctx, CancelBookingCommand{
		BookingID: bookingID, TenantID: tenantID,
	}))

	var reserved, used int
	require.NoError(t, db.QueryRow(`SELECT reserved_quantity, used_quantity FROM tenant_usage_meters WHERE tenant_id='tenant-1' AND quota_key='max_trips_per_month'`).Scan(&reserved, &used))
	assert.Equal(t, 0, reserved)
	assert.Equal(t, 0, used)
}

// Full plan rejects the 11th concurrent hold (STARTER max 10).
func TestMetering_OverQuotaRejected(t *testing.T) {
	db := newCascadeTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	ctx := context.Background()
	tenantID := shared.TenantID("tenant-1")
	seedCascadeLane(t, db)
	meter := entitlementApp.NewService(db)

	_, err := db.Exec(`INSERT INTO tenant_subscriptions (id, tenant_id, plan_id, status, current_period_start, current_period_end)
		VALUES ('sub_m_3', 'tenant-1', 'STARTER', 'ACTIVE', '2026-01-01 00:00:00', '2026-12-31 00:00:00')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tenant_usage_meters (id, tenant_id, quota_key, period_start, period_end, used_quantity, reserved_quantity, max_quantity, updated_at)
		VALUES ('m_full_1', 'tenant-1', 'max_trips_per_month', '2026-01-01 00:00:00', '2026-12-31 00:00:00', 10, 0, 10, '2026-01-01 00:00:00')`)
	require.NoError(t, err)

	bookingUC := NewCreateBookingUseCase(unitOfWork, idGen, clk).WithUsageMeter(meter)
	_, err = bookingUC.Execute(ctx, gateTestCmd(tenantID))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota")
}
