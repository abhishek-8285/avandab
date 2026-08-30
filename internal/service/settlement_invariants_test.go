package service_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
	"transport-app/internal/events"
	"transport-app/internal/shared"
)

func TestSettlement_InvariantsAndReplayAudit(t *testing.T) {
	dbConn, svcs, bus := setupComplianceTestDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))

	tenantID := "tenant-1"
	driverID := "drv-inv-1"
	vehicleID := "veh-inv-1"
	customerID := "cust-inv-1"
	routeID := "route-inv-1"
	bookingID := "bk-inv-1"
	tripID := "trip-inv-1"

	// 1. Seed Prerequisites
	_, err := dbConn.Exec(`INSERT INTO drivers (id, driver_id, tenant_id, first_name, last_name, phone, license_number, license_expiry, pan, status)
		VALUES (?, ?, ?, 'Ramesh', 'Kumar', '9876543210', 'DL-INV-001', '2029-01-01', 'ABCDE1234F', 'available')`,
		driverID, driverID, tenantID)
	require.NoError(t, err)

	_, err = dbConn.Exec(`INSERT INTO driver_payout_accounts (id, tenant_id, driver_id, account_holder_name, account_number_encrypted, account_number_masked, ifsc_code, bank_name, is_primary, verification_status)
		VALUES ('acc-inv-1', ?, ?, 'Ramesh Kumar', 'enc_112233', 'XXXXXX3344', 'HDFC0001234', 'HDFC Bank', 1, 'verified')`,
		tenantID, driverID)
	require.NoError(t, err)

	_, err = dbConn.Exec(`INSERT INTO vehicles (id, vehicle_number, tenant_id, registration_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status)
		VALUES (?, 'DL01AB1234', ?, 'DL01AB1234', 'truck', 10000, '2028-01-01', '2028-01-01', '2028-01-01', 'available')`,
		vehicleID, tenantID)
	require.NoError(t, err)

	_, err = dbConn.Exec(`INSERT INTO customers (id, tenant_id, name, phone, email, status)
		VALUES (?, ?, 'Logistics Corp', '9999988888', 'logistics@corp.com', 'active')`,
		customerID, tenantID)
	require.NoError(t, err)

	_, err = dbConn.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare)
		VALUES (?, ?, 'Delhi', 'Jaipur', 250.0, 5.0, 5000.0)`,
		routeID, tenantID)
	require.NoError(t, err)

	// Booking with price ₹5,000
	_, err = dbConn.Exec(`INSERT INTO bookings (id, booking_number, tenant_id, customer_id, route_id, pickup_date, vehicle_type, price, status)
		VALUES (?, 'BK-INV-001', ?, ?, ?, datetime('now'), 'truck', 5000.0, 'confirmed')`,
		bookingID, tenantID, customerID, routeID)
	require.NoError(t, err)

	// Trip with booking and route assigned
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, tenant_id, booking_id, route_id, driver_id, vehicle_id, departure_time, status)
		VALUES (?, 'TRIP-INV-001', ?, ?, ?, ?, ?, datetime('now'), 'started')`,
		tripID, tenantID, bookingID, routeID, driverID, vehicleID)
	require.NoError(t, err)

	// Configure Rate Model & Settings: commission_pct = 10% (Gross = 5000, Commission = 500)
	_, _ = dbConn.Exec(`INSERT OR REPLACE INTO company_config (key, value) VALUES ('settlement_rate_model', 'commission_pct')`)
	_, _ = dbConn.Exec(`INSERT OR REPLACE INTO company_config (key, value) VALUES ('settlement_commission_pct', '10.0')`)
	_, _ = dbConn.Exec(`INSERT OR REPLACE INTO company_config (key, value) VALUES ('tds_rate_with_pan', '1.0')`)

	// Approved Kharcha: ₹300 fuel expense
	_, err = dbConn.Exec(`INSERT INTO driver_expenses (id, tenant_id, driver_id, trip_id, expense_type, category, amount, status, approved, created_at)
		VALUES ('exp-inv-1', ?, ?, ?, 'fuel', 'fuel', 300.0, 'approved', 1, datetime('now'))`,
		tenantID, driverID, tripID)
	require.NoError(t, err)

	// Approved Advance Request: ₹200 cash advance
	_, err = dbConn.Exec(`INSERT INTO driver_advance_requests (id, tenant_id, driver_id, trip_id, amount, reason, status, requested_at)
		VALUES ('adv-inv-1', ?, ?, ?, 200.0, 'Toll & food', 'approved', datetime('now'))`,
		tenantID, driverID, tripID)
	require.NoError(t, err)

	// 2. Execute DeliverTripWithPOD -> triggers TripDelivered event
	trip, err := svcs.Trips.DeliverTripWithPOD(ctx, domain.TripID(tripID), "https://vault.avandab.com/epod/pod1.jpg")
	require.NoError(t, err)
	assert.Equal(t, domain.TripDelivered, trip.Status)

	// Invariant 1: Exactly ONE settlement record created
	var settleCount int
	err = dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM driver_settlements WHERE trip_id = ?`, tripID).Scan(&settleCount)
	require.NoError(t, err)
	assert.Equal(t, 1, settleCount)

	settlement, err := svcs.Settlements.GetSettlement(ctx, "stl-"+tripID)
	if err != nil {
		// Try fetching via findByTripID directly
		var sID string
		_ = dbConn.QueryRowContext(ctx, `SELECT id FROM driver_settlements WHERE trip_id = ?`, tripID).Scan(&sID)
		settlement, err = svcs.Settlements.GetSettlement(ctx, sID)
	}
	require.NoError(t, err)
	require.NotNil(t, settlement)

	// Gross = 4500, Commission = 500, Advances = 500
	// TDS Base = 4500 - 500 - 500 = 3500
	// TDS 1% = 35.0
	// Net Payout = 4500 - 500 - 500 - 35 = 3465.0
	assert.Equal(t, 4500.0, settlement.GrossFare)
	assert.Equal(t, 500.0, settlement.CommissionAmount)
	assert.Equal(t, 500.0, settlement.AdvancesKharcha)
	assert.Equal(t, 35.0, settlement.TDSAmount)
	assert.Equal(t, 3465.0, settlement.NetPayout)

	// Invariant 2: Driver Ledger Entries created exactly once
	var ledgerCount int
	err = dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM driver_ledger_entries WHERE driver_id = ?`, driverID).Scan(&ledgerCount)
	require.NoError(t, err)
	assert.True(t, ledgerCount >= 4, "Expected ledger entries for earning, commission, advances, tds")

	// Invariant 3: Driver Wallet Available Balance == Settlement Net Payout
	var driverBalance float64
	err = dbConn.QueryRowContext(ctx, `SELECT balance_after FROM driver_ledger_entries WHERE driver_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, driverID).Scan(&driverBalance)
	require.NoError(t, err)
	assert.Equal(t, settlement.NetPayout, driverBalance)

	// Invariant 4: Advance requests attached to settlement
	var attachedSettlementID sql.NullString
	err = dbConn.QueryRowContext(ctx, `SELECT settlement_id FROM driver_advance_requests WHERE id = 'adv-inv-1'`).Scan(&attachedSettlementID)
	require.NoError(t, err)
	assert.True(t, attachedSettlementID.Valid && attachedSettlementID.String != "")

	// -------------------------------------------------------------
	// 3. REPLAY / DUPLICATE EVENT AUDIT
	// Re-emit TripDelivered event 5 times to test replay idempotency
	// -------------------------------------------------------------
	for i := 0; i < 5; i++ {
		bus.Publish(ctx, events.Event{
			Type: events.TripDelivered,
			Payload: map[string]interface{}{
				"trip_id":      domain.TripID(tripID),
				"booking_id":   &bookingID,
				"driver_id":    &driverID,
				"pod_url":      "https://vault.avandab.com/epod/pod1.jpg",
				"delivered_at": time.Now(),
				"occurred_at":  time.Now(),
			},
		})
	}

	// Invariant 5: Replay did NOT create duplicate settlements
	var settleCountAfterReplay int
	err = dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM driver_settlements WHERE trip_id = ?`, tripID).Scan(&settleCountAfterReplay)
	require.NoError(t, err)
	assert.Equal(t, 1, settleCountAfterReplay, "Duplicate TripDelivered events must NOT create extra settlements")

	// Invariant 6: Replay did NOT duplicate ledger entries
	var ledgerCountAfterReplay int
	err = dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM driver_ledger_entries WHERE driver_id = ?`, driverID).Scan(&ledgerCountAfterReplay)
	require.NoError(t, err)
	assert.Equal(t, ledgerCount, ledgerCountAfterReplay, "Duplicate TripDelivered events must NOT duplicate ledger entries")

	// Invariant 7: Driver balance remains exactly identical after replay
	var balanceAfterReplay float64
	err = dbConn.QueryRowContext(ctx, `SELECT balance_after FROM driver_ledger_entries WHERE driver_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, driverID).Scan(&balanceAfterReplay)
	require.NoError(t, err)
	assert.Equal(t, driverBalance, balanceAfterReplay, "Driver balance must not drift on duplicate events")
}
