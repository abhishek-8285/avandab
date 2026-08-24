package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
	"transport-app/internal/events"
	intOCR "transport-app/internal/integration/ocr"
)

func newVerifyTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_kharcha_verify_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := filepath.Join(cwd, "../../db/migrations")
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newVerifier(db *sql.DB) *KharchaVerifyService {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewKharchaVerifyService(db, logger, intOCR.NewClient(intOCR.Config{Provider: "mock"}, logger))
}

// seedRouteCorridor creates a Nashik→Pune-ish corridor so GPS distances
// are deterministic.
func seedRouteCorridor(t *testing.T, db *sql.DB, tripID string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('rt-verify', 'Nashik', 'Pune', 210, 5, 15000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT OR IGNORE INTO route_locations
		(route_id, source_lat, source_lng, dest_lat, dest_lng) VALUES
		('rt-verify', 19.9975, 73.7898, 18.5204, 73.8567)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, tenant_id, status) VALUES (?, 'TR-VERIFY', ?, datetime('now'), '1', 'started')`, tripID, "rt-verify")
	require.NoError(t, err)
}

func seedExpense(t *testing.T, db *sql.DB, id string, lat, lng *float64, amount float64, receipt bool) {
	t.Helper()
	var rec any
	if receipt {
		rec = "/storage/receipts/" + id + ".jpg"
	}
	_, err := db.Exec(`
		INSERT INTO driver_expenses (id, trip_id, driver_id, expense_type, category,
			amount, status, created_at, latitude, longitude, receipt_url, tenant_id)
		VALUES (?, 'tr-verify', 'drv-1', 'fuel', 'fuel', ?, 'pending',
		        datetime('now'), ?, ?, ?, '1')`,
		id, amount, lat, lng, rec)
	require.NoError(t, err)
}

func fptr(v float64) *float64 { return &v }

func TestKharchaVerify_AutoVerifiedHappyPath(t *testing.T) {
	db := newVerifyTestDB(t)
	svc := newVerifier(db)
	ctx := context.Background()
	seedRouteCorridor(t, db, "tr-verify")

	// Category history → median 3000.
	seedExpense(t, db, "hist-1", nil, nil, 2500, false)
	seedExpense(t, db, "hist-2", nil, nil, 3500, false)
	// Claim at the fuel stop right on the route corridor, amount in band,
	// receipt present (mock OCR: ₹3000 @ 0.94).
	seedExpense(t, db, "exp-auto", fptr(19.9970), fptr(73.7900), 3000, true)

	state, err := svc.VerifyExpense(ctx, "exp-auto")
	require.NoError(t, err)
	assert.Equal(t, VerifyAutoVerified, state)

	var vs, reason string
	var ocrAmt, ocrConf sql.NullFloat64
	require.NoError(t, db.QueryRow(`SELECT verification_state, flag_reason, ocr_amount, ocr_confidence
		FROM driver_expenses WHERE id='exp-auto'`).Scan(&vs, &reason, &ocrAmt, &ocrConf))
	assert.Empty(t, reason)
	assert.InDelta(t, 3000, ocrAmt.Float64, 0.01)
	assert.InDelta(t, 0.94, ocrConf.Float64, 0.001)
}

func TestKharchaVerify_FlaggedDistanceFromRoute(t *testing.T) {
	db := newVerifyTestDB(t)
	svc := newVerifier(db)
	seedRouteCorridor(t, db, "tr-verify")
	// Delhi coords: thousands of km from the corridor.
	seedExpense(t, db, "exp-far", fptr(28.6139), fptr(77.2090), 3000, true)

	state, err := svc.VerifyExpense(context.Background(), "exp-far")
	require.NoError(t, err)
	assert.Equal(t, VerifyFlagged, state)

	var reason string
	require.NoError(t, db.QueryRow(`SELECT flag_reason FROM driver_expenses WHERE id='exp-far'`).Scan(&reason))
	assert.Contains(t, reason, "distance_from_route_km")
}

func TestKharchaVerify_FlaggedAmountOverMedian(t *testing.T) {
	db := newVerifyTestDB(t)
	svc := newVerifier(db)
	seedRouteCorridor(t, db, "tr-verify")
	seedExpense(t, db, "hist-m1", nil, nil, 2800, false)
	seedExpense(t, db, "hist-m2", nil, nil, 3200, false)
	// Near the route but 10k against a ~3k median → >2x flag wins over OCR.
	seedExpense(t, db, "exp-big", fptr(19.9970), fptr(73.7900), 10000, true)

	state, err := svc.VerifyExpense(context.Background(), "exp-big")
	require.NoError(t, err)
	assert.Equal(t, VerifyFlagged, state)
	var reason string
	require.NoError(t, db.QueryRow(`SELECT flag_reason FROM driver_expenses WHERE id='exp-big'`).Scan(&reason))
	assert.Contains(t, reason, "gt_2.00x_median")
}

func TestKharchaVerify_FlaggedDuplicateWindow(t *testing.T) {
	db := newVerifyTestDB(t)
	svc := newVerifier(db)
	seedRouteCorridor(t, db, "tr-verify")
	seedExpense(t, db, "dup-1", fptr(19.9970), fptr(73.7900), 3000, false)
	seedExpense(t, db, "dup-2", fptr(19.9970), fptr(73.7900), 3000, false)

	state, err := svc.VerifyExpense(context.Background(), "dup-2")
	require.NoError(t, err)
	assert.Equal(t, VerifyFlagged, state)
	var reason string
	require.NoError(t, db.QueryRow(`SELECT flag_reason FROM driver_expenses WHERE id='dup-2'`).Scan(&reason))
	assert.Contains(t, reason, "duplicate")
}

func TestKharchaVerify_ManualWithoutEvidence(t *testing.T) {
	db := newVerifyTestDB(t)
	svc := newVerifier(db)
	seedExpense(t, db, "exp-manual", nil, nil, 1200, false)

	state, err := svc.VerifyExpense(context.Background(), "exp-manual")
	require.NoError(t, err)
	assert.Equal(t, VerifyManual, state)
}

func TestKharchaVerify_EventBusTriggersVerification(t *testing.T) {
	db := newVerifyTestDB(t)
	bus := events.NewInMemoryBus()
	svc := newVerifier(db)
	svc.SubscribeExpenseCreated(bus)
	seedExpense(t, db, "exp-bus", nil, nil, 900, false)

	bus.Publish(context.Background(), events.Event{
		Type:    events.ExpenseCreated,
		Payload: map[string]any{"expense_id": "exp-bus"},
	})

	var vs string
	require.NoError(t, db.QueryRow(`SELECT verification_state FROM driver_expenses WHERE id='exp-bus'`).Scan(&vs))
	assert.Equal(t, VerifyManual, vs, "no evidence path still lands a decided state via bus")
}

func TestKharchaVerify_ListFlaggedTenantIsolation(t *testing.T) {
	db := newVerifyTestDB(t)
	svc := newVerifier(db)
	_, err := db.Exec(`INSERT INTO driver_expenses (id, trip_id, driver_id, expense_type, category,
		amount, status, created_at, latitude, longitude, tenant_id)
		VALUES ('far-a', NULL, 'd', 'fuel', 'fuel', 100, 'pending', datetime('now'), 28.6, 77.2, '1'),
		       ('far-b', NULL, 'd', 'fuel', 'fuel', 100, 'pending', datetime('now'), 28.6, 77.2, 'tenant-b')`)
	require.NoError(t, err)
	_, _ = svc.VerifyExpense(context.Background(), "far-a")
	_, _ = svc.VerifyExpense(context.Background(), "far-b")

	flagged, err := svc.ListFlaggedExpenses(context.Background(), "1", 50)
	require.NoError(t, err)
	require.Len(t, flagged, 1)
	assert.Equal(t, "far-a", flagged[0]["id"])
}

func TestHaversineKm_Sanity(t *testing.T) {
	// Nashik→Pune ≈ 180km great-circle; assert coarse bounds.
	d := HaversineKm(19.9975, 73.7898, 18.5204, 73.8567)
	assert.Greater(t, d, 150.0)
	assert.Less(t, d, 220.0)
	// Zero distance.
	assert.InDelta(t, 0.0, HaversineKm(19.0, 73.0, 19.0, 73.0), 0.000001)
}
