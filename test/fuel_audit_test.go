package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	chi "github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

// ─── Test 1: Annotate mode — needs_review but approval still allowed ──────────

func TestFuelAudit_AnnotateMode_E2E(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	futureDate := time.Now().AddDate(1, 0, 0).Format("2006-01-02")

	_, err := dbConn.Exec(`
		INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type,
		  capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry,
		  fuel_sensor_fitted, tank_capacity_litres, status, tenant_id)
		VALUES ('v-fa-1','TRK-FA1','MH01FA001','truck',15.0,'diesel',?,?,?,?,1,120.0,'available','tenant-1')`,
		futureDate, futureDate, futureDate, futureDate)
	require.NoError(t, err)
	_, err = dbConn.Exec(`
		INSERT INTO drivers (id, driver_id, first_name, last_name, phone,
		  license_number, license_expiry, status, tenant_id)
		VALUES ('d-fa-1','DRV-FA1','Raju','Kumar','+919100000001','DL-FA1',?,'available','tenant-1')`,
		futureDate)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('r-fa-1','Mumbai','Pune',150.0,4.0,5000.0,'tenant-1')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`
		INSERT INTO trips (id, trip_number, route_id, vehicle_id, driver_id, departure_time, status, tenant_id)
		VALUES ('t-fa-1','TRP-FA-01','r-fa-1','v-fa-1','d-fa-1',datetime('now','-2 hours'),'in_transit','tenant-1')`)
	require.NoError(t, err)

	// Seed fuel claim: driver claims 50L
	expID, err := svcs.Kharcha.CreateExpense(ctx, "t-fa-1", "d-fa-1", "fuel", 5000.0, "Fuel fill", "", 50.0)
	require.NoError(t, err)

	// Seed fuel_events: only 35L refill detected (< 50L claimed, > 20% variance)
	_, err = dbConn.Exec(`
		INSERT INTO fuel_events (id, vehicle_id, trip_id, driver_id, event_type,
		  fuel_level_before, fuel_level_after, odometer_before, odometer_after,
		  estimated_litres, confidence, occurred_at)
		VALUES ('fe-fa-1','v-fa-1','t-fa-1','d-fa-1','refill_detected',
		  40.0, 69.2, 100.0, 100.0, 35.0, 0.9, datetime('now','-1 hour'))`)
	require.NoError(t, err)

	// Run audit pass
	n, err := svcs.FuelAudit.AuditPendingClaims(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "should audit 1 claim")

	// audit_status = needs_review (50L claimed, 35L expected → ~43% variance)
	var auditStatus string
	require.NoError(t, dbConn.QueryRow(`SELECT audit_status FROM driver_expenses WHERE id = ?`, expID).Scan(&auditStatus))
	assert.Equal(t, "needs_review", auditStatus)

	// Verify fuel_claim_audits row populated
	var claimLitres, expectedLevel, variance float64
	var result string
	require.NoError(t, dbConn.QueryRow(`
		SELECT litres_claimed, litres_expected_level, variance_pct, result
		FROM fuel_claim_audits WHERE expense_id = ?`, expID).Scan(&claimLitres, &expectedLevel, &variance, &result))
	assert.InDelta(t, 50.0, claimLitres, 0.01)
	assert.InDelta(t, 35.0, expectedLevel, 0.01)
	assert.Greater(t, variance, 20.0)
	assert.Equal(t, "needs_review", result)

	// Annotate mode (fuel.audit_enforce=false by default): ApproveExpense still succeeds
	err = svcs.Kharcha.ApproveExpense(ctx, expID, "admin-01")
	assert.NoError(t, err, "annotate mode must allow approval of needs_review")
}

// ─── Test 2: Enforce mode — needs_review blocks approval until reviewed ───────

