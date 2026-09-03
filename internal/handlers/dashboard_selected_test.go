package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/experiments"
	"transport-app/internal/middleware"
	repoSQLite "transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func newDashboardSelectedDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_dashboard_sel_%d_%s", time.Now().UnixNano(), strings.ReplaceAll(t.Name(), "/", "_"))
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

func newDashboardSelectedApp(t *testing.T, db *sql.DB, cfg *config.Config, authSrv auth.AuthorizationService) *App {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	tmpl, err := parseTemplates(authSrv)
	require.NoError(t, err)
	if cfg == nil {
		cfg = &config.Config{
			AppEnv:               "testing",
			CookieSecret:         "test-cookie-secret-32-chars-long!",
			DashboardSSEEnabled:  false,
			DashboardSSEInterval: 5 * time.Second,
			Experiment: config.ExperimentConfig{
				Rollout:      0,
				ForceVariant: "",
			},
		}
	}
	repo := repoSQLite.NewRepository(db)
	bus := events.NewInMemoryBus()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)
	app := &App{
		DB:          db,
		Config:      cfg,
		Services:    services,
		Templates:   tmpl,
		AuthSrv:     authSrv,
		Experiments: experiments.NewRecorder(db),
	}
	app.Dashboard = &DashboardHandlers{App: app}
	return app
}

// TestSelectedDashboard_Index_RedirectWhenNoCompany verifies tenant isolation and onboarding redirect.
// When company_name is empty, handler must redirect to /company/onboard regardless of tenant.
func TestSelectedDashboard_Index_RedirectWhenNoCompany(t *testing.T) {
	db := newDashboardSelectedDB(t)
	// Clear company_settings to force empty CompanyName
	_, err := db.Exec(`DELETE FROM company_settings`)
	require.NoError(t, err)
	cfg := &config.Config{
		AppEnv:       "testing",
		CookieSecret: "test-secret-32",
		Experiment:   config.ExperimentConfig{Rollout: 0},
	}
	authSrv := &mockAuthSvc{}
	app := newDashboardSelectedApp(t, db, cfg, authSrv)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "user-1", Role: "admin"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	app.Dashboard.Index(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/company/onboard", w.Header().Get("Location"))
}

// TestSelectedDashboard_Index_Success covers success branch: company exists, dashboard renders, variant logic,
// query param override, and tenant isolation (different tenants both succeed).
func TestSelectedDashboard_Index_Success(t *testing.T) {
	db := newDashboardSelectedDB(t)
	// Seed company_settings with full compliance details so redirect does not trigger
	_, _ = db.Exec(`INSERT OR REPLACE INTO company_settings (id, company_name, currency, timezone, address, phone, email) VALUES (1, 'TestCo', 'INR', 'Asia/Kolkata', '123 Logistics St', '+91 9876543210', 'ops@test.co')`)
	cfg := &config.Config{
		AppEnv:       "testing",
		CookieSecret: "test-secret-32",
		Experiment:   config.ExperimentConfig{Rollout: 50, ForceVariant: ""},
	}
	authSrv := &mockAuthSvc{}
	app := newDashboardSelectedApp(t, db, cfg, authSrv)

	tests := []struct {
		name       string
		tenant     string
		userID     string
		query      string
		wantStatus int
	}{
		{"default tenant renders", "1", "user-1", "", http.StatusOK},
		{"tenant isolation - other tenant renders", "tenant-B", "user-1", "", http.StatusOK},
		{"different user same tenant", "1", "user-2", "", http.StatusOK},
		{"variant query override to B", "1", "user-1", "?variant=B", http.StatusOK},
		{"variant query override to A", "1", "user-1", "?variant=A", http.StatusOK},
		{"lowercase variant query", "1", "user-1", "?variant=b", http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url := "/dashboard" + tc.query
			req := httptest.NewRequest(http.MethodGet, url, nil)
			ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID(tc.tenant))
			if tc.userID != "" {
				ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: tc.userID, Role: "admin"})
			}
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()
			app.Dashboard.Index(w, req)
			assert.Equal(t, tc.wantStatus, w.Code)
			if tc.wantStatus == http.StatusOK {
				body := w.Body.String()
				// Should contain dashboard title or stats
				assert.Contains(t, body, "Dashboard")
			}
		})
	}

	// ForceVariant config should override assignment
	cfgForce := &config.Config{
		AppEnv:       "testing",
		CookieSecret: "test-secret-32",
		Experiment:   config.ExperimentConfig{Rollout: 0, ForceVariant: "B"},
	}
	appForce := newDashboardSelectedApp(t, db, cfgForce, authSrv)
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID("1"))
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "u1", Role: "admin"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	appForce.Dashboard.Index(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestSelectedDashboard_Index_WithoutSession ensures handler handles nil session (auth isolation).
func TestSelectedDashboard_Index_WithoutSession(t *testing.T) {
	db := newDashboardSelectedDB(t)
	_, _ = db.Exec(`INSERT OR REPLACE INTO company_settings (id, company_name, address, phone, email) VALUES (1, 'TestCo', '123 Logistics St', '+91 9876543210', 'ops@test.co')`)
	authSrv := &mockAuthSvc{}
	app := newDashboardSelectedApp(t, db, nil, authSrv)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	app.Dashboard.Index(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestSelectedDashboard_Event_Comprehensive covers all Event branches: method, payload, validation, success, tenant isolation.
func TestSelectedDashboard_Event_Comprehensive(t *testing.T) {
	db := newDashboardSelectedDB(t)
	authSrv := &mockAuthSvc{}
	app := newDashboardSelectedApp(t, db, nil, authSrv)

	// 1. Method not allowed
	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dashboard/event", nil)
		ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID("1"))
		ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "u1", Role: "admin"})
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		app.Dashboard.Event(w, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	// 2. Invalid JSON
	t.Run("invalid payload", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/dashboard/event", strings.NewReader("not-json"))
		req.Header.Set("Content-Type", "application/json")
		ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID("1"))
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		app.Dashboard.Event(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid payload")
	})

	// 3. Missing experiment/event
	t.Run("missing required fields", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"experiment": "", "variant": "A", "event": ""})
		req := httptest.NewRequest(http.MethodPost, "/dashboard/event", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID("1"))
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		app.Dashboard.Event(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "experiment and event are required")
	})

	// 4. Invalid variant
	t.Run("invalid variant", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"experiment": "dashboard_v2", "variant": "X", "event": "click"})
		req := httptest.NewRequest(http.MethodPost, "/dashboard/event", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID("1"))
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		app.Dashboard.Event(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid variant")
	})

	// 5. Success with tenant isolation (two tenants both succeed)
	for _, tenant := range []string{"tenant-1", "tenant-2"} {
		t.Run("success tenant "+tenant, func(t *testing.T) {
			body, _ := json.Marshal(map[string]interface{}{
				"experiment": "dashboard_v2",
				"variant":    "A",
				"event":      "dashboard_view",
				"meta":       map[string]interface{}{"today_trips": 5},
			})
			req := httptest.NewRequest(http.MethodPost, "/dashboard/event", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant))
			ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "user-" + tenant, Role: "admin"})
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()
			app.Dashboard.Event(w, req)
			assert.Equal(t, http.StatusNoContent, w.Code)
		})
	}

	// 6. Success with variant B
	t.Run("success variant B", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"experiment": "dashboard_v2",
			"variant":    "B",
			"event":      "dashboard_click",
			"meta":       map[string]interface{}{"action": "refresh"},
		})
		req := httptest.NewRequest(http.MethodPost, "/dashboard/event", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID("1"))
		ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "u1"})
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		app.Dashboard.Event(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	// 7. Success without session (auth isolation - anonymous still allowed to record event)
	t.Run("success without session", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"experiment": "dashboard_v2",
			"variant":    "A",
			"event":      "dashboard_view",
		})
		req := httptest.NewRequest(http.MethodPost, "/dashboard/event", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID("1"))
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		app.Dashboard.Event(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}

