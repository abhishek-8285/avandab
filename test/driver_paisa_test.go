package test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/service"
)

// seedPaisaFixtures creates driver drv-paisa with:
//   - one PAID settlement net 13900 (st-paid, trip tr-old)
//   - one PENDING settlement net 13900 for trip tr-new (not counted)
func seedPaisaFixtures(t *testing.T, db *sql.DB, day string) {
	t.Helper()
	must := func(q string, args ...any) {
		_, err := db.Exec(q, args...)
		require.NoError(t, err, q)
	}
	must(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
	      VALUES ('drv-paisa','DP1','Paisa','Driver','+91-9000000000','DL-P','2030-01-01','available','tenant-a')`)
	must(`INSERT INTO trips (id, trip_number, driver_id, vehicle_id, route_id, departure_time, status, tenant_id)
	      VALUES ('tr-old','TR-OLD','drv-paisa',NULL,'r-1',?, 'completed','tenant-a')`, day)
	must(`INSERT INTO trips (id, trip_number, driver_id, vehicle_id, route_id, departure_time, status, tenant_id)
	      VALUES ('tr-new','TR-NEW','drv-paisa',NULL,'r-1',?, 'in_transit','tenant-a')`, day)

	must(`INSERT INTO driver_settlements
	      (id, trip_id, driver_id, gross_fare, deductions, tds_amount, net_payout, status, paid_at)
	      VALUES ('st-paid','tr-old','drv-paisa',18200,4100,200,13900,'paid',?)`, day)
}

// TestSpec22_DriverBalance_Formula — Spec 22 §7 S7 / §5.2:
// running_balance = Σ paid settlements.net − Σ paid advances + Σ approved advances.
func TestSpec22_DriverBalance_Formula(t *testing.T) {
	db := NewTestDB(t)
	svc := service.NewDriverBalanceService(db, nil)
	ctx := context.Background()
	day := "2026-08-20 10:00:00"
	seedPaisaFixtures(t, db, day)

	bal, err := svc.GetBalance(ctx, "drv-paisa")
	require.NoError(t, err)
	assert.InDelta(t, 13900.0, bal.RunningBalance, 1.0,
		"one paid settlement, no advances")
	assert.Equal(t, "st-paid", bal.LastSettlementID)
	assert.Equal(t, 0, bal.PendingAdvances)

	// Request an advance → pending count rises; balance unchanged.
	adv, err := svc.RequestAdvance(ctx, "tenant-a", "drv-paisa", "tr-new", 4000, "tyre puncture")
	require.NoError(t, err)
	bal, err = svc.GetBalance(ctx, "drv-paisa")
	require.NoError(t, err)
	assert.Equal(t, 1, bal.PendingAdvances)
	assert.InDelta(t, 13900.0, bal.RunningBalance, 1.0,
		"pending advance must not change balance")

	// Admin approves → +4000 credit.
	require.NoError(t, svc.DecideAdvance(ctx, adv.ID, "approved", "admin-1", "ok"))
	bal, err = svc.GetBalance(ctx, "drv-paisa")
	require.NoError(t, err)
	assert.InDelta(t, 17900.0, bal.RunningBalance, 1.0)

	// Double decision rejected.
	err = svc.DecideAdvance(ctx, adv.ID, "rejected", "admin-2", "")
	assert.Error(t, err, "already-decided advance must be idempotent-guarded")
}

// TestSpec22_AdvanceLifecycleThroughSettlement — Spec 22 §5.5 / §7 S7 exit
// gate: approved advance is deducted by the next settlement generation and
// flips to 'paid' when the settlement is marked paid; pending ones never are.
func TestSpec22_AdvanceLifecycleThroughSettlement(t *testing.T) {
	db := NewTestDB(t)
	ctx := context.Background()
	day := "2026-08-20 10:00:00"

	seedPaisaFixtures(t, db, day)
	// Settlement engine needs booking price on the trip's booking.
	must := func(q string, args ...any) {
		_, err := db.Exec(q, args...)
		require.NoError(t, err, q)
	}
	must(`INSERT INTO bookings (id, booking_number, customer_id, route_id, vehicle_type, pickup_date, price, status, tenant_id)
	      VALUES ('bk-p','BN-P','cust-1','r-1','truck',?,50000,'confirmed','tenant-a')`, day)
	must(`UPDATE trips SET booking_id='bk-p' WHERE id='tr-new'`)

	svc := service.NewDriverBalanceService(db, nil)
	adv, err := svc.RequestAdvance(ctx, "tenant-a", "drv-paisa", "tr-new", 4000, "fuel top-up")
	require.NoError(t, err)
	require.NoError(t, svc.DecideAdvance(ctx, adv.ID, "approved", "admin-1", ""))

	// A second advance stays pending — must NOT be deducted (edge case 8).
	pendingAdv, err := svc.RequestAdvance(ctx, "tenant-a", "drv-paisa", "tr-new", 700, "food")
	require.NoError(t, err)

	svcs := NewTestServices(t, db)
	settleSvc := svcs.Settlements
	rec, err := settleSvc.GenerateSettlement(ctx, "tr-new", false)
	require.NoError(t, err)
	assert.InDelta(t, 4000.0, rec.AdvancesKharcha, 0.01,
		"only the approved advance is deducted; pending 700 excluded")
	assert.Equal(t, "pending", rec.Status)

	// Balance while settlement pending: approved advance still counts.
	bal, err := svc.GetBalance(ctx, "drv-paisa")
	require.NoError(t, err)
	assert.InDelta(t, 17900.0, bal.RunningBalance, 1.0)

	// Mark paid → advance flips to paid and drops out of the credit side.
	_, err = settleSvc.MarkPaid(ctx, rec.ID, "ref-1", time.Now().UTC())
	require.NoError(t, err)

	// Generated settlement net is engine-computed; assert the advance's
	// effect instead: paid advance must cancel its earlier +4000 credit.
	balAfter, err := svc.GetBalance(ctx, "drv-paisa")
	require.NoError(t, err)
	var genNet float64
	require.NoError(t, db.QueryRow(`SELECT net_payout FROM driver_settlements WHERE id=?`, rec.ID).Scan(&genNet))
	assert.InDelta(t, 13900.0+genNet-4000.0, balAfter.RunningBalance, 1.0,
		"paid settlement nets minus paid-out advance")

	// And the advance row itself is 'paid'.
	var advStatus string
	require.NoError(t, db.QueryRow(`SELECT status FROM driver_advance_requests WHERE id=?`, adv.ID).Scan(&advStatus))
	assert.Equal(t, "paid", advStatus)

	_ = pendingAdv
}
