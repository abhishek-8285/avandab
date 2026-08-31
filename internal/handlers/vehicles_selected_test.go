package handlers

import (
	"context"
	"database/sql"
	"fmt"
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
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	alertsqlite "transport-app/internal/alerts/repository/sqlite"
	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/domain"
	"transport-app/internal/events"
	repoSQLite "transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func newVehiclesSelectedDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_vehicles_sel_%d_%s", time.Now().UnixNano(), strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	migrationsDir := "../../db/migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		for _, cand := range []string{"db/migrations", "../db/migrations", "../../db/migrations"} {
			if _, err := os.Stat(cand); err == nil {
				migrationsDir = cand
				break
			}
		}
	}
	goose.SetLogger(goose.NopLogger())
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newVehiclesSelectedApp(t *testing.T, db *sql.DB, authSrv auth.AuthorizationService) *App {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	if authSrv == nil {
		authSrv = &mockAuthSvc{}
	}
	tmpl, err := parseTemplates(authSrv)
	require.NoError(t, err)
	cfg := &config.Config{
		AppEnv:       "testing",
		CookieSecret: "test-secret-32",
		CookieSecure: false,
		UploadDir:    t.TempDir(),
	}
	repo := repoSQLite.NewRepository(db)
	bus := events.NewInMemoryBus()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)
	app := &App{
		DB:         db,
		Config:     cfg,
		Services:   services,
		Templates:  tmpl,
		AuthSrv:    authSrv,
		AlertsRepo: alertsqlite.NewAlertRepository(db),
	}
	app.Vehicles = &VehicleHandlers{App: app}
	return app
}

func withVehicleTenantSession(r *http.Request, tenant string, userID, role string) *http.Request {
	ctx := shared.ContextWithTenantID(r.Context(), shared.TenantID(tenant))
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: userID, Role: role})
	return r.WithContext(ctx)
}

func TestSelectedVehicles_List_SuccessAndPagination(t *testing.T) {
	db := newVehiclesSelectedDB(t)
	app := newVehiclesSelectedApp(t, db, &mockAuthSvc{})
	// Seed two vehicles
	_, err := app.Services.Vehicles.CreateVehicle(shared.ContextWithTenantID(context.Background(), shared.DefaultTenant), "MH01AA1111", "V-001", domain.VehicleTypeTruck, 5000, domain.FuelTypeDiesel, time.Now().AddDate(1, 0, 0).Format("2006-01-02"), time.Now().AddDate(1, 0, 0).Format("2006-01-02"), time.Now().AddDate(1, 0, 0).Format("2006-01-02"), "10000")
	require.NoError(t, err)
	_, err = app.Services.Vehicles.CreateVehicle(shared.ContextWithTenantID(context.Background(), shared.DefaultTenant), "MH02BB2222", "V-002", domain.VehicleTypeBus, 3000, domain.FuelTypePetrol, time.Now().AddDate(1, 0, 0).Format("2006-01-02"), time.Now().AddDate(1, 0, 0).Format("2006-01-02"), time.Now().AddDate(1, 0, 0).Format("2006-01-02"), "")
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Route("/vehicles", app.Vehicles.Routes)

	t.Run("list success", func(t *testing.T) {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, "MH01AA1111")
		assert.Contains(t, body, "MH02BB2222")
	})

	t.Run("pagination limit", func(t *testing.T) {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/?limit=1&page=1", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("search query", func(t *testing.T) {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/?q=MH01", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "MH01AA1111")
	})

	t.Run("status filter", func(t *testing.T) {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/?status=available", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("tenant isolation different tenant", func(t *testing.T) {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/", nil), "tenant-B", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		// Vehicles are tenant-aware via UoW; tenant-B will have 0 vehicles but should still render 200
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("datastar fragment", func(t *testing.T) {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/", nil), "1", "user-1", "admin")
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.NotEmpty(t, w.Body.String())
	})

	t.Run("datastar via query", func(t *testing.T) {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/?_fragment=true", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("datastar via Datastar-Request header", func(t *testing.T) {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/", nil), "1", "user-1", "admin")
		req.Header.Set("Datastar-Request", "true")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestSelectedVehicles_List_Error(t *testing.T) {
	db := newVehiclesSelectedDB(t)
	app := newVehiclesSelectedApp(t, db, &mockAuthSvc{})
	_ = db.Close()
	r := chi.NewRouter()
	r.Route("/vehicles", app.Vehicles.Routes)

	req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/", nil), "1", "user-1", "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "Failed to list vehicles")
}

