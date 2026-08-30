package application_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/entitlement/application"
	"transport-app/internal/entitlement/domain"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	migrationsDir := filepath.Join(filepath.Dir(filename), "..", "..", "..", "db", "migrations")

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))

	// Seed test tenants
	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug, status) VALUES ('tenant-a', 'Tenant Alpha', 'tenant-a', 'active'), ('tenant-b', 'Tenant Beta', 'tenant-b', 'active'), ('tenant-pilot', 'Pilot Tenant', 'tenant-pilot', 'active');`)
	require.NoError(t, err)

	return db
}

func TestEntitlement_QuotaLimit_10thSucceeds_11thFails(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	ctx := context.Background()

	now := time.Now().UTC()
	start := now.Add(-24 * time.Hour)
	end := now.Add(30 * 24 * time.Hour)

	_, err := svc.CreateSubscription(ctx, "tenant-a", domain.PlanStarter, domain.SubTrial, start, end)
	require.NoError(t, err)

	// Consume 9 trips
	for i := 1; i <= 9; i++ {
		idemKey := fmt.Sprintf("trip_booking_%d", i)
		err := svc.ReserveQuota(ctx, nil, "tenant-a", domain.QuotaMaxTripsPerMonth, 1, idemKey, "booking", fmt.Sprintf("bk_%d", i))
		require.NoError(t, err, "Trip %d reservation should succeed", i)
		err = svc.CommitQuota(ctx, nil, "tenant-a", domain.QuotaMaxTripsPerMonth, 1, idemKey, "booking", fmt.Sprintf("bk_%d", i))
		require.NoError(t, err, "Trip %d commit should succeed", i)
	}

	// 10th trip succeeds (reaches max limit 10/10)
	err = svc.ReserveQuota(ctx, nil, "tenant-a", domain.QuotaMaxTripsPerMonth, 1, "trip_booking_10", "booking", "bk_10")
	assert.NoError(t, err, "10th trip should succeed on Starter plan")
	err = svc.CommitQuota(ctx, nil, "tenant-a", domain.QuotaMaxTripsPerMonth, 1, "trip_booking_10", "booking", "bk_10")
	assert.NoError(t, err)

	// 11th trip MUST fail with ErrQuotaExceeded
	err = svc.ReserveQuota(ctx, nil, "tenant-a", domain.QuotaMaxTripsPerMonth, 1, "trip_booking_11", "booking", "bk_11")
	assert.ErrorIs(t, err, domain.ErrQuotaExceeded, "11th trip must be rejected with ErrQuotaExceeded")
}

func TestEntitlement_TwoPhaseReservation_ReleaseOnFailure(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	ctx := context.Background()

	now := time.Now().UTC()
	_, err := svc.CreateSubscription(ctx, "tenant-a", domain.PlanStarter, domain.SubTrial, now.Add(-1*time.Hour), now.Add(30*24*time.Hour))
	require.NoError(t, err)

	// Reserve for booking that later fails
	idemKey := "failed_booking_attempt"
	err = svc.ReserveQuota(ctx, nil, "tenant-a", domain.QuotaMaxTripsPerMonth, 1, idemKey, "booking", "bk_fail")
	require.NoError(t, err)

	meter, err := svc.CheckQuota(ctx, "tenant-a", domain.QuotaMaxTripsPerMonth)
	require.NoError(t, err)
	assert.Equal(t, 1, meter.Reserved)
	assert.Equal(t, 9, meter.Remaining)

	// Simulate failure -> release reservation
	err = svc.ReleaseQuota(ctx, nil, "tenant-a", domain.QuotaMaxTripsPerMonth, 1)
	require.NoError(t, err)

	meterAfter, err := svc.CheckQuota(ctx, "tenant-a", domain.QuotaMaxTripsPerMonth)
	require.NoError(t, err)
	assert.Equal(t, 0, meterAfter.Reserved)
	assert.Equal(t, 0, meterAfter.Used)
	assert.Equal(t, 10, meterAfter.Remaining, "Capacity must be fully restored without loss")
}

func TestEntitlement_ConcurrentReservationRace(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	ctx := context.Background()

	now := time.Now().UTC()
	_, err := svc.CreateSubscription(ctx, "tenant-a", domain.PlanStarter, domain.SubTrial, now.Add(-1*time.Hour), now.Add(30*24*time.Hour))
	require.NoError(t, err)

	// Consume 9 trips so exactly 1 slot remains
	for i := 1; i <= 9; i++ {
		idemKey := fmt.Sprintf("pre_trip_%d", i)
		require.NoError(t, svc.ReserveQuota(ctx, nil, "tenant-a", domain.QuotaMaxTripsPerMonth, 1, idemKey, "booking", fmt.Sprintf("b_%d", i)))
		require.NoError(t, svc.CommitQuota(ctx, nil, "tenant-a", domain.QuotaMaxTripsPerMonth, 1, idemKey, "booking", fmt.Sprintf("b_%d", i)))
	}

	// 10 concurrent goroutines vying for the single remaining 10th slot
	concurrency := 10
	var wg sync.WaitGroup
	successCount := 0
	failureCount := 0
	var mu sync.Mutex

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			idem := fmt.Sprintf("concurrent_slot_%d", idx)
			err := svc.ReserveQuota(ctx, nil, "tenant-a", domain.QuotaMaxTripsPerMonth, 1, idem, "booking", fmt.Sprintf("b_conc_%d", idx))
			mu.Lock()
			if err == nil {
				successCount++
			} else if err == domain.ErrQuotaExceeded {
				failureCount++
			} else {
				t.Logf("Goroutine %d returned unexpected error: %v", idx, err)
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 1, successCount, "Exactly ONE concurrent caller must win the last slot")
	assert.Equal(t, 9, failureCount, "All other concurrent callers must receive ErrQuotaExceeded")
}

func TestEntitlement_ReadOnly_AllowsInFlightTrips_BlocksIngress(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	ctx := context.Background()

	now := time.Now().UTC()
	_, err := svc.CreateSubscription(ctx, "tenant-a", domain.PlanGrowth, domain.SubReadOnly, now.Add(-30*24*time.Hour), now.Add(-1*time.Hour))
	require.NoError(t, err)

	// Ingress operations MUST be blocked
	ingressOps := []domain.OpCode{
		domain.OpCreateBooking,
		domain.OpCreateDispatch,
		domain.OpCreateVehicle,
		domain.OpCreateDriver,
	}
	for _, op := range ingressOps {
		allowed, err := svc.CanExecuteOperation(ctx, "tenant-a", op, "")
		assert.False(t, allowed, "Op %s should be blocked in READ_ONLY", op)
		assert.ErrorIs(t, err, domain.ErrOperationBlocked)
	}

	// In-flight operational execution MUST be allowed to safely complete
	inFlightOps := []domain.OpCode{
		domain.OpIngestTelemetry,
		domain.OpCompleteStop,
		domain.OpCompleteTrip,
		domain.OpIssueInvoice,
		domain.OpExecuteSettlement,
		domain.OpReadResource,
	}
	for _, op := range inFlightOps {
		allowed, err := svc.CanExecuteOperation(ctx, "tenant-a", op, "active_trip_123")
		assert.True(t, allowed, "Op %s must be permitted for in-flight safety during READ_ONLY", op)
		assert.NoError(t, err)
	}
}

func TestEntitlement_PlanUpgrade_ImmediatelyExpandsQuota(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	ctx := context.Background()

	now := time.Now().UTC()
	_, err := svc.CreateSubscription(ctx, "tenant-a", domain.PlanStarter, domain.SubTrial, now.Add(-1*time.Hour), now.Add(30*24*time.Hour))
	require.NoError(t, err)

	status, err := svc.CheckQuota(ctx, "tenant-a", domain.QuotaMaxTripsPerMonth)
	require.NoError(t, err)
	assert.Equal(t, 10, status.Max)

	// Upgrade to GROWTH tier (250 trips/mo)
	_, err = svc.CreateSubscription(ctx, "tenant-a", domain.PlanGrowth, domain.SubActive, now.Add(-1*time.Hour), now.Add(30*24*time.Hour))
	require.NoError(t, err)

	// Reset meter to pick up new plan max
	_, err = db.Exec(`UPDATE tenant_usage_meters SET max_quantity = 250 WHERE tenant_id = 'tenant-a' AND quota_key = 'max_trips_per_month'`)
	require.NoError(t, err)

	statusUpgraded, err := svc.CheckQuota(ctx, "tenant-a", domain.QuotaMaxTripsPerMonth)
	require.NoError(t, err)
	assert.Equal(t, 250, statusUpgraded.Max)
}

func TestEntitlement_PilotOverride_EnablesMultiStopOnStarter(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	ctx := context.Background()

	now := time.Now().UTC()
	_, err := svc.CreateSubscription(ctx, "tenant-pilot", domain.PlanStarter, domain.SubTrial, now.Add(-1*time.Hour), now.Add(30*24*time.Hour))
	require.NoError(t, err)

	// Default starter plan has multi_stop disabled
	hasMS, err := svc.HasFeature(ctx, "tenant-pilot", domain.FeatureMultiStop)
	require.NoError(t, err)
	assert.False(t, hasMS, "Starter plan by default does not include multi_stop")

	// Set explicit pilot override for live pilot evaluation
	exp := now.Add(14 * 24 * time.Hour)
	err = svc.SetEntitlementOverride(ctx, "tenant-pilot", "FEATURE", string(domain.FeatureMultiStop), "enabled", "Phase P7 Live Pilot Evaluation", &exp)
	require.NoError(t, err)

	// Now multi_stop must be enabled via override
	hasMSAfter, err := svc.HasFeature(ctx, "tenant-pilot", domain.FeatureMultiStop)
	require.NoError(t, err)
	assert.True(t, hasMSAfter, "Pilot override must grant multi_stop feature on Starter plan")
}

func TestEntitlement_CrossTenantIsolation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := application.NewService(db)
	ctx := context.Background()

	now := time.Now().UTC()
	_, err := svc.CreateSubscription(ctx, "tenant-a", domain.PlanStarter, domain.SubTrial, now.Add(-1*time.Hour), now.Add(30*24*time.Hour))
	require.NoError(t, err)
	_, err = svc.CreateSubscription(ctx, "tenant-b", domain.PlanStarter, domain.SubTrial, now.Add(-1*time.Hour), now.Add(30*24*time.Hour))
	require.NoError(t, err)

	// Tenant A consumes all 10 trips
	for i := 1; i <= 10; i++ {
		idemKey := fmt.Sprintf("tenant_a_trip_%d", i)
		require.NoError(t, svc.ReserveQuota(ctx, nil, "tenant-a", domain.QuotaMaxTripsPerMonth, 1, idemKey, "booking", fmt.Sprintf("a_%d", i)))
		require.NoError(t, svc.CommitQuota(ctx, nil, "tenant-a", domain.QuotaMaxTripsPerMonth, 1, idemKey, "booking", fmt.Sprintf("a_%d", i)))
	}

	// Tenant A is maxed out
	err = svc.ReserveQuota(ctx, nil, "tenant-a", domain.QuotaMaxTripsPerMonth, 1, "tenant_a_11", "booking", "a_11")
	assert.ErrorIs(t, err, domain.ErrQuotaExceeded)

	// Tenant B should still have full 10 capacity untouched
	statusB, err := svc.CheckQuota(ctx, "tenant-b", domain.QuotaMaxTripsPerMonth)
	require.NoError(t, err)
	assert.Equal(t, 0, statusB.Used)
	assert.Equal(t, 10, statusB.Remaining)

	err = svc.ReserveQuota(ctx, nil, "tenant-b", domain.QuotaMaxTripsPerMonth, 1, "tenant_b_1", "booking", "b_1")
	assert.NoError(t, err, "Tenant B must be able to consume its own isolated quota")
}
