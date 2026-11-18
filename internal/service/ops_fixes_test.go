package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

// #9: a fuel claim with no litres and no telemetry (expected 0) used to map to
// 0% variance -> "passed". Amount > 0 with NULL litres must go to review.
func TestFuelAudit_NilLitres_ZeroExpected_NeedsReview(t *testing.T) {
	db := auditTestDB(t)
	svcs := auditTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)
	seedAuditBase(t, db, false, 0, 4.0) // no sensor, no snapshots -> expected 0

	_, err := db.Exec(`INSERT INTO driver_expenses
		(id, trip_id, driver_id, expense_type, category, amount, fuel_litres, status, created_at, tenant_id)
		VALUES ('e-nil', 't1', 'd1', 'fuel', 'fuel', 1000.0, NULL, 'pending', ?, '1')`,
		auditTimeStr(time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)

	n, err := svcs.FuelAudit.AuditPendingClaims(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	assert.Equal(t, "needs_review", auditStatusOf(t, db, "e-nil"),
		"amount>0 with no litres must not auto-pass even at zero expected")
}

// #10: ApproveExpense used to swallow the settlement-line INSERT error and
// commit anyway. A forced insert failure must fail approval and roll back.
func TestApproveExpense_SettlementLineFailure_RollsBack(t *testing.T) {
	db := auditTestDB(t)
	svcs := auditTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)
	seedAuditBase(t, db, false, 0, 4.0)

	_, err := db.Exec(`INSERT INTO driver_settlements (id, trip_id, driver_id, net_payout)
		VALUES ('stl-1', 't1', 'd1', 5000.0)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO driver_expenses
		(id, trip_id, driver_id, expense_type, category, amount, status, created_at, tenant_id)
		VALUES ('e-appr', 't1', 'd1', 'fuel', 'fuel', 1000.0, 'pending', ?, '1')`,
		auditTimeStr(time.Now()))
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TRIGGER fail_settlement_line BEFORE INSERT ON settlement_lines
		BEGIN SELECT RAISE(ABORT, 'forced line failure'); END`)
	require.NoError(t, err)

	err = svcs.Kharcha.ApproveExpense(ctx, "e-appr", "user-1")
	require.Error(t, err, "approval must surface settlement-line insert failure")

	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM driver_expenses WHERE id = 'e-appr'`).Scan(&status))
	assert.Equal(t, "pending", status, "approval must roll back")

	var payout float64
	require.NoError(t, db.QueryRow(`SELECT net_payout FROM driver_settlements WHERE id = 'stl-1'`).Scan(&payout))
	assert.InDelta(t, 5000.0, payout, 0.01, "settlement deduction must roll back")
}

// #17a: AvgVariancePct averaged every audit row across all tenants.
func TestFuelAudit_Stats_AvgVariancePct_TenantScoped(t *testing.T) {
	db := auditTestDB(t)
	svcs := auditTestServices(t, db)
	ctxA := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-a"))

	_, err := db.Exec(`INSERT INTO driver_expenses (id, expense_type, category, amount, audit_status, status, tenant_id) VALUES
		('e-a', 'fuel', 'fuel', 100, 'passed', 'approved', 'tenant-a'),
		('e-b', 'fuel', 'fuel', 100, 'passed', 'approved', 'tenant-b')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO fuel_claim_audits (id, expense_id, variance_pct, result) VALUES
		('fa-a', 'e-a', 10.0, 'passed'),
		('fa-b', 'e-b', 100.0, 'passed')`)
	require.NoError(t, err)

	st, err := svcs.FuelAudit.GetAuditStats(ctxA)
	require.NoError(t, err)
	assert.InDelta(t, 10.0, st.AvgVariancePct, 0.01,
		"stats must average only the caller tenant's audit rows")
}

// #17b: KmplReport is keyed by vehicle_id only; vehicles from other tenants leak.
func TestKmplReport_TenantScoped(t *testing.T) {
	db := auditTestDB(t)
	svcs := auditTestServices(t, db)
	ctxA := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-a"))

	_, err := db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, status, current_mileage, tenant_id) VALUES
		('veh-a', 'RNA', 'RNA', 'truck', 10, 'available', 4.0, 'tenant-a'),
		('veh-b', 'RNB', 'RNB', 'truck', 10, 'available', 4.0, 'tenant-b')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO fuel_events
		(id, vehicle_id, event_type, estimated_litres, occurred_at) VALUES
		('fe-a', 'veh-a', 'refill_detected', 20.0, '2026-08-19 09:00:00'),
		('fe-b', 'veh-b', 'refill_detected', 20.0, '2026-08-19 09:00:00')`)
	require.NoError(t, err)

	from := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	rows, err := svcs.FuelAudit.KmplReport(ctxA, from, to)
	require.NoError(t, err)
	require.Len(t, rows, 1, "only caller-tenant vehicles may appear")
	assert.Equal(t, "veh-a", rows[0].VehicleID)
}

// #17c: ReviewClaim updated the audit row by expense_id alone; a tenant could
// overwrite another tenant's audit verdict.
func TestReviewClaim_CrossTenant_Rejected(t *testing.T) {
	db := auditTestDB(t)
	svcs := auditTestServices(t, db)
	ctxA := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-a"))

	_, err := db.Exec(`INSERT INTO driver_expenses (id, expense_type, category, amount, audit_status, status, tenant_id)
		VALUES ('e-b', 'fuel', 'fuel', 100, 'needs_review', 'pending', 'tenant-b')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO fuel_claim_audits (id, expense_id, result) VALUES ('fa-b', 'e-b', 'needs_review')`)
	require.NoError(t, err)

	err = svcs.FuelAudit.ReviewClaim(ctxA, "e-b", "passed", "", "user-1")
	require.Error(t, err, "tenant A must not review tenant B's claim")

	var result string
	require.NoError(t, db.QueryRow(`SELECT result FROM fuel_claim_audits WHERE expense_id = 'e-b'`).Scan(&result))
	assert.Equal(t, "needs_review", result)
}
