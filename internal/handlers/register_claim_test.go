package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	repoSQLite "transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
)

func newRegisterTestApp(t *testing.T) *App {
	t.Helper()
	if cwd, _ := os.Getwd(); filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	db := newCustomersSelectedDB(t)
	tmpl, err := parseTemplates(&mockAuthSvc{})
	require.NoError(t, err)
	authStore := auth.NewSessionStore("test-secret-key-that-is-at-least-32-chars-long", false)
	cfg := &config.Config{
		AppEnv:       "testing",
		CookieSecure: false,
	}
	services := service.NewServices(repoSQLite.NewRepository(db), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), events.NewInMemoryBus())
	app := &App{
		DB:          db,
		Config:      cfg,
		Services:    services,
		Templates:   tmpl,
		AuthSrv:     &mockAuthSvc{},
		AuthStore:   authStore,
		ResetTokens: auth.NewResetTokenStore(0),
	}
	return app
}

func postRegisterWithCompany(app *App, name, email, password, companyName string) *httptest.ResponseRecorder {
	form := url.Values{}
	form.Set("name", name)
	form.Set("email", email)
	form.Set("phone", "9900112233")
	form.Set("company_name", companyName)
	form.Set("password", password)
	form.Set("confirm_password", password)
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	NewAuthHandlers(app).Register(rr, req)
	return rr
}

func TestRegister_SelfServiceAccountProvisionsIsolatedTenant(t *testing.T) {
	app := newRegisterTestApp(t)

	rr := postRegisterWithCompany(app, "Fleet Owner", "owner@fleet.test", "strong-pass-1", "Fast Freight")
	assert.Equal(t, http.StatusSeeOther, rr.Code)
	loc := rr.Header().Get("Location")
	assert.Equal(t, "/company/onboard", loc, "self-registered admin must be sent to company onboarding")

	var roleID int
	var tenantID string
	require.NoError(t, app.DB.QueryRow(`SELECT role_id, tenant_id FROM users WHERE email = 'owner@fleet.test'`).Scan(&roleID, &tenantID))
	assert.Equal(t, 6, roleID, "self-registered account must hold the org_admin role, never platform admin")
	assert.NotEqual(t, "1", tenantID, "must not be assigned to default tenant 1")

	var tenantName string
	require.NoError(t, app.DB.QueryRow(`SELECT name FROM tenants WHERE id = ?`, tenantID).Scan(&tenantName))
	assert.Equal(t, "Fast Freight", tenantName)
}

func TestRegister_MultipleRegistrationsGetIsolatedTenants(t *testing.T) {
	app := newRegisterTestApp(t)

	rr1 := postRegisterWithCompany(app, "Alice Owner", "owner1@fleet.test", "strong-pass-1", "Alice Logistics")
	require.Equal(t, http.StatusSeeOther, rr1.Code)
	assert.Equal(t, "/company/onboard", rr1.Header().Get("Location"))

	rr2 := postRegisterWithCompany(app, "Bob Owner", "owner2@fleet.test", "strong-pass-2", "Bob Transport")
	assert.Equal(t, http.StatusSeeOther, rr2.Code)
	assert.Equal(t, "/company/onboard", rr2.Header().Get("Location"))

	var tid1, tid2 string
	var roleID1, roleID2 int
	require.NoError(t, app.DB.QueryRow(`SELECT role_id, tenant_id FROM users WHERE email = 'owner1@fleet.test'`).Scan(&roleID1, &tid1))
	require.NoError(t, app.DB.QueryRow(`SELECT role_id, tenant_id FROM users WHERE email = 'owner2@fleet.test'`).Scan(&roleID2, &tid2))

	assert.Equal(t, 6, roleID1)
	assert.Equal(t, 6, roleID2)
	assert.NotEqual(t, "1", tid1)
	assert.NotEqual(t, "1", tid2)
	assert.NotEqual(t, tid1, tid2, "two distinct self-registered users must get two different isolated tenant IDs")
}
