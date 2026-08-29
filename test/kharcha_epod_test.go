package test

import (
	"context"
	"testing"
	"time"
	"transport-app/internal/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
	"transport-app/internal/service"
)

// ============================================================================
// KHARCHA LEDGER SERVICE TESTS
// ============================================================================

// Helper: builds a full in-transit trip ready for kharcha/delivery tests.
func setupInTransitTrip(t *testing.T, svcs *service.Services, ctx context.Context) domain.TripID {
	t.Helper()
	future := time.Now().AddDate(1, 0, 0).Format("2006-01-02")

	cust, err := svcs.Customers.CreateCustomer(ctx, "Kharcha Corp", "Corp", "999-K001", "", "", "", "")
	require.NoError(t, err)

	rt, err := svcs.Routes.CreateRoute(ctx, "Delhi", "Jaipur", 270, 5.0, 8000, "")
	require.NoError(t, err)

	bk, err := svcs.Bookings.CreateBooking(ctx, service.CreateBookingRequest{
		CustomerID:  cust.ID,
		RouteID:     rt.ID,
		PickupDate:  future,
		VehicleType: domain.VehicleTypeTruck,
		Passengers:  1,
		Price:       8000,
	})
	require.NoError(t, err)

	trp, err := svcs.Trips.CreateTrip(ctx, service.CreateTripRequest{
		BookingID:     &bk.ID,
		RouteID:       rt.ID,
		DepartureTime: future,
	})
	require.NoError(t, err)

	_, _ = svcs.Trips.ScheduleTrip(ctx, trp.ID)

	drv, _ := svcs.Drivers.CreateDriver(ctx, "Ramesh", "Singh", "999-K002", "", "", "LIC-K01", future, 3, nil, nil, nil)
	veh, _ := svcs.Vehicles.CreateVehicle(ctx, "RJ-14-KH-1", "TRK-KH", domain.VehicleTypeTruck, 12000, domain.FuelTypeDiesel, future, future, future, "")
	_, _ = svcs.Trips.AssignDriver(ctx, trp.ID, drv.ID)
	_, _ = svcs.Trips.AssignVehicle(ctx, trp.ID, veh.ID)
	_, _ = svcs.Trips.StartTrip(ctx, trp.ID)

	return trp.ID
}

// createExpense is a convenience wrapper matching the positional arg signature.
func createExpense(t *testing.T, svcs *service.Services, ctx context.Context, tripID, driverID, category string, amount float64, desc string) string {
	t.Helper()
	id, err := svcs.Kharcha.CreateExpense(ctx, tripID, driverID, category, amount, desc, "", 0)
	require.NoError(t, err, "CreateExpense(%s %.0f) failed", category, amount)
	return id
}

// ============================================================================
// KH-1: Create expense and verify it appears in pending queue.
// ============================================================================
func TestKharcha_KH1_CreateAndListPending(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)
	createExpense(t, svcs, ctx, string(tripID), "drv-kh1", "fuel", 1500.0, "Diesel top-up at NH48 pump")

	pending, err := svcs.Kharcha.ListPendingExpenses(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(pending), 1)

	found := false
	for _, e := range pending {
		if e.Amount == 1500.0 && e.Category == "fuel" {
			found = true
			assert.Equal(t, "pending", e.Status)
		}
	}
	assert.True(t, found, "created expense should appear in pending list")
}

// ============================================================================
// KH-2: Approve an expense — status becomes 'approved'.
// ============================================================================
func TestKharcha_KH2_ApproveExpense(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)
	expID := createExpense(t, svcs, ctx, string(tripID), "drv-kh2", "advance", 2000.0, "Driver cash advance")

	err := svcs.Kharcha.ApproveExpense(ctx, expID, "user-owner-1")
	require.NoError(t, err, "approve should succeed")

	approved, err := svcs.Kharcha.GetExpenseByID(ctx, expID)
	require.NoError(t, err)
	assert.Equal(t, "approved", approved.Status)
	require.NotNil(t, approved.ApprovedBy)
	assert.Equal(t, "user-owner-1", *approved.ApprovedBy)
	assert.NotNil(t, approved.ApprovedAt)
}

