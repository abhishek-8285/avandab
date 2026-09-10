package application

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
)

func TestCreateTrip_AutoSeedsDefaultPickupAndDropStops(t *testing.T) {
	db := newTripTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	clk := clock.NewRealClock()
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))

	// Seed route
	_, err := db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('rt-auto-1', 'tenant-1', 'Delhi Hub', 'Jaipur Center', 260, 5.5, 12000)`)
	require.NoError(t, err)

	// Seed customer and booking
	_, err = db.Exec(`INSERT INTO customers (id, tenant_id, name, phone, email)
		VALUES ('cust-auto-1', 'tenant-1', 'Rajesh Sharma', '+91-9988776655', 'rajesh@example.com')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, vehicle_type, price, status, pickup_date)
		VALUES ('bk-auto-1', 'tenant-1', 'BK-AUTO-01', 'cust-auto-1', 'rt-auto-1', 'truck', 12000, 'confirmed', '2026-09-15 08:00:00')`)
	require.NoError(t, err)

	uc := NewCreateTripUseCase(unitOfWork, idGen, clk)
	bookingID := "bk-auto-1"
	depTime := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	tripID, err := uc.Execute(ctx, CreateTripCommand{
		TenantID:      shared.TenantID("tenant-1"),
		BookingID:     &bookingID,
		RouteID:       "rt-auto-1",
		DepartureTime: depTime,
		Remarks:       "Auto-seed test trip",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, tripID)

	// Verify 2 stops were created in trip_stops
	rows, err := db.Query(`SELECT stop_sequence, stop_type, location_name, address, otp_required, otp_code, pod_required, consignee_name, consignee_phone
		FROM trip_stops WHERE trip_id = ? ORDER BY stop_sequence ASC`, string(tripID))
	require.NoError(t, err)
	defer rows.Close()

	type stopRow struct {
		seq            int
		stopType       string
		locName        string
		address        string
		otpReq         int
		otpCode        string
		podReq         int
		consigneeName  string
		consigneePhone string
	}
	var stops []stopRow
	for rows.Next() {
		var s stopRow
		require.NoError(t, rows.Scan(&s.seq, &s.stopType, &s.locName, &s.address, &s.otpReq, &s.otpCode, &s.podReq, &s.consigneeName, &s.consigneePhone))
		stops = append(stops, s)
	}
	require.Len(t, stops, 2, "must auto-seed 2 stops (pickup + drop)")

	// Stop 1: Pickup
	assert.Equal(t, 1, stops[0].seq)
	assert.Equal(t, "pickup", stops[0].stopType)
	assert.Equal(t, "Delhi Hub", stops[0].locName)
	assert.Equal(t, 0, stops[0].otpReq)
	assert.Equal(t, 0, stops[0].podReq)

	// Stop 2: Drop
	assert.Equal(t, 2, stops[1].seq)
	assert.Equal(t, "drop", stops[1].stopType)
	assert.Equal(t, "Jaipur Center", stops[1].locName)
	assert.Equal(t, 1, stops[1].otpReq)
	assert.Len(t, stops[1].otpCode, 6, "OTP must be 6 digits")
	assert.Equal(t, 1, stops[1].podReq)
	assert.Equal(t, "Rajesh Sharma", stops[1].consigneeName)
	assert.Equal(t, "+91-9988776655", stops[1].consigneePhone)

	// Verify sync to legacy trips table
	var podOTP, podConsignee string
	require.NoError(t, db.QueryRow(`SELECT pod_otp, pod_consignee_name FROM trips WHERE id = ?`, string(tripID)).Scan(&podOTP, &podConsignee))
	assert.Equal(t, stops[1].otpCode, podOTP)
	assert.Equal(t, "Rajesh Sharma", podConsignee)
}
