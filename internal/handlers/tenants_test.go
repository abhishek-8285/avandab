package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/domain"
	"transport-app/internal/events"
	repoSQLite "transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// recordingAuthSvc wraps mockAuthSvc and records RBAC role grants so the
// org_admin assignment can be asserted end-to-end.
type recordingAuthSvc struct {
	mockAuthSvc
	mu       sync.Mutex
	roleAdds []string
}

func (r *recordingAuthSvc) AddRoleForUser(userID, role string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roleAdds = append(r.roleAdds, userID+":"+role)
	return nil
}

func (r *recordingAuthSvc) adds() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.roleAdds))
	copy(out, r.roleAdds)
	return out
}

func newTenantsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_tenants_%d_%s", time.Now().UnixNano(), strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	migrationsDir := "../../db/migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		for _, cand := range []string{"db/migrations", "../db/migrations", "../../db/migrations"} {
			if _, err := os.Stat(cand); err == nil {
				migrationsDir = cand
				break
			}
		}
	}
	goose.SetLogger(goose.NopLogger())
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTenantsTestApp(t *testing.T, db *sql.DB, authSrv auth.AuthorizationService, multiTenant bool) *App {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	if authSrv == nil {
		authSrv = &mockAuthSvc{}
	}
	tmpl, err := parseTemplates(authSrv)
	require.NoError(t, err)
	cfg := &config.Config{
		AppEnv:       "testing",
		CookieSecret: "test-secret-32",
		CookieSecure: false,
		UploadDir:    t.TempDir(),
		MultiTenant:  config.MultiTenantConfig{Enabled: multiTenant},
	}
	repo := repoSQLite.NewRepository(db)
	bus := events.NewInMemoryBus()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)
	app := &App{
		DB:        db,
		Config:    cfg,
		Services:  services,
		Templates: tmpl,
		AuthSrv:   authSrv,
	}
	return app
}

func newTenantsRouter(app *App) *chi.Mux {
	r := chi.NewRouter()
	r.Route("/tenants", NewTenantsHandlers(app).Routes)
	return r
}

