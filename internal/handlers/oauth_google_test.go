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
	"strings"
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

func newGoogleDBGoogleTest(t *testing.T) *sql.DB {
	t.Helper()
	if cwd, _ := os.Getwd(); filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	db, err := sql.Open("sqlite", "file:tmp/google_oauth_spec_test.db")
	require.NoError(t, err)
	require.NoError(t, db.Ping())

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "db/migrations"))
	_, _ = db.Exec(`DELETE FROM users WHERE email LIKE '%@google-spec-test.local'`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newGoogleTestHarnessGoogleSpec(t *testing.T, oauthCfg *auth.OAuthConfig) (*GoogleOAuthHandlers, *App, *sql.DB) {
	t.Helper()
	tmpl, err := parseTemplates(&mockAuthSvc{})
	require.NoError(t, err)

	cfg := &config.Config{AppEnv: "development", CookieSecure: false}
	db := newGoogleDBGoogleTest(t)
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
		_, _ = db.Exec(`DELETE FROM users WHERE email LIKE '%@google-spec-test.local'`)
	})
	return h, app, db
}

func fakeGoogleServer(t *testing.T, id, email, name, picture string, verified bool) *auth.OAuthConfig {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = r.ParseForm()
		if r.FormValue("code") == "bad-code" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"valid-google-access-token","token_type":"Bearer","expires_in":3600}`))
	})

	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		payload, _ := json.Marshal(map[string]interface{}{
			"id":             id,
			"email":          email,
			"verified_email": verified,
			"name":           name,
			"picture":        picture,
		})
		_, _ = w.Write(payload)
	})

	cfg := auth.NewGoogleOAuthConfig("google-client-id-xyz", "google-client-secret-123", "https://avandab.local/auth/google/callback")
	cfg.TokenURL = srv.URL + "/token"
	cfg.UserInfoURL = srv.URL + "/userinfo"
	return cfg
}

func TestGoogleOAuth_ConsentRedirect_ScopesAndState(t *testing.T) {
	fakeCfg := fakeGoogleServer(t, "gid-1", "user@google-spec-test.local", "Test User", "https://img/1.png", true)
	h, _, _ := newGoogleTestHarnessGoogleSpec(t, fakeCfg)

	req := httptest.NewRequest(http.MethodGet, "/auth/google", nil)
	w := httptest.NewRecorder()
	h.Begin(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	loc := w.Header().Get("Location")
	assert.Contains(t, loc, "https://accounts.google.com/o/oauth2/v2/auth")
	assert.Contains(t, loc, "client_id=google-client-id-xyz")
	assert.Contains(t, loc, "scope=openid+email+profile")
	assert.Contains(t, loc, "state=")

	var stateCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == googleStateCookie {
			stateCookie = c
		}
	}
	require.NotNil(t, stateCookie, "oauth_state cookie must be set")
	assert.True(t, stateCookie.HttpOnly)
	assert.NotEmpty(t, stateCookie.Value)
}

func TestGoogleOAuth_CSRFRejection_StateMismatch(t *testing.T) {
	fakeCfg := fakeGoogleServer(t, "gid-1", "user@google-spec-test.local", "Test User", "", true)
	h, _, _ := newGoogleTestHarnessGoogleSpec(t, fakeCfg)

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=good-code&state=forged-state", nil)
	req.AddCookie(&http.Cookie{Name: googleStateCookie, Value: "legit-state"})
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

func TestGoogleOAuth_CSRFRejection_MissingCookie(t *testing.T) {
	fakeCfg := fakeGoogleServer(t, "gid-1", "user@google-spec-test.local", "Test User", "", true)
	h, _, _ := newGoogleTestHarnessGoogleSpec(t, fakeCfg)

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=good-code&state=some-state", nil)
	w := httptest.NewRecorder()
	h.Callback(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))
}

func TestGoogleOAuth_RejectsUnverifiedEmail(t *testing.T) {
	fakeCfg := fakeGoogleServer(t, "gid-unverified", "unverified@google-spec-test.local", "Unverified User", "", false)
	h, _, _ := newGoogleTestHarnessGoogleSpec(t, fakeCfg)

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

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=good-code&state="+stateCookie.Value, nil)
	req.AddCookie(stateCookie)
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
	assert.Contains(t, flash.Value, "not verified")
}

func TestGoogleOAuth_NewUser_ProvisionsIsolatedTenantAndAdmin(t *testing.T) {
	fakeCfg := fakeGoogleServer(t, "gid-fresh-1", "fresh-owner@google-spec-test.local", "Fresh Owner", "https://pic.url", true)
	h, _, db := newGoogleTestHarnessGoogleSpec(t, fakeCfg)

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

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=auth-code-ok&state="+stateCookie.Value, nil)
	req.AddCookie(stateCookie)
	w := httptest.NewRecorder()
	h.Callback(w, req)

	require.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/company/onboard", w.Header().Get("Location"), "new tenant admin must redirect to /company/onboard")

	var sessionCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			sessionCookie = c
		}
	}
	require.NotNil(t, sessionCookie, "session cookie must be set on signup")
	assert.NotEmpty(t, sessionCookie.Value)

	var tenantID string
	var roleID int64
	var googleSub, authProvider string
	err := db.QueryRow(
		`SELECT u.tenant_id, u.role_id, u.google_sub, u.auth_provider 
		 FROM users u WHERE u.email = ?`,
		"fresh-owner@google-spec-test.local",
	).Scan(&tenantID, &roleID, &googleSub, &authProvider)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(tenantID, "tenant_"), "isolated tenant ID must start with tenant_")
	assert.Equal(t, int64(6), roleID, "new user must have RoleOrgAdmin (role_id = 6), never platform RoleAdmin")
	assert.Equal(t, "gid-fresh-1", googleSub)
	assert.Equal(t, "google", authProvider)

	var tenantCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(1) FROM tenants WHERE id = ?`, tenantID).Scan(&tenantCount))
	assert.Equal(t, 1, tenantCount, "tenant record must exist in tenants table")
}

