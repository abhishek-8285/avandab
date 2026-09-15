package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

func gateAuthedRequest(t *testing.T, method, path, tenantID string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
		UserID: "u-gate-1", Role: "org_admin", Name: "Gate Owner",
	})
	ctx = shared.ContextWithTenantID(ctx, shared.TenantID(tenantID))
	return req.WithContext(ctx)
}

// Ratchet: end-to-end company-gate ordering with the real settings service.
// tenant-zz is seeded with no company profile (incomplete). A fresh owner
// must reach /consent BEFORE completing company onboarding (DPDP consent is
// a legal prerequisite), while /dashboard stays gated. Fails if /consent is
// re-gated or the gate stops enforcing.
func TestCompanyGate_FreshOwnerReachesConsentBeforeOnboarding(t *testing.T) {
	app := newRegisterTestApp(t)
	authH := NewAuthHandlers(app)
	gate := middleware.RequireCompanyCompliance(app.Services.Settings)

	req := gateAuthedRequest(t, http.MethodGet, "/consent", "tenant-zz")
	rr := httptest.NewRecorder()
	gate(http.HandlerFunc(authH.ConsentNoticePage)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "fresh owner must reach consent notice before onboarding")
	assert.Contains(t, rr.Body.String(), "Your data")

	req = gateAuthedRequest(t, http.MethodGet, "/dashboard", "tenant-zz")
	rr = httptest.NewRecorder()
	gate(http.HandlerFunc(app.Dashboard.Index)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusSeeOther, rr.Code, "dashboard stays gated until onboarding")
	assert.Equal(t, "/company/onboard", rr.Header().Get("Location"))
}
