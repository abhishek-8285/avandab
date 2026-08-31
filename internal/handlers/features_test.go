package handlers

import (
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newFeatureTestApp(t *testing.T) *App {
	t.Helper()
	// Templates are loaded from a path relative to the repo root; go test
	// runs with cwd in the package directory, so chdir to the root first
	// (mirrors TestAllTemplatesRenderCleanly).
	if cwd, _ := os.Getwd(); filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	tmpl, err := parseTemplates(&mockAuthSvc{})
	if err != nil {
		t.Fatalf("parseTemplates: %v", err)
	}
	return &App{Templates: tmpl}
}

// featureRouter wires only the public feature routes, mirroring main.go.
func featureRouter(app *App) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/features/{slug}", app.FeaturePage)
	r.Get("/features", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	return r
}

func TestFeaturePagesAllSlugs(t *testing.T) {
	app := newFeatureTestApp(t)
	r := featureRouter(app)

	slugs := []string{
		"dashboard", "trips", "routes", "bookings", "vehicles", "drivers",
		"customers", "invoices", "payments", "reports", "audit-logs",
		"settings", "users", "company", "kharcha", "assistant",
	}

	for _, slug := range slugs {
		t.Run(slug, func(t *testing.T) {
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/features/"+slug, nil))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rr.Code)
			}
			fc, _ := GetFeature(slug)
			if !strings.Contains(rr.Body.String(), html.EscapeString(fc.Title)) {
				t.Fatalf("body missing feature title %q", fc.Title)
			}
			if !strings.Contains(rr.Body.String(), html.EscapeString(fc.Summary)) {
				t.Fatalf("body missing meta description %q", fc.Summary)
			}
		})
	}
}

func TestFeaturePageNotFound(t *testing.T) {
	app := newFeatureTestApp(t)
	r := featureRouter(app)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/features/does-not-exist", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestFeaturesIndexRedirect(t *testing.T) {
	app := newFeatureTestApp(t)
	r := featureRouter(app)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/features", nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 302", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/" {
		t.Fatalf("location = %q, want /", loc)
	}
}
