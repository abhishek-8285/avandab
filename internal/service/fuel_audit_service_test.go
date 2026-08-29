package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
)

// auditTestDB opens an in-memory SQLite DB with all migrations applied.
func auditTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_fuelaudit_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(db, "../../db/migrations"))
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func auditTestServices(t *testing.T, db *sql.DB) *service.Services {
	t.Helper()
	cfg := &config.Config{
		AppEnv:        "testing",
		Port:          "8080",
		DatabaseURL:   "file::memory:?cache=shared",
		CookieSecret:  "test-secret-32bytes-long-enough!",
		SessionMaxAge: 24 * 3600 * 1000000000,
		LogLevel:      "error",
		UploadDir:     "./uploads",
		MaxUploadSize: 10 << 20,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return service.NewServices(sqlite.NewRepository(db), cfg, logger, events.NewInMemoryBus())
}

func auditTimeStr(t time.Time) string { return t.Format("2006-01-02 15:04:05") }

// seedAuditBase inserts route, vehicle, driver, trip and returns the trip id.
func seedAuditBase(t *testing.T, db *sql.DB, sensorFitted bool, capacity, kmpl float64) string {
	t.Helper()
	sensor := 0
	if sensorFitted {
		sensor = 1
	}
	_, err := db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('r1', 'Delhi', 'Jaipur', 0, 5, 8000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type,
		 insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage,
		 tank_capacity_litres, fuel_sensor_fitted)
		VALUES ('v1', 'KA01AB1234', 'KA01AB1234', 'truck', 2000, 'diesel',
		        '2027-01-01', '2027-01-01', '2027-01-01', 'available', ?,
		        ?, ?)`, kmpl, capacity, sensor)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers
		(id, driver_id, first_name, last_name, phone, license_number, license_expiry, status)
		VALUES ('d1', 'D-001', 'Ravi', 'Kumar', '9988776655', 'KA-12345', '2028-01-01', 'available')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, driver_id, vehicle_id)
		VALUES ('t1', 'TRIP-001', 'r1', ?, 'in_transit', 'd1', 'v1')`,
		auditTimeStr(time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	return "t1"
}

// seedRefill inserts one refill_detected fuel_event.
func seedRefill(t *testing.T, db *sql.DB, id string, at time.Time, litres, odoBefore, odoAfter float64) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO fuel_events
		(id, vehicle_id, trip_id, driver_id, event_type, estimated_litres, confidence,
		 odometer_before, odometer_after, occurred_at)
		VALUES (?, 'v1', 't1', 'd1', 'refill_detected', ?, 1.0, ?, ?, ?)`,
		id, litres, odoBefore, odoAfter, auditTimeStr(at))
	require.NoError(t, err)
}

// seedSnapshot inserts one telemetry snapshot for the vehicle.
func seedSnapshot(t *testing.T, db *sql.DB, id string, at time.Time, odo float64) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO telemetry_snapshots
		(id, trip_id, vehicle_id, timestamp, odometer)
		VALUES (?, 't1', 'v1', ?, ?)`, id, auditTimeStr(at), odo)
	require.NoError(t, err)
}

