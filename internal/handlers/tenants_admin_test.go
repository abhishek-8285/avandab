package handlers

// HTTP characterization for the platform tenant-console mutation endpoints
// (handlers-end-state ADR, Phase 2). The SQL moved to
// internal/tenant/application (covered by admin_test.go there); these cases pin
// the handler contract for the plan / trial / override actions plus the
// validation and RBAC boundaries that stay in the handler.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// postTenantAction sends an authenticated super-admin POST to one /tenants route.
func postTenantAction(t *testing.T, r http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := withTenantSession(httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode())), "1", "super-1", "admin")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestTenantsAdmin_PlanTrialOverrideEndpoints(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, nil, true)
	r := newTenantsRouter(app)

	w := postTenantForm(r, "Acme Corp", "acme", "owner@acme.test", "Ada Admin", "Str0ngPassphrase!")
	require.Equal(t, http.StatusSeeOther, w.Code)

	// Plan update: 303 + ACTIVE subscription carrying the provider link.
	w = postTenantAction(t, r, "/tenants/acme/plan", url.Values{
		"plan_id":                  {"SCALE"},
		"provider_subscription_id": {"rzp_acme_1"},
	})
	require.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/tenants", w.Header().Get("Location"))
	var planID, status, provider string
	require.NoError(t, db.QueryRow(`SELECT plan_id, status, COALESCE(provider_subscription_id, '') FROM tenant_subscriptions WHERE tenant_id = 'acme'`).Scan(&planID, &status, &provider))
	assert.Equal(t, "SCALE", planID)
	assert.Equal(t, "ACTIVE", status)
	assert.Equal(t, "rzp_acme_1", provider)

	// Missing plan_id defaults to GROWTH and keeps the Razorpay link.
	w = postTenantAction(t, r, "/tenants/acme/plan", url.Values{})
	require.Equal(t, http.StatusSeeOther, w.Code)
	require.NoError(t, db.QueryRow(`SELECT plan_id, status, COALESCE(provider_subscription_id, '') FROM tenant_subscriptions WHERE tenant_id = 'acme'`).Scan(&planID, &status, &provider))
	assert.Equal(t, "GROWTH", planID, "empty plan_id must default to GROWTH")
	assert.Equal(t, "rzp_acme_1", provider, "plan-only change must keep the payment-provider link")

	// Extend trial: back to TRIAL and roughly +14 days.
	w = postTenantAction(t, r, "/tenants/acme/extend-trial", url.Values{})
	require.Equal(t, http.StatusSeeOther, w.Code)
	var trialEnd string
	require.NoError(t, db.QueryRow(`SELECT status, trial_end FROM tenant_subscriptions WHERE tenant_id = 'acme'`).Scan(&status, &trialEnd))
	assert.Equal(t, "TRIAL", status)
	parsed, err := time.Parse(time.RFC3339, trialEnd)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().UTC().Add(14*24*time.Hour), parsed, 5*time.Minute)

	// Override: legacy id shape, value and reason persisted.
	w = postTenantAction(t, r, "/tenants/acme/override", url.Values{
		"entitlement_type": {"FEATURE"},
		"key_name":         {"max_vehicles"},
		"override_value":   {"100"},
		"reason":           {"contract"},
	})
	require.Equal(t, http.StatusSeeOther, w.Code)
	var overrideID, overrideVal, reason string
	require.NoError(t, db.QueryRow(`SELECT id, override_value, reason FROM tenant_entitlement_overrides WHERE tenant_id = 'acme' AND key_name = 'max_vehicles'`).Scan(&overrideID, &overrideVal, &reason))
	assert.Equal(t, "ovr_acme_max_vehicles", overrideID)
	assert.Equal(t, "100", overrideVal)
	assert.Equal(t, "contract", reason)

	// All three mutations are audit-logged.
	for _, action := range []string{"tenant.plan_update", "tenant.extend_trial", "tenant.override"} {
		var audits int
		require.NoError(t, db.QueryRow(`SELECT COUNT(1) FROM audit_logs WHERE action = ? AND record_id = 'acme'`, action).Scan(&audits))
		assert.Positive(t, audits, "action %q must be audit-logged", action)
	}
}