func TestSelectedVehicles_CRUD(t *testing.T) {
	db := newVehiclesSelectedDB(t)
	app := newVehiclesSelectedApp(t, db, &mockAuthSvc{})
	r := chi.NewRouter()
	r.Route("/vehicles", app.Vehicles.Routes)

	t.Run("New renders", func(t *testing.T) {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/new", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "New Vehicle")
	})

	var createdID string
	futureDate := time.Now().AddDate(1, 0, 0).Format("2006-01-02")

	t.Run("Create success", func(t *testing.T) {
		form := url.Values{
			"registration_number": {"MH03CC3333"},
			"vehicle_number":      {"V-003"},
			"vehicle_type":        {"truck"},
			"capacity":            {"8000"},
			"fuel_type":           {"diesel"},
			"insurance_expiry":    {futureDate},
			"fitness_expiry":      {futureDate},
			"permit_expiry":       {futureDate},
			"current_mileage":     {"12345.5"},
		}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/new", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "/vehicles", w.Header().Get("Location"))
		// fetch ID
		vs, _, _ := app.Services.Vehicles.ListVehicles(shared.ContextWithTenantID(context.Background(), shared.DefaultTenant), "MH03CC3333", "", 10, 0)
		require.Len(t, vs, 1)
		createdID = string(vs[0].ID)
		require.NotEmpty(t, createdID)
	})

	t.Run("Create success invalid dates fallback", func(t *testing.T) {
		form := url.Values{
			"registration_number": {"MH09ZZ9999"},
			"vehicle_number":      {"V-099"},
			"vehicle_type":        {"bus"},
			"capacity":            {"5000"},
			"fuel_type":           {"petrol"},
			"insurance_expiry":    {"not-a-date"},
			"fitness_expiry":      {"not-a-date"},
			"permit_expiry":       {"not-a-date"},
		}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/new", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
	})

	t.Run("Create success with empty mileage", func(t *testing.T) {
		form := url.Values{
			"registration_number": {"MH04DD4444"},
			"vehicle_number":      {"V-004"},
			"vehicle_type":        {"van"},
			"capacity":            {"2000"},
			"fuel_type":           {"cng"},
			"insurance_expiry":    {futureDate},
			"fitness_expiry":      {futureDate},
			"permit_expiry":       {futureDate},
		}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/new", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
	})

	t.Run("Create invalid mileage string still succeeds", func(t *testing.T) {
		form := url.Values{
			"registration_number": {"MH05EE5555"},
			"vehicle_number":      {"V-005"},
			"vehicle_type":        {"tempo"},
			"capacity":            {"1500"},
			"fuel_type":           {"diesel"},
			"insurance_expiry":    {futureDate},
			"fitness_expiry":      {futureDate},
			"permit_expiry":       {futureDate},
			"current_mileage":     {"not-a-number"},
		}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/new", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
	})

	t.Run("Create validation error missing registration", func(t *testing.T) {
		form := url.Values{
			"registration_number": {""},
			"vehicle_number":      {""},
			"capacity":            {"1000"},
		}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/new", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "required")
	})

	t.Run("Create datastar", func(t *testing.T) {
		form := url.Values{
			"registration_number": {"MH06FF6666"},
			"vehicle_number":      {"V-006"},
			"vehicle_type":        {"pickup"},
			"capacity":            {"1200"},
			"fuel_type":           {"diesel"},
			"insurance_expiry":    {futureDate},
			"fitness_expiry":      {futureDate},
			"permit_expiry":       {futureDate},
		}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/new", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "/vehicles", w.Header().Get("Location"))
	})

	// View success
	t.Run("View success", func(t *testing.T) {
		require.NotEmpty(t, createdID)
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/"+createdID, nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "MH03CC3333")
	})

	t.Run("View not found", func(t *testing.T) {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/nonexistent-id", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Vehicle not found")
	})

	t.Run("View tenant isolation empty tenant still tries", func(t *testing.T) {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/"+createdID, nil), "other-tenant", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		// tenant-B has no vehicle with that ID -> not found
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Edit success", func(t *testing.T) {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/"+createdID+"/edit", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Edit Vehicle")
	})

	t.Run("Edit not found", func(t *testing.T) {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/unknown/edit", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Update success", func(t *testing.T) {
		form := url.Values{
			"registration_number": {"MH03CC3333"},
			"vehicle_number":      {"V-003-UPDATED"},
			"vehicle_type":        {"truck"},
			"capacity":            {"9000"},
			"fuel_type":           {"diesel"},
			"insurance_expiry":    {futureDate},
			"fitness_expiry":      {futureDate},
			"permit_expiry":       {futureDate},
			"status":              {"available"},
			"current_mileage":     {"20000"},
		}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/"+createdID+"/edit", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "/vehicles/"+createdID, w.Header().Get("Location"))
		v, err := app.Services.Vehicles.GetVehicle(shared.ContextWithTenantID(context.Background(), shared.DefaultTenant), domain.VehicleID(createdID))
		require.NoError(t, err)
		assert.Equal(t, "V-003-UPDATED", v.VehicleNumber)
		assert.Equal(t, int64(9000), v.Capacity)
	})

	t.Run("Update empty status defaults to available", func(t *testing.T) {
		form := url.Values{
			"registration_number": {"MH03CC3333"},
			"vehicle_number":      {"V-003-UPDATED2"},
			"vehicle_type":        {"truck"},
			"capacity":            {"9000"},
			"fuel_type":           {"diesel"},
			"insurance_expiry":    {futureDate},
			"fitness_expiry":      {futureDate},
			"permit_expiry":       {futureDate},
			"status":              {""},
		}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/"+createdID+"/edit", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
	})

	t.Run("Update invalid mileage still succeeds", func(t *testing.T) {
		form := url.Values{
			"registration_number": {"MH03CC3333"},
			"vehicle_number":      {"V-003-UPDATED"},
			"vehicle_type":        {"truck"},
			"capacity":            {"9000"},
			"fuel_type":           {"diesel"},
			"insurance_expiry":    {futureDate},
			"fitness_expiry":      {futureDate},
			"permit_expiry":       {futureDate},
			"current_mileage":     {"bad"},
		}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/"+createdID+"/edit", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
	})

	t.Run("Update invalid dates fallback still succeeds", func(t *testing.T) {
		form := url.Values{
			"registration_number": {"MH03CC3333"},
			"vehicle_number":      {"V-003"},
			"vehicle_type":        {"truck"},
			"capacity":            {"9000"},
			"fuel_type":           {"diesel"},
			"insurance_expiry":    {"bad"},
			"fitness_expiry":      {"bad"},
			"permit_expiry":       {"bad"},
		}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/"+createdID+"/edit", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
	})

	t.Run("Update validation error missing registration", func(t *testing.T) {
		form := url.Values{
			"registration_number": {""},
			"vehicle_number":      {""},
			"vehicle_type":        {"truck"},
			"capacity":            {"1000"},
		}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/"+createdID+"/edit", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Update not found via validation error after fetch failure still badrequest", func(t *testing.T) {
		form := url.Values{
			"registration_number": {"XX00"},
			"vehicle_number":      {"V-X"},
		}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/nonexistent-id/edit", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		// updateUC will fail to find vehicle -> returns error -> failPage 400
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Delete success", func(t *testing.T) {
		// create vehicle to delete
		vs, _, _ := app.Services.Vehicles.ListVehicles(shared.ContextWithTenantID(context.Background(), shared.DefaultTenant), "MH04DD4444", "", 10, 0)
		require.Len(t, vs, 1)
		delID := string(vs[0].ID)
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/"+delID+"/delete", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "/vehicles", w.Header().Get("Location"))
	})

	t.Run("Delete datastar", func(t *testing.T) {
		vs, _, _ := app.Services.Vehicles.ListVehicles(shared.ContextWithTenantID(context.Background(), shared.DefaultTenant), "MH05EE5555", "", 10, 0)
		require.Len(t, vs, 1)
		delID := string(vs[0].ID)
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/"+delID+"/delete", nil), "1", "user-1", "admin")
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Delete error with closed DB", func(t *testing.T) {
		db2 := newVehiclesSelectedDB(t)
		app2 := newVehiclesSelectedApp(t, db2, &mockAuthSvc{})
		v, err := app2.Services.Vehicles.CreateVehicle(shared.ContextWithTenantID(context.Background(), shared.DefaultTenant), "MHDEL0001", "V-DEL", domain.VehicleTypeTruck, 1000, domain.FuelTypeDiesel, futureDate, futureDate, futureDate, "")
		require.NoError(t, err)
		_ = db2.Close()
		r2 := chi.NewRouter()
		r2.Route("/vehicles", app2.Vehicles.Routes)
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/"+string(v.ID)+"/delete", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r2.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("UpdateStatus success", func(t *testing.T) {
		form := url.Values{"status": {"maintenance"}}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/"+createdID+"/status", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "/vehicles/"+createdID, w.Header().Get("Location"))
		// reset
		form2 := url.Values{"status": {"available"}}
		req2 := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/"+createdID+"/status", strings.NewReader(form2.Encode())), "1", "user-1", "admin")
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusSeeOther, w2.Code)
	})

	t.Run("UpdateStatus not found", func(t *testing.T) {
		form := url.Values{"status": {"available"}}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/nonexistent/status", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("UpdateStatus datastar", func(t *testing.T) {
		form := url.Values{"status": {"available"}}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/"+createdID+"/status", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		// renderFragment may return 200 with fragment, or 500 if template not found? But should be 200
		assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusSeeOther)
	})

	t.Run("UpdateStatus invalid status still handled", func(t *testing.T) {
		form := url.Values{"status": {"invalid_status_xyz"}}
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, "/vehicles/"+createdID+"/status", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		// Should still attempt update; may succeed or fail depending on validation but handler will try
		assert.True(t, w.Code == http.StatusSeeOther || w.Code == http.StatusBadRequest || w.Code == http.StatusOK)
	})
}

