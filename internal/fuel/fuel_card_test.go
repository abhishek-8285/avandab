package fuel_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/fuel"
	fuelapp "transport-app/internal/fuel/application"
)

func setupFuelCardTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_fuel_card_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))
	return db
}

func TestFuelCard_MaskAndHash(t *testing.T) {
	rawNum := "7102123456789999"
	masked := fuel.MaskCardNumber(rawNum)
	assert.Equal(t, "**** **** **** 9999", masked)

	hash1 := fuel.HashCardToken(rawNum)
	hash2 := fuel.HashCardToken(rawNum)
	assert.Equal(t, hash1, hash2, "same raw card number must produce identical SHA-256 hash")
	assert.NotEmpty(t, hash1)

	// Test with spaces/dashes
	hash3 := fuel.HashCardToken("7102-1234-5678-9999")
	assert.Equal(t, hash1, hash3, "normalized card numbers should have identical hashes")
}

func TestFuelCard_RegistrationAndSpend(t *testing.T) {
	db := setupFuelCardTestDB(t)
	tenantID := "tenant-fuel-1"

	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Fleet Co', 'fleet-co')`, tenantID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, vehicle_type, capacity, tank_capacity_litres)
		VALUES ('veh-1', $1, 'MH12AB1234', 'MH12AB1234', 'truck', 10000, 300.0)`, tenantID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO drivers (id, tenant_id, driver_id, first_name, last_name, phone)
		VALUES ('drv-1', $1, 'DRV-001', 'Ramesh', 'Kumar', '9876543210')`, tenantID)
	require.NoError(t, err)

	repo := fuel.NewSQLFuelCardRepository(db)
	useCase := fuelapp.NewFuelCardUseCase(repo)

	ctx := context.Background()

	// 1. Register a valid card
	vehID := "veh-1"
	drvID := "drv-1"
	card, err := useCase.RegisterCard(ctx, tenantID, fuel.RegisterFuelCardRequest{
		CardNumber:        "7102123456781111",
		Provider:          string(fuel.ProviderIOCL),
		AssignedVehicleID: &vehID,
		AssignedDriverID:  &drvID,
		DailySpendLimit:   40000,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, card.ID)
	assert.Equal(t, "**** **** **** 1111", card.CardNumberMasked)
	assert.Equal(t, fuel.ProviderIOCL, card.Provider)
	assert.Equal(t, "veh-1", *card.AssignedVehicleID)
	assert.Equal(t, "drv-1", *card.AssignedDriverID)
	assert.Equal(t, 40000.0, card.DailySpendLimit)

	// 2. Duplicate card registration within same tenant should fail
	_, err = useCase.RegisterCard(ctx, tenantID, fuel.RegisterFuelCardRequest{
		CardNumber: "7102123456781111",
		Provider:   string(fuel.ProviderIOCL),
	})
	require.Error(t, err, "expected duplicate card token hash error")

	// 3. Invalid provider should fail
	_, err = useCase.RegisterCard(ctx, tenantID, fuel.RegisterFuelCardRequest{
		CardNumber: "7102123456782222",
		Provider:   "INVALID_PROVIDER",
	})
	require.Error(t, err, "invalid provider must be rejected")

	// 4. List cards
	cards, err := useCase.ListCards(ctx, tenantID)
	require.NoError(t, err)
	require.Len(t, cards, 1)
	assert.Equal(t, card.ID, cards[0].ID)
}