func TestTenantsAdmin_PlanAndOverrideAreTenantScoped(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, nil, true)
	r := newTenantsRouter(app)

	require.Equal(t, http.StatusSeeOther, postTenantForm(r, "Acme Corp", "acme", "owner@acme.test", "Ada", "Str0ngPassphrase!").Code)
	require.Equal(t, http.StatusSeeOther, postTenantForm(r, "Beta Ltd", "beta", "owner@beta.test", "Bea", "Str0ngPassphrase!").Code)

	require.Equal(t, http.StatusSeeOther, postTenantAction(t, r, "/tenants/acme/plan", url.Values{"plan_id": {"SCALE"}}).Code)
	require.Equal(t, http.StatusSeeOther, postTenantAction(t, r, "/tenants/acme/override", url.Values{"key_name": {"max_vehicles"}, "override_value": {"100"}}).Code)
	require.Equal(t, http.StatusSeeOther, postTenantAction(t, r, "/tenants/beta/override", url.Values{"key_name": {"max_vehicles"}, "override_value": {"200"}}).Code)

	// Only acme moved plans; beta keeps its provisioned trial subscription.
	var planID, status string
	require.NoError(t, db.QueryRow(`SELECT plan_id, status FROM tenant_subscriptions WHERE tenant_id = 'acme'`).Scan(&planID, &status))
	assert.Equal(t, "SCALE", planID)
	require.NoError(t, db.QueryRow(`SELECT plan_id, status FROM tenant_subscriptions WHERE tenant_id = 'beta'`).Scan(&planID, &status))
	assert.Equal(t, "STARTER", planID)
	assert.Equal(t, "TRIAL", status)

	// Overrides are per tenant and upsert in place per (tenant, type, key).
	require.Equal(t, http.StatusSeeOther, postTenantAction(t, r, "/tenants/acme/override", url.Values{"key_name": {"max_vehicles"}, "override_value": {"150"}}).Code)
	var acmeVal, betaVal string
	require.NoError(t, db.QueryRow(`SELECT override_value FROM tenant_entitlement_overrides WHERE tenant_id = 'acme'`).Scan(&acmeVal))
	require.NoError(t, db.QueryRow(`SELECT override_value FROM tenant_entitlement_overrides WHERE tenant_id = 'beta'`).Scan(&betaVal))
	assert.Equal(t, "150", acmeVal)
	assert.Equal(t, "200", betaVal)
	var acmeRows int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM tenant_entitlement_overrides WHERE tenant_id = 'acme'`).Scan(&acmeRows))
	assert.Equal(t, 1, acmeRows, "same (tenant, type, key) must upsert, not duplicate")
}

func TestTenantsAdmin_NewMutationRoutesForbiddenWithoutManage(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, denyAuthSvc{}, true)
	r := newTenantsRouter(app)

	for _, path := range []string{"/tenants/acme/plan", "/tenants/acme/extend-trial", "/tenants/acme/override"} {
		t.Run(path, func(t *testing.T) {
			req := withSession(httptest.NewRequest(http.MethodPost, path, nil), "viewer-1", "viewer")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}
}

func TestTenantsAdmin_CreateValidationMessages(t *testing.T) {
	db := newTenantsTestDB(t)
	app := newTenantsTestApp(t, db, nil, true)
	r := newTenantsRouter(app)

	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{
			name: "organization name",
			form: url.Values{"slug": {"acme"}, "email": {"a@acme.test"}, "admin_name": {"Ada"}, "password": {"Str0ngPassphrase!"}},
			want: "Organization Name is required",
		},
		{
			name: "admin email",
			form: url.Values{"name": {"Acme Corp"}, "slug": {"acme"}, "admin_name": {"Ada"}, "password": {"Str0ngPassphrase!"}},
			want: "Admin Email is required",
		},
		{
			name: "admin name",
			form: url.Values{"name": {"Acme Corp"}, "slug": {"acme"}, "email": {"a@acme.test"}, "password": {"Str0ngPassphrase!"}},
			want: "Admin Name is required",
		},
		{
			name: "admin password length",
			form: url.Values{"name": {"Acme Corp"}, "slug": {"acme"}, "email": {"a@acme.test"}, "admin_name": {"Ada"}, "password": {"short"}},
			want: "at least 12 characters",
		},
		{
			name: "slug required after normalization",
			form: url.Values{"name": {"Acme Corp"}, "slug": {"!!!"}, "email": {"a@acme.test"}, "admin_name": {"Ada"}, "password": {"Str0ngPassphrase!"}},
			want: "Slug must be lowercase letters/digits",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postTenantAction(t, r, "/tenants/new", tc.form)
			assert.Equal(t, http.StatusOK, w.Code, "validation failures re-render the form")
			assert.Contains(t, w.Body.String(), tc.want)
		})
	}

	var created int
	require.NoError(t, db.QueryRow(`SELECT COUNT(1) FROM tenants WHERE id LIKE 'acme%' OR slug LIKE 'acme%'`).Scan(&created))
	assert.Zero(t, created, "validation failures must not provision tenants")
}
