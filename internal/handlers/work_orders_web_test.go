package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

func woWebRequest(t *testing.T, handler http.HandlerFunc, method, path, tenant, form string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form))
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req = withSession(req, "user-1", "admin")
	req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant)))
	// chi URL params for {id} routes.
	rctx := chi.NewRouteContext()
	if id := chiIDFromPath(path); id != "" {
		rctx.URLParams.Add("id", id)
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func chiIDFromPath(path string) string {
	// /maintenance/work-orders/{id}[/suffix]
	rest, ok := strings.CutPrefix(path, "/maintenance/work-orders/")
	if !ok || rest == "" || rest == "new" {
		return ""
	}
	if i := strings.Index(rest, "/"); i >= 0 {
		return rest[:i]
	}
	return rest
}

func TestWorkOrderWeb_Create_And_Detail(t *testing.T) {
	db := newMaintHandlerTestDB(t)
	app := newMaintHandlerApp(t, db, maintAllowAuthSvc{})
	insertMaintTestVehicle(t, db, "veh-web", "REG-WEB")

	// Validation: missing title → back to form with flash.
	w := woWebRequest(t, app.Maintenance.CreateWorkOrder, "POST", "/maintenance/work-orders", "1",
		url.Values{"vehicle_id": {"veh-web"}}.Encode())
	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "/maintenance/work-orders/new")

	// Create → redirect to detail.
	w = woWebRequest(t, app.Maintenance.CreateWorkOrder, "POST", "/maintenance/work-orders", "1",
		url.Values{"vehicle_id": {"veh-web"}, "title": {"Web brake job"}, "cost_estimate": {"2500"}}.Encode())
	require.Equal(t, http.StatusSeeOther, w.Code)
	loc := w.Header().Get("Location")
	require.Contains(t, loc, "/maintenance/work-orders/")

	// Detail renders title + actions.
	w = woWebRequest(t, app.Maintenance.ViewWorkOrder, "GET", loc, "1", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Web brake job")
	assert.Contains(t, w.Body.String(), "Start Work")
	assert.Contains(t, w.Body.String(), "REG-WEB", "vehicle shows registration, not raw UUID")
	assert.NotContains(t, w.Body.String(), `value="assigned"`, "no redundant Assign status button next to the Assign form")

	// Foreign tenant sees 404.
	w = woWebRequest(t, app.Maintenance.ViewWorkOrder, "GET", loc, "tenant-X", "")
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Transition via web → redirect; detail shows new status.
	w = woWebRequest(t, app.Maintenance.TransitionWorkOrder, "POST", loc+"/transition", "1",
		url.Values{"status": {"assigned"}}.Encode())
	assert.Equal(t, http.StatusSeeOther, w.Code)
	w = woWebRequest(t, app.Maintenance.ViewWorkOrder, "GET", loc, "1", "")
	assert.Contains(t, w.Body.String(), "Assigned")

	// Unknown status → flash, stays put.
	w = woWebRequest(t, app.Maintenance.TransitionWorkOrder, "POST", loc+"/transition", "1",
		url.Values{"status": {"flying"}}.Encode())
	assert.Equal(t, http.StatusSeeOther, w.Code)
	var flash string
	for _, c := range w.Result().Cookies() {
		if c.Name == "flash_error" {
			flash = c.Value
		}
	}
	assert.NotEmpty(t, flash)

	// Dashboard lists the card.
	req := withSession(httptest.NewRequest("GET", "/maintenance", nil), "user-1", "admin")
	req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.TenantID("1")))
	dash := httptest.NewRecorder()
	app.Maintenance.Index(dash, req)
	require.Equal(t, http.StatusOK, dash.Code)
	assert.Contains(t, dash.Body.String(), "Open Job Cards")
	assert.Contains(t, dash.Body.String(), "Web brake job")
}

func TestWorkOrderWeb_RBAC_Denial(t *testing.T) {
	db := newMaintHandlerTestDB(t)
	app := newMaintHandlerApp(t, db, maintDenyAuthSvc{})

	r := chi.NewRouter()
	r.With(middleware.ResourcePermission(app.AuthSrv, "maintenance", "read")).Get("/maintenance/work-orders/{id}", app.Maintenance.ViewWorkOrder)
	r.With(middleware.ResourcePermission(app.AuthSrv, "maintenance", "create")).Post("/maintenance/work-orders", app.Maintenance.CreateWorkOrder)

	req := withSession(httptest.NewRequest("GET", "/maintenance/work-orders/wo-x", nil), "viewer-1", "viewer")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	form := url.Values{"vehicle_id": {"v"}, "title": {"t"}}
	req = withSession(httptest.NewRequest("POST", "/maintenance/work-orders", strings.NewReader(form.Encode())), "viewer-1", "viewer")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}