// createFuelClaim creates a fuel claim at the given time with litres claimed.
func createFuelClaim(t *testing.T, db *sql.DB, id string, at time.Time, litres float64) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO driver_expenses
		(id, trip_id, driver_id, expense_type, category, amount, fuel_litres, description, status, created_at)
		VALUES (?, 't1', 'd1', 'fuel', 'fuel', 1000.0, ?, 'diesel top-up', 'pending', ?)`,
		id, litres, auditTimeStr(at))
	require.NoError(t, err)
}

func auditStatusOf(t *testing.T, db *sql.DB, expenseID string) string {
	t.Helper()
	var s string
	require.NoError(t, db.QueryRow(
		`SELECT audit_status FROM driver_expenses WHERE id = ?`, expenseID).Scan(&s))
	return s
}

// TestFuelAudit_CheckA_LevelBased verifies Check A: two refill_detected
// events of 20L each → expected 40L. Claim 40L passes, claim 60L is flagged.
func TestFuelAudit_CheckA_LevelBased(t *testing.T) {
	db := auditTestDB(t)
	svcs := auditTestServices(t, db)
	ctx := context.Background()
	seedAuditBase(t, db, true, 100, 4.0)

	// Trip departs 08:00; two refills at 09:00 and 09:10 (20L each).
	seedRefill(t, db, "fe1", time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC), 20, 100, 100)
	seedRefill(t, db, "fe2", time.Date(2026, 8, 19, 9, 10, 0, 0, time.UTC), 20, 100, 100)

	// Both claims created at the same instant — neither is a "previous
	// claim" for the other, so both audit the full trip window.
	at := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	createFuelClaim(t, db, "e-pass", at, 40)
	createFuelClaim(t, db, "e-review", at, 60)

	n, err := svcs.FuelAudit.AuditPendingClaims(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	assert.Equal(t, "passed", auditStatusOf(t, db, "e-pass"))
	assert.Equal(t, "needs_review", auditStatusOf(t, db, "e-review"))

	var expected float64
	require.NoError(t, db.QueryRow(
		`SELECT litres_expected_level FROM fuel_claim_audits WHERE expense_id = 'e-pass'`).Scan(&expected))
	assert.InDelta(t, 40.0, expected, 0.01)
}

// TestFuelAudit_CheckB_OdometerBased verifies Check B: 100 km driven at
// kmpl 4.0 → expected 25L. Claim 25L passes, claim 40L is flagged.
func TestFuelAudit_CheckB_OdometerBased(t *testing.T) {
	db := auditTestDB(t)
	svcs := auditTestServices(t, db)
	ctx := context.Background()
	// No fuel sensor and no fuel events → Check A unavailable, B decides.
	seedAuditBase(t, db, false, 0, 4.0)

	seedSnapshot(t, db, "sn1", time.Date(2026, 8, 19, 8, 30, 0, 0, time.UTC), 100)
	seedSnapshot(t, db, "sn2", time.Date(2026, 8, 19, 9, 30, 0, 0, time.UTC), 200)

	at := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	createFuelClaim(t, db, "e-pass", at, 25)
	createFuelClaim(t, db, "e-review", at, 40)

	n, err := svcs.FuelAudit.AuditPendingClaims(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	assert.Equal(t, "passed", auditStatusOf(t, db, "e-pass"))
	assert.Equal(t, "needs_review", auditStatusOf(t, db, "e-review"))

	var expected float64
	require.NoError(t, db.QueryRow(
		`SELECT litres_expected_odo FROM fuel_claim_audits WHERE expense_id = 'e-pass'`).Scan(&expected))
	assert.InDelta(t, 25.0, expected, 0.01)
}

// TestFuelAudit_Windowing_ExcludesRefillsOutsideWindow verifies refills
// before the trip start and after the claim are NOT summed (Spec 03 §11.1).
func TestFuelAudit_Windowing_ExcludesRefillsOutsideWindow(t *testing.T) {
	db := auditTestDB(t)
	svcs := auditTestServices(t, db)
	ctx := context.Background()
	seedAuditBase(t, db, true, 100, 4.0)

	seedRefill(t, db, "fe-before", time.Date(2026, 8, 19, 7, 0, 0, 0, time.UTC), 20, 50, 50)   // before trip start
	seedRefill(t, db, "fe-1", time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC), 20, 100, 100)      // in window
	seedRefill(t, db, "fe-2", time.Date(2026, 8, 19, 9, 10, 0, 0, time.UTC), 20, 100, 100)     // in window
	seedRefill(t, db, "fe-after", time.Date(2026, 8, 19, 11, 0, 0, 0, time.UTC), 20, 100, 100) // after claim

	createFuelClaim(t, db, "e1", time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC), 40)

	_, err := svcs.FuelAudit.AuditPendingClaims(ctx)
	require.NoError(t, err)
	assert.Equal(t, "passed", auditStatusOf(t, db, "e1"))

	var expected float64
	require.NoError(t, db.QueryRow(
		`SELECT litres_expected_level FROM fuel_claim_audits WHERE expense_id = 'e1'`).Scan(&expected))
	assert.InDelta(t, 40.0, expected, 0.01, "only the two in-window refills count")
}

// setupNeedsReviewClaim builds a claim flagged needs_review by the audit.
func setupNeedsReviewClaim(t *testing.T, db *sql.DB, svcs *service.Services, ctx context.Context) string {
	t.Helper()
	seedAuditBase(t, db, true, 100, 4.0)
	seedRefill(t, db, "fe1", time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC), 20, 100, 100)
	seedRefill(t, db, "fe2", time.Date(2026, 8, 19, 9, 10, 0, 0, time.UTC), 20, 100, 100)
	createFuelClaim(t, db, "e-flag", time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC), 60)

	_, err := svcs.FuelAudit.AuditPendingClaims(ctx)
	require.NoError(t, err)
	require.Equal(t, "needs_review", auditStatusOf(t, db, "e-flag"))
	return "e-flag"
}

// TestFuelAudit_EnforceGate_BlocksNeedsReview verifies fuel.audit_enforce
// =true blocks approval of a flagged claim and leaves status pending.
func TestFuelAudit_EnforceGate_BlocksNeedsReview(t *testing.T) {
	db := auditTestDB(t)
	svcs := auditTestServices(t, db)
	ctx := context.Background()
	expenseID := setupNeedsReviewClaim(t, db, svcs, ctx)

	_, err := db.Exec(`UPDATE company_config SET value = 'true' WHERE key = 'fuel.audit_enforce'`)
	require.NoError(t, err)

	err = svcs.Kharcha.ApproveExpense(ctx, expenseID, "user-1")
	require.Error(t, err)
	assert.Equal(t, "claim flagged by fuel audit (needs review); review at /fuel/audit", err.Error())

	var status string
	require.NoError(t, db.QueryRow(
		`SELECT status FROM driver_expenses WHERE id = ?`, expenseID).Scan(&status))
	assert.Equal(t, "pending", status, "flagged claim must never flip to approved")

	// The kharcha view surfaces the flag for the queue badge.
	exp, err := svcs.Kharcha.GetExpenseByID(ctx, expenseID)
	require.NoError(t, err)
	assert.Equal(t, "needs_review", exp.AuditStatus)
	require.NotNil(t, exp.FuelLitres)
	assert.InDelta(t, 60.0, *exp.FuelLitres, 0.01)
}

// TestFuelAudit_AnnotateMode_AllowsApproval verifies the default annotate
// mode: needs_review claims remain approvable (badge only).
func TestFuelAudit_AnnotateMode_AllowsApproval(t *testing.T) {
	db := auditTestDB(t)
	svcs := auditTestServices(t, db)
	ctx := context.Background()
	expenseID := setupNeedsReviewClaim(t, db, svcs, ctx)

	// fuel.audit_enforce defaults to 'false' from the migration seed.
	err := svcs.Kharcha.ApproveExpense(ctx, expenseID, "user-1")
	require.NoError(t, err)

	var status, auditStatus string
	require.NoError(t, db.QueryRow(
		`SELECT status, audit_status FROM driver_expenses WHERE id = ?`, expenseID).Scan(&status, &auditStatus))
	assert.Equal(t, "approved", status)
	assert.Equal(t, "needs_review", auditStatus, "audit trail badge must remain after approval")
}

// TestFuelAudit_ManualReview verifies the admin review verdict flips the
// audit row and the expense audit_status (Spec 03 §3.2 step 5).
func TestFuelAudit_ManualReview(t *testing.T) {
	db := auditTestDB(t)
	svcs := auditTestServices(t, db)
	ctx := context.Background()
	expenseID := setupNeedsReviewClaim(t, db, svcs, ctx)

	err := svcs.FuelAudit.ReviewClaim(ctx, expenseID, "failed", "driver receipt shows 45L", "user-1")
	require.NoError(t, err)

	assert.Equal(t, "failed", auditStatusOf(t, db, expenseID))

	var result, reviewedBy, note string
	var reviewedAt *time.Time
	require.NoError(t, db.QueryRow(
		`SELECT result, COALESCE(reviewed_by,''), COALESCE(review_note,''), reviewed_at
		 FROM fuel_claim_audits WHERE expense_id = ?`, expenseID).Scan(&result, &reviewedBy, &note, &reviewedAt))
	assert.Equal(t, "failed", result)
	assert.Equal(t, "user-1", reviewedBy)
	assert.Equal(t, "driver receipt shows 45L", note)
	require.NotNil(t, reviewedAt)

	// Invalid verdicts are rejected.
	err = svcs.FuelAudit.ReviewClaim(ctx, expenseID, "maybe", "", "user-1")
	require.Error(t, err)
}

// TestFuelAudit_CreateExpense_PersistsLitresOnlyForFuel verifies the
// fuel_litres capture in CreateExpense (Spec 03 §3.2 step 1).
func TestFuelAudit_CreateExpense_PersistsLitresOnlyForFuel(t *testing.T) {
	db := auditTestDB(t)
	svcs := auditTestServices(t, db)
	ctx := context.Background()
	seedAuditBase(t, db, false, 0, 4.0)

	fuelID, err := svcs.Kharcha.CreateExpense(ctx, "t1", "d1", "fuel", 1500, "diesel", "", 40)
	require.NoError(t, err)
	tollID, err := svcs.Kharcha.CreateExpense(ctx, "t1", "d1", "toll", 500, "nh48 toll", "", 40)
	require.NoError(t, err)

	var litres *float64
	require.NoError(t, db.QueryRow(
		`SELECT fuel_litres FROM driver_expenses WHERE id = ?`, fuelID).Scan(&litres))
	require.NotNil(t, litres)
	assert.InDelta(t, 40.0, *litres, 0.01)

	require.NoError(t, db.QueryRow(
		`SELECT fuel_litres FROM driver_expenses WHERE id = ?`, tollID).Scan(&litres))
	assert.Nil(t, litres, "non-fuel claims must not persist fuel_litres")
}