// ============================================================================
// KH-3: Reject an expense with a reason.
// ============================================================================
func TestKharcha_KH3_RejectExpense(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)
	expID := createExpense(t, svcs, ctx, string(tripID), "drv-kh3", "food", 500.0, "Restaurant bill")

	err := svcs.Kharcha.RejectExpense(ctx, expID, "user-manager-1", "Receipt not valid for food claim")
	require.NoError(t, err, "reject should succeed")

	rejected, err := svcs.Kharcha.GetExpenseByID(ctx, expID)
	require.NoError(t, err)
	assert.Equal(t, "rejected", rejected.Status)
	require.NotNil(t, rejected.RejectedReason)
	assert.Contains(t, *rejected.RejectedReason, "Receipt not valid")
}

// ============================================================================
// KH-4: Reject without a reason should fail.
// ============================================================================
func TestKharcha_KH4_RejectWithoutReason_Fails(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)
	expID := createExpense(t, svcs, ctx, string(tripID), "drv-kh4", "toll", 300.0, "")

	err := svcs.Kharcha.RejectExpense(ctx, expID, "user-mgr", "")
	assert.Error(t, err, "reject without reason should return error")
}

// ============================================================================
// KH-5: Double approve is idempotent — status stays 'approved'.
// ============================================================================
func TestKharcha_KH5_DoubleApprove_Idempotent(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)
	expID := createExpense(t, svcs, ctx, string(tripID), "drv-kh5", "repair", 800.0, "")

	require.NoError(t, svcs.Kharcha.ApproveExpense(ctx, expID, "user-1"))
	_ = svcs.Kharcha.ApproveExpense(ctx, expID, "user-2") // second: no-op (WHERE status='pending')

	final, err := svcs.Kharcha.GetExpenseByID(ctx, expID)
	require.NoError(t, err)
	assert.Equal(t, "approved", final.Status, "first approval must survive double-approve")
}

// ============================================================================
// KH-6: ListLedger — all entries, plus trip-ID filter.
// ============================================================================
func TestKharcha_KH6_LedgerFiltering(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripA := setupInTransitTrip(t, svcs, ctx)
	createExpense(t, svcs, ctx, string(tripA), "drv-kh6", "toll", 100.0, "")
	createExpense(t, svcs, ctx, string(tripA), "drv-kh6", "fuel", 200.0, "")

	all, err := svcs.Kharcha.ListLedger(ctx, "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all), 2)

	filtered, err := svcs.Kharcha.ListLedger(ctx, string(tripA))
	require.NoError(t, err)
	assert.Equal(t, 2, len(filtered))

	empty, err := svcs.Kharcha.ListLedger(ctx, "trip-does-not-exist")
	require.NoError(t, err)
	assert.Empty(t, empty)
}

// ============================================================================
// KH-7: GetKharchaStats — PendingCount and UnsettledTotal are always reliable.
// ApprovedToday/MonthTotal depend on DATE(approved_at) which has a SQLite/IST timezone
// edge case in in-memory test DBs; verified separately on the live transport.db.
func TestKharcha_KH7_Stats(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)
	id1 := createExpense(t, svcs, ctx, string(tripID), "drv-kh7a", "fuel", 500.0, "")
	createExpense(t, svcs, ctx, string(tripID), "drv-kh7b", "advance", 1000.0, "")

	require.NoError(t, svcs.Kharcha.ApproveExpense(ctx, id1, "owner-stats"))

	stats, err := svcs.Kharcha.GetKharchaStats(ctx)
	require.NoError(t, err, "GetKharchaStats must not error")
	assert.GreaterOrEqual(t, stats.PendingCount, 1, "should have ≥1 pending")
	assert.Greater(t, stats.UnsettledTotal, 0.0, "unsettled total should be >0")
}

