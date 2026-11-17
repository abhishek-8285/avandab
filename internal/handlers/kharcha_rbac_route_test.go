package handlers

// Route-level regression tests for the H1 money-authorization fix: the
// approve/reject endpoints and the tenant-wide read surfaces must be gated by
// the kharcha permissions, not by trips:update/read (the driver role holds
// those). These tests mount the REAL route definitions, so retagging a route
// back to trips:update would fail here.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

// kharchaRBACRouter mounts KharchaHandlers.Routes with a real Casbin service
// and a per-request session, mirroring the production middleware stack.
func kharchaRBACRouter(t *testing.T, app *App, session *auth.SessionData) http.Handler {
	t.Helper()
	authSvc, err := auth.NewCasbinAuthorizationService(app.DB)
	require.NoError(t, err)
	app.AuthSrv = authSvc

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
			if session != nil {
				ctx = context.WithValue(ctx, auth.ContextUser, session)
			}
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Route("/kharcha", app.Kharcha.Routes)
	return r
}

func seedKharchaRBACUsers(t *testing.T, app *App) {
	t.Helper()
	_, err := app.DB.Exec(`INSERT OR IGNORE INTO users
		(id, name, email, password_hash, role_id, status, tenant_id)
		VALUES ('u-admin','Admin','admin@rbac.test','hash',1,'active','1'),
		       ('u-disp','Dispatcher','disp@rbac.test','hash',2,'active','1'),
		       ('u-driver','Driver','driver@rbac.test','hash',5,'active','1')`)
	require.NoError(t, err)
	_, err = app.DB.Exec(`INSERT OR IGNORE INTO user_roles (user_id, role_id)
		VALUES ('u-admin',1), ('u-disp',2), ('u-driver',5)`)
	require.NoError(t, err)
}

func doKharcha(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestKharchaApproveRoute_DeniesDispatcherAndDriver is the red-proven test for
// the H1 fix: before the fix the route was gated on trips:update, which the
// dispatcher and driver roles hold, so both reached approval.
func TestKharchaApproveRoute_DeniesDispatcherAndDriver(t *testing.T) {
	dbConn, app, _ := setupMobileAPITestEnv(t)
	defer dbConn.Close()
	seedKharchaRBACUsers(t, app)

	dispatcher := kharchaRBACRouter(t, app, &auth.SessionData{UserID: "u-disp", Role: "dispatcher"})
	assert.Equal(t, http.StatusForbidden, doKharcha(t, dispatcher, http.MethodPost, "/kharcha/exp-x/approve").Code,
		"dispatcher holds trips:update but must not approve money")
	assert.Equal(t, http.StatusForbidden, doKharcha(t, dispatcher, http.MethodPost, "/kharcha/exp-x/reject").Code)

	driver := kharchaRBACRouter(t, app, &auth.SessionData{UserID: "u-driver", Role: "driver"})
	assert.Equal(t, http.StatusForbidden, doKharcha(t, driver, http.MethodPost, "/kharcha/exp-x/approve").Code,
		"driver holds trips:update but must not approve money")

	admin := kharchaRBACRouter(t, app, &auth.SessionData{UserID: "u-admin", Role: "admin"})
	adminResp := doKharcha(t, admin, http.MethodPost, "/kharcha/exp-x/approve")
	assert.NotEqual(t, http.StatusForbidden, adminResp.Code,
		"admin must pass the kharcha:approve gate (handler then reports the missing expense)")
}

// TestKharchaReadRoutes_DenyDriver pins the read-surface split: the driver
// role keeps trips:read but must not open the tenant-wide queue/ledger.
func TestKharchaReadRoutes_DenyDriver(t *testing.T) {
	dbConn, app, _ := setupMobileAPITestEnv(t)
	defer dbConn.Close()
	seedKharchaRBACUsers(t, app)

	driver := kharchaRBACRouter(t, app, &auth.SessionData{UserID: "u-driver", Role: "driver"})
	for _, target := range []string{"/kharcha/pending", "/kharcha/ledger"} {
		assert.Equal(t, http.StatusForbidden, doKharcha(t, driver, http.MethodGet, target).Code, target)
	}

	dispatcher := kharchaRBACRouter(t, app, &auth.SessionData{UserID: "u-disp", Role: "dispatcher"})
	for _, target := range []string{"/kharcha/pending", "/kharcha/ledger"} {
		assert.NotEqual(t, http.StatusForbidden, doKharcha(t, dispatcher, http.MethodGet, target).Code,
			"dispatcher holds kharcha:read and must keep the queue: %s", target)
	}
}
