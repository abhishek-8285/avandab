package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Overdue breach (real write path via ReportBreach, then backdated
// detected_at) raises exactly one compliance_breach ops alert; a fresh
// breach in the same tenant raises nothing.
func TestBreachWatch_SweepOverdueRaisesOnce(t *testing.T) {
	svcs := newPrivacyTestServices(t)
	ctx := context.Background()
	const tenant = "tenant_breach_watch_1"

	require.NoError(t, execTenant(t, svcs, tenant))
	require.NotNil(t, svcs.BreachWatch, "BreachWatch must be wired by NewServices over sqlite")

	overdueID, err := svcs.Privacy.ReportBreach(ctx, tenant, "lost drive", "", "", "", 10)
	require.NoError(t, err)
	freshID, err := svcs.Privacy.ReportBreach(ctx, tenant, "phish click", "", "", "", 1)
	require.NoError(t, err)

	db := svcs.DB()
	require.NotNil(t, db)
	_, err = db.Exec(`UPDATE breach_incidents SET detected_at = $1 WHERE id = $2`,
		time.Now().UTC().Add(-73*time.Hour).Format("2006-01-02 15:04:05"), overdueID)
	require.NoError(t, err)

	raised, err := svcs.BreachWatch.SweepOverdue(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, raised, "only the overdue incident (%s) may raise; fresh %s must not", overdueID, freshID)

	alerts, total, err := svcs.OpsAlerts.ListAlerts(ctx, tenant, OpsAlertFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, alerts, 1)
	require.Equal(t, OpsAlertComplianceBreach, alerts[0].AlertType)
	require.NotNil(t, alerts[0].EntityID)
	require.Equal(t, overdueID, *alerts[0].EntityID)
	require.Equal(t, OpsAlertStatusOpen, alerts[0].Status)
}

// Second sweep raises nothing for the already-alerted incident (dedupe via
// entity_type/entity_id); acknowledged alerts also suppress re-raise.
func TestBreachWatch_SweepOverdueDedupes(t *testing.T) {
	svcs := newPrivacyTestServices(t)
	ctx := context.Background()
	const tenant = "tenant_breach_watch_2"

	require.NoError(t, execTenant(t, svcs, tenant))

	id, err := svcs.Privacy.ReportBreach(ctx, tenant, "old leak", "", "", "", 5)
	require.NoError(t, err)
	db := svcs.DB()
	_, err = db.Exec(`UPDATE breach_incidents SET detected_at = $1 WHERE id = $2`,
		time.Now().UTC().Add(-80*time.Hour).Format("2006-01-02 15:04:05"), id)
	require.NoError(t, err)

	raised, err := svcs.BreachWatch.SweepTenant(ctx, tenant)
	require.NoError(t, err)
	require.Equal(t, 1, raised)

	raised, err = svcs.BreachWatch.SweepTenant(ctx, tenant)
	require.NoError(t, err)
	require.Equal(t, 0, raised, "already-alerted incident must not double-raise")

	_, total, err := svcs.OpsAlerts.ListAlerts(ctx, tenant, OpsAlertFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, total)

	// Acknowledged (not just open) still suppresses.
	alerts, _, err := svcs.OpsAlerts.ListAlerts(ctx, tenant, OpsAlertFilters{})
	require.NoError(t, err)
	require.NoError(t, svcs.OpsAlerts.AcknowledgeAlert(ctx, alerts[0].ID, "u1"))
	raised, err = svcs.BreachWatch.SweepTenant(ctx, tenant)
	require.NoError(t, err)
	require.Equal(t, 0, raised)
}

// Tenant with only fresh incidents contributes zero alerts.
func TestBreachWatch_SweepTenantFreshOnly(t *testing.T) {
	svcs := newPrivacyTestServices(t)
	ctx := context.Background()
	const tenant = "tenant_breach_watch_3"

	require.NoError(t, execTenant(t, svcs, tenant))
	_, err := svcs.Privacy.ReportBreach(ctx, tenant, "just happened", "", "", "", 2)
	require.NoError(t, err)

	raised, err := svcs.BreachWatch.SweepTenant(ctx, tenant)
	require.NoError(t, err)
	require.Equal(t, 0, raised)
}
