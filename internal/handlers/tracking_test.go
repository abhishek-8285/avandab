package handlers

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"transport-app/internal/auth"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireSessionForTest mirrors the production protected-group guard
// (middleware.RequireAuth): no session context → 303 to /login.
func requireSessionForTest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// TestTrackingPage_RedirectWithoutSession verifies /tracking bounces
// anonymous traffic to /login (Spec 04 §7).
func TestTrackingPage_RedirectWithoutSession(t *testing.T) {
	app := newTelemetryTestApp(t, denyAuthSvc{})
	tr := &TrackingHandlers{App: app}

	r := chi.NewRouter()
	r.Use(requireSessionForTest)
	r.Route("/tracking", tr.Routes)

	req := httptest.NewRequest(http.MethodGet, "/tracking", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))
}

// TestTrackingPage_TemplateRenders verifies the tracking page content renders
// with the map config injected (Spec 04 §1.3): map container, tile URLs,
// poll endpoint, marker states.
func TestTrackingPage_TemplateRenders(t *testing.T) {
	app := newTelemetryTestApp(t, denyAuthSvc{})
	tmpl := app.Templates.Lookup("tracking.html")
	require.NotNil(t, tmpl)

	data := map[string]interface{}{
		"MapConfig": map[string]interface{}{
			"Provider": "auto", "GoogleStyle": "m", "GL": "IN",
			"OSMUrl":  "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png",
			"PollSec": 10,
		},
		"LiveEndpoint": "/api/v1/telemetry/live",
	}
	var buf strings.Builder
	require.NoError(t, tmpl.Execute(&buf, data))
	body := buf.String()
	assert.Contains(t, body, `id="live-map"`)
	assert.Contains(t, body, "/api/v1/telemetry/live")
	assert.Contains(t, body, "tile.openstreetmap.org")
	assert.Contains(t, body, "maintenance_due")

	// Tile policy regression guards (Spec 04 §2): OSM-only, attribution
	// mandatory, no Google tile scraping in any code path.
	assert.NotContains(t, body, "mt1.google.com", "Google tile scraping must not return")
	assert.Contains(t, body, "openstreetmap.org/copyright", "OSM attribution is required by ODbL")

	// Live-feed correctness regressions:
	// - polling must pause while SSE healthy (no duplicate traffic)
	assert.Contains(t, body, "stopPolling()")
	// - coordinates of 0 are valid (null-safe, not truthiness)
	assert.Contains(t, body, "hasPos(")
	assert.NotContains(t, body, "!v.lat || !v.lng")
	// - telemetry bursts coalesce into one repaint
	assert.Contains(t, body, "scheduleRerender()")

	// No fabricated data / off-system icon fonts on this page.
	assert.NotContains(t, body, "Smart Allocation")
	assert.NotContains(t, body, "material-symbols-outlined")
}

// TestTrackingLayout_MapAssetsConditional verifies the layout loads Leaflet
// only when Extra.MapAssets is set (Spec 04 §2).
func TestTrackingLayout_MapAssetsConditional(t *testing.T) {
	app := newTelemetryTestApp(t, denyAuthSvc{})
	layout := app.Templates.Lookup("layout.html")
	require.NotNil(t, layout)

	run := func(extra map[string]interface{}) string {
		var buf strings.Builder
		require.NoError(t, layout.Execute(&buf, struct {
			Title          string
			Content        template.HTML
			User           *auth.SessionData
			Query          string
			Notifications  interface{}
			UnreadCount    int
			HasUnread      bool
			FlashError     string
			FlashSuccess   string
			Version        string
			PWAEnabled     bool
			Features       map[string]bool
			Extra          map[string]interface{}
			CanonicalPath  string
			NoIndex        bool
			SEODescription string
			OGImage        string
			OGType         string
			SEOJSONLD      template.HTML
		}{
			Title: "X", Content: template.HTML("<p>x</p>"), User: nil,
			Notifications: nil, UnreadCount: 0, HasUnread: false,
			FlashError: "", FlashSuccess: "", Version: "v1", PWAEnabled: false, Extra: extra,
			CanonicalPath: "/tracking", NoIndex: true,
		}))
		return buf.String()
	}

	assert.Contains(t, run(map[string]interface{}{"MapAssets": true}), "/static/js/leaflet/leaflet.js")
	assert.Contains(t, run(map[string]interface{}{"MapAssets": true}), "/static/js/leaflet.markercluster/leaflet.markercluster.js")
	assert.Contains(t, run(map[string]interface{}{"MapAssets": true}), "/static/css/leaflet/leaflet.css")
	assert.NotContains(t, run(nil), "leaflet.js")
	assert.NotContains(t, run(nil), "leaflet.css")
}

// TestTrackingPage_AuthenticatedRoundTrip exercises the full handler path
// (not just template lookup) and re-checks the tile-policy guards on the
// rendered response body.
func TestTrackingPage_AuthenticatedRoundTrip(t *testing.T) {
	app := newTelemetryTestApp(t, denyAuthSvc{})
	tr := &TrackingHandlers{App: app}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, withSession(req, "ops-1", "ops"))
		})
	})
	r.Route("/tracking", tr.Routes)

	req := httptest.NewRequest(http.MethodGet, "/tracking", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `id="live-map"`)
	assert.Contains(t, body, "/api/v1/telemetry/live")
	assert.NotContains(t, body, "mt1.google.com")
	assert.Contains(t, body, "openstreetmap.org/copyright")
}