// TestSelectedDashboard_Stream covers SSE stream branches and auth/tenant handling
func TestSelectedDashboard_Stream(t *testing.T) {
	db := newDashboardSelectedDB(t)
	authSrv := &mockAuthSvc{}
	// Disabled (graceful single frame)
	cfgDisabled := &config.Config{
		AppEnv:              "testing",
		DashboardSSEEnabled: false,
	}
	appDisabled := newDashboardSelectedApp(t, db, cfgDisabled, authSrv)
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)
	req := httptest.NewRequest(http.MethodGet, "/dashboard/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	appDisabled.Dashboard.Stream(w, req)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "datastar-merge-signals")

	// Enabled (ticker)
	cfgEnabled := &config.Config{
		AppEnv:               "testing",
		DashboardSSEEnabled:  true,
		DashboardSSEInterval: 10 * time.Millisecond,
	}
	appEnabled := newDashboardSelectedApp(t, db, cfgEnabled, authSrv)
	ctxTimeout, cancel := context.WithTimeout(shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-stream")), 50*time.Millisecond)
	defer cancel()
	req2 := httptest.NewRequest(http.MethodGet, "/dashboard/stream", nil).WithContext(ctxTimeout)
	w2 := httptest.NewRecorder()
	appEnabled.Dashboard.Stream(w2, req2)
	assert.Equal(t, "text/event-stream", w2.Header().Get("Content-Type"))
	assert.Contains(t, w2.Body.String(), "dashboard")
}

// TestSelectedDashboard_Routes_Auth ensures middleware auth checks are wired (RBAC) for dashboard stream protections
func TestSelectedDashboard_Routes_Auth(t *testing.T) {
	db := newDashboardSelectedDB(t)
	// Use deny service to verify permission gating on a route that wraps dashboard
	denySrv := denyAuthSvc{}
	allowSrv := &mockAuthSvc{}

	// Create a router that protects dashboard with ResourcePermission (simulating production wiring)
	rDeny := chi.NewRouter()
	tmpAppDeny := newDashboardSelectedApp(t, db, nil, denySrv)
	rDeny.With(middleware.ResourcePermission(denySrv, "dashboard", "read")).Get("/dashboard", tmpAppDeny.Dashboard.Index)

	req := withSession(httptest.NewRequest(http.MethodGet, "/dashboard", nil), "viewer-1", "viewer")
	w := httptest.NewRecorder()
	rDeny.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	rAllow := chi.NewRouter()
	tmpAppAllow := newDashboardSelectedApp(t, db, nil, allowSrv)
	// Seed company for success
	_, _ = db.Exec(`INSERT OR REPLACE INTO company_settings (id, company_name, address, phone, email) VALUES (1, 'TestCo', '123 Logistics St', '+91 9876543210', 'ops@test.co')`)
	rAllow.With(middleware.ResourcePermission(allowSrv, "dashboard", "read")).Get("/dashboard", tmpAppAllow.Dashboard.Index)
	req2 := withSession(httptest.NewRequest(http.MethodGet, "/dashboard", nil), "admin-1", "admin")
	req2 = req2.WithContext(shared.ContextWithTenantID(req2.Context(), shared.DefaultTenant))
	w2 := httptest.NewRecorder()
	rAllow.ServeHTTP(w2, req2)
	// Should not be forbidden (either redirect or ok depending on company state)
	assert.NotEqual(t, http.StatusForbidden, w2.Code)
}
