package test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/handlers"
	"transport-app/internal/shared"
)

// Helper to seed base settlement testing data
func seedSettlementTestData(t *testing.T, db *sql.DB) {
	t.Helper()
	// Drivers
	_, _ = db.Exec(`INSERT OR REPLACE INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, pan, status) VALUES ('drv-pan-1', 'DRV-P1', 'Ramesh', 'Kumar', '9800000001', 'DL-MH-001', '2030-12-31', 'ABCDE1234F', 'available')`)
	_, _ = db.Exec(`INSERT OR REPLACE INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, pan, status) VALUES ('drv-nopan-1', 'DRV-P2', 'Suresh', 'Singh', '9800000002', 'DL-MH-002', '2030-12-31', NULL, 'available')`)

	// Routes
	_, _ = db.Exec(`INSERT OR REPLACE INTO routes (id, source, destination, distance, estimated_hours, standard_fare) VALUES ('route-stl-1', 'Mumbai', 'Pune', 420.0, 8.0, 5000.0)`)

	// Bookings
	_, _ = db.Exec(`INSERT OR REPLACE INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('bkg-stl-1', '1', 'BK-STL-1', 'cust-mh', 'route-stl-1', '2026-08-20', 'truck', 'confirmed', 5000.0)`)

	// Trips
	_, _ = db.Exec(`INSERT OR REPLACE INTO trips (id, tenant_id, trip_number, booking_id, route_id, driver_id, status, departure_time) VALUES ('trip-stl-1', '1', 'TRP-STL-1', 'bkg-stl-1', 'route-stl-1', 'drv-pan-1', 'completed', datetime('now'))`)
	_, _ = db.Exec(`INSERT OR REPLACE INTO trips (id, tenant_id, trip_number, booking_id, route_id, driver_id, status, departure_time) VALUES ('trip-stl-2', '1', 'TRP-STL-2', 'bkg-stl-1', 'route-stl-1', 'drv-nopan-1', 'completed', datetime('now'))`)
}

// 1. Test Migration 00051 RoundTrip
func TestSettlement_Migration00051_RoundTrip(t *testing.T) {
	name := fmt.Sprintf("test_stl_mig_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=foreign_keys(OFF)")
	require.NoError(t, err)
	defer db.Close()

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../db/migrations"))
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

	// Check settlement_lines table exists
	var count int
	err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='settlement_lines'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "settlement_lines table must exist")

	// Check company_config seeds
	var flagCount int
	err = db.QueryRow(`SELECT count(*) FROM company_config WHERE key IN ('settlement_rate_model', 'settlement_rate_per_km', 'settlement_fixed_fare', 'settlement_commission_pct', 'tds_section', 'tds_rate_with_pan', 'tds_rate_without_pan')`).Scan(&flagCount)
	require.NoError(t, err)
	assert.Equal(t, 7, flagCount, "7 settlement config keys must be seeded")

	// Check RBAC permissions
	var permCount int
	err = db.QueryRow(`SELECT count(*) FROM permissions WHERE name IN ('settlements:read', 'settlements:write', 'settlements:approve')`).Scan(&permCount)
	require.NoError(t, err)
	assert.Equal(t, 3, permCount, "3 settlement permissions must be seeded")

	// Rollback to 50
	require.NoError(t, goose.DownTo(db, "../db/migrations", 50))

	err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='settlement_lines'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "settlement_lines table must be dropped after rollback")

	// Re-apply
	require.NoError(t, goose.Up(db, "../db/migrations"))
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
}

