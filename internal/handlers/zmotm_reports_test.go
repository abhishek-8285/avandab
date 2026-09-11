package handlers

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func setupZMOTMReportsTestApp(t *testing.T) (*App, *chi.Mux) {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	db := newReportsTestDB(t)
	repo := sqlite.NewRepository(db)
	bus := events.NewInMemoryBus()
	cfg := &config.Config{
		AppEnv:        "testing",
		Port:          "8080",
		ExportMaxRows: 50000,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)

	authSvc := &mockAuthSvc{
		allowed: map[string]bool{
			"authorized-user:reports:read": true,
		},
	}

	tmpl, err := parseTemplates(authSvc)
	require.NoError(t, err)

	app := &App{
		DB:        db,
		Config:    cfg,
		Templates: tmpl,
		AuthSrv:   authSvc,
		Services:  services,
	}
	reportHandlers := &ReportHandlers{App: app}
	app.Reports = reportHandlers

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			userID := req.Header.Get("X-Test-User")
			tenant := req.Header.Get("X-Test-Tenant")
			if tenant == "" {
				tenant = string(shared.DefaultTenant)
			}
			ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant))
			if userID != "" {
				ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
					UserID: userID,
					Role:   "admin",
				})
			}
			req = req.WithContext(ctx)
			next.ServeHTTP(w, req)
		})
	})

	r.Route("/reports", func(sub chi.Router) {
		reportHandlers.Routes(sub)
	})
	reportHandlers.RegisterAPIRoutes(r)

	return app, r
}

func TestVehicleMasterReport_ParityAndIsolation(t *testing.T) {
	app, r := setupZMOTMReportsTestApp(t)

	// Ensure tenants exist for FK triggers
	_, _ = app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-alpha', 'Alpha Corp', 'alpha')`)
	_, _ = app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-beta', 'Beta Corp', 'beta')`)

	// Seed vehicles for tenant-alpha
	_, err := app.DB.Exec(`
INSERT INTO vehicles (
    id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type,
    status, fleet_class, ownership, manufacturer, model, acquisition_date, acquisition_value,
    acquisition_currency, fleet_number, chassis_no, engine_number
) VALUES
('veh-alpha-1', 'tenant-alpha', 'MH12AB1234', 'TRK-001', 'truck', 10000, 'diesel',
 'available', 'CV', 'O', 'Tata Motors', 'Prima 4028', '2023-01-15', 3500000.0,
 'INR', 'FL-101', 'MAT12345678', 'ENG98765432'),
('veh-alpha-2', 'tenant-alpha', 'MH12CD5678', 'TRK-002', 'truck', 5000, 'diesel',
 'running', 'PV', 'C', 'Ashok Leyland', 'Ecomet 1214', '2023-05-20', 2200000.0,
 'INR', 'FL-102', 'MAL87654321', 'ENG12345678')
`)
	require.NoError(t, err)

	// Seed vehicle for tenant-beta (must not appear in alpha's report)
	_, err = app.DB.Exec(`
INSERT INTO vehicles (
    id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type,
    status, fleet_class, ownership, manufacturer, model, acquisition_date, acquisition_value,
    acquisition_currency, fleet_number, chassis_no, engine_number
) VALUES
('veh-beta-1', 'tenant-beta', 'DL01XY9999', 'BETA-001', 'truck', 8000, 'diesel',
 'available', 'CV', 'F', 'Mahindra', 'Blazo X', '2024-02-01', 2800000.0,
 'INR', 'FL-999', 'MAM99999999', 'ENG99999999')
`)
	require.NoError(t, err)

	// 1. CSV Export via /reports/vehicles.csv
	reqCSV := httptest.NewRequest("GET", "/reports/vehicles.csv", nil)
	reqCSV.Header.Set("X-Test-User", "authorized-user")
	reqCSV.Header.Set("X-Test-Tenant", "tenant-alpha")
	wCSV := httptest.NewRecorder()
	r.ServeHTTP(wCSV, reqCSV)

	assert.Equal(t, http.StatusOK, wCSV.Code)
	body := wCSV.Body.Bytes()
	require.True(t, len(body) >= 3, "must contain BOM")
	content := string(body[3:])

	reader := csv.NewReader(strings.NewReader(content))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 3, "1 header row + 2 alpha vehicle rows")

	expectedHeaders := []string{
		"Vehicle No", "Type", "Category", "Manufacturer", "Model",
		"Purchase Date", "Purchase Value", "Currency", "Fuel Type",
		"Fleet Number", "Chassis No", "Engine SNo",
	}
	assert.Equal(t, expectedHeaders, records[0], "p.18 column layout")

	// Row 1 assertions
	row1 := records[1]
	assert.Equal(t, "MH12AB1234", row1[0])
	assert.Equal(t, "O", row1[1])
	assert.Equal(t, "CV", row1[2])
	assert.Equal(t, "Tata Motors", row1[3])
	assert.Equal(t, "Prima 4028", row1[4])
	assert.Equal(t, "2023-01-15", row1[5])
	assert.Equal(t, "3500000.00", row1[6])
	assert.Equal(t, "INR", row1[7])
	assert.Equal(t, "diesel", row1[8])
	assert.Equal(t, "FL-101", row1[9])
	assert.Equal(t, "MAT12345678", row1[10])
	assert.Equal(t, "ENG98765432", row1[11])

	// Row 2 assertions
	row2 := records[2]
	assert.Equal(t, "MH12CD5678", row2[0])
	assert.Equal(t, "C", row2[1])
	assert.Equal(t, "PV", row2[2])

	// Check that tenant-beta vehicle was NOT leaked
	assert.NotContains(t, content, "DL01XY9999")

	// 2. Alias /reports/vehicle-master.csv should return identical headers
	reqAlias := httptest.NewRequest("GET", "/reports/vehicle-master.csv", nil)
	reqAlias.Header.Set("X-Test-User", "authorized-user")
	reqAlias.Header.Set("X-Test-Tenant", "tenant-alpha")
	wAlias := httptest.NewRecorder()
	r.ServeHTTP(wAlias, reqAlias)
	assert.Equal(t, http.StatusOK, wAlias.Code)

	// 3. REST API /api/v1/reports/vehicle-master
	reqAPI := httptest.NewRequest("GET", "/api/v1/reports/vehicle-master", nil)
	reqAPI.Header.Set("X-Test-User", "authorized-user")
	reqAPI.Header.Set("X-Test-Tenant", "tenant-alpha")
	wAPI := httptest.NewRecorder()
	r.ServeHTTP(wAPI, reqAPI)

	assert.Equal(t, http.StatusOK, wAPI.Code)
	var apiResp struct {
		Vehicles []VehicleMasterDTO `json:"vehicles"`
		Total    int64              `json:"total"`
	}
	err = json.NewDecoder(wAPI.Body).Decode(&apiResp)
	require.NoError(t, err)
	assert.Equal(t, int64(2), apiResp.Total)
	require.Len(t, apiResp.Vehicles, 2)
	assert.Equal(t, "MH12AB1234", apiResp.Vehicles[0].VehicleNo)
	assert.Equal(t, "Tata Motors", apiResp.Vehicles[0].Manufacturer)
}

