package handlers

// HTTP characterization for the driver-vehicle assignment actions
// (handlers-end-state ADR, Phase 2). The DB semantics moved to
// internal/dispatch/application (covered by assignment_test.go there); these
// cases pin the handler contract: form validation, session-derived actor
// identity, tenant scoping from the request context and the post/redirect/get
// targets.

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dispatchapp "transport-app/internal/dispatch/application"
)

func seedAssignmentHandlerRows(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES ('drv-http-1', 'DRVHTTP1', 'Http', 'Driver', '9876543201', 'DL-HTTP-1', date('now','+1 year'), 'available', 'tenant-1'),
		       ('drv-http-2', 'DRVHTTP2', 'Http', 'Driver', '9876543202', 'DL-HTTP-2', date('now','+1 year'), 'available', 'tenant-1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
		VALUES ('veh-http-1', 'REG-HTTP-1', 'MH-01-HTTP-1', 'truck', 5000, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 'tenant-1'),
		       ('veh-http-2', 'REG-HTTP-2', 'MH-01-HTTP-2', 'truck', 5000, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 'tenant-1')`)
	require.NoError(t, err)
}

// assignmentRequest builds the POST a browser would send: url id route param,
// form body, and the tenant + session context the router middleware installs.
func assignmentRequest(t *testing.T, target, form, urlID, tenant, userID string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", urlID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return withTenantSession(req, tenant, userID, "admin")
}

func TestAssignmentHandlers_ValidationRejectsEmptyTargets(t *testing.T) {
	db := newCustomersSelectedDB(t)
	app := &App{DB: db}
	dh := &DriverHandlers{App: app}
	vh := &VehicleHandlers{App: app}

	w := httptest.NewRecorder()
	dh.AssignVehicleToDriver(w, assignmentRequest(t, "/drivers/drv-http-1/assign-vehicle", "", "drv-http-1", "tenant-1", "admin-1"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "vehicle_id is required")

	w = httptest.NewRecorder()
	vh.AssignDriverToVehicle(w, assignmentRequest(t, "/vehicles/veh-http-1/assign-driver", "", "veh-http-1", "tenant-1", "admin-1"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "driver_id is required")

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_preferred_vehicles WHERE tenant_id='tenant-1'`).Scan(&count))
	assert.Zero(t, count, "validation failures must not write assignment rows")
}

func TestAssignmentHandlers_AssignUnassignRedirectAndTenantScope(t *testing.T) {
	db := newCustomersSelectedDB(t)
	seedAssignmentHandlerRows(t, db)
	app := &App{DB: db}
	dh := &DriverHandlers{App: app}
	vh := &VehicleHandlers{App: app}
	svc := dispatchapp.NewAssignmentService(db)

	// Driver page: assign the preferred vehicle, then verify redirect + row.
	w := httptest.NewRecorder()
	dh.AssignVehicleToDriver(w, assignmentRequest(t, "/drivers/drv-http-1/assign-vehicle", "vehicle_id=veh-http-1", "drv-http-1", "tenant-1", "admin-1"))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	assert.Equal(t, "/drivers/drv-http-1", w.Header().Get("Location"))
	a, err := svc.ActiveByDriver(context.Background(), "tenant-1", "drv-http-1")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "veh-http-1", a.VehicleID)

	var assignedBy string
	require.NoError(t, db.QueryRow(`SELECT assigned_by FROM driver_preferred_vehicles WHERE id = ?`, a.ID).Scan(&assignedBy))
	assert.Equal(t, "admin-1", assignedBy, "assigned_by must come from the session, not the form")

	// A vehicle-side unassign issued for another tenant must not touch the row.
	w = httptest.NewRecorder()
	vh.UnassignDriverFromVehicle(w, assignmentRequest(t, "/vehicles/veh-http-1/unassign-driver", "", "veh-http-1", "tenant-2", "admin-2"))
	require.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/vehicles/veh-http-1", w.Header().Get("Location"))
	a, err = svc.ActiveByDriver(context.Background(), "tenant-1", "drv-http-1")
	require.NoError(t, err)
	require.NotNil(t, a, "another tenant must not unassign the active row")

	// Same tenant unassign closes it.
	w = httptest.NewRecorder()
	vh.UnassignDriverFromVehicle(w, assignmentRequest(t, "/vehicles/veh-http-1/unassign-driver", "", "veh-http-1", "tenant-1", "admin-1"))
	require.Equal(t, http.StatusSeeOther, w.Code)
	a, err = svc.ActiveByDriver(context.Background(), "tenant-1", "drv-http-1")
	require.NoError(t, err)
	assert.Nil(t, a)

	// Vehicle page: assign the driver, then driver-side unassign mirrors it.
	w = httptest.NewRecorder()
	vh.AssignDriverToVehicle(w, assignmentRequest(t, "/vehicles/veh-http-2/assign-driver", "driver_id=drv-http-2", "veh-http-2", "tenant-1", "admin-1"))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	assert.Equal(t, "/vehicles/veh-http-2", w.Header().Get("Location"))
	b, err := svc.ActiveByVehicle(context.Background(), "tenant-1", "veh-http-2")
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, "drv-http-2", b.DriverID)

	w = httptest.NewRecorder()
	dh.UnassignVehicleFromDriver(w, assignmentRequest(t, "/drivers/drv-http-2/unassign-vehicle", "", "drv-http-2", "tenant-1", "admin-1"))
	require.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/drivers/drv-http-2", w.Header().Get("Location"))
	b, err = svc.ActiveByVehicle(context.Background(), "tenant-1", "veh-http-2")
	require.NoError(t, err)
	assert.Nil(t, b)
}
