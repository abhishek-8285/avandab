package application_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/settlement/application"
	settleSQL "transport-app/internal/settlement/infrastructure/persistence/sql"
)

// Crash-partial: a wallet-rail header committed but its ledger appends never
// landed. The idempotent retry must heal the wallet — header reused AND
// missing entries appended. Pre-fix this returned the bare header (red).
func TestBackfillOwnLedger_HealsCrashPartial(t *testing.T) {
	db := setupSettlementTestDB(t)
	_, err := db.Exec(legacyLinesSchema)
	require.NoError(t, err)

	tenantID := "tenant-1"
	driverID := "drv-settle-1"
	tripID := "trip-partial-77"

	// Header as the wallet rail writes it (set_ prefix), no ledger rows.
	_, err = db.Exec(`INSERT INTO driver_settlements
		(id, tenant_id, trip_id, driver_id, gross_fare, deductions, net_payout, status,
		 commission_rate, commission_amount, toll_adjustment, advance_deductions, tds_rate, tds_amount)
		VALUES ('set_partial_1', ?, ?, ?, 2000.0, 220.0, 1780.0, 'calculated',
		 0.10, 200.0, 0, 0, 0.01, 20.0)`,
		tenantID, tripID, driverID)
	require.NoError(t, err)

	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	ctx := context.Background()

	got, err := svc.CalculateAndCreateSettlement(ctx, tenantID, application.CalculateSettlementRequest{
		TripID: tripID, DriverID: driverID, GrossFare: 2000.0, CommissionRate: 0.10, TDSRate: 0.01,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "set_partial_1", got.ID, "same header row must be reused")

	// Still exactly one header.
	var headers int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM driver_settlements WHERE trip_id = ?`, tripID).Scan(&headers))
	assert.Equal(t, 1, headers)

	// Wallet healed: TRIP_EARNING +2000, COMMISSION -200, PENALTY(TDS) -20.
	rows, err := db.Query(
		`SELECT entry_type, amount FROM driver_ledger_entries WHERE reference_id = 'set_partial_1'`)
	require.NoError(t, err)
	defer rows.Close()
	gotEntries := map[string]float64{}
	for rows.Next() {
		var typ string
		var amt float64
		require.NoError(t, rows.Scan(&typ, &amt))
		gotEntries[typ] = amt
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, map[string]float64{
		"TRIP_EARNING": 2000.0, "COMMISSION": -200.0, "PENALTY": -20.0,
	}, gotEntries)
}

// Legacy-owned rows must never gain wallet entries, even with empty ledger.
func TestBackfillOwnLedger_SkipsLegacyOwned(t *testing.T) {
	db := setupSettlementTestDB(t)
	_, err := db.Exec(legacyLinesSchema)
	require.NoError(t, err)

	tenantID := "tenant-1"
	driverID := "drv-settle-1"
	tripID := "trip-legacy-4243"

	_, err = db.Exec(`INSERT INTO driver_settlements
		(id, tenant_id, trip_id, driver_id, gross_fare, deductions, net_payout, status)
		VALUES ('stl-legacy-2', ?, ?, ?, 2000.0, 518.0, 1482.0, 'pending')`,
		tenantID, tripID, driverID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO settlement_lines
		(id, settlement_id, trip_id, line_type, label, amount)
		VALUES ('ln-9', 'stl-legacy-2', ?, 'gross_fare', 'Trip fare', 2000.0)`, tripID)
	require.NoError(t, err)

	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)

	got, err := svc.CalculateAndCreateSettlement(context.Background(), tenantID,
		application.CalculateSettlementRequest{TripID: tripID, DriverID: driverID, GrossFare: 2000.0})
	require.NoError(t, err)
	assert.Equal(t, "stl-legacy-2", got.ID)

	var n int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM driver_ledger_entries WHERE driver_id = ?`, driverID).Scan(&n))
	assert.Equal(t, 0, n, "legacy-owned trip must gain zero wallet entries")
}

// Bridge-style headers (non-set_ prefix, no lines) are not ours — no append.
func TestBackfillOwnLedger_SkipsForeignHeaders(t *testing.T) {
	db := setupSettlementTestDB(t)
	_, err := db.Exec(legacyLinesSchema)
	require.NoError(t, err)

	tenantID := "tenant-1"
	driverID := "drv-settle-1"
	tripID := "trip-bridge-11"

	_, err = db.Exec(`INSERT INTO driver_settlements
		(id, tenant_id, trip_id, driver_id, gross_fare, deductions, net_payout, status)
		VALUES ('paid-bridge-1', ?, ?, ?, 3000.0, 0, 3000.0, 'paid')`,
		tenantID, tripID, driverID)
	require.NoError(t, err)

	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)

	got, err := svc.CalculateAndCreateSettlement(context.Background(), tenantID,
		application.CalculateSettlementRequest{TripID: tripID, DriverID: driverID, GrossFare: 3000.0})
	require.NoError(t, err)
	assert.Equal(t, "paid-bridge-1", got.ID)

	var n int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM driver_ledger_entries WHERE driver_id = ?`, driverID).Scan(&n))
	assert.Equal(t, 0, n, "foreign header must gain zero wallet entries")
}

// Complete own header: second call appends nothing (stable idempotency).
func TestBackfillOwnLedger_StableWhenComplete(t *testing.T) {
	db := setupSettlementTestDB(t)

	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	ctx := context.Background()

	first, err := svc.CalculateAndCreateSettlement(ctx, "tenant-1",
		application.CalculateSettlementRequest{
			TripID: "trip-complete-5", DriverID: "drv-settle-1",
			GrossFare: 1500.0, CommissionRate: 0.10, TDSRate: 0.01,
		})
	require.NoError(t, err)

	second, err := svc.CalculateAndCreateSettlement(ctx, "tenant-1",
		application.CalculateSettlementRequest{
			TripID: "trip-complete-5", DriverID: "drv-settle-1",
			GrossFare: 1500.0, CommissionRate: 0.10, TDSRate: 0.01,
		})
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)

	var n int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM driver_ledger_entries WHERE reference_id = ?`, first.ID).Scan(&n))
	assert.Equal(t, 3, n, "TRIP_EARNING + COMMISSION + PENALTY(TDS), no duplicates")
}
