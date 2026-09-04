package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

func newWorkOrderAPIRouter(app *App) chi.Router {
	r := chi.NewRouter()
	app.Maintenance.RegisterAPIRoutes(r)
	return r
}

func woAPIRequest(t *testing.T, r chi.Router, method, path, tenant, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req = withSession(req, "user-1", "admin")
	req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestWorkOrderAPI_Lifecycle(t *testing.T) {
	db := newMaintHandlerTestDB(t)
	app := newMaintHandlerApp(t, db, maintAllowAuthSvc{})
	r := newWorkOrderAPIRouter(app)

	// tenant-A must exist: close-books records are trigger-FK-checked.
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-A','A','a')`)
	require.NoError(t, err)

	// Validation: missing vehicle/title → 400.
	w := woAPIRequest(t, r, "POST", "/api/v1/work-orders", "tenant-A", `{"title":"x"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Bad JSON → 400.
	w = woAPIRequest(t, r, "POST", "/api/v1/work-orders", "tenant-A", `{oops`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Create → 201.
	w = woAPIRequest(t, r, "POST", "/api/v1/work-orders", "tenant-A",
		`{"vehicle_id":"va","title":"Brake pads","cost_estimate":4500}`)
	require.Equal(t, http.StatusCreated, w.Code)
	var created map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	id, _ := created["id"].(string)
	require.NotEmpty(t, id)
	assert.Equal(t, "open", created["status"])
	assert.Equal(t, "tenant-A", created["tenant_id"])

	// Get own → 200; foreign tenant → 404.
	w = woAPIRequest(t, r, "GET", "/api/v1/work-orders/"+id, "tenant-A", "")
	assert.Equal(t, http.StatusOK, w.Code)
	w = woAPIRequest(t, r, "GET", "/api/v1/work-orders/"+id, "tenant-B", "")
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Assign → 200, moves open → assigned.
	w = woAPIRequest(t, r, "POST", "/api/v1/work-orders/"+id+"/assign", "tenant-A",
		`{"assignee":"Ramesh","vendor":"City Garage"}`)
	require.Equal(t, http.StatusOK, w.Code)
	var assigned map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &assigned))
	assert.Equal(t, "assigned", assigned["status"])
	assert.Equal(t, "Ramesh", assigned["assignee"])

	// Cross-tenant assign → 404.
	w = woAPIRequest(t, r, "POST", "/api/v1/work-orders/"+id+"/assign", "tenant-B",
		`{"assignee":"X","vendor":"Y"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Unknown transition → 400.
	w = woAPIRequest(t, r, "POST", "/api/v1/work-orders/"+id+"/transition", "tenant-A",
		`{"status":"flying"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// in_progress → done → 200 each.
	w = woAPIRequest(t, r, "POST", "/api/v1/work-orders/"+id+"/transition", "tenant-A",
		`{"status":"in_progress"}`)
	assert.Equal(t, http.StatusOK, w.Code)
	w = woAPIRequest(t, r, "POST", "/api/v1/work-orders/"+id+"/transition", "tenant-A",
		`{"status":"done"}`)
	require.Equal(t, http.StatusOK, w.Code)
	var done map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &done))
	assert.Equal(t, "done", done["status"])
	assert.NotNil(t, done["closed_at"])

	// Terminal card is immutable → 409.
	w = woAPIRequest(t, r, "POST", "/api/v1/work-orders/"+id+"/transition", "tenant-A",
		`{"status":"open"}`)
	assert.Equal(t, http.StatusConflict, w.Code)

	// List filtered by status; empty tenant lists nothing.
	w = woAPIRequest(t, r, "GET", "/api/v1/work-orders?status=done", "tenant-A", "")
	require.Equal(t, http.StatusOK, w.Code)
	var list map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Len(t, list["work_orders"], 1)
	w = woAPIRequest(t, r, "GET", "/api/v1/work-orders?status=open", "tenant-A", "")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Empty(t, list["work_orders"])
	w = woAPIRequest(t, r, "GET", "/api/v1/work-orders", "tenant-B", "")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Empty(t, list["work_orders"])
}

func TestWorkOrderAPI_RBAC(t *testing.T) {
	db := newMaintHandlerTestDB(t)
	app := newMaintHandlerApp(t, db, maintDenyAuthSvc{})
	r := chi.NewRouter()
	r.With(middleware.RequirePermission(app.AuthSrv, "maintenance", "read")).Get("/api/v1/work-orders", app.Maintenance.APIListWorkOrders)
	r.With(middleware.RequirePermission(app.AuthSrv, "maintenance", "create")).Post("/api/v1/work-orders", app.Maintenance.APICreateWorkOrder)

	w := woAPIRequest(t, r, "GET", "/api/v1/work-orders", "tenant-A", "")
	assert.Equal(t, http.StatusForbidden, w.Code)
	w = woAPIRequest(t, r, "POST", "/api/v1/work-orders", "tenant-A", `{"vehicle_id":"v","title":"t"}`)
	assert.Equal(t, http.StatusForbidden, w.Code)
}
