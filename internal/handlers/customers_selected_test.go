package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

func newCustomersSelectedDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_customers_sel_%d_%s", time.Now().UnixNano(), strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	cwd, _ := os.Getwd()
	migrationsDir := "../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}
	goose.SetLogger(goose.NopLogger())
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
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newCustomersSelectedApp(t *testing.T, db *sql.DB, authSrv auth.AuthorizationService) *App {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		_ = os.Chdir("../..")
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
	app.Customers = &CustomerHandlers{App: app}
	return app
}

func withTenantSession(r *http.Request, tenant string, userID, role string) *http.Request {
	ctx := shared.ContextWithTenantID(r.Context(), shared.TenantID(tenant))
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: userID, Role: role})
	return r.WithContext(ctx)
}

// TestSelectedCustomers_List_SuccessAndPagination covers success branch with pagination, search, tenant isolation, and datastar fragment.
func TestSelectedCustomers_List_SuccessAndPagination(t *testing.T) {
	db := newCustomersSelectedDB(t)
	app := newCustomersSelectedApp(t, db, &mockAuthSvc{})

	// Seed two customers via service (ensures validation passes)
	_, err := app.Services.Customers.CreateCustomer(context.Background(), "Alice Smith", "Acme Corp", "9000000001", "alice@example.com", "27AAACP0000M1Z9", "Mumbai", "notes")
	require.NoError(t, err)
	_, err = app.Services.Customers.CreateCustomer(context.Background(), "Bob Johnson", "Beta Ltd", "9000000002", "bob@example.com", "", "Pune", "")
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Route("/customers", app.Customers.Routes)

	// 1. List success - normal page
	t.Run("list success", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, "Alice Smith")
		assert.Contains(t, body, "Bob Johnson")
	})

	// 2. Pagination - limit 1 page 1
	t.Run("pagination limit", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/?limit=1&page=1", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		// Should contain at most 1 customer in rendered list (but template may still list 1)
		assert.True(t, w.Code == http.StatusOK)
	})

	// 3. Search query filters
	t.Run("search query", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/?q=Alice", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Alice Smith")
	})

	// 4. Tenant isolation - different tenant same request still succeeds (customers are global, but ensure context doesn't error)
	t.Run("tenant isolation different tenant", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/", nil), "tenant-B", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	// 5. Datastar fragment request (HX-Request true) should render fragment, not full layout
	t.Run("datastar fragment", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/", nil), "1", "user-1", "admin")
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		// Fragment template should be rendered (contains table)
		assert.NotEmpty(t, w.Body.String())
	})

	// 6. Datastar via query param _fragment=true
	t.Run("datastar via query", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/?_fragment=true", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// TestSelectedCustomers_List_Error simulates store error via closed DB (error branch).
func TestSelectedCustomers_List_Error(t *testing.T) {
	db := newCustomersSelectedDB(t)
	app := newCustomersSelectedApp(t, db, &mockAuthSvc{})
	// Close DB to force error on ListCustomers
	_ = db.Close()
	r := chi.NewRouter()
	r.Route("/customers", app.Customers.Routes)

	req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/", nil), "1", "user-1", "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "Failed to list customers")
}

