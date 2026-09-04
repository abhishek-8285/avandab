package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/shared"
)

// actorContext builds a request context carrying tenant + session role,
// mimicking an authenticated browser session after middleware ran.
func actorContext(role string) context.Context {
	ctx := shared.ContextWithTenantID(context.Background(), "1")
	return context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
		UserID: "actor-1",
		Role:   role,
		Name:   "Actor",
	})
}

func userCreateForm(roleID string) *http.Request {
	form := url.Values{
		"email":    {"victim@example.com"},
		"name":     {"Victim"},
		"phone":    {"9999999999"},
		"password": {"Test1234"},
		"role_id":  {roleID},
		"status":   {"active"},
	}
	req := httptest.NewRequest(http.MethodPost, "/users/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// An org_admin (paying customer's own admin) must NOT be able to mint a
// global admin. Regression test for the privilege-escalation hole where
// role_id came straight from the form with no actor check.
func TestUsers_Create_OrgAdminCannotGrantAdmin(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, &mockAuthSvc{}, false)
	h := &UserHandlers{App: app}

	req := userCreateForm("1").WithContext(actorContext("org_admin"))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM users WHERE email = 'victim@example.com'`).Scan(&count))
	assert.Equal(t, 0, count, "escalated admin must not be created")
}

// An acting admin CAN grant the admin role (legitimate flow).
func TestUsers_Create_AdminCanGrantAdmin(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, &mockAuthSvc{}, false)
	h := &UserHandlers{App: app}

	req := userCreateForm("1").WithContext(actorContext("admin"))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	var roleID int64
	require.NoError(t, db.QueryRow(`SELECT role_id FROM users WHERE email = 'victim@example.com'`).Scan(&roleID))
	assert.Equal(t, domain.DefaultRoleID(domain.RoleAdmin), roleID)
}

// Non-admin roles stay grantable by org_admin (normal team management).
func TestUsers_Create_OrgAdminCanGrantDispatcher(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, &mockAuthSvc{}, false)
	h := &UserHandlers{App: app}

	req := userCreateForm("2").WithContext(actorContext("org_admin"))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	var roleID int64
	require.NoError(t, db.QueryRow(`SELECT role_id FROM users WHERE email = 'victim@example.com'`).Scan(&roleID))
	assert.Equal(t, int64(2), roleID)
}

// Non-admin actors must not see the global admin role in the New dropdown.
// Backend guard stays source of truth; this only avoids a 403-by-surprise.
func TestUsers_New_OrgAdminCannotSeeAdminRole(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, &mockAuthSvc{}, false)
	h := &UserHandlers{App: app}

	req := httptest.NewRequest(http.MethodGet, "/users/new", nil).WithContext(actorContext("org_admin"))
	rec := httptest.NewRecorder()
	h.New(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, ">admin<", "org_admin must not see global admin option")
	assert.Contains(t, body, ">org_admin<", "own manageable roles stay visible")
}

// Acting admin sees the full role list including global admin.
func TestUsers_New_AdminSeesAdminRole(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, &mockAuthSvc{}, false)
	h := &UserHandlers{App: app}

	req := httptest.NewRequest(http.MethodGet, "/users/new", nil).WithContext(actorContext("admin"))
	rec := httptest.NewRecorder()
	h.New(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), ">admin<", "admin keeps full list")
}

// Same filtering applies on the Edit form.
func TestUsers_Edit_OrgAdminCannotSeeAdminRole(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, &mockAuthSvc{}, false)
	h := &UserHandlers{App: app}

	ctx := shared.ContextWithTenantID(context.Background(), "1")
	seeded, err := app.Services.Users.CreateUserWithPassword(
		ctx, "staff@example.com", "Staff", "8888888888", "Test1234",
		domain.DefaultRoleID(domain.RoleViewer), domain.UserStatusActive, "1")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/users/"+seeded.ID.String()+"/edit", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", seeded.ID.String())
	req = req.WithContext(context.WithValue(actorContext("org_admin"), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.Edit(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), ">admin<", "org_admin must not see global admin option on edit")
}

// Promoting an existing user to admin is blocked the same way.
func TestUsers_Update_OrgAdminCannotPromoteToAdmin(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, &mockAuthSvc{}, false)
	h := &UserHandlers{App: app}

	ctx := shared.ContextWithTenantID(context.Background(), "1")
	seeded, err := app.Services.Users.CreateUserWithPassword(
		ctx, "staff@example.com", "Staff", "8888888888", "Test1234",
		domain.DefaultRoleID(domain.RoleViewer), domain.UserStatusActive, "1")
	require.NoError(t, err)

	form := url.Values{
		"email":   {"staff@example.com"},
		"name":    {"Staff"},
		"phone":   {"8888888888"},
		"role_id": {"1"},
		"status":  {"active"},
	}
	req := httptest.NewRequest(http.MethodPost, "/users/"+seeded.ID.String()+"/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", seeded.ID.String())
	req = req.WithContext(context.WithValue(actorContext("org_admin"), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	var roleID int64
	require.NoError(t, db.QueryRow(`SELECT role_id FROM users WHERE email = 'staff@example.com'`).Scan(&roleID))
	assert.Equal(t, domain.DefaultRoleID(domain.RoleViewer), roleID, "role must be unchanged")
}
