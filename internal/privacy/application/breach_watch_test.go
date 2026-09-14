package application

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"transport-app/internal/events"
	legacy "transport-app/internal/service"
	"transport-app/internal/shared/id"
)

// Overdue breach (real write path via ReportBreach, then backdated
// detected_at) raises exactly one compliance_breach ops alert; a fresh
// breach in the same tenant raises nothing.
func TestBreachWatch_SweepOverdueRaisesOnce(t *testing.T) {
	db := newPrivacyTestDB(t)
	privacy := NewPrivacyService(db, id.NewUUIDGenerator())
	alerts := legacy.NewOpsAlertServiceForTest(db, events.NewInMemoryBus())
	watch := NewBreachWatchService(db, privacy, alerts, slog.Default())
	ctx := context.Background()
	const tenant = "tenant_breach_watch_1"

	seedPrivacyTenant(t, db, tenant)

	overdueID, err := privacy.ReportBreach(ctx, tenant, "lost drive", "", "", "", 10)
	require.NoError(t, err)
	freshID, err := privacy.ReportBreach(ctx, tenant, "phish click", "", "", "", 1)
	require.NoError(t, err)

	_, err = db.Exec(`UPDATE breach_incidents SET detected_at = $1 WHERE id = $2`,
		time.Now().UTC().Add(-73*time.Hour).Format("2006-01-02 15:04:05"), overdueID)
	require.NoError(t, err)

	raised, err := watch.SweepOverdue(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, raised, "only the overdue incident (%s) may raise; fresh %s must not", overdueID, freshID)

	got, total, err := alerts.ListAlerts(ctx, tenant, legacy.OpsAlertFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, got, 1)
	require.Equal(t, legacy.OpsAlertComplianceBreach, got[0].AlertType)
	require.NotNil(t, got[0].EntityID)
	require.Equal(t, overdueID, *got[0].EntityID)
	require.Equal(t, legacy.OpsAlertStatusOpen, got[0].Status)
}

// Second sweep raises nothing for the already-alerted incident (dedupe via
// entity_type/entity_id); acknowledged alerts also suppress re-raise.
func TestBreachWatch_SweepOverdueDedupes(t *testing.T) {
	db := newPrivacyTestDB(t)
	privacy := NewPrivacyService(db, id.NewUUIDGenerator())
	alerts := legacy.NewOpsAlertServiceForTest(db, events.NewInMemoryBus())
	watch := NewBreachWatchService(db, privacy, alerts, slog.Default())
	ctx := context.Background()
	const tenant = "tenant_breach_watch_2"

	seedPrivacyTenant(t, db, tenant)

	id, err := privacy.ReportBreach(ctx, tenant, "old leak", "", "", "", 5)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE breach_incidents SET detected_at = $1 WHERE id = $2`,
		time.Now().UTC().Add(-80*time.Hour).Format("2006-01-02 15:04:05"), id)
	require.NoError(t, err)

	raised, err := watch.SweepTenant(ctx, tenant)
	require.NoError(t, err)
	require.Equal(t, 1, raised)

	raised, err = watch.SweepTenant(ctx, tenant)
	require.NoError(t, err)
	require.Equal(t, 0, raised, "already-alerted incident must not double-raise")

	_, total, err := alerts.ListAlerts(ctx, tenant, legacy.OpsAlertFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, total)

	// Acknowledged (not just open) still suppresses.
	listed, _, err := alerts.ListAlerts(ctx, tenant, legacy.OpsAlertFilters{})
	require.NoError(t, err)
	require.NoError(t, alerts.AcknowledgeAlert(ctx, listed[0].ID, "u1"))
	raised, err = watch.SweepTenant(ctx, tenant)
	require.NoError(t, err)
	require.Equal(t, 0, raised)
}

// Tenant with only fresh incidents contributes zero alerts.
func TestBreachWatch_SweepTenantFreshOnly(t *testing.T) {
	db := newPrivacyTestDB(t)
	privacy := NewPrivacyService(db, id.NewUUIDGenerator())
	alerts := legacy.NewOpsAlertServiceForTest(db, events.NewInMemoryBus())
	watch := NewBreachWatchService(db, privacy, alerts, slog.Default())
	ctx := context.Background()
	const tenant = "tenant_breach_watch_3"

	seedPrivacyTenant(t, db, tenant)
	_, err := privacy.ReportBreach(ctx, tenant, "just happened", "", "", "", 2)
	require.NoError(t, err)

	raised, err := watch.SweepTenant(ctx, tenant)
	require.NoError(t, err)
	require.Equal(t, 0, raised)
}
