package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	transportdb "transport-app/db"
	"transport-app/internal/auth"
	"transport-app/internal/driver/application"
	"transport-app/internal/shared"
)

// setupLifecycleTestDB applies the real migration chain to a scratch file DB
// so route/auth wiring tests run against the true schema (incl. 00131
// nullable license columns and 00132 nullable vehicle expiries).
func setupLifecycleTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "lifecycle.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	migFS, err := fs.Sub(transportdb.Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)
	_, err = provider.Up(context.Background())
	require.NoError(t, err)

	_, err = database.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t-lc', 'LC Fleet', 'lc-fleet')`)
	require.NoError(t, err)
	return database
}

func lifecycleRouter(db *sql.DB) chi.Router {
	r := chi.NewRouter()
	NewDriverLifecycleAPIHandler(application.NewDriverAppService(db)).RegisterRoutes(r)
	return r
}

func lifecycleRequest(t *testing.T, r chi.Router, method, target, tenantID, userID string) *httptest.ResponseRecorder {
	return lifecycleRequestAs(t, r, method, target, tenantID, userID, "driver", "")
}

func lifecycleRequestAs(t *testing.T, r chi.Router, method, target, tenantID, userID, role, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	ctx := req.Context()
	if userID != "" {
		ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: userID, Role: role})
	}
	if tenantID != "" {
		ctx = shared.ContextWithTenantID(ctx, shared.TenantID(tenantID))
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req.WithContext(ctx))
	return rr
}

func TestGetOnboarding_Unauthenticated(t *testing.T) {
	r := lifecycleRouter(setupLifecycleTestDB(t))

	rr := lifecycleRequest(t, r, http.MethodGet, "/api/v1/drivers/me/onboarding", "", "")
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestGetOnboarding_MissingTenantPanics(t *testing.T) {
	r := lifecycleRouter(setupLifecycleTestDB(t))

	// Fail-closed contract: an authenticated request without a tenant must
	// panic (surfaces as 500 via Recoverer), never silently default.
	assert.Panics(t, func() {
		lifecycleRequest(t, r, http.MethodGet, "/api/v1/drivers/me/onboarding", "", "drv-1")
	})
}

func TestGetOnboarding_ReturnsState(t *testing.T) {
	db := setupLifecycleTestDB(t)
	r := lifecycleRouter(db)
	ctx := context.Background()

	require.NoError(t, application.NewDriverAppService(db).RegisterDriver(ctx, "t-lc", "drv-1", "Route Driver", "route@test.com", "9000000001"))

	rr := lifecycleRequest(t, r, http.MethodGet, "/api/v1/drivers/me/onboarding", "t-lc", "drv-1")
	require.Equal(t, http.StatusOK, rr.Code)

	var state map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &state))
	assert.Equal(t, "drv-1", state["driver_id"])
	assert.Equal(t, "profile", state["current_step"])
}