func TestGateRegisterReport_ParityAndIsolation(t *testing.T) {
	app, r := setupZMOTMReportsTestApp(t)

	_, _ = app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-alpha', 'Alpha Corp', 'alpha')`)
	_, _ = app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-beta', 'Beta Corp', 'beta')`)

	// Seed vehicle & driver
	_, err := app.DB.Exec(`
INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, status)
VALUES ('veh-g1', 'tenant-alpha', 'MH14XY1000', 'TRK-G1', 'truck', 10000, 'diesel', 'running');
INSERT INTO drivers (id, tenant_id, driver_id, first_name, last_name, phone, license_number, status)
VALUES ('drv-g1', 'tenant-alpha', 'DRV-G1', 'Ramesh', 'Patil', '+919876543210', 'DL-MH-12345', 'available');
`)
	require.NoError(t, err)

	// Seed routes
	_, err = app.DB.Exec(`
INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare)
VALUES
('route-alpha-1', 'tenant-alpha', 'Pune Depot', 'Mumbai Hub', 150.0, 3.0, 5000.0),
('route-beta-1', 'tenant-beta', 'Delhi Depot', 'Agra Hub', 200.0, 4.0, 6000.0);
`)
	require.NoError(t, err)

	now := time.Now().UTC()
	startOdo := 12500.0
	closeOdo := 12840.5
	depTime := now.Add(-5 * time.Hour)
	arrTime := now.Add(-1 * time.Hour)

	// Seed completed trip for tenant-alpha with start_odometer and close_odometer
	_, err = app.DB.Exec(`
INSERT INTO trips (
    id, tenant_id, trip_number, vehicle_id, driver_id, route_id, status,
    start_odometer, close_odometer, gate_facility_id,
    started_at, completed_at, departure_time, arrival_time
) VALUES (
    'trip-alpha-g1', 'tenant-alpha', 'TRIP-2026-001', 'veh-g1', 'drv-g1', 'route-alpha-1', 'completed',
    ?, ?, 'FAC-PUNE-NORTH',
    ?, ?, ?, ?
)`, startOdo, closeOdo, depTime, arrTime, depTime, arrTime)
	require.NoError(t, err)

	// Seed trip for tenant-beta
	_, err = app.DB.Exec(`
INSERT INTO trips (
    id, tenant_id, trip_number, route_id, departure_time, status, start_odometer, close_odometer
) VALUES (
    'trip-beta-g1', 'tenant-beta', 'TRIP-BETA-001', 'route-beta-1', ?, 'completed', 5000.0, 5200.0
)`, depTime)
	require.NoError(t, err)

	// 1. CSV Export
	reqCSV := httptest.NewRequest("GET", "/reports/gate-register.csv", nil)
	reqCSV.Header.Set("X-Test-User", "authorized-user")
	reqCSV.Header.Set("X-Test-Tenant", "tenant-alpha")
	wCSV := httptest.NewRecorder()
	r.ServeHTTP(wCSV, reqCSV)

	assert.Equal(t, http.StatusOK, wCSV.Code)
	body := wCSV.Body.Bytes()
	content := string(body[3:])

	reader := csv.NewReader(strings.NewReader(content))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2, "1 header + 1 alpha row")

	expectedHeaders := []string{
		"Trip No", "Vehicle No", "Driver", "Route", "Gate Facility",
		"Gate Out Time", "Gate In Time", "Start Reading", "Close Reading",
		"Distance KM", "Status",
	}
	assert.Equal(t, expectedHeaders, records[0])

	row := records[1]
	assert.Equal(t, "TRIP-2026-001", row[0])
	assert.Equal(t, "MH14XY1000", row[1])
	assert.Equal(t, "Ramesh Patil", row[2])
	assert.Equal(t, "FAC-PUNE-NORTH", row[4])
	assert.Equal(t, "12500.0", row[7])
	assert.Equal(t, "12840.5", row[8])
	assert.Equal(t, "340.5", row[9], "Distance KM = 12840.5 - 12500.0 = 340.5")
	assert.Equal(t, "completed", row[10])

	// Tenant beta isolated
	assert.NotContains(t, content, "TRIP-BETA-001")

	// 2. REST API /api/v1/reports/gate-register
	reqAPI := httptest.NewRequest("GET", "/api/v1/reports/gate-register", nil)
	reqAPI.Header.Set("X-Test-User", "authorized-user")
	reqAPI.Header.Set("X-Test-Tenant", "tenant-alpha")
	wAPI := httptest.NewRecorder()
	r.ServeHTTP(wAPI, reqAPI)

	assert.Equal(t, http.StatusOK, wAPI.Code)
	var apiResp struct {
		Records []GateRegisterDTO `json:"records"`
		Total   int64             `json:"total"`
	}
	err = json.NewDecoder(wAPI.Body).Decode(&apiResp)
	require.NoError(t, err)
	assert.Equal(t, int64(1), apiResp.Total)
	require.Len(t, apiResp.Records, 1)
	assert.Equal(t, "FAC-PUNE-NORTH", apiResp.Records[0].GateFacility)
	assert.InDelta(t, 340.5, apiResp.Records[0].DistanceKM, 0.01)
}