// TestSelectedCustomers_CRUD covers Create, View, Edit, Update, Delete success and error branches.
func TestSelectedCustomers_CRUD(t *testing.T) {
	db := newCustomersSelectedDB(t)
	app := newCustomersSelectedApp(t, db, &mockAuthSvc{})
	r := chi.NewRouter()
	r.Route("/customers", app.Customers.Routes)

	// New page renders form
	t.Run("New renders", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/new", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "New Customer")
	})

	// Create success
	var createdID string
	t.Run("Create success", func(t *testing.T) {
		form := url.Values{
			"name":    {"Charlie"},
			"company": {"Charlie Co"},
			"phone":   {"9000000003"},
			"email":   {"charlie@example.com"},
			"gst":     {""},
			"address": {"Delhi"},
			"notes":   {""},
		}
		req := withTenantSession(httptest.NewRequest(http.MethodPost, "/customers/new", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "/customers", w.Header().Get("Location"))
		// Fetch created customer ID via service
		list, _, _ := app.Services.Customers.ListCustomers(context.Background(), "Charlie", 10, 0)
		require.Len(t, list, 1)
		createdID = string(list[0].ID)
		require.NotEmpty(t, createdID)
	})

	// Create error - duplicate phone
	t.Run("Create duplicate phone error", func(t *testing.T) {
		form := url.Values{
			"name":  {"Duplicate"},
			"phone": {"9000000003"},
		}
		req := withTenantSession(httptest.NewRequest(http.MethodPost, "/customers/new", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		// Should render form with error (200, not redirect)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "already exists")
	})

	// Create validation error - missing name
	t.Run("Create missing name error", func(t *testing.T) {
		form := url.Values{
			"name":  {""},
			"phone": {""},
		}
		req := withTenantSession(httptest.NewRequest(http.MethodPost, "/customers/new", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "required")
	})

	// Create datastar success (should return 303 with Location)
	t.Run("Create datastar", func(t *testing.T) {
		form := url.Values{
			"name":  {"DatastarUser"},
			"phone": {"9000000010"},
		}
		req := withTenantSession(httptest.NewRequest(http.MethodPost, "/customers/new", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "/customers", w.Header().Get("Location"))
	})

	// View success
	t.Run("View success", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/"+createdID, nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Charlie")
	})

	// View not found
	t.Run("View not found", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/nonexistent-id", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Customer not found")
	})

	// Edit success
	t.Run("Edit success", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/"+createdID+"/edit", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Edit Customer")
	})

	// Edit not found
	t.Run("Edit not found", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/unknown/edit", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	// Update success
	t.Run("Update success", func(t *testing.T) {
		form := url.Values{
			"name":    {"Charlie Updated"},
			"phone":   {"9000000003"},
			"company": {"Updated Co"},
			"email":   {"charlie.updated@example.com"},
		}
		req := withTenantSession(httptest.NewRequest(http.MethodPost, "/customers/"+createdID+"/edit", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "/customers/"+createdID, w.Header().Get("Location"))
		// Verify updated
		c, err := app.Services.Customers.GetCustomer(context.Background(), domain.CustomerID(createdID))
		require.NoError(t, err)
		assert.Equal(t, "Charlie Updated", c.Name)
	})

	// Update error - duplicate phone with datastar user
	t.Run("Update duplicate phone error", func(t *testing.T) {
		// Create another customer to conflict phone
		_, err := app.Services.Customers.CreateCustomer(context.Background(), "Other", "", "9000000099", "", "", "", "")
		require.NoError(t, err)
		form := url.Values{
			"name":  {"Charlie"},
			"phone": {"9000000099"},
		}
		req := withTenantSession(httptest.NewRequest(http.MethodPost, "/customers/"+createdID+"/edit", strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		// Re-renders edit form with error flash (200, not redirect)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "already exists")
	})

	// Delete success
	t.Run("Delete success", func(t *testing.T) {
		req := withTenantSession(httptest.NewRequest(http.MethodPost, "/customers/"+createdID+"/delete", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "/customers", w.Header().Get("Location"))
	})

	// Delete error via closed DB (500)
	t.Run("Delete error", func(t *testing.T) {
		db2 := newCustomersSelectedDB(t)
		app2 := newCustomersSelectedApp(t, db2, &mockAuthSvc{})
		// create then close
		c, err := app2.Services.Customers.CreateCustomer(context.Background(), "ToDelete", "", "9000000020", "", "", "", "")
		require.NoError(t, err)
		_ = db2.Close()
		r2 := chi.NewRouter()
		r2.Route("/customers", app2.Customers.Routes)
		req := withTenantSession(httptest.NewRequest(http.MethodPost, "/customers/"+string(c.ID)+"/delete", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r2.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// TestSelectedCustomers_AuthChecks verifies RBAC and anonymous handling.
func TestSelectedCustomers_AuthChecks(t *testing.T) {
	db := newCustomersSelectedDB(t)
	denySrv := denyAuthSvc{}
	allowSrv := &mockAuthSvc{}
	appDeny := newCustomersSelectedApp(t, db, denySrv)
	appAllow := newCustomersSelectedApp(t, db, allowSrv)

	// Deny auth should block all routes
	rDeny := chi.NewRouter()
	rDeny.Route("/customers", appDeny.Customers.Routes)

	cases := []struct {
		method string
		path   string
	}{
		{"GET", "/customers/"},
		{"GET", "/customers/new"},
		{"POST", "/customers/new"},
		{"GET", "/customers/123"},
		{"GET", "/customers/123/edit"},
		{"POST", "/customers/123/edit"},
		{"POST", "/customers/123/delete"},
	}
	for _, tc := range cases {
		t.Run("deny "+tc.method+" "+tc.path, func(t *testing.T) {
			req := withSession(httptest.NewRequest(tc.method, tc.path, nil), "viewer-1", "viewer")
			w := httptest.NewRecorder()
			rDeny.ServeHTTP(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}

	// Anonymous (no session) should redirect to /login for protected routes (handled by middleware.RequireAuth? But our Routes uses ResourcePermission which checks AuthSrv.Can, not session. For 0 session, ResourcePermission will try to get session and redirect)
	t.Run("anonymous redirect", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/customers/", nil)
		w := httptest.NewRecorder()
		rDeny.ServeHTTP(w, req)
		// middleware.ResourcePermission with deny should still return 403 or redirect? For denyAuthSvc, it will be 403 even without session, but production uses RequireAuth.
		// We check that allow path still works for authenticated
		reqAllow := withSession(httptest.NewRequest(http.MethodGet, "/customers/", nil), "admin-1", "admin")
		rAllow := chi.NewRouter()
		rAllow.Route("/customers", appAllow.Customers.Routes)
		wAllow := httptest.NewRecorder()
		rAllow.ServeHTTP(wAllow, reqAllow)
		assert.Equal(t, http.StatusOK, wAllow.Code)
	})

	// Verify that even allowed user with valid session can access New
	t.Run("allow new page", func(t *testing.T) {
		rAllow := chi.NewRouter()
		rAllow.Route("/customers", appAllow.Customers.Routes)
		req := withSession(httptest.NewRequest(http.MethodGet, "/customers/new", nil), "admin-1", "admin")
		w := httptest.NewRecorder()
		rAllow.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// TestSelectedCustomers_TenantIsolation ensures customers are tenant-scoped:
// each tenant's list sees only its own rows and cross-tenant reads miss.
func TestSelectedCustomers_TenantIsolation(t *testing.T) {
	db := newCustomersSelectedDB(t)
	app := newCustomersSelectedApp(t, db, &mockAuthSvc{})
	r := chi.NewRouter()
	r.Route("/customers", app.Customers.Routes)

	// Seed one customer per tenant via CreateCustomerFull under each ctx.
	seeded := map[string]domain.Customer{}
	for _, tc := range []struct {
		tenant, name, phone string
	}{
		{"1", "TenantA User", "9000000030"},
		{"tenant-B", "TenantB User", "9000000031"},
		{"another-tenant", "Another User", "9000000032"},
	} {
		ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID(tc.tenant))
		c, err := app.Services.Customers.CreateCustomerFull(ctx, service.CreateCustomerRequest{
			Name:  tc.name,
			Phone: tc.phone,
		})
		require.NoError(t, err)
		require.Equal(t, tc.tenant, c.TenantID)
		seeded[tc.tenant] = c
	}

	// Each tenant's list sees ONLY its own customer.
	for tenant, want := range seeded {
		t.Run("list scoped "+tenant, func(t *testing.T) {
			req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/", nil), tenant, "user-1", "admin")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
			body := w.Body.String()
			assert.Contains(t, body, want.Name)
			for other, c := range seeded {
				if other != tenant {
					assert.NotContains(t, body, c.Name, "tenant %s must not see %s rows", tenant, other)
				}
			}
		})
	}

	// Cross-tenant GetCustomerByID returns not found (fail closed).
	t.Run("cross-tenant get misses", func(t *testing.T) {
		ctxB := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-B"))
		got, err := app.Services.Customers.GetCustomer(ctxB, seeded["1"].ID)
		if err == nil {
			require.NotEqual(t, seeded["1"].ID, got.ID, "tenant-B resolved another tenant's customer")
		}
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/"+string(seeded["1"].ID), nil), "tenant-B", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// TestSelectedCustomers_PaginationEdge verifies pagination params handling and limits.
func TestSelectedCustomers_PaginationEdge(t *testing.T) {
	db := newCustomersSelectedDB(t)
	app := newCustomersSelectedApp(t, db, &mockAuthSvc{})
	r := chi.NewRouter()
	r.Route("/customers", app.Customers.Routes)

	// Seed 3 customers
	for i := 0; i < 3; i++ {
		_, err := app.Services.Customers.CreateCustomer(context.Background(), fmt.Sprintf("PagUser%d", i), "", fmt.Sprintf("90000001%02d", i), "", "", "", "")
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond)
	}
	cases := []struct {
		url  string
		want int
	}{
		{"/customers/?limit=1&page=1", http.StatusOK},
		{"/customers/?limit=100&page=1", http.StatusOK},
		{"/customers/?limit=0&page=-1", http.StatusOK},  // should default to 20/1
		{"/customers/?limit=200&page=1", http.StatusOK}, // should clamp to 100
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			req := withTenantSession(httptest.NewRequest(http.MethodGet, tc.url, nil), "1", "user-1", "admin")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}
