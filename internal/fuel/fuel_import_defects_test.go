package fuel_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/fuel"
	fuelapp "transport-app/internal/fuel/application"
)

func setupFuelImportDefectsDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_fuel_import_defects_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))
	return db
}

func seedFuelImportTenant(t *testing.T, db *sql.DB, tenantID string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, $2, $3)`, tenantID, tenantID, tenantID)
	require.NoError(t, err)
}

func seedFuelImportVehicleDriver(t *testing.T, db *sql.DB, tenantID, vehID, drvID string, tankCap float64) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, vehicle_type, capacity, tank_capacity_litres)
		VALUES ($1, $2, $3, $3, 'truck', 10000, $4)`, vehID, tenantID, "VN-"+vehID, tankCap)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, tenant_id, driver_id, first_name, last_name, phone)
		VALUES ($1, $2, $3, 'F', 'L', '9999999999')`, drvID, tenantID, "D-"+drvID)
	require.NoError(t, err)
}

func registerFuelImportCard(t *testing.T, ctx context.Context, uc *fuelapp.FuelCardUseCase, tenantID, cardNum, vehID, drvID string) {
	t.Helper()
	_, err := uc.RegisterCard(ctx, tenantID, fuel.RegisterFuelCardRequest{
		CardNumber:        cardNum,
		Provider:          string(fuel.ProviderIOCL),
		AssignedVehicleID: &vehID,
		AssignedDriverID:  &drvID,
		DailySpendLimit:   50000,
	})
	require.NoError(t, err)
}

// (a) Ledger references must carry the real fuel_card_transactions.id.
// Defect: usecase:134-165 builds txn without ID then posts GL with empty
// txn.ID; repo:283-338 writes money_ledger.ref_id="" and
// accounting_sync_log.entity_id="" before InsertTransaction generates the ID.
func TestFuelImport_LedgerCarriesRealTransactionID(t *testing.T) {
	db := setupFuelImportDefectsDB(t)
	tenantID := "tenant-ledger-id"
	seedFuelImportTenant(t, db, tenantID)
	seedFuelImportVehicleDriver(t, db, tenantID, "veh-ledger-1", "drv-ledger-1", 300.0)

	repo := fuel.NewSQLFuelCardRepository(db)
	uc := fuelapp.NewFuelCardUseCase(repo)
	ctx := context.Background()
	cardNum := "7102123456789001"
	registerFuelImportCard(t, ctx, uc, tenantID, cardNum, "veh-ledger-1", "drv-ledger-1")

	now := time.Now().UTC()
	_, err := uc.SyncTransactions(ctx, tenantID, fuel.SyncFuelTransactionsRequest{
		Transactions: []fuel.IngestTransactionItem{
			{
				CardNumber:      cardNum,
				ExternalTxnID:   "TXN-LEDGER-001",
				TxnTime:         now,
				FuelStationName: "IOCL Highway",
				FuelType:        "DIESEL",
				VolumeLitres:    50.0,
				RatePerLitre:    90.0,
				TotalAmount:     4500.0,
			},
		},
	})
	require.NoError(t, err)

	txn, err := repo.GetTransactionByExternalID(ctx, tenantID, "TXN-LEDGER-001")
	require.NoError(t, err)
	require.NotEmpty(t, txn.ID, "inserted txn must have a real ID")

	var linked int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM money_ledger WHERE tenant_id=$1 AND ref_table='fuel_card_transactions' AND ref_id=$2`,
		tenantID, txn.ID).Scan(&linked))
	require.Equal(t, 2, linked, "both debit+credit ledger rows must reference the real txn ID")

	var orphans int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM money_ledger WHERE tenant_id=$1 AND ref_table='fuel_card_transactions' AND (ref_id='' OR ref_id IS NULL)`,
		tenantID).Scan(&orphans))
	require.Equal(t, 0, orphans, "no ledger row may carry an empty ref_id")

	var entityID string
	require.NoError(t, db.QueryRow(
		`SELECT entity_id FROM accounting_sync_log WHERE idempotency_key=$1`,
		"fuel-card-txn-TXN-LEDGER-001").Scan(&entityID))
	require.Equal(t, txn.ID, entityID, "sync log entity_id must be the real txn ID")
}

// (b) A failed fuel_card_transactions insert must roll back GL side effects.
// Defect: GL posted before insert with no txn; ledger write errors ignored
// (`_, _ =`); use-case swallows insert errors and returns nil.
func TestFuelImport_FailedInsertRollsBackGL(t *testing.T) {
	db := setupFuelImportDefectsDB(t)
	tenantID := "tenant-rollback"
	seedFuelImportTenant(t, db, tenantID)
	seedFuelImportVehicleDriver(t, db, tenantID, "veh-rb-1", "drv-rb-1", 300.0)

	repo := fuel.NewSQLFuelCardRepository(db)
	uc := fuelapp.NewFuelCardUseCase(repo)
	ctx := context.Background()
	cardNum := "7102123456789002"
	registerFuelImportCard(t, ctx, uc, tenantID, cardNum, "veh-rb-1", "drv-rb-1")

	now := time.Now().UTC()
	// total_amount=0 violates fuel_card_transactions CHECK (total_amount > 0),
	// so the txn insert must fail while the earlier GL writes would succeed.
	result, err := uc.SyncTransactions(ctx, tenantID, fuel.SyncFuelTransactionsRequest{
		Transactions: []fuel.IngestTransactionItem{
			{
				CardNumber:      cardNum,
				ExternalTxnID:   "TXN-ROLLBACK-001",
				TxnTime:         now,
				FuelStationName: "IOCL Bypass",
				FuelType:        "DIESEL",
				VolumeLitres:    10.0,
				RatePerLitre:    90.0,
				TotalAmount:     0,
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.IngestedCount, "invalid txn must not ingest (proves insert failed)")

	var syncCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM accounting_sync_log WHERE idempotency_key=$1`,
		"fuel-card-txn-TXN-ROLLBACK-001").Scan(&syncCount))
	require.Equal(t, 0, syncCount, "failed insert must not leave an orphaned sync-log row")

	var ledgerCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM money_ledger WHERE tenant_id=$1`, tenantID).Scan(&ledgerCount))
	require.Equal(t, 0, ledgerCount, "failed insert must not leave orphaned ledger rows")
}

// (c) Replaying the same external_txn_id must not duplicate accounting side effects.
// Defect: no idempotency guard before pilferage/expense/GL work; replay
// re-runs RecordPilferageAlert and bumps Anomaly/Matched counts even though
// the txn insert fails on UNIQUE(tenant_id, external_txn_id).
func TestFuelImport_ReplayDoesNotDuplicateAccounting(t *testing.T) {
	db := setupFuelImportDefectsDB(t)
	tenantID := "tenant-replay"
	seedFuelImportTenant(t, db, tenantID)
	seedFuelImportVehicleDriver(t, db, tenantID, "veh-replay-1", "drv-replay-1", 200.0)

	repo := fuel.NewSQLFuelCardRepository(db)
	uc := fuelapp.NewFuelCardUseCase(repo)
	ctx := context.Background()
	cardNum := "7102123456789003"
	registerFuelImportCard(t, ctx, uc, tenantID, cardNum, "veh-replay-1", "drv-replay-1")

	now := time.Now().UTC()
	payload := fuel.SyncFuelTransactionsRequest{
		Transactions: []fuel.IngestTransactionItem{
			{
				CardNumber:      cardNum,
				ExternalTxnID:   "TXN-REPLAY-001",
				TxnTime:         now,
				FuelStationName: "IOCL Ambala",
				FuelType:        "DIESEL",
				VolumeLitres:    250.0, // > 200*1.05 => anomaly path
				RatePerLitre:    90.0,
				TotalAmount:     22500.0,
			},
		},
	}

	first, err := uc.SyncTransactions(ctx, tenantID, payload)
	require.NoError(t, err)
	require.Equal(t, 1, first.IngestedCount)
	require.Equal(t, 1, first.AnomalyCount)

	countOf := func(q string, args ...any) int {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRow(q, args...).Scan(&n))
		return n
	}
	ledgerBefore := countOf(`SELECT COUNT(*) FROM money_ledger WHERE tenant_id=$1`, tenantID)
	syncBefore := countOf(`SELECT COUNT(*) FROM accounting_sync_log WHERE idempotency_key=$1`, "fuel-card-txn-TXN-REPLAY-001")
	txnBefore := countOf(`SELECT COUNT(*) FROM fuel_card_transactions WHERE tenant_id=$1`, tenantID)
	alertBefore := countOf(`SELECT COUNT(*) FROM ops_alerts WHERE tenant_id=$1 AND alert_type='fuel_theft_confirmed'`, tenantID)
	require.Equal(t, 2, ledgerBefore)
	require.Equal(t, 1, syncBefore)
	require.Equal(t, 1, txnBefore)
	require.Equal(t, 1, alertBefore)

	second, err := uc.SyncTransactions(ctx, tenantID, payload)
	require.NoError(t, err)
	require.Equal(t, 0, second.IngestedCount, "replay must not ingest a second txn")
	require.Equal(t, 0, second.AnomalyCount, "replay must not recount the anomaly")

	require.Equal(t, ledgerBefore, countOf(`SELECT COUNT(*) FROM money_ledger WHERE tenant_id=$1`, tenantID), "replay must not duplicate ledger rows")
	require.Equal(t, syncBefore, countOf(`SELECT COUNT(*) FROM accounting_sync_log WHERE idempotency_key=$1`, "fuel-card-txn-TXN-REPLAY-001"), "replay must not duplicate sync-log rows")
	require.Equal(t, txnBefore, countOf(`SELECT COUNT(*) FROM fuel_card_transactions WHERE tenant_id=$1`, tenantID), "replay must not duplicate txn rows")
	require.Equal(t, alertBefore, countOf(`SELECT COUNT(*) FROM ops_alerts WHERE tenant_id=$1 AND alert_type='fuel_theft_confirmed'`, tenantID), "replay must not duplicate pilferage alerts")
}

// (d) Cross-tenant expense links must be rejected.
// Defects: usecase:40-51 takes assigned IDs without ownership lookup;
// repo:454-468 links any expenseID without verifying its tenant, and the
// follow-up MarkExpenseVerified is fire-and-forget (`_ =`).
func TestFuelImport_CrossTenantExpenseLinkRejected(t *testing.T) {
	db := setupFuelImportDefectsDB(t)
	tenantA := "tenant-xa"
	tenantB := "tenant-xb"
	seedFuelImportTenant(t, db, tenantA)
	seedFuelImportTenant(t, db, tenantB)
	seedFuelImportVehicleDriver(t, db, tenantA, "veh-xa-1", "drv-xa-1", 300.0)
	seedFuelImportVehicleDriver(t, db, tenantB, "veh-xb-1", "drv-xb-1", 300.0)

	repo := fuel.NewSQLFuelCardRepository(db)
	uc := fuelapp.NewFuelCardUseCase(repo)
	ctx := context.Background()

	cardNum := "7102123456789004"
	registerFuelImportCard(t, ctx, uc, tenantA, cardNum, "veh-xa-1", "drv-xa-1")

	now := time.Now().UTC()
	_, err := uc.SyncTransactions(ctx, tenantA, fuel.SyncFuelTransactionsRequest{
		Transactions: []fuel.IngestTransactionItem{
			{
				CardNumber:      cardNum,
				ExternalTxnID:   "TXN-XA-001",
				TxnTime:         now,
				FuelStationName: "IOCL City",
				FuelType:        "DIESEL",
				VolumeLitres:    40.0,
				RatePerLitre:    90.0,
				TotalAmount:     3600.0,
			},
		},
	})
	require.NoError(t, err)
	txn, err := repo.GetTransactionByExternalID(ctx, tenantA, "TXN-XA-001")
	require.NoError(t, err)

	// Expense owned by tenant B.
	_, err = db.Exec(`INSERT INTO driver_expenses (id, tenant_id, driver_id, category, expense_type, amount, status, created_at)
		VALUES ('exp-foreign-1', $1, 'drv-xb-1', 'fuel', 'fuel', 3600.0, 'pending', $2)`,
		tenantB, now.Format(time.RFC3339))
	require.NoError(t, err)

	_, err = uc.ReconcileTransaction(ctx, tenantA, txn.ID, fuel.ReconcileTransactionRequest{
		ExpenseID: "exp-foreign-1",
		Notes:     "cross-tenant link attempt",
	})
	require.Error(t, err, "linking tenant-A txn to tenant-B expense must be rejected")

	fresh, err := repo.GetTransactionByExternalID(ctx, tenantA, "TXN-XA-001")
	require.NoError(t, err)
	if fresh.MatchedExpenseID != nil {
		require.NotEqual(t, "exp-foreign-1", *fresh.MatchedExpenseID, "txn must not remain linked to foreign expense")
	}

	// Registering a card in A against B's vehicle must also be rejected.
	_, err = uc.RegisterCard(ctx, tenantA, fuel.RegisterFuelCardRequest{
		CardNumber:        "7102123456789005",
		Provider:          string(fuel.ProviderIOCL),
		AssignedVehicleID: strPtrFuelImport("veh-xb-1"),
	})
	require.Error(t, err, "assigning a foreign-tenant vehicle must be rejected")
}

func strPtrFuelImport(s string) *string { return &s }