func TestFuelCard_SyncTransactions_ReconciliationLoop(t *testing.T) {
	db := setupFuelCardTestDB(t)
	tenantID := "tenant-fuel-sync"

	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Express Logistics', 'express-log')`, tenantID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, vehicle_type, capacity, tank_capacity_litres)
		VALUES ('veh-truck-1', $1, 'DL01XY9999', 'DL01XY9999', 'truck', 15000, 200.0)`, tenantID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO drivers (id, tenant_id, driver_id, first_name, last_name, phone)
		VALUES ('drv-sync-1', $1, 'DRV-SYNC-1', 'Suresh', 'Singh', '9811122233')`, tenantID)
	require.NoError(t, err)

	repo := fuel.NewSQLFuelCardRepository(db)
	useCase := fuelapp.NewFuelCardUseCase(repo)

	ctx := context.Background()

	// Register card assigned to veh-truck-1
	vehID := "veh-truck-1"
	drvID := "drv-sync-1"
	cardNum := "7102555544443333"
	_, err = useCase.RegisterCard(ctx, tenantID, fuel.RegisterFuelCardRequest{
		CardNumber:        cardNum,
		Provider:          string(fuel.ProviderBPCL),
		AssignedVehicleID: &vehID,
		AssignedDriverID:  &drvID,
		DailySpendLimit:   60000,
	})
	require.NoError(t, err)

	// Seed existing driver expense to test MATCHED_EXPENSE
	now := time.Now().UTC()
	_, err = db.Exec(`INSERT INTO driver_expenses (id, tenant_id, driver_id, category, expense_type, amount, status, created_at)
		VALUES ('exp-match-1', $1, 'drv-sync-1', 'fuel', 'fuel', 4500.0, 'pending', $2)`,
		tenantID, now.Format(time.RFC3339))
	require.NoError(t, err)

	// Sync batch of 3 transactions:
	// 1. Normal txn matching exp-match-1 -> MATCHED_EXPENSE
	// 2. Normal txn without match -> SYSTEM_GENERATED (auto-create verified expense)
	// 3. Anomaly txn: volume 250L > tank 200L * 1.05 = 210L -> FLAGGED_ANOMALY
	syncReq := fuel.SyncFuelTransactionsRequest{
		Transactions: []fuel.IngestTransactionItem{
			{
				CardNumber:       cardNum,
				ExternalTxnID:    "TXN-MATCH-001",
				TxnTime:          now,
				FuelStationName:  "BPCL Highway Oasis",
				FuelType:         "DIESEL",
				VolumeLitres:     50.0,
				RatePerLitre:     90.0,
				TotalAmount:      4500.0,
				OdometerReported: floatPtr(52000.0),
			},
			{
				CardNumber:       cardNum,
				ExternalTxnID:    "TXN-SYSGEN-002",
				TxnTime:          now.Add(-2 * time.Hour),
				FuelStationName:  "BPCL Karnal Express",
				FuelType:         "DIESEL",
				VolumeLitres:     40.0,
				RatePerLitre:     90.0,
				TotalAmount:      3600.0,
				OdometerReported: floatPtr(52200.0),
			},
			{
				CardNumber:       cardNum,
				ExternalTxnID:    "TXN-ANOMALY-003",
				TxnTime:          now.Add(-4 * time.Hour),
				FuelStationName:  "BPCL Ambala Bypass",
				FuelType:         "DIESEL",
				VolumeLitres:     250.0, // Exceeds 200 * 1.05 = 210!
				RatePerLitre:     90.0,
				TotalAmount:      22500.0,
				OdometerReported: floatPtr(52400.0),
			},
		},
	}

	result, err := useCase.SyncTransactions(ctx, tenantID, syncReq)
	require.NoError(t, err)
	assert.Equal(t, 3, result.IngestedCount)
	assert.Equal(t, 1, result.MatchedCount)
	assert.Equal(t, 1, result.GeneratedCount)
	assert.Equal(t, 1, result.AnomalyCount)

	// Verify Transaction 1: MATCHED_EXPENSE
	tx1, err := repo.GetTransactionByExternalID(ctx, tenantID, "TXN-MATCH-001")
	require.NoError(t, err)
	assert.Equal(t, fuel.ReconStatusMatchedExpense, tx1.ReconciliationStatus)
	require.NotNil(t, tx1.MatchedExpenseID)
	assert.Equal(t, "exp-match-1", *tx1.MatchedExpenseID)

	// Verify the matched driver expense status was updated to approved / auto_verified
	var expStatus, verifyState string
	err = db.QueryRow(`SELECT status, verification_state FROM driver_expenses WHERE id = 'exp-match-1'`).Scan(&expStatus, &verifyState)
	require.NoError(t, err)
	assert.Equal(t, "approved", expStatus)
	assert.Equal(t, "auto_verified", verifyState)

	// Verify Transaction 2: SYSTEM_GENERATED
	tx2, err := repo.GetTransactionByExternalID(ctx, tenantID, "TXN-SYSGEN-002")
	require.NoError(t, err)
	assert.Equal(t, fuel.ReconStatusSystemGenerated, tx2.ReconciliationStatus)
	require.NotNil(t, tx2.MatchedExpenseID)

	var autoExpStatus, autoVerifyState string
	var autoExpAmount float64
	err = db.QueryRow(`SELECT status, verification_state, amount FROM driver_expenses WHERE id = $1`, *tx2.MatchedExpenseID).Scan(&autoExpStatus, &autoVerifyState, &autoExpAmount)
	require.NoError(t, err)
	assert.Equal(t, "approved", autoExpStatus)
	assert.Equal(t, "auto_verified", autoVerifyState)
	assert.Equal(t, 3600.0, autoExpAmount)

	// Verify Transaction 3: FLAGGED_ANOMALY
	tx3, err := repo.GetTransactionByExternalID(ctx, tenantID, "TXN-ANOMALY-003")
	require.NoError(t, err)
	assert.Equal(t, fuel.ReconStatusFlaggedAnomaly, tx3.ReconciliationStatus)
	assert.Contains(t, *tx3.Notes, "exceeds vehicle tank capacity")

	// Verify ops_alerts table has an anomaly record
	var alertCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM ops_alerts WHERE tenant_id = $1 AND alert_type = 'fuel_theft_confirmed'`, tenantID).Scan(&alertCount)
	require.NoError(t, err)
	assert.Equal(t, 1, alertCount)

	// Verify money_ledger double-entry records
	var debitCount, creditCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM money_ledger WHERE tenant_id = $1 AND direction = 'debit'`, tenantID).Scan(&debitCount)
	require.NoError(t, err)
	err = db.QueryRow(`SELECT COUNT(*) FROM money_ledger WHERE tenant_id = $1 AND direction = 'credit'`, tenantID).Scan(&creditCount)
	require.NoError(t, err)
	assert.Equal(t, 3, debitCount, "3 transactions should produce 3 debit entries in money_ledger")
	assert.Equal(t, 3, creditCount, "3 transactions should produce 3 credit entries in money_ledger")

	// Verify accounting_sync_log records
	var syncLogCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM accounting_sync_log WHERE entity_type = 'fuel_card_transaction'`).Scan(&syncLogCount)
	require.NoError(t, err)
	assert.Equal(t, 3, syncLogCount)
}

func TestFuelCard_ManualReconcile(t *testing.T) {
	db := setupFuelCardTestDB(t)
	tenantID := "tenant-fuel-recon"

	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Recon Logistics', 'recon-log')`, tenantID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, vehicle_type, capacity, tank_capacity_litres)
		VALUES ('veh-recon-1', $1, 'KA01AB7777', 'KA01AB7777', 'truck', 18000, 350.0)`, tenantID)
	require.NoError(t, err)

	repo := fuel.NewSQLFuelCardRepository(db)
	useCase := fuelapp.NewFuelCardUseCase(repo)
	ctx := context.Background()

	vehID := "veh-recon-1"
	cardNum := "7102999988887777"
	_, err = useCase.RegisterCard(ctx, tenantID, fuel.RegisterFuelCardRequest{
		CardNumber:        cardNum,
		Provider:          string(fuel.ProviderHPCL),
		AssignedVehicleID: &vehID,
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	syncReq := fuel.SyncFuelTransactionsRequest{
		Transactions: []fuel.IngestTransactionItem{
			{
				CardNumber:      cardNum,
				ExternalTxnID:   "TXN-UNREC-101",
				TxnTime:         now,
				FuelStationName: "HPCL Bangalore Central",
				FuelType:        "DIESEL",
				VolumeLitres:    70.0,
				RatePerLitre:    95.0,
				TotalAmount:     6650.0,
			},
		},
	}
	_, err = useCase.SyncTransactions(ctx, tenantID, syncReq)
	require.NoError(t, err)

	tx, err := repo.GetTransactionByExternalID(ctx, tenantID, "TXN-UNREC-101")
	require.NoError(t, err)

	// Create an expense to manually reconcile against
	_, err = db.Exec(`INSERT INTO driver_expenses (id, tenant_id, driver_id, category, expense_type, amount, status, created_at)
		VALUES ('exp-manual-1', $1, 'drv-sync-1', 'fuel', 'fuel', 6650.0, 'pending', $2)`, tenantID, now.Format(time.RFC3339))
	require.NoError(t, err)

	// Perform manual reconcile
	updatedTx, err := useCase.ReconcileTransaction(ctx, tenantID, tx.ID, fuel.ReconcileTransactionRequest{
		ExpenseID: "exp-manual-1",
		Notes:     "Manually confirmed with driver slip",
	})
	require.NoError(t, err)
	assert.Equal(t, fuel.ReconStatusMatchedExpense, updatedTx.ReconciliationStatus)
	assert.Equal(t, "exp-manual-1", *updatedTx.MatchedExpenseID)
	assert.Equal(t, "Manually confirmed with driver slip", *updatedTx.Notes)
}

func TestFuelCard_TenantIsolation(t *testing.T) {
	db := setupFuelCardTestDB(t)
	tenant1 := "tenant-alpha"
	tenant2 := "tenant-beta"

	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES 
		($1, 'Alpha Fleet', 'alpha-fleet'),
		($2, 'Beta Fleet', 'beta-fleet')`, tenant1, tenant2)
	require.NoError(t, err)

	repo := fuel.NewSQLFuelCardRepository(db)
	useCase := fuelapp.NewFuelCardUseCase(repo)
	ctx := context.Background()

	// Register card under tenant 1
	card1, err := useCase.RegisterCard(ctx, tenant1, fuel.RegisterFuelCardRequest{
		CardNumber: "7102111122223333",
		Provider:   string(fuel.ProviderIOCL),
	})
	require.NoError(t, err)

	// Register card with same number under tenant 2 (should succeed because hash uniqueness is per tenant)
	card2, err := useCase.RegisterCard(ctx, tenant2, fuel.RegisterFuelCardRequest{
		CardNumber: "7102111122223333",
		Provider:   string(fuel.ProviderIOCL),
	})
	require.NoError(t, err)
	assert.NotEqual(t, card1.ID, card2.ID)

	// Tenant 1 should only see its own card
	list1, err := useCase.ListCards(ctx, tenant1)
	require.NoError(t, err)
	require.Len(t, list1, 1)
	assert.Equal(t, card1.ID, list1[0].ID)

	// Tenant 2 should only see its own card
	list2, err := useCase.ListCards(ctx, tenant2)
	require.NoError(t, err)
	require.Len(t, list2, 1)
	assert.Equal(t, card2.ID, list2[0].ID)

	// Tenant 1 cannot reconcile tenant 2 card
	_, err = useCase.ReconcileTransaction(ctx, tenant1, "fake-id", fuel.ReconcileTransactionRequest{
		ExpenseID: "exp-1",
	})
	require.Error(t, err, "cross-tenant operation must fail")
}

func floatPtr(f float64) *float64 {
	return &f
}
