package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/ev"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func setupEVTestApp(t *testing.T) (*App, *chi.Mux) {
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
			"admin-user:ev:read":   true,
			"admin-user:ev:write":  true,
			"viewer-user:ev:read":  true,
			"viewer-user:ev:write": false,
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

	app.EV = NewEVHandlers(app)

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

	app.EV.RegisterAPIRoutes(r)
	r.Route("/ev", app.EV.Routes)
	return app, r
}

func TestEVAPI_FullLifecycleAndRBAC(t *testing.T) {
	app, r := setupEVTestApp(t)

	tenantID := "tenant-ev-test"
	_, err := app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ($1, 'EV Tenant', 'ev-tenant')`, tenantID)
	require.NoError(t, err)

	vehicleID := "ev-truck-101"
	_, err = app.DB.Exec(`INSERT OR IGNORE INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, current_mileage) VALUES
		($1, $2, 'KA-01-EV-8888', 'EV-8888', 'truck', 20.0, 'electric', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 10000.0)`,
		vehicleID, tenantID)
	require.NoError(t, err)

	// 1. RBAC check: viewer cannot configure profile
	profPayload := ev.ConfigureEVProfileInput{
		BatteryCapacityKWh:    80.0,
		MaxRangeKM:            320.0,
		ChargingStandard:      "CCS2",
		MinSafeSoC:            15.0,
		NominalEfficiencyWhKM: 180.0,
	}
	body, _ := json.Marshal(profPayload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ev/vehicles/"+vehicleID+"/profile", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// 2. Admin successfully configures EV profile
	req = httptest.NewRequest(http.MethodPost, "/api/v1/ev/vehicles/"+vehicleID+"/profile", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var createdProf ev.EVProfile
	err = json.Unmarshal(rec.Body.Bytes(), &createdProf)
	require.NoError(t, err)
	assert.Equal(t, 80.0, createdProf.BatteryCapacityKWh)
	assert.Equal(t, 320.0, createdProf.MaxRangeKM)

	// 3. Admin ingests fast charging telemetry (SoC 25%, 60 kW)
	telemPayload := ev.IngestEVTelemetryInput{
		VehicleID:       vehicleID,
		SoCPercent:      25.0,
		SoHPercent:      99.0,
		ChargingStatus:  ev.ChargingStatusFastCharging,
		ChargingPowerKW: 60.0,
	}
	body, _ = json.Marshal(telemPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/ev/telemetry", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 4. Verify charging sessions has active session
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ev/charging-sessions?vehicle_id="+vehicleID, nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var sessResp struct {
		Sessions []ev.ChargingSession `json:"sessions"`
		Total    int                  `json:"total"`
	}
	err = json.Unmarshal(rec.Body.Bytes(), &sessResp)
	require.NoError(t, err)
	assert.Equal(t, 1, sessResp.Total)
	assert.Equal(t, ev.SessionStatusInProgress, sessResp.Sessions[0].Status)

	// 5. Ingest completion telemetry (SoC 85%, DISCHARGING)
	telemDone := ev.IngestEVTelemetryInput{
		VehicleID:      vehicleID,
		SoCPercent:     85.0,
		SoHPercent:     99.0,
		ChargingStatus: ev.ChargingStatusDischarging,
	}
	body, _ = json.Marshal(telemDone)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/ev/telemetry", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 6. Record green energy summary
	summaryIn := ev.RecordEnergySummaryInput{
		VehicleID:         vehicleID,
		DistanceKM:        150.0,
		EnergyConsumedKWh: 27.0,
	}
	body, _ = json.Marshal(summaryIn)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/ev/energy-summary", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)

	// 7. Get fleet metrics
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ev/metrics", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var metrics ev.EVFleetMetrics
	err = json.Unmarshal(rec.Body.Bytes(), &metrics)
	require.NoError(t, err)
	assert.Equal(t, 1, metrics.TotalEVs)
	assert.Equal(t, 150.0, metrics.TotalGreenDistanceKM)
	assert.Equal(t, 82.5, metrics.TotalCO2AvoidedKG) // 150 km * 0.55 = 82.5 kg

	// 8. Test Web Dashboard UI rendering
	req = httptest.NewRequest(http.MethodGet, "/ev", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "EV Telematics &amp; Fleet Battery")
	assert.Contains(t, rec.Body.String(), "EV-8888")

	// 9. Test Web Configure Profile Form POST
	form := url.Values{}
	form.Set("vehicle_id", vehicleID)
	form.Set("battery_capacity_kwh", "90.0")
	form.Set("max_range_km", "350.0")
	form.Set("charging_standard", "CCS2")
	form.Set("min_safe_soc", "20")
	form.Set("nominal_efficiency_wh_km", "190")

	req = httptest.NewRequest(http.MethodPost, "/ev/profiles", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/ev", rec.Header().Get("Location"))
}