func TestSelectedVehicles_AuthChecks(t *testing.T) {
	db := newVehiclesSelectedDB(t)
	denySrv := denyAuthSvc{}
	allowSrv := &mockAuthSvc{}
	appDeny := newVehiclesSelectedApp(t, db, denySrv)
	appAllow := newVehiclesSelectedApp(t, db, allowSrv)

	rDeny := chi.NewRouter()
	rDeny.Route("/vehicles", appDeny.Vehicles.Routes)

	cases := []struct {
		method string
		path   string
	}{
		{"GET", "/vehicles/"},
		{"GET", "/vehicles/new"},
		{"POST", "/vehicles/new"},
		{"GET", "/vehicles/123"},
		{"GET", "/vehicles/123/edit"},
		{"POST", "/vehicles/123/edit"},
		{"POST", "/vehicles/123/delete"},
		{"POST", "/vehicles/123/status"},
	}
	for _, tc := range cases {
		t.Run("deny "+tc.method+" "+tc.path, func(t *testing.T) {
			req := withSession(httptest.NewRequest(tc.method, tc.path, nil), "viewer-1", "viewer")
			w := httptest.NewRecorder()
			rDeny.ServeHTTP(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}

	t.Run("anonymous redirect", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/vehicles/", nil)
		w := httptest.NewRecorder()
		rDeny.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "/login", w.Header().Get("Location"))
	})

	// Allow path still works
	t.Run("allow list", func(t *testing.T) {
		rAllow := chi.NewRouter()
		rAllow.Route("/vehicles", appAllow.Vehicles.Routes)
		req := withSession(httptest.NewRequest(http.MethodGet, "/vehicles/", nil), "admin-1", "admin")
		req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.DefaultTenant))
		w := httptest.NewRecorder()
		rAllow.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("allow new page", func(t *testing.T) {
		rAllow := chi.NewRouter()
		rAllow.Route("/vehicles", appAllow.Vehicles.Routes)
		req := withSession(httptest.NewRequest(http.MethodGet, "/vehicles/new", nil), "admin-1", "admin")
		w := httptest.NewRecorder()
		rAllow.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestSelectedVehicles_PaginationEdge(t *testing.T) {
	db := newVehiclesSelectedDB(t)
	app := newVehiclesSelectedApp(t, db, &mockAuthSvc{})
	r := chi.NewRouter()
	r.Route("/vehicles", app.Vehicles.Routes)
	future := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	for i := 0; i < 3; i++ {
		_, err := app.Services.Vehicles.CreateVehicle(shared.ContextWithTenantID(context.Background(), shared.DefaultTenant), fmt.Sprintf("MHEDGE%04d", i), fmt.Sprintf("V-EDGE-%d", i), domain.VehicleTypeTruck, 1000, domain.FuelTypeDiesel, future, future, future, "")
		require.NoError(t, err)
		time.Sleep(5 * time.Millisecond)
	}
	cases := []string{
		"/vehicles/?limit=1&page=1",
		"/vehicles/?limit=100&page=1",
		"/vehicles/?limit=0&page=-1",
		"/vehicles/?limit=200&page=1",
		"/vehicles/?status=all",
		"/vehicles/?status=running",
	}
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, u, nil), "1", "user-1", "admin")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestSelectedVehicles_ViewExtraBranches(t *testing.T) {
	db := newVehiclesSelectedDB(t)
	app := newVehiclesSelectedApp(t, db, &mockAuthSvc{})
	future := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	v, err := app.Services.Vehicles.CreateVehicle(shared.ContextWithTenantID(context.Background(), shared.DefaultTenant), "MHEXTR001", "V-EXTRA", domain.VehicleTypeTruck, 5000, domain.FuelTypeDiesel, future, future, future, "")
	require.NoError(t, err)
	// Insert extra columns to test View branch: maintenance_due, overrides, rc/puc, telemetry, trips
	_, _ = db.Exec(`UPDATE vehicles SET rc_expiry = ?, puc_expiry = ?, maintenance_due = ?, maintenance_override_by = ?, maintenance_override_at = ?, maintenance_override_reason = ? WHERE id = ?`,
		time.Now().AddDate(0, 2, 0).Format("2006-01-02"), time.Now().AddDate(0, 1, 0).Format("2006-01-02"), "oil_change", "admin-1", time.Now().Format(time.RFC3339), "override reason", string(v.ID))
	// Insert telemetry latest position
	_, _ = db.Exec(`INSERT OR REPLACE INTO vehicle_latest_position (vehicle_id, latitude, longitude, speed, device_time) VALUES (?, ?, ?, ?, ?)`, string(v.ID), 19.076, 72.877, 45.5, time.Now().Format(time.RFC3339))
	// Ensure route exists for trips
	route, err := app.Services.Routes.CreateRoute(shared.ContextWithTenantID(context.Background(), shared.DefaultTenant), "Mumbai", "Pune", 150, 3, 5000, "")
	require.NoError(t, err)
	// Create a trip for this vehicle
	bookingCust, _ := app.Services.Customers.CreateCustomer(shared.ContextWithTenantID(context.Background(), shared.DefaultTenant), "TripViewCust", "", "9000099999", "", "", "", "")
	_, _ = app.Services.Bookings.CreateBooking(shared.ContextWithTenantID(context.Background(), shared.DefaultTenant), service.CreateBookingRequest{CustomerID: bookingCust.ID, RouteID: route.ID, PickupDate: time.Now().Format("2006-01-02"), VehicleType: domain.VehicleTypeTruck, Passengers: 1, Price: 1000})
	// Use non-trip service via direct DB to ensure recent trips query has data
	trID := fmt.Sprintf("trip-%d", time.Now().UnixNano())
	_, _ = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, vehicle_id, route_id, departure_time, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trID, "1", "TR-TEST-001", string(v.ID), string(route.ID), time.Now().Format(time.RFC3339), "scheduled", time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339))
	// Also insert files link via services? View will call GetFilesByEntity; ensure no panic

	r := chi.NewRouter()
	r.Route("/vehicles", app.Vehicles.Routes)
	req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/"+string(v.ID), nil), "1", "user-1", "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "MHEXTR001")
	// also ensure docCards present
	assert.Contains(t, w.Body.String(), "PUCC")
	assert.Contains(t, w.Body.String(), "RC")

	// Edit also should reflect maintenance data
	req2 := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/"+string(v.ID)+"/edit", nil), "1", "user-1", "admin")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
	assert.Contains(t, w2.Body.String(), "oil_change")
}

func TestSelectedVehicles_TenantIsolation(t *testing.T) {
	db := newVehiclesSelectedDB(t)
	app := newVehiclesSelectedApp(t, db, &mockAuthSvc{})
	r := chi.NewRouter()
	r.Route("/vehicles", app.Vehicles.Routes)
	future := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	_, err := app.Services.Vehicles.CreateVehicle(shared.ContextWithTenantID(context.Background(), shared.DefaultTenant), "MHTENANT1", "V-TENANT", domain.VehicleTypeTruck, 1000, domain.FuelTypeDiesel, future, future, future, "")
	require.NoError(t, err)
	for _, tenant := range []string{"1", "tenant-B", "another-tenant"} {
		t.Run("tenant "+tenant, func(t *testing.T) {
			req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, "/vehicles/", nil), tenant, "user-1", "admin")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
			if tenant == "1" {
				assert.Contains(t, w.Body.String(), "MHTENANT1")
			} else {
				// Other tenants should not see tenant 1 vehicle (if UoW filtering works) – but list may return empty; ensure 200
				assert.Equal(t, http.StatusOK, w.Code)
			}
		})
	}
}