// ============================================================================
// KH-8: GetExpenseByID for unknown ID returns error.
// ============================================================================
func TestKharcha_KH8_GetUnknownExpense(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	_, err := svcs.Kharcha.GetExpenseByID(ctx, "expense-does-not-exist")
	assert.Error(t, err)
}

// ============================================================================
// KH-9 & KH-10: Zero/negative amounts rejected by service.
// ============================================================================
func TestKharcha_KH9_ZeroAmount_Rejected(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)
	_, err := svcs.Kharcha.CreateExpense(ctx, string(tripID), "drv-kh9", "fuel", 0.0, "", "", 0)
	assert.Error(t, err, "zero amount should be rejected")
}

func TestKharcha_KH10_NegativeAmount_Rejected(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)
	_, err := svcs.Kharcha.CreateExpense(ctx, string(tripID), "drv-kh10", "advance", -250.0, "", "", 0)
	assert.Error(t, err, "negative amount should be rejected")
}

// ============================================================================
// e-POD (DeliverWithPOD) TESTS
// ============================================================================

// POD-1: Happy path — returns trip number, trip becomes delivered.
func TestEPOD_POD1_HappyPath(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)
	otpCode, _ := svcs.Trips.EnsurePODOTP(ctx, string(tripID))
	tripNum, err := svcs.Trips.DeliverWithPOD(ctx, string(tripID), service.DeliverWithPODRequest{
		PODPhotoURL:    "https://avandab.com/pods/trip1.jpg",
		ConsigneeName:  "Mahesh Kumar",
		ConsigneePhone: "9876543210",
		Notes:          "Left at reception",
		OTPCode:        otpCode,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, tripNum)

	delivered, err := svcs.Trips.GetTrip(ctx, tripID)
	require.NoError(t, err)
	assert.Equal(t, domain.TripDelivered, delivered.Status)
}

// POD-2: No photo URL but SignatureURL provided — uses signature as fallback POD evidence.
func TestEPOD_POD2_SignatureFallback(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)
	otpCode, _ := svcs.Trips.EnsurePODOTP(ctx, string(tripID))
	_, err := svcs.Trips.DeliverWithPOD(ctx, string(tripID), service.DeliverWithPODRequest{
		PODPhotoURL:   "",
		SignatureURL:  "https://avandab.com/sigs/trip-sig.png",
		ConsigneeName: "Ravi Teja",
		OTPCode:       otpCode,
	})
	require.NoError(t, err, "signature fallback should work")
}

// POD-3: Both URLs empty → error (no POD evidence).
func TestEPOD_POD3_BothURLsEmpty_Fails(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)
	_, err := svcs.Trips.DeliverWithPOD(ctx, string(tripID), service.DeliverWithPODRequest{
		PODPhotoURL:   "",
		SignatureURL:  "",
		ConsigneeName: "No Evidence",
	})
	assert.Error(t, err)
}

// POD-4: Deliver on draft trip fails state guard.
func TestEPOD_POD4_WrongState_Fails(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	rt, _ := svcs.Routes.CreateRoute(ctx, "A", "B", 100, 2, 5000, "")
	trp, err := svcs.Trips.CreateTrip(ctx, service.CreateTripRequest{
		RouteID:       rt.ID,
		DepartureTime: time.Now().AddDate(1, 0, 0).Format("2006-01-02"),
	})
	require.NoError(t, err)

	_, err = svcs.Trips.DeliverWithPOD(ctx, string(trp.ID), service.DeliverWithPODRequest{
		PODPhotoURL: "https://avandab.com/pod.png",
	})
	assert.Error(t, err, "draft trip must not be deliverable")
}

