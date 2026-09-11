package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	"transport-app/internal/sustainability"
	esgapp "transport-app/internal/sustainability/application"
)

func setupESGTestApp(t *testing.T) (*App, *chi.Mux) {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	db := newReportsTestDB(t)
	repo := sqlite.NewRepository(db)
	bus := events.NewInMemoryBus()
	cfg := &config.Config{
		AppEnv: "testing",
		Port:   "8080",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)

	authSvc := &mockAuthSvc{
		allowed: map[string]bool{
			"admin-user:esg:read":   true,
			"admin-user:esg:write":  true,
			"viewer-user:esg:read":  true,
			"viewer-user:esg:write": false,
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

	esgRepo := sustainability.NewSQLESGRepository(db)
	esgUseCase := esgapp.NewESGUsecase(esgRepo)
	esgHandlers := NewESGHandlers(app, esgUseCase)
	app.ESG = esgHandlers

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
					Role:   "manager",
				})
			}
			req = req.WithContext(ctx)
			next.ServeHTTP(w, req)
		})
	})

	esgHandlers.RegisterAPIRoutes(r)
	return app, r
}

func TestESGAPI_EndpointsAndRBAC(t *testing.T) {
	app, r := setupESGTestApp(t)

	tenantID := "tenant-esg-api"
	_, err := app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ($1, 'ESG Logistics', 'esg-logistics')`, tenantID)
	require.NoError(t, err)

	// Seed Route, Vehicle, Customer, Booking, Trip
	_, err = app.DB.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('rte-esg-1', $1, 'Mumbai', 'Pune', 150.0, 3.5, 12000.0)`, tenantID)
	require.NoError(t, err)

	_, err = app.DB.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, vehicle_type, capacity, fuel_type, tank_capacity_litres)
		VALUES ('veh-esg-1', $1, 'MH12ESG001', 'MH12ESG001', 'truck', 10000, 'diesel', 200.0)`, tenantID)
	require.NoError(t, err)

	_, err = app.DB.Exec(`INSERT INTO customers (id, tenant_id, name, phone) VALUES ('cust-esg-1', $1, 'ESG Client', '9876543210')`, tenantID)
	require.NoError(t, err)

	_, err = app.DB.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, vehicle_type, pickup_date, cargo_weight, price, status)
		VALUES ('bk-esg-1', $1, 'BK-ESG-01', 'cust-esg-1', 'rte-esg-1', 'truck', datetime('now'), 10000.0, 15000.0, 'confirmed')`, tenantID)
	require.NoError(t, err)

	now := time.Now().UTC()
	startStr := now.Add(-5 * time.Hour).Format(time.RFC3339)

	// Seed Trip with fuel_consumed_liters: 40.0
	_, err = app.DB.Exec(`INSERT INTO trips (id, tenant_id, trip_number, route_id, booking_id, vehicle_id, fuel_consumed_liters, departure_time, status)
		VALUES ('trip-esg-1', $1, 'TRIP-ESG-01', 'rte-esg-1', 'bk-esg-1', 'veh-esg-1', 40.0, $2, 'completed')`,
		tenantID, startStr)
	require.NoError(t, err)

	// 1. GET /api/v1/esg/trips/{trip_id} - RBAC test without permission
	reqNoAuth := httptest.NewRequest(http.MethodGet, "/api/v1/esg/trips/trip-esg-1", nil)
	reqNoAuth.Header.Set("X-Test-User", "unknown-user")
	reqNoAuth.Header.Set("X-Test-Tenant", tenantID)
	recNoAuth := httptest.NewRecorder()
	r.ServeHTTP(recNoAuth, reqNoAuth)
	assert.Equal(t, http.StatusForbidden, recNoAuth.Code)

	// 2. GET /api/v1/esg/trips/{trip_id} - Allowed for viewer-user (esg:read)
	reqViewer := httptest.NewRequest(http.MethodGet, "/api/v1/esg/trips/trip-esg-1", nil)
	reqViewer.Header.Set("X-Test-User", "viewer-user")
	reqViewer.Header.Set("X-Test-Tenant", tenantID)
	recViewer := httptest.NewRecorder()
	r.ServeHTTP(recViewer, reqViewer)
	assert.Equal(t, http.StatusOK, recViewer.Code)

	var cert sustainability.TripCarbonCertificate
	err = json.NewDecoder(recViewer.Body).Decode(&cert)
	require.NoError(t, err)
	assert.Equal(t, "trip-esg-1", cert.TripID)
	assert.InDelta(t, 107.2, cert.CO2eKG, 0.1) // 40L * 2.68 = 107.2 kg
	assert.Equal(t, sustainability.MethodologyFuelPrimary, cert.Methodology)

	// 3. GET /api/v1/esg/trips/{trip_id} - 404 for non-existent trip
	req404 := httptest.NewRequest(http.MethodGet, "/api/v1/esg/trips/non-existent-trip", nil)
	req404.Header.Set("X-Test-User", "viewer-user")
	req404.Header.Set("X-Test-Tenant", tenantID)
	rec404 := httptest.NewRecorder()
	r.ServeHTTP(rec404, req404)
	assert.Equal(t, http.StatusNotFound, rec404.Code)

	// 4. POST /api/v1/esg/snapshots/generate - Forbidden for viewer-user (esg:write required)
	genPayload := `{
		"period_start": "` + now.Add(-30*24*time.Hour).Format("2006-01-02") + `",
		"period_end": "` + now.Add(24*time.Hour).Format("2006-01-02") + `"
	}`
	reqGenViewer := httptest.NewRequest(http.MethodPost, "/api/v1/esg/snapshots/generate", bytes.NewBufferString(genPayload))
	reqGenViewer.Header.Set("Content-Type", "application/json")
	reqGenViewer.Header.Set("X-Test-User", "viewer-user")
	reqGenViewer.Header.Set("X-Test-Tenant", tenantID)
	recGenViewer := httptest.NewRecorder()
	r.ServeHTTP(recGenViewer, reqGenViewer)
	assert.Equal(t, http.StatusForbidden, recGenViewer.Code)

	// 5. POST /api/v1/esg/snapshots/generate - Allowed for admin-user
	reqGenAdmin := httptest.NewRequest(http.MethodPost, "/api/v1/esg/snapshots/generate", bytes.NewBufferString(genPayload))
	reqGenAdmin.Header.Set("Content-Type", "application/json")
	reqGenAdmin.Header.Set("X-Test-User", "admin-user")
	reqGenAdmin.Header.Set("X-Test-Tenant", tenantID)
	recGenAdmin := httptest.NewRecorder()
	r.ServeHTTP(recGenAdmin, reqGenAdmin)
	assert.Equal(t, http.StatusCreated, recGenAdmin.Code)

	var snapshot sustainability.ESGEmissionSnapshot
	err = json.NewDecoder(recGenAdmin.Body).Decode(&snapshot)
	require.NoError(t, err)
	assert.NotEmpty(t, snapshot.ID)
	assert.Equal(t, 1, snapshot.TotalTrips)
	assert.InDelta(t, 150.0, snapshot.TotalDistanceKM, 0.01)
	assert.InDelta(t, 107.2, snapshot.TotalCO2eKG, 0.1)

	// 6. GET /api/v1/esg/snapshots - Allowed for viewer-user
	reqSnapshots := httptest.NewRequest(http.MethodGet, "/api/v1/esg/snapshots", nil)
	reqSnapshots.Header.Set("X-Test-User", "viewer-user")
	reqSnapshots.Header.Set("X-Test-Tenant", tenantID)
	recSnapshots := httptest.NewRecorder()
	r.ServeHTTP(recSnapshots, reqSnapshots)
	assert.Equal(t, http.StatusOK, recSnapshots.Code)

	var listSnaps []sustainability.ESGEmissionSnapshot
	err = json.NewDecoder(recSnapshots.Body).Decode(&listSnaps)
	require.NoError(t, err)
	require.Len(t, listSnaps, 1)
	assert.Equal(t, snapshot.ID, listSnaps[0].ID)

	// 7. GET /api/v1/esg/reports/brsr - JSON format
	reqBRSR := httptest.NewRequest(http.MethodGet, "/api/v1/esg/reports/brsr?year="+now.Format("2006"), nil)
	reqBRSR.Header.Set("X-Test-User", "viewer-user")
	reqBRSR.Header.Set("X-Test-Tenant", tenantID)
	recBRSR := httptest.NewRecorder()
	r.ServeHTTP(recBRSR, reqBRSR)
	assert.Equal(t, http.StatusOK, recBRSR.Code)

	var brsrReport sustainability.BRSRPrinciple6Report
	err = json.NewDecoder(recBRSR.Body).Decode(&brsrReport)
	require.NoError(t, err)
	assert.Equal(t, "FY "+now.Format("2006"), brsrReport.ReportingPeriod)
	assert.InDelta(t, 0.11, brsrReport.Scope1EmissionsTonne, 0.01)

	// 8. GET /api/v1/esg/reports/brsr?format=csv - CSV export
	reqCSV := httptest.NewRequest(http.MethodGet, "/api/v1/esg/reports/brsr?year="+now.Format("2006")+"&format=csv", nil)
	reqCSV.Header.Set("X-Test-User", "viewer-user")
	reqCSV.Header.Set("X-Test-Tenant", tenantID)
	recCSV := httptest.NewRecorder()
	r.ServeHTTP(recCSV, reqCSV)
	assert.Equal(t, http.StatusOK, recCSV.Code)
	assert.Equal(t, "text/csv", recCSV.Header().Get("Content-Type"))
	assert.Contains(t, recCSV.Body.String(), "Reporting Period,Scope 1 (Tonnes)")
	assert.Contains(t, recCSV.Body.String(), now.Format("2006"))
}
