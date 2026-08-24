package test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	intOCR "transport-app/internal/integration/ocr"
	"transport-app/internal/service"
)

func seedVerifyFixtures(t *testing.T, db *sql.DB) {
	t.Helper()
	must := func(q string, args ...any) {
		_, err := db.Exec(q, args...)
		require.NoError(t, err, q)
	}
	// Route with geocoded endpoints ~Pune→Nagpur corridor.
	must(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
	      VALUES ('r-v','Pune','Nagpur',700,12,20000)`)
	must(`INSERT INTO route_locations (route_id, source_lat, source_lng, dest_lat, dest_lng)
	      VALUES ('r-v',18.5204,73.8567,21.1458,79.0882)`)
	must(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
	      VALUES ('drv-v','DV1','Veri','Fy','+91-8000000000','DL-V','2030-01-01','available','tenant-a')`)
	must(`INSERT INTO trips (id, trip_number, driver_id, route_id, departure_time, status, tenant_id)
	      VALUES ('tr-v','TR-V','drv-v','r-v','2026-08-24 06:00:00','in_transit','tenant-a')`)
}

// TestSpec22_OCRMockCanned — Spec 22 §7 S8: the mock adapter returns the
// spec's canned fixture regardless of input.
func TestSpec22_OCRMockCanned(t *testing.T) {
	res, err := intOCR.NewMockClient().Extract(context.Background(), "/receipts/x.jpg")
	require.NoError(t, err)
	assert.InDelta(t, 3000.0, res.Amount, 0.001)
	assert.InDelta(t, 0.94, res.Confidence, 0.001)

	_, err = intOCR.NewMockClient().Extract(context.Background(), "")
	assert.Error(t, err, "no receipt on file must fail honestly")
}

func insertExpense(t *testing.T, db *sql.DB, id string, lat, lng any, amount float64, category string, createdAgoMin int) {
	insertExpenseReceipt(t, db, id, lat, lng, amount, category, createdAgoMin, nil)
}

func insertExpenseReceipt(t *testing.T, db *sql.DB, id string, lat, lng any, amount float64, category string, createdAgoMin int, receipt any) {
	t.Helper()
	must := func(q string, args ...any) {
		_, err := db.Exec(q, args...)
		require.NoError(t, err, q)
	}
	must(`INSERT INTO driver_expenses
	      (id, trip_id, driver_id, expense_type, category, amount, description,
	       status, tenant_id, latitude, longitude, created_at, receipt_url)
	      VALUES (?, 'tr-v', 'drv-v', ?, ?, ?, 'seeded', 'pending', 'tenant-a', ?, ?,
	              datetime('now', ?), ?)`,
		id, category, category, amount, lat, lng, "-"+itoa(createdAgoMin)+" minutes", receipt)
}

// TestSpec22_VerificationRuleTable — Spec 22 §7 S8: all three
// verification_states are reachable per the §5.3 rule table.
func TestSpec22_VerificationRuleTable(t *testing.T) {
	db := NewTestDB(t)
	seedVerifyFixtures(t, db)
	svc := service.NewKharchaVerifyService(db, nil, intOCR.NewMockClient())
	ctx := context.Background()

	stateOf := func(id string) string {
		t.Helper()
		var s string
		require.NoError(t, db.QueryRow(
			`SELECT verification_state FROM driver_expenses WHERE id=?`, id).Scan(&s), id)
		return s
	}
	reasonOf := func(id string) string {
		t.Helper()
		var r string
		require.NoError(t, db.QueryRow(
			`SELECT flag_reason FROM driver_expenses WHERE id=?`, id).Scan(&r), id)
		return r
	}

	// History to give the fuel median ≥3 samples (1000×3 → median 1000).
	for i, amt := range []float64{1000, 1000, 1000} {
		insertExpense(t, db, "hist-fuel-"+itoa(i), nil, nil, amt, "fuel", 60*24*30)
	}

	// AUTO: on-route (near Pune endpoint) + mock OCR 3000@0.94 within ±20%
	// of... wait — 3000 vs median 1000 is 3x → would flag. Use food with no
	// median history instead: flags require median>0 for rule 3; auto band
	// requires median>0 too. So seed a matching median first.
	insertExpense(t, db, "hist-food-1", nil, nil, 3000, "food", 60*24*30)
	insertExpense(t, db, "hist-food-2", nil, nil, 3000, "food", 60*24*30)
	insertExpense(t, db, "hist-food-3", nil, nil, 3000, "food", 60*24*30)

	insertExpenseReceipt(t, db, "exp-auto", 18.525, 73.862, 3100, "food", 0, "/receipts/auto.jpg")
	st, err := svc.VerifyExpense(ctx, "exp-auto")
	require.NoError(t, err)
	assert.Equal(t, "auto_verified", st, "OCR 0.94 + within band + near route")
	assert.Equal(t, "auto_verified", stateOf("exp-auto"))

	// FLAGGED — distance: far off-route point (Ladakh-ish).
	insertExpense(t, db, "exp-dist", 34.15, 77.57, 500, "fuel", 0)
	st, err = svc.VerifyExpense(ctx, "exp-dist")
	require.NoError(t, err)
	assert.Equal(t, "flagged", st)
	assert.Contains(t, reasonOf("exp-dist"), "distance_from_route_km")

	// FLAGGED — duplicate: same driver+category ±30min window.
	now := time.Now()
	insertExpenseAt(t, db, "exp-dup-a", 18.530, 73.870, 500, "toll", now)
	insertExpenseAt(t, db, "exp-dup-b", 18.540, 73.880, 500, "toll", now.Add(5*time.Minute))
	st, err = svc.VerifyExpense(ctx, "exp-dup-b")
	require.NoError(t, err)
	assert.Equal(t, "flagged", st)
	assert.Contains(t, reasonOf("exp-dup-b"), "duplicate_of")

	// MANUAL — no receipt (no OCR) and clean otherwise.
	insertExpense(t, db, "exp-manual", 18.524, 73.861, 2900, "repair", 0)
	st, err = svc.VerifyExpense(ctx, "exp-manual")
	require.NoError(t, err)
	assert.Equal(t, "manual", st)
}

func insertExpenseAt(t *testing.T, db *sql.DB, id string, lat, lng, amount float64, category string, at time.Time) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO driver_expenses
	      (id, trip_id, driver_id, expense_type, category, amount, description,
	       status, tenant_id, latitude, longitude, created_at)
	      VALUES (?, 'tr-v', 'drv-v', ?, ?, ?, 'seeded', 'pending', 'tenant-a', ?, ?, ?)`,
		id, category, category, amount, lat, lng, at.UTC().Format("2006-01-02 15:04:05"))
	require.NoError(t, err)
}