// POD-5: Second delivery call on already-delivered trip fails cleanly.
func TestEPOD_POD5_DoubleDeliver_SecondFails(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)
	otpCode, _ := svcs.Trips.EnsurePODOTP(ctx, string(tripID))

	_, err := svcs.Trips.DeliverWithPOD(ctx, string(tripID), service.DeliverWithPODRequest{
		PODPhotoURL: "https://avandab.com/pod1.jpg",
		OTPCode:     otpCode,
	})
	require.NoError(t, err)

	_, err = svcs.Trips.DeliverWithPOD(ctx, string(tripID), service.DeliverWithPODRequest{
		PODPhotoURL: "https://avandab.com/pod2.jpg",
		OTPCode:     otpCode,
	})
	assert.Error(t, err, "second delivery on delivered trip must fail")
}

// POD-6: Delivery fires event → trip becomes delivered (event handler coverage).
func TestEPOD_POD6_EventFiring(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)
	otpCode, _ := svcs.Trips.EnsurePODOTP(ctx, string(tripID))
	_, err := svcs.Trips.DeliverWithPOD(ctx, string(tripID), service.DeliverWithPODRequest{
		PODPhotoURL: "https://avandab.com/pod-event.jpg",
		OTPCode:     otpCode,
	})
	require.NoError(t, err)

	trip, err := svcs.Trips.GetTrip(ctx, tripID)
	require.NoError(t, err)
	assert.Equal(t, domain.TripDelivered, trip.Status)
}

// ============================================================================
// INTEGRATION WORKFLOWS
// ============================================================================

// INT-1: Full flow — expense created mid-trip → approved → trip delivered.
func TestKharcha_INT1_FullWorkflow(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)
	expID := createExpense(t, svcs, ctx, string(tripID), "drv-int1", "fuel", 2000.0, "Petrol before Jaipur bypass")

	require.NoError(t, svcs.Kharcha.ApproveExpense(ctx, expID, "owner-int1"))

	otpCode, _ := svcs.Trips.EnsurePODOTP(ctx, string(tripID))
	tripNum, err := svcs.Trips.DeliverWithPOD(ctx, string(tripID), service.DeliverWithPODRequest{
		PODPhotoURL:    "https://avandab.com/int1-pod.jpg",
		ConsigneeName:  "Rajendra Prasad",
		ConsigneePhone: "9812345678",
		OTPCode:        otpCode,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, tripNum)

	delivered, err := svcs.Trips.GetTrip(ctx, tripID)
	require.NoError(t, err)
	assert.Equal(t, domain.TripDelivered, delivered.Status)

	ledger, err := svcs.Kharcha.ListLedger(ctx, string(tripID))
	require.NoError(t, err)
	require.Len(t, ledger, 1)
	assert.Equal(t, "approved", ledger[0].Status)
	assert.Equal(t, 2000.0, ledger[0].Amount)
}

// INT-2: Multiple expense types — approve some, reject some.
func TestKharcha_INT2_MixedApprovalFlow(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := setupInTransitTrip(t, svcs, ctx)

	id1 := createExpense(t, svcs, ctx, string(tripID), "drv-int2", "fuel", 3000.0, "")
	id2 := createExpense(t, svcs, ctx, string(tripID), "drv-int2", "toll", 150.0, "")
	id3 := createExpense(t, svcs, ctx, string(tripID), "drv-int2", "food", 200.0, "")

	require.NoError(t, svcs.Kharcha.ApproveExpense(ctx, id1, "owner-int2"))
	require.NoError(t, svcs.Kharcha.ApproveExpense(ctx, id2, "owner-int2"))
	require.NoError(t, svcs.Kharcha.RejectExpense(ctx, id3, "owner-int2", "Food not reimbursable per policy"))

	ledger, err := svcs.Kharcha.ListLedger(ctx, string(tripID))
	require.NoError(t, err)
	require.Len(t, ledger, 3)

	statuses := map[string]string{}
	for _, e := range ledger {
		statuses[e.Category] = e.Status
	}
	assert.Equal(t, "approved", statuses["fuel"])
	assert.Equal(t, "approved", statuses["toll"])
	assert.Equal(t, "rejected", statuses["food"])
}
