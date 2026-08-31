package handlers

import (
	"context"
	"database/sql"
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
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/middleware"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	"transport-app/internal/telemetry"
)

func newRealtimeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_realtime_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
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

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestDashboardSSE_StreamHeadersAndPayload(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	db := newRealtimeTestDB(t)
	repo := sqlite.NewRepository(db)
	bus := events.NewInMemoryBus()
	cfg := &config.Config{
		AppEnv:               "testing",
		Port:                 "8080",
		DashboardSSEEnabled:  true,
		DashboardSSEInterval: 10 * time.Millisecond,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)

	authSvc := &mockAuthSvc{}
	tmpl, err := parseTemplates(authSvc)
	require.NoError(t, err)

	app := &App{
		Config:    cfg,
		Templates: tmpl,
		AuthSrv:   authSvc,
		Services:  services,
	}
	app.Dashboard = &DashboardHandlers{App: app}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	ctx = shared.ContextWithTenantID(ctx, shared.DefaultTenant)
	req := httptest.NewRequest("GET", "/dashboard/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	app.Dashboard.Stream(w, req)

	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", w.Header().Get("Connection"))
	assert.Equal(t, "no", w.Header().Get("X-Accel-Buffering"))

	body := w.Body.String()
	assert.Contains(t, body, "event: datastar-merge-signals")
	assert.Contains(t, body, `data: {"dashboard":`)
}

func TestDashboardSSE_DisabledGracefulClose(t *testing.T) {
	db := newRealtimeTestDB(t)
	repo := sqlite.NewRepository(db)
	bus := events.NewInMemoryBus()
	cfg := &config.Config{
		AppEnv:              "testing",
		Port:                "8080",
		DashboardSSEEnabled: false,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)

	app := &App{
		Config:   cfg,
		Services: services,
	}
	app.Dashboard = &DashboardHandlers{App: app}

	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)
	req := httptest.NewRequest("GET", "/dashboard/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	// Should emit single frame and return immediately without hanging
	app.Dashboard.Stream(w, req)

	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), `data: {"dashboard":`)
}

func TestMapSSE_StreamHeadersAndPayload(t *testing.T) {
	db := newRealtimeTestDB(t)
	liveStore := telemetry.NewLiveStore(db, 15*time.Minute)

	app := &App{
		Config: &config.Config{AppEnv: "testing"},
	}
	mapHandler := NewMapHandlers(app, liveStore)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	ctx = shared.ContextWithTenantID(ctx, shared.DefaultTenant)
	req := httptest.NewRequest("GET", "/map/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	mapHandler.Stream(w, req)

	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", w.Header().Get("Connection"))
	assert.Equal(t, "no", w.Header().Get("X-Accel-Buffering"))

	body := w.Body.String()
	assert.Contains(t, body, "event: datastar-merge-signals")
	assert.Contains(t, body, `data: {"vehicles":`)
}

func TestMapPage_Render(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	authSvc := &mockAuthSvc{}
	tmpl, err := parseTemplates(authSvc)
	require.NoError(t, err)

	app := &App{
		Config:    &config.Config{AppEnv: "testing"},
		Templates: tmpl,
		AuthSrv:   authSvc,
	}
	mapHandler := NewMapHandlers(app, nil)

	req := httptest.NewRequest("GET", "/map", nil)
	ctx := context.WithValue(req.Context(), auth.ContextUser, &auth.SessionData{UserID: "u-1", Role: "admin"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	mapHandler.Page(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `id="map"`)
	assert.Contains(t, w.Body.String(), "Live Fleet Map")
	assert.Contains(t, w.Body.String(), "/static/js/map.js")
}

func TestSkipForPaths_TimeoutBypass(t *testing.T) {
	r := chi.NewRouter()

	// Short 20ms timeout on all routes EXCEPT /stream/bypass
	r.Use(middleware.SkipForPaths(
		chiMiddleware.Timeout(20*time.Millisecond),
		"/stream/bypass",
	))

	r.Get("/normal", func(w http.ResponseWriter, req *http.Request) {
		select {
		case <-req.Context().Done():
			return
		case <-time.After(50 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}
	})

	r.Get("/stream/bypass", func(w http.ResponseWriter, req *http.Request) {
		time.Sleep(30 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("stream-ok"))
	})

	// /normal should hit timeout
	reqNormal := httptest.NewRequest("GET", "/normal", nil)
	wNormal := httptest.NewRecorder()
	r.ServeHTTP(wNormal, reqNormal)
	assert.Equal(t, http.StatusGatewayTimeout, wNormal.Code)

	// /stream/bypass should succeed without timeout
	reqBypass := httptest.NewRequest("GET", "/stream/bypass", nil)
	wBypass := httptest.NewRecorder()
	r.ServeHTTP(wBypass, reqBypass)
	assert.Equal(t, http.StatusOK, wBypass.Code)
	assert.Equal(t, "stream-ok", wBypass.Body.String())
}

func TestDashboardSSE_ThroughMiddlewareStack(t *testing.T) {
	db := newRealtimeTestDB(t)
	repo := sqlite.NewRepository(db)
	bus := events.NewInMemoryBus()
	cfg := &config.Config{
		AppEnv:              "testing",
		Port:                "8080",
		DashboardSSEEnabled: false,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)

	authSvc := &mockAuthSvc{}
	tmpl, err := parseTemplates(authSvc)
	require.NoError(t, err)

	app := &App{
		Config:    cfg,
		Templates: tmpl,
		AuthSrv:   authSvc,
		Services:  services,
	}
	app.Dashboard = &DashboardHandlers{App: app}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.SPAMiddleware)
	r.Get("/dashboard/stream", app.Dashboard.Stream)

	req := httptest.NewRequest("GET", "/dashboard/stream", nil)
	ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "event: datastar-merge-signals")
}
