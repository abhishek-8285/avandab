package handlers

import (
	"bytes"
	"context"
	"image/png"
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
	"transport-app/internal/shared"
)

func setupPWATestRouter(t *testing.T, pwaEnabled bool) (*chi.Mux, *App, *config.Config) {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	cfg := &config.Config{
		AppEnv:     "testing",
		StaticDir:  "internal/static",
		PWAEnabled: pwaEnabled,
	}

	authSvc := &mockAuthSvc{}
	tmpl, _ := parseTemplates(authSvc)

	app := &App{
		Config:    cfg,
		Templates: tmpl,
		AuthSrv:   authSvc,
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
			ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
				UserID: "u-1",
				Role:   "admin",
			})
			req = req.WithContext(ctx)
			next.ServeHTTP(w, req)
		})
	})

	// Mount PWA routes
	app.MountPWARoutes(r)

	// Mount static file server
	fileServer := http.FileServer(http.Dir(cfg.StaticDir))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Mount a sample page to test layout rendering
	r.Get("/dashboard", func(w http.ResponseWriter, req *http.Request) {
		app.renderPage(w, req, "dashboard.html", PageData{
			Title: "Operations Dashboard",
			User: &auth.SessionData{
				UserID: "u-1",
				Role:   "admin",
			},
		})
	})

	return r, app, cfg
}

func TestManifestContentType(t *testing.T) {
	r, _, _ := setupPWATestRouter(t, true)

	req := httptest.NewRequest("GET", "/manifest.webmanifest", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/manifest+json")
	assert.Contains(t, rec.Header().Get("Cache-Control"), "public, max-age=86400")

	body := rec.Body.String()
	assert.Contains(t, body, "Avandab Fleet Management")
	assert.Contains(t, body, "Avandab")
	assert.Contains(t, body, "/dashboard")
	assert.Contains(t, body, "standalone")
	assert.Contains(t, body, "icon-192.png")
	assert.Contains(t, body, "icon-512.png")
	assert.Contains(t, body, "#2563eb")
}

func TestServiceWorkerContentType(t *testing.T) {
	r, _, _ := setupPWATestRouter(t, true)

	req := httptest.NewRequest("GET", "/sw.js", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/javascript")
	assert.Contains(t, rec.Header().Get("Cache-Control"), "no-cache")
	assert.Equal(t, "/", rec.Header().Get("Service-Worker-Allowed"))

	body := rec.Body.String()
	assert.Contains(t, body, "CACHE_NAME")
	assert.Contains(t, body, "STATIC_CACHE")
	assert.Contains(t, body, "cacheFirst")
	assert.Contains(t, body, "networkFirst")
	assert.Contains(t, body, "PRECACHE_ASSETS")
}

func TestPWADisabled(t *testing.T) {
	r, _, _ := setupPWATestRouter(t, false)

	// 1. /manifest.webmanifest -> 404
	reqM := httptest.NewRequest("GET", "/manifest.webmanifest", nil)
	recM := httptest.NewRecorder()
	r.ServeHTTP(recM, reqM)
	assert.Equal(t, http.StatusNotFound, recM.Code)

	// 2. /sw.js -> 404
	reqSW := httptest.NewRequest("GET", "/sw.js", nil)
	recSW := httptest.NewRecorder()
	r.ServeHTTP(recSW, reqSW)
	assert.Equal(t, http.StatusNotFound, recSW.Code)

	// 3. /dashboard -> 200, but layout does not include manifest or SW registration
	reqD := httptest.NewRequest("GET", "/dashboard", nil)
	recD := httptest.NewRecorder()
	r.ServeHTTP(recD, reqD)
	assert.Equal(t, http.StatusOK, recD.Code)
	body := recD.Body.String()
	assert.NotContains(t, body, `href="/manifest.webmanifest"`)
	assert.NotContains(t, body, `navigator.serviceWorker.register`)
}

func TestPWAEnabledLayout(t *testing.T) {
	r, _, _ := setupPWATestRouter(t, true)

	req := httptest.NewRequest("GET", "/dashboard", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `<link rel="manifest" href="/manifest.webmanifest">`)
	assert.Contains(t, body, `navigator.serviceWorker.register('/sw.js'`)
	assert.Contains(t, body, `<meta name="theme-color" content="#2563eb">`)
	assert.Contains(t, body, `<link rel="apple-touch-icon" href="/static/icons/icon-192.png">`)
}

func TestIconsExistAndValid(t *testing.T) {
	r, _, _ := setupPWATestRouter(t, true)

	// 1. icon-192.png
	req192 := httptest.NewRequest("GET", "/static/icons/icon-192.png", nil)
	rec192 := httptest.NewRecorder()
	r.ServeHTTP(rec192, req192)

	assert.Equal(t, http.StatusOK, rec192.Code)
	assert.Contains(t, rec192.Header().Get("Content-Type"), "image/png")

	img192, err := png.Decode(bytes.NewReader(rec192.Body.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, 192, img192.Bounds().Dx())
	assert.Equal(t, 192, img192.Bounds().Dy())

	// 2. icon-512.png
	req512 := httptest.NewRequest("GET", "/static/icons/icon-512.png", nil)
	rec512 := httptest.NewRecorder()
	r.ServeHTTP(rec512, req512)

	assert.Equal(t, http.StatusOK, rec512.Code)
	assert.Contains(t, rec512.Header().Get("Content-Type"), "image/png")

	img512, err := png.Decode(bytes.NewReader(rec512.Body.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, 512, img512.Bounds().Dx())
	assert.Equal(t, 512, img512.Bounds().Dy())
}

func TestFaviconSVGServingAndMime(t *testing.T) {
	r, _, _ := setupPWATestRouter(t, true)

	req := httptest.NewRequest("GET", "/static/img/favicon.svg", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "image/svg+xml")
	assert.Contains(t, rec.Body.String(), "<svg")
	assert.Contains(t, rec.Body.String(), "xmlns=\"http://www.w3.org/2000/svg\"")
}
