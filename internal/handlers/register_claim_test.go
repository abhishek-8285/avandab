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

func postRegister(app *App, email, password string) *httptest.ResponseRecorder {
	form := url.Values{}
	form.Set("name", "Fleet Owner")
	form.Set("email", email)
	form.Set("phone", "9900112233")
	form.Set("password", password)
	form.Set("confirm_password", password)
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	NewAuthHandlers(app).Register(rr, req)
	return rr
}

func TestRegister_FirstRunClaimBecomesAdminAndOnboards(t *testing.T) {
	app := newRegisterTestApp(t)

	rr := postRegister(app, "owner@fleet.test", "strong-pass-1")
	assert.Equal(t, http.StatusSeeOther, rr.Code)
	loc := rr.Header().Get("Location")
	assert.Equal(t, "/company/onboard", loc, "first registrant must be sent to company onboarding")

	var roleID int
	require.NoError(t, app.DB.QueryRow(`SELECT role_id FROM users WHERE email = 'owner@fleet.test'`).Scan(&roleID))
	assert.Equal(t, 1, roleID, "first self-registered account must hold the admin role")
}

func TestRegister_SecondRegistrantStaysViewer(t *testing.T) {
	app := newRegisterTestApp(t)

	rr1 := postRegister(app, "owner@fleet.test", "strong-pass-1")
	require.Equal(t, http.StatusSeeOther, rr1.Code)

	rr2 := postRegister(app, "staff@fleet.test", "strong-pass-2")
	assert.Equal(t, http.StatusSeeOther, rr2.Code)
	assert.Equal(t, "/dashboard", rr2.Header().Get("Location"), "later registrants go to dashboard as viewers")

	var roleID int
	require.NoError(t, app.DB.QueryRow(`SELECT role_id FROM users WHERE email = 'staff@fleet.test'`).Scan(&roleID))
	assert.NotEqual(t, 1, roleID, "second registrant must not gain admin")

	var admins int
	require.NoError(t, app.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE role_id = 1`).Scan(&admins))
	assert.Equal(t, 1, admins)
}
