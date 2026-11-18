package service_test

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/events"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// seedMarkPaidSettlement mirrors the full trip prerequisite chain required by
// the strict-FK migrations so GenerateSettlement can run, then creates the
// settlement and returns its id.
func seedMarkPaidSettlement(t *testing.T, ctx context.Context, dbConn *sql.DB, svcs *service.Services, tripID string) string {
	t.Helper()
	tenantID := "tenant-1"
	driverID := tripID + "-drv"
	vehicleID := tripID + "-veh"
	customerID := tripID + "-cus"
	routeID := tripID + "-route"
	bookingID := tripID + "-bk"

	_, err := dbConn.ExecContext(ctx, `INSERT INTO drivers
		(id, driver_id, tenant_id, first_name, last_name, phone, license_number, license_expiry, status)
		VALUES (?, ?, ?, 'R', 'K', '999', ?, '2029-01-01', 'available')`,
		driverID, driverID, tenantID, "DL-"+tripID)
	require.NoError(t, err)
	_, err = dbConn.ExecContext(ctx, `INSERT INTO vehicles
		(id, vehicle_number, tenant_id, registration_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status)
		VALUES (?, ?, ?, ?, 'truck', 10000, '2028-01-01', '2028-01-01', '2028-01-01', 'available')`,
		vehicleID, vehicleID, tenantID, vehicleID)
	require.NoError(t, err)
	_, err = dbConn.ExecContext(ctx, `INSERT INTO customers (id, tenant_id, name, phone, status)
		VALUES (?, ?, 'Logistics', '9999988888', 'active')`, customerID, tenantID)
	require.NoError(t, err)
	_, err = dbConn.ExecContext(ctx, `INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare)
		VALUES (?, ?, 'Delhi', 'Jaipur', 250.0, 5.0, 5000.0)`, routeID, tenantID)
	require.NoError(t, err)
	_, err = dbConn.ExecContext(ctx, `INSERT INTO bookings
		(id, booking_number, tenant_id, customer_id, route_id, pickup_date, vehicle_type, price, status)
		VALUES (?, ?, ?, ?, ?, datetime('now'), 'truck', 5000.0, 'confirmed')`,
		bookingID, bookingID, tenantID, customerID, routeID)
	require.NoError(t, err)
	_, err = dbConn.ExecContext(ctx, `INSERT INTO trips
		(id, trip_number, tenant_id, booking_id, route_id, driver_id, vehicle_id, departure_time, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), 'started')`,
		tripID, tripID, tenantID, bookingID, routeID, driverID, vehicleID)
	require.NoError(t, err)

	rec, err := svcs.Settlements.GenerateSettlement(ctx, tripID, false)
	require.NoError(t, err)
	return rec.ID
}

func TestMarkPaid_IdempotentAndAdvanceError(t *testing.T) {
	t.Run("double mark paid does not republish or overwrite paid_at", func(t *testing.T) {
		dbConn, svcs, bus := setupComplianceTestDB(t)
		ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))

		var pubs int64
		bus.Subscribe(events.DriverPayoutSettled, func(context.Context, events.Event) error {
			atomic.AddInt64(&pubs, 1)
			return nil
		})

		settlementID := seedMarkPaidSettlement(t, ctx, dbConn, svcs, "trip-mp1")

		t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
		r1, err := svcs.Settlements.MarkPaid(ctx, settlementID, "pay-1", t1)
		require.NoError(t, err)
		require.NotNil(t, r1.PaidAt)

		t2 := time.Date(2026, 2, 2, 10, 0, 0, 0, time.UTC)
		r2, err := svcs.Settlements.MarkPaid(ctx, settlementID, "pay-2", t2)
		require.NoError(t, err)
		require.NotNil(t, r2.PaidAt)
		assert.Equal(t, r1.PaidAt.UTC(), r2.PaidAt.UTC(), "second MarkPaid must not overwrite paid_at")
		require.NotNil(t, r2.PaymentRef)
		assert.Equal(t, "pay-1", *r2.PaymentRef, "second MarkPaid must not overwrite payment_ref")
		assert.Equal(t, int64(1), atomic.LoadInt64(&pubs), "payout-settled must publish exactly once")
	})

	t.Run("advance update failure propagates", func(t *testing.T) {
		dbConn, svcs, _ := setupComplianceTestDB(t)
		ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))
		settlementID := seedMarkPaidSettlement(t, ctx, dbConn, svcs, "trip-mp2")

		_, err := dbConn.ExecContext(ctx, `DROP TABLE driver_advance_requests`)
		require.NoError(t, err)

		_, err = svcs.Settlements.MarkPaid(ctx, settlementID, "pay-3", time.Now())
		assert.Error(t, err, "advance status update failure must be returned, not swallowed")
	})
}
