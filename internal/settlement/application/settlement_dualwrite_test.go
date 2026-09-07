package application_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/settlement/application"
	settleSQL "transport-app/internal/settlement/infrastructure/persistence/sql"
)

const legacyLinesSchema = `
CREATE TABLE settlement_lines (
	id TEXT PRIMARY KEY,
	settlement_id TEXT NOT NULL,
	trip_id TEXT NOT NULL,
	line_type TEXT NOT NULL CHECK (line_type IN ('gross_fare','commission','advances','deduction','tds','adjustment')),
	label TEXT NOT NULL,
	amount REAL NOT NULL,
	ref_id TEXT,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);`

// TestDualWriteGuard_ReusesLegacySettlement: when the legacy trip-close flow
// already accounted a trip (driver_settlements row + settlement_lines), the
// wallet rail must reuse the single row — no error (soft guard, never fail
// hard) and zero duplicate ledger appends.
func TestDualWriteGuard_ReusesLegacySettlement(t *testing.T) {
	db := setupSettlementTestDB(t)
	_, err := db.Exec(legacyLinesSchema)
	require.NoError(t, err)

	tenantID := "tenant-1"
	driverID := "drv-settle-1"
	tripID := "trip-legacy-4242"

	// Legacy-style header: disputed status (legacy vocabulary, never written by
	// the wallet rail) with NULL wallet-rail breakdown columns.
	_, err = db.Exec(`INSERT INTO driver_settlements
		(id, tenant_id, trip_id, driver_id, gross_fare, deductions, net_payout, status)
		VALUES ('stl-legacy-1', ?, ?, ?, 2000.0, 518.0, 1482.0, 'disputed')`,
		tenantID, tripID, driverID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO settlement_lines
		(id, settlement_id, trip_id, line_type, label, amount)
		VALUES ('ln-1', 'stl-legacy-1', ?, 'gross_fare', 'Trip fare', 2000.0),
		       ('ln-2', 'stl-legacy-1', ?, 'commission', 'Platform commission', -200.0)`,
		tripID, tripID)
	require.NoError(t, err)

	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	ctx := context.Background()

	// Cross-check query sees the legacy breakdown.
	hasLegacy, err := repo.HasLegacySettlementLines(ctx, tripID)
	require.NoError(t, err)
	assert.True(t, hasLegacy)

	// Wallet rail reuses the legacy row instead of dual-writing.
	got, err := svc.CalculateAndCreateSettlement(ctx, tenantID, application.CalculateSettlementRequest{
		TripID:         tripID,
		DriverID:       driverID,
		GrossFare:      2000.0,
		CommissionRate: 0.10,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "stl-legacy-1", got.ID)
	assert.Equal(t, "disputed", got.Status)

	// No duplicate ledger entries appended for the reused trip.
	var ledgerCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM driver_ledger_entries WHERE driver_id = ?`, driverID).Scan(&ledgerCount))
	assert.Equal(t, 0, ledgerCount)
}

// TestDualWriteGuard_FailOpenWithoutLegacyTable: on DBs without the
// settlement_lines table the cross-check errors and the guard must fail open —
// settlement creation proceeds normally (never break prod).
func TestDualWriteGuard_FailOpenWithoutLegacyTable(t *testing.T) {
	db := setupSettlementTestDB(t) // no settlement_lines table by design
	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	ctx := context.Background()

	// Cross-check surfaces the missing table as an error (guard logs + continues).
	_, err := repo.HasLegacySettlementLines(ctx, "trip-failopen-1")
	require.Error(t, err)

	got, err := svc.CalculateAndCreateSettlement(ctx, "tenant-1", application.CalculateSettlementRequest{
		TripID:         "trip-failopen-1",
		DriverID:       "drv-settle-1",
		GrossFare:      1500.0,
		CommissionRate: 0.10,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 1500.0, got.GrossFare)
}

// TestHasLegacySettlementLines_FalseWhenNoLines: no breakdown rows (or rows
// for other trips) means the legacy flow does not own the trip.
func TestHasLegacySettlementLines_FalseWhenNoLines(t *testing.T) {
	db := setupSettlementTestDB(t)
	_, err := db.Exec(legacyLinesSchema)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO settlement_lines
		(id, settlement_id, trip_id, line_type, label, amount)
		VALUES ('ln-other', 'stl-other', 'trip-other-9', 'gross_fare', 'Other trip', 500.0)`)
	require.NoError(t, err)

	repo := settleSQL.NewSQLSettlementRepository(db)
	ctx := context.Background()

	hasLegacy, err := repo.HasLegacySettlementLines(ctx, "trip-unseen-7")
	require.NoError(t, err)
	assert.False(t, hasLegacy)

	hasLegacy, err = repo.HasLegacySettlementLines(ctx, "")
	require.NoError(t, err)
	assert.False(t, hasLegacy)
}
