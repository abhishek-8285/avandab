package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ratchet: the feature gate answered with a bare http.Error — text/plain, no
// HTML, no viewport meta. On a phone that means the 980px default layout
// viewport scaled down to the screen, so the message rendered at roughly 7px
// inside a black page (the avandab.com/geofences screenshot). The denial must
// render through layout.html, which is responsive by construction.
func TestRenderFeatureGateError_RendersResponsiveHTMLPage(t *testing.T) {
	db := newShareTestDB(t)
	app := newShareTestApp(t, db, allowAuthSvc{})

	r := chi.NewRouter()
	r.Get("/geofences", func(w http.ResponseWriter, req *http.Request) {
		app.RenderFeatureGateError(w, req, http.StatusForbidden,
			"Geofences & Detention Billing is not enabled",
			"This add-on is not enabled for your organisation. Contact your account manager to enable Geofences & Detention Billing.")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/geofences", nil))

	require.Equal(t, http.StatusForbidden, rec.Code, "the 403 is part of the contract — do not downgrade it to 200")
	body := rec.Body.String()

	assert.True(t, strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html"),
		"denial must be an HTML page, got %q", rec.Header().Get("Content-Type"))
	assert.Contains(t, body, `name="viewport"`,
		"without the viewport meta the phone scales the page down and the text becomes unreadable")
	assert.Contains(t, body, "width=device-width")
	assert.Contains(t, body, "Geofences &amp; Detention Billing is not enabled")
	assert.Contains(t, body, "not enabled for your organisation")
	// The app shell, not a naked error string: the reader must have a way out.
	// (Unauthenticated here, so the template offers Home/Login; a signed-in
	// driver gets the same page with a Dashboard button.)
	assert.Contains(t, body, `href="/"`, "the denial must render inside the app shell, not bare text")
	assert.Contains(t, body, `href="/login"`)
}

// An XHR/API caller must still get JSON, not HTML — the gate sits in front of
// API routes too (e.g. the geofence feed).
func TestRenderFeatureGateError_JSONForAPIClients(t *testing.T) {
	db := newShareTestDB(t)
	app := newShareTestApp(t, db, allowAuthSvc{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/geofences", nil)
	req.Header.Set("Accept", "application/json")
	app.RenderFeatureGateError(rec, req, http.StatusForbidden, "nope", "add-on off")

	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	assert.Equal(t, "ERR_FEATURE_DISABLED", payload.Error.Code)
	assert.Equal(t, "add-on off", payload.Error.Message)
}