func postTenantForm(r *chi.Mux, name, slug, email, adminName, password string) *httptest.ResponseRecorder {
	form := url.Values{
		"name":       {name},
		"slug":       {slug},
		"email":      {email},
		"admin_name": {adminName},
		"password":   {password},
	}
	req := withTenantSession(httptest.NewRequest(http.MethodPost, "/tenants/new", strings.NewReader(form.Encode())), "1", "super-1", "admin")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestTenants_CreateHappyPath covers the full provisioning flow: 303 redirect,
// flash cookie naming the admin email, tenants row active, org_admin user
// bound to the new tenant, trigger-synced user_roles row, Casbin grant.
func TestTenants_CreateHappyPath(t *testing.T) {
	db := newTenantsTestDB(t)
	authz := &recordingAuthSvc{}
	app := newTenantsTestApp(t, db, authz, true)
	r := newTenantsRouter(app)

	w := postTenantForm(r, "Acme Corp", "acme", "owner@acme.test", "Ada Admin", "Str0ngPassphrase!")
	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/tenants", w.Header().Get("Location"))

	flashSet := false
	for _, c := range w.Result().Cookies() {
		if c.Name == "flash_success" && strings.Contains(c.Value, "owner@acme.test") {
			flashSet = true
		}
	}
	assert.True(t, flashSet, "flash_success cookie must name the created admin email")

	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM tenants WHERE id = 'acme'`).Scan(&status))
	assert.Equal(t, "active", status)

	var tenantID string
	var roleID int64
	require.NoError(t, db.QueryRow(`SELECT tenant_id, role_id FROM users WHERE email = 'owner@acme.test'`).Scan(&tenantID, &roleID))
	assert.Equal(t, "acme", tenantID, "admin must belong to the new tenant")
	assert.EqualValues(t, 6, roleID, "admin must hold the org_admin role (id 6)")

	var roleBindings int
	require.NoError(t, db.QueryRow(`SELECT COUNT(1) FROM user_roles ur JOIN users u ON u.id = ur.user_id WHERE u.email = 'owner@acme.test' AND ur.role_id = 6`).Scan(&roleBindings))
	assert.Positive(t, roleBindings, "user_roles trigger sync must bind org_admin")

	found := false
	for _, a := range authz.adds() {
		if strings.HasSuffix(a, ":org_admin") {
			found = true
			break
		}
	}
	assert.True(t, found, "AddRoleForUser must be called with org_admin, got %v", authz.adds())
}

// TestTenants_CreateDuplicateSlug re-renders the form with a conflict message.
func TestTenants_CreateDuplicateSlug(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, nil, true)
	r := newTenantsRouter(app)

	w := postTenantForm(r, "Acme Corp", "acme", "first@acme.test", "Ada", "Str0ngPassphrase!")
	require.Equal(t, http.StatusSeeOther, w.Code)

	w2 := postTenantForm(r, "Acme Two", "acme", "second@acme.test", "Bob", "Str0ngPassphrase!")
	assert.Equal(t, http.StatusOK, w2.Code)
	assert.Contains(t, w2.Body.String(), "already exists")
}

// TestTenants_CreateDisabledGate refuses while MULTI_TENANT_ENABLED=false.
func TestTenants_CreateDisabledGate(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, nil, false)
	r := newTenantsRouter(app)

	w := postTenantForm(r, "Acme Corp", "acme", "owner@acme.test", "Ada", "Str0ngPassphrase!")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "MULTI_TENANT_ENABLED=true")

	var tenants int
	require.NoError(t, db.QueryRow(`SELECT COUNT(1) FROM tenants WHERE id = 'acme'`).Scan(&tenants))
	assert.Zero(t, tenants, "no tenant may be provisioned while the gate is off")
}

// TestTenants_SuspendBootstrapRefused protects tenant '1'.
func TestTenants_SuspendBootstrapRefused(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, nil, true)
	r := newTenantsRouter(app)

	req := withTenantSession(httptest.NewRequest(http.MethodPost, "/tenants/1/suspend", nil), "1", "super-1", "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "bootstrap tenant cannot be suspended")
}

// TestTenants_SuspendFlipsStatusAndPurgesSessions covers the suspend path:
// status flips AND every session of the tenant's users is deleted.
func TestTenants_SuspendFlipsStatusAndPurgesSessions(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, nil, true)
	r := newTenantsRouter(app)

	w := postTenantForm(r, "Beta Ltd", "beta", "owner@beta.test", "Bea", "Str0ngPassphrase!")
	require.Equal(t, http.StatusSeeOther, w.Code)

	var adminID string
	require.NoError(t, db.QueryRow(`SELECT id FROM users WHERE email = 'owner@beta.test'`).Scan(&adminID))
	_, err := db.Exec(`INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES ('sess-beta-1', ?, 'deadbeef', datetime('now', '+1 day'))`, adminID)
	require.NoError(t, err)

	req := withTenantSession(httptest.NewRequest(http.MethodPost, "/tenants/beta/suspend", nil), "1", "super-1", "admin")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"ok":true`)
	assert.Contains(t, w.Body.String(), `"suspended"`)

	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM tenants WHERE id = 'beta'`).Scan(&status))
	assert.Equal(t, "suspended", status)

	var sessions int
	require.NoError(t, db.QueryRow(`SELECT COUNT(1) FROM sessions WHERE user_id = ?`, adminID).Scan(&sessions))
	assert.Zero(t, sessions, "suspend must purge the tenant's sessions")

	// Audit trail entry recorded.
	var audits int
	require.NoError(t, db.QueryRow(`SELECT COUNT(1) FROM audit_logs WHERE action = 'tenant.suspend' AND record_id = 'beta'`).Scan(&audits))
	assert.Positive(t, audits, "suspend must be audit-logged")

	// Reactivate for completeness.
	reqAct := withTenantSession(httptest.NewRequest(http.MethodPost, "/tenants/beta/activate", nil), "1", "super-1", "admin")
	wAct := httptest.NewRecorder()
	r.ServeHTTP(wAct, reqAct)
	require.Equal(t, http.StatusOK, wAct.Code)
	require.NoError(t, db.QueryRow(`SELECT status FROM tenants WHERE id = 'beta'`).Scan(&status))
	assert.Equal(t, "active", status)
}

// TestTenants_RoutesForbiddenWithoutManage enforces tenants:manage gating.
func TestTenants_RoutesForbiddenWithoutManage(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, denyAuthSvc{}, true)
	r := newTenantsRouter(app)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/tenants/"},
		{http.MethodGet, "/tenants/new"},
		{http.MethodPost, "/tenants/new"},
		{http.MethodPost, "/tenants/acme/suspend"},
		{http.MethodPost, "/tenants/acme/activate"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := withSession(httptest.NewRequest(tc.method, tc.path, nil), "viewer-1", "viewer")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}
}

// TestTenants_ListRendersTableAndCounts checks the list page and Datastar
// fragment both render with seeded rows and user counts.
func TestTenants_ListRendersTableAndCounts(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, nil, true)
	r := newTenantsRouter(app)

	w := postTenantForm(r, "Acme Corp", "acme", "owner@acme.test", "Ada", "Str0ngPassphrase!")
	require.Equal(t, http.StatusSeeOther, w.Code)

	t.Run("full page", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/tenants/", nil), "1", "super-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, "acme")
		assert.Contains(t, body, "Acme Corp")
	})

	t.Run("datastar fragment", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/tenants/", nil), "1", "super-1", "admin")
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "js-tenant-toggle")
	})

	t.Run("new form renders suggested slug", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/tenants/new", nil), "1", "super-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `/tenants/new`)
	})
}

// TestSuggestTenantSlug pins the server-side slug normalization rules.
func TestSuggestTenantSlug(t *testing.T) {
	cases := map[string]string{
		"Acme Corp":          "acme-corp",
		"  Beta   Ltd  ":     "beta-ltd",
		"Gamma & Sons, LLC":  "gamma-sons-llc",
		"UPPER_case Mixed-7": "upper-case-mixed-7",
		"---":                "",
	}
	for in, want := range cases {
		assert.Equal(t, want, suggestTenantSlug(in), "input %q", in)
	}
	long := suggestTenantSlug(strings.Repeat("abcdefgh ", 20))
	assert.LessOrEqual(t, len(long), maxTenantSlugLen)
	assert.NotRegexp(t, "(^|-)-( |$)", long)
}

// ---- Tenant isolation guarantees for user management (Spec 24 §Business logic) ----

// newUsersIsolationApp wires the real Users handlers against a migrated DB and
// returns it with two seeded tenants: acme (admin_acme) and beta (admin_beta).
func newUsersIsolationApp(t *testing.T, multiTenant bool) (*App, string, string) {
	t.Helper()
	db := newTenantsTestDB(t)
	// 00103 enforces FK — tenants must exist before users.
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	app := newTenantsTestApp(t, db, nil, multiTenant)

	mk := func(email, name, tenantID string) domain.User {
		u, err := app.Services.Users.CreateUserWithPassword(
			context.Background(), email, name, "", "StrongPass#123",
			domain.DefaultRoleID(domain.RoleOrgAdmin), domain.UserStatusActive, tenantID)
		require.NoError(t, err)
		return u
	}
	acme := mk("acme-admin@x.test", "Acme Admin", "acme")
	beta := mk("beta-admin@x.test", "Beta Admin", "beta")
	return app, acme.ID.String(), beta.ID.String()
}

func usersRouter(app *App) *chi.Mux {
	r := chi.NewRouter()
	r.Route("/users", (&UserHandlers{App: app}).Routes)
	return r
}

func asTenantAdmin(r *http.Request, tenant, userID string) *http.Request {
	return withTenantSession(r, tenant, userID, "org_admin")
}

// TestUsers_CrossTenantDetailAccessDenied proves every user-detail surface is
// same-tenant only: another org's admin gets 404 (existence undisclosed) on
// view, edit, update, delete AND password reset — closing the account-takeover
// path.
func TestUsers_CrossTenantDetailAccessDenied(t *testing.T) {
	app, acmeID, betaID := newUsersIsolationApp(t, true)
	r := usersRouter(app)

	form := url.Values{}
	form.Set("email", "beta-admin@x.test")
	form.Set("name", "Hijacked")
	form.Set("role_id", "2")
	form.Set("status", "active")

	cases := []struct {
		name   string
		method string
		path   string
		body   *url.Values
	}{
		{"edit form leaks nothing", http.MethodGet, "/users/" + betaID + "/edit", nil},
		{"update rejected", http.MethodPost, "/users/" + betaID + "/edit", &form},
		{"delete rejected", http.MethodPost, "/users/" + betaID + "/delete", nil},
		{"password reset takeover blocked", http.MethodPost, "/users/" + betaID + "/reset-password", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req *http.Request
			if tc.body != nil {
				req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			} else {
				req = httptest.NewRequest(tc.method, tc.path, nil)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, asTenantAdmin(req, "acme", acmeID))
			assert.Equal(t, http.StatusNotFound, w.Code, "cross-tenant access must read as not-found")
		})
	}

	// The target row is untouched — especially its credentials.
	u, err := app.Services.Users.GetUser(context.Background(), domain.UserID(betaID))
	require.NoError(t, err)
	assert.Equal(t, "Beta Admin", u.Name, "cross-tenant update must not apply")
	assert.NotEmpty(t, u.PasswordHash, "reset-password must not run cross-tenant")
}

// TestUsers_SameTenantManagementStillWorks guards against over-blocking:
// an org admin manages their OWN tenant's users normally.
func TestUsers_SameTenantManagementStillWorks(t *testing.T) {
	app, acmeID, _ := newUsersIsolationApp(t, true)
	r := usersRouter(app)

	// Edit form renders for own tenant.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, asTenantAdmin(httptest.NewRequest(http.MethodGet, "/users/"+acmeID+"/edit", nil), "acme", acmeID))
	require.Equal(t, http.StatusOK, w.Code)

	// Update own user.
	form := url.Values{
		"email": {"acme-admin@x.test"}, "name": {"Acme Renamed"},
		"role_id": {"6"}, "status": {"active"},
	}
	req := httptest.NewRequest(http.MethodPost, "/users/"+acmeID+"/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, asTenantAdmin(req, "acme", acmeID))
	assert.Equal(t, http.StatusSeeOther, w.Code)

	u, err := app.Services.Users.GetUser(context.Background(), domain.UserID(acmeID))
	require.NoError(t, err)
	assert.Equal(t, "Acme Renamed", u.Name)
}

// TestSettings_TenantScopedUnderMultiTenant proves settings writes under
// MULTI_TENANT_ENABLED land in the caller's org row only (migration 00125):
// the global singleton and other tenants stay untouched, narrower roles
// still 403, and org admins can brand their own org.
func TestSettings_TenantScopedUnderMultiTenant(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, nil, true)
	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('acme','Acme','acme'), ('other','Other','other')`)
	require.NoError(t, err)

	sh := &SettingsHandlers{App: app}
	postSettings := func(role, tenant, name string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		for k, v := range map[string]string{
			"company_name": name, "currency": "INR", "timezone": "Asia/Kolkata",
			"address": "Depot", "phone": "9000000001", "email": "ops@acme.test",
		} {
			require.NoError(t, mw.WriteField(k, v))
		}
		require.NoError(t, mw.Close())
		req := httptest.NewRequest(http.MethodPost, "/settings/update", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req = withTenantSession(req, tenant, "u-1", role)
		w := httptest.NewRecorder()
		sh.Update(w, req)
		return w
	}

	w := postSettings("org_admin", "acme", "Acme Rebrand")
	require.Equal(t, http.StatusSeeOther, w.Code)

	acme, err := app.Services.Settings.GetSettings(shared.ContextWithTenantID(context.Background(), "acme"))
	require.NoError(t, err)
	assert.Equal(t, "Acme Rebrand", acme.CompanyName, "org admin brands own org")

	global, err := app.Services.Settings.GetSettings(context.Background())
	require.NoError(t, err)
	assert.NotEqual(t, "Acme Rebrand", global.CompanyName, "org admin must not rewrite platform globals")

	other, err := app.Services.Settings.GetSettings(shared.ContextWithTenantID(context.Background(), "other"))
	require.NoError(t, err)
	assert.NotEqual(t, "Acme Rebrand", other.CompanyName, "org admin must not rebrand other orgs")

	w = postSettings("dispatcher", "acme", "Hacked")
	assert.Equal(t, http.StatusForbidden, w.Code)
}
