package service_test

// Cross-tenant regression tests for the kharcha money path (audit follow-up):
// trip/driver ids are UUIDs, not an authorization boundary, and the settlement
// deduction must never reach another org's payout row.

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// seedKharchaTenantRefs creates the route/trip/driver rows a kharcha claim in
// tenant refers to. Trips and drivers are tenant-owned in production, so the
// fixtures mirror that.
func seedKharchaTenantRefs(t *testing.T, db *sql.DB, tenant, tripID, driverID string) {
	t.Helper()
	routeID := "r-" + tripID
	_, err := db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES (?, 'Mumbai', 'Pune', 150.0, 4.0, 5000.0, ?)`, routeID, tenant)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, route_id, driver_id, departure_time, status, tenant_id)
		VALUES (?, ?, ?, ?, datetime('now','-2 hours'), 'in_transit', ?)`,
		tripID, "TRP-"+tripID, routeID, driverID, tenant)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers
		(id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, tenant_id)
		VALUES (?, ?, 'Test', 'Driver', '+919000000001', ?, ?, '2030-01-01', 'available', ?)`,
		driverID, "DRV-"+driverID, driverID+"@driver.test", "DL-"+driverID, tenant)
	require.NoError(t, err)
}

func seedKharchaActor(t *testing.T, db *sql.DB, userID, email, tenant string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id)
		VALUES (?, ?, 'hash', 'Manager', 1, 'active', ?)`, userID, email, tenant)
	require.NoError(t, err)
}

// TestKharcha_CreateRejectsForeignTenantRefs proves a claim cannot be filed
// against another organization's trip or driver.
func TestKharcha_CreateRejectsForeignTenantRefs(t *testing.T) {
	dbConn, svcs := openMigratedDB(t)
	ctxA := shared.ContextWithTenantID(context.Background(), "tenant-a")

	seedKharchaTenantRefs(t, dbConn, "tenant-b", "trip-b", "drv-b")

	for name, opts := range map[string]service.CreateExpenseOpts{
		"foreign trip":   {TripID: "trip-b", Category: "toll", Amount: 100},
		"foreign driver": {DriverID: "drv-b", Category: "toll", Amount: 100},
		"both foreign":   {TripID: "trip-b", DriverID: "drv-b", Category: "toll", Amount: 100},
	} {
		_, err := svcs.Kharcha.CreateExpenseWithOpts(ctxA, opts)
		require.Error(t, err, "%s must be refused", name)
		assert.Contains(t, err.Error(), "not found in this organization", name)
	}

	var count int
	require.NoError(t, dbConn.QueryRow(`SELECT COUNT(*) FROM driver_expenses WHERE tenant_id = 'tenant-a'`).Scan(&count))
	assert.Zero(t, count, "no claim may be persisted for a foreign reference")
}

// TestKharcha_ApproveDoesNotTouchForeignTenantSettlement proves the approval
// deduction is tenant-guarded even for a pre-existing forged claim row: the
// other org's settlement row must stay untouched.
func TestKharcha_ApproveDoesNotTouchForeignTenantSettlement(t *testing.T) {
	dbConn, svcs := openMigratedDB(t)
	ctxA := shared.ContextWithTenantID(context.Background(), "tenant-a")

	seedKharchaTenantRefs(t, dbConn, "tenant-b", "trip-b", "drv-b")
	seedKharchaActor(t, dbConn, "admin-a", "admin@a.test", "tenant-a")
	_, err := dbConn.Exec(`INSERT INTO driver_settlements
		(id, trip_id, driver_id, gross_fare, advances_kharcha, net_payout, status, tenant_id)
		VALUES ('stl-b', 'trip-b', 'drv-b', 5000, 0, 4000, 'pending', 'tenant-b')`)
	require.NoError(t, err)
	// Forged claim in tenant-a referencing tenant-b's trip+driver ids.
	_, err = dbConn.Exec(`INSERT INTO driver_expenses
		(id, trip_id, driver_id, expense_type, category, amount, status, tenant_id)
		VALUES ('exp-forged', 'trip-b', 'drv-b', 'toll', 'toll', 300, 'pending', 'tenant-a')`)
	require.NoError(t, err)

	require.NoError(t, svcs.Kharcha.ApproveExpense(ctxA, "exp-forged", "admin-a"))

	var advances, payout float64
	require.NoError(t, dbConn.QueryRow(
		`SELECT advances_kharcha, net_payout FROM driver_settlements WHERE id = 'stl-b'`).Scan(&advances, &payout))
	assert.Zero(t, advances, "foreign tenant settlement must not be adjusted")
	assert.Equal(t, 4000.0, payout, "foreign tenant payout must not change")
	var lines int
	require.NoError(t, dbConn.QueryRow(`SELECT COUNT(*) FROM settlement_lines WHERE settlement_id = 'stl-b'`).Scan(&lines))
	assert.Zero(t, lines, "no deduction line may be written into a foreign settlement")
}

// TestKharcha_ApproveAdjustsLegacySettlementWithoutTenant keeps the reason for
// commit b55dd94: settlements written before tenant_id was populated must keep
// adjusting, otherwise approving an expense silently stops moving money.
func TestKharcha_ApproveAdjustsLegacySettlementWithoutTenant(t *testing.T) {
	dbConn, svcs := openMigratedDB(t)
	ctxA := shared.ContextWithTenantID(context.Background(), "tenant-a")

	seedKharchaTenantRefs(t, dbConn, "tenant-a", "trip-a", "drv-a")
	seedKharchaActor(t, dbConn, "admin-a", "admin@a.test", "tenant-a")
	_, err := dbConn.Exec(`INSERT INTO driver_settlements
		(id, trip_id, driver_id, gross_fare, advances_kharcha, net_payout, status, tenant_id)
		VALUES ('stl-legacy', 'trip-a', 'drv-a', 5000, 0, 4000, 'pending', '')`)
	require.NoError(t, err)

	expID, err := svcs.Kharcha.CreateExpense(ctxA, "trip-a", "drv-a", "toll", 500.0, "Toll plaza", "", 0)
	require.NoError(t, err)
	require.NoError(t, svcs.Kharcha.ApproveExpense(ctxA, expID, "admin-a"))

	var advances, payout float64
	require.NoError(t, dbConn.QueryRow(
		`SELECT advances_kharcha, net_payout FROM driver_settlements WHERE id = 'stl-legacy'`).Scan(&advances, &payout))
	assert.Equal(t, 500.0, advances, "legacy settlement without tenant_id must still be adjusted")
	assert.Equal(t, 3500.0, payout, "legacy net payout must be reduced by the claim")
}
