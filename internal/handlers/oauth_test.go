package handlers

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
)

func newGoogleDB(t *testing.T) *sql.DB {
	t.Helper()
	if cwd, _ := os.Getwd(); filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	db, err := sql.Open("sqlite", "file:tmp/google_oauth_test.db")
	require.NoError(t, err)
	require.NoError(t, db.Ping())

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "db/migrations"))
	// fresh rows per test run
	_, _ = db.Exec(`DELETE FROM users WHERE email LIKE '%@google-test.local'`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newGoogleTestHarness(t *testing.T, oauthCfg *auth.OAuthConfig) (*GoogleOAuthHandlers, *App, *sql.DB) {
	t.Helper()
	tmpl, err := parseTemplates(&mockAuthSvc{})
	require.NoError(t, err)

	cfg := &config.Config{AppEnv: "development", CookieSecure: false}
	db := newGoogleDB(t)
	app := &App{
		Templates:   tmpl,
		AuthStore:   auth.NewSessionStore("test-secret-key-that-is-at-least-32-chars-long", false),
		Config:      cfg,
		ResetTokens: auth.NewResetTokenStore(0),
		GoogleOAuth: oauthCfg,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app.Services = service.NewServices(sqlite.NewRepository(db), cfg, logger, nil)
	h := NewGoogleOAuthHandlers(app, oauthCfg)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM users WHERE email LIKE '%@google-test.local'`)
	})
	return h, app, db
}

func fakeGoogle(t *testing.T, email string) *auth.OAuthConfig {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fake-at","token_type":"Bearer","expires_in":3600}`))
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		payload, _ := json.Marshal(map[string]interface{}{
			"sub": "google-sub-1", "email": email, "email_verified": true, "name": "Google Op",
		})
		_, _ = w.Write(payload)
	})

	cfg := auth.NewGoogleOAuthConfig("cid", "csecret", "https://app.local/auth/google/callback")
	cfg.TokenURL = srv.URL + "/token"
	cfg.UserInfoURL = srv.URL + "/userinfo"
	return cfg
}

// TestGoogleOAuth_Begin_RedirectsWithState — configured flow redirects to
// Google with state + sets the HttpOnly state cookie.
func TestGoogleOAuth_Begin_RedirectsWithState(t *testing.T) {
	h, _, _ := newGoogleTestHarness(t, fakeGoogle(t, "op@google-test.local"))

	req := httptest.NewRequest(http.MethodGet, "/auth/google", nil)
	w := httptest.NewRecorder()
	h.Begin(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	loc := w.Header().Get("Location")
	assert.Contains(t, loc, "accounts.google.com")
	assert.Contains(t, loc, "state=")

	var stateCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == googleStateCookie {
			stateCookie = c
		}
	}
	require.NotNil(t, stateCookie, "oauth_state cookie must be set")
	assert.True(t, stateCookie.HttpOnly)
}

// TestGoogleOAuth_Disabled_Degrades — unconfigured flow redirects to /login
// with a flash message instead of erroring.
func TestGoogleOAuth_Disabled_Degrades(t *testing.T) {
	h, _, _ := newGoogleTestHarness(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/auth/google", nil)
	w := httptest.NewRecorder()
	h.Begin(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))
	require.False(t, h.Enabled(), "nil config must disable the flow")
}

// TestGoogleOAuth_Callback_StateMismatchRejected — CSRF: cookie state must
// equal query state or the callback refuses and bounces to /login.
func TestGoogleOAuth_Callback_StateMismatchRejected(t *testing.T) {
	h, _, _ := newGoogleTestHarness(t, fakeGoogle(t, "op@google-test.local"))

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=c&state=evil", nil)
	req.AddCookie(&http.Cookie{Name: googleStateCookie, Value: "honest"})
	w := httptest.NewRecorder()
	h.Callback(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))

	var flash *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "flash_error" {
			flash = c
		}
	}
	require.NotNil(t, flash)
	assert.Contains(t, flash.Value, "invalid state")
}

// TestGoogleOAuth_Callback_MissingStateCookieRejected — no cookie at all is
// the replay/forgery case and must be rejected the same way.
func TestGoogleOAuth_Callback_MissingStateCookieRejected(t *testing.T) {
	h, _, _ := newGoogleTestHarness(t, fakeGoogle(t, "op@google-test.local"))

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=c&state=x", nil)
	w := httptest.NewRecorder()
	h.Callback(w, req)
	assert.Equal(t, "/login", w.Header().Get("Location"))
}

// TestGoogleOAuth_Callback_HappyPathNewTenant — full flow against a fake
// Google: new operator provisions an isolated tenant, lands on onboarding,
// and the session cookie is issued.
func TestGoogleOAuth_Callback_HappyPathNewTenant(t *testing.T) {
	h, app, db := newGoogleTestHarness(t, fakeGoogle(t, "fresh-op@google-test.local"))
	_ = app

	// obtain a real state cookie via Begin
	beginReq := httptest.NewRequest(http.MethodGet, "/auth/google", nil)
	beginW := httptest.NewRecorder()
	h.Begin(beginW, beginReq)
	var stateCookie *http.Cookie
	for _, c := range beginW.Result().Cookies() {
		if c.Name == googleStateCookie {
			stateCookie = c
		}
	}
	require.NotNil(t, stateCookie)

	cbURL := "/auth/google/callback?code=authcode&state=" + stateCookie.Value
	req := httptest.NewRequest(http.MethodGet, cbURL, nil)
	req.AddCookie(stateCookie)
	w := httptest.NewRecorder()
	h.Callback(w, req)

	require.Equal(t, http.StatusSeeOther, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, "/company/onboard", w.Header().Get("Location"), "new tenant admin must land in onboarding")

	var sessionCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			sessionCookie = c
		}
	}
	require.NotNil(t, sessionCookie, "session cookie must be issued")
	assert.NotEmpty(t, sessionCookie.Value)

	// user persisted with google identity + isolated tenant
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(1) FROM users u JOIN tenants t ON t.id = u.tenant_id
		 WHERE u.email = ? AND u.google_sub = 'google-sub-1' AND u.auth_provider = 'google'`,
		"fresh-op@google-test.local").Scan(&count))
	assert.Equal(t, 1, count)

	// state cookie cleared
	var cleared bool
	for _, c := range w.Result().Cookies() {
		if c.Name == googleStateCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	assert.True(t, cleared, "g_state must be cleared after use")
}
