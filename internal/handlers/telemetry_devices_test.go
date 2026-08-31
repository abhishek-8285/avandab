package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"transport-app/internal/auth"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// denyAuthSvc denies every permission check.
type denyAuthSvc struct{}

func (denyAuthSvc) Can(userID, resource, action string) bool { return false }
func (denyAuthSvc) Reload() error                            { return nil }
func (denyAuthSvc) AddRoleForUser(userID, role string) error { return nil }
func (denyAuthSvc) DeleteRolesForUser(userID string) error   { return nil }

func newTelemetryTestApp(t *testing.T, authSrv auth.AuthorizationService) *App {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	tmpl, err := parseTemplates(authSrv)
	require.NoError(t, err)
	return &App{Templates: tmpl, AuthSrv: authSrv}
}

func withSession(r *http.Request, userID, role string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), auth.ContextUser,
		&auth.SessionData{UserID: userID, Role: role}))
}

func TestTelemetryDeviceRoutes_ForbiddenForViewer(t *testing.T) {
	app := newTelemetryTestApp(t, denyAuthSvc{})
	td := NewTelemetryDeviceHandlers(app, nil, "test-pepper")

	r := chi.NewRouter()
	r.Route("/telemetry/devices", td.Routes)

	req := withSession(httptest.NewRequest(http.MethodGet, "/telemetry/devices", nil), "viewer-1", "viewer")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestTelemetryDeviceRoutes_RedirectWithoutSession(t *testing.T) {
	app := newTelemetryTestApp(t, denyAuthSvc{})
	td := NewTelemetryDeviceHandlers(app, nil, "test-pepper")

	r := chi.NewRouter()
	r.Route("/telemetry/devices", td.Routes)

	req := httptest.NewRequest(http.MethodGet, "/telemetry/devices", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))
}

func TestTelemetryQuarantineRoutes_ForbiddenForViewer(t *testing.T) {
	app := newTelemetryTestApp(t, denyAuthSvc{})
	td := NewTelemetryDeviceHandlers(app, nil, "test-pepper")

	r := chi.NewRouter()
	r.Route("/telemetry/quarantine", td.QuarantineRoutes)

	req := withSession(httptest.NewRequest(http.MethodGet, "/telemetry/quarantine", nil), "viewer-1", "viewer")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}
