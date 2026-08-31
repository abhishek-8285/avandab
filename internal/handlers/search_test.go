package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

// withUserAndTenant sets both the session user and the tenant context.
func withUserAndTenant(userID, tenant string, authSrv auth.AuthorizationService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), auth.ContextUser, &auth.SessionData{UserID: userID})
			ctx = shared.ContextWithTenantID(ctx, shared.TenantID(tenant))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newSearchTestApp(t *testing.T) *App {
	t.Helper()
	name := fmt.Sprintf("test_search_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	migrationsDir := "../../db/migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		for _, cand := range []string{"db/migrations", "../db/migrations", "../../db/migrations"} {
			if _, err := os.Stat(cand); err == nil {
				migrationsDir = cand
				break
			}
		}
	}
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
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

	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	tmpl, err := parseTemplates(allowAllAuthSvc{})
	require.NoError(t, err)

	return &App{DB: db, Templates: tmpl, AuthSrv: allowAllAuthSvc{}}
}

func seedSearchFixtures(t *testing.T, app *App) {
	t.Helper()
	_, err := app.DB.Exec(`
INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, status, odometer, insurance_expiry, fitness_expiry, permit_expiry, current_mileage)
VALUES ('v-1', 'MH01AB1234', 'Tata Ace 1', 'truck', 2, 'diesel', 'available', 1000, '2030-01-01', '2030-01-01', '2030-01-01', 12);

INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
VALUES ('d-1', 'DRV-9', 'Rajesh', 'Kumar', '+919820011223', 'MH1220110012345', '2030-01-01', 'available', '1');

INSERT INTO customers (id, name, phone, gst) VALUES ('c-1', 'Acme Logistics', '9820011223', '27ABCDE1234F1Z5');

INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
VALUES ('r-1', 'Mumbai', 'Pune', 150, 4, 5000);

INSERT INTO bookings (id, booking_number, customer_id, route_id, pickup_date, vehicle_type, passengers, price, status, created_at)
VALUES ('b-1', 'BK-1001', 'c-1', 'r-1', '2026-08-01', 'truck', 0, 5000, 'pending', '2026-08-01 10:00:00');
`)
	require.NoError(t, err)
}

func TestSearchPage_GroupsAndMatches(t *testing.T) {
	app := newSearchTestApp(t)
	seedSearchFixtures(t, app)

	r := chi.NewRouter()
	r.Get("/search", app.SearchPage)

	get := func(url string) *httptest.ResponseRecorder {
		req := withSession(httptest.NewRequest(http.MethodGet, url, nil), "u1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := get("/search?q=MH01")
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "MH01AB1234")
	assert.Contains(t, body, "/vehicles/v-1")

	w = get("/search?q=Acme")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Acme Logistics")
	assert.Contains(t, w.Body.String(), "27ABCDE1234F1Z5")

	w = get("/search?q=9820011223")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Rajesh Kumar")

	w = get("/search?q=BK-1001")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "BK-1001")

	// No matches → friendly empty state, not a blank page
	w = get("/search?q=zzzznothing")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "No matches found")

	// No query → landing empty state
	w = get("/search")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Search everything")

	// LIKE wildcards must be escaped, not treated as patterns
	w = get("/search?q=%25%25")
	require.Equal(t, http.StatusOK, w.Code)
}

// denyAllAuthSvc blocks every permission — search must return an empty page,
// not leak rows the user cannot list.
type denyAllAuthSvc struct{ allowAllAuthSvc }

func (denyAllAuthSvc) Can(userID, resource, action string) bool { return false }

func TestSearchPage_PermissionGated(t *testing.T) {
	app := newSearchTestApp(t)
	seedSearchFixtures(t, app)
	app.AuthSrv = denyAllAuthSvc{}

	r := chi.NewRouter()
	r.Get("/search", app.SearchPage)

	req := withSession(httptest.NewRequest(http.MethodGet, "/search?q=MH01", nil), "u1", "viewer")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "No matches found")
	assert.NotContains(t, w.Body.String(), "MH01AB1234")
	_ = auth.SessionData{}
}

// TestSearchAPI_TenantScopedAndPermFiltered — Spec 22 §7 S6: "mh12"-style
// queries return tenant-scoped hits across entity types and sections are
// dropped when the caller lacks the read permission.
func TestSearchAPI_TenantScopedAndPermFiltered(t *testing.T) {
	app := newSearchTestApp(t)

	// tenant-a vehicle MH12AB9999; tenant-b vehicle MH12ZZ7777.
	_, err := app.DB.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
	    VALUES ('v-a','MH12AB9999','Truck A','truck',1,'available','2030-01-01','2030-01-01','2030-01-01','tenant-a')`)
	require.NoError(t, err)
	_, err = app.DB.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
	    VALUES ('v-b','MH12ZZ7777','Truck B','truck',1,'available','2030-01-01','2030-01-01','2030-01-01','tenant-b')`)
	require.NoError(t, err)

	denyVehicles := &denyResourceAuthSvc{resource: "vehicles", allowAllAuthSvc: allowAllAuthSvc{}}
	apiAppDenied := &App{DB: app.DB, AuthSrv: denyVehicles}

	r := chi.NewRouter()
	r.With(withUserAndTenant("u1", "tenant-a", nil)).Get("/api/search", app.SearchAPI)
	r.With(withUserAndTenant("u1", "tenant-a", nil)).Get("/nv/api/search", apiAppDenied.SearchAPI)

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=MH12", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Vehicles []struct {
			ID string `json:"id"`
		} `json:"vehicles"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Vehicles, 1, "only tenant-a's vehicle may appear")
	assert.Equal(t, "v-a", resp.Vehicles[0].ID)

	// Permission filter: vehicles section vanishes for a caller without
	// vehicles:read.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/nv/api/search?q=MH12", nil)
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	assert.NotContains(t, w2.Body.String(), `"v-a"`)
}

type denyResourceAuthSvc struct {
	allowAllAuthSvc
	resource string
}

func (d *denyResourceAuthSvc) Can(userID string, resource, action string) bool {
	return resource != d.resource
}