func TestFuelKMPLReport_ParityAndIsolation(t *testing.T) {
	app, r := setupZMOTMReportsTestApp(t)

	_, _ = app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-alpha', 'Alpha Corp', 'alpha')`)
	_, _ = app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-beta', 'Beta Corp', 'beta')`)

	// Seed vehicles: dispenser pump (FS) and truck
	_, err := app.DB.Exec(`
INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, status, fleet_class)
VALUES
('veh-pump-1', 'tenant-alpha', 'PUMP-DEPOT-01', 'PUMP-01', 'truck', 50000, 'diesel', 'available', 'FS'),
('veh-truck-1', 'tenant-alpha', 'MH12FF5000', 'TRK-5000', 'truck', 10000, 'diesel', 'running', 'CV');
`)
	require.NoError(t, err)

	now := time.Now().UTC()

	// Seed two consecutive fuel issues for veh-truck-1
	// Issue 1: 100 Litres at odometer 20000 km
	// Issue 2: 120 Litres at odometer 20480 km
	// Distance between issues = 480 km -> KMPL = 480 / 120 = 4.0 KMPL
	_, err = app.DB.Exec(`
INSERT INTO fuel_issues (
    id, tenant_id, issue_number, fuel_station_id, vehicle_id, litres_issued,
    vehicle_odometer, rate_per_litre, total_cost, issued_at
) VALUES
('fuel-1', 'tenant-alpha', 'FI-2026-001', 'veh-pump-1', 'veh-truck-1', 100.0,
 20000.0, 95.0, 9500.0, ?),
('fuel-2', 'tenant-alpha', 'FI-2026-002', 'veh-pump-1', 'veh-truck-1', 120.0,
 20480.0, 96.0, 11520.0, ?)
`, now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	require.NoError(t, err)

	// Seed fuel issue for tenant-beta
	_, err = app.DB.Exec(`
INSERT INTO fuel_issues (
    id, tenant_id, issue_number, fuel_station_id, vehicle_id, litres_issued,
    vehicle_odometer, rate_per_litre, total_cost, issued_at
) VALUES
('fuel-beta-1', 'tenant-beta', 'FI-BETA-999', 'veh-pump-1', 'veh-truck-1', 50.0,
 10000.0, 95.0, 4750.0, ?)
`, now)
	require.NoError(t, err)

	// 1. CSV Export
	reqCSV := httptest.NewRequest("GET", "/reports/fuel-kmpl.csv", nil)
	reqCSV.Header.Set("X-Test-User", "authorized-user")
	reqCSV.Header.Set("X-Test-Tenant", "tenant-alpha")
	wCSV := httptest.NewRecorder()
	r.ServeHTTP(wCSV, reqCSV)

	assert.Equal(t, http.StatusOK, wCSV.Code)
	body := wCSV.Body.Bytes()
	content := string(body[3:])

	reader := csv.NewReader(strings.NewReader(content))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 3, "1 header + 2 alpha records")

	expectedHeaders := []string{
		"Vehicle No", "Date", "Issue No", "Fuel Station", "Driver", "Trip No",
		"Litres Issued", "Rate", "Total Cost", "Odometer", "Previous Odometer",
		"Distance KM", "KMPL",
	}
	assert.Equal(t, expectedHeaders, records[0])

	// Issue 1: initial fill (no previous odometer)
	assert.Equal(t, "MH12FF5000", records[1][0])
	assert.Equal(t, "FI-2026-001", records[1][2])
	assert.Equal(t, "PUMP-DEPOT-01", records[1][3])
	assert.Equal(t, "100.00", records[1][6])
	assert.Equal(t, "20000.0", records[1][9])
	assert.Equal(t, "", records[1][10], "no previous odometer")

	// Issue 2: second fill (reconciles 480 KM and 4.00 KMPL)
	assert.Equal(t, "MH12FF5000", records[2][0])
	assert.Equal(t, "FI-2026-002", records[2][2])
	assert.Equal(t, "120.00", records[2][6])
	assert.Equal(t, "20480.0", records[2][9])
	assert.Equal(t, "20000.0", records[2][10])
	assert.Equal(t, "480.0", records[2][11])
	assert.Equal(t, "4.00", records[2][12])

	// Tenant beta isolated
	assert.NotContains(t, content, "FI-BETA-999")

	// 2. REST API /api/v1/reports/fuel-kmpl
	reqAPI := httptest.NewRequest("GET", "/api/v1/reports/fuel-kmpl", nil)
	reqAPI.Header.Set("X-Test-User", "authorized-user")
	reqAPI.Header.Set("X-Test-Tenant", "tenant-alpha")
	wAPI := httptest.NewRecorder()
	r.ServeHTTP(wAPI, reqAPI)

	assert.Equal(t, http.StatusOK, wAPI.Code)
	var apiResp struct {
		Records []FuelKMPLDTO `json:"records"`
		Total   int64         `json:"total"`
	}
	err = json.NewDecoder(wAPI.Body).Decode(&apiResp)
	require.NoError(t, err)
	assert.Equal(t, int64(2), apiResp.Total)
	require.Len(t, apiResp.Records, 2)
	assert.NotNil(t, apiResp.Records[1].KMPL)
	assert.InDelta(t, 4.0, *apiResp.Records[1].KMPL, 0.01)
}

