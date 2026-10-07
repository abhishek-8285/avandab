package features

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

func gateTestRequest(tenant string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if tenant != "" {
		req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant)))
	}
	return req
}

// Unresolved tenant must be denied, never served defaults or tenant "1".
func TestGate_NoTenantDenied(t *testing.T) {
	reg := testRegistry(t, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })

	rec := httptest.NewRecorder()
	Gate(reg, "fastag", nil)(next).ServeHTTP(rec, gateTestRequest(""))
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = httptest.NewRecorder()
	Gate(reg, "ewaybill", nil)(next).ServeHTTP(rec, gateTestRequest(""))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// Resolved tenant with explicit grant passes through.
func TestGate_GrantedTenantPasses(t *testing.T) {
	reg := testRegistry(t, nil)
	ctx := context.Background()
	require.NoError(t, reg.Set(ctx, "tenant-g", "fastag", true, "admin"))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	rec := httptest.NewRecorder()
	Gate(reg, "fastag", nil)(next).ServeHTTP(rec, gateTestRequest("tenant-g"))
	assert.Equal(t, http.StatusTeapot, rec.Code)
}

// Ratchet: the denial used to be a bare http.Error — text/plain, no HTML, no
// viewport meta — which a phone scales down from the 980px default layout
// viewport, rendering the message at roughly 7px. The denial must go through
// the app's renderer, with the add-on's own name and the 403 intact.
func TestGate_AddonDenialRendersThroughTheAppRenderer(t *testing.T) {
	reg := testRegistry(t, nil)
	// The screenshot case: geofences defaults on, this org's grant is revoked
	// (or its subscription lapsed), so the gate denies.
	require.NoError(t, reg.Set(context.Background(), "tenant-x", "geofences", false, "admin"))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })

	var got struct {
		status, code   int
		title, message string
		rendered       bool
	}
	render := func(w http.ResponseWriter, r *http.Request, status int, title, message string) {
		got.status, got.title, got.message, got.rendered = status, title, message, true
		w.WriteHeader(status)
		_, _ = w.Write([]byte("<!doctype html><html><head>" +
			`<meta name="viewport" content="width=device-width, initial-scale=1.0">` +
			"</head><body>Geofences &amp; Detention Billing</body></html>"))
	}

	rec := httptest.NewRecorder()
	Gate(reg, "geofences", render)(next).ServeHTTP(rec, gateTestRequest("tenant-x"))

	require.True(t, got.rendered, "add-on denial must not fall back to http.Error")
	assert.Equal(t, http.StatusForbidden, got.status)
	assert.Contains(t, got.message, "not enabled for your organisation")
	assert.Contains(t, got.message, "Geofences & Detention Billing", "the message must name the add-on")
	assert.Contains(t, got.title, "Geofences & Detention Billing")
	assert.Contains(t, rec.Body.String(), `name="viewport"`)
}

// Core features keep their 404 but stop emitting stdlib plain text.
func TestGate_CoreDenialRendersThroughTheAppRenderer(t *testing.T) {
	reg := testRegistry(t, nil)
	require.NoError(t, reg.Set(context.Background(), "tenant-x", "ewaybill", false, "admin"))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })

	var status int
	render := func(w http.ResponseWriter, r *http.Request, s int, _, _ string) {
		status = s
		w.WriteHeader(s)
	}

	rec := httptest.NewRecorder()
	Gate(reg, "ewaybill", render)(next).ServeHTTP(rec, gateTestRequest("tenant-x"))
	assert.Equal(t, http.StatusNotFound, status)
}

// A granted tenant must not reach the renderer at all.
func TestGate_GrantedTenantNeverRendersDenial(t *testing.T) {
	reg := testRegistry(t, nil)
	require.NoError(t, reg.Set(context.Background(), "tenant-r", "geofences", true, "admin"))

	render := func(w http.ResponseWriter, r *http.Request, s int, _, _ string) {
		t.Errorf("granted tenant was denied: status %d", s)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	rec := httptest.NewRecorder()
	Gate(reg, "geofences", render)(next).ServeHTTP(rec, gateTestRequest("tenant-r"))
	assert.Equal(t, http.StatusTeapot, rec.Code)
}
