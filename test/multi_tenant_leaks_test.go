package test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// Tenant contexts for cross-tenant leak tests. shared.TenantIDFromContext
// fails closed, so every read/write below is scoped by exactly these keys.
func ctxAsTenant(t string) context.Context {
	return shared.ContextWithTenantID(context.Background(), shared.TenantID(t))
}

// TestKharcha_TenantIsolation proves kharcha reads/approvals are scoped to
// the acting tenant: an expense created under acme is invisible to beta.
func TestKharcha_TenantIsolation(t *testing.T) {
	db := NewTestDB(t)
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('acme','Acme','acme'), ('beta','Beta','beta')`)
	svcs := NewTestServices(t, db)

	acme := ctxAsTenant("acme")
	beta := ctxAsTenant("beta")

	expID, err := svcs.Kharcha.CreateExpenseWithOpts(acme, service.CreateExpenseOpts{
		DriverID:    "drv-acme-1",
		Category:    "fuel",
		Amount:      1200,
		Description: "acme diesel",
		FuelLitres:  40,
	})
	require.NoError(t, err)
	require.NotEmpty(t, expID)

	// Pending queue: acme sees its expense, beta does not.
	acmePending, err := svcs.Kharcha.ListPendingExpenses(acme)
	require.NoError(t, err)
	assert.Len(t, acmePending, 1)
	assert.Equal(t, expID, acmePending[0].ID)

	betaPending, err := svcs.Kharcha.ListPendingExpenses(beta)
	require.NoError(t, err)
	assert.Empty(t, betaPending)

	// Ledger: same scoping, with and without a trip filter.
	acmeLedger, err := svcs.Kharcha.ListLedger(acme, "")
	require.NoError(t, err)
	assert.Len(t, acmeLedger, 1)

	betaLedger, err := svcs.Kharcha.ListLedger(beta, "")
	require.NoError(t, err)
	assert.Empty(t, betaLedger)

	// Single-row read fails closed across tenants, succeeds within.
	_, err = svcs.Kharcha.GetExpenseByID(beta, expID)
	require.Error(t, err, "beta must not read an acme expense")

	got, err := svcs.Kharcha.GetExpenseByID(acme, expID)
	require.NoError(t, err)
	assert.Equal(t, expID, got.ID)

	// Approval from another tenant must fail closed (no rows in scope).
	require.Error(t, svcs.Kharcha.ApproveExpense(beta, expID, "beta-admin"))
	stillPending, err := svcs.Kharcha.ListPendingExpenses(acme)
	require.NoError(t, err)
	assert.Len(t, stillPending, 1, "cross-tenant approve must not flip status")

	// Sanity: approval inside the owning tenant works.
	require.NoError(t, svcs.Kharcha.ApproveExpense(acme, expID, "acme-admin"))
	acmePending, err = svcs.Kharcha.ListPendingExpenses(acme)
	require.NoError(t, err)
	assert.Empty(t, acmePending)
}

// TestFuelAudit_TenantIsolation proves the fuel-audit queue only audits and
// lists claims belonging to the acting tenant.
func TestFuelAudit_TenantIsolation(t *testing.T) {
	db := NewTestDB(t)
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('acme','Acme','acme'), ('beta','Beta','beta')`)
	svcs := NewTestServices(t, db)

	acme := ctxAsTenant("acme")
	beta := ctxAsTenant("beta")

	// Vehicle-bearing trip so the audit pass can write an audit row for the claim.
	future := "2030-01-01"
	_, err := db.Exec(`
		INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type,
		  capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry,
		  status, tenant_id)
		VALUES ('v-acme-1','TRK-AC1','MH01AC001','truck',15.0,'diesel',?,?,?,?, 'available','acme')`,
		future, future, future, future)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('r-acme-1','Mumbai','Pune',150.0,4.0,5000.0,'acme')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, route_id, vehicle_id, driver_id, departure_time, status, tenant_id)
		VALUES ('t-acme-1','TRP-AC-01','r-acme-1','v-acme-1','drv-acme-fuel',datetime('now','-2 hours'),'in_transit','acme')`)
	require.NoError(t, err)

	_, err = svcs.Kharcha.CreateExpenseWithOpts(acme, service.CreateExpenseOpts{
		TripID:      "t-acme-1",
		DriverID:    "drv-acme-fuel",
		Category:    "fuel",
		Amount:      900,
		Description: "acme fuel claim",
		FuelLitres:  30,
	})
	require.NoError(t, err)

	// Audit pass under beta finds nothing to audit.
	betaAudited, err := svcs.FuelAudit.AuditPendingClaims(beta)
	require.NoError(t, err)
	assert.Zero(t, betaAudited)

	// Acme's own audit pass picks the claim up (and writes its audit row).
	acmeAudited, err := svcs.FuelAudit.AuditPendingClaims(acme)
	require.NoError(t, err)
	assert.Equal(t, 1, acmeAudited)

	// Beta's claim list is empty; acme sees exactly one.
	betaClaims, err := svcs.FuelAudit.ListAuditClaims(beta)
	require.NoError(t, err)
	assert.Empty(t, betaClaims)

	acmeClaims, err := svcs.FuelAudit.ListAuditClaims(acme)
	require.NoError(t, err)
	require.Len(t, acmeClaims, 1)

	// Stats are scoped too.
	betaStats, err := svcs.FuelAudit.GetAuditStats(beta)
	require.NoError(t, err)
	assert.Zero(t, betaStats.NeedsReviewCount+betaStats.PendingCount+betaStats.PassedCount+betaStats.FailedCount)
}

// TestFuelAudit_UnauditedClaimDoesNotCrash proves the LEFT JOIN NULL-scan fix:
// a fuel expense with NO fuel_claim_audits row must list and read cleanly
// (variance zero-valued, Result empty) instead of failing the whole query.
func TestFuelAudit_UnauditedClaimDoesNotCrash(t *testing.T) {
	db := NewTestDB(t)
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('acme','Acme','acme')`)
	svcs := NewTestServices(t, db)
	acme := ctxAsTenant("acme")

	expID, err := svcs.Kharcha.CreateExpenseWithOpts(acme, service.CreateExpenseOpts{
		DriverID: "drv-f-1", Category: "fuel", Amount: 500,
	})
	require.NoError(t, err)

	claims, err := svcs.FuelAudit.ListAuditClaims(acme)
	require.NoError(t, err, "unaudited fuel claim must not break ListAuditClaims")
	require.Len(t, claims, 1)
	assert.Equal(t, expID, claims[0].ExpenseID)
	assert.Zero(t, claims[0].VarianceLitres)
	assert.Empty(t, claims[0].Result)

	detail, err := svcs.FuelAudit.GetAuditDetail(acme, expID)
	require.NoError(t, err, "unaudited fuel claim must not break GetAuditDetail")
	assert.Equal(t, "fuel", detail.Category)
}
