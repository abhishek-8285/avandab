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

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/facility"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func setupFacilityTestApp(t *testing.T) (*App, *chi.Mux) {
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
			"authorized-user:facilities:read":  true,
			"authorized-user:facilities:write": true,
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
	facilityHandlers := &FacilityHandlers{App: app}
	app.Facilities = facilityHandlers

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

	facilityHandlers.RegisterAPIRoutes(r)
	return app, r
}

func TestFacilityAPI_CRUDAndTenantIsolation(t *testing.T) {
	app, r := setupFacilityTestApp(t)

	_, _ = app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-alpha', 'Alpha Corp', 'alpha')`)
	_, _ = app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-beta', 'Beta Corp', 'beta')`)

	// 1. Create Facility in tenant-alpha
	lat := 18.5204
	lon := 73.8567
	createPayload := facility.CreateFacilityInput{
		FacilityCode: "MM21000000757",
		Name:         "Pune Central Depot",
		FacilityType: facility.FacilityTypeDepot,
		Plant:        "PLANT-PUN",
		Circle:       "Maharashtra",
		ProfitCenter: "PC-500",
		CostCenter:   "CC-600",
		Address:      "Shivajinagar",
		City:         "Pune",
		State:        "Maharashtra",
		Pincode:      "411005",
		Latitude:     &lat,
		Longitude:    &lon,
	}
	body, _ := json.Marshal(createPayload)
	req := httptest.NewRequest("POST", "/api/v1/facilities", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "authorized-user")
	req.Header.Set("X-Test-Tenant", "tenant-alpha")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var createResp struct {
		Facility facility.Facility `json:"facility"`
	}
	err := json.NewDecoder(w.Body).Decode(&createResp)
	require.NoError(t, err)
	assert.Equal(t, "MM21000000757", createResp.Facility.FacilityCode)
	assert.Equal(t, "Pune Central Depot", createResp.Facility.Name)
	assert.Equal(t, facility.FacilityTypeDepot, createResp.Facility.FacilityType)
	facID := createResp.Facility.ID

	// 2. Duplicate FacilityCode in tenant-alpha should return 409 Conflict
	reqDup := httptest.NewRequest("POST", "/api/v1/facilities", bytes.NewReader(body))
	reqDup.Header.Set("Content-Type", "application/json")
	reqDup.Header.Set("X-Test-User", "authorized-user")
	reqDup.Header.Set("X-Test-Tenant", "tenant-alpha")
	wDup := httptest.NewRecorder()
	r.ServeHTTP(wDup, reqDup)
	assert.Equal(t, http.StatusConflict, wDup.Code)

	// 3. Get Facility by ID (tenant-alpha)
	reqGet := httptest.NewRequest("GET", "/api/v1/facilities/"+facID, nil)
	reqGet.Header.Set("X-Test-User", "authorized-user")
	reqGet.Header.Set("X-Test-Tenant", "tenant-alpha")
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, reqGet)

	assert.Equal(t, http.StatusOK, wGet.Code)
	var getResp struct {
		Facility facility.Facility `json:"facility"`
	}
	err = json.NewDecoder(wGet.Body).Decode(&getResp)
	require.NoError(t, err)
	assert.Equal(t, "Pune Central Depot", getResp.Facility.Name)

	// 4. Get Facility by Code (fallback in route)
	reqGetCode := httptest.NewRequest("GET", "/api/v1/facilities/MM21000000757", nil)
	reqGetCode.Header.Set("X-Test-User", "authorized-user")
	reqGetCode.Header.Set("X-Test-Tenant", "tenant-alpha")
	wGetCode := httptest.NewRecorder()
	r.ServeHTTP(wGetCode, reqGetCode)
	assert.Equal(t, http.StatusOK, wGetCode.Code)

	// 5. Cross-tenant Isolation: tenant-beta cannot see or get alpha's facility
	reqBetaGet := httptest.NewRequest("GET", "/api/v1/facilities/"+facID, nil)
	reqBetaGet.Header.Set("X-Test-User", "authorized-user")
	reqBetaGet.Header.Set("X-Test-Tenant", "tenant-beta")
	wBetaGet := httptest.NewRecorder()
	r.ServeHTTP(wBetaGet, reqBetaGet)
	assert.Equal(t, http.StatusNotFound, wBetaGet.Code)

	// 6. Update Facility (tenant-alpha)
	updatePayload := facility.UpdateFacilityInput{
		Name:         "Pune Main Logistics Hub",
		FacilityType: facility.FacilityTypeHub,
		Plant:        "PLANT-PUN-02",
		Circle:       "Maharashtra West",
		City:         "Pune",
	}
	upBody, _ := json.Marshal(updatePayload)
	reqUp := httptest.NewRequest("PUT", "/api/v1/facilities/"+facID, bytes.NewReader(upBody))
	reqUp.Header.Set("Content-Type", "application/json")
	reqUp.Header.Set("X-Test-User", "authorized-user")
	reqUp.Header.Set("X-Test-Tenant", "tenant-alpha")
	wUp := httptest.NewRecorder()
	r.ServeHTTP(wUp, reqUp)

	assert.Equal(t, http.StatusOK, wUp.Code)
	var upResp struct {
		Facility facility.Facility `json:"facility"`
	}
	err = json.NewDecoder(wUp.Body).Decode(&upResp)
	require.NoError(t, err)
	assert.Equal(t, "Pune Main Logistics Hub", upResp.Facility.Name)
	assert.Equal(t, facility.FacilityTypeHub, upResp.Facility.FacilityType)

	// 7. List Facilities
	reqList := httptest.NewRequest("GET", "/api/v1/facilities?q=Pune&type=hub", nil)
	reqList.Header.Set("X-Test-User", "authorized-user")
	reqList.Header.Set("X-Test-Tenant", "tenant-alpha")
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqList)

	assert.Equal(t, http.StatusOK, wList.Code)
	var listResp struct {
		Facilities []facility.Facility `json:"facilities"`
		Total      int64               `json:"total"`
	}
	err = json.NewDecoder(wList.Body).Decode(&listResp)
	require.NoError(t, err)
	assert.Equal(t, int64(1), listResp.Total)
	require.Len(t, listResp.Facilities, 1)
	assert.Equal(t, "Pune Main Logistics Hub", listResp.Facilities[0].Name)

	// 8. Delete Facility
	reqDel := httptest.NewRequest("DELETE", "/api/v1/facilities/"+facID, nil)
	reqDel.Header.Set("X-Test-User", "authorized-user")
	reqDel.Header.Set("X-Test-Tenant", "tenant-alpha")
	wDel := httptest.NewRecorder()
	r.ServeHTTP(wDel, reqDel)
	assert.Equal(t, http.StatusOK, wDel.Code)

	// After deletion, Get should return 404
	reqGetAfterDel := httptest.NewRequest("GET", "/api/v1/facilities/"+facID, nil)
	reqGetAfterDel.Header.Set("X-Test-User", "authorized-user")
	reqGetAfterDel.Header.Set("X-Test-Tenant", "tenant-alpha")
	wGetAfterDel := httptest.NewRecorder()
	r.ServeHTTP(wGetAfterDel, reqGetAfterDel)
	assert.Equal(t, http.StatusNotFound, wGetAfterDel.Code)
}

func TestFacilityAPI_RBACForbidden(t *testing.T) {
	_, r := setupFacilityTestApp(t)

	endpoints := []struct {
		method string
		url    string
	}{
		{"GET", "/api/v1/facilities"},
		{"POST", "/api/v1/facilities"},
		{"GET", "/api/v1/facilities/fac-123"},
		{"PUT", "/api/v1/facilities/fac-123"},
		{"DELETE", "/api/v1/facilities/fac-123"},
	}

	for _, ep := range endpoints {
		t.Run("RBAC_Forbidden_"+ep.method+"_"+ep.url, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.url, bytes.NewReader([]byte("{}")))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Test-User", "unauthorized-user")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}
}