// 2. Test Settlement Generation, Persistence & Idempotency
func TestSettlement_Generate_Persistence_And_Idempotency(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	seedSettlementTestData(t, dbConn)
	ctx := context.Background()

	// First generation
	rec1, err := svcs.Settlements.GenerateSettlement(ctx, "trip-stl-1", false)
	require.NoError(t, err)
	assert.NotEmpty(t, rec1.ID)
	assert.Equal(t, domain.TripID("trip-stl-1"), rec1.TripID)
	assert.Equal(t, domain.DriverID("drv-pan-1"), rec1.DriverID)
	assert.Equal(t, "pending", rec1.Status)
	assert.NotEmpty(t, rec1.Lines, "Lines must be populated")

	// Verify persistence in DB
	var dbID, dbStatus string
	var gross, net float64
	err = dbConn.QueryRow(`SELECT id, status, gross_fare, net_payout FROM driver_settlements WHERE trip_id = 'trip-stl-1'`).Scan(&dbID, &dbStatus, &gross, &net)
	require.NoError(t, err, "Settlement MUST be persisted in DB")
	assert.Equal(t, rec1.ID, dbID)
	assert.Equal(t, "pending", dbStatus)

	// Idempotency: re-running without force returns exact same settlement
	rec2, err := svcs.Settlements.GenerateSettlement(ctx, "trip-stl-1", false)
	require.NoError(t, err)
	assert.Equal(t, rec1.ID, rec2.ID, "Re-generating without force must return existing settlement")

	var rowCount int
	_ = dbConn.QueryRow(`SELECT count(*) FROM driver_settlements WHERE trip_id = 'trip-stl-1'`).Scan(&rowCount)
	assert.Equal(t, 1, rowCount, "Exactly 1 settlement row must exist for the trip")

	// Force recalculation
	recForce, err := svcs.Settlements.GenerateSettlement(ctx, "trip-stl-1", true)
	require.NoError(t, err)
	assert.Equal(t, rec1.ID, recForce.ID)
}

// 3. Test Rate Models (per_km, fixed, commission_pct)
func TestSettlement_RateModels(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	seedSettlementTestData(t, dbConn)
	ctx := context.Background()

	// Model 1: per_km (420 km * 11.90 = 4998.00)
	_, _ = dbConn.Exec(`UPDATE company_config SET value = 'per_km' WHERE key = 'settlement_rate_model'`)
	recKM, err := svcs.Settlements.GenerateSettlement(ctx, "trip-stl-1", true)
	require.NoError(t, err)
	assert.Equal(t, 4998.00, recKM.GrossFare)
	assert.Equal(t, 0.0, recKM.CommissionAmount)
	assert.Equal(t, "per_km", recKM.RateModel)

	// Model 2: fixed (5000.00)
	_, _ = dbConn.Exec(`UPDATE company_config SET value = 'fixed' WHERE key = 'settlement_rate_model'`)
	recFixed, err := svcs.Settlements.GenerateSettlement(ctx, "trip-stl-1", true)
	require.NoError(t, err)
	assert.Equal(t, 5000.00, recFixed.GrossFare)
	assert.Equal(t, 0.0, recFixed.CommissionAmount)
	assert.Equal(t, "fixed", recFixed.RateModel)

	// Model 3: commission_pct (Booking price=5000, 5% commission = 250, Gross = 4750)
	_, _ = dbConn.Exec(`UPDATE company_config SET value = 'commission_pct' WHERE key = 'settlement_rate_model'`)
	recComm, err := svcs.Settlements.GenerateSettlement(ctx, "trip-stl-1", true)
	require.NoError(t, err)
	assert.Equal(t, 4750.00, recComm.GrossFare)
	assert.Equal(t, 250.00, recComm.CommissionAmount)
	assert.Equal(t, "commission_pct", recComm.RateModel)
}

// 4. Test TDS Section 194C (1% with PAN vs 2% without PAN)
func TestSettlement_TDS_Calculation(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	seedSettlementTestData(t, dbConn)
	ctx := context.Background()

	_, _ = dbConn.Exec(`UPDATE company_config SET value = 'fixed' WHERE key = 'settlement_rate_model'`)
	_, _ = dbConn.Exec(`UPDATE company_config SET value = '5000.00' WHERE key = 'settlement_fixed_fare'`)

	// Driver 1 with PAN -> 1% of 5000 = 50.00 TDS
	recPAN, err := svcs.Settlements.GenerateSettlement(ctx, "trip-stl-1", true)
	require.NoError(t, err)
	assert.Equal(t, 1.0, recPAN.TDSRate)
	assert.Equal(t, 50.0, recPAN.TDSAmount)
	assert.Equal(t, 4950.0, recPAN.NetPayout)

	// Driver 2 without PAN -> 2% of 5000 = 100.00 TDS (Sec 206AA)
	recNoPAN, err := svcs.Settlements.GenerateSettlement(ctx, "trip-stl-2", true)
	require.NoError(t, err)
	assert.Equal(t, 2.0, recNoPAN.TDSRate)
	assert.Equal(t, 100.0, recNoPAN.TDSAmount)
	assert.Equal(t, 4900.0, recNoPAN.NetPayout)
}