func TestFuelAudit_EnforceMode_E2E(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	futureDate := time.Now().AddDate(1, 0, 0).Format("2006-01-02")

	// Enable enforce mode FOR tenant-1 (config reads are tenant-scoped;
	// company_config PK is (tenant_id, key)).
	_, err := dbConn.Exec(`
		INSERT INTO company_config (tenant_id, key, value)
		VALUES ('tenant-1', 'fuel.audit_enforce', 'true')
		ON CONFLICT(tenant_id, key) DO UPDATE SET value = 'true'`)
	require.NoError(t, err)

	_, err = dbConn.Exec(`
		INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type,
		  capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry,
		  fuel_sensor_fitted, tank_capacity_litres, status, tenant_id)
		VALUES ('v-fa-2','TRK-FA2','MH01FA002','truck',15.0,'diesel',?,?,?,?,1,120.0,'available','tenant-1')`,
		futureDate, futureDate, futureDate, futureDate)
	require.NoError(t, err)
	_, err = dbConn.Exec(`
		INSERT INTO drivers (id, driver_id, first_name, last_name, phone,
		  license_number, license_expiry, status, tenant_id)
		VALUES ('d-fa-2','DRV-FA2','Suresh','Kumar','+919100000002','DL-FA2',?,'available','tenant-1')`,
		futureDate)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('r-fa-2','Delhi','Agra',200.0,3.0,6000.0,'tenant-1')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`
		INSERT INTO trips (id, trip_number, route_id, vehicle_id, driver_id, departure_time, status, tenant_id)
		VALUES ('t-fa-2','TRP-FA-02','r-fa-2','v-fa-2','d-fa-2',datetime('now','-2 hours'),'in_transit','tenant-1')`)
	require.NoError(t, err)

	expID, err := svcs.Kharcha.CreateExpense(ctx, "t-fa-2", "d-fa-2", "fuel", 5000.0, "Fuel", "", 50.0)
	require.NoError(t, err)
	_, err = dbConn.Exec(`
		INSERT INTO fuel_events (id, vehicle_id, trip_id, driver_id, event_type,
		  fuel_level_before, fuel_level_after, odometer_before, odometer_after,
		  estimated_litres, confidence, occurred_at)
		VALUES ('fe-fa-2','v-fa-2','t-fa-2','d-fa-2','refill_detected',
		  30.0, 55.0, 500.0, 500.0, 30.0, 0.9, datetime('now','-1 hour'))`)
	require.NoError(t, err)

	// Run audit → needs_review (50 vs 30L = 67% variance)
	_, err = svcs.FuelAudit.AuditPendingClaims(ctx)
	require.NoError(t, err)

	// Enforce mode: approval must fail
	err = svcs.Kharcha.ApproveExpense(ctx, expID, "admin-01")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fuel audit")

	// Admin reviews with verdict=passed
	require.NoError(t, svcs.FuelAudit.ReviewClaim(ctx, expID, "passed", "Verified receipt OK", "admin-01"))

	// audit_status now = passed
	var auditStatus string
	require.NoError(t, dbConn.QueryRow(`SELECT audit_status FROM driver_expenses WHERE id = ?`, expID).Scan(&auditStatus))
	assert.Equal(t, "passed", auditStatus)

	// Approval now succeeds
	err = svcs.Kharcha.ApproveExpense(ctx, expID, "admin-01")
	assert.NoError(t, err)
}

// ─── Test 3: Cross-check — A vs B disagreement → needs_review ─────────────────