func TestGetOnboardingFunnel_Unauthenticated(t *testing.T) {
	r := lifecycleRouter(setupLifecycleTestDB(t))

	rr := lifecycleRequest(t, r, http.MethodGet, "/api/v1/drivers/onboarding/funnel", "", "")
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestGetOnboardingFunnel_ReturnsCounts(t *testing.T) {
	db := setupLifecycleTestDB(t)
	svc := application.NewDriverAppService(db)
	r := lifecycleRouter(db)
	ctx := context.Background()

	require.NoError(t, svc.RegisterDriver(ctx, "t-lc", "drv-f1", "Stuck Driver", "f1@test.com", "9000000101"))
	require.NoError(t, svc.RegisterDriver(ctx, "t-lc", "drv-f2", "Kyc Driver", "f2@test.com", "9000000102"))
	require.NoError(t, svc.SubmitLicense(ctx, "t-lc", "drv-f2", "DL-F2", "RTO",
		time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour), nil))

	rr := lifecycleRequest(t, r, http.MethodGet, "/api/v1/drivers/onboarding/funnel", "t-lc", "drv-f1")
	require.Equal(t, http.StatusOK, rr.Code)

	var funnel struct {
		TenantID    string         `json:"tenant_id"`
		Started     int            `json:"started"`
		InProgress  int            `json:"in_progress"`
		StuckByStep map[string]int `json:"stuck_by_step"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &funnel))
	assert.Equal(t, "t-lc", funnel.TenantID)
	assert.Equal(t, 2, funnel.Started)
	assert.Equal(t, 2, funnel.InProgress)
	assert.Equal(t, 1, funnel.StuckByStep["profile"])
	assert.Equal(t, 1, funnel.StuckByStep["kyc_documents"])
}

func TestGetOnboarding_TenantIsolation(t *testing.T) {
	db := setupLifecycleTestDB(t)
	r := lifecycleRouter(db)
	ctx := context.Background()

	require.NoError(t, application.NewDriverAppService(db).RegisterDriver(ctx, "t-lc", "drv-1", "Route Driver", "route@test.com", "9000000001"))

	// Tenant B asking for tenant A's driver gets only the generic empty
	// state — none of tenant A's onboarding data leaks across the boundary.
	rr := lifecycleRequest(t, r, http.MethodGet, "/api/v1/drivers/me/onboarding", "t-other", "drv-1")
	require.Equal(t, http.StatusOK, rr.Code)

	var state map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &state))
	assert.Equal(t, "profile", state["current_step"])
	assert.Empty(t, state["completed_steps"])
}

func TestReviewerEndpoints_DriverForbidden(t *testing.T) {
	db := setupLifecycleTestDB(t)
	r := lifecycleRouter(db)
	ctx := context.Background()

	require.NoError(t, application.NewDriverAppService(db).RegisterDriver(ctx, "t-lc", "drv-1", "Route Driver", "route@test.com", "9000000001"))

	// A driver session must never self-approve: license verify, claim
	// verify and vehicle assignment are reviewer-only.
	rr := lifecycleRequestAs(t, r, http.MethodPost, "/api/v1/drivers/drv-1/verify", "t-lc", "drv-1", "driver",
		`{"license_id":"lic-1","approve":true,"reason":"self approve"}`)
	assert.Equal(t, http.StatusForbidden, rr.Code)

	rr = lifecycleRequestAs(t, r, http.MethodPost, "/api/v1/drivers/drv-1/vehicle-claims/claim-1/verify", "t-lc", "drv-1", "driver",
		`{"approve":true,"reason":"self approve"}`)
	assert.Equal(t, http.StatusForbidden, rr.Code)

	rr = lifecycleRequestAs(t, r, http.MethodPost, "/api/v1/drivers/drv-1/vehicle-assignments", "t-lc", "drv-1", "driver",
		`{"vehicle_id":"veh-1","assignment_type":"dedicated"}`)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestReviewerEndpoints_Unauthenticated(t *testing.T) {
	r := lifecycleRouter(setupLifecycleTestDB(t))

	rr := lifecycleRequest(t, r, http.MethodPost, "/api/v1/drivers/drv-1/verify", "", "")
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestReviewerEndpoints_AdminPassesGate(t *testing.T) {
	db := setupLifecycleTestDB(t)
	r := lifecycleRouter(db)

	// Admin sessions reach the service layer (result code is the service's
	// business, but it must never be the role gate).
	rr := lifecycleRequestAs(t, r, http.MethodPost, "/api/v1/drivers/drv-1/verify", "t-lc", "admin-1", "admin",
		`{"license_id":"lic-missing","approve":true,"reason":"reviewed"}`)
	assert.NotEqual(t, http.StatusForbidden, rr.Code)

	rr = lifecycleRequestAs(t, r, http.MethodPost, "/api/v1/drivers/drv-1/verify", "t-lc", "org-1", "org_admin",
		`{"license_id":"lic-missing","approve":true,"reason":"reviewed"}`)
	assert.NotEqual(t, http.StatusForbidden, rr.Code)
}

func TestSubmitLicense_BadExpiryRejected(t *testing.T) {
	db := setupLifecycleTestDB(t)
	r := lifecycleRouter(db)
	ctx := context.Background()

	require.NoError(t, application.NewDriverAppService(db).RegisterDriver(ctx, "t-lc", "drv-1", "Route Driver", "route@test.com", "9000000001"))

	// A missing/malformed expiry must fail the request, never fabricate +5y.
	rr := lifecycleRequestAs(t, r, http.MethodPost, "/api/v1/drivers/me/license", "t-lc", "drv-1", "driver",
		`{"license_number":"DL-XYZ-1","issuing_authority":"RTO","expires_on":"not-a-date"}`)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestSubmitLicense_ValidAccepted(t *testing.T) {
	db := setupLifecycleTestDB(t)
	r := lifecycleRouter(db)
	ctx := context.Background()

	require.NoError(t, application.NewDriverAppService(db).RegisterDriver(ctx, "t-lc", "drv-1", "Route Driver", "route@test.com", "9000000001"))

	rr := lifecycleRequestAs(t, r, http.MethodPost, "/api/v1/drivers/me/license", "t-lc", "drv-1", "driver",
		`{"license_number":"DL-XYZ-1","issuing_authority":"RTO","expires_on":"2032-12-31","classes":["LMV"]}`)
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestSubmitForVerification_RouteWired(t *testing.T) {
	db := setupLifecycleTestDB(t)
	svc := application.NewDriverAppService(db)
	r := lifecycleRouter(db)
	ctx := context.Background()

	// The mobile app's final onboarding step posts here: an unregistered
	// route used to 404, stranding drivers at the last step.
	rr := lifecycleRequestAs(t, r, http.MethodPost, "/api/v1/drivers/me/verification/submit", "t-lc", "drv-1", "driver", "")
	assert.NotEqual(t, http.StatusNotFound, rr.Code, "route must be registered")

	// Unauthenticated → 401, not 404.
	rr = lifecycleRequest(t, r, http.MethodPost, "/api/v1/drivers/me/verification/submit", "", "")
	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	// No license yet → 400 (service contract, not a routing artifact).
	require.NoError(t, svc.RegisterDriver(ctx, "t-lc", "drv-1", "Route Driver", "route@test.com", "9000000001"))
	rr = lifecycleRequestAs(t, r, http.MethodPost, "/api/v1/drivers/me/verification/submit", "t-lc", "drv-1", "driver", "")
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	// With license → 200, onboarding submitted.
	require.NoError(t, svc.SubmitLicense(ctx, "t-lc", "drv-1", "DL-XYZ-1", "RTO",
		time.Now().Add(-24*time.Hour), time.Now().Add(365*24*time.Hour), nil))
	rr = lifecycleRequestAs(t, r, http.MethodPost, "/api/v1/drivers/me/verification/submit", "t-lc", "drv-1", "driver", "")
	require.Equal(t, http.StatusOK, rr.Code)

	state, err := svc.GetOnboardingState(ctx, "t-lc", "drv-1")
	require.NoError(t, err)
	assert.Equal(t, "submitted", state.OverallStatus)
}