func TestGoogleOAuth_ExistingUser_LogsInSuccessfully(t *testing.T) {
	email := "existing-admin@google-spec-test.local"
	fakeCfg := fakeGoogleServer(t, "gid-existing-99", email, "Existing Admin", "", true)
	h, app, db := newGoogleTestHarnessGoogleSpec(t, fakeCfg)

	// Seed existing tenant and user
	_, err := db.Exec(`INSERT OR REPLACE INTO tenants (id, name, status, created_at, updated_at) VALUES ('tenant_seeded_1', 'Seeded Fleet', 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	hashed, err := auth.HashPassword("Str0ngP@ssw0rd!")
	require.NoError(t, err)
	_, err = db.Exec(
		`INSERT OR REPLACE INTO users (id, email, password_hash, name, role_id, tenant_id, status, created_at, updated_at) 
		 VALUES ('u_seeded_1', ?, ?, 'Existing Admin', 1, 'tenant_seeded_1', 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		email, hashed,
	)
	require.NoError(t, err)
	// Seed company settings so it redirects to /dashboard
	_, _ = db.Exec(`INSERT OR REPLACE INTO company_settings (id, tenant_id, company_name, created_at, updated_at) VALUES ('cs_1', 'tenant_seeded_1', 'Seeded Fleet Inc', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)

	_ = app

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

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=auth-code-ok&state="+stateCookie.Value, nil)
	req.AddCookie(stateCookie)
	w := httptest.NewRecorder()
	h.Callback(w, req)

	require.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/dashboard", w.Header().Get("Location"), "configured existing user should land on /dashboard")

	var sessionCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			sessionCookie = c
		}
	}
	require.NotNil(t, sessionCookie)

	var googleSub, authProvider string
	require.NoError(t, db.QueryRow(`SELECT google_sub, auth_provider FROM users WHERE email = ?`, email).Scan(&googleSub, &authProvider))
	assert.Equal(t, "gid-existing-99", googleSub)
	assert.Equal(t, "google", authProvider)
}

func TestGoogleOAuth_Disabled_DegradesGracefully(t *testing.T) {
	h, _, _ := newGoogleTestHarnessGoogleSpec(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/auth/google", nil)
	w := httptest.NewRecorder()
	h.Begin(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))

	cbReq := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=abc&state=xyz", nil)
	cbW := httptest.NewRecorder()
	h.Callback(cbW, cbReq)

	assert.Equal(t, http.StatusSeeOther, cbW.Code)
	assert.Equal(t, "/login", cbW.Header().Get("Location"))
}
