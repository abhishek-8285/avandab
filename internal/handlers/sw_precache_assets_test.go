package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/config"
)

// Red-proving test ONLY for sw.js atomic-precache defect. No production edits.
// internal/static/js/sw.js:13-14 precaches /static/css/material-symbols.css and
// /static/css/material-icons.css which do not exist under internal/static/css,
// and install uses atomic cache.addAll at sw.js:39-44, so one 404 rejects the
// whole install. Route: app.go:1630-1655, registration: seo_head.html:20-30.
// Static + httptest only: no browser, no live DB/network.

func swPrecacheTestDirs(t *testing.T) (staticDir, swPath string) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must resolve test file path")
	handlersDir := filepath.Dir(thisFile)
	// handlersDir = <repo>/internal/handlers regardless of test cwd.
	staticDir = filepath.Join(handlersDir, "..", "static")
	swPath = filepath.Join(staticDir, "js", "sw.js")
	return staticDir, swPath
}

func swPrecacheAssets(t *testing.T, swPath string) []string {
	t.Helper()
	raw, err := os.ReadFile(swPath)
	require.NoError(t, err, "must read sw.js at %s", swPath)
	body := string(raw)

	// Premise: install is atomic over the whole list.
	require.Contains(t, body, "cache.addAll(PRECACHE_ASSETS)",
		"premise: sw.js install must use atomic cache.addAll(PRECACHE_ASSETS)")

	start := strings.Index(body, "PRECACHE_ASSETS")
	require.NotEqual(t, -1, start, "premise: PRECACHE_ASSETS list must exist in sw.js")
	block := body[start:]
	end := strings.Index(block, "];")
	require.NotEqual(t, -1, end, "premise: PRECACHE_ASSETS list must terminate")
	block = block[:end]

	// Local (never package-level): CI forbids shared mutable state in tests.
	swPrecacheURLRe := regexp.MustCompile(`['"](\/static\/[^'"]+)['"]`)
	assets := swPrecacheURLRe.FindAllString(block, -1)
	require.NotEmpty(t, assets, "premise: PRECACHE_ASSETS must list /static/ URLs")
	// Strip surrounding quotes.
	out := make([]string, 0, len(assets))
	for _, a := range assets {
		out = append(out, a[1:len(a)-1])
	}
	return out
}

func TestSWPrecacheAssetsResolveToDisk(t *testing.T) {
	staticDir, swPath := swPrecacheTestDirs(t)
	assets := swPrecacheAssets(t, swPath)

	var missing []string
	for _, u := range assets {
		rel := strings.TrimPrefix(u, "/static/")
		full := filepath.Join(staticDir, filepath.FromSlash(rel))
		fi, err := os.Stat(full)
		if err != nil || fi.IsDir() {
			missing = append(missing, u)
		}
	}
	assert.Empty(t, missing,
		"sw.js PRECACHE_ASSETS must all resolve to real files on disk (atomic addAll fails on first 404)")
}

func TestSWPrecacheAssetsServeOK(t *testing.T) {
	staticDir, swPath := swPrecacheTestDirs(t)
	assets := swPrecacheAssets(t, swPath)

	cfg := &config.Config{AppEnv: "testing", StaticDir: staticDir, PWAEnabled: true}
	app := &App{Config: cfg}
	r := chi.NewRouter()
	app.MountPWARoutes(r)
	fileServer := http.FileServer(http.Dir(cfg.StaticDir))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Premise: the worker itself is served (app.go:1654).
	reqSW := httptest.NewRequest("GET", "/sw.js", nil)
	recSW := httptest.NewRecorder()
	r.ServeHTTP(recSW, reqSW)
	require.Equal(t, http.StatusOK, recSW.Code, "premise: GET /sw.js must serve (MountPWARoutes)")

	var failed []string
	for _, u := range assets {
		req := httptest.NewRequest("GET", u, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			failed = append(failed, u)
		}
	}
	assert.Empty(t, failed,
		"every sw.js PRECACHE_ASSETS URL must serve 200 (else atomic install rejects)")
}