// 5. Test Net Payout Floor at Zero
func TestSettlement_NetPayout_Floor(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	seedSettlementTestData(t, dbConn)
	ctx := context.Background()

	// Add huge kharcha/advances exceeding gross fare
	_, _ = dbConn.Exec(`INSERT INTO driver_expenses (id, trip_id, driver_id, expense_type, category, amount, status) VALUES ('exp-huge', 'trip-stl-1', 'drv-pan-1', 'fuel', 'fuel', 10000.0, 'approved')`)

	rec, err := svcs.Settlements.GenerateSettlement(ctx, "trip-stl-1", true)
	require.NoError(t, err)
	assert.Equal(t, 0.0, rec.NetPayout, "Net payout must never fall below 0")
}

// 6. Test Kharcha Approval Updates Existing Settlement
func TestSettlement_KharchaApproval_Wiring(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	seedSettlementTestData(t, dbConn)
	ctx := context.Background()

	_, _ = dbConn.Exec(`UPDATE company_config SET value = 'fixed' WHERE key = 'settlement_rate_model'`)
	_, _ = dbConn.Exec(`UPDATE company_config SET value = '5000.00' WHERE key = 'settlement_fixed_fare'`)

	// 1. Generate settlement first
	recInitial, err := svcs.Settlements.GenerateSettlement(ctx, "trip-stl-1", true)
	require.NoError(t, err)
	assert.Equal(t, 4950.0, recInitial.NetPayout)

	// 2. Submit a kharcha expense
	_, _ = dbConn.Exec(`INSERT INTO driver_expenses (id, trip_id, driver_id, expense_type, category, amount, status) VALUES ('exp-kh-1', 'trip-stl-1', 'drv-pan-1', 'toll', 'toll', 300.0, 'pending')`)

	// 3. Approve expense via KharchaService
	err = svcs.Kharcha.ApproveExpense(ctx, "exp-kh-1", "admin-1")
	require.NoError(t, err)

	// 4. Verify settlement row updated
	recUpdated, err := svcs.Settlements.GetSettlement(ctx, recInitial.ID)
	require.NoError(t, err)
	assert.Equal(t, 300.0, recUpdated.AdvancesKharcha)
	assert.Equal(t, 4650.0, recUpdated.NetPayout) // 4950 - 300

	// 5. Verify settlement line added
	var lineCount int
	_ = dbConn.QueryRow(`SELECT count(*) FROM settlement_lines WHERE ref_id = 'exp-kh-1'`).Scan(&lineCount)
	assert.Equal(t, 1, lineCount, "Settlement line must be inserted for approved expense")
}

// 7. Test Status Machine: MarkPaid, Confirm, Dispute
func TestSettlement_Status_Machine_And_Dispute(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	seedSettlementTestData(t, dbConn)
	ctx := context.Background()

	rec, err := svcs.Settlements.GenerateSettlement(ctx, "trip-stl-1", true)
	require.NoError(t, err)
	assert.Equal(t, "pending", rec.Status)

	// Mark Paid
	paidTime := time.Now().UTC()
	recPaid, err := svcs.Settlements.MarkPaid(ctx, rec.ID, "TXN-ICICI-9999", paidTime)
	require.NoError(t, err)
	assert.Equal(t, "paid", recPaid.Status)
	assert.NotNil(t, recPaid.PaymentRef)
	assert.Equal(t, "TXN-ICICI-9999", *recPaid.PaymentRef)
	assert.NotNil(t, recPaid.PaidAt)

	// Confirm
	recConf, err := svcs.Settlements.ConfirmSettlement(ctx, rec.ID)
	require.NoError(t, err)
	assert.Equal(t, "paid", recConf.Status)
	assert.NotNil(t, recConf.ConfirmedAt)

	// Dispute
	recDisp, err := svcs.Settlements.DisputeSettlement(ctx, rec.ID, "KM odometer discrepancy", 5100.0)
	require.NoError(t, err)
	assert.Equal(t, "disputed", recDisp.Status)
	assert.NotNil(t, recDisp.DisputedAt)
	assert.NotNil(t, recDisp.DisputeReason)
	assert.Equal(t, "KM odometer discrepancy", *recDisp.DisputeReason)
}