func TestFuelAudit_CrossCheck_E2E(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	futureDate := time.Now().AddDate(1, 0, 0).Format("2006-01-02")

	_, err := dbConn.Exec(`
		INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type,
		  capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry,
		  fuel_sensor_fitted, tank_capacity_litres, current_mileage, status, tenant_id)
		VALUES ('v-fa-3','TRK-FA3','MH01FA003','truck',15.0,'diesel',?,?,?,?,1,120.0,4.0,'available','tenant-1')`,
		futureDate, futureDate, futureDate, futureDate)
	require.NoError(t, err)
	_, err = dbConn.Exec(`
		INSERT INTO drivers (id, driver_id, first_name, last_name, phone,
		  license_number, license_expiry, status, tenant_id)
		VALUES ('d-fa-3','DRV-FA3','Prem','Sahu','+919100000003','DL-FA3',?,'available','tenant-1')`,
		futureDate)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('r-fa-3','Nagpur','Raipur',300.0,5.0,8000.0,'tenant-1')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`
		INSERT INTO trips (id, trip_number, route_id, vehicle_id, driver_id, departure_time, status, tenant_id)
		VALUES ('t-fa-3','TRP-FA-03','r-fa-3','v-fa-3','d-fa-3',datetime('now','-6 hours'),'in_transit','tenant-1')`)
	require.NoError(t, err)

	// Claim 40L
	expID, err := svcs.Kharcha.CreateExpense(ctx, "t-fa-3", "d-fa-3", "fuel", 4000.0, "Diesel", "", 40.0)
	require.NoError(t, err)

	// Check A: 40L refill detected
	_, err = dbConn.Exec(`
		INSERT INTO fuel_events (id, vehicle_id, trip_id, driver_id, event_type,
		  fuel_level_before, fuel_level_after, odometer_before, odometer_after,
		  estimated_litres, confidence, occurred_at)
		VALUES ('fe-fa-3','v-fa-3','t-fa-3','d-fa-3','refill_detected',
		  30.0, 63.3, 100.0, 100.0, 40.0, 0.9, datetime('now','-2 hours'))`)
	require.NoError(t, err)

	// Check B: 100km at 4 kmpl = 25L → disagrees with 40L by 60% (>25% margin).
	// First snapshot at now-4h (before trip departure now-3h) so the window query
	// captures both: windowStart=tripStart=now-3h (exclusive), both rows fall within.
	_, err = dbConn.Exec(`
		INSERT INTO telemetry_snapshots (id, vehicle_id, trip_id, driver_id, timestamp, speed, fuel_level, odometer)
		VALUES
		  ('ts-fa-3a','v-fa-3','t-fa-3','d-fa-3',datetime('now','-4 hours'),60.0,30.0,100.0),
		  ('ts-fa-3b','v-fa-3','t-fa-3','d-fa-3',datetime('now','-30 minutes'),50.0,23.3,200.0)`)
	require.NoError(t, err)

	n, err := svcs.FuelAudit.AuditPendingClaims(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	var result string
	require.NoError(t, dbConn.QueryRow(`SELECT result FROM fuel_claim_audits WHERE expense_id = ?`, expID).Scan(&result))
	assert.Equal(t, "needs_review", result, "cross-check disagreement must flag needs_review")
}

// ─── Test 4: Re-audit upsert — UNIQUE constraint honored ──────────────────────

func TestFuelAudit_ReAudit_Upsert(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	futureDate := time.Now().AddDate(1, 0, 0).Format("2006-01-02")

	_, err := dbConn.Exec(`
		INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type,
		  capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry,
		  fuel_sensor_fitted, tank_capacity_litres, status, tenant_id)
		VALUES ('v-fa-4','TRK-FA4','MH01FA004','truck',15.0,'diesel',?,?,?,?,1,120.0,'available','tenant-1')`,
		futureDate, futureDate, futureDate, futureDate)
	require.NoError(t, err)
	_, err = dbConn.Exec(`
		INSERT INTO drivers (id, driver_id, first_name, last_name, phone,
		  license_number, license_expiry, status, tenant_id)
		VALUES ('d-fa-4','DRV-FA4','Amit','Sharma','+919100000004','DL-FA4',?,'available','tenant-1')`,
		futureDate)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('r-fa-4','Lucknow','Varanasi',300.0,5.0,7000.0,'tenant-1')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`
		INSERT INTO trips (id, trip_number, route_id, vehicle_id, driver_id, departure_time, status, tenant_id)
		VALUES ('t-fa-4','TRP-FA-04','r-fa-4','v-fa-4','d-fa-4',datetime('now','-2 hours'),'in_transit','tenant-1')`)
	require.NoError(t, err)

	expID, err := svcs.Kharcha.CreateExpense(ctx, "t-fa-4", "d-fa-4", "fuel", 3000.0, "Fuel", "", 30.0)
	require.NoError(t, err)

	// Run audit twice — exactly 1 row in fuel_claim_audits
	_, err = svcs.FuelAudit.AuditPendingClaims(ctx)
	require.NoError(t, err)
	_, err = svcs.FuelAudit.AuditPendingClaims(ctx)
	require.NoError(t, err)

	var count int
	require.NoError(t, dbConn.QueryRow(`SELECT COUNT(*) FROM fuel_claim_audits WHERE expense_id = ?`, expID).Scan(&count))
	assert.Equal(t, 1, count, "ON CONFLICT upsert must produce exactly 1 audit row")
}