func TestBreakdownReport_ParityAndIsolation(t *testing.T) {
	app, r := setupZMOTMReportsTestApp(t)

	_, _ = app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-alpha', 'Alpha Corp', 'alpha')`)
	_, _ = app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-beta', 'Beta Corp', 'beta')`)

	// Seed vehicle
	_, err := app.DB.Exec(`
INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, status)
VALUES ('veh-brk-1', 'tenant-alpha', 'MH12BK9000', 'TRK-9000', 'truck', 10000, 'diesel', 'maintenance');
`)
	require.NoError(t, err)

	now := time.Now().UTC()

	// Seed ops_alert (vehicle_breakdown)
	_, err = app.DB.Exec(`
INSERT INTO ops_alerts (
    id, tenant_id, alert_type, severity, title, description, entity_type, entity_id,
    status, resolution_note, created_at, resolved_at
) VALUES (
    'alert-bk-1', 'tenant-alpha', 'vehicle_breakdown', 'critical', 'Engine Overheating',
    'Radiator coolant leak on highway NH48', 'vehicle', 'veh-brk-1',
    'resolved', 'Replaced coolant hose and refilled coolant', ?, ?
)`, now.Add(-10*time.Hour), now.Add(-2*time.Hour))
	require.NoError(t, err)

	// Seed work_orders (job card)
	_, err = app.DB.Exec(`
INSERT INTO work_orders (
    id, tenant_id, vehicle_id, title, description, vendor, status, created_at
) VALUES (
    'wo-bk-1', 'tenant-alpha', 'veh-brk-1', 'Brake Lining Inspection',
    'Routine 20,000 km brake shoe replacement', 'Bosch Authorized Service',
    'open', ?
)`, now.Add(-1*time.Hour))
	require.NoError(t, err)

	// Seed breakdown alert for tenant-beta
	_, err = app.DB.Exec(`
INSERT INTO ops_alerts (
    id, tenant_id, alert_type, severity, title, status, created_at
) VALUES (
    'alert-beta-bk', 'tenant-beta', 'vehicle_breakdown', 'critical', 'Transmission Failure',
    'open', ?
)`, now)
	require.NoError(t, err)

	// 1. CSV Export
	reqCSV := httptest.NewRequest("GET", "/reports/breakdown.csv", nil)
	reqCSV.Header.Set("X-Test-User", "authorized-user")
	reqCSV.Header.Set("X-Test-Tenant", "tenant-alpha")
	wCSV := httptest.NewRecorder()
	r.ServeHTTP(wCSV, reqCSV)

	assert.Equal(t, http.StatusOK, wCSV.Code)
	body := wCSV.Body.Bytes()
	content := string(body[3:])

	reader := csv.NewReader(strings.NewReader(content))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 3, "1 header + 1 alert + 1 work order")

	expectedHeaders := []string{
		"Notification ID", "Vehicle No", "Type", "Severity", "Status",
		"Reported At", "Resolved At", "Description", "Resolution Note",
	}
	assert.Equal(t, expectedHeaders, records[0])

	assert.Contains(t, content, "alert-bk-1")
	assert.Contains(t, content, "MH12BK9000")
	assert.Contains(t, content, "Breakdown Alert")
	assert.Contains(t, content, "Engine Overheating")
	assert.Contains(t, content, "wo-bk-1")
	assert.Contains(t, content, "Job Card")

	// Beta alert isolated
	assert.NotContains(t, content, "Transmission Failure")

	// 2. REST API /api/v1/reports/breakdown
	reqAPI := httptest.NewRequest("GET", "/api/v1/reports/breakdown", nil)
	reqAPI.Header.Set("X-Test-User", "authorized-user")
	reqAPI.Header.Set("X-Test-Tenant", "tenant-alpha")
	wAPI := httptest.NewRecorder()
	r.ServeHTTP(wAPI, reqAPI)

	assert.Equal(t, http.StatusOK, wAPI.Code)
	var apiResp struct {
		Records []BreakdownDTO `json:"records"`
		Total   int64          `json:"total"`
	}
	err = json.NewDecoder(wAPI.Body).Decode(&apiResp)
	require.NoError(t, err)
	assert.Equal(t, int64(2), apiResp.Total)
	require.Len(t, apiResp.Records, 2)
}

func TestZMOTMReports_RBACForbidden(t *testing.T) {
	_, r := setupZMOTMReportsTestApp(t)

	endpoints := []string{
		"/reports/vehicle-master.csv",
		"/reports/gate-register.csv",
		"/reports/fuel-kmpl.csv",
		"/reports/breakdown.csv",
		"/api/v1/reports/vehicle-master",
		"/api/v1/reports/gate-register",
		"/api/v1/reports/fuel-kmpl",
		"/api/v1/reports/breakdown",
	}

	for _, ep := range endpoints {
		t.Run("RBAC_Forbidden_"+ep, func(t *testing.T) {
			req := httptest.NewRequest("GET", ep, nil)
			req.Header.Set("X-Test-User", "unauthorized-user")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code, "Expected 403 Forbidden for %s", ep)
		})
	}
}