// 8. Test HTTP Endpoints & RBAC
func TestSettlement_HTTP_API_Endpoints_And_RBAC(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	seedSettlementTestData(t, dbConn)

	appAuth := &handlers.App{DB: dbConn, AuthSrv: &mockPhase6Auth{allowed: true}}
	settleHandler := handlers.NewSettlementHandlers(appAuth, svcs.Settlements, &mockPhase6Auth{allowed: true})

	r := chi.NewRouter()
	settleHandler.Mount(r)

	ctxAdmin := context.WithValue(shared.ContextWithTenantID(context.Background(), "1"), auth.ContextUser, &auth.SessionData{UserID: "admin-1", Role: "admin"})

	// 1. POST /api/settlements/generate
	genBody := `{"trip_id":"trip-stl-1","force_recompute":true}`
	reqGen := httptest.NewRequest("POST", "/api/settlements/generate", bytes.NewReader([]byte(genBody))).WithContext(ctxAdmin)
	reqGen.Header.Set("Content-Type", "application/json")
	recGen := httptest.NewRecorder()
	r.ServeHTTP(recGen, reqGen)
	assert.Equal(t, http.StatusOK, recGen.Code)

	var genResp map[string]interface{}
	err := json.Unmarshal(recGen.Body.Bytes(), &genResp)
	require.NoError(t, err)
	settlementID := genResp["settlement_id"].(string)
	assert.NotEmpty(t, settlementID)

	// 2. GET /api/settlements
	reqList := httptest.NewRequest("GET", "/api/settlements", nil).WithContext(ctxAdmin)
	recList := httptest.NewRecorder()
	r.ServeHTTP(recList, reqList)
	assert.Equal(t, http.StatusOK, recList.Code)

	// 3. GET /api/settlements/{id}/deductions
	reqDed := httptest.NewRequest("GET", fmt.Sprintf("/api/settlements/%s/deductions", settlementID), nil).WithContext(ctxAdmin)
	recDed := httptest.NewRecorder()
	r.ServeHTTP(recDed, reqDed)
	assert.Equal(t, http.StatusOK, recDed.Code)

	// 4. POST /api/settlements/{id}/mark-paid
	paidBody := `{"payment_ref":"TXN-HDFC-1234","paid_at":"2026-08-20T10:00:00Z"}`
	reqPaid := httptest.NewRequest("POST", fmt.Sprintf("/api/settlements/%s/mark-paid", settlementID), bytes.NewReader([]byte(paidBody))).WithContext(ctxAdmin)
	reqPaid.Header.Set("Content-Type", "application/json")
	recPaid := httptest.NewRecorder()
	r.ServeHTTP(recPaid, reqPaid)
	assert.Equal(t, http.StatusOK, recPaid.Code)

	// 5. POST /api/settlements/{id}/confirm
	reqConf := httptest.NewRequest("POST", fmt.Sprintf("/api/settlements/%s/confirm", settlementID), nil).WithContext(ctxAdmin)
	recConf := httptest.NewRecorder()
	r.ServeHTTP(recConf, reqConf)
	assert.Equal(t, http.StatusOK, recConf.Code)

	// 6. POST /api/settlements/{id}/dispute
	dispBody := `{"reason":"Fuel claim deducted twice","expected_net":4800.0}`
	reqDisp := httptest.NewRequest("POST", fmt.Sprintf("/api/settlements/%s/dispute", settlementID), bytes.NewReader([]byte(dispBody))).WithContext(ctxAdmin)
	reqDisp.Header.Set("Content-Type", "application/json")
	recDisp := httptest.NewRecorder()
	r.ServeHTTP(recDisp, reqDisp)
	assert.Equal(t, http.StatusOK, recDisp.Code)

	// 7. RBAC Denied
	appDeny := &handlers.App{DB: dbConn, AuthSrv: &mockPhase6Auth{allowed: false}}
	settleHandlerDeny := handlers.NewSettlementHandlers(appDeny, svcs.Settlements, &mockPhase6Auth{allowed: false})
	rDeny := chi.NewRouter()
	settleHandlerDeny.Mount(rDeny)

	ctxGuest := context.WithValue(shared.ContextWithTenantID(context.Background(), "1"), auth.ContextUser, &auth.SessionData{UserID: "guest-1", Role: "guest"})
	reqDeny := httptest.NewRequest("GET", "/api/settlements", nil).WithContext(ctxGuest)
	recDeny := httptest.NewRecorder()
	rDeny.ServeHTTP(recDeny, reqDeny)
	assert.Equal(t, http.StatusForbidden, recDeny.Code)
}
