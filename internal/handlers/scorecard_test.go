package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

// TestScorecardRoutes_ForbiddenForViewer verifies every /scorecard route
// returns 403 for a user without scorecard:* permissions (Spec 03 §8 RBAC).
func TestScorecardRoutes_ForbiddenForViewer(t *testing.T) {
	app := newTelemetryTestApp(t, denyAuthSvc{})
	sc := &ScorecardHandlers{App: app}

	r := chi.NewRouter()
	r.Route("/scorecard", sc.Routes)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"leaderboard", http.MethodGet, "/scorecard"},
		{"table", http.MethodGet, "/scorecard/table"},
		{"driver-detail", http.MethodGet, "/scorecard/drivers/d1"},
		{"resolve", http.MethodPost, "/scorecard/drivers/d1/resolve"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := withSession(httptest.NewRequest(tc.method, tc.path, nil), "viewer-1", "viewer")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}
}

// TestScorecardRoutes_RedirectWithoutSession verifies the routes bounce
// anonymous traffic to /login.
func TestScorecardRoutes_RedirectWithoutSession(t *testing.T) {
	app := newTelemetryTestApp(t, denyAuthSvc{})
	sc := &ScorecardHandlers{App: app}

	r := chi.NewRouter()
	r.Route("/scorecard", sc.Routes)

	req := httptest.NewRequest(http.MethodGet, "/scorecard", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))
}

// TestScorecardTierBadge verifies the tier badge CSS mapping.
//
// Tiers now return the same semantic `badge-*` utilities as statusBadgeClass,
// so a tier chip and a status chip share one dark-aware mechanism. Asserting
// the literal token instead of a raw palette pair means the test survives the
// next token rename — only a change in which TIER maps to which badge should
// break it.
func TestScorecardTierBadge(t *testing.T) {
	assert.Equal(t, "badge-success", string(tierBadgeClass("A")))
	assert.Equal(t, "badge-info", string(tierBadgeClass("B")))
	assert.Equal(t, "badge-alert", string(tierBadgeClass("C")))
	assert.Equal(t, "badge-alert", string(tierBadgeClass("Z")))
}
