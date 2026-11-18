package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

// A retry after a post-commit ledger failure must backfill the missing ledger
// entries instead of returning the orphan header untouched.
func TestGenerateSettlement_RetryBackfillsLedger(t *testing.T) {
	dbConn, svcs, _ := setupComplianceTestDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))
	tripID := "trip-ledger-backfill"

	settlementID := seedMarkPaidSettlement(t, ctx, dbConn, svcs, tripID)

	var before int
	require.NoError(t, dbConn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM driver_ledger_entries WHERE reference_type = 'settlement' AND reference_id = ?`, settlementID).Scan(&before))
	require.Greater(t, before, 0, "first generate must append ledger entries")

	// Simulate a post-commit failure: header survives, ledger write lost.
	_, err := dbConn.ExecContext(ctx, `DELETE FROM driver_ledger_entries WHERE reference_id = ?`, settlementID)
	require.NoError(t, err)

	rec, err := svcs.Settlements.GenerateSettlement(ctx, tripID, false)
	require.NoError(t, err)
	require.NotNil(t, rec)

	var after int
	require.NoError(t, dbConn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM driver_ledger_entries WHERE reference_type = 'settlement' AND reference_id = ?`, settlementID).Scan(&after))
	assert.Greater(t, after, 0, "idempotent retry must backfill the missing ledger entries")
}
