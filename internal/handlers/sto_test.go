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
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	"transport-app/internal/sto"
)

func setupSTOTestApp(t *testing.T) (*App, *chi.Mux) {
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
			"authorized-user:sto:read":        true,
			"authorized-user:sto:write":       true,
			"authorized-user:loadboard:read":  true,
			"authorized-user:loadboard:write": true,
			"viewer-user:sto:read":            true,
			"viewer-user:sto:write":           false,
			"viewer-user:loadboard:read":      true,
			"viewer-user:loadboard:write":     false,
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
	stoRepo := sto.NewSQLRepository(db)
	stoHandlers := &STOHandlers{App: app, STOSvc: sto.NewService(stoRepo, db)}
	app.STO = stoHandlers

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
					Role:   "dispatcher",
				})
			}
			req = req.WithContext(ctx)
			next.ServeHTTP(w, req)
		})
	})

	stoHandlers.RegisterAPIRoutes(r)
	return app, r
}

func TestSTOAndLoadBoardAPI(t *testing.T) {
	app, r := setupSTOTestApp(t)

	tenantID := "tenant-sto-api"
	_, err := app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-sto-api', 'STO Corp', 'sto-api')`)
	require.NoError(t, err)

	fac1 := "fac-hnd-01"
	fac2 := "fac-hnd-02"
	_, err = app.DB.Exec(`
		INSERT INTO facilities (id, tenant_id, facility_code, name, facility_type, city)
		VALUES 
		($1, $2, 'PLANT-DEL', 'Delhi Plant', 'depot', 'Delhi'),
		($3, $2, 'RDC-JAIPUR', 'Jaipur Hub', 'hub', 'Jaipur')`,
		fac1, tenantID, fac2)
	require.NoError(t, err)

	// 1. POST /api/v1/sto — Create STO
	createBody := `{"origin_facility_id": "` + fac1 + `", "destination_facility_id": "` + fac2 + `", "material_code": "STEEL-COIL", "material_description": "CR Steel Coils", "quantity": 30, "uom": "MT", "required_delivery_date": "2026-09-30"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sto", bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "authorized-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	var stoCreated sto.StockTransferOrder
	err = json.NewDecoder(rec.Body).Decode(&stoCreated)
	require.NoError(t, err)
	assert.NotEmpty(t, stoCreated.ID)
	assert.Equal(t, sto.StatusDRAFT, stoCreated.Status)

	// 2. GET /api/v1/sto/{id}
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/sto/"+stoCreated.ID, nil)
	reqGet.Header.Set("X-Test-User", "authorized-user")
	reqGet.Header.Set("X-Test-Tenant", tenantID)
	recGet := httptest.NewRecorder()
	r.ServeHTTP(recGet, reqGet)
	assert.Equal(t, http.StatusOK, recGet.Code)

	// 3. POST /api/v1/sto/{id}/release
	reqRel := httptest.NewRequest(http.MethodPost, "/api/v1/sto/"+stoCreated.ID+"/release", nil)
	reqRel.Header.Set("X-Test-User", "authorized-user")
	reqRel.Header.Set("X-Test-Tenant", tenantID)
	recRel := httptest.NewRecorder()
	r.ServeHTTP(recRel, reqRel)
	assert.Equal(t, http.StatusOK, recRel.Code)

	// 4. POST /api/v1/sto/{id}/post-to-loadboard
	postBody := `{"vehicle_type_required": "truck", "target_rate": 35000, "max_rate": 40000, "visibility": "PRIVATE", "expires_hours": 48}`
	reqPostLB := httptest.NewRequest(http.MethodPost, "/api/v1/sto/"+stoCreated.ID+"/post-to-loadboard", bytes.NewBufferString(postBody))
	reqPostLB.Header.Set("Content-Type", "application/json")
	reqPostLB.Header.Set("X-Test-User", "authorized-user")
	reqPostLB.Header.Set("X-Test-Tenant", tenantID)
	recPostLB := httptest.NewRecorder()
	r.ServeHTTP(recPostLB, reqPostLB)
	assert.Equal(t, http.StatusCreated, recPostLB.Code)

	var lbListing sto.LoadBoardListing
	err = json.NewDecoder(recPostLB.Body).Decode(&lbListing)
	require.NoError(t, err)
	assert.NotEmpty(t, lbListing.ID)
	assert.Equal(t, "Delhi", lbListing.OriginCity)
	assert.Equal(t, "Jaipur", lbListing.DestinationCity)

	// 5. GET /api/v1/loadboard/listings
	reqListLB := httptest.NewRequest(http.MethodGet, "/api/v1/loadboard/listings?status=OPEN", nil)
	reqListLB.Header.Set("X-Test-User", "authorized-user")
	reqListLB.Header.Set("X-Test-Tenant", tenantID)
	recListLB := httptest.NewRecorder()
	r.ServeHTTP(recListLB, reqListLB)
	assert.Equal(t, http.StatusOK, recListLB.Code)

	// 6. POST /api/v1/loadboard/listings/{id}/bids
	bidBody := `{"carrier_id": "carr-delhi", "carrier_name": "Delhi Super Express", "bid_amount": 36000, "remarks": "Prompt delivery"}`
	reqBid := httptest.NewRequest(http.MethodPost, "/api/v1/loadboard/listings/"+lbListing.ID+"/bids", bytes.NewBufferString(bidBody))
	reqBid.Header.Set("Content-Type", "application/json")
	reqBid.Header.Set("X-Test-User", "authorized-user")
	reqBid.Header.Set("X-Test-Tenant", tenantID)
	recBid := httptest.NewRecorder()
	r.ServeHTTP(recBid, reqBid)
	assert.Equal(t, http.StatusCreated, recBid.Code)

	var bidResp sto.LoadBoardBid
	err = json.NewDecoder(recBid.Body).Decode(&bidResp)
	require.NoError(t, err)
	assert.NotEmpty(t, bidResp.ID)

	// 7. POST /api/v1/loadboard/listings/{id}/award
	awardBody := `{"bid_id": "` + bidResp.ID + `"}`
	reqAward := httptest.NewRequest(http.MethodPost, "/api/v1/loadboard/listings/"+lbListing.ID+"/award", bytes.NewBufferString(awardBody))
	reqAward.Header.Set("Content-Type", "application/json")
	reqAward.Header.Set("X-Test-User", "authorized-user")
	reqAward.Header.Set("X-Test-Tenant", tenantID)
	recAward := httptest.NewRecorder()
	r.ServeHTTP(recAward, reqAward)
	assert.Equal(t, http.StatusOK, recAward.Code)

	// 8. RBAC: viewer-user cannot create STO or award bids -> 403
	reqViewerSTO := httptest.NewRequest(http.MethodPost, "/api/v1/sto", bytes.NewBufferString(createBody))
	reqViewerSTO.Header.Set("Content-Type", "application/json")
	reqViewerSTO.Header.Set("X-Test-User", "viewer-user")
	reqViewerSTO.Header.Set("X-Test-Tenant", tenantID)
	recViewerSTO := httptest.NewRecorder()
	r.ServeHTTP(recViewerSTO, reqViewerSTO)
	assert.Equal(t, http.StatusForbidden, recViewerSTO.Code)

	reqViewerAward := httptest.NewRequest(http.MethodPost, "/api/v1/loadboard/listings/"+lbListing.ID+"/award", bytes.NewBufferString(awardBody))
	reqViewerAward.Header.Set("Content-Type", "application/json")
	reqViewerAward.Header.Set("X-Test-User", "viewer-user")
	reqViewerAward.Header.Set("X-Test-Tenant", tenantID)
	recViewerAward := httptest.NewRecorder()
	r.ServeHTTP(recViewerAward, reqViewerAward)
	assert.Equal(t, http.StatusForbidden, recViewerAward.Code)
}
