package handlers

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

// Regression for the platform-leak report: every public signup must mint an
// ORG admin (role 6), never the platform admin (role 1). A tenant owner must
// not see /tenants, suspend orgs, or mint global admins — while keeping full
// power inside their own org (onboarding, own settings).
func TestPlatform_SelfRegisterYieldsOrgAdminWithoutPlatformPowers(t *testing.T) {
	app := newRegisterTestApp(t)
	authz := &recordingAuthSvc{}
	app.AuthSrv = authz

	rr := postRegisterWithCompany(app, "Plat Owner", "owner@plat.test", "strong-pass-1", "Plat Fleet")
	require.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Equal(t, "/company/onboard", rr.Header().Get("Location"))

	var userID string
	var roleID int
	var tenantID string
	require.NoError(t, app.DB.QueryRow(
		`SELECT id, role_id, tenant_id FROM users WHERE email = 'owner@plat.test'`,
	).Scan(&userID, &roleID, &tenantID))
	assert.Equal(t, 6, roleID, "public signup must mint org_admin (6), never platform admin (1)")

	var sawOrgAdmin, sawAdmin bool
	for _, a := range authz.adds() {
		if strings.HasSuffix(a, ":org_admin") {
			sawOrgAdmin = true
		}
		if strings.HasSuffix(a, ":admin") {
			sawAdmin = true
		}
	}
	assert.True(t, sawOrgAdmin, "Casbin must bind org_admin, got %v", authz.adds())
	assert.False(t, sawAdmin, "Casbin must never bind platform admin on signup, got %v", authz.adds())

	// Session cookie must carry org_admin, not admin.
	sessReq := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rr.Result().Cookies() {
		sessReq.AddCookie(c)
	}
	sess, ok := app.AuthStore.ValidateSession(sessReq)
	require.True(t, ok, "signup must issue a valid session")
	assert.Equal(t, "org_admin", sess.Role)

	// Real Casbin over seeded role_permissions: no platform powers, full
	// org powers.
	enforcer, err := auth.NewCasbinAuthorizationService(app.DB)
	require.NoError(t, err)
	require.NoError(t, enforcer.AddRoleForUser(userID, "org_admin"))
	assert.False(t, enforcer.Can(userID, "tenants", "manage"), "tenant owner must not manage tenants")
	assert.True(t, enforcer.Can(userID, "users", "create"), "tenant owner keeps team management")
	assert.True(t, enforcer.Can(userID, "settings", "update"), "tenant owner keeps own settings")

	// Control: a platform admin keeps tenants:manage.
	require.NoError(t, enforcer.AddRoleForUser("platform-1", "admin"))
	assert.True(t, enforcer.Can("platform-1", "tenants", "manage"))
}

// Tenant org admins must pass the onboarding/settings gates for their own org;
// narrower roles still bounce.
func TestPlatform_OrgAdminKeepsOwnOrgGates(t *testing.T) {
	app := newRegisterTestApp(t)
	app.Config.MultiTenant.Enabled = true
	_, err := app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-plat', 'Plat Fleet', 'plat-fleet')`)
	require.NoError(t, err)
	sh := &SettingsHandlers{App: app}

	orgCtx := func(role string) context.Context {
		ctx := shared.ContextWithTenantID(context.Background(), "tenant-plat")
		return context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "u-1", Role: role, Name: "U"})
	}

	t.Run("onboard page allows org_admin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/company/onboard", nil).WithContext(orgCtx("org_admin"))
		w := httptest.NewRecorder()
		sh.OnboardPage(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("onboard page rejects dispatcher", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/company/onboard", nil).WithContext(orgCtx("dispatcher"))
		w := httptest.NewRecorder()
		sh.OnboardPage(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
	})

	t.Run("settings update allows org_admin", func(t *testing.T) {
		req := multipartSettingsPost(t, orgCtx("org_admin"), map[string]string{
			"company_name": "Plat Fleet", "currency": "INR", "timezone": "Asia/Kolkata",
			"address": "Depot 1", "phone": "9000000001", "email": "ops@plat.test",
		})
		w := httptest.NewRecorder()
		sh.Update(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
	})

	t.Run("settings update rejects dispatcher", func(t *testing.T) {
		req := multipartSettingsPost(t, orgCtx("dispatcher"), map[string]string{
			"company_name": "Hacked", "currency": "INR", "timezone": "Asia/Kolkata",
			"address": "X", "phone": "9000000002", "email": "x@plat.test",
		})
		w := httptest.NewRecorder()
		sh.Update(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

// multipartSettingsPost builds a multipart settings form — Update requires
// multipart (logo upload), urlencoded posts 400 at ParseMultipartForm.
func multipartSettingsPost(t *testing.T, ctx context.Context, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		require.NoError(t, mw.WriteField(k, v))
	}
	require.NoError(t, mw.Close())
	req := httptest.NewRequest(http.MethodPost, "/settings/update", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req.WithContext(ctx)
}
