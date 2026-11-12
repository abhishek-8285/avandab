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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/coldchain"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func setupColdChainTestApp(t *testing.T) (*App, *chi.Mux) {
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
			"admin-user:coldchain:read":   true,
			"admin-user:coldchain:write":  true,
			"viewer-user:coldchain:read":  true,
			"viewer-user:coldchain:write": false,
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

	app.ColdChain = NewColdChainHandlers(app)

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

	app.ColdChain.RegisterAPIRoutes(r)
	r.Route("/cold-chain", app.ColdChain.Routes)
	return app, r
}

func TestColdChainAPI_FullLifecycleAndRBAC(t *testing.T) {
	app, r := setupColdChainTestApp(t)

	tenantID := "tenant-cc-test"
	_, err := app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ($1, 'ColdChain Tenant', 'coldchain-tenant')`, tenantID)
	require.NoError(t, err)

	vehicleID := "reefer-truck-101"
	_, err = app.DB.Exec(`INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, current_mileage) VALUES
		($1, $2, 'KA-01-CC-9999', 'REF-9999', 'truck', 15.0, 'diesel', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 25000.0)`,
		vehicleID, tenantID)
	require.NoError(t, err)

	// 1. RBAC check: viewer cannot configure reefer profile (403 Forbidden)
	profPayload := coldchain.ConfigureReeferProfileInput{
		ReeferMakeModel:  "Thermo King Super II",
		NumZones:         1,
		Zone1Type:        coldchain.ZoneTypeChilled,
		Zone1TargetTempC: 4.0,
		Zone1MinTempC:    2.0,
		Zone1MaxTempC:    8.0,
		DefrostMode:      "AUTO",
	}
	body, _ := json.Marshal(profPayload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cold-chain/vehicles/"+vehicleID+"/profile", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// 2. Admin successfully configures reefer profile (200 OK)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cold-chain/vehicles/"+vehicleID+"/profile", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var createdProf coldchain.ReeferProfile
	err = json.Unmarshal(rec.Body.Bytes(), &createdProf)
	require.NoError(t, err)
	assert.Equal(t, "Thermo King Super II", createdProf.ReeferMakeModel)
	assert.Equal(t, 4.0, createdProf.Zone1TargetTempC)

	// 3. Admin ingests compliant telemetry (4.5°C, COOLING, door CLOSED)
	telemPayload := coldchain.IngestReeferTelemetryInput{
		VehicleID:          vehicleID,
		ReeferPowerStatus:  coldchain.PowerStatusOn,
		ReeferMode:         coldchain.ModeCooling,
		Zone1TempC:         4.5,
		Zone1SetpointC:     4.0,
		DoorStatus:         coldchain.DoorClosed,
		ReeferFuelLevelPct: 90.0,
		ReeferBatteryV:     12.6,
	}
	body, _ = json.Marshal(telemPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cold-chain/telemetry", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 4. Ingest out-of-spec telemetry (12.5°C > max safe 8.0°C) -> triggers OVER_TEMP excursion
	overTempPayload := coldchain.IngestReeferTelemetryInput{
		VehicleID:          vehicleID,
		ReeferPowerStatus:  coldchain.PowerStatusOn,
		ReeferMode:         coldchain.ModeCooling,
		Zone1TempC:         12.5,
		Zone1SetpointC:     4.0,
		DoorStatus:         coldchain.DoorClosed,
		ReeferFuelLevelPct: 88.0,
		ReeferBatteryV:     12.5,
	}
	body, _ = json.Marshal(overTempPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cold-chain/telemetry", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 5. Verify excursion was logged
	req = httptest.NewRequest(http.MethodGet, "/api/v1/cold-chain/excursions?vehicle_id="+vehicleID, nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var excResp struct {
		Excursions []coldchain.TemperatureExcursion `json:"excursions"`
		Total      int                              `json:"total"`
	}
	err = json.Unmarshal(rec.Body.Bytes(), &excResp)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, excResp.Total, 1)
	excursionID := excResp.Excursions[0].ID
	assert.Equal(t, coldchain.ExcursionOverTemp, excResp.Excursions[0].ExcursionType)
	assert.Equal(t, coldchain.SeverityCritical, excResp.Excursions[0].Severity)
	assert.Equal(t, coldchain.ExcursionOpen, excResp.Excursions[0].Status)

	// 6. Acknowledge excursion
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cold-chain/excursions/"+excursionID+"/acknowledge", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var ackExc coldchain.TemperatureExcursion
	err = json.Unmarshal(rec.Body.Bytes(), &ackExc)
	require.NoError(t, err)
	assert.Equal(t, coldchain.ExcursionAcknowledged, ackExc.Status)

	// 7. Resolve excursion via API
	resPayload := coldchain.ResolveExcursionInput{
		ResolutionNote: "Thermostat re-calibrated and cooling coil checked",
		ResolvedBy:     "Senior Tech",
	}
	body, _ = json.Marshal(resPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cold-chain/excursions/"+excursionID+"/resolve", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 8. Test Certify Trip Compliance API
	routeID := "route-cc-1"
	_, err = app.DB.Exec(`INSERT OR IGNORE INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES
		($1, $2, 'Bangalore DC', 'Chennai Port', 350.0, 7.0, 15000.0)`, routeID, tenantID)
	require.NoError(t, err)

	tripID := "trip-cc-999"
	_, err = app.DB.Exec(`INSERT OR IGNORE INTO trips (id, tenant_id, trip_number, vehicle_id, route_id, status, departure_time) VALUES
		($1, $2, 'TRIP-CC-999', $3, $4, 'completed', datetime('now', '-5 hours'))`,
		tripID, tenantID, vehicleID, routeID)
	require.NoError(t, err)

	certPayload := struct {
		VehicleID                 string    `json:"vehicle_id"`
		CertifiedBy               string    `json:"certified_by"`
		Readings                  []float64 `json:"readings"`
		TotalExcursionDurationSec int       `json:"total_excursion_duration_sec"`
		DoorOpenCount             int       `json:"door_open_count"`
		StartTime                 time.Time `json:"start_time"`
		EndTime                   time.Time `json:"end_time"`
	}{
		VehicleID:                 vehicleID,
		CertifiedBy:               "Quality Officer",
		Readings:                  []float64{4.0, 4.2, 4.1, 4.3, 4.0},
		TotalExcursionDurationSec: 0,
		DoorOpenCount:             1,
		StartTime:                 time.Now().Add(-5 * time.Hour),
		EndTime:                   time.Now(),
	}
	body, _ = json.Marshal(certPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cold-chain/trips/"+tripID+"/certify", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var certSummary coldchain.ReeferTripSummary
	err = json.Unmarshal(rec.Body.Bytes(), &certSummary)
	require.NoError(t, err)
	assert.Equal(t, coldchain.ComplianceCompliant, certSummary.ComplianceStatus)
	assert.InDelta(t, 4.12, certSummary.Zone1MeanKineticTempC, 0.2)

	// 9. Get Trip Summary API
	req = httptest.NewRequest(http.MethodGet, "/api/v1/cold-chain/trips/"+tripID+"/summary", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 10. Get Fleet Metrics API
	req = httptest.NewRequest(http.MethodGet, "/api/v1/cold-chain/metrics", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var metrics coldchain.ColdChainFleetMetrics
	err = json.Unmarshal(rec.Body.Bytes(), &metrics)
	require.NoError(t, err)
	assert.Equal(t, 1, metrics.TotalReefers)

	// 11. Test Web Dashboard UI rendering
	req = httptest.NewRequest(http.MethodGet, "/cold-chain", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Cold Chain Telematics")
	assert.Contains(t, rec.Body.String(), "REF-9999")

	// 12. Test Web Configure Profile Form POST
	form := url.Values{}
	form.Set("vehicle_id", vehicleID)
	form.Set("reefer_make_model", "Carrier Transicold X4")
	form.Set("num_zones", "2")
	form.Set("zone1_type", "FROZEN")
	form.Set("zone1_target_temp_c", "-20.0")
	form.Set("zone1_min_temp_c", "-25.0")
	form.Set("zone1_max_temp_c", "-18.0")
	form.Set("defrost_mode", "CYCLE")

	req = httptest.NewRequest(http.MethodPost, "/cold-chain/profiles", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/cold-chain", rec.Header().Get("Location"))

	// 13. Test Web Resolve Excursion Form POST
	// Create another excursion to resolve via Web
	openExcPayload := coldchain.IngestReeferTelemetryInput{
		VehicleID:           vehicleID,
		ReeferPowerStatus:   coldchain.PowerStatusOn,
		ReeferMode:          coldchain.ModeCooling,
		Zone1TempC:          0.0, // Frozen zone target is -20, so 0 is critical over-temp
		Zone1SetpointC:      -20.0,
		DoorStatus:          coldchain.DoorOpen,
		DoorOpenDurationSec: 400,
	}
	body, _ = json.Marshal(openExcPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cold-chain/telemetry", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/cold-chain/excursions?vehicle_id="+vehicleID+"&status=OPEN", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var openExcResp struct {
		Excursions []coldchain.TemperatureExcursion `json:"excursions"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &openExcResp)
	require.NotEmpty(t, openExcResp.Excursions)
	newExcID := openExcResp.Excursions[0].ID

	resForm := url.Values{}
	resForm.Set("resolution_note", "Cargo door closed; auxiliary generator engaged.")
	req = httptest.NewRequest(http.MethodPost, "/cold-chain/excursions/"+newExcID+"/resolve", strings.NewReader(resForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/cold-chain", rec.Header().Get("Location"))
}