// ─── Test 5: KmplReport — per-vehicle efficiency ──────────────────────────────

func TestFuelAudit_KmplReport(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	now := time.Now().UTC()
	futureDate := now.AddDate(1, 0, 0).Format("2006-01-02")

	for _, item := range []struct{ id, reg string }{
		{"v-kr-1", "MH01KR001"},
		{"v-kr-2", "MH01KR002"},
	} {
		_, err := dbConn.Exec(`
			INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type,
			  capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry,
			  fuel_sensor_fitted, tank_capacity_litres, current_mileage, status, tenant_id)
			VALUES (?,?,?,'truck',15.0,'diesel',?,?,?,?,1,200.0,4.0,'available','tenant-1')`,
			item.id, item.reg, item.reg, futureDate, futureDate, futureDate, futureDate)
		require.NoError(t, err)

		// 50L refill
		_, err = dbConn.Exec(`
			INSERT INTO fuel_events (id, vehicle_id, event_type, estimated_litres, confidence, occurred_at)
			VALUES (?, ?, 'refill_detected', 50.0, 1.0, datetime('now','-1 hour'))`,
			"fe-kr-"+item.id, item.id)
		require.NoError(t, err)

		// 200km driven → computed kmpl = 200/50 = 4.0
		_, err = dbConn.Exec(`
			INSERT INTO telemetry_snapshots (id, vehicle_id, timestamp, speed, fuel_level, odometer)
			VALUES (?,?,datetime('now','-2 hours'),60.0,50.0,1000.0),
			       (?,?,datetime('now','-30 minutes'),50.0,25.0,1200.0)`,
			"ts-kr-"+item.id+"a", item.id, "ts-kr-"+item.id+"b", item.id)
		require.NoError(t, err)
	}

	from := now.AddDate(0, 0, -7)
	rows, err := svcs.FuelAudit.KmplReport(ctx, from, now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, rows, 2, "one row per vehicle with refill events")

	for _, row := range rows {
		assert.InDelta(t, 200.0, row.OdometerDeltaKm, 1.0)
		assert.InDelta(t, 50.0, row.RefillLitres, 0.01)
		assert.InDelta(t, 4.0, row.ComputedKmpl, 0.1)
		assert.InDelta(t, 0.0, row.VariancePct, 2.0)
	}
}

// ─── Test 6: HTTP route existence — /fuel/reports/kmpl registered ─────────────

func TestFuelAudit_KmplRoute_Registered(t *testing.T) {
	_, _, _, _, app := setupComplianceTestEnv(t)

	// Build minimal router with fuel routes (no auth — just verify routing)
	r := chi.NewRouter()
	r.Route("/fuel", app.FuelAudit.Routes)

	// GET /fuel/reports/kmpl should not 404 (ResourcePermission will 403 with no session,
	// but the route must exist — 403 proves chi matched it)
	req := httptest.NewRequest(http.MethodGet, "/fuel/reports/kmpl", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusNotFound, w.Code,
		"/fuel/reports/kmpl must be registered (got 404 = route missing)")

	// Same check for dashboard and audit queue
	for _, path := range []string{"/fuel/audit", "/fuel/audit/queue"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.NotEqual(t, http.StatusNotFound, w.Code, "%s must be registered", path)
	}

	// POST /fuel/audit/run
	req = httptest.NewRequest(http.MethodPost, "/fuel/audit/run",
		strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.NotEqual(t, http.StatusNotFound, w.Code, "/fuel/audit/run must be registered")
}
