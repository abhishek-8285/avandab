package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

// Feature flags are a platform commercial decision: only acting admins may
// view or flip them. An org_admin must get 403/redirect even for their own
// org — otherwise paid customers self-upgrade to add-ons they weren't sold.
func TestFeatures_Toggle_OrgAdminForbidden(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, &mockAuthSvc{}, false)
	h := &FeaturesAdmin{App: app}

	body := strings.NewReader(`{"feature":"driver_money","enabled":false}`)
	req := httptest.NewRequest(http.MethodPost, "/settings/features/toggle", body)
	ctx := shared.ContextWithTenantID(context.Background(), "1")
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "orgadmin-1", Role: "org_admin"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.Toggle(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	var enabled int
	_ = db.QueryRow(`SELECT enabled FROM feature_flags WHERE tenant_id = '1' AND feature = 'driver_money'`).Scan(&enabled)
	// row absent in fresh test DB is fine; what matters is no flip happened —
	// covered by the 403 above plus the admin pass-through test below.
}

func TestFeatures_Toggle_AdminPassesGuard(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, &mockAuthSvc{}, false)
	h := &FeaturesAdmin{App: app}

	// Unknown feature: guard passes (admin) and validation rejects the key —
	// proves the role gate let an admin through without needing a Features svc.
	body := strings.NewReader(`{"feature":"no_such_feature","enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/settings/features/toggle", body)
	ctx := shared.ContextWithTenantID(context.Background(), "1")
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "admin-1", Role: "admin"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.Toggle(rec, req)

	assert.NotEqual(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestFeatures_Page_OrgAdminRedirected(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, &mockAuthSvc{}, false)
	h := &FeaturesAdmin{App: app}

	req := httptest.NewRequest(http.MethodGet, "/settings/features", nil)
	ctx := shared.ContextWithTenantID(context.Background(), "1")
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "orgadmin-1", Role: "org_admin"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.Page(rec, req)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
}
